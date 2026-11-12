// SPDX-License-Identifier: Apache-2.0

package concurrency

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func rcGuard(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func rcRuntime(t *testing.T) Runtime {
	t.Helper()
	r := NewReconciler()
	t.Cleanup(func() {
		if err := r.Close(rcGuard(t)); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return r
}

func rcMount(t *testing.T, r Runtime, spec Spec) uint64 {
	t.Helper()
	id, err := r.Mount(rcGuard(t), spec)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func rcSettle(t *testing.T, r Runtime) {
	t.Helper()
	if err := r.Settle(rcGuard(t)); err != nil {
		t.Fatal(err)
	}
}

func rcSnapshot(t *testing.T, r Runtime, id uint64) Snapshot {
	t.Helper()
	ss, err := r.Snapshots(rcGuard(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range ss {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("fiber %d missing from snapshots: %+v", id, ss)
	return Snapshot{}
}

func TestReconcilerMissingDependencyAndActiveVisibility(t *testing.T) {
	r := rcRuntime(t)
	entered := make(chan *Context, 1)
	consumer := rcMount(t, r, Spec{ID: "consumer", Needs: []string{"sample"}, Apply: func(c *Context) error {
		entered <- c
		return nil
	}})
	rcSettle(t, r)
	if got := rcSnapshot(t, r, consumer).State; got != Pending {
		t.Fatalf("missing dependency state = %s", got)
	}
	provided, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	provider := rcMount(t, r, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Context) error {
		if err := c.Provide("sample", "value"); err != nil {
			return err
		}
		close(provided)
		<-release
		return nil
	}})
	<-provided
	if got := rcSnapshot(t, r, provider).State; got != Loading {
		t.Fatalf("provider before Apply completion = %s", got)
	}
	if got := rcSnapshot(t, r, consumer).State; got != Pending {
		t.Fatalf("LOADING provision leaked: consumer = %s", got)
	}
	select {
	case <-entered:
		t.Fatal("consumer ran before provider ACTIVE")
	default:
	}
	close(release)
	rcSettle(t, r)
	c := <-entered
	if value, err := c.Read("sample"); err != nil || value != "value" {
		t.Fatalf("bound Read = %v, %v", value, err)
	}
	if got := rcSnapshot(t, r, consumer); got.State != Active || len(got.Epoch) != 1 || got.Epoch[0] == 0 {
		t.Fatalf("consumer snapshot = %+v", got)
	}
}

func TestReconcilerRetainsProviderThroughConsumerCleanup(t *testing.T) {
	r := rcRuntime(t)
	var providerDisposed atomic.Bool
	provider := rcMount(t, r, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Context) error {
		if err := c.Defer("release-resource", func(context.Context) error { providerDisposed.Store(true); return nil }); err != nil {
			return err
		}
		return c.Provide("sample", &providerDisposed)
	}})
	rcSettle(t, r)
	cleanupEntered, releaseCleanup := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-releaseCleanup:
		default:
			close(releaseCleanup)
		}
	}()
	consumerCtx := make(chan *Context, 1)
	consumer := rcMount(t, r, Spec{ID: "consumer", Needs: []string{"sample"}, Apply: func(c *Context) error {
		consumerCtx <- c
		return c.Defer("consumer", func(context.Context) error {
			value, err := c.Read("sample")
			if err != nil {
				return err
			}
			if value.(*atomic.Bool).Load() {
				return errors.New("provider released before consumer cleanup")
			}
			close(cleanupEntered)
			<-releaseCleanup
			return nil
		})
	}})
	rcSettle(t, r)
	c := <-consumerCtx
	if err := r.Dispose(rcGuard(t), provider); err != nil {
		t.Fatal(err)
	}
	<-cleanupEntered
	if providerDisposed.Load() {
		t.Fatal("provider disposer ran before consumer cleanup completed")
	}
	if c.Enter() {
		c.Leave()
		t.Fatal("quiescent consumer accepted a lease")
	}
	if c.Std().Err() != context.Canceled {
		t.Fatalf("Std error = %v", c.Std().Err())
	}
	if got := rcSnapshot(t, r, consumer).State; got != Unloading {
		t.Fatalf("consumer state = %s", got)
	}
	close(releaseCleanup)
	rcSettle(t, r)
	if !providerDisposed.Load() {
		t.Fatal("provider resource not disposed")
	}
	if got := rcSnapshot(t, r, consumer).State; got != Pending {
		t.Fatalf("consumer after provider loss = %s", got)
	}
	if _, err := c.Read("sample"); !errors.Is(err, ErrInactive) {
		t.Fatalf("released binding Read = %v", err)
	}
}

func TestReconcilerEpochRevisionAndProviderUID(t *testing.T) {
	r := rcRuntime(t)
	var loads atomic.Int32
	provider := rcMount(t, r, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Context) error {
		return c.Provide("sample", loads.Add(1))
	}})
	consumer := rcMount(t, r, Spec{ID: "consumer", Needs: []string{"sample"}})
	rcSettle(t, r)
	before := rcSnapshot(t, r, consumer)
	if err := r.Update(rcGuard(t), provider); err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
	after := rcSnapshot(t, r, consumer)
	if before.Revision != after.Revision || before.Epoch[0] == after.Epoch[0] {
		t.Fatalf("provider restart epochs = %v -> %v", before.Epoch, after.Epoch)
	}
	if err := r.Update(rcGuard(t), consumer); err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
	updated := rcSnapshot(t, r, consumer)
	if updated.Revision != after.Revision+1 || updated.Epoch[0] != after.Epoch[0] {
		t.Fatalf("consumer update epoch = %+v after %+v", updated, after)
	}
}

