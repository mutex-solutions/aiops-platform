# Architecture proposal: lazy, Grafana-native minimalist

Status: historical draft for the judge panel, 2026-10-27. One of three design alternatives; the [canonical decisions](../../architecture/00-decisions.md) supersede it. Targets and roadmap dates below are proposal assumptions, not measured results or current commitments. Executor contracts are requirements of the canonical record §2.2; upstream boundaries follow §4.3. Editorial preparation follows [ADR-056](../../adr/ADR-056-documentation-baseline.md), and the publication-rights gate remains open.

Angle: a senior engineer who has been paged at 3am and wants the fewest moving parts that can still be defended in front of a customer. Reuse the Grafana stack, HolmesGPT, GoAlert, Alertmanager, the k8s-monitoring chart and mcp-grafana. Write the least code that ships a credible MVP in months. Name what we refuse to build.

Every external fact below is taken from `docs/research/*.md`; the section is cited inline. Where this proposal departs from a research recommendation, it says so.

---

## 1. Principles

1. **Unmodified upstream, always.** Alloy, Mimir, Loki, Tempo, Pyroscope, the dashboard server, HolmesGPT, GoAlert and Envoy run as the vendor ships them. Our contribution to each is rendered configuration through supported interfaces, without changing core code or linking non-permissive packages. This is also what keeps us Apache-2.0 next to AGPL services (see docs/research/grafana-stack.md §13.1).
2. **One binary, one database.** The control plane is one Go executable (`aiops`) and one PostgreSQL. No Kafka, Redis, NATS, Celery or message broker in the kernel (see docs/research/gap-kernel-hot-path-and-grafana-ui-integration.md §2).
3. **The dashboard server is the UI.** We ship one app plugin that proxies to the kernel and Slack as the second surface. No portal SPA, no admin SPA, no second login (see gap-kernel-hot-path §3.1).
4. **The agent recommends, the kernel executes.** The LLM never holds a write credential and never executes a write. It investigates with read-role toolsets; humans click a typed action; the kernel's change hub runs it (canonical record §2.2: write-credential invisibility).
5. **Extension points are earned, not designed.** An extension point exists only where the MVP needs a second implementation or a process boundary today. Five in MVP (§4). Everything else is a Go package with one implementation and a `ponytail:` note naming the trigger for promoting it.
6. **Policy is kernel code.** Approval floor, risk tiers, change ledger, audit chain and PII scrub live in one kernel package that is the only caller of executors. A plugin cannot skip a contract it never sees (§2.14).
7. **Fail closed on tenancy, approvals and writes; degrade gracefully on everything else.** A missing approver answerer means no remediation, not auto-remediation. A failed investigator row means an incident without an RCA, not a dropped page.
8. **Operator-visible means logged.** Every alert, correlation decision, model answer, approval and execution is an append-only event in the same transaction as the state change (copied from dsh, see docs/research/dsh-architecture.md §8.1 point 9).
9. **Ship profiles, not features.** A deployment is a profile (an ordered list of YAML row layers over an empty root), and the only difference between hosted and self-hosted is which rows are composed (dsh-architecture.md §7, §8.1 points 3 and 4).
10. **Months, not years.** MVP-1 demo in one quarter, self-host-full GA in two, hosted GA in three (§7). Anything that threatens that is deferred, and the deferral is written down with its trigger.

---

## 2. Decisions

Each subsection takes one position on one of the fourteen open topics, with the evidence and what we refuse.

### 2.1 Kernel language and plugin mechanism

**Choice.** Go. One `aiops` binary with a compile-time registry: each row type registers itself from `init()`, the profile picks rows by `type`, and the kernel instantiates them. Composition semantics are dsh's: YAML rows `{id, type, required, config}`, ordered patch layers (bundle, profile, site, tenant overlay), whole-row config replace, no deep merge, `aiops config render --tenant <id>` dumps the effective tree, `aiops config schema` dumps what each row accepts.

In-process: everything we write (ingest, correlation, change hub, policy, approvals, audit, fleet, auth, tenant, Slack, on-call adapter, dashboard provisioning, the HolmesGPT client, the Kubernetes executor).

Out-of-process: only things that already are processes with their own stable API. HolmesGPT over HTTP/SSE, GoAlert over GraphQL, the dashboard server over its HTTP API, Envoy via rendered config, the Alloy fleet over Connect-RPC. We do not wrap any of them in our own gRPC proto.

Not in MVP: the generic gRPC-over-UDS "remote plugin" adapter, the Python plugin SDK, Wasm (Extism/wazero), the Kubernetes operator with CRDs.

**Evidence.** docs/research/plugin-architecture.md §3 scores the compile-time registry best on self-host simplicity, ingest performance and ecosystem fit, and the gRPC sidecar best on isolation and cross-language. The research recommends the union. This proposal takes only the first half for MVP because the one cross-language component we need (HolmesGPT) already exposes HTTP/SSE (gap-kernel-hot-path §4.1), so a second RPC layer on top of it is work with no consumer. dsh-architecture.md §8.2 confirms Go's native `plugin` package is unsuitable and that compile-in plus composition is the Go-native equivalent of Cordis. gap-kernel-hot-path §4.3 keeps hashicorp/go-plugin confined to the dashboard app backend, where the dashboard server is the host.

**Trigger to add the gRPC adapter.** The first second out-of-process implementation of any extension point (a first-party investigator, a Python compliance scanner, a third-party executor). The Go interface and a proto generated from it are a one-week job then; designing it now would be guessing at a contract nobody calls.

**Testability.** `aiops plugin check <row>` mounts one row against a fake host (recording logger, in-memory store, manual-clock scheduler, replay-strict HTTP fixtures) exactly as plugin-architecture.md §4.7 describes, minus the gRPC and Wasm legs.

### 2.2 Investigator runtime

**Choice.** HolmesGPT is the only investigator in MVP (`investigator.holmes`). The kernel drives `POST /api/chat` with `stream: true`, stores `conversation_history` as the investigation transcript after every event, books `token_count` events against the tenant budget, enforces a step cap and a per-run cost cap, and stops the run when either is hit.

Toolsets handed to HolmesGPT are read-only: Prometheus-API, Loki, Tempo and Kubernetes read access with read-role credentials. `enable_tool_approval` stays `false`. HolmesGPT's own approval flow is not used for writes because the agent never writes (principle 4). Remediation is a separate kernel flow (§2.14).

Verification requirements come from the canonical record §2.2: check alert clearance and typed read-back before close; keep `failed` distinct from `uncertain`. Correlation follows the current signal and identity contract in §2.1, not an investigation runtime's own labels.

Historical scope choice: use a public upstream investigation API rather than implement a first-party loop in this proposal. No comparative cost or quality measurement is claimed. The canonical record now selects a first-party default harness and supersedes this choice.

