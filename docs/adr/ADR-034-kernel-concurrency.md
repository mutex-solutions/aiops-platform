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
stamp. A tracked worker releases its own lease as its callback returns, before it
stamps the return and reports it, so a record stamped in time holds no lease and
its lease is judged by the same instant; the record keeps the drain open until the
owner has the report. (Until the S1 review the worker released its lease after the
owner acknowledged the report, and a deadline that fell due in between blamed a
worker that had returned in time.)

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

### Further owner decisions, 2026-11-19

7. **An entry is a token the kernel mints.** `kernel.Define` stamps an
   unexported token into the `Plugin` value it returns. A copy keeps the token.
   A `Plugin` built without it has no entry of its own and inherits the entry
   of the context that mounts it. Binding maps compare tokens. The kernel does
   not import `unsafe` and does not derive identity from a function value.
8. **`Available` takes one argument.** The undefined `hold` argument is removed
   from the contract (`07-c4-plugin-sdk.md` and the plugin record). It returns
   only by ADR with a definition.
9. **`Plugin.Decode` is `func(raw any) (any, error)`.** The kernel knows no
   configuration format; the loader passes the merged tree and `Apply` receives
   what `Decode` returns.

### Owner decisions on hooks and built-ins, 2026-11-19

10. **Dispatch needs a bound dispatcher.** The owner of a hook calls
    `Bind(c)` once in `Apply`; the kernel checks that the fiber of `c` provides
    the owning key and returns a dispatcher whose methods have the documented
    shape `(ctx, Audience, a)`. The hook value itself cannot dispatch.
11. **Sealed hooks have a third binding map.** `Config` carries a map from a
    sealed hook to the entries allowed to listen, compared by entry token like
    the two key maps. The launcher fills it from the distribution manifest, and
    the test constructor switches it off with the other two.
12. **A waterfall fails closed.** A listener that errors or panics fails the
    dispatch with an error naming the listener. Events keep the documented
    rule: the listener is skipped and logged.
13. **Deliberately open until a consumer exists.** `reflect`, `registry` and
    `hooks` are provided as values with no methods. `Fiber.Update(ctx, cfg)`
    keeps the running activation and returns the error when the new config
    fails to resolve or decode. `Serial` and `Bail` behave alike because Go
    listeners are synchronous; both names stay. A listener has no timeout of
    its own; the dispatch context bounds it. `internal/plugin`,
    `internal/status` and `internal/service` are declared but not wired until
    the loader (S1) fixes their payloads.

## 2026-11-19 first full cold-mount run on a dedicated host

Evidence: `test/spike/s0/results/coldmount-full-first/` (commit `225ffbe`, 120
runs, 18,039 s, GOMAXPROCS 8, same host as the scale run). **Classification:
pass, no reasons**, on both candidates. Like the scale run it predates the
hazard fixes and is candidate evidence; the final port reruns the gate.

Arm A puts a cold tree mount on every acknowledged request; arm B0 is the warm
baseline; arm B1 adds background mount churn beside warm requests.
Milliseconds, `reconciler` / `graph-lock`:

| Body, arm | p50 | p99 | max |
|---|---|---|---|
| 100 items, A (cold on the ack path) | 1.39 / 1.34 | 2.03 / 1.95 | 5.8 / 10.2 |
| 100 items, B0 (warm) | 0.81 / 0.80 | 1.35 / 1.34 | 3.7 / 2.7 |
| 100 items, B1 (warm beside churn) | 0.83 / 0.83 | 1.38 / 1.39 | 4.2 / 6.1 |
| 1 MiB, A | 7.28 / 7.35 | 10.0 / 9.9 | 26.7 / 24.2 |
| 1 MiB, B0 | 5.58 / 5.58 | 7.18 / 7.17 | 18.0 / 17.5 |
| 1 MiB, B1 | 5.57 / 5.55 | 7.23 / 7.22 | 22.4 / 22.5 |

A cold mount adds about 0.6 ms at p50 and 0.7 ms at p99 to a small request and
about 1.7 ms and 2.8 ms to a 1 MiB request. The mount itself is at most 1.0 ms
at p99 and 9.5 ms at worst. Mount churn beside warm traffic moves p99 by 0.03
to 0.06 ms in the median matched pair and 0.16 ms at worst. Every request
succeeded. The timeout fixture behaves as specified on both candidates: the
synchronous path answers 503 with a one-second retry hint and commits nothing,
the job snoozes and commits nothing, and no foreign scope contributes.

One known defect of the harness binary used: the per-cell field
`coldEnsureCalls` reports a shared counter for arm A (fixed later in
`c5ce385`); totals and latencies are unaffected. This reading goes to ADR-036
with the tenancy cost.

## 2026-11-19 the final port under the acceptance harnesses

The port in `sdk/kernel` is driven through its exported API by an adapter
backend named `kernel`, registered beside the two prototype cores in every
harness. Its first run found two port defects, both fixed to match the
reference:

- **Liveness is decided first.** On an ended activation `Provide` reported an
  undeclared key instead of inactive. Every entry point of a context now
  reports inactive before any other refusal.
- **An update resolves its config whenever the fiber has a target.** The port
  resolved only on an ACTIVE fiber; the reference resolves during a gated load
  and a blocked teardown too (corpus D17 and D20).

Results on commit `e34047e`:

| Harness | `kernel` backend |
|---|---|
| Differential corpus under the causal comparator, live pinned oracle, GOMAXPROCS 8 and 2 | 31 of 32 cases match, plus the two diagnostics D06 and D23. D25 differs and is an owner-accepted exception (ADR-035) |
| Property workload, 1,000 seeds × 1,000 operations, `-race` | pass (`results/property-full-kernel-port/`) |
| Package tests of composition, hooks, observers, scale, callpath | pass |

Evidence: `test/spike/s0/results/causal-kernel-port/` and
`property-full-kernel-port/`. The performance gates on the port are not yet
rerun; the adapter adds an availability function and an intercept lookup per
provision, so a pass through it is conservative.

### Owner decisions, 2026-11-19 (continued)

14. **D25 is not carried** (ADR-035). `Plugin` gets no second apply shape.
15. **A failing commit aborts the update.** `internal/commit` becomes a
    waterfall, which fails closed (decision 12): the new config is not
    retained and the fiber is not restarted. This matches the prototype.

## 2026-11-19 fourth full callpath run

Evidence: `test/spike/s0/results/callpath-full-fourth/` (commit `c5f0a79`,
7,224 s, dedicated host). First run under protocol amendment 2.
**Classification: pass.** Owner `Enter` and `Leave` allocate nothing in every
aggregate on both candidates. Six aggregates show candidate throughput
variation of 11.5% to 17.0% beside a clean control (1.7% to 6.2%); amendment 2
records these as characteristics of the candidate, not as an invalid run. The
run measures the prototype cores; the port reruns the gate.

## 2026-11-20 findings from the restart gate on kernel-owned resources

The restart gate was rebuilt with the pool, the job clients and the listener
owned by kernel fibers (ADR-045 records the runs). Writing it found one defect
and three limits of the launcher read API.

**Fixed: the ledger kept provided values.** A `provide` or `service` event
referenced the provided value until the event window moved past it, so a
disposed resource stayed reachable from the kernel and any service value was
visible in the event stream. Events now carry numbers and strings only: the
key, the label and the provision uid. `Root.Inspect` reports the value of a
live provision and nothing for one that has ended. Tests hold a weak pointer
to a provided value and require it to be collected after its fiber is disposed
or restarted, while the kernel is alive and before the window has moved.

**Recorded for the loader (S1), not changed:**

- The ledger has no lease events and no timestamps. That an unload waited for
  open leases is read from event order (UNLOADING, tracked goroutines done,
  first disposer, no deadline event), not from a record of the leases.
- `Fiber` revision counts updates, not restarts. A dependant that restarts
  because its provider did keeps its revision; the restart set is read from
  the status events.
- A goroutine the kernel starts carries no profiler labels; a plugin that
  wants attribution labels its own work.
- A tracked goroutine must end when the fiber's context is cancelled, so a
  server's accept loop stops at quiesce. While a listener restarts as a
  dependant, clients see refused connections rather than a 503.

## Line budget, frozen 2026-11-20

The project owner fixed the hard cap of `sdk/kernel` at **8,200 physical
lines** of production Go (comments and blank lines included, tests excluded),
measured by `tools/kernelcheck lines` and enforced in CI. The count at the
freeze is 7,451 lines in 19 files, one package. Growth past the cap, or a second
package under `sdk/kernel`, needs an ADR. This closes the last open point of
this ADR besides the performance gates on the port.

```
    65 sdk/kernel/builtin.go
   137 sdk/kernel/config.go
   368 sdk/kernel/context.go
     3 sdk/kernel/doc.go
  1002 sdk/kernel/effect.go
    94 sdk/kernel/effectscope.go
   225 sdk/kernel/event.go
   455 sdk/kernel/hooks.go
   670 sdk/kernel/inspect.go
   444 sdk/kernel/key.go
    30 sdk/kernel/logger.go
   850 sdk/kernel/observe.go
  1473 sdk/kernel/owner.go
    10 sdk/kernel/registry.go
   378 sdk/kernel/root.go
   263 sdk/kernel/rootview.go
   151 sdk/kernel/seam.go
   611 sdk/kernel/transition.go
   222 sdk/kernel/waterfall.go
  7451 total (cap 8200)
```

## S1 additions to the kernel API (ADR-059)

ADR-059 decision 2 approves five additions for the loader, and decision 20 a sixth
for the launcher's gate (G12). Decision 36 approves G6, G7, G8, G10 and G11, which no
earlier decision named (see the owner decision at the end of this section). Each is
built on what the kernel already does and changes no existing call. A batch with none of the new operations behaves as before.

