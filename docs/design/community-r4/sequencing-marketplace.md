# Community-first order of work and marketplace readiness (round 4)

Status: design proposal of 2026-11-11, written for the round-4 amendment of the canonical record. It changes nothing by itself: section 12 lists what must change in `docs/architecture/00-decisions.md` and in the plugin record, and until that amendment is applied 00 wins. Conventions: `00 §n` is `docs/architecture/00-decisions.md`; `record §n` is `docs/design/plugin-system-2026-11.md`; `08 §n` is `docs/architecture/08-profiles-roadmap-adrs.md`. dsh is DeepSeek Harness. CR1 is the first community release and CR2 the second. Reader codes: SH a single-tenant self-hoster, CO a plugin contributor, HO the hosted operator, MP the marketplace.

Inputs: the owner decisions of 2026-11-11 (section 1), 00 §1, §2.4, §4, §6, §7 and §9, record §9 to §16 and §18, and 08 §2, §4 and §5. Outside facts are listed with URL and date in section 14; anything not checked is marked UNVERIFIED. Nothing here is measured, and no step has a date.

---

## 0. What this design decides

| # | Decision | Section |
|---|---|---|
| D1 | The dsh plugin architecture is kept whole. Only the order of work changes, by one rule: the contract a plugin author sees is complete at the first community release, and the machinery an operator of many tenants or many replicas needs arrives later behind the same definitions | 3.1 |
| D2 | Steps S2, S4, S6 and S9 are each split in two. The order is S0, S1, S2a, S3, S4a, S5, S6a, S7, S8 (first community release), S2b (second community release), then S9a, S4b, S6b, S9b and S10 as re-baselined at the CR1 review. This replaces working assumption W1 | 3.2, 3.3 |
| D3 | CR1 is `self-host-full` with one tenant and one kernel process, plus `dev`, with `aiops-only` proposed as a supported profile. Its acceptance test has an operator leg (the 00 §7.1 demo, unchanged) and a contributor leg (a plugin for a backend outside the default stack, written from the template with no kernel checkout, built into a distribution and used) | 4 |
| D4 | Thirteen guarantees hold at CR1 so that nothing written for it is rewritten later. The two that are easy to miss: every plugin is tested with two tenants although one is deployed, and every `contrib` definition is written from its proto although the remote runtime ships after CR1 | 4.4 |
| D5 | ADR-040 is decided: every definition has its own `major.minor` version; manifests carry a generated `requires` block and distributions a generated `serves` table; `engines.aiops` is removed; an old major is served for at least 12 months and two further minor releases after its successor ships, and is removed only in a platform major release | 5 |
| D6 | The remote protocol has one frozen handshake message that negotiates the protocol major; the kernel serves the current and the previous major under the same window. The rule is fixed now and built at S2b | 6 |
| D7 | Manifest schema v1 reserves `publisher`, `homepage`, `source`, `support`, `edition` and `commercial.entitlement`, each validated now. A price is never a manifest field. Gate check 11 (edition and entitlement) is reserved and fails closed | 7 |
| D8 | The public index is a signed Git repository with a versioned format. Its CI re-certifies every listed plugin against each release candidate, and a regression on a stable definition blocks our release | 8 |
| D9 | Each item deferred to enterprise attaches to a seam that exists at CR1: a manifest field, a gate check, a `trust.yaml` list, the index format, the outbox, or the builder | 9 |
| D10 | Community plugins live in `contrib/` of this repository under the id owner `contrib`, built into the `aiops-contrib` distribution, or in the author's own repository and listed in the index. The maintainer promise is written as nine rules | 11 |

Not decided here, recorded for the owner: the community and enterprise boundary, where enterprise plugins live, and the contributor agreement (section 10 and section 13).

---

## 1. Owner decisions this design rests on

| # | Decision (2026-11-11) | Used in |
|---|---|---|
| 1 | The end goal is a plugin marketplace where users contribute and sell plugins and the project earns from it; third parties may run their own marketplaces | 8.3, 9 |
| 2 | The marketplace with billing is a later enterprise feature and is not built now | 9 |
| 3 | Phase 1 is the community edition: something people can run, users and feedback first. Dates are planning targets | 3, 4 |
| 4 | Keep the plugin architecture and change the order of work; this replaces W1 of 00 §2.4. W2, W3 and W4 stay working assumptions | 3 |
| 5 | Do now what would be a migration later: a version per definition with a support window, a versioned remote protocol with a handshake, manifest fields for publisher and licence, the gate and the attestation. Defer billing, entitlement, publisher verification, review, the marketplace UI, WebAssembly and third-party plugins on hosted | 5, 6, 7, 9 |
| 6 | Writing a plugin must be easy: SDK, solo test kit, a template, one check command. The index is a signed public repository | 8, 11 |
| 7 | First-party connectors target the Grafana ecosystem and stay the default; a contributor must be able to write plugins for another observability vendor or stack and contribute them back | 4.3, 5.6, 11 |
| 8 | The boundary between community and enterprise is open and is recorded as a proposal | 10 |

---

## 2. Two axes that must not be confused

"Before or after the first community release" is an order of work. "Community or enterprise" is an edition. A part that comes after CR1 is not thereby enterprise. Section 3 answers the first question and section 10 proposes an answer to the second.

| Part | Order | Edition (proposal of section 10) |
|---|---|---|
| Remote runtime and the Python SDK (S2b) | After CR1 | Community |
| Generations across several replicas (S4b) | After CR1 | Community: one organisation needs high availability |
| Many tenant trees, tenant overlays, tenant create and delete as operator actions (S4b) | After CR1 | The mechanism is community; open for the owner (10.2 row 1) |
| Manager service, cohort update, quarantine (S6b) | After CR1 | Community, except cohorts |
| Hosted runtime and operations (S9b) | After CR1 | Open for the owner (10.2 row 2) |
| Marketplace service, billing, entitlement issuing | Not planned | Enterprise |

---

## 3. The re-cut order of work

### 3.1 The rule that decides the cut

**The author contract is whole at CR1; the operator machinery is in its single-tenant, single-process form.** The author contract is record §4 (one `plugin.Define(Spec)`, needs and provisions as data, effects, hooks, the tenant handle, `scope`), the definitions with their fakes and suites, the manifest, and the builder. The operator machinery is whatever sits behind a definition as a provider row: how many tenants `tenant-trees` mounts, who commits a generation, where a remote instance runs.

This is the dsh property used on ourselves. A later part arrives as a provider row, a new row, a new layer or a wider test, never as a change to an interface a plugin compiles against. Where a later part would need an interface change, the interface is done now (section 4.4). The rule also says what is not cut: nothing in the kernel, nothing in the policy spine, and no test that proves a plugin is tenant-correct.

### 3.2 Verdict per step of the plugin record

