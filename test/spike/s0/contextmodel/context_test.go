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

func TestHostContextHasRegisteredIdentityAndFrozenRootDeclarations(t *testing.T) {
	entry := cm.NewEntry("kernel")
	key, other := cm.NewKey[int]("item", 1), cm.NewKey[int]("other", 1)
	r := root(t, cm.RootSpec{Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{key.Any(), other.Any()}})
	spec := cm.Spec{ID: "caller", Inject: []cm.Dep{cm.Need(key)}, Provides: []cm.AnyKey{key.Any()}}
	host, err := r.HostContext(spec)
	host = must(t, host, err)
	if host.Parent() != r.Context() || host.Entry() == (cm.Plugin{}) || host.Entry() == entry || host.Entry().Name() != "kernel" {
		t.Fatal("host did not use its own opaque entry under the generic root")
	}
	if host.Spec().ID != "root" || spec.ID != "caller" {
		t.Fatal("host ID was caller-selected or changed the caller's Spec")
	}
	spec.Inject[0], spec.Provides[0] = cm.Dep{}, cm.AnyKey{}
	copy := host.Spec()
	copy.ID, copy.Inject[0], copy.Provides[0] = "changed", cm.Dep{}, cm.AnyKey{}
	expected := cm.Spec{ID: "root", Inject: []cm.Dep{cm.Need(key)}, Provides: []cm.AnyKey{key.Any()}}
	if !reflect.DeepEqual(host.Spec(), expected) {
		t.Fatal("host retained caller-owned declaration containers")
	}
	if err := host.AuthorizeProvide(key.Any()); err != nil {
		t.Fatal(err)
	}
	wantError(t, host.AuthorizeProvide(other.Any()), cm.ErrUndeclared)
	if err := host.AuthorizeRead(key.Any(), false); err != nil {
		t.Fatal(err)
	}
	wantError(t, host.AuthorizeRead(other.Any(), false), cm.ErrUndeclared)
	values := map[string]any{"origin": "host"}
	extended := host.WithValues(values).Extend()
	values["origin"] = "changed"
	if extended.Entry() != host.Entry() || !reflect.DeepEqual(extended.Spec(), expected) || extended.Values()["origin"] != "host" {
		t.Fatal("host derivation lost its identity, frame, or frozen values")
	}
	descendant := child(t, extended, cm.Spec{ID: entry.Name()})
	if descendant.Entry() != host.Entry() || descendant.Values()["origin"] != "host" {
		t.Fatal("Child replaced the inherited host identity or metadata")
	}
	wantError(t, descendant.AuthorizeProvide(key.Any()), cm.ErrUndeclared)
	registered := attach(t, r, r.Context(), host.Entry(), cm.Spec{})
	if registered.Entry() != host.Entry() {
		t.Fatal("host identity was not a genuinely registered entry")
	}
	empty, err := r.HostContext(cm.Spec{ID: "not-root"})
	empty = must(t, empty, err)
	if empty.Entry() != host.Entry() || empty.Spec().ID != "root" || len(empty.Spec().Provides) != 0 {
		t.Fatal("nil Provides changed the host identity or added declarations")
	}
	wantError(t, empty.AuthorizeProvide(key.Any()), cm.ErrUndeclared)
	scope := cm.NewScope[string](key.Any())
	_, err = scope.With(host, "metadata")
	if err != nil {
		t.Fatal(err)
	}
	_, err = scope.With(empty, "not-owned")
	wantError(t, err, cm.ErrScope)
	generic := r.Context()
	if generic.Entry() != (cm.Plugin{}) || !reflect.DeepEqual(generic.Spec(), cm.Spec{}) || generic.Parent() != nil {
		t.Fatal("HostContext changed the generic root")
	}
	for _, parent := range []*cm.Context{generic, generic.Extend()} {
		_, err = parent.Child(cm.Spec{ID: "root"})
		wantError(t, err, cm.ErrEntry)
	}
	user := child(t, attach(t, r, generic, entry, cm.Spec{}), cm.Spec{ID: "root"})
	if user.Entry() != entry || user.Entry() == host.Entry() {
		t.Fatal("same-text entry or Spec ID acquired the host identity")
	}
}

