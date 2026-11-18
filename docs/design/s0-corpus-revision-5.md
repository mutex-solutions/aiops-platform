# S0 corpus revision 5: explicit D13 activation barriers for fibers `a`, `d` and `u`

Status: the final script (draft 3) was planned and recorded **before its first
oracle replay and before it was run on any candidate**; this note and its
[pre-execution record](../../test/differential/results/corpus-revision-5/preexecution.json)
were written first. Disclosure, stated plainly: two earlier drafts were replayed
on the oracle (three recorded replays each) and run on the Go candidates before
this design (attempts 1 and 2, below). Each keeps its own note, pre-execution
record, oracle replays and captured failing Go traces under
`results/corpus-revision-5/attempt-1/` and `attempt-2/`. This is a fixture
causality correction, not a semantic adaptation, threshold relaxation or S0 pass.
The [original criteria](s0-spike-criteria.md), the
[revision-2](s0-corpus-revision-2.md), [revision-3](s0-corpus-revision-3.md) and
[revision-4](s0-corpus-revision-4.md) notes, all earlier freezes and all traces
and receipts remain unchanged historical evidence.

## Observation

Revision 3 fixed the barrier for fiber `f` only. Fiber `d` (plugin `disposed`,
empty Apply) is mounted and then disposed after `waitMark active-apply`, with
nothing ordering `d`'s activation first. The pinned JavaScript run to completion
disposes `d` while it is still `LOADING` (oracle events: `plugin disposed:true d`,
then `LOADING -> UNLOADING`, with no `ACTIVE`). The concurrent Go candidates may
instead have `d` activate first, or even not yet applied (`apply d` ordered after
`plugin disposed d`). The extended causal comparator then rejects the pair on the
number of recorded events: with the revision-3 fixture, 100 runs per candidate
under `-race` matched 0 of 100 on both (reconciler: 37 != 38 in 67, 37 != 36 in
29, 37 != 35 in 2, `apply d` / `plugin disposed d` order reversal in 2;
graph-lock: 37 != 38 in 44, 37 != 36 in 30, 37 != 35 in 20, order reversal in 6).
The original expectations pass either way (they assert only the six states at
`six`, `d` being DISPOSED in both cases). This is a fixture causality gap, not a
candidate defect. Revision 3 is retained as the record of that race.

## Attempt 1 (insufficient, retained)

Draft 1 inserted only `{"op": "awaitFiber", "fiber": "d"}` before `dispose d`
(script SHA-256
`7a015acdea27185a71305cddd61d8d33bea24fb5ddadfdaa561c201591fbaa97`, kept as
`attempt-1/D13.attempt-1.script.json`; its note and record name
`corpus/revision-5/D13.json`, which now holds the final script). On an idle host
it matched 100 of 100 on both candidates under `-race`, and again with
`GOMAXPROCS=2`. Under host load (two other `go test -race -count=200` copies
running) one of 100 `graph-lock` runs failed (`event segment 0: record count
mismatch: 38 != 37`), and a further capture pass under the same load reproduced the
verdict on both candidates (`reconciler` at runs 142 and 156, `graph-lock` at
runs 310 and 761 of a 1500 run pass). The captured failing traces
(`attempt-1/failing-trace.*.json`) differ from the oracle only for fiber `u`: the
oracle records `u LOADING -> ACTIVE`, then `plugin disposed u`, `u ACTIVE ->
UNLOADING`; the candidates record `plugin disposed u` and `u LOADING ->
UNLOADING` with no ACTIVE. Cause: `u`'s `waitMark u-ready` is a mark inside `u`'s
Apply, before Apply returns, so it orders nothing about `u`'s ACTIVE transition,
and `dispose u {await:false}` can be requested while `u` is still activating. The
same gap as `d`, one fiber over, hidden by the larger differences caused by `d`.

## Attempt 2 (insufficient, retained)

