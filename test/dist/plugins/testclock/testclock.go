// SPDX-License-Identifier: Apache-2.0

// Package testclock is a clock provider on the real time, for the test distribution.
// The plugin that will provide clock in a shipped distribution is plugins/clock, which
// another step builds; until it is linked, the control rows of the test distribution
// (test-state and test-submit) poll through this one. Its Timer is an effect of the
// caller's fiber, as the clock definition requires (clock.Clock).
package testclock

import (
	"context"
	"time"

	clock "github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// ID is the plugin id.
const ID = "aiops-test/test-clock"

// New returns the plugin, which provides clock.Key.
func New() kernel.Plugin {
	return kernel.Define(kernel.Plugin{
		ID:       ID,
		Provides: []kernel.AnyKey{clock.Key},
		Apply:    func(c *kernel.Context, _ any) error { return clock.Key.Provide(c, system{}) },
	})
}

type system struct{}

func (system) Now() time.Time { return time.Now() }

func (system) After(c *kernel.Context, d time.Duration) (clock.Timer, error) {
	t := &timer{ch: make(chan time.Time, 1)}
	err := c.Effect("clock timer", func() (kernel.Disposer, error) {
		t.t = time.AfterFunc(d, func() { t.ch <- time.Now() })
		return func(context.Context) error { t.t.Stop(); return nil }, nil
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

type timer struct {
	t  *time.Timer
	ch chan time.Time
}

func (t *timer) C() <-chan time.Time { return t.ch }
func (t *timer) Stop() bool          { return t.t.Stop() }
