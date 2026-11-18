<!-- SPDX-License-Identifier: Apache-2.0 -->
# D13 fixture revision 5 - pre-execution record

Written before the three recorded revision-5 oracle replays, implementing
`docs/design/s0-corpus-revision-5.md`. Scope is only the D13 activation barrier
for fiber `d`; no contract adaptation or acceptance relaxation. Disclosure: the
same script was earlier run as an unrecorded draft by the live causal rate
harness; it was not changed afterwards and no other draft exists.

Revision 3 remains immutable (script SHA-256
`07438cacef25b6cdf7b9f0d825645e41fa07ecd5bd64d29c9073a0d34c2d6381`, preserved
byte-for-byte in `D13-v3.script.json`; freeze v3 and its tests untouched).

Problem: nothing orders fiber `d` ACTIVE before its disposal, so the oracle
(run to completion) shows `d` disposed while LOADING and a concurrent candidate
may show it ACTIVE first or not yet applied; the causal comparator then reports
different record counts (37 against 35..38).

Delta, exactly one external step inserted between `waitMark active-apply` and
`dispose d`: `awaitFiber d`. Fiber Await settles when `d` leaves its activation
(it never fails and never waits on a gate), so `d` is ACTIVE in both runtimes
before the disposal. All six state expectations, IDs, action order and cleanup
bodies remain. No sleep, retry, hidden candidate-only wait, event sorting or
expected-output injection.

Interpreter identity: `oracle/replay.mjs` SHA-256
`5f6714acf1e568d6358c08614321bfe520357dd0aee6a44c438faa25296306c4` (as
revisions 3 and 4).

Replay the final case at least three times on exact isolated Node 24.22.1,
retain each trace and receipt here, and add a revision-5 overlay freeze that binds
the v4 base. Any mismatch between oracle and candidates will be reported as such.
