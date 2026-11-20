package boot

import (
	"strings"
	"testing"
)

func i64(n int64) Value     { return Value{Kind: KindInt, Int: n} }
func str(s string) Value    { return Value{Kind: KindString, Str: s} }
func mapv(f ...Field) Value { return Value{Kind: KindMap, Map: f} }

// TestDigestKnownAnswer pins the encoding to a value computed by an
// independent implementation (a short Python script written from the
// description at EncodeDigest), so the description is enough to reimplement.
func TestDigestKnownAnswer(t *testing.T) {
	cfg := mapv(
		Field{"k", i64(1)},
		Field{"f", Value{Kind: KindFloat, Float: 2.5}},
		Field{"r", Value{Kind: KindRef, Ref: Ref{RefEnv, "N"}}},
		Field{"l", Value{Kind: KindList, List: []Value{str("x"), {}, {Kind: KindBool, Bool: true}}}},
	)
	rows := []Row{{ID: "a", Plugin: "o/a", Config: cfg, Enabled: true, Roles: []Role{RoleAPI}}}
	mods := ModuleTable{"o/a": {Module: "m", Version: "v1", Sum: "sum"}}
	d := EncodeDigest(rows, mods, []LockDigest{{"n", "d"}})
	const want = "sha256:51d19eb6ded7f6f084dbb8850728c808a4e81e47f7e6bc9ef987e7014fd14b7d"
	if d.String() != want {
		t.Fatalf("digest %s, want %s", d, want)
	}
}

func TestDigestStability(t *testing.T) {
	a := Row{ID: "a", Plugin: "o/a", Enabled: true, Roles: []Role{RoleAPI, RoleWorker}, Inject: []string{"x", "y"},
		Config: mapv(Field{"a", i64(1)}, Field{"b", str("v")}), Isolate: []string{"p", "q"},
		Intercept: []Field{{"i", i64(1)}, {"j", i64(2)}}}
	b := Row{ID: "b", Plugin: "o/b", Parent: "a", Enabled: true}
	base := EncodeDigest([]Row{a, b}, nil, nil)

	same := map[string]func() Digest{
		"row order": func() Digest { return EncodeDigest([]Row{b, a}, nil, nil) },
		"map order": func() Digest {
			r := a
			r.Config = mapv(Field{"b", str("v")}, Field{"a", i64(1)})
			r.Intercept = []Field{{"j", i64(2)}, {"i", i64(1)}}
			return EncodeDigest([]Row{r, b}, nil, nil)
		},
		"set order": func() Digest {
			r := a
			r.Roles = []Role{RoleWorker, RoleAPI}
			r.Inject = []string{"y", "x"}
			r.Isolate = []string{"q", "p"}
			return EncodeDigest([]Row{r, b}, nil, nil)
		},
		"nil and empty sets": func() Digest {
			r := b
			r.Roles, r.Inject, r.Isolate, r.Intercept = []Role{}, []string{}, []string{}, []Field{}
			return EncodeDigest([]Row{a, r}, nil, nil)
		},
		"unused module fields": func() Digest {
			return EncodeDigest([]Row{a, b}, ModuleTable{}, nil)
		},
	}
	for name, f := range same {
		if f() != base {
			t.Errorf("%s changes the digest", name)
		}
	}
	// the module table and lock order, and fields outside the identity, do not matter
	m1 := ModuleTable{"x/a": {Module: "m", Version: "1", Sum: "s", Provides: []string{"k"}, Plugin: 1}, "x/b": {Module: "n"}}
	m2 := ModuleTable{"x/b": {Module: "n", Needs: []string{"z"}, Volatile: []string{"v"}}, "x/a": {Module: "m", Version: "1", Sum: "s"}}
	if EncodeDigest(nil, m1, nil) != EncodeDigest(nil, m2, nil) {
		t.Error("module fields outside the identity change the digest")
	}
	l1 := []LockDigest{{"a", "1"}, {"b", "2"}}
	if EncodeDigest(nil, nil, l1) != EncodeDigest(nil, nil, []LockDigest{l1[1], l1[0]}) {
		t.Error("lock order changes the digest")
	}
	if l1[0].Name != "a" {
		t.Error("EncodeDigest sorted its input")
	}
}

