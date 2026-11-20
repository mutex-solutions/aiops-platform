// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestBareHandleAnswersWhatItCannotServe(t *testing.T) {
	h := newHandle(nil, context.Background(), 7, nil)
	if err := h.requestDispose(); !errors.Is(err, ErrInactive) {
		t.Fatalf("requestDispose on a handle with no owner link = %v", err)
	}
}

func TestEffectKeepsTheBridgeRefusalAndTheCleanupFailure(t *testing.T) {
	refusal, cleanupFailure := errors.New("bridge refused"), errors.New("cleanup failed")
	h := newHandle(nil, context.Background(), 1, nil)
	h.live.Store(true)
	h.effectFn = func(string, disposer) error { return refusal }
	var cleaned int
	h.cleanupFn = func(dispose disposer) error { cleaned++; return cleanupFailure }
	err := h.effect("resource", func() (disposer, error) {
		return func(context.Context) error { return nil }, nil
	})
	if !errors.Is(err, refusal) || !errors.Is(err, cleanupFailure) || errors.Is(err, ErrInactive) || cleaned != 1 {
		t.Fatalf("effect = %v (cleanups %d), want the refusal and the cleanup failure", err, cleaned)
	}
}

func TestTrackedWorkerPanicWithAnErrorKeepsItsIdentity(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := hazardReconciler(t)
		boom := errors.New("worker boom")
		id := rcMount(t, r, Spec{ID: "w", Apply: func(c *Handle) error {
			return c.Go("worker", func(context.Context) error { panic(boom) })
		}})
		synctest.Wait() // The worker panicked and the owner took its completion.
		if err := r.Settle(rcGuard(t)); !errors.Is(err, boom) {
			t.Fatalf("Settle = %v, want the failed activation with the panic value's identity", err)
		}
		snap := rcSnapshot(t, r, id)
		if snap.State != Failed || !strings.Contains(snap.Error, `tracked goroutine "worker" panic: worker boom`) {
			t.Fatalf("state=%s error=%q", snap.State, snap.Error)
		}
		if err := r.(Waiter).AwaitFiber(rcGuard(t), id); !errors.Is(err, boom) {
			t.Fatalf("await = %v, want the panic value's identity", err)
		}
	})
}

func TestOwnerRefusesMalformedRequestsFromALiveActivation(t *testing.T) {
	r := rcRuntime(t)
	type outcome struct{ tracked, scope, child, childID error }
	got := make(chan outcome, 1)
	rcMount(t, r, Spec{ID: "w", Apply: func(c *Handle) error {
		var o outcome
		_, o.tracked = c.trackedFn("")
		_, o.scope = c.scopeFn("", nil)
		_, o.child = c.MountChild(Spec{ID: "bad", Needs: []string{""}})
		got <- o
		return nil
	}})
	o := <-got
	if o.tracked == nil || !strings.Contains(o.tracked.Error(), "empty tracked work label") || errors.Is(o.tracked, ErrInactive) {
		t.Fatalf("empty tracked label = %v", o.tracked)
	}
	if o.scope == nil || !strings.Contains(o.scope.Error(), "nonempty label") {
		t.Fatalf("empty scope request = %v", o.scope)
	}
	if o.child == nil || !strings.Contains(o.child.Error(), "nil or incomparable declaration") {
		t.Fatalf("invalid child declaration = %v", o.child)
	}
	rcSettle(t, r)
	snaps, err := r.Snapshots(rcGuard(t))
	if err != nil || len(snaps) != 1 {
		t.Fatalf("a refused child was created: %v %+v", err, snaps)
	}
}

