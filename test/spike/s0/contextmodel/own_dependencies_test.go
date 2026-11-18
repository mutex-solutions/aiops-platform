// SPDX-License-Identifier: Apache-2.0

package contextmodel_test

import (
	"reflect"
	"testing"

	cm "github.com/mutex-solutions/aiops-platform/test/spike/s0/contextmodel"
)

// Dependencies is the effective list used to authorise reads; OwnDependencies is
// what the fiber itself declared, which is all the oracle lists as its inject
// (a child reaches a parent's injection by walking up, within one realm).
func TestOwnDependenciesExcludeInheritedDeclarations(t *testing.T) {
	entry := cm.NewEntry("unit")
	a, b := cm.NewKey[string]("a", 1), cm.NewKey[string]("b", 1)
	r := root(t, cm.RootSpec{Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{a.Any(), b.Any()}})
	parent := attach(t, r, r.Context(), entry, cm.Spec{Inject: []cm.Dep{cm.Need(a)}})
	plain := child(t, parent, cm.Spec{})
	mixed := child(t, parent, cm.Spec{Inject: []cm.Dep{cm.Need(b)}})
	keys := func(deps []cm.Dep) []string {
		var out []string
		for _, dep := range deps {
			out = append(out, dep.Key().Name())
		}
		return out
	}
	if got := keys(parent.OwnDependencies()); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("parent own = %v", got)
	}
	if got := keys(plain.OwnDependencies()); len(got) != 0 {
		t.Fatalf("child without declarations owns %v", got)
	}
	if got := keys(plain.Dependencies()); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("effective list must still carry the inherited declaration: %v", got)
	}
	if got := keys(mixed.OwnDependencies()); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("mixed own = %v", got)
	}
	if got := keys(mixed.Dependencies()); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("mixed effective = %v", got)
	}
	// A derived context shares its frame's declarations; an isolated realm for
	// the key drops the declaration from both views, as it does for reads.
	if got := keys(mixed.Extend().OwnDependencies()); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("extended own = %v", got)
	}
	private := isolated(t, mixed, b.Any(), cm.NewLabel("private"))
	if got := keys(private.OwnDependencies()); len(got) != 0 {
		t.Fatalf("isolated realm kept a declaration: %v", got)
	}
}
