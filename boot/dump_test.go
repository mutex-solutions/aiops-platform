package boot

import (
	"encoding/json"
	"io/fs"
	"os"
	"strings"
	"testing"
)

// loadS1 composes the fixture distribution under testdata/s1.
func loadS1(t *testing.T, roles ...Role) Admission {
	t.Helper()
	root := os.DirFS("testdata/s1")
	home, _ := fs.Sub(root, "home")
	dd, _ := fs.Sub(root, "dist")
	md, err := fs.ReadFile(dd, "modules.yaml")
	if err != nil {
		t.Fatal(err)
	}
	dy, err := fs.ReadFile(dd, "dist.yaml")
	if err != nil {
		t.Fatal(err)
	}
	mods, err := ParseModules("modules.yaml", md)
	if err != nil {
		t.Fatal(err)
	}
	dist, err := ParseDist("dist.yaml", dy)
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := Compose(Sources{Profile: "demo", Home: home, Bundles: home}, Options{Server: true, Modules: mods})
	if err != nil {
		t.Fatal(err)
	}
	return Admit(c, mods, dist, roles)
}

func TestDumpGoldenS1(t *testing.T) {
	for _, tc := range []struct {
		name  string
		roles []Role
	}{{"s1", nil}, {"s1-api", []Role{RoleAPI}}, {"s1-worker", []Role{RoleWorker}}} {
		a := loadS1(t, tc.roles...)
		golden(t, tc.name+".dump.golden", RenderDump(a))
		golden(t, tc.name+".hooks.golden", RenderHooks(a))
		golden(t, tc.name+".schema.golden", RenderConfigSchema(a))
	}
}

const richMods = `
o/prov:
  provides: [pk]
  hooks: [{hook: h/one, stage: 2, name: late}]
o/open:
  provides: [ok]
  needs: [pk]
  volatile: [x]
  hooks: [{hook: h/one, stage: 1, name: early}, {hook: h/two, stage: 0}]
o/other: {provides: [pk]}
o/kinds: {}
o/same: {hooks: [{hook: h/one, stage: 1, name: early}]}
`

const richDist = `
providers: {pk: [o/prov]}
sealedHooks: {h/two: [o/open]}
requiredRows: [prov]
requiredKeys: [pk]
`

const richBundle = `
- insert:
    - {id: prov, plugin: o/prov, roles: [api], required: true, config: {pw: "${secret:db-password}", list: [1, 2.5, true, null, "s", {k: v}]}}
    - id: open1
      plugin: o/open
      roles: [api, worker]
      inject: [pk]
      intercept: {pk: {mode: strict}}
      config: {x: 1}
    - id: grp
      plugin: core:group
      isolate: [ok]
      config:
        - {id: c1, plugin: o/open}
        - {id: c2, plugin: o/open}
    - {id: kinds, plugin: o/kinds, config: {b: true, f: 1.5, i: 3, n: null, l: [1], m: {s: x, r: "${env:X}"}}}
    - {id: same2, plugin: o/same}
    - {id: rogue, plugin: o/other}
    - {id: missing, plugin: x/none}
    - {id: iso, plugin: o/open, isolate: [pk]}
`

func TestDumpGoldenRich(t *testing.T) {
	c := comp(t, richBundle, "- {id: open1, config: {x: 2}}")
	a := Admit(c, mustMods(t, richMods), mustDist(t, richDist), nil)
	if !a.Refused() {
		t.Fatal("the fixture isolates a bound key and must be refused")
	}
	golden(t, "rich.dump.golden", RenderDump(a))
	golden(t, "rich.hooks.golden", RenderHooks(a))
	golden(t, "rich.schema.golden", RenderConfigSchema(a))
}

func TestDumpIsDeterministic(t *testing.T) {
	a, b := loadS1(t), loadS1(t)
	for name, f := range map[string]func(Admission) string{"dump": RenderDump, "hooks": RenderHooks, "schema": RenderConfigSchema} {
		if f(a) != f(b) {
			t.Errorf("%s differs between two runs", name)
		}
		if strings.Contains(f(a), "/home/") {
			t.Errorf("%s holds an absolute path", name)
		}
	}
}

func TestSchemaIsJSON(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal([]byte(RenderConfigSchema(loadS1(t))), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["$schema"] == nil || doc["x-aiops-limit"] == nil {
		t.Errorf("document: %v", doc)
	}
}

func TestDumpEmptyComposition(t *testing.T) {
	c := comp(t, "[]")
	a := Admit(c, nil, DistManifest{}, []Role{RoleWorker, RoleAPI})
	d := RenderDump(a)
	for _, want := range []string{"roles api,worker", "rows none", "## diagnostics\nnone\n"} {
		if !strings.Contains(d, want) {
			t.Errorf("dump lacks %q:\n%s", want, d)
		}
	}
	if h := RenderHooks(a); !strings.Contains(h, "no hook chains") {
		t.Errorf("hooks: %s", h)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(RenderConfigSchema(a)), &doc); err != nil {
		t.Fatal(err)
	}
}

func TestShowValue(t *testing.T) {
	v := Value{Kind: KindMap, Map: []Field{
		{"a", Value{Kind: KindList, List: []Value{{Kind: KindInt, Int: 1}, {Kind: KindFloat, Float: 2.5}, {Kind: KindBool, Bool: true}, {}}}},
		{"r", Value{Kind: KindRef, Ref: Ref{RefFile, "/x"}}},
		{"s", Value{Kind: KindString, Str: "q\"x"}},
	}}
	if got, want := ShowValue(v), `{a: [int(1), float(2.5), true, null], r: ${file:/x}, s: "q\"x"}`; got != want {
		t.Errorf("got %s want %s", got, want)
	}
}

func TestDumpGateLineWithoutPartial(t *testing.T) {
	a := loadS1(t)
	a.Gate.Slots[CheckKeyHookClass-1].Partial = ""
	if d := RenderDump(a); !strings.Contains(d, "check 7 Key and hook classes: 1 failed\n") {
		t.Errorf("dump:\n%s", d)
	}
	a.Gate.Slots[CheckKeyHookClass-1].Fails = nil
	if d := RenderDump(a); !strings.Contains(d, "check 7 Key and hook classes: pass\n") {
		t.Errorf("dump:\n%s", d)
	}
}
