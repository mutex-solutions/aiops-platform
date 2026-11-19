// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
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

// bubbleStacks returns the stack dump of the goroutines in the calling
// goroutine's synctest bubble only. runtime.Stack(all) is process-wide, so a
// count taken from it also sees an owner of an earlier test that has closed its
// done channel but not yet returned (the close precedes the return, and the
// goroutine can be descheduled in between). Goroutines of other tests are never
// in this bubble, which is the isolation the idle-worker assertions mean.
func bubbleStacks(t testing.TB) string {
	t.Helper()
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	for n == len(buf) {
		buf = make([]byte, 2*len(buf))
		n = runtime.Stack(buf, true)
	}
	blocks := strings.Split(string(buf[:n]), "\n\n")
	tag := bubbleTag.FindString(blocks[0]) // The calling goroutine is first.
	if tag == "" {
		t.Fatal("bubbleStacks must run inside synctest.Test")
	}
	var mine []string
	for _, b := range blocks {
		header, _, _ := strings.Cut(b, "\n")
		if strings.HasSuffix(header, ", "+tag+"]:") {
			mine = append(mine, b)
		}
	}
	return strings.Join(mine, "\n\n")
}

var bubbleTag = regexp.MustCompile(`synctest bubble \d+`)

func rcRuntime(t *testing.T) Runtime {
	t.Helper()
	r := NewRecordingReconciler()
	t.Cleanup(func() {
		if err := closeRuntime(rcGuard(t), r); err != nil {
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

func rcInspect(t *testing.T, r Runtime) Inspection {
	t.Helper()
	inspector, ok := r.(Inspector)
	if !ok {
		t.Fatal("reconciler does not implement Inspector")
	}
	inspection, err := inspector.Inspect(rcGuard(t))
	if err != nil {
		t.Fatal(err)
	}
	return inspection
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

func TestReconcilerScopeUnloadDeadlineRetainsDeadlineExceededCause(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewRecordingReconciler()
		handles := make(chan *EffectHandle, 1)
		id := rcMount(t, r, Spec{ID: "scope-deadline", Apply: func(c *Handle) error {
			h, err := c.RequestEffectScope("deadline", func(s *EffectScope) error {
				return s.Defer("wait-for-budget", func(ctx context.Context) error {
					<-ctx.Done()
					return ctx.Err()
				})
			})
			if err == nil {
				handles <- h
			}
			return err
		}})
		h := <-handles
		if err := h.AwaitSetup(rcGuard(t)); err != nil {
			t.Fatal(err)
		}
		rcSettle(t, r)
		if err := r.Dispose(rcGuard(t), id); err != nil {
			t.Fatal(err)
		}
		before := time.Now()
		if err := h.Await(rcGuard(t)); !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || time.Since(before) != testDispose {
			t.Fatalf("native cleanup deadline cause = %v after %s", err, time.Since(before))
		}
		synctest.Wait()
		_ = r.Settle(rcGuard(t))
		if err := r.Close(rcGuard(t)); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("unload deadline diagnostic lost: %v", err)
		}
	})
}

func TestReconcilerScopeIncrementalPanicRollsBackOnNativeLane(t *testing.T) {
	r := rcRuntime(t)
	var mu sync.Mutex
	var order []string
	appendOrder := func(label string) { mu.Lock(); order = append(order, label); mu.Unlock() }
	panicErr := errors.New("scope setup panic")
	handles := make(chan *EffectHandle, 1)
	var applyReturned atomic.Bool
	id := rcMount(t, r, Spec{ID: "incremental-scope", Apply: func(c *Handle) error {
		defer applyReturned.Store(true)
		h, err := c.RequestEffectScope("outer", func(s *EffectScope) error {
			if !applyReturned.Load() {
				return errors.New("scope setup overlapped Apply")
			}
			if err := s.Defer("first", func(context.Context) error { appendOrder("first"); return nil }); err != nil {
				return err
			}
			if err := s.Scope("nested", func(child *EffectScope) error {
				return child.Defer("second", func(ctx context.Context) error {
					if _, err := r.(Inspector).Inspect(ctx); err != nil {
						return err
					}
					appendOrder("second")
					return nil
				})
			}); err != nil {
				return err
			}
			panic(panicErr)
		})
		if err == nil {
			handles <- h
		}
		return err
	}})
	h := <-handles
	if err := h.AwaitSetup(rcGuard(t)); !errors.Is(err, panicErr) {
		t.Fatalf("scope rollback lost typed panic cause: %v", err)
	} else {
		var typed *scopePanicError
		if !errors.As(err, &typed) || typed.Value != panicErr {
			t.Fatalf("scope panic identity = %#v", typed)
		}
	}
	rcSettle(t, r)
	mu.Lock()
	if !slices.Equal(order, []string{"second", "first"}) {
		t.Errorf("incremental rollback order = %v", order)
	}
	mu.Unlock()
	if s := rcSnapshot(t, r, id); s.State != Active || s.Target == nil || len(s.EffectTree) != 0 || s.Error == "" {
		t.Fatalf("scope failure changed activation or lost cached diagnostic: %+v", s)
	}
}

func TestReconcilerScopePendingQuiescenceNeverRunsSetup(t *testing.T) {
	r := rcRuntime(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	handles := make(chan *EffectHandle, 1)
	var called atomic.Bool
	id := rcMount(t, r, Spec{ID: "pending-scope", Apply: func(c *Handle) error {
		h, err := c.RequestEffectScope("never-started", func(*EffectScope) error { called.Store(true); return nil })
		if err != nil {
			return err
		}
		handles <- h
		close(entered)
		<-release
		return nil
	}})
	<-entered
	h := <-handles
	if err := r.Dispose(rcGuard(t), id); err != nil {
		t.Fatal(err)
	}
	if err := h.AwaitSetup(rcGuard(t)); !errors.Is(err, ErrInactive) || !errors.Is(err, ErrInactiveEffect) {
		t.Fatalf("never-started scope cancellation = %v", err)
	}
	if called.Load() || h.Metadata().State != ScopeDone {
		t.Fatal("quiescence ran late scope setup")
	}
	releaseOnce.Do(func() { close(release) })
	rcSettle(t, r)
	if err := h.RequestWithdraw(); err != nil {
		t.Fatalf("completed scope withdrawal not idempotent: %v", err)
	}
	if s := rcSnapshot(t, r, id); s.State != Disposed || s.Error != "" || len(s.EffectTree) != 0 {
		t.Fatalf("expected pending cancellation became runtime failure: %+v", s)
	}
}

func TestReconcilerScopeManualCleanupFailureRemainsHandleLocal(t *testing.T) {
	r := rcRuntime(t)
	contexts := make(chan *Handle, 1)
	id := rcMount(t, r, Spec{ID: "active-scope", Apply: func(c *Handle) error { contexts <- c; return nil }})
	rcSettle(t, r)
	c := <-contexts
	cleanupErr := errors.New("manual cleanup diagnostic")
	h, err := c.RequestEffectScope("manual", func(s *EffectScope) error {
		return s.Defer("resource", func(context.Context) error { return cleanupErr })
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.AwaitSetup(rcGuard(t)); err != nil {
		t.Fatal(err)
	}
	if err := h.RequestWithdraw(); err != nil {
		t.Fatal(err)
	}
	if err := h.RequestWithdraw(); err != nil {
		t.Fatal(err)
	}
	if err := h.Await(rcGuard(t)); !errors.Is(err, cleanupErr) {
		t.Fatalf("manual scope cleanup error identity = %v", err)
	}
	rcSettle(t, r)
	if s := rcSnapshot(t, r, id); s.State != Active || !s.Alive || s.Target == nil || len(s.EffectTree) != 0 || s.Error != cleanupErr.Error() {
		t.Fatalf("manual scope failure retired activation or lost diagnostic: %+v", s)
	}
}

func TestReconcilerContextOwnershipAckSurvivesCancellationAfterAdmission(t *testing.T) {
	for _, operation := range []string{"provide", "child"} {
		t.Run(operation, func(t *testing.T) {
			// Use the actual owner handler and root activation, but hold the
			// transport acknowledgment until after native quiescence. This makes
			// the accepted-command/canceled-context race deterministic.
			r := &core{cfg: testConfig(), inbox: make(chan message, 1), wake: make(chan struct{}, 1), done: make(chan struct{})}
			o := newOwner(r)
			o.startRoot()
			c := o.root.act.ctx
			if !c.Enter() {
				t.Fatal("root setup lease refused")
			}
			result := make(chan answer, 1)
			if operation == "provide" {
				go func() { result <- answer{err: c.Provide("resource", "actual")} }()
			} else {
				go func() {
					id, err := c.MountChild(Spec{ID: "actual-child", Needs: []string{"missing"}})
					result <- answer{id: id, err: err}
				}()
			}
			queued := <-r.inbox
			reply, _ := o.handle(queued)
			if reply.err != nil {
				t.Fatal(reply.err)
			}
			o.quiesce(o.root)
			if c.Std().Err() != context.Canceled {
				t.Fatal("fixture did not cancel after admission")
			}
			queued.ack <- reply
			got := <-result
			if got.err != nil || got.id != reply.id {
				t.Fatalf("native admission succeeded but ownership ack was lost: reply=%+v got=%+v", reply, got)
			}
			if operation == "provide" {
				if provision := o.providers["resource"]; provision == nil || provision.binding.value != "actual" || !c.Owns("resource") {
					t.Fatal("accepted native reservation was not retained")
				}
			} else if got.id == 0 || o.fibers[got.id] == nil {
				t.Fatal("accepted native child did not return its real ID")
			}
			c.Leave()
			o.work()
			close(r.done)
			if err := r.observers.close(rcGuard(t)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReconcilerProvideCancellationRaceNeverReportsFailureForReservedUID(t *testing.T) {
	r := rcRuntime(t)
	contexts := make(chan *Handle, 1)
	const count = 64
	keys := make([]string, count)
	resources := make([]*atomic.Bool, count)
	for i := range count {
		keys[i], resources[i] = "resource-"+strconv.Itoa(i), new(atomic.Bool)
	}
	provider := rcMount(t, r, Spec{ID: "provider", Provides: keys, Apply: func(c *Handle) error {
		contexts <- c
		if err := c.Defer("resources", func(context.Context) error {
			for _, resource := range resources {
				resource.Store(true)
			}
			return nil
		}); err != nil {
			return err
		}
		return c.Provide(keys[0], resources[0])
	}})
	rcSettle(t, r)
	c := <-contexts
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	rcMount(t, r, Spec{ID: "consumer", Needs: keys[:1], Apply: func(c *Handle) error {
		return c.Defer("gated-consumer", func(context.Context) error {
			value, err := c.Read(keys[0])
			if err != nil || value.(*atomic.Bool).Load() {
				return errors.New("provider resource released before retained consumer cleanup")
			}
			close(entered)
			<-release
			if value.(*atomic.Bool).Load() {
				return errors.New("provider resource released during retained consumer cleanup")
			}
			return nil
		})
	}})
	rcSettle(t, r)
	start := make(chan struct{})
	var workers sync.WaitGroup
	var rejected [count]atomic.Bool
	for i := 1; i < count; i++ {
		if !c.Enter() {
			t.Fatal("setup lease refused")
		}
		workers.Go(func() {
			defer c.Leave()
			<-start
			if err := c.Provide(keys[i], resources[i]); err != nil {
				if !errors.Is(err, ErrInactive) {
					t.Errorf("setup rejection = %v", err)
				}
				rejected[i].Store(true)
				resources[i].Store(true) // The caller owns rollback only on rejection.
			}
		})
	}
	close(start)
	if err := r.Dispose(rcGuard(t), provider); err != nil {
		t.Fatal(err)
	}
	<-entered
	workers.Wait()
	for _, provision := range rcInspect(t, r).Provisions {
		if provision.Owner != provider {
			continue
		}
		for i, key := range keys {
			if provision.Key == key && (rejected[i].Load() || resources[i].Load()) {
				t.Fatalf("successful native UID %d returned rejection/caller rollback for %s", provision.UID, key)
			}
		}
	}
	releaseOnce.Do(func() { close(release) })
	rcSettle(t, r)
}

func TestReconcilerRetiringContextCannotDisposeDesiredReplacement(t *testing.T) {
	r := rcRuntime(t)
	contexts := make(chan *Handle, 2)
	cleanupEntered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var loads atomic.Int32
	id := rcMount(t, r, Spec{ID: "replacement", Apply: func(c *Handle) error {
		contexts <- c
		if loads.Add(1) != 1 {
			return nil
		}
		return c.Defer("held-cleanup", func(context.Context) error { close(cleanupEntered); <-release; return nil })
	}})
	rcSettle(t, r)
	old := <-contexts
	if err := r.Update(rcGuard(t), id); err != nil {
		t.Fatal(err)
	}
	<-cleanupEntered
	if err := old.Dispose(); !errors.Is(err, ErrInactive) {
		t.Fatalf("retiring activation disposed newer desired epoch: %v", err)
	}
	if s := rcSnapshot(t, r, id); !s.Alive || s.Target == nil || s.Target.Revision != 1 {
		t.Fatalf("stale Dispose poisoned replacement target: %+v", s)
	}
	releaseOnce.Do(func() { close(release) })
	rcSettle(t, r)
	<-contexts
	if s := rcSnapshot(t, r, id); s.State != Active || !s.Alive || s.Revision != 1 || loads.Load() != 2 {
		t.Fatalf("replacement did not survive stale context: %+v", s)
	}
}

func TestReconcilerPartialEffectCleanupUsesFreshNativeDisposeBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewRecordingReconciler()
		contexts := make(chan *Handle, 1)
		id := rcMount(t, r, Spec{ID: "rollback", Apply: func(c *Handle) error { contexts <- c; return nil }})
		rcSettle(t, r)
		c := <-contexts
		if !c.Enter() {
			t.Fatal("setup lease rejected")
		}
		if err := r.Dispose(rcGuard(t), id); err != nil {
			t.Fatal(err)
		}
		if c.Std().Err() != context.Canceled || c.cleanupFn == nil {
			t.Fatal("native rollback bridge missing after quiescence")
		}
		cleanupErr := errors.New("partial disposer failure")
		before := time.Now()
		err := c.cleanupFn(func(ctx context.Context) error {
			if ctx.Err() != nil {
				return errors.New("rollback inherited canceled activation context")
			}
			deadline, ok := ctx.Deadline()
			if !ok || deadline.Sub(before) != testDispose {
				return errors.New("rollback lacks native disposal budget")
			}
			if _, err := r.(Inspector).Inspect(ctx); err != nil {
				return err
			}
			<-ctx.Done()
			return cleanupErr
		})
		if !errors.Is(err, cleanupErr) || !errors.Is(err, context.DeadlineExceeded) || time.Since(before) != testDispose {
			t.Fatalf("rollback result/budget = %v after %s", err, time.Since(before))
		}
		if c.released.Load() || c.lease.Load() != 1 {
			t.Fatal("rollback deadline fabricated native lease completion")
		}
		if err := c.cleanupFn(func(context.Context) error { panic("rollback panic") }); err == nil {
			t.Fatal("rollback panic escaped native callback boundary")
		}
		c.Leave()
		rcSettle(t, r)
		if err := r.Close(rcGuard(t)); err != nil {
			t.Fatal(err)
		}
	})
}

func TestReconcilerEffectRegistrationCancellationTransfersOwnershipExactlyOnce(t *testing.T) {
	r := rcRuntime(t)
	contexts := make(chan *Handle, 1)
	id := rcMount(t, r, Spec{ID: "effect-ack", Apply: func(c *Handle) error { contexts <- c; return nil }})
	rcSettle(t, r)
	c := <-contexts
	const count = 128
	var calls [count]atomic.Int32
	var workers sync.WaitGroup
	start := make(chan struct{})
	// Reserve all setup leases before quiescence. Registration may be accepted
	// or rejected by the owner; only the accepting side may own cleanup.
	for i := range count {
		if !c.Enter() {
			t.Fatal("setup lease rejected before race")
		}
		workers.Go(func() {
			defer c.Leave()
			<-start
			dispose := func(context.Context) error { calls[i].Add(1); return nil }
			if err := c.Defer("racing-effect", dispose); err != nil {
				if !errors.Is(err, ErrInactive) {
					t.Errorf("registration race returned %v", err)
				}
				if err := c.cleanupFn(dispose); err != nil {
					t.Errorf("caller rollback = %v", err)
				}
			}
		})
	}
	close(start)
	if err := r.Dispose(rcGuard(t), id); err != nil {
		t.Fatal(err)
	}
	workers.Wait()
	rcSettle(t, r)
	for i := range count {
		if got := calls[i].Load(); got != 1 {
			t.Fatalf("registration %d transferred disposer ownership %d times", i, got)
		}
	}
}

func TestReconcilerBatchInvalidSuffixHasNoPrefixOrIDEffects(t *testing.T) {
	r := rcRuntime(t)
	id := rcMount(t, r, Spec{ID: "existing"})
	rcSettle(t, r)
	before := rcInspect(t, r)
	events, err := r.Events(rcGuard(t))
	if err != nil {
		t.Fatal(err)
	}
	var callbacks atomic.Int32
	operations := []BatchOperation{
		{Kind: BatchMount, Spec: Spec{ID: "not-mounted", Apply: func(*Handle) error { callbacks.Add(1); return nil }}},
		{Kind: BatchUpdate, Fiber: id},
		{Kind: BatchDispose, Fiber: id},
		{Kind: BatchUpdate, Fiber: id},
	}
	if ids, err := r.(Batcher).Batch(rcGuard(t), operations); err == nil || ids != nil {
		t.Fatalf("invalid suffix admitted prefix: ids=%v err=%v", ids, err)
	}
	if after := rcInspect(t, r); !reflect.DeepEqual(after, before) || callbacks.Load() != 0 {
		t.Fatalf("invalid batch mutated native state: before=%+v after=%+v callbacks=%d", before, after, callbacks.Load())
	}
	afterEvents, err := r.Events(rcGuard(t))
	if err != nil || !reflect.DeepEqual(afterEvents, events) {
		t.Fatalf("invalid batch changed ledger: %v %+v", err, afterEvents)
	}
	if next := rcMount(t, r, Spec{ID: "next"}); next != id+1 {
		t.Fatalf("invalid prefix consumed mount identity: %d -> %d", id, next)
	}
}

func TestReconcilerBatchRepeatedUpdatesPublishOnlyLatestRevision(t *testing.T) {
	r := rcRuntime(t)
	var loads atomic.Int32
	id := rcMount(t, r, Spec{ID: "updated", Apply: func(*Handle) error { loads.Add(1); return nil }})
	rcSettle(t, r)
	ids, err := r.(Batcher).Batch(rcGuard(t), []BatchOperation{
		{Kind: BatchUpdate, Fiber: id},
		{Kind: BatchUpdate, Fiber: id},
		{Kind: BatchMount, Spec: Spec{ID: "new"}},
	})
	if err != nil || !slices.Equal(ids, []uint64{0, 0, id + 1}) {
		t.Fatalf("batch result positions = %v, %v", ids, err)
	}
	rcSettle(t, r)
	if s := rcSnapshot(t, r, id); s.Revision != 2 || s.Target == nil || s.Target.Revision != 2 || s.State != Active || loads.Load() != 2 {
		t.Fatalf("intermediate revision admitted an activation: %+v loads=%d", s, loads.Load())
	}
	events, err := r.Events(rcGuard(t))
	if err != nil {
		t.Fatal(err)
	}
	var loading, unloading int
	for _, event := range events {
		if event.Fiber == id && event.Kind == "state" {
			if event.State == Loading {
				loading++
			}
			if event.State == Unloading {
				unloading++
			}
		}
	}
	if loading != 2 || unloading != 1 {
		t.Fatalf("batch did not coalesce lifecycle work: Loading=%d Unloading=%d", loading, unloading)
	}
}

func TestReconcilerBatchColdConsumerBeforeProviderUpdateNeverBindsOldUID(t *testing.T) {
	r := rcRuntime(t)
	var providerLoads, checks atomic.Int32
	provider := rcMount(t, r, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Handle) error { return c.Provide("sample", providerLoads.Add(1)) }})
	rcSettle(t, r)
	oldUID := rcInspect(t, r).Provisions[0].UID
	seen := make(chan any, 1)
	ids, err := r.(Batcher).Batch(rcGuard(t), []BatchOperation{
		{Kind: BatchMount, Spec: Spec{ID: "cold", Needs: []string{"sample"}, Check: func(c *Handle) bool {
			checks.Add(1)
			value, err := c.Read("sample")
			return err == nil && value == int32(2)
		}, Apply: func(c *Handle) error { value, err := c.Read("sample"); seen <- value; return err }}},
		{Kind: BatchUpdate, Fiber: provider},
	})
	if err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
	if s := rcSnapshot(t, r, ids[0]); s.State != Active || len(s.Epoch) != 1 || s.Epoch[0] == oldUID || checks.Load() != 1 {
		t.Fatalf("cold Check/Apply observed superseded provider: %+v checks=%d", s, checks.Load())
	}
	if value := <-seen; value != int32(2) {
		t.Fatalf("cold consumer bound old provider value: %v", value)
	}
}

func TestReconcilerBatchHeldApplyJoinsBeforeLatestActivation(t *testing.T) {
	r := rcRuntime(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var loads, running, peak atomic.Int32
	id := rcMount(t, r, Spec{ID: "held", Apply: func(*Handle) error {
		current := running.Add(1)
		defer running.Add(-1)
		for previous := peak.Load(); current > previous && !peak.CompareAndSwap(previous, current); previous = peak.Load() {
		}
		if loads.Add(1) == 1 {
			close(entered)
			<-release
			return errors.New("superseded failure")
		}
		return nil
	}})
	<-entered
	if _, err := r.(Batcher).Batch(rcGuard(t), []BatchOperation{{Kind: BatchUpdate, Fiber: id}, {Kind: BatchUpdate, Fiber: id}}); err != nil {
		t.Fatal(err)
	}
	if s := rcSnapshot(t, r, id); s.Target == nil || s.Target.Revision != 2 || s.State != Loading || loads.Load() != 1 {
		t.Fatalf("batch overlapped or hid held Apply: %+v loads=%d", s, loads.Load())
	}
	releaseOnce.Do(func() { close(release) })
	rcSettle(t, r)
	if s := rcSnapshot(t, r, id); s.State != Active || s.Revision != 2 || s.Error != "" || loads.Load() != 2 || peak.Load() != 1 {
		t.Fatalf("old batch completion poisoned/reentered latest target: %+v loads=%d peak=%d", s, loads.Load(), peak.Load())
	}
}

func TestReconcilerDiamondProviderRemovalFollowsBothConsumerStableTransitions(t *testing.T) {
	r := rcRuntime(t)
	provider := rcMount(t, r, Spec{ID: "provider", Provides: []string{"a"}, Apply: func(c *Handle) error { return c.Provide("a", "retained") }})
	branches := make([]uint64, 0, 2)
	for _, key := range []string{"b", "c"} {
		branches = append(branches, rcMount(t, r, Spec{ID: "branch-" + key, Needs: []string{"a"}, Provides: []string{key}, Apply: func(c *Handle) error {
			value, err := c.Read("a")
			if err != nil {
				return err
			}
			return c.Provide(key, value)
		}}))
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var loads atomic.Int32
	leaf := rcMount(t, r, Spec{ID: "leaf", Needs: []string{"b", "c"}, Apply: func(c *Handle) error {
		if loads.Add(1) != 1 {
			return nil
		}
		return c.Defer("gated-leaf", func(context.Context) error {
			close(entered)
			<-release
			return nil
		})
	}})
	rcSettle(t, r)
	oldUID := rcSnapshot(t, r, branches[0]).Epoch[0]
	if err := r.Update(rcGuard(t), provider); err != nil {
		t.Fatal(err)
	}
	<-entered
	if inspection := rcInspect(t, r); len(inspection.Provisions) != 3 || inspection.Provisions[0].UID != oldUID || inspection.Provisions[0].Active {
		t.Fatalf("provider was not retained while diamond leaf cleaned: %+v", inspection.Provisions)
	}
	releaseOnce.Do(func() { close(release) })
	rcSettle(t, r)
	events, err := r.Events(rcGuard(t))
	if err != nil {
		t.Fatal(err)
	}
	var removed uint64
	stable := make(map[uint64]uint64)
	for _, event := range events {
		if event.Kind == "state" && event.From == Unloading && event.State == Pending {
			stable[event.Fiber] = event.Sequence
		}
		if event.Kind == "remove" && event.Fiber == provider && event.UID == oldUID {
			removed = event.Sequence
		}
	}
	for _, consumer := range append(branches, leaf) {
		if stable[consumer] == 0 || removed == 0 || stable[consumer] >= removed {
			t.Fatalf("provider removal %d overtook consumer %d stable transition %d; raw ledger=%+v", removed, consumer, stable[consumer], events)
		}
	}
}

func TestReconcilerObserverQueueRemainsOpenUntilNativeCloseActuallyFinishes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewRecordingReconciler()
		observers := r.(ObserverRuntime)
		entered, release := make(chan struct{}), make(chan struct{})
		id := rcMount(t, r, Spec{ID: "blocked-cleanup", ApplyEffect: func(*Handle) (Disposer, error) {
			return func(context.Context) error { close(entered); <-release; return nil }, nil
		}})
		rcSettle(t, r)
		if err := r.Close(rcGuard(t)); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("blocked native Close = %v", err)
		}
		<-entered
		synctest.Wait()
		before := observers.ObserverStatus()
		if before.Closing || before.Stopped {
			t.Fatalf("bounded lifecycle failure prematurely closed observer queue: %+v", before)
		}
		inspection := rcInspect(t, r)
		if before.Accepted != inspection.Sequence || rcSnapshot(t, r, id).State != Unloading {
			t.Fatalf("observer admissions diverged from live native ledger: status=%+v sequence=%d", before, inspection.Sequence)
		}
		close(release)
		synctest.Wait()
		if err := r.Close(rcGuard(t)); err != nil {
			t.Fatal(err)
		}
		after := observers.ObserverStatus()
		if !after.Closing || !after.Stopped || after.Accepted <= before.Accepted || after.Accepted != after.Delivered || after.Remaining != 0 {
			t.Fatalf("real cleanup completion events dropped or queue not joined: before=%+v after=%+v", before, after)
		}
	})
}

func TestReconcilerRootIsNativeHiddenAnchorWithActualProvisionRecords(t *testing.T) {
	r := NewRecordingReconciler()
	t.Cleanup(func() {
		if err := closeRuntime(rcGuard(t), r); err != nil {
			t.Error(err)
		}
	})
	root, err := r.(RootProvider).RootContext(rcGuard(t))
	if err != nil {
		t.Fatal(err)
	}
	again, err := r.(RootProvider).RootContext(rcGuard(t))
	if err != nil || again != root || root.fiber != 0 || !root.Enter() {
		t.Fatalf("root context identity/live anchor = %p / %p, err=%v", root, again, err)
	}
	root.Leave()
	if err := root.Dispose(); !errors.Is(err, ErrRootDispose) {
		t.Fatalf("root self disposal = %v", err)
	}
	if err := r.Dispose(rcGuard(t), 0); !errors.Is(err, ErrUnknown) {
		t.Fatalf("ordinary Dispose reached host root: %v", err)
	}
	if err := r.Update(rcGuard(t), 0); !errors.Is(err, ErrUnknown) {
		t.Fatalf("ordinary Update reached host root: %v", err)
	}
	if inspection := rcInspect(t, r); len(inspection.Fibers) != 0 || len(inspection.Provisions) != 0 || inspection.Sequence != 0 {
		t.Fatalf("root fabricated startup fiber/events: %+v", inspection)
	}
	if err := root.Provide("", nil); err == nil {
		t.Fatal("root accepted empty provision key")
	}
	type builtin struct{ name string }
	firstValue, secondValue := &builtin{"first"}, &builtin{"second"}
	if err := root.Provide("zeta", firstValue); err != nil {
		t.Fatal(err)
	}
	if err := root.Provide("alpha", secondValue); err != nil {
		t.Fatal(err)
	}
	if err := root.Provide("zeta", "duplicate"); err == nil {
		t.Fatal("root bypassed native duplicate reservation")
	}
	if !root.Owns("zeta") || !root.Owns("alpha") {
		t.Fatal("root provisions not native Context ownership")
	}
	var seen any
	consumer := rcMount(t, r, Spec{ID: "consumer", Needs: []string{"zeta"}, Apply: func(c *Handle) error { var err error; seen, err = c.Read("zeta"); return err }})
	rcSettle(t, r)
	inspection := rcInspect(t, r)
	if seen != firstValue || len(inspection.Fibers) != 1 || inspection.Fibers[0].ID != consumer || len(inspection.Provisions) != 2 {
		t.Fatalf("root provision did not bind real user fiber: %+v value=%p", inspection, seen)
	}
	first, second := inspection.Provisions[0], inspection.Provisions[1]
	if first.Key != "zeta" || second.Key != "alpha" || first.UID == 0 || first.UID >= second.UID || first.Owner != 0 || second.Owner != 0 || first.Name != "root" || second.Name != "root" || !first.Active || !second.Active || first.Value != firstValue || second.Value != secondValue {
		t.Fatalf("root native provider records = %+v", inspection.Provisions)
	}
	events, err := r.Events(rcGuard(t))
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Sequence != uint64(len(events)) {
		t.Fatalf("inspection watermark=%d event prefix=%d", inspection.Sequence, len(events))
	}
	var serviceKeys []string
	for _, event := range events {
		if event.Fiber == 0 {
			if event.Kind == "mount" || event.Kind == "state" || event.Name != "root" {
				t.Fatalf("host anchor fabricated plugin lifecycle event: %+v", event)
			}
			if event.Kind == "service" {
				serviceKeys = append(serviceKeys, keyText(event.Key))
				if event.State != Active {
					t.Fatalf("root service was not ACTIVE: %+v", event)
				}
			}
		}
	}
	if !slices.Equal(serviceKeys, []string{"zeta", "alpha"}) {
		t.Fatalf("root service notices = %v", serviceKeys)
	}
}

func TestReconcilerRootCloseJoinsChildrenUsersAndRootWorkBeforeLIFOCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewRecordingReconciler()
		root, err := r.(RootProvider).RootContext(rcGuard(t))
		if err != nil {
			t.Fatal(err)
		}
		var mu sync.Mutex
		var order []string
		appendOrder := func(label string) { mu.Lock(); order = append(order, label); mu.Unlock() }
		if err := root.Defer("first", func(context.Context) error { appendOrder("first"); return nil }); err != nil {
			t.Fatal(err)
		}
		if err := root.Provide("host", "retained"); err != nil {
			t.Fatal(err)
		}
		if err := root.Defer("last", func(ctx context.Context) error {
			if _, err := r.(Inspector).Inspect(ctx); err != nil {
				return err
			}
			appendOrder("last")
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		childEntered, userEntered := make(chan struct{}), make(chan struct{})
		childRelease, userRelease, workRelease := make(chan struct{}), make(chan struct{}), make(chan struct{})
		child, err := root.MountChild(Spec{ID: "structural-child", ApplyEffect: func(*Handle) (Disposer, error) {
			return func(context.Context) error { close(childEntered); <-childRelease; return nil }, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		user := rcMount(t, r, Spec{ID: "ordinary-user", Needs: []string{"host"}, ApplyEffect: func(c *Handle) (Disposer, error) {
			return func(context.Context) error {
				value, err := c.Read("host")
				if err != nil || value != "retained" {
					return errors.New("root released retained binding before user cleanup")
				}
				close(userEntered)
				<-userRelease
				return nil
			}, nil
		}})
		if err := root.Go("host-work", func(context.Context) error { <-workRelease; return nil }); err != nil {
			t.Fatal(err)
		}
		if !root.Enter() {
			t.Fatal("root lease rejected before Close")
		}
		rcSettle(t, r)
		closed := make(chan error, 1)
		go func() { closed <- r.Close(rcGuard(t)) }()
		<-childEntered
		<-userEntered
		if root.Std().Err() != context.Canceled || root.Enter() {
			t.Fatal("Close did not quiesce host activation")
		}
		if _, err := root.MountChild(Spec{}); !errors.Is(err, ErrInactive) {
			t.Fatalf("closing root accepted child: %v", err)
		}
		inspection := rcInspect(t, r)
		if len(inspection.Provisions) != 1 || inspection.Provisions[0].Active || inspection.Provisions[0].Value != "retained" || inspection.Provisions[0].Owner != 0 {
			t.Fatalf("root provision not retained during ordinary cleanup: %+v", inspection.Provisions)
		}
		mu.Lock()
		if len(order) != 0 {
			t.Errorf("root cleanup overtook children/users/work: %v", order)
		}
		mu.Unlock()
		close(childRelease)
		close(userRelease)
		synctest.Wait()
		if rcSnapshot(t, r, child).State != Disposed || rcSnapshot(t, r, user).State != Disposed {
			t.Fatal("Close failed to join ordinary fibers")
		}
		close(workRelease)
		synctest.Wait()
		mu.Lock()
		if len(order) != 0 {
			t.Errorf("root cleanup overtook explicit host lease: %v", order)
		}
		mu.Unlock()
		root.Leave()
		if err := <-closed; err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		if !slices.Equal(order, []string{"last", "first"}) {
			t.Errorf("host effect cleanup order = %v", order)
		}
		mu.Unlock()
		if root.Owns("host") || root.lease.Load() != 0 || !root.released.Load() {
			t.Fatal("root activation not actually released")
		}
		if _, err := r.(RootProvider).RootContext(rcGuard(t)); !errors.Is(err, ErrClosed) {
			t.Fatalf("RootContext after Close = %v", err)
		}
	})
}

func TestReconcilerRootIgnoredTrackedCancellationKeepsOwnerAlive(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewRecordingReconciler()
		root, err := r.(RootProvider).RootContext(rcGuard(t))
		if err != nil {
			t.Fatal(err)
		}
		var cleaned atomic.Bool
		if err := root.Defer("resource", func(context.Context) error { cleaned.Store(true); return nil }); err != nil {
			t.Fatal(err)
		}
		release := make(chan struct{})
		if err := root.Go("blocked", func(context.Context) error { <-release; return nil }); err != nil {
			t.Fatal(err)
		}
		before := time.Now()
		if err := r.Close(rcGuard(t)); !errors.Is(err, context.DeadlineExceeded) || time.Since(before) != testDrain {
			t.Fatalf("ignored root cancellation deadline = %v after %s", err, time.Since(before))
		}
		if cleaned.Load() || root.lease.Load() != 1 || root.released.Load() {
			t.Fatal("deadline fabricated host work completion")
		}
		if _, err := r.(Inspector).Inspect(rcGuard(t)); err != nil {
			t.Fatalf("owner exited with real host work outstanding: %v", err)
		}
		close(release)
		synctest.Wait()
		if err := r.Close(rcGuard(t)); err != nil {
			t.Fatal(err)
		}
		if !cleaned.Load() || !root.released.Load() {
			t.Fatal("actual host completion did not finish Close")
		}
	})
}

func TestReconcilerRootTrackedFailureRetiresOnlyOwnedLifetimeUntilExplicitClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewRecordingReconciler()
		root, err := r.(RootProvider).RootContext(rcGuard(t))
		if err != nil {
			t.Fatal(err)
		}
		if err := root.Provide("host", "retained"); err != nil {
			t.Fatal(err)
		}
		failure := &rcInspectingError{r: r}
		release := make(chan struct{})
		if err := root.Go("host-failure", func(context.Context) error { <-release; return failure }); err != nil {
			t.Fatal(err)
		}
		child, err := root.MountChild(Spec{ID: "child"})
		if err != nil {
			t.Fatal(err)
		}
		var consumerCleaned atomic.Bool
		consumer := rcMount(t, r, Spec{ID: "consumer", Needs: []string{"host"}, Apply: func(c *Handle) error {
			return c.Defer("consumer", func(context.Context) error {
				if _, err := c.Read("host"); err != nil {
					return err
				}
				consumerCleaned.Store(true)
				return nil
			})
		}})
		unrelated := rcMount(t, r, Spec{ID: "unrelated"})
		rcSettle(t, r)
		close(release)
		synctest.Wait()
		if !root.released.Load() || failure.calls.Load() != 1 || rcSnapshot(t, r, child).State != Disposed {
			t.Fatalf("root retirement leaked activation/formatted on owner: released=%v calls=%d child=%+v", root.released.Load(), failure.calls.Load(), rcSnapshot(t, r, child))
		}
		if s := rcSnapshot(t, r, consumer); s.State != Pending || s.Target != nil || !s.Alive || !consumerCleaned.Load() {
			t.Fatalf("root service loss did not clean dependent: %+v", s)
		}
		if s := rcSnapshot(t, r, unrelated); s.State != Active || !s.Alive {
			t.Fatalf("root failure stopped unrelated fiber: %+v", s)
		}
		if _, err := r.(RootProvider).RootContext(rcGuard(t)); !errors.Is(err, ErrInactive) {
			t.Fatalf("retired root capability = %v", err)
		}
		if err := r.Update(rcGuard(t), unrelated); err != nil {
			t.Fatalf("root failure closed ordinary command transport: %v", err)
		}
		if err := r.(Waiter).AwaitFiber(rcGuard(t), unrelated); err != nil {
			t.Fatal(err)
		}
		if s := rcSnapshot(t, r, unrelated); s.State != Active || s.Revision != 1 {
			t.Fatalf("unrelated fiber cannot update after root retirement: %+v", s)
		}
		if inspection := rcInspect(t, r); len(inspection.Provisions) != 0 {
			t.Fatalf("retired root retained provision records: %+v", inspection.Provisions)
		}
		if err := r.Close(rcGuard(t)); !errors.Is(err, failure) {
			t.Fatalf("retired host failure disappeared before explicit Close: %v", err)
		}
		if err := r.Close(rcGuard(t)); !errors.Is(err, failure) {
			t.Fatalf("repeat Close lost retained host failure: %v", err)
		}
	})
}

