// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestDefaultMeasure deliberately SKIPS without the explicit parent authorization.
// A skip is not a scale pass. The parent must freeze the final source, finish
// parity, record host evidence and manage execution without competing full tests.
func TestDefaultMeasure(t *testing.T) { DefaultMeasure(t) }

func DefaultMeasure(t *testing.T) {
	t.Helper()
	if os.Getenv("S0_SCALE_RUN") != "1" {
		t.Skip("S0 scale performance WITHHELD: require S0_SCALE_RUN=1 after parent final parity/source-hash freeze")
	}
	path, out := os.Getenv("S0_SCALE_IDENTITY"), os.Getenv("S0_SCALE_OUT")
	if path == "" || out == "" {
		t.Fatal("parent-owned S0_SCALE_IDENTITY JSON and fresh S0_SCALE_OUT directory required")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var identity Identity
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&identity)
	err = errors.Join(err, file.Close())
	if err != nil {
		t.Fatal(err)
	}
	// Exclusive creation prevents accidental replacement/merging of evidence.
	// This only writes beneath the parent-provided fresh output directory.
	if err := os.Mkdir(out, 0o755); err != nil {
		t.Fatal(err)
	}
	ordinal := 0
	report, runErr := Measure(t.Context(), identity, FrozenPlan(), func(result RunResult) error {
		ordinal++
		stem := fmt.Sprintf("%06d-%s-%s-r%02d-t%03d", ordinal, result.Backend, result.Workload.Name, result.Repetition, result.Trial)
		jsonFile, err := os.OpenFile(filepath.Join(out, stem+".json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		if err := errors.Join(WriteJSON(jsonFile, result), jsonFile.Close()); err != nil {
			return err
		}
		csvFile, err := os.OpenFile(filepath.Join(out, stem+".csv"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		return errors.Join(WriteCSV(csvFile, []RunResult{result}), csvFile.Close())
	})
	if runErr != nil {
		report.Reasons = append(report.Reasons, "driver: "+runErr.Error())
	}
	summary, err := os.OpenFile(filepath.Join(out, "report.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(WriteJSON(summary, report), summary.Close()); err != nil {
		t.Fatal(err)
	}
	if runErr != nil || report.Classification != Pass {
		t.Fatalf("scale classification %s (not an acceptance pass); raw records: %s; driver error: %v", report.Classification, out, runErr)
	}
}

func TestMeasurementPreconditionsNoRuntime(t *testing.T) {
	// Every call returns before creating a runtime; this is NOT a benchmark run.
	if _, err := Measure(context.Background(), Identity{}, FrozenPlan(), nil); err == nil {
		t.Fatal("missing parent authority accepted")
	}
	plan := FrozenPlan()
	if plan.Repetitions != 10 || plan.GraphTrials*plan.Repetitions != 1000 || plan.TreeTrials*10*plan.Repetitions != 1000 || validatePlan(plan) != nil {
		t.Fatalf("wrong observation floors: %+v", plan)
	}
	for _, bad := range []Plan{{Repetitions: 9, GraphTrials: 1000, TreeTrials: 100}, {Repetitions: 10, GraphTrials: 99, TreeTrials: 10}, {Repetitions: 10, GraphTrials: 100, TreeTrials: 9}} {
		if validatePlan(bad) == nil {
			t.Fatalf("insufficient plan accepted: %+v", bad)
		}
	}
	if validDigest("missing") || !validDigest("628bd6ec24e5983bfbe9fcdada096eaa84db3e95227f11e8fd5a7fa33a26f19c") {
		t.Fatal("SHA256 identity validation failed")
	}
}
