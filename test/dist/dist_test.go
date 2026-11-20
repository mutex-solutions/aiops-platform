// SPDX-License-Identifier: Apache-2.0

package testdist

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/internal/serve"
	"github.com/mutex-solutions/aiops-platform/test/dist/plugins/teststate"
	"github.com/mutex-solutions/aiops-platform/test/dist/plugins/testsubmit"
)

// These tests run the launcher in this process, with the real loader, the real kernel
// and the plugins of the test distribution, on short limits. The same plugins run in
// the built binary in test/loader-smoke.

// fastLimits makes every deadline a tenth of a second, so that a fault that passes
// one is seen at once.
const fastLimits = "loadDeadline: 200ms\ndrainDeadline: 200ms\ndisposeDeadline: 200ms\nshutdownBound: 5s\nleakGrace: 100ms\n"

type buf struct {
	mu    sync.Mutex
	b     bytes.Buffer
	ready chan struct{}
	once  sync.Once
}

func (w *buf) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.b.Write(p)
	if w.ready != nil && strings.Contains(w.b.String(), "aiops: ready") {
		w.once.Do(func() { close(w.ready) })
	}
	return len(p), nil
}

func (w *buf) String() string { w.mu.Lock(); defer w.mu.Unlock(); return w.b.String() }

// proc is one launcher run in this process.
type proc struct {
	t      *testing.T
	home   string
	out    *buf
	errb   *buf
	sig    chan os.Signal
	done   chan int
	status func() serve.Status
}

func start(t *testing.T, profile string, patches ...string) *proc {
	t.Helper()
	d, err := Distribution()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	limits := filepath.Join(t.TempDir(), "limits.yaml")
	if err := os.WriteFile(limits, []byte(fastLimits), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"--profile", profile, "--home", home, "--limits", limits}
	for _, p := range patches {
		args = append(args, "--patch", filepath.Join("patches", p+".yml"))
	}
	p := &proc{t: t, home: home, out: &buf{ready: make(chan struct{})}, errb: &buf{}, sig: make(chan os.Signal, 1), done: make(chan int, 1)}
	env := serve.Env{Args: args, Getenv: func(string) string { return "" }, Stdout: p.out, Stderr: p.errb, Signals: p.sig}
	go func() { p.done <- serve.Run(context.Background(), "aiops-test", env, d) }()
	return p
}

// ready waits for the process to say it is ready, or to exit.
func (p *proc) ready() {
	p.t.Helper()
	select {
	case <-p.out.ready:
	case code := <-p.done:
		p.t.Fatalf("exited with %d before it was ready; stderr:\n%s", code, p.errb.String())
	}
}

// stop sends SIGTERM and returns the exit code.
func (p *proc) stop() int {
	p.sig <- syscall.SIGTERM
	return <-p.done
}

func (p *proc) wait() int { return <-p.done }

func (p *proc) readStatus() serve.Status {
	p.t.Helper()
	data, err := os.ReadFile(filepath.Join(p.home, "run", serve.StatusFile))
	if err != nil {
		p.t.Fatal(err)
	}
	var s serve.Status
	if err := json.Unmarshal(data, &s); err != nil {
		p.t.Fatal(err)
	}
	return s
}

func (p *proc) readState() teststate.State {
	p.t.Helper()
	data, err := os.ReadFile(filepath.Join(p.home, "run", teststate.File))
	if err != nil {
		p.t.Fatal(err)
	}
	var s teststate.State
	if err := json.Unmarshal(data, &s); err != nil {
		p.t.Fatal(err)
	}
	return s
}

func TestDistributionHoldsTheReleaseTablePlusTheTestPlugins(t *testing.T) {
	d, err := Distribution()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Registry.Modules) != 14 {
		t.Fatalf("%d modules", len(d.Registry.Modules))
	}
	// The test table is the release table plus the test plugins, never the other way.
	if _, ok := d.Registry.Modules["aiops/loader"]; !ok {
		t.Fatal("the release loader is missing")
	}
	// The launcher checks every entry against its plugin and every key name against the
	// key table when it starts, so a run of any profile proves the table is consistent.
}

