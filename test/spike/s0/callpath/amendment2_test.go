// SPDX-License-Identifier: Apache-2.0

package callpath

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mutex-solutions/aiops-platform/test/spike/s0/scale"
)

// Tests for protocol amendment 2. None asserts a timing threshold.

func TestAmendmentIsTwo(t *testing.T) {
	if ProtocolAmendment != 2 || WarmupRepetitions != 1 || !FrozenPlan().Frozen() || FrozenPlan().WarmupReps != 1 || DryPlan().WarmupReps != 1 {
		t.Fatal("amendment 2 constants changed")
	}
}

// The warm-up is a recorded, labelled baseline repetition per candidate whose
// measurements enter no classification input, and every repetition (warm-up and
// measured, baseline and stress) mounts its own fresh fixture.
func TestWarmupIsRecordedLabelledExcludedAndFixturesAreFresh(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	p := tinyPipeline()
	rep, err := Run(context.Background(), Config{Plan: p, Dry: true, Out: out, Root: repoRoot})
	if err != nil {
		t.Fatal(err)
	}
	names := runnableNames()
	if rep.Protocol != 2 || len(rep.Warmup) != len(names) {
		t.Fatalf("report protocol %d, %d warm-up entries", rep.Protocol, len(rep.Warmup))
	}
	var seqs []uint64
	for _, cand := range names {
		var w BaselineRecord
		readJSON(t, filepath.Join(out, "baseline-"+cand+"-warmup.json"), &w)
		if !w.Warmup || !strings.Contains(w.Label, "warmup") || w.Protocol != 2 || w.Candidate != cand || len(w.Arms) != 2*4 || len(w.Errors) != 0 {
			t.Fatalf("warm-up record: %+v", w)
		}
		seqs = append(seqs, w.FixtureSeq)
		for r := range p.Repetitions {
			var m BaselineRecord
			readJSON(t, filepath.Join(out, "baseline-"+cand+"-rep0"+string(rune('0'+r))+".json"), &m)
			if m.Warmup || m.Protocol != 2 {
				t.Fatalf("measured record labelled warm-up or wrong amendment: %+v", m)
			}
			seqs = append(seqs, m.FixtureSeq)
			for _, callers := range p.Callers {
				var sf stressFile
				readJSON(t, filepath.Join(out, "stress-"+cand+"-c0"+string(rune('0'+callers))+"-rep0"+string(rune('0'+r))+".json"), &sf)
				seqs = append(seqs, sf.FixtureSeq)
			}
		}
	}
	for _, s := range seqs {
		if s == 0 {
			t.Fatalf("fixture sequence missing: %v", seqs)
		}
	}
	slices.Sort(seqs)
	if len(slices.Compact(slices.Clone(seqs))) != len(seqs) {
		t.Fatalf("a fixture was shared between repetitions: %v", seqs)
	}
	// Exclusion: only the measured repetitions are counted.
	for _, a := range rep.Aggregates {
		if a.Arm == "stress" {
			continue
		}
		if a.Runs != p.Repetitions || a.Count != p.Repetitions*p.SamplesPerRun || len(a.Mallocs) != p.Repetitions || len(a.Throughput) != p.Repetitions {
			t.Fatalf("aggregate %s/%s/c%d counted warm-up data: runs=%d count=%d", a.Candidate, a.Operation, a.Callers, a.Runs, a.Count)
		}
	}
	for _, e := range rep.Warmup {
		if len(e.Arms) != 8 || e.File == "" || e.FixtureSeq == 0 {
			t.Fatalf("warm-up listing incomplete: %+v", e)
		}
	}
}