**G1, a child mount in a batch.**

```go
func (c *Context) MountOp(p Plugin, cfg any) BatchOp
```

It has the semantics of `c.Plugin(p, cfg)`: the child inherits c's entry when
`p` was not defined, sees c's isolate and intercept chain, is an effect of c,
and a refused declaration gives a child that is mounted failed. It is applied
with the rest of the batch. `Root.Batch` returns one `*Fiber` per operation in
operation order, so the position of a `MountOp` holds the new child. The batch
is refused whole, with no effect and an error that matches `ErrInactive` and
`ErrInactiveEffect`, when c's activation has ended, is ending or was replaced by
a restart, and when an earlier operation of the same batch ends that activation
(later in the batch is allowed: the child is mounted and goes with that
activation). An operation ends it when it disposes or restarts c's fiber, or a fiber
whose restart reaches c's fiber through a key that it provides or as an ancestor, at
any depth: exactly the fibers that the batch's invalidation restarts and those that
their effect walks dispose. A caller that cannot tell which activations its other
operations end puts its child mounts first: each child then goes with an activation
that ends, and the parent's next activation mounts what it needs (the loader's
groups mount their children again when they activate). An `UpdateOpWith` whose restart `internal/update`
vetoes, and a `CommitOp`, restart nothing. (Until the S1 review a restart earlier in
the batch was not checked, and the batch was admitted with a child that was disposed
at once. The first fix counted only operations on c's fiber itself, so a batch that
restarted c's fiber through a provider or an ancestor still admitted such a child;
the second review found it, and the refusal now follows every path of the
invalidation.)

**G2, an update with a config in a batch.**

```go
func UpdateOpWith(f *Fiber, cfg any) BatchOp
```

Its effect is that of `f.Update(ctx, cfg)`. Before the batch is admitted,
`Root.Batch` takes the operations that carry a config in operation order and
resolves each through `internal/config` and `Decode` (when the fiber has a
desired target), then commits it through `internal/commit`. A refused config,
an error, a failed commit or a veto (`ErrCommitVetoed`) refuses the whole batch
and nothing is retained. When all have passed, `internal/update` runs for each
fiber that has a target, in operation order, and the configs are retained, the
last winning when a fiber appears twice, in the owner turn that admits the batch,
before any activation it starts (see G7). A veto of `internal/update` follows
`Fiber.Update`: the config is still retained, that fiber is not restarted, and the
rest of the batch goes on. An error of `internal/update` refuses the batch whole
and retains nothing, which departs from `Fiber.Update` (it retains before it
runs that waterfall) because a batch is all or nothing. A batch that the core
refuses after this, for example one that disposes the fiber earlier, retains
nothing; the core refuses every operation but a dispose after a dispose of the
same fiber, so a vetoed update and a `CommitOp` there are refused as an update is. A fiber with no target skips resolution and `internal/update`, and a
`When` fiber takes no config.

**G3, a dependency from an untyped key.**

```go
func NeedKey(k AnyKey) Dep
```

It is `Need` for a key held as `AnyKey`. A nil or zero key gives a `Dep` that a
mount refuses with `ErrKey`. There is no untyped `Want`.

**G4, the intercept config type.**

```go
func InterceptType(k AnyKey) (reflect.Type, bool)
```

It returns the type declared by `InterceptConfig[C]()`. `ok` is false for a nil
or zero key and for a key whose intercept config is dynamically typed.

