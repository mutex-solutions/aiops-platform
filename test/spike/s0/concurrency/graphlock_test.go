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

// Every candidate owns one global observer worker. Register final Close before
// per-test gate cleanups so those gates release first, including in synctest.
func graphTestRuntime(t *testing.T) Runtime {
	t.Helper()
	g := NewGraphLock()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Root deadline/failure tests intentionally retain diagnostics; worker
		// shutdown, not a diagnostic-free second Close, is this cleanup's duty.
		_ = g.Close(ctx)
	})
	return g
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

func graphInspect(t *testing.T, ctx context.Context, g Runtime) Inspection {
	t.Helper()
	inspector, ok := g.(Inspector)
	if !ok {
		t.Fatal("runtime does not expose native inspection")
	}
	inspection, err := inspector.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return inspection
}

func graphInspectedFiber(t *testing.T, inspection Inspection, id uint64) Snapshot {
	t.Helper()
	for _, fiber := range inspection.Fibers {
		if fiber.ID == id {
			return fiber
		}
	}
	t.Fatalf("inspection missing fiber %d", id)
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
	g := graphTestRuntime(t)
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
	g := graphTestRuntime(t)
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
	g := graphTestRuntime(t)
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
	if got := graphSnapshot(t, ctx, g, provider); got.State != Unloading || !reflect.DeepEqual(got.Effects, []string{"provide:service", "provider-effect"}) {
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
	g := graphTestRuntime(t)
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
	g := graphTestRuntime(t)
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
	g := graphTestRuntime(t)
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
	g := graphTestRuntime(t)
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
	if got := graphSnapshot(t, ctx, g, id); got.State != Loading || got.Alive || got.Target != nil || len(got.Effects) != 1 || cleanupCalls.Load() != 0 {
		t.Fatalf("self request treated as completed: %+v", got)
	}
	releaseOnce.Do(func() { close(releaseApply) })
	graphSettle(t, ctx, g)
	if got := graphSnapshot(t, ctx, g, id); got.State != Disposed || got.Alive || got.Target != nil || cleanupCalls.Load() != 1 {
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
	g := graphTestRuntime(t)
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
		g := graphTestRuntime(t).(*GraphLock)
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
		g := graphTestRuntime(t).(*GraphLock)
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
		g := graphTestRuntime(t).(*GraphLock)
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
	g := graphTestRuntime(t).(*GraphLock)
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
	g := graphTestRuntime(t)
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
	g := graphTestRuntime(t)
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
	g := graphTestRuntime(t).(*GraphLock)
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
	g := graphTestRuntime(t)
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
	g := graphTestRuntime(t)
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
	g := graphTestRuntime(t)
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
		g := graphTestRuntime(t).(*GraphLock)
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
				g := graphTestRuntime(t).(*GraphLock)
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
		g := graphTestRuntime(t).(*GraphLock)
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
	g := graphTestRuntime(t).(*GraphLock)
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
	g := graphTestRuntime(t).(*GraphLock)
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
	g := graphTestRuntime(t).(*GraphLock)
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
	g := graphTestRuntime(t).(*GraphLock)
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
			g := graphTestRuntime(t).(*GraphLock)
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

func TestGraphLockInspectTargetCoalescesWhileOldCleanupHeld(t *testing.T) {
	for _, checked := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-check", true: "accepted-check"}[checked], func(t *testing.T) {
			ctx := graphTestContext(t)
			g := graphTestRuntime(t).(*GraphLock)
			provider := graphMount(t, ctx, g, Spec{ID: "provider", Provides: []string{"service"}, Apply: func(c *Context) error {
				return c.Provide("service", "actual")
			}})
			if err := g.AwaitFiber(ctx, provider); err != nil {
				t.Fatal(err)
			}
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			t.Cleanup(func() { once.Do(func() { close(release) }) })
			var attempts, checks atomic.Int64
			spec := Spec{ID: "consumer", Needs: []string{"service"}, Apply: func(c *Context) error {
				if attempts.Add(1) != 1 {
					return nil
				}
				return c.Defer("held-old", func(context.Context) error {
					close(started)
					<-release
					return nil
				})
			}}
			if checked {
				spec.Check = func(*Context) bool { checks.Add(1); return true }
			}
			id := graphMount(t, ctx, g, spec)
			graphSettle(t, ctx, g)
			before := graphInspectedFiber(t, graphInspect(t, ctx, g), id)
			if !before.Alive || before.Target == nil || before.Target.Revision != 0 || !reflect.DeepEqual(before.Target.UIDs, before.Epoch) || len(before.Epoch) != 1 {
				t.Fatalf("initial target: %+v", before)
			}
			for revision := uint64(1); revision <= 3; revision++ {
				if err := g.Update(ctx, id); err != nil {
					t.Fatal(err)
				}
				if revision == 1 {
					graphReceive(t, ctx, started)
				}
				got := graphInspectedFiber(t, graphInspect(t, ctx, g), id)
				want := &EpochSnapshot{Revision: revision, UIDs: before.Epoch}
				if got.State != Unloading || !got.Alive || got.Revision != revision || !reflect.DeepEqual(got.Target, want) || !reflect.DeepEqual(got.Epoch, before.Epoch) || !reflect.DeepEqual(got.Effects, []string{"held-old"}) || attempts.Load() != 1 {
					t.Fatalf("revision %d target must not be old activation: %+v target %+v", revision, got, got.Target)
				}
				got.Target.UIDs[0] = 999
				got.Target.Revision = 999
				got.Epoch[0] = 999
				got.Effects[0] = "reader mutation"
			}
			if checked && checks.Load() != 1 {
				t.Fatalf("inspection executed predicate: %d checks", checks.Load())
			}
			once.Do(func() { close(release) })
			graphSettle(t, ctx, g)
			got := graphInspectedFiber(t, graphInspect(t, ctx, g), id)
			if got.State != Active || got.Target == nil || got.Target.Revision != 3 || attempts.Load() != 2 || !reflect.DeepEqual(got.Epoch, before.Epoch) {
				t.Fatalf("coalesced activation: %+v attempts %d", got, attempts.Load())
			}
			if err := g.Close(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGraphLockInspectProviderLossWithdrawsTargetBeforeRemoval(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	value := &struct{ Label string }{Label: "retained identity"}
	provider := graphMount(t, ctx, g, Spec{ID: "provider", Provides: []string{"service"}, Apply: func(c *Context) error {
		return c.Provide("service", value)
	}})
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	consumer := graphMount(t, ctx, g, Spec{ID: "consumer", Needs: []string{"service"}, Apply: func(c *Context) error {
		return c.Defer("held-dependent", func(context.Context) error {
			close(started)
			<-release
			return nil
		})
	}})
	graphSettle(t, ctx, g)
	initial := graphInspect(t, ctx, g)
	if len(initial.Provisions) != 1 || !initial.Provisions[0].Active {
		t.Fatalf("active provision: %+v", initial.Provisions)
	}
	binding := graphInspectedFiber(t, initial, consumer)
	if binding.Target == nil || !reflect.DeepEqual(binding.Target.UIDs, binding.Epoch) {
		t.Fatalf("active consumer target: %+v", binding)
	}
	if err := g.Dispose(ctx, provider); err != nil {
		t.Fatal(err)
	}
	graphReceive(t, ctx, started)
	held := graphInspect(t, ctx, g)
	p := graphInspectedFiber(t, held, provider)
	c := graphInspectedFiber(t, held, consumer)
	if p.State != Unloading || p.Alive || p.Target != nil || c.State != Unloading || !c.Alive || c.Target != nil || !reflect.DeepEqual(c.Epoch, binding.Epoch) {
		t.Fatalf("provider loss left live target: provider %+v consumer %+v", p, c)
	}
	want := initial.Provisions[0]
	want.Active = false
	if len(held.Provisions) != 1 || held.Provisions[0] != want || held.Provisions[0].Value != value {
		t.Fatalf("held provision disappeared or stayed active: %+v want %+v", held.Provisions, want)
	}
	// Repeated cuts must not resurrect the committed consumer binding as a
	// desired target while its provider is no longer ACTIVE.
	for i := 0; i < 4; i++ {
		if got := graphInspectedFiber(t, graphInspect(t, ctx, g), consumer); got.Target != nil {
			t.Fatalf("inspection fabricated a committed target: %+v", got)
		}
	}
	once.Do(func() { close(release) })
	graphSettle(t, ctx, g)
	after := graphInspect(t, ctx, g)
	p, c = graphInspectedFiber(t, after, provider), graphInspectedFiber(t, after, consumer)
	if len(after.Provisions) != 0 || p.State != Disposed || p.Alive || p.Target != nil || c.State != Pending || c.Target != nil || !c.Alive {
		t.Fatalf("removed provider inspection: %+v", after)
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockInspectStickyFailureHasNoTarget(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	provider := graphMount(t, ctx, g, Spec{ID: "provider", Provides: []string{"service"}, Apply: func(c *Context) error {
		return c.Provide("service", "actual")
	}})
	var attempts atomic.Int64
	id := graphMount(t, ctx, g, Spec{ID: "consumer", Needs: []string{"service"}, Apply: func(*Context) error {
		if attempts.Add(1) == 1 {
			return errors.New("sticky startup failure")
		}
		return nil
	}})
	graphSettle(t, ctx, g)
	for i := 0; i < 2; i++ {
		got := graphInspectedFiber(t, graphInspect(t, ctx, g), id)
		if got.State != Failed || !got.Alive || got.Target != nil || attempts.Load() != 1 {
			t.Fatalf("sticky target after provider changes: %+v", got)
		}
		if i == 0 {
			if err := g.Update(ctx, provider); err != nil {
				t.Fatal(err)
			}
			graphSettle(t, ctx, g)
		}
	}
	if err := g.Update(ctx, id); err != nil {
		t.Fatal(err)
	}
	graphSettle(t, ctx, g)
	got := graphInspectedFiber(t, graphInspect(t, ctx, g), id)
	if got.State != Active || got.Target == nil || got.Target.Revision != 1 || !reflect.DeepEqual(got.Target.UIDs, got.Epoch) || attempts.Load() != 2 {
		t.Fatalf("explicit update target: %+v", got)
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
	archived := graphInspect(t, ctx, g)
	retired := graphInspectedFiber(t, archived, id)
	if retired.State != Disposed || retired.Alive || retired.Target != nil || len(retired.Epoch) != 1 {
		t.Fatalf("disposed target: %+v", retired)
	}
	retired.Epoch[0] = 999
	again := graphInspectedFiber(t, graphInspect(t, ctx, g), id)
	if again.Epoch[0] != got.Epoch[0] || again.Alive || again.Target != nil {
		t.Fatalf("archived snapshot copy leaked: %+v", again)
	}
}

func TestGraphLockInspectReservationsUseUIDOrderAndOwnedCopies(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	reserved, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	consumerStarted, releaseConsumer := make(chan struct{}), make(chan struct{})
	var consumerOnce sync.Once
	t.Cleanup(func() { consumerOnce.Do(func() { close(releaseConsumer) }) })
	consumer := graphMount(t, ctx, g, Spec{ID: "consumer", Needs: []string{"z"}, Apply: func(*Context) error {
		close(consumerStarted)
		<-releaseConsumer
		return nil
	}})
	value := &graphRecordJSONProbe{g: g, ctx: ctx} // Marshaling would append an unwanted fixture event.
	provider := graphMount(t, ctx, g, Spec{ID: "first", Provides: []string{"a", "z"}, Apply: func(c *Context) error {
		if err := c.Provide("z", value); err != nil {
			return err
		}
		if err := c.Provide("a", value); err != nil {
			return err
		}
		if err := c.Defer("actual-effect", func(context.Context) error { return nil }); err != nil {
			return err
		}
		close(reserved)
		<-release
		return nil
	}})
	graphReceive(t, ctx, reserved)
	other := graphMount(t, ctx, g, Spec{ID: "second", Provides: []string{"middle"}, Apply: func(c *Context) error {
		return c.Provide("middle", value)
	}})
	if err := g.AwaitFiber(ctx, other); err != nil {
		t.Fatal(err)
	}
	inspection := graphInspect(t, ctx, g)
	want := []ProvisionSnapshot{
		{Key: "z", UID: 1, Owner: provider, Name: "first", Value: value},
		{Key: "a", UID: 2, Owner: provider, Name: "first", Value: value},
		{Key: "middle", UID: 3, Owner: other, Name: "second", Value: value, Active: true},
	}
	if !reflect.DeepEqual(inspection.Provisions, want) {
		t.Fatalf("reservation creation order: %+v want %+v", inspection.Provisions, want)
	}
	pending := graphInspectedFiber(t, inspection, consumer)
	loading := graphInspectedFiber(t, inspection, provider)
	if pending.State != Pending || pending.Target != nil || !pending.Alive || loading.State != Loading || loading.Target == nil || loading.Target.Revision != 0 || len(loading.Target.UIDs) != 0 {
		t.Fatalf("reserved LOADING binding/target: pending %+v loading %+v", pending, loading)
	}
	loading.Target.Revision = 999
	loading.Target.UIDs = append(loading.Target.UIDs, 999)
	loading.Effects[0] = "reader mutation"
	inspection.Fibers[0].Name = "reader mutation"
	inspection.Provisions[0] = ProvisionSnapshot{Key: "reader mutation", UID: 999}
	again := graphInspect(t, ctx, g)
	if !reflect.DeepEqual(again.Provisions, want) || graphInspectedFiber(t, again, consumer).Name != "consumer" {
		t.Fatalf("inspection leaked mutable storage: %+v", again)
	}
	loading = graphInspectedFiber(t, again, provider)
	if loading.Target.Revision != 0 || len(loading.Target.UIDs) != 0 || !reflect.DeepEqual(loading.Effects, []string{"provide:z", "provide:a", "actual-effect"}) {
		t.Fatalf("loading snapshot leaked mutable storage: %+v", loading)
	}
	once.Do(func() { close(release) })
	graphReceive(t, ctx, consumerStarted)
	loadingConsumer := graphInspectedFiber(t, graphInspect(t, ctx, g), consumer)
	if loadingConsumer.State != Loading || loadingConsumer.Target == nil || loadingConsumer.Target.Revision != 0 || !reflect.DeepEqual(loadingConsumer.Target.UIDs, []uint64{want[0].UID}) {
		t.Fatalf("LOADING target lost actual dependency reservation: %+v", loadingConsumer)
	}
	consumerOnce.Do(func() { close(releaseConsumer) })
	graphSettle(t, ctx, g)
	for i := range want {
		want[i].Active = true
	}
	active := graphInspect(t, ctx, g)
	if !reflect.DeepEqual(active.Provisions, want) {
		t.Fatalf("publication reordered reservations: %+v", active.Provisions)
	}
	var published []string
	events, err := g.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == "fixture" {
			t.Fatal("inspection invoked service value marshaler")
		}
		if event.Kind == "service:publish" {
			published = append(published, event.Key)
		}
	}
	if !reflect.DeepEqual(published, []string{"middle", "a", "z"}) {
		t.Fatalf("fixture did not distinguish publication vs creation order: %v", published)
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockInspectCheckResultGatesTargetWithoutCallingCheck(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	checkStarted, applyStarted := make(chan struct{}, 2), make(chan struct{})
	results := make(chan bool, 2)
	releaseApply := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(releaseApply) }) })
	var checks, applies atomic.Int64
	id := graphMount(t, ctx, g, Spec{ID: "checked", Check: func(c *Context) bool {
		checks.Add(1)
		if c.Enter() {
			c.Leave()
			t.Error("inspection fixture predicate received a live context")
		}
		checkStarted <- struct{}{}
		select {
		case result := <-results:
			return result
		case <-c.Std().Done():
			return false
		}
	}, Apply: func(*Context) error {
		applies.Add(1)
		close(applyStarted)
		<-releaseApply
		return nil
	}})
	graphReceive(t, ctx, checkStarted)
	for i := 0; i < 2; i++ {
		if got := graphInspectedFiber(t, graphInspect(t, ctx, g), id); got.Target != nil || checks.Load() != 1 || applies.Load() != 0 {
			t.Fatalf("unaccepted readonly checker became a target: %+v", got)
		}
	}
	results <- false
	graphSettle(t, ctx, g)
	if got := graphInspectedFiber(t, graphInspect(t, ctx, g), id); got.State != Pending || got.Target != nil {
		t.Fatalf("false predicate target: %+v", got)
	}
	if err := g.Notify(ctx); err != nil {
		t.Fatal(err)
	}
	graphReceive(t, ctx, checkStarted)
	if got := graphInspectedFiber(t, graphInspect(t, ctx, g), id); got.Target != nil || checks.Load() != 2 {
		t.Fatalf("pending recheck became target: %+v", got)
	}
	results <- true
	graphReceive(t, ctx, applyStarted)
	got := graphInspectedFiber(t, graphInspect(t, ctx, g), id)
	if got.State != Loading || got.Target == nil || got.Target.Revision != 0 || len(got.Target.UIDs) != 0 || checks.Load() != 2 || applies.Load() != 1 {
		t.Fatalf("accepted LOADING target: %+v", got)
	}
	once.Do(func() { close(releaseApply) })
	graphSettle(t, ctx, g)
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

type graphInspectErrorProbe struct {
	g     *GraphLock
	ctx   context.Context
	calls atomic.Int64
}

func (p *graphInspectErrorProbe) Error() string {
	p.calls.Add(1)
	// Events takes mu but does not format startup errors. Reentrancy proves both
	// live and archived diagnostic formatting happens outside the graph owner.
	_, _ = p.g.Events(p.ctx)
	return "actual diagnostic"
}

func TestGraphLockInspectFormatsDiagnosticsOutsideMutex(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	probe := &graphInspectErrorProbe{g: g, ctx: ctx}
	id := graphMount(t, ctx, g, Spec{ID: "failure", Apply: func(*Context) error { return probe }})
	if err := g.AwaitFiber(ctx, id); err != probe || probe.calls.Load() != 0 {
		t.Fatalf("startup unexpectedly formatted error: %T calls %d", err, probe.calls.Load())
	}
	for _, disposed := range []bool{false, true} {
		snapshots := make(chan []Snapshot, 1)
		go func() {
			got, _ := g.Snapshots(ctx)
			snapshots <- got
		}()
		got := graphInspectedFiber(t, Inspection{Fibers: graphReceive(t, ctx, snapshots)}, id)
		if got.Error != "actual diagnostic" || got.Alive == disposed || got.Target != nil {
			t.Fatalf("reentrant diagnostic snapshot: %+v", got)
		}
		inspections := make(chan Inspection, 1)
		go func() { got, _ := g.Inspect(ctx); inspections <- got }()
		got = graphInspectedFiber(t, graphReceive(t, ctx, inspections), id)
		if got.Error != "actual diagnostic" {
			t.Fatalf("reentrant inspection diagnostic: %+v", got)
		}
		if !disposed {
			if err := g.Dispose(ctx, id); err != nil {
				t.Fatal(err)
			}
			if err := g.AwaitFiber(ctx, id); err != probe || probe.calls.Load() != 2 {
				t.Fatalf("archive formatted diagnostic under owner: %T calls %d", err, probe.calls.Load())
			}
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if inspection, err := g.Inspect(canceled); !errors.Is(err, context.Canceled) || len(inspection.Fibers) != 0 || len(inspection.Provisions) != 0 || probe.calls.Load() != 4 {
		t.Fatalf("canceled inspection: %+v %v calls %d", inspection, err, probe.calls.Load())
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockConfigurationPreparedBeforeApplyAndRawRetention(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	raw := &struct{ Name string }{"raw"}
	retained := &struct{ Name string }{"retained"}
	effective := &struct{ Name string }{"effective"}
	var resolves atomic.Int64
	cell := NewConfiguration(raw, ConfigHooks{Resolve: func(_ context.Context, got any) (any, error) {
		resolves.Add(1)
		if got != raw {
			return nil, errors.New("unexpected raw input")
		}
		if _, err := g.Inspect(ctx); err != nil {
			return nil, err
		}
		return effective, nil
	}})
	contexts := make(chan *Context, 1)
	failure := errors.New("Apply failed after config publication")
	id := graphMount(t, ctx, g, Spec{ID: "configured", Config: cell, Apply: func(c *Context) error {
		contexts <- c
		snapshot := cell.Snapshot()
		if c.Config() != effective || c.configVersion != 0 || !snapshot.Prepared || snapshot.Effective != effective {
			return errors.New("Apply did not observe already-published effective identity")
		}
		if _, err := g.Inspect(ctx); err != nil {
			return err
		}
		return failure
	}})
	if err := g.AwaitFiber(ctx, id); err != failure {
		t.Fatalf("configured Apply failure: %v", err)
	}
	activation := graphReceive(t, ctx, contexts)
	cell.RetainRaw(retained)
	got := graphInspectedFiber(t, graphInspect(t, ctx, g), id)
	if got.State != Failed || !got.Configured || !got.ConfigPrepared || got.RawConfig != retained || got.EffectiveConfig != effective || got.ConfigVersion != 1 || resolves.Load() != 1 || activation.Config() != effective {
		t.Fatalf("raw replacement changed effective activation: %+v resolves %d", got, resolves.Load())
	}
	if err := g.Dispose(ctx, id); err != nil {
		t.Fatal(err)
	}
	graphSettle(t, ctx, g)
	cell.RetainRaw("later cell mutation")
	archived := graphInspectedFiber(t, graphInspect(t, ctx, g), id)
	if archived.State != Disposed || !archived.ConfigPrepared || archived.RawConfig != retained || archived.EffectiveConfig != effective || archived.ConfigVersion != 1 {
		t.Fatalf("archive retained mutable cell instead of value view: %+v", archived)
	}
}

func TestGraphLockConfigurationLoadedVetoAndRejectionBoundaries(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	invalid, rejected := errors.New("invalid candidate"), errors.New("update hook rejection")
	var resolves, commits, updates, applies atomic.Int64
	cell := NewConfiguration("old", ConfigHooks{
		Resolve: func(_ context.Context, raw any) (any, error) {
			resolves.Add(1)
			if _, err := g.Inspect(ctx); err != nil {
				return nil, err
			}
			if raw == "invalid" {
				return nil, invalid
			}
			return "resolved:" + raw.(string), nil
		},
		Commit: func(context.Context, any) error {
			commits.Add(1)
			_, err := g.Inspect(ctx)
			return err
		},
		Update: func(_ context.Context, value any, _ func() error) error {
			updates.Add(1)
			if _, err := g.Inspect(ctx); err != nil {
				return err
			}
			if value == "resolved:reject" {
				return rejected
			}
			return nil
		},
	})
	contexts := make(chan *Context, 1)
	id := graphMount(t, ctx, g, Spec{ID: "veto", Config: cell, Apply: func(c *Context) error {
		applies.Add(1)
		contexts <- c
		return nil
	}})
	graphSettle(t, ctx, g)
	activation := graphReceive(t, ctx, contexts)
	if err := g.UpdateConfig(ctx, id, "new", false); err != nil {
		t.Fatal(err)
	}
	got := graphInspectedFiber(t, graphInspect(t, ctx, g), id)
	if got.State != Active || got.Revision != 0 || got.RawConfig != "new" || got.EffectiveConfig != "resolved:new" || got.ConfigVersion != 1 || !got.ConfigPrepared || applies.Load() != 1 || commits.Load() != 1 || updates.Load() != 1 || activation.Config() != "resolved:old" {
		t.Fatalf("loaded veto restarted or lost config boundaries: %+v", got)
	}
	if err := g.UpdateConfig(ctx, id, "invalid", false); err != invalid {
		t.Fatalf("invalid loaded candidate: %v", err)
	}
	invalidView := graphInspectedFiber(t, graphInspect(t, ctx, g), id)
	if !reflect.DeepEqual(invalidView, got) || commits.Load() != 1 || updates.Load() != 1 {
		t.Fatalf("invalid candidate committed or changed state: %+v", invalidView)
	}
	if err := g.UpdateConfig(ctx, id, "reject", false); err != rejected {
		t.Fatalf("rejected update: %v", err)
	}
	got = graphInspectedFiber(t, graphInspect(t, ctx, g), id)
	if got.RawConfig != "reject" || got.EffectiveConfig != "resolved:new" || got.ConfigVersion != 2 || got.Revision != 0 || applies.Load() != 1 {
		t.Fatalf("rejected hook published effective or undid raw: %+v", got)
	}
	if err := g.UpdateConfig(ctx, id, "no-save", true); err != nil {
		t.Fatal(err)
	}
	got = graphInspectedFiber(t, graphInspect(t, ctx, g), id)
	if got.RawConfig != "no-save" || got.EffectiveConfig != "resolved:no-save" || got.ConfigVersion != 3 || commits.Load() != 2 || updates.Load() != 3 || applies.Load() != 1 || resolves.Load() != 5 {
		t.Fatalf("noSave skipped more than persistence: %+v resolve %d commit %d update %d", got, resolves.Load(), commits.Load(), updates.Load())
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockConfigurationFailurePreparationAndRetry(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	invalid := errors.New("invalid startup config")
	var resolves, applies, commits, updates atomic.Int64
	cell := NewConfiguration("invalid", ConfigHooks{
		Resolve: func(_ context.Context, raw any) (any, error) {
			resolves.Add(1)
			if raw == "invalid" {
				return nil, invalid
			}
			return "resolved:" + raw.(string), nil
		},
		Commit: func(context.Context, any) error { commits.Add(1); return nil },
		Update: func(context.Context, any, func() error) error { updates.Add(1); return nil },
	})
	id := graphMount(t, ctx, g, Spec{ID: "bad-config", Config: cell, Apply: func(c *Context) error {
		applies.Add(1)
		if c.Config() != "resolved:valid" {
			return errors.New("wrong retry effective config")
		}
		return nil
	}})
	if err := g.AwaitFiber(ctx, id); err != invalid {
		t.Fatalf("startup config rejection not joinable: %v", err)
	}
	got := graphInspectedFiber(t, graphInspect(t, ctx, g), id)
	if got.State != Failed || got.Target != nil || got.ConfigPrepared || !got.Configured || got.RawConfig != "invalid" || got.EffectiveConfig != nil || applies.Load() != 0 || resolves.Load() != 1 {
		t.Fatalf("startup validation failure: %+v", got)
	}
	// FAILED has an inactive desired epoch: retain+commit, then restart and
	// prepare in the worker. The loaded Update hook must not run.
	if err := g.UpdateConfig(ctx, id, "valid", false); err != nil {
		t.Fatal(err)
	}
	got = graphInspectedFiber(t, graphInspect(t, ctx, g), id)
	if got.State != Active || got.Revision != 1 || got.Error != "" || got.Target == nil || !got.ConfigPrepared || got.RawConfig != "valid" || got.EffectiveConfig != "resolved:valid" || got.ConfigVersion != 1 || applies.Load() != 1 || resolves.Load() != 2 || commits.Load() != 1 || updates.Load() != 0 {
		t.Fatalf("native failed config retry: %+v", got)
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockConfigurationCoalescesLatestInputDuringHeldCleanup(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	updated := make(chan string, 2)
	applied := make(chan any, 3)
	var attempts atomic.Int64
	cell := NewConfiguration("old", ConfigHooks{
		Resolve: func(_ context.Context, raw any) (any, error) { return "resolved:" + raw.(string), nil },
		Update: func(_ context.Context, value any, next func() error) error {
			updated <- value.(string)
			return next()
		},
	})
	id := graphMount(t, ctx, g, Spec{ID: "coalesced-config", Config: cell, Apply: func(c *Context) error {
		applied <- c.Config()
		if attempts.Add(1) != 1 {
			return nil
		}
		return c.Defer("old-config-effect", func(context.Context) error {
			close(started)
			<-release
			return nil
		})
	}})
	graphSettle(t, ctx, g)
	if value := graphReceive(t, ctx, applied); value != "resolved:old" {
		t.Fatalf("initial input: %v", value)
	}
	results := make(chan error, 2)
	go func() { results <- g.UpdateConfig(ctx, id, "middle", false) }()
	if value := graphReceive(t, ctx, updated); value != "resolved:middle" {
		t.Fatal(value)
	}
	graphReceive(t, ctx, started)
	go func() { results <- g.UpdateConfig(ctx, id, "latest", false) }()
	if value := graphReceive(t, ctx, updated); value != "resolved:latest" {
		t.Fatal(value)
	}
	// Update's event is the real acceptance barrier, not an invented adapter
	// revision. Joining the graph cut with this event waits for restart ack.
	for {
		g.mu.Lock()
		revision := g.fibers[id].revision
		changed := g.changed
		g.mu.Unlock()
		if revision == 2 {
			break
		}
		graphReceive(t, ctx, changed)
	}
	held := graphInspectedFiber(t, graphInspect(t, ctx, g), id)
	if held.State != Unloading || held.Target == nil || held.Target.Revision != 2 || held.RawConfig != "latest" || held.EffectiveConfig != "resolved:old" || held.ConfigVersion != 2 || attempts.Load() != 1 {
		t.Fatalf("held cleanup lost latest raw or invented activation: %+v", held)
	}
	once.Do(func() { close(release) })
	for i := 0; i < 2; i++ {
		if err := graphReceive(t, ctx, results); err != nil {
			t.Fatal(err)
		}
	}
	if value := graphReceive(t, ctx, applied); value != "resolved:latest" || attempts.Load() != 2 {
		t.Fatalf("coalescing activated stale input: %v attempts %d", value, attempts.Load())
	}
	got := graphInspectedFiber(t, graphInspect(t, ctx, g), id)
	if got.State != Active || got.EffectiveConfig != "resolved:latest" || got.Revision != 2 {
		t.Fatalf("latest config not active: %+v", got)
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockConfigurationSupersededPrepareNeverCallsApply(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	var resolves atomic.Int64
	cell := NewConfiguration("old", ConfigHooks{Resolve: func(_ context.Context, raw any) (any, error) {
		if resolves.Add(1) == 1 {
			close(started)
			<-release
		}
		return "resolved:" + raw.(string), nil
	}})
	applied := make(chan any, 2)
	id := graphMount(t, ctx, g, Spec{ID: "superseded-config", Config: cell, Apply: func(c *Context) error { applied <- c.Config(); return nil }})
	graphReceive(t, ctx, started)
	cell.RetainRaw("latest")
	once.Do(func() { close(release) })
	graphSettle(t, ctx, g)
	if value := graphReceive(t, ctx, applied); value != "resolved:latest" || resolves.Load() != 2 {
		t.Fatalf("stale preparation reached Apply: %v resolves %d", value, resolves.Load())
	}
	select {
	case value := <-applied:
		t.Fatalf("old preparation also applied: %v", value)
	default:
	}
	got := graphInspectedFiber(t, graphInspect(t, ctx, g), id)
	if got.State != Active || got.Revision != 0 || got.ConfigVersion != 1 || got.EffectiveConfig != "resolved:latest" || got.Error != "" {
		t.Fatalf("superseded preparation poisoned latest target: %+v", got)
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockConfigurationVetoCannotPublishAfterBindingChange(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	cell := NewConfiguration("old", ConfigHooks{
		Resolve: func(_ context.Context, raw any) (any, error) { return "resolved:" + raw.(string), nil },
		Update:  func(context.Context, any, func() error) error { close(entered); <-release; return nil },
	})
	provider := graphMount(t, ctx, g, Spec{ID: "provider", Provides: []string{"service"}, Apply: func(c *Context) error { return c.Provide("service", true) }})
	id := graphMount(t, ctx, g, Spec{ID: "config-consumer", Needs: []string{"service"}, Config: cell})
	graphSettle(t, ctx, g)
	result := make(chan error, 1)
	go func() { result <- g.UpdateConfig(ctx, id, "new", false) }()
	graphReceive(t, ctx, entered)
	if err := g.Dispose(ctx, provider); err != nil {
		t.Fatal(err)
	}
	graphSettle(t, ctx, g)
	once.Do(func() { close(release) })
	if err := graphReceive(t, ctx, result); err != nil {
		t.Fatal(err)
	}
	got := graphInspectedFiber(t, graphInspect(t, ctx, g), id)
	if got.State != Pending || got.Target != nil || got.RawConfig != "new" || got.EffectiveConfig != "resolved:old" || got.Revision != 0 {
		t.Fatalf("stale retained-instance update published after provider loss: %+v", got)
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockApplyEffectReturnedCleanupIsPostBodyAndSelfDisposeSafe(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	value := &struct{ Name string }{"retained"}
	var order []string
	id := graphMount(t, ctx, g, Spec{ID: "returned-cleanup", Provides: []string{"service"}, ApplyEffect: func(c *Context) (Disposer, error) {
		if err := c.Provide("service", value); err != nil {
			return nil, err
		}
		if err := c.Defer("body-effect", func(context.Context) error { order = append(order, "body"); return nil }); err != nil {
			return nil, err
		}
		if err := c.Dispose(); err != nil {
			return nil, err
		}
		return func(context.Context) error {
			inspection := graphInspect(t, ctx, g)
			if len(inspection.Provisions) != 1 || inspection.Provisions[0].Value != value || inspection.Provisions[0].Active {
				return errors.New("returned cleanup lost actual retiring provision")
			}
			order = append(order, "returned")
			return nil
		}, nil
	}})
	graphSettle(t, ctx, g)
	if !reflect.DeepEqual(order, []string{"returned", "body"}) {
		t.Fatalf("returned cleanup not appended after body: %v", order)
	}
	inspection := graphInspect(t, ctx, g)
	got := graphInspectedFiber(t, inspection, id)
	if got.State != Disposed || got.Error != "" || got.Alive || len(inspection.Provisions) != 0 {
		t.Fatalf("self disposal lost returned cleanup: %+v", inspection)
	}
	if _, err := g.Mount(ctx, Spec{ID: "ambiguous", Apply: func(*Context) error { return nil }, ApplyEffect: func(*Context) (Disposer, error) { return nil, nil }}); err == nil {
		t.Fatal("both Apply forms accepted")
	}
}

func TestGraphLockWithdrawEventRetainsValueBeforeRemoval(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	value := &struct{ Name string }{"real value"}
	provider := graphMount(t, ctx, g, Spec{ID: "provider", Provides: []string{"service"}, Apply: func(c *Context) error { return c.Provide("service", value) }})
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	consumer := graphMount(t, ctx, g, Spec{ID: "consumer", Needs: []string{"service"}, Apply: func(c *Context) error {
		return c.Defer("held", func(context.Context) error { close(started); <-release; return nil })
	}})
	graphSettle(t, ctx, g)
	if err := g.Dispose(ctx, provider); err != nil {
		t.Fatal(err)
	}
	graphReceive(t, ctx, started)
	events, err := g.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var consumerUnload, withdraw uint64
	for _, event := range events {
		if event.Fiber == consumer && event.Kind == "state" && event.State == Unloading {
			consumerUnload = event.Sequence
		}
		if event.Kind == "service:withdraw" {
			withdraw = event.Sequence
			if event.Fiber != provider || event.State != Unloading || event.Value != value || event.UID == 0 {
				t.Fatalf("withdrawal fabricated identity: %+v", event)
			}
		}
		if event.Kind == "service:remove" {
			t.Fatal("provision removed before cleanup barrier")
		}
	}
	if consumerUnload == 0 || withdraw <= consumerUnload {
		t.Fatalf("withdraw notification did not follow recursive dependent transition: %d %d", consumerUnload, withdraw)
	}
	once.Do(func() { close(release) })
	graphSettle(t, ctx, g)
	if remove := graphEventSequence(t, ctx, g, provider, "service:remove"); remove <= withdraw {
		t.Fatalf("remove did not follow withdrawal: %d <= %d", remove, withdraw)
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockPendingDisposeUnloadsWithoutApply(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	var calls atomic.Int64
	id := graphMount(t, ctx, g, Spec{ID: "pending", Needs: []string{"missing"}, Apply: func(*Context) error { calls.Add(1); return nil }})
	graphSettle(t, ctx, g)
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
	var states []State
	for _, event := range events {
		if event.Kind == "state" {
			states = append(states, event.State)
		}
		if event.Kind == "effect:add" || event.Kind == "effects:gone" {
			t.Fatalf("pending disposal invented activation effects: %+v", event)
		}
	}
	if !reflect.DeepEqual(states, []State{Unloading, Disposed}) || calls.Load() != 0 {
		t.Fatalf("pending disposal skipped unload or invoked Apply: %v calls %d", states, calls.Load())
	}
}

func TestGraphLockConfigurationPrepareUsesLoadDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := graphTestContext(t)
		g := graphTestRuntime(t).(*GraphLock)
		g.knobs.load = time.Second
		started, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		t.Cleanup(func() { once.Do(func() { close(release) }) })
		var applies atomic.Int64
		cell := NewConfiguration("raw", ConfigHooks{Resolve: func(context.Context, any) (any, error) {
			close(started)
			<-release // Ignore cancellation until the test releases the real worker.
			return "resolved", nil
		}})
		id := graphMount(t, ctx, g, Spec{ID: "stuck-prepare", Config: cell, Apply: func(*Context) error { applies.Add(1); return nil }})
		graphReceive(t, ctx, started)
		graphReceive(t, ctx, time.After(time.Second))
		synctest.Wait()
		held := graphInspectedFiber(t, graphInspect(t, ctx, g), id)
		if held.State != Unloading || held.Target != nil || held.Error == "" || held.ConfigPrepared || applies.Load() != 0 {
			t.Fatalf("configuration resolution escaped load deadline: %+v", held)
		}
		once.Do(func() { close(release) })
		graphSettle(t, ctx, g)
		got := graphInspectedFiber(t, graphInspect(t, ctx, g), id)
		if got.State != Failed || got.ConfigPrepared || got.EffectiveConfig != nil || applies.Load() != 0 {
			t.Fatalf("late resolution published or invoked Apply: %+v", got)
		}
		if err := g.Close(ctx); err != nil {
			t.Fatal(err)
		}
	})
}

func TestGraphLockApplyEffectSupersededBodyStillOwnsReturnedCleanup(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	var attempts atomic.Int64
	var cleaned []int64
	id := graphMount(t, ctx, g, Spec{ID: "superseded-return", ApplyEffect: func(*Context) (Disposer, error) {
		attempt := attempts.Add(1)
		if attempt == 1 {
			close(started)
			<-release
		}
		return func(context.Context) error { cleaned = append(cleaned, attempt); return nil }, nil
	}})
	graphReceive(t, ctx, started)
	if err := g.Update(ctx, id); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	graphSettle(t, ctx, g)
	if attempts.Load() != 2 || !reflect.DeepEqual(cleaned, []int64{1}) {
		t.Fatalf("superseded return lost or reran cleanup: attempts %d cleanup %v", attempts.Load(), cleaned)
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cleaned, []int64{1, 2}) {
		t.Fatalf("replacement cleanup was not independently owned: %v", cleaned)
	}
}

func TestGraphLockChildCanRunDuringParentLoadUnlessItsProviderIsPending(t *testing.T) {
	for _, ownProvider := range []bool{false, true} {
		t.Run(map[bool]string{false: "inherited-requirement", true: "parent-provision"}[ownProvider], func(t *testing.T) {
			ctx := graphTestContext(t)
			g := graphTestRuntime(t).(*GraphLock)
			root := graphMount(t, ctx, g, Spec{ID: "root-provider", Provides: []string{"root"}, Apply: func(c *Context) error { return c.Provide("root", "inherited") }})
			if err := g.AwaitFiber(ctx, root); err != nil {
				t.Fatal(err)
			}
			release := make(chan struct{})
			var once sync.Once
			t.Cleanup(func() { once.Do(func() { close(release) }) })
			children, childRan := make(chan uint64, 1), make(chan struct{})
			parent := graphMount(t, ctx, g, Spec{ID: "parent", Needs: []string{"root"}, Provides: []string{"parent"}, Apply: func(c *Context) error {
				if err := c.Provide("parent", "owned"); err != nil {
					return err
				}
				// The context frontend has already encoded inherited requirements;
				// native structural ownership adds no invented dependency gate.
				needs := []string{"root"}
				if ownProvider {
					needs = append(needs, "parent")
				}
				child, err := c.MountChild(Spec{ID: "child", Needs: needs, Config: NewConfiguration("child-config", ConfigHooks{}), Apply: func(child *Context) error {
					if value, err := child.Read("root"); err != nil || value != "inherited" || child.Config() != "child-config" {
						return errors.New("child lost encoded requirement or own configuration")
					}
					if ownProvider {
						if value, err := child.Read("parent"); err != nil || value != "owned" {
							return errors.New("child did not bind actual ACTIVE parent provision")
						}
					}
					close(childRan)
					return nil
				}})
				if err != nil {
					return err
				}
				children <- child
				<-release
				return nil
			}})
			child := graphReceive(t, ctx, children)
			if ownProvider {
				got := graphSnapshot(t, ctx, g, child)
				if got.State != Pending || got.Target != nil {
					t.Fatalf("child bound LOADING parent reservation: %+v", got)
				}
			} else {
				graphReceive(t, ctx, childRan)
				if err := g.AwaitFiber(ctx, child); err != nil {
					t.Fatal(err)
				}
			}
			if got := graphSnapshot(t, ctx, g, parent); got.State != Loading || !reflect.DeepEqual(got.Effects, []string{"provide:parent", "child"}) {
				t.Fatalf("child ownership was not visible during parent Apply: %+v", got)
			}
			once.Do(func() { close(release) })
			graphSettle(t, ctx, g)
			if ownProvider {
				graphReceive(t, ctx, childRan)
			}
			if err := g.Dispose(ctx, parent); err != nil {
				t.Fatal(err)
			}
			graphSettle(t, ctx, g)
			if got := graphSnapshot(t, ctx, g, child); got.State != Disposed || got.Alive {
				t.Fatalf("parent disposal left child alive: %+v", got)
			}
			if err := g.Close(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// With a service dependency the child leaves first (the provider retains its
// effects until its dependants unload). Without one, the oracle order applies:
// the parent's earlier-registered effect runs while the child is unloading.
func TestGraphLockParentJoinsChildBeforeEffectsWithoutDependencyDeadlock(t *testing.T) {
	for _, depends := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-needs", true: "parent-service"}[depends], func(t *testing.T) {
			ctx := graphTestContext(t)
			g := graphTestRuntime(t).(*GraphLock)
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			t.Cleanup(func() { once.Do(func() { close(release) }) })
			children, contexts := make(chan uint64, 1), make(chan *Context, 1)
			var parentCleaned atomic.Bool
			parentCleanedCh := make(chan struct{})
			parent := graphMount(t, ctx, g, Spec{ID: "parent", Provides: []string{"service"}, Apply: func(c *Context) error {
				contexts <- c
				if err := c.Provide("service", "retained"); err != nil {
					return err
				}
				if err := c.Defer("parent-effect", func(context.Context) error { parentCleaned.Store(true); close(parentCleanedCh); return nil }); err != nil {
					return err
				}
				var needs []string
				if depends {
					needs = []string{"service"}
				}
				child, err := c.MountChild(Spec{ID: "child", Needs: needs, Apply: func(child *Context) error {
					return child.Defer("child-effect", func(context.Context) error {
						close(started)
						<-release
						if depends && parentCleaned.Load() {
							return errors.New("parent effect released before structural child cleanup")
						}
						if depends {
							if value, err := child.Read("service"); err != nil || value != "retained" {
								return errors.New("parent service lost during structural child cleanup")
							}
						}
						return nil
					})
				}})
				if err == nil {
					children <- child
				}
				return err
			}})
			child, old := graphReceive(t, ctx, children), graphReceive(t, ctx, contexts)
			graphSettle(t, ctx, g)
			g.mu.Lock()
			activation := g.fibers[parent].current
			g.mu.Unlock()
			if err := g.Dispose(ctx, parent); err != nil {
				t.Fatal(err)
			}
			graphReceive(t, ctx, started)
			if depends {
				if parentCleaned.Load() {
					t.Fatal("parent cleanup started before child completed")
				}
				held := graphInspect(t, ctx, g)
				// The child unloaded through the withdrawn service, not through a
				// disposal request, so it stays Alive until the parent's walk.
				if p, c := graphInspectedFiber(t, held, parent), graphInspectedFiber(t, held, child); p.State != Unloading || c.State != Unloading || p.Alive || !c.Alive || len(p.Effects) != 3 || len(held.Provisions) != 1 || held.Provisions[0].Active {
					t.Fatalf("structural teardown falsely completed: %+v", held)
				}
			} else {
				graphReceive(t, ctx, parentCleanedCh)
				held := graphInspect(t, ctx, g)
				if p, c := graphInspectedFiber(t, held, parent), graphInspectedFiber(t, held, child); p.State != Unloading || c.State != Unloading || p.Alive || c.Alive {
					t.Fatalf("parent left UNLOADING while its child still unloads: %+v", held)
				}
			}
			if _, err := old.MountChild(Spec{ID: "stale-child"}); !errors.Is(err, ErrInactive) {
				t.Fatalf("retiring parent accepted child: %v", err)
			}
			once.Do(func() { close(release) })
			graphSettle(t, ctx, g)
			if !parentCleaned.Load() || graphSnapshot(t, ctx, g, child).State != Disposed || graphSnapshot(t, ctx, g, parent).Error != "" {
				t.Fatal("structural child teardown did not finish cleanly")
			}
			g.mu.Lock()
			fibers, services, indexes, childrenLeft, effects := len(g.fibers), len(g.services), len(g.indexByKey), len(activation.children), len(activation.effects)
			g.mu.Unlock()
			if fibers != 0 || services != 0 || indexes != 0 || childrenLeft != 0 || effects != 0 {
				t.Fatalf("structural teardown retained active references: %d %d %d %d %d", fibers, services, indexes, childrenLeft, effects)
			}
			if childDone, parentGone := graphEventSequence(t, ctx, g, child, "dispose:complete"), graphEventSequence(t, ctx, g, parent, "effects:gone"); childDone >= parentGone {
				t.Fatalf("parent effects preceded child completion: %d >= %d", childDone, parentGone)
			}
		})
	}
}

func TestGraphLockChildProviderLossAndStaleParentCannotAffectOtherRoot(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	provider := graphMount(t, ctx, g, Spec{ID: "provider", Provides: []string{"service"}, Apply: func(c *Context) error { return c.Provide("service", "value") }})
	other := graphMount(t, ctx, g, Spec{ID: "unrelated"})
	children, contexts := make(chan uint64, 2), make(chan *Context, 2)
	parent := graphMount(t, ctx, g, Spec{ID: "parent", Needs: []string{"service"}, Apply: func(c *Context) error {
		contexts <- c
		child, err := c.MountChild(Spec{ID: "child"})
		if err == nil {
			children <- child
		}
		return err
	}})
	graphSettle(t, ctx, g)
	old, child := graphReceive(t, ctx, contexts), graphReceive(t, ctx, children)
	if err := g.Update(ctx, provider); err != nil {
		t.Fatal(err)
	}
	graphSettle(t, ctx, g)
	fresh, replacement := graphReceive(t, ctx, contexts), graphReceive(t, ctx, children)
	if child == replacement || fresh == old || graphSnapshot(t, ctx, g, child).State != Disposed || graphSnapshot(t, ctx, g, replacement).State != Active {
		t.Fatal("provider restart failed to replace activation-owned child")
	}
	if _, err := old.MountChild(Spec{ID: "stale"}); !errors.Is(err, ErrInactive) {
		t.Fatalf("old parent context attached child to replacement: %v", err)
	}
	if err := g.Dispose(ctx, provider); err != nil {
		t.Fatal(err)
	}
	graphSettle(t, ctx, g)
	if got := graphSnapshot(t, ctx, g, parent); got.State != Pending || got.Target != nil {
		t.Fatalf("provider loss did not invalidate parent: %+v", got)
	}
	if graphSnapshot(t, ctx, g, replacement).State != Disposed || graphSnapshot(t, ctx, g, other).State != Active {
		t.Fatal("provider loss leaked child or unloaded independent root")
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockLoadingParentSelfDisposeRetiresLoadingChild(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := graphTestContext(t)
		g := graphTestRuntime(t).(*GraphLock)
		g.knobs.load = time.Second
		children := make(chan uint64, 1)
		childStarted, requested, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var once sync.Once
		t.Cleanup(func() { once.Do(func() { close(release) }) })
		var parentCleaned atomic.Bool
		parent := graphMount(t, ctx, g, Spec{ID: "self-parent", Apply: func(c *Context) error {
			if err := c.Defer("parent-effect", func(context.Context) error { parentCleaned.Store(true); return nil }); err != nil {
				return err
			}
			child, err := c.MountChild(Spec{ID: "stuck-child", Apply: func(*Context) error { close(childStarted); <-release; return nil }})
			if err != nil {
				return err
			}
			children <- child
			<-childStarted
			err = c.Dispose()
			close(requested)
			return err
		}})
		child := graphReceive(t, ctx, children)
		graphReceive(t, ctx, requested)
		graphReceive(t, ctx, time.After(time.Second))
		synctest.Wait()
		inspection := graphInspect(t, ctx, g)
		p, c := graphInspectedFiber(t, inspection, parent), graphInspectedFiber(t, inspection, child)
		// Oracle order: parent-effect was registered before the child, so it runs
		// right after the child's disposal is requested and need not wait for it.
		if p.State != Unloading || c.State != Unloading || p.Alive || c.Alive || c.Error == "" || !parentCleaned.Load() {
			t.Fatalf("loading child falsely released parent: %+v", inspection)
		}
		once.Do(func() { close(release) })
		graphSettle(t, ctx, g)
		if !parentCleaned.Load() || graphSnapshot(t, ctx, g, parent).State != Disposed || graphSnapshot(t, ctx, g, child).State != Disposed {
			t.Fatal("child release failed to finish parent self-disposal")
		}
	})
}

func TestGraphLockChildMountValidationCopiesAndIndependentDetach(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	contexts := make(chan *Context, 1)
	parent := graphMount(t, ctx, g, Spec{ID: "parent", Apply: func(c *Context) error { contexts <- c; return nil }})
	graphSettle(t, ctx, g)
	c := graphReceive(t, ctx, contexts)
	if id, err := c.MountChild(Spec{ID: "invalid", Needs: []string{"duplicate", "duplicate"}}); err == nil || id != 0 {
		t.Fatalf("native child skipped root declaration validation: %d %v", id, err)
	}
	needs := []string{"missing"}
	first, err := c.MountChild(Spec{ID: "first", Needs: needs})
	if err != nil {
		t.Fatal(err)
	}
	needs[0] = "reader mutation"
	second, err := c.MountChild(Spec{ID: "second", Needs: []string{"missing"}})
	if err != nil || second != first+1 {
		t.Fatalf("sibling mount IDs not native reservation order: %d %d %v", first, second, err)
	}
	g.mu.Lock()
	activation := g.fibers[parent].current
	child := g.fibers[first]
	copied := child.spec.Needs[0]
	g.mu.Unlock()
	if copied != "missing" {
		t.Fatal("child declaration retained caller-owned slice")
	}
	if err := g.Dispose(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := g.AwaitFiber(ctx, first); err != nil {
		t.Fatal(err)
	}
	g.mu.Lock()
	detached, siblings := child.parent == nil, len(activation.children)
	g.mu.Unlock()
	if !detached || siblings != 1 || graphSnapshot(t, ctx, g, parent).State != Active || !reflect.DeepEqual(graphSnapshot(t, ctx, g, parent).Effects, []string{"child"}) {
		t.Fatal("independent child disposal retained membership or affected sibling owner")
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
	g.mu.Lock()
	remaining := len(activation.children)
	g.mu.Unlock()
	if remaining != 0 || graphSnapshot(t, ctx, g, second).State != Disposed {
		t.Fatal("parent shutdown retained structural sibling")
	}
}

func TestGraphLockTrackedActiveFailureInvalidatesDependantsBeforeCleanup(t *testing.T) {
	for _, panicWorker := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "panic"}[panicWorker], func(t *testing.T) {
			ctx := graphTestContext(t)
			g := graphTestRuntime(t).(*GraphLock)
			probe := &graphInspectErrorProbe{g: g, ctx: ctx}
			contexts := make(chan *Context, 1)
			var cleaned atomic.Bool
			provider := graphMount(t, ctx, g, Spec{ID: "tracked-provider", Provides: []string{"service"}, Apply: func(c *Context) error {
				contexts <- c
				if err := c.Provide("service", "retained"); err != nil {
					return err
				}
				return c.Defer("provider-effect", func(context.Context) error { cleaned.Store(true); return nil })
			}})
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			t.Cleanup(func() { once.Do(func() { close(release) }) })
			consumer := graphMount(t, ctx, g, Spec{ID: "dependent", Needs: []string{"service"}, Apply: func(c *Context) error {
				return c.Defer("consumer-effect", func(context.Context) error {
					close(started)
					<-release
					if value, err := c.Read("service"); err != nil || value != "retained" || cleaned.Load() {
						return errors.New("tracked failure released provider before dependent cleanup")
					}
					return nil
				})
			}})
			graphSettle(t, ctx, g)
			c := graphReceive(t, ctx, contexts)
			if err := c.Go("failure", func(context.Context) error {
				if _, err := g.Inspect(ctx); err != nil {
					return err
				}
				if panicWorker {
					panic(probe) // Error method reenters mu; recovery must format off-lock.
				}
				return probe
			}); err != nil {
				t.Fatal(err)
			}
			graphReceive(t, ctx, started)
			inspection := graphInspect(t, ctx, g)
			p, d := graphInspectedFiber(t, inspection, provider), graphInspectedFiber(t, inspection, consumer)
			if p.State != Unloading || p.Target != nil || p.Error == "" || len(p.Tracked) != 0 || d.State != Unloading || d.Target != nil || cleaned.Load() || len(inspection.Provisions) != 1 || inspection.Provisions[0].Active {
				t.Fatalf("tracked failure did not quiesce native dependency graph: %+v", inspection)
			}
			if err := c.Go("late", func(context.Context) error { return nil }); !errors.Is(err, ErrInactive) {
				t.Fatalf("failed activation accepted new tracked work: %v", err)
			}
			once.Do(func() { close(release) })
			graphSettle(t, ctx, g)
			if got := graphSnapshot(t, ctx, g, provider); got.State != Failed || got.Target != nil || got.Error == "" || !cleaned.Load() || len(got.Tracked) != 0 {
				t.Fatalf("tracked failure not sticky after real cleanup: %+v", got)
			}
			if !panicWorker {
				if err := g.AwaitFiber(ctx, provider); err != probe {
					t.Fatalf("async failure lost exact error identity: %T", err)
				}
			}
			if graphSnapshot(t, ctx, g, consumer).State != Pending {
				t.Fatal("dependent did not return to pending")
			}
			if err := g.Close(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGraphLockTrackedFailureDuringLoadingRetainsApplyUntilReturn(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	failure := errors.New("worker failed during Apply")
	contexts := make(chan *Context, 1)
	workerRelease, applyRelease := make(chan struct{}), make(chan struct{})
	var workerOnce, applyOnce sync.Once
	t.Cleanup(func() { workerOnce.Do(func() { close(workerRelease) }); applyOnce.Do(func() { close(applyRelease) }) })
	var cleaned atomic.Bool
	id := graphMount(t, ctx, g, Spec{ID: "loading-worker", Apply: func(c *Context) error {
		if err := c.Defer("partial", func(context.Context) error { cleaned.Store(true); return nil }); err != nil {
			return err
		}
		if err := c.Go("loading", func(context.Context) error { <-workerRelease; return failure }); err != nil {
			return err
		}
		contexts <- c
		<-applyRelease
		return nil
	}})
	c := graphReceive(t, ctx, contexts)
	workerOnce.Do(func() { close(workerRelease) })
	graphReceive(t, ctx, c.Std().Done())
	got := graphSnapshot(t, ctx, g, id)
	if got.State != Loading || got.Target != nil || got.Error != failure.Error() || len(got.Tracked) != 0 || cleaned.Load() || len(got.Effects) != 1 {
		t.Fatalf("tracked failure faked loading Apply completion: %+v", got)
	}
	if err := c.Defer("too-late", func(context.Context) error { return nil }); !errors.Is(err, ErrInactive) {
		t.Fatalf("quiescing Apply accepted new effect: %v", err)
	}
	applyOnce.Do(func() { close(applyRelease) })
	if err := g.AwaitFiber(ctx, id); err != failure {
		t.Fatalf("loading async failure not joinable: %v", err)
	}
	if got := graphSnapshot(t, ctx, g, id); got.State != Failed || !cleaned.Load() {
		t.Fatalf("loading worker did not unwind actual Apply: %+v", got)
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockTrackedLedgerUsesStartOrderCopiesAndRealDrain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := graphTestContext(t)
		g := graphTestRuntime(t).(*GraphLock)
		g.knobs.leaseDrain = time.Second
		contexts := make(chan *Context, 1)
		var cleaned atomic.Bool
		id := graphMount(t, ctx, g, Spec{ID: "tracked", Apply: func(c *Context) error {
			contexts <- c
			return c.Defer("effect", func(context.Context) error { cleaned.Store(true); return nil })
		}})
		graphSettle(t, ctx, g)
		c := graphReceive(t, ctx, contexts)
		releases := []chan struct{}{make(chan struct{}), make(chan struct{}), make(chan struct{})}
		var once [3]sync.Once
		t.Cleanup(func() {
			for i := range releases {
				once[i].Do(func() { close(releases[i]) })
			}
		})
		for i, label := range []string{"z", "a", "z"} {
			if err := c.Go(label, func(context.Context) error { <-releases[i]; return nil }); err != nil {
				t.Fatal(err)
			}
		}
		got := graphSnapshot(t, ctx, g, id)
		if !reflect.DeepEqual(got.Tracked, []string{"z", "a", "z"}) {
			t.Fatalf("tracked ledger sorted labels instead of native tokens: %+v", got)
		}
		got.Tracked[0] = "reader mutation"
		once[1].Do(func() { close(releases[1]) })
		synctest.Wait()
		if got := graphSnapshot(t, ctx, g, id); !reflect.DeepEqual(got.Tracked, []string{"z", "z"}) {
			t.Fatalf("completion or slice copy did not update native ledger: %+v", got)
		}
		if err := g.Dispose(ctx, id); err != nil {
			t.Fatal(err)
		}
		graphReceive(t, ctx, time.After(time.Second))
		synctest.Wait()
		if got := graphSnapshot(t, ctx, g, id); got.State != Unloading || len(got.Tracked) != 2 || cleaned.Load() || got.Error == "" {
			t.Fatalf("lease deadline pretended tracked workers vanished: %+v", got)
		}
		once[0].Do(func() { close(releases[0]) })
		once[2].Do(func() { close(releases[2]) })
		graphSettle(t, ctx, g)
		if got := graphSnapshot(t, ctx, g, id); got.State != Disposed || len(got.Tracked) != 0 || !cleaned.Load() {
			t.Fatalf("real worker completion did not release cleanup: %+v", got)
		}
		events, err := g.Events(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var starts, done int
		var lastDone, gone uint64
		for _, event := range events {
			switch event.Kind {
			case "go:start":
				starts++
			case "go:done":
				done++
				lastDone = event.Sequence
			case "effects:gone":
				gone = event.Sequence
			}
		}
		if starts != 3 || done != 3 || gone <= lastDone {
			t.Fatalf("tracked completion order: start %d done %d lastDone %d gone %d", starts, done, lastDone, gone)
		}
	})
}

func TestGraphLockTrackedSupersededErrorCannotPoisonReplacement(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	contexts := make(chan *Context, 2)
	id := graphMount(t, ctx, g, Spec{ID: "replace-tracked", Apply: func(c *Context) error { contexts <- c; return nil }})
	graphSettle(t, ctx, g)
	old := graphReceive(t, ctx, contexts)
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	if err := old.Go("stale", func(context.Context) error { <-release; return errors.New("superseded async failure") }); err != nil {
		t.Fatal(err)
	}
	if err := g.Update(ctx, id); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	graphSettle(t, ctx, g)
	fresh := graphReceive(t, ctx, contexts)
	got := graphSnapshot(t, ctx, g, id)
	if fresh == old || got.State != Active || got.Revision != 1 || got.Error != "" || got.Target == nil || len(got.Tracked) != 0 {
		t.Fatalf("stale async result poisoned replacement: %+v", got)
	}
	if err := old.Go("late", func(context.Context) error { return nil }); !errors.Is(err, ErrInactive) {
		t.Fatalf("old context launched tracked work on replacement: %v", err)
	}
	if err := fresh.Go("cancel", func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }); err != nil {
		t.Fatal(err)
	}
	if err := g.Dispose(ctx, id); err != nil {
		t.Fatal(err)
	}
	graphSettle(t, ctx, g)
	if got := graphSnapshot(t, ctx, g, id); got.State != Disposed || got.Error != "" || len(got.Tracked) != 0 {
		t.Fatalf("normal worker cancellation became failure: %+v", got)
	}
}

func TestGraphLockNativeTrackedDoneIsIdempotent(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	contexts := make(chan *Context, 1)
	id := graphMount(t, ctx, g, Spec{ID: "idempotent", Apply: func(c *Context) error { contexts <- c; return nil }})
	graphSettle(t, ctx, g)
	c := graphReceive(t, ctx, contexts)
	if !c.Enter() {
		t.Fatal("cannot acquire helper setup lease")
	}
	done, err := c.trackedStart("native")
	if err != nil {
		c.Leave()
		t.Fatal(err)
	}
	done(nil)
	done(errors.New("duplicate completion must not fail activation"))
	c.Leave()
	if got := graphSnapshot(t, ctx, g, id); got.State != Active || got.Error != "" || len(got.Tracked) != 0 {
		t.Fatalf("duplicate completion changed activation: %+v", got)
	}
	events, err := g.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var completions int
	for _, event := range events {
		if event.Kind == "go:done" {
			completions++
		}
	}
	if completions != 1 {
		t.Fatalf("native completion committed %d times", completions)
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockNativeRootIsLazyStableAndNotAUserFiber(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	if g.root != nil {
		t.Fatal("constructor eagerly allocated a builtin root")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if root, err := g.RootContext(canceled); root != nil || !errors.Is(err, context.Canceled) || g.root != nil {
		t.Fatalf("canceled root request allocated an owner: %p %v", root, err)
	}
	root, err := g.RootContext(ctx)
	if err != nil || root == nil || root.fiber != 0 {
		t.Fatalf("native root context: %p %v", root, err)
	}
	const callers = 8
	contexts := make(chan *Context, callers)
	for i := 0; i < callers; i++ {
		go func() { got, _ := g.RootContext(ctx); contexts <- got }()
	}
	for i := 0; i < callers; i++ {
		if got := graphReceive(t, ctx, contexts); got != root {
			t.Fatal("RootContext replaced the native root")
		}
	}
	if inspection := graphInspect(t, ctx, g); len(inspection.Fibers) != 0 || len(inspection.Provisions) != 0 || inspection.Sequence != 0 {
		t.Fatalf("root creation fabricated user lifecycle work: %+v", inspection)
	}
	g.mu.Lock()
	state, revision, worker := g.root.state, g.root.revision, g.root.worker
	g.mu.Unlock()
	if state != Active || revision != 1 || worker || !root.Enter() {
		t.Fatal("native root was not an idle ACTIVE revision-1 owner")
	}
	root.Leave()
	if err := root.Dispose(); !errors.Is(err, ErrRootDispose) {
		t.Fatalf("root self disposal did not preserve runtime ownership: %v", err)
	}
	if err := g.Update(ctx, 0); !errors.Is(err, ErrUnknown) {
		t.Fatalf("user Update controlled root: %v", err)
	}
	if err := g.Dispose(ctx, 0); !errors.Is(err, ErrUnknown) {
		t.Fatalf("user Dispose controlled root: %v", err)
	}
	if err := g.AwaitFiber(ctx, 0); !errors.Is(err, ErrUnknown) {
		t.Fatalf("user AwaitFiber joined root: %v", err)
	}
	user := graphMount(t, ctx, g, Spec{ID: "first-user"})
	if user != 1 {
		t.Fatalf("root consumed a user ID: %d", user)
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := g.RootContext(ctx); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed runtime returned native root: %v", err)
	}
	if root.Enter() {
		root.Leave()
		t.Fatal("closed root accepted a lease")
	}
}

func TestGraphLockRootProvisionsUseActualIdentityAndReservationOrder(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	root, err := g.RootContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	value := &graphRecordJSONProbe{g: g, ctx: ctx}
	consumer := graphMount(t, ctx, g, Spec{ID: "consumer", Needs: []string{"z"}, Apply: func(c *Context) error {
		got, err := c.Read("z")
		if err != nil || got != value {
			return errors.New("consumer did not bind actual root provision")
		}
		return nil
	}})
	if err := root.Provide("", value); err == nil {
		t.Fatal("trusted root accepted an empty provision key")
	}
	for _, key := range []string{"z", "a"} {
		if err := root.Provide(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if !root.Owns("z") || !root.Owns("a") {
		t.Fatal("root reservation did not publish native ownership")
	}
	if err := root.Provide("z", value); err == nil {
		t.Fatal("root duplicate provision accepted")
	}
	duplicate := graphMount(t, ctx, g, Spec{ID: "duplicate", Provides: []string{"a"}, Apply: func(c *Context) error { return c.Provide("a", "collision") }})
	graphSettle(t, ctx, g)
	if graphSnapshot(t, ctx, g, duplicate).State != Failed || graphSnapshot(t, ctx, g, consumer).State != Active {
		t.Fatal("root provision bypassed native binding/reservation constraints")
	}
	inspection := graphInspect(t, ctx, g)
	want := []ProvisionSnapshot{
		{Key: "z", UID: 1, Owner: 0, Name: "root", Value: value, Active: true},
		{Key: "a", UID: 2, Owner: 0, Name: "root", Value: value, Active: true},
	}
	if !reflect.DeepEqual(inspection.Provisions, want) || len(inspection.Fibers) != 2 {
		t.Fatalf("native root inspection fabricated data: %+v", inspection)
	}
	inspection.Provisions[0] = ProvisionSnapshot{Key: "reader mutation"}
	if got := graphInspect(t, ctx, g); !reflect.DeepEqual(got.Provisions, want) {
		t.Fatalf("root provision inspection leaked container ownership: %+v", got)
	}
	if err := g.Record(ctx, "after-inspection", nil); err != nil {
		t.Fatal(err)
	}
	events, err := g.Events(ctx)
	if err != nil || inspection.Sequence >= uint64(len(events)) {
		t.Fatalf("inspection watermark not an actual preceding ledger cut: %d %d %v", inspection.Sequence, len(events), err)
	}
	var published []string
	for _, event := range events {
		if event.Sequence <= inspection.Sequence && event.Kind == "fixture" {
			t.Fatal("native inspection invoked root value marshaler")
		}
		if event.Kind == "service:publish" {
			if event.Fiber != 0 || event.Name != "root" || event.Value != value {
				t.Fatalf("root publish lost native owner identity: %+v", event)
			}
			published = append(published, event.Key)
		}
		if event.Fiber == 0 && event.Kind == "mount" {
			t.Fatal("root introduced hidden plugin mount")
		}
	}
	if !reflect.DeepEqual(published, []string{"z", "a"}) {
		t.Fatalf("root publish order: %v", published)
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if got := graphInspect(t, ctx, g); len(got.Provisions) != 0 || len(got.Fibers) != 2 {
		t.Fatalf("native root shutdown leaked service or user row: %+v", got)
	}
}

func TestGraphLockRootCloseJoinsUsersAndNativeChildrenBeforeEffects(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	root, err := g.RootContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Provide("service", "retained"); err != nil {
		t.Fatal(err)
	}
	var rootCleaned atomic.Bool
	if err := root.Defer("root-effect", func(context.Context) error {
		if _, err := g.Inspect(ctx); err != nil {
			return err
		}
		rootCleaned.Store(true)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan string, 2), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	makeSpec := func(name string) Spec {
		return Spec{ID: name, Needs: []string{"service"}, Apply: func(c *Context) error {
			return c.Defer(name, func(context.Context) error {
				started <- name
				<-release
				if value, err := c.Read("service"); err != nil || value != "retained" || rootCleaned.Load() {
					return errors.New("root cleanup outran native consumer/child teardown")
				}
				return nil
			})
		}}
	}
	user := graphMount(t, ctx, g, makeSpec("user"))
	child, err := root.MountChild(makeSpec("child"))
	if err != nil {
		t.Fatal(err)
	}
	graphSettle(t, ctx, g)
	closed := make(chan error, 1)
	go func() { closed <- g.Close(ctx) }()
	graphReceive(t, ctx, started)
	graphReceive(t, ctx, started)
	inspection := graphInspect(t, ctx, g)
	if rootCleaned.Load() || len(inspection.Provisions) != 1 || inspection.Provisions[0].Active || inspection.Provisions[0].Owner != 0 || graphInspectedFiber(t, inspection, user).State != Unloading || graphInspectedFiber(t, inspection, child).State != Unloading {
		t.Fatalf("root Close faked completion: %+v", inspection)
	}
	select {
	case err := <-closed:
		t.Fatalf("Close returned while consumers held cleanup: %v", err)
	default:
	}
	if err := root.Provide("late", true); !errors.Is(err, ErrInactive) {
		t.Fatalf("retiring root accepted late provision: %v", err)
	}
	once.Do(func() { close(release) })
	if err := graphReceive(t, ctx, closed); err != nil {
		t.Fatal(err)
	}
	g.mu.Lock()
	state, current, fibers, services, indexes := g.root.state, g.root.current, len(g.fibers), len(g.services), len(g.indexByKey)
	g.mu.Unlock()
	if !rootCleaned.Load() || state != Disposed || current != nil || fibers != 0 || services != 0 || indexes != 0 || root.Owns("service") {
		t.Fatal("root Close retained native active references")
	}
	if err := g.Close(ctx); err != nil {
		t.Fatalf("idempotent root Close: %v", err)
	}
}

func TestGraphLockRootCloseRetainsTrackedWorkAcrossLeaseDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := graphTestContext(t)
		g := graphTestRuntime(t).(*GraphLock)
		g.knobs.leaseDrain = time.Second
		root, err := g.RootContext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := root.Provide("service", "retained"); err != nil {
			t.Fatal(err)
		}
		var cleaned atomic.Bool
		if err := root.Defer("root-effect", func(context.Context) error { cleaned.Store(true); return nil }); err != nil {
			t.Fatal(err)
		}
		release := make(chan struct{})
		var once sync.Once
		t.Cleanup(func() { once.Do(func() { close(release) }) })
		if err := root.Go("stuck-root", func(context.Context) error { <-release; return nil }); err != nil {
			t.Fatal(err)
		}
		closeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		closed := make(chan error, 1)
		go func() { closed <- g.Close(closeCtx) }()
		graphReceive(t, ctx, root.Std().Done())
		graphReceive(t, ctx, time.After(time.Second))
		synctest.Wait()
		g.mu.Lock()
		tracked, effects, current, diagnostic := len(g.root.current.tracked), len(g.root.current.effects), g.root.current, g.root.lastErr
		g.mu.Unlock()
		if tracked != 1 || effects != 2 || current == nil || diagnostic == nil || cleaned.Load() {
			t.Fatal("root lease deadline discarded real work or effects")
		}
		if inspection := graphInspect(t, ctx, g); len(inspection.Fibers) != 0 || len(inspection.Provisions) != 1 || inspection.Provisions[0].Active {
			t.Fatalf("held root introduced user row or removed service: %+v", inspection)
		}
		if err := graphReceive(t, ctx, closed); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("external Close timeout: %v", err)
		}
		once.Do(func() { close(release) })
		graphSettle(t, ctx, g)
		if err := g.Close(ctx); err == nil || !cleaned.Load() {
			t.Fatal("completed root lost real lease diagnostic or cleanup")
		}
	})
}

func TestGraphLockRootCleanupDeadlineDoesNotFinishDisposer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := graphTestContext(t)
		g := graphTestRuntime(t).(*GraphLock)
		g.knobs.cleanup = time.Second
		root, err := g.RootContext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		started, release := make(chan context.Context, 1), make(chan struct{})
		var once sync.Once
		t.Cleanup(func() { once.Do(func() { close(release) }) })
		if err := root.Defer("held-root", func(cleanup context.Context) error { started <- cleanup; <-release; return nil }); err != nil {
			t.Fatal(err)
		}
		closed := make(chan error, 1)
		go func() { closed <- g.Close(ctx) }()
		cleanup := graphReceive(t, ctx, started)
		graphReceive(t, ctx, cleanup.Done())
		synctest.Wait()
		g.mu.Lock()
		retained, diagnostic := g.root.current != nil && len(g.root.current.effects) == 1, g.root.lastErr
		g.mu.Unlock()
		if !retained || diagnostic == nil {
			t.Fatal("root cleanup deadline pretended disposer finished")
		}
		select {
		case err := <-closed:
			t.Fatalf("root Close completed while disposer blocked: %v", err)
		default:
		}
		once.Do(func() { close(release) })
		if err := graphReceive(t, ctx, closed); err == nil {
			t.Fatal("root cleanup diagnostic disappeared")
		}
	})
}

func TestGraphLockRootTrackedFailureIsContainedAndReturnedByClose(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	root, err := g.RootContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Provide("service", "actual"); err != nil {
		t.Fatal(err)
	}
	child, err := root.MountChild(Spec{ID: "root-child"})
	if err != nil {
		t.Fatal(err)
	}
	consumer := graphMount(t, ctx, g, Spec{ID: "consumer", Needs: []string{"service"}})
	unrelated := graphMount(t, ctx, g, Spec{ID: "unrelated"})
	graphSettle(t, ctx, g)
	failure := &graphInspectErrorProbe{g: g, ctx: ctx}
	if err := root.Go("failure", func(context.Context) error { return failure }); err != nil {
		t.Fatal(err)
	}
	graphReceive(t, ctx, root.Std().Done())
	graphSettle(t, ctx, g)
	if graphSnapshot(t, ctx, g, child).State != Disposed || graphSnapshot(t, ctx, g, consumer).State != Pending || graphSnapshot(t, ctx, g, unrelated).State != Active {
		t.Fatal("root worker failure escaped structural/dependency scope")
	}
	if _, err := g.RootContext(ctx); !errors.Is(err, ErrInactive) {
		t.Fatalf("terminal root silently restarted: %v", err)
	}
	if inspection := graphInspect(t, ctx, g); len(inspection.Provisions) != 0 || len(inspection.Fibers) != 3 || failure.calls.Load() != 0 {
		t.Fatalf("root failure inspection fabricated user row or formatted error: %+v", inspection)
	}
	if err := g.Close(ctx); !errors.Is(err, failure) {
		t.Fatalf("Close lost native root failure identity: %T", err)
	}
	if status := g.ObserverStatus(); !status.Stopped || status.Delivered != status.Accepted || status.Remaining != 0 {
		t.Fatalf("failed root skipped actual observer queue shutdown: %+v", status)
	}
}

func TestGraphLockObserversDeliverActualLedgerOutsideGraphOwner(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	root, err := g.RootContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var seen []Event
	_, err = g.ObserveEvents(root, "whole-ledger", func(_ context.Context, event Event) error {
		if _, err := g.Inspect(ctx); err != nil {
			return err
		}
		mu.Lock()
		seen = append(seen, event)
		mu.Unlock()
		if event.Kind == "fixture" && event.Key == "observer/request" {
			return g.Record(ctx, "observer/reentered", map[string]any{"actual": true})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := g.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	provider := graphMount(t, ctx, g, Spec{ID: "provider", Provides: []string{"service"}, Apply: func(c *Context) error {
		if err := c.Provide("service", "actual"); err != nil {
			return err
		}
		return c.Defer("effect", func(context.Context) error { return nil })
	}})
	graphMount(t, ctx, g, Spec{ID: "consumer", Needs: []string{"service"}})
	graphSettle(t, ctx, g)
	if err := g.Record(ctx, "observer/request", nil); err != nil {
		t.Fatal(err)
	}
	if err := g.Update(ctx, provider); err != nil {
		t.Fatal(err)
	}
	graphSettle(t, ctx, g)
	if err := g.AwaitObservers(ctx); err != nil {
		t.Fatal(err)
	}
	// A callback can append after a captured barrier; take a second explicit
	// barrier to include its native reentrant Record rather than infer delivery.
	if err := g.AwaitObservers(ctx); err != nil {
		t.Fatal(err)
	}
	events, err := g.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	observed := append([]Event(nil), seen...)
	mu.Unlock()
	if !reflect.DeepEqual(observed, events[len(before):]) {
		t.Fatalf("observer stream differs from actual native ledger: got %v want %v", observed, events[len(before):])
	}
	status := g.ObserverStatus()
	if status.Accepted != uint64(len(events)) || status.Delivered != status.Accepted || status.ObserverErrors != 0 || status.Remaining != 0 || status.Observers != 1 {
		t.Fatalf("native observer queue watermark: %+v", status)
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
	status = g.ObserverStatus()
	if !status.Stopped || status.Accepted != status.Delivered || status.Remaining != 0 || status.Observers != 0 {
		t.Fatalf("Close did not finish actual dispatcher: %+v", status)
	}
}

func TestGraphLockObserverCallbackLeaseRetainsOwnerAndSettleDoesNotJoin(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	contexts := make(chan *Context, 1)
	var cleaned atomic.Bool
	id := graphMount(t, ctx, g, Spec{ID: "observer-owner", Apply: func(c *Context) error {
		contexts <- c
		return c.Defer("effect", func(context.Context) error { cleaned.Store(true); return nil })
	}})
	graphSettle(t, ctx, g)
	owner := graphReceive(t, ctx, contexts)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	_, err := g.ObserveEvents(owner, "held", func(context.Context, Event) error { close(started); <-release; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Record(ctx, "trigger", nil); err != nil {
		t.Fatal(err)
	}
	graphReceive(t, ctx, started)
	// No lifecycle transition exists yet. A blocked observer must not become an
	// implicit global barrier in Settle or prevent other commands using mu.
	graphSettle(t, ctx, g)
	other := graphMount(t, ctx, g, Spec{ID: "independent"})
	if err := g.AwaitFiber(ctx, other); err != nil {
		t.Fatal(err)
	}
	if err := g.Dispose(ctx, id); err != nil {
		t.Fatal(err)
	}
	got := graphSnapshot(t, ctx, g, id)
	if got.State != Unloading || len(got.Effects) != 2 || cleaned.Load() {
		t.Fatalf("observer callback lease did not retain owner effects: %+v", got)
	}
	status := g.ObserverStatus()
	if !status.InFlight || status.Remaining == 0 {
		t.Fatalf("blocked callback vanished from queue status: %+v", status)
	}
	once.Do(func() { close(release) })
	graphSettle(t, ctx, g)
	if err := g.AwaitObservers(ctx); err != nil {
		t.Fatal(err)
	}
	if !cleaned.Load() || graphSnapshot(t, ctx, g, id).State != Disposed || g.ObserverStatus().Skipped == 0 {
		t.Fatal("quiescing observer callbacks were not skipped or real owner cleanup failed")
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockObserverBindingAndFailuresDoNotFailActivations(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	foreign := graphTestRuntime(t).(*GraphLock)
	root, err := g.RootContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	other, err := foreign.RootContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range []*Context{nil, other} {
		if _, err := g.ObserveEvents(owner, "foreign", func(context.Context, Event) error { return nil }); !errors.Is(err, ErrObserverBinding) {
			t.Fatalf("observer accepted nil/foreign native context: %v", err)
		}
	}
	probe := &graphInspectErrorProbe{g: g, ctx: ctx}
	failing, err := g.ObserveEvents(root, "errors", func(_ context.Context, event Event) error {
		if event.Kind == "fixture" && event.Key == "observer/panic" {
			panic(probe)
		}
		return probe
	})
	if err != nil {
		t.Fatal(err)
	}
	var successes atomic.Uint64
	successful, err := g.ObserveEvents(root, "continue", func(context.Context, Event) error { successes.Add(1); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Record(ctx, "observer/panic", nil); err != nil {
		t.Fatal(err)
	}
	if err := g.Record(ctx, "observer/error", nil); err != nil {
		t.Fatal(err)
	}
	if err := g.AwaitObservers(ctx); err != nil {
		t.Fatalf("observer failure incorrectly rejected barrier: %v", err)
	}
	status := g.ObserverStatus()
	g.mu.Lock()
	active := g.root.state == Active && !g.root.current.retiring
	g.mu.Unlock()
	if !active || status.ObserverErrors < 2 || successes.Load() < 2 || status.LastObserverError == "" || probe.calls.Load() == 0 {
		t.Fatalf("off-owner diagnostics failed or affected lifecycle: %+v", status)
	}
	if err := failing(ctx); err != nil {
		t.Fatal(err)
	}
	if err := successful(ctx); err != nil {
		t.Fatal(err)
	}
	if err := g.Close(ctx); err != nil {
		t.Fatalf("observer diagnostics became runtime Close failure: %v", err)
	}
	if err := foreign.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockObserverCanRequestOwnDisposalButNotJoinDispatcher(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	contexts := make(chan *Context, 1)
	id := graphMount(t, ctx, g, Spec{ID: "self-observer", Apply: func(c *Context) error { contexts <- c; return nil }})
	graphSettle(t, ctx, g)
	owner := graphReceive(t, ctx, contexts)
	checked := make(chan bool, 1)
	_, err := g.ObserveEvents(owner, "self-disposal", func(callback context.Context, _ Event) error {
		if err := g.AwaitObservers(callback); err == nil {
			return errors.New("observer joined its own dispatcher")
		}
		err := owner.Dispose()
		checked <- err == nil
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Record(ctx, "dispose-request", nil); err != nil {
		t.Fatal(err)
	}
	if !graphReceive(t, ctx, checked) {
		t.Fatal("callback could not request own disposal")
	}
	if err := g.AwaitFiber(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := g.AwaitObservers(ctx); err != nil {
		t.Fatal(err)
	}
	if got := graphSnapshot(t, ctx, g, id); got.State != Disposed || len(got.Effects) != 0 {
		t.Fatalf("observer self-disposal did not really complete: %+v", got)
	}
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGraphLockCloseTimeoutDoesNotPrematurelyCloseObserverAdmission(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	root, err := g.RootContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	_, err = g.ObserveEvents(root, "held", func(context.Context, Event) error { close(started); <-release; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Record(ctx, "start", nil); err != nil {
		t.Fatal(err)
	}
	graphReceive(t, ctx, started)
	bounded, cancel := context.WithCancel(ctx)
	closed := make(chan error, 1)
	go func() { closed <- g.Close(bounded) }()
	graphReceive(t, ctx, root.Std().Done())
	cancel()
	if err := graphReceive(t, ctx, closed); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close bounded by live callback: %v", err)
	}
	if status := g.ObserverStatus(); status.Closing || status.Stopped || !status.InFlight {
		t.Fatalf("lifecycle timeout closed dispatcher before later native events: %+v", status)
	}
	once.Do(func() { close(release) })
	if err := g.Close(ctx); err != nil {
		t.Fatal(err)
	}
	status := g.ObserverStatus()
	events, err := g.Events(ctx)
	if err != nil || !status.Stopped || status.Accepted != uint64(len(events)) || status.Delivered != status.Accepted || status.Remaining != 0 {
		t.Fatalf("retry Close lost actual post-timeout events: %+v events %d err %v", status, len(events), err)
	}
}

func TestGraphLockRootCloseJoinsUnrelatedUserCleanupBeforeHostEffects(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := graphTestContext(t)
		g := graphTestRuntime(t).(*GraphLock)
		root, err := g.RootContext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var hostCleaned atomic.Bool
		if err := root.Defer("host-resource", func(context.Context) error { hostCleaned.Store(true); return nil }); err != nil {
			t.Fatal(err)
		}
		started, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		t.Cleanup(func() { once.Do(func() { close(release) }) })
		user := graphMount(t, ctx, g, Spec{ID: "ordinary-user-with-no-root-binding", Apply: func(c *Context) error {
			return c.Defer("user-resource", func(context.Context) error {
				close(started)
				<-release
				if hostCleaned.Load() {
					return errors.New("host resources released before unrelated user cleanup")
				}
				return nil
			})
		}})
		graphSettle(t, ctx, g)
		closed := make(chan error, 1)
		go func() { closed <- g.Close(ctx) }()
		graphReceive(t, ctx, started)
		synctest.Wait()
		if hostCleaned.Load() {
			t.Fatal("root Close only joined service consumers and native children")
		}
		if got := graphSnapshot(t, ctx, g, user); got.State != Unloading || len(got.Effects) != 1 {
			t.Fatalf("unrelated user cleanup falsely completed: %+v", got)
		}
		select {
		case err := <-closed:
			t.Fatalf("Close returned before ordinary user cleanup: %v", err)
		default:
		}
		once.Do(func() { close(release) })
		if err := graphReceive(t, ctx, closed); err != nil {
			t.Fatal(err)
		}
		if !hostCleaned.Load() || graphSnapshot(t, ctx, g, user).Error != "" {
			t.Fatal("ordinary user did not finish before host cleanup")
		}
	})
}

func TestGraphLockProvisionOwnershipUnwindsInActualLIFOOrder(t *testing.T) {
	for _, trustedRoot := range []bool{false, true} {
		t.Run(map[bool]string{false: "user", true: "root"}[trustedRoot], func(t *testing.T) {
			ctx := graphTestContext(t)
			g := graphTestRuntime(t).(*GraphLock)
			var calls []string
			cleanup := func(c *Context, label string, wantA, wantB bool) Disposer {
				return func(context.Context) error {
					inspection := graphInspect(t, ctx, g)
					keys := make(map[string]bool)
					for _, provision := range inspection.Provisions {
						keys[provision.Key] = true
					}
					if keys["a"] != wantA || keys["b"] != wantB || c.Owns("a") != wantA || c.Owns("b") != wantB {
						return errors.New("provision removed outside its native LIFO slot at " + label)
					}
					calls = append(calls, label)
					return nil
				}
			}
			setup := func(c *Context) error {
				if err := c.Defer("old", cleanup(c, "old", false, false)); err != nil {
					return err
				}
				if err := c.Provide("a", "actual-a"); err != nil {
					return err
				}
				if err := c.Defer("middle", cleanup(c, "middle", true, false)); err != nil {
					return err
				}
				if err := c.Provide("b", "actual-b"); err != nil {
					return err
				}
				return c.Defer("new", cleanup(c, "new", true, true))
			}
			var owner *Context
			var id uint64
			if trustedRoot {
				var err error
				owner, err = g.RootContext(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if err := setup(owner); err != nil {
					t.Fatal(err)
				}
			} else {
				contexts := make(chan *Context, 1)
				id = graphMount(t, ctx, g, Spec{ID: "lifo", Provides: []string{"a", "b"}, ApplyEffect: func(c *Context) (Disposer, error) {
					contexts <- c
					if err := setup(c); err != nil {
						return nil, err
					}
					return cleanup(c, "returned", true, true), nil
				}})
				graphSettle(t, ctx, g)
				owner = graphReceive(t, ctx, contexts)
				got := graphSnapshot(t, ctx, g, id)
				if !reflect.DeepEqual(got.Effects, []string{"old", "provide:a", "middle", "provide:b", "new", "apply"}) {
					t.Fatalf("provision ownership missing from actual effect ledger: %+v", got)
				}
			}
			if trustedRoot {
				if err := g.Close(ctx); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := g.Dispose(ctx, id); err != nil {
					t.Fatal(err)
				}
				graphSettle(t, ctx, g)
				if got := graphSnapshot(t, ctx, g, id); got.Error != "" {
					t.Fatal(got.Error)
				}
			}
			want := []string{"new", "middle", "old"}
			if !trustedRoot {
				want = append([]string{"returned"}, want...)
			}
			if !reflect.DeepEqual(calls, want) || owner.Owns("a") || owner.Owns("b") {
				t.Fatalf("custom/provision cleanup order %v want %v", calls, want)
			}
			events, err := g.Events(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var removed []string
			for _, event := range events {
				if event.Kind == "service:remove" {
					removed = append(removed, event.Key)
					if event.State != Unloading || event.Value != "actual-"+event.Key {
						t.Fatalf("actual removal lost identity/state: %+v", event)
					}
				}
			}
			if !reflect.DeepEqual(removed, []string{"b", "a"}) {
				t.Fatalf("service removal used declaration order instead of native ownership order: %v", removed)
			}
		})
	}
}

func TestGraphLockProvisionLIFORemovalVisibleDuringOlderHeldCleanup(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	started, release := make(chan *Context, 1), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	failure := errors.New("partial Apply failure")
	id := graphMount(t, ctx, g, Spec{ID: "partial-provision", Provides: []string{"service"}, Apply: func(c *Context) error {
		if err := c.Defer("older", func(context.Context) error { started <- c; <-release; return nil }); err != nil {
			return err
		}
		if err := c.Provide("service", "actual"); err != nil {
			return err
		}
		return failure
	}})
	owner := graphReceive(t, ctx, started)
	inspection := graphInspect(t, ctx, g)
	got := graphInspectedFiber(t, inspection, id)
	if len(inspection.Provisions) != 0 || owner.Owns("service") || got.State != Unloading || !reflect.DeepEqual(got.Effects, []string{"older"}) {
		t.Fatalf("removed provision stayed owned during older callback: %+v", inspection)
	}
	once.Do(func() { close(release) })
	if err := g.AwaitFiber(ctx, id); err != failure {
		t.Fatalf("partial cleanup replaced actual startup rejection: %v", err)
	}
	if got := graphSnapshot(t, ctx, g, id); got.State != Failed || len(got.Effects) != 0 {
		t.Fatalf("partial reservation did not unwind completely: %+v", got)
	}
}

func TestGraphLockDependencyWaitWakesOnlyActualReleasedProvider(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	var providers [2]uint64
	var consumers [2]uint64
	var activations [2]*graphActivation
	var waits [2]<-chan struct{}
	started := make(chan int, 2)
	releases := [2]chan struct{}{make(chan struct{}), make(chan struct{})}
	var once [2]sync.Once
	t.Cleanup(func() {
		for i := range releases {
			once[i].Do(func() { close(releases[i]) })
		}
	})
	for i, key := range []string{"a", "b"} {
		providers[i] = graphMount(t, ctx, g, Spec{ID: "provider-" + key, Provides: []string{key}, Apply: func(c *Context) error { return c.Provide(key, key) }})
		consumers[i] = graphMount(t, ctx, g, Spec{ID: "consumer-" + key, Needs: []string{key}, Apply: func(c *Context) error {
			return c.Defer("held", func(context.Context) error { started <- i; <-releases[i]; return nil })
		}})
	}
	graphSettle(t, ctx, g)
	g.mu.Lock()
	for i := range providers {
		activations[i] = g.fibers[providers[i]].current
	}
	g.mu.Unlock()
	for _, id := range providers {
		if err := g.Dispose(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	graphReceive(t, ctx, started)
	graphReceive(t, ctx, started)
	g.mu.Lock()
	for i := range activations {
		waits[i] = activations[i].dependantsChanged
	}
	g.mu.Unlock()
	if err := g.Record(ctx, "unrelated-ledger-event", nil); err != nil {
		t.Fatal(err)
	}
	other := graphMount(t, ctx, g, Spec{ID: "unrelated"})
	if err := g.AwaitFiber(ctx, other); err != nil {
		t.Fatal(err)
	}
	for i, changed := range waits {
		select {
		case <-changed:
			t.Fatalf("unrelated state/debug event woke provider %d", i)
		default:
		}
	}
	once[0].Do(func() { close(releases[0]) })
	if err := g.AwaitFiber(ctx, providers[0]); err != nil {
		t.Fatal(err)
	}
	graphReceive(t, ctx, waits[0])
	select {
	case <-waits[1]:
		t.Fatal("releasing consumer a woke unrelated provider b")
	default:
	}
	if got := graphSnapshot(t, ctx, g, consumers[0]); got.State != Pending || got.Target != nil {
		t.Fatalf("actual provider release failed to reach stable consumer state: %+v", got)
	}
	once[1].Do(func() { close(releases[1]) })
	if err := g.AwaitFiber(ctx, providers[1]); err != nil {
		t.Fatal(err)
	}
	graphReceive(t, ctx, waits[1])
	graphSettle(t, ctx, g)
}

func TestGraphLockDependencyReleaseBeforeProviderWaitIsNotMissed(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	provider := graphMount(t, ctx, g, Spec{ID: "provider", Provides: []string{"service"}, Apply: func(c *Context) error { return c.Provide("service", "actual") }})
	consumer := graphMount(t, ctx, g, Spec{ID: "consumer", Needs: []string{"service"}})
	graphSettle(t, ctx, g)
	if err := g.Dispose(ctx, consumer); err != nil {
		t.Fatal(err)
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
	if got := graphSnapshot(t, ctx, g, provider); got.State != Disposed || got.Error != "" {
		t.Fatalf("provider missed a release completed before its wait: %+v", got)
	}
}

func TestGraphLockBatchValidatesEntirePrefixBeforeMutation(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	id := graphMount(t, ctx, g, Spec{ID: "existing"})
	graphSettle(t, ctx, g)
	before := graphInspect(t, ctx, g)
	g.mu.Lock()
	next := g.nextFiber
	g.mu.Unlock()
	for _, operations := range [][]BatchOperation{
		{{Kind: BatchMount, Spec: Spec{ID: "uncommitted"}}, {Kind: BatchUpdate, Fiber: id}, {Kind: BatchDispose, Fiber: 9999}},
		{{Kind: BatchDispose, Fiber: id}, {Kind: BatchUpdate, Fiber: id}},
		{{Kind: BatchMount, Spec: Spec{ID: "uncommitted"}}, {Kind: BatchMount, Spec: Spec{ID: "invalid", Needs: []string{"x", "x"}}}},
		{{Kind: BatchUpdate, Fiber: 0}},
	} {
		if ids, err := g.Batch(ctx, operations); err == nil || ids != nil {
			t.Fatalf("invalid suffix committed a prefix: ids %v err %v", ids, err)
		}
		if got := graphInspect(t, ctx, g); !reflect.DeepEqual(got, before) {
			t.Fatalf("invalid Batch changed actual graph/ledger: before %+v after %+v", before, got)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if ids, err := g.Batch(canceled, []BatchOperation{{Kind: BatchMount, Spec: Spec{ID: "canceled"}}}); !errors.Is(err, context.Canceled) || ids != nil {
		t.Fatalf("canceled Batch mutated graph: %v %v", ids, err)
	}
	g.mu.Lock()
	unchanged := g.nextFiber == next && g.batchDepth == 0 && len(g.batchDirty) == 0 && len(g.batchSeen) == 0
	g.mu.Unlock()
	if !unchanged || graphMount(t, ctx, g, Spec{ID: "after-rollback"}) != next+1 {
		t.Fatal("failed Batch consumed user IDs or retained reconciliation transaction")
	}
}

func TestGraphLockBatchTwoUpdatesAdmitOnlyLatestActivation(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	var applies atomic.Int64
	id := graphMount(t, ctx, g, Spec{ID: "coalesced", Apply: func(*Context) error { applies.Add(1); return nil }})
	graphSettle(t, ctx, g)
	ids, err := g.Batch(ctx, []BatchOperation{{Kind: BatchUpdate, Fiber: id}, {Kind: BatchUpdate, Fiber: id}})
	if err != nil || !reflect.DeepEqual(ids, []uint64{0, 0}) {
		t.Fatalf("actual batch results: %v %v", ids, err)
	}
	graphSettle(t, ctx, g)
	got := graphSnapshot(t, ctx, g, id)
	if got.State != Active || got.Revision != 2 || got.Target == nil || got.Target.Revision != 2 || applies.Load() != 2 {
		t.Fatalf("Batch ran an intermediate revision: %+v applies %d", got, applies.Load())
	}
	events, err := g.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var updates, loads int
	for _, event := range events {
		if event.Kind == "update" {
			updates++
		}
		if event.Kind == "state" && event.State == Loading {
			loads++
		}
	}
	if updates != 2 || loads != 2 {
		t.Fatalf("Batch hid desired changes or admitted extra load: updates %d loads %d", updates, loads)
	}
	if _, err := g.Batch(ctx, []BatchOperation{{Kind: BatchDispose, Fiber: id}, {Kind: BatchDispose, Fiber: id}}); err != nil {
		t.Fatal(err)
	}
	graphSettle(t, ctx, g)
	if graphSnapshot(t, ctx, g, id).State != Disposed {
		t.Fatal("repeated disposal did not settle")
	}
}

func TestGraphLockBatchColdConsumerCannotBindProviderOldEpoch(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	var generation atomic.Int64
	provider := graphMount(t, ctx, g, Spec{ID: "provider", Provides: []string{"service"}, Apply: func(c *Context) error { return c.Provide("service", generation.Add(1)) }})
	graphSettle(t, ctx, g)
	old := graphInspect(t, ctx, g).Provisions[0].UID
	values := make(chan any, 2)
	needs := []string{"service"}
	operations := []BatchOperation{
		{Kind: BatchMount, Spec: Spec{ID: "cold", Needs: needs, Apply: func(c *Context) error {
			value, err := c.Read("service")
			if err == nil {
				values <- value
			}
			return err
		}}},
		{Kind: BatchUpdate, Fiber: provider},
	}
	ids, err := g.Batch(ctx, operations)
	if err != nil || len(ids) != 2 || ids[0] == 0 || ids[1] != 0 {
		t.Fatalf("Batch result: %v %v", ids, err)
	}
	needs[0], operations[0].Spec.ID = "caller mutation", "caller mutation"
	graphSettle(t, ctx, g)
	if value := graphReceive(t, ctx, values); value != int64(2) {
		t.Fatalf("cold Batch consumer ran against pre-commit provider: %v", value)
	}
	select {
	case value := <-values:
		t.Fatalf("cold consumer also ran an intermediate activation: %v", value)
	default:
	}
	got := graphSnapshot(t, ctx, g, ids[0])
	if got.State != Active || got.Name != "cold" || len(got.Epoch) != 1 || got.Epoch[0] == old || got.Error != "" {
		t.Fatalf("owned Batch declaration/new native UID: %+v", got)
	}
}

func TestGraphLockBatchWaitsOldApplyBeforeLatestTarget(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	started, release := make(chan *Context, 1), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	var applies atomic.Int64
	id := graphMount(t, ctx, g, Spec{ID: "held-apply", Apply: func(c *Context) error {
		if applies.Add(1) == 1 {
			started <- c
			<-release
		}
		return nil
	}})
	old := graphReceive(t, ctx, started)
	if _, err := g.Batch(ctx, []BatchOperation{{Kind: BatchUpdate, Fiber: id}, {Kind: BatchUpdate, Fiber: id}}); err != nil {
		t.Fatal(err)
	}
	graphReceive(t, ctx, old.Std().Done())
	if got := graphSnapshot(t, ctx, g, id); got.State != Loading || got.Revision != 2 || got.Target == nil || got.Target.Revision != 2 || applies.Load() != 1 {
		t.Fatalf("Batch overlapped old Apply or lost latest target: %+v", got)
	}
	once.Do(func() { close(release) })
	graphSettle(t, ctx, g)
	if got := graphSnapshot(t, ctx, g, id); got.State != Active || got.Revision != 2 || got.Error != "" || applies.Load() != 2 {
		t.Fatalf("latest target did not replace sole old writer: %+v", got)
	}
}

func TestGraphLockEffectLateCleanupUsesFreshBudgetAndRetainsLease(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	contexts := make(chan *Context, 1)
	var existingCleaned atomic.Bool
	id := graphMount(t, ctx, g, Spec{ID: "effect-owner", Apply: func(c *Context) error {
		contexts <- c
		return c.Defer("existing", func(context.Context) error { existingCleaned.Store(true); return nil })
	}})
	graphSettle(t, ctx, g)
	owner := graphReceive(t, ctx, contexts)
	setupStarted, releaseSetup := make(chan struct{}), make(chan struct{})
	cleanupStarted, releaseCleanup := make(chan context.Context, 1), make(chan struct{})
	var setupOnce, cleanupOnce sync.Once
	t.Cleanup(func() { setupOnce.Do(func() { close(releaseSetup) }); cleanupOnce.Do(func() { close(releaseCleanup) }) })
	result := make(chan error, 1)
	go func() {
		result <- owner.Effect("late", func() (Disposer, error) {
			close(setupStarted)
			<-releaseSetup
			return func(cleanup context.Context) error {
				if cleanup.Err() != nil {
					return errors.New("immediate cleanup inherited canceled activation")
				}
				if _, exists := cleanup.Deadline(); !exists {
					return errors.New("immediate cleanup has no native budget")
				}
				if _, err := g.Inspect(ctx); err != nil {
					return err
				}
				cleanupStarted <- cleanup
				<-releaseCleanup
				return nil
			}, nil
		})
	}()
	graphReceive(t, ctx, setupStarted)
	if err := g.Dispose(ctx, id); err != nil {
		t.Fatal(err)
	}
	graphReceive(t, ctx, owner.Std().Done())
	setupOnce.Do(func() { close(releaseSetup) })
	cleanup := graphReceive(t, ctx, cleanupStarted)
	if cleanup.Err() != nil || existingCleaned.Load() {
		t.Fatal("late cleanup lost fresh context or activation ownership")
	}
	if got := graphSnapshot(t, ctx, g, id); got.State != Unloading || !reflect.DeepEqual(got.Effects, []string{"existing"}) {
		t.Fatalf("late setup disposer incorrectly registered or released old effects: %+v", got)
	}
	select {
	case err := <-result:
		t.Fatalf("Effect returned before real late cleanup: %v", err)
	default:
	}
	cleanupOnce.Do(func() { close(releaseCleanup) })
	if err := graphReceive(t, ctx, result); !errors.Is(err, ErrInactiveEffect) || !errors.Is(err, ErrInactive) {
		t.Fatalf("late effect rejection: %v", err)
	}
	graphSettle(t, ctx, g)
	if got := graphSnapshot(t, ctx, g, id); got.State != Disposed || !existingCleaned.Load() || got.Error != "" {
		t.Fatalf("late cleanup did not finish before activation effects: %+v", got)
	}
}

func TestGraphLockEffectRollbackPanicIsContainedOutsideGraphOwner(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	setupErr := errors.New("partial setup failure")
	probe := &graphInspectErrorProbe{g: g, ctx: ctx}
	var cleaned atomic.Int64
	id := graphMount(t, ctx, g, Spec{ID: "partial-effect", Apply: func(c *Context) error {
		return c.Effect("partial", func() (Disposer, error) {
			return func(context.Context) error { cleaned.Add(1); panic(probe) }, setupErr
		})
	}})
	if err := g.AwaitFiber(ctx, id); !errors.Is(err, setupErr) {
		t.Fatalf("cleanup panic lost setup rejection: %v", err)
	}
	got := graphSnapshot(t, ctx, g, id)
	if got.State != Failed || got.Error == "" || cleaned.Load() != 1 || probe.calls.Load() == 0 || len(got.Effects) != 0 {
		t.Fatalf("rollback panic escaped off-owner boundary or registered stale effect: %+v", got)
	}
}

func TestGraphLockEffectCleanupDeadlineRecordsWithoutFakingReturn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := graphTestContext(t)
		g := graphTestRuntime(t).(*GraphLock)
		g.knobs.cleanup = time.Second
		contexts := make(chan *Context, 1)
		id := graphMount(t, ctx, g, Spec{ID: "blocked-effect", Apply: func(c *Context) error { contexts <- c; return nil }})
		graphSettle(t, ctx, g)
		owner := graphReceive(t, ctx, contexts)
		setupErr := errors.New("partial setup")
		started, release := make(chan context.Context, 1), make(chan struct{})
		var once sync.Once
		t.Cleanup(func() { once.Do(func() { close(release) }) })
		result := make(chan error, 1)
		go func() {
			result <- owner.Effect("partial", func() (Disposer, error) {
				return func(cleanup context.Context) error { started <- cleanup; <-release; return nil }, setupErr
			})
		}()
		cleanup := graphReceive(t, ctx, started)
		graphReceive(t, ctx, cleanup.Done())
		synctest.Wait()
		if got := graphSnapshot(t, ctx, g, id); got.State != Active || got.Error == "" || len(got.Effects) != 0 || owner.lease.Load() != 1 {
			t.Fatalf("deadline hid actual blocked setup cleanup or changed failure policy: %+v", got)
		}
		select {
		case err := <-result:
			t.Fatalf("cleanup deadline pretended disposer returned: %v", err)
		default:
		}
		once.Do(func() { close(release) })
		if err := graphReceive(t, ctx, result); !errors.Is(err, setupErr) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("rollback did not retain setup/deadline errors: %v", err)
		}
		if owner.lease.Load() != 0 {
			t.Fatal("real rollback completion retained setup lease")
		}
		events, err := g.Events(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var deadlines int
		for _, event := range events {
			if event.Kind == "deadline:cleanupEffect" {
				deadlines++
			}
		}
		if deadlines != 1 {
			t.Fatalf("cleanup deadline recorded %d times", deadlines)
		}
	})
}

func TestGraphLockScopeIncrementalRollbackRetainsNestedOwnership(t *testing.T) {
	for _, panics := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "panic"}[panics], func(t *testing.T) {
			ctx := graphTestContext(t)
			g := graphTestRuntime(t).(*GraphLock)
			failure := errors.New("incremental scope failure")
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			t.Cleanup(func() { once.Do(func() { close(release) }) })
			handles := make(chan *EffectHandle, 1)
			var order []string
			id := graphMount(t, ctx, g, Spec{ID: "scope-rollback", Apply: func(c *Context) error {
				handle, err := c.RequestEffectScope("owned-scope", func(scope *EffectScope) error {
					if _, err := g.Inspect(ctx); err != nil {
						return err
					}
					if err := scope.Defer("first", func(context.Context) error { order = append(order, "first"); return nil }); err != nil {
						return err
					}
					if err := scope.Scope("nested", func(child *EffectScope) error {
						return child.Defer("held-last", func(context.Context) error { close(started); <-release; order = append(order, "last"); return nil })
					}); err != nil {
						return err
					}
					if panics {
						panic(failure)
					}
					return failure
				})
				if err == nil {
					handles <- handle
				}
				return err
			}})
			handle := graphReceive(t, ctx, handles)
			graphReceive(t, ctx, started)
			held := graphSnapshot(t, ctx, g, id)
			if held.State != Loading || len(held.EffectTree) != 1 || held.EffectTree[0].Label != "owned-scope" || held.EffectTree[0].State != ScopeCleaning || len(held.EffectTree[0].Children) != 2 || held.EffectTree[0].Children[1].Children[0].State != ScopeCleaning {
				t.Fatalf("rollback removed actual nested resources before return: %+v", held)
			}
			held.EffectTree[0].Children[0].Label = "reader mutation"
			if got := graphSnapshot(t, ctx, g, id); got.EffectTree[0].Children[0].Label != "first" {
				t.Fatal("scope metadata returned shared mutable storage")
			}
			once.Do(func() { close(release) })
			if err := handle.AwaitSetup(ctx); !errors.Is(err, failure) {
				t.Fatalf("scope setup lost actual failure identity: %v", err)
			}
			graphSettle(t, ctx, g)
			got := graphSnapshot(t, ctx, g, id)
			if got.State != Active || len(got.EffectTree) != 0 || len(got.Effects) != 0 || !reflect.DeepEqual(order, []string{"last", "first"}) {
				t.Fatalf("handle-only rollback poisoned activation or lost LIFO: %+v order %v", got, order)
			}
			if panics {
				var panicErr *ScopePanicError
				if err := handle.AwaitSetup(ctx); !errors.As(err, &panicErr) {
					t.Fatalf("panic not preserved as typed scope error: %v", err)
				}
			}
		})
	}
}

func TestGraphLockScopeWithdrawalsShareApplyLaneAndExactIdentity(t *testing.T) {
	ctx := graphTestContext(t)
	g := graphTestRuntime(t).(*GraphLock)
	type pair struct{ first, second *EffectHandle }
	handles := make(chan pair, 1)
	var running atomic.Int64
	var order []string
	id := graphMount(t, ctx, g, Spec{ID: "scope-lane", Apply: func(c *Context) error {
		if running.Add(1) != 1 {
			return errors.New("Apply lane already busy")
		}
		defer running.Add(-1)
		makeScope := func(name string) (*EffectHandle, error) {
			return c.RequestEffectScope("same-label", func(scope *EffectScope) error {
				if running.Add(1) != 1 {
					return errors.New("scope setup overlapped Apply or another scope")
				}
				defer running.Add(-1)
				order = append(order, "setup-"+name)
				return scope.Defer(name, func(context.Context) error {
					if running.Add(1) != 1 {
						return errors.New("scope cleanup overlapped transition lane")
					}
					defer running.Add(-1)
					order = append(order, "cleanup-"+name)
					return nil
				})
			})
		}
		first, err := makeScope("first")
		if err != nil {
			return err
		}
		second, err := makeScope("second")
		if err != nil {
			return err
		}
		if err := first.RequestWithdraw(); err != nil {
			return err
		}
		if err := first.RequestWithdraw(); err != nil {
			return err
		}
		handles <- pair{first, second}
		return nil
	}})
	h := graphReceive(t, ctx, handles)
	graphSettle(t, ctx, g)
	if err := h.first.Await(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.second.AwaitSetup(ctx); err != nil {
		t.Fatal(err)
	}
	got := graphSnapshot(t, ctx, g, id)
	if got.State != Active || len(got.EffectTree) != 1 || got.EffectTree[0].Children[0].Label != "second" || !reflect.DeepEqual(order, []string{"setup-first", "cleanup-first", "setup-second"}) {
		t.Fatalf("same-label handle affected wrong slot or lane: %+v order %v", got, order)
	}
	if err := h.second.RequestWithdraw(); err != nil {
		t.Fatal(err)
	}
	if err := g.Dispose(ctx, id); err != nil {
		t.Fatal(err)
	}
	graphSettle(t, ctx, g)
	if err := h.second.Await(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.second.RequestWithdraw(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"setup-first", "cleanup-first", "setup-second", "cleanup-second"}) || running.Load() != 0 {
		t.Fatalf("withdraw/unload duplicated or overlapped cleanup: %v", order)
	}
}

func TestGraphLockScopeQueuedCancellationAndHeldCleanupAreReal(t *testing.T) {
	t.Run("never-started", func(t *testing.T) {
		ctx := graphTestContext(t)
		g := graphTestRuntime(t).(*GraphLock)
		handles, requested, release := make(chan *EffectHandle, 1), make(chan struct{}), make(chan struct{})
		var once sync.Once
		t.Cleanup(func() { once.Do(func() { close(release) }) })
		var calls atomic.Int64
		id := graphMount(t, ctx, g, Spec{ID: "cancel-queued", Apply: func(c *Context) error {
			h, err := c.RequestEffectScope("queued", func(*EffectScope) error { calls.Add(1); return nil })
			if err != nil {
				return err
			}
			handles <- h
			err = c.Dispose()
			close(requested)
			<-release
			return err
		}})
		h := graphReceive(t, ctx, handles)
		graphReceive(t, ctx, requested)
		once.Do(func() { close(release) })
		graphSettle(t, ctx, g)
		if err := h.AwaitSetup(ctx); !errors.Is(err, ErrInactiveEffect) || !errors.Is(err, ErrInactive) {
			t.Fatalf("queued setup cancellation: %v", err)
		}
		if calls.Load() != 0 || graphSnapshot(t, ctx, g, id).State != Disposed {
			t.Fatal("quiesced queued scope ran setup or blocked owner disposal")
		}
	})
	t.Run("held-root-cleanup", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx := graphTestContext(t)
			g := graphTestRuntime(t).(*GraphLock)
			g.knobs.cleanup = time.Second
			root, err := g.RootContext(ctx)
			if err != nil {
				t.Fatal(err)
			}
			started, release := make(chan context.Context, 1), make(chan struct{})
			var once sync.Once
			t.Cleanup(func() { once.Do(func() { close(release) }) })
			h, err := root.RequestEffectScope("held", func(scope *EffectScope) error {
				return scope.Defer("resource", func(cleanup context.Context) error { started <- cleanup; <-release; return nil })
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := h.AwaitSetup(ctx); err != nil {
				t.Fatal(err)
			}
			if err := h.RequestWithdraw(); err != nil {
				t.Fatal(err)
			}
			cleanup := graphReceive(t, ctx, started)
			graphReceive(t, ctx, cleanup.Done())
			synctest.Wait()
			if meta := h.Metadata(); meta.State != ScopeCleaning || len(meta.Children) != 1 {
				t.Fatalf("deadline hid actual held scope resources: %+v", meta)
			}
			select {
			case <-h.effect.done:
				t.Fatal("cleanup deadline falsely completed handle")
			default:
			}
			g.mu.Lock()
			busy, effects := g.root.worker, len(g.root.current.effects)
			g.mu.Unlock()
			if !busy || effects != 1 {
				t.Fatal("held scope lost native lane or ownership slot")
			}
			once.Do(func() { close(release) })
			if err := h.Await(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("native scope budget diagnostic: %v", err)
			}
			graphSettle(t, ctx, g)
		})
	})
}