| Step (record §16) | Verdict | Before CR1 | After CR1 | Who needs the later part, and why it can wait |
|---|---|---|---|---|
| S0 Spike and kernel | Before, whole | The spike, `sdk/kernel`, `kerneltest`, the differential and property tests, the line budget. The 500-tree and cold-mount measurements stay in the spike as a kernel-only bench | Nothing | SH, CO, HO alike. The bench needs only `Extend` and `Isolate`, which are ported anyway, and its result can change the tenancy model the author contract is built on. Measured after community plugins exist, a failed threshold would be a migration instead of a design change |
| S1 Loader and launcher | Before, minus one layer | `loader`, `composition`, `boot`, the launch services, layers 0 to 4 and 6, the row format with all ten fields (`roles` and `required` included), dumps, references, the module table, `restart-policy`, `reload`, `readiness`, the matrix harness | Layer 5 (tenant overlay) and its rules; golden dumps per role; the tenant-tree column of the matrix | HO. One tenant is configured in the site layer. `roles` stays on every server row now because it is row data that bundle authors write, and adding it later would edit every bundle, community ones included |
| S2 Ecosystem path | Split | **S2a:** the two toy registries; `plugin.Define`, `plugingen`, manifest v1 (section 7); the builder (`dist build`, `dist sign`, strict versions, licence scan, notices); `plugin new`, `check`, `smoke`, `pack`, `sign`, `push`, `install`, `verify` for in-process artifacts; the attestation; `trust.yaml`; the gate; definition versions (section 5); the generator and lint that give a `contrib` definition its proto and its message types, applied to each such definition when it lands; the Go template | **S2b:** `sdk/remote`, `sdk/remotehost`, the `remote` proxy row, the handshake (section 6), the Python SDK `aiops-sdk` and its template, `plugins.lock`, remote artifacts in `install` and `verify`, gate checks 3 and 9 | CO in other languages, MP (a closed or paid plugin can only be remote) and HO (W4). SH runs the product without it, and a contribution that is contributed back is reviewed Go in process. The protocol rule is fixed now so that the first remote plugin already speaks a versioned protocol |
| S3 Infrastructure definitions | Before, whole | Every definition, provider, fake and suite of the step; the transaction contract; per-plugin roles; migrations by the `durable` leader; the exit test unchanged, on one process | Nothing. `events-nats` and `kek-kms` keep their triggers | SH: nothing runs without state, jobs, events and HTTP. CO: the fakes and suites are the content of the solo test kit. `Drained()` stays in the `durable`-class definitions now because adding a method later breaks third-party providers |
| S4 Reload and tenancy | Split | **S4a:** `tenant-directory` holding the tenant `default`, `tenant-handle` and `tenantMint`, `authz`, `tenants.Table`, `tenant-trees` mounting one pinned tree at boot, the seven bound views, `sidecars` with `sidecars-static`; `tenant-lifecycle` with the create saga, run once for `default`, and the hooks `tenant/create` and `tenant/purge`; `admin-api` for bindings and platform users; a generation as a digest that the `reload` queue applies, judges and commits or restores on one process | **S4b:** `composer` and its state machine (canary, promotion), the generation object and `reload-generation`, tenant-scope generations and replica reports; trees mounted on first use and unloaded when idle, the cold lookup, the load cap; layer 5 and per-tenant toggles; the tenant required set; tenant create, suspend and delete as operator actions on `admin-api`; the 500-tenant exit | HO, and SH only when it runs several replicas. The single tree is kept because `scope: tenant` is a manifest field: without bound views `investigator-holmes` and `on-call-provider-goalert` would be written as process-scope plugins and rewritten for hosted. The two lifecycle hooks are kept for the same reason: a plugin's per-tenant setup and purge steps are listeners, and they are written once |
| S5 Policy spine and first dispatcher | Before, minus remote forms | `identity`, `audit-chain`, `pii-pseudonym`, `approval`, `change-hub`, `credential-lease`, `remediation` with a no-op executor, both bindings, `sdk/gate`; `notifiers`, `notifications` and an in-process notifier; `test/policy-bypass/` on a test-only profile seeded with two tenants | The remote Go and remote Python notifier (S2b); bypass attempts 3, 17, 18, 20 (idle tree and remote forms), 22 and 23, each with the part it attacks | SH: the CR1 demo approves and applies a change. The fences are in-process bindings and database roles, so none of them waits for the remote runtime |
| S6 Manager and index | Split | **S6a:** `plugin-inventory`; `list`, `inspect`, `install`, `enable`, `disable`, `remove` and `purge` as `aiopsctl` operations on the profile files through the reload queue; the public index repository with its format and its CI (section 8); `aiopsctl index add` and `plugin search`; `contrib/` and the `aiops-contrib` distribution | **S6b:** `plugin-manager` as a service row with its platform tables, `plugin-index` as a server row, `update --cohort`, `quarantine`, exemptions by version, `profile rollback --scope`, the 03:00 drill | HO and MP: operations for many tenants and a takedown path for a published version. SH edits files on its own host, where file access is the authority |
| S7 Telemetry base | Before, whole | As 00 §7.2. The 300 req/s figure is measured and recorded | The 300 req/s pass threshold becomes a gate of S9b | The owner names the agent as part of the community product. The threshold is sized for 50,000 agents (00 §2.1 row 7) |
| S8 Incident loop | Before, whole | As 00 §7.2 and 08 §2.2, all 23 method rows. The exit gains the contributor leg (section 4.3) | "A Python tool installed as a remote plugin" moves to the S2b exit | SH |
| S9 Hosted and second wave | After, split | Nothing | **S9a:** the method and estate groups 1 to 6 and 8 of 08 §2.3, SecOps, FinOps, the Windows agent, `aiops-only` as far as CR1 left it. **S9b:** the hosted runtime and operations, group 7 | S9a serves SH and has no edge from S4b, S6b or S9b. S9b serves HO |
| S10 Breadth | After | Nothing | As 00 §7.2, re-baselined at the CR1 review | SH for the two pillars, HO for hosted scale |

What the re-cut takes off the path to the first release: the remote runtime and the Python SDK, the composer and everything multi-replica, every multi-tenant behaviour beyond one pinned tree, the manager service with cohort update and quarantine, and three hosted-sized exit tests (S4, S6, the S7 load threshold). What it does not take off: the kernel, the loader, the in-process authoring path, the infrastructure definitions and the policy spine, because the community product is made of rows that inject them. No estimate of effort is given; none has been measured.

### 3.3 The order, with exits

| Phase | Step | Builds | Exit test |
|---|---|---|---|
| P0 Model core | S0 | As record §16 | As record §16 |
| | S1 | As record §16 without layer 5 | The failure matrix (process-scope columns) and the recomposition suite pass on the built binary; golden dumps |
| | S2a | The in-process authoring path (3.2) | A repository with no kernel checkout creates an in-process echoer from the template, passes `plugin check` with no container runtime, packs and signs it; a clean machine builds and signs a distribution that holds it and calls it. The gate denies a copy that needs a definition version the release does not serve and a copy whose attestation names another digest. An SDK preview is tagged |
| | S3 | As record §16 | As record §16, on one process |
| | S4a | The tenancy minimum (3.2) | The create saga makes the tenant `default` at first boot and a toy listener on `tenant/create` runs its step once; a `scope: tenant` toy plugin mounts in the tree of `default` with only bound views; a transaction with no handle sees zero rows; a job runs under the tenant of the transaction that wrote it; a failed generation is restored on one process with the audit record `composition/applied` |
| | S5 | As record §16, in process | `test/policy-bypass/` passes for every attempt whose target exists, with a real egress dispatcher and two seeded tenants |
| P1 Community release | S6a | Inventory, CLI operations, index repository and CI, `contrib/`, `aiops-contrib` | `aiops-contrib` is built by the builder from `dist/contrib.yaml`, boots `self-host-full` with every contrib row disabled and passes the T3 of `aiops`; the index document verifies before it is parsed |
| | S7 | As 00 §7.2 | As 00 §7.2; the load figure recorded |
| | S8 | As 00 §7.2 | **CR1**: the acceptance test of section 4.3 |
| P1 follow-up | S2b | The remote runtime, single-tenant form, and the Python SDK | **CR2**: a repository with no kernel checkout publishes a remote Go echoer, a remote Python stamper, a remote notifier and a Python tool; a clean machine installs them into `self-host-full` from signed artifacts with no rebuild and calls each; a plugin that offers only an unknown protocol major is refused at the handshake with the reason logged on both sides; every `contrib` definition shipped in CR1 passes its suite through the remote adapter, or is changed while it is still `v0` |
| P2, two tracks | S9a | The community second wave | Re-baselined at the CR1 review |
| | S4b, S6b, S9b | Multi-replica and multi-tenant runtime, the manager service, hosted | The S4 and S6 exits of record §16, then the P2 exit of 00 §7.2 |
| P3 | S10 | As 00 §7.2 | As 00 §7.2 |
| Enterprise | not planned | The marketplace service (section 9) | Not set |

| From | To | Why this order |
|---|---|---|
| S0 | S1 | Unchanged |
| S1 | S2a | The builder needs the module table, and `plugin smoke` needs the loader for T3 |
| S2a | S3 | From S2a no definition merges without its fake, suite, version, a provider and a consumer |
| S3 | S4a | The tenant directory and the bound views need `sql`, `secrets` and `sidecars` |
| S4a | S5 | The policy spine needs the tenant handle and per-plugin roles |
| S5 | S7 | Every state-changing row injects `audit`, the rows of the telemetry base included |
| S2a | S6a | The index CI runs the gate and reads the attestation; `aiops-contrib` is built by the builder. S6a may run beside S3 to S8 |
| S7, S6a | S8 | The loop on `self-host-full` reads the signals the telemetry base ships; the contributor leg needs the index and `contrib/`. The `aiops-only` slice of S8 has no edge from S7 |
| S8 | S2b | Owner decision 3: a release people can run comes before a second runtime |
| S2b | S4b, S6b | One tenant per connection and activations across replicas are ratified on a runtime that exists |
| S8 | S9a | The second wave extends the released loop; within P2 community items go before hosted ones (owner decision 3) |

Standing rules, from S2a: no definition merges without its fake, its suite, its `definition.yaml`, a provider and a consumer; no plugin merges without `aiopsctl plugin check` and a T3 test; nothing outside the closed vocabularies of 00 §4.3 merges into `sdk/kernel`, `boot/` or `loader/`; whoever changes a `v0` definition fixes every plugin under `plugins/` and `contrib/` in the same pull request.

Tags that are not releases: an SDK preview at the S2a exit and again at S3, so that contributors can try `plugin new` and `plugin check` on toy and infrastructure definitions and report on the authoring path before the product exists. A preview of the loop on `aiops-only` before S7 finishes is possible and is an owner option (section 13, O3).

---

## 4. The first community release

### 4.1 What ships

