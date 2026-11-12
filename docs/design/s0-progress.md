# S0 progress and evidence boundaries

This is an interim engineering status, not S0 acceptance. The original
[criteria](s0-spike-criteria.md) and [receipt](s0-criteria-freeze.json) remain
byte-identical. No S1 product work is authorized or implemented.

## Owner decisions after the initial findings

- ADR-034 is partially accepted: self-disposal in Apply/callbacks is a request;
  completion is awaited outside the callback. Concurrency core and line budget
  are still open. The canonical record was clarified in the same uncommitted
  change as the ADR.
- ADR-045 option A is approved for additional bounded evidence while preserving
  all original guarantees. No final SQL mechanism or weaker contract is accepted.
- The owner subsequently created the private repository
  `mutex-solutions/aiops-platform`, approved the reviewed partial S0 checkpoint on
  private `main`, and selected `github.com/mutex-solutions/aiops-platform` as the
  current module prefix (ADR-057). No public disclosure, visibility change or
  CI/CD setup is authorized. L14 remains open. Frozen criteria/pins/receipts keep
  their historical bytes; they are not silently renamed. The old namespace may
  therefore remain in frozen historical records, but no current first-party Go
  module or import uses it. After renaming, focused vet and explicit live-oracle
  `go test -race -count=3` passed for differential, concurrency, partial replay
  and context metadata packages. SQL gate failures were not converted into
  passes, and the excluded capability draft was not executed.

## Evidence obtained

- The minimal raw-SQL/session-membership/mutable-GUC boundary gate fails. Own-scope,
  missing-scope, rollback and pool-reuse controls pass, and direct foreign-schema
  write is denied. Positive role/scope escapes remain actual failures, not an
  expected-success test that passes the design. See the
  [result index](../../test/spike/s0/results/README.md).
- The ordinary-authority follow-up reproduces three limitations: custom USERSET
  values remain writable after REVOKE SET; SECURITY DEFINER cannot SET ROLE;
  removing SET membership denies trusted and caller call sites equally. A green
  diagnostic runner means the limitations were recorded, not that SQL passed.
- Pinned Cordis has a recorded 36-script corpus: 35 ordinary observations and the
  retained D26 diagnostic. Explicit live replay under the race-instrumented Go
  runner passed three repetitions; the comparator preserves actual event order.
  D25/D26 distinguish request-style self-disposal from awaiting one's own
  transition. These are oracle/reference observations, **not Go kernel parity**.
  The [differential README](../../test/differential/README.md) and its freeze
  manifest identify the exact scripts, failed initial fixture controls and
  authoritative receipts. D13 subsequently exposed an implicit JavaScript
  scheduling assumption in the Go adapter; an explicit version-2 `u-ready`
  barrier was planned before new replay, with v1 preserved. The same revised
  script passed three oracle repetitions with unchanged six-state assertions;
  this is not a hidden Go-only wait or a Go-equivalence claim.
- Both experimental schedulers have lifecycle/availability implementations and
  scoped deterministic race tests. Common tests check revision, retained
  injection, lease quiescence, consumer-before-provider cleanup and readonly
  consumer-bound availability notifications. A shared test-only event recorder
  and fiber-scoped Await are being integrated with the neutral replay adapter;
  its partial observations explicitly refuse a full equivalence claim while
  provision inspection, target epochs and config/realm/hook APIs are incomplete.
  A multi-disposer expired-deadline edge was patched with a virtual-time
  regression; candidate tests and focused vet passed after that narrow fix.
  Combined focused `go vet` and explicit live-oracle plus both candidate/replay
  packages under `go test -race -count=3` subsequently passed. The replay adapter
  now tests target-specific retained-error identity and explicitly selects the
  D13-v2 script on both cores. Its comparison status still refuses full equality
  because the listed metadata/native APIs are absent. No numeric scale/latency
  acceptance or final-kernel parity follows.
- A separate standard-library-only context/key metadata prototype has tests for
  immutable derivation, name/major/realm identity, entry-bound authorization,
  inherited consumer checks, intercept order, optional-resolution refusal and
  scope-writer/audience data. It explicitly does not prove ACTIVE/effect ownership
  or integration with either runtime. Final focused vet and explicit live-oracle
  race tests across differential, both candidates, partial replay and metadata
  packages passed three repetitions. SQL packages were not included in that pass;
  the original SQL gate remains failed and the capability artifact unexecuted.

## Tool-policy interruption

A pre-execution tool review rejected a later shell request that would check the
new capability protocol's hash and original receipt. Its body did not run. That
operation was not retried through another command, tool or child. The capability
probe remains **not executed** and has no experimental outcome. The candidate
source was handed off **not formatted or compiled**; it is an unvalidated local
artifact, not a result. That source and its draft protocol are excluded from the
reviewed private checkpoint. No protocol waives review controls or authorizes
bypassing a tool restriction. Independent nonblocked kernel work can continue.

## Still required

- Validate any new SQL authority mechanism against the original negative tests,
  then caller-Tx InsertTx/state/outbox/audit atomicity, role cost, background-worker
  identity and under-load resource restarts. No such result exists yet.
- Complete both candidates against the whole carried semantic corpus and prove
  accepted Go additions separately: immutable context/realms/intercepts,
  availability, optional/child ownership, hooks/audiences/bindings/scopes,
  tracked goroutines, Batch/Settle and deadline behavior.
- Run the full property workload, 10,000-fiber/500-tree/cold-ack and call-path
  measurements on eligible runs, then choose by correctness first.
- Implement the final SDK kernel and kerneltest, rerun all affected gates,
  vet/race/per-file coverage, and fix the approved physical line budget.

No partial pass, skipped integration run or test-only prototype is reported as
completion of S0.
