// SPDX-License-Identifier: Apache-2.0

// Package loader is the loader plugin: the entry tree (03-c3-kernel.md:64). The
// launcher mounts it first (03-c3-kernel.md:360; 07-c4-plugin-sdk.md:1360). It owns
// one fiber per enabled row of the composition whose roles include the process role,
// mounted as its children, with the row's inject, intercept and isolate applied to
// the row's context (07-c4-plugin-sdk.md:1347). It provides the loader key and the
// composition key.
//
// A group (core:group, and core:include after the launcher resolved it) is an entry
// whose own fiber mounts the entries that name it as Parent, so a group's inject
// gates them and its isolate and intercept scope them (00-decisions.md:475;
// 07-c4-plugin-sdk.md:1252).
//
// The package imports the standard library, sdk/kernel and sdk/def only: it reads
// no file, no YAML and no other plugin.
package loader

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sync"

	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// BatchFunc commits operations as one kernel batch: it is (*kernel.Root).Batch. A
// plugin never holds the Root, and the kernel has no batch on a context, so the
// launcher hands the loader this one function in its config, the value of
// "root.Plugin(loader, tree)" (07-c4-plugin-sdk.md:1360). A launch key would be a
// seventh one (03-c3-kernel.md:63 names six), and it would offer the power to
// dispose any fiber to every consumer of the key.
type BatchFunc func(ctx context.Context, ops ...kernel.BatchOp) ([]*kernel.Fiber, error)

// Config is the config of the loader plugin: the tree to mount, parents before
// children, and the batch function. A loader with no Batch mounts Rows and serves
// its keys, and Apply answers ErrUnsupported.
type Config struct {
	Rows  []def.EntrySpec
	Batch BatchFunc
}

// New returns the loader plugin. Each call returns a new plugin value, a new entry:
// the module table holds one, and the binding maps list it.
func New() kernel.Plugin {
	return kernel.Define(kernel.Plugin{
		ID:       "aiops/loader",
		Inject:   []kernel.Dep{kernel.Need(launch.ProfileContextKey), kernel.Need(launch.CatalogKey)},
		Provides: []kernel.AnyKey{def.Key, composition.Key},
		Decode: func(raw any) (any, error) {
			switch v := raw.(type) {
			case nil:
				return Config{}, nil
			case Config:
				return v, nil
			}
			return nil, fmt.Errorf("loader: config is %T, want loader.Config", raw)
		},
		Apply: func(c *kernel.Context, raw any) error {
			cfg := raw.(Config)
			t := &tree{
				role: launch.ProfileContextKey.Of(c).Role(), cat: launch.CatalogKey.Of(c), batch: cfg.Batch,
				sem: make(chan struct{}, 1), lc: c,
				patch: def.PatchContext.Bind(c), init: def.EntryInit.Bind(c), volatile: def.VolatileUpdate.Bind(c),
				entries: map[string]*entry{}, prev: map[string][]*kernel.Fiber{},
			}
			c.Defer("loader-tree", func(context.Context) error { t.close(); return nil })
			ops := make([]def.Op, len(cfg.Rows))
			for i, r := range cfg.Rows {
				ops[i] = def.Op{Kind: def.OpCreate, Entry: r}
			}
			if _, err := t.stage(c.Std(), ops); err != nil {
				return err
			}
			if err := def.Key.Provide(c, t); err != nil {
				return err
			}
			if err := composition.Key.Provide(c, reader{t}); err != nil {
				return err
			}
			t.mountUnder(c, "")
			return nil
		},
	})
}

// entry is one row of the tree. fiber is nil while the entry has none: it is not
// mounted, or its group is not active. kids is the context a group mounts its
// children from, set while the group's activation lives.
type entry struct {
	spec  def.EntrySpec
	fiber *kernel.Fiber
	kids  *kernel.Context
}

