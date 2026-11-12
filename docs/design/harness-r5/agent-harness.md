# Agent harness as a plugin point, and the first-party harness (round 5)

Status: design proposal, 2026-11-11, for the owner decisions of this round: (A) the agent layer is built new and HolmesGPT is one optional agent-harness plugin; (B) GoAlert is one on-call provider plugin, which another design of this round covers; (C) licences are reviewed. `docs/architecture/00-decisions.md` (cited as `00 §n`) wins until it is amended; `docs/design/plugin-system-2026-11.md` is "plugin record §n", `docs/design/community-first-2026-11.md` is "community record §n", and 05, 07 and 09 are files of `docs/architecture/`. Every licence and upstream fact marked "read today" was read on 2026-11-11 at the source in section 12; the rest is marked UNVERIFIED. This is engineering analysis, not legal advice; section 10 lists the questions a lawyer must answer.

## 0. Answer in short

1. **One contract for every pillar agent.** The registry `investigators` becomes `harnesses`, and its dispatcher `investigations` becomes `agent-runs`. A harness takes one task (a persona, a first message built by code, an offered tool set, a budget) and returns a stream of steps and one answer. Incident investigation, a chat question and a compliance or FinOps question are the same call with a different task.
2. **The default harness is ours and new.** `harness-native` is a loop plus four small listener rows (budget, repeat guard, tool-failure memory, compaction), in process, in `aiops-base`. It reaches a model only through `llm` and a tool only through `tools`.
3. **The method stays outside every harness.** The context pack, the answer contract, the finish gate, the second pass, budgets, policy gates, the transcript and verification are rows of ours around the call, moved from `casefile` to the dispatcher, so that any harness a user selects gets them and a chat turn gets them too.
4. **The product now owns its read tools.** `tool-metrics-query`, `tool-logs-query`, `tool-traces-query` and `tool-kubernetes-read` ship in the first community release as ordinary `tools` contributions; `tool-profiles-query` follows. A connector pack contributes tools the same way.
5. **`toolsetBindings` is withdrawn.** Its only consumer was the HolmesGPT adapter. The vendor-neutral read seam is `tools`, and the no-code path for a pack is one instantiable row, `tool-mcp`, that mounts an allow-listed set of tools from an MCP server.
6. **HolmesGPT is `harness-holmesgpt`:** an optional plugin outside the release image and outside every shipped profile. It honours the contract with six stated departures. Nothing in `sdk/`, `aiops-base` or a default bundle names it.
7. **No licence forces any of this.** HolmesGPT is Apache-2.0 (read today). The one thing the new design cannot use is the query parser of the default logs store, which is AGPL-3.0-only; the logs tool therefore validates nothing client-side and bounds its reads instead.
8. **We now own answer quality, so evaluation moves before the release.** A case set, a pure grader, `aiopsctl eval` and a recorded baseline are part of the first community release, not of P2.
9. **Cost of the decision.** S8 grows by a definition, a dispatcher, the loop, four read tools and the evaluation stage, and is split in two. It loses the upstream spike, the adapter, the binding registry and one container. Hosted loses the per-tenant harness pod.

## 1. Terms, and what changes

| Term | Meaning |
|---|---|
| Pillar agent | A pillar group's persona, tool set and harness selection. Not a row type: it is the config of `pillar-<name>` |
| Harness | A contribution to `harnesses`: the thing that turns one task into model calls and tool calls and returns an answer |
| Run | One call of a harness for one task, with one transcript |
| Loop row | A listener on the hooks of the first-party loop |
| External harness | A harness whose loop runs in an upstream process that a plugin of ours or of a third party drives |

| Today (00 as it stands) | This design |
|---|---|
| `investigators` (open key), dispatcher `investigations`, methods `Investigate` and `Ask` | `harnesses`, dispatcher `agent-runs`, one method `Run` with a task kind |
| `investigator-holmes` is the only contribution and the MVP default; "there is no native loop" (row 16) | `harness-native` is the default; `harness-holmesgpt` is optional |
| `casefile` owns `investigation/first-message` and `investigation/answer`; chat calls the dispatcher directly and skips both | `agent-runs` owns `run/first-message` and `run/answer`, so every caller gets them |
| Telemetry is read by HolmesGPT's native toolsets, selected by `toolsetBindings`; credentials sit in the investigator container | Telemetry is read by first-party `tools` contributions in the kernel process; the harness holds no credential |
| Three kernel-data tools reach the investigator through `tools-mcp-facade` | Every tool is called in process through the `tools` dispatcher; the facade is off by default and serves external harnesses only |
| Container `investigator-holmes` in every profile; per tenant in hosted | No harness container in any default set |
| Lab: one live case in P1, grader and catalogue in P2 | A case set, the grader and a baseline before the first release |

## 2. The harness definition

### 2.1 Contract

`sdk/def/harnesses/v0`, with `definition.yaml` at `0.1`, a fake (scripted steps) and a conformance suite. The key is a registry. Its three roles exist from the first day: the definition, the provider `agent-runs`, and the consumers `casefile` (incident runs) and `chat` (questions).

```go
// sdk/def/harnesses/v0. Registry, provider-bound to agent-runs (section 2.7). Remote: contrib, from S8a.
type Harness interface {
	Describe() Descriptor                                         // id, version, upstream, departures: static, read without a run
	Run(ctx context.Context, req *RunRequest, out StepSink) error // one task; the tenant is the handle in ctx
}
type Registry interface {
	Register(c *kernel.Context, name string, h Harness) error // an effect of the caller's fiber
	Dispatch(ctx context.Context, t Task) (*Outcome, error)   // the only way to reach a harness; runs inside the caller's job
	Inventory() []Entry
}
type RunRequest struct {
	Run          RunID
	Task         Task       // Kind (investigate | ask), Agent (pillar id), Subject (entity reference or incident), Trigger
	Instructions string     // the pillar persona as text
	FirstMessage []Block    // built by code on run/first-message; sent first, unchanged, in order
	History      []Message  // earlier turns in the neutral form; empty on a first turn
	Resume       []byte     // this harness's own history from its previous turn; opaque to everything else
	Answer       AnswerSpec // JSON Schema and required keys; empty means free text
	Tools        ToolSet    // the only tools the run may call
	Model        ModelAccess // the only way to a model
	Budget       Budget
}
type ToolSet interface {
	Specs() []ToolSpec                                              // Name, Description, Params, Signal, Dialect, ScopeNote
	Invoke(ctx context.Context, call ToolCall) (*ToolResult, error) // in process: through the tools dispatcher
	Remote() *Endpoint                                              // another process: the tool facade URL and a run token
}
type ModelAccess interface {
	Stream(ctx context.Context, r *ModelRequest) (ModelStream, error) // r.Tier: fast | standard | deep; never a provider, a URL or a key
	Limits(t Tier) ModelLimits                                        // context and output tokens of the entry the tier resolves to
	Remote() *Endpoint                                                // another process: the OpenAI-compatible base URL and a run token
}
type Budget struct {
	Steps        int           // model calls that may request tools; the call after the last must answer without tools
	ToolCalls    int
	InputTokens  int64         // summed over the run; 0 is not capped
	OutputTokens int64
	Deadline     time.Duration // also the deadline of ctx
	WrapUp       time.Duration // how long before the deadline the harness starts its final answer
	USD          *float64      // informational; present only where the price table states a rate
}
```

### 2.2 Inputs

