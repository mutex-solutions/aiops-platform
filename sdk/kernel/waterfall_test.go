// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
)

func TestWaterfallComposesOutermostFirstAndIsOrderedByStage(t *testing.T) {
	fx := newHookFx(t)
	w, wD := newWf[int, int](fx, "t/wf", Stages("outer", "inner"))
	var order []string
	var mu sync.Mutex
	wrap := func(id string) WaterfallListener[int, int] {
		return func(_ context.Context, a int, next Next[int]) (int, error) {
			record(&order, &mu, id+" before")
			v, err := next()
			record(&order, &mu, id+" after")
			return v + 1, err
		}
	}
	// The innermost stage is the owner's built-in behaviour: a listener like another.
	builtin := func(_ context.Context, a int, _ Next[int]) (int, error) {
		record(&order, &mu, "builtin")
		return a * 10, nil
	}
	want(t, fx.mountIn("", listenWaterfall(w, "z", "inner", builtin)), Active)
	want(t, fx.mountIn("", listenWaterfall(w, "b", "outer", wrap("b"))), Active)
	want(t, fx.mountIn("", listenWaterfall(w, "a", "outer", wrap("a"))), Active)
	v, err := wD.Run(frontCtx(t), Unscoped, 3)
	if err != nil || v != 32 {
		t.Fatalf("result = %d, %v", v, err)
	}
	if !slices.Equal(order, []string{"a before", "b before", "builtin", "b after", "a after"}) {
		t.Fatalf("order = %v", order)
	}
	// An empty chain returns the zero value.
	_, emptyD := newWf[int, int](fx, "t/wf-empty")
	if v, err := emptyD.Run(frontCtx(t), Unscoped, 3); v != 0 || err != nil {
		t.Fatalf("empty = %d, %v", v, err)
	}
}

func TestWaterfallListenerThatSkipsNextVetoesTheRest(t *testing.T) {
	fx := newHookFx(t)
	w, wD := newWf[int, int](fx, "t/veto")
	var inner atomic.Int64
	want(t, fx.mountIn("", listenWaterfall(w, "a", DefaultStage, func(context.Context, int, Next[int]) (int, error) { return 0, nil })), Active)
	want(t, fx.mountIn("", listenWaterfall(w, "b", DefaultStage, func(_ context.Context, _ int, _ Next[int]) (int, error) { inner.Add(1); return 7, nil })), Active)
	if v, err := wD.Run(frontCtx(t), Unscoped, 1); v != 0 || err != nil || inner.Load() != 0 {
		t.Fatalf("veto: %d, %v, inner %d", v, err, inner.Load())
	}
}