| Item | CR1 |
|---|---|
| Version | `0.1.0`. A `0.y` platform version says that no definition is stable yet (section 5.6) |
| Profiles, supported | `self-host-full` on compose or Helm, with the tenant `default` and one `aiops --role all` process; `dev`; `cli`; `agent-host`; `agent-k8s`. `aiops-only` is proposed as supported with a reduced demo (section 13, O2); if the owner declines, it keeps the CI boot of 00 §7.1 |
| Profiles, absent | `hosted` |
| Artifacts | The images `aiops` and `aiops-contrib`, the builder image, `aiopsctl`, the Go SDK modules, the release `dist.yaml` with its `serves` table, the charts and compose files |
| Bundles | As 00 §4.4 at P1 scope, with `aiops-core` at 8 rows (`remote`, `plugin-manager` and `plugin-index` join at S2b and S6b) and `aiops-base` without `composer` (S4b) |
| Required set | The keys of 00 §6.1 and the rows `loader`, `restart-policy`, `reload` and `readiness`. `composer` is required only in a profile that runs more than one replica |

### 4.2 What people can do with it

An operator can install the agent on a VM and on a cluster and see four signals on provisioned dashboards; receive alerts from the shipped rule files or from any Alertmanager-compatible sender; get one incident per entity with a Slack thread and one page through the bundled on-call provider; read an RCA from the investigator that cites the change candidates; approve and apply one of five Kubernetes actions with a second approver, a ledger record and two verification checks; ask for the compliance score; export the audit chain; run all of it on a local open-weight model. An operator can also enable a row of `aiops-contrib`, or build and sign a distribution that adds any in-process plugin, without changing the core source.

A contributor can create a plugin from the template, test it alone on the real kernel with fakes, run one check command, publish a signed artifact, list it in the public index, and open a pull request that puts it into `aiops-contrib`.

Nobody can yet: run several kernel replicas, create a second tenant through an API, install a plugin without a rebuild, write a plugin in Python, or run the hosted profile.

### 4.3 The acceptance test

| Leg | What must pass | Where |
|---|---|---|
| A. Operator | The six-step demo of 00 §7.1 with its host leg and the recorded local-model run, unchanged. The fixture replay passes on `dev` in its no-page form | `self-host-full`; CI replay and `lab/` live |
| B. Own stores (proposal, O2) | Steps 2 to 5 and the audit export of step 6, replayed from fixtures recorded against a Prometheus-compatible metrics store, an Alertmanager and a Loki-compatible logs store, none of them deployed by us | `aiops-only`; CI replay |
| C. Contributor | On a machine with the SDK module, `aiopsctl` and a Go toolchain, and no kernel checkout: (1) `aiopsctl plugin new <definition> --lang go` creates a plugin for a backend outside the default stack; (2) after the author's code, `aiopsctl plugin check ./` passes T0 to T2 with the generated cycle test and two-tenant test and no container runtime; (3) `plugin pack` and `plugin sign`; (4) an operator runs `trust add`, `plugin install` (answer: `rebuild-required`), `dist build`, `dist sign`, `dist use`, `plugin install`, `plugin enable`; (5) the row is ACTIVE and one step of leg A or B runs through it; (6) the same source, opened as a pull request under `contrib/`, is green in CI and appears in the next `aiops-contrib` image with its row disabled | A scratch repository; CI |
| D. Index and gate | The public index lists the plugin of leg C and one plugin of `contrib/`; `aiopsctl plugin search` verifies the detached signature before parsing; the re-certification job has run against the release candidate and its report is a release asset. Four refusals: a plugin that needs a definition version the release does not serve; a plugin with a missing or foreign attestation; a plugin with `edition: enterprise`; a plugin with a non-empty `commercial.entitlement` | CI |

Which registry leg C contributes to is fixed by the backend-neutral definitions design of this round. Until it names one, the fallback is `signals`, which exists at P1 and is marked `contrib`. At least one plugin for a backend outside the default stack is in `contrib/` at CR1, written by us through the public path only, so that the path is exercised on every commit and not once at the release.

### 4.4 What "everything is a plugin, testable alone" guarantees at CR1

Each guarantee names its proof and the later part that attaches to it without a change to any plugin.

| # | Guarantee at CR1 | Proven by | Later part that attaches unchanged |
|---|---|---|---|
| G1 | The kernel is the complete port: `Extend`, `Isolate`, `Intercept`, epochs, six states, hooks with an audience. No product word | Differential and property tests, vocabulary and line-budget checks | Tenant trees, pillar groups and remote proxies are compositions of it |
| G2 | The loader composes layers 0 to 4 and 6 with the full row format; rows restart in place; the `reload` queue admits, applies, judges and restores | Failure matrix, recomposition suite, golden dumps | Layer 5 is one more layer; `composer` is a different committer of the same verdict |
| G3 | One author shape. `Apply` may run again in the same process; no package-level state, bare goroutine or `init` | Plugin vet (T0), the generated cycle test (T1) | The same plugin value in every profile and, where its needs allow, in the remote runtime |
| G4 | Every service another plugin calls has a definition, a fake and a suite, and a plugin imports definitions only | Import analyzer; the standing rule; `conformance.Run` | Provider swaps (`events-nats`, `kek-kms`, `sidecars-kubernetes`) are rows |
| G5 | Plugins are tenant-correct although one tenant is deployed: the handle travels in the context, every tenant table has `tenant_id` under forced row-level security and a per-plugin role, every registry stores contributions in `tenants.Table`, a `scope: tenant` plugin uses bound views only | The generated two-tenant test for every process-scope plugin; the tenant-fence attempts of `test/policy-bypass/` on two seeded tenants | Many trees, cold lookup and overlays change `tenant-trees` and `tenants.Table`, not their callers |
| G6 | Policy is bound and fails closed with both binding maps in the signed distribution manifest | `test/policy-bypass/` | Hosted adds required keys; it loosens nothing |
| G7 | Every `contrib` definition is remote-shaped: its message types are generated from the proto written with it, its methods take a context and those types only, and no method takes a `Tx`, a function or a channel | Definition lint in CI | S2b adds the adapter runtime. A `contrib` definition does not leave `v0` before its adapter has passed the suite |
| G8 | Versions are data from the first plugin: `definition.yaml`, the generated `requires`, the distribution `serves`, manifest schema v1 with the reserved fields, the attestation | Manifest parity check; leg D | The support window, the index and an entitlement check read fields that already exist |
| G9 | In-process third-party code enters only through the builder and a signed distribution manifest; `aiops-contrib` is built the same way | S2a and S3 exits; leg C | The enterprise edition is one more distribution (section 10.3) |
| G10 | The solo test kit is complete for Go: `kerneltest`, fakes, the generated tests, `plugin check` with no kernel checkout and no container runtime, and a template that CI builds | S2a exit; template job (11.2) | The Python fixture of S2b wraps the same runner |
| G11 | Durable state is declared in `Spec.Durable`, migrated expand-then-contract by the leader and purged by plugin id | Uninstall residue and purge tests of S3 | The tenant delete saga reads the same registry |
| G12 | Every server row states its `roles`, though CR1 runs `--role all` | Bundle lint | The role split is a deployment change |
| G13 | Every state change is an outbox event and an audit append in the caller's transaction | The suites of `events` and `audit` | Meters and marketplace usage reports are subscribers |

### 4.5 What is deliberately absent, and how it arrives

| Absent at CR1 | In its place | Arrives as | Why no plugin changes |
|---|---|---|---|
| Remote runtime, Python SDK, install without a rebuild | The builder path; `aiops-contrib` | S2b: the `remote` row, `sdk/remotehost`, `aiops-sdk` | G7. An in-process plugin whose needs are inside the callback set also runs remote from the same value |
| Several replicas, canary and promotion of a generation | One process; `reload` commits or restores | S4b: `composer`, the generation object | Plugins never see who commits; G2 |
| Many tenants, trees on first use, overlays, tenant create and delete as operator actions | The tenant `default`, created by the saga at first boot; one pinned tree; the site layer | S4b: a wider `tenant-trees`, `tenantTrees.Ensure` inside `tenants.Table`, layer 5 | G5. A tenant-writable mark on a config field is additive: absent means not writable |
| Manager service, cohort update, quarantine, exemptions by version | `aiopsctl` on the profile files | S6b: `plugin-manager`, `plugin-index` rows | Same operations library and the same queue |
| Hosted bundle rows (`kek-kms`, `sidecars-kubernetes`, `reload-generation`, `cohorts`, `billing-meter`, limits, pools) | Their self-host counterparts | S9b | Allowed-set choices and new rows; G4 |
| Third-party plugins on hosted | Nothing | S2b, S4b, S6b, then W4 | Gate check 9 |
| WebAssembly | `wasm` validates in `runtimes` and the gate denies it | Its trigger (00 §7.3) | A proxy row, as for remote |
| Marketplace, billing, entitlement, publisher verification, review | Reserved fields and check 11 | Enterprise (section 9) | Section 9 names the seam of each |

