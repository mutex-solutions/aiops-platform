// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

// TestDefaultMeasure is the frozen scale run (section 8.1 and 8.2). It SKIPS
// without S0_SCALE_RUN=1; a skip is not a pass. A full run FAILS unless the
// classification is pass: missed thresholds, correctness failures, noise and
// missing identity/noise/raw evidence are all non-pass.
func TestDefaultMeasure(t *testing.T) {
	if os.Getenv("S0_SCALE_RUN") != "1" {
		t.Skip("S0 scale NOT RUN: set S0_SCALE_RUN=1 S0_SCALE_IDENTITY=<json> S0_SCALE_OUT=<fresh dir> on an idle host")
	}
	if os.Getenv("S0_SCALE_DRYRUN") == "1" {
		t.Fatal("S0_SCALE_RUN and S0_SCALE_DRYRUN are mutually exclusive")
	}
	runScale(t, FrozenPlan())
}

// TestDryRunScale exercises the whole pipeline (identity, raw records, floors,
// classification, manifest) at a reduced size. Labelled NOT ACCEPTANCE; the
// classification can never be pass.
func TestDryRunScale(t *testing.T) {
	if os.Getenv("S0_SCALE_DRYRUN") != "1" {
		t.Skip("dry run NOT RUN: set S0_SCALE_DRYRUN=1 S0_SCALE_OUT=<fresh dir>")
	}
	if os.Getenv("S0_SCALE_DRYSIZE") == "full" {
		runScale(t, DryFullSizePlan()) // frozen fixture sizes, one root per cell, still NOT acceptance.
		return
	}
	runScale(t, DryPlan())
}

type runFiles struct {
	writer *EvidenceWriter
	trace  string
	runs   int
}

// traceRetention is the policy for the heavy raw trace (every event and every
// archived fiber of a root). "all" is the default and the only policy that can
// pass; "first-and-failed" keeps the first trial of each case and every failure
// and makes the run inconclusive.
func traceRetention() string {
	if v := os.Getenv("S0_SCALE_TRACE"); v != "" {
		return v
	}
	return "all"
}

func runScale(t *testing.T, plan Plan) {
	t.Helper()
	out := os.Getenv("S0_SCALE_OUT")
	var supplied *Identity
	if path := os.Getenv("S0_SCALE_IDENTITY"); path != "" {
		id, err := LoadIdentity(path)
		if err != nil {
			t.Fatal(err)
		}
		supplied = &id
	} else if !plan.Dry {
		t.Fatal("S0_SCALE_IDENTITY (parent identity JSON) required")
	}
	if policy := traceRetention(); policy != "all" && policy != "first-and-failed" {
		t.Fatalf("S0_SCALE_TRACE must be all or first-and-failed, got %q", policy)
	}
	writer, err := NewEvidenceWriter(out) // refuses an existing directory.
	if err != nil {
		t.Fatal(err)
	}
	rec, closure := CollectIdentity(repoRoot, "scale", plan, plan.Frozen(), plan.Dry, supplied)
	for name, v := range map[string]any{"identity.json": rec, "source-closure.json": closure} {
		if err := writer.WriteJSON(name, v, false); err != nil {
			t.Fatal(err)
		}
	}
	files := &runFiles{writer: writer}
	report, runErr := Measure(t.Context(), rec, plan, files.consume)
	if runErr != nil {
		report.Add(inconclusive("driver: %v", runErr))
	}
	if traceRetention() != "all" {
		report.Add(inconclusive("raw trace retention reduced (%s): not every root's events and archives are kept", traceRetention()))
	}
	if problems := writer.Verify(); len(problems) != 0 {
		report.Add(inconclusive("raw record verification: %s", strings.Join(problems, "; ")))
	}
	if err := writer.WriteJSON("report.json", report, false); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteJSON("manifest.json", writer.Manifest(), false); err != nil {
		t.Fatal(err)
	}
	t.Logf("classification=%s dry=%v acceptanceEligible=%v runs=%d reasons=%d", report.Classification, report.Dry, report.Acceptance, report.Runs, len(report.Reasons))
	for i, reason := range report.Reasons {
		if i < 25 {
			t.Log(reason)
		}
	}
	if runErr != nil || report.Classification != Pass {
		t.Fatalf("scale classification %s (a dry run, failure, noise or missing evidence is never a pass)", report.Classification)
	}
}

// consume writes one root's raw record exclusively before the next root starts.
func (f *runFiles) consume(run RunRecord) error {
	r := run.Result
	stem := fmt.Sprintf("%06d-%s-%s-r%02d-t%03d", run.Ordinal, r.Backend, r.Workload.Name, r.Repetition, r.Trial)
	trace := struct {
		Events          any `json:"events"`
		FinalFibers     any `json:"finalFibers"`
		FinalProvisions any `json:"finalProvisions"`
	}{r.Events, r.FinalFibers, r.FinalProvisions}
	keep := traceRetention() == "all" || r.Repetition+r.Trial == 0 || r.Classification == Fail
	light := struct {
		RunRecord
		TraceFile string `json:"traceFile"`
	}{RunRecord: run}
	light.Result.Events, light.Result.FinalFibers, light.Result.FinalProvisions = nil, nil, nil
	if keep {
		light.TraceFile = stem + ".trace.json.gz"
		if err := f.writer.WriteJSON(light.TraceFile, trace, true); err != nil {
			return err
		}
	}
	if err := f.writer.WriteJSON(stem+".json.gz", light, true); err != nil {
		return err
	}
	f.runs++
	return f.writer.WriteCSVFile(stem+".csv", func(w io.Writer) error { return WriteCSV(w, []RunResult{r}) })
}
