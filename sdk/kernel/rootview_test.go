// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
)

func viewKernel(t *testing.T, mod func(*Config)) *Root {
	t.Helper()
	cfg := frontConfig()
	if mod != nil {
		mod(&cfg)
	}
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(context.Background()); err != nil {
			t.Logf("close: %v", err)
		}
	})
	return r
}

func TestRenderKeyAndPlainValue(t *testing.T) {
	k := NewKey[string]("svc", 2)
	named := k.k.addr(NewLabel("realm").token)
	for _, c := range []struct {
		in         svcKey
		key, label string
	}{{nil, "", ""}, {k.k.def, "svc@2", ""}, {named, "svc@2/realm", ""}, {"effect label", "", "effect label"}, {7, "7", ""}} {
		if key, label := renderKey(c.in); key != c.key || label != c.label {
			t.Errorf("renderKey(%#v) = %q, %q; want %q, %q", c.in, key, label, c.key, c.label)
		}
	}
	if plainValue(&provided{value: "x"}) != "x" || plainValue(&provided{}) != nil || plainValue(5) != 5 {
		t.Error("plainValue must unwrap a provision and pass anything else through")
	}
}

func TestRootViewSnapshotsInspectAndEvents(t *testing.T) {
	r := viewKernel(t, nil)
	key := NewKey[string]("svc", 0)
	var effects atomic.Int64
	provider := r.Plugin(plug("p/provider", nil, []AnyKey{key}, func(c *Context) error {
		c.Defer("cleanup", func(context.Context) error { effects.Add(1); return nil })
		return key.Provide(c, "value")
	}), nil)
	consumer := r.Plugin(plug("p/consumer", []Dep{Need(key)}, nil, nil), nil)
	want(t, provider, Active)
	want(t, consumer, Active)
	ctx := frontCtx(t)

	snaps, err := r.Snapshots(ctx)
	if err != nil || len(snaps) != 2 {
		t.Fatalf("Snapshots = %d, %v; want 2", len(snaps), err)
	}
	p, c := snaps[0], snaps[1]
	if p.ID != provider.ID() || c.ID != consumer.ID() || p.ID >= c.ID {
		t.Fatalf("ids %d %d do not match Fiber.ID %d %d", p.ID, c.ID, provider.ID(), consumer.ID())
	}
	if p.Plugin != "p/provider" || p.State != Active || !p.Alive || p.Leaked || p.Error != "" {
		t.Errorf("provider snapshot %+v", p)
	}
	if !slices.Contains(p.Effects, "cleanup") || len(p.EffectTree) != len(p.Effects) || p.Target == nil || p.Target.Revision != p.Revision {
		t.Errorf("provider effects %v tree %d target %+v", p.Effects, len(p.EffectTree), p.Target)
	}
	if len(c.Epoch) != 1 {
		t.Errorf("consumer epoch %v, want its one provision", c.Epoch)
	}

	view, err := r.Inspect(ctx)
	if err != nil || len(view.Fibers) != 2 {
		t.Fatalf("Inspect = %+v, %v", view, err)
	}
	var prov Provision
	for _, p := range view.Provisions {
		if p.Key == "svc@0" {
			prov = p
		}
	}
	if prov.Key != "svc@0" || prov.Value != "value" || !prov.Active || prov.Owner != p.ID || prov.Plugin != "p/provider" || prov.UID != c.Epoch[0] {
		t.Errorf("provision %+v", prov)
	}

	events, err := r.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if view.Sequence != events[len(events)-1].Sequence {
		t.Errorf("watermark %d, last event %d", view.Sequence, events[len(events)-1].Sequence)
	}
	var service, effect, state int
	for i, e := range events {
		if e.Sequence != uint64(i)+1 {
			t.Fatalf("event %d has sequence %d", i, e.Sequence)
		}
		switch e.Kind {
		case "service":
			if e.Fiber == 0 {
				continue // The built-in services of the root.
			}
			service++
			if e.Key != "svc@0" || e.Label != "" || e.Value != "value" || e.Fiber != p.ID {
				t.Errorf("service event %+v", e)
			}
		case "effect":
			if e.Label == "cleanup" && e.Key == "" && e.Fiber == p.ID {
				effect++
			}
		case "state":
			state++
			if e.From == "" || e.State == "" || e.Plugin == "" {
				t.Errorf("state event %+v", e)
			}
		}
	}
	if service == 0 || effect != 1 || state == 0 {
		t.Errorf("events: %d service, %d cleanup effect, %d state", service, effect, state)
	}

	// Every result is a copy: changing one changes nothing the kernel holds.
	snaps[0].Effects[0], snaps[0].Epoch, snaps[0].Target.UIDs = "mutated", append(snaps[0].Epoch, 9), append(snaps[0].Target.UIDs, 9)
	snaps[0].EffectTree[0].Label = "mutated"
	again, _ := r.Snapshots(ctx)
	if again[0].Effects[0] == "mutated" || again[0].EffectTree[0].Label == "mutated" || slices.Contains(again[0].Epoch, 9) || slices.Contains(again[0].Target.UIDs, 9) {
		t.Error("a snapshot shares memory with the kernel")
	}
	events[0].Label = "mutated"
	if more, _ := r.Events(ctx); more[0].Label == "mutated" {
		t.Error("an event shares memory with the kernel")
	}
	if status := r.RetentionStatus(); status.EventsTotal != uint64(len(events)) || status.ArchiveLimit != frontConfig().ArchiveLimit {
		t.Errorf("retention %+v", status)
	}
}

