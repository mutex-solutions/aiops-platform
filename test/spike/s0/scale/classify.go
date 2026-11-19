// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"fmt"
	"slices"
	"time"
)

// Finding is one reason with the class it forces. Pass is never a finding: it is
// the absence of every other one, and only when the run is not a dry run.
type Finding struct {
	Class Classification
	Text  string
}

func fail(format string, args ...any) Finding {
	return Finding{Fail, fmt.Sprintf(format, args...)}
}
func invalid(format string, args ...any) Finding {
	return Finding{Invalid, fmt.Sprintf(format, args...)}
}
func inconclusive(format string, args ...any) Finding {
	return Finding{Inconclusive, fmt.Sprintf(format, args...)}
}

// Resolve applies the precedence fail > invalid > inconclusive > pass. Missing
// evidence is an inconclusive finding, so it can never reach pass; a dry run can
// never be better than inconclusive. Reasons keep their original order, prefixed
// by class, and every finding is reported, not only the decisive ones.
func Resolve(findings []Finding, dry bool) (Classification, []string) {
	class := Pass
	rank := map[Classification]int{Pass: 0, Inconclusive: 1, Invalid: 2, Fail: 3}
	var reasons []string
	for _, f := range findings {
		if rank[f.Class] > rank[class] {
			class = f.Class
		}
		reasons = append(reasons, string(f.Class)+": "+f.Text)
	}
	if dry {
		reasons = append(reasons, "inconclusive: DRY RUN - reduced protocol, never satisfies the S0 gate")
		if class == Pass {
			class = Inconclusive
		}
	}
	return class, reasons
}

// budgetFinding applies the frozen latency limit of one operation to the
// aggregate distribution of all its individual observations. Mount and leaf are
// p99 limits; every restart is held to its limit at the maximum (the criteria
// state a plain bound); each cold tree mount must be strictly below 50 ms.
func budgetFinding(label, operation string, d Distribution) *Finding {
	var f Finding
	switch operation {
	case "mount-settle":
		if d.P99 > int64(MountP99Limit) {
			f = fail("%s: mount+settle p99 %s > %s", label, time.Duration(d.P99), MountP99Limit)
		}
	case "leaf-update-settle":
		if d.P99 > int64(LeafP99Limit) {
			f = fail("%s: leaf update+settle p99 %s > %s", label, time.Duration(d.P99), LeafP99Limit)
		}
	case "root-restart-settle", "shared-provider-restart-settle", "all-providers-batch-restart-settle":
		if d.Max > int64(RestartLimit) {
			f = fail("%s: %s max %s > %s", label, operation, time.Duration(d.Max), RestartLimit)
		}
	case "cold-tree-mount-settle":
		if d.Max >= int64(ColdMountLimit) {
			f = fail("%s: cold mount max %s is not < %s", label, time.Duration(d.Max), ColdMountLimit)
		}
	}
	if f.Text == "" {
		return nil
	}
	return &f
}

// floorFindings: fewer observations or repetitions than the stated floor, or than
// the plan scheduled, is inconclusive (§4). The scheduled count matters because
// an aborted run must not look complete.
func floorFindings(label string, observations, repetitions, scheduled int, dry bool) []Finding {
	var out []Finding
	if observations < MinimumSamples && !dry {
		out = append(out, inconclusive("%s: %d observations, floor is %d", label, observations, MinimumSamples))
	}
	if repetitions < MinimumRepetitions && !dry {
		out = append(out, inconclusive("%s: %d repetitions, floor is %d", label, repetitions, MinimumRepetitions))
	}
	if observations != scheduled {
		out = append(out, inconclusive("%s: %d observations recorded, %d scheduled (aborted or missing evidence)", label, observations, scheduled))
	}
	return out
}

// heapFinding compares one post-GC retained checkpoint with its baseline, signed,
// so a retained value below the baseline is not an overflow.
func heapFinding(label string, baseline, retained uint64, limit int64) *Finding {
	if delta := int64(retained) - int64(baseline); delta > limit {
		f := fail("%s: retained heap %d B over baseline exceeds %d B (archives and history included)", label, delta, limit)
		return &f
	}
	return nil
}

// idleGoroutineFindings: any candidate-owned goroutine count above its
// enumerated baseline owner is a nonzero idle goroutine count. Raw stacks are in
// the record for review; the verdict never waits for a reviewer to find a leak.
func idleGoroutineFindings(label string, baseline, retained Goroutines) []Finding {
	var out []Finding
	for _, owner := range slices.Sorted(mapKeysInt(retained.Counts)) {
		if owner != "external" && retained.Counts[owner] > baseline.Counts[owner] {
			out = append(out, fail("%s: candidate goroutine owner %s = %d above baseline %d (idle goroutines must be zero per instance)", label, owner, retained.Counts[owner], baseline.Counts[owner]))
		}
	}
	return out
}

func mapKeysInt(values map[string]int) func(func(string) bool) {
	return func(yield func(string) bool) {
		for key := range values {
			if !yield(key) {
				return
			}
		}
	}
}

// noiseFindings classifies host evidence. Swap activity or a throttled period in
// any run is invalid (repeat unchanged); a counter that was not observable is
// inconclusive and never read as zero.
func noiseFindings(deltas []HostDelta) []Finding {
	var out []Finding
	var swap, throttle, unknownSwap, unknownThrottle uint64
	for _, d := range deltas {
		swap += d.SwapDelta
		throttle += d.ThrottleDiff
		if !d.SwapKnown {
			unknownSwap++
		}
		if !d.ThrottleKnow {
			unknownThrottle++
		}
	}
	if swap > 0 {
		out = append(out, invalid("host swapped %d pages during measured runs; repeat the unchanged protocol", swap))
	}
	if throttle > 0 {
		out = append(out, invalid("cgroup CPU throttled %d periods during measured runs; repeat the unchanged protocol", throttle))
	}
	if unknownSwap > 0 {
		out = append(out, inconclusive("swap counters unobservable in %d runs", unknownSwap))
	}
	if unknownThrottle > 0 {
		out = append(out, inconclusive("cgroup throttle counters unobservable in %d runs", unknownThrottle))
	}
	if len(deltas) == 0 {
		out = append(out, inconclusive("no host noise samples"))
	}
	return out
}

// cvFinding: CV across independent repetitions above the limit is invalid; an
// unevaluable CV (fewer than two repetitions) is inconclusive.
func cvFinding(label string, rates []float64) *Finding {
	cv, err := CV(rates)
	if err != nil {
		f := inconclusive("%s: throughput CV not evaluable: %v", label, err)
		return &f
	}
	if cv > NoiseCVLimit {
		f := invalid("%s: throughput CV %.3f > %.2f; repeat the unchanged protocol", label, cv, NoiseCVLimit)
		return &f
	}
	return nil
}