| Input | Who builds it | Rule for the harness |
|---|---|---|
| Task and persona | The caller names kind, pillar and subject. `agent-runs` reads the persona from the pillar group | The persona is the system instruction. A harness may add its own operating text; it may not contradict or drop the persona |
| First message | Listeners on `run/first-message` (`context-pack` first): fenced, bounded blocks in stage order | Sent as the first user message, byte for byte. The suite compares the first model request with the blocks |
| Tools offered | `agent-runs`: the pillar's tool set, filtered to effects class `read` and to the actor's permissions; a tool that is not ACTIVE is absent | The run calls nothing else. A tool the model names that is not offered is answered with an error result and not executed |
| Model | `agent-runs` hands a view of `llm` that carries the run id and the lease | Every model call goes through it. The harness asks for a tier, never for a model, a provider or a key |
| Budget | Operator values and `TenantLimits`, in units that exist on every install: steps, tool calls, tokens, seconds. USD only as information (00 §2.1 row 13) | The harness stops itself inside the budget. The dispatcher enforces the same numbers from the stream as the backstop |
| Tenant | The tenant handle in `ctx`, minted by the entry dispatcher (00 §5.1) | Never an argument, never read from a payload, never forwarded as a string |
| History | `casefile` rows in the neutral form, or the opaque blob the same harness returned | A neutral history must be accepted. An opaque one is returned only to the harness id and version that produced it |

Two task kinds exist in `v0`: `investigate` (the answer contract is the RCA contract) and `ask` (free text with evidence references). The finish-gate turn (P2), a paid verification turn and a routing classifier (both behind their 00 §7.3 triggers) are further kinds of the same call, added as a minor version when their consumer ships.

### 2.3 The stream of steps

A harness emits steps in the order things happened. `agent-runs` appends each one as a row of `casefile.investigation_step` before it does anything else with it, so the transcript is complete at a crash (00 principle 8).

| Step | Carries | Required |
|---|---|---|
| `ModelCall` | Tier, model alias, input and output tokens, duration | Yes, once per model call. `llm-gateway` meters the same call under the run id, and a difference between the two counts is recorded on the run |
| `ToolCall` | Call id, tool name, arguments | Yes, before the invoke |
| `ToolResult` | Call id, status, a reference to the output, bytes, truncated or not | Yes, after it |
| `Message` | Text meant for a person (an interim finding) | Optional |
| `Note` | A closed kind (`compaction`, `pruned`, `reminder`, `tool-disabled`, `tier`, `wrap-up`) and what it changed | Yes, whenever the harness changes what the model sees. With it the model-visible history of any step can be rebuilt from the rows |
| `Answer` (terminal) | The answer text, the structured form if any, `stoppedBy` (`done`, `steps`, `deadline`, `tokens`, `tool-calls`), an optional opaque history | Exactly one per call, or none when `ctx` ended the run |

There is no `ApprovalRequired` step. Every offered tool is a free read, and anything that writes is a typed plan a person starts through `remediation` (00 §2.2), so a harness has nothing to ask approval for.

### 2.4 Answer, cancel and deadline

- **Answer.** The harness returns text, and JSON when an `AnswerSpec` is set. It builds no entity reference and sets no flag. Listeners on `run/answer` do that: `rca-contract` for kind `investigate`, as today.
- **Cancel.** Cancelling `ctx` is the only cancel. The harness stops model and tool calls and returns within a grace of 5 s (initial value). After the grace the dispatcher abandons the call, opens the breaker of that (tenant, row) and writes the run as `incomplete`.
- **Deadline.** `Budget.Deadline` is the deadline of `ctx`. At `Deadline - WrapUp` a conforming harness sends one last request without tools and answers with what it has. If no answer arrives, the dispatcher builds the digest from the step rows, as 00 §2.1 row 16 already states.
- **Crash.** A worker crash restarts the run from the first message. That is safe because every tool is a read. Resuming from the stored steps is possible with our own loop and waits for a measured cost.

### 2.5 What a harness must never do, and what stops it

| Never | Stopped by, in process | Stopped by, in another process |
|---|---|---|
| Write, or start anything on a target | The `ToolSet` holds read tools only; the `tools/pre-execute` gate refuses a write for a run caller; `credentials` is consumer-bound to `remediation` | The same, plus every tool of the upstream's own rendered off, with a deny-list in the adapter's code and the recorded tool list as proof |
| Hold a credential | A harness with no departure injects nothing that yields a secret. A harness manifest that declares `capabilities.secrets` must also declare the departure `instance-key` or `native-reads`; `plugin check` and the index lint compare the two | It gets a run token (or, as a declared departure, an instance key) and nothing else. Read credentials inside the process are the departure `native-reads` |
| Call a model outside the gateway | It has no `dialer` need and no `capabilities.network`; the import analyzer and gate check 7 see both | Egress limited to the kernel's internal listener (NetworkPolicy, or the compose network); the gateway's per-run meter is compared with the reported `ModelCall` steps |
| Derive or carry the tenant | The handle is the only source; the suite runs two tenants | One instance per tenant in a multi-tenant profile (gate check 9) |
| Build references or flags, post, page or notify | Its only output is the sink; `notifiers` and `oncallProviders` are not in its needs | It has no route to them |
| Keep tenant data after the run, or load tools at run time | Generated cycle test and the two-tenant test; the tool list is `Specs()` | The upstream's memory and plugin features are rendered off |

`llm-gateway` also reads the `agents-paused` kill switch, so no path spends on a model while it is set, whatever dispatched the call. Today only `casefile` reads it, which leaves chat turns running.

Prompt injection is bounded by construction, not by a filter: alert text and tool output are fenced as data, no offered tool writes, and no offered tool can send data to an address the model chooses (no generic HTTP tool, MCP tools are allow-listed per server). The worst outcome of an injected instruction is a wrong answer, which the answer contract and a person read.

### 2.6 Departures

`Describe()` lists departures from a closed list. Boot prints them, the dump shows them, and the dispatcher has a stated fallback for each. A harness in a bundle of a shipped profile has none.

| Departure | Meaning | Fallback in `agent-runs` |
|---|---|---|
| `advisory-steps` | The step ceiling is a rendered setting of the upstream, not enforced per run | Counts steps from the stream and cancels at the ceiling; the run is `incomplete` |
| `best-effort-cancel` | Closing the stream may not stop the upstream at once | Grace, then abandon and breaker |
| `opaque-history` | A continued run needs the upstream's own history format | Stores the blob once per turn as an artifact; a follow-up turn stays on the same harness and version |
| `instance-key` | The process holds a long-lived virtual key instead of a run token | Concurrency per instance key is held at 1 where per-run metering is required |
| `no-model-steps` | The upstream reports no step per model call | The gateway's rows stand in |
| `native-reads` | The upstream reads telemetry with its own toolsets and holds read credentials | Allowed only by explicit row config that carries the recorded tool list; never in the project's default bundles |

### 2.7 Version, class and conformance

- **Version.** `v0`, promoted by the rule of 00 §4.3. The second contribution the promotion asks for is `harness-holmesgpt` or the remote reference harness of the template.
- **Class.** Proposed provider-bound to `agent-runs` (today `investigators` is open). The fences a second provider would remove are the transcript (principle 8), the read-only tool set and the selection rule. If the reviewers keep it open, the kill switch in `llm-gateway` and the `tools` gate still hold.
- **Remote.** `contrib` from S8a. A remote harness needs two callbacks added to the set of plugin record §11: `tools` (the offered set of the active run only) and the existing `llm`.
- **Conformance, obligations half** (the contribution, driven by a scripted model and recording tools, no network):

| Obligation | Check |
|---|---|
| First message and answer schema arrive unchanged | Byte comparison of the first model request |
| Only offered tools, only through the tool set | A scripted call to an unknown tool yields an error result and zero invokes |
| Every model call through `ModelAccess` | The fake counts; for a remote harness the suite runs with egress denied |
| Steps are complete and ordered | The step list equals the fakes' logs |
| Exactly one terminal step | Scripts for done, ceiling, token cap |
| Budget | At the ceiling the last request carries no tools; `stoppedBy` is set |
| Cancel and deadline | Return inside the grace; no call after the cancel |
| Continuation | A run with a neutral history continues from it |
| No effect | The recording sandbox shows zero writes |
| Two tenants | A run for A calls no fake under B |
| Dispose | A call after the row is gone returns `ErrInactive` |
| Departures are truthful | Each declared departure is exercised; an undeclared one fails |

