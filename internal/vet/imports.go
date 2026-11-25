// SPDX-License-Identifier: Apache-2.0

package vet

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

const modPath = "github.com/mutex-solutions/aiops-platform"

// Document references cited in every report line (file:line of docs/architecture
// and docs/adr at the time of writing; the text of the rule is what counts).
const (
	docKernel  = "07-c4-plugin-sdk.md:27"
	docDef     = "07-c4-plugin-sdk.md:28"
	docSDKPkg  = "07-c4-plugin-sdk.md:29"
	docPlugin  = "07-c4-plugin-sdk.md:34,46; 08-profiles-roadmap-adrs.md:1255"
	docExample = "07-c4-plugin-sdk.md:36,42"
	docBoot    = "07-c4-plugin-sdk.md:37-38; 08-profiles-roadmap-adrs.md:1254"
	docU2      = "ADR-054 decision 13"
	docFake    = "07-c4-plugin-sdk.md:33,47; 08-profiles-roadmap-adrs.md:1258"
	docLink    = "ADR-060 decision 28; ADR-059 decision 9"
	docMain    = "ADR-061 decision 28"
	docTools   = "ADR-061 decision 21; ADR-044, Consequences"
	docModZip  = "ADR-044, Consequences; the owner's decision of 2026-11-24"
)

// toolbox are the packages that aiopsctl alone links and hands to boot (ADR-061
// decision 21): plugingen, the import analyzer and plugin vet, the suites, the runner
// of the suite binaries it embeds (ADR-060 decision 46) and the templates, the
// builder, and the launcher of aiopsctl; builderLibs are the modules
// that only the builder links (ADR-044, Consequences).
var (
	toolbox     = []string{"internal/plugingen", "internal/vet", "internal/authoring", "internal/packsuite", "templates", "internal/builder", "internal/aiopsctl"}
	builderLibs = []string{"golang.org/x/tools", "github.com/google/licenseclassifier/v2", "github.com/sergi/go-diff", "github.com/davecgh/go-spew"}
)

func isToolbox(p string) bool {
	for _, t := range toolbox {
		if hasPath(p, modPath+"/"+t) {
			return true
		}
	}
	for _, m := range builderLibs {
		if hasPath(p, m) {
			return true
		}
	}
	return false
}

// isModuleZip reports a package of golang.org/x/mod that the module zip of the toolbox
// links, which enters no server binary: every package of the module but semver and
// sumdb/note, which sigstore-go links into both binaries (ADR-047 decision 4).
func isModuleZip(p string) bool {
	return hasPath(p, "golang.org/x/mod") && p != "golang.org/x/mod/semver" && p != "golang.org/x/mod/sumdb/note"
}

// bootDefs are the only definitions boot/ and loader/ may import.
var bootDefs = []string{"launch", "loader", "composition"}

// Upstream is what the import boundary U2 reads of an entry of the upstream register
// tools/upstream-components.yaml (ADR-054 decisions 3 and 13): the modules the upstream
// publishes, and where they may be imported, the directories of its plugins and its
// importers. The other clause of U2, the require lists of the SDK modules, is the
// modfile check.
type Upstream struct {
	ID        string
	Modules   []string
	Plugins   []string
	Importers []ImportDir
}

type ImportDir struct{ Dir string }

// ReadRegister reads the entries of the upstream register at path.
func ReadRegister(path string) ([]Upstream, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r struct{ Entries []Upstream }
	if err := yaml.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return r.Entries, nil
}

// upstreamReason is U2 for the import imp, from outside the repository, of the file
// rel: the entry whose modules hold imp, by the longest module path, lists the
// directory of rel among its plugins or its importers. A module in no entry fails, so
// a module of another project that an entry brings in is imported only once it has an
// entry of its own (ADR-054 decision 18).
func upstreamReason(reg []Upstream, rel, imp string) string {
	var e *Upstream
	n := 0
	for i := range reg {
		for _, m := range reg[i].Modules {
			if hasPath(imp, m) && len(m) > n {
				e, n = &reg[i], len(m)
			}
		}
	}
	if e == nil {
		return "U2: its module is in no entry's modules of the upstream register (" + docU2 + ")"
	}
	for _, p := range e.Plugins {
		if hasPath(rel, "plugins/"+path.Base(p)) {
			return ""
		}
	}
	for _, i := range e.Importers {
		if hasPath(rel, i.Dir) {
			return ""
		}
	}
	return "U2: imported outside the plugins and importers of its register entry " + e.ID + " (" + docU2 + ")"
}

