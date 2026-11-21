// SPDX-License-Identifier: Apache-2.0

package loader

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

// A restart restarts the entry's fiber with the spec the tree holds, as a retry does
// (def.OpRestart, ADR-059 decision 19): the plugin is applied again, so a fiber that
// failed for a transient reason is ACTIVE after it. An update that changes nothing
// restarts nothing, FAILED or not, and an entry with no fiber to restart is unchanged.
// A restart after such an update in the same plan, as a rollback of reload sends for a
// row whose inverse update restarts nothing, restarts the FAILED fiber.
func TestRestartRestartsTheFiber(t *testing.T) {
	var applies atomic.Int32
	failing := map[int32]bool{1: true, 4: true} // the applies that fail
	f := kernel.Define(kernel.Plugin{ID: "t/f", Apply: func(*kernel.Context, any) error {
		if failing[applies.Add(1)] {
			return errors.New("transient")
		}
		return nil
	}})
	off := row("off", "t/f")
	off.Enabled = false
	x := newFx(t, fxOpt{plugins: plugins(f), rows: []def.EntrySpec{row("f", "t/f"), off}})
	x.want("f", kernel.Failed)
	for _, tc := range []struct {
		ops     []def.Op
		want    []def.Result
		st      kernel.State
		applies int32
	}{
		{[]def.Op{update(row("f", "t/f"))}, []def.Result{{ID: "f", Outcome: def.Unchanged}}, kernel.Failed, 1},
		{[]def.Op{restart("f"), restart("off")}, []def.Result{{ID: "f", Outcome: def.Restarted}, {ID: "off", Outcome: def.Unchanged}}, kernel.Active, 2},
		{[]def.Op{update(row("f", "t/f"))}, []def.Result{{ID: "f", Outcome: def.Unchanged}}, kernel.Active, 2},
		{[]def.Op{restart("f")}, []def.Result{{ID: "f", Outcome: def.Restarted}}, kernel.Active, 3},
		{[]def.Op{restart("f")}, []def.Result{{ID: "f", Outcome: def.Restarted}}, kernel.Failed, 4},
		{[]def.Op{update(row("f", "t/f")), restart("f")}, []def.Result{{ID: "f", Outcome: def.Unchanged}, {ID: "f", Outcome: def.Restarted}}, kernel.Active, 5},
	} {
		if res := x.apply(tc.ops...); !slices.Equal(res, tc.want) {
			t.Fatalf("Apply = %v, want %v", res, tc.want)
		}
		if st := x.state("f"); st != tc.st || applies.Load() != tc.applies {
			t.Errorf("after %v: f is %s, applied %d times; want %s, %d", tc.want, st, applies.Load(), tc.st, tc.applies)
		}
	}
}

// gated is a group g that injects the key named gate.
func gated() def.EntrySpec {
	g := group("g")
	g.Inject = []string{"gate"}
	return g
}

// provider is a row plugin that provides gate.
func provider(p *probe, id string, gate kernel.Key[int]) kernel.Plugin {
	pl := p.plugin(id, func(c *kernel.Context, _ any) error { return gate.Provide(c, 1) })
	pl.Provides = []kernel.AnyKey{gate}
	return kernel.Define(pl)
}

// A plan that restarts the provider a group injects and replaces a child of the
// group: the first batch quiesces the group's activation, so the second must not
// mount the child from it. The group's next activation mounts the child, and the
// plan, whose first batch is committed, is not an error.
func TestSecondBatchSkipsAnEndingGroupActivation(t *testing.T) {
	gate := kernel.NewKey[int]("gate", 0)
	p := newProbe()
	slow := newSlowGate() // its disposal holds the group's old activation open
	rp := row("p", "t/p")
	rp.Config = "v1"
	fails := kernel.Define(kernel.Plugin{ID: "t/n", Apply: func(*kernel.Context, any) error { return errors.New("n fails") }})
	x := newFx(t, fxOpt{plugins: plugins(provider(p, "t/p", gate), p.plugin("t/k", nil), p.plugin("t/k2", nil), slow.plugin(), fails),
		keys: map[string]kernel.AnyKey{"gate": gate},
		rows: []def.EntrySpec{rp, gated(), child("g", "k", "t/k"), child("g", "slow", "t/slow")}})
	x.want("g:k", kernel.Active)
	rp.Config = "v2"
	res, err := x.ld.Apply(t.Context(), []def.Op{update(rp), update(child("g", "k", "t/k2")), create(row("n", "t/n"))})
	close(slow.release)
	want := []def.Result{{ID: "p", Outcome: def.Restarted}, {ID: "g:k", Outcome: def.Restarted}, {ID: "n", Outcome: def.Created}}
	if err != nil || !slices.Equal(res, want) {
		t.Fatalf("Apply = %v, %v", res, err)
	}
	x.settle()
	x.want("g:k", kernel.Active)
	x.want("n", kernel.Failed)
	x.consistent()
	if p.count("t/k2") != 1 {
		t.Errorf("t/k2 applied %d times", p.count("t/k2"))
	}

	// An activation that has ended mounts nothing more, whatever the tree says.
	c := ended(t, x.root)
	x.tree.mu.Lock()
	x.tree.entries["g"].kids, x.tree.entries["g:k"].fiber = c, nil
	x.tree.mu.Unlock()
	if items := x.tree.mountItems(t.Context(), []string{"g:k"}); len(items) != 0 {
		t.Errorf("mounts from an ended activation: %v", items)
	}
}

