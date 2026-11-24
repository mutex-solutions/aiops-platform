// SPDX-License-Identifier: Apache-2.0

package templates_test

import (
	"errors"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/mutex-solutions/aiops-platform/templates"
)

// The files of the Go template (07-c4-plugin-sdk.md:1551-1559; ADR-060 decision 24):
// the Spec in plugin.go, one stub per method under internal/, the suite and a solo test
// in plugin_test.go, fixtures/, the hand-written half of plugin.yaml, one disabled row in
// bundle.yaml, README.md, LICENSE, go.mod and a CI workflow. plugin check --write writes
// the generated half and the two generated tests.
var goFiles = map[string][]string{
	"echoers":  {"internal/echo/echo.go", "fixtures/echoers/README.md"},
	"stampers": {"internal/stamp/stamp.go", "fixtures/stampers/README.md"},
	"commands": {"internal/command/command.go", "fixtures/commands/README.md"},
}

var common = []string{"plugin.go", "plugin_test.go", "plugin.yaml", "bundle.yaml", "README.md", "LICENSE", "go.mod", ".github/workflows/check.yml"}

func TestGoTemplates(t *testing.T) {
	if !slices.Equal(templates.Registries, []string{"commands", "echoers", "stampers"}) {
		t.Errorf("Registries = %v, want a Go template for each registry of S2a", templates.Registries)
	}
	for reg, own := range goFiles {
		d, err := templates.NewData("acme/echo-loud", "v0.3.0", false)
		if err != nil {
			t.Fatal(err)
		}
		files, err := templates.Render(reg, d)
		if err != nil {
			t.Fatalf("%s: %v", reg, err)
		}
		want := slices.Sorted(slices.Values(append(slices.Clone(common), own...)))
		if got := slices.Sorted(maps.Keys(files)); !slices.Equal(got, want) {
			t.Errorf("%s writes %v, want %v", reg, got, want)
		}
		for name, b := range files {
			if !strings.HasSuffix(name, ".go") {
				continue
			}
			if _, err := parser.ParseFile(token.NewFileSet(), name, b, parser.AllErrors); err != nil {
				t.Errorf("%s/%s does not parse: %v", reg, name, err)
			}
			if f, err := format.Source(b); err != nil || string(f) != string(b) {
				t.Errorf("%s/%s is not gofmt-formatted", reg, name)
			}
		}
		for _, w := range []string{"id: acme/echo-loud", "publisher: {id: acme}", "license: Apache-2.0", "runtimes: [inproc]", "scope: [process]"} {
			if !strings.Contains(string(files["plugin.yaml"]), w) {
				t.Errorf("%s: plugin.yaml lacks %q:\n%s", reg, w, files["plugin.yaml"])
			}
		}
		if strings.Contains(string(files["plugin.yaml"]), "requires:") {
			t.Errorf("%s: plugin.yaml holds a generated field; plugin check --write writes them", reg)
		}
		if !strings.Contains(string(files["bundle.yaml"]), "plugin: acme/echo-loud") || !strings.Contains(string(files["bundle.yaml"]), "enabled: false") {
			t.Errorf("%s: bundle.yaml is not one disabled row of the plugin:\n%s", reg, files["bundle.yaml"])
		}
		for _, w := range []string{"module example.com/acme/echo-loud", "github.com/mutex-solutions/aiops-platform/sdk v0.3.0", "github.com/mutex-solutions/aiops-platform/sdk/testkit v0.3.0"} {
			if !strings.Contains(string(files["go.mod"]), w) {
				t.Errorf("%s: go.mod lacks %q:\n%s", reg, w, files["go.mod"])
			}
		}
		if strings.Contains(string(files["go.mod"]), "replace") {
			t.Errorf("%s: go.mod holds a replace line", reg)
		}
		if !strings.Contains(string(files[".github/workflows/check.yml"]), "aiopsctl plugin check ./") {
			t.Errorf("%s: the workflow does not run the check", reg)
		}
		if !strings.Contains(string(files["README.md"]), "-race") {
			t.Errorf("%s: the README does not say that T1 runs under -race, with cgo and a C compiler", reg)
		}
		if !strings.Contains(string(files["LICENSE"]), "Apache License") {
			t.Errorf("%s: LICENSE is not Apache-2.0", reg)
		}
	}
}

