// SPDX-License-Identifier: Apache-2.0

// Package recomposition is the recomposition suite on the built test binary
// (test/dist/cmd/aiops-test): the cases of plugin-system-2026-11.md:506 that S1 runs
// (ADR-059 decision 5; 08-profiles-roadmap-adrs.md:693). The process boots the profile
// "smoke" with a live layer file as its last --patch; a test writes the next generation
// into that file and submits it through test-submit (the control file), and reads what
// reload did from the state file of test-state and what the spine saw from the report
// it keeps (toy-spine, config "report"). Every wait is a bounded poll of those files.
//
//	(a) in-place restart of the spine provider under load
//	(b) a submission from a leased admin call
//	(c) the judge fires while a fiber is UNLOADING
//	(d) a newly enabled row that never activates
//	(e) a replica restart during promotion converging without a second exit: S4b
package recomposition

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/test/dist/harness"
	"github.com/mutex-solutions/aiops-platform/test/dist/plugins/teststate"
	"github.com/mutex-solutions/aiops-platform/test/dist/plugins/testsubmit"
	"github.com/mutex-solutions/aiops-platform/test/dist/plugins/toy"
)

func TestMain(m *testing.M) {
	code := m.Run()
	harness.Cleanup()
	os.Exit(code)
}

// budget bounds every wait of one case.
const budget = 40 * time.Second

// liveBase is what the live layer holds at boot: a valid layer that changes nothing.
const liveBase = "- {id: extra, enabled: false}\n"

// world is a running process and its live layer file.
type world struct {
	t    *testing.T
	p    *harness.Proc
	live string
	ops  uint64 // submissions so far
}

// boot starts the binary on "smoke" with the patch files (a name in test/dist/patches or
// an absolute path) and the live layer last, and waits for it to say it is ready.
func boot(t *testing.T, patches ...string) *world {
	t.Helper()
	bin := harness.Binary(t)
	dir := t.TempDir()
	w := &world{t: t, live: filepath.Join(dir, "live.yml")}
	if err := os.WriteFile(w.live, []byte(liveBase), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := harness.Options{Context: t.Context()}
	for _, f := range patches {
		if !filepath.IsAbs(f) {
			f = harness.PatchFile(f)
		}
		opts.Patches = append(opts.Patches, f)
	}
	opts.Patches = append(opts.Patches, w.live)
	p, err := harness.Start(bin, opts)
	if err != nil {
		t.Fatal(err)
	}
	w.p = p
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("output of the process:\n%s", p.Output())
		}
		p.Release()
	})
	s, ok := p.Until(budget, func(s harness.Snapshot) bool { return s.Running() && s.Status.Ready && s.State.Ready })
	if !ok {
		t.Fatalf("the process did not become ready: %+v", s.Status)
	}
	return w
}

// submit writes the patch file of test/dist/patches into the live layer and submits one
// generation with the marker prefix ("" or "leased"). It returns the id of the operation.
func (w *world) submit(patch, prefix string) uint64 {
	w.t.Helper()
	body, err := os.ReadFile(harness.PatchFile(patch))
	if err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(w.live, body, 0o644); err != nil {
		w.t.Fatal(err)
	}
	w.ops++
	if err := w.p.Submit(prefix + string(rune('0'+w.ops))); err != nil {
		w.t.Fatal(err)
	}
	return w.ops
}

// verdictOf waits for operation id to be final and returns the snapshot.
func (w *world) verdictOf(id uint64) harness.Snapshot {
	w.t.Helper()
	s, ok := w.p.Until(budget, func(s harness.Snapshot) bool { return s.OpDone(id) })
	if !ok {
		w.t.Fatalf("operation %d has no verdict: exited=%v code=%d, reload=%+v", id, s.Exited, s.Code, s.State.Reload)
	}
	if s.State.Reload.ID != id {
		w.t.Fatalf("the last operation is %d, want %d", s.State.Reload.ID, id)
	}
	return s
}

func (w *world) spine() (r toy.SpineReport) {
	w.p.ReadJSON("spine.json", &r)
	return r
}

// verdict returns the verdict row of id.
func verdict(op *teststate.Op, id string) (teststate.Verdict, bool) {
	for _, v := range op.Verdict {
		if v.ID == id {
			return v, true
		}
	}
	return teststate.Verdict{}, false
}

// allActive says whether every mounted row of the snapshot is ACTIVE.
func allActive(s harness.Snapshot) bool {
	for _, r := range s.State.Rows {
		if r.Mount == "mounted" && r.State != "ACTIVE" {
			return false
		}
	}
	return len(s.State.Rows) > 0
}

