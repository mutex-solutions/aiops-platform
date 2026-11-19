// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestResolvePrecedence(t *testing.T) {
	cases := []struct {
		name     string
		findings []Finding
		dry      bool
		want     Classification
	}{
		{"no finding passes", nil, false, Pass},
		{"dry run never passes", nil, true, Inconclusive},
		{"inconclusive", []Finding{inconclusive("x")}, false, Inconclusive},
		{"invalid beats inconclusive", []Finding{inconclusive("a"), invalid("b")}, false, Invalid},
		{"fail beats invalid", []Finding{invalid("a"), fail("b"), inconclusive("c")}, false, Fail},
		{"fail beats everything regardless of order", []Finding{fail("b"), invalid("a")}, false, Fail},
		{"dry cannot hide a failure", []Finding{fail("b")}, true, Fail},
		{"dry cannot soften invalid", []Finding{invalid("b")}, true, Invalid},
	}
	for _, c := range cases {
		got, reasons := Resolve(c.findings, c.dry)
		if got != c.want {
			t.Errorf("%s: %s, want %s (%v)", c.name, got, c.want, reasons)
		}
		if len(c.findings) > 0 && len(reasons) < len(c.findings) {
			t.Errorf("%s: a finding was dropped from the reasons", c.name)
		}
	}
	if _, reasons := Resolve(nil, true); len(reasons) == 0 || !strings.Contains(reasons[0], "DRY RUN") {
		t.Errorf("dry run is not labelled in the reasons: %v", reasons)
	}
}

func TestBudgetFindingsAreTheFrozenLimits(t *testing.T) {
	ms := func(n int64) int64 { return n * int64(time.Millisecond) }
	cases := []struct {
		op   string
		d    Distribution
		miss bool
	}{
		{"mount-settle", Distribution{P99: 2 * int64(time.Second)}, false},
		{"mount-settle", Distribution{P99: 2*int64(time.Second) + 1}, true},
		{"mount-settle", Distribution{P99: ms(1), Max: 60 * int64(time.Second)}, false}, // p99 limit, not max
		{"leaf-update-settle", Distribution{P99: ms(10)}, false},
		{"leaf-update-settle", Distribution{P99: ms(10) + 1}, true},
		{"root-restart-settle", Distribution{P99: ms(1), Max: 5 * int64(time.Second)}, false},
		{"root-restart-settle", Distribution{P99: ms(1), Max: 5*int64(time.Second) + 1}, true}, // held at max
		{"shared-provider-restart-settle", Distribution{Max: 5*int64(time.Second) + 1}, true},
		{"all-providers-batch-restart-settle", Distribution{Max: 5*int64(time.Second) + 1}, true},
		{"cold-tree-mount-settle", Distribution{P99: ms(1), Max: ms(50) - 1}, false},
		{"cold-tree-mount-settle", Distribution{P99: ms(1), Max: ms(50)}, true}, // strictly below 50 ms
		{"teardown", Distribution{P99: 99 * int64(time.Second)}, false},         // no frozen time limit
		{"hot-tree-lookup", Distribution{P99: 99 * int64(time.Second)}, false},
	}
	for _, c := range cases {
		got := budgetFinding("x", c.op, c.d)
		if (got != nil) != c.miss || (got != nil && got.Class != Fail) {
			t.Errorf("%s %+v: finding=%v, want miss=%v", c.op, c.d, got, c.miss)
		}
	}
}

func TestFloorFindings(t *testing.T) {
	if got := floorFindings("k", 1000, 10, 1000, false); len(got) != 0 {
		t.Fatalf("complete cell flagged: %v", got)
	}
	for _, c := range []struct{ obs, reps, scheduled int }{{999, 10, 999}, {1000, 9, 1000}, {900, 10, 1000}} {
		got := floorFindings("k", c.obs, c.reps, c.scheduled, false)
		if len(got) == 0 {
			t.Errorf("%+v accepted", c)
		}
		for _, f := range got {
			if f.Class != Inconclusive {
				t.Errorf("%+v: below-floor evidence must be inconclusive, got %s", c, f.Class)
			}
		}
	}
	// Even a dry plan must notice an aborted (incomplete) cell.
	if got := floorFindings("k", 3, 2, 4, true); len(got) != 1 {
		t.Fatalf("dry incomplete cell: %v", got)
	}
}

