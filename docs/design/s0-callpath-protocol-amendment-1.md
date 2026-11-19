# S0 callpath measurement protocol, amendment 1

Date: 2026-11-19. Decision: owner. Scope: how `test/spike/s0/callpath` measures the
frozen call-path gate of [s0-spike-criteria.md](s0-spike-criteria.md) section 8.3 and
the "Noise and invalid runs" paragraph of section 4. This file is written before the
harness change that implements it and before any run under it.

## Why

[ADR-034, "Second full callpath run and what it cannot yet decide"](../adr/ADR-034-kernel-concurrency.md)
records an analysis of the second full run. Two readings of that run come from the
measurement, not from the lease:

1. The one stray object in 4 of 60 runs is allocated by the harness. The coordinator
   goroutine parks in `sync.WaitGroup.Wait` inside the measured window, and the
   runtime allocates a wait-queue record (`runtime.acquireSudog`) when the per-P
   cache is empty. The empty timer-only control loop shows the same rate as the
   lease loop.
2. Throughput variation above 10% at 8 and 32 callers appears in the timer-only
   control too. A window is a few milliseconds; callers start staggered through a
   channel close and finish at different times; throughput was taken from the
   coordinator's wall clock and tracks the finish skew (correlation about -0.8) in
   every variant including the control.

The two earlier full runs
([first](../../test/spike/s0/results/callpath-full-first/),
[second](../../test/spike/s0/results/callpath-full-second/)) stay recorded exactly
as they are, with their classifications. They are not reclassified and not
re-evaluated under this amendment. Their records carry no protocol amendment field,
which reads as amendment 0 (the original protocol).

## What does not change

Criteria and thresholds (owner Enter+Leave p99 <= 2 us in aggregate and in every
run, zero allocation, the 10% throughput-variation rule, swap and cgroup-throttle
counters, GOMAXPROCS=8, no race detector); the three operations and the timer
control; both candidates; caller counts 1, 8 and 32; ten independent repetitions;
100,000 observations per operation, candidate, caller count and repetition (1,000,000
per arm); candidate and arm alternation; the stress arm and its accounting; evidence
and identity requirements; classification precedence fail > invalid > inconclusive
> pass; refusal of any reduced plan outside dry-run. Every observation stays
recorded: no sampling, reservoir, dropping or subtraction. `criteria.go` constants
and `TestFrozenProtocol` are untouched.

## Window length

Unchanged: 100,000 observations per run. Longer runs would raise the raw record
size in proportion (the baseline arm already writes about 24 million latencies) and
change the frozen plan counts for a benefit that the first run under this amendment
can measure. Per-caller windows are short at 32 callers (about 3,125 operations per
caller); if the first run under this amendment still shows control variation above
10% with callers released together, a longer window is the next amendment to
consider, decided on that data, not assumed here.

## Allocation reading

The bound is unchanged: zero objects per run for owner Enter+Leave, read as the
process-wide `Mallocs` delta between two `runtime.ReadMemStats` calls that bracket
the window, per run, never averaged, with no control subtraction.

The measured window now contains no allocation by the harness. Before the first
`ReadMemStats` the harness creates and warms everything:

- all caller goroutines, the latency, outcome and span arrays, and every atomic
  counter or flag the window uses;
- the coordinator's own sleep timer: the coordinator calls `time.Sleep` at the
  poll interval several times, so the goroutine's timer record exists and the
  timer-heap slot it needs has been grown;
- a `runtime.GC()` as today.

Inside the window the harness does exactly: one atomic store releasing the callers,
one `time.Now`, then a loop that reads an atomic completion counter and, while it is
below the caller count, calls `time.Sleep(250 us)`. It never parks on a
`sync.WaitGroup`, channel, mutex or condition, so it never acquires a wait-queue
record. No goroutine, channel, map or slice is created or grown in the window, and
callers never allocate: a caller loop is atomic load, `time.Now`, the operation,
`time.Now`, store into a preallocated slot, and one atomic add when done.

