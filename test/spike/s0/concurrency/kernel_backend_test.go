// SPDX-License-Identifier: Apache-2.0

package concurrency

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/test/spike/s0/internal/eventqueue"
)

// These tests exercise the kernel backend through the spike interfaces only. The
// capabilities the public sdk/kernel API still lacks are asserted as loud refusals,
// so an export that closes a gap turns the matching test red and must be mapped.

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

func TestKernelSelfDispose(t *testing.T) {
	ctx, r := kernelFixture(t)
	errs := make(chan error, 1)
	id := kernelMount(t, ctx, r, Spec{ID: "self", Apply: func(c *Context) error { errs <- c.Dispose(); return nil }})
	if err := <-errs; err != nil {
		t.Fatalf("self-dispose from Apply = %v, want an accepted request", err)
	}
	if err := r.(Waiter).AwaitFiber(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	snapshots, err := r.Snapshots(ctx)
	if err != nil || len(snapshots) != 1 || snapshots[0].State != Disposed || snapshots[0].ID != id {
		t.Fatalf("snapshots = %+v, %v; want the fiber DISPOSED", snapshots, err)
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

// TestKernelGapsFailLoudly lists what the public API still cannot express. Each
// must refuse with ErrKernelUnsupported; none may succeed vacuously.
func TestKernelGapsFailLoudly(t *testing.T) {
	ctx, r := kernelFixture(t)
	root, err := r.(RootProvider).RootContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	owned := make(chan *Context, 1)
	id := kernelMount(t, ctx, r, Spec{ID: "holder", Apply: func(c *Context) error { owned <- c; return nil }})
	if err := r.(Waiter).AwaitFiber(ctx, id); err != nil {
		t.Fatal(err)
	}
	fiberCtx := <-owned
	gaps := map[string]error{}
	_, gaps["Check without a need"] = r.Mount(ctx, Spec{ID: "check", Check: func(*Context) bool { return true }})
	_, gaps["fiber-bound observer"] = r.(ObserverRuntime).ObserveEvents(fiberCtx, "o", func(context.Context, Event) error { return nil })
	_, gaps["fiber-bound observer with overflow"] = r.(OverflowObserverRuntime).ObserveEventsWith(fiberCtx, "o", func(context.Context, Event) error { return nil }, nil)
	for name, err := range gaps {
		if !errors.Is(err, ErrKernelUnsupported) {
			t.Errorf("%s = %v, want ErrKernelUnsupported", name, err)
		}
	}
	if _, err := r.(ObserverRuntime).ObserveEvents(root, "root", func(context.Context, Event) error { return nil }); err != nil {
		t.Errorf("a launcher observer is refused: %v", err)
	}
	if gap := UnsupportedGap(ctx, r); gap != "" {
		t.Errorf("UnsupportedGap = %q, want none", gap)
	}
}

// kernelScenario drives the same operations on any backend.
func kernelScenario(t *testing.T, build func() Runtime) ([]Event, []Snapshot, Inspection, ObserverStatus) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := build()
	defer func() { _ = r.Close(ctx) }()
	if _, err := r.(RootProvider).RootContext(ctx); err != nil {
		t.Fatal(err)
	}
	record := r.(Recorder)
	if err := record.Record(ctx, "before", map[string]any{"a": 1}); err != nil {
		t.Fatal(err)
	}
	p := kernelMount(t, ctx, r, Spec{ID: "p", Provides: []string{"k"}, Config: NewConfiguration("raw", ConfigHooks{}), Apply: func(c *Context) error {
		if err := c.Defer("cleanup", func(context.Context) error { return nil }); err != nil {
			return err
		}
		return c.Provide("k", 7)
	}})
	if err := r.(Waiter).AwaitFiber(ctx, p); err != nil {
		t.Fatal(err)
	}
	c := kernelMount(t, ctx, r, Spec{ID: "c", Needs: []string{"k"}})
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	if err := record.Record(ctx, "mid", nil); err != nil {
		t.Fatal(err)
	}
	if err := r.(ConfigUpdater).UpdateConfig(ctx, p, "raw2", false); err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.Dispose(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.(ObserverRuntime).AwaitObservers(ctx); err != nil {
		t.Fatal(err)
	}
	events, err := r.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	snapshots, err := r.Snapshots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := r.(Inspector).Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return events, snapshots, inspection, r.(ObserverRuntime).ObserverStatus()
}

// TestKernelReadBackMatchesTheReconciler runs one scenario on both and requires
// the same ledger, snapshots, provisions and observer counters: the sequence
// starts at 1 and the identities at 1 although the kernel installs built-ins.
func TestKernelReadBackMatchesTheReconciler(t *testing.T) {
	events, snapshots, inspection, status := kernelScenario(t, NewRecordingKernel)
	wantEvents, wantSnapshots, wantInspection, wantStatus := kernelScenario(t, NewRecordingReconciler)
	if len(events) != len(wantEvents) {
		t.Fatalf("%d events, reconciler %d", len(events), len(wantEvents))
	}
	for i := range events {
		if len(events[i].Data) == 0 && len(wantEvents[i].Data) == 0 {
			events[i].Data, wantEvents[i].Data = nil, nil
		}
	}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Errorf("events differ:\n kernel     %+v\n reconciler %+v", events, wantEvents)
	}
	if !reflect.DeepEqual(snapshots, wantSnapshots) {
		t.Errorf("snapshots differ:\n kernel     %+v\n reconciler %+v", snapshots, wantSnapshots)
	}
	if !reflect.DeepEqual(inspection, wantInspection) {
		t.Errorf("inspection differs:\n kernel     %+v\n reconciler %+v", inspection, wantInspection)
	}
	if status.Accepted != wantStatus.Accepted || status.Delivered != wantStatus.Delivered || status.Remaining != 0 {
		t.Errorf("observer status %+v, reconciler %+v", status, wantStatus)
	}
}

func TestKernelRecordOrdersAgainstEventsAndObservers(t *testing.T) {
	ctx, r := kernelFixture(t)
	root, err := r.(RootProvider).RootContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var seen []Event
	if _, err := r.(ObserverRuntime).ObserveEvents(root, "all", func(_ context.Context, e Event) error {
		mu.Lock()
		seen = append(seen, e)
		mu.Unlock()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rec := r.(Recorder)
	for i := range 3 {
		if err := rec.Record(ctx, "step", map[string]any{"i": i}); err != nil {
			t.Fatal(err)
		}
		kernelMount(t, ctx, r, Spec{ID: "f"})
		if err := r.Settle(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.(ObserverRuntime).AwaitObservers(ctx); err != nil {
		t.Fatal(err)
	}
	events, err := r.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	// An observer is registered by an effect, and that event precedes its first.
	if len(seen) != len(events)-1 || seen[0].Sequence != 2 || events[0].Kind != "effect" {
		t.Fatalf("observer saw %d events from %d, ledger has %d", len(seen), seen[0].Sequence, len(events))
	}
	next := 0
	for i, e := range events {
		if e.Sequence != uint64(i)+1 {
			t.Fatalf("event %d has sequence %d", i, e.Sequence)
		}
		if i == 0 {
			continue
		}
		if seen[i-1].Sequence != e.Sequence || seen[i-1].Kind != e.Kind {
			t.Fatalf("event %d: ledger %+v, observer %+v", i, e, seen[i-1])
		}
		if e.Kind == "fixture" {
			if e.Key != "step" || e.Data["i"] != json.Number(strconv.Itoa(next)) || seen[i-1].Data["i"] != e.Data["i"] {
				t.Fatalf("fixture %d: %+v, observer %+v", next, e, seen[i-1])
			}
			if events[i+1].Kind != "mount" {
				t.Fatalf("the mount after record %d is not next: %+v", next, events[i+1])
			}
			next++
		}
	}
	if next != 3 {
		t.Fatalf("found %d records, want 3", next)
	}
}

func TestKernelCheckIsEvaluatedPerConsumerThroughItsProviders(t *testing.T) {
	ctx, r := kernelFixture(t)
	var allow, checks, loads atomic.Int64
	kernelMount(t, ctx, r, Spec{ID: "a", Provides: []string{"a"}, Apply: func(c *Context) error { return c.Provide("a", "va") }})
	kernelMount(t, ctx, r, Spec{ID: "b", Provides: []string{"b"}, Apply: func(c *Context) error { return c.Provide("b", "vb") }})
	id := kernelMount(t, ctx, r, Spec{ID: "checked", Needs: []string{"a", "b"}, Check: func(c *Context) bool {
		checks.Add(1)
		a, errA := c.Read("a")
		b, errB := c.Read("b")
		if c.Enter() || !errors.Is(c.Defer("x", func(context.Context) error { return nil }), ErrInactive) || !errors.Is(c.Dispose(), ErrInactive) {
			return false
		}
		return errA == nil && errB == nil && a == "va" && b == "vb" && allow.Load() == 1
	}, Apply: func(*Context) error { loads.Add(1); return nil }})
	other := kernelMount(t, ctx, r, Spec{ID: "plain", Needs: []string{"a"}, Apply: func(*Context) error { return nil }})
	state := func(id uint64, want State) {
		t.Helper()
		if err := r.Settle(ctx); err != nil {
			t.Fatal(err)
		}
		snapshots, _ := r.Snapshots(ctx)
		for _, s := range snapshots {
			if s.ID == id && s.State != want {
				t.Fatalf("fiber %d is %s, want %s", id, s.State, want)
			}
		}
	}
	state(id, Pending)
	state(other, Active) // The check of one consumer never gates another.
	if loads.Load() != 0 || checks.Load() == 0 {
		t.Fatalf("loads %d, checks %d", loads.Load(), checks.Load())
	}
	allow.Store(1)
	before := checks.Load()
	if err := r.(Notifier).Notify(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	state(id, Active)
	if n := checks.Load() - before; n != 1 {
		t.Fatalf("one evaluation ran the predicate %d times, want once for two needs", n)
	}
	allow.Store(0)
	if err := r.(Notifier).Notify(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	state(id, Pending)
}

func TestKernelConfigHooks(t *testing.T) {
	ctx, r := kernelFixture(t)
	var mu sync.Mutex
	var calls []string
	note := func(s string) { mu.Lock(); calls = append(calls, s); mu.Unlock() }
	var veto atomic.Bool
	cell := NewConfiguration("raw0", ConfigHooks{
		Resolve: func(_ context.Context, raw any) (any, error) {
			note("resolve:" + raw.(string))
			return "eff-" + raw.(string), nil
		},
		Commit: func(_ context.Context, raw any) error { note("commit:" + raw.(string)); return nil },
		Update: func(_ context.Context, resolved any, next func() error) error {
			note("update:" + resolved.(string))
			if veto.Load() {
				return nil
			}
			return next()
		},
	})
	seen := make(chan any, 8)
	id := kernelMount(t, ctx, r, Spec{ID: "cfg", Config: cell, Apply: func(c *Context) error { seen <- c.Config(); return nil }})
	if err := r.(Waiter).AwaitFiber(ctx, id); err != nil || <-seen != "eff-raw0" {
		t.Fatalf("first activation: %v", err)
	}
	if err := r.(ConfigUpdater).UpdateConfig(ctx, id, "raw1", false); err != nil || <-seen != "eff-raw1" {
		t.Fatalf("update: %v", err)
	}
	if err := r.(ConfigUpdater).UpdateConfig(ctx, id, "raw2", true); err != nil || <-seen != "eff-raw2" {
		t.Fatalf("noSave update: %v", err)
	}
	veto.Store(true)
	if err := r.(ConfigUpdater).UpdateConfig(ctx, id, "raw3", false); err != nil {
		t.Fatal(err)
	}
	select {
	case v := <-seen:
		t.Fatalf("a vetoed update restarted the fiber: %v", v)
	default:
	}
	snapshots, err := r.Snapshots(ctx)
	if err != nil || len(snapshots) != 1 || !snapshots[0].Configured || !snapshots[0].ConfigPrepared ||
		snapshots[0].RawConfig != "raw3" || snapshots[0].EffectiveConfig != "eff-raw3" || snapshots[0].ConfigVersion != 3 {
		t.Fatalf("snapshot %+v, %v; a veto retains the raw config and publishes the candidate", snapshots, err)
	}
	mu.Lock()
	want := []string{"resolve:raw0", "resolve:raw1", "commit:raw1", "update:eff-raw1", "resolve:raw1",
		"resolve:raw2", "update:eff-raw2", "resolve:raw2", "resolve:raw3", "commit:raw3", "update:eff-raw3"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("hook calls\n got  %v\n want %v", calls, want)
	}
	mu.Unlock()
	failing := kernelMount(t, ctx, r, Spec{ID: "bad", Apply: func(*Context) error { return nil }, Config: NewConfiguration("x", ConfigHooks{
		Resolve: func(context.Context, any) (any, error) { return nil, errors.New("resolve failed") }})})
	if err := r.(Waiter).AwaitFiber(ctx, failing); err == nil {
		t.Error("a failing Resolve did not fail the activation")
	}
}

func TestKernelObserversAndRetention(t *testing.T) {
	ctx, r := kernelFixture(t)
	root, _ := r.(RootProvider).RootContext(ctx)
	obs := r.(OverflowObserverRuntime)
	if _, err := obs.ObserveEvents(nil, "x", func(context.Context, Event) error { return nil }); !errors.Is(err, ErrObserverBinding) {
		t.Errorf("nil owner = %v", err)
	}
	other, _ := NewKernel().(RootProvider).RootContext(ctx)
	if _, err := obs.ObserveEvents(other, "x", func(context.Context, Event) error { return nil }); !errors.Is(err, ErrObserverBinding) {
		t.Errorf("foreign owner = %v", err)
	}
	if _, err := obs.ObserveEvents(root, "", func(context.Context, Event) error { return nil }); !errors.Is(err, eventqueue.ErrObserver) {
		t.Errorf("no label = %v", err)
	}
	if _, err := obs.ObserveEvents(root, "nil", nil); !errors.Is(err, eventqueue.ErrObserver) {
		t.Errorf("no callback = %v", err)
	}
	var reentrant atomic.Pointer[error]
	if _, err := obs.ObserveEvents(root, "reentrant", func(cb context.Context, _ Event) error {
		if reentrant.Load() == nil {
			err := obs.AwaitObservers(cb)
			reentrant.Store(&err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	kernelMount(t, ctx, r, Spec{ID: "a"})
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	if err := obs.AwaitObservers(ctx); err != nil {
		t.Fatal(err)
	}
	if err := reentrant.Load(); err == nil || !errors.Is(*err, eventqueue.ErrReentrant) {
		t.Errorf("AwaitObservers from a callback = %v", err)
	}
	events, _ := r.Events(ctx)
	status := obs.ObserverStatus()
	if status.Accepted != uint64(len(events)) || status.Delivered != status.Accepted || status.Observers != 1 {
		t.Errorf("status %+v, %d events", status, len(events))
	}
	retention := r.(RetentionReporter).RetentionStatus()
	if retention.EventsTotal != uint64(len(events)) || retention.Windowed != len(events) || retention.EventWindow != DefaultEventWindow ||
		retention.ArchiveLimit != DefaultArchiveLimit || retention.ObserverBacklog != DefaultObserverBacklog {
		t.Errorf("retention %+v, %d events", retention, len(events))
	}
}

func TestKernelEventWindowAndExpiry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := NewKernelWith(Retention{EventWindow: 4, ArchiveLimit: 1})
	defer func() { _ = r.Close(ctx) }()
	first := kernelMount(t, ctx, r, Spec{ID: "one"})
	second := kernelMount(t, ctx, r, Spec{ID: "two"})
	for _, id := range []uint64{first, second} {
		if err := r.Dispose(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := r.Settle(ctx); err != nil {
			t.Fatal(err)
		}
	}
	events, err := r.Events(ctx)
	if !errors.Is(err, ErrEventsTruncated) || len(events) != 4 || events[3].Sequence <= 4 {
		t.Fatalf("events = %+v, %v; want the 4-event tail and ErrEventsTruncated", events, err)
	}
	if err := r.(Waiter).AwaitFiber(ctx, first); !errors.Is(err, ErrExpired) {
		t.Fatalf("await of an evicted fiber = %v, want ErrExpired", err)
	}
	if status := r.(RetentionReporter).RetentionStatus(); status.EventWindow != 4 || status.Windowed != 4 || status.ArchiveEvicted != 1 {
		t.Errorf("retention %+v", status)
	}
	if err := r.Dispose(ctx, 99); !errors.Is(err, ErrUnknown) {
		t.Errorf("dispose of an unissued fiber = %v, want ErrUnknown", err)
	}
}

func TestKernelEffectScope(t *testing.T) {
	ctx, r := kernelFixture(t)
	var order []string
	var mu sync.Mutex
	note := func(s string) Disposer {
		return func(context.Context) error { mu.Lock(); order = append(order, s); mu.Unlock(); return nil }
	}
	handles := make(chan *EffectHandle, 2)
	id := kernelMount(t, ctx, r, Spec{ID: "scoped", Apply: func(c *Context) error {
		h, err := c.RequestEffectScope("tree", func(s *EffectScope) error {
			if err := s.Defer("a", note("a")); err != nil {
				return err
			}
			return s.Scope("inner", func(s *EffectScope) error { return s.Defer("b", note("b")) })
		})
		if err != nil {
			return err
		}
		failing, err := c.RequestEffectScope("fails", func(s *EffectScope) error {
			if err := s.Defer("c", note("c")); err != nil {
				return err
			}
			panic("boom")
		})
		handles <- h
		handles <- failing
		return err
	}})
	if err := r.(Waiter).AwaitFiber(ctx, id); err != nil {
		t.Fatal(err)
	}
	h, failing := <-handles, <-handles
	if err := h.AwaitSetup(ctx); err != nil {
		t.Fatal(err)
	}
	if meta := h.Metadata(); meta.State != ScopeReady || len(meta.Children) != 2 || meta.Children[1].Label != "inner" {
		t.Fatalf("metadata %+v", meta)
	}
	var panicErr *ScopePanicError
	if err := failing.AwaitSetup(ctx); !errors.As(err, &panicErr) || panicErr.Value != "boom" {
		t.Fatalf("failing setup = %v", err)
	}
	if err := h.RequestWithdraw(); err != nil {
		t.Fatal(err)
	}
	if err := h.Await(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := []string{"c", "b", "a"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("cleanup order %v, want %v", order, want)
	}
}

// A disposer returned by a load that requested its own disposal must still run:
// the approximation runs it at once, which is earlier than the prototypes run it.
func TestKernelApplyEffectOfASelfDisposingLoadStillRunsItsDisposer(t *testing.T) {
	ctx, r := kernelFixture(t)
	ran := make(chan struct{}, 1)
	id := kernelMount(t, ctx, r, Spec{ID: "self", ApplyEffect: func(c *Context) (Disposer, error) {
		if err := c.Dispose(); err != nil {
			return nil, err
		}
		return func(context.Context) error { ran <- struct{}{}; return nil }, nil
	}})
	if err := r.(Waiter).AwaitFiber(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ran:
	default:
		t.Fatal("the returned disposer of a self-disposing load never ran")
	}
	if snapshots, _ := r.Snapshots(ctx); len(snapshots) != 1 || snapshots[0].State != Disposed || snapshots[0].Error != "" {
		t.Fatalf("snapshots %+v", snapshots)
	}
}

// An asynchronous update hook returns after its synchronous prefix and completes
// later, calling next then: the request returns at the prefix, and Await at the end.
func TestKernelUpdateAsyncHookCompletesAfterTheRequestReturns(t *testing.T) {
	ctx, r := kernelFixture(t)
	release, finished := make(chan struct{}), make(chan error, 1)
	var hookResult error
	seen := make(chan any, 4)
	cell := NewConfiguration("a", ConfigHooks{UpdateAsync: func(_ context.Context, _ any, next func() error) (func(context.Context) error, error) {
		go func() {
			<-release
			hookResult = next()
			finished <- hookResult
		}()
		return func(w context.Context) error {
			select {
			case err := <-finished:
				finished <- err
				return err
			case <-w.Done():
				return w.Err()
			}
		}, nil
	}})
	id := kernelMount(t, ctx, r, Spec{ID: "async", Config: cell, Apply: func(c *Context) error { seen <- c.Config(); return nil }})
	if err := r.(Waiter).AwaitFiber(ctx, id); err != nil || <-seen != "a" {
		t.Fatalf("first activation: %v", err)
	}
	request, err := r.(ConfigRequester).RequestUpdateConfig(ctx, id, "b", false)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case v := <-seen:
		t.Fatalf("the update restarted the fiber before its hook continued: %v", v)
	default:
	}
	close(release)
	if err := request.Await(ctx); err != nil || <-seen != "b" {
		t.Fatalf("await = %v", err)
	}
	if err := request.Await(ctx); err != nil {
		t.Fatalf("a second Await = %v", err)
	}
	if hookResult != nil {
		t.Fatalf("next = %v", hookResult)
	}
}

// failingCommitScenario drives a failing Commit on any backend, with the fiber
// loaded or gated on a need, and returns what the caller sees.
func failingCommitScenario(t *testing.T, build func() Runtime, gated bool) (error, Snapshot, []Event) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := build()
	defer func() { _ = r.Close(ctx) }()
	failure := errors.New("commit failed")
	cell := NewConfiguration("old", ConfigHooks{
		Commit: func(context.Context, any) error { return failure },
	})
	spec := Spec{ID: "cfg", Config: cell, Apply: func(*Context) error { return nil }}
	if gated {
		spec.Needs = []string{"absent"}
	}
	id := kernelMount(t, ctx, r, spec)
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	err := r.(ConfigUpdater).UpdateConfig(ctx, id, "new", false)
	if !errors.Is(err, failure) {
		t.Fatalf("update error %v, want the Commit failure", err)
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	snapshots, serr := r.Snapshots(ctx)
	if serr != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots %v, %v", snapshots, serr)
	}
	events, eerr := r.Events(ctx)
	if eerr != nil {
		t.Fatal(eerr)
	}
	for i := range events {
		if len(events[i].Data) == 0 {
			events[i].Data = nil
		}
	}
	return err, snapshots[0], events
}

// TestKernelFailingCommitAbortsLikeTheReconciler requires the same returned error
// identity, retained config and events from both backends on a failing Commit:
// nothing is retained and the fiber is not restarted (ADR-034 owner decision 15).
func TestKernelFailingCommitAbortsLikeTheReconciler(t *testing.T) {
	for _, gated := range []bool{false, true} {
		t.Run(map[bool]string{false: "loaded", true: "gated"}[gated], func(t *testing.T) {
			err, snap, events := failingCommitScenario(t, NewRecordingKernel, gated)
			wantErr, wantSnap, wantEvents := failingCommitScenario(t, NewRecordingReconciler, gated)
			if err.Error() != wantErr.Error() || errors.Unwrap(err) != errors.Unwrap(wantErr) {
				t.Errorf("error %v, reconciler %v", err, wantErr)
			}
			if snap.RawConfig != "old" || snap.ConfigVersion != 0 {
				t.Errorf("retained %+v after a failing commit", snap)
			}
			if !reflect.DeepEqual(snap, wantSnap) {
				t.Errorf("snapshot\n kernel     %+v\n reconciler %+v", snap, wantSnap)
			}
			if !reflect.DeepEqual(events, wantEvents) {
				t.Errorf("events\n kernel     %+v\n reconciler %+v", events, wantEvents)
			}
		})
	}
}

// A noSave update skips Commit, so a failing Commit does not stop it.
func TestKernelNoSaveSkipsAFailingCommit(t *testing.T) {
	ctx, r := kernelFixture(t)
	cell := NewConfiguration("old", ConfigHooks{Commit: func(context.Context, any) error { return errors.New("commit failed") }})
	id := kernelMount(t, ctx, r, Spec{ID: "cfg", Config: cell, Apply: func(*Context) error { return nil }})
	if err := r.(ConfigUpdater).UpdateConfig(ctx, id, "new", true); err != nil {
		t.Fatal(err)
	}
	if raw, _, version := cell.RawSnapshot(); raw != "new" || version != 1 {
		t.Fatalf("raw %v version %d", raw, version)
	}
}

// The callpath gate budgets zero allocations for the owner's Enter and Leave in
// every measured run, and a run may hold the first Enter of a fresh activation.
// The kernel's own path allocates nothing; this holds the adapter to the same
// budget, first call included.
func TestKernelEnterLeaveAllocatesNothing(t *testing.T) {
	ctx, r := kernelFixture(t)
	const fresh = 100
	ctxs := make(chan *Context, fresh)
	for range fresh {
		id := kernelMount(t, ctx, r, Spec{ID: "leased", Apply: func(c *Context) error {
			ctxs <- c
			return nil
		}})
		if err := r.(Waiter).AwaitFiber(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	firsts := make([]*Context, 0, fresh)
	for range fresh {
		firsts = append(firsts, <-ctxs)
	}
	// testing.AllocsPerRun warms up with one call, which would hide an allocation
	// of the first call, so the first calls are measured here as it measures: on
	// one P, mallocs over all runs divided by the runs.
	refused := 0
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for _, c := range firsts {
		if !c.Enter() {
			refused++
			continue
		}
		c.Leave()
	}
	runtime.ReadMemStats(&after)
	if refused != 0 {
		t.Fatalf("Enter refused on %d active fibers", refused)
	}
	if got := (after.Mallocs - before.Mallocs) / fresh; got != 0 {
		t.Fatalf("the first Enter+Leave of an activation allocated %d objects per call, want 0", got)
	}
	c := firsts[0]
	if got := testing.AllocsPerRun(1000, func() {
		if !c.Enter() {
			panic("Enter refused on an active fiber")
		}
		c.Leave()
	}); got != 0 {
		t.Fatalf("Enter+Leave allocated %v objects per call, want 0", got)
	}
}

// The adapter forgets the fibers whose kernel record was evicted, so its own heap
// stays bounded by the archive, and still answers for them as the kernel does.
func TestKernelForgetsEvictedFibers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := NewKernelWith(Retention{ArchiveLimit: 4}).(*kernelRuntime)
	defer func() { _ = r.Close(ctx) }()
	var first uint64
	for i := range 2*r.cfg.ArchiveLimit + 1100 {
		id, err := r.Mount(ctx, Spec{ID: "churn", Apply: func(*Context) error { return nil }})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = id
		}
		if err := r.Dispose(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := r.AwaitFiber(ctx, id); err != nil && !errors.Is(err, ErrExpired) {
			t.Fatal(err)
		}
	}
	r.mu.RLock()
	held := len(r.fibers)
	r.mu.RUnlock()
	if held > 2*r.cfg.ArchiveLimit+1024 {
		t.Fatalf("adapter holds %d fibers after churn; the kernel archive holds %d", held, r.cfg.ArchiveLimit)
	}
	if _, ok := r.lookup(first); ok {
		t.Fatal("the first fiber is still held although its record was evicted")
	}
	if err := r.AwaitFiber(ctx, first); !errors.Is(err, ErrExpired) {
		t.Fatalf("AwaitFiber of an evicted fiber = %v, want ErrExpired", err)
	}
	if err := r.Dispose(ctx, first); err != nil {
		t.Fatalf("Dispose of an evicted fiber = %v, want nil (a no-op, as in the kernel)", err)
	}
	if err := r.Update(ctx, first); !errors.Is(err, ErrUnknown) {
		t.Fatalf("Update of an evicted fiber = %v, want ErrUnknown, as the kernel answers", err)
	}
	if err := r.Dispose(ctx, 1<<40); !errors.Is(err, ErrUnknown) {
		t.Fatalf("Dispose of a never issued fiber = %v, want ErrUnknown", err)
	}
}
