# S0 comparison amendment 3 — added causal capabilities for hooks, update control and metadata

Status: written and committed **before** any comparator change and before any run
with the new rules. The order is provable from history: this file and
[`s0-causal-comparison-freeze-3.json`](s0-causal-comparison-freeze-3.json) are
their own commit; the comparator, its tests and the evidence follow in later
commits. Unlike amendment 2 no probe of a half-written comparator preceded this
text: every rule below was derived from the oracle (`test/differential/oracle/
replay.mjs` and the vendored Cordis sources `events.js`, `fiber.js` as bundled in
`oracle/vendor/cordis/lib/index.js`), the Go hook engine (`test/spike/s0/hooks`) and
the recorded scripts and oracle traces, and from the **existing** comparator's
unsupported verdicts (listed below). If implementation shows a rule is missing,
an addendum is committed before any recorded run, as for amendment 2.

Authority: the repository owner decided on 2026-11-18 that the comparator may be
extended, but only through a written amendment recorded before execution that
names each added capability and explains why it does not loosen the criteria,
followed by a rerun of all scenarios. This amendment is that record. It extends
[amendment 1](s0-causal-comparison-amendment.md) (ADR-035),
[amendment 2](s0-causal-comparison-amendment-2.md) and its
[addendum](s0-causal-comparison-amendment-2-addendum.md); it replaces none. The
original criteria SHA-256 stays
`628bd6ec24e5983bfbe9fcdada096eaa84db3e95227f11e8fd5a7fa33a26f19c`; strict
`Compare`, historical results, the corpus and the candidates are unchanged. This
amendment does not establish S0 acceptance.

## Why this amendment exists

After amendment 2 the comparator accepts 34 candidate/scenario pairs and reports 6
mismatches (D06, D22, D23 on both candidates; owner questions, untouched here).
28 pairs, scenarios D01, D17, D20, D24, D25, D28, D29, D30, D31, D32, D33, D34,
D35, D36 on both candidates, are `ErrCausalUnsupported`. All 14 are strict
(total-order) matches today, so the comparator is the only reason they have no
causal verdict. The existing comparator, run once per scenario (rate recorder,
one run, results not recorded as evidence), gave these first reasons; a scenario
can contain more than the first.

| Scenario | First unsupported reason (existing comparator) |
|---|---|
| D01 | runtime metadata expression (`$meta`); also a root `provide` command |
| D17, D20 | async/control metadata on `update` (`await:false`, `task`) |
| D24 | async/control/catch metadata on `lateEffect` |
| D25 | async/control/catch metadata on `selfDispose` |
| D28, D30 | custom update continuation/veto policy (`retain`, `unchanged`) |
| D29 | body operation `listen` (an `internal/update` listener) |
| D31, D36 | async/control/catch metadata on `listen` (`symbol`) |
| D32, D33, D34, D35 | nested root operation `listen` |

## What this amendment may and may not do

It may only add the ability to **prove** facts. It does not weaken any check of
amendments 1 and 2: complete event content and multiplicity, per-lane order,
conflict order, outcome/state/epoch/config/output equality, identity bijection,
witness hashes and the unsupported-is-not-a-match rule all stay, and every pair
accepted before must be accepted again for the same reasons. No scenario id or
label is switched on, no event is sorted, dropped or merged, and no tolerance knob
exists. A construct without a stated rule stays `ErrCausalUnsupported`.

The two defences against the "same invalid stream" false-green class of amendment
2 apply to every capability: each trace is **validated alone** against the rule
(so a violation shared by both sides is rejected before any comparison), and
content, multiplicity and every conflicting order are then compared between the
two traces (so a violation on one side only is rejected too). A violation is
reported as a mismatch, not as `ErrCausalUnsupported`, whenever the trace itself
proves it.

Where this amendment gives an event a **new lane** it names the lane and every
relation between it and other lanes. Two events of different lanes keep their
relative order in both traces if and only if they share a cell (label, output
cell, service cell) or a relation below orders them; an unordered pair is
unordered because the semantics of the construct (not the schedule seen) leaves
it so, and the reason is stated.

## Vocabulary added

