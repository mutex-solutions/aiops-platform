// SPDX-License-Identifier: Apache-2.0

package loader

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// counting wraps the batch function and counts its calls.
func counting(calls *atomic.Int32, fail *atomic.Bool) func(*kernel.Root) BatchFunc {
	return func(r *kernel.Root) BatchFunc {
		return func(ctx context.Context, ops ...kernel.BatchOp) ([]*kernel.Fiber, error) {
			calls.Add(1)
			if fail != nil && fail.Load() {
				return nil, errors.New("batch refused")
			}
			return r.Batch(ctx, ops...)
		}
	}
}

func TestOnePlanIsOneBatch(t *testing.T) {
	p := newProbe()
	a, b, c, d := p.plugin("t/a", nil), p.plugin("t/b", nil), p.plugin("t/c", nil), p.plugin("t/d", nil)
	var calls atomic.Int32
	rb := row("b", "t/b")
	rb.Config = "old"
	x := newFx(t, fxOpt{plugins: plugins(a, b, c, d), rows: []def.EntrySpec{row("a", "t/a"), rb, row("c", "t/c")}, batch: counting(&calls, nil)})
	rb.Config = "new"
	rd := row("d", "t/d")
	res := x.apply(remove("a"), update(rb), create(rd))
	if calls.Load() != 1 {
		t.Errorf("the plan took %d kernel batches", calls.Load())
	}
	want := []def.Result{{ID: "a", Outcome: def.Removed}, {ID: "b", Outcome: def.Restarted}, {ID: "d", Outcome: def.Created}}
	for i, w := range want {
		if res[i] != w {
			t.Errorf("result %d = %v, want %v", i, res[i], w)
		}
	}
	if _, ok := x.ld.Locate("a"); ok || x.fibers("t/a") != 0 {
		t.Error("a is still there")
	}
	x.want("b", kernel.Active)
	x.want("d", kernel.Active)
	if p.count("t/b") != 2 || p.lastConfig("t/b") != "new" || p.count("t/c") != 1 || p.count("t/d") != 1 {
		t.Errorf("applies: b %d (config %v), c %d, d %d", p.count("t/b"), p.lastConfig("t/b"), p.count("t/c"), p.count("t/d"))
	}
}

func TestRefusedBatchIsAllOrNothing(t *testing.T) {
	p := newProbe()
	a := p.plugin("t/a", nil)
	dec := kernel.Define(kernel.Plugin{ID: "t/dec", Decode: func(raw any) (any, error) {
		if raw == "bad" {
			return nil, errors.New("config refused")
		}
		return raw, nil
	}, Apply: func(*kernel.Context, any) error { return nil }})
	var calls atomic.Int32
	var fail atomic.Bool
	ra, rz := row("a", "t/a"), row("z", "t/dec")
	ra.Config, rz.Config = "old", "good"
	x := newFx(t, fxOpt{plugins: plugins(a, dec), rows: []def.EntrySpec{ra, rz}, batch: counting(&calls, &fail)})

	// The kernel refuses the config of one update: nothing of the plan happens.
	ra.Config, rz.Config = "new", "bad"
	if _, err := x.ld.Apply(t.Context(), []def.Op{update(ra), create(row("f", "t/a")), update(rz)}); err == nil || !contains(err.Error(), "config refused") {
		t.Fatalf("Apply = %v", err)
	}
	if _, ok := x.ld.Locate("f"); ok || p.count("t/a") != 1 {
		t.Error("a create of the refused plan happened")
	}
	if e, _ := x.ld.Locate("a"); e.Spec.Config != "old" {
		t.Errorf("the refused plan changed a: %v", e.Spec.Config)
	}
	if e, _ := x.ld.Locate("z"); e.Spec.Config != "good" {
		t.Errorf("the refused plan changed z: %v", e.Spec.Config)
	}

	// The batch function fails: the tree is put back, children and removals too.
	fail.Store(true)
	if _, err := x.ld.Apply(t.Context(), []def.Op{remove("a"), create(group("g")), create(child("g", "k", "t/a"))}); err == nil {
		t.Fatal("Apply on a refused batch")
	}
	for _, id := range []string{"g", "g:k"} {
		if _, ok := x.ld.Locate(id); ok {
			t.Errorf("%s survived a refused batch", id)
		}
	}
	x.want("a", kernel.Active)
	fail.Store(false)
	x.apply(update(ra))
	x.want("a", kernel.Active)
}

