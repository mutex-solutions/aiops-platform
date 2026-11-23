# Community-first record, 2026-11: order of work, marketplace readiness, vendor-neutral connectors

Status: decided by the chief architect on 2026-11-11 from the owner decisions of that date (section 1), two round-4 designs (`docs/design/community-r4/sequencing-marketplace.md` and `vendor-neutral-connectors.md`) and three reviews of them (contributor journey, dsh fidelity and compatibility, scope; 47 findings, dispositions in section 8). Applied the same day to `docs/architecture/00-decisions.md` and, minimally, to the plugin record. 00 wins where it and this record differ; this record wins over the two designs. Conventions: `00 §n` is `docs/architecture/00-decisions.md`; `record §n` is `docs/design/plugin-system-2026-11.md`; `04` to `09` are files of `docs/architecture/`. dsh is DeepSeek Harness. CR1 is the first community release. Nothing here is measured and no step has a date. Outside facts are listed in section 10 with URL, version and date; what was not checked is marked UNVERIFIED.

Amended 2026-11-11 a second time by the harness record `docs/design/harness-and-upstreams-2026-11.md` (cited as "harness record §n") and the licence record `docs/design/licence-audit-2026-11.md`, from the owner decisions of the second round (row 9 of section 1). The order of work, the versioning rules and the pack artifact form are unchanged. Edited and marked: section 1 (row 9), 2.4, 3.1, 3.3, 4.1, 6.1 to 6.5, 6.7, 7.3 and 9. Where an unmarked passage still names the investigator, `toolsetBindings`, a toolset binding, the investigator half of the connector scenario, a Slack thread or a page by GoAlert, 00 and the harness record win.

Amended 2026-11-11 a third time: the owner confirmed W2 to W13 of 00 §2.4 in words on that date. They are owner decisions from that date and no longer working assumptions, and O1 of section 9 is answered: the remote runtime and the Python SDK (S2b) ship before CR1. No design changed. Edited and marked: section 1 (row 4 and the W5 and W6 paragraph), 2.3, 2.4, 8 (N1) and 9 (O1, O3, O15).

## 1. Owner decisions of 2026-11-11

| # | Decision (the owner's words, restated) | Applied in |
|---|---|---|
| 1 | The end goal is a plugin marketplace where users contribute and sell plugins to each other and the project earns from it. That is why the design follows dsh. Third parties may run their own marketplaces | 4, 5 |
| 2 | The marketplace with billing is a later enterprise feature. It is not built now | 4.7 |
| 3 | Phase 1 is the community edition: something people can run, users and feedback first. There is no schedule pressure; dates are planning targets | 2, 3 |
| 4 | Keep the dsh plugin architecture and change the order of work: a usable community release early (kernel, loader, enough infrastructure for a single-tenant self-host, then agent, incident loop, Slack, on-call); what only hosted or the marketplace needs comes after (multi-tenant trees, canary generations, per-tenant remote plugin pods, the full plugin manager). This replaces working assumption W1 of 00 §2.4. W2, W3 and W4 stayed working assumptions at that point and are owner decisions since the confirmation of 2026-11-11 (00 §2.4) | 2 |
| 5 | Do now what would be a migration later: a version per service definition with a support window for old majors; a versioned remote protocol with a handshake; manifest fields for publisher and licence; the compatibility gate and the conformance attestation. Defer to enterprise: billing, entitlement for paid plugins, publisher verification, review, the marketplace UI, WebAssembly, third-party plugins on hosted | 4 |
| 6 | Community priority: writing a plugin is easy (SDK, solo test kit, a template plugin, one check command before a pull request). The plugin index is a signed public repository | 4.5, 7 |
| 7 | This edition ships first-party connectors and plugins for the Grafana ecosystem, which stays first and is the default. A contributor must be able to write plugins for another observability vendor or stack and contribute them back. Nothing may hard-wire the default stack where a definition should be | 6, 7 |
| 8 | The boundary between community and enterprise is open. It is recorded as a proposal, not decided | 5 |
| 9 | Second round, amended 2026-11-11. (A) The agent layer is built new; HolmesGPT is one agent-harness plugin a user may choose for the incident, compliance or FinOps agent. (B) GoAlert is one on-call provider plugin. (C) Every licence of the plugin bases is reviewed, and whatever conflicts is rebuilt | 3, 6; harness record; licence record |

Two readings the owner had not stated in words were first recorded as working assumptions in 00 §2.4. The owner confirmed both on 2026-11-11, and they are owner decisions W5 and W6 (00 §2.4):

- **W5.** The remote runtime in its single-tenant form and the Python SDK (step S2b) stay before CR1, as 00 §2.1 row 1 already decided. Decision 4 defers per-tenant remote plugin pods, not the runtime, and decision 5 puts the versioned protocol on the do-now list. The sequencing design proposed S2b after CR1; that is not applied (section 8, N1), and O1 is answered: before CR1.
- **W6.** `aiops-only` is a supported profile of CR1 for the connector scenario (section 6.4), and one reference pack for a non-default store is kept green in CI. Decision 7 has no release test without them.

## 2. The community-first order of work

### 2.1 The rule

**The contract a plugin author sees is complete at CR1; the machinery an operator of many tenants or many replicas needs arrives later behind the same definitions.** The author contract is record §4 (one `plugin.Define(Spec)`, needs and provisions as data, effects, hooks, the tenant handle, `scope`), the definitions with their fakes and suites, the manifest, both runtimes and the builder. The operator machinery is whatever sits behind a definition as a provider row: how many tenants `tenant-trees` mounts, who commits a generation, where a remote instance runs. A later part arrives as a provider row, a new row, a new layer or a wider test, never as a change to an interface a plugin compiles against. Nothing is cut from the kernel, the policy spine or the tests that prove a plugin tenant-correct.

"Before or after CR1" is an order of work. "Community or enterprise" is an edition (section 5). A part that comes after CR1 is not thereby enterprise.

### 2.2 Verdict per step of record §16

| Step | Before CR1 | After CR1 | Who needs the later part |
|---|---|---|---|
| S0 Spike and kernel | Whole. The 500-tree and cold-mount measurements stay as a kernel-only bench, because a failed threshold after community plugins exist would be a migration | Nothing | |
| S1 Loader and launcher | Whole except layer 5. The row format keeps all ten fields; every server row states `roles` | Layer 5 (tenant overlay), golden dumps per role, the tenant-tree column of the matrix | Hosted |
| S2a Authoring path | Toy registries, `plugin.Define`, the manifest generator, manifest schema 1.0, definition versions, the builder, the gate, `plugin new` to `plugin verify`, `trust.yaml`, `plugins.lock`, the attestation, the artifact form with no code (data pack), the Go template, the frozen handshake message | Nothing | |
| S2b Remote runtime, single-tenant form (W5) | `sdk/remote`, `sdk/remotehost`, the `remote` proxy row, protocol major 0, the Python SDK `aiops-sdk` and its template; one shared instance per plugin | Per-tenant instances and activations across replicas (S4b) | Hosted (W4) |
| S3 Infrastructure definitions | Whole, on one process. `Drained()` stays in the durable-class definitions | Nothing; `events-nats` and `kek-kms` keep their triggers | |
| S4 Reload and tenancy | **S4a:** `tenant-directory` with the tenant `default`, `tenant-handle`, `tenantMint`, `authz`, `tenants.Table`, `tenant-trees` mounting one pinned tree per tenant of the directory (one at CR1, two in the test-only profile), the seven bound views, `sidecars-static`, `tenant-lifecycle` with the create saga and the hooks `tenant/create` and `tenant/purge`, `admin-api` for bindings and platform users. `reload` composes, judges and commits or restores a generation as the single committer, holding one PostgreSQL advisory lock; a second process that cannot take it fails readiness and names `composer` | **S4b:** `composer` with canary and promotion, the generation object, `reload-generation`, trees on first use and the cold lookup, layer 5, tenant create, suspend and delete as operator actions, the 500-tenant exit | Hosted; a self-host with several replicas |
| S5 Policy spine and first dispatcher | Whole, including the remote notifier forms. Adds the `Notification` and `Capabilities` types with their proto and the target binding (which row config names a notifier for incidents, approvals and digests). `test/policy-bypass/` runs on two seeded tenants and keeps attempt 21, the two-tenant hook audience test and the disabled-for-tenant test | Bypass attempts that attack an idle tree (S4b) | |
| S6 Manager and index | **S6a:** `plugin-inventory`; `plugin-manager` behind `pluginManager` in its file-backed form (list, inspect, install, enable, disable, remove, purge; audited; the bound consumer of `reload` and `durablePurge`; reached through `admin-api`); `plugin-index`; the index repository with format 1.0 and its pull-request checks; `contrib/` and `aiops-contrib` | **S6b:** `update --cohort`, `quarantine`, exemptions by version, `profile rollback --scope`, the manager's platform tables, the 03:00 drill; added as methods, so the binding maps do not change | Hosted and the marketplace |
| S7, S8a, S8b Product | Whole: 00 §7.2 and the 23 method rows of 08 §2.2. The 300 req/s token figure is measured and recorded | The 300 req/s pass threshold becomes a gate of S9b | Hosted (50,000 agents) |
| S9, S10 | Nothing | **S9a** community second wave; **S9b** hosted runtime and operations; S10 as 00 §7.2 | S9a self-hosters; S9b hosted |

