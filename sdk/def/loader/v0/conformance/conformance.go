// SPDX-License-Identifier: Apache-2.0

// Package conformance is the suite of the loader definition: what the real loader
// and the fake must both do (07-c4-plugin-sdk.md, T2 "every provider and the fake
// behave the same"). It drives a loader through its interface and its three hooks
// and checks the outcomes, the refusals, the mount state and fiber state of each
// entry, and the payloads the hooks carry. It imports the standard library,
// sdk/kernel and the definition only.
package conformance

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"

	loader "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Setup is what the launcher gives a loader: the role of the process, the plugins
// that rows name, by id, and the volatile config paths of each plugin id.
type Setup struct {
	Role     string
	Plugins  map[string]kernel.Plugin
	Volatile map[string][]string
}

// Mount returns a loader with no entry, set up as s says, that dispatches its hooks
// in the returned root, on which the suite mounts its listener.
type Mount func(t *testing.T, s Setup) (loader.Loader, *kernel.Root)

// The process role and the two plugins that rows name. Neither plugin does anything.
const (
	role = "api"
	p    = "conformance/p"
	q    = "conformance/q"
)

const group = loader.GroupPlugin

// Run runs the suite, each case on a loader of its own.
func Run(t *testing.T, mount Mount) {
	t.Run("mount states", func(t *testing.T) {
		s := start(t, mount, nil)
		off, other, h := row("off", p), row("w", p), row("h", group)
		off.Enabled, other.Roles, h.Enabled = false, []string{"worker"}, false
		s.apply([]string{"created a", "recorded off", "recorded w", "created g", "created g:c", "recorded h", "recorded h:c"},
			create(row("a", p)), create(off), create(other), create(row("g", group)), create(child("g", "c", p)), create(h), create(child("h", "c", p)))
		s.is("a", loader.Mounted, kernel.Active)
		s.is("g", loader.Mounted, kernel.Active)
		s.is("g:c", loader.Mounted, kernel.Active)
		s.is("off", loader.Disabled, "")
		s.is("w", loader.OtherRole, "")
		s.is("h", loader.Disabled, "")
		s.is("h:c", loader.ParentUnmounted, "")
		if _, err := s.ld.Await(t.Context(), "nope"); !errors.Is(err, loader.ErrNotFound) {
			t.Errorf("Await of an unknown id = %v", err)
		}
		if _, ok := s.ld.Locate("nope"); ok {
			t.Error("Locate found an unknown id")
		}
		h.Enabled = true // enabling a group mounts the entries under it
		s.apply([]string{"created h"}, update(h))
		s.is("h", loader.Mounted, kernel.Active)
		s.is("h:c", loader.Mounted, kernel.Active)
		s.heard([]string{"a", "g", "g:c", "h", "h:c"})
	})

	t.Run("a refused plan changes nothing", func(t *testing.T) {
		s := start(t, mount, nil)
		s.apply([]string{"created a", "created g"}, create(row("a", p)), create(row("g", group)))
		orphan, under, moved, changed := row("x", p), row("x", p), row("a", p), row("a", p)
		orphan.Parent, under.Parent, moved.Parent, changed.Config = "nope", "a", "g", "changed"
		for _, tc := range []struct {
			want error
			ops  []loader.Op
		}{
			{loader.ErrExists, []loader.Op{create(row("x", p)), create(row("a", p))}},
			{loader.ErrParent, []loader.Op{create(orphan)}},
			{loader.ErrNotGroup, []loader.Op{create(under)}},
			{loader.ErrParent, []loader.Op{update(moved)}},
			{loader.ErrNotFound, []loader.Op{update(changed), update(row("x", p))}},
			{loader.ErrNotFound, []loader.Op{remove("x")}},
			{nil, []loader.Op{create(row("x", p)), {Kind: "rename", Entry: row("a", p)}}}, // any error
		} {
			if _, err := s.ld.Apply(t.Context(), tc.ops); err == nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("Apply = %v, want %v", err, tc.want)
			}
			if _, ok := s.ld.Locate("x"); ok {
				t.Error("a refused plan created x")
			}
			if e, _ := s.ld.Locate("a"); e.Spec.Config != nil || e.Spec.Parent != "" {
				t.Errorf("a refused plan changed a: %+v", e.Spec)
			}
		}
		s.is("a", loader.Mounted, kernel.Active)
	})

	t.Run("updates", func(t *testing.T) {
		s := start(t, mount, map[string][]string{p: {"level"}})
		a, off, g := row("a", p), row("off", p), row("g", group)
		a.Config, off.Enabled = map[string]any{"level": "info", "port": int64(1)}, false
		s.apply([]string{"created a", "recorded off", "created g"}, create(a), create(off), create(g))
		s.is("a", loader.Mounted, kernel.Active)
		s.apply([]string{"unchanged a"}, update(a))
		a.Config = map[string]any{"level": "debug", "port": int64(1)}
		s.apply([]string{"committed a"}, update(a))
		s.is("a", loader.Mounted, kernel.Active)
		a.Config = map[string]any{"level": "debug", "port": int64(2)}
		s.apply([]string{"restarted a"}, update(a))
		s.is("a", loader.Mounted, kernel.Active)
		a.Plugin = q // a new plugin is a new fiber
		s.apply([]string{"restarted a"}, update(a))
		s.is("a", loader.Mounted, kernel.Active)
		off.Config, g.Config = "changed", "changed"
		s.apply([]string{"unchanged off", "unchanged g"}, update(off), update(g))
		off.Enabled = true
		s.apply([]string{"created off"}, update(off))
		s.is("off", loader.Mounted, kernel.Active)
		a.Enabled = false
		s.apply([]string{"unmounted a"}, update(a))
		s.is("a", loader.Disabled, "")
		s.heard([]string{"a", "a", "g", "off"})
		s.h.mu.Lock()
		defer s.h.mu.Unlock()
		want := []loader.VolatileUpdateArgs{{ID: "a", Config: map[string]any{"level": "debug", "port": int64(1)}, Paths: []string{"level"}}}
		if !reflect.DeepEqual(s.h.volatile, want) {
			t.Errorf("volatile-update heard %+v, want %+v", s.h.volatile, want)
		}
	})

	t.Run("remove takes the entries beneath", func(t *testing.T) {
		s := start(t, mount, nil)
		s.apply([]string{"created g", "created g:c", "created a"}, create(row("g", group)), create(child("g", "c", p)), create(row("a", p)))
		s.is("g:c", loader.Mounted, kernel.Active)
		s.apply([]string{"removed g"}, remove("g"))
		for _, id := range []string{"g", "g:c"} {
			if _, ok := s.ld.Locate(id); ok {
				t.Errorf("%s survived the remove of g", id)
			}
		}
		s.is("a", loader.Mounted, kernel.Active)
		s.apply([]string{"created g", "created g:c"}, create(row("g", group)), create(child("g", "c", p)))
		s.is("g:c", loader.Mounted, kernel.Active)
	})

	// The built-in step is innermost (ADR-034 G8): a listener that does not call next,
	// or returns no context, vetoes the entry, which is FAILED; the entries under a
	// vetoed group wait, PENDING. entry-init carries the context the entry was mounted
	// from, the one the chain returned.
	t.Run("patch-context and entry-init", func(t *testing.T) {
		s := start(t, mount, nil)
		s.apply([]string{"created ok", "created veto", "created veto:c", "created nil"},
			create(row("ok", p)), create(row("veto", group)), create(child("veto", "c", p)), create(row("nil", p)))
		s.is("ok", loader.Mounted, kernel.Active)
		s.is("veto", loader.Mounted, kernel.Failed)
		s.is("veto:c", loader.Mounted, kernel.Pending)
		s.is("nil", loader.Mounted, kernel.Failed)
		s.heard([]string{"nil", "ok", "veto"})
		s.h.mu.Lock()
		defer s.h.mu.Unlock()
		for _, in := range s.h.inits {
			if in.Context == nil || in.Entry.ID == "ok" && in.Context != s.h.built["ok"] {
				t.Errorf("entry-init of %s has the context %p, the chain returned %p", in.Entry.ID, in.Context, s.h.built[in.Entry.ID])
			}
		}
	})
}