---

## 5. Versioning policy (ADR-040, decided)

### 5.1 What carries a version

| Versioned thing | Form | Where it is declared | Who reads it |
|---|---|---|---|
| A service definition | `major.minor`. The major stays in the import path, the key and the proto package | `sdk/def/<name>/v<N>/definition.yaml` (key, version, stability, class, remote mark, deprecation fields) and a constant in the package | `plugingen`, the gate, the index CI |
| The author API (the kernel as a plugin sees it, `sdk/plugin`, `sdk/gate`) | `major.minor`, named `pluginApi`; in-process only | The SDK | The gate, the builder |
| The remote protocol | An integer major (section 6) | The proto package | The handshake |
| The manifest schema | `manifestVersion: "major.minor"` | `plugin.yaml` | The gate, the index CI |
| The index format | `indexVersion: "major.minor"` | The index document | `aiopsctl`, a third-party index |
| The platform release | Semantic version | The distribution manifest | People; the index `compat` map |
| The Go modules `sdk/kernel`, `sdk/def`, `sdk/testkit` and the Python package | Packaging versions | `go.mod`, the package index | The Go toolchain and the builder only. No gate reads them |

### 5.2 Rules for a definition version

1. **Major.** Any change after which a plugin that passed the suite could fail it, fail to compile, or be refused at the wire: a removed or changed method, a changed message field, a tightened obligation.
2. **Minor.** Additive only, and the meaning of additive depends on the side. For a consumer, a new method on the service. For a provider or a contribution, a new optional capability discovered through `Capabilities()` or a separate optional interface, never a new required method, because that breaks every implementer at compile time. On the wire, new optional fields and new calls; a receiver ignores what it does not know.
3. **Suite.** A suite revision inside a minor may add tests for new optional capabilities only. Tightening an existing obligation is a major, or is free while the definition is `v0`.
4. **Compatibility of a minor.** A plugin built and attested against `1.3` runs on any distribution that serves `1.y` with `y >= 3`.

### 5.3 The support window for an old major

- **Promise.** When `v<N+1>` of a stable definition first ships in a release, the release distribution keeps serving `v<N>` for at least 12 months and at least two further minor releases, whichever is longer. It is removed only in a platform major release. The promise covers N and N-1; an older major is not promised.
- **What "served" means.** The release module table holds a row that provides the old key. For a singular key that row is an adapter that injects `v<N+1>` and provides `v<N>`; for a registry, the dispatcher provides both registry keys and adapts old contributions. For a provider-bound key the adapter is in the allowed set of the old key. The suite of `v<N>` runs in CI against the adapter for the whole window: that test is the definition of "served".
- **Announcement.** `definition.yaml` of the old major gains `deprecated: {since: <release>, removeNotBefore: <date>, successor: v<N+1>}`. Every release's notes list the served majors with their earliest removal date. `plugin check`, the boot log and `plugin list` print a deprecation warning per row.
- **After removal.** The adapter row leaves the release module table. The frozen package stays in `sdk/def`, marked deprecated, so no Go module needs a major bump for a removal and no import path ever changes. A self-hoster who needs the old major longer may keep the last adapter in a self-built distribution, unsupported.
- **The author API.** Two kernels cannot share one context tree, so a `pluginApi` major cannot be served beside its predecessor. For in-process plugins the window is therefore a notice period of the same length, and the break needs an ADR and a platform major. In-process plugins are rebuilt for every release anyway; what the promise protects is their source.
- **Early withdrawal.** Only for a vulnerability that cannot be fixed inside the old contract, by ADR, with a security advisory, and the index marks the affected plugins.
- **`v0`.** No window. A `v0` definition may break at any minor release; the break is listed in the release notes with a migration note, and the standing rule of 3.3 fixes `plugins/` and `contrib/` in the same change.
- **The numbers** (12 months, two minors) are initial values, reviewed at the CR1 review when the release rhythm is known. Prior art is in section 14: Kubernetes ties the lifetime of an API version to its stability level, and the Terraform plugin protocol keeps two majors in service with additive minors.

### 5.4 What replaces "one SDK semver"

`engines.aiops` is removed. A single range forces every plugin to be released again at every SDK major even when no definition it uses changed, which in a marketplace is every publisher at every release. Nothing hand-written replaces it; the constraint is generated.

```yaml
# plugin.yaml, generated half (plugingen from the Spec and the SDK it was built with; aiops-sdk manifest in Python)
requires:
  pluginApi: "1.2"          # in-process only
  remoteProtocol: [1]       # remote only: the majors the plugin's SDK speaks
  definitions:
    notifiers: "1.3"        # needs >= 1.3 and < 2
    secrets: "1.0"

# distribution manifest, generated from the module table and signed with it
edition: community
serves:
  pluginApi: "1.4"
  remoteProtocol: [1]
  definitions:
    notifiers: [{version: "1.5"}, {version: "2.0"}]
    secrets:   [{version: "1.0"}]
```

| Gate check | Now reads | Denies when |
|---|---|---|
| 1 (was `engines.aiops`) | `manifestVersion`, `requires.pluginApi` | The manifest major is unknown; `requires` is absent; the `pluginApi` major differs or the minor is above the served one |
| 2 Definition versions | `requires.definitions` against `serves.definitions` | A major is not served, or the needed minor is above the served one. The dump names the definition and what is served |
| 3 Remote protocol | The handshake (section 6) | No common major; the manifest digest differs from the lock; a registration names a definition version that is not served |
| 10 Attestation | The signed attestation: subject digest, and per definition the major, minor and suite revision that passed | It is missing, names another subject, or does not cover a (definition, major) the plugin uses |

An allowed row that uses a deprecated major gets a warning, never a denial. Exemptions keep their dsh shape and pair an exact `id@version` with exact served versions; they cover the minor comparison of checks 1 and 2, and check 4. An unserved major cannot be exempted, because there is no provider to call. The gate still reads manifests only and runs at the same six points. What it cannot see is a behaviour change inside a minor; that is the job of re-certification (section 8.2).

### 5.5 Go packaging

`sdk/def` stays one Go module, as record §5 decided. Its module version is a release train number. A removed definition major is never deleted from it (5.3), so the import-compatibility rule of Go modules holds without a module major bump. One Go module per definition would let the toolchain enforce the same policy, at the price of some forty modules to tag; it is not needed for the policy, and a package can later be carved into a nested module without changing its import path. `ponytail:` one module; split when a definition needs a release rhythm of its own.

### 5.6 `v0` and promotion to `v1`

A definition is `v0` when it lands. It is promoted by a decision at a release review when all of these hold: one release has shipped with it; for a `contrib` definition, its remote adapter has passed the suite (from S2b); for a definition that faces an observability or operations backend, a second real provider or contribution for a different backend family has passed the suite. The third condition serves owner decision 7: an interface that only the default stack has implemented may carry that stack's assumptions, and freezing it would make the second vendor pay for them. Which definitions are backend-facing is listed by the backend-neutral definitions design of this round.

Consequence, said plainly: at CR1 almost every definition is `v0`, the platform is `0.1.0`, and the window of 5.3 has nothing to protect yet. What exists from S2a is the mechanism. Platform `1.0` is the release in which `pluginApi`, the manifest schema and the remote protocol are declared stable; that is a release review decision, not a plan item.

Platform version rule: a major release removes at least one served major; a minor release may add definitions, minors and new majors; a patch release changes no contract.

---

## 6. Remote protocol version and handshake (input to ADR-041)

Decided now, built at S2b.

1. **The major is the proto package.** `aiops.remote.v1` holds activation, registration, calls and callbacks. A breaking change is `aiops.remote.v2`, served beside `v1`. Inside a major, changes are additive, and both sides ignore unknown fields, calls and feature names.
2. **One frozen handshake.** The first message on every connection is in a separate package, `aiops.remote.handshake`, whose shape never changes across majors: the list of protocol majors the sender speaks, the sender's kind (`kernel` or `plugin`), and a string map for diagnostics (SDK name and version, release). The kernel dials and sends its list; the plugin answers with its own; the kernel picks the highest common major and says so. With no common major both sides log the two lists and the kernel closes the stream. This is gate check 3.
3. **What the first message of the chosen major carries**, from `v1` on: the plugin id, version and manifest digest (compared with `plugins.lock`); the activation id (replica id, epoch); the definitions the plugin will register with, each as `name@major.minor`; a list of feature names, of which unknown ones are ignored. The tenant and the row come from the connection certificate, never from a field (record §11).
4. **Definition versions on the wire.** A registration names `definition@major.minor`, and the adapter package is `aiops.def.<name>.v<N>`. The dispatcher accepts a registration when it serves that major with at least that minor.
5. **Window.** The kernel serves the current and the previous protocol major under the rule of 5.3. An SDK may speak one major or several.
6. **Direction.** A newer kernel with an older plugin works inside the window. An older kernel with a newer plugin works only if the plugin's SDK still speaks a major the kernel has and needs no minor above what the kernel serves; otherwise the handshake refuses and names the reason.

