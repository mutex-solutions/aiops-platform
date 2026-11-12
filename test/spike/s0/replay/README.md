# Neutral candidate replay adapter — partial integration

This test-only adapter executes actual script actions against both experimental
scheduler candidates. It does not read Script.Expect to produce responses;
expectations are evaluated independently afterward. Each Runtime is real and
fresh, not a fake host.

The adapter explicitly returns `ErrUnsupported` in Result.ComparisonError while
provision-store inspection, target epoch/config ownership, contexts and hook
APIs are incomplete. A valid partial trace and passing checkpoint assertions
are not ordered Cordis equivalence. A strict comparator test deliberately shows
the remaining D14 mismatch; no status sorting, dropped restart or invented
provision snapshot is used to turn it green.

Fixture events are recorded synchronously through optional test-only Recorder
instrumentation into the same actual state/service event ledger. Every raw
scheduler event remains available as RuntimeEvents. Root output/marks and
Apply/cleanup bodies therefore have a measurable event linearization rather than
being reconstructed after the fact from expected output. This does not by itself
prove G5 production observer delivery or safety.

The recorded initial integration failure and subsequent fixes are summarized in
[the result record](../results/scheduler-integration-initial.md). D13's original
implicit scheduling assumption requires an explicit versioned causal barrier;
[revision 2](../../../../docs/design/s0-corpus-revision-2.md) preserves its first
script and failure rather than hiding a candidate-only Await.

Snapshot assertions on the presently supported lifecycle scripts, refusal of
unsupported operations, expectation noninterference, immutable per-activation
cleanup config and raw recorder ownership/order are tested. Full 36-case Go
comparison, property/scale/cold-path acceptance and final kernel remain pending.
There is no S0 completion or selected-core claim in this package.
