// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"os"
	"path/filepath"
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

// rest returns a go.mod without its replace lines and the lines that name a module of
// the repository, so that a repository file and its served copy compare.
func rest(gomod string) string {
	var out []string
	inReplace := false
	for _, l := range strings.Split(gomod, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "replace ("):
			inReplace = true
		case inReplace && t == ")":
			inReplace = false
		case inReplace || strings.HasPrefix(t, "replace ") || strings.Contains(t, rootModule+"/") || strings.HasPrefix(t, "require "+rootModule+" "),
			t == "":
		default:
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}
