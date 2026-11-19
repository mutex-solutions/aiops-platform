// SPDX-License-Identifier: Apache-2.0

package callpath

import (
	"context"
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	co "github.com/mutex-solutions/aiops-platform/test/spike/s0/composition"
	cc "github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
	"github.com/mutex-solutions/aiops-platform/test/spike/s0/scale"
)

// These tests assert causal semantics on both real candidates with tiny counts.
// None asserts a timing threshold.

func forBackends(t *testing.T, body func(*testing.T, context.Context, scale.Backend)) {
	t.Helper()
	for _, b := range supportedBackends(t) {
		t.Run(b.Name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			body(t, ctx, b)
		})
	}
}

func newFixture(t *testing.T, ctx context.Context, b scale.Backend, opt FixtureOptions) *Fixture {
	t.Helper()
	fx, err := NewFixture(ctx, b, opt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := fx.Close(cleanup); err != nil {
			t.Error(err)
		}
	})
	return fx
}

func waitState(t *testing.T, ctx context.Context, fx *Fixture, id uint64, want cc.State) {
	t.Helper()
	for {
		snaps, err := fx.Runtime.Snapshots(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range snaps {
			if s.ID == id && s.State == want {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("fiber %d never reached %s: %v", id, want, ctx.Err())
		default:
			runtime.Gosched()
		}
	}
}

// The bound view must hold the PROVIDER's lease for the method body. The
// consumer is torn down and reactivated while a body is in flight, which would
// deadlock if the consumer's lease were held; the provider's cleanup must wait.
func TestBoundViewTakesProviderLeaseNotConsumers(t *testing.T) {
	forBackends(t, func(t *testing.T, ctx context.Context, b scale.Backend) {
		var armed atomic.Bool
		entered, release := make(chan struct{}), make(chan struct{})
		fx := newFixture(t, ctx, b, FixtureOptions{Leaves: 1, Gate: func(*Owner) {
			if armed.CompareAndSwap(true, false) {
				entered <- struct{}{}
				<-release
			}
		}})
		old := fx.Binding()
		view, err := co.Resolve(old.Ctx, fx.Key)
		if err != nil || view.Consumer() != old.Ctx {
			t.Fatalf("bound view must retain the resolving consumer: %v", err)
		}
		oldOwner := view.Owner()
		armed.Store(true)
		type result struct {
			gen uint64
			err error
		}
		called := make(chan result, 1)
		go func() { g, err := view.Call(); called <- result{g, err} }()
		<-entered

		// Consumer fully reactivates while the body still runs: no consumer lease.
		if err := fx.Runtime.Update(ctx, fx.Consumer.ID); err != nil {
			t.Fatal(err)
		}
		if err := fx.WaitBindingGen(ctx, old.Gen); err != nil {
			t.Fatalf("consumer teardown blocked by a call-path lease: %v", err)
		}
		if err := fx.Consumer.Await(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := co.Resolve(old.Ctx, fx.Key); !errors.Is(err, cc.ErrInactive) {
			t.Fatalf("released consumer activation still resolves: %v", err)
		}

		// The provider's cleanup must wait for the body's lease.
		if err := fx.Runtime.Update(ctx, fx.Provider.ID); err != nil {
			t.Fatal(err)
		}
		waitState(t, ctx, fx, fx.Provider.ID, cc.Unloading)
		if oldOwner.Closed() {
			t.Fatal("provider cleanup ran while its lease was held by the body")
		}
		close(release)
		r := <-called
		if r.err != nil || r.gen != oldOwner.Gen {
			t.Fatalf("body result = %+v", r)
		}
		if err := fx.Provider.Await(ctx); err != nil {
			t.Fatal(err)
		}
		if !oldOwner.Closed() || fx.Owner().Gen != oldOwner.Gen+1 {
			t.Fatalf("old owner closed=%v current gen=%d", oldOwner.Closed(), fx.Owner().Gen)
		}
		if fx.Violations() != 0 {
			t.Fatalf("%d bodies ran after cleanup", fx.Violations())
		}
	})
}

// After the owner is updated: a fresh binding and provision are published, old
// activations refuse Enter and Read, and no inactive-owner body executes.
func TestReactivationPublishesFreshBindingAndOldActivationsRefuse(t *testing.T) {
	forBackends(t, func(t *testing.T, ctx context.Context, b scale.Backend) {
		var bodies atomic.Int64
		fx := newFixture(t, ctx, b, FixtureOptions{Leaves: 1, Gate: func(*Owner) { bodies.Add(1) }})
		oldBinding := fx.Binding()
		oldView, err := co.Resolve(oldBinding.Ctx, fx.Key)
		if err != nil {
			t.Fatal(err)
		}
		before := fx.Checkpoint(ctx, "before", -1, 0)
		if err := fx.Runtime.Update(ctx, fx.Provider.ID); err != nil {
			t.Fatal(err)
		}
		if err := fx.WaitBindingGen(ctx, oldBinding.Gen); err != nil {
			t.Fatal(err)
		}
		if err := errors.Join(fx.Provider.Await(ctx), fx.Consumer.Await(ctx)); err != nil {
			t.Fatal(err)
		}
		after := fx.Checkpoint(ctx, "after", -1, 0)
		if len(before.Provisions) != 1 || len(after.Provisions) != 1 || before.Provisions[0].UID == after.Provisions[0].UID ||
			before.Provisions[0].Owner != after.Provisions[0].Owner || !after.Provisions[0].Active {
			t.Fatalf("provision did not republish under a fresh UID: %+v -> %+v", before.Provisions, after.Provisions)
		}
		fresh := fx.Binding()
		if fresh.Gen <= oldBinding.Gen || fresh.Ctx == oldBinding.Ctx || fx.Owner().Gen != oldView.Owner().Gen+1 {
			t.Fatal("binding and owner activation were not republished")
		}
		ran := bodies.Load()
		if _, err := oldView.Call(); !errors.Is(err, ErrOwnerInactive) || classify(err) != OutcomeRefusedEnter {
			t.Fatalf("old owner activation accepted Enter: %v", err)
		}
		if _, err := co.Resolve(oldBinding.Ctx, fx.Key); classify(err) != OutcomeRefusedRead {
			t.Fatalf("old consumer activation accepted Read: %v", err)
		}
		if bodies.Load() != ran || fx.Violations() != 0 {
			t.Fatal("an inactive owner's method body executed")
		}
		freshView, err := co.Resolve(fresh.Ctx, fx.Key)
		if err != nil {
			t.Fatal(err)
		}
		if gen, err := freshView.Call(); err != nil || gen != fx.Owner().Gen || bodies.Load() != ran+1 {
			t.Fatalf("fresh activation call = %d, %v", gen, err)
		}
		// The measured operations agree with the direct calls.
		for _, op := range []Operation{LookupView, Lease, Dispatch} {
			if outcome, _ := fx.Operation(op)(); outcome != OutcomeOK {
				t.Fatalf("%s outcome %s", op, outcome)
			}
		}
	})
}

func tinyStress() Plan {
	return Plan{Repetitions: 1, SamplesPerRun: 20, StressSamples: 20, Leaves: 10, UpdatesPerSecond: 100, OutstandingLimit: 8,
		OwnerUpdateEvery: 5, ScheduledUpdates: 20, StressDuration: 200 * time.Millisecond, Callers: []int{3}}
}

// More than eight outstanding is a recorded failure: not queued, not slowed, the
// arrival is kept in the record, and the rate is not adjusted.
func TestOverflowBeyondEightOutstandingIsRecordedFailure(t *testing.T) {
	forBackends(t, func(t *testing.T, ctx context.Context, b scale.Backend) {
		p := tinyStress()
		p.OwnerUpdateEvery = 1000 // No owner reactivation in this test.
		fx := newFixture(t, ctx, b, FixtureOptions{Leaves: p.Leaves})
		release := make(chan struct{})
		rec := RunStress(ctx, fx, p, 3, StressHooks{
			BeforeItem:    func(int) { <-release }, // The first eight admitted items stay outstanding.
			AfterSchedule: func() { close(release) },
		})
		s := SummarizeStress(rec)
		if s.Scheduled != 20 || s.Succeeded != 8 || s.Overflow != 12 || s.Failed != 0 || s.TimedOut != 0 {
			t.Fatalf("summary = %+v", s)
		}
		for k, m := range rec.Mutations {
			wantOverflow := k >= 8
			if (m.Outcome == MutationOverflow) != wantOverflow || m.K != k || m.ScheduledNS != int64(k)*int64(10*time.Millisecond) {
				t.Fatalf("mutation %d = %+v", k, m)
			}
			if wantOverflow && m.AckNS != 0 {
				t.Fatalf("overflow arrival %d was executed instead of rejected", k)
			}
		}
		class, reasons := Evaluate(EvalInput{Plan: p, Candidates: []string{b.Name}, Stress: []StressSummary{s}})
		if class != scale.Fail || !strings.Contains(strings.Join(reasons, "\n"), "overflowed the outstanding cap") {
			t.Fatalf("overflow classified %s: %v", class, reasons)
		}
	})
}

// A tiny stress run keeps every arrival, observation and causal checkpoint, and
// reactivates the owner exactly at every OwnerUpdateEvery-th schedule.
func TestStressRecordsEverythingAndOwnerUpdatesReactivate(t *testing.T) {
	forBackends(t, func(t *testing.T, ctx context.Context, b scale.Backend) {
		p := tinyStress()
		fx := newFixture(t, ctx, b, FixtureOptions{Leaves: p.Leaves})
		rec := RunStress(ctx, fx, p, 3, StressHooks{})
		s := SummarizeStress(rec)
		if len(rec.Mutations) != 20 || s.Observations != 20 || len(rec.Observations.Outcomes) != 20 || len(rec.Observations.ScheduledNS) != 20 ||
			s.Scheduled != s.Succeeded+s.Overflow+s.Failed+s.TimedOut || s.OwnerUpdates != 4 {
			t.Fatalf("incomplete stress record: %+v", s)
		}
		if !slices.Equal(rec.Observations.CallerCounts, []int{7, 7, 6}) {
			t.Fatalf("caller split %v", rec.Observations.CallerCounts)
		}
		if s.Overflow == 0 && s.Failed == 0 && s.TimedOut == 0 {
			if s.OwnerReactivations != 4 || s.ConsumerReactivations != 4 {
				t.Fatalf("4 owner updates produced %d owner activations and %d bindings", s.OwnerReactivations, s.ConsumerReactivations)
			}
			// before-window + one per owner update + after-drain.
			if len(rec.Checkpoints) != 6 {
				t.Fatalf("%d causal checkpoints", len(rec.Checkpoints))
			}
		}
		if s.Violations != 0 || s.OutcomeCounts[OutcomeError.String()] != 0 {
			t.Fatalf("correctness violated: %+v", s)
		}
		if problems := checkFinal(fx, rec, p); len(problems) != 0 {
			t.Fatal(problems)
		}
	})
}

func TestNearestRankOnAllObservations(t *testing.T) {
	var values []int64
	for i := int64(1000); i >= 1; i-- {
		values = append(values, i)
	}
	if d := scale.Summarize(values); d.Count != 1000 || d.P50 != 500 || d.P95 != 950 || d.P99 != 990 || d.Max != 1000 {
		t.Fatalf("nearest rank over 1000 = %+v", d)
	}
	// 101 distinct values 1000..900: ceil(.99*101)=100th smallest is 999.
	if v, _ := scale.NearestRank(values[:101], .99); v != 999 {
		t.Fatalf("p99 of 101 = %d, want 999", v)
	}
	small := []int64{9, 1, 5, 3, 7}
	if v, _ := scale.NearestRank(small, .99); v != 9 {
		t.Fatalf("p99 of five = %d, want the maximum", v)
	}
	if v, _ := scale.NearestRank(small, .50); v != 5 {
		t.Fatalf("p50 of five = %d", v)
	}

	// A measured arm keeps every observation, including refusals, with no filtering.
	refuse := func() (Outcome, uint64) { return OutcomeRefusedEnter, 0 }
	arm := MeasureArm(Lease, refuse, 4, 103)
	if arm.Summary.Count != 103 || len(arm.LatencyNS) != 103 || len(arm.Outcomes) != 103 || arm.OutcomeCounts["refused_enter"] != 103 ||
		!slices.Equal(arm.CallerCounts, []int{26, 26, 26, 25}) {
		t.Fatalf("arm dropped or filtered observations: %+v", arm.Summary)
	}
	if arm.Summary != scale.Summarize(arm.LatencyNS) {
		t.Fatal("summary is not computed over every recorded observation")
	}
	for _, v := range arm.LatencyNS {
		if v < 0 {
			t.Fatal("negative latency")
		}
	}
}

func TestOperationsAreSeparateAndCorrect(t *testing.T) {
	forBackends(t, func(t *testing.T, ctx context.Context, b scale.Backend) {
		fx := newFixture(t, ctx, b, FixtureOptions{Leaves: 2})
		for _, op := range []Operation{LookupView, Lease, Dispatch, Timer} {
			arm := MeasureArm(op, fx.Operation(op), 2, 50)
			if arm.OutcomeCounts["ok"] != 50 || arm.Operation != op {
				t.Fatalf("%s: %v", op, arm.OutcomeCounts)
			}
		}
	})
}

func passingInput() EvalInput {
	p := FrozenPlan()
	in := EvalInput{Plan: p, Candidates: []string{"reconciler", "graph-lock"}, Hosts: []HostDelta{{Run: "r", SwapKnown: true, ThrottleKnow: true}}, RunProtocols: []int{ProtocolAmendment, ProtocolAmendment}}
	dist := scale.Distribution{Count: SamplesPerRun, P50: 20, P95: 40, P99: 100, Max: 900}
	for _, cand := range in.Candidates {
		for _, c := range CallerConcurrencies() {
			for _, op := range []Operation{LookupView, Lease, Dispatch, Timer} {
				arm := "baseline"
				if op == Timer {
					arm = "timer"
				}
				a := Aggregate{Candidate: cand, Arm: arm, Operation: op, Callers: c, Runs: 10, Count: 1_000_000, Dist: dist}
				for range 10 {
					a.PerRun = append(a.PerRun, dist)
					a.Mallocs = append(a.Mallocs, 0)
					a.Throughput = append(a.Throughput, 1e7)
				}
				in.Aggregates = append(in.Aggregates, a)
			}
			for rep := range 10 {
				in.Stress = append(in.Stress, StressSummary{Rep: rep, Candidate: cand, Callers: c, Observations: SamplesPerRun,
					OutcomeCounts: map[string]int{"ok": SamplesPerRun}, Scheduled: ScheduledUpdates, Succeeded: ScheduledUpdates,
					OwnerUpdates: 12, OwnerReactivations: 12, ConsumerReactivations: 12})
			}
		}
	}
	return in
}

// The evaluator is pure: these cases pin the fail/invalid/inconclusive rules
// with synthetic numbers, never with measured data.
func TestEvaluateNeverPassesMissingOrFailedEvidence(t *testing.T) {
	if class, reasons := Evaluate(passingInput()); class != scale.Pass || len(reasons) != 0 {
		t.Fatalf("complete evidence classified %s: %v", class, reasons)
	}
	mutate := func(f func(*EvalInput)) EvalInput {
		in := passingInput()
		f(&in)
		return in
	}
	leaseIndex := func(in *EvalInput) *Aggregate {
		for i := range in.Aggregates {
			if in.Aggregates[i].Operation == Lease && in.Aggregates[i].Callers == 8 {
				return &in.Aggregates[i]
			}
		}
		return nil
	}
	timerIndex := func(in *EvalInput) *Aggregate {
		for i := range in.Aggregates {
			if in.Aggregates[i].Operation == Timer && in.Aggregates[i].Callers == 8 {
				return &in.Aggregates[i]
			}
		}
		return nil
	}
	cases := []struct {
		name string
		in   EvalInput
		want scale.Classification
	}{
		{"aggregate p99 over 2us", mutate(func(in *EvalInput) { leaseIndex(in).Dist.P99 = 2001 }), scale.Fail},
		{"one run p99 over 2us", mutate(func(in *EvalInput) { leaseIndex(in).PerRun[3].P99 = 2001 }), scale.Fail},
		{"one allocation in one run", mutate(func(in *EvalInput) { leaseIndex(in).Mallocs[7] = 1 }), scale.Fail},
		{"refusal in the mutation-free baseline", mutate(func(in *EvalInput) { leaseIndex(in).NonOK = 1 }), scale.Fail},
		{"inactive body executed", mutate(func(in *EvalInput) { in.Stress[0].Violations = 1 }), scale.Fail},
		{"overflow", mutate(func(in *EvalInput) { in.Stress[1].Overflow, in.Stress[1].Succeeded = 1, ScheduledUpdates-1 }), scale.Fail},
		{"drain timeout", mutate(func(in *EvalInput) { in.Stress[2].DrainTimedOut = true }), scale.Fail},
		{"candidate run error", mutate(func(in *EvalInput) { in.RunErrors = []string{"fixture: boom"} }), scale.Fail},
		{"fail beats invalid and inconclusive", mutate(func(in *EvalInput) {
			leaseIndex(in).Dist.P99 = 5000
			in.Invalid, in.IdentityProblems = []string{"x"}, []string{"y"}
		}), scale.Fail},
		{"control throughput noise", mutate(func(in *EvalInput) { timerIndex(in).Throughput[0] = 5e6 }), scale.Invalid},
		{"swap observed", mutate(func(in *EvalInput) { in.Hosts[0].SwapDelta = 1 }), scale.Invalid},
		{"throttle observed", mutate(func(in *EvalInput) { in.Hosts[0].ThrottleDiff = 1 }), scale.Invalid},
		{"protocol violation", mutate(func(in *EvalInput) { in.Invalid = []string{"GOMAXPROCS=24"} }), scale.Invalid},
		{"missing identity", mutate(func(in *EvalInput) { in.IdentityProblems = []string{"parent identity JSON missing"} }), scale.Inconclusive},
		{"missing raw evidence", mutate(func(in *EvalInput) { in.EvidenceProblems = []string{"raw record x unreadable"} }), scale.Inconclusive},
		{"noise unobservable", mutate(func(in *EvalInput) { in.Hosts[0].SwapKnown = false }), scale.Inconclusive},
		{"no noise records", mutate(func(in *EvalInput) { in.Hosts = nil }), scale.Inconclusive},
		{"too few observations", mutate(func(in *EvalInput) { leaseIndex(in).Count = 999_999 }), scale.Inconclusive},
		{"missing arm", mutate(func(in *EvalInput) { in.Aggregates = in.Aggregates[1:] }), scale.Inconclusive},
		{"missing stress runs", mutate(func(in *EvalInput) { in.Stress = in.Stress[1:] }), scale.Inconclusive},
		{"dry run", mutate(func(in *EvalInput) { in.Dry = true }), scale.Inconclusive},
		{"reduced plan", mutate(func(in *EvalInput) { in.Plan.Repetitions = 2 }), scale.Inconclusive},
	}
	for _, c := range cases {
		if class, reasons := Evaluate(c.in); class != c.want || len(reasons) == 0 {
			t.Errorf("%s: classified %s, want %s (%v)", c.name, class, c.want, reasons)
		}
	}
}

func TestPlanRejectsReducedAcceptanceAndKeepsFrozenValues(t *testing.T) {
	f := FrozenPlan()
	if !f.Frozen() || f.validate() != nil || f.Repetitions != 10 || f.SamplesPerRun != 100_000 || f.ScheduledUpdates != 12_000 ||
		!slices.Equal(f.Callers, []int{1, 8, 32}) || LeaseP99Limit != 2*time.Microsecond {
		t.Fatalf("frozen plan changed: %+v", f)
	}
	if DryPlan().Frozen() || tinyStress().Frozen() || DryPlan().validate() != nil || tinyStress().validate() != nil {
		t.Fatal("reduced plans must validate yet never be frozen")
	}
	out := filepath.Join(t.TempDir(), "never-created")
	if _, err := Run(context.Background(), Config{Plan: DryPlan(), Out: out, Root: repoRoot}); err == nil {
		t.Fatal("non-dry run accepted a reduced plan")
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("rejected run created evidence")
	}
}

func tinyPipeline() Plan {
	return Plan{Repetitions: 2, WarmupReps: WarmupRepetitions, SamplesPerRun: 200, StressSamples: 40, Leaves: 10, UpdatesPerSecond: 100, OutstandingLimit: 8,
		OwnerUpdateEvery: 5, ScheduledUpdates: 20, StressDuration: 200 * time.Millisecond, Callers: []int{1, 3}}
}

// The whole pipeline, with tiny counts: raw records, identity, manifest and a
// report that is labelled non-acceptance and never passes.
func TestDryPipelineWritesWellFormedRecordsAndRefusesOverwrite(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	p := tinyPipeline()
	rep, err := Run(context.Background(), Config{Plan: p, Dry: true, Out: out, Root: repoRoot})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Classification == scale.Pass || rep.Acceptance || !strings.Contains(rep.Label, "NOT ACCEPTANCE") {
		t.Fatalf("dry run label/classification: %s %q acceptance=%v", rep.Classification, rep.Label, rep.Acceptance)
	}
	if !strings.Contains(strings.Join(rep.Reasons, "\n"), "dry-run") {
		t.Fatal("dry run did not record why it is inconclusive")
	}
	// Two candidates x (baseline + two stress callers) x two repetitions, two warm-up
	// records, identity files and the report.
	names, _ := filepath.Glob(filepath.Join(out, "*.json"))
	if want := 2*2*(1+2) + 2 + 3; len(names) != want {
		t.Fatalf("%d records, want %d: %v", len(names), want, names)
	}
	if problems := (&evidenceWriter{dir: out, manifest: rep.Manifest}).verify(); len(problems) != 0 {
		t.Fatal(problems)
	}
	var id IdentityRecord
	readJSON(t, filepath.Join(out, "identity.json"), &id)
	if id.Acceptance || !strings.Contains(id.Mode, "NOT ACCEPTANCE") || id.SourceSHA256 == "" || id.CriteriaSHA256 != id.FreezeSHA256 || id.Executable == "" ||
		id.GOMAXPROCS != runtime.GOMAXPROCS(0) || id.GoVersion == "" {
		t.Fatalf("identity record: %+v", id)
	}
	for _, path := range []string{filepath.Join(out, "identity.json"), filepath.Join(out, "report.json")} {
		text, _ := os.ReadFile(path)
		if strings.Contains(string(text), "/home/") || strings.Contains(string(text), "/Users/") {
			t.Fatalf("%s records a workstation path", path)
		}
	}
	for _, cand := range []string{"reconciler", "graph-lock"} {
		for rep := range 2 {
			var base BaselineRecord
			readJSON(t, filepath.Join(out, "baseline-"+cand+"-rep0"+string(rune('0'+rep))+".json"), &base)
			if len(base.Arms) != 2*4 || len(base.Errors) != 0 || base.HostBefore.UTC.IsZero() {
				t.Fatalf("baseline record: arms=%d errors=%v", len(base.Arms), base.Errors)
			}
			for _, a := range base.Arms {
				if len(a.LatencyNS) != 200 || len(a.Outcomes) != 200 || a.Summary != scale.Summarize(a.LatencyNS) || a.Summary.Count != 200 {
					t.Fatalf("%s/c%d lost observations", a.Operation, a.Callers)
				}
			}
			for _, callers := range p.Callers {
				var sf stressFile
				readJSON(t, filepath.Join(out, "stress-"+cand+"-c0"+string(rune('0'+callers))+"-rep0"+string(rune('0'+rep))+".json"), &sf)
				if len(sf.Record.Mutations) != 20 || len(sf.Record.Observations.LatencyNS) != 40 || len(sf.Record.Checkpoints) < 2 || sf.Summary.File == "" {
					t.Fatalf("stress record incomplete: %+v", sf.Summary)
				}
			}
		}
	}
	// Candidate order alternates between repetitions.
	if rep.Aggregates[0].Runs != 2 {
		t.Fatalf("aggregate runs = %d", rep.Aggregates[0].Runs)
	}
	if _, err := Run(context.Background(), Config{Plan: p, Dry: true, Out: out, Root: repoRoot}); err == nil {
		t.Fatal("existing evidence directory was reused")
	}
}

func readJSON(t *testing.T, path string, into any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, into); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// Missing raw evidence is detected, not assumed.
func TestEvidenceVerificationDetectsTampering(t *testing.T) {
	dir := t.TempDir()
	w := &evidenceWriter{dir: dir}
	if err := w.write("a.json", map[string]int{"x": 1}); err != nil {
		t.Fatal(err)
	}
	if err := w.write("a.json", 1); err == nil {
		t.Fatal("exclusive write replaced evidence")
	}
	if len(w.verify()) != 0 {
		t.Fatal("clean evidence reported as damaged")
	}
	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if len(w.verify()) == 0 {
		t.Fatal("changed evidence not detected")
	}
	if err := os.Remove(filepath.Join(dir, "a.json")); err != nil {
		t.Fatal(err)
	}
	if len(w.verify()) == 0 {
		t.Fatal("missing evidence not detected")
	}
}

func TestIdentityRequiresParentFactsAndNeverInventsThem(t *testing.T) {
	rec, closure := CollectIdentity(repoRoot, FrozenPlan(), false, nil)
	if len(rec.Problems) == 0 || closure.SHA256 == "" || rec.CriteriaSHA256 == "" {
		t.Fatalf("missing parent identity went unnoticed: %v", rec.Problems)
	}
	if class, _ := Evaluate(EvalInput{Plan: FrozenPlan(), IdentityProblems: rec.Problems}); class == scale.Pass {
		t.Fatal("missing identity can pass")
	}
	good := Identity{BaselineCommit: "abc", Commands: []string{"x"}, FinalParity: true, HostEvidence: "h", ExpectedSourceSHA256: strings.Repeat("0", 64)}
	rec, _ = CollectIdentity(repoRoot, FrozenPlan(), false, &good)
	if len(rec.Problems) != 0 && !strings.Contains(strings.Join(rec.Problems, ";"), "affinity") && !strings.Contains(strings.Join(rec.Problems, ";"), "kernel") {
		t.Fatalf("complete parent identity rejected: %v", rec.Problems)
	}
	if !strings.Contains(strings.Join(rec.Invalid, ";"), "differs from computed") {
		t.Fatalf("mismatched expected digest not flagged: %v", rec.Invalid)
	}
}

// The closure hash must cover every module-local package the test binary links.
func TestSourceClosureCoversLinkedModulePackages(t *testing.T) {
	gomod, err := os.ReadFile(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	module := ""
	for _, line := range strings.Split(string(gomod), "\n") {
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			module = strings.TrimSpace(rest)
		}
	}
	covered := append(slices.Clone(closureClasses), callpathClass)
	seen := map[string]bool{}
	queue := []string{callpathClass}
	for len(queue) > 0 {
		dir := queue[0]
		queue = queue[1:]
		if seen[dir] {
			continue
		}
		seen[dir] = true
		if !slices.Contains(covered, dir) {
			t.Errorf("linked package %s is not in the source closure", dir)
		}
		files, _ := filepath.Glob(filepath.Join(repoRoot, filepath.FromSlash(dir), "*.go"))
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") && dir != callpathClass {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range f.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				if rest, ok := strings.CutPrefix(path, module+"/"); ok {
					queue = append(queue, rest)
				}
			}
		}
	}
	c, err := sourceClosure(repoRoot)
	if err != nil || len(c.Files) < 20 || c.SHA256 == "" {
		t.Fatalf("closure: %v %d", err, len(c.Files))
	}
	for _, f := range c.Files {
		if strings.HasPrefix(f.Path, "/") || strings.Contains(f.Path, "..") {
			t.Fatalf("closure path %s is not repository-relative", f.Path)
		}
	}
}

// supportedBackends is the scale table for a test: a backend whose adapter lacks a
// capability the harness needs becomes a skipped subtest that prints the gap. It
// is never counted as a pass and never dropped without a trace.
func supportedBackends(t *testing.T) []scale.Backend {
	t.Helper()
	run, unsupported := scale.Runnable(scale.Candidates())
	for _, u := range unsupported {
		t.Run(u.Name, func(t *testing.T) { t.Skipf("UNSUPPORTED backend %s, not run, not a pass: %s", u.Name, u.Gap) })
	}
	return run
}
