package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/internal/plugingen"
	testdist "github.com/mutex-solutions/aiops-platform/test/dist"
)

// The driver takes -write and --write, as the Go flag syntax does, and refuses a
// call with no directory.
func TestFlags(t *testing.T) {
	for _, flag := range []string{"-write", "--write"} {
		var out strings.Builder
		if err := run([]string{flag, t.TempDir()}, &out); err != nil || !strings.HasPrefix(out.String(), "plugingen: 0 plugins") {
			t.Errorf("%s on a directory with no plugin: %v, %q", flag, err, out.String())
		}
	}
	if err := run([]string{"--write"}, io.Discard); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("no directory: %v", err)
	}
}

// dirs maps each plugin of the release table, of the test distribution and of the
// test release to its directory: one plugin per directory (ADR-048 decision 18).
var dirs = map[string]string{
	"aiops/loader":                   "loader",
	"aiops/clock":                    "plugins/clock",
	"aiops/config-refs":              "plugins/config-refs",
	"aiops/readiness":                "plugins/readiness",
	"aiops/reload":                   "plugins/reload",
	"aiops/reload-socket":            "plugins/reload-socket",
	"aiops/restart-policy":           "plugins/restart-policy",
	"aiops/cli":                      "plugins/cli",
	"aiops/command-profile":          "plugins/command-profile",
	"aiops/command-plugin":           "plugins/command-plugin",
	"aiops/command-trust":            "plugins/command-trust",
	"aiops/command-dist":             "plugins/command-dist",
	"aiops/command-key":              "plugins/command-key",
	"aiops/operations":               "plugins/operations",
	"aiops-test/toy-provider":        "test/dist/plugins/toy-provider",
	"aiops-test/toy-consumer":        "test/dist/plugins/toy-consumer",
	"aiops-test/toy-spine":           "test/dist/plugins/toy-spine",
	"aiops-test/fail-apply":          "test/dist/plugins/fail-apply",
	"aiops-test/panic-apply":         "test/dist/plugins/panic-apply",
	"aiops-test/slow-apply":          "test/dist/plugins/slow-apply",
	"aiops-test/bad-config":          "test/dist/plugins/bad-config",
	"aiops-test/rogue-provider":      "test/dist/plugins/rogue-provider",
	"aiops-test/leaky-goroutine":     "test/dist/plugins/leaky-goroutine",
	"aiops-test/slow-dispose":        "test/dist/plugins/slow-dispose",
	"aiops-test/unserved-version":    "test/dist/plugins/unserved-version",
	"aiops-test/unsigned-row":        "test/dist/plugins/unsigned-row",
	"aiops-test/foreign-attestation": "test/dist/plugins/foreign-attestation",
	"aiops-test/test-state":          "test/dist/plugins/teststate",
	"aiops-test/test-submit":         "test/dist/plugins/testsubmit",
	"aiops/example-dispatch":         "examples/dispatch",
	"aiops/example-echoer":           "examples/echoer",
	"aiops/example-stamper":          "examples/stamper",
	"aiops/example-caller":           "examples/caller",
}

