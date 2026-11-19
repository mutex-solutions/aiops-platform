// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestColdMountRun is the frozen section 9 run. It SKIPS without S0_COLD_RUN=1;
// a skip is not a pass. A full run FAILS unless the classification is pass.
func TestColdMountRun(t *testing.T) {
	if os.Getenv("S0_COLD_RUN") != "1" {
		t.Skip("S0 cold mount NOT RUN: set S0_COLD_RUN=1 S0_COLD_IDENTITY=<json> S0_COLD_OUT=<fresh dir> on an idle host")
	}
	if os.Getenv("S0_COLD_DRYRUN") == "1" {
		t.Fatal("S0_COLD_RUN and S0_COLD_DRYRUN are mutually exclusive")
	}
	runCold(t, FrozenColdPlan())
}

// TestDryRunColdMount runs the whole pipeline at 2 repetitions of 3.5 s. Labelled
// NOT ACCEPTANCE; the classification can never be pass.
func TestDryRunColdMount(t *testing.T) {
	if os.Getenv("S0_COLD_DRYRUN") != "1" {
		t.Skip("dry run NOT RUN: set S0_COLD_DRYRUN=1 S0_COLD_OUT=<fresh dir>")
	}
	runCold(t, DryColdPlan())
}

func runCold(t *testing.T, plan ColdPlan) {
	t.Helper()
	var supplied *Identity
	if path := os.Getenv("S0_COLD_IDENTITY"); path != "" {
		id, err := LoadIdentity(path)
		if err != nil {
			t.Fatal(err)
		}
		supplied = &id
	} else if !plan.Dry {
		t.Fatal("S0_COLD_IDENTITY (parent identity JSON) required")
	}
	writer, err := NewEvidenceWriter(os.Getenv("S0_COLD_OUT")) // refuses an existing directory.
	if err != nil {
		t.Fatal(err)
	}
	rec, closure := CollectIdentity(repoRoot, "cold-mount", plan, plan.Frozen(), plan.Dry, supplied)
	for name, v := range map[string]any{"identity.json": rec, "source-closure.json": closure} {
		if err := writer.WriteJSON(name, v, false); err != nil {
			t.Fatal(err)
		}
	}
	ordinal := 0
	report, runErr := MeasureColdMount(t.Context(), rec, plan, func(run ColdRun) error {
		ordinal++
		name := fmt.Sprintf("%04d-%s-%s-%s-r%02d.json.gz", ordinal, run.Backend, run.Body.Name, run.Arm, run.Repetition)
		return writer.WriteJSON(name, run, true)
	})
	if runErr != nil {
		report.Add(inconclusive("driver: %v", runErr))
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
		t.Fatalf("cold-mount classification %s (a dry run, failure, noise or missing evidence is never a pass)", report.Classification)
	}
}
