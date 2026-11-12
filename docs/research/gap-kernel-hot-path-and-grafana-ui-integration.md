# Gap-fill: kernel hot path (events / store / scheduler), alert-ingest envelope, and Grafana as the UI

Research note, 2026-10-27. This note considers three design questions: (a) which technology backs the `events`, `store` and `scheduler` services in each profile and what the alert hot path must sustain; (b) how the Go control plane attaches to Grafana as its UI (plugin backend role, identity propagation, signing for self-hosters); (c) how to separate investigation, policy, paging and presentation. Public upstream facts carry a URL and the date or version they were checked against; anything not confirmed from a primary source is marked **UNVERIFIED**.

Historical research snapshot: recommendations and targets below are not measurements and are superseded where they differ from the [canonical decisions](../architecture/00-decisions.md). The executor requirements are those of that record §2.2. Editorial preparation follows [ADR-056](../adr/ADR-056-documentation-baseline.md); the publication-rights gate remains open.

## 0. Decisions

| # | Question | Decision | Section |
|---|---|---|---|
| D1 | What backs `store` | PostgreSQL in every profile except `single-node`, which uses SQLite (one writer, WAL). Blob = local dir (single-node) or the S3-compatible bucket the LGTM stack already needs. | §2.2 |
| D2 | What backs `scheduler` | River (Go, MPL-2.0, v0.50.0) on the same Postgres/SQLite: transactional enqueue, periodic jobs, unique jobs, leader election. No separate scheduler process. | §2.2, §2.4 |
| D3 | What backs `events` | A transactional outbox table in the same database, consumed by River workers; Postgres `LISTEN/NOTIFY` as the wake-up, not as the log. NATS JetStream is an optional `events.nats` fan-out plugin for the hosted profile. The kernel does **not** reuse the Kafka that Mimir ingest-storage and Tempo 3 need; Kafka appears only as an optional outbound `sink.kafka` plugin. No Redis. | §2.3, §2.4 |
| D4 | Hot-path envelope | Webhook ack p99 ≤ 250 ms after durable write; dedupe + deterministic correlation p99 ≤ 2 s; first notification p95 ≤ 5 s after incident open; 2,000 alerts/s per kernel node sustained, 50/s sustained and 1,000 burst per tenant by default; 429 + `Retry-After` on quota, never a drop. | §2.5 |
| D5 | Grafana app plugin backend | A thin proxy (`CallResource` → kernel HTTP API), stateless, exactly what Grafana OnCall's plugin did. The kernel is a separate deployment and must run without Grafana. | §3.1 |
| D6 | Identity into the kernel | Kernel verifies Grafana's signed `X-Grafana-Id` token against `${root_url}/api/signing-keys/keys`; `(grafana_url, org_id)` → tenant comes from the app-instance settings; Grafana basic role is a floor; approver bindings and quorum live in the kernel. | §3.2 |
| D7 | Plugin signing | Ship an unsigned build for self-hosters (allow-list), a privately signed build (`rootUrls`) for our hosted Grafana, and submit for a community signature once the API is stable. | §3.3 |
| D8 | Investigation and remediation separation | Historical option: HolmesGPT provides investigation behind `investigator.holmes`. Approval, credentials, change records, verification and audit remain platform requirements, not capabilities delegated to the investigation runtime. | §4.1 |
| D9 | GoAlert vs native on-call | GoAlert embedded behind `on-call-provider` for MVP; native module is phase 2 with the triggers from `oncall-and-aiops-oss.md` §A.4. | §4.2 |
| D10 | go-plugin vs plain gRPC/UDS | Plain gRPC over UDS/mTLS (Dapr-style) for kernel plugins. hashicorp/go-plugin is used in exactly one place, the Grafana app backend, because Grafana is the host there. | §4.3 |
| D11 | Grafana org vs instance per tenant | Org-per-tenant on shared Grafana pools is the hosted default; instance-per-tenant is an opt-in isolation tier. Self-hosters are one org = tenant `default`. The `grafana` datasource row carries `{url, org_id, service_account_token}` so both are one mapping. | §3.4 |

## 1. What the earlier reports already settled (not repeated here)

- Kernel = Go, compile-time registry plus gRPC-over-UDS remote adapter, dsh-style patch-layer profiles; host services named but not backed (`plugin-architecture.md` §4.1, §4.4).
- Durable state changes and scheduled work must survive process restarts; a transient UI event bus is not the durable record. Current transaction and outbox requirements are in the canonical record §4.3.
- Hosted LGTM profile runs microservices with Kafka (Mimir ingest-storage, Tempo 3); self-hosted profile runs monolithic modes without Kafka (`grafana-stack.md` §12.3, §14.3).
- Grafana backend plugins are go-plugin subprocesses; signing levels private/community/commercial (`grafana-stack.md` §10.6, `plugin-architecture.md` §2.1).
- GoAlert embedded now, native later (`oncall-and-aiops-oss.md` §A.4); HolmesGPT wrapped as the RCA plugin (`oncall-and-aiops-oss.md` §B.1).

## 2. (a) Backing technology for `events`, `store`, `scheduler`

### 2.1 Candidates

