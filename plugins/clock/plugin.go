// SPDX-License-Identifier: Apache-2.0

// Package clock is the clock row: it provides the clock key with the standard
// library's time (03-c3-kernel.md:69; ADR-059 decision 8). "Now, timers and tickers
// as effects": every timer is an effect of the fiber that asks for it, so it stops
// when that fiber unloads (07-c4-plugin-sdk.md:516). The row has no config and no
// policy number.
package clock

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	def "github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

// Config is the config of the row, which has none.
type Config struct{}

// Plugin is the clock row. Each binary has one value of it: the module table holds
// it and the binding maps list it.
var Plugin = plugin.Define(plugin.Spec[Config]{
	ID:       "aiops/clock",
	Provides: []kernel.AnyKey{def.Key},
	Apply: func(c *kernel.Context, _ *Config) error {
		s := &system{act: c, wake: make(chan struct{}, 1)}
		c.Go("clock timers", s.run)
		return def.Key.Provide(c, s)
	},
})

// system is the clock over package time. Its pending timers wait in one list that
// a tracked goroutine of the row fires, so no timer starts a goroutine of its own
// (plugin vet). A timer belongs to the context it was started from.
type system struct {
	act     *kernel.Context // the activation that provides the clock
	mu      sync.Mutex
	pending []*timer
	wake    chan struct{} // one slot: the pending timers changed
}

func (*system) Now() time.Time { return time.Now() }

// After records the timer's stop as an effect scope of c's fiber, so that a fiber
// that is unloading is refused with kernel.ErrInactiveEffect and an unloaded one has
// its timer stopped, then starts the timer. The scope is withdrawn when the timer
// fires or is stopped, so the ledger holds the pending timers only. The kernel runs
// the scope's setup on the fiber's lane after After returns; a fiber that quiesces
// before that never records the stop, and the row drops its timer at its next pass.
// Once the row's activation is released, no goroutine fires a timer, so After
// returns kernel.ErrInactive and records nothing.
func (s *system) After(c *kernel.Context, d time.Duration) (def.Timer, error) {
	leave, ok := s.act.Enter()
	if !ok {
		return nil, kernel.ErrInactive
	}
	defer leave()
	t := &timer{s: s, at: time.Now().Add(d), ch: make(chan time.Time, 1)}
	h, err := c.RequestEffectScope("clock timer", func(es *kernel.EffectScope) error {
		return es.Defer("clock timer", func(context.Context) error { t.Stop(); return nil })
	})
	if err != nil {
		return nil, err
	}
	t.h = h
	s.mu.Lock()
	s.pending = append(s.pending, t)
	s.mu.Unlock()
	s.poke()
	return t, nil
}

func (s *system) poke() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// run fires the timers as they fall due until the row unloads.
func (s *system) run(ctx context.Context) error {
	wait := time.NewTimer(time.Hour)
	defer wait.Stop()
	for {
		next, ok := s.fire(time.Now())
		var due <-chan time.Time
		if ok {
			wait.Reset(time.Until(next))
			due = wait.C
		}
		select {
		case <-ctx.Done():
			return nil
		case <-s.wake:
		case <-due:
		}
	}
}

// fire delivers every timer due at now and returns the time of the next one. It
// asks for each withdrawal before it delivers the time, so that whoever receives it
// finds the timer already on its way out of the ledger.
func (s *system) fire(now time.Time) (next time.Time, ok bool) {
	s.mu.Lock()
	var due []*timer
	s.pending = slices.DeleteFunc(s.pending, func(t *timer) bool {
		if sealed(t.h) {
			return true // dropped: no one waits for it
		}
		if t.at.After(now) {
			if !ok || t.at.Before(next) {
				next, ok = t.at, true
			}
			return false
		}
		due = append(due, t)
		return true
	})
	s.mu.Unlock()
	for _, t := range due {
		_ = t.h.RequestWithdraw()
		t.ch <- now
	}
	return next, ok
}

// sealed reports whether the kernel sealed the scope h without running its setup: the
// fiber that asked for the timer quiesced first, so its stop was never recorded.
func sealed(h *kernel.EffectHandle) bool {
	ended, cancel := context.WithCancel(context.Background())
	cancel()
	return errors.Is(h.AwaitSetup(ended), kernel.ErrInactiveEffect)
}

type timer struct {
	s  *system
	at time.Time
	ch chan time.Time
	h  *kernel.EffectHandle
}

func (t *timer) C() <-chan time.Time { return t.ch }

func (t *timer) Stop() bool {
	t.s.mu.Lock()
	i := slices.Index(t.s.pending, t)
	if i >= 0 {
		t.s.pending = slices.Delete(t.s.pending, i, i+1)
	}
	t.s.mu.Unlock()
	if i < 0 {
		return false
	}
	_ = t.h.RequestWithdraw()
	return true
}
