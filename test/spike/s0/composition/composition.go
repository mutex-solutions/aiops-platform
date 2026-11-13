// SPDX-License-Identifier: Apache-2.0

// Package composition connects immutable contextmodel metadata to either S0
// scheduler. It is a shared test-only frontend, not a selected production SDK.
package composition

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	cc "github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
	cm "github.com/mutex-solutions/aiops-platform/test/spike/s0/contextmodel"
)

// Spec separates immutable declarations from actual activation/configuration.
// Apply's returned disposer is owned by the scheduler, including self-disposal.
type Spec struct {
	Metadata cm.Spec
	Config   *cc.Configuration
	Apply    func(*Context) (cc.Disposer, error)
}

// RootAuthority is construction authority. Never pass it to plugin code: only
// Start can attach an entry identity; Child always inherits its parent's entry.
type RootAuthority struct {
	backend         cc.Runtime
	metadata        *cm.Root
	keys            []cm.AnyKey
	mu              sync.Mutex // Construction only; Resolve and provider views never take it.
	tokens          map[cm.Label]uint64
	root            *Context
	builtinState    atomic.Pointer[builtinState]
	pendingBuiltins map[string]struct{}
}

func NewRuntime(backend cc.Runtime, spec cm.RootSpec) (*RootAuthority, error) {
	if backend == nil {
		return nil, cm.ErrContext
	}
	metadata, err := cm.NewRoot(spec)
	if err != nil {
		return nil, err
	}
	r := &RootAuthority{backend: backend, metadata: metadata, keys: slices.Clone(spec.Keys), tokens: map[cm.Label]uint64{{}: 0}}
	r.root, err = r.wrap(nil, metadata.Context(), nil)
	return r, err
}

func (r *RootAuthority) Context() *Context { return r.root }

// HostContext attaches trusted builtin metadata to the actual native host
// activation. It creates no user fiber and grants no bound-key bypass. Keep the
// RootAuthority private; plugin children can inherit but never mint this entry.
func (r *RootAuthority) HostContext(ctx context.Context, spec cm.Spec) (*Context, error) {
	if r == nil {
		return nil, cm.ErrContext
	}
	metadata, err := r.metadata.HostContext(spec)
	if err != nil {
		return nil, err
	}
	provider, ok := r.backend.(cc.RootProvider)
	if !ok {
		return nil, errors.New("native root activation unavailable")
	}
	raw, err := provider.RootContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.wrap(r.root, metadata, raw)
}

// ProvideRoot is trusted host construction, not a plugin operation. It owns the
// value in the real native root activation, without mounting a hidden user fiber.
// Only ordinary open keys are accepted; bound providers still require their
// approved entry. Root effects and values are released by Runtime.Close.
func ProvideRoot[T any](ctx context.Context, authority *RootAuthority, at *Context, key cm.Key[T], value T, available func(*Context) bool) error {
	if authority == nil || at == nil || at.authority != authority {
		return cm.ErrContext
	}
	if key.Any().ProviderClass() != cm.Open {
		return cm.ErrBinding
	}
	address, err := at.NativeAddress(key.Any())
	if err != nil {
		return err
	}
	provider, ok := authority.backend.(cc.RootProvider)
	if !ok {
		return errors.New("native root activation unavailable")
	}
	native, err := provider.RootContext(ctx)
	if err != nil {
		return err
	}
	release, err := authority.reserveProvision(address)
	if err != nil {
		return err
	}
	defer release()
	return native.Provide(address, &provision{view: func(*Context) T { return value },
		available: available, observation: value, observable: true})
}

// Context preserves the actual captured scheduler context through metadata-only
// derivation. Both its tree and its compiled key addresses are immutable.
type Context struct {
	authority *RootAuthority
	parent    *Context
	metadata  *cm.Context
	raw       *cc.Context
	addresses map[cm.AnyKey]string
}

