# Harness and upstreams record, 2026-11: the agent harness as a plugin point, upstreams as plugins, on-call

Status: decided by the chief architect on 2026-11-11 from the owner decisions of that date (section 1), two round-5 designs (`docs/design/harness-r5/agent-harness.md` and `upstreams-as-plugins.md`, cited as "harness design §n" and "upstreams design §n"), the licence record `docs/design/licence-audit-2026-11.md` (cited as "licence record §n") and three reviews of the designs (harness generality, licence and naming, scope and order; 44 findings, dispositions in section 11). Applied the same day to `docs/architecture/00-decisions.md` (`00 §n`) and, minimally, to the plugin record and the community record. 00 wins where it and this record differ; this record wins over the two designs. Nothing here is measured, no step has a date, and every effort figure is an estimate. Licence statements are engineering analysis, not legal advice; the questions for a lawyer are licence record §6.

Owner confirmation, 2026-11-11: the owner confirmed W2 to W13 in words on 2026-11-11 (00 §2.4). W7 to W13 of section 1 were written here as working assumptions and are owner decisions from that date; the alternatives beside them are recorded as options not taken. For W11 the owner's yes is given, the lawyer's answer on schedule patents (licence record Q3) is still open, and the fallback of section 8.3 stands as written.

## 1. Owner decisions of this round, and the owner decisions W7 to W13

| # | The owner's words, restated | Where applied |
|---|---|---|
| A | The agent layer is built new (said earlier on 2026-11-11). HolmesGPT is one plugin only: an agent-harness plugin that stands in for other harness layers, so a user may choose it as the harness of the incident agent, the compliance agent or the FinOps agent | Sections 2 to 6 |
| B | GoAlert is one plugin: an on-call provider plugin | Sections 7 and 8 |
| C | The open-source edition is written on the components chosen as plugin bases; review each licence, and say which conflict and must be rebuilt | The licence record |

The words decide three things: nothing in the core names or needs HolmesGPT or GoAlert; a harness is selectable per pillar agent; a first-party harness exists. They did not decide the points below. Each was written as a working assumption, and the owner confirmed each in words on 2026-11-11, so each is an owner decision of 2026-11-11 under the same label (00 §2.4). The third column records the alternative that was not taken and the places it would have reopened.

| # | Owner decision of 2026-11-11 | Alternative not taken, and what it would have reopened | Decide by, as first written; closed by the confirmation |
|---|---|---|---|
| W7 | The first community release ships `harness-native` as the default harness of every shipped profile | (i) The release ships on the neutral contract with the HolmesGPT plugin as the composed harness and the native loop follows in S9a: the path to the release loses the loop, the loop rows and the read tools, a shipped profile carries the departures of section 6, and a harness container returns to the default set. Reopens 00 §2.1 rows 2 and 16, §4.4, §6.3, S8a. Closed by the final reconciliation of 2026-11-11: the owner's words make the default harness first-party, so alternative (i) is not available and W7 covers only the scope of the loop at the release (00 §2.4 W7) | S8a start |
| W8 | One home rule: a first-party plugin that wraps an optional upstream (`harness-holmesgpt`, `on-call-provider-goalert`, the Slack rows) lives under `plugins/`, ships in the release image, and sits in an optional bundle that no shipped profile lists. Both adapters are built in S8b and ship with 0.1.0 | `contrib/` and the `aiops-contrib` image for the adapters (independence proven by the build, but a second image, the `contrib` id, and never selectable on hosted); or the adapters in S9a (the release has one real contribution per registry). Reopens section 7.5, 00 §4.4, S8b | Before S6a |
| W9 | `tool-mcp` ships in 0.1.0 and `toolsetBindings` is withdrawn. A pack with no code then reaches only systems that offer a Streamable HTTP MCP server with read tools; any other system needs a read connector with code | Keep a binding registry inside the HolmesGPT plugin only. Reopens section 5.3, leg C1, community record §6.4 and §6.5 | Before S2a ends |
| W10 | Evaluation leg E, a smoke baseline on a case set, is a gate of 0.1.0 | E stays in P2 as 00 had it. Reopens section 9, S8b | S8a start, with the sizing of section 9 |
| W11 | On-call option (b): a small first-party provider, `on-call-provider-builtin`, is the default. The owner's yes is given; no code before the lawyer's answer to licence record Q3 | Option (a), providers only (section 8.2). Reopens 00 §2.1 row 3, §4.4, §5.2, the `notifiers` types of S5, leg A step 2 | Before S5 ends |
| W12 | Shared fate: the first-party loop, the read tools and the built-in provider run in the kernel process, under the bounds of sections 4.3, 5.2 and 8.4 | A worker-only role split on self-host, or an external provider for an independent pager. Reopens 00 principle 6 | S8a start |
| W13 | The chat-ops bundle for Slack is operator-enabled and listed by no shipped profile; the release gate runs chat steps through the API and replays the Slack path | Keep it in the shipped profiles and accept a release gate on a revocable contract (licence record §2.4). Reopens 00 §6.3, §7.1 | Before S8b |

## 2. The harness definition

### 2.1 Contract

`sdk/def/harnesses/v0`, `definition.yaml` at `0.1`, with a fake (scripted steps) and a conformance suite. It replaces `investigators`; its dispatcher `agent-runs` replaces `investigations`. The key is a registry, provider-bound to `agent-runs`. Consumers declare `Need(harnesses)`: `casefile` (incident runs), `chat` (questions) and `ask-api`, the API route behind `aiopsctl ask`, which is the consumer that exists at S8a. Remote: `contrib` from S8a.