### 2.3 The order, with exits

Order: S0, S1, S2a, S2b, S3, S4a, S5, then S6a (may run beside S3 to S8b), S7, S8a, S8b = CR1; then two tracks, S9a beside S4b, S6b, S9b; then S10. S2b stays before S8b (owner decision W5, 00 §2.4); the option of moving it after S8b is closed and recorded as rejected in section 9, O1.

| Phase | Step | Exit test |
|---|---|---|
| P0 Model core | S0 | As record §16 |
| | S1 | The failure matrix (process-scope columns) and the recomposition suite pass on the built binary; golden dumps |
| | S2a | A repository with no kernel checkout creates an in-process echoer from the template, passes `plugin check`, packs and signs it. A clean machine builds and signs a distribution that holds it, installs a pack with no code and no rebuild, and calls both. The gate denies a copy that needs a definition version the release does not serve and a copy whose attestation names another digest |
| | S2b | The same repository publishes a remote Go echoer and a remote Python stamper; a clean machine installs them from signed artifacts with no rebuild and calls each. A plugin that offers only an unknown protocol major is refused at the handshake, with the reason logged on both sides |
| | S3 | As record §16, on one process |
| | S4a | The create saga makes `default` at first boot and a toy listener on `tenant/create` runs once. A `scope: tenant` toy plugin mounts with bound views only, in two trees on the test-only profile. A transaction with no handle sees zero rows. A failed generation is restored with `composition/applied` in the audit chain. A second process fails readiness |
| | S5 | `test/policy-bypass/` passes with a real egress dispatcher and two seeded tenants |
| P1 Community release | S6a | `aiops-contrib` is built by the builder from `dist/contrib.yaml`, boots `self-host-full` with every contrib row disabled and passes the T3 of `aiops`; the index document verifies before it is parsed |
| | S7 | As 00 §7.2; the load figure recorded |
| | S8a, S8b | **CR1**: the acceptance test of section 3.3 |
| P2, two tracks | S9a | Re-baselined at the CR1 review |
| | S4b, S6b, S9b | The S4 and S6 exits of record §16, then the P2 exit of 00 §7.2 |
| P3 | S10 | As 00 §7.2 |
| Enterprise | not planned | Not set (section 4.7) |

Standing rules, from S2a: no definition merges without its fake, its suite, its `definition.yaml`, a provider and a consumer; no plugin merges without `aiopsctl plugin check` and a T3 test; nothing outside the closed vocabularies of 00 §4.3 merges into `sdk/kernel`, `boot/` or `loader/`; whoever changes a `v0` definition fixes every plugin under `plugins/` and `contrib/` in the same pull request, as far as replay can prove (section 7.4 rule 3).

### 2.4 What the re-cut removes from and adds to the path to CR1

| Change to the path | Source |
|---|---|
| Removed: `composer`, canary generations, the generation object, trees on first use, layer 5, cohort update, quarantine, three hosted-sized exits (S4, S6, the S7 load threshold) | Owner decision 4 |
| Added: `definition.yaml`, `requires`, `serves`, the support-window rule, manifest schema 1.0, the frozen handshake | Owner decision 5 |
| Added: the template per registry, kept true by a CI job; one check command; the index pull-request checks | Owner decision 6 |
| Added: the `signals` fields, the data-pack artifact form, the connector scenario, the `aiops-only` scenario run. Amended 2026-11-11: `toolsetBindings` is withdrawn; the path to CR1 instead gains the harness definition, the first-party harness, four read tools, `tool-mcp`, the built-in on-call provider and an evaluation leg (harness record §10 totals them) | Owner decisions 7 and 9 |
| Added: one reference pack and one sample sender kept green in CI (W6) | Owner decision W6 (00 §2.4); the system the pack targets is O3 |
| Not added: SDK preview tags, a release-candidate job that runs third-party suites, any entitlement or edition machinery | Sections 8 and 4.7 |

## 3. The first community release

### 3.1 What ships

| Item | CR1 |
|---|---|
| Version | `0.1.0`. A `0.y` platform version says that no definition is stable yet |
| Profiles | `self-host-full` on compose or Helm with the tenant `default` and one `aiops --role all` process; `dev`; `cli`; `agent-host`; `agent-k8s`; `aiops-only` for the connector scenario (W6). `hosted` is absent |
| Artifacts | The images `aiops` and `aiops-contrib`, the builder image, `aiopsctl`, the Go SDK modules, `aiops-sdk` for Python (W5), the release `dist.yaml` with its `serves` table, the charts and compose files |
| Bundles | 00 §4.4 at P1 scope. `aiops-base` has no `composer` row (S4b) |
| Required set | The keys of 00 §6.1 and the rows `loader`, `restart-policy`, `reload`, `readiness`. A profile that composes `composer` requires it; the profiles of CR1 do not compose it |

An operator can install the agent on a VM and a cluster, see four signals, receive alerts, get one incident per entity and one page to the on-call person of a rotation (amended 2026-11-11: with no container beyond the kernel, its database and the default stack; a chat thread needs the optional Slack bundle), read an RCA from the default harness, approve and apply one of five Kubernetes actions, ask for the compliance score, export the audit chain, and run it all on a local open-weight model. An operator can enable a row of `aiops-contrib`, install a data pack or a remote plugin with no rebuild, or build and sign a distribution that adds an in-process plugin, without changing the core source. Nobody can yet run several kernel replicas, create a second tenant through an API, or run the hosted profile.

### 3.2 Guarantees that hold at CR1, so nothing written for it is rewritten

| # | Guarantee | Proven by | Later part that attaches unchanged |
|---|---|---|---|
| G1 | The kernel is the complete port, with no product word | Differential and property tests, vocabulary and line-budget checks | Tenant trees, pillar groups, remote proxies |
| G2 | The loader composes layers 0 to 4 and 6 with the full row format; `reload` admits, applies, judges and restores | Failure matrix, recomposition suite | Layer 5; `composer` as a different committer of the same verdict |
| G3 | One author shape; `Apply` may run again in the same process | Plugin vet, the generated cycle test | The same plugin value in every profile |
| G4 | Every service another plugin calls has a definition, a fake and a suite | Import analyzer, `conformance.Run` | Provider swaps are rows |
| G5 | Plugins are tenant-correct although one tenant is deployed: the handle in the context, forced row-level security, per-plugin roles, `tenants.Table`, bound views for `scope: tenant` | The generated two-tenant test, two trees in the test-only profile, the tenant-fence attempts on two seeded tenants | Many trees, the cold lookup and overlays change `tenant-trees`, not its callers |
| G6 | Policy is bound and fails closed; both binding maps are in the signed distribution manifest and do not change at S6b | `test/policy-bypass/` | Hosted adds required keys |
| G7 | Every `contrib` definition passes its suite through its remote adapter | The suite, in every runtime (W5) | Per-tenant instances |
| G8 | Versions are data from the first plugin: `definition.yaml`, `requires`, `serves`, manifest schema 1.0, the attestation | Manifest parity; leg D | The support window and the index read fields that exist |
| G9 | In-process third-party code enters only through the builder and a signed distribution manifest | S2a and S3 exits; leg C2 | An enterprise edition is one more distribution |
| G10 | The solo test kit is complete: `kerneltest`, fakes, generated tests, `plugin check` with no kernel checkout | S2a exit; the template job | |
| G11 | Durable state is declared, migrated expand-then-contract and purged by plugin id | Residue and purge tests of S3 | The tenant delete saga |
| G12 | Every server row states its `roles`, though CR1 runs `--role all` | Bundle lint | The role split is a deployment change |
| G13 | Every state change is an outbox event and an audit append in the caller's transaction | The `events` and `audit` suites | A meter subscribes to state-change events |

