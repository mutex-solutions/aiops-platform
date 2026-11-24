// SPDX-License-Identifier: Apache-2.0

package commandplugin_test

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

	row "github.com/mutex-solutions/aiops-platform/plugins/command-plugin"
	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0/fake"
	operations "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

var update = flag.Bool("update", false, "rewrite the help goldens")

// stub provides operations: the five methods of the artifact record each request and
// answer the values of the stub, or err.
type stub struct {
	operations.Operations
	in  operations.Inspected
	v   operations.Verified
	err error

	mu   sync.Mutex
	reqs []any
}

func (s *stub) record(ctx context.Context, r any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = append(s.reqs, r)
	return cmp.Or(ctx.Err(), s.err)
}

func (s *stub) last() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.reqs) == 0 {
		return operations.SpecRequest{Spec: "no request"}
	}
	return s.reqs[len(s.reqs)-1]
}

func (s *stub) Pack(ctx context.Context, r operations.PackRequest) (operations.Written, error) {
	return operations.Written{Paths: []string{r.Out}}, s.record(ctx, r)
}

func (s *stub) Sign(ctx context.Context, r operations.SignRequest) (operations.Written, error) {
	return operations.Written{Paths: []string{r.Out}}, s.record(ctx, r)
}

func (s *stub) Push(ctx context.Context, r operations.PushRequest) (operations.Pushed, error) {
	return operations.Pushed{Spec: "oci://" + r.Registry + "@sha256:ab"}, s.record(ctx, r)
}

func (s *stub) Inspect(ctx context.Context, r operations.SpecRequest) (operations.Inspected, error) {
	return s.in, s.record(ctx, r)
}

func (s *stub) Verify(ctx context.Context, r operations.SpecRequest) (operations.Verified, error) {
	return s.v, s.record(ctx, r)
}

// mountRow mounts s as the provider of operations and the row on a root of its own.
func mountRow(t *testing.T, s *stub) (*kernel.Root, *kernel.Fiber) {
	root := kerneltest.New(t)
	kerneltest.Mount(root, kernel.Define(kernel.Plugin{ID: "test/operations", Provides: []kernel.AnyKey{operations.Key},
		Apply: func(c *kernel.Context, _ any) error { return operations.Key.Provide(c, s) }}))
	return root, root.Plugin(row.Plugin, nil)
}

func mount(t *testing.T) (conformance.Subject, *kernel.Root) {
	root, f := mountRow(t, &stub{})
	return conformance.Contribution(f, "fixtures"), root
}

// The row registers the paths that fixtures/commands/paths.json lists, which the name
// lint reads (ADR-061 decision 5). Most of its commands are not built yet, so
// commands/run and commands/cancel wait for them.
func TestPaths(t *testing.T) { conformance.Paths(t, mount) }

// The help of each command is the CLI spec (ADR-061 decision 30).
func TestHelp(t *testing.T) { conformance.Help(t, mount, *update) }

// world is the row mounted on s, with the recording dispatcher in the working directory
// /work and the home /site.
func world(t *testing.T, s *stub) *fake.Registry {
	root, _ := mountRow(t, s)
	rec, p := fake.New()
	rec.Dir, rec.Home = "/work", "/site"
	kerneltest.Mount(root, p)
	if err := root.Settle(t.Context()); err != nil {
		t.Fatal(err)
	}
	return rec
}

func dispatch(t *testing.T, rec *fake.Registry, argv ...string) (code int, out, errOut string) {
	var o, e bytes.Buffer
	code, _ = rec.Dispatch(t.Context(), argv, commands.Streams{Out: &o, Err: &e})
	return code, o.String(), e.String()
}

// built are the commands that run through the operations library (ADR-061 decision
// 16; ADR-047 decision 1).
var built = []string{"plugin inspect", "plugin pack", "plugin push", "plugin sign", "plugin verify"}

// Every other command answers that it is not built yet, with exit 1, on a command line
// that its spec reads.
func TestNotBuiltYet(t *testing.T) {
	rec := world(t, &stub{})
	for _, name := range rec.Inventory() {
		if slices.Contains(built, name) {
			continue
		}
		s, _ := rec.Spec(name)
		argv := s.Path
		for range s.Min {
			argv = append(argv, "x")
		}
		for _, f := range s.Flags {
			if f.Required {
				argv = append(argv, "--"+f.Name, "x")
			}
		}
		if code, _, errOut := dispatch(t, rec, argv...); code != 1 || !strings.Contains(errOut, name+" is not built yet") {
			t.Errorf("%q: exit %d, stderr %q", argv, code, errOut)
		}
	}
}

// pack, sign and push call the library with the plugin directory, the layout and the
// key file resolved against the working directory, the layout artifact/ of the
// directory by default (ADR-047 decision 1), and the environment of the invocation,
// from which the library reads the key password and the registry credentials. pack and
// sign name the layout on stderr; push prints the spec by digest on stdout (ADR-061
// decisions 11 and 30).
func TestPackSignAndPushCallTheLibrary(t *testing.T) {
	s := &stub{}
	rec := world(t, s)
	for _, c := range []struct {
		argv     []string
		want     any
		out, err string
	}{
		{[]string{"plugin", "pack"}, operations.PackRequest{Dir: "/work", Out: "/work/artifact"}, "", "wrote /work/artifact"},
		{[]string{"plugin", "pack", "x", "--out", "/layouts/x"}, operations.PackRequest{Dir: "/work/x", Out: "/layouts/x"}, "", "wrote /layouts/x"},
		{[]string{"plugin", "sign", "--key", "a.key"}, operations.SignRequest{Out: "/work/artifact", Key: "/work/a.key"}, "", "signed /work/artifact"},
		{[]string{"plugin", "push", "registry.example/acme/x", "--out", "o"}, operations.PushRequest{Out: "/work/o", Registry: "registry.example/acme/x"},
			"oci://registry.example/acme/x@sha256:ab\n", ""},
	} {
		code, out, errOut := dispatch(t, rec, c.argv...)
		if code != 0 || out != c.out || !strings.Contains(errOut, c.err) {
			t.Errorf("%q: exit %d, stdout %q, stderr %q", c.argv, code, out, errOut)
		}
		if got := fmt.Sprintf("%+v", strip(s.last())); got != fmt.Sprintf("%+v", c.want) {
			t.Errorf("%q: request %s, want %+v", c.argv, got, c.want)
		}
	}
}

