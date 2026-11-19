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
retained accepted events (documented in `observer.go`); the 2026-11-19 section on the bounded observer queue below closes this.

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

Left open at that time (closed in the 2026-11-19 section on the bounded observer queue below): the observer dispatcher queue was still unbounded for a stalled observer
(unchanged, documented in `observer.go`); the harness's own per-sample records
still grow with the number of samples and count toward the section 8.2 reading of a
sampled run.

## 2026-11-19 bounded observer queue and overflow rule

The one item the retention work left open is closed in both candidates
(`test/spike/s0/internal/eventqueue`, `test/spike/s0/concurrency`). Criteria,
thresholds and the differential, property and scale suites are unchanged.
Numbers are development-host observations on a shared machine, not acceptance
evidence.

### Constraints that decide the design

- The kernel (graph-lock holder, reconciler owner) must never block on an observer,
  so a bound cannot push back: something has to give on overflow.
- Observers must stay serialized on one worker: the existing tests pin the
  cross-observer order within an event (`A1 B1 A2 B2`, `error1 panic1 healthy1`) and
  write to unsynchronized slices from several observers. A goroutine per observer
  is therefore out, and so is a per-observer queue served by its own goroutine.
- A slow observer must not make an observer that keeps up lose events, and must not
  lose silently itself. With one worker, a stalled callback blocks everyone behind
  it, so isolating the stall requires handing the worker role over, not only
  dropping something.

### Rule

`Retention.ObserverBacklog` (default `DefaultObserverBacklog` = 65,536 events,
reported by `RetentionStatus.ObserverBacklog` and `ObserverStatus.Backlog`; 0 selects
the default, a larger value is honoured) bounds the accepted events that are not yet
delivered, the in-flight one included. When `Append` (on the owner, under the lock) finds
that many undelivered events since the last detachment, it, in constant time and
without calling user code:

1. detaches the observer the worker is inside (the callback being run, or about to
   be): it leaves the copy-on-write listener list and is marked overflowed, with
   `Lost` = events captured for it minus those whose delivery started;
2. records the overflow in `ObserverStatus` (`Overflowed`, `EventsLost`, `LastOverflow`)
   and queues one diagnostic for the failure path;
3. starts a new worker generation that resumes the same event at the next listener,
   and accepts the new event as usual (the producer never waits and no event of any
   other observer is dropped).

The stalled callback's goroutine finishes that one callback and exits; the new worker
logs the overflow through the existing failure path (`ObserverErrors`,
`LastObserverError`, the `Logger`). If the observer was registered with
`ObserveEventsWith` its `onOverflow` runs once, on that goroutine, after the callback
returned, under the observer's lease, so it is serialized after the observer's own last
callback and is skipped (never run) when the owner is disposed or quiescing. A callback
that never returns never sees it; status and Close report the overflow regardless.
`AwaitObservers` and `Close` return an error matching `ErrObserverOverflow` (an
`*ObserverOverflow` with label, lost count, last sequence addressed to the observer
and the bound), joined with any other result. `Close` waits for detached goroutines
too; `ObserverStatus.Orphaned` counts those still inside a callback. A fiber
registering an observer is not failed: the observer's registration is detached and its
fiber keeps running, so the failure is the observer's, and visible; a fiber that wants
to fail on it can do so from `onOverflow`.

Counting only events accepted since the last detachment keeps the backlog that
built up behind a stalled observer from being charged to the observer that takes over:
with the bound per detachment, the queue holds at most `Backlog * (1 + k)` events while
`k` detached backlogs are still draining. A second observer that stalls in its turn is
detached in its turn (`TestInheritedBacklogIsNotChargedToTheNextObserver`).

Alternatives considered:

- Global bound with drop-oldest or drop-newest: loses events for observers that keep
  up because of one that does not, and is silent per observer. Rejected.
- Per-observer queue plus per-observer goroutine: isolates stalls with no handover,
  but breaks the pinned cross-observer order and makes unsynchronized observers race;
  costs a goroutine and a queue per observer (10,000 fibers could register 10,000).
  Rejected.
