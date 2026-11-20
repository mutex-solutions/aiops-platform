// SPDX-License-Identifier: Apache-2.0

// Package fake is the in-memory entry tree of the loader definition. It decides
// what the real loader decides, by the same rules: which entries are mounted, the
// refusals, the outcome of each operation, and when the three hooks fire with what.
// It runs no fiber of a row: an entry that gets a fiber is ACTIVE until SetState
// says otherwise, or FAILED when a listener of loader/patch-context fails or
// vetoes it. Its plugin provides loader.Key and binds the three hooks, so a test of
// a listener can dispatch them (07-c4-plugin-sdk.md:1410). The conformance suite
// holds it to the real loader. It imports the standard library, sdk/kernel and the
// loader and launch definitions only.
package fake

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sync"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	loader "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Loader is an in-memory loader.Loader. Set Role and Volatile before its plugin is
// mounted, as the launcher sets them for the real loader.
type Loader struct {
	// Role is the process role: an entry whose Roles do not name it is not mounted.
	// "" and launch.RoleAll mount every role.
	Role string
	// Volatile lists the volatile config paths of each plugin id, as
	// launch.CatalogEntry.Volatile does: an update that changes only those commits in
	// place, any other config change restarts.
	Volatile map[string][]string

	mu      sync.Mutex
	lc      *kernel.Context // the plugin's activation, nil while it has none
	entries map[string]entry
	order   []string // ids in creation order, parents before children

	entryInit kernel.EventDispatcher[loader.EntryInitArgs]
	volatile  kernel.EventDispatcher[loader.VolatileUpdateArgs]
	patch     kernel.WaterfallDispatcher[loader.PatchContextArgs, *kernel.Context]
}

// entry is an entry and its fiber: the scripted state, and the context the fiber
// was mounted from.
type entry struct {
	spec  loader.EntrySpec
	state kernel.State
	ctx   *kernel.Context
}

var _ loader.Loader = (*Loader)(nil)

// New returns an empty loader and the plugin that provides it under loader.Key.
// Each call returns a new plugin value, a new entry of its own.
func New() (*Loader, kernel.Plugin) {
	f := &Loader{entries: map[string]entry{}}
	return f, kernel.Define(kernel.Plugin{
		ID:       "aiops/loader-fake",
		Provides: []kernel.AnyKey{loader.Key},
		Apply: func(c *kernel.Context, _ any) error {
			f.entryInit = loader.EntryInit.Bind(c)
			f.volatile = loader.VolatileUpdate.Bind(c)
			f.patch = loader.PatchContext.Bind(c)
			f.setLC(c)
			c.Defer("loader-fake", func(context.Context) error { f.setLC(nil); return nil })
			return loader.Key.Provide(c, f)
		},
	})
}

func (f *Loader) setLC(c *kernel.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lc = c
}

// mount says whether e has a fiber in the tree entries, by the rule of the real
// loader: it is enabled, of the role, and its group is mounted.
func (f *Loader) mount(entries map[string]entry, e loader.EntrySpec) loader.Mount {
	switch {
	case !e.Enabled:
		return loader.Disabled
	case f.Role != "" && f.Role != launch.RoleAll && !slices.Contains(e.Roles, f.Role):
		return loader.OtherRole
	}
	if p, ok := entries[e.Parent]; ok && f.mount(entries, p.spec) != loader.Mounted {
		return loader.ParentUnmounted
	}
	return loader.Mounted
}

// plan is the tree as the operations of one Apply leave it.
type plan struct {
	entries  map[string]entry
	order    []string
	fresh    map[string]bool // ids that get a new fiber: created, or replaced
	announce []loader.VolatileUpdateArgs
}