// A call that passed its own liveness check and then lost the race with disposal
// reaches the owner on a stale activation: every bridge answers ErrInactive.
func TestOwnerAnswersAStaleActivationWithErrInactive(t *testing.T) {
	r := rcRuntime(t)
	handles := make(chan *Handle, 1)
	id := rcMount(t, r, Spec{ID: "w", Apply: func(c *Handle) error { handles <- c; return nil }})
	h := <-handles
	rcSettle(t, r)
	if err := r.Dispose(rcGuard(t), id); err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
	if _, err := h.childFn(pluginSpec{ID: "late"}); !errors.Is(err, ErrInactive) {
		t.Fatalf("childFn = %v", err)
	}
	if _, err := h.scopeFn("late", func(*EffectScope) error { return nil }); !errors.Is(err, ErrInactive) {
		t.Fatalf("scopeFn = %v", err)
	}
	if _, err := h.trackedFn("late"); !errors.Is(err, ErrInactive) {
		t.Fatalf("trackedFn = %v", err)
	}
}

func TestScopeCallbacksRefuseMalformedRegistrations(t *testing.T) {
	r := rcRuntime(t)
	gate := make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(open)
	type outcome struct{ deferNone, deferNil, nested error }
	got := make(chan outcome, 1)
	handles := make(chan *EffectHandle, 1)
	rcMount(t, r, Spec{ID: "w", Apply: func(c *Handle) error {
		eh, err := c.RequestEffectScope("scope", func(s *EffectScope) error {
			<-gate
			var o outcome
			o.deferNone = s.Defer("", func(context.Context) error { return nil })
			o.deferNil = s.Defer("resource", nil)
			o.nested = s.Scope("", nil)
			got <- o
			return nil
		})
		handles <- eh
		return err
	}})
	eh := <-handles
	ended, cancel := context.WithCancel(context.Background())
	cancel()
	if err := eh.AwaitSetup(ended); !errors.Is(err, context.Canceled) {
		t.Fatalf("AwaitSetup with an ended context while setup runs = %v", err)
	}
	open()
	if err := eh.AwaitSetup(rcGuard(t)); err != nil {
		t.Fatal(err)
	}
	o := <-got
	for name, err := range map[string]error{"empty label": o.deferNone, "nil disposer": o.deferNil, "nested scope": o.nested} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	rcSettle(t, r)
}

// Disposal that arrives while a scope's setup runs ends the activation as soon as
// that setup returns: the fiber is never published as ACTIVE.
func TestDisposalDuringScopeSetupSkipsActive(t *testing.T) {
	r := rcRuntime(t)
	gate := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(open)
	handles := make(chan *EffectHandle, 1)
	id := rcMount(t, r, Spec{ID: "w", Apply: func(c *Handle) error {
		eh, err := c.RequestEffectScope("scope", func(*EffectScope) error {
			close(started)
			<-gate
			return nil
		})
		handles <- eh
		return err
	}})
	eh := <-handles
	<-started
	if err := r.Dispose(rcGuard(t), id); err != nil {
		t.Fatal(err)
	}
	open()
	rcSettle(t, r)
	if snap := rcSnapshot(t, r, id); snap.State != Disposed {
		t.Fatalf("state = %s", snap.State)
	}
	if err := eh.AwaitSetup(rcGuard(t)); err != nil {
		t.Fatalf("setup that returned nil = %v", err)
	}
	if err := eh.Await(rcGuard(t)); err != nil {
		t.Fatalf("scope cleanup = %v", err)
	}
	for _, e := range eventsOf(t, r, "state", "") {
		if e.Fiber == id && e.State == Active {
			t.Fatalf("the fiber was published ACTIVE: %v", kinds(eventsOf(t, r, "state", "")))
		}
	}
}

// A self-disposing activation that still holds a lease is released by the lease's
// last leave, whose notice the owner reads after the self-disposal flag is set.
func TestSelfDisposalWithAnOpenLeaseIsFinishedByTheLastLeave(t *testing.T) {
	r := rcRuntime(t)
	id := rcMount(t, r, Spec{ID: "w", Apply: func(c *Handle) error {
		if !c.Enter() {
			return errors.New("enter refused")
		}
		defer c.Leave()
		return c.Dispose()
	}})
	rcSettle(t, r)
	if snap := rcSnapshot(t, r, id); snap.State != Disposed {
		t.Fatalf("state = %s", snap.State)
	}
}