func TestReconcilerTrackedRecordsNativeOrderAndRetainsCleanupUntilExit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewRecordingReconciler()
		first, second := make(chan struct{}), make(chan struct{})
		var cleaned atomic.Bool
		contexts := make(chan *Handle, 1)
		id := rcMount(t, r, Spec{ID: "tracked", Apply: func(c *Handle) error {
			contexts <- c
			if err := c.Go("zeta-first", func(context.Context) error { <-first; return nil }); err != nil {
				return err
			}
			if err := c.Go("alpha-second", func(context.Context) error { <-second; return nil }); err != nil {
				return err
			}
			return c.Defer("resource", func(context.Context) error { cleaned.Store(true); return nil })
		}})
		rcSettle(t, r)
		c := <-contexts
		if s := rcSnapshot(t, r, id); !slices.Equal(s.Tracked, []string{"zeta-first", "alpha-second"}) || s.State != Active {
			t.Fatalf("native tracked creation order = %+v", s)
		}
		inspection := rcInspect(t, r)
		inspection.Fibers[0].Tracked[0] = "corrupt"
		if s := rcSnapshot(t, r, id); s.Tracked[0] != "zeta-first" {
			t.Fatal("mutable tracked list escaped inspection")
		}
		if err := r.Dispose(rcGuard(t), id); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if cleaned.Load() || c.Std().Err() != context.Canceled || c.lease.Load() != 2 {
			t.Fatalf("quiescence released live work: cleaned=%v leases=%d", cleaned.Load(), c.lease.Load())
		}
		if err := c.Go("late", func(context.Context) error { t.Error("late work launched"); return nil }); !errors.Is(err, ErrInactive) {
			t.Fatalf("quiescent tracked start = %v", err)
		}
		close(first)
		synctest.Wait()
		if s := rcSnapshot(t, r, id); !slices.Equal(s.Tracked, []string{"alpha-second"}) || cleaned.Load() || c.lease.Load() != 1 {
			t.Fatalf("record removed before actual final exit: %+v leases=%d cleaned=%v", s, c.lease.Load(), cleaned.Load())
		}
		close(second)
		synctest.Wait()
		rcSettle(t, r)
		if s := rcSnapshot(t, r, id); len(s.Tracked) != 0 || s.State != Disposed || !cleaned.Load() || c.lease.Load() != 0 {
			t.Fatalf("tracked cleanup did not drain: %+v", s)
		}
		events, err := r.Events(rcGuard(t))
		if err != nil {
			t.Fatal(err)
		}
		var ledger []string
		for _, event := range events {
			if event.Kind == "go:start" || event.Kind == "go:done" {
				ledger = append(ledger, event.Kind+":"+keyText(event.Key))
			}
		}
		if !slices.Equal(ledger, []string{"go:start:zeta-first", "go:start:alpha-second", "go:done:zeta-first", "go:done:alpha-second"}) {
			t.Fatalf("native tracked ledger = %v", ledger)
		}
		if err := r.Close(rcGuard(t)); err != nil {
			t.Fatal(err)
		}
	})
}

