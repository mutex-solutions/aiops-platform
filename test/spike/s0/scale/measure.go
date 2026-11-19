// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

// RunRecord is what the driver hands the raw-record consumer for every closed
// root, before the next root starts: the full result, the host noise samples
// taken around it, and the retained-heap metrics derived from its checkpoints.
type RunRecord struct {
	Ordinal    int             `json:"ordinal"`
	Result     RunResult       `json:"result"`
	HostBefore HostSample      `json:"hostBefore"`
	HostAfter  HostSample      `json:"hostAfter"`
	Host       HostDelta       `json:"hostDelta"`
	Retained   RetainedMetrics `json:"retainedHeap"`
}

// RetainedMetrics is the section 8.1/8.2 retained-heap evidence of one root:
// post-GC HeapAlloc (observers drained first, runtime archives and event history
// included) at every retained checkpoint minus the named baseline's HeapAlloc.
type RetainedMetrics struct {
	Baseline        string  `json:"baseline"` // "empty-root" (graphs) or "shared-only" (trees)
	BaselineHeap    uint64  `json:"baselineHeapAlloc"`
	DeltaBytes      []int64 `json:"deltaBytes"`
	FirstDeltaBytes int64   `json:"firstCheckpointDeltaBytes"` // before any churn history accumulates.
	MaxDeltaBytes   int64   `json:"maxDeltaBytes"`
	Fibers          int     `json:"retainedFibers"`
	PerFiberBytes   float64 `json:"maxPerFiberBytes"`
	SlopeBytesPerCP float64 `json:"slopeBytesPerCheckpoint"`
	StackInuseDelta int64   `json:"maxStackInuseDeltaBytes"`
	Goroutines      int     `json:"goroutinesAtLastCheckpoint"`
	LimitBytes      int64   `json:"limitBytes"`
}

// slope is the least-squares slope of values against their index.
func slope(values []int64) float64 {
	n := float64(len(values))
	if n < 2 {
		return 0
	}
	var sx, sy, sxx, sxy float64
	for i, v := range values {
		x, y := float64(i), float64(v)
		sx, sy, sxx, sxy = sx+x, sy+y, sxx+x*x, sxy+x*y
	}
	return (n*sxy - sx*sy) / (n*sxx - sx*sx)
}

// retainedMetrics derives the retained-heap evidence of one result. Trees are
// measured against their shared-only baseline with 32 MiB; graphs against the
// empty root with 128 MiB. An empty Retained list yields no deltas (and so no
// evidence, which the classifier reports as inconclusive, never as a pass).
func retainedMetrics(result RunResult) RetainedMetrics {
	m := RetainedMetrics{Baseline: "empty-root", BaselineHeap: result.EmptyRoot.Memory.HeapAlloc, LimitBytes: GraphHeapLimit, Fibers: result.Workload.Fibers}
	base := result.EmptyRoot
	if result.SharedOnly != nil {
		base = *result.SharedOnly
		m.Baseline, m.BaselineHeap, m.LimitBytes = "shared-only", base.Memory.HeapAlloc, TreeHeapLimit
		m.Fibers = result.Workload.Trees * result.Workload.TreeWidth
	}
	for i, c := range result.Retained {
		delta := int64(c.Memory.HeapAlloc) - int64(base.Memory.HeapAlloc)
		m.DeltaBytes = append(m.DeltaBytes, delta)
		if i == 0 {
			m.FirstDeltaBytes = delta
		}
		if i == 0 || delta > m.MaxDeltaBytes {
			m.MaxDeltaBytes = delta
		}
		if stack := int64(c.Memory.StackInuse) - int64(base.Memory.StackInuse); stack > m.StackInuseDelta {
			m.StackInuseDelta = stack
		}
		m.Goroutines = len(c.Goroutines.Stacks)
	}
	if m.Fibers > 0 {
		m.PerFiberBytes = float64(m.MaxDeltaBytes) / float64(m.Fibers)
	}
	m.SlopeBytesPerCP = slope(m.DeltaBytes)
	return m
}

type OperationSummary struct {
	Backend      string                  `json:"backend"`
	Workload     string                  `json:"workload"`
	Operation    string                  `json:"operation"`
	Observations int                     `json:"observations"`
	Repetitions  int                     `json:"repetitions"`
	Scheduled    int                     `json:"scheduledObservations"`
	Aggregate    Distribution            `json:"aggregate"`
	PerRun       map[string]Distribution `json:"perRun"`
	ThroughputCV float64                 `json:"throughputCV"`
}

