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

## Second full callpath run and what it cannot yet decide, 2026-11-19

The [second full run](../../test/spike/s0/results/callpath-full-second/invocation.json)
on an idle dedicated host, with the striped lease counter, met the latency bound
in every run: owner Enter+Leave p99 was 89 to 840 ns in aggregate at 1, 8 and 32
callers on both candidates, worst single run 1661 ns, against 2000 ns. The run is
still classified failed and invalid, for two reasons that an analysis on a
development host attributes to the measurement, not to the lease. Neither the
harness nor the criteria were changed.

- **One object in 4 of 60 runs.** The harness requires a process-wide allocation
  delta of exactly zero across each window. Replicating the window 1,500 times per
  configuration showed the empty timer-only control loop records a non-zero delta
  as often as the lease loop (for example 164 against 168 windows at 32 callers on
  one candidate). A heap-profile diff attributes the object to the wait-queue
  record the coordinator goroutine allocates when it parks to wait for the callers
  to finish. With that wait replaced by a non-parking one, non-zero windows fell
  to zero in 10 of 12 configurations. The lease path itself allocates nothing.
- **Throughput variation above 10% at 8 and 32 callers.** The timer-only control,
  which touches no lease, shows the same variation in both full runs (12 to 18% at
  8 callers). A window of 100,000 operations lasts a few milliseconds; callers
  start through a channel close and finish at different times, and throughput
  follows that finish skew (correlation about -0.8) in every variant including the
  control. Shard collisions correlate weakly with throughput in this window shape
  (-0.19 at 8 callers). No lease design can bring this metric under 10% with the
  present window.

A 64-shard counter was evaluated: it lowers p99 further and makes collisions
rarer, but costs about 4 KiB per activation (about 40 MiB at 10,000 activations,
against a 128 MiB retained-heap limit) and does not change either finding above.
The counter stays at 16 shards.

Open for the owner: whether the callpath protocol is amended, in writing and
before another run, so that the coordinator does not allocate inside the window
and the noise check uses a window in which callers start together and throughput
is taken from the callers' own timestamps. Until then the call-path gate is
recorded as: latency bound met, allocation and noise readings unresolved.

## 2026-11-19 bounded history and key-indexed service holds

The scale harness found two candidate defects. Both are fixed in both candidates
(`test/spike/s0/concurrency`); the criteria, thresholds and the differential and
property suites are unchanged. All numbers below are development-host
observations on a shared machine, not acceptance evidence.

### Retention contract (`retention.go`)

Fiber IDs come from one counter that only grows; an ID is never reused (checked by
`TestBoundedArchiveAndExpiredIDContract`). Hence "issued and not live" is
decidable without a stored record. After a fiber incarnation is DISPOSED and its
teardown is joined, the runtime keeps:

| What | Bound | Default | Where it is read |
|---|---|---|---|
| Archive of recently disposed incarnations (DISPOSED snapshot, formatted-on-read diagnostic, retained startup error) | `Retention.ArchiveLimit` records, oldest disposed evicted first | 1024 | `Snapshots`, `Inspect`, `AwaitFiber`, `RetentionStatus` |
| Tail of the native event ledger | `Retention.EventWindow` events | 4096 (512 KiB) | `Events`, `RetentionStatus` |
| Complete event history | only when asked: `Retention.RecordLimit` events | 0 (off); `Recording()` = 1,048,576 | `Events`, `RetentionStatus` |
| Diagnostics of past failed incarnations that every later `Settle`/`Close` still reports (reconciler only) | 64 (`rcTerminalLimit`), then a count in the error and in `RetentionStatus.TerminalDropped` | 64 | `Settle`, `Close` |

This follows the oracle: a disposed Cordis fiber leaves the registry
(`remove()` in the dispose effect) and is garbage once unreferenced; its state and
error stay reachable only through a reference the caller holds (`Fiber.await()`
throws `_error`). Here the ID is the handle, a bounded archive stands in for "still
referenced by a recent caller", and a waiter that is already waiting does hold the
exact record: a waiter registered before disposal receives the retained startup
error even when the archive evicted its record before the waiter was served
(`TestInFlightWaiterSurvivesArchiveEviction`).

Contract for an ID whose record was evicted (an expired ID), in types and doc
comments (`ErrExpired`, `retention.go`):