| Part | Content |
|---|---|
| `Harness` | `Describe()` returns id, version, upstream, the task kinds it supports and its departures, read without a run. `Run(ctx, request, sink)` handles one task; the tenant is the handle in `ctx` |
| `Registry` | `Register` as an effect of the caller's fiber; `Dispatch(ctx, task)`, the only way to reach a harness; `Inventory()` |
| Task | Kind (`investigate`, `ask` in `v0`), agent (the pillar id), subject (an entity reference or an incident), trigger. A kind a harness does not list goes to the default harness |
| Request | Run id and an opaque run handle; the persona; the operating rules (content the pillar group names, so the loop holds no prose); the first message as blocks built by code; a neutral history or the harness's own opaque blob; the answer spec (a JSON Schema, or empty for free text); the tool set; model access; the budget; the allowed tier range |
| Tool set | `Specs()` (name, description, parameters, signal, dialect, scope note) and `Invoke`. In another process the same set is reached through the `tools` callback of the remote protocol |
| Model access | `Stream` with a tier (`fast`, `standard`, `deep`) inside the run's range, never a provider, URL or key; `Limits(tier)` |
| Budget | Steps, tool calls, input and output tokens, deadline, wrap-up time; USD only as information. Keyed by (pillar, kind) in `TenantLimits` with one default |

### 2.2 Steps, answer, cancel

- **Steps.** `ModelCall`, `ToolCall`, `ToolResult`, `Message`, `Note` (a closed kind for anything that changed what the model sees) and one terminal `Answer` with `stoppedBy`. `agent-runs` writes each step to its own schema `agent_runs` (`run`, `run_step`, artifact references) before it forwards it. `casefile.investigation` keeps a run id; `casefile.investigation_step` is not built. A compliance or FinOps run therefore needs no incident. Steps of child agents are flattened into the run and count against the same caps. There is no approval step: every offered tool is a free read.
- **Answer.** Text, and JSON when a spec is set. The harness builds no reference and sets no flag; listeners on `run/answer` do.
- **Cancel and deadline.** Cancelling `ctx` is the only cancel, with a grace of 5 s (initial value). At deadline minus wrap-up a conforming harness sends one last request without tools. With no answer the dispatcher builds the digest from the step rows.
- **Crash.** The run restarts from the first message, which is safe because every tool is a read.

### 2.3 What stops a harness

Caps are enforced at the two views a harness holds, never from the stream it reports. The tool set refuses call N+1 and any tool outside the offered set. The model view and `llm-gateway` refuse a request after the step ceiling, a request with tools at the ceiling, tokens past the cap, a tier outside the range and any call while `agents-paused` is set. The stream is transcript only.

| Never | Enforced by |
|---|---|
| Write, or start anything on a target | The tool set holds effects class `read` only, filtered to the actor's permissions; `tools/pre-execute`; `credentials` is consumer-bound to `remediation` |
| Hold a credential | A harness with no departure injects nothing that yields a secret; `plugin check` and the index lint compare `capabilities.secrets` with the declared departures |
| Reach the network | One rule: a `harnesses` contributor reaches the network only through a `sidecars` endpoint of its own row. `dialer` in its `uses`, or `capabilities.network` on a remote manifest, is refused by `plugin check`, the index lint and admission. The plugin vet gains "no dial and no HTTP client outside `dialer`" for every plugin, a rule that binds from the step that builds `dialer` (amended 2026-11-23, ADR-061 decision 38), with one named exception: the operations plugin of ADR-060 decision 41 may call `net.DialUnix` for `<home>/run/reload.sock` (ADR-062 decision 14) |
| Call a model outside the gateway | The rule above, plus a key that is lease-bound: the gateway accepts an instance key only while `agent-runs` holds a run lease for that instance |
| Derive or carry the tenant | The handle is the only source; one instance per tenant in a multi-tenant profile |
| Build references or flags, post, page or notify | Its only output is the sink |
| Keep tenant data after the run | The generated cycle and two-tenant tests; otherwise the departure `retains-run-data` |

Prompt injection is bounded by construction in the default bundles: alert text and tool output are fenced as data, no offered tool writes, and none sends data to an address the model chooses. Two residual paths exist and are printed at boot: a `tool-mcp` row (its effects class is asserted by the pack, not proven by the recording sandbox, and model-written arguments leave the kernel) and a harness with `native-reads`.

### 2.4 Departures

A closed list, sized from three harnesses read on paper (HolmesGPT, dsh through its `acp` profile, a Python agent library). `Describe()` declares them, boot prints them, the dump shows them. A harness in a shipped profile has none. The fallback of a departure is built with the first harness that declares it.

| Departure | Meaning | Fallback in `agent-runs` |
|---|---|---|
| `advisory-steps` | The upstream's step ceiling is a rendered setting | The views still refuse at the ceiling; the run is `incomplete` |
| `best-effort-cancel` | Closing the stream may not stop the upstream | Grace, abandon, breaker; the lease ends, so its key stops working |
| `opaque-history` | A continued run needs the upstream's own history | Stored once per turn; a follow-up stays on that harness and version |
| `instance-key` | The process holds a long-lived virtual key | Lease-bound key; concurrency 1 per key until a cassette proves a key per slot |
| `no-model-steps` | No step per model call | The gateway's rows stand in |
| `native-reads` | The upstream reads with its own toolsets and holds read credentials | Section 6.2 |
| `wrapped-prompt` | Persona or first message cannot be sent unchanged | The gateway's digest of the first model request is recorded and the run is marked |
| `no-answer-schema` | No structured answer | The schema is appended as a last text block and the one repair turn applies |
| `retains-run-data` | The process keeps session data | One instance per tenant, an ephemeral volume, an `external` durable namespace that blocks tenant delete until wiped |
| `opaque-context` | The harness cannot emit `Note` | The gateway stores a digest of every model request under the run id |

A harness that calls a vendor-hosted model itself (`external-model`) is not admitted in the community edition; it is an enterprise item (00 §7.3).

### 2.5 Topologies, conformance