- **Command-driven event** (`config`, `commit`, `update`): produced by the
  synchronous part of a root `update` or `resolveConfig` command on a *loaded*
  fiber (`ACTIVE`, `LOADING` after its `apply` event, or `UNLOADING`). It carries
  the target's `fiber` field and sits in the target's lane **and** in the root
  command lane. A `config` event is **frame-driven** (not command-driven) exactly
  when it is the first `config` event of the fiber since it entered `LOADING`;
  `commit` and `update` events are always command-driven. Command-driven events are
  not frame events: a gate parked in the target's frame does not block them (K6.2).
- **Bundle** of a command, in this order: `update` command: `config`, then
  `commit` (absent for `mode:"noSave"`), then one `update` event only if the
  plugin declares policy `retain` or `unchanged` (K5); `resolveConfig`: `config`.
  Bundles of the commands targeting one fiber occur in script order (the root
  issues commands sequentially and the synchronous part of command *k* ends before
  command *k+1* starts).
- **New-shape fiber**: a fiber whose plugin declares an update policy or an
  `internal/update` listener, or that is the target of an `update` with
  `await:false` or `task`, or of an `update` issued while an earlier mutation
  or startup of the same segment is not settled (K6). Fibers with only the
  amendment-1/2 command shapes keep exactly the amendment-1/2 rules; K5, K6 and
  K7 apply to new-shape fibers.
- **Chain lane** `chain:<fiber>:<label>#<k>`: the invocation of the `internal/update`
  listener `<label>` registered by `<fiber>`'s Apply for the *k*-th `update`
  command on that fiber. Identity: fiber, label, ordinal of the command.
- **Dispatch lane** `dispatch:<key>[:symbol]`: the activity of the one `dispatch` of
  an event key. **Listener lane** `hook:<label>`: one `listen` registration,
  used for `emit` and `parallel` only. Identity: event key (plus the symbol flag)
  and the unique listener label.
- **Task**: a name given by `task` on an `update` or `dispatch`. A task is
  *complete* when every event its construct will produce has occurred (stated per
  construct) and, for a restart-capable update, when the fiber is stable.

## Capabilities added

### K1. Static context metadata (`$meta`) (D01)

Rule.

1. A `$meta` expression is accepted in a `value`, `base`, `config` or `meta` field
   when it is evaluated in a *static* context and its path resolves inside the
   statically merged metadata of that context. The evaluating context is: for a
   plugin body, the fiber's mount context (for a context created by a body
   `context` operation, that registered context); for a root command, its `context`
   field, else the context of its `fiber`, else `root`. Static metadata is the
   merge, parent first, of the `meta` of the script `contexts` entries (and body
   `context` entries) along the parent chain; a key of a later context shadows
   the earlier one per key.
2. The first path segment must be a key of that merged metadata; later segments
   index into the JSON value. An empty path, a first segment that is not a
   metadata key (a runtime member of the context object) or a context with
   non-static metadata stays `ErrCausalUnsupported`.
3. Metadata is fixed when a context is created, so the value cannot depend on the
   schedule. When an `output` operation's value is exactly one `$meta`
   expression, the `output` event value must equal the statically resolved value
   (validated alone, JSON-equal, an unresolved path being `{"$undefined":true}`).
   Elsewhere the value is compared by content between the traces, as `$config` and
   `$output` values are today.

Why a real difference is still rejected. A wrong or missing metadata value in an
output is rejected alone (rule 3) and by content between traces; a value read
from the wrong context (parent instead of child) differs from the static value;
an output missing on both sides fails the end-of-script count of root and fiber
outputs. The only unproved region (a `$meta` nested in a larger value, or in a
config) is compared by content only, which is the existing treatment of
`$config`.

### K2. Root provision command (`provide` as a step) (D01)

Rule.

1. A root step `provide` with `key`, a pure-JSON `value` (no `$config`, `$meta`,
   `$output`), no `to`, default mode, optional static `context`, is a root
   command. Its address (`key`, realm of its context) has exactly this one
   provider: no fiber provides it and no second root step provides it; otherwise
   `ErrCausalUnsupported`.
