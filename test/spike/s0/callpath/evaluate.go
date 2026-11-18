// SPDX-License-Identifier: Apache-2.0

package callpath

import (
	"fmt"

	"github.com/mutex-solutions/aiops-platform/test/spike/s0/scale"
)

// Aggregate combines every repetition of one arm/candidate/operation/concurrency.
// Dist is nearest-rank over ALL observations; PerRun keeps each run's own.
type Aggregate struct {
	Candidate   string               `json:"candidate"`
	Arm         string               `json:"arm"` // baseline, stress or timer.
	Operation   Operation            `json:"operation"`
	Callers     int                  `json:"callers"`
	Runs        int                  `json:"runs"`
	Count       int                  `json:"count"`
	Dist        scale.Distribution   `json:"aggregate"`
	PerRun      []scale.Distribution `json:"perRun"`
	Mallocs     []uint64             `json:"mallocsPerRun"`
	AllocsPerOp float64              `json:"allocsPerOp"`
	NonOK       int                  `json:"nonOKObservations"`
	Refusals    int                  `json:"refusals"`
	Errors      int                  `json:"errors"`
	Throughput  []float64            `json:"throughputPerSecondPerRun"`
	CV          float64              `json:"throughputCV"`
}

// EvalInput is everything classification depends on: no clock, no filesystem.
type EvalInput struct {
	Plan             Plan
	Dry              bool
	Candidates       []string
	IdentityProblems []string
	Invalid          []string
	Aggregates       []Aggregate
	Stress           []StressSummary
	Hosts            []HostDelta
	EvidenceProblems []string
	RunErrors        []string
}