func TestReconcilerStickyFailureRequiresExplicitUpdate(t *testing.T) {
	r := rcRuntime(t)
	provider := rcMount(t, r, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Context) error { return c.Provide("sample", 1) }})
	var attempts atomic.Int32
	id := rcMount(t, r, Spec{ID: "failing", Needs: []string{"sample"}, Apply: func(c *Context) error {
		if attempts.Add(1) == 1 {
			return errors.New("sticky failure")
		}
		return nil
	}})
	if err := r.Settle(rcGuard(t)); err == nil {
		t.Fatal("failed Apply did not report error")
	}
	if got := rcSnapshot(t, r, id).State; got != Failed {
		t.Fatalf("failed state = %s", got)
	}
	if err := r.Update(rcGuard(t), provider); err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(rcGuard(t)); err == nil {
		t.Fatal("provider restart cleared sticky failure")
	}
	if attempts.Load() != 1 {
		t.Fatalf("failure retried %d times", attempts.Load())
	}
	if err := r.Update(rcGuard(t), id); err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
	if got := rcSnapshot(t, r, id).State; got != Active {
		t.Fatalf("updated failure state = %s", got)
	}
}

func TestReconcilerSupersededLoadErrorAndCoalescing(t *testing.T) {
	r := rcRuntime(t)
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var attempts atomic.Int32
	id := rcMount(t, r, Spec{ID: "superseded", Apply: func(c *Context) error {
		if attempts.Add(1) == 1 {
			close(entered)
			<-release
			return errors.New("obsolete error")
		}
		return nil
	}})
	<-entered
	for range 32 {
		if err := r.Update(rcGuard(t), id); err != nil {
			t.Fatal(err)
		}
	}
	close(release)
	rcSettle(t, r)
	if attempts.Load() != 2 {
		t.Fatalf("coalesced attempts = %d, want 2", attempts.Load())
	}
	if s := rcSnapshot(t, r, id); s.State != Active || s.Revision != 32 || s.Error != "" {
		t.Fatalf("new target poisoned by stale completion: %+v", s)
	}
}

func TestReconcilerSelfDisposeDoesNotAwaitOwnApply(t *testing.T) {
	r := rcRuntime(t)
	var cleaned atomic.Bool
	requested := make(chan error, 1)
	id := rcMount(t, r, Spec{ID: "self-dispose", Apply: func(c *Context) error {
		if err := c.Defer("partial", func(context.Context) error { cleaned.Store(true); return nil }); err != nil {
			return err
		}
		requested <- c.Dispose()
		return nil
	}})
	if err := <-requested; err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
	if !cleaned.Load() {
		t.Fatal("self-disposal did not unwind registered effects")
	}
	events, err := r.Events(rcGuard(t))
	if err != nil {
		t.Fatal(err)
	}
	var disposed bool
	for _, e := range events {
		if e.Fiber == id && e.State == Active {
			t.Fatal("self-disposed load published ACTIVE")
		}
		if e.Fiber == id && e.Kind == "state" && e.State == Disposed {
			disposed = true
		}
	}
	if !disposed {
		t.Fatal("self-disposal did not reach DISPOSED")
	}
}