### 3.3 The acceptance demo

| Leg | What must pass | Where |
|---|---|---|
| A. Operator | The six-step demo of 00 §7.1 with its host leg and the recorded local-model run. Amended 2026-11-11: the scripts name roles and take a provider map; chat steps run through the API, and the `dev` no-page form is gone | `self-host-full`; CI replay and `lab/` live |
| B. Own stores | The connector scenario of section 6.4 with the default pack, from fixtures recorded against a Prometheus-compatible store, an Alertmanager and a Loki-compatible store that we do not deploy | `aiops-only`; CI replay |
| C1. Data pack | With `aiopsctl` only: `plugin new connector` creates the reference pack for a logs store outside the default stack; `plugin check` passes; `plugin pack` and `plugin sign`; an operator runs `trust add`, `plugin install` (no rebuild) and `plugin enable`; the script of leg B passes with the pack's fixtures | A scratch repository; `aiops-only` |
| C2. Code | With the SDK, `aiopsctl` and a Go toolchain, no kernel checkout: `plugin new signals --lang go` creates a contribution for a sample sender family that retries only 5xx; `plugin check` passes T0 to T2 with the generated cycle and two-tenant tests; it is packed and signed; an operator admits it through `dist build`, `dist sign`, `dist use`, enables its row, and one alert of the scenario arrives through it. The same code under the `contrib` id is green as a pull request under `contrib/` and is in the next `aiops-contrib` image with its row disabled | A scratch repository; CI |
| D. Index and gate | The index lists the reference pack; `plugin search` verifies the detached signature before parsing. Four refusals: a plugin that needs a definition version the release does not serve; a missing or foreign attestation; a manifest with an unknown major; a pack whose bundle changes an existing row or inserts a row of a plugin that is not instantiable | CI |
| E. Evaluation (added 2026-11-11) | The smoke baseline on the hand-written case set, both models; every case-run inside its caps (00 §7.1; harness record §9) | `lab/`; a maintainer run |
| F. Upstream independence (added 2026-11-11) | The leave-one-out render of every shipped profile; legs A and B with a provider map that names no optional upstream plugin; a replay of each optional upstream plugin from its own cassettes | CI |

## 4. Marketplace readiness

### 4.1 Versioning per definition (ADR-040, decided)

- **What carries a version.** A definition has `major.minor` in `sdk/def/<name>/v<N>/definition.yaml` (key, version, stability, class, remote mark, `compatibleFrom`, deprecation fields). The major stays in the import path, the key and the proto package. `engines.aiops` is removed: a single range would make every publisher release again at every SDK major.
- **Generated, never hand-written.** A manifest carries `requires` (`pluginApi` for in-process code, `remoteProtocol` majors for remote code, `definitions` as `name: ["major.minor"]`, one per major). A pack with no code has no `pluginApi`; `plugin check --write` derives its `requires.definitions` from the registries its rows feed. A signed distribution manifest carries `serves` with the same shape. The Go module versions are packaging numbers that no gate reads.
- **Major and minor.** A major is any change after which a plugin that passed the suite could fail it, fail to compile or be refused at the wire. A minor is additive: for a consumer a new method; for a provider or contribution a new optional capability, never a new required method; on the wire new optional fields. A suite revision inside a minor adds tests for new optional capabilities only.
- **Compatibility.** On a stable major, a plugin attested on `1.3` runs where `1.y` is served with `y >= 3`. At major 0 every minor may break: the gate, the attestation and a remote registration need the exact minor, unless `definition.yaml` carries `compatibleFrom: 0.x`, in which case `compatibleFrom <= needed <= served`. A plugin built for `0.1.0` is therefore rebuilt or re-attested at every release while its definitions are `v0`, and the index reports it `incompatible` until then.
- **Support window.** When `v<N+1>` of a stable definition ships, the release keeps serving `v<N>` through an adapter row, proven in CI by the old suite, for at least 12 months and two further minor releases, and removes it only in a platform major release. At most two majors of a definition are in service: a new major does not ship while the previous-but-one is inside its window. The frozen package stays in `sdk/def`, so no import path changes at removal. The numbers are initial values (O9).
- **What the window covers.** Consumers and contributions. A third-party provider of an open singular key (`annotations`, a contributed `gateway`) must move with the major; its row carries a deprecation warning from the release that ships `v<N+1>`.
- **`v0`.** No window. A break is in the release notes with a migration note.
- **Promotion to `v1`**, a release review decision, when: one release has shipped with the definition; a `contrib` definition has passed its suite through the remote adapter; and, for the four vendor-neutral read seams (`signals`, `tools` since the amendment of 2026-11-11, `evidenceSources`, `estateSources`), a second contribution or pack for a different backend family has passed. For a data-only registry a second family is a pack whose connector scenario passes on another dialect. The review may waive the third condition with a recorded reason. Dialect contracts (`sql`, `metricsQuery`, `logsQuery`) and stack contracts (`gateway`, `fleetTemplates`, `limitsRenderers`, `annotations`) are exempt from it.
- **Promotion is not a break.** `v1.0` is contract-identical to the last `0.y`; nothing may ride with promotion. The release serves that `0.y` beside `1.0` through an identity adapter for the normal window. The import path is the only source change.
- **Early withdrawal** only for a vulnerability that cannot be fixed inside the old contract, by ADR and advisory.

### 4.2 What is stable when

| Versioned thing | At CR1 | At platform 1.0 |
|---|---|---|
| Definitions | Almost all `0.y`; the mechanism exists | Promoted one by one (4.1) |
| Manifest schema | `1.0`, frozen-additive from the release that ships it: a reader ignores unknown fields of a higher minor; `plugin check` is strict | Unchanged |
| Index format | `1.0`, frozen-additive | Unchanged |
| Handshake message | Frozen | Unchanged |
| Author API (`pluginApi`) | `0.y`; may break at a minor release with a migration note. Two kernels cannot share a tree, so a later major gets a notice period, not a served predecessor | `1.0` declared at a release review |
| Remote protocol | Major 0 (`aiops.remote.v0`), exact match | Major 1; the current and the previous major served under the window |
| Attestation | A typed statement; the type URI carries its version | Unchanged |
| Artifact layout | Frozen-additive from its first publication, after L14 (ADR-047) | Unchanged |

### 4.3 Remote protocol version and handshake (input to ADR-041)

1. The major is the proto package, `aiops.remote.v<N>`. Inside a major, changes are additive and both sides ignore unknown fields, calls and feature names.
2. One frozen handshake message, in its own package `aiops.remote.handshake`, opens every connection: the protocol majors the sender speaks, its kind (`kernel` or `plugin`) and a string map for diagnostics. The kernel dials, both sides send their lists, the kernel picks the highest common major. With none, both sides log the two lists and the kernel closes the stream. This is gate check 3.
3. The first message of the chosen major carries the plugin id, version and manifest digest (compared with `plugins.lock`), the activation id, the definitions the plugin registers with as `name@major.minor`, and feature names. The tenant and the row come from the connection certificate, never from a field. One optional field is reserved for a record the kernel passes to the plugin, so that an enterprise plugin can later verify an entitlement offline; nothing reads it now.
4. A dispatcher accepts a registration when it serves that major and the minor rule of 4.1 holds.

The callback set, deadlines for remote gate listeners, the reconnect budget and the instance identity on a single-tenant self-host, where no cell certificate authority exists, stay with ADR-041: provisional at S2b, ratified at S5 (policy callbacks) and S4b (one tenant per connection, activations across replicas).

### 4.4 Manifest schema 1.0