// HeapSummary aggregates the per-root retained metrics of one backend/fixture.
type HeapSummary struct {
	Backend       string  `json:"backend"`
	Workload      string  `json:"workload"`
	Baseline      string  `json:"baseline"`
	Roots         int     `json:"roots"`
	MedianBytes   int64   `json:"medianDeltaBytes"`
	MaxFirstBytes int64   `json:"maxFirstCheckpointDeltaBytes"`
	MaxBytes      int64   `json:"maxDeltaBytes"`
	MaxPerFiber   float64 `json:"maxPerFiberBytes"`
	LimitBytes    int64   `json:"limitBytes"`
	MaxSlope      float64 `json:"maxSlopeBytesPerCheckpoint"`
	MaxStackDelta int64   `json:"maxStackInuseDeltaBytes"`
	Goroutines    int     `json:"maxGoroutinesAtLastCheckpoint"`
}

type Report struct {
	Identity       IdentityRecord     `json:"identity"`
	Plan           Plan               `json:"plan"`
	Dry            bool               `json:"dry"`
	Acceptance     bool               `json:"acceptanceEligible"`
	StartedUTC     time.Time          `json:"startedUTC"`
	EndedUTC       time.Time          `json:"endedUTC"`
	TimerOverhead  Distribution       `json:"timerOverhead"`
	Summaries      []OperationSummary `json:"summaries"`
	Heap           []HeapSummary      `json:"retainedHeap"`
	Host           []HostDelta        `json:"hostDeltas"`
	Classification Classification     `json:"classification"`
	Reasons        []string           `json:"reasons"`
	Findings       []Finding          `json:"-"`
	Runs           int                `json:"runs"`
}

// Add appends findings and re-resolves the classification, never upgrading it.
func (r *Report) Add(findings ...Finding) {
	r.Findings = append(r.Findings, findings...)
	r.Classification, r.Reasons = Resolve(r.Findings, r.Dry)
}

