// SPDX-License-Identifier: Apache-2.0

// Package loadersmoke boots the built test binary (test/dist/cmd/aiops-test) on the
// test profile "smoke": the first time the real launcher, the real loader, the five
// core rows and the real kernel run together in one process (08-profiles-roadmap-
// adrs.md:693, S1). Every wait is a bounded poll of a file the process writes; no test
// sleeps for a fixed time.
package loadersmoke

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/internal/serve"
	testdist "github.com/mutex-solutions/aiops-platform/test/dist"
	"github.com/mutex-solutions/aiops-platform/test/dist/plugins/teststate"
	"github.com/mutex-solutions/aiops-platform/test/internal/proc"
	"github.com/mutex-solutions/aiops-platform/test/trusted"
)

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// build compiles the test binary into the temporary directory of t.
func build(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "aiops-test")
	out, err := exec.Command("go", "build", "-o", bin, "../dist/cmd/aiops-test").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

type process struct {
	t      *testing.T
	home   string
	cmd    *exec.Cmd
	child  *proc.Child
	stderr *syncBuf
	exited chan int
}

func patch(name string) string {
	abs, err := filepath.Abs(filepath.Join("..", "dist", "patches", name+".yml"))
	if err != nil {
		panic(err)
	}
	return abs
}

// start runs the binary on the profile with the patch files (absolute paths) and
// extra environment variables.
func start(t *testing.T, bin string, env []string, patches ...string) *process {
	t.Helper()
	return startWith(t, bin, env, nil, patches...)
}

