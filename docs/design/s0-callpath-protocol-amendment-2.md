# S0 callpath measurement protocol, amendment 2

Date: 2026-11-19. Decision: owner. Scope: how `test/spike/s0/callpath` measures the
frozen call-path gate of [s0-spike-criteria.md](s0-spike-criteria.md) section 8.3 and
reads the "Noise and invalid runs" paragraph of section 4. It builds on
[amendment 1](s0-callpath-protocol-amendment-1.md), which stays in force except where
this file says otherwise. This file is written before the harness change that
implements it and before any run under it.

## Why

The third full run
([test/spike/s0/results/callpath-full-third](../../test/spike/s0/results/callpath-full-third/),
`invocation.json` and `report.json`) is the first under amendment 1, on an idle
dedicated host with GOMAXPROCS=8.

- The latency bound was met in every run: owner Enter+Leave p99 255 to 809 ns in
  aggregate, worst single run 1188 ns, budget 2000 ns.
- The timer-only control varied by 1.9 to 4.0% in throughput (12 to 18% before
  amendment 1). The host was quiet.
- Owner Enter+Leave allocated zero objects in 59 of 60 runs. The exception is
  `reconciler`, 1 caller, run 0: one object, with a control of zero, which amendment 1
  classifies as FAIL. The second full run failed at exactly the same place.
- Four baseline arms varied above 10% with a clean control: graph-lock owner
  Enter+Leave at 8 callers 16.2%, graph-lock lookup+lease+call at 8 callers 10.2%,
  reconciler lookup at 32 callers 12.8%, reconciler lookup+lease+call at 32 callers
  11.3%.

Two development observations (not acceptance evidence) explain the two findings:

1. Six development dry runs printed per-run allocation counts. Non-zero readings
   occurred only in repetition 0 and only for `reconciler`, the candidate measured
   first in repetition 0. Candidate order alternates per repetition, so `graph-lock`
   is first in repetition 1 and showed nothing. Once the stray object landed in the
   reconciler's own timer control. It is therefore a start-of-process effect on the
   first measured windows, not the reconciler's lease path.
2. An earlier analysis found that when callers truly overlap, throughput correlates
   (about -0.65) with how many callers share a cache line in the 16-shard lease
   counter. The shard is chosen by hashed stack address and differs per run. That is
   a property of the candidate, not of the host, and the timer-only control (which
   touches no counter) does not carry it.

The three earlier full runs stay recorded exactly as they are, with their
classifications. They are not reclassified and not re-evaluated under this
amendment. Records of the first and second run carry no amendment field (read as
amendment 0); the third carries `protocolAmendment: 1`.

## What does not change

Thresholds: owner Enter+Leave p99 <= 2 us in aggregate and in every run; zero
allocation per operation for owner Enter+Leave; caller counts 1, 8 and 32; at least
10 independent repetitions; at least 1,000,000 observations per arm (100,000 per
operation, candidate, caller count and repetition); GOMAXPROCS=8; no race detector.
The three operations and the timer control, both candidates, alternation, the stress
arm, evidence and identity requirements, the 10% rule value, swap and cgroup throttle
counters, classification precedence fail > invalid > inconclusive > pass, and refusal
of a reduced plan outside dry-run. `criteria.go` and `TestFrozenProtocol` are
untouched. Amendment 1's allocation rules stay: control non-zero for a run makes that
run invalid for allocation; control zero and lease non-zero is FAIL; a missing control
reading is inconclusive.

## 1. Warm-up repetition

**What is executed.** Once, at process start, before measured repetition 0: one
complete baseline repetition for every candidate, with exactly the windows of a
measured baseline repetition: the same operations (lookup, owner Enter+Leave,
lookup+lease+call, timer control), caller counts 1, 8 and 32, sample count per window,
`runWindow` path, fresh fixture, and host sampling. Candidates run in the order of
repetition 0.

**Why one pass covers both alternation orders.** The stray objects followed "the first
candidate measured in the process". What has to be true is that every window shape
(candidate, operation, caller count) has been exercised once in this process before
its first measured window. A baseline repetition runs every shape of both candidates,
so one warm-up repetition does that; which candidate or operation comes first within
it does not change the set of shapes touched. Running the second alternation order as
well would repeat the same shapes and add nothing.

**What it does not include.** The stress arm. Stress windows are 120 s each, carry no
allocation or throughput-variation classification, and are correctness-gated, so a
start-of-process transient there cannot change any classified reading. A stress warm-up
would cost twelve minutes to protect nothing that is classified. Stress correctness is
checked in every measured repetition as before. The baseline warm-up adds only the
short baseline windows (no stress) to the run, small next to the two-hour stress time.