The callback set, deadlines for remote gate listeners, the reconnect budget and the instance identity on a single-tenant self-host stay with ADR-041, which is now provisional on paper at S2a, ratified at S2b (the callback set, the policy callbacks included) and at S4b (one tenant per connection, activations across replicas).

---

## 7. Manifest schema v1: reserved fields and their validation

The hand-written half of `plugin.yaml`. Fields new in this design are marked.

```yaml
manifestVersion: "1.0"            # new
id: acme/signal-ingest-foo
version: 0.3.1
title: Foo alert ingest
description: Receives Foo webhooks as alert signals.   # new
license: Apache-2.0
publisher: {id: acme, name: Acme Ltd, url: https://acme.example}   # new
homepage: https://acme.example/aiops-foo               # new
source: https://git.example/acme/aiops-foo             # new
support: {url: https://git.example/acme/aiops-foo/issues, email: plugins@acme.example}   # new
edition: community                # new
commercial: {entitlement: ""}     # new, reserved
stability: experimental
scope: process
runtimes: [inproc]
capabilities: {network: [api.foo.example:443], secrets: [apiKey]}
```

| Field | Rule checked by `plugin check`, the gate and the index CI | At CR1 | Used later by |
|---|---|---|---|
| `manifestVersion` | Required. A known major. A reader that knows `1.1` ignores fields added in `1.2`; `plugin check` of the matching SDK is strict and fails on an unknown field | `"1.0"` | Schema evolution without a flag day |
| `publisher.id` | Required. `[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?`. Equal to the owner segment of `id`. `aiops` and `contrib` are reserved to artifacts signed by a release key: the builder refuses another module that declares them, and `trust.yaml` limits each signer to id patterns | Filled | The marketplace's publisher record |
| `publisher.name`, `publisher.url` | Optional. A string of at most 80 characters; an `https` URL | Optional | Listing |
| Publisher verification | Not a manifest field. A publisher cannot declare itself verified; an index states it (section 8.1) | Absent | Enterprise |
| `license` | Required SPDX expression (unchanged). `LicenseRef-<idstring>` is valid syntax and then needs `licenseUrl` or a licence file in the artifact. In-process code stays under gate check 6 and 08 L16, on detected licences | SPDX list ids only in `aiops`, `aiops-contrib` and the public index | A commercial licence text |
| `homepage`, `source` | Optional `https` URLs. The public index requires `source` | Optional | Listing, review |
| `support` | Optional. At least one of `url` and `email` when present. The public index requires it, because re-certification must be able to reach someone | Optional | A paid support offer |
| `edition` | `community` (default) or `enterprise`: the lowest platform edition the plugin runs on. Check 11 denies `enterprise` on a distribution whose signed manifest says `edition: community`, with a message that says so | `community` | First-party enterprise plugins |
| `commercial.entitlement` | An opaque reference, empty or an absolute URI. Check 11 denies a non-empty value unless a valid entitlement record from an issuer listed in `trust.yaml` is present. CR1 ships no verifier and an empty issuer list, so a non-empty value always denies | Empty; must be empty in `aiops`, `aiops-contrib` and the public index | Paid plugins |
| Price | Never a manifest field. A price changes without a new plugin version and differs per marketplace, so it lives in a marketplace's listing, keyed by plugin id | | |
| `runtimes` | `inproc`, `remote`, `wasm`. `remote` is accepted from S2b; `wasm` validates and the gate denies it | `inproc` | |

`edition` is a label for an honest refusal, not a lock: a self-hoster can build a distribution that says anything. What stops unpaid use of a paid plugin is the entitlement record, and what stops a missing enterprise service is that its key has no provider.

---

## 8. Index, re-certification and third-party indexes

### 8.1 The public index (format v1)

A public Git repository, external to the kernel (`ext-plugin-index`). One file per plugin, written by pull request. CI composes one document, signs it with a detached signature, and publishes both. `aiopsctl` verifies the signature before it parses the document. The index is trusted for discovery only: an artifact is still verified by its own signature against `trust.yaml`, locked by digest and admitted by the gate.

```yaml
indexVersion: "1.0"
name: community
serial: 412                      # rises by one per publish; a client refuses a lower serial than it has seen
expires: 2026-12-15              # a client warns on an expired document
plugins:
  - id: acme/signal-ingest-foo
    publisher: {id: acme, name: Acme Ltd, keys: ["<key id>"], verified: false}
    versions:
      - version: 0.3.1
        artifact: oci://registry.example/acme/signal-ingest-foo@sha256:...
        manifestDigest: sha256:...
        runtimes: [inproc]
        license: Apache-2.0
        edition: community
        requires: {pluginApi: "1.2", definitions: {signals: "0.4"}}
        attestations: [{type: conformance, digest: sha256:...}]
        compat: [{release: 0.1.0, status: certified, suite: signals@0.4+r3, checked: 2026-11-30}]
        yanked: false
```

Rules of the public index during the community phase: `commercial.entitlement` is empty; `license` is an SPDX list id; `source` and `support` are present; the first pull request of a publisher records its signing key, and a later change of keys needs a maintainer's review; `publisher.verified` is always `false`, because nobody verifies yet. No index is configured by default (record §11 stands): `aiopsctl index add community` adds the entry whose URL and key are pinned in the binary, and no command but `index` and `plugin search|install <id>@<version>` contacts it. `ponytail:` a serial and an expiry, not a full update framework; adopt one if an index ever becomes a source of trust for install and not only for discovery.

### 8.2 Re-certification when definitions change

A CI job of the index repository. It answers the question the gate cannot: does a listed plugin still behave on the new release.

| When | What runs | Result |
|---|---|---|
| A pull request to the index | Entry schema; the artifact signature against the publisher's recorded key; the attestation subject; the gate, on manifests only, against the `serves` table of the current and the previous minor release | The pull request is green or names the unmet check |
| Every release candidate and every release | For the latest version of every listed plugin: the gate against the candidate's `serves`; then `aiopsctl plugin verify <spec> --against <candidate>`, which runs the conformance suites of the candidate. For an in-process plugin that is a throwaway distribution built from the candidate's `dist.yaml` plus the module; for a remote plugin (from S2b) the pulled image | One `compat` entry per (plugin version, release): `certified`, `compatible-unverified` (the gate passes; the suite could not run, for example it needs a credential), `incompatible` (names the check), `failed` (names the obligation) |
| A definition gains a major, or a major passes its removal date | The same job, plus a list of the plugins that still use the old major | An issue per plugin, addressed to its `support` contact |

- **It guards our promise, not only theirs.** A plugin that uses only stable definitions, was `certified` on the previous release and is `failed` or `incompatible` on the candidate blocks the release until the cause is explained: either we broke a contract, or the plugin relied on something outside it and the report says what. On `v0` definitions a failure is information and opens an issue.
- **Third-party code runs isolated.** The verify step runs in a throwaway runner with no repository token, no secret and no network beyond a module proxy and the registry. It uploads a result file. A second job, which runs no third-party code, composes and signs the document.
- **Output.** The job writes the composed document, never the plugin files, so it opens no pull requests against publishers. The report is a release asset.
- **Client side.** `plugin search` and `plugin install <id>@<version>` show the status for the running release and refuse `incompatible` and `failed` by default; the local gate decides in any case.

### 8.3 What a third-party index or marketplace needs from us

| Need | What we publish | From |
|---|---|---|
| The index format | The `indexVersion` 1 schema and `aiopsctl index compose`, `sign` and `verify`, the commands our own CI uses | S6a |
| The artifact format | The OCI layout and media types of `plugin pack`; the attestation as a typed statement whose type is a URI of ours. Whether cosign attaches a predicate type of our own directly is UNVERIFIED and goes to ADR-020 | S2a |
| A way to compute compatibility offline | Each release publishes its `dist.yaml` with the `serves` table; `aiopsctl plugin gate <spec> --against <dist.yaml>` runs the gate with no kernel | S2a |
| A way to certify | The conformance suites inside `aiopsctl`, and `plugin verify` | S2a; remote from S2b |
| Trust roots | `trust.yaml` keeps `signers` (key, id patterns, runtimes), `distributions` (keys) and `allowUnsigned`, and gains `indexes` (name, URL, key, the id patterns it may list) and `entitlementIssuers` (reserved, empty). An index can never list an id outside its patterns, so a third-party index cannot shadow `aiops/*` or `contrib/*`. When two configured indexes list one id, `install` needs `--index <name>`; a private index never falls through to a public one | S2a, S6a |
| Their own review or verification marks | A site may require more attestation types per signer or per index: `requireAttestations: [conformance]` today, with `review` or any type of theirs later. The list is configuration, and check 10 already reads typed attestations | S2a |
| Paid plugins | The entitlement record format and its verifier, when the enterprise phase designs them. The proposal is that both are open, like signatures, so that a third party's issuer key in `entitlementIssuers` works on any edition | Enterprise |
| A name to use | Whether a third party may say "compatible with" or "certified for" this project, and under which mark policy, is an owner decision (O8) | Open |

