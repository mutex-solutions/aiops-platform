# S0 scale and cold-mount harness — ready to run; acceptance runs NOT executed

**Status: the SCALE gate (criteria §4, §8.1, §8.2) and the COLD MOUNT gate (§9)
have a complete harness. Neither has been executed as an acceptance run.** Ordinary
`go test` runs only small deterministic semantic tests (no timing thresholds).
`TestDefaultMeasure` and `TestColdMountRun` SKIP unless explicitly authorised, and
a skip is not a pass. Dry-run modes exist, are labelled in every record and can
never classify as `pass`. The parent owns final parity, source/hash freeze, the
idle-host evidence, execution and candidate selection.

This is a test-only experiment, not a final SDK, production port, new policy,
transaction/HTTP workload or new S0 acceptance criterion. It imports only the Go
standard library and the S0 concurrency/composition/contextmodel code.

Authority is the unchanged frozen
[`s0-spike-criteria.md`](../../../../docs/design/s0-spike-criteria.md) §§4, 8.1,
8.2 and 9 (§8.3 call path is `test/spike/s0/callpath`). No threshold is tuned here.
The runner computes the criteria digest itself and compares it with the freeze
receipt (`docs/design/s0-criteria-freeze.json`); a mismatch is `invalid`.

## Requirements table

"Before" is the state recorded by the previous version of this README; "Now" is
what the harness implements. `done` means implemented and covered by a unit test
or a small-fixture semantic test; every status refers to the harness, not to a
candidate result.

### SCALE (§4, §8.1, §8.2)

