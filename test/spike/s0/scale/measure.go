// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"encoding/hex"
	"fmt"
	"runtime"
	"slices"
	"time"
)

// Identity and host/noise evidence are parent-owned. This package never invents
// a freeze/hash, claims missing cgroup data means unlimited, or authorizes itself
// to execute a scale run. Exact commands/exit codes belong to the outer driver.
type Identity struct {
	CriteriaSHA256   string   `json:"criteriaSHA256"`
	SourceSHA256     string   `json:"sourceSHA256"`
	ExecutableSHA256 string   `json:"executableSHA256"`
	BaselineCommit   string   `json:"baselineCommit"`
	Commands         []string `json:"commands"`
	FinalParity      bool     `json:"finalParityCompleted"`
	HostEvidence     string   `json:"hostEvidence"`
	ObservedThrottle bool     `json:"observedThrottle"`
	ObservedSwap     bool     `json:"observedSwap"`
}

type Plan struct {
	Repetitions int `json:"repetitions"`
	GraphTrials int `json:"graphTrials"`
	TreeTrials  int `json:"treeTrials"`
}

func validDigest(value string) bool {
	bytes, err := hex.DecodeString(value)
	return err == nil && len(bytes) == 32
}

func validatePlan(plan Plan) error {
	if plan.Repetitions < MinimumRepetitions || plan.GraphTrials <= 0 || plan.TreeTrials <= 0 ||
		int64(plan.GraphTrials)*int64(plan.Repetitions) < MinimumSamples ||
		int64(plan.TreeTrials)*MinimumRepetitions*int64(plan.Repetitions) < MinimumSamples {
		return fmt.Errorf("plan is below frozen independent repetition/sample floors")
	}
	return nil
}

func FrozenPlan() Plan {
	return Plan{Repetitions: MinimumRepetitions, GraphTrials: 100, TreeTrials: 10}
}

type OperationSummary struct {
	Backend      string                  `json:"backend"`
	Workload     string                  `json:"workload"`
	Operation    string                  `json:"operation"`
	Aggregate    Distribution            `json:"aggregate"`
	PerRun       map[string]Distribution `json:"perRun"`
	ThroughputCV float64                 `json:"throughputCV"`
}

type Report struct {
	Identity       Identity           `json:"identity"`
	Plan           Plan               `json:"plan"`
	StartedUTC     time.Time          `json:"startedUTC"`
	EndedUTC       time.Time          `json:"endedUTC"`
	TimerOverhead  Distribution       `json:"timerOverhead"`
	Summaries      []OperationSummary `json:"summaries"`
	Classification Classification     `json:"classification"`
	Reasons        []string           `json:"reasons"`
	Runs           int                `json:"runs"`
}

