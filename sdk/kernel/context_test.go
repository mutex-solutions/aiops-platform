// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
)

func TestIsolateGivesEachRealmItsOwnProvider(t *testing.T) {
	r := newKernel(t)
	svc := NewKey[int]("svc", 0)
	provider := func(id string, v int) Plugin {
		return plug(id, nil, []AnyKey{svc}, func(c *Context) error { return svc.Provide(c, v) })
	}
	results := make(chan int, 4)
	reader := plug("reader", []Dep{Need(svc)}, nil, func(c *Context) error { results <- svc.Of(c); return nil })

	shared := NewLabel("shared")
	own := r.Context().Isolate(svc)
	joinedA, joinedB := r.Context().Isolate(svc, shared), r.Context().Extend().Isolate(svc, shared)

	want(t, r.Plugin(provider("default", 1), nil), Active)
	want(t, own.Plugin(provider("own", 2), nil), Active)
	want(t, joinedA.Plugin(provider("shared", 3), nil), Active)
	// The same label joins the realm: a second provider there is a duplicate.
	if err := failure(t, joinedB.Plugin(provider("shared-twin", 4), nil)); err == nil {
		t.Fatal("two providers in one realm")
	}
	for _, from := range []*Context{r.Context(), own, joinedB} {
		want(t, from.Plugin(reader, nil), Active)
	}
	if a, b, c := <-results, <-results, <-results; a != 1 || b != 2 || c != 3 {
		t.Fatalf("readers saw %d %d %d, want 1 2 3", a, b, c)
	}
	// A provision made in a realm is not declared for another: the fiber's Provides
	// are those of its own mount.
	bad := r.Plugin(plug("cross", nil, []AnyKey{svc}, func(c *Context) error {
		return svc.Provide(c.Isolate(svc), 5)
	}), nil)
	if err := failure(t, bad); !errors.Is(err, ErrUndeclared) {
		t.Fatalf("provision in another realm: %v", err)
	}
}

func TestInheritedNeedsReachChildrenButNotOtherRealms(t *testing.T) {
	r := newKernel(t)
	svc := NewKey[int]("svc", 0)
	want(t, r.Plugin(plug("prov", nil, []AnyKey{svc}, func(c *Context) error { return svc.Provide(c, 11) }), nil), Active)
	var inherited int
	var children []*Fiber
	parent := r.Plugin(plug("parent", []Dep{Need(svc)}, nil, func(c *Context) error {
		children = append(children, c.When(nil, func(w *Context) error { inherited = svc.Of(w); return nil }))
		children = append(children, c.Isolate(svc).When(nil, func(w *Context) error { svc.Of(w); return nil }))
		return nil
	}), nil)
	want(t, parent, Active)
	want(t, children[0], Active)
	if inherited != 11 {
		t.Fatalf("child read %d, want the parent's binding", inherited)
	}
	if err := failure(t, children[1]); !errors.Is(err, ErrUndeclared) {
		t.Fatalf("a declaration crossed a realm: %v", err)
	}
}

func TestIsolateRefusals(t *testing.T) {
	bound := NewKey[int]("bound", 0, ProviderBound())
	open := NewKey[int]("open", 0)
	r := newKernel(t)
	c := r.Context()
	for name, fn := range map[string]func(){
		"zero key":   func() { c.Isolate(Key[int]{}) },
		"nil key":    func() { c.Isolate(nil) },
		"zero label": func() { c.Isolate(open, Label{}) },
		"two labels": func() { c.Isolate(open, NewLabel("a"), NewLabel("b")) },
	} {
		if e := injectFailure(t, fn); e == nil {
			t.Errorf("%s: not refused", name)
		}
	}

	// A provider-bound key may be isolated only by the fiber that provides it, whatever
	// the binding maps say: a kernel with the maps off still needs the declaration.
	var stranger, provider *Fiber
	var errStranger error
	provider = r.Plugin(plug("provider", nil, []AnyKey{bound}, func(c *Context) error {
		c.Isolate(bound)
		return nil
	}), nil)
	stranger = r.Plugin(plug("stranger", nil, nil, func(c *Context) error { c.Isolate(bound); return nil }), nil)
	want(t, provider, Active)
	errStranger = failure(t, stranger)
	if !errors.Is(errStranger, ErrIsolation) {
		t.Fatalf("stranger: %v", errStranger)
	}
	// The launcher may isolate it too, as it provides the launch keys.
	c.Isolate(bound)

	// With the maps on, a provider that is not listed is refused as well.
	listed := plug("listed", nil, []AnyKey{bound}, func(c *Context) error { c.Isolate(bound); return nil })
	k := newBound(t, Bindings{bound: {listed}}, nil)
	want(t, k.Plugin(listed, nil), Active)
	unlisted := k.Plugin(plug("unlisted", nil, nil, func(c *Context) error {
		// Declare the key behind the mount check, as a bypass would.
		c.frame.provides = append(c.frame.provides, bound.k.def)
		c.Isolate(bound)
		return nil
	}), nil)
	if err := failure(t, unlisted); !errors.Is(err, ErrIsolation) {
		t.Fatalf("unlisted: %v", err)
	}
}