- `AwaitFiber` returns `ErrExpired`. The startup error is no longer known, so
  success must not be claimed; the error is distinct from `ErrUnknown`.
- `Dispose` is the same idempotent no-op as for an archived ID. A configuration
  request behaves as for any disposed ID (unchanged between archived and expired).
  `Update` on a non-live ID is `ErrUnknown` as before.
- The fiber is absent from `Snapshots` and `Inspect`: those list live fibers plus
  the retained archive, which holds at most `ArchiveLimit` DISPOSED rows.
- An ID that was never issued (above the last issued, or 0, the root) stays
  `ErrUnknown`.

Event ledger. Nothing outside the runtime needs the runtime to hold complete
history by default: the replay interpreter, the differential receipts and the
property model compare the whole ledger and the observer and composition suites
read it, so they opt in with `NewRecordingReconciler` / `NewRecordingGraphLock`
(`Recording()`); the scale harness opts in per fixture (`harnessRetention`). The
recorder is appended in the same linearization as the state change (under the
graph lock, or on the owner), with no callback and no blocking. When the cap is
reached the recorder fails and stays failed: `Events` returns
`ErrRecorderOverflow` instead of a partial list, `Close` joins it,
`RetentionStatus` reports the overflow and the number of events not recorded, and
the runtime keeps operating. No event is silently dropped for a recorder; the
observer queue still receives every event (`TestRecorderOverflowIsExplicit`). Without
a recorder, `Events` returns the tail with `ErrEventsTruncated` once older events
were dropped, never a silent partial list. Both candidates expose
`RetentionStatus()` (`RetentionReporter`), read lock-free, for the harness.

Not bounded by this change: the observer dispatcher queue. A stalled observer still
retains accepted events (documented in `observer.go`, unchanged).

### Held service events and pending loads, indexed by key

A provider's `service` event is held until the pre-checks of its dependants have
been applied, and a dependant's load worker waits for the held events of its needs
(oracle scenarios D09 and D14). That order is unchanged. The bookkeeping used to
rescan every pending load against every held event on every composed pre-check
completion, in `graph-lock` (`startLoadsLocked`) and, after every owner message, in
the reconciler (`startLoads`). Now, in both:

- held events are counted per key (`heldSvc`), and a held event counts the checks it
  still waits for (`pending`) instead of rescanning its wait list;
- pending loads are indexed by every key they need (`loadsByKey`);
- a key release (a held event emitted, or a key leaving the `waking`/`notifying`
  set) examines only the loads indexed under that key and starts, in insertion
  order, those whose needed keys are all free. A load becomes startable only when a
  key it needs stops being held, so no other load is examined.

`TestHeldServiceIndexConsistency` drives randomised provide/restart, check
completion, dispose, update and notify interleavings on both candidates and checks
that no activation's load runs twice, no load starts before the service event of the
provision it binds is in the ledger, every consumer that can be ACTIVE is, and no
index entry survives quiescence (mutation-checked: dropping a release hangs it).

The graph-lock mount growth of about n^1.5 on chains, present before the held-event
commits, was the `Settle` predicate: it scanned every fiber under the graph lock
on every wake-up, and every appended event wakes the waiter, so mounting n fibers
cost about n times the number of events. `Settle` now reads a counter of busy
fibers kept at the two places a fiber's worker flag or state changes.

### Measurements (development host, not acceptance)

Settle time of 10,000 composed realm fibers, best of three (ms):

| Providers | reconciler before | reconciler after | graph-lock before | graph-lock after |
|---|---|---|---|---|
| 10 | 930 | 354 | 518 | 339 |
| 100 | 1209 | 364 | 978 | 347 |
| 1,000 | 395 (invalid: 2 s check deadlines failed fibers) | 303 | 2538 | 323 |

Retained heap above the shared-only baseline after each mount-all, 10 cycles of 500
four-fiber trees, candidate and fixture only (MiB):

| | cycle 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9 | 10 |
|---|---|---|---|---|---|---|---|---|---|---|
| reconciler before | 11.3 | 18.4 | 23.0 | 28.9 | 33.6 | 39.2 | 45.9 | 46.5 | 54.9 | 55.5 |
| reconciler after | 10.7 | 12.4 | 12.5 | 12.5 | 12.7 | 12.6 | 12.7 | 12.7 | 12.7 | 12.8 |
| graph-lock before | 11.6 | 18.7 | 24.0 | 31.7 | 38.5 | 45.7 | 52.6 | 62.8 | 63.6 | 76.5 |
| graph-lock after | 10.6 | 11.3 | 11.4 | 12.5 | 11.3 | 11.4 | 11.4 | 11.4 | 11.4 | 11.4 |

