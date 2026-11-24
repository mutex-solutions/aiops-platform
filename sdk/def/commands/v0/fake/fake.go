// SPDX-License-Identifier: Apache-2.0

// Package fake is the recording dispatcher of the commands definition
// (07-c4-plugin-sdk.md:494; ADR-061 decision 4): it dispatches by the rules of
// commands.Table, as the cli row does, records each registration and each dispatch,
// and exposes Inventory. A command row mounts alone with it. It imports the standard
// library, sdk/kernel and its definition only.
package fake

import (
	"context"
	"slices"
	"sync"

	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Registry is a recording commands.Registry. Set the exported fields before the plugin
// is mounted: each activation gives its invocations Dir, Env and Home, and names the
// rows of Pending in help and in the answer to a path that no row registered. Its own
// methods act on the current activation.
type Registry struct {
	Dir     string
	Env     map[string]string
	Home    string
	Pending []commands.Pending

	mu    sync.Mutex
	cur   *commands.Table
	specs map[string]commands.Spec // what each registration that succeeded registered, by name
	calls [][]string
}

var _ commands.Registry = (*Registry)(nil)

// New returns an empty registry and the plugin that provides it under commands.Key.
// Each call returns a new plugin value. Every activation starts with no registration
// and provides a value with the methods of commands.Registry only: Calls and Spec are
// the test's (no side door, 07-c4-plugin-sdk.md §4.3).
func New() (*Registry, kernel.Plugin) {
	f := &Registry{specs: map[string]commands.Spec{}}
	return f, kernel.Define(kernel.Plugin{ID: "aiops/commands-fake", Provides: []kernel.AnyKey{commands.Key},
		Apply: func(c *kernel.Context, _ any) error {
			env := commands.Environment{Dir: f.Dir, Home: f.Home,
				Env:     func(name string) (string, bool) { v, ok := f.Env[name]; return v, ok },
				Pending: func() []commands.Pending { return slices.Clone(f.Pending) }}
			t := commands.NewTable(c, env)
			f.mu.Lock()
			f.cur = t
			f.mu.Unlock()
			return commands.Key.Provide(c, struct{ commands.Registry }{recording{f, t}})
		}})
}

// recording is what an activation provides: its table, with each call recorded.
type recording struct {
	f *Registry
	t *commands.Table
}

func (r recording) Register(c *kernel.Context, s commands.Spec, cmd commands.Command) error {
	err := r.t.Register(c, s, cmd)
	if err == nil {
		r.f.mu.Lock()
		r.f.specs[s.Name()] = s
		r.f.mu.Unlock()
	}
	return err
}

func (r recording) Dispatch(ctx context.Context, argv []string, s commands.Streams) (int, error) {
	r.f.mu.Lock()
	r.f.calls = append(r.f.calls, slices.Clone(argv))
	r.f.mu.Unlock()
	return r.t.Dispatch(ctx, argv, s)
}

func (r recording) Inventory() []string { return r.t.Inventory() }

func (f *Registry) current() commands.Registry {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cur == nil {
		return nil
	}
	return recording{f, f.cur}
}

// Register registers with the current activation.
func (f *Registry) Register(c *kernel.Context, s commands.Spec, cmd commands.Command) error {
	if r := f.current(); r != nil {
		return r.Register(c, s, cmd)
	}
	return kernel.ErrInactive
}

// Dispatch records argv and dispatches it with the current activation.
func (f *Registry) Dispatch(ctx context.Context, argv []string, s commands.Streams) (int, error) {
	if r := f.current(); r != nil {
		return r.Dispatch(ctx, argv, s)
	}
	return 1, kernel.ErrInactive
}

// Inventory lists the paths registered with the current activation, sorted.
func (f *Registry) Inventory() []string {
	if r := f.current(); r != nil {
		return r.Inventory()
	}
	return nil
}

// Calls returns the argv of every dispatch so far, in order, of every activation.
func (f *Registry) Calls() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// Spec returns the spec last registered under name, the path as one string.
func (f *Registry) Spec(name string) (commands.Spec, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.specs[name]
	return s, ok
}
