// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// ColdPair is one matched B0/B1 repetition: the delta that section 9 holds to 5 ms.
type ColdPair struct {
	Backend string `json:"backend"`
	Body    string `json:"body"`
	Rep     int    `json:"repetition"`
	B0P99   int64  `json:"b0P99NS"`
	B1P99   int64  `json:"b1P99NS"`
	Delta   int64  `json:"deltaNS"`
}

type ColdReport struct {
	Identity       IdentityRecord      `json:"identity"`
	Plan           ColdPlan            `json:"plan"`
	Dry            bool                `json:"dry"`
	Acceptance     bool                `json:"acceptanceEligible"`
	StartedUTC     time.Time           `json:"startedUTC"`
	EndedUTC       time.Time           `json:"endedUTC"`
	Timeouts       []ColdTimeoutResult `json:"coldTimeoutFixtures"`
	Cells          []ColdCell          `json:"cells"`
	Pairs          []ColdPair          `json:"matchedPairs"`
	Classification Classification      `json:"classification"`
	Reasons        []string            `json:"reasons"`
	Findings       []Finding           `json:"-"`
	Runs           int                 `json:"runs"`
}

// Add appends findings and re-resolves the classification.
func (r *ColdReport) Add(findings ...Finding) {
	r.Findings = append(r.Findings, findings...)
	r.Classification, r.Reasons = Resolve(r.Findings, r.Dry)
}

// MeasureColdMount runs the section 9 protocol for an explicitly authorised
// driver. consume MUST preserve each run's raw record before the next run starts.
// Per body size and repetition the candidate order alternates, and so does the
// B0/B1 order; arm A follows the pair on even repetitions and precedes it on odd
// ones. Only the frozen plan can be acceptance-eligible.
func MeasureColdMount(ctx context.Context, rec IdentityRecord, plan ColdPlan, consume func(ColdRun) error) (ColdReport, error) {
	report := ColdReport{Identity: rec, Plan: plan, Dry: plan.Dry, Acceptance: rec.Acceptance && plan.Frozen(), StartedUTC: time.Now().UTC()}
	report.Add(rec.Findings()...)
	finish := func(err error) (ColdReport, error) {
		report.EndedUTC = time.Now().UTC()
		return report, err
	}
	if consume == nil {
		report.Add(inconclusive("no raw-record consumer"))
		return finish(errors.New("raw consumer required"))
	}
	if err := plan.validate(); err != nil {
		return finish(err)
	}
	if !plan.Dry {
		if !plan.Frozen() {
			return finish(errors.New("a non-frozen plan must be labelled Dry; refusing"))
		}
		if len(rec.Invalid) != 0 || len(rec.Problems) != 0 {
			return finish(fmt.Errorf("refusing to start: identity/environment findings %v %v", rec.Invalid, rec.Problems))
		}
	}
	var names []string
	candidates, unsupported := Runnable(Candidates())
	report.Add(UnsupportedFindings(unsupported)...)
	for _, b := range candidates {
		names = append(names, b.Name)
		report.Timeouts = append(report.Timeouts, RunColdTimeoutFixture(ctx, b, plan))
	}
	var runs []ColdRun
	for _, body := range plan.Bodies {
		for rep := 0; rep < plan.Repetitions; rep++ {
			backends := slices.Clone(candidates)
			arms := []ColdArm{ArmB0, ArmB1, ArmA}
			if rep%2 == 1 {
				slices.Reverse(backends)
				arms = []ColdArm{ArmA, ArmB1, ArmB0}
			}
			for _, backend := range backends {
				for _, arm := range arms {
					if err := ctx.Err(); err != nil {
						report.Add(inconclusive("interrupted: %v", err))
						return finish(err)
					}
					run := RunColdArm(ctx, backend, plan, body, arm, rep)
					report.Runs++
					if err := consume(run); err != nil {
						report.Add(inconclusive("raw record not preserved: %v", err))
						return finish(err)
					}
					runs = append(runs, run)
				}
			}
		}
	}
	findings, cells := EvaluateCold(plan, names, runs, report.Timeouts)
	report.Add(findings...)
	for _, key := range slices.SortedFunc(mapKeysCold(cells), func(a, b ColdKey) int { return strings.Compare(a.String(), b.String()) }) {
		report.Cells = append(report.Cells, *cells[key])
	}
	type pairKey struct {
		backend, body string
		rep           int
	}
	b0, b1 := map[pairKey]int64{}, map[pairKey]int64{}
	for _, run := range runs {
		lat, _, _, _ := measured(run)
		k := pairKey{run.Backend, run.Body.Name, run.Repetition}
		switch run.Arm {
		case ArmB0:
			b0[k] = Summarize(lat).P99
		case ArmB1:
			b1[k] = Summarize(lat).P99
		}
	}
	for k, v0 := range b0 {
		if v1, ok := b1[k]; ok {
			report.Pairs = append(report.Pairs, ColdPair{Backend: k.backend, Body: k.body, Rep: k.rep, B0P99: v0, B1P99: v1, Delta: v1 - v0})
		}
	}
	slices.SortFunc(report.Pairs, func(a, b ColdPair) int {
		if c := strings.Compare(a.Backend+"/"+a.Body, b.Backend+"/"+b.Body); c != 0 {
			return c
		}
		return a.Rep - b.Rep
	})
	if rec.SourceSHA256 != "" {
		if now, err := sourceClosure(repoRoot); err != nil || now.SHA256 != rec.SourceSHA256 {
			report.Add(invalid("source closure changed during the run (or is unreadable): was %s", rec.SourceSHA256))
		}
	}
	report.Add()
	return finish(nil)
}

func mapKeysCold(m map[ColdKey]*ColdCell) func(func(ColdKey) bool) {
	return func(yield func(ColdKey) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}
