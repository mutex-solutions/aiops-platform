# S0 corpus revision 4: explicit D22 child activation barrier

Status: the final script (draft 3) was planned and recorded **before its first
oracle replay**; this note and its
[pre-execution record](../../test/differential/results/corpus-revision-4/preexecution.json)
were written first. Disclosure: two earlier drafts were replayed on the oracle
and on the Go candidates before this design (attempts 1 and 2, below, each kept
with its oracle traces), and draft 3 was run on the Go candidates (never the
oracle) only after the pre-execution record was regenerated. This is a fixture
causality correction, not a semantic adaptation, threshold relaxation or S0
pass. The [original criteria](s0-spike-criteria.md), the
[revision-2](s0-corpus-revision-2.md) and [revision-3](s0-corpus-revision-3.md)
notes, all earlier freezes and all traces and receipts remain unchanged
historical evidence.

## Observation

D22 mounts `parent`, whose Apply mounts `child`. The child's Apply registers a
late effect (`await: false`) whose setup and cleanup are gated. The script waits
for the `effect-setup` mark, then requests disposal of `parent` and later
snapshots `joining-cleanup`, expecting both fibers `UNLOADING`. The
`effect-setup` mark proves only that the child's Apply body has started; nothing
waits for the child fiber to leave LOADING. The pinned JavaScript microtask order
finishes the child's activation first. A concurrent Go candidate can have the
child still `LOADING` when disposal is requested or snapshotted, and the
snapshot legitimately reads `LOADING`
(`D22 joining-cleanup/states/1/state: actual "LOADING" != expected "UNLOADING"`).
Reproduced before this revision with
`go test -race -count=200 -run 'TestEffectsCheckpointExecution/D22'`, also with
`GOMAXPROCS=2`. This is a fixture causality gap, not a candidate defect. The
original D22 is retained as the record of that race.

## Attempt 1 (rejected, retained)

Draft 1 inserted only `{"op": "awaitFiber", "fiber": "child"}` before
`dispose parent` (script SHA-256
`8c0226ecdaf0fc661a63c9f849cd0c7bc1eb6ca3ce59d924f210d2fff6f1dc3f`). The oracle
accepted it (three identical replays, retained under
`results/corpus-revision-4/attempt-1/`). The Go candidates then failed with
`step 3 awaitFiber fiber="child": unknown fiber` and one race-detector report. Cause: the
Go interpreter records a fiber's native id (`fiber.id`) only after `Mount` returns,
and reads it unlocked in `await`. For a child mounted from the parent's body, the
external script can reach `awaitFiber` (after the child's `effect-setup` mark)
before the parent's `child` operation has returned and stored the id. This is an
interpreter-bookkeeping ordering gap in that position, not a kernel defect, and it
is closed by script causality rather than by emulation: the target must be known
to be mounted first.

## Attempt 2 (insufficient, retained)

Draft 2 added `mark child-mounted` in `parent.apply` after the `child` op, and
external `waitMark child-mounted` plus `awaitFiber child` before `dispose parent`
(script SHA-256 `1d5aad5737a5f56eac55d047ebbd7ab289c667a5c9d02a758d643305a8dcd16c`,
with its three oracle replays under `results/corpus-revision-4/attempt-2/`). It
passed 400 + 300 sequential `-race` runs but failed about 1 in 300 on both
candidates when the host was loaded: `D22 joining-cleanup/states/0/state: actual
"LOADING" != expected "UNLOADING"`, where state 0 is the PARENT. Captured failing
traces (`attempt-2/failing-trace.*.json`) show the event order: child
`LOADING -> ACTIVE`, then `plugin disposed` for parent and child, child
`ACTIVE -> UNLOADING`, its cleanup, child `DISPOSED`, and only then parent
`LOADING -> UNLOADING`; both checkpoints read the parent `LOADING`. Cause: the
`child-mounted` mark runs inside the parent's Apply and `awaitFiber child` orders
only the child's activation. Nothing ordered the parent's own `LOADING -> ACTIVE`
before `dispose parent` (`await: false`), so disposal could be requested while the
parent was still activating; the disposal then ran child-first, and the
`joining-cleanup` snapshot saw the parent still LOADING. Same class of fixture
causality gap, one fiber further up.

## Revision

Change only D22 fixture data. Relative to the original:

1. In `parent.apply`, after the `child` operation, add
   `{"op": "mark", "label": "child-mounted"}`. The mark is real parent-body
   progress: it runs exactly when the parent's `child` operation has returned,
   i.e. the child fiber is mounted and its id recorded.
2. Between `waitMark effect-setup` and `dispose parent`, add external steps
   `{"op": "waitMark", "label": "child-mounted"}`,
   `{"op": "awaitFiber", "fiber": "child"}` then
   `{"op": "awaitFiber", "fiber": "parent"}`.

`awaitFiber parent` resolves when the parent's activation completes, which now
cannot precede the child's ACTIVE and does not wait on any gate (the parent's Apply
returns after mounting the child). Parent ACTIVE is therefore observed before
disposal is requested.

Fiber Await settles when the fiber leaves its activation; the effect is
`await: false`, so the child's Apply does not wait on the held gate, and Await
resolves when the child is ACTIVE. No `catch` is needed since the child does not
fail. Both runtimes already support every operation used: the oracle
`target().await()` and `mark`/`waitMark`, and the Go interpreter's `awaitFiber`
over the target-only `AwaitFiber` waiter. No interpreter, harness or candidate
change is needed. The extra `mark` is an additional trace event present in every
runtime, not filtered.

Alternatives considered and rejected:

- `awaitFiber child` alone (attempt 1) and without the parent await (attempt 2):
  rejected by the failures above.
- `mark` in the child's Apply plus external `waitMark` instead of `awaitFiber`:
  a mark inside Apply fires before Apply returns, so it does not order the ACTIVE
  transition (the existing `effect-setup` gate mark has this weakness).
- `settle`: global quiescence, held open by the deliberately gated effect, and not
  an equivalent operation across runtimes.
- A candidate-only wait, sleep, retry or event sorting: forbidden masking.
- `awaitFiber parent` alone: orders the parent but not the child's activation,
  and the child fiber is not addressable until it is mounted; hence both awaits.

All original expectations stay unchanged and the other steps keep their order.
The exact same revised script is executed on the pinned oracle and on both Go
candidates.

## Interpreter identity

The oracle `oracle/replay.mjs` (SHA-256
`5f6714acf1e568d6358c08614321bfe520357dd0aee6a44c438faa25296306c4`) is the same
bytes bound by revision 3. Revision-4 receipts bind these bytes.

## Acceptance boundary

The revision succeeds only if the revised script runs on the pinned oracle and
candidates with unchanged assertions and actual trace order. A mismatch remains a
mismatch. Recording an oracle result is not a Go comparison pass, and passing this
small script does not satisfy property/performance or final-kernel gates.
