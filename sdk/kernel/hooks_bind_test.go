// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// The tests of ADR-034 owner decisions 10 (a bound dispatcher), 11 (the binding map
// of sealed hooks) and 12 (a waterfall fails closed).

// ownerPlugin is a plugin that provides key and binds the hooks in Apply, before it
// provides the key, as an owner does.
func ownerPlugin(id string, key Key[int], bind func(c *Context)) Plugin {
	return plug(id, nil, []AnyKey{key}, func(c *Context) error {
		bind(c)
		return key.Provide(c, 1)
	})
}

func TestBindByTheProviderSucceeds(t *testing.T) {
	r := newKernel(t)
	key := NewKey[int]("bind-key", 0)
	ev := NewEvent[int]("t/bind-ev", OwnedBy(key))
	w := NewWaterfall[int, int]("t/bind-wf", OwnedBy(key))
	var ed EventDispatcher[int]
	var wd WaterfallDispatcher[int, int]
	want(t, r.Plugin(ownerPlugin("owner", key, func(c *Context) { ed, wd = ev.Bind(c), w.Bind(c) }), nil), Active)
	var heard atomic.Int64
	want(t, r.Plugin(listenEvent(ev, "l", DefaultStage, func(context.Context, int) (any, error) { heard.Add(1); return nil, nil }), nil), Active)
	want(t, r.Plugin(listenWaterfall(w, "m", DefaultStage, func(context.Context, int, Next[int]) (int, error) { return 7, nil }), nil), Active)
	ctx := frontCtx(t)
	if err := ed.Emit(ctx, Unscoped, 1); err != nil || heard.Load() != 1 {
		t.Fatalf("emit: %v, %d", err, heard.Load())
	}
	if v, err := wd.Run(ctx, Unscoped, 1); err != nil || v != 7 {
		t.Fatalf("run: %d, %v", v, err)
	}
	// The launcher's own context may bind a plugin hook, as it may provide any key.
	if err := ev.Bind(r.Context()).Emit(ctx, Unscoped, 1); err != nil || heard.Load() != 2 {
		t.Fatalf("launcher: %v, %d", err, heard.Load())
	}
}

func TestBindByANonProviderFailsItsFiber(t *testing.T) {
	r := newKernel(t)
	key := NewKey[int]("bind-key", 0)
	other := NewKey[int]("bind-other", 0)
	ev := NewEvent[int]("t/bind-ev", OwnedBy(key))
	w := NewWaterfall[int, int]("t/bind-wf", OwnedBy(key))
	for name, p := range map[string]Plugin{
		"no provides":        plug("a", nil, nil, func(c *Context) error { ev.Bind(c); return nil }),
		"another key":        plug("b", nil, []AnyKey{other}, func(c *Context) error { ev.Bind(c); return nil }),
		"waterfall":          plug("c", nil, nil, func(c *Context) error { w.Bind(c); return nil }),
		"kernel hook":        plug("d", nil, []AnyKey{key}, func(c *Context) error { InternalStatus.Bind(c); return nil }),
		"kernel waterfall":   plug("e", nil, []AnyKey{key}, func(c *Context) error { InternalConfig.Bind(c); return nil }),
		"isolated realm key": plug("f", nil, []AnyKey{key}, func(c *Context) error { ev.Bind(c.Isolate(key)); return nil }),
	} {
		if err := failure(t, r.Plugin(p, nil)); !errors.Is(err, ErrNotOwner) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if err := refusal(t, func() { ev.Bind(nil) }); !errors.Is(err, ErrHook) {
		t.Fatalf("nil context: %v", err)
	}
	if err := refusal(t, func() { w.Bind(nil) }); !errors.Is(err, ErrHook) {
		t.Fatalf("nil context: %v", err)
	}
}

func TestBindNeedsTheProviderBindingMapToAdmitTheEntry(t *testing.T) {
	key := NewKey[int]("bound-key", 0, ProviderBound())
	ev := NewEvent[int]("t/bound-ev", OwnedBy(key))
	good := plug("good", nil, []AnyKey{key}, nil)
	var gc *Context
	good.Apply = func(c *Context, _ any) error { gc = c; ev.Bind(c); return key.Provide(c, 1) }
	r := newBound(t, Bindings{key: {good}}, nil)
	want(t, r.Plugin(good, nil), Active)

	// A plugin that declares Provides but is not listed never reaches its Apply: the
	// mount is refused by the binding map.
	var reached atomic.Bool
	bad := plug("bad", nil, []AnyKey{key}, func(c *Context) error { reached.Store(true); ev.Bind(c); return nil })
	if err := failure(t, r.Plugin(bad, nil)); !errors.Is(err, ErrBinding) || reached.Load() {
		t.Fatalf("unlisted provider: %v, reached %v", err, reached.Load())
	}
	// Bind decides by the predicate of Provide: a context with the declaration of
	// the listed fiber and the entry of another is refused.
	forged := *gc
	forged.entry = new(entryToken)
	if err := refusal(t, func() { ev.Bind(&forged) }); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("forged entry: %v", err)
	}
	if !gc.mayProvide(ev.def.owner) || forged.mayProvide(ev.def.owner) {
		t.Fatal("mayProvide")
	}
	if err := key.Provide(&forged, 2); !errors.Is(err, ErrBinding) {
		t.Fatalf("Provide agrees: %v", err)
	}
}