// Measure executes only when called by an explicitly authorized parent driver.
// consume MUST preserve a raw record before allowing the next run, typically
// paired JSON/CSV files. Results cannot accumulate runtime archives in this loop.
// Graphs collect 100 fresh roots * 10 reps = 1000 observations per operation;
// trees collect 10 roots * 10 cycles * 10 reps = 1000 shared restart observations.
// Candidate order alternates every repetition on exactly the same declarations.
func Measure(ctx context.Context, identity Identity, plan Plan, consume func(RunResult) error) (Report, error) {
	report := Report{Identity: identity, Plan: plan, StartedUTC: time.Now().UTC(), Classification: Inconclusive}
	if runtime.GOMAXPROCS(0) != 8 {
		return report, fmt.Errorf("frozen measurement requires GOMAXPROCS=8, got %d", runtime.GOMAXPROCS(0))
	}
	if !identity.FinalParity || !validDigest(identity.CriteriaSHA256) || !validDigest(identity.SourceSHA256) || !validDigest(identity.ExecutableSHA256) || identity.BaselineCommit == "" || len(identity.Commands) == 0 || identity.HostEvidence == "" || consume == nil {
		return report, fmt.Errorf("parent final parity, frozen SHA256 identities, baseline, command, host evidence and raw consumer required")
	}
	if err := validatePlan(plan); err != nil {
		return report, err
	}
	var timer []int64
	for i := 0; i < MinimumSamples; i++ {
		start := time.Now()
		timer = append(timer, time.Since(start).Nanoseconds())
	}
	report.TimerOverhead = Summarize(timer)
	type bucket struct {
		values []int64
		perRun map[string][]int64
		perRep map[int][]int64
	}
	buckets := make(map[string]*bucket)
	labels := make(map[string][3]string)
	failed, invalid := false, identity.ObservedSwap || identity.ObservedThrottle
	for rep := 0; rep < plan.Repetitions; rep++ {
		backends := Candidates()
		if rep%2 == 1 {
			slices.Reverse(backends)
		}
		for _, f := range FrozenFixtures() {
			trials := plan.GraphTrials
			if f.Topology == Trees {
				trials = plan.TreeTrials
			}
			for trial := 0; trial < trials; trial++ {
				for _, backend := range backends {
					if err := ctx.Err(); err != nil {
						return report, err
					}
					result := RunT(ctx, backend, f)
					result.Repetition, result.Trial = rep, trial
					if err := consume(result); err != nil {
						return report, err
					}
					report.Runs++
					if result.Classification == Fail {
						failed = true
						report.Reasons = append(report.Reasons, fmt.Sprintf("%s/%s rep%d trial%d: %v", backend.Name, f.Name, rep, trial, result.Errors))
					}
					baseline := result.EmptyRoot
					limit := int64(128 << 20)
					if result.SharedOnly != nil {
						baseline, limit = *result.SharedOnly, 32<<20
					}
					for _, retained := range result.Retained {
						if int64(retained.Memory.HeapAlloc)-int64(baseline.Memory.HeapAlloc) > limit {
							failed = true
							report.Reasons = append(report.Reasons, backend.Name+"/"+f.Name+": retained heap exceeds frozen limit (archives/history included)")
						}
						for owner, count := range retained.Goroutines.Counts {
							if owner != "external" && count > baseline.Goroutines.Counts[owner] {
								report.Reasons = append(report.Reasons, fmt.Sprintf("%s/%s: observed idle candidate owner %s = %d, baseline %d; raw-stack review required before an idle-goroutine conclusion", backend.Name, f.Name, owner, count, baseline.Goroutines.Counts[owner]))
							}
						}
					}
					for _, sample := range result.Samples {
						key := backend.Name + "/" + f.Name + "/" + sample.Operation
						b := buckets[key]
						if b == nil {
							b = &bucket{perRun: make(map[string][]int64), perRep: make(map[int][]int64)}
							buckets[key], labels[key] = b, [3]string{backend.Name, f.Name, sample.Operation}
						}
						b.values = append(b.values, sample.LatencyNS) // failed attempts stay in denominators
						run := fmt.Sprintf("rep-%02d/trial-%03d", rep, trial)
						b.perRun[run] = append(b.perRun[run], sample.LatencyNS)
						b.perRep[rep] = append(b.perRep[rep], sample.LatencyNS)
					}
				}
			}
		}
	}
	for _, key := range slices.Sorted(mapKeys(buckets)) {
		b, label := buckets[key], labels[key]
		summary := OperationSummary{Backend: label[0], Workload: label[1], Operation: label[2], Aggregate: Summarize(b.values), PerRun: make(map[string]Distribution)}
		for run, values := range b.perRun {
			summary.PerRun[run] = Summarize(values)
		}
		var rates []float64
		for rep := 0; rep < plan.Repetitions; rep++ {
			var total int64
			for _, value := range b.perRep[rep] {
				total += value
			}
			if total > 0 {
				rates = append(rates, float64(len(b.perRep[rep]))*1e9/float64(total))
			}
		}
		summary.ThroughputCV, _ = CV(rates)
		if summary.ThroughputCV > .10 {
			invalid = true
			report.Reasons = append(report.Reasons, key+": throughput CV >10%; repeat unchanged protocol")
		}
		if len(b.values) < MinimumSamples || len(b.perRep) < MinimumRepetitions {
			report.Reasons = append(report.Reasons, key+": insufficient observations/repetitions")
		}
		budgetMiss := (label[2] == "mount-settle" && summary.Aggregate.P99 > int64(2*time.Second)) ||
			(label[2] == "leaf-update-settle" && summary.Aggregate.P99 > int64(10*time.Millisecond)) ||
			((label[2] == "root-restart-settle" || label[2] == "shared-provider-restart-settle" || label[2] == "all-providers-update-loop-settle") && summary.Aggregate.Max > int64(5*time.Second)) ||
			(label[2] == "cold-tree-mount-settle" && summary.Aggregate.Max >= int64(50*time.Millisecond))
		if budgetMiss {
			failed = true
			report.Reasons = append(report.Reasons, key+": observed frozen latency limit exceeded")
		}
		report.Summaries = append(report.Summaries, summary)
	}
	// No source-owner backlog or native Batch can be proved by the public
	// prototype contract. Those are real gaps; a full §8 pass must remain withheld.
	report.Reasons = append(report.Reasons, "prototype has no native Batch or observable pending event backlog; §8 all-provider Batch acceptance not measured", "§8.3 call-path and §9 acknowledgement/churn are not executed by this graph/tree harness")
	if invalid {
		report.Classification = Invalid
	}
	if failed { // A candidate failure is never discarded as environmental noise.
		report.Classification = Fail
	}
	report.EndedUTC = time.Now().UTC()
	return report, nil
}
