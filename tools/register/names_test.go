package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// namesRegister holds names of the forms U1 reads: one word, a kebab-case name, a
// name with a dot, a name an item excepts in some places and one it excepts in every
// place.
const namesRegister = `
entries:
  - id: port
    names: [cordis]
  - id: yaml-lib
    names: [go-yaml]
    importers:
      - {dir: boot, decision: an accepted decision}
  - id: queue
    names: [river, riverqueue]
    plugins: [jobs-river]
    importers:
      - {dir: plugins/jobs-river, decision: an accepted decision}
  - id: node
    names: [nodejs, node.js]
  - id: lang
    names: [go, golang]
  - id: object-api
    names: [s3]
exceptions:
  - {item: the port, tokens: [cordis], places: [sdk/kernel/, 00 §2.1 row 1, prose that no check reads]}
  - {item: the language, tokens: [go, golang], places: [every place]}
  - {item: the protocols, tokens: [s3], places: [ids]}
`

// U1 fails on a name of an entry, as a whole word, a kebab-case or snake-case segment
// or a camel-case part, outside the places an item of exceptions gives it and the
// directories of the entry's plugins and importers; in a fake or a suite the finding is
// U3's (ADR-054 decisions 9, 14, 15 and 17).
func TestNameLint(t *testing.T) {
	r, err := parse([]byte(namesRegister))
	if err != nil {
		t.Fatal(err)
	}
	def := "sdk/def/a/v0/a.go"
	for _, c := range []struct {
		name string
		p    place
		want string // a substring of the one finding; empty means none
	}{
		{"word", place{def + ":3", def, "// as River does"}, def + ":3: U1: \"river\" is a name of the register entry queue"},
		{"in a fake", place{"f", "sdk/def/a/v0/fake/f.go", "river"}, "f: U3: \"river\""},
		{"in a suite", place{"c", "sdk/def/a/v0/conformance/c.go", "river"}, "c: U3: \"river\""},
		{"in the SQL fake of the testkit", place{"s", "sdk/testkit/sqlfake/s.go", "river"}, "s: U3: \"river\""},
		{"in the testkit outside it", place{"k", "sdk/testkit/cycle/k.go", "river"}, "k: U1: \"river\""},
		{"part of a word", place{"x", def, "rivet riverside rivers"}, ""},
		{"camel-case part", place{"x", def, "var queueRiverName int"}, "\"river\""},
		{"a name of one word, whole", place{"x", def, "riverqueue"}, "\"riverqueue\""},
		{"snake-case segment", place{"x", def, "the_river_job"}, "\"river\""},
		{"kebab-case segment", place{"x", def, "jobs-river"}, "\"river\""},
		{"a name with a dot", place{"x", def, "run by Node.js"}, "\"node.js\""},
		{"a kebab-case name as camel-case parts", place{"x", "sdk/plugin/a.go", "var goYamlDecoder int"}, "\"go-yaml\""},
		{"a kebab-case name in a module path", place{"x", "sdk/plugin/a.go", `"go.yaml.in/yaml/v3"`}, "\"go-yaml\""},
		{"its importer", place{"x", "boot/a.go", `"go.yaml.in/yaml/v3"`}, ""},
		{"the directory of its plugin", place{"x", "plugins/jobs-river/internal/s0/a.go", "river"}, ""},
		{"an item's directory", place{"x", "sdk/kernel/a.go", "the Cordis model"}, ""},
		{"outside an item's directory", place{"x", "sdk/kernelx/a.go", "the Cordis model"}, "\"cordis\""},
		{"an item's documentation place", place{"x", "00 §2.1 row 1", "a port of Cordis"}, ""},
		{"another documentation place", place{"x", "00 §2.1 row 9", "a port of Cordis"}, "\"cordis\""},
		{"an item of every place", place{"x", def, "go func() {} // in golang"}, ""},
		{"a path", place{"sdk/def/river/v0/a.go", "sdk/def/river/v0/a.go", "sdk/def/river/v0/a.go"}, "sdk/def/river/v0/a.go: U1: \"river\""},
		{"an id in its importer", place{"plugins/jobs-river/plugin.yaml: id", "ids", "aiops/jobs-river"}, "plugins/jobs-river/plugin.yaml: id: U1: \"river\""},
		{"an id an item gives the name", place{"x", "ids", "aiops/blob-s3"}, ""},
		{"a name an item gives ids only", place{"x", def, "an S3 bucket"}, "\"s3\""},
		{"a schema of a core row", place{"plugins/clock/plugin.yaml: configSchema", "schemas", `{"properties":{"riverJob":{"type":"string"}}}`},
			"plugins/clock/plugin.yaml: configSchema: U1: \"river\""},
		{"a schema, though an item gives ids", place{"x", "schemas", `{"properties":{"s3":{}}}`}, "\"s3\""},
		{"a command path", place{"plugins/command-x/fixtures/commands/paths.json: river sync", "commands", "river sync"},
			"plugins/command-x/fixtures/commands/paths.json: river sync: U1: \"river\""},
		{"a command path of the entry's plugin", place{"x", "commands", "jobs-river"}, "\"river\""},
	} {
		got := lint(r, []place{c.p})
		switch {
		case c.want == "" && len(got) != 0:
			t.Errorf("%s: want no finding, got %q", c.name, got)
		case c.want != "" && (len(got) != 1 || !strings.Contains(got[0], c.want)):
			t.Errorf("%s: want one finding with %q, got %q", c.name, c.want, got)
		}
	}
}