func TestHostContextHasNoBoundOrSignedPrivileges(t *testing.T) {
	entry := cm.NewEntry("kernel")
	provider := cm.NewKey[int]("provider", 1, cm.Classes(cm.ProviderBound, cm.Open))
	consumer := cm.NewKey[int]("consumer", 1, cm.Classes(cm.Open, cm.ConsumerBound))
	signed := cm.NewKey[int]("signed", 1, cm.Classes(cm.Open, cm.Signed))
	r := root(t, cm.RootSpec{
		Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{provider.Any(), consumer.Any(), signed.Any()},
		Bindings: []cm.Binding{
			{Key: provider.Any(), Providers: []cm.Plugin{entry}},
			{Key: consumer.Any(), Consumers: []cm.Plugin{entry}},
			{Key: signed.Any(), Consumers: []cm.Plugin{entry}},
		},
	})
	attach(t, r, r.Context(), entry, cm.Spec{Provides: []cm.AnyKey{provider.Any()}, Inject: []cm.Dep{cm.Need(consumer), cm.Need(signed)}})
	host, err := r.HostContext(cm.Spec{})
	host = must(t, host, err)
	wantError(t, host.AuthorizeProvide(provider.Any()), cm.ErrBinding)
	for _, key := range []cm.AnyKey{consumer.Any(), signed.Any()} {
		wantError(t, host.AuthorizeRead(key, false), cm.ErrBinding)
	}
	for _, spec := range []cm.Spec{
		{ID: "kernel", Provides: []cm.AnyKey{provider.Any()}},
		{ID: "kernel", Inject: []cm.Dep{cm.Need(consumer)}},
		{ID: "kernel", Inject: []cm.Dep{cm.Need(signed)}},
	} {
		_, err = r.HostContext(spec)
		wantError(t, err, cm.ErrBinding)
		_, err = host.Child(spec)
		wantError(t, err, cm.ErrBinding)
		_, err = r.WithEntry(r.Context(), host.Entry(), spec)
		wantError(t, err, cm.ErrBinding)
	}
	_, err = host.Isolate(provider.Any())
	wantError(t, err, cm.ErrIsolation)
}

func TestHostContextIsRootLocalAndUsesOrdinaryDeclarationValidation(t *testing.T) {
	key := cm.NewKey[int]("item", 1, cm.Config[int]())
	r := root(t, cm.RootSpec{Keys: []cm.AnyKey{key.Any()}})
	foreign := root(t, cm.RootSpec{Keys: []cm.AnyKey{key.Any()}})
	host, err := r.HostContext(cm.Spec{})
	host = must(t, host, err)
	other, err := foreign.HostContext(cm.Spec{})
	other = must(t, other, err)
	if host.Entry() == other.Entry() || host.Entry().Name() != other.Entry().Name() {
		t.Fatal("independent roots shared a host identity")
	}
	_, err = foreign.WithEntry(foreign.Context(), host.Entry(), cm.Spec{})
	wantError(t, err, cm.ErrEntry)
	_, err = r.WithEntry(r.Context(), other.Entry(), cm.Spec{})
	wantError(t, err, cm.ErrEntry)
	_, err = r.WithEntry(other, host.Entry(), cm.Spec{})
	wantError(t, err, cm.ErrContext)
	for _, invalid := range []*cm.Root{nil, {}} {
		_, err = invalid.HostContext(cm.Spec{})
		wantError(t, err, cm.ErrContext)
	}
	for _, test := range []struct {
		spec cm.Spec
		err  error
	}{
		{cm.Spec{Provides: []cm.AnyKey{cm.NewKey[int]("missing", 1).Any()}}, cm.ErrKey},
		{cm.Spec{Provides: []cm.AnyKey{cm.NewKey[string]("item", 1).Any()}}, cm.ErrCollision},
		{cm.Spec{Provides: []cm.AnyKey{key.Any(), key.Any()}}, cm.ErrDuplicate},
		{cm.Spec{Inject: []cm.Dep{cm.Need(key), cm.Need(key)}}, cm.ErrDuplicate},
		{cm.Spec{Inject: []cm.Dep{cm.NeedWith(key, "wrong")}}, cm.ErrConfig},
	} {
		_, err = r.HostContext(test.spec)
		wantError(t, err, test.err)
	}
}

