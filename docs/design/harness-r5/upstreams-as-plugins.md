# Upstreams as plugins: the rule, the inventory, on-call, and the amendments (round 5 design)

Status: design proposal, 2026-11-11, input to the decide step of round 5. It does not amend anything by itself. Conventions: `00 §n` is `docs/architecture/00-decisions.md`; `record §n` is `docs/design/plugin-system-2026-11.md`; `community §n` is `docs/design/community-first-2026-11.md`; `02` and `06` are files of `docs/architecture/`.

Owner decisions this design applies (2026-11-11, the owner's words, restated): (A) the agent layer is built new, and HolmesGPT is one agent-harness plugin a user may choose for the incident, compliance or FinOps agent, never "the investigator" of the product; (B) GoAlert is one on-call provider plugin, not the on-call engine of the product; (C) the open-source edition is written on the components chosen as plugin bases, so each licence is reviewed and anything that conflicts is rebuilt.

Scope. This file designs the rule and applies it to every upstream except the agent harness, whose definition and first-party implementation are the sibling design `docs/design/harness-r5/agent-harness.md`. Where a harness id is needed here it is a placeholder that the sibling design fixes: "the default harness row", `harness-holmes` for the HolmesGPT plugin (today `investigator-holmes`), `aiops-harness-holmes` for its bundle and `harness-sidecar` for its container. Licences are not audited here: the licence record of this round supplies each verdict, and this design only makes every verdict cheap to act on. It is engineering analysis, not legal advice; the questions a lawyer must answer are in section 7.2.

Nothing here is measured. Effort figures are estimates.

## 1. The rule

### 1.1 Terms

| Term | Meaning |
|---|---|
| Upstream | Software, a service or content that this repository does not write. Five forms, kept apart because the licence question differs for each: a **process** we deploy from an unmodified image or package; a **library** we link; a **service** we call over the network and do not deploy; **content** we ship (rules, dashboards, runbooks, control text, schemas, query strings); a **contract**, where a wire format or dialect is what a definition is declared as |
| Core | `sdk/kernel`, `boot/`, `loader/`, every definition under `sdk/def` with its fake and suite, every row of `aiops-core`, every dispatcher and policy row, the required set of the release distribution manifest, the container ids of 00 §3, and the acceptance scripts |
| Base provider allow-list | The rows of `aiops-base` that wrap an upstream: those whose register entry is a `contract` (`sql-postgres`, `sql-platform`, `kv-postgres`, `blob-s3`, `llm-provider-openai-compatible`, `identity-oidc`, `notifier-email`), plus `jobs-river`. The list is closed and lives in the register; adding to it needs an ADR |
| Upstream plugin | The one provider or contribution that wraps an upstream, with its bundle, its sidecar spec or chart toggle where it runs a process, and its cassettes |
| Stack bundle | A bundle that carries the write side of one observability stack (agent, gateway, stores, limits, provisioning, dashboards). It is absent as a whole from `aiops-only` and `minimal`. `aiops-stack`, `agent-base` and the agent bundles are the default stack bundle of this edition (community §6.6) |
| `minimal` | A test-only profile: `aiops-core`, `aiops-base`, `aiops-pillars-mvp`, `aiops-oncall`, `aiops-selfhost`. It runs the whole loop with the kernel, a PostgreSQL server and a model endpoint, and nothing else |

### 1.2 Statement (proposed principle 9 of 00 §1)

**An upstream is a plugin, never the product.**

1. Every upstream enters through exactly one provider or contribution behind a definition that has a fake and a conformance suite. The fake passes the suite, so the definition has two implementations before any upstream is attached (principle 3 already says this; the rule adds that the suite and the fake name and import no upstream).
2. The upstream plugin lives in its own bundle, in a connector pack, or in a stack bundle. A shipped profile chooses it by listing the bundle; the operator swaps it by disabling one row and enabling another.
3. Nothing in the core names or needs one specific upstream: no definition key, hook, interface type or proto package; no dispatcher or policy row; no container id; no key or row of the required set in the distribution manifest; no bundle of `minimal`; no acceptance script.
4. A library is imported only inside the plugin that wraps it. A process is deployed only when a composed bundle carries its plugin. Content carries its source and licence annotation and sits in a bundle or pack, never in a plugin of `aiops-base`.
5. Removing any one upstream plugin from the distribution leaves a binary that builds, boots every shipped profile and degrades only the capability that plugin provided, which the dump names.
6. "Default" is a word for bundles and for the Technology column. It is never a word for a contract. Where a contract really is one upstream's dialect, 00 says so as a stated exception (1.4).

### 1.3 Where an upstream's name may appear

| Place | Rule | Why |
|---|---|---|
| Definition key, hook, interface, proto package, `definition.yaml`, fake, conformance suite | Never | These outlive every implementation |
| Imports of `sdk/kernel`, `boot/`, `loader/`, `sdk/def` | No upstream module. The one exception is pgx in `sdk/def/sql` (W2) | A swap must be one plugin, not an SDK major |
| Dispatcher, policy row, any row of `aiops-core` | Never, in id, schema name, config key or required set | The core must read the same after a swap |
| Any other row of `aiops-base` | Only a row on the base provider allow-list (1.1) may wrap an upstream | `minimal` composes `aiops-base`, so what it wraps is what every install needs |
| The row that wraps an upstream | Carries the upstream's lowercase technical name once, as the `<impl>` suffix (`on-call-provider-goalert`, `jobs-river`). Exception: a row inside a stack bundle keeps a role name, because 00 §8.1 forbids the stack's marks in any name | An honest id tells the operator what they are enabling. Whether an owner's mark may sit in an id is LQ1 |
| Bundle name | A bundle whose rows all wrap one upstream is named for it and is never listed by `minimal`. A bundle that holds a dispatcher is never named for an upstream | `chat` in `aiops-chatops-slack` breaks this today (2.1) |
| Container id, compose service, chart component key, Kubernetes resource name, database name, metric name | Role-named, always. 00 §8.1 already requires it; `investigator-holmes` and `oncall-goalert` break it today | The deployment view must not change when the image does |
| 00 §2.1 decision sentences, 00 §5.4 credential classes, 00 §5.6 saga stages, 00 §6.4 cohorts | By role. An upstream appears only after "default:" or in the Rationale and Technology columns | A decision that names an upstream cannot survive its swap |
| Credential of one upstream | Declared in that plugin's `capabilities.secrets` and printed by `plugin inspect`. 00 §5.4 keeps one row per class, by role | The table stays a design artifact of the core |
| Acceptance scripts, the connector scenario | Roles and a provider map; no upstream token in a script | 5.4 |
| Plugin `title` and `description`, release notes, prose | Allowed, under the attribution rules of 00 §8.1 and community §6.7, which now apply to first-party plugins too | Referential use |

### 1.4 Stated exceptions: contracts, not defaults

Each is a place where the plan honestly keeps one implementation and says so. A swap here is a product change, not a row change.

| Contract | Where | Standing |
|---|---|---|
| PostgreSQL 16 dialect behind `sql`, `platformSql`, `sqlDriver` | 00 §2.1 row 9, §2.4 W2 | Working assumption W2, pending the owner. The only upstream every profile needs. A PostgreSQL-compatible server is a config value at the operator's risk |
| The Prometheus query API with PromQL behind `metricsQuery`; the Loki query API with LogQL behind `logsQuery` | 00 §4.1; community §6.2 | Decided. Only rows of a connector pack or stack bundle inject them |
| The S3 API behind `blob-s3`; the OpenAI-compatible API behind `llm-provider-openai-compatible`; OIDC behind `identity-oidc`; SMTP behind `notifier-email`; OCI artifacts; OTLP and remote write at the gateway edge | 00 §3.2, §4.4 | Protocols with several implementations. Naming a protocol is not naming an upstream |
| Cordis 4.1.0 semantics in `sdk/kernel` | 00 §2.1 row 1, ADR-035 | A port with attribution; the original is a test-only oracle |
| The default stack bundle | 00 §6.2; community §6.6 | Decided: this edition collects, stores, isolates and limits only on the default stack. It is a bundle, absent from `aiops-only` and `minimal` |

### 1.5 The upstream register

One data file, `upstreams.yaml` at the repository root, replaces the scattered lists (the AGPL boundary list of 00 §3.3, the tool lists of 08 §5). It is the single place where the licence record's verdicts land, and CI reads it.

```yaml
- id: goalert                      # lowercase technical name
  names: [goalert]                 # tokens the name lint looks for
  form: process                    # process | library | service | content | contract
  role: oncall-engine              # container id or ext- id, role-named
  behind: oncallProviders          # the definition; "none" for a chart add-on nothing calls
  plugins: [aiops/on-call-provider-goalert]
  bundle: aiops-oncall-goalert
  modules: []                      # Go module paths that only the listed plugins may import
  images: [oncall-engine]          # keys under images.<role> in chart and compose values
  pin: "<version>"
  licence: "<SPDX, from the licence record>"
  verdict: "<ok | ok-with-conditions | optional-plugin-only | replace-or-rebuild | unverified>"
  default: false                   # true only when a shipped profile lists the bundle
  swap: row                        # config | row | plugin | stack | contract
  exception: ""                    # for form: contract, the W or ADR that states it
```

`ponytail:` one YAML file and lints on existing tools; no registry service, no generated docs. Add a generator when the file and the tables of 00 §3.2 drift twice.

### 1.6 How CI checks the rule

Every check extends a tool the plan already has. None is review.

| # | Check | Tier, tool | Fails when |
|---|---|---|---|
| U1 | Name lint | T0; the plugin vet | A token from any `names` list appears in a definition key, hook, proto package, a path under `sdk/`, `boot/` or `loader/`, the id or schema of a dispatcher, a policy row or a row of `aiops-core`, a row of `aiops-base` outside the base provider allow-list, a container id of 00 §3, a compose service or chart component key, or a file under `test/acceptance/` |
| U2 | Import boundary | T0; the import analyzer of 00 §4.3 | A module in some entry's `modules` is imported outside that entry's plugin directories; `sdk/kernel` has any `require`; `sdk/def` requires anything but `sdk/kernel` and pgx |
| U3 | Suite neutrality | T0 | A file under `sdk/def/*/conformance` or `fake` imports an upstream module or holds an upstream token. Upstream cassettes live in the plugin directory (T4) |
| U4 | Leave-one-out render | T3; `aiopsctl profile render` on the static graph | For each shipped profile P and each upstream bundle B it lists, P without B fails to render, loses a required key, or leaves a row PENDING that the golden file `test/leave-one-out/<P>.yaml` does not list. This generalises the existing check that `aiops-only` boots without `aiops-stack` (00 §6.3) |
| U5 | Minimal distribution | T3; the builder | `dist/minimal.yaml`, generated from the register as the release table minus every upstream plugin that is not on the base provider allow-list, does not build, does not boot `minimal`, or fails leg E of 5.4. `dist/contrib.yaml` already sets the pattern |
| U6 | Required-set lint | T0; the distribution manifest | The manifest's required set holds a row id that appears in any register entry's `plugins`, or a key whose only allowed provider does, unless that row is on the base provider allow-list (1.1; section 4) |
| U7 | Register and verdicts | Release check, replacing 08 §5.1 L3 | A deployed image, linked module or shipped content directory has no register entry; an entry is `unverified`; an entry with `default: true` has a verdict other than `ok` or `ok-with-conditions`; a `replace-or-rebuild` entry appears in any `dist/*.yaml`; an AGPL entry has a non-empty `modules` |
| U8 | Deployment lint | T0 on chart and compose | An image reference sits outside `images.<role>`; an upstream component is enabled by a chart default instead of by the rendered profile; a compose service of a non-default upstream is not behind a compose profile |

What the checks do not prove: that a second real implementation would be good. The fake proves the seam; U4 and U5 prove absence is survivable; the leg E replays (5.4) prove a real alternative where one ships.

### 1.7 What the rule buys for the five uses the licences must allow

| Use | How the rule localises the question |
|---|---|
| 1. The Apache-2.0 release bundles or pulls the upstream | Each upstream is one register entry with its images and modules; the release module table and the chart defaults are derived from `default` and `verdict` (U7, U8) |
| 2. Our hosted service runs it unmodified | A process is role-named and rendered from one image value; a per-tenant instance exists only where an upstream plugin's manifest asks for one, and no default bundle carries such a plugin (section 3, sibling design) |
| 3. A commercial edition, with closed plugins in the same binary or as separate processes | A closed plugin links `sdk/*` only, never another plugin (00 §4.3), so a library with file-level or weak copyleft stays inside one plugin directory (U2). Whether same-binary linking still matters is LQ5 |
| 4. Third parties sell plugins that wrap or call the upstream | The definition, fake and suite are ours and name no upstream. A seller's adapter takes nothing from our adapter, so the upstream's terms bind only the seller's plugin |
| 5. The upstream's name in a plugin id or description | One place per upstream (the wrapping row and its bundle), lint R1 to R6 of community §6.7 extended to first-party plugins, and `renamedTo` in the index if an owner objects |

## 2. Inventory: where an upstream is hard-wired today

### 2.1 Ids

| Today | Kind | Problem | Becomes |
|---|---|---|---|
| `investigator-holmes` (00 §3.2) | Container id | Named for an upstream; in the default container set of S, A, D and per tenant in H | `harness-sidecar`: "an out-of-process agent harness, deployed only when an optional harness plugin is composed". Not in any default profile (sibling design) |
| `oncall-goalert` (00 §3.2) | Container id | Same | `oncall-engine`: "an external on-call engine the operator chooses to deploy with an optional provider plugin". Not in any default profile |
| `oncall-db` (00 §3.2) | Container, H only | Exists only for per-tenant GoAlert | Removed. The built-in provider keeps its state in `kernel-db` (3.4) |
| `investigator-holmes` (row, 00 §2.1 rows 2, 16; §4.4 `pillar-incident`) | Row in a default bundle | The default and only harness; `pillar-incident` holds it | The default harness row in `pillar-incident`; `harness-holmes` in `aiops-harness-holmes`, optional (sibling design) |
| `on-call-provider-goalert` | Row in a default bundle | The only on-call provider; `self-host-full` and `hosted` list its bundle | Stays as the id of the optional plugin. Default provider: `on-call-provider-builtin` in `aiops-oncall` |
| `on-call-proxy-goalert` (P2, `api` role) | Row, enabled by `aiops-hosted` | Hosted machinery for one upstream | Leaves the hosted profile. Kept in `aiops-oncall-goalert` only if a hosted per-tenant engine is ever offered (trigger, 6.1) |
| `aiops-oncall-goalert` | Bundle listed by `hosted` and `self-host-full` | A default bundle named for an upstream | Optional bundle, listed by no shipped profile. New default bundle `aiops-oncall` |
| `tool-oncall-who` in `aiops-oncall-goalert` | Row | A neutral tool (it asks `paging`) shipped only "where GoAlert runs" | Moves to `aiops-base`. Its `prerequisites` state "an on-call provider is registered for the tenant", so the `tools` dispatcher offers it only then |
| `chat` in `aiops-chatops-slack` | Dispatcher in a bundle named for one service | A second chat product would need the Slack bundle | Moves to `aiops-base`. The bundle keeps `chat-surface-slack`, `slack-commands`, `notifier-slack`, `approval-surface-slack`, `identity-slack-link` |
| `approval-surface-app` in `aiops-base` (P2) | Row in base | Needs the app plugin of one dashboard server | Moves to `aiops-dashboards` (5.1) |
| `toolset-bindings` in `aiops-base`; four `toolset-binding` rows "for HolmesGPT's native toolsets" in `aiops-datasources` | Dispatcher and default-pack rows | The registry's concept (a toolset of one harness) is HolmesGPT's; the default pack's rows serve only that harness | Sibling design decides the neutral read path. This design's constraint: rows that bind one harness's native toolsets ship in that harness's bundle, and the default pack carries only rows the default harness uses |
| `tools-mcp-facade` in `aiops-base` | Row | Exists to serve kernel tools to an out-of-process harness | Sibling design decides its home. Constraint: `minimal` must run without it |
| `gateway-renderer`, `fleet`, `fleet-composer`, `fleet-template-*`, `annotator`, `dashboard-sync`, `stack-provisioning`, `limits` | Rows of the stack bundle | Each works with one upstream and carries a role name | Unchanged: the stack-bundle exception of 1.3. The swap unit is the bundle |
| `jobs-river`, `sql-postgres`, `kv-postgres`, `blob-s3`, `events-nats`, `kek-kms`, `sidecars-kubernetes`, `lease-kubernetes`, `remediation-executor-kubernetes`, `chat-surface-slack`, `fleet-template-sensor-tetragon` | Provider and contribution rows | None. They are the wrapping rows and carry the `<impl>` name | Unchanged |
| `plugins/investigator-holmes/`, `aiops/investigator-holmes` (00 §8.1 examples) | Naming examples | An upstream plugin as the model example | Use a first-party example (`aiops/notifier-email`, `plugins/notifier-email/`) |

### 2.2 00-decisions, by section

| Where | Hard-wired today | Neutral replacement |
|---|---|---|
| §1 Kernel bullet; README line 11 | "investigates them with an LLM (HolmesGPT), pages people (GoAlert)", "serves Slack chat-ops" | "investigates them with an agent harness, pages the on-call person, serves chat-ops"; defaults named once, after "default:" |
| §1 principle 6 | "(HolmesGPT, GoAlert) run per tenant in hosted" | "An upstream process with unauthenticated admin endpoints or a single-tenant design runs one instance per tenant in hosted; its plugin manifest states it. No default bundle carries such a process" |
| §1 principle 7 | The list "Alloy, the four backends, Grafana, HolmesGPT, GoAlert, Envoy and the object store" | "every upstream process a composed bundle deploys, each listed once in the upstream register"; plus the new principle 9 |
| §2.1 row 2 | "HolmesGPT is the only MVP investigator" and all that follows | Sibling design |
| §2.1 row 3 | GoAlert as provider, per-tenant in hosted, `oncall-db`, ceiling 40, subdomain proxy, Twilio subaccounts, "We write no paging code", native module on trigger | Section 3: a first-party engine is the default; external engines are optional provider plugins; no per-tenant engine in hosted |
| §2.1 row 5 | "the Alertmanager `Watchdog` route straight to the provider's `Heartbeat`"; rationale "GoAlert has no grouping" | 3.5: the closed list is restated by role; the rationale is "an on-call provider is not assumed to group" |
| §2.1 row 7 | "Envoy in every profile that runs the backends" | "The tenant gateway proxy in every profile that composes the stack bundle (default: Envoy)". The `gateway` suite becomes behavioural (section 4) |
| §2.1 row 9 | "River runs on the same database behind `jobs`", "River job args stay compatible", S0 spike items | Decision by role ("the bound `jobs` provider runs on the same database"); the River facts move under "Provider rules" as facts of `jobs-river` |
| §2.1 rows 13, 16, 19 | "HolmesGPT's `token_count`", "No HolmesGPT pod may hold the provider key", "around `investigator-holmes`", "its native logs toolset" | "the harness's reported usage as a fallback meter", "no harness holds a provider key", "around whichever harness the pillar selects", "the bound logs read tool" (sibling design) |
| §2.3 | "`on-call-provider-builtin` is trigger-based"; "`notifier-sms-twilio` and `notifier-push-ntfy` are dropped" | The built-in provider is built at S8. Carrier notifiers are not first-party at the first release and are open to contribution (3.4) |
| §3.1 `kernel`, `kernel-api`, `kernel-worker` rows | "on-call adapter"; "per-tenant on-call subdomains (GoAlert UI and Twilio callbacks)"; "River workers, the leaders (River, ...)" | "the on-call provider"; removed; "job workers, the leaders (jobs, ...)" |
| §3.2 | `investigator-holmes`, `oncall-goalert`, `oncall-db` rows; `kernel-db` "in S also the GoAlert and dashboard databases"; the AGPL boundary paragraph as prose | Rows renamed and marked "optional, with its plugin"; `oncall-db` removed; the boundary paragraph points at the register, which U7 checks |
| §3.3 | `ext-paging-carriers` "GoAlert contact methods"; `ext-pagerduty` "tenants above the GoAlert ceiling" | "Targets of page notifiers: an SMTP relay, a webhook push target, a carrier where a carrier notifier is installed"; `ext-oncall-saas`: "an external on-call service reached by a provider plugin" |
| §4.1 | `investigators` and `oncallProviders` rows list first-party rows `investigator-*`, `on-call-provider-*`; `sidecars` "upstream process" | `oncallProviders` first-party row: `on-call-provider-builtin`. `notifiers` gains the `page` class (3.4). Keys are already neutral |
| §4.4 | Bundle table | Section 5.1 |
| §5.2 | `paging` "(the GoAlert data stays in GoAlert's own database, on `oncall-db` in hosted)"; durable `external` example "a GoAlert database" | "(an external engine keeps its own data)"; new schema `on_call_provider_builtin`; example "a database of an external engine" |
| §5.3 | "Schedules and escalation: GoAlert" | "Schedules and escalation: the bound on-call provider; with the default provider, schema `on_call_provider_builtin`" |
| §5.4 | Seven upstream-named rows: three GoAlert rows, Twilio subaccount token, SMTP "each GoAlert", "Investigator service JWT ... HolmesGPT's native toolsets", "rendered into the HolmesGPT toolset config", "HolmesGPT's Kubernetes toolset" | One class "External on-call provider credentials: tenant secrets under the provider plugin's scope, declared in its manifest". New class "Page ack token" (3.4). Harness rows by role (sibling design) |
| §5.6 | Create saga lists GoAlert database, admin user, keys, heartbeat monitors, Twilio subaccount; delete stage 5 "Drop the GoAlert database, delete the Twilio subaccount" | "each composed plugin performs its own step on `tenant/create`"; stage 5 "`external` namespaces declared by composed or detached plugins". No upstream is named |
| §5.7 | "Dead-man path ... tenant GoAlert's heartbeat endpoint"; GoAlert ingress rule | 3.5: the platform-health receiver is a fixed peer of the Alertmanager egress policy |
| §6.1 required set | Hosted adds the rows `fleet-agentapi`, `signal-ingest-alert-webhook`, `kek-kms` | Unchanged in substance, moved: rows of a stack bundle may be required only by a profile layer of a profile that composes that bundle, never by the distribution manifest (U6) |
| §6.3 | `hosted` and `self-host-full` list `oncall-goalert`; `aiops-only` "(GoAlert optional)"; `dev` "compose with PostgreSQL, Envoy, SeaweedFS" | Section 5.2 |
| §6.4 | "the image versions of the tenant's sidecars (HolmesGPT, GoAlert)" | "the image versions of a tenant's upstream sidecar processes, where an optional plugin runs one" |
| §7.1 | "GoAlert pages by SMS", "HolmesGPT posts an RCA", "a Slack thread", "PolicyReport and Kubescape findings", the `dev` no-page form | Section 5.4 |
| §7.2 | S7 "Investigator spike: cassettes at HolmesGPT 0.46.0"; S8 `investigator-holmes`, `on-call-provider-goalert`; hosted "per-tenant HolmesGPT", "GoAlert per tenant on `oncall-db` under the ceiling"; standing artifact "one always-on pod per tenant (GoAlert)" | S8 builds `on-call-provider-builtin`; the hosted items are removed; the standing target becomes "no always-on pod per tenant by default" |
| §7.3 | Triggers that name GoAlert or HolmesGPT (`on-call-provider-builtin`, the 30th hosted tenant, `tool-*-query`, `investigator-native`) | Section 6.1 |
| §9 | ADR-003, ADR-004, ADR-014 (HolmesGPT); ADR-008 (GoAlert ceiling); ADR-021 (GoAlert heartbeats, `oncall-db`) | ADR-008 withdrawn; ADR-021 rewritten (3.5); the harness ADRs follow the sibling design; two new ADRs (6.1) |

### 2.3 Other documents and figures

Occurrences per file of the strings `holmes` and `goalert`, case-insensitive, counted on 2026-11-11, for the writers of the update step. 00 itself holds 43 and 67.

| File | `holmes` | `goalert` | What changes |
|---|---|---|---|
| `01-c1-system-context.md` | 10 | 34 | The system box holds on-call by default; `ext-paging-carriers` reworded; the hosted rows on per-tenant engines removed |
| `02-c2-containers.md` | 34 | 31 | §1 list of upstream processes; C2-a1 "the two upstream engines the kernel drives" (there are none by default); container and relationship tables; D-1 tenant namespace and `oncall-db`; D-2 and D-3 node table; coverage table |
| `03-c3-kernel.md` | 48 | 16 | Role placement, failure matrix cells for paging, `jobs-river` prose stays as provider facts |
| `04-c3-agent-fleet-gateway.md` | 2 | 0 | The readers of the gateway read routes, by role |
| `05-c3-pillars.md`, `09-c3-estate-and-troubleshooting.md` | 110, 32 | 12, 0 | Sibling design |
| `06-c3-chatops-oncall.md` | 40 | 134 | §1.1 rows, §8 whole, §8.2 adapter mapping moves to the plugin's own README, §8.3, §8.4 (no pinned tenant row by default), §9 becomes the built-in provider's design, §10, §11 |
| `07-c4-plugin-sdk.md` | 25 | 2 | `notifiers` types gain `page`, `Recipient`, `Direct`; the `oncallProviders` interface is unchanged |
| `08-profiles-roadmap-adrs.md` | 29 | 26 | §1.4 bundles per profile, DR-02, DR-03, DR-15, §4 layout (`deploy/compose` comment), §4.1 chart (no HolmesGPT or GoAlert Deployment by default), §5 from the licence record and the register |
| `README.md` (repository) | 1 | 1 | Line 11, as in 2.2 |
| Plugin record, community record | 3, 7 | 7, 0 | Section 6.2, 6.3 |

Figures whose sources name one of the two upstreams in a node id or label: 27 of 72 under `docs/architecture/diagrams/src/`. Redraw with role ids; a default view draws no harness or on-call container outside the kernel.

| Group | Figure ids |
|---|---|
| On-call only (6) | `01-c1-context-aiops-only`, `01-c1-context-hosted`, `02-control-signals`, `05-signal-to-incident`, `06-alert-page-ack`, `06-oncall-components` |
| Both (5) | `02-deploy-hosted-cell`, `02-deploy-self-host`, `03-kernel-signal-to-incident`, `06-deployment-hosted`, `06-deployment-selfhost` |
| Harness only (16) | `02-control-investigate`, `02-optional-aiops-only`, `03-alert-to-remediation-flow`, `03-kernel-connector-seams`, `04-change-evidence-path`, `04-gateway-filters`, `05-change-evidence`, `05-compliance-pillar`, `05-connector-seams`, `05-investigation`, `05-pillar-composition`, `06-slack-conversation`, `07-connector-pack`, `08-connector-packs-aiops-only`, `09-investigation-sequence-p1`, `09-method-plugins-hooks` |
| Other upstream labels (provider facts; the label may stay, in the Technology line) | `03-kernel-infrastructure-providers` and `06-slack-conversation` (`jobs-river`), `04-enrol-first-write`, `04-kernel-agent-path`, `04-kernel-control-path`, `05-investigation`, `09-investigation-sequence-p1` (`Envoy`), `09-correlate-components` ("River job" becomes "job") |

## 3. On-call

### 3.1 What changes whichever option the owner picks

1. GoAlert is one optional provider plugin: `aiops/on-call-provider-goalert` in the bundle `aiops-oncall-goalert`, listed by no shipped profile. Its container, when the operator has the chart deploy one, is `oncall-engine`.
2. The hosted machinery built around it leaves the plan: one engine per tenant, `oncall-db`, the ceiling of 40 and ADR-008, the subdomain proxy on the `api` role, one Twilio subaccount per tenant, the pinned tenant row and its share of ADR-036, three credential classes of 00 §5.4.
3. `paging` and the `oncallProviders` contract stay as 00 §4.1 and 07 have them: `WhoIsOnCall`, `Page`, `Ack`, `Resolve`, `Status`, optional `Heartbeat`. The suite and the fake are already neutral.
4. The dead-man design can no longer assume that an external engine watches the kernel (3.5).

### 3.2 The choice

| | (a) No engine of our own | (b) A small first-party engine as the default |
|---|---|---|
| Default profiles compose | `paging` with no provider | `paging` with `on-call-provider-builtin` |
| Who hears about an incident | Everyone behind the notifier rows that receive `incidents`: a channel, a mail list, a webhook | The on-call person of the bound policy, then the next escalation step |
| Rotation, overrides, escalation, repeat until ack | None, unless an external engine is installed | Yes |
| Ack | Recorded by `paging`; it stops nothing | Stops the escalation |
| On-call in a first install | No | Yes, with no extra container, in `dev` and `aiops-only` too |
| The page step of the acceptance demo | Runs on the conformance fake, or names an upstream (which the rule forbids), or leaves the demo | Runs on first-party code |
| Hosted | An engine per tenant or a SaaS account per tenant | Multi-tenant by row-level security like every other plugin; no process per tenant |
| Pager that survives a dead kernel | The external engine's heartbeat monitor, schedule-aware | Not provided by the engine. Replaced by a static platform-health receiver (3.5) |
| SMS and voice | Whatever the external engine has | Not first-party at the first release: email, chat direct message and webhook, which reaches a push service or an SMS gateway. A carrier notifier is a contribution |
| Cost to build | None beyond the provider plugins | About 8 to 9 engineer-weeks for v1 (3.7) |
| Cost to keep | Adapter drift against each upstream's API | Schedule arithmetic, escalation reliability, a schema |
| The owner's original request, an on-call module | Not met: the module is somebody else's product | Met |

### 3.3 Recommendation: (b), with a fence

Reasons, in order of weight:

1. **The request.** The owner asked for an on-call module because the open-source on-call project of the default stack was archived (the reason as given in this round's brief; not re-read here). Under (a) the product has no on-call until the operator installs another project, which is the situation the request was meant to end.
2. **The rule.** 00 §2.1 row 12 lists on-call in the MVP scope. A capability in the MVP scope must run on `minimal`; under (a) it cannot.
3. **Decision B read with decision A.** The owner's pattern for the agent layer is a first-party default with the upstream as one alternative. (b) is the same pattern.
4. **Hosted.** (b) removes a second database, a proxy row on the internet-facing role, three credential classes, a tenant ceiling and the one always-on pod per tenant.
5. **The marketplace.** Page channels become `notifiers` contributions, which third parties can already write in any language (S5).

The fence that keeps (b) small, stated so that a later request is a visible scope change:

- One process-scope plugin, no upstream code, no port: it is written from the table in 3.4.
- The time arithmetic is the standard library's (`time` with IANA locations). No recurrence-rule library.
- Notification only through `notifiers`. The engine holds no carrier client.
- No schedule editor UI at the first release: a file applied by `aiopsctl`, the API, and two chat verbs.
- It does not try to be the pager of last resort for its own host process (3.5).

00 §2.1 row 3 said "schedule math would be a five-year maintenance item for us". That stays true and is the price of (b). The fence bounds it: one rotation shape and overrides at v1, a property test over every instant of a year in three time zones, and every richer rule behind a trigger.

### 3.4 `on-call-provider-builtin`, v1

A contribution to `oncallProviders` in the new bundle `aiops-oncall`, process scope, schema `on_call_provider_builtin` under row-level security, role `worker` for its jobs and `api` for its routes. It passes the existing `oncallProviders` suite, so swapping it for another provider is disabling one row and enabling the other.

| Part | v1, built at S8 | Not in v1; trigger |
|---|---|---|
| Responder | A platform user (`tenant_directory.platform_user`) with contact methods (a notifier target name plus an address) and ordered notification rules (delay, contact method) | |
| Rotation | An ordered list of responders, a shift length in hours, days or weeks, a handoff time and an IANA time zone. "Who is on call at t" is a pure function | Time-of-week restrictions and layered rotations: the first tenant that needs follow-the-sun |
| Schedule | One rotation plus overrides (replace a responder for an interval). An override wins | Temporary schedules, calendar export: on request |
| Escalation policy | Ordered steps. A step has targets (a schedule or a responder) and a delay to the next step; the policy has a repeat count. After the last repeat the page is `unacknowledged`, which the timeline and the thread show | Per-priority policies: the owner-routing trigger of 00 §7.3 |
| Binding | `paging.provider_binding` names the provider row id and a policy; `provider` stops being the literal `goalert` | |
| `Page` | Idempotent on incident id. Writes page state and a delayed job per step through `jobs`, unique on (page, step, repeat). Each step resolves its targets at fire time and sends one `Notification` of class `page` per notification rule | |
| `Ack`, `Resolve` | Stop the pending steps. From the chat button, the API, `aiopsctl`, or a signed one-time ack link in the page body | Ack by SMS reply or by voice key: a carrier notifier with an inbound route |
| `Status`, `WhoIsOnCall` | Local reads. The reconcile poll of `paging` becomes a no-op for this provider | |
| `Heartbeat` | The end-to-end monitor only, in process (3.5) | |
| Administration | `aiopsctl oncall apply -f` (schedules, policies and bindings as one file, so they can live in Git), `oncall who`, `oncall override`; the same over the API; `/aiops oncall who` and `oncall override` in chat. A provisioned dashboard panel shows the roster | An editor in the dashboard app: with `aiops-app` (P2) |
| Limits | Per tenant: schedules, policies, steps, pages per hour, in `TenantLimits` | |

`Schedules` and `Overrides` do not enter the `oncallProviders` contract (00 §7.3 said they would arrive with this row). Their only caller is the plugin's own API and commands, and an interface method with one implementation and one caller in the same plugin is not a seam.

Changes to `notifiers` (definition `v0`; its types are first written at S5, so these are part of the first form, not a later break):

| Change | Rule |
|---|---|
| `Notification.Class` gains `page` | The `notifications` dispatcher sends a `page` with no `notify/pre-send` call, no digest window and no pacing. This keeps 00 §2.1 row 5: nothing a plugin registers can withhold a page. The guard half of the suite asserts it |
| `Notification.Recipient` | An address for one person (`mailto:`, a chat user id, a URL, `tel:`), set only by the on-call provider and valid only with class `page`. The scrub applies to title and body, never to the recipient |
| `Capabilities.Direct` | The notifier can deliver to a `Recipient`. A notifier without it cannot be a contact method |
| `receives` gains `pages` | Row config, as for the other classes. `notifier-email`, `notifier-slack` and `notifier-webhook` ship with it |
| A second consumer of `notifiers` | `on-call-provider-builtin` injects the key beside `router`. The key is provider-bound, not consumer-bound, so the binding maps do not change |

Two consequences that 00 must state plainly:

- **Paging now depends on `pii`.** With the built-in provider a page leaves through `notifications`, which injects `pii`. Today 00 §6.1 says paging keeps running when `pii` is lost. That sentence becomes true only for an external provider. The page body is built by code from the incident key, the priority, the signal type and a link, so the scrub has little to do, but the dependency is real. The platform-health route of 3.5 covers it.
- **Paging shares fate with `kernel-db`.** 00 §2.1 row 3 split `oncall-db` so that it would not. With the built-in provider that property is gone, in hosted too. The platform-health route covers it. An operator who needs an independent pager installs an external provider plugin, and that is the documented reason to do so.

Contact addresses are personal data in a tenant table. They are purged with the plugin's schema at tenant delete and are never sent to a model.

### 3.5 The dead-man contract, restated by role

Today the `Watchdog` route posts to the external engine's heartbeat monitor, and a second monitor on the same engine proves the kernel path (ADR-021). An engine inside the kernel cannot report the kernel's own death (06 §9 says so). The contract becomes two watchers in two failure domains that watch each other, and it names no upstream.

| Watches | Watcher | Mechanism | Pages through |
|---|---|---|---|
| The alerting path (ruler, Alertmanager, the webhook to ingest) | The kernel | The always-firing `Watchdog` alert arrives at `signal-core` every minute and beats the bound provider's end-to-end monitor through `paging.Heartbeat`. A missed beat for 10 minutes pages the tenant's `platform` policy | The bound on-call provider |
| The kernel's page path (process up, ingest ready, `notifications` and its policy providers ACTIVE, `kernel-db` reachable) | The alerting stack | One rule group `platform-page-path` on the kernel's own readiness series, routed by the Alertmanager straight to the **platform-health receiver**: a static email or webhook receiver from operator config, never through the kernel | The Alertmanager's own notifier |
| Both at once (one VM lost) | Nobody inside the product | The self-host notes say so, as they do today for a shared database. An external heartbeat URL can be set as an extra target of the kernel's beat | |

- The closed exception list of 00 §2.1 row 5 is restated: platform health may bypass ingest, estate alerts never do. P1 has two entries: `Watchdog` (to ingest, and also straight to the provider's direct heartbeat target when the bound provider returns one), and `platform-page-path` (straight to the platform-health receiver). The P2 "rule group about the platform's own components" is this second entry, moved to P1 and limited to the page path.
- An external provider that implements `Heartbeater` and returns a direct target keeps today's stronger form: its own monitor is the schedule-aware pager of last resort.
- The T3 test of 06 §8.3 keeps its three assertions, with "the entries of this list for the phase" now two.
- In `aiops-only`, `aiopsctl` prints the rule group, the route and a scrape snippet for the customer's own ruler and Alertmanager, as it prints the `Watchdog` snippet today.
- The sender side stays specific to the default stack (community §6.6): the rules are Prometheus rules and the routes are Alertmanager routes.

ADR-021 is rewritten around this table. What it still has to settle: the timeout under a long `group_interval`; which readiness series the rule group reads and their names; whether the platform-health receiver may be one of the tenant's own notifier webhooks.

### 3.6 The GoAlert plugin

| Aspect | Design |
|---|---|
| Id, bundle | `aiops/on-call-provider-goalert` in `aiops-oncall-goalert`; first-party, optional, `stability: experimental` until it has a maintainer and a fixture set |
| Default form | It connects to a GoAlert the operator already runs, as a provider plugin for a SaaS does: an endpoint and an admin login passed once to its setup step (00 §5.4 has this for `aiops-only` today) |
| Optional form | The chart or compose deploys one shared `oncall-engine` instance behind a value that is false by default and a compose profile. Whether the release may reference or mirror that image is for the licence record (LQ2) |
| Hosted | Not offered at S9b. A hosted tenant that wants an external engine points a provider plugin at an endpoint it runs. A per-tenant engine run by us needs a trigger and its own ADR |
| What moves out of the architecture set | 06 §8.2 "adapter mapping" and "GoAlert API keys", the setup saga step, the heartbeat monitors: they become the plugin's README and its `capabilities.secrets`. The three credential rows of 00 §5.4 become one class by role |
| Proof | The `oncallProviders` suite; T4 cassettes at the pinned version; leg E of 5.4 |
| When | Not on the path to the first release. Built at S8 if there is capacity, else S9a; leg E runs with whichever optional providers exist and with the conformance fake otherwise |
| Nothing taken | The built-in provider takes no code, schema or test from it. The adapter holds GraphQL query strings for its API; whether those are copied content is LQ3 |

### 3.7 What (b) costs

Estimates, in engineer-weeks, not measured.

| Item | Weeks |
|---|---|
| Schema, the apply file, API and CLI | 1.5 |
| Rotation and override arithmetic with property tests (daylight-saving handoffs, zone changes, overlapping overrides) | 2 |
| Escalation on `jobs`, ack and resolve, the ack link, the suite, the two-tenant test | 2 |
| `notifiers`: class `page`, `Recipient`, `Direct`, the guard tests | 1 |
| The dead-man restated: the rule group, the platform-health receiver, the T3 test, the printed snippets | 1 |
| Documents, the roster panel, the demo fixtures | 1 |
| **Total for v1** | **8.5** |
| Removed from the plan: the per-tenant engine in hosted (`oncall-db`, proxy, subaccounts, pinned rows, the ceiling work) | Not estimated in 00; at least as large |
| Still to pay if the GoAlert plugin is wanted | The adapter and its setup step, self-host form only |

Standing cost: the arithmetic (time zone data arrives with the Go release), escalation correctness under restarts (jobs are durable; a step that fires twice sends twice, which the notifier's `DedupeKey` absorbs), and support questions that an on-call product attracts. `ponytail:` one rotation shape; add restrictions and layers on the trigger in 3.4.

### 3.8 What the product offers with no provider at all

With (b), "no provider plugin installed" only means no external one: the built-in engine is in every shipped server profile. If the operator disables it, or binds no policy, the behaviour is what 06 §1.1 already describes: `paging` is ACTIVE with nothing registered or bound, the page job records "no provider" or "no policy" on the timeline, and the incident notice still goes to every notifier row that receives `incidents`. Under (a) that is the whole offer.

## 4. The other upstreams the plan bundles

| Upstream (role id) | Form | Behind a definition today | Still named in the core | Verdict of this design | Swap class |
|---|---|---|---|---|---|
| Tenant gateway proxy, Envoy (`gateway`) | Process | Yes: `gateway`, provided by `gateway-renderer` in `aiops-stack`; the container id is a role | 00 principle 7; row 7 "Envoy in every profile"; §6.3 `dev`; "the check of the rendered configuration against Envoy is the suite of the `gateway` definition" (08 §4) | **Keep as the default with no second implementation.** One component enforces tenancy and gets one security review. Two fixes: the decision text names the role; the `gateway` suite becomes a black-box behaviour table run against a running proxy (verify a token, inject and strip the tenant header, the route table, 401 and 403 answered as 503 on write routes, body limits, fail closed, a key change changes the listener), and the byte-level goldens become the provider's own tests | Plugin: a second provider row of `gateway`, inside a stack bundle |
| Object store, SeaweedFS (`object-store`) | Process | Yes: `blob`, provider `blob-s3`. The server is an image value; a customer endpoint already replaces it | 00 §3.2 Technology; §6.3 `dev`; 08 DR-15 and the `deploy/compose` comment | **Already a provider; the server is a config choice.** One gap: `blob-s3` is the only provider, so every composition needs an S3 server, and 00 marks the store optional in `aiops-only` without saying what then provides `blob`. Recommended: `blob-postgres` (objects as rows under row-level security, a size cap) as the provider in `minimal` and in `aiops-only` without a store. `ponytail:` rows with a 32 MiB cap; move a tenant to `blob-s3` when reports exceed it. This also means a kernel-only install needs no second server if the bundled store's verdict ever changes | Config (the server); row (the provider) |
| Jobs library, River (`jobs-river`) | Library | Yes: `jobs`, provider-bound to `jobs-river`, with an in-memory fake | 00 row 9 decision text; §3.1 "River workers", "the leaders (River, ...)"; §5.5 "the job priority River already has"; §4.3 provider rules (correct place) | **Keep as the default with no second real implementation.** The fake is the second implementation; a real one is not built. What keeps the swap open: no River type in the `jobs` interface or suite; River imported only under `plugins/jobs-river/` (U2), which also means every leader takes leadership through `jobs` or a lock through `sql`, never River's API; no feature of a paid edition (08 L9); the `Drained()` procedure. `jobs-river` is on the base provider allow-list because `jobs` is a required key with one allowed provider | Plugin: a new `jobs-<impl>` provider and one binding line |
| Dashboard server, default Grafana OSS (`dashboard-ui`) | Process, plus content, plus the app plugin (P2) | Partly: `annotations` (optional, singular) and `content`. Provisioning files are bundle content | `approval-surface-app` in `aiops-base`; `annotator` and the dashboard content inside `aiops-stack`, so `aiops-only` has no annotation even where the customer runs the same server; §7.1 steps 1, 3 and 6 | **Keep as the default with no second implementation**: it is the product's visual surface, and the API, `aiopsctl` and chat already carry every person step without it. One fix: split the dashboard rows and content out of `aiops-stack` into a bundle `aiops-dashboards`, so the server can be left out, or be the customer's own, without dropping the stores | Stack: new content, an `annotations` provider and an app |
| Agent, default Alloy with the upstream cluster chart (`agent`) | Process, chart, content | Partly: `fleet` implements that agent's remote-config API; `fleetTemplates` emits its config syntax | 00 §1 Agent bullet; row 6; row 11 | **Keep as the default with no second implementation.** The neutral seam is not `fleet`: it is the enrol and token API, the write routes at the gateway (remote write, logs push, OTLP over HTTP, profiles push) and the published label contract of 04, which a customer-run collector can meet today (00 row 11). A second served-config protocol keeps its trigger (the OpAMP listener, 00 §7.3) | Stack |
| PostgreSQL (`kernel-db`) | Contract | `sql`, one allowed provider | Everywhere, by decision | Stated exception (W2) | Contract |
| The four stores, the data-plane log | Process | Reads: `metricsQuery`, `logsQuery`, connector packs. Writes: the stack bundle | 00 §3.2, §5.3, §5.5, §5.6 by role ids | Default stack bundle; unchanged | Stack (write side); row (read side) |
| Slack (`ext-slack`) | Service, library | `chatSurfaces`, `notifiers`, `identities`, the approval surface | The `chat` dispatcher sits in the Slack bundle; 00 §1 "Slack chat-ops"; §7.1 | Move `chat` to `aiops-base`; demo text by role | Row |
| Model endpoint | Service | `llmProviders` | No | Compliant | Config |
| Sensor, rightsizing runner, fan-out bus, renderers, the assistant endpoint | Process | `fleetTemplates`, `signals`, `events`, `reportRenderers`; the assistant endpoint has no caller and is a chart add-on (`behind: none`) | No; all optional or later | Compliant; each gets a register entry | Row or plugin |
| Signature tool (ADR-020) | Library or tool | No definition: the gate's trust check and install call it | Possibly inside `boot/` | Open (O5): verification in `boot/` must be standard-library code over a documented signature format, or sit in a plugin, because U2 allows no upstream module in `boot/` | Plugin |

Read with section 3 and the sibling design, the required keys of every server profile then rest on two upstreams: PostgreSQL, a contract, and River, a library behind `jobs`. Everything else a first install needs is first-party or a protocol.

## 5. Bundles, profiles and the acceptance demo after the change

### 5.1 Bundles (changes only)

| Bundle | Change |
|---|---|
| `aiops-base` | Gains `chat` (from `aiops-chatops-slack`), `tool-oncall-who` (from `aiops-oncall-goalert`) and, if adopted, `blob-postgres` (disabled where `blob-s3` is enabled). Loses `approval-surface-app` (P2). Harness rows per the sibling design |
| `aiops-oncall` (new, default) | `on-call-provider-builtin` |
| `aiops-oncall-goalert` | Optional, listed by no shipped profile: `on-call-provider-goalert` with an optional sidecar spec. `on-call-proxy-goalert` is withdrawn from the plan |
| `aiops-harness-holmes` (new, optional; name from the sibling design) | The HolmesGPT harness plugin, its sidecar spec, and the rows that bind its native toolsets |
| `aiops-chatops-slack` | Five rows: `chat-surface-slack`, `slack-commands`, `notifier-slack`, `approval-surface-slack`, `identity-slack-link` |
| `aiops-datasources` | The default connector pack keeps its dialect rows (`metrics-query-http` and the P2 rows) and the host runbook. Its investigator-facing rows are whatever the default harness reads through (sibling design) |
| `aiops-stack` | Loses `annotator`, the dashboard provisioning files, and at P2 `dashboard-sync` and `identity-dashboard-token` |
| `aiops-dashboards` (new, default where a dashboard server is ours or the customer's) | `annotator`, the provisioned dashboards as content, the on-call roster panel; P2: `dashboard-sync`, `identity-dashboard-token`, `approval-surface-app` |
| `aiops-hosted` | Loses the per-tenant instances "for `investigator-holmes` and `on-call-provider-goalert`" and `on-call-proxy-goalert`. `sidecars-kubernetes` stays for remote plugin instances |
| `aiops-dev` | Loses "no on-call binding": `dev` pages a local webhook sink |

How an operator turns on an optional first-party upstream plugin: exactly as an installed artifact (00 §6.1). `aiopsctl plugin install aiops/on-call-provider-goalert` finds the module in the release table, so it answers no `rebuild-required`; it appends the plugin's bundle after the profile's own, rows disabled; `plugin enable` follows; the chart or compose renders the sidecar unit only if the operator sets its value. No new mechanism. If a verdict later requires it, the plugin leaves the release table by deleting one line of `dist/release.yaml` and is published as a separate artifact.

Row counts in 00 §4.4 are recomputed in the decide step from both designs.

### 5.2 Profiles

| Profile | Bundles, in order | Containers a first install runs |
|---|---|---|
| `minimal` (test-only; new) | core, base, pillars-mvp, oncall, selfhost | `kernel`, `kernel-db` |
| `aiops-only` | core, base, datasources, chatops-slack, oncall, dashboards (optional, against the customer's own server), pillars-mvp, selfhost, then installed packs | `kernel`, `kernel-db`; an object store only if `blob-s3` is chosen |
| `dev` | core, base, datasources, stack, dashboards, agent-base, chatops-slack, oncall, pillars-mvp, selfhost, dev | The above plus the stack: the gateway proxy, the object store, the four stores, the dashboard server |
| `self-host-full` | core, base, datasources, stack, dashboards, agent-base, (agent-security from P2), chatops-slack, oncall, pillars-mvp, (pillars-ext from P2), selfhost | As `dev`. No harness container and no on-call container |
| `hosted` | core, base, datasources, stack, dashboards, agent-base, (agent-security), chatops-slack, oncall, pillars-mvp, (pillars-ext), hosted | The cell of 02 D-1 without `oncall-db` and without tenant instances of a harness or an on-call engine. A tenant namespace holds remote plugin instances only |
| `cli`, `agent-host`, `agent-k8s` | Unchanged | |

`ponytail:` `minimal` is a test profile, not a shipped one. Ship it when someone asks for a kernel-only install.

### 5.3 What the first release lets an operator do (community §3.1, reworded)

An operator can install the agent on a VM and a cluster, see four signals, receive alerts, get one incident per entity with a chat thread and one page to the on-call person of a rotation, read an RCA from the default harness, approve and apply one of five Kubernetes actions, ask for the compliance score, export the audit chain, and run it all on a local open-weight model. Nothing in that sentence needs a container beyond the kernel, its database and the default stack.

### 5.4 The acceptance demo, by role

The scripts live under `test/acceptance/` and take a provider map (`providers/<name>.yaml`: role to row id). U1 fails a script that holds an upstream token.

**Leg A, the operator,** on `self-host-full` with `providers/default.yaml`:

1. A host installer run and a cluster chart install of the agent both appear in the fleet inventory within two minutes, and all four signals show on the profile's dashboards.
2. A rule from the shipped rule files fires on one entity. The burst becomes one incident keyed on that entity, and a thread opens on the bound chat surface. The on-call person of the bound escalation policy is paged once on their first contact method, and acks from the thread. A second run lets the first step time out and asserts the page reaches the second step.
3. The harness of the incident pillar posts an RCA to the thread, citing the last deployment, taken from the change candidates the kernel put in the first message, and log evidence read through the bound logs tool. The RCA is also written through `annotations`.
4. Unchanged: the thread offers rollout restart (L1); a second user approves; the action applies; the two zero-cost checks verify it; the incident closes when the alert clears.
5. `/aiops compliance score` answers from the findings of the two scanner payload families in the fixtures. A failed control shows as a `control:<id>` incident.
6. Unchanged: the loop is visible in the provisioned dashboards and exportable from the audit chain.

The host leg and the local-model run are unchanged except that "the logs toolset" reads "the bound logs tool". In CI the first contact method is a webhook sink; a live run uses email or a chat direct message. The `dev` no-page form is removed: `dev` composes `aiops-oncall`.

**Leg B, own stores,** unchanged in what it proves. It asserts that the timeline says no policy is bound, in place of "no page". Its investigator half no longer runs "the pinned investigator image": it runs the default harness with replayed model turns and one successful call on each bound read tool. A pack that binds an optional harness's native toolsets proves them in that harness plugin's own suite (sibling design; community §6.4 amended).

**Legs C and D** are unchanged.

**Leg E, upstream independence (new).**

- E1: the `dist/minimal.yaml` binary boots `minimal` and passes steps 2 to 4 and 6 of leg A from fixtures: alert in by the generic webhook, one incident, one page to a webhook sink, an RCA from the default harness on replayed model turns, an approved action on a recording executor, the audit export. Two containers: the kernel and its database.
- E2: the leave-one-out render (U4) is green for every shipped profile.
- E3: for each optional first-party upstream plugin that ships in the release (the external on-call provider, the external harness), steps 2 and 3 are replayed from that plugin's cassettes with its provider map. Where none ships yet, the step runs against the conformance fake and the release notes say so.

The release also requires, as before, `aiops-only` to boot in CI without `aiops-stack` and `dev` to boot every row.

## 6. Amendments

### 6.1 00-decisions.md

| Section | Amendment |
|---|---|
| Status line, §2.4 | Record the three owner decisions of this round. Add working assumption W7: the built-in on-call provider is the default (option (b) of this design), pending the owner's choice between (a) and (b) |
| §1 | Kernel bullet by role. Principle 6 and principle 7 reworded (2.2). New principle 9 (1.2). "Eight rules" becomes nine |
| §2.1 row 3 | Rewritten: **Default.** `on-call-provider-builtin`, a contribution to `oncallProviders`: rotations, overrides, escalation steps, notification through `notifiers` with class `page`. **Alternatives.** Any provider plugin; `on-call-provider-goalert` is the first, optional. **Hosted.** No per-tenant engine. **Dead-man.** As 3.5. Rationale: the owner's request for an on-call module; one engine per tenant does not scale and puts an upstream in the core |
| §2.1 rows 2, 13, 16, 19 | Sibling design |
| §2.1 rows 5, 7, 9 | Reworded by role (2.2); the `gateway` suite is behavioural; the River facts are provider rules |
| §2.3 | New paragraph "Round 5": `on-call-provider-builtin` is built, not trigger-based; carrier notifiers stay out of the first release; the on-call research note's "port the semantics" is not applied: the provider is written new |
| §3.1 | `kernel-api` loses the on-call subdomains; `kernel` and `kernel-worker` reworded |
| §3.2 | `investigator-holmes` becomes `harness-sidecar` and `oncall-goalert` becomes `oncall-engine`, both "optional, with its plugin; in no default profile"; `oncall-db` removed; `kernel-db` loses "the GoAlert database"; the AGPL paragraph points at `upstreams.yaml` |
| §3.3 | `ext-paging-carriers` reworded; `ext-pagerduty` becomes `ext-oncall-saas` |
| §4.1 | `oncallProviders`: first-party row `on-call-provider-builtin`. `notifiers`: the `page` class, `Recipient`, `Direct` |
| §4.3 | New rule "Upstreams": 1.3 in short, the register, the stack-bundle exception |
| §4.4 | Section 5.1 |
| §4.5 | T0 gains U1, U2, U3, U6, U8; T3 gains U4, U5; the release check gains U7; T4 says upstream cassettes live with the plugin |
| §5.2 | New schema `on_call_provider_builtin` (responder, contact method, rotation, override, policy, step, page state); `paging` reworded |
| §5.3 | Schedules and escalation by role |
| §5.4 | Three GoAlert rows and the Twilio subaccount row become one class "External on-call provider credentials". New row "Page ack token: held in the page message; lives until the page closes; revoked by ack or resolve; bound: ack of one page". The SMTP row loses "each GoAlert" |
| §5.5 | On-call limits in `TenantLimits` |
| §5.6 | Create and delete sagas name no upstream (2.2) |
| §5.7 | The dead-man bullet as 3.5: the platform-health receiver is a fixed peer of the Alertmanager egress policy; the tenant engine ingress rule is removed |
| §6.1 | Required-set sentence of U6. The run-time paragraph: without `pii`, pages of the built-in provider wait, and the platform-health route fires |
| §6.2 | Connector scenario: the investigator half runs the default harness |
| §6.3 | Section 5.2 |
| §6.4 | Cohort item 1 by role |
| §7.1 | Section 5.4 |
| §7.2 | S5 defines `page`, `Recipient` and `Direct`; S8 builds `on-call-provider-builtin` and leg E; S9b loses the per-tenant engines; the standing target is no always-on pod per tenant by default |
| §7.3 | Remove the row for `on-call-provider-builtin` and the `Schedules` and `Overrides` methods. Reword the provider row: "`on-call-provider-<service>`: a first customer request, or a contribution". New rows: time-of-week restrictions and layers (the first tenant that needs follow-the-sun); a first-party carrier notifier for SMS or voice, and ack by reply (the first design partner that cannot use email, chat or a push webhook); a schedule editor (with `aiops-app`); a per-tenant external engine run by us in hosted (a hosted tenant asks; own ADR); a second real `jobs` provider (the River verdict changes, or a feature we use moves to a paid edition); a second `gateway` provider or dashboard server (a contributor proposes one) |
| §8.1 | The container rule gains "an upstream's name never appears in a container id, a compose service or a chart key". Naming examples use a first-party plugin. The plugin-id rule gains: a first-party plugin that wraps an upstream follows `<contribution noun>-<system>` and the lint of community §6.7 |
| §8.2 | New terms: upstream, upstream plugin, upstream register, stack bundle, `minimal`. "Dead-man switch" redefined as 3.5 |
| §9 | ADR-008 withdrawn. ADR-021 rewritten (3.5). New ADR, number assigned in the decide step: the upstream rule, the register schema, checks U1 to U8, the exception list. New ADR: the built-in on-call provider (the model and arithmetic of 3.4, the `page` class, the ack token, the limits, the fence). ADR-020 gains O5 |

### 6.2 Plugin record

| Section | Amendment |
|---|---|
| §5 catalogue | `oncallProviders`: first-party rows "`paging` plus `on-call-provider-builtin`"; consumers unchanged. `notifiers`: consumers gain `on-call-provider-builtin`; the contract line gains the `page` class. `kv`, `blob`: first-party rows gain `blob-postgres` if adopted. `sidecars`: consumers become "optional upstream plugins, `remote`" |
| §5 "Why each provider-bound key is bound" | One sentence: a bound key with one allowed provider that wraps an upstream (`jobs`) is the case the base provider allow-list names |
| §6 row inventory | The on-call line becomes "`paging`, `on-call-provider-builtin`; base, oncall"; `tool-oncall-who` in base; `chat` in base; `annotator` and the dashboard content in `dashboards`; the count sentence is recomputed |
| §7, §13 | The failure matrix gains the cell "built-in provider, `notifications` or `pii` not ACTIVE: pages wait; the platform-health route fires" |
| §14 | The contributor's check gains nothing; the repository's CI gains U1 to U8 |
| §16 | S5 and S8 as in 6.1 |
| §17 | A round-5 entry that lists these amendments |

### 6.3 Community record

| Section | Amendment |
|---|---|
| §1 | A row for the round-5 owner decisions, applied in this record's §3, §6 |
| §2.2, §2.3 | S8 line: the built-in on-call provider and leg E |
| §3.1 | The operator sentence of 5.3 |
| §3.3 | Leg A by role; leg B's assertion; new leg E |
| §6.1 | A capability row "On-call: `oncallProviders`; `on-call-provider-builtin`; `aiops-oncall`; neutral; a contributor supplies an `oncallProviders` contribution" |
| §6.3 D2 | Toolset admission is a rule of the harness plugin that owns native toolsets, not of the core (sibling design) |
| §6.4 | The investigator half of the connector scenario runs the default harness |
| §6.6 | Add: what stays specific to a default in this edition also includes the dead-man sender (already in 00) and nothing of on-call or the agent layer |
| §6.7 | R1 to R6 apply to first-party plugins that name an upstream; R7 (`renamedTo`) applies to them too |
| §7.3 | "On-call providers" stays in the portable list; add "page channels, as `notifiers` contributions" |
| §9 | New owner item: the choice between (a) and (b). New items O1 to O6 of 7.1 |

## 7. Open items

### 7.1 Decisions and checks

| # | Item | Who | By |
|---|---|---|---|
| O1 | On-call: (a) providers only, or (b) the built-in provider as default. This design recommends (b) and writes the amendments for it | Owner | Before S5 ends, because the `notifiers` types depend on it |
| O2 | Whether the GoAlert plugin is built for the first release (as the second real `oncallProviders` contribution and the E3 replay) or at S9a | Chief architect | S8 start |
| O3 | `blob-postgres`: adopt, or keep an S3 endpoint as a need of every composition and say so in `aiops-only` | Chief architect | S3 |
| O4 | Split `aiops-dashboards` out of `aiops-stack` before the first release (a later split changes users' profile layers) | Chief architect | S7 |
| O5 | Where signature verification runs and what it links (ADR-020), given that `boot/` may import no upstream module | ADR-020 | S2a |
| O6 | `minimal` as a shipped profile | Owner | On request |
| O7 | The ids this file borrows from the sibling design (`harness-sidecar`, `aiops-harness-holmes`, the default harness row, the neutral read path that replaces `toolsetBindings` for the default harness, the home of `tools-mcp-facade`) | Decide step | This round |
| O8 | The 30-second reconcile poll and the pinned tenant row stay in the plan only for external providers; whether `paging` keeps a poll at all when the only provider is local | 06 | S8 start |
| O9 | UNVERIFIED here: that the Go standard library's zone handling covers every handoff case the property test will name; that an Alertmanager email or webhook receiver is enough as a platform-health receiver for the operators we target; the effort figures of 3.7 | ADR for the built-in provider | S8 start |
| O10 | Migration of schedules between providers is not designed. Moving from the built-in provider to an external one, or back, is manual in this edition | | On request |

### 7.2 Questions for a lawyer

| # | Question | Why the design needs the answer |
|---|---|---|
| LQ1 | May an upstream's name sit in a first-party plugin id, row id, bundle name, chart value key or image key (`on-call-provider-goalert`), for each mark owner the register lists, and does the answer change when a third party sells a plugin with such an id or lists it in a paid marketplace? | 1.3 makes the wrapping row carry the name; `renamedTo` is the fallback |
| LQ2 | Does a chart or compose file in an Apache-2.0 release that references an upstream's unmodified image by digest count as distributing that image? Does mirroring it in our registry? What must the release then carry (notices, source offer) for each licence class in the register? | U8 puts every image behind one value; the answer sets which defaults may be on |
| LQ3 | Is an adapter that holds an upstream's API schema, GraphQL query strings, proto files or configuration templates copying that upstream's content, and under which notice? | The GoAlert adapter and every renderer in the stack bundle hold such text |
| LQ4 | May recorded responses of an upstream's API (cassettes, fixtures) be published in our repository under Apache-2.0? | T4 and leg E3 depend on them |
| LQ5 | A closed-source enterprise plugin compiled into the same binary as a plugin that links a library under a file-level or weak copyleft licence: does the import boundary (the closed plugin never imports that plugin or that library) keep the closed plugin outside the licence's reach? | Use 3 of 1.7; the answer decides whether such a library may stay in the release table of an enterprise distribution or must sit behind a remote plugin |
| LQ6 | In the hosted service, is configuration we render for an unmodified network-copyleft service, or content we provision into it, a modification of that service? | Use 2; principle 7 rests on "unmodified" |

---

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