**G5, the three kernel events and the registry lookup.**
`InternalPlugin`, `InternalStatus` and `InternalService` are now
`*Event[LedgerEvent]`. The kernel dispatches them from one internal observer of
the dispatcher that `Root.Observe` uses. It is subscribed when the first
listener of any of the three is registered, so a kernel with no listener
subscribes to nothing and the owner does no work for these hooks. The
subscription takes no lease of the root and leaves no effect on it (each listener
runs under its own fiber's lease), so a stuck listener blames its own fiber alone
and a subscription made again after an overflow costs the root nothing. A listener
runs off the owner, one event at a time in the order of `Sequence`. A failing,
panicking or slow listener is skipped and logged to `Config.ObserverLog`, and
cannot block or fail the kernel. Listeners are admitted only through
`Config.Listeners` or the root context. The stream is bounded by
`Config.ObserverQueue`: a listener that stalls the dispatcher until that many
events wait detaches the kernel's subscription by the overflow rule, so the
events of that time are lost for every listener and no listener is removed. The
kernel then subscribes again, and just before the first event it delivers it
dispatches to all three hooks a `LedgerEvent` of Kind `overflow` whose
`Sequence` is the one before that event. A listener resyncs by calling
`Root.Inspect` and ignoring every later event whose `Sequence` is not above
`Inspection.Sequence`.

| Ledger Kind | Hook | Meaning |
|---|---|---|
| `mount` | `internal/plugin` | a fiber was created |
| `state` | `internal/status` | the fiber changed state (`From`, `State`) |
| `state` with `State` DISPOSED | `internal/plugin`, after `internal/status` | the fiber was removed |
| `provide` | `internal/service` | a provision appeared (`Key`, `UID`) |
| `service` | `internal/service` | a provision changed availability (`State` is its provider's) |
| `remove` | `internal/service` | a provision ended |
| `overflow` | all three | events were lost, see above |

Every other Kind (`effect`, `dispose-effect`, `dispose:request`, `scope:*`,
`go:*`, `deadline`) reaches no hook.

```go
func (r *registry) Fiber(id uint64) (*Fiber, bool)
```

This is the method of the `registry` built-in that the retry plugin needs. It
finds a live or archived fiber by id and is false for id 0, an id never issued
and an evicted record. The `*Fiber` can dispose, await and restart that fiber,
so whoever holds the registry has those powers over every fiber. It restarts with
`Update(ctx, nil)`, which keeps the config the fiber retains. The core does not
keep a fiber's config cell, so it cannot change that config: `Commit`, `Update`
with a config, `UpdateOpWith` and `CommitOp` of it return an error (until the S1
review they reported success and retained nothing). The documents
do not say who may hold it, so `RegistryKey` is now consumer-bound: only the
entries that `Config.Consumers` lists resolve it, and `NewForTest` keeps every
entry admitted. The project owner accepted this on 2026-11-21 (ADR-059 decision 33).
`reflect`, `hooks` and `logger` stay open.

**G6, a config committed without a restart.**

```go
func (f *Fiber) Commit(ctx context.Context, cfg any) error
func CommitOp(f *Fiber, cfg any) BatchOp
```

`Commit` vets `cfg` exactly as `Fiber.Update` does: when the fiber has a desired
target it resolves `cfg` through `internal/config` and `Decode`, then it runs
`internal/commit`, which fails closed (a veto is `ErrCommitVetoed`). Only a
commit that completes retains `cfg`. It runs no `internal/update` and does not
restart the fiber: the running activation keeps the value it was applied with
and its `Apply` is not called again. A plugin learns the new value from whoever
commits it (the loader dispatches `loader/volatile-update`), and the next
activation of the fiber, whatever starts it (a provider's epoch change, a
retry, an `UpdateOp`), resolves the retained `cfg`. A fiber mounted with `When`
takes no config and `Commit` ignores `cfg`. `CommitOp` is the same operation in
a batch, handled with `UpdateOpWith` before the batch is admitted; the config is
retained in the owner turn that admits the batch, and a refused batch retains
nothing.

**G7, config changes of one fiber are serialised.** This closes the open point
of G2: a batch retained its configs before the core admitted it and restored the
old ones when the core refused it, so a concurrent `Update` or `Commit` of the
same fiber in that window could be overwritten by the restore. Each fiber's
config now has a change lock. `Update` and `Commit` take it only to retain. A
batch takes the locks of the fibers it changes in ascending fiber id (a fiber
named twice once), from before its first `internal/update` until the core has
admitted or refused it, and releases them after the restore. So a restore never
overwrites a newer value: the newer change waits and retains after it. The lock
is held across the `internal/update` hooks of the batch, so such a hook must
not change the config of a fiber of its own batch; no other plugin code runs
under it. Two `Update`s of one fiber race as before (the last retain wins, as
if they ran in that order, and each restart applies the retained config).

The S1 review found that the restore was not enough: a restart that takes no lock
(an `UpdateOp`, the registry's `Update(ctx, nil)`, a provider's epoch change)
could start an activation in that window and apply a config that the core then
refused, and keep running with it. A batch now retains nothing before it is
admitted: `Root.Batch` hands each config and its cell to the core, and the owner
retains them in the turn that admits the batch, before it starts any activation.
There is no restore; the locks are still held, as above, until the core has
admitted or refused the batch, so a newer `Update` or `Commit` retains after it.

**G8, the owner's built-in step is innermost in a waterfall.**

```go
func (d WaterfallDispatcher[A, R]) RunWith(ctx context.Context, aud Audience, a A,
    inner func(context.Context, A) (R, error)) (R, error)
```

This is the contract of `07-c4-plugin-sdk.md:386`: listeners run outermost
first, and a listener that does not call `next` vetoes the rest of the chain
and the built-in behaviour. The next of the last listener calls `inner`, and
with no listener `inner` runs alone; a listener that does not call `next` means
`inner` does not run. An error of `inner` fails the dispatch as a listener's
does, as a `*ListenerError` naming the owner's fiber, and `inner` is never
skipped for an owner that is quiescing. `Run` is unchanged: a nil `inner`, and
the zero result past the last listener. The loader uses it for
`loader/patch-context`: the row's isolate and intercept are the innermost step,
a listener wraps what `next` returns, and a veto (no `next`, or a nil context)
fails that entry with `ErrPatchVetoed` and never runs its plugin. The docs do
not say what a veto of this hook means; the project owner chose fail closed on
2026-11-21 (ADR-059 decision 34).

**G9, a drain past its deadline is a leak (ADR-059 decision 13).** When the drain
deadline of a fiber falls due and the fiber's own work is not over (a tracked
goroutine or a lease still open, judged by the rule that already decides whether
the deadline is a verdict), the fiber is marked leaked exactly as one whose
disposer is still running past the dispose deadline: the snapshot says `Leaked`
for the rest of the incarnation, the archived one included, and the diagnostic
of `Settle` and `Close` wraps the exported sentinel `ErrLeaked` together with
`context.DeadlineExceeded`. The text "fiber leaked" is no longer the way to match
it: the launcher uses `errors.Is(err, kernel.ErrLeaked)`. A drain that ends in
time is not a leak, and nothing else about the lifecycle changes: the mark is the
only effect. The `deadline` ledger event of the stage (`drain` or `dispose`) is
appended after the mark, so an observer that sees it also sees `Leaked` in the
snapshot.

**G10, the kernel logs what it knows.** When a tracked goroutine returns an
error or panics, and when a fiber is marked leaked, the kernel writes one record
at level ERROR to `Config.LogHandler` (nothing when it is nil), with the
attributes `fiber` (id), `plugin` (the plugin id), `label` and `error` (the error
text), under the messages `tracked goroutine failed` and `fiber marked leaked`. The
`label` of a failed goroutine is its own. The `label` of a leak is that of what still
runs, as the failure matrix asks ("Logged with its label", 08 §1.6): the disposer in
flight at the dispose deadline, or the tracked goroutines that had not returned by
the drain deadline (sorted, comma-separated); a drain held by a lease alone has no
label and gives its stage, `drain`. The stage is in `error` either way. No config value is
in a record. The record of a goroutine is written on the goroutine's own worker,
before the failure is reported to the owner, so the cause is in the log before
the fiber is FAILED. The record of a leak is written by a goroutine that the
owner starts, because the owner runs no foreign code; a panic of the handler is
contained. A goroutine that ends because its context was cancelled writes no
record.

**G11, the observer of the root sees the events of `Close`.** The report that the
launcher made was true: the root quiesces when `Close` begins, so the lease of
the root refused every callback from then on and the dispatcher skipped each
later event (in a test with two fibers, `ObserverStatus.Skipped` was 13 after `Close` and
the observer had seen only the events before it; none of the skipped ones were
delivered), among them the `deadline` of a leaking fiber. The observer that
`Root.Observe` registers now delivers an event accepted before the dispatcher
closes even when the root's lease is refused, with the context of the callback
already cancelled by the quiescence, and it registers no cleanup effect on the root
(see the owner decision below), so it is not removed when the root is disposed and
the last events of the root are delivered too. The
dispatcher already drained its queue before it stopped. When `Close` returns the
diagnostic of a fiber that is still blocked, it now first waits, within its
context, until the events accepted so far are delivered, so the diagnostic and
the event reach a reader together. During that wait an observer that delivers
nothing for one drain deadline (a listener of a kernel event stuck in the relay, say)
is detached by the overflow rule, so the wait is bounded even when the context of
`Close` never ends. An event for an observer that was detached is counted in
`ObserverStatus.EventsLost`, and one still waiting when the context of `Close` ends
is in `Remaining`. Such a detachment says that the stall caused it, not the backlog
bound: its `ObserverOverflow` (in `Close`'s error, the terminal signal and
`Config.ObserverLog`) has `Stall` set to the drain deadline and `Limit` 0, its text
says that the observer delivered nothing for that long while `Close` waited, and so
does `ObserverStatus.LastOverflow`. It is still counted in `Overflowed`, which counts
every detached observer, and matches `ErrObserverOverflow`, the error of every
detachment. (Until the second S1 review it was reported as an overflow of a backlog
bound that was never reached.) Observers registered on
a fiber's own handle by a test keep the old rule.

**Owner decision, 2026-11-21: a launcher observer registers no effect on the
root.** Until then `Root.Observe` registered an empty cleanup effect on the root,
with its `effect` event in the ledger, and it stayed until `Close`. A launcher
that observes again after each overflow, as boot's watch resyncs, so grew the
root's effects by one per overflow for the life of the process. `Root.Observe` now
registers none: the observer leaves when its `Disposer` is called or when the
dispatcher closes, and an observer made again after an overflow leaves nothing
behind, as the relay already did (G5). The S0 adapter's test that compared the
ledger and the observer with the prototype, whose observer is an effect of its
owner, no longer expects that event: the effect of the root's observer is not part
of the contract a plugin sees, since only the launcher observes.

**G12, the classes of a key and the seal of a hook (ADR-059 decision 20).**

```go
func KeyClasses(k AnyKey) (providerBound, consumerBound, ok bool)
func HookSealed(h AnyHook) (sealed, ok bool)
func (u HookUse) Sealed() bool
```

This is the sixth kernel addition of S1, which the project owner approved after the
five of ADR-059 decision 2. Like `InterceptType`, each reads what a definition
declared and nothing else. `providerBound` is `ProviderBound()`. `consumerBound` is
`ConsumerBound()` or `Signed()`: both are keys that only the entries
`Config.Consumers` lists resolve, and they differ only in who the distribution may
list, which the kernel does not check. `sealed` is `Sealed()` or a hook of the
kernel. `ok` is false for a nil or zero key or hook. `HookUse.Sealed` is the same
seal for a hook as a plugin lists it in `Plugin.Hooks`, which holds a `HookUse`
and no `AnyHook`; it is false for a zero `HookUse`. They are the predicates that
`New` holds the binding maps and `Config.Listeners` to, so a launcher that derives
the classes from the module table's definitions through them cannot disagree with
the kernel. Gate check 7 derives the classes of keys and hooks this way, the hooks a
plugin of the module table listens on included, and `dist.yaml` lists only who is
allowed (bindings and listeners), never a class. They take no lock, run no plugin
code and return values.

**Owner decision, 2026-11-21: G6, G7, G8, G10 and G11 are approved** (ADR-059 decision
36). No earlier decision named them: G6 adds `Commit` and `CommitOp`, G8 adds `RunWith`,
G7 adds the per-fiber config change lock, G10 adds ERROR records to `Config.LogHandler`,
and G11 delivers the events of `Close` and adds the exported field
`ObserverOverflow.Stall`; decision 21 covered only the root observer's effect in G11.
They merge to the main branch with G1 to G5 (decision 2), G9 (decision 13) and G12
(decision 20) after the S0 gates on the final port are recorded (ADR-059 decision 1),
their exact shapes are recorded here then, and the differential corpus, the property
workload and the call-path gate run again on the merged kernel (decision 2). The full
property workload passed on the branch kernel with G1 to G12 at `ab9b405`
(`test/spike/s0/results/property-full-s1-converged/`); that run is evidence on the
branch, not the run after the merge.

**Merged to the main branch, 2026-11-22.** G1 to G12 reached the main branch in
`fb0a86b`, with the final-port kernel backend adapter. On the merged kernel the live
oracle comparison passed at `GOMAXPROCS=8` and at `GOMAXPROCS=2`, 68.5 s and 67.3 s of
package time (`test/spike/s0/results/causal-main-merged/`), and the full property
workload passed on three backends in a frozen checkout: exit 0, 4,525.13 s in total,
kernel backend 1,686.34 s, with the same source closure as `7f497a6`
(`test/spike/s0/results/property-full-main-merged/`). The call-path gate failed once on
`fb0a86b` (`test/spike/s0/results/callpath-full-merge-commit/`) and then passed on the
merged kernel in a same-session comparison with `8ec2d8c`
(`test/spike/s0/results/callpath-full-merged-ab/`); see "2026-11-22 call-path gate on
the merge commit" below.

The kernel is 7,900 physical lines of production Go (cap 8,200): 140 for G1 and
G2, 24 for G3 and G4, 91 for the events, 19 for the registry lookup, 49 for G6,
27 for G7, 24 for G8 and 75 for G9 to G11. The fixes of the S1 review recorded in
G1, G2, G5, G6, G7, G9 (R2 above), G10 and G11 bring it to 7,981, and the G1 comment
that says which restarts refuse a child mount to 7,985. The fixes of the second
review (18 lines for G1's refusal along every path of the invalidation, 13 for the
stall detach of G11, 2 fewer for the root observer's effect) bring it to 8,014, and
G12 (24, and 5 for `HookUse.Sealed`) to 8,043.

## 2026-11-22 S0 closed; lease cost under contention as a tracking point

S0 closed on 2026-11-22 on the project owner's approval of the S0 report
([`s0-report.md`](../design/s0-report.md)).

**Tracking point: lease cost under contention.** The final-port call-path gate
(`test/spike/s0/results/callpath-full-final-port/`, commit `8ec2d8c`, classification
pass; S0 report §4.3) measured `owner_enter_leave` in the baseline arm:

| p50 / p99 | 1 caller | 8 callers | 32 callers |
|---|---|---|---|
| `kernel` | 151 / 370 ns | 315 / 1,054 ns | 332 / 1,073 ns |
| reconciler | 130 / 194 ns | 130 / 631 ns | 130 / 609 ns |
| graph lock | 130 / 287 ns | 130 / 367 ns | 130 / 436 ns |

With 8 and 32 callers, `Enter` and `Leave` on `kernel` cost about 2.5 times the
prototypes' at p50. The `kernel` p99 is at most 1,073 ns, inside the 2 µs bound, and no
run of any candidate allocates. The cause: the lease counter picks a random shard per
call, so concurrent callers share cache lines. The owner decided on 2026-11-22 to keep
the kernel as it is, with no change now, and to review this point at the S3
measurements.

## 2026-11-22 call-path gate on the merge commit

The call-path gate failed on `fb0a86b` on the measurement host: `FAIL: baseline
kernel/owner_enter_leave/c8: run 9 p99 5548ns > 2000ns`. With 8 and 32 callers, the
`kernel` p50 over all runs rose from 315 and 332 ns (`8ec2d8c`) to 433 and 464 ns. Five
runs at 8 callers and seven at 32 had a p99 of 1,418 to 1,595 ns, from the fourth run
on, against one run at each count on `8ec2d8c` (1,619 and 1,487 ns). One caller did not
move (151 and 150 ns), nor did the prototypes.

**Cause: not a change of the kernel or the adapter.** Nothing the merge added runs on
the contended `Enter` and `Leave`:

- The machine code of `handle.enter`, `confirm`, `release`, `leaseCount.shard` and
  the adapter's `Enter`, `Leave` and forwarding closure is the same on both commits,
  except that the handle's field offsets are 8 bytes higher: `handle.act`, which G1's
  child mount in a batch reads, sits before `live`.
- The shift moves no field onto another cache line. The handle is in the 1,408-byte
  size class (128-byte aligned slots, 8-byte header): `live` stays on line 0, each
  shard counter alone on lines 1 to 16, `leave` on line 18.
  `TestLeaseShardsOwnTheirCacheLines` holds this: each shard has its line to itself
  and no other handle field shares one, at every 8-byte offset of the handle.
- The adapter's changes (values filed by provision identity, pruning by watermark)
  run inside `Provide` only. The fixture listens on no kernel event, so no relay runs.

Under contention every `Enter` and `Leave` moves a randomly picked shard line from
another core, so the cost follows the transfer latency between the cores the callers'
threads run on. That has two modes, on the measurement host and on the development host
alike: fast, about 250 to 350 ns p50 and 700 ns p99, and slow, about 500 to 620 ns and
1,450 to 1,800 ns. The mode changes from window to window, even between the 8- and
32-caller windows of one fixture, on both commits. The `fb0a86b` run met the slow mode
from its fourth run on; `8ec2d8c` met it in one run. On the measurement host one window
alone crossed 2 µs and failed the gate: run 9 at 8 callers, 5,548 ns; the next highest of
either commit there is 1,619 ns (`8ec2d8c`). A host disturbance is the most likely cause
of that window, but this is inferred: the run's raw records and host-noise samples are
not in the repository.

Development host (shared 2-socket KVM guest, 24 vCPU, `GOMAXPROCS=8`; not acceptance),
commits run in alternating processes, medians of the per-window p50 / p99 in ns:

| `owner_enter_leave` | `8ec2d8c` | `c06304f` | `2576547` (test only) |
|---|---|---|---|
| protocol order, c8 (30 windows) | 447 / 1,542 | 488 / 1,599 | |
| protocol order, c32 (30 windows) | 419 / 1,500 | 500 / 1,619 | |
| fixture lease, c8 (30 windows) | 525 / 1,734 | 500 / 1,681 | 501 / 1,743 |
| fixture lease, c32 (30 windows) | 535 / 1,788 | 520 / 1,746 | 531 / 1,790 |
| kernel only, c8 (30 windows) | 499 / 1,731 | 502 / 1,750 | 486 / 1,796 |
| kernel only, c32 (30 windows) | 517 / 1,753 | 526 / 1,766 | 502 / 1,763 |

"Protocol order" is the gate's own sequence (warm-up, then baseline and stress arms of
all three candidates alternating, 10 repetitions, stress windows shortened to 2 s), three
processes per commit. The other rows are fresh 100-leaf fixtures measured with the gate's
`MeasureArm`, ten processes per commit. Slow windows (p99 above 1,200 ns) in protocol
order: 23 and 21 of 30 on `8ec2d8c`, 23 and 26 on `c06304f`. On this host the p99 of
`8ec2d8c` itself exceeds 2 µs in 5 of the 120 windows of the last four rows. No
`owner_enter_leave` window allocated in protocol order; in the other rows a single
allocation appeared in some windows of every arm, also in a control arm that calls no
kernel code.

Re-run on the same host on 2026-11-23, after the guard test was amended. `c06304f` and
`2576547` build a byte-identical gate test binary, so protocol order ran `8ec2d8c` and
`2576547` only, four processes each; the other rows rotated the three commits by process,
ten processes each. "Kernel only" here is `Context.Enter` and its leave on a test root
with one owner and 100 leaf fibers, with the loop of `MeasureArm`.

| `owner_enter_leave`, re-run | `8ec2d8c` | `c06304f` | `2576547` (test only) |
|---|---|---|---|
| protocol order, c8 (40 windows) | 444 / 1,498 | | 334 / 892 |
| protocol order, c32 (40 windows) | 461 / 1,484 | | 364 / 1,086 |
| fixture lease, c8 (30 windows) | 506 / 1,696 | 515 / 1,752 | 512 / 1,741 |
| fixture lease, c32 (30 windows) | 518 / 1,758 | 524 / 1,774 | 520 / 1,744 |
| kernel only, c8 (30 windows) | 472 / 1,678 | 460 / 1,646 | 465 / 1,677 |
| kernel only, c32 (30 windows) | 477 / 1,664 | 469 / 1,640 | 476 / 1,690 |

In protocol order the slow windows were 30 and 28 of 40 on `8ec2d8c` and 19 and 20 on
`2576547`, between 2 and 19 of 20 per process, and the p99 exceeded 2 µs in four windows
of `8ec2d8c` (2,001 to 2,880 ns) and one of `2576547` (2,277 ns). All but one window of
the other rows were slow. No `owner_enter_leave` window allocated in protocol order; in
the other rows one to three allocations appeared in about half the windows on every
commit, and one in 3 of the 90 windows of the control arm.

No kernel or adapter change is made: removing `act` or padding the handle changes no
line that is written, and the shard choice stays as decided above for S3. The gate on the
merged kernel is to be run again on the measurement host with `8ec2d8c` in the same
session, alternating by process, and judged against it.

**Same-session comparison on the measurement host, 2026-11-22 and 2026-11-23.** Four full
gate runs, one process each, alternating `e3a72af` (main after the merge: `sdk/kernel` as
in `fb0a86b` plus the test-only guard above, and the same gate source closure) and
`8ec2d8c` (`test/spike/s0/results/callpath-full-merged-ab/`; the failed run on `fb0a86b`
is in `test/spike/s0/results/callpath-full-merge-commit/`). `kernel` `owner_enter_leave`,
baseline arm, p50 / p99 over all runs, the highest window p99, and the slow windows (p99
above 1,200 ns) of 10, in ns:

| Run | Commit | Gate | 8 callers | 32 callers | Highest window p99 | Slow windows, 8 + 32 callers |
|---|---|---|---|---|---|---|
| 1 | `e3a72af` | pass | 367 / 1,309 | 368 / 1,304 | 1,636 | 2 + 2 |
| 2 | `8ec2d8c` | pass | 581 / 1,519 | 549 / 1,513 | 1,795 | 8 + 8 |
| 3 | `e3a72af` | pass | 401 / 1,406 | 426 / 1,412 | 1,762 | 5 + 5 |
| 4 | `8ec2d8c` | pass | 455 / 1,466 | 443 / 1,460 | 1,648 | 5 + 6 |

The gate passes on the merged kernel: one caller stays at 150 ns p50 in every run, and no
`owner_enter_leave` run of any candidate allocates. The slow mode appears on both commits,
more often on `8ec2d8c` in this session (27 of 40 windows at 8 and 32 callers, against 14
of 40 on `e3a72af`), and no `owner_enter_leave` window of either exceeds 1,795 ns, so the
2026-11-22 failure points to the host's slow mode plus one disturbed window (5,548 ns, 3.1
times that), not to a change of the kernel. The cause of that window stays inferred: the
failed run's 120 host-noise samples, now in
`test/spike/s0/results/callpath-full-merge-commit/report.json`, show no swap pages moved
and no throttled periods, and its raw records stay on the measurement host. The lease
cost under contention stays the tracking point for S3 that the owner decided.