func TestBindOnAnEndedActivationIsRefused(t *testing.T) {
	r := newKernel(t)
	key := NewKey[int]("bind-key", 0)
	ev := NewEvent[int]("t/bind-ev", OwnedBy(key))
	var kept *Context
	f := r.Plugin(ownerPlugin("owner", key, func(c *Context) { kept = c }), nil)
	want(t, f, Active)
	if err := f.Dispose(frontCtx(t)); err != nil {
		t.Fatal(err)
	}
	want(t, f, Disposed)
	if err := refusal(t, func() { ev.Bind(kept) }); !errors.Is(err, ErrInactive) || !errors.Is(err, ErrInactiveEffect) {
		t.Fatalf("late Bind: %v", err)
	}
}

func TestDispatcherDiesWithItsActivation(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	key := NewKey[int]("bind-key", 0)
	ev := NewEvent[int]("t/die-ev", OwnedBy(key))
	w := NewWaterfall[int, int]("t/die-wf", OwnedBy(key))
	var ed EventDispatcher[int]
	var wd WaterfallDispatcher[int, int]
	owner := r.Plugin(ownerPlugin("owner", key, func(c *Context) { ed, wd = ev.Bind(c), w.Bind(c) }), nil)
	want(t, owner, Active)
	var calls atomic.Int64
	want(t, r.Plugin(listenEvent(ev, "l", DefaultStage, func(context.Context, int) (any, error) { calls.Add(1); return "stop", nil }), nil), Active)
	want(t, r.Plugin(listenWaterfall(w, "m", DefaultStage, func(context.Context, int, Next[int]) (int, error) { calls.Add(1); return 1, nil }), nil), Active)
	if err := ed.Emit(ctx, Unscoped, 1); err != nil || calls.Load() != 1 {
		t.Fatalf("alive: %v, %d", err, calls.Load())
	}
	if err := owner.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	want(t, owner, Disposed)
	calls.Store(0)
	checkDead(t, ed, wd)
	if calls.Load() != 0 {
		t.Fatalf("%d listener calls from a dead dispatcher", calls.Load())
	}
}

