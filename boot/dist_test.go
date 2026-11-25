package boot

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseDistOK(t *testing.T) {
	d, err := ParseDist("dist.yaml", []byte(`
modules: [example.test/m/b, example.test/m/a]
providers: {z: [o/b, o/a], a: [o/c]}
consumers: {k: [o/x]}
sealedHooks: {approval/gates: [o/a]}
requiredKeys: [sql, jobs]
requiredRows: [loader, g:child]
`))
	if err != nil {
		t.Fatal(err)
	}
	want := DistManifest{
		Modules:      []string{"example.test/m/a", "example.test/m/b"},
		Providers:    []Binding{{"a", []string{"o/c"}}, {"z", []string{"o/a", "o/b"}}},
		Consumers:    []Binding{{"k", []string{"o/x"}}},
		SealedHooks:  []HookListeners{{"approval/gates", []string{"o/a"}}},
		RequiredKeys: []string{"jobs", "sql"},
		RequiredRows: []string{"g:child", "loader"},
	}
	if !reflect.DeepEqual(d, want) {
		t.Errorf("got %+v\nwant %+v", d, want)
	}
}

func TestParseDistRefusals(t *testing.T) {
	dist := []struct{ name, in string }{
		{"empty file", ""},
		{"not a mapping", "[a]"},
		{"unknown field", "x: 1"},
		{"modules not a list", "modules: example.test/m"},
		{"module malformed", "modules: ['a b']"},
		{"module twice", "modules: [example.test/m, example.test/m]"},
		{"providers not a mapping", "providers: [a]"},
		{"binding key malformed", "providers: {'a b': [o/x]}"},
		{"binding not a list", "providers: {a: o/x}"},
		{"binding id malformed", "providers: {a: [nonsense]}"},
		{"binding id is a core plugin", "providers: {a: [core:group]}"},
		{"binding id twice", "providers: {a: [o/x, o/x]}"},
		{"binding id not a string", "providers: {a: [1]}"},
		{"sealed hook name malformed", "sealedHooks: {'/x': [o/a]}"},
		{"required keys not a list", "requiredKeys: sql"},
		{"required key malformed", "requiredKeys: ['a b']"},
		{"required row malformed", "requiredRows: ['a b']"},
		{"duplicate key", "requiredKeys: []\nrequiredKeys: []"},
		{"alias", "requiredKeys: &a []\nrequiredRows: *a"},
		{"two documents", "{}\n---\n{}"},
	}
	for _, tc := range dist {
		t.Run("dist "+tc.name, func(t *testing.T) {
			_, err := ParseDist("dist.yaml", []byte(tc.in))
			var ce *Error
			if !errors.As(err, &ce) || ce.File != "dist.yaml" {
				t.Fatalf("want a typed error naming the file, got %v", err)
			}
		})
	}
}

func TestOkHookAndPath(t *testing.T) {
	for s, want := range map[string]bool{
		"approval/gates": true, "tenant/create": true, "a.b-c_d": true, "": false, "/x": false, ".x": false,
		"a b": false, "a$": false, strings.Repeat("a", 129): false, strings.Repeat("a", 128): true,
	} {
		if okHook(s) != want {
			t.Errorf("okHook(%q) != %v", s, want)
		}
	}
	for s, want := range map[string]bool{"a.b[0]": true, "": false, "a b": false, "a\x01": false, "a\x7f": false, strings.Repeat("a", 257): false} {
		if okPath(s) != want {
			t.Errorf("okPath(%q) != %v", s, want)
		}
	}
}

// FuzzReadDist feeds arbitrary bytes to the reader of dist.yaml. It may not panic,
// an error is always the typed *Error, and what is read is deterministic and already
// sorted.
func FuzzReadDist(f *testing.F) {
	for _, s := range []string{
		"", "{}", "[]", "providers: {k: [o/a]}\nconsumers: {k: [o/b]}\nsealedHooks: {h/x: [o/a]}\nrequiredKeys: [a]\nrequiredRows: [r, g:c]",
		"providers: &a {}\nconsumers: *a", "\x00\xff", "[[[[", "a: !!str b", "providers: {a: [o/x, o/x]}", "{a: 1, a: 2}",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := ParseDist("dist.yaml", data)
		if err != nil {
			var ce *Error
			if !errors.As(err, &ce) {
				t.Fatalf("untyped error %T: %v", err, err)
			}
		} else {
			d2, _ := ParseDist("dist.yaml", data)
			if !reflect.DeepEqual(d, d2) {
				t.Fatal("not deterministic")
			}
			for _, b := range append(append([]Binding(nil), d.Providers...), d.Consumers...) {
				if !isSorted(b.Plugins) || !utf8.ValidString(b.Key) {
					t.Fatalf("binding not sorted: %+v", b)
				}
			}
			if !isSorted(d.RequiredKeys) || !isSorted(d.RequiredRows) {
				t.Fatal("required sets not sorted")
			}
		}
	})
}

func isSorted(ss []string) bool {
	for i := 1; i < len(ss); i++ {
		if ss[i-1] >= ss[i] {
			return false
		}
	}
	return true
}
