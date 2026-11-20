// SPDX-License-Identifier: Apache-2.0

// Package clock is the clock row: it provides the clock key with the standard
// library's time (03-c3-kernel.md:69; ADR-059 decision 8). "Now, timers and tickers
// as effects": every timer is an effect of the fiber that asks for it, so it stops
// when that fiber unloads (07-c4-plugin-sdk.md:516). The row has no config and no
// policy number.
package clock

import (
	"context"
	"time"

	def "github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Plugin is the clock row. Each binary has one value of it: the module table holds
// it and the binding maps list it.
var Plugin = kernel.Define(kernel.Plugin{
	ID:       "aiops/clock",
	Provides: []kernel.AnyKey{def.Key},
	Apply:    func(c *kernel.Context, _ any) error { return def.Key.Provide(c, system{}) },
})

// system is the clock over package time. It holds no state: a timer belongs to the
// context it was started from.
type system struct{}

func (system) Now() time.Time { return time.Now() }

// After starts the timer and records its stop as an effect scope of c's fiber, so
// that a fiber that is unloading is refused with kernel.ErrInactiveEffect and an
// unloaded one has its timer stopped. The scope is withdrawn when the timer fires or
// is stopped, so the ledger holds the pending timers only. The kernel runs the
// scope's setup on the fiber's lane after After returns; a fiber that quiesces before
// that never records the stop, and its timer runs out unread.
func (system) After(c *kernel.Context, d time.Duration) (def.Timer, error) {
	t := &timer{ch: make(chan time.Time, 1), owned: make(chan struct{})}
	t.t = time.AfterFunc(d, t.fire)
	h, err := c.RequestEffectScope("clock timer", func(s *kernel.EffectScope) error {
		return s.Defer("clock timer", func(context.Context) error { t.t.Stop(); return nil })
	})
	t.h = h
	close(t.owned)
	if err != nil {
		t.t.Stop()
		return nil, err
	}
	return t, nil
}

type timer struct {
	ch    chan time.Time
	t     *time.Timer
	h     *kernel.EffectHandle // nil when After was refused
	owned chan struct{}        // closed once h is set
}

// fire asks for the withdrawal before it delivers the time, so that whoever receives
// it finds the timer already on its way out of the ledger.
func (t *timer) fire() {
	now := time.Now()
	<-t.owned
	if t.h != nil {
		_ = t.h.RequestWithdraw()
	}
	t.ch <- now
}

func (t *timer) C() <-chan time.Time { return t.ch }

func (t *timer) Stop() bool {
	if !t.t.Stop() {
		return false
	}
	_ = t.h.RequestWithdraw()
	return true
}