func TestSmokeProfileInProcess(t *testing.T) {
	p := start(t, "smoke")
	p.ready()
	if s := p.readStatus(); s.Phase != "running" || !s.Ready {
		t.Fatalf("status %+v", s)
	}
	if code := p.stop(); code != boot.ExitOK {
		t.Fatalf("exit %d; stderr:\n%s", code, p.errb.String())
	}
	if s := p.readStatus(); s.Phase != "stopped" || s.Exit != 0 || s.Ready {
		t.Fatalf("final status %+v", s)
	}
	st := p.readState()
	if !st.Closing || st.Ready || len(st.Rows) != 5 || st.Digest == "" || st.Generation == 0 {
		t.Fatalf("final state %+v", st)
	}
}

// TestSmokeStateShowsEveryRowActive reads the state file while the process runs. The
// file is written within one poll interval of a change, so a test waits for the file
// to hold the state it wants: here, ready with every row ACTIVE.
func TestSmokeStateShowsEveryRowActive(t *testing.T) {
	p := start(t, "smoke")
	p.ready()
	st := waitState(t, p, func(s teststate.State) bool { return s.Ready })
	for _, r := range st.Rows {
		if r.Mount != "mounted" || r.State != "ACTIVE" {
			t.Fatalf("row %+v", r)
		}
	}
	if code := p.stop(); code != boot.ExitOK {
		t.Fatalf("exit %d", code)
	}
}

func TestFaultRows(t *testing.T) {
	// For each patch: the exit code, or running (then the process is stopped and its
	// code is ExitOK), and a substring of stderr.
	tests := []struct {
		patch string
		code  int // -1: the process runs and stops cleanly
		want  string
	}{
		{"fail-apply.optional", -1, `"fault" is FAILED`},
		{"fail-apply.required", boot.ExitRequired, `required row "fault" is FAILED`},
		{"panic-apply.optional", -1, `"fault" is FAILED`},
		{"panic-apply.required", boot.ExitRequired, `required row "fault" is FAILED`},
		{"panic-apply-tracked.optional", -1, "panic-apply: tracked goroutine panics"},
		{"slow-apply.optional", -1, `"fault" is FAILED`},
		{"slow-apply.required", boot.ExitRequired, `required row "fault" is FAILED`},
		{"bad-config.optional", -1, "invalid config"},
		{"bad-config.required", boot.ExitRequired, "invalid config"},
		{"rogue-provider.optional", -1, `row "fault" detached as disabled`},
		{"rogue-provider.required", boot.ExitRefused, "denied"},
		{"rogue-provider.isolate", boot.ExitRefused, "static graph"},
		{"cycle", boot.ExitRefused, "static graph"},
		{"unknown-plugin.required", boot.ExitRefused, "not in the module table"},
	}
	for _, tc := range tests {
		t.Run(tc.patch, func(t *testing.T) {
			p := start(t, "smoke", tc.patch)
			var code int
			if tc.code < 0 {
				p.ready()
				code = p.stop()
				if code != boot.ExitOK {
					t.Fatalf("exit %d; stderr:\n%s", code, p.errb.String())
				}
			} else if code = p.wait(); code != tc.code {
				t.Fatalf("exit %d, want %d; stderr:\n%s", code, tc.code, p.errb.String())
			}
			if !strings.Contains(p.errb.String(), tc.want) {
				t.Fatalf("stderr lacks %q:\n%s", tc.want, p.errb.String())
			}
		})
	}
}

// TestSlowDisposeLeaksAtShutdown: a disposer that passes the dispose deadline marks the
// fiber leaked, so a stop is a replacement and not a clean exit.
func TestSlowDisposeLeaksAtShutdown(t *testing.T) {
	p := start(t, "smoke", "slow-dispose.optional")
	p.ready()
	if code := p.stop(); code != boot.ExitLeak {
		t.Fatalf("exit %d; stderr:\n%s", code, p.errb.String())
	}
}

