<!-- SPDX-License-Identifier: Apache-2.0 -->
# D22 fixture revision 4 - pre-execution record

Written before any revision-4 replay (oracle or candidate), implementing
`docs/design/s0-corpus-revision-4.md`. Scope is only the D22 child activation
barrier; no contract adaptation or acceptance relaxation.

The original D22 stays immutable (script SHA-256
`fda677fb41026d454b7d225b30cc64170bb6938d310c173ad77ffef9c66e7965`, preserved
byte-for-byte in `D22-v1.script.json`).

Problem: nothing orders the child fiber ACTIVE before the parent's disposal, so a
concurrent candidate can snapshot the child LOADING instead of UNLOADING.

History: attempt 1 (only `awaitFiber child`, preserved with its three oracle
replays under `attempt-1/`) hit an interpreter id-bookkeeping gap on the Go side
(`unknown fiber`: the id is stored after `Mount` returns). Attempt 2 (mark + waitMark +
awaitFiber child, preserved with its oracle replays and two captured failing Go
traces under `attempt-2/`) still failed about 1 in 300 under load because the
parent was snapshotted LOADING (nothing ordered parent ACTIVE before dispose). The
final script below was written after that, before any oracle replay of it; it was
not yet executed anywhere when this record was written.

Delta from the original, no change to expectations: `mark child-mounted` after
the `child` op in `parent.apply`; external `waitMark child-mounted`,
`awaitFiber child` and `awaitFiber parent` inserted, in that order, between
`waitMark effect-setup` and `dispose parent`. No sleep, retry, hidden
candidate-only wait, event sorting or expected-output injection.

Interpreter identity: `oracle/replay.mjs` SHA-256
`5f6714acf1e568d6358c08614321bfe520357dd0aee6a44c438faa25296306c4` (as revision 3).

Replay the final case at least three times on exact isolated Node 24.22.1,
retain each trace and receipt here, and add a revision-4 overlay freeze that binds
the v3 base. Any mismatch between oracle and candidates will be reported as such.
