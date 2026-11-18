# S0 scale harness — performance withheld

**Status: author helpers tested; all 10,000-fiber and full tree performance runs
are NOT RUN.** An ordinary package test reports `TestDefaultMeasure` as **SKIP**,
not scale acceptance. Do not enable it while the parent's full property/race
corpus is running. The parent owns final semantic parity, source/hash freeze,
measurement-host evidence, execution and candidate selection.

This is a test-only experiment, not a final SDK, production port, new policy,
transaction/HTTP workload or new S0 acceptance criterion. It imports only the Go
standard library and the existing S0 concurrency/composition/contextmodel code.
No Docker, SQL, native dependency, tool script, CI change or module edit is needed.

Authority is the unchanged frozen
[`s0-spike-criteria.md`](../../../../docs/design/s0-spike-criteria.md) §§4, 8 and 9.
The parent must verify its exact bytes against the freeze receipt. No threshold
is tuned here. The snapshot receipt identifies SHA-256
`628bd6ec24e5983bfbe9fcdada096eaa84db3e95227f11e8fd5a7fa33a26f19c`;
that text is not a fresh verification or parent source freeze.

## Exact deterministic fixture data

`FrozenFixtures()` constructs data only. Generating 10,000 nodes is not mounting
10,000 fibers and is not a performance observation.

| Fixture | Exact measured fiber count, excluding backend root |
| --- | --- |
| independent | 10,000 zero-Need fibers; node 0 is its separately declared provider, **within** the total; last node is updated |
| chain | 10,000; node 0 provides; each later node needs predecessor and provides its own distinct key |
| fanout | one provider + 9,999 consumers |
| diamonds | one root + 3,333 complete groups, each two siblings + one two-Need join = 10,000 |
| realms | 100 actual opaque labels, each one provider + 99 consumers, equal key name |
| trees | 97 shared fibers (one provider + 96 consumers) plus 500×2, 500×4, 300×2, or 300×4 local fibers |

`Graph`, `RealmFixture` and `TreeFixture` permit explicit small helper fixtures;
`Validate` rejects duplicate provider keys, undeclared/missing providers, duplicate
Needs, and forward/cyclic edges. Node/key types are shallow. Generation and
validation are iterative, not recursive, including the 10k chain. Runtime stack
growth/recursion safety at 10k is **unmeasured**, not inferred from a small smoke.

Every actual native `Spec` declares all body/cleanup reads. Each body uses real
`Config`, `Read`/typed `Resolve`, `Provide` of its immutable integer node index,
and one native `Defer`-owned resource. Cleanup reads dependencies again, so
provider retention is tested rather than assumed. There is no double provision
of the root key. No body starts a persistent goroutine or performs network I/O.

Graphs use native string-keyed specs; realm/tree fixtures use the existing
composition frontend with **real contextmodel opaque labels**, not a substitute
realm implementation. Their metadata-only parents add no hidden container
fibers. Trees use immutable provider-index/config values plus opaque realm labels
to distinguish scope and validate actual Resolve/cleanup isolation. A separate
`composition.Scope` value/write is not exercised. All bodies acquire and release
the actual activation lease, using native `Enter`/`Leave` for graphs and the
composition bridge for realms/trees; the fixture counter tracks those leases.
This is not a zero-allocation or p99 call-path claim.

Precise remaining scope seam: local consumer specs currently start as siblings
under the precompiled metadata-only realm context. `Scope.With` must run on the
actual local provider **after successful native Provide**, and its derived
context must then become the metadata parent used to compile those consumers.
Changing to native children would add inherited dependencies/lifetime behavior;
adding a scope/container provider would violate the frozen two/four-fiber total.
Neither change is made here. A future approved harness-only two-phase composition
construction can derive the provider-written parent and still mount the declared
consumers as roots, but needs explicit dependency/restart propagation validation.
No fixture node, edge, key, count or scope-writer success is invented to close it.

Seed is fixed at `0x53000000`. This harness uses no PRNG: fixture and operation
orders are deterministic and recorded. The property corpus's SplitMix64/random
stream is a separate parent-owned test. Candidate order alternates by repetition.

## One real root per `RunT`

`RunT(ctx, backend, fixture)` returns a small `RunResult` schema containing
`Workload`, raw individual `Sample`s, actual final snapshots/provisions/events,
resource/lease counts, memory checkpoints and goroutine stacks. It performs:

1. Real graph mount + `Settle`, then inspects **every required fiber ACTIVE**, its
   effective configuration and actual retained dependency/provision UID/owner.
   `Settle` or `Await` returning by itself is never treated as ACTIVE.
