// SPDX-License-Identifier: Apache-2.0

package boot

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"unicode"
)

// The lints of step 1 of plugin check that need data beside the manifest (ADR-048
// decision 15; 07-c4-plugin-sdk.md:1567): the toolbox's ReadManifest runs them on a
// manifest that the strict reader accepted. The SPDX list, the marks and the tier words
// are data that aiopsctl hands in, since the marks name upstreams, which boot's word
// list forbids (ADR-061 decision 21); with none, as in aiops, only the reserved owners
// are linted.

// ManifestLint is the data of the lints of step 1: the ids of the SPDX license list and
// its exceptions; the marks of 00-decisions.md §8.1, which no id, title, content path
// or command path holds; and the words that the name and title of a third-party plugin
// never hold (08-profiles-roadmap-adrs.md:1368, T7). Each mark and word is lowercase.
type ManifestLint struct {
	Licenses     map[string]bool
	Marks, Tiers []string
}

// rootModule is the module path of the release, the one module in which an aiops id
// passes step 1 (ADR-048 decision 23).
const rootModule = "github.com/mutex-solutions/aiops-platform"

// lint returns the findings of step 1 on m, the manifest in dir of files.
func (l ManifestLint) lint(files fs.FS, dir string, m Manifest) []string {
	out := reservedOwner(files, dir, m)
	if l.Licenses != nil {
		exprs := []string{m.License}
		for _, in := range m.Integrates {
			exprs = append(exprs, in.License)
		}
		for _, e := range exprs {
			for _, id := range strings.FieldsFunc(e, func(r rune) bool { return r == ' ' || r == '(' || r == ')' }) {
				id = strings.TrimSuffix(id, "+")
				if !slices.Contains([]string{"AND", "OR", "WITH"}, id) && !strings.Contains(id, "LicenseRef-") && !l.Licenses[id] {
					out = append(out, fmt.Sprintf("license: %s is not an id of the SPDX license list that aiopsctl embeds", id))
				}
			}
		}
	}
	named := [][2]string{{"id", m.ID}, {"title", m.Title}}
	_ = fs.WalkDir(files, path.Join(dir, "content"), func(p string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			named = append(named, [2]string{"content path", strings.TrimPrefix(p, dir+"/content/")})
		}
		return nil
	})
	if raw, err := fs.ReadFile(files, path.Join(dir, "fixtures/commands/paths.json")); err == nil {
		var paths []struct{ Path string }
		if err := json.Unmarshal(raw, &paths); err != nil {
			out = append(out, "fixtures/commands/paths.json: "+err.Error())
		}
		for _, p := range paths {
			named = append(named, [2]string{"command path", p.Path})
		}
	}
	for _, n := range named {
		for _, mark := range l.Marks {
			if slices.Contains(words(n[1]), mark) {
				out = append(out, fmt.Sprintf("the %s %q holds the mark %s (00-decisions.md §8.1)", n[0], n[1], mark))
			}
		}
	}
	owner, name, _ := strings.Cut(m.ID, "/")
	if owner == "aiops" || owner == "contrib" {
		return out
	}
	for _, in := range m.Integrates {
		for _, tier := range l.Tiers {
			for _, n := range [][2]string{{"name", name}, {"title", m.Title}} {
				if slices.Contains(words(n[1]), tier) {
					out = append(out, fmt.Sprintf("the %s %q holds %q, which a third-party name never holds (T7)", n[0], n[1], tier))
				}
			}
		}
		if !strings.HasSuffix(name, "-"+in.System) || strings.Count(name, in.System) != 1 {
			out = append(out, fmt.Sprintf("the name %q of a plugin that integrates %s is not <contribution noun>-%s or connector-%s, with the system once, after the noun (T7)", name, in.System, in.System, in.System))
		}
		if !strings.Contains(strings.ToLower(m.Title), " for "+in.System) {
			out = append(out, fmt.Sprintf("the title %q does not read \"... for %s\", with the mark after \"for\" (T7)", m.Title, in.System))
		}
	}
	return out
}

// words returns the lowercase words of s, split at every rune that is not a letter or a
// digit.
// ponytail: a mark is found as a whole word, so a combined form such as two marks run
// together passes; a substring match would flag ordinary words that hold a mark.
func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// reservedOwner is the clause of step 1 on the reserved owners, decided by where the
// code is (ADR-048 decision 23): an aiops id passes only in the root module of the
// release, and a contrib id only in a module under its contrib/ path or, for a pack
// with no code, in a directory under contrib/ below the root module's go.mod.
func reservedOwner(files fs.FS, dir string, m Manifest) []string {
	owner, _, _ := strings.Cut(m.ID, "/")
	if owner != "aiops" && owner != "contrib" {
		return nil
	}
	mod, root := moduleOf(files, dir)
	switch {
	case mod == "":
		return []string{fmt.Sprintf("the owner %s is reserved to the release pipeline, and no go.mod at or above the plugin names its module", owner)}
	case owner == "aiops" && mod == rootModule,
		owner == "contrib" && strings.HasPrefix(mod, rootModule+"/contrib/"),
		owner == "contrib" && mod == rootModule && len(m.Runtimes) == 0 && strings.HasPrefix(dir, path.Join(root, "contrib")+"/"):
		return nil
	}
	return []string{fmt.Sprintf("the owner %s is reserved to the release pipeline and passes in the module %s only, not in %s (ADR-048 decision 23)", owner, map[string]string{"aiops": rootModule, "contrib": rootModule + "/contrib/<name>"}[owner], mod)}
}

// moduleOf returns the module path of the go.mod nearest at or above dir in files, and
// its directory; "" when there is none.
func moduleOf(files fs.FS, dir string) (string, string) {
	for d := dir; ; d = path.Dir(d) {
		if b, err := fs.ReadFile(files, path.Join(d, "go.mod")); err == nil {
			s := bufio.NewScanner(bytes.NewReader(b))
			for s.Scan() {
				if p, ok := strings.CutPrefix(strings.TrimSpace(s.Text()), "module "); ok {
					return strings.TrimSpace(p), d
				}
			}
		}
		if d == "." {
			return "", ""
		}
	}
}
