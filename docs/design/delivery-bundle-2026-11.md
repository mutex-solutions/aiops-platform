# Delivery bundle record, 2026-11: from a customer request to a planned, approved and verified change

Status: proposal, awaiting the project owner; changes no decision and no roadmap order. Nothing here is implemented or measured, and no step has a date. `docs/architecture/00-decisions.md` wins wherever it and this record differ. Where the design needs an accepted line changed, this record names the line and asks (section 14); it edits none.

Conventions. `00:72` is line 72 of `docs/architecture/00-decisions.md`; `01`, `03`, `05`, `06`, `07`, `08` and `09` name the other files of `docs/architecture/` by their number. `HR`, `CR` and `LR` are the harness record, the community record and the licence record under `docs/design/`. `ADR-061:1407` is a line of that ADR. The plugin record is cited by section. Line numbers are those of commit `50665fa`. `aiops-delivery` and every other new id here is a working name. Products are named only where an outside fact or a recorded licence verdict is about them; no name in this record says which systems any user of the platform runs. Licence statements are engineering analysis, not legal advice.

## 1. The requirement

A services company runs platform and delivery engineers. Each engineer serves two or three projects of different customers. Work arrives from customer-facing channels:

- meetings, which are recorded, summarised into requirements and turned into a ticket or a task input;
- chat, on two chat products;
- ticket systems, on three issue trackers.

Depending on the customer, the team opens a task and logs time in one system or another. Today an engineer then hands the input to a coding agent, one of several third-party agent harnesses, by hand.

The wanted flow has five steps:

1. Take a request from a customer-facing source.
2. A coding agent writes a plan. The agent has a configured "brain", a memory that knows which repository, environment and conventions each request maps to.
3. The engineer reads the plan, edits it and approves it.
4. The agent executes the plan. The outcome is a pull request, a deployment or another change tied to code and system context.
5. The engineer verifies the outcome with the customer and loops until it is accepted.

Engineers who develop may tune the flow: add plugins, change prompts. The engineer's job becomes facing the customer and verifying outcomes. The work is mostly operations work, with development work included.

Two questions came with it. Does this fit the platform as a plugin, or does it need a separate platform? And how does it compare with an open-source personal agent that has a message gateway over many chat channels and a persistent memory (section 12)?

## 2. A plugin bundle, not a separate platform

**Answer.** The requirement fits as one optional first-party bundle, working name `aiops-delivery`, on the definitions the platform already has in design. It needs no change to the kernel, the launcher or the loader, no new shipped profile and no change to the order of work. It does not fit everywhere as written: seven places meet an open or a closed decision, and each is a line to amend by ADR or a question for the owner, not a reason for a second platform.

**Reasons.**

1. Every step of the flow maps onto a definition that exists in design (section 3): a run of an agent harness for the plan (`00:45`), `approval.Decide` for the decision (`00:68`), the change ledger and the lease for the write (`00:69`, `00:70`), an executor whose write is a pull request (`00:62`), a verifier (`00:75`), the audit chain (`00:73`) and the tenant for the customer (`00:326`).
2. The part that is hard to build twice is the policy spine. Approval floor, change ledger, credential leases, quorum, audit chain and scrub are first-party plugins that the kernel makes impossible to shadow (principle 5, `00:28`), and five generic mechanisms keep a tuned prompt or an added plugin from weakening them (`00:259`). A separate platform would rebuild these or run without them. Section 12 shows what running without them looks like.
3. The ways in already exist or are on the path: an optional bundle of the release that an operator enables with no rebuild (`00:521`), a remote plugin in any language (`08:391`), a pack with no code (`00:507`), and a distribution the operator builds and signs for site-private in-process code (`00:107`, `08:403`). Nothing in the design falls outside the closed vocabularies of the kernel, the launcher and the loader (`00:263`).
4. A pillar is one `core:group` row that names its persona, tool set, harness selection and executors (`00:55`). Delivery work is one more group.
5. No outside product covers the whole flow (section 12), so there is no platform to adopt in place of a bundle.

**Where it does not fit as written.**

| # | The requirement meets | Source | How this record treats it |
|---|---|---|---|
| a | "The model never writes a plan"; an executor accepts only typed plans of its own schema | `00:72` | Two different things are called a plan. The work plan is an answer of a run. The executor's plan is a typed envelope built by code, with the patch named by digest. One sentence of `00:72` is asked for (D5) |
| b | A harness never writes, holds a credential or reaches the network | `00:45`, `HR:56`, `HR:58` | The first forms keep every run read-only. A run that edits a checkout is a later form behind its own ADR (D6) |
| c | A decision counts only if the subject is not the requester | `03:445` | One engineer cannot both propose and approve. The owner chooses (D3) |
| d | One tenant, `default`, until S4b; tenant create on a self-host is open | `08:720`, `08:1183` | Customer as tenant is recommended and depends on ADR-049 (D2) |
| e | A harness that calls a vendor-hosted model itself is not admitted in the community edition | `HR:83`, `00:632` | The owner confirms or revisits before ADR-051 is accepted (D7) |
| f | There is no work-item object: a signal is an alert, a change or a finding, and the incident machine is keyed on a correlation key | `00:55`, `03:199` | New: one plugin with its own record (section 4.1) |
| g | The product text counts seven pillars | `00:55`, `08:1070` | The owner chooses how an optional pillar group is named in the text (D1) |

## 3. The requirement mapped onto what exists

"Arrives" is the roadmap step that builds the named part (`08:618` to `08:635`). Nothing in code today belongs to this area: `sdk/def` holds `clock`, `commands`, `composition`, `configrefs`, `echoers`, `launch`, `loader`, `operations`, `reload` and `stampers`, and tenancy, the policy spine and the agent runtime are design text until S4a, S5 and S8a (`08:699`, `08:700`, `08:729`).

