// SPDX-License-Identifier: Apache-2.0

// Package contextmodel is a test-only, immutable context/key front end. It adapts
// the Cordis context semantics; see NOTICE for the source pin and MIT notices.
// It is not a runtime or the selected kernel. Snapshots below are explicit data:
// a later runtime must publish only ACTIVE provisions, track epochs/dependants,
// own effects, and revoke/drain them. This package does not simulate those steps.
//
// Input and returned slices/maps of metadata never expose internal containers.
// Arbitrary config, service and scope values cannot be deep-cloned generically:
// their owners must supply immutable values (clone caller-owned maps/slices first)
// and must not mutate values obtained from a context. Functions must be safe for
// concurrent calls. No callback executes under a lock; there are no global maps.
package contextmodel

import (
	"fmt"
	"reflect"
	"slices"
)

// Fault is an immutable sentinel suitable for errors.Is.
type Fault string

func (e Fault) Error() string { return string(e) }

const (
	ErrContext     Fault = "invalid or foreign context"
	ErrKey         Fault = "invalid or unregistered key"
	ErrCollision   Fault = "incompatible key definition"
	ErrEntry       Fault = "unregistered entry"
	ErrBinding     Fault = "entry is not authorized"
	ErrUndeclared  Fault = "undeclared key"
	ErrIsolation   Fault = "isolation is not authorized"
	ErrWant        Fault = "provider-bound key cannot be optional"
	ErrConfig      Fault = "invalid intercept config"
	ErrDuplicate   Fault = "duplicate provision or declaration"
	ErrUnavailable Fault = "unavailable to this consumer"
	ErrScope       Fault = "scope writer is not the owning provider"
)

// Class applies independently to a key's provider and consumer sides.
type Class uint8

const (
	Open Class = iota
	ProviderBound
	ConsumerBound
	Signed
)

type keyID struct {
	name  string
	major int
}

// AnyKey is definition data. Identity is name+major, not an allocation address.
// Equal identities must have exactly the same Go type, config type and classes.
// Distinct named interfaces are rejected even if structurally assignable.
type AnyKey struct {
	id                    keyID
	valueType, configType reflect.Type
	provider, consumer    Class
}

func (k AnyKey) Name() string         { return k.id.name }
func (k AnyKey) Major() int           { return k.id.major }
func (k AnyKey) ProviderClass() Class { return k.provider }
func (k AnyKey) ConsumerClass() Class { return k.consumer }

// Key carries the exact service type as well as its definition data.
type Key[T any] struct{ key AnyKey }

func (k Key[T]) Any() AnyKey { return k.key }

// KeyOption is immutable data, not a registration callback.
type KeyOption struct {
	provider, consumer Class
	config             reflect.Type
}

func Classes(provider, consumer Class) KeyOption {
	return KeyOption{provider: provider, consumer: consumer}
}

func Config[C any]() KeyOption { return KeyOption{config: reflect.TypeFor[C]()} }

// NewKey declares data only. NewRoot validates it and detects collisions.
// Omitting Config allows dynamically typed config; Intercepts still checks C.
func NewKey[T any](name string, major int, options ...KeyOption) Key[T] {
	k := AnyKey{id: keyID{name, major}, valueType: reflect.TypeFor[T]()}
	for _, option := range options {
		if option.config != nil {
			k.configType = option.config
		} else {
			k.provider, k.consumer = option.provider, option.consumer
		}
	}
	return Key[T]{key: k}
}

// Nonzero-size tokens matter: distinct allocations of zero-size Go values may
// compare equal. Name text is diagnostic and never confers authority.
type entryToken struct{ marker byte }

// Plugin is the sole entry value shape. Copies preserve the private identity;
// NewEntry with identical text creates a different, unprivileged identity.
type Plugin struct {
	token *entryToken
	name  string
}

func NewEntry(specName string) Plugin { return Plugin{token: &entryToken{}, name: specName} }
func (p Plugin) Name() string         { return p.name }

// Binding is trusted construction data, cloned by NewRoot. Missing lists deny
// bound access; nil never disables enforcement. For Signed, Consumers is the
// already-approved entry set, not a plugin's own claim about its signer.
type Binding struct {
	Key       AnyKey
	Providers []Plugin
	Consumers []Plugin
}

type RootSpec struct {
	Entries  []Plugin
	Keys     []AnyKey
	Bindings []Binding
}

