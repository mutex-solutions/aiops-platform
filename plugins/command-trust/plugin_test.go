// SPDX-License-Identifier: Apache-2.0

package commandtrust_test

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

	row "github.com/mutex-solutions/aiops-platform/plugins/command-trust"
	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0/fake"
	operations "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

var update = flag.Bool("update", false, "rewrite the help goldens")

// stub provides operations: AddTrust records each request and answers ch and err, so
// the tests read what the row asks of the library without a key file.
type stub struct {
	operations.Operations
	ch  operations.Change
	err error

	mu  sync.Mutex
	got []operations.TrustRequest
}

func (s *stub) AddTrust(ctx context.Context, r operations.TrustRequest) (operations.Change, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, r)
	return s.ch, cmp.Or(ctx.Err(), s.err)
}

// last is the last request, the zero request when the row made none.
func (s *stub) last() operations.TrustRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.got) == 0 {
		return operations.TrustRequest{}
	}
	return s.got[len(s.got)-1]
}

// mountRow mounts s as the provider of operations and the row on a root of its own.
func mountRow(t *testing.T, s *stub) (*kernel.Root, *kernel.Fiber) {
	root := kerneltest.New(t)
	kerneltest.Mount(root, kernel.Define(kernel.Plugin{ID: "test/operations", Provides: []kernel.AnyKey{operations.Key},
		Apply: func(c *kernel.Context, _ any) error { return operations.Key.Provide(c, s) }}))
	return root, root.Plugin(row.Plugin, nil)
}

func mount(t *testing.T) (conformance.Subject, *kernel.Root) {
	root, f := mountRow(t, &stub{ch: operations.Change{Outcome: operations.NotRunning}})
	return conformance.Contribution(f, "fixtures"), root
}

// The row keeps the obligations of a command on the cases of its fixtures (ADR-061
// decision 5).
func TestObligations(t *testing.T) { conformance.Run(t, mount) }

// The help of each command is the CLI spec (ADR-061 decision 30).
func TestHelp(t *testing.T) { conformance.Help(t, mount, *update) }

// world is the row mounted on s, with the recording dispatcher in the working directory
// /work and the home /home.
func world(t *testing.T, s *stub) *fake.Registry {
	root, _ := mountRow(t, s)
	rec, p := fake.New()
	rec.Dir, rec.Home = "/work", "/home"
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

// trust add calls AddTrust with the home of the invocation, the key file resolved
// against the working directory, and each comma-separated list of --ids, --runtimes and
// --signed, or --distribution (ADR-061 decisions 16, 17 and 20; ADR-020 decision 5).
// It names what it wrote and the outcome on stderr, prints nothing on stdout and exits 0
// for not-running and restart-required.
func TestAddMapsTheFormsToTheRequest(t *testing.T) {
	s := &stub{ch: operations.Change{Written: []string{"/home/trust.yaml"}, Outcome: operations.NotRunning}}
	rec := world(t, s)
	code, out, errOut := dispatch(t, rec, "trust", "add", "k.pub", "--ids", "acme/x,acme/*", "--runtimes", "inproc,remote", "--signed", "identities")
	if code != 0 || out != "" || !strings.Contains(errOut, "/home/trust.yaml") || !strings.Contains(errOut, "not-running") {
		t.Errorf("trust add --ids: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	r := s.last()
	if r.Home != "/home" || r.KeyFile != "/work/k.pub" || !slices.Equal(r.IDs, []string{"acme/x", "acme/*"}) ||
		!slices.Equal(r.Runtimes, []string{"inproc", "remote"}) || !slices.Equal(r.Signed, []string{"identities"}) || r.Distribution || r.Env == nil {
		t.Errorf("request %+v", r)
	}
	s.ch = operations.Change{Outcome: operations.RestartRequired, Reason: operations.ReasonNotSubmitted}
	if code, _, errOut := dispatch(t, rec, "trust", "add", "/keys/root.pub", "--distribution"); code != 0 || !strings.Contains(errOut, "restart-required: not-submitted") {
		t.Errorf("trust add --distribution: exit %d, stderr %q", code, errOut)
	}
	if r := s.last(); r.KeyFile != "/keys/root.pub" || !r.Distribution || r.IDs != nil || r.Runtimes != nil || r.Signed != nil {
		t.Errorf("request %+v", r)
	}
}

// A refusal of the library exits 1 with its reason, and a usage error of the library,
// such as no home, exits 2 (ADR-061 decision 10).
func TestAddExits(t *testing.T) {
	for _, c := range []struct {
		err  error
		code int
	}{
		{fmt.Errorf("%w: wasm is refused", operations.ErrRefused), 1},
		{fmt.Errorf("%w: write.lock is held by 7 trust add", operations.ErrLocked), 1},
		{fmt.Errorf("%w: AIOPS_HOME is unset", operations.ErrUsage), 2},
	} {
		code, out, errOut := dispatch(t, world(t, &stub{err: c.err}), "trust", "add", "k.pub", "--ids", "acme/*")
		if code != c.code || out != "" || !strings.Contains(errOut, c.err.Error()) {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", c.err, code, out, errOut)
		}
	}
}