| Hand-written field | Rule checked by `plugin check`, the gate and the index checks | At CR1 |
|---|---|---|
| `manifestVersion` | Required; a known major, else gate check 1 denies | `"1.0"` |
| `id`, `version`, `title`, `description` | `id` is `<owner>/<name>` (00 §8.1); `description` is optional | |
| `publisher {id, name, url}` | `id` is required and equals the owner segment of `id`. `aiops` and `contrib` are reserved to artifacts built by the release pipeline: the builder refuses another module that declares them, and `trust.yaml` limits each signer to id patterns. `name` and `url` are optional | Filled for `id`; the rest may be empty |
| `license` | Required SPDX expression; SPDX list ids only in `aiops`, `aiops-contrib` and the project index | |
| `source`, `support {url, email}` | Optional; the project index requires both | |
| `stability`, `scope` | As record §4 | |
| `runtimes` | A list of `inproc`, `remote`, `wasm`. An empty list is valid and means a pack with no code. `wasm` validates and the gate denies it | |
| `capabilities {network, secrets}` | A network entry is a host or `config:` and a JSON Pointer into the config, such as `config:/endpoint`, for an endpoint the site sets | |
| `integrates [{system, versions, tested, credential}]` | Optional. The external system, the versions the fixtures were recorded against, and whether the read credential is `read-only` or `broader` | |
| `instantiable` | `true` only on a first-party generic plugin whose rows a pack may insert; such a plugin registers contributions and nothing else | Three plugins (6.4) |

Generated fields are those of record §4 plus `requires`. Not in schema 1.0: an edition, an entitlement reference, a price, a publisher verification mark, a custom licence reference field. The enterprise phase introduces what it needs with a manifest major, so a community kernel refuses such a plugin by gate check 1, and the gate stays at ten checks. On self-host any gate check is an honest refusal, not a lock, because the operator may build and sign a distribution; what binds there is access to the artifact registry and a check inside the plugin.

Gate checks reworded: 1 reads `manifestVersion` and `requires.pluginApi` and denies an absent `requires`; 2 compares `requires.definitions` with `serves`; 3 is the handshake; 8 gains the pack rule of 6.4; 10 reads a typed attestation that names, per definition, the version and suite revision that passed, and for a pack the scenario version and which halves ran (amended 2026-11-23, ADR-060 decision 56: the suite revision only of a definition the plugin provides, contributes to or, for a pack, feeds; 08 §1.6 holds the one gate text). Exemptions cover only the minor comparison of checks 1 and 2, and check 4.

### 4.5 The index and re-certification

The project index is a public Git repository, external to the kernel (`ext-plugin-index`): one file per plugin, written by pull request; CI composes one document and a detached signature. `aiopsctl` verifies the signature before parsing. The index is trusted for discovery only: an artifact is still verified by its own signature against `trust.yaml`, locked by digest and admitted by the gate. No index is configured by default.

Format 1.0: `indexVersion`, `name`, `serial` (a client refuses a lower one than it has seen), `expires`; per plugin `id`, `publisher {id, name, keys}`, `renamedTo`; per version the artifact reference by digest, `manifestDigest`, `runtimes`, `license`, `requires`, `integrates`, `attestations`, `yanked`, and `compat [{release, status, suite, scenario, checked}]`. Status values are `suite-passed`, `gate-passed`, `incompatible` and `failed`; the word "certified" is not used (O8). `ponytail:` a serial and an expiry, not a full update framework; adopt one if an index ever becomes a source of trust for install.

| When | What runs | From |
|---|---|---|
| A pull request to the index | Entry schema; the name lint; the artifact signature against the publisher's recorded key; the attestation subject; the gate on manifests against the `serves` of the current and the previous minor release. For a pack with no code, also both halves of the connector scenario, since no third-party code runs | S6a |
| Every release | The gate on manifests for the latest version of every listed plugin against the release's `serves`; `compat` is written; a plugin that used a changed `v0` definition shows `incompatible` until it is re-attested | S6a |
| Every release candidate | Third-party suites in a throwaway runner with no token, no secret and no network beyond a module proxy and the registry; a regression of a `suite-passed` plugin on stable definitions blocks our release | Trigger: the first listed plugin with code outside `contrib/` that uses a stable definition (00 §7.3) |

`contrib/` and the reference pack run their suites on every commit, so nothing listed at CR1 goes unproven.

### 4.6 What a third-party index or marketplace gets

| Need | What we publish | From |
|---|---|---|
| The index format | The format 1.0 schema and `aiopsctl index compose`, `sign` and `verify`, the commands our own CI uses | S6a |
| The artifact and attestation formats | The OCI layout of `plugin pack`, with the signature and the attestation as OCI referrers; the attestation type, an in-toto Statement v1 of predicate type `https://github.com/mutex-solutions/aiops-platform/attestation/conformance/v1`, which `aiopsctl` writes; `cosign attest` writes a Statement v0.1 for a type it does not know (ADR-020) | S2a |
| Compatibility offline | Each release publishes its `dist.yaml` with `serves`; `aiopsctl plugin gate <spec> --against <dist.yaml>` runs the gate with no kernel | S2a |
| A way to prove conformance | The suites inside `aiopsctl`, and `plugin verify` | S2a, S2b |
| Trust roots | `trust.yaml` keeps `signers` (key, id patterns, runtimes, and the `signed` list of the signed keys and hooks it is trusted for), `distributions` and `allowUnsigned`, and gains at S6a `indexes` (name, URL, key, the id patterns it may list) and `requireAttestations` per signer or index (ADR-020). An index cannot list an id outside its patterns, so it cannot shadow `aiops/*` or `contrib/*`; with two indexes listing one id, `install` needs `--index` | S2a, S6a |
| A name to use | Open: the project's mark policy (O8) | |

A third party does not get our signing keys, a place in our index without a pull request, or a code path in the kernel that calls its service. The kernel contacts an index only on an operator's manager command.

### 4.7 Deferred to enterprise, and the seam each attaches to

| Deferred item | Seam that exists at CR1 | Change to community plugins |
|---|---|---|
| Entitlement for paid plugins | `manifestVersion` (a new manifest major carries the fields; an old kernel denies by check 1); the reserved field of the remote first message; `trust.yaml` as the place for issuer keys | None |
| Billing for plugin sales | None in the kernel: a sale is a marketplace record keyed by plugin id and publisher id | None |
| Usage metering | State-change events in the outbox with tenant and row; the metric labels `plugin` and `tenant`. Per-call durable usage records are not a property of community dispatchers; they arrive with a meter row | None |
| Publisher verification | `publisher.id` equal to the id owner; signers limited to id patterns; the index document as the place for a verification statement | None |
| Review and curation | Typed attestations; `requireAttestations` | None |
| Marketplace UI, ratings, featured lists | Index format 1.0; `plugin search` and `plugin inspect` | None |
| WebAssembly | `wasm` in `runtimes`; the proxy-row pattern of `remote` | None |
| Third-party plugins on hosted | Gate check 9; `sidecars`; W4; S4b and S6b | None |
| First-party enterprise plugins | The builder and `dist.yaml`: an enterprise edition is a distribution (O6) | None |
| Takedown of a published version | `yanked` in the index; the lock by digest; `quarantine` at S6b | None |

## 5. Community and enterprise: a proposal, not a decision

**Proposed principle:** everything a single organisation needs to self-host is community; what serves selling to and operating for many organisations is enterprise. Test question: would one platform team, running the product for its own company, miss it?

| Feature | Proposed side | Note |
|---|---|---|
| Kernel, loader, SDK, definitions with fakes and suites, the builder, the gate, signing, `aiopsctl`, the index format and the project index | Community | The contract every plugin and every third-party marketplace builds on |
| Agent, telemetry base, incident loop, chat-ops, on-call, approvals, the ledger, the audit chain and export, the lab | Community | Owner decision 4 |
| OIDC sign-on, permission checks, audit export; the pillars of P2 and P3 | Community | One organisation needs them |
| Several replicas with canary generations (S4b); the remote runtime and the Python SDK; a private index | Community | High availability and contribution |
| Connector packs for self-host, for any vendor | Community | Owner decision 7 |
| More than one tenant on a self-host | Mechanism community; the tenant lifecycle API open (O2) | Isolation code must be in the core to be tested by every plugin |
| The `hosted` profile and the rows of `aiops-hosted` | Enterprise by the principle; open (O2) | 00 ships them in the Apache-2.0 repository today |
| A key-management provider such as `kek-kms` | Community; open (O2) | One organisation with a KMS needs it |
| The marketplace service, the entitlement issuer, plugins we sell, third-party plugins and SaaS bindings on hosted, paid packs, paid support | Enterprise | Owner decisions 1 and 2 |

