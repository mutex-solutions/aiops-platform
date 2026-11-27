// SPDX-License-Identifier: Apache-2.0

// Package dispatch is example-dispatch, the dispatcher of the two toy registries: it
// provides echoers and stampers and is the only caller of their contributions
// (plugin-system-2026-11.md §5; 07-c4-plugin-sdk.md:504). It is a toy plugin of the
// S2a test release and of no shipped bundle (ADR-060 decision 38). Each registration
// is an effect of the contributor's fiber, each call runs inside a lease on that
// fiber, and every method of a released registry returns kernel.ErrInactive
// (07-c4-plugin-sdk.md:516-519). It needs no policy key: the toy registries carry no
// tenant data and no effect (08-profiles-roadmap-adrs.md:694).
package dispatch

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"

	echoers "github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0"
	stampers "github.com/mutex-solutions/aiops-platform/sdk/def/stampers/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

// ID is the plugin id.
const ID = "aiops/example-dispatch"

// Plugin is the dispatcher. It has no config.
var Plugin = plugin.Define(plugin.Spec[struct{}]{
	ID:       ID,
	Provides: []kernel.AnyKey{echoers.Key, stampers.Key},
	Apply: func(c *kernel.Context, _ *struct{}) error {
		return errors.Join(
			echoers.Key.Provide(c, echoRegistry{newTable[echoers.Echoer](c, echoers.Label, echoers.ErrName, echoers.ErrUnknown)}),
			stampers.Key.Provide(c, stampRegistry{newTable[stampers.Stamper](c, stampers.Label, stampers.ErrName, stampers.ErrUnknown)}))
	},
})

// table holds the registrations of one registry for one activation of the dispatcher.
type table[C any] struct {
	act                 *kernel.Context
	label               func(name string) string
	errName, errUnknown error

	mu   sync.Mutex
	regs map[string]*registration[C]
}

type registration[C any] struct {
	c    *kernel.Context
	impl C
}

func newTable[C any](act *kernel.Context, label func(string) string, errName, errUnknown error) *table[C] {
	return &table[C]{act: act, label: label, errName: errName, errUnknown: errUnknown, regs: map[string]*registration[C]{}}
}

// register adds impl under name as an effect of c's fiber, which removes it when that
// fiber unloads.
func (t *table[C]) register(c *kernel.Context, name string, impl C) error {
	leave, ok := t.act.Enter()
	if !ok {
		return kernel.ErrInactive
	}
	defer leave()
	if name == "" {
		return t.errName
	}
	r := &registration[C]{c: c, impl: impl}
	return c.Effect(t.label(name), func() (kernel.Disposer, error) {
		t.mu.Lock()
		defer t.mu.Unlock()
		if t.regs[name] != nil {
			return nil, t.errName
		}
		t.regs[name] = r
		return func(context.Context) error {
			t.mu.Lock()
			defer t.mu.Unlock()
			if t.regs[name] == r {
				delete(t.regs, name)
			}
			return nil
		}, nil
	})
}

// call runs fn on the contribution registered under name, inside a lease on its fiber.
func (t *table[C]) call(name string, fn func(C) (string, error)) (string, error) {
	leave, ok := t.act.Enter()
	if !ok {
		return "", kernel.ErrInactive
	}
	defer leave()
	t.mu.Lock()
	r := t.regs[name]
	t.mu.Unlock()
	if r == nil {
		return "", t.errUnknown
	}
	done, ok := r.c.Enter()
	if !ok {
		return "", t.errUnknown
	}
	defer done()
	return fn(r.impl)
}

func (t *table[C]) names() []string {
	leave, ok := t.act.Enter()
	if !ok {
		return nil
	}
	defer leave()
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Sorted(maps.Keys(t.regs))
}

// echoRegistry and stampRegistry are the values provided: each method set is exactly
// its definition's interface (07-c4-plugin-sdk.md:516).
type (
	echoRegistry  struct{ t *table[echoers.Echoer] }
	stampRegistry struct{ t *table[stampers.Stamper] }
)

func (r echoRegistry) Register(c *kernel.Context, name string, e echoers.Echoer) error {
	return r.t.register(c, name, e)
}

func (r echoRegistry) Echo(ctx context.Context, name, msg string) (string, error) {
	return r.t.call(name, func(e echoers.Echoer) (string, error) { return e.Echo(ctx, msg) })
}

func (r echoRegistry) Inventory() []string { return r.t.names() }

func (r stampRegistry) Register(c *kernel.Context, name string, s stampers.Stamper) error {
	return r.t.register(c, name, s)
}

func (r stampRegistry) Stamp(ctx context.Context, name, body string) (string, error) {
	return r.t.call(name, func(s stampers.Stamper) (string, error) { return s.Stamp(ctx, body) })
}

func (r stampRegistry) Inventory() []string { return r.t.names() }
