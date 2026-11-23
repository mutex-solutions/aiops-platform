// SPDX-License-Identifier: Apache-2.0

// Package fake is the recording dispatcher of the echoers definition
// (07-c4-plugin-sdk.md:494; plugin-system-2026-11.md §5): it keeps the guard of the
// definition, records every call and keeps every registered echoer readable. It
// imports the standard library, sdk/kernel and its definition only.
package fake

import (
	"context"
	"maps"
	"slices"
	"sync"

	echoers "github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Registry is a recording echoers.Registry. Its own methods act on the current
// activation of its plugin.
type Registry struct {
	mu    sync.Mutex
	cur   *activation // the current activation; nil before one
	calls []Call
}

// activation is what one activation of the plugin provides: the methods of
// echoers.Registry over its own registrations, which return kernel.ErrInactive
// once the activation is released, whatever activation follows it.
type activation struct {
	f     *Registry
	c     *kernel.Context
	table map[string]*registered // guarded by f.mu
}

type registered struct {
	c *kernel.Context
	e echoers.Echoer
}

// Call is one recorded call of Echo.
type Call struct{ Name, Msg string }

var (
	_ echoers.Registry = (*Registry)(nil)
	_ echoers.Registry = (*activation)(nil)
)

// New returns an empty registry and the plugin that provides it under echoers.Key.
// Each call returns a new plugin value, a new entry of its own. Every activation of
// the plugin starts with no registration and provides a value of its own with the
// methods of echoers.Registry only: Calls and Contribution are the test's (no side
// door, 07-c4-plugin-sdk.md §4.3).
func New() (*Registry, kernel.Plugin) {
	f := &Registry{}
	return f, kernel.Define(kernel.Plugin{ID: "aiops/echoers-fake", Provides: []kernel.AnyKey{echoers.Key},
		Apply: func(c *kernel.Context, _ any) error {
			a := &activation{f: f, c: c, table: map[string]*registered{}}
			f.mu.Lock()
			f.cur = a
			f.mu.Unlock()
			return echoers.Key.Provide(c, a)
		}})
}

func (f *Registry) current() *activation {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cur
}

// Register adds e under name as an effect of c's fiber, with the current activation.
func (f *Registry) Register(c *kernel.Context, name string, e echoers.Echoer) error {
	if a := f.current(); a != nil {
		return a.Register(c, name, e)
	}
	return kernel.ErrInactive
}

// Echo records the call and calls the echoer registered under name with the current
// activation, inside a lease on its fiber.
func (f *Registry) Echo(ctx context.Context, name, msg string) (string, error) {
	if a := f.current(); a != nil {
		return a.Echo(ctx, name, msg)
	}
	return "", kernel.ErrInactive
}

// Inventory lists the names registered with the current activation, sorted.
func (f *Registry) Inventory() []string {
	if a := f.current(); a != nil {
		return a.Inventory()
	}
	return nil
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
	if f.cur == nil || f.cur.table[name] == nil {
		return nil, false
	}
	return f.cur.table[name].e, true
}

func (a *activation) Register(c *kernel.Context, name string, e echoers.Echoer) error {
	leave, ok := a.c.Enter()
	if !ok {
		return kernel.ErrInactive
	}
	defer leave()
	if name == "" {
		return echoers.ErrName
	}
	r := &registered{c: c, e: e}
	return c.Effect(echoers.Label(name), func() (kernel.Disposer, error) {
		a.f.mu.Lock()
		defer a.f.mu.Unlock()
		if a.table[name] != nil {
			return nil, echoers.ErrName
		}
		a.table[name] = r
		return func(context.Context) error {
			a.f.mu.Lock()
			defer a.f.mu.Unlock()
			if a.table[name] == r {
				delete(a.table, name)
			}
			return nil
		}, nil
	})
}

func (a *activation) Echo(ctx context.Context, name, msg string) (string, error) {
	leave, ok := a.c.Enter()
	if !ok {
		return "", kernel.ErrInactive
	}
	defer leave()
	a.f.mu.Lock()
	a.f.calls = append(a.f.calls, Call{Name: name, Msg: msg})
	r := a.table[name]
	a.f.mu.Unlock()
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

// Inventory lists the registered names, sorted; nothing once the activation is
// released.
func (a *activation) Inventory() []string {
	leave, ok := a.c.Enter()
	if !ok {
		return nil
	}
	defer leave()
	a.f.mu.Lock()
	defer a.f.mu.Unlock()
	return slices.Sorted(maps.Keys(a.table))
}