// checkDead requires every method of the dispatchers to fail with ErrInactive.
func checkDead(t *testing.T, ed EventDispatcher[int], wd WaterfallDispatcher[int, int]) {
	t.Helper()
	ctx := frontCtx(t)
	errs := map[string]error{
		"emit":     ed.Emit(ctx, Unscoped, 1),
		"parallel": ed.Parallel(ctx, Unscoped, 1),
		"run": func() error {
			v, err := wd.Run(ctx, Unscoped, 1)
			if v != 0 {
				t.Errorf("run returned %d", v)
			}
			return err
		}(),
		"serial": func() error { v, err := ed.Serial(ctx, Unscoped, 1); return errNoValue(t, v, err) }(),
		"bail":   func() error { v, err := ed.Bail(ctx, Unscoped, 1); return errNoValue(t, v, err) }(),
	}
	for name, err := range errs {
		if !errors.Is(err, ErrInactive) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func errNoValue(t *testing.T, v any, err error) error {
	t.Helper()
	if v != nil {
		t.Errorf("value %v from a dead dispatcher", v)
	}
	return err
}

func TestZeroDispatchersAreDead(t *testing.T) {
	checkDead(t, EventDispatcher[int]{}, WaterfallDispatcher[int, int]{})
	// A dispatcher of a hook with no context of its own is dead too.
	key := NewKey[int]("bind-key", 0)
	checkDead(t, EventDispatcher[int]{e: NewEvent[int]("t/zero", OwnedBy(key))},
		WaterfallDispatcher[int, int]{w: NewWaterfall[int, int]("t/zero", OwnedBy(key))})
}

func TestARestartNeedsANewBind(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	key := NewKey[int]("bind-key", 0)
	ev := NewEvent[int]("t/restart-ev", OwnedBy(key))
	w := NewWaterfall[int, int]("t/restart-wf", OwnedBy(key))
	var mu sync.Mutex
	var eds []EventDispatcher[int]
	var wds []WaterfallDispatcher[int, int]
	owner := r.Plugin(ownerPlugin("owner", key, func(c *Context) {
		mu.Lock()
		defer mu.Unlock()
		eds, wds = append(eds, ev.Bind(c)), append(wds, w.Bind(c))
	}), nil)
	want(t, owner, Active)
	var calls atomic.Int64
	want(t, r.Plugin(listenEvent(ev, "l", DefaultStage, func(context.Context, int) (any, error) { calls.Add(1); return nil, nil }), nil), Active)
	if err := owner.Update(ctx, nil); err != nil {
		t.Fatal(err)
	}
	want(t, owner, Active)
	mu.Lock()
	defer mu.Unlock()
	if len(eds) != 2 {
		t.Fatalf("%d activations", len(eds))
	}
	checkDead(t, eds[0], wds[0])
	if calls.Load() != 0 {
		t.Fatal("the dispatcher of the earlier activation called a listener")
	}
	if err := eds[1].Emit(ctx, Unscoped, 1); err != nil || calls.Load() != 1 {
		t.Fatalf("new dispatcher: %v, %d", err, calls.Load())
	}
	if _, err := wds[1].Run(ctx, Unscoped, 1); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchRacingTheOwnersDisposalNeverSucceedsAfterIt(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	key := NewKey[int]("bind-key", 0)
	ev := NewEvent[int]("t/orace-ev", OwnedBy(key))
	w := NewWaterfall[int, int]("t/orace-wf", OwnedBy(key))
	var ed EventDispatcher[int]
	var wd WaterfallDispatcher[int, int]
	owner := r.Plugin(ownerPlugin("owner", key, func(c *Context) { ed, wd = ev.Bind(c), w.Bind(c) }), nil)
	want(t, owner, Active)
	var calls atomic.Int64
	entered := make(chan struct{}, 1)
	want(t, r.Plugin(listenEvent(ev, "l", DefaultStage, func(context.Context, int) (any, error) {
		calls.Add(1)
		select {
		case entered <- struct{}{}:
		default:
		}
		return nil, nil
	}), nil), Active)
	want(t, r.Plugin(listenWaterfall(w, "m", DefaultStage, func(_ context.Context, _ int, next Next[int]) (int, error) {
		calls.Add(1)
		return next()
	}), nil), Active)

	var disposed, stop atomic.Bool
	var late atomic.Int64
	var wg sync.WaitGroup
	for i := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				after := disposed.Load() // Read before the call: the call starts after the disposal.
				var err error
				switch i % 3 {
				case 0:
					err = ed.Emit(ctx, Unscoped, 0)
				case 1:
					err = ed.Parallel(ctx, Unscoped, 0)
				default:
					_, err = wd.Run(ctx, Unscoped, 0)
				}
				if after && err == nil {
					late.Add(1)
				}
			}
		}()
	}
	<-entered
	if err := owner.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	want(t, owner, Disposed)
	disposed.Store(true)
	done := calls.Load()
	stop.Store(true)
	wg.Wait()
	if late.Load() != 0 || calls.Load() != done {
		t.Fatalf("%d dispatches succeeded and %d listener calls ran after the disposal", late.Load(), calls.Load()-done)
	}
}

