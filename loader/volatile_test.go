// SPDX-License-Identifier: Apache-2.0

package loader

import (
	"slices"
	"testing"
)

type m = map[string]any
type l = []any

func TestVolatileOnly(t *testing.T) {
	vol := []string{"window", "a.b", "rules.*.url", "list", "*.x"}
	for _, tc := range []struct {
		name     string
		old, new any
		paths    []string
		only     bool
	}{
		{"identical", m{"window": 1}, m{"window": 1}, nil, true},
		{"nil trees", nil, nil, nil, true},
		{"scalar", m{"window": 1}, m{"window": 2}, []string{"window"}, true},
		{"not listed", m{"name": "a"}, m{"name": "b"}, []string{"name"}, false},
		{"listed and not", m{"window": 1, "name": "a"}, m{"window": 2, "name": "b"}, []string{"name", "window"}, false},
		{"nested", m{"a": m{"b": 1, "c": 1}}, m{"a": m{"b": 2, "c": 1}}, []string{"a.b"}, true},
		{"nested sibling", m{"a": m{"b": 1, "c": 1}}, m{"a": m{"b": 1, "c": 2}}, []string{"a.c"}, false},
		{"key added", m{}, m{"window": 1}, []string{"window"}, true},
		{"key removed", m{"window": 1, "name": "a"}, m{"window": 1}, []string{"name"}, false},
		{"nested key added", m{"a": m{}}, m{"a": m{"b": 1}}, []string{"a.b"}, true},
		{"a covered path covers its subtree", m{"list": l{1, 2}}, m{"list": l{3, 4, 5}}, []string{"list"}, true},
		{"subtree of a covered path", m{"list": m{"deep": m{"x": 1}}}, m{"list": m{"deep": m{"x": 2}}}, []string{"list.deep.x"}, true},
		{"wildcard over list indexes", m{"rules": l{m{"url": "a"}, m{"url": "b"}}}, m{"rules": l{m{"url": "a"}, m{"url": "c"}}}, []string{"rules.1.url"}, true},
		{"wildcard over other fields", m{"rules": l{m{"url": "a", "id": 1}}}, m{"rules": l{m{"url": "a", "id": 2}}}, []string{"rules.0.id"}, false},
		{"list grows", m{"rules": l{m{"url": "a"}}}, m{"rules": l{m{"url": "a"}, m{"url": "b"}}}, []string{"rules"}, false},
		{"list shrinks", m{"list": l{1, 2}}, m{"list": l{1}}, []string{"list"}, true},
		{"element type changes", m{"rules": l{m{"url": "a"}}}, m{"rules": l{"x"}}, []string{"rules.0"}, false},
		{"wildcard over map keys", m{"k": m{"x": 1}, "j": m{"x": 1}}, m{"k": m{"x": 2}, "j": m{"x": 3}}, []string{"j.x", "k.x"}, true},
		{"type change under a volatile path", m{"window": 1}, m{"window": "1"}, []string{"window"}, true},
		{"int to float is a change", m{"window": int64(1)}, m{"window": float64(1)}, []string{"window"}, true},
		{"map becomes scalar above a volatile field", m{"a": m{"b": 1}}, m{"a": 1}, []string{"a"}, false},
		{"scalar becomes map", m{"a": 1}, m{"a": m{"b": 1}}, []string{"a"}, false},
		{"map becomes list", m{"list": m{}}, m{"list": l{}}, []string{"list"}, true},
		{"root type changes", m{"window": 1}, l{1}, []string{""}, false},
		{"root becomes nil", m{"window": 1}, nil, []string{""}, false},
		{"unplain values compare deeply", m{"window": []int{1}}, m{"window": []int{2}}, []string{"window"}, true},
		{"a dotted key is covered by an ancestor only", m{"list": m{"a.b": 1}}, m{"list": m{"a.b": 2}}, []string{"list.a.b"}, true},
		{"a dotted key is not named by a path", m{"a.b": 1}, m{"a.b": 2}, []string{"a.b"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths, only := volatileOnly(tc.old, tc.new, vol)
			if !slices.Equal(paths, tc.paths) || only != tc.only {
				t.Errorf("volatileOnly = %q, %v; want %q, %v", paths, only, tc.paths, tc.only)
			}
		})
	}
	if _, only := volatileOnly(m{"window": 1}, m{"window": 2}, nil); only {
		t.Error("with no volatile path nothing is volatile")
	}
}
