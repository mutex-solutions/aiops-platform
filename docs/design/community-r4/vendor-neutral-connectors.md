# Vendor-neutral connectors: how a contributor adds another observability stack

Status: design proposal, 2026-11-11, community round 4. Not canonical: `docs/architecture/00-decisions.md` (cited `00 §n`) wins until an ADR applies section 9 of this file. The plugin record is `docs/design/plugin-system-2026-11.md` (cited `record §n`); `04`, `05`, `07`, `09` are files of `docs/architecture/`.

Owner input, 2026-11-11: this edition ships first-party connectors and plugins for the Grafana ecosystem, which stays first and is the default. A contributor must be able to write plugins for another observability vendor or stack and contribute them back. Nothing may hard-wire the default stack where a definition should be.

Short answer to the owner's question: yes, with five small changes to definitions that are cheap now and a migration later (section 6). Today the registries a contributor needs mostly exist (`signals`, `tools`, `estateSources`, `investigators`), but four things in the plan assume the default stack outside the stack bundle, and one deferral says other vendors wait for a paying customer. HolmesGPT 0.46.0 already ships read toolsets for Datadog, Elasticsearch and OpenSearch, VictoriaLogs, VictoriaMetrics, Coralogix and New Relic (section 2.4), so for the investigator a second vendor is a binding, not new code.

Outside facts were read on 2026-11-11 and are listed in section 10 with URL and version. Anything not read on that date is marked UNVERIFIED.

## 1. Inventory: every place the platform touches an observability backend

"Neutral" means the contract names no backend, query language or backend API. "Home" is the bundle that holds the first-party implementation. Phase tags are those of 00.

| # | Capability | Owner today (definition or registry, first-party row, home) | Contract today | What a contributor for vendor X implements |
|---|---|---|---|---|
| 1 | Alert intake | `signals` registry, dispatcher `signal-core`; `signal-ingest-alert-webhook` (Alertmanager and dashboard-alerting payloads), `signal-ingest-generic-json`; `aiops-base` | Neutral in shape (`Parsed`, `Signal{Kind, Labels, Targets}`). Three assumptions are implicit: the sender retries a 429, the alert state and the sender's alert id are "payload family fields", and the truncated count that `storm-mode` reads is an Alertmanager field | A `signals` contribution (parse half, remote from P1), a default label map, a statement of what the sender retries. Nothing if X sends through Alertmanager |
| 2 | Metric read by the investigator | `toolsetBindings`, one provider `toolset-bindings`; `aiops-datasources` | Not neutral: the config has fixed slots `metrics`, `logs`, `traces` with `{url, auth}`, and the map from slot to a HolmesGPT toolset lives in `investigator-holmes` | A binding that names a HolmesGPT toolset and its config; or `tools` contributions when HolmesGPT has no toolset for X |
| 3 | Metric read by the kernel | `metricsQuery`, provider `metrics-query-http`; `aiops-datasources`. Consumers: `fleet-sweeper` (P1), `estate-source-series` (P2) | Assumes the Prometheus HTTP query API and PromQL: the consumers hold query strings. 07 gives no signature | Nothing if X speaks that API (endpoint config). Otherwise X does not provide this key; X ships typed readers (rows 12, 8) |
| 4 | Log read by the investigator | `toolsetBindings` slot `logs` | As row 2; the P1 host runbook also carries the query text in LogQL | A binding, and the pack's own host runbook |
| 5 | Log read by the kernel | `logsQuery`, provider `logs-query-http` (P2); consumer `change-hub` | Assumes the Loki HTTP query API and LogQL, and the label contract of the `host-logs` template | See row 8 |
| 6 | Trace read | Investigator only, `toolsetBindings` slot `traces`. The kernel reads no traces: the `trace-graph` estate reader reads service-graph series through `metricsQuery` | As row 2 | A binding. No kernel contract exists, and none is needed |
| 7 | Profile read | None in MVP. `tool-profiles-query` waits for its trigger (00 §7.3). HolmesGPT 0.46.0 ships no profiles toolset | None | Nothing now; later a `tools` contribution |
| 8 | Change evidence read | P1: the investigator, with the logs toolset and the host runbook. P2: `change-hub` through `logsQuery` (ADR-028, open) | P1 neutral once the runbook moves. P2 not neutral: a base plugin would build LogQL | P1: the runbook text. P2: a contribution to a typed registry (section 2.2) |
| 9 | Annotation write | `annotations`, provider `annotator` (P1), `dashboard-sync` (P2); `aiops-stack`. Absent in `aiops-only` | Singular key; 07 gives no signature. Written from outbox events. In-process only | An in-process provider of `annotations`. Optional: the RCA already reaches chat and the API |
| 10 | Rule and alert content | Static Prometheus rule files in `aiops-stack`, served by `content`; label contract `rule_id`, `rule_pack`, `signal_type`, identity labels of exactly one entity (00 §2.1 row 17) | The label contract is neutral. The files are PromQL | A rule pack in X's own format that meets the label contract, proven on the alert it emits (section 3.3) |
| 11 | Dashboards content | Provisioning files for the dashboard server in `aiops-stack`; panels read `kernel-db` read-only views and the backends | Not neutral, by design | Optional dashboards for X's own UI as pack content. No contract |
| 12 | Estate source | `estateSources` registry, dispatcher `estate`; `estate-source-series` over `metricsQuery`; `aiops-base` (P2) | The registry is neutral (a reader returns a run and writes nothing). The first reader is PromQL tables over exporter series, and it sits in `aiops-base` | Nothing if X speaks the Prometheus API and holds the same exporter series. Otherwise an `estateSources` contribution with its own client |
| 13 | Verification condition read | `verifiers` under `change-hub`: `verify-alert-cleared` reads resolved signals from `signals`, with no backend query; `verify-readback` goes through the executor | Neutral, provided the signal source delivers resolves | A resolve fixture in the `signals` suite. Nothing else |
| 14 | Agent and collector config | `fleetTemplates`, dispatcher `fleet-composer`, template rows; `agent-base`; the `fleet` collector API; the `aiops-agent` chart | Not neutral, by design: the output is configuration for one agent and its remote-config API. The published label contract (04) is the neutral part | In this edition nothing: X's users run `aiops-only` with their own collectors and meet the label contract (section 5) |
| 15 | Tenant provisioning, gateway and limits | `stack-provisioning`, `gateway` (`gateway-renderer`), `limitsRenderers` (four renderer rows, P2), `auth`; `aiops-stack` | `limitsRenderers` is neutral in shape (`Backend()`, `Render`, `Drift`). The gateway route table, the tenant header and provisioning are the four default backends | In this edition nothing. A full second stack is a new stack bundle (section 5) |
| 16 | Platform dead-man | The Alertmanager `Watchdog` route to the on-call provider's `Heartbeat` (00 §2.1 row 5) | The provider side (`Heartbeater`) is neutral; the sender side is an Alertmanager route | A recipe in the pack's README, or a statement that X has none |
| 17 | Storm input | `storm-mode` adds the truncated count a payload reports | An Alertmanager payload field | A neutral `Truncated` field set by the contribution (section 6) |
| 18 | Tenant identity at the backend | "The tenant id is `X-Scope-OrgID`" (00 §2.1 row 7) | Stack-specific, correctly placed in `aiops-stack` | A connector maps the platform tenant to X's tenant in its own row config. Single tenant in the community edition: one constant |
| 19 | Runners that read history | `rightsizing-runner` (krr, P2) on the gateway query routes | Prometheus API | Out of scope for a connector |

