// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// hookFx is a kernel with one scope whose owner is a provider fiber, so that a
// test can build audiences and scoped listener contexts.
type hookFx struct {
	t     *testing.T
	r     *Root
	key   Key[int]
	scope Scope[string]
	prov  *Context
	fiber *Fiber
}

func newHookFx(t *testing.T, cfgs ...func(*Config)) *hookFx {
	t.Helper()
	cfg := frontConfig()
	for _, f := range cfgs {
		f(&cfg)
	}
	fx := &hookFx{t: t, r: NewForTest(t, cfg), key: NewKey[int]("hk-owner", 0)}
	fx.scope = NewScope[string](fx.key)
	fx.fiber = fx.r.Plugin(plug("hk-prov", nil, []AnyKey{fx.key}, func(c *Context) error {
		fx.prov = c
		return fx.key.Provide(c, 1)
	}), nil)
	want(t, fx.fiber, Active)
	return fx
}

// aud is the audience of the listeners scoped to v.
func (fx *hookFx) aud(v string) Audience {
	fx.t.Helper()
	a, err := fx.scope.Audience(fx.prov, func(s string) bool { return s == v })
	if err != nil {
		fx.t.Fatal(err)
	}
	return a
}

// mountIn mounts p below the scope value v, or at the root when v is empty.
func (fx *hookFx) mountIn(v string, p Plugin) *Fiber {
	fx.t.Helper()
	if v == "" {
		return fx.r.Plugin(p, nil)
	}
	sc, err := fx.scope.With(fx.prov, v)
	if err != nil {
		fx.t.Fatal(err)
	}
	return sc.Plugin(p, nil)
}

func hookPlug(id string, uses []HookUse, apply func(*Context) error) Plugin {
	p := plug(id, nil, nil, apply)
	p.Hooks = uses
	return p
}

// listenEvent is a plugin that declares and registers one Event listener.
func listenEvent[A any](e *Event[A], id, stage string, fn EventListener[A]) Plugin {
	return hookPlug(id, []HookUse{e.Use(stage)}, func(c *Context) error { e.On(c, stage, fn); return nil })
}

func listenWaterfall[A, R any](w *Waterfall[A, R], id, stage string, fn WaterfallListener[A, R]) Plugin {
	return hookPlug(id, []HookUse{w.Use(stage)}, func(c *Context) error { w.On(c, stage, fn); return nil })
}

func record(into *[]string, mu *sync.Mutex, s string) {
	mu.Lock()
	defer mu.Unlock()
	*into = append(*into, s)
}

// refusal returns the error of the refused call that fn panics with.
func refusal(t testing.TB, fn func()) error {
	t.Helper()
	rc, ok := catch(fn).(*refusedCall)
	if !ok {
		t.Fatal("call was not refused")
	}
	return rc.err
}

func noop(context.Context, int) (any, error) { return nil, nil }

func TestHookEventChainOrderIsStageThenPluginID(t *testing.T) {
	fx := newHookFx(t)
	ev := NewEvent[int]("t/order", Stages("outer", "inner"))
	var got []string
	var mu sync.Mutex
	tag := func(s string) EventListener[int] {
		return func(context.Context, int) (any, error) { record(&got, &mu, s); return nil, nil }
	}
	// Mounted c, b, a: the chain does not follow registration.
	want(t, fx.mountIn("", listenEvent(ev, "c", "outer", tag("c"))), Active)
	want(t, fx.mountIn("", listenEvent(ev, "b", "inner", tag("b"))), Active)
	want(t, fx.mountIn("", hookPlug("a", []HookUse{ev.Use("outer")}, func(c *Context) error {
		ev.On(c, "outer", tag("a1"))
		ev.On(c, "outer", tag("a2")) // Same stage and id: registration order.
		return nil
	})), Active)
	if err := ev.Emit(frontCtx(t), Unscoped, 1); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"a1", "a2", "c", "b"}) {
		t.Fatalf("order = %v", got)
	}
	if ev.Name() != "t/order" || ev.Owner() != nil || ev.Use("inner").Hook() != "t/order" || ev.Use("inner").Stage() != "inner" {
		t.Fatal("accessors")
	}
}