## 2026-11-23 proposed addition G13: the Volatile hand-off

- Status: **proposed 2026-11-23, awaiting the project owner; revised the same day after
  review.** Nothing of it is on `main` or on `s2a/integration`. Its code lands on
  `s2a/integration` and reaches `main` by merge (ADR-060 decision 32), and only after the
  owner accepts this section (ADR-060 decision 3).
- Citations: `00`, `03`, `07` and `08` are the files of `docs/architecture/` with those
  numbers, at `a3c85ae`. `07:1377` there is `07:1375` at `491a518`, the line that ADR-060
  decision 3 cites, and `07:1382` on `s2a/integration` (`502a355`). On `502a355` the
  package documentation of `sdk/plugin` cites `07:1375`, and the documentation of its
  `Volatile` type cites `:1382`. `sdk/kernel` is the same at `a3c85ae` and at `502a355`;
  the kernel's vocabulary list is longer on `502a355` (ADR-060 decision 45). Lines of this
  ADR are given as "line N". The plugin record, `docs/design/plugin-system-2026-11.md`, is
  cited by section.
- The reference model is Cordis 4.1.0 with `cordis-plugin-loader` 1.1.0 as dsh publishes
  them, pinned in `docs/design/s0-reference-pins.md` and vendored for the differential
  oracle under `test/differential/oracle/vendor/`. There, `entry.ts:N` is
  `loader/src/config/entry.ts`, `index.ts:N` is `loader/src/index.ts`, "the Cordis
  README" is `cordis/README.md` and "the cosmokit README" is `cosmokit/README.md`.

### Requirement

