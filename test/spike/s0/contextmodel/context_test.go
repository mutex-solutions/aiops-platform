// SPDX-License-Identifier: Apache-2.0

package contextmodel_test

import (
	"errors"
	"reflect"
	"sync"
	"testing"

	cm "github.com/mutex-solutions/aiops-platform/test/spike/s0/contextmodel"
)

func must[T any](t *testing.T, value T, err error) T {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func root(t *testing.T, spec cm.RootSpec) *cm.Root {
	t.Helper()
	value, err := cm.NewRoot(spec)
	return must(t, value, err)
}

func attach(t *testing.T, r *cm.Root, parent *cm.Context, entry cm.Plugin, spec cm.Spec) *cm.Context {
	t.Helper()
	value, err := r.WithEntry(parent, entry, spec)
	return must(t, value, err)
}

func child(t *testing.T, parent *cm.Context, spec cm.Spec) *cm.Context {
	t.Helper()
	value, err := parent.Child(spec)
	return must(t, value, err)
}

func isolated(t *testing.T, parent *cm.Context, key cm.AnyKey, label cm.Label) *cm.Context {
	t.Helper()
	value, err := parent.Isolate(key, label)
	return must(t, value, err)
}

func intercepted(t *testing.T, parent *cm.Context, key cm.AnyKey, value any) *cm.Context {
	t.Helper()
	result, err := parent.Intercept(key, value)
	return must(t, result, err)
}

func provided[T any](t *testing.T, c *cm.Context, key cm.Key[T], value T) cm.Provision {
	t.Helper()
	result, err := cm.Provide(c, key, value, nil)
	return must(t, result, err)
}

func snapshot(t *testing.T, r *cm.Root, provisions ...cm.Provision) *cm.Snapshot {
	t.Helper()
	value, err := r.Snapshot(provisions...)
	return must(t, value, err)
}

func resolves[T comparable](t *testing.T, s *cm.Snapshot, c *cm.Context, key cm.Key[T], expected T) {
	t.Helper()
	got, err := cm.Resolve(s, c, key)
	if err != nil || got != expected {
		t.Fatalf("Resolve(%s@%d) = %v, %v; want %v", key.Any().Name(), key.Any().Major(), got, err, expected)
	}
}

func wantError(t *testing.T, err, expected error) {
	t.Helper()
	if !errors.Is(err, expected) {
		t.Fatalf("error = %v, want %v", err, expected)
	}
}

func TestImmutableExtendAndDeclarationContainers(t *testing.T) {
	entry := cm.NewEntry("owner/unit")
	key := cm.NewKey[string]("item", 1)
	r := root(t, cm.RootSpec{Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{key.Any()}})
	needs, gives := []cm.Dep{cm.Need(key)}, []cm.AnyKey{key.Any()}
	parent := attach(t, r, r.Context(), entry, cm.Spec{ID: "parent", Inject: needs, Provides: gives})
	needs[0], gives[0] = cm.Dep{}, cm.AnyKey{}
	extended := parent.Extend()
	if extended.Parent() != parent || extended.Entry() != entry || parent.Parent() != r.Context() {
		t.Fatal("Extend changed the parent reference or entry identity")
	}
	copy := parent.Spec()
	copy.Inject[0], copy.Provides[0], copy.ID = cm.Dep{}, cm.AnyKey{}, "changed"
	if parent.Spec().ID != "parent" || parent.Spec().Inject[0].Key() != key.Any() || parent.Spec().Provides[0] != key.Any() {
		t.Fatal("caller-owned slices mutated frozen declarations")
	}
	s := snapshot(t, r, provided(t, extended, key, "value"))
	resolves(t, s, parent, key, "value")
	resolves(t, s, extended, key, "value")
	dependencies := extended.Dependencies()
	dependencies[0] = cm.Dep{}
	if extended.Dependencies()[0].Key() != key.Any() || len(r.Context().Dependencies()) != 0 {
		t.Fatal("returned list mutated a node or leaked into its parent")
	}
	if r.Context().Spec().ID != "" || len(r.Context().Spec().Inject) != 0 {
		t.Fatal("child declarations mutated root")
	}
}

func TestPerKeyAndMajorRealmsJoinByLabelValue(t *testing.T) {
	entry := cm.NewEntry("unit")
	one, same := cm.NewKey[string]("item", 1), cm.NewKey[string]("item", 1)
	two, other := cm.NewKey[string]("item", 2), cm.NewKey[string]("other", 1)
	r := root(t, cm.RootSpec{Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{one.Any(), same.Any(), two.Any(), other.Any()}})
	base := attach(t, r, r.Context(), entry, cm.Spec{Provides: []cm.AnyKey{one.Any(), two.Any(), other.Any()}})
	label := cm.NewLabel("join")
	a := isolated(t, base, one.Any(), label)
	b := isolated(t, base, same.Any(), label)
	separate := isolated(t, base, one.Any(), cm.NewLabel("join"))
	fresh, err := base.Isolate(one.Any())
	fresh = must(t, fresh, err)
	local := provided(t, a, one, "local")
	s := snapshot(t, r, provided(t, base, one, "root"), provided(t, base, two, "second"), provided(t, base, other, "other"), local)
	consumerSpec := cm.Spec{Inject: []cm.Dep{cm.Need(same), cm.Need(two), cm.Need(other)}}
	resolves(t, s, child(t, base, consumerSpec), one, "root")
	joined := child(t, b, consumerSpec)
	resolves(t, s, joined, same, "local")
	resolves(t, s, joined, two, "second")
	resolves(t, s, joined, other, "other")
	for _, c := range []*cm.Context{separate, fresh} {
		_, err := cm.Resolve(s, child(t, c, consumerSpec), one)
		wantError(t, err, cm.ErrUnavailable)
	}
	_, err = r.Snapshot(local, provided(t, b, same, "duplicate"))
	wantError(t, err, cm.ErrDuplicate)
	_, err = base.Isolate(one.Any(), cm.Label{})
	wantError(t, err, cm.ErrIsolation)
	_, err = base.Isolate(one.Any(), label, label)
	wantError(t, err, cm.ErrIsolation)
}

func TestInheritedNeedsDoNotCrossRealmsAndWantIsNotReadPermission(t *testing.T) {
	entry := cm.NewEntry("unit")
	key := cm.NewKey[int]("item", 1)
	r := root(t, cm.RootSpec{Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{key.Any()}})
	parent := attach(t, r, r.Context(), entry, cm.Spec{Inject: []cm.Dep{cm.Need(key)}, Provides: []cm.AnyKey{key.Any()}})
	s := snapshot(t, r, provided(t, parent, key, 1))
	resolves(t, s, child(t, parent, cm.Spec{}), key, 1)
	isolated := isolated(t, parent, key.Any(), cm.NewLabel("separate"))
	_, err := cm.Resolve(s, isolated.Extend(), key)
	wantError(t, err, cm.ErrUndeclared)
	optional := attach(t, r, r.Context(), entry, cm.Spec{Inject: []cm.Dep{cm.Want(key)}})
	ready, err := s.Available(optional, key.Any())
	if err != nil || !ready {
		t.Fatalf("Want availability = %v, %v", ready, err)
	}
	_, err = cm.Resolve(s, optional, key)
	wantError(t, err, cm.ErrUndeclared)
	requiredChild := child(t, optional, cm.Spec{Inject: []cm.Dep{cm.Need(key)}})
	resolves(t, s, requiredChild, key, 1)
	if deps := requiredChild.Dependencies(); len(deps) != 1 || deps[0].Optional() {
		t.Fatal("required child did not dominate the inherited optional declaration")
	}
	stillRequired := child(t, parent, cm.Spec{Inject: []cm.Dep{cm.Want(key)}})
	resolves(t, s, stillRequired, key, 1)
}

func TestEntryIdentityBindingsAndInheritedAuthorization(t *testing.T) {
	owner, reader, outsider := cm.NewEntry("unit/owner"), cm.NewEntry("unit/reader"), cm.NewEntry("unit/owner")
	key := cm.NewKey[string]("item", 1, cm.Classes(cm.ProviderBound, cm.ConsumerBound))
	entries := []cm.Plugin{owner, reader, outsider}
	providers, consumers := []cm.Plugin{owner}, []cm.Plugin{reader}
	bindings := []cm.Binding{{Key: key.Any(), Providers: providers, Consumers: consumers}}
	r := root(t, cm.RootSpec{Entries: entries, Keys: []cm.AnyKey{key.Any()}, Bindings: bindings})
	providers[0], consumers[0], entries[0], bindings[0] = outsider, outsider, outsider, cm.Binding{}
	if owner == outsider || owner.Name() != outsider.Name() {
		t.Fatal("equal diagnostic strings must have distinct value identities")
	}
	provider := attach(t, r, r.Context(), owner, cm.Spec{ID: "owner", Provides: []cm.AnyKey{key.Any()}})
	_, err := r.WithEntry(r.Context(), outsider, cm.Spec{ID: owner.Name(), Provides: []cm.AnyKey{key.Any()}})
	wantError(t, err, cm.ErrBinding)
	_, err = r.WithEntry(r.Context(), outsider, cm.Spec{ID: reader.Name(), Inject: []cm.Dep{cm.Need(key)}})
	wantError(t, err, cm.ErrBinding)
	consumer := attach(t, r, r.Context(), reader, cm.Spec{Inject: []cm.Dep{cm.Need(key)}})
	s := snapshot(t, r, provided(t, provider, key, "safe"))
	inherited := child(t, consumer, cm.Spec{ID: owner.Name()})
	if inherited.Entry() != reader {
		t.Fatal("child self-declared ID changed entry authority")
	}
	resolves(t, s, inherited, key, "safe")
	wrongIdentity := attach(t, r, consumer, outsider, cm.Spec{ID: reader.Name()})
	_, err = cm.Resolve(s, wrongIdentity, key)
	wantError(t, err, cm.ErrBinding)
	_, err = s.Available(wrongIdentity, key.Any())
	wantError(t, err, cm.ErrBinding)
	_, err = provider.Isolate(key.Any())
	if err != nil {
		t.Fatalf("approved declaring provider exception denied: %v", err)
	}
	_, err = consumer.Isolate(key.Any())
	wantError(t, err, cm.ErrIsolation)
	_, err = child(t, provider, cm.Spec{ID: owner.Name()}).Isolate(key.Any())
	wantError(t, err, cm.ErrIsolation)
	_, err = r.WithEntry(r.Context(), reader, cm.Spec{Inject: []cm.Dep{cm.Want(key)}})
	wantError(t, err, cm.ErrWant)
	_, err = r.WithEntry(r.Context(), cm.NewEntry(owner.Name()), cm.Spec{})
	wantError(t, err, cm.ErrEntry)
	_, err = r.Context().Child(cm.Spec{ID: owner.Name()})
	wantError(t, err, cm.ErrEntry)
}

func TestSignedConsumersUseApprovedEntriesNotSelfClaims(t *testing.T) {
	provider, allowed, denied := cm.NewEntry("provider"), cm.NewEntry("reader"), cm.NewEntry("reader")
	key := cm.NewKey[int]("item", 1, cm.Classes(cm.Open, cm.Signed))
	r := root(t, cm.RootSpec{Entries: []cm.Plugin{provider, allowed, denied}, Keys: []cm.AnyKey{key.Any()}, Bindings: []cm.Binding{{Key: key.Any(), Consumers: []cm.Plugin{allowed}}}})
	p := attach(t, r, r.Context(), provider, cm.Spec{Provides: []cm.AnyKey{key.Any()}})
	c := attach(t, r, r.Context(), allowed, cm.Spec{Inject: []cm.Dep{cm.Need(key)}})
	resolves(t, snapshot(t, r, provided(t, p, key, 2)), child(t, c, cm.Spec{ID: denied.Name()}), key, 2)
	_, err := r.WithEntry(r.Context(), denied, cm.Spec{Inject: []cm.Dep{cm.Need(key)}})
	wantError(t, err, cm.ErrBinding)
	closed := root(t, cm.RootSpec{Entries: []cm.Plugin{allowed}, Keys: []cm.AnyKey{key.Any()}})
	_, err = closed.WithEntry(closed.Context(), allowed, cm.Spec{Inject: []cm.Dep{cm.Need(key)}})
	wantError(t, err, cm.ErrBinding)
}

func TestNeedWithAndInterceptsAreAncestorFirstAndPerChild(t *testing.T) {
	type config struct{ Value string }
	entry := cm.NewEntry("unit")
	key := cm.NewKey[string]("item", 1, cm.Config[config]())
	other := cm.NewKey[string]("other", 1, cm.Config[config]())
	r := root(t, cm.RootSpec{Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{key.Any(), other.Any()}})
	base := intercepted(t, r.Context(), key.Any(), config{"root"})
	base = intercepted(t, base, other.Any(), config{"other"})
	parent := attach(t, r, base, entry, cm.Spec{Inject: []cm.Dep{cm.NeedWith(key, config{"parent"})}})
	middle := intercepted(t, parent, key.Any(), config{"middle"})
	leaf := child(t, middle, cm.Spec{Inject: []cm.Dep{cm.NeedWith(key, config{"leaf"})}})
	expected := []config{{"root"}, {"parent"}, {"middle"}, {"leaf"}}
	if got := cm.Intercepts[config](leaf, key.Any()); !reflect.DeepEqual(got, expected) {
		t.Fatalf("chain = %v, want %v", got, expected)
	}
	if got := cm.Intercepts[config](parent, key.Any()); !reflect.DeepEqual(got, expected[:2]) {
		t.Fatalf("child mutated parent intercepts: %v", got)
	}
	if got := cm.Intercepts[config](leaf, other.Any()); !reflect.DeepEqual(got, []config{{"other"}}) {
		t.Fatalf("another key chain = %v", got)
	}
	got := cm.Intercepts[config](leaf, key.Any())
	got[0] = config{"changed"}
	if cm.Intercepts[config](leaf, key.Any())[0] != expected[0] {
		t.Fatal("returned list exposed internal storage")
	}
	value, with := leaf.Spec().Inject[0].Config()
	if !with || value != expected[3] {
		t.Fatal("NeedWith config not retained as declaration data")
	}
	if _, with := cm.Need(key).Config(); with {
		t.Fatal("plain Need claimed a config")
	}
	_, err := leaf.Intercept(key.Any(), "wrong type")
	wantError(t, err, cm.ErrConfig)
	_, err = leaf.Child(cm.Spec{Inject: []cm.Dep{cm.NeedWith(key, "wrong type")}})
	wantError(t, err, cm.ErrConfig)
	defer func() {
		if recovered := recover(); recovered != cm.ErrConfig {
			t.Errorf("wrong config result type panic = %v", recovered)
		}
	}()
	cm.Intercepts[string](leaf, key.Any())
}

func TestPerConsumerAvailabilityAndBoundViews(t *testing.T) {
	provider, consumer := cm.NewEntry("provider"), cm.NewEntry("consumer")
	key := cm.NewKey[string]("item", 1, cm.Config[string]())
	r := root(t, cm.RootSpec{Entries: []cm.Plugin{provider, consumer}, Keys: []cm.AnyKey{key.Any()}})
	p := attach(t, r, r.Context(), provider, cm.Spec{Provides: []cm.AnyKey{key.Any()}})
	first := attach(t, r, r.Context(), consumer, cm.Spec{Inject: []cm.Dep{cm.NeedWith(key, "first")}})
	second := attach(t, r, r.Context(), consumer, cm.Spec{Inject: []cm.Dep{cm.NeedWith(key, "second")}})
	var checked, viewed []*cm.Context
	provision, err := cm.ProvideFor(p, key, func(c *cm.Context) string {
		viewed = append(viewed, c)
		return c.Entry().Name() + ":" + cm.Intercepts[string](c, key.Any())[0]
	}, func(c *cm.Context) bool {
		checked = append(checked, c)
		return cm.Intercepts[string](c, key.Any())[0] == "first"
	})
	provision = must(t, provision, err)
	s := snapshot(t, r, provision)
	resolves(t, s, first, key, "consumer:first")
	ready, err := s.Available(second, key.Any())
	if err != nil || ready {
		t.Fatalf("second availability = %v, %v", ready, err)
	}
	_, err = cm.Resolve(s, second, key)
	wantError(t, err, cm.ErrUnavailable)
	resolves(t, s, first.Extend(), key, "consumer:first")
	if len(checked) != 4 || checked[0] != first || checked[1] != second || checked[2] != second || len(viewed) != 2 || viewed[0] != first {
		t.Fatal("availability/view was cached for the wrong consumer or called while unavailable")
	}
}

func TestScopesProviderWriterInheritanceAndExplicitAudience(t *testing.T) {
	provider, consumer, impostor := cm.NewEntry("provider"), cm.NewEntry("consumer"), cm.NewEntry("provider")
	key := cm.NewKey[string]("item", 1, cm.Classes(cm.ProviderBound, cm.Open))
	r := root(t, cm.RootSpec{Entries: []cm.Plugin{provider, consumer, impostor}, Keys: []cm.AnyKey{key.Any()}, Bindings: []cm.Binding{{Key: key.Any(), Providers: []cm.Plugin{provider}}}})
	owner := attach(t, r, r.Context(), provider, cm.Spec{Provides: []cm.AnyKey{key.Any()}})
	reader := attach(t, r, r.Context(), consumer, cm.Spec{})
	pretender := attach(t, r, r.Context(), impostor, cm.Spec{ID: provider.Name()})
	scope := cm.NewScope[string](key.Any())
	first, err := scope.With(owner, "first")
	first = must(t, first, err)
	second, err := scope.With(owner, "second")
	second = must(t, second, err)
	listener := child(t, first, cm.Spec{})
	if value, exists := scope.From(listener); !exists || value != "first" {
		t.Fatal("typed scope value did not inherit")
	}
	if _, exists := scope.From(owner); exists {
		t.Fatal("scope write mutated its parent")
	}
	for _, writer := range []*cm.Context{reader, listener, pretender, nil, r.Context()} {
		_, err := scope.With(writer, "forbidden")
		wantError(t, err, cm.ErrScope)
		_, err = scope.Audience(writer, func(string) bool { return true })
		wantError(t, err, cm.ErrScope)
	}
	if value, exists := scope.From(second); !exists || value != "second" {
		t.Fatal("sibling scope value changed")
	}
	audience, err := scope.Audience(owner, func(value string) bool { return value == "first" })
	audience = must(t, audience, err)
	if !audience.Matches(listener) || audience.Matches(second) || audience.Matches(owner) || audience.Matches(nil) {
		t.Fatal("explicit audience did not filter by scope")
	}
	unscoped := cm.Audience{}
	if !unscoped.Matches(reader) || !unscoped.Matches(r.Context()) || unscoped.Matches(first) || unscoped.Matches(listener) || unscoped.Matches(nil) {
		t.Fatal("unscoped dispatch reached a scoped listener")
	}
	allScoped, err := scope.Audience(owner, func(string) bool { return true })
	allScoped = must(t, allScoped, err)
	if allScoped.Matches(reader) || !allScoped.Matches(first) || !allScoped.Matches(second) {
		t.Fatal("scope predicate acquired a global bypass")
	}
	foreign := root(t, cm.RootSpec{Entries: []cm.Plugin{provider}, Keys: []cm.AnyKey{key.Any()}, Bindings: []cm.Binding{{Key: key.Any(), Providers: []cm.Plugin{provider}}}})
	foreignOwner := attach(t, foreign, foreign.Context(), provider, cm.Spec{Provides: []cm.AnyKey{key.Any()}})
	foreignScope, err := scope.With(foreignOwner, "first")
	foreignScope = must(t, foreignScope, err)
	if audience.Matches(foreignScope) {
		t.Fatal("audience crossed a root boundary")
	}
	_, err = scope.Audience(owner, nil)
	wantError(t, err, cm.ErrScope)
	other := cm.NewScope[int](key.Any())
	both, err := other.With(first, 7)
	both = must(t, both, err)
	if value, exists := scope.From(both); !exists || value != "first" {
		t.Fatal("second typed scope write hid first scope")
	}
	if value, exists := other.From(both); !exists || value != 7 {
		t.Fatal("second typed scope was not recorded")
	}
}

func TestNilTypedValuesAndDynamicConfig(t *testing.T) {
	entry := cm.NewEntry("unit")
	key := cm.NewKey[any]("item", 1)
	r := root(t, cm.RootSpec{Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{key.Any()}})
	c := attach(t, r, r.Context(), entry, cm.Spec{Inject: []cm.Dep{cm.Need(key)}, Provides: []cm.AnyKey{key.Any()}})
	if value, err := cm.Resolve(snapshot(t, r, provided[any](t, c, key, nil)), c, key); value != nil || err != nil {
		t.Fatalf("nil interface resolve = %v, %v", value, err)
	}
	nilConfig := intercepted(t, c, key.Any(), nil)
	if got := cm.Intercepts[any](nilConfig, key.Any()); len(got) != 1 || got[0] != nil {
		t.Fatalf("nil interface config = %v", got)
	}
	var typedNil *int
	pointerConfig := intercepted(t, c, key.Any(), typedNil)
	if got := cm.Intercepts[*int](pointerConfig, key.Any()); len(got) != 1 || got[0] != nil {
		t.Fatalf("typed nil config = %v", got)
	}
	scope := cm.NewScope[any](key.Any())
	scoped, err := scope.With(c, nil)
	scoped = must(t, scoped, err)
	if value, exists := scope.From(scoped); value != nil || !exists {
		t.Fatal("nil scope value confused with absence")
	}
}

func TestRootRejectsInvalidKeysBindingsAndCollisions(t *testing.T) {
	type first interface{ String() string }
	type second interface{ String() string }
	entry := cm.NewEntry("unit")
	zeroMajor := cm.NewKey[int]("initial", 0)
	zeroRoot := root(t, cm.RootSpec{Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{zeroMajor.Any()}})
	zeroContext := attach(t, zeroRoot, zeroRoot.Context(), entry, cm.Spec{Inject: []cm.Dep{cm.Need(zeroMajor)}, Provides: []cm.AnyKey{zeroMajor.Any()}})
	resolves(t, snapshot(t, zeroRoot, provided(t, zeroContext, zeroMajor, 0)), zeroContext, zeroMajor, 0)
	key := cm.NewKey[string]("item", 1)
	bound := cm.NewKey[string]("bound", 1, cm.Classes(cm.ProviderBound, cm.ConsumerBound))
	for name, keys := range map[string][]cm.AnyKey{
		"value type":      {key.Any(), cm.NewKey[int]("item", 1).Any()},
		"named interface": {cm.NewKey[first]("item", 1).Any(), cm.NewKey[second]("item", 1).Any()},
		"classes":         {key.Any(), cm.NewKey[string]("item", 1, cm.Classes(cm.ProviderBound, cm.Open)).Any()},
		"config":          {key.Any(), cm.NewKey[string]("item", 1, cm.Config[int]()).Any()},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := cm.NewRoot(cm.RootSpec{Keys: keys})
			wantError(t, err, cm.ErrCollision)
		})
	}
	for _, invalid := range []cm.AnyKey{cm.AnyKey{}, cm.NewKey[int]("", 1).Any(), cm.NewKey[int]("item", -1).Any(), cm.NewKey[int]("item", 1, cm.Classes(cm.ConsumerBound, cm.Open)).Any(), cm.NewKey[int]("item", 1, cm.Classes(cm.Open, cm.ProviderBound)).Any()} {
		_, err := cm.NewRoot(cm.RootSpec{Keys: []cm.AnyKey{invalid}})
		wantError(t, err, cm.ErrKey)
	}
	for _, invalid := range [][]cm.Plugin{{{}}, {cm.NewEntry("")}, {entry, entry}} {
		_, err := cm.NewRoot(cm.RootSpec{Entries: invalid})
		wantError(t, err, cm.ErrEntry)
	}
	for _, test := range []struct {
		bindings []cm.Binding
		err      error
	}{
		{[]cm.Binding{{Key: cm.NewKey[int]("missing", 1).Any()}}, cm.ErrKey},
		{[]cm.Binding{{Key: bound.Any()}, {Key: bound.Any()}}, cm.ErrBinding},
		{[]cm.Binding{{Key: key.Any(), Providers: []cm.Plugin{entry}}}, cm.ErrBinding},
		{[]cm.Binding{{Key: key.Any(), Consumers: []cm.Plugin{entry}}}, cm.ErrBinding},
		{[]cm.Binding{{Key: bound.Any(), Providers: []cm.Plugin{cm.NewEntry(entry.Name())}}}, cm.ErrEntry},
	} {
		_, err := cm.NewRoot(cm.RootSpec{Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{key.Any(), bound.Any()}, Bindings: test.bindings})
		wantError(t, err, test.err)
	}
	r := root(t, cm.RootSpec{Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{key.Any(), bound.Any()}})
	_, err := r.WithEntry(r.Context(), entry, cm.Spec{Provides: []cm.AnyKey{bound.Any()}})
	wantError(t, err, cm.ErrBinding)
	if key.Any().ProviderClass() != cm.Open || key.Any().ConsumerClass() != cm.Open || bound.Any().ProviderClass() != cm.ProviderBound || bound.Any().ConsumerClass() != cm.ConsumerBound {
		t.Fatal("class definition data changed")
	}
}

