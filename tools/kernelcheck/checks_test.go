package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const mp = "github.com/mutex-solutions/aiops-platform"

func hasPath(p, prefix string) bool { return p == prefix || strings.HasPrefix(p, prefix+"/") }

func imp(paths ...string) string {
	s := "package p\n\nimport (\n"
	for _, p := range paths {
		s += "\t_ \"" + p + "\"\n"
	}
	return s + ")\n"
}

func TestLinesProvisionalAndTestdata(t *testing.T) {
	d := t.TempDir()
	write(t, d, "a.go", "package k\n")
	write(t, d, "testdata/x/b.go", "package x\n")
	if err := checkLines(d, 5, true); err != nil {
		t.Fatalf("testdata must be skipped: %v", err)
	}
	write(t, d, "sub/c.go", "package sub\n")
	if err := checkLines(d, 5, true); err == nil {
		t.Fatal("subpackage accepted")
	}
}

// One cap may hold two directories, each one package, as the launcher's two do under
// the one row of cmd/aiopsctl (ADR-060 decision 35): their lines add up.
func TestLinesOfTwoDirectories(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	write(t, a, "a.go", "package a\n\n")
	write(t, b, "b.go", "package b\n\n")
	if err := run([]string{"lines", a + "," + b, "4", "provisional"}); err != nil {
		t.Fatalf("two directories of two lines each under a cap of 4: %v", err)
	}
	if err := run([]string{"lines", a + "," + b, "3", "provisional"}); err == nil {
		t.Fatal("two directories of two lines each under a cap of 3 passed")
	}
}

// The imports command reads the register it is given, with every key of an entry, and
// fails when it cannot.
func TestRunImportsReadsTheRegister(t *testing.T) {
	d, r := t.TempDir(), t.TempDir()
	reg := filepath.Join(r, "register.yaml")
	write(t, r, "register.yaml", "entries:\n  - id: y\n    names: [y]\n    form: library\n    modules: [example.org/y]\n    importers:\n      - {dir: boot, decision: an accepted decision}\nexceptions: []\n")
	write(t, d, "boot/b.go", imp("example.org/y/z"))
	if err := run([]string{"imports", d, reg}); err != nil {
		t.Fatalf("an importer the register lists is refused: %v", err)
	}
	write(t, d, "plugins/p/internal/x/x.go", imp("example.org/y/z"))
	if err := run([]string{"imports", d, reg}); err == nil || !strings.Contains(err.Error(), "register entry y") {
		t.Fatalf("an import outside the importers of the register: %v", err)
	}
	write(t, r, "bad.yaml", "entries: {\n")
	for _, p := range []string{filepath.Join(r, "missing.yaml"), filepath.Join(r, "bad.yaml")} {
		if run([]string{"imports", d, p}) == nil {
			t.Errorf("imports with the register %s succeeded", p)
		}
	}
}

func TestRunNewCommands(t *testing.T) {
	d := t.TempDir()
	write(t, d, "boot/a.go", "package boot\n")
	if err := run([]string{"imports", d, filepath.Join("..", "upstream-components.yaml")}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"pluginvet", d}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"lines", filepath.Join(d, "boot"), "5", "provisional"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"lines", filepath.Join(d, "boot"), "-1"}); err == nil {
		t.Error("negative cap accepted")
	}
	for _, a := range [][]string{{"pluginvet"}, {"imports"}, {"imports", d}, {"lines", d, "5", "other"}} {
		if run(a) == nil {
			t.Errorf("run(%v) succeeded", a)
		}
	}
	if _, err := os.Stat(d); err != nil {
		t.Fatal(err)
	}
}