func TestDigestSensitivity(t *testing.T) {
	row := func(mod func(*Row)) Digest {
		r := Row{ID: "a", Plugin: "o/a", Enabled: true, Config: mapv(Field{"k", i64(1)})}
		mod(&r)
		return EncodeDigest([]Row{r}, nil, nil)
	}
	base := row(func(*Row) {})
	mods := map[string]func(*Row){
		"id":               func(r *Row) { r.ID = "b" },
		"plugin":           func(r *Row) { r.Plugin = "o/b" },
		"parent":           func(r *Row) { r.Parent = "p" },
		"enabled":          func(r *Row) { r.Enabled = false },
		"required":         func(r *Row) { r.Required = true },
		"group":            func(r *Row) { r.Group = true },
		"roles":            func(r *Row) { r.Roles = []Role{RoleEdge} },
		"inject":           func(r *Row) { r.Inject = []string{"x"} },
		"isolate":          func(r *Row) { r.Isolate = []string{"x"} },
		"intercept":        func(r *Row) { r.Intercept = []Field{{"k", i64(1)}} },
		"config":           func(r *Row) { r.Config = mapv(Field{"k", i64(2)}) },
		"int vs float":     func(r *Row) { r.Config = mapv(Field{"k", Value{Kind: KindFloat, Float: 1}}) },
		"string vs ref":    func(r *Row) { r.Config = mapv(Field{"k", Value{Kind: KindRef, Ref: Ref{RefEnv, "1"}}}) },
		"string vs string": func(r *Row) { r.Config = mapv(Field{"k", str("1")}) },
		"bool true":        func(r *Row) { r.Config = mapv(Field{"k", Value{Kind: KindBool, Bool: true}}) },
		"bool false":       func(r *Row) { r.Config = mapv(Field{"k", Value{Kind: KindBool}}) },
		"null":             func(r *Row) { r.Config = mapv(Field{"k", Value{}}) },
		"list":             func(r *Row) { r.Config = mapv(Field{"k", Value{Kind: KindList}}) },
		"key bytes":        func(r *Row) { r.Config = mapv(Field{"j", i64(1)}) },
	}
	seen := map[Digest]string{base: "base"}
	for name, m := range mods {
		d := row(m)
		if prev, dup := seen[d]; dup {
			t.Errorf("%s has the same digest as %s", name, prev)
		}
		seen[d] = name
	}
	if EncodeDigest(nil, ModuleTable{"a": {Version: "1"}}, nil) == EncodeDigest(nil, ModuleTable{"a": {Version: "2"}}, nil) {
		t.Error("module version is not in the digest")
	}
	if EncodeDigest(nil, nil, []LockDigest{{"a", "1"}}) == EncodeDigest(nil, nil, []LockDigest{{"a", "2"}}) {
		t.Error("lock digest is not in the digest")
	}
}

func TestDigestNumericForms(t *testing.T) {
	cfg := func(v Value) Digest {
		return EncodeDigest([]Row{{ID: "a", Plugin: "o/a", Config: mapv(Field{"k", v})}}, nil, nil)
	}
	negZero := Value{Kind: KindFloat, Float: 0}
	negZero.Float = -negZero.Float
	if cfg(negZero) != cfg(Value{Kind: KindFloat}) {
		t.Error("-0 and +0 differ")
	}
	// the same numbers written differently in YAML give one digest
	same := [][2]string{{"1e3", "1000.0"}, {"0.5", ".5"}, {"-0.0", "0.0"}, {"7", "+7"}, {"-0", "0"}}
	for _, p := range same {
		da := yamlDigest(t, p[0])
		db := yamlDigest(t, p[1])
		if da != db {
			t.Errorf("%s and %s differ", p[0], p[1])
		}
	}
	for _, p := range [][2]string{{"1", "1.0"}, {"1", "'1'"}, {"true", "'true'"}, {"null", "'null'"}} {
		if yamlDigest(t, p[0]) == yamlDigest(t, p[1]) {
			t.Errorf("%s and %s are the same", p[0], p[1])
		}
	}
}

func yamlDigest(t *testing.T, scalar string) Digest {
	t.Helper()
	b := "- insert: [{id: a, plugin: o/a, config: {k: " + scalar + "}}]\n"
	c, _, err := files{bundle: b}.compose(Options{})
	if err != nil {
		t.Fatalf("%s: %v", scalar, err)
	}
	return c.Digest
}

func TestDigestString(t *testing.T) {
	var d Digest
	if got := d.String(); got != "sha256:"+strings.Repeat("00", 32) {
		t.Error(got)
	}
}

func TestComposeDigestCoversModulesAndLocks(t *testing.T) {
	f := files{bundle: base}
	a, _, _ := f.compose(Options{Server: true})
	b, _, _ := f.compose(Options{Server: true, Modules: ModuleTable{"o/a": {Version: "v2"}}})
	c, _, _ := f.compose(Options{Server: true, Locks: []LockDigest{{"o/a", "sha256:x"}}})
	if a.Digest == b.Digest || a.Digest == c.Digest || b.Digest == c.Digest {
		t.Error("modules and locks must change the generation digest")
	}
}
