// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// cfgRecorder is a plugin whose Apply records every config it receives.
type cfgRecorder struct {
	mu   sync.Mutex
	seen []any
}

func (c *cfgRecorder) plugin(decode func(any) (any, error)) Plugin {
	return Plugin{ID: "x/cfg", Decode: decode, Apply: func(_ *Context, cfg any) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.seen = append(c.seen, cfg)
		return nil
	}}
}

func (c *cfgRecorder) last() any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seen[len(c.seen)-1]
}

func (c *cfgRecorder) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.seen)
}

func decodeUpper(raw any) (any, error) {
	s, ok := raw.(string)
	if !ok {
		return nil, fmt.Errorf("config is %T, want string", raw)
	}
	return "decoded:" + s, nil
}

func TestApplyGetsTheRawConfigWithoutDecode(t *testing.T) {
	r := newKernel(t)
	rec := &cfgRecorder{}
	want(t, r.Plugin(rec.plugin(nil), 42), Active)
	if rec.last() != 42 {
		t.Fatalf("Apply got %v", rec.last())
	}
}

func TestApplyGetsWhatDecodeReturns(t *testing.T) {
	r := newKernel(t)
	rec := &cfgRecorder{}
	want(t, r.Plugin(rec.plugin(decodeUpper), "a"), Active)
	if rec.last() != "decoded:a" {
		t.Fatalf("Apply got %v", rec.last())
	}
}

func TestADecodeErrorOrPanicFailsTheFiber(t *testing.T) {
	r := newKernel(t)
	rec := &cfgRecorder{}
	if err := failure(t, r.Plugin(rec.plugin(decodeUpper), 7)); err == nil {
		t.Fatal("no error")
	}
	boom := errors.New("boom")
	var pe *PanicError
	err := failure(t, r.Plugin(rec.plugin(func(any) (any, error) { panic(boom) }), nil))
	if !errors.As(err, &pe) || !errors.Is(err, boom) {
		t.Fatalf("a Decode panic: %v", err)
	}
	if rec.count() != 0 {
		t.Fatal("Apply ran after a failed Decode")
	}
}

func TestAPluginWithoutApplyIsNotDecoded(t *testing.T) {
	r := newKernel(t)
	f := r.Plugin(Plugin{ID: "x/none", Decode: func(any) (any, error) { return nil, errors.New("must not run") }}, nil)
	want(t, f, Active)
}

