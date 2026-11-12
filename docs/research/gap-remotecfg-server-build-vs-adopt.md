# Gap research: the Alloy remote-config wire contract, the three community servers, and build-vs-adopt for the `fleet` plugin

Date of research: 2026-10-27. Alloy version analysed: v1.21.2 (released 2026-10-22), which pins `github.com/grafana/alloy-remote-config v0.0.12` in its `go.mod`. All code facts below were read from the tagged sources, not from docs, unless stated otherwise.

Prior reports this builds on: `grafana-stack.md` §3.5 (protocol is open, Grafana server is Cloud-only, community servers UNVERIFIED) and `datadog-agent.md` §2.12/§7.4 (Fleet Automation = inventory, config history, remote upgrade, remote flare; "the single biggest piece to own"). Nothing from those sections is repeated except where needed for a decision.

## 0. Answer in one page

1. **The wire contract is tiny and fully knowable.** `collector.v1.CollectorService` has three unary Connect-RPCs: `GetConfig`, `RegisterCollector`, `UnregisterCollector`. The server always returns the **full** configuration (no deltas). The server-chosen `hash` is an opaque string the agent echoes back; the server answers `not_modified = true` when it matches. The agent never trusts the server hash for local decisions; it hashes the content itself (FNV-1a 32-bit) to decide whether to reload.
2. **Every error keeps the last-good config.** There is no error-code taxonomy in the client. Any transport or RPC error on `GetConfig` sets status `FAILED`, bumps a failure counter and leaves the running config untouched; at startup and on local config reload it additionally falls back to the on-disk cache. A config that fails to parse/load is also reverted to the cached last-good config. The one dangerous case is `RegisterCollector` failing at startup: the remotecfg service exits (the Alloy process keeps running on the cached config) and **stops polling until Alloy is restarted**. The server's `RegisterCollector` therefore has to be as available as the agent fleet needs it to be.
3. **The three community servers are all single-maintainer projects.** `procoduck/shepherd` (Apache-2.0, Go, v0.20.0 on 2026-10-27, 1 star, 1 human contributor) is by far the most complete: real multi-tenancy, OIDC, validation gates, GitOps, revisions, a health beacon; it is Kubernetes-only, needs PostgreSQL 16+, is pre-1.0 and ships breaking changes. `danielfree19/alloy-fleet-manager` (Apache-2.0, TypeScript/Fastify + Postgres, v0.1.1, last commit 2026-04-25, single-tenant) has no later activity in the public GitHub snapshot reviewed here; this note relies only on that public snapshot. `opsplane-services/alloy-remote-config-server` (MIT, Go, last push 2026-02-18, Go-template per `template` attribute, Redis) has no auth, no hash/`not_modified`, no status handling. **None** implements remote upgrade, remote support bundle, or OpAMP.
4. **Build, do not adopt.** The agent-facing surface is three RPCs (a few hundred lines with `connect-go`); the hard parts are the data model, tenancy, auth and validation, which have to live in our kernel's tenant database anyway. Adopting shepherd would mean adopting a second tenancy/auth/UI product beside our kernel. Borrow its designs (declare-wrapped merge, Alertmanager matchers, `sha256(content)` hash, three validation gates, version-aware validation keyed on `collector.version`, beacon pipeline, DB-outage serve cache) and use it as the conformance oracle in tests.
5. **Datadog Fleet Automation parity needs one extra thing Alloy cannot give us: a host helper.** Inventory, health, config history and rollback all fit inside the remotecfg protocol plus a server-injected self-monitoring pipeline. Remote upgrade and remote support bundle do not: Alloy has no self-updater (grafana/alloy#3122 is an open proposal since 2025-03-27) and `/-/support` is a local-only HTTP endpoint. A small optional `fleet-hostagent` binary (the analogue of Datadog's `installer`) is the honest answer for VMs; on Kubernetes the operator/Helm path covers upgrades and the API server covers bundles.
6. **Speak OpAMP too, as a second listener on the same plugin, but in phase 2.** Alloy's OTel engine can only be managed over OpAMP (`remotecfg` is unsupported in engine-extension mode; `alloy otel-supervisor` is EXPERIMENTAL and in manual mode connects to any OpAMP server). Grafana Cloud made OTel-collector remote management GA on 2026-07-08. `opamp-go` (Apache-2.0, active) gives us the server side. Alloy's `RemoteConfigStatus`/`EffectiveConfig` messages are copies of OpAMP's, so one internal model serves both. Do **not** plan on OpAMP packages for upgrades yet: the upstream supervisor accepts `accepts_packages` in config but it is "currently disabled at runtime".

## 1. The wire contract as consumed by Alloy 1.21.2

### 1.1 Transport

| Item | Fact | Source |
|---|---|---|
| Protocol | Connect-RPC (connectrpc.com), unary only. The client is built with `collectorv1connect.NewCollectorServiceClient(httpClient, args.URL, connect.WithInterceptors(userAgent))` with **no** `connect.WithGRPC()`/`WithGRPCWeb()`, so it speaks the Connect protocol, not gRPC framing. A plain `grpc-go` server will not work; a `connect-go` handler (or anything implementing Connect unary over HTTP/1.1 or h2c) will. | `internal/service/remotecfg/api_client.go` (v1.21.2) |
| Paths | `POST <url>/collector.v1.CollectorService/GetConfig`, `/RegisterCollector`, `/UnregisterCollector`. Default body `Content-Type: application/proto`; connect-go handlers also accept `application/json`. | generated `collectorv1connect/collector.connect.go` in alloy-remote-config; confirmed by alloy-fleet-manager `docs/remotecfg.md` and shepherd `docs/spec.md` §4.1 |
| HTTP client | `prometheus/common` HTTP client config: one of `authorization`, `basic_auth`, `bearer_token(_file)`, `oauth2`; `tls_config`, proxy settings, `http_headers`, `enable_http2` default true, `follow_redirects` default true. `User-Agent` set by an interceptor. | `api_client.go`, `arguments.go`; [remotecfg docs](https://grafana.com/docs/alloy/latest/reference/config-blocks/remotecfg/) |
| Poll | `poll_frequency` default `1m`, minimum `10s`, plus a constant 100 ms jitter. | `arguments.go`, `config_manager.go` |
| Identity | `id` defaults to the UUID in `alloy_seed.json`; `name` is free text; `attributes` is a user map; keys starting with `collector.` are rejected at validation and `collector.version` / `collector.os` are injected by the service. | `arguments.go`, `remotecfg.go` `getSystemAttributes()` |
| Cache | Last successfully loaded remote config is written atomically to `<storage.path>/remotecfg/<hash-of-arguments>` (atomic write since v1.20.0). The file name changes when `url`, auth, attributes, etc. change, so a changed `remotecfg` block starts with an empty cache. | `config_manager.go`; CHANGELOG 1.20.0 "remotecfg: Use atomic writes for cache file (#7131)" |
| Stability | `remotecfg` is `StabilityGenerallyAvailable` (GA since v1.7.0). | `remotecfg.go` `Definition()`; CHANGELOG v1.7.0 |

### 1.2 Messages (alloy-remote-config v0.0.12, `api/collector/v1/collector.proto`)

| RPC | Request fields (field number) | Response fields | Idempotency option |
|---|---|---|---|
| `GetConfig` | `id` (1, required); `attributes` (2, **deprecated**); `hash` (3); `local_attributes` (4); `remote_config_status` (5); `effective_config` (6) | `content` (1) "The full configuration for the collector"; `hash` (2); `not_modified` (3) | `NO_SIDE_EFFECTS` |
| `RegisterCollector` | `id` (1, required); `attributes` (2, deprecated); `name` (3); `local_attributes` (4) | empty | `IDEMPOTENT` ("will update the collector's attributes if the collector is already registered and the attributes are different") |
| `UnregisterCollector` | `id` (1) | empty | `IDEMPOTENCY_UNKNOWN` |

Supporting messages: `RemoteConfigStatus { RemoteConfigStatuses status; string error_message }` with enum `UNSET=0, APPLIED=1, APPLYING=2, FAILED=3`; `EffectiveConfig { AgentConfigMap config_map }`; `AgentConfigMap { map<string, AgentConfigFile> config_map }`; `AgentConfigFile { bytes body; string content_type }`. The comments on these messages are copied from the OpAMP specification (including the "SHOULD be unset if unchanged since the last GetConfigRequest" rule), which is why a single internal model can back both protocols (§5).

Version history of the contract on the Alloy side (CHANGELOG): `RegisterCollector` used since v1.3.0; `not_modified` honoured since v1.5.0; `attributes` renamed from `metadata` in the 1.x preview; `remote_config_status` and `effective_config` sent since **v1.12.0 (2025-12-01)**. The deprecated `attributes` field (2) is **not** sent by v1.21.2 at all: `remotecfg.go` only populates `LocalAttributes`. A server that still reads field 2 sees an empty map.

### 1.3 What the client actually does (v1.21.2 `remotecfg.go` + `config_manager.go`)

Startup (`Run`):
1. Creates an isolated controller for remote components (`host.NewController("remotecfg")`).
2. `fetchLoadConfig(allowCacheFallback = true)`: calls `GetConfig` **before** registering. On any error it loads the on-disk cache.
3. `RegisterCollector` with `id`, `local_attributes` (= system attrs merged with user attrs), `name`. If this returns an error, `Run` returns that error. The scheduler logs `node exited with error` and drops the task; the Alloy process keeps running whatever was loaded in step 2, but **the polling loop never starts** (`internal/runtime/internal/controller/scheduler.go` lines 103–114). Shepherd's spec records the same observation ("Alloy stops its remote-config service when start-up registration fails").
4. Polling loop: every `poll_frequency` → `fetchLoadConfig(false)`; on context cancel → `UnregisterCollector` with a 1 s timeout.

Config reload (`Update`, e.g. `/-/reload` or SIGHUP): rebuilds the HTTP client if `url`/auth changed, recomputes the cache path, re-registers, then `fetchLoadConfig(true)`.

Each `GetConfig` request carries:
- `id`, `local_attributes` (always);
- `hash` = the **last non-empty `hash` the server returned** (`remoteHash`). If the server never sets `hash`, the client sends `""` forever and the server can never answer `not_modified`;
- `remote_config_status` only when it changed since the last *successful* send (reset after a failed request so it is re-sent);
- `effective_config` only when it changed since the last successful send; it is a single entry with key `""`, `content_type = "text/plain"`, body = the exact bytes that were loaded.

After any load attempt, if a status or effective-config change is pending, the client makes **one extra immediate `GetConfig` call** (`notifyStatusUpdate`) purely to deliver it; the response is ignored. Servers should expect two calls in quick succession after every config change and must treat `GetConfig` as the heartbeat/status channel.

Response handling, in order:

| Server response | Client behaviour | Status reported next |
|---|---|---|
| `not_modified = true` | No change. Metric `remotecfg_last_load_not_modified = 1`. Status becomes `APPLIED` **only if** last-received hash == last-loaded hash; otherwise unchanged (prevents a server that keeps re-serving a broken config from seeing `APPLIED`). | `APPLIED` or unchanged |
| `content` identical to the last content received (client FNV hash) | Skipped, nothing logged above debug. | unchanged |
| `content` identical to the currently loaded config | Skipped, status `APPLIED` (covers "revert to previous revision"). | `APPLIED` |
| `content` empty (`""`) | `parseAndLoad` returns without calling `LoadSource`; previously loaded remote components keep running; hash of empty recorded as loaded; status `APPLIED`. This is the documented "serve nothing but do not disturb" path shepherd relies on for unclaimed collectors. | `APPLIED` |
| New `content` that parses and loads | `APPLYING` → `LoadSource` → `APPLIED`; cache file written; `remotecfg_hash{hash}` gauge set. | `APPLIED` + new `effective_config` |
| New `content` that fails to parse/load | `FAILED` with `diag.Diagnostics.AllMessages()` as `error_message`; if a last-loaded hash exists, the cached config is re-parsed and re-loaded ("attempting to reload cached configuration to restore component health"); `last_received_hash` records the bad hash so the next `not_modified` does not flip to `APPLIED`. | `FAILED` (+ effective config of the restored cache) |
| Any RPC/transport error (401/403/404/415, connect codes, timeout, DNS) | `remotecfg_load_failures_total++`, `remotecfg_last_load_successful = 0`, status `FAILED` with `err.Error()`. During periodic polls: "continuing with current config". At startup/reload: cache fallback. Nothing is unloaded. | `FAILED` |
| `RegisterCollector` error at startup | Service exits; no further polling until Alloy restart (see above). | n/a |
| `RegisterCollector` error on reload (`Update`) | `Update` returns the error; the reload is reported as failed; the existing polling loop (if alive) continues. | n/a |

So the answer to "which error codes make Alloy keep its last-good config" is **all of them**; the only server behaviour that disables the agent is failing `RegisterCollector` at agent startup.

Metrics emitted (names as registered in `metrics.go`; whether the service registerer adds an `alloy_` prefix was not checked, UNVERIFIED): `remotecfg_hash{hash}`, `remotecfg_last_received_hash{hash}`, `remotecfg_last_load_successful`, `remotecfg_last_load_not_modified`, `remotecfg_load_failures_total`, `remotecfg_load_attempts_total`, `remotecfg_last_load_success_timestamp_seconds`, `remotecfg_request_duration_seconds`. These are what a server-injected self-monitoring pipeline should scrape to get agent-side truth about config delivery.

### 1.4 Server obligations derived from the client

1. Implement the three RPCs as Connect unary handlers behind h2c (so HTTP/1.1+proto, HTTP/1.1+JSON and HTTP/2 all work). `connect-go` generated handler + `net/http` is enough; shepherd and opsplane do exactly this.
2. Always return the whole config in `content`. There is no delta, patch, or multi-file form on this protocol (the multi-file `AgentConfigMap` exists only on the agent→server effective-config side).
3. Return a deterministic `hash` for the served bytes and answer `{not_modified: true, hash}` with empty content when `request.hash` equals it. Shepherd and alloy-fleet-manager both use `hex(sha256(content))`; the algorithm is the server's choice because the agent only echoes it. Keep served bytes free of timestamps or the hash will never match (shepherd removed the generation timestamp for this reason, #213).
4. Never return an RPC error for "nothing to serve"; return `content = ""`. An error flips the agent to `FAILED` and fires alerts even though nothing changed.
5. Make `RegisterCollector` tolerate backend outages: cache verdicts/last-served config per principal so registration succeeds during a database outage (shepherd `agent.outage_cache_ttl`, default 60 s), or agents that restart during the outage go deaf until their next restart.
6. Treat `GetConfig` as heartbeat: update `last_seen`, persist `remote_config_status` and `effective_config` **only when present** (absence means unchanged, not cleared), derive "inactive" from `last_seen` at read time.
7. Route on `id` + `local_attributes` (including `collector.version` and `collector.os`), never on the deprecated `attributes` field; `name` arrives only on `RegisterCollector`.
8. Bound request size (the effective-config echo is the largest message; shepherd refuses > 8 MiB with `CodeResourceExhausted`) and validate `id` length.
9. Budget for bursts: at least `N × (1/poll_frequency)` plus an extra call after each change; Grafana Cloud rate-limits `GetConfig`/`RegisterCollector` at 20 req/s per stack by default.

### 1.5 What the protocol does not carry

No server→agent commands (restart, collect bundle, upgrade), no server-assigned attributes pushed to the agent, no package/binary offers, no health model beyond `RemoteConfigStatus`, no streaming. Everything in §4 that goes beyond "serve config" must come from either a server-injected pipeline, a host helper, or Kubernetes.

### 1.6 The open proto is only the agent-facing half of Grafana's service

Grafana Cloud's Fleet Management management API lives on the **same** service name, `collector.v1.CollectorService`, and adds `ListCollectors`, `GetCollector`, `CreateCollector`, `UpdateCollector`, `BulkUpdateCollectors`, `DeleteCollector`, `BulkDeleteCollectors`, `ListCollectorAttributes`, with a `Collector` object `{id, name, remote_attributes, local_attributes, enabled, created_at, updated_at, marked_inactive_at, collector_type ∈ {ALLOY, OTEL}}`; a separate `pipeline.v1.PipelineService` has `Get/List/Create/Update/Upsert/Delete/BulkDelete/SyncPipelines`, `ListPipelinesRevisions`, `ListPipelineRevisions`, `GetPipelineRevision`, `validate_only` on writes, Alertmanager-syntax `matchers`, `config_type ∈ {ALLOY, OTEL}`, `source ∈ {GIT, TERRAFORM, GRAFANA}`; there is no rollback RPC (rollback = resubmit a revision snapshot). None of these management RPCs are in `grafana/alloy-remote-config`; whether Grafana publishes that proto anywhere is UNVERIFIED (not found). Our `fleet` plugin should expose an equivalent shape (collectors, pipelines, revisions, `validate_only`, Alertmanager matchers) so Terraform/GitOps tooling written for Grafana Cloud ports easily, but it will be our own proto.

Sources: [collector API](https://grafana.com/docs/grafana-cloud/send-data/fleet-management/api-reference/collector-api/), [pipeline API](https://grafana.com/docs/grafana-cloud/send-data/fleet-management/api-reference/pipeline-api/) (fetched 2026-10-27).

## 2. The three community servers

All facts from GitHub API metadata and repository files fetched 2026-10-27.

| | procoduck/shepherd | danielfree19/alloy-fleet-manager | opsplane-services/alloy-remote-config-server |
|---|---|---|---|
| License | Apache-2.0 | Apache-2.0 | MIT per `LICENSE`/GitHub API (README badge says "Apache v2", inconsistent) |
| Language / stack | Go server + React SPA (Go 1.27.3, Node 26, pnpm 12) | TypeScript (Fastify, Node ≥ 20), React UI, Go `fleetctl` + Terraform provider, npm SDK | Go |
| Created / last push | 2026-08-25 / 2026-10-27 | 2026-04-25 / 2026-04-25 | 2024-06-23 / 2026-02-18 |
| Latest release | v0.20.0 + chart 0.24.0 (2026-10-27); v0.19.1 2026-10-26 | v0.1.1 (2026-04-26) | v0.3.1 |
| Stars / open issues | 1 / 2 | 23 / 2 | 22 / 0 |
| Contributors | `procoduck` 281 commits, dependabot 47, one other with 2 | 1 (18 commits) | 2 (`oleewere` 22, `langerma` 2) |
| Public source reviewed | GitHub | GitHub snapshot | GitHub |
| Runtime requirements | Kubernetes 1.32+, Helm 3/4, **PostgreSQL 16+**; `make dev` compose for trying only | Docker/K8s/systemd + PostgreSQL | Docker; in-memory or Redis |
| Multi-tenancy | Yes: organisations, collectors claimed into orgs, several tenants per org, strict tenant mode (checks label maps/relabel rules), org-bound agent tokens, per-org roles | No (single-tenant RBAC) | `ORG_NAME` env per instance, shared Redis namespace |
| Agent auth | `basic_auth` with token UUID/secret, tokens bound to an org and optional cluster/role allowlists; OIDC identities for agents; token verdict cache during DB outage | Bearer: shared `AGENT_BEARER_TOKEN` or per-collector `fmt_…` token with the `agent` role (`collectors.poll` only) | None; README recommends Istio for TLS/mTLS/OAuth |
| Human auth | Local accounts (argon2id, throttled), OIDC with group→role mapping (presets for Entra, Okta, Google, Auth0, Keycloak, Cognito, GitLab, authentik, OneLogin), service accounts, Connect mgmt API `shepherd.mgmt.v1`, MCP server | Local users (bcrypt 12), API tokens, OIDC SSO, 14 permissions / 4 built-in roles, audit log | None |
| `hash` / `not_modified` | `hex(sha256(content))`; honours `request.hash`; empty config for unclaimed collectors | `sha256`; honours | Not implemented: no `hash`, never `not_modified`, full render each poll |
| `remote_config_status` / `effective_config` | Status + error persisted per instance; effective config: not stored as far as the spec shows (UNVERIFIED) | `last_seen` + status stored; `effective_config` "accepted on the wire but ignored" | Ignored; `Register`/`Unregister` are logging no-ops |
| Config model | Pipelines with **Alertmanager matchers** (ANDed) over `{cluster, role}` + optional admin labels + `local_attributes`; each pipeline wrapped in a `declare` block and merged; wizards, visual builder, raw text | Pipelines with jsonb `selector` (equality subset match) | Go templates (`*.conf.tmpl`) chosen by `template` attribute |
| Validation | 3 gates: syntax; `alloy validate` + component schema; merge dry-run against each affected collector's full config; **version-aware** against every `collector.version` in the fleet (schemas embedded for v1.18.0–v1.21.2); sandbox simulation | `alloy fmt` strict syntax | None |
| History / rollback | `pipeline_revisions`, restore keeps enabled state and re-runs gate | `pipeline_versions`, rollback by PATCH to older content | None |
| GitOps | Yes (PAT, basic, SSH, Azure DevOps SP, GitHub App), transactional per commit | Deferred | "ideally use GitOps" for template files |
| Health / inventory | Beacon pipeline (`prometheus.exporter.self` → remote_write to shepherd) reporting per-pipeline component health; `inactive_after`; refusal reasons on collector row | `last_seen`, status | Prometheus metrics on the server only |
| Remote upgrade / bundle / OpAMP | No / No / No | No / No / No | No / No / No |
| Maturity statement | "active development and pre-1.0; expect breaking changes"; verified-baseline ledger re-run per release; signed releases, SBOM | v0.1.x; "MVP" wording in docs | README note says it uses `attributes` "to be up-to-date with the alloy implementation" (outdated; code reads `local_attributes`) |

Reading: shepherd is a serious product with one author; alloy-fleet-manager is a well-documented but apparently abandoned (on GitHub) MVP; opsplane is a template renderer that predates the status/effective-config half of the protocol. The `grafana-stack.md` "small star counts" caveat is confirmed: 1, 23 and 22 stars respectively.

Sources: [shepherd](https://github.com/procoduck/shepherd) (README, `CHANGELOG.md`, `docs/spec.md`, `docs/project-status.md`), [alloy-fleet-manager](https://github.com/danielfree19/alloy-fleet-manager) (README, `docs/remotecfg.md`, `docs/auth.md`, `CHANGELOG.md`), [opsplane](https://github.com/opsplane-services/alloy-remote-config-server) (README, `pkg/config/grpc.go`).

## 3. Build vs adopt

| Option | What we get | What it costs | Verdict |
|---|---|---|---|
| Adopt shepherd as the fleet server | Everything in §2 today, actively maintained, Alloy 1.21.2-validated | Kubernetes-only (contradicts "compose self-hosters"), PostgreSQL 16+, a second tenancy/RBAC/OIDC/UI product next to our kernel, its org model would have to be mapped 1:1 to our tenants, bus factor 1, pre-1.0 breaking changes per release, no OpAMP, no upgrade/bundle | No as a dependency; yes as the conformance oracle and design reference |
| Adopt alloy-fleet-manager | Simple schema, token-per-collector model, Terraform provider | TypeScript in a Go kernel, single-tenant, public snapshot has no later activity since April | No |
| Adopt opsplane | Nothing we cannot write in an afternoon | No hash, no auth, no status | No |
| Build `fleet` plugin on `connect-go` + `opamp-go` | Exact fit to the kernel's tenant DB, secrets, auth and plugin test kit; one model for Alloy and OTel collectors; room for the host helper | We own the merge engine, validation gates and revision model (shepherd's spec is a public, detailed blueprint for all three) | **Build** |

Build size estimate for the agent-facing part: handler for three RPCs, tenant lookup from the bearer/basic credential, matcher evaluation, merge, `sha256`, serve cache, status/effective-config upsert. The design work is in the merge/validation/revision model, which we need regardless of which server speaks the wire.

Test strategy (fits `plugin-architecture.md` §4.7 "run a plugin alone against a fake kernel"): a compose/kind fixture that starts real Alloy v1.21.2 containers with a `remotecfg` block against the plugin, asserts the `remotecfg_*` metrics and the status/effective-config rows, then runs the same Alloy against shepherd to diff behaviour. Both shepherd and alloy-fleet-manager already ship such e2e suites to copy from.

## 4. Covering the Datadog Fleet Automation features Alloy lacks natively

`datadog-agent.md` §2.12 lists: inventory with config history, central config, remote integration/log config, tags, upgrades (via the separate `installer` binary, `remote_updates: true`), remote flares, public API. Mapping each onto Alloy:

### 4.1 Inventory and health (protocol-native)

- Registry row per `(tenant, id)` from `RegisterCollector`; instance heartbeat from every `GetConfig` (`last_seen`, `collector.version`, `collector.os`, `local_attributes`, `remote_config_status`, `error_message`, `effective_config` bytes).
- Health beyond "config applied": inject a **baseline self-monitoring pipeline** into every served config (Grafana Cloud does this: "autogenerated self-monitoring pipelines that collect internal metrics and logs"; shepherd's "beacon" does the same with `prometheus.exporter.self`). Ours writes to the tenant's Mimir/Loki/Pyroscope with a `fleet_collector_id` label: `prometheus.exporter.self` + `prometheus.scrape` → `remote_write`; `loki.source.journal`/`windowsevent` filtered to the Alloy unit → Loki; `pyroscope.scrape` of Alloy's own pprof → Pyroscope. Per-component health comes from `alloy_component_controller_running_components` and `alloy_component_evaluation_*`; config-delivery truth from the `remotecfg_*` metrics in §1.3.
- "Inactive" is derived from `last_seen` at read time (shepherd `agent.inactive_after`; Grafana Cloud `marked_inactive_at`).
- Tags: Grafana Cloud's `remote_attributes` (server-side labels on a collector, usable in matchers) is the equivalent of Datadog tags and costs one jsonb column.

### 4.2 Config history and rollback (protocol-native)

- Append-only `pipeline_revisions` (snapshot, operation INSERT/UPDATE/DELETE, author) as in Grafana Cloud and shepherd; `served_configs(collector_id, hash, content, computed_at)` so "what was this host running at time T" is answerable from the server; the agent's echoed `effective_config` is the ground truth to reconcile against.
- Rollback is server-side: restore a revision, re-run the validation gate, serve; the agent picks it up within `poll_frequency` and reports `APPLIED` (the "content identical to currently loaded" branch means reverting to a previous revision never triggers a reload if the agent is already on it).
- Agent-side protection already exists (parse failure → cached config reload), so the server's job is fleet-wide: canary by matcher subset, watch `FAILED` counts per served hash, auto-disable the revision that introduced a hash that fails on more than N% of targets. This is cheap because `FAILED` arrives on the immediate notify call, not the next poll.
- Validation gate before serving, in order of cost: Alloy syntax parse; `alloy validate` using a pinned Alloy binary; per-`collector.version` schema check (shepherd's Stage 3 — refuse a component the oldest active agent lacks, because "an Alloy that cannot load a remote config keeps its last one"); optional sandbox run.

### 4.3 Remote support bundle (not protocol-native)

Alloy's bundle is `GET /-/support?duration=N` on the HTTP listen address (default `localhost:12345`), returns a zip with `alloy-metadata.yaml`, `alloy-components.json`, `alloy-peers.json`, two metrics samples, runtime flags, environment, logs, `pprof/`, `sources/` and `sources/remote-config/remote.alloy`; it is plain text, local-only, and "isn't covered by the backward-compatibility guarantees". There is no way to trigger it through `remotecfg`.

Options:
1. **Flare-lite, always on**: the baseline pipeline in §4.1 already streams what most of the bundle contains (metrics, logs, pprof profiles, and the server holds the served config). A "collect diagnostics" button in our UI/Slack then assembles a bundle **server-side** from Mimir/Loki/Pyroscope + `served_configs` + `effective_config`. This covers the Datadog "remote flare" use case for triage without touching the host.
2. **Full bundle on Kubernetes**: the platform's operator (or any in-cluster job) calls `/-/support` on the pod via the API server and uploads to the tenant's object store. No agent change.
3. **Full bundle on VMs/Windows**: needs the host helper (§4.4) to call `localhost:12345/-/support` and upload.

Recommend 1 as the default, 2 and 3 as the "full bundle" action where available.

### 4.4 Remote upgrade (not protocol-native, and not solved by OpAMP either)

Facts: Alloy has no self-update; `grafana/alloy#3122` "Enable Alloy to upgrade itself" is an open `proposal` opened 2025-03-27 with 3 comments, the last on 2025-10-07 asking whether the feature is only for bare-metal users; a maintainer comment states the binary cannot auto-update itself. The OpAMP route does not help yet: the upstream `opampsupervisor` spec says the `accepts_packages` capability "is accepted in configuration but is currently disabled at runtime" (tracking `opentelemetry-collector-contrib#47272`), and Alloy's `otel-supervisor` "always sets `agent.executable` to the running Alloy binary", so even a future packages flow would be replacing Alloy itself from inside Alloy.

Design, by target:

| Target | Mechanism | Who initiates |
|---|---|---|
| Kubernetes (Helm/`k8s-monitoring`/alloy-operator) | Bump the chart/image version: our operator reconciles a `CollectorRelease` CR, or we open a GitOps PR | fleet plugin → operator / GitOps |
| Linux packages (apt/dnf/zypper) | `fleet-hostagent` (tiny Go binary, systemd service) polls our `fleet.v1.HostAgentService` for `desired_version`, runs the package manager, verifies `alloy --version` and `/-/ready`, reverts via the package manager on failure, reports result | fleet plugin → host agent |
| Windows (MSI) | Same helper as a Windows service running the MSI silently; the forum thread on Windows upgrades (600+ servers via PowerShell) shows this is what operators already do by hand | fleet plugin → host agent |
| Docker/Podman standalone | Out of scope for the helper; document Watchtower-style image bumps | operator |

The helper is the Datadog `installer` analogue (`datadog-agent.md` notes it is a separate binary used by Fleet Automation upgrades). It is optional: a host without it still gets inventory, config, history, rollback and flare-lite. It should speak our own small Connect service (two RPCs: `Poll` returning desired version + pending actions, `Report`), reuse the collector's credentials and `id`, and be built as a plugin-owned binary so it is tested in the same fixture. Signed release artifacts and a per-tenant allowlist of versions are the minimum safety bar (OpAMP's code-signing guidance applies by analogy: "Any executable code that is part of a package should be signed to prevent a compromised Server from delivering malicious code").

### 4.5 Fleet plugin C3 (components) — proposed

| Component | Responsibility | Depends on (kernel host services) |
|---|---|---|
| `fleet-agentapi` | Connect handlers for `collector.v1.CollectorService`; OpAMP handler at `/v1/opamp` (§5); auth of agent credentials; request-size and id limits; serve cache; outage cache | `host.Auth`, `host.DB`, `host.Metrics` |
| `fleet-registry` | Tenants' collectors, instances, agent tokens, `remote_attributes`, inactive derivation | `host.DB`, `host.Secrets` |
| `fleet-compose` | Pipelines, Alertmanager matchers, `declare`-wrap merge, baseline self-monitoring pipeline injection, `sha256` hash, `served_configs` | `host.DB` |
| `fleet-validate` | Alloy syntax, `alloy validate` with pinned binaries, per-version schema diff, `otelcol validate` for OTel configs | pinned Alloy/otelcol binaries in the plugin image |
| `fleet-revisions` | Revision log, restore, canary rollout, auto-disable on `FAILED` ratio | `host.DB`, `host.Events` |
| `fleet-health` | Reads `remotecfg_*`/`alloy_component_*` series and Alloy logs from the tenant's Mimir/Loki to compute component-level status; feeds the incident pillar | `host.Datasources` |
| `fleet-hostagent-api` | `fleet.v1.HostAgentService` for the optional helper: desired version, bundle collection, result reporting; bundle upload to object store | `host.ObjectStore` |
| `fleet-mgmt` | Management API mirroring Grafana Cloud's shape (collectors, pipelines, revisions, `validate_only`, `SyncPipelines`) consumed by the UI, Slack plugin, Terraform provider | `host.Auth` (RBAC), `host.Audit` |

Extension points exposed to other plugins: `fleet.ConfigSource` (a pillar can contribute a pipeline fragment, e.g. SecOps adds `beyla` network rules), `fleet.ValidationGate`, `fleet.OnStatusChange` events.

## 5. Should the server also speak OpAMP?

Yes, as a second listener in the same plugin, after the remotecfg path is solid. Evidence:

| Point | Fact | Source / date |
|---|---|---|
| Alloy OTel engine is OpAMP-only | Engine-extension docs: "The `remotecfg` block isn't supported, and configuration reload isn't available. Use OpenTelemetry OpAMP for configuration management instead." | [engine-extension](https://grafana.com/docs/alloy/latest/set-up/otel_engine/engine-extension/), public preview, fetched 2026-10-27 |
| `alloy otel-supervisor` | EXPERIMENTAL; simple mode hard-wires Grafana Cloud env vars (`GCLOUD_FM_URL` + `/v1/opamp`, basic auth of instance id:token); manual mode takes an upstream supervisor YAML and "Use manual mode to connect to an OpAMP server other than Grafana Fleet Management"; capability shown is `accepts_remote_config: true`; added in v1.19.0 (2026-08-21); v1.20.0 removed an inert `reports_remote_config` capability from the built-in config | [otel-supervisor](https://grafana.com/docs/alloy/latest/reference/cli/otel-supervisor/); CHANGELOG |
| OpAMP extension in the OTel engine | Added v1.18.0 (2026-07-17); the upstream `opampextension` (alpha) reports effective config, health, available components, heartbeat, optional restart via SIGHUP; it cannot accept remote config | CHANGELOG; [opampextension README](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/extension/opampextension/README.md) |
| Grafana Cloud behaviour to match | OpAMP over HTTP at `/v1/opamp`, protobuf `AgentToServer`/`ServerToAgent`, 30 s default polling, supervisor required for remote config, extension-only agents are status-only, "Fleet Management supports OpAMP capabilities for status reporting and remote configuration", "The Supervisor doesn't automatically roll back to the last working remote configuration" | [FM architecture](https://grafana.com/docs/grafana-cloud/send-data/fleet-management/introduction/architecture/) |
| GA date | Remote management of OTel Collectors GA 2026-07-08 | secondary (opsmatters/What's New summary in search results), UNVERIFIED against Grafana's own announcement |
| Spec and library | OpAMP spec status **Beta** (`opamp-spec` v0.20.0 latest tag; repo pushed 2026-10-26); `opamp-go` Apache-2.0, 238 stars, latest tag v0.25.0, pushed 2026-10-26, provides HTTP + WebSocket server transports | GitHub API 2026-10-27 |
| Capabilities we would implement | Server: `AcceptsStatus`, `OffersRemoteConfig`, `AcceptsEffectiveConfig`; later `OffersConnectionSettings` (heartbeat/polling offers). Agent side expected: `ReportsStatus`, `AcceptsRemoteConfig`, `ReportsEffectiveConfig`, `ReportsHealth`, `ReportsRemoteConfig`, `ReportsHeartbeat`, `ReportsAvailableComponents`. Skip `OffersPackages`/`AcceptsPackagesStatus` until the supervisor enables packages | spec enums (`AgentCapabilities`, `ServerCapabilities`), supervisor spec |
| Supervisor rollback | `agent.automatic_config_rollback: true` (off by default) caches the last working config and swaps back on start failure — the agent-side equivalent of what Alloy's remotecfg does unconditionally | supervisor spec "Automatic Remote Configuration Rollback" |

Why one plugin, not two: the Alloy proto's `RemoteConfigStatus` (UNSET/APPLIED/APPLYING/FAILED + `error_message`) and `EffectiveConfig`/`AgentConfigMap`/`AgentConfigFile` are the OpAMP messages with the comments copied verbatim; OpAMP's `AgentRemoteConfig { AgentConfigMap config; bytes config_hash }` is the push-side twin of `GetConfigResponse { content, hash }`. Differences to absorb in the adapter: hash is `string` vs `bytes`; polling default 1 m vs 30 s; OpAMP identity is `instance_uid` (16-byte UUID) + `AgentDescription.identifying_attributes`, Alloy's is `id` string + `local_attributes`; OpAMP has `ComponentHealth`, Alloy has none; OpAMP has WebSocket, Alloy is poll-only. Model it as Grafana Cloud does: `collector_type ∈ {ALLOY, OTEL}` and `config_type ∈ {ALLOY, OTEL}` on the same tables, with the merge engine producing Alloy syntax for one and Collector YAML for the other.

Phase order: remotecfg first (it is GA, every Alloy install can use it with one block, and it is the only path for the default engine); OpAMP second for (a) Alloy OTel engine users, (b) plain `otelcol-contrib` fleets that customers already run and do not want to replace, (c) parity with Grafana Cloud.

## 6. Open questions and unverified items

- UNVERIFIED: whether Alloy's service registerer prefixes the `remotecfg_*` metric names with `alloy_` (the `metrics.go` constructors set no namespace).
- UNVERIFIED: whether shepherd stores the agent's `effective_config` bytes (its spec persists status and error; effective config was not found in the schema excerpt).
- UNVERIFIED: the GA date 2026-07-08 for OpAMP remote management in Grafana Cloud comes from a secondary summary, not from a Grafana page fetched here.
- UNVERIFIED: whether Grafana publishes the management-side protos (`pipeline.v1`, the extra `collector.v1` RPCs) anywhere; the only public repo contains the three agent RPCs.
- Open: whether `fleet-hostagent` should be our own binary or a thin wrapper around the OpAMP supervisor once `accepts_packages` is enabled upstream (contrib #47272 status not checked beyond the spec note).
- Open: Alloy's isolated remotecfg controller and clustering — the docs page does not describe the interplay; CHANGELOG entries show cluster-peer notifications were fixed for remotecfg components, so clustered scrape sharding inside remote pipelines should be e2e-tested, not assumed.
- Open: compose self-hosters need a single-binary fleet server with SQLite or Postgres; the design above assumes the kernel's `host.DB`, so the plugin must not require Postgres-only features (shepherd's pg-specific `jsonb`/`sqlc` approach is not portable).

## 7. Sources

Primary (fetched 2026-10-27):
- Proto and README: https://github.com/grafana/alloy-remote-config (Apache-2.0; `api/collector/v1/collector.proto` at `main`; tag v0.0.12 = commit 2025-10-27 "Merge pull request #25 from grafana/effective-config")
- Alloy v1.21.2 sources: https://github.com/grafana/alloy/blob/v1.21.2/go.mod ; `/internal/service/remotecfg/{remotecfg.go,config_manager.go,api_client.go,arguments.go,metrics.go}` ; `/internal/service/http/supportbundle.go` ; `/internal/runtime/internal/controller/{node_service.go,scheduler.go}` ; `/internal/alloycli/cmd_run.go`
- Alloy CHANGELOG: https://github.com/grafana/alloy/blob/main/CHANGELOG.md (1.21.0 2026-10-16, 1.20.0 2026-09-25, 1.19.0 2026-08-21, 1.18.0 2026-07-17, 1.12.0 2025-12-01, v1.7.0, v1.5.0, v1.3.0)
- Alloy releases API: v1.21.2 published 2026-10-22
- Alloy docs: https://grafana.com/docs/alloy/latest/reference/config-blocks/remotecfg/ ; https://grafana.com/docs/alloy/latest/reference/cli/otel-supervisor/ ; https://grafana.com/docs/alloy/latest/set-up/otel_engine/engine-extension/ ; https://grafana.com/docs/alloy/latest/troubleshoot/support_bundle/
- Alloy self-upgrade proposal: https://github.com/grafana/alloy/issues/3122 (open, label `proposal`, created 2025-03-27, updated 2025-10-07, 3 comments)
- Grafana Cloud Fleet Management: https://grafana.com/docs/grafana-cloud/send-data/fleet-management/introduction/architecture/ ; https://grafana.com/docs/grafana-cloud/send-data/fleet-management/api-reference/ ; …/api-reference/collector-api/ ; …/api-reference/pipeline-api/ ; https://grafana.com/docs/grafana-cloud/send-data/fleet-management/set-up/onboard-collectors/standalone-otel/
- OpAMP: https://github.com/open-telemetry/opamp-spec/blob/main/specification.md (Status: Beta) ; https://github.com/open-telemetry/opamp-go (v0.25.0) ; https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/cmd/opampsupervisor/specification/README.md ; https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/cmd/opampsupervisor/README.md (alpha) ; https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/extension/opampextension/README.md (alpha)
- Community servers: https://github.com/procoduck/shepherd (README, CHANGELOG.md, docs/spec.md, docs/project-status.md) ; https://github.com/danielfree19/alloy-fleet-manager (README, docs/remotecfg.md, docs/auth.md, CHANGELOG.md, package.json) ; https://github.com/opsplane-services/alloy-remote-config-server (README, pkg/config/grpc.go) ; GitHub REST API metadata for all three plus opamp-go/opamp-spec

Secondary:
- Grafana community forum, "Grafana Alloy Upgrade Process for Windows Deployments": https://community.grafana.com/t/grafana-alloy-upgrade-process-for-windows-deployments/146339
- Search-result summaries citing a Grafana What's New entry for OpAMP GA on 2026-07-08 (not fetched directly)

---

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
