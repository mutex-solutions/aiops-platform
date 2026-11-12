# Architecture proposal: plugin-kernel purist

Status: historical proposal, one of three design alternatives for the judge panel. Date: 2026-10-27. The [canonical decisions](../../architecture/00-decisions.md) supersede this proposal. Targets, sizing estimates and roadmap dates below are proposal assumptions, not measurements or current commitments. Executor contracts are requirements of the canonical record §2.2; upstream boundaries follow §4.3. Editorial preparation follows [ADR-056](../../adr/ADR-056-documentation-baseline.md), and the publication-rights gate remains open.
Angle: optimise for "everything is a plugin, testable alone, composed by profiles" exactly as DeepSeek Harness / Cordis do it, ported to a Go server kernel. Where purity costs self-host simplicity the cost is stated, not hidden.

How to read: section 1 fixes the rules every later section obeys; section 2 takes a position on each of the fourteen open decisions and cites the research doc that carries the evidence; sections 3 to 6 are the inventory (containers, extension points, data, profiles); section 7 is the roadmap; section 8 is what can go wrong and what this proposal refuses to build. Research citations use the form `docs/research/<file>.md §n`.

Naming convention used throughout: plugin, bundle, profile and container ids use signal names (`metrics`, `logs`, `traces`, `profiles`) or our own words, never a Grafana Labs mark (`docs/research/grafana-stack.md §13.2`). Prose may say that a plugin writes to Mimir or reads from Loki.

---

## 1. Principles

These are the rules the rest of the document is checked against. Each one names the dsh/Cordis mechanism it comes from and the server-side form it takes here.

| # | Principle | Origin | Server-side form |
|---|---|---|---|
| P1 | The kernel is a loader with a product composed on top, not a product with a plugin API bolted on. | `dsh-architecture.md §0`, §7 ("apps are profiles") | The `aiops` binary boots a profile. The hosted service, the self-hosted service, the single-node demo and the agent config generator differ only in which bundles are listed. There is no second binary for any surface except the CLI. |
| P2 | Three-role seam: definition, provider, consumer. Extension plugins depend on definitions, never on concrete providers. | `dsh-architecture.md §2.3`, §6 | Every extension point is a small Go interface plus a proto service in `sdk/ext/<point>`; providers live in separate modules; a pillar that needs logs imports `ext/datasource`, never the Loki client. |
| P3 | Declared needs drive activation; row order carries no load semantics. | `dsh-architecture.md §1.1`, §1.4 (epoch string) | A row is `PENDING` until every `requires` is `ACTIVE`; replacing a provider restarts its consumers; the loader computes this, plugin authors do not wire events. |
| P4 | Every effect is reversible; disposal is complete and reverse-ordered. | `dsh-architecture.md §1.4` (`ctx.effect`), `plugin-architecture.md §4.3` | Routes, scheduled jobs, event subscriptions, extension-point registrations and metrics are registered through the `Host` and unwound by the kernel after `Shutdown`. Every registry gets the mandatory "dispose the contributing row, assert cleanup" test. |
| P5 | Composition is data: ordered, id-addressed patch layers over an empty root; whole-row replace; dumpable with provenance. | `dsh-architecture.md §3.2`, §8.1 item 3 | `aiopsctl profile render --dump` prints the composed tree with the layer that last touched each row. No deep merge. No code in manifests. Config expressions are CEL over `env` and already-active services, never a general language. |
| P6 | A plugin that cannot be tested alone is not a plugin. | `dsh-architecture.md §5`, `plugin-architecture.md §4.7` | Fake host (`sdk/plugintest`), per-point conformance suite, recorded fixtures in strict replay, real-composition smoke through the loader from the built binary. "Hand-built suites are insufficient" for product-visible rows. |
| P7 | Explicit, published failure policy. | `dsh-architecture.md §5.4` | A short global list of required rows stops boot; everything else degrades to a warning; detached panics are fatal. The matrix is a test that boots the real artifact. |
| P8 | Process boundary where language or trust changes; in-process otherwise. The runtime is a profile row decision, not an SDK decision. | `plugin-architecture.md §3` (matrix), §4.1 | Every extension point has an in-process form and a `remote.<point>` gRPC adapter. Every first-party Go plugin must also build and pass conformance as a standalone gRPC process (`aiops plugin serve <id>`); CI proves nothing in-tree is privileged. |
| P9 | Operator-visible means logged. | `dsh-architecture.md §8.1` item 9, `gap-kernel-hot-path-and-grafana-ui-integration.md §2.4` | Every state change is an outbox event in the same transaction; approvals, tool outcomes and notifications are events; projections (Slack thread, dashboard, timeline) derive from them. |
| P10 | Mandatory policy cannot be bypassed by an executor. | Canonical record §2.2, §4.3 | Approval, ledger-before-write, credential leases, quorum, audit and PII scrub remain obligations at the execution boundary. This proposal's host-side placement is historical; the current record assigns them to bound providers and dispatcher guards (section 2.14). |
| P11 | Honesty about the purity tax. | this proposal | Where a seam, a sidecar or a second runtime exists only to keep the model pure, section 8 says what it costs a self-hoster and which profile hides it. |

---

## 2. Decisions

### 2.1 Kernel language and plugin mechanism

**Choice.** Go kernel with a compile-time module registry (Alloy / Caddy / OTel Collector shape), a gRPC-over-UDS (local) or mTLS-TCP (Kubernetes) `remote.<point>` adapter for every extension point so plugins in any language run out of process, and Wasm (Extism on wazero) as a third runtime deferred to phase 3 for tiny untrusted contributions. Not Cordis/Node, not Python/pluggy as the kernel, not Go's native `plugin` package.

**Evidence.** `plugin-architecture.md §0` and §3: the weighted matrix gives the Go registry 159 and the gRPC sidecar 186 out of 215 and concludes they are complementary, which is why Alloy, OTel Collector, Grafana and Dapr each ship both. Cordis says of itself that its API "is not yet stable and may change without notice" (§2.3). Go `plugin` cannot unload, needs toolchain lockstep and is poorly supported by the race detector (§2.1). `dsh-architecture.md §8.2` lists exactly which dsh mechanics are JS-specific (Proxy contexts, `!!js` YAML, pnpm per profile, ESM HMR) and their replacements. `gap-kernel-hot-path-and-grafana-ui-integration.md §4.3` (D10) settles plain gRPC over hashicorp/go-plugin because our sidecars are started by the orchestrator, not launched by the host.

**What runs where.**

| Runtime | Rows | Why |
|---|---|---|
| In-process (compiled into the default binary) | `datasource.*`, `signal-ingest.*`, `correlator.*`, `approval-policy.builtin`, `notifier.*`, `chat-surface.slack-*`, `scheduler.*`, `store.*`, `events.*`, `secrets.*`, `gateway.inproc`, `tenant`, `limits-renderer.*`, `fleet`, `auth`, `llm` gateway and `llm.provider.*`, `on-call-provider.{goalert,pagerduty,opsgenie,irm}` adapters, `report-renderer.{markdown,html}`, `verifier.*`, `change-hub`, `casefile`, `audit`, `admin` | Hot path, Go ecosystem clients, no untrusted code, no per-tenant memory of credentials |
| Out of process (gRPC) | `investigator.holmes` (Python, HolmesGPT), future `investigator.*` and `pillar.*` sidecars in Python, `remediation-executor.*` (`instancing: per-tenant`, holds write credentials in memory), `report-renderer.pdf` (headless Chromium), `tool.*` contributed by customers in other languages, test-only `llm.provider.replay` | Language boundary, blast-radius containment, per-tenant resource limits |
| Wasm (phase 3) | `approval-policy.cel-wasm`, report templates, tiny `tool.*` contributions | Sandboxed, hot-reloadable, no process per plugin |

**The purist rule that makes this more than a recommendation.** The in-process registry and the remote adapter implement the same `sdk/ext/<point>` interface, and the conformance suite for each point runs three times: directly against the Go implementation, through `remote.<point>` against the same plugin served by `aiops plugin serve <id>`, and (phase 3) through the Wasm adapter. If a first-party plugin only passes in-process, that is a defect, exactly as "a spec passing only alone" is a defect in dsh's test policy (`dsh-architecture.md §1.4`). Consequence: every extension-point interface is proto-first; no Go-only types (no `io.Reader`, no `context.Value` smuggling) cross a point boundary.

**Kernel SDK contract (sketch).** Lifecycle `Validate(cfg) → Provision(ctx, host, cfg) → Start(ctx) → Update(ctx, cfg)* → Shutdown(ctx)`, plus `Health()`, mirrored 1:1 in proto and the Python SDK (`plugin-architecture.md §4.3`). Manifest `plugin.yaml` with `id`, `version`, `kernelApi` range, `runtime`, `provides`, `requires` (required or optional), `tenantScope`, `instancing`, `capabilities`, `configSchema` (JSON Schema 2020-12), `artifact` (OCI ref + cosign) (§4.2). Compatibility is checked at the composition boundaries the kernel owns: install, boot, recompose (`dsh-architecture.md §4.3`).