The harness's own sampled run also kept its trace in the measured heap: each
sample held the raw dump of every goroutine twice, and the per-owner counts pinned
the whole dump through string slicing, which dominated the growth after the
candidate fix. The samples now keep counts only (retained, disposed and closed
checkpoints keep raw stacks). A sampled `RunCycles` run of the same 500 trees then
reads 11.6 to 21.8 MiB (reconciler) and 11.5 to 20.8 MiB (graph-lock) over ten
cycles; the remaining slope is the harness's own per-sample records (about 500
samples per cycle), not the candidates.

Growth curves, one root per fixture and process (`TestDevObserve`), milliseconds at
the fiber counts in the first column. Before: the reconciler's 8k and 10k realm runs
failed with 2 s check deadlines (its 10k restart did not run); the dash marks that.
After, every curve is linear: doubling the fibers about doubles the time, including
graph-lock realms (323 to 407 ms from 8k to 10k) and graph-lock chains.

| Shape (ms at 1k/2k/4k/8k/10k fibers) | Operation | Candidate | Before | After |
|---|---|---|---|---|
| chain (1k / 2k / 4k / 8k / 10k) | mount-settle | reconciler | 39 / 69 / 156 / 284 / 397 | 39 / 71 / 138 / 280 / 356 |
| chain (1k / 2k / 4k / 8k / 10k) | mount-settle | graph-lock | 42 / 96 / 237 / 770 / 1041 | 37 / 68 / 136 / 268 / 352 |
| chain (1k / 2k / 4k / 8k / 10k) | root-restart-settle | reconciler | 55 / 119 / 228 / 464 / 564 | 55 / 113 / 221 / 465 / 535 |
| chain (1k / 2k / 4k / 8k / 10k) | root-restart-settle | graph-lock | 56 / 133 / 346 / 957 / 1282 | 53 / 108 / 236 / 462 / 566 |
| fanout (1k / 2k / 4k / 8k / 10k) | mount-settle | reconciler | 28 / 47 / 97 / 219 / 291 | 26 / 52 / 117 / 230 / 269 |
| fanout (1k / 2k / 4k / 8k / 10k) | mount-settle | graph-lock | 27 / 51 / 98 / 192 / 245 | 25 / 51 / 94 / 214 / 233 |
| diamonds (1k / 4k / 10k) | mount-settle | reconciler | 41 / 155 / 404 | 37 / 152 / 399 |
| diamonds (1k / 4k / 10k) | mount-settle | graph-lock | 36 / 145 / 368 | 38 / 145 / 357 |
| realms (1k / 2k / 4k / 8k / 10k) | mount-settle | reconciler | 53 / 106 / 429 / 654 / 2101 | 40 / 84 / 164 / 321 / 407 |
| realms (1k / 2k / 4k / 8k / 10k) | mount-settle | graph-lock | 48 / 90 / 238 / 726 / 1023 | 47 / 85 / 166 / 324 / 407 |
| realms (1k / 2k / 4k / 8k / 10k) | all-providers-batch-restart-settle | reconciler | 54 / 113 / 393 / 2142 / - | 37 / 82 / 172 / 364 / 462 |
| realms (1k / 2k / 4k / 8k / 10k) | all-providers-batch-restart-settle | graph-lock | 45 / 110 / 258 / 635 / 1066 | 44 / 105 / 211 / 409 / 535 |

Hot path: `Context.Enter`/`Leave` and `Read` are untouched. `BenchmarkEnterLeave`
reports 0 B/op and 0 allocs/op at 1, 8 and 32 callers on both candidates, before and
after (about 24 ns/op at one caller on this host in both). The only per-event
addition on the owner or under the graph lock is one write into a ring that grows
up to its bound and, with a recorder, one append; it was not benchmarked
separately. Its cost does not appear in the mount curves above.

Left open: the observer dispatcher queue is still unbounded for a stalled observer
(unchanged, documented in `observer.go`); the harness's own per-sample records
still grow with the number of samples and count toward the section 8.2 reading of a
sampled run.