func TestVolatileOnlyUpdateCommitsInPlace(t *testing.T) {
	p := newProbe()
	v := p.plugin("t/v", nil)
	l := newListener("t/listener")
	rv := row("v", "t/v")
	cfg := func(window int64, name string, urls ...string) map[string]any {
		rules := make([]any, len(urls))
		for i, u := range urls {
			rules[i] = map[string]any{"url": u}
		}
		return map[string]any{"window": window, "name": name, "rules": rules}
	}
	rv.Config = cfg(5, "x", "a")
	x := newFx(t, fxOpt{plugins: plugins(v), pre: []kernel.Plugin{l.plugin}, rows: []def.EntrySpec{rv},
		modules: []launch.CatalogEntry{{ID: "t/v", Volatile: []string{"window", "rules.*.url"}}}})
	states := watchStates(t, x.root)
	before := states.of(t, x.root, "t/v")

	for _, tc := range []struct {
		name  string
		to    map[string]any
		want  def.Outcome
		paths []string
	}{
		{"volatile scalar", cfg(6, "x", "a"), def.Committed, []string{"window"}},
		{"volatile under a wildcard", cfg(6, "x", "b"), def.Committed, []string{"rules.0.url"}},
		{"both", cfg(7, "x", "c"), def.Committed, []string{"rules.0.url", "window"}},
		{"nothing", cfg(7, "x", "c"), def.Unchanged, nil},
	} {
		rv.Config = tc.to
		if res := x.apply(update(rv)); res[0].Outcome != tc.want {
			t.Fatalf("%s: %v", tc.name, res)
		}
		if e, _ := x.ld.Locate("v"); !deepEqual(e.Spec.Config, tc.to) {
			t.Errorf("%s: the committed config is %v", tc.name, e.Spec.Config)
		}
	}
	if p.count("t/v") != 1 || states.of(t, x.root, "t/v") != before {
		t.Errorf("a volatile commit restarted the fiber: %d applies, %d state events", p.count("t/v"), states.of(t, x.root, "t/v")-before)
	}
	l.mu.Lock()
	heard := append([]def.VolatileUpdateArgs(nil), l.volatile...)
	l.mu.Unlock()
	if len(heard) != 3 || heard[0].ID != "v" || len(heard[0].Paths) != 1 || heard[0].Paths[0] != "window" ||
		len(heard[2].Paths) != 2 || !deepEqual(heard[2].Config, cfg(7, "x", "c")) {
		t.Errorf("loader/volatile-update heard %+v", heard)
	}

	// Anything else restarts the fiber with the new config.
	for _, to := range []map[string]any{cfg(7, "y", "c"), cfg(7, "y", "c", "d"), {"window": int64(7)}} {
		rv.Config = to
		if res := x.apply(update(rv)); res[0].Outcome != def.Restarted {
			t.Fatalf("%v: %v", to, res)
		}
		x.want("v", kernel.Active)
		if !deepEqual(p.lastConfig("t/v"), to) {
			t.Errorf("the restart applied %v, want %v", p.lastConfig("t/v"), to)
		}
	}
	if p.count("t/v") != 4 {
		t.Errorf("applies %d, want 4", p.count("t/v"))
	}
	l.mu.Lock()
	n := len(l.volatile)
	l.mu.Unlock()
	if n != 3 {
		t.Errorf("a restart announced a volatile commit: %d events", n)
	}
}