// The documentation places of decision 22: the Decision column of 00 §2.1 without the
// text after "default:", the classes of the credential table of §5.4 and the stages of
// §5.6, each with its file line.
func TestDocPlaces(t *testing.T) {
	doc := strings.Join([]string{
		"### 2.1 Decision table",
		"",
		"| # | Topic | Decision | Rationale | Research | Status |",
		"|---|---|---|---|---|---|",
		"| 9 | Store | A store (default: River, `jobs-river`) and more. | River | r | s |",
		"",
		"### 5.4 Secrets and credentials",
		"",
		"| Credential | Holder |",
		"|---|---|",
		"| River token | a holder named River |",
		"",
		"### 5.6 Tenant lifecycle",
		"",
		"- **Delete** is a saga.",
		"  1. `quiesce`. River flush.",
		"- **Verify.** River passes.",
		"",
		"### 5.7 Cell network rules",
		"",
		"  1. A River rule.",
	}, "\n")
	want := []place{
		{decisionsFile + ":5 (00 §2.1 row 9)", "00 §2.1 row 9", " A store () and more. "},
		{decisionsFile + ":11 (00 §5.4)", "00 §5.4", " River token "},
		{decisionsFile + ":16 (00 §5.6)", "00 §5.6", "  1. `quiesce`. River flush."},
	}
	if got := docPlaces(doc); !slices.Equal(got, want) {
		t.Errorf("docPlaces:\n got %q\nwant %q", got, want)
	}
}

// The places of the repository hold no name of the register outside the places it
// gives the name (ADR-054 decisions 14 and 22). Nor do the files under sdk/, boot/ and
// loader/ hold the names of the metrics store's query endpoint and of the Kubernetes
// API, which the commit that adds U1 rewords to role names so that they hold once
// those upstreams have entries (decision 14); in the documentation places such names
// fail as their entries land (decision 22).
func TestRepositoryNames(t *testing.T) {
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, registerFile))
	if err != nil {
		t.Fatal(err)
	}
	r, err := parse(data)
	if err != nil {
		t.Fatal(err)
	}
	ps, err := places(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"sdk/kernel/builtin.go", "sdk/def/clock/v0/fake/fake.go", "boot/admit.go", "loader/apply.go", "ids", "schemas", "00 §2.1 row 9", "00 §5.4", "00 §5.6"} {
		if !slices.ContainsFunc(ps, func(p place) bool { return p.key == key }) {
			t.Errorf("no place has the key %s", key)
		}
	}
	if !slices.ContainsFunc(ps, func(p place) bool { return p.key == "ids" && p.text == "aiops/clock" }) {
		t.Error("the id of plugins/clock is no place")
	}
	// U1 reads each path that a command row lists in fixtures/commands/paths.json, the
	// file the suite holds its registrations to (ADR-061 decision 5).
	if !slices.ContainsFunc(ps, func(p place) bool { return p.key == "commands" && p.text == "profile render" }) {
		t.Error("the path profile render of plugins/command-profile is no place")
	}
	// U1 reads the schema of a row of aiops-core as it reads its id (decision 14).
	if !slices.ContainsFunc(ps, func(p place) bool { return p.key == "schemas" && strings.Contains(p.text, "maxFailures") }) {
		t.Error("the configSchema of plugins/restart-policy is no place")
	}
	// U1 reads the id of each row of a bundle that a shipped profile lists, and each
	// golden dump of a shipped profile, line by line (decision 14; 00:651; 08:1266).
	if !slices.ContainsFunc(ps, func(p place) bool { return p.key == "ids" && p.text == "restart-policy" }) {
		t.Error("the row restart-policy of aiops-core, which the cli profile lists, is no place")
	}
	if !slices.ContainsFunc(ps, func(p place) bool { return p.key == "ids" && p.text == "command-profile" }) {
		t.Error("the row command-profile of aiops-cli, which the cli profile lists, is no place")
	}
	for dump, line := range map[string]string{"cmd/aiopsctl/testdata/cli/cli.dump.golden": "plugin=aiops/cli", "cmd/aiopsctl/testdata/cli/cli.hooks.golden": ""} {
		if !slices.ContainsFunc(ps, func(p place) bool { return p.key == dump && strings.Contains(p.text, line) }) {
			t.Errorf("the golden dump %s of the shipped cli profile is no place", dump)
		}
	}
	if bad := lint(r, ps); len(bad) > 0 {
		t.Errorf("U1 or U3 finds names in the repository:\n%s", strings.Join(bad, "\n"))
	}
	r.Entries = append(r.Entries, entry{ID: "metrics-store", Names: []string{"prometheus"}}, entry{ID: "kubernetes-api", Names: []string{"kubernetes", "k8s"}})
	code := slices.DeleteFunc(ps, func(p place) bool { return strings.HasPrefix(p.key, "00 ") })
	if bad := lint(r, code); len(bad) > 0 {
		t.Errorf("names to reword in the commit that adds U1:\n%s", strings.Join(bad, "\n"))
	}
}