type permissions struct {
	providers, consumers map[*entryToken]bool
}

type registry struct {
	entries  map[*entryToken]Plugin
	keys     map[keyID]AnyKey
	bindings map[keyID]permissions
}

// Root is an authority handle. Do not hand it to a child: only the owner may
// attach registered entry identities to contexts. There is no binding bypass.
type Root struct {
	context *Context
	data    *registry
}

func NewRoot(spec RootSpec) (*Root, error) {
	r := &registry{entries: make(map[*entryToken]Plugin), keys: make(map[keyID]AnyKey), bindings: make(map[keyID]permissions)}
	for _, entry := range spec.Entries {
		if entry.token == nil || entry.name == "" {
			return nil, ErrEntry
		}
		if _, exists := r.entries[entry.token]; exists {
			return nil, fmt.Errorf("%w: repeated entry", ErrEntry)
		}
		r.entries[entry.token] = entry
	}
	for _, key := range spec.Keys {
		if key.id.name == "" || key.id.major < 0 || key.valueType == nil ||
			(key.provider != Open && key.provider != ProviderBound) ||
			(key.consumer != Open && key.consumer != ConsumerBound && key.consumer != Signed) {
			return nil, ErrKey
		}
		if old, exists := r.keys[key.id]; exists && old != key {
			return nil, fmt.Errorf("%w: %s@%d", ErrCollision, key.Name(), key.Major())
		}
		r.keys[key.id] = key
	}
	for _, binding := range spec.Bindings {
		if err := r.check(binding.Key); err != nil {
			return nil, err
		}
		if _, exists := r.bindings[binding.Key.id]; exists {
			return nil, fmt.Errorf("%w: repeated binding", ErrBinding)
		}
		if (binding.Key.provider == Open && len(binding.Providers) != 0) ||
			(binding.Key.consumer == Open && len(binding.Consumers) != 0) {
			return nil, fmt.Errorf("%w: binding on open side", ErrBinding)
		}
		p := permissions{providers: make(map[*entryToken]bool), consumers: make(map[*entryToken]bool)}
		for _, list := range []struct {
			entries []Plugin
			into    map[*entryToken]bool
		}{{binding.Providers, p.providers}, {binding.Consumers, p.consumers}} {
			for _, entry := range list.entries {
				if registered, ok := r.entries[entry.token]; !ok || registered != entry {
					return nil, ErrEntry
				}
				list.into[entry.token] = true
			}
		}
		r.bindings[binding.Key.id] = p
	}
	return &Root{context: &Context{data: r}, data: r}, nil
}

func (r *registry) check(key AnyKey) error {
	if r == nil {
		return ErrContext
	}
	registered, exists := r.keys[key.id]
	if !exists {
		return ErrKey
	}
	if registered != key {
		return ErrCollision
	}
	return nil
}

func (r *Root) Context() *Context { return r.context }

// WithEntry is the only identity assignment path. Entry text in a child Spec
// has no connection to this registration. A root owner can attach under any
// of its own contexts, but cannot attach an entry or context from another root.
func (r *Root) WithEntry(parent *Context, entry Plugin, spec Spec) (*Context, error) {
	if r == nil || r.data == nil || parent == nil || parent.data != r.data {
		return nil, ErrContext
	}
	if registered, ok := r.data.entries[entry.token]; !ok || registered != entry {
		return nil, ErrEntry
	}
	return parent.child(entry, spec)
}

// Dep is a declaration, not an optional untracked read.
type Dep struct {
	key      AnyKey
	optional bool
	config   any
	with     bool
}

func Need[T any](key Key[T]) Dep { return Dep{key: key.Any()} }
func NeedWith[T, C any](key Key[T], config C) Dep {
	return Dep{key: key.Any(), config: config, with: true}
}
func Want[T any](key Key[T]) Dep  { return Dep{key: key.Any(), optional: true} }
func (d Dep) Key() AnyKey         { return d.key }
func (d Dep) Optional() bool      { return d.optional }
func (d Dep) Config() (any, bool) { return d.config, d.with }

// Spec is metadata only: no Apply callback, effect, state or fake host.
type Spec struct {
	ID       string
	Inject   []Dep
	Provides []AnyKey
}

type frame struct{ spec Spec }
type labelToken struct{ marker byte }

