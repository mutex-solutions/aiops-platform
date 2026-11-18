// SPDX-License-Identifier: Apache-2.0

package callpath

import (
	"runtime"
	"sync"
	"time"

	"github.com/mutex-solutions/aiops-platform/test/spike/s0/scale"
)

// Arm is the raw record of one operation at one caller concurrency in one run.
// Every observation is present in call order per caller: no sampling, no
// reservoir, no subtraction and no success-only filtering. Callers' blocks are
// concatenated in caller order; CallerCounts gives the block lengths.
type Arm struct {
	Operation      Operation          `json:"operation"`
	Callers        int                `json:"callers"`
	Samples        int                `json:"samples"`
	WallNS         int64              `json:"wallNS"`
	Mallocs        uint64             `json:"mallocsInWindow"`
	AllocsPerOp    float64            `json:"allocsPerOp"`
	AllocBytes     uint64             `json:"allocBytesInWindow"`
	NumGC          uint32             `json:"gcCyclesInWindow"`
	CallerCounts   []int              `json:"callerCounts"`
	LatencyNS      []int64            `json:"latencyNS"`
	Outcomes       string             `json:"outcomes"` // One digit per observation: 0 ok, 1 refused read, 2 refused enter, 3 error.
	OutcomeCounts  map[string]int     `json:"outcomeCounts"`
	Summary        scale.Distribution `json:"summary"`
	AllocsMeasured string             `json:"allocationNote"`
	StartOffsetNS  []int64            `json:"startOffsetNS,omitempty"`      // Stress only.
	ScheduledNS    []int64            `json:"scheduledOffsetNS,omitempty"`  // Stress only.
	ActivationGen  []uint64           `json:"ownerActivationGen,omitempty"` // Stress only.
}

// split distributes n observations over callers; the first n%callers get one more.
func split(n, callers int) []int {
	counts := make([]int, callers)
	for i := range counts {
		counts[i] = n / callers
		if i < n%callers {
			counts[i]++
		}
	}
	return counts
}

func countOutcomes(outcomes string) map[string]int {
	counts := make(map[string]int, len(outcomeNames))
	for _, name := range outcomeNames {
		counts[name] = 0
	}
	for i := range len(outcomes) {
		counts[outcomeNames[outcomes[i]-'0']]++
	}
	return counts
}

// MeasureArm runs op with callers independent goroutines, each recording its own
// individual latencies with a monotonic clock around exactly one op call. The
// goroutines and arrays exist before the allocation window opens, so a nonzero
// Mallocs is allocation by the operation (or by the runtime wake callback it
// reaches), never by the harness.
func MeasureArm(operation Operation, op Op, callers, samples int) Arm {
	counts := split(samples, callers)
	latency := make([]int64, samples)
	outcomes := make([]byte, samples)
	start := make(chan struct{})
	var ready, done sync.WaitGroup
	offset := 0
	for _, n := range counts {
		l, o := latency[offset:offset+n], outcomes[offset:offset+n]
		offset += n
		ready.Add(1)
		done.Add(1)
		go func() {
			ready.Done()
			<-start
			for i := range l {
				t0 := time.Now()
				outcome, _ := op()
				l[i] = int64(time.Since(t0))
				o[i] = outcome.char()
			}
			done.Done()
		}()
	}
	ready.Wait()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	t0 := time.Now()
	close(start)
	done.Wait()
	wall := time.Since(t0)
	runtime.ReadMemStats(&after)
	arm := Arm{Operation: operation, Callers: callers, Samples: samples, WallNS: wall.Nanoseconds(),
		Mallocs: after.Mallocs - before.Mallocs, AllocBytes: after.TotalAlloc - before.TotalAlloc, NumGC: after.NumGC - before.NumGC,
		CallerCounts: counts, LatencyNS: latency, Outcomes: string(outcomes), AllocsMeasured: "process-wide Mallocs delta across the observation window; no subtraction"}
	arm.AllocsPerOp = float64(arm.Mallocs) / float64(samples)
	arm.OutcomeCounts = countOutcomes(arm.Outcomes)
	arm.Summary = scale.Summarize(latency)
	return arm
}
