// SPDX-License-Identifier: Apache-2.0

package commandprofile_test

import (
	"bytes"
	"cmp"
	"context"
	"flag"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	row "github.com/mutex-solutions/aiops-platform/plugins/command-profile"
	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0/fake"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	operations "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

var update = flag.Bool("update", false, "rewrite the help goldens")

// stub provides operations, the one key the row calls besides commands (ADR-061
// decision 20). Render and VerifyProfile record each request and answer as the library
// would for it: the profile "refused" holds an error, the profile "stop" stops on a
// layer file, and each dump names its flag and the profile. InitProfile copies the
// profile cli once and refuses any other; SanitizeProfile finds nothing to set aside.
// Each needs a home. err, when set, is every answer.
type stub struct {
	operations.Operations
	err error

	mu       sync.Mutex
	renders  []operations.RenderRequest
	profiles []operations.ProfileRequest
	held     map[string]bool
}

func (s *stub) Render(ctx context.Context, r operations.RenderRequest) (launch.Rendered, error) {
	s.mu.Lock()
	s.renders = append(s.renders, r)
	s.mu.Unlock()
	if s.err != nil {
		return launch.Rendered{}, s.err
	}
	if r.Profile == "stop" {
		f := "profiles/stop/aiops.patch.yml"
		return launch.Rendered{Diagnostics: []launch.Diagnostic{{Check: "compose", File: f, Line: 1, Text: f + ":1: syntax: broken"}}}, nil
	}
	out := launch.Rendered{Departures: []string{"the site adds consumers of appExit: acme/x"}, Dumps: map[string]string{},
		Diagnostics: []launch.Diagnostic{{Text: "trust: the test trusts the distribution", Warning: true}}}
	if r.Profile == "refused" {
		out.Diagnostics = append(out.Diagnostics, launch.Diagnostic{Text: `denied: row "x"`})
	}
	for _, d := range r.Dumps {
		out.Dumps[d] = strings.TrimPrefix(d, "--") + " of " + r.Profile + "\n"
	}
	return out, ctx.Err()
}

func (s *stub) VerifyProfile(ctx context.Context, r operations.RenderRequest) (launch.Rendered, error) {
	r.Profile = cmp.Or(r.Profile, "cli")
	return s.Render(ctx, r)
}

func (s *stub) profile(r operations.ProfileRequest) error {
	s.mu.Lock()
	s.profiles = append(s.profiles, r)
	s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if r.Home == "" {
		return fmt.Errorf("%w: no home: %s is unset or empty", operations.ErrUsage, operations.HomeVar)
	}
	return nil
}

func (s *stub) InitProfile(ctx context.Context, r operations.ProfileRequest) (operations.Change, error) {
	if err := s.profile(r); err != nil {
		return operations.Change{}, err
	}
	if r.Profile != "cli" {
		return operations.Change{}, fmt.Errorf("%w: no shipped profile %s", operations.ErrRefused, r.Profile)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.held[r.Home] {
		return operations.Change{Notes: []string{"the home holds profiles/cli/profile.yaml, which is kept"}}, ctx.Err()
	}
	s.held[r.Home] = true
	return operations.Change{Written: []string{r.Home + "/profiles/cli/profile.yaml"}, Outcome: operations.NotRunning,
		Notes: []string{"copied the shipped profile cli to profiles/cli/profile.yaml"}}, ctx.Err()
}

func (s *stub) SanitizeProfile(ctx context.Context, r operations.ProfileRequest) (operations.Change, error) {
	if err := s.profile(r); err != nil {
		return operations.Change{}, err
	}
	return operations.Change{Notes: []string{"no layer file of the home stops the composition of " + cmp.Or(r.Profile, "cli")}}, ctx.Err()
}

func (s *stub) lastRender() operations.RenderRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.renders[len(s.renders)-1]
}

func (s *stub) lastProfile() operations.ProfileRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.profiles[len(s.profiles)-1]
}

// mountRow mounts s as the provider of operations and the row on a root of its own,
// with no launch key: the row reaches the toolbox only through operations.
func mountRow(t *testing.T, s *stub) (*kernel.Root, *kernel.Fiber) {
	if s.held == nil {
		s.held = map[string]bool{}
	}
	root := kerneltest.New(t)
	kerneltest.Mount(root, kernel.Define(kernel.Plugin{ID: "test/operations", Provides: []kernel.AnyKey{operations.Key},
		Apply: func(c *kernel.Context, _ any) error { return operations.Key.Provide(c, s) }}))
	return root, root.Plugin(row.Plugin, nil)
}

func mount(t *testing.T) (conformance.Subject, *kernel.Root) {
	root, f := mountRow(t, &stub{})
	return conformance.Contribution(f, "fixtures"), root
}

// The row keeps the obligations of a command on the cases of its fixtures (ADR-061
// decision 5).
func TestObligations(t *testing.T) { conformance.Run(t, mount) }

// The help of each command is the CLI spec (ADR-061 decision 30).
func TestHelp(t *testing.T) { conformance.Help(t, mount, *update) }

// world is the row mounted on s with the recording dispatcher, in the working directory
// /work.
type world struct {
	s        *stub
	rec      *fake.Registry
	out, err bytes.Buffer
}

