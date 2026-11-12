# Experimental lifecycle concurrency cores

Both implementations are S0 probes, **not sdk/kernel**. They use standard-library
Go only and share a neutral fixture API. The same declared setup and common tests
exercise actual implementations rather than a fake runtime.

- `NewReconciler` has one graph-state owner, indexed notifications, a deduplicated
  queue and off-owner transition/check workers.
- `NewGraphLock` has a graph mutex and workers only for active transitions/checks;
  plugin callbacks never execute under the graph lock.
- Both retain providers until dependent cleanup completes, use revision and
  provision identities, distinguish sticky startup failure from availability,
  quiesce before sequential LIFO disposal and track call-path lease drain.
- Inside Apply, self-disposal requests a target change without awaiting itself;
  external target-scoped completion is separate. Explicit Await retains a startup
  error after disposal; source disposal-promise semantics are handled separately
  by the replay adapter, not by globally swallowing errors.
- `Record` is test-only immutable JSON instrumentation, sequenced in the same
  actual event ledger. It supports replay evidence, not a production policy or
  a replacement for G5's off-owner observer proof.

Common lifecycle and availability tests, candidate-specific race tests and
virtual-time deadline regressions have run. Initial integration failures were
preserved and investigated; see
[the result record](../results/scheduler-integration-initial.md). Exact current
validation must be rerun after in-progress API integrations settle.

## Explicit remaining gaps

No complete context-tree, parent/child or optional ownership, key classes, scopes,
hooks, config pipeline, Batch, tracked-Go API or final-kernel construction exists
in these cores yet. A separate metadata prototype is not integration proof.

Snapshots currently expose activation/committed dependency UIDs and the desired
revision separately; they do not yet reproduce the source target-epoch diagnostic
at every in-flight boundary. Full ordered trace parity remains unproved, notably
for off-owner availability checks. Histories are retained for inspection and are
not a bounded production observer/evidence-retention design. Active availability
checks still need the complete cancellation/deadline proof.

No 10,000-fiber/500-tree/cold-ack/call-path performance acceptance, full randomized
lifecycle property run, final-port coverage, selected-core decision or line budget
is established by passing these unit tests. Do not compare partial checkpoints
as if they were the frozen complete Cordis traces. Follow the
[criteria](../../../../docs/design/s0-spike-criteria.md) and the
[neutral replay limitations](../replay/README.md).

Both required upstream MIT notices and the adaptation note are in [NOTICE](NOTICE).
