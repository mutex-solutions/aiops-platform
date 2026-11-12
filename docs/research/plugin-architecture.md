# Plugin architecture for a long-running multi-tenant AIOps server

Research note, 2026-10-27. Scope: how to implement "everything is a plugin, testable in isolation, composed by profiles" for the aiops-platform control plane, and which language/mechanism to pick. All facts carry the source URL and the date/version they were checked against. Anything not confirmed from a primary source is marked **UNVERIFIED**.

Historical research snapshot: recommendations and SDK sketches below are superseded where they differ from the [canonical decisions](../architecture/00-decisions.md). Editorial preparation follows [ADR-056](../adr/ADR-056-documentation-baseline.md); it supplies no new measurement or publication authorization, and the publication-rights gate remains open.

## 0. Summary and recommendation

**Recommendation: a Go kernel with a compile-time module registry (Caddy / Alloy / OTel-Collector style), a gRPC-over-UDS "remote plugin" adapter for every extension point so Python LLM pillars run as out-of-process plugins (Dapr pluggable-component style), and dsh-style YAML patch-layer profiles for composition. Do not build the kernel on Cordis/Node, do not use Go's native `plugin` package, and defer Wasm (Extism/wazero) to a later phase for small sandboxed contributor plugins (tool, approval-policy, report-renderer).**

Rationale for this historical recommendation: Alloy, Mimir, Loki, Tempo, Pyroscope and the OTel Collector use Go and registry-based composition. Public Python tools such as HolmesGPT, krr and Robusta motivate a language-neutral process boundary. Cordis's README warns that its API is not yet stable; the ordered YAML patch layers, row-by-id replacement and bundle manifests demonstrated by dsh remain useful design references. The current decision is a Go port of the pinned Cordis model, with its public upstream provenance and MIT notices retained in the canonical record; this survey does not override that decision.

## 1. Constraints that drive the choice

