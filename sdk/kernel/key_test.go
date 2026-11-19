// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

type greeter interface{ Greet() string }

type word string

func (w word) Greet() string { return string(w) }

func TestNewKeyDeclaresDataAndRejectsMalformedNames(t *testing.T) {
	for _, tc := range []struct {
		name  string
		major int
	}{{"", 0}, {"x", -1}} {
		if p := catch(func() { NewKey[int](tc.name, tc.major) }); p == nil {
			t.Errorf("NewKey(%q, %d) did not panic", tc.name, tc.major)
		}
	}
	k := NewKey[int]("n", 3, ProviderBound(), ConsumerBound(), InterceptConfig[string]())
	if k.Name() != "n" || k.Major() != 3 || k.k.provider != classBound || k.k.consumer != classBound || k.k.config == nil {
		t.Fatalf("key = %+v", k.k)
	}
	if s := NewKey[int]("s", 0, Signed()); s.k.consumer != classSigned {
		t.Fatal("Signed did not set the consumer class")
	}
	var zero Key[int]
	if zero.Name() != "" || zero.Major() != 0 {
		t.Fatal("zero key has a name")
	}
	var dep Dep
	if dep.Key() != nil || dep.Optional() {
		t.Fatal("zero Dep declares a key")
	}
	if d := Need(k); d.Key().Name() != "n" || d.Key().Major() != 3 || d.Optional() {
		t.Fatal("Need lost its key")
	} else if _, has := d.Config(); has {
		t.Fatal("Need carries a config")
	}
	if d := Want(k); !d.Optional() {
		t.Fatal("Want is not optional")
	}
	d := NeedWith(k, "cfg")
	if c, ok := d.Config(); d.Optional() || !ok || c != "cfg" {
		t.Fatal("NeedWith lost its config")
	}
}

func TestAddressesAreInternedPerRealm(t *testing.T) {
	k := NewKey[int]("a", 2)
	l1, l2 := NewLabel("x"), NewLabel("x")
	if k.k.addr(nil) != k.k.def || k.k.addr(l1.token) != k.k.addr(l1.token) || k.k.addr(l1.token) == k.k.addr(l2.token) {
		t.Fatal("addresses are not interned by realm")
	}
	if got := k.k.def.String(); got != "a@2" {
		t.Fatalf("default address = %q", got)
	}
	if got := k.k.addr(l1.token).String(); got != "a@2/x" {
		t.Fatalf("realm address = %q", got)
	}
}

func TestProvideAndOfRoundTripTypedValues(t *testing.T) {
	r := newKernel(t)
	text := NewKey[string]("text", 0)
	iface := NewKey[greeter]("greeter", 0)
	var got string
	var nilGreeter greeter = word("unset")
	p := r.Plugin(plug("p", nil, []AnyKey{text, iface}, func(c *Context) error {
		if err := text.Provide(c, "hello"); err != nil {
			return err
		}
		return iface.Provide(c, nil) // A nil interface value survives.
	}), nil)
	want(t, p, Active)
	cons := r.Plugin(plug("c", []Dep{Need(text), Need(iface)}, nil, func(c *Context) error {
		got, nilGreeter = text.Of(c), iface.Of(c)
		return nil
	}), nil)
	want(t, cons, Active)
	if got != "hello" || nilGreeter != nil {
		t.Fatalf("got %q, %v", got, nilGreeter)
	}
}

