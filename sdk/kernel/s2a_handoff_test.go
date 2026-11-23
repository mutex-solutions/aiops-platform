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

// Tests of the hand-off of a committed config to the running activation (ADR-034
// G13): Fiber.Commit and CommitOp of a Plugin stamped with WithCommit.

// box is what the Decode of a plugin with a hand-off returns in these tests: the
// config resolved for it and the committed value that a hand-off stored in it last,
// as sdk/plugin keeps the cells of the Volatile fields of a config.
type box struct {
	cfg  any
	held atomic.Pointer[box]
}

// now is what a read of the box returns: the last value handed off, else its own.
func (b *box) now() any {
	if c := b.held.Load(); c != nil {
		return c.cfg
	}
	return b.cfg
}

type handOff struct{ applied, committed *box }

// handOffs is a plugin with a hand-off that records each value its Apply received
// and each call of its hand-off. Apply fails for the config "fail", and the
// hand-off panics, after it stored the value, for a config that starts with
// "decoded:boom".
type handOffs struct {
	mu      sync.Mutex
	applied []*box
	calls   []handOff
}

func (h *handOffs) plugin(id string, inject []Dep, decode func(any) (any, error)) Plugin {
	if decode == nil {
		decode = func(raw any) (any, error) { return raw, nil }
	}
	return WithCommit(Define(Plugin{ID: id, Inject: inject,
		Decode: func(raw any) (any, error) {
			v, err := decode(raw)
			if err != nil {
				return nil, err
			}
			return &box{cfg: v}, nil
		},
		Apply: func(_ *Context, cfg any) error {
			b := cfg.(*box)
			h.mu.Lock()
			h.applied = append(h.applied, b)
			h.mu.Unlock()
			if b.cfg == "decoded:fail" {
				return errors.New("apply failed")
			}
			return nil
		}}), func(applied, committed any) {
		a, c := applied.(*box), committed.(*box)
		a.held.Store(c)
		h.mu.Lock()
		h.calls = append(h.calls, handOff{a, c})
		h.mu.Unlock()
		if s, _ := c.cfg.(string); strings.HasPrefix(s, "decoded:boom") {
			panic("hand-off failed")
		}
	})
}

func (h *handOffs) get() ([]*box, []handOff) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]*box(nil), h.applied...), append([]handOff(nil), h.calls...)
}

// load returns the retained raw config of the cell.
func (c *cfgCell) load() any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.raw
}

// hold blocks the first caller whose value is v until release is closed, and closes
// entered when it does. Later callers pass.
type hold struct {
	v                 any
	entered, released chan struct{}
	first             atomic.Bool
}

func newHold(v any) *hold {
	return &hold{v: v, entered: make(chan struct{}), released: make(chan struct{})}
}

func (g *hold) pass(v any) {
	if v == g.v && g.first.CompareAndSwap(false, true) {
		close(g.entered)
		<-g.released
	}
}

func (g *hold) release() { close(g.released) }

func provider(r *Root, svc Key[int]) *Fiber {
	return r.Plugin(plug("x/provider", nil, []AnyKey{svc}, func(c *Context) error { return svc.Provide(c, 1) }), nil)
}