// --contrib writes the owner contrib and a module of its own under contrib/ of this
// repository (08-profiles-roadmap-adrs.md:407; ADR-048 decision 23).
func TestContrib(t *testing.T) {
	d, err := templates.NewData("acme/echo-loud", "v0.3.0", true)
	if err != nil {
		t.Fatal(err)
	}
	files, err := templates.Render("echoers", d)
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != "contrib/echo-loud" || !strings.Contains(string(files["plugin.yaml"]), "id: contrib/echo-loud") ||
		!strings.Contains(string(files["go.mod"]), "module github.com/mutex-solutions/aiops-platform/contrib/echo-loud") {
		t.Errorf("contrib: %+v\n%s\n%s", d, files["plugin.yaml"], files["go.mod"])
	}
}

// The connector template writes a pack with no code: runtimes: [], a README, LICENSE,
// fixtures/README.md and the workflow; plugin new writes its bundle from the release
// (ADR-047 decision 12).
func TestConnectorTemplate(t *testing.T) {
	d, err := templates.NewData("acme/connector-example", "v0.3.0", false)
	if err != nil {
		t.Fatal(err)
	}
	files, err := templates.Render("connector", d)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".github/workflows/check.yml", "LICENSE", "README.md", "fixtures/README.md", "plugin.yaml"}
	if got := slices.Sorted(maps.Keys(files)); !slices.Equal(got, want) {
		t.Errorf("connector writes %v, want %v", got, want)
	}
	if !strings.Contains(string(files["plugin.yaml"]), "runtimes: []") {
		t.Errorf("plugin.yaml of a pack:\n%s", files["plugin.yaml"])
	}
}

// The workflow of a generated plugin, its steps run as a runner runs them on a machine
// with no aiopsctl on PATH, fails before plugin check with exit 1 and a line that names
// the install of aiopsctl, and not with the 127 of a command not found
// (07-c4-plugin-sdk.md:1559).
func TestWorkflowNamesTheInstall(t *testing.T) {
	for _, reg := range []string{"echoers", templates.Connector} {
		d, err := templates.NewData("acme/echo-loud", "v0.3.0", false)
		if err != nil {
			t.Fatal(err)
		}
		files, err := templates.Render(reg, d)
		if err != nil {
			t.Fatal(err)
		}
		// The run of each step, a plain scalar on its line or a literal block below it.
		var runs []string
		lines := strings.Split(string(files[".github/workflows/check.yml"]), "\n")
		for i, l := range lines {
			indent, run, ok := strings.Cut(l, "run: ")
			if !ok || strings.Trim(indent, " -") != "" {
				continue
			}
			block := run == "|"
			for _, b := range lines[i+1:] {
				if !block || strings.TrimSpace(b) != "" && len(b)-len(strings.TrimLeft(b, " ")) <= len(indent) {
					break
				}
				run += "\n" + b
			}
			runs = append(runs, strings.TrimPrefix(run, "|"))
		}
		code, out := 0, []byte(nil)
		for _, run := range runs {
			cmd := exec.Command("/bin/bash", "--noprofile", "--norc", "-eo", "pipefail", "-c", run)
			cmd.Dir, cmd.Env = t.TempDir(), []string{"PATH=/usr/bin:/bin"}
			out, err = cmd.CombinedOutput()
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				code = ee.ExitCode()
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		if code != 1 || !strings.Contains(string(out), "install") || !strings.Contains(string(out), "aiopsctl") {
			t.Errorf("%s: the workflow exits %d, printing %q; want exit 1 and a line that names the install of aiopsctl", reg, code, out)
		}
	}
}

// An unknown registry, a language other than Go, a malformed id and a reserved owner
// are refused.
func TestRefusals(t *testing.T) {
	d, _ := templates.NewData("acme/x", "v0.3.0", false)
	if _, err := templates.Render("tools", d); err == nil {
		t.Error("a registry with no template is rendered")
	}
	for _, id := range []string{"x", "Acme/x", "aiops/x", "contrib/x", "acme/"} {
		if _, err := templates.NewData(id, "v0.3.0", false); err == nil {
			t.Errorf("NewData(%q) = nil error", id)
		}
	}
}

// The Go tool builds no file of a template: every embedded file but the package's own
// carries the suffix .tmpl (07-c4-plugin-sdk.md:1549).
func TestNoTemplateFileIsGo(t *testing.T) {
	err := fs.WalkDir(templates.FS, ".", func(p string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() && !strings.HasSuffix(p, ".tmpl") {
			t.Errorf("%s does not end in .tmpl", p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