2. Leaf `Update` + `Settle`, checked ACTIVE again.
3. Root-provider `Update` of fixture node **0** + `Settle`; for independent this
   restarts node 0 only, not fictitious dependents.
4. For realms, one real native `Batch` containing every provider's `BatchUpdate`,
   followed by `Settle`, labeled `all-providers-batch-restart-settle`. Graph mounts
   use `BatchMount`; composed mounts use `RootAuthority.BatchStart`, which compiles
   the actual metadata/specs and commits one native Batch with real returned IDs.
   No temporary backend, staging error, fake ID or Update-loop fallback is used.
5. Normal teardown submits **all** disposal requests in one native `BatchDispose`
   prefix before `Settle`. Providers may be requested first: the runtime must
   retain dependent bindings until cleanup drains. Only final failure/idempotent
   cleanup uses individual Dispose acknowledgements, never reported as a measured
   successful Batch. The check requires DISPOSED archives with no live effects,
   tracked work or target, no provision entries (ACTIVE or reserved), and zero
   fixture-owned active resources and open leases. Archives are not live fibers.
6. Always close the real root under a fresh cleanup context, even if the operation
   context expired. Compare candidate-owned goroutine stacks against the harness
   baseline after Close, then release frontend references. Native archive/event
   history is never truncated. A failed candidate cannot get a pass by leaking.

Trees first mount exactly 97 shared fibers, capturing the shared-only baseline
before any local mount. A run then executes ten cycles of individual tree cold
mount in one actual `BatchStart` + `Settle`, hot actual lookup, **one native Batch
containing the shared provider's Update** + `Settle`, reverse-tree-order disposal
in one native BatchDispose prefix, and remount at the next cycle. The shared
provider really invalidates its complete dependency closure. Every cold mount
has its own sample. Native Batch calls and mount/update/dispose group counts are
recorded in each `RunResult`; this helper does not claim sample-floor acceptance.

The prototype deadlines stay **load 2s, drain 5s, dispose 2s**. The outer
operation/cleanup guard is **15s**, not a changed kernel policy or a 2s whole
mount deadline. A mount taking 2.1s can finish and be recorded as a budget miss
instead of being hidden as an infrastructure timeout. Context errors remain
failed samples. There are no bare sleeps, retries or unlimited waits.

## Measurement and accounting

- `LatencyNS` includes the actual action plus **lifecycle `Settle` only**, using
  monotonic `time.Now` differences. Immediately afterward `AfterSettle` records
  native `ObserverStatus` **before** the separate explicit `AwaitObservers` join.
  `ObserverWaitNS`, `ObserversEndedUTC` and `SettleObserversLatencyNS` preserve that
  extra wait and the complete start-through-observers envelope; no existing
  lifecycle sample is silently relabeled to include it. Runtime.Settle is never
  changed to join callbacks. Correctness inspection is outside both timings.
  Graph operations remain sequential closed-loop, not §9's open-loop scheduler.
- Each operation records before/after heap/allocation/GC counters, resource/lease
  counts, full goroutine stacks/ownership and retained event-history counts.
  `eventBacklog` is the actual native dispatcher `Accepted - Delivered`, including
  an **in-flight** event, alongside raw `Remaining`, `Queued` and `InFlight`. The
  counters are checked for consistency with overflow protection. Post-join
  checkpoints record observed completion; no constant zero is substituted.
  Native owner inbox/work-queue depth is still not exposed and is **not** inferred
  from this dispatcher status. Retained history length is never backlog.
- Before every runtime GC checkpoint, observers are explicitly completed. A failed
  observer barrier records its actual live counters and an error, without forcing
  GC or presenting a completed retained-heap checkpoint. The empty harness cut has
  no runtime; empty-root/shared/retained/disposed/post-Close cuts name the actual
  native observer status. No listener body is added solely for scale accounting;
  the dispatcher still accepts/delivers the actual native event stream.
- Heap comparisons after GC include runtime archives and event history, plus any
  retained result/frontend references. Caller-owned declarative `Fixture` data,
  handle/context slices and the harness itself already exist at empty-root time;
  compiled native/realm specs are prepared **after** that baseline. Tree local
  specs/metadata are predeclared before shared-only time. These baselines measure
  activation/runtime retention; they do not claim total fixture-generator memory.
- Capturing per-operation stacks/history is diagnostic overhead outside the clock
  interval but can affect cache/GC scheduling; no observer-disabled optimized
  claim is made. Final raw events preserve total order, never sorted for equality.
  Composition provision values are private wrappers: ordinary JSON may encode an
  event's `Value` as `{}` rather than its provided integer. The parent diagnostic
  writer separately records actual `composition.ProvisionObservation` results;
  this harness never replaces an opaque value with an expected fixture value.