// newListening is a kernel built by New with the given sealed-hook map.
func newListening(t *testing.T, l map[AnyHook][]Plugin) (*Root, error) {
	t.Helper()
	cfg := frontConfig()
	cfg.Listeners = l
	r, err := New(cfg)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = r.Close(context.Background()) })
	return r, nil
}

func TestSealedHookAdmitsTheListedEntriesOnly(t *testing.T) {
	key := NewKey[int]("seal-key", 0)
	ev := NewEvent[int]("loader/seal-ev", OwnedBy(key), Sealed())
	w := NewWaterfall[int, int]("loader/seal-wf", OwnedBy(key), Sealed())
	var heard atomic.Int64
	onEv := func(context.Context, int) (any, error) { heard.Add(1); return nil, nil }
	onWf := func(_ context.Context, _ int, next Next[int]) (int, error) { heard.Add(1); return next() }
	allowedEv := listenEvent(ev, "allowed-ev", DefaultStage, onEv)
	allowedWf := listenWaterfall(w, "allowed-wf", DefaultStage, onWf)
	r, err := newListening(t, map[AnyHook][]Plugin{ev: {allowedEv}, w: {allowedWf}})
	if err != nil {
		t.Fatal(err)
	}
	want(t, r.Plugin(allowedEv, nil), Active)
	want(t, r.Plugin(allowedWf, nil), Active)
	// A copy of a listed plugin is the same entry; a hand-built plugin with the same
	// id and the same Apply is not, even though it declares the hook.
	copied := allowedEv
	copied.ID = "copy"
	want(t, r.Plugin(copied, nil), Active)
	forged := hookPlug("allowed-ev", []HookUse{ev.Use(DefaultStage)}, func(c *Context) error { ev.On(c, DefaultStage, onEv); return nil })
	forged.entry = nil
	if err := failure(t, r.Plugin(forged, nil)); !errors.Is(err, ErrSealed) {
		t.Fatalf("hand-built plugin: %v", err)
	}
	// An unlisted plugin, and a plugin listed for the other hook, are refused.
	for name, p := range map[string]Plugin{
		"unlisted event":     listenEvent(ev, "x", DefaultStage, onEv),
		"unlisted waterfall": listenWaterfall(w, "y", DefaultStage, onWf),
		"wrong hook event":   listenEvent(ev, "allowed-wf-again", DefaultStage, onEv),
	} {
		if name == "wrong hook event" {
			p.entry = allowedWf.entry
		}
		if err := failure(t, r.Plugin(p, nil)); !errors.Is(err, ErrSealed) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// The launcher's own context may listen, as before.
	ev.On(r.Context(), DefaultStage, onEv)
	w.On(r.Context(), DefaultStage, onWf)
	// An admitted listener still has to declare the hook.
	undeclared := hookPlug("undeclared", nil, func(c *Context) error { ev.On(c, DefaultStage, onEv); return nil })
	r2, err := newListening(t, map[AnyHook][]Plugin{ev: {undeclared}})
	if err != nil {
		t.Fatal(err)
	}
	if err := failure(t, r2.Plugin(undeclared, nil)); !errors.Is(err, ErrHookUndeclared) {
		t.Fatalf("undeclared: %v", err)
	}
	// The listeners run: 2 plugins and the launcher on the event, 2 on the waterfall.
	ctx := frontCtx(t)
	if err := ev.Bind(r.Context()).Emit(ctx, Unscoped, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Bind(r.Context()).Run(ctx, Unscoped, 1); err != nil {
		t.Fatal(err)
	}
	if heard.Load() != 5 {
		t.Fatalf("heard = %d, want 5", heard.Load())
	}
}

func TestSealedKernelHookCanBeListedForAPlugin(t *testing.T) {
	p := hookPlug("status-listener", []HookUse{InternalStatus.Use(DefaultStage)}, func(c *Context) error {
		InternalStatus.On(c, DefaultStage, noopAny)
		return nil
	})
	r, err := newListening(t, map[AnyHook][]Plugin{InternalStatus: {p}})
	if err != nil {
		t.Fatal(err)
	}
	want(t, r.Plugin(p, nil), Active)
}

func noopAny(context.Context, LedgerEvent) (any, error) { return nil, nil }

func TestTheTestConstructorSwitchesTheSealedMapOff(t *testing.T) {
	key := NewKey[int]("seal-key", 0)
	ev := NewEvent[int]("loader/seal-ev", OwnedBy(key), Sealed())
	w := NewWaterfall[int, int]("loader/seal-wf", OwnedBy(key), Sealed())
	cfg := frontConfig()
	// The map is ignored: it lists nobody, and it would even be refused by New.
	cfg.Listeners = map[AnyHook][]Plugin{ev: nil, NewEvent[int]("t/open", OwnedBy(key)): nil}
	r := NewForTest(t, cfg)
	want(t, r.Plugin(listenEvent(ev, "any-ev", DefaultStage, noop), nil), Active)
	want(t, r.Plugin(listenWaterfall(w, "any-wf", DefaultStage, func(_ context.Context, _ int, next Next[int]) (int, error) { return next() }), nil), Active)
	want(t, r.Plugin(hookPlug("any-status", []HookUse{InternalStatus.Use(DefaultStage)}, func(c *Context) error {
		InternalStatus.On(c, DefaultStage, noopAny)
		return nil
	}), nil), Active)
	// It does not switch off the declaration.
	undeclared := hookPlug("undeclared", nil, func(c *Context) error { ev.On(c, DefaultStage, noop); return nil })
	if err := failure(t, r.Plugin(undeclared, nil)); !errors.Is(err, ErrHookUndeclared) {
		t.Fatalf("undeclared: %v", err)
	}
	// New with the same map is refused.
	if _, err := newListening(t, cfg.Listeners); err == nil {
		t.Fatal("New accepted a bad listener map")
	}
}

func TestNewChecksTheListenerMap(t *testing.T) {
	key := NewKey[int]("seal-key", 0)
	sealed := NewEvent[int]("loader/seal-ev", OwnedBy(key), Sealed())
	open := NewEvent[int]("t/open", OwnedBy(key))
	openWf := NewWaterfall[int, int]("t/open-wf", OwnedBy(key))
	listed := plug("listed", nil, nil, nil)
	for name, tc := range map[string]struct {
		l    map[AnyHook][]Plugin
		want error
	}{
		"nil hook":             {map[AnyHook][]Plugin{nil: {listed}}, ErrHook},
		"typed nil event":      {map[AnyHook][]Plugin{(*Event[int])(nil): {listed}}, ErrHook},
		"typed nil waterfall":  {map[AnyHook][]Plugin{(*Waterfall[int, int])(nil): {listed}}, ErrHook},
		"event not sealed":     {map[AnyHook][]Plugin{open: {listed}}, ErrSealed},
		"waterfall not sealed": {map[AnyHook][]Plugin{openWf: {listed}}, ErrSealed},
		"never defined":        {map[AnyHook][]Plugin{sealed: {{ID: "raw"}}}, ErrBinding},
		"listed twice":         {map[AnyHook][]Plugin{sealed: {listed, listed}}, ErrDuplicate},
	} {
		r, err := newListening(t, tc.l)
		if !errors.Is(err, tc.want) || r != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Every problem is reported, with the maps for keys.
	_, err := newListening(t, map[AnyHook][]Plugin{open: {listed}, sealed: {listed, listed}})
	if !errors.Is(err, ErrSealed) || !errors.Is(err, ErrDuplicate) {
		t.Fatalf("joined: %v", err)
	}
	// A valid map, an empty list and no map are accepted.
	if _, err := newListening(t, map[AnyHook][]Plugin{sealed: {listed}}); err != nil {
		t.Fatal(err)
	}
	if _, err := newListening(t, map[AnyHook][]Plugin{sealed: nil}); err != nil {
		t.Fatal(err)
	}
	if _, err := newListening(t, nil); err != nil {
		t.Fatal(err)
	}
	if (listenerSet)(nil).allows(sealed.def, listed.entry) || (listenerSet{sealed.def: {}}).allows(sealed.def, nil) {
		t.Fatal("allows")
	}
}

// Decision 12: a waterfall fails closed.

func TestWaterfallFailsClosedOnAListenerError(t *testing.T) {
	testWaterfallFailsClosed(t, func(boom error) WaterfallListener[int, int] {
		return func(context.Context, int, Next[int]) (int, error) { return 5, boom }
	})
}

func TestWaterfallFailsClosedOnAListenerPanic(t *testing.T) {
	testWaterfallFailsClosed(t, func(boom error) WaterfallListener[int, int] {
		return func(context.Context, int, Next[int]) (int, error) { panic(boom) }
	})
}

// testWaterfallFailsClosed mounts, outermost first, a listener that fails ("a"),
// one that would run after it ("b") and the owner's built-in behaviour as the
// innermost stage; the dispatch must fail with a *ListenerError naming "a" and
// neither "b" nor the built-in behaviour may run.
func testWaterfallFailsClosed(t *testing.T, failing func(error) WaterfallListener[int, int]) {
	fx := newHookFx(t)
	w, wd := newWf[int, int](fx, "t/closed", Stages("outer", "inner", "builtin"))
	boom := errors.New("boom")
	var later, builtin atomic.Int64
	want(t, fx.mountIn("", listenWaterfall(w, "a", "outer", failing(boom))), Active)
	want(t, fx.mountIn("", listenWaterfall(w, "b", "inner", func(_ context.Context, _ int, next Next[int]) (int, error) {
		later.Add(1)
		return next()
	})), Active)
	want(t, fx.mountIn("", listenWaterfall(w, "z", "builtin", func(context.Context, int, Next[int]) (int, error) {
		builtin.Add(1)
		return 9, nil
	})), Active)
	v, err := wd.Run(frontCtx(t), Unscoped, 0)
	var le *ListenerError
	if v != 0 || !errors.Is(err, boom) || !errors.As(err, &le) || le.PluginID != "a" {
		t.Fatalf("run: %d, %v", v, err)
	}
	if later.Load() != 0 || builtin.Load() != 0 {
		t.Fatalf("after the failure: later %d, built-in %d", later.Load(), builtin.Load())
	}
	// The kernel's own path, with the built-in behaviour as the root's inner function.
	var inner atomic.Int64
	_, err = runInner(frontCtx(t), w, fx.r, 0, func(context.Context, int) (int, error) { inner.Add(1); return 1, nil })
	if !errors.Is(err, boom) || inner.Load() != 0 {
		t.Fatalf("runInner: %v, inner %d", err, inner.Load())
	}
}

func TestWaterfallFailsClosedWhenALaterListenerFails(t *testing.T) {
	fx := newHookFx(t)
	w, wd := newWf[int, int](fx, "t/closed-late", Stages("outer", "inner"))
	boom := errors.New("boom")
	var after atomic.Int64
	want(t, fx.mountIn("", listenWaterfall(w, "a", "outer", func(_ context.Context, _ int, next Next[int]) (int, error) {
		v, err := next()
		after.Add(1) // A listener that ignores the failure cannot turn it into a success.
		return v + 1, err
	})), Active)
	want(t, fx.mountIn("", listenWaterfall(w, "b", "inner", func(context.Context, int, Next[int]) (int, error) {
		return 3, boom
	})), Active)
	_, err := wd.Run(frontCtx(t), Unscoped, 0)
	var le *ListenerError
	// The outer listener passes the error on, so it is named too: "b" is in the chain.
	if !errors.Is(err, boom) || !errors.As(err, &le) || !strings.Contains(err.Error(), "listener b") {
		t.Fatalf("run: %v", err)
	}
}
