// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const modulePrefix = "github.com/mutex-solutions/aiops-platform/"

// The source closure must name every module-local package the measured test
// binary links, otherwise a changed linked package would not change the hash.
func TestSourceClosureCoversEveryLinkedLocalPackage(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "-test", "-f", "{{.ImportPath}}", ".").Output()
	if err != nil {
		t.Skipf("go list unavailable: %v", err)
	}
	covered := map[string]bool{}
	for _, class := range append(slices.Clone(closureClasses), scaleClass) {
		covered[modulePrefix+class] = true
	}
	for _, pkg := range strings.Fields(string(out)) {
		pkg, _, _ = strings.Cut(pkg, " ") // "pkg [pkg.test]" variants
		if strings.HasPrefix(pkg, modulePrefix) && !covered[strings.TrimSuffix(strings.TrimSuffix(pkg, ".test"), "_test")] {
			t.Errorf("linked local package %s is not in the source closure", pkg)
		}
	}
	closure, err := sourceClosure(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, f := range closure.Files {
		have[f.Path] = true
		if strings.HasSuffix(f.Path, "_test.go") && !strings.HasPrefix(f.Path, scaleClass+"/") {
			t.Errorf("dependency test file %s is hashed although it is not linked", f.Path)
		}
	}
	for _, want := range []string{"go.mod", "docs/design/s0-spike-criteria.md", "docs/design/s0-criteria-freeze.json", scaleClass + "/README.md", scaleClass + "/coldmount.go", scaleClass + "/measure_test.go", "test/spike/s0/concurrency/graphlock.go"} {
		if !have[want] {
			t.Errorf("closure lacks %s", want)
		}
	}
}

func TestSourceClosureTracksEveryByte(t *testing.T) {
	root := t.TempDir()
	write := func(rel, text string) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, extra := range closureExtras {
		write(extra, extra)
	}
	for _, class := range append(slices.Clone(closureClasses), scaleClass) {
		write(class+"/a.go", "package a")
	}
	write(scaleClass+"/README.md", "readme")
	first, err := sourceClosure(root)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := sourceClosure(root)
	if first.SHA256 != again.SHA256 || !validDigest(first.SHA256) {
		t.Fatal("closure hash is not deterministic")
	}
	write("test/spike/s0/concurrency/a.go", "package a // changed")
	changed, _ := sourceClosure(root)
	if changed.SHA256 == first.SHA256 {
		t.Fatal("a changed dependency did not change the closure hash")
	}
	write("test/spike/s0/concurrency/a_test.go", "package a")
	withTest, _ := sourceClosure(root)
	if withTest.SHA256 != changed.SHA256 {
		t.Fatal("an unlinked dependency test file changed the hash")
	}
	write(scaleClass+"/b_test.go", "package scale")
	scaleTest, _ := sourceClosure(root)
	if scaleTest.SHA256 == withTest.SHA256 {
		t.Fatal("a scale test file (compiled into the binary) is not hashed")
	}
	if err := os.Remove(filepath.Join(root, "go.sum")); err != nil {
		t.Fatal(err)
	}
	if _, err := sourceClosure(root); err == nil {
		t.Fatal("a missing closure file was accepted")
	}
}

func TestCollectIdentityChecksAgainstTheFreezeReceipt(t *testing.T) {
	supplied := &Identity{BaselineCommit: "abc", Commands: []string{"cmd"}, FinalParity: true, HostEvidence: "recorded"}
	rec, _ := CollectIdentity(repoRoot, "scale", FrozenPlan(), true, false, supplied)
	if rec.CriteriaSHA256 == "" || rec.CriteriaSHA256 != rec.FreezeSHA256 {
		t.Fatalf("criteria %s vs freeze receipt %s", rec.CriteriaSHA256, rec.FreezeSHA256)
	}
	if !validDigest(rec.SourceSHA256) || !validDigest(rec.Executable) || rec.GoVersion == "" || rec.Affinity == "" {
		t.Fatalf("identity facts missing: %+v", rec)
	}
	for _, v := range rec.Args {
		if strings.Contains(v, "/") {
			t.Errorf("a path leaked into the recorded arguments: %q", v)
		}
	}
	// This ordinary test binary is a race or non-frozen-GOMAXPROCS build often
	// enough that protocol violations are expected; what matters is that each one
	// is recorded and none is silent.
	if raceEnabled && len(rec.Invalid) == 0 {
		t.Error("a race build was not flagged invalid")
	}
	missing, _ := CollectIdentity(repoRoot, "scale", FrozenPlan(), true, false, nil)
	if len(missing.Problems) == 0 {
		t.Error("missing parent identity was not a problem")
	}
	for _, bad := range []Identity{{Commands: []string{"c"}, FinalParity: true, HostEvidence: "h"}, {BaselineCommit: "b", FinalParity: true, HostEvidence: "h"},
		{BaselineCommit: "b", Commands: []string{"c"}, HostEvidence: "h"}, {BaselineCommit: "b", Commands: []string{"c"}, FinalParity: true}} {
		r, _ := CollectIdentity(repoRoot, "scale", FrozenPlan(), true, false, &bad)
		if len(r.Problems) == 0 {
			t.Errorf("incomplete identity accepted: %+v", bad)
		}
	}
	wrong := *supplied
	wrong.ExpectedSourceSHA256 = strings.Repeat("0", 64)
	if r, _ := CollectIdentity(repoRoot, "scale", FrozenPlan(), true, false, &wrong); len(r.Invalid) == 0 {
		t.Error("an expected digest that differs from the computed one was accepted")
	}
	dry, _ := CollectIdentity(repoRoot, "scale", DryPlan(), false, true, supplied)
	if dry.Acceptance || !strings.Contains(dry.Mode, "DRY RUN") {
		t.Errorf("dry identity is not labelled: %+v", dry.Mode)
	}
	if rec.Acceptance != true {
		t.Errorf("a frozen, non-dry plan is acceptance-eligible: %v", rec.Acceptance)
	}
	if got := rec.Findings(); len(rec.Problems)+len(rec.Invalid) != len(got) {
		t.Errorf("identity facts not turned into findings: %d vs %d", len(got), len(rec.Problems)+len(rec.Invalid))
	}
}

