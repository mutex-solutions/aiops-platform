// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestColdPlanIsTheFrozenSection9(t *testing.T) {
	p := FrozenColdPlan()
	if !p.Frozen() || p.validate() != nil {
		t.Fatal("frozen cold plan is not frozen/valid")
	}
	if p.ScheduledRequests() != 15_000 || p.MeasuredRequests() != 12_000 || p.ScheduledChurn() != 7_500 {
		t.Fatalf("arrivals: requests %d measured %d churn %d", p.ScheduledRequests(), p.MeasuredRequests(), p.ScheduledChurn())
	}
	if p.Repetitions != 10 || p.Warmup != 30*time.Second || p.Window != 120*time.Second || p.RequestsPerSecond != 100 || p.ChurnPerSecond != 50 ||
		p.ChurnCap != 8 || p.IdleBeforeDispose != 100*time.Millisecond || p.Labels != 500 || p.TreeWidth != 4 || p.RequestCap != 32 || p.RequestTimeout != 5*time.Second {
		t.Fatalf("a section 9 number changed: %+v", p)
	}
	if len(p.Bodies) != 2 || p.Bodies[0].Items != 100 || p.Bodies[1].Items*ItemBytes != 1<<20 {
		t.Fatalf("bodies: %+v", p.Bodies)
	}
	if AckP99Items != 250*time.Millisecond || AckP99MiB != time.Second || ChurnDeltaLimit != 5*time.Millisecond || SuccessFloor != 0.999 {
		t.Fatal("a section 9 threshold changed")
	}
	if DryColdPlan().Frozen() {
		t.Fatal("the dry cold plan claims to be frozen")
	}
	for _, mutate := range []func(*ColdPlan){
		func(p *ColdPlan) { p.Repetitions = 9 }, func(p *ColdPlan) { p.Window = time.Second }, func(p *ColdPlan) { p.ChurnPerSecond = 49 },
		func(p *ColdPlan) { p.ChurnCap = 9 }, func(p *ColdPlan) { p.Labels = 499 }, func(p *ColdPlan) { p.Bodies = p.Bodies[:1] }, func(p *ColdPlan) { p.Dry = true },
	} {
		bad := FrozenColdPlan()
		mutate(&bad)
		if bad.Frozen() {
			t.Errorf("mutated cold plan is still frozen: %+v", bad)
		}
	}
	if _, err := MeasureColdMount(context.Background(), IdentityRecord{}, ColdPlan{Repetitions: 1, Warmup: 0, Window: time.Second, Drain: time.Second, RequestsPerSecond: 1, RequestCap: 1, ChurnPerSecond: 1, ChurnCap: 1, RequestTimeout: time.Second, Labels: 2, TreeWidth: 4, Bodies: []BodySize{{Name: "b", Items: 1}}}, func(ColdRun) error { return nil }); err == nil {
		t.Fatal("a reduced cold plan not labelled Dry was run")
	}
	if _, err := MeasureColdMount(context.Background(), IdentityRecord{Problems: []string{"x"}}, FrozenColdPlan(), func(ColdRun) error { return nil }); err == nil {
		t.Fatal("frozen cold plan with identity problems must refuse before running")
	}
}

// The cold-timeout fixture on both real candidates: 503 with Retry-After, or a
// snooze; never a commit; never a fall-through to the warm foreign scope.
func TestColdTimeoutFixture(t *testing.T) {
	for _, backend := range supportedBackends(t) {
		t.Run(backend.Name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			res := RunColdTimeoutFixture(ctx, backend, FrozenColdPlan())
			if got := res.Findings(); len(got) != 0 {
				t.Fatalf("%+v: %v", res, got)
			}
			if res.SyncStatus != 503 || !res.JobSnoozed || res.LedgerAfter != 0 || !res.Warm {
				t.Fatalf("%+v", res)
			}
		})
	}
	bad := ColdTimeoutResult{Backend: "x", SyncStatus: 200, SyncCommitted: true, LedgerAfter: 1, ForeignContributed: true, Warm: true}
	if got := bad.Findings(); len(got) < 3 || got[0].Class != Fail {
		t.Fatalf("an acknowledged uncommitted write / fall-through was not a failure: %v", got)
	}
}

// microColdPlan keeps section 9's rates and caps with a fraction of a second of
// load and enough scope labels that none is reused: structure, not performance.
func microColdPlan() ColdPlan {
	p := FrozenColdPlan()
	p.Dry, p.Repetitions = true, 1
	p.Warmup, p.Window, p.Drain = 100*time.Millisecond, 300*time.Millisecond, 20*time.Second
	p.Labels = 64
	return p
}