// stop ends the process with SIGTERM and checks it stopped cleanly.
func (w *world) stop() {
	w.t.Helper()
	if code := w.p.Stop(20 * time.Second); code != 0 {
		w.t.Fatalf("the process stopped with %d, want a clean stop; output:\n%s", code, w.p.Output())
	}
}

// (a) The heavy spine provider restarts in place while three consumers call it in a
// loop, each call holding a lease of the spine's fiber, and while an outside caller that
// no fiber tracks and no dependant owns holds a lease across the start of the unload
// (toy.Spine, outsideHold). The consumers are dependants, so the unload waits for them
// anyway; the outside call is held back by its lease alone. The restart drains the
// leases before the disposer runs: no call is open when the disposer begins and none
// sees a provider that is being disposed. A call that arrives once the unload began is
// refused, and the consumers are unloaded and come back, so calls go on after the
// restart.
func TestSpineRestartsInPlaceUnderLoad(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "spine-outside.yml")
	// A config in a patch replaces the whole config: this is spine-load.yml's plus the
	// outside caller. spine-restart.yml replaces it again, so the new incarnation has none.
	if err := os.WriteFile(outside, []byte("- {id: spine, config: {workers: 2, report: spine.json, outsideHold: 300ms}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := boot(t, "spine-load.yml", outside)
	before, ok := w.p.Until(budget, func(harness.Snapshot) bool { return w.spine().Calls >= 100 })
	if !ok {
		t.Fatalf("the consumers made no load: %+v\n%s", w.spine(), before.Output)
	}
	callsBefore := w.spine().Calls

	id := w.submit("spine-restart.yml", "")
	s := w.verdictOf(id)
	op := s.State.Reload
	if op.Status != "passed" || op.Generation != 1 {
		t.Fatalf("the generation did not pass: %+v", op)
	}
	if !contains(op.Applied["restarted"], "spine") {
		t.Fatalf("the spine was not restarted in place: applied %v", op.Applied)
	}
	for _, row := range []string{"spine", "store", "consumer", "loada", "loadb", "loadc"} {
		if v, ok := verdict(op, row); !ok || v.Outcome != "pass" || v.State != "ACTIVE" {
			t.Errorf("verdict of %s: %+v (present %v)", row, v, ok)
		}
	}

	// The consumers returned to ACTIVE and are calling the new incarnation.
	s, ok = w.p.Until(budget, func(s harness.Snapshot) bool {
		r := w.spine()
		return s.Running() && s.Status.Ready && allActive(s) && r.Incarnations == 2 && r.Calls >= callsBefore+100
	})
	r := w.spine()
	if !ok {
		t.Fatalf("the consumers did not return: spine %+v, state %+v", r, s.State.Rows)
	}
	if r.HalfDisposed != 0 || r.AtDispose != 0 {
		t.Errorf("a call met a provider that was being disposed: %+v", r)
	}
	if r.OpenAtUnload != 1 {
		t.Errorf("the outside call was not open when the unload began, so the drain had no lease to wait for: %+v", r)
	}
	if r.Refused == 0 {
		t.Errorf("no call made after the unload began was refused: %+v", r)
	}
	w.stop()
	if r := w.spine(); r.HalfDisposed != 0 || r.AtDispose != 0 || r.InFlight != 0 {
		t.Errorf("after the stop: %+v", r)
	}
}

// (b) A leased call submits a generation that restarts the row that is making the call.
// Submit never waits, so the lease can be held until reload has begun the operation, which
// then waits for that lease at the drain of the row and goes on once it is released: no
// deadlock, and the result is delivered, both in the outcome that the row writes and in
// the verdict that reload gives.
func TestSubmissionFromALeasedCall(t *testing.T) {
	w := boot(t)
	id := w.submit("submit-restart.yml", "leased")
	s := w.verdictOf(id)
	op := s.State.Reload
	if op.Status != "passed" || op.Generation != 1 || !contains(op.Applied["restarted"], "submit") {
		t.Fatalf("the generation did not restart the submitter and pass: %+v", op)
	}
	var out testsubmit.Outcome
	if !w.p.ReadJSON(testsubmit.Result, &out) {
		t.Fatal("the submitter wrote no outcome")
	}
	if !out.OK || out.ID != id || !out.Leased || !out.Ran || out.Error != "" {
		t.Fatalf("outcome %+v: the submission was not made inside a lease and delivered", out)
	}
	if s, ok := w.p.Until(budget, func(s harness.Snapshot) bool { return s.Status.Ready && allActive(s) }); !ok {
		t.Fatalf("the tree did not settle: %+v", s.State.Rows)
	}
	w.stop()
}

// (c) The spine takes 600ms to dispose, which is slow and bounded. A generation restarts
// it, and its fiber is UNLOADING when reload begins to judge: the verdict waits, and is
// given when the tree is ACTIVE again, never before.
func TestJudgeWaitsForAnUnloadingFiber(t *testing.T) {
	w := boot(t, "spine-slow-dispose.yml")
	id := w.submit("spine-slow-dispose.restart.yml", "")
	sawUnloading := false
	var early string
	s, ok := w.p.Until(budget, func(s harness.Snapshot) bool {
		if s.RowState("spine") == "UNLOADING" {
			if s.OpDone(id) {
				early = "the verdict was given while the spine was UNLOADING"
			} else {
				sawUnloading = true
			}
		}
		return s.OpDone(id)
	})
	if !ok {
		t.Fatalf("no verdict: %+v", s.State.Reload)
	}
	if early != "" {
		t.Error(early)
	}
	if !sawUnloading {
		t.Error("the spine was never seen UNLOADING before the verdict: the case did not run")
	}
	op := s.State.Reload
	if op.Status != "passed" {
		t.Fatalf("the verdict was given early or wrong: %+v", op)
	}
	if v, ok := verdict(op, "spine"); !ok || v.Outcome != "pass" || v.State != "ACTIVE" {
		t.Errorf("verdict of spine: %+v", v)
	}
	if got := s.RowState("spine"); got != "ACTIVE" {
		t.Errorf("the spine is %q when the verdict is given", got)
	}
	w.stop()
}

// (d) A row that the generation enables never activates: the verdict reports it, the rows
// that were ACTIVE stay so, and reload rolls the generation back with one inverse plan: the
// result names the composition that the tree ran before (Result.Previous), which is
// running again, with the row disabled as it was (ADR-059 decision 12).
func TestNewlyEnabledRowThatNeverActivates(t *testing.T) {
	w := boot(t)
	boot0, _ := w.p.Until(budget, func(s harness.Snapshot) bool { return s.HaveState && s.State.Digest != "" })
	was := boot0.State.Digest
	id := w.submit("never-active.yml", "")
	s := w.verdictOf(id)
	op := s.State.Reload
	if op.Status != "rolled back" || !strings.Contains(op.Error, "failed verdict") || op.Rollback == nil || op.Rollback.Error != "" {
		t.Fatalf("the generation should have a failed verdict and be rolled back: %+v", op)
	}
	if op.Generation != 1 || op.Rollback.Generation != 2 {
		t.Errorf("the failed generation is 1 and the restored one 2: %d and %d", op.Generation, op.Rollback.Generation)
	}
	for _, v := range op.Rollback.Verdict {
		if v.Outcome == "fail" {
			t.Errorf("the restored tree has a failed row: %+v", v)
		}
	}
	v, ok := verdict(op, "extra")
	if !ok || v.Outcome != "fail" || v.State != "PENDING" || !strings.Contains(v.Detail, "enabled by this generation") {
		t.Errorf("verdict of extra: %+v (present %v)", v, ok)
	}
	for _, row := range []string{"clock", "refs", "readiness", "restart-policy", "reload", "spine", "store", "consumer", "state", "submit"} {
		if v, ok := verdict(op, row); !ok || v.Outcome != "pass" || v.State != "ACTIVE" {
			t.Errorf("verdict of %s: %+v (present %v)", row, v, ok)
		}
		if got := s.RowState(row); got != "ACTIVE" {
			t.Errorf("row %s is %q after the failed generation", row, got)
		}
	}
	// The row is disabled again, as it was before the generation: not mounted, no fiber.
	if rs, ok := s.Row("extra"); !ok || rs.Mount != "disabled" {
		t.Errorf("row extra after the rollback: %+v (present %v), want disabled", rs, ok)
	}
	if op.PreviousDigest != was || op.PreviousRows == 0 || op.Digest == was {
		t.Errorf("Result.Previous is not the composition that booted: previous %q (%d rows), candidate %q, booted %q",
			op.PreviousDigest, op.PreviousRows, op.Digest, was)
	}
	if !s.Status.Ready || !s.Running() {
		t.Errorf("the process is not serving after a rolled back generation: %+v", s.Status)
	}
	w.stop()
}

// (e) A replica restart during promotion converging without a second exit needs the
// composer and a promoting set, which are S4b (ADR-059 decision 5). It is reported, and
// it is not a pass.
func TestReplicaRestartDuringPromotion(t *testing.T) {
	const msg = "not yet (S4b): a replica restart during promotion converging without a second exit; needs composer (ADR-059 decision 5)"
	t.Log(msg)
	t.Skip(msg)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
