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

func TestParseModulesOK(t *testing.T) {
	m, err := ParseModules("modules.yaml", []byte(`
o/a:
  module: example.test/a
  version: v1
  sum: "h1:x"
  provides: [b, a]
  needs: [n]
  wants: [w]
  volatile: [timeout, config.auth, "config[0]"]
  hooks:
    - {hook: z/hook, stage: 2, name: late}
    - {hook: a.hook, stage: 0}
  classes: {providerBound: [b], consumerBound: [n, a], sealedHooks: [z/hook]}
`))
	if err != nil {
		t.Fatal(err)
	}
	want := ModuleEntry{ID: "o/a", Module: "example.test/a", Version: "v1", Sum: "h1:x",
		Provides: []string{"a", "b"}, Needs: []string{"n"}, Wants: []string{"w"},
		Volatile: []string{"config.auth", "config.timeout", "config[0]"},
		Hooks:    []HookDecl{{"a.hook", 0, ""}, {"z/hook", 2, "late"}},
		Classes:  &ModuleClasses{ProviderBound: []string{"b"}, ConsumerBound: []string{"a", "n"}, SealedHooks: []string{"z/hook"}}}
	if !reflect.DeepEqual(m["o/a"], want) {
		t.Errorf("got %+v\nwant %+v", m["o/a"], want)
	}
	if e, err := ParseModules("m.yaml", []byte("{}")); err != nil || len(e) != 0 {
		t.Errorf("empty table: %v %v", e, err)
	}
	// Hook entries are keyed by hook and stage, not by hook alone, as the kernel and the
	// manifest key them (ADR-048 decisions 10 and 18): one hook at two stages is two
	// entries, sorted by hook and stage.
	two, err := ParseModules("modules.yaml", []byte("o/b:\n  hooks:\n"+
		"    - {hook: reload/operation, stage: 1, name: late}\n    - {hook: reload/operation, stage: 0, name: default}\n"))
	if want := []HookDecl{{"reload/operation", 0, "default"}, {"reload/operation", 1, "late"}}; err != nil || !reflect.DeepEqual(two["o/b"].Hooks, want) {
		t.Errorf("one hook at two stages: %+v, %v; want %+v", two["o/b"].Hooks, err, want)
	}
}

func TestParseDistAndModulesRefusals(t *testing.T) {
	dist := []struct{ name, in string }{
		{"empty file", ""},
		{"not a mapping", "[a]"},
		{"unknown field", "x: 1"},
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
	mods := []struct{ name, in string }{
		{"empty file", ""},
		{"not a mapping", "[a]"},
		{"id malformed", "nonsense: {}"},
		{"id is a core plugin", "core:group: {}"},
		{"entry not a mapping", "o/a: []"},
		{"unknown field", "o/a: {x: 1}"},
		{"module not a string", "o/a: {module: [x]}"},
		{"provides not a list", "o/a: {provides: a}"},
		{"provides malformed", "o/a: {provides: ['a b']}"},
		{"volatile malformed", "o/a: {volatile: ['a b']}"},
		{"hooks not a list", "o/a: {hooks: x}"},
		{"hooks alias", "o/a: {hooks: &h []}\no/b: {hooks: *h}"},
		{"hook not a mapping", "o/a: {hooks: [x]}"},
		{"hook without a name", "o/a: {hooks: [{stage: 1}]}"},
		{"hook name malformed", "o/a: {hooks: [{hook: 'a b'}]}"},
		{"hook name not a string", "o/a: {hooks: [{hook: [x]}]}"},
		{"hook stage not a number", "o/a: {hooks: [{hook: h, stage: late}]}"},
		{"hook stage negative", "o/a: {hooks: [{hook: h, stage: -1}]}"},
		{"hook stage a list", "o/a: {hooks: [{hook: h, stage: [1]}]}"},
		{"hook stage name not a string", "o/a: {hooks: [{hook: h, name: [x]}]}"},
		{"hook unknown field", "o/a: {hooks: [{hook: h, x: 1}]}"},
		{"hook twice", "o/a: {hooks: [{hook: h}, {hook: h}]}"},
		{"classes not a mapping", "o/a: {classes: [x]}"},
		{"classes unknown field", "o/a: {classes: {open: [x]}}"},
		{"provider-bound key malformed", "o/a: {classes: {providerBound: ['a b']}}"},
		{"consumer-bound key not a list", "o/a: {classes: {consumerBound: x}}"},
		{"sealed hook malformed", "o/a: {classes: {sealedHooks: ['/x']}}"},
	}
	for _, tc := range mods {
		t.Run("modules "+tc.name, func(t *testing.T) {
			_, err := ParseModules("modules.yaml", []byte(tc.in))
			var ce *Error
			if !errors.As(err, &ce) || ce.File != "modules.yaml" {
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

// FuzzReadDist feeds arbitrary bytes to both readers. Neither may panic, an
// error is always the typed *Error, and what is read is deterministic and
// already sorted.
func FuzzReadDist(f *testing.F) {
	for _, s := range []string{
		"", "{}", "[]", "providers: {k: [o/a]}\nconsumers: {k: [o/b]}\nsealedHooks: {h/x: [o/a]}\nrequiredKeys: [a]\nrequiredRows: [r, g:c]",
		"o/a:\n  provides: [a]\n  needs: [b]\n  volatile: [x.y]\n  hooks: [{hook: h/x, stage: 3, name: s}]",
		"o/a: {module: m, version: v, sum: s, wants: [w]}", "providers: &a {}\nconsumers: *a", "o/a: {hooks: [{hook: h, stage: 99999999999}]}",
		"\x00\xff", "[[[[", "a: !!str b", "providers: {a: [o/x, o/x]}", "{a: 1, a: 2}",
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
		m, err := ParseModules("modules.yaml", data)
		if err != nil {
			var ce *Error
			if !errors.As(err, &ce) {
				t.Fatalf("untyped error %T: %v", err, err)
			}
			return
		}
		for id, e := range m {
			if e.ID != id || !isSorted(e.Provides) || !isSorted(e.Needs) || !isSorted(e.Wants) || !isSorted(e.Volatile) {
				t.Fatalf("entry not normalised: %+v", e)
			}
			for _, v := range e.Volatile {
				if !strings.HasPrefix(v, "config") {
					t.Fatalf("volatile path %q has no config prefix", v)
				}
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