**Required rows.** `loader`, `store`, `events`, `plugin-host`, `api-server`. Everything else, including `signal-ingest` and `chat-surface`, is best-effort at boot: the kernel starts with every row that can activate and prints one grouped diagnostic for the rest (`dsh-architecture.md §5.4`). A profile may mark any row `required: true` to tighten this.

**Where purity costs.** Adding an in-tree Go plugin means a rebuild (`aiopsctl build --manifest dist.yaml`, ocb-style). Self-hosters never run the builder: the published `aiops` binary has every first-party in-process plugin compiled in and the profile selects which activate. The price is a fat binary (expect well over 100 MB) and that a self-hoster who writes their own Go plugin must either rebuild or run it as a gRPC process. The second path is the supported one.

### 2.2 Investigator runtime

**Historical choice.** One `investigator` interface, with HolmesGPT and a first-party loop as possible providers; this proposal selected `investigator.holmes` for MVP. The canonical record now selects a first-party default harness (§2.1 row 2). Verification and correlation remain separate platform responsibilities, and every executor must satisfy the canonical record §2.2.

**Evidence.** `gap-kernel-hot-path-and-grafana-ui-integration.md §4.1` describes HolmesGPT 0.45.0, Apache-2.0, CNCF Sandbox, and its stateless `POST /api/chat` with SSE, `approval_required`, `conversation_history` and `tool_decisions`. `oncall-and-aiops-oss.md §B.1` surveys its public telemetry toolsets; `plugin-architecture.md §2.2` surveys the YAML toolset format. These facts support an adapter design, not a measured performance or quality claim and not authorization for an investigator to execute writes.

**Contract.** `Investigate(ctx, tenant, Incident, ToolSet) → stream Finding` where `Finding` is one of `ToolCall`, `ToolResult`, `ApprovalRequired{pending[]}`, `Message`, `Answer`, `TokenUsage`. The kernel owns the transcript (`casefile.investigation` rows, one per step so a crash resumes), turns `ApprovalRequired` into `approval` service requests, and resumes the turn from a River job when a decision lands. The investigator never talks to the LLM provider directly in hosted mode: its base URL is the kernel's per-tenant `llm` endpoint (section 2.13), which is how budgets apply to an out-of-process investigator.

**Tenancy.** Hosted: `instancing: per-tenant` because HolmesGPT's toolset and model configuration are process-level and its admin endpoints are unauthenticated (D8). Self-host: one shared instance. Conformance: the SSE contract replayed from cassettes, plus "tool call never executes without a kernel decision".

**Compatibility criterion.** An enabled investigator must fit the read-only harness and tool boundary. Models do not produce remediation plans; the canonical record §2.2 defines a typed action catalogue and dispatcher guards. A provider that cannot satisfy this boundary remains outside shipped profiles.

### 2.3 On-call

**Choice.** `on-call-provider` is a first-class extension point from day one. MVP rows: `goalert` (embedded, the self-host default; one instance per tenant in hosted), `pagerduty`, `opsgenie`, `irm` (thin webhook/API adapters), and `none` (route to `notifier` only, for the single-node profile). A native schedules/escalation module `on-call-provider.builtin` is phase 2 and only on a trigger. The incident timeline and postmortem live in a separate small plugin (`casefile`) regardless of the on-call choice.

**Evidence.** `oncall-and-aiops-oss.md §A.2`, §A.3, §A.4: GoAlert is Apache-2.0, Go, single binary on Postgres, v0.35.0 (2026-09-21), does schedules, rotations, overrides, escalation, Slack, Twilio, email, has a GraphQL API, and is single-tenant. Everything else is archived (OnCall OSS, Dispatch, OpenDuty, Cabot), does not schedule (Keep, Alertmanager, SigNoz, Uptime Kuma) or is a monolith (OneUptime). OnCall OSS is not adopted; its AGPL and plugin-directory restrictions are reviewed separately in the on-call licence audit. `gap-kernel-hot-path-and-grafana-ui-integration.md §4.2` (D9) ratifies: GoAlert as a per-tenant sidecar with its own database, `WhoIsOnCall` cached in `store.kv` with a 60 s TTL so Slack `/aiops oncall who` never waits on GoAlert.

**Contract.** `Schedules`, `WhoIsOnCall(schedule, at)`, `Page(Escalation, Incident) → PageID`, `Ack`, `Resolve`, `Overrides`, optional capability `Heartbeat` (dead-man switch). The verbs match what mcp-grafana already models for OnCall so the chat agent can drive either backend (`oncall-and-aiops-oss.md §B.8`). Conformance runs against the `goalert/demo` container.

**Triggers for the native module (any one).** Hosted tenant count beyond what one GoAlert per tenant can be operated at; Slack-native schedule editing becomes a top request; one data model is needed across schedules and the incident timeline. When triggered, port GoAlert's `schedule`, `override`, `escalation`, `oncall` package semantics (Apache-2.0 permits it) into roughly ten tables behind the same interface; do not port the UI.

**Where purity costs.** A self-hoster who wants paging runs GoAlert and its database as a second service even though the kernel already has Postgres. The alternative, writing schedule math in the MVP, is worse. The `single-node` profile composes `on-call-provider.none` by default so the demo path has no sidecar.

### 2.4 Slack runtime

**Choice.** `chat-surface.slack` is a Go plugin inside the kernel on `slack-go/slack` (v0.31.0), with two receiver rows behind one transport-agnostic handler core: `chat-surface.slack-events` (Events API, hosted profile) and `chat-surface.slack-socket-mode` (self-host profiles). One Slack app, one bot user, pillar as a routing dimension. No Bolt Python or JS sidecar.

**Evidence.** `gap-slack-chatops-tenancy-and-runtime.md §0`, §3, §6, §8: slack-go merged `agents.sessions.*`, the agent events and `authorizations` parsing in v0.30.0; the Bolt `Assistant` helpers still call `assistant.threads.setStatus`, which Slack deprecated for February 2027, so a Bolt sidecar would buy nothing and would move installation → tenant routing, user linking, `approvals.decide` checks and L3 quorum out of the kernel. Socket Mode is excluded from the Marketplace and an unlisted distributed app is throttled to 1 request per minute on `conversations.replies`, so Events API is the hosted transport; Socket Mode is the right self-host default because it needs no ingress. §9 gives the C3 (receiver, installer, router, identity, commands, conversation, session, streamer, approvals, webapi, audit).

**Purist shape.** `chat-surface` is an extension point; a Teams or Mattermost surface can be a gRPC plugin in any language. `conformance/chat-surface/` tests envelope normalisation, 3-second ack, retry dedup, external-user rejection and approval idempotency over both runtimes with recorded Slack fixtures. The kernel keeps its own thin `slackapi` interface so slack-go's breaking minors (Go 1.26 floor, struct changes) stay local to one package. `notifier.slack` shares the installation store and Web API client pool with the chat surface.

### 2.5 Who receives alert webhooks

**Choice.** The kernel's `signal-ingest.*` rows receive every Alertmanager, Grafana-managed alerting and vendor webhook. One transaction writes the raw group and the normalised alerts and emits `signal.received`; the `correlator` opens or joins the incident; the `router` component decides from tenant policy whether the incident pages (`on-call-provider.Page`) or only notifies (`notifier`). The on-call provider never receives alert webhooks directly, not even GoAlert, whose native integration keys are not used.

**Evidence.** `gap-kernel-hot-path-and-grafana-ui-integration.md §2.5` (D4): ack p99 ≤ 250 ms after a durable write, dedupe plus deterministic correlation p99 ≤ 2 s, first notification p95 ≤ 5 s, 2,000 alerts/s per node, per-tenant buckets with 429 and `Retry-After`, idempotency keys `(tenant, source, fingerprint, startsAt)`, storm mode above 500 alerts/min collapsing into one incident. The canonical record §2.1 row 5 governs current admission, signal identity and paging. Routing through one chokepoint is what makes the provider swappable, gives one idempotency key, one quota, one audit row and one PII scrub for every path.

**One documented exception.** The dead-man switch. An Alertmanager `Watchdog` route may go straight to the on-call provider's `Heartbeat` capability (GoAlert has a heartbeat package) so "the kernel is down" still pages. This is the only alert path that bypasses the kernel, and it carries no tenant data.

**Where purity costs.** A GoAlert user loses GoAlert's own Alertmanager integration ergonomics, and the kernel ingest path is on the paging SLO. The hot-path envelope in D4 is a requirement on `signal-ingest`, not an aspiration; the failure matrix marks `signal-ingest.alertmanager` as `required: true` in the hosted and self-host-full profiles.

### 2.6 Remote config and fleet server

**Choice.** Build the `fleet` plugin on `connect-go`, implementing `collector.v1.CollectorService` (`GetConfig`, `RegisterCollector`, `UnregisterCollector`) exactly as Alloy 1.21.2 consumes it; add an OpAMP listener (`opamp-go`) on the same plugin in phase 2; do not adopt shepherd, alloy-fleet-manager or opsplane, but use shepherd as the conformance oracle and design reference. A small optional `fleet-hostagent` helper for VM remote upgrade and support bundles is phase 2.