type tree struct {
	role  string
	cat   launch.Catalog
	batch BatchFunc
	sem   chan struct{} // one Apply at a time; a channel so that a wait can end with its context

	patch    kernel.WaterfallDispatcher[def.PatchContextArgs, *kernel.Context]
	init     kernel.EventDispatcher[def.EntryInitArgs]
	volatile kernel.EventDispatcher[def.VolatileUpdateArgs]

	mu      sync.Mutex // guards what follows; never held across a kernel call that runs a hook
	lc      *kernel.Context
	entries map[string]*entry
	order   []string                   // ids in creation order: parents before children
	prev    map[string][]*kernel.Fiber // removed fibers that may not be DISPOSED yet, by entry id
}

var _ def.Loader = (*tree)(nil)

func (t *tree) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lc, t.order = nil, nil
	clear(t.entries)
	clear(t.prev)
}

func isGroup(plugin string) bool { return plugin == def.GroupPlugin || plugin == def.IncludePlugin }

// mount says whether e has a fiber: an entry that is disabled or of another role has
// none, nor has one under a group that has none (03-c3-kernel.md:64;
// 03-c3-kernel.md:547: --role all mounts every row). Call with mu held.
func (t *tree) mount(e *entry) def.Mount {
	switch {
	case !e.spec.Enabled:
		return def.Disabled
	case t.role != launch.RoleAll && !slices.Contains(e.spec.Roles, t.role):
		return def.OtherRole
	}
	if p := t.entries[e.spec.Parent]; p != nil && t.mount(p) != def.Mounted {
		return def.ParentUnmounted
	}
	return def.Mounted
}

// parentCtx is the context e is mounted from, nil while its group has none.
func (t *tree) parentCtx(e *entry) *kernel.Context {
	if e.spec.Parent == "" {
		return t.lc
	}
	return t.entries[e.spec.Parent].kids // a child always has its group: Remove takes both
}

func cloneSpec(e def.EntrySpec) def.EntrySpec {
	e.Roles, e.Inject, e.Isolate, e.Intercept = slices.Clone(e.Roles), slices.Clone(e.Inject), slices.Clone(e.Isolate), maps.Clone(e.Intercept)
	return e
}

// Create, Update and Remove are Apply with one operation: one code path.
func (t *tree) Create(ctx context.Context, e def.EntrySpec) error {
	return t.one(ctx, def.Op{Kind: def.OpCreate, Entry: e})
}

func (t *tree) Update(ctx context.Context, e def.EntrySpec) error {
	return t.one(ctx, def.Op{Kind: def.OpUpdate, Entry: e})
}

func (t *tree) Remove(ctx context.Context, id string) error {
	return t.one(ctx, def.Op{Kind: def.OpRemove, Entry: def.EntrySpec{ID: id}})
}

func (t *tree) one(ctx context.Context, op def.Op) error {
	_, err := t.Apply(ctx, []def.Op{op})
	return err
}

// Locate returns the entry of id with the state of its fiber.
func (t *tree) Locate(id string) (def.Entry, bool) {
	t.mu.Lock()
	e := t.entries[id]
	if e == nil {
		t.mu.Unlock()
		return def.Entry{}, false
	}
	out, f := def.Entry{Spec: cloneSpec(e.spec), Mount: t.mount(e)}, e.fiber
	t.mu.Unlock()
	if out.Mount == def.Mounted {
		out.State = kernel.Pending
		if f != nil {
			out.State, _ = f.State(context.Background())
		}
	}
	return out, true
}

// Await waits for the fiber of id to settle. A child whose group has not mounted it
// yet waits for the group first, and is PENDING if it still has no fiber.
func (t *tree) Await(ctx context.Context, id string) (kernel.State, error) {
	f, parent, err := t.fiberOf(id)
	if err != nil {
		return "", err
	}
	if f == nil && parent != "" {
		if _, err := t.Await(ctx, parent); err != nil {
			return "", err
		}
		f, _, _ = t.fiberOf(id)
	}
	if f == nil {
		return kernel.Pending, nil
	}
	_ = f.Await(ctx) // a startup error is the FAILED state, which is the answer
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return f.State(ctx)
}