**Where the default stack is hard-wired outside a stack-specific row.** Five places, all fixed by section 6:

1. `toolset-bindings` has fixed slots and `investigator-holmes` holds the slot-to-toolset map (rows 2, 4, 6).
2. `change-hub`, a base plugin, would build LogQL at P2 (rows 5, 8).
3. `estate-source-series`, a PromQL reader, is listed in `aiops-base` (row 12).
4. The host runbook with its LogQL text is content of `aiops-pillars-mvp` (row 8).
5. 00 §7.3 defers "toolset bindings or tools for Datadog, Elasticsearch and CloudWatch" to the first paying `aiops-only` customer, which contradicts the owner's wish.

Everything else that names the default stack is inside `aiops-stack`, `agent-base` or `aiops-datasources`, which is where it belongs.

## 2. The contract for reads

### 2.1 Decision

**Both, split by layer, with the dialect declared by the key and not negotiated at run time.**

- **Layer A, typed read intents, is the contract for every first-party consumer outside the connector bundles.** A plugin of `aiops-base` or of a pillar bundle never holds a query string in any dialect. It calls a registry whose request is typed: `estateSources` ("run this reader for this tenant scope"), a new `evidenceSources` ("change evidence for these entities in this window"), `verifiers`, `signals`. A vendor implements a contribution with its own client and its own query text.
- **Layer B, raw query strings, exists only below Layer A and only inside dialect-specific rows.** `metricsQuery` v1 is declared as the Prometheus HTTP query API with PromQL, and `logsQuery` v1 (P2) as the Loki HTTP query API with LogQL, the same way `sql` is declared a PostgreSQL dialect contract (00 §2.4 W2). Only first-party rows that are themselves dialect-specific inject them: `fleet-sweeper`, `estate-source-series`, and the first-party evidence contribution. A backend that speaks the dialect is reached by endpoint configuration of the existing provider row. A backend that does not speak it provides nothing under that key; the rows that need the key stay PENDING and say so, which is the existing behaviour of a missing provider.
- **The investigator keeps its native toolsets.** Which toolset serves which signal is a contribution to `toolsetBindings`, now a real registry. The model writes the vendor's dialect; the kernel never parses it.

### 2.2 Why not the other two

| Option | Rejected because |
|---|---|
| Raw strings only, with a dialect parameter on one key | Every consumer would branch per dialect, so adding vendor X would edit `estate`, `change-hub` and every later reader. The fake could not be the second implementation of N dialects (principle 3). A capability check at call time is the check-then-use pattern the record forbids |
| Typed intents only, including a typed query language for metrics and logs | That is a query language of our own. "Series for this entity and metric family" does not remove the dependency on which exporter wrote the series, so the estate reader would still be vendor-specific. Out of scope beside "no rule evaluator of our own" (00 §1) |
| A registry of query backends per dialect behind `metricsQuery` | No first-party or shared consumer will hold query text in a second dialect: a vendor reader calls its own client. The remote mark `contrib P2` on the two query keys (00 §4.1) has no use under this decision and is removed |

A second raw dialect becomes a new key only when a shared consumer needs raw strings in it. It is never a mode of an existing key.

### 2.3 Consequences for first-party consumers

| Consumer | Today | After |
|---|---|---|
| Estate readers (P2) | `estate-source-series` in `aiops-base`, PromQL tables over `metricsQuery` | The `estate` plugin and the `estateSources` registry stay in `aiops-base` and are vendor-neutral. `estate-source-series` moves to `aiops-datasources`. With no reader ACTIVE, `estate` answers "not known" with a gap, as designed. A vendor ships `estate-source-<system>`. The reader rules (liveness witness, writes nothing) already bind every reader |
| Change evidence, P1 | The investigator reads with the logs toolset and the host runbook | Unchanged, but the runbook is content of the connector bundle, so each pack ships the query text for its own dialect. A profile with no log binding prints "no log source bound" in the first-message footer |
| Change evidence, P2 (ADR-028) | `change-hub` calls `logsQuery` and maps lines | `change-hub` dispatches a new registry `evidenceSources`: `Read(ctx, Scope{Entities, From, To}) -> Run{Lines{At, Entity, Class, Actor, Ref}, Unseen}`. The first contribution, `evidence-source-logs` in `aiops-datasources`, holds the LogQL and injects `logsQuery`. `change-hub` keeps the mapping to `external_change` rows, the coverage report and the rule that it stores references, never lines. This shapes ADR-028 and costs nothing before P2 |
| Verification | `verify-alert-cleared` reads `signals`; no backend query | Unchanged. The `signals` suite gains an obligation: a contribution proves a firing and resolved fixture pair, or declares that its sender sends no resolve, in which case the check reports that it could not see the alert state |
| Context pack v1 | Reads no backend | Unchanged, plus one line built by code from the `toolsetBindings` inventory: which signals are bound and in which dialect, so the model writes the right language |
| `fleet-sweeper` | PromQL over self-monitoring series | Unchanged. It is a row of `aiops-stack` and reads series our own agent writes |