func TestHookEventModes(t *testing.T) {
	fx := newHookFx(t)
	ctx := frontCtx(t)
	ev := NewEvent[int]("t/modes")
	var calls atomic.Int64
	results := []any{nil, false, 0, "later"}
	for i, v := range results {
		want(t, fx.mountIn("", listenEvent(ev, string(rune('a'+i)), DefaultStage, func(context.Context, int) (any, error) {
			calls.Add(1)
			return v, nil
		})), Active)
	}
	// Emit ignores values: every listener runs.
	if err := ev.Emit(ctx, Unscoped, 0); err != nil || calls.Load() != 4 {
		t.Fatalf("emit: %v, %d calls", err, calls.Load())
	}
	// nil and false go on; the zero value 0 stops, and is the result.
	for name, mode := range map[string]func(context.Context, Audience, int) (any, error){"bail": ev.Bail, "serial": ev.Serial} {
		calls.Store(0)
		if v, err := mode(ctx, Unscoped, 0); err != nil || v != 0 || calls.Load() != 3 {
			t.Fatalf("%s: %v, %v, %d calls", name, v, err, calls.Load())
		}
	}
	// A chain that never stops returns nil.
	none := NewEvent[int]("t/none")
	want(t, fx.mountIn("", listenEvent(none, "n", DefaultStage, noop)), Active)
	if v, err := none.Bail(ctx, Unscoped, 0); v != nil || err != nil {
		t.Fatalf("no stop: %v, %v", v, err)
	}
	// An empty chain is not an error.
	empty := NewEvent[int]("t/empty")
	if err := empty.Parallel(ctx, Unscoped, 0); err != nil {
		t.Fatal(err)
	}
}

func TestHookEventParallelRunsListenersAtTheSameTime(t *testing.T) {
	fx := newHookFx(t)
	ev := NewEvent[int]("t/parallel")
	var arrived sync.WaitGroup
	arrived.Add(3)
	var ran atomic.Int64
	for _, id := range []string{"a", "b", "c"} {
		want(t, fx.mountIn("", listenEvent(ev, id, DefaultStage, func(context.Context, int) (any, error) {
			arrived.Done()
			arrived.Wait() // Returns only if all three run at once.
			ran.Add(1)
			return nil, nil
		})), Active)
	}
	if err := ev.Parallel(frontCtx(t), Unscoped, 0); err != nil || ran.Load() != 3 {
		t.Fatalf("parallel: %v, %d ran", err, ran.Load())
	}
}

func TestHookEventListenerFailureIsSkippedAndLogged(t *testing.T) {
	var logged []error
	fx := newHookFx(t, func(c *Config) { c.ObserverLog = func(err error) { logged = append(logged, err) } })
	ctx := frontCtx(t)
	ev := NewEvent[int]("t/fail")
	boom := errors.New("boom")
	var after atomic.Int64
	want(t, fx.mountIn("", listenEvent(ev, "a", DefaultStage, func(context.Context, int) (any, error) { return nil, boom })), Active)
	want(t, fx.mountIn("", listenEvent(ev, "b", DefaultStage, func(context.Context, int) (any, error) { panic(boom) })), Active)
	want(t, fx.mountIn("", listenEvent(ev, "c", DefaultStage, func(context.Context, int) (any, error) { after.Add(1); return "stop", nil })), Active)
	for name, run := range map[string]func() error{
		"emit":     func() error { return ev.Emit(ctx, Unscoped, 0) },
		"parallel": func() error { return ev.Parallel(ctx, Unscoped, 0) },
		"bail":     func() error { _, err := ev.Bail(ctx, Unscoped, 0); return err },
	} {
		if err := run(); err != nil {
			t.Fatalf("%s returned the listener failure: %v", name, err)
		}
	}
	if after.Load() != 3 {
		t.Fatalf("a failing listener stopped the chain: %d", after.Load())
	}
	if len(logged) != 6 {
		t.Fatalf("logged %d failures, want 6: %v", len(logged), logged)
	}
	// Parallel logs in no fixed order: look the failures up by their fiber.
	byID := map[string][]error{}
	for _, err := range logged {
		var le *ListenerError
		if !errors.As(err, &le) || !errors.Is(err, boom) {
			t.Fatalf("attribution: %v", err)
		}
		byID[le.PluginID] = append(byID[le.PluginID], err)
	}
	var pe *PanicError
	if len(byID["a"]) != 3 || len(byID["b"]) != 3 || errors.As(byID["a"][0], &pe) || !errors.As(byID["b"][0], &pe) {
		t.Fatalf("by fiber: %v", byID)
	}
	if !strings.Contains(byID["b"][0].Error(), "panicked") || !strings.Contains(byID["a"][0].Error(), "listener a") {
		t.Fatalf("text: %v / %v", byID["a"][0], byID["b"][0])
	}
	var np PanicError
	if np.Unwrap() != nil {
		t.Fatal("a panic that is not an error unwraps to nothing")
	}
}

