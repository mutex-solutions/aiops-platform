// SPDX-License-Identifier: Apache-2.0

package concurrency

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func graphTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func graphReceive[T any](t *testing.T, ctx context.Context, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-ctx.Done():
		t.Fatalf("barrier: %v", ctx.Err())
		var zero T
		return zero
	}
}

func graphMount(t *testing.T, ctx context.Context, g Runtime, spec Spec) uint64 {
	t.Helper()
	id, err := g.Mount(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func graphSettle(t *testing.T, ctx context.Context, g Runtime) {
	t.Helper()
	if err := g.Settle(ctx); err != nil {
		t.Fatal(err)
	}
}

func graphSnapshot(t *testing.T, ctx context.Context, g Runtime, id uint64) Snapshot {
	t.Helper()
	snapshots, err := g.Snapshots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range snapshots {
		if snapshot.ID == id {
			return snapshot
		}
	}
	t.Fatalf("missing snapshot %d", id)
	return Snapshot{}
}

func graphEventSequence(t *testing.T, ctx context.Context, g Runtime, fiber uint64, kind string) uint64 {
	t.Helper()
	events, err := g.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found uint64
	for i, event := range events {
		if event.Sequence != uint64(i+1) {
			t.Errorf("event sequence[%d] = %d", i, event.Sequence)
		}
		if event.Fiber == fiber && event.Kind == kind {
			found = event.Sequence
		}
	}
	if found == 0 {
		t.Fatalf("missing event %d %q", fiber, kind)
	}
	return found
}

func TestGraphLockLifecycle(t *testing.T) {
	ctx := graphTestContext(t)
	g := NewGraphLock()
	var mu sync.Mutex
	var calls []string
	appendCall := func(label string) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, label)
	}
	id := graphMount(t, ctx, g, Spec{ID: "lifecycle", Apply: func(c *Context) error {
		appendCall("apply")
		if err := c.Defer("first", func(context.Context) error { appendCall("first"); return nil }); err != nil {
			return err
		}
		return c.Defer("second", func(context.Context) error { appendCall("second"); return nil })
	}})
	graphSettle(t, ctx, g)
	if got := graphSnapshot(t, ctx, g, id); got.State != Active || got.Revision != 0 {
		t.Fatalf("active: %+v", got)
	}
	if err := g.Update(ctx, id); err != nil {
		t.Fatal(err)
	}
	graphSettle(t, ctx, g)
	if got := graphSnapshot(t, ctx, g, id); got.State != Active || got.Revision != 1 {
		t.Fatalf("updated: %+v", got)
	}
	if err := g.Dispose(ctx, id); err != nil {
		t.Fatal(err)
	}
	graphSettle(t, ctx, g)
	if got := graphSnapshot(t, ctx, g, id); got.State != Disposed || len(got.Effects) != 0 {
		t.Fatalf("disposed: %+v", got)
	}
	mu.Lock()
	got := append([]string(nil), calls...)
	mu.Unlock()
	want := []string{"apply", "second", "first", "apply", "second", "first"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	request := graphEventSequence(t, ctx, g, id, "dispose:request")
	gone := graphEventSequence(t, ctx, g, id, "effects:gone")
	complete := graphEventSequence(t, ctx, g, id, "dispose:complete")
	if !(request < gone && gone < complete) {
		t.Fatalf("disposal order: request %d gone %d complete %d", request, gone, complete)
	}
}

func TestGraphLockMissingDependencyAndProviderRestart(t *testing.T) {
	ctx := graphTestContext(t)
	g := NewGraphLock()
	bound := make(chan any, 4)
	consumer := graphMount(t, ctx, g, Spec{ID: "consumer", Needs: []string{"service"}, Apply: func(c *Context) error {
		value, err := c.Read("service")
		if err == nil {
			bound <- value
		}
		return err
	}})
	graphSettle(t, ctx, g)
	if got := graphSnapshot(t, ctx, g, consumer); got.State != Pending {
		t.Fatalf("missing dependency: %+v", got)
	}
	var generation atomic.Int64
	provider := graphMount(t, ctx, g, Spec{ID: "provider", Provides: []string{"service"}, Apply: func(c *Context) error {
		return c.Provide("service", generation.Add(1))
	}})
	graphSettle(t, ctx, g)
	first := graphSnapshot(t, ctx, g, consumer)
	if first.State != Active || len(first.Epoch) != 1 || graphReceive(t, ctx, bound) != int64(1) {
		t.Fatalf("first activation: %+v", first)
	}
	if err := g.Update(ctx, provider); err != nil {
		t.Fatal(err)
	}
	graphSettle(t, ctx, g)
	second := graphSnapshot(t, ctx, g, consumer)
	if second.State != Active || second.Epoch[0] == first.Epoch[0] || graphReceive(t, ctx, bound) != int64(2) {
		t.Fatalf("restart epochs: first %+v second %+v", first, second)
	}
	if err := g.Dispose(ctx, provider); err != nil {
		t.Fatal(err)
	}
	graphSettle(t, ctx, g)
	if got := graphSnapshot(t, ctx, g, consumer); got.State != Pending {
		t.Fatalf("removed provider: %+v", got)
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockBlockedCleanupRetainsProviderAndEffects(t *testing.T) {
	ctx := graphTestContext(t)
	g := NewGraphLock()
	cleanupStarted := make(chan struct{})
	releaseCleanup := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseCleanup) }) })
	providerGone := make(chan struct{}, 1)
	provider := graphMount(t, ctx, g, Spec{ID: "provider", Provides: []string{"service"}, Apply: func(c *Context) error {
		if err := c.Provide("service", "retained"); err != nil {
			return err
		}
		return c.Defer("provider-effect", func(context.Context) error { providerGone <- struct{}{}; return nil })
	}})
	consumer := graphMount(t, ctx, g, Spec{ID: "consumer", Needs: []string{"service"}, Apply: func(c *Context) error {
		return c.Defer("consumer-effect", func(context.Context) error {
			close(cleanupStarted)
			<-releaseCleanup
			value, err := c.Read("service")
			if err != nil || value != "retained" {
				return errors.New("provider not retained during dependant cleanup")
			}
			return nil
		})
	}})
	graphSettle(t, ctx, g)
	disposed := make(chan error, 1)
	go func() { disposed <- g.Dispose(ctx, provider) }()
	graphReceive(t, ctx, cleanupStarted)
	if got := graphSnapshot(t, ctx, g, consumer); got.State != Unloading || !reflect.DeepEqual(got.Effects, []string{"consumer-effect"}) {
		t.Fatalf("consumer cleanup hidden early: %+v", got)
	}
	if got := graphSnapshot(t, ctx, g, provider); got.State != Unloading || len(got.Effects) != 1 {
		t.Fatalf("provider released early: %+v", got)
	}
	select {
	case <-providerGone:
		t.Fatal("provider cleanup started before dependant completed")
	default:
	}
	// External commands must still acquire the mutex while a disposer blocks.
	other := graphMount(t, ctx, g, Spec{ID: "independent"})
	if graphSnapshot(t, ctx, g, other).State == Disposed {
		t.Fatal("independent mount disposed")
	}
	releaseOnce.Do(func() { close(releaseCleanup) })
	if err := graphReceive(t, ctx, disposed); err != nil {
		t.Fatal(err)
	}
	graphReceive(t, ctx, providerGone)
	graphSettle(t, ctx, g)
	if got := graphSnapshot(t, ctx, g, consumer); got.State != Pending || got.Error != "" {
		t.Fatalf("consumer after cleanup: %+v", got)
	}
	consumerGone := graphEventSequence(t, ctx, g, consumer, "effects:gone")
	providerEffectsGone := graphEventSequence(t, ctx, g, provider, "effects:gone")
	if consumerGone >= providerEffectsGone {
		t.Fatalf("provider reclaimed before dependant: %d >= %d", consumerGone, providerEffectsGone)
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockStickyFailureOnlyUpdateRetries(t *testing.T) {
	ctx := graphTestContext(t)
	g := NewGraphLock()
	provider := graphMount(t, ctx, g, Spec{ID: "provider", Provides: []string{"service"}, Apply: func(c *Context) error {
		return c.Provide("service", 1)
	}})
	var attempts atomic.Int64
	consumer := graphMount(t, ctx, g, Spec{ID: "failed", Needs: []string{"service"}, Apply: func(*Context) error {
		if attempts.Add(1) == 1 {
			return errors.New("intentional current failure")
		}
		return nil
	}})
	graphSettle(t, ctx, g)
	if got := graphSnapshot(t, ctx, g, consumer); got.State != Failed || got.Error == "" {
		t.Fatalf("failure: %+v", got)
	}
	if err := g.Update(ctx, provider); err != nil {
		t.Fatal(err)
	}
	graphSettle(t, ctx, g)
	if got := graphSnapshot(t, ctx, g, consumer); got.State != Failed || attempts.Load() != 1 {
		t.Fatalf("dependency retried sticky failure: %+v attempts %d", got, attempts.Load())
	}
	if err := g.Update(ctx, consumer); err != nil {
		t.Fatal(err)
	}
	graphSettle(t, ctx, g)
	if got := graphSnapshot(t, ctx, g, consumer); got.State != Active || got.Error != "" || attempts.Load() != 2 {
		t.Fatalf("explicit retry: %+v attempts %d", got, attempts.Load())
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockStaleLoadFailureDoesNotPoisonNewRevision(t *testing.T) {
	ctx := graphTestContext(t)
	g := NewGraphLock()
	started := make(chan *Context, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	var attempts atomic.Int64
	id := graphMount(t, ctx, g, Spec{ID: "stale", Apply: func(c *Context) error {
		if attempts.Add(1) == 1 {
			started <- c
			<-release
			return errors.New("superseded failure")
		}
		return nil
	}})
	old := graphReceive(t, ctx, started)
	if err := g.Update(ctx, id); err != nil {
		t.Fatal(err)
	}
	graphReceive(t, ctx, old.Std().Done())
	if old.Enter() {
		old.Leave()
		t.Fatal("quiescing context accepted a new lease")
	}
	releaseOnce.Do(func() { close(release) })
	graphSettle(t, ctx, g)
	if got := graphSnapshot(t, ctx, g, id); got.State != Active || got.Revision != 1 || got.Error != "" || attempts.Load() != 2 {
		t.Fatalf("new target poisoned by stale failure: %+v", got)
	}
	if err := old.Dispose(); !errors.Is(err, ErrInactive) {
		t.Fatalf("stale context disposed replacement: %v", err)
	}
	if err := old.Defer("late", func(context.Context) error { return nil }); !errors.Is(err, ErrInactive) {
		t.Fatalf("stale context registered effect: %v", err)
	}
	if _, err := old.Read("anything"); !errors.Is(err, ErrInactive) {
		t.Fatalf("released context Read: %v", err)
	}
	graphEventSequence(t, ctx, g, id, "load:stale")
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockStaleDependencyEpochFailureDoesNotPoisonTarget(t *testing.T) {
	ctx := graphTestContext(t)
	g := NewGraphLock()
	var generation atomic.Int64
	provider := graphMount(t, ctx, g, Spec{ID: "provider", Provides: []string{"service"}, Apply: func(c *Context) error {
		return c.Provide("service", generation.Add(1))
	}})
	graphSettle(t, ctx, g)
	started := make(chan *Context, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	var attempts atomic.Int64
	consumer := graphMount(t, ctx, g, Spec{ID: "consumer", Needs: []string{"service"}, Apply: func(c *Context) error {
		started <- c
		if attempts.Add(1) == 1 {
			<-release
			return errors.New("failure of superseded dependency epoch")
		}
		return nil
	}})
	old := graphReceive(t, ctx, started)
	first := graphSnapshot(t, ctx, g, consumer)
	if err := g.Update(ctx, provider); err != nil {
		t.Fatal(err)
	}
	graphReceive(t, ctx, old.Std().Done())
	releaseOnce.Do(func() { close(release) })
	graphSettle(t, ctx, g)
	fresh := graphReceive(t, ctx, started)
	got := graphSnapshot(t, ctx, g, consumer)
	if got.State != Active || got.Revision != 0 || got.Error != "" || attempts.Load() != 2 || reflect.DeepEqual(got.Epoch, first.Epoch) {
		t.Fatalf("same revision's new epoch poisoned: old %+v new %+v", first, got)
	}
	if value, err := fresh.Read("service"); err != nil || value != int64(2) {
		t.Fatalf("replacement dependency binding: %v %v", value, err)
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockSelfDisposeRequestIsNotCompletion(t *testing.T) {
	ctx := graphTestContext(t)
	g := NewGraphLock()
	requestReturned := make(chan struct{})
	releaseApply := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseApply) }) })
	var cleanupCalls atomic.Int64
	id := graphMount(t, ctx, g, Spec{ID: "self", Apply: func(c *Context) error {
		if err := c.Defer("effect", func(context.Context) error { cleanupCalls.Add(1); return nil }); err != nil {
			return err
		}
		if !c.Enter() {
			return errors.New("initial lease refused")
		}
		err := c.Dispose()
		c.Leave() // Never await a reload while retaining this lease.
		close(requestReturned)
		<-releaseApply
		return err
	}})
	graphReceive(t, ctx, requestReturned)
	if got := graphSnapshot(t, ctx, g, id); got.State != Unloading || len(got.Effects) != 1 || cleanupCalls.Load() != 0 {
		t.Fatalf("self request treated as completed: %+v", got)
	}
	releaseOnce.Do(func() { close(releaseApply) })
	graphSettle(t, ctx, g)
	if got := graphSnapshot(t, ctx, g, id); got.State != Disposed || cleanupCalls.Load() != 1 {
		t.Fatalf("self disposal completion: %+v", got)
	}
	request := graphEventSequence(t, ctx, g, id, "dispose:request")
	complete := graphEventSequence(t, ctx, g, id, "dispose:complete")
	if request >= complete {
		t.Fatalf("request/completion ordering: %d >= %d", request, complete)
	}
}

func TestGraphLockLeaseDrainAndImmutableBindings(t *testing.T) {
	ctx := graphTestContext(t)
	g := NewGraphLock()
	provider := graphMount(t, ctx, g, Spec{ID: "provider", Provides: []string{"service"}, Apply: func(c *Context) error {
		return c.Provide("service", "stable")
	}})
	contexts := make(chan *Context, 2)
	cleaned := make(chan struct{}, 2)
	consumer := graphMount(t, ctx, g, Spec{ID: "consumer", Needs: []string{"service"}, Apply: func(c *Context) error {
		contexts <- c
		return c.Defer("lease-owned", func(context.Context) error { cleaned <- struct{}{}; return nil })
	}})
	graphSettle(t, ctx, g)
	old := graphReceive(t, ctx, contexts)
	if !old.Enter() {
		t.Fatal("active lease refused")
	}
	var leaveOnce sync.Once
	t.Cleanup(func() { leaveOnce.Do(old.Leave) })
	if err := g.Update(ctx, provider); err != nil {
		t.Fatal(err)
	}
	graphReceive(t, ctx, old.Std().Done())
	if old.Enter() {
		old.Leave()
		t.Fatal("quiescing lease accepted")
	}
	if value, err := old.Read("service"); err != nil || value != "stable" {
		t.Fatalf("leased immutable binding disappeared: %v %v", value, err)
	}
	select {
	case <-cleaned:
		t.Fatal("cleanup started while leased")
	default:
	}
	if got := graphSnapshot(t, ctx, g, consumer); got.State != Unloading || len(got.Effects) != 1 {
		t.Fatalf("leased unload: %+v", got)
	}
	leaveOnce.Do(old.Leave)
	graphSettle(t, ctx, g)
	graphReceive(t, ctx, cleaned)
	fresh := graphReceive(t, ctx, contexts)
	if fresh == old || !fresh.Enter() {
		t.Fatal("replacement context not independently active")
	}
	fresh.Leave()
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockLoadDeadlineKeepsStuckApplyAsSoleWriter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := graphTestContext(t)
		g := NewGraphLock().(*GraphLock)
		g.knobs.load = time.Second
		started := make(chan *Context, 1)
		release := make(chan struct{})
		var releaseOnce sync.Once
		t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
		var attempts, cleanups atomic.Int64
		id := graphMount(t, ctx, g, Spec{ID: "stuck-load", Apply: func(c *Context) error {
			attempts.Add(1)
			if err := c.Defer("partial", func(context.Context) error { cleanups.Add(1); return nil }); err != nil {
				return err
			}
			started <- c
			<-release // Deliberately ignores cancellation until explicitly released.
			return errors.New("late load result")
		}})
		old := graphReceive(t, ctx, started)
		graphReceive(t, ctx, old.Std().Done())
		synctest.Wait()
		if got := graphSnapshot(t, ctx, g, id); got.State != Unloading || got.Error == "" || len(got.Effects) != 1 || attempts.Load() != 1 || cleanups.Load() != 0 {
			t.Fatalf("load timeout falsely released partial effects: %+v", got)
		}
		if err := old.Defer("too-late", func(context.Context) error { return nil }); !errors.Is(err, ErrInactive) {
			t.Fatalf("timed-out Apply could register late effects: %v", err)
		}
		if old.Enter() {
			old.Leave()
			t.Fatal("timed-out Apply accepted a new lease")
		}
		releaseOnce.Do(func() { close(release) })
		graphSettle(t, ctx, g)
		if got := graphSnapshot(t, ctx, g, id); got.State != Failed || got.Error == "" || len(got.Effects) != 0 || cleanups.Load() != 1 {
			t.Fatalf("timeout not sticky after actual unwind: %+v", got)
		}
		if err := g.Close(ctx); err != nil {
			t.Fatal(err)
		}
	})
}

func TestGraphLockLoadDeadlineCooperativelyUnwinds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := graphTestContext(t)
		g := NewGraphLock().(*GraphLock)
		g.knobs.load = time.Second
		var cleanups atomic.Int64
		id := graphMount(t, ctx, g, Spec{ID: "cooperative", Apply: func(c *Context) error {
			if err := c.Defer("partial", func(context.Context) error { cleanups.Add(1); return nil }); err != nil {
				return err
			}
			<-c.Std().Done()
			return c.Std().Err()
		}})
		graphSettle(t, ctx, g)
		if got := graphSnapshot(t, ctx, g, id); got.State != Failed || got.Error == "" || len(got.Effects) != 0 || cleanups.Load() != 1 {
			t.Fatalf("cooperative timeout: %+v", got)
		}
		graphEventSequence(t, ctx, g, id, "deadline:load")
		if err := g.Close(ctx); err != nil {
			t.Fatal(err)
		}
	})
}

