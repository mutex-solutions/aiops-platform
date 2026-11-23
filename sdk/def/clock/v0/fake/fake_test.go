// SPDX-License-Identifier: Apache-2.0

package fake_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	clock "github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

var t0 = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

// fired reports whether t has fired, without blocking and without a sleep: the
// fake sends before Advance returns.
func fired(t clock.Timer) (time.Time, bool) {
	select {
	case at := <-t.C():
		return at, true
	default:
		return time.Time{}, false
	}
}

// mount mounts the fake and a consumer, and returns what the consumer got.
func mount(t *testing.T) (*fake.Clock, clock.Clock, *kernel.Context) {
	t.Helper()
	f, provider := fake.NewAt(t0)
	var got clock.Clock
	var cc *kernel.Context
	consumer := kernel.Define(kernel.Plugin{ID: "aiops/consumer", Inject: []kernel.Dep{kernel.Need(clock.Key)}, Apply: func(c *kernel.Context, _ any) error {
		got, cc = clock.Key.Of(c), c
		return nil
	}})
	root := kerneltest.New(t)
	kerneltest.Mount(root, provider, consumer)
	if got == nil {
		t.Fatal("the consumer did not run")
	}
	return f, got, cc
}

func TestNowOnlyMovesOnAdvance(t *testing.T) {
	f, c, _ := mount(t)
	if !c.Now().Equal(t0) {
		t.Fatalf("Now = %v", c.Now())
	}
	f.Advance(90 * time.Second)
	f.Advance(-time.Hour) // ignored
	f.Advance(0)
	if want := t0.Add(90 * time.Second); !c.Now().Equal(want) {
		t.Fatalf("Now = %v, want %v", c.Now(), want)
	}
}

func TestTimersFireInOrderAtTheirOwnTime(t *testing.T) {
	f, c, cc := mount(t)
	mk := func(d time.Duration) clock.Timer {
		tm, err := c.After(cc, d)
		if err != nil {
			t.Fatal(err)
		}
		return tm
	}
	a, b, same := mk(10*time.Second), mk(5*time.Second), mk(10*time.Second)
	if f.Pending() != 3 {
		t.Fatalf("Pending = %d", f.Pending())
	}
	f.Advance(4 * time.Second)
	if _, ok := fired(b); ok {
		t.Fatal("fired early")
	}
	f.Advance(time.Second)
	if at, ok := fired(b); !ok || !at.Equal(t0.Add(5*time.Second)) {
		t.Fatalf("b fired at %v, %v", at, ok)
	}
	if _, ok := fired(a); ok {
		t.Fatal("a fired early")
	}
	f.Advance(time.Hour) // both due; each carries its own time
	if at, ok := fired(a); !ok || !at.Equal(t0.Add(10*time.Second)) {
		t.Fatalf("a fired at %v, %v", at, ok)
	}
	if _, ok := fired(same); !ok {
		t.Fatal("same did not fire")
	}
	if f.Pending() != 0 || !c.Now().Equal(t0.Add(time.Hour+5*time.Second)) {
		t.Fatalf("Pending %d, Now %v", f.Pending(), c.Now())
	}
}

func TestStopAndImmediateTimers(t *testing.T) {
	f, c, cc := mount(t)
	stopped, _ := c.After(cc, time.Second)
	if !stopped.Stop() || stopped.Stop() {
		t.Fatal("Stop must report true once")
	}
	f.Advance(time.Minute)
	if _, ok := fired(stopped); ok {
		t.Fatal("a stopped timer fired")
	}
	now, _ := c.After(cc, 0)
	if at, ok := fired(now); !ok || !at.Equal(f.Now()) {
		t.Fatalf("a zero timer fired at %v, %v", at, ok)
	}
	if now.Stop() {
		t.Fatal("Stop of a fired timer must report false")
	}
}

