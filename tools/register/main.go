// Command register runs the checks of ADR-054 that read the names of the upstream
// register: the name lint U1 (decisions 14 and 22) and suite neutrality U3 (decision
// 15), in names.go, and the register check U7 (decision 12; ADR-060 decisions 36 and
// 51). The import boundary U2 is tools/kernelcheck imports.
//
// U7 reads tools/upstream-components.yaml and fails when an
// entry breaks the schema of ADR-054 decisions 3 and 4, when a module in the package
// graph of a module of the repository, tests included, has no entry, when a build
// of a module selects a version other than an entry's pin, and on the clauses of
// decision 12 that have a case at S2a. The build of a module is read as the package
// graph of its packages and their tests (go list -deps -test), which needs no go.mod
// of a module outside that graph. It reads with -mod=readonly from the module cache
// and looks up nothing on the network.
//
// Two readings of decision 3: an entry whose modes are only tool or none fills no
// container role, since a program CI runs and an item refused by name have none; and
// a condition names the L or T row that enforces it in its row key.
//
// The clauses on images, baked-in tools and embedded data sets have no case until
// the builder image, the server image and the SPDX list data merge, and the clause
// on the base provider allow-list none before the first row of aiops-base (S3); each
// lands with its first case (08-profiles-roadmap-adrs.md:678).
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	registerFile = "tools/upstream-components.yaml"
	modPath      = "github.com/mutex-solutions/aiops-platform"
)

// modules are the directories of the repository's Go modules.
var modules = []string{".", "sdk", "sdk/kernel", "sdk/testkit"}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: register <repository root>")
		os.Exit(2)
	}
	if err := runCheck(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "register:", err)
		os.Exit(1)
	}
}

func runCheck(root string) error {
	data, err := os.ReadFile(filepath.Join(root, registerFile))
	if err != nil {
		return err
	}
	r, err := parse(data)
	if err != nil {
		return fmt.Errorf("%s: %w", registerFile, err)
	}
	in, err := gather(root)
	if err != nil {
		return err
	}
	ps, err := places(root)
	if err != nil {
		return err
	}
	var fails []string
	if bad := check(r, in); len(bad) > 0 {
		fails = append(fails, "U7 fails (ADR-054 decision 12):\n"+strings.Join(bad, "\n"))
	}
	if bad := lint(r, ps); len(bad) > 0 {
		fails = append(fails, "U1 or U3 fails (ADR-054 decisions 14, 15 and 22):\n"+strings.Join(bad, "\n"))
	}
	if len(fails) > 0 {
		return errors.New(strings.Join(fails, "\n"))
	}
	fmt.Printf("register: %d entries; every module of the package graphs has an entry at its pin; %d places hold no name outside its places\n", len(r.Entries), len(ps))
	return nil
}

type register struct {
	Entries       []entry     `yaml:"entries"`
	Exceptions    []exception `yaml:"exceptions"`
	BaseProviders []string    `yaml:"baseProviders"`
}

// entry holds the keys of ADR-054 decision 3.
type entry struct {
	ID         string         `yaml:"id"`
	Role       string         `yaml:"role"`
	Names      []string       `yaml:"names"`
	Form       string         `yaml:"form"`
	Behind     string         `yaml:"behind"`
	Plugins    []string       `yaml:"plugins"`
	Bundle     string         `yaml:"bundle"`
	Modules    []string       `yaml:"modules"`
	Images     []image        `yaml:"images"`
	Pin        string         `yaml:"pin"`
	Licence    string         `yaml:"licence"`
	Elected    string         `yaml:"elected"`
	Verdict    string         `yaml:"verdict"`
	Modes      []string       `yaml:"modes"`
	Blockers   map[int]string `yaml:"blockers"`
	Conditions []condition    `yaml:"conditions"`
	Evidence   []evidence     `yaml:"evidence"`
	Mark       string         `yaml:"mark"`
	Contains   []contained    `yaml:"contains"`
	Default    *bool          `yaml:"default"`
	SBOM       string         `yaml:"sbom"`
	Source     string         `yaml:"source"`
	Importers  []importer     `yaml:"importers"`
}