- `DisposedWithArchives` is before root Close; `PostClose` deliberately still
  references that Go root/result. Neither is claimed to be a zero-byte heap after
  removing all evidence. The next fresh trial does not reuse a closed root.
- Goroutines are identified by `runtime.Stack` ownership, not a toleranced
  subtraction of `runtime.NumGoroutine`. Fixed root workers are recorded before
  mounting. Full per-instance idle/termination conclusions require reviewing raw
  stacks, including any `Settle`/Close return-vs-worker-return boundary.

`FrozenPlan()` uses ten independent repetitions. Each graph operation has 100
fresh roots × ten repetitions = **1,000 individual observations per candidate,
topology and operation**. Tree shared restarts have ten roots × ten cycles × ten
repetitions = 1,000; cold mounts have much more. These are deliberately not ten
benchmark `ns/op` averages. The floor cannot be lowered via environment flags.

Nearest-rank quantiles report count/p50/p95/p99/max for individual samples and
per-root distributions, then aggregates. Population throughput CV is evaluated
across independent repetitions; `>10%`, host swapping or throttling invalidates
unchanged-protocol performance conclusions. Candidate failures override noise
classification. Timer overhead is recorded separately. Parent host evidence must
include actual before/after limits, swapping/throttling and background-load facts;
these are not discovered or invented by this package.

Current aggregate classification can be **fail**, **invalid**, or
**inconclusive**, never a whole-S0 acceptance pass: native Batch and dispatcher
backlog now use the real APIs, but the separate provider-written scope-value gap
is explicit; §8.3 call path and §9 acknowledgement arms are not implemented here. `RunT` success alone remains inconclusive regardless of
how fast a small graph runs. The defined `pass` schema value is reserved for later
complete, parent-validated evidence, not emitted to bless missing probes.

## Reproducible commands

Safe author checks (no full-scale runtime performance):

```sh
go test ./test/spike/s0/scale -run '^Test(SmallRealLifecycleSmoke|ObserverBacklogArithmetic|NearestRankAndCV|StackOwnershipAndRawWriters|MeasurementPreconditionsNoRuntime|DefaultMeasure)$' -count=1 -v
go vet ./test/spike/s0/scale
go test -race ./test/spike/s0/scale -run '^Test(SmallRealLifecycleSmoke|ObserverBacklogArithmetic|NearestRankAndCV|StackOwnershipAndRawWriters|MeasurementPreconditionsNoRuntime|DefaultMeasure)$' -count=3
```

The explicit filter above exercises only helper arithmetic/serialization, driver
preconditions, the default SKIP, and the real 16-fiber smoke (four opaque realms ×
four fibers) per candidate per requested `-count`. It does not execute diagnostic
or performance tests. Separate fixture unit tests can generate exact 10k/500/300
shapes **as data only**. Do not confuse helper passes with frozen budgets.

The following is a **future parent-managed command template, NOT executed**.
First complete full final-source parity, stop competing full-property/race jobs,
freeze criteria/source/fixture/executable hashes, retain host evidence and compile
a non-race executable. Use a fresh output directory; the harness refuses to
replace existing evidence. A full floor run is intentionally expensive.

```sh
go test -c -o "$S0_PARENT_EXECUTABLE" ./test/spike/s0/scale
# Parent hashes that exact executable and source tree, completes identity JSON,
# and records these exact commands/exit codes outside the running artifact.
GOMAXPROCS=8 S0_SCALE_RUN=1 \
  S0_SCALE_IDENTITY="$S0_PARENT_IDENTITY" \
  S0_SCALE_OUT="$S0_PARENT_FRESH_RESULT_DIRECTORY" \
  "$S0_PARENT_EXECUTABLE" -test.run '^TestDefaultMeasure$' -test.count=1 -test.timeout=24h
```

Identity JSON contains `criteriaSHA256`, `sourceSHA256`, `executableSHA256`,
`baselineCommit`, `commands`, `finalParityCompleted: true`, `hostEvidence`,
`observedThrottle` and `observedSwap`. The parent owns verification of those bytes
and the exact command, never the harness. Every closed root is written to paired
JSON/CSV before the next root, followed by `report.json`. Final events and archived
snapshots stay in raw JSON. Errors, failed operations and slow samples are not
removed from denominators. DefaultMeasure deliberately exits nonzero for an
inconclusive/invalid/failed report rather than letting `go test` imply acceptance.

No performance output file has been produced by the author checks. No candidate
selection or S0-complete claim is made.