| # | Criteria text (quoted) | Implemented by | Before | Now |
| --- | --- | --- | --- | --- |
| S1 | §4 "Use the same host and toolchain for both candidates/arms, `GOMAXPROCS=8`" | `CollectIdentity` records Go version, GOOS/GOARCH, GOMAXPROCS; `Measure` refuses a non-frozen plan unless `Dry`; GOMAXPROCS≠8 is `invalid` | partial (error only, no toolchain record) | done |
| S2 | §4 "Record CPU affinity and resource limits rather than inferring dedicated hardware from visible CPU count." | `IdentityRecord.Affinity/CPUMax/MemLimit/NumCPU/CPUModel/Kernel`, `cgroupFile` ("unavailable" is recorded, never "unlimited") | missing | done |
| S3 | §4 "Fixture deadlines: load 2 seconds, drain 5 seconds, dispose 2 seconds." | unchanged kernel knobs; the harness only adds a 15 s outer guard (`TrialGuard`), not a changed policy | done | done |
| S4 | §4 "at least ten independent repetitions" … "Alternate arm/candidate order." | `FrozenPlan.Repetitions=10`; candidate order reverses on odd repetitions (`Measure`) | done | done |
| S5 | §4 "at least 1,000 observations **per candidate, topology and operation**, across fresh runs, not ten aggregate `ns/op` averages" | graphs 100 fresh roots × 10 reps; trees 10 roots × 10 cycles × 10 reps (cold mounts: ×500/300 trees); `Plan.expectedObservations`, `floorFindings` | partial (a reason string only) | done (floor and scheduled count both gate; short = `inconclusive`) |
| S6 | §4 "Quantiles use nearest-rank on individual operation latencies. Report p50, p95, p99, maximum, sample count and per-run distributions as well as aggregates." | `NearestRank`, `Summarize` (exported API unchanged), `OperationSummary.PerRun` | done | done |
| S7 | §4 "If a probe has fewer observations than its stated floor, it is inconclusive." | `floorFindings` → `Inconclusive`; also fewer than scheduled | partial | done |
| S8 | §4 "Optimized performance is measured without the race detector." | `raceEnabled` (`race_on.go`/`race_off.go`) → `invalid` | missing | done |
| S9 | §4 "Throughput coefficient of variation greater than 10% across independent runs, or observed host CPU throttling or swapping, requires a repeat of the unchanged protocol … Preserve the invalid data" | `cvFinding`; `ReadHost` before/after every root, `hostDelta`, `noiseFindings` (swap/throttle → `invalid`, unobservable → `inconclusive`) | partial (parent-asserted booleans) | done |
| S10 | §4 "Candidate OOM, panic, deadlock, race, resource leak, unbounded queue or its own cap violation is a failure, not environmental noise." | any `RunResult.Errors` → `Fail`; `Fail` outranks `Invalid` in `Resolve`; earlier roots' paired raw records persist | partial | partial: a process crash (OOM, stack overflow, panic) leaves raw records but no `report.json`; that absence is itself the failure |
| S11 | §2 run record: criteria digest, baseline commit, source-tree digest "including uncommitted experimental files", executable digest, exact commands, toolchain | `IdentityRecord`, `sourceClosure` (every linked local package, working tree), `fileSHA256(os.Executable())`, parent-supplied `Identity` | missing (parent digests trusted) | done |
| S12 | §2 "Keep raw results as well as summaries." §4 "Do not discard slow runs" | per root: `….json.gz` (samples, checkpoints, host, retained metrics), `….trace.json.gz` (every event, archive, provision), `….csv`; manifest with SHA-256, `Verify` | partial | done (about 19 GiB for the frozen plan) |
| S13 | (evidence integrity) no overwrite | `NewEvidenceWriter` refuses an existing directory; every file `O_EXCL` | partial | done |
| S14 | §8.1 "Each topology has exactly 10,000 measured fibers, excluding the kernel's root" and the five shapes | `fixture.go` `Graph`, `RealmFixture`, `FrozenFixtures` | done | done |
| S15 | §8.1 "Every required fixture fiber must reach ACTIVE before a mount settles." | `inspectActive` (every fiber, epoch, provision UID/owner) | done | done |
| S16 | §8.1 "Measure mount/settle, leaf update/settle, provider restart/settle and teardown"; realms "Restart one provider (local closure) and all providers in one Batch" | `RunCycles` (`mount-settle`, `leaf-update-settle`, `root-restart-settle`, `all-providers-batch-restart-settle`, `teardown`) | done | done |
| S17 | §8.1 "The empty root and post-shared fixture baselines are recorded separately." | `RunResult.EmptyRoot`, `SharedOnly` | done | done |
| S18 | §8.1 mount p99 ≤2 s; leaf p99 ≤10 ms; root-provider restart ≤5 s | `budgetFinding` (restart held at the maximum) | done | done |
| S19 | §8.1 "Retained heap over the empty/shared baseline ≤128 MiB" | `retainedMetrics`, `heapFinding` on every retained checkpoint; per-fiber, total, slope, stack-in-use recorded | partial | done (method below) |
| S20 | §8.1 "Idle goroutine per fiber: Zero; fixed root workers recorded separately" | `EmptyRoot.Goroutines` enumerates root workers before any mount; `idleGoroutineFindings` → `Fail` on any candidate owner above its baseline | partial (reason only) | done |
| S21 | §8.1 "Teardown: Resource/registry/effect/lease baseline restored" | `inspectDisposed`, post-Close owner comparison, fixture resource/lease counters | done | done |
| S22 | §8.1 "Property/random workload seed … record the PRNG algorithm" | the harness has no PRNG; `Workload.PRNG` records "none"; the property corpus is parent-owned | n/a | n/a |
| S23 | §8.2 shared 97 fibers; two/four-fiber trees; "A scope value distinguishes each tree … Count every container fiber if the fixture creates one" | `TreeFixture`; **`MountTree` two-phase construction with the real `composition.Scope`** (see below); `scopeStep` | partial (scope seam open) | done |
| S24 | §8.2 "500 loaded trees and an additional 300-tree run" | `FrozenFixtures` (500/300 × 2/4) | done | done |
| S25 | §8.2 "ten full cycles of mount-all, hot lookup, one Batch shared-provider restart, reverse-order tree disposal and remount; independently record each tree's cold mount" | `runTrees` | done | done |
| S26 | §8.2 "Eager tree mount at boot: Zero" | snapshot count equals 97 before the first local mount | done | done |
| S27 | §8.2 "Each measured in-process cold mount <50 ms; report maximum" | `budgetFinding` (maximum, strictly below) | done | done |
| S28 | §8.2 "Idle goroutines that scale with trees: Zero" | same owner comparison against the shared-only checkpoint | partial | done |
| S29 | §8.2 "Retained heap above shared-only baseline for 500 trees ≤32 MiB" | `retainedMetrics` against `SharedOnly`; strict reading, **see Decision 1** | partial | done (strict; open decision) |
| S30 | §8.2 "Shared root-provider restart and settle ≤5 s" | one native Batch containing the shared provider's Update; maximum ≤5 s; **every shared and mounted local fiber must reload exactly once** | partial | done |
| S31 | §8.2 "Cross-scope lookup/callback/effect leakage: Zero" | consumer bodies check the real `Scope.From` value and the realm-local provision; `HotLookup`; a consumer's `Scope.With` must be refused | partial | done |
| S32 | §8.2 "Churn and final teardown: Empty owned ledger and restored resource baseline" | per cycle resource/lease check, `inspectDisposed` at the end | done | done |

