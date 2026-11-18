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

## 2026-11-18 call-path lease: wake rule and striped counter

Cause of the first full callpath run missing section 8.3 at 8 and 32 callers: every
Enter/Leave wrote one shared counter and every Leave called the scheduler wake
(`leaseChanged`), even with nobody draining.

- Ownership: a successful `Enter` is paired with exactly one `Leave` by the same
  logical operation (Effect/Go/tracked work, composition and hook dispatch). Enter
  refuses once `live` is false and rolls back its own increment.
- Wake rule: Enter and Leave decrement or increment first and read `live` second.
  `leaseChanged` is called only when `live` is already false (a drainer exists).
  Drainers clear `live` and only then read the lease sum, so either the drainer
  sees the decrement or the Leave sees `live=false` and wakes it; the last Leave
  cannot be missed, and a live activation's Enter/Leave makes no wake call. The
  full interleaving argument is the comment on `Context.Enter` in
  `test/spike/s0/concurrency/types.go`, shared by both cores (graph-lock waits on a
  sticky one-slot channel; the reconciler owner re-checks `live` and the sum).
- Counter: 16 cache-line-padded shards chosen from the caller's stack address (no
  allocation, lock or syscall), summed by the drainer after `live=false`. A Leave
  may hit another shard than its Enter, so single shards can be negative; only the
  sum is read. After `live=false` only decrements and rolled-back pairs occur, so a
  sum is never below the true open count and a zero sum proves no lease is open.
  Padding alone (tried first) did not fix graph-lock; the wake-rule change alone
  did not move either core. Cost: about 1 KiB per activation.
- Development-host measurement (shared, busy machine, GOMAXPROCS=8, not acceptance;
  `BenchmarkEnterLeave`, medians of sampled single Enter+Leave latency, 0 allocs):

  | callers | p50 before | p99 before | p50 after | p99 after |
  |---|---|---|---|---|
  | 1 | ~50 ns | 60-100 ns | ~50 ns | ~80 ns |
  | 8 | ~800 ns | ~2.6-2.7 us | ~50 ns | 90-150 ns |
  | 32 | ~800 ns | ~2.8 us | ~50 ns | ~470-500 ns |

  The acceptance decision remains the full frozen protocol on the idle host.

## 2026-11-19 teardown order of a parent and its children, carried from the oracle

Owner decision (2026-11-18): where a Go candidate differs from the oracle it is
fixed to match. Both candidates used to quiesce and join a parent's children before
any of the parent's ordinary disposers. The oracle (vendored Cordis,
`Fiber` in `fiber.ts`) does not: a child's disposer is one effect on its parent
(`parent.fiber.effect(...)`), so a parent unload disposes it like any other effect.

Order now implemented by both `reconciler` and `graph-lock`, and the oracle's:

1. The parent is disposed (`plugin disposed parent`) and enters UNLOADING. Its
   context stops accepting new children, effects and leases.
2. The parent first waits for its service dependants (a provider keeps its
   implementation until every fiber bound to it has finished unloading). This is
   the unchanged service axis; a child that is a service dependant leaves here,
   through the withdrawal, not through a disposal request.
3. The parent's effects are disposed in reverse registration order. The child is
   one entry of that list at the slot where it was mounted. Reaching it only
   requests its disposal (`plugin disposed child`, child UNLOADING, its own
   cleanup) and does not wait for it. An effect registered after the child
   (including the Apply-returned disposer) therefore runs before the child is
   touched; an effect registered before it runs right after the request, while the
   child is still unloading. Parent disposers stay sequential in both candidates.
4. After the walk the parent waits until every child is DISPOSED, then it is
   DISPOSED itself. A parent is never DISPOSED before its children, and no disposer
   runs twice. Deadline diagnostics are unchanged: the join has its own bounded
   wait, and an ignored child cleanup is reported on the child.

Consequences for plugin authors (carried semantics, not guarded):

- A child can still be ACTIVE, with a live context, running goroutines and
  accepting new effects, children and leases, while its parent's cleanup runs.
  Nothing cancels the child before its slot in the parent's walk is reached.
- A resource a parent registered after a child (or returned from Apply) is
  released before the child is told to stop. A child must not rely on such a
  resource, or on one registered before the child, being open during its own
  cleanup: the child can run its disposer after, or concurrently with, the
  parent's. A resource the child needs must be a service the child injects
  (retained until the child has unloaded) or be owned by the child itself.
- The parent's own context refuses new children and effects from the moment it is
  disposed; a child's context does so only from the moment its disposal is
  requested. A child may therefore start new work in the window between the
  parent's UNLOADING and the child's slot, as in the oracle; that work is cleaned
  by the child's own unload and joined before the parent is DISPOSED.
- Reads of an inherited dependency keep working during that window and the
  child's own teardown: the declaring ancestor's binding is retained until that
  ancestor is released, which now follows the join of its children.

Not changed: sequential disposers within one parent (the oracle starts the
top-level disposers of a fiber concurrently, one microtask apart), the service
dependency retention rules, lease drain, and deadline budgets. Evidence:
`test/spike/s0/results/causal-d22-d06-fixes` and the D22 row of
`docs/design/s0-causal-coverage.md`.