// The second batch of a plan does not mount a child that the group's new
// activation is mounting: the child gets one fiber.
func TestSecondBatchSkipsAChildTheGroupIsMounting(t *testing.T) {
	gate := kernel.NewKey[int]("gate", 0)
	p := newProbe()
	slow := newSlowGate() // its disposal holds the plan in its wait for the second batch
	l := newListener("t/listener")
	rp := row("p", "t/p")
	rp.Config = "v1"
	x := newFx(t, fxOpt{plugins: plugins(provider(p, "t/p", gate), p.plugin("t/k", nil), p.plugin("t/k2", nil), slow.plugin(), p.plugin("t/s2", nil)),
		keys: map[string]kernel.AnyKey{"gate": gate}, pre: []kernel.Plugin{l.plugin},
		rows: []def.EntrySpec{rp, gated(), child("g", "k", "t/k"), row("s", "t/slow")}})
	x.want("g:k", kernel.Active)
	// The group's next activation is held while it builds g:k.
	building, release := make(chan struct{}), make(chan struct{})
	var armed atomic.Bool
	armed.Store(true)
	l.mu.Lock()
	l.patch = func(a def.PatchContextArgs, next kernel.Next[*kernel.Context]) (*kernel.Context, error) {
		if a.Entry.ID == "g:k" && armed.Swap(false) {
			close(building)
			<-release
		}
		return next()
	}
	l.mu.Unlock()

	rp.Config = "v2"
	var res []def.Result
	var err error
	var wg sync.WaitGroup
	wg.Go(func() {
		res, err = x.ld.Apply(t.Context(), []def.Op{update(rp), update(child("g", "k", "t/k2")), update(row("s", "t/s2"))})
	})
	<-building
	close(slow.release)
	wg.Wait()
	close(release)
	if err != nil || len(res) != 3 {
		t.Fatalf("Apply = %v, %v", res, err)
	}
	x.settle()
	x.want("g:k", kernel.Active)
	x.want("s", kernel.Active)
	if n := x.fibers("t/k2"); n != 1 {
		t.Errorf("g:k has %d live fibers", n)
	}
	x.consistent()
}

