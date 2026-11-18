# S0 comparison amendment 4 — snapshots after an un-awaited disposal

Status: written and committed **before** any comparator change and before any run
with the new rule. The order is provable from history: this file and
[`s0-causal-comparison-freeze-4.json`](s0-causal-comparison-freeze-4.json) are their
own commit; the comparator, its tests and the evidence follow in later commits. The
rule was derived from the oracle (`test/differential/oracle/replay.mjs`, the
`dispose` case and `checkpoint`), the recorded D22 oracle trace and the measured
candidate behaviour in [the coverage note](s0-causal-coverage.md), section "D22 and
D06 fixes (2026-11-19)". No half-written comparator was probed.

Authority: the repository owner decided on 2026-11-19 that the remaining D22
difference is a measurement matter, to be handled by a new comparator rule introduced
through a written amendment, not by adding synchronisation to the candidates. It
extends [amendment 1](s0-causal-comparison-amendment.md),
[amendment 2](s0-causal-comparison-amendment-2.md) and
[amendment 3](s0-causal-comparison-amendment-3.md) (with the addenda) and replaces none.
The original criteria SHA-256 stays
`628bd6ec24e5983bfbe9fcdada096eaa84db3e95227f11e8fd5a7fa33a26f19c`; strict `Compare`,
historical results, the corpus and the candidates are unchanged. This amendment does
not establish S0 acceptance.

## The measurement problem

`dispose parent {await:false}` returns after its synchronous part. The oracle runs
every continuation to completion, so the next root step, `snapshot joining-setup`, is
taken synchronously and always after exactly the 14 events of the synchronous part
(`plugin parent disposed`, `parent ACTIVE -> UNLOADING`). The Go candidates unload on
their own goroutines while the replay goroutine takes the snapshot, so the cut holds 14,
15 or 18 events (`reconciler` 35/100 and 38/100 of the runs in two measurements,
`graph-lock` 99/100 and 100/100). After the fix of the teardown order both candidates
produce the oracle's event sequence and final states; only the position of the cut
inside the teardown differs. No semantics orders the teardown against that snapshot:
a snapshot has no wait, the disposal was explicitly not awaited, and no fixture barrier
can be written in both runtimes.

## What this amendment may and may not do

It adds one relaxation, stated once below, to the comparison of a cut that follows an
un-awaited disposal, and adds no other proving power and no other relaxation. Every
check of amendments 1 to 3 stays: complete event content and multiplicity, per-lane
order, conflict order, state, epoch, config and output equality at every other cut,
identity bijection, witness hashes, the unsupported-is-not-a-match rule, and the
validation of each trace alone. No scenario id or label is used, no event is sorted,
dropped or merged, and no tolerance knob exists. Where the stated scope does not hold,
the comparison is exactly the old one.

## Vocabulary

- **Un-awaited disposal** (UD): a root `dispose` step with `await:false`, with or
  without `task`. Its **own event** is the root-lane `plugin {disposed:true}` event of
  its target F; it belongs to the synchronous part of the command and is never
  floating.