| # | Need | Existing definition, row or invariant | Source | Arrives |
|---|---|---|---|---|
| 1 | A request opened by API or CLI, with no chat product | Every step the loop gives a person is a public API operation with an `aiopsctl` command; a row outside `aiops-cli` adds its command through a companion bundle | `03:543`, `ADR-061:1407`, `00:284` | S2a (`commands`), S8b |
| 2 | A request opened from chat by an engineer | The `chat` dispatcher resolves the tenant and mints the handle; a first-party plugin registers its own verb with `chatCommands` | `06:29`, `06:165`, `06:764` | S8b |
| 3 | A request pushed by a tracker | A webhook with a per-tenant token is admitted, deduplicated and acknowledged by `signal-core`; but every signal is correlated to an entity and consumed by the incident machine, so only the transport fits | `00:48`, `01:335`, `03:196`, `03:199` | S8b; the work item is new |
| 4 | A meeting summary as a request | None. It enters as a file through row 1 | none | new, no platform part |
| 5 | The agent reads the ticket, the repository and the wiki while planning | `tools`, the vendor-neutral read seam; rows of the instantiable `tool-mcp` for a system with a Streamable HTTP MCP server and read tools; a remote Python tool | `00:233`, `07:822`, `00:113`, `08:630` | S8a; S8b |
| 6 | The "brain": which repository, environment and conventions a request maps to | Versioned per-tenant rows in the owning plugin's schema, as the tenant maps of `signal_core` are; one more bounded block on the open hook `run/first-message` | `05:425`, `09:354`, `03:220` | S3 (storage), S8a (hook) |
| 7 | Prompts the engineers tune | Persona and operating rules are content the pillar group names | `05:189`, `05:190`, `07:1327` | S8b |
| 8 | The agent writes a plan | One run through `harnesses`: a task, a read-only tool set, an answer spec as a JSON Schema, a checker on `run/answer` | `00:45`, `HR:38`, `07:461` | S8a |
| 9 | A run with no incident | `agent-runs` keeps the transcript in its own schema, so a run needs no incident | `00:45`, `03:200` | S8a |
| 10 | A coding harness for this work only | Selection order: the tenant, then the pillar group's `harness` field, then the default | `HR:101` to `HR:103` | S8a; S4b (per tenant) |
| 11 | A third-party harness out of process | Principle 6; three topologies; the role id `harness-sidecar` in no default set | `00:29`, `HR:89` to `HR:91`, `HR:181` | S2b, S8b |
| 12 | The engineer edits the plan, and an edit voids the approval | Approval binds to a digest over plan, target and inputs, so an edited plan needs a new approval | `00:72`, `06:223` | S5 |
| 13 | The engineer approves | `approval.Decide` is the one entry for every surface: API, a chat card, later the dashboard app | `03:277`, `03:442` to `03:449` | S5; surfaces S8b and P2 |
| 14 | The agent never holds the push credential | A write credential comes only from `credentials.Write(token)` at `Apply`, for one action, for minutes | `00:69`, `07:678` | S5; first real lease S8b |
| 15 | Outcome: a pull request | `remediation-executor-gitops-pr`: the write is a pull request a person merges, the controller's sync is the receipt, a revert pull request is the rollback | `00:62`, `05:736`, `01:131` | S9a item 5, after ADR-031 (`08:831`) |
| 16 | Outcome: a deployment through GitOps | The merge is the deployment; `signal-ingest-gitops-webhook` feeds the sync into the ledger | `05:736`, `00:287` | S8b, S9a |
| 17 | Outcome: a deployment by pipeline or on a host | Deferred: a host executor on the customer's automation controller, behind its own trigger and ADR | `00:617`, `08:1199` | trigger |
| 18 | Verify the outcome | Contributions to `verifiers`; a typed read-back through the executor's `Verify`; one verdict from a closed list | `00:75`, `05:600`, `05:611` to `05:614` | S5, S8b |
| 19 | Customer acceptance and the rework loop | None. Each iteration is a new digest and so a new approval | `06:223` | new |
| 20 | A customer that wants two approvers or a freeze | The tier table can only be raised per tenant; gate listeners may raise or deny, never grant | `00:68`, `03:287` | S5; listeners P2 |
| 21 | Tuning cannot weaken the floor | Mechanisms M1 to M5, proven by `test/policy-bypass` | `00:259`, `08:626` | S5 |
| 22 | Separation between customers | One tenant id is the row-level-security fence, the secret scope, the blob prefix and the audit chain; delete purges every namespace | `00:326`, `00:335`, `00:377`, `00:433` | S4a (mechanism), S4b (a second tenant) |
| 23 | One engineer on several customers | `tenant_binding(tenant, subject, role)` and `authz.Check(subject, tenant, permission)` | `00:339`, `00:224` | S4a |
| 24 | Notices: plan ready, outcome ready | `notifiers`, with email and webhook rows and the instantiable `notifier-webhook` | `00:230`, `06:93`, `07:137` | S5, S8b |
| 25 | Write back to a tracker; log time | None. Read tools refuse a write; a contributed plugin writes to a vendor only as an executor | `07:789`, `CR:374` | new (D10) |
| 26 | Evidence per customer | The audit chain is per tenant, appended in the caller's transaction, exportable | `00:73`, `03:480` | S5 |
| 27 | Cost control | One model route with a daily budget and per-run caps keyed by pillar and kind; `agents-paused` | `00:56`, `HR:41` | S8a |
| 28 | Add a plugin with no rebuild | A remote Go or Python plugin; a pack with no code | `08:391`, `08:623`, `00:507` | S2b, S6a |
| 29 | Packaging | An optional first-party bundle listed by no shipped profile, enabled with `plugin install` and `plugin enable` | `00:521`, `00:112`, `08:401` | S8b (first use of the pattern) |

## 4. What is new

The standing rules apply to each item: no definition merges without its fake, its suite, its `definition.yaml`, a provider and a consumer; no plugin merges without `aiopsctl plugin check` and a T3 test; a definition lands with its first consumer (`08:678`, `08:680`, `00:206`). The list is ordered by when each item is first needed (section 10).

### 4.1 The first build: one plugin, one command row, one group row

**`delivery`, a plugin.** In-process Go, first-party, under `plugins/`, roles api and worker. Key: none. It provides nothing, because no other plugin calls it in process: the CLI and the chat verb reach it through its routes, and a seam exists when a consumer does (`00:26`). It injects `sql`, `blob`, `jobs`, `events`, `http`, `authz`, `audit` and `harnesses`, wants `notifiers` and `chatCommands`, and from the guarded form injects `executors` and `changes`, which have open consumers (`00:227`, `00:234`). It injects neither `approval`, which is reached through `change-hub` (`03:296`), nor `credentials`, which is consumer-bound to `remediation` (`00:227`). It listens on `run/first-message` (open, `07:460`) and on `run/answer` (signed, so a first-party listener, `07:461`), for the pair (delivery, `investigate`) only. Its durable namespace is the schema `delivery` and a blob prefix: `request` (source reference, text digest, subject, state), `plan_version` (number, parent, author subject, digest, blob reference), `iteration` (run ids and change ids of one request), `mapping` (the versioned per-tenant rows of section 6.2) and `note`. With no definition it has no fake and no suite of its own; it carries the generated cycle test and two-tenant test of every process-scope plugin (`00:300`) and a T3 test. A `deliveries` definition (singular, class open, provider `delivery`, an in-memory record as the fake, a suite over the state machine and the digest rule) is written in the change that adds its first in-process consumer, and not before.

**`command-delivery`, a contribution to `commands`.** Key `commands`: a registry, class open on both sides, provider `cli`, no remote contribution (`00:222`). The definition, its fake and its suite exist since S2a (`sdk/def/commands/v0/definition.yaml`); this adds one contribution, which passes that suite. It sits in the companion bundle `aiops-delivery-cli` that the operator enables in the profile `cli`, by the pattern of `command-slack` (`ADR-061:1407`, `00:284`), and it calls the routes of `delivery`. Paths: `delivery request`, `plan`, `propose`, `status`, `accept`, `rework`, `map`.

**`pillar-delivery`, a `core:group` row.** No key, no definition and no suite: a pillar has none (`00:55`). The bundle `aiops-delivery` carries it, as the first-party bundle `aiops-pillars-mvp` carries its groups (`05:181`); an installed artifact could not (`05:154`). Its config names the persona and operating rules as content, leaves `harness` empty so the default harness runs (`05:191`), sets `kinds: {investigate: {firstMessage: delivery, answer: delivery}, ask: {}}` in the form of `05:193`, an empty `signalKinds`, the default tools, and from the guarded form the executor row of section 4.2. If the group schema of S8a refuses one row as both builder and checker, `delivery` splits into two rows.

**Content.** A persona, operating rules and the plan schema (a JSON Schema: summary, assumptions, steps with intent and paths touched, repository and base, environment, checks, rollback, open questions, and an optional change set for small configuration changes). It is bundle content, versioned with the bundle (`08:339`), and holds no customer text (`08:1348`).

### 4.2 The guarded pull request: three contributions to existing definitions

**The catalogue action `open-pull-request`, a contribution to `executors`.** Key `executors`: a registry, provider-bound to `remediation`, the only caller of an executor (`00:57`, `00:234`); consumers are the surfaces that ask for a typed action, here `delivery`. The definition, its fake and its suite are built at S5 with a no-op executor (`08:700`); this contribution passes the obligations half of that suite (`00:57`). The preferred home is the same executor row that `remediation-executor-gitops-pr` needs, so delivery adds a catalogue action and not a second executor family; if ADR-031 keeps that row to fields a GitOps controller owns, it is a sibling row. Target: an entity reference of a kind the action declares, with the repository as a typed parameter (`07:64`). Parameters: repository, base commit, branch, patch digest, plan digest. `Plan` is side-effect free and reads the repository with a read credential: it checks that the patch applies on the base commit, reports the paths and the size as blast radius, and refuses a patch over the size bound, a path on the deny list (pipeline definitions by default) or a patch that contains a pseudonym (section 13). `Apply` pushes one branch and opens one pull request under the lease, idempotent on the token; the receipt is the pull request reference and its head commit. `Rollback` closes the pull request and deletes the branch before a merge, and is a revert pull request after one. `Verify` reads the pull request back and returns a state (`07:671` to `07:675`).

**A lease row for a Git forge, behind `credentials`.** Key `credentials`: provider-bound to `credential-lease` with its `lease-*` rows and consumer-bound to `remediation` (`00:227`). No new consumer of the key is added. It is the row ADR-031 decides for the GitOps executor (`08:1165`); this record asks that the same decision cover application repositories (section 9, row 31).