// Apply checks the operations in order against the tree as the earlier ones leave
// it, all or nothing, then mounts each entry that gets a new fiber from the context
// that loader/patch-context returns for it, dispatches loader/entry-init for each
// and loader/volatile-update for each commit in place. A child whose group is not
// ACTIVE stays PENDING. A failed dispatch of an event is not an error, as in the real
// loader: the plan is applied.
func (f *Loader) Apply(ctx context.Context, ops []loader.Op) ([]loader.Result, error) {
	f.mu.Lock()
	if f.lc == nil {
		f.mu.Unlock()
		return nil, kernel.ErrInactive
	}
	p := &plan{entries: maps.Clone(f.entries), order: slices.Clone(f.order), fresh: map[string]bool{}}
	var res []loader.Result
	for i, op := range ops {
		r, err := f.stage(p, op)
		if err != nil {
			f.mu.Unlock()
			return nil, fmt.Errorf("operation %d (%s %q): %w", i, op.Kind, op.Entry.ID, err)
		}
		res = append(res, r)
	}
	// A new fiber for an entry mounted now that was not before, was created or
	// replaced, or whose group has a new fiber.
	var born []string
	for _, id := range p.order {
		e := p.entries[id]
		old, had := f.entries[id]
		if f.mount(p.entries, e.spec) == loader.Mounted && (!had || p.fresh[id] || slices.Contains(born, e.spec.Parent) ||
			f.mount(f.entries, old.spec) != loader.Mounted) {
			e.state, e.ctx = kernel.Pending, nil
			p.entries[id], born = e, append(born, id)
		}
	}
	f.entries, f.order = p.entries, p.order
	lc := f.lc
	f.mu.Unlock()
	var inits []loader.EntryInitArgs
	for _, id := range born {
		f.mu.Lock()
		e, ok := f.entries[id]
		g, under := f.entries[e.spec.Parent]
		f.mu.Unlock()
		pc := lc
		if under {
			pc = g.ctx
		}
		if !ok || under && (g.state != kernel.Active || pc == nil) {
			continue // its group mounts it when it is ACTIVE
		}
		mc, err := f.Patch(ctx, e.spec, pc)
		st := kernel.Active
		if err != nil {
			mc, st = pc, kernel.Failed
		}
		f.mu.Lock()
		if e, ok := f.entries[id]; ok {
			e.state, e.ctx = st, mc
			f.entries[id] = e
		}
		f.mu.Unlock()
		inits = append(inits, loader.EntryInitArgs{Entry: e.spec, Context: mc})
	}
	for _, a := range inits {
		_ = f.entryInit.Emit(ctx, kernel.Unscoped, a)
	}
	for _, a := range p.announce {
		_ = f.volatile.Emit(ctx, kernel.Unscoped, a)
	}
	return res, nil
}

// stage applies one operation to p, or refuses it, as the real loader does.
func (f *Loader) stage(p *plan, op loader.Op) (loader.Result, error) {
	e := cloneSpec(op.Entry)
	cur, ok := p.entries[e.ID]
	r := loader.Result{ID: e.ID}
	switch op.Kind {
	case loader.OpCreate:
		g, under := p.entries[e.Parent]
		switch {
		case ok:
			return r, loader.ErrExists
		case e.Parent != "" && !under:
			return r, loader.ErrParent
		case under && !isGroup(g.spec.Plugin):
			return r, loader.ErrNotGroup
		}
		p.entries[e.ID], p.order, p.fresh[e.ID] = entry{spec: e}, append(p.order, e.ID), true
		r.Outcome = loader.Recorded
		if f.mount(p.entries, e) == loader.Mounted {
			r.Outcome = loader.Created
		}
	case loader.OpRemove:
		if !ok {
			return r, loader.ErrNotFound
		}
		gone := []string{e.ID}
		for _, id := range p.order {
			if slices.Contains(gone, p.entries[id].spec.Parent) {
				gone = append(gone, id)
			}
		}
		p.order = slices.DeleteFunc(p.order, func(id string) bool { return slices.Contains(gone, id) })
		for _, id := range gone {
			delete(p.entries, id)
		}
		r.Outcome = loader.Removed
	case loader.OpUpdate:
		switch {
		case !ok:
			return r, loader.ErrNotFound
		case e.Parent != cur.spec.Parent:
			return r, loader.ErrParent
		}
		was := f.mount(p.entries, cur.spec)
		old := cur.spec
		cur.spec = e
		p.entries[e.ID] = cur
		r.Outcome = f.outcome(p, old, e, was)
	default:
		return r, fmt.Errorf("unknown operation kind %q", op.Kind)
	}
	return r, nil
}