func TestOfIsRefusedWithAnInjectErrorThatFailsTheFiber(t *testing.T) {
	r := newKernel(t)
	svc := NewKey[int]("svc", 0)
	refused := func(name string, e *InjectError, reason error) {
		t.Helper()
		if !errors.Is(e, reason) {
			t.Errorf("%s: %v, want %v", name, e, reason)
		}
	}
	refused("zero key", injectFailure(t, func() { Key[int]{}.Of(r.Context()) }), ErrKey)
	refused("nil context", injectFailure(t, func() { svc.Of(nil) }), ErrUndeclared)
	refused("root declares nothing", injectFailure(t, func() { svc.Of(r.Context()) }), ErrUndeclared)

	f := r.Plugin(plug("undeclared", nil, nil, func(c *Context) error { svc.Of(c); return nil }), nil)
	err := failure(t, f)
	var ie *InjectError
	if !errors.As(err, &ie) || !errors.Is(err, ErrUndeclared) || ie.Name != "svc" || ie.Major != 0 {
		t.Fatalf("fiber error = %v", err)
	}
	if msg := ie.Error(); !strings.Contains(msg, "svc@0") {
		t.Fatalf("message = %q", msg)
	}
	// An InjectError of no key has no name.
	if msg := injectErr(nil, ErrKey).Error(); !strings.Contains(msg, "invalid key") {
		t.Fatalf("message = %q", msg)
	}

	// A Want declares a When and never allows a read, even through a child.
	w := r.Plugin(plug("wanter", []Dep{Want(svc)}, nil, func(c *Context) error { svc.Of(c.Extend()); return nil }), nil)
	if err := failure(t, w); !errors.Is(err, ErrUndeclared) {
		t.Fatalf("Want read: %v", err)
	}
	// A context that outlives its activation is refused, not served.
	var saved *Context
	prov := r.Plugin(plug("prov", nil, []AnyKey{svc}, func(c *Context) error { return svc.Provide(c, 1) }), nil)
	want(t, prov, Active)
	cons := r.Plugin(plug("cons", []Dep{Need(svc)}, nil, func(c *Context) error { saved = c; return nil }), nil)
	want(t, cons, Active)
	if err := cons.Dispose(frontCtx(t)); err != nil {
		t.Fatal(err)
	}
	want(t, cons, Disposed)
	refused("stale context", injectFailure(t, func() { svc.Of(saved) }), ErrInactive)
}

func TestProvideIsRefused(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	svc := NewKey[int]("svc", 0)
	other := NewKey[int]("other", 0)
	check := func(name string, got error, reason error) {
		t.Helper()
		var ie *InjectError
		if !errors.As(got, &ie) || !errors.Is(got, reason) {
			t.Errorf("%s: %v, want an InjectError for %v", name, got, reason)
		}
	}
	check("zero key", Key[int]{}.Provide(r.Context(), 1), ErrKey)
	check("zero key, view", Key[int]{}.ProvideFor(r.Context(), func(*Context) int { return 1 }), ErrKey)
	check("nil context", svc.Provide(nil, 1), ErrUndeclared)
	check("nil view", svc.ProvideFor(r.Context(), nil), errNilView)

	var results []error
	f := r.Plugin(plug("p", nil, []AnyKey{svc}, func(c *Context) error {
		results = append(results, other.Provide(c, 1)) // Not in Provides.
		if err := svc.Provide(c, 1); err != nil {
			return err
		}
		results = append(results, svc.Provide(c, 2)) // A second provider in one realm.
		return nil
	}), nil)
	want(t, f, Active)
	check("undeclared", results[0], ErrUndeclared)
	if results[1] == nil || !strings.Contains(results[1].Error(), "duplicate provision") {
		t.Fatalf("second provision: %v", results[1])
	}

	// A second fiber that provides the same key in the same realm.
	g := r.Plugin(plug("q", nil, []AnyKey{svc}, func(c *Context) error { return svc.Provide(c, 3) }), nil)
	if err := failure(t, g); !strings.Contains(err.Error(), "duplicate provision") {
		t.Fatalf("two providers of one key: %v", err)
	}

	// After unload the provision is refused as inactive.
	var saved *Context
	later := NewKey[int]("later", 0)
	h := r.Plugin(plug("h", nil, []AnyKey{later}, func(c *Context) error { saved = c; return nil }), nil)
	want(t, h, Active)
	if err := h.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	want(t, h, Disposed)
	check("inactive", later.Provide(saved, 1), ErrInactive)
}

