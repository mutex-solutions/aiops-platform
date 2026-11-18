// SPDX-License-Identifier: Apache-2.0

package concurrency

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func scopeUnitBudget() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

// These collector-only tests exercise the actual shared work object, not a
// substitute runtime or control owner. Native scheduling is tested below.
func TestScopeCollectorRollbackOrderAndPanicIdentity(t *testing.T) {
	for _, panics := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "panic"}[panics], func(t *testing.T) {
			setupErr, disposeErr, panicErr := errors.New("setup"), errors.New("dispose"), errors.New("panic")
			var order []string
			var captured *EffectScope
			e := newScopeEffect("root", func(s *EffectScope) error {
				captured = s
				for _, label := range []string{"first", "second", "third", "fourth"} {
					if err := s.Defer(label, func(context.Context) error {
						order = append(order, label)
						if label == "third" {
							panic(panicErr)
						}
						if label == "second" {
							return disposeErr
						}
						return nil
					}); err != nil {
						return err
					}
				}
				if panics {
					panic(setupErr)
				}
				return setupErr
			})
			var budgets int
			err := e.runSetup(context.Background(), func() (context.Context, context.CancelFunc) {
				budgets++
				return scopeUnitBudget()
			})
			if !errors.Is(err, setupErr) || !errors.Is(err, disposeErr) || !errors.Is(err, panicErr) {
				t.Fatalf("lost setup/cleanup identity: %v", err)
			}
			var panicValue *ScopePanicError
			if !errors.As(err, &panicValue) || (panicValue.Value != panicErr && panicValue.Value != setupErr) {
				t.Fatalf("lost typed panic value: %#v", panicValue)
			}
			if budgets != 1 || !reflect.DeepEqual(order, []string{"fourth", "third", "second", "first"}) {
				t.Fatalf("budgets/order = %d/%v", budgets, order)
			}
			if !e.setupFinished() || !e.cleanupFinished() || e.metadata().State != ScopeDone {
				t.Fatal("failed setup did not publish actual rollback completion")
			}
			if !errors.Is(e.handle.AwaitSetup(context.Background()), setupErr) || !errors.Is(e.handle.Await(context.Background()), disposeErr) {
				t.Fatal("completion handles lost immutable errors")
			}
			if !errors.Is(captured.Defer("late", func(context.Context) error { return nil }), ErrScopeSealed) {
				t.Fatal("late disposer accepted after setup sealed")
			}
			if !errors.Is(captured.Scope("late", func(*EffectScope) error { return nil }), ErrScopeSealed) {
				t.Fatal("late nested setup accepted after setup sealed")
			}
			if !errors.Is(e.runSetup(context.Background(), scopeUnitBudget), ErrScopeStarted) {
				t.Fatal("duplicate setup was accepted")
			}
			if !errors.Is(e.runCleanup(context.Background()), disposeErr) || len(order) != 4 {
				t.Fatal("duplicate cleanup reran callbacks or changed its result")
			}
		})
	}
}

func TestScopeCollectorNestedRollbackOneBudgetAndDetachedTree(t *testing.T) {
	setupErr, childErr := errors.New("root setup"), errors.New("child setup")
	var order []string
	var budgets int
	var saved EffectMeta
	e := newScopeEffect("root", func(s *EffectScope) error {
		if err := s.Defer("first", func(context.Context) error { order = append(order, "first"); return nil }); err != nil {
			return err
		}
		if err := s.Scope("nested", func(child *EffectScope) error {
			if err := child.Defer("nested resource", func(context.Context) error { order = append(order, "nested"); return nil }); err != nil {
				return err
			}
			saved = s.effect.handle.Metadata()
			return childErr
		}); !errors.Is(err, childErr) {
			return errors.New("nested setup error was lost")
		}
		if err := s.Defer("last", func(context.Context) error { order = append(order, "last"); return nil }); err != nil {
			return err
		}
		return setupErr
	})
	if saved.State != "" || e.metadata().State != ScopePending {
		t.Fatal("setup ran before work admission")
	}
	if err := e.runSetup(context.Background(), func() (context.Context, context.CancelFunc) {
		budgets++
		return scopeUnitBudget()
	}); !errors.Is(err, setupErr) {
		t.Fatal(err)
	}
	if budgets != 1 || !reflect.DeepEqual(order, []string{"nested", "last", "first"}) {
		t.Fatalf("rollback budgets/order = %d/%v", budgets, order)
	}
	if len(saved.Children) != 2 || saved.Children[1].State != ScopeSetup || saved.Children[1].Children[0].Label != "nested resource" {
		t.Fatalf("missing actual nested setup ownership: %+v", saved)
	}
	if saved.State != ScopeSetup || saved.Children[0].State != ScopeReady {
		t.Fatalf("old snapshot changed with later rollback: %+v", saved)
	}
	saved.Children[1].Children[0].Label = "caller mutation"
	if e.metadata().Children[1].Children[0].Label != "nested resource" {
		t.Fatal("returned metadata aliases collector publication")
	}
}