| Constraint | Consequence for the plugin mechanism |
|---|---|
| Long-running multi-tenant server, hosted by us and self-hosted by users | Plugins must be configured per tenant; a crashing plugin must not take down other tenants; self-hosters want one binary or one compose file. |
| Grafana-stack data plane (Alloy on hosts, Mimir/Loki/Tempo/Pyroscope) | Tenant identity is the `X-Scope-OrgID` header ([Mimir docs](https://grafana.com/docs/mimir/latest/manage/secure/authentication-and-authorization/), checked 2026-10-27); Grafana orgs are the UI-side isolation unit ([Grafana docs](https://grafana.com/docs/grafana/latest/administration/organization-management/)). Our tenant ID must be valid for both (see §5). |
| LLM pillars (incident, change, compliance, SecOps, FinOps, continuous improvement, exec reporting) | The LLM ecosystem we reuse is Python: HolmesGPT toolsets ([holmesgpt.dev](https://holmesgpt.dev/latest/data-sources/custom-toolsets/)), krr, Robusta. A language-neutral adapter lets Python tools participate without requiring a Python control plane. |
| On-call module | Grafana OnCall OSS entered maintenance 2025-03-11 and was archived 2026-03-24; Cloud Connection and the IRM mobile app stopped working with OSS on that date ([Grafana blog](https://grafana.com/blog/grafana-oncall-maintenance-mode/), [migration doc](https://grafana.com/docs/grafana-cloud/alerting-and-irm/irm/set-up/migrate/oncall-oss/)). The repo now lives at `grafana-cold-storage/oncall`, AGPLv3, read-only. So `on-call-provider` is a first-class extension point with no upstream to lean on. |
| "Everything is a plugin, testable in isolation before attach" (dsh/Cordis model) | Need: a manifest, explicit dependency declaration, lifecycle with reversible effects, a fake kernel for tests, and a conformance kit per extension point. |
| Expect many external contributors | Low barrier: a contributor should be able to write a plugin in Go **or** Python, run the conformance kit locally with no kernel checkout, and publish to an OCI registry. |

## 2. Evidence survey

### 2.1 Go options

| Option | Mechanism | Isolation | Cross-language | Hot reload | State checked |
|---|---|---|---|---|---|
| **Caddy modules** | Named type registers itself in `init()` via `caddy.RegisterModule`; `CaddyModule()` returns `ModuleInfo{ID, New}`; IDs are `namespace.name` and each namespace maps to a Go interface; JSON config unmarshals straight into the module struct; guests loaded with `ctx.LoadModule`. Lifecycle interfaces `Provisioner`, `Validator`, `CleanerUpper`. Compiled in with `xcaddy` (throwaway go module with blank imports). | In-process (none) | No | Config reload with overlapping instances; code requires rebuild | Caddy v2 docs, [caddyserver.com/docs/extending-caddy](https://caddyserver.com/docs/extending-caddy), checked 2026-10-27 |
| **OTel Collector Builder (ocb)** | `builder-config.yaml` with `dist` block and `receivers/processors/exporters/extensions/connectors/providers` lists of `gomod` entries plus `replaces`; generates sources and compiles a distribution; `--skip-compilation` lets a repo commit the generated sources; strict versioning checks every component's `gomod` against `dist::otelcol_version` major/minor. Component contract is `Start(ctx, Host) error` / `Shutdown(ctx) error`; `Start` error aborts collector startup; `Shutdown` must be safe without `Start`. Stability enum `Undefined, Unmaintained, Deprecated, Development, Alpha, Beta, Stable`. | In-process | No | No | [ocb README](https://github.com/open-telemetry/opentelemetry-collector/blob/main/cmd/builder/README.md) (main, example pins v0.129.0); [component v1.70.0](https://pkg.go.dev/go.opentelemetry.io/collector/component) published 2026-10-26 |
| **Grafana Alloy component registry** | `component.Register(Registration{Name, Stability, Community, Args, Exports, Build})` from `init()`; `Register` panics on duplicate name, bad identifier, prefix ambiguity, or missing stability; `Registry.Get` refuses community components unless `--feature.community-components.enabled` and gates on `MinStability`. Component contract `Run(ctx) error` (blocks, called exactly once) + `Update(args) error` (called concurrently with `Run`). `Options` carries `ID, Logger, Registerer, Tracer, DataPath, OnStateChange, GetServiceData, MinStability`. | In-process | No | Config yes (`Update`); code no | [registry.go](https://github.com/grafana/alloy/blob/main/internal/component/registry.go), [component.go](https://github.com/grafana/alloy/blob/main/internal/component/component.go), main, checked 2026-10-27. Note: only `StabilityUndefined/PublicPreview/GenerallyAvailable` referenced in registry.go; an "experimental" level was not seen there. |
| **Prometheus SD registry** | `discovery.RegisterConfig(cfg)` registers `cfg.Name()+"_sd_configs"`; panics on duplicate; the registry builds a dynamic struct with `reflect.StructOf` so YAML keys like `kubernetes_sd_configs` are discovered at unmarshal time. Only `static_configs` is registered by default; all others register in their package `init()`. | In-process | No | No | [discovery/registry.go](https://github.com/prometheus/prometheus/blob/main/discovery/registry.go), main, checked 2026-10-27 |
| **hashicorp/go-plugin** | Subprocess plugins over net/rpc or gRPC on a local socket; `HandshakeConfig{ProtocolVersion, MagicCookieKey/Value}` (cookie is "not a security measure, just a UX feature"); `VersionedPlugins` negotiates protocol versions; `SecureConfig` checksums the binary; `AutoMTLS` "has the client and server automatically negotiate mTLS for transport authentication"; `Reattach` lets the host upgrade while plugins keep running; panics in plugins cannot crash the host; stdout/stderr forwarded. "Only designed to work over a local [reliable] network." Users: Packer, Terraform, Nomad, Vault, Boundary, Waypoint. | Process | Yes (gRPC) | Reattach only | v1.8.0 published 2026-04-29, MPL-2.0 ([pkg.go.dev](https://pkg.go.dev/github.com/hashicorp/go-plugin), [README](https://github.com/hashicorp/go-plugin/blob/main/README.md)); pkg.go.dev flags v1.8.0 as "not the latest" so a newer tag may exist, **UNVERIFIED** |
| **Grafana backend plugins** | Built on go-plugin/gRPC since Grafana v7.0; each backend runs as a subprocess; capabilities QueryData, CallResource, CheckHealth, metrics, streaming; official SDK is Go (`grafana-plugin-sdk-go`); instance management caches per-datasource state. Signing: `@grafana/sign-plugin` writes `MANIFEST.txt` (file SHA256s + PGP signature, `manifestVersion 2.0.0`); levels private (needs `--rootUrls`), community, commercial; token is a Grafana Cloud access policy with `plugins:write`; sign-plugin v4 requires Node 24.15+. Unsigned plugins need `[plugins] allow_loading_unsigned_plugins = id1,id2` (env `GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS`); enforced since Grafana 7.4.0 per [grafana/grafana#31125](https://github.com/grafana/grafana/issues/31125) (secondary source; the official signatures page 404'd during this research, **UNVERIFIED** for exact wording). | Process | gRPC, Go SDK only | No | [backend overview](https://grafana.com/developers/plugin-tools/key-concepts/backend-plugins/), [sign a plugin](https://grafana.com/developers/plugin-tools/publish-a-plugin/sign-a-plugin), checked 2026-10-27 |
| **Dapr pluggable components** | Component runs "as a distinct process or container in a pod", registers with the Dapr sidecar over Unix Domain Sockets using gRPC, written in "any gRPC-supported language", released on its own cadence, started by the user not by Dapr. Built-ins are Go-only and compiled into the runtime. SDKs listed: Go and .NET, both "In development" (preview feature). Conformance suite: `tests/config/<type>/<component>/*.yaml` + `tests.yml` with `operations:` list, run with `go test -tags=conftests ./tests/conformance -run TestStateConformance/redis`; CI matrix split into `pr-components` (docker, no secrets) and `cron-components` (cloud creds). | Process/container | Yes | n/a | Dapr docs v1.19, page modified 2026-10-25 ([overview](https://docs.dapr.io/developing-applications/develop-components/pluggable-components/pluggable-components-overview/), [SDKs](https://docs.dapr.io/developing-applications/develop-components/pluggable-components/pluggable-components-sdks/)); [conformance README](https://github.com/dapr/components-contrib/blob/main/tests/conformance/README.md). Claim "conformance required before Stable" **UNVERIFIED**. |
| **WebAssembly: Extism + wazero** | Extism Go SDK runs `.wasm` plugins on wazero; `Manifest{Wasm: [file/url/data + sha256 hash], Config, AllowedHosts, AllowedPaths, Memory{max_pages, max_http_response_bytes, max_var_bytes}}`; `allowed_hosts` empty = no network, `null` = all; bytes-in/bytes-out `Call`; host functions via `NewHostFunctionWithStack`; `NewCompiledPlugin(...).Instance()` for cheap concurrent instances; PDKs for Rust, JS, Go (TinyGo), Haskell, AssemblyScript, C, Zig, .NET (Python PDK not on the PDK page). wazero: zero-dependency pure-Go, 1.0 March 2023, passes Core 1.0 and 2.0 conformance, WASI = `wasi_snapshot_preview1` only (no preview2/component model on the spec page). `timeout_ms` not on the manifest page, **UNVERIFIED**. | Sandbox (memory, hosts, paths) | Yes (any PDK) | Yes (re-instantiate) | [go-sdk README](https://github.com/extism/go-sdk/blob/main/README.md), [manifest](https://extism.org/docs/concepts/manifest), [PDKs](https://extism.org/docs/concepts/pdk), [wazero](https://wazero.io/), [wazero specs](https://wazero.io/specs/), checked 2026-10-27 |
| **wasmCloud** | Components are "stateless, sandboxed binaries" following the Component Model; v2 docs use "services" and "host plugins" for non-Wasm capabilities; runtime links components by interface at run time (`implements <component>` label since 2.10.0); tracks Wasmtime and WASI P2 with P3 planned. Operational weight (host, wadm, NATS) is far above what a self-hoster wants. CNCF status not on the page, **UNVERIFIED**. | Sandbox | Yes | Yes | [wasmCloud v2 components](https://wasmcloud.com/docs/concepts/components), updated 2026-09-23 |
| **Envoy Proxy-Wasm** | ABI v0.2.1 between proxies and Wasm extensions; SDKs C++, Rust, Go, AssemblyScript; hosts Envoy, ATS, NGINX (Kong module), MOSN, OpenResty. Relevant only as prior art for "ABI as the contract". | Sandbox | Yes | Yes | [proxy-wasm/spec README](https://github.com/proxy-wasm/spec/blob/main/README.md), checked 2026-10-27 |
| **Go native `plugin` package** | Official doc: "Plugins are currently supported only on Linux, FreeBSD, and macOS"; crashes likely unless "the same version of the toolchain, the same build tags, and the same values of certain flags and environment variables"; "all common dependencies of the application and its plugins are built from exactly the same source code"; "A plugin is only initialized once, and cannot be closed"; "poorly supported by the Go race detector"; the doc itself suggests blank-importing plugins into one static binary, or IPC. | In-process, worse than static | No | No | [pkg.go.dev/plugin](https://pkg.go.dev/plugin), Go 1.27.3 docs, checked 2026-10-27 |
| **Kubernetes operator / CRD as contract** | "Operators are software extensions to Kubernetes that make use of custom resources"; controller reconciles desired vs actual; runs as an ordinary Deployment; "any language / runtime that can act as a client for the Kubernetes API". Strong for *deployment and tenant configuration*, weak as the in-request plugin call path (reconcile loops are eventually consistent, not RPC). | Process/pod | Yes | Yes | [kubernetes.io operator](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/), last modified 2025-07-16, docs v1.37 |

Takeaways for Go:

- Every Grafana/OTel/Prometheus project in our data plane uses the same shape: global registry filled from `init()`, panics on bad registration, a `Build`/`Create` factory, a stability gate, and a build manifest (ocb) or build tool (xcaddy) to produce a custom binary. Contributors to this ecosystem already know it.
- The contract they converge on is small: a zero-value `Args` struct the kernel unmarshals, `Validate`, `Start/Run`, `Update`, `Shutdown`, plus an `Options`/`Host` bag of services. We copy that shape.
- For cross-language and failure containment the industry answer is gRPC subprocess/sidecar (go-plugin, Grafana, Dapr), not shared libraries. go-plugin's subprocess handshake (stdout negotiation line, magic cookie) is awkward for containerised Python; Dapr's "you start it, it registers over a UDS" model fits Kubernetes and compose better.

### 2.2 Python options

| Option | Mechanism | Notes | State checked |
|---|---|---|---|
| **pluggy** | `HookspecMarker("project")` defines hooks, `HookimplMarker("project")` implements, `PluginManager.register()` / `load_setuptools_entrypoints("group")`; LIFO call order, `firstresult`, `historic`, `tryfirst/trylast`, generator `wrapper=True` (1.1+); impls validated against specs at registration; `check_pending()` enforces specs. Users: pytest (1400+ plugins), tox, devpi, Kedro. | Mature, tiny, in-process only; no lifecycle (`apply/dispose`) of its own, no config schema, no isolation. | pluggy 1.7.0 released 2026-10-14, [docs](https://pluggy.readthedocs.io/en/stable/) |
| **importlib.metadata entry points** | `entry_points.txt` in `*.dist-info`; groups must match `^\w+(\.\w+)*$`; object refs `module:attr`; recommended group naming "names starting with a PyPI name owned by the consumer project, followed by `.`"; `entry_points(group=...)` selectable API since 3.10, always returns `EntryPoints` since 3.12, `EntryPoint` lost tuple interface in 3.13. | stdlib discovery; this is what pluggy, Airflow and Dagster build on. | [PyPA spec](https://packaging.python.org/en/latest/specifications/entry-points/) updated Jan 2026; [importlib.metadata](https://docs.python.org/3/library/importlib.metadata.html) Python 3.15.2 docs dated 2026-10-26 |
| **Airflow providers** | A provider is a package exposing the `apache_airflow_provider` entry point pointing at `get_provider_info()` which returns a dict validated by `provider_info.schema.json` (`hooks`, `connection-types`, `extra-links`, `notifications`, `executors`, `config`, `plugins`, `auth-managers`, `secrets-backends`, `triggers`, ...). Airflow scans installed distributions; `airflow providers` CLI verifies discovery. | Best Python example of "manifest returned by code + JSON schema". No isolation; a bad provider import is swallowed as optional feature. | apache-airflow 3.4.0 docs, [create custom providers](https://airflow.apache.org/docs/apache-airflow-providers/howto/create-custom-providers.html) |
| **Dagster** | Integration libraries are separate `dagster-*` packages exposing `ConfigurableResource` (Pydantic) and asset definitions wired through `Definitions`; the `dg` CLI discovers custom component types via the `dagster_dg.plugin` entry point group (renamed from `dagster_dg.library`); core and libraries versioned separately (e.g. 1.10.x / 0.26.x). | Same entry-point pattern; Pydantic config models are the schema. Details from search summaries of the Dagster changelog, exact release **UNVERIFIED**. | [docs.dagster.io changelog](https://docs.dagster.io/about/changelog), [dg discussion #28472](https://github.com/dagster-io/dagster/discussions/28472) |
| **HolmesGPT toolsets** | YAML `toolsets: <name>: {description, tags [core|cli|cluster], prerequisites: [{env: [...]}, {command: ...}], installation_instructions, tools: [{name, description, command, parameters}]}`; `{{ var }}` LLM-inferred, `${VAR}` env hidden from the LLM, `{{ env.X }}` Jinja2; loaded via `--custom-toolsets`, Helm `toolsets:` values; `holmes toolset refresh` re-checks prerequisites. HTTP-type and MCP-server toolsets exist as separate types; Python `Toolset` subclasses with `prerequisites_callable()`. | This is the right shape for our `tool` extension point: declarative prerequisites decide enablement, and the LLM only sees description + parameters. Page banner says "previous version"; exact version **UNVERIFIED**. | [custom toolsets](https://holmesgpt.dev/latest/data-sources/custom-toolsets/), [example YAML](https://github.com/robusta-dev/holmesgpt/blob/master/examples/custom_toolset.yaml), [Python SDK 0.36.0](https://holmesgpt.dev/0.36.0/reference/python-sdk/) |
| **FastAPI routers** | `APIRouter(prefix, tags, dependencies, responses)`; `app.include_router(router, prefix=..., dependencies=...)` can adapt a router from a package you cannot modify; nested `router.include_router`; routers are merged (not mounted) so they share one OpenAPI schema. | Good for mounting a plugin's HTTP surface; it is a routing primitive, not a plugin system. | [FastAPI bigger applications](https://fastapi.tiangolo.com/tutorial/bigger-applications/), checked 2026-10-27 |

Takeaway for Python: entry points + a Pydantic/JSON-schema manifest + pluggy hooks is a complete, boring, stdlib-first plugin story *inside one Python process*. It has no answer for isolation, per-tenant process limits, or non-Python plugins. It is the right mechanism **inside** a Python pillar runtime (e.g. how an investigator plugin loads its toolsets), not for the kernel.

### 2.3 TypeScript options

| Option | Mechanism | Notes | State checked |
|---|---|---|---|
| **Cordis (dsh kernel)** | "plugins contribute services, typed events, and reversible effects to a shared context"; "There is no privileged core to patch"; "registrations are effects that unwind when their plugin unloads". Services are keys on `ctx` (`ctx.tools`, `ctx.agents`, `ctx.llm`, ...); consumers declare `inject` and are not applied until the service exists (and are unloaded if it disappears); fiber states `PENDING, LOADING, ACTIVE, UNLOADING, DISPOSED, FAILED`; loader watches files for HMR. Profiles/bundles: `package.json` `dsh.profile` lists bundles, `dsh.bundle` points at a patch file; effective config = empty root + each bundle patch + profile `cordis.patch.yml` + `$DSH_HOME/cordis.patch.yml` + `--patch` overlays; "A patch targets a row by id and replaces its whole config, or inserts new rows"; `dsh --profile web --dump-config` prints the result. | The composition model is excellent and we copy it. The runtime is not a fit: the README says "Cordis is under active development. The API is not yet stable and may change without notice"; dsh docs are labelled "Preview"; it is a single-process agent-app kernel with no multi-tenant story; the third-party claim that Cordis became dsh's kernel "since August 2026" is **UNVERIFIED** from a primary source. Fiber-state and `inject` semantics above come from the npm README summary and third-party explainers, not from a page fetched directly in this research (**partially UNVERIFIED**). | [dsh architecture.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/architecture.md), [dsh reference](https://deepseek-harness.github.io/deepseek-harness/en/reference/), [cordis README](https://github.com/cordiverse/cordis/blob/main/packages/core/README.md), checked 2026-10-27; paper arXiv 2608.25512 |
| **NestJS modules** | `@Module({providers, controllers, imports, exports})`; providers encapsulated by default; `forRoot()` dynamic modules; `@Global()` discouraged; `Test.createTestingModule` for tests. | DI container, not a plugin system: no manifest, no runtime enable/disable, no isolation. | [docs.nestjs.com/modules](https://docs.nestjs.com/modules) (v11 docs) |
| **Backstage new backend system** | `createBackendPlugin({pluginId, register(env)})`; `env.registerExtensionPoint(ref, impl)` or factory form with `reportModuleStartupFailure` (1.48.0); modules depend on extension points exported from a `-node` package and "are completely initialized before our plugin gets initialized"; `coreServices.*` (auth, cache, database, discovery, httpAuth, httpRouter, lifecycle, logger, permissions, rootConfig, rootHealth, rootHttpRouter, rootLifecycle, rootLogger, scheduler, urlReader, userInfo, pluginMetadata, metrics/tracing alpha). Failure containment: `backend.startup.default.onPluginBootFailure: abort|continue` and per-plugin / per-module overrides (plugin level v1.36.0, module level backend-app-api 1.2.3). Test kit: `startTestBackend({features:[plugin(), mockServices.rootConfig.factory({data}), logger.factory]})` + supertest; `mockServices.<svc>.factory()` / `.mock()`; `ServiceFactoryTester`; `TestDatabases` via testcontainers; `registerMswTestHooks` strict mode; `createServiceMock` for custom service refs (backend-test-utils 1.11.0). | Closest OSS analogue to what we need: explicit extension points, service refs, optional-vs-required plugins in config, and a fake-kernel test harness. We copy the *shape* of extension points, core services, boot-failure policy and `startTestBackend` into Go. | Stable docs v1.56.0: [extension points](https://backstage.io/docs/backend-system/architecture/extension-points/), [core services](https://backstage.io/docs/backend-system/core-services/index/), [testing](https://backstage.io/docs/backend-system/building-plugins-and-modules/testing/), [building backends](https://backstage.io/docs/backend-system/building-backends/index/), [v1.48.0 changelog](https://backstage.io/docs/releases/v1.48.0-changelog/), [v1.36.0 changelog](https://backstage.io/docs/next/releases/v1.36.0-changelog/), [PR #29348](https://github.com/backstage/backstage/pull/29348) |

Could we just use Cordis/Node for the control plane? Technically yes: Node 24 is the current LTS line and Node 26 is Current (v26.12.1 released 2026-10-21, v24.22.1 2026-10-20, [nodejs.org](https://nodejs.org/en/about/previous-releases); Active vs Maintenance split not stated on that page, **UNVERIFIED**). But it would add a third language to a Go data plane plus Python pillars, bind the kernel to an API its authors call unstable, and give us nothing for the two hardest requirements (process isolation per plugin, per-tenant resource limits) that gRPC sidecars do not already give. Backstage proves a TypeScript kernel can run a multi-tenant-ish platform, but Backstage's own operational model (one Node process, Postgres, plugins all in-process) is what the user wants to leave behind.

### 2.4 Cross-cutting mechanisms

| Concern | Best prior art | What we take |
|---|---|---|
| Manifest + config schema | Airflow `get_provider_info()` validated by `provider_info.schema.json`; Alloy zero-value `Args` struct; Extism manifest JSON schema at `extism/manifest/schema.json`; Grafana `plugin.json` + `MANIFEST.txt` | A `plugin.yaml` manifest (§4.1) and a JSON Schema for the plugin's config block, both validated by the kernel before `Provision`. |
| Extension points vs services | Backstage: extension points are owned by the plugin that exposes them, exported from a `-node` lib, "multiple extension points with few methods"; Cordis: services are `ctx` keys, consumers `inject` them | Extension points are Go interfaces in `sdk/ext/<point>`; host services in `sdk/host`. Plugins declare `provides` and `requires` (required/optional) in the manifest. |
| Lifecycle | OTel `Start/Shutdown` (Start error aborts startup; Shutdown safe without Start); Caddy `Provision/Validate/Cleanup`; Alloy `Run/Update` (Update concurrent with Run); Cordis effects auto-unwind | `Validate → Provision → Start → Update* → Shutdown`, plus `Health`. The kernel tracks every registration a plugin makes through the host (routes, scheduled jobs, event subscriptions) and unwinds them on Shutdown, Cordis-style, so plugins do not write manual teardown for kernel-owned effects. |
| Dependency declaration, version range | ocb strict versioning (component `gomod` must match `otelcol_version` major/minor); go-plugin `VersionedPlugins`; Terraform `terraform-registry-manifest.json` `protocol_versions`; Node/Backstage semver | Manifest `kernelApi: ">=1.2 <2"` (semver range) and `protocol: 1` for gRPC plugins; kernel refuses to load a mismatch with a clear error. |
| Isolation / failure containment | Backstage `onPluginBootFailure: abort|continue` per plugin and per module; go-plugin "A panic in a plugin doesn't panic the plugin user"; Dapr separate process/container | Manifest `required: true|false` per profile row; in-process Go plugins recover panics at the kernel boundary and mark the plugin `Failed`; out-of-process plugins are supervised, restarted with backoff, and their extension points degrade to "unavailable" rather than failing the tenant. |
| Hot reload | Alloy `Update`; Caddy config reload with overlapping instances; Cordis HMR; Extism re-instantiate | Config hot reload via `Update` for all plugins. Code hot reload only for gRPC (restart the sidecar) and Wasm plugins; in-process Go plugins require a rebuild (as in Alloy/Caddy). |
| Profile composition | dsh: ordered bundle patches → profile patch → home patch → `--patch` overlays, "later layers win per row", row replaced whole not deep-merged, `--dump-config` | Identical semantics in `aiopsctl profile render` (§4.4), with one extra layer: per-tenant overlay. |
| Distribution | ORAS (CNCF Sandbox per its own footer, docs v1.3): any OCI-distribution registry stores arbitrary artifacts keyed by `config.mediaType`; OCI image-spec artifacts guidance says non-image content "MAY be packaged using the image manifest" with a non-image `config.mediaType`; Terraform registry: zip per platform + `SHA256SUMS` + detached GPG `.sig`, "Terraform will verify this during `terraform init`" | Plugins are OCI artifacts (`application/vnd.aiops.plugin.v1`) pushed with ORAS: Go plugin = source module ref for `aiopsctl build`, gRPC plugin = container image ref, Wasm plugin = `.wasm` blob. |
| Signing | cosign keyless: Fulcio issues short-lived certs bound to an OIDC identity, Rekor logs it, TUF distributes roots; Grafana `MANIFEST.txt` PGP; Terraform GPG | cosign-sign every published artifact; kernel verifies `--certificate-identity`/issuer policy (keyless) or a configured public key; `allow_unsigned_plugins: [ids]` escape hatch like Grafana's. |
| Sandboxing | Extism `allowed_hosts`/`allowed_paths`/memory pages; gVisor (Sentry in Go, `runsc` OCI runtime, "higher per-system call overhead") | gRPC sidecars run as normal containers; in the hosted tier optionally under gVisor `RuntimeClass` (platform choice, not a plugin-SDK concern; platform list ptrace/KVM/systrap **UNVERIFIED** here). Wasm phase for untrusted tiny plugins. |
| Conformance test kits | Dapr `tests.yml` + `go test -tags=conftests -run TestStateConformance/<comp>`; OTel `componenttest.NewNopHost()`, `NewNopTelemetrySettings()`, `CheckConfigStruct`, `componenttest.Telemetry` reader for asserting emitted metrics; Terraform `TF_ACC=1 go test` runs a real `terraform` binary against real infra, guarded so plain `go test` cannot spend money; CNI: plugin is an executable, env vars + JSON on stdin/stdout, `VERSION` op reports `supportedVersions`, numbered error codes; Backstage `startTestBackend` + `mockServices` + msw strict mode | §4.5. |

### 2.5 Multi-tenant concerns

| Concern | Evidence | Decision |
|---|---|---|
| Tenant identity | Mimir: tenant from `X-Scope-OrgID`; Mimir "trusts whatever value arrives in the header", a reverse proxy must authenticate; federation `tenant-1\|tenant-2`; IDs ≤150 bytes, charset `0-9 a-z A-Z ! - _ . * ' ( )`, `.`/`..`/`__mimir_cluster` forbidden ([auth](https://grafana.com/docs/mimir/latest/manage/secure/authentication-and-authorization/), [tenant IDs](https://grafana.com/docs/mimir/latest/configure/about-tenant-ids/)). Loki/Tempo/Pyroscope use the same header (**UNVERIFIED** in this pass, well known). | Our tenant ID is the LGTM tenant ID, restricted further to `[a-z0-9][a-z0-9-]{0,62}` so it is also a valid DNS label and Kubernetes namespace. The kernel is the authenticating proxy that injects the header. |
| Grafana orgs | Orgs isolate folders, dashboards, data sources, alerts, notification channels, service accounts, teams; users can belong to several; "cannot be easily shared with another organization"; deleting an org deletes its teams and dashboards ([org management](https://grafana.com/docs/grafana/latest/administration/organization-management/)). | One Grafana org per tenant in the hosted tier; the `grafana` datasource plugin stores `{tenant → org_id, service_account_token}`. Self-hosters with one org map it to the single `default` tenant. |
| Per-tenant plugin enablement/config | dsh rows-by-id patch layers; Backstage per-plugin startup policy | Tenant overlay is the last profile layer: `tenants/<id>/aiops.patch.yml` may enable/disable rows and override `config` for rows whose manifest says `tenantScope: per-tenant`. Rows with `tenantScope: global` cannot be patched per tenant. |
| LLM budgets and credentials | LiteLLM proxy: `max_budget` + `budget_duration` per key/user/team/customer, `tpm_limit`/`rpm_limit`, per-model limits, model access groups, budget reservation for concurrent requests, fail-closed options, Postgres required ([LiteLLM docs](https://docs.litellm.ai/docs/proxy/users)). | Plugins never hold LLM credentials. The kernel exposes `host.LLM` which routes through a gateway keyed by `(tenant, plugin)`; budgets are tenant rows. First implementation can be an embedded LiteLLM (team = tenant, key = tenant+plugin) or an in-kernel equivalent; either way the plugin contract is the same. Datasource credentials are stored by `host.Secrets` scoped `(tenant, plugin, name)`; a plugin cannot read another plugin's secrets. |
| Isolation of blast radius | Dapr/go-plugin process model; Backstage continue-on-failure | gRPC plugins may run one process per tenant (`instancing: per-tenant`) or shared with tenant passed in metadata (`instancing: shared`), declared in the manifest. Default shared; per-tenant for remediation executors and anything holding tenant credentials in memory. |

## 3. Decision matrix

Scores 1 (poor) to 5 (excellent). Weights reflect this product: hosted multi-tenant + self-hosted, Python LLM pillars, many contributors.

| Criterion (weight) | Go compile-time registry | Go gRPC sidecar (Dapr-style) | hashicorp/go-plugin | Wasm (Extism/wazero) | Go native `plugin` | K8s operator/CRD | Python pluggy + entry points | TS Cordis (dsh) | TS NestJS | TS Backstage backend |
|---|---|---|---|---|---|---|---|---|---|---|
| Failure containment (5) | 2 | 5 | 5 | 4 | 1 | 5 | 2 | 2 | 2 | 3 |
| Test in isolation with fake kernel (5) | 5 | 4 | 4 | 4 | 2 | 3 | 5 | 4 | 4 | 5 |
| Cross-language, Python pillars (5) | 1 | 5 | 4 | 3 | 1 | 5 | n/a (is Python) 3 | 1 | 1 | 1 |
| Self-host simplicity, one binary/compose (4) | 5 | 3 | 3 | 4 | 2 | 1 | 3 | 3 | 3 | 2 |
| Ingest/correlation performance (3) | 5 | 3 | 3 | 3 | 5 | 2 | 2 | 2 | 2 | 2 |
| Ecosystem fit with Grafana/OTel/Alloy (4) | 5 | 4 | 4 | 2 | 1 | 4 | 2 | 1 | 1 | 1 |
| Contributor barrier (4) | 4 | 4 | 3 | 3 | 1 | 2 | 5 | 3 | 3 | 3 |
| Distribution + signing maturity (3) | 4 | 5 | 4 | 4 | 1 | 5 | 3 | 2 | 2 | 3 |
| Hot reload (2) | 2 | 4 | 3 | 5 | 1 | 5 | 2 | 5 | 2 | 2 |
| Mechanism maturity/stability in 2026 (4) | 5 | 5 | 5 | 3 | 2 | 5 | 5 | 1 | 4 | 4 |
| Per-tenant config/limits fit (4) | 3 | 5 | 4 | 4 | 1 | 4 | 2 | 2 | 2 | 3 |
| **Weighted total (max 215)** | **159** | **186** | **168** | **150** | **68** | **160** | **137** | **97** | **103** | **116** |

Reading the matrix: no single mechanism wins everything. The compile-time registry wins self-host simplicity, performance and ecosystem fit; the gRPC sidecar wins isolation, cross-language and tenancy. They are complementary, which is exactly why Alloy, OTel Collector, Grafana and Dapr each ship *both* an in-tree registry and a process boundary. The recommendation is therefore the union, not a pick.

## 4. Recommendation and SDK contract sketch

### 4.1 Language and mechanism

| Layer | Language | Plugin mechanism |
|---|---|---|
| Agent side (customer hosts) | Go: Grafana Alloy unchanged; our contribution is Alloy config bundles, not Alloy plugins | Alloy's own registry |
| Kernel (control plane) | Go | Compile-time registry: `ext.Register(Registration{...})` from `init()`; `aiopsctl build --manifest dist.yaml` (ocb-style) produces a distribution binary; the default `aiops` binary ships with the base bundle compiled in |
| Data-plane services (ingest, correlator, scheduler, storage, notifier) | Go, in-process plugins | Same registry |
| LLM pillars (investigator/agent, tool, report-renderer when LLM-backed) | Python 3.11+ (HolmesGPT, krr and contributed tools) | Out-of-process gRPC plugins: kernel-side `remote.<point>` adapter is itself a registered Go plugin that proxies the extension-point interface to a gRPC service over UDS (local) or mTLS TCP (Kubernetes); Python SDK package `aiops-plugin-sdk` wraps the proto |
| Untrusted or tiny contributions (approval-policy expressions, report templates, simple tools) | Any Extism PDK language, phase 2 | `wasm.<point>` adapter on Extism/wazero with `allowed_hosts`/memory limits from the manifest |
| Deployment on Kubernetes | Go operator (optional) | CRDs `Tenant`, `PluginRelease`, `ProfileBinding` reconcile into the same YAML layers; not required for compose self-hosters |

Why not go-plugin directly: its subprocess launch + stdout handshake assumes the host launches the plugin binary; our Python pillars run as containers that the orchestrator starts. We borrow its ideas (protocol version negotiation, checksum, AutoMTLS-equivalent via mTLS) over a plain gRPC service definition, which is what Dapr does.

### 4.2 Plugin manifest (`plugin.yaml`)

```yaml
apiVersion: aiops.dev/v1
kind: Plugin
id: grafana-labs/loki-datasource          # <owner>/<name>, lowercase, unique in registry
version: 1.4.0                            # semver of this plugin
kernelApi: ">=1.0 <2.0"                   # semver range of sdk/ext it was built against
runtime: inproc | grpc | wasm
protocol: 1                               # gRPC/Wasm ABI version; omitted for inproc
stability: experimental | preview | ga    # kernel refuses < minStability (Alloy-style)
provides:                                 # extension points implemented
  - point: datasource
    name: loki                            # row type becomes datasource.loki
requires:                                 # host services and other points
  - service: secrets
  - service: tenant
  - point: notifier            # optional dependency
    optional: true
tenantScope: per-tenant | global
instancing: shared | per-tenant           # grpc/wasm only
capabilities:                             # enforced by kernel/sandbox
  network: ["loki.svc:3100", "*.grafana.net"]
  secrets: ["loki/*"]
  filesystem: []
configSchema: ./config.schema.json        # JSON Schema (draft 2020-12) for the row's config
artifact:                                 # how aiopsctl fetches it
  oci: ghcr.io/grafana-labs/aiops-loki-datasource:1.4.0
  signature: cosign-keyless               # identity policy configured on the kernel
```

Field provenance: `provides/requires` from Backstage extension points and Cordis `inject`; `stability` gate from Alloy; `kernelApi` range from ocb strict versioning and Terraform `protocol_versions`; `capabilities` from Extism `allowed_hosts`; `tenantScope/instancing` are ours; `configSchema` from Airflow provider-info JSON schema and Grafana `plugin.json`.

### 4.3 Lifecycle hooks (Go interface, mirrored 1:1 in the gRPC proto and Python SDK)

```go
type Plugin interface {
    Validate(cfg Config) error                       // read-only, no I/O (Caddy Validator)
    Provision(ctx context.Context, host Host, cfg Config) error  // cheap: wire deps, no network (Caddy Provisioner)
    Start(ctx context.Context) error                 // error aborts this row only, not the kernel (OTel Start, Backstage continue)
    Update(ctx context.Context, cfg Config) error    // hot config; may be called while running (Alloy Update)
    Health(ctx context.Context) Health               // {Status: ok|degraded|failed, Message}
    Shutdown(ctx context.Context) error              // safe without Start; idempotent (OTel Shutdown)
}
```

Kernel guarantees, copied from the projects surveyed: `Start` is called exactly once per instance; `Update` may run concurrently with the plugin's own goroutines; the kernel unwinds every registration the plugin made through `Host` (HTTP routes, scheduled jobs, event subscriptions, extension-point registrations) in reverse order after `Shutdown` returns, so plugin authors only clean up resources the kernel did not hand them (Cordis "effects"). A panic in an in-process plugin is recovered at the kernel boundary and turns the row `Failed`; the profile row's `required: true|false` decides whether the kernel aborts (Backstage `onPluginBootFailure`).

### 4.4 Host services (what a plugin may `require`)

`logger`, `metrics` (Prometheus registerer pre-labelled with plugin id and tenant, as Alloy does), `tracer`, `config`, `secrets` (tenant+plugin scoped), `tenant` (current tenant from request/job context), `store.kv`, `store.sql` (Postgres via migrations owned by the plugin, like Backstage `database`), `scheduler` (distributed cron, tenant-aware), `events` (typed bus with `session/*`, `signal/*`, `incident/*`, `remediation/*` topics; waterfall vs parallel semantics taken from dsh event domains), `http.router` (plugin-prefixed, like Backstage `httpRouter`), `llm` (gateway with per-tenant budget), `discovery` (find another plugin's endpoint), `lifecycle` (register shutdown hooks). This is Backstage `coreServices` minus Backstage-specific auth, plus `tenant`, `events` and `llm`.

### 4.5 Extension points

Each is a small Go interface in `sdk/ext/<point>` with a matching proto service in `proto/aiops/ext/<point>/v1`. Rule from Backstage: "multiple extension points with few methods". Row type in profiles is `<point>.<name>`.

| Point | Interface sketch (Go) | Typical in-tree impls | Runtime |
|---|---|---|---|
| `datasource` | `Query(ctx, tenant, QueryRequest) (Frames, error)`; `Capabilities() []string` | `loki`, `mimir`, `tempo`, `pyroscope`, `grafana` (dashboards/alerts/orgs), `kubernetes`, `aws`, `datadog` (optional, for customers not on LGTM) | inproc |
| `signal-ingest` | `Serve(ctx, host) error` registering an HTTP/gRPC endpoint; emits `signal/*` events | `alertmanager-webhook`, `grafana-alerting`, `otlp-events`, `cloudevents`, `pagerduty-inbound` | inproc |
| `correlator` | `Correlate(ctx, tenant, []Signal) ([]Candidate, error)` | `label-overlap`, `topology-graph`, `time-window`, `llm-rerank` (gRPC) | inproc, grpc |
| `investigator` (agent) | `Investigate(ctx, tenant, Incident, ToolSet) (stream Finding, error)`; `Pillar() string` | `incident`, `change`, `compliance`, `secops`, `finops`, `continuous-improvement`, `exec-report` — all Python | grpc |
| `tool` | `Describe() ToolSpec` (name, description, JSON-schema params, prerequisites); `Invoke(ctx, tenant, args) (Result, error)` | HolmesGPT-style YAML/bash toolsets, `kubectl`, `logql`, `promql`, `krr`, MCP bridge (MCP spec 2026-07-28 tools/resources/prompts, [spec](https://modelcontextprotocol.io/specification/latest)) | inproc, grpc, wasm |
| `remediation-executor` | `Plan(ctx, tenant, Action) (Plan, error)`; `Apply(ctx, tenant, Plan) (Receipt, error)`; `Rollback(...)`; must be idempotent (CNI DEL rule) | `kubernetes-apply`, `argo-rollouts`, `aws-ssm`, `ansible`, `runbook-http` | grpc (per-tenant instancing) |
| `approval-policy` | `Evaluate(ctx, tenant, Action, Context) Decision{Allow|Deny|RequireApproval(approvers)}` | `always-ask`, `rego`, `cel`, `business-hours` | inproc, wasm |
| `notifier` | `Send(ctx, tenant, Notification) (DeliveryID, error)`; `Capabilities()` (threads, buttons, ack) | `slack`, `teams`, `email`, `webhook`, `sms-twilio`, `push-ntfy` | inproc |
| `chat-surface` | `Serve(ctx, host)` registering inbound events; `Reply(ctx, tenant, Thread, Message)`; maps chat identity to tenant+user | `slack-socket-mode`, `slack-events`, `teams-bot`; routes `@pillar` questions to the right investigator | inproc |
| `scheduler` | `Schedule(ctx, tenant, JobSpec) (JobID, error)`; `Cancel`; leader-elected | `postgres-cron` (default), `kubernetes-cronjob`, `temporal` (optional) | inproc |
| `storage` | `KV`, `SQL` (migrations), `Blob` interfaces | `postgres`, `sqlite` (single-node self-host), `s3-blob`, `minio` | inproc |
| `report-renderer` | `Render(ctx, tenant, ReportSpec, Data) (Artifact, error)` | `markdown`, `html`, `pdf` (gRPC, headless chromium), `grafana-dashboard-snapshot` | inproc, grpc, wasm |
| `on-call-provider` | `Schedules(ctx, tenant)`, `WhoIsOnCall(ctx, tenant, schedule, at)`, `Page(ctx, tenant, Escalation, Incident)`, `Ack/Resolve`, `Overrides` | `builtin` (our module: schedules, rotations, escalation chains, Slack/SMS/push via `notifier`), `pagerduty`, `opsgenie`, `grafana-cloud-irm` | inproc, grpc |

### 4.6 Profiles

Semantics copied from dsh and applied by `aiopsctl profile render`:

1. Start from an empty row list.
2. Apply each bundle patch listed in the profile's `profile.yaml` `bundles:` in order (`aiops-base`, then e.g. `aiops-lgtm`, `aiops-slack`, `aiops-oncall-builtin`, `aiops-pillars-python`).
3. Apply the profile's own `aiops.patch.yml`.
4. Apply the site-level `$AIOPS_HOME/aiops.patch.yml`.
5. Apply `--patch <file>` overlays in argv order.
6. Apply the tenant overlay `tenants/<id>/aiops.patch.yml` (only rows with `tenantScope: per-tenant`).

Each layer is a list of rows `{id, type: <point>.<name>, required: bool, config: {...}}`. A patch targets a row by `id` and **replaces its whole `config`** (no deep merge), or inserts a new row, or sets `enabled: false`. `aiopsctl profile render --tenant acme --dump` prints the effective tree, mirroring `dsh --dump-config`. Shipped profile templates: `single-node` (sqlite, all in one binary, no Python pillars), `lgtm-self-host` (compose with LGTM + pillars), `hosted-multi-tenant` (Kubernetes, operator, gVisor optional), `aiops-only` (no LGTM, external datasources such as Datadog/Dynatrace via optional datasource plugins). Row `config` is validated against the plugin's `configSchema` at render time, before anything starts.

### 4.7 Test kit: run a plugin alone against a fake kernel with recorded fixtures

Package `sdk/plugintest` (Go) and `aiops_plugin_sdk.testing` (Python), both talking to the same fixtures directory:

| Piece | Prior art | What it does |
|---|---|---|
| `plugintest.NewFakeHost(opts)` | OTel `componenttest.NewNopHost`, Backstage `mockServices.*.factory()` / `.mock()` | In-memory implementations of every host service: recording logger, Prometheus registry you can read back (like `componenttest.Telemetry.GetMetric`), in-memory KV/SQL (sqlite), fake scheduler with manual clock, fake events bus, fake secrets, fake `llm` returning scripted responses, fake `tenant`. Every fake records calls so tests can assert interactions. |
| `plugintest.CheckManifest(path)` / `CheckConfigStruct` | OTel `componenttest.CheckConfigStruct`, Airflow `provider_info.schema.json` | Validates `plugin.yaml` against the SDK schema, the config schema against draft 2020-12, and that `provides` points exist for the declared `kernelApi` range. |
| `plugintest.Run(t, plugin, cfg, fixtures)` | Backstage `startTestBackend({features:[...]})` | Drives `Validate → Provision → Start → Update → Shutdown` with the fake host, asserts Shutdown unwinds all registrations, asserts `Shutdown` before `Start` is safe, asserts panics are contained. |
| Conformance suites per extension point | Dapr `tests.yml` + `go test -tags=conformance -run TestDatasourceConformance/<name>`; CNI `VERSION` op | `conformance/<point>/` contains the black-box tests every implementation must pass (e.g. `datasource`: query with tenant isolation, empty result, timeout, cancellation; `notifier`: idempotent resend, thread reply; `remediation-executor`: Plan is side-effect free, Apply twice is a no-op, Rollback after partial Apply). The same suite runs over an in-process plugin, over the gRPC adapter against a sidecar, and over the Wasm adapter, so the three runtimes are proven equivalent. Optional operations declared in the plugin's `conformance.yml` as in Dapr. |
| Recorded fixtures | Backstage `registerMswTestHooks` strict mode; Terraform `TF_ACC` guard | `fixtures/<point>/<name>/*.yaml` hold recorded Loki/Mimir/Tempo responses, Alertmanager webhooks, Slack events and LLM transcripts. Default mode is replay with strict mode: any unmatched outbound HTTP fails the test, so tests cannot leak traffic. `AIOPS_ACC=1` switches to record/acceptance mode against real backends (testcontainers for LGTM, Backstage `TestDatabases`-style matrix for Postgres/SQLite). |
| gRPC contract tests | go-plugin `VersionedPlugins`; Dapr conformance over UDS | `plugintest.ServeGRPC(plugin)` in Python starts the plugin on a UDS; the Go suite connects through the real `remote.<point>` adapter. CI runs this for every Python pillar. |
| `aiopsctl plugin check ./` | HolmesGPT `holmes toolset refresh` prerequisites, Grafana plugin validator | One command for contributors: manifest + schema + conformance + signature check, no kernel checkout needed (the SDK module is the only dependency). |

### 4.8 What is deliberately not built

- No Go `plugin` package, for the reasons the Go docs give (toolchain lockstep, no unload, race detector).
- No Cordis/Node kernel; dsh composition semantics are copied in YAML, which is all that was valuable for a server.
- No wasmCloud; too much platform for a self-hoster, and WASI P2 tooling in Go is not there (wazero is preview1 only as of its spec page).
- No per-plugin Kubernetes operator as the *call path*; CRDs are an optional deployment front-end that renders into the same profile layers.
- No deep-merge in profile patches; whole-row replace is simpler to reason about and is what dsh chose.

## 5. Open questions

1. Does the gRPC adapter need streaming for `investigator` findings and `signal-ingest`, or is server-streaming enough? (Design: server-streaming only, revisit if bidirectional chat needs it.)
2. Should the hosted tier run Python pillars one-process-per-tenant (`instancing: per-tenant`) by default? Cost vs. isolation; LiteLLM-style budget reservation may make shared processes acceptable for read-only pillars.
3. LLM gateway: embed LiteLLM (Postgres dependency, large) or implement budgets in-kernel against our own Postgres? Contract is identical either way.
4. Tenant ID: do we enforce the stricter DNS-label subset from day one (breaks importing existing Mimir tenants with `.` or `*` in the name)?
5. Signing policy for self-hosters: keyless cosign requires outbound access to Fulcio/Rekor at verify time unless bundles are embedded; need the offline story.
6. Grafana orgs vs Grafana stacks: in Grafana Cloud each tenant may be a separate stack rather than an org; the `grafana` datasource plugin must support both mappings.

## 6. Sources

Primary sources fetched 2026-10-27 unless noted.

- DeepSeek Harness architecture: https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/architecture.md ; reference: https://deepseek-harness.github.io/deepseek-harness/en/reference/ ; CLI reference: https://github.com/deepseek-ai/deepseek-harness/blob/master/apps/cli/reference/README.md
- Cordis README (cordiverse/cordis, main): https://github.com/cordiverse/cordis/blob/main/packages/core/README.md ; npm: https://www.npmjs.com/package/@deepseek-ai/cordis (403 during research; API details from https://www.npmjs.com/package/cordis/v/3.7.0 summary and https://dev.to/worldlinetech/understanding-cordis-the-typescript-framework-built-for-hot-swapping-everything-1ihb)
- Caddy: https://caddyserver.com/docs/extending-caddy
- OTel Collector Builder: https://github.com/open-telemetry/opentelemetry-collector/blob/main/cmd/builder/README.md ; component v1.70.0: https://pkg.go.dev/go.opentelemetry.io/collector/component ; componenttest v0.164.0: https://pkg.go.dev/go.opentelemetry.io/collector/component/componenttest
- Grafana Alloy: https://github.com/grafana/alloy/blob/main/internal/component/registry.go ; https://github.com/grafana/alloy/blob/main/internal/component/component.go
- Prometheus SD registry: https://github.com/prometheus/prometheus/blob/main/discovery/registry.go
- hashicorp/go-plugin v1.8.0: https://pkg.go.dev/github.com/hashicorp/go-plugin ; https://github.com/hashicorp/go-plugin/blob/main/README.md ; releases: https://github.com/hashicorp/go-plugin/releases
- Go `plugin` package (Go 1.27.3): https://pkg.go.dev/plugin
- Grafana backend plugins: https://grafana.com/developers/plugin-tools/key-concepts/backend-plugins/ ; signing: https://grafana.com/developers/plugin-tools/publish-a-plugin/sign-a-plugin ; plugin management: https://grafana.com/docs/grafana/latest/administration/plugin-management/ ; unsigned plugins issue: https://github.com/grafana/grafana/issues/31125
- Dapr v1.19: https://docs.dapr.io/developing-applications/develop-components/pluggable-components/pluggable-components-overview/ ; SDKs: https://docs.dapr.io/developing-applications/develop-components/pluggable-components/pluggable-components-sdks/ ; conformance: https://github.com/dapr/components-contrib/blob/main/tests/conformance/README.md
- Extism: https://github.com/extism/go-sdk/blob/main/README.md ; https://extism.org/docs/concepts/manifest ; https://extism.org/docs/concepts/pdk
- wazero: https://wazero.io/ ; https://wazero.io/specs/
- wasmCloud v2 (updated 2026-09-23): https://wasmcloud.com/docs/concepts/components
- Proxy-Wasm spec v0.2.1: https://github.com/proxy-wasm/spec/blob/main/README.md
- Kubernetes operator pattern (modified 2025-07-16): https://kubernetes.io/docs/concepts/extend-kubernetes/operator/
- pluggy 1.7.0 (2026-10-14): https://pluggy.readthedocs.io/en/stable/
- Entry points spec (updated Jan 2026): https://packaging.python.org/en/latest/specifications/entry-points/ ; importlib.metadata (Python 3.15.2 docs, 2026-10-26): https://docs.python.org/3/library/importlib.metadata.html
- Airflow providers (apache-airflow 3.4.0 docs): https://airflow.apache.org/docs/apache-airflow-providers/howto/create-custom-providers.html
- Dagster changelog: https://docs.dagster.io/about/changelog ; dg/Components discussion: https://github.com/dagster-io/dagster/discussions/28472
- HolmesGPT: https://holmesgpt.dev/latest/data-sources/custom-toolsets/ ; https://github.com/robusta-dev/holmesgpt/blob/master/examples/custom_toolset.yaml ; https://holmesgpt.dev/0.36.0/reference/python-sdk/
- FastAPI: https://fastapi.tiangolo.com/tutorial/bigger-applications/
- NestJS: https://docs.nestjs.com/modules
- Backstage v1.56.0 docs: https://backstage.io/docs/backend-system/architecture/extension-points/ ; https://backstage.io/docs/backend-system/core-services/index/ ; https://backstage.io/docs/backend-system/building-plugins-and-modules/testing/ ; https://backstage.io/docs/backend-system/building-backends/index/ ; https://backstage.io/docs/releases/v1.48.0-changelog/ ; https://backstage.io/docs/next/releases/v1.36.0-changelog/ ; https://github.com/backstage/backstage/pull/29348
- ORAS docs v1.3: https://oras.land/docs/ ; OCI artifacts guidance: https://github.com/opencontainers/image-spec/blob/main/artifacts-guidance.md
- cosign: https://docs.sigstore.dev/cosign/signing/overview/
- gVisor: https://gvisor.dev/docs/
- terraform-plugin-testing v1.15.x: https://developer.hashicorp.com/terraform/plugin/testing/acceptance-tests ; registry publishing: https://developer.hashicorp.com/terraform/registry/providers/publishing
- CNI spec 1.1.0: https://github.com/containernetworking/cni/blob/main/SPEC.md
- Mimir: https://grafana.com/docs/mimir/latest/manage/secure/authentication-and-authorization/ ; https://grafana.com/docs/mimir/latest/configure/about-tenant-ids/
- Grafana organizations: https://grafana.com/docs/grafana/latest/administration/organization-management/
- Grafana OnCall OSS status: https://grafana.com/blog/grafana-oncall-maintenance-mode/ ; https://grafana.com/docs/grafana-cloud/alerting-and-irm/irm/set-up/migrate/oncall-oss/ ; archived repo https://github.com/grafana-cold-storage/oncall
- LiteLLM budgets: https://docs.litellm.ai/docs/proxy/users
- MCP specification 2026-07-28: https://modelcontextprotocol.io/specification/latest
- Node.js releases: https://nodejs.org/en/about/previous-releases

---

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
