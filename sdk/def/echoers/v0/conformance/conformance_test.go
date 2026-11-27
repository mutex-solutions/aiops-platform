// SPDX-License-Identifier: Apache-2.0

package conformance_test

import (
	"context"
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	echoers "github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

type echoFunc func(ctx context.Context, msg string) (string, error)

func (f echoFunc) Echo(ctx context.Context, msg string) (string, error) { return f(ctx, msg) }

// contribution mounts a plugin that registers what echoer returns for its context.
func contribution(echoer func(c *kernel.Context) echoers.Echoer) conformance.Mount {
	return func(t *testing.T) (conformance.Subject, *kernel.Root) {
		root := kerneltest.New(t)
		p := kernel.Define(kernel.Plugin{ID: "test/under-test", Inject: []kernel.Dep{kernel.Need(echoers.Key)},
			Apply: func(c *kernel.Context, _ any) error { return echoers.Key.Of(c).Register(c, "under-test", echoer(c)) }})
		return conformance.Contribution(root.Plugin(p, nil)), root
	}
}

// correct is an echoer that keeps the contract.
func correct(c *kernel.Context) echoers.Echoer {
	return echoFunc(func(ctx context.Context, msg string) (string, error) {
		leave, ok := c.Enter()
		if !ok {
			return "", kernel.ErrInactive
		}
		defer leave()
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "echo: " + msg, nil
	})
}

// An echoer that keeps the contract passes the obligations half.
func TestObligationsPassOnACorrectEchoer(t *testing.T) {
	conformance.Run(t, contribution(correct))
}

// The suite records the half it ran only when every case of it ran (ADR-060 decision
// 50): on a mount that skips each case after the first, as one that cannot set them up
// does, most of the half did not run, and nothing is recorded for the release build or
// step 5 of plugin check to attest.
func TestASuiteWithASkippedCaseRecordsNothing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(deftest.RecordVar, dir)
	mount := contribution(correct)
	t.Run("suite", func(t *testing.T) {
		conformance.Run(t, func(t *testing.T) (conformance.Subject, *kernel.Root) {
			if strings.Count(t.Name(), "/") > 1 {
				t.Skip("a case that this mount cannot set up")
			}
			return mount(t)
		})
	})
	if files, err := filepath.Glob(filepath.Join(dir, "*.json")); err != nil || len(files) > 0 {
		t.Errorf("the suite recorded %v (%v); a suite with a case that skipped records nothing", files, err)
	}
}

const stubEnv = "ECHOERS_CONFORMANCE_STUB"

var failLine = regexp.MustCompile(`(?m)^\s+\S+\.go:\d+: (\S+): `)

// A stub that answers "not implemented", as each stub of a fresh template does, fails
// every obligation with exactly one line that starts with its id (07-c4-plugin-sdk.md:1561,
// :1563). The suite runs in a child process, whose failure is the point.
func TestStubFailsOncePerObligation(t *testing.T) {
	if os.Getenv(stubEnv) == "1" {
		conformance.Run(t, contribution(func(*kernel.Context) echoers.Echoer {
			return echoFunc(func(context.Context, string) (string, error) { return "", errors.New("not implemented") })
		}))
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestStubFailsOncePerObligation$", "-test.count=1")
	cmd.Env = append(os.Environ(), stubEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the obligations half passed a stub:\n%s", out)
	}
	var got []string
	for _, m := range failLine.FindAllStringSubmatch(string(out), -1) {
		got = append(got, m[1])
	}
	if !slices.Equal(got, conformance.Obligations) {
		t.Fatalf("failure lines name %v, want each of %v once:\n%s", got, conformance.Obligations, out)
	}
}

const leaselessEnv = "ECHOERS_CONFORMANCE_LEASELESS"

// A dispatcher that keeps every rule but one, the lease on a contribution's fiber
// around each call (07-c4-plugin-sdk.md:503, M2), fails the guard half on the one
// case that holds a call in flight while the contribution's fiber is disposed. The
// suite runs in a child process, whose failure is the point.
func TestLeaselessDispatcherFailsTheGuardHalf(t *testing.T) {
	if os.Getenv(leaselessEnv) == "1" {
		conformance.Run(t, func(t *testing.T) (conformance.Subject, *kernel.Root) {
			root := kerneltest.New(t)
			return conformance.Dispatcher(root.Plugin(leaseless, nil)), root
		})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestLeaselessDispatcherFailsTheGuardHalf$", "-test.count=1")
	cmd.Env = append(os.Environ(), leaselessEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the guard half passed a dispatcher that takes no lease:\n%s", out)
	}
	var got []string
	for _, m := range failLine.FindAllStringSubmatch(string(out), -1) {
		got = append(got, m[1])
	}
	if want := []string{"echoers/guard-lease"}; !slices.Equal(got, want) {
		t.Fatalf("failure lines name %v, want %v:\n%s", got, want, out)
	}
}

// leaseless provides echoers.Key with a registry per activation that calls a
// echoer without entering its fiber.
var leaseless = kernel.Define(kernel.Plugin{ID: "test/leaseless", Provides: []kernel.AnyKey{echoers.Key},
	Apply: func(c *kernel.Context, _ any) error {
		return echoers.Key.Provide(c, &leaselessRegistry{act: c, regs: map[string]echoers.Echoer{}})
	}})

type leaselessRegistry struct {
	act  *kernel.Context
	mu   sync.Mutex
	regs map[string]echoers.Echoer
}

func (r *leaselessRegistry) Register(c *kernel.Context, name string, x echoers.Echoer) error {
	leave, ok := r.act.Enter()
	if !ok {
		return kernel.ErrInactive
	}
	defer leave()
	if name == "" {
		return echoers.ErrName
	}
	return c.Effect(echoers.Label(name), func() (kernel.Disposer, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.regs[name] != nil {
			return nil, echoers.ErrName
		}
		r.regs[name] = x
		return func(context.Context) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			delete(r.regs, name)
			return nil
		}, nil
	})
}

func (r *leaselessRegistry) Echo(ctx context.Context, name, msg string) (string, error) {
	leave, ok := r.act.Enter()
	if !ok {
		return "", kernel.ErrInactive
	}
	defer leave()
	r.mu.Lock()
	x := r.regs[name]
	r.mu.Unlock()
	if x == nil {
		return "", echoers.ErrUnknown
	}
	return x.Echo(ctx, msg) // no lease on the echoer's fiber
}

func (r *leaselessRegistry) Inventory() []string {
	leave, ok := r.act.Enter()
	if !ok {
		return nil
	}
	defer leave()
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Sorted(maps.Keys(r.regs))
}
