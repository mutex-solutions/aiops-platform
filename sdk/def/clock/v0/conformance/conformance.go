// SPDX-License-Identifier: Apache-2.0

// Package conformance is the suite of the clock definition: what every provider of
// clock.Key and the fake must do (07-c4-plugin-sdk.md §8.2, T2 "every provider and
// the fake behave the same"). The obligations half holds a provider to the methods
// of clock.Clock. The guard half holds it to what it ends and refuses: a timer leaves
// with the fiber that started it; after the provider's release the consumer it
// served is released, a read of the key fails with kernel.ErrInactive, and so does
// After on the value kept from before, even from a fiber that is live, which then
// holds no effect of it; the provided value has no method outside the interface. Each obligation has an id, the first word of its case's name. The
// package imports the standard library, sdk/kernel and definitions only.
package conformance

import (
	"errors"
	"testing"
	"time"

	clock "github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Mount mounts a provider of clock.Key on a root of its own and returns the root
// and the provider's fiber, ACTIVE. The suite mounts its consumers on the root and
// releases the provider by disposing the fiber.
type Mount func(t *testing.T) (*kernel.Root, *kernel.Fiber)

// Run runs the suite, each case on a provider of its own.
func Run(t *testing.T, mount Mount) {
	deftest.Ran(t, "clock", "obligations", "guard")
	t.Run("obligations", func(t *testing.T) {
		t.Run("clock.O1 now does not go back", func(t *testing.T) {
			u := use(t, mount)
			if a, b := u.clk.Now(), u.clk.Now(); a.IsZero() || b.Before(a) {
				t.Errorf("Now = %v, then %v", a, b)
			}
		})

		t.Run("clock.O2 a timer of zero or less fires at once", func(t *testing.T) {
			u := use(t, mount)
			for _, d := range []time.Duration{0, -time.Second} {
				start := u.clk.Now()
				tm := u.after(d)
				select {
				case at := <-tm.C():
					if at.Before(start) {
						t.Errorf("a timer of %v fired at %v, before it started at %v", d, at, start)
					}
				case <-t.Context().Done():
					t.Fatalf("a timer of %v did not fire", d)
				}
				if tm.Stop() {
					t.Errorf("Stop of a fired timer of %v reported it pending", d)
				}
			}
		})

		t.Run("clock.O3 stop cancels a pending timer once", func(t *testing.T) {
			u := use(t, mount)
			tm := u.after(time.Hour)
			if !tm.Stop() {
				t.Error("Stop of a pending timer reported it fired")
			}
			if tm.Stop() {
				t.Error("a second Stop reported the timer pending")
			}
			select {
			case at := <-tm.C():
				t.Errorf("a stopped timer fired at %v", at)
			default:
			}
		})

		t.Run("clock.O4 the ledger holds the pending timers only", func(t *testing.T) {
			u := use(t, mount)
			tm := u.after(time.Hour)
			u.effects(1)
			tm.Stop()
			u.effects(0)
			<-u.after(0).C()
			u.effects(0)
		})
	})

	t.Run("guard", func(t *testing.T) {
		t.Run("clock.G1 a timer leaves with its fiber", func(t *testing.T) {
			u := use(t, mount)
			tm := u.after(time.Hour)
			u.effects(1) // the stop is recorded on the fiber's lane before the dispose
			deftest.Dispose(t, u.root, u.f)
			if tm.Stop() {
				t.Error("the timer of a disposed fiber is still pending")
			}
		})

		t.Run("clock.G2 after release the key and After fail with ErrInactive", func(t *testing.T) {
			u := use(t, mount)
			live, lc := hold(t, u.root)
			deftest.Dispose(t, u.root, u.provider)
			if err := deftest.ReadError(func() { clock.Key.Of(u.c) }); !errors.Is(err, kernel.ErrInactive) {
				t.Errorf("a read of the key after release = %v, want ErrInactive", err)
			}
			// lc is the context of a fiber that does not need the clock, so it is live:
			// only the released provider can refuse the call.
			if _, err := u.clk.After(lc, 0); !errors.Is(err, kernel.ErrInactive) {
				t.Errorf("After from a live fiber after release = %v, want ErrInactive", err)
			}
			_ = u.root.Settle(t.Context())
			if labels, err := live.Effects(t.Context()); err != nil || len(labels) != 0 {
				t.Errorf("a refused After left %q, %v in the caller's ledger", labels, err)
			}
		})

		t.Run("clock.G3 no side door", func(t *testing.T) {
			u := use(t, mount)
			if doors := deftest.SideDoors[clock.Clock](u.clk); len(doors) > 0 {
				t.Errorf("the provided value has methods outside clock.Clock: %v", doors)
			}
		})
	})
}

// user is a consumer of the clock on a mounted provider.
type user struct {
	t        *testing.T
	root     *kernel.Root
	provider *kernel.Fiber
	f        *kernel.Fiber
	c        *kernel.Context
	clk      clock.Clock
}

func use(t *testing.T, mount Mount) *user {
	t.Helper()
	u := &user{t: t}
	u.root, u.provider = mount(t)
	u.f, u.c, u.clk = deftest.Consume(t, u.root, clock.Key)
	return u
}

// hold mounts on root a fiber that needs nothing and returns it with its context.
func hold(t *testing.T, root *kernel.Root) (*kernel.Fiber, *kernel.Context) {
	t.Helper()
	var held *kernel.Context
	f := root.Plugin(kernel.Define(kernel.Plugin{ID: "conformance/holder",
		Apply: func(c *kernel.Context, _ any) error { held = c; return nil }}), nil)
	deftest.Active(t, f)
	return f, held
}

func (u *user) after(d time.Duration) clock.Timer {
	u.t.Helper()
	tm, err := u.clk.After(u.c, d)
	if err != nil {
		u.t.Fatalf("After(%v) = %v", d, err)
	}
	return tm
}

// effects wants n effects in the consumer's ledger once the kernel settled, which
// joins the work the timers asked of the fiber's lane.
func (u *user) effects(n int) {
	u.t.Helper()
	_ = u.root.Settle(u.t.Context())
	if labels, err := u.f.Effects(u.t.Context()); err != nil || len(labels) != n {
		u.t.Errorf("the consumer's ledger holds %q, %v; want %d effects", labels, err, n)
	}
}