func TestPlanFloorsAndFrozenIdentity(t *testing.T) {
	plan := FrozenPlan()
	if !plan.Frozen() || validatePlan(plan) != nil || plan.GraphTrials*plan.Repetitions != 1000 || plan.TreeTrials*plan.TreeCycles*plan.Repetitions != 1000 {
		t.Fatalf("frozen plan: %+v", plan)
	}
	if DryPlan().Frozen() {
		t.Fatal("the dry plan claims to be frozen")
	}
	for _, mutate := range []func(*Plan){
		func(p *Plan) { p.Repetitions = 9 }, func(p *Plan) { p.GraphTrials = 99 }, func(p *Plan) { p.TreeTrials = 9 },
		func(p *Plan) { p.TreeCycles = 9 }, func(p *Plan) { p.GraphFibers = 9_999 }, func(p *Plan) { p.TreeCounts = []int{500} },
		func(p *Plan) { p.Dry = true },
	} {
		bad := FrozenPlan()
		mutate(&bad)
		if bad.Frozen() {
			t.Errorf("mutated plan still frozen: %+v", bad)
		}
	}
	for _, bad := range []Plan{{Repetitions: 9, GraphTrials: 1000, TreeTrials: 100, TreeCycles: 10}, {Repetitions: 10, GraphTrials: 99, TreeTrials: 10, TreeCycles: 10}, {Repetitions: 10, GraphTrials: 100, TreeTrials: 9, TreeCycles: 10}} {
		if validatePlan(bad) == nil {
			t.Errorf("insufficient plan accepted: %+v", bad)
		}
	}
	// A Dry plan can never be run as a frozen one.
	if _, err := Measure(context.Background(), IdentityRecord{}, Plan{Repetitions: 1, GraphTrials: 1, TreeTrials: 1, TreeCycles: 1, GraphFibers: 100, TreeCounts: []int{2}}, func(RunRecord) error { return nil }); err == nil {
		t.Fatal("a reduced plan not labelled Dry was run")
	}
}

func TestFrozenPlanFixturesAreTheFrozenFixtures(t *testing.T) {
	got, err := FrozenPlan().Fixtures()
	if err != nil {
		t.Fatal(err)
	}
	want := FrozenFixtures()
	if len(got) != len(want) {
		t.Fatalf("%d fixtures, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i].Name || len(got[i].Nodes) != len(want[i].Nodes) || got[i].Topology != want[i].Topology {
			t.Errorf("fixture %d: %s/%d, want %s/%d", i, got[i].Name, len(got[i].Nodes), want[i].Name, len(want[i].Nodes))
		}
	}
	p := FrozenPlan()
	f500x4 := got[5+1]
	if f500x4.TreeCount != 500 || f500x4.TreeWidth != 4 {
		t.Fatalf("fixture order: %+v", f500x4.Name)
	}
	if n := p.expectedObservations(got[0], "mount-settle"); n != 1000 {
		t.Errorf("graph observations per operation = %d", n)
	}
	if n := p.expectedObservations(f500x4, "shared-provider-restart-settle"); n != 1000 {
		t.Errorf("shared restart observations = %d", n)
	}
	if n := p.expectedObservations(f500x4, "cold-tree-mount-settle"); n != 500_000 {
		t.Errorf("cold mount observations = %d", n)
	}
	if ops := operations(got[4]); len(ops) != 5 || ops[3] != "all-providers-batch-restart-settle" {
		t.Errorf("realm operations %v", ops)
	}
}

func TestHeapFindingIsSignedAndAtTheLimit(t *testing.T) {
	const base = 10 << 20
	if heapFinding("x", base, base+uint64(GraphHeapLimit), GraphHeapLimit) != nil {
		t.Error("exactly the limit is within the limit")
	}
	if f := heapFinding("x", base, base+uint64(GraphHeapLimit)+1, GraphHeapLimit); f == nil || f.Class != Fail {
		t.Error("one byte over the limit must fail")
	}
	if heapFinding("x", base, 1, TreeHeapLimit) != nil {
		t.Error("a checkpoint below the baseline is not an overflow (unsigned wrap)")
	}
	if heapFinding("x", base, base+uint64(TreeHeapLimit)+1, TreeHeapLimit) == nil {
		t.Error("the 32 MiB tree limit is not applied")
	}
}