// startWith is start with extra flags.
func startWith(t *testing.T, bin string, env, flags []string, patches ...string) *process {
	t.Helper()
	home := t.TempDir()
	if err := trusted.Write(home, testdist.Signed); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--profile", "smoke", "--home", home}, flags...)
	for _, p := range patches {
		args = append(args, "--patch", p)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	p := &process{t: t, home: home, stderr: &syncBuf{}, exited: make(chan int, 1)}
	p.cmd = proc.Command(ctx, bin, args...)
	p.cmd.Env = append(os.Environ(), env...)
	p.cmd.Stderr = p.stderr
	child, err := proc.Start(p.cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(child.Kill)
	p.child = child
	go func() {
		<-child.Done()
		p.exited <- p.cmd.ProcessState.ExitCode()
	}()
	return p
}

func (p *process) wait() int {
	select {
	case code := <-p.exited:
		return code
	case <-time.After(60 * time.Second):
		p.child.Kill()
		p.t.Fatalf("the process did not exit; stderr:\n%s", p.stderr.String())
		return -1
	}
}

func (p *process) read(name string, v any) bool {
	data, err := os.ReadFile(filepath.Join(p.home, "run", name))
	return err == nil && json.Unmarshal(data, v) == nil
}

// waitState polls the state file until ok holds, within a bound.
func (p *process) waitState(ok func(teststate.State) bool) teststate.State {
	p.t.Helper()
	var st teststate.State
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for !(p.read(teststate.File, &st) && ok(st)) {
		select {
		case <-tick.C:
		case code := <-p.exited:
			p.t.Fatalf("exited with %d; stderr:\n%s", code, p.stderr.String())
		case <-deadline.C:
			p.t.Fatalf("timed out; last state %+v; stderr:\n%s", st, p.stderr.String())
		}
	}
	return st
}

// waitTree is waitState until ok holds and the loader's mounted rows agree with the
// kernel's fibers (teststate.TreeProblems), the oracle that the loader's own records
// cannot be.
func (p *process) waitTree(ok func(teststate.State) bool) teststate.State {
	p.t.Helper()
	var bad []string
	defer func() {
		if p.t.Failed() {
			p.t.Logf("the loader's rows and the kernel's fibers differ: %v", bad)
		}
	}()
	return p.waitState(func(s teststate.State) bool {
		var fibers []teststate.Fiber
		if !ok(s) || !p.read(teststate.KernelFile, &fibers) {
			return false
		}
		bad = teststate.TreeProblems(s, fibers)
		return len(bad) == 0
	})
}

func rowState(s teststate.State, id string) string {
	for _, r := range s.Rows {
		if r.ID == id {
			return r.State
		}
	}
	return ""
}

// logLines returns the lines of stderr that hold every one of parts.
func (p *process) logLines(parts ...string) []string {
	var out []string
	for _, l := range strings.Split(p.stderr.String(), "\n") {
		if !slices.ContainsFunc(parts, func(s string) bool { return !strings.Contains(l, s) }) {
			out = append(out, l)
		}
	}
	return out
}

// waitLog polls stderr until a line holds every one of parts, within a bound.
func (p *process) waitLog(parts ...string) {
	p.t.Helper()
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for len(p.logLines(parts...)) == 0 {
		select {
		case <-tick.C:
		case code := <-p.exited:
			p.t.Fatalf("exited with %d; stderr:\n%s", code, p.stderr.String())
		case <-deadline.C:
			p.t.Fatalf("no log line holds %q; stderr:\n%s", parts, p.stderr.String())
		}
	}
}

// generation submits the generation id after the live layer holds body, and returns
// the reload operation once it has finished.
func (p *process) generation(live string, id uint64, body string) *teststate.Op {
	p.t.Helper()
	if err := os.WriteFile(live, []byte(body), 0o644); err != nil {
		p.t.Fatal(err)
	}
	p.submit(string(rune('0' + id)))
	return p.waitState(func(s teststate.State) bool {
		return s.Reload != nil && s.Reload.ID == id && s.Reload.Status != "queued" && s.Reload.Status != "running"
	}).Reload
}

func (p *process) stop() {
	p.t.Helper()
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	if code := p.wait(); code != boot.ExitOK {
		p.t.Fatalf("exit %d; stderr:\n%s", code, p.stderr.String())
	}
}

func (p *process) submit(marker string) {
	control := filepath.Join(p.home, "run", "submit.json")
	if err := os.WriteFile(control+".tmp", []byte(marker), 0o644); err != nil {
		p.t.Fatal(err)
	}
	if err := os.Rename(control+".tmp", control); err != nil {
		p.t.Fatal(err)
	}
}

func TestLoaderSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the test binary")
	}
	bin := build(t)

	t.Run("boots every row, then stops on SIGTERM", func(t *testing.T) {
		p := start(t, bin, []string{"AIOPS_REF_NOTE=from-env"}, patch("refs"))
		// Ready is set by the real readiness row once the required set is ACTIVE; a row that
		// is not required, test-submit among them, may still be LOADING at that instant, so
		// the wait is for every mounted row to be ACTIVE as well.
		p.waitState(func(s teststate.State) bool {
			for _, r := range s.Rows {
				if r.Mount != "disabled" && (r.Mount != "mounted" || r.State != "ACTIVE") {
					return false
				}
			}
			return s.Ready && len(s.Rows) > 0
		})
		// The latch is set while boot may still be at its required audit, so the status file
		// says "running" a moment after the state file says ready.
		var status serve.Status
		for end := time.Now().Add(30 * time.Second); !p.read(serve.StatusFile, &status) || status.Phase != "running" || !status.Ready; {
			if time.Now().After(end) {
				t.Fatalf("status %+v", status)
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
		if code := p.wait(); code != boot.ExitOK {
			t.Fatalf("exit %d; stderr:\n%s", code, p.stderr.String())
		}
		p.read(serve.StatusFile, &status)
		if status.Phase != "stopped" || status.Exit != 0 || status.Ready {
			t.Fatalf("final status %+v", status)
		}
		var final teststate.State
		if !p.read(teststate.File, &final) || !final.Closing || final.Ready {
			t.Fatalf("final state %+v", final)
		}
		if !strings.Contains(p.stderr.String(), "note=from-env") {
			t.Fatalf("the reference was not resolved before the row applied; stderr:\n%s", p.stderr.String())
		}
	})

	t.Run("live recomposition and a rollback", func(t *testing.T) {
		live := filepath.Join(t.TempDir(), "live.yml")
		write := func(body string) {
			if err := os.WriteFile(live, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		write("- {id: extra, enabled: false}\n")
		p := start(t, bin, nil, live)
		p.waitState(func(s teststate.State) bool { return s.Ready })

		// extra calls the spine all the time and holds each call's lease for 300ms, so its
		// fiber takes up to that long to drain when it unloads.
		const extra = "- {id: extra, enabled: true, config: {note: extra, callEvery: 1ms, callHold: 300ms}"
		write(extra + "}\n")
		p.submit("1")
		st := p.waitState(func(s teststate.State) bool {
			return s.Reload != nil && s.Reload.ID == 1 && s.Reload.Status != "queued" && s.Reload.Status != "running"
		})
		if st.Reload.Status != "passed" || st.Reload.Generation != 1 {
			t.Fatalf("operation %+v", st.Reload)
		}
		p.waitState(func(s teststate.State) bool { return rowState(s, "extra") == "ACTIVE" })

		// flaky fails its first two applies (the bundle's failTimes: 2), so the generation that
		// enables it has a failed verdict: reload rolls it back with one inverse plan (ADR-059
		// decision 12), the row is disabled again and the process stays ready. The same
		// generation replaces extra (its inject changes), and the loader mounts the new extra
		// only once the old one, slow to drain, is disposed, so reload judges long after
		// restart-policy's 20ms backoff could have retried flaky to ACTIVE. The verdict fails
		// all the same: a row FAILED during the apply fails it, and restart-policy holds its
		// retries while the operation is in flight (ADR-059 decision 19). The retry itself is
		// covered at boot.
		write(extra + ", inject: [toy.store]}\n- {id: flaky, enabled: true}\n")
		p.submit("2")
		st = p.waitState(func(s teststate.State) bool {
			return s.Reload != nil && s.Reload.ID == 2 && s.Reload.Status != "queued" && s.Reload.Status != "running"
		})
		if st.Reload.Status != "rolled back" || st.Reload.Rollback == nil || st.Reload.Rollback.Error != "" {
			t.Fatalf("operation %+v", st.Reload)
		}
		p.waitState(func(s teststate.State) bool {
			for _, r := range s.Rows {
				if r.ID == "flaky" && r.Mount != "disabled" {
					return false
				}
			}
			return rowState(s, "extra") == "ACTIVE" && s.Ready
		})
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
		if code := p.wait(); code != boot.ExitOK {
			t.Fatalf("exit %d; stderr:\n%s", code, p.stderr.String())
		}
	})

	// The test distribution's bundle has no group, so this one is a patch: a core:group
	// row with two consumers and a child that fails every apply, disabled. A generation
	// restarts the group, which takes its children with it; the next one enables the bad
	// child too, so its verdict fails and reload restores the group as it was.
	t.Run("a group restarts and rolls back", func(t *testing.T) {
		dir := t.TempDir()
		group, live := filepath.Join(dir, "group.yml"), filepath.Join(dir, "live.yml")
		write := func(file, body string) {
			if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		write(group, "- insert:\n    - {id: grp, plugin: core:group, roles: [api], config: ["+
			"{id: a, plugin: aiops-test/toy-consumer, roles: [api], config: {note: a}}, "+
			"{id: b, plugin: aiops-test/toy-consumer, roles: [api], config: {note: b}}, "+
			"{id: bad, plugin: aiops-test/fail-apply, roles: [api], enabled: false}]}\n")
		write(live, "- {id: extra, enabled: false}\n")
		p := start(t, bin, nil, group, live)
		settled := func(s teststate.State) bool {
			return s.Ready && rowState(s, "grp") == "ACTIVE" && rowState(s, "grp:a") == "ACTIVE" && rowState(s, "grp:b") == "ACTIVE"
		}
		p.waitTree(settled)
		gen := func(id uint64, body string) *teststate.Op {
			write(live, body)
			p.submit(string(rune('0' + id)))
			return p.waitState(func(s teststate.State) bool {
				return s.Reload != nil && s.Reload.ID == id && s.Reload.Status != "queued" && s.Reload.Status != "running"
			}).Reload
		}
		if op := gen(1, "- {id: grp, inject: [toy.store]}\n"); op.Status != "passed" || !slices.Contains(op.Applied["restarted"], "grp") {
			t.Fatalf("the group did not restart: %+v", op)
		}
		p.waitTree(settled)
		op := gen(2, "- {id: grp, inject: [toy.store]}\n- {id: \"grp:bad\", enabled: true}\n")
		if op.Status != "rolled back" || op.Rollback == nil || op.Rollback.Error != "" {
			t.Fatalf("operation %+v", op)
		}
		p.waitTree(func(s teststate.State) bool {
			for _, r := range s.Rows {
				if r.ID == "grp:bad" && r.Mount != "disabled" {
					return false
				}
			}
			return settled(s)
		})
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
		if code := p.wait(); code != boot.ExitOK {
			t.Fatalf("exit %d; stderr:\n%s", code, p.stderr.String())
		}
	})

	// ADR-034 G13 end to end. The toy consumer logs its note and tag at each call of its
	// load goroutine, so a run sees what the running activation reads after Apply.
	const call, applied = `msg="toy-consumer call"`, `msg="toy-consumer applied"`
	write := func(t *testing.T, file, body string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("a volatile note is committed in place", func(t *testing.T) {
		live := filepath.Join(t.TempDir(), "live.yml")
		const row = "- {id: consumer, config: {callEvery: 20ms, note: %s}}\n"
		write(t, live, fmt.Sprintf(row, "first"))
		p := start(t, bin, nil, live)
		p.waitState(func(s teststate.State) bool { return s.Ready && rowState(s, "consumer") == "ACTIVE" })
		p.waitLog(call, "note=first")
		if op := p.generation(live, 1, fmt.Sprintf(row, "second")); op.Status != "passed" || !slices.Contains(op.Applied["committed"], "consumer") {
			t.Fatalf("operation %+v", op)
		}
		p.waitLog(call, "note=second")
		if a, b := p.logLines(applied, "note=first"), p.logLines(applied, "note=second"); len(a) != 1 || len(b) != 0 {
			t.Fatalf("the row applied again: %q, %q", a, b)
		}
		p.stop()
	})

	// A literal changed beside a reference that did not change commits in place, and
	// the reference is resolved again for the hand-off (question Q5 (a)).
	t.Run("a tag committed beside a reference reads the reference again", func(t *testing.T) {
		dir := t.TempDir()
		refs, live := filepath.Join(dir, "refs"), filepath.Join(dir, "live.yml")
		if err := os.Mkdir(refs, 0o755); err != nil {
			t.Fatal(err)
		}
		const row = "- {id: consumer, config: {callEvery: 20ms, note: \"${file:note}\", tag: %s}}\n"
		write(t, filepath.Join(refs, "note"), "file-one")
		write(t, live, fmt.Sprintf(row, "alpha"))
		p := startWith(t, bin, nil, []string{"--refs-dir", refs}, live)
		p.waitState(func(s teststate.State) bool { return s.Ready && rowState(s, "consumer") == "ACTIVE" })
		p.waitLog(call, "note=file-one", "tag=alpha")
		write(t, filepath.Join(refs, "note"), "file-two")
		if op := p.generation(live, 1, fmt.Sprintf(row, "beta")); op.Status != "passed" || !slices.Contains(op.Applied["committed"], "consumer") {
			t.Fatalf("operation %+v", op)
		}
		p.waitLog(call, "note=file-two", "tag=beta")
		if a := p.logLines(applied); len(a) != 1 {
			t.Fatalf("the row applied again: %q", a)
		}
		p.stop()
	})

	// The loader checks a child's new config on its fiber before the batch disposes the
	// group, with a CommitOp: the child's running activation is handed nothing, and the
	// activation that replaces it, mounted only once the old one is disposed, applies it.
	// How long the child runs after the batch is the kernel's to order; the kernel test
	// TestCommitOpHandsItsConfigOnceTheBatchIsAdmitted holds that window open.
	t.Run("a child of a replaced group does not take its new note in place", func(t *testing.T) {
		dir := t.TempDir()
		group, live := filepath.Join(dir, "group.yml"), filepath.Join(dir, "live.yml")
		write(t, group, "- insert:\n    - {id: grp, plugin: core:group, roles: [api], config: ["+
			"{id: a, plugin: aiops-test/toy-consumer, roles: [api], config: {callEvery: 20ms, note: old}}]}\n")
		write(t, live, "- {id: extra, enabled: false}\n")
		p := start(t, bin, nil, group, live)
		p.waitTree(func(s teststate.State) bool { return s.Ready && rowState(s, "grp:a") == "ACTIVE" })
		p.waitLog(call, "note=old")
		op := p.generation(live, 1, "- {id: grp, inject: [toy.store]}\n- {id: \"grp:a\", config: {callEvery: 20ms, note: new}}\n")
		if op.Status != "passed" || !slices.Contains(op.Applied["restarted"], "grp") {
			t.Fatalf("operation %+v", op)
		}
		p.waitLog(call, "note=new")
		if first := p.logLines("note=new")[0]; !strings.Contains(first, applied) {
			t.Fatalf("the running activation of grp:a read the new note before its replacement applied: %s", first)
		}
		p.stop()
	})

	for _, tc := range []struct {
		name  string
		patch string
		code  int
		want  string
	}{
		{"a cycle", "cycle", boot.ExitRefused, "static graph"},
		{"a required row whose Apply fails", "fail-apply.required", boot.ExitRequired, `required row "fault" is FAILED`},
		{"an unknown plugin id on a required row", "unknown-plugin.required", boot.ExitRefused, "not in the module table"},
	} {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			p := start(t, bin, nil, patch(tc.patch))
			if code := p.wait(); code != tc.code || !strings.Contains(p.stderr.String(), tc.want) {
				t.Fatalf("exit %d, want %d with %q; stderr:\n%s", code, tc.code, tc.want, p.stderr.String())
			}
		})
	}

	t.Run("an untracked goroutine that panics is fatal", func(t *testing.T) {
		p := start(t, bin, nil, patch("panic-apply-untracked.optional"))
		if code := p.wait(); code == 0 || !strings.Contains(p.stderr.String(), "panic") {
			t.Fatalf("exit %d; stderr:\n%s", code, p.stderr.String())
		}
	})
}
