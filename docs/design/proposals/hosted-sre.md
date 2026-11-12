# Architecture proposal: the hosted multi-tenant SRE and security angle

Status: historical proposal for the judge panel, 2026-10-27. One of three design alternatives; the [canonical decisions](../../architecture/00-decisions.md) supersede it. Targets, sizing arithmetic and roadmap dates below are proposal assumptions, not measured results or current commitments. Executor contracts are requirements of the canonical record §2.2; upstream boundaries follow §4.3. Editorial preparation follows [ADR-056](../../adr/ADR-056-documentation-baseline.md), and the publication-rights gate remains open.
Angle: I own the service that runs this for many customers. I optimise for tenant isolation, blast radius, credential hygiene, the ingest SLO, per-tenant limits, unit cost, upgrades and data deletion, while keeping the one-binary self-host profile honest. Every decision below says what it costs at 100 tenants, because that is where naive choices stop working.

Research base: the public-upstream surveys cited inline under `docs/research/`. Citations use the form "see docs/research/<file>.md §n". Items the research marks UNVERIFIED stay UNVERIFIED here; no new verification is claimed.

Naming note: product, plugin and container names never contain Grafana, Loki, Tempo, Mimir or Pyroscope (trademark policy, see docs/research/grafana-stack.md §13.2). Containers are named by role (`metrics-store`, `logs-store`, `traces-store`, `profiles-store`, `dashboard-ui`); the technology column says which upstream project fills the role.

---

## 1. Principles

1. **The tenant is the unit of everything.** One tenant id (`[a-z0-9][a-z0-9-]{0,62}`, valid for all four backends, Kubernetes namespaces and DNS labels; see docs/research/plugin-architecture.md §2.5) is the key for limits, credentials, secrets, sidecars, Slack installations, Grafana org, retention, billing, deletion and audit. If a feature cannot say which tenant it belongs to, it does not ship.
2. **The ingest path never depends on the kernel.** Agents authenticate to a stateless gateway with short-lived JWTs; the only control-plane endpoint on the ingest SLO is token issuance, which runs as its own minimal process. A kernel deploy, migration or outage must not produce a single 4xx on the write path (a 4xx at the agent is data loss, not a retry; see docs/research/gap-tenant-gateway-and-agent-enrolment.md §1.1).
3. **Shared fate is a budget, not a default.** In-process kernel plugins are first-party Go only. Anything that executes model-chosen actions, holds a tenant's write credential in memory, or comes from a third party runs out-of-process, per tenant where it holds credentials, with scale-to-zero where it is idle most of the time.
4. **One stateful dependency for the kernel.** PostgreSQL (SQLite on single node) carries state, jobs, scheduler and the event outbox. No Kafka, Redis or message broker in the kernel. The object store the backends already need doubles as blob storage.
5. **Cells bound blast radius and upgrades.** A cell is one complete hosted stack in one region (gateway, backends, kernel, dashboard pools). Tenants are pinned to a cell. Nothing is global except DNS and a tenant-to-cell directory. Upgrades roll cell by cell, pool by pool, cohort by cohort.
6. **Fail closed on tenancy, fail open on features.** Missing or ambiguous tenant context is a hard reject everywhere (gateway, kernel API, chat surface, approval). A failed optional plugin degrades one row for one tenant and is reported; the failure matrix is published and tested (see docs/research/dsh-architecture.md §5.4).
7. **Operator-visible means logged.** Every alert, query result, decision, approval, tool call and remediation receipt is an append-only event in the per-tenant audit chain, PII-scrubbed at the chokepoints (ingest, model input, egress to chat and notifications, artifacts).
8. **Self-host is the same binary and the same composition machinery.** The hosted profile adds rows (Envoy renderer, per-tenant sidecar supervisor, cell directory, billing meters); it uses the same supported plugin interfaces. Anything that only works hosted is a bug.
9. **Reuse boring OSS behind an extension point, keep the swap local.** HolmesGPT, GoAlert, Alloy and Tetragon are adopted as plugins with conformance suites, so replacing one changes one row.
10. **Cost is a design input.** Every per-tenant resource has a line in the fixed-cost table (§5.6). The target at 100 tenants is at most one always-on pod per tenant, trending to zero once the native on-call module lands.

### 1.1 What breaks at 100 tenants (and where this proposal handles it)

Assumptions for the arithmetic: 100 tenants, 500 agents per tenant on average (50,000 agents), access-token TTL 15 min, one investigation per tenant per hour at peak, 25 tenants per dashboard pool.

| Pressure point | What breaks with the naive design | Handled in |
|---|---|---|
| Token issuance | 5 `oauth2` blocks per agent refreshing every 15 min = 278 token requests/s. Verifying `client_secret` with Argon2id at 30 to 60 ms each needs 10 to 16 cores for an endpoint that must never be down. | §2.7: secrets are 256-bit random, stored as HMAC-SHA256, verified in microseconds; the `auth` process is separate and stateless |
| Fleet polling | 50,000 `GetConfig` calls per minute (833/s), each a DB read if done naively; `RegisterCollector` failure at startup stops an agent polling until restart (see docs/research/gap-remotecfg-server-build-vs-adopt.md §0) | §2.6: in-memory serve cache keyed by `(agent_id, hash)`, DB-outage cache, fleet API in the edge process |
| Investigator sidecars | One always-on Python process per tenant = 100 processes idle most of the day | §2.2: per-tenant instancing with scale-to-zero; investigation is an async job that wakes the pod |
| On-call sidecars | One GoAlert per tenant = 100 deployments plus 100 databases to upgrade, monitor and back up | §2.3: GoAlert ceiling of 40 hosted tenants, native module committed for phase 2 |
| Dashboard UI | One shared instance means one upgrade, one plugin install and one incident affect every tenant | §2.8: pools of 25 orgs, pool is the upgrade unit |
| Per-tenant limits files | Four overrides files with 100 blocks each; a malformed render takes `traces-store` down because it reads a single file with no keep-last-value | §2.7 and §5.4: renderer validates before writing; the other three use `optional-keep-last-value-on-failure` |
| Alert storms | One tenant's 5,000-pod cluster failing produces thousands of alerts in a minute and starves the others' webhooks and LLM budget | §2.5: per-tenant token bucket, storm mode, 429 with `Retry-After`, never drop |
| LLM provider quotas | One provider key shared by 100 tenants; one tenant's burst trips the provider rate limit for everyone | §2.13: per-provider global limiter with per-tenant fair share, optional per-tenant upstream keys |
| Data deletion | Three of four backends have no tenant-delete API; profiles cannot be erased per tenant until compaction | §5.5: deletion saga with per-step status; contractual language; dedicated bucket for regulated tenants |
| Secrets | A single KEK means one key compromise exposes every tenant | §5.3: envelope encryption with one DEK per tenant; deletion is crypto-shredding |
| Slack | One signing secret and one client secret for the whole hosted app | §2.4: accepted and recorded; per-installation bot tokens are tenant-scoped; rotation decision before first install |
| Sidecar upgrades | 100 per-tenant image rollouts per release | §5.7: tenant cohorts (`canary`, `stable`), rollout by cohort, automatic halt on error ratio |
| Kafka in the hosted write path | The metrics and traces backends need Kafka in microservices mode (see docs/research/grafana-stack.md §12.3); one cluster per cell is shared by every tenant's writes | §3: per-tenant ingestion limits at the distributors are the fence in front of Kafka; shuffle sharding on by default; Kafka is per cell, never per tenant |

---

## 2. Decisions

Each subsection states the choice, the evidence, and the hosted consequence. Decisions D1 to D14 map to the numbered open questions in the brief.

### 2.1 D1: Kernel language and plugin mechanism

**Choice.** Go kernel with a compile-time module registry for in-process plugins, a gRPC-over-UDS (local) or gRPC-over-mTLS (Kubernetes) adapter per extension point for out-of-process plugins, and dsh-style YAML patch-layer profiles for composition. Wasm (Extism on wazero) is phase 2, limited to `approval-policy` and `report-renderer` templates. No Python kernel, no TypeScript on Cordis, no Go `plugin` package, no hashicorp/go-plugin for kernel plugins.

**Which parts run in-process.** Only first-party Go plugins on the hot path or holding no tenant write credentials: `gateway` renderer, `auth`, `tenant`, `fleet`, `signal-ingest.*`, `correlator`, `scheduler`, `events`, `store.*`, `secrets`, `llm` gateway, `notifier.*`, `chat-surface.slack-*`, `datasource.*` (read-only query adapters), `approval` service, `change-ledger`, `audit`, `limits-renderer.*`, `on-call-provider.pagerduty|opsgenie` (thin HTTP adapters), the Grafana app proxy target.

**Which parts run out-of-process.** `investigator.*` (HolmesGPT and any Python pillar), `remediation-executor.*` (holds write credential leases), `report-renderer.pdf` (headless Chromium), `on-call-provider.goalert` (a whole product), `tool.*` implementations that run commands, and every third-party plugin regardless of language. Manifest field `instancing: per-tenant` is mandatory for anything that holds tenant credentials in memory.