Open questions that follow, all the owner's: O2 (the three boundary cases), O5 (contributor agreement: a sign-off, proposed, or a licence agreement that keeps a relicensing option; and whether plugins under `contrib/` are promised free in every edition), O6 (where enterprise plugins live, proposed a separate private repository built into an enterprise distribution by the builder; and whether a written promise is published that nothing moves from community to enterprise), O8 (the mark policy). The project index lists open licences only during the community phase.

## 6. Vendor-neutral connectors

### 6.1 Capability inventory

| Capability | Owner (definition, first-party row, bundle) | Neutral | What a contributor for system X supplies |
|---|---|---|---|
| Alert intake | `signals`; `signal-ingest-alert-webhook`, `signal-ingest-generic-json`; `aiops-base` | Yes, after 6.3 | A `signals` contribution with a default label map and its busy reply; nothing if X sends through Alertmanager |
| Metric, log and trace reads by the agent (amended 2026-11-11) | `tools`; the read tools of `aiops-datasources`; rows of `aiops/tool-mcp` in a pack | Yes | A `tool-mcp` row for a system with a Streamable HTTP MCP server: data. Otherwise a `tools` contribution: code |
| On-call (added 2026-11-11) | `oncallProviders`; `on-call-provider-builtin` in `aiops-oncall` | Yes | An `oncallProviders` contribution; a page channel is a `notifiers` contribution |
| Metric read by the kernel | `metricsQuery`; `metrics-query-http`; `aiops-datasources` | A dialect contract | Endpoint config if X speaks the Prometheus query API; otherwise nothing under this key |
| Change evidence | P1: the agent with the logs tool and the pack's host runbook (amended 2026-11-11). P2: `evidenceSources` under `change-hub` | Yes | The runbook text; at P2 an `evidenceSources` contribution |
| Estate source (P2) | `estateSources` under `estate`; `estate-source-series` in `aiops-datasources` | Yes | Nothing if X holds the same exporter series behind the Prometheus API; else a reader |
| Verification | `verifiers`; reads resolved signals, no backend query | Yes | A firing and resolved fixture pair in the `signals` suite |
| Profile read | None in MVP | | Later a `tools` contribution |
| Annotation write | `annotations`; `annotator`; `aiops-stack` | Singular, optional | An in-process provider, optional |
| Rule content | Rule files in `aiops-stack`; the label contract of 00 §2.1 row 17 | The contract yes, the files no | A rule pack in X's format, proven on the alert it emits |
| Dashboards, agent and fleet, gateway, limits, provisioning, the dead-man sender | `aiops-stack`, `agent-base` | No, by design (6.6) | Nothing in this edition |

Before this record the default stack was hard-wired outside a stack-specific row in five places: fixed slots in `toolset-bindings` with the slot-to-toolset map inside `investigator-holmes`; `change-hub` building LogQL at P2; `estate-source-series` listed in `aiops-base`; the host runbook in `aiops-pillars-mvp`; and the 00 §7.3 row that made other vendors wait for a paying customer. All five are fixed below.

### 6.2 The read contract: both, split by layer

- **Layer A, typed read intents, is the contract of every first-party plugin outside a connector pack or a stack bundle.** Such a plugin never holds a query string in any dialect. It calls a registry whose request is typed (`estateSources`, `evidenceSources`, `verifiers`, `signals`), and a vendor implements a contribution with its own client. CI enforces it on the static graph: no plugin of `aiops-base` or of a pillar bundle declares a need on `metricsQuery` or `logsQuery`.
- **Layer B, raw query strings, exists only inside dialect-specific rows.** `metricsQuery` is declared as the Prometheus HTTP query API with PromQL, and `logsQuery` (P2) as the Loki HTTP query API with LogQL, the way `sql` is a PostgreSQL dialect contract. A backend that speaks the dialect is reached by endpoint config. One that does not provides nothing under the key, and the rows that need it stay PENDING. There is no dialect negotiation and no remote adapter for these keys.
- **The product owns its read tools (amended 2026-11-11).** Which tool serves which signal is a `tools` contribution with a `Signal`, a `Dialect` and a `ScopeNote`. The model writes the vendor's dialect; the kernel never parses it. A harness that keeps native toolsets declares the departure `native-reads` (harness record §6.2).

Rejected: raw strings with a dialect parameter (every consumer would branch per dialect); typed intents only (a query language of our own); a registry of query backends per dialect (no shared consumer holds a second dialect).

### 6.3 Changes to definitions

| # | Change | When, and why then |
|---|---|---|
| D1 | Withdrawn 2026-11-11: the read seam is `tools`, and its no-code form is the instantiable `aiops/tool-mcp` (harness record §5.3). The text that follows is kept as the record of round 4. `toolsetBindings` becomes a registry, dispatcher `toolset-bindings` in `aiops-base`. A contribution is `Binding{Name, Signal, Investigator, Toolset, Config, Runbook}` with `Resolve(ctx)`, which returns the endpoint and the read credential for the tenant of the handle. `Signal` is an attribute from `metrics`, `logs`, `traces`, `profiles`, `platform`, not a key: several bindings may share one. The row config is the message `toolsetBindings.BindingSpec`, versioned and suite-tested with the definition | Before CR1: turning a singular provider into a registry later changes every profile layer |
| D2 | Amended 2026-11-11: admission is the allow-list of a `tool-mcp` row, with a digest of the recorded tool list carried by the pack and the recorded text only for a server under an open licence; admission of an external harness's native toolsets is a rule of that harness plugin, not of the core. The round-4 text follows. Admission of a toolset is data. `investigator-holmes` names no backend and keeps only a deny-list in code (`bash`, generic clients such as `datadog/general`, `http`, `internet`, `mcp`). It renders every toolset that has no ACTIVE binding off. A binding carries its proof in the pack: the toolset id, the recorded tool list at the pinned investigator version and a cassette. The suite obligation is "the recorded tool list holds no write tool and no literal credential"; boot prints every bound toolset outside the default pack. The pin has a revision number, recorded in the attestation and in `compat` | Before CR1: the alternative is a vendor table in a pillar row |
| D3 | `signals`: `Signal` gains `State` (`firing`, `resolved`) and `SourceID`; `Parsed` gains `Truncated`; the route entry that `Routes` returns gains `busy` (429 by default, 503 for a sender that retries only 5xx) and `defaultLabelMap`, applied on `signal/normalize` before the tenant map. One registration shape stays. The suite gains the firing and resolved fixture pair | With S8b; the registration shape before CR1, the fields are additive |
| D4 | `metricsQuery` is stated as a dialect contract with a conformance corpus limited to what first-party readers use, remote `no`; the same for `logsQuery` when it ships | Before CR1: it stops a translator provider |
| D5 | Dialect-specific first-party rows and content live in `aiops-datasources`: the four default binding rows, `metrics-query-http`, the host runbook; at P2 `logs-query-http`, `estate-source-series`, `evidence-source-logs` | Before CR1 for the runbook and bindings: a bundle move later changes users' profile layers |
| D6 | `evidenceSources` (P2): `Read(ctx, Scope{Entities, From, To}) -> Run{Lines{At, Entity, Class, Actor, Ref}, Unseen}`, dispatched by `change-hub`, which keeps the mapping, the coverage report and the rule that it stores references. ADR-028 is rewritten around it | With P2 |
| D7 | The context pack footer lists each bound signal with its dialect and scope note, and names the runbook of the enabled logs binding. A tool row may state the pillars it joins (`pillars: [incident]`); the pillar group computes its toolset as its defaults plus the enabled tool rows that name it | With S8a; additive |

Not changed: `annotations`, `limitsRenderers`, `estateSources`, `tools`, `verifiers`, `fleetTemplates`, `gateway`, the kernel, `boot` and `loader`. `signal-ingest-alert-webhook` stays in `aiops-base` (section 8, N4).

### 6.4 The connector pack

A connector pack makes one external observability system usable for one or more capabilities of 6.1. It is one signed artifact: a `plugin.yaml`, a bundle of rows, content, and at most one plugin. It is not a manifest kind or a runtime. It covers capabilities, not a stack, so packs compose for mixed estates. `aiops-datasources` is the default connector pack.