| Topology | Who starts the process | Model and tools reached by | Fence |
|---|---|---|---|
| The plugin is the harness, in process | The kernel | The two views | The plugin vet and the import boundary |
| The plugin is the harness, remote | The orchestrator, as for any remote plugin | The `llm` and `tools` callbacks, each carrying the run handle | Deny-all egress; one instance per tenant in a multi-tenant profile |
| An in-process plugin drives another process | `sidecars` | The gateway's HTTP surface with a lease-bound key; tools per request or through `tools-mcp-facade` | An internal compose network rendered by the plugin, a NetworkPolicy on Kubernetes; for a static unit boot says the fence is the operator's |

S8a builds the first two. The third arrives with `harness-holmesgpt` (the HTTP surface, the facade and the key lease have no earlier consumer). A third process inside a remote plugin needs loopback shims in the SDK and waits for its trigger (00 §7.3).

Conformance has an obligations half (first message and answer spec unchanged, only offered tools, every model call through the view, complete ordered steps, one terminal step, budget, cancel, continuation, no effect, two tenants, dispose, truthful departures) and a guard half (read-only set, leases, step rows before forwarding, `run/answer`, `agents-paused`, selection, each fallback). Two levels are named in the attestation and shown by the index: `replay` (the adapter against cassettes, in `aiopsctl`, every pull request) and `live` (the real upstream at the pinned digest against `llm-provider-replay` and recording tools, egress denied; on a pin bump and before a release that ships the plugin). Departures are proven truthful only at `live`. `test/policy-bypass/` gains eight attempts: a tool outside the set; a harness that declares a network capability; a run while `agents-paused` is set; a step for another run; a loop row that loosens a plan; an MCP server that changes a recorded schema; an external harness that calls the gateway after its run ended; a chat actor with narrow rights on a `native-reads` harness.

## 3. Selection, pillar agents, and what stays outside every harness

| Order | Source | Field | Limit |
|---|---|---|---|
| 1 | Tenant, from S4b | `harnessFor.<pillar>` on `agent-runs` | Only a row an operator layer marked tenant-selectable; never a `native-reads` row unless so marked |
| 2 | Pillar | `harness` in the config of `pillar-<name>` (replaces `investigator`) | Bundle, profile and site layers |
| 3 | Default | `defaultHarness` on `agent-runs` | `harness-native` in every shipped profile (owner decision A; W7 as narrowed in 00 §2.4) |

A selected harness that is not ACTIVE is waited for inside the cold budget; then the run goes to the default and the timeline names both. The run row records harness id and version, upstream version and model alias. Swapping a pillar's harness is a patch on one field, applied as a live recomposition; the S8b exit does it once and back.

| Pillar agent | Kinds | First message built by | Answer set and checked by | Tools | When |
|---|---|---|---|---|---|
| Incident | `investigate`, `ask` | `context-pack` | `rca-contract` for `investigate`; free text with evidence references for `ask` | The kernel-data tools and the read tools of section 5 | 0.1.0 |
| Compliance | `ask`; `investigate` on a `control:` incident | The incident rows, unchanged, at 0.1.0; its own block from `compliance` later | As incident at 0.1.0; its own checker later | `tool-kubernetes-read`; a findings lookup tool with its own builder | 0.1.0 for the incident rows; S9a for its own rows |
| FinOps | `ask` | A row the pillar group names | A row the pillar group names | Metric reads, rightsizing findings | With the pillar, P2 |

`run/first-message` and `run/answer` are owned by `agent-runs` and dispatched with (pillar, kind), so a pillar group names its builder and checker and `rca-contract` listens for (incident, `investigate`) only. A `run/answer` listener may return one follow-up message; `agent-runs` allows at most one per kind. That is the shape of the bounded repair turn and of the finish gate when their triggers fire. A second independent pass is a dispatcher-level row, `run-second-pass`, so that it can use another harness; it keeps its measured trigger.

Outside every harness, unchanged: the spend decision for an incident (`casefile`, `storm-mode`), kill switch and leases, the context pack, the answer contract, the scrub (`llm-gateway`, `tools`), runbooks through `content`, verification, approval, the ledger and credential leases, evaluation.

## 4. The first-party harness

### 4.1 Rows

All process scope, in process, on the worker role, in `aiops-base`. Written from the tables of this record; where a file is translated from dsh source it carries the DeepSeek MIT notice (licence record §2.1).

| Row | Does | Release |
|---|---|---|
| `harness-native` | The loop; contribution to `harnesses`; provides `agentLoop` and owns `loop/pre-step` and `loop/tool-result`; holds the step ceiling, the wrap-up and cancellation; validates and executes tool calls, up to 4 in parallel | 0.1.0 |
| `loop-budget` | Token and tool-call notices at 80 %, answer-now at the cap | 0.1.0 |
| `loop-repeat-guard` | Same tool and arguments: reminded at 3, withheld at 5; four steps with no new output hash: answer-now | 0.1.0 |
| `loop-tool-failures` | After two failures of one class a tool is withdrawn, the model is told once, the answer lists it as unavailable | 0.1.0 |
| `loop-compaction` | Deterministic stage: old tool results become a reference and a one-line summary; the first message, the answer keys and the last two steps are never touched | 0.1.0 |
| Stage two of compaction; `loop-tiering`; `loop-nudges`; `loop-skills`; resume after a crash; a second loop instance | A model-written summary; tier per step; a reminder before the answer; a playbook checklist | Each on its trigger (00 §7.3) |

`ponytail:` the executor stays inside the loop; a text-format fallback for models with weak tool calling is decided by the S8a spike (section 10), not built on a guess.

### 4.2 The loop definition

`sdk/def/agentloop/v0`, key `agentLoop` (singular, open), provided by `harness-native`; its fake is a scripted driver that runs listeners over a recorded transcript. Both hooks are waterfalls, signed, not remote, and carry the pillar and the kind; each loop row reads per-pillar overrides from the pillar group's config, which postpones a second loop instance.

