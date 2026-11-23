// Command deflint is the definition lint of the standing rule 1 of S2a (ADR-060
// decision 32; 08-profiles-roadmap-adrs.md:676): no definition merges without its
// fake, its suite, its definition.yaml, a provider and a consumer. For every
// definition sdk/def/<name>/v<N> it checks:
//
//   - the files: definition.yaml, the definition package, fake/, conformance/ with Run,
//     and a test of the package that calls deftest.Agree (ADR-060 decision 7);
//   - definition.yaml in its fixed layout, read with the YAML library, with a suite
//     revision of 1 or more (decisions 7, 10 and 55);
//   - at least one provider and one consumer among the plugins of the repository's
//     distributions, the test distributions included, counting the launcher, which
//     provides the launch keys and consumes configRefsDryRun (00-decisions.md:212;
//     ADR-059 decision 16);
//   - the rules of 07-c4-plugin-sdk.md:514-520 that a static check decides: the method
//     set (no side door), the caller's context first, Inventory() on a registry, and
//     no upstream import.
//
// A definition whose consumer lands later is no error as long as it has one consumer:
// 08:676 asks a provider and a consumer per definition, not per key.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const modPath = "github.com/mutex-solutions/aiops-platform"

// Document references cited in the problems.
const (
	docRule   = "08-profiles-roadmap-adrs.md:676; ADR-060 decision 32"
	docLayout = "ADR-060 decisions 7, 10 and 55"
	docRules  = "07-c4-plugin-sdk.md:514-520"
)

// pendingSuites are the S1 definitions whose suites land with ADR-060 decision 44. The
// lint fails when one of them has its suite, so the entry leaves with the change that
// adds the suite and no definition keeps an exemption it no longer needs.
var pendingSuites = []string{"clock", "composition", "configrefs", "launch", "reload"}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: deflint <repo-root>")
		os.Exit(2)
	}
	problems, n, err := lint(os.Args[1], distributions())
	if err != nil {
		fmt.Fprintln(os.Stderr, "deflint:", err)
		os.Exit(1)
	}
	for _, p := range problems {
		fmt.Println(p)
	}
	if len(problems) > 0 {
		fmt.Fprintf(os.Stderr, "deflint: %d problems in %d definitions\n", len(problems), n)
		os.Exit(1)
	}
	fmt.Printf("deflint: %d definitions checked\n", n)
}

// definition is one sdk/def/<name>/v<N> directory.
type definition struct {
	name  string // <name>
	major int    // N
	dir   string // the directory
	rel   string // sdk/def/<name>/v<N>
	keys  []string
}

func (d *definition) path() string { return modPath + "/" + d.rel }

// lint runs every check on the definitions under root and returns the problems,
// sorted, and the number of definitions.
func lint(root string, plugins []plugin) ([]string, int, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, 0, err
	}
	defs, err := find(root)
	if err != nil {
		return nil, 0, err
	}
	var out []string
	var typed []*definition // the definitions with a Go package to type-check
	paths := []string{"./dist", "./test/dist", "./examples"}
	for _, d := range defs {
		out = append(out, prefix(d, files(d))...)
		out = append(out, prefix(d, layout(d))...)
		out = append(out, prefix(d, imports(d))...)
		if len(goFiles(d.dir, false)) > 0 {
			typed, paths = append(typed, d), append(paths, d.path())
		}
	}
	pkgs, err := list(root, paths...)
	if err != nil {
		return nil, 0, err
	}
	c := newChecker(pkgs)
	for _, d := range typed {
		files, err := c.parseDir(d.path())
		if err != nil {
			return nil, 0, err
		}
		pkg, _, err := c.check(d.path(), files)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, prefix(d, shapes(pkg))...)
	}
	for _, path := range sortedKeys(pkgs) {
		if p := pkgs[path]; p.Module == nil || p.Module.Path != modPath || hasPath(path, modPath+"/tools") {
			continue
		}
		files, err := c.parseDir(path)
		if err != nil {
			return nil, 0, err
		}
		_, info, err := c.check(path, files)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, sideDoors(c.fset, root, info, files)...)
	}
	out = append(out, roles(defs, plugins)...)
	sort.Strings(out)
	return out, len(defs), nil
}

func prefix(d *definition, problems []string) []string {
	for i, p := range problems {
		problems[i] = d.rel + ": " + p
	}
	return problems
}

func hasPath(p, prefix string) bool { return p == prefix || strings.HasPrefix(p, prefix+"/") }

var majorDir = regexp.MustCompile(`^v(0|[1-9][0-9]*)$`)