// Measure executes the scale protocol for an explicitly authorised driver.
// consume MUST preserve the raw record before the next run starts. Graphs
// collect GraphTrials fresh roots per repetition per operation; trees collect
// TreeTrials roots x TreeCycles cycles per repetition. Candidate order alternates
// every repetition on identical declarations. Only the frozen plan can be
// acceptance-eligible, and even then only an evaluation with no finding passes.
func Measure(ctx context.Context, rec IdentityRecord, plan Plan, consume func(RunRecord) error) (Report, error) {
	report := Report{Identity: rec, Plan: plan, Dry: plan.Dry, Acceptance: rec.Acceptance && plan.Frozen(), StartedUTC: time.Now().UTC()}
	report.Add(rec.Findings()...)
	finish := func(err error) (Report, error) {
		report.EndedUTC = time.Now().UTC()
		return report, err
	}
	if consume == nil {
		report.Add(inconclusive("no raw-record consumer"))
		return finish(errors.New("raw consumer required"))
	}
	if plan.Dry {
		if err := plan.validateStructure(); err != nil {
			return finish(err)
		}
	} else {
		if !plan.Frozen() {
			return finish(errors.New("a non-frozen plan must be labelled Dry; refusing"))
		}
		if err := validatePlan(plan); err != nil {
			return finish(err)
		}
		if len(rec.Invalid) != 0 || len(rec.Problems) != 0 {
			return finish(fmt.Errorf("refusing to start: identity/environment findings %v %v", rec.Invalid, rec.Problems))
		}
	}
	fixtures, err := plan.Fixtures()
	if err != nil {
		return finish(err)
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
	heaps := make(map[string][]RetainedMetrics)
	for rep := 0; rep < plan.Repetitions; rep++ {
		backends := Candidates()
		if rep%2 == 1 {
			slices.Reverse(backends)
		}
		for _, f := range fixtures {
			trials := plan.GraphTrials
			if f.Topology == Trees {
				trials = plan.TreeTrials
			}
			for trial := 0; trial < trials; trial++ {
				for _, backend := range backends {
					if err := ctx.Err(); err != nil {
						report.Add(inconclusive("interrupted: %v", err))
						return finish(err)
					}
					before := ReadHost()
					result := RunCycles(ctx, backend, f, plan.TreeCycles)
					after := ReadHost()
					result.Repetition, result.Trial = rep, trial
					report.Runs++
					run := RunRecord{Ordinal: report.Runs, Result: result, HostBefore: before, HostAfter: after,
						Host:     hostDelta(fmt.Sprintf("%s/%s/rep-%02d/trial-%03d", backend.Name, f.Name, rep, trial), before, after),
						Retained: retainedMetrics(result)}
					report.Host = append(report.Host, run.Host)
					if err := consume(run); err != nil {
						report.Add(inconclusive("raw record not preserved: %v", err))
						return finish(err)
					}
					label := backend.Name + "/" + f.Name
					if result.Classification == Fail {
						report.Add(fail("%s rep%d trial%d: %v", label, rep, trial, result.Errors))
					}
					for owner, count := range result.HarnessBaseline.Goroutines.Counts {
						if owner != "external" && count > 0 {
							report.Add(fail("%s rep%d trial%d: an earlier root leaked candidate goroutines (%s=%d); this and later timings are contaminated", label, rep, trial, owner, count))
						}
					}
					base := result.EmptyRoot
					if result.SharedOnly != nil {
						base = *result.SharedOnly
					}
					worst := 0
					for i, retained := range result.Retained {
						if retained.Memory.HeapAlloc > result.Retained[worst].Memory.HeapAlloc {
							worst = i
						}
						// Graphs: idle goroutines per fiber against the empty root. Trees:
						// goroutines that scale with trees against the shared-only cut.
						report.Add(idleGoroutineFindings(fmt.Sprintf("%s rep%d trial%d retained[%d]", label, rep, trial, i), base.Goroutines, retained.Goroutines)...)
					}
					// Every checkpoint is held to the limit (archives and event history
					// included); the worst one names the root and its position.
					if len(result.Retained) != 0 {
						if finding := heapFinding(fmt.Sprintf("%s rep%d trial%d checkpoint %d of %d (first checkpoint %d B)", label, rep, trial, worst, len(result.Retained), run.Retained.FirstDeltaBytes),
							base.Memory.HeapAlloc, result.Retained[worst].Memory.HeapAlloc, run.Retained.LimitBytes); finding != nil {
							report.Add(*finding)
						}
					}
					if len(result.Retained) == 0 {
						report.Add(inconclusive("%s rep%d trial%d: no retained-heap checkpoint", label, rep, trial))
					}
					heaps[label] = append(heaps[label], run.Retained)
					for _, sample := range result.Samples {
						key := label + "/" + sample.Operation
						b := buckets[key]
						if b == nil {
							b = &bucket{perRun: make(map[string][]int64), perRep: make(map[int][]int64)}
							buckets[key] = b
						}
						b.values = append(b.values, sample.LatencyNS) // failed attempts stay in denominators
						name := fmt.Sprintf("rep-%02d/trial-%03d", rep, trial)
						b.perRun[name] = append(b.perRun[name], sample.LatencyNS)
						b.perRep[rep] = append(b.perRep[rep], sample.LatencyNS)
					}
				}
			}
		}
	}
	for _, f := range fixtures {
		for _, backend := range Candidates() {
			for _, op := range operations(f) {
				key := backend.Name + "/" + f.Name + "/" + op
				b := buckets[key]
				if b == nil {
					report.Add(inconclusive("%s: no observations", key))
					continue
				}
				summary := OperationSummary{Backend: backend.Name, Workload: f.Name, Operation: op, Observations: len(b.values),
					Repetitions: len(b.perRep), Scheduled: plan.expectedObservations(f, op), Aggregate: Summarize(b.values), PerRun: make(map[string]Distribution)}
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
				if finding := cvFinding(key, rates); finding != nil {
					report.Add(*finding)
				}
				report.Add(floorFindings(key, summary.Observations, summary.Repetitions, summary.Scheduled, plan.Dry)...)
				if finding := budgetFinding(backend.Name+"/"+f.Name, op, summary.Aggregate); finding != nil {
					report.Add(*finding)
				}
				report.Summaries = append(report.Summaries, summary)
			}
			label := backend.Name + "/" + f.Name
			report.Heap = append(report.Heap, summarizeHeap(backend.Name, f.Name, heaps[label]))
		}
	}
	report.Add(noiseFindings(report.Host)...)
	// The linked source must not have changed while the run was in progress.
	if rec.SourceSHA256 != "" {
		if now, err := sourceClosure(repoRoot); err != nil || now.SHA256 != rec.SourceSHA256 {
			report.Add(invalid("source closure changed during the run (or is unreadable): was %s", rec.SourceSHA256))
		}
	}
	report.Add() // resolve even when nothing else was added
	return finish(nil)
}

func summarizeHeap(backend, workload string, metrics []RetainedMetrics) HeapSummary {
	s := HeapSummary{Backend: backend, Workload: workload, Roots: len(metrics)}
	var maxima []int64
	for _, m := range metrics {
		s.Baseline, s.LimitBytes = m.Baseline, m.LimitBytes
		maxima = append(maxima, m.MaxDeltaBytes)
		s.MaxBytes = max(s.MaxBytes, m.MaxDeltaBytes)
		s.MaxFirstBytes = max(s.MaxFirstBytes, m.FirstDeltaBytes)
		s.MaxPerFiber = max(s.MaxPerFiber, m.PerFiberBytes)
		s.MaxSlope = max(s.MaxSlope, m.SlopeBytesPerCP)
		s.MaxStackDelta = max(s.MaxStackDelta, m.StackInuseDelta)
		s.Goroutines = max(s.Goroutines, m.Goroutines)
	}
	if len(maxima) > 0 {
		s.MedianBytes, _ = NearestRank(maxima, .50)
	}
	return s
}
