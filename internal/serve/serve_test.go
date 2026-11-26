// SPDX-License-Identifier: Apache-2.0

package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/dist"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/test/trusted"
)

// readyWriter is a stdout that tells the test when the launcher says it is ready.
type readyWriter struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	ready chan struct{}
	once  sync.Once
}

func (w *readyWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	if strings.Contains(w.buf.String(), "aiops: booted") {
		w.once.Do(func() { close(w.ready) })
	}
	return len(p), nil
}

type safeBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *safeBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// distribution is the loader alone with no required row: a process that boots at once
// and has nothing to wait for. The release distribution is run by test/loader-smoke.
func distribution(t *testing.T) Distribution {
	t.Helper()
	reg, signed, lim, err := dist.Load()
	if err != nil {
		t.Fatal(err)
	}
	reg.Modules = boot.ModuleTable{boot.LoaderID: reg.Modules[boot.LoaderID]}
	// The loader reads the role from profileContext, which is consumer-bound.
	signed = trusted.Rebind(signed, "{consumers: {profileContext: [aiops/loader]}}")
	return Distribution{Registry: reg, Signed: signed, Limits: lim}
}

func env(args ...string) (Env, *safeBuf, chan os.Signal) {
	var stderr safeBuf
	sig := make(chan os.Signal, 1)
	return Env{Args: args, Getenv: func(string) string { return "" }, Stdout: &safeBuf{}, Stderr: &stderr, Signals: sig}, &stderr, sig
}