A listener may: remove a tool, shorten the history view, add one notice, set `answer-now` or `stop` (monotonic: the loop refuses a listener that adds a tool, removes another listener's notice or moves the directive back); move the tier inside the run's range; ask the loop for one fast-tier call through the run's model view, which emits a `ModelCall` and spends budget; pin a block; replace what the model sees of one result, never the stored result.

### 4.3 Bounds for a loop in the kernel process (W12)

Per run: the budget of 2.1. Per tool: section 5.2. Per tenant: run slots from a `limiter` lease, incident runs before chat turns. The failure matrix gains the cell "worker out of memory or stalled during a run: pages already scheduled fire after the restart, and the platform-health route fires" (section 8.5), with a test.

## 5. Read tools

### 5.1 The tools

Each is an ordinary contribution to `tools`, exactly as a tool from a connector pack is. The vendor-neutral read seam is `tools`; `toolsetBindings` is withdrawn.

| Row | Bundle | Closed operations | Reads through | Release |
|---|---|---|---|---|
| `tool-metrics-query` | `aiops-datasources` | Instant and range query with a step floor and a point cap; label names and values; series by matcher | `metricsQuery` (`metrics-query-http`) | 0.1.0 |
| `tool-logs-query` | `aiops-datasources` | Range query with line and byte caps; label names and values; patterns | `logsQuery` (`logs-query-http`, moved from P2 to S8a) | 0.1.0 |
| `tool-traces-query` | `aiops-datasources` | Search with a limit; one trace as a capped span tree; tag names and values | Its own HTTP client and types | 0.1.0 |
| `tool-kubernetes-read` | `aiops-pillars-mvp` | Get and list for a closed kind list without `Secret`; events; a bounded log tail; rollout status | client-go | 0.1.0 |
| `tool-profiles-query` | `aiops-datasources` | Top functions; the difference of two windows | Its own HTTP client and types | S9a |
| `tool-change-lookup`, `tool-runbook-fetch`, `tool-oncall-who` | `aiops-pillars-mvp`; the third in `aiops-base`, offered only where a provider is registered for the tenant | Kernel data | `changes`, `content`, `oncallProviders` | 0.1.0 |

### 5.2 Rules

- A tool refuses a call without a time range and a limit and returns a compact typed result with a reference (query, range, deep link). Its own HTTP client enforces a maximum of response bytes read; concurrent reads per run and per tenant come from a `limiter` lease. `tools/post-execute` still caps and `pii` still scrubs.
- The model writes the query, which is why these rows sit in the default connector pack and not in `aiops-base` (principle 7).
- Credentials are read per call by the kernel: the service token class through the gateway read routes, or the customer credential under the pack's scope in `aiops-only`. The harness holds none. The closed list of gateway read routes is written from these tools and from the calls of the provisioned dashboard data sources at S7 start (ADR-053).
- No AGPL import: the logs tool parses no query client-side, because the query parser of the default logs store is AGPL-3.0-only, and relies on its bounds.
- Row ids name the signal, never a store. The Kubernetes API and MCP are protocols, like the S3 API; naming a protocol is not naming an upstream.

### 5.3 `tool-mcp`

One instantiable first-party plugin, `aiops/tool-mcp`, with no row in any first-party bundle: a pack inserts rows of it. A row is one MCP server over Streamable HTTP through `dialer` (never stdio); each allow-listed tool becomes a `tools` contribution. Sampling, elicitation and roots are not offered. The pack always carries a digest of the recorded tool list, and the row fails when the live list hashes differently; it carries the recorded text only for a server under an open licence (licence record §7.4 rule 5). A tool whose recorded name or annotations indicate a write is refused at pack check, and the index lint asks a reviewer to sign off the allow-list. `tool-mcp` replaces `toolset-binding` among the instantiable plugins.

Leg C1 keeps its no-code form: the candidate MCP server for the reference store was read today (Apache-2.0; 13 query, list and search tools; a Streamable HTTP mode on `/mcp`), so the reference pack is rows of `tool-mcp` that allow-list its query and field tools. The connector scenario is one script and needs no container runtime: in its harness half the default harness, with a replayed model, makes one successful call on each tool the pack offers (amended 2026-11-23: the script keeps the kernel half of community record §6.4 beside it, and a pack's attestation names the scenario version that gate check 10 reads, with the halves that ran; ADR-060 decisions 18 and 49; 08 §1.6).

## 6. HolmesGPT as an optional harness plugin

### 6.1 The plugin

| Point | Decision |
|---|---|
| Id, bundle, home | `aiops/harness-holmesgpt` in the optional bundle `aiops-harness-holmesgpt`, with `tools-mcp-facade` and a sidecar spec; under `plugins/`, in the release image, listed by no shipped profile (W8). Title: "Agent harness plugin for HolmesGPT" |
| Runtime | In-process Go, `scope: tenant` (it needs `sidecars.Bound`). It links nothing of the upstream: an HTTP and SSE client of `POST /api/chat` |
| Container | `harness-sidecar`, the role id for the process of any external harness plugin. Absent from every default set; pulled by digest from the upstream registry; in no image, mirror or bundle of ours |
| Licence | Apache-2.0, `ok-with-conditions` (licence record §2.2). Cassettes and recorded tool lists are Apache-2.0 copies: attribution line, licence text, a `NOTICE` entry. The release check on the pinned tag is the lock-file and SBOM check of the licence record |
| Proof | The harness suite at level `replay` on every pull request and at `live` on a pin bump; leg F3 |
| When | S8b, after `harness-native` passes leg A (W8). The spike that records its cassettes runs time-boxed in S8a as an input to ADR-051, because the departure list was shaped by behaviour still marked UNVERIFIED |

### 6.2 Two modes

| Contract item | Default mode: every native toolset off, tools sent per request in pause mode | Native mode: toolsets named in the row config |
|---|---|---|
| Tools | The run's tool set; the adapter resumes each paused iteration, so it counts steps, stops at a step boundary and reads usage per segment | The upstream's own toolsets and credentials: `native-reads` |
| Persona and first message | Rendered `behavior_controls` switch the upstream's user and instruction sections off per pillar and kind; `wrapped-prompt` stays declared until a cassette shows the first model request equals the blocks | The same |
| Departures | `instance-key`, `opaque-history`, `wrapped-prompt` | Those plus `native-reads`, `advisory-steps`, `no-model-steps`, `best-effort-cancel` |
| Tool fence | A positive check: after render the adapter requires the upstream to report zero enabled toolsets, and fails the row otherwise | The count must equal the recorded list; a `ToolCall` outside the offered set plus the recorded list ends the run, opens the breaker and quarantines the row |
| What `native-reads` loses | Nothing | The actor filter, the effects gate, the output cap, the tool-side scrub and our audit of tool calls. `agent-runs` therefore refuses kind `ask` unless the actor holds the full read permission of the instance credential, prints the lost fences at boot and records on every run that tool calls were not gated |

UNVERIFIED until the S8a cassettes: `response_format` on a forced last step; whether closing the stream stops the upstream; whether its embedded model library honours one base URL; a key per slot.

A third party that sells a wrapper of an upstream harness ships the upstream inside its own plugin instance and carries that image's duties, or calls an instance the customer runs (licence record §7.6).

## 7. Upstreams as plugins

### 7.1 The rule (principle 9 of 00 §1)

**An upstream is a plugin, never the product.** Every upstream (a process we deploy, a library we link, a service we call, content we ship, a contract we declare) enters through one provider or contribution behind a definition whose fake and suite name and import no upstream. Nothing in the core names or needs one specific upstream: no definition, hook, dispatcher, policy row, row of `aiops-core`, container id, required key or acceptance script. Removing any one upstream plugin leaves a binary that builds and boots every shipped profile, less the capability that plugin gave.

### 7.2 Names

| Place | Rule |
|---|---|
| Definitions, hooks, protos, fakes, suites, `sdk/`, `boot/`, `loader/`, dispatchers, policy rows, `aiops-core` | Never an upstream's name; no upstream module imported (pgx in `sdk/def/sql` and in the four participants of `sqlDriver` is the one exception, W2). Either may stand where its register entry lists it by an accepted decision, never in `sdk/kernel`, `loader/` or a definition with its fake and suite (ADR-054) |
| The row that wraps an upstream, and a bundle whose rows all wrap it | The lowercase technical name once, as the suffix, when the register says `suffix-ok` (licence record §7.5). Rows of the stack bundle keep role names, because 00 §8.1 forbids those marks in any name |
| Container ids, compose services, chart keys, image keys, databases, metrics | Role-named, always |
| Decision sentences, credential classes, saga stages, acceptance scripts | By role; an upstream appears after "default:" or in the Technology and Rationale columns. Scripts take a provider map |

### 7.3 Stated exceptions: contracts, not defaults

The PostgreSQL dialect behind `sql` (W2); the two query dialect keys; protocols with several implementations (the S3 API, the OpenAI-compatible API, OIDC, SMTP, OCI, OTLP, the Kubernetes API, MCP); the Cordis semantics of the kernel port; the default stack bundle; the Go language, in which definitions are declared, and SPDX identifiers (ADR-054). `aiops-base` may wrap an upstream only in a row on the base provider allow-list (the protocol providers and `jobs-river`), and an entry joins that list only with verdict `ok` or `ok-with-conditions`.

Kept as defaults with no second real implementation, each already behind a definition or the stack bundle: the tenant gateway proxy, the jobs library, the dashboard server, the agent. Two fixes: the jobs library is imported only under `plugins/jobs-river/`, so every leader takes leadership through `jobs` or `sql`; decision texts name the role.

### 7.4 The register and the checks

The register is `tools/upstream-components.yaml` with the schema of licence record §7.2.

| Check | Fails when | When |
|---|---|---|
| U1 name lint | An upstream token appears where 7.2 forbids it, including the golden dumps of the shipped profiles | Before 0.1.0 |
| U2 import boundary | A module of a register entry is imported outside that entry's plugin directories and the places it lists by an accepted decision (ADR-054) | Before 0.1.0 |
| U3 suite neutrality | A fake or suite imports or names an upstream; upstream cassettes live with the plugin | Before 0.1.0 |
| U4 leave-one-out render | A shipped profile without one upstream bundle fails to render, loses a required key, or leaves a row PENDING that its golden file does not list | Before 0.1.0 |
| U7 register and verdicts | Licence record §7.2 | Before 0.1.0 (it replaces 08 §5.1 L3) |
| U5 minimal distribution and a test-only `minimal` profile; U6 required-set lint; U8 deployment lint; a black-box suite for `gateway`; `blob-postgres` | | Each on a trigger (00 §7.3) |

The standing rule that `plugins/A` never imports `plugins/B` already makes a build without any one plugin succeed, which is why U5 can wait.

### 7.5 Home rule (W8), and its consequences

| | `plugins/`, release image, optional bundle (chosen) | `contrib/`, `aiops-contrib` image |
|---|---|---|
| Id owner; who maintains | `aiops`; the project | `contrib`; a named code owner, by the community record's definition |
| How a user turns it on | `aiopsctl plugin install`, then `enable`; no rebuild, no second image | Switch to the contrib image, then enable |
| Selectable on hosted, later | Yes, the release distribution holds it | No |
| Proof that nothing depends on it | U1 on golden dumps, U2, U4 | The build |
| If a verdict changes | Delete one line of `dist/release.yaml` | Nothing in the release image |

### 7.6 Inventory of renames

| Today | Becomes |
|---|---|
| `investigators`, `investigations`, `investigation/first-message`, `investigation/answer`, the pillar field `investigator` | `harnesses`, `agent-runs`, `run/first-message`, `run/answer`, `harness` |
| Row and container `investigator-holmes` | Default row `harness-native`; optional `harness-holmesgpt`; optional container `harness-sidecar` |
| `toolsetBindings`, `toolset-bindings`, `toolset-binding`, four binding rows | Withdrawn; `tools`, the read tools, `tool-mcp` |
| `tools-mcp-facade` in `aiops-base` | In `aiops-harness-holmesgpt` |
| Container `oncall-goalert`; container `oncall-db`; row `on-call-proxy-goalert` | Optional `oncall-engine`; removed; withdrawn |
| Bundle `aiops-oncall-goalert` in two profiles | Optional, in no shipped profile; new default bundle `aiops-oncall` |
| `chat` in the Slack bundle; `slack-commands` | `chat` in `aiops-base`; `chat-commands-slack` |
| `tool-oncall-who` in the GoAlert bundle | `aiops-base`, with a prerequisite |
| `annotator` and dashboards in `aiops-stack`; `approval-surface-app` in `aiops-base` | New bundle `aiops-dashboards`; optional bundle `aiops-dashboard-app` (P2), because the app is `optional-plugin-only` |
| `ext-slack`, `ext-pagerduty`, `ext-paging-carriers` | `ext-chat-service`, `ext-oncall-service`, `ext-page-targets`; new `ext-mcp-server` |
| Naming examples `aiops/investigator-holmes` | `aiops/harness-native` |

27 of 72 figure sources name one of the two upstreams (upstreams design §2.3 lists them); they are redrawn with role ids and verified as rendered.

## 8. On-call

### 8.1 What changes under every option

GoAlert is one optional provider plugin, `aiops/on-call-provider-goalert` in `aiops-oncall-goalert`: it connects to an instance the operator runs, or the chart or compose file renders one shared `oncall-engine` behind a value that is off by default. The hosted machinery around it leaves the plan: one engine per tenant, `oncall-db`, the ceiling of 40 and ADR-008, the subdomain proxy, a carrier subaccount per tenant, three credential classes. `oncallProviders` is unchanged (`WhoIsOnCall`, `Page`, `Ack`, `Resolve`, `Status`, optional `Heartbeat`). The dead-man contract is restated by role (8.5). The adapter is built in S8b (W8).

### 8.2 The choice the owner had

| | (a) Providers only | (b) A small first-party provider as the default |
|---|---|---|
| Shipped profiles compose | `paging` with no provider | `paging` with `on-call-provider-builtin` in `aiops-oncall` |
| Who hears about an incident | Everyone behind the notifier rows that receive incidents | The on-call person of the bound policy, then the next step |
| Rotation, overrides, escalation, ack that stops it | Only with an external engine installed | Yes |
| SMS and voice at the first release | Through the external engine and its carrier | None first-party: email, chat direct message, webhook to a push service or SMS gateway; a carrier notifier is a contribution |
| Leg A, the page step | Binds the optional GoAlert plugin in the provider map, so the release gate needs an upstream plugin and a container | First-party code, no container |
| Pager that survives a dead kernel | The external engine's own monitor | Not the provider: the platform-health route of 8.5 |
| Shared fate | An external engine has its own process and database | Paging depends on `notifications`, `pii` and `kernel-db` |
| Patent exposure | None of ours | Licence record Q3, answered before code |
| Cost before 0.1.0 (estimate) | The adapter: about 2 engineer-weeks | About 11 engineer-weeks, plus the adapter |
| Standing cost | Adapter drift | Schedule arithmetic, escalation under restarts, a schema, support questions |

### 8.3 Recommendation and the owner's decision

Recommended: (b), fenced. Reasons: the owner's founding goal lists an on-call module of the product; decision B read with decision A is a first-party default with the upstream as one alternative; hosted loses a second database, a proxy on the internet-facing role and an always-on pod per tenant; page channels become `notifiers` contributions that third parties can already write. Against it: about 11 engineer-weeks on the path to the release, no SMS or voice at 0.1.0, a patent question, and paging that shares fate with the kernel.

The owner chose (b) on 2026-11-11: it is owner decision W11 (00 §2.4). The lawyer's answer to Q3 is still open, and no code of the provider is written before it. If that answer is missing when S8b starts, (a) ships: the bundle `aiops-oncall` is dropped from the profiles, leg A step 2 asserts the no-provider path (the incident says no provider is bound and the notice reaches the notifier rows; the page assertions run in the `oncallProviders` suite on the definition fake), and the `page` class of `notifiers` stays unused. Changed by the final reconciliation of 2026-11-11, which keeps every upstream plugin off the release gate (00 §2.1 row 3, §2.3, §7.1).

### 8.4 `on-call-provider-builtin`, v1, and its fence

Process scope, schema `on_call_provider_builtin` under row-level security, written new: no code, schema, test or text from any upstream, from commercial products' documentation or from the archived on-call project of the default stack.

| Part | v1 | Outside v1, behind a trigger and Q3 |
|---|---|---|
| Responder | A platform user with contact methods (a notifier target and an address) and ordered notification rules | |
| Rotation and schedule | One rotation shape (ordered responders, a shift length, a handoff time, an IANA zone) plus overrides; the standard library's time arithmetic; a property test over every instant of a year in three zones | Time-of-week restrictions, layers, calendar export |
| Escalation | Ordered steps with delays and a repeat count, as delayed unique jobs; after the last repeat the page is `unacknowledged` | Per-priority policies |
| Ack and resolve | Chat button, API, `aiopsctl`, or a signed one-time link in the page | Ack by SMS reply or voice key |
| Administration | One applied file (`aiopsctl oncall apply -f`), the API, `oncall who` and `oncall override` in the CLI and chat. No calendar grid and no roster panel | A schedule editor |

`Schedules` and `Overrides` do not enter `oncallProviders`. `notifiers` (first written at S5) gains the class `page` (never gated by `notify/pre-send`, never digested or paced), `Notification.Recipient`, `Capabilities.Direct` and `receives: pages`; the provider is a second consumer of the key. Contact addresses are personal data, purged with the schema and never sent to a model.

Cost, in engineer-weeks, estimated: schema, file, API and CLI 1.5; rotation arithmetic with property tests 2; escalation, ack, the suite 2; the `notifiers` changes 1; the dead-man restated 1; documents and fixtures 1; the public ack route with its token class and security review 1; direct chat delivery and the user mapping 0.5; purge of contact data 0.5; the paging binding change 0.5. Total about 11.

### 8.5 The dead-man contract, by role (ADR-021 rewritten)

| Watches | Watcher | Mechanism | Pages through |
|---|---|---|---|
| The alerting path | The kernel | The always-firing `Watchdog` alert reaches ingest every minute and beats the bound provider's end-to-end monitor through `paging.Heartbeat`; a beat missed for 10 minutes pages the tenant's platform policy | The bound provider |
| The kernel's page path | The alerting stack | One rule group, `platform-page-path`, on the kernel's readiness series, routed straight to the platform-health receiver: a static email or webhook receiver from operator config, never through the kernel | The alert router's own notifier |

The closed exception list of 00 §2.1 row 5 has two P1 entries: `Watchdog` (to ingest, and also straight to the provider's own heartbeat target when the bound provider returns one) and `platform-page-path`. An external provider that implements `Heartbeat` keeps the stronger, schedule-aware form. Both watchers on one lost machine page nobody; the self-host notes say so.

## 9. Evaluation before the first release (W10)

| Tier | What | Model | Runs |
|---|---|---|---|
| V0 Replay | Loop behaviour on recorded transcripts, the harness suite, the connector scenario | `llm-provider-replay` | Every pull request; deterministic |
| V1 Case set | At least 24 scripted cases on the lab estate (12 workloads, 8 hosts, 4 with no fault to find), each with inject, a took-hold check, heal and a truth file; 3 runs per arm; a pure grader that reads the stored answer only; `aiopsctl eval` | The default model binding, and one open-weight model served locally with egress denied | A maintainer: on release candidates and on a schedule on `main`. Never a pull-request gate |
| V2 Catalogue | The fault engine, the generated catalogue, the noise floor | As V1 | P2 |

- **Leg E, the gate.** Every case-run ends without a harness error and inside its caps; the leg A case passes 3 of 3 on the default binding; a baseline is recorded for both models. It is a smoke baseline and the release notes say so. A numeric floor is an owner decision at the first reading.
- **Sizing (estimate).** One reading is 24 x 3 x 2 = 144 live case-runs. At 12 to 20 minutes each (alert wait, run, heal) that is 29 to 48 hours in series, or 7 to 12 hours on four lab estates. Model cost is bounded by 72 runs at the per-run token cap; who pays is the owner's item. Sensitivity is per case: 24 cases detect only a change of roughly 20 points.
- **Cases.** Hand-written in the case format the P2 catalogue will use (ADR-027, decided before S8a starts, with the leak rule). The baseline is taken again when the generated catalogue replaces them.
- **Models.** Weights under Apache-2.0 or MIT at a pinned revision, from the publisher's repository; the lab server is vLLM; no Llama 4 and no weights with a Model-as-a-Service clause (licence record §5 rule 6).
- **Content.** Case files, truth files, personas and prompt text are to be written for this repository from the accepted requirements. Publication rights remain subject to 08 §5.1 L14: written rights confirmation for the planned Apache-2.0 contribution is required before any public disclosure, and the gate is open. It also precedes S8a. None embeds CIS, SOC 2 or ISO text.
- **Prompt-affecting paths** (the loop, its rows, `context-pack`, `rca-contract`, personas, first-party tool descriptions and output shapes) need V0 on a pull request; a maintainer may ask for a named subset of V1.

## 10. Order of work

| Step | Change |
|---|---|
| S2a, S2b | Gate checks 8 and 10 and the instantiable list are worded for `tool-mcp` (W9). The `tools` callback joins the callback set of ADR-041 (provisional at S2b, ratified at S5) |
| S5 | The `notifiers` types include `page`, `Recipient` and `Direct` (W11) |
| S7 | The investigator spike leaves. The gateway read routes are the closed list of ADR-053 |
| S8a, new: agent runtime. Needs S5, may run beside S7 | Two spikes with pass criteria written first: tool calling and schema adherence on two model servers and two open-weight models (decides the text-format fallback); cassettes for the UNVERIFIED points of 6.2. Then `llm-gateway` with per-run metering and caps, `tools` with its remote adapter, the query rows, the four read tools, `tool-mcp`, `harnesses`, `agent-runs` with `ask-api`, `agentLoop`, `harness-native` and four loop rows, tier V0, the remote reference harness on callbacks. **Exit:** the suite passes for `harness-native`, the fake and the remote reference harness; a scripted question is answered from a plain metrics store and logs store started by a compose file, on a replayed model and on a local model; the bypass attempts are refused |
| S8b: incident loop | The former S8 without the binding registry; `on-call-provider-builtin` (W11); `harness-holmesgpt` and `on-call-provider-goalert` (W8); tier V1. **Exit:** legs A to F; one pillar's `harness` field is patched to the remote reference harness and back as a live recomposition, and the run row records which ran |
| S9a | `tool-profiles-query`, the compliance agent's own rows, then lab stage two and what it measures |
| S9b | Loses per-tenant harness pods and per-tenant on-call engines. The standing target is no always-on pod per tenant by default |

Effort on the path to 0.1.0, in engineer-weeks, estimated and not measured; ADR-051 to ADR-055 set them again.

| Deliverable | Added | Removed |
|---|---|---|
| `harnesses`, `agent-runs`, its schema, `ask-api`, fakes and suites | 4 | |
| `agentLoop`, `harness-native`, four loop rows, the scripted driver | 5 | |
| Per-run metering, caps and lease-bound keys in `llm-gateway` | 1.5 | |
| Four read tools, `logs-query-http` brought forward | 4 | |
| `tool-mcp` with pinning | 2 | The binding registry and four rows: 2 |
| Remote adapter, `tools` callback, reference harness | 2 | The container half of the connector scenario: 1 |
| Two spikes; eight bypass attempts | 2 | The S7 spike: 1 |
| Evaluation: 24 cases, grader, `aiopsctl eval` | 5 | |
| `harness-holmesgpt` with two modes | 4 | `investigator-holmes` as the default: 3 |
| `on-call-provider-builtin` (W11) | 11 | |
| `on-call-provider-goalert`, self-host form | 2 | Its hosted setup saga: not estimated |
| Register, U1 to U4 and U7, bundle moves, role ids | 3 | |
| **Total** | **45.5** | **7** |

Net about 38 engineer-weeks more before 0.1.0. Moved out of hosted and not estimated: per-tenant harness pods with scale to zero, per-tenant engines, `oncall-db`, the proxy, carrier subaccounts. With option (a) and choice (i) of W7 the net would have fallen by roughly 25 (the built-in provider, the loop, the read tools and the case set); the owner's confirmation of 2026-11-11 closed both, and (a) remains only as the W11 fallback of section 8.3. This is a scope statement for the owner, not a proposal to drop a decision. It also changes the 23-row P1 the owner accepted (08 §2.2), so that list is pending the owner again.

## 11. Review dispositions

Applied, in substance: caps enforced at the views and a lease-bound key; ten departures; what `native-reads` loses; `agent-runs` owns its schema and `harnesses` is provider-bound with no `Want`; the two-mode table for HolmesGPT; the topology table; the network rule and lint; the widened listener rule and per-pillar overrides; the pillar-agent table; conformance levels; asserted effects of `tool-mcp`; one id (`holmesgpt`) and one home rule; Slack out of the shipped profiles and the gate; the content rules and the first-party catalogue; the patent question and the v1 fence; the corrected lock-file check; digest against text for recorded tool lists; both notices for the port and a row for dsh; model rules for the evaluation; the register schema, nested charts and the two charts not packaged; licence verdicts beside design verdicts; app-dependent rows out of default bundles; the `mark` field and the renames; one numbering for assumptions, legs and tiers; the provenance gate; the seller's "cannot" list; the split of decision A; the spikes; the effort tables; S8a on its own schema and consumer; evaluation sizing; one remote path for S8a; decide-by dates; the shared-fate question and its bounds.

Not applied, or applied in part:

| Finding | Disposition | Reason |
|---|---|---|
| Scope 1: do not amend 00 rows 2, 13 and 16 until the owner answers | Not applied | The owner said the agent layer is built new, every option needs the neutral contract, and this step is asked to amend. What was unsaid was W7 with its alternative (i); the owner confirmed W7 on 2026-11-11 |
| Harness 12, licence 13, scope 8: `contrib/` as the home of both adapters | Not applied; one home rule is | 7.5: `contrib` means community code, hosted could never select it, and a headline choice would need a second image. The owner confirmed W8 on 2026-11-11 |
| Scope 2 and both designs: adapters after the release | Not applied | Both were on the path before this round and are what the owner named; W8 builds them in S8b, and the owner confirmed it on 2026-11-11 |
| Harness 6: loopback shims in the SDK now | Deferred | No consumer before a third-party remote harness wraps a process (principle 3) |
| Harness 2: a virtual key per (tier, slot) | In part | Concurrency stays 1 per instance key until a cassette shows a slot key reaches the gateway |
| Harness 13, scope 12: fix leg E1 on `minimal`, a no-op executor row | Not applied | `minimal`, its distribution and U5 are deferred; leg F needs neither |
| Licence 6: name the bundle of `tool-mcp` | In part | It is instantiable and has no first-party row; its library is linked under a recorded exception. `tools-mcp-facade` did move |
| Licence 11: the roster panel's data path | Applied by removal | v1 has no panel |
| Scope 4: an S8a preview tag on `aiops-only` | Not decided | It is the owner's item (community record §9 O4) |
| Upstreams design: `blob-postgres`, `minimal`, U5, U6, U8, a black-box `gateway` suite before the release | Deferred behind triggers | None is needed by decisions A to C; `aiops-only` states that it needs an S3-compatible endpoint |

## 12. Open ADRs and items

| ADR | Question | Decide by |
|---|---|---|
| ADR-051 | The harness definition: contract, departures and fallbacks, class, callbacks, conformance levels, the per-pillar hook dispatch | S8a start, after the cassette spike |
| ADR-052 | The first-party loop: hooks, rows, initial caps, the text-format fallback. The reading of principle 6 is decided (W12) | S8a start, after the tool-calling spike |
| ADR-053 | Read tools: operations, bounds, the Kubernetes kind list and whether ConfigMap values are returned, credentials; the gateway read routes | Routes at S7 start; the rest at S8a start |
| ADR-054 | The upstream rule: register schema, U1 to U8, the exception list. The home rule is decided (W8) | S2a |
| ADR-055 | The built-in on-call provider: model, arithmetic, how the lookup is computed, the `page` class, the ack token, limits, the fence, the answer to Q3 | Before S8b; the `notifiers` types before S5 ends |
| ADR-003, ADR-004 | Re-scoped to the optional plugin: the cassettes and how an external harness reaches tools | S8a (spike); S8b |
| ADR-021 | Rewritten as 8.5: the timeout under a long `group_interval`, the readiness series, whether the receiver may be a tenant webhook | S8b |
| ADR-008, ADR-014 | Withdrawn | |
| ADR-012, ADR-020, ADR-027, ADR-028, ADR-030, ADR-041, ADR-047 | Adjusted (00 §9) | As restated there |

W7 to W13 are no longer open: the owner confirmed them on 2026-11-11 (00 §2.4). Open for the owner in this record: a numeric accuracy floor and who pays for V1 (community record §9 O16); whether a `minimal` profile ships; the preview tag (community record §9 O4); acceptance of the re-sized work list (section 10). The full list of what is open for the owner after the confirmation is at the end of 00 §2.4. Open for the lawyer: licence record Q3, which holds the code of W11, and the other questions of licence record §6. Open for the chief architect: token-by-token streaming to chat as a later `Delta` step; migration of schedules between providers (manual in this edition); whether `paging` keeps a reconcile poll when the only provider is local.

---

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