- **Guard half** (the dispatcher): the tool set holds read tools only; leases are held for the call; every step is a row before it is forwarded; the answer passes `run/answer`; `agents-paused` stops a run; the selection order of section 3.1; each fallback of section 2.6; a step after the terminal one is dropped and counted.
- `test/policy-bypass/` gains six attempts: a harness invokes a tool outside its set; a harness plugin declares a network capability and dials a model; a run starts while `agents-paused` is set; a harness emits a step for another run; a loop row loosens a plan (section 4.2); an MCP server changes a tool's schema after it was recorded (section 5.3).

## 3. Selection, and what stays outside every harness

### 3.1 Selection

| Order | Source | Field | Who may set it |
|---|---|---|---|
| 1 | Tenant | `harnessFor.<pillar>` on the `agent-runs` row, marked tenant-writable | The tenant overlay, from S4b; it may name only a harness row that is installed and enabled for that tenant, checked when written |
| 2 | Pillar | `harness` in the config of `pillar-<name>` (it replaces `investigator`) | Bundle, profile and site layers |
| 3 | Default | `defaultHarness` on the `agent-runs` row, `harness-native` in every shipped profile | Bundle, profile and site layers |

- A selected harness that is not ACTIVE is waited for inside the definition's cold budget. After that the run goes to the default harness and the timeline says which harness was asked for and which ran. The fallback is always toward the default, which has no departure.
- The run row records harness id, harness version, upstream version and model alias. An evaluation row carries the same four values (section 7).
- Swapping the harness of a pillar is a patch on one field, applied as a live recomposition.

### 3.2 What stays outside

| Concern | Row of ours | Where it attaches | Why outside |
|---|---|---|---|
| Spend decision for an incident | `casefile` with `storm-mode` on `incident/investigate` | Before `Dispatch` | It is about the incident, not about the loop |
| Kill switch, slot lease, daily budget | `agent-runs`, `limiter`, `llm-gateway` | Before the call and on every model call | A harness cannot be trusted to stop itself |
| Context pack | `context-pack` on `run/first-message` | Builds `FirstMessage` | Code knows the alerts, the change candidates and what was not read |
| Answer contract and its checks | `rca-contract` on `run/first-message` (sets the schema) and `run/answer` (parses, builds references, sets flags) | Around the call | The model reasons; code builds every reference (00 §2.1 row 16) |
| One bounded repair of an unparsed answer | `agent-runs`: one follow-up turn with the parse error | After `run/answer` | Works for every harness; stays behind its 00 §7.3 trigger |
| Finish gate (P2) | `agent-runs`: one follow-up turn with the late alerts and changes | After the answer | Same |
| Second independent pass | `run-second-pass`: a second `Dispatch` with a fresh context and a configured difference (tier or harness), compared by code on the root-cause entity | After the answer, when code-computed doubt is present | Independence is strongest when the second pass can be another harness. The brief lists it with the loop; it is placed one level up for that reason |
| PII scrub | `llm-gateway` and `tools`, through `pii` | Every model request and tool result | Unchanged |
| Transcript and RCA | `casefile` (`investigation`, `investigation_step`, `artifact`) | Written by `agent-runs` through `incidents` | One record for every harness. No new schema |
| Runbooks, playbooks, skills | `content` with `tool-runbook-fetch`; titles in the first message | A tool and a block | Harness-neutral already |
| Verification | `verifiers` under `change-hub` | After `Apply` | Unchanged; no harness is involved |
| Approval, ledger, leases | `approval`, `change-hub`, `credential-lease`, `remediation` | The write path | Unchanged |
| Evaluation | `lab/`, `aiopsctl eval` | Outside every profile | Section 7 |

`casefile` keeps the incident state machine and its `investigate` job; the job now calls `Dispatch`. `casefile` declares `Want(harnesses)` and `agent-runs` declares `Need(incidents)`, which is not a `Need` cycle.

## 4. The first-party harness

### 4.1 Rows

Built new, as a loop and small listeners, the way dsh builds its agent (`dsh-architecture` §6; the installed base bundle of dsh 0.2.2-rc.1, read today, mounts `agent-loop`, `llm-retry`, `tool-executor-native`, `token-meter`, `compaction-basic`, `tool-result-pruner`, `repeat-tool-reminder`, `timeout-policy` and `skill` as separate rows). All rows are process scope, in process, on the worker role, in `aiops-base`.

| Row | Does | dsh row it mirrors | Release |
|---|---|---|---|
| `harness-native` | The loop of section 4.3. Contribution to `harnesses`; provides `agentLoop` and owns its two hooks. Holds the step ceiling, the deadline wrap-up and cancellation, because the contract requires them of every harness | `agent-loop` | 0.1.0 |
| Model access | No row: `ModelAccess` over `llm`. Retry and backoff on an overloaded model server stay in `llm-gateway` | `llm`, `llm-retry` | 0.1.0 |
| Tool dispatch | No row: `ToolSet.Invoke` into the `tools` dispatcher, up to 4 calls of one step in parallel (initial value) | `tools`, `tool-executor-native` | 0.1.0 |
| `loop-budget` | Token and tool-call caps of the run; a notice to the model at 80 %; answer-now at the cap | `token-meter`, `timeout-policy` | 0.1.0 |
| `loop-repeat-guard` | Repeat: the same tool with the same arguments is reminded at 3 and withheld at 5. Thrash: 4 consecutive steps whose results add no new output hash are answered with answer-now | `repeat-tool-reminder` | 0.1.0 |
| `loop-tool-failures` | Remembers (tool, error class) for the run; after 2 failures of one class the tool is withdrawn from the next requests, the model is told once, and the answer lists it under inputs that were not available | none | 0.1.0 |
| `loop-compaction` | Stage one, deterministic: when the history passes a share of the tier's context limit, old tool results are replaced by their reference and a one-line summary built by the tool. The first message, the answer keys and the last two steps are never touched | `tool-result-pruner` | 0.1.0 |
| `loop-compaction`, stage two | A summary of older turns written by a fast-tier model call | `compaction-basic` | Later: runs that end on the context limit after stage one reach 5 % of lab case-runs |
| `loop-tiering` | Chooses the tier per step: standard by default, fast for summaries, deep for the final answer when configured | `agent-default-model` | Later: a second model entry is configured on an install (the existing 00 §7.3 trigger) |
| `loop-nudges` | Before the final answer, one reminder when a tool the task kind marks expected was never called | none | Later: the existing trigger (10 % of wrong lab case-runs never called the change lookup) |
| `loop-skills` | Keeps the steps of a fetched playbook as a checklist that survives compaction | `skill` | With the playbook pack (P2) |
| `tool-mcp` | An MCP client, as a `tools` contribution and not as a loop row, so that every harness gets its tools (section 5.3) | the package `dsh-mcp-client` | 0.1.0 |

All numbers are initial values, set again from the first evaluation readings (section 7).

### 4.2 The loop definition

The loop rows must not import `plugins/harness-native`, so the hooks have a definition of their own: `sdk/def/agentloop/v0`, key `agentLoop` (singular, open), provided by `harness-native`. Its fake is a scripted loop driver that runs listeners over a recorded transcript, so a loop row is tested alone on the real kernel (principle 2). It is the equivalent of the agent-loop test kit that `dsh-architecture` §5.2 describes; that kit is not in the installed package, so the comparison is UNVERIFIED.