- **Float set** Fl(UD): F and every structural descendant of F (children mounted from
  F's Apply, transitively), exactly the fibers whose teardown F's disposal starts by
  the existing ownership model (amendment 2, C2). The set is structural only. **Scope
  condition:** no fiber of Fl provides an address, writes an output cell or is the
  target of any other root command (`update`, `restart`, `rawConfig`, `inspect`,
  `resolveConfig`, `dispose`, `lateEffect`). Fibers that depend on a provision of Fl
  exist only if Fl provides something, so under the scope condition there are none; if
  the condition does not hold the relaxation is not applied at all and the old strict
  comparison stays.
- **Ordering command** after UD: a root `awaitFiber` of a fiber of Fl, or a root
  `waitMark` of a label every static producer of which is a fiber of Fl (a mark or gate
  of Fl's bodies and effects). Such a command returns only after work UD started, so
  what precedes it is ordered after that work.
- **Floating cut**: the cut that ends the segment in which UD was issued, when no
  ordering command lies between UD and that cut; and every later cut up to, but not
  including, the **barrier**. The barrier is the first later cut that is a `settle`, or
  whose segment contains an ordering command (when UD is followed by an ordering command
  before its own cut, that cut is the barrier and nothing floats). A script without a
  barrier floats to its last cut and the tail.
- **Window**: the segments from the one that ends at the first floating cut through the
  barrier segment inclusive (through the tail when there is no barrier).
- **Floating event** (per trace): an event of the window that comes after UD's own
  event in that trace and whose actor is in Fl. Every other event of the window is
  **pinned**.

## Capability added

### L1. A cut after an un-awaited disposal does not order the work the disposal started

Rule, in terms of the comparison of two traces `expected` (oracle) and `actual`, for
every window as defined:

1. **Pinned events stay on their side.** For every segment of the window the pinned
   events of the two traces, in order, are compared exactly as before (same content,
   multiplicity, and every conflicting pair in the same order). An event that is not
   floating therefore cannot move across any cut of the window.
2. **Whole window, causal order.** The whole window, floating and pinned events
   together, must have the same events with the same multiplicity on both sides, and
   every pair of events that conflicts under the existing model (same lane, shared
   cell, ordered lanes) must be in the same relative order. This keeps the order of
   every lane (a fiber's teardown events, a gate and what follows it) and the order
   between causally ordered lanes unchanged; only a floating event's position against
   a cut may differ.
3. **States at a floating cut.** For a fiber of Fl the actual state at the cut must be
   a state the expected lane holds in the window: the state F or the descendant is in
   at UD's own event and every `to` of the expected status events of that fiber after
   UD's event up to the end of the window. Its `fiber`, `revision`, raw and effective
   config must equal the expected ones; its `epoch` and `fiberUID` are compared only
   when the two states are equal (they are functions of the state, and each trace's own
   epoch/uid consistency with its state is validated alone as before). Every fiber
   outside Fl, all provisions and all outputs are compared exactly as before.
4. **The barrier and everything after is exact.** The barrier cut, the pinned events of
   its segment, the cumulative content up to it (rule 2 ends there), its states, and
   every later segment and checkpoint are compared exactly as before. At the barrier
   the downstream work is therefore complete and equal.
5. **The end of the trace is unchanged:** all events present, same per-lane order, the
   tail compared as before.

Each trace is also validated alone exactly as before: the recorded states of a floating
cut must equal the states reconstructed from that trace's own events, a `LOADING` or
`UNLOADING` state needs its parked-gate witness, and every gate, lane, program-order and
lifecycle rule applies unchanged. The relaxation therefore never admits a state the
trace's own events do not produce.

How "downstream of UD" is decided, and why this is not guesswork. It is decided
statically from the script by the existing structural ownership relation and then
checked on each trace: a floating event must carry the actor of a fiber of Fl and occur
after UD's own event. Descendancy comes from the Apply bodies that mount children, the
same relation amendment 2 uses to bound a parent's `DISPOSED` by its children's. The
scope condition removes every other way for an event to depend on UD (provisions,
outputs, other commands). An event by any other actor is pinned; a root-lane event is
pinned.

Why real differences are still rejected.

- An event of an **unrelated lane** moved across the cut is pinned, so rule 1 compares
  its segment exactly and rejects it.
- A **missing** downstream event, on one side or on both (rule 2 and the per-trace
  program-order and completeness rules), a **duplicated** one (multiplicity), and
  **reordered** downstream events inside a lane (rule 2, same-lane conflict) are
  rejected.
- A fiber **observed at the cut in a state the oracle lane never passes through** is
  rejected by rule 3 (and, for a state its own events do not produce, by validation
  alone).
- Work that is **not complete at the next ordered cut** is rejected by rules 2 and 4:
  the window ends at the barrier, so a downstream event that has not occurred by the
  barrier is missing from the window, and the barrier's states and pinned events are
  exact.
- The relaxation **does not apply** to an **awaited** disposal, to a cut ordered after
  the work by an ordering command, or when the scope condition fails: those keep the
  old strict comparison.
- A downstream event that floats **past the barrier** is missing from the window, so it
  is rejected.

## Decisions about other non-awaited commands

Not extended, deliberately. An `update` with `await:false`, a `dispatch` with
`await:false` and a `selfDispose` already have explicit lanes, requests, tasks and
relations (amendments 3, K3 to K8) and a script that snapshots right after them must
already supply a witness; no scenario of the corpus floats on them and no rule here is
stated for them. `mount` is never awaited by the root and a snapshot right after it
without a wait is refused as a race (amendment 2, A2). They stay as they are.

## Capabilities not added

The relaxation for Fl that provides addresses or writes output cells, for fibers that
depend on a disposed provider, for un-awaited commands other than `dispose`, and for
cuts that follow an ordering command. A pair needing one of them keeps the strict
verdict (and, where it is not provable, `ErrCausalUnsupported`).

## Execution

All scenarios are rerun against the pinned oracle (Node 24.22.1) on both candidates,
with the latest fixture revision where one exists (D13 revision 5, D22 revision 4). A
pair is `match` only when it matches 100 of 100 runs under `-race` and 100 of 100 with
`GOMAXPROCS=2` under `-race`. Every pair accepted before must be accepted again for the
same reasons, and no previously rejected false-green test may start to pass. D22 moves
to the accepted list only if it matches 100 of 100 in both modes on both candidates;
otherwise its rate and differing events are reported and its row stays truthful.

## Negative tests required

Mutated or hand-built traces that must be rejected as mismatches (never
`ErrCausalUnsupported`), one-sided and with the identical corruption on both sides where
meaningful: an unrelated lane's event moved across the same cut; a downstream event
missing on both sides; a downstream event duplicated; downstream events reordered inside
a lane; a fiber observed at the cut in a state not on its oracle lane; a cut after an
**awaited** disposal with the same floating; completeness at the following ordered cut
violated. Positive controls: the recorded oracle trace of D22 against itself and against
traces whose floating cut holds 15 and 18 events.