- Per-observer queue with one worker: the worker is still stuck inside the stalled
  callback, so every other queue fills too. It needs the same handover and duplicates
  every envelope per observer. Rejected for the shared queue above.
- Block the producer: forbidden (G1, kernel never waits on an observer).
- Fail the observer's fiber on overflow: needs the dispatcher to call back into the
  runtime from its worker or from the owner path; the observer error and the optional
  terminal callback give the same signal without that reentry.

Unfairness that remains, by design: the bound is blamed on whichever observer the
worker is inside when it is crossed. A burst of more than `Backlog` events arriving
faster than the worker can start on them detaches the observer then running, even if
that observer is merely slow. Tests that use a tiny bound synchronize for that reason;
the default is far above any producer burst measured.

### Sizing

Largest backlog observed (`ObserverStatus.PeakBacklog`, reconciler and graph-lock, no
overflow in any suite): concurrency tests 55, observers 17, replay 23, properties 36,
composition 30, hooks 31, callpath 50, scale tests 179, `S0_SCALE_REPRO` (about 100,000
events per run, 10,000-fiber mounts, no observers attached, so the worker only pops
events) 1,662. The default is about 40 times the largest of these. An opt-in
larger bound is needed by no suite.

Memory: an envelope is 160 bytes (128-byte event, sequence, listener slice header).
Nothing is preallocated: an idle dispatcher holds no queue, an observer costs one
registration of about 130 bytes and no queue, and a detached observer's registration
is released with its fiber. The full default bound is 10 MiB of envelopes, about 20 MiB
while the slice grows by doubling, and only while events are actually waiting: far
below the 128 MiB scale budget (section 8.1) and not reached at 10,000 fibers
(1,662 events waiting at worst, about 0.3 MiB).

### Cost

`BenchmarkAppend` (dispatcher, 3 runs each, before / after): producer cost per
emitted event with 0, 1 and 8 observers that keep up is 179 / 162, 147 / 160 and
165 / 170 ns, 0 allocs, about 460 B amortized queue growth in both: no measurable
change. Worker-side delivery per event (`drain-ns/event`) rises with the observer
count by about 4 ns per observer per event (one more critical section per
listener): 1 observer 34 / 43 ns, 8 observers 86 / 118 ns. `BenchmarkEventEmission`
(a `Record` call through the kernel, about 4.4 to 5.2 us graph-lock and 7.1 to 7.9 us
reconciler) is the same before and after within noise. The lease fast path
(`Context.Enter`/`Leave`) is untouched.

Tests: `observer_overflow_test.go` (both candidates, real activations: a stalled
observer hits the bound, the one that keeps up receives every event in order, the
overflow is reported exactly once with its count, nothing is retained, Close
reports it; just-below-bound slow progress loses nothing; overflow racing fiber
disposal and racing Close; the option and default in status) and
`internal/eventqueue/overflow_test.go` (the algorithm, including the inherited
backlog, an inactive owner and manual removal). No existing test asserted unbounded
queuing; `TestCloseReportsHungWork...` queues 2,048 events behind a hung callback and
stays below the default.

Left open: a callback that never returns keeps its goroutine and cannot receive its
terminal signal (Go cannot stop it); this is reported (`Orphaned`, `Close` timing out
with the real status), not hidden. The harness's own per-sample records are unchanged.

## 2026-11-19 reconciler: atomic admission, completion-time deadlines, own-work drain clock, Settle/Close contract

Four hazards of the reconciler (the single-owner candidate) were pinned by
`hazards_test.go` and `hazards_graph_test.go` and are fixed here. The graph-lock
candidate is changed separately. Every hazard test of the reconciler now passes
(also with `S0_HAZARD_REPRO=1`).

### Atomic admission (R1)

Rule: a `Mount`, `Batch`, `Update` or `Dispose` either returns its result or has no
effect. Never both "the caller got an error" and "the command was applied".

Mechanism: every command that is not a wait carries one claim word with three
states (queued, claimed, abandoned). The owner takes it queued to claimed
immediately before it applies the command. A caller that gives up (context done,
runtime closed) moves it queued to abandoned. The compare-and-swap decides who wins.

