// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
)

// The served copy of each go.mod of the repository, the root module's and the three SDK
// modules', names the version in its require lines of the repository's modules and
// holds no replace line, and differs from the repository's file in nothing else
// (ADR-060 decision 21; ADR-044 decision 23).
func TestServedGoMod(t *testing.T) {
	const version = "v0.2.0-s2a.1"
	for _, dir := range []string{".", "sdk", "sdk/kernel", "sdk/testkit"} {
		orig, err := os.ReadFile(filepath.Join("..", "..", dir, "go.mod"))
		if err != nil {
			t.Fatal(err)
		}
		served, err := Served(orig, version)
		if err != nil {
			t.Fatal(err)
		}
		f, err := modfile.Parse("go.mod", served, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(f.Replace) > 0 {
			t.Errorf("%s: the served copy keeps %d replace lines", dir, len(f.Replace))
		}
		for _, r := range f.Require {
			if strings.HasPrefix(r.Mod.Path+"/", rootModule+"/") && r.Mod.Version != version {
				t.Errorf("%s: the served copy requires %s %s", dir, r.Mod.Path, r.Mod.Version)
			}
		}
		if a, b := rest(string(orig)), rest(string(served)); a != b {
			t.Errorf("%s: the served copy differs outside its repository requirements and replace lines:\n%s\n----\n%s", dir, a, b)
		}
	}
}

// The comparison of TestServedGoMod sees any change of a served copy but the versions
// of its repository requirements and its replace lines: another module line, a comment
// that names the repository, another go line and another requirement each differ
// (ADR-060 decision 21).
func TestServedComparison(t *testing.T) {
	orig, err := os.ReadFile(filepath.Join("..", "..", "sdk", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Served(orig, "v0.2.0-s2a.1")
	if err != nil {
		t.Fatal(err)
	}
	served := string(b)
	if rest(string(orig)) != rest(served) {
		t.Fatalf("the served copy differs:\n%s", served)
	}
	for _, bad := range []string{
		strings.Replace(served, "module "+rootModule+"/sdk\n", "module "+rootModule+"/sdk/evil\n", 1),
		served + "\n// served from " + rootModule + "/sdk\n",
		strings.Replace(served, "\ngo ", "\ngo 1.99\n// go ", 1),
		served + "\nrequire example.test/extra v1.0.0\n",
	} {
		if bad == served || rest(string(orig)) == rest(bad) {
			t.Errorf("the comparison does not see:\n%s", bad)
		}
	}
}

// rest returns a go.mod as it parses, formatted, with no replace directive and the
// version of each requirement of a module of the repository set to v0.0.0, so that a
// repository file and its served copy compare in everything else: the module line,
// the go and toolchain lines, every other requirement, exclude and retract, and the
// comments.
func rest(gomod string) string {
	f, err := modfile.Parse("go.mod", []byte(gomod), nil)
	if err != nil {
		return "does not parse: " + err.Error()
	}
	for _, r := range slices.Clone(f.Replace) {
		_ = f.DropReplace(r.Old.Path, r.Old.Version)
	}
	for _, r := range f.Require {
		if slices.Contains(sdkModules, r.Mod.Path) {
			_ = f.AddRequire(r.Mod.Path, "v0.0.0")
		}
	}
	f.Cleanup()
	b, err := f.Format()
	if err != nil {
		return "does not format: " + err.Error()
	}
	return string(b)
}