### 2.4 The investigator: what HolmesGPT ships for other backends

Verified against the HolmesGPT repository at tag 0.46.0 (released 2026-11-04, Apache-2.0), the version 00 §3.2 pins, by listing `holmes/plugins/toolsets/` and `docs/data-sources/builtin-toolsets/` at that tag.

| Backend | Toolset ids at 0.46.0 | Notes |
|---|---|---|
| Prometheus-compatible stores | `prometheus/metrics`, `prometheus/multi-tenant-metrics` | A `subtype` field selects `prometheus`, `victoriametrics`, `coralogix`, `grafana-cloud`, `aws-managed-prometheus`, `azure-managed-prometheus`, `google-managed-prometheus` |
| VictoriaMetrics | `prometheus/metrics` with `subtype: victoriametrics` | The upstream page states that no separate toolset is required |
| VictoriaLogs | `victorialogs` | Tools: query, streams, field names, field values, hits |
| Elasticsearch and OpenSearch | `elasticsearch/data`, `elasticsearch/cluster`, `opensearch/query_assist` | The upstream page states that the first two work with both, through `api_url` |
| Datadog | `datadog/metrics`, `datadog/logs`, `datadog/traces`, `datadog/general` | Each takes `api_key` and `app_key` in its config. `datadog/general` exposes a generic API GET and POST search |
| Coralogix, New Relic | `coralogix`, `newrelic` | DataPrime and NRQL |
| The default stack | `prometheus/*`, a logs toolset and `grafana/tempo`, `grafana/dashboards` under `grafana/` | As planned. These are upstream ids: they appear only as config values of a binding row, never in a name of ours (08 §5.2 T1) |
| ClickHouse | A database toolset page (`database-clickhouse`) | A SQL connector, not an observability toolset |
| SigNoz | None found at 0.46.0 | A SigNoz pack ships `tools` contributions, or binds the ClickHouse database toolset. UNVERIFIED whether that is usable for SigNoz schemas |
| Profiles of any vendor | None found | Matches 05 §5.5 |

Three consequences:

1. **A binding is data.** For every backend in the table the "connector" on the investigator side is a toolset id plus a config map. No Go or Python is written.
2. **The allow-list belongs to `investigator-holmes`.** 00 §2.1 row 2 says toolsets are read-only by construction. That was checked for the default toolsets only. `investigator-holmes` keeps a table of the toolset ids it will render at the pinned HolmesGPT version, each admitted after its tool list is read and a cassette recorded. `bash` is never in it. `datadog/general` is not admitted by default because it is a generic API client. UNVERIFIED: the read-only property of each non-default toolset; the table admits one only after that review.
3. **The read credential sits inside the investigator.** That is already true in `aiops-only` (00 §6.3). For a SaaS vendor it is an API credential, so a pack must document the narrowest read scope the vendor offers. In hosted, the investigator's egress is closed to everything but the kernel and the tenant gateway (00 §2.1 row 2), so SaaS bindings in hosted are an enterprise question and are not designed here.

Where HolmesGPT has no toolset, the existing path applies: a `tools` contribution served through the MCP facade, read-only by the dispatcher's effects class (00 §4.1). The deferred `tool-*-query` rows keep their trigger.

## 3. The connector pack

### 3.1 Definition

A connector pack makes one external observability system usable for one or more capabilities of section 1. It is a bundle published as one signed artifact: rows, content and, when code is needed, one plugin. It is not a new kernel concept, a new manifest kind or a new runtime. A pack covers capabilities, not a whole stack, because mixed estates are normal (metrics in one system, logs in another), and several packs compose in one profile.

Three sizes, by what the contributor has to write:

| Size | When | Contains | Runtime |
|---|---|---|---|
| Data pack | The system speaks an API the platform already has a row for: alerts through Alertmanager, the Prometheus query API, a HolmesGPT toolset | Rows of first-party generic plugins inserted under the pack's own row ids (`aiops/toolset-binding`, `aiops/metrics-query-http`, `aiops/signal-ingest-generic-json`), a label map, a rule pack or a rule check, a host runbook | None. No code |
| Read connector | The system has its own alert payload or its own query API | The above plus one plugin that registers with `signals`, and from P2 with `evidenceSources` and `estateSources`, and with `tools` where the investigator has no toolset | Remote (any language) or in-process Go. Every registry it uses is `contrib` |
| Stack bundle | The contributor wants agents, a tenant gateway, limits and provisioning for a second self-hosted stack | Section 5 | In-process Go, through a distribution or `aiops-contrib` |

### 3.2 What a pack ships

```
connector-<system>/
  plugin.yaml          id, version, title, license, publisher, engines.aiops, runtimes, capabilities, integrates
  bundle.patch.yml     the rows, disabled by default
  content/             label-map.yml, rules/<pack>/, runbooks/host.md, webhook-template.json, dashboards/
  fixtures/<system>-<version>/   recorded alert payloads (firing, resolved), query responses, toolset cassettes
  testenv/             a compose file for an open-source backend: how the fixtures were recorded
  <code>               only for a read connector
```

