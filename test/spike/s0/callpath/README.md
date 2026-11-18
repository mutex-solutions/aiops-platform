# S0 §8.3 actual callpath harness (authoring only)

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
