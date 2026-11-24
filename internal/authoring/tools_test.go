// SPDX-License-Identifier: Apache-2.0

package authoring

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
)

// Template answers the files of the template of a registry for a plugin id, and refuses
// a registry with no template and a language other than Go (ADR-060 decision 24).
func TestTemplate(t *testing.T) {
	tl := Tools{SDK: "v0.3.0"}
	files, err := tl.Template(t.Context(), launch.TemplateRequest{Registry: "echoers", ID: "acme/echo", Lang: "go"})
	if err != nil || !strings.Contains(string(files["plugin.go"]), `const ID = "acme/echo"`) || !strings.Contains(string(files["go.mod"]), "sdk v0.3.0") {
		t.Fatalf("Template = %d files, %v", len(files), err)
	}
	files, err = tl.Template(t.Context(), launch.TemplateRequest{Registry: "connector", ID: "acme/connector-x"})
	if err != nil || !strings.Contains(string(files["plugin.yaml"]), "runtimes: []") {
		t.Errorf("the connector template: %v", err)
	}
	for _, q := range []launch.TemplateRequest{{Registry: "tools", ID: "acme/x", Lang: "go"}, {Registry: "echoers", ID: "acme/x", Lang: "python"}, {Registry: "echoers", ID: "x", Lang: "go"}} {
		if _, err := tl.Template(t.Context(), q); err == nil {
			t.Errorf("Template(%+v) = nil error", q)
		}
	}
}

// Vet is step 3: the import analyzer and plugin vet of the shared package on a plugin of
// any module, and for every artifact the refusal of a dashboard-server plugin.json
// (08-profiles-roadmap-adrs.md:391).
func TestVet(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":                    "module example.com/acme/echo\n\ngo 1.26.0\n",
		"plugin.yaml":               "id: acme/echo\n",
		"plugin.go":                 "package echo\n\nfunc init() {}\n",
		"content/board/plugin.json": "{}",
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ds, err := Tools{}.Vet(t.Context(), dir)
	if err != nil || len(ds) != 2 {
		t.Fatalf("Vet = %+v, %v; want the init function and the plugin.json", ds, err)
	}
	for _, d := range ds {
		if d.Check != "3" || d.File == "" || d.Warning {
			t.Errorf("a finding of step 3 with no file: %+v", d)
		}
	}
	pack := t.TempDir()
	if err := os.WriteFile(filepath.Join(pack, "plugin.yaml"), []byte("id: acme/pack\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ds, err := (Tools{}).Vet(t.Context(), pack); err != nil || len(ds) != 0 {
		t.Errorf("a pack with no code: %+v, %v", ds, err)
	}
}

// The lint data that aiopsctl hands to boot: the SPDX license list it embeds, licenses
// and exceptions, at the version its register entry pins; the marks of
// 00-decisions.md §8.1, each named in that paragraph; and the tier words of T7.
func TestLint(t *testing.T) {
	l := Lint()
	for _, id := range []string{"Apache-2.0", "MIT", "BSD-3-Clause", "GPL-2.0-only", "Classpath-exception-2.0", "LLVM-exception"} {
		if !l.Licenses[id] {
			t.Errorf("the SPDX list lacks %s", id)
		}
	}
	if len(l.Licenses) != 740+86 || l.Licenses["Apache"] || SPDXVersion != "3.29.0" {
		t.Errorf("the SPDX list holds %d ids at %s; want 740 licenses and 86 exceptions of 3.29.0", len(l.Licenses), SPDXVersion)
	}
	doc, err := os.ReadFile("../../docs/architecture/00-decisions.md")
	if err != nil {
		t.Fatal(err)
	}
	var marks string
	for _, line := range strings.Split(string(doc), "\n") {
		if strings.HasPrefix(line, "**Trademarks.**") {
			marks = strings.ToLower(line)
		}
	}
	if len(l.Marks) < 9 {
		t.Errorf("marks %v", l.Marks)
	}
	for _, m := range l.Marks {
		if !strings.Contains(marks, m) {
			t.Errorf("the mark %s is not named in the trademark rule of 00-decisions.md §8.1", m)
		}
	}
	for _, w := range []string{"official", "certified"} {
		if !strings.Contains(strings.Join(l.Tiers, " "), w) {
			t.Errorf("the tier words %v lack %s", l.Tiers, w)
		}
	}
}

// A pack's suite and scenario run from the test binaries that the release build embeds
// (ADR-060 decision 46); an aiopsctl that embeds none says so, and that it is not carried.
func TestPackSuiteNotCarried(t *testing.T) {
	tl := Tools{}
	dir := t.TempDir()
	if _, err := tl.Suite(t.Context(), launch.SuiteRequest{Dir: dir, Definitions: map[string]string{"echoers": "0.1"}}); !errors.Is(err, launch.ErrNotCarried) || !strings.Contains(err.Error(), "echoers") {
		t.Errorf("Suite of a pack with no embedded binary: %v", err)
	}
	if _, err := tl.Scenario(t.Context(), launch.ScenarioRequest{Dir: dir, Name: "echo"}); !errors.Is(err, launch.ErrNotCarried) || !strings.Contains(err.Error(), "echo") {
		t.Errorf("Scenario with no embedded binary: %v", err)
	}
}