func TestHookEventLogSinkPanicIsContained(t *testing.T) {
	fx := newHookFx(t, func(c *Config) { c.ObserverLog = func(error) { panic("sink") } })
	ev := NewEvent[int]("t/sink")
	want(t, fx.mountIn("", listenEvent(ev, "a", DefaultStage, func(context.Context, int) (any, error) { return nil, errors.New("x") })), Active)
	if err := ev.Emit(frontCtx(t), Unscoped, 0); err != nil {
		t.Fatal(err)
	}
}

func TestHookEventFailureWithNoSinkIsDropped(t *testing.T) {
	fx := newHookFx(t)
	ev := NewEvent[int]("t/nosink")
	want(t, fx.mountIn("", listenEvent(ev, "a", DefaultStage, func(context.Context, int) (any, error) { return nil, errors.New("x") })), Active)
	if err := ev.Emit(frontCtx(t), Unscoped, 0); err != nil {
		t.Fatal(err)
	}
}

func TestHookEventParallelAudiencePanicFailsTheDispatch(t *testing.T) {
	fx := newHookFx(t)
	ev := NewEvent[int]("t/par-pred")
	want(t, fx.mountIn("A", listenEvent(ev, "a", DefaultStage, noop)), Active)
	boom := errors.New("predicate")
	aud, err := fx.scope.Audience(fx.prov, func(string) bool { panic(boom) })
	if err != nil {
		t.Fatal(err)
	}
	if err := ev.Parallel(frontCtx(t), aud, 0); !errors.Is(err, boom) {
		t.Fatal(err)
	}
}