// Warm-up measurements reach no classification input, but its integrity does.
func TestWarmupIntegrityFailureStillFailsTheRun(t *testing.T) {
	odd := BaselineRecord{Protocol: ProtocolAmendment, Warmup: true, Candidate: "reconciler", FixtureSeq: 1,
		Arms: []Arm{{Operation: Lease, Callers: 1, Mallocs: 99, ThroughputPS: 1, OutcomeCounts: map[string]int{"ok": 1}}}}
	cases := []struct {
		name   string
		mutate func(*BaselineRecord)
		want   scale.Classification
		text   string
	}{
		{"wild measurements only", func(*BaselineRecord) {}, scale.Pass, ""},
		{"error outcome", func(r *BaselineRecord) { r.Errors = []string{"lookup_view/c8: map[error:3]"} }, scale.Fail, "warmup reconciler"},
		{"refused baseline observation", func(r *BaselineRecord) { r.Errors = []string{"owner_enter_leave/c1: map[refused_enter:1]"} }, scale.Fail, "refused_enter"},
		{"inactive body executed", func(r *BaselineRecord) { r.Errors = []string{"1 inactive-owner bodies executed"} }, scale.Fail, "inactive-owner"},
		{"harness error", func(r *BaselineRecord) { r.Errors = []string{"fixture: boom"} }, scale.Fail, "fixture: boom"},
		{"other amendment", func(r *BaselineRecord) { r.Protocol = 1 }, scale.Invalid, "amendment 1"},
	}
	for _, c := range cases {
		in := passingInput()
		rec := odd
		c.mutate(&rec)
		s := foldWarmup(&in, rec, "baseline-reconciler-warmup.json")
		class, reasons := Evaluate(in)
		if class != c.want || !strings.Contains(strings.Join(reasons, "\n"), c.text) {
			t.Errorf("%s: classified %s, want %s: %v", c.name, class, c.want, reasons)
		}
		if len(s.Arms) != 1 || s.Arms[0].Mallocs != 99 {
			t.Errorf("%s: warm-up reading not listed: %+v", c.name, s)
		}
	}
}

func TestNoiseRuleReadsTheTimerControl(t *testing.T) {
	// Arm 8 callers of reconciler: lease (owner Enter+Leave) and the timer control.
	set := func(f func(arm, ctl *Aggregate)) EvalInput {
		in := passingInput()
		var arm, ctl *Aggregate
		for i := range in.Aggregates {
			a := &in.Aggregates[i]
			if a.Candidate == "reconciler" && a.Callers == 8 {
				switch a.Operation {
				case Lease:
					arm = a
				case Timer:
					ctl = a
				}
			}
		}
		f(arm, ctl)
		return in
	}
	noisy := func(a *Aggregate) { a.Throughput[0] = 5e6 } // CV well above 10% of 1e7.
	cases := []struct {
		name string
		in   EvalInput
		want scale.Classification
		text string
	}{
		{"clean control, noisy arm", set(func(a, _ *Aggregate) { noisy(a) }), scale.Pass, "OBSERVATION: baseline reconciler/owner_enter_leave/c8: candidateVariation"},
		{"noisy control, clean arm", set(func(_, c *Aggregate) { noisy(c) }), scale.Invalid, "timer-only control CV"},
		{"noisy control, noisy arm", set(func(a, c *Aggregate) { noisy(a); noisy(c) }), scale.Invalid, "timer-only control CV"},
		{"control missing", set(func(_, c *Aggregate) { c.Throughput = nil }), scale.Inconclusive, "control throughput CV unavailable"},
	}
	for _, c := range cases {
		class, reasons := Evaluate(c.in)
		if class != c.want || !strings.Contains(strings.Join(reasons, "\n"), c.text) {
			t.Errorf("%s: classified %s, want %s: %v", c.name, class, c.want, reasons)
		}
	}
	// The observation lands on the aggregate as a value, and the arm is not otherwise flagged.
	in := set(func(a, _ *Aggregate) { noisy(a) })
	_, reasons := Evaluate(in)
	for _, r := range reasons {
		if !strings.HasPrefix(r, "OBSERVATION: ") {
			t.Errorf("unexpected non-observation reason on a pass-eligible input: %s", r)
		}
	}
	var got float64
	for _, a := range in.Aggregates {
		if a.Operation == Lease && a.Callers == 8 && a.Candidate == "reconciler" {
			got = a.CandidateVariation
		}
	}
	if got <= NoiseCVLimit {
		t.Fatalf("candidateVariation = %v, want above the limit", got)
	}
	// A noisy arm never hides a candidate failure.
	in = set(func(a, _ *Aggregate) { noisy(a); a.Mallocs[2] = 1 })
	if class, _ := Evaluate(in); class != scale.Fail {
		t.Fatalf("noisy arm with an allocation classified %s", class)
	}
}

// Records of an earlier amendment are never evaluated under amendment 2 rules.
func TestAmendmentOneRecordsAreNotEvaluatedUnderAmendmentTwo(t *testing.T) {
	in := passingInput()
	in.RunProtocols = []int{1, 1, 1}
	class, reasons := Evaluate(in)
	if class != scale.Invalid || !strings.Contains(strings.Join(reasons, "\n"), "protocol amendment 1") {
		t.Fatalf("amendment 1 records classified %s: %v", class, reasons)
	}
	in.RunProtocols = []int{1, ProtocolAmendment}
	if class, reasons := Evaluate(in); class != scale.Invalid || !strings.Contains(strings.Join(reasons, "\n"), "mix") {
		t.Fatalf("mixture classified %s: %v", class, reasons)
	}
}
