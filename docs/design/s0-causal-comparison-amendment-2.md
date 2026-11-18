# S0 comparison amendment 2 — added causal capabilities

Status: written and committed **before** any comparator change and before any
run with the new rules. The order is provable from history: this file and
[`s0-causal-comparison-freeze-2.json`](s0-causal-comparison-freeze-2.json) are
their own commit; the comparator, its tests and the evidence follow in later
commits.

Authority: the repository owner decided on 2026-11-18 that the comparator may be
extended, but only through a written amendment recorded before execution that
names each added capability and explains why it does not loosen the criteria,
followed by a rerun of all 36 scenarios. This amendment is that record. It
extends [amendment 1](s0-causal-comparison-amendment.md) (ADR-035) and does not
replace it. The original criteria SHA-256 stays
`628bd6ec24e5983bfbe9fcdada096eaa84db3e95227f11e8fd5a7fa33a26f19c`; strict
`Compare`, historical results, the corpus and the candidates are unchanged.
This amendment does not establish S0 acceptance.

## What this amendment may and may not do

It may only add the ability to **prove** facts. It does not weaken any check of
amendment 1: complete event content and multiplicity, per-actor order, conflict
order, outcome/state/epoch/config/output equality, identity bijection, witness
hashes and the unsupported-is-not-a-match rule all stay. Every pair accepted
before must be accepted again for the same reasons. No scenario id or label is
switched on, no event is sorted, dropped or merged, and no tolerance knob exists.
An unproved construct stays `ErrCausalUnsupported`.

Two defences against the earlier "same invalid stream" false-green class apply to
every capability below:

1. Each trace is **validated alone** against the rule (so a violation shared by
   both sides is rejected before any comparison), and
2. content, multiplicity and every conflicting order are then compared between
   the two traces (so a violation on one side only is rejected too).

## Vocabulary

- **Actor**: a fiber, or the root command lane. Every event is attributed to
  exactly one actor. Events carrying a `fiber` field use it. Events without one
  (`gate`, `mark`, `output`, `error` with `op`) are attributed to the unique
  static owner of their label or cell. If several actors could own the event,
  the **phase witness** decides: the owner must be the only candidate that, in
  that trace, is inside the frame that can emit the event (`LOADING` after its
  `apply` event for Apply-body operations; `UNLOADING` for cleanup operations).
  Zero or several candidates is `ErrCausalUnsupported` (zero candidates inside
  the frame is a mismatch when the stream shows the event outside any frame of
  every candidate), never a guess.
- **Lane**: the events of one actor. Lane order must be identical in both traces
  (unchanged rule, now also for the new event kinds).
- **Segment** `i`: the raw events between the cut of checkpoint `i-1` and the cut
  of checkpoint `i`, observed by the replay recorders. A root command written
  between checkpoint steps `i-1` and `i` is issued in segment `i`. A cut is a
  boundary the recorder observed; no event is inserted.
- **Label cell**: `label:<L>`. A `gate` or `mark` event is an **arrival** at it.

## Capabilities added

### C1. Gate, mark, waitMark and release (D11, D13, D21, D22, D23)

Rule.

1. A `gate L` or `mark L` operation in a plugin Apply or cleanup body, or in an
   effect program (C3), has exactly one static owner fiber (the label occurs in
   one plugin and that plugin is mounted once), or is resolved by the phase
   witness. Its event is in the owner's lane. An Apply-body arrival must occur
   while the owner is `LOADING` after its `apply` event of the current
   activation. A cleanup-body arrival must occur while the owner is
   `UNLOADING` after its own `cleanup` event of that activation.
2. `waitMark L n` (root): every event produced by a root command issued later in
   the script (`plugin` events of root mount/dispose, root `output`, the
   `error` event of a root `awaitFiber` with `catch`) must occur after the `n`-th
   arrival at `L` in the same trace, and the trace must contain at least `n`
   arrivals at `L` by the end of the segment in which the wait was issued.