func TestProviderBoundKeyNeedsAListedEntry(t *testing.T) {
	bound := NewKey[int]("bound", 0, ProviderBound())
	listed := plug("listed", nil, []AnyKey{bound}, func(c *Context) error { return bound.Provide(c, 7) })
	stranger := plug("stranger", nil, []AnyKey{bound}, nil)
	r := newBound(t, Bindings{bound: {listed}}, nil)
	want(t, r.Plugin(listed, nil), Active)
	// The stranger is refused at mount.
	if err := failure(t, r.Plugin(stranger, nil)); !errors.Is(err, ErrBinding) {
		t.Fatalf("stranger: %v", err)
	}
	// A child that names itself like the listed plugin but is not the value gains nothing.
	forger := Plugin{ID: listed.ID, Provides: []AnyKey{bound}, Apply: func(c *Context, _ any) error { return nil }}
	if err := failure(t, r.Plugin(forger, nil)); !errors.Is(err, ErrBinding) {
		t.Fatalf("forger: %v", err)
	}
	// Provide itself checks too, for a context that bypassed the mount check.
	bypass := r.Plugin(plug("bypass", nil, nil, func(c *Context) error {
		c.frame.provides = append(c.frame.provides, bound.k.def)
		return bound.Provide(c, 1)
	}), nil)
	if err := failure(t, bypass); !errors.Is(err, ErrBinding) {
		t.Fatalf("bypass: %v", err)
	}
	// The launcher's own context may provide a provider-bound key, as the launch keys are.
	if err := bound.Provide(r.Context(), 9); err == nil {
		t.Fatal("two providers of a bound key were accepted")
	}
}

func TestHostContextProvidesBoundKeys(t *testing.T) {
	launch := NewKey[string]("launch", 0, ProviderBound())
	r := newBound(t, nil, nil)
	if err := launch.Provide(r.Context(), "ready"); err != nil {
		t.Fatal(err)
	}
	var got string
	c := r.Plugin(plug("c", []Dep{Need(launch)}, nil, func(c *Context) error { got = launch.Of(c); return nil }), nil)
	want(t, c, Active)
	if got != "ready" {
		t.Fatalf("got %q", got)
	}
}

func TestProvideForBindsTheViewToTheCaller(t *testing.T) {
	r := newKernel(t)
	svc := NewKey[string]("svc", 0, InterceptConfig[int]())
	scoped := NewScope[string](svc)
	var views atomic.Int32
	prov := r.Plugin(plug("prov", nil, []AnyKey{svc}, func(c *Context) error {
		return svc.ProvideFor(c, func(caller *Context) string {
			views.Add(1)
			who, _ := scoped.From(caller)
			return fmt.Sprint(caller.frame.id, "/", who, "/", Intercepts[int](caller, svc))
		})
	}), nil)
	want(t, prov, Active)
	results := make(chan string, 2)
	reader := func(id string, deps ...Dep) Plugin {
		return plug(id, append([]Dep{Need(svc)}, deps...), nil, func(c *Context) error {
			results <- svc.Of(c)
			return nil
		})
	}
	want(t, r.Plugin(reader("one"), nil), Active)
	want(t, r.Plugin(reader("two", NeedWith(Key[int]{}, 0)), nil), Failed)
	want(t, r.Plugin(reader("three"), nil), Active)
	if a, b := <-results, <-results; a != "one//[]" || b != "three//[]" {
		t.Fatalf("views = %q, %q", a, b)
	}
	if views.Load() != 2 {
		t.Fatalf("view ran %d times", views.Load())
	}
}

func TestAvailableGatesActivationPerConsumer(t *testing.T) {
	r := newKernel(t)
	svc := NewKey[int]("svc", 0)
	var open atomic.Bool
	var checks atomic.Int32
	prov := r.Plugin(plug("prov", nil, []AnyKey{svc}, func(c *Context) error {
		return svc.Provide(c, 1, Available(func(caller *Context) bool {
			checks.Add(1)
			return open.Load() && caller.frame.id == "chosen"
		}))
	}), nil)
	want(t, prov, Active)
	chosen := r.Plugin(plug("chosen", []Dep{Need(svc)}, nil, func(c *Context) error { svc.Of(c); return nil }), nil)
	skipped := r.Plugin(plug("skipped", []Dep{Need(svc)}, nil, nil), nil)
	want(t, chosen, Pending)
	want(t, skipped, Pending)
	open.Store(true)
	notify(t, r, svc)
	want(t, chosen, Active)
	want(t, skipped, Pending)
	if checks.Load() == 0 {
		t.Fatal("the predicate never ran")
	}
}

