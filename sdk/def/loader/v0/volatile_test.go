// SPDX-License-Identifier: Apache-2.0

package loader_test

import (
	"reflect"
	"slices"
	"testing"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	loader "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
)

type m = map[string]any
type l = []any

func TestVolatileOnly(t *testing.T) {
	vol := []string{"window", "a.b", "rules.*.url", "list", "*.x"}
	for _, tc := range []struct {
		name     string
		old, new any
		paths    [][]string
		only     bool
	}{
		{"identical", m{"window": 1}, m{"window": 1}, nil, true},
		{"nil trees", nil, nil, nil, true},
		{"scalar", m{"window": 1}, m{"window": 2}, [][]string{{"window"}}, true},
		{"not listed", m{"name": "a"}, m{"name": "b"}, [][]string{{"name"}}, false},
		{"listed and not", m{"window": 1, "name": "a"}, m{"window": 2, "name": "b"}, [][]string{{"name"}, {"window"}}, false},
		{"nested", m{"a": m{"b": 1, "c": 1}}, m{"a": m{"b": 2, "c": 1}}, [][]string{{"a", "b"}}, true},
		{"nested sibling", m{"a": m{"b": 1, "c": 1}}, m{"a": m{"b": 1, "c": 2}}, [][]string{{"a", "c"}}, false},
		{"key added", m{}, m{"window": 1}, [][]string{{"window"}}, true},
		{"key removed", m{"window": 1, "name": "a"}, m{"window": 1}, [][]string{{"name"}}, false},
		{"nested key added", m{"a": m{}}, m{"a": m{"b": 1}}, [][]string{{"a", "b"}}, true},
		{"a covered path covers its subtree", m{"list": l{1, 2}}, m{"list": l{3, 4, 5}}, [][]string{{"list"}}, true},
		{"subtree of a covered path", m{"list": m{"deep": m{"x": 1}}}, m{"list": m{"deep": m{"x": 2}}}, [][]string{{"list", "deep", "x"}}, true},
		{"wildcard over list indexes", m{"rules": l{m{"url": "a"}, m{"url": "b"}}}, m{"rules": l{m{"url": "a"}, m{"url": "c"}}}, [][]string{{"rules", "1", "url"}}, true},
		{"wildcard over other fields", m{"rules": l{m{"url": "a", "id": 1}}}, m{"rules": l{m{"url": "a", "id": 2}}}, [][]string{{"rules", "0", "id"}}, false},
		{"list grows", m{"rules": l{m{"url": "a"}}}, m{"rules": l{m{"url": "a"}, m{"url": "b"}}}, [][]string{{"rules"}}, false},
		{"list shrinks", m{"list": l{1, 2}}, m{"list": l{1}}, [][]string{{"list"}}, true},
		{"element type changes", m{"rules": l{m{"url": "a"}}}, m{"rules": l{"x"}}, [][]string{{"rules", "0"}}, false},
		{"wildcard over map keys", m{"k": m{"x": 1}, "j": m{"x": 1}}, m{"k": m{"x": 2}, "j": m{"x": 3}}, [][]string{{"j", "x"}, {"k", "x"}}, true},
		{"type change under a volatile path", m{"window": 1}, m{"window": "1"}, [][]string{{"window"}}, true},
		{"int to float is a change", m{"window": int64(1)}, m{"window": float64(1)}, [][]string{{"window"}}, true},
		{"map becomes scalar above a volatile field", m{"a": m{"b": 1}}, m{"a": 1}, [][]string{{"a"}}, false},
		{"scalar becomes map", m{"a": 1}, m{"a": m{"b": 1}}, [][]string{{"a"}}, false},
		{"map becomes list", m{"list": m{}}, m{"list": l{}}, [][]string{{"list"}}, true},
		{"root type changes", m{"window": 1}, l{1}, [][]string{{}}, false},
		{"root becomes nil", m{"window": 1}, nil, [][]string{{}}, false},
		{"unplain values compare deeply", m{"window": []int{1}}, m{"window": []int{2}}, [][]string{{"window"}}, true},
		{"a dotted key is covered by an ancestor only", m{"list": m{"a.b": 1}}, m{"list": m{"a.b": 2}}, [][]string{{"list", "a.b"}}, true},
		{"a dotted key is not named by a path", m{"a.b": 1}, m{"a.b": 2}, [][]string{{"a.b"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths, only := loader.VolatileOnly(tc.old, tc.new, vol)
			if !slices.EqualFunc(paths, tc.paths, slices.Equal[[]string]) || only != tc.only {
				t.Errorf("VolatileOnly = %q, %v; want %q, %v", paths, only, tc.paths, tc.only)
			}
		})
	}
	if _, only := loader.VolatileOnly(m{"window": 1}, m{"window": 2}, nil); only {
		t.Error("with no volatile path nothing is volatile")
	}
}

// A path is its list of keys, so a key that holds a dot stays one key: a change of the
// label "k8s.pod" and a change of the field pod of the map k8s have different paths.
func TestVolatileOnlyPathsKeepADottedKey(t *testing.T) {
	dotted, _ := loader.VolatileOnly(m{"labels": m{"k8s.pod": "a"}}, m{"labels": m{"k8s.pod": "b"}}, nil)
	nested, _ := loader.VolatileOnly(m{"labels": m{"k8s": m{"pod": "a"}}}, m{"labels": m{"k8s": m{"pod": "b"}}}, nil)
	if reflect.DeepEqual(dotted, nested) {
		t.Errorf("a dotted key and a nested field have the same paths: %q", dotted)
	}
}

// A change whose old or new value holds a reference is not volatile (ADR-059 decision
// 17): the tree holds it unresolved, and only config-refs resolves it, at an
// activation. A change of a literal beside a reference that did not change is.
func TestVolatileOnlyRefusesAReference(t *testing.T) {
	ref := launch.Ref{Scheme: "env", Name: "AIOPS_REF_A"}
	vol := []string{"note", "rules"}
	for i, tc := range []struct{ old, new m }{
		{m{"note": "plain"}, m{"note": ref}},                                        // becomes a reference
		{m{"note": ref}, m{"note": "plain"}},                                        // was one
		{m{"rules": l{m{"url": "a"}}}, m{"rules": l{m{"url": "a"}, m{"url": ref}}}}, // holds one beneath
		{m{"rules": l{}}, m{"rules": l{}, "note": m{"k": l{ref}}}},                  // a new key holds one
		{m{"note": "a", "x": ref}, m{"note": "b"}},                                  // a removed value held one
	} {
		if paths, only := loader.VolatileOnly(tc.old, tc.new, vol); only || len(paths) == 0 {
			t.Errorf("case %d: VolatileOnly = %q, %v; want not volatile", i, paths, only)
		}
	}
	if paths, only := loader.VolatileOnly(m{"note": ref}, m{"note": ref}, vol); !only || len(paths) != 0 {
		t.Error("an unchanged reference is no change")
	}
	beside := []struct{ old, new m }{
		{m{"note": "a", "rules": l{ref}}, m{"note": "b", "rules": l{ref}}},               // a volatile field beside it
		{m{"note": "a", "x": m{"y": ref}}, m{"note": "b", "x": m{"y": ref}}},             // a field no volatile path covers
		{m{"rules": l{m{"url": "a", "k": ref}}}, m{"rules": l{m{"url": "b", "k": ref}}}}, // a sibling beneath
	}
	for i, tc := range beside {
		if paths, only := loader.VolatileOnly(tc.old, tc.new, vol); !only || len(paths) != 1 {
			t.Errorf("beside %d: VolatileOnly = %q, %v; want one volatile path", i, paths, only)
		}
	}
}

// WithoutRefs is the Config of loader/volatile-update: the tree with every reference
// replaced by nil, so the event never carries one, and nothing else changed.
func TestWithoutRefs(t *testing.T) {
	ref := launch.Ref{Scheme: "env", Name: "AIOPS_REF_A"}
	in := m{"note": "b", "k": ref, "l": l{1, ref, m{"r": ref, "s": "x"}}}
	got := loader.WithoutRefs(in)
	want := m{"note": "b", "k": nil, "l": l{1, nil, m{"r": nil, "s": "x"}}}
	if !reflect.DeepEqual(got, want) || loader.HoldsRef(got) || !loader.HoldsRef(in) {
		t.Errorf("WithoutRefs(%v) = %v, want %v and the input as it was", in, got, want)
	}
	for _, v := range []any{nil, "x", m{"a": l{1}}} {
		if g := loader.WithoutRefs(v); !reflect.DeepEqual(g, v) {
			t.Errorf("WithoutRefs(%v) = %v", v, g)
		}
	}
}

func TestHoldsRef(t *testing.T) {
	ref := launch.Ref{Scheme: "env", Name: "AIOPS_REF_A"}
	for _, v := range []any{ref, m{"a": ref}, l{1, m{"b": l{ref}}}} {
		if !loader.HoldsRef(v) {
			t.Errorf("HoldsRef(%v) = false", v)
		}
	}
	for _, v := range []any{nil, "${env:X}", m{"a": l{1, "x"}}} {
		if loader.HoldsRef(v) {
			t.Errorf("HoldsRef(%v) = true", v)
		}
	}
}
