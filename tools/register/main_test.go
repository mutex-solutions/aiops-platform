package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// base is a register that passes U7 against the inputs of baseInputs.
const base = `
entries:
  - id: lib
    names: [lib]
    form: library
    behind: none
    modules: [example.org/lib]
    pin: v1.0.0
    licence: MIT
    verdict: ok
    modes: [link]
    evidence:
      - {source: example.org/lib@v1.0.0 LICENSE, read: 2026-11-23, version: v1.0.0}
    mark: needs-approval
    contains:
      - {module: example.org/dep, version: v0.2.0, licence: BSD-3-Clause}
    default: true
  - id: river-like
    names: [river-like]
    form: library
    behind: jobs
    modules: [example.org/mpl]
    pin: v0.5.0
    licence: MPL-2.0
    verdict: ok-with-conditions
    modes: [link]
    conditions:
      - {condition: one NOTICE line with the source URL and the version, row: L8}
    evidence:
      - {source: example.org/mpl@v0.5.0 LICENSE, read: 2026-11-23, version: v0.5.0}
    mark: needs-approval
    default: false
  - id: tool
    names: [tool]
    form: process
    behind: none
    pin: v8.0.0
    licence: MIT
    verdict: ok
    modes: [tool]
    evidence:
      - {source: https://example.org/tool/LICENSE, read: 2026-11-23, version: v8.0.0}
    mark: needs-approval
    default: false
  - id: store
    role: metrics-store
    names: [store]
    form: process
    behind: none
    licence: AGPL-3.0-only
    verdict: ok-with-conditions
    modes: [pull]
    conditions:
      - {condition: run unmodified as a separate process, row: L2}
    evidence:
      - {source: https://example.org/store/LICENSE, read: 2026-11-23, version: v3.2.4}
    mark: never
    default: true
  - id: refused
    names: [refused]
    form: process
    behind: none
    licence: LicenseRef-Refused-EULA
    verdict: replace-or-rebuild
    modes: [none]
    evidence:
      - {source: licence record §2.1, read: 2026-11-11, version: default branch}
    mark: never
    default: false
exceptions:
  - {item: the Go language and SPDX identifiers, tokens: [go, golang, spdx], places: [every place], source: ADR-054 decision 21}
baseProviders: [jobs-river]
`

func baseInputs() inputs {
	return inputs{
		graph:        map[string]string{"example.org/lib": "v1.0.0", "example.org/dep": "v0.2.0", "example.org/mpl": "v0.5.0"},
		builds:       map[string][]string{"example.org/lib": {"v1.0.0"}, "example.org/dep": {"v0.2.0"}},
		dist:         map[string]string{"dist/dist.yaml": "modules: [aiops/clock]\n"},
		sec32:        []string{"gateway", "metrics-store"},
		agplBoundary: []string{"metrics-store"},
		record:       map[string]string{"gateway": "Apache-2.0"},
	}
}

func run(t *testing.T, src string, in inputs) []string {
	t.Helper()
	r, err := parse([]byte(src))
	if err != nil {
		return []string{err.Error()}
	}
	return check(r, in)
}

func TestBasePasses(t *testing.T) {
	if got := run(t, base, baseInputs()); len(got) != 0 {
		t.Fatalf("U7 failed the base register:\n%s", strings.Join(got, "\n"))
	}
}

