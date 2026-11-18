// SPDX-License-Identifier: Apache-2.0

package concurrency

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

// These tests pin the structural teardown order carried from the oracle (the
// vendored Cordis fiber): a child's disposer is one effect in its parent's
// ordered effect list, so the parent's cleanup that was registered after the
// child runs first, the child is invalidated and unloaded when the walk reaches
// it, and the parent is DISPOSED only after the child is. Service dependency
// retention is a separate axis and is covered by the existing dependency tests.

type ptCandidate struct {
	name string
	make func() Runtime
}

var ptCandidates = []ptCandidate{{"reconciler", NewReconciler}, {"graph-lock", NewGraphLock}}

func ptContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func ptRuntime(t *testing.T, c ptCandidate) Runtime {
	t.Helper()
	r := c.make()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = closeRuntime(ctx, r)
	})
	return r
}

func ptReceive(t *testing.T, ctx context.Context, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-ctx.Done():
		t.Fatalf("barrier %s: %v", what, ctx.Err())
	}
}

func ptSnapshot(t *testing.T, ctx context.Context, r Runtime, id uint64) Snapshot {
	t.Helper()
	snapshots, err := r.Snapshots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range snapshots {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no snapshot for fiber %d", id)
	return Snapshot{}
}

type ptLog struct {
	mu    sync.Mutex
	items []string
}

func (l *ptLog) add(item string) {
	l.mu.Lock()
	l.items = append(l.items, item)
	l.mu.Unlock()
}

func (l *ptLog) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.items)
}

// ptStateOrder returns the index of the event in which the named fiber entered
// the given state, or -1.
func ptStateOrder(events []Event, name string, state State) int {
	for i, event := range events {
		if event.Kind == "state" && event.Name == name && event.State == state {
			return i
		}
	}
	return -1
}

// The D22 shape: the parent cleanup registered after the child runs first,
// while the child is still ACTIVE and not yet requested for disposal; only
// afterwards is the child invalidated, torn down, and joined.
func TestParentCleanupRunsBeforeChildTeardown(t *testing.T) {
	for _, c := range ptCandidates {
		t.Run(c.name, func(t *testing.T) {
			ctx := ptContext(t)
			r := ptRuntime(t, c)
			var log ptLog
			var childID uint64
			childGate, releaseChild := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			t.Cleanup(func() { releaseOnce.Do(func() { close(releaseChild) }) })
			parentCleanup := make(chan struct{})
			var atParentCleanup Snapshot
			var oldContext *Context
			parent, err := r.Mount(ctx, Spec{ID: "parent", ApplyEffect: func(pc *Context) (Disposer, error) {
				oldContext = pc
				id, err := pc.MountChild(Spec{ID: "child", ApplyEffect: func(*Context) (Disposer, error) {
					return func(context.Context) error {
						close(childGate)
						<-releaseChild
						log.add("child-cleanup")
						return nil
					}, nil
				}})
				if err != nil {
					return nil, err
				}
				childID = id
				return func(context.Context) error {
					// Observe from inside the parent's cleanup: the child must
					// still be an ordinary ACTIVE, alive fiber.
					snapshots, err := r.Snapshots(ctx)
					if err != nil {
						return err
					}
					for _, s := range snapshots {
						if s.ID == childID {
							atParentCleanup = s
						}
					}
					log.add("parent-cleanup")
					close(parentCleanup)
					return nil
				}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			if err := r.Dispose(ctx, parent); err != nil {
				t.Fatal(err)
			}
			ptReceive(t, ctx, parentCleanup, "parent cleanup")
			ptReceive(t, ctx, childGate, "child teardown started")
			if atParentCleanup.State != Active || !atParentCleanup.Alive {
				t.Fatalf("child was torn down before the parent's cleanup ran: %+v", atParentCleanup)
			}
			p, ch := ptSnapshot(t, ctx, r, parent), ptSnapshot(t, ctx, r, childID)
			if p.State != Unloading || ch.State != Unloading || ch.Alive {
				t.Fatalf("parent must stay UNLOADING until the child is DISPOSED: parent=%+v child=%+v", p, ch)
			}
			if _, err := oldContext.MountChild(Spec{ID: "late"}); !errors.Is(err, ErrInactive) {
				t.Fatalf("a disposing parent accepted a new child: %v", err)
			}
			releaseOnce.Do(func() { close(releaseChild) })
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			if got := log.get(); !slices.Equal(got, []string{"parent-cleanup", "child-cleanup"}) {
				t.Fatalf("cleanup order or multiplicity = %v", got)
			}
			events, err := r.Events(ctx)
			if err != nil {
				t.Fatal(err)
			}
			childDone, parentDone := ptStateOrder(events, "child", Disposed), ptStateOrder(events, "parent", Disposed)
			if childDone < 0 || parentDone < 0 || childDone > parentDone {
				t.Fatalf("parent DISPOSED (%d) before child DISPOSED (%d)", parentDone, childDone)
			}
			parentUnloading := ptStateOrder(events, "parent", Unloading)
			childUnloading := ptStateOrder(events, "child", Unloading)
			if parentUnloading < 0 || childUnloading < 0 || parentUnloading > childUnloading {
				t.Fatalf("child UNLOADING (%d) preceded parent UNLOADING (%d)", childUnloading, parentUnloading)
			}
		})
	}
}

// Position matters: an effect registered before the child runs after the
// child's disposal was requested, and does not wait for the child to finish.
// This is the carried oracle semantic: a child can still be unloading, using
// the parent's earlier resources, after those resources were cleaned.
func TestParentEffectRegisteredBeforeChildDoesNotWaitForChild(t *testing.T) {
	for _, c := range ptCandidates {
		t.Run(c.name, func(t *testing.T) {
			ctx := ptContext(t)
			r := ptRuntime(t, c)
			var log ptLog
			childGate, releaseChild := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			t.Cleanup(func() { releaseOnce.Do(func() { close(releaseChild) }) })
			earlyCleaned := make(chan struct{})
			parent, err := r.Mount(ctx, Spec{ID: "parent", Apply: func(pc *Context) error {
				if err := pc.Defer("early", func(context.Context) error {
					log.add("early-cleanup")
					close(earlyCleaned)
					return nil
				}); err != nil {
					return err
				}
				_, err := pc.MountChild(Spec{ID: "child", ApplyEffect: func(*Context) (Disposer, error) {
					return func(context.Context) error {
						close(childGate)
						<-releaseChild
						log.add("child-cleanup")
						return nil
					}, nil
				}})
				return err
			}})
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			if err := r.Dispose(ctx, parent); err != nil {
				t.Fatal(err)
			}
			ptReceive(t, ctx, childGate, "child teardown started")
			ptReceive(t, ctx, earlyCleaned, "earlier effect cleaned while child unloads")
			if got := log.get(); !slices.Equal(got, []string{"early-cleanup"}) {
				t.Fatalf("log while the child's cleanup is held = %v", got)
			}
			if s := ptSnapshot(t, ctx, r, parent); s.State != Unloading {
				t.Fatalf("parent left UNLOADING before its child was DISPOSED: %+v", s)
			}
			releaseOnce.Do(func() { close(releaseChild) })
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			if got := log.get(); !slices.Equal(got, []string{"early-cleanup", "child-cleanup"}) {
				t.Fatalf("final log = %v", got)
			}
			if s := ptSnapshot(t, ctx, r, parent); s.State != Disposed || s.Error != "" {
				t.Fatalf("parent = %+v", s)
			}
		})
	}
}