func TestCommitHandsTheConfigToTheRunningActivation(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	var h handOffs
	f := r.Plugin(h.plugin("x/handoff", nil, decodeUpper), "a")
	want(t, f, Active)
	for _, cfg := range []string{"b", "c"} {
		if err := f.Commit(ctx, cfg); err != nil {
			t.Fatal(err)
		}
	}
	want(t, f, Active)
	applied, calls := h.get()
	if len(applied) != 1 || len(calls) != 2 || calls[0].applied != applied[0] || calls[1].applied != applied[0] ||
		calls[0].committed.cfg != "decoded:b" || calls[1].committed.cfg != "decoded:c" || applied[0].now() != "decoded:c" {
		t.Fatalf("applies %d, hand-offs %+v", len(applied), calls)
	}
	// A Plugin without a hand-off: its activation is not recorded and nothing is
	// resolved again.
	var rec cfgRecorder
	g := r.Plugin(rec.plugin(decodeUpper), "a")
	want(t, g, Active)
	if err := g.Commit(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	g.cell.mu.Lock()
	recorded := g.cell.cur.ctx != nil
	g.cell.mu.Unlock()
	if rec.count() != 1 || recorded {
		t.Fatalf("applies %v, activation recorded %v", rec.seen, recorded)
	}
}

func TestCommitOpHandsItsConfigOnceTheBatchIsAdmitted(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	veto := false
	InternalCommit.On(r.Context(), DefaultStage, func(_ context.Context, _ any, next Next[bool]) (bool, error) {
		if veto {
			return false, nil
		}
		return next()
	})
	InternalUpdate.On(r.Context(), DefaultStage, func(_ context.Context, a any, next Next[any]) (any, error) {
		if b, ok := a.(*box); ok && b.cfg == "veto" {
			return nil, nil
		}
		return next()
	})
	var h handOffs
	calls := func() int { _, c := h.get(); return len(c) }
	f := r.Plugin(h.plugin("x/a", nil, nil), "a0")
	want(t, f, Active)
	// Only the last CommitOp of a fiber hands off, before Batch returns.
	if _, err := r.Batch(ctx, CommitOp(f, "a1"), CommitOp(f, "a2")); err != nil {
		t.Fatal(err)
	}
	if applied, c := h.get(); len(c) != 1 || c[0].applied != applied[0] || c[0].committed.cfg != "a2" || applied[0].now() != "a2" {
		t.Fatalf("hand-offs %+v when Batch returned, want one of a2", c)
	}
	// A refused batch, by a veto or by the core, hands nothing off.
	veto = true
	if _, err := r.Batch(ctx, CommitOp(f, "a2")); !errors.Is(err, ErrCommitVetoed) {
		t.Fatalf("veto: %v", err)
	}
	veto = false
	other := r.Plugin(plug("x/other", nil, nil, nil), nil)
	if _, err := r.Batch(ctx, CommitOp(f, "a3"), DisposeOp(other), UpdateOp(other)); err == nil {
		t.Fatal("a batch that updates a fiber it disposes was accepted")
	}
	if n := calls(); n != 1 {
		t.Fatalf("a refused batch handed off: %d hand-offs", n)
	}

	// A batch that ends the activation of the CommitOp's fiber hands it nothing, nor
	// does a CommitOp that a later UpdateOpWith of its fiber follows, whether
	// internal/update vetoes its restart or not.
	mount := func(id string, inject []Dep) *Fiber {
		g := r.Plugin(h.plugin(id, inject, nil), "c0")
		want(t, g, Active)
		return g
	}
	g := mount("x/dispose", nil)
	ends := map[string][]BatchOp{"dispose": {CommitOp(g, "c1"), DisposeOp(g)}}
	g = mount("x/restart", nil)
	ends["restart"] = []BatchOp{CommitOp(g, "c1"), UpdateOp(g)}
	g = mount("x/later", nil)
	ends["changed later"] = []BatchOp{CommitOp(g, "c1"), UpdateOpWith(g, "c2")}
	g = mount("x/vetoed", nil)
	ends["changed later, restart vetoed"] = []BatchOp{CommitOp(g, "c1"), UpdateOpWith(g, "veto")}
	// A child whose parent the batch disposes, the CommitOp before or after the
	// dispose: the loader's check of a config before it disposes a group. The
	// parent's unwind waits on unwind, so the child keeps its activation until the
	// batches are checked.
	unwind := make(chan struct{})
	for _, order := range []string{"child before", "child after"} {
		var child atomic.Pointer[Fiber]
		parent := r.Plugin(plug("x/parent", nil, nil, func(c *Context) error {
			child.Store(c.Plugin(h.plugin("x/child", nil, nil), "c0"))
			c.Defer("unwind", func(context.Context) error { <-unwind; return nil })
			return nil
		}), nil)
		want(t, parent, Active)
		want(t, child.Load(), Active)
		ends[order] = []BatchOp{CommitOp(child.Load(), "c1"), DisposeOp(parent)}
		if order == "child after" {
			ends[order] = []BatchOp{DisposeOp(parent), CommitOp(child.Load(), "c1")}
		}
	}
	// A consumer whose provider the batch restarts.
	svc := NewKey[int]("svc", 0)
	p := provider(r, svc)
	want(t, p, Active)
	ends["consumer"] = []BatchOp{CommitOp(mount("x/consumer", []Dep{Need(svc)}), "c1"), UpdateOp(p)}
	for name, ops := range ends {
		if _, err := r.Batch(ctx, ops...); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if n := calls(); n != 1 {
			t.Fatalf("%s: a CommitOp handed off to an activation its batch ends", name)
		}
	}
	close(unwind)
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	applied, _ := h.get()
	for _, b := range applied {
		if b.now() == "c1" && b.cfg != "c1" {
			t.Fatalf("an ended activation reads c1")
		}
	}
}

func TestARetainDuringTheResolveOfAnActivationResolvesAgain(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	g := newHold("a")
	InternalConfig.On(r.Context(), DefaultStage, func(_ context.Context, raw any, next Next[any]) (any, error) {
		g.pass(raw)
		return next()
	})
	var h handOffs
	f := r.Plugin(h.plugin("x/a", nil, decodeUpper), "a")
	<-g.entered
	if err := f.Commit(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	g.release()
	want(t, f, Active)
	if applied, calls := h.get(); len(applied) != 1 || applied[0].cfg != "decoded:b" || len(calls) != 0 {
		t.Fatalf("applies %d (first %v), hand-offs %+v", len(applied), applied[0].cfg, calls)
	}

	// An activation that resolved the committed config itself is handed nothing: a
	// commit that began with no target resolves its config while the fiber restarts.
	svc := NewKey[int]("svc", 0)
	var w handOffs
	g = newHold("c")
	f = r.Plugin(w.plugin("x/w", []Dep{Need(svc)}, decodeUpper), "a")
	err := window(t, r, svc, f, "c", func() {
		<-g.entered
		if _, err := r.Batch(ctx, UpdateOp(f)); err != nil {
			t.Error(err)
		}
		want(t, f, Active)
		g.release()
	})
	if err != nil {
		t.Fatal(err)
	}
	if applied, calls := w.get(); len(applied) != 2 || applied[1].cfg != "decoded:c" || len(calls) != 0 {
		t.Fatalf("applies %d, hand-offs %+v", len(applied), calls)
	}
}

// window commits cfg to a fiber that waits for svc, and lets the provider arrive
// and the fiber become ACTIVE while internal/commit holds the commit, so that the
// commit began with no target and finds an activation that applied the older
// config. It returns the error of Commit, which runs to its end after.
func window(t *testing.T, r *Root, svc Key[int], f *Fiber, cfg any, after func()) error {
	t.Helper()
	g := newHold(cfg)
	InternalCommit.On(r.Context(), DefaultStage, func(_ context.Context, raw any, next Next[bool]) (bool, error) {
		g.pass(raw)
		return next()
	})
	want(t, f, Pending)
	done := make(chan error, 1)
	go func() { done <- f.Commit(frontCtx(t), cfg) }()
	<-g.entered
	want(t, provider(r, svc), Active)
	want(t, f, Active)
	g.release()
	if after != nil {
		after()
	}
	return <-done
}

func TestCommitResolvesForAnActivationThatBeganWithoutATarget(t *testing.T) {
	r := newKernel(t)
	svc := NewKey[int]("svc", 0)
	var h handOffs
	f := r.Plugin(h.plugin("x/a", []Dep{Need(svc)}, decodeUpper), "a")
	if err := window(t, r, svc, f, "b", nil); err != nil {
		t.Fatal(err)
	}
	want(t, f, Active)
	applied, calls := h.get()
	if len(applied) != 1 || applied[0].cfg != "decoded:a" || len(calls) != 1 || calls[0].committed.cfg != "decoded:b" || applied[0].now() != "decoded:b" {
		t.Fatalf("applies %d, hand-offs %+v", len(applied), calls)
	}
}

func TestCommitKeepsTheActivationWhenTheHandOffCannotBeMade(t *testing.T) {
	t.Run("a config that does not resolve", func(t *testing.T) {
		r := newKernel(t)
		ctx := frontCtx(t)
		svc := NewKey[int]("svc", 0)
		var h handOffs
		f := r.Plugin(h.plugin("x/a", []Dep{Need(svc)}, decodeUpper), "a")
		err := window(t, r, svc, f, 1, nil)
		if err == nil || !strings.Contains(err.Error(), "want string") {
			t.Fatalf("Commit returned %v, want the error of Decode", err)
		}
		want(t, f, Active)
		if applied, calls := h.get(); len(applied) != 1 || len(calls) != 0 || applied[0].now() != "decoded:a" {
			t.Fatalf("applies %d, hand-offs %+v", len(applied), calls)
		}
		if _, err := r.Batch(ctx, UpdateOp(f)); err != nil {
			t.Fatal(err)
		}
		if err := failure(t, f); !strings.Contains(err.Error(), "want string") {
			t.Fatalf("the next activation failed with %v", err)
		}
	})
	t.Run("a resolve that a newer retain passed", func(t *testing.T) {
		r := newKernel(t)
		ctx := frontCtx(t)
		svc := NewKey[int]("svc", 0)
		g := newHold("b")
		InternalConfig.On(r.Context(), DefaultStage, func(_ context.Context, raw any, next Next[any]) (any, error) {
			g.pass(raw)
			return next()
		})
		var h handOffs
		f := r.Plugin(h.plugin("x/a", []Dep{Need(svc)}, decodeUpper), "a")
		err := window(t, r, svc, f, "b", func() {
			<-g.entered
			if err := f.Commit(ctx, "c"); err != nil {
				t.Error(err)
			}
			g.release()
		})
		if err != nil {
			t.Fatal(err)
		}
		if applied, calls := h.get(); len(applied) != 1 || len(calls) != 1 || calls[0].committed.cfg != "decoded:c" || applied[0].now() != "decoded:c" {
			t.Fatalf("applies %d, hand-offs %+v", len(applied), calls)
		}
	})
	t.Run("a hand-off that panics", func(t *testing.T) {
		r := newKernel(t)
		ctx := frontCtx(t)
		var h handOffs
		f := r.Plugin(h.plugin("x/a", nil, decodeUpper), "a")
		want(t, f, Active)
		var pe *PanicError
		if err := f.Commit(ctx, "boom"); !errors.As(err, &pe) {
			t.Fatalf("Commit returned %v, want a *PanicError", err)
		}
		if _, err := r.Batch(ctx, CommitOp(f, "boom2")); err != nil {
			t.Fatal(err)
		}
		want(t, f, Active)
		applied, calls := h.get()
		if len(applied) != 1 || len(calls) != 2 || applied[0].now() != "decoded:boom2" {
			t.Fatalf("applies %d, hand-offs %+v", len(applied), calls)
		}
		if _, err := r.Batch(ctx, UpdateOp(f)); err != nil {
			t.Fatal(err)
		}
		want(t, f, Active)
		if applied, _ = h.get(); len(applied) != 2 || applied[1].cfg != "decoded:boom2" {
			t.Fatalf("the next activation applied %v", applied[len(applied)-1].cfg)
		}
	})
}

func TestCommitHandsNothingToAnEndedActivation(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	var h handOffs
	f := r.Plugin(h.plugin("x/a", nil, decodeUpper), "fail")
	if err := failure(t, f); err == nil {
		t.Fatal("no error")
	}
	if err := f.Commit(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	if applied, calls := h.get(); len(applied) != 1 || len(calls) != 0 {
		t.Fatalf("applies %d, hand-offs %+v", len(applied), calls)
	}
	if s, err := f.State(ctx); err != nil || s != Failed {
		t.Fatalf("state %s, %v: the commit restarted the fiber", s, err)
	}
}

func TestCommitsAndRestartsRaceToTheLastCommit(t *testing.T) {
	const commits, restarts = 300, 100
	r := newKernel(t)
	ctx := frontCtx(t)
	var h handOffs
	f := r.Plugin(h.plugin("x/race", nil, nil), 0)
	want(t, f, Active)
	var wg sync.WaitGroup
	wg.Go(func() {
		for range restarts {
			if _, err := r.Batch(ctx, UpdateOp(f)); err != nil {
				t.Error(err)
				return
			}
		}
	})
	for i := 1; i <= commits; i++ {
		if err := f.Commit(ctx, i); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	want(t, f, Active)
	applied, calls := h.get()
	last := map[*box]int{}
	for _, c := range calls {
		n := c.committed.cfg.(int)
		if n <= c.applied.cfg.(int) || n <= last[c.applied] {
			t.Fatalf("an activation applied with %d and handed %d was handed %d", c.applied.cfg, last[c.applied], n)
		}
		last[c.applied] = n
	}
	if got := applied[len(applied)-1].now(); got != commits {
		t.Fatalf("the settled activation reads %v, want %d (%d applies, %d hand-offs)", got, commits, len(applied), len(calls))
	}
}

func TestCommitAfterAVetoedUpdate(t *testing.T) {
	for _, update := range []string{"Update", "UpdateOpWith"} {
		t.Run(update, func(t *testing.T) {
			r := newKernel(t)
			ctx := frontCtx(t)
			InternalUpdate.On(r.Context(), DefaultStage, func(context.Context, any, Next[any]) (any, error) { return nil, nil })
			var h handOffs
			f := r.Plugin(h.plugin("x/a", nil, decodeUpper), "a")
			want(t, f, Active)
			var err error
			if update == "Update" {
				err = f.Update(ctx, "b")
			} else {
				_, err = r.Batch(ctx, UpdateOpWith(f, "b"))
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := f.Commit(ctx, "c"); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Batch(ctx, CommitOp(f, "d")); err != nil {
				t.Fatal(err)
			}
			want(t, f, Active)
			applied, calls := h.get()
			if len(applied) != 1 || applied[0].cfg != "decoded:a" || len(calls) != 2 ||
				calls[0].committed.cfg != "decoded:c" || calls[1].committed.cfg != "decoded:d" || applied[0].now() != "decoded:d" {
				t.Fatalf("applies %d, hand-offs %+v", len(applied), calls)
			}
		})
	}
}
