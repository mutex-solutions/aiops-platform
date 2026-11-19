// SPDX-License-Identifier: Apache-2.0

package callpath

import (
	"runtime"
	"sync"
	"sync/atomic"
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
	SpanNS         []int64            `json:"callerSpanNS"`        // Amendment 1: each caller's first-start to last-end span.
	ThroughputPS   float64            `json:"throughputPerSecond"` // Amendment 1: sum of per-caller observations/span.
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

// pollInterval is the coordinator's sleep between completion checks (amendment 1).
const pollInterval = 250 * time.Microsecond

// ProtocolAmendment is the callpath measurement protocol this harness implements
// (docs/design/s0-callpath-protocol-amendment-2.md, which builds on amendment 1).
// Records without the field are amendment 0, the original protocol.
const ProtocolAmendment = 2

// RunThroughput is the amendment-1 run throughput: the sum over callers of that
// caller's observations divided by its own first-start to last-end span. A zero
// span is read as 1 ns so a clock tie can never divide by zero.
func RunThroughput(counts []int, spanNS []int64) float64 {
	sum := 0.0
	for i, n := range counts {
		span := max(spanNS[i], 1)
		sum += float64(n) * 1e9 / float64(span)
	}
	return sum
}

// waitDone is the coordinator's wait. It polls an atomic counter and sleeps on
// the goroutine's own (pre-warmed) timer; it never parks on a wait queue, so the
// runtime has no wait-queue record to allocate.
func waitDone(done *atomic.Int32, n int32) {
	for done.Load() < n {
		time.Sleep(pollInterval)
	}
}

type windowResult struct {
	Mallocs, Bytes uint64
	NumGC          uint32
	Wall           time.Duration
}

// runWindow starts callers goroutines, each running work(i) once the single start
// flag flips, and returns the process-wide allocation delta and coordinator wall
// time of the window. Everything the window uses (goroutines, flag, counter,
// MemStats, the coordinator's sleep timer) exists and is warm before the first
// ReadMemStats. ready, if set, runs in each caller just before it signals ready.
func runWindow(callers int, ready func(int), work func(int)) windowResult {
	var rdy sync.WaitGroup
	var start atomic.Bool
	var done atomic.Int32
	rdy.Add(callers)
	for i := range callers {
		go func() {
			if ready != nil {
				ready(i)
			}
			rdy.Done()
			for !start.Load() { // Yield so callers beyond GOMAXPROCS can reach the flag.
				runtime.Gosched()
			}
			work(i)
			done.Add(1)
		}()
	}
	rdy.Wait() // Before the window: parking here is allowed.
	runtime.GC()
	for range 4 {
		time.Sleep(pollInterval) // Creates the timer record and grows the timer heap slot.
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	t0 := time.Now()
	start.Store(true)
	waitDone(&done, int32(callers))
	wall := time.Since(t0)
	runtime.ReadMemStats(&after)
	return windowResult{Mallocs: after.Mallocs - before.Mallocs, Bytes: after.TotalAlloc - before.TotalAlloc, NumGC: after.NumGC - before.NumGC, Wall: wall}
}

// MeasureArm runs op with callers independent goroutines, each recording its own
// individual latencies with a monotonic clock around exactly one op call. Under
// amendment 1 the window holds no harness allocation (see runWindow), callers are
// released by one start flag, and throughput comes from the callers' own spans.
func MeasureArm(operation Operation, op Op, callers, samples int) Arm {
	counts := split(samples, callers)
	latency := make([]int64, samples)
	outcomes := make([]byte, samples)
	spans := make([]int64, callers)
	offsets := make([]int, callers)
	for i := 1; i < callers; i++ {
		offsets[i] = offsets[i-1] + counts[i-1]
	}
	w := runWindow(callers, nil, func(c int) {
		l, o := latency[offsets[c]:offsets[c]+counts[c]], outcomes[offsets[c]:offsets[c]+counts[c]]
		var first, end time.Time
		for i := range l {
			t0 := time.Now()
			if i == 0 {
				first = t0
			}
			outcome, _ := op()
			end = time.Now()
			l[i] = int64(end.Sub(t0))
			o[i] = outcome.char()
		}
		spans[c] = int64(end.Sub(first))
	})
	arm := Arm{Operation: operation, Callers: callers, Samples: samples, WallNS: w.Wall.Nanoseconds(),
		Mallocs: w.Mallocs, AllocBytes: w.Bytes, NumGC: w.NumGC,
		CallerCounts: counts, LatencyNS: latency, Outcomes: string(outcomes), SpanNS: spans,
		AllocsMeasured: "process-wide Mallocs delta across the observation window; no subtraction"}
	arm.ThroughputPS = RunThroughput(counts, spans)
	arm.AllocsPerOp = float64(arm.Mallocs) / float64(samples)
	arm.OutcomeCounts = countOutcomes(arm.Outcomes)
	arm.Summary = scale.Summarize(latency)
	return arm
}
