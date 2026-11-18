# S0 corpus revision 5: explicit D13 activation barrier for fiber `d`

Status: the final script was planned and recorded **before its three recorded
oracle replays**; this note and its
[pre-execution record](../../test/differential/results/corpus-revision-5/preexecution.json)
were written first. Disclosure, stated plainly: before this note existed the same
one-step script had already been exercised as a draft by the live causal rate
harness (`TestCausalRates`, which runs a fresh pinned-oracle execution per
scenario and then the Go candidates) in three 100-run passes per candidate. Those
executions were exploratory, are not retained as receipts, and were not followed
by any script change: this is the only D13 draft, there is no superseded attempt.
This is a fixture causality correction, not a semantic adaptation, threshold
relaxation or S0 pass. The [original criteria](s0-spike-criteria.md), the
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

## Revision

Change only D13 fixture data, relative to revision 3: insert one external step
immediately before `dispose d`, after `waitMark active-apply`:

    {"op": "awaitFiber", "fiber": "d"}

Fiber `Await` settles when the fiber leaves its activation, so `d` is `ACTIVE` in
every runtime before its disposal is requested, and the disposal then runs the
ordinary ACTIVE teardown (`plugin disposed`, `ACTIVE -> UNLOADING`,
`UNLOADING -> DISPOSED`) in both. `d` never fails and its Apply never waits on a
gate, so no `catch` is needed and the await cannot be held open. `d` is mounted by
a root `mount`, whose id the Go interpreter has already stored when the mount
returns; this is not the body-mounted child position of revision 4, so no extra
mark is needed to make `d` addressable. Both runtimes already support the
operation (oracle `target().await()`; Go interpreter `awaitFiber` over the
target-only `AwaitFiber` waiter), so no interpreter, harness or candidate change
is needed. The oracle trace changes (`d` now shows `ACTIVE`), as expected for a
script that is causally stricter; the Go candidates are compared against the
oracle execution of this same script.

Alternatives considered and rejected:

- The previously proposed `mark` in the `disposed` plugin's Apply plus a root
  `waitMark` before `awaitFiber d`: the mark fires inside Apply, before Apply
  returns, so it does not order the ACTIVE transition, and once `awaitFiber d` is
  present it orders nothing further. It would add an event and a wait without
  adding ordering, so it is omitted rather than kept as decoration.
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