func TestInvalidOperationsFailClosed(t *testing.T) {
	entry := cm.NewEntry("unit")
	key, wrong := cm.NewKey[int]("item", 1), cm.NewKey[string]("item", 1)
	r := root(t, cm.RootSpec{Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{key.Any()}})
	c := attach(t, r, r.Context(), entry, cm.Spec{})
	_, err := cm.Provide(c, key, 1, nil)
	wantError(t, err, cm.ErrUndeclared)
	_, err = cm.Provide(c, wrong, "bad", nil)
	wantError(t, err, cm.ErrCollision)
	declared := child(t, c, cm.Spec{Inject: []cm.Dep{cm.Need(key)}, Provides: []cm.AnyKey{key.Any()}})
	_, err = cm.ProvideFor(declared, key, nil, nil)
	wantError(t, err, cm.ErrUnavailable)
	_, err = c.Child(cm.Spec{Inject: []cm.Dep{cm.Need(key), cm.Want(key)}})
	wantError(t, err, cm.ErrDuplicate)
	_, err = c.Child(cm.Spec{Provides: []cm.AnyKey{key.Any(), key.Any()}})
	wantError(t, err, cm.ErrDuplicate)
	_, err = r.Snapshot(cm.Provision{})
	wantError(t, err, cm.ErrContext)
	_, err = cm.Resolve(snapshot(t, r), declared, key)
	wantError(t, err, cm.ErrUnavailable)
	_, err = cm.Resolve[int](nil, declared, key)
	wantError(t, err, cm.ErrContext)
	_, err = cm.Resolve(snapshot(t, r), c, key)
	wantError(t, err, cm.ErrUndeclared)
	foreign := root(t, cm.RootSpec{Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{key.Any()}})
	_, err = r.WithEntry(foreign.Context(), entry, cm.Spec{})
	wantError(t, err, cm.ErrContext)
	_, err = foreign.Snapshot(provided(t, declared, key, 1))
	wantError(t, err, cm.ErrContext)
	_, err = cm.Resolve(snapshot(t, foreign), declared, key)
	wantError(t, err, cm.ErrContext)
}