// TestLeakyGoroutineDelaysDrain: a tracked goroutine that ignores its context holds
// the unload past the drain deadline, which the shutdown reports.
func TestLeakyGoroutineDelaysDrain(t *testing.T) {
	p := start(t, "smoke", "leaky-goroutine.optional")
	p.ready()
	code := p.stop()
	if !strings.Contains(p.errb.String(), "drain") {
		t.Fatalf("exit %d; stderr lacks drain:\n%s", code, p.errb.String())
	}
}

func TestSpineHasASlowButBoundedUnload(t *testing.T) {
	// A profile of the toys with config on the spine: drain and dispose delays under
	// the deadlines. The patch makes the spine slow, so the unload is the slowest of
	// the tree and still finishes.
	dir := t.TempDir()
	patch := filepath.Join(dir, "slow-spine.yml")
	if err := os.WriteFile(patch, []byte("- {id: spine, config: {workers: 3, applyDelay: 20ms, drainDelay: 30ms, disposeDelay: 40ms}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := Distribution()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	limits := filepath.Join(dir, "limits.yaml")
	if err := os.WriteFile(limits, []byte(fastLimits), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errb, sig := &buf{ready: make(chan struct{})}, &buf{}, make(chan os.Signal, 1)
	done := make(chan int, 1)
	go func() {
		done <- serve.Run(context.Background(), "aiops-test", serve.Env{
			Args:   []string{"--profile", "smoke", "--home", home, "--limits", limits, "--patch", patch},
			Getenv: func(string) string { return "" }, Stdout: out, Stderr: errb, Signals: sig}, d)
	}()
	select {
	case <-out.ready:
	case code := <-done:
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	sig <- syscall.SIGTERM
	if code := <-done; code != boot.ExitOK {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
}

func TestSubmitRowIsUnlinked(t *testing.T) {
	p := start(t, "control")
	p.ready()
	control := filepath.Join(p.home, "run", testsubmit.Control)
	if err := os.WriteFile(control+".tmp", []byte(`{"patches": ["a.yml"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(control+".tmp", control); err != nil {
		t.Fatal(err)
	}
	waitFile(t, p, testsubmit.Result)
	data, _ := os.ReadFile(filepath.Join(p.home, "run", testsubmit.Result))
	var o testsubmit.Outcome
	if err := json.Unmarshal(data, &o); err != nil {
		t.Fatal(err)
	}
	if o.OK || o.Error != testsubmit.ErrNotLinked.Error() || len(o.Request) != 64 {
		t.Fatalf("outcome %+v", o)
	}
	if code := p.stop(); code != boot.ExitOK {
		t.Fatalf("exit %d", code)
	}
}

func TestSubmitRowRefusesAMalformedControlFile(t *testing.T) {
	p := start(t, "control")
	p.ready()
	if err := os.WriteFile(filepath.Join(p.home, "run", testsubmit.Control), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFile(t, p, testsubmit.Result)
	data, _ := os.ReadFile(filepath.Join(p.home, "run", testsubmit.Result))
	if !strings.Contains(string(data), "control file:") {
		t.Fatalf("result %s", data)
	}
	if code := p.stop(); code != boot.ExitOK {
		t.Fatalf("exit %d", code)
	}
}

// poll calls ok every few milliseconds until it holds, with a bound. A file written by
// another process cannot be waited for any other way without a dependency.
func poll(t *testing.T, what string, ok func() bool) {
	t.Helper()
	bound := time.NewTimer(30 * time.Second)
	defer bound.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for !ok() {
		select {
		case <-tick.C:
		case <-bound.C:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func waitState(t *testing.T, p *proc, ok func(teststate.State) bool) teststate.State {
	t.Helper()
	var got teststate.State
	poll(t, "the state file", func() bool {
		data, err := os.ReadFile(filepath.Join(p.home, "run", teststate.File))
		return err == nil && json.Unmarshal(data, &got) == nil && ok(got)
	})
	return got
}

func waitFile(t *testing.T, p *proc, name string) {
	t.Helper()
	poll(t, name, func() bool {
		_, err := os.Stat(filepath.Join(p.home, "run", name))
		return err == nil
	})
}