| Hook | Kind | Owner | Access | Remote | A listener may |
|---|---|---|---|---|---|
| `loop/pre-step` | waterfall | `harness-native` | signed | no | Tighten the plan of the next request: shorten the history view, remove a tool, add one notice, lower the tier, or set `answer-now` or `stop` |
| `loop/tool-result` | waterfall | `harness-native` | signed | no | Replace what the model sees of one result (never the stored result) and record a failure |

A listener can only tighten. The loop compares the plan before and after each listener and refuses one that adds a tool, removes a notice of another listener or moves the directive back toward `continue`. Order is by declared stage and then plugin id, as for every hook.

### 4.3 One run

1. Assemble the request: operating rules, the persona, tool schemas in a stable order, the first message, the history view. The prefix is stable across steps, so a model server's prompt cache is reused.
2. Run `loop/pre-step`. Emit a `Note` for anything a listener changed.
3. Call the model through `ModelAccess.Stream` at the plan's tier. Emit `ModelCall`.
4. For each tool call: validate the name and the arguments against the offered schema. An invalid call is answered with an error result and counted as a failure. A valid one is emitted as `ToolCall`, invoked, and emitted as `ToolResult`, then passed through `loop/tool-result`.
5. Repeat from 1 until the model answers without a tool call, the step ceiling is reached, or `Deadline - WrapUp` passes. In the last two cases send one request without tools that says to answer now and to name what is missing.
6. Emit `Answer` with `stoppedBy`.

The loop keeps no state outside the run and nothing per tenant between runs. Its history is the step rows, so "model-visible means logged" holds without a second store.

### 4.4 Earlier deferrals that are now possible

| Item (00 §7.3 or a stated limit) | Before | Now |
|---|---|---|
| `investigator-native` | Behind a measured gap or a HolmesGPT exit criterion | It is the default; the row and its trigger leave §7.3 |
| `tool-{metrics-query,logs-query,traces-query,kubectl-read}` | Behind "a native toolset is measured insufficient" | Required; in 0.1.0 (section 5) |
| `tool-profiles-query` | Behind three triggers; no upstream toolset existed | Ours to build; first item of S9a |
| `tool-mcp-bridge` | Behind ADR-004 and a customer request | Ships as `tool-mcp`, because a no-code pack needs it |
| Model-written wrap-up after a deadline | Behind 5 % deadline cancels | In 0.1.0: the loop knows its own deadline, so the wrap-up is one request, not a new mechanism |
| Per-run cost cap and exact per-run metering | Not possible: the gateway saw one virtual key per instance and no run id (07 R7) | In 0.1.0: every model call carries the run id |
| Tier-based model routing | Hosted P2 | A loop row with a trigger, usable on self-host too |
| `investigator-ensemble`, the second pass | Needed a second investigator | A dispatcher row over the same contract; still gated on the lab measure |
| Finish gate, bounded re-analysis (P2) | Depended on the upstream's history format and its compaction | A follow-up turn with a neutral history built from our own rows |
| One bounded rewrite of an unparsed answer; process nudges | Behind measured triggers | Unchanged triggers; the cost is now one turn or one listener |
| Paid verification turn | A new mechanism | A task kind of the same call; same trigger |
| Model limits for an unknown alias; `token_count` parity; the patterns route (ADR-003 items 1, 5, 6) | Open spike questions on the release path | Gone from the release path: the gateway knows the model entry, and the logs tool calls the route we choose |
| Resume a run after a crash | Not possible | Possible from the step rows; waits for a measured cost of restarted runs |
| Per-tenant harness pod, its cold start and scale to zero (ADR-014) | Hosted P2 work | Not needed by default |

## 5. Read tools

### 5.1 The tools

Each is a contribution to `tools`, exactly as a tool from a connector pack is. The vendor-neutral definition is the `tools` registry; `ToolSpec` gains three display fields, `Signal`, `Dialect` and `ScopeNote`, which the footer of the first message lists. They replace the display fields of `toolsetBindings`.

| Row | Bundle | Closed operations | Speaks | Read through | Release |
|---|---|---|---|---|---|
| `tool-metrics-query` | `aiops-datasources` | Instant query; range query with a step floor and a point cap; label names; label values; series by matcher with a limit | The Prometheus HTTP query API | `metricsQuery` (`metrics-query-http`) | 0.1.0 |
| `tool-logs-query` | `aiops-datasources` | Range query with a line and byte cap and a direction; label names and values; patterns for a selector | The HTTP query API of the default logs store | `logsQuery` (`logs-query-http`, moved from P2 to S8a) | 0.1.0 |
| `tool-traces-query` | `aiops-datasources` | Search with a limit; one trace by id as a capped span tree; tag names and values | The HTTP API of the default traces store | Its own client; no new dialect key, because nothing else would consume one | 0.1.0 |
| `tool-kubernetes-read` | `aiops-pillars-mvp`, naming the pillars it joins | Get and list for a closed kind list; events of an object; the bounded tail of a pod log; rollout status and history. The kind `Secret` is not in the list | The Kubernetes API through client-go | Its own client | 0.1.0 |
| `tool-profiles-query` | `aiops-datasources` | Top functions for a selector and window; the difference of two windows | The HTTP API of the default profiles store, with our own types | Its own client | S9a |
| `tool-change-lookup`, `tool-runbook-fetch`, `tool-oncall-who` | as today | as today | kernel data | `changes`, `content`, `oncallProviders` | 0.1.0; the third only where an on-call provider plugin is composed |

### 5.2 Rules

- **Bounds are the tool's, the cap is the dispatcher's.** A tool takes a time range, a limit and a step and refuses a call without them. It returns a compact typed result (a long series as minimum, maximum, last value and slope; logs as capped lines plus a pattern summary; a trace as the slowest and the failed spans). `tools/post-execute` still caps and `pii` still scrubs.
- **Every result carries a reference** (the query, the range, a deep link), so the RCA cites evidence without copying telemetry (00 §5.3).
- **The model writes the query.** That is why these rows sit in the default connector pack: no row of `aiops-base` or of a pillar bundle holds a query string (principle 7). The tool description names the dialect in prose.
- **Credentials.** The tools read through the gateway query routes with the existing service token class of 00 §5.4, or in `aiops-only` with the customer credential in tenant secrets under the pack's scope. The credential is read per call. The classes "Investigator service JWT" and the instance-held "LLM virtual key" leave the default path; the Kubernetes read credential is now held by the kernel through tenant secrets. Where the query rows take endpoint and credential in `aiops-only` (ADR-028) must be decided at S8a start, not at P2 start.
- **Gateway routes.** The read routes the gateway must carry are now a closed list we write from these tools (05 A8), not the outcome of a spike.
- **No AGPL import.** The tools speak HTTP with types of our own. `pkg/logql/syntax` and `pkg/logcli` of the default logs store are AGPL-3.0-only at v3.7.9 (its `LICENSING.md`, read today), so the logs tool parses no query and relies on the bounds above. `pkg/loghttp` and `pkg/logproto` are Apache-2.0 at that tag, and the list moves between tags, so they are not used either. The existing deny on the profiles store's `api` package stands (08 §5.1 L2).
- **Names.** Row ids name the signal, never a store: `tool-logs-query`, not a name with a product in it (00 §8.1).

### 5.3 `tool-mcp`, the MCP client

One instantiable first-party plugin, `aiops/tool-mcp`. A row is one MCP server; each allow-listed tool of that server becomes a `tools` contribution, so it passes the same dispatcher (effects gate, cap, scrub, audit) and reaches every harness.