// Every arm on both real candidates: what is simulated and what is counted.
func TestColdArmsStructure(t *testing.T) {
	plan := microColdPlan()
	body := BodySize{Name: "100-items", Items: 100}
	for _, backend := range supportedBackends(t) {
		for _, arm := range []ColdArm{ArmB0, ArmB1, ArmA} {
			t.Run(backend.Name+"/"+string(arm), func(t *testing.T) {
				run := RunColdArm(context.Background(), backend, plan, body, arm, 0)
				if len(run.Errors) != 0 {
					t.Fatalf("errors: %v", run.Errors)
				}
				if len(run.Acks) != plan.ScheduledRequests() || len(run.Churn) != plan.ScheduledChurn() {
					t.Fatalf("arrivals recorded: %d requests %d churn", len(run.Acks), len(run.Churn))
				}
				committed := 0
				for _, a := range run.Acks {
					if a.Outcome != "ok" || !a.Committed || a.Status != 200 {
						t.Fatalf("request %d: %+v", a.Seq, a)
					}
					committed++
					if a.EndedNS-a.ScheduledNS != a.LatencyNS {
						t.Fatalf("latency is not measured from the scheduled arrival: %+v", a)
					}
				}
				if run.Ledger != committed || run.Outstanding != 0 || !run.GoroutinesOK {
					t.Fatalf("ledger %d committed %d outstanding %d goroutinesOK %v", run.Ledger, committed, run.Outstanding, run.GoroutinesOK)
				}
				switch arm {
				case ArmA:
					if run.ColdEnsures != int64(len(run.Acks)) {
						t.Fatalf("arm A cold ensures %d for %d requests", run.ColdEnsures, len(run.Acks))
					}
					for _, a := range run.Acks {
						if a.ColdEnsures != 1 || a.MountNS <= 0 {
							t.Fatalf("arm A request %d lacks its inline mount: %+v", a.Seq, a)
						}
					}
				case ArmB0, ArmB1:
					if run.ColdEnsures != 0 {
						t.Fatalf("arm %s has %d cold-ensure calls on the ack path", arm, run.ColdEnsures)
					}
				}
				for _, ch := range run.Churn {
					switch arm {
					case ArmB1:
						if ch.Outcome == "overflow" {
							continue // a loaded host may hit the cap of eight; the eval rule fails that run
						}
						if ch.Outcome != "ok" || !ch.MountedOK || !ch.DisposedOK || ch.DisposedNS < ch.DisposeAtNS || ch.DisposeAtNS-ch.MountedNS < plan.IdleBeforeDispose.Nanoseconds() {
							t.Fatalf("B1 churn %d: %+v", ch.Seq, ch)
						}
					default:
						if ch.MountedOK || ch.Outcome != "idle(B0)" {
							t.Fatalf("scheduler without mounts started work: %+v", ch)
						}
					}
				}
			})
		}
	}
}

func syntheticRun(plan ColdPlan, backend string, body BodySize, arm ColdArm, rep int, p99 time.Duration) ColdRun {
	run := ColdRun{Backend: backend, Body: body, Arm: arm, Repetition: rep, GoroutinesOK: true,
		Host: HostDelta{SwapKnown: true, ThrottleKnow: true}}
	for i := 0; i < plan.ScheduledRequests(); i++ {
		sched := int64(i) * int64(time.Second) / int64(plan.RequestsPerSecond)
		lat := int64(time.Millisecond)
		if i%50 == 49 {
			lat = p99.Nanoseconds() // 2% of observations, so the nearest-rank p99 (and the maximum) is this value.
		}
		run.Acks = append(run.Acks, AckRecord{Seq: i, Warmup: time.Duration(sched) < plan.Warmup, ScheduledNS: sched, EndedNS: sched + lat, LatencyNS: lat, Outcome: "ok", Status: 200, Committed: true})
	}
	for i := 0; i < plan.ScheduledChurn(); i++ {
		ch := ChurnRecord{Seq: i, Outcome: "idle(B0)"}
		if arm == ArmB1 {
			ch = ChurnRecord{Seq: i, Outcome: "ok", MountedOK: true, DisposedOK: true, MountLatency: 300_000}
		}
		run.Churn = append(run.Churn, ch)
	}
	if arm == ArmA {
		run.ColdEnsures = int64(len(run.Acks))
	}
	run.Ledger = len(run.Acks)
	return run
}

