// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
)

// The reasons a key operation is refused. A *InjectError wraps exactly one of
// them, or a lifecycle error of the core (ErrInactive), so errors.Is names the
// reason. The contract names no reason; these are the prototype's.
var (
	// ErrKey reports a zero or malformed key.
	ErrKey = errors.New("invalid key")
	// ErrBinding reports an entry that the binding map does not list for the key
	// (07-c4-plugin-sdk.md:247, :555-558).
	ErrBinding = errors.New("entry is not authorized by the binding map")
	// ErrUndeclared reports a key the context does not declare as a Need
	// (07-c4-plugin-sdk.md:180, :249) or, for a provision, as Provides (:218).
	ErrUndeclared = errors.New("undeclared key")
	// ErrWant reports a Want on a provider-bound key (07-c4-plugin-sdk.md:555).
	ErrWant = errors.New("provider-bound key cannot be optional")
	// ErrIsolation reports an Isolate that is not allowed (07-c4-plugin-sdk.md:195).
	ErrIsolation = errors.New("isolation is not authorized")
	// ErrConfig reports an intercept config of the wrong type (07-c4-plugin-sdk.md:190).
	ErrConfig = errors.New("invalid intercept config")
	// ErrDuplicate reports one key declared twice in one declaration list.
	ErrDuplicate = errors.New("duplicate declaration")
	// ErrScope reports a scope write or audience from a fiber that does not
	// provide the owning key (07-c4-plugin-sdk.md:206).
	ErrScope = errors.New("scope writer is not the owning provider")

	errNilView   = errors.New("provision view must not be nil")
	errNoBinding = errors.New("no provision is bound to this activation")
)

// InjectError is the refusal of a key operation. Key.Of panics with it
// (07-c4-plugin-sdk.md:180) and the fiber boundary recovers it; Provide and
// ProvideFor return it. It unwraps to the reason.
type InjectError struct {
	Name  string // The key's name.
	Major int    // The key's major.
	Err   error  // The reason.
}

func (e *InjectError) Error() string {
	return fmt.Sprintf("key %s@%d: %v", e.Name, e.Major, e.Err)
}

func (e *InjectError) Unwrap() error { return e.Err }

func injectErr(k *keyData, err error) *InjectError {
	if k == nil {
		return &InjectError{Err: err}
	}
	return &InjectError{Name: k.name, Major: k.major, Err: err}
}

// class is the provider or consumer class of a key (07-c4-plugin-sdk.md:553-559).
type class uint8

const (
	classOpen class = iota
	classBound
	classSigned // Consumer side only.
)

// keyData is the identity of one declared key. Two keys are the same key exactly
// when they share one keyData.
type keyData struct {
	name               string
	major              int
	config             reflect.Type // The intercept config type, nil for any.
	provider, consumer class
	def                *address // The default-realm address.
	realms             sync.Map // *labelToken -> *address
}

// address is one key in one realm: the service key the core sees. It is interned,
// so equal addresses are the same pointer.
type address struct {
	key   *keyData
	realm *labelToken
}

func (a *address) String() string {
	if a.realm == nil {
		return fmt.Sprintf("%s@%d", a.key.name, a.key.major)
	}
	return fmt.Sprintf("%s@%d/%s", a.key.name, a.key.major, a.realm.name)
}

// addr returns the address of k in realm, nil for the default realm. It takes no
// lock for the default realm.
func (k *keyData) addr(realm *labelToken) *address {
	if realm == nil {
		return k.def
	}
	if a, ok := k.realms.Load(realm); ok {
		return a.(*address)
	}
	a, _ := k.realms.LoadOrStore(realm, &address{key: k, realm: realm})
	return a.(*address)
}

// AnyKey is a key of any service type: the argument of Isolate, Intercept and
// Intercepts and the element of Plugin.Provides (07-c4-plugin-sdk.md:190, :195,
// :218). Every Key[T] is an AnyKey.
type AnyKey interface {
	Name() string
	Major() int
	anyKey() *keyData
}