func TestReconcilerSequentialLIFOPartialFailure(t *testing.T) {
	r := rcRuntime(t)
	secondEntered, releaseSecond := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-releaseSecond:
		default:
			close(releaseSecond)
		}
	}()
	var mu sync.Mutex
	var order []string
	appendOrder := func(s string) { mu.Lock(); defer mu.Unlock(); order = append(order, s) }
	id := rcMount(t, r, Spec{ID: "partial", Apply: func(c *Context) error {
		if err := c.Defer("first", func(context.Context) error { appendOrder("first"); return nil }); err != nil {
			return err
		}
		if err := c.Defer("second", func(context.Context) error {
			appendOrder("second-start")
			close(secondEntered)
			<-releaseSecond
			appendOrder("second-end")
			return nil
		}); err != nil {
			return err
		}
		return errors.New("setup failed after two effects")
	}})
	<-secondEntered
	mu.Lock()
	got := append([]string(nil), order...)
	mu.Unlock()
	if !reflect.DeepEqual(got, []string{"second-start"}) {
		t.Fatalf("concurrent/out-of-order cleanup = %v", got)
	}
	if s := rcSnapshot(t, r, id); s.State != Unloading {
		t.Fatalf("partial cleanup state = %s", s.State)
	}
	close(releaseSecond)
	if err := r.Settle(rcGuard(t)); err == nil {
		t.Fatal("partial setup error lost")
	}
	mu.Lock()
	got = append([]string(nil), order...)
	mu.Unlock()
	if !reflect.DeepEqual(got, []string{"second-start", "second-end", "first"}) {
		t.Fatalf("cleanup order = %v", got)
	}
	if err := r.Dispose(rcGuard(t), id); err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
}

func TestReconcilerDeclaredDuplicateAndLateEffects(t *testing.T) {
	r := rcRuntime(t)
	ctxs := make(chan *Context, 2)
	id := rcMount(t, r, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Context) error {
		ctxs <- c
		if err := c.Provide("undeclared", nil); err == nil {
			return errors.New("undeclared provision accepted")
		}
		if err := c.Provide("sample", "value"); err != nil {
			return err
		}
		if err := c.Provide("sample", "duplicate"); err == nil {
			return errors.New("duplicate provision accepted")
		}
		return nil
	}})
	rcSettle(t, r)
	old := <-ctxs
	if err := r.Update(rcGuard(t), id); err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
	<-ctxs
	if err := old.Defer("late", func(context.Context) error { return nil }); !errors.Is(err, ErrInactive) {
		t.Fatalf("old Defer = %v", err)
	}
	if err := old.Provide("sample", "late"); !errors.Is(err, ErrInactive) {
		t.Fatalf("old Provide = %v", err)
	}
	if old.Enter() {
		old.Leave()
		t.Fatal("old activation accepted a lease")
	}
}

func TestReconcilerLeaseDrainAndSnapshotIsolation(t *testing.T) {
	r := rcRuntime(t)
	ctxs := make(chan *Context, 2)
	var disposed atomic.Bool
	id := rcMount(t, r, Spec{ID: "leased", Apply: func(c *Context) error {
		ctxs <- c
		return c.Defer("resource", func(context.Context) error { disposed.Store(true); return nil })
	}})
	rcSettle(t, r)
	c := <-ctxs
	if !c.Enter() {
		t.Fatal("active lease refused")
	}
	if err := r.Update(rcGuard(t), id); err != nil {
		t.Fatal(err)
	}
	if c.Enter() {
		c.Leave()
		t.Fatal("new lease accepted after quiesce")
	}
	if disposed.Load() {
		t.Fatal("resource disposed with an outstanding lease")
	}
	if s := rcSnapshot(t, r, id); s.State != Unloading {
		t.Fatalf("leased state = %s", s.State)
	}
	c.Leave()
	rcSettle(t, r)
	<-ctxs
	if !disposed.Load() {
		t.Fatal("lease release did not wake cleanup")
	}
	ss, err := r.Snapshots(rcGuard(t))
	if err != nil {
		t.Fatal(err)
	}
	ss[0].State = Failed
	ss[0].Effects[0] = "corrupt"
	if s := rcSnapshot(t, r, id); s.State != Active || s.Effects[0] == "corrupt" {
		t.Fatalf("mutable snapshot escaped: %+v", s)
	}
	events, err := r.Events(rcGuard(t))
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range events {
		if e.Sequence != uint64(i+1) {
			t.Fatalf("event %d sequence = %d", i, e.Sequence)
		}
	}
	events[0].Sequence = 999
	fresh, err := r.Events(rcGuard(t))
	if err != nil {
		t.Fatal(err)
	}
	if fresh[0].Sequence != 1 {
		t.Fatal("mutable event slice escaped")
	}
}