// Label equality is opaque value identity, like a Cordis symbol. Reusing a
// label joins one key's realm; using the same text in NewLabel does not.
type Label struct {
	token *labelToken
	name  string
}

func NewLabel(name string) Label { return Label{token: &labelToken{}, name: name} }

type intercept struct {
	key   keyID
	value any
}
type scopeToken struct{ marker byte }

// Context is an immutable parent-linked node. All maps/slices are private and
// frozen before publication. Extend preserves the declaring frame and entry;
// Child creates a frame but always inherits the registered entry, never an ID.
type Context struct {
	parent     *Context
	data       *registry
	entry      Plugin
	frame      *frame
	isolation  map[keyID]Label
	intercepts []intercept
	scopes     map[*scopeToken]any
}

func (c *Context) Parent() *Context { return c.parent }
func (c *Context) Entry() Plugin    { return c.entry }
func (c *Context) Extend() *Context {
	return &Context{parent: c, data: c.data, entry: c.entry, frame: c.frame}
}

func (c *Context) Child(spec Spec) (*Context, error) { return c.child(c.entry, spec) }

func (c *Context) child(entry Plugin, spec Spec) (*Context, error) {
	if c == nil || c.data == nil {
		return nil, ErrContext
	}
	if registered, ok := c.data.entries[entry.token]; !ok || registered != entry {
		return nil, ErrEntry
	}
	spec.Inject, spec.Provides = slices.Clone(spec.Inject), slices.Clone(spec.Provides)
	node := &Context{parent: c, data: c.data, entry: entry, frame: &frame{spec: spec}}
	seen := make(map[keyID]bool)
	for _, dep := range spec.Inject {
		if err := node.checkConsumer(dep.key); err != nil {
			return nil, err
		}
		if seen[dep.key.id] {
			return nil, ErrDuplicate
		}
		seen[dep.key.id] = true
		if dep.optional && dep.key.provider == ProviderBound {
			return nil, ErrWant
		}
		if dep.with {
			if err := checkConfig(dep.key, dep.config); err != nil {
				return nil, err
			}
			node.intercepts = append(node.intercepts, intercept{dep.key.id, dep.config})
		}
	}
	clear(seen)
	for _, key := range spec.Provides {
		if err := node.checkProvider(key); err != nil {
			return nil, err
		}
		if seen[key.id] {
			return nil, ErrDuplicate
		}
		seen[key.id] = true
	}
	return node, nil
}

// Spec returns a defensive copy of declaration containers, not arbitrary values.
func (c *Context) Spec() Spec {
	if c.frame == nil {
		return Spec{}
	}
	spec := c.frame.spec
	spec.Inject, spec.Provides = slices.Clone(spec.Inject), slices.Clone(spec.Provides)
	return spec
}

func (c *Context) checkProvider(key AnyKey) error {
	if c == nil {
		return ErrContext
	}
	if err := c.data.check(key); err != nil {
		return err
	}
	if c.entry.token == nil || (key.provider == ProviderBound && !c.data.bindings[key.id].providers[c.entry.token]) {
		return ErrBinding
	}
	return nil
}

func (c *Context) checkConsumer(key AnyKey) error {
	if c == nil {
		return ErrContext
	}
	if err := c.data.check(key); err != nil {
		return err
	}
	if c.entry.token == nil || (key.consumer != Open && !c.data.bindings[key.id].consumers[c.entry.token]) {
		return ErrBinding
	}
	return nil
}

func (c *Context) owns(key AnyKey) bool {
	return c.frame != nil && slices.Contains(c.frame.spec.Provides, key)
}

func (c *Context) realm(key keyID) Label {
	for node := c; node != nil; node = node.parent {
		if label, exists := node.isolation[key]; exists {
			return label
		}
	}
	return Label{}
}

// Isolate returns a child. A provider-bound exception requires the actual
// registered, approved provider entry and its own Provides declaration.
func (c *Context) Isolate(key AnyKey, labels ...Label) (*Context, error) {
	if c == nil {
		return nil, ErrContext
	}
	if err := c.data.check(key); err != nil {
		return nil, err
	}
	if len(labels) > 1 || (len(labels) == 1 && labels[0].token == nil) {
		return nil, ErrIsolation
	}
	if key.provider == ProviderBound && (c.checkProvider(key) != nil || !c.owns(key)) {
		return nil, ErrIsolation
	}
	label := NewLabel(key.Name())
	if len(labels) == 1 {
		label = labels[0]
	}
	node := c.Extend()
	node.isolation = map[keyID]Label{key.id: label}
	return node, nil
}