func TestLifecycleFrontendMetadataBridges(t *testing.T) {
	entry := cm.NewEntry("unit")
	key := cm.NewKey[int]("item", 0)
	r := root(t, cm.RootSpec{Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{key.Any()}})
	required := attach(t, r, r.Context(), entry, cm.Spec{Inject: []cm.Dep{cm.Need(key)}})
	if err := required.AuthorizeRead(key.Any(), false); err != nil {
		t.Fatal(err)
	}
	base, err := required.Realm(key.Any())
	if err != nil || base != (cm.Label{}) {
		t.Fatalf("root realm = %v %v", base, err)
	}
	label := cm.NewLabel("joined")
	private := isolated(t, required, key.Any(), label)
	actual, err := private.Realm(key.Any())
	if err != nil || actual != label {
		t.Fatalf("opaque realm identity was not preserved: %v", err)
	}
	wantError(t, private.AuthorizeRead(key.Any(), false), cm.ErrUndeclared)
	optional := attach(t, r, r.Context(), entry, cm.Spec{Inject: []cm.Dep{cm.Want(key)}})
	wantError(t, optional.AuthorizeRead(key.Any(), false), cm.ErrUndeclared)
	if err := optional.AuthorizeRead(key.Any(), true); err != nil {
		t.Fatal(err)
	}
	var invalid *cm.Context
	_, err = invalid.Realm(key.Any())
	wantError(t, err, cm.ErrContext)
	wantError(t, invalid.AuthorizeRead(key.Any(), false), cm.ErrContext)
	_, err = required.Realm(cm.NewKey[string]("item", 0).Any())
	wantError(t, err, cm.ErrCollision)
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

func TestImmutableValuesInheritAndShadowWithoutExposingContainers(t *testing.T) {
	entry := cm.NewEntry("unit")
	r := root(t, cm.RootSpec{Entries: []cm.Plugin{entry}})
	base := attach(t, r, r.Context(), entry, cm.Spec{ID: "base"})
	payload := &struct{ Name string }{"immutable"}
	input := map[string]any{"base": 1, "shared": "ancestor", "payload": payload, "": "empty", "nil": "ancestor"}
	ancestor := base.WithValues(input)
	input["base"], input["added"] = 0, true
	delete(input, "")
	leaf := child(t, ancestor.WithValues(map[string]any{"shared": "leaf", "nil": nil}), cm.Spec{ID: "child"}).Extend()
	expected := map[string]any{"base": 1, "shared": "leaf", "payload": payload, "": "empty", "nil": nil}
	if got := leaf.Values(); !reflect.DeepEqual(got, expected) {
		t.Fatalf("merged values = %v, want %v", got, expected)
	}
	for name, expectedValue := range expected {
		if got, exists := leaf.Value(name); !exists || !reflect.DeepEqual(got, expectedValue) {
			t.Fatalf("Value(%q) = %v, %v; want %v", name, got, exists, expectedValue)
		}
	}
	if got, _ := leaf.Value("payload"); got != payload {
		t.Fatal("arbitrary payload was cloned rather than retained")
	}
	if got, exists := leaf.Value("missing"); exists || got != nil {
		t.Fatal("missing value was confused with a stored nil")
	}
	if _, exists := leaf.Value("added"); exists {
		t.Fatal("WithValues retained caller-owned map storage")
	}
	copy := leaf.Values()
	copy["base"], copy["new"] = 0, true
	delete(copy, "shared")
	if !reflect.DeepEqual(leaf.Values(), expected) {
		t.Fatal("Values exposed an internal container")
	}
	if got, _ := ancestor.Value("shared"); got != "ancestor" {
		t.Fatal("descendant shadowing mutated its ancestor")
	}
	sibling := ancestor.WithValues(map[string]any{"shared": "sibling"})
	if got, _ := sibling.Value("shared"); got != "sibling" || !reflect.DeepEqual(leaf.Values(), expected) {
		t.Fatal("sibling values leaked across branches")
	}
	for _, values := range []map[string]any{nil, {}} {
		extended := leaf.WithValues(values)
		if extended.Parent() != leaf || extended.Entry() != leaf.Entry() || !reflect.DeepEqual(extended.Spec(), leaf.Spec()) || !reflect.DeepEqual(extended.Values(), expected) {
			t.Fatal("nil/empty values did not behave like Extend")
		}
	}
	empty := base.Values()
	empty["base"] = 0
	if len(base.Values()) != 0 || len(r.Context().Values()) != 0 {
		t.Fatal("metadata reached an ancestor or empty Values exposed storage")
	}
}

func TestValuesCannotMintEntryDeclarationRealmOrScopeAuthority(t *testing.T) {
	owner, reader, outsider := cm.NewEntry("owner"), cm.NewEntry("reader"), cm.NewEntry("outsider")
	key := cm.NewKey[int]("item", 1, cm.Classes(cm.ProviderBound, cm.ConsumerBound), cm.Config[string]())
	r := root(t, cm.RootSpec{Entries: []cm.Plugin{owner, reader, outsider}, Keys: []cm.AnyKey{key.Any()}, Bindings: []cm.Binding{{Key: key.Any(), Providers: []cm.Plugin{owner}, Consumers: []cm.Plugin{reader}}}})
	provider := attach(t, r, r.Context(), owner, cm.Spec{Provides: []cm.AnyKey{key.Any()}})
	label := cm.NewLabel("actual")
	scope := cm.NewScope[string](key.Any())
	scoped, err := scope.With(isolated(t, provider.WithValues(map[string]any{"flow": "metadata"}), key.Any(), label), "trusted")
	scoped = must(t, scoped, err)
	consumer := attach(t, r, scoped, reader, cm.Spec{ID: "actual", Inject: []cm.Dep{cm.NeedWith(key, "actual")}})
	claims := map[string]any{
		"entry": owner, "frame": provider.Spec(), "frames": []cm.Spec{provider.Spec()},
		"isolation": cm.NewLabel("claimed"), "intercepts": "claimed", "data": cm.RootSpec{},
		"scope": scope, "scopes": "claimed", "parent": r.Context(), "hook/id": owner.Name(),
	}
	derived := consumer.WithValues(claims)
	if derived.Parent() != consumer || derived.Entry() != reader || !reflect.DeepEqual(derived.Spec(), consumer.Spec()) {
		t.Fatal("value names overwrote privileged context fields")
	}
	if err := derived.AuthorizeRead(key.Any(), false); err != nil {
		t.Fatal(err)
	}
	if realm, err := derived.Realm(key.Any()); err != nil || realm != label {
		t.Fatalf("values changed actual realm = %v, %v", realm, err)
	}
	if got := cm.Intercepts[string](derived, key.Any()); !reflect.DeepEqual(got, []string{"actual"}) {
		t.Fatalf("values changed intercepts = %v", got)
	}
	if value, exists := scope.From(derived); !exists || value != "trusted" || (cm.Audience{}).Matches(derived) {
		t.Fatal("values changed inherited scope or audience")
	}
	if value, exists := derived.Value("flow"); !exists || value != "metadata" {
		t.Fatal("scope derivation hid generic metadata")
	}
	wantError(t, derived.AuthorizeProvide(key.Any()), cm.ErrBinding)
	_, err = scope.With(derived, "claimed")
	wantError(t, err, cm.ErrScope)
	_, err = derived.Isolate(key.Any())
	wantError(t, err, cm.ErrIsolation)
	unbound := attach(t, r, derived, outsider, cm.Spec{}).WithValues(claims)
	wantError(t, unbound.AuthorizeRead(key.Any(), false), cm.ErrBinding)
	undeclared := attach(t, r, r.Context(), reader, cm.Spec{}).WithValues(map[string]any{"frame": consumer.Spec(), "hook/id": "actual"})
	wantError(t, undeclared.AuthorizeRead(key.Any(), false), cm.ErrUndeclared)
	if _, exists := scope.From(undeclared); exists || !(cm.Audience{}).Matches(undeclared.WithValues(claims)) {
		t.Fatal("generic values minted a private scope")
	}
	noOwner := child(t, provider, cm.Spec{}).WithValues(claims)
	wantError(t, noOwner.AuthorizeProvide(key.Any()), cm.ErrUndeclared)
	_, err = scope.With(noOwner, "claimed")
	wantError(t, err, cm.ErrScope)
}

func TestAvailabilityUsesActualConsumerValuesWithoutGrantingAuthority(t *testing.T) {
	provider, reader, outsider := cm.NewEntry("provider"), cm.NewEntry("reader"), cm.NewEntry("outsider")
	key := cm.NewKey[string]("item", 1, cm.Classes(cm.Open, cm.ConsumerBound))
	r := root(t, cm.RootSpec{Entries: []cm.Plugin{provider, reader, outsider}, Keys: []cm.AnyKey{key.Any()}, Bindings: []cm.Binding{{Key: key.Any(), Consumers: []cm.Plugin{reader}}}})
	p := attach(t, r, r.Context(), provider, cm.Spec{Provides: []cm.AnyKey{key.Any()}})
	base := attach(t, r, r.Context(), reader, cm.Spec{Inject: []cm.Dep{cm.Need(key)}}).WithValues(map[string]any{"hook/id": "base"})
	first, second := base.WithValues(map[string]any{"hook/id": "first"}), base.WithValues(map[string]any{"hook/id": "second"})
	var checked, viewed []*cm.Context
	provision, err := cm.ProvideFor(p, key, func(c *cm.Context) string {
		viewed = append(viewed, c)
		value, _ := c.Value("hook/id")
		return value.(string)
	}, func(c *cm.Context) bool {
		checked = append(checked, c)
		value, _ := c.Value("hook/id")
		return value == "first"
	})
	s := snapshot(t, r, must(t, provision, err))
	ready, err := s.Available(first, key.Any())
	if err != nil || !ready {
		t.Fatalf("first availability = %v, %v", ready, err)
	}
	resolves(t, s, first, key, "first")
	ready, err = s.Available(second, key.Any())
	if err != nil || ready {
		t.Fatalf("second availability = %v, %v", ready, err)
	}
	_, err = cm.Resolve(s, second, key)
	wantError(t, err, cm.ErrUnavailable)
	descendant := child(t, first, cm.Spec{}).Extend()
	resolves(t, s, descendant, key, "first")
	unbound := attach(t, r, first, outsider, cm.Spec{}).WithValues(map[string]any{"hook/id": "first", "entry": reader})
	_, err = s.Available(unbound, key.Any())
	wantError(t, err, cm.ErrBinding)
	_, err = cm.Resolve(s, unbound, key)
	wantError(t, err, cm.ErrBinding)
	if !reflect.DeepEqual(checked, []*cm.Context{first, first, second, second, descendant}) || !reflect.DeepEqual(viewed, []*cm.Context{first, descendant}) {
		t.Fatal("metadata callbacks used a substitute consumer or ran without authorization")
	}
	if value, _ := base.Value("hook/id"); value != "base" {
		t.Fatal("consumer values mutated the source context")
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
	c = c.WithValues(map[string]any{"base": "immutable", "delta": 0})
	p, err := cm.ProvideFor(c, key, func(caller *cm.Context) int {
		value, _ := scope.From(caller)
		delta, _ := caller.Value("delta")
		return value + cm.Intercepts[int](caller, key.Any())[0] + delta.(int)
	}, func(caller *cm.Context) bool { _, exists := scope.From(caller); return exists })
	p = must(t, p, err)
	s := snapshot(t, r, p)
	var group sync.WaitGroup
	for delta := range 16 {
		group.Go(func() {
			for range 50 {
				input := map[string]any{"delta": delta}
				derived := c.WithValues(input).Extend()
				input["delta"] = -1
				copy := derived.Values()
				copy["delta"] = -2
				if value, exists := derived.Value("delta"); !exists || value != delta || derived.Values()["base"] != "immutable" || c.Values()["delta"] != 0 {
					t.Error("parallel values exposed storage or leaked across branches")
					return
				}
				got, err := cm.Resolve(s, derived, key)
				if err != nil || got != 4+delta {
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