func TestReconcilerChainDiamondInvalidationAndNoIdleWorkers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewReconciler()
		contexts := make(map[string]chan *Context)
		var ids []uint64
		for _, spec := range []Spec{
			{ID: "root", Provides: []string{"root"}},
			{ID: "left", Needs: []string{"root"}, Provides: []string{"left"}},
			{ID: "right", Needs: []string{"root"}, Provides: []string{"right"}},
			{ID: "leaf", Needs: []string{"left", "right"}, Provides: []string{"leaf"}},
			{ID: "tail", Needs: []string{"leaf"}},
		} {
			captured := make(chan *Context, 2)
			contexts[spec.ID] = captured
			provides := slices.Clone(spec.Provides)
			spec.Apply = func(c *Context) error {
				captured <- c
				for _, key := range provides {
					if err := c.Provide(key, c); err != nil {
						return err
					}
				}
				return nil
			}
			ids = append(ids, rcMount(t, r, spec))
		}
		rcSettle(t, r)
		old := make([]*Context, 0, len(ids))
		for _, name := range []string{"root", "left", "right", "leaf", "tail"} {
			old = append(old, <-contexts[name])
		}
		if err := r.Update(rcGuard(t), ids[0]); err != nil {
			t.Fatal(err)
		}
		rcSettle(t, r)
		for _, c := range old {
			if !c.released.Load() || c.Enter() {
				t.Fatal("superseded activation still usable")
			}
		}
		for _, id := range ids {
			if got := rcSnapshot(t, r, id).State; got != Active {
				t.Fatalf("fiber %d state = %s", id, got)
			}
		}
		synctest.Wait()
		// All transition goroutines have returned: the only candidate goroutine
		// left inside this isolated bubble is its global inbox owner.
		stacks := make([]byte, 1<<20)
		n := runtime.Stack(stacks, true)
		if got := bytes.Count(stacks[:n], []byte(".(*rcOwner).run(")); got != 1 {
			t.Fatalf("global reconciler owner stack count = %d", got)
		}
		if bytes.Contains(stacks[:n], []byte(".(*rcOwner).load.func")) || bytes.Contains(stacks[:n], []byte(".(*rcOwner).disposeNext.func")) {
			t.Fatal("idle activation retains a transition goroutine")
		}
		if err := r.Close(rcGuard(t)); err != nil {
			t.Fatal(err)
		}
	})
}

func TestReconcilerConcurrentLeaseQuiesceAndRequestWake(t *testing.T) {
	r := rcRuntime(t)
	contexts := make(chan *Context, 2)
	id := rcMount(t, r, Spec{ID: "race", Apply: func(c *Context) error {
		contexts <- c
		return c.Defer("cleanup", func(context.Context) error { return nil })
	}})
	rcSettle(t, r)
	c := <-contexts
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range 32 {
		workers.Go(func() {
			<-start
			for range 1000 {
				if c.Enter() {
					c.Leave()
				}
			}
		})
	}
	close(start)
	if err := r.Update(rcGuard(t), id); err != nil {
		t.Fatal(err)
	}
	workers.Wait()
	rcSettle(t, r)
	fresh := <-contexts
	if c.lease.Load() != 0 || !c.released.Load() {
		t.Fatal("quiescent lease race did not drain")
	}
	for range 32 {
		workers.Go(func() {
			for range 1000 {
				_ = fresh.Dispose()
			}
		})
	}
	workers.Wait()
	rcSettle(t, r)
	if got, err := r.Snapshots(rcGuard(t)); err != nil || len(got) != 1 || got[0].State != Disposed || len(got[0].Effects) != 0 {
		t.Fatalf("coalesced disposal ledger = %+v, %v", got, err)
	}
}