func TestReconcilerTrackedActiveErrorFailsAndInvalidatesConsumers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewRecordingReconciler()
		release := make(chan struct{})
		workerErr := &rcInspectingError{r: r}
		provider := rcMount(t, r, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Handle) error {
			if err := c.Go("failing", func(context.Context) error { <-release; return workerErr }); err != nil {
				return err
			}
			return c.Provide("sample", "retained")
		}})
		var consumerCleaned atomic.Bool
		consumer := rcMount(t, r, Spec{ID: "consumer", Needs: []string{"sample"}, Apply: func(c *Handle) error {
			return c.Defer("consumer", func(context.Context) error { consumerCleaned.Store(true); return nil })
		}})
		rcSettle(t, r)
		close(release)
		synctest.Wait()
		if err := r.(Waiter).AwaitFiber(rcGuard(t), provider); !errors.Is(err, workerErr) {
			t.Fatalf("tracked failure lost identity: %v", err)
		}
		if s := rcSnapshot(t, r, provider); s.State != Failed || s.Target != nil || len(s.Tracked) != 0 || !s.Alive || workerErr.calls.Load() != 1 {
			t.Fatalf("ACTIVE tracked failure = %+v Error calls=%d", s, workerErr.calls.Load())
		}
		if s := rcSnapshot(t, r, consumer); s.State != Pending || s.Target != nil || !consumerCleaned.Load() {
			t.Fatalf("tracked provider failure did not invalidate consumer closure: %+v", s)
		}
		if err := r.(Notifier).Notify(rcGuard(t), "sample"); err != nil {
			t.Fatal(err)
		}
		if s := rcSnapshot(t, r, provider); s.Target != nil || s.State != Failed {
			t.Fatalf("notice cleared sticky tracked failure: %+v", s)
		}
		if err := r.Dispose(rcGuard(t), provider); err != nil {
			t.Fatal(err)
		}
		rcSettle(t, r)
		if err := r.(Waiter).AwaitFiber(rcGuard(t), provider); !errors.Is(err, workerErr) {
			t.Fatalf("disposed tracked failure lost identity: %v", err)
		}
		if err := r.Close(rcGuard(t)); err != nil {
			t.Fatal(err)
		}
	})
}