func TestLoadIdentityIsStrict(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.json")
	if err := os.WriteFile(good, []byte(`{"baselineCommit":"abc","commands":["c"],"finalParityCompleted":true,"hostEvidence":"h"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if id, err := LoadIdentity(good); err != nil || id.BaselineCommit != "abc" || !id.FinalParity {
		t.Fatalf("%+v %v", id, err)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"baselineCommit":"abc","observedSwap":false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadIdentity(bad); err == nil {
		t.Fatal("an unknown field (the retired parent-asserted noise flags) was accepted")
	}
	if _, err := LoadIdentity(filepath.Join(dir, "absent.json")); err == nil {
		t.Fatal("a missing identity file was accepted")
	}
}

func TestEvidenceWriterRefusesOverwrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out")
	w, err := NewEvidenceWriter(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewEvidenceWriter(dir); err == nil {
		t.Fatal("an existing output directory was reused")
	}
	if _, err := NewEvidenceWriter(""); err == nil {
		t.Fatal("empty output directory accepted")
	}
	if err := w.WriteJSON("a.json", map[string]int{"x": 1}, false); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteJSON("a.json", map[string]int{"x": 2}, false); err == nil {
		t.Fatal("an existing evidence file was overwritten")
	}
	for _, name := range []string{"../escape.json", "sub/x.json", "..", "."} {
		if err := w.WriteJSON(name, 1, false); err == nil {
			t.Errorf("evidence name %q escaped the directory", name)
		}
	}
	if err := w.WriteJSON("z.json.gz", []int{1, 2, 3}, true); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteCSVFile("r.csv", func(w2 io.Writer) error {
		_, err := w2.Write([]byte("a,b\n"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got := w.Verify(); len(got) != 0 {
		t.Fatalf("clean evidence reported as bad: %v", got)
	}
	if len(w.Manifest()) != 3 {
		t.Fatalf("manifest %v", w.Manifest())
	}
	want := fmt.Sprintf("%x", sha256.Sum256([]byte("{\"x\":1}\n")))
	if w.Manifest()[0].SHA256 != want || w.Manifest()[0].Bytes != 8 {
		t.Fatalf("manifest entry %+v want sha %s", w.Manifest()[0], want)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := w.Verify(); len(got) != 1 || !strings.Contains(got[0], "changed after writing") {
		t.Fatalf("tampering not detected: %v", got)
	}
	if err := os.Remove(filepath.Join(dir, "r.csv")); err != nil {
		t.Fatal(err)
	}
	if got := w.Verify(); len(got) != 2 {
		t.Fatalf("deleted record not detected: %v", got)
	}
}

func TestHostDeltaNeverReadsAMissingCounterAsZero(t *testing.T) {
	before := HostSample{SwapObserved: true, ThrottleSeen: true, SwapIn: 10, SwapOut: 5, Throttled: 3}
	after := before
	if d := hostDelta("r", before, after); d.SwapDelta != 0 || d.ThrottleDiff != 0 || !d.SwapKnown || !d.ThrottleKnow {
		t.Fatalf("quiet: %+v", d)
	}
	after.SwapOut, after.Throttled = 6, 4
	if d := hostDelta("r", before, after); d.SwapDelta != 1 || d.ThrottleDiff != 1 {
		t.Fatalf("moved: %+v", d)
	}
	after.SwapIn = 1 // a counter that went backwards must not wrap or read as zero
	if d := hostDelta("r", before, after); d.SwapDelta == 0 {
		t.Fatalf("a reset swap counter read as quiet: %+v", d)
	}
	missing := HostSample{}
	if d := hostDelta("r", missing, missing); d.SwapKnown || d.ThrottleKnow {
		t.Fatalf("unobserved counters reported as known: %+v", d)
	}
	if s := ReadHost(); s.UTC.IsZero() || s.Goroutines == 0 {
		t.Fatalf("host sample empty: %+v", s)
	}
}

func TestMeasureRefusals(t *testing.T) {
	ctx := context.Background()
	none := func(RunRecord) error { return nil }
	// No runtime is created by any of these: each returns before the first run.
	if _, err := Measure(ctx, IdentityRecord{}, FrozenPlan(), nil); err == nil {
		t.Error("missing raw consumer accepted")
	}
	r, err := Measure(ctx, IdentityRecord{Problems: []string{"x"}}, FrozenPlan(), none)
	if err == nil || r.Runs != 0 || r.Classification != Inconclusive {
		t.Errorf("frozen plan with identity problems must refuse before running: %v runs=%d %s", err, r.Runs, r.Classification)
	}
	r, err = Measure(ctx, IdentityRecord{Invalid: []string{"GOMAXPROCS=1"}}, FrozenPlan(), none)
	if err == nil || r.Runs != 0 || r.Classification != Invalid {
		t.Errorf("invalid environment must refuse before running: %v runs=%d %s", err, r.Runs, r.Classification)
	}
	if _, err := Measure(ctx, IdentityRecord{}, Plan{Dry: true}, none); err == nil {
		t.Error("an empty dry plan was run")
	}
}

// One tiny dry pipeline: real roots, every operation bucket present, never a pass.
func TestDryMeasureNeverPasses(t *testing.T) {
	plan := Plan{Dry: true, Repetitions: 2, GraphTrials: 1, TreeTrials: 1, TreeCycles: 1, GraphFibers: 100, TreeCounts: []int{3}}
	var records []RunRecord
	rec, _ := CollectIdentity(repoRoot, "scale", plan, false, true, nil)
	report, err := Measure(context.Background(), rec, plan, func(r RunRecord) error {
		records = append(records, r)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// 2 reps x every runnable candidate x (5 graphs + 2 tree widths).
	candidates, _ := Runnable(Candidates())
	n := len(candidates)
	if report.Runs != 14*n || len(records) != 14*n {
		t.Fatalf("runs = %d records = %d", report.Runs, len(records))
	}
	if !report.Dry || report.Acceptance || report.Classification == Pass {
		t.Fatalf("a dry run must be labelled and can never pass: %+v", report.Classification)
	}
	if len(report.Summaries) != n*(4+4+4+4+5+4+4) || len(report.Heap) != n*7 {
		t.Fatalf("summaries = %d heap = %d", len(report.Summaries), len(report.Heap))
	}
	// Classification, not speed: even with no finding at all a dry run resolves
	// below pass, and the report says why.
	if class, reasons := Resolve(nil, true); class == Pass || !strings.Contains(strings.Join(reasons, "\n"), "DRY RUN") {
		t.Fatalf("an empty dry run resolved to %s: %v", class, reasons)
	}
	if !strings.Contains(strings.Join(report.Reasons, "\n"), "DRY RUN") {
		t.Errorf("dry report does not name the dry run: %v", report.Reasons)
	}
	for _, f := range report.Findings {
		// A latency limit (p99 or max) depends on the speed of the machine running
		// ordinary go test, so it is not asserted here. Any other failure at tiny
		// size (errors, leaks, heap) is a real defect.
		timing := strings.Contains(f.Text, " p99 ") || strings.Contains(f.Text, " max ")
		if f.Class == Fail && !timing {
			t.Errorf("candidate failure at tiny size: %s", f.Text)
		}
	}
	// Candidate order alternates by repetition, on identical declarations.
	first := func(rep int) string {
		for _, r := range records {
			if r.Result.Repetition == rep {
				return r.Result.Backend
			}
		}
		return ""
	}
	if first(0) == first(1) || first(0) == "" {
		t.Errorf("candidate order did not alternate: %q %q", first(0), first(1))
	}
	for _, r := range records {
		if r.Host.Run == "" || len(r.Retained.DeltaBytes) == 0 || r.Result.Cycles == 0 && r.Result.Workload.Topology == Trees {
			t.Fatalf("incomplete run record %d", r.Ordinal)
		}
	}
	// Missing evidence is never a pass: with one repetition no CV is evaluable.
	one := plan
	one.Repetitions = 1
	rec1, _ := CollectIdentity(repoRoot, "scale", one, false, true, nil)
	r1, err := Measure(context.Background(), rec1, one, func(RunRecord) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	var sawCV bool
	for _, f := range r1.Findings {
		sawCV = sawCV || (f.Class == Inconclusive && strings.Contains(f.Text, "CV not evaluable"))
	}
	if !sawCV {
		t.Error("a single repetition did not make the noise evaluation inconclusive")
	}
}