// Every plugin of the module tables, the release table of dist, the test plugins of
// test/dist and the toy plugins of the test release, has a plugin.yaml beside its
// package with its id, the seven generated fields and no S1 field, and the generated
// cycle test beside it (ADR-060 decisions 11, 13, 28 and 44). The parity step of make
// check and of CI, which this command runs, holds their content to what plugingen
// writes.
func TestEveryPluginOfTheModuleTablesHasAGeneratedManifest(t *testing.T) {
	root := filepath.Join("..", "..")
	var ids []string
	for _, m := range testdist.Modules() { // the release table, the test plugins and the toy plugins
		ids = append(ids, m.Plugin.ID)
	}
	var listed []string
	for id := range dirs {
		listed = append(listed, id)
	}
	slices.Sort(ids)
	slices.Sort(listed)
	if !slices.Equal(ids, listed) {
		t.Fatalf("the module tables hold %v, this test lists %v", ids, listed)
	}
	found, err := pluginDirs([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	for id, dir := range dirs {
		src, err := os.ReadFile(filepath.Join(root, dir, "plugin.yaml"))
		if err != nil {
			t.Errorf("%s: %v", id, err)
			continue
		}
		var m map[string]any
		if err := yaml.Unmarshal(src, &m); err != nil {
			t.Errorf("%s: %v", id, err)
			continue
		}
		if m["id"] != id {
			t.Errorf("%s/plugin.yaml: id %v", dir, m["id"])
		}
		for _, f := range []string{"uses", "provides", "registers", "hooks", "configSchema", "durable", "requires"} {
			if _, ok := m[f]; !ok {
				t.Errorf("%s/plugin.yaml: no %s", dir, f)
			}
		}
		if _, ok := m["needs"]; ok {
			t.Errorf("%s/plugin.yaml: the S1 field needs is left", dir)
		}
		if _, err := os.Stat(filepath.Join(root, dir, plugingen.CycleFile)); err != nil {
			t.Errorf("%s: %v", dir, err)
		}
		if !slices.Contains(found, filepath.Join(root, dir)) {
			t.Errorf("%s: the parity step does not find it", dir)
		}
	}
}

// A release serves no definition outside sdk/def: the test definitions are fixtures of
// the launcher's tests at suite revision 0, so the check fails when a release's signed
// manifest links a package that holds a definition.yaml anywhere else (ADR-060
// decisions 28 and 55). The test distribution serves its two.
func TestAReleaseServesOnlyTheDefinitionsOfTheSDK(t *testing.T) {
	root := filepath.Join("..", "..")
	release, err := os.ReadFile(filepath.Join(root, "dist", "distribution.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := servesOnlySDK(root, release); err != nil {
		t.Fatalf("the release: %v", err)
	}
	if err := servesOnlySDK(root, testdist.Signed); err == nil ||
		!strings.Contains(err.Error(), "test/dist/def/spine/v0, github.com/mutex-solutions/aiops-platform/test/dist/def/toy/v0") {
		t.Fatalf("the test distribution as a release: %v", err)
	}
	if err := servesOnlySDK(root, []byte("x: [")); err == nil {
		t.Fatal("a malformed manifest passes")
	}
}

// The release module table is generated into dist/ from the modules of dist/dist.yaml
// and committed, and the parity step of make check and of CI, which this command runs,
// fails when it differs (ADR-060 decision 20). The release lists no package under
// examples/ or test/, so the shipped table gains nothing (ADR-060 decision 38). The input
// of the test release, test/release/dist.yaml, has the same form: the release table plus
// the dispatcher, the instantiable echoer, the caller and teststate, with every binding
// and the required set of the release; its table generates too, and a module that
// declares no plugin is refused.
func TestReleaseTableIsGenerated(t *testing.T) {
	root := filepath.Join("..", "..")
	read := func(p string) []byte {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	input := read("dist/dist.yaml")
	if got, err := table(root, input); err != nil || !bytes.Equal(got, read("dist/modules.go")) {
		t.Fatalf("dist/modules.go is not what plugingen writes from dist/dist.yaml: %v", err)
	}
	rel, err := boot.ParseDist("dist.yaml", input)
	if err != nil || slices.ContainsFunc(rel.Modules, func(m string) bool { return strings.Contains(m, "/examples/") || strings.Contains(m, "/test/") }) {
		t.Fatalf("the release lists %v (%v)", rel.Modules, err)
	}
	data := read("test/release/dist.yaml")
	tst, err := boot.ParseDist("test/release/dist.yaml", data)
	if err != nil {
		t.Fatal(err)
	}
	toys := []string{modPath + "/examples/caller", modPath + "/examples/dispatch", modPath + "/examples/echoer", modPath + "/test/dist/plugins/teststate"}
	if want := slices.Sorted(slices.Values(slices.Concat(rel.Modules, toys))); !slices.Equal(tst.Modules, want) {
		t.Errorf("the test release lists %v, want %v", tst.Modules, want)
	}
	tb := slices.Concat(tst.Providers, tst.Consumers)
	for _, b := range slices.Concat(rel.Providers, rel.Consumers) {
		i := slices.IndexFunc(tb, func(x boot.Binding) bool { return x.Key == b.Key })
		if i < 0 || slices.ContainsFunc(b.Plugins, func(id string) bool { return !slices.Contains(tb[i].Plugins, id) }) {
			t.Errorf("the test release binds %s to less than the release's %v", b.Key, b.Plugins)
		}
	}
	if !reflect.DeepEqual(tst.SealedHooks, rel.SealedHooks) || !slices.Equal(tst.RequiredKeys, rel.RequiredKeys) || !slices.Equal(tst.RequiredRows, rel.RequiredRows) {
		t.Errorf("the test release has other sealed hooks or another required set: %+v", tst)
	}
	if src, err := table(root, data); err != nil || !bytes.Contains(src, []byte(`"`+modPath+`/test/dist/plugins/teststate"`)) {
		t.Errorf("the table of the test release: %v", err)
	}
	if _, err := table(root, []byte("modules: ["+modPath+"/boot]\n")); err == nil || !strings.Contains(err.Error(), modPath+"/boot declares no plugin") {
		t.Errorf("a module that declares no plugin: %v", err)
	}
}