func TestReconcilerTrackedFailureDuringApplyWaitsForSingleTransition(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewRecordingReconciler()
		applyEntered, releaseApply, releaseWork := make(chan struct{}), make(chan struct{}), make(chan struct{})
		contexts := make(chan *Handle, 1)
		workerErr := errors.New("worker failed while Apply running")
		var cleaned atomic.Bool
		id := rcMount(t, r, Spec{ID: "loading", ApplyEffect: func(c *Handle) (Disposer, error) {
			contexts <- c
			if err := c.Go("early-error", func(context.Context) error { <-releaseWork; return workerErr }); err != nil {
				return nil, err
			}
			close(applyEntered)
			<-releaseApply
			return func(context.Context) error { cleaned.Store(true); return nil }, nil
		}})
		<-applyEntered
		c := <-contexts
		close(releaseWork)
		synctest.Wait()
		if s := rcSnapshot(t, r, id); s.State != Loading || s.Target != nil || len(s.Tracked) != 0 || c.Std().Err() != context.Canceled || cleaned.Load() {
			t.Fatalf("tracked error raced in-flight Apply teardown: %+v", s)
		}
		if err := c.Defer("late", func(context.Context) error { return nil }); !errors.Is(err, ErrInactive) {
			t.Fatalf("failed LOADING context accepted effects: %v", err)
		}
		close(releaseApply)
		synctest.Wait()
		if err := r.(Waiter).AwaitFiber(rcGuard(t), id); !errors.Is(err, workerErr) {
			t.Fatalf("in-flight Apply overwrote tracked error: %v", err)
		}
		if s := rcSnapshot(t, r, id); s.State != Failed || !cleaned.Load() || s.Target != nil {
			t.Fatalf("tracked early failure did not unwind returned cleanup: %+v", s)
		}
		if err := r.Dispose(rcGuard(t), id); err != nil {
			t.Fatal(err)
		}
		rcSettle(t, r)
		if err := r.Close(rcGuard(t)); err != nil {
			t.Fatal(err)
		}
	})
}