### COLD MOUNT (§9)

The probe did not exist: no file mentioned section 9 beyond "not implemented".

| # | Criteria text (quoted) | Implemented by | Before | Now |
| --- | --- | --- | --- | --- |
| C1 | "Compare diagnostic arm A (cold mount inline before commit/ack) with arm B (all ack/page dependencies pinned while other synthetic trees cold-mount and churn)." | `RunColdArm` arms `A`, `B0`, `B1`; the pinned set is the 97 shared fibers, mounted for the whole run and never churned | missing | done |
| C2 | "alternate a pinned-only baseline B0 and pinned-with-churn B1 on identical data and workload" | `MeasureColdMount`: B0/B1 order reverses on odd repetitions; same fixture, body and schedule per cell | missing | done |
| C3 | "Each arm uses ten 30-second-warm-up plus 120-second-measured repetitions." | `FrozenColdPlan` (10 × 30 s + 120 s), warm-up arrivals recorded and flagged, excluded from the measured window | missing | done |
| C4 | "B1 schedules 50 fresh four-fiber tree mounts per second (IDs round-robin across 500 scope labels), at most eight mounts in flight, then disposes each tree after 100 ms of idle time" | churn scheduler, 50/s, label = seq mod 500, cap 8, `MountTreeAwait`, 100 ms idle, `disposeTree` (one native BatchDispose, joined per fiber) | missing | done |
| C5 | "B0 runs the same scheduler without starting mounts." | same `schedule` loop, arrivals recorded as `idle(B0)` | missing | done |
| C6 | "Record every scheduled/completed background mount and disposal; a missed churn arrival or cap/backlog overflow fails B1" | `ChurnRecord` per arrival (`Outcome`, `Late`, timestamps); `runFindings` fails overflow, late start, collision, error, not-disposed | missing | done |
| C7 | "Pair B0/B1 repetitions in alternating order and evaluate the ≤5 ms delta on each matched pair as well as aggregate." | `EvaluateCold`: B1 p99 − B0 p99 per matched repetition and pooled; `ColdReport.Pairs` | missing | done |
| C8 | "Arm A uses one fresh four-fiber scope per first-time request and the same 100-request/s schedule; report its complete latency and mount results, not a pinning exemption." | `AckCold`: every request mounts a fresh tree inline before commit; `ColdCell.Aggregate` and `.Mount` | missing | done |
| C9 | "At 100 requests/second test bodies with 100 synthetic items and a separate 1 MiB body." | `FrozenColdPlan.Bodies` (100 items; 4,096 items × 256 B = 1 MiB) | missing | done |
| C10 | "Arm B has zero cold-ensure/mount calls in its measured ack path" | `coldProbe.ensures` counted only by `AckCold`; B arms with a nonzero count fail | missing | done |
| C11 | "p99 ack ≤250 ms for the 100-item body and ≤1 second at 1 MiB." | `ackLimit`, per run and pooled | missing | done |
| C12 | "Background cold churn may increase arm B p99 by no more than 5 ms over its pinned baseline." | see C7 | missing | done |
| C13 | "Record the complete mount+transaction latency of arm A rather than subtracting mount cost." | arm A latency is scheduled arrival to acknowledgement, mount included | missing | done |
| C14 | §4 open loop: "Latency starts at scheduled arrival, includes queue/pool wait"; "Record every scheduled arrival, including overload, error and timeout"; request cap 32; "5 seconds from scheduled arrival"; "at most 30 seconds for all committed/acknowledged IDs to reach logical completion"; "at least 99.9% successful commits or acknowledgements" | `schedule` never slows down; `AckRecord` for every arrival; cap 32 → `overflow`; 5 s deadline; 30 s drain → `Outstanding`; ledger reconciliation; success ratio | missing | done |
| C15 | "A separate cold-timeout fixture returns 503 with Retry-After on synchronous lookup or snoozes the simulated job. It never acknowledges an uncommitted write or falls through to another scope's/process contribution." | `RunColdTimeoutFixture` (both outcomes) with a warm foreign scope present | missing | done |
| C16 | noise, floors, identity, raw records, refusal to overwrite (§2, §4) | shared with SCALE: `CollectIdentity`, `ReadHost`, `Resolve`, `EvidenceWriter` | missing | done |