// home is a home that trusts the signed manifest of distribution (test/trusted).
func home(t *testing.T, bundle string) string {
	t.Helper()
	h := t.TempDir()
	trusted.Write(t, h, distribution(t).Signed)
	for name, data := range map[string]string{"profiles/p/profile.yaml": "bundles: [b]", "bundles/b/bundle.yaml": bundle} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(h, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(h, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

func readStatus(t *testing.T, h string) Status {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(h, "run", StatusFile))
	if err != nil {
		t.Fatal(err)
	}
	var s Status
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRunBootsAndStops(t *testing.T) {
	h := home(t, "[]")
	out := &readyWriter{ready: make(chan struct{})}
	e, _, sig := env("--profile", "p", "--home", h)
	e.Stdout = out
	done := make(chan int, 1)
	go func() { done <- Run(context.Background(), "aiops", e, distribution(t)) }()
	select {
	case <-out.ready:
	case code := <-done:
		t.Fatalf("exited with %d", code)
	}
	if s := readStatus(t, h); s.Phase != "running" || s.Ready || s.Profile != "p" || s.Role != "all" {
		t.Fatalf("status %+v", s)
	}
	sig <- syscall.SIGTERM
	if code := <-done; code != boot.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if s := readStatus(t, h); s.Phase != "stopped" || s.Ready || s.Exit != 0 {
		t.Fatalf("final status %+v", s)
	}
}

// One server process runs on a home (ADR-062 decision 2, until S4b): a second process on
// the home refuses at once, exit 1, naming the process lock, and writes no status, so
// status.json keeps saying that the first one runs its profile, and aiopsctl submits to
// it.
func TestASecondProcessOnTheHomeRefuses(t *testing.T) {
	h := home(t, "[]")
	out := &readyWriter{ready: make(chan struct{})}
	e, _, sig := env("--profile", "p", "--home", h)
	e.Stdout = out
	done := make(chan int, 1)
	go func() { done <- Run(context.Background(), "aiops", e, distribution(t)) }()
	select {
	case <-out.ready:
	case code := <-done:
		t.Fatalf("the first process exited with %d", code)
	}
	defer func() { sig <- syscall.SIGTERM; <-done }()
	e2, stderr2, sig2 := env("--profile", "other", "--home", h)
	second := make(chan int, 1)
	go func() { second <- Run(context.Background(), "aiops", e2, distribution(t)) }()
	select {
	case code := <-second:
		if code != boot.ExitRefused || !strings.Contains(stderr2.String(), "process.lock") {
			t.Errorf("the second process: exit %d, stderr %q; want exit 1 naming run/process.lock", code, stderr2.String())
		}
	case <-time.After(10 * time.Second):
		sig2 <- syscall.SIGTERM
		<-second
		t.Errorf("a second process ran on the home: %s", stderr2.String())
	}
	if s := readStatus(t, h); s.Phase != "running" || s.Profile != "p" {
		t.Errorf("status.json after the second process: %+v; want the first one's, running p", s)
	}
}

// A snapshot/ that a live write of aiopsctl holds, under the flock of write.lock, is
// that write's, which restores or keeps its files under its lock: a server that
// replaces one killed mid-submission boots, so that the write's await reaches it
// (ADR-062 decision 10). A snapshot that no writer holds is a stopped write's, and the
// server refuses it, naming profile sanitize (ADR-061 decision 17).
func TestRunBootsTheSnapshotOfALiveWrite(t *testing.T) {
	h := home(t, "[]")
	if err := os.MkdirAll(filepath.Join(h, "snapshot"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h, "snapshot", "paths.json"), []byte(`{"Replaced":["profiles/p/aiops.patch.yml"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	e, stderr, _ := env("--profile", "p", "--home", h)
	if code := Run(context.Background(), "aiops", e, distribution(t)); code != boot.ExitRefused || !strings.Contains(stderr.String(), "profile sanitize") {
		t.Fatalf("a snapshot that no writer holds: exit %d, %s; want 1 naming profile sanitize", code, stderr.String())
	}
	lock, err := os.OpenFile(filepath.Join(h, "write.lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err == nil {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	out := &readyWriter{ready: make(chan struct{})}
	e, stderr, sig := env("--profile", "p", "--home", h)
	e.Stdout = out
	done := make(chan int, 1)
	go func() { done <- Run(context.Background(), "aiops", e, distribution(t)) }()
	select {
	case <-out.ready:
		sig <- syscall.SIGTERM
		<-done
	case code := <-done:
		t.Fatalf("a snapshot that a live write holds: exit %d, %s; want the server to boot", code, stderr.String())
	}
}

func TestRunUsesTheHomeFromTheEnvironmentAndShippedFiles(t *testing.T) {
	h := t.TempDir()
	shipped := fstest.MapFS{"profiles/p/profile.yaml": {Data: []byte("bundles: [b]")}, "bundles/b/bundle.yaml": {Data: []byte("[]")}}
	d := distribution(t)
	d.Shipped = shipped
	trusted.Write(t, h, d.Signed)
	e, stderr, _ := env("--profile", "p", "--role", "bogus")
	e.Getenv = func(n string) string {
		if n == "AIOPS_HOME" {
			return h
		}
		return ""
	}
	// A bad role proves the flags were read and the home found, and ends at once.
	if code := Run(context.Background(), "aiops", e, d); code != boot.ExitUsage || !strings.Contains(stderr.String(), `role "bogus"`) {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	// With the shipped files and a patch file by absolute path (a warning for an
	// unknown id), the process reaches ready.
	patch := filepath.Join(h, "extra.yml")
	if err := os.WriteFile(patch, []byte("- {id: nobody, enabled: false}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := &readyWriter{ready: make(chan struct{})}
	e, stderr, sig := env("--profile", "p", "--patch", patch)
	e.Stdout, e.Getenv = out, func(string) string { return h }
	done := make(chan int, 1)
	go func() { done <- Run(context.Background(), "aiops", e, d) }()
	<-out.ready
	sig <- syscall.SIGINT
	if code := <-done; code != boot.ExitOK || !strings.Contains(stderr.String(), "unknown-id") {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
}

func TestRunRelativePatchFile(t *testing.T) {
	h := home(t, "[]")
	e, stderr, _ := env("--profile", "p", "--home", h, "--patch", "no/such/patch.yml")
	if code := Run(context.Background(), "aiops", e, distribution(t)); code != boot.ExitRefused || !strings.Contains(stderr.String(), "patch:no/such/patch.yml") && !strings.Contains(stderr.String(), "no/such/patch.yml") {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
}

func TestRunUsageErrors(t *testing.T) {
	for name, args := range map[string][]string{
		"unknown flag": {"--nope"}, "no profile": {"--home", "x"}, "no home": {"--profile", "p"}, "extra argument": {"--profile", "p", "--home", "x", "y"},
	} {
		e, stderr, _ := env(args...)
		if code := Run(context.Background(), "aiops", e, distribution(t)); code != boot.ExitUsage || !strings.Contains(stderr.String(), "usage: aiops") {
			t.Errorf("%s: exit %d: %s", name, code, stderr.String())
		}
	}
}

// The usage text is the launcher's documentation of its exit codes (ADR-059 decision
// 29), so it lists every code of boot.Run with what it means.
func TestUsageListsTheExitCodes(t *testing.T) {
	for code, what := range map[int]string{
		boot.ExitOK: "OK", boot.ExitRefused: "refused before any row mounted", boot.ExitUsage: "usage",
		boot.ExitRequired: "required set not met", boot.ExitLeak: "leak or shutdown overran",
		boot.ExitGaveUp:   "restart-policy gave up on an infrastructure provider",
		boot.ExitInternal: "launcher failure", boot.ExitRestore: "restore failed",
	} {
		if s := fmt.Sprintf("%d %s", code, what); !strings.Contains(usage, s) {
			t.Errorf("the usage text does not list exit code %q", s)
		}
	}
}

func TestRunLimitsFile(t *testing.T) {
	h := home(t, "[]")
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("loadDeadline: 0s"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{bad, filepath.Join(dir, "missing.yaml")} {
		e, stderr, _ := env("--profile", "p", "--home", h, "--limits", path)
		if code := Run(context.Background(), "aiops", e, distribution(t)); code != boot.ExitRefused || !strings.Contains(stderr.String(), "error:") {
			t.Fatalf("exit %d: %s", code, stderr.String())
		}
	}
	// A good limits file is used: a bound of one second is read without error.
	good := filepath.Join(dir, "good.yaml")
	if err := os.WriteFile(good, []byte("loadDeadline: 1s\nshutdownBound: 5s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := &readyWriter{ready: make(chan struct{})}
	e, _, sig := env("--profile", "p", "--home", h, "--limits", good)
	e.Stdout = out
	done := make(chan int, 1)
	go func() { done <- Run(context.Background(), "aiops", e, distribution(t)) }()
	<-out.ready
	sig <- syscall.SIGTERM
	if code := <-done; code != boot.ExitOK {
		t.Fatalf("exit %d", code)
	}
}

func TestRunRuntimeDirectoryFails(t *testing.T) {
	h := home(t, "[]")
	// aiopsctl creates run/ with the home (ADR-047 decision 15); a file stands there here.
	if err := os.Remove(filepath.Join(h, "run")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h, "run"), nil, 0o644); err != nil { // a file where the directory belongs
		t.Fatal(err)
	}
	e, stderr, _ := env("--profile", "p", "--home", h)
	if code := Run(context.Background(), "aiops", e, distribution(t)); code != boot.ExitInternal || !strings.Contains(stderr.String(), "error:") {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
}

func TestStatusFileFailureIsAWarning(t *testing.T) {
	h := home(t, "[]")
	if err := os.MkdirAll(filepath.Join(h, "run", StatusFile, "x"), 0o755); err != nil { // a directory where the file belongs
		t.Fatal(err)
	}
	out := &readyWriter{ready: make(chan struct{})}
	e, stderr, sig := env("--profile", "p", "--home", h)
	e.Stdout = out
	done := make(chan int, 1)
	go func() { done <- Run(context.Background(), "aiops", e, distribution(t)) }()
	<-out.ready
	sig <- syscall.SIGTERM
	if code := <-done; code != boot.ExitOK || !strings.Contains(stderr.String(), "warning: status file:") {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
}

// stubBoot replaces boot.Run for one test with f.
func stubBoot(t *testing.T, f func(context.Context, boot.RunOptions) int) {
	t.Cleanup(func() { bootRun = boot.Run })
	bootRun = f
}

func TestRunPassesTheRefsDirAsAFlag(t *testing.T) {
	h, dir := home(t, "[]"), t.TempDir()
	var flags map[string]string
	stubBoot(t, func(_ context.Context, o boot.RunOptions) int { flags = o.Flags; return boot.ExitOK })
	e, stderr, _ := env("--profile", "p", "--home", h, "--refs-dir", dir)
	if code := Run(context.Background(), "aiops", e, distribution(t)); code != boot.ExitOK || flags["refs-dir"] != dir {
		t.Fatalf("exit %d, flags %v: %s", code, flags, stderr.String())
	}
}

// The status file carries each row's state and what it waits for (ADR-059 decision 31
// as amended), and leaves the field out before boot has read the rows.
func TestStatusFileCarriesTheRows(t *testing.T) {
	var got map[string]any
	data := statusJSON(boot.ProcessStatus{Phase: boot.PhaseRunning, Rows: []boot.RowStatus{
		{ID: "store", State: boot.StateActive}, {ID: "user", State: boot.StatePending, WaitsFor: []string{"store"}}}})
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	want := []any{map[string]any{"id": "store", "state": "ACTIVE"},
		map[string]any{"id": "user", "state": "PENDING", "waitsFor": []any{"store"}}}
	if !reflect.DeepEqual(got["rows"], want) {
		t.Errorf("rows = %#v, want %#v", got["rows"], want)
	}
	got = nil
	if err := json.Unmarshal(statusJSON(boot.ProcessStatus{Phase: boot.PhaseStarting}), &got); err != nil || got["rows"] != nil {
		t.Errorf("rows before boot read them = %#v, %v", got["rows"], err)
	}
}

// A server hands boot the streams of the process for cmdline, and neither a working
// directory nor an environment (ADR-061 decision 13).
func TestRunPassesTheStreamsAndNoEnviron(t *testing.T) {
	h := home(t, "[]")
	var got boot.RunOptions
	stubBoot(t, func(_ context.Context, o boot.RunOptions) int { got = o; return boot.ExitOK })
	e, stderr, _ := env("--profile", "p", "--home", h)
	e.Stdin = strings.NewReader("in")
	if code := Run(context.Background(), "aiops", e, distribution(t)); code != boot.ExitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if got.Stdio != (boot.Stdio{In: e.Stdin, Out: e.Stdout, Err: e.Stderr}) || got.Dir != "" || got.Environ != nil {
		t.Errorf("Stdio %+v, Dir %q, Environ %q", got.Stdio, got.Dir, got.Environ)
	}
	// The toolbox of a server reads the root of the file system and carries no Tools.
	if got.Tools != nil || got.Files == nil || got.Command {
		t.Errorf("Tools %v, Files %v, Command %v", got.Tools, got.Files, got.Command)
	}
}

// The distribution's Kernel seam reaches boot with the runtime directory of the home;
// with none, boot gets none.
// --refs-dir names an absolute directory that exists; anything else is a usage error,
// given before boot runs and before the status file is written.
func TestRunRefusesABadRefsDir(t *testing.T) {
	h, dir := home(t, "[]"), t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stubBoot(t, func(context.Context, boot.RunOptions) int { t.Error("boot ran"); return boot.ExitOK })
	for name, refs := range map[string]string{"relative": "refs", "missing": filepath.Join(dir, "absent"), "a file": file} {
		e, stderr, _ := env("--profile", "p", "--home", h, "--refs-dir", refs)
		if code := Run(context.Background(), "aiops", e, distribution(t)); code != boot.ExitUsage || !strings.Contains(stderr.String(), "--refs-dir") || !strings.Contains(stderr.String(), "usage: aiops") {
			t.Errorf("%s: exit %d: %s", name, code, stderr.String())
		}
	}
	if _, err := os.Stat(filepath.Join(h, "run", StatusFile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a usage error wrote the status file: %v", err)
	}
}

func TestRunPassesTheKernelSeamWithTheRunDir(t *testing.T) {
	h := home(t, "[]")
	var seam func(*kernel.Root)
	stubBoot(t, func(_ context.Context, o boot.RunOptions) int { seam = o.Kernel; return boot.ExitOK })
	e, stderr, _ := env("--profile", "p", "--home", h)
	if code := Run(context.Background(), "aiops", e, distribution(t)); code != boot.ExitOK || seam != nil {
		t.Fatalf("exit %d, seam set %v: %s", code, seam != nil, stderr.String())
	}
	d, gotDir := distribution(t), ""
	d.Kernel = func(_ *kernel.Root, dir string) { gotDir = dir }
	if code := Run(context.Background(), "aiops", e, d); code != boot.ExitOK || seam == nil {
		t.Fatalf("exit %d, seam set %v: %s", code, seam != nil, stderr.String())
	}
	seam(nil)
	if gotDir != filepath.Join(h, "run") {
		t.Errorf("run dir = %q", gotDir)
	}
}

// staleStatus leaves what a process killed while ready leaves in the home.
func staleStatus(t *testing.T, h string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(h, "run"), 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(Status{Phase: "running", Ready: true, Profile: "p"})
	if err := os.WriteFile(filepath.Join(h, "run", StatusFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunReplacesAStaleStatusBeforeBoot(t *testing.T) {
	h := home(t, "[]")
	staleStatus(t, h)
	var seen Status
	stubBoot(t, func(context.Context, boot.RunOptions) int { seen = readStatus(t, h); return boot.ExitOK })
	e, stderr, _ := env("--profile", "p", "--home", h)
	if code := Run(context.Background(), "aiops", e, distribution(t)); code != boot.ExitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if !reflect.DeepEqual(seen, Status{Phase: "starting", Profile: "p"}) {
		t.Fatalf("status when boot starts %+v", seen)
	}
}

// Only the site user enters run/ (ADR-047 decision 14), which the socket of
// reload-socket relies on (ADR-062 decisions 4 and 16). The home of the test is made
// through the operations library, which creates run/ with mode 0700 itself, so run/ is
// removed first: Run creates it here.
func TestRunCreatesTheRuntimeDirectoryWithMode0700(t *testing.T) {
	h := home(t, "[]")
	if err := os.RemoveAll(filepath.Join(h, "run")); err != nil {
		t.Fatal(err)
	}
	stubBoot(t, func(context.Context, boot.RunOptions) int { return boot.ExitOK })
	e, stderr, _ := env("--profile", "p", "--home", h)
	if code := Run(context.Background(), "aiops", e, distribution(t)); code != boot.ExitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	fi, err := os.Stat(filepath.Join(h, "run"))
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("run/ = %v, %v; want mode 0700", fi.Mode(), err)
	}
}

// The client of reload-socket reads phase and profile of the status file through
// reload.ProcessStatus and imports no package of boot or internal/ (ADR-062 decision
// 8): every status this package writes carries both, with the values boot gave.
func TestEachStatusDecodesIntoTheWireType(t *testing.T) {
	for _, p := range []boot.Phase{boot.PhaseStarting, boot.PhaseRunning, boot.PhaseStopping, boot.PhaseStopped} {
		b := statusJSON(boot.ProcessStatus{Phase: p, Profile: "dev", Role: "api", Ready: true, Exit: 3, Detail: "d"})
		var fields map[string]any
		var s reload.ProcessStatus
		if err := json.Unmarshal(b, &fields); err != nil {
			t.Fatal(err)
		}
		_, hasPhase := fields["phase"]
		_, hasProfile := fields["profile"]
		if err := json.Unmarshal(b, &s); err != nil || !hasPhase || !hasProfile || s != (reload.ProcessStatus{Phase: string(p), Profile: "dev"}) {
			t.Errorf("%s: %s reads as %+v, %v", p, b, s, err)
		}
	}
	if boot.PhaseStopped != reload.PhaseStopped {
		t.Errorf("the stopped phase is %q here and %q on the wire", boot.PhaseStopped, reload.PhaseStopped)
	}
	if StatusFile != reload.StatusFile {
		t.Errorf("status file %q, wire %q", StatusFile, reload.StatusFile)
	}
}

func TestRunLimitsFailureReplacesAStaleStatus(t *testing.T) {
	h := home(t, "[]")
	staleStatus(t, h)
	e, _, _ := env("--profile", "p", "--home", h, "--limits", filepath.Join(t.TempDir(), "typo.yaml"))
	if code := Run(context.Background(), "aiops", e, distribution(t)); code != boot.ExitRefused {
		t.Fatalf("exit %d", code)
	}
	if s := readStatus(t, h); s.Phase != "stopped" || s.Ready || s.Exit != boot.ExitRefused || !strings.Contains(s.Detail, "typo.yaml") {
		t.Fatalf("status %+v", s)
	}
}

func TestOverlayPrefersTheFirstFile(t *testing.T) {
	o := overlay{fstest.MapFS{"a": {Data: []byte("first")}}, fstest.MapFS{"a": {Data: []byte("second")}, "b": {Data: []byte("second")}}}
	for name, want := range map[string]string{"a": "first", "b": "second"} {
		data, err := fsReadFile(o, name)
		if err != nil || string(data) != want {
			t.Fatalf("%s: %q %v", name, data, err)
		}
	}
	if _, err := (overlay{first: fstest.MapFS{}}).Open("x"); err == nil {
		t.Fatal("a missing file with no second file")
	}
}

func fsReadFile(o overlay, name string) ([]byte, error) {
	f, err := o.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var b bytes.Buffer
	_, err = b.ReadFrom(f)
	return b.Bytes(), err
}