func (k *keyData) Name() string     { return k.name }
func (k *keyData) Major() int       { return k.major }
func (k *keyData) anyKey() *keyData { return k }

// dataOf returns the key behind k, or ErrKey for nil or a zero key.
func dataOf(k AnyKey) (*keyData, error) {
	if k == nil {
		return nil, ErrKey
	}
	d := k.anyKey()
	if d == nil {
		return nil, ErrKey
	}
	return d, nil
}

// Key names one service of type T with its major, its provider class and its
// consumer class (07-c4-plugin-sdk.md:177). The zero Key is invalid and every
// operation on it is refused.
type Key[T any] struct{ k *keyData }

// KeyOption is a declaration option of NewKey (07-c4-plugin-sdk.md:179).
type KeyOption func(*keyData)

// ProviderBound marks the key provider-bound: Provide succeeds only for an entry
// that the provider binding map lists (07-c4-plugin-sdk.md:266, :555).
func ProviderBound() KeyOption { return func(k *keyData) { k.provider = classBound } }

// ConsumerBound marks the key consumer-bound: it resolves only for an entry that
// the consumer binding map lists (07-c4-plugin-sdk.md:556).
func ConsumerBound() KeyOption { return func(k *keyData) { k.consumer = classBound } }

// Signed marks the key signed: the consumer binding map lists the approved
// entries, never a plugin's own claim (07-c4-plugin-sdk.md:557).
func Signed() KeyOption { return func(k *keyData) { k.consumer = classSigned } }

// InterceptConfig declares the type of the per-consumer intercept config of the key, as
// the "intercept config type" of the Key (07-c4-plugin-sdk.md:177). Without it the
// config is dynamically typed.
func InterceptConfig[C any]() KeyOption { return func(k *keyData) { k.config = reflect.TypeFor[C]() } }

// NewKey declares a key (07-c4-plugin-sdk.md:179). It is called in a definition
// package. An empty name or a negative major is a programming error and panics.
func NewKey[T any](name string, major int, opts ...KeyOption) Key[T] {
	if name == "" || major < 0 {
		panic(fmt.Sprintf("kernel.NewKey: invalid name %q or major %d", name, major))
	}
	k := &keyData{name: name, major: major}
	k.def = &address{key: k}
	for _, o := range opts {
		o(k)
	}
	return Key[T]{k: k}
}

// Name returns the key's name.
func (k Key[T]) Name() string {
	if k.k == nil {
		return ""
	}
	return k.k.name
}

// Major returns the key's major.
func (k Key[T]) Major() int {
	if k.k == nil {
		return 0
	}
	return k.k.major
}

func (k Key[T]) anyKey() *keyData { return k.k }

// cell wraps a service value so that a nil interface value survives the any.
type cell[T any] struct{ value T }

// provided is the value the core stores for one provision.
type provided struct {
	view      func(caller *Context) any // Returns a cell[T].
	available func(caller *Context) bool
	value     any // The value of a plain Provide, for the launcher's read API; nil for a view.
}

// ProvideOption is an option of Provide and ProvideFor (07-c4-plugin-sdk.md:181).
type ProvideOption func(*provided)

// Available makes the provision available only to the consumers for which fn
// returns true. It is evaluated off the owner, for each consumer, whenever the
// consumer's availability is checked (07-c4-plugin-sdk.md:181, :349; ADR-034
// owner decision 8: it takes this one argument).
func Available(fn func(caller *Context) bool) ProvideOption {
	return func(p *provided) { p.available = fn }
}

