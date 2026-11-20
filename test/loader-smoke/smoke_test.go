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
	"github.com/mutex-solutions/aiops-platform/test/dist/plugins/teststate"
	"github.com/mutex-solutions/aiops-platform/test/internal/proc"
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
	home := t.TempDir()
	args := []string{"--profile", "smoke", "--home", home}
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

func rowState(s teststate.State, id string) string {
	for _, r := range s.Rows {
		if r.ID == id {
			return r.State
		}
	}
	return ""
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

		write("- {id: extra, enabled: true}\n")
		p.submit("1")
		st := p.waitState(func(s teststate.State) bool {
			return s.Reload != nil && s.Reload.ID == 1 && s.Reload.Status != "queued" && s.Reload.Status != "running"
		})
		if st.Reload.Status != "passed" || st.Reload.Generation != 1 {
			t.Fatalf("operation %+v", st.Reload)
		}
		p.waitState(func(s teststate.State) bool { return rowState(s, "extra") == "ACTIVE" })

		// flaky with no failTimes fails every apply, so the generation that enables it has a
		// failed verdict: reload rolls it back with one inverse plan (ADR-059 decision 12), the
		// row is disabled again and the process stays ready. The bundle's failTimes: 2 would
		// make the verdict a race between the judge and restart-policy's retries, which extra,
		// restarted in the same generation with a slow apply, makes the retries win. The
		// retry itself is covered at boot.
		write("- {id: extra, enabled: true, config: {note: extra, applyDelay: 100ms}}\n- {id: flaky, enabled: true, config: {}}\n")
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
		p.waitState(settled)
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
		p.waitState(settled)
		op := gen(2, "- {id: grp, inject: [toy.store]}\n- {id: \"grp:bad\", enabled: true}\n")
		if op.Status != "rolled back" || op.Rollback == nil || op.Rollback.Error != "" {
			t.Fatalf("operation %+v", op)
		}
		p.waitState(func(s teststate.State) bool {
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