// NativeAddress is construction/inspection metadata, not permission to perform
// an optional plugin read. Trusted adapters use it to decode actual backend
// provision observations without replacing typed authorization or epochs.
func (c *Context) NativeAddress(key cm.AnyKey) (string, error) {
	if c == nil || c.metadata == nil {
		return "", cm.ErrContext
	}
	if _, err := c.metadata.Realm(key); err != nil {
		return "", err
	}
	address, ok := c.addresses[key]
	if !ok {
		return "", cm.ErrKey
	}
	return address, nil
}

func (c *Context) Parent() *Context      { return c.parent }
func (c *Context) Metadata() *cm.Context { return c.metadata }
func (c *Context) Entry() cm.Plugin      { return c.metadata.Entry() }
func (c *Context) Config() any {
	if c.raw == nil {
		return nil
	}
	return c.raw.Config()
}
func (c *Context) Std() context.Context {
	if c.raw == nil {
		return context.Background()
	}
	return c.raw.Std()
}

func (r *RootAuthority) wrap(parent *Context, metadata *cm.Context, raw *cc.Context) (*Context, error) {
	addresses := make(map[cm.AnyKey]string, len(r.keys))
	// Opaque label identities come exclusively from contextmodel. This map only
	// serializes those identities for a string-keyed backend; it is not a realm tree.
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, key := range r.keys {
		label, err := metadata.Realm(key)
		if err != nil {
			return nil, err
		}
		token, exists := r.tokens[label]
		if !exists {
			token = uint64(len(r.tokens))
			r.tokens[label] = token
		}
		addresses[key] = fmt.Sprintf("%q@%d/realm:%d", key.Name(), key.Major(), token)
	}
	return &Context{authority: r, parent: parent, metadata: metadata, raw: raw, addresses: addresses}, nil
}

func (c *Context) derived(metadata *cm.Context) *Context {
	return &Context{authority: c.authority, parent: c, metadata: metadata, raw: c.raw, addresses: c.addresses}
}

func (c *Context) Extend() *Context { return c.derived(c.metadata.Extend()) }

// WithValues derives ordinary immutable data without assigning entry identity,
// scope-writer authority, or lifecycle ownership.
func (c *Context) WithValues(values map[string]any) *Context {
	return c.derived(c.metadata.WithValues(values))
}
func (c *Context) Intercept(key cm.AnyKey, value any) (*Context, error) {
	metadata, err := c.metadata.Intercept(key, value)
	if err != nil {
		return nil, err
	}
	return c.derived(metadata), nil
}
func (c *Context) Isolate(key cm.AnyKey, labels ...cm.Label) (*Context, error) {
	metadata, err := c.metadata.Isolate(key, labels...)
	if err != nil {
		return nil, err
	}
	return c.authority.wrap(c, metadata, c.raw)
}

// Fiber is a real scheduler handle. Await joins this target only, never Settle.
type Fiber struct {
	ID      uint64
	Context *Context
	backend cc.Runtime
}

func (f *Fiber) Dispose(ctx context.Context) error { return f.backend.Dispose(ctx, f.ID) }
func (f *Fiber) Await(ctx context.Context) error {
	waiter, ok := f.backend.(cc.Waiter)
	if !ok {
		return errors.New("scheduler does not implement target Await")
	}
	return waiter.AwaitFiber(ctx, f.ID)
}

func (r *RootAuthority) Start(ctx context.Context, parent *Context, entry cm.Plugin, spec Spec) (*Fiber, error) {
	if parent == nil || parent.authority != r {
		return nil, cm.ErrContext
	}
	metadata, err := r.metadata.WithEntry(parent.metadata, entry, spec.Metadata)
	if err != nil {
		return nil, err
	}
	template, native, err := r.compile(parent, metadata, spec)
	if err != nil {
		return nil, err
	}
	id, err := r.backend.Mount(ctx, native)
	if err != nil {
		return nil, err
	}
	return &Fiber{ID: id, Context: template, backend: r.backend}, nil
}