3. `release L` (root, no event of its own): a gate arrival blocks its owner's
   frame, so every **later event of the same lane** must be in a segment not
   earlier than the segment in which `release L` was issued (the recorder's
   cut proves the release had not yet been issued earlier). A gate in an
   asynchronous effect program (`await:false`) does not block the lane; it
   blocks only the effect's cleanup-program events and the owner's exit from
   `UNLOADING` (the teardown joins the pending setup). Those events must also
   not precede the segment of the release of every gate in that program.
4. Compared as sequences: per-lane order of arrivals. Compared as sets with
   multiplicity: arrivals of different actors at **different** labels (the script
   can observe an arrival only through `waitMark`, rule 2, and influence it only
   through `release`, rule 3). Arrivals of different actors at the **same** label
   conflict, so their relative order must match in both traces.
5. A checkpoint may contain a fiber in `LOADING` or `UNLOADING` only if the
   trace shows an arrived, not yet released gate in that fiber's own frame at the
   cut, the arrival was awaited before the cut by an earlier root `waitMark` (or
   the gate is an unreleased asynchronous-effect gate behind such a wait), and no
   `release` of that label was issued before the cut. Otherwise the state is
   unproved and the result stays `ErrCausalUnsupported`, as before.

Why general, and why real differences are still rejected. A gate or mark is the
protocol's only cross-fiber causal construct. The rules restate what each
construct means (`mark`/`gate` append and notify, `waitMark` blocks the root,
`release` unblocks one frame) and add nothing scenario shaped. A release that
overtakes its continuation, a continuation before its gate arrival, a root
command running before the awaited arrival, a swapped same-lane mark pair, a
missing or extra arrival, an arrival by the wrong owner or outside its frame, and
a mark value change are all rejected by rules 1 to 4 and by content equality. The
only freedom is the relative order of arrivals at different labels by different
actors, which no script can observe.

### C2. Child fibers mounted from a parent body (D06, D22)

Rule. A `child` operation in a parent Apply body (or in a context created there
with the dynamic `context` operation, see C4) mounts one child fiber, once per
parent activation. Static requirements: fixed child id, known plugin and context,
and a parent that is never the target of a root `update`/`restart` and mounts
once (a re-mounting parent is `ErrCausalUnsupported`).

1. The child's `plugin disposed:false` event is in the child lane and must occur
   after the parent's `apply` event and before the parent's first `UNLOADING`.