func TestRootViewRetainsDisposedFibersAndTheirErrors(t *testing.T) {
	r := viewKernel(t, func(c *Config) { c.ArchiveLimit = 1 })
	ctx := frontCtx(t)
	boom := errors.New("boom")
	first := r.Plugin(plug("p/first", nil, nil, func(*Context) error { return boom }), nil)
	if s, err := await(t, first); s != Failed || !errors.Is(err, boom) {
		t.Fatalf("first = %s, %v", s, err)
	}
	if err := first.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	if err := first.Await(ctx); !errors.Is(err, boom) {
		t.Fatalf("archived await = %v", err)
	}
	snaps, _ := r.Snapshots(ctx)
	if len(snaps) != 1 || snaps[0].State != Disposed || snaps[0].Alive || snaps[0].Error == "" || snaps[0].ID != first.ID() {
		t.Fatalf("archived snapshot %+v", snaps)
	}
	second := r.Plugin(plug("p/second", nil, nil, nil), nil)
	want(t, second, Active)
	if err := second.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	_ = second.Await(ctx)
	if err := first.Await(ctx); !errors.Is(err, ErrExpired) || errors.Is(err, ErrUnknown) {
		t.Fatalf("evicted await = %v, want ErrExpired", err)
	}
	if err := (&Fiber{ref: r.core.fiber(1 << 20)}).Await(ctx); !errors.Is(err, ErrUnknown) {
		t.Fatalf("never issued await = %v, want ErrUnknown", err)
	}
	if status := r.RetentionStatus(); status.ArchiveEvicted != 1 || status.Archived != 1 {
		t.Errorf("retention %+v", status)
	}
}

func TestRootViewEventsTruncatedAndClosed(t *testing.T) {
	r := viewKernel(t, func(c *Config) { c.EventWindow = 2 })
	ctx := frontCtx(t)
	want(t, r.Plugin(plug("p/a", nil, nil, nil), nil), Active)
	events, err := r.Events(ctx)
	if !errors.Is(err, ErrEventsTruncated) || len(events) != 2 || events[1].Sequence <= 2 {
		t.Fatalf("Events = %+v, %v; want the 2-event tail and ErrEventsTruncated", events, err)
	}
	if err := r.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Snapshots(ctx); !errors.Is(err, ErrClosed) {
		t.Errorf("Snapshots after Close = %v", err)
	}
	if _, err := r.Inspect(ctx); !errors.Is(err, ErrClosed) {
		t.Errorf("Inspect after Close = %v", err)
	}
	if ev, err := r.Events(ctx); !errors.Is(err, ErrClosed) || ev != nil {
		t.Errorf("Events after Close = %v, %v", ev, err)
	}
	if err := r.Notify(ctx); !errors.Is(err, ErrClosed) {
		t.Errorf("Notify after Close = %v", err)
	}
}

func TestRootDisposeIsRefused(t *testing.T) {
	r := newKernel(t)
	if err := r.Context().Dispose(); !errors.Is(err, ErrRootDispose) {
		t.Fatalf("root Dispose = %v, want ErrRootDispose", err)
	}
}

func TestRootNotifyRechecksAvailability(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	key := NewKey[string]("gated", 0)
	var open atomic.Bool
	want(t, r.Plugin(plug("p/provider", nil, []AnyKey{key}, func(c *Context) error {
		return key.Provide(c, "v", Available(func(*Context) bool { return open.Load() }))
	}), nil), Active)
	consumer := r.Plugin(plug("p/consumer", []Dep{Need(key)}, nil, nil), nil)
	want(t, consumer, Pending)
	open.Store(true)
	if err := r.Notify(ctx, key); err != nil {
		t.Fatal(err)
	}
	want(t, consumer, Active)
	open.Store(false)
	if err := r.Notify(ctx, key, key); err != nil {
		t.Fatal(err)
	}
	want(t, consumer, Pending)
	for name, bad := range map[string]AnyKey{"nil": nil, "zero": Key[string]{}} {
		if err := r.Notify(ctx, key, bad); !errors.Is(err, ErrKey) {
			t.Errorf("%s key: Notify = %v, want ErrKey", name, err)
		}
	}
	if err := r.Notify(ctx); err != nil {
		t.Errorf("Notify of no key = %v", err)
	}
}