**Evidence.** The decision matrix in docs/research/plugin-architecture.md §3 scores the compile-time registry 159 and the gRPC sidecar 186 out of 215; the recommendation (§0, §4.1) is the union, which is also what Alloy, the OTel Collector, Grafana and Dapr each ship. Cordis's own README says its API "is not yet stable and may change without notice" (§2.3). The dsh composition semantics worth porting are language-neutral (see docs/research/dsh-architecture.md §8.1); the Proxy context, `!!js` YAML and ESM HMR are not (§8.2). Plain gRPC/UDS rather than go-plugin because our sidecars are started by the orchestrator, not launched by the host (see docs/research/gap-kernel-hot-path-and-grafana-ui-integration.md §4.3).

**Hosted consequence.** A panic in an in-process plugin is recovered at the kernel boundary and marks that row `Failed`, but memory and CPU are shared. Therefore the hosted profile runs the same binary as three deployments with different row sets (§3): `kernel-edge` (auth, fleet agent API, JWKS), `kernel-api` (HTTP/gRPC API, signal ingest, chat receivers, Grafana proxy target), `kernel-worker` (River workers, scheduler leader, reconcilers, sidecar supervisor). A bad correlation job cannot take down token issuance. Self-host runs all rows in one process.

### 2.2 D2: Investigator runtime

**Historical choice.** HolmesGPT as `investigator.holmes`, behind a replaceable investigation interface. The platform owns the transcript and retains authorization, credential custody, typed actions, verification and audit outside the investigation runtime. The current default harness differs: see the canonical record §2.1 row 2. The requirements relevant to every executor are in §2.2.

**Evidence.** HolmesGPT 0.45.0 is Apache-2.0 and CNCF Sandbox, with a stateless `/api/chat` SSE interface, an `approval_required` event and `tool_decisions` resume (see docs/research/gap-kernel-hot-path-and-grafana-ui-integration.md §4.1; docs/research/oncall-and-aiops-oss.md §B.1). These are public integration facts, not a performance result or proof that the platform's executor contracts are satisfied.

**Hosted consequence.** HolmesGPT's toolset and model configuration are process-level and its admin endpoints are unauthenticated, so one shared instance would be a cross-tenant boundary violation. The hosted profile runs it `instancing: per-tenant`, in the tenant's namespace, with a NetworkPolicy allowing only `kernel-worker` ingress and only the kernel LLM gateway and the tenant gateway as egress. It gets exactly two credentials: an LLM virtual key for its tenant (§2.13) and a read-only service JWT for its tenant's datasources. It never sees write credentials (§2.14). Scale-to-zero after 15 minutes idle; the `investigate` River job triggers the scale-up and tolerates cold start because investigation latency is minutes by nature (p50 target 3 minutes, see docs/research/gap-kernel-hot-path-and-grafana-ui-integration.md §2.5). Concurrency per tenant defaults to 2 investigations; the per-tenant LLM budget is the second fence. Exit criterion for replacing it: if the stateless API cannot carry typed execution plans, the kernel wraps tool invocation itself and uses HolmesGPT only as planner (same exit clause as the gap report).

### 2.3 D3: On-call

**Choice.** GoAlert embedded behind the `on-call-provider` extension point for MVP, with `pagerduty` and `opsgenie` adapters next to it, and a **committed** native schedules and escalation module in phase 2 (Q3 2027), not "if it earns its place". The hosted tier runs one GoAlert per tenant up to a hard ceiling of 40 tenants on GoAlert; tenants 41 and later either bring PagerDuty or Opsgenie or wait for the native module. Push notifications via ntfy through GoAlert's webhook contact method.

**Evidence.** Grafana OnCall OSS is archived, AGPL, and heavy (Django, Celery, Redis, MySQL); GoAlert is Apache-2.0, one Go binary on PostgreSQL, actively released, with native Alertmanager and Grafana integration keys and a GraphQL API (see docs/research/oncall-and-aiops-oss.md §A.1 to §A.4). GoAlert has no org concept, so it is single-tenant by construction (§A.2). The gap report confirms v0.35.0 and the "one GoAlert per tenant as a sidecar with its own database" shape (see docs/research/gap-kernel-hot-path-and-grafana-ui-integration.md §4.2).

**Why the native module is committed rather than conditional.** Every hosted tenant adds a GoAlert deployment, a database on the shared cluster, a Twilio configuration, an upgrade, a backup and a monitor. At 100 tenants that is 100 of each for a feature that is roughly ten tables and timer math. The self-host profile keeps GoAlert as its default for as long as it is maintained. The native module ports GoAlert's `schedule`, `override`, `escalation` and `oncall` package semantics (Apache-2.0 permits it) onto kernel tables with a `tenant_id` column and River timers, and reuses the same `notifier.*` rows. No UI port; schedules render in the Grafana app plugin and Slack.

**Hosted consequence while on GoAlert.** The `tenant` reconciler creates the GoAlert deployment and database in the tenant create saga; `WhoIsOnCall` is cached in `store.kv` for 60 s so `/aiops oncall who` never waits on a sidecar; GoAlert's own web UI is reachable only through the kernel proxy with the tenant's identity, never directly; its database is on the shared kernel PostgreSQL cluster as a separate database with its own role, so per-tenant backup and deletion are one `DROP DATABASE`.

### 2.4 D4: Slack runtime

**Choice.** `chat-surface.slack` is a Go plugin inside the kernel on `slack-go/slack`, with two receivers behind one handler core: `slack-events` (Events API, hosted default) and `slack-socket-mode` (self-host default). No Bolt sidecar. The hosted tier owns one distributed Slack app installed per customer workspace or Grid org through OAuth v2; self-hosters create their own app from a manifest we generate.

**Evidence.** The Bolt `Assistant` helpers wrap `assistant.threads.setStatus`, deprecated for February 2027, while slack-go v0.30.0 already has the Agent Sessions API, the three agent events and `authorizations` parsing (see docs/research/gap-slack-chatops-tenancy-and-runtime.md §1, §8). Socket Mode is excluded from the Marketplace and an unlisted distributed app is throttled to 1 request per minute on `conversations.replies`, which an agent that reads thread context cannot live with (§3). Everything that makes the surface safe (installation to tenant routing, user linking, `approvals.decide`, L3 quorum, external-user rejection, digesting) is kernel policy, so a sidecar would round-trip for every message (§8).

**Hosted consequence.** The receiver is a thin always-200 shim: verify signature, dedupe on `event_id`, write to the outbox, ack within 3 s; all work happens in `kernel-worker`. Route on `authorizations[0]`, never on the inner event team (§4). Bot tokens are tenant-scoped secrets; the app-level signing secret and client secret are global to our app and are the one shared blast radius on this surface, recorded in §8. Approve and Deny buttons carry only `approval_id` plus a nonce, are limited with `visible_to_user_ids` to linked `approvals.decide` holders, and quorum is counted on `platform_user_id` with `verified_for_approvals = true`, so two Slack identities of one human cannot form L3 (§5). Externally shared channels are refused unless explicitly bound. Token rotation for the hosted app is decided before the first customer install (UNVERIFIED whether the Marketplace requires it, §10).

### 2.5 D5: Who receives alert webhooks