| Candidate | Already present in which profile | License / version (checked 2026-10-27) | Go client | Fits which host service | Verdict |
|---|---|---|---|---|---|
| **PostgreSQL** | `lgtm-self-host`, `hosted`, `aiops-only` (kernel's own DB; Grafana also needs a DB) | PostgreSQL License; docs read at v18 | pgx v5 (River's primary driver) | `store.sql`, `store.kv`, `scheduler` (via River), `events` durable log (outbox) and wake-ups (`LISTEN/NOTIFY`) | **Adopt** as the one stateful dependency of the kernel |
| **SQLite** | `single-node` only | Public domain; River SQLite driver experimental since River 0.23.0, tested with `modernc.org/sqlite` (pure Go) | `database/sql` + `riversqlite` | Same services at single-writer scale; River reports roughly a quarter of Postgres speed (~9.6k jobs/s over 1M jobs on an M4 laptop) | **Adopt** for `single-node` (dev, demo, tiny estates) |
| **River** (job queue on Postgres/SQLite) | n/a (library) | MPL-2.0, v0.50.0 released 2026-10-27 ([releases](https://github.com/riverqueue/river/releases)) | native | `scheduler` (periodic/cron jobs, scheduled jobs, unique jobs, snooze, cancel, multiple queues, pause, retries, leader election, OpenTelemetry) | **Adopt**. OSS tier lacks per-queue concurrency limits, DLQ and workflows (River Pro); kernel adds a per-tenant token bucket itself |
| **Kafka (reuse Mimir/Tempo cluster)** | `hosted` only; absent from the three other profiles because Mimir `-target=all`, Loki, Tempo `-target=all` and Pyroscope run without it ([Tempo architecture](https://grafana.com/docs/tempo/latest/operations/architecture/): "No Kafka is required" in monolithic mode) | Apache-2.0; Go client `twmb/franz-go` BSD-3-Clause v1.22.2, pinned v1.21.6 in Mimir `go.mod` and v1.21.2 in Tempo `go.mod` (main, 2026-10-27) | franz-go | A partitioned WAL; Mimir uses only Produce (no transactions, no idempotent writes), Metadata, consumer groups and ListOffsets ([Mimir ingest storage](https://grafana.com/docs/mimir/latest/get-started/about-grafana-mimir-architecture/about-ingest-storage-architecture/)) | **Reject as kernel dependency**; keep as optional `sink.kafka` export plugin (§2.3) |
| **NATS JetStream** | none | Apache-2.0, nats-server v2.15.2 (2026-10-26); CNCF/Synadia dispute settled 2025-05-01 with trademarks assigned to the Linux Foundation and the project staying Apache-2.0 ([CNCF blog](https://www.cncf.io/blog/2025/05/01/protecting-nats-and-the-integrity-of-open-source-cncfs-commitment-to-the-community/), [Synadia press](https://www.synadia.com/press/cncf-and-synadia-align-on-securing-the-future-of-nats)) | nats.go Apache-2.0 | Streams, durable consumers, KV, object store ([JetStream concepts](https://docs.nats.io/nats-concepts/jetstream), 2.15 docs); can be embedded in a Go binary (official docs mention embedding; the in-process `DontListen` + `nats.InProcessServer` path is community-documented only, **UNVERIFIED** against an official page) | **Optional plugin** `events.nats` for hosted fan-out; not a default dependency |
| **Redis / Valkey** | none | Redis ≥ 8.0 is tri-licensed RSALv2 / SSPLv1 / AGPLv3; 7.4 is RSALv2 / SSPLv1 ([redis.io licenses](https://redis.io/legal/licenses/), page says current OSS release is 8.8). Valkey is BSD-3 (not re-verified this pass) | go-redis | Streams, pub/sub, locks | **Reject**: adds a third stateful system for nothing Postgres cannot do at our volume; AGPL option is acceptable but the SSPL/RSAL history is a self-hoster conversation we do not need |
| **Temporal / Kubernetes CronJob** | none / k8s only | - | - | Workflow engine / cron | **Reject for the kernel**; both remain possible `scheduler.*` rows for people who already run them (`plugin-architecture.md` §4.5) |

### 2.2 Per-profile mapping

| Host service | `single-node` | `lgtm-self-host` (compose) | `hosted-multi-tenant` (Kubernetes) | `aiops-only` |
|---|---|---|---|---|
| `store.sql` | SQLite file, WAL, `SetMaxOpenConns(1)` write pool, `busy_timeout=5000` (River's documented SQLite guidance) | Postgres 16+ container | Postgres HA (operator or managed); `tenant_id` column on every row plus row-level security as a second fence; each plugin owns its schema and migrations | Postgres |
| `store.kv` | table in the same DB | same | same | same |
| `store.blob` | local directory | MinIO (already in the compose for Loki/Mimir/Tempo) | the S3-compatible bucket the LGTM stack already uses, prefixed per tenant | S3 or local |
| `scheduler` | River `riversqlite`, poll-only (no `LISTEN`) | River `riverpgxv5`, leader elected | River `riverpgxv5`; N kernel replicas, one leader per database+schema with a 5-second TTL ([River leader election](https://riverqueue.com/docs/leader-election)) | River `riverpgxv5` |
| `events` durable | outbox table + River workers (same transaction as the state change) | outbox + River; `LISTEN/NOTIFY` wake-up | outbox + River; `LISTEN/NOTIFY` wake-up; optional `events.nats` for low-latency fan-out to many replicas, the Slack surface and Grafana Live | outbox + River |
| `events` ephemeral (UI live updates, chat typing, progress) | in-process bus | `LISTEN/NOTIFY` | `events.nats` (JetStream KV for presence, core NATS for progress) or `LISTEN/NOTIFY` until measured | in-process |
| Kafka | none | none | not used by the kernel; `sink.kafka` optional | none |

### 2.3 Why the kernel does not reuse Kafka

1. **It is not there in three of four profiles.** Mimir, Loki, Tempo and Pyroscope all run without Kafka in monolithic mode, and the self-hosted profile is explicitly monolithic (`grafana-stack.md` §14.3). A kernel that needs Kafka would force self-hosters to run it for a few hundred events per second.
2. **The data-plane Kafka is a write-ahead log, not a work queue.** Mimir shards series to partitions and maps each partition to exactly one ingester per zone by pod ordinal; the push succeeds "when Kafka brokers confirm that all records are replicated and persisted" and "the distributor requires the Kafka cluster to be available for push requests to succeed" ([Mimir ingest storage](https://grafana.com/docs/mimir/latest/get-started/about-grafana-mimir-architecture/about-ingest-storage-architecture/), stable since Mimir 3.0). Tempo 3 uses it the same way with RF1 and live-store/block-builder consumers ([Tempo architecture](https://grafana.com/docs/tempo/latest/operations/architecture/)). The kernel's workload is per-tenant jobs that wait (approvals open for hours, verification rechecks with exponential backoff, scheduled compliance scans, unique "one investigation per incident" jobs). Those are queue semantics (per-message ack, delay, uniqueness, cancellation), which Kafka consumer groups do not give and River does.
3. **Blast-radius coupling.** Sharing the ingest cluster means kernel topics compete with the write path of every tenant's metrics, and a Mimir-driven Kafka upgrade rolls the control plane. Running a second Kafka just for the kernel is the worst of both.
4. **Volume does not justify it.** The strictest commercial ingest limits are hundreds of alerts per five minutes per tenant (§2.5); River works ~46k jobs/s on a laptop ([River benchmarks](https://riverqueue.com/docs/benchmarks), 2,000 workers, M2 MacBook Air; the page warns against cross-system comparison). Postgres is three orders of magnitude above the need.
5. **Where Kafka is right**: customers who want the firehose of `incident/*`, `change/*` and `finding/*` events in their own lake. `sink.kafka` (franz-go, BSD-3) writes the outbox to a topic they own. If Mimir's cluster is reachable it can be the same cluster, but that is the customer's choice, not the kernel's dependency.

### 2.4 How the three services are built

- **Outbox, not pub/sub.** Every state change that must be observed (`signal.received`, `incident.opened`, `approval.requested`, …) is an insert into `events` in the same transaction as the state change, with `(tenant_id, seq)` ordering. River jobs are inserted in that same transaction (`InsertTx`), so "event committed" and "work scheduled" are one atomic fact; this is the River guarantee that a job "is visible only after commit" ([River docs](https://riverqueue.com/docs)). Consumers are at-least-once; handlers are idempotent on `event_id`.
- **`LISTEN/NOTIFY` is a doorbell only.** Payload must be shorter than 8000 bytes, delivery is at commit, identical payloads in one transaction are folded, the server-wide queue is 8 GB and "transactions calling NOTIFY will fail at commit" if it fills, and a listener stuck in a long transaction blocks cleanup ([PostgreSQL NOTIFY](https://www.postgresql.org/docs/current/sql-notify.html), v18). The kernel therefore notifies `(tenant_id, event_id)` only and reads the row; River itself uses the same mechanism and falls back to polling.
- **Scheduler = River periodic jobs on the leader.** Leadership is coordinated through the unlogged `river_leader` table with a five-second TTL; maintenance and periodic insertion run only on the leader; all replicas must register compatible periodic schedules ([River leader election](https://riverqueue.com/docs/leader-election)). The proposal represents tenant schedules as River periodic jobs keyed `(tenant, plugin, schedule_id)` with `UniqueOpts`.
- **Per-tenant fairness.** River OSS has multiple queues and pause but no per-queue concurrency limits (River Pro). The kernel keeps a per-tenant token bucket in Postgres (`store.kv`) checked at job start; jobs over budget snooze. `ponytail:` one table and a `SELECT ... FOR UPDATE SKIP LOCKED` is enough until a tenant can starve another in a measured test; the upgrade path is River Pro or a NATS work-queue stream.
- **SQLite caveats accepted for `single-node`.** River calls SQLite support experimental, one writer at a time, `SQLITE_BUSY` under contention, batch operations one row at a time ([River SQLite](https://riverqueue.com/docs/sqlite)). The conformance kit runs every `store`/`scheduler` test against both drivers, so plugins never see the difference.
- **Why not embed NATS in `single-node` instead of SQLite+River?** It would give streams and KV in one binary, but then the durable record would live in two systems (NATS file store plus SQLite) and the test matrix doubles. One database per profile is the lazy choice; `events.nats` stays a plugin.

### 2.5 Hot-path envelope

What the upstream systems actually do (primary sources):

| Fact | Value | Source |
|---|---|---|
| Alertmanager route defaults | `group_wait` 30s, `group_interval` 5m, `repeat_interval` 4h | [Alertmanager configuration](https://prometheus.io/docs/alerting/latest/configuration/) (latest, checked 2026-10-27) |
| Webhook batch size | `max_alerts` default 0 = all alerts in the group are sent; truncation reported in `truncatedAlerts`; payload `version: "4"` | same |
| Pipeline timeout | `group_interval` "also sets the context timeout for the notification pipeline for each send"; a send slower than `group_interval` is cancelled | same |
| Retry semantics | Retry stage uses `backoff/v5` exponential backoff and "we retry indefinitely until the context is canceled" ([notify/retry_stage.go](https://github.com/prometheus/alertmanager/blob/main/notify/retry_stage.go), main 2026-10-27). Grafana-managed alerting pulls `prometheus/alertmanager v0.28.2` replaced by the `grafana/prometheus-alertmanager` variant dated 2026-10-09 ([grafana/alerting go.mod](https://github.com/grafana/alerting/blob/main/go.mod)); identical retry behaviour in that variant **UNVERIFIED** | |
| Grafana webhook payload | `version: "1"`, `orgId`, `groupKey`, `truncatedAlerts`, per-alert `fingerprint`, `values`, `silenceURL`; Max Alerts 0 = no limit; optional HMAC-SHA256 `X-Grafana-Alerting-Signature` over `timestamp:body` with a timestamp header for replay protection; mTLS; basic or bearer (not both) | [Grafana webhook notifier](https://grafana.com/docs/grafana/latest/alerting/configure-notifications/manage-contact-points/integrations/webhook-notifier/) (latest) |
| Grafana IRM ingest limits | 300 alerts / 5 min per integration, 900 / 5 min per organization, both enforced together; excess is discarded with 429 and "IRM doesn't queue or replay"; API 300 req/min per key; incident creation 1 per 6 s per stack | [Grafana IRM rate limits](https://grafana.com/docs/grafana-cloud/observe-and-act/respond-to-incidents/reference/rate-limits/) (checked 2026-10-27) |
| PagerDuty Events API v2 | limits are dynamic; "up to 10,000 events/min per integration key, upon request" for AIOps accounts; 512 KB payload; on 429 "retry … preferably with a backoff of a few minutes" | [PagerDuty Events API v2 overview](https://docs.pagerduty.com/developer/events-api-v2-overview) |
| Opsgenie Alert API | token bucket per request domain, per-minute and per-second checks, 429 or delayed; exact figures are plan-dependent and not published; third-party figure ~500 RPM / 100 RPS on Standard is **UNVERIFIED** (the official page returned 429 to this research) | [Opsgenie rate limiting](https://docs.opsgenie.com/docs/api-rate-limiting) via search summary |
| Mimir Alertmanager per-tenant limits | `alertmanager_notification_rate_limit` default 0 (unlimited), per-integration map default `{}`, `alertmanager_max_alerts_count` default 0, `alertmanager_max_dispatcher_aggregation_groups` default 0; runtime overrides hot-reload | [Mimir configuration parameters](https://grafana.com/docs/mimir/latest/configure/configuration-parameters/) (`limits` block, main 2026-10-27) |

Design targets for the kernel (ours, derived from the above; these are requirements for the C3, not measurements):

| Metric | Target | Rationale |
|---|---|---|
| Webhook ack latency | p99 ≤ 250 ms for a group of ≤ 100 alerts, ≤ 1 s for the largest accepted body (1 MB) | Ack only after the raw group is durably written (one insert, no processing). Alertmanager cancels sends that exceed `group_interval` (5 m) and retries forever; a fast, dumb ack is what keeps upstream from piling retries during a storm |
| Dedupe + deterministic correlation | p99 ≤ 2 s from ack to incident open/join | Same-fingerprint refresh and same-`service` join run synchronously in the ingest job; topology-fold, change-window-fold and shared-alertname-fold run as follow-up River jobs. Upstream has already added ≥ 30 s (`group_wait`), so 2 s is invisible to users but keeps Slack threads coherent |
| First notification | p95 ≤ 5 s after incident open | Slack post or page via `notifier`/`on-call-provider`; digest window per target applies after the first message under the configured notification policy |
| Throughput per kernel node | 2,000 alerts/s sustained, 10,000/s for 60 s, with Postgres on 4 vCPU | River's 46k jobs/s on a laptop is the upper bound for the queue; our rows carry labels and annotations, so assume one order of magnitude less; 2,000/s is ~130× the Grafana IRM org quota |
| Per-tenant quota (default row, overridable) | 50 alerts/s sustained, 1,000 burst, 10,000 open alerts | A 5,000-pod cluster that fails entirely produces ~5,000 alerts in a minute; the bucket absorbs it. Grafana IRM's 3/s is the floor we must beat, not match |
| Backpressure | 429 + `Retry-After` when a tenant bucket is empty; raw group still written when the store is healthy; 503 only when the database is down | Never discard (contrast IRM). Alertmanager will retry; our idempotency key makes retries harmless |
| Idempotency key | `(tenant, source, fingerprint, startsAt)` for alerts; `(tenant, groupKey, receivedAt-bucket)` for raw groups | Alertmanager re-sends on every `group_interval` and on retry; at-least-once duplicates are the normal case |
| Storm mode | > 500 alerts/min for one tenant collapses new alerts into one `storm` incident and switches targets to digest only; LLM investigation is not started on storm incidents | Withholding an investigation must not withhold a page; current paging and storm-mode requirements are in the canonical record §2.1 row 5 |
| Investigation (LLM) | not on the hot path; p50 ≤ 3 min, cost-capped per run | HolmesGPT runs as an async job; its latency is minutes by nature |
| Inbound signature checks | Verify Grafana HMAC (timestamp window 5 min) and per-tenant webhook tokens before the insert | Trust boundary; cost is negligible |

## 3. (b) Grafana as the UI

### 3.1 The app plugin backend is a thin proxy, not the kernel

Evidence for making the backend a proxy:

- Grafana launches each backend plugin as a subprocess over hashicorp/go-plugin and manages its lifetime and instances; an "app instance is an app plugin of certain type that have been configured and enabled in a Grafana organization" (comment on `PluginContext.AppInstanceSettings`, [grafana-plugin-sdk-go backend/common.go](https://github.com/grafana/grafana-plugin-sdk-go/blob/main/backend/common.go), v0.298.0 released 2026-10-26). The process belongs to Grafana, is per Grafana instance, and dies with it. A kernel that holds River workers, outbox consumers, per-tenant sidecar supervisors and the HolmesGPT transcript store cannot live inside a process Grafana may restart on plugin update.
- The kernel must run with no Grafana at all (`aiops-only` profile; Slack-only operators; customers on Datadog who only want the AIOps layer, per the user brief).
- Precedent: Grafana OnCall's own Go plugin backend (`grafana-plugin/pkg/plugin/proxy.go` on the archived `grafana-cold-storage/oncall`, branch `dev`, archived 2026-03-24) does nothing but `ProxyRequestToOnCall`: it joins the path onto `OnCallAPIURL`, strips `Cookie`/`Set-Cookie`, sets `Authorization: <OnCallToken>`, `X-Instance-Context` (`stack_id`, `org_id`, `grafana_token`), `X-Grafana-Context` (`UserID`, `IsAnonymous`, `Name`, `Login`, `Email`, `Role`) and `X-OnCall-User-Context`, then copies the response back. User permissions were fetched from Grafana with the plugin's service-account token at `api/access-control/users/{id}/permissions` (`permissions.go`). Grafana Labs ran the largest Grafana-native on-call product this way; we copy the shape and fix the weak part (unsigned JSON identity headers, §3.2).

So the app plugin = Scenes frontend + a Go backend of three handlers: `CallResource` proxy to the kernel, `CheckHealth` (kernel reachable, tenant mapped), and a settings page that stores `{kernel_url, tenant_id}` in `jsonData` and the kernel token in `secureJsonData`. No business logic, no database, no goroutines beyond the HTTP client. Grafana Live streaming (`RunStream`) is optional later for incident timelines; polling the kernel's SSE through the proxy is enough for MVP.

### 3.2 Identity propagation: Grafana org/user/role → kernel tenant/user/role

What Grafana hands the plugin backend (SDK v0.298.0, [backend/common.go](https://github.com/grafana/grafana-plugin-sdk-go/blob/main/backend/common.go), [http_headers constants](https://pkg.go.dev/github.com/grafana/grafana-plugin-sdk-go/backend)):

| Input | Field / header | Trust level | Notes |
|---|---|---|---|
| Org | `PluginContext.OrgID` (marked "Deprecated: Use Namespace instead") | trusted (gRPC from Grafana) | Namespace is the multi-tenant replacement; both are set by Grafana, not the browser |
| App config | `PluginContext.AppInstanceSettings.JSONData` / `DecryptedSecureJSONData` | trusted | per org; this is where `tenant_id` and the kernel token live |
| User | `PluginContext.User{Login, Name, Email, Role}`; "Will not be provided if Grafana backend initiated the request, for example when request is coming from Grafana Alerting" | trusted but unsigned; nil for backend-initiated requests | Role is the basic org role (`Admin`/`Editor`/`Viewer`) |
| Grafana-signed ID token | header `X-Grafana-Id` (`GrafanaUserSignInTokenHeaderName`) on `CallResource`, `QueryData`, `CheckHealth`, streams | verifiable JWT, signed by Grafana, JWKS at `/api/signing-keys/keys` | The `idForwarding` toggle existed 2023-09-25 → 2024-08-21 and was then removed from the registry ([toggles-gitlog.csv](https://github.com/grafana/grafana/blob/main/pkg/services/featuremgmt/toggles-gitlog.csv)); the current `ForwardIDMiddleware` has no feature check and sets the header on every plugin request, deriving a token from the OBO access token in multi-tenant Grafana and failing open ([forward_id_middleware.go](https://github.com/grafana/grafana/blob/main/pkg/services/pluginsintegration/clientmiddleware/forward_id_middleware.go), [PR #133071](https://github.com/grafana/grafana/pull/133071)). The SDK constant's doc string still says it requires the toggle; treat that as stale. Minimum Grafana version for unconditional forwarding **UNVERIFIED** (the toggle was GA-by-default before removal per the gitlog; exact release not confirmed) |
| External IdP tokens | `Authorization` (OAuth access token), `X-Id-Token` (IdP ID token) | verifiable against the IdP | Only when the user logged into Grafana through OAuth and the plugin route forwards them |
| Fine-grained permissions | `authlib/authz` `EnforcementClient.HasAccess(ctx, idToken, action)` with `JWKsURL = app_url + /api/signing-keys/keys`; plugin declares `roles` + `grants` and page `includes.action` in `plugin.json` (Grafana ≥ 11.6.0) | verifiable | Assigning custom roles to specific users "requires either Grafana Cloud or a Grafana Enterprise license"; in OSS only grants to basic roles apply ([implement RBAC in app plugins](https://grafana.com/developers/plugin-tools/how-to-guides/app-plugins/implement-rbac-in-app-plugins)) |
| Managed service account for the plugin | `externalServiceAccounts` toggle + `iam.permissions` in `plugin.json` | - | Public preview, created 2023-09-28, "only supports single organization setups" (same guide; [feature toggles](https://grafana.com/docs/grafana/latest/setup-grafana/configure-grafana/feature-toggles/)). Not usable for org-per-tenant |

Mapping rules adopted:

1. **Tenant** = `AppInstanceSettings.JSONData.tenant_id`, bound at install time by an org admin and verified by the kernel against the token in `secureJsonData` (token is minted per `(grafana_url, org_id, tenant_id)` and is the only thing the proxy adds, like OnCall's `OnCallToken`). A Grafana org can map to exactly one tenant; a tenant can be reached from several Grafana orgs/instances (hosted + customer's own Grafana).
2. **User** = the `sub` of the verified `X-Grafana-Id` JWT, with `login`/`email` as display fields. The kernel verifies signature, issuer (`root_url`) and expiry against the cached JWKS; the proxy forwards the header untouched. When the header is missing (older Grafana, or `User == nil` because Alerting made the call), the proxy marks the request `actor=plugin` and the kernel allows read and ingest paths only.
3. **Role** = Grafana basic role is a floor: `Admin` → `tenant-admin`, `Editor` → `operator`, `Viewer` → `viewer`. Everything above the floor (`approvals:decide`, `remediation:apply`, `secrets:write`) is a kernel-side binding `(tenant, subject, role)` managed in the kernel admin UI/API, because OSS Grafana cannot bind custom roles to individual users. Approval quorum (L3 = two distinct deciders under the canonical record §2.2) is evaluated in the kernel against those bindings, keyed by the stable `sub`, so two Grafana sessions of one person cannot satisfy it.
4. **Writes require a verified subject.** `approve`, `deny`, `apply`, `rollback` and secret writes are refused for `actor=plugin`; the error tells the operator to upgrade Grafana or use the Slack surface (where the subject is the verified Slack user id) or an OIDC token from the shared IdP against the kernel API directly.
5. **Kernel → Grafana** (annotations, dashboards, silences, org provisioning) uses a per-org Grafana service account token stored in `host.Secrets` under `(tenant, grafana, sa_token)`; service accounts are isolated per org ([organization management](https://grafana.com/docs/grafana/latest/administration/organization-management/)). Not `externalServiceAccounts`.
6. **Org membership sync** for self-hosters is Grafana's job: Generic OAuth `org_mapping = <ExternalOrgName>:<OrgIdOrName>:<Role>` with `org_attribute_path` (array) and `*` wildcards is OSS-only (explicitly "not supported on Grafana Cloud") ([generic OAuth](https://grafana.com/docs/grafana/latest/setup-grafana/configure-security/configure-authentication/generic-oauth/)); hosted tenants are provisioned by the kernel's `grafana` plugin through the org API instead.

Sequence for an approval from Grafana: browser → `/api/plugins/<id>/resources/approvals/{id}:decide` → Grafana attaches `X-Grafana-Id` → plugin backend adds `Authorization: Bearer <tenant token>` and forwards → kernel gateway verifies both, resolves `(tenant, sub)`, checks the binding, evaluates quorum, writes the decision and the `approval.decided` event in one transaction → River job resumes the HolmesGPT turn with `tool_decisions` (§4.1).

### 3.3 Signing for self-hosters

| Mode | What it needs | Who uses it | Source |
|---|---|---|---|
| Development | `app_mode = development` (the create-plugin Docker env runs this way and loads unsigned plugins) | contributors | [sign a plugin](https://grafana.com/developers/plugin-tools/publish-a-plugin/sign-a-plugin) |
| Unsigned allow-list | `[plugins] allow_loading_unsigned_plugins = <id>` or `GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=<id>`; the grafana.ini comment reads "Plugins with modified signatures are never loaded", so the artifact must ship **without** a `MANIFEST.txt`, not with a stale one | self-hosters; our Helm/compose profiles set it by default | grafana.ini wording confirmed via community quotes ([forum](https://community.grafana.com/t/manually-enable-plugin-disabled/55204), [grafana-zabbix issue](https://github.com/grafana/grafana-zabbix/issues/1767)); the official docs page was not fetched (404 at the guessed URL), exact doc page **UNVERIFIED** |
| Private signature | `npm run sign -- --rootUrls https://a,https://b`; `rootUrls` must match each instance's `root_url`; token is a Grafana Cloud Access Policy with scope `plugins:write`, realm = org; a Grafana Cloud account is required and its name must match the first segment of the plugin id | our hosted Grafana pools (we know the URLs); large self-hosters who sign their own build with their own Cloud account | [sign a plugin](https://grafana.com/developers/plugin-tools/publish-a-plugin/sign-a-plugin); `@grafana/sign-plugin` v4 needs Node ≥ 24.15.0 |
| Community signature | review by Grafana Labs; catalog distribution; then no config at all for self-hosters | once the kernel API is v1 | same page |

Decision: the release pipeline produces two artifacts from one build: `aiops-app-unsigned.zip` (no manifest) and `aiops-app-private.zip` signed for our hosted `rootUrls`. `aiopsctl grafana install` picks the unsigned one and writes the env var; self-hosters who already have a Cloud account can run `aiopsctl grafana sign --root-urls ...` which wraps `@grafana/sign-plugin`. Private signing cannot be done on the user's behalf because the token must belong to an account whose name equals the plugin id prefix. Submitting for a community signature is a milestone after the kernel API freezes, since re-signing is needed on every release anyway.

### 3.4 Org per tenant vs instance per tenant

| Concern | Org per tenant (shared instance) | Instance per tenant | Source |
|---|---|---|---|
| Cost / ops | "Multiple organizations are easier and cheaper to manage than multiple instances of Grafana" | one Deployment, DB and plugin install per tenant | [organization management](https://grafana.com/docs/grafana/latest/administration/organization-management/) |
| Isolated per org | folders, dashboards, data sources, alerts, notification channels, annotations, reports, service accounts, teams | everything | same |
| Shared across orgs | users, configuration settings, authentication providers, Enterprise licenses; alert state history backend (`[unified_alerting.state_history]`) is instance-level config (`grafana-stack.md` §10.5) | nothing | same |
| Plugin install vs enable | install is instance-wide (Server Admin); enable/config is per org (Org Admin), app instance settings are per org | per tenant | [plugin management](https://grafana.com/docs/grafana/latest/administration/plugin-management/), SDK comment |
| Plugin-managed service accounts | `externalServiceAccounts` "only supports single organization setups" | works (preview) | [RBAC guide](https://grafana.com/developers/plugin-tools/how-to-guides/app-plugins/implement-rbac-in-app-plugins) |
| Grafana Cloud model | - | Grafana Cloud is one stack per customer (observed model; not re-verified this pass, **UNVERIFIED**) | |
| Grafana Assistant (on-prem) | per-org plugin but needs a Cloud stack per instance (`grafana-stack.md` §10.7) | per tenant | |

Decision D11: org-per-tenant is the hosted default because the kernel never relies on anything that is instance-scoped (identity comes from `X-Grafana-Id` + app-instance settings, not from `externalServiceAccounts`; the kernel's own SA token per org handles write-back; alert state history goes to the tenant's Loki through the kernel gateway, not Grafana's instance-level config). Instance-per-tenant is an opt-in tier for customers who need separate auth providers, separate plugin sets, or who bring their own Grafana; the `grafana` datasource row already carries `{url, org_id, service_account_token}` so the kernel does not care which it is (`plugin-architecture.md` §2.5). Self-hosters run one org and tenant `default`, which is also what every `lgtm-self-host` compose profile does.

## 4. (c) Resolving the four cross-report conflicts

### 4.1 Investigation runtime and platform-owned remediation

Historical option: **HolmesGPT behind an investigation interface.** This is not the current default-harness decision; see the canonical record §2.1 row 2. The architectural boundary remains useful: investigation does not own approval, credential custody or execution.

- HolmesGPT 0.45.0 (2026-10-15), Apache-2.0, CNCF Sandbox since 2025-10-08 (`oncall-and-aiops-oss.md` §B.1), exposes a stateless `POST /api/chat`: the client resends `conversation_history`; `stream: true` yields SSE events `start_tool_calling`, `tool_calling_result` (status `success | error | approval_required`), `ai_message`, `approval_required` (with `pending_approvals[]` of `tool_call_id, tool_name, description, params`), `ai_answer_end`, `token_count`, compaction events and `error` (code 5204 = rate limit). `enable_tool_approval` defaults to `false`, in which case tools needing approval are "automatically converted to errors" fed back to the model; with `true` the stream pauses and the client resumes with `conversation_history` plus `tool_decisions` ([HolmesGPT HTTP API](https://holmesgpt.dev/latest/reference/http-api/); page banner says "previous version", so exact 0.45.0 parity is **UNVERIFIED**). Auth is an optional `HOLMES_API_KEY`; "Admin endpoints are currently unauthenticated"; no multi-tenant guidance on the page.
- An adapter can persist the upstream conversation and translate its pending approvals into platform requests. That is an integration option, not authorization to execute a write through a harness. The current harness is read-only, and the remediation dispatcher owns execution under the canonical record §2.1 row 2 and §2.2.
- Platform requirements are stated in the canonical record §2.2: approval before writes, action-scoped credential leases, a change record before execution, L0–L3 quorum, typed plans, audit, PII scrub, verification and cancellation. The upstream API does not establish or satisfy these requirements by itself. Their plugin boundaries and transaction rules are in §4.3 of that record.
- If HolmesGPT is enabled in a multi-tenant deployment, its process-level toolset/model configuration and unauthenticated admin endpoints require a per-tenant instance and network isolation. A contract suite would need to pin the HTTP/SSE behaviour and demonstrate that the adapter cannot bypass platform requirements. No such test result is claimed here.
- Compatibility is judged at the read-only harness and tool boundary. A model must not be allowed to supply a free-form shell or a remediation plan; the canonical record §2.2 defines the typed action catalogue.

### 4.2 GoAlert embedded vs native on-call module

Adopted: **GoAlert embedded behind `on-call-provider` for MVP** (`oncall-and-aiops-oss.md` §A.4), v0.35.0 (2026-09-21) confirmed via GitHub API 2026-10-27. The `plugin-architecture.md` §4.5 row list is amended: `on-call-provider.builtin` is phase 2; MVP rows are `goalert`, `pagerduty`, `opsgenie`, `grafana-irm`. Consequences for the kernel: the hosted profile runs one GoAlert per tenant (single-tenant product) as a sidecar container with its own Postgres database; the kernel's `events` outbox feeds GoAlert's generic/Alertmanager integration key; `WhoIsOnCall` is cached in `store.kv` with a 60 s TTL so Slack `/oncall who` never waits on GoAlert. Native module triggers remain: hosted tenant count beyond what one-GoAlert-per-tenant can operate, Slack-native schedule editing demand, or the need for one data model across schedules and incident timeline.

### 4.3 Plain gRPC/UDS vs hashicorp/go-plugin

Adopted: **plain gRPC over UDS (local) or mTLS TCP (Kubernetes), Dapr-style registration** for kernel plugins (`plugin-architecture.md` §4.1). hashicorp/go-plugin v1.8.0 (2026-04-29) is confirmed as the latest tag ([tags](https://github.com/hashicorp/go-plugin/tags), checked 2026-10-27), which closes the UNVERIFIED note in that report; it remains unsuitable because its handshake assumes the host launches the plugin binary, while our Python pillars and sidecars are started by the orchestrator. go-plugin is nevertheless used in one place: the Grafana app backend (§3.1), where Grafana is the host and the SDK hides go-plugin entirely. That backend is not a kernel plugin and does not implement the kernel SDK; it is a client of the kernel API. This keeps "one process boundary mechanism per host": go-plugin inside Grafana, gRPC/UDS inside the kernel.

### 4.4 Grafana org-per-tenant vs instance-per-tenant

See §3.4, D11.

## 5. Kernel C3 inputs that follow from these decisions

| Component (inside the `aiops` binary) | Responsibility | Backing |
|---|---|---|
| `gateway` | HTTP/gRPC API; authn: OIDC (shared IdP), Grafana `X-Grafana-Id` JWKS verify, kernel tenant tokens, per-tenant webhook tokens, Grafana HMAC; injects `tenant` into context; PII scrub at egress | stateless |
| `signal-ingest.*` plugins | parse Alertmanager v4 / Grafana v1 / vendor payloads; write raw group + normalized alerts; idempotency; per-tenant bucket; 429 | Postgres/SQLite |
| `correlator` | sync rules in the ingest transaction; fold rules as River jobs; storm mode | Postgres + River |
| `jobs` | River client; queues `ingest`, `correlate`, `notify`, `investigate`, `remediate`, `report`, `compliance`; per-tenant token bucket | River |
| `scheduler` | River periodic jobs; tenant cron rows; leader-only | River |
| `events` | outbox writer + `LISTEN/NOTIFY` doorbell + subscriber API; `events.nats` and `sink.kafka` as rows | Postgres; NATS/Kafka optional |
| `store` | `sql` (per-plugin schemas + migrations), `kv`, `blob` | Postgres/SQLite, S3/local |
| `secrets` | tenant+plugin scoped; Grafana SA tokens, Slack tokens, cloud creds | Postgres with envelope encryption (KMS plugin later) |
| `llm` | gateway with per-tenant budgets (LiteLLM embedded or in-kernel; unchanged open question) | Postgres |
| `plugin-host` | in-process registry; `remote.<point>` gRPC adapters; sidecar supervision (HolmesGPT, GoAlert, Python pillars) | - |
| `admin` | tenants, bindings `(tenant, subject, role)`, profiles, plugin releases | Postgres |

External containers per profile: Postgres (or SQLite file), S3/MinIO, LGTM stack, Grafana + app plugin, HolmesGPT, GoAlert, optional NATS, optional Kafka sink target.

## 6. Open questions

1. Grafana version floor: the first release in which `X-Grafana-Id` is forwarded with no toggle (the gitlog shows removal on 2024-08-21; the matching Grafana release and whether Grafana 11.x LTS lines still gate it are **UNVERIFIED**). Until pinned, the proxy must tolerate a missing header (§3.2 rule 2).
2. Does a Grafana service-account token calling plugin resources populate `PluginContext.User` and `X-Grafana-Id` with the SA identity? **UNVERIFIED**; affects automation users approving via the Grafana API rather than the kernel API.
3. `LISTEN/NOTIFY` ceiling with several kernel replicas and thousands of events/s: the docs describe one server-wide queue and commit-time failure when full; the point at which `events.nats` becomes mandatory needs a load test, not a guess.
4. River Pro (concurrency limits, DLQ, workflows) vs kernel-side equivalents: revisit once a tenant can be shown to starve another.
5. HolmesGPT typed execution plans: whether the planner/executor split in §4.1 is needed in MVP or can wait for the first remediation pillar.
6. Grafana IRM as an `on-call-provider` row needs the IRM API's own 300 req/min per key budget modelled in the notifier's digest logic.
7. Whether the hosted profile's optional instance-per-tenant tier is Grafana operator `Grafana` CRs or a plain Helm release per tenant (operator CRD coverage was already an open item in `grafana-stack.md` §15).

## 7. UNVERIFIED items

- Grafana release that made `X-Grafana-Id` forwarding unconditional (gitlog date only).
- SA-token behaviour for `PluginContext.User` / `X-Grafana-Id`.
- Exact wording and URL of the official Grafana docs page for `allow_loading_unsigned_plugins` (grafana.ini comment confirmed through community quotes only).
- Grafana Cloud "one stack per customer" model (not re-fetched this pass).
- HolmesGPT HTTP API page parity with 0.45.0 (page banner says "previous version").
- Opsgenie Alert API numeric limits (official page returned 429; third-party figures only).
- Whether `grafana/prometheus-alertmanager` variant preserves the upstream retry-stage semantics.
- Official NATS documentation of `DontListen` + `nats.InProcessServer` (community sources only).
- Valkey license and suitability (not re-verified; rejected anyway).

## 8. Sources

Primary sources fetched 2026-10-27 unless noted.

- Grafana Mimir ingest storage: https://grafana.com/docs/mimir/latest/get-started/about-grafana-mimir-architecture/about-ingest-storage-architecture/ ; Kafka backend configuration: https://grafana.com/docs/mimir/latest/configure/configure-kafka-backend/ ; configuration parameters (`limits` block): https://grafana.com/docs/mimir/latest/configure/configuration-parameters/ ; Mimir `go.mod` (main): https://github.com/grafana/mimir/blob/main/go.mod
- Grafana Tempo architecture: https://grafana.com/docs/tempo/latest/operations/architecture/ ; Tempo `go.mod` (main): https://github.com/grafana/tempo/blob/main/go.mod
- franz-go: https://github.com/twmb/franz-go (BSD-3-Clause, tag v1.22.2)
- River: https://github.com/riverqueue/river (MPL-2.0, v0.50.0 2026-10-27) ; docs: https://riverqueue.com/docs ; drivers: https://riverqueue.com/docs/database-drivers ; SQLite: https://riverqueue.com/docs/sqlite ; benchmarks: https://riverqueue.com/docs/benchmarks ; leader election: https://riverqueue.com/docs/leader-election
- PostgreSQL NOTIFY (v18 docs): https://www.postgresql.org/docs/current/sql-notify.html
- NATS JetStream concepts (2.15 docs): https://docs.nats.io/nats-concepts/jetstream ; nats-server v2.15.2 (Apache-2.0): https://github.com/nats-io/nats-server/releases ; CNCF statement 2025-05-01: https://www.cncf.io/blog/2025/05/01/protecting-nats-and-the-integrity-of-open-source-cncfs-commitment-to-the-community/ ; Synadia press: https://www.synadia.com/press/cncf-and-synadia-align-on-securing-the-future-of-nats ; embedding (community): https://kaustavdm.in/nats-upspeak-app-framework/ , https://gosuda.org/blog/posts/how-embedded-nats-communicate-with-go-application-z36089af0
- Redis licensing: https://redis.io/legal/licenses/
- Prometheus Alertmanager configuration: https://prometheus.io/docs/alerting/latest/configuration/ ; retry stage (main): https://github.com/prometheus/alertmanager/blob/main/notify/retry_stage.go ; grafana/alerting `go.mod`: https://github.com/grafana/alerting/blob/main/go.mod
- Grafana webhook contact point: https://grafana.com/docs/grafana/latest/alerting/configure-notifications/manage-contact-points/integrations/webhook-notifier/
- Grafana IRM rate limits: https://grafana.com/docs/grafana-cloud/observe-and-act/respond-to-incidents/reference/rate-limits/ ; archived OnCall limits: https://grafana.com/docs/oncall/latest/oncall-api-reference/
- PagerDuty Events API v2: https://docs.pagerduty.com/developer/events-api-v2-overview
- Opsgenie rate limiting (via search summary; page rate-limited this research): https://docs.opsgenie.com/docs/api-rate-limiting
- grafana-plugin-sdk-go v0.298.0 (2026-10-26): https://github.com/grafana/grafana-plugin-sdk-go/blob/main/backend/common.go ; https://pkg.go.dev/github.com/grafana/grafana-plugin-sdk-go/backend
- Grafana forward-ID middleware (main): https://github.com/grafana/grafana/blob/main/pkg/services/pluginsintegration/clientmiddleware/forward_id_middleware.go ; PR #133071: https://github.com/grafana/grafana/pull/133071 ; feature toggle gitlog: https://github.com/grafana/grafana/blob/main/pkg/services/featuremgmt/toggles-gitlog.csv ; feature toggles doc: https://grafana.com/docs/grafana/latest/setup-grafana/configure-grafana/feature-toggles/ ; community thread on using the token from an app plugin: https://community.grafana.com/t/usage-example-of-idforwarding-feature/128489
- Grafana RBAC in app plugins (Grafana ≥ 11.6): https://grafana.com/developers/plugin-tools/how-to-guides/app-plugins/implement-rbac-in-app-plugins
- Grafana plugin signing: https://grafana.com/developers/plugin-tools/publish-a-plugin/sign-a-plugin ; plugin management: https://grafana.com/docs/grafana/latest/administration/plugin-management/ ; unsigned allow-list wording (community quotes of grafana.ini): https://community.grafana.com/t/manually-enable-plugin-disabled/55204 , https://github.com/grafana/grafana-zabbix/issues/1767
- Grafana organizations: https://grafana.com/docs/grafana/latest/administration/organization-management/ ; Generic OAuth `org_mapping`: https://grafana.com/docs/grafana/latest/setup-grafana/configure-security/configure-authentication/generic-oauth/
- Grafana OnCall plugin backend (archived 2026-03-24, branch `dev`): https://github.com/grafana-cold-storage/oncall/blob/dev/grafana-plugin/pkg/plugin/proxy.go , https://github.com/grafana-cold-storage/oncall/blob/dev/grafana-plugin/pkg/plugin/permissions.go
- HolmesGPT HTTP API: https://holmesgpt.dev/latest/reference/http-api/ ; release 0.45.0 (2026-10-15): https://github.com/HolmesGPT/holmesgpt/releases
- GoAlert v0.35.0 (2026-09-21): https://github.com/target/goalert/releases
- hashicorp/go-plugin tags (v1.8.0 latest): https://github.com/hashicorp/go-plugin/tags
- Related research: `plugin-architecture.md`, `grafana-stack.md`, `oncall-and-aiops-oss.md`, `dsh-architecture.md` (2026-10-27). Current product requirements: [canonical decisions](../architecture/00-decisions.md) §2.1, §2.2 and §4.3.

---

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
