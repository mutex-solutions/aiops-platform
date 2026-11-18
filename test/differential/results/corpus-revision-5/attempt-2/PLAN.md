<!-- SPDX-License-Identifier: Apache-2.0 -->
# D13 fixture revision 5 - pre-execution record

Written before any replay (oracle or candidate) of the final script, implementing
`docs/design/s0-corpus-revision-5.md`. Scope is only the D13 activation barriers
for fibers `d` and `u`; no contract adaptation or acceptance relaxation.

Revision 3 remains immutable (script SHA-256
`07438cacef25b6cdf7b9f0d825645e41fa07ecd5bd64d29c9073a0d34c2d6381`, preserved
byte-for-byte in `D13-v3.script.json`; freeze v3 and its tests untouched).

Problem: nothing orders fibers `d` and `u` ACTIVE before their disposal, so the
oracle (run to completion) and a concurrent candidate record different event
sets, and the causal comparator reports different record counts.

History: attempt 1 (only `awaitFiber d`, preserved with its own note,
pre-execution record, three oracle replays and two captured failing Go traces
under `attempt-1/`) matched 100 of 100 idle but failed about 1 in 100 to 1 in 400
under host load on both candidates: fiber `u`, whose only barrier is a mark inside
its Apply, was disposed while LOADING. The final script below was written after
that, before any replay of it; it was not yet executed anywhere when this record
was written.

Delta from revision 3, no change to expectations: external `awaitFiber d`
between `waitMark active-apply` and `dispose d`; external `awaitFiber u`
between `waitMark u-ready` and `dispose u`. Neither fiber fails or waits on a
gate, so neither await can be held open. No sleep, retry, hidden candidate-only
wait, event sorting or expected-output injection.

Interpreter identity: `oracle/replay.mjs` SHA-256
`5f6714acf1e568d6358c08614321bfe520357dd0aee6a44c438faa25296306c4` (as
revisions 3 and 4).

Replay the final case at least three times on exact isolated Node 24.22.1,
retain each trace and receipt here, and add a revision-5 overlay freeze that binds
the v4 base. Any mismatch between oracle and candidates will be reported as such.