**`verify-pull-request`, a contribution to `verifiers`.** Key `verifiers`: class open, dispatched by `change-hub` (`00:227`, `00:75`). It reads whether the pull request is at the expected head, the state of its checks, whether it is merged and, for a GitOps repository, whether the sync was seen. It reports a state; the mapping onto the closed verdict list is fixed by the ADR of section 11, row B. Customer acceptance is a state of the delivery request and never a verdict word (`05:611` to `05:614`).

### 4.3 Two definitions, each written only when its first consumer is built

**`requestSources`, for push and pull intake.** A plural key, a registry; class open; tenant by handle; remote `contrib`, so it is written with its remote adapter (`08:645`). Provider and dispatcher: `delivery`. Contributions: `request-source-*` rows, in-process or remote. A contribution has `Describe` (family, push or poll), `Parse(payload)` and an optional `Poll(cursor)`, and each returns requests with an external reference, a project key, a title, a body and the external requester. The dispatcher calls the contribution, never the reverse, because a remote plugin cannot own a route or a job (`07:1117`). Fake: a scripted source. Suite: `Parse` is deterministic and has no effect, the external reference is stable across a replay, the body is returned raw and never pre-scrubbed (`00:74`), a poll cursor only advances. It lands with the first source that is not the API, and the ADR that defines it also settles how a pushed webhook gets its tenant (section 9, row 13).

**`workspaces`, for a run that edits a checkout.** A singular key, one engine; provider-bound; no remote form. Provider: a row that names sandbox instances an operator started, in the way `sidecars-static` names shared instances (`03:113`). Consumer: the dispatcher of the run. `Open(repository, base commit, mode)` returns a handle, `Seal(handle)` returns the patch and the log as artifacts by digest, `Wipe(handle)` removes the checkout. The provider clones with a read credential of its own scope, cuts the patch itself and wipes; the run inside holds no credential. Fake: an in-memory tree. Suite: no credential is readable inside, `Seal` is stable for the same tree, nothing survives `Wipe`, two tenants never share a handle. It lands only with the ADR of section 11, row D, which also decides whether such a run stays in `harnesses` or needs a registry of its own (D6).

### 4.4 Credential classes to add to the table of `00` §5.4 before any row is built

The table is a required design artifact (`00:381`). Each class below needs holder, lifetime, revocation and blast radius there first.

| Class | Needed by | Note |
|---|---|---|
| Repository read credential | `Plan` and `verify-pull-request`; later the `workspaces` provider | Tenant secret, read-only. Where the forge offers an MCP server with read tools, the agent's reads use the existing MCP server credential class (`00:407`) |
| Repository write lease | `open-pull-request` | The class of ADR-031 (`00:769`); one branch and one pull request in one repository |
| Tracker write credential | Write-back, if the owner wants it (D10) | Not planned in the first forms |
| Pipeline trigger credential | A deployment outside GitOps | Deferred with its executor class (`00:617`) |

### 4.5 Reused unchanged

`agent-runs` and the default harness `harness-native` (`00:111`), `llm-gateway`, `tools` with rows of `tool-mcp`, `approval` with `approval-surface-api`, `change-hub` with `verify-readback`, `remediation`, `credential-lease`, `audit-chain`, `pii-pseudonym`, `notifications`, `chat`, `tenant-directory`. The binding maps of the release gain no provider and no consumer of a bound key in the first three forms; `workspaces` would add one provider-bound key, which is part of its ADR.

## 5. The flow, traced

### 5.1 Step 1: take a request

- **Entry.** `aiopsctl delivery request add --source <reference> --file <text>`, the route `POST /v1/delivery/requests`, or the chat verb `delivery` on an enabled surface. A meeting summary enters as a file; the platform records and transcribes nothing.
- **Keys.** `http` runs identity and then mints the tenant handle before the handler (`00:217`); on chat, the surface's installation record resolves the tenant and `chat` mints (`00:330`). `authz.Check` with a permission the plugin declares, `deliveries.open`. `sql`, `events` and `audit` commit the request row, its event and its audit row in one transaction (`03:480`).
- **Invariants.** The tenant is never derived from the payload (`00:327`). The request text is untrusted input and only ever reaches a model as a fenced block of the first message.
- **Mapping.** The `mapping` rows resolve the subject (an entity reference), the repository, the base branch and the environment. A request that matches no row stops in `triage` for a person; no run starts.
- **No signal.** The request is not a `Signal`, so `correlate` and `casefile` are not involved and the incident measures are not touched.

### 5.2 Step 2: the agent writes a plan

- **Keys.** `delivery` calls `harnesses.Dispatch` with kind `investigate`, the pillar `delivery`, the entity reference as subject and the request id as trigger. `agent-runs` selects the harness, takes the slot lease and the budget lease, writes the run row and dispatches `run/first-message` with the pillar and the kind (`03:200`).
- **Hooks.** On `run/first-message`, `delivery` adds one bounded, fenced block: the request text, the mapping row, the notes and which convention files of the repository to read first. On `run/answer`, `delivery` validates the answer against the plan schema and may return one follow-up message (`07:461`).
- **Tools and model.** The harness reads the ticket and the repository through rows of `tool-mcp`; every call gets the actor filter, the output cap, the scrub and an audit row (`07:789`). The model is reached only through `llm-gateway`, whose inner scrub no hook can skip (`00:56`).
- **Invariants.** The harness holds no credential and every offered tool is a read, so a crash restart is safe (`00:45`). Every step is a row before it is forwarded (`03:200`).
- **Result.** `delivery` stores the answer in `blob` by digest as plan version 1, emits `delivery.plan_ready` and sends a notice.
- **Why kind `investigate`.** It needs no change to `harnesses` v0: budgets and the builder and checker rows are keyed by pillar and kind (`HR:41`, `HR:113`), and a compliance run already uses the kind with no incident (`00:45`). Whether plan writing gets a kind of its own is part of D6.

### 5.3 Step 3: the engineer reads, edits and approves

Two gates exist, and they are different things.

- **Plan sign-off, a product gate.** `delivery plan show` prints the plan; `delivery plan edit --file` uploads an edited text as a new version with a parent, an author and a new digest. `delivery plan sign` records the subject and the digest under `authz.Check` with `deliveries.sign-plan`, with an audit row. A sign-off authorises nothing to be written on any target. It is not an `approval.Decision`, and it clears on any later version.
- **Write approval, the decision of record.** `delivery propose` makes code build the typed envelope `{action: open-pull-request, target, parameters: {repository, base commit, branch, patch digest, plan digest}}`. `remediation` calls `Plan`; `change-hub` writes the `ChangeRecord` in `planned` and opens `approval.Request` with the digest and the tier of the action (`03:296`, `03:319`). `approval.Decide` counts a decision only from a verified subject that holds `approvals.decide` in the tenant, is not the requester, has not voted and names the digest (`03:442` to `03:447`).
- **Requester.** In the form that changes nothing, the engineer who proposes is the requester and a second platform user approves, as in the incident flow (`03:319`). The alternatives are D3.
- **Invariants.** The approval floor, the quorum of the tier and the monotonic gates `approval/gates` and `change/pre-dispatch` (`00:68`). A gate listener sees the plan digest, the action class, scrubbed target references and the requester, and no parameter (`03:287`).

### 5.4 Step 4: execute

- **The patch is an input, named by digest.** It has one of three recorded origins: the change set of a read-only run's answer (small configuration changes); a file a person uploaded, made with any tool outside the platform; or, in the last form, a sealed workspace. The write path is the same for all three and does not depend on who or what wrote the patch.
- **Dispatch.** `change-hub` re-checks the quorum and the bindings, re-plans, compares the digest, runs `change/pre-dispatch` and only then signs the `ChangeToken`; a changed digest or a lost quorum returns the record to `pending_approval` with no token (`03:298`, `03:449`).
- **Apply.** The `remediation` guard refuses `Apply` without a valid token or with a changed digest (`00:70`). The executor takes the forge lease from `credentials.Write(token)`, pushes the branch and opens the pull request. No change becomes `applied` without a receipt, and a missed deadline marks it `uncertain`, never `applied` (`03:474`, `03:300`).
- **Merge.** A person merges on the forge, under the forge's own login and branch protection; it is the last human gate and is outside the system (`01:131`).

### 5.5 Step 5: verify with the customer, and loop