func checkConfig(key AnyKey, value any) error {
	if key.configType == nil {
		return nil
	}
	if value == nil {
		switch key.configType.Kind() {
		case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
			return nil
		}
	} else if reflect.TypeOf(value).AssignableTo(key.configType) {
		return nil
	}
	return ErrConfig
}

func (c *Context) Intercept(key AnyKey, config any) (*Context, error) {
	if c == nil {
		return nil, ErrContext
	}
	if err := c.data.check(key); err != nil {
		return nil, err
	}
	if err := checkConfig(key, config); err != nil {
		return nil, err
	}
	node := c.Extend()
	node.intercepts = []intercept{{key.id, config}}
	return node, nil
}

// Intercepts returns a new list in ancestor-first order, including NeedWith.
// A wrong C is a programming error and panics with ErrConfig (never silently
// drops an entry). Config values themselves retain their documented ownership.
func Intercepts[C any](c *Context, key AnyKey) []C {
	if c == nil {
		panic(ErrContext)
	}
	if err := c.data.check(key); err != nil {
		panic(err)
	}
	if key.configType != nil && key.configType != reflect.TypeFor[C]() {
		panic(ErrConfig)
	}
	var nodes []*Context
	for node := c; node != nil; node = node.parent {
		nodes = append(nodes, node)
	}
	var result []C
	for _, node := range slices.Backward(nodes) {
		for _, item := range node.intercepts {
			if item.key != key.id {
				continue
			}
			var value C
			if item.value != nil {
				var ok bool
				value, ok = item.value.(C)
				if !ok {
					panic(ErrConfig)
				}
			} else if checkConfig(AnyKey{configType: reflect.TypeFor[C]()}, nil) != nil {
				panic(ErrConfig)
			}
			result = append(result, value)
		}
	}
	return result
}

// Dependencies returns the effective ancestor-first declaration list for the
// current realms. Required declarations dominate Want. Config chains are read
// through Intercepts, not merged here. Consumer authorization is checked again
// at every availability/read; an inherited declaration cannot transfer authority.
func (c *Context) Dependencies() []Dep {
	var nodes []*Context
	for node := c; node != nil; node = node.parent {
		nodes = append(nodes, node)
	}
	seen := make(map[keyID]int)
	var result []Dep
	for _, node := range slices.Backward(nodes) {
		if node.frame == nil || (node.parent != nil && node.parent.frame == node.frame) {
			continue
		}
		for _, dep := range node.frame.spec.Inject {
			if node.realm(dep.key.id) != c.realm(dep.key.id) {
				continue
			}
			if index, exists := seen[dep.key.id]; exists {
				if !dep.optional {
					result[index] = dep
				}
			} else {
				seen[dep.key.id] = len(result)
				result = append(result, dep)
			}
		}
	}
	return result
}

func (c *Context) declared(key AnyKey, optional bool) bool {
	for _, dep := range c.Dependencies() {
		if dep.key.id == key.id && (optional || !dep.optional) {
			return true
		}
	}
	return false
}

// Provision is immutable preparation data, not a mounted or ACTIVE effect.
// Its functions receive the resolving consumer, never a substitute context.
type Provision struct {
	key       AnyKey
	provider  *Context
	view      func(*Context) any
	available func(*Context) bool
}

type cell[T any] struct{ value T }

func Provide[T any](c *Context, key Key[T], value T, available func(*Context) bool) (Provision, error) {
	return ProvideFor(c, key, func(*Context) T { return value }, available)
}

func ProvideFor[T any](c *Context, key Key[T], view func(*Context) T, available func(*Context) bool) (Provision, error) {
	if err := c.checkProvider(key.Any()); err != nil {
		return Provision{}, err
	}
	if !c.owns(key.Any()) {
		return Provision{}, ErrUndeclared
	}
	if view == nil {
		return Provision{}, ErrUnavailable
	}
	return Provision{key: key.Any(), provider: c, view: func(caller *Context) any { return cell[T]{view(caller)} }, available: available}, nil
}

type address struct {
	key   keyID
	label Label
}