## Scope seam — outcome

The previous README recorded: *"local consumer specs currently start as siblings
under the precompiled metadata-only realm context. `Scope.With` must run on the
actual local provider after successful native Provide, and its derived context must
then become the metadata parent used to compile those consumers."*

**Closed, without bending the fixture.** `composition.Scope.With` runs on the
realm-local provider's own activation after its successful native `Provide`
(`scopeStep`), and the derived context is published per tree. `MountTree` then:

1. commits the provider alone with one native `BatchStart` (parent: the realm
   context) and joins that target only (`Fiber.Await`);
2. takes the provider-written derived context as the **metadata parent** of the
   declared local consumers and commits them as **roots** in one more
   `BatchStart`. This is the pattern `composition_test.go`
   (`TestEntryIdentityAndNativeScopeOwnership`) already uses.

Nothing is added: the tree is still exactly two/four fibers (97 + 500×2/4 and
97 + 300×2/4 in total), there is no container fiber, no new key, edge or count,
and each consumer keeps its single native dependency on the realm-local key.
Because `contextmodel.Context.OwnDependencies` lists only a fiber's own
declarations, a consumer parented under the provider's derived context does not
inherit the provider's need on the shared key.

Validated (`tree_scope_test.go`, both candidates, 2/4-fiber trees):

- shared-provider restart reloads every shared and every mounted local fiber
  exactly once (complete dependency closure; also a per-cycle invariant in
  `runTrees`), while one tree provider's restart reloads only that tree;
- each consumer observes `Scope.From == its tree` and its own tree's provider
  value under the identical key name; a consumer's `Scope.With` is refused;
- the archived fiber count is exactly shared + cycles × locals (no hidden fiber);
- a stale incarnation's derived context is never reused (`MountTree` clears it).

One consequence is part of the measurement: a tree's cold mount is **two** native
`BatchStart` calls (provider, then consumers) inside one measured interval
(`BatchStart(provider)` + provider `Await` + `BatchStart(consumers)` + `Settle`).
That interval is what is compared with 50 ms.

## Implementation notes (interpretations; each is the stricter reading)

1. **Decision 1 (open, owner): retained heap in §8.2 is gated at every cycle.**
   The table says "Retained heap above shared-only baseline for 500 trees ≤32 MiB".
   The harness checks every retained checkpoint (one per cycle, archives and event
   history included) against 32 MiB, for all four tree fixtures including the
   300-tree runs. The runtime keeps the archive and the events of every disposed
   incarnation, so the same 500 trees retain more each cycle. The first checkpoint
   (500 trees loaded, no history) is reported next to the worst
   (`HeapSummary.MaxFirstBytes`, `RetainedMetrics.FirstDeltaBytes`) so the owner can
   decide whether the budget is the first checkpoint, or whether the candidates
   must bound their history. Nothing here relaxes it.
2. Restarts (`root-restart-settle`, `shared-provider-restart-settle`,
   `all-providers-batch-restart-settle`) are held to 5 s at the **maximum**, not
   p99; the criteria give a plain bound. Cold tree mounts are held strictly below
   50 ms at the maximum.
3. Idle goroutines: any candidate-owned goroutine owner whose count exceeds its
   enumerated empty-root (graphs) or shared-only (trees) count at a retained
   checkpoint is a `Fail`, without waiting for a reviewer.
4. A bucket must hold exactly the scheduled observation count, not merely the
   floor; a shortfall is `inconclusive`. A plan that is not byte-equal to
   `FrozenPlan` is never acceptance-eligible and must be labelled `Dry`.
5. Throughput CV stays as before (per bucket, across the ten repetitions,
   observations ÷ summed latency). Fewer than two evaluable repetitions is
   `inconclusive`. Swap counters (`pswpin`/`pswpout`) are system-wide; any paging
   during any root is `invalid`.
6. Per-sample event history length is the dispatcher's accepted count; cloning
   the ledger per sample was quadratic. The ledger is fetched once per root, its
   contiguous sequence verified and its length required to equal the accepted
   count (`verifyLedger`).
7. A tree cold-mount sample is confirmed by the fixture's own record that every
   fiber of that incarnation ran its body (after `Provide`, scope and read checks)
   plus a clean target join. The complete Inspect-based ACTIVE check (every epoch,
   provision UID and owner) runs for the first and last tree of each cycle and for
   every tree once per cycle, because a full Inspect per sample is quadratic in
   archived history. Graph operations still run the complete check after every
   operation.
