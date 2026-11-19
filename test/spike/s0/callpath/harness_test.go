// SPDX-License-Identifier: Apache-2.0

package callpath

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFrozenProtocol(t *testing.T) {
	if Repetitions*SamplesPerRun < 1_000_000 || ScheduledUpdates != UpdatesPerSecond*int(StressDuration.Seconds()) || Leaves != 100 || OutstandingLimit != 8 {
		t.Fatal("frozen callpath protocol changed")
	}
}

func envIdentity(t *testing.T) *Identity {
	t.Helper()
	path := os.Getenv("S0_CALLPATH_IDENTITY")
	if path == "" {
		t.Log("S0_CALLPATH_IDENTITY not set: the run will be inconclusive")
		return nil
	}
	id, err := LoadIdentity(path)
	if err != nil {
		t.Fatalf("identity file: %v", err)
	}
	return &id
}

// TestFullCallpath runs the complete frozen §4/§8.3 protocol. It skips (NOT
// measured) unless S0_CALLPATH_RUN=1, and then FAILS unless the classification
// is pass: a missed threshold, a correctness failure, noise, or missing
// identity/noise/raw evidence is never reported as success.
func TestFullCallpath(t *testing.T) {
	if os.Getenv("S0_CALLPATH_RUN") != "1" {
		t.Skip("NOT MEASURED: the full callpath protocol was not run; set S0_CALLPATH_RUN=1 (see README)")
	}
	if os.Getenv("S0_CALLPATH_DRYRUN") == "1" {
		t.Fatal("S0_CALLPATH_DRYRUN and S0_CALLPATH_RUN are mutually exclusive: a dry run is never acceptance")
	}
	out := os.Getenv("S0_CALLPATH_OUT")
	if out == "" {
		t.Fatal("fresh S0_CALLPATH_OUT directory required")
	}
	if deadline, ok := t.Deadline(); ok && time.Until(deadline) < 3*time.Hour {
		t.Fatalf("test deadline %s is shorter than the ~2.2h protocol; pass -timeout 4h", time.Until(deadline).Round(time.Second))
	}
	report, err := Run(t.Context(), Config{Plan: FrozenPlan(), Out: out, Identity: envIdentity(t), Root: repoRoot, Progress: func(s string) { t.Log(s) }})
	if err != nil {
		t.Fatalf("callpath run: %v", err)
	}
	if report.Classification != "pass" {
		t.Fatalf("callpath classification %s (raw records in the output directory):\n%s", report.Classification, strings.Join(report.Reasons, "\n"))
	}
	for _, r := range report.Reasons { // Observations (candidateVariation) accompany a pass.
		t.Log(r)
	}
	t.Logf("callpath classification pass; candidate evidence only; raw records in %s", out)
}

// TestDryRunCallpath exercises the whole pipeline with reduced counts. It is for
// development only: its output is labelled NOT ACCEPTANCE and its classification
// can never be pass. It fails only if the pipeline itself is broken.
func TestDryRunCallpath(t *testing.T) {
	if os.Getenv("S0_CALLPATH_DRYRUN") != "1" {
		t.Skip("dry run not requested: set S0_CALLPATH_DRYRUN=1 (development only, NOT acceptance)")
	}
	if os.Getenv("S0_CALLPATH_RUN") == "1" {
		t.Fatal("refusing to combine a dry run with S0_CALLPATH_RUN")
	}
	out := os.Getenv("S0_CALLPATH_OUT")
	if out == "" {
		t.Fatal("fresh S0_CALLPATH_OUT directory required")
	}
	report, err := Run(context.Background(), Config{Plan: DryPlan(), Dry: true, Out: out, Identity: envIdentity(t), Root: repoRoot, Progress: func(s string) { t.Log(s) }})
	if err != nil {
		t.Fatalf("dry run pipeline: %v", err)
	}
	if report.Classification == "pass" || report.Acceptance {
		t.Fatal("a dry run must never classify as pass or acceptance")
	}
	t.Logf("%s; classification %s", report.Label, report.Classification)
	for _, r := range report.Reasons {
		t.Log(r)
	}
}
