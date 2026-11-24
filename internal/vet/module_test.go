// SPDX-License-Identifier: Apache-2.0

package vet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// put writes each file under root, by slash path.
func put(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const sdk = "github.com/mutex-solutions/aiops-platform"

// Step 3 of plugin check runs the import analyzer and plugin vet of make check on a
// plugin of any module, with no checkout of this repository (ADR-060 decision 23;
// 07-c4-plugin-sdk.md:1569): the plugin directory may sit below its module root, an
// import is judged against the module's own path, and each finding names its file,
// relative to the plugin directory, and its line.
func TestModuleRunsTheAnalyzerAndVetFromAnyModuleRoot(t *testing.T) {
	root := t.TempDir()
	put(t, root, map[string]string{
		"go.mod":            "module example.com/acme\n\ngo 1.26.0\n",
		"echo/plugin.yaml":  "id: acme/echo\n",
		"other/plugin.yaml": "id: acme/other\n",
		"other/plugin.go":   "package other\n",
		"shared/shared.go":  "package shared\n",
		"echo/plugin.go": `package echo

import (
	_ "` + sdk + `/sdk/def/echoers/v0/fake"
	_ "` + sdk + `/sdk/kernel"
	_ "` + sdk + `/plugins/clock"
	_ "example.com/acme/echo/internal/client"
	_ "example.com/acme/other"
	_ "example.com/acme/shared"
)

func init() {}
`,
		"echo/extra.go":                  "package echo\n",
		"echo/internal/client/client.go": "package client\n\nfunc start(f func()) { go f() }\n",
		"echo/plugin_test.go":            "package echo_test\n\nimport _ \"" + sdk + "/sdk/def/echoers/v0/fake\"\n",
		"echo/testdata/x/x.go":           "package x\n\nfunc init() {}\n",
	})
	got, err := Module(filepath.Join(root, "echo"))
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, f := range got {
		if f.File == "" || f.Line == 0 {
			t.Errorf("a finding with no file or line: %+v", f)
		}
		lines = append(lines, f.String())
	}
	report := strings.Join(lines, "\n")
	for _, want := range []string{
		"plugin.go:4: imports " + sdk + "/sdk/def/echoers/v0/fake: no fake or sdk/testkit package in a non-test file",
		"plugin.go:6: imports " + sdk + "/plugins/clock: a plugin imports",
		"plugin.go:8: imports example.com/acme/other: a plugin imports",
		"plugin.go:12: init function",
		"extra.go:1: everything of a plugin but plugin.go is under internal/",
		"internal/client/client.go:3: bare go statement",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("no finding %q in:\n%s", want, report)
		}
	}
	if len(got) != 6 {
		t.Errorf("%d findings, want 6:\n%s", len(got), report)
	}
	for _, clean := range []string{"plugin.go:5:", "plugin.go:7:", "plugin.go:9:", "plugin_test.go", "testdata"} {
		if strings.Contains(report, clean) {
			t.Errorf("a finding on %s, which the rules allow:\n%s", clean, report)
		}
	}
}

// A plugin at its module root, as plugin new writes it, with every import the rules
// allow, has no finding.
func TestModuleOfACleanPlugin(t *testing.T) {
	root := t.TempDir()
	put(t, root, map[string]string{
		"go.mod":      "module example.com/acme/echo\n\ngo 1.26.0\n",
		"plugin.yaml": "id: acme/echo\n",
		"plugin.go": `package echo

import (
	"errors"

	_ "` + sdk + `/sdk/def/echoers/v0"
	_ "` + sdk + `/sdk/plugin"
	_ "example.com/acme/echo/internal/client"
	_ "example.org/vendor/client"
)

var ErrX = errors.New("x")
`,
		"internal/client/client.go": "package client\n",
		"plugin_test.go":            "package echo_test\n\nimport _ \"" + sdk + "/sdk/testkit/kerneltest\"\n",
	})
	got, err := Module(root)
	if err != nil || len(got) != 0 {
		t.Errorf("Module = %v, %v; want no finding", got, err)
	}
}

// A directory with no go.mod at or above it, or a file that does not parse, is an
// error, not a finding.
func TestModuleErrors(t *testing.T) {
	root := t.TempDir()
	put(t, root, map[string]string{"p/plugin.go": "package p\n"})
	if _, err := Module(filepath.Join(root, "p")); err == nil || !strings.Contains(err.Error(), "go.mod") {
		t.Errorf("no go.mod: %v", err)
	}
	put(t, root, map[string]string{"go.mod": "go 1.26.0\n"})
	if _, err := Module(filepath.Join(root, "p")); err == nil || !strings.Contains(err.Error(), "module") {
		t.Errorf("a go.mod with no module line: %v", err)
	}
	put(t, root, map[string]string{"go.mod": "module example.com/p\n", "p/bad.go": "package"})
	if _, err := Module(filepath.Join(root, "p")); err == nil {
		t.Error("a file that does not parse is no error")
	}
	if got, err := Module(filepath.Join(root, "missing")); err == nil {
		t.Errorf("a missing directory: %v", got)
	}
}
