// SPDX-License-Identifier: Apache-2.0

// Package fake is the recording dispatcher of the echoers definition
// (07-c4-plugin-sdk.md:494; plugin-system-2026-11.md §5): it keeps the guard of the
// definition, records every call and keeps every registered echoer readable. It
// imports the standard library, sdk/kernel and its definition only.
package fake

import (
	"context"
	"slices"
	"sync"

	echoers "github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Registry is a recording echoers.Registry.
type Registry struct {
	mu    sync.Mutex
	act   *kernel.Context        // the activation that provides the registry; nil before one
	table map[string]*registered // the registrations of that activation
	calls []Call
}

type registered struct {
	c *kernel.Context
	e echoers.Echoer
}

// Call is one recorded call of Echo.
type Call struct{ Name, Msg string }

var _ echoers.Registry = (*Registry)(nil)

// New returns an empty registry and the plugin that provides it under echoers.Key.
// Each call returns a new plugin value, a new entry of its own. Every activation of
// the plugin starts with no registration.
func New() (*Registry, kernel.Plugin) {
	f := &Registry{}
	return f, kernel.Define(kernel.Plugin{ID: "aiops/echoers-fake", Provides: []kernel.AnyKey{echoers.Key},
		Apply: func(c *kernel.Context, _ any) error {
			f.mu.Lock()
			f.act, f.table = c, map[string]*registered{}
			f.mu.Unlock()
			return echoers.Key.Provide(c, f)
		}})
}

// enter takes a lease on the providing activation and returns its registrations.
func (f *Registry) enter() (leave func(), table map[string]*registered, ok bool) {
	f.mu.Lock()
	act, table := f.act, f.table
	f.mu.Unlock()
	if act == nil {
		return nil, nil, false
	}
	leave, ok = act.Enter()
	return leave, table, ok
}

// Register adds e under name as an effect of c's fiber.
func (f *Registry) Register(c *kernel.Context, name string, e echoers.Echoer) error {
	leave, table, ok := f.enter()
	if !ok {
		return kernel.ErrInactive
	}
	defer leave()
	if name == "" {
		return echoers.ErrName
	}
	r := &registered{c: c, e: e}
	return c.Effect(echoers.Label(name), func() (kernel.Disposer, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if table[name] != nil {
			return nil, echoers.ErrName
		}
		table[name] = r
		return func(context.Context) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			if table[name] == r {
				delete(table, name)
			}
			return nil
		}, nil
	})
}

// Echo records the call and calls the echoer registered under name inside a lease on
// its fiber.
func (f *Registry) Echo(ctx context.Context, name, msg string) (string, error) {
	leave, table, ok := f.enter()
	if !ok {
		return "", kernel.ErrInactive
	}
	defer leave()
	f.mu.Lock()
	f.calls = append(f.calls, Call{Name: name, Msg: msg})
	r := table[name]
	f.mu.Unlock()
	if r == nil {
		return "", echoers.ErrUnknown
	}
	done, ok := r.c.Enter()
	if !ok {
		return "", echoers.ErrUnknown
	}
	defer done()
	return r.e.Echo(ctx, msg)
}

// Inventory lists the registered names, sorted; nothing once the registry is released.
func (f *Registry) Inventory() []string {
	leave, table, ok := f.enter()
	if !ok {
		return nil
	}
	defer leave()
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, 0, len(table))
	for name := range table {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Calls returns every call of Echo so far, in order, of every activation.
func (f *Registry) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// Contribution returns the echoer registered under name with the current activation,
// so a test can call it after its fiber is released.
func (f *Registry) Contribution(name string) (echoers.Echoer, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r := f.table[name]; r != nil {
		return r.e, true
	}
	return nil, false
}