What a third party does not get: our signing keys, a place in our index without a pull request, or any code path in the kernel that calls their service. The kernel never contacts an index or a marketplace; `aiopsctl` does, on a command.

---

## 9. Deferred to enterprise, and the seam each attaches to

"Seam" is something that exists in the community code at CR1. The last column says what changes there when the item arrives.

| Deferred item | Seam at CR1 | What arrives | Change to community plugins and definitions |
|---|---|---|---|
| Entitlement for paid plugins | `commercial.entitlement` in the manifest; gate check 11, which fails closed; `entitlementIssuers` in `trust.yaml` | A verifier for a signed entitlement record, read offline by the gate like a signature, and an issuer service | None. The reserved check gains its body in `boot`; no manifest, plugin or definition changes |
| Billing for plugin sales | Not in the kernel at all: a sale is a marketplace record keyed by plugin id and publisher id | The marketplace service | None |
| Usage metering | G13: every dispatcher call and every `plugin-manager` operation is an outbox event with tenant and row; metrics carry `plugin` and `tenant` labels | A meter row that subscribes (the planned `billing-meter` of the hosted bundle is the pattern) | None |
| Publisher verification | `publisher.id` equal to the id owner; signers limited to id patterns; `publisher.verified` in the index document | A marketplace verifies identity and signs entries that say so | None |
| Review and curation | Typed attestations; `requireAttestations` in `trust.yaml` | A `review` attestation type and a pipeline that issues it | None; the list is site configuration |
| Marketplace UI | Index format v1; `plugin search` and `plugin inspect` | A web application that reads index documents | None |
| Ratings, download counts, featured lists | The index is static and keyed by id and version | Marketplace data beside it | None |
| WebAssembly | `wasm` in `runtimes`; the proxy-row pattern of `remote` | A host row, on its 00 §7.3 trigger | None |
| Third-party plugins on hosted | Gate check 9; `sidecars`; W4 | S2b, S4b, S6b and the hosted bundle | None |
| First-party enterprise plugins | The builder and `dist.yaml`; `edition` in both manifests | An enterprise distribution: the release plus enterprise modules (section 10.3) | None |
| Takedown of a published version | `yanked` in the index; the lock by digest | `quarantine` and `revoke-version` of S6b | None |

One thing is deliberately not prepared: a plugin-side licence check. A paid plugin that wants to verify its own licence does so through its declared `capabilities.network`, as any program would, and the kernel offers no service for it.

---

## 10. Community and enterprise: a proposal for the owner

### 10.1 The principle

Proposal, as the owner suggested it: **everything a single organisation needs to self-host is community; what serves selling to and operating for many organisations is enterprise.** A test question for any feature: would one platform team, running the product for its own company, miss it? If yes, community.

### 10.2 Examples under the principle

| # | Feature | Proposed side | Reason, and where it is not obvious |
|---|---|---|---|
| | Kernel, loader, SDK, every definition with fake and suite, the builder, the gate, signing, `aiopsctl`, the index format and the public index | Community | The contract every plugin and every third-party marketplace builds on |
| | Agent, telemetry base, incident loop, chat-ops, on-call, approvals, the ledger, the audit chain and its export, the lab | Community | The product of owner decision 4 |
| | Single sign-on through OIDC, permission checks, audit export | Community | One organisation needs them. Many open-core products charge for these; the principle says not to |
| | The pillars of P2 and P3 (SecOps, FinOps, Continuous Improvement, Executive Reporting) | Community | They serve one organisation. Executive Reporting is the usual candidate for a paid tier; the owner may decide otherwise |
| | Several kernel replicas, generations with a canary (S4b) | Community | High availability for one organisation |
| | The remote runtime and the Python SDK (S2b) | Community | A contributor needs them |
| | A private index for one organisation's own plugins | Community | Needed by one organisation, and by any third-party marketplace |
| | Connectors for commercial vendors, contributed or first-party | Community | Owner decision 7 |
| 1 | More than one tenant on a self-host (teams or environments of one organisation); the tenancy mechanism of S4b | Community for the mechanism; open for the tenant lifecycle API | Isolation code must be in the core to be tested by every plugin. Whether creating tenants is community is the first real boundary question |
| 2 | The `hosted` profile and the rows of `aiops-hosted`: cohorts, meters, dashboard pools, per-tenant limits, the distributed chat app, per-tenant upstream instances | Enterprise by the principle | They exist to operate for many organisations. Today 00 ships `hosted` in the Apache-2.0 repository, so this is a change the owner must make in words |
| 3 | A key-management provider such as `kek-kms` | Community | One organisation with a cloud KMS needs it; it is in the hosted bundle today only because hosted enables it |
| | The marketplace service: listings, checkout, payouts, publisher verification, the review pipeline, the UI | Enterprise | Owner decisions 1 and 2 |
| | The entitlement issuer; plugins we sell | Enterprise | |
| | Third-party plugins on hosted; features for a provider that manages many customers (cross-organisation views, white label) | Enterprise | Operating for many organisations |
| | Paid support and service levels | Enterprise | Not software |

### 10.3 Licence implications, as open owner decisions

| # | Question | Options | What this design assumes meanwhile |
|---|---|---|---|
| L-a | The core licence | Apache-2.0, decided (00 §1). Open part: a written promise that nothing in the community edition at a release later moves to enterprise | Apache-2.0; no promise text yet |
| L-b | Where enterprise plugins live | (1) A separate private repository; the enterprise edition is a distribution built by the builder from the release plus enterprise modules. (2) An `ee/` directory in this repository under another licence. (3) Remote plugins only | Proposal: (1). The public repository stays purely Apache-2.0, the L7 allow-list and 08 L9 keep their plain meaning, and the builder is the seam that already exists (G9). (3) alone cannot work, because a remote plugin cannot provide a key |
| L-c | Contributor agreement | A Developer Certificate of Origin sign-off, or a contributor licence agreement | Proposal: the sign-off. Apache-2.0 already lets an enterprise edition include community code, so an agreement is needed only to keep the option of relicensing the core itself. It must be decided before the first external pull request is merged, because collecting agreements afterwards is the migration owner decision 5 wants to avoid |
| L-d | Contributed plugins and the paid marketplace | Plugins under `contrib/` stay free in every edition; or no commitment | Proposal: they stay free. The project earns from third-party paid plugins, not from reselling contributions |
| L-e | Licences in the public index | Open licences only during the community phase; or commercial entries too | Open licences only (8.1). A third-party index may list anything |
| L-f | The hosted bundle's licence, if row 2 of 10.2 goes to enterprise | Stays Apache-2.0 in this repository although enterprise in name; or moves under L-b | None: the hosted rows are not built before S9b |
| L-g | The project's own name and marks | A mark policy for third-party marketplaces and for the word "certified" | None |

Two facts bear on L-c and are sourced in section 14: CNCF requires the sign-off for incoming code and leaves an agreement to each project; and one widely used infrastructure vendor that changed its products to a source-available licence in 2023 kept its APIs, SDKs and most libraries under the previous licence. That an agreement was what made that change possible is UNVERIFIED here.

---

## 11. Contributor experience in the community phase

### 11.1 Where plugins live

| Tier | Source | Id owner | Enters a binary by | Who checks it | Who keeps it working |
|---|---|---|---|---|---|
| First-party | `plugins/<name>/` | `aiops` | The release distribution `aiops` | Maintainers | Maintainers |
| Contrib | `contrib/<name>/` in this repository, each plugin its own Go module that requires the SDK only | `contrib` | The distribution `aiops-contrib`, defined by `dist/contrib.yaml`; rows disabled by default and never in a required set | Maintainers review; our CI runs its suites on every commit | Its named code owner for behaviour; maintainers for build and suite (11.5) |
| Indexed | The author's repository | The author's publisher id | A distribution the site builds; from S2b a remote install | Automated checks of the index only (8.2) | The author |
| Unlisted | Anywhere | Any | An OCI reference the site trusts | The site | The author |