**Evidence.** oncall-and-aiops-oss.md §B.1 (HolmesGPT Apache-2.0, CNCF Sandbox, Grafana-native toolsets; "wrap it as the incident-rca plugin rather than writing our own agent loop"). gap-kernel-hot-path §4.1 documents the SSE event contract, the stateless history model and the multi-tenancy caveat (process-level toolset config, unauthenticated admin endpoints), which is why the hosted profile runs one HolmesGPT per tenant with the kernel as its only network peer. The canonical record §2.2 supplies the verification and execution requirements; no measured saving is asserted by this proposal.

**Exit criterion.** If HolmesGPT's `/api/chat` changes incompatibly or cannot answer non-incident chat questions well enough for the Slack surface, the `investigator` interface is where a replacement lands; nothing else in the kernel knows what HolmesGPT is.

### 2.3 On-call

**Choice.** Embed GoAlert behind the `on-call-provider` extension point. One GoAlert per tenant in hosted (its own schema in the shared PostgreSQL cluster, its own container); one GoAlert in self-host-full. The interface has five verbs: `Schedules`, `WhoIsOnCall`, `Page`, `Ack`, `Resolve`. The kernel pages one GoAlert alert per incident through a per-tenant generic integration key, and caches `WhoIsOnCall` in `store.kv` for 60 seconds so `/aiops oncall who` never waits on GoAlert.

SMS, voice, email and the schedule UI are GoAlert's (Twilio and SMTP configured on GoAlert). We write no notification channel code. Push is GoAlert's webhook contact method pointed at ntfy if a tenant wants it.

Refused: a native schedules and escalation module (triggers to revisit are exactly those in oncall-and-aiops-oss.md §A.4: hosted tenant count beyond what one-GoAlert-per-tenant can operate, Slack-native schedule editing as a top request, or one data model across schedules and incident timeline). Refused in MVP: PagerDuty, Opsgenie and IRM adapters; each is a day's work against the five-verb interface when the first customer asks.

**Evidence.** oncall-and-aiops-oss.md §A.1 (OnCall OSS archived 2026-03-24, AGPL, one company-maintained AGPL variant), §A.3 feature matrix (GoAlert is the only active Apache-2.0 project with schedules, escalation, Alertmanager and dashboard-server webhook intake, Twilio, Slack and a GraphQL API), §A.4 recommendation. gap-kernel-hot-path §4.2 confirms the per-tenant sidecar shape. grafana-stack.md §11 confirms nothing open source from the vendor covers on-call.

### 2.4 Slack runtime

**Choice.** `chat-surface.slack` is a Go plugin inside the kernel on `slack-go/slack` v0.31.0, pinned and vendored behind a thin internal interface. Hosted composes the Events API receiver (one distributed app we own, OAuth v2 install per customer workspace, request signing). Self-host composes the Socket Mode receiver with an app the customer creates from `aiops slack manifest`. Both receivers emit the same normalized inbound envelope; routing, identity, approvals, sessions and streaming are transport-agnostic.

One Slack app, one bot user, one agent. Pillar is a routing dimension (`/aiops <pillar>`, channel binding, thread stickiness), not a separate app.

Refused: a Bolt (Python or JS) sidecar.

**Evidence.** gap-slack-chatops-tenancy-and-runtime.md §0 and §8: slack-go merged `agents.sessions.*`, the agent events and `authorizations` parsing in v0.30.0; the Bolt `Assistant` helpers still wrap the method deprecated for February 2027; a sidecar would move tenant routing and approval authorization out of the kernel. §3 explains why Socket Mode is wrong for the hosted tier (Marketplace exclusion, 1 request/minute throttle for unlisted distributed apps, ten connections shared by all workspaces) and right for self-host (no ingress). §5 places L3 quorum evaluation in the kernel, never in Slack.

### 2.5 Who receives Alertmanager and dashboard-server alert webhooks

**Choice.** The kernel's `signal-ingest` row receives every alert webhook. It acks after one durable insert, then dedupes, applies the deterministic correlation rules, opens or joins an incident, and writes the outbox events that fan out to the on-call provider (one page per incident), the Slack thread and the investigation job. GoAlert never receives raw alerts in our profiles.

Why route through platform ingest: authenticated admission, normalization and incident identity must precede paging, so repeated raw deliveries do not become separate pages. Routing through the kernel also makes the kernel the single audit point and lets a tenant with no on-call provider still get incidents.

