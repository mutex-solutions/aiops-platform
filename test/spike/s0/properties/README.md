# S0 real-runtime lifecycle properties

This is **test-only evidence for the two experimental concurrency candidates**,
not the production SDK, final kernel or a fake host. It imports the standard
library and the existing `concurrency` package only. It changes no criteria,
canonical decisions, dependencies, runtime ports or scheduler implementation.

## Commands and evidence classification

From the module root, a short check is:

```sh
GOMAXPROCS=8 go test -race -count=1 -timeout=3m -v ./test/spike/s0/properties
GOMAXPROCS=8 go vet ./test/spike/s0/properties
```

The default is **16 seeds × 128 operations per candidate**, not the frozen gate.
`TestFullPropertyGate` reports an explicit **SKIP / NOT RUN** in that mode; passing
smoke or the model unit tests never establishes S0 property acceptance.

The parent owns the full run, after all shared-source writers have stopped.
Obtain the exact source digest without executing a runtime first:

```sh
GOMAXPROCS=8 go test -race -count=1 -run '^TestPropertyIdentity$' -v ./test/spike/s0/properties
```

Then run the corpus once using `sourceSHA256` from that identity:

```sh
GOMAXPROCS=8 \
S0_PROPERTY_FULL=1 \
S0_PROPERTY_SOURCE_SHA256='<exact frozen source digest printed by the test>' \
S0_PROPERTY_RESULTS='test/spike/s0/properties/evidence' \
go test -race -count=1 -timeout=30m -v ./test/spike/s0/properties
```

`TestRuntimeProperties` owns the full corpus: **1,000 seeds × 1,000 operations
for EACH of `NewReconciler` and `NewGraphLock`**, with a fresh actual backend per
seed. Selecting `TestFullPropertyGate` alone does not execute the corpus and is
always marked skipped. No count/operation override can shrink the full workload.
The full mode refuses to start without the parent's exact source freeze digest
and `GOMAXPROCS=8`. The command must include `-race`; this fixture reports no
optimized-performance result. A race report, unexpected error, panic, deadline,
model mismatch, retained effect or owned goroutine is a failure.

Every run verifies the criteria bytes against the existing freeze receipt,
records criteria hash, current baseline commit, a source hash manifest, test
executable hash, Go version and GOMAXPROCS. The source manifest covers Go files
in the actual local `concurrency`, `internal/eventqueue` and `properties` package
closure, module files, frozen criteria and its receipt,
including uncommitted source. Unrelated S0 probe packages are not in this executable
and do not invalidate its source fence.
Its aggregate is SHA-256 over sorted `relative-path NUL hex-file-digest LF` records.
A changed source digest after execution invalidates the evidence; do not promote
such a smoke run or continue a full run while another agent edits shared source.
The compiled test executable hash separately identifies the actual build. The
parent must preserve the exact command, exit status and race output alongside
JSON evidence. This fixture is not a complete §2 run-record/report generator.

No full run has been performed by this package's authoring task. The parent will
run the frozen objective in one managed background job, avoiding duplicate work.

## Deterministic workload and independent model

* Seeds are the frozen `0x53000000 + seedIndex`, for index 0…999. PRNG is explicitly
  SplitMix64 with fixed uint64 arithmetic; known vectors test its implementation.
* Eight indexed provider slots and eight indexed consumer slots form a small DAG.
  Needs refer to provider indexes. Providers can need only lower-index providers;
  a separate generated missing address is never provided. Slots 0–4 give a chain
  and diamond, while other declarations choose generated edges/missing Needs.
* Each stream begins with consumers-before-providers, a provider body failure,
  dependency identity change while that failure is sticky, explicit update
  recovery, disposal and remount. Remaining choices select legal mount, dispose,
  update, provider-value update and failure-input toggle+update operations.
  Every generated command changes or targets one real mounted incarnation.
