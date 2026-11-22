// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Tests of the child mount and the update with a config as batch operations
// (ADR-059 decision 2).

func TestChildMountOpMountsUnderTheContextInOneBatch(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	parentCtx := make(chan *Context, 1)
	parent := r.Plugin(plug("s1/parent", nil, nil, func(c *Context) error { parentCtx <- c; return nil }), nil)
	want(t, parent, Active)
	pc := <-parentCtx
	x := r.Plugin(plug("s1/x", nil, nil, nil), nil)
	y := r.Plugin(plug("s1/y", nil, nil, nil), nil)
	want(t, x, Active)
	want(t, y, Active)
	before := x.ref.id

	got, err := r.Batch(ctx, pc.MountOp(plug("s1/child", nil, nil, nil), nil), MountOp(plug("s1/top", nil, nil, nil), nil), UpdateOp(x), DisposeOp(y))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || got[2] != x || got[3] != y || got[0] == nil || got[1] == nil || got[0].ID() <= before || got[1].ID() != got[0].ID()+1 {
		t.Fatalf("batch result %+v", got)
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	for f, state := range map[*Fiber]State{got[0]: Active, got[1]: Active, x: Active, y: Disposed} {
		if s, err := f.State(ctx); err != nil || s != state {
			t.Fatalf("fiber %d is %s (%v), want %s", f.ID(), s, err, state)
		}
	}
	// The child is an effect of the parent, so it goes with it; the top-level mount does not.
	if err := parent.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	if s, _ := got[0].State(ctx); s != Disposed {
		t.Fatalf("child of a disposed parent is %s", s)
	}
	if s, _ := got[1].State(ctx); s != Active {
		t.Fatalf("top-level fiber is %s", s)
	}
	// The root context mounts a child of the root.
	rootChild, err := r.Batch(ctx, r.Context().MountOp(plug("s1/rootchild", nil, nil, nil), nil))
	if err != nil {
		t.Fatal(err)
	}
	want(t, rootChild[0], Active)
}

func TestChildMountOpInheritsTheEntryAndTheChain(t *testing.T) {
	bound := NewKey[int]("s1.bound", 1, ConsumerBound())
	chain := NewKey[[]string]("s1.chain", 1, InterceptConfig[string]())
	parentEntry := Define(Plugin{ID: "s1/entry"})
	stranger := Define(Plugin{ID: "s1/stranger"})
	provider := Define(Plugin{ID: "s1/chainprov", Provides: []AnyKey{chain}, Apply: func(c *Context, _ any) error {
		return chain.ProvideFor(c, func(caller *Context) []string { return Intercepts[string](caller, chain) })
	}})
	r := newBound(t, nil, Bindings{bound: {parentEntry}})
	ctx := frontCtx(t)
	if err := bound.Provide(r.Context(), 7); err != nil {
		t.Fatal(err)
	}
	want(t, r.Plugin(provider, nil), Active)
	ctxs := make(chan *Context, 2)
	capture := func(entry Plugin, id string) *Context {
		p := entry
		p.ID = id
		p.Apply = func(c *Context, _ any) error { ctxs <- c.Intercept(chain, "from-"+id); return nil }
		want(t, r.Plugin(p, nil), Active)
		return <-ctxs
	}
	pc := capture(parentEntry, "s1/entry")
	var seen []string
	read := Plugin{ID: "s1/reader", Inject: []Dep{Need(bound), NeedKey(chain)}, Apply: func(c *Context, _ any) error {
		seen = chain.Of(c)
		if bound.Of(c) != 7 {
			return errors.New("bound value")
		}
		return nil
	}}
	// The child was never defined, so it has the parent's entry, which the consumer
	// map lists, and the intercept chain of the context it is mounted from.
	got, err := r.Batch(ctx, pc.MountOp(read, nil))
	if err != nil {
		t.Fatal(err)
	}
	want(t, got[0], Active)
	if !slices.Equal(seen, []string{"from-s1/entry"}) {
		t.Fatalf("intercept chain %v", seen)
	}
	// From the root the same plugin has the host's entry and is refused.
	top, err := r.Batch(ctx, MountOp(read, nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := failure(t, top[0]); !errors.Is(err, ErrBinding) {
		t.Fatalf("top-level mount of a plugin the host's entry may not run: %v", err)
	}
	// A refused declaration mounts a failed child, as c.Plugin does.
	sc := capture(stranger, "s1/stranger")
	bad, err := r.Batch(ctx, sc.MountOp(read, nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := failure(t, bad[0]); !errors.Is(err, ErrBinding) {
		t.Fatalf("child of an entry the consumer map does not list: %v", err)
	}
}

func TestChildMountOpRefusesTheWholeBatch(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	ctxs := make(chan *Context, 4)
	mount := func(id string) (*Fiber, *Context) {
		f := r.Plugin(plug(id, nil, nil, func(c *Context) error { ctxs <- c; return nil }), nil)
		want(t, f, Active)
		return f, <-ctxs
	}
	never := plug("s1/never", nil, nil, nil)
	neverTop := plug("s1/never-top", nil, nil, nil)
	none := func(what string) {
		t.Helper()
		snaps, err := r.Snapshots(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range snaps {
			if s.Plugin == "s1/never" || s.Plugin == "s1/never-top" {
				t.Fatalf("%s: a refused batch mounted %+v", what, s)
			}
		}
	}
	refused := func(what string, err error, targets ...error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: accepted", what)
		}
		for _, target := range targets {
			if !errors.Is(err, target) {
				t.Fatalf("%s: %v does not match %v", what, err, target)
			}
		}
		none(what)
	}

	// The parent's activation has ended.
	gone, gc := mount("s1/gone")
	if err := gone.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	if err := gone.Await(ctx); err != nil {
		t.Fatal(err)
	}
	_, err := r.Batch(ctx, MountOp(neverTop, nil), gc.MountOp(never, nil))
	refused("ended parent", err, ErrInactive, ErrInactiveEffect)

	// The parent was restarted: the old context is no longer its activation.
	again, ac := mount("s1/again")
	if err := again.Update(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := again.Await(ctx); err != nil {
		t.Fatal(err)
	}
	<-ctxs
	_, err = r.Batch(ctx, ac.MountOp(never, nil))
	refused("restarted parent", err, ErrInactive)

	// An earlier operation of the batch disposes the parent's fiber.
	pf, pc := mount("s1/dies")
	_, err = r.Batch(ctx, DisposeOp(pf), pc.MountOp(never, nil))
	refused("parent disposed earlier in the batch", err, ErrInactive)
	if s, _ := pf.State(ctx); s != Active {
		t.Fatalf("a refused batch left the parent %s", s)
	}
	// In the other order the child exists, then goes with the parent.
	got, err := r.Batch(ctx, pc.MountOp(plug("s1/child", nil, nil, nil), nil), DisposeOp(pf))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	if s, _ := got[0].State(ctx); s != Disposed {
		t.Fatalf("child of a parent disposed in the same batch is %s", s)
	}

	// A context of an availability check has no activation to mount under.
	checked := make(chan *Context, 1)
	k := NewKey[int]("s1.avail", 1)
	want(t, r.Plugin(plug("s1/availprov", nil, []AnyKey{k}, func(c *Context) error {
		return k.Provide(c, 1, Available(func(caller *Context) bool {
			select {
			case checked <- caller:
			default:
			}
			return true
		}))
	}), nil), Active)
	want(t, r.Plugin(plug("s1/availcons", []Dep{Need(k)}, nil, nil), nil), Active)
	_, err = r.Batch(ctx, (<-checked).MountOp(never, nil))
	refused("availability context", err, ErrInactive)

	// A context of another kernel, and the zero operation.
	other := newKernel(t)
	_, err = r.Batch(ctx, other.Context().MountOp(never, nil))
	refused("foreign context", err, errForeignFiber)
	_, err = r.Batch(ctx, (*Context)(nil).MountOp(never, nil))
	refused("nil context", err, ErrUnknown)
}

// cfgFixture is a plugin that records the config each activation applies.
type cfgFixture struct {
	mu      sync.Mutex
	applied map[string][]any
}

var errBadCfg = errors.New("bad config")

func (x *cfgFixture) plugin(id string, inject ...Dep) Plugin {
	return Define(Plugin{ID: id, Inject: inject, Decode: func(raw any) (any, error) {
		if s, ok := raw.(string); ok && strings.HasPrefix(s, "bad") {
			return nil, errBadCfg
		}
		return raw, nil
	}, Apply: func(_ *Context, cfg any) error {
		x.mu.Lock()
		defer x.mu.Unlock()
		if x.applied == nil {
			x.applied = make(map[string][]any)
		}
		x.applied[id] = append(x.applied[id], cfg)
		return nil
	}})
}

func (x *cfgFixture) got(id string) []any {
	x.mu.Lock()
	defer x.mu.Unlock()
	return slices.Clone(x.applied[id])
}

// order records the calls of the three config waterfalls of one kernel, and lets a
// test refuse, veto or fail a value.
type order struct {
	mu   sync.Mutex
	log  []string
	fail map[string]string // "commit:boom" -> "error" | "veto"; "update:x" -> "error" | "veto".
}

func (o *order) note(s string) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.log = append(o.log, s)
	return o.fail[s]
}

func (o *order) listen(r *Root) {
	ctx := r.Context()
	InternalConfig.On(ctx, DefaultStage, func(_ context.Context, a any, next Next[any]) (any, error) {
		o.note(fmt.Sprintf("config:%v", a))
		return next()
	})
	InternalCommit.On(ctx, DefaultStage, func(_ context.Context, a any, next Next[bool]) (bool, error) {
		switch o.note(fmt.Sprintf("commit:%v", a)) {
		case "error":
			return false, errors.New("commit failed")
		case "veto":
			return false, nil
		}
		return next()
	})
	InternalUpdate.On(ctx, DefaultStage, func(_ context.Context, a any, next Next[any]) (any, error) {
		switch o.note(fmt.Sprintf("update:%v", a)) {
		case "error":
			return nil, errors.New("update failed")
		case "veto":
			return nil, nil
		}
		return next()
	})
}

func (o *order) snapshot() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.log)
}

func TestUpdateOpWithRunsTheConfigPipelineBeforeTheBatchIsAdmitted(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	var x cfgFixture
	var o order
	a := r.Plugin(x.plugin("s1/a"), "init")
	b := r.Plugin(x.plugin("s1/b"), "init")
	want(t, a, Active)
	want(t, b, Active)
	o.listen(r)
	o.mu.Lock()
	o.log = nil
	o.mu.Unlock()

	got, err := r.Batch(ctx, UpdateOpWith(a, "c1"), UpdateOpWith(b, "c2"), UpdateOpWith(a, "c3"))
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != a || got[1] != b || got[2] != a {
		t.Fatalf("batch result %+v", got)
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	// The order: resolve and commit of each operation, then internal/update of each.
	// What follows is the activations, which resolve the retained configs again.
	wantOrder := []string{"config:c1", "commit:c1", "config:c2", "commit:c2", "config:c3", "commit:c3", "update:c1", "update:c2", "update:c3"}
	if log := o.snapshot(); len(log) < len(wantOrder) || !slices.Equal(log[:len(wantOrder)], wantOrder) {
		t.Fatalf("order %v, want it to start with %v", log, wantOrder)
	}
	// The later config of a fiber wins, and two updates of one fiber are one restart.
	if !reflect.DeepEqual(x.got("s1/a"), []any{"init", "c3"}) || !reflect.DeepEqual(x.got("s1/b"), []any{"init", "c2"}) {
		t.Fatalf("applied a=%v b=%v", x.got("s1/a"), x.got("s1/b"))
	}
}

func TestUpdateOpWithRefusalRefusesTheWholeBatchAndRetainsNothing(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	var x cfgFixture
	o := order{fail: map[string]string{"commit:cerr": "error", "commit:cveto": "veto", "update:uerr": "error"}}
	a := r.Plugin(x.plugin("s1/a"), "init")
	b := r.Plugin(x.plugin("s1/b"), "init")
	want(t, a, Active)
	want(t, b, Active)
	o.listen(r)
	restart := func() {
		t.Helper()
		if _, err := r.Batch(ctx, UpdateOp(a), UpdateOp(b)); err != nil {
			t.Fatal(err)
		}
		if err := r.Settle(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for name, c := range map[string]struct {
		cfg  string
		want error
	}{
		"decode": {"bad", errBadCfg},
		"commit": {"cerr", nil},
		"veto":   {"cveto", ErrCommitVetoed},
		"update": {"uerr", nil},
	} {
		_, err := r.Batch(ctx, UpdateOpWith(a, "ok-"+name), UpdateOpWith(b, c.cfg))
		if err == nil || (c.want != nil && !errors.Is(err, c.want)) {
			t.Fatalf("%s: %v, want %v", name, err, c.want)
		}
		if err := r.Settle(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(x.got("s1/a")) != 1 || len(x.got("s1/b")) != 1 {
		t.Fatalf("a refused batch restarted a fiber: a=%v b=%v", x.got("s1/a"), x.got("s1/b"))
	}
	// Nothing was retained: a restart applies the config of the mount.
	restart()
	if !reflect.DeepEqual(x.got("s1/a"), []any{"init", "init"}) || !reflect.DeepEqual(x.got("s1/b"), []any{"init", "init"}) {
		t.Fatalf("a=%v b=%v", x.got("s1/a"), x.got("s1/b"))
	}

	// A batch the core refuses after the pipeline ran gets the configs back.
	if _, err := r.Batch(ctx, UpdateOpWith(b, "late"), DisposeOp(a), UpdateOp(a)); err == nil {
		t.Fatal("a batch that updates a fiber it disposes was accepted")
	}
	restart()
	if got := x.got("s1/b"); !reflect.DeepEqual(got, []any{"init", "init", "init"}) {
		t.Fatalf("b applied %v after a refused batch", got)
	}

	// A fiber that is gone, never issued or has no config.
	if err := b.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.Await(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Batch(ctx, UpdateOpWith(b, "x")); !errors.Is(err, ErrInactive) {
		t.Fatalf("update of a disposed fiber: %v", err)
	}
	ghost := &Fiber{ref: r.core.fiber(1 << 40), cell: &cfgCell{root: r}}
	if _, err := r.Batch(ctx, UpdateOpWith(ghost, "x")); !errors.Is(err, ErrUnknown) {
		t.Fatalf("update of a fiber never issued: %v", err)
	}
	if _, err := r.Batch(ctx, UpdateOpWith(nil, "x")); !errors.Is(err, ErrUnknown) {
		t.Fatalf("update of a nil fiber: %v", err)
	}
}

func TestUpdateOpWithVetoOfInternalUpdateRetainsTheConfigWithoutARestart(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	var x cfgFixture
	o := order{fail: map[string]string{"update:hold": "veto"}}
	a := r.Plugin(x.plugin("s1/a"), "init")
	b := r.Plugin(x.plugin("s1/b"), "init")
	want(t, a, Active)
	want(t, b, Active)
	o.listen(r)
	if _, err := r.Batch(ctx, UpdateOpWith(a, "hold"), UpdateOpWith(b, "go")); err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	if got := x.got("s1/a"); !reflect.DeepEqual(got, []any{"init"}) {
		t.Fatalf("a vetoed restart ran: %v", got)
	}
	if got := x.got("s1/b"); !reflect.DeepEqual(got, []any{"init", "go"}) {
		t.Fatalf("the rest of the batch did not restart: %v", got)
	}
	// The retained config applies at the next restart.
	if _, err := r.Batch(ctx, UpdateOp(a)); err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	if got := x.got("s1/a"); !reflect.DeepEqual(got, []any{"init", "hold"}) {
		t.Fatalf("a applied %v", got)
	}
}

func TestUpdateOpWithOnAFiberWithNoTargetOrNoConfig(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	var x cfgFixture
	k := NewKey[int]("s1.gate", 1)
	o := order{}
	o.listen(r)
	// Gated on a need: no target, so cfg is not resolved here and the activation
	// that follows fails the fiber with the decode error, as Update does.
	gated := r.Plugin(x.plugin("s1/gated", Need(k)), "init")
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Batch(ctx, UpdateOpWith(gated, "bad-skipped")); err != nil {
		t.Fatalf("a fiber with no target resolved its config: %v", err)
	}
	for _, line := range o.snapshot() {
		if strings.Contains(line, "bad-skipped") && !strings.HasPrefix(line, "commit:") {
			t.Fatalf("a fiber with no target ran %s", line)
		}
	}
	if err := k.Provide(r.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if err := failure(t, gated); !errors.Is(err, errBadCfg) {
		t.Fatalf("gated fiber: %v", err)
	}

	// A When takes no config: the operation is a plain restart.
	var runs int
	var mu sync.Mutex
	var when *Fiber
	parent := r.Plugin(plug("s1/whenparent", nil, nil, func(c *Context) error {
		when = c.When(nil, func(*Context) error { mu.Lock(); runs++; mu.Unlock(); return nil })
		return nil
	}), nil)
	want(t, parent, Active)
	want(t, when, Active)
	if _, err := r.Batch(ctx, UpdateOpWith(when, "ignored")); err != nil {
		t.Fatal(err)
	}
	want(t, when, Active)
	if mu.Lock(); runs != 2 {
		t.Fatalf("When body ran %d times, want 2", runs)
	}
	mu.Unlock()
}

// A batch that the core refuses never had its config applied, even by an activation
// that a concurrent restart of the same fiber starts while the batch is pending.
func TestRefusedBatchConfigIsNeverAppliedByAConcurrentRestart(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	var x cfgFixture
	a := r.Plugin(x.plugin("s1/a"), "init")
	b := r.Plugin(x.plugin("s1/b"), "init")
	want(t, a, Active)
	want(t, b, Active)
	// One goroutine restarts b without a config, one activation after the other, while
	// batches that carry a config for b are refused by the core.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := r.Batch(ctx, UpdateOp(b)); err != nil {
				t.Error(err)
				return
			}
			_ = b.Await(ctx)
		}
	}()
	for i := 0; i < 300; i++ {
		if _, err := r.Batch(ctx, UpdateOpWith(b, "refused"), DisposeOp(a), UpdateOp(a)); err == nil {
			t.Fatal("a batch that updates a fiber it disposes was admitted")
		}
	}
	close(stop)
	wg.Wait()
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	got := x.got("s1/b")
	if n := len(got) - len(slices.DeleteFunc(slices.Clone(got), func(v any) bool { return v == "refused" })); n != 0 {
		t.Fatalf("%d of %d activations applied the config of a refused batch", n, len(got))
	}
	if raw := b.cell.load(); raw != "init" {
		t.Fatalf("retained config %v, want init", raw)
	}
}

// An update or a commit after a dispose of the same fiber refuses the batch whether
// or not a listener of internal/update vetoes the restart.
func TestAnUpdateOrCommitAfterADisposeRefusesTheBatch(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	var x cfgFixture
	o := order{fail: map[string]string{"update:late": "veto"}}
	a := r.Plugin(x.plugin("s1/a"), "init")
	want(t, a, Active)
	o.listen(r)
	for _, c := range []struct {
		name string
		op   BatchOp
	}{{"update", UpdateOpWith(a, "go")}, {"vetoed update", UpdateOpWith(a, "late")}, {"commit", CommitOp(a, "late")}} {
		if _, err := r.Batch(ctx, DisposeOp(a), c.op); err == nil {
			t.Fatalf("%s after a dispose of the same fiber was admitted", c.name)
		}
		if err := r.Settle(ctx); err != nil {
			t.Fatal(err)
		}
		if s, err := a.State(ctx); err != nil || s != Active {
			t.Fatalf("%s: a refused batch left the fiber %s (%v)", c.name, s, err)
		}
		if raw := a.cell.load(); raw != "init" {
			t.Fatalf("%s: a refused batch retained %v", c.name, raw)
		}
	}
}

// A child mount under a fiber that an earlier operation of the batch restarts is
// refused, as one under a fiber that it disposes is (ADR-034 G1); a restart later in
// the batch is allowed and the child goes with the old activation, and an operation
// that does not restart does not end the activation.
func TestChildMountOpUnderAFiberRestartedEarlierInTheBatch(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	ctxs := make(chan *Context, 4)
	pf := r.Plugin(plug("s1/restarted", nil, nil, func(c *Context) error { ctxs <- c; return nil }), nil)
	want(t, pf, Active)
	pc := <-ctxs
	_, err := r.Batch(ctx, UpdateOp(pf), pc.MountOp(plug("s1/never", nil, nil, nil), nil))
	if !errors.Is(err, ErrInactive) || !errors.Is(err, ErrInactiveEffect) {
		t.Fatalf("child under a fiber restarted earlier in the batch: %v", err)
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	if len(ctxs) != 0 {
		t.Fatal("a refused batch restarted the parent")
	}
	kept, err := r.Batch(ctx, CommitOp(pf, "kept"), pc.MountOp(plug("s1/kept", nil, nil, nil), nil))
	if err != nil {
		t.Fatal(err)
	}
	want(t, kept[1], Active)
	got, err := r.Batch(ctx, pc.MountOp(plug("s1/child", nil, nil, nil), nil), UpdateOp(pf))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	if s, _ := got[0].State(ctx); s != Disposed {
		t.Fatalf("child mounted before its parent's restart is %s", s)
	}
	want(t, pf, Active)
}

// A child mount is refused when an earlier operation of the batch ends its parent's
// activation by any path the batch restarts it through, not only an operation on the
// parent's own fiber: a provider it needs that is updated or disposed, an ancestor
// that is updated, and a provider beneath such an ancestor (ADR-034 G1). A refused
// batch applies nothing. The same mount before the operation is admitted and goes
// with the old activation.
func TestChildMountOpUnderAFiberRestartedThroughAnotherOperation(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	k, j := NewKey[int]("s1.k", 0), NewKey[int]("s1.j", 0)
	cctx, dctx, jctx := make(chan *Context, 4), make(chan *Context, 4), make(chan *Context, 4)
	pf := r.Plugin(plug("s1/provider", nil, []AnyKey{k}, func(c *Context) error { return k.Provide(c, 1) }), nil)
	cf := r.Plugin(plug("s1/consumer", []Dep{Need(k)}, nil, func(c *Context) error { cctx <- c; return nil }), nil)
	af := r.Plugin(plug("s1/ancestor", nil, nil, func(c *Context) error {
		c.Plugin(plug("s1/descendant", nil, nil, func(c *Context) error { dctx <- c; return nil }), nil)
		c.Plugin(plug("s1/inner-provider", nil, []AnyKey{j}, func(c *Context) error { return j.Provide(c, 2) }), nil)
		return nil
	}), nil)
	jf := r.Plugin(plug("s1/inner-consumer", []Dep{Need(j)}, nil, func(c *Context) error { jctx <- c; return nil }), nil)
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	for _, f := range []*Fiber{pf, cf, af, jf} {
		want(t, f, Active)
	}
	cc, dc, jc := <-cctx, <-dctx, <-jctx
	for _, c := range []struct {
		name   string
		op     BatchOp
		parent *Context
	}{
		{"consumer of a provider updated earlier", UpdateOp(pf), cc},
		{"consumer of a provider disposed earlier", DisposeOp(pf), cc},
		{"descendant of an ancestor updated earlier", UpdateOp(af), dc},
		{"consumer of a provider beneath an ancestor updated earlier", UpdateOp(af), jc},
	} {
		_, err := r.Batch(ctx, c.op, c.parent.MountOp(plug("s1/never", nil, nil, nil), nil))
		if !errors.Is(err, ErrInactive) || !errors.Is(err, ErrInactiveEffect) {
			t.Fatalf("child under a %s: %v", c.name, err)
		}
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	for _, f := range []*Fiber{pf, cf, af, jf} {
		want(t, f, Active)
	}
	if len(cctx)+len(dctx)+len(jctx) != 0 {
		t.Fatal("a refused batch restarted a fiber")
	}
	got, err := r.Batch(ctx, cc.MountOp(plug("s1/before", nil, nil, nil), nil), UpdateOp(pf))
	if err != nil {
		t.Fatalf("child mounted before its parent's provider is updated: %v", err)
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	if s, _ := got[0].State(ctx); s != Disposed {
		t.Fatalf("child under the consumer's old activation is %s", s)
	}
	want(t, cf, Active)
}