2. The child's `plugin disposed:true` event must occur after the parent's
   `plugin disposed:true` (or the parent's `UNLOADING`), and no child may reach
   `DISPOSED` after its parent has reached `DISPOSED`. Equivalently the parent's
   `DISPOSED` follows the `DISPOSED` of every child (structural join).
3. Child lanes, the parent lane and their order relations are otherwise
   independent actors: teardown order between a parent's own cleanup callbacks and
   its children is not constrained, because the two share no cell, exactly as for
   any two independent fibers. Shared cells (labels, outputs, services) still
   conflict.
4. Child states appear in checkpoints in creation order; the list must equal the
   order of the `plugin disposed:false` events in each trace and must match
   between traces.

Why general. This is the structural ownership relation of a fiber tree, stated
once. A child created before the parent applied, a child outliving its parent, a
parent disposed before a child, a missing or duplicate child and a wrong-parent
child are rejected by rules 1, 2 and by multiplicity and lane equality.

### C3. Effects with bodies, and async/control/catch metadata (D18, D23)

Rule. Body operations `effect` (with `program`, `cleanup`, `to`, `await:false`,
`mode:"partial"`, `error`, `catch`) and `withdraw` (with `task`) are accepted,
together with `catch` on `effect`/`read`, `await:false` on `effect` and on root
`dispose`, `task` on root `dispose` and on `withdraw`, `count` on `waitMark`.
`global`, `symbol`, `prepend`, `listen`, `dispatch`, `awaitTask`, `selfDispose`
and `lateEffect` remain unsupported.

1. All events a fiber produces through effects, including setup-program events,
   cleanup-program events, caught `error` events with `op:"effect"`, and marks
   in cleanup programs, are lane events of the owning fiber, in program order.
2. Cleanup-program events are legal only while the owner is `UNLOADING`, or in
   `LOADING` after `apply` when the program is a `partial` unwind or a
   `withdraw` of that effect. The cleanup of one effect is emitted at most once
   (single-shot) and, for effects registered in one activation, in reverse
   registration order (the lane-order comparison already binds this to the
   oracle).
3. A caught `error` with `op` and no `fiber` is attributed by the phase witness
   (C-vocabulary). A fiber that reaches `FAILED` requires an error of its own.
4. A pending asynchronous setup (`await:false`) is joined by teardown: the owner
   leaves `UNLOADING` only after every effect cleanup program has run
   (rule C1.3 for the segment relation).

Why general. Effects are bodies of one fiber executed on one lane. Order
violations inside a body, a cleanup that runs twice or not at all, one outside
the unwind window, or in the wrong order, a lost or extra caught error, and a
failure without a cause are rejected by lane equality and the window rules.

### C4. Intercept contexts, NeedWith, service, and available provision (D07, D08, D09)

Rule.

1. `intercept` contexts are realm-transparent: they inherit the parent's realm
   map unchanged, so address resolution is the same as in the parent. A body
   `context` operation creating an `isolate` context is a static realm
   assignment identical to a script-level `contexts` entry (needed by D06).
2. `needs` values that are pure JSON (no `$output`, `$meta`, `$config`) are
   accepted. `provide mode:"service"` is an ordinary provision for ordering. A
   `read mode:"config"` is a read of the service cell followed by a write to its
   output cell. The merged result is carried by the output value, which is
   compared by content between the traces; the comparator never recomputes it.
3. `provide mode:"available"` makes the provision's readiness predicate a read of
   the output cell named by `value.ready`, indexed by the consuming context's
   `meta.audience`. A consumer may enter `LOADING` only if every available-mode
   address it needs has `outputs[cell][audience] === true` **at that point of the
   trace** (outputs are replayed from the `output` events). A consumer in
   `LOADING`/`ACTIVE` whose predicate is false may not exist, and a consumer
   that left `PENDING` while the predicate was false is rejected.
4. A `notify` root command may be followed by at most one `service` event per
   named key and realm, with the provider `ACTIVE`; it is a root-lane event that
   conflicts with the startup `LOADING` of every consumer of that address (so
   its relative order must be the same in both traces).

Why general. Intercept chains and NeedWith configuration change values, not
dependency order, and value changes are caught by content comparison (a missing
outer intercept key or a wrong winner is an output mismatch). Availability is
the one place where readiness is not provider state; rule 3 makes that predicate
an explicit read so a consumer that loads early, flaps, or loads for the wrong
audience is rejected.

### C5. Proof of ownership for an output cell shared across realms (D03)

Rule. An output cell may be written by several fibers (same or different
plugins, any realms). All writers of one cell write-conflict, so every pair of
output events for that cell, and events of any actor that reads it, keep their
relative order in both traces. Each `output` event is attributed to the unique
writer which, in that trace, is in the frame required by C1.1, has not already
produced the number of writes to that cell that its body can perform in this
activation, and (for a consuming plugin) is bound to the provider that the event
value is read from. No unique writer is `ErrCausalUnsupported`; an event while no
writer is in its frame is a mismatch.

Why general. Ownership is proved from the trace, not assumed from a label. A
write by a fiber that is not ready, a write by the wrong realm, a duplicate
write and a write outside the frame are rejected. Value differences are caught
by content equality.

### C6. More than one provider of one address (D04, D15)

Rule. An address may have several candidate providers (static: any fiber whose
plugin provides the key in the realm of its context). At every point of a trace
the store has at most one **owner** per address, derived from the events.

1. A `service` event while the provider is `ACTIVE` is a publication by the
   unique candidate that became `ACTIVE` and has not yet published; it requires
   the address to be unoccupied. A publication while the address is occupied (a
   replacement) is rejected.
2. Retirement notifications are attributed to the current owner and keep the
   existing rules. Consumer bindings carry the provider **and** its activation,
   so a consumer cannot be bound to a retired provider of the same address.
3. **Swap (D15).** The provider that replaces another publishes only after the
   former owner's deletion notification; consumers become `LOADING` again only
   after the new publication's provider is `ACTIVE`, and bind to the new
   provision identity.
4. **Refusal (D04).** A second candidate whose Apply performs `provide` on an
   occupied address must fail in that frame with an `error` event of its own, must
   never publish, goes `LOADING -> UNLOADING -> FAILED`, and the store, its owner
   and value are unchanged. The refusal is recognised only when the candidate's
   Apply body consists solely of `provide`, `output` and `mark` operations (it has
   no other error source) and the address was occupied by a live owner at the
   candidate's `apply` event.
5. **Error text by class.** The owner decision of 2026-11-18 records the D04
   duplicate-provision error **text** as not carried. For exactly the error
   event recognised by rule 4, the comparison fingerprint replaces
   `error.message` by the class `duplicate-provision-refused`. `error.name`
   (`Error`), the `fiber` field, the event's position and everything else stay
   compared exactly. Every other error event, including all other D04 events and
   any error in a stream whose rule-4 conditions do not hold, is compared
   verbatim.

Why general. Candidate ownership is derived from the stream, so a candidate
that replaces, double-owns, publishes while occupied, is refused but then
publishes, is refused without failing, or fails for a different reason (its
message is compared verbatim then) is rejected. The class folding applies to one
proven event shape and to no other text.

## Capabilities not added

None of the six is declared not added in this amendment. If implementation shows
that a rule above cannot be proven in code, the evidence receipts for this round
list that capability as not implemented; this amendment is then not claimed for
it and its scenarios stay `ErrCausalUnsupported`. No rule is bent to rescue a
pair.

## Explicitly out of scope

- **D05** (unchecked `ctx.get`) and **D26** (awaited self-dispose): not-carried
  boundaries (decision record section 15). They are not run through the
  comparator; `selfDispose` stays unsupported.
- Error **text** is never normalised except for the single class in C6.5.
- Dynamic contexts other than the body `isolate` form, `listen`/`dispatch`,
  `awaitTask`, `lateEffect`, update vetoes and runtime `$meta` expressions stay
  unsupported.
- Fixture races (a snapshot taken with no wait on an in-flight asynchronous
  transition, for example D13 revision 3 fiber `d` and D22 revision 4's
  `joining-*` snapshots) are not repaired by the comparator. A race that gives
  different observed states or segment contents is reported as a mismatch.
- Candidate defects (D14 reconciler publication order, D09 consumer churn and
  missing second `service` event) are reported as mismatches, never excused.

## Execution

All 36 scenarios are rerun against the pinned oracle (Node 24.22.1) on both
candidates, with the latest fixture revision where one exists (D13 revision 3,
D22 revision 4, D10 as in the current test). A pair counts as `match` only when
it matches 100 of 100 runs under `-race` and 100 of 100 with `GOMAXPROCS=2`.
Anything less is reported with its rate and differing events.

Negative tests are required for every capability: a hand-built or mutated trace
containing a real violation of the rule must be rejected (both-sides-same
violation, missing event on both sides, swapped causally ordered pair, wrong
owner, duplicate), next to positive tests. Tests of amendment 1 that pinned
`gate`, `mark`, `effect`, `child`, `available`, a second provider, or the
D15/D18 compile result as unsupported described the frozen scope and are
rewritten to the new scope, each keeping a still-unsupported control.
