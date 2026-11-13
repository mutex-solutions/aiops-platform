# S0 comparison amendment — verified causal equivalence

Status: owner-approved methodology, implementation and verification pending.
Approval: the owner selected **“Duyệt causal comparison có kiểm chứng”** in the
ADR-035 confirmation during the resumed S0 session. This approval does not select
an implementation, waive a failed gate or approve any SQL-authority mechanism.

This amendment is recorded **before the new comparator is implemented/tested**.
The original criteria, pin receipt, scripts, traces and strict comparator remain
unchanged. Historical failures keep their original classifications. The original
criteria SHA-256 remains
`628bd6ec24e5983bfbe9fcdada096eaa84db3e95227f11e8fd5a7fa33a26f19c`.

## Narrow change

Alongside strict total-order diagnostics, compare equivalent interleavings only
where independence can be demonstrated from the identical executed script and
actual observation witnesses. Keep all original ordered raw event records. No
sorting/deleting events, stripping error/state data, changing payloads, dropping
scenarios, final-state-only rewriting or global callback serialization is allowed.

All event content and multiplicity, per-fiber order, dependency conflicts,
operation outcomes, error diagnostics, state/epoch/config relationships,
provider ownership and explicit command/gate/task/checkpoint causality remain
obligations. A different fiber is not sufficient proof of independence. Missing
proof is an unsupported comparison, not a match.

## Version 1 execution/witness contract

`CompareCausal(scriptJSON, expectedTraceJSON, actualTraceJSON, expectedWitness,
actualWitness)` is a separate API. Existing `Compare` remains strict and its
outcome is retained separately. Neither function reads script assertions to
manufacture actual execution.

A sidecar `s0-causal-witness/v1` binds the exact script and raw trace bytes by
SHA-256 and records each **actual** checkpoint position:

```json
{
  "schema": "s0-causal-witness/v1",
  "scriptSHA256": "<hex>",
  "traceSHA256": "<hex>",
  "checkpoints": [{ "label": "before", "afterEvent": 23 }]
}
```

Positions are observed by the replay engine when it records the checkpoint, not
reconstructed from a golden trace or inferred from desired state. Both streams
must validate consecutive sequence counters, cut bounds/labels/order and hashes.
The current trace v1 has no inline checkpoint marker; absent sidecars therefore
cannot establish these barriers and must not silently gain causal acceptance.
Sidecars contain no semantic event replacements; raw trace schema stays v1.

Version 1 begins conservatively with statically provable dependency/root-command
scenarios. It compiles provider/consumer conflicts and output-cell reads/writes
from actual script actions. It preserves conflicts even when two actors share a
resource through different paths. Gate/mark/continuation/task, dynamic metadata,
availability and child semantics require sufficient explicit witnesses before
support is claimed; they must not be treated as independent just because the
initial static model cannot express them.

Checkpoint equality preserves states, raw/effective values, outputs and global
bijective identities. Provision-array interleavings may differ only for
independent provider creations, with the same key/realm/owner/value/active and
identity relationships; no blanket set comparison or payload UID rewriting.

## Validation before using a match as evidence

Adversarial tests must reject at least:

- provider/dependent event reversal or early provider removal;
- event loss, extras, duplication, illegal restart/state transition and changed
  config/output/error content;
- stale/wrong epoch binding, UID aliasing/reuse and cross-realm substitution;
- a shared output-cell conflict mislabelled as independent;
- missing/forged hashes, checkpoint cuts or unsupported script operations;
- user payload changes even when fields are named events/timestamp/provisionUID.

A positive test must accept only a justified independent-branch interleaving.
Run the same real scripts on the pinned oracle and both actual Go candidates;
report strict and causal outcomes separately. Method/sidecar/compiler/source
identities join the run receipt. Negative unit tests alone are not candidate
parity, G5 delivery or final-port acceptance.

## Unchanged gates

No numerical budget changes: workload counts, native lifecycle deadlines,
100-percent per-production-file coverage, physical-line gate, full property,
scale/cold path, licence/provenance, SQL authority/atomicity and restart tests
remain. All carried behaviors and per-assertion adaptations remain required.
The private repository stays private, without CI/CD or S1 work. See
[ADR-035](../adr/ADR-035-cordis-tracking.md) for scope and limits.