// Of returns the bound provision of k, or the view bound to c when the provider
// used ProvideFor (07-c4-plugin-sdk.md:180, :255). It needs a Need on k declared by
// c's fiber or an ancestor in the same realm, or a When on k; otherwise it panics
// with *InjectError, which the fiber boundary recovers and which fails the fiber.
func (k Key[T]) Of(c *Context) T {
	d, err := dataOf(k)
	if err != nil {
		panic(injectErr(nil, err))
	}
	if c == nil {
		panic(injectErr(d, ErrUndeclared))
	}
	v, err := c.read(d)
	if err != nil {
		panic(injectErr(d, err))
	}
	return v.view(c).(cell[T]).value
}

// Provide provides v as a service of c's fiber, as an effect
// (07-c4-plugin-sdk.md:181, :366). The fiber must declare k in Provides and, for a
// provider-bound key, be listed by the provider binding map. It returns a
// *InjectError otherwise, and when the fiber is inactive or already provides k; an inactive fiber is refused with ErrInactive before
// any other refusal.
func (k Key[T]) Provide(c *Context, v T, o ...ProvideOption) error {
	return k.provide(c, func(*Context) any { return cell[T]{v} }, v, o)
}

// ProvideFor provides a view bound to each consumer: view receives the resolving
// context and reads the caller's entry, scope values and intercept chain from it
// (07-c4-plugin-sdk.md:183-185). Refusals are those of Provide; a nil view is
// refused.
func (k Key[T]) ProvideFor(c *Context, view func(caller *Context) T, o ...ProvideOption) error {
	var v func(caller *Context) any
	if view != nil {
		v = func(caller *Context) any { return cell[T]{view(caller)} }
	}
	return k.provide(c, v, nil, o)
}

func (k Key[T]) provide(c *Context, view func(*Context) any, value any, o []ProvideOption) error {
	// Liveness first, for every refusal; handle.provide decides it again atomically.
	if c.ended() {
		return injectErr(k.k, ErrInactive)
	}
	if view == nil {
		return injectErr(k.k, errNilView)
	}
	d, err := dataOf(k)
	if err != nil {
		return injectErr(nil, err)
	}
	if c == nil {
		return injectErr(d, ErrUndeclared)
	}
	p := &provided{view: view, value: value}
	for _, opt := range o {
		opt(p)
	}
	if err := c.provideKey(d, p); err != nil {
		return injectErr(d, err)
	}
	return nil
}

// Dep is a declared need: data in Plugin.Inject (07-c4-plugin-sdk.md:187-189,
// :234).
type Dep struct {
	key      *keyData
	optional bool
	config   any
	hasCfg   bool
}

// Need declares k as required: it gates activation and joins the epoch
// (07-c4-plugin-sdk.md:187).
func Need[T any](k Key[T]) Dep { return Dep{key: k.k} }

// NeedWith is Need plus the per-consumer intercept config cfg, which joins the
// intercept chain of the fiber's context (07-c4-plugin-sdk.md:188).
func NeedWith[T, C any](k Key[T], cfg C) Dep { return Dep{key: k.k, config: cfg, hasCfg: true} }

// Want declares a When on k. It never gates the plugin itself and does not allow a
// read of k; a Want on a provider-bound key is refused (07-c4-plugin-sdk.md:189,
// :555).
func Want[T any](k Key[T]) Dep { return Dep{key: k.k, optional: true} }

// Key returns the declared key, nil for a zero Dep.
func (d Dep) Key() AnyKey {
	if d.key == nil {
		return nil
	}
	return d.key
}

// Optional reports whether the Dep is a Want.
func (d Dep) Optional() bool { return d.optional }

// Config returns the NeedWith config.
func (d Dep) Config() (any, bool) { return d.config, d.hasCfg }

// checkConfig reports whether value can be the intercept config of k.
func checkConfig(k *keyData, value any) error {
	if k.config == nil {
		return nil
	}
	if value == nil {
		if nilable(k.config) {
			return nil
		}
		return ErrConfig
	}
	if reflect.TypeOf(value).AssignableTo(k.config) {
		return nil
	}
	return ErrConfig
}

// nilable reports whether the nil interface value converts to t.
func nilable(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return true
	}
	return false
}

