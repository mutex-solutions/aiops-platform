// SPDX-License-Identifier: Apache-2.0

package plugingen

import (
	goparser "go/parser"
	"go/token"
	"math"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

const mod = "github.com/mutex-solutions/aiops-platform"

func mustDef(t *testing.T, path, src string) Definition {
	t.Helper()
	d, err := ParseDefinition(path, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func defs(t *testing.T) []Definition {
	return []Definition{
		mustDef(t, mod+"/sdk/def/echoers/v0", "key: echoers\nversion: \"0.1\"\nstability: experimental\nclass: {provider: open, consumer: open}\nremote: \"no\"\nsuite: 1\n"),
		mustDef(t, mod+"/sdk/def/launch/v0", "key: launch\nversion: \"0.3\"\nstability: preview\nclass:\n  catalog: {provider: bound, consumer: open}\n  appExit: {provider: bound, consumer: bound}\nremote: \"no\"\ncompatibleFrom: \"0.2\"\nsuite: 2\n"),
		mustDef(t, mod+"/sdk/def/reload/v0", "key: reload\nversion: \"0.1\"\nstability: preview\nclass: {provider: open, consumer: bound}\nremote: \"no\"\nsuite: 1\nhooks:\n  reload/operation: {stages: [default], access: open, remote: \"no\"}\n"),
	}
}

func TestParseDefinition(t *testing.T) {
	ds := defs(t)
	if d := ds[1]; d.Name != "launch" || d.Major != 0 || d.Version != "0.3" || d.CompatibleFrom != "0.2" || d.Suite != 2 ||
		!reflect.DeepEqual(d.Classes, map[string]Class{"catalog": {"bound", "open"}, "appExit": {"bound", "bound"}}) {
		t.Errorf("launch = %+v", d)
	}
	if d := ds[2]; !reflect.DeepEqual(d.Classes, map[string]Class{"reload": {"open", "bound"}}) ||
		!reflect.DeepEqual(d.Hooks, map[string]Hook{"reload/operation": {Stages: []string{"default"}, Access: "open", Remote: "no"}}) {
		t.Errorf("reload = %+v", d)
	}
	for name, src := range map[string]string{
		"a major other than the path's": "key: echoers\nversion: \"1.0\"\nstability: preview\nclass: {provider: open, consumer: open}\nremote: \"no\"\nsuite: 1\n",
		"an unknown field":              "key: echoers\nversion: \"0.1\"\nstability: preview\nclass: {provider: open, consumer: open}\nremote: \"no\"\nsuite: 1\nflavour: x\n",
	} {
		if _, err := ParseDefinition(mod+"/sdk/def/echoers/v0", []byte(src)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
	if _, err := ParseDefinition(mod+"/test/dist/def", []byte("key: x\n")); err == nil {
		t.Error("a path with no <name>/v<N> parsed")
	}
}

// requires.definitions names every definition whose key or hook the plugin uses,
// provides, contributes to or listens on, keyed by name, and leaves out the kernel's
// keys and hooks and a key the plugin declares itself (ADR-060 decision 9; ADR-048
// decision 12).
func TestRequires(t *testing.T) {
	g := plugin.Generated{
		Uses:      []plugin.Use{{Key: "catalog", Major: 0, As: "need"}, {Key: "echoers", Major: 0, As: "need"}, {Key: "logger", Major: 0, As: "want"}},
		Provides:  []plugin.KeyRef{{Key: "own.ready", Major: 0}},
		Registers: []plugin.KeyRef{{Key: "echoers", Major: 0}},
		Hooks:     []plugin.HookRef{{Hook: "internal/status", Stage: "default"}, {Hook: "reload/operation", Stage: "default"}},
	}
	if err := Requires(&g, defs(t)); err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"echoers": {"0.1"}, "launch": {"0.3"}, "reload": {"0.1"}}
	if !reflect.DeepEqual(g.Requires.Definitions, want) {
		t.Fatalf("definitions = %v, want %v", g.Requires.Definitions, want)
	}
	for name, g := range map[string]plugin.Generated{
		"a used key no definition declares":       {Uses: []plugin.Use{{Key: "nowhere", As: "need"}}},
		"a registry no definition declares":       {Registers: []plugin.KeyRef{{Key: "nowhere"}}},
		"a hook no definition declares":           {Hooks: []plugin.HookRef{{Hook: "nowhere/hook", Stage: "default"}}},
		"a key at another major than its version": {Uses: []plugin.Use{{Key: "echoers", Major: 1, As: "need"}}},
	} {
		if err := Requires(&g, defs(t)); err == nil {
			t.Errorf("%s: Requires passed", name)
		}
	}
}

// The major is in the import path and the key (00-decisions.md:266), so a key maps
// to the definition that declares it at its major. An adapter row that serves v1
// over v0 names both majors in requires (ADR-048 decision 12), and its cycle test
// mounts the fake of the major it injects, each major under a name of its own.
func TestTwoMajorsOfOneDefinition(t *testing.T) {
	v1 := mustDef(t, mod+"/sdk/def/echoers/v1", "key: echoers\nversion: \"1.0\"\nstability: experimental\nclass: {provider: open, consumer: open}\nremote: \"no\"\nsuite: 1\n")
	ds := append(defs(t), v1)
	g := plugin.Generated{Uses: []plugin.Use{{Key: "echoers", Major: 0, As: "need"}}, Provides: []plugin.KeyRef{{Key: "echoers", Major: 1}}}
	if err := Requires(&g, ds); err != nil {
		t.Fatal(err)
	}
	if want := map[string][]string{"echoers": {"0.1", "1.0"}}; !reflect.DeepEqual(g.Requires.Definitions, want) {
		t.Errorf("definitions = %v, want %v", g.Requires.Definitions, want)
	}
	g = plugin.Generated{Provides: []plugin.KeyRef{{Key: "echoers", Major: 2}}}
	if err := Requires(&g, ds); err == nil || !strings.Contains(err.Error(), "key echoers at major 2") {
		t.Errorf("a provided key at a major no definition declares: Requires = %v", err)
	}
	if got := injected(ds, []plugin.Use{{Key: "echoers", Major: 1, As: "need"}}); len(got) != 1 || got[0].Path != v1.Path {
		t.Errorf("the fakes of echoers@1 = %+v, want the fake of %s alone", got, v1.Path)
	}
	src, err := cycleTest(target{ID: "aiops/adapter", Name: "adapter", ImportPath: mod + "/examples/adapter", Accessor: "Plugin"},
		[]Definition{ds[0], v1}, nil, "nil")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`echoersfake "` + mod + `/sdk/def/echoers/v0/fake"`, `echoersv1fake "` + mod + `/sdk/def/echoers/v1/fake"`,
		"echoersFake, echoersPlugin := echoersfake.New()", "echoersv1Fake, echoersv1Plugin := echoersv1fake.New()",
		"cycle.Fake{Plugin: echoersv1Plugin, Inventory: echoersv1Fake.Inventory}"} {
		if !strings.Contains(string(src), s) {
			t.Errorf("%s lacks %q:\n%s", CycleFile, s, src)
		}
	}
}

// The generated cycle test compiles whatever the plugin's package is called: no two
// imports of the file take one name, a package named cycle or testing included, and
// the plugin is reached through its own import.
func TestCycleTestImportNames(t *testing.T) {
	for _, name := range []string{"cycle", "testing", "echoersfake", "target", "zzplugin"} {
		tg := target{ID: "test/x", Name: name, ImportPath: mod + "/x/" + name, Accessor: "Plugin"}
		src, err := cycleTest(tg, defs(t)[:1], nil, "nil")
		if err != nil {
			t.Fatal(err)
		}
		f, err := goparser.ParseFile(token.NewFileSet(), CycleFile, src, goparser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]string{}
		for _, im := range f.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			n := path.Base(p)
			if im.Name != nil {
				n = im.Name.Name
			}
			if seen[n] != "" {
				t.Errorf("package %s: %s and %s are both imported as %s:\n%s", name, seen[n], p, n, src)
			}
			seen[n] = p
			if p == tg.ImportPath && !strings.Contains(string(src), n+".Plugin,") {
				t.Errorf("package %s: the plugin is imported as %s and not run through it:\n%s", name, n, src)
			}
		}
	}
}

// The fake of a definition provides every key the definition declares, so the cycle
// test of a plugin that injects one key of a definition and provides another could
// never pass: the fake and the plugin would both provide the key. plugingen refuses
// such a plugin, naming the key and the definition, instead of writing that test
// (ADR-060 decision 12).
func TestTwoKeysOfOneDefinition(t *testing.T) {
	_, err := Generate(filepath.Join("testdata", "twokeys"))
	if err == nil || !strings.Contains(err.Error(), "appExit") || !strings.Contains(err.Error(), mod+"/sdk/def/launch/v0") {
		t.Fatalf("Generate of a plugin that provides a key of a definition it injects: %v", err)
	}
}

// Step 1 of plugin check decides a reserved owner by where the code is (ADR-048
// decision 23): aiops only in the root module of this repository, contrib only in a
// module under its contrib/ path; any other owner anywhere. readTarget reads the
// module path with go list.
func TestReservedOwners(t *testing.T) {
	for _, c := range []struct {
		id, module string
		ok         bool
	}{
		{"aiops/clock", mod, true},
		{"aiops/nested-echo", "example.com/acme/nested", false},
		{"aiops/x", mod + "/contrib/x", false},
		{"contrib/x", mod + "/contrib/x", true},
		{"contrib/x", mod, false},
		{"contrib/x", mod + "/contribx", false},
		{"contrib/x", "example.com/acme/contrib/x", false},
		{"acme/x", "example.com/acme/nested", true},
		{"aiops-test/x", "example.com/acme/nested", true},
	} {
		if err := reservedOwner(c.id, c.module); (err == nil) != c.ok {
			t.Errorf("reservedOwner(%s, %s) = %v, want ok %v", c.id, c.module, err, c.ok)
		}
	}
	tg, err := readTarget(filepath.Join("testdata", "echo"))
	if err != nil || tg.Module != mod {
		t.Errorf("readTarget(testdata/echo).Module = %q, %v; want %s", tg.Module, err, mod)
	}
}

const s1Manifest = `# The hand-written half (07-c4-plugin-sdk.md section 2).
manifestVersion: "1.0"
id: aiops/example
version: 0.1.0
title: Example
description: A description long enough to pass the width at which an emitter might fold a plain scalar onto a second line.
license: Apache-2.0
publisher: {id: aiops}
stability: experimental
scope: [process]   # a set
runtimes: [inproc]
provides:
  - {key: old, major: 0}
hooks: []
`

func generated() plugin.Generated {
	return plugin.Generated{
		Uses:         []plugin.Use{{Key: "echoers", Major: 0, As: "need"}},
		Provides:     []plugin.KeyRef{},
		Registers:    []plugin.KeyRef{{Key: "echoers", Major: 0}},
		Hooks:        []plugin.HookRef{},
		ConfigSchema: map[string]any{"$schema": "https://json-schema.org/draft/2020-12/schema", "type": "object", "properties": map[string]any{"n": map[string]any{"type": "integer", "minimum": 0.0}}, "additionalProperties": false},
		Durable:      []any{},
		Requires:     plugin.Requires{PluginAPI: plugin.APIVersion, Definitions: map[string][]string{"echoers": {"0.1"}}},
	}
}

// Rewrite keeps every hand-written field and comment, and writes the seven
// generated fields after them, in order, under the marker (ADR-048 decision 1).
func TestRewrite(t *testing.T) {
	out, err := Rewrite([]byte(s1Manifest), generated())
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	for _, line := range []string{"# The hand-written half (07-c4-plugin-sdk.md section 2).", `manifestVersion: "1.0"`,
		"publisher: {id: aiops}", "scope: [process]   # a set", "  - {key: echoers, major: 0, as: need}", "provides: []",
		"# " + Marker, `  pluginApi: "` + plugin.APIVersion + `"`, `    echoers: ["0.1"]`,
		"description: A description long enough to pass the width at which an emitter might fold a plain scalar onto a second line."} {
		if !strings.Contains(text, line+"\n") {
			t.Errorf("Rewrite lost or changed %q:\n%s", line, text)
		}
	}
	var order []int
	for _, k := range append([]string{"runtimes"}, generatedFields...) {
		order = append(order, strings.Index(text, "\n"+k+":"))
	}
	if !slices.IsSorted(order) || slices.Contains(order, -1) {
		t.Errorf("fields out of order %v:\n%s", order, text)
	}
	if strings.Contains(text, "key: old") {
		t.Errorf("the old generated half stayed:\n%s", text)
	}
	again, err := Rewrite(out, generated())
	if err != nil || string(again) != text {
		t.Errorf("Rewrite is not idempotent (%v):\n%s", err, again)
	}
	if err := Parity(out, generated()); err != nil {
		t.Errorf("Parity of the rewritten file: %v", err)
	}
}

// Rewrite keeps every line of the hand-written half as written, blank lines, a folded
// scalar, spacing, comments and list indentation included, and moves a hand-written
// field that follows a generated one, with its comment, before the generated half
// (ADR-048 decision 1).
func TestRewriteKeepsTheHandWrittenLines(t *testing.T) {
	const hand = `# The hand-written half.
manifestVersion: "1.0"

id: aiops/example
version: 0.1.0
title: Example
description: >
  A folded description
  across two lines.
license:   Apache-2.0   # spaced
publisher: {id: aiops}
stability: experimental
scope: [process]
runtimes:
- inproc
`
	out, err := Rewrite([]byte(hand+"\n"), generated())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), hand+"# "+Marker+"\nuses:\n") {
		t.Errorf("Rewrite changed a hand-written line:\n%s", out)
	}
	again, err := Rewrite(out, generated())
	if err != nil || string(again) != string(out) {
		t.Errorf("Rewrite is not idempotent (%v):\n%s", err, again)
	}
	mixed := hand + "provides:\n  - {key: old, major: 0}\n# The comment of instantiable.\ninstantiable: false\nhooks: []\n"
	out, err = Rewrite([]byte(mixed), generated())
	if err != nil {
		t.Fatal(err)
	}
	if want := hand + "# The comment of instantiable.\ninstantiable: false\n# " + Marker + "\n"; !strings.HasPrefix(string(out), want) ||
		strings.Contains(string(out), "key: old") {
		t.Errorf("Rewrite of a generated field before a hand-written one:\n%s", out)
	}
	if err := Parity(out, generated()); err != nil {
		t.Errorf("Parity: %v", err)
	}
}