func TestReconcilerTrackedSupersededErrorCannotPoisonNewTarget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewRecordingReconciler()
		release := make(chan struct{})
		var loads atomic.Int32
		id := rcMount(t, r, Spec{ID: "updated", Apply: func(c *Handle) error {
			if loads.Add(1) != 1 {
				return nil
			}
			return c.Go("old-worker", func(context.Context) error { <-release; return errors.New("obsolete worker failure") })
		}})
		rcSettle(t, r)
		if err := r.Update(rcGuard(t), id); err != nil {
			t.Fatal(err)
		}
		if s := rcSnapshot(t, r, id); s.Target == nil || s.Target.Revision != 1 || !slices.Equal(s.Tracked, []string{"old-worker"}) || s.State != Unloading {
			t.Fatalf("new target not retained while old worker drains: %+v", s)
		}
		close(release)
		synctest.Wait()
		rcSettle(t, r)
		if s := rcSnapshot(t, r, id); s.State != Active || s.Target == nil || s.Target.Revision != 1 || s.Error != "" || len(s.Tracked) != 0 || loads.Load() != 2 {
			t.Fatalf("old tracked completion poisoned target: %+v loads=%d", s, loads.Load())
		}
		if err := r.Close(rcGuard(t)); err != nil {
			t.Fatal(err)
		}
	})
}

func TestReconcilerTrackedIgnoredCancellationKeepsRecordAfterDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewRecordingReconciler()
		release := make(chan struct{})
		var cleaned atomic.Bool
		id := rcMount(t, r, Spec{ID: "blocked-worker", Apply: func(c *Handle) error {
			if err := c.Go("blocked", func(context.Context) error { <-release; return nil }); err != nil {
				return err
			}
			return c.Defer("resource", func(context.Context) error { cleaned.Store(true); return nil })
		}})
		rcSettle(t, r)
		if err := r.Close(rcGuard(t)); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("ignored tracked cancellation did not bound Close: %v", err)
		}
		if s := rcSnapshot(t, r, id); !slices.Equal(s.Tracked, []string{"blocked"}) || cleaned.Load() || s.State != Unloading {
			t.Fatalf("deadline fabricated tracked completion: %+v cleaned=%v", s, cleaned.Load())
		}
		close(release)
		synctest.Wait()
		if err := r.Close(rcGuard(t)); err != nil {
			t.Fatal(err)
		}
		if !cleaned.Load() {
			t.Fatal("real tracked completion did not finish pending Close")
		}
	})
}

func TestReconcilerTrackedCancellationPanicAndDeadlineClassification(t *testing.T) {
	for _, result := range []string{"canceled", "panic", "deadline"} {
		t.Run(result, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := NewRecordingReconciler()
				release := make(chan struct{})
				id := rcMount(t, r, Spec{ID: result, Apply: func(c *Handle) error {
					return c.Go(result, func(ctx context.Context) error {
						if result == "canceled" {
							<-ctx.Done()
							return context.Canceled
						}
						<-release
						if result == "panic" {
							panic("tracked panic")
						}
						return context.DeadlineExceeded
					})
				}})
				rcSettle(t, r)
				if result == "canceled" {
					if err := r.Dispose(rcGuard(t), id); err != nil {
						t.Fatal(err)
					}
					synctest.Wait()
					rcSettle(t, r)
					if s := rcSnapshot(t, r, id); s.Error != "" || s.State != Disposed || len(s.Tracked) != 0 {
						t.Fatalf("normal shutdown cancellation became failure: %+v", s)
					}
				} else {
					close(release)
					synctest.Wait()
					err := r.(Waiter).AwaitFiber(rcGuard(t), id)
					if err == nil || (result == "deadline" && !errors.Is(err, context.DeadlineExceeded)) {
						t.Fatalf("tracked %s lost failure: %v", result, err)
					}
					if s := rcSnapshot(t, r, id); s.State != Failed || s.Target != nil || s.Error == "" || len(s.Tracked) != 0 {
						t.Fatalf("tracked %s was not sticky failure: %+v", result, s)
					}
					if err := r.Dispose(rcGuard(t), id); err != nil {
						t.Fatal(err)
					}
					rcSettle(t, r)
				}
				if err := r.Close(rcGuard(t)); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func TestReconcilerTrackedAdmissionRaceLeavesNoReservationOrLease(t *testing.T) {
	r := rcRuntime(t)
	contexts := make(chan *Handle, 2)
	id := rcMount(t, r, Spec{ID: "admission", Apply: func(c *Handle) error { contexts <- c; return nil }})
	rcSettle(t, r)
	old := <-contexts
	start := make(chan struct{})
	var workers sync.WaitGroup
	var accepted, completed atomic.Int32
	for range 64 {
		workers.Go(func() {
			<-start
			err := old.Go("same-label", func(context.Context) error { completed.Add(1); return nil })
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrInactive) {
				t.Errorf("start race: %v", err)
			}
		})
	}
	close(start)
	if err := r.Update(rcGuard(t), id); err != nil {
		t.Fatal(err)
	}
	workers.Wait()
	rcSettle(t, r)
	<-contexts
	if accepted.Load() != completed.Load() || old.lease.Load() != 0 || !old.released.Load() {
		t.Fatalf("admitted work lost completion: admitted=%d completed=%d lease=%d released=%v", accepted.Load(), completed.Load(), old.lease.Load(), old.released.Load())
	}
	if s := rcSnapshot(t, r, id); s.State != Active || s.Target == nil || s.Target.Revision != 1 || len(s.Tracked) != 0 {
		t.Fatalf("admission left native record on replacement: %+v", s)
	}
}

func TestReconcilerWithdrawalValuePrecedesActualRemovalAndReload(t *testing.T) {
	r := rcRuntime(t)
	provider := rcMount(t, r, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Handle) error {
		return c.Provide("sample", "retained")
	}})
	rcMount(t, r, Spec{ID: "consumer", Needs: []string{"sample"}, Apply: func(*Handle) error { return nil }})
	rcSettle(t, r)
	if err := r.Update(rcGuard(t), provider); err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
	events, err := r.Events(rcGuard(t))
	if err != nil {
		t.Fatal(err)
	}
	withdraw, removed, reloading := -1, -1, -1
	for i, event := range events {
		if event.Fiber != provider {
			continue
		}
		if event.Kind == "service" && event.State == Unloading && withdraw < 0 {
			withdraw = i
			if event.Value != "retained" {
				t.Fatalf("withdrawal lost its real retained value: %+v", event)
			}
		}
		if event.Kind == "remove" && removed < 0 {
			removed = i
		}
		if event.Kind == "state" && event.From == Unloading && event.State == Loading && reloading < 0 {
			reloading = i
		}
	}
	if withdraw < 0 || removed <= withdraw || reloading <= removed {
		t.Fatalf("withdraw/remove/reload order = %d/%d/%d", withdraw, removed, reloading)
	}
}

func TestReconcilerTargetRevisionWhileCommittedCleanupIsGated(t *testing.T) {
	r := rcRuntime(t)
	rcMount(t, r, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Handle) error { return c.Provide("sample", "retained") }})
	cleanupEntered, releaseCleanup := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(releaseCleanup) })
	var loads atomic.Int32
	consumer := rcMount(t, r, Spec{ID: "consumer", Needs: []string{"sample"}, Check: func(*Handle) bool { return true }, Apply: func(c *Handle) error {
		if loads.Add(1) != 1 {
			return nil
		}
		return c.Defer("gated", func(context.Context) error {
			close(cleanupEntered)
			<-releaseCleanup
			return nil
		})
	}})
	rcSettle(t, r)
	before := rcSnapshot(t, r, consumer)
	if !before.Alive || before.Target == nil || before.Target.Revision != 0 || !slices.Equal(before.Target.UIDs, before.Epoch) {
		t.Fatalf("initial target = %+v, snapshot=%+v", before.Target, before)
	}
	if err := r.Update(rcGuard(t), consumer); err != nil {
		t.Fatal(err)
	}
	<-cleanupEntered
	for revision := uint64(1); revision <= 2; revision++ {
		if revision == 2 {
			if err := r.Update(rcGuard(t), consumer); err != nil {
				t.Fatal(err)
			}
		}
		s := rcSnapshot(t, r, consumer)
		if s.State != Unloading || !s.Alive || s.Target == nil || s.Target.Revision != revision || s.Revision != revision || !slices.Equal(s.Epoch, before.Epoch) || !slices.Equal(s.Target.UIDs, before.Epoch) {
			t.Fatalf("desired revision hidden by committed cleanup: %+v target=%+v", s, s.Target)
		}
		// Target tuples and committed lists are independent copies in every API.
		s.Target.UIDs[0] = 999
		s.Target.Revision = 999
		s.Epoch[0] = 999
		inspection := rcInspect(t, r)
		for _, snapshot := range inspection.Fibers {
			if snapshot.ID == consumer && (snapshot.Target == nil || snapshot.Target.Revision != revision || !slices.Equal(snapshot.Epoch, before.Epoch) || !slices.Equal(snapshot.Target.UIDs, before.Epoch)) {
				t.Fatalf("mutable target escaped into inspection: %+v", snapshot)
			}
		}
	}
	releaseOnce.Do(func() { close(releaseCleanup) })
	rcSettle(t, r)
	if s := rcSnapshot(t, r, consumer); s.State != Active || s.Target == nil || s.Target.Revision != 2 || loads.Load() != 2 {
		t.Fatalf("latest desired revision not committed: %+v loads=%d", s, loads.Load())
	}
}