// find lists the definitions sdk/def/<name>/v<N> under root.
func find(root string) ([]*definition, error) {
	names, err := os.ReadDir(filepath.Join(root, "sdk", "def"))
	if err != nil {
		return nil, err
	}
	var out []*definition
	for _, n := range names {
		if !n.IsDir() || n.Name() == "internal" {
			continue
		}
		versions, err := os.ReadDir(filepath.Join(root, "sdk", "def", n.Name()))
		if err != nil {
			return nil, err
		}
		for _, v := range versions {
			m := majorDir.FindStringSubmatch(v.Name())
			if !v.IsDir() || m == nil {
				continue
			}
			major, _ := strconv.Atoi(m[1])
			rel := "sdk/def/" + n.Name() + "/" + v.Name()
			out = append(out, &definition{name: n.Name(), major: major, dir: filepath.Join(root, filepath.FromSlash(rel)), rel: rel})
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no definition under sdk/def")
	}
	return out, nil
}

// goFiles returns the names of the .go files directly in dir, tests or not.
func goFiles(dir string, tests bool) []string {
	all, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	var out []string
	for _, p := range all {
		if strings.HasSuffix(p, "_test.go") == tests {
			out = append(out, p)
		}
	}
	return out
}

func anyContains(paths []string, s string) bool {
	return slices.ContainsFunc(paths, func(p string) bool {
		b, err := os.ReadFile(p)
		return err == nil && bytes.Contains(b, []byte(s))
	})
}

// files checks that the files of the definition are present.
func files(d *definition) []string {
	var out []string
	if _, err := os.Stat(filepath.Join(d.dir, "definition.yaml")); err != nil {
		out = append(out, "no definition.yaml ("+docRule+")")
	}
	if len(goFiles(d.dir, false)) == 0 {
		out = append(out, "no Go file of the definition package ("+docRule+")")
	}
	if !anyContains(goFiles(d.dir, true), "deftest.Agree(") {
		out = append(out, "no test calls deftest.Agree, so nothing holds definition.yaml to the Go declarations (ADR-060 decision 7)")
	}
	if len(goFiles(filepath.Join(d.dir, "fake"), false)) == 0 {
		out = append(out, "no fake: fake/ holds no Go file ("+docRule+")")
	}
	suite := goFiles(filepath.Join(d.dir, "conformance"), false)
	pending := slices.Contains(pendingSuites, d.name)
	switch {
	case len(suite) > 0 && pending:
		out = append(out, "its suite exists: remove it from pendingSuites in tools/deflint")
	case len(suite) == 0 && !pending:
		out = append(out, "no suite: conformance/ holds no Go file ("+docRule+")")
	case !pending && !anyContains(suite, "func Run("):
		out = append(out, "the suite has no Run (ADR-059 decision 32)")
	}
	return out
}

// file is definition.yaml as the YAML library reads it. Class is a mapping, either
// {provider, consumer} or one such mapping per key.
type file struct {
	Key            string                          `yaml:"key"`
	Version        string                          `yaml:"version"`
	Stability      string                          `yaml:"stability"`
	Class          map[string]yaml.Node            `yaml:"class"`
	Remote         string                          `yaml:"remote"`
	CompatibleFrom string                          `yaml:"compatibleFrom"`
	Deprecated     *map[string]string              `yaml:"deprecated"`
	Suite          *int                            `yaml:"suite"`
	Hooks          map[string]map[string]yaml.Node `yaml:"hooks"`
}

var (
	order      = []string{"key", "version", "stability", "class", "remote", "compatibleFrom", "deprecated", "suite", "hooks"}
	majorMinor = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
)

func oneOf(v string, allowed ...string) bool { return slices.Contains(allowed, v) }

// layout reads definition.yaml strictly, checks its fixed layout and sets d.keys.
func layout(d *definition) []string {
	src, err := os.ReadFile(filepath.Join(d.dir, "definition.yaml"))
	if err != nil {
		return nil // files reports it
	}
	var out []string
	bad := func(format string, a ...any) { out = append(out, fmt.Sprintf(format, a...)+" ("+docLayout+")") }
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return []string{fmt.Sprintf("definition.yaml is not one YAML mapping: %v (%s)", err, docLayout)}
	}
	next := 0
	for i := 0; i < len(doc.Content[0].Content); i += 2 {
		at := slices.Index(order, doc.Content[0].Content[i].Value)
		if at < next {
			bad("definition.yaml: field %q is unknown, repeated or out of order", doc.Content[0].Content[i].Value)
		}
		next = max(next, at+1)
	}
	var f file
	dec := yaml.NewDecoder(bytes.NewReader(src))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return append(out, fmt.Sprintf("definition.yaml: %v (%s)", err, docLayout))
	}
	m := majorMinor.FindStringSubmatch(f.Version)
	if m == nil || m[1] != strconv.Itoa(d.major) {
		bad("version %q is not \"%d.<minor>\"", f.Version, d.major)
	}
	if !oneOf(f.Stability, "experimental", "preview", "ga") {
		bad("stability %q is not experimental, preview or ga", f.Stability)
	}
	if !oneOf(f.Remote, "contrib", "callback", "no") {
		bad("remote %q is not contrib, callback or no", f.Remote)
	}
	if f.Key == "" {
		bad("no key")
	}
	classes := map[string]map[string]string{}
	if _, single := f.Class["provider"]; single {
		classes[f.Key] = map[string]string{}
		for side, n := range f.Class {
			classes[f.Key][side] = n.Value
		}
	} else {
		if f.Key != d.name {
			bad("key %q: with one class per key it names the definition, %q", f.Key, d.name)
		}
		for key, n := range f.Class {
			var c map[string]string
			if err := n.Decode(&c); err != nil {
				bad("class of %s: %v", key, err)
			}
			classes[key] = c
		}
	}
	if len(classes) == 0 {
		bad("no class")
	}
	for _, key := range sortedKeys(classes) {
		c := classes[key]
		if len(c) != 2 || !oneOf(c["provider"], "open", "bound") || !oneOf(c["consumer"], "open", "bound", "signed") {
			bad("class of %s is not {provider: open|bound, consumer: open|bound|signed}: %v", key, c)
		}
		d.keys = append(d.keys, key)
	}
	if f.CompatibleFrom != "" {
		cf := majorMinor.FindStringSubmatch(f.CompatibleFrom)
		if d.major != 0 || cf == nil || m == nil || cf[1] != m[1] || atoi(cf[2]) > atoi(m[2]) {
			bad("compatibleFrom %q: only at major 0, a minor of the same major up to the version's", f.CompatibleFrom)
		}
	}
	if f.Deprecated != nil {
		dep := *f.Deprecated
		_, dateErr := time.Parse("2006-01-02", dep["removeNotBefore"])
		s := majorDir.FindStringSubmatch(dep["successor"])
		if len(dep) != 3 || dep["since"] == "" || dateErr != nil || s == nil || atoi(s[1]) <= d.major {
			bad("deprecated is not {since: <release>, removeNotBefore: <date>, successor: v<a later major>}: %v", dep)
		}
	}
	if f.Suite == nil || *f.Suite < 1 {
		bad("the suite revision of a definition under sdk/def is 1 or more")
	}
	for _, name := range sortedKeys(f.Hooks) {
		h := f.Hooks[name]
		var stages []string
		if n, ok := h["stages"]; ok {
			_ = n.Decode(&stages)
		}
		sorted := slices.Clone(stages)
		slices.Sort(sorted)
		if len(h) != 3 || len(stages) == 0 || slices.Contains(stages, "") || len(slices.Compact(sorted)) != len(stages) ||
			!oneOf(h["access"].Value, "open", "signed", "sealed") || !oneOf(h["remote"].Value, "yes", "no") {
			bad("hook %s is not {stages: [<stage>, ...], access: open|signed|sealed, remote: yes|no}", name)
		}
	}
	return out
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// imports checks that no file of the definition, its fake or its suite imports an
// upstream module: only the standard library, sdk/kernel, sdk/def and, in tests,
// sdk/testkit (07-c4-plugin-sdk.md:520, U3).
func imports(d *definition) []string {
	var out []string
	_ = filepath.WalkDir(d.dir, func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() || !strings.HasSuffix(p, ".go") {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if err != nil {
			out = append(out, err.Error())
			return nil
		}
		for _, im := range f.Imports {
			path, _ := strconv.Unquote(im.Path.Value)
			std := !strings.Contains(strings.SplitN(path, "/", 2)[0], ".")
			test := strings.HasSuffix(p, "_test.go") && hasPath(path, modPath+"/sdk/testkit")
			if !std && !test && !hasPath(path, modPath+"/sdk/kernel") && !hasPath(path, modPath+"/sdk/def") {
				rel, _ := filepath.Rel(d.dir, p)
				out = append(out, fmt.Sprintf("%s imports %s: a definition, its fake and its suite import no upstream module (%s)", filepath.ToSlash(rel), path, docRules))
			}
		}
		return nil
	})
	return out
}

// plugin is what the provider and consumer check reads of one plugin.
type plugin struct {
	id                 string
	provides, consumes []string
}

// roles checks that each definition has a provider and a consumer among plugins.
func roles(defs []*definition, plugins []plugin) []string {
	var out []string
	owner := map[string]*definition{}
	for _, d := range defs {
		for _, k := range d.keys {
			if o := owner[k]; o != nil {
				out = append(out, fmt.Sprintf("%s: key %s is declared by %s too", d.rel, k, o.rel))
			}
			owner[k] = d
		}
	}
	provided, consumed := map[*definition]bool{}, map[*definition]bool{}
	for _, p := range plugins {
		for _, k := range p.provides {
			provided[owner[k]] = true
		}
		for _, k := range p.consumes {
			consumed[owner[k]] = true
		}
	}
	for _, d := range defs {
		if !provided[d] {
			out = append(out, d.rel+": no plugin of a distribution, test distributions and the launcher included, provides a key of it ("+docRule+")")
		}
		if !consumed[d] {
			out = append(out, d.rel+": no plugin of a distribution, test distributions and the launcher included, consumes a key of it ("+docRule+")")
		}
	}
	return out
}
