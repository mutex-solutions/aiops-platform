<!-- SPDX-License-Identifier: Apache-2.0 -->
# D13 fixture revision 5 - pre-execution record

Written before any replay (oracle or candidate) of the final script, implementing
`docs/design/s0-corpus-revision-5.md`. Scope is only the D13 activation barriers
for fibers `a`, `d` and `u`; no contract adaptation or acceptance relaxation.

Revision 3 remains immutable (script SHA-256
`07438cacef25b6cdf7b9f0d825645e41fa07ecd5bd64d29c9073a0d34c2d6381`, preserved
byte-for-byte in `D13-v3.script.json`; freeze v3 and its tests untouched).

Problem: nothing orders fibers `a`, `d` and `u` ACTIVE before the snapshot or
before their disposal, so the oracle (run to completion) and a concurrent
candidate record different event sets or a different activation position, and
the causal comparator reports different record counts or an unsettled checkpoint.

History: attempt 1 (only `awaitFiber d`) failed under load because `u` was disposed
LOADING; attempt 2 (`awaitFiber d` and `awaitFiber u`) failed under load because
`a`, asserted ACTIVE at `six`, was still LOADING at the snapshot (an original
expectation failure). Each is preserved with its own note, pre-execution record,
three oracle replays and captured failing Go traces under `attempt-1/` and
`attempt-2/`. The final script below was written after that, before any replay of
it; it was not yet executed anywhere when this record was written.

Delta from revision 3, no change to expectations: external `awaitFiber a`
immediately after `waitMark active-apply`; external `awaitFiber d` between that
and `dispose d`; external `awaitFiber u` between `waitMark u-ready` and `dispose u`.
None of the three fibers fails or waits on a gate, so no await can be held open.
No sleep, retry, hidden candidate-only wait, event sorting or expected-output
injection.

Interpreter identity: `oracle/replay.mjs` SHA-256
`5f6714acf1e568d6358c08614321bfe520357dd0aee6a44c438faa25296306c4` (as
revisions 3 and 4).

Replay the final case at least three times on exact isolated Node 24.22.1,
retain each trace and receipt here, and add a revision-5 overlay freeze that binds
the v4 base. Any mismatch between oracle and candidates will be reported as such.