// syntheticSet is a complete, passing set of runs for both candidates.
func syntheticSet(plan ColdPlan, mutate func(*ColdRun)) []ColdRun {
	var runs []ColdRun
	for _, backend := range []string{"reconciler", "graph-lock"} {
		for _, body := range plan.Bodies {
			for rep := 0; rep < plan.Repetitions; rep++ {
				for _, arm := range []ColdArm{ArmA, ArmB0, ArmB1} {
					run := syntheticRun(plan, backend, body, arm, rep, 3*time.Millisecond)
					if mutate != nil {
						mutate(&run)
					}
					runs = append(runs, run)
				}
			}
		}
	}
	return runs
}

func goodTimeouts() []ColdTimeoutResult {
	var out []ColdTimeoutResult
	for _, b := range []string{"reconciler", "graph-lock"} {
		out = append(out, ColdTimeoutResult{Backend: b, SyncStatus: 503, SyncRetryAfterNS: int64(time.Second), JobSnoozed: true, Warm: true})
	}
	return out
}

// evalPlan is a NON-dry plan whose counts are small, so synthetic records stay
// cheap while every floor and rule (ten repetitions, caps, limits) is live.
func evalPlan() ColdPlan {
	p := FrozenColdPlan()
	p.Warmup, p.Window = 0, time.Second
	return p
}

func classOf(findings []Finding) Classification {
	class, _ := Resolve(findings, false)
	return class
}