func TestScopeCollectorLateResourcesAndHeldCleanupRemainOwned(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var captured *EffectScope
	e := newScopeEffect("root", func(s *EffectScope) error {
		captured = s
		cancel()
		return s.Defer("late acquisition", func(got context.Context) error {
			if got != ctx {
				return errors.New("cleanup context was replaced")
			}
			close(entered)
			<-release // Intentionally ignores cancellation; actual return owns completion.
			return nil
		})
	})
	if err := e.runSetup(ctx, scopeUnitBudget); err != nil {
		t.Fatal(err)
	}
	if captured == nil || len(e.metadata().Children) != 1 {
		t.Fatal("cancellation discarded a resource acquired before setup sealed")
	}
	go func() { finished <- e.runCleanup(ctx) }()
	<-entered
	if e.cleanupFinished() || e.metadata().State != ScopeCleaning || e.metadata().Children[0].State != ScopeCleaning {
		close(release)
		t.Fatal("cancellation falsely released real cleanup ownership")
	}
	if !errors.Is(e.handle.Await(ctx), context.Canceled) || e.cleanupFinished() {
		close(release)
		t.Fatal("canceled waiter changed actual work completion")
	}
	close(release)
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("cleanup cancellation not retained: %v", err)
	}
	if !e.cleanupFinished() || e.metadata().State != ScopeDone {
		t.Fatal("actual cleanup return did not complete scope")
	}
}

func TestScopeCollectorPendingCancellationAndUnavailableHandles(t *testing.T) {
	var called atomic.Bool
	e := newScopeEffect("not started", func(*EffectScope) error { called.Store(true); return nil })
	if !e.cancelPending(nil) || e.cancelPending(nil) || called.Load() {
		t.Fatal("pending cancellation ran setup or was not single-shot")
	}
	if !errors.Is(e.handle.AwaitSetup(context.Background()), ErrInactiveEffect) || !errors.Is(e.handle.Await(context.Background()), ErrInactive) {
		t.Fatal("pending cancellation lost inactive identity")
	}
	if !e.setupFinished() || !e.cleanupFinished() || e.metadata().State != ScopeDone {
		t.Fatal("empty canceled scope was not completed")
	}
	if !errors.Is(e.handle.RequestWithdraw(), ErrScopeUnavailable) {
		t.Fatal("unbound handle invented local withdrawal authority")
	}
	var nilHandle *EffectHandle
	if !errors.Is(nilHandle.AwaitSetup(context.Background()), ErrScopeUnavailable) || !errors.Is(nilHandle.Await(context.Background()), ErrScopeUnavailable) || !errors.Is(nilHandle.RequestWithdraw(), ErrScopeUnavailable) {
		t.Fatal("nil handle did not report unavailable native work")
	}
	if nilHandle.Metadata().Label != "" {
		t.Fatal("nil handle fabricated ownership metadata")
	}
	unavailable := newScopeEffect("missing budget", func(*EffectScope) error { called.Store(true); return nil })
	if !errors.Is(unavailable.runSetup(context.Background(), nil), ErrScopeUnavailable) || called.Load() {
		t.Fatal("missing native cleanup bridge ran unowned setup")
	}
	if !errors.Is(unavailable.runCleanup(context.Background()), ErrScopeUnavailable) {
		t.Fatal("unavailable setup changed its cleanup result")
	}
}

