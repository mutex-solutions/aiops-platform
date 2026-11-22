package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const modPath = "github.com/mutex-solutions/aiops-platform"

// Document references cited in every report line (file:line of docs/architecture
// and docs/adr at the time of writing; the text of the rule is what counts).
const (
	docKernel = "07-c4-plugin-sdk.md:27"
	docDef    = "07-c4-plugin-sdk.md:28"
	docPlugin = "07-c4-plugin-sdk.md:34,46; 08-profiles-roadmap-adrs.md:1255"
	docBoot   = "07-c4-plugin-sdk.md:37-38; 08-profiles-roadmap-adrs.md:1254"
	docYAML   = "ADR-059-s1-scope.md:43"
	docFake   = "07-c4-plugin-sdk.md:33,47; 08-profiles-roadmap-adrs.md:1258"
)

// bootDefs are the only definitions boot/ and loader/ may import.
var bootDefs = []string{"launch", "loader", "composition"}

// pluginThirdParty grandfathers third-party imports per plugin. A new plugin
// gets no third-party import without an entry here (07-c4-plugin-sdk.md:34).
var pluginThirdParty = map[string][]string{
	// The S0 spike probes under plugins/jobs-river/internal use pgx and River;
	// "River only under plugins/jobs-river/" is the documented exception
	// (07-c4-plugin-sdk.md:70).
	"jobs-river": {"github.com/jackc/pgx/v5", "github.com/riverqueue/river"},
}

var yamlPrefixes = []string{"go.yaml.in/yaml", "gopkg.in/yaml", "sigs.k8s.io/yaml", "github.com/goccy/go-yaml"}

// fakeHomes may import a fake or testkit package from non-test files: the fakes
// and suites themselves, the testkit, test trees, and the aiopsctl check runner,
// which carries the fakes by design (07-c4-plugin-sdk.md:33,
// 01-c1-system-context.md:279).
var fakeHomes = []string{"sdk/def/", "sdk/testkit/", "test/", "cmd/aiopsctl/"}

func isStd(p string) bool { return !strings.Contains(strings.SplitN(p, "/", 2)[0], ".") }

func hasPath(p, prefix string) bool { return p == prefix || strings.HasPrefix(p, prefix+"/") }

func isKernel(p string) bool  { return hasPath(p, modPath+"/sdk/kernel") }
func isTestkit(p string) bool { return hasPath(p, modPath+"/sdk/testkit") }

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
// slash path of the file below the repository root.
func importReasons(rel string, test bool, imp string) []string {
	var out []string
	std := isStd(imp)
	add := func(rule, doc string) { out = append(out, rule+" ("+doc+")") }
	seg := strings.Split(rel, "/")
	tk := test && isTestkit(imp)
	switch {
	case hasPath(rel, "sdk/kernel"):
		if !std {
			add("sdk/kernel imports the standard library only, tests included", docKernel)
		}
	case hasPath(rel, "sdk/def"):
		if !(std || isKernel(imp) || isDef(imp) || tk) {
			add("sdk/def imports the standard library and sdk/kernel only (sdk/testkit too in tests)", docDef)
		}
	case hasPath(rel, "boot"):
		if !(std || isKernel(imp) || isDef(imp, bootDefs...) || hasPath(imp, "go.yaml.in/yaml/v3") || tk) {
			add("boot imports the standard library, go.yaml.in/yaml/v3, sdk/kernel and sdk/def/{launch,loader,composition} only; nothing from plugins, loader, cmd, test, dist", docBoot)
		}
	case hasPath(rel, "loader"):
		if !(std || isKernel(imp) || isDef(imp, bootDefs...) || tk) {
			add("loader imports the standard library, sdk/kernel and sdk/def/{launch,loader,composition} only; nothing from plugins, boot, cmd, test, dist", docBoot)
		}
	case hasPath(rel, "plugins") && len(seg) > 2:
		third := false
		for _, p := range pluginThirdParty[seg[1]] {
			third = third || hasPath(imp, p)
		}
		if !(std || isKernel(imp) || isDef(imp) || hasPath(imp, modPath+"/plugins/"+seg[1]) || third || tk) {
			add("a plugin imports the SDK, its own packages and allow-listed modules only; never another plugin, boot, loader, cmd, test, dist", docPlugin)
		}
	}
	if !hasPath(rel, "boot") && !hasPath(rel, "tools") {
		for _, y := range yamlPrefixes {
			if strings.HasPrefix(imp, y) {
				add("the YAML package is imported in boot/ and tools/ only", docYAML)
				break
			}
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

// listDeps returns the import paths in the dependency closure of pkg.
type listDeps func(root, pkg string) ([]string, error)

func goListDeps(root, pkg string) ([]string, error) {
	cmd := exec.Command("go", "list", "-deps", pkg)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list -deps %s: %w", pkg, err)
	}
	return strings.Fields(string(out)), nil
}

// checkImports applies the import boundary table to every Go file under root
// and, when cmd/aiops exists, to the dependency closure of the server binary.
func checkImports(root string, list listDeps) error {
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
			for _, r := range importReasons(rel, test, path) {
				bad = append(bad, fmt.Sprintf("%s:%d: imports %s: %s", rel, fset.Position(im.Pos()).Line, path, r))
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(bad)
	if st, e := os.Stat(filepath.Join(root, "cmd", "aiops")); e != nil || !st.IsDir() {
		fmt.Println("imports: cmd/aiops does not exist; server closure check skipped")
	} else {
		deps, err := list(root, "./cmd/aiops")
		if err != nil {
			return err
		}
		sort.Strings(deps)
		for _, d := range deps {
			if isFakePath(d) {
				bad = append(bad, fmt.Sprintf("cmd/aiops: dependency closure contains %s: no fake or sdk/testkit package in a server binary (%s)", d, docFake))
			}
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("import boundary violations:\n%s", strings.Join(bad, "\n"))
	}
	return nil
}