func TestReconcilerOwnerAvailableDuringReentrantCallbacks(t *testing.T) {
	r := rcRuntime(t)
	child := make(chan uint64, 1)
	id := rcMount(t, r, Spec{ID: "outer", Apply: func(c *Context) error {
		id, err := r.Mount(c.Std(), Spec{ID: "child"})
		if err != nil {
			return err
		}
		child <- id
		if _, err := r.Snapshots(c.Std()); err != nil {
			return err
		}
		return c.Defer("reentrant", func(ctx context.Context) error {
			if _, err := r.Events(ctx); err != nil {
				return err
			}
			return r.Update(ctx, id)
		})
	}})
	rcSettle(t, r)
	childID := <-child
	if err := r.Dispose(rcGuard(t), id); err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
	if s := rcSnapshot(t, r, childID); s.State != Active || s.Revision != 1 {
		t.Fatalf("reentrant update = %+v", s)
	}
}

func TestReconcilerCanceledWaitersAndClosedCommands(t *testing.T) {
	r := NewReconciler()
	entered, release := make(chan struct{}), make(chan struct{})
	id := rcMount(t, r, Spec{ID: "blocked", Apply: func(*Context) error { close(entered); <-release; return nil }})
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.Settle(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Settle = %v", err)
	}
	if err := r.Dispose(ctx, id); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Dispose = %v", err)
	}
	if s := rcSnapshot(t, r, id); s.State != Loading {
		t.Fatalf("canceled command mutated state: %+v", s)
	}
	close(release)
	rcSettle(t, r)
	if err := r.Close(rcGuard(t)); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(rcGuard(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Mount(rcGuard(t), Spec{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Mount after Close = %v", err)
	}
}

func TestReconcilerAwaitRetainsFailedErrorAfterDisposal(t *testing.T) {
	r := rcRuntime(t)
	startupErr := errors.New("retained startup failure")
	id := rcMount(t, r, Spec{ID: "failed", Apply: func(*Context) error { return startupErr }})
	if err := r.(Waiter).AwaitFiber(rcGuard(t), id); !errors.Is(err, startupErr) {
		t.Fatalf("initial AwaitFiber lost error identity: %v", err)
	}
	if err := r.Dispose(rcGuard(t), id); err != nil {
		t.Fatalf("Dispose request surfaced startup error: %v", err)
	}
	if err := r.(Waiter).AwaitFiber(rcGuard(t), id); !errors.Is(err, startupErr) {
		t.Fatalf("disposed AwaitFiber lost error identity: %v", err)
	}
	if s := rcSnapshot(t, r, id); s.State != Disposed || s.Error != startupErr.Error() || len(s.Effects) != 0 {
		t.Fatalf("failed disposed metadata = %+v", s)
	}
	if err := r.Dispose(rcGuard(t), id); err != nil {
		t.Fatalf("repeat Dispose surfaced retained error: %v", err)
	}
	if err := r.(Waiter).AwaitFiber(rcGuard(t), id); !errors.Is(err, startupErr) {
		t.Fatalf("repeat retired AwaitFiber lost error: %v", err)
	}
	// Runtime-wide completion is not the target's retained startup exception.
	rcSettle(t, r)
}

func TestReconcilerCleanupDiagnosticDoesNotBecomeAwaitFailure(t *testing.T) {
	r := NewReconciler()
	cleanupErr := errors.New("cleanup diagnostic")
	id := rcMount(t, r, Spec{ID: "cleanup", Apply: func(c *Context) error { return c.Defer("cleanup", func(context.Context) error { return cleanupErr }) }})
	if err := r.(Waiter).AwaitFiber(rcGuard(t), id); err != nil {
		t.Fatal(err)
	}
	if err := r.Dispose(rcGuard(t), id); err != nil {
		t.Fatal(err)
	}
	if err := r.(Waiter).AwaitFiber(rcGuard(t), id); err != nil {
		t.Fatalf("cleanup diagnostic became startup error: %v", err)
	}
	if got := rcSnapshot(t, r, id); got.State != Disposed || got.Error != cleanupErr.Error() {
		t.Fatalf("cleanup diagnostic not retained: %+v", got)
	}
	if err := r.(Waiter).AwaitFiber(rcGuard(t), id); err != nil {
		t.Fatalf("retired cleanup diagnostic became startup error: %v", err)
	}
	if err := r.Close(rcGuard(t)); !errors.Is(err, cleanupErr) {
		t.Fatalf("runtime lost cleanup diagnostic: %v", err)
	}
}

func TestReconcilerAwaitFiberDoesNotJoinUnrelatedTransitions(t *testing.T) {
	r := rcRuntime(t)
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	unrelated := rcMount(t, r, Spec{ID: "blocked", Apply: func(*Context) error { close(entered); <-release; return nil }})
	<-entered
	target := rcMount(t, r, Spec{ID: "target"})
	if err := r.(Waiter).AwaitFiber(rcGuard(t), target); err != nil {
		t.Fatal(err)
	}
	if s := rcSnapshot(t, r, unrelated); s.State != Loading {
		t.Fatalf("unrelated state = %s", s.State)
	}
	if err := r.Dispose(rcGuard(t), target); err != nil {
		t.Fatal(err)
	}
	if err := r.(Waiter).AwaitFiber(rcGuard(t), target); err != nil {
		t.Fatal(err)
	}
	if s := rcSnapshot(t, r, target); s.State != Disposed || len(s.Effects) != 0 {
		t.Fatalf("retired snapshot = %+v", s)
	}
	if err := r.(Waiter).AwaitFiber(rcGuard(t), target+100); !errors.Is(err, ErrUnknown) {
		t.Fatalf("unknown AwaitFiber = %v", err)
	}
	close(release)
	rcSettle(t, r)
}

func TestReconcilerRecordsImmutableFixtureEnvelopesAndEventMetadata(t *testing.T) {
	r := rcRuntime(t)
	recorder := r.(Recorder)
	data := map[string]any{"fiber": "sample", "large": json.Number("9007199254740993"), "nested": map[string]any{"values": []any{"initial"}}}
	if err := recorder.Record(rcGuard(t), "before", data); err != nil {
		t.Fatal(err)
	}
	data["nested"].(map[string]any)["values"].([]any)[0] = "corrupt"
	id := rcMount(t, r, Spec{ID: "sample", Provides: []string{"sample"}, Apply: func(c *Context) error {
		if err := recorder.Record(c.Std(), "apply", map[string]any{"fiber": "sample"}); err != nil {
			return err
		}
		return c.Provide("sample", "actual")
	}})
	if err := r.(Waiter).AwaitFiber(rcGuard(t), id); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Record(rcGuard(t), "after", nil); err != nil {
		t.Fatal(err)
	}
	events, err := r.Events(rcGuard(t))
	if err != nil {
		t.Fatal(err)
	}
	var before, apply, active, after uint64
	var published bool
	for _, e := range events {
		if e.Kind == "fixture" {
			switch e.Key {
			case "before":
				before = e.Sequence
				if number, ok := e.Data["large"].(json.Number); !ok || number.String() != "9007199254740993" {
					t.Fatalf("Record lost JSON integer precision: %v", e.Data["large"])
				}
				values := e.Data["nested"].(map[string]any)["values"].([]any)
				if values[0] != "initial" {
					t.Fatalf("Record retained caller map: %v", values)
				}
				values[0] = "corrupt-returned-events"
			case "apply":
				apply = e.Sequence
			case "after":
				after = e.Sequence
			}
		}
		if e.Fiber == id {
			if e.Name != "sample" {
				t.Fatalf("missing event name: %+v", e)
			}
			if e.Kind == "state" && e.State == Active {
				active = e.Sequence
				if e.From != Loading {
					t.Fatalf("ACTIVE From = %s", e.From)
				}
			}
			if e.Kind == "service" && e.State == Active && e.Value == "actual" {
				published = true
			}
		}
	}
	if !(before < apply && apply < active && active < after) || !published {
		t.Fatalf("fixture/state order = %d/%d/%d/%d, service=%v", before, apply, active, after, published)
	}
	fresh, err := r.Events(rcGuard(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := fresh[0].Data["nested"].(map[string]any)["values"].([]any)[0]; got != "initial" {
		t.Fatalf("Events exposed archive map: %v", got)
	}
	if err := recorder.Record(rcGuard(t), "invalid", map[string]any{"function": func() {}}); err == nil {
		t.Fatal("Record accepted non-JSON envelope")
	}
	if err := r.Dispose(rcGuard(t), id); err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
	ss, err := r.Snapshots(rcGuard(t))
	if err != nil {
		t.Fatal(err)
	}
	ss[0].Name = "corrupt"
	if got := rcSnapshot(t, r, id); got.Name != "sample" || got.State != Disposed || got.Effects != nil {
		t.Fatalf("retired snapshot not immutable: %+v", got)
	}
}

func TestReconcilerConsumerStatePrecedesProviderServiceNotice(t *testing.T) {
	r := rcRuntime(t)
	consumer := rcMount(t, r, Spec{ID: "consumer", Needs: []string{"sample"}})
	provider := rcMount(t, r, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Context) error { return c.Provide("sample", "value") }})
	rcSettle(t, r)
	events, err := r.Events(rcGuard(t))
	if err != nil {
		t.Fatal(err)
	}
	var loading, notice uint64
	for _, e := range events {
		if e.Fiber == consumer && e.Kind == "state" && e.State == Loading {
			loading = e.Sequence
		}
		if e.Fiber == provider && e.Kind == "service" && e.State == Active {
			notice = e.Sequence
		}
	}
	if loading == 0 || notice == 0 || loading >= notice {
		t.Fatalf("consumer LOADING=%d must precede service notice=%d", loading, notice)
	}
}

func TestReconcilerConsumerBoundAvailabilityNotify(t *testing.T) {
	r := rcRuntime(t)
	rcMount(t, r, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Context) error { return c.Provide("sample", "value") }})
	var allowed atomic.Bool
	var checks, applies atomic.Int32
	first := rcMount(t, r, Spec{ID: "allowed", Needs: []string{"sample"}, Check: func(c *Context) bool {
		v, err := c.Read("sample")
		return err == nil && v == "value"
	}})
	second := rcMount(t, r, Spec{ID: "gated", Needs: []string{"sample"}, Check: func(c *Context) bool {
		checks.Add(1)
		if _, err := r.Snapshots(c.Std()); err != nil {
			return false
		}
		if c.Enter() {
			c.Leave()
			panic("predicate accepted lease")
		}
		if err := c.Defer("forbidden", func(context.Context) error { return nil }); !errors.Is(err, ErrInactive) {
			panic("predicate accepted effect")
		}
		if err := c.Dispose(); !errors.Is(err, ErrInactive) {
			panic("predicate accepted disposal")
		}
		return allowed.Load()
	}, Apply: func(*Context) error { applies.Add(1); return nil }})
	rcSettle(t, r)
	if s := rcSnapshot(t, r, first); s.State != Active {
		t.Fatalf("independent consumer = %+v", s)
	}
	if s := rcSnapshot(t, r, second); s.State != Pending {
		t.Fatalf("unavailable consumer = %+v", s)
	}
	if applies.Load() != 0 || checks.Load() != 1 {
		t.Fatalf("false predicate retried: checks=%d Apply=%d", checks.Load(), applies.Load())
	}
	allowed.Store(true)
	if err := r.(Notifier).Notify(rcGuard(t), "unrelated"); err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
	if applies.Load() != 0 {
		t.Fatal("unrelated key retried predicate")
	}
	if err := r.(Notifier).Notify(rcGuard(t), "sample"); err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
	if s := rcSnapshot(t, r, second); s.State != Active {
		t.Fatalf("Notify did not activate: %+v", s)
	}
	if err := r.(Notifier).Notify(rcGuard(t), "sample"); err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
	if applies.Load() != 1 {
		t.Fatal("unchanged successful predicate restarted activation")
	}
	allowed.Store(false)
	if err := r.(Notifier).Notify(rcGuard(t), "sample"); err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
	if s := rcSnapshot(t, r, second); s.State != Pending {
		t.Fatalf("availability loss did not unload: %+v", s)
	}
	if applies.Load() != 1 {
		t.Fatal("availability loss retried Apply")
	}
}