| Rule | Detail |
|---|---|
| Transport | Streamable HTTP only, dialled through `dialer`. Never stdio: the kernel does not start processes |
| Allow-list | The row config names the tools. Anything else the server lists is ignored |
| Proof travels with the pack | The pack carries the recorded tool list (names, schemas, descriptions) at a pinned server version, and states that the credential is read-only. This is the rule `toolsetBindings` had, moved here |
| Pinned text | The model sees the recorded description and schema, not the live ones. A live schema that differs from the record fails the row until it is recorded again |
| Server-initiated features | Sampling, elicitation and roots are not offered, so a server cannot reach the model through us |
| Credential | `${secret:name}` in the pack's secret scope; a new line in the credential table of 00 §5.4 |

`tool-mcp` replaces `toolset-binding` in the list of instantiable plugins (with `signal-ingest-generic-json` and `notifier-webhook`). The client library is the MCP Go SDK, linked in the plugin and never in `sdk/kernel` (section 10).

### 5.4 Effect on connector packs

| Community record item | Becomes |
|---|---|
| Data pack: rows of `toolset-binding` | Rows of `tool-mcp` for a system that has an MCP server; otherwise the pack is a read connector with one `tools` contribution |
| The four vendor-neutral read seams (`signals`, `toolsetBindings`, `evidenceSources`, `estateSources`) | `signals`, `tools`, `evidenceSources`, `estateSources` |
| Connector scenario, investigator half: the pinned investigator image against replayed responses, needs a container runtime | The default harness with a replayed model makes one successful call on each tool the pack offers. It runs in `aiopsctl`, so the scenario has one half and needs no container runtime |
| Leg C1 of 00 §7.1: a reference data pack for a logs store outside the default stack | The same leg with a `tool-mcp` row. A candidate server for the proposed store exists under Apache-2.0 (read today by the licence API only; section 12). If its tools do not fit, C1 becomes a read connector and needs the SDK |

## 6. HolmesGPT as an optional harness plugin

### 6.1 The plugin

| Point | Design |
|---|---|
| Id and home | `contrib/harness-holmesgpt`, source under `contrib/harness-holmesgpt/`, shipped disabled in the `aiops-contrib` image and listed in the project index. It is not in the `aiops` release image, so a shipped profile that named it would fail `profile render`: the guarantee is mechanical. The project maintains it |
| Runtime | In-process Go, `scope: tenant`, because it needs `sidecars.Bound` and `secrets.Bound`, which a remote plugin cannot inject |
| What it links | Nothing of HolmesGPT. It is an HTTP and SSE client of `POST /api/chat` |
| What it renders | The upstream config: the model list with one entry per tier that points at the gateway's HTTP surface, every toolset off, the adapter's deny-list, `enable_tool_approval` false, the step ceiling |
| Tools, default mode | The run's `ToolSet` is sent as `frontend_tools` in `pause` mode; each paused call is executed through `ToolSet.Invoke` and returned with `frontend_tool_results`. The request fields exist at 0.46.0 (read today). This was the fallback of ADR-004 and becomes the first choice, because it is per request |
| Tools, opt-in mode | Native toolsets named in the row config, each with its endpoint, a secret reference and the recorded tool list. This is the departure `native-reads` |
| Kernel surfaces it needs | The OpenAI-compatible HTTP surface of `llm-gateway` and, for the MCP alternative, `tools-mcp-facade`. Both are off by default and are enabled in the profile layer by whoever installs an external harness |
| Tests | The harness suite on cassettes recorded at the pinned tag; the ADR-003 spike, re-scoped, records them. A pin bump re-records |
| When | After the first-party harness passes leg A. Proposed as the first item of S9a; a contributor may bring it earlier. It is not a gate of 0.1.0 |

### 6.2 What it can honour

"Read today" means the HTTP API reference of the repository at tag 0.46.0.

| Contract item | Through the adapter | Verdict |
|---|---|---|
| Persona | `additional_system_prompt` (read today). The upstream's own system prompt stays around it; `behavior_controls` switches sections of it | Honoured, with the upstream's prompt present. Whether it can be reduced to ours alone is UNVERIFIED |
| First message unchanged | The `ask` field | Honoured |
| Answer schema | `response_format` (read today) | Honoured; on a forced last step UNVERIFIED |
| Tools per run | `frontend_tools` with `pause` (read today) | Honoured if several calls in one step pause and resume correctly: UNVERIFIED |
| Model only through the gateway | Server-side model list, selected per request by key name (read today); egress limited to the kernel | Honoured by config and network. That its embedded model library sends every call there is UNVERIFIED |
| Tier | One model key per run | Per run, not per step |
| Step ceiling | No per-request limit is documented | Departure `advisory-steps` |
| Tokens and model steps | `token_count` events; no event per model call | Departure `no-model-steps` |
| Run token | The model key is server config | Departure `instance-key` |
| Cancel and deadline | The client closes the stream | Departure `best-effort-cancel` until a cassette shows the upstream stops |
| History | It returns its own `conversation_history`, whose first message must be a system message (read today) | Departure `opaque-history` |
| Compaction, repeat guard, tool-failure memory, tiering | Its own behaviour; `conversation_history_compaction_start` and `conversation_history_compacted` map to `Note` | Our loop rows do not apply to it |
| Never write | Toolsets off, deny-list, no approval flow | Honoured by the rendered config; the golden config is the proof |
| No credentials | The instance key only, in the default mode | Honoured apart from `instance-key`; `native-reads` in the opt-in mode |
| Tenant | Admin endpoints are unauthenticated (read today), and the config is per server | One instance per tenant in a multi-tenant profile; `HOLMES_API_KEY` set; ingress from the kernel only |

### 6.3 Where its container sits

- 00 §3.2 loses the row `investigator-holmes`. A new optional row, `harness-sidecar`, stands for the upstream process of any external harness plugin: absent from every shipped profile, one shared instance on a self-host through `sidecars-static`, one per tenant in a multi-tenant profile through `sidecars-kubernetes`.
- The image is pulled by digest from the upstream registry and is in no image, mirror or bundle of ours. The upstream licence text ships beside the plugin.
- Third-party and optional plugins on hosted wait for the enterprise edition (00 §7.3), so the hosted default has no such container.
- A release check on the pinned tag fails if its lock file names `litellm-enterprise` or a `proxy` extra. At 0.46.0 `pyproject.toml` pins `litellm = "1.103.5"` with no extra (read today).

### 6.4 Two more harnesses a contributor could wrap

Neither is built by us. They show that the definition fits a library with no server and an agent behind a session protocol.

| | A Python agent library as a remote plugin | An agent that speaks the Agent Client Protocol |
|---|---|---|
| Example | Pydantic AI, or the OpenAI Agents SDK | dsh started with its `acp` profile, or any other agent with that protocol |
| Shape | The plugin is the harness: about a hundred lines on `aiops-sdk` that register with `harnesses`, wrap each `ToolSpec` as a function tool over the `tools` callback, and point the library's OpenAI-compatible client at the `llm` callback | An adapter that opens a session, passes the tool facade as an HTTP MCP server in `session/new` (the `mcpServers` parameter, read today), sends the first message as the prompt and maps session updates to steps; `session/cancel` is the cancel |
| Contract | No departure expected: tools per run, a structured answer, steps per model call | Departures expected: `opaque-history`, `advisory-steps`, `instance-key`; the agent's own shell and file tools must be off, proven by its rendered profile |
| Licence, read today | MIT for both libraries (licence API) | The protocol repository is Apache-2.0 (licence API). Each agent behind it has its own terms. For dsh the lead is `licence-audit-agent-ai.md` §5 (its own code MIT, its install closure not all permissive); not re-read for this note |
| Who ships the process | The publisher, in the plugin's image | The user installs the agent; no image of ours or of the index contains it |

## 7. Evaluation before the first release

With an upstream harness, a wrong answer was partly the upstream's. With our own loop, prompts and tool outputs, every change can move accuracy, and nothing else will tell us. The lab of 00 §2.1 row 18 therefore splits differently.