// Parity compares the seven fields as read, so a comment or a layout never fails it
// and a value always does, naming the field (ADR-048 decision 1).
func TestParity(t *testing.T) {
	out, err := Rewrite([]byte(s1Manifest), generated())
	if err != nil {
		t.Fatal(err)
	}
	relaid := strings.Replace(string(out), "  - {key: echoers, major: 0, as: need}", "  # a comment\n  - key: echoers\n    as: need\n    major: 0", 1)
	if err := Parity([]byte(relaid), generated()); err != nil {
		t.Errorf("a comment and a layout failed parity: %v", err)
	}
	for field, edit := range map[string][2]string{
		"registers":    {"registers:\n  - {key: echoers, major: 0}", "registers: []"},
		"configSchema": {"minimum: 0", "minimum: 1"},
		"requires":     {`echoers: ["0.1"]`, `echoers: ["0.2"]`},
		"durable":      {"durable: []\n", ""},
	} {
		changed := strings.Replace(string(out), edit[0], edit[1], 1)
		if changed == string(out) {
			t.Fatalf("%s: the edit changed nothing", field)
		}
		if err := Parity([]byte(changed), generated()); err == nil || !strings.Contains(err.Error(), field) {
			t.Errorf("%s: Parity = %v", field, err)
		}
	}
	// Parity reads with boot's strict mode, as plugin check does (ADR-048 decision 15):
	// an unknown field at any minor and a broken rule of the schema fail it.
	for want, edit := range map[string][]string{
		`unknown field "flavour"`:    {"stability: experimental", "stability: experimental\nflavour: x"},
		`unknown field "badge"`:      {`manifestVersion: "1.0"`, `manifestVersion: "1.4"`, "publisher: {id: aiops}", "publisher: {id: aiops, badge: x}"},
		`publisher.id "acme" is not`: {"publisher: {id: aiops}", "publisher: {id: acme}"},
	} {
		changed := strings.NewReplacer(edit...).Replace(string(out))
		if err := Parity([]byte(changed), generated()); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: Parity = %v", want, err)
		}
	}
}