func TestReconcilerStalePredicateRetainsProviderAndDiscardsResult(t *testing.T) {
	r := rcRuntime(t)
	var released atomic.Bool
	provider := rcMount(t, r, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Context) error {
		if err := c.Defer("resource", func(context.Context) error { released.Store(true); return nil }); err != nil {
			return err
		}
		return c.Provide("sample", &released)
	}})
	rcSettle(t, r)
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var applies atomic.Int32
	consumer := rcMount(t, r, Spec{ID: "consumer", Needs: []string{"sample"}, Check: func(c *Context) bool {
		close(entered)
		<-release
		value, err := c.Read("sample")
		return err == nil && !value.(*atomic.Bool).Load()
	}, Apply: func(*Context) error { applies.Add(1); return nil }})
	<-entered
	if err := r.Dispose(rcGuard(t), provider); err != nil {
		t.Fatal(err)
	}
	if released.Load() {
		t.Fatal("provider released while availability reader retained its binding")
	}
	close(release)
	rcSettle(t, r)
	if !released.Load() {
		t.Fatal("provider not released after availability reader completed")
	}
	if s := rcSnapshot(t, r, consumer); s.State != Pending || applies.Load() != 0 {
		t.Fatalf("stale predicate activated: %+v attempts=%d", s, applies.Load())
	}
}