// Native ownership must invalidate children at parent quiescence, before waiting
// for service dependants or running provider cleanup. No idle watcher goroutine,
// fake parent service, or package-wide Settle substitutes for this relation.
func (c *Context) Child(spec Spec) (*Fiber, error) {
	if c.raw == nil {
		return nil, cc.ErrInactive
	}
	metadata, err := c.metadata.Child(spec.Metadata)
	if err != nil {
		return nil, err
	}
	template, native, err := c.authority.compile(c, metadata, spec)
	if err != nil {
		return nil, err
	}
	id, err := c.raw.MountChild(native)
	if err != nil {
		return nil, err
	}
	return &Fiber{ID: id, Context: template, backend: c.authority.backend}, nil
}

// When mounts a genuine child with required dependencies. An inherited Want does
// not gate its parent and never authorizes a read in the parent's Apply.
func (c *Context) When(id string, deps []cm.Dep, apply func(*Context) (cc.Disposer, error)) (*Fiber, error) {
	return c.Child(Spec{Metadata: cm.Spec{ID: id, Inject: deps}, Apply: apply})
}

func (r *RootAuthority) compile(parent *Context, metadata *cm.Context, spec Spec) (*Context, cc.Spec, error) {
	template, err := r.wrap(parent, metadata, nil)
	if err != nil {
		return nil, cc.Spec{}, err
	}
	native := cc.Spec{ID: metadata.Spec().ID, Config: spec.Config}
	var required []cm.AnyKey
	for _, dep := range metadata.Dependencies() {
		if err := metadata.AuthorizeRead(dep.Key(), true); err != nil {
			return nil, cc.Spec{}, err
		}
		if !dep.Optional() {
			required = append(required, dep.Key())
			native.Needs = append(native.Needs, template.addresses[dep.Key()])
		}
	}
	for _, key := range metadata.Spec().Provides {
		native.Provides = append(native.Provides, template.addresses[key])
	}
	activation := func(raw *cc.Context) *Context {
		return &Context{authority: r, parent: parent, metadata: metadata, raw: raw, addresses: template.addresses}
	}
	if len(required) != 0 {
		native.Check = func(raw *cc.Context) bool {
			caller := activation(raw)
			for _, key := range required {
				p, err := caller.read(key)
				if err != nil || (p.available != nil && !p.available(caller)) {
					return false
				}
			}
			return true
		}
	}
	native.ApplyEffect = func(raw *cc.Context) (cc.Disposer, error) {
		if spec.Apply == nil {
			return nil, nil
		}
		return spec.Apply(activation(raw))
	}
	return template, native, nil
}

type provision struct {
	view        any
	available   func(*Context) bool
	observation any
	observable  bool
}

func (c *Context) read(key cm.AnyKey) (*provision, error) {
	if err := c.metadata.AuthorizeRead(key, false); err != nil {
		return nil, err
	}
	if c.raw == nil {
		return nil, cc.ErrInactive
	}
	value, err := c.raw.Read(c.addresses[key])
	if err != nil {
		return nil, err
	}
	p, ok := value.(*provision)
	if !ok {
		return nil, cm.ErrKey
	}
	return p, nil
}

// Resolve invokes the typed view with the actual resolving wrapper after per-call
// metadata authorization and native captured-epoch Read. Availability gates native
// targets, not committed reads: teardown retains its binding until native release.
func Resolve[T any](caller *Context, key cm.Key[T]) (T, error) {
	var zero T
	if caller == nil {
		return zero, cm.ErrContext
	}
	p, err := caller.read(key.Any())
	if err != nil {
		return zero, err
	}
	view, ok := p.view.(func(*Context) T)
	if !ok {
		return zero, cm.ErrCollision
	}
	return view(caller), nil
}

func Provide[T any](owner *Context, key cm.Key[T], value T, available func(*Context) bool) error {
	if owner == nil {
		return cm.ErrContext
	}
	if err := owner.metadata.AuthorizeProvide(key.Any()); err != nil {
		return err
	}
	if owner.raw == nil {
		return cc.ErrInactive
	}
	address := owner.addresses[key.Any()]
	release, err := owner.authority.reserveProvision(address)
	if err != nil {
		return err
	}
	defer release()
	return owner.raw.Provide(address, &provision{view: func(*Context) T { return value },
		available: available, observation: value, observable: true})
}