func TestExtendSharesTheActivationAndDeclarations(t *testing.T) {
	r := newKernel(t)
	svc := NewKey[int]("svc", 0)
	want(t, r.Plugin(plug("prov", nil, []AnyKey{svc}, func(c *Context) error { return svc.Provide(c, 3) }), nil), Active)
	var got int
	f := r.Plugin(plug("c", []Dep{Need(svc)}, nil, func(c *Context) error {
		e := c.Extend().Extend()
		if e.parent == nil || e.Std() != c.Std() {
			return errors.New("Extend lost the activation")
		}
		got = svc.Of(e)
		return nil
	}), nil)
	want(t, f, Active)
	if got != 3 {
		t.Fatalf("got %d", got)
	}
}

func TestEffectsRunLastInFirstOutAndRefuseWhenInactive(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	var order []string
	var saved *Context
	f := r.Plugin(plug("p", nil, nil, func(c *Context) error {
		saved = c
		if err := c.Effect("first", func() (Disposer, error) {
			return func(context.Context) error { order = append(order, "first"); return nil }, nil
		}); err != nil {
			return err
		}
		c.Defer("second", func(context.Context) error { order = append(order, "second"); return nil })
		// Setup errors, empty labels and nil setups are errors, not effects.
		if err := c.Effect("bad", func() (Disposer, error) { return nil, errors.New("setup failed") }); err == nil {
			return errors.New("a failing setup was recorded")
		}
		if err := c.Effect("", func() (Disposer, error) { return nil, nil }); err == nil {
			return errors.New("an empty label was accepted")
		}
		return c.Effect("noop", func() (Disposer, error) { return nil, nil })
	}), nil)
	want(t, f, Active)
	if got := ledger(t, f); !slices.Equal(got, []string{"first", "second"}) {
		t.Fatalf("ledger = %v", got)
	}
	if err := f.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	want(t, f, Disposed)
	if !slices.Equal(order, []string{"second", "first"}) {
		t.Fatalf("disposal order = %v", order)
	}
	var ran atomic.Bool
	err := saved.Effect("late", func() (Disposer, error) {
		return func(context.Context) error { ran.Store(true); return nil }, nil
	})
	if !errors.Is(err, ErrInactiveEffect) || !errors.Is(err, ErrInactive) {
		t.Fatalf("late Effect: %v", err)
	}
	p := catch(func() { saved.Defer("late", func(context.Context) error { return nil }) })
	if e, ok := p.(*refusedCall); !ok || !errors.Is(e, ErrInactiveEffect) {
		t.Fatalf("late Defer panic = %#v", p)
	}
	if p := catch(func() { saved.Go("late", func(context.Context) error { return nil }) }); p == nil {
		t.Fatal("late Go was not refused")
	}
	if ran.Load() {
		t.Fatal("a refused setup's disposer was never run")
	}
}

