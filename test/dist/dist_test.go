// SPDX-License-Identifier: Apache-2.0

package testdist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/internal/serve"
	"github.com/mutex-solutions/aiops-platform/test/dist/plugins/teststate"
	"github.com/mutex-solutions/aiops-platform/test/trusted"
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
	if w.ready != nil && strings.Contains(w.b.String(), "aiops: booted") {
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
	return startEnv(t, nil, profile, patches...)
}

// startEnv is start with an environment. A patch that is an absolute path is used as
// it is; any other names a file of patches/.
func startEnv(t *testing.T, environ map[string]string, profile string, patches ...string) *proc {
	t.Helper()
	d, err := Distribution()
	if err != nil {
		t.Fatal(err)
	}
	return startDist(t, d, environ, profile, patches...)
}

// startDist is startEnv with the distribution given.
func startDist(t *testing.T, d serve.Distribution, environ map[string]string, profile string, patches ...string) *proc {
	t.Helper()
	home := t.TempDir()
	if err := trusted.Write(home, d.Signed); err != nil {
		t.Fatal(err)
	}
	limits := filepath.Join(t.TempDir(), "limits.yaml")
	if err := os.WriteFile(limits, []byte(fastLimits), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"--profile", profile, "--home", home, "--limits", limits}
	for _, p := range patches {
		if !filepath.IsAbs(p) {
			p = filepath.Join("patches", p+".yml")
		}
		args = append(args, "--patch", p)
	}
	p := &proc{t: t, home: home, out: &buf{ready: make(chan struct{})}, errb: &buf{}, sig: make(chan os.Signal, 1), done: make(chan int, 1)}
	env := serve.Env{Args: args, Getenv: func(n string) string { return environ[n] }, Stdout: p.out, Stderr: p.errb, Signals: p.sig}
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
	if len(d.Registry.Modules) != 29 {
		t.Fatalf("%d modules", len(d.Registry.Modules))
	}
	// The test table is the release table plus the test plugins, never the other way.
	if _, ok := d.Registry.Modules["aiops/loader"]; !ok {
		t.Fatal("the release loader is missing")
	}
	// The volatile paths of toy-consumer are derived from its plugin.Volatile fields and
	// read from the marks in its manifest, note the path the S1 hand list named, so a
	// change of note or tag alone commits in place (ADR-048 decisions 9 and 18).
	if v := d.Registry.Modules["aiops-test/toy-consumer"].Volatile; !slices.Equal(v, []string{"config.note", "config.tag"}) {
		t.Fatalf("toy-consumer volatile paths %q, want [config.note config.tag]", v)
	}
	// The launcher checks every entry against its plugin and every key name against the
	// key table when it starts, so a run of any profile proves the table is consistent.
}

func TestSmokeProfileInProcess(t *testing.T) {
	p := start(t, "smoke")
	p.ready()
	waitState(t, p, func(s teststate.State) bool { return s.Ready }) // set by the readiness row
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
	if !st.Closing || st.Ready || len(st.Rows) != 12 || st.Digest == "" || st.Generation == 0 {
		t.Fatalf("final state %+v", st)
	}
}