| Tier | What | Model | Runs | Gate |
|---|---|---|---|---|
| E0 Replay | Loop behaviour on recorded transcripts: caps, repeat guard, tool-failure memory, compaction, wrap-up; the harness suite; the connector scenario | `llm-provider-replay` | Every pull request | Deterministic pass |
| E1 Case set | Scripted faults on the lab estate (kind, one enrolled VM, the demo application under a GitOps controller), each with inject, a check that the fault took hold, heal and a truth file | The default model binding, and one open-weight model served locally with egress denied | A maintainer, on a release candidate and on any change to a prompt-affecting path | Section 7.2 |
| E2 Catalogue | The fault engine, the generated catalogue, the noise floor over about 300 case-runs per arm | As E1 | P2, as 09 §7.2 describes | The P2 release report |

### 7.1 What exists at 0.1.0

- **Case set.** At least 24 cases: 12 on workloads, 8 on hosts, 4 with no fault to find (an alert that clears by itself, a planned change), so that a confident wrong cause is measured. Each case runs 3 times per arm.
- **Grader.** The pure function of 09 §7.2, brought forward: it reads the stored RCA only and compares the code-built root-cause reference, the suspect change and the "no cause" cases with the truth file. No model is used to grade.
- **Process measures per run**, from the step rows: steps, tool calls, failed tool calls by tool, tokens, seconds, which cap ended the run, repeat-guard and compaction events. The share of wrong runs with a failed read tool is the measure that used to gate `tool-*-query`; it becomes a standing column.
- **`aiopsctl eval`** runs the set and reproduces the report from the dataset hash, the grader version, the harness id and version and the model binding.
- **The leak rule** of 09 §7.1 is unchanged and is a precondition of every case.
- **Harness comparison.** The same set runs against any harness. When `harness-holmesgpt` exists it gets a row beside `harness-native`. That row is the only measured basis for the decision of this round, and later the basis of a quality line for a harness in an index.

### 7.2 The gate, and what it is not

- A release needs: every case-run ends without a harness error and inside its caps; the leg A case passes 3 of 3 on the default binding; a baseline is recorded for both models.
- With 72 case-runs per arm the set detects only large changes (roughly 15 points at 2 sigma near a 75 % pass rate). It is a smoke baseline, named so in the release notes. A numeric accuracy floor for 0.1.0 is an owner decision taken at the first reading (O4).
- **Prompt-affecting paths** are `plugins/harness-native/`, the loop rows, `context-pack`, `rca-contract`, the personas, and the description and output shape of every first-party tool. A pull request that touches one carries an E1 result recorded by a maintainer. Contributors run E0 only; no paid check faces them.
- ADR-027's second half (catalogue, grader, sizing, who pays) is decided at S8a start instead of P2 start.

## 8. Effects

### 8.1 Order of work

| Step | Change |
|---|---|
| S7 | The investigator spike (ADR-003, ADR-004) leaves S7. Nothing else moves |
| S8a, new: agent runtime | `llm-gateway` as a pass-through with per-run metering; `tools` with its remote adapter; `metrics-query-http`, `logs-query-http`, the four read tools, `tool-mcp`; `harnesses`, `agent-runs`, `agentLoop`, `harness-native` and the four loop rows; fakes and suites; tier E0; the remote reference harness of the template. **Exit:** the suite passes for `harness-native`, the fake and the remote reference harness; on `dev` a scripted task is answered from the dev stack with a replayed model and with a local model; the six new policy-bypass attempts are refused |
| S8b: incident loop | The former S8 without `investigator-holmes`, the binding registry and the facade as a default row; plus tier E1. **Exit:** the four legs of 00 §7.1 and a fifth, leg E: the evaluation gate of section 7.2 |
| S9a | Gains, in this order: `harness-holmesgpt` with its comparison row, `tool-profiles-query`, then lab stage two and the items it measures (`run-second-pass`, stage two of compaction, nudges) |
| S9b | Loses "per-tenant HolmesGPT with scale-to-zero". Hosted runs the loop in `kernel-worker` |

### 8.2 Containers, credentials and hosted