// A batch update of a provider invalidates its consumers, which cancels a check
// those consumers still have in flight.
func TestBatchUpdateCancelsAConsumersCheckInFlight(t *testing.T) {
	r := rcRuntime(t)
	var calls atomic.Int32
	inCheck := make(chan *Handle, 1)
	canceled := make(chan struct{})
	provider := rcMount(t, r, Spec{ID: "p", Provides: []string{"K"}, Apply: func(c *Handle) error { return c.Provide("K", 1) }})
	rcMount(t, r, Spec{ID: "c", Needs: []string{"K"}, Check: func(h *Handle) bool {
		if calls.Add(1) == 2 {
			inCheck <- h
			<-h.Std().Done()
			close(canceled)
		}
		return true
	}})
	rcSettle(t, r)
	if err := r.(Notifier).Notify(rcGuard(t), "K"); err != nil {
		t.Fatal(err)
	}
	<-inCheck // The recheck is running and blocked.
	if _, err := r.(Batcher).Batch(rcGuard(t), []BatchOperation{{Kind: BatchUpdate, Fiber: provider}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	case <-rcGuard(t).Done():
		t.Fatal("the in-flight check was not canceled by the batch")
	}
	rcSettle(t, r)
	for _, s := range mustSnapshots(t, r) {
		if s.State != Active {
			t.Fatalf("%s is %s after the batch settled", s.Name, s.State)
		}
	}
}

func mustSnapshots(t *testing.T, r Runtime) []Snapshot {
	t.Helper()
	snaps, err := r.Snapshots(rcGuard(t))
	if err != nil {
		t.Fatal(err)
	}
	return snaps
}

// Checks that outlive their deadline keep holding their provider's bindings. The
// provider's drain then waits on both consumers, and Settle names it.
func TestHungChecksHoldTheirProviderAndSettleNamesTheWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := hazardReconciler(t)
		gate := make(chan struct{})
		var once sync.Once
		open := func() { once.Do(func() { close(gate) }) }
		t.Cleanup(open)
		provider := rcMount(t, r, Spec{ID: "p", Provides: []string{"K"}, Apply: func(c *Handle) error { return c.Provide("K", 1) }})
		hung := Spec{Needs: []string{"K"}, Check: func(*Handle) bool { <-gate; return true }}
		hung.ID = "c1"
		c1 := rcMount(t, r, hung)
		hung.ID = "c2"
		c2 := rcMount(t, r, hung)
		synctest.Wait()
		time.Sleep(testLoad + time.Second) // Both checks overran their deadline.
		if err := r.Dispose(context.Background(), provider); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		err := r.Settle(rcGuard(t))
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Settle = %v", err)
		}
		for _, want := range []string{"check deadline", "drain: waiting for fiber"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("Settle = %q, missing %q", err, want)
			}
		}
		open()
		synctest.Wait()
		rcSettle(t, r)
		for _, id := range []uint64{provider, c1, c2} {
			if snap := rcSnapshot(t, r, id); id == provider && snap.State != Disposed {
				t.Fatalf("provider is %s", snap.State)
			}
		}
	})
}