func TestReconcilerPredicatePanicIsPendingNotStickyFailure(t *testing.T) {
	r := rcRuntime(t)
	var panics atomic.Bool
	panics.Store(true)
	id := rcMount(t, r, Spec{ID: "predicate", Check: func(*Context) bool {
		if panics.Load() {
			panic("unavailable")
		}
		return true
	}})
	rcSettle(t, r)
	if s := rcSnapshot(t, r, id); s.State != Pending || s.Error == "" {
		t.Fatalf("predicate panic = %+v", s)
	}
	panics.Store(false)
	if err := r.Update(rcGuard(t), id); err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
	if s := rcSnapshot(t, r, id); s.State != Active || s.Error != "" {
		t.Fatalf("predicate retry = %+v", s)
	}
}

func TestReconcilerDisposeDeadlinePersistsAcrossLaterEffects(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewReconciler()
		firstEntered, firstRelease := make(chan struct{}), make(chan struct{})
		laterEntered, laterRelease := make(chan struct{}), make(chan struct{})
		id := rcMount(t, r, Spec{ID: "cleanup-chain", Apply: func(c *Context) error {
			if err := c.Defer("earlier", func(context.Context) error {
				close(laterEntered)
				<-laterRelease
				return nil
			}); err != nil {
				return err
			}
			return c.Defer("last-registered", func(context.Context) error {
				close(firstEntered)
				<-firstRelease
				return nil
			})
		}})
		rcSettle(t, r)
		if err := r.Dispose(rcGuard(t), id); err != nil {
			t.Fatal(err)
		}
		<-firstEntered
		if err := r.Settle(rcGuard(t)); err == nil {
			t.Fatal("first ignored disposer did not report its deadline")
		}
		close(firstRelease)
		<-laterEntered
		synctest.Wait()
		before := time.Now()
		if err := r.Settle(rcGuard(t)); err == nil {
			t.Fatal("later ignored disposer lost the expired cleanup deadline")
		}
		if time.Since(before) != 0 {
			t.Fatal("later ignored disposer waited for an unrelated caller timeout")
		}
		close(laterRelease)
		synctest.Wait()
		_ = r.Settle(rcGuard(t))
		if err := r.Close(rcGuard(t)); err != nil {
			t.Fatal(err)
		}
	})
}

