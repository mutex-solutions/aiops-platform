<!-- SPDX-License-Identifier: Apache-2.0 -->
# D13 fixture revision 3 - pre-execution record

Written before any revision-3 replay (oracle or candidate), implementing
`docs/design/s0-corpus-revision-3.md`. Scope is only the D13 failure-barrier
repair; no contract adaptation or acceptance relaxation.

Revision 2 remains immutable (script SHA-256
`b285c1deb72ccccefe14184cadfe8269684580e70c4c6ba8a0ecf39f3bd161c2`, preserved
byte-for-byte in `D13-v2.script.json`; freeze v2 and its tests untouched).

Problem: nothing orders fiber `f` (Apply fails) before the `six` snapshot, so
graph-lock can snapshot `f` as LOADING instead of FAILED (about 3% of runs).

Delta, exactly one external step inserted between `waitMark unload` and
`snapshot six`: `awaitFiber f` with `catch: true`. Fiber Await rejects with the
startup failure of a FAILED fiber; `catch` records it as an `error` event. All
six state expectations, IDs, action order and cleanup bodies remain. No sleep,
retry, hidden candidate-only wait, event sorting or expected-output injection.

Interpreter identity: current `oracle/replay.mjs` SHA-256
`5f6714acf1e568d6358c08614321bfe520357dd0aee6a44c438faa25296306c4` (revision 2
used `b0f4e447...`; the only difference is opt-in witness instrumentation).

Replay the revised case at least three times on exact isolated Node 24.22.1,
retain each trace and receipt here, and add a revision-3 overlay freeze that binds
the v2 base. Reuse the unchanged other 35 entries without claiming they were
rerun. Any mismatch between oracle and candidates will be reported as a mismatch.