// Only an ACTIVE fiber takes volatile values in place: a FAILED one is restarted
// with the new config, which is how an operator fixes it.
func TestVolatileOnlyUpdateOfAFailedFiberRestartsIt(t *testing.T) {
	var applies atomic.Int32
	v := kernel.Define(kernel.Plugin{ID: "t/v", Decode: func(raw any) (any, error) {
		if raw.(map[string]any)["window"] == int64(-1) {
			return nil, errors.New("window must not be negative")
		}
		return raw, nil
	}, Apply: func(*kernel.Context, any) error { applies.Add(1); return nil }})
	l := newListener("t/listener")
	rv := row("v", "t/v")
	rv.Config = map[string]any{"window": int64(-1)}
	x := newFx(t, fxOpt{plugins: plugins(v), pre: []kernel.Plugin{l.plugin}, rows: []def.EntrySpec{rv},
		modules: []launch.CatalogEntry{{ID: "t/v", Volatile: []string{"window"}}}})
	x.want("v", kernel.Failed)
	rv.Config = map[string]any{"window": int64(5)}
	if res := x.apply(update(rv)); res[0].Outcome != def.Restarted {
		t.Fatalf("outcome %v", res)
	}
	x.want("v", kernel.Active)
	l.mu.Lock()
	defer l.mu.Unlock()
	if applies.Load() != 1 || len(l.volatile) != 0 {
		t.Errorf("applies %d, volatile-update heard %v", applies.Load(), l.volatile)
	}
}

// slowGate is a plugin whose disposal waits: it says when the disposal starts and
// whether a second fiber of the plugin was applied while one was disposing.
type slowGate struct {
	entered chan struct{}
	release chan struct{}
	busy    atomic.Bool
	overlap atomic.Bool
	applies atomic.Int32
}

func newSlowGate() *slowGate {
	return &slowGate{entered: make(chan struct{}, 8), release: make(chan struct{})}
}

func (g *slowGate) plugin() kernel.Plugin {
	return kernel.Define(kernel.Plugin{ID: "t/slow", Apply: func(c *kernel.Context, _ any) error {
		g.applies.Add(1)
		if g.busy.Load() {
			g.overlap.Store(true)
		}
		c.Defer("slow", func(context.Context) error {
			g.busy.Store(true)
			g.entered <- struct{}{}
			<-g.release
			g.busy.Store(false)
			return nil
		})
		return nil
	}})
}

func TestNeverCreatesWhileThePreviousFiberIsNotDisposed(t *testing.T) {
	t.Run("remove then create in one plan", func(t *testing.T) {
		g := newSlowGate()
		x := newFx(t, fxOpt{plugins: plugins(g.plugin()), rows: []def.EntrySpec{row("s", "t/slow")}})
		var wg sync.WaitGroup
		var res []def.Result
		var err error
		wg.Go(func() { res, err = x.ld.Apply(t.Context(), []def.Op{remove("s"), create(row("s", "t/slow"))}) })
		<-g.entered
		if e, ok := x.ld.Locate("s"); !ok || e.Mount != def.Mounted || e.State != kernel.Pending {
			t.Errorf("while the old fiber disposes: %+v", e)
		}
		close(g.release)
		wg.Wait()
		if err != nil || len(res) != 2 || res[0].Outcome != def.Removed || res[1].Outcome != def.Created {
			t.Fatalf("Apply = %v, %v", res, err)
		}
		x.settle()
		x.want("s", kernel.Active)
		if g.overlap.Load() || g.applies.Load() != 2 {
			t.Errorf("overlap %v, applies %d", g.overlap.Load(), g.applies.Load())
		}
	})

	t.Run("across plans", func(t *testing.T) {
		g := newSlowGate()
		x := newFx(t, fxOpt{plugins: plugins(g.plugin()), rows: []def.EntrySpec{row("s", "t/slow")}})
		if _, err := x.ld.Apply(t.Context(), []def.Op{remove("s")}); err != nil {
			t.Fatal(err)
		}
		<-g.entered
		var wg sync.WaitGroup
		var err error
		wg.Go(func() { _, err = x.ld.Apply(t.Context(), []def.Op{create(row("s", "t/slow"))}) })
		close(g.release)
		wg.Wait()
		x.settle()
		x.want("s", kernel.Active)
		if err != nil || g.overlap.Load() || g.applies.Load() != 2 {
			t.Errorf("err %v, overlap %v, applies %d", err, g.overlap.Load(), g.applies.Load())
		}
	})

	t.Run("a wait that is cut short leaves an entry with no fiber", func(t *testing.T) {
		g := newSlowGate()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		x := newFx(t, fxOpt{plugins: plugins(g.plugin()), rows: []def.EntrySpec{row("s", "t/slow")},
			batch: func(r *kernel.Root) BatchFunc {
				return func(c context.Context, ops ...kernel.BatchOp) ([]*kernel.Fiber, error) {
					fibers, err := r.Batch(c, ops...)
					cancel() // the plan is committed: the wait for the disposal gets no time
					return fibers, err
				}
			}})
		if _, err := x.ld.Apply(ctx, []def.Op{remove("s"), create(row("s", "t/slow"))}); !errors.Is(err, context.Canceled) {
			t.Fatalf("Apply = %v", err)
		}
		<-g.entered
		if e, _ := x.ld.Locate("s"); e.Mount != def.Mounted || e.State != kernel.Pending {
			t.Fatalf("entry %+v", e)
		}
		close(g.release)
		// An Update of the entry with no fiber mounts it, after the wait.
		if res := x.apply(update(row("s", "t/slow"))); res[0].Outcome != def.Created {
			t.Fatalf("outcome %v", res)
		}
		x.want("s", kernel.Active)
		if g.overlap.Load() || g.applies.Load() != 2 {
			t.Errorf("overlap %v, applies %d", g.overlap.Load(), g.applies.Load())
		}
	})

	t.Run("a previous fiber that does not dispose is refused", func(t *testing.T) {
		p := newProbe()
		x := newFx(t, fxOpt{plugins: plugins(p.plugin("t/a", nil)), rows: []def.EntrySpec{row("a", "t/a")}})
		f, _, _ := x.tree.fiberOf("a")
		x.tree.prev["b"] = []*kernel.Fiber{f}
		if err := x.tree.settle(t.Context(), []string{"b"}); !errors.Is(err, def.ErrNotDisposed) {
			t.Errorf("settle = %v", err)
		}
		// A sweep keeps it, because it is alive, and forgets what is DISPOSED.
		x.tree.sweep(t.Context())
		if len(x.tree.prev["b"]) != 1 {
			t.Errorf("sweep dropped a live fiber")
		}
		x.apply(remove("a"))
		if err := f.Await(t.Context()); err != nil {
			t.Fatal(err)
		}
		x.tree.sweep(t.Context())
		if len(x.tree.prev) != 0 {
			t.Errorf("prev after a sweep: %v", x.tree.prev)
		}
	})
}