type image struct{ Ref, Key string }
type condition struct{ Condition, Row string }
type evidence struct{ Source, Read, Version string }
type importer struct{ Dir, Decision string }

// contained is a module, package, nested chart or image layer an upstream brings in.
type contained struct{ Module, Package, Chart, Layer, Version, Licence string }

type exception struct {
	Item   string   `yaml:"item"`
	Tokens []string `yaml:"tokens"`
	Places []string `yaml:"places"`
	Source string   `yaml:"source"`
}

// parse reads the register; an unknown key fails (ADR-054 decision 3).
func parse(data []byte) (register, error) {
	var r register
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	err := dec.Decode(&r)
	return r, err
}

// The enumerations of ADR-054 decision 4 and the licence lists of L7
// (08-profiles-roadmap-adrs.md §5.1).
var (
	forms    = []string{"process", "library", "service", "content", "contract", "port"}
	verdicts = []string{"ok", "ok-with-conditions", "optional-plugin-only", "replace-or-rebuild", "unverified"}
	marks    = []string{"suffix-ok", "needs-approval", "never"}
	modes    = []string{"network", "pull", "ship", "link", "copy", "tool", "none", "ideas", "user-installs", "implement"}
	allowed  = []string{"Apache-2.0", "MIT", "BSD-2-Clause", "BSD-3-Clause", "ISC", "0BSD", "Zlib", "PostgreSQL",
		"Unicode-DFS-2016", "Unicode-3.0", "BlueOak-1.0.0"}
	conditional = []string{"MPL-2.0", "CC0-1.0", "LicenseRef-MCP-composite"}
	idPattern   = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	rowPattern  = regexp.MustCompile(`^[LT][0-9]+$`)
	licenceID   = regexp.MustCompile(`[A-Za-z0-9.+-]+`)
)

// inputs are what U7 reads besides the register.
type inputs struct {
	graph        map[string]string   // module path to version, over the package graphs of every module, tests included
	builds       map[string][]string // module path to every version a module's build selects for it
	dist         map[string]string   // each dist/*.yaml, by path, to its text
	sec32        []string            // the ids of the table of 00 §3.2
	agplBoundary []string            // the AGPL boundary list of 00:167
	record       map[string]string   // a component id of the licence record to its "Licence as verified"
}

