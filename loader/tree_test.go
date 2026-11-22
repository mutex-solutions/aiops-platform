// SPDX-License-Identifier: Apache-2.0

package loader

import (
	"context"
	"errors"
	"sync"
	"testing"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

func TestMountsEnabledRowsOfTheRole(t *testing.T) {
	for _, tc := range []struct {
		role string
		want map[string]def.Mount
	}{
		{"api", map[string]def.Mount{"a": def.Mounted, "w": def.OtherRole, "off": def.Disabled, "none": def.OtherRole}},
		{"worker", map[string]def.Mount{"a": def.OtherRole, "w": def.Mounted, "off": def.Disabled, "none": def.OtherRole}},
		{launch.RoleAll, map[string]def.Mount{"a": def.Mounted, "w": def.Mounted, "off": def.Disabled, "none": def.Mounted}},
	} {
		t.Run(tc.role, func(t *testing.T) {
			p := newProbe()
			a, w, off, none := p.plugin("t/a", nil), p.plugin("t/w", nil), p.plugin("t/off", nil), p.plugin("t/none", nil)
			rw, roff, rnone := row("w", "t/w"), row("off", "t/off"), row("none", "t/none")
			rw.Roles, roff.Enabled, rnone.Roles = []string{"worker"}, false, nil
			x := newFx(t, fxOpt{role: tc.role, plugins: plugins(a, w, off, none), rows: []def.EntrySpec{row("a", "t/a"), rw, roff, rnone}})
			for id, m := range tc.want {
				e, ok := x.ld.Locate(id)
				if !ok || e.Mount != m {
					t.Fatalf("%s: %v %v, want %s", id, e, ok, m)
				}
				if m == def.Mounted {
					x.want(id, kernel.Active)
					if p.count("t/"+id) != 1 {
						t.Errorf("%s applied %d times", id, p.count("t/"+id))
					}
					continue
				}
				if e.State != "" || p.count("t/"+id) != 0 {
					t.Errorf("%s is %q and applied %d times, want no fiber", id, e.State, p.count("t/"+id))
				}
				if _, err := x.ld.Await(t.Context(), id); !errors.Is(err, def.ErrNotMounted) {
					t.Errorf("Await(%s) = %v", id, err)
				}
			}
			if _, ok := x.ld.Locate("nope"); ok {
				t.Error("Locate found an unknown id")
			}
			if _, err := x.ld.Await(t.Context(), "nope"); !errors.Is(err, def.ErrNotFound) {
				t.Errorf("Await(nope) = %v", err)
			}
		})
	}
}

func TestGroupsNestAndGateTheirChildren(t *testing.T) {
	gate := kernel.NewKey[int]("gate", 0)
	p := newProbe()
	leaf, direct := p.plugin("t/leaf", nil), p.plugin("t/direct", nil)
	provider := kernel.Define(kernel.Plugin{ID: "t/gate", Provides: []kernel.AnyKey{gate},
		Apply: func(c *kernel.Context, _ any) error { return gate.Provide(c, 1) }})
	g1 := group("g1")
	g1.Inject = []string{"gate", "gate"}
	g2 := child("g1", "g2", def.GroupPlugin)
	x := newFx(t, fxOpt{plugins: plugins(leaf, direct), keys: map[string]kernel.AnyKey{"gate": gate},
		rows: []def.EntrySpec{g1, g2, child("g1:g2", "leaf", "t/leaf"), child("g1", "direct", "t/direct")}})

	// The group's inject gates every entry beneath it.
	x.want("g1", kernel.Pending)
	for _, id := range []string{"g1:g2", "g1:g2:leaf", "g1:direct"} {
		if e, _ := x.ld.Locate(id); e.Mount != def.Mounted || e.State != kernel.Pending {
			t.Fatalf("%s: %+v", id, e)
		}
		x.want(id, kernel.Pending)
	}
	if p.count("t/leaf") != 0 || p.count("t/direct") != 0 {
		t.Fatal("a child was mounted under a group that is PENDING")
	}

	gf := x.root.Plugin(provider, nil)
	x.settle()
	for _, id := range []string{"g1", "g1:g2", "g1:g2:leaf", "g1:direct"} {
		x.want(id, kernel.Active)
	}
	if p.count("t/leaf") != 1 || p.count("t/direct") != 1 {
		t.Fatalf("applies: leaf %d, direct %d", p.count("t/leaf"), p.count("t/direct"))
	}

	// The provider goes: the group unloads with its whole subtree and the children
	// have no fiber; it returns and mounts them again, one fiber at a time.
	if err := gf.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	x.settle()
	x.want("g1:g2:leaf", kernel.Pending)
	x.root.Plugin(provider, nil)
	x.settle()
	for _, id := range []string{"g1", "g1:g2", "g1:g2:leaf", "g1:direct"} {
		x.want(id, kernel.Active)
	}
	if p.count("t/leaf") != 2 || p.maxLive["t/leaf"] != 1 || p.maxLive["t/direct"] != 1 {
		t.Errorf("leaf applied %d times, at most %d alive", p.count("t/leaf"), p.maxLive["t/leaf"])
	}

	// A child created under the active group is mounted from the group's context
	// in one batch; a group and its child created together are mounted by the group.
	x.apply(create(child("g1", "late", "t/direct")), create(group("g3")), create(child("g3", "k", "t/leaf")))
	x.want("g1:late", kernel.Active)
	x.want("g3:k", kernel.Active)
	x.apply(remove("g1:late"))
	if _, ok := x.ld.Locate("g1:late"); ok {
		t.Error("the removed child is still there")
	}
}

func TestGroupDisabledUnmountsItsChildren(t *testing.T) {
	p := newProbe()
	k := p.plugin("t/k", nil)
	x := newFx(t, fxOpt{plugins: plugins(k), rows: []def.EntrySpec{group("g"), child("g", "k", "t/k")}})
	x.want("g:k", kernel.Active)
	g := group("g")
	g.Enabled = false
	if res := x.apply(update(g)); res[0].Outcome != def.Unmounted {
		t.Fatalf("outcome %v", res)
	}
	if e, _ := x.ld.Locate("g:k"); e.Mount != def.ParentUnmounted || e.State != "" {
		t.Fatalf("child of a disabled group: %+v", e)
	}
	if _, err := x.ld.Await(t.Context(), "g:k"); !errors.Is(err, def.ErrNotMounted) {
		t.Errorf("Await = %v", err)
	}
	if res := x.apply(update(row("g", def.GroupPlugin))); res[0].Outcome != def.Created {
		t.Fatalf("outcome %v", res)
	}
	x.want("g:k", kernel.Active)
	if p.count("t/k") != 2 || p.maxLive["t/k"] != 1 {
		t.Errorf("applies %d, alive at most %d", p.count("t/k"), p.maxLive["t/k"])
	}
	// A group has no config of its own, and a spec with no change does nothing.
	g = group("g")
	g.Config = "ignored"
	if res := x.apply(update(g), update(child("g", "k", "t/k"))); res[0].Outcome != def.Unchanged || res[1].Outcome != def.Unchanged {
		t.Fatalf("outcomes %v", res)
	}
}

type limitCfg struct{ Max int }

func TestIsolateGivesEachSiblingItsOwnRealm(t *testing.T) {
	db := kernel.NewKey[string]("db", 0)
	var seen syncMap
	prov := func(id string) kernel.Plugin {
		return kernel.Define(kernel.Plugin{ID: id, Provides: []kernel.AnyKey{db}, Apply: func(c *kernel.Context, cfg any) error {
			if err := db.Provide(c, cfg.(string)); err != nil {
				return err
			}
			c.Plugin(kernel.Plugin{ID: id + "/user", Inject: []kernel.Dep{kernel.Need(db)}, Apply: func(cc *kernel.Context, _ any) error {
				seen.set(id, db.Of(cc))
				return nil
			}}, nil)
			return nil
		}})
	}
	ra, rb, rc := row("a", "t/a"), row("b", "t/b"), row("c", "t/c")
	ra.Config, rb.Config, rc.Config = "one", "two", "three"
	ra.Isolate, rb.Isolate = []string{"db"}, []string{"db"}
	// c shares the default realm, so it is the one provider of db in it.
	x := newFx(t, fxOpt{plugins: plugins(prov("t/a"), prov("t/b"), prov("t/c")), keys: map[string]kernel.AnyKey{"db": db}, rows: []def.EntrySpec{ra, rb, rc}})
	for _, id := range []string{"a", "b", "c"} {
		x.want(id, kernel.Active)
	}
	if seen.get("t/a") != "one" || seen.get("t/b") != "two" || seen.get("t/c") != "three" {
		t.Errorf("each user must read its own realm: %v", seen.m)
	}

	// Without isolate a second provider in the default realm does not become ACTIVE.
	rd := row("d", "t/c")
	rd.Config = "four"
	x.apply(create(rd))
	if st := x.state("d"); st == kernel.Active {
		t.Errorf("a second provider in the default realm is %s", st)
	}
}

func TestGroupIsolateAndInterceptScopeTheirChildren(t *testing.T) {
	db := kernel.NewKey[string]("db", 0)
	limit := kernel.NewKey[[]limitCfg]("limit", 0, kernel.InterceptConfig[limitCfg]())
	tag := kernel.NewKey[[]any]("tag", 0)
	provide := func(id string, k kernel.AnyKey, fn func(c *kernel.Context) error) kernel.Plugin {
		return kernel.Define(kernel.Plugin{ID: id, Provides: []kernel.AnyKey{k}, Apply: func(c *kernel.Context, _ any) error { return fn(c) }})
	}
	limits := provide("t/limits", limit, func(c *kernel.Context) error {
		return limit.ProvideFor(c, func(caller *kernel.Context) []limitCfg { return kernel.Intercepts[limitCfg](caller, limit) })
	})
	tags := provide("t/tags", tag, func(c *kernel.Context) error {
		return tag.ProvideFor(c, func(caller *kernel.Context) []any { return kernel.Intercepts[any](caller, tag) })
	})
	var got syncMap
	user := kernel.Define(kernel.Plugin{ID: "t/user", Inject: []kernel.Dep{kernel.Need(limit), kernel.Need(tag)},
		Apply: func(c *kernel.Context, cfg any) error {
			got.set(cfg.(string), []any{limit.Of(c), tag.Of(c)})
			return nil
		}})
	dbp := func(id string) kernel.Plugin {
		return kernel.Define(kernel.Plugin{ID: id, Provides: []kernel.AnyKey{db}, Apply: func(c *kernel.Context, _ any) error { return db.Provide(c, id) }})
	}
	d0, d1 := dbp("t/d0"), dbp("t/d1")

	g := group("g")
	g.Isolate = []string{"db"}
	g.Intercept = map[string]any{"limit": map[string]any{"Max": int64(1)}, "tag": "g"}
	inner := child("g", "u", "t/user")
	inner.Config = "inner"
	inner.Intercept = map[string]any{"limit": map[string]any{"Max": float64(2)}, "tag": 7}
	outer := row("o", "t/user")
	outer.Config = "outer"
	pg := child("g", "d1", "t/d1")
	x := newFx(t, fxOpt{plugins: plugins(limits, tags, user, d0, d1),
		keys: map[string]kernel.AnyKey{"db": db, "limit": limit, "tag": tag},
		rows: []def.EntrySpec{row("limits", "t/limits"), row("tags", "t/tags"), g, inner, outer, row("d0", "t/d0"), pg}})
	for _, id := range []string{"limits", "tags", "g", "g:u", "o", "d0", "g:d1"} {
		x.want(id, kernel.Active)
	}
	// The intercept chain is ancestors first, and a typed key decodes its config.
	if v := got.get("inner"); len(v.([]any)[0].([]limitCfg)) != 2 || v.([]any)[0].([]limitCfg)[0].Max != 1 || v.([]any)[0].([]limitCfg)[1].Max != 2 {
		t.Errorf("limit chain of the inner row: %v", v)
	}
	if v := got.get("inner").([]any)[1].([]any); len(v) != 2 || v[0] != "g" || v[1] != 7 {
		t.Errorf("tag chain of the inner row: %v", v)
	}
	if v := got.get("outer").([]any); len(v[0].([]limitCfg)) != 0 || len(v[1].([]any)) != 0 {
		t.Errorf("a row outside the group sees an intercept: %v", v)
	}
	// d0 provides db in the default realm and g:d1 in the group's: both are ACTIVE.
}

func TestARowThatCannotBeBuiltFailsAloneAndSaysWhy(t *testing.T) {
	keyed := kernel.NewKey[int]("keyed", 0, kernel.InterceptConfig[limitCfg]())
	p := newProbe()
	ok := p.plugin("t/ok", nil)
	bad := func(id string, mutate func(*def.EntrySpec)) def.EntrySpec {
		r := row(id, "t/ok")
		mutate(&r)
		return r
	}
	rows := []def.EntrySpec{
		row("ok", "t/ok"),
		bad("plugin", func(r *def.EntrySpec) { r.Plugin = "t/missing" }),
		bad("inject", func(r *def.EntrySpec) { r.Inject = []string{"nope"} }),
		bad("isolate", func(r *def.EntrySpec) { r.Isolate = []string{"nope"} }),
		bad("intercept", func(r *def.EntrySpec) { r.Intercept = map[string]any{"nope": 1} }),
		bad("decode", func(r *def.EntrySpec) { r.Intercept = map[string]any{"keyed": map[string]any{"Max": "x"}} }),
		bad("marshal", func(r *def.EntrySpec) { r.Intercept = map[string]any{"keyed": make(chan int)} }),
		bad("bound", func(r *def.EntrySpec) { r.Isolate = []string{"catalog"} }),
	}
	x := newFx(t, fxOpt{plugins: plugins(ok), keys: map[string]kernel.AnyKey{"keyed": keyed, "catalog": launch.CatalogKey}, rows: rows})
	x.want("ok", kernel.Active)
	for id, why := range map[string]string{
		"plugin": "unknown plugin", "inject": "unknown key name", "isolate": "unknown key name", "intercept": "unknown key name",
		"decode": "intercept", "marshal": "intercept", "bound": "isolation",
	} {
		x.want(id, kernel.Failed)
		if err := x.failure(id); !contains(err, why) {
			t.Errorf("%s fails with %q, want %q", id, err, why)
		}
	}
	if x.state("ok") != kernel.Active || x.fiber == nil {
		t.Fatal("a failed row took the loader down")
	}
	if st, _ := x.fiber.State(t.Context()); st != kernel.Active {
		t.Errorf("the loader is %s", st)
	}

	spec := row("plugin", "t/missing")
	_, _, _, err := x.tree.resolve(t.Context(), x.tree.lc, spec)
	if !errors.Is(err, def.ErrUnknownPlugin) {
		t.Errorf("resolve: %v", err)
	}
	spec = row("inject", "t/ok")
	spec.Inject = []string{"nope"}
	if _, _, _, err := x.tree.resolve(t.Context(), x.tree.lc, spec); !errors.Is(err, def.ErrUnknownKey) {
		t.Errorf("resolve: %v", err)
	}
	spec = row("bound", "t/ok")
	spec.Isolate = []string{"catalog"}
	if _, _, _, err := x.tree.resolve(t.Context(), x.tree.lc, spec); !errors.Is(err, kernel.ErrIsolation) {
		t.Errorf("resolve: %v", err)
	}
}

func TestRowInjectIsNotDeclaredTwice(t *testing.T) {
	k := kernel.NewKey[int]("k", 0)
	p := newProbe()
	has := kernel.Define(kernel.Plugin{ID: "t/has", Inject: []kernel.Dep{kernel.Need(k)}, Apply: func(*kernel.Context, any) error { return nil }})
	prov := kernel.Define(kernel.Plugin{ID: "t/prov", Provides: []kernel.AnyKey{k}, Apply: func(c *kernel.Context, _ any) error { return k.Provide(c, 1) }})
	r := row("r", "t/has")
	r.Inject = []string{"k"}
	x := newFx(t, fxOpt{plugins: plugins(has, p.plugin("t/other", nil)), keys: map[string]kernel.AnyKey{"k": k}, pre: []kernel.Plugin{prov}, rows: []def.EntrySpec{r}})
	x.want("r", kernel.Active)
}

// A row's inject of a key its plugin only Wants makes the key a Need: the row
// waits for it, PENDING, instead of failing on a key declared twice.
func TestRowInjectOfAWantedKeyGatesTheRow(t *testing.T) {
	k := kernel.NewKey[int]("k", 0)
	wants := kernel.Define(kernel.Plugin{ID: "t/wants", Inject: []kernel.Dep{kernel.Want(k)}, Apply: func(*kernel.Context, any) error { return nil }})
	prov := kernel.Define(kernel.Plugin{ID: "t/prov", Provides: []kernel.AnyKey{k}, Apply: func(c *kernel.Context, _ any) error { return k.Provide(c, 1) }})
	r := row("r", "t/wants")
	r.Inject = []string{"k"}
	x := newFx(t, fxOpt{plugins: plugins(wants), keys: map[string]kernel.AnyKey{"k": k}, rows: []def.EntrySpec{r, row("w", "t/wants")}})
	x.want("w", kernel.Active)
	if st := x.state("r"); st != kernel.Pending {
		t.Fatalf("r is %s (%s), want PENDING until k has a provider", st, x.failure("t/wants"))
	}
	x.root.Plugin(prov, nil)
	x.settle()
	x.want("r", kernel.Active)
}

func TestLoaderConfigAndAbsentBatch(t *testing.T) {
	p := newProbe()
	a := p.plugin("t/a", nil)
	x := newFx(t, fxOpt{plugins: plugins(a), cfg: Config{Rows: []def.EntrySpec{row("a", "t/a")}}})
	x.want("a", kernel.Active)
	for _, err := range []error{
		x.ld.Create(t.Context(), row("b", "t/a")), x.ld.Update(t.Context(), row("a", "t/a")), x.ld.Remove(t.Context(), "a"),
	} {
		if !errors.Is(err, def.ErrUnsupported) {
			t.Errorf("without a batch function: %v", err)
		}
	}
	if _, ok := x.ld.Locate("b"); ok {
		t.Error("a refused Create left an entry")
	}

	// A nil config is an empty tree; any other type fails the loader, not the kernel.
	y := newFx(t, fxOpt{plugins: plugins(a), cfg: nil, rows: nil})
	if _, ok := y.ld.Locate("a"); ok {
		t.Error("an empty tree has an entry")
	}
	root, _ := bareKernel(t)
	if err := root.Plugin(New(), 42).Await(t.Context()); err == nil || !contains(err.Error(), "want loader.Config") {
		t.Errorf("a config of the wrong type: %v", err)
	}
	if err := root.Plugin(New(), nil).Await(t.Context()); err != nil {
		t.Errorf("a nil config: %v", err)
	}
}

func TestInitialTreeThatCannotBeStagedFailsTheLoader(t *testing.T) {
	for name, rows := range map[string][]def.EntrySpec{
		"duplicate": {row("a", "t/a"), row("a", "t/a")},
		"no parent": {child("g", "a", "t/a")},
		"not group": {row("a", "t/a"), child("a", "b", "t/a")},
		"empty id":  {row("", "t/a")},
	} {
		t.Run(name, func(t *testing.T) {
			root, _ := bareKernel(t)
			if err := root.Plugin(New(), Config{Rows: rows}).Await(t.Context()); err == nil {
				t.Fatal("the loader mounted an invalid tree")
			}
		})
	}
}

func TestRefusedPlanHasNoEffect(t *testing.T) {
	p := newProbe()
	a := p.plugin("t/a", nil)
	calls := 0
	x := newFx(t, fxOpt{plugins: plugins(a), rows: []def.EntrySpec{row("a", "t/a"), group("g"), child("g", "k", "t/a")},
		batch: func(r *kernel.Root) BatchFunc {
			return func(ctx context.Context, ops ...kernel.BatchOp) ([]*kernel.Fiber, error) {
				calls++
				return r.Batch(ctx, ops...)
			}
		}})
	notGroup := child("a", "x", "t/a")
	for name, tc := range map[string]struct {
		ops  []def.Op
		want error
	}{
		"exists":       {[]def.Op{create(row("n", "t/a")), create(row("a", "t/a"))}, def.ErrExists},
		"no parent":    {[]def.Op{create(child("zz", "x", "t/a"))}, def.ErrParent},
		"not a group":  {[]def.Op{create(notGroup)}, def.ErrNotGroup},
		"update":       {[]def.Op{remove("a"), update(row("nope", "t/a"))}, def.ErrNotFound},
		"remove":       {[]def.Op{remove("nope")}, def.ErrNotFound},
		"parent moved": {[]def.Op{update(row("g:k", "t/a"))}, def.ErrParent},
		"empty id":     {[]def.Op{create(row("n", "t/a")), create(row("", "t/a"))}, def.ErrEmptyID},
	} {
		before := calls
		if _, err := x.ld.Apply(t.Context(), tc.ops); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
		if calls != before {
			t.Errorf("%s: a refused plan reached the kernel", name)
		}
		for _, id := range []string{"a", "g", "g:k"} {
			if e, ok := x.ld.Locate(id); !ok || e.Mount != def.Mounted {
				t.Errorf("%s: %s is gone", name, id)
			}
		}
		if _, ok := x.ld.Locate("n"); ok {
			t.Errorf("%s: n was created", name)
		}
	}
	if _, err := x.ld.Apply(t.Context(), []def.Op{{Kind: "weird", Entry: row("a", "t/a")}}); err == nil {
		t.Error("an unknown operation was accepted")
	}
	if p.count("t/a") != 2 {
		t.Errorf("t/a applied %d times", p.count("t/a"))
	}
}

// syncMap is a map of what the plugins saw, safe across goroutines.
type syncMap struct {
	mu sync.Mutex
	m  map[string]any
}

func (s *syncMap) set(k string, v any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]any{}
	}
	s.m[k] = v
}

func (s *syncMap) get(k string) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[k]
}