2. The trace must contain exactly one `service` event for the address from this
   command. It is a root-lane event, its `value` must equal the command value
   (validated alone), and it occurs after the awaited prerequisites of the
   command (rule C1.2 of amendment 2).
3. From that event on the provider `root` is `ACTIVE` with activation 1. A fiber
   that needs the address may not leave `PENDING` before that event (the existing
   readiness rule, with `root` as provider), and its binding carries provider
   `root` and activation 1. A checkpoint lists the provision with provider
   `root`, `active:true`, the command value, in insertion order with the fiber
   provisions, with the store domain check of amendment 1 applied unchanged.
4. Identity token `provision:[key,realm,"root",1]`; the opaque UID may not change
   for the lifetime of the script (root is never disposed; a root disposal is not
   a supported command).
5. The `service` event conflicts with every event of a fiber that needs the
   address (service cell write against read) and with every root-lane event, so
   its position relative to consumer startup is the same in both traces.

Why a real difference is still rejected. A consumer loading before the
publication, a value differing from the command, a missing or duplicated
publication, a publication by another provider, a stale binding and a swapped
order of publication and consumer startup are rejected by rules 2, 3 and 5 and by
the identity rule.

### K3. Self-disposal request from Apply (`selfDispose`, `await:false`) (D25)

Rule. A top-level operation `selfDispose` (no `await` or `await:false`, no `task`,
no `catch`) in the Apply body of a fiber mounted by a root `mount`, which is
never itself the target of a root `dispose`, `update` or `restart`. `await:true`
is the D26 not-carried boundary and stays unsupported. Per activation:

1. Exactly one `selfDispose` event `{fiber, await:false}`, in the fiber's lane,
   while the fiber is `LOADING` after its `apply` event, at its program position
   in the exact Apply sequence (so a `mark` written after it occurs after it).
2. The fiber's `plugin disposed:true` event is a lane event of the fiber (not a root
   command), after the `selfDispose` event, and is the only cause of teardown.
3. The fiber leaves `LOADING` for `UNLOADING` only after its whole Apply body ran
   (a request does not abort a running Apply; the frame returns first). Cleanup
   body, cleanup events and `UNLOADING -> DISPOSED` follow the amendment-1/2 rules
   for a disposed fiber.

Why a real difference is still rejected. A teardown that begins before Apply
returned, a disposal without a request, a request that is never followed by
teardown (fiber stays `LOADING`/`ACTIVE`), a missing or duplicated request event
and a cleanup before the request are rejected by rules 1 to 3 and by
multiplicity.

### K4. Late-effect refusal (`lateEffect`) (D24)

Rule. `lateEffect {fiber, label, catch:true, to}` as an operation in an effect's
cleanup program (Apply-registered effect, executed during teardown) or as a root
command. `fiber` names a root-mounted fiber; `catch` must be true and `to`
non-empty (an uncaught refusal would throw out of the script). Every execution
produces, adjacent in the lane of the executor, one `error` event
`{op:"lateEffect", error:{name,message,code}}` and one `output` event `{to}`.

1. Number of executions: one per run of the effect cleanup program (each teardown
   of the registering fiber, exactly once per activation, in program order with
   the other events of that cleanup program) and one per root command.
2. The `error` event is legal only while the target fiber is `UNLOADING` or
   `DISPOSED`; its payload `code` must be `INACTIVE_EFFECT`. A refusal while the
   target is `PENDING`, `LOADING`, `ACTIVE` or `FAILED` is rejected (a live target
   would have created the effect silently).
3. Attribution of an `error` event with `op:"lateEffect"` (no `fiber`): the
   candidates are the registering fiber(s) with an effect cleanup program in
   teardown (phase witness `UNLOADING`, rule vocabulary of amendment 2) and the
   root commands not yet consumed whose awaited prerequisites hold (an awaited
   root `dispose` must have reached `DISPOSED`, A1 of the addendum). Exactly one
   candidate is attributed; zero is a mismatch, several is
   `ErrCausalUnsupported`.
4. The `output` cell is written by the executor (fiber lane or root lane); a cell
   shared by a fiber and the root is `ErrCausalUnsupported`, as for every output.