| Place | Change |
|---|---|
| 00 §3.1 `kernel`, `kernel-worker` | Responsibilities gain the agent loop and the read tools |
| 00 §3.2 | `investigator-holmes` removed. `harness-sidecar` added as optional, in no shipped profile (section 6.3) |
| 00 §3.3 | New `ext-mcp-server`: an MCP server the customer or a vendor runs, reached by `tool-mcp` rows. `ext-customer-estate` is read by the kernel (`tool-kubernetes-read`), no longer by an investigator container |
| 00 §5.4 | Out of the default path: the investigator service JWT and the instance-held LLM virtual key (both remain for an external harness). Changed holder: the Kubernetes read credential, now the kernel through tenant secrets. New: the run token of an external harness (minutes, ends at the run deadline, one tenant's budget share for one run) and the MCP server credential |
| Hosted | No per-tenant harness pod by default, so the idle fixed-cost table of 00 §7.2 loses one line and ADR-014 leaves the hosted plan. `kernel-worker` needs egress to the gateway read routes and, under ADR-016, to customer Kubernetes APIs; both go through `dialer` |
| Principle 6 | "Anything that runs model-chosen actions runs out of process" needs a precise reading: code or commands a model wrote, and any third-party harness, run out of process. The first-party loop executes only typed read tools through the dispatcher, under caps, a deadline and a breaker, and runs in process, as the kernel-data tools already do. This is working assumption W8 |

### 8.3 Names

- **Rule, for 00 §8.1.** No id in `sdk/`, `boot/`, `loader/`, a definition key, a hook, a schema, a metric, a container id of the default set, or a row of a bundle that a shipped profile lists may contain the name of an upstream project. An upstream's name appears once, after the contribution noun, in the id of the plugin that wraps it (community record §6.7 R2), and in that plugin's own bundle. CI greps the golden dumps of the shipped profiles and the SDK tree against a deny-list of upstream names.
- **Renames that follow.** `investigators` to `harnesses`; `investigations` to `agent-runs`; `investigation/first-message` and `investigation/answer` to `run/first-message` and `run/answer`; the pillar config field `investigator` to `harness`; the example ids in 00 §8.1 (`aiops/investigator-holmes`, `plugins/investigator-holmes/`) to `aiops/harness-native` and `plugins/harness-native/`. `casefile.investigation` and `investigation_step` keep their names: they name the record, not an upstream.
- **Gone from the core.** `investigator-holmes` as a row and as a container id, `toolset-bindings`, `toolset-binding`, the four `toolset-binding-*` rows.

## 9. Amendments

ADR numbers above ADR-050 are provisional; the reconciliation of this round assigns them.

### 9.1 `00-decisions.md`

| Section | Amendment |
|---|---|
| Header, §1 | Status line gains this round. Out of scope reads "SSH or a shell from the kernel or any harness". Principle 6 as in section 8.2, with "an external harness" in place of HolmesGPT. Principle 7: HolmesGPT leaves the list of upstreams the core renders; "toolset ids are binding data" is deleted |
| §2.1 row 1 | "HolmesGPT and GoAlert are upstream processes reached over their own APIs" becomes "an optional plugin may drive an upstream process over that process's own API" |
| §2.1 row 2 | Rewritten as "Agent harness": the contract of section 2, `harness-native` as the default, selection (section 3.1), read tools as first-party `tools` contributions, external harnesses as optional plugins with declared departures, no harness container by default |
| §2.1 row 12 | A pillar group holds its persona, tool set, `harness` selection and executors |
| §2.1 row 13 | Metering is the gateway's own, per run; a per-run token cap; the gateway reads `agents-paused`; the rationale reads "no harness holds a provider key"; the HTTP surface is off by default |
| §2.1 row 16 | "Around `investigator-holmes`; there is no native loop" becomes: the method is rows of ours on the hooks of `agent-runs`, around whichever harness is selected. The wrap-up before the deadline is P1; the code-built digest stays as the backstop |
| §2.1 row 18 | P1 gains the case set, the grader, `aiopsctl eval` and a recorded baseline; P2 keeps the fault engine, the generated catalogue and the noise floor |
| §2.1 row 19 | P1 change evidence is read by the agent with `tool-logs-query` and the host runbook |
| §2.3 | A paragraph for this round: `toolsetBindings` withdrawn; the connector scenario has one half; `tool-mcp-bridge` ships as `tool-mcp`; hooks move from `casefile` to `agent-runs` |
| §2.4 | Owner decisions A, B and C. New working assumptions W7 to W10 (section 11) |
| §3.1 to §3.3, §5.4 | As in section 8.2 |
| §4.1 | The row `investigators; oncallProviders` becomes `harnesses` (class `P`, remote `contrib` S8a) with its contract line, and `oncallProviders` on its own line. The row `toolsetBindings; evidenceSources` keeps `evidenceSources` only. `tools`: `ToolSpec` gains `Signal`, `Dialect`, `ScopeNote`; remote gains `callback` for a remote harness. `metricsQuery, logsQuery`: both P1. New line `agentLoop` (open; `harness-native`; remote no). `instantiable` plugins: `tool-mcp` in place of `toolset-binding` |
| §4.2 | `harnesses` joins the provider-bound dispatchers, with its fence stated (section 2.7) |
| §4.3 | Hooks paragraph: `run/first-message` and `run/answer` are owned by `agent-runs`; `agents-paused` is read by `agent-runs` and `llm-gateway`; `loop/pre-step` and `loop/tool-result`. Promotion rule: the four read seams are `signals`, `tools`, `evidenceSources`, `estateSources` |
| §4.4 | `aiops-base`: `investigations` becomes `agent-runs`; `toolset-bindings` leaves; `harness-native`, `loop-budget`, `loop-repeat-guard`, `loop-tool-failures`, `loop-compaction` enter; `tools-mcp-facade` stays, disabled by default. About 53 rows. `aiops-datasources`: the four binding rows leave; `tool-metrics-query`, `tool-logs-query`, `tool-traces-query` and `logs-query-http` enter. Still 5 rows. `aiops-pillars-mvp`: `investigator-holmes` leaves, `tool-kubernetes-read` enters. Still 11. `aiops-hosted` and `aiops-selfhost`: no investigator instance. `self-host-full` composes about 106 rows |
| §4.5 | T4: the upstream cassettes belong to the optional plugin. E0 is named as part of T3 and T4; E1 as T5 |
| §5.5 | Per-run caps (steps, tool calls, tokens, deadline) join `TenantLimits` |
| §6.1 | Layer 6: `agents-paused` readers as above |
| §6.2 | Pack sizes and the connector scenario as in section 5.4 |
| §6.3 | `aiops-only`: "HolmesGPT reaches the kernel tools with its virtual key" is deleted; the default pack's tool rows are pointed at customer endpoints in the profile layer |
| §6.4 | Cohorts select the image versions of the sidecars of optional upstream plugins |
| §7.1 | Step 3: "the incident agent posts an RCA". Host leg: read with `tool-logs-query`. Leg B and C1 as in section 5.4. New leg E, evaluation |
| §7.2 | As in section 8.1 |
| §7.3 | Removed: `investigator-native`; `tool-{metrics-query,logs-query,traces-query,kubectl-read}`; `tool-mcp-bridge`; the model-written wrap-up. Rewritten: `investigator-ensemble` as `run-second-pass`; `tool-profiles-query` as an S9a item; the measures table row for `tool-*-query` as a standing evaluation column. Added: stage two of compaction, `loop-tiering`, resume after a crash, a second instance of the loop with other listeners (trigger: two pillars need different loop rows) |
| §8.1, §8.2 | The naming rule and renames of section 8.3. Glossary: harness, pillar agent, run, loop row, external harness, departure |
| §9 | ADR-003 re-scoped to the optional plugin: cassettes at the pinned tag, the open points of section 6.2; decide by the start of that plugin. ADR-004 re-scoped: how an external harness reaches tools (`frontend_tools` first, the facade second). ADR-014 withdrawn from the hosted plan. ADR-012 gains per-run metering. ADR-027: second half at S8a start. ADR-028: at S8a start. ADR-030 gains the task kinds and `AnswerSpec`. ADR-047: the `toolsetBindings` part is withdrawn, the `tool-mcp` proof rule is added. New: ADR-051 the harness definition (contract, departures, class, callbacks), S8a start. ADR-052 the first-party loop (hooks, rows, initial caps, the reading of principle 6), S8a start. ADR-053 read tools (operations, bounds, the Kubernetes kind list, credentials), S8a start |

### 9.2 The plugin record

| Section | Amendment |
|---|---|
| §2 | Q6: the row count. A dated note that the investigator rows of this record are superseded by this design |
| §4 | The `scope: tenant` example `investigator-holmes` becomes `harness-holmesgpt`; `harness-native` is an example of process scope |
| §5 | Catalogue: `investigators` becomes `harnesses`, class `P`; `agentLoop` added; `toolsetBindings` removed; `tools` gains the callback. The sentence that lists `investigators` among the keys left open is edited, with the reason of section 2.7 |
| §6 | The row "`investigator` point; `tool` point and MCP facade" maps to `agent-runs`, `harness-native` and its loop rows, `tools`, the read tools, `tool-mcp`; `tools-mcp-facade` disabled by default |
| §7 | `test/policy-bypass/` gains attempts 25 to 30 (section 2.7) |
| §8 | Hook list: `investigation/*` renamed to `run/*` with owner `agent-runs`; `loop/pre-step` and `loop/tool-result` added (signed, not remote) |
| §10 | The definitions that tolerate a cold start name `harnesses`, for external harnesses only |
| §11 | The callback set gains `tools`, limited to the offered set of the active run |
| §14 | T1 gains the scripted loop driver of `agentLoop`; T4 as in 00 §4.5 |
| §15 | Row 53: the agent-loop test kit now has an equivalent (`agentloop` fake, `llm-provider-replay`, the tool recording fake). No status changes |
| §16 | S7 loses the spike; S8 is split into S8a and S8b; S9a and S9b as in section 8.1 |
| §17, §18 | The file list below; ADR-051 to ADR-053 |

### 9.3 Other files of the set, as pointers

| File | What changes |
|---|---|
| 02 | The investigator container leaves C2-a1 and the deployment views; the kernel reads the gateway and the Kubernetes API itself |
| 03 | C3-c: `agent-runs`, `harness-native`, loop rows; hook owners; the `investigate` queue is unchanged |
| 05 | §2, §4, §5.4, §5.5 rewritten around `harnesses` and the read tools; A2, A3 and A8 close; figures `05-investigation`, `05-pillar-composition`, `05-connector-seams` redrawn and verified as rendered |
| 06 | `conversation` calls `Dispatch` with kind `ask` |
| 07 | §4.3 the `harnesses` and `agentLoop` interfaces; §4.6 `Budget`; §5 the callback; §8.2 the suites; §8.4 the worked pack with a `tool-mcp` row |
| 08 | DR-02 superseded; roadmap; §5.1 gains the checks of section 10 |
| 09 | §4.1 step 8 (the loop is ours) and step 11; §4.2 the sequence; §4.3 budgets; §7 the lab stages |

## 10. Licence notes for what this design adds

No component in this design is blocked by its licence. The loop is built new by owner decision, not because a licence required it.

| Component | Use here | Licence, read today | Mode | Verdict |
|---|---|---|---|---|
| HolmesGPT 0.46.0 | Optional plugin; the adapter links nothing | Apache-2.0 (licence API and `LICENSE`; no `NOTICE` among the top-level entries read) | Pull by digest | Ok with the conditions of `licence-audit-agent-ai.md` §4.1 |
| MCP Go SDK | `tool-mcp`; `tools-mcp-facade` | A composite file: Apache-2.0 for new and relicensed contributions, MIT for the rest, CC-BY-4.0 for documentation. The licence API returns `NOASSERTION` | Link, in plugins only | Ok with conditions: a reviewed exception in the allow-list of 08 §5.1 L7, both texts shipped |
| client-go | `tool-kubernetes-read` | Apache-2.0 | Link | Ok |
| Prometheus client and parser packages | Not required; `metricsQuery` is plain HTTP | Apache-2.0 | Link if ever used | Ok |
| Query parser and CLI packages of the default logs store | None | AGPL-3.0-only at v3.7.9 | Must not be linked | Not usable: the logs tool has no client-side query validation |
| Client packages of the default traces and profiles stores | None; own HTTP types | Not re-read for this note: UNVERIFIED here; `licence-audit-observability-stack.md` is the lead | Must not be linked unless on the per-tag allow-list | Own types |
| Pydantic AI; OpenAI Agents SDK | Examples only | MIT; MIT | A publisher's image | Ok |
| Agent Client Protocol | Example only | Apache-2.0 | A protocol we would implement | Ok; each agent behind it has its own terms |
| MCP server for the proposed reference store | Candidate for leg C1 | Apache-2.0 by the licence API only | The user runs it | Ok if chosen; text not read |

Rules that follow. (1) The first-party harness and the read tools are written from the stores' API documentation. Text adapted from HolmesGPT's prompts or toolsets is allowed under Apache-2.0 with an attribution line and a note of changes; none is planned. (2) A harness plugin's manifest names the licence of the process it drives, separately from its own (`integrates[].license`, an additive field), and the project index lists a harness only when that process is under an open licence. (3) The naming rule of section 8.3 is also the trademark rule: the upstream's name appears once, descriptively, in the wrapping plugin's id and title.

Questions for a lawyer:

1. May a plugin id contain an upstream's name (`harness-holmesgpt`), also when a third party sells the plugin? (Also question 4 of `licence-audit-agent-ai.md`.)
2. Does shipping a recorded tool list (names, schemas, descriptions) of an upstream's toolset or of a vendor's MCP server inside a pack need more than attribution and the licence text?
3. Is the composite MCP licence file acceptable in a commercial enterprise distribution that links the Go SDK into a closed plugin?
4. When a harness plugin on a marketplace drives a process under commercial terms, what must the listing terms say so that the obligation stays with the seller?

## 11. Assumptions, open items and what was not verified

Working assumptions, pending the owner's words:

| # | Assumption | A different answer reopens |
|---|---|---|
| W7 | The HolmesGPT adapter lives under `contrib/`, is maintained by the project, and is built after the first community release | Section 6.1; S9a. The alternative is `plugins/` in an optional bundle of the release image |
| W8 | The first-party loop runs in process, under the reading of principle 6 in section 8.2 | Section 4; hosted |
| W9 | `tool-mcp` ships in 0.1.0 and `toolsetBindings` is withdrawn | Section 5.3, 5.4; leg C1; ADR-047 |
| W10 | Evaluation tier E1 is a gate of 0.1.0 with the smoke baseline of section 7.2 | Leg E; S8b |

Open:

| # | Item | Owner |
|---|---|---|
| O1 | `harnesses` provider-bound or open (section 2.7) | Review; ADR-051 |
| O2 | Whether GoAlert's wrapper follows the same home rule as W7, so that both upstreams are treated alike | The on-call design of this round |
| O3 | The Kubernetes kind list and whether ConfigMap values are returned | ADR-053 |
| O4 | A numeric accuracy floor for 0.1.0 | Owner, at the first reading |
| O5 | Initial caps (steps, tool calls, tokens, deadline, wrap-up, parallel reads) | ADR-052, from E1 |
| O6 | Whether leg C1 stays a no-code pack (depends on the candidate MCP server's tools) | S8a |
| O7 | Token-by-token streaming to chat: a `Delta` step as a minor version, when the chat streamer needs it | 06 |
| O8 | A model with weak tool calling: whether the loop needs a text-format fallback for tool calls | E1 on the local model |

UNVERIFIED, in one place: whether HolmesGPT's own system prompt can be reduced to ours; `response_format` on its forced last step; parallel calls under `frontend_tools` pause mode; whether closing the stream stops the upstream; whether its embedded model library honours one base URL for every call; any per-request or server-side step limit of HolmesGPT (the API reference documents none); the contents of the published HolmesGPT image; the tool list and licence text of the candidate MCP server; the licence texts of the two example libraries beyond the licence API; the permission method of the Agent Client Protocol; how dsh's `acp` profile is pointed at an OpenAI-compatible endpoint; the Apache-2.0 sub-trees of the traces and profiles stores at the pinned tags.

## 12. Sources, read 2026-11-11

| Subject | Source | Version or commit |
|---|---|---|
| HolmesGPT licence | `https://api.github.com/repos/HolmesGPT/holmesgpt/license`; `https://github.com/HolmesGPT/holmesgpt/blob/master/LICENSE` | `LICENSE` blob `dfe5ed9cfe`, default branch |
| HolmesGPT tag | `https://api.github.com/repos/HolmesGPT/holmesgpt/commits/0.46.0`; the tree of that tag | Commit `586045cae6`, 2026-11-04 |
| HolmesGPT HTTP API | `https://raw.githubusercontent.com/HolmesGPT/holmesgpt/0.46.0/docs/reference/http-api.md` | Tag 0.46.0 |
| HolmesGPT dependencies | `https://raw.githubusercontent.com/HolmesGPT/holmesgpt/0.46.0/pyproject.toml` | Tag 0.46.0 |
| MCP Go SDK | `https://api.github.com/repos/modelcontextprotocol/go-sdk/license` | `LICENSE` blob `5791499cb0`, `main` |
| client-go | `https://api.github.com/repos/kubernetes/client-go/license` | `LICENSE` blob `d645695673`, `master` |
| Prometheus | `https://api.github.com/repos/prometheus/prometheus/license`; `.../prometheus/client_golang/license` | `LICENSE` blob `261eeb9e9f`, `main` |
| Default logs store | `https://raw.githubusercontent.com/grafana/loki/v3.7.9/LICENSING.md` | Tag v3.7.9 |
| Pydantic AI | `https://api.github.com/repos/pydantic/pydantic-ai/license` | `LICENSE` blob `1bf1f55e6d`, `main` |
| OpenAI Agents SDK | `https://api.github.com/repos/openai/openai-agents-python/license` | `LICENSE` blob `e5ad2c5aa3`, `main` |
| Agent Client Protocol | `https://api.github.com/repos/agentclientprotocol/agent-client-protocol/license`; `https://agentclientprotocol.com/protocol/session-setup` | `LICENSE` blob `1de02305f8`, `main`; page as served |
| Candidate MCP server for leg C1 | `https://api.github.com/repos/VictoriaMetrics-Community/mcp-victorialogs/license` (the API answers for `VictoriaMetrics/mcp-victorialogs`) | `LICENSE` blob `ab88bcce6d`, `main` |
| dsh rows | The installed package `@deepseek-ai/dsh` and its base bundle `cordis.patch.yml` | 0.2.2-rc.1 |
| Leads, not proof | `docs/research/licence-audit-agent-ai.md`, `docs/research/licence-audit-observability-stack.md`, `docs/research/dsh-architecture.md` | Of this repository |

---

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