**Recording.** Its raw records, one per candidate, are written to the output directory
with `warmup: true` and the label `warmup`, in files named
`baseline-<candidate>-warmup.json`, carry `protocolAmendment: 2`, and are listed in the
report under `warmup` with each arm's allocation count, throughput and outcome counts,
so the owner can see them. They are fsynced and appear in the manifest.

**Exclusion.** Warm-up records are excluded from every percentile, aggregate,
allocation reading, variation (CV) and observation count used for classification. The
10 measured repetitions and the 1,000,000 observations per arm are met without the
warm-up; a plan whose measured repetitions fall short is inconclusive however many
warm-ups ran.

**Integrity still counts.** Warm-up excludes measurements, not correctness. A failure
of integrity inside the warm-up still fails the run: an error outcome, a refused
observation in the mutation-free baseline, an inactive-owner body execution, a fixture
or close error, or any harness error (including failure to write the record). Each is
reported exactly as in a measured repetition. Its protocol amendment also takes part in
the refusal to mix amendments. Host swap or throttle observed during the warm-up is
reported but, being a measurement, does not invalidate the measured runs.

**What the warm-up must not hide.** A candidate that allocates on its first use of an
activation after warm-up, for example per-activation lazy state, is still caught. Every
measured repetition mounts its own fresh fixture (`NewFixture` is called per baseline
run and per stress run, never shared with the warm-up or another repetition), so each
measured repetition starts with a new provider activation, new consumer binding and new
lease counters. What is warmed is the process: runtime, scheduler, timer heap, GC
state, code and data caches, per-P caches. Anything a candidate does once per
activation happens again in each measured repetition, inside a measured window, and a
non-zero reading there fails the run when the control is zero, as before. Each fixture
carries a sequence number, recorded in the baseline records, so a test (and a reader)
can see that the warm-up and every measured repetition used distinct fixtures.

## 2. Noise rule

The frozen sentence reads: throughput coefficient of variation greater than 10% across
independent runs, or observed host CPU throttling or swapping, requires a repeat of the
unchanged protocol before a performance conclusion. Its purpose is to detect an
environment that cannot support a performance conclusion. This amendment reads the
variation half of the rule from the arm that measures the environment: the timer-only
control of the same candidate and caller count, which runs the same callers, windows
and bracketing code with an empty operation.

For every candidate and caller count:

- control variation (CV of the ten per-run throughputs) above 10%: the environment is
  noisy. Every measured arm of that candidate and caller count is INVALID, and the
  unchanged protocol is repeated, as the frozen sentence says. Swap or cgroup throttle
  observed in a run stays INVALID exactly as before.
- control variation of 10% or less: the host is quiet. The measured arm's own variation
  is still computed and reported. Above 10% it is a characteristic of the candidate,
  not an invalid run. It is recorded in the aggregate as `candidateVariation` with its
  value, listed in the report's reasons as an `OBSERVATION` (neither FAIL nor INVALID),
  and so reaches the summary the owner reads when choosing a core. It does not change
  the classification.
- control missing or its variation not computable: INCONCLUSIVE.

**What this gives up.** A noisy environment that disturbs contended arms but not the
control would go undetected by the variation rule. That is unlikely: the control runs
the same callers on the same Ps in the same window shape, in the same fixture and run
as the measured arms, so frequency scaling, a noisy neighbour or throttling hits it too;
what it does not carry is contention on shared candidate state, which is precisely a
candidate property. Three further guards remain: the swap and cgroup throttle counters
for every run; the latency bound, which must hold in every single run (a disturbed
window shows up as a tail above 2 us and fails, never averaged away); and the owner's
own reading of the reported `candidateVariation` values. The observation is reported
rather than hidden so that a candidate whose throughput depends on placement-dependent
sharing is visible, not excused.

## 3. Records and evaluation

Every baseline, stress, identity and report record carries `protocolAmendment: 2`. The
evaluator refuses a set of runs that mixes amendments, and refuses to classify as pass
any run whose amendment is not the current one: that is INVALID with the reason
recorded. In particular a report whose records carry amendment 1 (or 0) is not
evaluated under the amendment 2 rules. This document is added to the source-closure
digest. The dry-run plan carries the same warm-up at its reduced size.

## 4. Validation before the next full run

1. Ordinary `go test`, deterministic, no timing thresholds: warm-up records excluded
   from every classification input while a warm-up integrity failure still fails;
   measured repetitions use fresh fixtures; clean control plus noisy arm is pass-eligible
   with a `candidateVariation` observation, noisy control invalid, missing control
   inconclusive; amendments are never mixed.
2. Development dry runs on a shared host, labelled NOT ACCEPTANCE, reporting per-run
   allocation counts in the measured repetitions (is repetition 0 clean) and the control
   and arm variations. No dry-run result is committed.
3. The full run is the owner's, on the idle host, with the unchanged command in
   `test/spike/s0/callpath/README.md`.