// fakeHomes may import a fake or testkit package from non-test files: the fakes
// and suites themselves, the testkit, test trees, and the aiopsctl check runner,
// which carries the fakes by design (07-c4-plugin-sdk.md:33,
// 01-c1-system-context.md:279).
var fakeHomes = []string{"sdk/def/", "sdk/testkit/", "test/", "cmd/aiopsctl/"}

func isStd(p string) bool { return !strings.Contains(strings.SplitN(p, "/", 2)[0], ".") }

func hasPath(p, prefix string) bool { return p == prefix || strings.HasPrefix(p, prefix+"/") }

func isKernel(p string) bool    { return hasPath(p, modPath+"/sdk/kernel") }
func isTestkit(p string) bool   { return hasPath(p, modPath+"/sdk/testkit") }
func isSDKPlugin(p string) bool { return hasPath(p, modPath+"/sdk/plugin") }

func isFakePath(p string) bool {
	if isTestkit(p) {
		return true
	}
	for _, s := range strings.Split(p, "/") {
		if s == "fake" {
			return true
		}
	}
	return false
}

// isDef reports whether p is under sdk/def, or under one of the named definitions.
func isDef(p string, names ...string) bool {
	if len(names) == 0 {
		return hasPath(p, modPath+"/sdk/def")
	}
	for _, n := range names {
		if hasPath(p, modPath+"/sdk/def/"+n) {
			return true
		}
	}
	return false
}

// importReasons returns one reason per rule that the import breaks. rel is the
// slash path of the file below the repository root. An import from outside the
// repository is U2's to judge in boot/ and in a plugin; the other rules refuse it
// whatever the register lists.
func importReasons(rel string, test bool, imp string, reg []Upstream) []string {
	var out []string
	std := isStd(imp)
	third := !std && !hasPath(imp, modPath)
	add := func(rule, doc string) { out = append(out, rule+" ("+doc+")") }
	seg := strings.Split(rel, "/")
	tk := test && isTestkit(imp)
	if test && imp == modPath+"/"+path.Dir(rel) {
		return nil // an external test of the package in its own directory, as plugingen writes
	}
	switch {
	case hasPath(rel, "sdk/kernel"):
		if !std {
			add("sdk/kernel imports the standard library only, tests included", docKernel)
		}
	case hasPath(rel, "sdk/def"):
		if !(std || isKernel(imp) || isDef(imp) || tk) {
			add("sdk/def imports the standard library and sdk/kernel only (sdk/testkit too in tests)", docDef)
		}
	case hasPath(rel, "sdk/plugin"):
		if !(std || isKernel(imp) || isDef(imp) || isSDKPlugin(imp) || tk) {
			add("sdk/plugin imports the standard library, sdk/kernel and sdk/def only (sdk/testkit too in tests)", docSDKPkg)
		}
	case hasPath(rel, "boot"):
		if !(std || isKernel(imp) || isDef(imp, bootDefs...) || third || tk) {
			add("boot imports the standard library, sdk/kernel, sdk/def/{launch,loader,composition} and the upstream modules the register lists it for only; nothing from plugins, loader, cmd, test, dist", docBoot)
		}
	case hasPath(rel, "loader"):
		if !(std || isKernel(imp) || isDef(imp, bootDefs...) || tk) {
			add("loader imports the standard library, sdk/kernel and sdk/def/{launch,loader,composition} only; nothing from plugins, boot, cmd, test, dist", docBoot)
		}
	case hasPath(rel, "plugins") && len(seg) > 2:
		if !(std || isKernel(imp) || isDef(imp) || isSDKPlugin(imp) || hasPath(imp, modPath+"/plugins/"+seg[1]) || third || tk) {
			add("a plugin imports the SDK, its own packages and the upstream modules the register lists it for only; never another plugin, boot, loader, cmd, test, dist", docPlugin)
		}
	case hasPath(rel, "examples"):
		// The tests, and the list examples/examples.go that the definition lint reads,
		// may also import the other examples.
		own := len(seg) > 2 && hasPath(imp, modPath+"/examples/"+seg[1])
		all := (test || rel == "examples/examples.go") && hasPath(imp, modPath+"/examples")
		if !(std || isKernel(imp) || isDef(imp) || isSDKPlugin(imp) || own || all || tk) {
			add("an example imports the SDK and its own packages only (the other examples too in tests and in examples/examples.go); never internal, a plugin, boot, loader, cmd, test, dist", docExample)
		}
	}
	if hasPath(rel, "cmd") && isDef(imp, "commands", "operations") {
		add("no command code in a main: a package under cmd/ imports neither sdk/def/commands nor sdk/def/operations, tests included; every command is a row", docMain)
	}
	if third {
		if r := upstreamReason(reg, rel, imp); r != "" {
			out = append(out, r)
		}
	}
	if !test && isFakePath(imp) {
		ok := false
		for _, h := range fakeHomes {
			ok = ok || strings.HasPrefix(rel, h)
		}
		if !ok {
			add("no fake or sdk/testkit package in a non-test file outside sdk/def, sdk/testkit, test/ and cmd/aiopsctl", docFake)
		}
	}
	return out
}