func TestHookEventStopsWhenTheDispatchContextEnds(t *testing.T) {
	fx := newHookFx(t)
	ev := NewEvent[int]("t/cancel")
	ctx, cancel := context.WithCancel(frontCtx(t))
	var second atomic.Int64
	want(t, fx.mountIn("", listenEvent(ev, "a", DefaultStage, func(context.Context, int) (any, error) { cancel(); return nil, nil })), Active)
	want(t, fx.mountIn("", listenEvent(ev, "b", DefaultStage, func(context.Context, int) (any, error) { second.Add(1); return nil, nil })), Active)
	if err := ev.Emit(ctx, Unscoped, 0); !errors.Is(err, context.Canceled) || second.Load() != 0 {
		t.Fatalf("emit: %v, second %d", err, second.Load())
	}
	// Ended before the dispatch: nothing runs, in every mode.
	if _, err := ev.Bail(ctx, Unscoped, 0); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := ev.Parallel(ctx, Unscoped, 0); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// Ended by the last listener: the dispatch reports it.
	one := NewEvent[int]("t/cancel-last")
	ctx2, cancel2 := context.WithCancel(frontCtx(t))
	want(t, fx.mountIn("", listenEvent(one, "a", DefaultStage, func(context.Context, int) (any, error) { cancel2(); return nil, nil })), Active)
	if err := one.Parallel(ctx2, Unscoped, 0); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestHookOnRefusals(t *testing.T) {
	fx := newHookFx(t)
	ev := NewEvent[int]("t/refuse", Stages("outer", "inner"))
	fn := EventListener[int](noop)
	apply := func(c *Context) error { ev.On(c, "outer", fn); return nil }

	// Undeclared: no Hooks at all, or another stage than the declared one.
	if err := failure(t, fx.mountIn("", hookPlug("none", nil, apply))); !errors.Is(err, ErrHookUndeclared) {
		t.Fatalf("no Hooks: %v", err)
	}
	if err := failure(t, fx.mountIn("", hookPlug("other", []HookUse{ev.Use("inner")}, apply))); !errors.Is(err, ErrHookUndeclared) {
		t.Fatalf("other stage: %v", err)
	}
	// Declared but undefined stage in On.
	bad := hookPlug("stage", []HookUse{ev.Use("outer")}, func(c *Context) error { ev.On(c, "nope", fn); return nil })
	if err := failure(t, fx.mountIn("", bad)); !errors.Is(err, ErrStage) {
		t.Fatalf("On stage: %v", err)
	}
	// A listener of another hook of the same kind is not declared either.
	other := NewEvent[int]("t/other")
	if err := failure(t, fx.mountIn("", hookPlug("x", []HookUse{other.Use(DefaultStage)}, apply))); !errors.Is(err, ErrHookUndeclared) {
		t.Fatalf("other hook: %v", err)
	}
	// A bad declaration refuses the mount: unknown stage, zero use, duplicate.
	for name, tc := range map[string]struct {
		uses []HookUse
		want error
	}{
		"unknown stage": {[]HookUse{ev.Use("nope")}, ErrStage},
		"zero":          {[]HookUse{{}}, ErrHook},
		"duplicate":     {[]HookUse{ev.Use("outer"), ev.Use("outer")}, ErrDuplicate},
	} {
		if err := failure(t, fx.mountIn("", hookPlug(name, tc.uses, nil))); !errors.Is(err, tc.want) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if (HookUse{}).Hook() != "" || (HookUse{}).Stage() != "" || ev.Use("nope").Stage() != "" {
		t.Fatal("invalid uses name nothing")
	}
	// A nil function and a nil context are refused.
	if err := failure(t, fx.mountIn("", hookPlug("nilfn", []HookUse{ev.Use("outer")}, func(c *Context) error { ev.On(c, "outer", nil); return nil }))); !errors.Is(err, ErrHook) {
		t.Fatalf("nil fn: %v", err)
	}
	if err := refusal(t, func() { ev.On(nil, "outer", fn) }); !errors.Is(err, ErrHook) {
		t.Fatalf("nil context: %v", err)
	}
	w := NewWaterfall[int, int]("t/w")
	if err := refusal(t, func() { w.On(fx.prov, DefaultStage, nil) }); !errors.Is(err, ErrHook) {
		t.Fatalf("nil waterfall fn: %v", err)
	}
	// A declared On succeeds, also from a When body, which shares its plugin's hooks.
	var viaWhen *Fiber
	ok := hookPlug("ok", []HookUse{ev.Use("outer")}, func(c *Context) error {
		viaWhen = c.When(nil, func(w *Context) error { ev.On(w, "outer", fn); return nil })
		return nil
	})
	want(t, fx.mountIn("", ok), Active)
	want(t, viaWhen, Active)
	if got := ledger(t, viaWhen); !slices.Contains(got, "hook t/refuse/outer") {
		t.Fatalf("ledger = %v", got)
	}
}

func TestHookOnAnInactiveFiberIsRefused(t *testing.T) {
	fx := newHookFx(t)
	ev := NewEvent[int]("t/inactive")
	var kept *Context
	f := fx.mountIn("", hookPlug("p", []HookUse{ev.Use(DefaultStage)}, func(c *Context) error { kept = c; return nil }))
	want(t, f, Active)
	if err := f.Dispose(frontCtx(t)); err != nil {
		t.Fatal(err)
	}
	want(t, f, Disposed)
	if err := refusal(t, func() { ev.On(kept, DefaultStage, noop) }); !errors.Is(err, ErrInactiveEffect) {
		t.Fatalf("late On: %v", err)
	}
	if err := ev.Emit(frontCtx(t), Unscoped, 0); err != nil {
		t.Fatal(err)
	}
}

func TestHookSealedKernelHooksListenOnlyFromTheLauncher(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	var seen atomic.Int64
	hookStatus.On(r.Context(), DefaultStage, func(context.Context, any) (any, error) { seen.Add(1); return nil, nil })
	if err := hookStatus.Emit(ctx, Unscoped, nil); err != nil || seen.Load() != 1 {
		t.Fatalf("launcher listener: %v, %d", err, seen.Load())
	}
	// A plugin may declare it, and is still refused.
	p := hookPlug("sneak", []HookUse{hookStatus.Use(DefaultStage)}, func(c *Context) error {
		hookStatus.On(c, DefaultStage, func(context.Context, any) (any, error) { return nil, nil })
		return nil
	})
	if err := failure(t, r.Plugin(p, nil)); !errors.Is(err, ErrSealed) {
		t.Fatalf("plugin on a sealed hook: %v", err)
	}
	// Nobody outside the kernel defines an internal/ hook; Sealed seals another.
	if p := catch(func() { NewEvent[int]("internal/mine") }); p == nil {
		t.Fatal("internal/ name accepted")
	}
	if p := catch(func() { NewWaterfall[int, int]("internal/mine") }); p == nil {
		t.Fatal("internal/ waterfall name accepted")
	}
	sealed := NewEvent[int]("loader/sealed", Sealed())
	sp := hookPlug("sneak2", []HookUse{sealed.Use(DefaultStage)}, func(c *Context) error { sealed.On(c, DefaultStage, noop); return nil })
	if err := failure(t, r.Plugin(sp, nil)); !errors.Is(err, ErrSealed) {
		t.Fatalf("Sealed option: %v", err)
	}
	// The six kernel hooks exist under their contract names.
	names := []string{hookPlugin.Name(), hookStatus.Name(), hookService.Name(), hookConfig.Name(), hookUpdate.Name(), hookCommit.Name()}
	if !slices.Equal(names, []string{"internal/plugin", "internal/status", "internal/service", "internal/config", "internal/update", "internal/commit"}) {
		t.Fatalf("names = %v", names)
	}
}

func TestHookDefinitionsAreChecked(t *testing.T) {
	for name, fn := range map[string]func(){
		"no name":         func() { NewEvent[int]("") },
		"empty stage":     func() { NewEvent[int]("t/x", Stages("a", "")) },
		"repeated stage":  func() { NewWaterfall[int, int]("t/x", Stages("a", "b", "a")) },
		"unnamed default": func() { NewWaterfall[int, int]("") },
	} {
		if catch(fn) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	key := NewKey[int]("hk-owned", 0)
	if o := NewEvent[int]("t/own", OwnedBy(key)).Owner(); o == nil || o.Name() != "hk-owned" {
		t.Fatalf("owner = %v", o)
	}
	if o := NewWaterfall[int, int]("t/own", OwnedBy(key)).Owner(); o == nil || o.Name() != "hk-owned" {
		t.Fatalf("waterfall owner = %v", o)
	}
	w := NewWaterfall[int, int]("t/plain", Stages("s"))
	if w.Owner() != nil || w.Name() != "t/plain" || w.Use("s").Stage() != "s" {
		t.Fatal("waterfall accessors")
	}
}

func TestHookAudienceFiltersListenersByScope(t *testing.T) {
	fx := newHookFx(t)
	ctx := frontCtx(t)
	ev := NewEvent[int]("t/aud")
	var a, b, plain atomic.Int64
	for _, l := range []struct {
		id, scope string
		n         *atomic.Int64
	}{{"a", "A", &a}, {"b", "B", &b}, {"c", "", &plain}} {
		want(t, fx.mountIn(l.scope, listenEvent(ev, l.id, DefaultStage, func(context.Context, int) (any, error) { l.n.Add(1); return nil, nil })), Active)
	}
	audA := fx.aud("A")
	runs := []func(Audience) error{
		func(x Audience) error { return ev.Emit(ctx, x, 0) },
		func(x Audience) error { return ev.Parallel(ctx, x, 0) },
		func(x Audience) error { _, err := ev.Serial(ctx, x, 0); return err },
		func(x Audience) error { _, err := ev.Bail(ctx, x, 0); return err },
	}
	for _, run := range runs {
		if err := run(audA); err != nil {
			t.Fatal(err)
		}
		if err := run(Unscoped); err != nil {
			t.Fatal(err)
		}
	}
	// A scoped audience reaches its own scope only; Unscoped reaches the unscoped.
	if a.Load() != 4 || b.Load() != 0 || plain.Load() != 4 {
		t.Fatalf("A=%d B=%d unscoped=%d", a.Load(), b.Load(), plain.Load())
	}
}

func TestHookAudienceIsDecidedBeforeTheFirstListenerRuns(t *testing.T) {
	fx := newHookFx(t)
	ev := NewEvent[int]("t/frozen")
	var open atomic.Bool
	open.Store(true)
	aud, err := fx.scope.Audience(fx.prov, func(string) bool { return open.Load() })
	if err != nil {
		t.Fatal(err)
	}
	var first, second int
	want(t, fx.mountIn("A", listenEvent(ev, "a", DefaultStage, func(context.Context, int) (any, error) { first++; open.Store(false); return nil, nil })), Active)
	want(t, fx.mountIn("A", listenEvent(ev, "b", DefaultStage, func(context.Context, int) (any, error) { second++; return nil, nil })), Active)
	if err := ev.Emit(frontCtx(t), aud, 0); err != nil || first != 1 || second != 1 {
		t.Fatalf("first dispatch: %v %d %d", err, first, second)
	}
	// The next dispatch decides again.
	if err := ev.Emit(frontCtx(t), aud, 0); err != nil || first != 1 || second != 1 {
		t.Fatalf("second dispatch: %v %d %d", err, first, second)
	}
}

func TestHookAudiencePredicatePanicFailsTheDispatch(t *testing.T) {
	fx := newHookFx(t)
	ev := NewEvent[int]("t/pred")
	var ran atomic.Int64
	want(t, fx.mountIn("A", listenEvent(ev, "a", DefaultStage, func(context.Context, int) (any, error) { ran.Add(1); return nil, nil })), Active)
	boom := errors.New("predicate")
	aud, err := fx.scope.Audience(fx.prov, func(string) bool { panic(boom) })
	if err != nil {
		t.Fatal(err)
	}
	if err := ev.Emit(frontCtx(t), aud, 0); !errors.Is(err, boom) || ran.Load() != 0 {
		t.Fatalf("emit: %v, ran %d", err, ran.Load())
	}
}

func TestHookScopedHookRefusesAnUnscopedDispatch(t *testing.T) {
	fx := newHookFx(t)
	ctx := frontCtx(t)
	ev := NewEvent[int]("t/scoped", Scoped())
	w := NewWaterfall[int, int]("t/scoped-w", Scoped())
	for name, err := range map[string]error{
		"emit":     ev.Emit(ctx, Unscoped, 0),
		"parallel": ev.Parallel(ctx, Unscoped, 0),
		"waterfall": func() error {
			_, err := w.Run(ctx, Unscoped, 0)
			return err
		}(),
	} {
		if !errors.Is(err, ErrUnscoped) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if err := ev.Emit(ctx, fx.aud("A"), 0); err != nil {
		t.Fatal(err)
	}
}

func TestHookListenerLeavesWithItsFiber(t *testing.T) {
	fx := newHookFx(t)
	ctx := frontCtx(t)
	ev := NewEvent[int]("t/leave", Stages("s"))
	var calls atomic.Int64
	count := func(context.Context, int) (any, error) { calls.Add(1); return nil, nil }
	keep := fx.mountIn("", listenEvent(ev, "keep", "s", count))
	gone := fx.mountIn("", listenEvent(ev, "gone", "s", count))
	want(t, keep, Active)
	want(t, gone, Active)
	if got := ledger(t, gone); !slices.Contains(got, "hook t/leave/s") {
		t.Fatalf("ledger = %v", got)
	}
	if err := ev.Emit(ctx, Unscoped, 0); err != nil || calls.Load() != 2 {
		t.Fatalf("%v, %d", err, calls.Load())
	}
	if err := gone.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	want(t, gone, Disposed)
	calls.Store(0)
	if err := ev.Emit(ctx, Unscoped, 0); err != nil || calls.Load() != 1 {
		t.Fatalf("after disposal: %v, %d", err, calls.Load())
	}
	// An update reruns Apply: the listener is registered once, not twice.
	if err := keep.Update(ctx); err != nil {
		t.Fatal(err)
	}
	want(t, keep, Active)
	calls.Store(0)
	if err := ev.Emit(ctx, Unscoped, 0); err != nil || calls.Load() != 1 {
		t.Fatalf("after update: %v, %d", err, calls.Load())
	}
}

func TestHookListenerInFlightHoldsDisposalAndNewDispatchSkipsIt(t *testing.T) {
	fx := newHookFx(t)
	ctx := frontCtx(t)
	ev := NewEvent[int]("t/lease")
	inside, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	var kept *Context
	f := fx.mountIn("", hookPlug("slow", []HookUse{ev.Use(DefaultStage)}, func(c *Context) error {
		kept = c
		ev.On(c, DefaultStage, func(context.Context, int) (any, error) {
			if calls.Add(1) == 1 {
				close(inside)
				<-release
			}
			return nil, nil
		})
		return nil
	}))
	want(t, f, Active)
	done := make(chan error, 1)
	go func() { done <- ev.Emit(ctx, Unscoped, 0) }()
	<-inside
	if err := f.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	// The fiber is quiescing: its listener is not called again, and the disposal
	// has not completed while the call is in flight.
	<-kept.Std().Done()
	if err := ev.Emit(ctx, Unscoped, 0); err != nil || calls.Load() != 1 {
		t.Fatalf("dispatch during quiesce: %v, %d", err, calls.Load())
	}
	if s, _ := f.State(ctx); s == Disposed {
		t.Fatal("disposed with a listener in flight")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	want(t, f, Disposed)
}

func TestHookDispatchRacingDisposalNeverCallsAfterIt(t *testing.T) {
	fx := newHookFx(t)
	ctx := frontCtx(t)
	ev := NewEvent[int]("t/race")
	w := NewWaterfall[int, int]("t/race-w")
	var calls atomic.Int64
	entered := make(chan struct{}, 1)
	f := fx.mountIn("", hookPlug("racer", []HookUse{ev.Use(DefaultStage), w.Use(DefaultStage)}, func(c *Context) error {
		ev.On(c, DefaultStage, func(context.Context, int) (any, error) {
			calls.Add(1)
			select {
			case entered <- struct{}{}:
			default:
			}
			return nil, nil
		})
		w.On(c, DefaultStage, func(_ context.Context, _ int, next Next[int]) (int, error) { calls.Add(1); return next() })
		return nil
	}))
	want(t, f, Active)
	var stop atomic.Bool
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				switch i % 3 {
				case 0:
					_ = ev.Emit(ctx, Unscoped, 0)
				case 1:
					_ = ev.Parallel(ctx, Unscoped, 0)
				default:
					_, _ = w.Run(ctx, Unscoped, 0)
				}
			}
		}()
	}
	<-entered
	if err := f.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	want(t, f, Disposed)
	done := calls.Load()
	stop.Store(true)
	wg.Wait()
	if calls.Load() != done {
		t.Fatalf("%d listener calls after the disposal completed", calls.Load()-done)
	}
}

func TestHookRegistrationRacingDispatchAndDisposal(t *testing.T) {
	fx := newHookFx(t)
	ctx := frontCtx(t)
	ev := NewEvent[int]("t/churn")
	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			_ = ev.Emit(ctx, Unscoped, 0)
		}
	}()
	for i := range 20 {
		f := fx.mountIn("", listenEvent(ev, string(rune('a'+i%5)), DefaultStage, noop))
		want(t, f, Active)
		if i%2 == 0 {
			if err := f.Dispose(ctx); err != nil {
				t.Fatal(err)
			}
			want(t, f, Disposed)
		}
	}
	stop.Store(true)
	wg.Wait()
}