// strip drops the caller of a request, after checking that it carries the environment.
func strip(r any) any {
	switch q := r.(type) {
	case operations.PackRequest:
		if q.Env != nil {
			q.Caller = operations.Caller{}
		}
		return q
	case operations.SignRequest:
		if q.Env != nil {
			q.Caller = operations.Caller{}
		}
		return q
	case operations.PushRequest:
		if q.Env != nil {
			q.Caller = operations.Caller{}
		}
		return q
	case operations.SpecRequest:
		if q.Env != nil {
			q.Caller = operations.Caller{}
		}
		return q
	}
	return r
}

// inspect prints what the library read of a spec, a layout resolved against the
// working directory or an oci:// spec as written, on stdout, with the line that no
// signer was verified (ADR-047 decision 2), and exits 0.
func TestInspectPrintsTheArtifact(t *testing.T) {
	s := &stub{in: operations.Inspected{Digest: "sha256:ab", ID: "acme/x", Version: "0.1.0",
		Rows:    []operations.Row{{ID: "logs", Plugin: "aiops/echoer", Config: map[string]any{"endpoint": "http://x.example.test"}}},
		Content: []string{"labels.yaml"}}}
	rec := world(t, s)
	for spec, want := range map[string]string{"artifact": "/work/artifact", "oci://registry.example/acme/x:0.1.0": "oci://registry.example/acme/x:0.1.0"} {
		code, out, errOut := dispatch(t, rec, "plugin", "inspect", spec)
		if q := strip(s.last()).(operations.SpecRequest); code != 0 || errOut != "" || q.Spec != want {
			t.Errorf("inspect %s: exit %d, stderr %q, request %+v", spec, code, errOut, q)
		}
		for _, line := range []string{"digest: sha256:ab", "id: acme/x", "version: 0.1.0", "runtimes: []", "signer: not verified",
			`row logs: plugin aiops/echoer, config {"endpoint":"http://x.example.test"}`, "content: labels.yaml"} {
			if !strings.Contains(out, line+"\n") {
				t.Errorf("inspect %s: stdout %q lacks %q", spec, out, line)
			}
		}
	}
}

// verify prints each step on stdout and exits 0 when every step passed, and 1 naming
// the steps that did not, the suites not built among them (ADR-060 decision 46); it
// passes the home and the builder to the library, which refuses with exit 1.
func TestVerifyPrintsTheSteps(t *testing.T) {
	steps := []operations.VerifyStep{{Name: "signature", Outcome: "pass", Reason: "signer k"}, {Name: "attestation", Outcome: "pass"},
		{Name: "suites", Outcome: "not-built", Reason: "package check is not built yet"}}
	s := &stub{v: operations.Verified{Digest: "sha256:ab", Signer: "k", Steps: steps}}
	rec := world(t, s)
	code, out, errOut := dispatch(t, rec, "plugin", "verify", "oci://registry.example/acme/x@sha256:ab")
	if code != 1 || !strings.Contains(out, "step suites: not-built: package check is not built yet\n") || !strings.Contains(out, "signer: k\n") ||
		!strings.Contains(errOut, "suites") {
		t.Errorf("verify with the suites not built: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if q := strip(s.last()).(operations.SpecRequest); q.Home != "/site" || q.Builder != "image" || q.Spec != "oci://registry.example/acme/x@sha256:ab" {
		t.Errorf("request %+v", q)
	}
	s.v.Steps[2].Outcome, s.v.Passed = "pass", true
	if code, _, errOut := dispatch(t, rec, "plugin", "verify", "artifact", "--builder", "local"); code != 0 || errOut != "" {
		t.Errorf("verify that passes: exit %d, stderr %q", code, errOut)
	}
	s.err = fmt.Errorf("%w: check 5: no signature verifies", operations.ErrRefused)
	if code, out, errOut := dispatch(t, rec, "plugin", "verify", "artifact"); code != 1 || out != "" || !strings.Contains(errOut, "check 5") {
		t.Errorf("a refusal: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

// A usage error of the library exits 2, and a refusal 1 with its reason (ADR-061
// decision 10).
func TestExits(t *testing.T) {
	for _, c := range []struct {
		err  error
		code int
	}{
		{fmt.Errorf("%w: AIOPS_KEY_PASSWORD is unset", operations.ErrUsage), 2},
		{fmt.Errorf("%w: the layout is not empty", operations.ErrRefused), 1},
	} {
		rec := world(t, &stub{err: c.err})
		for _, argv := range [][]string{{"plugin", "pack"}, {"plugin", "sign", "--key", "k"}, {"plugin", "push", "r.example/x"}, {"plugin", "inspect", "a"}} {
			if code, out, errOut := dispatch(t, rec, argv...); code != c.code || out != "" || !strings.Contains(errOut, c.err.Error()) {
				t.Errorf("%q with %v: exit %d, stdout %q, stderr %q", argv, c.err, code, out, errOut)
			}
		}
	}
}