- **Why `contrib/` is in this repository.** While definitions are `v0` they change, and the standing rule fixes contrib plugins in the same pull request as the change. One repository makes that one change. Each contrib plugin is still its own Go module with no import of `plugins/` or of the root module, and it is built through the builder, so it stays a proof that a plugin needs no kernel.
- **When it moves out.** `ponytail:` one repository; move `contrib/` to a repository of its own when its CI time passes that of the core, or when a code owner needs merge rights that the core must not grant.
- **Why the owner segment is `contrib`.** A row names its plugin by id. An id that carried the contributor's name would change when the plugin changes hands, and every profile that names it would break. The code owner is recorded in the repository's owner file and in `support`.
- **Which tier for a connector to another vendor.** Either. Contrib when the author wants it shipped in an image people can pull; indexed when the author wants to keep the release rhythm.

### 11.2 The template

`aiopsctl plugin new <definition> --lang go` writes a complete plugin for that registry, from a template compiled into `aiopsctl` (its source is `templates/plugin-go/`). The Python template arrives with S2b.

| File | Content |
|---|---|
| `plugin.go` | The `Spec`: id, `Inject` of `secrets` and `dialer`, the registration with the chosen definition, a config struct with schema tags, `Apply` |
| `internal/client.go`, `internal/<definition>.go` | A client for the vendor's API built on the injected dialer; one stub per method of the definition, each returning "not implemented" |
| `plugin_test.go` | `conformance.Run` for the definition, and one solo test on `kerneltest` with fakes |
| `zz_cycle_test.go`, `zz_tenants_test.go` | Generated: the cycle test and the two-tenant test |
| `fixtures/` | Recorded vendor responses; tests replay by default |
| `plugin.yaml`, `bundle.yaml` | The hand-written manifest half with `publisher`, `license` and `support` to fill in; one row, disabled |
| `README.md`, `LICENSE`, `go.mod`, a CI workflow | What it connects to, its config, the permissions it needs on the vendor side; Apache-2.0; the SDK and the test kit only; one job that runs the check |

A new plugin compiles and passes T0 and the generated tests at once. Its conformance run fails with one line per obligation of the definition: that list is the contributor's work list. CI of this repository generates a plugin from the template for every registry definition and asserts exactly that state, so a template can never fall behind a definition.

### 11.3 The one check command

`aiopsctl plugin check ./` before a pull request. It runs T0, T1 and T2 in every runtime the manifest lists, the effects check, manifest parity (with `--write` it regenerates the generated half, `requires` included), and pack-and-verify with a throwaway key; it prints the attestation it would sign.

| Property | Requirement |
|---|---|
| Needs | The SDK module, `aiopsctl`, a Go toolchain. No kernel checkout, no container runtime, no network after the first module download. A plugin with its own schema also needs PostgreSQL, which the test kit downloads or finds in a container |
| Output | One line per failed obligation, with the obligation id and a link to its text; deprecation warnings for definition majors in their window; exit code 0 only when the pull-request CI would pass T0 to T2 |
| Not included | `aiopsctl plugin smoke ./ --against <release>` (T3) needs a container runtime. Our CI runs it for `contrib/`; an outside author runs it when they can |

Inside this repository `make check` does the same for changed plugins.

### 11.4 The review path for a contributed plugin

1. **Fit.** If the plugin contributes to an existing registry, go to step 2. If it needs a new definition or a change to one, that is a core change with its own short design note: a definition needs a consumer, a fake and a suite, and nobody adds one for a single plugin.
2. **Write.** `plugin new`, the author's code, fixtures recorded from the vendor, `plugin check` green.
3. **Pull request.** It adds `contrib/<name>/` and one line in `dist/contrib.yaml`. CI runs `plugin check`, `plugin smoke` against the main branch, the licence scan of the builder, the name lint and the sign-off check.
4. **Review.** A maintainer checks a fixed list and nothing else:

| # | Check |
|---|---|
| 1 | Imports are the SDK and the plugin's own dependencies, all inside the L7 allow-list; the licence is Apache-2.0 |
| 2 | Every outbound call uses the injected dialer, and `capabilities.network` lists the hosts it reaches |
| 3 | Credentials come only from `secrets`; none is logged or put in a fixture |
| 4 | The plugin writes nothing to the vendor unless its definition is `executors`, in which case 00 §2.2 applies in full |
| 5 | Fixtures hold no customer data, and the tests pass with the network off |
| 6 | The README states the vendor-side permissions and the vendor versions the fixtures were recorded against |
| 7 | The id follows 00 §8.1 and names the vendor's product only to say what it connects to; an attribution the vendor's mark policy asks for is in the README. Each vendor's policy is the contributor's to check and is UNVERIFIED by us |
| 8 | A code owner is named and accepts the terms of 11.5 |
| 9 | `stability: experimental` |

5. **Merge.** The next `aiops-contrib` image holds the plugin with its row disabled. An index entry is generated for it.
6. **Promotion.** `experimental` becomes `stable` after one release in `contrib/` with no open breaking defect and one recorded live run by the code owner against a named vendor version. We cannot run most vendors ourselves, and the stability label says so.

For the indexed tier there is no review: the author opens a pull request to the index, and the checks of 8.2 decide. The entry is shown as not reviewed by the maintainers.

No review time is promised here, because no maintainer group has been named (O7).

### 11.5 What a maintainer promises about compatibility

1. A stable definition is never broken inside a major. An old major is served for at least 12 months and two further minor releases after its successor ships, and is removed only in a platform major release, with the date in every release's notes.
2. A `v0` definition may break at a minor release. Each break is in the release notes with a migration note.
3. Whoever breaks a definition fixes every plugin under `plugins/` and `contrib/` in the same pull request. A contrib plugin therefore always builds and passes its suite on the main branch.
4. A suite never tightens an obligation of a stable definition inside a major.
5. The author API, the manifest schema and the index format change additively inside a major. The remote protocol is served in its current and previous major.
6. Every release candidate is run against the public index, and a regression of a certified plugin on stable definitions blocks the release (8.2).
7. An early withdrawal happens only for a vulnerability, by ADR and advisory.
8. A contrib row is disabled by default, is never in a required set, and is never needed by a first-party row.
9. What is not promised: that a vendor's API stays as the fixtures recorded it. That is the code owner's part. A contrib plugin whose code owner has not answered a failing live report for two releases is marked unmaintained in the index and leaves `aiops-contrib` at the next release; its source stays under `contrib/`.

Also not promised: performance, the behaviour of first-party rows beyond their definitions, and golden dumps.

---

## 12. Amendments

### 12.1 `docs/architecture/00-decisions.md`, by section

| Section | Amendment |
|---|---|
| Header | Status amended 2026-11-11 for the community-first order and marketplace readiness; this design as an input |
| §1 | Add an editions sentence: phase 1 is the community edition; the marketplace with billing is a later enterprise feature and is not built; the boundary is an open owner decision (§9) |
| §2.1 row 1 | Runtimes: in-process Go is proven at S2a; the remote runtime and the Python SDK ship at S2b, after the first community release; the proto and message types of a `contrib` definition are still written with the definition. Status gains "ADR-040 decided" |
| §2.3 | Plugin record points settled: the split of S2, S4, S6 and S9; `engines.aiops` removed; `composer` not a base row of a single-process profile |
| §2.4 | W1 is replaced by the owner decision of 2026-11-11: the architecture is kept and the order is S0, S1, S2a, S3, S4a, S5, then S6a, S7, S8 to the first community release, then S2b, then the parts only hosted or the marketplace needs. W2, W3 and W4 stay |
| §3.1, §3.3 | `aiops-contrib` exists from S6a and `remote-plugin` from S2b; `ext-plugin-index` is first read at S6a; the generation object from S4b |
| §4.1 | Definition: a `major.minor` version in `definition.yaml`; the promotion rule from `v0` to `v1` (three conditions). Plugin: the hand-written manifest fields lose `engines.aiops` and gain `manifestVersion`, `description`, `publisher`, `homepage`, `source`, `support`, `edition`, `commercial`; the generated fields gain `requires`. Remote column: adapters run from S2b |
| §4.3 | The versioning bullet is replaced by section 5 of this design. The standing rule gains `definition.yaml` and the same-change fix of `plugins/` and `contrib/` |
| §4.4 | `aiops-core` is 8 rows at CR1; `remote` joins at S2b, `plugin-manager` and `plugin-index` at S6b. `composer` joins `aiops-base` at S4b; `tenant-lifecycle` and `admin-api` ship at S4a with the surface of section 3.2. The row count of `self-host-full` at CR1 is restated |
| §4.5 | The contributor paragraph: the template, `plugin check` without a container runtime, `contrib/`, the index re-certification; the two-tenant test is required from S2a |
| §6.1 | The ten gate checks become eleven: 1 reads `manifestVersion` and `pluginApi`, 2 reads definition versions, 10 reads typed attestations, 11 is edition and entitlement. Exemptions cover the minor comparison of 1 and 2, and check 4. Generations: on one process `reload` commits or restores; `composer` owns the state machine from S4b. Required set: `composer` only in a profile that runs more than one replica. `trust.yaml` gains `indexes` and the reserved `entitlementIssuers` |
| §6.3 | `self-host-full` and `dev` are the profiles of the first community release; `hosted` follows S9b; `aiops-only` per O2 |
| §7.1 | The exit test gains legs C and D of section 4.3, and leg B if O2 is accepted. "A Python tool installed as a remote plugin" leaves the S8 exit |
| §7.2 | The phase table is replaced by section 3.3: P0 Model core (S0, S1, S2a, S3, S4a, S5), P1 Community release (S6a, S7, S8), P1 follow-up (S2b), P2 in two tracks (S9a; S4b, S6b, S9b), P3 (S10), Enterprise (not planned). The sentence "product work starts after the model is proven" is replaced. Standing rules date from S2a |
| §7.3 | The per-definition version range leaves the deferred list. New rows with triggers: the remote runtime (S2b, after CR1), the marketplace service and each item of section 9 (the enterprise phase, by owner decision), one Go module per definition (a definition needs its own release rhythm), a separate `contrib` repository (11.1), a full update framework for the index (8.1) |
| §8.1, §8.2 | Code paths gain `contrib/<name>/`, `templates/` and `definition.yaml`; the id owners `aiops` and `contrib` are reserved. Glossary gains edition, community release, publisher, index, re-certification, support window, `requires` and `serves` |
| §9 | ADR-040 decided (this design). Decide-by values: ADR-020 and ADR-044 at S2a; ADR-041 provisional at S2a, ratified at S2b and S4b; ADR-036 (bench at S0, cost model at S4b) and ADR-038 at S4b; ADR-043 and ADR-046 at S5 unchanged. Four new open ADRs, numbered at reconciliation: the community and enterprise boundary with the home of enterprise plugins (owner); the contributor agreement (owner, before the first external pull request); index format v1 with trust roots and re-certification (S6a); manifest schema v1 with the reserved fields and the entitlement check (S2a) |