func (t *tree) fiberOf(id string) (*kernel.Fiber, string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.entries[id]
	switch {
	case e == nil:
		return nil, "", def.ErrNotFound
	case t.mount(e) != def.Mounted:
		return nil, "", def.ErrNotMounted
	}
	return e.fiber, e.spec.Parent, nil
}

// group is the plugin of a group entry: it mounts the entries under id whenever it
// activates, which is again after each restart, and gives their fibers up when it
// unloads. Its inject, which gates it, is added by resolve.
func (t *tree) group(e def.EntrySpec) kernel.Plugin {
	return kernel.Plugin{ID: e.Plugin, Apply: func(c *kernel.Context, _ any) error {
		c.Defer("group-children", func(context.Context) error { t.detach(e.ID, c); return nil })
		t.mountUnder(c, e.ID)
		return nil
	}}
}

// detach forgets the fibers of the children of group id, which went with the
// activation c of the group.
func (t *tree) detach(id string, c *kernel.Context) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if g := t.entries[id]; g != nil && g.kids == c {
		g.kids = nil
		for _, k := range t.order {
			if e := t.entries[k]; e.spec.Parent == id && e.fiber != nil {
				t.addPrev(k, e.fiber)
				e.fiber = nil
			}
		}
	}
}

func (t *tree) addPrev(id string, f *kernel.Fiber) { t.prev[id] = append(slices.Clone(t.prev[id]), f) }

// mountUnder mounts, from c, every entry that is to be mounted under parent: the
// loader's own activation for "" and a group's for its id. It runs in the Apply of
// that fiber, so it uses Context.Plugin.
func (t *tree) mountUnder(c *kernel.Context, parent string) {
	t.mu.Lock()
	if g := t.entries[parent]; g != nil {
		g.kids = c
	}
	var specs []def.EntrySpec
	for _, id := range t.order {
		if e := t.entries[id]; e.spec.Parent == parent && t.mount(e) == def.Mounted {
			specs = append(specs, e.spec)
		}
	}
	t.mu.Unlock()
	for _, s := range specs {
		p, cfg, mc := t.build(c.Std(), c, s)
		f := mc.Plugin(p, cfg)
		t.adopt(c.Std(), mountItem{spec: s, pc: c, mc: mc}, f)
	}
	// Registered after the children, so it runs before the kernel's own walk of them.
	c.Defer("children-in-reverse", func(ctx context.Context) error { t.teardown(ctx, parent); return nil })
}

// teardown disposes the fibers of the entries under parent one at a time, the last
// created first, each until it is DISPOSED. The kernel disposes the children of a
// fiber as the effects that mounted them, in reverse order, but it does not wait for
// one before it starts the next, so their disposers overlap; the tree must come down
// in reverse (03-c3-kernel.md:370 "disposers in strict reverse order"). It reads the
// tree when it runs, so it covers the entries that a batch mounted later. A failure
// to dispose is left to the kernel's own walk.
func (t *tree) teardown(ctx context.Context, parent string) {
	t.mu.Lock()
	var fibers []*kernel.Fiber
	for _, id := range slices.Backward(t.order) {
		if e := t.entries[id]; e.spec.Parent == parent && e.fiber != nil {
			fibers = append(fibers, e.fiber)
		}
	}
	t.mu.Unlock()
	for _, f := range fibers {
		_ = f.Dispose(ctx)
		_ = f.Await(ctx)
	}
}

// mountItem is an entry that is being mounted from pc, with the context made for it.
type mountItem struct {
	spec def.EntrySpec
	pc   *kernel.Context
	mc   *kernel.Context
	op   kernel.BatchOp
}

// adopt records f as the fiber of the entry, unless its group has restarted since
// pc was read: the group's new activation mounts the entry itself. It then
// dispatches loader/entry-init. A dispatch error means the loader ended or ctx
// ended: the entry is created, so it is not an error of the operation.
func (t *tree) adopt(ctx context.Context, m mountItem, f *kernel.Fiber) {
	t.mu.Lock()
	if e := t.entries[m.spec.ID]; e != nil && t.parentCtx(e) == m.pc {
		e.fiber = f
	}
	t.mu.Unlock()
	_ = t.init.Emit(ctx, kernel.Unscoped, def.EntryInitArgs{Entry: m.spec, Context: m.mc})
}