// A timer is an effect of the caller's fiber: it is gone when the fiber is
// disposed, and a disposed fiber cannot start one (07-c4-plugin-sdk.md:516).
func TestTimerLeavesWithItsFiber(t *testing.T) {
	f, provider := fake.NewAt(t0)
	var timer clock.Timer
	var cc *kernel.Context
	var cl clock.Clock
	consumer := kernel.Define(kernel.Plugin{ID: "aiops/consumer", Inject: []kernel.Dep{kernel.Need(clock.Key)}, Apply: func(c *kernel.Context, _ any) error {
		cl, cc = clock.Key.Of(c), c
		var err error
		timer, err = cl.After(c, time.Minute)
		return err
	}})
	root := kerneltest.New(t)
	kerneltest.Mount(root, provider)
	fib := root.Plugin(consumer, nil)
	if err := fib.Await(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.Pending() != 1 {
		t.Fatalf("Pending = %d", f.Pending())
	}
	if err := fib.Dispose(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := fib.Await(context.Background()); err != nil { // Dispose does not wait
		t.Fatal(err)
	}
	if f.Pending() != 0 {
		t.Fatalf("Pending after dispose = %d", f.Pending())
	}
	f.Advance(time.Hour)
	if _, ok := fired(timer); ok {
		t.Fatal("the timer of a disposed fiber fired")
	}
	if _, err := cl.After(cc, time.Second); !errors.Is(err, kernel.ErrInactiveEffect) {
		t.Fatalf("After on a disposed fiber: %v", err)
	}
}

// The stop of a timer is recorded by the scope's setup, which the kernel never runs for
// an activation that quiesced first: an Apply that starts a timer and then fails. Such
// a timer is not pending and never fires.
func TestTimerOfAFailedApplyNeverFires(t *testing.T) {
	f, provider := fake.NewAt(t0)
	var timer clock.Timer
	consumer := kernel.Define(kernel.Plugin{ID: "aiops/consumer", Inject: []kernel.Dep{kernel.Need(clock.Key)}, Apply: func(c *kernel.Context, _ any) error {
		var err error
		if timer, err = clock.Key.Of(c).After(c, time.Minute); err != nil {
			return err
		}
		return errors.New("boom")
	}})
	root := kerneltest.New(t)
	kerneltest.Mount(root, provider)
	fib := root.Plugin(consumer, nil)
	if err := fib.Await(t.Context()); err == nil {
		t.Fatal("the Apply did not fail")
	}
	if err := fib.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	_ = fib.Await(t.Context())   // Dispose does not wait; Await reports the failure
	_ = root.Settle(t.Context()) // it reports the failure
	if f.Pending() != 0 {
		t.Fatalf("Pending = %d after the fiber that started the timer failed and was disposed", f.Pending())
	}
	f.Advance(time.Hour)
	if _, ok := fired(timer); ok {
		t.Fatal("the timer of a failed and disposed fiber fired")
	}
}

// A timer that fires or is stopped leaves its fiber's effect ledger, so a fiber that
// starts one timer after another holds only those still pending.
func TestFiredAndStoppedTimersLeaveTheLedger(t *testing.T) {
	f, provider := fake.NewAt(t0)
	var cl clock.Clock
	var cc *kernel.Context
	consumer := kernel.Define(kernel.Plugin{ID: "aiops/consumer", Inject: []kernel.Dep{kernel.Need(clock.Key)}, Apply: func(c *kernel.Context, _ any) error {
		cl, cc = clock.Key.Of(c), c
		return nil
	}})
	root := kerneltest.New(t)
	kerneltest.Mount(root, provider)
	fib := root.Plugin(consumer, nil)
	if err := fib.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	for range 100 {
		tm, err := cl.After(cc, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		f.Advance(time.Second)
		if _, ok := fired(tm); !ok {
			t.Fatal("the timer did not fire")
		}
	}
	if _, err := cl.After(cc, 0); err != nil {
		t.Fatal(err)
	}
	stopped, err := cl.After(cc, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !stopped.Stop() {
		t.Fatal("Stop reported a pending timer as fired")
	}
	if _, err := cl.After(cc, time.Minute); err != nil {
		t.Fatal(err)
	}
	// The kernel withdraws an effect on the fiber's lane; Settle joins that work.
	if err := root.Settle(t.Context()); err != nil {
		t.Fatal(err)
	}
	if labels, _ := fib.Effects(t.Context()); !slices.Equal(labels, []string{"clock timer"}) {
		t.Errorf("effects = %d %v, want the one pending timer", len(labels), labels[:min(len(labels), 3)])
	}
}

// clock is open (03-c3-kernel.md:69): any plugin provides and injects it under binding maps.
func TestKeyIsOpenUnderBindingMaps(t *testing.T) {
	_, provider := fake.NewAt(t0)
	consumer := deftest.Plugin("aiops/consumer", []kernel.Dep{kernel.Need(clock.Key)}, nil, nil, func(c *kernel.Context) error { _ = clock.Key.Of(c); return nil })
	root := deftest.Bound(t, deftest.Config(nil, nil))
	deftest.Active(t, root.Plugin(provider, nil))
	deftest.Active(t, root.Plugin(consumer, nil))
}

// The use restart-policy makes of it: failures in a window, with backoff, run on
// the fake in no real time.
func TestRestartPolicyShapedUse(t *testing.T) {
	f, c, cc := mount(t)
	const window, giveUpAfter = time.Minute, 3
	var failures []time.Time
	for i := range 4 {
		failures = append(failures, c.Now())
		var recent int
		for _, at := range failures {
			if c.Now().Sub(at) <= window {
				recent++
			}
		}
		if recent >= giveUpAfter {
			if i != 2 {
				t.Fatalf("gave up at failure %d, want 2", i)
			}
			return
		}
		tm, _ := c.After(cc, time.Duration(1<<i)*time.Second)
		f.Advance(time.Duration(1<<i) * time.Second)
		if _, ok := fired(tm); !ok {
			t.Fatal("the backoff did not elapse")
		}
	}
	t.Fatal("never gave up")
}
