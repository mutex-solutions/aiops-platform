# S0 corpus revision 2: explicit D13 activation barrier

Status: planned and recorded **before revision-2 replay**. This is a fixture
causality correction, not a semantic adaptation, threshold relaxation or S0 pass.
The [original criteria](s0-spike-criteria.md) bytes/hash remain unchanged.
The first [corpus freeze](../../test/differential/results/corpus-freeze.json) and
all corresponding traces, receipts and failed controls remain historical evidence.

## Observation

D13 intends to observe all six states behind causal barriers. It mounts a fiber
whose Apply is empty and whose cleanup waits at `unload`, then disposes it after
awaiting disposal of an unrelated empty fiber. Under the pinned JavaScript
microtask scheduler, the first Apply has returned before that later action.
The Go candidate can legitimately schedule the unrelated callback first and
cancel the first activation before its body registered cleanup. The script then
waits for an unload-gate arrival that never occurred, and the test times out.

The integration run was failed, not counted as equality. No fake cleanup mark,
hidden candidate-only Await, event sorting, status deletion or expected-output
injection is used to mask it. Go run-to-completion is already a not-carried
execution detail; its intended observable lifecycle semantics still need the
same explicit causal operation sequence in both runtimes.

## Revision

Change only D13 fixture data:

1. Its `unloading` plugin Apply explicitly emits `u-ready` after the fixture's
   cleanup registration is owned.
2. The external steps explicitly wait for `u-ready` before requesting that fiber's
   disposal.
3. Keep all original six-state expectations and all state/service events. Execute
   exactly the same revised script on the pinned oracle and both Go candidates.

Preserve the revision-1 D13 bytes before editing; the old corpus freeze continues
to refer to them. Record new script/hash/receipts in a revision-2 result directory
and a new freeze manifest. Other scripts and the interpreter do not change for
this correction. Any further fixture correction requires its own description,
preserved failing record and revised evidence identity before rerunning.

## Acceptance boundary

The revision succeeds only if the revised script runs on the pinned oracle and
candidates with unchanged six-state assertions and actual trace order. A future
mismatch remains a mismatch. Recording an oracle result is not a Go comparison
pass, and passing these small scripts does not satisfy property/performance or
final-kernel gates.