- `verify-readback` calls the executor's `Verify`; `verify-pull-request` reports head, checks, merge and sync. `change-hub` records one verdict from the closed list (`05:611` to `05:614`).
- The request moves to `customer_review`. The engineer records `delivery accept` or `delivery rework --note`, each under `authz.Check` and audited.
- **Rework.** The note becomes a block of the next first message. A new plan version, a new sign-off, a new patch digest, a new approval, a new token and a new `ChangeRecord` follow; the link between the records of one request is the `iteration` rows of `delivery`.
- **On accept.** Code writes one `note` row (what was asked, repository, environment, plan digest, change reference). Memory is written by code or by a person, never by a model (section 6.3).

### 5.6 Plan revision

| Revision | What happens | Effect on gates |
|---|---|---|
| The engineer edits the text | A new `plan_version` with parent, author and digest | The sign-off clears |
| The engineer asks the agent to revise | A follow-up run that carries the neutral history, or the harness's own blob where it declares `opaque-history` (`07:1197`, `HR:74`) | The sign-off clears |
| The plan or the patch changes after the write approval | The envelope digest changes | The record returns to `pending_approval`; a new decision is needed (`03:298`, `06:223`) |
| The customer asks for a change after delivery | A new iteration (section 5.5) | Every gate runs again |

### 5.7 The two outcomes and their tiers

The tier of a catalogue action is fixed in its entry, and the tier table can only be raised per tenant (`00:72`, `00:68`). L0 to L2 need one approver who is not the requester and L3 two (`03:448`). The tiers below are proposals for the ADR of section 11, row B.

| Outcome | What the platform writes | Proposed tier | Why | Last human gate |
|---|---|---|---|---|
| A pull request | One branch and one pull request in one repository | L1, the tier a rollout restart has today (`03:319`) | Nothing changes on a running system until a person merges; the rollback is to close and delete | The merge on the forge (`01:131`) |
| A deployment through GitOps | Nothing more: the merge is the deployment and the controller's sync is the receipt (`05:736`) | The tier of the pull request | The platform's write is the same | The merge on the forge |
| A deployment by a typed Kubernetes action | One of the five catalogue actions, asked for through the API with no alert (`00:72`, `03:319`) | The fixed tier of that action | Unchanged from the incident flow | `approval.Decide` |
| A deployment by pipeline trigger or on a host | Not in this design. It is the class of the deferred host executor (`00:617`) | To be set by its own ADR; L2 outside production and L3 for production are proposed, as two catalogue actions | A gate listener cannot see the environment (`03:287`), so a rule by environment is a separate action today | `approval.Decide`, two approvers at L3 |

Opening a pull request can start the customer's pipeline on content a model wrote. The deny list in `Plan` and the approval reduce that exposure; the control that removes it is the forge's own rule that holds pipelines for such a change, which the platform cannot enforce (section 13).

## 6. Customers, tenancy and where the memory lives

### 6.1 A customer is a tenant

This is the recommended model, not a recorded decision (D2).

- One tenant id is already the `tenant_id` column, the secret scope, the blob prefix and the key of every per-tenant record (`00:326`). Row-level security is forced on every tenant table (`00:335`), secrets are scoped (tenant, plugin, name) under one key per tenant (`00:377`), the audit chain is per tenant (`00:73`), and the model budget is per tenant (`00:353`).
- An engineer who serves three customers is three `tenant_binding` rows for one platform user, and `authz.Check(subject, tenant, permission)` decides per customer (`00:339`, `00:224`).
- Intake names the customer by credential, never by message: the binding of the caller on the API, the channel binding on chat (`00:327`, `06:289`).
- Off-boarding a customer is the tenant delete saga, which purges every namespace of every plugin from the `durable` registry, `delivery` included (`00:433`).
- The cost is measured for hundreds of tenant trees (`ADR-036:79`); a handful of customers is far inside it.

**What stands in the way.** The first community release runs the one tenant `default`, and nobody can create a second tenant through an API before S4b (`08:546`, `08:720`, `08:808`). Whether a self-host may create a second tenant at all is the open tenant case of ADR-049, due before S4b (`08:1183`), and the proposed edition principle puts "operating for many organisations" on the enterprise side (`08:963`). Until S4b there are two honest forms: one install per customer, or several customers as mapping rows in the tenant `default` with no fence between them (shared secret scope, audit chain, budget and purge). A project level between tenant and entity is not proposed: it would rebuild every fence above inside one plugin.

### 6.2 The mapping, the configured part of the "brain"

Versioned per-tenant rows in the schema `delivery`, edited through the API and `aiopsctl delivery map`, as the tenant maps of `signal_core` are (`05:425`). A row maps a rule (source, project key, labels) to a subject (an entity reference in the grammar of `00:672`), a repository with its forge and base branch, an environment (cluster and namespace references), the convention files to read first, and where the task and the time log of that customer live. The estate cannot hold this: it is a projection that is never edited through the API or chat, and its first-slice classes hold no repository (`00:58`, `09:203` to `09:207`).

### 6.3 Where memory lives, and where it does not

| Kind of memory | Home | Written by |
|---|---|---|
| Conventions that belong to the code | Files in the customer's repository, read through the read tools | The customer's and the company's engineers, by pull request |
| The mapping of section 6.2 | Rows of `delivery`, versioned | A person |
| Notes from accepted deliveries | Rows of `delivery`, bounded into the first-message block | Code at acceptance, or a person |
| Persona and operating rules | Bundle content, site scope; a per-customer variant is a row or a tenant-writable config field, because content cannot be addressed by a tenant overlay (`07:1327`, `00:465`) | Engineers, by a profile or site layer |
| The transcript of each run | `agent_runs.run_step` (`00:346`) | `agent-runs` |

Memory does not live in the harness, which may keep no tenant data after a run (`HR:62`), and it is not written by a model: write tools are never offered to a harness or chat (`07:789`), and the recall the platform designs elsewhere is computed by code or promoted by a person (`09:620`). It does not live in this repository either: customer text and customer references never enter it (`08:1348`). The neutral bundle is first-party; per-customer rows, notes and prompts are site data, site-layer patches, or packs under the company's own publisher id (`08:1368`).

Run transcripts, plan text and notes are plaintext rows; only secrets are encrypted (`00:377`). The erasure bound is the tenant delete, which is one more reason for section 6.1.

## 7. Harnesses, trackers and chat products as optional plugins

The rule is principle 9: every upstream enters through one provider or contribution behind a definition whose fake and suite name no upstream, and lives in its own bundle or pack (`00:32`). `aiops-delivery` wraps no upstream. An optional bundle that wraps none has a precedent in `aiops-dashboard-app` (`00:290`).

**Coding harnesses.**

- A wrapped harness is its own optional plugin in its own optional bundle, under the home rule W8 (`00:112`); its process is the role id `harness-sidecar`, which the operator installs (`HR:181`). It is selected for the delivery pillar alone through the group's `harness` field, and `harness-native` stays the default everywhere (`HR:102`, `00:111`).
- As a harness it runs with its own shell and file tools off, proven by its rendered profile, as the round-5 design already states for an agent behind the Agent Client Protocol (`docs/design/harness-r5/agent-harness.md:376`). It reaches the model through the gateway's HTTP surface with a lease-bound key (`03:520`).
- A harness that calls a vendor-hosted model itself is not admitted in the community edition (`HR:83`). This is the largest edition finding for the requirement, because that is how such agents are commonly used with a vendor login (D7).
- Verdicts already recorded: goose, opencode and the Agent Client Protocol are `ok` at licence-file level with the vendors' service terms UNVERIFIED (`LR:88`); Codex CLI is `ok-with-conditions` (`LR:89`); the Claude Agent SDK is `optional-plugin-only`, never shipped by the project and never in the community index (`LR:90`); OpenHands is `unverified` and stays out until its runtime repository is read (`LR:92`). The register holds no entry for any agent harness today, and an entry lands in the same commit as its first use (`08:1337`, `08:1347`).
- The lowest-risk first adapter is a protocol adapter for the Agent Client Protocol that drives an agent the user installs. Its id stays generic until a register entry allows a name (`08:1370`).

**Trackers.**