func TestHookQuiescingListenerInASnapshotIsSkipped(t *testing.T) {
	fx := newHookFx(t)
	ctx := frontCtx(t)
	ev := NewEvent[int]("t/skip")
	w := NewWaterfall[int, int]("t/skip-w")
	var victim *Fiber
	var hits atomic.Int64
	// "a" runs first, takes "z" out and waits until it is gone; "z" is still in the
	// snapshot the dispatch already holds.
	killer := func() {
		if err := victim.Dispose(ctx); err != nil {
			t.Error(err)
		}
		if err := victim.Await(ctx); err != nil {
			t.Error(err)
		}
	}
	want(t, fx.mountIn("", listenEvent(ev, "a", DefaultStage, func(context.Context, int) (any, error) { killer(); return nil, nil })), Active)
	victim = fx.mountIn("", listenEvent(ev, "z", DefaultStage, func(context.Context, int) (any, error) { hits.Add(1); return nil, nil }))
	want(t, victim, Active)
	if err := ev.Emit(ctx, Unscoped, 0); err != nil || hits.Load() != 0 {
		t.Fatalf("event: %v, %d", err, hits.Load())
	}
	want(t, fx.mountIn("", listenWaterfall(w, "a", DefaultStage, func(_ context.Context, _ int, next Next[int]) (int, error) { killer(); return next() })), Active)
	victim = fx.mountIn("", listenWaterfall(w, "z", DefaultStage, func(context.Context, int, Next[int]) (int, error) { hits.Add(1); return 9, nil }))
	want(t, victim, Active)
	if v, err := w.Run(ctx, Unscoped, 0); err != nil || v != 0 || hits.Load() != 0 {
		t.Fatalf("waterfall: %v, %v, %d", v, err, hits.Load())
	}
}

