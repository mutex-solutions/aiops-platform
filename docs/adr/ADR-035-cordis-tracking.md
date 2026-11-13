# ADR-035: Cordis source pin and parallel trace comparison

- Status: **causal-comparison methodology owner-approved; implementation and final S0 ratification pending**.
- Approval: the owner explicitly selected “Duyệt causal comparison có kiểm chứng”
  in the resumed S0 confirmation. The preexecution
  [amendment](../design/s0-causal-comparison-amendment.md) records its narrow scope.
- Reference: Cordis 4.1.0, loader 1.1.0, source-content pin harness
  `fcc0ad462172522aaf5c93a7b2e01f6b9f0b0159`.
- Scope: S0 oracle provenance and comparison methodology; no core or final SDK
  is selected by this record.

## Context and verified evidence

The exact reference packages, their source-content match, complete notices and
isolated Node runtime are recorded in [S0 pins](../design/s0-reference-pins.md).
The oracle corpus and its original/revised fixtures retain their raw traces,
receipts, source hashes and failed initial controls. Package byte identity is
verified; signed build provenance and a unique historical publisher commit are
not claimed. Re-sync remains a separate ADR followed by affected differential
reruns.

The [frozen criteria](../design/s0-spike-criteria.md) require identical scripts,
settled states, revision/provision identity relationships and ordered status/
service traces. The current strict comparator keeps every event and array order;
it normalizes only the declared opaque identities and built-in key rename.

Current Go candidates preserve an actual total event order but need not schedule
independent callbacks in the same order as a single JavaScript microtask queue.
The accepted checklist explicitly does not carry JavaScript run-to-completion
(property 22), while G5 still requires each implementation's true total observer
order. Strict mismatches are retained, not called passes merely because this
reason is understood.

Examples from recorded diagnostic runs:

- D02 mounts independent providers in separate realms. Config/Apply completion
  and provision-creation order can differ while both isolated values remain right.
- D16's single root change restarts two independent diamond branches. Both target
  epochs may be right while the branches' config/Apply prefixes reverse. A gate
  inside Apply cannot control a prefix emitted before that gate.
- D14/D15 once exposed a real dependency-order defect: provider removal overtook
  the consumer's final stable transition. The native implementation was corrected
  and affected strict cases rerun. This is not an independent-interleaving waiver.
- D04 duplicate-provision errors and D20 cancellation-versus-deliberate-error
  events remain distinct diagnostic questions. A causal method must not erase or
  normalize those failures automatically.

## Decision requested

### 1. Preserve strict comparison and bound fixture-only repairs

Record explicit, identical causal controls before revised fixture execution;
preserve earlier bytes and failures. A D02 revision can join/check its first
provider before mounting the second. D13 needs an explicit statement of which
six-state path remains under test. D16 may gate branch work, but must retain the
same diamond, single root update and all transition/epoch obligations. Do not
add dependencies, split that update or insert hidden adapter waits solely to
force one runtime's scheduler order. If prefixes remain unconstrained, report
that failure rather than claiming the fixture repair is sufficient.

This route leaves the frozen comparator unchanged and cannot establish a pass
for all parallel interleavings by itself.

### 2. Proposed owner-approved causal comparison for independent interleavings

If the owner accepts this methodology, add a **separate, explicit** causal
comparison alongside strict diagnostics, with a recorded protocol amendment and
new evidence identity before execution. Keep complete, totally ordered raw traces
from both implementations. Never replace them with sorted/filtered evidence.

The comparison would require:

1. Equal event content/multiplicity and operation outcomes, plus bijective opaque
   provision/fiber identities. Errors, values and startup/restart boundaries are
   not normalized simply because messages or schedules differ.
2. Exact per-fiber lifecycle/body/config/update/cleanup order. A dependent's read
   must reference the actually captured provision identity, not another equal
   value or a later epoch.
3. All dependency-causal edges: publication before dependent activation/read;
   invalidation before refusing new work; leases/tracked work/children drained
   before effects release; dependent teardown before provider removal; and the
   accepted status-before-service-notification relationships.
4. Explicit fixture-command, gate/mark, continuation and task-join edges, including
   root update causality and single-shot/no-double-transition obligations.
5. Equal settled states, desired epochs, retained bindings where observed, raw/
   effective config identity obligations and resource/effect ledgers. Independent
   reservation-array order may differ only if identity/address ownership is equal
   under the same justified independence relation, never by a blanket set rewrite.
6. A mismatch whenever an edge or identity relation cannot be proved. Independence
   is narrow and demonstrable; “different fibers” alone is not independence.

Strict total-order comparison remains available and its result is reported
separately. G5 still requires each raw observer stream to have one lossless total
order; causal equivalence does not excuse missing/reordered delivery within a
runtime. Tests must reject dependency reordering, dropped/extra/duplicate events,
epoch reuse, cancelled-versus-explicit failure substitution and wrong checkpoint
values before the new method can be trusted.

**This method is owner-approved, but not yet implemented/verified or an S0 pass.**
The linked preexecution amendment and canonical reference record that approval;
original frozen criteria bytes and strict comparator are retained. Neither useful
partial results nor schedule/implementation cost supplies broader permission.

### 3. Do not serialize the whole kernel merely to mimic JavaScript

A global callback serialization/stop-the-world replacement would be an
architectural change, not fixture bookkeeping. It is not selected by this ADR.
Both candidate concurrency designs, actual parallel execution, G1–G5 and the
full scale/call-path evidence remain required.

## Consequences while proposed

- Original strict mismatches stay strict failures; causal results are separately
  labelled only after the approved method is implemented and negatively tested.
  Original criteria bytes remain immutable; the linked amendment records the
  owner's explicitly approved methodological extension.
- Independent correctness/property/observer/scale-harness work may continue,
  with actual scope and not-run gates reported truthfully.
- Oracle tests, property tests, vet/race and local smoke are separate from full
  final-port differential/coverage/performance acceptance.
- The final kernel still requires all carried behaviors and explicit tests for
  accepted adaptations/additions, not whole-scenario exclusions.
