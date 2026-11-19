// SPDX-License-Identifier: Apache-2.0

package concurrency

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// These tests exercise the kernel backend through the spike interfaces only. The
// capabilities the public sdk/kernel API lacks are asserted as loud refusals, so
// an export that closes a gap turns the matching test red and must be mapped.

func kernelFixture(t *testing.T) (context.Context, Runtime) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	r := NewKernel()
	t.Cleanup(func() {
		defer cancel()
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = r.Close(closeCtx) // A deliberately failed fiber may be reported.
	})
	return ctx, r
}

func kernelMount(t *testing.T, ctx context.Context, r Runtime, spec Spec) uint64 {
	t.Helper()
	id, err := r.Mount(ctx, spec)
	if err != nil {
		t.Fatalf("mount %s: %v", spec.ID, err)
	}
	return id
}

func TestKernelMountUpdateDispose(t *testing.T) {
	ctx, r := kernelFixture(t)
	var applies, cleanups atomic.Int64
	id := kernelMount(t, ctx, r, Spec{ID: "a", Apply: func(c *Context) error {
		applies.Add(1)
		return c.Defer("cleanup", func(context.Context) error { cleanups.Add(1); return nil })
	}})
	if err := r.(Waiter).AwaitFiber(ctx, id); err != nil || applies.Load() != 1 {
		t.Fatalf("await = %v, applies = %d", err, applies.Load())
	}
	if err := r.Update(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := r.(Waiter).AwaitFiber(ctx, id); err != nil || applies.Load() != 2 || cleanups.Load() != 1 {
		t.Fatalf("after update: await = %v, applies = %d, cleanups = %d", err, applies.Load(), cleanups.Load())
	}
	if err := r.Dispose(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(ctx); err != nil || cleanups.Load() != 2 {
		t.Fatalf("settle = %v, cleanups = %d", err, cleanups.Load())
	}
	if err := r.Dispose(ctx, id); err != nil {
		t.Fatalf("second dispose must be idempotent: %v", err)
	}
	if err := r.Dispose(ctx, 0); !errors.Is(err, ErrUnknown) {
		t.Fatalf("dispose 0 = %v, want ErrUnknown", err)
	}
	if err := r.Update(ctx, 999); !errors.Is(err, ErrUnknown) {
		t.Fatalf("update of an unissued ID = %v, want ErrUnknown", err)
	}
}

func TestKernelProvideNeedAvailability(t *testing.T) {
	ctx, r := kernelFixture(t)
	got := make(chan any, 1)
	consumer := kernelMount(t, ctx, r, Spec{ID: "consumer", Needs: []string{"svc"}, Apply: func(c *Context) error {
		v, err := c.Read("svc")
		if err != nil {
			return err
		}
		got <- v
		if _, err := c.Read("other"); !errors.Is(err, ErrUnknown) {
			t.Errorf("read of an undeclared key = %v, want ErrUnknown", err)
		}
		return nil
	}})
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case v := <-got:
		t.Fatalf("consumer ran before its provider: %v", v)
	default:
	}
	provider := kernelMount(t, ctx, r, Spec{ID: "provider", Provides: []string{"svc"}, Apply: func(c *Context) error {
		if err := c.Provide("svc", 42); err != nil {
			return err
		}
		if !c.Owns("svc") {
			t.Error("Owns is false after a successful Provide")
		}
		if err := c.Provide("undeclared", 1); err == nil {
			t.Error("provide of an undeclared key succeeded")
		}
		return nil
	}})
	if err := r.(Waiter).AwaitFiber(ctx, consumer); err != nil {
		t.Fatal(err)
	}
	if v := <-got; v != 42 {
		t.Fatalf("consumer read %v, want 42", v)
	}
	if err := r.Dispose(ctx, provider); err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestKernelReadAfterUnloadIsInactive(t *testing.T) {
	ctx, r := kernelFixture(t)
	ctxs := make(chan *Context, 1)
	kernelMount(t, ctx, r, Spec{ID: "p", Provides: []string{"k"}, Apply: func(c *Context) error { return c.Provide("k", 1) }})
	id := kernelMount(t, ctx, r, Spec{ID: "c", Needs: []string{"k"}, Apply: func(c *Context) error { ctxs <- c; return nil }})
	if err := r.(Waiter).AwaitFiber(ctx, id); err != nil {
		t.Fatal(err)
	}
	c := <-ctxs
	if v, err := c.Read("k"); err != nil || v != 1 {
		t.Fatalf("live read = %v, %v", v, err)
	}
	if err := r.Dispose(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Read("k"); !errors.Is(err, ErrInactive) {
		t.Fatalf("read after unload = %v, want ErrInactive", err)
	}
	if c.Enter() {
		t.Fatal("Enter succeeded after unload")
	}
}

func TestKernelLeaseHoldsDrain(t *testing.T) {
	ctx, r := kernelFixture(t)
	ctxs := make(chan *Context, 1)
	cleaned := make(chan struct{})
	id := kernelMount(t, ctx, r, Spec{ID: "leased", Apply: func(c *Context) error {
		ctxs <- c
		return c.Defer("probe", func(context.Context) error { close(cleaned); return nil })
	}})
	if err := r.(Waiter).AwaitFiber(ctx, id); err != nil {
		t.Fatal(err)
	}
	c := <-ctxs
	if !c.Enter() {
		t.Fatal("Enter refused on an active fiber")
	}
	if err := r.Dispose(ctx, id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cleaned:
		t.Fatal("cleanup ran while a lease was open")
	case <-time.After(100 * time.Millisecond):
	}
	if c.Enter() {
		t.Fatal("a new lease was granted while unloading")
	}
	c.Leave()
	select {
	case <-cleaned:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup did not run after the lease was released")
	}
}

func TestKernelSelfDisposeIsAGap(t *testing.T) {
	ctx, r := kernelFixture(t)
	errs := make(chan error, 1)
	id := kernelMount(t, ctx, r, Spec{ID: "self", Apply: func(c *Context) error { errs <- c.Dispose(); return nil }})
	if err := r.(Waiter).AwaitFiber(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := <-errs; !errors.Is(err, ErrKernelUnsupported) {
		t.Fatalf("self-dispose = %v, want ErrKernelUnsupported", err)
	}
}

func TestKernelRootContext(t *testing.T) {
	ctx, r := kernelFixture(t)
	root, err := r.(RootProvider).RootContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Dispose(); !errors.Is(err, ErrRootDispose) {
		t.Fatalf("root dispose = %v, want ErrRootDispose", err)
	}
	id := kernelMount(t, ctx, r, Spec{ID: "c", Needs: []string{"launch"}})
	if err := root.Provide("launch", "v"); err != nil {
		t.Fatal(err)
	}
	if err := r.(Waiter).AwaitFiber(ctx, id); err != nil {
		t.Fatal(err)
	}
}

func TestKernelChildMountAndTracked(t *testing.T) {
	ctx, r := kernelFixture(t)
	childRan := make(chan struct{})
	tracked := make(chan struct{})
	parent := kernelMount(t, ctx, r, Spec{ID: "parent", Apply: func(c *Context) error {
		if _, err := c.MountChild(Spec{ID: "child", Apply: func(*Context) error { close(childRan); return nil }}); err != nil {
			return err
		}
		return c.Go("worker", func(std context.Context) error { close(tracked); <-std.Done(); return std.Err() })
	}})
	if err := r.(Waiter).AwaitFiber(ctx, parent); err != nil {
		t.Fatal(err)
	}
	for _, ch := range []chan struct{}{childRan, tracked} {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatal("child or tracked worker did not run")
		}
	}
	if err := r.Dispose(ctx, parent); err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	failing := kernelMount(t, ctx, r, Spec{ID: "failing", Apply: func(c *Context) error {
		return c.Go("bad", func(context.Context) error { return errors.New("worker failed") })
	}})
	// The worker's error fails the fiber, at load or just after ACTIVE.
	deadline := time.Now().Add(5 * time.Second)
	for r.(Waiter).AwaitFiber(ctx, failing) == nil {
		if time.Now().After(deadline) {
			t.Fatal("a failing tracked worker did not fail its fiber")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// ApplyEffect is an approximation (see plan); it must still own the returned
// disposer after the body effects and run it first-registered-last on dispose.
func TestKernelApplyEffectOrdersAfterBodyEffects(t *testing.T) {
	ctx, r := kernelFixture(t)
	var order []string
	var mu sync.Mutex
	note := func(s string) Disposer {
		return func(context.Context) error { mu.Lock(); order = append(order, s); mu.Unlock(); return nil }
	}
	id := kernelMount(t, ctx, r, Spec{ID: "ae", ApplyEffect: func(c *Context) (Disposer, error) {
		if err := c.Defer("body", note("body")); err != nil {
			return nil, err
		}
		return note("returned"), nil
	}})
	if err := r.(Waiter).AwaitFiber(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := r.Dispose(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "returned" || order[1] != "body" {
		t.Fatalf("cleanup order %v, want [returned body]", order)
	}
}

func TestKernelEffectUsesTheLease(t *testing.T) {
	ctx, r := kernelFixture(t)
	disposed := make(chan struct{})
	id := kernelMount(t, ctx, r, Spec{ID: "effect", Apply: func(c *Context) error {
		return c.Effect("res", func() (Disposer, error) {
			return func(context.Context) error { close(disposed); return nil }, nil
		})
	}})
	if err := r.(Waiter).AwaitFiber(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := r.Dispose(ctx, id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-disposed:
	case <-time.After(5 * time.Second):
		t.Fatal("effect disposer did not run on dispose")
	}
}

func TestKernelUpdateConfig(t *testing.T) {
	ctx, r := kernelFixture(t)
	seen := make(chan any, 4)
	id := kernelMount(t, ctx, r, Spec{ID: "cfg", Config: NewConfiguration("one", ConfigHooks{}), Apply: func(c *Context) error {
		seen <- c.Config()
		return nil
	}})
	if err := r.(Waiter).AwaitFiber(ctx, id); err != nil || <-seen != "one" {
		t.Fatalf("first activation: %v", err)
	}
	if err := r.(ConfigUpdater).UpdateConfig(ctx, id, "two", false); err != nil {
		t.Fatal(err)
	}
	if got := <-seen; got != "two" {
		t.Fatalf("restart saw %v, want two", got)
	}
	request, err := r.(ConfigRequester).RequestUpdateConfig(ctx, id, "three", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := request.Await(ctx); err != nil || <-seen != "three" {
		t.Fatalf("requested update: %v", err)
	}
	bare := kernelMount(t, ctx, r, Spec{ID: "bare"})
	if err := r.(ConfigUpdater).UpdateConfig(ctx, bare, "x", false); err == nil {
		t.Fatal("a fiber without a configuration accepted an update")
	}
	if err := r.(ConfigUpdater).UpdateConfig(ctx, 999, "x", false); !errors.Is(err, ErrUnknown) {
		t.Fatalf("update of an unissued ID = %v, want ErrUnknown", err)
	}
}

func TestKernelBatch(t *testing.T) {
	ctx, r := kernelFixture(t)
	var applies atomic.Int64
	apply := func(*Context) error { applies.Add(1); return nil }
	ids, err := r.(Batcher).Batch(ctx, []BatchOperation{
		{Kind: BatchMount, Spec: Spec{ID: "a", Apply: apply}},
		{Kind: BatchMount, Spec: Spec{ID: "b", Needs: []string{"x"}, Apply: apply}},
	})
	if err != nil || len(ids) != 2 || ids[0] == 0 || ids[1] == 0 || ids[0] == ids[1] {
		t.Fatalf("batch mount = %v, %v", ids, err)
	}
	if err := r.(Waiter).AwaitFiber(ctx, ids[0]); err != nil {
		t.Fatal(err)
	}
	out, err := r.(Batcher).Batch(ctx, []BatchOperation{{Kind: BatchUpdate, Fiber: ids[0]}, {Kind: BatchDispose, Fiber: ids[1]}})
	if err != nil || out[0] != 0 || out[1] != 0 {
		t.Fatalf("batch update+dispose = %v, %v", out, err)
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r.(Batcher).Batch(ctx, []BatchOperation{{Kind: BatchUpdate, Fiber: 999}}); !errors.Is(err, ErrUnknown) {
		t.Fatalf("batch of an unknown fiber = %v, want ErrUnknown", err)
	}
	if _, err := r.(Batcher).Batch(ctx, []BatchOperation{{Kind: BatchMount, Spec: Spec{ID: ""}}}); err == nil {
		t.Fatal("a batch mount without an ID was accepted")
	}
}

func TestKernelClose(t *testing.T) {
	ctx, r := kernelFixture(t)
	disposed := make(chan struct{})
	id := kernelMount(t, ctx, r, Spec{ID: "a", Apply: func(c *Context) error {
		return c.Defer("d", func(context.Context) error { close(disposed); return nil })
	}})
	if err := r.(Waiter).AwaitFiber(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-disposed:
	default:
		t.Fatal("Close returned before the fiber was disposed")
	}
	if _, err := r.Mount(ctx, Spec{ID: "late"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("mount after Close = %v, want ErrClosed", err)
	}
	if err := r.Close(ctx); err != nil {
		t.Fatalf("second Close = %v", err)
	}
}

func TestKernelValidationMatchesTheReconciler(t *testing.T) {
	ctx, r := kernelFixture(t)
	for name, spec := range map[string]Spec{
		"both apply forms": {ID: "x", Apply: func(*Context) error { return nil }, ApplyEffect: func(*Context) (Disposer, error) { return nil, nil }},
		"duplicate need":   {ID: "x", Needs: []string{"a", "a"}},
		"empty provide":    {ID: "x", Provides: []string{""}},
	} {
		if _, err := r.Mount(ctx, spec); err == nil {
			t.Errorf("%s: mount accepted", name)
		}
	}
}

// TestKernelGapsFailLoudly lists every capability the public API cannot express.
// Each must refuse with ErrKernelUnsupported; none may succeed vacuously.
func TestKernelGapsFailLoudly(t *testing.T) {
	ctx, r := kernelFixture(t)
	root, err := r.(RootProvider).RootContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	gaps := map[string]error{}
	_, gaps["Snapshots"] = r.Snapshots(ctx)
	_, gaps["Events"] = r.Events(ctx)
	_, gaps["Inspect"] = r.(Inspector).Inspect(ctx)
	gaps["Notify"] = r.(Notifier).Notify(ctx, "k")
	gaps["Record"] = r.(Recorder).Record(ctx, "fixture", nil)
	_, gaps["ObserveEvents"] = r.(ObserverRuntime).ObserveEvents(root, "o", func(context.Context, Event) error { return nil })
	_, gaps["ObserveEventsWith"] = r.(OverflowObserverRuntime).ObserveEventsWith(root, "o", nil, nil)
	gaps["AwaitObservers"] = r.(ObserverRuntime).AwaitObservers(ctx)
	_, gaps["Check"] = r.Mount(ctx, Spec{ID: "check", Check: func(*Context) bool { return true }})
	_, gaps["ConfigHooks"] = r.Mount(ctx, Spec{ID: "hooks", Config: NewConfiguration(1, ConfigHooks{Commit: func(context.Context, any) error { return nil }})})
	_, gaps["EffectScope"] = root.RequestEffectScope("scope", func(*EffectScope) error { return nil })
	for name, err := range gaps {
		want := ErrKernelUnsupported
		if name == "EffectScope" {
			want = ErrScopeUnavailable // The spike Context has no scope bridge: its own loud refusal.
		}
		if !errors.Is(err, want) {
			t.Errorf("%s = %v, want %v", name, err, want)
		}
	}
	if status := r.(ObserverRuntime).ObserverStatus(); status.ObserverErrors == 0 || status.LastObserverError == "" {
		t.Errorf("ObserverStatus reports no gap: %+v", status)
	}
	if _, ok := r.(RetentionReporter); ok {
		t.Error("RetentionReporter implemented: its report cannot carry the gap")
	}
}
