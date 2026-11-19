// SPDX-License-Identifier: Apache-2.0

package concurrency

// The graph-lock waits for dependants and children without a parked goroutine
// and names a blocked awaited fiber on every waiter (ADR-034, graph-lock wait
// and blame rule).

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

type chainFixture struct {
	grand, mid, dep uint64
	release         func()
}

// hungChain mounts grandparent <- provider <- dependant, where the dependant's
// cleanup ignores its context and blocks until release.
func hungChain(t *testing.T, g Runtime) chainFixture {
	t.Helper()
	gate := make(chan struct{})
	var once sync.Once
	f := chainFixture{release: func() { once.Do(func() { close(gate) }) }}
	t.Cleanup(f.release)
	ctx := context.Background()
	f.grand = graphMount(t, ctx, g, Spec{ID: "grandparent", Provides: []string{"a"}, Apply: func(c *Context) error { return c.Provide("a", 1) }})
	f.mid = graphMount(t, ctx, g, Spec{ID: "provider", Needs: []string{"a"}, Provides: []string{"b"}, Apply: func(c *Context) error { return c.Provide("b", 2) }})
	f.dep = graphMount(t, ctx, g, Spec{ID: "dependant", Needs: []string{"b"}, Apply: func(c *Context) error {
		return c.Defer("hang", func(context.Context) error { <-gate; return nil })
	}})
	graphSettle(t, ctx, g)
	return f
}

func waitEvents(t *testing.T, g Runtime, fiber uint64, kind string) []Event {
	t.Helper()
	var out []Event
	for _, e := range eventsOf(t, g, kind, "") {
		if e.Fiber == fiber {
			out = append(out, e)
		}
	}
	return out
}

func TestGraphLockBlockedDependantIsNamedOnEveryAncestor(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := graphTestRuntime(t).(*GraphLock)
		ctx := context.Background()
		f := hungChain(t, g)
		if err := g.Dispose(ctx, f.grand); err != nil {
			t.Fatal(err)
		}
		// Before any deadline: the chain is simply unloading, nothing is blamed.
		time.Sleep(time.Second)
		for _, id := range []uint64{f.grand, f.mid, f.dep} {
			if s := graphSnapshotByID(t, g, id); s.State != Unloading || s.Error != "" {
				t.Fatalf("fiber %d before any deadline: %s %q", id, s.State, s.Error)
			}
		}
		time.Sleep(2 * time.Second)
		grand, mid, dep := graphSnapshotByID(t, g, f.grand), graphSnapshotByID(t, g, f.mid), graphSnapshotByID(t, g, f.dep)
		for _, s := range []Snapshot{grand, mid, dep} {
			if s.State != Unloading {
				t.Fatalf("%s is %s, want UNLOADING (a deadline never fakes completion)", s.Name, s.State)
			}
		}
		if !strings.Contains(dep.Error, "cleanup deadline exceeded") {
			t.Fatalf("hung dependant: %q", dep.Error)
		}
		if !strings.Contains(mid.Error, fmt.Sprintf("dependant fiber %d (dependant) is blocked in cleanup", f.dep)) {
			t.Fatalf("provider does not name the hung dependant and stage: %q", mid.Error)
		}
		if !strings.Contains(grand.Error, fmt.Sprintf("dependant fiber %d (provider) is blocked in awaiting dependants", f.mid)) {
			t.Fatalf("grandparent does not name the provider it waits for: %q", grand.Error)
		}
		// A waiter claims no deadline of its own.
		for _, id := range []uint64{f.grand, f.mid} {
			if e := waitEvents(t, g, id, "deadline:cleanup"); len(e) != 0 {
				t.Fatalf("waiter %d claims a cleanup deadline: %v", id, e)
			}
			if e := waitEvents(t, g, id, "deadline:lease"); len(e) != 0 {
				t.Fatalf("waiter %d claims a lease deadline: %v", id, e)
			}
		}
		if e := waitEvents(t, g, f.mid, "wait:dependant"); len(e) != 1 || e[0].UID != f.dep || e[0].Key != "cleanup" {
			t.Fatalf("provider wait event: %+v", e)
		}
		if e := waitEvents(t, g, f.grand, "wait:dependant"); len(e) != 1 || e[0].UID != f.mid {
			t.Fatalf("grandparent wait event: %+v", e)
		}
		if e := waitEvents(t, g, f.dep, "deadline:cleanup"); len(e) != 1 {
			t.Fatalf("hung dependant deadline events: %v", e)
		}
		// Settle returns promptly: one diagnostic per blocked fiber, in order.
		sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		started := time.Now()
		err := g.Settle(sctx)
		if err == nil || errors.Is(err, context.DeadlineExceeded) || time.Since(started) != 0 {
			t.Fatalf("Settle err = %v after %v, want an immediate diagnostic", err, time.Since(started))
		}
		lines := strings.Split(err.Error(), "\n")
		if len(lines) != 3 || !strings.HasPrefix(lines[0], "fiber 1 (grandparent)") || !strings.HasPrefix(lines[1], "fiber 2 (provider)") || !strings.HasPrefix(lines[2], "fiber 3 (dependant): blocked past its cleanup deadline") {
			t.Fatalf("Settle diagnostics: %q", lines)
		}
		g.mu.Lock()
		blocked, counted := len(g.blocked), g.nblocked
		g.mu.Unlock()
		if blocked != 3 || counted != 3 {
			t.Fatalf("blocked accounting: set=%d counter=%d, want 3", blocked, counted)
		}
		// Genuine condition gone once the disposer returns: everything unwinds.
		f.release()
		synctest.Wait()
		if err := g.Settle(sctx); err != nil {
			t.Fatalf("Settle after the disposer returned: %v", err)
		}
		if s := graphSnapshotByID(t, g, f.grand); s.State != Disposed {
			t.Fatalf("grandparent is %s after release", s.State)
		}
		for _, id := range []uint64{f.mid, f.dep} { // their provider is gone: they wait, they are not disposed
			if s := graphSnapshotByID(t, g, id); s.State != Pending {
				t.Fatalf("fiber %d is %s after release, want PENDING", id, s.State)
			}
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		if len(g.blocked) != 0 || g.nblocked != 0 || g.busy != 0 {
			t.Fatalf("accounting after recovery: blocked=%d nblocked=%d busy=%d", len(g.blocked), g.nblocked, g.busy)
		}
	})
}