**Evidence.** `gap-remotecfg-server-build-vs-adopt.md §0` items 1 to 6 and §3: the wire contract is three unary RPCs, the server returns the full config, the agent hashes content itself and keeps last-good on any error; the three community servers are single-maintainer, pre-1.0 or dormant, shepherd is Kubernetes-only and would add a second tenancy/auth/UI product next to the kernel; Alloy's OTel engine is OpAMP-only and Grafana made OTel remote management GA in Cloud. §4.5 gives the C3 (`fleet-agentapi`, `-registry`, `-compose`, `-validate`, `-revisions`, `-health`, `-hostagent-api`, `-mgmt`). `grafana-stack.md §3.5`: the proto is open and Apache-2.0; Grafana ships no server.

**Purist shape.** `fleet` exposes two extension points of its own: `fleet.config-source` (a pillar contributes a pipeline fragment, for example SecOps adds Tetragon policy tailing) and `fleet.validation-gate` (syntax, pinned `alloy validate`, per-version schema). It must run on SQLite in `single-node`, so no Postgres-only features (the research flags shepherd's `jsonb`/`sqlc` approach as non-portable, §6). `RegisterCollector` availability is on the agent SLO (a failed register at startup stops polling until restart, §0 item 2), so the `fleet` and `auth` rows may be served from a separately scaled process in hosted (section 2.7).

### 2.7 Tenant gateway for ingest

**Choice.** `gateway` is an extension point with two implementations that pass the same conformance suite: `gateway.envoy` (hosted and self-host-full; the plugin renders Envoy config with `jwt_authn` and `claim_to_headers` writing `X-Scope-OrgID` from a signed claim, plus the JWKS file; Envoy pods have no runtime dependency on the kernel) and `gateway.inproc` (single-node; a Go reverse proxy in the kernel binary with the same route table). Per-agent credentials are OAuth2 client credentials minted by `Enrol`; access tokens are 10 to 15 minute JWTs that every Alloy write block refreshes itself. Tenant id is `[a-z0-9][a-z0-9-]{0,62}` and is the `X-Scope-OrgID` value. The kernel is not the proxy in hosted.

**Evidence.** `gap-tenant-gateway-and-agent-enrolment.md §0` (D1 to D5), §1.1 (4xx is terminal for remote_write and loki.write, so credential rotation must overlap and the gateway's availability is the ingestion SLO), §1.3 (the enrolment sequence and the 15-line local `config.alloy` containing only a `remotecfg` block), §2.2 (gateway candidates; Envoy `jwt_authn` needs no round trip per request; kernel-as-proxy is single-node only), §3 (the kernel configures the gateway but does not sit in front of it), §6 (`auth`, `gateway`, `tenant`, `fleet` contracts). `plugin-architecture.md §2.5` for the tenant-id charset.

**Purist shape.** `conformance/gateway/` runs identical black-box tests against both implementations: reject missing token, reject pipe-joined tenants on write routes, strip any client-supplied `X-Scope-OrgID`, 413 on oversize body, pass `Retry-After`, fail closed. The `auth` row (`/oauth2/token`, JWKS) is the one control-plane endpoint on the ingest SLO; by P8 it can run as its own process from the same binary (`aiops plugin serve auth`) and the hosted profile does exactly that.

**Where purity costs.** Self-host-full gains an Envoy container. The alternative (kernel in the ingest path) means every kernel deploy is a 4xx window that drops samples at the agent. The compose profile therefore ships Envoy by default and documents the one-line patch to switch to `gateway.inproc` for sites that accept that window.

### 2.8 Hosted Grafana tenancy

**Choice.** One Grafana org per tenant on shared Grafana pools is the hosted default; instance per tenant is an opt-in isolation tier. The Grafana app plugin (`aiops-app`) is a thin proxy: Scenes frontend plus a Go backend with `CallResource` → kernel HTTP API, `CheckHealth` and a settings page holding `{kernel_url, tenant_id}` and the tenant token. Identity enters the kernel as the Grafana-signed `X-Grafana-Id` JWT verified against `/api/signing-keys/keys`; the Grafana basic role is a floor, approver bindings and quorum live in the kernel. Self-hosters run one org mapped to tenant `default`. The release pipeline ships an unsigned build (allow-list) and a privately signed build for our hosted root URLs; community signature once the kernel API freezes.

**Evidence.** `gap-kernel-hot-path-and-grafana-ui-integration.md §3.1` to §3.4 (D5, D6, D7, D11): the backend plugin is a go-plugin subprocess owned by Grafana and may be restarted on plugin update, so the kernel cannot live inside it; Grafana OnCall's own plugin backend did nothing but proxy; `externalServiceAccounts` only supports single-org setups so it is unusable for org-per-tenant; OSS Grafana cannot bind custom roles to individual users, so anything above the basic-role floor is a kernel binding. `grafana-stack.md §10.2`, §10.6, §14.4.

**Purist note.** Grafana is a consumer of the kernel API, not a plugin host and not a plugin. The `datasource.dashboards` row (Grafana HTTP API: orgs, data sources, annotations, silences, rendering) carries `{url, org_id, service_account_token}` so org-per-tenant and instance-per-tenant are the same mapping. The kernel must run with no Grafana at all (`aiops-only` profile, Slack-only operators).

### 2.9 Kernel state

**Choice.** PostgreSQL in every profile except `single-node`, which uses SQLite (WAL, one writer). River (MPL-2.0) on the same database for jobs, periodic schedules, unique jobs and leader election. Events are a transactional outbox table consumed by River workers, with `LISTEN/NOTIFY` as the doorbell. No Kafka and no Redis in the kernel; `events.nats` is an optional fan-out plugin for hosted, `sink.kafka` an optional export plugin. Each plugin owns its schema and migrations; every row carries `tenant_id` and the hosted profile enables row-level security as a second fence.

**Evidence.** `gap-kernel-hot-path-and-grafana-ui-integration.md §2.1` to §2.4 (D1 to D3): Kafka is absent from three of four profiles and is a write-ahead log, not a work queue; River gives per-message ack, delay, uniqueness, cancellation and leader election; River SQLite is experimental and roughly a quarter of Postgres speed, accepted for single-node; `LISTEN/NOTIFY` payload and queue limits mean it is a doorbell only; per-tenant fairness is a kernel-side token bucket until a tenant can be shown to starve another. The canonical record §4.3 governs current transaction, event and job contracts.

**Purist shape.** `store`, `scheduler` and `events` are extension points with `postgres` and `sqlite` providers; the conformance suite runs every `store`/`scheduler`/`events` test on both drivers so plugins never observe the difference. `scheduler.kubernetes-cronjob` and `scheduler.temporal` remain legal rows for people who already run those.

### 2.10 eBPF sensor bundle

**Choice.** Alloy's privileged DaemonSet carries `beyla.ebpf` (already a supervised child process) and `pyroscope.ebpf` (in-process library, behind a feature flag, restart at the orchestrator level). Tetragon is a second DaemonSet with its own chart, operator and CRDs; Falco is an optional, mutually exclusive plugin for tenants that want the curated Falco rule library. Kernel floor Tier A: Linux 5.10+ with BTF on amd64/arm64; Tier B: RHEL-family 8.5+ best effort; Tier C: Alloy without eBPF components, chosen automatically by the config generator. No BTF mirror in v1. This proposal ratifies Tetragon over Falco, which overrides `oncall-and-aiops-oss.md §B.3`.

**Evidence.** `gap-ebpf-sensor-bundle-and-kernel-floor.md §1`, §3.2 (Tetragon: in-kernel filtering, namespace-scoped CRDs, Go user space on cilium/ebpf, non-root operator, floor inside Tier A/B; the flip condition is "must ship curated detections at v1 without authoring a starter pack"), §4, §6 (two DaemonSets, capability lists, no `privileged: true`), §7 (non-goals). `grafana-stack.md §3.7` for the Alloy component facts.

**Purist shape.** Sensors are agent-side plugin rows (`agent.sensor.beyla`, `agent.sensor.profiler`, `agent.sensor.tetragon`, `agent.sensor.falco`), each declaring the config fragment it contributes, the capabilities it needs and its kernel floor; the generator composes them and refuses `falco` next to `tetragon`. The server-side counterpart is `pillar.secops` owning the starter TracingPolicy pack and the Loki ruler detection rules; that is new work with no upstream equivalent and is called out in section 8.

### 2.11 Agent packaging

**Choice.** `features.yaml` → generated `config.alloy`, the k8s-monitoring chart pattern applied to hosts as well. The generator is the `fleet-compose` component of the `fleet` plugin; agent-side feature and sensor rows contribute fragments. Kubernetes layout: the k8s-monitoring chart with alloy-operator, logs-collector DaemonSet, receiver DaemonSet, privileged DaemonSet (eBPF), clustered metrics StatefulSet, events singleton, plus the Tetragon DaemonSet and optional `obi-k8s-cache`; enrolment once per cluster by a pre-install Job producing a Secret. Hosts: two systemd units (`alloy`, `tetragon`), a 0600 client secret, and a local `config.alloy` holding only the `remotecfg` block; everything else is served. Windows: `prometheus.exporter.windows` and `loki.source.windowsevent` only, MSI/Chocolatey install, no eBPF, network or runtime-security telemetry.

**Evidence.** `datadog-agent.md §3` (install paths), §7 items 1, 2, 4, 5 (one package many processes; tiny customer-facing surface; fleet server is the biggest missing piece; Windows parity is structurally hard). `grafana-stack.md §3.8` (collector presets and the DaemonSet + StatefulSet + singleton layout). `gap-tenant-gateway-and-agent-enrolment.md §1.3` (the enrolment flow and the 15-line local config). `gap-ebpf-sensor-bundle-and-kernel-floor.md §6`. `gap-remotecfg-server-build-vs-adopt.md §4.5` (`fleet-validate` with pinned Alloy binaries).

**Purist shape.** An agent profile is composed exactly like a server profile: `agent-base` bundle, then `agent-k8s` or `agent-host` or `agent-windows` mode bundle, then the tenant overlay (feature toggles), then per-collector attributes from `remotecfg`. `aiopsctl agent render --profile agent-host --dump` prints the composed `config.alloy` with provenance comments; `aiopsctl agent check` runs `alloy validate` with the binary pinned to the collector's reported version. The customer never edits Alloy syntax.

### 2.12 Pillar scope for MVP

**Choice.** All seven pillars exist as plugin categories in the SDK and profile vocabulary from day one (`pillar.<name>` is a row type like any other), but the MVP ships real machinery for Incident, Change and Compliance, plus chat-ops and on-call. FinOps (OpenCost, krr), SecOps (Tetragon, Trivy Operator, Kubescape, Prowler), Continuous Improvement (Sloth/Pyrra SLOs, profile regressions) and Executive Reporting (image renderer plus templates, scheduled PDF) ship later as plugins with real inputs. The loader refuses a `pillar.*` row whose manifest declares no `requires` on a `datasource`, `tool` or `signal-ingest` point: a prompt is not a capability.

**Requirements and public inputs.** The canonical record §2.1 row 12 phases Incident, Change and Compliance first and requires a pillar to have real signal or tool inputs. `oncall-and-aiops-oss.md §B.10` surveys public tools for each pillar.

### 2.13 LLM access

**Choice.** Per-tenant budgets, rate limits, model routing and cost booking live in the kernel's `llm` gateway service, backed by its own Postgres tables. Providers are plugins: `llm.provider.openai-compatible`, `anthropic`, `bedrock`, `ollama-vllm`, and `litellm` for sites that already run a LiteLLM proxy. LiteLLM is not embedded in the kernel. The gateway also exposes an OpenAI-compatible endpoint per tenant so out-of-process investigators (HolmesGPT) and any customer tool go through the same budget and audit chokepoint; plugins never hold provider credentials.

**Evidence.** `plugin-architecture.md §2.5` (LiteLLM budget features; "plugins never hold LLM credentials"; the contract is identical either way) and §5 Q3 (open question). `oncall-and-aiops-oss.md §B.8` (LiteLLM v1.106.4, MIT core with an `enterprise/` directory, now a Rust core with Python SDK). The canonical record §2.1 row 13 governs current model access, metering and budgets.

**Why in-kernel.** Budgets must be enforced where approvals and audit already are, per `(tenant, plugin, user)`, in the same transaction as the `llm.request` event; an embedded LiteLLM would be a second multi-tenant system with its own Postgres and a second place where tenant identity can be wrong. The cost, a few thousand lines for budgets, retries, routing and usage accounting, is accepted; the `litellm` provider row keeps the 100-plus provider catalogue available without owning it. `llm.provider.replay` (recorded transcripts) is the test provider every investigator conformance run uses.

### 2.14 Core contracts every executor plugin must honour

**Requirements.** The following execution obligations are governed by the canonical record §2.2. This historical proposal placed checks at a kernel host boundary; the current record places them in bound policy providers and dispatcher guards (§4.3). A required conformance suite is not evidence that it has been implemented or run.

| Contract | Where enforced | How a plugin sees it |
|---|---|---|
| Approval floor: read verbs free, writes pause; tenant policy can only add gates | `plugin-host` tool dispatcher classifies every `tool.Invoke` as read or write using the tool's declared `Describe().Effects`; a write never runs without an `approval.Decision` id in the call context | `tool` implementations declare effects honestly; an undeclared write is a conformance failure (the fake host executes the tool against a recording sandbox and diffs state) |
| Write-role credential invisibility | `secrets` hands write-scoped secrets only to `remediation-executor` rows and only inside a `ChangeRecord` context; investigators' `ToolSet` is rendered from read-role datasources | A `remediation-executor` receives credentials by reference from the host at `Apply`; it never sees them at `Plan` |
| Change ledger before write | `change-hub` mints the `ChangeRecord`, captures pre-state and synthesises rollback, then calls `Apply` with a kernel-signed `ChangeToken`; the `remote.remediation-executor` adapter rejects any `Apply` without a valid token | `Plan(ctx, tenant, Action) → Plan{steps, rollback, blast_radius}` must be side-effect free; `Apply(ctx, tenant, Plan, ChangeToken) → Receipt`; `Rollback(ctx, tenant, Receipt)`; `Apply` twice with the same token is a no-op |
| Risk tiers L0 to L3 with distinct-approver quorum | `approval` service, keyed on the stable platform user id, L3 requires two distinct `approvals.decide` holders neither of whom requested, re-checked at dispatch; tier follows blast radius of the fix | `approval-answerer` surfaces (Slack, Grafana) only produce a `Decision`; quorum is never counted in a surface |
| Audit chain | `audit` host service: per-tenant sha256 hash chain, append-only, sealed asynchronously, exportable; every `Decision`, `ChangeRecord`, `Receipt`, acted-upon `Finding`, notification and LLM request is a link | Plugins write audit through `host.Audit`; direct table access is not granted to plugin schemas |
| PII scrub at chokepoints | `gateway` egress to Slack and Grafana responses, the `llm` request path, the outbox writer for external sinks, `casefile.artifact` creation; salted, correlation-preserving pseudonyms | Plugins receive scrubbed tool output when the tenant enables it and cannot disable it |
| Typed execution plans | `change-hub` accepts only `Plan` objects compiled against a server-confirmed scope and binds the approval to a digest over plan, target and inputs | No free-form shell reaches an executor from a model |
| Verification before close | `verifier` chain runs the cost ladder (deterministic rungs first, one paid agent turn last); `failed` and `uncertain` are distinct verdicts; failure feeds the mandatory-rollback sweep | A pillar cannot resolve an incident it remediated without a verifier verdict |

**Authority.** The canonical record §2.2 establishes these first-party requirements and §4.3 establishes the service and upstream boundaries. `gap-slack-chatops-tenancy-and-runtime.md §5` sketches how a chat surface supplies identity and decisions without owning quorum or execution.

---

## 3. Container inventory

Presence columns: H = hosted multi-tenant, S = self-host-full (compose or Helm), A = aiops-only, N = single-node (demo, dev, tiny estates). "opt" means optional row, "per-tenant" means one instance per tenant.

| Container | Technology | Responsibility | H | S | A | N |
|---|---|---|---|---|---|---|
| `aiops` kernel | Go, one binary, profile-booted | Loader, plugin host, store, events, scheduler, API server, in-process plugins (ingest, correlator, change-hub, casefile, approval, audit, notifier, chat-surface, llm gateway, tenant, fleet, auth, limits renderers, datasources) | yes (N replicas) | yes | yes | yes (the only required container) |
| `aiops-auth` | same binary, `aiops plugin serve auth` | `/oauth2/token`, JWKS; separately scaled because it is on the ingest SLO | yes | opt | no | in-process |
| `tenant-gateway` | Envoy (`gateway.envoy` renders config) | Verifies agent and service JWTs, injects `X-Scope-OrgID`, enforces route table and body limits in front of the four backends and the fleet API | yes | yes (default) | no | in-process `gateway.inproc` |
| Metrics backend | Mimir (AGPL, separate service) | Metrics storage and ruler/alertmanager | microservices with Kafka | monolithic | no (customer's own) | monolithic |
| Logs backend | Loki (AGPL) | Logs storage, ruler (SecOps detections) | microservices | monolithic | no | monolithic |
| Traces backend | Tempo (AGPL) | Traces, service graph metrics | microservices with Kafka | monolithic | no | monolithic |
| Profiles backend | Pyroscope (AGPL) | Continuous profiles | microservices | single binary | no | single binary |
| Object storage | S3 or MinIO | Backend blocks; kernel `store.blob` prefix per tenant | S3 | MinIO | opt | local dir |
| Kernel database | PostgreSQL 16+ | `store.sql`, `store.kv`, River, outbox | HA | one container | one container | SQLite file |
| Dashboards | Grafana OSS (AGPL) + `aiops-app` plugin | Tenant UI (org per tenant), alerting rules, annotations; app plugin is a thin proxy to the kernel | shared pools; opt instance per tenant | yes | opt (customer's own, app plugin installed) | yes |
| `investigator-holmes` | Python, HolmesGPT, gRPC adapter sidecar | Investigation runtime for the incident pillar and chat `ask` | per-tenant | one shared | one shared | one shared (opt) |
| `oncall-goalert` | GoAlert (Go) + its Postgres database | Schedules, escalation, paging behind `on-call-provider.goalert` | per-tenant | one | opt | no (`on-call-provider.none`) |
| `pillar-*` sidecars | Python or Go, gRPC | Later pillars that need Python tooling (krr, Prowler wrappers) | per-tenant or shared per manifest | opt | opt | opt |
| `report-renderer-pdf` | headless Chromium, gRPC | PDF rendering for executive reports (phase 2) | opt | opt | opt | no |
| `events-nats` | NATS JetStream | Low-latency fan-out across kernel replicas, Slack surface, Grafana Live | opt | no | no | no |
| `sink-kafka` target | customer's Kafka | Firehose export of `incident/*`, `change/*`, `finding/*` events | opt | opt | opt | no |
| Slack | external | Chat-ops surface; Events API (H) or Socket Mode (S, A, N) | yes | yes | yes | opt |
| Customer agent: `aiops-agent` | Alloy v1.21.x unmodified; DaemonSet or systemd unit | Metrics, logs, OTLP receiver, `beyla.ebpf`, `pyroscope.ebpf`; local config is one `remotecfg` block | customer side | customer side | n/a (customer's own collectors) | local demo |
| Customer agent: `aiops-sensor` | Tetragon DaemonSet or systemd unit + tetragon-operator | Runtime security events to Loki | customer side, Tier A/B only | same | n/a | opt |
| Customer agent: `obi-k8s-cache` | Deployment, 1 per 50 nodes | Kubernetes metadata cache for Beyla | customer side (large clusters) | same | n/a | no |
| `fleet-hostagent` | small Go binary (phase 2) | Remote upgrade and support bundle on VMs | customer side, opt | opt | n/a | no |
| `aiopsctl` | Go CLI | `profile render`, `plugin check`, `agent render`, `ui install/sign`, `build --manifest` | operators and contributors | same | same | same |

The AGPL boundary: the four backends and Grafana are always separate processes reached over HTTP; no kernel package imports `github.com/grafana/mimir/pkg/...`, `.../loki/pkg/logql` or `.../tempo/pkg/traceql`; the Apache-2.0 sub-trees (Loki `pkg/push`, Tempo `pkg/tempopb`, the plugin SDK, Scenes) may be imported (`grafana-stack.md §13.1`).

---

## 4. Extension points

Rule from Backstage, adopted: many extension points with few methods (`plugin-architecture.md §2.4`). Each point is `sdk/ext/<point>` (Go interface) plus `proto/aiops/ext/<point>/v1`; the row type in a profile is `<point>.<name>`. Every point has a conformance directory `conformance/<point>/` whose tests run over the in-process implementation and over the `remote.<point>` adapter.

| Point id | Purpose | MVP implementations | Runtime | Conformance highlights |
|---|---|---|---|---|
| `datasource` | `Query(ctx, tenant, QueryRequest) → Frames`; `Capabilities()` | `promql` (Mimir or any Prometheus API), `logql` (Loki), `traceql` (Tempo), `profiles` (Pyroscope), `dashboards` (Grafana HTTP API), `kubernetes`; phase 2: `datadog`, `elasticsearch`, `cloudwatch` for `aiops-only` | inproc | tenant isolation (never leaks another tenant's header), empty result, timeout, cancellation, 429 passthrough |
| `signal-ingest` | Registers an inbound endpoint, parses one payload family, writes raw group plus normalised alerts, emits `signal.received` | `alertmanager` (v4 payload), `unified-alerting` (v1 payload, HMAC), `generic-json`; phase 2: `cloudevents`, `pagerduty-inbound`, `robusta` | inproc | ack ≤ 250 ms after durable write, idempotency on retries, per-tenant bucket → 429 with `Retry-After`, signature rejection, oversize body |
| `correlator` | `Correlate(ctx, tenant, []Signal) → []Candidate`; sync rules in the ingest transaction, fold rules as jobs | `fingerprint`, `service-key`, `topology-fold`, `change-window-fold`, `shared-alertname-fold`, `storm`; phase 2: `llm-rerank` (advisory) | inproc; `llm-rerank` grpc | deterministic rules replay from fixtures with identical output; storm threshold; re-root when a cause arrives late |
| `investigator` | `Investigate(ctx, tenant, Incident, ToolSet) → stream Finding`; `Ask(...)` for chat | `holmes`; `ensemble` (wrapper, phase 2); `native` (phase 2, only on trigger) | grpc (`holmes`), inproc (`ensemble`) | SSE/stream contract from cassettes; pauses on `ApprovalRequired`; resumes from transcript; never executes a write without a decision id |
| `tool` | `Describe() → ToolSpec{name, params, effects, prerequisites}`; `Invoke(ctx, tenant, args) → Result` | `promql-query`, `logql-query`, `traceql-search`, `profiles-query`, `kubectl-read`, `mcp-bridge` (any MCP server, including mcp-grafana), `runbook-fetch`, `change-lookup`, `service-graph`; phase 2: `krr`, `trivy-reports`, `opencost` | inproc, grpc, wasm (phase 3) | declared effects match observed effects under the recording sandbox; prerequisites decide enablement; output cap; PII scrub applied when enabled |
| `verifier` | `Verify(ctx, tenant, Incident, Receipt) → Verdict{cleared, still_present, uncertain, failed}`; ordered by cost | `alert-cleared` ($0), `typed-readback` ($0), `cloud-alarm-state` ($0), `agent-turn` (one paid read-only turn) | inproc; `agent-turn` uses `investigator` | cheaper rungs run first; `failed` ≠ `uncertain`; exponential backoff on repeated `uncertain` |
| `remediation-executor` | `Plan`, `Apply(…, ChangeToken)`, `Rollback`; idempotent | `kubernetes-apply` (rollout restart, scale, patch), `runbook-http`; phase 2: `argo-rollouts`, `aws-ssm`, `ansible`, `gitops-pr` | grpc, `instancing: per-tenant` | `Plan` is side-effect free; `Apply` refuses without a valid token; `Apply` twice is a no-op; `Rollback` after partial `Apply`; credentials absent at `Plan` |
| `approval-policy` | `Evaluate(ctx, tenant, Action, Context) → Decision{allow, deny, require(tier, approvers)}` | `always-ask`, `tier-table` (L0 to L3 from blast radius), `business-hours`, `change-freeze`; phase 3: `cel-wasm` | inproc; wasm later | policy can only add gates over the floor; L3 requires two distinct holders; freeze windows |
| `approval-answerer` | Surfaces that collect a human `Decision` | `slack` (Block Kit), `dashboards-app` (resource call with verified `X-Grafana-Id`), `api` (OIDC bearer) | inproc | decision carries a verified platform user id; first decision wins; idempotent on `(approval_id, action_ts)`; fails closed when no answerer is composed |
| `notifier` | `Send(ctx, tenant, Notification) → DeliveryID`; `Capabilities()` (threads, buttons, ack) | `slack`, `email`, `webhook`; phase 2: `teams`, `sms-twilio`, `push-ntfy` | inproc | idempotent resend, thread reply, digest window per target (one message per interval, P1 first) |
| `chat-surface` | `Serve(ctx, host)` registering inbound events; `Reply`; maps chat identity to tenant and user | `slack-events`, `slack-socket-mode`; phase 2: `teams`, `mattermost` | inproc (Slack); grpc allowed | envelope normalisation, 3 s ack, retry dedup, external-user rejection, `authorizations[0]` routing, thread stickiness |
| `on-call-provider` | `Schedules`, `WhoIsOnCall`, `Page`, `Ack`, `Resolve`, `Overrides`; optional `Heartbeat` | `goalert`, `pagerduty`, `opsgenie`, `irm`, `none`; phase 2: `builtin` | inproc adapters; GoAlert is a sidecar | against `goalert/demo`: page, ack, resolve round trip; `WhoIsOnCall` cache TTL; optional ops declared in `conformance.yml` |
| `scheduler` | `Schedule(ctx, tenant, JobSpec)`, `Cancel`; leader-elected periodic jobs | `river-postgres`, `river-sqlite`; optional `kubernetes-cronjob`, `temporal` | inproc | unique jobs, snooze, cancel, leader hand-over, per-tenant bucket |
| `store` | `SQL` (per-plugin schema and migrations), `KV`, `Blob` | `postgres`, `sqlite`, `s3-blob`, `local-blob` | inproc | migrations up and down, tenant column enforced, RLS fence (hosted), blob prefix per tenant |
| `events` | Outbox writer, doorbell, subscriber API; optional fan-out | `outbox-postgres`, `outbox-sqlite`; optional `nats`; `sink.kafka` | inproc | event visible only after commit; at-least-once with idempotent consumers; ordering per `(tenant, seq)` |
| `secrets` | `Get/Put(tenant, plugin, name)` with envelope encryption | `postgres-envelope`; phase 2: `vault`, `kms` | inproc | a plugin cannot read another plugin's secret; write-scoped secrets only inside a `ChangeRecord` context |
| `llm.provider` | `Complete`, `Stream`, `Embed`, `Models()` behind the kernel `llm` gateway | `openai-compatible`, `anthropic`, `bedrock`, `ollama-vllm`, `litellm`, `replay` (test) | inproc | budget decrement in the same transaction as the request event; retry classification; `replay` strict mode fails on unmatched requests |
| `report-renderer` | `Render(ctx, tenant, ReportSpec, Data) → Artifact` | `markdown`, `html`; phase 2: `pdf` (Chromium sidecar), `panel-image` (image renderer) | inproc; `pdf` grpc | deterministic output for fixed data; artifact stored via `store.blob` with scrub |
| `gateway` | `Render(RouteTable, JWKS) → artifact`; for `inproc`, serve it | `envoy`, `inproc` | inproc (renders) | strip and inject `X-Scope-OrgID`, scope per route, no pipe on writes, fail closed, body limits, `Retry-After` passthrough |
| `limits-renderer` | `Render(TenantLimits) → backend overrides file` | `metrics`, `logs`, `traces`, `profiles` | inproc | complete tenant block emitted (Tempo does not merge), validated before write, drift detection read-back |
| `fleet.config-source` | A plugin contributes an agent pipeline fragment with matchers | `base-telemetry`, `sensor-beyla`, `sensor-profiler`, `sensor-tetragon`; phase 2: `secops-policies`, `finops-opencost` | inproc | fragments compose into a valid `config.alloy` for every Tier (A, B, C) |
| `fleet.validation-gate` | Reject or accept a composed agent config | `syntax`, `alloy-validate` (pinned binaries), `schema-per-version` | inproc | invalid configs never reach `served_configs` |
| `agent.feature` and `agent.sensor` | Agent-side rows in the `agent-*` profiles (feature toggles and sensors with capability and kernel-floor declarations) | `host-metrics`, `host-logs`, `k8s-metrics`, `k8s-logs`, `k8s-events`, `otlp-receiver`, `windows-metrics`, `windows-events`, `beyla`, `profiler`, `tetragon`, `falco` | n/a (data rows consumed by `fleet-compose`) | mutual exclusion (`falco` vs `tetragon`), Tier C drops eBPF rows, capability list matches the research tables |

**Host services a plugin may `require`** (`plugin-architecture.md §4.4`, extended): `logger`, `metrics` (pre-labelled with plugin id and tenant), `tracer`, `config`, `secrets`, `tenant` (current tenant from request or job context), `store.sql`, `store.kv`, `store.blob`, `scheduler`, `events`, `http.router` (plugin-prefixed), `llm` (budgeted gateway), `approval`, `audit`, `change-hub`, `pii`, `discovery`, `lifecycle`.

**Plugin test kit** (`sdk/plugintest` in Go, `aiops_plugin_sdk.testing` in Python, same fixtures directory): `NewFakeHost` (recording logger, readable metrics registry, in-memory SQL via SQLite, fake scheduler with manual clock, fake events, fake secrets, scripted `llm`, fake `tenant`); `CheckManifest`; `Run(t, plugin, cfg, fixtures)` drives the full lifecycle and asserts that `Shutdown` unwinds every registration and that `Shutdown` before `Start` is safe; strict-replay recorded fixtures with `AIOPS_ACC=1` for record mode against testcontainers; `aiopsctl plugin check ./` runs manifest, schema, conformance and signature checks with the SDK module as the only dependency (`plugin-architecture.md §4.7`).

**Testing tiers** (from `dsh-architecture.md §5.3`): bare fake-host unit mount → per-point conformance in all runtimes → real-composition smoke booting a test-only profile from the built binary (`loader-smoke`) → recorded-session replay (`llm.provider.replay`, Slack and Alertmanager fixtures, `AIOPS_SNAPSHOT=replay` enforced in CI) → real-API end-to-end that self-skips without keys → the startup failure matrix.

---

## 5. Tenancy and data model

### 5.1 Tenant identity, one string everywhere

The tenant id `[a-z0-9][a-z0-9-]{0,62}` is simultaneously the `X-Scope-OrgID` value for all four backends, a valid Kubernetes namespace and DNS label, the `tenant_id` column on every kernel row, the `secrets` scope, the River queue prefix and the `store.blob` prefix. Mappings the kernel keeps: tenant → Grafana `(url, org_id, service_account_token)`; tenant → Slack installations (several allowed) and channel bindings; tenant → agent clients; tenant → `TenantLimits` and `plan`. Nothing derives the tenant from a payload: Slack routing uses the installation record, Grafana uses app-instance settings, agents use the JWT claim, webhooks use the per-tenant token or HMAC.

### 5.2 What the kernel stores (Postgres or SQLite)

| Schema (owner plugin) | Tables (abridged) | Notes |
|---|---|---|
| `admin` (kernel) | `tenant`, `tenant_limits`, `plan`, `binding(tenant, subject, role)`, `platform_user`, `api_token`, `profile_overlay`, `plugin_release`, `plugin_schema` | Bindings above the Grafana basic-role floor live here; current authorization requirements are in the canonical record §2.2 |
| `auth` | `agent_client` (Argon2id secret hash, status), `bootstrap_token`, `service_token`, `signing_key`, `jti_denylist` | JWTs are not stored; revocation is "stop issuing" (`gap-tenant-gateway-and-agent-enrolment.md §1.4`) |
| `fleet` | `agent`, `agent_status`, `effective_config`, `pipeline`, `pipeline_revision`, `served_config`, `rollout` | Inventory and config history, the Datadog Fleet Automation features Alloy lacks (`gap-remotecfg-server-build-vs-adopt.md §4`) |
| `tenant` | `limits_render`, `limits_drift`, `delete_saga_step` | The create and delete sagas with per-step status (`gap-tenant-gateway-and-agent-enrolment.md §5.3`) |
| `signal` | `raw_group`, `alert`, `alert_label_map`, `severity_map`, `criticality_map` | Idempotency keys as in D4 |
| `casefile` | `incident`, `incident_event` (timeline), `investigation`, `investigation_step` (transcript), `approval_request`, `approval_decision`, `artifact`, `problem_record`, `postmortem` | Append-only events with projections; the Slack thread and the Grafana timeline are projections |
| `change` | `change_record`, `change_plan`, `change_receipt`, `rollback`, `change_window`, `freeze`, `external_change` (Argo, Flux, CloudTrail harvest) | Ledger minted before any write |
| `compliance` | `framework_row`, `control`, `control_result`, `attestation`, `finding`, `risk_decision`, `posture_snapshot` | Scanner findings normalised to one `Finding` schema shared with SecOps |
| `chat` | `slack_installation`, `slack_installation_workspace`, `slack_channel_binding`, `slack_user_link`, `slack_thread_session`, `slack_interaction_log` | From `gap-slack-chatops-tenancy-and-runtime.md §4`, §5, §9 |
| `oncall` | `provider_binding`, `page`, `who_is_on_call_cache` | GoAlert keeps its own database; the kernel keeps references |
| `llm` | `budget`, `usage`, `model_route`, `model_tier`, `benchmark_score` | Budgets per `(tenant, plugin)`; usage per request |
| `audit` | `audit_event`, `audit_chain_head` | Per-tenant hash chain; ORM and trigger guards block update and delete |
| `events` | `outbox`, River tables (`river_job`, `river_leader`, `river_queue`) | One transaction for state change, event and job |
| `secrets` | `secret` (envelope-encrypted blob, scope `(tenant, plugin, name)`) | KMS provider later |

Every table carries `tenant_id`; the hosted profile enables row-level security keyed on a session variable the `tenant` host service sets, as a second fence behind the application scope. Each plugin ships its own migrations; the kernel runs them in dependency order at boot and refuses to start a row whose migrations failed.

### 5.3 What lives in the observability backends, never in the kernel

Metrics, logs, traces and profiles from agents; alert state history (routed to the tenant's logs backend through the gateway, not Grafana's instance-level setting); Tetragon and Falco events (logs backend, with SecOps detections as ruler rules); OpenCost and krr outputs (metrics backend); Kubernetes PolicyReports and Trivy reports (metrics and logs via the k8s collectors); SLO recording rules (Sloth into the metrics ruler); Grafana annotations for every `ChangeRecord` and incident open/resolve; dashboards and folders in the tenant's Grafana org. The kernel stores references (query, time range, annotation id), never copies. The control plane does not implement a telemetry store (canonical record §1).

### 5.4 Per-tenant composition

The tenant overlay is the last profile layer (`plugin-architecture.md §4.6`): `tenants/<id>/aiops.patch.yml` may enable or disable rows and replace `config` for rows whose manifest says `tenantScope: per-tenant` (pillar agents, approval policies, notifiers, chat bindings, investigator model choice, agent feature toggles). Rows with `tenantScope: global` (gateway, store, scheduler, fleet, auth, llm providers) cannot be patched per tenant. `aiopsctl profile render --tenant acme --dump` shows the effective tree.

### 5.5 Isolation and lifecycle

Hosted: per-tenant `investigator`, `remediation-executor` and GoAlert instances supervised by `plugin-host` with `instancing: per-tenant`; optionally gVisor `RuntimeClass` for sidecars; secrets scoped `(tenant, plugin, name)`; LLM budgets per tenant; Slack and Grafana rate limits are per workspace and per org already. Tenant create is cheap (limits files, Grafana org and data sources, first bootstrap token). Tenant delete is the saga in `gap-tenant-gateway-and-agent-enrolment.md §5.3`: stop issuing tokens, remove ruler and alertmanager configs, request deletion on the metrics backend, retention-to-1h on logs and traces, object-storage prefix sweeps, verify, then Grafana org and fleet records. The honest limit: profiles cannot be erased per tenant before compaction, so the hosted terms must say "best effort until compaction" or regulated tenants get a dedicated profiles bucket.

---

## 6. Profiles

### 6.1 Bundles

A bundle is a manifest that lists rows (`aiops.bundle.patch`), nothing else. Later bundles override earlier ones by row id; a patch replaces the whole `config`.

| Bundle | Contributes |
|---|---|
| `aiops-base` | `loader`, `plugin-host`, `store.*`, `events.outbox-*`, `scheduler.river-*`, `secrets.postgres-envelope`, `api-server`, `admin`, `audit`, `pii`, `casefile`, `change-hub`, `approval` + `approval-policy.tier-table`, `llm` gateway, `verifier.*` ($0 rungs), `correlator.*` (deterministic), `signal-ingest.{alertmanager,unified-alerting,generic-json}`, `notifier.{email,webhook}`, `report-renderer.{markdown,html}`, `plugin-manager` (never self-removable) |
| `aiops-obs-backends` | `datasource.{promql,logql,traceql,profiles,dashboards}`, `limits-renderer.{metrics,logs,traces,profiles}`, `tenant`, `fleet` (+ `fleet.config-source.base-telemetry`, sensors, validation gates), `auth`, `tool.{promql-query,logql-query,traceql-search,profiles-query,service-graph}` |
| `aiops-hosted` | `gateway.envoy`, `auth` as standalone process, `events.nats` (opt), `chat-surface.slack-events`, per-tenant `instancing` overrides for `investigator.holmes`, `remediation-executor.*`, `on-call-provider.goalert`; RLS on; hosted hot-path quotas |
| `aiops-selfhost` | `gateway.envoy` (default; one-row patch to `gateway.inproc`), `chat-surface.slack-socket-mode`, shared sidecar instancing, single tenant `default` |
| `aiops-single-node` | `store.sqlite`, `scheduler.river-sqlite`, `events.outbox-sqlite`, `gateway.inproc`, `on-call-provider.none`, `chat-surface.slack-socket-mode` (opt), `llm.provider.ollama-vllm` default |
| `aiops-chatops-slack` | `chat-surface.slack-*` handler core rows, `notifier.slack`, `approval-answerer.slack`, `/aiops` commands |
| `aiops-oncall-goalert` | `on-call-provider.goalert` + sidecar spec, `tool.oncall-who` |
| `aiops-pillars-mvp` | `pillar.incident` (+ `investigator.holmes`, `tool.{kubectl-read,mcp-bridge,runbook-fetch,change-lookup}`, `remediation-executor.{kubernetes-apply,runbook-http}`, `verifier.agent-turn`), `pillar.change` (external change harvest from Argo/Flux/CloudTrail/git, annotations, change-to-incident correlation), `pillar.compliance` (first-party catalogue wording, PolicyReport and Kubescape inputs, scanner findings) |
| `aiops-pillars-ext` (phase 2+) | `pillar.finops` (OpenCost, krr), `pillar.secops` (Tetragon policies and ruler rules, Trivy, Kubescape, Prowler), `pillar.improve` (Sloth/Pyrra SLOs, profile regressions), `pillar.report` (templates, scheduler, `report-renderer.pdf`) |
| `aiops-external-datasources` | `datasource.{datadog,elasticsearch,cloudwatch,prometheus-generic}`, `signal-ingest.{pagerduty-inbound,cloudevents}` for `aiops-only` |
| `agent-base` | `agent.feature.{host-metrics,host-logs,otlp-receiver}`, `agent.sensor.{beyla,profiler}` (Tier-gated), self-monitoring pipeline, `remotecfg` skeleton |
| `agent-k8s` | `agent.feature.{k8s-metrics,k8s-logs,k8s-events}`, collector layout (logs DaemonSet, receiver DaemonSet, privileged DaemonSet, clustered metrics StatefulSet, events singleton), `agent.sensor.tetragon`, `obi-k8s-cache` for > 50 nodes, cluster-level enrolment Job |
| `agent-host` | two systemd units, 0600 secret, Tier detection, `agent.sensor.tetragon` |
| `agent-windows` | `agent.feature.{windows-metrics,windows-events}` only; MSI/Chocolatey |

### 6.2 Shipped profiles

| Profile | Bundles (in order) | Who | What it proves |
|---|---|---|---|
| `hosted` | `aiops-base`, `aiops-obs-backends`, `aiops-chatops-slack`, `aiops-oncall-goalert`, `aiops-pillars-mvp`, `aiops-pillars-ext`, `aiops-hosted` | us, Kubernetes, N kernel replicas, microservice backends with Kafka | Multi-tenant isolation, per-tenant sidecars, Envoy gateway, Events API Slack |
| `self-host-full` | `aiops-base`, `aiops-obs-backends`, `aiops-chatops-slack`, `aiops-oncall-goalert`, `aiops-pillars-mvp`, `aiops-selfhost` | customers, compose or Helm, monolithic backends, one Postgres, MinIO | The same rows as hosted minus multi-tenancy; one `default` tenant |
| `aiops-only` | `aiops-base`, `aiops-external-datasources`, `aiops-chatops-slack`, `aiops-pillars-mvp`, `aiops-selfhost` (+ opt `aiops-oncall-goalert`) | customers on Datadog or their own Prometheus who want only the AIOps layer | The kernel runs with no agent, no gateway, no Grafana; datasources are the only backend contact |
| `single-node` | `aiops-base`, `aiops-obs-backends`, `aiops-pillars-mvp`, `aiops-single-node` | demos, development, tiny estates, the conformance bench | One binary, one SQLite file, one local Alloy; the purity test: every row must activate here or declare why not |
| `agent-host` | `agent-base`, `agent-host` | customer VMs and bare metal | `features.yaml` → `config.alloy` with Tier detection |
| `agent-k8s` | `agent-base`, `agent-k8s` | customer clusters | Collector layout plus Tetragon DaemonSet, one enrolment per cluster |
| `agent-windows` | `agent-base`, `agent-windows` | Windows servers | Metrics and event logs only |

### 6.3 Composition order (precedence, lowest to highest)

1. Empty root.
2. Each bundle's patch in profile order.
3. Profile user layer `$AIOPS_HOME/profiles/<name>/aiops.patch.yml`.
4. Site layer `$AIOPS_HOME/aiops.patch.yml`.
5. `--patch <file>` overlays in argv order (never persisted).
6. Tenant overlay `tenants/<id>/aiops.patch.yml` (per-tenant rows only).
7. Environment kill switches (`AIOPS_TELEMETRY_DISABLED`, `AIOPS_AGENTS_PAUSED`) that patch `disabled` and cannot be undone by config.

Semantics copied from `dsh-architecture.md §3.2`: later layers win per row; a patch targets a row by id and replaces its whole `config`; `insert` adds rows; an unknown id warns; an empty patch file fails boot (disable a layer with `[]`). `aiopsctl profile render --dump` prints rows grouped by the bundle that introduced them and the layer that last patched them; `--dump-config-schema` emits one JSON Schema for the whole composition from each plugin's `configSchema`, which feeds the admin UI and the docs.

---

## 7. MVP scope and phased roadmap

### 7.1 MVP (what proves the model, nothing else)

| Area | In MVP | Deferred |
|---|---|---|
| Kernel | loader, plugin-host (inproc + `remote.<point>`), lifecycle, failure matrix, `store`/`events`/`scheduler` on Postgres and SQLite, `secrets`, `audit` chain, `pii`, `llm` gateway with budgets and three providers plus `replay`, `admin`, `api-server`, profile composition with dump and schema dump | Wasm runtime, `events.nats`, `sink.kafka`, KMS secrets |
| SDK and tests | `sdk/ext/*` for the 22 points in section 4, protos, Go and Python SDKs, `plugintest` fake host, conformance suites for every MVP point in both runtimes, `loader-smoke`, `aiopsctl plugin check` | Wasm conformance |
| Fleet and ingest | `fleet` (three RPCs, compose, validate, revisions, health), `Enrol`, `auth`, `gateway.envoy` and `gateway.inproc`, `tenant` with four `limits-renderer`s and the delete saga, `agent-host`, `agent-k8s`, `agent-windows` profiles, Tetragon DaemonSet | OpAMP listener, `fleet-hostagent`, BTF mirror (never) |
| Incident loop | `signal-ingest.{alertmanager,unified-alerting,generic-json}`, deterministic `correlator.*` with storm mode, `casefile`, `investigator.holmes`, `tool.*` core set, `approval` + `tier-table`, `approval-answerer.{slack,dashboards-app,api}`, `change-hub`, `remediation-executor.{kubernetes-apply,runbook-http}`, `verifier.*` ladder, `notifier.{slack,email,webhook}` | `investigator.ensemble`, `investigator.native`, `correlator.llm-rerank` |
| Change and compliance | external change harvest (Argo, Flux, CloudTrail, git webhooks), Grafana annotations, change-to-incident correlation, freeze windows; first-party catalogue wording, PolicyReport and Kubescape inputs, scanner findings register | canary, pre-change snapshots and backups, CR document drafting |
| Chat-ops and on-call | `chat-surface.slack-events` and `slack-socket-mode`, one app, `/aiops` verbs, sessions and streaming, Block Kit approvals; `on-call-provider.{goalert,pagerduty,opsgenie,irm,none}` | Teams, Mattermost, native on-call |
| UI | `aiops-app` Grafana app plugin (thin proxy, Scenes pages for incidents, approvals, changes, compliance, fleet), unsigned and privately signed builds | community signature, instance-per-tenant tier automation |
| Profiles | `hosted`, `self-host-full`, `aiops-only`, `single-node`, the three agent profiles | |

### 7.2 Roadmap by quarter

| Quarter | Theme | Deliverables | Exit test |
|---|---|---|---|
| Q4 2026 (Nov to Dec) | Kernel and the install path | Kernel core, SDK, `plugintest`, composition and dumps, failure matrix; `fleet` three RPCs, `Enrol`, `auth`, both gateways, `tenant` limits renderers; `agent-host` and `agent-k8s` profiles; `single-node` profile | One `curl | sh` on a VM and one Helm install on a cluster ship metrics, logs, traces and profiles to a single-node stack with `X-Scope-OrgID`; every kernel row passes conformance in both runtimes |
| Q1 2027 | Incident loop end to end | `signal-ingest`, `correlator`, `casefile`, `investigator.holmes`, `tool` core set, `approval`, `change-hub`, `remediation-executor.kubernetes-apply`, `verifier` ladder, `notifier.slack`, `chat-surface.slack-*`, `on-call-provider.goalert`, `aiops-app` unsigned build, `self-host-full` profile | Alert → incident → Slack thread with HolmesGPT findings → approval in Slack → rollout restart under a `ChangeRecord` → verification → resolve, replayed from fixtures in CI and run live against the lab cluster; D4 latency targets measured |
| Q2 2027 | Change, compliance, hosted hardening; MVP GA | `pillar.change`, `pillar.compliance`, `aiops-only` profile with Datadog and generic Prometheus datasources, hosted per-tenant sidecars, RLS, delete saga, privately signed `aiops-app`, Events API Slack app submitted for Marketplace | First hosted tenants; a tenant deletion completes with verified prefix sweeps on three of four backends and the documented profiles caveat |
| Q3 2027 | Later pillars, fleet parity | `pillar.finops` (OpenCost, krr), `pillar.secops` (Tetragon starter policies and ruler rules, Trivy, Kubescape, Prowler), OpAMP listener, `fleet-hostagent`, `events.nats`, `investigator.ensemble` | Weekly FinOps and SecOps digests in Slack from real data; remote upgrade of a VM agent from the UI |
| Q4 2027 | Improvement, reporting, third-party runtime | `pillar.improve` (SLOs via Sloth, profile regressions), `pillar.report` (`report-renderer.pdf`, monthly executive PDF), Wasm runtime for `approval-policy` and `tool`, community plugin signature, native on-call only if a trigger fired | A third party publishes a `tool` plugin as an OCI artifact and it passes `aiopsctl plugin check` without a kernel checkout |

---

## 8. Top risks and what this proposal refuses to build

### 8.1 Risks

| # | Risk | Why it is real | Mitigation |
|---|---|---|---|
| R1 | The purity tax: every first-party plugin must pass conformance in-process and over gRPC, and every point is proto-first | Doubles conformance runtime; forbids convenient Go-only types; contributors may find the rule pedantic | Conformance runs are parallel and cached per point; the rule is what guarantees a third-party plugin has the same power as ours. If CI time becomes the complaint, the gRPC run can move to nightly for in-process rows, never dropped |
| R2 | HolmesGPT API drift and typed execution plans | Its HTTP API page is marked "previous version" (`gap-kernel-hot-path-and-grafana-ui-integration.md §7`); admin endpoints are unauthenticated; it may not carry typed plans | Cassette-based conformance pins the contract; per-tenant instancing in hosted; the exit criterion in 2.2 keeps the fallback local to one row |
| R3 | River SQLite is experimental | One writer, `SQLITE_BUSY` under contention (`gap-kernel-hot-path-and-grafana-ui-integration.md §2.4`) | `single-node` is demo, dev and tiny estates only; conformance runs on both drivers so the Postgres path is never untested |
| R4 | slack-go churn | Breaking changes in consecutive minors, volunteer project (`gap-slack-chatops-tenancy-and-runtime.md §8`) | Thin `slackapi` interface, pinned and vendored, examples in CI |
| R5 | Envoy in the self-host-full profile | One more container and config surface for compose users | Rendered by the plugin, conformance-tested; one-row patch to `gateway.inproc` documented with its drawback |
| R6 | GoAlert per tenant in hosted | N sidecars and N databases; GoAlert is single-tenant | It is the phase-2 native-module trigger; `WhoIsOnCall` cache keeps Slack fast; the interface keeps the swap local |
| R7 | In-kernel LLM gateway re-implements LiteLLM features | Budgets, retries, routing, usage accounting are a few thousand lines we own | `llm.provider.litellm` keeps the provider catalogue available; budgets at the audit chokepoint is the non-negotiable part |
| R8 | Grafana identity forwarding floor | The release that made `X-Grafana-Id` unconditional is UNVERIFIED (`gap-kernel-hot-path-and-grafana-ui-integration.md §6`) | The proxy tolerates a missing header and the kernel allows read and ingest only for `actor=plugin`; writes require a verified subject via Slack or OIDC |
| R9 | Tetragon detection content is new work | No upstream equivalent to `falco-rules`; `pillar.secops` must author the starter policy pack and ruler rules (`gap-ebpf-sensor-bundle-and-kernel-floor.md §8`) | Falco stays a one-row swap; SecOps is phase 3 so the pack has two quarters |
| R10 | Tenant erasure guarantees on profiles | Level-0 segments are multi-tenant objects until compaction | Terms of service wording, or a dedicated profiles bucket for regulated tenants |
| R11 | AGPL boundary discipline | One accidental import of a backend package changes the licence story | A CI check forbids the listed module paths; the allowed Apache-2.0 sub-trees are enumerated |
| R12 | Contributor barrier for Python pillars | A Python plugin author must run a gRPC conformance suite | `aiops_plugin_sdk.testing` wraps it; `aiopsctl plugin check` needs no kernel checkout; the HolmesGPT toolset YAML shape means most contributions are `tool` rows, not pillars |
| R13 | The fat default binary | Every in-process first-party plugin compiled in; well over 100 MB | Accepted for self-host simplicity; `aiopsctl build --manifest` exists for people who want a slim distribution |

### 8.2 What this proposal refuses to build

- A telemetry lakehouse or second telemetry store in the control plane (canonical record §1).
- A Cordis or Node kernel; a Python kernel; Go's native `plugin` package (`plugin-architecture.md §4.8`).
- Kafka or Redis as kernel dependencies; a second Kafka for the control plane (`gap-kernel-hot-path-and-grafana-ui-integration.md §2.3`).
- Deep-merge configuration semantics, code in manifests, or a general expression language in YAML (`dsh-architecture.md §3.2`, §8.2).
- A modified Grafana or AGPL backend build, or any import of their non-permissive packages (`grafana-stack.md §13`). Integrations use supported interfaces.
- A native on-call engine before one of the three triggers in 2.3 fires; a mobile push app (ntfy via webhook covers it); status pages.
- A BTF mirror, runtime eBPF compilation, kernel-module drivers, Windows eBPF, Fargate or Autopilot eBPF (`gap-ebpf-sensor-bundle-and-kernel-floor.md §5`, §7).
- Persona-only pillars with no signal or tool input (canonical record §2.1 row 12).
- Our own identity provider or realm-per-tenant provisioning; OIDC from the customer's or our shared IdP, Grafana basic roles as the floor (canonical record §1; `gap-kernel-hot-path-and-grafana-ui-integration.md §3.2`).
- A second product UI; Grafana app plugin plus Slack plus a minimal kernel admin page are the surfaces.
- Per-pillar Slack apps as the default; one app, pillar as routing (`gap-slack-chatops-tenancy-and-runtime.md §6`).
- Module hot reload in the server; config-only recomposition and rolling restarts (`dsh-architecture.md §8.2`).
- Adopting shepherd or any community fleet server as a dependency (`gap-remotecfg-server-build-vs-adopt.md §3`).
- An on-call provider that receives alert webhooks directly, with the single documented dead-man exception (2.5).

---

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
