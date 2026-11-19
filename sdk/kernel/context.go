// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"slices"
)

// Disposer reverts one successful setup operation. It runs at most once, under
// the dispose deadline, off the owner goroutine (07-c4-plugin-sdk.md:199-200).
type Disposer func(context.Context) error

// labelToken gives a Label its identity. It is not zero-size, so two tokens are
// never equal.
type labelToken struct{ name string }

// Label names a realm of one key. Reusing one Label joins the realm; the same text
// in two NewLabel calls does not (07-c4-plugin-sdk.md:195). The zero Label is
// invalid.
type Label struct{ token *labelToken }

// NewLabel returns a new realm label. name is diagnostic only.
func NewLabel(name string) Label { return Label{token: &labelToken{name: name}} }

// frame is the declaration of one mounted fiber, shared by every context derived
// inside its activation.
type frame struct {
	id       string
	inject   []Dep
	provides []*address // The addresses the fiber may provide, in the realms of the mount.
	at       *Context   // The context the fiber was mounted from.
	host     bool       // The root frame: the launcher's own context.
}

// needs reports whether the frame declares k as a required Need.
func (f *frame) needs(k *keyData) bool {
	return slices.ContainsFunc(f.inject, func(d Dep) bool { return d.key == k && !d.optional })
}

type isolation struct {
	key   *keyData
	label *labelToken
}

type intercept struct {
	key   *keyData
	value any
}

type scopeToken struct{ _ byte }

type scopeValue struct {
	token *scopeToken
	value any // A cell[V].
}

// Context is the one mediator of the kernel: it resolves keys, records every
// registration as an effect of its fiber and carries realms, scope values and the
// intercept chain (07-c4-plugin-sdk.md:194-203, :234). It is immutable apart from
// the activation behind it: Extend, Isolate and Intercept return new nodes that
// share the activation. A fiber's context stops working when its activation ends.
type Context struct {
	parent *Context
	root   *Root
	h      *handle
	entry  entryID
	frame  *frame
	iso    *isolation
	icpts  []intercept
	scope  *scopeValue
}

func (c *Context) derive() *Context {
	return &Context{parent: c, root: c.root, h: c.h, entry: c.entry, frame: c.frame}
}

// Extend returns a child node that shares c's entry, activation and declarations
// (07-c4-plugin-sdk.md:194).
func (c *Context) Extend() *Context { return c.derive() }

// realmOf returns the realm label of k seen from c, nil for the default realm.
func (c *Context) realmOf(k *keyData) *labelToken {
	for n := c; n != nil; n = n.parent {
		if n.iso != nil && n.iso.key == k {
			return n.iso.label
		}
	}
	return nil
}

// providerAllowed reports whether c's entry may provide k.
func (c *Context) providerAllowed(k *keyData) bool {
	return c.frame.host || k.provider == classOpen || c.root.unbound || c.root.providers.allows(k, c.entry)
}

// declaresProvide reports whether c's fiber declared k in Provides for c's realm.
func (c *Context) declaresProvide(k *keyData) bool {
	return c.frame.host || slices.Contains(c.frame.provides, k.addr(c.realmOf(k)))
}

// Isolate returns a child in which k lives in its own realm, named by label or by
// a fresh one (07-c4-plugin-sdk.md:195). A provider-bound key is refused except to
// the fiber that provides it. A refusal panics: it fails the fiber, as Key.Of does
// (ADR-034 owner decision 2).
func (c *Context) Isolate(k AnyKey, label ...Label) *Context {
	d, err := dataOf(k)
	if err != nil {
		refuse(injectErr(nil, err))
	}
	if len(label) > 1 || (len(label) == 1 && label[0].token == nil) {
		refuse(injectErr(d, ErrIsolation))
	}
	if d.provider == classBound && !(c.providerAllowed(d) && c.declaresProvide(d)) {
		refuse(injectErr(d, ErrIsolation))
	}
	l := NewLabel(d.name)
	if len(label) == 1 {
		l = label[0]
	}
	n := c.derive()
	n.iso = &isolation{key: d, label: l.token}
	return n
}

// Intercept returns a child that carries cfg as one more link of k's intercept
// chain (07-c4-plugin-sdk.md:196). A cfg that is not the key's config type panics
// with *InjectError and fails the fiber.
func (c *Context) Intercept(k AnyKey, cfg any) *Context {
	d, err := dataOf(k)
	if err != nil {
		refuse(injectErr(nil, err))
	}
	if err := checkConfig(d, cfg); err != nil {
		refuse(injectErr(d, err))
	}
	n := c.derive()
	n.icpts = []intercept{{key: d, value: cfg}}
	return n
}

// consumerAllowed reports whether c's entry may resolve k.
func (c *Context) consumerAllowed(k *keyData) bool {
	return k.consumer == classOpen || c.root.unbound || c.root.consumers.allows(k, c.entry)
}

// declarer returns the nearest context, c first, whose fiber declares k as a Need
// in the realm c sees: the activation that holds the binding. It walks up the
// fiber chain and stops at the first fiber that declares k; a Want never
// qualifies.
func (c *Context) declarer(k *keyData) *Context {
	want := c.realmOf(k)
	for n := c; n != nil; n = n.parent {
		if n.frame.needs(k) && n.frame.at.realmOf(k) == want {
			return n
		}
	}
	return nil
}