type suite struct {
	t  *testing.T
	ld loader.Loader
	h  *heard
}

func start(t *testing.T, mount Mount, volatile map[string][]string) *suite {
	t.Helper()
	plugins := map[string]kernel.Plugin{}
	for _, id := range []string{p, q} {
		plugins[id] = kernel.Define(kernel.Plugin{ID: id, Apply: func(*kernel.Context, any) error { return nil }})
	}
	ld, root := mount(t, Setup{Role: role, Plugins: plugins, Volatile: volatile})
	return &suite{t: t, ld: ld, h: listen(t, root)}
}

// apply applies ops and wants the outcomes, as "<outcome> <id>".
func (s *suite) apply(want []string, ops ...loader.Op) {
	s.t.Helper()
	res, err := s.ld.Apply(s.t.Context(), ops)
	var got []string
	for _, r := range res {
		got = append(got, string(r.Outcome)+" "+r.ID)
	}
	if err != nil || !slices.Equal(got, want) {
		s.t.Fatalf("Apply = %q, %v; want %q", got, err, want)
	}
}

// is awaits the entry and wants its mount and state; an entry that is not mounted
// has no state and Await refuses it.
func (s *suite) is(id string, m loader.Mount, st kernel.State) {
	s.t.Helper()
	got, err := s.ld.Await(s.t.Context(), id)
	if m == loader.Mounted && (err != nil || got != st) || m != loader.Mounted && !errors.Is(err, loader.ErrNotMounted) {
		s.t.Errorf("Await(%s) = %q, %v; want %q (%s)", id, got, err, st, m)
	}
	if e, ok := s.ld.Locate(id); !ok || e.Spec.ID != id || e.Mount != m || e.State != st {
		s.t.Errorf("Locate(%s) = %+v, %v; want %s and %q", id, e, ok, m, st)
	}
}

