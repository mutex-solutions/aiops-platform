// SPDX-License-Identifier: Apache-2.0

package concurrency

// Targeted wake-ups of the graph-lock waiters (AwaitFiber, Settle). wakeCtx
// (hazards_graph_test.go) counts a waiter's looks at its context: one when it
// parks and one per wake-up, so a count above the waiter count is a wake-up.

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
)

type wakeGates struct {
	mu    sync.Mutex
	gates map[string]chan struct{}
}

func newWakeGates(t *testing.T, names ...string) *wakeGates {
	w := &wakeGates{gates: map[string]chan struct{}{}}
	for _, n := range names {
		w.gates[n] = make(chan struct{})
	}
	t.Cleanup(func() {
		for n := range w.gates {
			w.open(n)
		}
	})
	return w
}

func (w *wakeGates) open(name string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ch := w.gates[name]; ch != nil {
		close(ch)
		w.gates[name] = nil
	}
}

func (w *wakeGates) wait(name string) {
	w.mu.Lock()
	ch := w.gates[name]
	w.mu.Unlock()
	if ch != nil {
		<-ch
	}
}

func TestGraphLockAwaitFiberWokenOnlyByItsFiber(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := graphTestRuntime(t).(*GraphLock)
		gates := newWakeGates(t, "x", "y")
		ctx := context.Background()
		x := graphMount(t, ctx, g, Spec{ID: "x", Apply: func(*Context) error { gates.wait("x"); return nil }})
		y := graphMount(t, ctx, g, Spec{ID: "y", Apply: func(*Context) error { gates.wait("y"); return nil }})
		synctest.Wait()
		const waiters = 20
		parent, cancel := context.WithCancel(ctx)
		defer cancel()
		var onX, onY [waiters]*wakeCtx
		results := make(chan error, 2*waiters)
		for i := 0; i < waiters; i++ {
			onX[i], onY[i] = &wakeCtx{Context: parent}, &wakeCtx{Context: parent}
			go func() { results <- g.AwaitFiber(onX[i], x) }()
			go func() { results <- g.AwaitFiber(onY[i], y) }()
		}
		synctest.Wait()
		count := func(set [waiters]*wakeCtx) (n int64) {
			for _, c := range set {
				n += c.done.Load()
			}
			return n
		}
		// Unrelated churn: events, mounts and disposals of other fibers.
		for i := 0; i < 5; i++ {
			if err := g.Record(ctx, "tick", nil); err != nil {
				t.Fatal(err)
			}
			other := graphMount(t, ctx, g, Spec{ID: "other"})
			if err := g.AwaitFiber(ctx, other); err != nil {
				t.Fatal(err)
			}
			if err := g.Dispose(ctx, other); err != nil {
				t.Fatal(err)
			}
			if err := g.AwaitFiber(ctx, other); err != nil {
				t.Fatal(err)
			}
			synctest.Wait()
		}
		if a, b := count(onX), count(onY); a != waiters || b != waiters {
			t.Fatalf("unrelated fibers woke waiters: Done calls on x=%d y=%d, want %d each", a, b, waiters)
		}
		// Y changes: only its waiters wake, and they return.
		gates.open("y")
		synctest.Wait()
		if a, b := count(onX), count(onY); a != waiters || b != waiters {
			t.Fatalf("y completing: Done calls on x=%d y=%d, want %d each (a waiter that returns looks once)", a, b, waiters)
		}
		if len(results) != waiters {
			t.Fatalf("waiters on y returned %d, want %d", len(results), waiters)
		}
		// X changes: its waiters return too.
		gates.open("x")
		synctest.Wait()
		if len(results) != 2*waiters {
			t.Fatalf("waiters returned %d, want %d", len(results), 2*waiters)
		}
		for i := 0; i < 2*waiters; i++ {
			if err := <-results; err != nil {
				t.Fatal(err)
			}
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		for _, f := range []uint64{x, y} {
			if n := len(g.fibers[f].waiters); n != 0 {
				t.Fatalf("fiber %d still has %d registered waiters", f, n)
			}
		}
	})
}

func TestGraphLockSettleWokenOnlyWhenSettleCanHold(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := graphTestRuntime(t).(*GraphLock)
		gates := newWakeGates(t, "a", "b")
		ctx := context.Background()
		graphMount(t, ctx, g, Spec{ID: "a", Apply: func(*Context) error { gates.wait("a"); return nil }})
		graphMount(t, ctx, g, Spec{ID: "b", Apply: func(*Context) error { gates.wait("b"); return nil }})
		synctest.Wait()
		const waiters = 20
		parent, cancel := context.WithCancel(ctx)
		defer cancel()
		var set [waiters]*wakeCtx
		results := make(chan error, waiters)
		for i := range set {
			set[i] = &wakeCtx{Context: parent}
			go func() { results <- g.Settle(set[i]) }()
		}
		synctest.Wait()
		done := func() (n int64) {
			for _, c := range set {
				n += c.done.Load()
			}
			return n
		}
		for i := 0; i < 5; i++ {
			if err := g.Record(ctx, "tick", nil); err != nil {
				t.Fatal(err)
			}
		}
		other := graphMount(t, ctx, g, Spec{ID: "other"})
		if err := g.AwaitFiber(ctx, other); err != nil {
			t.Fatal(err)
		}
		gates.open("a") // one of two busy fibers finishes: the condition cannot hold yet
		synctest.Wait()
		if n := done(); n != waiters || len(results) != 0 {
			t.Fatalf("Done calls = %d, returned = %d: a partial completion woke the Settle waiters", n, len(results))
		}
		gates.open("b")
		synctest.Wait()
		if len(results) != waiters {
			t.Fatalf("%d of %d Settle waiters returned once nothing was busy", len(results), waiters)
		}
		for range set {
			if err := <-results; err != nil {
				t.Fatal(err)
			}
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		if n := len(g.settleWaiters); n != 0 {
			t.Fatalf("%d Settle waiters still registered", n)
		}
	})
}

func TestGraphLockCanceledWaitersUnregister(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := graphTestRuntime(t).(*GraphLock)
		gates := newWakeGates(t, "x")
		x := graphMount(t, context.Background(), g, Spec{ID: "x", Apply: func(*Context) error { gates.wait("x"); return nil }})
		synctest.Wait()
		ctx, cancel := context.WithCancel(context.Background())
		errs := make(chan error, 2)
		go func() { errs <- g.AwaitFiber(ctx, x) }()
		go func() { errs <- g.Settle(ctx) }()
		synctest.Wait()
		cancel()
		for i := 0; i < 2; i++ {
			if err := <-errs; err != context.Canceled {
				t.Fatalf("waiter returned %v, want context.Canceled", err)
			}
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		if len(g.settleWaiters) != 0 || len(g.fibers[x].waiters) != 0 {
			t.Fatalf("canceled waiters left registered: settle=%d fiber=%d", len(g.settleWaiters), len(g.fibers[x].waiters))
		}
	})
}
