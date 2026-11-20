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

// After starts the timer inside Effect, so that a fiber that is unloading is refused
// with kernel.ErrInactiveEffect and an unloaded one has its timer stopped.
func (system) After(c *kernel.Context, d time.Duration) (def.Timer, error) {
	var t *time.Timer
	err := c.Effect("clock timer", func() (kernel.Disposer, error) {
		t = time.NewTimer(d)
		return func(context.Context) error { t.Stop(); return nil }, nil
	})
	if err != nil {
		return nil, err
	}
	return timer{t}, nil
}

type timer struct{ t *time.Timer }

func (t timer) C() <-chan time.Time { return t.t.C }
func (t timer) Stop() bool          { return t.t.Stop() }