Linearization argument: the claim word moves out of "queued" exactly once. If the
caller wins, the owner finds "abandoned" at dequeue and drops the message, so the
error means no effect. If the owner wins, the command is applied in that same turn
and the reply is already on its way; the caller's give-up CAS fails and it takes
the reply instead of the context error, so "applied" always comes with the real
result. The wait after a claim is bounded because the owner never runs a callback
and replies in the turn in which it claimed, before it can close `done`.
Cancellation therefore stays effective exactly while the command is queued.

Settle, Close and AwaitFiber admit no mutation and stay cancellable at any time.
The earlier pinned behaviour of `Update`/`Dispose` (effect despite a context error,
at-least-once) fell out of the same mechanism: it is gone, and
`TestHazardR1UpdateDisposeEffectDespiteError` now expects "applied implies
reported" for the reconciler (it already did for the graph-lock). With 10,000
mounts under short contexts the burst leaves 5,000 accepted, 5,000 context errors,
5,000 fibers and no fiber whose id no caller holds.

### Deadlines are judged by the callback's return time (R2)

Rule: a callback is judged by when it returned, not by when the owner got to its
message.

Mechanism: each load, check, scope, dispose and tracked callback has a small
handshake. The worker stamps the instant the callback returned under a mutex,
before it sends the completion; the completion carries the stamp. When a deadline
falls due the owner reads the stamp: returned before the deadline means the work
finished in time and only its message is late, so the entry is retired without a
verdict and the queued message completes the transition. Not returned, or returned
at or after the deadline, is a real overrun and is failed as before. The drain
stage is judged the same way over its own work: the last lease by the instant it
was released (stamped on the quiesced release path only), tracked work by its
stamp, a lease still held by a tracked worker that is waiting for the owner's
acknowledgement is discounted.

Interleaving argument: the owner reads its clock (now >= deadline) before it takes
the handshake mutex, and the worker writes the stamp under the same mutex, so one
of two orders holds. If the stamp was written first it is the true return time,
and the verdict is exact whether the message is in flight, queued behind other
work or already handled. If the owner took the mutex first, the callback had not
returned at an instant at or after the deadline, so it truly overran and its stamp
can only be later. A completion that is ready together with its timer is judged
after the verdict (the owner calls `expire` before it handles a completion), so
select's random choice cannot matter. The dispose budget is one total deadline for
the effect walk: when an effect finished in time but the budget lapsed while its
message waited, the budget is re-armed for the effects still to run, which are
then judged by their own return time.

### Drain clock measures the fiber's own draining (R3)

Rule: a fiber's drain budget covers its own leases, tracked work and in-flight
check, nothing else. Waiting for dependants and for structural children is a
separate wait without a clock of its own, bounded transitively by those fibers'
own drain and dispose budgets.