8. Cold mount, what is simulated: no database or tenant cache. A request takes a
   pinned activation's lease (`Enter`), resolves its typed provision
   (`composition.Resolve`), leaves, and hashes one 256-byte item (the item size is
   an interpretation; the criteria fix only the count and 1 MiB), for every item;
   then appends one commit to an in-memory ledger and only then is acknowledged.
   Arm A resolves the fresh scope's local contribution instead.
9. Cold mount counting: `overflow` of the 32-request cap and `error` fail the
   run outright; `timeout` (5 s from scheduled arrival) and `503` stay in the
   99.9% success rule with their latency in every distribution; an unserved
   overflow carries the 5 s timeout as its latency. p99 ack limits and the 5 ms
   delta are applied to every run and to the pooled observations. Arm A is held to
   integrity and the 99.9% rule only: no latency limit is invented for it.
10. Churn "late" means a mount started more than one churn interval (20 ms) after
    its scheduled instant: the offered rate was not sustained, so B1 fails.
11. The cold-timeout fixture stalls a cold scope's real provider body (it
    cooperates with cancellation) so its mount cannot finish inside a 50 ms
    cold-ensure budget; the budget is a fixture parameter, not a kernel policy.
12. The previous in-process diagnostic (`diagnostic_test.go`) and its v2 process
    isolation rule are kept as history; failures at 10,000 fibers should be run one
    case per process (`TestDevObserve`).

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

`Graph`, `RealmFixture` and `TreeFixture` permit explicit small fixtures;
`Validate` rejects duplicate provider keys, undeclared/missing providers, duplicate
Needs, and forward/cyclic edges. Generation and validation are iterative, not
recursive, including the 10k chain. Every actual native `Spec` declares all
body/cleanup reads. Each body uses real `Config`, `Read`/typed `Resolve`, `Provide`
of its immutable integer node index, and one native `Defer`-owned resource;
cleanup reads dependencies again. No body starts a persistent goroutine or does
network I/O. Graphs use native string-keyed specs; realm/tree fixtures use the
composition frontend with real contextmodel opaque labels.

Seed is fixed at `0x53000000`; the harness uses no PRNG, fixture and operation
orders are deterministic and recorded.

## One real root per run

`RunCycles(ctx, backend, fixture, cycles)` (`RunT` = ten cycles) runs one fresh
root: mount + `Settle` with every fiber inspected ACTIVE; leaf `Update`; node-0
`Update`; for realms one native Batch of every provider's update; teardown as one
native `BatchDispose` prefix. Trees mount the 97 shared fibers, capture the
shared-only baseline, then run the cycles of per-tree cold mount (`MountTree`),
hot lookup, one Batch shared-provider update, reverse-order disposal and remount.
The root is always closed under a fresh cleanup context and its goroutine owners
compared with the baseline taken before the root existed.

Deadlines stay load 2 s, drain 5 s, dispose 2 s inside the kernel; the outer guard
is 15 s so a 2.1 s mount is recorded as a budget miss, not hidden as a timeout.
Context errors remain failed samples. No bare sleeps, retries or unlimited waits.

## Measurement and accounting

- `LatencyNS` covers the action plus lifecycle `Settle` only (monotonic
  `time.Now`); `AfterSettle` records native `ObserverStatus` before the explicit
  `AwaitObservers` join, whose cost is kept separately. Correctness inspection is
  outside both timings.
- Retained heap method: `runtime.GC()` then `HeapAlloc`, after observers are
  drained, at every retained checkpoint; the delta is against the empty-root
  (graphs) or shared-only (trees) checkpoint. Archives, event history and any held
  result/frontend references are included; compiled specs are prepared after the
  empty-root baseline. `HeapInuse`, `StackInuse` and `Sys` are recorded too.
  Per-fiber bytes, the regression slope per checkpoint and the goroutine count are
  in `RetainedMetrics`/`HeapSummary`.
- Goroutines are identified by `runtime.Stack` ownership, never by a toleranced
  `NumGoroutine` subtraction; root workers are recorded before mounting.
- Capturing per-operation stacks is diagnostic overhead outside the clock interval
  but can affect cache/GC scheduling; no observer-disabled optimized claim is made.
- Composition provision values are private wrappers: raw event `Value` may encode
  as `{}`; the diagnostic writer records `composition.ProvisionObservation`.