func newWorld(t *testing.T, s *stub, home string) *world {
	w := &world{s: s}
	root, _ := mountRow(t, s)
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

// profile render calls Render with each relative path resolved against the working
// directory but the patches, which it passes as written with the working directory, so
// a dump names a relative patch as the operator wrote it; --dist as the directory of
// distribution.yaml, the home of the invocation unless --home names one, and the
// environment of the invocation (ADR-061 decisions 2, 20 and 27).
func TestRenderResolvesPaths(t *testing.T) {
	w := newWorld(t, &stub{}, "/home")
	if code := w.run(t.Context(), "profile", "render", "--profile", "demo", "--bundles", "b", "--patch", "p.yml", "--patch", "/abs.yml",
		"--dist", "d", "--server", "--role", "api,worker", "--role", "edge"); code != 0 {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	want := launch.RenderRequest{Home: "/home", Profile: "demo", Bundles: "/work/b", Patches: []string{"p.yml", "/abs.yml"},
		Roles: []string{"api", "worker", "edge"}, Server: true, Against: "/work/d/distribution.yaml", Dir: "/work"}
	got := w.s.lastRender()
	if fmt.Sprint(got.RenderRequest) != fmt.Sprint(want) || got.Env == nil {
		t.Errorf("request %+v, want %+v and the environment", got, want)
	}
	w.run(t.Context(), "profile", "render", "--profile", "demo", "--home", "h")
	if got := w.s.lastRender(); got.Home != "/work/h" || got.Bundles != "" || got.Against != "" {
		t.Errorf("request %+v", got)
	}
	if code := w.run(t.Context(), "profile", "verify"); code != 0 || w.out.String() != "the site adds consumers of appExit: acme/x\n" {
		t.Errorf("profile verify: exit %d, stdout %q", code, w.out.String())
	}
	if got := w.s.lastRender(); got.Home != "/home" || got.Profile != "cli" || got.Dumps != nil {
		t.Errorf("verify request %+v; want the home and cli, no dump", got)
	}
}

// profile init and profile sanitize call the library with the home, the profile and the
// environment of the invocation, print its notes and the outcome on stderr and nothing
// on stdout (ADR-061 decisions 17 and 41).
func TestInitAndSanitizeCallTheLibrary(t *testing.T) {
	w := newWorld(t, &stub{}, "/home")
	if code := w.run(t.Context(), "profile", "init", "cli"); code != 0 || w.out.String() != "" ||
		w.err.String() != "copied the shipped profile cli to profiles/cli/profile.yaml\nnot-running: the change applies at the next start\n" {
		t.Errorf("profile init cli: exit %d, stdout %q, stderr %q", code, w.out.String(), w.err.String())
	}
	if r := w.s.lastProfile(); r.Home != "/home" || r.Profile != "cli" || r.Env == nil {
		t.Errorf("request %+v", r)
	}
	if code := w.run(t.Context(), "profile", "init", "cli"); code != 0 || w.err.String() != "the home holds profiles/cli/profile.yaml, which is kept\n" {
		t.Errorf("a second profile init cli: exit %d, stderr %q", code, w.err.String())
	}
	if code := w.run(t.Context(), "profile", "sanitize", "dev"); code != 0 || !strings.Contains(w.err.String(), "composition of dev") {
		t.Errorf("profile sanitize dev: exit %d, stderr %q", code, w.err.String())
	}
	if r := w.s.lastProfile(); r.Home != "/home" || r.Profile != "dev" {
		t.Errorf("request %+v", r)
	}
	if w.run(t.Context(), "profile", "sanitize"); w.s.lastProfile().Profile != "" {
		t.Errorf("sanitize with no profile asks for %q; the library takes cli", w.s.lastProfile().Profile)
	}
}

// A usage error of the library, such as no home, exits 2 and names the variable; a
// refusal or a held lock exits 1 with its reason (ADR-061 decisions 10 and 17).
func TestLibraryErrorsExit(t *testing.T) {
	w := newWorld(t, &stub{}, "")
	for _, argv := range [][]string{{"profile", "init", "cli"}, {"profile", "sanitize"}} {
		if code := w.run(t.Context(), argv...); code != 2 || !strings.Contains(w.err.String(), "AIOPS_HOME") {
			t.Errorf("%q: exit %d: %s", argv, code, w.err.String())
		}
	}
	for _, c := range []struct {
		err  error
		code int
	}{
		{fmt.Errorf("%w: \"../x\" is not the name of a profile", operations.ErrUsage), 2},
		{fmt.Errorf("%w: no shipped profile x", operations.ErrRefused), 1},
		{fmt.Errorf("%w: by process 7 (trust add)", operations.ErrLocked), 1},
	} {
		w := newWorld(t, &stub{err: c.err}, "/home")
		for _, argv := range [][]string{{"profile", "init", "x"}, {"profile", "sanitize"}, {"profile", "render", "--profile", "x"}, {"profile", "verify"}} {
			if code := w.run(t.Context(), argv...); code != c.code || w.out.String() != "" || !strings.Contains(w.err.String(), c.err.Error()) {
				t.Errorf("%q with %v: exit %d, stdout %q, stderr %q", argv, c.err, code, w.out.String(), w.err.String())
			}
		}
	}
}

// The row registers its four paths and needs commands and operations, and no launch
// key (ADR-061 decision 20).
func TestRegistersFourPaths(t *testing.T) {
	w := newWorld(t, &stub{}, "")
	if got := w.rec.Inventory(); !slices.Equal(got, []string{"profile init", "profile render", "profile sanitize", "profile verify"}) {
		t.Fatalf("Inventory = %q", got)
	}
}