Why a real difference is still rejected. A refusal event missing on both sides
fails rule 1's count, an effect accepted while the target is live fails rule 2, a
refusal with another code fails rule 2, a refusal outside teardown or by the wrong
owner fails rule 3, a swapped error/output pair fails lane order, a second run of
the cleanup program fails the single-shot rule of amendment 2 (C3.2).

### K5. Update continuation policies `retain` and `unchanged` (D28, D30)

Rule. The plugin field `update` may be `retain` or `unchanged` (also with
`configTransform`, already supported). `reject` and `next` stay unsupported. The
target of every `update` command must be `ACTIVE` when the command is issued
(witness: `ACTIVE` in the previous checkpoint and no mutation or startup pending
since, otherwise `ErrCausalUnsupported`). The bundle of such a command carries an
`update` event `{fiber, config, mode}` with `mode` equal to the plugin policy and
`config` the resolved candidate.

1. `retain`: no restart request ever follows. The fiber must not leave `ACTIVE`
   because of the command, no `apply` and no `LOADING` occurs, the revision stays
   unchanged, and the effective configuration after the command is the `update`
   event's `config`.
2. `unchanged`: compare the event's `config` with the effective configuration in
   force just before the event (the last `apply` or `update` config of the fiber,
   JSON-equal). Equal: veto, same obligations as `retain`. Different: exactly one
   restart request, served by exactly one `UNLOADING -> LOADING -> apply` cycle
   with `apply.config` equal to the event's `config`, and the revision increases by
   one.
3. `mode:"noSave"` produces no `commit` event and the same continuation rules.
4. `resolveConfig` (already supported) never publishes: no `commit`, no `update`
   event, no lifecycle change; its bundle is the single `config` event.

Why a real difference is still rejected. A restart after a veto is
`UNLOADING` without a request (mismatch, rule 1/2 and K6.3). A missing restart for
a changed target leaves a request unserved at the settled cut (K6.3). A policy
event with the wrong mode, a missing or duplicated hook event, a published
candidate that the veto should have kept out (effective configuration checked by
content in the checkpoint and by the sanity rule of K7.5) and a commit under
`noSave` are rejected by content, multiplicity and lane order.

### K6. Asynchronous root updates, coalescing and tasks (D17, D20; shared with D29)

Rule.

1. A root `update` may carry `await:false` and `task`. A task name is unique
   across update and dispatch tasks. `await:false` does not block later root
   commands; they inherit only the explicit obligations of the root program
   (`waitMark`, `awaitFiber`, `awaitTask`, awaited commands). A root `update` is
   accepted when its target is a loaded fiber and the target's lifecycle is
   *determinate* at the command: no startup/mutation of its segment is pending
   unless a `waitMark` was issued since the last cut (the wait proves the
   target's state: stable, or parked at a gate whose arrival was awaited and not
   yet released). Otherwise `ErrCausalUnsupported` (the rule of amendment 2, A2,
   for disposals). `restart` keeps the amendment-1 rules (no async form).
2. **Command events are not frame events.** Bundle events of a command (see
   Vocabulary) are root-lane events of the target, produced by the synchronous
   part of the command. They must occur after the command's awaited obligations
   (waits) and may occur while the target's frame is parked at a gate; they
   neither clear nor are blocked by the parked gate. Every other event of the
   target lane keeps C1.3 of amendment 2 (a parked gate blocks the rest of the
   frame until the cut shows the release).