func TestADeferRefusalFailsTheFiber(t *testing.T) {
	r := newKernel(t)
	for name, apply := range map[string]func(*Context){
		"nil disposer": func(c *Context) { c.Defer("x", nil) },
		"empty go":     func(c *Context) { c.Go("", func(context.Context) error { return nil }) },
	} {
		f := r.Plugin(plug(name, nil, nil, func(c *Context) error { apply(c); return nil }), nil)
		if err := failure(t, f); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestGoIsCancelledAndAwaitedAtUnload(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	started := make(chan struct{})
	var exited atomic.Bool
	var std context.Context
	f := r.Plugin(plug("p", nil, nil, func(c *Context) error {
		std = c.Std()
		c.Go("worker", func(g context.Context) error {
			close(started)
			<-g.Done()
			exited.Store(true)
			return g.Err()
		})
		return nil
	}), nil)
	want(t, f, Active)
	<-started
	if got := ledgerTracked(t, r, f); !slices.Equal(got, []string{"worker"}) {
		t.Fatalf("tracked = %v", got)
	}
	if std.Err() != nil {
		t.Fatal("Std was cancelled while ACTIVE")
	}
	if err := f.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	want(t, f, Disposed)
	if !exited.Load() || std.Err() == nil {
		t.Fatal("Go was not cancelled and awaited at unload")
	}
}

// ledgerTracked returns the labels of the tracked work of f.
func ledgerTracked(t testing.TB, r *Root, f *Fiber) []string {
	t.Helper()
	s, err := f.ref.snapshot(frontCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	return s.Tracked
}

func TestEnterIsALeaseWithoutAllocations(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	var c *Context
	f := r.Plugin(plug("p", nil, nil, func(x *Context) error { c = x; return nil }), nil)
	want(t, f, Active)
	if got := testing.AllocsPerRun(1000, func() {
		leave, ok := c.Enter()
		if !ok {
			t.Fatal("an active fiber refused a lease")
		}
		leave()
	}); got != 0 {
		t.Fatalf("Enter and leave allocate %v times", got)
	}
	leave, ok := c.Enter()
	if !ok {
		t.Fatal("no lease")
	}
	// Unload waits for the open lease, so the fiber is not DISPOSED yet.
	if err := f.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	leave()
	want(t, f, Disposed)
	if _, ok := c.Enter(); ok {
		t.Fatal("a disposed fiber granted a lease")
	}
}

func TestScopeIsWrittenOnlyByTheProviderOfTheOwningKey(t *testing.T) {
	r := newKernel(t)
	owner := NewKey[int]("owner", 0)
	scope := NewScope[string](owner)
	var providerCtx, strangerCtx, scoped, listener *Context
	var before error
	prov := r.Plugin(plug("prov", nil, []AnyKey{owner}, func(c *Context) error {
		providerCtx = c
		// Declaring the key is not providing it.
		_, before = scope.With(c, "early")
		if err := owner.Provide(c, 1); err != nil {
			return err
		}
		var err error
		scoped, err = scope.With(c, "t1")
		return err
	}), nil)
	want(t, prov, Active)
	if !errors.Is(before, ErrScope) {
		t.Fatalf("With before Provide: %v", before)
	}
	want(t, r.Plugin(plug("stranger", nil, nil, func(c *Context) error { strangerCtx = c; return nil }), nil), Active)
	if _, err := scope.With(strangerCtx, "x"); !errors.Is(err, ErrScope) {
		t.Fatalf("stranger With: %v", err)
	}
	if _, err := scope.With(nil, "x"); !errors.Is(err, ErrScope) {
		t.Fatalf("nil With: %v", err)
	}
	if v, ok := scope.From(scoped.Extend()); !ok || v != "t1" {
		t.Fatalf("From = %q, %v", v, ok)
	}
	if _, ok := scope.From(providerCtx); ok {
		t.Fatal("the scope leaked to the writer's own node")
	}
	if v, ok := scope.From(scoped.Extend().Extend().Intercept(owner, nil)); !ok || v != "t1" {
		t.Fatalf("nested From = %q, %v", v, ok)
	}
	if v, _ := scope.From(nil); v != "" {
		t.Fatal("From(nil) found a value")
	}
	var zero Scope[string]
	if _, ok := zero.From(scoped); ok {
		t.Fatal("a zero scope found a value")
	}
	if _, err := NewScope[string](nil).With(providerCtx, "x"); !errors.Is(err, ErrScope) {
		t.Fatalf("scope with no owner: %v", err)
	}

	// The nearest value wins, and a different scope never sees it.
	inner, err := scope.With(providerCtx, "t2")
	if err != nil {
		t.Fatal(err)
	}
	listener, err = scope.With(scoped, "deep")
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := scope.From(inner); v != "t2" {
		t.Fatalf("inner = %q", v)
	}
	if v, _ := scope.From(listener); v != "deep" {
		t.Fatalf("nearest = %q", v)
	}
	if _, ok := NewScope[string](owner).From(listener); ok {
		t.Fatal("another scope saw the value")
	}

	// After unload the writer is refused.
	if err := prov.Dispose(frontCtx(t)); err != nil {
		t.Fatal(err)
	}
	want(t, prov, Disposed)
	if _, err := scope.With(providerCtx, "late"); !errors.Is(err, ErrScope) {
		t.Fatalf("late With: %v", err)
	}
	if _, err := scope.Audience(providerCtx, func(string) bool { return true }); !errors.Is(err, ErrScope) {
		t.Fatalf("late Audience: %v", err)
	}
}

func TestAudienceNamesWhoMayHearADispatch(t *testing.T) {
	r := newKernel(t)
	other := newKernel(t)
	owner := NewKey[int]("owner", 0)
	scope := NewScope[string](owner)
	var audience Audience
	var t1, t2, plain, foreign *Context
	var bad []error
	prov := r.Plugin(plug("prov", nil, []AnyKey{owner}, func(c *Context) error {
		if err := owner.Provide(c, 1); err != nil {
			return err
		}
		var err error
		if t1, err = scope.With(c, "t1"); err != nil {
			return err
		}
		if t2, err = scope.With(c, "t2"); err != nil {
			return err
		}
		plain = c.Extend()
		if audience, err = scope.Audience(c, func(v string) bool { return v == "t1" }); err != nil {
			return err
		}
		_, e1 := scope.Audience(c, nil)
		_, e2 := scope.Audience(nil, func(string) bool { return true })
		bad = []error{e1, e2}
		return nil
	}), nil)
	want(t, prov, Active)
	want(t, other.Plugin(plug("l", nil, nil, func(c *Context) error { foreign = c; return nil }), nil), Active)
	for _, e := range bad {
		if !errors.Is(e, ErrScope) {
			t.Fatalf("Audience refusal: %v", e)
		}
	}
	if _, err := scope.Audience(r.Context(), func(string) bool { return true }); !errors.Is(err, ErrScope) {
		t.Fatalf("Audience by a non-provider: %v", err)
	}
	for name, tc := range map[string]struct {
		a        Audience
		listener *Context
		want     bool
	}{
		"scoped match":          {audience, t1.Extend(), true},
		"scoped mismatch":       {audience, t2, false},
		"scoped, unscoped":      {audience, plain, false},
		"scoped, nil":           {audience, nil, false},
		"scoped, other kernel":  {audience, foreign, false},
		"unscoped, unscoped":    {Unscoped, plain, true},
		"unscoped, scoped":      {Unscoped, t1, false},
		"unscoped, nil":         {Unscoped, nil, false},
		"unscoped, scoped deep": {Unscoped, t1.Extend().Extend(), false},
	} {
		if got := tc.a.Matches(tc.listener); got != tc.want {
			t.Errorf("%s: Matches = %v, want %v", name, got, tc.want)
		}
	}
}

func TestConsumerBoundKeysResolveOnlyForListedEntries(t *testing.T) {
	secret := NewKey[string]("secret", 0, ConsumerBound())
	signed := NewKey[string]("signed", 0, Signed())
	var got, inherited string
	var child *Fiber
	listed := plug("listed", []Dep{Need(secret), Need(signed)}, nil, func(c *Context) error {
		got = secret.Of(c) + signed.Of(c)
		// A child that was never defined inherits the entry of the plugin that mounted it.
		child = c.Plugin(Plugin{ID: "child", Inject: []Dep{Need(secret)}, Apply: func(cc *Context, _ any) error { inherited = secret.Of(cc); return nil }}, nil)
		return nil
	})
	stranger := plug("stranger", []Dep{Need(secret)}, nil, nil)
	r := newBound(t, nil, Bindings{secret: {listed}, signed: {listed}})
	provider := r.Plugin(plug("provider", nil, []AnyKey{secret, signed}, func(c *Context) error {
		if err := secret.Provide(c, "s"); err != nil {
			return err
		}
		return signed.Provide(c, "g")
	}), nil)
	want(t, provider, Active)
	if err := failure(t, r.Plugin(stranger, nil)); !errors.Is(err, ErrBinding) {
		t.Fatalf("stranger: %v", err)
	}
	f := r.Plugin(listed, nil)
	want(t, f, Active)
	want(t, child, Active)
	if got != "sg" || inherited != "s" {
		t.Fatalf("got %q, inherited %q", got, inherited)
	}
	// Resolution checks the class as well, for a context that bypassed the mount check.
	bypass := r.Plugin(plug("bypass", nil, nil, func(c *Context) error {
		c.frame.inject = []Dep{Need(secret)}
		c.frame.at = c
		secret.Of(c)
		return nil
	}), nil)
	if err := failure(t, bypass); !errors.Is(err, ErrBinding) {
		t.Fatalf("bypass: %v", err)
	}
	// The launcher's own context is not a listed entry either.
	if e := injectFailure(t, func() { secret.Of(r.Context()) }); !errors.Is(e, ErrBinding) {
		t.Fatalf("host resolve: %v", e)
	}
}

// Every entry point of a context decides liveness before any other refusal: on an
// activation that has ended each reports ErrInactive, not a declaration, class,
// argument or ownership error. The prototype's Provide does so (the property
// harness checks it). The registrations decide again under the lease for the
// action itself, so the first check opens no window.
func TestAnEndedActivationIsInactiveBeforeAnyOtherRefusal(t *testing.T) {
	r := newKernel(t)
	declared, undeclared := NewKey[int]("declared", 0), NewKey[int]("undeclared", 0)
	bound := NewKey[int]("bound", 0, ProviderBound())
	typed := NewKey[int]("typed", 0, InterceptConfig[string]())
	ev := NewEvent[int]("t/ended-ev", OwnedBy(undeclared))
	wf := NewWaterfall[int, int]("t/ended-wf", OwnedBy(undeclared))
	var held *Context
	f := r.Plugin(plug("p/ended", nil, []AnyKey{declared}, func(c *Context) error { held = c; return nil }), nil)
	want(t, f, Active)
	if err := f.Dispose(frontCtx(t)); err != nil {
		t.Fatal(err)
	}
	want(t, f, Disposed)
	cases := map[string]func() error{
		"Provide undeclared":    func() error { return undeclared.Provide(held, 1) },
		"Provide not bound":     func() error { return bound.Provide(held, 1) },
		"Provide zero key":      func() error { return Key[int]{}.Provide(held, 1) },
		"Provide declared":      func() error { return declared.Provide(held, 1) },
		"ProvideFor undeclared": func() error { return undeclared.ProvideFor(held, func(*Context) int { return 1 }) },
		"ProvideFor nil view":   func() error { return declared.ProvideFor(held, nil) },
		"ProvideFor declared":   func() error { return declared.ProvideFor(held, func(*Context) int { return 1 }) },
		"Bind event":            func() error { return refusal(t, func() { ev.Bind(held) }) },
		"Bind waterfall":        func() error { return refusal(t, func() { wf.Bind(held) }) },
		"On event":              func() error { return refusal(t, func() { ev.On(held, DefaultStage, noop) }) },
		"On event nil":          func() error { return refusal(t, func() { ev.On(held, "no-such-stage", nil) }) },
		"On waterfall":          func() error { return refusal(t, func() { wf.On(held, DefaultStage, nil) }) },
		"Isolate zero key":      func() error { return refusal(t, func() { held.Isolate(nil) }) },
		"Isolate bound key":     func() error { return refusal(t, func() { held.Isolate(bound) }) },
		"Isolate zero label":    func() error { return refusal(t, func() { held.Isolate(declared, Label{}) }) },
		"Intercept zero key":    func() error { return refusal(t, func() { held.Intercept(nil, nil) }) },
		"Intercept bad config":  func() error { return refusal(t, func() { held.Intercept(typed, 1) }) },
		"Plugin":                func() error { return refusal(t, func() { held.Plugin(Plugin{ID: "child", Inject: []Dep{{}}}, nil) }) },
		"When":                  func() error { return refusal(t, func() { held.When([]Dep{{}}, nil) }) },
		"Effect empty label":    func() error { return held.Effect("", nil) },
		"Effect":                func() error { return held.Effect("e", func() (Disposer, error) { return nil, nil }) },
		"Defer":                 func() error { return refusal(t, func() { held.Defer("", nil) }) },
		"Go empty label":        func() error { return refusal(t, func() { held.Go("", nil) }) },
		"RequestEffectScope":    func() error { _, err := held.RequestEffectScope("", nil); return err },
		"Dispose":               func() error { return held.Dispose() },
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, ErrInactive) {
				t.Fatalf("%v, want ErrInactive", err)
			}
		})
	}
}
