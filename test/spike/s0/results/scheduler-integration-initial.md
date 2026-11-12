# Initial scheduler/replay integration failure

Command (repository root):

```sh
GOTOOLCHAIN=local GOMAXPROCS=8 go test -mod=readonly -race -count=1 \
  ./test/spike/s0/concurrency ./test/spike/s0/replay -timeout=90s
```

Managed execution `bash-4b6840ff83ee4f61` completed with process exit **1**. This was
an integration run while the narrow optional replay APIs were being completed,
not source-qualified performance or final-kernel acceptance. Its failures were
investigated rather than marked skipped or passed:

- Reconciler self-disposal timed out at 30 seconds; a repeated request re-enqueued
  an already-removing fiber indefinitely. The owner loop was corrected to make
  the request single-shot. A leaked owner then caused the idle-owner-count test
  to fail and a virtual-time test to report blocked goroutines. These are not
  environmental invalidations.
- Reconciler replay checkpoints reported unknown disposed fibers before the
  value-only retired snapshot history and targeted Await integration completed.
  The adapter did not fabricate DISPOSED rows from the script's expectations.
- The raw JSON fixture recorder roundtrip used float64 instead of json.Number;
  visually equal numeric payloads failed a typed immutability assertion. Both
  implementations now preserve JSON numbers and test large-integer precision.
- D13 could wait forever for an unload gate if an empty Apply had not registered
  cleanup before the script disposed it. This is separately preserved and
  corrected with an explicit shared fixture barrier under
  [corpus revision 2](../../../../docs/design/s0-corpus-revision-2.md), not a hidden
  candidate-only wait or a synthesized cleanup observation.
- The package integration ultimately hit its 90-second timeout; the process was
  not an acceptance pass. Focused candidate fixes/race tests were run afterward,
  but they do not retroactively change this failure or establish full parity.

The original tool output was collected in-session. This file is an accurate
failure summary, not a replacement raw trace or a claim of a reproducible hash for
that in-progress source snapshot. Subsequent qualified runs must record their
own complete source and executable identities.