3. **Request accounting (coalescing, supersession).** A restart request is
   recorded at the command's `commit` event (default policy; at its `config` event
   under `noSave`) or at the `update` event of K5.2. Requests are counted per fiber
   per segment. An `UNLOADING` of the target whose cause is an update (not a
   disposal, a bound provider's retirement or an own failure) requires a pending
   request. The next `LOADING` that follows consumes **all** pending requests
   (one reload serves every request made before it started: Cordis `_refresh`
   re-arms one epoch, `_unload` ends in one `_reload`). A second `UNLOADING` or a
   second reload without a new request is a mismatch; at every settled cut no
   request may remain pending ("request not served").
4. **Latest target.** For a new-shape fiber whose plugin has no `configTransform`
   the frame `config` event of every `LOADING` equals the raw configuration of the
   latest `commit` (or of the mount command) occurring before it, and the
   following `apply` carries the same value. A stale target (an earlier
   candidate applied after a later commit) is a mismatch. A fiber may not reach
   `FAILED` while a request is pending (a superseded load error is logged by its
   `error` event but cannot fail the latest target; the fiber goes to
   `UNLOADING`).
5. **Revision and raw configuration.** The checkpoint revision of a new-shape
   fiber equals the number of restart requests made so far (each request calls
   `restart()` once, even when coalesced: D17 ends at revision 2, D20 at 1; `retain` and
   an `unchanged` veto add none). The raw configuration is the last committed
   candidate. The commits of a segment are in script order and carry the commands'
   configurations (pure JSON).
6. A task of a restart-capable update is complete when its request is served and
   the fiber is stable; a `settle` completes every task. `awaitTask` of such a
   task is not added (no rule gives its completion an observable event).
7. `awaitTask` (root): `task` names an issued task of a dispatch (K8) or of an
   update with an `internal/update` listener (K7). It blocks the root: every later
   root command's events, and its own `error`/`output` events, occur after the task
   is complete. With `catch` and `to`: if and only if the task is rejected (rules
   of K7 and K8 decide rejection statically), one `error {op:"awaitTask"}` event
   and one `output {to}` event are produced in the root lane, in that order; they
   are absent otherwise. Without `catch` a rejected task is `ErrCausalUnsupported`.
   A root output cell is also expected exactly as many times as the script
   executes the command that writes it (end-of-script count, new for every root
   output, no existing scenario has a conditionally missing root output).

Why a real difference is still rejected. An update that restarts twice
(coalescing violated), an update restarting after a veto, a missing restart, a
stale candidate applied, a superseded load failing the fiber, a revision that
counts the coalesced requests wrongly, a command event ahead of its awaited
`waitMark`, a continuation that overtakes a gate parked in its own frame, an
`awaitTask` that resolves before its task and a missing or extra rejection event
are rejected by rules 2 to 7 and by content and multiplicity. The freedoms are:
command events versus frame events of other lanes are unordered only through the
gate exemption of rule 2, which the command's own waits bound from below.

### K7. `internal/update` listeners installed by Apply (D29)

Rule. A top-level `listen` with key `internal/update` (no `symbol`, `global`,
`prepend`) in the Apply body of a root-mounted fiber, unique `label`, whose program
uses only `when`, `gate`, `mark`, `output`, `fail`, `return` (no `next`), where
every `when` compares `{"$config":path}` (the resolved candidate) or a literal with
a literal. The plugin has no `configTransform` and no policy, so the candidate
equals the command's configuration (pure JSON), and the listener can be evaluated
statically per command. Because no body calls `next`, every update of the fiber is
retained: no request is ever made (K6.3 never starts a reload for it).

1. Each `update` command on the fiber starts one chain lane. Its items, computed
   statically from the candidate, are the `listener {label}` event, the events of
   the executed `mark`/`gate`/`output` operations in program order, ending at a
   `fail` (the chain is rejected) or at the program's end (not rejected). Chain
   items after a gate form the continuation.
2. The chain's `listener` event occurs after the command's bundle `commit` event
   (its `config` event under `noSave`). With several chains not started,
   a `listener` event is attributed to the oldest chain whose bundle is complete.
3. A gate parks the chain lane: continuation items occur after the release (the
   cut shows it, C1.3). A chain item occurs while its fiber is `ACTIVE` in the same
   activation; an item after the fiber left `ACTIVE` or after its disposal is a
   mismatch (a hook continuation may not complete after its fiber left).
4. Completion of a chain's task (K6.7): every chain item consumed. An awaited
   command (default `await`) completes its chain before later root events.
5. Effective configuration. The deferred and conditional publication (no
   `update` event exists for a listener-only chain) is not recomputed. At every
   checkpoint the effective configuration of the fiber must equal the `apply` config
   or the resolved candidate of one of its update commands (never an invented
   value); beyond that it is compared by content between the traces, together with
   raw configuration and revision (K6.5).

Why a real difference is still rejected. A listener missing or duplicated, one
that ran for the wrong candidate (the `when` branch is statically known), a
continuation before its gate arrival or before the release cut, after its fiber
left, a stale update published (checkpoint effective configuration differs from
the oracle's) and a rejection event without a rejection are rejected by rules 1
to 5 and by content.

### K8. Host hook dispatch: `listen`, `dispatch`, `return`, `next` (D31 to D36)

Scope. Root `listen` steps (key, optional `symbol`, unique `label`, program) and
root `dispatch` steps (key, optional `symbol`, `mode` in `emit`, `parallel`,
`serial`, `bail`, `waterfall`, `to`, `value`, `catch`, `await`, `task`). `global` and
`prepend` stay unsupported. The listeners of a dispatch are the `listen` steps with
the same key identity (key, symbol flag) that precede it in the script, in
registration order. Each key identity is dispatched at most once (otherwise
`ErrCausalUnsupported`). A listener program is a sequence of `mark`, `gate`,
`output`, `fail`, `return`, `next` (only waterfall) with no `catch`, no nesting.
Permitted per mode: `emit`: `mark`, `gate`, `output`, `return`; `parallel`:
those plus `fail`; `serial`: `mark`, `gate`, `output`, `fail`, `return`; `bail`:
`mark`, `output`, `fail`, `return` (a gate would return a promise, a truthy bail
value); `waterfall`: `mark`, `output`, `fail`, `return`, `next`. `return` values are
pure JSON literals. A value is *bailed* unless it is `null`, `false` or
`{"$undefined":true}` (so `0` and `""` bail), which is the pinned `isBailed` and
the Go `Decision.Stop` contract. `await:false` is accepted for `serial` and
`parallel` only, with `task`, no `catch`.

Static plan. Evaluating the programs yields, per dispatch, the exact events each lane
produces and the outcome (resolved or rejected). Lanes, relations and events:

- **serial, bail, waterfall**: all events (listener, program, `inner`) and the
  completion events are in the dispatch lane, in program order. `serial` runs
  listener *i+1* only after listener *i* finished, gates included; the run stops
  at the first bailed return value or `fail`; later listeners never run. `bail` is
  the same without gates. `waterfall` is a nested call chain: for each listener in
  order the `listener` event, the operations up to `next`, then the whole rest of
  the chain, then the operations after `next` (a `to` on `next` produces its
  `output` after the call returned); a listener that does not call `next` vetoes
  every later listener and the built-in step (no `inner` event); when the chain
  is exhausted the built-in step produces `inner {key}`; a second `next` in one
  body throws `next() called multiple times`, which propagates out of every
  enclosing listener to the dispatch.
- **emit**: listener *i* is lane `hook:<label>`: `listener`, then its operations; no
  failure. Every listener is invoked, in order; the **prefix** of a listener is its
  events up to and including its first `gate` (all of them without a gate); the
  prefix of listener *i* is complete before the `listener` event of *i+1*
  (`Emit` calls listeners in order and does not await a returned promise, pinned
  `emit` and Go `Emit`); items after a gate are the unawaited continuation.
- **parallel**: lanes `hook:<label>` as for `emit`, with `fail` ending a listener
  as rejected; every listener is invoked regardless of failures, prefixes in
  order (as in `emit`); the dispatch finishes after every listener lane is
  complete; it is rejected iff any listener failed (AggregateError).
- **Completion events** (dispatch lane): resolved with `to`: `output {to}`; rejected,
  awaited, with `catch`: `error {op:"dispatch"}`, then `output {to}` when `to`
  is set; resolved or rejected with `await:false`: none (the task carries the
  outcome, K6.7). Rejected, awaited, without `catch` is `ErrCausalUnsupported`.
  The completion events occur after the dispatch is finished (emit: after every
  prefix; parallel: after every lane; serial/bail/waterfall: they are last in the
  lane).

Relations to the root. The dispatch lane and listener lanes start after the
command's awaited prerequisites (`waitMark`, `awaitFiber`, earlier awaited
dispatches and `awaitTask`). Unless `await:false`, every later root command's
events occur after the dispatch is *done*: dispatch lane complete and, for emit
only, every listener prefix complete (an emit continuation is not awaited); for
`parallel` every listener lane complete. A gate parks its lane: the events after
it occur after the release (C1.3). `awaitTask` and `settle` (K6.6/K6.7) complete the
tasks. A gate label, a mark label and an output cell written by hook lanes may
not also be written by a plugin body (disjoint, else `ErrCausalUnsupported`).

Conflicts. Events of one lane conflict (lane order). The dispatch lane conflicts
with the root lane. Two listener lanes of one dispatch share no cell and no relation
beyond the prefix and completion relations above, so their *continuations* are
unordered (a gated emit listener's continuation after release against the
following listeners and the root), and the listener starts of `parallel` are
ordered only by the prefix rule.

Why a real difference is still rejected. A listener missing or run twice, one run
after a bail or a veto, a listener out of registration order, a `next` called twice
without the error, an `inner` event after a veto, a continuation before its gate
release, a dispatch completion before a listener lane finished, an `awaitTask` result
before the task finished, a wrong or missing aggregate or rejection event and a
changed value are rejected by the plan (validated alone, each trace is mapped
onto it event by event), by lane order and by content. The freedom is only the
interleaving of listener continuations of one `emit`/`parallel`, which by the
semantics of those modes nothing awaits.

## Capabilities not added

- `update` policies `reject` and `next` (no scenario; `reject` needs an uncaught
  rejection); update listeners with `next`; update listeners with a
  `configTransform` plugin or a policy.
- `selfDispose` with `await:true` (D26 boundary), outside the top level of Apply,
  in a re-mounted or root-disposed fiber.
- Gates in `bail` and `waterfall` listeners, failing `emit` listeners, `catch`
  inside listener programs, `listen` or `dispatch` in plugin bodies (other than
  the K7 listener), a key dispatched twice, `global`/`prepend` listeners, async
  `restart`, `awaitTask` of a restart-capable update task.
- Root `provide` with `to`, a service mode, a non-pure value, a second provider.

Scenarios needing only these constructs are listed as unsupported with the exact
reason; none of the 14 targets needs one of them.

## Explicitly out of scope

Unchanged from amendment 2: D05 and D26 (not-carried boundaries); error text is
compared verbatim; fixture races are reported, not repaired; candidate defects are
reported as mismatches. D06, D22 and D23 are owner questions and are neither
touched nor reclassified by this amendment.

## Execution

All scenarios are rerun against the pinned oracle (Node 24.22.1) on both
candidates, with the latest fixture revision where one exists (D13 revision 5, D22
revision 4). A pair counts as `match` only when it matches 100 of 100 runs under
`-race` and 100 of 100 with `GOMAXPROCS=2` under `-race`. Anything less is reported
with its rate and differing events. Previously accepted pairs are rerun and must be
accepted for the same reasons.

## Negative tests required

For every capability: a hand-built or mutated trace with a real violation, checked
one-sided and with the identical corruption on both sides where meaningful.
Required cases, at least: a wrong metadata value (K1); a consumer loading before a
root publication, a changed publication value (K2); a teardown before Apply
returned, a missing request event (K3); a missing refusal on both sides, a refusal
while the target is live (K4); a veto followed by a restart, a missing restart, a
stale published candidate (K5); two reloads for coalesced requests, a stale
candidate applied, a superseded load failing the fiber, a command event before its
`waitMark` (K6); a listener after the fiber left, a continuation before its gate
arrival, a missing listener (K7); a veto ignored, `next` called twice without the
error, a bail that continues, a serial listener that starts before its predecessor
finished, a listener run twice, an `awaitTask` result before its task, a
listener out of order, a duplicate and a missing event on both sides (K8). Tests
of amendments 1 and 2 that pinned `selfDispose`, `lateEffect`, `listen`,
`dispatch`, `$meta`, async `update` or an update policy as unsupported described the
frozen scope; each is rewritten to a still-unsupported form (a rule-less variant)
and listed in the report.
