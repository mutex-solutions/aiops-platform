package main

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/mutex-solutions/aiops-platform/examples"
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
	"aiops/loader":               "loader",
	"aiops/clock":                "plugins/clock",
	"aiops/config-refs":          "plugins/config-refs",
	"aiops/readiness":            "plugins/readiness",
	"aiops/reload":               "plugins/reload",
	"aiops/restart-policy":       "plugins/restart-policy",
	"aiops-test/toy-provider":    "test/dist/plugins/toy-provider",
	"aiops-test/toy-consumer":    "test/dist/plugins/toy-consumer",
	"aiops-test/toy-spine":       "test/dist/plugins/toy-spine",
	"aiops-test/fail-apply":      "test/dist/plugins/fail-apply",
	"aiops-test/panic-apply":     "test/dist/plugins/panic-apply",
	"aiops-test/slow-apply":      "test/dist/plugins/slow-apply",
	"aiops-test/bad-config":      "test/dist/plugins/bad-config",
	"aiops-test/rogue-provider":  "test/dist/plugins/rogue-provider",
	"aiops-test/leaky-goroutine": "test/dist/plugins/leaky-goroutine",
	"aiops-test/slow-dispose":    "test/dist/plugins/slow-dispose",
	"aiops-test/test-state":      "test/dist/plugins/teststate",
	"aiops-test/test-submit":     "test/dist/plugins/testsubmit",
	"aiops/example-dispatch":     "examples/dispatch",
	"aiops/example-echoer":       "examples/echoer",
	"aiops/example-stamper":      "examples/stamper",
	"aiops/example-caller":       "examples/caller",
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
	for _, m := range testdist.Modules() { // the release table and the test plugins
		ids = append(ids, m.Plugin.ID)
	}
	for _, p := range examples.Plugins() {
		ids = append(ids, p.ID)
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
