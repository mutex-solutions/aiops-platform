# S0 §8.3 actual callpath harness (runner implemented; acceptance run NOT executed)

Owns only this directory. No scheduler/composition changes, database, Node, product
provider, selection, acceptance claim, or performance execution accompany authoring.
Ordinary `go test` executes small causal semantic tests; the full run requires
`S0_CALLPATH_RUN=1` and is for the parent to review and execute later.

## Protocol recorded before execution

Authority: frozen `docs/design/s0-spike-criteria.md` §4 and §8.3, not an inferred
30/120-second rule from §2. §4 requires >=10 independent repetitions,
>=1,000,000 callpath observations per arm, individual nearest-rank p50/p95/p99/max,
and timer overhead separately. Its 30-second warmup/120-second window sentence
explicitly covers transaction and HTTP runs. No callpath baseline duration or
warmup duration is specified there. This harness fixes 100,000 observations **per
operation, candidate, concurrency and repetition**, ten fresh runs, alternating
candidate/arm order. Baseline is mutation-free and has no imposed elapsed duration.
No reduced count is an acceptance run. Every recorded observation remains present;
there is no latency subtraction, reservoir, sampled tail, or success-only filter.

Both native candidates use shared composition's actual typed `Provides`/`Need`,
per-call `AuthorizeRead`, captured-epoch `Read`, immutable provider view function,
and a real mounted provider activation. Separate operations are lookup/view,
complete contribution-owner Enter+Leave, and lookup/view + owner lease + actual
contribution method call. A bound view retains the resolving consumer and the
actual resource; its method acquires/releases the **provider's** native lease.
No consumer lease replaces the resource-owner lease. The bound-view value is
returned without allocating a fresh pointer; construction-time closures are not
made on calls. Baseline requires owner Enter+Leave p99 <=2µs and zero allocation
per operation, including the real scheduler wake callback. Other costs and
allocations are reported separately, never silently excluded from dispatch.

Stress is distinct: callers 1/8/32, exactly 12,000 open-time scheduled updates
(100/s for full 120s), round-robin across 100 unrelated mounted leaves, max eight
outstanding through Update acknowledgement + target Await. Every 1,000th schedule
also updates the leased owner in the same admitted work item; all 100 unrelated
leaf arrivals remain. Cap overflow is recorded as failure, never queued, slowed,
or used to change the rate. Consumer reactivation publishes a fresh atomic binding;
old activations may refuse Enter/Read and are counted as such, not retried inside
the latency sample. No inactive-owner method body may execute. Stress callers use
fixed 100,000 observations spread across the whole interval to bound raw storage;
concurrency means that many independent callers, not guaranteed simultaneous CPU
execution. No call throughput requirement is invented.

## Evidence boundary

Full execution must provide criteria/source closure/executable hashes, exact
commands, host/toolchain/affinity/GOMAXPROCS=8 and resource/noise records. Missing
identity/noise/raw evidence is inconclusive, never pass. Raw JSON records include
individual observations, outcomes, scheduled/observed mutation times and actual
provision UID/owner snapshots at causal checkpoints. Results are candidate, not
final-port evidence. Source closure hashing must cover actual linked Go inputs
(including composition and both candidates), not unrelated SQL/draft output.
The parent supplies/reviews that execution provenance and decides eligibility;
small unit checks cannot satisfy the performance gate.

## Running

Ordinary `go test` runs only the small causal semantic tests (no timing thresholds).
`TestFullCallpath` skips with "NOT MEASURED" unless `S0_CALLPATH_RUN=1`; a full run
then **fails** unless the classification is `pass`. Missed thresholds, correctness
failures, noise, and missing identity/noise/raw evidence are all non-pass.

Full acceptance run (about 2 hours 10 minutes: 10 repetitions x 2 candidates x 3
caller counts x 120 s stress, plus the short baselines). Use a quiet host, no race
detector, from the repository root, with a fresh output directory and the
parent-supplied identity JSON (`baselineCommit`, `commands`, `finalParityCompleted`,
`hostEvidence`, optional `expected*SHA256`):

```sh
GOMAXPROCS=8 S0_CALLPATH_RUN=1 S0_CALLPATH_OUT=<fresh dir> S0_CALLPATH_IDENTITY=<identity.json> \
  go test ./test/spike/s0/callpath -run '^TestFullCallpath$' -count=1 -timeout 4h -v
```

The runner computes the criteria, source-closure and executable digests itself and
records toolchain, affinity, GOMAXPROCS and per-run swap/throttle/load samples; it
never records absolute paths or host names. Output is created exclusively. A reduced
pipeline check (`S0_CALLPATH_DRYRUN=1`, `-run '^TestDryRunCallpath$'`, about 25 s) is
labelled NOT ACCEPTANCE and can never classify as `pass`.

## Implementation notes

Readings of the protocol where it was silent or ambiguous; each is the stricter one.

- **Operations.** `lookup_view` is `composition.Resolve` on the current consumer
  activation (per-call `AuthorizeRead`, captured-epoch `Read`, provider view
  function). The view returns a **value** `BoundView{consumer, owner}`, so no pointer
  is allocated per lookup. `owner_enter_leave` is the provider activation's native
  `Enter`+`Leave` including the scheduler wake callback. `lookup_view_lease_call` is
  Resolve + `BoundView.Call` (provider Enter, body, provider Leave). Each loads the
  current atomic binding/owner per call. The timer arm is the same measuring loop
  with an empty operation, reported separately and never subtracted.