- Reads: rows of `tool-mcp` in a pack with no code, one pack per system, where the system offers a Streamable HTTP MCP server with read tools (`00:113`); otherwise a remote read tool, which the S8b exit proves in Python (`08:630`).
- A tracker is not a named external system today. Naming one needs, in order, an `ext-` id, a trigger row and a numbered ADR (`01:396`), and a reading of its terms and mark policy at a primary source before any id carries its name (`08:1370`).
- Writes have no seam yet (section 3, row 25; D10).

**Chat products.**

- The one designed surface is the optional bundle for Slack, operator-enabled and in no shipped profile (`00:117`); its platform terms are rated `optional-plugin-only`, and a lawyer's question on commercial distribution and on the server-side transcript is open (`LR:123`, `LR:202`).
- A second chat product is a `chatSurfaces` contribution in in-process Go, because the definition has no remote adapter (`06:135`). A first-party one waits for the trigger "first customer request" (`00:602`). The licence record rates two candidates: one needs the vendor's written approval for its name, the other has store, bot and brand terms UNVERIFIED (`LR:129`).
- The designed surface refuses people outside the installing organisation: an externally shared channel is dropped unless bound, and an external member is never linked (`06:289`, `06:332`). A customer-facing thread is therefore captured by an engineer, with the text in the command, and not read from the thread by the platform.

## 8. The chosen design and the three proposals

Three proposals were worked through for this record.

- **Proposal A, least new surface.** No new definition, key, hook, kind or departure. The agent stays a read-only run whose answer is a plan and a change set of whole files; the only write is one catalogue action in the planned pull-request executor. New: one plugin, one command row, one group row.
- **Proposal B, a vertical of its own.** Four new definitions (request sources, the delivery record, runners, workspaces) with two dispatchers of their own. It shares only infrastructure and the policy spine, so the incident-shaped contracts of S5 to S8a could be accepted as written.
- **Proposal C, earliest value.** Fix four data formats now (request, mapping, plan, write envelope) and replace the parts one at a time: a hand-run kit outside the kernel today, a site-composed pillar on the first release, the bundle after it, the guarded write at S9a, the in-platform run last.

**The chosen design** is proposal A's surface, built in proposal C's order, with five parts of proposal B.

| Taken from | What | Why |
|---|---|---|
| A | No new definition for the first three forms; the plan run as kind `investigate` of its own pillar; the write as one catalogue action beside the GitOps executor | It needs no change to `harnesses`, `signals` or the binding maps, and the no-scaffolding rule holds (`08:680`) |
| A | The change set as an optional part of the answer | It serves small configuration changes with no workspace |
| C | The split of patch, envelope and write: a model or a person writes the patch, code builds the envelope, the platform guards the write | It makes the three origins of a patch one write path, and it is the reading of `00:72` that needs one sentence and not a rewrite |
| C | The order: earliest form first, the guarded write next, the in-platform run last | The in-platform run has the most open decisions, and nothing earlier depends on it |
| C | The upload of a patch made outside the platform | Engineers keep the agent they use today while the platform guards the write |
| B | The record with plan versions, the sign-off as a product gate, acceptance as a state and not a verdict | Proposal A overloads the verdict `uncertain` for "ask the customer", which distorts the measure behind the paid verification turn (`00:642`) |
| B | `verify-pull-request` | Without it every delivery change ends `uncertain` with reason `no_data` (`05:614`) |
| B | `requestSources`, written when the first source beyond the API is built | The requirement names six sources; the transport of `signal-core` fits and its consumer does not |
| B | `workspaces`, with the patch cut by the provider and not reported by the run | It is needed whichever registry the run lives in |
| B | Memory written by code or by a person only | It keeps the read seam closed (`07:789`) |

**Not taken.**

- From A: every delivery change ending `uncertain` on purpose; a pseudo service key for work that has no service.
- From B: a second agent registry (`runners`) and a second run dispatcher. A contributor would write two adapters against two suites for one agent, and improvements to `agent-runs` would not reach it. It is kept as the fallback of D6 if `harnesses` is frozen read-only for good. Also not taken: a service subject as the requester by default, which is the deferred sole-approver rule by a side door (`00:617`).
- From C: a command plugin for the hand-run kit. A convention on files gives the same value with no code (D8).

## 9. What steps S2b to S8a must not close off

No step is asked to build anything for delivery, to add a field, or to move. Each row asks for a wording or an open door, at the step that fixes the part. Definitions are `v0` at the first release and every minor may break (`00:267`), so a closed door is a cost of change later, not an impossibility; the cost is paid by every plugin under `plugins/` and `contrib/` in the same change (`08:678`).

| # | Step | The line or the coming decision | What it could close | Smallest protection |
|---|---|---|---|---|
| 1 | S2b | ADR-041, the callback set, "the whole set" (`07:1169`, `08:1175`) | A later run-scoped write of an artifact would need a protocol major | State that the set grows additively inside a major, and that "read" is a property of the offered tool set of `harnesses`, not of the protocol |
| 2 | S2b | ADR-041, deadlines and the reconnect budget (`08:1175`, `07:1196`) | A run of tens of minutes on a remote harness | A call deadline is a value of the definition, not a constant of the protocol |
| 3 | S2b | The adapter generator (`08:697`) | Remote contributions to a registry that an optional bundle defines | The generator is driven by any `definition.yaml` marked `contrib`, not by a list of the release's registries |
| 4 | S2b | "A remote harness declares no network capability" (`07:1197`, `HR:58`) | A remote contribution to another definition that needs a rendered network policy | Keep it a rule of the `harnesses` definition and its lint, not of the remote runtime or of the gate in general |
| 5 | S2b | The instance identity on a single-tenant self-host (`08:1175`) | A remote listener that later serves several tenants from one instance | The tenant comes from the connection, a listed mint point (`00:330`), never from a payload field |
| 6 | S3 | `limiter`, lease rows (`00:213`, `03:424`) | Long delivery runs take the two slots that incident runs need (`00:425`) | The lease key is a string the caller chooses, so a class of runs can get a pool of its own at S8a |
| 7 | S3 | `jobs` (`00:216`) | A run job of an hour | Handler timeouts and drain deadlines are per job kind |
| 8 | S3 | `blob` and `kv` (`00:215`) | An approval that names a plan or a patch; a remote plugin whose only state is `kv` and `blob` (`07:1118`) | An object can be addressed by digest and kept with no expiry; a `kv` key may have no expiry and a plugin can list its own prefix |
| 9 | S3 | The typed-operation catalog owed with `sql` (`00:265`, `ADR-045:480`) | A plugin that cannot look up its own versioned mapping rows | The catalog can express a versioned table with a match lookup, which the tenant maps of `signal_core` need as well (`05:425`) |
| 10 | S3 | Purge by plugin and tenant (`00:220`) | Nothing | Add no sub-tenant partition; record that erasure per customer assumes customer as tenant |
| 11 | S4a | `authz.Check(subject, tenant, permission)` (`00:224`, `06:66`) | No permission for proposing a change that has no incident | Permission names stay open strings that a plugin declares |
| 12 | S4a | "API: the kernel binding of the OIDC subject. A missing or ambiguous tenant is a hard reject" (`00:327`) | An engineer bound to three tenants cannot call the API | A request names its tenant and the binding is verified; `tenant_binding` stays many-to-many (`00:339`) |
| 13 | S4a | The mint points of `tenantMint` (`00:330`) | A later intake route with a token of its own would need a new mint point | Record that delivery intake uses `http` after identity and `chat`; a webhook token of its own is decided with `requestSources` |
| 14 | S5 | `changes`, the typed-action request (`03:296`, `03:319`) | A change with no incident | Neither an incident id nor alert labels are a required part of a request or of a `ChangeRecord` |
| 15 | S5 | `approval.Request` and the rule on the requester (`03:445`) | Approving a typed action outside the five Kubernetes ones; any later answer to D3 | The request binds a digest over a typed payload with an open set of action classes; the requester is a subject, not "the user who clicked" |
| 16 | S5 | The Typed plans row as tested by the `executors` suite and `test/policy-bypass` (`00:72`, `08:626`) | The amendment of D5 would read as a bypass | Word the test as: approval binds to a typed envelope built by code, and a model output is never an executable plan |
| 17 | S5 | The gate payload: digest, action class, target references, requester (`03:287`, `07:466`; ADR-043, ADR-046) | A rule by environment needs a duplicated catalogue action | The payload can grow additively |
| 18 | S5 | The lease of `credentials` (`00:69`); ADR-019 decides the Kubernetes lease only (`08:1153`) | A lease that is one branch and one pull request | The lease form is neutral to the target type and its lifetime is set by the lease row |
| 19 | S5 | `executors` v0 (`07:671` to `07:675`) | A patch as an input; a verification that waits days for a merge | A parameter may name an artifact by digest; the verification window stays a value of the catalogue entry (`05:600`) |
| 20 | S5, the P1 exit review | Check 1 reads the incident's alerts (`05:600`); the `uncertain` rate is a trigger measure (`00:593`, `00:642`) | Changes with no incident distort the measure | Decide how a change with no incident counts before thresholds are set |
| 21 | S5 | `pii`: no reverse mapping is described (`00:74`, `03:482`) | A patch with pseudonyms in it cannot be detected or repaired | A pseudonym can be recognised by code; the interface is not fixed as one-way only |
| 22 | S5 | The `notifiers` types under ADR-055, before S5 ends (`08:1189`, `06:119`) | Every delivery notice is a breaking change of the first registry with remote contributions | A notice of class `approval` needs no incident key; classes and action references can grow additively |
| 23 | S6a | The project index lists open licences only (`08:984`); a private index is proposed community (`08:970`) | A wrapper for a harness under commercial terms has no index | Confirm the private index as community before the index rules are accepted |
| 24 | S7 | ADR-025, the closed kind list (`00:672`, `07:929`) | A repository as a target, or work that has no service | State that a repository is a typed parameter under an entity target (`07:64`), and how a kind is added later (D12) |
| 25 | S8a | ADR-051 and ADR-030, task kinds (`07:595`, `HR:37`) | A plan or execute kind needs a second registry | `Kind` is extensible in a `v0` minor; "a kind a harness does not list goes to the default harness" holds for read kinds only |
| 26 | S8a | `Task.Subject` and `Task.Trigger` (`07:601`, `07:602`) | A consumer cannot tie a hook call to its own record | State that the trigger is an opaque reference of the consumer, delivered to in-process `run/*` listeners |
| 27 | S8a | The remote payload of `run/first-message`: "the incident key" in `07:460`, "the subject key" in `03:220` and `09:354` | A remote listener gets nothing to key on for a run with no incident | Write "the subject key" in all three places |
| 28 | S8a | The crash rule: "safe because every tool is a read" (`00:45`, `HR:48`) | A later run with effects is restarted automatically | The restart is conditional on a read-only run in the dispatcher's code; hold the promotion of `harnesses` to `v1` (`08:910`) until D6 is decided |
| 29 | S8a | ADR-012: the surface is OpenAI-compatible, the key serves one run at a time (`00:56`, `HR:75`) | Nothing, if both stay simplifications of `v0` | Do not write either as an invariant of the gateway |
| 30 | S8a | ADR-053: "a tool refuses a call without a time range and a limit" (`HR:161`) | A repository or ticket read tool, which has no time range | Keep it a rule of the telemetry tools; other read tools need a size bound |
| 31 | S9a start (adjacent) | ADR-031, the credential per Git forge (`08:1165`) | A second amendment of `00` §2.2 for the same credential class | Decide it for application repositories and for several forges in one tenant in the same ADR |
| 32 | S8b (adjacent) | "An optional bundle wraps one upstream" (`08:281`) | An optional first-party bundle that wraps none | Keep the sentence a description, as `aiops-dashboard-app` already requires (`00:290`) |