| Size | When | Contains | Code |
|---|---|---|---|
| Data pack | The system speaks an API the platform has a row for: alerts through Alertmanager, the Prometheus query API, an MCP server over Streamable HTTP with read tools (amended 2026-11-11; W9) | Rows of the instantiable first-party plugins (`aiops/tool-mcp`, `aiops/signal-ingest-generic-json`, `aiops/notifier-webhook`), a label map, a rule pack, a host runbook, fixtures. `runtimes: []` | None |
| Read connector | The system has its own alert payload or query API | The above plus one plugin that registers with `signals`, at P2 with `evidenceSources` and `estateSources`, and with `tools` for a read the default pack and `tool-mcp` do not cover | In-process Go or remote in any language (W5) |
| Stack bundle | Agents, a gateway, limits and provisioning for a second self-hosted stack | Section 6.6 | In-process Go. Not planned in this edition |

**Layout.** `plugin.yaml`; `bundle.yaml` (rows, disabled); `content/` (label map, `rules/<pack>/`, `runbooks/host.md`, a webhook template); `fixtures/<system>-<version>/` (firing and resolved payloads, backend responses, the recorded tool list, the cassette); `testenv/` (a compose file for an open-source backend); code only for a read connector. A bundle carried by an artifact has no name of its own: it is addressed by the plugin id.

**Install and trust (gate check 8, before CR1).** Install runs no third-party code and needs no rebuild for a pack with no code. The pack is locked by digest in `plugins.lock`; its content is unpacked under `$AIOPS_HOME/content/<plugin id>/` (a volume on Helm) and its digest is part of the generation digest. The bundle of an installed artifact may only insert rows; every inserted row is disabled; its `plugin` is the artifact's own id or an `instantiable` first-party plugin; it sets no field but `id`, `plugin`, `roles`, `enabled` and `config`, and the bundle holds no `${env:}` or `${file:}` reference (amended 2026-11-23, ADR-047 decision 19 and ADR-060 decision 57; 08 §1.6, check 8). The rows mount under one group named by the pack, so a row id is `<pack name>:<row>`, and two artifacts of one name never compose together: the second install is refused (ADR-047). The loader records the origin artifact of each row: the secret scope and the purge key of an instantiated row use the origin id, so one pack cannot read another pack's credential. A pack never adds a second provider of a singular key such as `metricsQuery`; it ships a profile-layer snippet that repoints the existing row. `plugin inspect` prints every row config and content path. A pack states its credential class in `integrates`; `broader` is refused in the project index and in `contrib/`, and boot warns on any enabled row that declares it (amended 2026-11-23: a row, since W9 withdrew the bindings), so 00 §2.2 takes no exception. The refused ranges of `dialer-guarded` are row config: the cell ranges in hosted, link-local only by default in self-host profiles, where the operator's own stores sit on private addresses.

**Tests.** T0: manifest parity, the name lint (6.7), the content lint. T1 and T2: as for any plugin, plus the obligations of D2 and D3. **The connector scenario** is one vendor-neutral script on `aiops-only` plus the pack, in the no-page form, replayed with `AIOPS_SNAPSHOT=replay`:

- *Kernel half*, no container runtime: a recorded firing alert becomes one incident keyed on the expected entity; the first message lists the bound signals; the recorded resolve yields `cleared`. No annotation is asserted. Amended 2026-11-11: this half and the next are one scenario that runs in `aiopsctl`.
- *Harness half* (amended 2026-11-11), no container runtime: the default harness, with model turns from `llm-provider-replay` and the tool endpoints repointed to a fixture server that replays the recorded responses, makes one successful call on each tool the pack offers.