Timing error: the coordinator notices completion up to one poll interval (250 us)
plus any wait for a free P after the last caller finishes. This only lengthens
`wallNS`, which is recorded for information and is no longer an input to any
reading (throughput comes from the callers' timestamps, below). Latency observations
are taken by the callers around exactly one operation call, with the callers'
own monotonic clock, so coordinator timing cannot enter them. A timer wake-up does
not interrupt a running caller: it runs on an idle P or when a P next enters the
scheduler, and the runtime preempts a running goroutine only after 10 ms, longer
than a window. Any such displacement that did occur would be inside a caller's
latency and stays there, in the lease and the control alike.

REQUIRED control: the timer-only loop (empty operation, same callers, same sample
count, same bracketing code, same run) reads the same counter. Per run:

- control zero, lease zero: allocation bound met for that run;
- control zero, lease non-zero: FAIL (the lease path allocated);
- control non-zero: that run is INVALID for allocation (environment or runtime), not
  a pass and not a lease failure, whatever the lease reading; the run is repeated
  unchanged and its data is preserved.

A missing control reading for a run is inconclusive. The control is read per run,
not pooled. Lookup and dispatch allocations remain reported only.

## Noise reading

Callers are released together by a start barrier. Each caller creates and signals
ready (outside the window), then loops `for !start.Load() { runtime.Gosched() }`.
The coordinator waits for every caller to signal ready, then stores the flag once,
after the first `ReadMemStats`. The `Gosched` yield lets callers beyond the number
of Ps reach the flag check at all.

Callers up to GOMAXPROCS (1 and 8 on 8 Ps) are all on a P and see the flag within a
yield of the store. Callers beyond GOMAXPROCS (32 on 8 Ps) cannot all be on CPU. The
deterministic rule is: the flag flips once, after all 32 are ready; callers not on a
P when it flips start when the scheduler gives them one (a yield by a spinning
caller, or the completion of an earlier caller), and no start simultaneity is claimed
or recorded for them. Nothing depends on it, because throughput uses each caller's
own span.

- Caller span = that caller's last operation end minus its first operation start,
  both from its own monotonic `time.Now`. Time spent before its first operation
  (waiting for the flag or a P) is outside the span; time off-CPU inside it is in it.
- Caller throughput = that caller's observations divided by its span.
- Run throughput = the sum of its callers' throughputs, recorded with the spans.

The coefficient of variation is computed over the ten independent runs of an arm
exactly as before (`scale.CV`), and above 10% is invalid and requires a repeat of
the unchanged protocol.

Run throughput is a noise statistic, not a capacity claim: with more callers than
Ps, callers that run in successive batches still each contribute their own rate, so
the sum exceeds what the host could do at once. That is the same in every run and
does not enter the CV.

Why this measures host noise: finish skew and start skew (channel-close staggering,
which callers get a P first) are outside each caller's own span, so they no longer
move the number. What stays inside a span is what the unchanged protocol means by
noise: frequency scaling, a noisy neighbour or throttling that slows the callers'
own operations, and a caller descheduled for part of its span. The swap and cgroup
throttle counters stay as they are, with the same invalid reading.

## Records

Every baseline and stress record, `identity.json` and `report.json` carry
`protocolAmendment: 1`. Raw arm records add the per-caller spans. The evaluator
refuses to classify as pass a set of runs that mixes amendments, or any run whose
amendment is not the current one: that is invalid, with the reason recorded, never
averaged. The amendment document is added to the source-closure digest.

## Validation before the next full run

1. Ordinary `go test` semantic tests, deterministic with no timing thresholds: the
   wait primitive does not allocate (`testing.AllocsPerRun`) and a looped window
   with no or trivial callers shows nothing from the harness; the barrier releases
   all callers only after all signalled ready; per-caller throughput arithmetic; a
   non-zero control makes a run invalid while a non-zero lease with a zero control
   fails; mixed amendments are refused. Race and non-race, repeated.
2. Dry runs on a development host, labelled NOT ACCEPTANCE, compared with the ADR
   numbers. They check the pipeline and the direction of the change, not the gate.
3. The full run is the owner's, on the idle host, with the unchanged command in
   `test/spike/s0/callpath/README.md`. No acceptance or dry-run result is committed
   with the amendment or its implementation.