| Row or file | Registry or key | Required | Notes |
|---|---|---|---|
| Signal source | `signals` | Unless alerts arrive through an existing row | Parse half only. The registration carries the default label map and the busy reply (section 6) |
| Toolset bindings, one row per signal | `toolsetBindings` | For each signal the investigator should read | Rows of the first-party plugin `aiops/toolset-binding` with `{signal, investigator, toolset, config}`. Credentials are `${secret:name}` references |
| Kernel-data tools | `tools` | Only where the investigator has no toolset | Read-only by the dispatcher |
| Query provider | `metricsQuery` | Only if the system speaks the Prometheus query API | A row of `aiops/metrics-query-http` with the endpoint. Never a translation layer |
| Evidence source (P2) | `evidenceSources` | Optional | Without it the evidence read reports "not seen" |
| Estate source (P2) | `estateSources` | Optional | Without it estate answers are "not known" |
| Annotation writer | `annotations` | Optional | In-process Go only: it provides a key |
| Label map | content, given to `signal-core` at registration | Yes when the system's labels differ from the canonical ones | Data. Maps the system's labels or tags to canonical labels and entity kinds, under the rules of 05 §5.1 |
| Rule pack | content | Optional | In the system's own format, with a `format` field. Authored by the contributor: 00 §2.1 row 17 forbids taking an expression from another platform |
| Host runbook | content | Yes if a logs binding is shipped | Holds the change-evidence query text in the system's dialect |
| Webhook or payload template | content | When the system needs one configured by hand | Printed by `aiopsctl plugin inspect` |

**The manifest that groups them** is the existing `plugin.yaml` plus the bundle patch. One optional hand-written field is added, `integrates: [{system, versions, tested}]`, which names the external system and the versions the fixtures were recorded against. It feeds the index search, the attestation and the naming rules of section 7. A data pack has `runtimes: []`: `aiopsctl plugin pack` must accept an artifact with no image and no module, and the artifact gains an optional `content/` layer that install unpacks under `content/<plugin id>/` for the `content` row to serve. Install still runs no third-party code.

**No coverage descriptor.** Which capability is covered by which row is derived from the composition: contributions in `signals`, entries in `toolsetBindings` by signal, the provider of `metricsQuery`, contributions in `evidenceSources` and `estateSources`, the provider of `annotations`. `aiopsctl profile render --dump` prints that table per profile, with "none" where nothing is composed. Skipped: a `connector.yaml`. Add it when the index needs something `plugin.yaml` cannot say.

### 3.3 Conformance and the solo test

| Tier | What a pack must pass | Mechanism |
|---|---|---|
| T0 | Manifest parity, name lint (section 7), content lint: every rule in a rule pack names `rule_id`, `rule_pack`, `signal_type` and one entity | `aiopsctl plugin check` |
| T1 | The plugin alone on the real kernel with fakes of `signals`, `evidenceSources`, `estateSources`, `tools`, `secrets`, `dialer`; the generated cycle and two-tenant tests | Existing |
| T2 | The obligations half of each registry suite it contributes to. `signals`: parse fixtures, the firing and resolved pair, the busy reply. `toolsetBindings`: every toolset id is in the investigator's table, the config validates, no literal credential. `evidenceSources` and `estateSources`: returns a run, writes nothing, liveness witness, honours the window and the deadline | Existing, plus the two new suites |
| T4, new: the connector scenario | One fixed script, vendor-neutral, replayed from the pack's fixtures on `aiops-only` plus the pack, in the no-page form of 00 §7.1: a recorded firing alert becomes one incident keyed on the expected entity; the first message lists the bound signals; the investigator cassette makes at least one successful call on each bound toolset; the recorded resolve yields `cleared`. A rule pack is proven here, on the alert it emits, not by parsing the rule language | `aiopsctl plugin smoke ./ --against <release>` with `AIOPS_SNAPSHOT=replay` enforced. Fixtures are recorded with the testkit recorder against a backend the contributor runs: the pack's compose file for open-source systems, a trial organisation for SaaS, with credentials scrubbed by the recorder |
| Attestation | The conformance attestation (gate check 10) names the suites, the scenario version and the `integrates.tested` versions | Existing, one more field |

A contributor therefore runs three commands before a pull request: `aiopsctl plugin new connector --lang go|python`, `aiopsctl plugin check ./`, `aiopsctl plugin smoke ./ --against <release>`. The connector template is the template plugin the owner asked for (decision 6). No kernel checkout, no live backend in CI.

Our CI does not hold credentials for anyone's SaaS. A pack that is contributed back is tested by replay only; its fixtures state the date and version they were recorded at, and a pack whose fixtures are older than two releases of the external system is flagged in the index, not removed.

### 3.4 How a profile selects a pack

| Case | Profile | What changes |
|---|---|---|
| Another system for reads, the customer runs its own collectors | `aiops-only` plus the pack: `aiopsctl plugin install <spec> --profile aiops-only`, then `plugin enable` per row | The pack's bundle is appended; its binding rows are enabled and the default binding rows for the same signals are disabled in the profile layer. `metrics-query-http` is repointed or disabled |
| A mixed estate | Any server profile plus one or more packs | Bindings are per signal, so the default pack can keep traces while another pack takes logs |
| A full second stack | A new profile that lists a contributed stack bundle in place of `aiops-stack` and `agent-base` | Section 5. Not planned for this edition |

`aiops-datasources` is the first-party connector pack for the default stack and keeps its name. Under this design it holds the three default binding rows, `metrics-query-http`, the host runbook, and from P2 `logs-query-http`, `estate-source-series` and `evidence-source-logs`. The dispatcher row `toolset-bindings` moves to `aiops-base`, because a profile may compose another pack without the default one.

## 4. Two worked examples

### 4.1 Open source: VictoriaMetrics with VictoriaLogs