Mechanism: the drain clock runs only while the fiber has work of its own and
stops when that work is done; entering the dispose walk starts the dispose budget
as before. A fiber that waits for a blocked fiber is reported by name ("fiber N
drain: waiting for fiber M", unwrapping to M's cause) in Settle and in its
snapshot, never as a deadline of its own.

Argument: a fiber's own work is finite by its own budget, a waiter's wait is the
sum of waits that are each bounded by the awaited fiber's budgets, and the
ownership graph (dependants, children) is acyclic, so every wait is bounded and
the culprit is the one fiber whose callback is actually blocked.

### Settle and Close contract (R4)

Settle returns when no fiber is mid-transition except fibers blocked past one of their own deadlines. It returns nil when nothing is blocked, otherwise one joined diagnostic per blocked fiber naming the fiber and the stage. It never returns while an unblocked fiber is mid-transition. If ctx ends first it returns ctx's error joined with the diagnostics known so far.

Close follows the same rule for its wait and always releases what it can (the
observer dispatcher included) once the runtime has drained, even when its own
context ended first. A fiber that waits on a blocked fiber counts as blocked, so a
hung dependant does not make Settle hang on its provider.

Mechanism: the owner keeps the number of busy units (one per fiber in a
transition, one per in-flight check). A quiet turn compares it with zero; when
fibers are blocked it compares it with the units held by the blocked set and the
fibers waiting on it, so the cost is paid only while Settle or Close is parked and
something is blocked. A waiter whose context ends is pushed to the owner by that
context and is answered with the context error joined with the diagnostics known
so far.

Argument: Settle answers only on a turn at which busy equals the blocked set's
units, which is exactly "no unblocked fiber is mid-transition"; a deadline can
only add fibers to the blocked set and the completion of a blocked fiber's
callback removes it from the set in the same turn that it changes phase, so the
two sides cannot be seen apart.

### Smaller fixes

Parked waiters cost nothing per turn: cancelled waiters are handed to the owner by
their context, and AwaitFiber waiters are examined only when their fiber changed
(previously every parked waiter was checked on every turn, about W squared over 2
context checks for W waiters; the test `TestReconcilerParkedWaitersCostNothingPerTurn`
pins that the count does not grow with the events). A load deadline is shown once
in a snapshot.

### Cost on the owner loop and per completion

Per command message: one compare-and-swap on the claim word. Per completion
message: one `time.Time` stamp (24 bytes) and, on the worker, one clock read, one
uncontended mutex and one small allocation for the handshake. Per owner turn that
handles a completion: one extra `expire` call, which is a comparison with the
earliest deadline when nothing is due. The lease fast path, `Enter` and `Leave`
are untouched; the quiesced release path stamps the instant the last lease is
released. No callback runs on the owner and the owner still never blocks.

## 2026-11-19 graph-lock: bounded waits with blame, targeted wake-ups, error rendering

Four confirmed hazards of the graph-lock core (pinned as tests in
`concurrency/hazards_*_test.go` before the fixes) and one structural hardening. The
reconciler is not touched by this section; it implements the same Settle/Close
contract separately.

### Wait and blame rule (G2)

Cause: a retiring fiber waited for its dependants (before its own teardown) and
for its structural children (after its effect walk) in one parked goroutine each,
with no diagnostic. One hung disposer left its provider and every ancestor
UNLOADING with an empty error forever, `Settle` and `Close` returned only the
caller's context error, and the observer dispatcher stayed open after the hung
disposer returned and the runtime drained.

Rule:

- A fiber's drain budget (lease drain, 5 s), cleanup budget (2 s, including scope
  and effect-rollback cleanup) and load budget (2 s) measure its own work only.
  Waiting for dependants or children is a different wait. It has no deadline of its
  own; it is bounded transitively by the awaited fibers' own deadlines, and a
  waiter never claims a deadline it did not miss (no `deadline:*` event, no
  deadline text on a waiter).
- A fiber is blocked while one of its callbacks is past one of its own deadlines
  and has not returned (a stall: `load`, `cleanup`, `scope cleanup`,
  `effect cleanup`, `lease drain`), or while it waits for an awaited fiber that
  is blocked. When an awaited fiber is blocked the waiter records, once per awaited
  fiber and stage, an error on itself that names the fiber and the stage
  (`awaiting dependants: dependant fiber 3 (dependant) is blocked in cleanup`) and a
  `wait:dependant` or `wait:child` event (`Event.UID` is the awaited fiber, `Key`
  its stage). The chain of blame is visible on every waiting ancestor: each names
  the fiber directly below it, and the fiber at the bottom carries the real
  deadline. The set is re-evaluated when an awaited fiber becomes blocked or
  recovers, and the walk up the waiters stops at the first one whose blame does not
  change. A fiber that starts waiting when its awaited fiber is already blocked
  records at once.
- Continuation style, no parked goroutine: the retiring activation records what it
  waits for under the lock (`a.wait`: dependants or children; its fiber keeps its
  worker slot and stays busy) and the event that can satisfy it, the release of a
  binding (`releaseBindingsLocked`) or the disposal of a child, resumes it
  (`resumeLocked`). Resuming the dependants wait starts the unload goroutine; the
  children wait starts the finishing goroutine, as the woken goroutine did before.
  Everything that runs a plugin callback (lease drain, the disposer walk) still has
  a goroutine.
- `Close` always closes the observer dispatcher once the runtime has drained. A
  `Close` that gives up (its context ended, or fibers are blocked) arms a one-time
  late close that fires when the last fiber and the root are gone; it parks no
  goroutine to wait for that.

