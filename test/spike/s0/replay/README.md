# Neutral candidate replay adapter — native observation integration

This test-only adapter executes script actions against both experimental scheduler
candidates. It never reads `Script.Expect` to produce responses. Assertions and
saved oracle traces are read only by independent tests, after real execution.
Every runtime is fresh and caller-owned; `Run` and `RunJSON` do not close it.

## Actual checkpoints, not inferred responses

A checkpoint requires the optional native `concurrency.Inspector`. Without it,
`snapshot`/`settle` returns explicit `ErrUnsupported`, including with `catch:true`;
no empty provision store or target epoch is fabricated. The adapter records the
single native inspection in `Result.RuntimeInspections`, retains committed epoch
and desired target separately, and projects:

- `FiberUID`: actual opaque runtime ID when `Alive`, otherwise JSON null, including
  requested disposal that is still `UNLOADING`.
- `Epoch`: actual `Target` revision/UID tuple, or null when native `Target` is nil.
  Neither lifecycle state, committed `Epoch`, nor a current read binding determines
  the target. A target/needs arity mismatch fails explicitly.
- Provisions: every native reservation in returned UID creation order, including
  inactive `LOADING` and retiring reservations that remain in the actual store.
  Provider name, UID, active flag and deep-cloned value are native observations;
  an expected response is never injected.
- Realm: `root:<key>` for this ordinary-string, root-only runtime binding. Keys are
  opaque strings; future context-model key encodings must supply a realm mapping,
  not silently inherit assumptions from this adapter.

Raw inspections, raw snapshots and projected provision values own separate mutable
containers. A real removal is projected as `$undefined`; service publication and
withdrawal notifications carry their actual value even during `UNLOADING` (state
is not a value decoder).

## Preserve needs declaration order

Use `RunJSON(ctx, runtime, originalBytes)` for source-order diagnostics. It first
calls the unchanged strict `differential.ParseScript`, then an isolated raw JSON
decoder preserves each plugin's `needs` object key order. No frozen protocol,
corpus, manifest or differential type changes are needed.

`Run(ctx, runtime, typedScript)` safely derives empty/single-key needs. Multi-key
maps have already lost declaration order: they execute in documented lexical
order for deterministic candidate diagnostics, but add explicit `needs order`
`ErrUnsupported` to `ComparisonError`. Reserializing a typed map cannot restore
source order; do not call that source parity.

## Honest comparison boundary

`Result.ComparisonError == nil` means the current script has complete native
observations eligible for comparison; it does **not** mean the trace matched.
Raw/effective configuration now comes from the runtime's real configuration cell,
and its actual Resolve/Commit/Update callbacks produce corresponding events.
Successful Apply-returned cleanup is owned natively after body effects. Missing
contexts, hooks or unsupported operations still fail explicitly rather than
fabricating a successful gate. Passing checkpoint assertions is not ordered
Cordis equivalence; only the strict complete-trace comparison establishes a match.

Fixture events are recorded synchronously through optional test-only `Recorder`
instrumentation into the same actual state/service ledger. Every raw scheduler
event remains in `RuntimeEvents`. Root output/marks and Apply/cleanup bodies thus
have an actual measurable linearization, not an order reconstructed from expected
output. This does not prove G5 production observer delivery or safety.

`TestActualGoldenTraceDiagnostics` executes D10, D12, D14–D19, D25, D27, D28,
D30 and explicit D13 revision 2 against both cores, then calls the strict
`differential.Compare` on the **complete actual trace**. Set `S0_COMPARE_RUN=1`
to make every semantic mismatch fail the test; a diagnostics-only invocation
records mismatches but is never a parity-pass claim. Errors,
extra transitions, service notifications, restarts and arrays are not dropped or
sorted to make that comparison green. It validates script/trace hashes against
the recorded golden provenance, logs the first actual event difference and saves
full actual trace, full comparison diagnostic, raw events/inspections and source
hashes in test temporary directories. For persistent evidence outside the frozen
corpus, explicitly choose an output directory:

```sh
S0_COMPARE_RUN=1 S0_REPLAY_EVIDENCE_DIR=/tmp/s0-replay-evidence \
  go test ./test/spike/s0/replay -run TestActualGoldenTraceDiagnostics -count=1 -v
go test ./test/spike/s0/replay -count=1
go test -race ./test/spike/s0/replay -count=1
```

The adapter uses native `Spec.ApplyEffect`: a successful body returns its real
plugin disposer, which the scheduler owns **after** body provision/effect
registrations, including when Apply requested self-disposal. It does not pre-own
a guarded slot or call a fake late `Context.Defer`. The loading/retiring inspection
regression requires a real provision to remain at the plugin teardown gate, then
disappear after actual removal. This catches the earlier pre-owned-slot ordering
counterexample instead of concealing it with a fabricated reservation. Config
hooks and remaining ordering mismatches must likewise fix actual execution, not
checkpoint projection.

The recorded initial integration failure and subsequent fixes are summarized in
[the result record](<../results/scheduler-integration-initial.md>). D13's original
implicit scheduling assumption requires an explicit versioned causal barrier;
[revision 2](<../../../../docs/design/s0-corpus-revision-2.md>) preserves its first
script and failure instead of silently substituting a candidate-only Await.

## Verified causal sidecars and host hooks

The owner-approved [ADR-035 amendment](../../../../docs/design/s0-causal-comparison-amendment.md)
adds a separate causal comparator; strict diagnostics are unchanged. Checkpoint
cuts use the actual native Inspect watermark and exact-byte script/trace hashes.
Missing control/independence proof is unsupported, not a match. Adversarial tests
reject identical malformed streams, including early Apply, omitted restart,
premature provider removal and inconsistent provision stores. Fresh pinned-Node
integration for D02/D10/D12/D16 now passes on both candidates with complete raw
trace/sidecar/provenance retention. This is a supported subset, not all scenarios.

Host hook replay installs one actual builtin bundle on the native root effect
ledger—no hidden provider fiber or provision-store row. D31–D36 execute against
both real hook engines; unchanged source assertions and complete strict traces
match in the retained typed-error run. Parallel aggregates contain ordered actual
causes; listener attribution is typed; only explicit native provenance is encoded.
Unknown wrappers, panic/cancellation and arbitrary joins are never string-stripped
into fixture errors. [The method record](../../../../docs/design/s0-hook-error-encoding.md)
precedes the change. Earlier D32/D36 strict failures remain retained, not relabelled.
Hook classes/declaration admission and detached async waterfall remain gaps.

[Selected semantic evidence](../results/semantic-integration-typed-errors/receipt.json)
records exact scope. Native observation integrity, unsupported-operation refusal,
source needs order, expectation noninterference, immutable per-activation cleanup
config and recorder order are tested. Full 36-case carried/adapted comparison,
scale/cold-path sampling and final kernel remain pending. There is no S0
completion or selected-core claim in this package.