func TestChangedRowIsReplacedOneFiberAtATime(t *testing.T) {
	k := kernel.NewKey[int]("k", 0)
	tag := kernel.NewKey[[]any]("tag", 0)
	p := newProbe()
	prov := kernel.Define(kernel.Plugin{ID: "t/prov", Provides: []kernel.AnyKey{k}, Apply: func(c *kernel.Context, _ any) error { return k.Provide(c, 1) }})
	for name, tc := range map[string]struct {
		mutate func(*def.EntrySpec)
		plugin string
	}{
		"inject":    {func(r *def.EntrySpec) { r.Inject = []string{"k"} }, "t/r"},
		"isolate":   {func(r *def.EntrySpec) { r.Isolate = []string{"k"} }, "t/r"},
		"intercept": {func(r *def.EntrySpec) { r.Intercept = map[string]any{"tag": 1} }, "t/r"},
		"plugin":    {func(r *def.EntrySpec) { r.Plugin = "t/r2" }, "t/r2"},
	} {
		t.Run(name, func(t *testing.T) {
			p := newProbe()
			x := newFx(t, fxOpt{plugins: plugins(p.plugin("t/r", nil), p.plugin("t/r2", nil)), keys: map[string]kernel.AnyKey{"k": k, "tag": tag},
				pre: []kernel.Plugin{prov}, rows: []def.EntrySpec{row("r", "t/r")}})
			r := row("r", "t/r")
			tc.mutate(&r)
			// The same set in another order is no change.
			if res := x.apply(update(row("r", "t/r"))); res[0].Outcome != def.Unchanged {
				t.Fatalf("no change: %v", res)
			}
			if res := x.apply(update(r)); res[0].Outcome != def.Restarted {
				t.Fatalf("outcome %v", res)
			}
			x.want("r", kernel.Active)
			if p.count(tc.plugin) != 1+b2i(tc.plugin == "t/r") || p.maxLive["t/r"] != 1 {
				t.Errorf("applies %d, alive at most %d", p.count(tc.plugin), p.maxLive["t/r"])
			}
			if name == "plugin" && p.live["t/r"] != 0 {
				t.Error("the old plugin is still alive")
			}
		})
	}
	_ = p
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestEnableDisableAndRoleChange(t *testing.T) {
	p := newProbe()
	x := newFx(t, fxOpt{plugins: plugins(p.plugin("t/a", nil)), rows: []def.EntrySpec{row("a", "t/a")}})
	off := row("a", "t/a")
	off.Enabled = false
	other := row("a", "t/a")
	other.Roles = []string{"worker"}
	for i, tc := range []struct {
		to   def.EntrySpec
		want def.Outcome
		mnt  def.Mount
	}{
		{off, def.Unmounted, def.Disabled},
		{other, def.Unchanged, def.OtherRole},
		{row("a", "t/a"), def.Created, def.Mounted},
		{other, def.Unmounted, def.OtherRole},
		{row("a", "t/a"), def.Created, def.Mounted},
	} {
		if res := x.apply(update(tc.to)); res[0].Outcome != tc.want {
			t.Fatalf("step %d: %v", i, res)
		}
		if e, _ := x.ld.Locate("a"); e.Mount != tc.mnt {
			t.Fatalf("step %d: %+v", i, e)
		}
	}
	x.want("a", kernel.Active)
	if p.count("t/a") != 3 || p.maxLive["t/a"] != 1 {
		t.Errorf("applies %d, alive at most %d", p.count("t/a"), p.maxLive["t/a"])
	}
	// Roles that still include the role change nothing.
	more := row("a", "t/a")
	more.Roles = []string{"api", "worker"}
	if res := x.apply(update(more)); res[0].Outcome != def.Unchanged {
		t.Errorf("outcome %v", res)
	}
}

func TestFiberThatEndedByItself(t *testing.T) {
	p := newProbe()
	x := newFx(t, fxOpt{plugins: plugins(p.plugin("t/a", nil), p.plugin("t/b", nil)), rows: []def.EntrySpec{row("a", "t/a"), row("b", "t/b")}})
	for _, id := range []string{"a", "b"} {
		f, _, _ := x.tree.fiberOf(id)
		if err := f.Dispose(t.Context()); err != nil {
			t.Fatal(err)
		}
		_ = f.Await(t.Context())
	}
	// Remove has nothing to dispose, and an Update mounts the entry again.
	if res := x.apply(remove("a")); res[0].Outcome != def.Removed {
		t.Fatalf("outcome %v", res)
	}
	rb := row("b", "t/b")
	rb.Config = "again"
	if res := x.apply(update(rb)); res[0].Outcome != def.Created {
		t.Fatalf("outcome %v", res)
	}
	x.want("b", kernel.Active)
	if p.count("t/b") != 2 {
		t.Errorf("applies %d", p.count("t/b"))
	}
}

func TestUpdateOfAChildWhoseGroupIsPendingIsKept(t *testing.T) {
	gate := kernel.NewKey[int]("gate", 0)
	p := newProbe()
	g := group("g")
	g.Inject = []string{"gate"}
	x := newFx(t, fxOpt{plugins: plugins(p.plugin("t/k", nil)), keys: map[string]kernel.AnyKey{"gate": gate}, rows: []def.EntrySpec{g, child("g", "k", "t/k")}})
	k := child("g", "k", "t/k")
	k.Config = "new"
	if res := x.apply(update(k)); res[0].Outcome != def.Unchanged {
		t.Fatalf("outcome %v", res)
	}
	x.root.Plugin(kernel.Define(kernel.Plugin{ID: "t/gate", Provides: []kernel.AnyKey{gate},
		Apply: func(c *kernel.Context, _ any) error { return gate.Provide(c, 1) }}), nil)
	x.settle()
	x.want("g:k", kernel.Active)
	if p.lastConfig("t/k") != "new" {
		t.Errorf("the child mounted with %v", p.lastConfig("t/k"))
	}
}

func TestOneApplyAtATime(t *testing.T) {
	p := newProbe()
	x := newFx(t, fxOpt{plugins: plugins(p.plugin("t/a", nil)), rows: []def.EntrySpec{row("a", "t/a")}})
	x.tree.sem <- struct{}{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := x.ld.Apply(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("Apply waiting for another: %v", err)
	}
	<-x.tree.sem
	if _, err := x.ld.Apply(t.Context(), nil); err != nil {
		t.Errorf("an empty plan: %v", err)
	}
	// Await ends with its context, for a mounted entry and for a child that waits for its group.
	if _, err := x.ld.Await(ctx, "a"); !errors.Is(err, context.Canceled) {
		t.Errorf("Await: %v", err)
	}
	x.apply(create(group("g")), create(child("g", "k", "t/a")))
	x.tree.mu.Lock()
	x.tree.entries["g:k"].fiber = nil
	x.tree.mu.Unlock()
	if _, err := x.ld.Await(ctx, "g:k"); !errors.Is(err, context.Canceled) {
		t.Errorf("Await of a child: %v", err)
	}
}

// A volatile commit is retained by the kernel with the tree's record, so a restart
// that the kernel starts later applies it and does not revert it.
func TestVolatileCommitSurvivesAKernelRestart(t *testing.T) {
	k := kernel.NewKey[int]("k", 0)
	p := newProbe()
	prov := p.plugin("t/prov", func(c *kernel.Context, _ any) error { return k.Provide(c, 1) })
	prov.Provides = []kernel.AnyKey{k}
	v := p.plugin("t/v", nil)
	rv := row("v", "t/v")
	rv.Inject = []string{"k"}
	rv.Config = map[string]any{"window": int64(5), "name": "x"}
	x := newFx(t, fxOpt{plugins: plugins(v, kernel.Define(prov)), keys: map[string]kernel.AnyKey{"k": k},
		rows: []def.EntrySpec{row("prov", "t/prov"), rv}, modules: []launch.CatalogEntry{{ID: "t/v", Volatile: []string{"window"}}}})
	x.want("v", kernel.Active)
	committed := map[string]any{"window": int64(9), "name": "x"}
	rv.Config = committed
	if res := x.apply(update(rv)); res[0].Outcome != def.Committed {
		t.Fatalf("outcome %v", res)
	}
	if p.count("t/v") != 1 {
		t.Fatalf("the commit restarted the fiber: %d applies", p.count("t/v"))
	}

	// The provider restarts: its epoch changes and the kernel restarts the entry.
	x.tree.mu.Lock()
	pf, vf := x.tree.entries["prov"].fiber, x.tree.entries["v"].fiber
	x.tree.mu.Unlock()
	if err := pf.Update(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	x.settle()
	x.want("v", kernel.Active)
	if p.count("t/v") != 2 || !deepEqual(p.lastConfig("t/v"), committed) {
		t.Errorf("the epoch restart applied %v after %d applies, want %v", p.lastConfig("t/v"), p.count("t/v"), committed)
	}
	// So does a restart through an update operation, which keeps the retained config.
	if _, err := x.root.Batch(t.Context(), kernel.UpdateOp(vf)); err != nil {
		t.Fatal(err)
	}
	x.settle()
	if p.count("t/v") != 3 || !deepEqual(p.lastConfig("t/v"), committed) {
		t.Errorf("the update operation applied %v after %d applies", p.lastConfig("t/v"), p.count("t/v"))
	}

	// A commit that the kernel refuses changes neither record and is announced to
	// no one.
	var veto atomic.Bool
	kernel.InternalCommit.On(x.root.Context(), kernel.DefaultStage, func(_ context.Context, _ any, next kernel.Next[bool]) (bool, error) {
		if veto.Load() {
			return false, nil
		}
		return next()
	})
	veto.Store(true)
	rv.Config = map[string]any{"window": int64(11), "name": "x"}
	if _, err := x.ld.Apply(t.Context(), []def.Op{update(rv)}); !errors.Is(err, kernel.ErrCommitVetoed) {
		t.Fatalf("a vetoed commit: %v", err)
	}
	if e, _ := x.ld.Locate("v"); !deepEqual(e.Spec.Config, committed) {
		t.Errorf("the tree records %v after a refused commit", e.Spec.Config)
	}
	if _, err := x.root.Batch(t.Context(), kernel.UpdateOp(vf)); err != nil {
		t.Fatal(err)
	}
	x.settle()
	if !deepEqual(p.lastConfig("t/v"), committed) {
		t.Errorf("a refused commit was retained: %v", p.lastConfig("t/v"))
	}
}
