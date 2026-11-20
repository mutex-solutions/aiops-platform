// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

// These tests drive the seam directly, with service keys that are not strings.

// keyOf is a service key as the front will pass it: a pointer, equal only to itself.
type keyOf struct{ name string }

func TestCoreConfigRequiresEveryNumberPositive(t *testing.T) {
	fields := map[string]func(*coreConfig, int){
		"loadDeadline":    func(c *coreConfig, v int) { c.loadDeadline = time.Duration(v) },
		"drainDeadline":   func(c *coreConfig, v int) { c.drainDeadline = time.Duration(v) },
		"disposeDeadline": func(c *coreConfig, v int) { c.disposeDeadline = time.Duration(v) },
		"archiveLimit":    func(c *coreConfig, v int) { c.archiveLimit = v },
		"eventWindow":     func(c *coreConfig, v int) { c.eventWindow = v },
		"observerQueue":   func(c *coreConfig, v int) { c.observerQueue = v },
		"ownerInbox":      func(c *coreConfig, v int) { c.ownerInbox = v },
		"terminalLimit":   func(c *coreConfig, v int) { c.terminalLimit = v },
	}
	for name, set := range fields {
		for _, bad := range []int{0, -1} {
			cfg := testConfig()
			set(&cfg, bad)
			c, err := newCore(cfg)
			if err == nil || c != nil || !strings.Contains(err.Error(), "coreConfig."+name) {
				t.Errorf("%s=%d: core=%v err=%v, want an error naming the field", name, bad, c, err)
			}
		}
	}
	if _, err := newCore(coreConfig{}); err == nil {
		t.Fatal("the zero configuration was accepted: the core must hold no defaults")
	}
	c, err := newCore(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-c.done
}

// activeHandle mounts one fiber and returns its live handle.
func activeHandle(t *testing.T, r *rt) (*handle, uint64) {
	t.Helper()
	ctx := rcGuard(t)
	got := make(chan *handle, 1)
	id, err := r.Mount(ctx, Spec{ID: "holder", Apply: func(c *Handle) error { got <- c; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	return <-got, id
}

func TestEnterAllocatesNothingAndReturnsTheActivationsLeave(t *testing.T) {
	r := NewReconciler().(*rt)
	t.Cleanup(func() { _ = closeRuntime(rcGuard(t), r) })
	c, _ := activeHandle(t, r)
	if got := testing.AllocsPerRun(1000, func() {
		leave, ok := c.enter()
		if !ok {
			panic("live activation refused a lease")
		}
		leave()
	}); got != 0 {
		t.Fatalf("enter+leave allocated %g times per call, want 0", got)
	}
	first, _ := c.enter()
	second, _ := c.enter()
	if c.lease.Load() != 2 {
		t.Fatalf("open leases = %d", c.lease.Load())
	}
	first()
	second()
	if c.lease.Load() != 0 {
		t.Fatalf("leases after leave = %d", c.lease.Load())
	}
}

func TestRefusedRegistrationsUnwindWithBothSentinels(t *testing.T) {
	r := NewReconciler().(*rt)
	ctx := rcGuard(t)
	t.Cleanup(func() { _ = closeRuntime(ctx, r) })
	c, id := activeHandle(t, r)
	if err := r.Dispose(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := r.AwaitFiber(ctx, id); err != nil {
		t.Fatal(err)
	}
	noop := func(context.Context) error { return nil }
	for name, call := range map[string]func(){
		"deferEffect": func() { c.deferEffect("late", noop) },
		"goTracked":   func() { c.goTracked("late", func(context.Context) error { return nil }) },
	} {
		t.Run(name, func(t *testing.T) {
			// The call is a registration without an error result: it panics with a
			// *refusedCall, and a callback boundary turns that into the fiber's error.
			err := invokeCallback(func() error { call(); return nil })
			if !errors.Is(err, ErrInactiveEffect) || !errors.Is(err, ErrInactive) {
				t.Fatalf("refused %s unwound as %v, want ErrInactiveEffect and ErrInactive", name, err)
			}
			var refused *refusedCall
			func() {
				defer func() { refused, _ = recover().(*refusedCall) }()
				call()
			}()
			if refused == nil {
				t.Fatalf("%s did not panic with *refusedCall", name)
			}
		})
	}
}

func TestInvalidRegistrationArgumentsPanicInsteadOfReturning(t *testing.T) {
	r := NewReconciler().(*rt)
	t.Cleanup(func() { _ = closeRuntime(rcGuard(t), r) })
	c, _ := activeHandle(t, r)
	for name, call := range map[string]func(){
		"nil disposer":   func() { c.deferEffect("x", nil) },
		"empty go label": func() { c.goTracked("", func(context.Context) error { return nil }) },
		"nil go run":     func() { c.goTracked("x", nil) },
		"unavailable go": func() {
			newHandle(r.core, context.Background(), 0, nil).goTracked("x", func(context.Context) error { return nil })
		},
		"empty effect": func() { _ = c.effect("", nil); panic(&refusedCall{err: errors.New("effect returns an error instead")}) },
		"inactive check": func() {
			newHandle(r.core, context.Background(), 0, nil).deferEffect("x", func(context.Context) error { return nil })
		},
		"mount child dead": func() {
			_, err := newHandle(r.core, context.Background(), 0, nil).mountChild(pluginSpec{ID: "x"})
			refuse(err)
		},
	} {
		if err := invokeCallback(func() error { call(); return nil }); err == nil {
			t.Errorf("%s: no error escaped the boundary", name)
		}
	}
}

// A refusal inside a Check fails that evaluation, which is visible: the fiber
// stays PENDING and its diagnostic names the refusal.
func TestRefusalInsideCheckIsVisibleOnTheFiber(t *testing.T) {
	r := NewReconciler().(*rt)
	ctx := rcGuard(t)
	t.Cleanup(func() { _ = closeRuntime(ctx, r) })
	key := &keyOf{"service"}
	provider, err := r.core.mount(ctx, pluginSpec{ID: "provider", Provides: []svcKey{key}, Apply: func(c *handle) error { return c.provide(key, "service", 1) }})
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := r.core.mount(ctx, pluginSpec{ID: "consumer", Needs: []svcKey{key}, Check: func(c *handle) bool {
		c.deferEffect("not allowed", func(context.Context) error { return nil })
		return true
	}})
	if err != nil {
		t.Fatal(err)
	}
	_ = provider
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	s, err := r.core.fiber(consumer).snapshot(ctx)
	if err != nil || s.State != Pending || !strings.Contains(s.Error, ErrInactiveEffect.Error()) {
		t.Fatalf("consumer = %+v, %v", s, err)
	}
}

func TestServiceKeysAreValuesNotStrings(t *testing.T) {
	r := NewReconciler().(*rt)
	ctx := rcGuard(t)
	t.Cleanup(func() { _ = closeRuntime(ctx, r) })
	a, b := &keyOf{"same name"}, &keyOf{"same name"}
	got := make(chan any, 1)
	if _, err := r.core.mount(ctx, pluginSpec{ID: "a", Provides: []svcKey{a}, Apply: func(c *handle) error { return c.provide(a, "a", "value of a") }}); err != nil {
		t.Fatal(err)
	}
	id, err := r.core.mount(ctx, pluginSpec{ID: "b", Needs: []svcKey{b}, Apply: func(c *handle) error { got <- 1; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	// b is a different key with the same name: nothing provides it.
	if state, err := r.core.fiber(id).state(ctx); err != nil || state != Pending {
		t.Fatalf("consumer of an unprovided key = %v, %v", state, err)
	}
	select {
	case <-got:
		t.Fatal("a key with the same name bound to another key's provider")
	default:
	}
	c := newHandle(r.core, context.Background(), 0, nil)
	_ = c
	for name, spec := range map[string]pluginSpec{
		"nil need":         {ID: "x", Needs: []svcKey{nil}},
		"duplicate need":   {ID: "x", Needs: []svcKey{a, a}},
		"incomparable key": {ID: "x", Provides: []svcKey{[]int{1}}},
		"two apply forms":  {ID: "x", Apply: func(*handle) error { return nil }, ApplyEffect: func(*handle) (disposer, error) { return nil, nil }},
	} {
		if _, err := r.core.mount(ctx, spec); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestFiberRefOperations(t *testing.T) {
	r := newReconcilerWith(func(c *coreConfig) { c.archiveLimit = 1 })
	ctx := rcGuard(t)
	t.Cleanup(func() { _ = closeRuntime(ctx, r) })
	mount := func(name string) fiberRef {
		id, err := r.core.mount(ctx, pluginSpec{ID: name, Apply: func(*handle) error { return nil }})
		if err != nil {
			t.Fatal(err)
		}
		return r.core.fiber(id)
	}
	first, second := mount("first"), mount("second")
	if err := first.await(ctx); err != nil {
		t.Fatal(err)
	}
	if state, err := first.state(ctx); err != nil || state != Active {
		t.Fatalf("state = %v, %v", state, err)
	}
	if err := first.update(ctx); err != nil {
		t.Fatal(err)
	}
	if err := first.await(ctx); err != nil {
		t.Fatal(err)
	}
	if s, err := first.snapshot(ctx); err != nil || s.Revision != 1 || s.State != Active || s.Name != "first" {
		t.Fatalf("snapshot after update = %+v, %v", s, err)
	}
	for _, f := range []fiberRef{first, second} {
		if err := f.dispose(ctx); err != nil {
			t.Fatal(err)
		}
		if err := f.await(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// The archive keeps one record: the second incarnation. The first expired.
	if s, err := second.snapshot(ctx); err != nil || s.State != Disposed {
		t.Fatalf("archived snapshot = %+v, %v", s, err)
	}
	if _, err := first.snapshot(ctx); !errors.Is(err, errExpired) {
		t.Fatalf("expired snapshot error = %v", err)
	}
	if err := first.await(ctx); !errors.Is(err, errExpired) {
		t.Fatalf("expired await = %v", err)
	}
	if err := first.dispose(ctx); err != nil {
		t.Fatalf("dispose of an expired id = %v, want the idempotent no-op", err)
	}
	unknown := r.core.fiber(1 << 40)
	if _, err := unknown.state(ctx); !errors.Is(err, errUnknown) {
		t.Fatalf("never issued id = %v", err)
	}
	if err := unknown.update(ctx); !errors.Is(err, errUnknown) {
		t.Fatalf("update of a never issued id = %v", err)
	}
	if err := r.core.fiber(0).dispose(ctx); !errors.Is(err, errUnknown) {
		t.Fatalf("dispose of the root id = %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := second.state(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("state with a done context = %v", err)
	}
}

// Every event an observer receives carries its sequence number, consecutive from
// 1: that is the total order of the core.
func TestObserverEventsCarryTheTotalOrderSequence(t *testing.T) {
	r := NewReconciler().(*rt)
	ctx := rcGuard(t)
	t.Cleanup(func() { _ = closeRuntime(ctx, r) })
	root, err := r.rootHandle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(chan ledgerEvent, 256)
	if _, err := r.observe(root, "order", func(_ context.Context, e ledgerEvent) error { seen <- e; return nil }, nil); err != nil {
		t.Fatal(err)
	}
	key := &keyOf{"k"}
	for i := range 3 {
		if _, err := r.core.mount(ctx, pluginSpec{ID: fmt.Sprintf("p%d", i), Provides: []svcKey{key}, Apply: func(c *handle) error {
			if i != 0 {
				return nil
			}
			return c.provide(key, "k", i)
		}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.awaitObservers(ctx); err != nil {
		t.Fatal(err)
	}
	close(seen)
	var last uint64
	var sawService bool
	for e := range seen {
		if e.Sequence == 0 || (last != 0 && e.Sequence != last+1) {
			t.Fatalf("sequence %d after %d", e.Sequence, last)
		}
		if e.Kind == "service" && e.Key == key {
			sawService = true
		}
		last = e.Sequence
	}
	if last == 0 || !sawService {
		t.Fatalf("observer saw %d events, service event with the key pointer: %v", last, sawService)
	}
	if _, err := r.observe(nil, "x", nil, nil); !errors.Is(err, errObserverBinding) {
		t.Fatalf("observe with no handle = %v", err)
	}
	other := NewReconciler().(*rt)
	t.Cleanup(func() { _ = closeRuntime(ctx, other) })
	if _, err := other.observe(root, "x", func(context.Context, ledgerEvent) error { return nil }, nil); !errors.Is(err, errObserverBinding) {
		t.Fatalf("observe with another core's handle = %v", err)
	}
}

// A disposer still running past the dispose deadline marks the fiber leaked, in the
// snapshot and in the Settle diagnostic, and the mark outlives the disposer.
func TestDisposerPastTheDeadlineMarksTheFiberLeaked(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewReconciler().(*rt)
		ctx := context.Background()
		gate := make(chan struct{})
		id, err := r.Mount(ctx, Spec{ID: "leaker", Apply: func(c *Handle) error {
			return c.Defer("stuck", func(context.Context) error { <-gate; return nil })
		}})
		if err != nil {
			t.Fatal(err)
		}
		bystander, err := r.Mount(ctx, Spec{ID: "bystander"})
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if err := r.Dispose(ctx, id); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if s := snapshotOf(t, r, id); s.Leaked {
			t.Fatal("marked leaked before the dispose deadline")
		}
		time.Sleep(testDispose + time.Second)
		synctest.Wait()
		err = r.Settle(ctx)
		if !errors.Is(err, ErrLeaked) || !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "fiber 1 dispose deadline") {
			t.Fatalf("Settle = %v, want the dispose deadline marked leaked", err)
		}
		if s := snapshotOf(t, r, id); !s.Leaked || !strings.Contains(s.Error, "leaked") {
			t.Fatalf("snapshot = %+v, want Leaked and its diagnostic", s)
		}
		if s := snapshotOf(t, r, bystander); s.Leaked {
			t.Fatal("an unrelated fiber was marked leaked")
		}
		close(gate)
		synctest.Wait()
		if err := r.Settle(ctx); err != nil {
			t.Fatalf("Settle after the disposer returned = %v", err)
		}
		if s := snapshotOf(t, r, id); s.State != Disposed || !s.Leaked {
			t.Fatalf("archived snapshot = %+v, want DISPOSED and still Leaked", s)
		}
		if err := closeRuntime(ctx, r); err != nil {
			t.Fatal(err)
		}
	})
}

// An Apply past the load deadline is a failure but not a leak: the contract marks
// only a disposer past the dispose deadline.
func TestApplyPastTheLoadDeadlineIsNotLeaked(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewReconciler().(*rt)
		ctx := context.Background()
		gate := make(chan struct{})
		id, err := r.Mount(ctx, Spec{ID: "slow", Apply: func(*Handle) error { <-gate; return nil }})
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(testLoad + time.Second)
		synctest.Wait()
		if s := snapshotOf(t, r, id); s.Leaked {
			t.Fatalf("a slow Apply was marked leaked: %+v", s)
		}
		close(gate)
		synctest.Wait()
		_ = closeRuntime(ctx, r)
	})
}

func snapshotOf(t *testing.T, r *rt, id uint64) Snapshot {
	t.Helper()
	s, err := r.core.fiber(id).snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Past terminalLimit only a count of the diagnostics is kept, and Settle says so.
func TestTerminalDiagnosticsAreBoundedByTheConfiguredLimit(t *testing.T) {
	r := newReconcilerWith(func(c *coreConfig) { c.terminalLimit = 2 })
	ctx := rcGuard(t)
	// A failed cleanup is the diagnostic every later Settle keeps.
	for i := range 5 {
		id, err := r.Mount(ctx, Spec{ID: fmt.Sprintf("bad%d", i), Apply: func(c *Handle) error {
			return c.Defer("failing cleanup", func(context.Context) error { return errors.New("cleanup boom") })
		}})
		if err != nil {
			t.Fatal(err)
		}
		if err := r.AwaitFiber(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := r.Dispose(ctx, id); err != nil {
			t.Fatal(err)
		}
		_ = r.Settle(ctx)
	}
	err := r.Settle(ctx)
	if err == nil || !strings.Contains(err.Error(), "3 further terminal diagnostics dropped (cap 2)") {
		t.Fatalf("Settle = %v", err)
	}
	if got := r.RetentionStatus().TerminalDropped; got != 3 {
		t.Fatalf("TerminalDropped = %d", got)
	}
	_ = closeRuntime(ctx, r)
}
