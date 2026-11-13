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
4. For realms, all provider `Update` acknowledgements followed by one `Settle`.
   This is labeled `all-providers-update-loop-settle`, **not native Batch**.
5. Teardown submits **all** disposal requests before `Settle`. Providers may be
   requested first: the actual runtime must retain dependent bindings until
   cleanup drains. The check requires DISPOSED archives with no live effects,
   tracked work or target, no provision entries (ACTIVE or reserved), and zero
   fixture-owned active resources and open leases. Archives are not live fibers.
6. Always close the real root under a fresh cleanup context, even if the operation
   context expired. Compare candidate-owned goroutine stacks against the harness
   baseline after Close, then release frontend references. Native archive/event
   history is never truncated. A failed candidate cannot get a pass by leaking.

Trees first mount exactly 97 shared fibers, capturing the shared-only baseline
before any local mount. A run then executes ten cycles of individual tree cold
mount + `Settle`, hot actual lookup, **one Update of the shared provider** +
`Settle`, reverse-tree-order disposal, and remount at the next cycle. One Update
really invalidates the complete shared dependency closure; it is not a fake loop
or an invented Batch API. Every cold mount has its own sample. The parent must
still withhold native-Batch-specific acceptance.

The prototype deadlines stay **load 2s, drain 5s, dispose 2s**. The outer
operation/cleanup guard is **15s**, not a changed kernel policy or a 2s whole
mount deadline. A mount taking 2.1s can finish and be recorded as a budget miss
instead of being hidden as an infrastructure timeout. Context errors remain
failed samples. There are no bare sleeps, retries or unlimited waits.

## Measurement and accounting

- Operation latency includes the actual action plus `Settle`, using monotonic
  `time.Now` differences. UTC scheduled/start/end timestamps accompany it.
  Graph operations are sequential closed-loop operations, not the open-loop
  HTTP/churn scheduler of §9. Correctness inspection is outside the timed action.
- Each operation records before/after heap/allocation/GC counters, resource/lease
  counts, full observed goroutine stacks and ownership groups, and event-history
  counts. The candidates expose **no pending event/observer backlog**. JSON uses
  `eventBacklog: null`; CSV says `unavailable`. History length is never queue depth.
- Heap comparisons after GC include runtime archives and event history, plus any
  retained result/frontend references. Caller-owned declarative `Fixture` data,
  handle/context slices and the harness itself already exist at empty-root time;
  compiled native/realm specs are prepared **after** that baseline. Tree local
  specs/metadata are predeclared before shared-only time. These baselines measure
  activation/runtime retention; they do not claim total fixture-generator memory.
- Capturing per-operation stacks/history is diagnostic overhead outside the clock
  interval but can affect cache/GC scheduling; no observer-disabled optimized
  claim is made. Final raw events preserve total order, never sorted for equality.
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
**inconclusive**, never a whole-S0 acceptance pass: required native Batch/backlog
and separate scope-value gaps are explicit; §8.3 call path and §9 acknowledgement arms
are not implemented here. `RunT` success alone remains inconclusive regardless of
how fast a small graph runs. The defined `pass` schema value is reserved for later
complete, parent-validated evidence, not emitted to bless missing probes.

## Reproducible commands

Safe author checks (no full-scale runtime performance):

```sh
go test ./test/spike/s0/scale -count=1 -v
go vet ./test/spike/s0/scale
go test -race -count=3 ./test/spike/s0/scale
```

Tests generate exact 10k/500/300 shapes **as data only**. The one real smoke uses
16 fibers (four actual opaque realms × four fibers), once per candidate per
requested `-count`. Do not confuse those helper passes with frozen budgets.

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