// check returns one line per finding.
func check(r register, in inputs) []string {
	var bad []string
	fail := func(id, format string, a ...any) { bad = append(bad, id+": "+fmt.Sprintf(format, a...)) }
	seen := map[string]bool{}
	pins := map[string]string{} // module path to the version an entry records
	for _, e := range r.Entries {
		id := e.ID
		if !idPattern.MatchString(id) {
			fail(id, "id is not lowercase kebab-case")
		}
		if seen[id] {
			fail(id, "id is not unique")
		}
		seen[id] = true
		required := map[string]bool{"names": e.Names != nil, "form": e.Form != "", "behind": e.Behind != "",
			"licence": e.Licence != "", "verdict": e.Verdict != "", "modes": len(e.Modes) > 0, "mark": e.Mark != "",
			"default": e.Default != nil}
		for _, k := range []string{"names", "form", "behind", "licence", "verdict", "modes", "mark", "default"} {
			if !required[k] {
				fail(id, "%s is required", k)
			}
		}
		deployed := slices.ContainsFunc(e.Modes, func(m string) bool { return m != "tool" && m != "none" })
		switch {
		case (e.Form == "process" || e.Form == "service") && deployed && e.Role == "":
			fail(id, "role is required for a process or a service")
		case len(e.Plugins) > 0 && e.Bundle == "":
			fail(id, "bundle is required with plugins")
		case len(e.Modules) > 0 && e.Pin == "":
			fail(id, "pin is required with modules")
		}
		for _, v := range []struct {
			key, value string
			set        []string
		}{{"form", e.Form, forms}, {"verdict", e.Verdict, verdicts}, {"mark", e.Mark, marks}} {
			if v.value != "" && !slices.Contains(v.set, v.value) {
				fail(id, "%s %q is not one of %v", v.key, v.value, v.set)
			}
		}
		for _, m := range e.Modes {
			if !slices.Contains(modes, m) {
				fail(id, "mode %q is not one of %v", m, modes)
			}
		}
		switch {
		case e.Verdict == "unverified":
			fail(id, "unverified entry (L3, L13)")
		case len(e.Evidence) == 0:
			fail(id, "evidence is required unless unverified")
		}
		for _, ev := range e.Evidence {
			if ev.Source == "" || ev.Read == "" || ev.Version == "" {
				fail(id, "evidence needs source, read and version")
			}
		}
		if e.Verdict == "ok-with-conditions" && len(e.Conditions) == 0 {
			fail(id, "verdict ok-with-conditions needs conditions")
		}
		for _, c := range e.Conditions {
			if c.Condition == "" || !rowPattern.MatchString(c.Row) {
				fail(id, "a condition needs its text and the L or T row that enforces it")
			}
		}
		if e.Default != nil && *e.Default && e.Verdict != "ok" && e.Verdict != "ok-with-conditions" {
			fail(id, "default is true and the verdict is %s", e.Verdict)
		}
		ids := licenceIDs(e.Licence)
		hasOR := slices.Contains(ids, "OR")
		if hasOR && (e.Elected == "" || !slices.Contains(ids, e.Elected)) {
			fail(id, "licence holds OR and elected names none of its sides")
		}
		ids = slices.DeleteFunc(ids, func(s string) bool { return s == "AND" || s == "OR" || s == "WITH" })
		if hasOR {
			ids = []string{e.Elected} // ponytail: an OR mixed with AND reads the elected side alone
		}
		agpl := slices.ContainsFunc(ids, func(s string) bool { return strings.HasPrefix(s, "AGPL") })
		if agpl && len(e.Modules) > 0 {
			fail(id, "an AGPL entry has modules; an Apache-2.0 sub-tree is an entry of its own (L2)")
		}
		if slices.Contains(e.Modes, "ship") && (e.SBOM == "" || e.Source == "") {
			fail(id, "a ship entry needs sbom and source (L5)")
		}
		if slices.Contains(e.Modes, "link") {
			for _, l := range ids {
				switch {
				case slices.Contains(allowed, l):
				case slices.Contains(conditional, l):
					if !slices.ContainsFunc(e.Conditions, func(c condition) bool { return c.Row == "L7" || c.Row == "L8" }) {
						fail(id, "%s needs a condition that cites L7 or L8", l)
					}
				default:
					fail(id, "linked licence %s is outside L7", l)
				}
			}
		}
		if e.Verdict == "replace-or-rebuild" {
			for path, text := range in.dist {
				for _, s := range append(append([]string{id}, e.Modules...), imageRefs(e.Images)...) {
					if strings.Contains(text, s) {
						fail(id, "a replace-or-rebuild entry appears in %s", path)
					}
				}
			}
		}
		for _, m := range e.Modules {
			pins[m] = e.Pin
		}
		for _, c := range e.Contains {
			if c.Module != "" {
				pins[c.Module] = c.Version
			}
		}
	}
	for _, m := range sortedKeys(in.graph) {
		if _, ok := pins[m]; !ok {
			bad = append(bad, fmt.Sprintf("%s %s: no entry has it in modules or contains (ADR-054 decisions 5 and 18)", m, in.graph[m]))
		}
	}
	for _, m := range sortedKeys(pins) {
		vs := in.builds[m]
		if v, ok := in.graph[m]; ok {
			vs = append(slices.Clone(vs), v)
		}
		for _, v := range vs {
			if v != pins[m] {
				bad = append(bad, fmt.Sprintf("%s: the build selects %s, the pin is %s; read the licence again at the new version (ADR-054 decision 6)", m, v, pins[m]))
			}
		}
	}
	for _, id := range in.sec32 {
		lic, ok := in.record[id]
		for _, e := range r.Entries {
			if e.Role == id {
				lic, ok = e.Licence, true
			}
		}
		if ok && strings.HasPrefix(strings.TrimSpace(lic), "AGPL") && !slices.Contains(in.agplBoundary, id) {
			fail(id, "AGPL component of 00 §3.2 missing from the AGPL boundary list of 00:167")
		}
	}
	slices.Sort(bad)
	return slices.Compact(bad)
}