// heard wants the ids that entry-init was dispatched for, in any order: a group
// mounts its children while the plan that created them is being committed.
func (s *suite) heard(want []string) {
	s.t.Helper()
	s.h.mu.Lock()
	var got []string
	for _, in := range s.h.inits {
		got = append(got, in.Entry.ID)
	}
	s.h.mu.Unlock()
	if slices.Sort(got); !slices.Equal(got, want) {
		s.t.Errorf("entry-init heard %q, want %q", got, want)
	}
}

// heard records the three hooks. Its listener vetoes the entry "veto" by not calling
// next and the entry "nil" by returning no context, and returns what next returned
// for any other.
type heard struct {
	mu       sync.Mutex
	inits    []loader.EntryInitArgs
	volatile []loader.VolatileUpdateArgs
	built    map[string]*kernel.Context // what next returned, by entry id
}

func listen(t *testing.T, root *kernel.Root) *heard {
	t.Helper()
	h := &heard{built: map[string]*kernel.Context{}}
	l := kernel.Define(kernel.Plugin{ID: "conformance/listener",
		Hooks: []kernel.HookUse{loader.PatchContext.Use(kernel.DefaultStage), loader.EntryInit.Use(kernel.DefaultStage), loader.VolatileUpdate.Use(kernel.DefaultStage)},
		Apply: func(c *kernel.Context, _ any) error {
			loader.PatchContext.On(c, kernel.DefaultStage, func(_ context.Context, a loader.PatchContextArgs, next kernel.Next[*kernel.Context]) (*kernel.Context, error) {
				if a.Entry.ID == "veto" {
					return a.Context, nil
				}
				built, err := next()
				h.mu.Lock()
				h.built[a.Entry.ID] = built
				h.mu.Unlock()
				if a.Entry.ID == "nil" {
					return nil, err
				}
				return built, err
			})
			loader.EntryInit.On(c, kernel.DefaultStage, func(_ context.Context, a loader.EntryInitArgs) (any, error) {
				h.mu.Lock()
				defer h.mu.Unlock()
				h.inits = append(h.inits, a)
				return nil, nil
			})
			loader.VolatileUpdate.On(c, kernel.DefaultStage, func(_ context.Context, a loader.VolatileUpdateArgs) (any, error) {
				h.mu.Lock()
				defer h.mu.Unlock()
				h.volatile = append(h.volatile, a)
				return nil, nil
			})
			return nil
		}})
	if err := root.Plugin(l, nil).Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	return h
}

func row(id, plugin string) loader.EntrySpec {
	return loader.EntrySpec{ID: id, Plugin: plugin, Enabled: true, Roles: []string{role}}
}

func child(parent, id, plugin string) loader.EntrySpec {
	e := row(parent+":"+id, plugin)
	e.Parent = parent
	return e
}

func create(e loader.EntrySpec) loader.Op { return loader.Op{Kind: loader.OpCreate, Entry: e} }
func update(e loader.EntrySpec) loader.Op { return loader.Op{Kind: loader.OpUpdate, Entry: e} }
func remove(id string) loader.Op {
	return loader.Op{Kind: loader.OpRemove, Entry: loader.EntrySpec{ID: id}}
}