**Choice.** The kernel's `signal-ingest.*` plugins receive every Alertmanager, Grafana-managed and vendor webhook, then route to the on-call provider. The on-call provider's own inbound integrations (GoAlert's Alertmanager and Grafana keys) are not exposed to tenants.

**Evidence.** Idempotency, per-tenant quota, storm mode, deterministic correlation, PII scrub and the audit event all have to happen exactly once and before anyone is paged; the ingest envelope (ack p99 250 ms after durable write, 2,000 alerts/s per node, 50/s sustained per tenant, 429 with `Retry-After`, never drop) is specified in docs/research/gap-kernel-hot-path-and-grafana-ui-integration.md §2.5, and Alertmanager retries forever until `group_interval` cancels, which only a fast dumb ack survives. Grafana IRM discards on 429 and does not replay; we do better by writing the raw group before anything else. Paging on raw alerts would also page on every re-send (Alertmanager re-sends on every `group_interval`).

**Hosted consequence.** Webhooks are authenticated per tenant: a per-tenant webhook token in the path plus the Grafana HMAC signature when present (5 minute timestamp window). The per-tenant bucket is checked before the insert; a tenant in storm mode (more than 500 alerts per minute) gets one `storm` incident, digest-only notifications and no LLM investigation until the storm clears. Mimir Alertmanager runs multi-tenant per cell; each tenant's contact point is provisioned by the `tenant` plugin to `https://api.<cell>/v1/ingest/alertmanager/<tenant>/<token>`. Grafana-managed alerting in the tenant's org uses the same contact point. `on-call-provider.Page` is called from a River job after incident open or join, with the incident id as the idempotency key.

### 2.6 D6: Remote config and fleet server

**Choice.** Build the `fleet` plugin on `connect-go` implementing `collector.v1.CollectorService` (three RPCs), with OpAMP as a second listener in the same plugin in phase 2 (`opamp-go`). Do not adopt shepherd, alloy-fleet-manager or opsplane as a dependency; use shepherd as the conformance oracle and design reference. Add an optional `fleet-hostagent` binary for VM and Windows upgrades and support bundles.

**Evidence.** The wire contract is three unary RPCs, the server always returns the full config and an opaque hash, the agent keeps its last-good config on any error (see docs/research/gap-remotecfg-server-build-vs-adopt.md §0, §1). All three community servers are single-maintainer; shepherd is Kubernetes-only, needs PostgreSQL 16, is pre-1.0 and would be a second tenancy, auth and UI product beside the kernel (§2, §3). Alloy has no self-updater and `/-/support` is local-only (§4.3, §4.4). The Alloy OTel engine can only be managed over OpAMP, and Alloy's status messages are copies of OpAMP's, so one data model serves both (§5).

**Hosted consequence.** `fleet-agentapi` runs in `kernel-edge` alongside `auth`, because `RegisterCollector` failing at startup silences an agent until Alloy restarts. A serve cache keyed by `(agent_id, hash)` answers `not_modified` without touching PostgreSQL; a DB-outage cache keeps serving the last computed config for every known agent. `effective_config` is stored only when changed. The baseline self-monitoring pipeline is injected into every served config and writes to the tenant's own stores with a `fleet_collector_id` label, which gives inventory and health at no control-plane cost. Validation gates run in `kernel-worker` before a revision can be served: syntax, `alloy validate` with pinned binaries, per-`collector.version` schema check, then canary by matcher subset with automatic disable of a revision whose `FAILED` ratio exceeds a threshold (§4.2). Pillar plugins contribute config fragments through `fleet.config-source`, which is how SecOps adds a Tetragon export pipeline or FinOps adds OpenCost scraping without touching the fleet code.

### 2.7 D7: Tenant gateway and agent enrolment

**Choice.** Hosted: stateless Envoy with `jwt_authn`, `claim_to_headers` writing `X-Scope-OrgID` from the signed `orgid` claim, JWKS from a file the control plane publishes, `forward: false`, fail closed, no body buffering for auth. Single node: an in-process Go reverse proxy with the same route table and the same conformance suite. Credentials: one OAuth2 client per agent (`client_id` = agent id, `client_secret` written once to `/etc/aiops-agent/client_secret`), minted by `POST /v1/enrol` in exchange for a tenant-scoped bootstrap token. Access tokens are JWTs with TTL 15 minutes and claims `{iss, sub, tenant, orgid, scope[], aud, exp, jti}`. Tenant id is the `orgid` claim; write-scoped tokens never carry a pipe.

**Evidence.** None of the four backends authenticates; the vendor charts forward whatever header the client sends or map a static htpasswd user (see docs/research/gap-tenant-gateway-and-agent-enrolment.md §2.1). Envoy `jwt_authn` is the only candidate with in-proxy verification, a JWT cache and built-in header hygiene; `ext_authz`, NGINX `auth_request` and Traefik `forwardAuth` all put a service on every request or need a cache bolted on (§2.2). Alloy already implements the client-credentials grant in every write block (§1.1). The enrolment flow and the "the kernel is not the proxy" resolution are §1.3 and §3.

**One correction to the gap report.** §1.4 proposes Argon2id for the per-agent client secret at rest. That is wrong for the hosted scale (§1.1 of this proposal): the secret is 256 bits of CSPRNG output, not a human password, so a fast keyed hash (HMAC-SHA256 with a server-side key from `secrets`) is the correct primitive and verification costs microseconds. Argon2id remains correct for human passwords, which the kernel does not store at all (identity is delegated to OIDC).

**Hosted consequence.** The `auth` rows (`/oauth2/token`, `/.well-known/jwks.json`, `/v1/enrol`) run in `kernel-edge`, three replicas minimum, with a read replica or in-memory client cache so token issuance survives a primary failover. Signing keys rotate monthly with an overlap window longer than the longest token TTL; the JWKS file is mounted into Envoy (`local_jwks`) so a stale remote fetch cannot matter (UNVERIFIED whether Envoy keeps serving on a failed `remote_jwks` refetch; `local_jwks` sidesteps it). Envoy `jwt_cache_size` is set to at least five times the active agents per replica. Body limits align end to end with the distributors. Access logs carry `orgid` and bytes for billing and abuse detection. Bootstrap tokens have TTL, `max_uses` and optional CIDR allow-lists; cloud instance identity (AWS IID, Azure MSI, GCP identity token) as an alternative to bootstrap tokens is phase 2 for autoscaled fleets (§8 open question 3). Traces use OTLP/HTTP at the edge so every write path is plain HTTP/1.1 POST; gRPC 4317 is not exposed in MVP.

### 2.8 D8: Hosted dashboard tenancy

**Choice.** One `dashboard-ui` org per tenant on shared instances, grouped into **pools of 25 tenants**; instance-per-tenant is an opt-in isolation tier. The Grafana app plugin is a thin proxy (`CallResource` to the kernel API, `CheckHealth`, settings page) with no business logic. Self-hosters are one org mapped to tenant `default`.

**Evidence.** Orgs isolate folders, dashboards, data sources, alerts, notification channels, annotations, service accounts and teams; auth providers, config and licenses are instance-wide, and the docs frame orgs as cheaper than instances (see docs/research/grafana-stack.md §10.2; docs/research/gap-kernel-hot-path-and-grafana-ui-integration.md §3.4). Grafana OnCall's own plugin backend was exactly such a proxy (§3.1). Identity reaches the kernel as the signed `X-Grafana-Id` token verified against `/api/signing-keys/keys`, the tenant comes from app-instance settings bound at install, and basic roles are only a floor; approver bindings and quorum live in the kernel (§3.2).

**Why pools.** A shared instance is one upgrade, one plugin install (instance-wide), one database and one auth configuration for every tenant on it. Pools of 25 bound that: a pool is upgraded, re-signed (private signature with `rootUrls` per pool) and restored as a unit, and a tenant's pool is an attribute the tenant reconciler uses. Four pools at 100 tenants, four Grafana upgrades per release, each touching a quarter of the fleet. Hosted login is our IdP through generic OAuth; org membership is provisioned by the `dashboard-sync` plugin through the org API because `org_mapping` is OSS-only and we cannot rely on it for pooled instances. Alert state history goes to the tenant's `logs-store` through the gateway, not to instance-level config.

**Instance-per-tenant tier.** For tenants that need their own auth provider, their own plugin set, or bring their own Grafana. The `datasource.dashboard-api` row carries `{url, org_id, service_account_token}`, so the kernel does not care which it is. Provisioned through the grafana-operator `Grafana` CR (CRD coverage beyond `Grafana` and `GrafanaDashboard` is UNVERIFIED; see docs/research/grafana-stack.md §15).

### 2.9 D9: Kernel state

**Choice.** PostgreSQL in every profile except `single-node` (SQLite, WAL, one writer). River on the same database for jobs, periodic schedules, unique jobs and leader election. Events are a transactional outbox table consumed by River workers with `LISTEN/NOTIFY` as a doorbell only. No Kafka, no Redis. NATS JetStream is an optional `events.nats` row for hosted fan-out once a load test shows `LISTEN/NOTIFY` is the bottleneck. Kafka appears only as an optional outbound `sink.kafka` row for customers who want the event firehose.

**Evidence.** docs/research/gap-kernel-hot-path-and-grafana-ui-integration.md §2.1 to §2.4: River v0.50.0 (MPL-2.0) gives transactional enqueue, periodic jobs, unique jobs and leader election; the data-plane Kafka is a partitioned WAL with Produce-only semantics, absent from three of four profiles, and its volume case does not exist (hundreds of alerts per five minutes per tenant versus River's tens of thousands of jobs per second). Redis 8 is tri-licensed and adds a third stateful system for nothing PostgreSQL cannot do at this volume.

**Hosted consequence.** One PostgreSQL cluster per cell for the kernel (HA via an operator or a managed service, PITR enabled, connection pooling in front because `kernel-api` and `kernel-worker` replicas multiply connections). Every table carries `tenant_id`; row-level security is enabled as a second fence with the tenant set per transaction from request context, so a missing `WHERE tenant_id` in a plugin query returns nothing rather than everything. Each plugin owns its schema and migrations (expand then contract, so N and N-1 kernel versions can run during a rollout). Per-tenant fairness is a token bucket in `store.kv` checked at job start; jobs over budget snooze (River OSS has no per-queue concurrency limits; River Pro or a NATS work queue is the upgrade path, marked in code). GoAlert databases live on the same cluster as separate databases with separate roles. Backups are per cell; per-tenant export is a kernel job that writes the tenant's rows to the object store, used both for customer export and for cell migration.

### 2.10 D10: eBPF sensor bundle

**Choice.** `beyla.ebpf` (RED metrics, traces, network flows) and `pyroscope.ebpf` (whole-host profiles) inside the privileged Alloy DaemonSet; Tetragon as a separate `sensor` DaemonSet for runtime security; Falco as an optional, mutually exclusive plugin for tenants that want the curated rule library. Kernel floor Tier A: Linux 5.10 or later with BTF, amd64 or arm64, cgroup v2; Tier B best effort: RHEL family 4.18.0-348 or later; Tier C: no eBPF components, selected automatically by the config generator. No BTF mirror. In-kernel enforcement off by default. Ratifying the gap report's Tetragon default over the earlier Falco assumption.

**Evidence.** docs/research/gap-ebpf-sensor-bundle-and-kernel-floor.md §1 (decisions), §2 (Beyla is a supervised child process of Alloy since 1.19, the profiler is in-process), §3.1 to §3.2 (Tetragon runs non-root with an explicit capability list, filters in-kernel, has namespaced policy CRDs and the only published overhead numbers; Falco needs curated rules authored by nobody else, which is the flip condition), §4 (floor), §5 (a mirror would unlock only Tetragon on kernels at or past end of support), §6 (two DaemonSets, not Datadog's one-pod sidecar).

**Hosted consequence.** Tetragon events land in the tenant's `logs-store` through the gateway and count against the tenant's logs ingestion limit; the default export set is exec and exit only, with a documented per-node volume estimate before a tenant enables kprobe policies. Starter `TracingPolicy` pack and the server-side detection rules (ruler rules over Tetragon events) are new work owned by the SecOps phase. The profiler is in-process in Alloy, so a profiler crash restarts Alloy; it is behind a feature flag and the restart policy lives in systemd and Kubernetes. Tier C clusters (Autopilot, Fargate) run only the non-privileged collectors.

### 2.11 D11: Agent packaging

**Choice.** A features YAML (`features: {hostMetrics, logs, otlp, ebpf.traces, ebpf.profiles, security.tetragon, ...}` plus destinations) is the only customer-visible surface; the fleet plugin renders it into `config.alloy` and serves it over `remotecfg`. Kubernetes layout follows the k8s-monitoring chart pattern: `agent` DaemonSet (privileged preset: node metrics, logs, OTLP receiver, `beyla.ebpf`, `pyroscope.ebpf`), `agent-metrics` clustered StatefulSet (cluster metrics, annotation discovery, operator objects), `agent-singleton` (events, anything exactly-once), `sensor` DaemonSet (Tetragon) with `tetragon-operator`, optional `obi-k8s-cache` above 50 nodes. Host layout: two systemd units (`alloy`, `tetragon`) and the optional `fleet-hostagent` unit; the local `config.alloy` is a 15-line `remotecfg` block. Windows: `prometheus.exporter.windows` plus `loki.source.windowsevent` only, MSI install, no eBPF, no network or security sensors.

**Evidence.** Datadog's customer-facing surface is an API key, a site, tags and a handful of feature flags, and the k8s-monitoring chart already works as `features.*` to generated config (see docs/research/datadog-agent.md §7 points 2 and 3; docs/research/grafana-stack.md §3.8). The enrolment flow and the local config shape are docs/research/gap-tenant-gateway-and-agent-enrolment.md §1.3; the privileged layout is docs/research/gap-ebpf-sensor-bundle-and-kernel-floor.md §6; Windows parity is structurally impossible without proprietary drivers (docs/research/datadog-agent.md §1.5, §7 point 5; docs/research/gap-ebpf-sensor-bundle-and-kernel-floor.md §7).

**Hosted consequence.** Kubernetes enrolment is one `Enrol` call by a Helm pre-install job per cluster producing a Secret for every Alloy pod; per-pod `id` is cluster plus pod name. Package integrity: the install script verifies the Alloy package sha256 returned by `Enrol`, and the `fleet-hostagent` only installs versions on the tenant's allow-list from signed release artifacts. The generator is a pure function (features document plus host facts in, config out), unit-tested without a cluster, which is also what makes the Tier A/B/C gating testable.

### 2.12 D12: Pillar scope for MVP

**Choice.** All seven pillars exist as plugin categories from day one (the `investigator` extension point carries `Pillar()`, and Slack routing, budgets and audit are pillar-aware), but MVP ships real machinery for **Incident, Change and Compliance** plus chat-ops and on-call. FinOps (OpenCost, krr), SecOps (Tetragon policy pack, Trivy Operator, Kubescape, Prowler), Continuous Improvement (SLO rules via Sloth or Pyrra, profile regression) and Executive Reporting (image renderer plus templates plus PDF) are later plugins in the roadmap (§7).

**Requirements and public inputs.** The canonical record §2.1 row 12 requires pillars to have real signal or tool inputs and phases Incident, Change and Compliance first. The public building-block survey is docs/research/oncall-and-aiops-oss.md §B.10.

**Hosted consequence.** Each pillar that needs a sidecar is a per-tenant cost line; shipping three keeps the idle footprint at one pod per tenant (§5.6). Change in MVP means the change ledger as a core contract (§2.14) plus a collector that turns Argo CD, Flux, Atlantis and Renovate events into change records and annotations; Compliance in MVP means PolicyReport and Kubescape ingestion, a control-to-evidence mapping and a scheduled score, with fixes becoming change requests rather than executing.

### 2.13 D13: LLM access

**Choice.** Per-tenant budgets in the kernel's own `llm` gateway. No embedded LiteLLM. The gateway exposes an OpenAI-compatible endpoint with per-tenant virtual keys, routes to `llm-provider.*` rows (OpenAI-compatible, Anthropic, local vLLM or Ollama for self-host), meters tokens from the provider usage fields, enforces a daily USD cap, requests per minute, tokens per minute and concurrency per tenant, and logs prompt hashes to the audit chain. A tenant can bring its own provider key (stored in tenant secrets; budgets still enforced by count). LiteLLM remains usable as an upstream `llm-provider.openai-compatible` for people who already run it.

**Evidence.** LiteLLM is a large open-core service with its own PostgreSQL and an `enterprise/` directory (see docs/research/oncall-and-aiops-oss.md §B.8 and the licence notes); the plugin contract is identical either way (docs/research/plugin-architecture.md §5 question 3; §2.5 "plugins never hold LLM credentials"). The budget model (per-key, per-team, duration, TPM and RPM limits, fail-closed) is LiteLLM's and is small to implement in-kernel against tables we already own.

**Hosted consequence.** HolmesGPT is configured with the kernel gateway as its only model base URL and its tenant's virtual key, so the provider key never leaves the kernel and the blast radius of a leaked virtual key is one tenant's budget. A per-provider global limiter with per-tenant fair share prevents one tenant's burst from tripping the provider's rate limit for everyone; large tenants can be moved to their own upstream key. Budget exhaustion fails closed: the investigation job snoozes until the budget window resets and the incident timeline says so. Model routing per tier (cheap model for verification rungs, stronger model for the final turn) is a kernel policy, not a prompt.

### 2.14 D14: Core contracts every executor plugin must honour

**Requirements.** The following executor obligations are governed by the canonical record §2.2. This historical proposal placed them in kernel-side checks; the current record places them in bound policy providers and dispatcher guards (§4.3). A conformance obligation is not a claim that implementation or testing has occurred.

| Contract | Enforcement in the kernel | What the executor must do |
|---|---|---|
| Approval floor | Read verbs are free; every `Apply` requires an `ApprovalRequest` that passed `approval-policy` and quorum. Tenant patterns can only add gates, never remove the floor. | Declare each action's verb class in `Plan` output; refuse `Apply` without a kernel-issued approval token |
| Write-role credential invisibility | Investigators and chat surfaces receive read-only service JWTs. Write credentials are leased by `secrets` to the executor at `Apply` time, scoped to one action, with a TTL of minutes, and are never returned in any response or transcript. | Accept credentials only through the lease API; never log or echo them; release the lease on completion |
| Change ledger before write | The kernel mints the `ChangeRecord` (plan digest, pre-state capture request, rollback plan) and passes `change_id` to `Apply`. `Apply` without a ledger id is rejected at the adapter. | Return `Plan{digest, blast_radius, pre_state_query, rollback_plan}` with no side effects; `Apply` is idempotent on `change_id`; `Rollback` is deterministic from the plan |
| Risk tiers L0 to L3 with distinct-approver quorum | Tier is computed from the plan's blast radius and the tenant's change windows, not from the finding's severity. L3 requires two distinct verified platform users holding `approvals.decide`, neither the requester, re-checked at dispatch. | Report blast radius honestly in `Plan`; the kernel owns the tier table |
| Typed execution plans, digest-bound approval | The approval binds to the plan digest, target and connection revision; `Apply` with a different digest is refused. No free-form shell from the model. | Accept only typed plans from the plugin's own schema |
| Audit chain | Every `Plan`, approval decision, `Apply` receipt and `Rollback` receipt is appended to the per-tenant SHA-256 chain with the plan digest. | Return receipts with stable ids and outcomes (`applied`, `failed`, `uncertain`) |
| PII scrub at chokepoints | The kernel scrubs tool output before it reaches the model, scrubs egress to chat and notifications, and scrubs artifacts. Pseudonyms are salted per tenant and correlation-preserving. | Return raw output; never pre-scrub (the kernel's scrub is the audited one) |
| Verification ladder | After `Apply`, the kernel runs deterministic rungs (alert cleared, typed read-back, cloud alarm state) before any paid agent turn; `failed` triggers the rollback policy, `uncertain` does not. | Provide a `Verify` read-back for the written resource |
| Timeouts and cancellation | Every call carries a deadline and is cancellable; a lost executor marks the change `uncertain`, never `applied`. | Honour context cancellation; make partial `Apply` observable |

**Authority.** The canonical record §2.2 establishes these first-party requirements; §4.3 establishes the service, transaction and upstream boundaries. Chat surfaces supply identity and decisions but cannot grant execution outside those contracts. The Slack integration sketch is docs/research/gap-slack-chatops-tenancy-and-runtime.md §5.

**Hosted consequence.** `remediation-executor.*` rows are `instancing: per-tenant` and run with a NetworkPolicy allowing only the kernel and the target systems the tenant configured. The executor pod gets no standing credentials at all; every `Apply` is a lease. An executor crash leaves at most one `uncertain` change, which the rollback sweep handles.

---

## 3. Container inventory

Legend for presence: H = hosted cell, S = `self-host-full`, A = `aiops-only`, 1 = `single-node`. "Agent side" containers run on customer infrastructure.

### 3.1 Control plane

| Container | Technology | Responsibility | H | S | A | 1 |
|---|---|---|---|---|---|---|
| `kernel-edge` | `aiops` binary (Go), rows: `auth`, `fleet-agentapi`, JWKS publisher | Token issuance, enrolment, agent config serving; the only control-plane endpoints on the ingest SLO; 3+ replicas, no dependency on `kernel-api` | yes | folded into `kernel` | folded | folded |
| `kernel-api` | `aiops` binary, rows: HTTP/gRPC API, `signal-ingest.*`, `chat-surface.slack-events`, Grafana proxy target, `datasource.*`, `llm` gateway endpoint | Everything request-shaped; stateless; scales on request rate | yes | folded into `kernel` | folded | folded |
| `kernel-worker` | `aiops` binary, rows: River workers, `scheduler`, `correlator` fold jobs, reconcilers (`tenant`, `dashboard-sync`, `fleet-revisions`), sidecar supervisor, `chat-surface.slack-socket-mode` in self-host, `notifier.*` | Everything job-shaped; leader-elected periodic jobs; scales on queue depth | yes | folded into `kernel` | folded | folded |
| `kernel` | `aiops` binary, all rows | Single process with every in-process row | no | yes | yes | yes (SQLite) |
| `kernel-db` | PostgreSQL 16+ (HA operator or managed), pgbouncer in front | Kernel state, River, outbox, secrets (envelope-encrypted), GoAlert databases | yes | yes | yes | SQLite file |
| `tenant-gateway` | Envoy (`jwt_authn`, `claim_to_headers`, `local_jwks`), N stateless replicas | Terminates agent and service auth, injects `X-Scope-OrgID`, route table with scope-per-route, body limits, access logs for billing | yes | yes (one tenant `default`, still JWT) | n/a | `gateway.inproc` Go reverse proxy inside `kernel` |
| `cell-directory` | Static DNS plus a one-table service | Maps tenant to cell for the install script, portal and Slack OAuth redirect; the only global component | yes (global) | no | no | no |
| `identity-provider` | Any OIDC provider (we operate one for hosted; self-hosters bring theirs) | Human login for portal, dashboards and the kernel API; we do not own an IdP | external | external | external | optional (local admin token) |
| `events-bus` (optional) | NATS JetStream, `events.nats` row | Low-latency fan-out across replicas, Slack streaming, Grafana Live | optional | no | no | no |

### 3.2 Observability backends (AGPL services behind HTTP, never linked; see docs/research/grafana-stack.md §13.1)

| Container | Technology | Responsibility | H | S | A | 1 |
|---|---|---|---|---|---|---|
| `metrics-store` | Grafana Mimir 3.2 (microservices, ingest-storage with Kafka in H; `-target=all` in S and 1) | Metrics, ruler, multi-tenant Alertmanager; per-tenant overrides file from `limits-renderer.metrics` | yes | yes | customer's | yes |
| `logs-store` | Grafana Loki 3.7 (distributed in H; monolithic in S and 1, prepare for 4.0 HA-monolithic) | Logs, pattern ingester, ruler for Tetragon detections, alert state history per tenant | yes | yes | customer's | yes |
| `traces-store` | Grafana Tempo 3.1 (microservices with Kafka in H; `-target=all` in S and 1) | Traces, TraceQL metrics, service graphs; per-tenant overrides single file, validated before write | yes | yes | customer's | yes |
| `profiles-store` | Grafana Pyroscope 2.4 (object storage mandatory) | Profiles; no per-tenant erasure until compaction (§5.5) | yes | yes | customer's | yes |
| `ingest-log` | Apache Kafka (or compatible), one cluster per cell | Write-ahead log for `metrics-store` and `traces-store` only; never a kernel dependency | yes | no | no | no |
| `object-store` | S3-compatible (cloud bucket in H; MinIO in S and 1) | Blocks and chunks for all four stores, per-tenant prefixes; kernel blobs (reports, bundles, exports) under `aiops/<tenant>/` | yes | yes | yes | local dir |
| `dashboard-ui` | Grafana OSS 13.x, pools of 25 orgs in H; one instance in S and 1 | Dashboards, explore, Grafana-managed alerting, the host for `aiops-app` | yes (pools) | yes | optional | yes |
| `dashboard-db` | PostgreSQL (one database per pool) | Dashboard UI state | yes | yes | optional | SQLite (Grafana default) |
| `aiops-app` | Grafana app plugin: Scenes frontend plus a Go backend of three handlers (`CallResource` proxy, `CheckHealth`, settings) | Tenant UI inside the dashboard UI; private signature per pool in H, unsigned allow-list in S and 1 | yes | yes | optional | yes |
| `image-renderer` | grafana-image-renderer (phase 2) | Panel PNGs for executive reports | phase 2 | phase 2 | phase 2 | optional |

### 3.3 Per-tenant sidecars (hosted) or shared singletons (self-host)

| Container | Technology | Responsibility | H | S | A | 1 |
|---|---|---|---|---|---|---|
| `investigator-holmes` | HolmesGPT 0.45 (Python), `investigator.holmes`, `instancing: per-tenant`, scale-to-zero | RCA runs with tenant-scoped read-only toolsets; speaks only to `kernel-worker`, the LLM gateway and the tenant gateway | per tenant | one shared | one shared | one shared |
| `oncall-goalert` | GoAlert 0.35 (Go) plus a database on `kernel-db`, `on-call-provider.goalert` | Schedules, escalation, SMS and voice via Twilio, ntfy push via webhook contact method; until the native module (phase 2) | per tenant, ceiling 40 | one | one | one |
| `executor-<name>` | gRPC plugin per executor family (`kubernetes-apply`, `argo-rollouts`, `aws-ssm`, `runbook-http`), `instancing: per-tenant` | Plan, Apply, Rollback, Verify under the §2.14 contracts; credentials only by lease | per tenant, on demand | one shared | one shared | in-process allowed |
| `report-renderer` | Headless Chromium plus a template runner (phase 2) | PDF reports | shared per cell | one | one | optional |

### 3.4 Agent side (customer hosts and clusters)

| Container or unit | Technology | Responsibility | k8s | host |
|---|---|---|---|---|
| `agent` DaemonSet / `alloy` systemd unit | Grafana Alloy 1.21 (unmodified), privileged preset: `beyla.ebpf` child process, `pyroscope.ebpf` in-process | Node metrics, logs, OTLP receiver, eBPF traces and profiles; `remotecfg` with per-agent OAuth2 client | yes | yes |
| `agent-metrics` StatefulSet | Alloy, clustered preset | Cluster metrics, kube-state-metrics scrape, annotation discovery, ServiceMonitor and PodMonitor | yes | n/a |
| `agent-singleton` | Alloy, singleton preset | Kubernetes events and anything exactly-once | yes | n/a |
| `sensor` DaemonSet / `tetragon` systemd unit | Tetragon 1.7 plus `tetragon-operator` | Runtime security events to `logs-store`; policies from `fleet.config-source` | yes | yes |
| `obi-k8s-cache` | Beyla/OBI metadata cache | Kubernetes metadata for eBPF above about 50 nodes | optional | n/a |
| `fleet-hostagent` | Small Go binary, systemd or Windows service | Package upgrades from the tenant's allow-list, support bundle upload; optional | n/a | optional |
| `kube-state-metrics`, `node-exporter`, `opencost` (later), `trivy-operator` (later), `kubescape` (later) | Upstream charts bundled by the agent chart | Standard exporters and scanners | yes | partial |

---

## 4. Extension points

Row type in profiles is `<point>.<name>`. Every point has a Go interface in `sdk/ext/<point>`, a proto in `proto/aiops/ext/<point>/v1`, a conformance suite in `conformance/<point>/`, and runs in the runtimes listed. Points marked "new" are additions to docs/research/plugin-architecture.md §4.5 driven by the hosted angle.

| Id | Purpose | MVP implementations | Runtime |
|---|---|---|---|
| `gateway` (new) | Render the route table and JWKS into a gateway artifact; conformance: strip and inject `X-Scope-OrgID`, scope per route, no pipe on writes, fail closed, body limits, `Retry-After` passthrough | `envoy` (hosted), `inproc` (single node) | inproc |
| `limits-renderer` (new) | Pure function from `TenantLimits` to one backend's overrides document; complete tenant block, validated | `metrics`, `logs`, `traces`, `profiles` | inproc |
| `secrets-backend` (new) | Key encryption key provider for envelope encryption | `local-file` (self-host), `aws-kms`, `gcp-kms`, `azure-keyvault` (hosted, one is enough for MVP) | inproc |
| `llm-provider` (new) | Upstream model endpoint behind the kernel gateway | `openai-compatible` (also covers LiteLLM and vLLM), `anthropic`, `ollama` | inproc |
| `fleet.config-source` (new) | Contribute a config fragment to served agent configs, selected by matchers | `baseline-selfmonitoring`, `tetragon-export`, `host-metrics`, `k8s-logs` | inproc |
| `fleet.validation-gate` (new) | Veto a revision before serving | `syntax`, `alloy-validate`, `version-schema` | inproc |
| `datasource` | Tenant-scoped read queries against observability backends and APIs | `prometheus-api`, `logs-api`, `traces-api`, `profiles-api`, `dashboard-api` (org, annotations, silences), `kubernetes` | inproc |
| `signal-ingest` | HTTP endpoints that normalise inbound alerts and findings into `signal/*` events with idempotency and per-tenant quota | `alertmanager-webhook`, `dashboard-alerting`, `generic-webhook`, `policyreport` (Compliance), `argocd-events` and `flux-events` (Change) | inproc |
| `correlator` | Deterministic dedupe, join and fold rules; storm mode | `fingerprint-refresh`, `service-join`, `topology-fold`, `change-window-fold`, `shared-alertname-fold` | inproc |
| `investigator` | Agentic investigation with `Pillar()`; server-streamed findings including `ApprovalRequired` | `holmes` (incident, change, compliance personas) | grpc, per-tenant |
| `tool` | Describe and invoke a tool with declarative prerequisites (HolmesGPT YAML shape) | `promql`, `logql-query`, `traceql-query`, `kubectl-read`, `change-ledger-read`, `mcp-bridge` | inproc, grpc |
| `remediation-executor` | `Plan`, `Apply`, `Rollback`, `Verify` under the §2.14 contracts | `kubernetes-apply`, `runbook-http` | grpc, per-tenant |
| `approval-policy` | `Evaluate(action, context)` to `Allow`, `Deny` or `RequireApproval(approvers, tier)` above the floor | `always-ask`, `cel` | inproc; wasm in phase 2 |
| `notifier` | Outbound messages with capabilities (threads, buttons, ack) | `slack`, `email`, `webhook` | inproc |
| `chat-surface` | Inbound conversation transport, identity mapping, sessions, streaming | `slack-events`, `slack-socket-mode` | inproc; grpc for third-party surfaces |
| `on-call-provider` | `Schedules`, `WhoIsOnCall`, `Page`, `Ack`, `Resolve`, `Overrides` | `goalert`, `pagerduty`, `opsgenie`; `builtin` in phase 2 | grpc (goalert), inproc (adapters) |
| `scheduler` | Tenant-aware distributed cron and delayed jobs | `river` | inproc |
| `storage` | `KV`, `SQL` (per-plugin migrations), `Blob` | `postgres`, `sqlite`, `s3-blob`, `local-blob` | inproc |
| `report-renderer` | Render a report spec plus data to an artifact | `markdown`, `html`; `pdf` in phase 2 | inproc; grpc for pdf |
| `identity` (new) | Verify a human caller: OIDC from the IdP, signed dashboard-UI id token, kernel service tokens | `oidc`, `dashboard-id-token`, `service-token` | inproc |
| `audit-sink` (new) | Export the audit chain to an external system | `object-store` (default), `webhook` | inproc |

Host services available to plugins (unchanged from docs/research/plugin-architecture.md §4.4, with two additions): `logger`, `metrics`, `tracer`, `config`, `secrets`, `tenant`, `store.kv`, `store.sql`, `store.blob`, `scheduler`, `events`, `http.router`, `llm`, `discovery`, `lifecycle`, plus `credential-lease` (one-action write credentials, §2.14) and `audit` (append to the chain).

---

## 5. Tenancy and data model

### 5.1 What the kernel stores (PostgreSQL, every row carries `tenant_id` unless marked platform)

| Area | Tables (plugin owner) | Notes |
|---|---|---|
| Tenants (platform) | `tenant` (id, cell, plan, status: provisioning, active, suspended, deleting, purged; cohort; dashboard pool; region), `tenant_limits` (the `TenantLimits` document, versioned), `tenant_binding` (tenant, subject, role) | Status drives token issuance and the delete saga |
| Identity | `user` (platform user id, OIDC issuer and subject, email), `service_token` (hashed, scopes, tenants, expiry, `jti` denylist for query routes) | No passwords, ever |
| Agents and fleet (`fleet`) | `bootstrap_token`, `agent` (id, tenant, client_id, secret HMAC, status, attrs, last_seen, version, os), `pipeline`, `pipeline_revision`, `served_config` (agent, hash, content, computed_at), `agent_status` (remote_config_status, error, effective_config when changed) | Serve cache in memory; `effective_config` is the reconciliation ground truth |
| Gateway (`auth`) | `signing_key` (kid, public, private encrypted, not_before, not_after) | JWKS rendered from here |
| Signals and incidents (`signal-ingest`, `correlator`) | `raw_group` (30 day retention), `alert` (normalised, fingerprint, startsAt), `incident` (FSM, correlation key, priority, criticality, suspect change, merged_into), `incident_event` (timeline), `service_dependency` (origin trust: declared, traced, discovered, observed) | Idempotency keys as in docs/research/gap-kernel-hot-path-and-grafana-ui-integration.md §2.5 |
| Investigations (`investigator`) | `investigation` (incident, pillar, model, cost, status), `investigation_transcript` (conversation history per step, PII-scrubbed), `finding` | Transcript committed per step so a crash resumes |
| Approvals and changes (`approval`, `change-ledger`) | `approval_request` (plan digest, tier, required quorum, TTL), `approval_decision` (platform user, source, verified), `change_record` (plan, pre-state, rollback, receipts, risk tier, FSM), `change_evidence`, `verification` (rung, verdict, evidence signature) | Approval state and dispatch checks follow canonical record §2.2 |
| Compliance (`compliance`) | `control`, `control_result`, `finding` (normalised scanner findings, monotonic risk), `risk_decision`, `posture_snapshot`, `attestation` | Fixes become change requests |
| Chat (`chat-surface.slack`) | `slack_installation`, `slack_installation_workspace`, `slack_channel_binding`, `slack_user_link`, `slack_thread_session`, `slack_interaction_log` | Shapes from docs/research/gap-slack-chatops-tenancy-and-runtime.md §9 |
| On-call | `oncall_provider_binding` (tenant, provider row, external ids), `oncall_cache` (who is on call, 60 s TTL) | Schedules live in GoAlert until the native module |
| LLM (`llm`) | `llm_virtual_key` (tenant, plugin, hashed), `llm_budget` (window, cap, spent), `llm_usage` (per call: model, tokens, cost, prompt hash) | Budget check is a transaction with the usage insert |
| Secrets (`secrets`) | `secret` (tenant, plugin, name, ciphertext, DEK id), `tenant_dek` (wrapped by the KEK from `secrets-backend`) | One DEK per tenant; deletion is crypto-shredding |
| Audit (`audit`) | `audit_event` (append-only, per-tenant SHA-256 chain), `audit_chain_head` | ORM guards and a database trigger block update and delete |
| Jobs and events | `river_*`, `event_outbox` (tenant, seq, type, payload, published_at) | Same transaction as the state change |
| Dashboard mapping (`dashboard-sync`) | `dashboard_org` (tenant, pool or instance url, org_id, service account token ref, app token ref) | One tenant to many orgs allowed; one org to exactly one tenant |
| Billing meters (platform, hosted only) | `usage_hourly` (tenant, metric: active series, log bytes, trace bytes, profile bytes, alerts, LLM USD, agents) | Derived from backend metrics and gateway access logs |

### 5.2 What lives in the observability backends

| Data | Store | Tenant key | Retention control |
|---|---|---|---|
| Metrics, recording and alerting rules, Alertmanager config | `metrics-store` | `X-Scope-OrgID` | `compactor_blocks_retention_period` per tenant |
| Logs, Tetragon events, Falco alerts, agent self-logs, alert state history | `logs-store` | same | `retention_period` per tenant, compactor retention enabled |
| Traces, service graph and span metrics | `traces-store` | same | `compaction.block_retention` per tenant |
| Profiles | `profiles-store` | same | `retention_period` (per-tenant applicability UNVERIFIED) |
| Dashboards, folders, annotations (change markers), Grafana-managed rules, contact points | `dashboard-ui` org | org id | Org deletion |
| Reports, support bundles, tenant exports | `object-store` under `aiops/<tenant>/` | prefix | Kernel job |

The control plane keeps workflow state and telemetry references; it does not implement a telemetry store. Read tools reach the customer's stores through supported connector interfaces (canonical record §2.1 and §4.3).

### 5.3 Credential classes and blast radius

| Credential | Holder | Scope | Lifetime | Revocation | Blast radius if leaked |
|---|---|---|---|---|---|
| Bootstrap token | Install script | One tenant, enrolment only | Hours to days, `max_uses`, optional CIDR | Delete; enrolled agents unaffected | New enrolments into that tenant |
| Agent client secret | One host, file 0600 | That agent's writes and fleet calls | Long-lived; re-enrol to rotate | `agent.status = revoked` (effective within one TTL) | One host |
| Access token (JWT) | Alloy component memory | One tenant, one scope set | 15 min | Expiry; emergency key rotation | One host for 15 min |
| Service token | Dashboard data sources, CI | One tenant (write) or a pipe list (read, admin only) | Long-lived; `jti` denylist on query routes | Denylist | One integration |
| Dashboard app token | App plugin `secureJsonData` | `(dashboard url, org, tenant)` | Long-lived | Rotate per pool | One org's proxy access |
| Slack bot token | Tenant secrets | One installation | Per Slack (rotation decision pending) | `tokens_revoked`, uninstall | One workspace |
| LLM virtual key | One investigator pod | One tenant's budget | Rotated on pod restart | Delete row | One tenant's LLM spend |
| Credential lease | One executor call | One action | Minutes | Automatic | One action |
| Tenant DEK | Kernel memory, wrapped at rest | One tenant's secrets | Rotate yearly | Re-wrap; destroy to shred | One tenant's secrets |
| KEK | `secrets-backend` (KMS) | All DEKs in the cell | KMS policy | KMS | Cell (the one key we guard hardest) |

### 5.4 Per-tenant limits

`TenantLimits` (see docs/research/gap-tenant-gateway-and-agent-enrolment.md §5.2) is the single document; plans are defaults, overrides are per tenant. The four renderers emit complete blocks. Rendering, reload and verification per backend follow §5.1 of that report: three backends read a file list with `optional-keep-last-value-on-failure`; `traces-store` reads a single file and does not merge with defaults, so its renderer validates and writes atomically. The reconciler reads back `/runtime_config?mode=diff` and `/api/overrides?scope=merged` and records drift as a tenant event. Kernel-side limits in the same document: alerts per second and burst, open alerts, investigations concurrency, LLM budget, agents, Slack digest interval, report frequency. `status: suspended` is enforced by `auth` refusing tokens; backend limits are the secondary guard. Shuffle sharding (`ingestion_tenant_shard_size`) is on by default in hosted so a tenant's series are confined to a subset of ingesters.

### 5.5 Tenant lifecycle: create, suspend, delete

Create is cheap and is a saga in `kernel-worker`: write limits files, provision the dashboard org plus four data sources in the tenant's pool, create the tenant DEK, mint the first bootstrap token, create the GoAlert deployment and database (while on GoAlert), provision the Alertmanager contact point, record `cell` in the directory. Each step idempotent and visible on the tenant resource.

Suspend: `auth` stops issuing tokens (effective within one TTL); sidecars scale to zero; Slack installation marked paused; data retained.

Delete follows the seven-step saga in docs/research/gap-tenant-gateway-and-agent-enrolment.md §5.3: revoke all credentials, remove ruler and Alertmanager configs, request `metrics-store` tenant deletion and poll, set 1 h retention on the other three, run empty-tenant deletion for traces, sweep object-store prefixes including non-current versions, delete the dashboard org, drop the GoAlert database, destroy the tenant DEK (crypto-shredding every secret and every encrypted kernel row at once), keep one audit row. Expected completion is about 19 h for metrics and a few hours for logs and traces; profiles cannot be erased per tenant until compaction because level-0 segments are multi-tenant objects. Consequences for the hosted service: the terms state "deleted within 72 hours, profiles best effort until compaction"; regulated tenants are provisioned with a dedicated object-store bucket for profiles (and optionally for all four stores) so erasure is a bucket delete. Deletion status is a first-class tenant attribute with per-step timestamps, because auditors ask.

### 5.6 Per-tenant fixed cost (hosted, idle)

| Item | Per tenant | Notes |
|---|---|---|
| Dashboard org | 0 pods | Shared pool |
| Overrides blocks | 0 pods | Four YAML blocks |
| Alertmanager config | 0 pods | Multi-tenant Alertmanager in `metrics-store` |
| `investigator-holmes` | 0 pods idle, 1 pod while investigating | Scale-to-zero; the pod's memory footprint is an estimate until measured |
| `oncall-goalert` | 1 small pod plus 1 database | Goes to 0 with the native module |
| `executor-*` | 0 pods idle | On demand per action |
| Kernel, gateway, backends, Kafka | shared | Per-tenant cost is usage: series, bytes, alerts, LLM tokens, all metered in `usage_hourly` |

Target: at most one always-on pod per tenant in MVP, zero after phase 2.

### 5.7 Upgrades

| Layer | Unit | Rule |
|---|---|---|
| Kernel binary | Cell, rolling | Expand-then-contract migrations; River job args backward compatible for one minor; `kernelApi` semver range checked at boot; `kernel-edge` upgraded last and one replica at a time |
| In-process plugins | Same as kernel | Rebuild of the distribution binary (`aiopsctl build`) |
| Per-tenant sidecars | Tenant cohort | `cohort: canary` tenants first (internal and design partners), then `stable` in batches of 10; halt on error ratio; image pins per tenant row |
| Observability backends | Cell, one backend at a time | Vendor charts and upgrade guides; never during a tenant delete saga |
| Dashboard UI | Pool | One pool at a time; app plugin re-signed per pool `rootUrls`; unsigned build for self-hosters |
| Gateway | Cell, rolling | Config changes via rendered artifact and hot reload; JWKS overlap window enforced |
| Agents | Fleet revision | Validation gates, canary by matcher, auto-disable on `FAILED` ratio; package upgrades via `fleet-hostagent` from the tenant's allow-list; Kubernetes via chart bump or GitOps PR |
| Protocols | gRPC `protocol` version in manifest | Kernel refuses a mismatch with a clear error |

---

## 6. Profiles

Composition semantics are the dsh ones (see docs/research/dsh-architecture.md §3.2; docs/research/plugin-architecture.md §4.6): empty root, bundle patches in order, profile patch, site patch, `--patch` overlays, then the tenant overlay for rows with `tenantScope: per-tenant`. Patches replace a row's whole `config`. `aiopsctl profile render --tenant <id> --dump` prints the effective tree and its provenance.

| Profile | Bundles (in order) | What the mode bundle does | Runtime shape |
|---|---|---|---|
| `hosted` (one per cell) | `aiops-base`, `aiops-observability-backends`, `aiops-gateway-envoy`, `aiops-fleet`, `aiops-slack-events`, `aiops-oncall-goalert`, `aiops-pillars-mvp`, `aiops-hosted` | Inserts `cell-directory` client, `secrets-backend.<kms>`, `limits-renderer.*`, `gateway.envoy`, sidecar supervisor with per-tenant instancing, billing meters, `dashboard-sync` with pools, `events.nats` (disabled until measured); sets `required: true` on `auth`, `fleet-agentapi`, `signal-ingest.*`, `tenant`; splits rows across `kernel-edge`, `kernel-api`, `kernel-worker` by a `role` selector | Kubernetes, three kernel deployments, Envoy, backends in microservices mode with Kafka, dashboard pools |
| `self-host-full` | `aiops-base`, `aiops-observability-backends`, `aiops-gateway-envoy`, `aiops-fleet`, `aiops-slack-socket`, `aiops-oncall-goalert`, `aiops-pillars-mvp` | One tenant `default`, still JWT-authenticated at the gateway; `secrets-backend.local-file`; one shared investigator and one GoAlert; `chat-surface.slack-socket-mode` | Kubernetes (Helm) or compose; one kernel process; backends in monolithic mode, no Kafka; one dashboard instance |
| `single-node` | `aiops-base`, `aiops-observability-backends-mono`, `aiops-gateway-inproc`, `aiops-fleet`, `aiops-slack-socket`, `aiops-oncall-goalert`, `aiops-pillars-mvp` | Same as `self-host-full` with `storage.sqlite`, `gateway.inproc`, `store.blob.local`; everything in one binary plus the four backend binaries and a dashboard instance | One VM or laptop; dev, demo, small estates |
| `aiops-only` | `aiops-base`, `aiops-external-datasources`, `aiops-slack-socket` (or `-events`), `aiops-oncall-goalert` (or adapters), `aiops-pillars-mvp` | No backends, no gateway, no fleet; `datasource.*` rows point at the customer's own Prometheus-compatible, logs, traces and dashboard APIs with customer credentials in tenant secrets; alerts arrive by webhook | Kernel plus PostgreSQL plus investigator plus optional GoAlert; can be hosted by us as a tenant whose datasources are external |
| `agent-host` | `agent-base`, `agent-ebpf`, `agent-security` | Renders `features:` into `config.alloy` for a VM: `prometheus.exporter.unix`, journal and file logs, OTLP receiver, `beyla.ebpf` and `pyroscope.ebpf` on Tier A/B, Tetragon unit and export pipeline, baseline self-monitoring; `remotecfg` only locally | `alloy` plus `tetragon` systemd units, optional `fleet-hostagent`; Windows variant with `agent-windows` instead of `agent-ebpf` and `agent-security` |
| `agent-k8s` | `agent-base`, `agent-k8s-layout`, `agent-ebpf`, `agent-security` | Helm chart wrapping the k8s-monitoring layout: `agent` DaemonSet (privileged), `agent-metrics` StatefulSet, `agent-singleton`, `sensor` DaemonSet, optional `obi-k8s-cache`; pre-install job calls `Enrol`; Tier C clusters drop `agent-ebpf` and `agent-security` automatically | Helm release per cluster |

Profile rows marked `required` in every server profile: `api-server`, `tenant`, `store`, `scheduler`, `events`, `audit`. Hosted adds `auth`, `fleet-agentapi`, `signal-ingest.alertmanager-webhook`, `gateway.envoy` renderer. Everything else is best-effort and degrades per row.

---

## 7. MVP scope and phased roadmap

### 7.1 MVP (what proves the product with design partners)

1. Telemetry base: `agent-host` and `agent-k8s` profiles, `Enrol`, fleet with three RPCs and validation gates, Envoy gateway, `TenantLimits` with four renderers, tenant create and delete sagas, dashboard org provisioning in pools.
2. Kernel: Go kernel, compile-time registry, gRPC adapter, profiles and `aiopsctl profile render`, host services on PostgreSQL and River, test kit with fake host and conformance suites for `gateway`, `limits-renderer`, `signal-ingest`, `investigator`, `remediation-executor`, `chat-surface`, `on-call-provider`.
3. Incident pillar: Alertmanager and Grafana webhooks, dedupe and deterministic correlation, storm mode, HolmesGPT investigator per tenant, Slack surface (Events API hosted, Socket Mode self-host) with sessions, streaming and Block Kit approvals, GoAlert on-call with PagerDuty and Opsgenie adapters, `kubernetes-apply` and `runbook-http` executors under the §2.14 contracts, verification ladder, audit chain, PII scrub.
4. Change pillar: change ledger as a core contract, Argo CD and Flux event collectors to change records and annotations, change-window fold in the correlator.
5. Compliance pillar: PolicyReport and Kubescape ingestion, control-to-evidence mapping, scheduled score, findings to change requests.
6. Grafana app plugin: incidents, approvals, changes, compliance score, fleet inventory; unsigned and private-signed builds.
7. Hosted operations: three kernel deployments, cell directory, billing meters, cohorts, backups, runbooks for the delete saga.

### 7.2 Roadmap by quarter

| Quarter | Milestone | Hosted-specific deliverables |
|---|---|---|
| Q4 2026 | Design freeze and kernel skeleton: SDK, manifest, lifecycle, test kit, profiles; `fleet` three RPCs; `auth` and `Enrol`; `gateway.envoy` and `gateway.inproc` with the shared conformance suite; `TenantLimits` and four renderers; `agent-host` and `agent-k8s` generators | First cell on Kubernetes with monolithic backends (Kafka deferred until the first cell carries real load); internal tenant only; token-issuance load test at 300 req/s |
| Q1 2027 | Incident pillar end to end: ingest, correlator, HolmesGPT per tenant with scale-to-zero, Slack both receivers, GoAlert plus adapters, executors with contracts, verification ladder, audit chain, app plugin v0 | Backends to microservices with Kafka; dashboard pools; tenant create and delete sagas verified against all four stores; 5 design-partner tenants on the `canary` cohort |
| Q2 2027 | Change and Compliance pillars; `aiops-only` profile; `single-node` profile with SQLite; Windows agent variant; hosted GA | 25 tenants; billing meters; per-tenant export; first cell-to-cell tenant migration drill; security review of the credential model |
| Q3 2027 | Native on-call module (`on-call-provider.builtin`) replacing GoAlert for hosted; OpAMP listener in `fleet`; `fleet-hostagent`; SecOps: Tetragon policy pack, detection rules, Trivy Operator and Kubescape findings; FinOps: OpenCost scrape and weekly krr run | GoAlert tenants migrated to native; 50 tenants; instance-per-tenant tier; `events.nats` if `LISTEN/NOTIFY` measured as the bottleneck |
| Q4 2027 | Continuous Improvement (SLO rules via Sloth or Pyrra, profile regression review); Executive Reporting (image renderer, templates, PDF, email); Wasm `approval-policy`; Prowler for cloud posture; cloud instance identity for enrolment | 100-tenant load test against the §1.1 table; second cell in a second region; SOC 2 evidence collection from the audit chain |

### 7.3 What is explicitly not in MVP

Native on-call, OpAMP, remote upgrade and support bundle from hosts, FinOps, SecOps detections, CI, executive reporting, Wasm, instance-per-tenant tier, `oauthPassThru` per-user data-plane authorization, Falco plugin, per-pillar separate Slack apps, GKE Autopilot and Fargate eBPF, BTF mirror, Windows network and security sensors.

---

## 8. Top risks and what I refuse to build

### 8.1 Top risks

| Risk | Impact | Mitigation and owner |
|---|---|---|
| Token issuance becomes the de facto ingest SPOF | Every agent stops writing after one TTL if `auth` is down | Separate `kernel-edge` deployment, three replicas, in-memory client cache, HMAC verification, monthly key rotation with overlap, load test at 300 req/s; TTL can be raised to 30 min with a `jti` denylist on write routes if needed (open question in docs/research/gap-tenant-gateway-and-agent-enrolment.md §8) |
| HolmesGPT multi-tenancy and typed plans | Unauthenticated admin endpoints; stateless API may not carry typed execution plans | Per-tenant instancing with NetworkPolicy; exit criterion: kernel wraps tool invocation and uses HolmesGPT only as planner (one row swap); the HTTP API page says "previous version", so 0.45.0 parity is UNVERIFIED and is the first thing the conformance suite pins |
| GoAlert per tenant does not scale operationally | 100 deployments and databases | Hard ceiling of 40, PagerDuty and Opsgenie adapters, native module committed for Q3 2027 |
| Profile erasure per tenant is impossible until compaction | Regulated tenants cannot get a deletion guarantee for profiles | Contract language; dedicated bucket for regulated tenants; hard-delete delay UNVERIFIED, must be measured in Q1 |
| Kafka in the write path | A cell's Kafka incident stops metrics and traces ingest for every tenant in the cell | Per cell, never per tenant; vendor-recommended sizing; the monolithic no-Kafka mode is the documented fallback for small cells |
| Shared Slack app secrets | One signing secret and client secret for all hosted installations | Rotate on a schedule; bot tokens tenant-scoped; the Marketplace listing (10 active installs) and the rotation decision are gating items before the first customer install |
| slack-go and River are volunteer or small-team projects with breaking minors | Build breaks, behaviour changes | Pin and vendor; thin internal interfaces so a replacement is local to one package; River Pro remains a paid upgrade path |
| AGPL boundary erosion | A contributor imports a backend Go package into the kernel | CI deny-list on `github.com/grafana/mimir`, `loki/pkg/logql`, `tempo/pkg/traceql` and friends; only the Apache-2.0 sub-trees named in docs/research/grafana-stack.md §13.1 are allowed |
| Dashboard pool upgrade regressions | A Grafana minor breaks the app plugin for 25 tenants | Pool at a time, canary pool with internal tenants, pinned Grafana line per pool, unsigned-build smoke test in CI; `X-Grafana-Id` forwarding floor is UNVERIFIED and the proxy tolerates a missing header (read paths only) |
| `LISTEN/NOTIFY` with many replicas | Server-wide queue fills, notifies fail at commit | Doorbell only, payload is `(tenant, event_id)`; `events.nats` row ready; load test before Q3 |
| Tetragon detections are new work | SecOps pillar has no out-of-the-box rules | Starter policy pack and ruler rules are a Q3 deliverable; Falco plugin remains the fallback for tenants who need curated rules on day one |
| Cost of per-tenant isolation | Sidecars and pools cost more than a fully shared design | The fixed-cost table (§5.6) is reviewed per quarter; scale-to-zero and the native on-call module are the two levers |

### 8.2 What I refuse to build

- A telemetry store, collector or telemetry cache inside the control plane (canonical record §1 and §4.3).
- Our own identity provider, password store, SCIM or SAML; identity is OIDC from an IdP we do not own.
- A Node or Cordis-based kernel, a Python kernel, Go `plugin` loading, or hashicorp/go-plugin for kernel plugins.
- Per-tenant Kafka, Redis, or any kernel dependency on the data-plane Kafka.
- A BTF mirror, kernel-module drivers, runtime compilation, Windows network or security sensors, Fargate or Autopilot eBPF.
- Modified AGPL service builds or imports of their non-permissive packages; integrations use supported network and plugin interfaces.
- Free-form shell execution from the model, in-kernel enforcement on by default, or any executor that holds standing write credentials.
- A shared HolmesGPT or shared executor across hosted tenants, however cheap.
- One Slack app per pillar as the default, a mobile push app (ntfy covers it), status pages, synthetic monitoring backends, agentless cloud scanning.
- An admin portal beyond `aiopsctl` and the operator org in the dashboard UI for MVP.
- Anything that only works in the hosted profile.

---

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