func TestReconcilerNativeProvisionReservationsAndUIDOrder(t *testing.T) {
	r := rcRuntime(t)
	provided, releaseApply := make(chan struct{}), make(chan struct{})
	cleanupEntered, releaseCleanup := make(chan struct{}), make(chan struct{})
	var applyOnce, cleanupOnce sync.Once
	defer applyOnce.Do(func() { close(releaseApply) })
	defer cleanupOnce.Do(func() { close(releaseCleanup) })
	provider := rcMount(t, r, Spec{ID: "owner-name", Provides: []string{"alpha", "zeta"}, Apply: func(c *Handle) error {
		// Actual reservation order deliberately differs from key and declaration order.
		if err := c.Provide("zeta", "actual-zeta"); err != nil {
			return err
		}
		if err := c.Provide("alpha", uint64(9007199254740993)); err != nil {
			return err
		}
		if err := c.Defer("hold-provisions", func(context.Context) error {
			close(cleanupEntered)
			<-releaseCleanup
			return nil
		}); err != nil {
			return err
		}
		close(provided)
		<-releaseApply
		return nil
	}})
	<-provided
	inspection := rcInspect(t, r)
	if len(inspection.Provisions) != 2 {
		t.Fatalf("reserved provision records = %+v", inspection.Provisions)
	}
	first, second := inspection.Provisions[0], inspection.Provisions[1]
	if first.Key != "zeta" || second.Key != "alpha" || first.UID == 0 || first.UID >= second.UID || first.Owner != provider || second.Owner != provider || first.Name != "owner-name" || second.Name != "owner-name" || first.Active || second.Active || first.Value != "actual-zeta" || second.Value != uint64(9007199254740993) {
		t.Fatalf("reservation order/metadata = %+v", inspection.Provisions)
	}
	if s := rcSnapshot(t, r, provider); !s.Alive || s.State != Loading || s.Target == nil || s.Target.Revision != 0 || len(s.Target.UIDs) != 0 {
		t.Fatalf("initial no-Needs target must be [0]: %+v target=%+v", s, s.Target)
	}
	inspection.Provisions[0].Value = "corrupt"
	inspection.Provisions[0].Key = "corrupt"
	inspection.Provisions[0].UID = 999
	inspection.Fibers[0].Target.Revision = 999
	if fresh := rcInspect(t, r); fresh.Provisions[0] != first || fresh.Fibers[0].Target.Revision != 0 {
		t.Fatalf("inspection did not copy its lists: %+v", fresh)
	}
	applyOnce.Do(func() { close(releaseApply) })
	rcSettle(t, r)
	for _, entry := range rcInspect(t, r).Provisions {
		if !entry.Active {
			t.Fatalf("ACTIVE provider record not active: %+v", entry)
		}
	}
	if err := r.Dispose(rcGuard(t), provider); err != nil {
		t.Fatal(err)
	}
	<-cleanupEntered
	inspection = rcInspect(t, r)
	if len(inspection.Provisions) != 2 {
		t.Fatalf("retiring reservations removed before their effects: %+v", inspection.Provisions)
	}
	for _, entry := range inspection.Provisions {
		if entry.Active || entry.Owner != provider || entry.Name != "owner-name" {
			t.Fatalf("retiring entry lost ownership/availability: %+v", entry)
		}
	}
	if inspection.Provisions[0].Value != first.Value || inspection.Provisions[1].Value != second.Value {
		t.Fatalf("retiring provision value replaced: %+v", inspection.Provisions)
	}
	if s := rcSnapshot(t, r, provider); s.Alive || s.Target != nil || s.State != Unloading {
		t.Fatalf("Dispose must clear source UID/target before teardown: %+v", s)
	}
	cleanupOnce.Do(func() { close(releaseCleanup) })
	rcSettle(t, r)
	if fresh := rcInspect(t, r); len(fresh.Provisions) != 0 || len(fresh.Fibers) != 1 || fresh.Fibers[0].Alive || fresh.Fibers[0].Target != nil || fresh.Fibers[0].State != Disposed {
		t.Fatalf("retired inspection = %+v", fresh)
	}
}

func TestReconcilerInspectUsesCachedAvailabilityWithoutCallbacks(t *testing.T) {
	r := rcRuntime(t)
	rcMount(t, r, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Handle) error { return c.Provide("sample", "value") }})
	var allowed atomic.Bool
	var checks atomic.Int32
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	consumer := rcMount(t, r, Spec{ID: "consumer", Needs: []string{"sample"}, Check: func(c *Handle) bool {
		attempt := checks.Add(1)
		// Reenter inspection from the predicate. An owner-invoked predicate
		// would deadlock here; inspection cannot recursively invoke it either.
		if _, err := r.(Inspector).Inspect(c.Std()); err != nil {
			return false
		}
		if attempt == 3 {
			entered <- struct{}{}
			<-release
		}
		return allowed.Load()
	}})
	rcSettle(t, r)
	for range 3 {
		if s := rcSnapshot(t, r, consumer); s.State != Pending || !s.Alive || s.Target != nil {
			t.Fatalf("false availability target = %+v", s)
		}
		_ = rcInspect(t, r)
	}
	if checks.Load() != 1 {
		t.Fatalf("inspection ran availability callback %d times", checks.Load())
	}
	allowed.Store(true)
	if err := r.(Notifier).Notify(rcGuard(t), "sample"); err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
	before := rcSnapshot(t, r, consumer)
	if before.Target == nil || before.State != Active {
		t.Fatalf("successful notice did not publish target: %+v", before)
	}
	allowed.Store(false)
	if err := r.(Notifier).Notify(rcGuard(t), "sample"); err != nil {
		t.Fatal(err)
	}
	<-entered
	for range 3 {
		s := rcSnapshot(t, r, consumer)
		if s.Target == nil || !reflect.DeepEqual(s.Target, before.Target) || s.State != Active {
			t.Fatalf("in-flight notice discarded last checked target: %+v", s)
		}
		_ = rcInspect(t, r)
	}
	if checks.Load() != 3 {
		t.Fatalf("inspection retried in-flight availability: %d", checks.Load())
	}
	releaseOnce.Do(func() { close(release) })
	rcSettle(t, r)
	if s := rcSnapshot(t, r, consumer); s.State != Pending || s.Target != nil || !s.Alive {
		t.Fatalf("failed notice did not invalidate target: %+v", s)
	}
}

type rcInspectingError struct {
	r     Runtime
	calls atomic.Int32
}

