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
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/fstest"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/dist"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
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
	reg, _, lim, err := dist.Load()
	if err != nil {
		t.Fatal(err)
	}
	reg.Modules = boot.ModuleTable{boot.LoaderID: reg.Modules[boot.LoaderID]}
	// The loader reads the role from profileContext, which is consumer-bound.
	man := boot.DistManifest{Consumers: []boot.Binding{{Key: "profileContext", Plugins: []string{boot.LoaderID}}}}
	return Distribution{Registry: reg, Manifest: man, Limits: lim}
}

func env(args ...string) (Env, *safeBuf, chan os.Signal) {
	var stderr safeBuf
	sig := make(chan os.Signal, 1)
	return Env{Args: args, Getenv: func(string) string { return "" }, Stdout: &safeBuf{}, Stderr: &stderr, Signals: sig}, &stderr, sig
}

func home(t *testing.T, bundle string) string {
	t.Helper()
	h := t.TempDir()
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

func TestRunUsesTheHomeFromTheEnvironmentAndShippedFiles(t *testing.T) {
	h := t.TempDir()
	shipped := fstest.MapFS{"profiles/p/profile.yaml": {Data: []byte("bundles: [b]")}, "bundles/b/bundle.yaml": {Data: []byte("[]")}}
	d := distribution(t)
	d.Shipped = shipped
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
	if seen != (Status{Phase: "starting", Profile: "p"}) {
		t.Fatalf("status when boot starts %+v", seen)
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