## 10. The earliest usable form, and what is throwaway

Steps S2b and S3 by themselves give engineers nothing to use: a domain plugin reads zero rows without a tenant handle (S4a, `00:335`), and an API has no verified caller before the policy spine (S5). The forms below are in order; none moves a step.

| Form | When | What a person can do | New | Needs |
|---|---|---|---|---|
| 0, a convention | Now | Keep four files per request in the team's own private repositories: the request, the mapping, the plan with a digest, the write envelope with a digest. The agent runs as today | Nothing in this repository | The owner's choice (D8) |
| 1, a site composition | The first community release | A pillar group in the site layer (`05:154`), a pack with rows of `tool-mcp`, a remote listener on `run/first-message` for the mapping. The plan is drafted by the platform with scrub, budget, read tools and a transcript | No code of the project, no ADR | Three unverified points (below) |
| 2, the bundle as a read-only desk | After S8b, on the S9a track | Open a request, get a checked plan, edit, sign off, fetch the plan and a change set, open the pull request by hand, record acceptance | Section 4.1 | ADR A of section 11; reads are free, so no amendment of `00` §2.2 |
| 3, the guarded pull request | S9a item 5, after ADR-031 | The platform opens the pull request under approval and a lease; the patch may be uploaded or come from an answer | Section 4.2 | ADR B; D3, D4, D5 |
| 4, sources and notices | After form 2, in any order with form 3 | Requests arrive by webhook or poll; notices go to email, a webhook or an enabled chat surface | `requestSources` | ADR E; D9, D10 |
| 5, the run inside the platform | Undated | The agent edits and tests in a sandboxed checkout; the workspace seals the patch | `workspaces` | ADR D; D6, D7 |
| Customers as tenants | S4b | The fences of section 6.1 | Nothing | ADR-049 (D2) |

**Unverified for form 1.** Whether a group row in the site layer may name persona and rule files that an installed pack unpacked (`07:1335`); what the route behind `aiopsctl ask` accepts beyond a question (`03:200`); and how a remote listener keys on a run with no incident (section 9, row 27). If the first fails, form 1 is skipped and form 2 is the earliest.

**Throwaway and kept.**

| Throwaway | Kept through every form |
|---|---|
| The files of form 0 as records: their digests will not match the canonical form S5 fixes, so they are history and never approvals | The four formats: request, mapping, plan schema, write envelope |
| The remote mapping listener of form 1, once `delivery` reads rows | The mapping content, the persona and rules, the packs of `tool-mcp` rows |
| Opening the pull request by hand (form 2) | The split of patch, envelope and write; one branch per envelope |
| The upload path as the main path, if form 5 is built | The upload path as an option |

## 11. The ADRs this needs

New ADRs take the next free number from ADR-063 when they are opened, and an ADR that changes a decision edits `00-decisions.md` in the same change (`08:999`). None changes the order of work.

| # | ADR | Decides | Edits | Due |
|---|---|---|---|---|
| A | The delivery bundle | The record and its states, the plan as an answer and not a plan of `00` §2.2, the plan schema, memory written by code or a person, retention of plan text, the pillar group | `00` §1 and §2.1 row 12 (`00:55`), by the choice of D1 | When the bundle is planned, at the review of the first community release |
| B | The typed envelope and the action `open-pull-request` | The sentence of D5, the action's tier, rollback and verification mapping, the requester (D3), the two gates (D4), the deny list, the pseudonym rule | `00` §2.2 Typed plans (`00:72`); the table of `00` §5.4 | S9a start, read with ADR-031 |
| C | ADR-031, already open | Asked to cover application repositories and several forges per tenant (section 9, row 31) | As it already states (`00:769`) | S9a start |
| D | Runs with workspace effects | `workspaces`; a kind and a departure of `harnesses` or a second registry (D6); sandbox, egress, clone credential, wipe, crash rule, slot class; the scrub on source code | `00` §2.1 row 2 (`00:45`) and one sentence of principle 6 (`00:29`) | After ADR-051 is accepted; before form 5 |
| E | Request intake | `requestSources`; how a pushed webhook gets its tenant; a tracker as a named external system; write-back by D10 | `00` §3.3 and §7.3, as `01:396` requires | Before form 4 |
| F | Admission of the first external coding harness | Topology, departures, model route, register entry and licence verdict | The register; nothing in `00` unless D7 changes `00:632` | Before the first wrapper is built |
| G | A second chat surface | Its rows, licence verdict and name | The deferred row `00:602` | On its trigger |

Inputs to ADRs that are already open, with no new ADR: ADR-041 (rows 1 to 5 of section 9), ADR-025 (row 24, D12), ADR-043 and ADR-046 (row 17), ADR-055 (row 22), ADR-051 and ADR-030 (rows 25 to 28), ADR-012 (row 29), ADR-053 (row 30), and ADR-049 (D2, D7).

## 12. The outside landscape

Every statement in this section was read at the primary source named with it, on 2026-11-27. What could not be verified is listed at the end and is not relied on.