func TestRootObserveOrderOptionsAndStatus(t *testing.T) {
	r := viewKernel(t, nil)
	ctx := frontCtx(t)
	if _, err := r.Observe("", func(context.Context, LedgerEvent) error { return nil }); err == nil {
		t.Error("an observer with no label was accepted")
	}
	if _, err := r.Observe("nil", nil); err == nil {
		t.Error("an observer with no callback was accepted")
	}
	var got []LedgerEvent
	fail := errors.New("observer failure")
	dispose, err := r.Observe("collect", func(_ context.Context, e LedgerEvent) error {
		got = append(got, e)
		if e.Sequence == 1 {
			return fail
		}
		return nil
	}, OnOverflow(nil))
	if err != nil {
		t.Fatal(err)
	}
	key := NewKey[int]("n", 0)
	want(t, r.Plugin(plug("p/a", nil, []AnyKey{key}, func(c *Context) error { return key.Provide(c, 1) }), nil), Active)
	if err := r.AwaitObservers(ctx); err != nil {
		t.Fatal(err)
	}
	all, err := r.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, all[len(all)-len(got):]) || len(got) == 0 {
		t.Fatalf("observer saw %d events, ledger has %d: not the same order", len(got), len(all))
	}
	status := r.ObserverStatus()
	if status.Observers < 1 || status.Accepted != all[len(all)-1].Sequence || status.Delivered != status.Accepted || status.Remaining != 0 {
		t.Errorf("status %+v", status)
	}
	if err := dispose(ctx); err != nil {
		t.Fatal(err)
	}
	if r.ObserverStatus().Observers != status.Observers-1 {
		t.Error("the disposer did not remove the observer")
	}
}

func TestRootObserveOverflowDetachesAndSignals(t *testing.T) {
	r := viewKernel(t, func(c *Config) { c.ObserverQueue = 2 })
	ctx := frontCtx(t)
	entered, release, signalled := make(chan struct{}), make(chan struct{}), make(chan *ObserverOverflow, 1)
	var once atomic.Bool
	_, err := r.Observe("stalled", func(context.Context, LedgerEvent) error {
		if once.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
		return nil
	}, OnOverflow(func(_ context.Context, o *ObserverOverflow) error { signalled <- o; return nil }))
	if err != nil {
		t.Fatal(err)
	}
	want(t, r.Plugin(plug("p/a", nil, nil, nil), nil), Active) // The first event stalls the observer; later ones pile up behind it.
	<-entered
	want(t, r.Plugin(plug("p/b", nil, nil, nil), nil), Active)
	if r.ObserverStatus().Overflowed != 1 {
		t.Fatalf("status %+v, want one detached observer", r.ObserverStatus())
	}
	close(release)
	overflow := <-signalled
	if overflow.Label != "stalled" || overflow.Lost == 0 || overflow.Limit != 2 || !errors.Is(overflow, ErrObserverOverflow) {
		t.Errorf("overflow %+v", overflow)
	}
	if err := r.AwaitObservers(ctx); !errors.Is(err, ErrObserverOverflow) {
		t.Errorf("AwaitObservers = %v, want ErrObserverOverflow", err)
	}
	if status := r.ObserverStatus(); status.Overflowed != 1 || status.EventsLost != overflow.Lost || status.LastOverflow == "" {
		t.Errorf("status %+v", status)
	}
}

func TestContextDisposeIsARequestNotACompletion(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	var inside, late error
	var held *Context
	f := r.Plugin(plug("p/self", nil, nil, func(c *Context) error {
		held = c
		inside = c.Extend().Dispose() // A derived node shares the activation.
		return nil
	}), nil)
	if err := f.Await(ctx); err != nil {
		t.Fatal(err)
	}
	if inside != nil {
		t.Fatalf("Dispose from Apply = %v, want an accepted request", inside)
	}
	want(t, f, Disposed)
	late = held.Dispose()
	if !errors.Is(late, ErrInactive) {
		t.Fatalf("Dispose after the activation ended = %v, want ErrInactive", late)
	}
	if err := f.Dispose(ctx); err != nil {
		t.Fatalf("a second request is idempotent: %v", err)
	}
}