| Capability | Mapping | Clean | Evidence |
|---|---|---|---|
| Alert intake | `vmalert` evaluates rules and sends to Alertmanager (`-notifier.url`), so the existing `signal-ingest-alert-webhook` row receives them unchanged. Log alerts are `vmalert` groups with `type: vlogs` | Yes, no code | vmalert docs; VictoriaLogs vmalert docs |
| Metric read, investigator | A binding to `prometheus/metrics` with `subtype: victoriametrics` | Yes, data | HolmesGPT 0.46.0 docs |
| Metric read, kernel | `metrics-query-http` pointed at `/prometheus/api/v1/*` (single node) or `/select/<tenant>/prometheus/api/v1/*` (cluster) | Yes, config. The pack runs the `metricsQuery` suite against the backend, because MetricsQL differs from PromQL in documented places. UNVERIFIED per query | VictoriaMetrics URL examples |
| Log read, investigator | A binding to `victorialogs` | Yes, data | HolmesGPT 0.46.0 source |
| Log read and change evidence, kernel (P2) | An `evidenceSources` contribution that calls `/select/logsql/query` with LogsQL; the response is JSON lines; the tenant is the `AccountID` and `ProjectID` headers | Yes, with code. There is no Loki-compatible read API, so `logs-query-http` cannot be reused | VictoriaLogs querying docs |
| Rule content | `vmalert` loads the Prometheus rule format and has `-dryRun`; the shipped rule files are the starting point and the pack's scenario proves the alert | Mostly. Stated differences: no duplicate rules in a group, no reliance on chained recording rules, limited label templating | vmalert docs |
| Estate source (P2) | `estate-source-series` unchanged, if the same exporter series are stored | Yes: the dependency is on the exporters, not on the store | 09 §2 |
| Collectors | The customer's own: the store accepts Prometheus remote write at `/api/v1/write` and OTLP metrics; the logs store accepts the Loki JSON push at `/insert/loki/api/v1/push` and OTLP logs. Our agent could write there, but its served config and the gateway routes are stack-specific (section 5) | Read side yes; our agent no | VictoriaMetrics and VictoriaLogs ingestion docs |
| Tenant identity | Numeric account and project ids in the URL or headers; the platform tenant is a string | No: a per-tenant mapping in the pack's row config. A constant in the single-tenant community edition | Same |
| Log patterns | The demo step "matching log pattern" (00 §7.1 step 3) uses a pattern endpoint of the default logs store. The nearest tools here are hits and field values | No. The demo step is stack-specific and the scenario of section 3.3 does not assert it | HolmesGPT 0.46.0 source |
| Traces, profiles | No toolset at HolmesGPT 0.46.0 for a trace or profile store of this vendor | Not covered. UNVERIFIED whether the vendor's trace store can be bound through another toolset | Directory listing |
| Annotations, limits, provisioning | None | Not covered; not needed for reads | |

Result: a data pack at the first community release (two bindings, a runbook, a label map, a rule check) and one small plugin at P2. This is the cheapest proof that the path works.

### 4.2 SaaS: Datadog

| Capability | Mapping | Clean | Evidence |
|---|---|---|---|
| Alert intake | The Webhooks integration: a monitor message mentions `@webhook-<name>`; the payload is a JSON template the user defines from variables such as `$ALERT_ID`, `$ALERT_CYCLE_KEY`, `$AGGREG_KEY`, `$ALERT_TRANSITION`, `$ALERT_SCOPE`, `$TAGS`, `$HOSTNAME`, `$LINK`; custom headers carry the bearer token. The pack ships the template as content and a `signals` contribution that parses exactly that template; tags `key:value` become labels | Yes, with code. The values of `$ALERT_TRANSITION` that mean resolved are UNVERIFIED here and are fixed by the firing and resolved fixture pair | Webhooks integration docs |
| Admission | The sender retries only on an internal error or a 5xx response, with a 15 s timeout and five retries of a missed connection. A 429 is therefore not retried, and the admission bucket of 00 §2.1 row 5 would drop the delivery | No, as designed today. Fixed by the busy reply in the registration (section 6): this family is answered 503 with `Retry-After` | Same |
| Metric read, investigator | A binding to `datadog/metrics` | Yes, data. The toolset holds an API key and an application key; the pack documents the narrowest read scope. UNVERIFIED: scoped application keys | HolmesGPT 0.46.0 docs |
| Metric read, kernel | None: there is no PromQL. The API is `GET /api/v1/query`, `POST /api/v2/query/timeseries` and `POST /api/v2/query/scalar` | Not through `metricsQuery`, by decision. `fleet-sweeper` is not composed in `aiops-only`; an estate reader is its own contribution | Metrics API docs |
| Log read, investigator | A binding to `datadog/logs` | Yes, data | HolmesGPT 0.46.0 docs |
| Change evidence, kernel (P2) | Two candidates: log search (`POST /api/v2/logs/events/search`, `filter.query`, `from`, `to`, cursor paging, permission `logs_read_data`) or the events API (`GET /api/v2/events`, `POST /api/v2/events/search`), where a v2 event has `category` `change` or `alert` | Partly. Log search only helps if the customer tags host logs to the label contract, which our `host-logs` template does and their agent does not. Change events fit `evidenceSources` better, but `changed_resource.type` allows only `feature_flag` and `configuration` | Logs and Events API docs |
| Trace read | A binding to `datadog/traces` | Yes, data | HolmesGPT 0.46.0 source |
| Annotation write | The v2 event intake accepts only the categories `change` and `alert`, on a separate intake host. The flat v1 `POST /api/v1/events` would fit an RCA marker, but it is deprecated and open only to organisations that already used it | No | Events API docs |
| Rule content | Monitors are not rule files. A rule pack is monitor definitions the customer applies, each carrying the tags `rule_id`, `rule_pack`, `signal_type` and grouped by one entity. Every expression is rewritten by the contributor | Label contract yes; content no reuse | 00 §2.1 row 17 |
| Estate source (P2) | A contribution with its own client and tag mapping. Query endpoints are rate limited per endpoint, reported in `X-RateLimit-*` headers, 429 on excess; the reader must budget its periodic runs. The actual limits are not published on the page read | Possible; cost unknown. UNVERIFIED: tag names for cluster, namespace and workload | Rate limits docs |
| Agent, fleet, gateway, limits, provisioning | None. The customer's own agent ships to the vendor. Hosts are keyed `host:ext/<name>` through the label map | Not applicable | 00 §6.3 |
| Dashboards | Kernel views live in PostgreSQL. UNVERIFIED whether the vendor's dashboards can read them | No | |
| Egress | A SaaS endpoint per site (several regional hosts). The community edition is on-prem first, so this pack is opt-in and its outbound hosts are declared in `capabilities.network` | Yes, declared | Metrics API docs |