func TestGraphLockLeaseDeadlineDoesNotReleaseBorrowedEffects(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := graphTestContext(t)
		g := NewGraphLock().(*GraphLock)
		g.knobs.leaseDrain = time.Second
		contexts := make(chan *Context, 1)
		var cleanups atomic.Int64
		id := graphMount(t, ctx, g, Spec{ID: "borrowed", Apply: func(c *Context) error {
			contexts <- c
			return c.Defer("borrowed-effect", func(context.Context) error { cleanups.Add(1); return nil })
		}})
		graphSettle(t, ctx, g)
		old := graphReceive(t, ctx, contexts)
		if !old.Enter() {
			t.Fatal("initial lease refused")
		}
		var leaveOnce sync.Once
		t.Cleanup(func() { leaveOnce.Do(old.Leave) })
		if err := g.Dispose(ctx, id); err != nil {
			t.Fatal(err)
		}
		// A timer receive advances virtual time, with no real scheduling sleep.
		graphReceive(t, ctx, time.After(g.knobs.leaseDrain))
		synctest.Wait()
		if got := graphSnapshot(t, ctx, g, id); got.State != Unloading || got.Error == "" || len(got.Effects) != 1 || cleanups.Load() != 0 {
			t.Fatalf("lease deadline released borrowed effect: %+v", got)
		}
		leaveOnce.Do(old.Leave)
		graphSettle(t, ctx, g)
		if got := graphSnapshot(t, ctx, g, id); got.State != Disposed || len(got.Effects) != 0 || cleanups.Load() != 1 {
			t.Fatalf("returned lease did not finish disposal: %+v", got)
		}
	})
}

