// SPDX-License-Identifier: Apache-2.0

// Package fake is the in-memory clock of the clock definition: time moves only
// when a test calls Advance, so a test of backoff and windows needs no sleep
// (07-c4-plugin-sdk.md:1410; 03-c3-kernel.md:66). It imports the standard library,
// sdk/kernel and its definition only.
package fake

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	clock "github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Clock is a manual clock that implements clock.Clock.
type Clock struct {
	mu      sync.Mutex
	now     time.Time
	seq     int
	pending []*timer
}

var _ clock.Clock = (*Clock)(nil)

// New returns a clock standing at start and the plugin that provides it under
// clock.Key. Each call returns a new plugin value, a new entry of its own.
func New(start time.Time) (*Clock, kernel.Plugin) {
	f := &Clock{now: start}
	return f, kernel.Define(kernel.Plugin{
		ID:       "aiops/clock-fake",
		Provides: []kernel.AnyKey{clock.Key},
		Apply:    func(c *kernel.Context, _ any) error { return clock.Key.Provide(c, f) },
	})
}

// Now returns the time the clock stands at.
func (f *Clock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// After starts a timer and records its stop as an effect scope of c's fiber, which
// is withdrawn when the timer fires or is stopped, as the clock row does. The kernel
// runs the scope's setup on the fiber's lane, and never for an activation that
// quiesced first: such a timer is dropped, as the clock row drops it.
func (f *Clock) After(c *kernel.Context, d time.Duration) (clock.Timer, error) {
	t := f.start(d)
	h, err := c.RequestEffectScope("clock timer", func(s *kernel.EffectScope) error {
		return s.Defer("clock timer", func(context.Context) error { t.stop(); return nil })
	})
	if err != nil {
		t.stop()
		return nil, err
	}
	f.mu.Lock()
	t.h = h
	fired := !slices.Contains(f.pending, t)
	f.mu.Unlock()
	if fired {
		_ = h.RequestWithdraw()
	}
	return t, nil
}

func (f *Clock) start(d time.Duration) *timer {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	t := &timer{f: f, at: f.now.Add(d), seq: f.seq, ch: make(chan time.Time, 1)}
	if d <= 0 {
		t.ch <- f.now
		return t
	}
	f.pending = append(f.pending, t)
	return t
}

// Advance moves the clock forward by d and fires, in order of time and then of
// start, every timer that falls due on the way. Each timer fires with the clock
// standing at its own time. A d of zero or less does nothing. The effects of the
// timers that fired are withdrawn before it returns.
func (f *Clock) Advance(d time.Duration) {
	for _, h := range f.advance(d) {
		_ = h.RequestWithdraw()
	}
}

// advance is Advance under the lock: it returns the effects of the timers that fired.
func (f *Clock) advance(d time.Duration) (fired []*kernel.EffectHandle) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prune()
	target := f.now.Add(d)
	for {
		next := -1
		for i, t := range f.pending {
			if t.at.After(target) {
				continue
			}
			if next < 0 || t.at.Before(f.pending[next].at) || (t.at.Equal(f.pending[next].at) && t.seq < f.pending[next].seq) {
				next = i
			}
		}
		if next < 0 {
			break
		}
		t := f.pending[next]
		f.pending = append(f.pending[:next], f.pending[next+1:]...)
		f.now = t.at
		t.ch <- t.at
		if t.h != nil { // nil while After has not returned: it withdraws then
			fired = append(fired, t.h)
		}
	}
	if target.After(f.now) {
		f.now = target
	}
	return fired
}

// Pending is the number of timers that have not fired and not been stopped.
func (f *Clock) Pending() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prune()
	return len(f.pending)
}

// prune drops the timers whose stop was never recorded. f.mu is held.
func (f *Clock) prune() {
	f.pending = slices.DeleteFunc(f.pending, func(t *timer) bool { return sealed(t.h) })
}

// sealed reports whether the kernel sealed the scope h without running its setup: the
// fiber that asked for the timer quiesced first, so no one waits for it.
func sealed(h *kernel.EffectHandle) bool {
	ended, cancel := context.WithCancel(context.Background())
	cancel()
	return errors.Is(h.AwaitSetup(ended), kernel.ErrInactiveEffect)
}

type timer struct {
	f   *Clock
	at  time.Time
	seq int
	ch  chan time.Time
	h   *kernel.EffectHandle // set by After under f.mu
}

func (t *timer) C() <-chan time.Time { return t.ch }

func (t *timer) Stop() bool {
	if !t.stop() {
		return false
	}
	_ = t.h.RequestWithdraw()
	return true
}

// stop takes t off the pending timers and reports whether it was there.
func (t *timer) stop() bool {
	t.f.mu.Lock()
	defer t.f.mu.Unlock()
	for i, p := range t.f.pending {
		if p == t {
			t.f.pending = append(t.f.pending[:i], t.f.pending[i+1:]...)
			return true
		}
	}
	return false
}