func TestScopeCollectorReleasesCompletedCallbackReferences(t *testing.T) {
	e := newScopeEffect("retained handle", func(s *EffectScope) error {
		return s.Scope("nested", func(child *EffectScope) error {
			return child.Defer("resource", func(context.Context) error { return nil })
		})
	})
	if err := e.runSetup(context.Background(), scopeUnitBudget); err != nil {
		t.Fatal(err)
	}
	if e.setup != nil || e.root.children[0].children[0].dispose == nil {
		t.Fatal("setup retained its callback or resource ownership was released early")
	}
	if err := e.runCleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if e.root.children[0].children[0].dispose != nil || e.handle.Metadata().Children[0].Children[0].Label != "resource" {
		t.Fatal("completed callback retained resource closure or lost actual metadata")
	}
	pending := newScopeEffect("canceled pending", func(*EffectScope) error { return nil })
	if !pending.cancelPending(nil) || pending.setup != nil {
		t.Fatal("canceled pending scope retained its never-run callback")
	}
}

func scopeRuntimeTest(t *testing.T, run func(*testing.T, context.Context, Runtime)) {
	t.Helper()
	for name, makeRuntime := range map[string]func() Runtime{"reconciler": NewReconciler, "graph-lock": NewGraphLock} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r := makeRuntime()
			defer r.Close(ctx)
			run(t, ctx, r)
		})
	}
}

func scopeReceive[T any](t *testing.T, ctx context.Context, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-ctx.Done():
		t.Fatal("real scope work did not reach its synchronization point")
		var zero T
		return zero
	}
}

func scopeFiber(t *testing.T, ctx context.Context, r Runtime, id uint64) Snapshot {
	t.Helper()
	values, err := r.Snapshots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range values {
		if value.ID == id {
			return value
		}
	}
	t.Fatalf("missing fiber %d", id)
	return Snapshot{}
}

func TestNativeScopeRollbackIsHandleLocalAndActualTree(t *testing.T) {
	scopeRuntimeTest(t, func(t *testing.T, ctx context.Context, r Runtime) {
		setupErr, cleanupErr := errors.New("setup sentinel"), errors.New("cleanup sentinel")
		handles := make(chan *EffectHandle, 1)
		entered, release := make(chan struct{}), make(chan struct{})
		var releaseOnce sync.Once
		defer releaseOnce.Do(func() { close(release) })
		var orderMu sync.Mutex
		var order []string
		appendOrder := func(value string) { orderMu.Lock(); order = append(order, value); orderMu.Unlock() }
		id, err := r.Mount(ctx, Spec{ID: "scope rollback", Apply: func(c *Context) error {
			h, err := c.RequestEffectScope("actual scope", func(s *EffectScope) error {
				if err := s.Defer("first", func(context.Context) error { appendOrder("first"); return cleanupErr }); err != nil {
					return err
				}
				if err := s.Scope("nested", func(child *EffectScope) error {
					return child.Defer("second", func(context.Context) error {
						close(entered)
						<-release
						appendOrder("second")
						return nil
					})
				}); err != nil {
					return err
				}
				return setupErr
			})
			if err == nil {
				handles <- h
			}
			return err
		}})
		if err != nil {
			t.Fatal(err)
		}
		h := scopeReceive(t, ctx, handles)
		scopeReceive(t, ctx, entered)
		pending := scopeFiber(t, ctx, r, id)
		if len(pending.EffectTree) != 1 || pending.EffectTree[0].Label != "actual scope" || pending.EffectTree[0].State != ScopeCleaning || len(pending.EffectTree[0].Children) != 2 || pending.EffectTree[0].Children[1].Children[0].Label != "second" {
			t.Fatalf("rollback lost actual nested ownership: %+v", pending.EffectTree)
		}
		if h.effect.setupFinished() || h.effect.cleanupFinished() {
			t.Fatal("handle completed before held rollback actually returned")
		}
		pending.EffectTree[0].Children[1].Children[0].Label = "caller mutation"
		if h.Metadata().Children[1].Children[0].Label != "second" {
			t.Fatal("native snapshot aliases scope metadata")
		}
		releaseOnce.Do(func() { close(release) })
		if err := h.AwaitSetup(ctx); !errors.Is(err, setupErr) || !errors.Is(err, cleanupErr) {
			t.Fatalf("setup completion lost rollback diagnostics: %v", err)
		}
		if err := h.Await(ctx); !errors.Is(err, cleanupErr) {
			t.Fatalf("cleanup completion lost identity: %v", err)
		}
		if err := r.Settle(ctx); err != nil {
			t.Fatal(err)
		}
		current := scopeFiber(t, ctx, r, id)
		if current.State != Active || len(current.EffectTree) != 0 {
			t.Fatalf("handled setup error failed fiber or retained phantom slot: %+v", current)
		}
		orderMu.Lock()
		defer orderMu.Unlock()
		if !reflect.DeepEqual(order, []string{"second", "first"}) {
			t.Fatalf("rollback order = %v", order)
		}
	})
}