func TestGraphLockCallPathDoesNotAcquireGraphMutex(t *testing.T) {
	ctx := graphTestContext(t)
	g := NewGraphLock().(*GraphLock)
	graphMount(t, ctx, g, Spec{ID: "provider", Provides: []string{"service"}, Apply: func(c *Context) error { return c.Provide("service", 42) }})
	contexts := make(chan *Context, 1)
	graphMount(t, ctx, g, Spec{ID: "consumer", Needs: []string{"service"}, Apply: func(c *Context) error { contexts <- c; return nil }})
	graphSettle(t, ctx, g)
	c := graphReceive(t, ctx, contexts)
	g.mu.Lock()
	done := make(chan error, 1)
	go func() {
		if !c.Enter() {
			done <- errors.New("lease refused")
			return
		}
		value, err := c.Read("service")
		if err == nil && value != 42 {
			err = errors.New("wrong immutable binding")
		}
		c.Leave()
		done <- err
	}()
	select {
	case err := <-done:
		g.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		g.mu.Unlock()
		t.Fatal("Read/Enter/Leave waited for graph mutex")
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockConcurrentHooksSynchronizeWithRetirement(t *testing.T) {
	ctx := graphTestContext(t)
	g := NewGraphLock()
	contexts := make(chan *Context, 2)
	id := graphMount(t, ctx, g, Spec{ID: "hooks", Provides: []string{"service"}, Apply: func(c *Context) error { contexts <- c; return nil }})
	graphSettle(t, ctx, g)
	old := graphReceive(t, ctx, contexts)
	start := make(chan struct{})
	var wg sync.WaitGroup
	var accepted, cleaned atomic.Int64
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := old.Defer("concurrent", func(context.Context) error { cleaned.Add(1); return nil }); err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrInactive) {
				t.Errorf("concurrent effect: %v", err)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		err := old.Provide("service", "value")
		if err != nil && !errors.Is(err, ErrInactive) {
			t.Errorf("concurrent Provide: %v", err)
		}
	}()
	close(start)
	if err := g.Update(ctx, id); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	graphSettle(t, ctx, g)
	if accepted.Load() != cleaned.Load() {
		t.Fatalf("accepted %d effects, cleaned %d", accepted.Load(), cleaned.Load())
	}
	if old.Enter() {
		old.Leave()
		t.Fatal("retired context accepted lease")
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockProvideReservesImmediatelyButBindsOnlyActive(t *testing.T) {
	ctx := graphTestContext(t)
	g := NewGraphLock()
	reserved := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	consumer := graphMount(t, ctx, g, Spec{ID: "consumer", Needs: []string{"service"}})
	graphMount(t, ctx, g, Spec{ID: "first", Provides: []string{"service"}, Apply: func(c *Context) error {
		if err := c.Provide("service", "reserved"); err != nil {
			return err
		}
		close(reserved)
		<-release
		return nil
	}})
	graphReceive(t, ctx, reserved)
	if got := graphSnapshot(t, ctx, g, consumer); got.State != Pending {
		t.Fatalf("loading reservation became visible early: %+v", got)
	}
	duplicateResult := make(chan error, 1)
	duplicate := graphMount(t, ctx, g, Spec{ID: "duplicate", Provides: []string{"service"}, Apply: func(c *Context) error {
		err := c.Provide("service", "collision")
		duplicateResult <- err
		return err
	}})
	if err := graphReceive(t, ctx, duplicateResult); err == nil {
		t.Fatal("duplicate Provide accepted a reserved key")
	}
	releaseOnce.Do(func() { close(release) })
	graphSettle(t, ctx, g)
	if got := graphSnapshot(t, ctx, g, consumer); got.State != Active {
		t.Fatalf("completed provider not visible: %+v", got)
	}
	if got := graphSnapshot(t, ctx, g, duplicate); got.State != Failed {
		t.Fatalf("duplicate error not sticky: %+v", got)
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockTeardownDropsActiveRegistryReferences(t *testing.T) {
	ctx := graphTestContext(t)
	g := NewGraphLock().(*GraphLock)
	graphMount(t, ctx, g, Spec{ID: "provider", Provides: []string{"service"}, Apply: func(c *Context) error { return c.Provide("service", "value") }})
	graphMount(t, ctx, g, Spec{ID: "consumer", Needs: []string{"service"}})
	graphSettle(t, ctx, g)
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
	g.mu.Lock()
	fibers, services, indexes := len(g.fibers), len(g.services), len(g.indexByKey)
	g.mu.Unlock()
	if fibers != 0 || services != 0 || indexes != 0 {
		t.Fatalf("active registry not empty: fibers %d services %d indexByKey %d", fibers, services, indexes)
	}
	// Historical snapshots are value-only; they do not retain callbacks,
	// service objects, captured injection maps, or activation goroutines.
	snapshots, err := g.Snapshots(ctx)
	if err != nil || len(snapshots) != 2 {
		t.Fatalf("inspection history: %v %v", snapshots, err)
	}
	for _, snapshot := range snapshots {
		if snapshot.State != Disposed || len(snapshot.Effects) != 0 {
			t.Fatalf("retired snapshot: %+v", snapshot)
		}
	}
}

func TestGraphLockAvailabilityIsConsumerBoundAndNotifyDoesNotReloadUnchanged(t *testing.T) {
	ctx := graphTestContext(t)
	g := NewGraphLock()
	graphMount(t, ctx, g, Spec{ID: "provider", Provides: []string{"service"}, Apply: func(c *Context) error { return c.Provide("service", "value") }})
	var allowA, allowB atomic.Bool
	allowA.Store(true)
	var checksA, checksB, loadsA, loadsB, unloadsA atomic.Int64
	readonly := func(c *Context) {
		if value, err := c.Read("service"); err != nil || value != "value" {
			t.Errorf("check bindings: %v %v", value, err)
		}
		if c.Enter() {
			c.Leave()
			t.Error("predicate context accepted lease")
		}
		if err := c.Defer("forbidden", func(context.Context) error { return nil }); !errors.Is(err, ErrInactive) {
			t.Errorf("predicate accepted effect: %v", err)
		}
		if err := c.Provide("forbidden", true); !errors.Is(err, ErrInactive) {
			t.Errorf("predicate accepted provision: %v", err)
		}
		if err := c.Dispose(); !errors.Is(err, ErrInactive) {
			t.Errorf("predicate self-dispose: %v", err)
		}
	}
	a := graphMount(t, ctx, g, Spec{ID: "A", Needs: []string{"service"}, Check: func(c *Context) bool {
		readonly(c)
		checksA.Add(1)
		return allowA.Load()
	}, Apply: func(c *Context) error {
		loadsA.Add(1)
		return c.Defer("A-effect", func(context.Context) error { unloadsA.Add(1); return nil })
	}})
	b := graphMount(t, ctx, g, Spec{ID: "B", Needs: []string{"service"}, Check: func(c *Context) bool {
		readonly(c)
		checksB.Add(1)
		return allowB.Load()
	}, Apply: func(*Context) error { loadsB.Add(1); return nil }})
	graphSettle(t, ctx, g)
	if sa, sb := graphSnapshot(t, ctx, g, a), graphSnapshot(t, ctx, g, b); sa.State != Active || sb.State != Pending || loadsA.Load() != 1 || loadsB.Load() != 0 || checksA.Load() != 1 || checksB.Load() != 1 {
		t.Fatalf("consumer-bound availability: A %+v B %+v", sa, sb)
	}
	before := graphSnapshot(t, ctx, g, a)
	if err := g.(Notifier).Notify(ctx, "service"); err != nil {
		t.Fatal(err)
	}
	graphSettle(t, ctx, g)
	if got := graphSnapshot(t, ctx, g, a); got.State != Active || !reflect.DeepEqual(got.Epoch, before.Epoch) || got.Revision != before.Revision || loadsA.Load() != 1 || unloadsA.Load() != 0 || checksA.Load() != 2 || checksB.Load() != 2 {
		t.Fatalf("unchanged notification reloaded: %+v loads %d unloads %d", got, loadsA.Load(), unloadsA.Load())
	}
	allowA.Store(false)
	allowB.Store(true)
	if err := g.(Notifier).Notify(ctx, "service"); err != nil {
		t.Fatal(err)
	}
	graphSettle(t, ctx, g)
	if sa, sb := graphSnapshot(t, ctx, g, a), graphSnapshot(t, ctx, g, b); sa.State != Pending || sb.State != Active || loadsA.Load() != 1 || unloadsA.Load() != 1 || loadsB.Load() != 1 {
		t.Fatalf("revocation/arrival: A %+v B %+v", sa, sb)
	}
	if checksA.Load() != 3 || checksB.Load() != 3 {
		t.Fatalf("denied check hot looped: A %d B %d", checksA.Load(), checksB.Load())
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockAvailabilityPanicIsPendingAndRunsOutsideMutex(t *testing.T) {
	ctx := graphTestContext(t)
	g := NewGraphLock()
	var checks, loads atomic.Int64
	var allow atomic.Bool
	id := graphMount(t, ctx, g, Spec{ID: "check", Check: func(*Context) bool {
		checks.Add(1)
		// This would deadlock if the predicate held the graph mutex.
		if _, err := g.Snapshots(ctx); err != nil {
			t.Errorf("reentrant snapshot: %v", err)
		}
		if !allow.Load() {
			panic("fixture availability panic")
		}
		return true
	}, Apply: func(*Context) error { loads.Add(1); return nil }})
	graphSettle(t, ctx, g)
	if got := graphSnapshot(t, ctx, g, id); got.State != Pending || got.Error == "" || loads.Load() != 0 || checks.Load() != 1 {
		t.Fatalf("check panic poisoned revision/hot loop: %+v", got)
	}
	allow.Store(true)
	if err := g.(Notifier).Notify(ctx); err != nil {
		t.Fatal(err)
	}
	graphSettle(t, ctx, g)
	if got := graphSnapshot(t, ctx, g, id); got.State != Active || got.Error != "" || loads.Load() != 1 || checks.Load() != 2 {
		t.Fatalf("check panic did not retry on Notify: %+v", got)
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockStaleAvailabilityResultCannotDenyNewNotice(t *testing.T) {
	ctx := graphTestContext(t)
	g := NewGraphLock()
	checkStarted := make(chan struct{})
	releaseCheck := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseCheck) }) })
	var checks, loads atomic.Int64
	id := graphMount(t, ctx, g, Spec{ID: "stale-check", Check: func(*Context) bool {
		if checks.Add(1) == 1 {
			close(checkStarted)
			<-releaseCheck
			return false
		}
		return true
	}, Apply: func(*Context) error { loads.Add(1); return nil }})
	graphReceive(t, ctx, checkStarted)
	if err := g.(Notifier).Notify(ctx); err != nil {
		t.Fatal(err)
	}
	releaseOnce.Do(func() { close(releaseCheck) })
	graphSettle(t, ctx, g)
	if got := graphSnapshot(t, ctx, g, id); got.State != Active || got.Error != "" || got.Revision != 0 || checks.Load() != 2 || loads.Load() != 1 {
		t.Fatalf("stale false result denied new notice: %+v", got)
	}
	graphEventSequence(t, ctx, g, id, "availability:stale")
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockCleanupDeadlineDoesNotClearEffectsOrRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := graphTestContext(t)
		g := NewGraphLock().(*GraphLock)
		g.knobs.cleanup = time.Second
		release := make(chan struct{})
		var releaseOnce sync.Once
		t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
		started := make(chan context.Context, 1)
		var attempts atomic.Int64
		id := graphMount(t, ctx, g, Spec{ID: "blocked", Apply: func(c *Context) error {
			attempts.Add(1)
			return c.Defer("stubborn", func(disposeCtx context.Context) error {
				started <- disposeCtx
				<-release // Deliberately ignores the design deadline.
				return nil
			})
		}})
		graphSettle(t, ctx, g)
		disposed := make(chan error, 1)
		go func() { disposed <- g.Dispose(ctx, id) }()
		disposeCtx := graphReceive(t, ctx, started)
		graphReceive(t, ctx, disposeCtx.Done())
		synctest.Wait()
		if got := graphSnapshot(t, ctx, g, id); got.State != Unloading || len(got.Effects) != 1 || got.Error == "" || attempts.Load() != 1 {
			t.Fatalf("deadline faked completion/retry: %+v", got)
		}
		releaseOnce.Do(func() { close(release) })
		if err := graphReceive(t, ctx, disposed); err != nil {
			t.Fatal(err)
		}
		graphSettle(t, ctx, g)
		if got := graphSnapshot(t, ctx, g, id); got.State != Disposed || len(got.Effects) != 0 {
			t.Fatalf("actual cleanup not completed: %+v", got)
		}
	})
}

func TestGraphLockAwaitFiberDoesNotJoinUnrelatedGates(t *testing.T) {
	for _, phase := range []string{"loading", "unloading"} {
		t.Run(phase, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx := graphTestContext(t)
				g := NewGraphLock().(*GraphLock)
				g.knobs.load, g.knobs.cleanup = time.Hour, time.Hour
				started := make(chan struct{})
				release := make(chan struct{})
				var releaseOnce sync.Once
				t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
				other := graphMount(t, ctx, g, Spec{ID: "unrelated", Apply: func(c *Context) error {
					if phase == "loading" {
						close(started)
						<-release
						return nil
					}
					return c.Defer("unrelated-effect", func(context.Context) error {
						close(started)
						<-release
						return nil
					})
				}})
				if phase == "unloading" {
					if err := g.AwaitFiber(ctx, other); err != nil {
						t.Fatal(err)
					}
					if err := g.Dispose(ctx, other); err != nil {
						t.Fatal(err)
					}
				}
				graphReceive(t, ctx, started)
				target := graphMount(t, ctx, g, Spec{ID: "target"})
				if err := g.AwaitFiber(ctx, target); err != nil {
					t.Fatalf("target joined unrelated %s gate: %v", phase, err)
				}
				pending := graphMount(t, ctx, g, Spec{ID: "pending", Needs: []string{"missing"}})
				if err := g.AwaitFiber(ctx, pending); err != nil {
					t.Fatalf("stable missing dependency did not join: %v", err)
				}
				if got := graphSnapshot(t, ctx, g, pending); got.State != Pending {
					t.Fatalf("await invented an activation: %+v", got)
				}
				if err := g.Dispose(ctx, target); err != nil {
					t.Fatal(err)
				}
				if err := g.AwaitFiber(ctx, target); err != nil {
					t.Fatalf("disposal joined unrelated gate: %v", err)
				}
				if err := g.AwaitFiber(ctx, target); err != nil {
					t.Fatalf("archived disposal not joinable: %v", err)
				}
				if err := g.AwaitFiber(ctx, 0); !errors.Is(err, ErrUnknown) {
					t.Fatalf("unknown target: %v", err)
				}
				releaseOnce.Do(func() { close(release) })
				if err := g.Close(ctx); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func TestGraphLockAwaitFiberJoinsActualLoadAndCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := graphTestContext(t)
		g := NewGraphLock().(*GraphLock)
		g.knobs.load, g.knobs.cleanup = time.Hour, time.Hour
		loadStarted, cleanupStarted := make(chan struct{}), make(chan struct{})
		releaseLoad, releaseCleanup := make(chan struct{}), make(chan struct{})
		var loadOnce, cleanupOnce sync.Once
		t.Cleanup(func() {
			loadOnce.Do(func() { close(releaseLoad) })
			cleanupOnce.Do(func() { close(releaseCleanup) })
		})
		id := graphMount(t, ctx, g, Spec{ID: "joined", Apply: func(c *Context) error {
			close(loadStarted)
			<-releaseLoad
			return c.Defer("joined-effect", func(context.Context) error {
				close(cleanupStarted)
				<-releaseCleanup
				return nil
			})
		}})
		graphReceive(t, ctx, loadStarted)
		joined := make(chan error, 1)
		go func() { joined <- g.AwaitFiber(ctx, id) }()
		synctest.Wait()
		select {
		case err := <-joined:
			t.Fatalf("await completed before Apply returned: %v", err)
		default:
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if err := g.AwaitFiber(canceled, id); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled await: %v", err)
		}
		loadOnce.Do(func() { close(releaseLoad) })
		if err := graphReceive(t, ctx, joined); err != nil {
			t.Fatal(err)
		}
		if err := g.Dispose(ctx, id); err != nil {
			t.Fatal(err)
		}
		graphReceive(t, ctx, cleanupStarted)
		go func() { joined <- g.AwaitFiber(ctx, id) }()
		synctest.Wait()
		select {
		case err := <-joined:
			t.Fatalf("await completed before disposer returned: %v", err)
		default:
		}
		cleanupOnce.Do(func() { close(releaseCleanup) })
		if err := graphReceive(t, ctx, joined); err != nil {
			t.Fatal(err)
		}
		if err := g.AwaitFiber(ctx, id); err != nil {
			t.Fatalf("disposed archive await: %v", err)
		}
		if got := graphSnapshot(t, ctx, g, id); got.State != Disposed {
			t.Fatalf("joined cleanup not disposed: %+v", got)
		}
	})
}

func TestGraphLockAwaitFiberRejectsStickyFailure(t *testing.T) {
	ctx := graphTestContext(t)
	g := NewGraphLock().(*GraphLock)
	failure := errors.New("actual Apply failure")
	var retry atomic.Bool
	id := graphMount(t, ctx, g, Spec{ID: "failed-join", Apply: func(*Context) error {
		if !retry.Load() {
			return failure
		}
		return nil
	}})
	for i := 0; i < 2; i++ {
		if err := g.AwaitFiber(ctx, id); !errors.Is(err, failure) {
			t.Fatalf("FAILED await %d did not reject with current sticky error: %v", i, err)
		}
	}
	retry.Store(true)
	if err := g.Update(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := g.AwaitFiber(ctx, id); err != nil {
		t.Fatalf("successful newer revision retained failure: %v", err)
	}
	failed := graphMount(t, ctx, g, Spec{ID: "failed-dispose", Apply: func(*Context) error { return failure }})
	if err := g.AwaitFiber(ctx, failed); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if err := g.Dispose(ctx, failed); err != nil {
		t.Fatal(err)
	}
	if err := g.AwaitFiber(ctx, failed); !errors.Is(err, failure) {
		t.Fatalf("disposed FAILED target lost the actual startup error: %v", err)
	}
	if got := graphSnapshot(t, ctx, g, failed); got.State != Disposed || got.Error != failure.Error() {
		t.Fatalf("disposed diagnostics lost: %+v", got)
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockAwaitFiberKeepsStartupErrorSeparateFromCleanup(t *testing.T) {
	ctx := graphTestContext(t)
	g := NewGraphLock().(*GraphLock)
	startup, cleanup := errors.New("startup error"), errors.New("cleanup diagnostic")
	failed := graphMount(t, ctx, g, Spec{ID: "failed-with-cleanup", Apply: func(c *Context) error {
		if err := c.Defer("cleanup", func(context.Context) error { return cleanup }); err != nil {
			return err
		}
		return startup
	}})
	if err := g.AwaitFiber(ctx, failed); err != startup {
		t.Fatalf("cleanup replaced or joined the actual startup error: %v", err)
	}
	if err := g.Dispose(ctx, failed); err != nil {
		t.Fatal(err)
	}
	if err := g.AwaitFiber(ctx, failed); err != startup {
		t.Fatalf("disposed archive lost startup error identity: %v", err)
	}
	succeeded := graphMount(t, ctx, g, Spec{ID: "successful-with-cleanup", Apply: func(c *Context) error {
		return c.Defer("cleanup", func(context.Context) error { return cleanup })
	}})
	if err := g.AwaitFiber(ctx, succeeded); err != nil {
		t.Fatal(err)
	}
	if err := g.Dispose(ctx, succeeded); err != nil {
		t.Fatal(err)
	}
	if err := g.AwaitFiber(ctx, succeeded); err != nil {
		t.Fatalf("cleanup diagnostic became an await startup rejection: %v", err)
	}
	if got := graphSnapshot(t, ctx, g, succeeded); got.State != Disposed || got.Error != cleanup.Error() {
		t.Fatalf("cleanup diagnostic not preserved for inspection: %+v", got)
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockRecordOwnsFixtureJSON(t *testing.T) {
	ctx := graphTestContext(t)
	g := NewGraphLock().(*GraphLock)
	nested := map[string]any{"label": "original"}
	items := []any{nested, []any{"kept"}}
	bytes := []byte("bytes")
	payload := map[string]any{"items": items, "bytes": bytes, "count": 7}
	if err := g.Record(ctx, "fixture/mark", payload); err != nil {
		t.Fatal(err)
	}
	// Mutation after Record returns cannot change the committed envelope.
	nested["label"] = "changed"
	items[1].([]any)[0] = "changed"
	bytes[0] = 'X'
	payload["count"] = 99
	want := map[string]any{
		"items": []any{map[string]any{"label": "original"}, []any{"kept"}},
		"bytes": "Ynl0ZXM=", "count": json.Number("7"),
	}
	events, err := g.Events(ctx)
	if err != nil || len(events) != 1 {
		t.Fatalf("fixture ledger: %v %v", events, err)
	}
	if event := events[0]; event.Sequence != 1 || event.Kind != "fixture" || event.Key != "fixture/mark" || event.Fiber != 0 || event.Name != "" || !reflect.DeepEqual(event.Data, want) {
		t.Fatalf("actual fixture envelope: %+v", event)
	}
	// A reader cannot mutate the historical ledger through Events either.
	events[0].Data["items"].([]any)[0].(map[string]any)["label"] = "reader mutation"
	events[0].Data["items"].([]any)[1].([]any)[0] = "reader mutation"
	events[0].Data["count"] = float64(123)
	again, err := g.Events(ctx)
	if err != nil || !reflect.DeepEqual(again[0].Data, want) {
		t.Fatalf("Events leaked fixture payload: %+v %v", again, err)
	}
	if err := g.Record(ctx, "fixture/non-json", map[string]any{"channel": make(chan struct{})}); err == nil {
		t.Fatal("non-JSON instrumentation accepted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := g.Record(canceled, "fixture/canceled", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled record: %v", err)
	}
	afterRejected, err := g.Events(ctx)
	if err != nil || len(afterRejected) != 1 {
		t.Fatalf("rejected record committed an event: %+v %v", afterRejected, err)
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := g.Record(ctx, "fixture/closed", nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed record: %v", err)
	}
}

// A caller-owned JSON marshaler reenters the runtime during Record's copy.
// Neither serialization nor Events may call it while holding the graph lock.
type graphRecordJSONProbe struct {
	g   *GraphLock
	ctx context.Context
}

func (p graphRecordJSONProbe) MarshalJSON() ([]byte, error) {
	if _, err := p.g.Snapshots(p.ctx); err != nil {
		return nil, err
	}
	if err := p.g.Record(p.ctx, "fixture/serialized", map[string]any{"copied": true}); err != nil {
		return nil, err
	}
	return []byte(`{"plain":true}`), nil
}

func TestGraphLockRecordFromCallbacksReentrantAndOrdered(t *testing.T) {
	ctx := graphTestContext(t)
	g := NewGraphLock().(*GraphLock)
	id := graphMount(t, ctx, g, Spec{ID: "callback", Check: func(*Context) bool {
		if err := g.Record(ctx, "fixture/check", map[string]any{"label": "callback"}); err != nil {
			t.Errorf("record from availability check: %v", err)
			return false
		}
		return true
	}, Apply: func(c *Context) error {
		if err := g.Record(ctx, "fixture/apply-before", map[string]any{"probe": graphRecordJSONProbe{g: g, ctx: ctx}}); err != nil {
			return err
		}
		if err := c.Defer("recorded-effect", func(context.Context) error {
			return g.Record(ctx, "fixture/cleanup", map[string]any{"label": "callback"})
		}); err != nil {
			return err
		}
		return g.Record(ctx, "fixture/apply-after", map[string]any{"label": "callback"})
	}})
	if err := g.AwaitFiber(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := g.Record(ctx, "fixture/change-before", map[string]any{"label": "callback"}); err != nil {
		t.Fatal(err)
	}
	if err := g.Update(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := g.Record(ctx, "fixture/change-after", map[string]any{"label": "callback"}); err != nil {
		t.Fatal(err)
	}
	if err := g.AwaitFiber(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := g.Dispose(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := g.AwaitFiber(ctx, id); err != nil {
		t.Fatal(err)
	}
	events, err := g.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var loading, applyBefore, effect, applyAfter, active, changeBefore, update, changeAfter, cleanup, disposed uint64
	for i, event := range events {
		if event.Sequence != uint64(i+1) {
			t.Fatalf("raw ledger order[%d]: %+v", i, event)
		}
		if event.Fiber == id && event.Name != "callback" {
			t.Fatalf("unstable core label: %+v", event)
		}
		switch {
		case event.Kind == "state" && event.State == Loading && loading == 0:
			loading = event.Sequence
		case event.Kind == "fixture" && event.Key == "fixture/apply-before" && applyBefore == 0:
			applyBefore = event.Sequence
		case event.Kind == "effect:add" && effect == 0:
			effect = event.Sequence
		case event.Kind == "fixture" && event.Key == "fixture/apply-after" && applyAfter == 0:
			applyAfter = event.Sequence
		case event.Kind == "state" && event.State == Active && active == 0:
			active = event.Sequence
		case event.Kind == "fixture" && event.Key == "fixture/change-before":
			changeBefore = event.Sequence
		case event.Kind == "update":
			update = event.Sequence
		case event.Kind == "fixture" && event.Key == "fixture/change-after":
			changeAfter = event.Sequence
		case event.Kind == "fixture" && event.Key == "fixture/cleanup":
			cleanup = event.Sequence
		case event.Kind == "state" && event.State == Disposed:
			disposed = event.Sequence
		}
	}
	if !(0 < loading && loading < applyBefore && applyBefore < effect && effect < applyAfter && applyAfter < active) {
		t.Fatalf("actual Apply marks not between lifecycle events: loading %d before %d effect %d after %d active %d", loading, applyBefore, effect, applyAfter, active)
	}
	if !(active < changeBefore && changeBefore < update && update < changeAfter && changeAfter < disposed && cleanup < disposed) {
		t.Fatalf("actual change/cleanup order: active %d before %d update %d after %d cleanup %d disposed %d", active, changeBefore, update, changeAfter, cleanup, disposed)
	}
}

func TestGraphLockEventsKeepRealStateAndServiceOrder(t *testing.T) {
	for _, phase := range []string{"initial", "late"} {
		t.Run(phase, func(t *testing.T) {
			ctx := graphTestContext(t)
			g := NewGraphLock().(*GraphLock)
			consumer := graphMount(t, ctx, g, Spec{ID: "consumer", Needs: []string{"service"}})
			value := &struct{ Label string }{Label: "actual"}
			contexts := make(chan *Context, 1)
			provider := graphMount(t, ctx, g, Spec{ID: "provider", Provides: []string{"service"}, Apply: func(c *Context) error {
				contexts <- c
				if phase == "initial" {
					return c.Provide("service", value)
				}
				return nil
			}})
			if err := g.AwaitFiber(ctx, provider); err != nil {
				t.Fatal(err)
			}
			if phase == "late" {
				if err := graphReceive(t, ctx, contexts).Provide("service", value); err != nil {
					t.Fatal(err)
				}
			}
			if err := g.AwaitFiber(ctx, consumer); err != nil {
				t.Fatal(err)
			}
			if err := g.Dispose(ctx, provider); err != nil {
				t.Fatal(err)
			}
			if err := g.AwaitFiber(ctx, provider); err != nil {
				t.Fatal(err)
			}
			events, err := g.Events(ctx)
			if err != nil {
				t.Fatal(err)
			}
			states := make(map[uint64]State)
			var consumerLoading, publish, remove, publishedUID uint64
			for i, event := range events {
				if event.Sequence != uint64(i+1) {
					t.Fatalf("raw event sequence[%d]: %+v", i, event)
				}
				if event.Name != map[uint64]string{consumer: "consumer", provider: "provider"}[event.Fiber] {
					t.Fatalf("event lost stable Spec.ID: %+v", event)
				}
				if event.Kind == "mount" {
					states[event.Fiber] = event.State
				}
				if event.Kind == "state" {
					if event.From != states[event.Fiber] {
						t.Fatalf("fabricated previous state: %+v (last %s)", event, states[event.Fiber])
					}
					states[event.Fiber] = event.State
					if event.Fiber == consumer && event.State == Loading {
						consumerLoading = event.Sequence
					}
				}
				if event.Kind == "service:publish" {
					publish, publishedUID = event.Sequence, event.UID
					if event.Value != value {
						t.Fatalf("published value not actual service identity: %+v", event)
					}
				}
				if event.Kind == "service:remove" {
					remove = event.Sequence
					if event.Value != value || event.UID != publishedUID {
						t.Fatalf("withdrawal lost actual service identity: %+v", event)
					}
				}
			}
			if !(0 < consumerLoading && consumerLoading < publish && publish < remove) {
				t.Fatalf("recursive no-Check status must precede service event: loading %d publish %d remove %d", consumerLoading, publish, remove)
			}
			if states[consumer] != Pending || states[provider] != Disposed {
				t.Fatalf("raw lifecycle events were lost: %v", states)
			}
			if err := g.Close(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}