Result: a read connector. The investigator side is three bindings. The code is one `signals` contribution and a payload template, later one evidence source. Annotations and rules do not map and the pack says so.

### 4.3 What the two examples prove about the contracts

| Finding | Contract change |
|---|---|
| Both vendors are covered for the investigator by toolsets that already exist | `toolsetBindings` as a registry is the main seam, and it is data |
| One vendor reuses the alert row, the other needs its own and does not retry a 429 | `signals` registration carries the busy reply and the default label map |
| Neither can use `logsQuery`; one can use `metricsQuery` | Typed `evidenceSources`; the query keys stay dialect contracts |
| Annotations do not map for the SaaS vendor | `annotations` stays optional and in no required set |
| Tenant ids differ in both | The mapping is row config of the pack, never a platform rule |

## 5. What stays stack-specific by design in this edition

These parts are built for the default stack and are not portable. That is a decision, not a gap: each renders configuration for a named upstream component (principle 7), and making them generic now would be an abstraction with one implementation.

| Part | Why it is not portable | What a contributor would add for a full second stack |
|---|---|---|
| Agent and fleet (`agent-base`, `fleet`, `fleet-agentapi`, the installer, the `aiops-agent` chart) | The templates emit configuration for one agent, served over its remote-config API; the Kubernetes layout is an upstream chart pinned as a subchart; the eBPF sensors are that agent's components | A stack bundle. The agent can stay: it can write Prometheus remote write and OTLP to other stores. What must change is the writer templates and the endpoints, so the writers would first be split from the collectors as their own `fleetTemplates` contributions. Not before a second stack is proposed |
| Tenant gateway (`gateway`, `gateway-renderer`) | The route table is the write, query, rule and admin paths of four named backends, and the tenant is injected as one header | A provider of `gateway` that renders the routes and the tenant scheme of the other stores. Providing a key is in-process code, so it enters through a distribution or `aiops-contrib` |
| Limits (`limits`, four renderer rows) | Each renderer emits one backend's overrides block and reads its merged view back | Renderer contributions to `limitsRenderers` for stores that have per-tenant limits. The registry is already neutral |
| Backend provisioning (`stack-provisioning`) | Tenant create, purge and rule load on four stores | The contributor's own provisioning plugin with its own `external` durable namespaces. No definition exists, because nobody calls it |
| Provisioned dashboards and `annotator`, later `dashboard-sync` and `aiops-app` | Provisioning files and the org API of one dashboard server | Dashboards as pack content; an `annotations` provider |
| Detection rule files as shipped | PromQL | A rule pack per system |
| The change-evidence label contract | Produced by our `host-logs` template | The contract is published (04), so another collector can meet it; we do not render other collectors |
| The lab (`lab/`) and the §7.1 demo | They deploy `self-host-full` unchanged | A pack proves itself with the connector scenario instead |

The honest statement for the community release notes: this edition reads from other systems through connector packs in `aiops-only`; it collects, stores, isolates and limits only on the default stack.

## 6. Changes to definitions, and when

"Before" means before the first community release, because the definition leaves `v0` at its first release and a later change is a new major with an adapter (00 §4.3). "With P2" means decided now and built with its first consumer.