- `07:1377`, in full: when only `Volatile` fields change, "which include every
  tenant-writable field", a running plugin sees "No call and no restart. The loader
  commits the value in place, unless the old or new value holds a reference, which
  restarts the row (ADR-059 decision 17); the next read of the `Volatile` field, or the
  next `tenants.ConfigFor` call, returns it". `00:484` and `08:188` state the same commit
  in place by the loader, with the same exception.
- ADR-059 decision 17: "When the old or the new value of a volatile field holds a
  reference, the loader does not commit the change in place but restarts the row, as it
  already does", so that "`loader/volatile-update` never carries an unresolved
  reference". The loader definition applies it at each changed path and states what
  follows from it (`sdk/def/loader/v0/volatile.go:36-40`): `config-refs` resolves
  references "only when a fiber activates"; "A literal changed beside a reference that
  did not change is volatile"; and `loader/volatile-update` carries the new config through
  `WithoutRefs`, which replaces every reference by nil (plugin record §8).
- `07:475`: "Configuration that may change without a restart is a `plugin.Volatile[T]`
  field. Every tenant-writable field is volatile, holds a literal or `${secret:name}`, and
  is read through `tenants.ConfigFor`." A `Volatile` field may therefore hold a
  reference: on `502a355` the test distribution's `refs-group.yml` sets the `note` of the
  toy consumer to `"${file:note}"`.
- `tenants.ConfigFor` "Returns the row config with that tenant's tenant-writable fields,
  from the handle" (`07:875`). It arrives with the `tenants` definition at S4a (`03:386`),
  and from S4b "A tenant-writable field is committed in place and read per call through
  `tenants.ConfigFor`" (`08:550`). G13 covers the first half of the last clause of
  `07:1377`, the read of a `Volatile` field of the row config. The `ConfigFor` half is S4a
  and S4b. It depends on G13 in one respect: under G13 a copy of a config reads the same
  `Volatile` cells as the config it was copied from ("What `sdk/plugin` does with it"), so
  `ConfigFor` builds each tenant's config with `Volatile` cells of its own and never
  writes through a cell of the row config.
- The reference model: volatile fields "are committed into the running references
  without a restart and announced via `loader/volatile-update`"
  (`docs/research/dsh-architecture.md:69`, after the Cordis README, "Volatile
  configuration", lines 103-111); "Committed into the running object; the fiber gets
  `loader/volatile-update`; no restart" (`docs/design/plugin-r3/literal-cordis-ts.md:430`);
  "Committed in place into the running references"
  (`docs/design/plugin-r3/cordis-port-go.md:714`). The plugin record carries it as is
  (§15, row 16; `08:58`) and keeps `Volatile[T]` out of the kernel: it lives in
  `sdk/plugin`, and "the loader commits volatile values, as `entry.ts:180` does" (§3).
  The next section states what that loader does.
- ADR-060 decision 3: a kernel change that hands a value committed in place to a running
  `plugin.Volatile[T]` field is an ADR-034 addition, proposed before it merges, inside the
  8,200-line cap, and verified by the differential corpus, the property workload and the
  call-path gate (ADR-059 decision 2).
- ADR-059 decisions 10, 17 and 44 stay as they are: S2a derives the volatile paths, the
  loader decides what is volatile-only (`loaderdef.VolatileOnly`), commits it with
  `CommitOp` and dispatches `loader/volatile-update` with the payload of decision 44 after
  that batch returns (`loader/apply.go:104`).

### What the reference loader does

`Entry.update` and `_commitVolatile` of `cordis-plugin-loader` 1.1.0:

1. **Who decides.** The loader. A change is volatile-only when `config` is the only
   option that changed, the fiber is ACTIVE, its context is unchanged and the raw configs
   are equal outside the fields the schema marks volatile (`entry.ts:148-151`). The
   loader then retains the raw config (`entry.ts:152`), an assignment that "does not
   validate, update the running instance or trigger a restart" (Cordis README:109).
2. **The object written.** `fiber.effectiveConfig`, "the object passed to activation"
   (Cordis README:109), which the core's fiber holds. `_commitVolatile` lists its
   volatile references (`entry.ts:169`) and writes into each with `updateVolatile`
   (`entry.ts:187`), which is "framework-owned" (cosmokit README:28).
3. **What is written.** The candidate resolved through the fiber's config hooks and
   schema, `fiber.resolveConfig(raw)` (`entry.ts:174`). That runs `internal/config`, on
   which the loader evaluates `!!js` expressions (`index.ts:104-110`), so an expression in
   a volatile field is evaluated again and its value is committed when it changed
   (`entry.ts:184-188`).
4. **A candidate that does not resolve** is logged, "leaves the running references
   unchanged; the raw config stays retained for the next activation" (`entry.ts:164`,
   `:175-178`). Nothing restarts.
5. **A value outside the volatile fields that differs** after resolution makes the loader
   apply "the ordinary update", a restart (`entry.ts:165`, `:180-182`).
6. **The fiber is told** through `loader/volatile-update` once every value is committed
   (`entry.ts:191-198`).

All of it runs in one JavaScript turn: no reader sees part of a commit and no activation
starts during one.

The port keeps the split of the reference: the kernel is the core's fiber and the loader
is a plugin. The kernel already holds two things the core holds: the retained raw config,
which G6 retains as the assignment of point 1 does, and the resolve of a candidate
(`vet`). Points 1 and 6 stay with the loader (`loader/apply.go:441-447`, `:104`). G13 adds
what the core holds as `effectiveConfig`, the config the running activation received,
and performs points 2 and 3 when the loader commits. It departs from the reference where
the following sections say:

- **Who writes.** The write is made by the kernel's commit, not by a separate call of the
  loader. In Go an activation can start, and a reader can read, while a commit runs, and
  only the kernel can order the write against the activations of the fiber (points 3 to 5
  of the decision). In Cordis an assignment to `rawConfig` writes nothing into the running
  object; under G13 a `Fiber.Commit` or a `CommitOp` does. The loader is the only caller of
  either outside the kernel, at `loader/apply.go:246` and `:442`, on `a3c85ae` and on
  `502a355`, and the first of these hands nothing off (point 3). The sentence of the
  plugin record §3 then reads: the loader decides and commits volatile values, and the
  kernel stores them in the running activation's fields at that commit. It is recorded as
  an amendment in §17 with the code (question Q1).
- **Point 4** is question Q2, **point 5** is question Q5, and the guarantee of one
  JavaScript turn is question Q3.

### What the kernel does today

G6 retains a committed config and restarts nothing, and "the running activation keeps the
value it was applied with" (line 1249). Each activation gets a value of its own:
`cfgCell.prepare` resolves the retained raw config through `internal/config` and `Decode`,
the `Apply` of `mountSpec` receives the result, and nothing keeps it. `Fiber.Commit`
resolves the candidate in `vet` when the fiber has a target and drops the result. On
`502a355`, `plugin.Volatile[T]` is `struct{ v T }` and `Get` returns the value decoded
for the activation; the documentation of the type says that a read returning a value
committed in place needs the ADR-034 addition that ADR-060 decision 3 names.

### Why no change outside the kernel is enough

- `Decode` belongs to the `Plugin` value, which every row of the plugin shares, and it
  also runs for a candidate before `internal/commit` may veto it. It cannot tell which
  activation a value is for or whether the commit completes, and it must be pure.
- `loader/volatile-update` is sealed, so every plugin with a volatile field would need an
  entry in `Config.Listeners`. It carries the raw config with every reference replaced by
  nil (`WithoutRefs`, `volatile.go:39-40`, under ADR-059 decision 17; plugin record §8),
  so a value that `internal/config` resolves would differ from what the next activation
  reads. Only the loader dispatches it, so a `Fiber.Commit` by any other holder of the
  fiber would never reach the field.
- The kernel hooks are sealed and carry no fiber identity.
- The loader cannot make the write itself, as the reference loader does: the two values a
  hand-off needs, the running activation's config and the committed config resolved as
  `Apply` would receive it, exist only inside the kernel, and only the kernel sees an
  activation start while a commit runs (point 4).

### Decision (proposed)

One optional function of a `Plugin`, stamped by a kernel function (question Q1):

```go
// WithCommit returns p with fn as its hand-off (G13). Plugin vet refuses a call of it
// in plugin code.
func WithCommit(p Plugin, fn func(applied, committed any)) Plugin
```

`plugin.Define` calls it for a config type that has a `Volatile` field; `plugin.Spec`
gets no such field. The kernel knows no `Volatile` field: it hands two opaque values, as
it hands `Apply` what `Decode` returns (owner decision 9), so `Volatile[T]` stays in
`sdk/plugin` as the plugin record has it (§3). A `Plugin` defined with `kernel.Define`
alone has no hand-off; plugin vet still allows "Plugin = kernel.Define or plugin.Define"
(`tools/kernelcheck/vet.go:291` on `502a355`).

**No author code runs as a hand-off.** `fn` is a function of `sdk/plugin`. Author code
passed to `WithCommit` would run at every commit, so plugin vet refuses the selector
`kernel.WithCommit` in plugin code with one entry of `forbiddenCalls`
(`tools/kernelcheck/vet.go:33-44` on `502a355`), as it refuses `time.Sleep`. The symbol is
new, so the rule refuses nothing that plugin vet accepts today (ADR-060 decision 4).

