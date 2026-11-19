// SPDX-License-Identifier: Apache-2.0

package kerneltest_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

var (
	keyA = kernel.NewKey[string]("fake/a", 1, kernel.ProviderBound())
	keyB = kernel.NewKey[int]("fake/b", 1, kernel.ConsumerBound())
)

func providerA() kernel.Plugin {
	return kernel.Plugin{ID: "fake/a", Provides: []kernel.AnyKey{keyA}, Apply: func(c *kernel.Context, _ any) error {
		return keyA.Provide(c, "a")
	}}
}

func providerB() kernel.Plugin {
	return kernel.Plugin{ID: "fake/b", Provides: []kernel.AnyKey{keyB}, Apply: func(c *kernel.Context, _ any) error {
		return keyB.Provide(c, 7)
	}}
}

func consumer(got *string) kernel.Plugin {
	return kernel.Plugin{ID: "app/consumer", Inject: []kernel.Dep{kernel.Need(keyA), kernel.Need(keyB)},
		Apply: func(c *kernel.Context, _ any) error {
			*got = fmt.Sprint(keyA.Of(c), keyB.Of(c))
			return nil
		}}
}

// stubTB records what a TB would be told and runs cleanups by hand, so a test can
// assert on a failure the real TB would turn into a failed test.
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
func (s *stubTB) Errorf(f string, a ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errs = append(s.errs, fmt.Sprintf(f, a...))
}

func (s *stubTB) Fatalf(f string, a ...any) {
	s.Errorf(f, a...)
	runtime.Goexit()
}

func (s *stubTB) finish() {
	for i := len(s.cleanups) - 1; i >= 0; i-- {
		s.cleanups[i]()
	}
}

func TestWorkedExampleShape(t *testing.T) {
	var got string
	root := kerneltest.New(t)
	kerneltest.Mount(root, providerA(), providerB())
	root.Plugin(consumer(&got), nil)
	if err := root.Settle(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got != "a7" {
		t.Fatalf("consumer saw %q", got)
	}
}

func TestAloneWithFakesOfBothBoundClasses(t *testing.T) {
	var got string
	root := kerneltest.New(t)
	kerneltest.Mount(root, providerA(), providerB(), consumer(&got))
	if got != "a7" {
		t.Fatalf("consumer saw %q", got)
	}
}

func TestRemountThreeTimes(t *testing.T) {
	root := kerneltest.New(t)
	ctx := t.Context()
	for i := range 3 {
		var n int
		p := kernel.Plugin{ID: "app/p", Apply: func(*kernel.Context, any) error { n++; return nil }}
		f := root.Plugin(p, nil)
		if err := f.Await(ctx); err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
		if err := f.Dispose(ctx); err != nil {
			t.Fatal(err)
		}
		if err := f.Await(ctx); err != nil {
			t.Fatalf("round %d dispose: %v", i, err)
		}
		if st, _ := f.State(ctx); st != kernel.Disposed || n != 1 {
			t.Fatalf("round %d: state %s, applies %d", i, st, n)
		}
	}
}

func TestMountReportsFailureThroughTB(t *testing.T) {
	for name, p := range map[string]kernel.Plugin{
		"apply fails": {ID: "app/bad", Apply: func(*kernel.Context, any) error { return errors.New("boom") }},
		"unmet need":  {ID: "app/lonely", Inject: []kernel.Dep{kernel.Need(keyA)}, Apply: func(*kernel.Context, any) error { return nil }},
	} {
		t.Run(name, func(t *testing.T) {
			s := &stubTB{TB: t}
			root := kerneltest.New(s)
			kerneltest.Mount(root, p)
			if len(s.errs) != 1 || !strings.Contains(s.errs[0], p.ID) {
				t.Fatalf("errors = %q", s.errs)
			}
			s.finish()
		})
	}
}

func TestMountPanicsOnForeignRoot(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("no panic")
		}
	}()
	kerneltest.Mount(nil)
}

func TestLeakedFiberFailsTheTest(t *testing.T) {
	release := make(chan struct{})
	s := &stubTB{TB: t}
	root := kerneltest.New(s, kerneltest.WithDeadlines(time.Second, time.Second, 20*time.Millisecond))
	kerneltest.Mount(root, kernel.Plugin{ID: "app/stuck", Apply: func(c *kernel.Context, _ any) error {
		c.Defer("block", func(context.Context) error { <-release; return nil })
		return nil
	}})
	s.finish()
	close(release)
	if len(s.errs) != 1 || !strings.Contains(s.errs[0], "leaked") {
		t.Fatalf("errors = %q", s.errs)
	}
}

func TestOptionsOverrideDefaults(t *testing.T) {
	// A zero capacity is refused by the kernel, so the option reached the config.
	s := &stubTB{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		kerneltest.New(s, kerneltest.WithConfig(func(c *kernel.Config) { c.OwnerInbox = 0 }))
	}()
	<-done
	if len(s.errs) != 1 || !strings.Contains(s.errs[0], "NewForTest") {
		t.Fatalf("errors = %q", s.errs)
	}
}