// build makes what mounts e from pc: the plugin, its config and the context. A row
// that cannot be built (an unknown plugin or key name, an isolate or intercept the
// kernel refuses, a listener of loader/patch-context that fails) is mounted as a
// fiber whose Apply returns the reason: that row is FAILED and the loader is not.
func (t *tree) build(ctx context.Context, pc *kernel.Context, e def.EntrySpec) (kernel.Plugin, any, *kernel.Context) {
	p, cfg, mc, err := t.resolve(ctx, pc, e)
	if err != nil {
		return kernel.Plugin{ID: e.ID, Apply: func(*kernel.Context, any) error { return err }}, nil, pc
	}
	return p, cfg, mc
}

func (t *tree) resolve(ctx context.Context, pc *kernel.Context, e def.EntrySpec) (p kernel.Plugin, cfg any, mc *kernel.Context, err error) {
	if isGroup(e.Plugin) {
		p = t.group(e)
	} else {
		var ok bool
		if p, ok = t.cat.Plugin(e.Plugin); !ok {
			return p, nil, nil, fmt.Errorf("row %q: plugin %q: %w", e.ID, e.Plugin, def.ErrUnknownPlugin)
		}
		cfg = e.Config
	}
	key := func(list string, name string) (kernel.AnyKey, error) {
		k, ok := t.cat.Key(name)
		if !ok {
			return nil, fmt.Errorf("row %q: %s %q: %w", e.ID, list, name, def.ErrUnknownKey)
		}
		return k, nil
	}
	p.Inject = slices.Clone(p.Inject)
	for _, n := range e.Inject {
		k, err := key("inject", n)
		if err != nil {
			return p, nil, nil, err
		}
		if !slices.ContainsFunc(p.Inject, func(d kernel.Dep) bool { return !d.Optional() && sameKey(d.Key(), k) }) {
			p.Inject = append(p.Inject, kernel.NeedKey(k))
		}
	}
	mc = pc
	for _, n := range e.Isolate {
		k, err := key("isolate", n)
		if err == nil {
			mc, err = guard(func() *kernel.Context { return mc.Isolate(k) })
		}
		if err != nil {
			return p, nil, nil, err
		}
	}
	for _, n := range slices.Sorted(maps.Keys(e.Intercept)) {
		k, err := key("intercept", n)
		var v any
		if err == nil {
			v, err = typed(k, e.Intercept[n])
		}
		if err == nil {
			mc, err = guard(func() *kernel.Context { return mc.Intercept(k, v) })
		}
		if err != nil {
			return p, nil, nil, err
		}
	}
	patched, err := t.patch.Run(ctx, kernel.Unscoped, def.PatchContextArgs{Entry: e, Context: mc})
	if err != nil {
		return p, nil, nil, fmt.Errorf("row %q: loader/patch-context: %w", e.ID, err)
	}
	if patched != nil {
		mc = patched
	}
	return p, cfg, mc, nil
}

func sameKey(a, b kernel.AnyKey) bool { return a.Name() == b.Name() && a.Major() == b.Major() }

// guard runs f, which Isolate or Intercept refuse by panicking, and returns the
// refusal as an error: the loader fails the row, never itself.
func guard(f func() *kernel.Context) (c *kernel.Context, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = r.(error)
		}
	}()
	return f(), nil
}

// typed makes the value that Context.Intercept takes for k from the parsed config v:
// a key with a declared intercept config type gets v decoded into it through its
// JSON form, a key with none gets v itself (kernel.InterceptType).
func typed(k kernel.AnyKey, v any) (any, error) {
	t, ok := kernel.InterceptType(k)
	if !ok {
		return v, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("intercept %q: %w", k.Name(), err)
	}
	out := reflect.New(t)
	if err := json.Unmarshal(b, out.Interface()); err != nil {
		return nil, fmt.Errorf("intercept %q: %w", k.Name(), err)
	}
	return out.Elem().Interface(), nil
}