func TestEvaluateColdSection9(t *testing.T) {
	plan := evalPlan()
	backends := []string{"reconciler", "graph-lock"}
	good, _ := EvaluateCold(plan, backends, syntheticSet(plan, nil), goodTimeouts())
	if class := classOf(good); class != Pass {
		t.Fatalf("a complete passing set did not pass: %s %v", class, good)
	}
	type mutation struct {
		name   string
		mutate func(*ColdRun)
		want   Classification
		drop   func([]ColdRun) []ColdRun
		noTO   bool
	}
	setLatency := func(run *ColdRun, d time.Duration) {
		for i := range run.Acks {
			if i%50 == 49 {
				run.Acks[i].LatencyNS = d.Nanoseconds()
			}
		}
	}
	cases := []mutation{
		{name: "B1 p99 6 ms over B0 in every pair", want: Fail, mutate: func(r *ColdRun) {
			if r.Arm == ArmB1 {
				setLatency(r, 9*time.Millisecond)
			}
		}},
		{name: "B1 5 ms over B0 is within the limit", want: Pass, mutate: func(r *ColdRun) {
			if r.Arm == ArmB1 {
				setLatency(r, 8*time.Millisecond)
			}
		}},
		{name: "B1 below B0 passes", want: Pass, mutate: func(r *ColdRun) {
			if r.Arm == ArmB0 {
				setLatency(r, 20*time.Millisecond)
			}
		}},
		{name: "one matched pair breaks 5 ms while the aggregate holds", want: Fail, mutate: func(r *ColdRun) {
			if r.Arm == ArmB1 && r.Repetition == 3 && r.Body.Items == 100 && r.Backend == "reconciler" {
				setLatency(r, 20*time.Millisecond)
			}
		}},
		{name: "100-item B p99 over 250 ms", want: Fail, mutate: func(r *ColdRun) {
			if r.Arm == ArmB0 && r.Body.Items == 100 {
				setLatency(r, 251*time.Millisecond)
			}
		}},
		{name: "1 MiB B p99 over 1 s", want: Fail, mutate: func(r *ColdRun) {
			if r.Arm == ArmB0 && r.Body.Items > 100 {
				setLatency(r, time.Second+time.Millisecond)
			}
		}},
		{name: "1 MiB at 900 ms is within the limit and the delta is checked separately", want: Pass, mutate: func(r *ColdRun) {
			if r.Body.Items > 100 && r.Arm != ArmA {
				setLatency(r, 900*time.Millisecond)
			}
		}},
		{name: "arm B cold ensure on the ack path", want: Fail, mutate: func(r *ColdRun) {
			if r.Arm == ArmB0 && r.Repetition == 0 {
				r.ColdEnsures = 1
			}
		}},
		{name: "arm A without cold ensures", want: Fail, mutate: func(r *ColdRun) {
			if r.Arm == ArmA && r.Repetition == 0 {
				r.ColdEnsures = 0
			}
		}},
		{name: "B1 churn overflow", want: Fail, mutate: func(r *ColdRun) {
			if r.Arm == ArmB1 && r.Repetition == 2 {
				r.Churn[7].Outcome, r.Churn[7].MountedOK = "overflow", false
			}
		}},
		{name: "B1 churn arrival started late", want: Fail, mutate: func(r *ColdRun) {
			if r.Arm == ArmB1 && r.Repetition == 2 {
				r.Churn[7].Late = true
			}
		}},
		{name: "B1 churn not disposed", want: Fail, mutate: func(r *ColdRun) {
			if r.Arm == ArmB1 && r.Repetition == 2 {
				r.Churn[1].DisposedOK = false
			}
		}},
		{name: "B0 scheduler started a mount", want: Fail, mutate: func(r *ColdRun) {
			if r.Arm == ArmB0 && r.Repetition == 2 {
				r.Churn[1].MountedOK = true
			}
		}},
		{name: "request cap overflow", want: Fail, mutate: func(r *ColdRun) {
			if r.Arm == ArmB0 && r.Repetition == 1 {
				r.Acks[40].Outcome = "overflow"
			}
		}},
		{name: "one timeout in a cell stays under 99.9%? 1 in 1000 is allowed", want: Pass, mutate: func(r *ColdRun) {
			if r.Arm == ArmB0 && r.Repetition == 1 && r.Body.Items == 100 && r.Backend == "reconciler" {
				r.Acks[40].Outcome = "timeout"
			}
		}},
		{name: "acknowledged without a commit", want: Fail, mutate: func(r *ColdRun) {
			if r.Arm == ArmB0 && r.Repetition == 1 {
				r.Acks[3].Committed = false
			}
		}},
		{name: "run reported an error", want: Fail, mutate: func(r *ColdRun) {
			if r.Arm == ArmA && r.Repetition == 4 {
				r.Errors = []string{"teardown: leaked"}
			}
		}},
		{name: "outstanding after drain", want: Fail, mutate: func(r *ColdRun) {
			if r.Arm == ArmB1 && r.Repetition == 4 {
				r.Outstanding = 2
			}
		}},
		{name: "swap during a run", want: Invalid, mutate: func(r *ColdRun) {
			if r.Arm == ArmB0 && r.Repetition == 4 {
				r.Host.SwapDelta = 7
			}
		}},
		{name: "a failure outranks swap noise", want: Fail, mutate: func(r *ColdRun) {
			if r.Arm == ArmB0 && r.Repetition == 4 {
				r.Host.SwapDelta, r.ColdEnsures = 7, 3
			}
		}},
		{name: "a lost arrival record", want: Inconclusive, drop: func(runs []ColdRun) []ColdRun {
			runs[5].Acks = runs[5].Acks[:len(runs[5].Acks)-1]
			return runs
		}},
		{name: "a missing repetition", want: Inconclusive, drop: func(runs []ColdRun) []ColdRun { return runs[3:] }},
		{name: "a missing cold-timeout fixture", want: Inconclusive, noTO: true},
	}
	for _, c := range cases {
		runs := syntheticSet(plan, c.mutate)
		if c.drop != nil {
			runs = c.drop(runs)
		}
		to := goodTimeouts()
		if c.noTO {
			to = to[:1]
		}
		findings, _ := EvaluateCold(plan, backends, runs, to)
		if got := classOf(findings); got != c.want {
			t.Errorf("%s: %s, want %s (%v)", c.name, got, c.want, findings)
		}
	}
	// Every acknowledgement counts: a run of failures cannot hide behind success-only latency.
	runs := syntheticSet(plan, func(r *ColdRun) {
		if r.Arm == ArmB0 && r.Repetition == 0 {
			for i := range r.Acks {
				if i%50 == 0 {
					r.Acks[i].Outcome, r.Acks[i].LatencyNS = "timeout", 5*int64(time.Second)
				}
			}
		}
	})
	findings, cells := EvaluateCold(plan, backends, runs, goodTimeouts())
	if classOf(findings) != Fail {
		t.Error("2% failed requests passed")
	}
	cell := cells[ColdKey{"reconciler", "100-items", ArmB0}]
	if cell.Outcomes["timeout"] == 0 || cell.Aggregate.Max < 5*int64(time.Second) || cell.Scheduled != 10*plan.MeasuredRequests() {
		t.Errorf("failed attempts left the denominator or the tail: %+v", cell)
	}
	// Missing a whole cell is inconclusive, and the dry plan cannot soften a failure.
	if f, _ := EvaluateCold(plan, []string{"reconciler", "graph-lock", "extra"}, syntheticSet(plan, nil), goodTimeouts()); classOf(f) != Inconclusive {
		t.Error("an absent candidate was not inconclusive")
	}
	dry := plan
	dry.Dry = true
	f, _ := EvaluateCold(dry, backends, syntheticSet(dry, nil), goodTimeouts())
	if class, _ := Resolve(f, true); class != Inconclusive {
		t.Errorf("a dry evaluation of a complete set = %s, want inconclusive", class)
	}
	if !strings.Contains(strings.Join(func() []string { _, r := Resolve(f, true); return r }(), " "), "DRY RUN") {
		t.Error("dry evaluation is not labelled")
	}
}
