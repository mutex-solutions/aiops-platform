// SPDX-License-Identifier: Apache-2.0

package callpath

import (
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/test/spike/s0/scale"
)

// Tests for protocol amendment 1. None asserts a timing threshold.

// The coordinator's wait path allocates nothing, even when it has to sleep. A
// helper goroutine that exists before measurement flips the counter after a
// delay, so waitDone sleeps at least once per run without any allocation of
// ours (and without a goroutine creation inside the measured function).
func TestCoordinatorWaitDoesNotAllocate(t *testing.T) {
	var pending, stop atomic.Bool
	var done atomic.Int32
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for !stop.Load() {
			if pending.CompareAndSwap(true, false) {
				time.Sleep(2 * pollInterval)
				done.Add(1)
			}
			runtime.Gosched()
		}
	}()
	defer func() { stop.Store(true); <-finished }()
	var want int32
	if n := testing.AllocsPerRun(300, func() {
		want++
		pending.Store(true)
		waitDone(&done, want)
	}); n != 0 {
		t.Fatalf("coordinator wait allocated %v objects per wait", n)
	}
}

// The whole window path (barrier, release, wait, completion, bracketing) with a
// caller that outlasts several poll intervals and with callers that return at
// once, repeated. The harness itself may not allocate; the count of non-zero
// windows is logged for inspection.
func TestWindowPathDoesNotAllocate(t *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(RequiredGOMAXPROCS))
	spin := func(int) {
		for t0 := time.Now(); time.Since(t0) < 3*pollInterval; {
		}
	}
	for _, c := range []struct {
		name    string
		callers int
		work    func(int)
	}{{"trivial callers", 8, func(int) {}}, {"one caller outlasting the poll", 1, spin}, {"callers beyond Ps", 32, spin}} {
		t.Run(c.name, func(t *testing.T) {
			const windows = 101
			var nonZero int
			var counts []uint64
			for range windows {
				w := runWindow(c.callers, nil, c.work)
				counts = append(counts, w.Mallocs)
				if w.Mallocs != 0 {
					nonZero++
				}
			}
			slices.Sort(counts)
			if counts[windows/2] != 0 {
				t.Fatalf("median window allocated %d objects (%d of %d windows non-zero)", counts[windows/2], nonZero, windows)
			}
			t.Logf("%d of %d windows read non-zero", nonZero, windows)
		})
	}
}

// No caller runs an operation before every caller has signalled ready, including
// when callers outnumber Ps.
func TestStartBarrierReleasesAfterAllReady(t *testing.T) {
	for _, callers := range []int{1, 8, 32} {
		var ready, early, ran atomic.Int32
		op := func() (Outcome, uint64) {
			ran.Add(1)
			if ready.Load() != int32(callers) {
				early.Add(1)
			}
			return OutcomeOK, 0
		}
		w := runWindow(callers, func(int) { ready.Add(1) }, func(int) { op() })
		if ran.Load() != int32(callers) || early.Load() != 0 || w.Wall <= 0 {
			t.Fatalf("callers=%d: ran %d, %d began before all were ready", callers, ran.Load(), early.Load())
		}
	}
}

func TestRunThroughputIsSumOfPerCallerRates(t *testing.T) {
	cases := []struct {
		name   string
		counts []int
		spans  []int64
		want   float64
	}{
		{"one caller", []int{1000}, []int64{1_000_000}, 1e6},
		{"equal callers add", []int{1000, 1000}, []int64{1_000_000, 1_000_000}, 2e6},
		{"each caller uses its own span", []int{1000, 500}, []int64{1_000_000, 2_000_000}, 1e6 + 250_000},
		{"zero span reads as 1ns", []int{3}, []int64{0}, 3e9},
	}
	for _, c := range cases {
		if got := RunThroughput(c.counts, c.spans); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	arm := MeasureArm(Timer, func() (Outcome, uint64) { return OutcomeOK, 0 }, 4, 400)
	if len(arm.SpanNS) != 4 || arm.ThroughputPS != RunThroughput(arm.CallerCounts, arm.SpanNS) || arm.ThroughputPS <= 0 {
		t.Fatalf("arm throughput not taken from caller spans: %+v", arm.SpanNS)
	}
	for _, s := range arm.SpanNS {
		if s < 0 || s > arm.WallNS {
			t.Fatalf("caller span %d outside the coordinator window %d", s, arm.WallNS)
		}
	}
}

func TestControlAllocationRule(t *testing.T) {
	set := func(f func(lease, control *Aggregate)) EvalInput {
		in := passingInput()
		var lease, control *Aggregate
		for i := range in.Aggregates {
			a := &in.Aggregates[i]
			if a.Candidate == "reconciler" && a.Callers == 8 {
				switch a.Operation {
				case Lease:
					lease = a
				case Timer:
					control = a
				}
			}
		}
		f(lease, control)
		return in
	}
	cases := []struct {
		name string
		in   EvalInput
		want scale.Classification
		text string
	}{
		{"control non-zero, lease zero", set(func(_, c *Aggregate) { c.Mallocs[4] = 1 }), scale.Invalid, "control allocated"},
		{"control and lease non-zero", set(func(l, c *Aggregate) { l.Mallocs[4], c.Mallocs[4] = 1, 1 }), scale.Invalid, "control allocated"},
		{"lease non-zero, control zero", set(func(l, _ *Aggregate) { l.Mallocs[4] = 1 }), scale.Fail, "budget is zero"},
		{"control reading missing", set(func(_, c *Aggregate) { c.Mallocs = c.Mallocs[:4] }), scale.Inconclusive, "no timer-only control"},
	}
	for _, c := range cases {
		class, reasons := Evaluate(c.in)
		if class != c.want || !strings.Contains(strings.Join(reasons, "\n"), c.text) {
			t.Errorf("%s: classified %s, want %s: %v", c.name, class, c.want, reasons)
		}
	}
	// A control failure in one run never hides a lease failure in another run.
	in := set(func(l, c *Aggregate) { c.Mallocs[1] = 1; l.Mallocs[2] = 1 })
	if class, _ := Evaluate(in); class != scale.Fail {
		t.Fatalf("lease failure hidden by an invalid control run: %s", class)
	}
}

func TestProtocolAmendmentsAreNeverMixed(t *testing.T) {
	cases := []struct {
		name string
		runs []int
		want scale.Classification
	}{
		{"all current", []int{1, 1, 1}, scale.Pass},
		{"one original-protocol run", []int{1, 0, 1}, scale.Invalid},
		{"all original protocol", []int{0, 0}, scale.Invalid},
		{"a future amendment", []int{1, 2}, scale.Invalid},
		{"none recorded", nil, scale.Inconclusive},
	}
	for _, c := range cases {
		in := passingInput()
		in.RunProtocols = c.runs
		class, reasons := Evaluate(in)
		if class != c.want {
			t.Errorf("%s: classified %s, want %s: %v", c.name, class, c.want, reasons)
		}
	}
	if !strings.Contains(strings.Join(checkProtocols([]int{1, 0}), ";"), "mix") {
		t.Fatal("mixture not named")
	}
}