// A fiber's budgets measure its own work: a provider that waits for a slow but
// healthy dependant, and then spends nearly its own budget, is blamed for nothing.
func TestGraphLockWaitingForHealthyDependantNeverBlames(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := graphTestRuntime(t).(*GraphLock)
		ctx := context.Background()
		slow := func(label string) func(*Context) error {
			return func(c *Context) error {
				return c.Defer(label, func(context.Context) error { time.Sleep(1900 * time.Millisecond); return nil })
			}
		}
		provider := graphMount(t, ctx, g, Spec{ID: "provider", Provides: []string{"k"}, Apply: func(c *Context) error {
			if err := slow("provider-cleanup")(c); err != nil {
				return err
			}
			return c.Provide("k", 1)
		}})
		dependant := graphMount(t, ctx, g, Spec{ID: "dependant", Needs: []string{"k"}, Apply: slow("dependant-cleanup")})
		graphSettle(t, ctx, g)
		if err := g.Dispose(ctx, provider); err != nil {
			t.Fatal(err)
		}
		time.Sleep(1950 * time.Millisecond) // the dependant is nearly done, the provider has not started its own cleanup
		bounded, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
		err := g.Settle(bounded)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "fiber") {
			t.Fatalf("Settle with only healthy fibers mid-transition: %v", err)
		}
		time.Sleep(10 * time.Second)
		if err := g.Settle(ctx); err != nil {
			t.Fatal(err)
		}
		// 3.8 s of cleanup in total, but no fiber exceeded its own 2 s budget.
		for _, id := range []uint64{provider, dependant} {
			s := graphSnapshotByID(t, g, id)
			if s.Error != "" {
				t.Fatalf("fiber %d %q carries %q", id, s.Name, s.Error)
			}
		}
		for _, kind := range []string{"deadline:cleanup", "deadline:lease", "wait:dependant", "wait:child"} {
			if e := eventsOf(t, g, kind, ""); len(e) != 0 {
				t.Fatalf("%s events on a healthy unload: %v", kind, e)
			}
		}
	})
}

func TestGraphLockParentBlamesBlockedChild(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := graphTestRuntime(t).(*GraphLock)
		ctx := context.Background()
		gate := make(chan struct{})
		var once sync.Once
		release := func() { once.Do(func() { close(gate) }) }
		t.Cleanup(release)
		parent := graphMount(t, ctx, g, Spec{ID: "parent", Apply: func(c *Context) error {
			_, err := c.MountChild(Spec{ID: "child", Apply: func(c *Context) error {
				return c.Defer("hang", func(context.Context) error { <-gate; return nil })
			}})
			return err
		}})
		graphSettle(t, ctx, g)
		if err := g.Dispose(ctx, parent); err != nil {
			t.Fatal(err)
		}
		time.Sleep(3 * time.Second)
		p := graphSnapshotByID(t, g, parent)
		if p.State != Unloading || !strings.Contains(p.Error, "awaiting children: child fiber 2 (child) is blocked in cleanup") {
			t.Fatalf("parent: %s %q", p.State, p.Error)
		}
		if e := waitEvents(t, g, parent, "wait:child"); len(e) != 1 || e[0].UID != 2 {
			t.Fatalf("parent wait event: %+v", e)
		}
		err := g.Settle(ctx)
		if err == nil || !strings.Contains(err.Error(), "fiber 1 (parent): awaiting children") || !strings.Contains(err.Error(), "fiber 2 (child): blocked past its cleanup deadline") {
			t.Fatalf("Settle: %v", err)
		}
		release()
		synctest.Wait()
		graphSettle(t, ctx, g)
		if s := graphSnapshotByID(t, g, parent); s.State != Disposed {
			t.Fatalf("parent is %s after the child returned", s.State)
		}
	})
}

