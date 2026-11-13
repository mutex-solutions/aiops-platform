# S0 hook error encoding — implementation-fidelity record

Recorded before changing native hook error representation or replay assertions.
This is implementation/codec work under the carried Event/Waterfall contract
(checklist row 18), **not** a new assertion exclusion or comparison normalization.
Frozen scripts, original assertions, traces and strict comparator remain unchanged.

## Observed mismatch retained

The initial real-engine replay matched complete D31/D33/D34/D35 traces on both
candidates. D32 retained a native joined string instead of source AggregateError
structure; D36 retained a different repeated-next sentinel string. Those original
strict failures remain failures. A test of the observed Go-shaped error is not
approval to waive either source assertion.

## Native representation and explicit encoding

- Listener attribution becomes an explicit `ListenerError` carrying plugin ID and
  exact cause, preserving diagnostic text and `errors.Is`/`errors.As` traversal.
- Parallel all-settled failure becomes a native `ParallelAggregateError` with
  ordered actual causes and `Unwrap() []error`. It is not inferred from arbitrary
  `errors.Join`; it preserves every member and order, including unknown errors.
- The repeated-next sentinel itself uses the source message
  `next() called multiple times`, retaining its Go identity and `errors.Is`.
- The fixture codec recognizes only these explicit native types, an explicit
  fixture-error origin, and the directly identified repeated-next sentinel.
  Transparent listener attribution remains separately retained in native
  diagnostics. No arbitrary wrapper traversal, substring rewriting, flattening,
  cause deletion or generic `errors.As` toward a convenient marker is allowed.
- Unknown errors/wrappers, panic payloads, cancellation/deadline and mixed generic
  joins remain their actual native diagnostics. They cannot become a fixture
  Error merely because one nested branch contains a known marker.

The source Error/AggregateError JSON representation is produced from actual
native semantics, not the script's expected payload. Source assertion exceptions
for D32/D36 are removed only after unchanged assertions and complete trace
comparison actually pass. All other behavior, complete event multiplicity/order,
lease ownership, audience and stage obligations remain unchanged.

## Required checks

Positive: ordered multiple failures with `errors.Is` identity, single-failure
aggregate identity, repeated-next sentinel and preserved attribution.
Negative: arbitrary wrappers; unknown aggregate members; panic containing a known
marker; generic joined fixture+cancellation; context errors. None may lose a cause
or become a false source match. Then run the same D31–D36 fixtures on both real
candidates and compare complete raw traces. Unit regressions alone do not establish
hook admission, async waterfall, observer containment or complete S0 acceptance.