// Snapshot is an explicit immutable provision set. Building it rejects duplicate
// name+major+realm addresses; no constructor silently overwrites a provision.
// Runtime integration must control publication/revocation; none is claimed here.
type Snapshot struct {
	data       *registry
	provisions map[address]Provision
}

func (r *Root) Snapshot(provisions ...Provision) (*Snapshot, error) {
	if r == nil || r.data == nil {
		return nil, ErrContext
	}
	snapshot := &Snapshot{data: r.data, provisions: make(map[address]Provision)}
	for _, p := range provisions {
		if p.provider == nil || p.provider.data != r.data || p.view == nil {
			return nil, ErrContext
		}
		if err := p.provider.checkProvider(p.key); err != nil {
			return nil, err
		}
		if !p.provider.owns(p.key) {
			return nil, ErrUndeclared
		}
		addr := address{p.key.id, p.provider.realm(p.key.id)}
		if _, exists := snapshot.provisions[addr]; exists {
			return nil, ErrDuplicate
		}
		snapshot.provisions[addr] = p
	}
	return snapshot, nil
}

func (s *Snapshot) lookup(c *Context, key AnyKey, optional bool) (Provision, bool, error) {
	if s == nil || c == nil || s.data == nil || c.data != s.data {
		return Provision{}, false, ErrContext
	}
	if err := c.checkConsumer(key); err != nil {
		return Provision{}, false, err
	}
	if !c.declared(key, optional) {
		return Provision{}, false, ErrUndeclared
	}
	p, exists := s.provisions[address{key.id, c.realm(key.id)}]
	return p, exists && (p.available == nil || p.available(c)), nil
}

// Available checks the predicate for this consumer on every call. A Want may
// be inspected for future child scheduling, but does not authorize Resolve.
func (s *Snapshot) Available(c *Context, key AnyKey) (bool, error) {
	_, ready, err := s.lookup(c, key, true)
	return ready, err
}

func Resolve[T any](s *Snapshot, c *Context, key Key[T]) (T, error) {
	var zero T
	p, ready, err := s.lookup(c, key.Any(), false)
	if err != nil {
		return zero, err
	}
	if !ready {
		return zero, ErrUnavailable
	}
	return p.view(c).(cell[T]).value, nil
}

// Scope has an unforgeable value-slot token and the key of its owning provider.
// With is metadata authorization by the declaring provider frame and its entry;
// the future runtime must additionally require that frame's active ownership.
type Scope[V any] struct {
	token *scopeToken
	owner AnyKey
}

func NewScope[V any](owner AnyKey) Scope[V] { return Scope[V]{token: &scopeToken{}, owner: owner} }

func (s Scope[V]) authorized(c *Context) bool {
	return s.token != nil && c != nil && c.checkProvider(s.owner) == nil && c.owns(s.owner)
}

// With freezes a new private map; it never mutates a parent or clones arbitrary V.
func (s Scope[V]) With(c *Context, value V) (*Context, error) {
	if !s.authorized(c) {
		return nil, ErrScope
	}
	node := c.Extend()
	node.scopes = map[*scopeToken]any{s.token: cell[V]{value}}
	return node, nil
}

func (s Scope[V]) From(c *Context) (V, bool) {
	for node := c; node != nil && s.token != nil; node = node.parent {
		if value, exists := node.scopes[s.token]; exists {
			return value.(cell[V]).value, true
		}
	}
	var zero V
	return zero, false
}

// Audience is explicit. Its zero value matches only contexts with no scope
// values; no global/wildcard constructor or bypass exists.
type Audience struct{ predicate func(*Context) bool }

func (a Audience) Matches(listener *Context) bool {
	if listener == nil || listener.data == nil {
		return false
	}
	if a.predicate != nil {
		return a.predicate(listener)
	}
	for node := listener; node != nil; node = node.parent {
		if len(node.scopes) != 0 {
			return false
		}
	}
	return true
}

// Audience is constructed only by the owning provider. Even a predicate that
// returns true cannot match a context without this scope or in another root.
func (s Scope[V]) Audience(owner *Context, predicate func(V) bool) (Audience, error) {
	if !s.authorized(owner) || predicate == nil {
		return Audience{}, ErrScope
	}
	return Audience{predicate: func(listener *Context) bool {
		if listener.data != owner.data {
			return false
		}
		value, exists := s.From(listener)
		return exists && predicate(value)
	}}, nil
}