func TestHookForeignKernelListenerNeverMatchesAScopedAudience(t *testing.T) {
	fx := newHookFx(t)
	other := newKernel(t)
	ev := NewEvent[int]("t/foreign")
	var n atomic.Int64
	want(t, other.Plugin(listenEvent(ev, "o", DefaultStage, func(context.Context, int) (any, error) { n.Add(1); return nil, nil }), nil), Active)
	if err := ev.Emit(frontCtx(t), fx.aud("A"), 0); err != nil || n.Load() != 0 {
		t.Fatalf("%v, %d", err, n.Load())
	}
}

func BenchmarkEventDispatch(b *testing.B) {
	for _, n := range []int{0, 1, 4} {
		b.Run(string(rune('0'+n))+"-listeners", func(b *testing.B) {
			r := NewForTest(b, frontConfig())
			ev := NewEvent[int]("bench/event")
			for i := range n {
				f := r.Plugin(listenEvent(ev, string(rune('a'+i)), DefaultStage, noop), nil)
				if err := f.Await(context.Background()); err != nil {
					b.Fatal(err)
				}
			}
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := ev.Emit(ctx, Unscoped, 1); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkWaterfallDispatch(b *testing.B) {
	for _, n := range []int{0, 1, 4} {
		b.Run(string(rune('0'+n))+"-listeners", func(b *testing.B) {
			r := NewForTest(b, frontConfig())
			w := NewWaterfall[int, int]("bench/waterfall")
			for i := range n {
				f := r.Plugin(listenWaterfall(w, string(rune('a'+i)), DefaultStage, func(_ context.Context, _ int, next Next[int]) (int, error) { return next() }), nil)
				if err := f.Await(context.Background()); err != nil {
					b.Fatal(err)
				}
			}
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := w.Run(ctx, Unscoped, 1); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