// Each case breaks one clause of ADR-054 decision 12 and U7 names it.
func TestEachClauseFails(t *testing.T) {
	edit := func(old, new string) string {
		if !strings.Contains(base, old) {
			t.Fatalf("base holds no %q", old)
		}
		return strings.Replace(base, old, new, 1)
	}
	for _, c := range []struct {
		name, src, want string
		in              func(*inputs)
	}{
		{name: "unknown key", src: edit("    form: library\n", "    form: library\n    colour: red\n"), want: "colour"},
		{name: "missing required key", src: edit("    mark: needs-approval\n    contains:", "    contains:"), want: "lib: mark is required"},
		{name: "value outside an enumeration", src: edit("verdict: ok\n    modes: [tool]", "verdict: fine\n    modes: [tool]"), want: "tool: verdict"},
		{name: "mode outside the list", src: edit("modes: [tool]", "modes: [borrow]"), want: "tool: mode"},
		{name: "id twice", src: edit("  - id: tool\n", "  - id: lib\n"), want: "lib: id is not unique"},
		{name: "process with no role", src: edit("    role: metrics-store\n", ""), want: "store: role is required"},
		{name: "conditions missing", src: edit("    conditions:\n      - {condition: run unmodified as a separate process, row: L2}\n", ""), want: "store: verdict ok-with-conditions needs conditions"},
		{name: "evidence missing", src: edit("    evidence:\n      - {source: https://example.org/tool/LICENSE, read: 2026-11-23, version: v8.0.0}\n", ""), want: "tool: evidence is required"},
		{name: "OR with no elected", src: edit("licence: MIT\n    verdict: ok\n    modes: [tool]", "licence: MIT OR GPL-2.0-only\n    verdict: ok\n    modes: [tool]"), want: "tool: licence holds OR"},
		{name: "unverified", src: edit("verdict: ok\n    modes: [tool]", "verdict: unverified\n    modes: [tool]"), want: "tool: unverified"},
		{name: "default not ok", src: edit("verdict: ok\n    modes: [link]\n    evidence:\n      - {source: example.org/lib", "verdict: optional-plugin-only\n    modes: [link]\n    evidence:\n      - {source: example.org/lib"), want: "lib: default"},
		{name: "refused entry in a dist file", src: base, in: func(i *inputs) { i.dist["dist/dist.yaml"] = "modules: [refused]\n" }, want: "refused: a replace-or-rebuild entry appears in dist/dist.yaml"},
		{name: "AGPL with modules", src: edit("    licence: AGPL-3.0-only\n", "    modules: [example.org/store]\n    licence: AGPL-3.0-only\n"), want: "store: an AGPL entry has modules"},
		{name: "ship with no sbom", src: edit("modes: [pull]", "modes: [ship]"), want: "store: a ship entry needs sbom and source"},
		{name: "linked licence outside L7", src: edit("licence: MIT\n    verdict: ok\n    modes: [link]", "licence: GPL-2.0-only\n    verdict: ok\n    modes: [link]"), want: "lib: linked licence GPL-2.0-only is outside L7"},
		{name: "MPL-2.0 with no condition of L7 or L8", src: edit("row: L8", "row: L3"), want: "river-like: MPL-2.0 needs a condition"},
		{name: "module with no entry", src: base, in: func(i *inputs) { i.graph["example.org/new"] = "v1.0.0" }, want: "example.org/new v1.0.0: no entry"},
		{name: "pin moved in the graph", src: base, in: func(i *inputs) { i.graph["example.org/lib"] = "v1.1.0" }, want: "example.org/lib: the build selects v1.1.0, the pin is v1.0.0"},
		{name: "pin moved in a build list", src: base, in: func(i *inputs) { i.builds["example.org/lib"] = []string{"v1.0.0", "v1.2.0"} }, want: "example.org/lib: the build selects v1.2.0, the pin is v1.0.0"},
		{name: "contains moved", src: base, in: func(i *inputs) { i.graph["example.org/dep"] = "v0.3.0" }, want: "example.org/dep: the build selects v0.3.0, the pin is v0.2.0"},
		{name: "AGPL component outside the boundary list", src: base, in: func(i *inputs) { i.agplBoundary = nil }, want: "metrics-store: AGPL"},
		{name: "AGPL from the licence record", src: base, in: func(i *inputs) { i.record["gateway"] = "AGPL-3.0-only; Apache-2.0 sub-trees" }, want: "gateway: AGPL"},
	} {
		in := baseInputs()
		if c.in != nil {
			c.in(&in)
		}
		got := run(t, c.src, in)
		if !slices.ContainsFunc(got, func(s string) bool { return strings.Contains(s, c.want) }) {
			t.Errorf("%s: want a finding with %q, got:\n%s", c.name, c.want, strings.Join(got, "\n"))
		}
	}
}

// The readers of 00 §3.2, of the AGPL boundary list and of the licence record find
// what the documents hold.
func TestDocumentReaders(t *testing.T) {
	root := filepath.Join("..", "..")
	dec, err := os.ReadFile(filepath.Join(root, "docs/architecture/00-decisions.md"))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := os.ReadFile(filepath.Join(root, "docs/design/licence-audit-2026-11.md"))
	if err != nil {
		t.Fatal(err)
	}
	ids, boundary := sec32(string(dec)), boundaryList(string(dec))
	for _, id := range []string{"gateway", "kernel-db", "dashboard-ui", "profiles-store"} {
		if !slices.Contains(ids, id) {
			t.Errorf("00 §3.2 ids %v lack %s", ids, id)
		}
	}
	if want := []string{"dashboard-ui", "metrics-store", "logs-store", "traces-store", "profiles-store"}; !slices.Equal(boundary, want) {
		t.Errorf("boundary list %v, want %v", boundary, want)
	}
	licences := recordLicences(string(rec))
	if !strings.HasPrefix(licences["dashboard-ui"], "AGPL") || !strings.HasPrefix(licences["gateway"], "Apache-2.0") {
		t.Errorf("record licences: dashboard-ui %q, gateway %q", licences["dashboard-ui"], licences["gateway"])
	}
}