Goroutines on a 10,000-fiber chain root restart (`Update` of the head of the
chain, sampled every 50 microseconds; the base is the three goroutines of the
idle runtime and the sampler): peak 10,002 and 10,003 before, 6 after.
`TestGraphLockChainUnloadParksNoGoroutines` holds the leaf of a 2,000-fiber chain
and checks that 1,999 fibers are parked with at most 10 extra goroutines.

### Settle and Close contract (R4)

The contract both cores implement, as written on `Runtime.Settle` in `types.go`:

> Settle returns when no fiber is mid-transition except fibers blocked past one of
> their own deadlines. It returns nil when nothing is blocked, otherwise one joined
> diagnostic per blocked fiber naming the fiber and the stage. It never returns
> while an unblocked fiber is mid-transition. If ctx ends first it returns ctx's
> error joined with the diagnostics known so far.

`Close(ctx)` follows the same rule for its wait. In the graph-lock a waiter that is
blocked on a blocked awaited fiber counts as blocked, so it is one of the joined
diagnostics (grandparent, provider, dependant: three lines, in fiber order). The
condition stays O(1): `refreshBusyLocked` keeps `busy` and `nblocked`, and Settle is
satisfied when `busy == nblocked` and the root is idle or blocked. A context error
with no diagnostics is returned as is (identity preserved).

A fiber is blocked from the moment its deadline is recorded, so a callback that
cooperates with its cancelled context returns an instant later and a Settle that
was already waiting may see the diagnostic first. Tests that released a hung
callback and then joined with Settle now let the callback return first.

Fibers that have no deadline are not blocked in this sense: an availability check
or effect-scope setup that hangs while the fiber is ACTIVE has none, so Settle
waits for it until the caller's context ends. That is unchanged and not covered here.

### Targeted wake-ups (G3)

Cause: `signalLocked` closed and reallocated one channel on every appended event, so
W parked waiters and E events cost exactly W x (E+1) wake-ups, and a waiter on
one fiber was woken by every event of every other fiber.

Now: no broadcast. `AwaitFiber` registers on the fiber it joins and is woken when
that fiber stops being busy; `Settle` registers in a settle waiter set that is woken
once, when `settledLocked` becomes true. Both wake-ups are in `refreshBusyLocked`
(the busy accounting), which every change of worker or state passes through, plus
the blocked accounting of G2. A waiter looks at its context once when it parks and
once per wake-up, so the test's `Done` count is exact. An event that changes
neither condition wakes nobody and allocates nothing; a canceled waiter unregisters
itself. Callers of the removed `wait(ctx, done)` helper: `Settle`, moved to the
settle waiter set (`Close` joins through `Settle`); `AwaitFiber` had its own copy
of the loop and now registers per fiber. The `signalLocked` calls in the transition
workers, pre-check completion and `RequestUpdateConfig` were general nudges; each
already changes worker or state through the refresh, so they are gone.

Wake-ups before / after (hazard test, E = 20 unrelated events):

| W waiters | before (W x (E+1)) | after |
|---|---|---|
| 10 | 210 | 10 |
| 100 | 2,100 | 100 |
| 1,000 | 21,000 | 1,000 |

for both `AwaitFiber` and `Settle`; the single remaining look is the one when the
waiter parks.

### Error rendering at the boundary (X1)

Cause: `Snapshots` and `Inspect` kept the plugin's error value and called its
`Error` method later, off the lock but on the caller's goroutine, so a panicking
`Error` method panicked a monitoring caller, and plugin code ran whenever any caller
looked.

