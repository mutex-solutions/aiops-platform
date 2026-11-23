// SPDX-License-Identifier: Apache-2.0

package cycle_test

import (
	"context"
	"fmt"
	"maps"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/cycle"
)

// registry is a recording fake of a registry keyed key: a registration is an effect
// of the caller's fiber labelled "<key>:<name>", the convention of the toy
// registries (echoers.Label).
type registry struct {
	key   kernel.Key[*registry]
	mu    sync.Mutex
	names map[string]bool
}

func newRegistry(name string) *registry {
	return &registry{key: kernel.NewKey[*registry](name, 0), names: map[string]bool{}}
}

func (r *registry) fake() cycle.Fake {
	return cycle.Fake{Plugin: kernel.Define(kernel.Plugin{ID: "test/" + r.key.Name() + "-fake", Provides: []kernel.AnyKey{r.key},
		Apply: func(c *kernel.Context, _ any) error { return r.key.Provide(c, r) }}), Inventory: r.Inventory}
}

func (r *registry) Register(c *kernel.Context, name string) error {
	return c.Effect(r.key.Name()+":"+name, func() (kernel.Disposer, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.names[name] = true
		return func(context.Context) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			delete(r.names, name)
			return nil
		}, nil
	})
}

// Leave records name with no effect, so it stays after the caller unloads.
func (r *registry) Leave(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.names[name] = true
}

func (r *registry) Inventory() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Sorted(maps.Keys(r.names))
}

// stubTB records failures, so a test can assert that Run fails; Fatalf ends the
// goroutine that runs Run, as testing.T's does.
type stubTB struct {
	testing.TB
	mu       sync.Mutex
	errs     []string
	cleanups []func()
}

func (s *stubTB) Helper()                  {}
func (s *stubTB) Context() context.Context { return context.Background() }
func (s *stubTB) Logf(string, ...any)      {}
func (s *stubTB) Cleanup(f func())         { s.cleanups = append(s.cleanups, f) }
func (s *stubTB) Failed() bool             { s.mu.Lock(); defer s.mu.Unlock(); return len(s.errs) > 0 }
func (s *stubTB) Errorf(f string, a ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errs = append(s.errs, fmt.Sprintf(f, a...))
}

func (s *stubTB) Fatalf(f string, a ...any) {
	s.Errorf(f, a...)
	runtime.Goexit()
}

// run runs cycle.Run on a stub and returns what it reported.
func run(p kernel.Plugin, registers []string, fakes ...cycle.Fake) string {
	s := &stubTB{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		cycle.Run(s, p, nil, registers, fakes...)
	}()
	<-done
	for i := len(s.cleanups) - 1; i >= 0; i-- {
		s.cleanups[i]()
	}
	return strings.Join(s.errs, "\n")
}

func contributor(r *registry, apply func(c *kernel.Context) error) kernel.Plugin {
	return kernel.Define(kernel.Plugin{ID: "test/contributor", Inject: []kernel.Dep{kernel.Need(r.key)},
		Apply: func(c *kernel.Context, _ any) error { return apply(c) }})
}

// A plugin that registers in the registry it lists, and leaves nothing behind,
// passes three rounds.
func TestACleanPluginPasses(t *testing.T) {
	r := newRegistry("test.listed")
	p := contributor(r, func(c *kernel.Context) error { return r.key.Of(c).Register(c, "one") })
	if got := run(p, []string{"test.listed"}, r.fake()); got != "" {
		t.Fatalf("Run failed a clean plugin:\n%s", got)
	}
	cycle.Run(t, p, nil, []string{"test.listed"}, r.fake())
}

// A registration in a registry that Spec.Registers does not list fails
// (ADR-048 decision 21).
func TestAnUnlistedRegistrationFails(t *testing.T) {
	r := newRegistry("test.unlisted")
	p := contributor(r, func(c *kernel.Context) error { return r.key.Of(c).Register(c, "one") })
	if got := run(p, nil, r.fake()); !strings.Contains(got, `registers "one" in test.unlisted, which Spec.Registers does not list`) {
		t.Fatalf("Run reported:\n%s", got)
	}
}

// What a plugin leaves in a fake after it is disposed fails: the Inventory is not
// back at its baseline.
func TestALeftoverFails(t *testing.T) {
	r := newRegistry("test.leftover")
	p := contributor(r, func(c *kernel.Context) error { r.key.Of(c).Leave("stale"); return nil })
	if got := run(p, []string{"test.leftover"}, r.fake()); !strings.Contains(got, "is not back at its baseline") {
		t.Fatalf("Run reported:\n%s", got)
	}
}

// A goroutine the plugin starts and does not end fails, through goleak.
func TestALeakedGoroutineFails(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	p := kernel.Define(kernel.Plugin{ID: "test/leaker", Apply: func(*kernel.Context, any) error {
		go func() { <-release }()
		return nil
	}})
	if got := run(p, nil); !strings.Contains(got, "goroutine") {
		t.Fatalf("Run reported:\n%s", got)
	}
}

// A plugin that does not activate fails at once.
func TestAFailedActivationFails(t *testing.T) {
	p := kernel.Define(kernel.Plugin{ID: "test/failing", Apply: func(*kernel.Context, any) error {
		return fmt.Errorf("refused")
	}})
	if got := run(p, nil); !strings.Contains(got, "round 1") || !strings.Contains(got, "refused") {
		t.Fatalf("Run reported:\n%s", got)
	}
}
