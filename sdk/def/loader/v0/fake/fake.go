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

// Create records the entry and dispatches loader/entry-init.
func (f *Loader) Create(ctx context.Context, e loader.EntrySpec) error {
	f.mu.Lock()
	if _, ok := f.entries[e.ID]; ok {
		f.mu.Unlock()
		return loader.ErrExists
	}
	f.entries[e.ID] = loader.Entry{Spec: e, State: kernel.Active}
	f.mu.Unlock()
	return f.entryInit.Emit(ctx, kernel.Unscoped, loader.EntryInitArgs{Entry: e})
}

// Update replaces the spec of an entry and dispatches loader/volatile-update with
// its new config.
func (f *Loader) Update(ctx context.Context, e loader.EntrySpec) error {
	f.mu.Lock()
	cur, ok := f.entries[e.ID]
	if !ok {
		f.mu.Unlock()
		return loader.ErrNotFound
	}
	cur.Spec = e
	f.entries[e.ID] = cur
	f.mu.Unlock()
	return f.volatile.Emit(ctx, kernel.Unscoped, loader.VolatileUpdateArgs{ID: e.ID, Config: e.Config})
}

// Remove forgets the entry.
func (f *Loader) Remove(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.entries[id]; !ok {
		return loader.ErrNotFound
	}
	delete(f.entries, id)
	return nil
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