// A fiber that starts waiting when its awaited fiber is already blocked is told
// at once, not only when a later event happens.
func TestGraphLockWaiterStartingLateIsBlamedAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := graphTestRuntime(t).(*GraphLock)
		ctx := context.Background()
		f := hungChain(t, g)
		if err := g.Dispose(ctx, f.dep); err != nil {
			t.Fatal(err)
		}
		time.Sleep(3 * time.Second)
		if s := graphSnapshotByID(t, g, f.mid); s.State != Active || s.Error != "" {
			t.Fatalf("provider before its own disposal: %s %q", s.State, s.Error)
		}
		if err := g.Dispose(ctx, f.mid); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if s := graphSnapshotByID(t, g, f.mid); s.State != Unloading || !strings.Contains(s.Error, "dependant fiber 3 (dependant) is blocked in cleanup") {
			t.Fatalf("provider that began waiting late: %s %q", s.State, s.Error)
		}
		if err := g.Settle(ctx); err == nil || !strings.Contains(err.Error(), "fiber 2 (provider)") {
			t.Fatalf("Settle: %v", err)
		}
	})
}

// Close gives up with the caller's context while a healthy disposer is still
// running; the observer dispatcher is closed when the runtime drains, without
// another Close call, and a later Close still reports cleanly.
func TestGraphLockCloseClosesObserversWhenRuntimeDrainsLater(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := graphTestRuntime(t).(*GraphLock)
		ctx := context.Background()
		graphMount(t, ctx, g, Spec{ID: "slow", Apply: func(c *Context) error {
			return c.Defer("slow", func(context.Context) error { time.Sleep(time.Second); return nil })
		}})
		graphSettle(t, ctx, g)
		bounded, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		err := g.Close(bounded)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Close err = %v, want the context error", err)
		}
		if g.ObserverStatus().Closing {
			t.Fatal("observer dispatcher closed while the runtime was still draining")
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if !g.ObserverStatus().Closing {
			t.Fatal("observer dispatcher still open after the runtime drained")
		}
		if err := g.Close(ctx); err != nil {
			t.Fatalf("later Close: %v", err)
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.lateClose {
			t.Fatal("late close still armed")
		}
	})
}

// A Close that succeeds first time leaves nothing armed and starts no goroutine.
func TestGraphLockCleanCloseLeavesNoLateClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := graphTestRuntime(t).(*GraphLock)
		ctx := context.Background()
		graphMount(t, ctx, g, Spec{ID: "a"})
		graphSettle(t, ctx, g)
		if err := g.Close(ctx); err != nil {
			t.Fatal(err)
		}
		g.mu.Lock()
		armed := g.lateClose
		g.mu.Unlock()
		if armed {
			t.Fatal("late close armed by a Close that succeeded")
		}
	})
}

// The wait for dependants parks no goroutine: with the leaf of a long chain
// held, every other fiber is waiting and the goroutine count stays flat. The
// parked implementation needed one goroutine per waiting fiber (9,137 on the
// 10,000-fiber chain restart of the first scale diagnostic).
func TestGraphLockChainUnloadParksNoGoroutines(t *testing.T) {
	const n = 2000
	synctest.Test(t, func(t *testing.T) {
		g := graphTestRuntime(t).(*GraphLock)
		ctx := context.Background()
		gate := make(chan struct{})
		var once sync.Once
		release := func() { once.Do(func() { close(gate) }) }
		t.Cleanup(release)
		var ids []uint64
		for i := 0; i < n; i++ {
			key, prev := fmt.Sprintf("k%d", i), fmt.Sprintf("k%d", i-1)
			spec := Spec{ID: fmt.Sprintf("n%d", i), Provides: []string{key}}
			if i > 0 {
				spec.Needs = []string{prev}
			}
			last := i == n-1
			spec.Apply = func(c *Context) error {
				if last {
					if err := c.Defer("hold", func(context.Context) error { <-gate; return nil }); err != nil {
						return err
					}
				}
				return c.Provide(key, i)
			}
			ids = append(ids, graphMount(t, ctx, g, spec))
		}
		graphSettle(t, ctx, g)
		synctest.Wait()
		base := runtime.NumGoroutine()
		if err := g.Update(ctx, ids[0]); err != nil { // restart the root of the chain
			t.Fatal(err)
		}
		synctest.Wait()
		g.mu.Lock()
		parked := 0
		for _, f := range g.fibers {
			if f.current != nil && f.current.wait == graphWaitDependants {
				parked++
			}
		}
		g.mu.Unlock()
		if parked != n-1 {
			t.Fatalf("%d fibers parked on their dependants, want %d", parked, n-1)
		}
		if extra := runtime.NumGoroutine() - base; extra > 10 {
			t.Fatalf("%d goroutines for %d waiting fibers", extra, parked)
		}
		release()
		synctest.Wait()
		graphSettle(t, ctx, g)
		snaps, err := g.Snapshots(ctx)
		if err != nil {
			t.Fatal(err)
		}
		active := 0
		for _, s := range snaps {
			if s.State == Active {
				active++
			}
		}
		if active != len(ids) {
			t.Fatalf("%d of %d fibers ACTIVE after the restart", active, len(ids))
		}
	})
}