Draft 2 added `{"op": "awaitFiber", "fiber": "u"}` between `waitMark u-ready` and
`dispose u` (script SHA-256
`4c129cb487ea612d8712663e4d2af4b581e9462b2e6268df7bee8de3f88af20d`, with its
three oracle replays under `attempt-2/`). It matched 100 of 100 on both
candidates in the idle `-race` and `GOMAXPROCS=2` passes and in one loaded
`GOMAXPROCS=2` pass. In the loaded `-race` pass one of 100 `graph-lock` runs
failed with `causal comparison unsupported: unsettled lifecycle checkpoint
without phase witness`, and the **original** expectation test
`TestD13NeedsFiberScopedAwait` failed in both parallel-load copies (200 runs
each) with `D13 six/states/2/state: actual "LOADING" != expected "ACTIVE"`. A
further capture pass under load reproduced it on both candidates (`reconciler`
at runs 262 and 467, `graph-lock` at run 531 of 1500). In the captured traces
(`attempt-2/failing-trace.*.json`) the event multiset is identical to the
oracle's, but `a LOADING -> ACTIVE` is recorded at event 38, after the `six`
snapshot, where the oracle records it at event 27. Cause: fiber `a` is asserted
`ACTIVE` at `six`, but its only barrier is `waitMark active-apply`, a mark inside
its Apply, which fires before Apply returns; nothing waits for its activation
before the snapshot. This was named as a possible gap in the draft 2 note and was
found by traces, not assumed.

## Revision

Change only D13 fixture data, relative to revision 3: insert three external
steps,

1. `{"op": "awaitFiber", "fiber": "a"}` immediately after `waitMark
   active-apply`;
2. `{"op": "awaitFiber", "fiber": "d"}` immediately before `dispose d`;
3. `{"op": "awaitFiber", "fiber": "u"}` immediately before `dispose u
   {await:false}`, after `waitMark u-ready`.

Fiber `Await` settles when the fiber leaves its activation, so `a`, `d` and `u`
are `ACTIVE` in every runtime before the snapshot or before their disposal is
requested, and the disposals then run the ordinary ACTIVE teardown in both. None
of these fibers fails and none of the three Applies waits on a gate, so no
`catch` is needed and the awaits cannot be held open (`u`'s deliberately gated
cleanup runs only after its disposal, and `awaitFiber u` precedes it). All three
are mounted by root `mount` steps, whose ids the Go interpreter has already stored
when the mount returns; this is not the body-mounted child position of revision 4,
so no extra mark is needed to make them addressable. Both runtimes already support
the operation (oracle `target().await()`; Go interpreter `awaitFiber` over the
target-only `AwaitFiber` waiter), so no interpreter, harness or candidate change is
needed. The oracle trace changes (`a`, `d` and `u` ACTIVE ordering), as expected
for a script that is causally stricter; the Go candidates are compared against
the oracle execution of this same script.

The other fibers need no new barrier: `p` never leaves PENDING (it needs an
absent key), `l` is held LOADING by the `load` gate whose arrival is waited for,
`f` is awaited with `catch`, `u` reaches UNLOADING when its cleanup gate arrival
is waited for, and `d` is disposed with the default awaited `dispose`. After the
`six` snapshot every remaining step ends in `settle`.

Alternatives considered and rejected:

- The previously proposed `mark` in the `disposed` plugin's Apply plus a root
  `waitMark` before `awaitFiber d`: the mark fires inside Apply, before Apply
  returns, so it does not order the ACTIVE transition (exactly the weakness of
  `u-ready` and `active-apply` shown above), and once `awaitFiber d` is present it
  orders nothing further. It would add an event and a wait without adding
  ordering, so it is omitted.
- `settle`: global quiescence, held open by the deliberately gated `loading` and
  `unloading` fibers, and not an equivalent operation across runtimes.
- A candidate-only wait, sleep, retry or event sorting: forbidden masking.

All six state expectations stay unchanged and the other steps keep their order.
The exact same revised script is executed on the pinned oracle and on both Go
candidates.

## Interpreter identity

The oracle `oracle/replay.mjs` (SHA-256
`5f6714acf1e568d6358c08614321bfe520357dd0aee6a44c438faa25296306c4`) is the same
bytes bound by revisions 3 and 4. Revision-5 receipts bind these bytes.

## Acceptance boundary

The revision succeeds only if the revised script runs on the pinned oracle and
candidates with unchanged assertions and actual trace order. A mismatch remains a
mismatch. Recording an oracle result is not a Go comparison pass, and passing this
small script does not satisfy property/performance or final-kernel gates.