func (e *rcInspectingError) Error() string {
	e.calls.Add(1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := e.r.(Inspector).Inspect(ctx); err != nil {
		return "error formatting could not reenter owner"
	}
	return "reentrant startup failure"
}

func TestReconcilerInspectNeverFormatsPluginErrorOnOwner(t *testing.T) {
	r := rcRuntime(t)
	startupErr := &rcInspectingError{r: r}
	id := rcMount(t, r, Spec{ID: "failed", Apply: func(*Handle) error { return startupErr }})
	if err := r.(Waiter).AwaitFiber(rcGuard(t), id); !errors.Is(err, startupErr) {
		t.Fatalf("Await lost startup error identity: %v", err)
	}
	for range 3 {
		if s := rcSnapshot(t, r, id); s.Error != "reentrant startup failure\nreentrant startup failure" || s.Target != nil {
			t.Fatalf("cached startup diagnostic = %+v", s)
		}
		_ = rcInspect(t, r)
		if err := r.Settle(rcGuard(t)); !errors.Is(err, startupErr) {
			t.Fatalf("Settle lost original startup cause: %v", err)
		}
	}
	if startupErr.calls.Load() != 1 {
		t.Fatalf("owner reformatted plugin error %d times", startupErr.calls.Load())
	}
	if err := r.Dispose(rcGuard(t), id); err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
	if s := rcSnapshot(t, r, id); s.Alive || s.Target != nil || s.State != Disposed || s.Error != "reentrant startup failure" {
		t.Fatalf("failed disposed history = %+v", s)
	}
	if err := r.(Waiter).AwaitFiber(rcGuard(t), id); !errors.Is(err, startupErr) {
		t.Fatalf("disposed Await lost startup error identity: %v", err)
	}
	if startupErr.calls.Load() != 1 {
		t.Fatal("retired inspection formatted plugin error")
	}
}

func TestReconcilerMissingDependencyAndActiveVisibility(t *testing.T) {
	r := rcRuntime(t)
	entered := make(chan *Handle, 1)
	consumer := rcMount(t, r, Spec{ID: "consumer", Needs: []string{"sample"}, Apply: func(c *Handle) error {
		entered <- c
		return nil
	}})
	rcSettle(t, r)
	if got := rcSnapshot(t, r, consumer); got.State != Pending || got.Target != nil || !got.Alive {
		t.Fatalf("missing dependency snapshot = %+v", got)
	}
	provided, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	provider := rcMount(t, r, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Handle) error {
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
	provider := rcMount(t, r, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Handle) error {
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
	consumerCtx := make(chan *Handle, 1)
	consumer := rcMount(t, r, Spec{ID: "consumer", Needs: []string{"sample"}, Apply: func(c *Handle) error {
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
	if got := rcSnapshot(t, r, consumer); got.State != Unloading || !got.Alive || got.Target != nil || len(got.Epoch) != 1 {
		t.Fatalf("provider loss must clear target but retain cleanup binding: %+v", got)
	}
	inspection := rcInspect(t, r)
	if len(inspection.Provisions) != 1 || inspection.Provisions[0].Active || inspection.Provisions[0].Owner != provider || inspection.Provisions[0].Name != "provider" || inspection.Provisions[0].Value != &providerDisposed {
		t.Fatalf("held retiring provision metadata = %+v", inspection.Provisions)
	}
	if s := rcSnapshot(t, r, provider); s.Alive || s.Target != nil {
		t.Fatalf("provider Dispose did not clear source liveness/target at request: %+v", s)
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
	provider := rcMount(t, r, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Handle) error {
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
	provider := rcMount(t, r, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Handle) error { return c.Provide("sample", 1) }})
	var attempts atomic.Int32
	id := rcMount(t, r, Spec{ID: "failing", Needs: []string{"sample"}, Apply: func(c *Handle) error {
		if attempts.Add(1) == 1 {
			return errors.New("sticky failure")
		}
		return nil
	}})
	if err := r.Settle(rcGuard(t)); err == nil {
		t.Fatal("failed Apply did not report error")
	}
	if got := rcSnapshot(t, r, id); got.State != Failed || got.Target != nil || !got.Alive {
		t.Fatalf("failed snapshot = %+v", got)
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
	if got := rcSnapshot(t, r, id); got.Target != nil || !got.Alive {
		t.Fatalf("provider notice revived sticky failed target: %+v", got)
	}
	if err := r.Update(rcGuard(t), id); err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
	if got := rcSnapshot(t, r, id); got.State != Active || got.Target == nil || got.Target.Revision != 1 || !slices.Equal(got.Target.UIDs, got.Epoch) {
		t.Fatalf("explicit Update failed to clear sticky target: %+v", got)
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
	id := rcMount(t, r, Spec{ID: "superseded", Apply: func(c *Handle) error {
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
	if s := rcSnapshot(t, r, id); s.State != Loading || s.Target == nil || s.Target.Revision != 32 || !s.Alive {
		t.Fatalf("new desired target hidden behind superseded load: %+v", s)
	}
	close(release)
	rcSettle(t, r)
	if attempts.Load() != 2 {
		t.Fatalf("coalesced attempts = %d, want 2", attempts.Load())
	}
	if s := rcSnapshot(t, r, id); s.State != Active || s.Revision != 32 || s.Error != "" || s.Target == nil || s.Target.Revision != 32 {
		t.Fatalf("new target poisoned by stale completion: %+v", s)
	}
}

func TestReconcilerSelfDisposeDoesNotAwaitOwnApply(t *testing.T) {
	r := rcRuntime(t)
	var cleaned atomic.Bool
	requested := make(chan error, 1)
	id := rcMount(t, r, Spec{ID: "self-dispose", Apply: func(c *Handle) error {
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

func TestReconcilerChildLoadsDuringParentLoadingAndIsJoinedAfterParentCleanup(t *testing.T) {
	r := rcRuntime(t)
	parentEntered, releaseParent := make(chan struct{}), make(chan struct{})
	childEntered, releaseChild := make(chan struct{}), make(chan struct{})
	var parentOnce, childOnce sync.Once
	defer parentOnce.Do(func() { close(releaseParent) })
	defer childOnce.Do(func() { close(releaseChild) })
	children := make(chan uint64, 1)
	contexts := make(chan *Handle, 1)
	var parentCleaned atomic.Bool
	parentCleanedCh := make(chan struct{})
	parent := rcMount(t, r, Spec{ID: "parent", ApplyEffect: func(c *Handle) (Disposer, error) {
		contexts <- c
		child, err := c.MountChild(Spec{ID: "child", ApplyEffect: func(*Handle) (Disposer, error) {
			return func(context.Context) error { close(childEntered); <-releaseChild; return nil }, nil
		}})
		if err != nil {
			return nil, err
		}
		children <- child
		close(parentEntered)
		<-releaseParent
		return func(context.Context) error { parentCleaned.Store(true); close(parentCleanedCh); return nil }, nil
	}})
	<-parentEntered
	child, old := <-children, <-contexts
	if err := r.(Waiter).AwaitFiber(rcGuard(t), child); err != nil {
		t.Fatal(err)
	}
	if s := rcSnapshot(t, r, parent); s.State != Loading {
		t.Fatalf("parent unexpectedly finished before child: %+v", s)
	}
	if s := rcSnapshot(t, r, child); s.State != Active {
		t.Fatalf("child could not load while parent LOADING: %+v", s)
	}
	parentOnce.Do(func() { close(releaseParent) })
	rcSettle(t, r)
	if err := r.Dispose(rcGuard(t), parent); err != nil {
		t.Fatal(err)
	}
	<-childEntered
	// Oracle order: the parent's own cleanup (registered after the child) runs
	// first; the child is invalidated and unloaded after it, and the parent stays
	// UNLOADING until the child is DISPOSED.
	<-parentCleanedCh
	if s := rcSnapshot(t, r, child); s.Alive || s.Target != nil || s.State != Unloading {
		t.Fatalf("parent teardown did not invalidate child: %+v", s)
	}
	if s := rcSnapshot(t, r, parent); s.State != Unloading {
		t.Fatalf("parent left UNLOADING before its child was DISPOSED: %+v", s)
	}
	if _, err := old.MountChild(Spec{ID: "late"}); !errors.Is(err, ErrInactive) {
		t.Fatalf("stale parent mounted a child: %v", err)
	}
	childOnce.Do(func() { close(releaseChild) })
	rcSettle(t, r)
	if !parentCleaned.Load() || rcSnapshot(t, r, parent).State != Disposed || rcSnapshot(t, r, child).State != Disposed {
		t.Fatal("structural disposal did not join both fibers")
	}
}

func TestReconcilerChildNeedsParentServiceWithoutDependencyDeadlock(t *testing.T) {
	r := rcRuntime(t)
	children := make(chan uint64, 1)
	seen := make(chan any, 1)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var parentCleaned atomic.Bool
	parent := rcMount(t, r, Spec{ID: "parent-provider", Provides: []string{"sample"}, ApplyEffect: func(c *Handle) (Disposer, error) {
		if err := c.Provide("sample", "parent-value"); err != nil {
			return nil, err
		}
		child, err := c.MountChild(Spec{ID: "child-consumer", Needs: []string{"sample"}, ApplyEffect: func(c *Handle) (Disposer, error) {
			value, err := c.Read("sample")
			if err != nil {
				return nil, err
			}
			seen <- value
			return func(context.Context) error {
				if _, err := c.Read("sample"); err != nil {
					return err
				}
				close(entered)
				<-release
				return nil
			}, nil
		}})
		if err != nil {
			return nil, err
		}
		children <- child
		return func(context.Context) error { parentCleaned.Store(true); return nil }, nil
	}})
	rcSettle(t, r)
	child := <-children
	if got := <-seen; got != "parent-value" {
		t.Fatalf("child binding = %v", got)
	}
	if err := r.Dispose(rcGuard(t), parent); err != nil {
		t.Fatal(err)
	}
	<-entered
	if parentCleaned.Load() {
		t.Fatal("parent cleanup overtook injecting child")
	}
	inspection := rcInspect(t, r)
	if len(inspection.Provisions) != 1 || inspection.Provisions[0].Value != "parent-value" || inspection.Provisions[0].Active {
		t.Fatalf("native provider record not retained during child cleanup: %+v", inspection.Provisions)
	}
	releaseOnce.Do(func() { close(release) })
	rcSettle(t, r)
	if !parentCleaned.Load() || rcSnapshot(t, r, child).State != Disposed {
		t.Fatal("parent/child service-dependency join deadlocked or leaked")
	}
}

func TestReconcilerChildPendingAndOldActivationCannotOwnReplacementChildren(t *testing.T) {
	r := rcRuntime(t)
	children := make(chan uint64, 2)
	contexts := make(chan *Handle, 2)
	parent := rcMount(t, r, Spec{ID: "parent", Apply: func(c *Handle) error {
		contexts <- c
		child, err := c.MountChild(Spec{ID: "pending-child", Needs: []string{"absent"}, Apply: func(*Handle) error { t.Error("missing-service child applied"); return nil }})
		if err != nil {
			return err
		}
		children <- child
		return nil
	}})
	rcSettle(t, r)
	old, first := <-contexts, <-children
	if rcSnapshot(t, r, parent).State != Active || rcSnapshot(t, r, first).State != Pending {
		t.Fatal("pending child incorrectly blocked parent activation")
	}
	if err := r.Update(rcGuard(t), parent); err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
	<-contexts
	second := <-children
	if rcSnapshot(t, r, first).State != Disposed || rcSnapshot(t, r, second).State != Pending {
		t.Fatal("replacement parent kept prior activation's child")
	}
	if _, err := old.MountChild(Spec{ID: "late"}); !errors.Is(err, ErrInactive) {
		t.Fatalf("released activation attached replacement child: %v", err)
	}
	if err := r.Dispose(rcGuard(t), parent); err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
	if rcSnapshot(t, r, second).State != Disposed {
		t.Fatal("pending child outlived its parent")
	}
}

func TestReconcilerChildOwnershipHasNoIdleWatchers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewRecordingReconciler()
		rcMount(t, r, Spec{ID: "parent", Apply: func(c *Handle) error {
			for range 16 {
				if _, err := c.MountChild(Spec{ID: "child"}); err != nil {
					return err
				}
			}
			return nil
		}})
		rcSettle(t, r)
		synctest.Wait()
		stacks := bubbleStacks(t)
		if strings.Count(stacks, ".(*owner).run(") != 1 || strings.Contains(stacks, ".(*owner).load.func") || strings.Contains(stacks, ".(*owner).disposeNext.func") {
			t.Fatal("idle structural graph retained transition/watcher goroutines")
		}
		if err := r.Close(rcGuard(t)); err != nil {
			t.Fatal(err)
		}
	})
}

func TestReconcilerChildCleanupHasBoundedParentJoin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewRecordingReconciler()
		entered, release := make(chan struct{}), make(chan struct{})
		parent := rcMount(t, r, Spec{ID: "parent", Apply: func(c *Handle) error {
			_, err := c.MountChild(Spec{ID: "child", ApplyEffect: func(*Handle) (Disposer, error) {
				return func(context.Context) error { close(entered); <-release; return nil }, nil
			}})
			return err
		}})
		rcSettle(t, r)
		if err := r.Dispose(rcGuard(t), parent); err != nil {
			t.Fatal(err)
		}
		<-entered
		if err := r.(Waiter).AwaitFiber(rcGuard(t), parent); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("ignored child cleanup did not bound parent join: %v", err)
		}
		close(release)
		synctest.Wait()
		_ = r.Settle(rcGuard(t))
		if err := r.Close(rcGuard(t)); err != nil {
			t.Fatal(err)
		}
	})
}

func TestReconcilerPendingDisposeHasUnloadingHistory(t *testing.T) {
	r := rcRuntime(t)
	id := rcMount(t, r, Spec{ID: "pending", Needs: []string{"absent"}, Apply: func(*Handle) error { t.Fatal("pending Apply ran"); return nil }})
	rcSettle(t, r)
	if err := r.Dispose(rcGuard(t), id); err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
	events, err := r.Events(rcGuard(t))
	if err != nil {
		t.Fatal(err)
	}
	var states []State
	for _, event := range events {
		if event.Fiber == id && event.Kind == "state" {
			states = append(states, event.State)
		}
	}
	if !slices.Equal(states, []State{Unloading, Disposed}) {
		t.Fatalf("pending disposal history = %v", states)
	}
	if s := rcSnapshot(t, r, id); s.Alive || s.Target != nil || s.State != Disposed {
		t.Fatalf("pending disposal metadata = %+v", s)
	}
}

func TestReconcilerApplyEffectOwnsReturnedCleanupAfterBodyProvisions(t *testing.T) {
	r := rcRuntime(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	id := rcMount(t, r, Spec{ID: "provider", Provides: []string{"sample"}, ApplyEffect: func(c *Handle) (Disposer, error) {
		if err := c.Provide("sample", "retained"); err != nil {
			return nil, err
		}
		return func(context.Context) error {
			close(entered)
			<-release
			return nil
		}, nil
	}})
	rcSettle(t, r)
	if err := r.Dispose(rcGuard(t), id); err != nil {
		t.Fatal(err)
	}
	<-entered
	inspection := rcInspect(t, r)
	if len(inspection.Provisions) != 1 || inspection.Provisions[0].Value != "retained" || inspection.Provisions[0].Active || inspection.Provisions[0].Owner != id {
		t.Fatalf("body provision removed before returned cleanup completed: %+v", inspection.Provisions)
	}
	if s := rcSnapshot(t, r, id); !slices.Equal(s.Effects, []string{"provide:sample", "apply"}) || s.Alive || s.Target != nil {
		t.Fatalf("returned disposer ownership = %+v", s)
	}
	releaseOnce.Do(func() { close(release) })
	rcSettle(t, r)
	if inspection := rcInspect(t, r); len(inspection.Provisions) != 0 {
		t.Fatalf("body provision leaked after returned cleanup: %+v", inspection.Provisions)
	}
}

func TestReconcilerApplyEffectCollectsSupersededAndSelfDisposeResults(t *testing.T) {
	for _, action := range []string{"self", "update"} {
		t.Run(action, func(t *testing.T) {
			r := rcRuntime(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			var loads, cleaned atomic.Int32
			id := rcMount(t, r, Spec{ID: "late-result", ApplyEffect: func(c *Handle) (Disposer, error) {
				if loads.Add(1) != 1 {
					return nil, nil
				}
				if action == "self" {
					if err := c.Dispose(); err != nil {
						return nil, err
					}
				}
				close(entered)
				<-release
				return func(context.Context) error { cleaned.Add(1); return nil }, nil
			}})
			<-entered
			if action == "update" {
				if err := r.Update(rcGuard(t), id); err != nil {
					t.Fatal(err)
				}
			}
			releaseOnce.Do(func() { close(release) })
			rcSettle(t, r)
			if cleaned.Load() != 1 {
				t.Fatalf("late returned cleanup ownership = %d", cleaned.Load())
			}
			if s := rcSnapshot(t, r, id); (action == "self" && (s.State != Disposed || s.Alive)) || (action == "update" && (s.State != Active || s.Target == nil || s.Target.Revision != 1)) {
				t.Fatalf("late-result lifecycle = %+v", s)
			}
		})
	}
}

func TestReconcilerApplyEffectRejectsAmbiguityAndFailedReturnedCleanup(t *testing.T) {
	r := rcRuntime(t)
	if _, err := r.Mount(rcGuard(t), Spec{Apply: func(*Handle) error { return nil }, ApplyEffect: func(*Handle) (Disposer, error) { return nil, nil }}); err == nil {
		t.Fatal("accepted both Apply and ApplyEffect")
	}
	var cleaned atomic.Bool
	startupErr := errors.New("failed return")
	id := rcMount(t, r, Spec{ID: "failed-effect", ApplyEffect: func(*Handle) (Disposer, error) {
		return func(context.Context) error { cleaned.Store(true); return nil }, startupErr
	}})
	if err := r.(Waiter).AwaitFiber(rcGuard(t), id); !errors.Is(err, startupErr) {
		t.Fatalf("ApplyEffect failure lost: %v", err)
	}
	if cleaned.Load() {
		t.Fatal("failed ApplyEffect transferred ownership of returned cleanup")
	}
	if err := r.Dispose(rcGuard(t), id); err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
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
	id := rcMount(t, r, Spec{ID: "partial", Apply: func(c *Handle) error {
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
	ctxs := make(chan *Handle, 2)
	id := rcMount(t, r, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Handle) error {
		ctxs <- c
		if err := c.Provide("undeclared", nil); err == nil {
			return errors.New("undeclared provision accepted")
		}
		if c.Owns("undeclared") || c.Owns("sample") {
			return errors.New("Owns invented a provision reservation")
		}
		if err := c.Provide("sample", "value"); err != nil {
			return err
		}
		if !c.Owns("sample") {
			return errors.New("successful provision not published to Owns")
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
	if old.Owns("sample") {
		t.Fatal("released activation retained provision ownership")
	}
}

func TestReconcilerLeaseDrainAndSnapshotIsolation(t *testing.T) {
	r := rcRuntime(t)
	ctxs := make(chan *Handle, 2)
	var disposed atomic.Bool
	id := rcMount(t, r, Spec{ID: "leased", Apply: func(c *Handle) error {
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
		r := NewRecordingReconciler()
		contexts := make(map[string]chan *Handle)
		var ids []uint64
		for _, spec := range []Spec{
			{ID: "root", Provides: []string{"root"}},
			{ID: "left", Needs: []string{"root"}, Provides: []string{"left"}},
			{ID: "right", Needs: []string{"root"}, Provides: []string{"right"}},
			{ID: "leaf", Needs: []string{"left", "right"}, Provides: []string{"leaf"}},
			{ID: "tail", Needs: []string{"leaf"}},
		} {
			captured := make(chan *Handle, 2)
			contexts[spec.ID] = captured
			provides := slices.Clone(spec.Provides)
			spec.Apply = func(c *Handle) error {
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
		old := make([]*Handle, 0, len(ids))
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
		stacks := bubbleStacks(t)
		if got := strings.Count(stacks, ".(*owner).run("); got != 1 {
			t.Fatalf("reconciler owner stack count in this bubble = %d", got)
		}
		if strings.Contains(stacks, ".(*owner).load.func") || strings.Contains(stacks, ".(*owner).disposeNext.func") {
			t.Fatal("idle activation retains a transition goroutine")
		}
		if err := r.Close(rcGuard(t)); err != nil {
			t.Fatal(err)
		}
	})
}

func TestReconcilerConcurrentLeaseQuiesceAndRequestWake(t *testing.T) {
	r := rcRuntime(t)
	contexts := make(chan *Handle, 2)
	id := rcMount(t, r, Spec{ID: "race", Apply: func(c *Handle) error {
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
	id := rcMount(t, r, Spec{ID: "outer", Apply: func(c *Handle) error {
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
	r := NewRecordingReconciler()
	entered, release := make(chan struct{}), make(chan struct{})
	id := rcMount(t, r, Spec{ID: "blocked", Apply: func(*Handle) error { close(entered); <-release; return nil }})
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
	if err := closeRuntime(rcGuard(t), r); err != nil {
		t.Fatal(err)
	}
	if err := closeRuntime(rcGuard(t), r); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Mount(rcGuard(t), Spec{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Mount after Close = %v", err)
	}
}

func TestReconcilerAwaitRetainsFailedErrorAfterDisposal(t *testing.T) {
	r := rcRuntime(t)
	startupErr := errors.New("retained startup failure")
	id := rcMount(t, r, Spec{ID: "failed", Apply: func(*Handle) error { return startupErr }})
	if err := r.(Waiter).AwaitFiber(rcGuard(t), id); !errors.Is(err, startupErr) {
		t.Fatalf("initial AwaitFiber lost error identity: %v", err)
	}
	if err := r.Dispose(rcGuard(t), id); err != nil {
		t.Fatalf("Dispose request surfaced startup error: %v", err)
	}
	if err := r.(Waiter).AwaitFiber(rcGuard(t), id); !errors.Is(err, startupErr) {
		t.Fatalf("disposed AwaitFiber lost error identity: %v", err)
	}
	if s := rcSnapshot(t, r, id); s.State != Disposed || s.Error != startupErr.Error() || len(s.Effects) != 0 || s.Alive || s.Target != nil {
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
	r := NewRecordingReconciler()
	cleanupErr := errors.New("cleanup diagnostic")
	id := rcMount(t, r, Spec{ID: "cleanup", Apply: func(c *Handle) error { return c.Defer("cleanup", func(context.Context) error { return cleanupErr }) }})
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
	if err := joinOwner(rcGuard(t), r); err != nil {
		t.Fatal(err)
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
	unrelated := rcMount(t, r, Spec{ID: "blocked", Apply: func(*Handle) error { close(entered); <-release; return nil }})
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

func TestReconcilerConsumerStatePrecedesProviderServiceNotice(t *testing.T) {
	r := rcRuntime(t)
	consumer := rcMount(t, r, Spec{ID: "consumer", Needs: []string{"sample"}})
	provider := rcMount(t, r, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Handle) error { return c.Provide("sample", "value") }})
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
	rcMount(t, r, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Handle) error { return c.Provide("sample", "value") }})
	var allowed atomic.Bool
	var checks, applies atomic.Int32
	first := rcMount(t, r, Spec{ID: "allowed", Needs: []string{"sample"}, Check: func(c *Handle) bool {
		v, err := c.Read("sample")
		return err == nil && v == "value"
	}})
	second := rcMount(t, r, Spec{ID: "gated", Needs: []string{"sample"}, Check: func(c *Handle) bool {
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
	}, Apply: func(*Handle) error { applies.Add(1); return nil }})
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
	provider := rcMount(t, r, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Handle) error {
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
	consumer := rcMount(t, r, Spec{ID: "consumer", Needs: []string{"sample"}, Check: func(c *Handle) bool {
		close(entered)
		<-release
		value, err := c.Read("sample")
		return err == nil && !value.(*atomic.Bool).Load()
	}, Apply: func(*Handle) error { applies.Add(1); return nil }})
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
	id := rcMount(t, r, Spec{ID: "predicate", Check: func(*Handle) bool {
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
		r := NewRecordingReconciler()
		firstEntered, firstRelease := make(chan struct{}), make(chan struct{})
		laterEntered, laterRelease := make(chan struct{}), make(chan struct{})
		id := rcMount(t, r, Spec{ID: "cleanup-chain", Apply: func(c *Handle) error {
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
				r := NewRecordingReconciler()
				contexts := make(chan *Handle, 1)
				blocked, release := make(chan struct{}), make(chan struct{})
				id := rcMount(t, r, Spec{ID: "deadline", Apply: func(c *Handle) error {
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

// An owner running outside the bubble (as a leftover of an earlier test would be)
// must not be counted, and a second owner inside it must be.
func TestBubbleStacksCountsOnlyThisBubblesOwners(t *testing.T) {
	outside := rcRuntime(t)
	rcSettle(t, outside)
	synctest.Test(t, func(t *testing.T) {
		first := NewRecordingReconciler()
		synctest.Wait()
		if got := strings.Count(bubbleStacks(t), ".(*owner).run("); got != 1 {
			t.Fatalf("owners in bubble with one runtime and one outside = %d, want 1", got)
		}
		// The process-wide dump the two idle-worker tests used to count sees both.
		all := make([]byte, 1<<20)
		if got := bytes.Count(all[:runtime.Stack(all, true)], []byte(".(*owner).run(")); got < 2 {
			t.Fatalf("process-wide dump saw %d owners, want the outside one too", got)
		}
		second := NewRecordingReconciler()
		synctest.Wait()
		if got := strings.Count(bubbleStacks(t), ".(*owner).run("); got != 2 {
			t.Fatalf("owners in bubble with two runtimes = %d, want 2", got)
		}
		for _, r := range []Runtime{first, second} {
			if err := closeRuntime(rcGuard(t), r); err != nil {
				t.Fatal(err)
			}
		}
	})
}
