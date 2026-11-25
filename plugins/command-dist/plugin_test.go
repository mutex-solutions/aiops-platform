// SPDX-License-Identifier: Apache-2.0

package commanddist_test

import (
	"bytes"
	"cmp"
	"context"
	"flag"
	"fmt"
	"strings"
	"sync"
	"testing"

	row "github.com/mutex-solutions/aiops-platform/plugins/command-dist"
	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0/fake"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	operations "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

var update = flag.Bool("update", false, "rewrite the help goldens")

// stub provides operations: SignDist, UseDist and Build record each request and answer
// w, ch, built and err, so the tests read what the row asks of the library without a
// key, an image or a toolchain.
type stub struct {
	operations.Operations
	w   operations.Written
	ch  operations.Change
	err error

	built launch.Built

	mu     sync.Mutex
	signs  []operations.SignDistRequest
	uses   []operations.UseRequest
	builds []operations.BuildRequest
}

func (s *stub) Build(ctx context.Context, r operations.BuildRequest) (launch.Built, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.builds = append(s.builds, r)
	return s.built, cmp.Or(ctx.Err(), s.err)
}

func (s *stub) SignDist(ctx context.Context, r operations.SignDistRequest) (operations.Written, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.signs = append(s.signs, r)
	return s.w, cmp.Or(ctx.Err(), s.err)
}

func (s *stub) UseDist(ctx context.Context, r operations.UseRequest) (operations.Change, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.uses = append(s.uses, r)
	return s.ch, cmp.Or(ctx.Err(), s.err)
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

// The row keeps the obligations of a command on the cases of its fixtures, whose
// paths.json the name lint reads (ADR-061 decision 5); the stub of the library answers
// each of its three commands.
func TestObligations(t *testing.T) { conformance.Run(t, mount) }

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

// dist sign calls SignDist with the image reference as written and the key file
// resolved against the working directory, and the environment of the invocation, from
// which the library reads the key password and the registry credentials; dist use
// calls UseDist with the home and the image. dist sign names the referrer it pushed,
// dist use what it wrote and the outcome, on stderr; each prints nothing on stdout and
// exits 0 (ADR-061 decisions 16, 20 and 30; ADR-020 decision 12).
func TestSignAndUseCallTheLibrary(t *testing.T) {
	s := &stub{w: operations.Written{Paths: []string{"registry.example/site@sha256:cd"}},
		ch: operations.Change{Written: []string{"/site/verified/sha256-ab", "/site/verified/current", "/site/image.yaml"}, Outcome: operations.RestartRequired, Reason: operations.ReasonImage}}
	rec := world(t, s)
	code, out, errOut := dispatch(t, rec, "dist", "sign", "registry.example/site:1", "--key", "site.key")
	if code != 0 || out != "" || !strings.Contains(errOut, "pushed registry.example/site@sha256:cd") {
		t.Errorf("dist sign: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if r := s.signs[len(s.signs)-1]; r.Image != "registry.example/site:1" || r.Key != "/work/site.key" || r.Env == nil {
		t.Errorf("request %+v", r)
	}
	code, out, errOut = dispatch(t, rec, "dist", "use", "oci://registry.example/site:1")
	if code != 0 || out != "" || !strings.Contains(errOut, "wrote /site/image.yaml") || !strings.Contains(errOut, "restart-required: image") {
		t.Errorf("dist use: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if r := s.uses[len(s.uses)-1]; r.Home != "/site" || r.Image != "oci://registry.example/site:1" || r.Env == nil {
		t.Errorf("request %+v", r)
	}
}

// A refusal of the library exits 1 with its reason, and a usage error of the library,
// such as no home or no key password, exits 2 (ADR-061 decision 10).
func TestSignAndUseExits(t *testing.T) {
	for _, c := range []struct {
		err  error
		code int
	}{
		{fmt.Errorf("%w: no key of distributions verifies the signature", operations.ErrRefused), 1},
		{fmt.Errorf("%w: AIOPS_KEY_PASSWORD is unset", operations.ErrUsage), 2},
	} {
		rec := world(t, &stub{err: c.err})
		for _, argv := range [][]string{{"dist", "sign", "img", "--key", "k"}, {"dist", "use", "img"}} {
			if code, out, errOut := dispatch(t, rec, argv...); code != c.code || out != "" || !strings.Contains(errOut, c.err.Error()) {
				t.Errorf("%q with %v: exit %d, stdout %q, stderr %q", argv, c.err, code, out, errOut)
			}
		}
	}
}

// dist build calls Build with dist.yaml and the module cache resolved against the
// working directory, each --mirror as a prefix and its site prefix, each --allow-bump and
// the builder, and the environment of the invocation; it names what the builder wrote
// and the digest of the signed manifest on stderr, prints nothing on stdout and exits
// 0. A malformed --mirror is a usage error, and a refusal of the builder exits 1 with
// its reason (ADR-061 decisions 10 and 30; ADR-044 decisions 7, 9 and 10).
func TestBuildCallsTheLibrary(t *testing.T) {
	s := &stub{built: launch.Built{Image: "/work/build/image", Manifest: []byte("distVersion: 1\n"), Aiopsctl: "/work/build/aiopsctl/aiopsctl"}}
	rec := world(t, s)
	code, out, errOut := dispatch(t, rec, "dist", "build", "site/dist.yaml", "--modcache", "/cache", "--mirror", "registry.example/aiops=mirror.site/aiops",
		"--allow-bump", "golang.org/x/text", "--allow-bump", "golang.org/x/sync", "--builder", "local")
	if code != 0 || out != "" || !strings.Contains(errOut, "wrote /work/build/image") || !strings.Contains(errOut, "wrote /work/build/aiopsctl/aiopsctl") ||
		!strings.Contains(errOut, "the signed manifest sha256:") {
		t.Errorf("dist build: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	r := s.builds[len(s.builds)-1]
	if r.DistYAML != "/work/site/dist.yaml" || r.ModCache != "/cache" || r.Builder != "local" || r.Env == nil ||
		r.Mirrors["registry.example/aiops"] != "mirror.site/aiops" || strings.Join(r.AllowBump, ",") != "golang.org/x/text,golang.org/x/sync" {
		t.Errorf("request %+v", r)
	}
	if code, _, errOut := dispatch(t, rec, "dist", "build", "dist.yaml", "--mirror", "registry.example/aiops"); code != 2 || !strings.Contains(errOut, "--mirror") {
		t.Errorf("a malformed mirror: exit %d, stderr %q", code, errOut)
	}
	refusal := fmt.Errorf("%w: module example.test/x declares the id aiops/x", operations.ErrRefused)
	if code, _, errOut := dispatch(t, world(t, &stub{err: refusal}), "dist", "build", "dist.yaml"); code != 1 || !strings.Contains(errOut, refusal.Error()) {
		t.Errorf("a refusal: exit %d, stderr %q", code, errOut)
	}
}