// A plan that the kernel refuses is undone without undoing what a group's
// activation did meanwhile: here the provider the group injects goes while the
// plan is in flight, and the group unloads with its children.
func TestRefusedPlanKeepsWhatAGroupDidMeanwhile(t *testing.T) {
	gate := kernel.NewKey[int]("gate", 0)
	p := newProbe()
	prov := kernel.Define(kernel.Plugin{ID: "t/gate", Provides: []kernel.AnyKey{gate}, Apply: func(c *kernel.Context, _ any) error { return gate.Provide(c, 1) }})
	var pf *kernel.Fiber
	var arm atomic.Bool
	x := newFx(t, fxOpt{plugins: plugins(p.plugin("t/a", nil), p.plugin("t/c", nil)), keys: map[string]kernel.AnyKey{"gate": gate},
		rows: []def.EntrySpec{gated(), child("g", "a", "t/a")},
		batch: func(r *kernel.Root) BatchFunc {
			return func(ctx context.Context, ops ...kernel.BatchOp) ([]*kernel.Fiber, error) {
				if arm.Swap(false) {
					if err := pf.Dispose(ctx); err != nil {
						return nil, err
					}
					_ = r.Settle(ctx)
				}
				return r.Batch(ctx, ops...)
			}
		}})
	pf = x.root.Plugin(prov, nil)
	x.settle()
	x.want("g:a", kernel.Active)

	arm.Store(true)
	if _, err := x.ld.Apply(t.Context(), []def.Op{create(child("g", "c", "t/c"))}); !errors.Is(err, kernel.ErrInactive) {
		t.Fatalf("Apply = %v", err)
	}
	if _, ok := x.ld.Locate("g:c"); ok {
		t.Error("g:c survived the refused plan")
	}
	if e, _ := x.ld.Locate("g:a"); e.Mount != def.Mounted || e.State != kernel.Pending {
		t.Errorf("g:a, whose group unloaded: %+v", e)
	}
	x.consistent()

	// The provider returns and the group mounts the tree as it was before the plan.
	x.root.Plugin(prov, nil)
	x.settle()
	x.want("g:a", kernel.Active)
	x.consistent()
	x.apply(create(child("g", "c", "t/c")), remove("g:a"))
	x.want("g:c", kernel.Active)
	x.consistent()
	if x.fibers("t/a") != 0 || p.count("t/c") != 1 {
		t.Errorf("live t/a %d, t/c applied %d times", x.fibers("t/a"), p.count("t/c"))
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

	// A group that activates while the previous fiber of an entry beneath it disposes
	// does not mount the entry: the plan does, after its wait. Here the id moves from
	// the top level into a group that the plan creates.
	t.Run("a group that activates meanwhile", func(t *testing.T) {
		g := newSlowGate()
		l := newListener("t/listener")
		activated := make(chan struct{})
		l.onInit = func(a def.EntryInitArgs) {
			if a.Entry.ID == "g" {
				close(activated)
			}
		}
		x := newFx(t, fxOpt{plugins: plugins(g.plugin()), pre: []kernel.Plugin{l.plugin}, rows: []def.EntrySpec{row("s", "t/slow")}})
		under := row("s", "t/slow")
		under.Parent = "g"
		var wg sync.WaitGroup
		var res []def.Result
		var err error
		wg.Go(func() { res, err = x.ld.Apply(t.Context(), []def.Op{remove("s"), create(group("g")), create(under)}) })
		<-g.entered
		<-activated
		x.want("g", kernel.Active) // its activation has mounted what it mounts
		st := x.state("s")
		close(g.release)
		wg.Wait()
		if st != kernel.Pending || g.overlap.Load() {
			t.Errorf("while the old fiber disposes: s is %s, overlap %v", st, g.overlap.Load())
		}
		want := []def.Result{{ID: "s", Outcome: def.Removed}, {ID: "g", Outcome: def.Created}, {ID: "s", Outcome: def.Created}}
		if err != nil || !slices.Equal(res, want) {
			t.Fatalf("Apply = %v, %v", res, err)
		}
		x.settle()
		x.want("s", kernel.Active)
		if g.overlap.Load() || g.applies.Load() != 2 || x.fibers("t/slow") != 1 {
			t.Errorf("overlap %v, applies %d, live %d", g.overlap.Load(), g.applies.Load(), x.fibers("t/slow"))
		}
	})

	t.Run("a previous fiber that does not dispose is refused", func(t *testing.T) {
		p := newProbe()
		x := newFx(t, fxOpt{plugins: plugins(p.plugin("t/a", nil)), rows: []def.EntrySpec{row("a", "t/a")}})
		f, _, _ := x.tree.fiberOf("a")
		x.tree.prev["b"] = []previous{{fiber: f}}
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

// The rule above holds by key too: a plan that mounts a provider of a key while a
// previous fiber provides it in the same realm mounts the new one only once the old one
// is DISPOSED, whether this plan or an earlier one retired the old one or a restart of
// its group gave it up. The kernel refuses a second provision of a key while the first
// stands, so the new row would be FAILED, and under ADR-059 decision 19 its generation
// rolled back. The old provider holds the key until its disposer returns. A provider in
// another realm does not wait.
func TestNeverCreatesAProviderWhileTheOldProviderOfItsKeyIsNotDisposed(t *testing.T) {
	k := kernel.NewKey[int]("k", 0)
	iso := func(e def.EntrySpec) def.EntrySpec { e.Isolate = []string{"k"}; return e }
	off := func(e def.EntrySpec) def.EntrySpec { e.Enabled = false; return e }
	on := func(e def.EntrySpec) def.EntrySpec { e.Enabled = true; return e }
	until := func(cond func() bool) {
		for !cond() {
			time.Sleep(time.Millisecond)
		}
	}
	for _, tc := range []struct {
		name   string
		rows   []def.EntrySpec
		before []def.Op // an earlier plan, which returns while the old provider disposes
		ops    []def.Op
		group  string // a group the plan mounts, whose activation mounts the new provider
		id     string // the entry of the new provider
		waits  bool
	}{
		{"remove, then create", []def.EntrySpec{row("old", "t/old")}, nil,
			[]def.Op{remove("old"), create(row("new", "t/new"))}, "", "new", true},
		{"replace, then create", []def.EntrySpec{row("old", "t/old")}, nil,
			[]def.Op{update(row("old", "t/other")), create(row("new", "t/new"))}, "", "new", true},
		{"disable, then enable the group of the new one", []def.EntrySpec{row("old", "t/old"), off(group("h")), child("h", "c", "t/new")}, nil,
			[]def.Op{update(off(row("old", "t/old"))), update(on(group("h")))}, "h", "h:c", true},
		{"in the realm of a group", []def.EntrySpec{iso(group("g")), child("g", "old", "t/old")}, nil,
			[]def.Op{remove("g:old"), create(child("g", "new", "t/new"))}, "", "g:new", true},
		{"in another realm", []def.EntrySpec{row("old", "t/old")}, nil,
			[]def.Op{remove("old"), create(iso(row("new", "t/new")))}, "", "new", false},
		{"removed by an earlier plan", []def.EntrySpec{row("old", "t/old")}, []def.Op{remove("old")},
			[]def.Op{create(row("new", "t/new"))}, "", "new", true},
		{"given up by a restart of its group", []def.EntrySpec{group("g"), child("g", "old", "t/old")}, []def.Op{restart("g")},
			[]def.Op{remove("g:old"), create(row("new", "t/new"))}, "", "new", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entered, release, activated := make(chan struct{}), make(chan struct{}), make(chan struct{})
			old := kernel.Define(kernel.Plugin{ID: "t/old", Provides: []kernel.AnyKey{k}, Apply: func(c *kernel.Context, _ any) error {
				if err := k.Provide(c, 1); err != nil {
					return err
				}
				c.Defer("hold", func(context.Context) error { close(entered); <-release; return nil }) // before the provision goes
				return nil
			}})
			nw := kernel.Define(kernel.Plugin{ID: "t/new", Provides: []kernel.AnyKey{k}, Apply: func(c *kernel.Context, _ any) error { return k.Provide(c, 2) }})
			other := kernel.Define(kernel.Plugin{ID: "t/other", Apply: func(*kernel.Context, any) error { return nil }})
			l := newListener("t/listener")
			l.onInit = func(a def.EntryInitArgs) {
				if a.Entry.ID == tc.group {
					close(activated)
				}
			}
			free := sync.OnceFunc(func() { close(release) })
			defer free()
			x := newFx(t, fxOpt{plugins: plugins(old, nw, other), keys: map[string]kernel.AnyKey{"k": k}, pre: []kernel.Plugin{l.plugin}, rows: tc.rows})
			var oldID string
			for _, r := range tc.rows {
				if r.Plugin == "t/old" {
					oldID = r.ID
					x.want(r.ID, kernel.Active)
				}
			}
			if tc.before != nil {
				if _, err := x.ld.Apply(t.Context(), tc.before); err != nil {
					t.Fatalf("Apply(before): %v", err)
				}
				<-entered
				// A group activation that ends gives the fiber up beside the fiber's disposer.
				until(func() bool { e, ok := x.ld.Locate(oldID); return !ok || e.State == kernel.Pending })
			}
			var wg sync.WaitGroup
			var err error
			applied := make(chan struct{})
			wg.Go(func() { defer close(applied); _, err = x.ld.Apply(t.Context(), tc.ops) })
			<-entered // the old provider is disposing and still provides k
			if tc.before != nil {
				// The plan disposes nothing: it has returned, or it holds the new provider.
				until(func() bool {
					select {
					case <-applied:
						return true
					default:
					}
					x.tree.mu.Lock()
					defer x.tree.mu.Unlock()
					return len(x.tree.prev[tc.id]) != 0
				})
			}
			if tc.group != "" {
				<-activated
				x.want(tc.group, kernel.Active) // its activation has mounted what it mounts
			}
			mounted := x.fibers("t/new") != 0
			free()
			wg.Wait()
			if mounted == tc.waits {
				t.Errorf("t/new mounted while the old provider of k disposes: %v, want %v", mounted, !tc.waits)
			}
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			x.settle()
			if st := x.state(tc.id); st != kernel.Active {
				t.Fatalf("%s is %s: %s", tc.id, st, x.failure("t/new"))
			}
			x.consistent()
		})
	}
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
	// A plan and a group activation that mounts wait for each other: the plan's wait
	// ends with its context, the activation's when the activation ends.
	x.tree.excl <- struct{}{}
	if _, _, err := x.tree.first(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("a plan waiting for a group activation: %v", err)
	}
	x.tree.mountUnder(ended(t, x.root), "")
	<-x.tree.excl
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

// A group that the kernel restarts (a provider of a key it injects restarts) retires
// the fibers of its children through its detach, with no Apply to sweep them. The next
// activation forgets those that are DISPOSED when it asks whether any still disposes,
// so the list it walks under the tree's lock holds no more than the fibers really still
// disposing, however often the group restarts.
func TestGroupRestartsKeepNoDisposedPreviousFibers(t *testing.T) {
	gate := kernel.NewKey[int]("gate", 0)
	p := newProbe()
	x := newFx(t, fxOpt{plugins: plugins(provider(p, "t/p", gate), p.plugin("t/c", nil)),
		keys: map[string]kernel.AnyKey{"gate": gate},
		rows: []def.EntrySpec{row("p", "t/p"), gated(), child("g", "c", "t/c")}})
	x.want("g:c", kernel.Active)
	x.tree.mu.Lock()
	pf := x.tree.entries["p"].fiber
	x.tree.mu.Unlock()
	const restarts = 20
	for range restarts {
		if _, err := x.root.Batch(t.Context(), kernel.UpdateOp(pf)); err != nil {
			t.Fatal(err)
		}
		x.settle()
		x.want("g:c", kernel.Active)
	}
	x.tree.mu.Lock()
	n := len(x.tree.prev["g:c"])
	x.tree.mu.Unlock()
	if n > 1 || p.count("t/c") != restarts+1 {
		t.Errorf("after %d restarts of g with no Apply: %d previous fibers of g:c kept, t/c applied %d times", restarts, n, p.count("t/c"))
	}
}

// A plan that restarts or removes the provider of a key a group injects, and creates
// a row under that group, is one batch the kernel admits: the mount goes before the
// operation that ends the group's activation, so the child goes with that activation
// and the group's next one mounts it again. The kernel refuses a child mount after
// an operation of the batch that ends its parent.
func TestPlanThatEndsAGroupThroughItsProviderCreatesAChildUnderIt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ops   func(def.EntrySpec) []def.Op
		state kernel.State // of g:y once settled; "" when it turns on a race of two providers
	}{
		{"restart", func(rp def.EntrySpec) []def.Op {
			rp.Config = map[string]any{"v": int64(2)}
			return []def.Op{update(rp), create(child("g", "y", "t/y"))}
		}, kernel.Active},
		{"remove", func(def.EntrySpec) []def.Op {
			return []def.Op{remove("p"), create(child("g", "y", "t/y"))}
		}, kernel.Pending},
		// p2 may provide k before p has withdrawn it, and the kernel then refuses p2's
		// provision, whatever the order of the batch.
		{"replace", func(def.EntrySpec) []def.Op {
			return []def.Op{remove("p"), create(row("p2", "t/prov2")), create(child("g", "y", "t/y"))}
		}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := kernel.NewKey[int]("k", 0)
			p := newProbe()
			provider := func(id string) kernel.Plugin {
				prov := p.plugin(id, func(c *kernel.Context, _ any) error { return k.Provide(c, 1) })
				prov.Provides = []kernel.AnyKey{k}
				return kernel.Define(prov)
			}
			rp := row("p", "t/prov")
			rp.Config = map[string]any{"v": int64(1)}
			g := group("g")
			g.Inject = []string{"k"}
			x := newFx(t, fxOpt{plugins: plugins(provider("t/prov"), provider("t/prov2"), p.plugin("t/y", nil)),
				keys: map[string]kernel.AnyKey{"k": k}, rows: []def.EntrySpec{rp, g}})
			x.want("g", kernel.Active)
			res := x.apply(tc.ops(rp)...)
			if last := res[len(res)-1]; last.ID != "g:y" || last.Outcome != def.Created {
				t.Errorf("results %v, want g:y created last", res)
			}
			if st := x.state("g:y"); tc.state != "" && st != tc.state {
				t.Errorf("g:y is %s, want %s", st, tc.state)
			}
			x.consistent()
		})
	}
}