func TestReconcilerDeadlinesBoundIgnoredCancellation(t *testing.T) {
	for _, stage := range []string{"load", "drain", "dispose"} {
		t.Run(stage, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := NewReconciler()
				contexts := make(chan *Context, 1)
				blocked, release := make(chan struct{}), make(chan struct{})
				id := rcMount(t, r, Spec{ID: "deadline", Apply: func(c *Context) error {
					contexts <- c
					if stage == "load" {
						close(blocked)
						<-release
					}
					return c.Defer("cleanup", func(context.Context) error {
						if stage == "dispose" {
							close(blocked)
							<-release
						}
						return nil
					})
				}})
				c := <-contexts
				if stage != "load" {
					rcSettle(t, r)
					if stage == "drain" && !c.Enter() {
						t.Fatal("lease refused")
					}
					if err := r.Dispose(rcGuard(t), id); err != nil {
						t.Fatal(err)
					}
				}
				if stage != "drain" {
					<-blocked
				}
				before := time.Now()
				if err := r.Settle(rcGuard(t)); err == nil {
					t.Fatal("ignored cancellation did not report a bounded deadline")
				}
				want := 2 * time.Second
				if stage == "drain" {
					want = 5 * time.Second
				}
				if elapsed := time.Since(before); elapsed != want {
					t.Fatalf("deadline elapsed = %s, want %s", elapsed, want)
				}
				if c.Enter() {
					c.Leave()
					t.Fatal("timed-out activation accepted a lease")
				}
				if stage == "drain" {
					c.Leave()
				} else {
					close(release)
				}
				synctest.Wait()
				// A current load timeout is sticky until explicit Update/disposal.
				if stage == "load" {
					if err := r.Dispose(rcGuard(t), id); err != nil {
						t.Fatal(err)
					}
				}
				_ = r.Settle(rcGuard(t))
				if err := r.Close(rcGuard(t)); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}