// Intercepts returns the merged intercept chain of k seen from caller, ancestors
// first, including NeedWith configs (07-c4-plugin-sdk.md:190). It is read inside a
// ProvideFor view. A C that is not the key's config type panics with *InjectError.
func Intercepts[C any](caller *Context, k AnyKey) []C {
	d, err := dataOf(k)
	if err != nil {
		panic(injectErr(nil, err))
	}
	if caller == nil || (d.config != nil && d.config != reflect.TypeFor[C]()) {
		panic(injectErr(d, ErrConfig))
	}
	var chain []*Context
	for n := caller; n != nil; n = n.parent {
		if len(n.icpts) != 0 {
			chain = append(chain, n)
		}
	}
	var out []C
	for i := len(chain) - 1; i >= 0; i-- {
		for _, it := range chain[i].icpts {
			if it.key != d {
				continue
			}
			var v C
			if it.value != nil {
				var ok bool
				if v, ok = it.value.(C); !ok {
					panic(injectErr(d, ErrConfig))
				}
			} else if !nilable(reflect.TypeFor[C]()) {
				panic(injectErr(d, ErrConfig))
			}
			out = append(out, v)
		}
	}
	return out
}

// entryToken is the identity of one entry, minted by Define (ADR-034 owner
// decision 7). It is never zero-size, so two tokens are never equal.
type entryToken struct{ _ byte }

// entryID identifies one registered Plugin value, "never the id a plugin declares
// about itself" (07-c4-plugin-sdk.md:563). nil is no entry of one's own.
type entryID = *entryToken

// Define returns a copy of p stamped with a fresh entry token: the Plugin value the
// module table holds (ADR-034 owner decision 7; 07-c4-plugin-sdk.md:236, :563). A
// binding map names the entry by this token, so a copy of the result is the same
// entry and a hand-built Plugin, even one with the same ID or the same Apply, is
// not. Defining a value that is already defined mints a new token: the result is a
// new entry and the old one keeps its own. A Plugin that was never defined has no
// entry of its own: mounted with Context.Plugin it inherits the entry of the
// mounting context, and listed in a binding map it is refused by New.
func Define(p Plugin) Plugin {
	p.entry = new(entryToken)
	return p
}

// Bindings lists, per key, the Plugin values the module table allows
// (07-c4-plugin-sdk.md:551). A key with no list admits no entry; a nil map never
// disables enforcement.
type Bindings map[AnyKey][]Plugin

// bindingSet is the checked form of one binding map.
type bindingSet map[*keyData]map[entryID]struct{}

// compileBindings checks b against the class that bound selects and returns the
// set. It reports a nil or zero key, a key whose class is open on that side, a key
// listed twice and an entry with no identity.
func compileBindings(side string, b Bindings, bound func(*keyData) bool) (bindingSet, error) {
	set := make(bindingSet, len(b))
	var errs []error
	for key, entries := range b {
		d, err := dataOf(key)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s binding: %w", side, err))
			continue
		}
		if !bound(d) {
			errs = append(errs, fmt.Errorf("%s binding for open key %s@%d: %w", side, d.name, d.major, ErrBinding))
			continue
		}
		if _, dup := set[d]; dup {
			errs = append(errs, fmt.Errorf("%s binding lists key %s@%d twice: %w", side, d.name, d.major, ErrDuplicate))
			continue
		}
		ids := make(map[entryID]struct{}, len(entries))
		for _, p := range entries {
			id := p.entry
			if id == nil {
				errs = append(errs, fmt.Errorf("%s binding for %s@%d lists plugin %q that was never defined: %w", side, d.name, d.major, p.ID, ErrBinding))
				continue
			}
			ids[id] = struct{}{}
		}
		set[d] = ids
	}
	return set, errors.Join(errs...)
}

func (s bindingSet) allows(k *keyData, e entryID) bool {
	_, ok := s[k][e]
	return e != nil && ok
}