func TestNativeScopeRequestsInsideApplyStayOnOneLane(t *testing.T) {
	scopeRuntimeTest(t, func(t *testing.T, ctx context.Context, r Runtime) {
		contexts := make(chan *Context, 1)
		handles := make(chan []*EffectHandle, 1)
		var applyReturned atomic.Bool
		var callbacks, overlap, acquired, disposed atomic.Int64
		id, err := r.Mount(ctx, Spec{ID: "same lane", Apply: func(c *Context) error {
			contexts <- c
			defer applyReturned.Store(true)
			var accepted []*EffectHandle
			for range 2 {
				h, err := c.RequestEffectScope("same label", func(s *EffectScope) error {
					running := callbacks.Add(1)
					if !applyReturned.Load() || running != 1 {
						overlap.Add(1)
					}
					defer callbacks.Add(-1)
					acquired.Add(1)
					return s.Defer("resource", func(context.Context) error {
						if callbacks.Add(1) != 1 {
							overlap.Add(1)
						}
						defer callbacks.Add(-1)
						disposed.Add(1)
						return nil
					})
				})
				if err != nil {
					return err
				}
				accepted = append(accepted, h)
			}
			if err := accepted[0].RequestWithdraw(); err != nil {
				return err
			}
			if err := accepted[0].RequestWithdraw(); err != nil {
				return err
			}
			handles <- accepted
			return nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		c := scopeReceive(t, ctx, contexts)
		hs := scopeReceive(t, ctx, handles)
		if hs[0] == hs[1] || hs[0].effect == hs[1].effect {
			t.Fatal("same-label scopes share native identity")
		}
		for _, h := range hs {
			if err := h.AwaitSetup(ctx); err != nil {
				t.Fatal(err)
			}
		}
		if err := hs[0].Await(ctx); err != nil {
			t.Fatal(err)
		}
		if err := r.Settle(ctx); err != nil {
			t.Fatal(err)
		}
		state := scopeFiber(t, ctx, r, id)
		if state.State != Active || len(state.EffectTree) != 1 || acquired.Load() != 2 || disposed.Load() != 1 || overlap.Load() != 0 {
			t.Fatalf("lane/withdraw/identity violation: state=%+v acquired=%d disposed=%d overlap=%d", state, acquired.Load(), disposed.Load(), overlap.Load())
		}
		if err := r.Dispose(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := hs[1].RequestWithdraw(); err != nil {
			t.Fatal(err)
		}
		if err := hs[1].Await(ctx); err != nil {
			t.Fatal(err)
		}
		if err := r.Settle(ctx); err != nil {
			t.Fatal(err)
		}
		if disposed.Load() != 2 || overlap.Load() != 0 {
			t.Fatalf("unload/manual cleanup duplicated: disposed=%d overlap=%d", disposed.Load(), overlap.Load())
		}
		if _, err := c.RequestEffectScope("stale", func(*EffectScope) error { t.Error("stale setup ran"); return nil }); !errors.Is(err, ErrInactiveEffect) || !errors.Is(err, ErrInactive) {
			t.Fatalf("stale activation accepted request: %v", err)
		}
	})
}

func TestNativeScopeHeldCleanupRetainsTreeAndProvider(t *testing.T) {
	scopeRuntimeTest(t, func(t *testing.T, ctx context.Context, r Runtime) {
		var providerCleaned atomic.Bool
		provider, err := r.Mount(ctx, Spec{ID: "scope provider", Provides: []string{"scope service"}, Apply: func(c *Context) error {
			if err := c.Defer("provider resource", func(context.Context) error { providerCleaned.Store(true); return nil }); err != nil {
				return err
			}
			return c.Provide("scope service", "committed")
		}})
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Settle(ctx); err != nil {
			t.Fatal(err)
		}
		handles := make(chan *EffectHandle, 1)
		entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var releaseOnce sync.Once
		defer releaseOnce.Do(func() { close(release) })
		consumer, err := r.Mount(ctx, Spec{ID: "scope consumer", Needs: []string{"scope service"}, Apply: func(c *Context) error {
			h, err := c.RequestEffectScope("provider-bound scope", func(s *EffectScope) error {
				return s.Defer("held resource", func(cleanup context.Context) error {
					close(entered)
					<-cleanup.Done()
					close(canceled)
					<-release
					value, err := c.Read("scope service")
					if err != nil || value != "committed" || providerCleaned.Load() {
						return errors.New("provider released before actual scope cleanup")
					}
					return nil
				})
			})
			if err == nil {
				handles <- h
			}
			return err
		}})
		if err != nil {
			t.Fatal(err)
		}
		h := scopeReceive(t, ctx, handles)
		if err := h.AwaitSetup(ctx); err != nil {
			t.Fatal(err)
		}
		if err := r.Dispose(ctx, provider); err != nil {
			t.Fatal(err)
		}
		scopeReceive(t, ctx, entered)
		scopeReceive(t, ctx, canceled) // The existing native disposal budget, not a test knob.
		state := scopeFiber(t, ctx, r, consumer)
		if len(state.EffectTree) != 1 || state.EffectTree[0].State != ScopeCleaning || providerCleaned.Load() || h.effect.cleanupFinished() {
			t.Fatalf("deadline falsely released ownership: consumer=%+v providerCleaned=%v", state, providerCleaned.Load())
		}
		waitCtx, cancel := context.WithCancel(ctx)
		cancel()
		if !errors.Is(h.Await(waitCtx), context.Canceled) {
			t.Fatal("held cleanup completed at cancellation rather than actual return")
		}
		releaseOnce.Do(func() { close(release) })
		if err := h.Await(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("native disposal deadline not retained after actual return: %v", err)
		}
		_ = r.Settle(ctx) // The native runtime may also report its retained deadline.
		if !providerCleaned.Load() || !h.effect.cleanupFinished() {
			t.Fatal("provider did not drain after scope really returned")
		}
	})
}

func TestScopeRequestValidationWithoutNativeBridge(t *testing.T) {
	c := &Context{}
	var setup atomic.Bool
	callback := func(*EffectScope) error { setup.Store(true); return nil }
	if _, err := c.RequestEffectScope("", callback); err == nil || errors.Is(err, ErrScopeUnavailable) {
		t.Fatal("empty label was not rejected before native capability lookup")
	}
	if _, err := c.RequestEffectScope("label", nil); err == nil || errors.Is(err, ErrScopeUnavailable) {
		t.Fatal("nil setup was not rejected before native capability lookup")
	}
	if _, err := c.RequestEffectScope("label", callback); !errors.Is(err, ErrScopeUnavailable) || setup.Load() {
		t.Fatal("context without native scope hook ran setup")
	}
	var nilContext *Context
	if _, err := nilContext.RequestEffectScope("label", callback); !errors.Is(err, ErrScopeUnavailable) {
		t.Fatal("nil context fabricated native ownership")
	}
}