| # | Change | Why it is a migration later | When |
|---|---|---|---|
| D1 | `toolsetBindings` becomes a registry. A contribution is `Binding{Signal, Investigator, Toolset, Config}` with a `Resolve(ctx)` that returns the endpoint and read credential for the tenant of the handle. Dispatcher `toolset-bindings` in `aiops-base`. A first-party generic plugin `aiops/toolset-binding` registers one binding from its row config. `investigator-holmes` renders what the registry returns, holds the allow-list of toolset ids for its pinned version, and names no backend. Remote: `contrib` | The key is already plural and already injected by `investigator-holmes`. Changing a singular provider into a registry after release changes every profile layer that sets its config | Before |
| D2 | `signals`: `Signal` gains the named fields `State` (`firing`, `resolved`) and `SourceID` (the sender's id of the alert instance); `Parsed` gains `Truncated`; `Register` takes a `Spec{Family, DefaultLabelMap, Busy}` where `Busy` is the status the sender retries (429 by default, 503 for senders that retry only 5xx) | These are fields `verify-alert-cleared`, the digest, `storm-mode` and admission already read, today as payload-family extras. A registration signature cannot change inside a major | Before |
| D3 | `metricsQuery` v1 is stated as a dialect contract: the Prometheus HTTP query API and PromQL, with a conformance corpus limited to what first-party readers use. Its remote mark becomes `no`. The same statement is written for `logsQuery` when it ships | It stops a contributor from providing the key with a translator, and it removes a remote adapter nobody needs | Before for `metricsQuery`; with P2 for `logsQuery` |
| D4 | Dialect-specific first-party rows and content live in `aiops-datasources`: the host runbook now; `estate-source-series` and `evidence-source-logs` at P2 | A bundle move after release changes customers' profile layers | Before for the runbook; with P2 for the rows |
| D5 | The artifact format accepts `runtimes: []` and an optional `content/` layer, served by `content` under the plugin id; `plugin.yaml` gains the optional `integrates` field | Additive, so not a migration. It is here because data packs are the easiest first contribution and need it | Before |
| D6 | The connector scenario (T4), the two new suites' obligations, and `aiopsctl plugin new connector` | Not a migration. It is the owner's priority 6 | Before: the scenario and the template. With P2: the P2 suites |
| D7 | `evidenceSources` replaces the direct `change-hub` to `logsQuery` call in ADR-028 | Decided now so that ADR-028 is written once | With P2 |
| D8 | The context pack footer lists bound signals and dialects from the `toolsetBindings` inventory | Not a migration | Before |
| D9 | 00 §7.3: the row "Toolset bindings or tools for Datadog, Elasticsearch and CloudWatch, first paying `aiops-only` customer" is replaced (section 9) | It is the stated policy against contributions | Before |
| D10 | The index format gains `renamedTo`, so an id that a mark owner objects to can be renamed without breaking `plugins.lock` (section 7) | An index format change after third parties publish is a migration | Before, with the index |

Not changed: `annotations` stays a singular, optional key; `limitsRenderers`, `estateSources`, `tools` and `verifiers` are already neutral; `fleetTemplates` and `gateway` stay as they are (section 5); the kernel, `boot` and `loader` are untouched.

Row effect at P1 scope: `aiops-base` gains the dispatcher `toolset-bindings`; `aiops-datasources` holds three binding rows and `metrics-query-http`. The counts of 00 §4.4 are restated by the ADR.

One standing test is added to keep the claim true: CI boots `aiops-only` with one non-default data pack and replays its connector scenario. The proposal is the VictoriaLogs logs binding, because the store is one Apache-2.0 binary and its dialect is not the default one. This is the read-side counterpart of the toy registries of S2. It is one maintained fixture set, and it is listed as an owner question in section 8.

## 7. Naming and trademark rules for vendor plugins from third parties

What the mark owners publish, as read on 2026-11-11:

| Owner | Published rule relevant to naming | Status |
|---|---|---|
| Grafana Labs | Marks may not be used in product or service names, domains or handles; breach is treated as a breach of the open-source licences (already 00 §8.1 and 08 §5.2 T1 to T5) | Verified earlier, in the repository's research |
| Elastic | The marks "may not be part of any product name"; referring uses such as "for Elasticsearch" or "compatible with Elasticsearch" are allowed, less prominent than the product name; not in a domain or social media name; a stated attribution sentence | Verified |
| OpenSearch (LF Projects) | A trademark policy exists; accurate referential use is allowed; the mark may not be in a domain name; one's own identifier must be at least as prominent | Partly verified from a search summary. The exact product-name clause is UNVERIFIED |
| Datadog | No published guideline for third-party naming was found. The website terms require written permission for use of the marks | UNVERIFIED |
| VictoriaMetrics | Only logo guidelines were found; no word-mark policy | UNVERIFIED |

Rules. R1 to R6 are checked by the name lint in `aiopsctl plugin check` and by the index CI; R7 and R8 are process.

| # | Rule |
|---|---|
| R1 | A plugin id is `<publisher>/<name>`. The publisher namespace is the publisher's own. `aiops/` is first-party only (the builder already refuses it elsewhere). A namespace equal to a vendor's name is reserved for that vendor; the index ties a namespace to its signer |
| R2 | The name is `<contribution noun>-<system>` or `connector-<system>`, where `<system>` is the lowercase technical name of the external system, used once, after the noun, to say what the plugin talks to. Never the system name alone, never with "official", "certified", "enterprise" or the vendor's product tiers. This is the form first-party rows already use (`on-call-provider-pagerduty`, `chat-surface-slack`, `investigator-holmes`) |
| R3 | The `title` is the name a person reads, so it is treated as the product name: "`<own name>` connector for `<System>`". The mark comes after "for" and is never the first word |
| R4 | No vendor logo, icon or colour scheme in an artifact, an index entry or the plugin manager |
| R5 | The pack's README states that the publisher is not affiliated with or endorsed by the mark owner, and carries the owner's attribution sentence where its policy gives one |
| R6 | The deny-list of 00 §8.1 (the default stack's marks and derived marks) applies to every id, title, bundle name and content path in the project's index and in `aiops-contrib`, whoever the publisher is, because the project distributes those. Packs for those systems use generic nouns, as `aiops-datasources` does |
| R7 | The project's index removes or renames an entry on a credible objection from a mark owner. `renamedTo` in the index keeps installed sites working (D10) |
| R8 | A third-party index or marketplace sets its own rules. The lint then warns and does not fail. Enterprise marketplace review (publisher verification, listing rules) is out of scope here |
| R9 | Contributed packs that live in this repository use the namespace `community/` and are built into `aiops-contrib`, not `aiops`. They are maintained by their contributors, tested by replay, and named in the release notes with their fixture date |

R2 rests on reading a plugin id as a technical identifier and not a product name. That reading is ours and has not been reviewed by counsel. It is an open item, and D10 exists so that a different answer is a rename and not a break.

## 8. Open items

1. **Owner.** Whether the project maintains one non-default reference pack in CI from the first community release (section 6), and which one.
2. **Owner.** Whether contributed packs live in this repository under `community/` and ship in `aiops-contrib` (R9), or only in the index as separate repositories.
3. **Owner, enterprise boundary.** A connector pack for self-host is community under the proposed principle (what one organisation needs to self-host). SaaS bindings in hosted, where the investigator's egress is closed, and paid packs are enterprise. To be recorded with decision 8, not decided here.
4. **Counsel.** R2 and R3 for marks whose owners publish no naming rule or a strict one (Datadog, Elastic).
5. **ADR-003 spike.** Extend the cassettes to one non-default toolset, so the allow-list of section 2.4 has a tested procedure, and record whether each admitted toolset is read-only.
6. **ADR-028.** Rewritten around `evidenceSources` (D7).
7. **UNVERIFIED facts** to settle when the first pack of each kind is written: MetricsQL differences per query of the `metricsQuery` corpus; the `$ALERT_TRANSITION` values; scoped application keys and actual query rate limits at Datadog; Datadog tag names for Kubernetes entities; whether a SigNoz or a ClickHouse-based stack can be bound through the database toolset; the OpenSearch product-name clause.
8. **Not designed.** A full second stack bundle (section 5). It waits for a contributor who proposes one.

## 9. Amendments this design needs

In `00-decisions.md`:

| Section | Change |
|---|---|
| §1 Product | After "on the Grafana observability stack": the default stack is first-party and the default; other systems are reached through connector packs; no base or pillar row names a backend, a query language or a backend API |
| §2.1 row 2 | Toolsets are selected by `toolsetBindings` contributions; `investigator-holmes` holds the allow-list of toolset ids for its pinned version |
| §2.1 row 5 | The busy reply is per sender family, from the `signals` registration; 429 is the default |
| §2.1 row 15, row 19 | `estate-source-series` is a row of `aiops-datasources`; the P2 evidence read goes through `evidenceSources`, with `evidence-source-logs` as the first contribution |
| §3.3 `ext-customer-observability` | "The customer's own observability systems that a composed connector pack covers; by default Prometheus-compatible and Loki-compatible stores, Alertmanager and the dashboard server" |
| §4.1 | `toolsetBindings` becomes a registry row of its own (`contrib` P1); `signals` gains the registration spec and the named fields; `metricsQuery` and `logsQuery` are stated as dialect contracts with remote `no`; `evidenceSources` is added (P2, `contrib`); `content` serves pack content under the plugin id |
| §4.4 | `toolset-bindings` moves to `aiops-base`; `aiops-datasources` is described as the default connector pack with its rows; the host runbook moves there from `aiops-pillars-mvp`; counts restated |
| §4.5 | The connector scenario is added to T4 |
| §6.3 `aiops-only` | "Customers with their own observability systems, through the default pack or another connector pack" |
| §7.3 | Replace the row "Toolset bindings or tools for Datadog, Elasticsearch and CloudWatch" with: "First-party maintenance of a non-default connector pack: a maintainer and a recorded fixture set exist. Community packs need no trigger" |
| §8.1 | Add the rules R1 to R9 for third-party names, the `community/` namespace and the bundle name form `<publisher>-connector-<system>` |
| §9 | A new ADR (next free number, ADR-047 or later) "Vendor-neutral connectors" that applies this table; ADR-028 re-scoped |

In the plugin record:

| Section | Change |
|---|---|
| §5 catalogue | The same four definition changes as 00 §4.1; the fake of `toolsetBindings` is a recording registry; the fakes of the query keys stay fixture responses |
| §6 row inventory | `toolset-bindings` in base; three binding rows and the moved rows in datasources |
| §8 hooks | No change. `signal/normalize` applies the registration's default label map before the tenant map |
| §11 | `plugin pack` accepts `runtimes: []` and a `content/` layer; `plugin.yaml` gains `integrates`; the index format gains `renamedTo` |
| §12 | The name lint joins gate check 5's neighbours as a check of `plugin check` and of the index CI, not of the runtime gate |
| §14 | The connector scenario; `aiopsctl plugin new connector` |

Also touched: 05 §5.5 (toolset wiring table reads from the registry), 07 §4.3 and §4.6 (the `signals` types, the new registry signatures), 09 §2 and §8.1 (reader home, evidence read).

## 10. Sources

All read on 2026-11-11.

| Fact | Source |
|---|---|
| HolmesGPT 0.46.0, released 2026-11-04, Apache-2.0; toolset directories and ids | `https://api.github.com/repos/HolmesGPT/holmesgpt/releases`; `https://github.com/HolmesGPT/holmesgpt/tree/0.46.0/holmes/plugins/toolsets`; `https://github.com/HolmesGPT/holmesgpt/tree/0.46.0/docs/data-sources/builtin-toolsets` (pages `prometheus.md`, `victoriametrics.md`, `victorialogs.md`, `elasticsearch.md`, `datadog.md`) |
| VictoriaMetrics query and write paths, tenant in the URL | `https://docs.victoriametrics.com/victoriametrics/url-examples/` |
| vmalert: Prometheus rule format, Alertmanager notifier, `-dryRun` | `https://docs.victoriametrics.com/victoriametrics/vmalert/` (first 100,000 characters read) |
| VictoriaLogs query API, LogsQL, tenant headers, response format | `https://docs.victoriametrics.com/victorialogs/querying/` |
| vmalert with VictoriaLogs, `type: vlogs`, limitations | `https://docs.victoriametrics.com/victorialogs/vmalert/` |
| VictoriaLogs ingestion paths | `https://docs.victoriametrics.com/victorialogs/data-ingestion/` |
| Datadog webhooks: mention, payload variables, headers, retry rule | `https://docs.datadoghq.com/integrations/webhooks/` |
| Datadog metrics query endpoints and sites | `https://docs.datadoghq.com/api/latest/metrics/` |
| Datadog log search body, paging, permission | `https://docs.datadoghq.com/api/latest/logs/`; `https://docs.datadoghq.com/api/latest/logs/search-logs-post.md` |
| Datadog events: v2 intake, categories, change event fields, v1 deprecation | `https://docs.datadoghq.com/api/latest/events/`; `https://docs.datadoghq.com/api/latest/events/post-an-event.md` |
| Datadog rate limit headers and status | `https://docs.datadoghq.com/api/latest/rate-limits/` |
| Elastic trademark rules | `https://www.elastic.co/legal/trademarks` |
| OpenSearch trademark policy (summary only) | `https://opensearch.org/trademark-brand-policy/` |

Not reachable on that date: `https://holmesgpt.dev/data-sources/builtin-toolsets/` returned 404 to the fetcher, so the HolmesGPT facts come from the repository at the tag, which is the stronger source.

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