func TestWaterfallNextTwiceLateAndConcurrent(t *testing.T) {
	fx := newHookFx(t)
	ctx := frontCtx(t)
	var inner atomic.Int64
	count := func(context.Context, int, Next[int]) (int, error) { inner.Add(1); return 1, nil }

	twice, twiceD := newWf[int, int](fx, "t/twice")
	var second error
	want(t, fx.mountIn("", listenWaterfall(twice, "a", DefaultStage, func(_ context.Context, _ int, next Next[int]) (int, error) {
		v, err := next()
		_, second = next()
		return v, err
	})), Active)
	want(t, fx.mountIn("", listenWaterfall(twice, "b", DefaultStage, count)), Active)
	if v, err := twiceD.Run(ctx, Unscoped, 0); v != 1 || err != nil || !errors.Is(second, ErrNext) || inner.Load() != 1 {
		t.Fatalf("twice: %d, %v, second %v, inner %d", v, err, second, inner.Load())
	}

	// A listener that does not handle the second error fails the dispatch with it.
	fail, failD := newWf[int, int](fx, "t/twice-fail")
	want(t, fx.mountIn("", listenWaterfall(fail, "a", DefaultStage, func(_ context.Context, _ int, next Next[int]) (int, error) {
		if _, err := next(); err != nil {
			return 0, err
		}
		return next()
	})), Active)
	if _, err := failD.Run(ctx, Unscoped, 0); !errors.Is(err, ErrNext) {
		t.Fatalf("unhandled twice: %v", err)
	}

	// next saved and called after its listener returned.
	late, lateD := newWf[int, int](fx, "t/late")
	var saved Next[int]
	want(t, fx.mountIn("", listenWaterfall(late, "a", DefaultStage, func(_ context.Context, _ int, next Next[int]) (int, error) { saved = next; return 0, nil })), Active)
	want(t, fx.mountIn("", listenWaterfall(late, "b", DefaultStage, count)), Active)
	inner.Store(0)
	if _, err := lateD.Run(ctx, Unscoped, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := saved(); !errors.Is(err, ErrNextExpired) || inner.Load() != 0 {
		t.Fatalf("late: %v, inner %d", err, inner.Load())
	}

	// Two goroutines call next at once: one wins.
	conc, concD := newWf[int, int](fx, "t/conc")
	want(t, fx.mountIn("", listenWaterfall(conc, "a", DefaultStage, func(_ context.Context, _ int, next Next[int]) (int, error) {
		var wg sync.WaitGroup
		var errs [2]error
		for i := range errs {
			wg.Add(1)
			go func() { defer wg.Done(); _, errs[i] = next() }()
		}
		wg.Wait()
		return 0, errors.Join(errs[0], errs[1])
	})), Active)
	want(t, fx.mountIn("", listenWaterfall(conc, "b", DefaultStage, count)), Active)
	inner.Store(0)
	if _, err := concD.Run(ctx, Unscoped, 0); !errors.Is(err, ErrNext) || inner.Load() != 1 {
		t.Fatalf("concurrent: %v, inner %d", err, inner.Load())
	}
}

func TestWaterfallRunWaitsForANextStartedOnAnotherGoroutine(t *testing.T) {
	fx := newHookFx(t)
	w, wD := newWf[int, int](fx, "t/async")
	started, release := make(chan struct{}), make(chan struct{})
	want(t, fx.mountIn("", listenWaterfall(w, "a", DefaultStage, func(_ context.Context, _ int, next Next[int]) (int, error) {
		go func() { _, _ = next() }()
		<-started // The continuation is running; this listener returns without waiting.
		return 0, nil
	})), Active)
	want(t, fx.mountIn("", listenWaterfall(w, "b", DefaultStage, func(context.Context, int, Next[int]) (int, error) {
		close(started)
		<-release
		return 0, nil
	})), Active)
	done := make(chan error, 1)
	go func() { _, err := wD.Run(frontCtx(t), Unscoped, 0); done <- err }()
	<-started
	select {
	case err := <-done:
		t.Fatalf("Run returned with its continuation still running: %v", err)
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestWaterfallListenerFailureFailsTheDispatch(t *testing.T) {
	fx := newHookFx(t)
	ctx := frontCtx(t)
	boom := errors.New("boom")
	for name, fn := range map[string]WaterfallListener[int, int]{
		"error": func(context.Context, int, Next[int]) (int, error) { return 5, boom },
		"panic": func(context.Context, int, Next[int]) (int, error) { panic(boom) },
	} {
		w, wD := newWf[int, int](fx, "t/"+name)
		want(t, fx.mountIn("", listenWaterfall(w, "a", DefaultStage, fn)), Active)
		v, err := wD.Run(ctx, Unscoped, 0)
		var le *ListenerError
		if v != 0 || !errors.Is(err, boom) || !errors.As(err, &le) || le.PluginID != "a" {
			t.Fatalf("%s: %d, %v", name, v, err)
		}
	}
}

func TestWaterfallStopsWhenTheDispatchContextEnds(t *testing.T) {
	fx := newHookFx(t)
	w, wD := newWf[int, int](fx, "t/wf-cancel")
	ctx, cancel := context.WithCancel(frontCtx(t))
	var inner atomic.Int64
	want(t, fx.mountIn("", listenWaterfall(w, "a", DefaultStage, func(_ context.Context, _ int, next Next[int]) (int, error) {
		cancel()
		return next()
	})), Active)
	want(t, fx.mountIn("", listenWaterfall(w, "b", DefaultStage, func(context.Context, int, Next[int]) (int, error) { inner.Add(1); return 1, nil })), Active)
	if _, err := wD.Run(ctx, Unscoped, 0); !errors.Is(err, context.Canceled) || inner.Load() != 0 {
		t.Fatalf("%v, inner %d", err, inner.Load())
	}
	// A listener that ends the context and returns normally is reported too.
	late, lateD := newWf[int, int](fx, "t/wf-cancel-late")
	ctx2, cancel2 := context.WithCancel(frontCtx(t))
	want(t, fx.mountIn("", listenWaterfall(late, "a", DefaultStage, func(context.Context, int, Next[int]) (int, error) { cancel2(); return 1, nil })), Active)
	if _, err := lateD.Run(ctx2, Unscoped, 0); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestWaterfallAudienceFiltersTheChain(t *testing.T) {
	fx := newHookFx(t)
	ctx := frontCtx(t)
	w, wD := newWf[int, int](fx, "t/wf-aud")
	var a, b, plain atomic.Int64
	for _, l := range []struct {
		id, scope string
		n         *atomic.Int64
	}{{"a", "A", &a}, {"b", "B", &b}, {"c", "", &plain}} {
		want(t, fx.mountIn(l.scope, listenWaterfall(w, l.id, DefaultStage, func(_ context.Context, _ int, next Next[int]) (int, error) {
			l.n.Add(1)
			return next()
		})), Active)
	}
	if _, err := wD.Run(ctx, fx.aud("A"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := wD.Run(ctx, Unscoped, 0); err != nil {
		t.Fatal(err)
	}
	if a.Load() != 1 || b.Load() != 0 || plain.Load() != 1 {
		t.Fatalf("A=%d B=%d unscoped=%d", a.Load(), b.Load(), plain.Load())
	}
	boom := errors.New("predicate")
	bad, err := fx.scope.Audience(fx.prov, func(string) bool { panic(boom) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wD.Run(ctx, bad, 0); !errors.Is(err, boom) {
		t.Fatalf("predicate panic: %v", err)
	}
}

func TestWaterfallListenerLeavesWithItsFiber(t *testing.T) {
	fx := newHookFx(t)
	ctx := frontCtx(t)
	w, wD := newWf[int, int](fx, "t/wf-leave")
	f := fx.mountIn("", listenWaterfall(w, "a", DefaultStage, func(context.Context, int, Next[int]) (int, error) { return 4, nil }))
	want(t, f, Active)
	if v, _ := wD.Run(ctx, Unscoped, 0); v != 4 {
		t.Fatalf("v = %d", v)
	}
	if err := f.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	want(t, f, Disposed)
	if v, err := wD.Run(ctx, Unscoped, 0); v != 0 || err != nil {
		t.Fatalf("after disposal: %d, %v", v, err)
	}
}
