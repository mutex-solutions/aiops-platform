package boot

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// s1Served is serves.definitions of the fixture distribution under testdata/s1: the
// definitions that own the hooks of its plugins, with the stages of each hook in chain
// order (ADR-060 decision 7). A key they do not declare is bound where dist.yaml lists
// who may hold it.
const s1Served = `{approval: [{version: "0.1", suite: 1, classes: {approval: {provider: bound, consumer: open}},
    hooks: {approval/gates: {stages: [early, policy], access: sealed, remote: "no"}}}],
  tool: [{version: "0.1", suite: 1, classes: {tools: {provider: open, consumer: open}},
    hooks: {tool/call: {stages: [default], access: open, remote: "no"}}}]}`

// s1Signed assembles the signed distribution manifest of the fixture, which go test
// -update writes to testdata/s1/dist/distribution.yaml for aiopsctl's tests: the
// manifests of dist/plugins, each attested at suite revision 1 for every definition its
// requires names, the bindings of dist/dist.yaml and s1Served.
func s1Signed(t *testing.T) string {
	t.Helper()
	d := newTestDist()
	d.definitions = s1Served
	dy, err := os.ReadFile("testdata/s1/dist/dist.yaml")
	if err != nil {
		t.Fatal(err)
	}
	d.bindings = bindingsText(mustDist(t, string(dy)))
	files, _ := filepath.Glob("testdata/s1/dist/plugins/*.yaml")
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		m, err := ReadManifest(f, src, ManifestStrict)
		if err != nil {
			t.Fatal(err)
		}
		var defs []string
		for _, n := range slices.Sorted(maps.Keys(m.Requires.Definitions)) {
			defs = append(defs, fmt.Sprintf(`%q: {"version": %q, "suite": 1}`, n, m.Requires.Definitions[n][0]))
		}
		d.add(string(src), "{"+strings.Join(defs, ", ")+"}")
	}
	return string(d.text())
}

// loadS1 renders the fixture distribution under testdata/s1 as aiopsctl profile render
// does, on a home that trusts its signed manifest.
func loadS1(t *testing.T, roles ...Role) Admission {
	t.Helper()
	signed, err := os.ReadFile("testdata/s1/dist/distribution.yaml")
	if err != nil {
		t.Fatal(err)
	}
	d, err := ReadDistribution("distribution.yaml", signed)
	if err != nil {
		t.Fatal(err)
	}
	home, _ := trustedHome(signed)
	root := os.DirFS("testdata/s1/home")
	err = fs.WalkDir(root, ".", func(p string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			var data []byte
			data, err = fs.ReadFile(root, p)
			home[p] = &fstest.MapFile{Data: data}
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := Render(Sources{Profile: "demo", Home: home, Bundles: home}, true, d, roles)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestDumpGoldenS1(t *testing.T) {
	golden(t, "s1/dist/distribution.yaml", s1Signed(t))
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
	if doc["$schema"] == nil || doc["properties"] == nil {
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
	a.Gate.Slots[CheckKeyHookClass-1].Partial, a.Gate.Slots[CheckKeyHookClass-1].Clauses = "", nil
	if d := RenderDump(a); !strings.Contains(d, "check 7 Key and hook classes: 1 failed\n") {
		t.Errorf("dump:\n%s", d)
	}
	a.Gate.Slots[CheckKeyHookClass-1].Fails = nil
	if d := RenderDump(a); !strings.Contains(d, "check 7 Key and hook classes: pass\n") {
		t.Errorf("dump:\n%s", d)
	}
}

// Render refuses what boot refuses before the gate: a profile that does not compose and
// a row with the loader's id. A row that the bundle of an installed artifact inserted
// shows the artifact as its origin in --dump, and boot hands the origin to the loader,
// which records it with the entry, at boot and in every live generation (ADR-060
// decisions 14 and 19).
func TestRenderRefusalsAndOrigin(t *testing.T) {
	d, err := ReadDistribution("d", newTestDist().text())
	if err != nil {
		t.Fatal(err)
	}
	home := fstest.MapFS{
		"profiles/p/profile.yaml": {Data: []byte("bundles: [b]")}, "bundles/b/bundle.yaml": {Data: []byte(rows("id: loader, plugin: o/x"))},
		"profiles/q/profile.yaml": {Data: []byte("bundles: [acme/pack]")}, "bundles/acme/pack/bundle.yaml": {Data: []byte(rows("id: e, plugin: acme/pack"))},
	}
	if _, _, err := Render(Sources{Profile: "nope", Home: home, Bundles: home}, false, d, nil); err == nil {
		t.Error("a profile that does not exist renders")
	}
	if _, _, err := Render(Sources{Profile: "p", Home: home, Bundles: home}, false, d, nil); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("the loader's id: %v", err)
	}
	a, _, err := Render(Sources{Profile: "q", Home: home, Bundles: home}, false, d, nil)
	if err != nil {
		t.Fatal(err)
	}
	if dump := RenderDump(a); !strings.Contains(dump, "\nrow pack:e plugin=acme/pack enabled=true required=false group=false roles=- parent=pack origin=acme/pack\n") {
		t.Errorf("dump:\n%s", dump)
	}
	r, _ := findRow(a.Comp, "pack:e")
	if s := entrySpecs(a.Comp); len(s) != 2 || s[1].Origin != "acme/pack" || launchRow(r).Origin != "acme/pack" {
		t.Errorf("entry specs %+v, launch row %+v", s, launchRow(r))
	}
}