### 12.1 The personal agent the owner named

The project that matches the name and both properties is **Hermes Agent**, published by Nous Research under the MIT licence (<https://github.com/NousResearch/hermes-agent>). The match rests on the name and the description; the owner's confirmation is D14.

| Property | What the source says | Source |
|---|---|---|
| Gateway | A single background process that connects to every configured platform; sessions are kept per chat and do not reset on inactivity. The page lists more than twenty platforms. It does not say whether several people in one group chat share a session | <https://hermes-agent.nousresearch.com/docs/user-guide/messaging/> |
| Memory | Two files the agent curates, one of notes (2,200 characters) and one user profile (1,375 characters), injected into the system prompt as a snapshot taken at session start. The model writes them through a `memory` tool. An approval of memory writes exists as a setting and is off by default. "Memory is scoped per profile by design". All sessions are also kept in a local SQLite file with full-text search | <https://hermes-agent.nousresearch.com/docs/user-guide/features/memory> |
| Approval | Per dangerous shell command, in modes `smart` (an auxiliary model assesses the risk; the default), `manual` and `off`; denied after a timeout of 300 seconds. "By default any member admitted to the chat can answer a pending approval, whoever's message raised it." A flag bypasses the prompts for a session, and in container backends the checks are skipped | <https://hermes-agent.nousresearch.com/docs/user-guide/security> |
| Access | An allow-list of user ids or a pairing code that the owner approves; everyone else is denied | The same page |
| Not on these pages | A tenant or an organisation; an audit log of who approved what; approval of a plan before execution | The three pages above |

A second member of the same class is OpenClaw, MIT (<https://github.com/openclaw/openclaw>): "The Gateway is the local control plane for sessions, tools, events, and channel connections", and tools run on the host for the main session unless sandboxing is configured. It routes an inbound message to an agent by channel, account and peer, and says of that separation: the workspace "is the default cwd, not a hard sandbox", cross-agent session access is on by default, "use separate gateways for strict separation", and "true isolation requires one agent per person" (<https://docs.openclaw.ai/concepts/multi-agent>).

**Comparison.** The class is a single-operator personal agent. Its trust model is one home directory, one memory and approvals in the chat.

| | The personal-agent class | This design |
|---|---|---|
| Channel gateway | One process, an adapter per platform, a session per chat | The `chat` dispatcher with one `chatSurfaces` row per product (`06:29`), and far fewer products, each in-process Go under a licence review (`06:135`) |
| Who is the customer | Not modelled; a profile or a separate gateway | The tenant, resolved from an installation record or a binding, never from the message (`00:327`) |
| Memory | Written by the model, small, one per profile, unreviewed by default | Rows written by code or a person, per tenant behind row-level security, purged with the tenant (section 6.3) |
| Approval | Per shell command, by any chat member by default, with a model-judged mode and a bypass | Per typed change, by a verified platform user who is not the requester, bound to a digest (`03:442` to `03:447`) |
| Evidence | Local logs | A per-tenant audit chain, exportable (`00:73`) |

What is worth taking is the pattern of a gateway with adapters and a default-deny allow-list, which the `chat` dispatcher already has. What is deliberately not taken is model-written memory and approval in the chat by whoever is present. A chat surface may carry a decision to `approval.Decide`; it may not be the approval.

### 12.2 Coding agents that turn a ticket or a chat message into a pull request

| Product | What the source says | Source |
|---|---|---|
| GitHub Copilot cloud agent | Only users with write access can trigger it; it pushes to a single branch; the user who asked for a pull request cannot approve it; the agent cannot approve or merge; workflows do not run until a person approves; hidden characters are filtered from the input; each commit links to the session log; session logs and audit events are available to administrators | <https://docs.github.com/en/copilot/concepts/agents/coding-agent/risks-and-mitigations> |
| The same, from an issue tracker | Triggered by assignment, a mention or an automation. No fixed mapping from tracker project to repository: the repository is named in the work item text or set as a workspace default. A follow-up mention opens a new pull request. No plan approval before code is described | <https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/integrate-coding-agent-with-jira> |
| Devin, from an issue tracker | Assignment, a label or a mention starts a session directly. The session link and the pull request link are posted back. No approval before the start, no plan posted to the ticket and no project-to-repository mapping are described | <https://docs.devin.ai/integrations/jira.md> |
| Jules | "it generates a plan before writing any code"; the user revises through chat and clicks approve; "If you navigate away, Jules will eventually auto-approve the plan, which is set on a timer." Editing the plan text is not described | <https://jules.google/docs/review-plan/> |
| Cursor cloud agents | Started from chat, a tracker, a pull request comment, the web or an API; run in isolated virtual machines; work on a separate branch and produce a pull request. No plan mode or approval before execution is described on the page | <https://cursor.com/docs/background-agent> |
| Claude Code, plan mode | The agent reads files, runs commands to explore and writes a plan without editing source; the user approves or keeps planning, and can open the plan in a text editor and edit it. The mode keeps its blocks in non-interactive runs | <https://code.claude.com/docs/en/permission-modes.md> |
| Linear, agents | An agent is an app user that an admin installs. Assigning an issue sets the agent as the delegate, not the assignee, "so humans maintain ownership while agents act on their behalf". Nothing on plans or approval | <https://linear.app/developers/agents> |
| Agent Client Protocol | A plan is "a list of steps or a Markdown document" that the agent sends to the client; the protocol defines no message by which a client edits or approves it. Consent per tool call is `session/request_permission` with allow and reject options, and "The Agent owns remembered decisions." | <https://agentclientprotocol.com/protocol/agent-plan>, <https://agentclientprotocol.com/protocol/tool-calls> |
| Atlantis | The requirement `approved` "requires pull requests to be approved by at least one user other than the author"; requirements are set on the server per repository, and a repository may override them only where the server allows | <https://www.runatlantis.io/docs/command-requirements> |
| Spec Kit, MIT | A sequence of steps from a constitution through specify, plan, tasks and implement, run "one at a time, reviewing each result before continuing" | <https://github.com/github/spec-kit> |

**What this means for the design.**

- No product read here maps an inbound request to a customer and then to a repository, an environment and conventions. The mapping of section 6.2 has no upstream to wrap.
- No product read here binds an approval to what executes. Plans are approved as prose, on a timer, or not at all, and the review is of the pull request. The digest binding is the platform's own (`00:72`).
- The controls worth copying are already rules here: the requester cannot approve (`03:445`), the agent holds no standing credential and pushes one branch (`00:69`), a person merges (`01:131`). One is not: holding the customer's pipeline until a person approves is a forge setting, outside the platform (section 13).
- Every product ends at the pull request. Verification with the customer, the rework loop and time logging exist in none of them.
- An adapter for the Agent Client Protocol must map a permission request to a refusal or to an end of the run, never to the agent's own remembered "always allow".

### 12.3 Meeting to ticket

| Product | What the source says | Source |
|---|---|---|
| Fireflies, hosted | After a meeting is processed it creates issues automatically in the configured project; one-way; routing by meeting title, host, participants and type; no review before creation is described | <https://guide.fireflies.ai/articles/6214483081-how-to-integrate-jira-with-fireflies> |
| Vexa, Apache-2.0 | Bots join three meeting products; the transcript is fetched over REST; an MCP endpoint is served; self-hostable | <https://github.com/Vexa-ai/vexa> |

For a customer-facing flow the step from summary to requirement needs an engineer's confirmation before it becomes the input of a plan. This design keeps capture outside the platform and takes only a confirmed summary (D11).

### 12.4 Not verified, and not relied on

Whether any named coding agent can be pointed at an OpenAI-compatible base URL. The licence files of the OpenHands repositories. Any plan-approval behaviour of the products above beyond the pages cited. The built-in recap features of meeting products. Each tracker's and each chat product's terms of service and mark policy.

## 13. Risks

| Risk | What can go wrong | What the design does | What stays open |
|---|---|---|---|
| Consent for recording | A meeting is recorded or transcribed without the consent the customer's jurisdiction or contract requires; a summary carries personal data into plaintext rows (`00:377`) | The platform records and transcribes nothing; only a summary a person confirmed enters, as a file | The company's own consent practice; D11 |
| Model access to customer code | Source code and ticket text of a customer reach a model endpoint the customer did not agree to | Inside the platform the only model route is `llm-gateway` (`00:56`). The first release is one upstream key for the install; a tenant's own key is a hosted P2 item (`00:56`) | Consent per customer and the endpoint per customer (D13). A patch made outside the platform and uploaded is outside the gateway: its model access is governed by the engineer's own tool |
| The scrub on source code | Pseudonyms are salted and correlation-preserving with no reverse mapping described (`00:74`, `03:482`); a change set from an answer could carry pseudonyms into a patch | `Plan` refuses a patch that contains one; the engineer edits those lines by hand | A pseudonym must be recognisable by code (section 9, row 21); what the scrub matches in source code was not found in the files read |
| Data separation | Before S4b every customer shares the tenant `default`: one secret scope, audit chain, budget and purge (section 6.1) | Customer as tenant from S4b; until then one install per customer is the form with a fence | ADR-049 (D2) |
| Cross-customer memory | A note or a mapping of one customer reaches a run of another | Memory is tenant rows under forced row-level security (`00:335`); a harness keeps no data after a run (`HR:62`); a wrapped harness that keeps session data needs one instance per tenant (`HR:80`) | Holds only with customer as tenant |
| Untrusted input | Ticket or chat text steers the agent | The text is a fenced block; every tool is a read; the write is a typed envelope a person approves; the worst case is a bad patch that two gates and the forge review still see | Form 5 adds a process that runs model-written commands with repository read access; its sandbox and egress are ADR D |
| Write credentials | A standing token for the forge leaks, or is wider than one repository | The credential is a lease for one action and minutes, given only at `Apply` (`00:69`) | Where the forge has no expiring per-repository token, ADR-031 names a standing token limited to branch push and pull-request creation as an amendment with a stated bound (`08:1165`) |
| The customer's pipeline | Opening a pull request runs content a model wrote, with the pipeline's secrets | The deny list on pipeline definitions; the approval before the push | The forge's rule that holds the pipeline for such a change is the customer's to set |
| One-person path | With a service subject as requester (D3 option 2), one engineer approves an agent-built change, and a repository with no branch protection has a single-person path to a merge | Not the default of this design | D3 |
| Cost | A long plan run holds one of the two run slots (`00:425`); a change set in an answer needs a high output cap | Caps per (pillar, kind) (`HR:41`); incident runs are taken first (`09:466`); the daily budget and `agents-paused` (`00:56`) | A slot class for long runs (section 9, row 6) |
| Terms and marks | A wrapper names a product whose terms forbid it, or enters the community index under commercial terms | Generic ids until a register entry allows a name; `optional-plugin-only` verdicts stay out of shipped profiles and the community index (`08:1356`, `08:1370`) | Each upstream's reading at a primary source |
| Capacity | The bundle competes with the path to the first release for the same people | Nothing is built before the S8b exit except by the owner's choice of D8 | The owner's priority |

## 14. Questions for the project owner

Each question states the options and a recommendation. None is decided here.

| # | Question | Options | Recommendation | Why |
|---|---|---|---|---|
| D1 | Is delivery work a pillar of this product? The text counts seven (`00:55`) | (a) An optional pillar group in an optional first-party bundle; row 12 gains one sentence that an optional bundle may carry a pillar group, and the shipped profiles keep seven. (b) An eighth pillar of the product, in the phasing of row 12. (c) Outside the project: a site-private distribution | (a) | It keeps the release gate and the golden dumps untouched, like every optional bundle (`00:521`), and the product text stays true |
| D2 | Is a customer a tenant, and may a community self-host create a second tenant? | (a) Customer is a tenant; tenant create on a self-host is community in ADR-049. (b) A project level inside one tenant. (c) One install per customer for good | (a), with (c) as the form until S4b | Every fence already exists per tenant (section 6.1). (b) rebuilds them in one plugin. If ADR-049 puts tenant create on the enterprise side, a services company with several customers has no fence in the community edition |
| D3 | Who is the requester of an agent-drafted change, given that the approver may not be the requester (`03:445`)? | (1) The engineer who proposes; a second platform user approves. (2) A service subject carrying the external origin; the same engineer approves. This is the deferred sole-approver rule and needs its own ADR that edits `00` §2.2 (`00:617`). (3) The external customer contact | (1) for the first build. Decide (2) explicitly if a single-engineer path is wanted, bounded to the pull-request action on repositories whose branch protection requires another person's review | (1) changes no line. (2) may be what the requirement means by "the engineer approves", and it should be a decision, not a side effect |
| D4 | One platform gate or two per iteration? | (a) A plan sign-off as a product gate, then the write approval on the envelope digest. (b) One approval at plan time, with the merge as the second human gate | (a) | At plan time the patch does not exist, so an approval there cannot bind what is written (`00:72`) |
| D5 | May the Typed plans row (`00:72`) gain one sentence? | (a) Add: a typed parameter may name an artifact by digest whose content a model or a person wrote; code builds the envelope and the digest covers the artifact. (b) Leave the row; pull requests are opened by hand for good (form 2). (c) Rewrite the row around "typed envelope" | (a), through ADR B, read with ADR-031 | It is additive and keeps "the model never writes a plan" true for the executor's plan |
| D6 | Where does a run that edits a checkout live, and when is it decided? This would edit `00:45` and one sentence of `00:29` | (a) In `harnesses`: a new kind and one more declared departure, with `workspaces` beside it. (b) A second registry and dispatcher. (c) Never inside the platform: patches are always uploaded or come from an answer | (a), decided after ADR-051 is accepted; (c) is how forms 2 and 3 already work | (b) doubles every adapter and suite. Until the decision, hold the promotion of `harnesses` to `v1` |
| D7 | Does a harness that calls a vendor-hosted model itself stay an enterprise item (`00:632`)? | (a) Yes; in the community edition a coding harness goes through the gateway, or runs outside the platform and its patch is uploaded. (b) Admit it in the community edition as a declared departure of an optional bundle, with scrub, budget and meter lost and printed at boot. (c) Decide per harness | (a) | The upload path already serves the agents engineers use today, with the write still guarded. Confirm before ADR-051 is accepted |
| D8 | What do engineers use before the first community release? | (a) Nothing new. (b) Form 0: the four formats as a convention in the team's own private repositories, with no code and nothing in this repository. (c) A command plugin in a distribution the company builds | (b) | Today's practice is already outside the invariants; the formats carry over and cost nothing to drop. (c) is code that is thrown away |
| D9 | Do trackers and a second chat product become named external systems now? This fires the triggers of `00:602` and `00:612` | (a) Trackers through generic rows and `tool-mcp` packs first; a second chat surface only after its product's terms are read. (b) Open the ADRs now and schedule first-party rows after S8b | (a) | No tracker and no chat product has a register entry or a mark reading yet (`08:1347`). Whether this requirement is the "first customer request" of `00:602` is the owner's word |
| D10 | Does the platform write back to a tracker (a comment, a status, a time log)? | (a) Stay manual. (b) A notice with a link through a row of `notifier-webhook`; a status change or a time log as an executor action at a low fixed tier, under approval. (c) A write tool | (a) for the first forms, then (b) for notices | A state-changing write to a vendor belongs to `executors` (`CR:374`). (c) breaks the read seam (`07:789`). Time logging has no definition and no consumer in the platform yet |
| D11 | Does meeting capture enter the platform? | (a) No; only a summary a person confirmed enters, as a file. (b) A connector for a self-hosted meeting bot | (a) | Consent and personal data stay with the company; no product read creates tickets with a review step (section 12.3) |
| D12 | Is a repository an entity kind or always a typed parameter (ADR-025, S7)? | (a) A typed parameter under a service, workload or namespace target. (b) A kind `repository` | (a), and ask ADR-025 to state how a kind is added | It matches `07:64`. Work with no natural service (a shared library, documentation) is the case that may later need (b) |
| D13 | Which model endpoint may see a customer's code and tickets? | (a) Deny by default: a tenant runs delivery only with a recorded consent and an endpoint the customer accepts. (b) One endpoint for every customer of the install | (a) | It is a contract matter per customer; the per-tenant route is a hosted P2 item (`00:56`), so before it (a) means one install per endpoint |
| D14 | Is Hermes Agent the project the comparison was asked about? | (a) Yes. (b) Another project of that name | (a) | The name and both properties match (section 12.1); the conclusions hold for the class either way |