func TestInterceptsAreAncestorFirstAndTyped(t *testing.T) {
	r := newKernel(t)
	typed := NewKey[int]("typed", 0, InterceptConfig[string]())
	dynamic := NewKey[int]("dynamic", 0)
	nilable := NewKey[int]("nilable", 0, InterceptConfig[*int]())
	plain := NewKey[int]("plain", 0, InterceptConfig[int]())
	want(t, r.Plugin(plug("typed-provider", nil, []AnyKey{typed}, func(c *Context) error { return typed.Provide(c, 1) }), nil), Active)
	var chain []string
	var dyn []any
	var ptrs []*int
	f := r.Plugin(plug("p", []Dep{NeedWith(typed, "need")}, nil, func(c *Context) error {
		leaf := c.Intercept(typed, "one").Extend().Intercept(typed, "two").Intercept(dynamic, 5).Intercept(dynamic, nil).Intercept(nilable, nil)
		chain = Intercepts[string](leaf, typed)
		dyn = Intercepts[any](leaf, dynamic)
		ptrs = Intercepts[*int](leaf, nilable)
		return nil
	}), nil)
	want(t, f, Active)
	if fmt.Sprint(chain) != "[need one two]" || fmt.Sprint(dyn) != "[5 <nil>]" || len(ptrs) != 1 || ptrs[0] != nil {
		t.Fatalf("chain = %v, dyn = %v, ptrs = %v", chain, dyn, ptrs)
	}

	c := r.Context()
	for name, fn := range map[string]func(){
		"zero key":         func() { Intercepts[string](c, Key[int]{}) },
		"nil key":          func() { Intercepts[string](c, nil) },
		"nil caller":       func() { Intercepts[string](nil, typed) },
		"wrong C":          func() { Intercepts[int](c, typed) },
		"item is not a C":  func() { Intercepts[string](c.Intercept(dynamic, 5), dynamic) },
		"nil is not a C":   func() { Intercepts[int](c.Intercept(dynamic, nil), dynamic) },
		"Intercept bad":    func() { c.Intercept(typed, 5) },
		"Intercept nil":    func() { c.Intercept(plain, nil) },
		"Intercept zero":   func() { c.Intercept(Key[int]{}, 5) },
		"Intercept nilkey": func() { c.Intercept(nil, 5) },
	} {
		if e := injectFailure(t, fn); e == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestBindingMapsAreCheckedAtConstruction(t *testing.T) {
	// A binding on an open side, a key twice and an entry with no Apply are each named.
	open := NewKey[int]("open", 0)
	_, err := compileBindings("provider", Bindings{open: {plug("a", nil, nil, nil)}}, func(k *keyData) bool { return k.provider == classBound })
	if !errors.Is(err, ErrBinding) {
		t.Fatalf("open key: %v", err)
	}
	if _, err := compileBindings("provider", Bindings{NewKey[int]("pb", 0, ProviderBound()): {Plugin{ID: "x"}}}, func(*keyData) bool { return true }); !errors.Is(err, ErrBinding) {
		t.Fatalf("an undefined entry: %v", err)
	}
	a := plug("a", nil, nil, nil)
	b := a // A copy is the same entry.
	set := bindingSet{open.k: {a.entry: {}}}
	if !set.allows(open.k, b.entry) || set.allows(open.k, nil) || set.allows(open.k, plug("z", nil, nil, nil).entry) || set.allows(NewKey[int]("o", 0).k, a.entry) {
		t.Fatal("allows is wrong")
	}
}
