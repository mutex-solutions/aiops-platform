# S0 Effect shape: proposed per-assertion mapping for checklist row 14

Status: **proposed; not acceptance and no API implementation change**.
The accepted Go API is `Effect(label, setup func() (Disposer,error)) error`;
checklist row 14 explicitly adapts Promise/iterable/disposer result shapes to this
form and strict sequential LIFO. Row 15 retains inactive refusal and effect-tree
diagnostics. The frozen D23 script uses a JavaScript generator and a returned
withdrawable disposer; that exact author API is not currently present in Go.

## Invariants to keep

- Every resource transferred to a fiber has actual native cleanup ownership.
- A failed setup which returns its acquired cleanup runs it immediately, in
  reverse acquisition order, once; no owned ledger entry is retained as success.
- A setup that panics does not manufacture a returned cleanup. Previously
  registered native effects still unwind; caller-owned unreturned resources are
  not claimed magically recovered. This limitation must not be hidden.
- A disposer exposed by a registration API is single-shot under concurrent/manual
  removal and native teardown, and its real ledger ownership is removed before
  diagnostics claim an empty owned set. Existing event registration handles do
  not by themselves prove general Effect withdrawal.
- New effects on quiescing/disposed contexts refuse before setup callbacks run.

## Narrow mapping proposed for owner review

D23's generator-specific syntax would be assessed under row 14's adapted Go
shape, **not** reported as raw-script parity. The equivalent Go fixture would
perform real resource acquisitions in setup and return their actual composite
cleanup alongside the setup error. `Context.Effect` would invoke that cleanup;
strict order/count and empty-native-ledger observations would be checked. A
composite returned by setup is caller code, not a hidden kernel collector or a
fake runtime. Its coverage does not imply support for arbitrary captured `Defer`
rollback, panics before cleanup is returned, generator yields or nested metadata.

D23's source return-value withdrawal is a separate assertion: either approve an
explicit native ownership-handle/collector design and test it fully, or explicitly
ratify that this API form is excluded under row 14 and require single-shot proofs
for every registration API that actually exposes a disposer. Do not silently
mark it excluded or add a private adapter ledger to report it passed.

Until that mapping is ratified or a native ownership-handle design is approved,
D23 remains unsupported and the full partial-effect/metadata gate remains open.
No frozen criteria/script/assertion is edited and no whole scenario is waived.