- **Allocation.** Allocations are the process-wide `Mallocs` delta across the
  observation window, with goroutines and arrays created beforehand. The zero
  allocation budget is applied to `owner_enter_leave` in every run (not an average).
  Other operations' allocations are reported only. Observed: `Resolve` allocates 2
  objects per call inside `contextmodel.Context.Dependencies` (reached from
  `AuthorizeRead`), so lookup and dispatch cannot be zero-allocation.
- **Threshold scope.** p99 <= 2 microseconds is required of the aggregate over all
  ten runs **and** of every individual run; any run over is a fail, not averaged away.
- **Mutation-free baseline.** Any non-OK observation (including a refusal) fails the
  run. One fresh fixture per repetition and candidate; operation and caller order
  reverse on odd repetitions.
- **Alternation.** Candidate order reverses on odd repetitions; arm order
  (baseline/stress) reverses on odd repetitions too, so two orderings of each
  appear (the pair is always reversed together).
- **Stress observation.** Callers observe `lookup_view_lease_call` (the full call
  path). Each observation has a scheduled instant spread evenly over the window
  (observation j at j x 120 s / 100,000, assigned to caller j mod callers). Latency
  is the call's own start-to-end time; the scheduled instant, actual start offset and
  owner activation seen are recorded alongside, so lateness is visible and never
  subtracted. This offered load is about 833 calls/s; the deterministic refusal
  semantics are proved by the ordinary tests, and the run fails unless every owner
  update produced exactly one owner activation and one fresh consumer binding.
- **Stress accounting.** Every scheduled arrival has a record, including overflow.
  Each admitted item has a 5 s deadline from its scheduled arrival (timeout is a
  failure). Any overflow, failed or timed-out item, any unexpected error class,
  any executed inactive-owner body, outstanding work after a 30 s drain, a
  non-ACTIVE fiber after drain, or a wrong observation count fails. This is stricter
  than the 99.9% acknowledgement rule of §4. Refusals on old activations are counted
  per outcome class and are not failures.
- **Stress fixtures.** One fresh fixture per (repetition, candidate, caller count),
  so each of the 60 stress runs has exactly 12,000 scheduled updates. There is no
  warm-up and no discarded observation.
- **Inactive-owner body.** The owner's native disposer sets a closed flag after leases
  drain; the body counts a violation if it runs with the flag set.
- **Noise.** Throughput is observations per wall second of each baseline arm; a
  coefficient of variation above 10% across the ten runs, any swap or cgroup CPU
  throttle delta, GOMAXPROCS other than 8, a race build, or a criteria digest that
  differs from the freeze receipt classifies as `invalid` (repeat unchanged). A
  counter that cannot be observed makes the run `inconclusive`. Candidate failures
  override noise classification.
- **Source closure.** Production Go of concurrency, composition, contextmodel,
  internal/eventqueue and scale (scale is linked for its exported nearest-rank, CV
  and candidate helpers), every file of this package including tests and this
  README, `go.mod`, `go.sum`, the criteria and the freeze receipt. A test checks that
  every module-local package the binary imports is covered. The process must run
  from the package directory (as `go test` does).
- **Dry run.** Reduced counts, or any plan other than the frozen one: classification
  can only be `inconclusive` or worse, and `Run` refuses a reduced plan unless `Dry`.

## Protocol amendment 1 (2026-11-19)

The measurement protocol is amended by
[docs/design/s0-callpath-protocol-amendment-1.md](../../../../docs/design/s0-callpath-protocol-amendment-1.md),
in writing before the next run. The frozen criteria are unchanged. The two earlier
full runs are not reclassified. Where this section and an earlier note disagree,
the amendment wins; the earlier text is kept for the record.

Superseded by the amendment:

- **Allocation** (Implementation notes): the sentence "goroutines and arrays created
  beforehand" is extended: the coordinator no longer waits on a `sync.WaitGroup`
  inside the window (sleep-poll on an atomic counter, timer warmed before), and a
  timer-only control that reads non-zero in a run makes that run invalid for
  allocation. A non-zero lease reading with a zero control remains a fail.
- **Noise** (Implementation notes): throughput is no longer observations per
  coordinator wall second. It is the sum of per-caller observations over each
  caller's own first-to-last span, with callers released by an atomic start flag.
  The 10% rule, swap/throttle counters and GOMAXPROCS rule are unchanged.
- **`MeasureArm` description** (the channel-close start and the `done.Wait()` window)
  describes the original protocol (amendment 0).

Not superseded: window length (100,000 per run), operations, candidates, callers,
repetitions, alternation, stress, thresholds, evidence and identity, and
classification precedence. This directory implements amendment 1: every record
carries `protocolAmendment: 1` and the evaluator refuses to mix amendments. The
full-run command in "Running" is unchanged.

## Protocol amendment 2 (2026-11-19)

Amended again by
[docs/design/s0-callpath-protocol-amendment-2.md](../../../../docs/design/s0-callpath-protocol-amendment-2.md),
in writing before the next run. Amendment 1 stays in force except where amendment 2
says otherwise; the three earlier full runs are not reclassified. Amendment 2 adds one
recorded, labelled warm-up baseline repetition at process start, excluded from
classification, and reads the 10% throughput-variation rule from the timer-only
control of the same candidate and caller count (a measured arm above 10% with a clean
control is reported as `candidateVariation`, not invalid). Thresholds, counts and the
full-run command are unchanged.