// licenceIDs splits an SPDX expression into its ids and operators.
func licenceIDs(expr string) []string { return licenceID.FindAllString(expr, -1) }

func imageRefs(images []image) []string {
	var out []string
	for _, i := range images {
		out = append(out, i.Ref)
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// gather reads the module graphs and the documents U7 reads.
func gather(root string) (inputs, error) {
	in := inputs{graph: map[string]string{}, builds: map[string][]string{}, dist: map[string]string{}}
	for _, m := range modules {
		dir := filepath.Join(root, m)
		out, err := goCmd(dir, "list", "-deps", "-test", "-f", "{{with .Module}}{{.Path}} {{.Version}}{{end}}", "./...")
		if err != nil {
			return in, err
		}
		for _, l := range strings.Split(out, "\n") {
			if p, v, ok := strings.Cut(l, " "); ok && !strings.HasPrefix(p, modPath) {
				in.graph[p] = v
				if !slices.Contains(in.builds[p], v) {
					in.builds[p] = append(in.builds[p], v)
				}
			}
		}
	}
	files, _ := filepath.Glob(filepath.Join(root, "dist", "*.yaml"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return in, err
		}
		in.dist[filepath.ToSlash(strings.TrimPrefix(f, filepath.Clean(root)+string(filepath.Separator)))] = string(b)
	}
	dec, err := os.ReadFile(filepath.Join(root, decisionsFile))
	if err != nil {
		return in, err
	}
	rec, err := os.ReadFile(filepath.Join(root, "docs/design/licence-audit-2026-11.md"))
	if err != nil {
		return in, err
	}
	in.sec32, in.agplBoundary, in.record = sec32(string(dec)), boundaryList(string(dec)), recordLicences(string(rec))
	if len(in.sec32) == 0 || len(in.agplBoundary) == 0 {
		return in, errors.New("00-decisions.md: no §3.2 table or no AGPL boundary list found")
	}
	return in, nil
}

func goCmd(dir string, args ...string) (string, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly", "GOPROXY=off")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go %s in %s: %v: %s", strings.Join(args, " "), dir, err, stderr.String())
	}
	return string(out), nil
}

var backticked = regexp.MustCompile("`([a-z0-9-]+)`")

// sec32 returns the ids of the table of 00 §3.2.
func sec32(doc string) []string {
	_, rest, _ := strings.Cut(doc, "\n### 3.2 ")
	section, _, _ := strings.Cut(rest, "\n### ")
	var ids []string
	for _, l := range strings.Split(section, "\n") {
		cells := strings.Split(l, "|")
		if len(cells) > 2 {
			if m := backticked.FindStringSubmatch(cells[1]); m != nil {
				ids = append(ids, m[1])
			}
		}
	}
	return ids
}

// boundaryList returns the AGPL boundary list of 00:167: the ids named between "The
// AGPL components we deploy are" and "and the check fails".
func boundaryList(doc string) []string {
	_, rest, _ := strings.Cut(doc, "The AGPL components we deploy are")
	list, _, _ := strings.Cut(rest, "and the check fails")
	var ids []string
	for _, m := range backticked.FindAllStringSubmatch(list, -1) {
		ids = append(ids, m[1])
	}
	return ids
}

// recordLicences maps each component id that a row of the licence record names in
// its first cell to that row's "Licence as verified" cell.
func recordLicences(doc string) map[string]string {
	out := map[string]string{}
	col := -1
	for _, l := range strings.Split(doc, "\n") {
		cells := strings.Split(l, "|")
		switch {
		case !strings.HasPrefix(l, "|"):
			col = -1
		case col < 0:
			col = slices.IndexFunc(cells, func(c string) bool { return strings.TrimSpace(c) == "Licence as verified" })
		case col < len(cells):
			for _, m := range backticked.FindAllStringSubmatch(cells[1], -1) {
				out[m[1]] = strings.TrimSpace(cells[col])
			}
		}
	}
	return out
}
