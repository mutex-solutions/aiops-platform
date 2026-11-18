# ADR-034: Kernel concurrency and self-disposal — initial S0 observation

- Status: **partially accepted, 2026-11-12: self-disposal request/completion semantics confirmed by the owner.** Concurrency core and line budget remain open.
- Date: 2026-11-12.
- No concurrency core or line budget has been selected by this document.

## Context

The accepted Go kernel must preserve the carried Cordis lifecycle properties,
including self-disposal from Apply, and add the Go lease/quiesce and concurrency
rules G1–G5. A transition must not wait for itself. The phrase “self-dispose from
Apply” needs an explicit request-versus-completion meaning before a Go API can
be judged faithful.

## Pinned oracle observation

Reference: Cordis 4.1.0 and loader 1.1.0, source-content pin harness
`fcc0ad462172522aaf5c93a7b2e01f6b9f0b0159`; isolated Node 24.22.1. The frozen
[criteria](../design/s0-spike-criteria.md) deliberately separate these cases.

- [D25 trace](../../test/differential/results/D25.trace.json): Apply requests
  disposal without awaiting it and returns. The fiber transitions LOADING ->
  UNLOADING -> DISPOSED, and the cleanup-complete mark is observed.
- [D26 trace](../../test/differential/results/D26.trace.json): Apply awaits its own
  disposal. At the 200 ms diagnostic bound it remains LOADING with inactive epoch
  and removed UID; neither the after-await mark nor cleanup completes. The runner
  records `SELF_DISPOSE_AWAIT_BLOCKED` rather than pretending successful settle.

Receipts are [D25](../../test/differential/results/D25.trace.json.run.json) and
[D26](../../test/differential/results/D26.trace.json.run.json). The same two cases
were replayed by the explicit pinned Node runner under `go test -race -count=3`;
the parser/comparator/runner tests and focused vet passed. This does not cover
any other differential scenario or the required property/performance corpus.

The diagnostic bound is not a Go performance threshold and the runner's exit 0
means it recorded the requested observation, not that awaited self-disposal is
supported. Source inspection explains a cycle: the loader awaits Apply, disposal
awaits the active transition, and Apply is awaiting disposal. No Go candidate or
final kernel has yet been compared against a complete differential corpus.

## Accepted clarification (owner confirmation, 2026-11-12)

Treat self-disposal **request acceptance** separately from **completion**.
Disposal requested from Apply or a callback queues the target change without
waiting for that same activation to finish. Apply returns; the runtime then
quiesces and disposes. Completion can be awaited externally after the callback
returns. A callback must not synchronously await completion of its own transition
or wait on a reload queue while holding its lease.

The owner confirmed this interpretation before API selection. It matches the
completed request-style behavior observed in D25, but is not permission to hide
D26: retain that diagnostic and state which forms the Go API rejects or exposes
as nonblocking requests. Completion inside Apply is not an accepted alternative.
A future change would require a deliberate adaptation, owner approval and a new
proof against deadlock.

## Accepted native effect-scope request/handle extension

The owner subsequently approved the concrete
[native effect-scope/handle contract](../design/s0-native-effect-scope-proposal.md),
selecting “Duyệt request/handle cùng lane” before implementation. Existing Effect,
Defer and Go remain. Scope admission and withdrawal are nonblocking requests;
setup, incremental rollback and cleanup execute in the fiber's one transition
lane. External AwaitSetup/Await observes actual completion, never an early
metadata deletion. Nested effect metadata, strict reverse single-shot cleanup,
error/panic rollback and Settle participation are required on both candidates.
This does not approve synchronous self-await, a provision-withdrawal capability,
a new deadline, a core selection or final SDK acceptance.

## Consequences of the partial decision

- Canonical self-disposal wording is clarified in the same change; no concurrency
  choice, deadline, threshold or frozen-criteria byte is changed.
- The one-reconciler and graph-lock candidates still require the full carried
  semantic corpus, property/race tests, 10,000-fiber comparison, 500-tree/cold-path
  evidence and complete final-port reruns.
- Enter lease ownership/cost, observer delivery, deadline behavior, selected
  concurrency core, actual kernel size and hard line budget remain undecided.
- A passing small lease-primitive test is not the kernel or either concurrency
  candidate. S0 is not complete.