func TestIdleGoroutineFindings(t *testing.T) {
	baseline := Goroutines{Counts: map[string]int{"root.worker": 1, "external": 2}}
	same := Goroutines{Counts: map[string]int{"root.worker": 1, "external": 9}}
	if got := idleGoroutineFindings("x", baseline, same); len(got) != 0 {
		t.Fatalf("an unrelated external goroutine is not a candidate leak: %v", got)
	}
	leak := Goroutines{Counts: map[string]int{"root.worker": 1, "fiber.idle": 1}}
	got := idleGoroutineFindings("x", baseline, leak)
	if len(got) != 1 || got[0].Class != Fail {
		t.Fatalf("a new candidate-owned goroutine is a failure: %v", got)
	}
	// One disappearing worker must not cancel a leaked one.
	swap := Goroutines{Counts: map[string]int{"fiber.idle": 1}}
	if got := idleGoroutineFindings("x", baseline, swap); len(got) != 1 {
		t.Fatalf("leak hidden by a vanished fixed worker: %v", got)
	}
}

func TestNoiseAndCVFindings(t *testing.T) {
	ok := HostDelta{SwapKnown: true, ThrottleKnow: true}
	if got := noiseFindings([]HostDelta{ok, ok}); len(got) != 0 {
		t.Fatalf("quiet host flagged: %v", got)
	}
	cases := []struct {
		name  string
		delta HostDelta
		want  Classification
	}{
		{"swap", HostDelta{SwapKnown: true, ThrottleKnow: true, SwapDelta: 1}, Invalid},
		{"throttle", HostDelta{SwapKnown: true, ThrottleKnow: true, ThrottleDiff: 1}, Invalid},
		{"swap unobservable", HostDelta{ThrottleKnow: true}, Inconclusive},
		{"throttle unobservable", HostDelta{SwapKnown: true}, Inconclusive},
	}
	for _, c := range cases {
		got := noiseFindings([]HostDelta{ok, c.delta})
		if len(got) == 0 || got[0].Class != c.want {
			t.Errorf("%s: %v, want %s", c.name, got, c.want)
		}
	}
	if got := noiseFindings(nil); len(got) == 0 || got[0].Class != Inconclusive {
		t.Errorf("no host samples must be inconclusive: %v", got)
	}
	if f := cvFinding("x", []float64{90, 110}); f != nil {
		t.Errorf("CV exactly 10%% is not above the limit: %v", f)
	}
	if f := cvFinding("x", []float64{80, 120}); f == nil || f.Class != Invalid {
		t.Errorf("CV 20%% must be invalid: %v", f)
	}
	if f := cvFinding("x", []float64{100}); f == nil || f.Class != Inconclusive {
		t.Errorf("one repetition cannot evaluate CV: %v", f)
	}
}

func TestSlope(t *testing.T) {
	if s := slope([]int64{10, 20, 30, 40}); s < 9.999 || s > 10.001 {
		t.Fatalf("slope = %v", s)
	}
	if slope([]int64{5, 5, 5}) != 0 || slope([]int64{7}) != 0 || slope(nil) != 0 {
		t.Fatal("flat or short series must have slope 0")
	}
}

func TestRetainedMetricsBaselines(t *testing.T) {
	cp := func(heap uint64) Checkpoint { return Checkpoint{Memory: Memory{HeapAlloc: heap}} }
	graph := RunResult{Workload: Workload{Fibers: 1000}, EmptyRoot: cp(100), Retained: []Checkpoint{cp(1100), cp(2100)}}
	m := retainedMetrics(graph)
	if m.Baseline != "empty-root" || m.LimitBytes != GraphHeapLimit || m.MaxDeltaBytes != 2000 || m.FirstDeltaBytes != 1000 || m.PerFiberBytes != 2 {
		t.Fatalf("graph metrics: %+v", m)
	}
	shared := cp(500)
	tree := RunResult{Workload: Workload{Fibers: 997, Trees: 10, TreeWidth: 4}, EmptyRoot: cp(100), SharedOnly: &shared, Retained: []Checkpoint{cp(540)}}
	m = retainedMetrics(tree)
	if m.Baseline != "shared-only" || m.LimitBytes != TreeHeapLimit || m.MaxDeltaBytes != 40 || m.Fibers != 40 || m.PerFiberBytes != 1 {
		t.Fatalf("tree metrics: %+v", m)
	}
	if m := retainedMetrics(RunResult{}); len(m.DeltaBytes) != 0 {
		t.Fatalf("no checkpoint must give no evidence: %+v", m)
	}
}

func TestReportAddNeverUpgrades(t *testing.T) {
	r := Report{Dry: false}
	r.Add()
	if r.Classification != Pass {
		t.Fatalf("empty complete report = %s", r.Classification)
	}
	r.Add(inconclusive("missing"))
	r.Add(fail("bad"))
	r.Add(inconclusive("more"))
	if r.Classification != Fail || len(r.Reasons) != 3 {
		t.Fatalf("%s %v", r.Classification, r.Reasons)
	}
}