// pluginLayout reports a non-test Go file of a plugin that is neither plugin.go
// nor under internal/ (07-c4-plugin-sdk.md:46).
func pluginLayout(rel string) string {
	seg := strings.Split(rel, "/")
	if len(seg) < 3 || seg[0] != "plugins" || seg[2] == "internal" || (len(seg) == 3 && seg[2] == "plugin.go") {
		return ""
	}
	return "everything of a plugin but plugin.go is under internal/ (" + docPlugin + ")"
}

func walkGo(root string, fn func(p, rel string, test bool) error) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		return fn(p, filepath.ToSlash(rel), strings.HasSuffix(p, "_test.go"))
	})
}

// ListDeps returns the import paths in the dependency closure of pkg.
type ListDeps func(root, pkg string) ([]string, error)

func GoListDeps(root, pkg string) ([]string, error) {
	cmd := exec.Command("go", "list", "-deps", pkg)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list -deps %s: %w", pkg, err)
	}
	return strings.Fields(string(out)), nil
}

// Imports applies the import boundary table and U2, with the entries reg of the
// upstream register, to every Go file under root; the fake rule, the toolbox rule and
// the rule of the module zip to the dependency closure of the server binary cmd/aiops;
// and the link guard of ADR-060 decision 28,
// no package under test/ or examples/, to the closures of cmd/aiops and cmd/aiopsctl,
// each where the command exists.
func Imports(root string, reg []Upstream, list ListDeps) error {
	var bad []string
	err := walkGo(root, func(p, rel string, test bool) error {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, parser.ImportsOnly|parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		if m := pluginLayout(rel); m != "" && !test {
			bad = append(bad, fmt.Sprintf("%s:1: %s", rel, m))
		}
		for _, im := range f.Imports {
			path, _ := strconv.Unquote(im.Path.Value)
			for _, r := range importReasons(rel, test, path, reg) {
				bad = append(bad, fmt.Sprintf("%s:%d: imports %s: %s", rel, fset.Position(im.Pos()).Line, path, r))
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(bad)
	for _, bin := range []string{"aiops", "aiopsctl"} {
		if st, e := os.Stat(filepath.Join(root, "cmd", bin)); e != nil || !st.IsDir() {
			fmt.Printf("imports: cmd/%s does not exist; its closure check skipped\n", bin)
			continue
		}
		deps, err := list(root, "./cmd/"+bin)
		if err != nil {
			return err
		}
		sort.Strings(deps)
		for _, d := range deps {
			if bin == "aiops" && isFakePath(d) {
				bad = append(bad, fmt.Sprintf("cmd/aiops: dependency closure contains %s: no fake or sdk/testkit package in a server binary (%s)", d, docFake))
			}
			if bin == "aiops" && isToolbox(d) {
				bad = append(bad, fmt.Sprintf("cmd/aiops: dependency closure contains %s: no part of the toolbox in a server binary (%s)", d, docTools))
			}
			if bin == "aiops" && isModuleZip(d) {
				bad = append(bad, fmt.Sprintf("cmd/aiops: dependency closure contains %s: no package of golang.org/x/mod in a server binary but semver and sumdb/note, which sigstore-go links; the module zip is the toolbox's (%s)", d, docModZip))
			}
			if hasPath(d, modPath+"/test") || hasPath(d, modPath+"/examples") {
				bad = append(bad, fmt.Sprintf("cmd/%s: dependency closure contains %s: no package under test/ or examples/ in cmd/aiops or cmd/aiopsctl (%s)", bin, d, docLink))
			}
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("import boundary violations:\n%s", strings.Join(bad, "\n"))
	}
	return nil
}