// profile render composes exactly what the server composes (08-profiles-roadmap-adrs.md
// §1.5): on the signed manifest the binary embeds and the same home, its digest is the
// digest the process booted, the module of each plugin included (ADR-060 decision 20).
func TestRenderComposesWhatTheProcessBooted(t *testing.T) {
	p := start(t, "smoke")
	p.ready()
	booted := p.readStatus().Digest
	d, err := Distribution()
	if err != nil {
		t.Fatal(err)
	}
	home := fstest.MapFS{}
	for _, fsys := range []fs.FS{d.Shipped, os.DirFS(p.home)} {
		err := fs.WalkDir(fsys, ".", func(name string, e fs.DirEntry, err error) error {
			if err == nil && !e.IsDir() {
				var data []byte
				data, err = fs.ReadFile(fsys, name)
				home[name] = &fstest.MapFile{Data: data}
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if code := p.stop(); code != boot.ExitOK {
		t.Fatalf("exit %d; stderr:\n%s", code, p.errb.String())
	}
	man, err := boot.ReadDistribution("distribution.yaml", d.Signed)
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := boot.Render(boot.Sources{Profile: "smoke", Home: home, Bundles: home}, true, man, nil)
	if err != nil || a.Refused() {
		t.Fatalf("render: %v %v", err, a.Diagnostics())
	}
	if got := fmt.Sprintf("%x", a.Comp.Digest[:]); got != booted {
		t.Errorf("render composes %s, the process booted %s", got, booted)
	}
}

// TestSmokeStateShowsEveryRowActive reads the state file while the process runs. The
// file is written within one poll interval of a change, so a test waits for the file
// to hold the state it wants: here, ready with every row ACTIVE. The latch follows the
// required set only, so a file can say ready while an optional row, test-state itself
// among them, is still LOADING: the wait is for both.
func TestSmokeStateShowsEveryRowActive(t *testing.T) {
	p := start(t, "smoke")
	p.ready()
	waitState(t, p, func(s teststate.State) bool {
		return s.Ready && !slices.ContainsFunc(s.Rows, func(r teststate.Row) bool {
			return r.Mount != "disabled" && (r.Mount != "mounted" || r.State != "ACTIVE")
		})
	})
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
// the unload past the drain deadline, so the fiber is marked leaked and a stop is a
// replacement, not a clean exit (ADR-059 decision 13).
func TestLeakyGoroutineDelaysDrain(t *testing.T) {
	p := start(t, "smoke", "leaky-goroutine.optional")
	p.ready()
	if code := p.stop(); code != boot.ExitLeak || !strings.Contains(p.errb.String(), "drain deadline") {
		t.Fatalf("exit %d, want %d and the drain deadline in stderr:\n%s", code, boot.ExitLeak, p.errb.String())
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
	if err := trusted.Write(home, d.Signed); err != nil {
		t.Fatal(err)
	}
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

// poll calls ok every few milliseconds until it holds, with a bound: a file written
// by a plugin cannot be waited for any other way without a dependency.
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

func TestReferenceIsResolvedByConfigRefs(t *testing.T) {
	p := startEnv(t, map[string]string{"AIOPS_REF_NOTE": "from-env"}, "smoke", "refs")
	p.ready()
	waitState(t, p, func(s teststate.State) bool { return s.Ready })
	if code := p.stop(); code != boot.ExitOK {
		t.Fatalf("exit %d; stderr:\n%s", code, p.errb.String())
	}
	if !strings.Contains(p.errb.String(), "note=from-env") {
		t.Fatalf("the consumer did not see the resolved value; stderr:\n%s", p.errb.String())
	}
}

// TestRequiredReferenceBootsOnTheReleaseRequiredSet: a required row whose config holds a
// reference boots on a required set that, like the release one, does not list
// configRefsReady, because the launcher adds config-refs and the key to the set itself
// (ADR-059 decision 18).
func TestRequiredReferenceBootsOnTheReleaseRequiredSet(t *testing.T) {
	d, err := Distribution()
	if err != nil {
		t.Fatal(err)
	}
	if m, err := boot.ReadDistribution("distribution.yaml", d.Signed); err != nil || slices.Contains(m.Bindings().RequiredKeys, "configRefsReady") {
		t.Fatalf("the test manifest requires configRefsReady, which hides what the release requires (%v)", err)
	}
	p := startDist(t, d, map[string]string{"AIOPS_REF_NOTE": "from-env"}, "smoke", "refs.required")
	p.ready()
	waitState(t, p, func(s teststate.State) bool { return s.Ready })
	if code := p.stop(); code != boot.ExitOK {
		t.Fatalf("exit %d; stderr:\n%s", code, p.errb.String())
	}
	if !strings.Contains(p.errb.String(), "note=from-env") {
		t.Fatalf("the required row did not see the resolved value; stderr:\n%s", p.errb.String())
	}
}

func TestUnresolvedReferenceFollowsTheMatrix(t *testing.T) {
	p := start(t, "smoke", "refs-unresolved.optional")
	p.ready()
	if code := p.stop(); code != boot.ExitOK || !strings.Contains(p.errb.String(), `"fault" is FAILED`) || !strings.Contains(p.errb.String(), "does not resolve") {
		t.Fatalf("exit %d; stderr:\n%s", code, p.errb.String())
	}
	p = start(t, "smoke", "refs-unresolved.required")
	if code := p.wait(); code != boot.ExitRequired || !strings.Contains(p.errb.String(), "does not resolve") {
		t.Fatalf("exit %d; stderr:\n%s", code, p.errb.String())
	}
}

// TestLiveRecomposition enables a disabled row through test-submit and the real reload,
// then a row that fails its first two applies: the generation that enables it has a failed
// verdict, so reload rolls it back (ADR-059 decision 12) instead of waiting for
// restart-policy to retry the row until it is ACTIVE, which the row does at boot.
// restart-policy keeps the short backoff of the smoke bundle, and the same generation
// replaces extra, which is slow to drain (it holds a lease of the spine for 150ms at a
// time), so the judge comes after the row could have been retried to ACTIVE: a row
// FAILED during the apply fails the verdict whatever its state when judged, and
// restart-policy holds its retries while the operation is in flight (ADR-059 decision
// 19).
func TestLiveRecomposition(t *testing.T) {
	live := filepath.Join(t.TempDir(), "live.yml")
	write := func(body string) {
		if err := os.WriteFile(live, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("- {id: extra, enabled: false}\n")
	p := startEnv(t, nil, "smoke", live)
	p.ready()
	waitState(t, p, func(s teststate.State) bool { return s.Ready })
	submit := func(marker string) {
		control := filepath.Join(p.home, "run", "submit.json")
		if err := os.WriteFile(control+".tmp", []byte(marker), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(control+".tmp", control); err != nil {
			t.Fatal(err)
		}
	}
	rowState := func(s teststate.State, id string) string {
		for _, r := range s.Rows {
			if r.ID == id {
				return r.State
			}
		}
		return ""
	}

	const extra = "- {id: extra, enabled: true, config: {note: extra, callEvery: 1ms, callHold: 150ms}"
	write(extra + "}\n")
	submit("1")
	st := waitState(t, p, func(s teststate.State) bool {
		return s.Reload != nil && s.Reload.ID == 1 && s.Reload.Status != "queued" && s.Reload.Status != "running"
	})
	if st.Reload.Status != "passed" || st.Reload.Generation != 1 || st.Reload.Error != "" {
		t.Fatalf("operation %+v", st.Reload)
	}
	waitState(t, p, func(s teststate.State) bool { return rowState(s, "extra") == "ACTIVE" })

	write(extra + ", inject: [toy.store]}\n- {id: flaky, enabled: true}\n")
	submit("2")
	st = waitState(t, p, func(s teststate.State) bool {
		return s.Reload != nil && s.Reload.ID == 2 && s.Reload.Status != "queued" && s.Reload.Status != "running"
	})
	if st.Reload.Status != "rolled back" || st.Reload.Rollback == nil || st.Reload.Rollback.Error != "" ||
		st.Reload.Generation != 2 || st.Reload.Rollback.Generation != 3 {
		t.Fatalf("operation %+v", st.Reload)
	}
	waitState(t, p, func(s teststate.State) bool {
		for _, r := range s.Rows {
			if r.ID == "flaky" && r.Mount != "disabled" {
				return false // still mounted: the rollback is not done
			}
		}
		return rowState(s, "extra") == "ACTIVE" && s.Ready
	})
	if code := p.stop(); code != boot.ExitOK {
		t.Fatalf("exit %d; stderr:\n%s", code, p.errb.String())
	}
}
