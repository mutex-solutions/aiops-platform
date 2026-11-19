// SPDX-License-Identifier: Apache-2.0

package composition_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	co "github.com/mutex-solutions/aiops-platform/test/spike/s0/composition"
	cc "github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
	cm "github.com/mutex-solutions/aiops-platform/test/spike/s0/contextmodel"
)

func both(t *testing.T, body func(*testing.T, context.Context, cc.Runtime)) {
	t.Helper()
	for _, candidate := range []struct {
		name string
		new  func() cc.Runtime
	}{{"graphlock", cc.NewRecordingGraphLock}, {"reconciler", cc.NewRecordingReconciler}, {"kernel", cc.NewRecordingKernel}} {
		t.Run(candidate.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r := candidate.new()
			defer func() {
				if err := r.Close(ctx); err != nil {
					t.Error(err)
				}
			}()
			if gap := cc.UnsupportedGap(ctx, r); gap != "" {
				t.Skipf("UNSUPPORTED backend, not run, not a pass: %s", gap)
			}
			body(t, ctx, r)
		})
	}
}
func root(t *testing.T, backend cc.Runtime, spec cm.RootSpec) *co.RootAuthority {
	t.Helper()
	r, err := co.NewRuntime(backend, spec)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func start(t *testing.T, ctx context.Context, r *co.RootAuthority, parent *co.Context, entry cm.Plugin, spec co.Spec) *co.Fiber {
	t.Helper()
	f, err := r.Start(ctx, parent, entry, spec)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func await(t *testing.T, ctx context.Context, f *co.Fiber) {
	t.Helper()
	if err := f.Await(ctx); err != nil {
		t.Fatal(err)
	}
}
func state(t *testing.T, ctx context.Context, r cc.Runtime, f *co.Fiber, want cc.State) {
	t.Helper()
	snapshots, err := r.Snapshots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range snapshots {
		if s.ID == f.ID {
			if s.State != want {
				t.Fatalf("%s = %s, want %s (%s)", s.Name, s.State, want, s.Error)
			}
			return
		}
	}
	t.Fatal("missing fiber")
}

func TestActualRealmsKeyMajorsAndCallerViews(t *testing.T) {
	both(t, func(t *testing.T, ctx context.Context, backend cc.Runtime) {
		entry := cm.NewEntry("unit")
		one := cm.NewKey[string]("svc", 1, cm.Config[string]())
		two := cm.NewKey[string]("svc", 2)
		r := root(t, backend, cm.RootSpec{Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{one.Any(), two.Any()}})
		label := cm.NewLabel("joined")
		a, _ := r.Context().Isolate(one.Any(), label)
		b, _ := r.Context().Extend().Isolate(one.Any(), label)
		other, _ := r.Context().Isolate(one.Any(), cm.NewLabel("joined"))
		outer, _ := b.Intercept(one.Any(), "outer")
		inner, _ := outer.Intercept(one.Any(), "inner")
		if a.Parent() != r.Context() || outer.Parent() != b || len(cm.Intercepts[string](b.Metadata(), one.Any())) != 0 {
			t.Fatal("immutable context tree changed")
		}
		p := start(t, ctx, r, a, entry, co.Spec{Metadata: cm.Spec{ID: "provider", Provides: []cm.AnyKey{one.Any(), two.Any()}}, Apply: func(c *co.Context) (cc.Disposer, error) {
			if err := co.ProvideFor(c, one, func(caller *co.Context) string {
				return fmt.Sprint(cm.Intercepts[string](caller.Metadata(), one.Any()))
			}, nil); err != nil {
				return nil, err
			}
			return nil, co.Provide(c, two, "major-two-global", nil)
		}})
		await(t, ctx, p)
		separate := start(t, ctx, r, other, entry, co.Spec{Metadata: cm.Spec{ID: "separate-provider", Provides: []cm.AnyKey{one.Any()}}, Apply: func(c *co.Context) (cc.Disposer, error) { return nil, co.Provide(c, one, "separate", nil) }})
		await(t, ctx, separate)
		seen := make(chan []string, 2)
		consumer := func(id string, config string) co.Spec {
			return co.Spec{Metadata: cm.Spec{ID: id, Inject: []cm.Dep{cm.NeedWith(one, config), cm.Need(two)}}, Apply: func(c *co.Context) (cc.Disposer, error) {
				x, err := co.Resolve(c, one)
				if err != nil {
					return nil, err
				}
				y, err := co.Resolve(c, two)
				seen <- []string{x, y}
				return nil, err
			}}
		}
		joined := start(t, ctx, r, inner, entry, consumer("joined", "need"))
		await(t, ctx, joined)
		if got := <-seen; !reflect.DeepEqual(got, []string{"[outer inner need]", "major-two-global"}) {
			t.Fatal(got)
		}
		isolated := start(t, ctx, r, other, entry, consumer("isolated", "other"))
		await(t, ctx, isolated)
		if got := <-seen; !reflect.DeepEqual(got, []string{"separate", "major-two-global"}) {
			t.Fatal(got)
		}
		missing := start(t, ctx, r, r.Context(), entry, consumer("missing-root-realm", ""))
		await(t, ctx, missing)
		state(t, ctx, backend, missing, cc.Pending)
		duplicate := start(t, ctx, r, b, entry, co.Spec{Metadata: cm.Spec{ID: "duplicate", Provides: []cm.AnyKey{one.Any()}}, Apply: func(c *co.Context) (cc.Disposer, error) { return nil, co.Provide(c, one, "replacement", nil) }})
		if err := duplicate.Await(ctx); err == nil {
			t.Fatal("duplicate provision succeeded")
		}
		state(t, ctx, backend, duplicate, cc.Failed)
	})
}

func TestEntryIdentityAndNativeScopeOwnership(t *testing.T) {
	both(t, func(t *testing.T, ctx context.Context, backend cc.Runtime) {
		owner, reader, impostor := cm.NewEntry("owner"), cm.NewEntry("reader"), cm.NewEntry("owner")
		key := cm.NewKey[string]("bound", 1, cm.Classes(cm.ProviderBound, cm.ConsumerBound))
		r := root(t, backend, cm.RootSpec{Entries: []cm.Plugin{owner, reader, impostor}, Keys: []cm.AnyKey{key.Any()}, Bindings: []cm.Binding{{Key: key.Any(), Providers: []cm.Plugin{owner}, Consumers: []cm.Plugin{reader}}}})
		_, err := r.Start(ctx, r.Context(), impostor, co.Spec{Metadata: cm.Spec{Provides: []cm.AnyKey{key.Any()}}})
		if !errors.Is(err, cm.ErrBinding) {
			t.Fatalf("spoofed entry: %v", err)
		}
		_, err = r.Start(ctx, r.Context(), reader, co.Spec{Metadata: cm.Spec{Inject: []cm.Dep{cm.Want(key)}}})
		if !errors.Is(err, cm.ErrWant) {
			t.Fatalf("bound optional: %v", err)
		}
		scope := co.NewScope[string](key.Any())
		contexts := make(chan *co.Context, 1)
		provider := start(t, ctx, r, r.Context(), owner, co.Spec{Metadata: cm.Spec{ID: "owner", Provides: []cm.AnyKey{key.Any()}}, Apply: func(c *co.Context) (cc.Disposer, error) {
			if _, err := scope.With(c, "declaration-only"); !errors.Is(err, cm.ErrScope) {
				return nil, errors.New("declaration authorized scope without provision")
			}
			if err := co.ProvideFor(c, key, func(caller *co.Context) string { v, _ := scope.From(caller); return v + ":" + caller.Entry().Name() }, nil); err != nil {
				return nil, err
			}
			scoped, err := scope.With(c, "provider-owned")
			if err == nil {
				contexts <- scoped
			}
			return nil, err
		}})
		await(t, ctx, provider)
		scoped := <-contexts
		seen := make(chan string, 1)
		consumer := start(t, ctx, r, scoped, reader, co.Spec{Metadata: cm.Spec{ID: "reader", Inject: []cm.Dep{cm.Need(key)}}, Apply: func(c *co.Context) (cc.Disposer, error) {
			if _, err := scope.With(c, "consumer-spoof"); !errors.Is(err, cm.ErrScope) {
				return nil, errors.New("consumer wrote scope")
			}
			if _, err := c.Child(co.Spec{Metadata: cm.Spec{ID: owner.Name(), Provides: []cm.AnyKey{key.Any()}}}); !errors.Is(err, cm.ErrBinding) {
				return nil, fmt.Errorf("child entry spoof: %v", err)
			}
			v, err := co.Resolve(c, key)
			if err == nil {
				seen <- v
			}
			return nil, err
		}})
		await(t, ctx, consumer)
		if got := <-seen; got != "provider-owned:reader" {
			t.Fatal(got)
		}
		if err := provider.Dispose(ctx); err != nil {
			t.Fatal(err)
		}
		await(t, ctx, provider)
		if _, err := scope.With(scoped, "late"); !errors.Is(err, cm.ErrScope) {
			t.Fatalf("inactive scope writer: %v", err)
		}
	})
}

func TestWhenIsRealOptionalChildAndConsumerAvailability(t *testing.T) {
	both(t, func(t *testing.T, ctx context.Context, backend cc.Runtime) {
		entry := cm.NewEntry("unit")
		key := cm.NewKey[int]("optional", 1)
		r := root(t, backend, cm.RootSpec{Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{key.Any()}})
		childHandle := make(chan *co.Fiber, 1)
		var parentLoads, childLoads, childCleanups atomic.Int32
		cleanupArrived, cleanupRelease := make(chan struct{}), make(chan struct{})
		var releaseOnce sync.Once
		defer releaseOnce.Do(func() { close(cleanupRelease) })
		retained := make(chan *co.Context, 2)
		parent := start(t, ctx, r, r.Context(), entry, co.Spec{Metadata: cm.Spec{ID: "parent", Inject: []cm.Dep{cm.Want(key)}}, Apply: func(c *co.Context) (cc.Disposer, error) {
			parentLoads.Add(1)
			if _, err := co.Resolve(c, key); !errors.Is(err, cm.ErrUndeclared) {
				return nil, fmt.Errorf("Want authorized read: %v", err)
			}
			child, err := c.When("conditional", []cm.Dep{cm.Need(key)}, func(child *co.Context) (cc.Disposer, error) {
				if value, err := co.Resolve(child, key); err != nil || value != 7 {
					return nil, fmt.Errorf("child read %v %v", value, err)
				}
				load := childLoads.Add(1)
				retained <- child
				return func(context.Context) error {
					if load == 1 {
						close(cleanupArrived)
						<-cleanupRelease
					}
					value, err := co.Resolve(child, key)
					if err != nil || value != 7 {
						return fmt.Errorf("retained teardown read: %v %v", value, err)
					}
					childCleanups.Add(1)
					return nil
				}, nil
			})
			if err == nil {
				childHandle <- child
			}
			return nil, err
		}})
		await(t, ctx, parent)
		child := <-childHandle
		await(t, ctx, child)
		state(t, ctx, backend, parent, cc.Active)
		state(t, ctx, backend, child, cc.Pending)
		var ready atomic.Bool
		provider := start(t, ctx, r, r.Context(), entry, co.Spec{Metadata: cm.Spec{ID: "provider", Provides: []cm.AnyKey{key.Any()}}, Apply: func(c *co.Context) (cc.Disposer, error) {
			return nil, co.Provide(c, key, 7, func(caller *co.Context) bool { return ready.Load() && caller.Metadata().Spec().ID == "conditional" })
		}})
		await(t, ctx, provider)
		await(t, ctx, child)
		state(t, ctx, backend, child, cc.Pending)
		ready.Store(true)
		if err := r.Notify(ctx, r.Context(), key.Any()); err != nil {
			t.Fatal(err)
		}
		await(t, ctx, child)
		state(t, ctx, backend, child, cc.Active)
		captured := <-retained
		ready.Store(false)
		if err := r.Notify(ctx, r.Context(), key.Any()); err != nil {
			t.Fatal(err)
		}
		select {
		case <-cleanupArrived:
		case <-ctx.Done():
			t.Fatal("availability loss did not quiesce child")
		}
		state(t, ctx, backend, child, cc.Unloading)
		if value, err := co.Resolve(captured, key); err != nil || value != 7 {
			t.Fatalf("captured unloading read: %v %v", value, err)
		}
		releaseOnce.Do(func() { close(cleanupRelease) })
		await(t, ctx, child)
		state(t, ctx, backend, child, cc.Pending)
		if _, err := co.Resolve(captured, key); !errors.Is(err, cc.ErrInactive) {
			t.Fatalf("released activation read: %v", err)
		}
		ready.Store(true)
		if err := r.Notify(ctx, r.Context(), key.Any()); err != nil {
			t.Fatal(err)
		}
		await(t, ctx, child)
		if err := provider.Dispose(ctx); err != nil {
			t.Fatal(err)
		}
		await(t, ctx, provider)
		await(t, ctx, child)
		state(t, ctx, backend, child, cc.Pending)
		state(t, ctx, backend, parent, cc.Active)
		if parentLoads.Load() != 1 || childLoads.Load() != 2 || childCleanups.Load() != 2 {
			t.Fatalf("loads/cleanup = %d/%d/%d", parentLoads.Load(), childLoads.Load(), childCleanups.Load())
		}
		if err := parent.Dispose(ctx); err != nil {
			t.Fatal(err)
		}
		await(t, ctx, parent)
		state(t, ctx, backend, child, cc.Disposed)
	})
}

func TestChildInheritanceAndQuiescenceBeforeProviderCleanup(t *testing.T) {
	both(t, func(t *testing.T, ctx context.Context, backend cc.Runtime) {
		entry := cm.NewEntry("unit")
		inherited := cm.NewKey[int]("inherited", 1)
		own := cm.NewKey[int]("parent-provision", 1)
		r := root(t, backend, cm.RootSpec{Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{inherited.Any(), own.Any()}})
		source := start(t, ctx, r, r.Context(), entry, co.Spec{Metadata: cm.Spec{ID: "source", Provides: []cm.AnyKey{inherited.Any()}}, Apply: func(c *co.Context) (cc.Disposer, error) { return nil, co.Provide(c, inherited, 3, nil) }})
		await(t, ctx, source)
		arrived, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		defer once.Do(func() { close(release) })
		var mu sync.Mutex
		var order []string
		record := func(s string) { mu.Lock(); order = append(order, s); mu.Unlock() }
		children := make(chan []*co.Fiber, 1)
		parent := start(t, ctx, r, r.Context(), entry, co.Spec{Metadata: cm.Spec{ID: "parent", Inject: []cm.Dep{cm.Need(inherited)}, Provides: []cm.AnyKey{own.Any()}}, Apply: func(c *co.Context) (cc.Disposer, error) {
			if err := co.Provide(c, own, 9, nil); err != nil {
				return nil, err
			}
			if err := c.Defer("provider-cleanup", func(context.Context) error { record("parent"); return nil }); err != nil {
				return nil, err
			}
			child, err := c.Child(co.Spec{Metadata: cm.Spec{ID: "child", Inject: []cm.Dep{cm.Need(own)}}, Apply: func(child *co.Context) (cc.Disposer, error) {
				v, err := co.Resolve(child, inherited)
				if err != nil || v != 3 {
					return nil, fmt.Errorf("inheritance: %v %v", v, err)
				}
				return func(context.Context) error {
					close(arrived)
					<-release
					v, err := co.Resolve(child, own)
					if err == nil && v == 9 {
						record("child")
					}
					return err
				}, nil
			}})
			if err != nil {
				return nil, err
			}
			started := make(chan struct{})
			early, err := c.Child(co.Spec{Metadata: cm.Spec{ID: "while-parent-loading"}, Apply: func(child *co.Context) (cc.Disposer, error) {
				value, err := co.Resolve(child, inherited)
				if err != nil || value != 3 {
					return nil, fmt.Errorf("inherited while loading: %v %v", value, err)
				}
				close(started)
				return nil, nil
			}})
			if err != nil {
				return nil, err
			}
			select {
			case <-started:
			case <-c.Std().Done():
				return nil, c.Std().Err()
			}
			private, err := c.Isolate(inherited.Any())
			if err != nil {
				return nil, err
			}
			stronger, err := private.Child(co.Spec{Metadata: cm.Spec{ID: "realm-child"}, Apply: func(child *co.Context) (cc.Disposer, error) {
				_, err := co.Resolve(child, inherited)
				if !errors.Is(err, cm.ErrUndeclared) {
					return nil, fmt.Errorf("cross-realm retained injection: %v", err)
				}
				return nil, nil
			}})
			if err == nil {
				children <- []*co.Fiber{child, stronger, early}
			}
			return nil, err
		}})
		await(t, ctx, parent)
		handles := <-children
		for _, child := range handles {
			await(t, ctx, child)
		}
		if err := parent.Dispose(ctx); err != nil {
			t.Fatal(err)
		}
		select {
		case <-arrived:
		case <-ctx.Done():
			t.Fatal("child cleanup did not start")
		}
		state(t, ctx, backend, parent, cc.Unloading)
		state(t, ctx, backend, handles[0], cc.Unloading)
		mu.Lock()
		premature := len(order)
		mu.Unlock()
		if premature != 0 {
			t.Fatal("parent cleanup preceded child")
		}
		once.Do(func() { close(release) })
		await(t, ctx, parent)
		mu.Lock()
		got := append([]string(nil), order...)
		mu.Unlock()
		if !reflect.DeepEqual(got, []string{"child", "parent"}) {
			t.Fatal(got)
		}
		for _, child := range handles {
			state(t, ctx, backend, child, cc.Disposed)
		}
	})
}
