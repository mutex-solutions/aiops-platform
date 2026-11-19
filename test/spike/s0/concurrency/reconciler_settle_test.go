// SPDX-License-Identifier: Apache-2.0

package concurrency

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

// Settle contract: it returns when no fiber is mid-transition except fibers
// blocked past one of their own deadlines, and never while an unblocked fiber is
// still mid-transition.

func TestReconcilerSettleWaitsForHealthyFiberBesideBlockedOne(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := hazardReconciler(t)
		hang, slow := make(chan struct{}), make(chan struct{})
		var h, s sync.Once
		t.Cleanup(func() { h.Do(func() { close(hang) }) })
		t.Cleanup(func() { s.Do(func() { close(slow) }) })
		hung := rcMount(t, r, Spec{ID: "hung", Apply: func(*Context) error { <-hang; return nil }})
		time.Sleep(1500 * time.Millisecond)
		healthy := rcMount(t, r, Spec{ID: "healthy", Apply: func(*Context) error { <-slow; return nil }})
		time.Sleep(600 * time.Millisecond) // hung is past its budget, healthy is mid-load

		done := make(chan error, 1)
		go func() { done <- r.Settle(context.Background()) }()
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("Settle returned while a healthy fiber is LOADING: %v", err)
		default:
		}
		s.Do(func() { close(slow) }) // the healthy fiber finishes inside its budget
		synctest.Wait()
		err := <-done
		if s := rcSnapshot(t, r, healthy); s.State != Active {
			t.Fatalf("healthy fiber is %s when Settle returned", s.State)
		}
		if err == nil || !strings.Contains(err.Error(), "fiber 1 load deadline") || strings.Contains(err.Error(), "fiber 2") {
			t.Fatalf("Settle = %v, want exactly the hung fiber %d's diagnostic", err, hung)
		}
		if n := strings.Count(err.Error(), "fiber 1 load deadline"); n != 1 {
			t.Fatalf("diagnostic repeated %d times: %q", n, err)
		}
	})
}

func TestReconcilerSettleCancelledReturnsContextErrorWithDiagnostics(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := hazardReconciler(t)
		hang, slow := make(chan struct{}), make(chan struct{})
		var h, s sync.Once
		t.Cleanup(func() { h.Do(func() { close(hang) }) })
		t.Cleanup(func() { s.Do(func() { close(slow) }) })
		rcMount(t, r, Spec{ID: "hung", Apply: func(*Context) error { <-hang; return nil }})
		time.Sleep(1500 * time.Millisecond)
		rcMount(t, r, Spec{ID: "healthy", Apply: func(*Context) error { <-slow; return nil }})
		time.Sleep(600 * time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		err := r.Settle(ctx)
		if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "fiber 1 load deadline") {
			t.Fatalf("Settle = %v, want the context error joined with the hung fiber's diagnostic", err)
		}
		// A context that is already cancelled when the owner dequeues Settle.
		pre, stop := context.WithCancel(context.Background())
		go func() { synctest.Wait(); stop() }()
		if err := r.Settle(pre); !errors.Is(err, context.Canceled) {
			t.Fatalf("Settle = %v, want context.Canceled", err)
		}
	})
}

// A parent waiting for a child, and a provider waiting for a dependant, count as
// blocked when what they wait for is blocked: Settle does not hang on them.
func TestReconcilerSettleReturnsForWaitersOfBlockedFiber(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := hazardReconciler(t)
		hang := make(chan struct{})
		var once sync.Once
		t.Cleanup(func() { once.Do(func() { close(hang) }) })
		provider := rcMount(t, r, Spec{ID: "provider", Provides: []string{"k"}, Apply: func(c *Context) error { return c.Provide("k", 1) }})
		rcMount(t, r, Spec{ID: "dependant", Needs: []string{"k"}, Apply: func(c *Context) error {
			return c.Defer("hang", func(context.Context) error { <-hang; return nil })
		}})
		rcSettle(t, r)
		if err := r.Dispose(context.Background(), provider); err != nil {
			t.Fatal(err)
		}
		err := r.Settle(rcGuard(t)) // fake time passes until the dependant's dispose budget lapses
		if err == nil || !strings.Contains(err.Error(), "fiber 2 dispose deadline") || !strings.Contains(err.Error(), "fiber 1 drain: waiting for fiber 2") {
			t.Fatalf("Settle = %v, want the dependant's deadline and the provider naming it", err)
		}
		once.Do(func() { close(hang) })
		synctest.Wait()
		if err := r.Settle(rcGuard(t)); err != nil && strings.Contains(err.Error(), "waiting for") {
			t.Fatalf("stale waiter diagnostic after recovery: %v", err)
		}
	})
}

// Close follows the same rule, and a Close that returns early still releases the
// observer dispatcher once the runtime has drained, even when its own context
// ended first.
func TestReconcilerCloseReleasesObserversAfterContextEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := hazardReconciler(t)
		gate := make(chan struct{})
		var once sync.Once
		t.Cleanup(func() { once.Do(func() { close(gate) }) })
		var ran atomic.Bool
		rcMount(t, r, Spec{ID: "slow", Apply: func(c *Context) error {
			return c.Defer("wait", func(context.Context) error { <-gate; ran.Store(true); return nil })
		}})
		rcSettle(t, r)
		status := func() bool { return r.(interface{ ObserverStatus() ObserverStatus }).ObserverStatus().Closing }
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		err := r.Close(ctx)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Close = %v, want the context error", err)
		}
		if status() {
			t.Fatal("observers closed while the runtime still has a fiber")
		}
		once.Do(func() { close(gate) })
		synctest.Wait()
		if !ran.Load() || !status() {
			t.Fatalf("after drain: disposer ran=%v observers closing=%v, want both", ran.Load(), status())
		}
	})
}

// A parked AwaitFiber or Settle costs nothing per owner turn: the number of
// times the waiters look at their contexts does not grow with the events
// appended while they wait.
func TestReconcilerParkedWaitersCostNothingPerTurn(t *testing.T) {
	for _, kind := range []string{"AwaitFiber", "Settle"} {
		t.Run(kind, func(t *testing.T) {
			few := g3Count(t, 0, kind, 50, 1)
			many := g3Count(t, 0, kind, 50, 40)
			if few != many {
				t.Fatalf("context checks grew with events: 1 event = %+v, 40 events = %+v", few, many)
			}
		})
	}
}