## Classification

`Resolve` applies **fail > invalid > inconclusive > pass**. Candidate failures
(budget, heap, goroutine, teardown, correctness, cap/overflow, outstanding work)
are `fail`; protocol-environment problems (criteria not the frozen bytes,
GOMAXPROCS≠8, race build, swap/throttle, CV >10%, source changed during the run)
are `invalid`; everything missing (identity facts, counters that cannot be
observed, observations below the floor or the scheduled count, absent cells,
unevaluable CV, incomplete raw records) is `inconclusive`. `pass` exists only when
no finding exists and the plan is the frozen one; `Dry` can never be better than
`inconclusive`. A full run fails its Go test unless the result is `pass`.

## Commands

Safe author checks (fast, deterministic, no timing thresholds):

```sh
go vet ./test/spike/s0/scale
go test -race -count=10 ./test/spike/s0/scale
```

Run compiled test binaries with this package directory as the working directory
(`go test` does that); source and criteria paths are relative to it.

Dry runs (reduced, labelled NOT ACCEPTANCE, never `pass`):

```sh
go test -c -o "$EXE" ./test/spike/s0/scale
cd test/spike/s0/scale
S0_SCALE_DRYRUN=1 S0_SCALE_OUT=<fresh dir> "$EXE" -test.run '^TestDryRunScale$' -test.count=1 -test.timeout 30m    # about 5 s
S0_SCALE_DRYRUN=1 S0_SCALE_DRYSIZE=full S0_SCALE_OUT=<fresh dir> "$EXE" -test.run '^TestDryRunScale$' -test.count=1 -test.timeout 60m   # frozen sizes, one root per cell, about 2.5 min, 100 MiB
S0_COLD_DRYRUN=1 S0_COLD_OUT=<fresh dir> "$EXE" -test.run '^TestDryRunColdMount$' -test.count=1 -test.timeout 30m   # about 90 s
```

Frozen runs (idle host, non-race executable, a fresh output directory each; the
output directory must not exist). `S0_SCALE_IDENTITY` / `S0_COLD_IDENTITY` is the
parent-supplied strict JSON:

```json
{"baselineCommit": "<commit>", "commands": ["<exact command lines>"], "finalParityCompleted": true,
 "hostEvidence": "<idle-host record: load, background processes, swap and throttle counters>",
 "expectedCriteriaSHA256": "<optional>", "expectedSourceSHA256": "<optional>", "expectedExecutableSHA256": "<optional>"}
```

```sh
go test -c -o "$EXE" ./test/spike/s0/scale          # no -race
cd test/spike/s0/scale
GOMAXPROCS=8 S0_SCALE_RUN=1 S0_SCALE_IDENTITY="$ID" S0_SCALE_OUT="$FRESH_SCALE_DIR" \
  "$EXE" -test.run '^TestDefaultMeasure$' -test.count=1 -test.timeout 24h -test.v
GOMAXPROCS=8 S0_COLD_RUN=1 S0_COLD_IDENTITY="$ID" S0_COLD_OUT="$FRESH_COLD_DIR" \
  "$EXE" -test.run '^TestColdMountRun$' -test.count=1 -test.timeout 12h -test.v
```

Run the two gates one after the other, never together. Trace retention defaults to
every root (`S0_SCALE_TRACE=all`); `first-and-failed` keeps less and caps the
result at `inconclusive`. Output: `identity.json`, `source-closure.json`, one
`….json.gz`/`….trace.json.gz`/`….csv` per root (scale) or one `….json.gz` per arm
run (cold mount), `report.json`, `manifest.json`.

Development-host observation helper (NOT acceptance; one case per process):

```sh
S0_SCALE_DEV=chain/graph-lock/10000 "$EXE" -test.run '^TestDevObserve$' -test.count=1 -test.v
S0_SCALE_DEV=trees/reconciler/500/4 "$EXE" -test.run '^TestDevObserve$' -test.count=1 -test.v
```

## Known candidate behaviour (reproductions, not fixed here)

`candidate_defects_test.go` holds two skipped, deliberately failing reproductions
(`S0_SCALE_REPRO=1`): graph-lock mount cost that grows with the number of
providers at a fixed fiber count (held service events rescanned per pending load
and per composed pre-check), and retained heap that grows with churn history at a
constant live set. See the report that accompanied this change for numbers.

No performance output file has been produced by the author checks. No candidate
selection or S0-complete claim is made.