`aiopsctl plugin check` runs both halves (amended 2026-11-11). Recording needs a model endpoint and a backend the contributor runs, with its MCP server where the pack uses `tool-mcp` (the pack's compose file, or a trial organisation for SaaS); the testkit recorder is a proxy between the kernel's tools and the backend and scrubs credentials. Our CI holds no vendor credential: a pack is tested by replay, and fixtures older than two releases of the external system are flagged in the index.

**Selection.** `aiopsctl plugin install <spec> --profile aiops-only`, then `plugin enable` per row; the default binding rows for the same signals are disabled in the profile layer if they are not wanted. Capability coverage is derived from the composition and printed by `profile render --dump`; there is no coverage descriptor.

### 6.5 Two worked examples

| Capability | VictoriaMetrics with VictoriaLogs | Datadog |
|---|---|---|
| Alert intake | `vmalert` sends to Alertmanager; the existing row receives it. No code | A `signals` contribution that parses a shipped webhook payload template; answered 503 when busy, because the sender retries only an internal error or a 5xx |
| Metric read, agent (amended 2026-11-11) | `tool-metrics-query` over the repointed `metrics-query-http`. Data | Rows of `tool-mcp` if the vendor offers a Streamable HTTP MCP server with read tools, which is UNVERIFIED; otherwise a `tools` contribution of the pack's plugin. Code unless such a server fits (amended 2026-11-23, W9: no binding exists) |
| Metric read, kernel | `metrics-query-http` repointed. MetricsQL differences per corpus query are UNVERIFIED | Not offered through `metricsQuery`, by decision |
| Log read, agent (amended 2026-11-11) | Rows of `tool-mcp` for the store's own MCP server (Apache-2.0, read tools, a Streamable HTTP mode; read 2026-11-11). Data | As for the metric read (amended 2026-11-23, W9) |
| Change evidence, kernel (P2) | One small `evidenceSources` plugin: there is no Loki-compatible read API | An `evidenceSources` contribution; only useful if the customer tags host logs to the label contract |
| Rule content | The Prometheus rule format loads, with stated differences | Monitor definitions rewritten by the contributor |
| Does not map | Tenant ids (a constant in this edition), log patterns, traces, profiles | Annotations, rule reuse, kernel metric reads |
| Result | A data pack at CR1, one small plugin at P2 | A read connector: one `signals` contribution, and `tools` contributions for its reads unless a vendor MCP server fits a `tool-mcp` row (amended 2026-11-23, W9; 07 §8.4) |

At the investigator's pinned version 0.46.0 the repository also ships toolsets for Elasticsearch and OpenSearch, Coralogix and New Relic, none for SigNoz and none for profiles. Whether each non-default toolset is read-only, and which are enabled by default, is UNVERIFIED; D2 makes the proof travel with the pack. Amended 2026-11-23 for W9: since `toolsetBindings` is withdrawn, no toolset of the investigator is a read seam of a pack, so such a system needs a Streamable HTTP MCP server with read tools for a data pack, or else a read connector (07 §8.4).

### 6.6 What stays Grafana-first by design in this edition

| Part | Why it is not portable | What a full second stack would add |
|---|---|---|
| Agent and fleet | The templates emit configuration for one agent and its remote-config API | A stack bundle; writers split from collectors first |
| Tenant gateway | The route table is the paths of four named backends and one tenant header | An in-process provider of `gateway` |
| Limits | Each renderer emits one backend's overrides block | Renderer contributions; the registry is neutral |
| Backend provisioning | Tenant create, purge and rule load on four stores | The contributor's own plugin; no definition, because nobody calls it |
| Provisioned dashboards, `annotator` | Files and the API of one dashboard server | Dashboards as pack content; an `annotations` provider |
| Shipped rule files; the lab and the 00 §7.1 demo | PromQL; they deploy `self-host-full` unchanged | A rule pack; the connector scenario |

For the release notes: this edition reads from other systems through connector packs in `aiops-only`; it collects, stores, isolates and limits only on the default stack. A full second stack bundle is not designed and waits for a contributor who proposes one.

### 6.7 Naming rules for vendor plugins

| # | Rule |
|---|---|
| R1 | A plugin id is `<publisher>/<name>`. The namespace is the publisher's own; `aiops` and `contrib` are reserved; the index ties a namespace to its signer |
| R2 | The name is `<contribution noun>-<system>` or `connector-<system>`, with the lowercase technical name of the system once, after the noun. Never "official", "certified" or a product tier |
| R3 | The `title` reads "<own name> connector for <System>"; the mark comes after "for" |
| R4 | No vendor logo, icon or colour scheme in an artifact or an index entry |
| R5 | The README states that the publisher is not affiliated with the mark owner and carries the owner's attribution sentence where its policy gives one |
| R6 | The deny-list of 00 §8.1 applies to every id, title and content path in the project index and in `aiops-contrib`, whoever the publisher is |
| R7 | The project index renames an entry on a credible objection from a mark owner; `renamedTo` keeps installed sites working |
| R8 | A third-party index sets its own rules; the lint then warns and does not fail |

R1 to R6 are a lint of `plugin check` and of the index checks, not of the runtime gate. R2 rests on reading a plugin id as a technical identifier, which counsel has not reviewed (O12). Amended 2026-11-11: R2 to R7 also apply to a first-party plugin that wraps an upstream (`harness-holmesgpt`, `on-call-provider-goalert`). The system's name is allowed in an id only when its entry in the upstream register is marked `suffix-ok`; an owner whose policy asks for approval keeps a generic id until cleared. No index or marketplace lists a plugin for the dashboard server, and `plugin check` refuses an artifact that contains one (licence record §7.5, §7.6).

## 7. The contributor journey

### 7.1 Where plugins live

| Tier | Source | Id owner | Enters by | Who checks it |
|---|---|---|---|---|
| First-party | `plugins/<name>/` | `aiops` | The release distribution | Maintainers |
| Contrib | `contrib/<name>/` in this repository; a Go module of its own, or no module for a data pack | `contrib` | Code: `aiops-contrib`, from `dist/contrib.yaml`, rows disabled. Data packs: a signed artifact published by release CI and listed in the index | Maintainers review; CI runs its suites on every commit |
| Indexed | The author's repository | The author's publisher id | A distribution the site builds, a remote install, or a pack install | The index checks only |
| Unlisted | Anywhere | Any | An OCI reference the site trusts | The site |

The tier is chosen at scaffold time (`plugin new ... --contrib` writes the `contrib` id). Changing tier later is a rename: the row's `plugin`, the secret scope and the schema name change, and the index records `renamedTo`. `contrib` is the one word for in-repository community plugins. `ponytail:` one repository; move `contrib/` out when its CI time passes that of the core.

### 7.2 The journey, step by step

1. **Fit.** A connector to another system is a pack (6.4). Anything else contributes to an existing registry. A new definition is a core change with its own design note.
2. **Scaffold.** `aiopsctl plugin new connector` (a data pack) or `aiopsctl plugin new <registry> --lang go|python`. The template compiles, passes T0 and the generated tests, and fails conformance with one line per obligation: that list is the work list. CI generates a plugin from the template for every registry and asserts that state.
3. **Write.** Rows and content for a pack; for code, a client on the injected dialer and the methods of the definition. Standard-library HTTP by default; another dependency needs maintainer approval for `contrib/`, must be on the L7 allow-list and must not change a release pin, else the plugin stays in the indexed tier.
4. **Record fixtures** from a backend the contributor runs.
5. **Check.** `aiopsctl plugin check ./`: T0 to T2 in every runtime the manifest lists, manifest parity (`--write` regenerates `requires`), pack-and-verify with a throwaway key, and the connector scenario. It needs the SDK, `aiopsctl` and a toolchain for code; no kernel checkout; PostgreSQL only for a plugin with its own schema. `plugin smoke ./ --against <release>` is the T3 run on the built release.
6. **Publish.** `plugin pack`, `plugin sign`, `plugin push`, then a pull request to the index, or a pull request that adds `contrib/<name>/`.
7. **Review for `contrib/`**, a fixed list: imports and licence; every outbound call on the injected dialer with `capabilities.network` filled; credentials only from `secrets`, never in a log or fixture; no write to the vendor unless the definition is `executors`; fixtures hold no customer data and tests pass with the network off; the README states vendor-side permissions and recorded versions; the name rules of 6.7; a named code owner; `stability: experimental`. For a binding, the recorded tool list is read by a maintainer.
8. **Use.** An operator installs and enables. The template README shows the row snippet, including the target binding for a notifier.

No review time is promised, because no maintainer group is named (O7).

### 7.3 What is and is not portable

Portable to another vendor through supported plugin interfaces, without changing the core source: alert intake, every agent read (amended 2026-11-11: with no code only where the system offers an MCP server with read tools), the harness itself, page channels as `notifiers` contributions, kernel metric reads for stores that speak the Prometheus query API, change evidence and estate sources at P2, tools, notifiers, on-call providers, chat surfaces, executors, rule packs and runbooks. Not portable in this edition: the agent and fleet, the tenant gateway, limits rendering, backend provisioning, provisioned dashboards and the annotator, the shipped rule files as written, the lab (6.6).

### 7.4 What a maintainer promises

1. A stable definition is never broken inside a major; the window of 4.1 holds, with the removal date in every release's notes.
2. A `v0` definition may break at a minor release, with a migration note. A plugin built for one release is rebuilt or re-attested for the next while its definitions are `v0`.
3. Whoever breaks a definition fixes every plugin under `plugins/` and `contrib/` in the same pull request, as far as replay can prove. A contrib plugin that needs a new vendor recording is marked `needs-fixture`, leaves the next `aiops-contrib` image with a line in the release notes, and returns when its code owner lands the fixture. CI on main builds `contrib/` in a Go workspace; SDK version lines are rewritten at the release tag.
4. A suite never tightens an obligation of a stable definition inside a major. Promotion to `v1` changes nothing but the import path.
5. The manifest schema and the index format change additively from 1.0.
6. A contrib row is disabled by default, never in a required set, never needed by a first-party row.
7. Not promised: that a vendor's API stays as the fixtures recorded it; performance; first-party row behaviour beyond definitions and the versioned row config of instantiable plugins. A contrib plugin whose code owner has not answered a failing live report for two releases is marked unmaintained and leaves `aiops-contrib`.

## 8. Review dispositions

All 47 findings were applied except those below. The largest applied groups: the data pack is in the CR1 cut with a versioned contract and its trust rules (section 6.4); major 0 compares minors exactly and promotion is not a break (4.1); S6 is split by operation, not by row, so the binding maps are stable (2.2); entitlement and edition machinery is out of the community release (4.4, 4.7); toolset admission is data (6.3 D2); the two designs' contradictions are resolved to `contrib`, `requires`, one check command and one scenario script.

| # | Not applied | Reason |
|---|---|---|
| N1 | The sequencing design's placement of the remote runtime and the Python SDK after CR1, written into 00 as an owner decision | The owner did not say it. Decision 4 defers per-tenant remote pods; decision 5 lists the protocol under "do now". 00 keeps the runtime before CR1, and the owner confirmed that on 2026-11-11 (owner decision W5, 00 §2.4; O1 is answered: before). With it go three fixes that only that placement needs: a loopback adapter test at S2a, the lock file moved to S2a alone, a Go-only template; they were conditions of O1 option (b) and are closed with it |
| N2 | Admitting a toolset id as a core change with a closed table in `investigator-holmes`, with ids pre-admitted at CR1 | Two other findings show it re-hard-wires vendors in a pillar row and cannot work for SaaS, where our CI records nothing. Applied from the same finding: the render is default-deny, the generic toolsets are in the code deny-list, the reference pack is decided |
| N3 | `edition`, `commercial.entitlement`, gate check 11, `entitlementIssuers`, `publisher.verified`, `LicenseRef`, a `review` attestation type and `homepage` in the community release (sequencing design D7) | Owner decision 5 defers entitlement, verification and review. A manifest major is the seam; the existing checks already fail closed |
| N4 | Moving `signal-ingest-alert-webhook` to `aiops-datasources` | The Alertmanager webhook is the common alert transport of Prometheus-compatible stacks, and a profile that composes another pack without the default one would lose it. A payload parser behind `signals` is a contribution, not a hard-wire. The product sentence is narrowed instead to "no query string and no telemetry store query API", with the static-graph check |
| N5 | A reverse adapter so that a third-party provider of an open singular key survives a new major | One implementer; the cheaper statement of 4.1 is applied |
| N6 | Three majors of one definition in service when two majors ship inside one window | One adapter per definition is cheaper; the ship rule of 4.1 is applied |
| N7 | Dropping the `contrib` id owner in favour of stable author ids | An id that carries a contributor's name changes when the plugin changes hands. The tier is chosen at scaffold time instead |
| N8 | Moving `aiopsctl index compose`, `sign`, `verify` and `plugin gate --against` to S6b or enterprise | They are the commands our own index checks use; nothing else is built for third parties |
| N9 | The first-party non-default reference as an owner option only, and a vendor code plugin written by us in `contrib/` | Decision 7 needs a release test. The smallest artifact is decided: one data pack and one sample sender under `examples/`. Which system the pack targets stays the owner's (O3) |
| N10 | A dispatcher guard that refuses a binding endpoint outside the origin's `capabilities.network` | The endpoint is site config, so the manifest names the field (`config:/endpoint`). The dialer ranges are applied instead |
| N11 | `plugin-index` only in the `cli` profile | It stays a row of `aiops-core`, as 00 §4.4 has it; the manager's install by `<id>@<version>` reads it |
| N12 | SDK preview tags at S2a and S3; a release-candidate job that runs third-party suites on the CR1 path; "certified" as a status word | Not on the owner's lists. The job has a trigger (4.5); the word waits for O8 |

## 9. Open items

| # | Item | Who | By |
|---|---|---|---|
| O1 | Answered 2026-11-11: (a), before CR1. This is owner decision W5 (00 §2.4); option (b) is rejected and kept here as the record. The question as it was put: the remote runtime and the Python SDK (S2b). (a) Before CR1, as 00 has it: Python and install without a rebuild on the first day, one more step before the release. (b) After CR1: the release is one step earlier, a code plugin from a third party needs a rebuild by each operator or a merge into `contrib/`, the handshake package and a toy remote echoer are still built at S2a, every `contrib` definition passes its suite through a generated adapter in loopback, and the Python tool leaves the S8b exit | Owner | Closed 2026-11-11 |
| O2 | Accept the community and enterprise principle and settle three cases: tenant create and delete on a self-host; whether the `hosted` profile and the `aiops-hosted` rows leave the Apache-2.0 repository; key-management providers | Owner | Before S9b is planned; the first before S4b |
| O3 | W6 is confirmed (owner decision of 2026-11-11, 00 §2.4): `aiops-only` is supported at CR1 for the connector scenario, and one reference pack is kept in CI. Still open: which system the pack targets (proposed: a logs binding for VictoriaLogs; its licence is confirmed under 08 §5.1 L13 first), and how much more of the demo `aiops-only` must replay at CR1 | Owner | Before S8a starts |
| O4 | An optional preview tag of the loop on `aiops-only` and `dev` before S7 finishes | Owner | Before S7 starts |
| O5 | The contributor agreement, and whether plugins under `contrib/` are promised free in every edition | Owner | Before the first external pull request is merged |
| O6 | Where enterprise plugins live, and whether a written community promise is published | Owner | Before any enterprise code exists |
| O7 | Who the maintainers are, and a review time for `contrib/` | Owner | CR1 review |
| O8 | The project's mark policy for third-party marketplaces and for the word "certified" | Owner | Before a third-party index is announced |
| O9 | The window numbers (12 months, two minor releases) | Chief architect | CR1 review |
| O10 | Whether a `signals` contribution may supply its own signature verifier; today the auth vocabulary of a route is bearer token or HMAC | ADR-047 | S8a start |
| O11 | Instance identity of a remote plugin on a single-tenant self-host | ADR-041 | S2b |
| O12 | Counsel: a plugin id as a technical identifier for marks whose owners publish no naming rule or a strict one | Counsel | Before the first vendor pack is listed |
| O13 | Amended 2026-11-11: the ADR-003 spike is re-scoped to the optional HolmesGPT plugin and runs time-boxed in S8a; the tool list procedure is the digest rule of `tool-mcp` | ADR-003 | S8a |
| O14 | 07 gives no Go signature for `metricsQuery`, `logsQuery` or `annotations`; their neutrality is rechecked when the signatures are written | 07 | S8a start |
| O15 | Added 2026-11-11 and closed the same day: the owner confirmed W7 to W13 of 00 §2.4, which are owner decisions from that date: the default harness in CR1; the two optional upstream adapters under `plugins/`, built in S8b; `tool-mcp` in place of the binding registry; evaluation as a gate; the built-in on-call provider as the default, with no SMS or voice first-party at CR1; the loop and the provider in the kernel process; the Slack bundle outside the shipped profiles. One condition of W11 remains: no code of the built-in provider before counsel's answer on schedule patents (O17, Q3), and if that answer is missing at S8b start CR1 ships providers only | Owner | Closed 2026-11-11; Q3 stays with O17 |
| O16 | Added 2026-11-11. A numeric accuracy floor for CR1, and who pays for an evaluation reading (144 live case-runs) | Owner | At the first reading |
| O17 | Added 2026-11-11. Counsel: the questions of licence record §6, first the schedule patents (Q3), the marks in plugin ids (Q4), the dashboard vendor's plugin and trademark policies (Q5, Q6) and the Slack terms (Q14) | Counsel | Q3 before any code of the built-in provider; the rest before the step each blocks |

After the confirmation of 2026-11-11 the owner's open items of this record are O2 to O8 (of O3 only the target system and how much of the demo `aiops-only` replays) and O16. The full list across the records is at the end of 00 §2.4.

| ADR | Question | Decide by |
|---|---|---|
| ADR-040 | Decided here: section 4.1 | |
| ADR-047 | Vendor-neutral connectors: the read contract, D1 to D7, the pack artifact form and gate check 8, toolset proof as data, the dialer ranges on self-host, the naming rules; ADR-028 is re-scoped around `evidenceSources` | S2a for the artifact form and the gate rule; S8a start for the definitions |
| ADR-048 | Manifest schema 1.0 and index format 1.0: the fields of 4.4 and 4.5, trust roots, the re-certification job and its trigger | S2a (manifest); S6a (index) |
| ADR-049 | The community and enterprise boundary, the home of enterprise plugins, the community promise | Owner; before S9b is planned |
| ADR-050 | The contributor agreement and the status of contributed plugins | Owner; before the first external pull request is merged |

Changed decide-by values: ADR-020 and ADR-044 at S2a; ADR-041 as in 4.3; ADR-036 (bench at S0, cost model at S4b); ADR-038 (restore on one process at S4a, the rest at S4b).

## 10. Outside facts

Read on 2026-11-11 by the two designs or the reviews; not re-read for this record.

| Fact | Source | Version |
|---|---|---|
| HolmesGPT 0.46.0, released 2026-11-04, Apache-2.0; toolset directories for `victorialogs`, `datadog`, `elasticsearch`, and the generic `bash`, `http`, `internet`, `mcp` | `https://github.com/HolmesGPT/holmesgpt/tree/0.46.0/holmes/plugins/toolsets` | Tag 0.46.0. Which toolsets are enabled by default, and the read-only property of each, UNVERIFIED |
| Datadog webhooks retry only on an internal error or a 5xx response; 15 s timeout; a 429 is not mentioned | `https://docs.datadoghq.com/integrations/webhooks/` | Current page, no version. The number and spacing of retries after a 5xx UNVERIFIED |
| VictoriaMetrics query paths; `vmalert` loads the Prometheus rule format and notifies Alertmanager; VictoriaLogs query API and tenant headers | `https://docs.victoriametrics.com/victoriametrics/url-examples/`, `.../victoriametrics/vmalert/`, `.../victorialogs/querying/` | Current pages. The VictoriaLogs licence was not read |
| Kubernetes removes no GA API version inside a major; beta versions are served 9 months or 3 minor releases after deprecation | `https://kubernetes.io/docs/reference/using-api/deprecation-policy/` | Current page |
| The Terraform plugin protocol keeps majors 5 and 6 in service, with additive minors | `https://developer.hashicorp.com/terraform/plugin/terraform-plugin-protocol` | Protocol 5.10 and 6.10 |
| Elastic's marks may not be part of a product name; referring uses are allowed | `https://www.elastic.co/legal/trademarks` | Current page |

UNVERIFIED and to settle with the first pack of each kind: scoped application keys, real query rate limits and Kubernetes tag names at Datadog; whether SigNoz or a ClickHouse-based stack can be bound through a database toolset; the OpenSearch product-name clause; naming rules of Datadog and VictoriaMetrics; the CNCF charter wording on sign-off. Whether cosign attaches an attestation type of our own is settled by ADR-020: `cosign attest` accepts the type but writes a Statement v0.1, so only `aiopsctl` writes the attestation.

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