// The kernel part of serves equals what sdk/kernel reports of its four built-in
// keys and its six hooks (ADR-048 decision 12).
func TestKernelPart(t *testing.T) {
	k := Kernel()
	for _, key := range []kernel.AnyKey{kernel.ReflectKey, kernel.RegistryKey, kernel.HooksKey, kernel.LoggerKey} {
		pb, cb, _ := kernel.KeyClasses(key)
		want := Class{Provider: "open", Consumer: "open"}
		if pb {
			want.Provider = "bound"
		}
		if cb {
			want.Consumer = "bound"
		}
		if got, ok := k.Keys[key.Name()]; !ok || got != want {
			t.Errorf("key %s = %+v, want %+v", key.Name(), got, want)
		}
	}
	hooks := []kernel.AnyHook{kernel.InternalPlugin, kernel.InternalStatus, kernel.InternalService, kernel.InternalConfig,
		kernel.InternalUpdate, kernel.InternalCommit}
	for _, h := range hooks {
		sealed, _ := kernel.HookSealed(h)
		access := "open"
		if sealed {
			access = "sealed"
		}
		if got := k.Hooks[h.Name()]; got.Access != access || !slices.Equal(got.Stages, []string{kernel.DefaultStage}) {
			t.Errorf("hook %s = %+v", h.Name(), got)
		}
	}
	if len(k.Keys) != 4 || len(k.Hooks) != len(hooks) {
		t.Errorf("kernel part = %+v", k)
	}
}