// outcome is what a new spec e of an entry that had old does (07-c4-plugin-sdk.md:1374).
func (f *Loader) outcome(p *plan, old, e loader.EntrySpec, was loader.Mount) loader.Outcome {
	now := f.mount(p.entries, e)
	switch {
	case was != loader.Mounted && now != loader.Mounted:
		return loader.Unchanged
	case was != loader.Mounted:
		p.fresh[e.ID] = true
		return loader.Created
	case now != loader.Mounted:
		return loader.Unmounted
	case old.Plugin != e.Plugin || !sameSet(old.Inject, e.Inject) || !sameSet(old.Isolate, e.Isolate) ||
		!(len(old.Intercept) == 0 && len(e.Intercept) == 0 || reflect.DeepEqual(old.Intercept, e.Intercept)):
		p.fresh[e.ID] = true
		return loader.Restarted
	case isGroup(e.Plugin): // a group has no config of its own
		return loader.Unchanged
	}
	paths, only := loader.VolatileOnly(old.Config, e.Config, f.Volatile[e.Plugin])
	switch {
	case len(paths) == 0:
		return loader.Unchanged
	case only:
		p.announce = append(p.announce, loader.VolatileUpdateArgs{ID: e.ID, Config: e.Config, Paths: paths})
		return loader.Committed
	}
	return loader.Restarted // in place: the fiber and its scripted state stay
}

func isGroup(plugin string) bool { return plugin == loader.GroupPlugin || plugin == loader.IncludePlugin }

func sameSet(a, b []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b)))
}

func cloneSpec(e loader.EntrySpec) loader.EntrySpec {
	e.Roles, e.Inject, e.Isolate, e.Intercept = slices.Clone(e.Roles), slices.Clone(e.Inject), slices.Clone(e.Isolate), maps.Clone(e.Intercept)
	return e
}

func (f *Loader) one(ctx context.Context, op loader.Op) error {
	_, err := f.Apply(ctx, []loader.Op{op})
	return err
}

// Create is Apply with one create.
func (f *Loader) Create(ctx context.Context, e loader.EntrySpec) error {
	return f.one(ctx, loader.Op{Kind: loader.OpCreate, Entry: e})
}

// Update is Apply with one update.
func (f *Loader) Update(ctx context.Context, e loader.EntrySpec) error {
	return f.one(ctx, loader.Op{Kind: loader.OpUpdate, Entry: e})
}

// Remove is Apply with one remove.
func (f *Loader) Remove(ctx context.Context, id string) error {
	return f.one(ctx, loader.Op{Kind: loader.OpRemove, Entry: loader.EntrySpec{ID: id}})
}

// Await returns the scripted state of a mounted entry at once.
func (f *Loader) Await(ctx context.Context, id string) (kernel.State, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	e, ok := f.Locate(id)
	switch {
	case !ok:
		return "", loader.ErrNotFound
	case e.Mount != loader.Mounted:
		return "", loader.ErrNotMounted
	}
	return e.State, nil
}

// Locate returns the entry of an id, mounted or not.
func (f *Loader) Locate(id string) (loader.Entry, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entries[id]
	return f.view(e), ok
}

// view is e as Locate returns it: a state only while it is mounted. Call with mu held.
func (f *Loader) view(e entry) loader.Entry {
	out := loader.Entry{Spec: cloneSpec(e.spec), Mount: f.mount(f.entries, e.spec)}
	if out.Mount == loader.Mounted {
		out.State = e.state
	}
	return out
}

// SetState scripts the state that Await and Locate report for an entry while it is
// mounted. It reports false for an unknown id.
func (f *Loader) SetState(id string, st kernel.State) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entries[id]
	if ok {
		e.state = st
		f.entries[id] = e
	}
	return ok
}

// Entries lists the entries as Locate returns them, sorted by id.
func (f *Loader) Entries() []loader.Entry {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]loader.Entry, 0, len(f.entries))
	for _, e := range f.entries {
		out = append(out, f.view(e))
	}
	slices.SortFunc(out, func(a, b loader.Entry) int { return cmp.Compare(a.Spec.ID, b.Spec.ID) })
	return out
}

// Patch dispatches loader/patch-context for an entry mounted from c and returns the
// context the chain ended with. Its innermost step stands for the real loader's,
// which applies the row's isolate and intercept: it returns a new context derived
// from c, never c, so that a listener that drops what next returned is seen (the
// fake has no key table and applies neither). A listener that does not call next,
// or returns no context, gives loader.ErrPatchVetoed, as in the real loader.
func (f *Loader) Patch(ctx context.Context, e loader.EntrySpec, c *kernel.Context) (*kernel.Context, error) {
	var built *kernel.Context
	out, err := f.patch.RunWith(ctx, kernel.Unscoped, loader.PatchContextArgs{Entry: e, Context: c},
		func(context.Context, loader.PatchContextArgs) (*kernel.Context, error) { built = c.Extend(); return built, nil })
	if err == nil && (built == nil || out == nil) {
		err = loader.ErrPatchVetoed
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}