Per-tenant Alertmanager configuration (the multi-tenant Alertmanager that ships with Mimir, or the dashboard server's built-in one for managed rules) is rendered by the `tenant` plugin at tenant create: one default route to the kernel webhook with a per-tenant token. Both payload versions (Alertmanager v4, dashboard-server v1) are parsed by the same row with version sniffing.

Escape hatch, unsupported: a tenant may add a second Alertmanager receiver straight to its GoAlert integration key.

**Evidence.** gap-kernel-hot-path §2.5 (ack p99 250 ms after durable write, 429 with Retry-After instead of drops, idempotency on `(tenant, source, fingerprint, startsAt)`, storm mode above 500 alerts/minute). grafana-stack.md §10.5 (both rule types, external Alertmanager as data source, webhook contact point) and §14 point 5. The canonical record §2.1 row 5 governs current alert admission and paging.

### 2.6 Remote config and fleet server

**Choice.** Build the `fleet` plugin on `connect-go` implementing `collector.v1.CollectorService` (three unary RPCs) inside the kernel. Data model: agents, tenant bootstrap tokens, pipelines with attribute matchers, `declare`-wrapped merge into one file, `sha256` hash, serve cache that survives a database outage, `remote_config_status` and `effective_config` stored per agent as the inventory and config history. Validation gate: `alloy validate` with a pinned binary in the kernel image. Config comes from the features YAML (§2.11), not hand-written pipelines, in MVP.

OpAMP: phase 2 as a second listener in the same plugin, started only when a customer brings an OTel-engine or `otelcol-contrib` fleet. Shepherd is used as the conformance oracle in the e2e test, not as a dependency.

Refused in MVP: the `fleet-hostagent` helper, remote upgrade, remote support bundle, canary rollouts. Hosts upgrade with their package manager or Ansible; Kubernetes upgrades with Helm.

**Evidence.** gap-remotecfg-server-build-vs-adopt.md §0 and §1 (the wire contract is three RPCs, full config every time, agent keeps last-good on every error, `RegisterCollector` failure at startup stops polling until restart), §2 and §3 (all three community servers are single-maintainer; shepherd is Kubernetes-only and would be a second tenancy product), §4.4 (Alloy has no self-updater), §5 (OpAMP is the only path for the OTel engine; `accepts_packages` is disabled upstream). grafana-stack.md §3.5 (protocol Apache-2.0, vendor server Cloud-only).

### 2.7 Tenant gateway for ingest

**Choice.** Envoy with `jwt_authn` and `claim_to_headers` writing `X-Scope-OrgID` from a signed `orgid` claim, JWKS delivered as a file the kernel renders. The same rendered Envoy config runs in hosted (N stateless replicas) and in self-host-full (one container in the compose). The dev-only `single-node` profile has no gateway: everything is loopback, the backends run single-tenant, and Alloy sets the header itself.

Per-agent credentials: an OAuth2 client (`client_id` = agent id, per-agent secret in a 0600 file) minted by `POST /v1/enrol` from a tenant-scoped bootstrap token. Every Alloy write block carries the same `oauth2 {}` block and refreshes its own 15-minute JWT. Token issuance (`/oauth2/token`, `/.well-known/jwks.json`) is served by the same `aiops` binary composed as the `auth-edge` profile and deployed separately in hosted so a kernel deploy never stops token refresh.

Tenant id: `[a-z0-9][a-z0-9-]{0,62}`, used verbatim as `X-Scope-OrgID` on all four backends. Trace transport at the edge is OTLP/HTTP only, so the gateway handles plain HTTP/1.1 POSTs on every route.

Refused in MVP: the in-process Go reverse proxy (`gateway.inproc`); it is built only if a customer needs a single network-exposed binary without a container runtime. Refused: NGINX and Traefik variants.

**Evidence.** gap-tenant-gateway-and-agent-enrolment.md §0 D1 to D5, §1.1 (a 401 at the agent is data loss, so the gateway must stay up through control-plane deploys), §1.3 (enrol flow and the 15-line `config.alloy`), §2.2 (Envoy `jwt_authn` needs no round trip per request; `ext_authz` and `forwardAuth` do), §3 (the kernel is not the proxy). plugin-architecture.md §2.5 (tenant id charset). grafana-stack.md §12.1 (all four backends trust the header and have no auth).

### 2.8 Hosted dashboard-server tenancy

**Choice.** One org per tenant on a shared dashboard-server pool. The `ui` kernel plugin provisions, per tenant, the org, four data sources (`metrics`, `logs`, `traces`, `profiles`) pointed at the gateway with `httpHeaderName1: Authorization` and a tenant-scoped service JWT, a service account token for write-back, and the app plugin instance settings `{kernel_url, tenant_id}` plus the kernel token in `secureJsonData`.

The app plugin (`aiops-app`) is a Scenes frontend plus a Go backend of three handlers: `CallResource` proxy to the kernel, `CheckHealth`, settings. No business logic, no database. Identity reaches the kernel as the signed `X-Grafana-Id` JWT verified against the instance's JWKS; the basic org role is a floor; approver bindings and quorum are kernel rows.

Instance-per-tenant: not built in MVP. The `ui` row already carries `{url, org_id, service_account_token}`, so a customer-owned or dedicated instance is a row override, not a feature.

Self-hosters: one org, tenant `default`. Plugin signing: unsigned allow-listed build for self-hosters, privately signed build for our hosted root URLs.

**Evidence.** gap-kernel-hot-path §3.1 (the archived OnCall plugin backend was exactly this proxy), §3.2 (identity and role mapping), §3.3 (signing modes), §3.4 and D11 (org per tenant is cheaper and the kernel relies on nothing instance-scoped). grafana-stack.md §10.2, §10.4, §10.6. gap-tenant-gateway §4 (data sources carry a bearer JWT, not a static header).

### 2.9 Kernel state

**Choice.** PostgreSQL 16+ for every profile except `single-node`, which uses SQLite (WAL, one writer). River (MPL-2.0, file-level copyleft, acceptable as a dependency) on the same database for jobs, periodic jobs, unique jobs and leader election. Events are a transactional outbox table written in the same transaction as the state change and the River job insert; `LISTEN/NOTIFY` is the doorbell only. Blob storage is the S3-compatible bucket the data plane already needs, prefixed per tenant.

Per-tenant fairness: one token-bucket table checked at job start; jobs over budget snooze. `ponytail:` upgrade path is River Pro or a NATS work queue once a tenant can be shown to starve another.

Refused: Kafka in the kernel (even where the data plane runs it), Redis, NATS (not even as an optional row in MVP; `events.nats` is added when `LISTEN/NOTIFY` is measured insufficient under several replicas), Temporal.

**Evidence.** gap-kernel-hot-path §2.1 to §2.4 in full; §2.3 for why the data-plane Kafka is a write-ahead log, not a work queue, and is absent in three of four profiles.

### 2.10 eBPF sensor bundle

**Choice.** `beyla.ebpf` (child process) and `pyroscope.ebpf` (in-process) inside the privileged Alloy DaemonSet rendered by the k8s-monitoring chart's `privileged` plus `daemonset` presets; on hosts, the same two components in the single `alloy` systemd unit when the host passes the tier check. Kernel floor: Tier A is Linux 5.10+ with BTF on amd64/arm64 and cgroup v2; Tier B is RHEL 8.5+ (4.18.0-348+) best effort; Tier C gets Alloy without eBPF components. The config generator decides per host from `uname -r`, `/etc/os-release` and `/sys/kernel/btf/vmlinux`.

Runtime security sensor: Tetragon is ratified over Falco, and it ships with the SecOps pillar in phase 2, not in MVP. A second privileged DaemonSet with no server-side consumer is cost without value.

Refused: a BTF mirror; Falco; running both sensors on one node; Windows eBPF; Fargate and Autopilot eBPF; kernel-module drivers; in-kernel enforcement by default.

**Evidence.** gap-ebpf-sensor-bundle-and-kernel-floor.md §1 decisions table, §2.1 and §2.2 (isolation model of the two components), §3.2 (Tetragon decision and its flip condition), §4.1 (floor), §5 (no BTF mirror), §6 (two DaemonSets on Kubernetes, two units on hosts), §7 (non-goals). grafana-stack.md §3.7 (capability lists). datadog-agent.md §7 points 5 and 6.

### 2.11 Agent packaging

**Choice.** The customer-visible surface is one `features:` YAML (host metrics, logs, Kubernetes, application OTLP, eBPF auto-instrumentation, profiling, integrations), modelled on the k8s-monitoring chart's `features.*` map and Datadog's handful of flags.

Hosts: `curl -fsSL .../install.sh | sh -s -- --token <bootstrap>` enrols, installs the Alloy package with a checksum, writes the per-agent secret and a `config.alloy` containing only a `remotecfg` block, and enables the `alloy` systemd unit. The `fleet` plugin renders the full `config.alloy` server-side from the features YAML and the host's tier. One unit per host in MVP (Tetragon joins as a second unit with SecOps).

Kubernetes: our `aiops-agent` Helm chart is an umbrella over the upstream `k8s-monitoring` chart (as a subchart) plus a pre-install Job that enrols once per cluster and writes the credential Secret. The chart renders the Alloy configs; the layout is the chart's own: `logs-collector` DaemonSet (filesystem log reader), `profiles-collector` privileged DaemonSet (`beyla.ebpf`, `pyroscope.ebpf`), `metrics-collector` clustered StatefulSet, `events-collector` singleton, optional OTLP `receiver` DaemonSet. `remotecfg` is optional on Kubernetes in MVP; fleet inventory for clusters comes from Alloy's self-reported series in the tenant's metrics store.

Windows: Alloy's Windows installer with `prometheus.exporter.windows` and `loki.source.windowsevent` only. No eBPF, no network or security telemetry. macOS: not in MVP.

Refused: writing our own collection chart; an operator in MVP; supporting hand-written Alloy pipelines through the UI in MVP (an escape hatch exists: a raw pipeline row the fleet plugin appends verbatim).

**Evidence.** grafana-stack.md §3.8 (chart presets and the DaemonSet/StatefulSet/singleton layout, `destinations` with auth), §3.1 (Alloy platforms and installers). datadog-agent.md §3 (install paths to match), §7 points 2 and 5 (tiny config surface; Windows parity is metrics and logs only). gap-tenant-gateway §1.3 (the install sequence and the Kubernetes variant). gap-ebpf §6.

### 2.12 Pillar scope for MVP

**Choice.** Ship Incident, Change, Compliance, chat-ops and on-call first; FinOps, SecOps, Continuous Improvement and Executive Reporting are later plugins. A pillar is not a code category: it is a bundle of rows (signal sources, a HolmesGPT persona and toolset, dashboards, a Slack channel binding default), so adding a pillar later adds rows, not kernel code.

MVP shape of each:

| Pillar | MVP shape | Machinery it reuses |
|---|---|---|
| Incident | Alert webhook to incident to Slack thread to HolmesGPT RCA to page; typed L1 actions with approval; zero-cost verification; RCA stored as incident artifact and dashboard-server annotation | signal-ingest, correlator, investigator.holmes, chat-surface.slack, on-call-provider.goalert, remediation-executor.kubernetes |
| Change | Change feed from Argo CD and Flux notification webhooks and a generic webhook, stored as change records and written as annotations; change-window correlation into incidents; the change ledger that every kernel write passes through | signal-ingest (kind `change`), correlator, change hub |
| Compliance | Kubernetes PolicyReports (Kyverno, Trivy Operator) and Kubescape scan results collected as findings; control mapping for CIS and SOC 2 from the Kubescape framework library; score panel and daily digest; a failing control opens a `control:<id>` incident | signal-ingest (kind `finding`), findings table, dashboards |
| Chat-ops | `/aiops <pillar> <question>`, thread sessions, streaming answers, approval buttons, `/aiops oncall who` and `ack` | chat-surface.slack, investigator.holmes, on-call-provider |
| On-call | GoAlert embedded, one page per incident | on-call-provider.goalert |

Later pillars, as plugins with real inputs: FinOps (OpenCost scraped into the metrics store, weekly krr run), SecOps (Tetragon events into the log store with ruler-derived findings, Trivy Operator, Kubescape, Prowler), Continuous Improvement (Sloth-generated SLO rules, profile regressions), Executive Reporting (image renderer plus a Markdown template plus a PDF step).

**Requirements and public inputs.** The canonical record §2.1 row 12 phases Incident, Change and Compliance first and requires real signal or tool inputs. oncall-and-aiops-oss.md §B.10 surveys public building blocks; §B.4 covers Kubernetes report inputs and §B.5 covers Argo and Flux notifications.

### 2.13 LLM access

**Choice.** Budgets in the kernel, no LiteLLM proxy. One `llm_budget` table per tenant (daily USD and token ceilings, per-run cost cap, step cap) and one `llm_usage` ledger fed by HolmesGPT's `token_count` SSE events priced with a small static price table that we maintain. The kernel refuses to start a new investigation when the tenant's budget is exhausted and stops a running one at the per-run cap; chat questions get a budget-exhausted answer instead of a model call.

Credentials: the HolmesGPT instance for a tenant receives either our provider key (hosted, metered by the table above) or the tenant's BYO key from the tenant-scoped secrets store, through its environment at start. Self-hosters who already run a LiteLLM proxy, Ollama or vLLM point HolmesGPT at that OpenAI-compatible base URL; that is HolmesGPT configuration, not our code.

Refused: embedding the LiteLLM proxy (its own PostgreSQL schema, UI, enterprise directory, and a second place where tenants and keys live); a kernel LLM gateway that proxies model traffic (nothing in MVP calls a model except HolmesGPT).

**Evidence.** plugin-architecture.md §2.5 ("plugins never hold LLM credentials"; the contract is the same whether LiteLLM or in-kernel) and §5 question 3. oncall-and-aiops-oss.md §B.8 (LiteLLM MIT core plus `enterprise/`; Ollama and vLLM for self-host). gap-kernel-hot-path §4.1 (`token_count` event in the SSE stream). The canonical record §2.1 row 13 defines current model-access, metering and budget requirements.

### 2.14 Core contracts every executor honours

**Choice.** Six contracts, all implemented in one kernel package (`policy`) that is the only code path allowed to call `remediation-executor.Apply`. Executor plugins receive a signed, typed plan and return a receipt; they cannot be invoked any other way, so there is nothing for a plugin author to honour or forget.

| Contract | MVP form | Enforced where |
|---|---|---|
| Approval floor | Reads are free; every write pauses for a human decision; tenant policy can add gates, never remove them; no unattended release in MVP | `policy.Gate` before every executor call; Slack and dashboard-server answerers; fails closed when no answerer row is composed |
| Write-role credential invisibility | HolmesGPT toolsets receive read-role credentials only; executors read write credentials from the tenant-scoped secrets store at apply time; no credential ever appears in a transcript or a Slack message | secrets store scoping `(tenant, plugin, name)`; investigator rows cannot request write-scoped secrets |
| Change ledger before write | `change_record` row inserted (with pre-state captured by the executor's `Plan`) in the same transaction that enqueues the apply job; rollback synthesized from the plan where the executor supports it | `policy.Apply` transaction; executor `Plan` is side-effect free by conformance test |
| Risk tiers L0 to L3 with distinct-approver quorum | L0 read; L1 reversible single-resource write (one approver, requester excluded); L2 one approver from the `approver` binding; L3 two distinct approver subjects, re-checked at dispatch. Tier follows the blast radius of the fix. Change windows and freezes: phase 2 | `policy.Tier` from the action catalogue; quorum evaluated against kernel bindings keyed by stable subject (`sub` from the dashboard-server JWT or the verified Slack user link) |
| Audit chain | Append-only `audit_event` table with a per-tenant SHA-256 chain, sealed by a River periodic job; PostgreSQL trigger blocks update and delete; export endpoint for auditors | kernel middleware on every state-changing call; the outbox writer |
| PII scrub at chokepoints | One pseudonymiser (salted, correlation-preserving tokens for emails, IPs, tokens, phone numbers) applied at two egress points: tool output before it is sent to the model and anything posted to Slack. Linear-time regex detectors; no ML | `investigator.holmes` client (request path) and `chat-surface.slack` (outbound) |

**Authority.** The canonical record §2.2 establishes these first-party executor requirements, and §4.3 establishes their current plugin and dispatcher boundaries. gap-slack §5 and gap-kernel-hot-path §3.2 sketch how a surface supplies verified identity without taking ownership of approval or execution.

---

## 3. Container inventory

Ours means we build and release it; upstream means we run the vendor image unmodified and render its configuration. Presence: H = hosted, S = self-host-full, N = single-node (dev and demo), A = aiops-only.

| Container | Technology | Responsibility | H | S | N | A |
|---|---|---|---|---|---|---|
| `aiops` (kernel) | Go, one binary, compile-time row registry, River | API, signal ingest, correlation, incidents, change hub, policy, approvals, audit, fleet (Connect-RPC), tenant and limits rendering, dashboard-server provisioning, Slack surface, GoAlert and HolmesGPT clients, Kubernetes executor | N replicas | 1 to 2 | 1 (SQLite) | 1 |
| `aiops` as `auth-edge` | same image, profile composes only `auth` rows | `/oauth2/token`, `/.well-known/jwks.json`, `/v1/enrol`; separately scaled so token refresh survives kernel deploys | yes | folded into kernel | folded | folded |
| `aiops-gateway` | Envoy (upstream), config rendered by the kernel | JWT verification, `X-Scope-OrgID` injection, route table with per-route scopes, body limits, fail closed | yes | yes | no (loopback) | no |
| `aiops-app` | Dashboard-server app plugin: Scenes frontend, Go backend (three handlers) | In-UI pages for incidents, changes, findings, approvals, fleet; proxies to the kernel | yes | yes | yes | yes |
| PostgreSQL | upstream 16+ | kernel state, River, outbox, audit chain; GoAlert schemas as separate databases in the same cluster | HA | 1 | SQLite file instead | 1 |
| Object storage | S3-compatible (cloud bucket or MinIO) | data-plane blocks and chunks; kernel blobs (RCA artifacts, reports) per tenant prefix | yes | MinIO | local dir | optional |
| Metrics store | Mimir (upstream), classic architecture with ingesters, multi-tenant on, ruler and multi-tenant Alertmanager enabled | metrics, recording and alert rules, per-tenant Alertmanager | microservices, no Kafka in MVP | monolithic `-target=all` | monolithic | no (customer's Prometheus-compatible store) |
| Log store | Loki (upstream), multi-tenant on, ruler on | logs, log patterns, log-based alerts | distributed, no Kafka | monolithic | monolithic | no (customer's Loki-compatible store) |
| Trace store | Tempo (upstream), multi-tenant on, metrics-generator on | traces, service graph, span metrics | monolithic in MVP (single writer, object storage); microservices with the data-plane Kafka when trace volume forces it | monolithic | monolithic | no |
| Profile store | Pyroscope (upstream) v2, multi-tenant on | profiles | microservices | single binary | single binary | no |
| Dashboard server | upstream OSS 13.x, our app plugin installed | UI, orgs per tenant, data sources, managed alerting, annotations, image renderer | shared pool, org per tenant | 1, one org | 1 | 1 (customer's own, or ours) |
| Image renderer | upstream `grafana-image-renderer` | panel PNGs for Slack and later reports | 1 | 1 | optional | optional |
| HolmesGPT | upstream image, config rendered by the kernel | investigation and chat answers over read-only toolsets | one per tenant, network-isolated, kernel is the only peer | 1 shared | 1 | 1 |
| GoAlert | upstream image | schedules, escalation, paging, SMS and voice via Twilio, email, Slack notifications | one per tenant | 1 | 1 | 1 |
| mcp-grafana | upstream image, streamable-http, per-tenant service account token, read-only flags | "bring your own assistant" endpoint for customers' own AI clients; candidate tool surface for HolmesGPT if it can mount MCP toolsets (not covered by the research base; verify before relying on it) | one per tenant, optional | optional | optional | optional |
| `aiops-agent` (customer side) | Alloy (upstream) under systemd, or the `aiops-agent` umbrella chart over `k8s-monitoring` | all telemetry collection; eBPF on Tier A and B hosts | customer hosts and clusters | same | same | customer's existing collectors |

Deferred containers, named so nobody wonders: Tetragon DaemonSet and operator (SecOps, phase 2), OpenCost (FinOps, phase 2), NATS (`events.nats`, when measured), Kafka for the data plane (when the trace store needs to scale out), `fleet-hostagent` (never in MVP), LiteLLM proxy (never as part of the platform).

---

## 4. Extension points

Rule: an extension point exists in MVP only where there is a second implementation or a process boundary today. Row type in a profile is `<point>.<name>`.

| Id | Purpose | Interface sketch (Go) | MVP implementations | Next implementations and their trigger |
|---|---|---|---|---|
| `signal-ingest` | Turn an external payload into normalized signals of kind `alert`, `change` or `finding` and write them durably; idempotent; per-tenant bucket | `Serve(ctx, host)` registers routes or watchers; emits `Signal{tenant, kind, fingerprint, labels, annotations, startsAt, source}` | `alerts-webhook` (Alertmanager v4 and dashboard-server v1 with version sniffing), `gitops-webhook` (Argo CD and Flux notification payloads, generic JSON), `policyreport` (Kubernetes PolicyReport and Trivy report CRDs via a watch through the tenant's cluster credentials) | `kubescape` (CLI output upload), `prowler` (JSON upload), `pagerduty-inbound`, `cloudevents`: each when its pillar ships |
| `investigator` | Run an investigation or answer a question with a read-only toolset, streaming findings; stateless from the kernel's view | `Investigate(ctx, tenant, Incident, ToolSet, Budget) (stream Event, error)`; `Ask(ctx, tenant, Thread, Question, ToolSet, Budget) (stream Event, error)` | `holmes` (HTTP/SSE client to HolmesGPT) | a first-party loop only if HolmesGPT's exit criterion (§2.2) is hit; the gRPC adapter arrives with it |
| `on-call-provider` | Who is on call, page, ack, resolve, list schedules | `Schedules`, `WhoIsOnCall(schedule, at)`, `Page(incident) (PageID)`, `Ack(PageID, subject)`, `Resolve(PageID)` | `goalert` (GraphQL and integration key) | `pagerduty`, `opsgenie`, `cloud-irm`, `native`: first customer request for the adapters; §2.3 triggers for native |
| `chat-surface` | Receive chat events, map installation and user to tenant and subject, drive sessions, streams and approval buttons | `Serve(ctx, host)`; `Post(ctx, tenant, Thread, Message)`; `Update(ctx, tenant, MessageRef, Message)`; normalized `InboundEnvelope` | `slack-events` (hosted), `slack-socket-mode` (self-host) | `teams`, `mattermost`: first customer request; these are the first candidates for the gRPC adapter |
| `remediation-executor` | Plan, apply and roll back one typed action from the catalogue; `Plan` is side-effect free; `Apply` is idempotent on the plan digest | `Plan(ctx, tenant, Action) (Plan{digest, preState, rollback})`; `Apply(ctx, tenant, Plan) (Receipt)`; `Rollback(ctx, tenant, Plan, Receipt)` | `kubernetes` (rollout restart, scale, rollback to previous ReplicaSet, delete pod, cordon node; cluster credential from the secrets store) | `argocd-sync-rollback`, `aws-ssm`, `ansible`, `runbook-http`: with the Change pillar's phase-2 scope |

Action catalogue for `remediation-executor.kubernetes` in MVP: five actions, each with a fixed risk tier, a JSON schema for parameters, and a rollback definition. The LLM is never asked to produce a plan; the kernel offers the applicable actions for the implicated workload (from alert labels) as buttons.

Host services (not extension points; one implementation each, injected into every row): `logger`, `metrics`, `tracer`, `config`, `secrets` (tenant and row scoped, envelope-encrypted in PostgreSQL), `tenant`, `store.sql` (per-row schemas and migrations), `store.kv`, `store.blob`, `jobs` (River), `scheduler` (River periodic on the leader), `events` (outbox writer and subscriber), `http.router` (row-prefixed), `llm.budget`. This is plugin-architecture.md §4.4 minus `llm` as a proxy and minus `discovery`.

Internal interfaces that look like extension points but are not promoted in MVP (one package each, `ponytail:` note with the trigger): `limits-renderer` (four pure functions, one per backend, see gap-tenant-gateway §5.2), `gateway` renderer (Envoy only; promote when `inproc` is built), `correlator` (three deterministic rules in one package; promote when an LLM re-ranker or a topology graph is added), `approval-policy` (fixed tiers; promote when a tenant needs Rego or CEL), `report-renderer` (phase 3), `datasource` (the kernel's own Prometheus-API and Loki clients for zero-cost verification and fleet health; promote with the aiops-only Datadog connector if ever).

Lifecycle for every row: `Validate(cfg)`, `Start(ctx, host)`, `Update(ctx, cfg)`, `Health(ctx)`, `Shutdown(ctx)`; the kernel unwinds every registration the row made through the host in reverse order after `Shutdown`; a panic marks the row `Failed`; `required: true` rows abort boot, the rest degrade to a warning (plugin-architecture.md §4.3; dsh-architecture.md §8.1 points 2 and 7).

---

## 5. Tenancy and data model

### 5.1 What the kernel stores (PostgreSQL, every row carries `tenant_id`, row-level security as the second fence)

| Package | Tables | Notes |
|---|---|---|
| `tenant` | `tenant` (id, plan, status, limits document), `tenant_binding` (tenant, subject, role: tenant-admin, operator, approver, viewer), `bootstrap_token` | `TenantLimits` is the one document rendered into four backend override files (gap-tenant-gateway §5.2); status `suspended` is enforced by refusing token issuance |
| `auth` | `agent_client` (agent id, tenant, Argon2id secret hash, status), `service_token` (jti, tenant or tenants, scopes, expiry), `signing_key` | JWT claims `{iss, sub, tenant, orgid, scope[], aud, exp, jti}`; denylist consulted on query routes only |
| `fleet` | `agent` (id, tenant, name, local attributes, last poll, remote config status, effective config), `pipeline` (tenant, name, matchers, body, hash), `served_config` (agent, hash, content, served at) | config history and rollback come for free from `served_config` |
| `signal` | `raw_group` (tenant, source, group key, payload, received at), `signal` (tenant, kind, fingerprint, labels, annotations, startsAt, endsAt, source) | idempotency keys from gap-kernel-hot-path §2.5 |
| `incident` | `incident` (tenant, correlation key, state, priority, criticality, suspect change, storm flag), `incident_signal`, `investigation` (incident, transcript JSON, model, usage, cost, state), `artifact` (incident, kind: rca, lkg, report, blob ref), `recheck` (incident, rung, verdict, evidence) | state machine `open, investigating, awaiting_approval, remediating, resolved, escalated`; the transcript is HolmesGPT's `conversation_history` |
| `change` | `change_record` (tenant, source: incident, manual, gitops, state, risk tier, action, plan digest, pre-state, rollback, receipt), `change_window` (phase 2) | ledger-before-write is a transaction invariant, not a convention |
| `finding` | `finding` (tenant, source, control id, resource, severity, status, first seen, last seen, evidence ref), `control` (framework, id, title, mapping to finding sources), `attestation` (phase 2) | compliance score is a query over this table |
| `approval` | `approval_request` (change record, tier, required quorum, state, expiry), `approval_decision` (request, subject, decision, surface, at) | approval and dispatch checks follow canonical record §2.2 |
| `audit` | `audit_event` (tenant, seq, actor, action, target, payload hash, prev hash, hash), `audit_chain_head` | append-only with a trigger; sealed asynchronously |
| `llm` | `llm_budget`, `llm_usage` | per tenant and per investigation |
| `chat` | `slack_installation`, `slack_installation_workspace`, `slack_channel_binding`, `slack_user_link`, `slack_thread_session`, `slack_interaction_log` | from gap-slack §9 |
| `ui` | `dashboard_org` (tenant, org id, service account token ref, app instance token ref) | one row per tenant per dashboard-server instance |
| `oncall` | `oncall_binding` (tenant, GoAlert URL, API key ref, integration key ref, service id), `page` (incident, provider page id, state) | `WhoIsOnCall` cache lives in `store.kv` |
| kernel | `events` (outbox), `river_*`, `tenant_bucket` | one database, one transaction per state change |

Secrets (cluster credentials, Slack tokens, provider keys, GoAlert API keys, dashboard-server service account tokens) are rows in `secret` with envelope encryption and `(tenant, package, name)` scoping. A KMS-backed key wrapper is a phase-2 row.

### 5.2 What lives in the backends, not in the kernel

| Data | Where | How the kernel reaches it |
|---|---|---|
| Metrics, logs, traces, profiles | Mimir, Loki, Tempo, Pyroscope per tenant under `X-Scope-OrgID` | never on the hot path; HolmesGPT queries through its toolsets with read-role service tokens through the gateway; the kernel's zero-cost verification rungs use the Prometheus API |
| Alert and recording rules | Mimir and Loki rulers (data-source managed), synced from PrometheusRule CRDs by Alloy's `mimir.rules.kubernetes`, or dashboard-server managed rules | rules are customer-owned; the kernel only installs the webhook route |
| Alertmanager configuration per tenant | Mimir's multi-tenant Alertmanager and the dashboard server's built-in one | rendered by `tenant` at create; a webhook receiver to the kernel |
| Dashboards, folders, data sources, annotations, silences | Dashboard server, one org per tenant | `ui` package over the HTTP API with the per-org service account; incidents, changes and RCA summaries are written as annotations so every dashboard shows them |
| Schedules, escalation policies, contact methods, notification log | GoAlert per tenant | `on-call-provider.goalert` over GraphQL; the kernel keeps only the page reference |
| Investigation reasoning | HolmesGPT is stateless; the transcript is the kernel's | `conversation_history` round-trips on every call |
| Blobs (RCA Markdown, PDF reports later, support bundles later) | object storage per tenant prefix | `store.blob` |

### 5.3 Tenant lifecycle

Create: insert the tenant, render the four override files, provision the dashboard-server org and four data sources, create the GoAlert database and container (hosted) or service (self-host), start the HolmesGPT instance (hosted), install the Alertmanager webhook route, mint the first bootstrap token. Nothing in the four backends needs to exist before the first write (gap-tenant-gateway §5.3).

Suspend: refuse token issuance; effective within one access-token TTL; backend limits stay as the second guard.

Delete: the saga from gap-tenant-gateway §5.3 as River jobs with per-step status on the tenant row. The research is explicit that Pyroscope cannot guarantee per-tenant erasure before compaction; hosted terms of service say so rather than the platform pretending otherwise.

Identity: a platform subject is either the `sub` of a verified dashboard-server `X-Grafana-Id` JWT, a verified Slack user link, or an OIDC subject from the operator's IdP calling the kernel API directly. Roles above the dashboard-server basic role (approver, tenant-admin) are kernel bindings. There is no per-tenant IdP provisioning; the IdP is whatever the dashboard server is already configured with (canonical record §1 and §2.1).

---

## 6. Profiles

A profile is `profile.yaml` naming bundles in order plus a patch file. Layers: bundle patches, profile patch, site patch (`$AIOPS_HOME/aiops.patch.yml`), `--patch` overlays, tenant overlay (per-tenant rows only). `aiops config render` prints the composed tree and the layer that set each row.

Bundles shipped:

| Bundle | Rows it inserts |
|---|---|
| `base` | host services, `api`, `signal-ingest.alerts-webhook`, `correlator`, `incident`, `change`, `finding`, `policy`, `approval`, `audit`, `llm.budget`, `tenant`, `auth` |
| `stack` | `fleet`, `limits` (four renderers), `gateway.envoy` renderer, `ui` (dashboard-server provisioning), zero-cost verification clients |
| `pillars-mvp` | `investigator.holmes`, `signal-ingest.gitops-webhook`, `signal-ingest.policyreport`, `remediation-executor.kubernetes`, pillar personas and toolset YAML for incident, change, compliance |
| `slack-hosted` | `chat-surface.slack-events` |
| `slack-selfhost` | `chat-surface.slack-socket-mode` |
| `oncall-goalert` | `on-call-provider.goalert` |

Profiles:

| Profile | Bundles | Distinguishing rows and settings | Containers (from §3) |
|---|---|---|---|
| `hosted` | base, stack, pillars-mvp, slack-hosted, oncall-goalert | `store.sql` PostgreSQL HA; `auth` rows also composed as the separate `auth-edge` deployment; `investigator.holmes` and `on-call-provider.goalert` with `instancing: per-tenant`; `ui` targets the shared dashboard-server pool with org per tenant; Kubernetes manifests via our Helm chart | kernel N, auth-edge, Envoy, PostgreSQL HA, object storage, Mimir microservices, Loki distributed, Tempo monolithic, Pyroscope microservices, dashboard-server pool, renderer, HolmesGPT per tenant, GoAlert per tenant, optional mcp-grafana per tenant |
| `self-host-full` | base, stack, pillars-mvp, slack-selfhost, oncall-goalert | `store.sql` PostgreSQL; one tenant `default` by default, more allowed; one HolmesGPT, one GoAlert; Docker Compose and a Helm chart; the compose includes Envoy and MinIO | kernel 1, Envoy, PostgreSQL, MinIO, four monolithic backends, dashboard server, renderer, HolmesGPT, GoAlert |
| `single-node` (dev and demo) | base, stack, pillars-mvp, slack-selfhost, oncall-goalert | SQLite; no gateway; backends single-tenant on loopback; `remotecfg` off, Alloy reads a local file; everything in one compose on a laptop | kernel 1, four monolithic backends, dashboard server, HolmesGPT, GoAlert |
| `aiops-only` | base, pillars-mvp, slack-selfhost (or slack-hosted when we host it), oncall-goalert | no `stack` bundle: no fleet, no gateway, no limits, no provisioning of a data plane; `ui` points at the customer's dashboard server (unsigned plugin allow-listed) or is disabled; HolmesGPT toolsets point at the customer's Prometheus-compatible and Loki-compatible endpoints and Kubernetes clusters; webhooks from their existing Alertmanager | kernel 1, PostgreSQL, HolmesGPT, GoAlert, app plugin on their dashboard server |
| `agent-host` | not a kernel profile: the features YAML plus the install script | `config.alloy` is a `remotecfg` block; the fleet plugin renders the pipelines; tier check gates eBPF components | Alloy under systemd (Tetragon unit added with SecOps) |
| `agent-k8s` | not a kernel profile: the `aiops-agent` umbrella chart | `k8s-monitoring` subchart with `destinations` set to the gateway and `features` from our values; pre-install enrol Job; `remotecfg` optional | Alloy DaemonSets, clustered StatefulSet, singleton, optional OTLP receiver |

`aiops-only` is the profile that proves the plugin model: it must boot with the `stack` bundle absent and nothing in `base` or `pillars-mvp` may import a `stack` package. A CI job boots every profile from the built binary and asserts the required-row table (dsh-architecture.md §8.1 point 7).

---

## 7. MVP scope and roadmap

### 7.1 MVP-1, the demo that has to work end to end

1. `curl | sh -s -- --token` on a Linux VM and `helm install aiops-agent` on a cluster; both appear in the fleet inventory within two minutes; metrics, logs, traces (eBPF, no code change) and profiles show in the tenant's dashboards.
2. A Prometheus rule fires; the kernel opens one incident for the burst of alerts; a Slack thread opens in the bound channel; GoAlert pages the on-call engineer by SMS; the engineer acks from Slack.
3. HolmesGPT posts an RCA into the thread within minutes, with the last deployment from the change feed and the matching log pattern; the RCA is an annotation on the service dashboard.
4. The thread offers "rollout restart" (L1); a second user approves; the kernel writes the change record, applies, verifies with the zero-cost rungs, and closes the incident when the alert clears.
5. `/aiops compliance score` answers from the PolicyReport findings; a failed CIS control shows as an incident with `control:` key.
6. The whole loop is visible in the app plugin and exportable from the audit chain.

### 7.2 Phased roadmap

| Quarter | Milestone | Scope |
|---|---|---|
| Q4 2026 (now) | Design accepted, skeleton | This proposal reviewed and merged with the other two; repo skeleton; kernel boot with profiles and the fake-host test kit; `tenant`, `auth`, `gateway.envoy` renderer, `fleet` three RPCs with a real Alloy in CI; `self-host-full` compose with the four backends and the dashboard server |
| Q1 2027 | MVP-1 (§7.1) on `self-host-full` and `single-node` | `signal-ingest.alerts-webhook`, correlator, incidents, outbox and River; `investigator.holmes`; `chat-surface.slack-socket-mode`; `on-call-provider.goalert`; `remediation-executor.kubernetes` with five actions; `policy` package with all six contracts; `ui` provisioning and the app plugin (unsigned); `agent-host` install script; `aiops-agent` umbrella chart |
| Q2 2027 | MVP-2: Change, Compliance, hosted GA | `signal-ingest.gitops-webhook` and annotations; change-window correlation; `signal-ingest.policyreport`, controls, score dashboard; `chat-surface.slack-events` and the distributed Slack app; `hosted` profile on Kubernetes with Envoy, `auth-edge`, per-tenant HolmesGPT and GoAlert, tenant create and suspend, delete saga; privately signed app plugin; `aiops-only` profile proven against a customer's existing stores; first paying tenants |
| Q3 2027 | Phase 2 pillars and fleet depth | SecOps: Tetragon DaemonSet and unit, starter TracingPolicy pack, Loki ruler detections, Trivy Operator and Kubescape as findings; FinOps: OpenCost scrape plus weekly krr job plus budget alerts; `on-call-provider.pagerduty`; executors `argocd-sync-rollback` and `runbook-http`; change windows and freezes; OpAMP listener if a customer needs it; KMS-backed secrets |
| Q4 2027 | Phase 3 | Executive Reporting (image renderer, Markdown template, PDF step, scheduled email through GoAlert's SMTP or our own SMTP row); Continuous Improvement (Sloth SLO rules, error-budget digest, profile regression check); community-signed app plugin; gRPC remote-plugin adapter if a second out-of-process implementation exists; native on-call module only if a §2.3 trigger has fired; `gateway.inproc` only if requested |

What the quarters deliberately do not contain: a portal, a test factory, a benchmark harness beyond cassette replay, a Python SDK, Wasm, an operator, multi-region, status pages.

---

## 8. Top risks and what we refuse to build

### 8.1 Risks

| Risk | Why it matters | Mitigation |
|---|---|---|
| HolmesGPT API drift | The docs page for `/api/chat` is marked "previous version"; parity with 0.45.0 is unverified (gap-kernel-hot-path §7); the project is pre-1.0 and now shared between two companies | Pin the image; the cassette-replayed conformance test for `investigator.holmes` fails loudly on drift; the `investigator` interface isolates the blast radius to one package |
| HolmesGPT multi-tenancy | Toolset and model config are process-level; admin endpoints are unauthenticated (gap-kernel-hot-path §4.1) | One instance per tenant in hosted, network policy allowing only the kernel; never expose it |
| Gateway availability equals ingestion SLO | Alloy treats 401 and 403 as terminal and drops samples (gap-tenant-gateway §1.1) | Envoy is stateless with a file JWKS; `auth-edge` is its own deployment; 15-minute tokens bound any kernel outage; overlap on key rotation |
| GoAlert sprawl in hosted | One container and one database per tenant | Acceptable to a few hundred tenants; the §2.3 trigger converts this into the native module when it stops being acceptable |
| slack-go volatility | Breaking changes in consecutive minors, volunteer project (gap-slack §8) | Pin and vendor; thin internal interface; run its examples in CI |
| Dashboard-server identity floor | The Grafana release that forwards `X-Grafana-Id` unconditionally is unverified (gap-kernel-hot-path §6.1) | The proxy tolerates a missing header as `actor=plugin`, read-only; pin a minimum version once verified |
| AGPL hygiene | One accidental import of a Mimir, Loki or Tempo Go package makes the kernel AGPL (grafana-stack.md §13.1) | A `go.mod` deny-list check in CI; only the listed Apache-2.0 sub-trees are importable |
| Trademark policy | No vendor marks in product, plugin or container names (grafana-stack.md §13.2) | Naming rule enforced in review: `aiops-*`, `metrics`, `logs`, `traces`, `profiles`, `ui`; attribution statement in docs |
| eBPF floor excludes old fleets | Tier C hosts get no traces, network or profiles (gap-ebpf §4) | The install script says so per host; Tier C still gets metrics and logs; no BTF mirror is a deliberate refusal |
| Compliance content rights | Scanner code licences do not necessarily cover framework text | Use first-party catalogue wording and the display allow-list; see the canonical record's licensing requirements and the pillar-base licence audit |
| k8s-monitoring chart major versions | We wrap it as a subchart; a 5.x could change the values schema | Pin the subchart; our values layer is small; test upgrades in CI with kind |
| Per-tenant erasure in the profile store | Pyroscope level-0 segments are multi-tenant objects (gap-tenant-gateway §5.3) | State it in hosted terms; regulated tenants can be given profiles off or a dedicated bucket later |
| LLM spend | Investigations are minutes of model time per incident | Kernel budgets, storm mode (no investigation on storm incidents), step caps, no best-of-2 |
| The three-RPC fleet server grows into a product | Grafana's own Fleet Management has pipelines, validation gates, canaries, a UI (gap-remotecfg §1.6) | MVP renders config from the features YAML only; raw pipelines are an escape hatch, not a UI |

### 8.2 What we refuse to build

| Refused | Instead | Revisit when |
|---|---|---|
| A native on-call engine (schedules, rotations, escalation) | GoAlert embedded | A §2.3 trigger fires |
| SMS, voice, push and email delivery code | GoAlert's Twilio, SMTP and webhook contact methods; ntfy for push | Never as kernel code |
| A first-party LLM agent loop, best-of-2, arbiter | HolmesGPT | HolmesGPT exit criterion (§2.2) |
| Agent-executed writes of any kind | Typed action catalogue executed by the kernel after approval | Never |
| An LLM gateway or embedded LiteLLM proxy | Kernel budget table; HolmesGPT's own provider client; customer-run proxies via base URL | A second model-calling component exists |
| A generic gRPC/UDS plugin runtime, a Python SDK, Wasm | Compile-time Go rows; out-of-process only for upstream services with their own APIs | A second out-of-process implementation of any extension point |
| A Kubernetes operator and CRDs for the platform | Helm charts and compose; profiles are YAML files | Customers ask for CRDs after v1 |
| Our own collection Helm chart or a modified Alloy | Umbrella over `k8s-monitoring`; unmodified Alloy with rendered config | Never |
| Fleet remote upgrade, remote support bundle, host helper binary | Package managers, Ansible, Helm; local `alloy` support bundle | A customer with no configuration management and a large VM fleet |
| A BTF mirror, runtime compilation, kernel modules, Windows eBPF | Tier model; Windows is metrics and event logs | Never for v1 |
| Falco, or Tetragon before SecOps | Nothing on the node until there is a server-side consumer | SecOps phase 2 ships Tetragon |
| A portal SPA, an admin SPA, a second login | Dashboard-server app plugin plus Slack plus the kernel API | Never |
| Per-tenant IdP provisioning, a Keycloak realm per tenant | The dashboard server's configured auth; kernel bindings for approver roles | Never |
| Kafka, Redis, NATS, Temporal in the kernel | PostgreSQL, River, outbox, `LISTEN/NOTIFY` | `events.nats` when `LISTEN/NOTIFY` is measured insufficient |
| Instance-per-tenant dashboard servers as a feature | Org per tenant; the `ui` row carries url and org id, so a dedicated instance is a row override | Customers who bring their own instance (works already) or need separate auth providers |
| A telemetry lakehouse inside the control plane | Query existing stores through supported connectors | Out of scope under the canonical record §1 |
| Datadog, Dynatrace, New Relic, Splunk connectors in MVP | `aiops-only` against Prometheus-compatible and Loki-compatible stores | First paying `aiops-only` customer on another vendor |
| Status pages, postmortem documents, incident timeline UI beyond the Slack thread and annotations | Uptime Kuma or OneUptime for status pages; RCA artifact is the postmortem | Phase 3 at the earliest |
| Multi-region, active-active control plane | One region per hosted deployment | After hosted GA |

The shortest path to a credible product is one Go binary, one database, five extension points, and five upstream projects we do not modify. Everything above that line is written down with the trigger that would justify crossing it.

---

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
