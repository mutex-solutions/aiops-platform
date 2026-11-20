// SPDX-License-Identifier: Apache-2.0

// Package fake is the in-memory entry tree of the loader definition: it records
// entries and their scripted states and mounts no fiber. Its plugin provides
// loader.Key and binds the three loader hooks, so a test of a listener can dispatch
// them (07-c4-plugin-sdk.md:1410). It imports the standard library, sdk/kernel and
// its definition only.
package fake

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"sync"

	loader "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Loader is an in-memory loader.Loader. A new entry is ACTIVE until SetState says
// otherwise.
type Loader struct {
	mu      sync.Mutex
	entries map[string]loader.Entry

	entryInit kernel.EventDispatcher[loader.EntryInitArgs]
	volatile  kernel.EventDispatcher[loader.VolatileUpdateArgs]
	patch     kernel.WaterfallDispatcher[loader.PatchContextArgs, *kernel.Context]
}

var _ loader.Loader = (*Loader)(nil)

// New returns an empty loader and the plugin that provides it under loader.Key.
// Each call returns a new plugin value, a new entry of its own.
func New() (*Loader, kernel.Plugin) {
	f := &Loader{entries: map[string]loader.Entry{}}
	return f, kernel.Define(kernel.Plugin{
		ID:       "aiops/loader-fake",
		Provides: []kernel.AnyKey{loader.Key},
		Apply: func(c *kernel.Context, _ any) error {
			f.entryInit = loader.EntryInit.Bind(c)
			f.volatile = loader.VolatileUpdate.Bind(c)
			f.patch = loader.PatchContext.Bind(c)
			return loader.Key.Provide(c, f)
		},
	})
}

// Apply applies the operations in order, all or nothing: an operation that is
// refused refuses the plan and leaves the entries as they were. The fake mounts
// nothing: every entry is Mounted whatever its Enabled and Roles say, an Update
// that changes the config is Committed and dispatches loader/volatile-update with
// the new config, and a Create dispatches loader/entry-init.
func (f *Loader) Apply(ctx context.Context, ops []loader.Op) ([]loader.Result, error) {
	f.mu.Lock()
	next := maps.Clone(f.entries)
	var res []loader.Result
	var inits []loader.EntrySpec
	var volatiles []loader.VolatileUpdateArgs
	for _, op := range ops {
		id := op.Entry.ID
		cur, ok := next[id]
		r := loader.Result{ID: id}
		switch {
		case op.Kind == loader.OpCreate && ok:
			f.mu.Unlock()
			return nil, loader.ErrExists
		case op.Kind == loader.OpCreate:
			next[id] = loader.Entry{Spec: op.Entry, Mount: loader.Mounted, State: kernel.Active}
			inits, r.Outcome = append(inits, op.Entry), loader.Created
		case !ok:
			f.mu.Unlock()
			return nil, loader.ErrNotFound
		case op.Kind == loader.OpRemove:
			delete(next, id)
			r.Outcome = loader.Removed
		default:
			cur.Spec = op.Entry
			next[id] = cur
			volatiles, r.Outcome = append(volatiles, loader.VolatileUpdateArgs{ID: id, Config: op.Entry.Config}), loader.Committed
		}
		res = append(res, r)
	}
	f.entries = next
	f.mu.Unlock()
	for _, e := range inits {
		if err := f.entryInit.Emit(ctx, kernel.Unscoped, loader.EntryInitArgs{Entry: e}); err != nil {
			return res, err
		}
	}
	for _, v := range volatiles {
		if err := f.volatile.Emit(ctx, kernel.Unscoped, v); err != nil {
			return res, err
		}
	}
	return res, nil
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

// Await returns the scripted state of the entry at once.
func (f *Loader) Await(ctx context.Context, id string) (kernel.State, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entries[id]
	if !ok {
		return "", loader.ErrNotFound
	}
	return e.State, nil
}

// Locate returns the entry of an id.
func (f *Loader) Locate(id string) (loader.Entry, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entries[id]
	return e, ok
}

// SetState scripts the state that Await and Locate report for an entry. It
// reports false for an unknown id.
func (f *Loader) SetState(id string, st kernel.State) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entries[id]
	if ok {
		e.State = st
		f.entries[id] = e
	}
	return ok
}

// Entries lists the entries, sorted by id.
func (f *Loader) Entries() []loader.Entry {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]loader.Entry, 0, len(f.entries))
	for _, e := range f.entries {
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b loader.Entry) int { return cmp.Compare(a.Spec.ID, b.Spec.ID) })
	return out
}

// Patch dispatches loader/patch-context for an entry built on c and returns the
// context the chain ended with. An empty chain returns c: the built-in isolate and
// intercept handling belongs to the real loader, not to this fake.
func (f *Loader) Patch(ctx context.Context, e loader.EntrySpec, c *kernel.Context) (*kernel.Context, error) {
	out, err := f.patch.Run(ctx, kernel.Unscoped, loader.PatchContextArgs{Entry: e, Context: c})
	if err != nil {
		return nil, err
	}
	if out == nil {
		return c, nil
	}
	return out, nil
}