### 12.2 The plugin record (`docs/design/plugin-system-2026-11.md`), by section

| Section | Amendment |
|---|---|
| Status, §1 | Amended 2026-11-11: the order of work is community first; the architecture is unchanged |
| §2 | Q11: both runtimes are built, in-process proven at S2a and remote at S2b. Q16: "model first (S0 to S6), then product" becomes the rule of section 3.1 of this design |
| §4 | The manifest paragraph: fields as section 7; `requires` generated |
| §5 | The versioning bullet is replaced by section 5; every definition has a `definition.yaml`; a `contrib` definition's types come from its proto (G7) |
| §9 | Generations: the single-process form (S4a) and the composer form (S4b) are stated separately; layer 5 arrives with S4b |
| §10 | The single pinned tree of S4a; trees on first use, the cold lookup and the budget are S4b |
| §11 | The runtime table gains "from S2a" and "from S2b"; publishing and install rows for remote artifacts are S2b; the index paragraph points to section 8; reserved id owners |
| §12 | The manager's operations split into `aiopsctl` operations (S6a) and the service row (S6b); gate checks 1, 2, 3 and 10 reworded and 11 added; exemptions narrowed |
| §13 | `composer` is required only with more than one replica; the tenant-tree column of the matrix is S4b |
| §14 | The template, the check requirements of 11.3, `contrib/`, re-certification |
| §15 | Row 34: no single engine range; generated per-definition requirements, an absent `requires` denies. Row 41: the index has a format version, a serial and an expiry |
| §16 | Replaced by section 3.3; the S2, S4 and S6 exits are divided as in 3.2 |
| §17 | The amendment list of 12.1 is added to it |
| §18 | ADR-033: the first owner statement is replaced by the 2026-11-11 decision. ADR-040 decided. ADR-041 timing. The new ADRs |

### 12.3 The rest of the set

| File | What must change |
|---|---|
| `08` | §2: the roadmap tables and the figure `08-roadmap-steps` redrawn for the new order and verified as rendered; §2.1 and §2.2 regrouped. §1.5 and §1.6: gate checks and the manager split. §4: `contrib/`, `templates/`, `dist/contrib.yaml`; the server-images row dates `aiops-contrib` from S6a. §5.1: L16 names `contrib/` (Apache-2.0, sign-off); new rows for the contributor agreement and for the home of enterprise plugins, open until the owner decides |
| `07` | §2 manifest and §4.1 versioning rewritten from sections 5 and 7; §5 the handshake of section 6; §8.3 the template and the check; the Python SDK is S2b |
| `01`, `02` | The contributor's flows E1 to E3 gain the template and `contrib/`; remote plugin containers are dated S2b; deployment views of CR1 show one kernel process |
| `03` | `composer`, `remote`, `plugin-manager` and `plugin-index` are marked with their step; `tenant-lifecycle` and `admin-api` with their S4a and S4b surfaces |
| `README.md` (root), `docs/architecture/README.md` | The status line names the community release; a contributor reading path that starts at the template |

---

## 13. Open items

| # | Item | Who decides | By |
|---|---|---|---|
| O1 | The community and enterprise boundary: accept the principle of 10.1, and settle rows 1 to 3 of 10.2 (tenant lifecycle on self-host, the hosted bundle, key-management providers) | Owner | Before S9b is planned; row 1 before S4b |
| O2 | `aiops-only` as a supported profile of CR1 with the reduced demo (leg B). Recommended: it lets a user try the product without replacing a telemetry stack, and it is the natural entry for owner decision 7. It adds a fixture set and documentation, no row | Owner | Before S8 starts |
| O3 | An optional preview tag of the loop on `aiops-only` and `dev` before S7 finishes. The owner's words put the agent in the community product, so this design keeps S7 before the release and only records the option | Owner | Before S7 starts |
| O4 | The remote runtime after the first release (S2b) and not before it. This design reads owner decision 5 as "the protocol rule is fixed now", not "the runtime ships in CR1". If the owner wants Python plugins on the first day, S2b moves before S8 and the release waits for one more step | Owner | Before S2a ends |
| O5 | The contributor agreement (10.3 L-c) and the promise that contributed plugins stay free (L-d) | Owner | Before the first external pull request is merged |
| O6 | Where enterprise plugins live (L-b) and whether a written community promise is published (L-a) | Owner | Before any enterprise code exists |
| O7 | Who the maintainers are, and with that a review time for `contrib/` pull requests | Owner | CR1 review |
| O8 | The project's own mark policy for third-party marketplaces and the word "certified" (L-g) | Owner | Before a third-party index is announced |
| O9 | The window numbers of 5.3 (12 months, two minor releases) | Chief architect, at the CR1 review | CR1 review |
| O10 | Which registry leg C uses, and which definitions count as backend-facing for the promotion rule of 5.6 | The backend-neutral definitions design of this round | Before S2a ends |
| O11 | Instance identity of a remote plugin on a single-tenant self-host, where no cell certificate authority exists | ADR-041 | S2b |
| O12 | Whether cosign attaches an attestation type of our own, and the entitlement record format | ADR-020; the enterprise phase | S2a; enterprise |

---

## 14. Outside facts used

All read on 2026-11-11.

| Fact | Source | Version |
|---|---|---|
| "GA API versions may be marked as deprecated, but must not be removed within a major version"; beta versions are served "9 months or 3 minor releases after deprecation (whichever is longer)"; alpha versions may be removed in any release | https://kubernetes.io/docs/reference/using-api/deprecation-policy/ (rule 4a) | Current documentation; page date not read |
| The Terraform plugin protocol has majors 5 and 6, both in service; "Major versions of the protocol delineate Terraform CLI and Terraform Plugin compatibility"; "Minor versions of the protocol are additive"; the registry uses the protocol version as compatibility metadata | https://developer.hashicorp.com/terraform/plugin/terraform-plugin-protocol | Protocol 5.10 and 6.10 |
| An SPDX licence expression may hold a user-defined reference, `"LicenseRef-"(idstring)`, with `idstring = 1*(ALPHA / DIGIT / "-" / ".")` | https://spdx.github.io/spdx-spec/v2.3.1/SPDX-license-expressions/ (annex D.1) | SPDX 2.3.1 |
| CNCF's intellectual-property policy asks for a Developer Certificate of Origin sign-off and Apache-2.0 on incoming code; a contributor licence agreement is each project's choice | https://github.com/cncf/foundation/blob/main/charter.md (section 11), read through a search summary, not the page itself | UNVERIFIED wording |
| HashiCorp announced on 2023-08-10 a change of its products from MPL 2.0 to the Business Source License 1.1; "HashiCorp APIs, SDKs, and almost all other libraries will remain MPL 2.0" | https://www.hashicorp.com/en/blog/hashicorp-adopts-business-source-license | Announcement of 2023-08-10 |
| cosign creates and verifies in-toto attestations (`cosign attest --predicate`, `cosign verify-attestation`). That it accepts a predicate type URI of our own is UNVERIFIED | https://docs.sigstore.dev/cosign/verifying/attestation/ | Current documentation |

---

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
