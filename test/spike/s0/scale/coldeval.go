// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"fmt"
	"time"
)

// ColdKey names one backend x body x arm cell.
type ColdKey struct {
	Backend string
	Body    string
	Arm     ColdArm
}

func (k ColdKey) String() string { return fmt.Sprintf("%s/%s/%s", k.Backend, k.Body, k.Arm) }

// ColdCell is the evaluation view of one cell over all repetitions.
type ColdCell struct {
	Backend      string         `json:"backend"`
	Body         string         `json:"body"`
	Arm          ColdArm        `json:"arm"`
	Runs         int            `json:"runs"`
	Scheduled    int            `json:"measuredScheduled"`
	Aggregate    Distribution   `json:"latencyAllAttempts"`
	PerRun       []Distribution `json:"perRunLatency"`
	Outcomes     map[string]int `json:"outcomes"`
	SuccessRatio float64        `json:"successRatio"`
	ColdEnsures  int64          `json:"coldEnsureCalls"`
	Mount        Distribution   `json:"mountLatency"` // arm A inline mounts; B1 background mounts.
	Throughput   []float64      `json:"throughputPerSecond"`
}

// measured returns the latencies and outcomes of the measured window only.
func measured(run ColdRun) (latencies []int64, outcomes map[string]int, firstSched, lastEnd int64) {
	outcomes = map[string]int{}
	first := true
	for _, a := range run.Acks {
		if a.Warmup {
			continue
		}
		latencies = append(latencies, a.LatencyNS)
		outcomes[a.Outcome]++
		if first || a.ScheduledNS < firstSched {
			firstSched, first = a.ScheduledNS, false
		}
		if a.EndedNS > lastEnd && a.Outcome == "ok" {
			lastEnd = a.EndedNS
		}
	}
	return
}

// Cells groups runs into cells. An arrival with no record (an unset slot) is an
// error outcome, never silently absent.
func Cells(runs []ColdRun) map[ColdKey]*ColdCell {
	cells := map[ColdKey]*ColdCell{}
	pooled := map[ColdKey][]int64{}
	mounts := map[ColdKey][]int64{}
	for _, run := range runs {
		key := ColdKey{run.Backend, run.Body.Name, run.Arm}
		c := cells[key]
		if c == nil {
			c = &ColdCell{Backend: key.Backend, Body: key.Body, Arm: key.Arm, Outcomes: map[string]int{}}
			cells[key] = c
		}
		lat, out, first, last := measured(run)
		c.Runs++
		c.Scheduled += len(lat)
		pooled[key] = append(pooled[key], lat...)
		c.PerRun = append(c.PerRun, Summarize(lat))
		for k, v := range out {
			c.Outcomes[k] += v
		}
		c.ColdEnsures += run.ColdEnsures
		for _, a := range run.Acks {
			if !a.Warmup && a.MountNS > 0 {
				mounts[key] = append(mounts[key], a.MountNS)
			}
		}
		for _, ch := range run.Churn {
			if ch.MountedOK && !ch.Warmup {
				mounts[key] = append(mounts[key], ch.MountLatency)
			}
		}
		if last > first {
			c.Throughput = append(c.Throughput, float64(out["ok"])*1e9/float64(last-first))
		}
	}
	for key, c := range cells {
		c.Aggregate = Summarize(pooled[key])
		c.Mount = Summarize(mounts[key])
		if c.Scheduled > 0 {
			c.SuccessRatio = float64(c.Outcomes["ok"]) / float64(c.Scheduled)
		}
	}
	return cells
}

// runFindings holds every per-run protocol check of one cold-mount arm run.
func runFindings(plan ColdPlan, run ColdRun) []Finding {
	var out []Finding
	label := fmt.Sprintf("%s/%s/%s rep%d", run.Backend, run.Body.Name, run.Arm, run.Repetition)
	for _, e := range run.Errors {
		out = append(out, fail("%s: %s", label, e))
	}
	if len(run.Acks) != plan.ScheduledRequests() {
		out = append(out, inconclusive("%s: %d request arrivals recorded, %d scheduled", label, len(run.Acks), plan.ScheduledRequests()))
	}
	if len(run.Churn) != plan.ScheduledChurn() {
		out = append(out, inconclusive("%s: %d churn arrivals recorded, %d scheduled", label, len(run.Churn), plan.ScheduledChurn()))
	}
	unset := 0
	for _, a := range run.Acks {
		switch a.Outcome {
		case "ok", "unavailable503", "timeout":
			// Counted by the 99.9% success rule of §4, with their latency kept.
		case "":
			unset++
		default:
			// Overflow of the request cap is a dropped arrival (§4) and an error is
			// a correctness failure: neither is tolerated by the 99.9% rule.
			out = append(out, fail("%s: request %d outcome %s %s", label, a.Seq, a.Outcome, a.Error))
		}
		if a.Outcome == "ok" && !a.Committed {
			out = append(out, fail("%s: request %d acknowledged without a commit", label, a.Seq))
		}
		if len(out) > 20 {
			break
		}
	}
	if unset > 0 {
		out = append(out, fail("%s: %d scheduled request arrivals have no outcome (lost)", label, unset))
	}
	if run.Arm == ArmB0 || run.Arm == ArmB1 {
		if run.ColdEnsures != 0 {
			out = append(out, fail("%s: %d cold-ensure/mount calls on the acknowledgement path, want zero", label, run.ColdEnsures))
		}
	}
	if run.Arm == ArmA && run.ColdEnsures == 0 {
		out = append(out, fail("%s: arm A made no cold-ensure calls", label))
	}
	if run.Arm == ArmB1 {
		for _, ch := range run.Churn {
			if ch.Outcome != "ok" || !ch.MountedOK || !ch.DisposedOK || ch.Late {
				out = append(out, fail("%s: churn arrival %d outcome=%s mounted=%v disposed=%v late=%v %s (a missed churn arrival, overflow or backlog fails B1)", label, ch.Seq, ch.Outcome, ch.MountedOK, ch.DisposedOK, ch.Late, ch.Error))
				break
			}
		}
	} else {
		for _, ch := range run.Churn {
			if ch.MountedOK || ch.Outcome != "idle(B0)" {
				out = append(out, fail("%s: the scheduler without mounts started work (%s)", label, ch.Outcome))
				break
			}
		}
	}
	if run.Outstanding != 0 {
		out = append(out, fail("%s: %d outstanding after the drain", label, run.Outstanding))
	}
	return out
}