Now: a plugin-supplied error is rendered exactly once where it comes back from
plugin code, on the plugin's worker goroutine, off the lock, under a recover that
turns a panicking `Error` into the fixed text `error formatting panic`
(`graphDiagnose`). The kernel stores a `graphDiagnostic` (the text, and `Unwrap`
to the plugin's value, so `errors.Is` and `errors.As` still work). Snapshots,
Inspect, the archive, `Close` and every join only read stored text. `AwaitFiber`
still returns the plugin's own value (identity is part of its contract). Audit of
every place graph-lock formats or holds an error or value from a plugin:

| Where | What happens |
|---|---|
| `runLoad`: Apply, ApplyEffect, config resolution (`prepareConfig`) | rendered once on the load worker; stored in `lastErr`; raw value kept in `awaitErr` for AwaitFiber |
| `runUnload`: disposers, scope cleanup | each result rendered on the unload worker as it returns |
| `cleanupEffect` (Effect rollback, late setup disposer) | rendered on the plugin's goroutine before the lock; stored in `cleanupErr` |
| `runScopeWork`: scope setup and cleanup | rendered on the lane worker before the lock |
| `startTracked` completion (Go helper) | rendered on the worker that ran the callback; stored in `lastErr`, raw in `awaitErr` |
| `graphCallApply`, `graphCallApplyEffect`, `graphCallDispose`, `graphCallCheck`, `graphCallPrepare` panics | `fmt.Errorf("... panic: %v")` inside the recover on the plugin's goroutine; `fmt` itself contains a panicking `Error`/`String` |
| `Snapshots`, `Inspect`, archive | read stored text only |
| `Close` | joins the stored diagnostics of the root's failure and `lastErr` |
| `Provide` values, service `Value` in events and `Inspect` | identity only, never formatted (`TestHazardG1...`) |
| `Record` data | marshalled on the caller's own goroutine before the lock |
| observer callback errors | owned by the shared dispatcher (`eventqueue`), unchanged and common to both cores |
| `runTracked` | `errors.Is` may call a plugin `Is` method on the worker |

A value that passes through plugin code and enters the kernel again is rendered
again at the new entry (an Effect rollback error that Apply then returns: two calls
to its `Error`). Each entry renders once; readers never render.

### Plugin-facing lock regions (hardening)

Kernel code behind `Provide`, `Defer`, `Effect` rollback, effect scopes, tracked
work, `MountChild` and `Dispose` runs on the plugin's goroutine inside the recover
that guards its Apply, so a kernel bug there would be reported as the plugin's
"Apply panic". No live panic exists (`TestHazardG1...` passes). Changes: those entry
points now `defer` a `guardEntry` that recovers kernel-side after the entry's own
deferred unlock, returns an error `internal error in <entry>` to the plugin and
records the same error and an `internal:<entry>` event on the fiber; the hand-locked
regions of `cleanupEffect` and `scopeCleanupContext` (budget read, post-callback
bookkeeping, cancel function) use `defer` or a closure with `defer`. A test parses
`graphlock.go` and fails on a bare `Unlock` in those functions; another injects a
kernel panic (a nil ledger) behind each entry and checks the error text, that no
"Apply panic" is reported and that the lock is free. Left as it was: the
hand-locked regions of worker and host-facing code that no plugin call reaches
(`Close`, `RequestUpdateConfig`, `checkAvailability`, the transition workers, timer
callbacks), and kernel panics on timer goroutines, which no recover can attribute.

### Measurements (development host, shared and busy, not acceptance)

Growth curves of the graph-lock, one root per fixture and process (`TestDevObserve`),
milliseconds at 1k / 2k / 4k / 8k / 10k fibers, before (916b564) and after. Both
are about linear; the absolute drop comes from not allocating a channel per event
and from not parking a goroutine per waiting fiber.

| Shape | Operation | Before | After |
|---|---|---|---|
| chain | mount-settle | 55 / 115 / 216 / 404 / 515 | 36 / 71 / 148 / 297 / 351 |
| chain | root-restart-settle | 76 / 176 / 320 / 661 / 832 | 59 / 106 / 211 / 435 / 569 |
| realms | mount-settle | 63 / 113 / 213 / 435 / 523 | 51 / 85 / 161 / 343 / 408 |
| realms | all-providers-batch-restart-settle | 69 / 135 / 276 / 560 / 700 | 53 / 92 / 194 / 428 / 521 |

`BenchmarkEventEmission` (graph-lock, 3 runs each, ns/op): 0 observers 5.3, 4.5, 5.3
before and 4.3, 4.7, 5.3 after; 8 observers 4.4, 4.1, 4.3 before and 3.7, 4.5, 4.2
after: within noise. One allocation fewer per event (the per-event channel):
19 to 18 allocs and about 100 B per `Record`. The lease fast path (`Enter`/`Leave`)
is untouched; the added cost on the transition path is one `blockedLocked` check in
`refreshBusyLocked`.

Differential and causal runs: live oracle and causal replay at the default
GOMAXPROCS and at 2, `-race`, `-count=3`: pass. Causal rates (100 runs per pair,
`-race` and GOMAXPROCS=2 `-race`): the 64 pairs that matched causally 100/100 before
still do, and D06 and D23 keep their pinned verdicts (100 causal mismatches with
the same messages) on both cores. Strict (total-order) match counts move within
noise in both directions on a busy machine; they are not pinned.

### Tests changed by the new contract and rendering

- `hazards_graph_test.go` G3: graph-lock asserts W looks instead of W x (E+1).
- `hazards_test.go` R4 graph-lock: the old comment (blocks until the context) is
  replaced; the first two Settles still end with the context error, now joined with
  the hung fiber's diagnostic, and `r4GraphLockContract` adds the prompt return.
- Tests that released a hung callback and joined with `Settle` right away now let the
  callback return first (`synctest.Wait`): LoadDeadlineKeepsStuckApplyAsSoleWriter,
  LoadDeadlineCooperativelyUnwinds, LeaseDeadlineDoesNotReleaseBorrowedEffects,
  CleanupDeadlineDoesNotClearEffectsOrRetry, ConfigurationPrepareUsesLoadDeadline,
  LoadingParentSelfDisposeRetiresLoadingChild,
  TrackedLedgerUsesStartOrderCopiesAndRealDrain.
- RootCloseRetainsTrackedWorkAcrossLeaseDeadline and
  RootCleanupDeadlineDoesNotFinishDisposer asserted that Close times out with the
  caller's context while a root callback is past its deadline; they now assert the
  diagnostic naming the root and its stage.
- ConfigurationCoalescesLatestInputDuringHeldCleanup waited on the removed
  `changed` channel; it polls for the revision.
- DependencyWaitWakesOnlyActualReleasedProvider observed the removed
  `dependantsChanged` channel; it checks the parked state of each provider.
- InspectFormatsDiagnosticsOutsideMutex and RootTrackedFailureIsContainedAndReturnedByClose
  asserted that `Error` is called only by readers; they now assert exactly one
  call, at the boundary.

## 2026-11-19 first full scale run on a dedicated host

Evidence: `test/spike/s0/results/scale-full-first/` (commit `225ffbe`, 10,800
runs, 22,372 s, GOMAXPROCS 8, dedicated 24 vCPU guest, no swap movement, no
cgroup throttling). The run predates the hazard fixes recorded above, so it is
candidate evidence; the final port reruns the gate.

**Classification: invalid, by the noise rule only.** No latency, memory or
goroutine bound failed on either candidate. Fifteen buckets had a throughput
coefficient of variation between 10% and 15.1% against the frozen 10% limit:
ten on `graph-lock`, five on `reconciler`. The scale protocol has no control
arm, so, unlike the call-path protocol after its amendment 2, it cannot tell
host noise from candidate noise. This note does not relax the rule. The gate is
repeated unchanged on the final port; if it is invalid again for the same
reason, a written amendment is proposed to the owner before any further run.

Selected results, milliseconds, `reconciler` / `graph-lock`:

| Workload, operation | p50 | p99 | max | Bound |
|---|---|---|---|---|
| chain, root restart to settle | 531 / 559 | 915 / 1028 | 991 / 1071 | 5,000 |
| diamonds, root restart to settle | 534 / 561 | 1020 / 1066 | 1081 / 1144 | 5,000 |
| realms, all providers batch restart | 434 / 522 | 857 / 949 | 866 / 979 | 5,000 |
| chain, teardown | 217 / 316 | 404 / 683 | 424 / 721 | |
| independent, teardown | 143 / 208 | 271 / 402 | 283 / 412 | |
| independent, mount to settle | 209 / 156 | 414 / 298 | 442 / 344 | |
| fanout, mount to settle | 267 / 192 | 489 / 355 | 500 / 377 | |
| leaf update to settle (all shapes) | 0.10–0.13 / 0.07–0.10 | 0.25–0.36 / 0.18–0.27 | 0.76 / 0.60 | |
| trees 500×4, cold tree mount | 0.20 / 0.19 | 0.59 / 0.54 | 9.7 / 9.1 | 50 |
| trees 300×4, cold tree mount (worst max) | 0.21 / 0.20 | 0.68 / 0.66 | 12.2 / 8.9 | 50 |
| trees 500×4, shared provider restart | 85 / 108 | 162 / 167 | 172 / 199 | |
| trees 500×4, reverse dispose | 29 / 43 | 60 / 78 | 67 / 94 | |

Retained heap, worst workload: 73.3 / 74.3 MiB for 1,000 roots (chain) against
128 MiB; 20.3 / 19.3 MiB for trees 500×4 against 32 MiB. Goroutines at the last
checkpoint: 4 / 3. Timer overhead p99 64 ns.

Reading: both candidates sit far inside every bound, and neither dominates.
`reconciler` has the lower p99 in 20 of 37 buckets, and is faster wherever work
is teardown, disposal or a restart that fans through shared providers (up to
about 40% on teardown). `graph-lock` is faster on mount of independent and
fan-out graphs (about 28%) and on single leaf updates (tens of microseconds).
Performance therefore does not select the core; the selection rests on the
failure-mode and maintainability arguments recorded in the two hazard sections
above.

## Decision, 2026-11-19: the core is `reconciler`

The project owner selected the `reconciler` core on 2026-11-19: one owner
goroutine holds all lifecycle state, and every transition is a message to it.

Grounds, in the order they weighed:

1. Both candidates match the oracle on the differential corpus within the
   recorded exceptions (ADR-035), pass the full property workload under
   `-race`, and have every known hazard fixed (the two sections above).
2. The first full scale run shows neither candidate dominating and both far
   inside every bound, so performance does not select.
3. With one owner there is no lock order to get wrong and no wait-and-blame
   rule to keep true as the kernel grows; the invariants are those of a
   sequential program. The independent review reached the same preference.
4. `reconciler` is faster where a production kernel spends its bad days:
   teardown, disposal and restarts that fan through shared providers.

Accepted costs: mounting many independent or fan-out graphs is about 28%
slower than `graph-lock`, a single leaf update is tens of microseconds slower,
and every transition passes through one loop, so a slow step in the owner
delays all fibers. The call-path lease stays off the owner loop (striped
counter) for that reason.

`graph-lock` stays in `test/spike/s0/concurrency` as the second implementation
for the differential and property tests. It is not ported and is not a
supported runtime.

Still open in this ADR: the cost and ownership of `Enter`, and the line budget,
both fixed with the final port to `sdk/kernel`.

## Owner decisions for the final port, 2026-11-19

The port to `sdk/kernel` is one package, `kernel`. The owner decided the points
the contract documents leave open:

1. **Test construction.** `sdk/kernel` exports one constructor that switches
   both binding maps off and takes a `testing.TB`. `kerneltest.New` wraps it,
   and a check in `tools/` forbids any other caller. One production file of the
   kernel imports `testing`, which is standard library.
2. **Signatures follow `07-c4-plugin-sdk.md`.** `Isolate`, `Intercept`,
   `Plugin`, `Defer` and `Go` return no error. A refused call fails the fiber,
   as `Key.Of` does with `*InjectError`.
3. **Capacity bounds are structural, not policy.** The archive limit, the event
   window, the observer queue and the owner inbox are capacities required at
   construction. The kernel holds no default for them; the launcher supplies
   them. They are not "policy numbers" in the sense of
   `08-profiles-roadmap-adrs.md` (repository rules).
4. **Deadlines are kernel-wide.** Load, drain and dispose deadlines are
   construction data for the whole kernel. A per-fiber deadline needs an ADR.
5. **No test recorder in the kernel.** The spike's `Record` instrumentation and
   full-history recorder are not ported. Harnesses order fixture records by the
   observer sequence.
6. **`Enter` has the documented shape**, `Enter() (leave func(), ok bool)`,
   with zero allocations per call as a gate of the port.