// read resolves k for c: the class check, the declaration check, then the
// captured binding of the declaring activation.
func (c *Context) read(k *keyData) (*provided, error) {
	if !c.consumerAllowed(k) {
		return nil, ErrBinding
	}
	owner := c.declarer(k)
	if owner == nil {
		return nil, ErrUndeclared
	}
	v, err := owner.h.read(k.addr(c.realmOf(k)))
	if err != nil {
		if errors.Is(err, errUnknown) {
			err = errNoBinding
		}
		return nil, err
	}
	p, ok := v.(*provided)
	if !ok {
		return nil, errNoBinding
	}
	return p, nil
}

// provideKey provides k from c's fiber after the class and declaration checks.
func (c *Context) provideKey(k *keyData, p *provided) error {
	if !c.providerAllowed(k) {
		return ErrBinding
	}
	if !c.declaresProvide(k) {
		return ErrUndeclared
	}
	return c.h.provide(k.addr(c.realmOf(k)), k.name, p)
}

// ownsKey reports whether c's fiber has actually provided k: the declaration, the
// class and the core's own record of the reservation.
func (c *Context) ownsKey(k *keyData) bool {
	return c.providerAllowed(k) && c.declaresProvide(k) && c.h.owns(k.addr(c.realmOf(k)))
}

// Effect runs setup and records its disposer as an effect of c's fiber; the label
// names it in the ledger (07-c4-plugin-sdk.md:199, :371). It returns an error that
// matches ErrInactiveEffect on an unloading or disposed fiber.
func (c *Context) Effect(label string, setup func() (Disposer, error)) error {
	return c.h.effect(label, setup)
}

// Defer records d as an effect of c's fiber (07-c4-plugin-sdk.md:200). A refused
// call, such as one on an unloading fiber, panics and fails the fiber; the refusal
// matches ErrInactiveEffect.
func (c *Context) Defer(label string, d Disposer) { c.h.deferEffect(label, d) }

// Go starts run as tracked work of c's fiber: Std is cancelled and run is awaited
// at unload (07-c4-plugin-sdk.md:201, :370). A refused call panics and fails the
// fiber. run must return when Std is cancelled.
func (c *Context) Go(label string, run func(context.Context) error) { c.h.goTracked(label, run) }

// Std returns the standard context of c's activation, cancelled when its unload
// starts (07-c4-plugin-sdk.md:202).
func (c *Context) Std() context.Context { return c.h.std }

// Enter takes an in-flight lease on c's activation, as a dispatcher does around a
// call (07-c4-plugin-sdk.md:203, :373). ok is false once the fiber is quiescing;
// leave must be called exactly once otherwise. It allocates nothing.
func (c *Context) Enter() (leave func(), ok bool) { return c.h.enter() }

// Scope carries a typed value down the context tree. Anyone reads it with From;
// only the fiber that provides the owning key writes it with With
// (07-c4-plugin-sdk.md:206, :440).
type Scope[V any] struct {
	token *scopeToken
	owner *keyData
}

// NewScope returns a scope whose writer is the provider of owner. The contract
// does not name a constructor; this is the prototype's. A scope built from an
// invalid key refuses every write.
func NewScope[V any](owner AnyKey) Scope[V] {
	d, _ := dataOf(owner)
	return Scope[V]{token: &scopeToken{}, owner: d}
}

// authorized reports whether c is the live provider of the owning key.
func (s Scope[V]) authorized(c *Context) bool {
	return s.owner != nil && c.ownsKey(s.owner)
}

// With returns a child of c in which the scope has value v. It is refused with
// ErrScope unless c's fiber has provided the owning key and is still active.
func (s Scope[V]) With(c *Context, v V) (*Context, error) {
	if c == nil {
		return nil, ErrScope
	}
	// The lease keeps the provision from being withdrawn while it is checked.
	leave, ok := c.h.enter()
	if !ok {
		return nil, ErrScope
	}
	defer leave()
	if !s.authorized(c) {
		return nil, ErrScope
	}
	n := c.derive()
	n.scope = &scopeValue{token: s.token, value: cell[V]{v}}
	return n, nil
}

// From returns the nearest value of the scope above c.
func (s Scope[V]) From(c *Context) (V, bool) {
	if s.token != nil {
		for n := c; n != nil; n = n.parent {
			if n.scope != nil && n.scope.token == s.token {
				return n.scope.value.(cell[V]).value, true
			}
		}
	}
	var zero V
	return zero, false
}

// Audience is a predicate over a listener's context, built by the owner of a scope.
// Its zero value, Unscoped, matches only listeners with no scope value
// (07-c4-plugin-sdk.md:207, :440).
type Audience struct{ match func(listener *Context) bool }

// Unscoped is the audience of a dispatch that names no scope: it reaches unscoped
// listeners only (07-c4-plugin-sdk.md:440).
var Unscoped Audience

// Matches reports whether the audience admits the listener.
func (a Audience) Matches(listener *Context) bool {
	if listener == nil {
		return false
	}
	if a.match != nil {
		return a.match(listener)
	}
	for n := listener; n != nil; n = n.parent {
		if n.scope != nil {
			return false
		}
	}
	return true
}

// Audience builds the audience of the listeners whose scope value satisfies
// predicate. Only the live provider of the owning key may build it, and a listener
// of another kernel never matches (07-c4-plugin-sdk.md:207).
func (s Scope[V]) Audience(owner *Context, predicate func(V) bool) (Audience, error) {
	if predicate == nil || owner == nil {
		return Audience{}, ErrScope
	}
	leave, ok := owner.h.enter()
	if !ok {
		return Audience{}, ErrScope
	}
	defer leave()
	if !s.authorized(owner) {
		return Audience{}, ErrScope
	}
	root := owner.root
	return Audience{match: func(listener *Context) bool {
		if listener.root != root {
			return false
		}
		v, ok := s.From(listener)
		return ok && predicate(v)
	}}, nil
}