**What "No call" means.** `07:1377` is a row of the table "What reaches a running
plugin", whose next row names what a call is: "disposers in reverse, then `Apply` with
the new config" (`07:1378`). "No call" is read as no `Apply` and no lifecycle call of the
plugin. `Decode`, and `Spec.Validate` in it, which "must be pure, since it runs at every
activation and on every candidate update" (`sdk/plugin/plugin.go` on `502a355`), already
run at every commit of a fiber that has a target (G6's `vet`), as the reference resolves
the candidate through the fiber's config hooks and schema at every volatile commit
(`entry.ts:174`; Cordis README:109). G13 runs them once more in the cases of points 4
and 5.

1. **When.** After `Fiber.Commit` retains `cfg`, before it returns. After `Root.Batch`
   admits a batch, before it returns, for each `CommitOp` that is the last operation of
   the batch to carry a config of its fiber and whose fiber's activation the batch does
   not end (point 3). `Update`, `UpdateOpWith`, `UpdateOp` and a mount hand nothing off:
   they restart. A restart that `internal/update` vetoes leaves the running activation as
   it is (G2), holding the config it was applied with while the vetoed config is
   retained; the next hand-off to it carries the config of that later commit.
2. **What.** `applied` is the value that the running activation's `Apply` received, the
   same value at every hand-off to that activation. `committed` is `cfg` resolved through
   `internal/config` and `Decode`, the value an activation started at that moment would
   receive; `Commit` already resolves it in `vet` when the fiber has a target. A
   reference is resolved again for it, as `entry.ts:174` evaluates an expression again
   (point 7).
3. **To whom.** The running activation is the latest whose config was resolved for its
   `Apply`. An activation whose unload has begun (its `Std` context is done: it is
   unloading, FAILED or disposed) is handed nothing: its calls in flight finish with the
   values they have, and the next activation resolves the retained config. A `CommitOp`
   hands nothing off when its batch ends its fiber's activation: the batch disposes or
   restarts that fiber, or a fiber whose restart reaches it through a key that it provides
   or as an ancestor, at any depth. That is the closure that `prepareBatch` computes for
   G1's refusal of a `MountOp` (`sdk/kernel/owner.go:1487-1527`), today only for a batch
   with a child mount; under G13 it computes it also for a batch with a `CommitOp` of a
   fiber with a hand-off, over the whole batch, since the hand-off follows the admission.
   In the admitting turn the owner quiesces each fiber the batch updates or disposes and
   each consumer it restarts, whatever its children (`invalidateBatch`,
   `owner.go:846-866`; `quiesce`, `transition.go:218-224`), while a child of such a fiber
   keeps its activation until its parent's effects unwind; the closure covers both.
   The loader's checks of a config before it disposes the fiber (`loader/apply.go:246`)
   are such `CommitOp`s: for a row that gains its first reference the batch disposes the
   row's fiber, and for each child of a group it replaces, the group's. The running
   activation is handed nothing of a replacing config, which is not volatile-only, as
   `retireIn` already drops its `loader/volatile-update` ("an entry with no activation
   left is told nothing", `loader/apply.go:298`).
4. **Order, and nothing lost.** The kernel counts the retains of each fiber's config.
   Hand-offs to an activation follow that count, and an activation is never handed a
   config older than the one it holds: a commit whose retain a newer one has passed by the
   time it would hand off does nothing, and the newer one hands off. An activation whose
   resolve a newer retain overtook resolves again before its `Apply` runs, so no retain
   falls between an activation's resolve and the hand-offs it receives.
5. **A fiber with no target when the commit began.** `vet`, or `stageUpdates` for a
   `CommitOp`, resolves nothing then (G6), and an activation that began in that window
   applied an older config. The commit resolves `cfg` after the retain and hands it off
   (question Q2). When `cfg` does not resolve then, it hands nothing off and restarts
   nothing: the activation keeps its values, as `entry.ts:164` and `:175-178` keep the
   running references and `08:468` keeps the row's instance, and the next activation
   resolves the retained config and fails on the error, as the documentation of
   `Fiber.Update` states for a fiber with no target (`sdk/kernel/config.go:239-241`).
   `Commit` returns that error after the retain, as `Update` returns the error of its
   restart request after it retains; `Batch` has admitted its batch and does not.
6. **A panic of the hand-off** is contained as a panic of `Decode` is, as a
   `*PanicError`, and restarts nothing: the activation keeps what the hand-off stored
   before the panic, and the next activation applies the retained config. `Commit`
   returns it after the retain; `Batch` does not. Under the recommended answers no commit
   restarts a fiber, so G1's "a `CommitOp`, restart nothing" (line 1137;
   `sdk/kernel/root.go:188`) and G6's "does not restart the fiber" (lines 1248-1249) stay
   true, and a `MountOp` beside a `CommitOp` is admitted or refused as today.
7. **A `Volatile` field that holds a reference.** A change whose old or new value holds
   one is not volatile-only, and the loader restarts the row (decision 17): nothing is
   handed off. A reference that did not change, beside a literal that did
   (`volatile.go:38-39`), is resolved again for `committed` (point 2), and the hand-off
   stores the value it now resolves to, as `entry.ts:184-188` stores the new value of a
   volatile expression. A field that is not `Volatile` keeps the value its activation
   resolved, whatever it resolves to now; the next activation applies the new one, as G6
   has it today for every field, where `entry.ts:180-182` restarts the fiber. Question
   Q5.
8. **The loader keeps its ACTIVE-only rule.** `loader/apply.go:441` commits in place only
   when `only && active(ctx, cur.fiber)`, since "a fiber that is not ACTIVE runs no
   activation to tell", and restarts a row that is not ACTIVE with `UpdateOpWith`.
   `entry.ts:148-150` has the same rule: "Only an active fiber in an unchanged context
   takes volatile-only config changes without a remount." `07:1377` speaks of a running
   plugin, as the reference does. G13 changes neither the rule nor what a row sees; the
   hand-off to an activation that is still LOADING (point 4) serves a commit that races
   the start of an activation, not a choice of the loader.
9. **A plugin without a hand-off.** Nothing changes for it: no activation is recorded, no
   config is resolved again and G6 holds as written. Every `Plugin` of the differential
   corpus, the property workload and the call-path fixture is one.
10. **Remote and manifest plugins.** `plugin.FromManifest` and the proxy row of a remote
    plugin arrive at S2b (`07:1133`; ADR-060 decision 4). G13 gives a hand-off only
    through `plugin.Define`. Whether `FromManifest` gives a proxy row a hand-off, or the
    proxy forwards `loader/volatile-update` to the remote instance, is left to S2b; it
    depends on the remote protocol, ADR-041 (`00:775`).

### Concurrency contract

- **Readers.** The kernel is not on the read path. A read of a `Volatile` field is code of
  `sdk/plugin` (below), with no lock and no allocation. The call-path gate measures four
  operations, `lookup_view`, `owner_enter_leave`, `lookup_view_lease_call` and
  `timer_overhead` (`test/spike/s0/callpath/criteria.go:28-31`); none runs new code, nor
  does a call that ends in the outcome `refused_read` or `refused_enter`
  (`test/spike/s0/callpath/fixture.go:33`). `handle` and its lease shards keep their
  layout (`TestLeaseShardsOwnTheirCacheLines`).
- **The committer.** The hand-off runs on the goroutine that called `Commit` or `Batch`,
  never on the owner, concurrently with the goroutines of the activation. It reads and
  writes only what `sdk/plugin` allocated for the two values, never the author's `*C`
  (below). It must publish what it writes safely for concurrent readers, must not block
  and must not change the config of any fiber: it runs under the fiber's config change
  lock of G7, which orders it with that fiber's other config changes, so a change of the
  same fiber from inside it would wait for itself. This amends G7: `Commit` takes the lock
  to hand off as well as to retain (line 1263), and the hand-off runs under it (lines
  1269-1270). A panic is contained (point 6).
- **Locks.** The retain count, the running activation's value, its `Std` context and the
  retain it holds are fields of `cfgCell`, under the cell's plain lock, which the owner
  already takes to retain in a batch admission and which never covers plugin code. The
  order stays the config change lock, then the plain lock. `internal/config` and `Decode`
  run under no lock, as now.
- **Allocation.** A hand-off reuses the value that `vet` resolved, and an activation is
  recorded in fields of its cell: a read allocates nothing, and an activation that no
  retain overtakes allocates nothing more. The resolves of points 4 and 5 allocate as any
  resolve does; a batch that carries a config finds the last `CommitOp` of each fiber in
  a small map, and the closure of point 3 is the set `prepareBatch` builds. All three are
  control paths.
- **Liveness.** An activation resolves again only when a config was retained during its
  resolve. One that keeps losing to new retains is bounded by the load deadline, as a slow
  `Apply` is.
- **Extra resolves.** For a plugin with a hand-off, `internal/config` and `Decode` run
  once more in two cases: an activation whose resolve a retain overtook (point 4) and the
  window of point 5.

### What `sdk/plugin` does with it

This part is outside the kernel and its cap, and is counted against the cap of the
`plugin` package, 2,500 lines, provisional (ADR-060 decision 35).

- For a config type with a `Volatile` field, `Decode` returns a value of `sdk/plugin`
  that holds the decoded `*C` and, by path, the cell of each `Volatile` field it
  allocated; the `Apply` of `Define` passes the `*C` to `Spec.Apply`. The kernel hands
  that value to `Apply` and to the hand-off and never looks into it. The tests of
  `sdk/plugin` that read the result of `Decode` read the `*C` from it.
- `Volatile[T]` holds a pointer to its cell, an `atomic.Pointer[T]`, so a copy of the
  config reads the same cell (hence the `ConfigFor` rule of the requirement). `Get`
  loads the cell pointer, loads the cell atomically and dereferences the result: two
  dependent loads and a dereference, with no lock and no allocation. A zero `Volatile`
  returns the zero `T`.
- The hand-off pairs the cells of `applied` and `committed` by path and stores each
  committed value into the applied cell. It reads only those records, never the `*C`,
  which belongs to the author, who may change its slices and maps while a commit runs; a
  concurrent read of an author's map would be a fatal error that no `recover` catches. A
  path in one record only is skipped: a volatile-only change keeps the shape of lists and
  maps (`loaderdef.VolatileOnly`).
- A field that is not `Volatile` keeps the value `Apply` received (point 7).
- Each read returns the value of one retain, and two reads in a row may return values of
  two commits, or of one commit partly stored; fields that must change together go in one
  `Volatile` of a struct type (question Q3). A `T` that holds a slice or a map shares it
  between the committed value and every reader, and a reader must not modify it, as the
  reference freezes a volatile value ("`createVolatile()` copies and freezes validated
  data", cosmokit README:28). The documentation of `Volatile` states that rule.
- The documentation of `Volatile` drops "stays open until the owner accepts it". The
  exported API of `sdk/kernel` gains `WithCommit`, so `pluginApi` (`plugin.APIVersion`,
  `0.2` on `502a355`) takes its next minor (ADR-060 decision 8).

### Cost

No prototype of G13 is committed, so every figure below other than the two measured
counts is an estimate. The G13 commit runs `kernelcheck lines sdk/kernel 8200` and
`kernelcheck lines sdk/plugin 2500` and records their output in this section.

Measured: `sdk/kernel` has 8,043 physical lines of production Go at `a3c85ae` and at
`502a355`, 157 under its cap of 8,200; the `plugin` package has 1,084 at `502a355`, under
its cap of 2,500.

Estimated, under the recommended answers, for `sdk/kernel`:

| Part | Estimate |
|---|---:|
| `WithCommit`, its unexported field and their comments | 8 |
| The cell's hand-off fields and the retain count | 9 |
| The hand-off, with the resolve of point 5 and the check of point 4 | 35 |
| An activation resolves again after a retain (`cfgCell.prepare`) | 18 |
| A `CommitOp` hands off once `Batch` admits it, outside the closure of point 3 (`stageUpdates`, `prepareBatch`) | 26 |
| `Fiber.Commit`, and the comments of `Commit` and `CommitOp` | 6 |
| **Total** | **about 100** |

That would put the kernel at about 8,140 lines, with no new file and no new import. Q2 (b)
and (c) and Q5 (b) add a restart path from a commit and the change to G1's refusal it
needs; they are not estimated, and are measured before the owner is asked to choose one
of them. The part in `sdk/plugin`, the record that `Decode` returns, the cells by path, the
hand-off and `Get`, is estimated at 50 to 80 lines, which would put the package at 1,134
to 1,164.

### Tests

In `sdk/kernel`, a new file `s2a_handoff_test.go`:

1. `TestCommitHandsTheConfigToTheRunningActivation`: two commits on an ACTIVE fiber give
   two hand-offs with the same `applied`, the resolved configs in order and one `Apply`;
   a `Plugin` without a hand-off is handed nothing.
2. `TestCommitOpHandsItsConfigOnceTheBatchIsAdmitted`: a `CommitOp` hands off before
   `Batch` returns; a refused batch hands nothing; a `CommitOp` whose fiber the same batch
   disposes, restarts with `UpdateOp`, or changes later with `UpdateOpWith` hands nothing.
   Nor does a `CommitOp` of a child whose parent the batch disposes, placed before or
   after the dispose, or one of a consumer whose provider the batch restarts. The child
   case is the loader's check at `loader/apply.go:246` on a fiber that has children.
3. `TestARetainDuringTheResolveOfAnActivationResolvesAgain`: `internal/config` holds the
   first resolve of an activation while a `Commit` retains; that activation's `Apply`
   receives the committed config and nothing is handed off.
4. `TestCommitResolvesForAnActivationThatBeganWithoutATarget`: a fiber PENDING on a need;
   `internal/commit` holds a `Commit` while the provider arrives and the fiber becomes
   ACTIVE; `Commit` resolves and hands off, with no restart.
5. `TestCommitKeepsTheActivationWhenTheHandOffCannotBeMade`: in the window of test 4, a
   config that `Decode` refuses is handed nothing and restarts nothing; the activation
   keeps its values, `Commit` returns the error, and the next activation fails with it. A
   resolve that a newer retain passed hands nothing. A hand-off that panics, under
   `Commit` and under a `CommitOp`, restarts nothing; `Commit` returns a `*PanicError`,
   and the next activation applies the committed config.
6. `TestCommitHandsNothingToAnEndedActivation`: a FAILED fiber takes a commit with no
   hand-off and no restart.
7. `TestCommitsAndRestartsRaceToTheLastCommit`, under `-race`: 300 commits against
   concurrent `UpdateOp` restarts; no hand-off goes back to an older config, and once the
   fiber settles its activation holds the last commit.
8. `TestCommitAfterAVetoedUpdate`: `internal/update` vetoes an `Update`, and in a second
   case an `UpdateOpWith`; a `Commit` and then a `CommitOp` follow. The activation that
   began before the veto is handed each committed config, and nothing restarts.
9. `FuzzOperationSequence` gives its plugins a hand-off and gains `Commit` and `CommitOp`
   operations and an `internal/update` listener that vetoes on a byte of the input. After
   each settled step, each ACTIVE fiber's activation holds, in what its hand-offs stored,
   the config of the last `Commit` or `CommitOp` that was retained after that activation
   resolved its own and that resolved, or else the config it was applied with. A vetoed
   update retains a config that nothing hands off, so "the retained config" is not the
   invariant.
10. The tests of G6 and G7 (`commit_test.go`, `serialise_test.go`, `s1_batch_test.go`)
    stay as they are; they hold G6 for a plugin without a hand-off.

In `sdk/plugin`: `Get` returns a value committed in place, through a copy of the config as
well; `Get` allocates nothing (`testing.AllocsPerRun`) and has a benchmark; `Get` and a
commit run concurrently under `-race`; a field that is not `Volatile` keeps the applied
value; an `Apply` that changes the slices and maps of its `*C` while commits run passes
under `-race`; and a `Volatile` field whose value an `internal/config` listener resolves
to something new takes it while a sibling literal changes (Q5 (a)). In
`tools/kernelcheck`: plugin vet refuses `kernel.WithCommit` in plugin code.

End to end, in `test/loader-smoke`, one of the S1 suites. On `502a355` the toy consumer of
`test/dist` reads its `note` only in `Apply` (`test/dist/plugins/toy-consumer/plugin.go`),
so a run has nothing to observe after `Apply`. The fixture changes: the toy consumer gains
a second `Volatile` field, `tag`, and logs `note` and `tag` at each call of its load
goroutine (`callEvery`); its `plugin.yaml` is regenerated, with `tag` marked volatile. The
suite then checks three cases:

- a live generation that changes only the `note` of a row with `callEvery` set: reload
  reports the row `committed`, no second "toy-consumer applied" line is logged for it,
  and a later call logs the new note;
- a row whose `note` is `"${file:note}"`: the harness rewrites the file, and a generation
  that changes only `tag` commits in place, after which a call logs the new tag and the
  file's new content (Q5 (a));
- a generation that replaces a group whose child keeps its id and plugin and gets a new
  note: the child's running activation never logs the new note, and the activation that
  replaces it does.

### How the work is run

- G13 belongs to the first group of ADR-060 decision 32, the authoring surface and the
  definitions, and lands on `s2a/integration`.
- Test first: kernel tests 1 to 9, the `sdk/plugin` and `tools/kernelcheck` tests and the
  three `test/loader-smoke` cases are committed failing before the code that makes them
  pass.
- The adversarial review round of that group covers G13, each finding confirmed by a
  reproduction, and the group merges into `main` only when that round leaves no open
  finding.

### Verification plan

ADR-059 decision 2 and ADR-060 decision 3, on the commit of `s2a/integration` that carries
G13, with the evidence under `test/spike/s0/results/` as for G1 to G12:

1. **Kernel and suites.** `make check` (the kernel tests under `-race`, full statement
   coverage, `kernelcheck lines sdk/kernel 8200`, the vocabulary against the list of
   `s2a/integration`, and the imports) and `make s1-suites`, since the loader commits in
   place through `CommitOp`.
2. **Differential corpus.** The live oracle comparison under the causal comparator at
   `GOMAXPROCS=8` and at `GOMAXPROCS=2`, expected to give the result of
   `causal-main-merged`: the adapter's plugins have no hand-off, so every case runs the
   code of G6 (point 9).
3. **Property workload.** The full run, 1,000 seeds × 1,000 operations on the three
   backends under `-race`, in a frozen checkout. It mounts with no config and commits
   nothing, so it shows that the lifecycle did not move.
4. **Call-path gate.** A full run on the measurement host, in one session alternating by
   process with the `main` commit it will merge into, judged against it as in the section
   "2026-11-22 call-path gate on the merge commit", and run again on the merge commit into
   `main`. No measured operation runs new code, so a difference beyond the spread of the
   slow mode described there is a finding to explain before the merge.
5. **The hand-off itself.** Tests 1 to 9, and `FuzzOperationSequence` run for 30 minutes,
   with its duration and result recorded beside the other runs (question Q4).

### Edits that come with the code

- `sdk/kernel`: the documentation of `Fiber.Commit` ("the running activation keeps the
  value it was applied with", "a plugin learns the new value from whoever commits it"), of
  `CommitOp` and of `cfgCell`, whose invariant that `cmu` "is held across nothing but the
  internal/update hooks, the core's admission and plain assignments"
  (`sdk/kernel/config.go:40-41`) gains the hand-off.
- G6 (lines 1248-1251): "the running activation keeps the value it was applied with and
  its `Apply` is not called again" then holds for a plugin without a hand-off; with one,
  the running activation's `Volatile` fields read the committed values and its `Apply` is
  not called again. "A plugin learns the new value from whoever commits it (the loader
  dispatches `loader/volatile-update`)" gains: and from its `Volatile` fields.
- G7: "`Update` and `Commit` take it only to retain" (line 1263) and "no other plugin code
  runs under it" (lines 1269-1270), as the concurrency contract states.
- G1 (line 1137) and `sdk/kernel/root.go:188`, "a `CommitOp`, restart nothing", stay
  as written under the recommended answers; under Q2 (b) or (c), or Q5 (b), they say
  which `CommitOp` restarts and G1's refusal counts it.
- Under Q1 (a) only: `07:192`, `sdk/kernel/root.go:17` and the `Plugin` sketch of plugin
  record §4, "plain data plus two functions", gain the third function. Under Q1 (b) they
  stay as written: the hand-off is an unexported field, as the entry token is.
- `sdk/plugin`: the package documentation, that of `Volatile`, and that of
  `Spec.Validate`, which runs at every commit too.
- `sdk/def/loader/v0/volatile.go:37-38`, "config-refs resolves them only when a fiber
  activates", becomes "when a fiber activates, and again when a commit hands a `Volatile`
  value off" (Q5 (a)).
- The test distribution's toy consumer and its `plugin.yaml` (Tests).
- The plugin record, as amendments in §17: the sentence of §3 on who commits volatile
  values, as stated in "What the reference loader does"; and §15 row 16 from "As is" to
  "Adapted", with what is lost under Q3 and Q5, and `08:58` from "carried" to "adapted".
- This section records the shape as merged and the measured line counts, as the section
  "S1 additions to the kernel API (ADR-059)" does for G1 to G12.

### Questions for the owner

Each question gives its options, the recommended one, why, and when the answer is needed.

**Q1. What shape does the hand-off take in the kernel API, and how is author code kept out
of it?** `07:192` lists the fields of `Plugin` as "plain data plus two functions", and
`07:68` (R10) leaves the accessors of `plugin.Volatile[T]` as sketches. A function that the
kernel runs at every commit must not be author code. Accepting any option also accepts the
amendment of plugin record §3 stated in "What the reference loader does".
- (a) An exported optional field `Plugin.Commit func(applied, committed any)`, set by
  `plugin.Define`; `07:192`, `root.go:17` and plugin record §4 list it. Any code can set
  it, `kernel.Define(kernel.Plugin{Commit: ...})` included. Plugin vet can find the key
  `Commit` in a `kernel.Plugin` literal, but not an assignment to the field of a
  variable, which needs the variable's type: plugin vet type-checks only the standard
  library and the vetted tree (`tools/kernelcheck/vet.go:67-84` on `502a355`), so in a
  plugin outside this repository `sdk/kernel` stays unresolved.
- (b) An exported function `kernel.WithCommit(p Plugin, fn func(applied, committed any))
  Plugin` that stamps an unexported field, as `Define` stamps the entry token (owner
  decision 7). `07:192` stays as written. Any code can call it, and plugin vet refuses the
  selector in plugin code with one `forbiddenCalls` entry.
- (c) A method `Context.OnCommit(fn func(committed any))` that the `Apply` of
  `plugin.Define` calls in each activation. `Plugin` stays as it is, but any `Apply` can
  call it, and plugin vet cannot find a method call without the receiver's type. The
  kernel must also hand over what was retained before the registration, which costs more
  lines.
- Recommended: (b). It is the only option whose exposure plugin vet closes with a rule of
  a form it already has, it leaves `07:192` and the plugin record's `Plugin` as written,
  and it follows the precedent of the entry token. Needed before the G13 code lands on
  `s2a/integration`.

**Q2. What does a commit do for an activation that began while it ran, on a fiber that had
no target when it began?** G6 resolves nothing for a fiber with no target, and such an
activation applied the older config. In every option, a commit whose retain a newer one
has passed does nothing (point 4).
- (a) Resolve `cfg` after the retain and hand it off. When `cfg` does not resolve, hand
  nothing off and restart nothing: the activation keeps its values, and the next
  activation resolves the retained config and fails on the error, as the documentation of
  `Fiber.Update` states for a fiber with no target (`config.go:239-241`). This is what
  `entry.ts:164` and `:175-178` do and what `08:468` states for a reference that does not
  resolve: "Rejected; the row keeps its instance".
- (b) As (a), but restart the fiber when `cfg` does not resolve, as `UpdateOp` does. The
  running activation ends for a config that `08:468` says it keeps. A `CommitOp` that
  restarts after its batch is admitted breaks G1's refusal of a `MountOp` under that
  activation, so G1 must count a `CommitOp` of a fiber with no target as a restart and
  then refuses batches that it admits today; G1 (line 1137), G6 (lines 1248-1249) and
  `root.go:188` are amended.
- (c) Restart the fiber in that window without resolving. Every valid volatile change in
  that window restarts the row, against "no restart" of `07:1377`, with the G1
  consequence of (b).
- Recommended: (a). It is what the reference loader does and what the failure matrix
  states, no commit restarts a fiber, and G1 and G6 keep their text. Needed before the
  G13 code lands on `s2a/integration`.

**Q3. What does a read return when one commit changes several `Volatile` fields?** The
reference loader stores every volatile value of a commit in one JavaScript turn
(`entry.ts:184-189`), so no reader sees part of a commit, and the plugin record marks
row 16 "As is". Go has no such turn: under either option below, two `Get` calls in a row
may straddle a commit. Either option therefore changes §15 row 16 from "As is" to
"Adapted", with the loss stated, and accepting one accepts that departure.
- (a) Per field: each `Volatile[T]` has its own cell and a commit stores them one after
  another, so two reads may return values of two commits, or of one commit partly stored.
  Fields that must change together go in one `Volatile` of a struct type. `Get` is two
  dependent loads and a dereference.
- (b) Per config: all `Volatile` fields of an activation read through one pointer, which
  a commit swaps once, and `sdk/plugin` gains an accessor that reads several fields from
  one commit; two `Get` calls without it may still straddle a commit. `Get` is three
  dependent loads (the shared cell, the snapshot, the slot) and a dereference, and the
  accessor is new author API.
- Recommended: (a), with the departure accepted. Neither option gives every sequence of
  reads the guarantee of one JavaScript turn. (a) gives it to the fields a plugin groups
  in one struct, decided where the config type is written, at the lowest cost on a read;
  (b) gives it only to code that calls a new accessor. Needed before the `sdk/plugin`
  part of G13 lands on `s2a/integration`.

**Q4. Do the S0 harnesses check the hand-off itself?** The corpus and the property
workload commit nothing in place: the adapter never calls `Fiber.Commit` or `CommitOp`,
the workload mounts with no config, and the two prototype cores have no such operation.
- (a) Run the three gates unchanged as evidence that nothing else moved, and check the
  hand-off in the kernel package: tests 1 to 9 and a recorded 30-minute run of
  `FuzzOperationSequence`.
- (b) Also add a commit-in-place operation to the property workload for the `kernel`
  backend alone, with the retained configs in its independent model, which changes the
  workload's frozen source digest.
- Recommended: (a). The oracle and the two prototypes give nothing to compare a hand-off
  with, and (b) changes frozen S0 evidence for a check that a fuzz target of the kernel
  package makes directly. Needed before the G13 code merges into `main`.

**Q5. What does a hand-off do with a value that `internal/config` resolves again?**
`committed` is resolved through `internal/config`, where `config-refs` resolves
`${env|file|secret|flag}` references (`plugins/config-refs/plugin.go:79`), so a reference
that did not change may resolve to a new value. The reference loader evaluates its
expressions again at a volatile commit (`entry.ts:174`; `index.ts:104-110`), stores a
volatile value that changed (`entry.ts:184-188`) and restarts the fiber when a value
outside the volatile fields changed (`entry.ts:180-182`). The loader definition states that
a reference is resolved "only when a fiber activates" (`volatile.go:37-38`).
- (a) Store every `Volatile` field of the resolved config, a reference resolved again
  included, and leave every other field to the next activation, as G6 does today. This is
  `entry.ts:184-188` for a volatile field and departs from `entry.ts:180-182`;
  `volatile.go:37-38` is amended. No commit restarts a fiber, and it needs no code beyond
  the rest of G13.
- (b) As (a), and restart, as `entry.ts:180-182` does, when the resolved config differs
  from the applied one outside its `Volatile` fields. `sdk/plugin` keeps the resolved tree
  beside each decoded value to compare. `Fiber.Commit` then restarts as `Update` does;
  for a `CommitOp`, `stageUpdates` compares before the batch is admitted and turns the
  operation into an update, which G1's refusal counts. G1 (line 1137), G6 (lines
  1248-1249), G7 (line 1263), the `cmu` invariant and `root.go:188` are amended. It adds
  a restart that `07:1377` does not list, a reference outside the `Volatile` fields that
  resolves to a new value; and an activation that began between the comparison and the
  admission is handed the `Volatile` fields with no comparison.
- (c) Keep the applied value of each `Volatile` field whose committed raw value holds a
  reference: the kernel also hands the retained raw config to the hand-off, which skips
  such a field, and the loader restarts a change inside a `Volatile` field that holds a
  reference, reading decision 17 at the field ("the old or the new value of a volatile
  field"). A reference is then resolved only when a fiber activates, as
  `volatile.go:37-38` states. This departs from `entry.ts:184-188`, and a literal changed
  inside such a field restarts the row where `volatile.go:38-39` commits it in place
  today.
- Recommended: (a). It keeps every clause of `07:1377` and decision 17: a change that holds
  a reference still restarts, a literal beside one still commits in place, and the next
  read returns what was committed, with the value its reference has now, as the reference
  loader gives a volatile expression. Its departure, `entry.ts:180-182`, is the behaviour
  that G6 already has for every field; (b) adds a restart that `07:1377` does not list,
  and (c) one that `volatile.go:39` does not have. Accepting (a) records the departure
  from `entry.ts:180-182` in §15 row 16 beside that of Q3. Needed before the G13 code
  lands on `s2a/integration`.