// A new check can start while the old activation's cleanup is gated, and the
// fiber is disposed before that check returns. Its check deadline then keeps the
// fiber, and the links of its old activation, until the check ends.
func TestCheckStartedDuringGatedCleanupKeepsTheDisposedFiberUntilItEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := hazardReconciler(t)
		cleanupGate, checkGate := make(chan struct{}), make(chan struct{})
		var cleanupOnce, checkOnce sync.Once
		openCleanup := func() { cleanupOnce.Do(func() { close(cleanupGate) }) }
		openCheck := func() { checkOnce.Do(func() { close(checkGate) }) }
		t.Cleanup(openCleanup)
		t.Cleanup(openCheck)
		var calls atomic.Int32
		rcMount(t, r, Spec{ID: "p", Provides: []string{"K"}, Apply: func(c *Handle) error { return c.Provide("K", 1) }})
		consumer := rcMount(t, r, Spec{ID: "c", Needs: []string{"K"},
			Check: func(*Handle) bool {
				if calls.Add(1) >= 2 { // The restart reuses the checked bindings: this is the Notify's check.
					<-checkGate
				}
				return true
			},
			Apply: func(c *Handle) error {
				return c.Defer("gated", func(context.Context) error { <-cleanupGate; return nil })
			}})
		rcSettle(t, r)
		ctx := context.Background()
		if err := r.Update(ctx, consumer); err != nil {
			t.Fatal(err)
		}
		synctest.Wait() // The restart's cleanup waits on its gate.
		if err := r.(Notifier).Notify(ctx, "K"); err != nil {
			t.Fatal(err)
		}
		synctest.Wait() // The notified check now waits on its gate.
		if err := r.Dispose(ctx, consumer); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		openCleanup()
		synctest.Wait()
		if snap := rcSnapshot(t, r, consumer); snap.State == Disposed {
			t.Fatalf("the fiber was disposed while its check was still running: %+v", snap)
		}
		time.Sleep(testLoad + time.Second)
		err := r.Settle(rcGuard(t))
		if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "check deadline") {
			t.Fatalf("Settle = %v, want the check deadline", err)
		}
		openCheck()
		synctest.Wait()
		rcSettle(t, r)
		if snap := rcSnapshot(t, r, consumer); snap.State != Disposed {
			t.Fatalf("state after the check returned = %s", snap.State)
		}
	})
}

// A lease released after the drain budget, by an owner that only handles it
// later, is an overrun of the activation's own drain, not of the owner. A check in
// flight keeps the fiber draining, so the owner judges the budget before it can
// finish the drain.
func TestDrainJudgesAReleasedLeaseByItsReleaseInstant(t *testing.T) {
	run := func(t *testing.T, releaseAt time.Duration) []ledgerEvent {
		var events []ledgerEvent
		synctest.Test(t, func(t *testing.T) {
			r := hazardReconciler(t)
			checkGate := make(chan struct{})
			var once sync.Once
			openCheck := func() { once.Do(func() { close(checkGate) }) }
			t.Cleanup(openCheck)
			var calls atomic.Int32
			handles := make(chan *Handle, 1)
			rcMount(t, r, Spec{ID: "p", Provides: []string{"K"}, Apply: func(c *Handle) error { return c.Provide("K", 1) }})
			id := rcMount(t, r, Spec{ID: "c", Needs: []string{"K"},
				Check: func(*Handle) bool {
					if calls.Add(1) >= 2 {
						<-checkGate
					}
					return true
				},
				Apply: func(c *Handle) error { handles <- c; return nil }})
			h := <-handles
			rcSettle(t, r)
			if !h.Enter() {
				t.Fatal("enter refused on a live activation")
			}
			if err := r.(Notifier).Notify(context.Background(), "K"); err != nil {
				t.Fatal(err)
			}
			synctest.Wait() // The recheck waits on its gate.
			if err := r.Dispose(context.Background(), id); err != nil {
				t.Fatal(err)
			}
			synctest.Wait() // Draining: the lease and the check hold the activation.
			stall := newStallCtx(2)
			go func() { _, _ = r.Snapshots(stall) }()
			synctest.Wait()
			time.Sleep(releaseAt)
			openCheck() // Only the lease can be late.
			synctest.Wait()
			h.Leave()
			time.Sleep(testDrain + time.Second - releaseAt)
			close(stall.gate)
			synctest.Wait()
			events = eventsOf(t, r, "deadline", "")
		})
		return events
	}
	if events := run(t, testDrain-time.Second); len(events) != 0 {
		t.Fatalf("a lease released in time was failed: %v", kinds(events))
	}
	if got := kinds(run(t, testDrain+time.Millisecond)); len(got) != 1 || got[0] != "deadline/drain" {
		t.Fatalf("deadline events = %v, want deadline/drain", got)
	}
}