// serves names each served major of a definition with its minor, compatibleFrom,
// deprecated, suite revision, classes and hooks, and the pluginApi with the kernel
// part (ADR-060 decision 9).
func TestServes(t *testing.T) {
	s := Serves(defs(t))
	if s.PluginAPI != plugin.APIVersion || !reflect.DeepEqual(s.Kernel, Kernel()) {
		t.Errorf("serves = %+v", s)
	}
	l := s.Definitions["launch"]
	if len(l) != 1 || l[0].Version != "0.3" || l[0].CompatibleFrom != "0.2" || l[0].Suite != 2 || l[0].Classes["appExit"] != (Class{"bound", "bound"}) {
		t.Errorf("launch = %+v", l)
	}
	if r := s.Definitions["reload"]; len(r) != 1 || r[0].Hooks["reload/operation"].Access != "open" {
		t.Errorf("reload = %+v", r)
	}
}

// Generate reads a plugin package through the driver: testdata/echo, written with
// plugin.Define, and testdata/legacy, written with kernel.Define, as the loader is
// (ADR-060 decision 13).
func TestGenerate(t *testing.T) {
	r, err := Generate(filepath.Join("testdata", "echo"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Generated.Registers, []plugin.KeyRef{{Key: "echoers", Major: 0}}) ||
		!reflect.DeepEqual(r.Generated.Requires.Definitions, map[string][]string{"echoers": {"0.1"}}) ||
		r.Generated.ConfigSchema["type"] != "object" {
		t.Errorf("echo = %+v", r.Generated)
	}
	cyc := string(r.Files[CycleFile])
	for _, s := range []string{"package echo_test", `echoersfake "` + mod + `/sdk/def/echoers/v0/fake"`, "zzplugin.Plugin",
		`[]string{"echoers"}`, "cycle.Fake{Plugin: echoersPlugin, Inventory: echoersFake.Inventory}",
		`map[string]any{"name": "loud"}`} {
		if !strings.Contains(cyc, s) {
			t.Errorf("%s lacks %q:\n%s", CycleFile, s, cyc)
		}
	}
	if !strings.Contains(string(r.Files[TenantsFile]), `t.Skip("not yet (S4a)`) {
		t.Errorf("%s:\n%s", TenantsFile, r.Files[TenantsFile])
	}
	if err := Parity(r.Manifest, r.Generated); err != nil {
		t.Errorf("Parity of the generated manifest: %v", err)
	}
	// An int64 default above 2^53 keeps every digit through the driver, the manifest
	// and boot's reading (ADR-048 decision 9).
	props, _ := r.Generated.ConfigSchema["properties"].(map[string]any)
	if max, _ := props["max"].(map[string]any); max["default"] != int64(math.MaxInt64) ||
		!strings.Contains(string(r.Manifest), "default: 9223372036854775807\n") {
		t.Errorf("max = %#v in\n%s", max, r.Manifest)
	}
	if err := Check(filepath.Join("testdata", "echo")); err != nil {
		t.Errorf("Check of the committed files: %v", err)
	}
	l, err := Generate(filepath.Join("testdata", "legacy"))
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Generated.ConfigSchema) != 0 || len(l.Generated.Registers) != 0 || l.Files[TenantsFile] != nil ||
		!strings.Contains(string(l.Files[CycleFile]), "zzplugin.New()") {
		t.Errorf("legacy = %+v\n%s", l.Generated, l.Files[CycleFile])
	}
}