func TestUpdateRestartsWithTheNewConfig(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	rec := &cfgRecorder{}
	f := r.Plugin(rec.plugin(decodeUpper), "a")
	want(t, f, Active)
	if err := f.Update(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	want(t, f, Active)
	if rec.count() != 2 || rec.last() != "decoded:b" {
		t.Fatalf("seen %v", rec.seen)
	}
}

func TestAFailedUpdateDecodeKeepsTheRunningActivation(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	rec := &cfgRecorder{}
	f := r.Plugin(rec.plugin(decodeUpper), "a")
	want(t, f, Active)
	if err := f.Update(ctx, 1); err == nil {
		t.Fatal("the update decoded")
	}
	panicky := &cfgRecorder{}
	g := r.Plugin(panicky.plugin(func(raw any) (any, error) {
		if raw == "bad" {
			panic("bad config")
		}
		return raw, nil
	}), "good")
	want(t, g, Active)
	var pe *PanicError
	if err := g.Update(ctx, "bad"); !errors.As(err, &pe) {
		t.Fatalf("a Decode panic on update: %v", err)
	}
	want(t, f, Active)
	want(t, g, Active)
	if rec.count() != 1 || panicky.count() != 1 {
		t.Fatalf("a failed update restarted the fiber: %d %d", rec.count(), panicky.count())
	}
	// The retained config is the old one: a restart without a config uses it.
	if _, err := r.Batch(ctx, UpdateOp(f)); err != nil {
		t.Fatal(err)
	}
	want(t, f, Active)
	if rec.count() != 2 || rec.last() != "decoded:a" {
		t.Fatalf("seen %v", rec.seen)
	}
}

func TestUpdateOfAFailedOrPendingFiberRetainsTheConfig(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	rec := &cfgRecorder{}
	f := r.Plugin(rec.plugin(decodeUpper), 1)
	if err := failure(t, f); err == nil {
		t.Fatal("no error")
	}
	// A bad config on a failed fiber is retained and fails it again at activation.
	if err := f.Update(ctx, 2); err != nil {
		t.Fatal(err)
	}
	if err := failure(t, f); err == nil {
		t.Fatal("a bad config activated")
	}
	if err := f.Update(ctx, "ok"); err != nil {
		t.Fatal(err)
	}
	want(t, f, Active)
	if rec.last() != "decoded:ok" {
		t.Fatalf("seen %v", rec.seen)
	}

	svc := NewKey[int]("svc", 0)
	p := rec.plugin(decodeUpper)
	p.Inject = []Dep{Need(svc)}
	pending := r.Plugin(p, "old")
	want(t, pending, Pending)
	if err := pending.Update(ctx, "new"); err != nil {
		t.Fatal(err)
	}
	want(t, pending, Pending)
	want(t, r.Plugin(plug("prov", nil, []AnyKey{svc}, func(c *Context) error { return svc.Provide(c, 1) }), nil), Active)
	want(t, pending, Active)
	if rec.last() != "decoded:new" {
		t.Fatalf("seen %v", rec.seen)
	}
}

func TestUpdateOfADisposedFiberFailsAndAWhenFiberIgnoresConfig(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	f := r.Plugin(plug("gone", nil, nil, nil), nil)
	want(t, f, Active)
	if err := f.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	want(t, f, Disposed)
	if err := f.Update(ctx, "x"); err == nil {
		t.Fatal("update of a disposed fiber succeeded")
	}
	var runs int
	var w *Fiber
	parent := r.Plugin(plug("p", nil, nil, func(c *Context) error {
		w = c.When(nil, func(*Context) error { runs++; return nil })
		return nil
	}), nil)
	want(t, parent, Active)
	want(t, w, Active)
	if err := w.Update(ctx, "ignored"); err != nil {
		t.Fatal(err)
	}
	want(t, w, Active)
	if runs != 2 {
		t.Fatalf("runs = %d", runs)
	}
	if err := r.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.Update(ctx, "x"); !errors.Is(err, errClosed) {
		t.Fatalf("update after close: %v", err)
	}
}

func TestBatchMountedFibersTakeConfigUpdates(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	rec := &cfgRecorder{}
	fibers, err := r.Batch(ctx, MountOp(rec.plugin(decodeUpper), "a"))
	if err != nil {
		t.Fatal(err)
	}
	want(t, fibers[0], Active)
	if err := fibers[0].Update(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	want(t, fibers[0], Active)
	if rec.last() != "decoded:b" {
		t.Fatalf("seen %v", rec.seen)
	}
}

func TestConfigListenersTransformAndVeto(t *testing.T) {
	r := newKernel(t)
	other := newKernel(t)
	rec, otherRec := &cfgRecorder{}, &cfgRecorder{}
	var calls int
	InternalConfig.On(r.Context(), DefaultStage, func(ctx context.Context, raw any, next Next[any]) (any, error) {
		calls++
		eff, err := next()
		if err != nil {
			return nil, err
		}
		return fmt.Sprintf("%v+%v", raw, eff), nil
	})
	want(t, r.Plugin(rec.plugin(nil), "raw"), Active)
	// The built-in behaviour is the innermost: it returns the raw value.
	if rec.last() != "raw+raw" || calls != 1 {
		t.Fatalf("seen %v after %d calls", rec.seen, calls)
	}
	// A listener of another kernel never sees this one's configs.
	want(t, other.Plugin(otherRec.plugin(nil), "raw"), Active)
	if otherRec.last() != "raw" || calls != 1 {
		t.Fatalf("seen %v after %d calls", otherRec.seen, calls)
	}
}

func TestAConfigListenerErrorFailsTheFiberAndTheUpdate(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	boom := errors.New("unresolved reference")
	InternalConfig.On(r.Context(), DefaultStage, func(_ context.Context, raw any, next Next[any]) (any, error) {
		if raw == "bad" {
			return nil, boom
		}
		return next()
	})
	rec := &cfgRecorder{}
	if err := failure(t, r.Plugin(rec.plugin(nil), "bad")); !errors.Is(err, boom) {
		t.Fatalf("mount: %v", err)
	}
	f := r.Plugin(rec.plugin(nil), "good")
	want(t, f, Active)
	var le *ListenerError
	if err := f.Update(ctx, "bad"); !errors.Is(err, boom) || !errors.As(err, &le) {
		t.Fatalf("update: %v", err)
	}
	want(t, f, Active)
	if rec.count() != 1 {
		t.Fatalf("seen %v", rec.seen)
	}
}

func TestCommitListenersSeeTheRawConfigBeforeItIsRetained(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	var committed []any
	InternalCommit.On(r.Context(), DefaultStage, func(_ context.Context, raw any) (any, error) {
		committed = append(committed, raw)
		return nil, errors.New("a failing listener is contained")
	})
	rec := &cfgRecorder{}
	f := r.Plugin(rec.plugin(nil), "a")
	want(t, f, Active)
	if len(committed) != 0 {
		t.Fatalf("a mount committed: %v", committed)
	}
	if err := f.Update(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	want(t, f, Active)
	if len(committed) != 1 || committed[0] != "b" || rec.last() != "b" {
		t.Fatalf("committed %v, seen %v", committed, rec.seen)
	}
	// A decode failure commits nothing.
	d := &cfgRecorder{}
	g := r.Plugin(d.plugin(decodeUpper), "a")
	want(t, g, Active)
	if err := g.Update(ctx, 1); err == nil || len(committed) != 1 {
		t.Fatalf("update: %v, committed %v", err, committed)
	}
}

func TestACommitThatEndsTheContextStopsTheUpdate(t *testing.T) {
	r := newKernel(t)
	ctx, cancel := context.WithCancel(frontCtx(t))
	InternalCommit.On(r.Context(), DefaultStage, func(context.Context, any) (any, error) { cancel(); return nil, nil })
	rec := &cfgRecorder{}
	f := r.Plugin(rec.plugin(nil), "a")
	want(t, f, Active)
	if err := f.Update(ctx, "b"); !errors.Is(err, context.Canceled) {
		t.Fatalf("update: %v", err)
	}
	want(t, f, Active)
	if rec.count() != 1 {
		t.Fatalf("seen %v", rec.seen)
	}
}

func TestUpdateListenersWrapTheRestartAndMayVetoIt(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	rec := &cfgRecorder{}
	f := r.Plugin(rec.plugin(decodeUpper), "a")
	want(t, f, Active)
	var payload []any
	veto := true
	InternalUpdate.On(r.Context(), DefaultStage, func(_ context.Context, resolved any, next Next[any]) (any, error) {
		payload = append(payload, resolved)
		if veto {
			return "vetoed", nil
		}
		return next()
	})
	if err := f.Update(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	want(t, f, Active)
	if rec.count() != 1 || len(payload) != 1 || payload[0] != "decoded:b" {
		t.Fatalf("veto: seen %v, payload %v", rec.seen, payload)
	}
	// The vetoed config stays retained: it applies at the next restart.
	veto = false
	if err := f.Update(ctx, "c"); err != nil {
		t.Fatal(err)
	}
	want(t, f, Active)
	if rec.count() != 2 || rec.last() != "decoded:c" {
		t.Fatalf("seen %v", rec.seen)
	}
}

func TestAnUpdateListenerFailureIsTheUpdatesFailure(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	rec := &cfgRecorder{}
	f := r.Plugin(rec.plugin(nil), "a")
	want(t, f, Active)
	boom := errors.New("refused")
	InternalUpdate.On(r.Context(), DefaultStage, func(context.Context, any, Next[any]) (any, error) { return nil, boom })
	if err := f.Update(ctx, "b"); !errors.Is(err, boom) {
		t.Fatalf("update: %v", err)
	}
	want(t, f, Active)
	if rec.count() != 1 {
		t.Fatalf("seen %v", rec.seen)
	}
}

// The candidate config of an update is resolved whenever the fiber has a desired
// target, as the prototype does: internal/config sees it while the fiber is still
// LOADING behind a gate (corpus D20), not at the next activation.
func TestUpdateResolvesTheCandidateWhileTheFiberLoads(t *testing.T) {
	r := newKernel(t)
	var resolved []any
	var mu sync.Mutex
	InternalConfig.On(r.Context(), DefaultStage, func(_ context.Context, raw any, next Next[any]) (any, error) {
		mu.Lock()
		resolved = append(resolved, raw)
		mu.Unlock()
		return next()
	})
	entered, release := make(chan struct{}, 1), make(chan struct{})
	f := r.Plugin(Define(Plugin{ID: "p/loading", Apply: func(*Context, any) error {
		entered <- struct{}{}
		<-release
		return nil
	}}), "v1")
	<-entered
	defer func() { close(release); want(t, f, Active) }()
	if err := f.Update(frontCtx(t), "v2"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(resolved) != 2 || resolved[1] != "v2" {
		t.Fatalf("internal/config saw %v while the fiber was LOADING, want [v1 v2]: the candidate is resolved at the update", resolved)
	}
}

// A candidate that fails to resolve is refused in every state with a target: the
// error returns, nothing is retained or committed, and the activation in progress
// goes on (the prototype's ApplyUpdate; ADR-034 owner decision 13).
func TestUpdateRefusesABadCandidateWhileTheFiberLoads(t *testing.T) {
	r := newKernel(t)
	var commits atomic.Int64
	InternalCommit.On(r.Context(), DefaultStage, func(context.Context, any) (any, error) { commits.Add(1); return nil, nil })
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var seen cfgRecorder
	p := seen.plugin(func(raw any) (any, error) {
		if raw == "bad" {
			return nil, errors.New("bad candidate")
		}
		return raw, nil
	})
	apply := p.Apply
	p.Apply = func(c *Context, v any) error {
		entered <- struct{}{}
		<-release
		return apply(c, v)
	}
	f := r.Plugin(p, "v1")
	<-entered
	if err := f.Update(frontCtx(t), "bad"); err == nil {
		t.Fatal("a bad candidate was accepted")
	}
	close(release)
	want(t, f, Active)
	if commits.Load() != 0 {
		t.Fatalf("%d commits for a refused candidate", commits.Load())
	}
	if err := f.Update(frontCtx(t), "v2"); err != nil {
		t.Fatal(err)
	}
	want(t, f, Active)
	seen.mu.Lock()
	defer seen.mu.Unlock()
	if last := seen.seen[len(seen.seen)-1]; last != "v2" {
		t.Fatalf("applied %v, want the last good candidate v2", seen.seen)
	}
}

// An update of a fiber whose disposal was requested is refused as inactive before
// anything is resolved or committed.
func TestUpdateOfADisposedFiberIsInactive(t *testing.T) {
	r := newKernel(t)
	f := r.Plugin(Define(Plugin{ID: "p/gone", Apply: func(*Context, any) error { return nil }}), "v1")
	want(t, f, Active)
	if err := f.Dispose(frontCtx(t)); err != nil {
		t.Fatal(err)
	}
	want(t, f, Disposed)
	if err := f.Update(frontCtx(t), "v2"); !errors.Is(err, ErrInactive) {
		t.Fatalf("%v, want ErrInactive", err)
	}
}
