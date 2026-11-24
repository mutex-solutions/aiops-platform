// SPDX-License-Identifier: Apache-2.0

package commandprofile_test

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"

	row "github.com/mutex-solutions/aiops-platform/plugins/command-profile"
	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0/fake"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	launchfake "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

var update = flag.Bool("update", false, "rewrite the help goldens")

// box is the toolbox of the tests. Render answers as boot would for the request it gets,
// which it records: a layer file of the home that holds "broken" stops the composition,
// the profile refused holds an error, and each dump names its flag and the profile.
// Shipped holds the profile cli.
type box struct {
	launch.NoTools
	mu   sync.Mutex
	last launch.RenderRequest
}

func (b *box) Render(ctx context.Context, q launch.RenderRequest) (launch.Rendered, error) {
	if err := ctx.Err(); err != nil {
		return launch.Rendered{}, err
	}
	b.mu.Lock()
	b.last = q
	b.mu.Unlock()
	for _, f := range []string{"profiles/" + q.Profile + "/aiops.patch.yml", "aiops.patch.yml"} {
		if data, err := os.ReadFile(filepath.Join(q.Home, f)); q.Home != "" && err == nil && strings.TrimSpace(string(data)) == "broken" {
			return launch.Rendered{Diagnostics: []launch.Diagnostic{{Check: "compose", File: f, Line: 1, Text: f + ":1: syntax: broken"}}}, nil
		}
	}
	r := launch.Rendered{Departures: []string{"the site adds consumers of appExit: acme/x"}, Dumps: map[string]string{},
		Diagnostics: []launch.Diagnostic{{Text: "trust: the test trusts the distribution", Warning: true}}}
	if q.Profile == "refused" {
		r.Diagnostics = append(r.Diagnostics, launch.Diagnostic{Text: `denied: row "x"`})
	}
	for _, d := range q.Dumps {
		r.Dumps[d] = strings.TrimPrefix(d, "--") + " of " + q.Profile + "\n"
	}
	return r, nil
}

func (b *box) request() launch.RenderRequest {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.last
}

func (*box) Gate(context.Context, launch.GateRequest) (launch.Verdict, error) {
	return launch.Verdict{}, launch.ErrNotCarried
}

func (*box) ReadManifest(context.Context, launch.ManifestRequest) (launch.ManifestSummary, []launch.Diagnostic, error) {
	return launch.ManifestSummary{}, nil, launch.ErrNotCarried
}

func (*box) Release(context.Context, string) (launch.Release, error) {
	return launch.Release{}, launch.ErrNotCarried
}

const cliProfile = "bundles: [aiops-core, aiops-cli]\n"

func (*box) Shipped(ctx context.Context) (map[string][]byte, error) {
	return map[string][]byte{"profiles/cli/profile.yaml": []byte(cliProfile)}, ctx.Err()
}

// mountRow mounts the launch fake with b and the row on a root of its own.
func mountRow(t *testing.T, b *box) (*kernel.Root, *kernel.Fiber) {
	root := kerneltest.New(t)
	l, lp := launchfake.New()
	l.Box = b
	kerneltest.Mount(root, lp)
	return root, root.Plugin(row.Plugin, nil)
}

// The row keeps the obligations of a command on the cases of its fixtures (ADR-061
// decision 5).
func TestObligations(t *testing.T) {
	conformance.Run(t, func(t *testing.T) (conformance.Subject, *kernel.Root) {
		root, f := mountRow(t, &box{})
		return conformance.Contribution(f, "fixtures"), root
	})
}

// The help of each command is the CLI spec (ADR-061 decision 30).
func TestHelp(t *testing.T) {
	conformance.Help(t, func(t *testing.T) (conformance.Subject, *kernel.Root) {
		root, f := mountRow(t, &box{})
		return conformance.Contribution(f, "fixtures"), root
	}, *update)
}

// world is the row mounted with the recording fake, in the working directory /work.
type world struct {
	b        *box
	rec      *fake.Registry
	out, err bytes.Buffer
}

func newWorld(t *testing.T, home string) *world {
	w := &world{b: &box{}}
	root, _ := mountRow(t, w.b)
	var p kernel.Plugin
	w.rec, p = fake.New()
	w.rec.Dir, w.rec.Home = "/work", home
	kerneltest.Mount(root, p)
	if err := root.Settle(t.Context()); err != nil {
		t.Fatal(err)
	}
	return w
}

func (w *world) run(ctx context.Context, argv ...string) int {
	w.out.Reset()
	w.err.Reset()
	code, _ := w.rec.Dispatch(ctx, argv, commands.Streams{Out: &w.out, Err: &w.err})
	return code
}