// testdata/cycle.yaml is read as a layer reads a row config, so the cycle test
// mounts only a config that the launcher would hand the plugin: the layer parser's
// typing of plain scalars, no anchor or alias, and no reference, which nothing
// resolves in the cycle test (ADR-059 decision 6).
func TestCycleConfigReadsTheLayerRules(t *testing.T) {
	read := func(src string) (string, error) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "testdata"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "testdata", "cycle.yaml"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		return cycleConfig(dir)
	}
	got, err := read("# a comment\nn: 10\nname: \"yes\"\nr: 0.5\nl: [a]\nm: {k: v}\nd: 2001-12-14\n")
	want := `map[string]any{"d": "2001-12-14", "l": []any{"a"}, "m": map[string]any{"k": "v"}, "n": int64(10), "name": "yes", "r": float64(0.5)}`
	if err != nil || got != want {
		t.Errorf("cycleConfig = %s, %v; want %s", got, err, want)
	}
	for _, src := range []string{"n: 012\n", "hex: 0x1F\n", "name: yes\n", "n: &v 1\nm: *v\n", "a: !!str x\n",
		"a: 1\na: 2\n", "url: ${env:URL}\n", "- a\n", "a: 1\n---\nb: 2\n"} {
		if got, err := read(src); err == nil {
			t.Errorf("cycleConfig(%q) = %s, want an error", src, got)
		}
	}
}

// A config in testdata/cycle.yaml is the row config the cycle test mounts with,
// written as a Go literal of the tree types the launcher parses.
func TestCycleConfigLiteral(t *testing.T) {
	got, err := goLiteral(map[string]any{"queue": int64(4), "rate": 0.5, "on": true, "name": "x", "tags": []any{"a"}, "none": nil})
	if err != nil {
		t.Fatal(err)
	}
	want := `map[string]any{"name": "x", "none": nil, "on": true, "queue": int64(4), "rate": float64(0.5), "tags": []any{"a"}}`
	if got != want {
		t.Errorf("goLiteral =\n%s\nwant\n%s", got, want)
	}
}