func TestContextRequestEffectScope(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	var order []string
	record := func(label string) Disposer {
		return func(context.Context) error { order = append(order, label); return nil }
	}
	var handle *EffectHandle
	var sealed, reqErr error
	var escaped *EffectScope
	var held *Context
	f := r.Plugin(plug("p/scope", nil, nil, func(c *Context) error {
		held = c
		var err error
		handle, err = c.RequestEffectScope("tree", func(s *EffectScope) error {
			escaped = s
			if err := s.Defer("a", record("a")); err != nil {
				return err
			}
			return s.Scope("inner", func(s *EffectScope) error { return s.Defer("b", record("b")) })
		})
		if err != nil {
			return err
		}
		_, reqErr = c.RequestEffectScope("", nil)
		return nil
	}), nil)
	want(t, f, Active)
	if reqErr == nil {
		t.Error("a scope with no label or setup was admitted")
	}
	if err := handle.AwaitSetup(ctx); err != nil {
		t.Fatal(err)
	}
	meta := handle.Metadata()
	if meta.Label != "tree" || meta.State != ScopeReady || len(meta.Children) != 2 || meta.Children[1].Label != "inner" || len(meta.Children[1].Children) != 1 {
		t.Fatalf("metadata %+v", meta)
	}
	meta.Children[0].Label = "mutated"
	if handle.Metadata().Children[0].Label != "a" {
		t.Error("Metadata shares memory with the scope")
	}
	if sealed = escaped.Defer("late", record("late")); !errors.Is(sealed, ErrScopeSealed) {
		t.Errorf("Defer after setup = %v, want ErrScopeSealed", sealed)
	}
	if err := handle.RequestWithdraw(); err != nil {
		t.Fatal(err)
	}
	if err := handle.RequestWithdraw(); err != nil {
		t.Fatalf("a second withdrawal is idempotent: %v", err)
	}
	if err := handle.Await(ctx); err != nil {
		t.Fatal(err)
	}
	if want := []string{"b", "a"}; !slices.Equal(order, want) {
		t.Fatalf("cleanup order %v, want %v", order, want)
	}
	if handle.Metadata().State != ScopeDone {
		t.Errorf("state after cleanup %q", handle.Metadata().State)
	}
	if err := f.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	want(t, f, Disposed)
	if _, err := held.RequestEffectScope("late", func(*EffectScope) error { return nil }); !errors.Is(err, ErrInactiveEffect) || !errors.Is(err, ErrInactive) {
		t.Errorf("request after disposal = %v", err)
	}
}

func TestContextRequestEffectScopeRollsBackOnPanic(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	boom := errors.New("boom")
	var cleaned atomic.Int64
	var handle *EffectHandle
	f := r.Plugin(plug("p/panic", nil, nil, func(c *Context) error {
		var err error
		handle, err = c.RequestEffectScope("fails", func(s *EffectScope) error {
			if err := s.Defer("a", func(context.Context) error { cleaned.Add(1); return nil }); err != nil {
				return err
			}
			panic(boom)
		})
		return err
	}), nil)
	want(t, f, Active)
	var typed *ScopePanicError
	err := handle.AwaitSetup(ctx)
	if !errors.As(err, &typed) || typed.Value != boom || !errors.Is(err, boom) || cleaned.Load() != 1 {
		t.Fatalf("AwaitSetup = %v, cleaned %d", err, cleaned.Load())
	}
	var nilHandle *EffectHandle
	if err := nilHandle.Await(ctx); err == nil {
		t.Error("a nil handle awaited without an error")
	}
}

func TestKernelHooksCanBeListedByTheLauncher(t *testing.T) {
	p := Define(Plugin{ID: "p/listener"})
	listeners := map[AnyHook][]Plugin{}
	for _, h := range []AnyHook{InternalPlugin, InternalStatus, InternalService, InternalConfig, InternalUpdate, InternalCommit} {
		listeners[h] = []Plugin{p}
		if h.Name() == "" {
			t.Error("a kernel hook has no name")
		}
	}
	cfg := frontConfig()
	cfg.Listeners = listeners
	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New with every kernel hook listed: %v", err)
	}
	if err := r.Close(frontCtx(t)); err != nil {
		t.Fatal(err)
	}
	// A hook of a plugin is not sealed and cannot be listed.
	key := NewKey[int]("owner", 0)
	cfg.Listeners = map[AnyHook][]Plugin{NewEvent[int]("p/own", OwnedBy(key)): {p}}
	if _, err := New(cfg); !errors.Is(err, ErrSealed) {
		t.Errorf("a plugin hook was accepted in Listeners: %v", err)
	}
}