// Evaluate classifies. Precedence is fail > invalid > inconclusive > pass, and
// pass requires the frozen plan plus complete identity, noise and raw evidence.
// A candidate failure is never downgraded to environmental noise.
func Evaluate(in EvalInput) (scale.Classification, []string) {
	var fail, invalid, inconclusive []string
	if in.Dry || !in.Plan.Frozen() {
		inconclusive = append(inconclusive, "reduced or dry-run plan: never acceptance")
	}
	fail = append(fail, in.RunErrors...)
	invalid = append(invalid, in.Invalid...)
	inconclusive = append(inconclusive, in.IdentityProblems...)
	inconclusive = append(inconclusive, in.EvidenceProblems...)

	find := func(arm, cand string, op Operation, callers int) *Aggregate {
		for i := range in.Aggregates {
			a := &in.Aggregates[i]
			if a.Arm == arm && a.Candidate == cand && a.Operation == op && a.Callers == callers {
				return a
			}
		}
		return nil
	}
	covered := func(a *Aggregate, label string) bool {
		if a == nil || a.Runs < MinRepetitions || a.Count < MinObservations || len(a.PerRun) != a.Runs {
			have := 0
			if a != nil {
				have = a.Count
			}
			inconclusive = append(inconclusive, fmt.Sprintf("%s: fewer than %d runs / %d observations (have %d)", label, MinRepetitions, MinObservations, have))
			return false
		}
		return true
	}
	for _, callers := range CallerConcurrencies() {
		for _, cand := range in.Candidates {
			covered(find("timer", cand, Timer, callers), fmt.Sprintf("timer overhead %s/c%d", cand, callers))
			for _, op := range []Operation{LookupView, Lease, Dispatch} {
				label := fmt.Sprintf("baseline %s/%s/c%d", cand, op, callers)
				a := find("baseline", cand, op, callers)
				if !covered(a, label) {
					continue
				}
				if a.NonOK > 0 {
					fail = append(fail, fmt.Sprintf("%s: %d non-OK observations in the mutation-free baseline", label, a.NonOK))
				}
				if cv, err := scale.CV(a.Throughput); err != nil {
					inconclusive = append(inconclusive, label+": throughput CV unavailable: "+err.Error())
				} else if cv > NoiseCVLimit {
					invalid = append(invalid, fmt.Sprintf("%s: throughput CV %.1f%% > 10%%; repeat the unchanged protocol", label, cv*100))
				}
				if op != Lease {
					continue
				}
				// §8.3 budget: aggregate AND every individual run, never averaged away.
				if a.Dist.P99 > int64(LeaseP99Limit) {
					fail = append(fail, fmt.Sprintf("%s: aggregate p99 %dns > %dns", label, a.Dist.P99, int64(LeaseP99Limit)))
				}
				for i, d := range a.PerRun {
					if d.P99 > int64(LeaseP99Limit) {
						fail = append(fail, fmt.Sprintf("%s: run %d p99 %dns > %dns", label, i, d.P99, int64(LeaseP99Limit)))
					}
				}
				for i, m := range a.Mallocs {
					if m != 0 {
						fail = append(fail, fmt.Sprintf("%s: run %d allocated %d objects; budget is zero", label, i, m))
					}
				}
			}
			// Stress: floors, then per-run correctness.
			runs, count := 0, 0
			for _, s := range in.Stress {
				if s.Candidate == cand && s.Callers == callers {
					runs++
					count += s.Observations
				}
			}
			if runs < MinRepetitions || count < MinObservations {
				inconclusive = append(inconclusive, fmt.Sprintf("stress %s/c%d: fewer than %d runs / %d observations (have %d runs, %d)", cand, callers, MinRepetitions, MinObservations, runs, count))
			}
		}
	}
	for _, s := range in.Stress {
		label := fmt.Sprintf("stress %s/c%d/rep%d", s.Candidate, s.Callers, s.Rep)
		if s.Observations != in.Plan.StressSamples {
			fail = append(fail, fmt.Sprintf("%s: %d observations recorded, plan has %d", label, s.Observations, in.Plan.StressSamples))
		}
		if s.Scheduled != in.Plan.ScheduledUpdates {
			fail = append(fail, fmt.Sprintf("%s: %d scheduled updates recorded, plan has %d", label, s.Scheduled, in.Plan.ScheduledUpdates))
		}
		if s.Overflow > 0 {
			fail = append(fail, fmt.Sprintf("%s: %d arrivals overflowed the outstanding cap", label, s.Overflow))
		}
		if s.Failed > 0 || s.TimedOut > 0 {
			fail = append(fail, fmt.Sprintf("%s: %d failed and %d timed-out mutation items", label, s.Failed, s.TimedOut))
		}
		if s.Scheduled != s.Succeeded+s.Overflow+s.Failed+s.TimedOut {
			fail = append(fail, label+": mutation outcomes do not account for every scheduled arrival")
		}
		if s.DrainTimedOut {
			fail = append(fail, label+": outstanding work remained after the 30s drain bound")
		}
		if s.Violations > 0 {
			fail = append(fail, fmt.Sprintf("%s: %d inactive-owner method bodies executed", label, s.Violations))
		}
		if n := s.OutcomeCounts[OutcomeError.String()]; n > 0 {
			fail = append(fail, fmt.Sprintf("%s: %d observations ended in an unexpected error class", label, n))
		}
		if s.OwnerReactivations != uint64(s.OwnerUpdates) || s.ConsumerReactivations != uint64(s.OwnerUpdates) {
			fail = append(fail, fmt.Sprintf("%s: %d owner updates produced %d owner activations and %d consumer bindings", label, s.OwnerUpdates, s.OwnerReactivations, s.ConsumerReactivations))
		}
		fail = append(fail, prefixed(label, s.Problems)...)
	}
	swapUnknown, throttleUnknown := false, false
	for _, h := range in.Hosts {
		if h.SwapKnown && h.SwapDelta > 0 {
			invalid = append(invalid, fmt.Sprintf("%s: host swapping observed (%d pages); repeat unchanged", h.Run, h.SwapDelta))
		}
		if h.ThrottleKnow && h.ThrottleDiff > 0 {
			invalid = append(invalid, fmt.Sprintf("%s: cgroup CPU throttling observed (%d periods); repeat unchanged", h.Run, h.ThrottleDiff))
		}
		swapUnknown = swapUnknown || !h.SwapKnown
		throttleUnknown = throttleUnknown || !h.ThrottleKnow
	}
	if len(in.Hosts) == 0 {
		inconclusive = append(inconclusive, "no per-run host noise records")
	}
	if swapUnknown {
		inconclusive = append(inconclusive, "swap activity could not be observed for every run")
	}
	if throttleUnknown {
		inconclusive = append(inconclusive, "CPU throttling could not be observed for every run")
	}

	var reasons []string
	reasons = append(reasons, prefixed("FAIL", fail)...)
	reasons = append(reasons, prefixed("INVALID", invalid)...)
	reasons = append(reasons, prefixed("INCONCLUSIVE", inconclusive)...)
	switch {
	case len(fail) > 0:
		return scale.Fail, reasons
	case len(invalid) > 0:
		return scale.Invalid, reasons
	case len(inconclusive) > 0:
		return scale.Inconclusive, reasons
	}
	return scale.Pass, nil
}

func prefixed(prefix string, items []string) []string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = prefix + ": " + s
	}
	return out
}