* Mounting creates a real spec with `Apply`, no configuration and one `Defer`-owned
  counter effect. Providers publish an immutable fixture value **before** an
  intentional failure, exercising partial setup unwind and strict availability.
  Failure input changes only after the prior operation has settled. An atomic
  pointer publishes one immutable generation that Apply loads once; a callback
  never rereads a later mutable failure flag. All instrumentation is run-owned;
  there are no package-global mutable fixtures or error objects.
* The model evaluates commands and its own abstract activation identities in DAG
  order. It never reads a runtime, snapshot, callback result or numeric UID to
  compute expected stable state, revision, application/disposal count or sticky
  error generation. A failure remains FAILED across dependency loss/arrival and
  recovers only after explicit Update. A missing dependency never executes Apply.

For every operation the harness calls the actual runtime and then its native
Settle barrier. It compares native Inspect state/Alive/revision, callback counts,
owned effects and immutable inputs against that independent model. Native
AwaitFiber must preserve the exact intentional error object for each failed or
failed-then-disposed incarnation. GraphLock Settle is a join and can return nil
on FAILED; Reconciler can return a wrapped aggregate. Only exact known error
leaves corresponding to **currently FAILED, independently expected** fibers are
accepted; aggregate mixtures with unknown leaves fail. Snapshot diagnostic text
may repeat the same known startup/await diagnostic but may contain nothing else.

Provision UIDs are **observed actual identities**, never predicted numeric
allocations. Inspection must have no duplicate address/UID, no inactive stable
reservation, and each consumer's target and committed epoch must equal actual
active provider UIDs in declared-Need order. Reads must preserve the exact real
provided value identity. A UID cannot later designate another value identity, and
one actual value cannot change UID. Cleanup checks the bound provider's owned
proof is still live, so release cannot overtake dependent disposal.

Atomic per-fiber guards enter both Apply and the Defer disposer, recording peak
concurrent callback writers. Every settled peak is at most one, no callback is
running, owned counts match ACTIVE fibers, old contexts reject new activity and
owned proofs of inactive activations are dead. This checks observable callback
single-writer/overlap behavior, **not an unsupported claim to instrument private
scheduler fields or every G1–G5 assertion**.

Final reverse-topological disposal checks all native archived snapshots are
DISPOSED/not Alive, with no target/effects/provisions and owned counters zero.
Inspection happens before Close because Reconciler Close terminates its owner;
this is native API behavior, not a fabricated post-Close snapshot. Close is then
joined. Exact goroutine IDs/stacks are compared against the combined pre-runtime
baseline after each seed and final teardown: only new actual fixture/core owner
or transition stacks count, and unrelated Go runtime/testing workers cannot
numerically cancel a leak. Scheduler yields allow only Close's finite return tail,
with a five-second failure bound and no sleep, broad count slack or GC-worker
false positives.

## Reproducing a failure

A failure prints the seed, first failing operation, exact generated command
prefix and JSON path. Default JSON location is `properties/evidence` (outside the
ignored `s0/results` tree); `S0_PROPERTY_RESULTS` can select another location. JSON
preserves the full first-failing prefix, independent model, actual inspection,
actual unsorted event ledger and source/build identities. This is prefix reduction,
**not** a claim of a globally minimal counterexample; preserve it and add a focused
regression when diagnosing a real core finding.

```sh
GOMAXPROCS=8 \
S0_PROPERTY_REPLAY='test/spike/s0/properties/evidence/<failure>.json' \
go test -race -count=1 -timeout=3m -run '^TestRuntimeProperties$' -v \
  ./test/spike/s0/properties
```

Replay drives the stored commands against the recorded real candidate, not a
fake runtime. It cannot count as full acceptance. Pass output remains compact;
setting `S0_PROPERTY_RESULTS` also writes a compact passing summary/manifest.
Streams remain reproducible from the pinned PRNG, generator bytes, seed and exact
operation count. Evidence remains candidate evidence, not final-port acceptance.