func TestParallelImmutableReadAndDerivation(t *testing.T) {
	entry := cm.NewEntry("unit")
	key := cm.NewKey[int]("item", 1, cm.Config[int]())
	r := root(t, cm.RootSpec{Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{key.Any()}})
	c := attach(t, r, r.Context(), entry, cm.Spec{Inject: []cm.Dep{cm.NeedWith(key, 1)}, Provides: []cm.AnyKey{key.Any()}})
	scope := cm.NewScope[int](key.Any())
	c, err := scope.With(c, 3)
	c = must(t, c, err)
	p, err := cm.ProvideFor(c, key, func(caller *cm.Context) int {
		value, _ := scope.From(caller)
		return value + cm.Intercepts[int](caller, key.Any())[0]
	}, func(caller *cm.Context) bool { _, exists := scope.From(caller); return exists })
	p = must(t, p, err)
	s := snapshot(t, r, p)
	var group sync.WaitGroup
	for range 16 {
		group.Go(func() {
			for range 50 {
				derived := c.Extend()
				got, err := cm.Resolve(s, derived, key)
				if err != nil || got != 4 {
					t.Errorf("concurrent immutable read = %v, %v", got, err)
					return
				}
				changed, err := derived.Intercept(key.Any(), 2)
				if err != nil || !reflect.DeepEqual(cm.Intercepts[int](changed, key.Any()), []int{1, 2}) || !reflect.DeepEqual(cm.Intercepts[int](c, key.Any()), []int{1}) {
					t.Error("parallel derivation mutated its parent")
					return
				}
			}
		})
	}
	group.Wait()
}