func ackLimit(body BodySize) time.Duration {
	if body.Items >= coldMiBItems {
		return AckP99MiB
	}
	return AckP99Items
}

// EvaluateCold applies section 9 and section 4 to a complete set of runs. Missing
// cells, runs, arrivals or fixtures are inconclusive; nothing missing can pass.
func EvaluateCold(plan ColdPlan, backends []string, runs []ColdRun, timeouts []ColdTimeoutResult) ([]Finding, map[ColdKey]*ColdCell) {
	var out []Finding
	cells := Cells(runs)
	byRun := map[ColdKey]map[int]ColdRun{}
	for _, run := range runs {
		key := ColdKey{run.Backend, run.Body.Name, run.Arm}
		if byRun[key] == nil {
			byRun[key] = map[int]ColdRun{}
		}
		byRun[key][run.Repetition] = run
		out = append(out, runFindings(plan, run)...)
	}
	for _, backend := range backends {
		for _, body := range plan.Bodies {
			for _, arm := range []ColdArm{ArmA, ArmB0, ArmB1} {
				key := ColdKey{backend, body.Name, arm}
				c := cells[key]
				if c == nil || c.Runs == 0 {
					out = append(out, inconclusive("%s: no runs", key))
					continue
				}
				if c.Runs < plan.Repetitions || (!plan.Dry && c.Runs < ColdMinimumReps) {
					out = append(out, inconclusive("%s: %d repetitions, floor is %d", key, c.Runs, plan.Repetitions))
				}
				if want := c.Runs * plan.MeasuredRequests(); c.Scheduled != want {
					out = append(out, inconclusive("%s: %d measured arrivals, %d scheduled", key, c.Scheduled, want))
				}
				if c.SuccessRatio < SuccessFloor {
					out = append(out, fail("%s: success %.4f below %.3f", key, c.SuccessRatio, SuccessFloor))
				}
				if f := cvFinding(key.String(), c.Throughput); f != nil {
					out = append(out, *f)
				}
				if arm == ArmB0 || arm == ArmB1 {
					limit := ackLimit(body)
					if c.Aggregate.P99 > limit.Nanoseconds() {
						out = append(out, fail("%s: p99 ack %s > %s", key, time.Duration(c.Aggregate.P99), limit))
					}
					for rep, d := range c.PerRun {
						if d.P99 > limit.Nanoseconds() {
							out = append(out, fail("%s: run %d p99 ack %s > %s", key, rep, time.Duration(d.P99), limit))
						}
					}
				}
			}
			b0, b1 := cells[ColdKey{backend, body.Name, ArmB0}], cells[ColdKey{backend, body.Name, ArmB1}]
			if b0 != nil && b1 != nil {
				if d := b1.Aggregate.P99 - b0.Aggregate.P99; d > ChurnDeltaLimit.Nanoseconds() {
					out = append(out, fail("%s/%s: aggregate B1 p99 exceeds B0 p99 by %s > %s", backend, body.Name, time.Duration(d), ChurnDeltaLimit))
				}
				for rep, r0 := range byRun[ColdKey{backend, body.Name, ArmB0}] {
					r1, ok := byRun[ColdKey{backend, body.Name, ArmB1}][rep]
					if !ok {
						out = append(out, inconclusive("%s/%s rep%d: no matched B1 run for B0", backend, body.Name, rep))
						continue
					}
					l0, _, _, _ := measured(r0)
					l1, _, _, _ := measured(r1)
					p0, p1 := Summarize(l0).P99, Summarize(l1).P99
					if d := p1 - p0; d > ChurnDeltaLimit.Nanoseconds() {
						out = append(out, fail("%s/%s rep%d: matched pair B1 p99 exceeds B0 p99 by %s > %s", backend, body.Name, rep, time.Duration(d), ChurnDeltaLimit))
					}
				}
				for rep := range byRun[ColdKey{backend, body.Name, ArmB1}] {
					if _, ok := byRun[ColdKey{backend, body.Name, ArmB0}][rep]; !ok {
						out = append(out, inconclusive("%s/%s rep%d: no matched B0 run for B1", backend, body.Name, rep))
					}
				}
			}
		}
		found := false
		for _, t := range timeouts {
			if t.Backend == backend {
				found = true
				out = append(out, t.Findings()...)
			}
		}
		if !found {
			out = append(out, inconclusive("%s: cold-timeout fixture not run", backend))
		}
	}
	var deltas []HostDelta
	for _, run := range runs {
		deltas = append(deltas, run.Host)
	}
	out = append(out, noiseFindings(deltas)...)
	return out, cells
}