// profile render resolves each relative path against the working directory, reads
// --dist as the directory of distribution.yaml, and takes the home from the invocation
// unless --home names one (ADR-061 decisions 2 and 27).
func TestRenderResolvesPaths(t *testing.T) {
	w := newWorld(t, "/home")
	if code := w.run(t.Context(), "profile", "render", "--profile", "demo", "--bundles", "b", "--patch", "p.yml", "--patch", "/abs.yml",
		"--dist", "d", "--server", "--role", "api,worker", "--role", "edge"); code != 0 {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	want := launch.RenderRequest{Home: "/home", Profile: "demo", Bundles: "/work/b", Patches: []string{"/work/p.yml", "/abs.yml"},
		Roles: []string{"api", "worker", "edge"}, Server: true, Against: "/work/d/distribution.yaml"}
	if got := w.b.request(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("request %+v, want %+v", got, want)
	}
	w.run(t.Context(), "profile", "render", "--profile", "demo", "--home", "h")
	if got := w.b.request(); got.Home != "/work/h" || got.Bundles != "" || got.Against != "" {
		t.Errorf("request %+v", got)
	}
}

func mode(t *testing.T, p string) os.FileMode {
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

// profile init copies the shipped template into the home under the write lock, with
// mode 0600 in directories of mode 0700, keeps a profile the home holds, and answers
// its outcome: restart-required with the reason not-submitted while a process of the
// profile runs, since version 1 of the wire of ADR-062 carries no profile init (ADR-061
// decisions 17 and 19; ADR-060 decision 27).
func TestInitWritesTheHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	w := newWorld(t, home)
	if code := w.run(t.Context(), "profile", "init", "cli"); code != 0 || !strings.Contains(w.err.String(), "not-running") {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	file := filepath.Join(home, "profiles", "cli", "profile.yaml")
	if data, err := os.ReadFile(file); err != nil || string(data) != cliProfile {
		t.Fatalf("%s: %q, %v", file, data, err)
	}
	lock, err := os.ReadFile(filepath.Join(home, "write.lock"))
	if err != nil || !strings.HasPrefix(string(lock), fmt.Sprint(os.Getpid())+" profile init") {
		t.Errorf("write.lock holds %q, %v", lock, err)
	}
	if m := [4]os.FileMode{mode(t, file), mode(t, filepath.Dir(file)), mode(t, home), mode(t, filepath.Join(home, "write.lock"))}; m != [4]os.FileMode{0o600, 0o700, 0o700, 0o600} {
		t.Errorf("modes %v", m)
	}
	if err := os.WriteFile(file, []byte("bundles: [mine]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "run"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "run", "status.json"), []byte(`{"phase": "running", "profile": "cli"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := w.run(t.Context(), "profile", "init", "cli"); code != 0 || !strings.Contains(w.err.String(), "kept") {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if data, _ := os.ReadFile(file); string(data) != "bundles: [mine]\n" {
		t.Errorf("init changed a profile the home holds: %q", data)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	w.err.Reset()
	if code := w.run(t.Context(), "profile", "init", "cli"); code != 0 || !strings.Contains(w.err.String(), "restart-required: not-submitted") {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if err := os.WriteFile(file, []byte("bundles: [mine]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := w.run(t.Context(), "profile", "sanitize"); code != 0 || !strings.Contains(w.err.String(), "no layer file") {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
}

// A command that writes the home is a usage error without one, and names the variable
// (ADR-061 decision 17).
func TestWritesNeedAHome(t *testing.T) {
	w := newWorld(t, "")
	for _, argv := range [][]string{{"profile", "init", "cli"}, {"profile", "sanitize"}} {
		if code := w.run(t.Context(), argv...); code != 2 || !strings.Contains(w.err.String(), "AIOPS_HOME") {
			t.Errorf("%q: exit %d: %s", argv, code, w.err.String())
		}
	}
	if code := w.run(t.Context(), "profile", "init", "../x"); code != 2 {
		t.Errorf("a profile name that leaves its directory: exit %d", code)
	}
}

// profile sanitize renames each layer file of the home that stops the composition, the
// profile layer of the profile it names and the site layer, to <name>.bak-<timestamp>,
// and then the profile composes (ADR-061 decision 41; dsh §3.4).
func TestSanitizeSetsBrokenLayersAside(t *testing.T) {
	home := t.TempDir()
	for _, f := range []string{"aiops.patch.yml", "profiles/dev/aiops.patch.yml"} {
		p := filepath.Join(home, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("broken\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	w := newWorld(t, home)
	if code := w.run(t.Context(), "profile", "sanitize", "dev"); code != 0 {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	for _, f := range []string{"aiops.patch.yml", "profiles/dev/aiops.patch.yml"} {
		baks, _ := filepath.Glob(filepath.Join(home, f+".bak-*"))
		if _, err := os.Stat(filepath.Join(home, f)); err == nil || len(baks) != 1 || !strings.Contains(w.err.String(), f) {
			t.Errorf("%s: kept %v, set aside as %v; stderr %s", f, err == nil, baks, w.err.String())
		}
	}
	if code := w.run(t.Context(), "profile", "sanitize", "dev"); code != 0 || !strings.Contains(w.err.String(), "no layer file") {
		t.Errorf("a second sanitize: exit %d: %s", code, w.err.String())
	}
}

// A write waits for the write lock of the home: while another holder has it, the
// profile is not written, and once it is released the write goes on (ADR-061 decision
// 19).
func TestInitWaitsForTheWriteLock(t *testing.T) {
	home := t.TempDir()
	f, err := os.OpenFile(filepath.Join(home, "write.lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	w := newWorld(t, home)
	done := make(chan int, 1)
	go func() {
		code, _ := w.rec.Dispatch(t.Context(), []string{"profile", "init", "cli"}, commands.Streams{})
		done <- code
	}()
	file := filepath.Join(home, "profiles", "cli", "profile.yaml")
	if _, err := os.Stat(file); err == nil {
		t.Fatal("init wrote the profile while another holder had the write lock")
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if code := <-done; code != 0 {
		t.Fatalf("exit %d once the lock was released", code)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal(err)
	}
}

// The row registers its four paths and needs commands and profileContext.
func TestRegistersFourPaths(t *testing.T) {
	w := newWorld(t, "")
	if got := w.rec.Inventory(); !slices.Equal(got, []string{"profile init", "profile render", "profile sanitize", "profile verify"}) {
		t.Fatalf("Inventory = %q", got)
	}
}
