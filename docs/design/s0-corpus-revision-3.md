# S0 corpus revision 3: explicit D13 failure barrier

Status: planned and recorded **before any revision-3 replay**. No revised D13
script has been executed on the oracle or on either Go candidate at the time this
note and its [pre-execution record](../../test/differential/results/corpus-revision-3/preexecution.json)
were written. This is a fixture causality correction, not a semantic adaptation,
threshold relaxation or S0 pass. The [original criteria](s0-spike-criteria.md),
the [revision-2 note](s0-corpus-revision-2.md), both earlier freezes and all
traces, receipts and failed controls remain unchanged historical evidence.

## Observation

Revision 2 added a barrier for fiber `u` but left fiber `f` (plugin `failed`,
whose Apply throws) without one. The script mounts `f`, then waits only for the
`active-apply`, `u-ready` and `unload` marks before the `six` snapshot, and
expects `/states/3/state` to be `FAILED`. Nothing makes the script wait for `f`'s
Apply to run and fail. The pinned JavaScript microtask order happens to finish
`f` first. The `graph-lock` candidate runs Apply on concurrent workers, so `f`'s
Apply can be scheduled after the `unload` gate arrival, and the snapshot then
legitimately sees `LOADING`. Observed in roughly 3% of `graph-lock` runs
(`D13 six/states/3/state: actual "LOADING" != expected "FAILED"`). This is a
fixture causality gap, not a candidate defect. The revision-2 D13 is a racy
fixture; it is retained, not deleted, as the record of that race.

## Revision

Change only D13 fixture data, relative to revision 2: add one external step
immediately before the `six` snapshot (after `waitMark unload`):

    {"op": "awaitFiber", "fiber": "f", "catch": true}

Fiber `Await` settles when the fiber leaves its activation. For a fiber whose
Apply failed it rejects with the retained startup failure. `catch: true` is the
existing script-level handling (used by D05, D06, D23, D24, D29, D32, D33, D36):
the rejection is recorded as an `error` trace event and the script continues. In
the oracle this is `target().await()` with the catch wrapper. In the Go
interpreter it is the existing `awaitFiber` operation over the target-only
`AwaitFiber` waiter plus the same `Catch` handling. Both runtimes already support
it, so no interpreter, harness or candidate change is needed.

Alternatives considered and rejected before running:

- `mark` inside `failed.apply` before `fail`, plus `waitMark`: the mark fires
  before the Apply throws, so it does not order the FAILED transition.
- `settle`: a global quiescence wait, held open by the deliberately gated
  `loading` and `unloading` fibers, and not an equivalent operation across
  runtimes.
- A candidate-only wait, sleep, retry or event sorting: forbidden masking.
- `awaitFiber` without `catch`: Await rejects for a failed fiber, so the script
  would abort.

All six state expectations stay unchanged. The added step introduces one `error`
event that is part of the actual trace in every runtime; it is not filtered. The
exact same revised script is executed on the pinned oracle and on both Go
candidates.

## Interpreter identity

The current `oracle/replay.mjs` (SHA-256
`5f6714acf1e568d6358c08614321bfe520357dd0aee6a44c438faa25296306c4`) differs from the
revision-2 interpreter (`b0f4e447...`, preserved under `oracle/history/`) only by
opt-in causal-witness instrumentation, enabled solely by `S0_CAUSAL_WITNESS_PATH`
(off for these replays, adds no trace data). Revision-3 receipts bind the current
bytes. The revision-2 tests keep binding the historical bytes.

## Acceptance boundary

The revision succeeds only if the revised script runs on the pinned oracle and
candidates with unchanged six-state assertions and actual trace order. A mismatch
remains a mismatch. Recording an oracle result is not a Go comparison pass, and
passing this small script does not satisfy property/performance or final-kernel
gates.