// ProvisionObservation exposes only a directly provided immutable value for
// trusted inspection. A provider-bound view is not an observed value until a
// real consumer resolves it; this function never invokes that callback.
func ProvisionObservation(value any) (any, bool) {
	p, ok := value.(*provision)
	if !ok || !p.observable {
		return nil, false
	}
	return p.observation, true
}
func ProvideFor[T any](owner *Context, key cm.Key[T], view func(*Context) T, available func(*Context) bool) error {
	if owner == nil {
		return cm.ErrContext
	}
	if err := owner.metadata.AuthorizeProvide(key.Any()); err != nil {
		return err
	}
	if owner.raw == nil {
		return cc.ErrInactive
	}
	if view == nil {
		return cm.ErrUnavailable
	}
	address := owner.addresses[key.Any()]
	release, err := owner.authority.reserveProvision(address)
	if err != nil {
		return err
	}
	defer release()
	return owner.raw.Provide(address, &provision{view: view, available: available})
}

// Enter and Leave expose this exact native activation's call-path lease. They
// are also used by lifecycle-owned registries and hook dispatchers.
func (c *Context) Enter() bool { return c != nil && c.raw != nil && c.raw.Enter() }
func (c *Context) Leave() {
	if c != nil && c.raw != nil {
		c.raw.Leave()
	}
}

// Owns reports actual successful native reservation, not a Provides claim.
// A caller performing a privileged derived operation must hold a lease too.
func (c *Context) Owns(key cm.AnyKey) bool {
	if c == nil || c.raw == nil || c.metadata.AuthorizeProvide(key) != nil {
		return false
	}
	address, ok := c.addresses[key]
	return ok && (c.ownsBuiltin(key) || c.raw.Owns(address))
}

// Go delegates tracked work to this exact native activation; the frontend does
// not invent worker bookkeeping or completion from a canceled wait.
func (c *Context) Go(label string, run func(context.Context) error) error {
	if c == nil || c.raw == nil {
		return cm.ErrContext
	}
	return c.raw.Go(label, run)
}

func (c *Context) Defer(label string, cleanup cc.Disposer) error {
	if c.raw == nil {
		return cc.ErrInactive
	}
	return c.raw.Defer(label, cleanup)
}
func (c *Context) Dispose() error {
	if c.raw == nil {
		return cc.ErrInactive
	}
	return c.raw.Dispose()
}
func (r *RootAuthority) Notify(ctx context.Context, caller *Context, keys ...cm.AnyKey) error {
	if caller == nil || caller.authority != r {
		return cm.ErrContext
	}
	notifier, ok := r.backend.(cc.Notifier)
	if !ok {
		return errors.New("scheduler does not implement Notify")
	}
	addresses := make([]string, len(keys))
	for i, key := range keys {
		if _, err := caller.metadata.Realm(key); err != nil {
			return err
		}
		addresses[i] = caller.addresses[key]
	}
	return notifier.Notify(ctx, addresses...)
}

// Scope keeps contextmodel's single scope/realm implementation, adding native
// successful-provider ownership and a lease so a declaration alone is not proof.
type Scope[V any] struct {
	metadata cm.Scope[V]
	owner    cm.AnyKey
}

func NewScope[V any](owner cm.AnyKey) Scope[V] {
	return Scope[V]{metadata: cm.NewScope[V](owner), owner: owner}
}
func (s Scope[V]) With(owner *Context, value V) (*Context, error) {
	if owner == nil || owner.raw == nil || !owner.raw.Enter() {
		return nil, cm.ErrScope
	}
	defer owner.raw.Leave()
	if !owner.Owns(s.owner) {
		return nil, cm.ErrScope
	}
	metadata, err := s.metadata.With(owner.metadata, value)
	if err != nil {
		return nil, err
	}
	return owner.derived(metadata), nil
}
func (s Scope[V]) From(c *Context) (V, bool) {
	if c != nil {
		return s.metadata.From(c.metadata)
	}
	var zero V
	return zero, false
}
