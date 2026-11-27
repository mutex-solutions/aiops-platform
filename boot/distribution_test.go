package boot

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// distFile is a signed distribution manifest of a site build with every field of
// ADR-060 decision 20.
func distFile() string {
	return `distVersion: 1
plugins:
  aiops/clock:
    package: github.com/mutex-solutions/aiops-platform/plugins/clock
    manifest: |
` + indent(minimalManifest, 6) + `
    attestation: |
` + indent(statement, 6) + `
  acme/echoer:
    package: example.com/acme/echoer
    manifest: "manifestVersion: \"1.0\"\n"
modules:
  - {path: example.com/acme/echoer, version: v0.1.1, sum: "` + sumA + `", licence: Apache-2.0}
  - {path: github.com/mutex-solutions/aiops-platform, licence: Apache-2.0}
  - {path: github.com/mutex-solutions/aiops-platform/sdk, replace: {path: ./sdk}, licence: Apache-2.0}
  - {path: go.yaml.in/yaml/v3, version: v3.0.6, sum: "` + sumA + `", licence: Apache-2.0 AND MIT}
packages: [example.com/acme/echoer, github.com/mutex-solutions/aiops-platform/boot]
pins:
  - {path: example.com/acme/echoer, version: v0.1.1, sum: "` + sumA + `", gomod: "` + sumA + `", release: v0.1.0}
  - {path: golang.org/toolchain, version: v0.0.1-go1.26.8.linux-amd64, sum: "` + sumA + `", gomod: "` + sumA + `"}
  - {path: golang.org/x/text, version: v0.43.0, gomod: "` + sumA + `", unchecked: true}
bindings:
  providers: {configRefsReady: [aiops/config-refs], configRefsDryRun: [aiops/config-refs]}
  consumers: {loader: [aiops/reload]}
  sealedHooks: {internal/status: [aiops/readiness, aiops/restart-policy]}
  required: {keys: [clock], rows: [loader, reload]}
site:
  consumers: {loader: [acme/echoer]}
acceptLicenses: [GPL-3.0-only, LicenseRef-acme]
serves:
  pluginApi: "0.2"
  kernel:
    keys:
      logger: {provider: open, consumer: open}
      registry: {provider: bound, consumer: bound}
    hooks:
      internal/status: {stages: [default], access: sealed, remote: "no"}
  definitions:
    echoers:
      - version: "0.1"
        compatibleFrom: "0.1"
        suite: 1
        classes:
          echoers: {provider: open, consumer: open}
    notifiers:
      - version: "0.3"
        deprecated: {since: 0.4.0, removeNotBefore: "2027-11-23", successor: v1}
        suite: 2
        classes:
          notifiers: {provider: bound, consumer: signed}
        hooks:
          notify/pre-send: {stages: [early, default], access: open, remote: "yes"}
      - version: "1.0"
        suite: 1
        classes:
          notifiers: {provider: bound, consumer: open}
`
}

func distValue(digest string) Distribution {
	return Distribution{
		Digest: digest,
		Plugins: map[string]DistPlugin{
			"aiops/clock": {Package: "github.com/mutex-solutions/aiops-platform/plugins/clock", Manifest: []byte(minimalManifest),
				Attestation: []byte(statement + "\n")},
			"acme/echoer": {Package: "example.com/acme/echoer", Manifest: []byte("manifestVersion: \"1.0\"\n")},
		},
		Modules: []DistModule{
			{GoModule: GoModule{"example.com/acme/echoer", "v0.1.1", sumA}, Licence: "Apache-2.0"},
			{GoModule: GoModule{Path: "github.com/mutex-solutions/aiops-platform"}, Licence: "Apache-2.0"},
			{GoModule: GoModule{Path: "github.com/mutex-solutions/aiops-platform/sdk"}, Replace: &GoModule{Path: "./sdk"}, Licence: "Apache-2.0"},
			{GoModule: GoModule{"go.yaml.in/yaml/v3", "v3.0.6", sumA}, Licence: "Apache-2.0 AND MIT"},
		},
		Packages: []string{"example.com/acme/echoer", "github.com/mutex-solutions/aiops-platform/boot"},
		Pins: []DistPin{
			{Path: "example.com/acme/echoer", Version: "v0.1.1", Sum: sumA, GoMod: sumA, Release: "v0.1.0"},
			{Path: "golang.org/toolchain", Version: "v0.0.1-go1.26.8.linux-amd64", Sum: sumA, GoMod: sumA},
			{Path: "golang.org/x/text", Version: "v0.43.0", GoMod: sumA, Unchecked: true},
		},
		Release: DistManifest{
			Providers:    []Binding{{"configRefsDryRun", []string{"aiops/config-refs"}}, {"configRefsReady", []string{"aiops/config-refs"}}},
			Consumers:    []Binding{{"loader", []string{"aiops/reload"}}},
			SealedHooks:  []HookListeners{{"internal/status", []string{"aiops/readiness", "aiops/restart-policy"}}},
			RequiredKeys: []string{"clock"}, RequiredRows: []string{"loader", "reload"},
		},
		Site:           DistManifest{Consumers: []Binding{{"loader", []string{"acme/echoer"}}}},
		AcceptLicenses: []string{"GPL-3.0-only", "LicenseRef-acme"},
		Serves: Serves{
			PluginAPI: "0.2",
			Kernel: ServedDefinition{Classes: map[string]KeyClass{"logger": {"open", "open"}, "registry": {"bound", "bound"}},
				Hooks: map[string]ServedHook{"internal/status": {[]string{"default"}, "sealed", "no"}}},
			Definitions: map[string][]ServedDefinition{
				"echoers": {{Version: "0.1", CompatibleFrom: "0.1", Suite: 1, Classes: map[string]KeyClass{"echoers": {"open", "open"}}}},
				"notifiers": {
					{Version: "0.3", Suite: 2, Deprecated: map[string]string{"since": "0.4.0", "removeNotBefore": "2027-11-23", "successor": "v1"},
						Classes: map[string]KeyClass{"notifiers": {"bound", "signed"}},
						Hooks:   map[string]ServedHook{"notify/pre-send": {[]string{"early", "default"}, "open", "yes"}}},
					{Version: "1.0", Suite: 1, Classes: map[string]KeyClass{"notifiers": {"bound", "open"}}},
				},
			},
		},
	}
}

func TestReadDistribution(t *testing.T) {
	data := []byte(distFile())
	got, err := ReadDistribution("dist.yaml", data)
	if want := distValue(digestOf(data)); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("%v\n got %+v\nwant %+v", err, got, want)
	}
	// A release carries no site, and the fields of bindings are optional.
	min := "distVersion: 1\nplugins: {}\nmodules: []\npackages: []\npins: []\nbindings: {}\nacceptLicenses: []\n" +
		"serves: {pluginApi: \"0.2\", kernel: {keys: {}, hooks: {}}, definitions: {}}\n"
	got, err = ReadDistribution("dist.yaml", []byte(min))
	if err != nil || len(got.Plugins) != 0 || !reflect.DeepEqual(got.Release, DistManifest{}) || !reflect.DeepEqual(got.Site, DistManifest{}) ||
		got.Serves.Kernel.Hooks != nil || len(got.Serves.Kernel.Classes) != 0 {
		t.Fatalf("%v: %+v", err, got)
	}
	for _, c := range []struct {
		name  string
		edits []string
		rule  Rule
		want  string
	}{
		{"another version", []string{"distVersion: 1", "distVersion: 2"}, RuleDist, "distVersion 2 is unknown"},
		{"no serves", []string{"\nserves:", "\nserved:"}, RuleUnknownField, `"served"`},
		{"no pins", []string{"\npins:", "\nlocks:"}, RuleUnknownField, `"locks"`},
		{"a plugin id", []string{"  acme/echoer:", "  echoer:"}, RuleDist, `plugins names "echoer"`},
		{"a plugin field", []string{"    package: example.com/acme/echoer", "    module: example.com/acme/echoer"}, RuleUnknownField, `"module"`},
		{"no package", []string{"    package: example.com/acme/echoer\n", ""}, RuleDist, "plugin acme/echoer states package"},
		{"a package", []string{"package: example.com/acme/echoer", `package: "example.com/a b"`}, RuleDist, `package "example.com/a b"`},
		{"a manifest that is no text", []string{`manifest: "manifestVersion: \"1.0\"\n"`, "manifest: {}"}, RuleType, "manifest must be a string"},
		{"an attestation that is no text", []string{"    attestation: |\n" + indent(statement, 6), "    attestation: []"}, RuleType,
			"attestation must be a string"},
		{"a version without a sum", []string{"version: v3.0.6, sum: \"" + sumA + "\",", "version: v3.0.6,"}, RuleDist, "both or neither"},
		{"a replaced module with a version", []string{"replace: {path: ./sdk}", "version: v0.0.0, sum: \"" + sumA + "\", replace: {path: ./sdk}"},
			RuleDist, "records a replacement"},
		{"a replacement field", []string{"{path: ./sdk}", "{path: ./sdk, dir: x}"}, RuleUnknownField, `"dir"`},
		{"a module licence", []string{"licence: Apache-2.0 AND MIT", "licence: Apache 2"}, RuleDist, "modules licence"},
		{"a module version", []string{"version: v3.0.6", "version: 3.0.6"}, RuleDist, "modules version"},
		{"unsorted modules", []string{"path: go.yaml.in/yaml/v3", "path: a.org/yaml"}, RuleDist, "modules is a set"},
		{"unsorted packages", []string{"[example.com/acme/echoer, ", "[z.org/x, "}, RuleDist, "packages is a set"},
		{"a pin with no hash", []string{"version: v0.43.0, gomod: \"" + sumA + "\"", "version: v0.43.0"}, RuleDist, "records sum, gomod or both"},
		{"unsorted pins", []string{"path: golang.org/x/text", "path: golang.org/a"}, RuleDist, "pins is a set"},
		{"a pin release", []string{"release: v0.1.0", "release: 0.1.0"}, RuleDist, "pins release"},
		{"a pin gomod", []string{"gomod: \"" + sumA + "\", unchecked", "gomod: x, unchecked"}, RuleDist, "pins gomod"},
		{"a pin unchecked", []string{"unchecked: true", "unchecked: 1"}, RuleType, "pins unchecked must be true or false"},
		{"a bindings field", []string{"  required: {keys:", "  needed: {keys:"}, RuleUnknownField, `"needed"`},
		{"a provider key", []string{"{configRefsReady:", "{\"a b\":"}, RuleDist, `bindings.providers names "a b"`},
		{"a sealed hook", []string{"{internal/status: [aiops", "{\"/status\": [aiops"}, RuleDist, `bindings.sealedHooks names "/status"`},
		{"unsorted listeners", []string{"[aiops/readiness, aiops/restart-policy]", "[aiops/restart-policy, aiops/readiness]"}, RuleDist,
			"bindings.sealedHooks internal/status is a set"},
		{"a consumer id", []string{"[acme/echoer]}", "[core:group]}"}, RuleDist, `site.consumers loader entry "core:group"`},
		{"a required field", []string{"{keys: [clock], rows:", "{keys: [clock], names:"}, RuleUnknownField, `"names"`},
		{"a required row", []string{"rows: [loader, reload]", "rows: [loader, \"a b\"]"}, RuleDist, "bindings.required.rows entry"},
		{"a site field", []string{"site:\n  consumers:", "site:\n  users:"}, RuleUnknownField, `"users"`},
		{"an accepted expression", []string{"[GPL-3.0-only, ", "[GPL-3.0-only OR MIT, "}, RuleDist, "acceptLicenses entry"},
		{"pluginApi", []string{`pluginApi: "0.2"`, `pluginApi: "0.2.0"`}, RuleDist, "serves.pluginApi"},
		{"no kernel hooks", []string{"    hooks:\n      internal/status", "    hookz:\n      internal/status"}, RuleUnknownField, `"hookz"`},
		{"a class", []string{"logger: {provider: open,", "logger: {provider: signed,"}, RuleDist, `serves.kernel.keys provider "signed"`},
		{"a class with no consumer", []string{"registry: {provider: bound, consumer: bound}", "registry: {provider: bound}"}, RuleDist,
			"serves.kernel.keys registry states consumer"},
		{"a key name", []string{"      logger:", "      \"log ger\":"}, RuleDist, `serves.kernel.keys names "log ger"`},
		{"an access", []string{"access: sealed", "access: closed"}, RuleDist, "serves.kernel.hooks access"},
		{"a remote mark", []string{`remote: "yes"`, `remote: contrib`}, RuleDist, "hooks remote"},
		{"no stage", []string{"stages: [default], access: sealed", "stages: [], access: sealed"}, RuleDist, "names each stage once"},
		{"a stage twice", []string{"[early, default]", "[early, early]"}, RuleDist, "names each stage once"},
		{"a stage name", []string{"[early, default]", "[early, \"de fault\"]"}, RuleDist, "stage"},
		{"a definition name", []string{"    echoers:\n      - version", "    Echoers:\n      - version"}, RuleDist, `serves.definitions names "Echoers"`},
		{"no major", []string{"    echoers:\n      - version: \"0.1\"\n        compatibleFrom: \"0.1\"\n        suite: 1\n        classes:\n          echoers: {provider: open, consumer: open}\n",
			"    echoers: []\n"}, RuleDist, "serves.definitions echoers serves no major"},
		{"unsorted majors", []string{`version: "1.0"`, `version: "0.4"`}, RuleDist, "serves.definitions notifiers is a set"},
		{"no suite", []string{"        suite: 2\n", ""}, RuleDist, "states suite"},
		{"a negative suite", []string{"suite: 2", "suite: -2"}, RuleType, "non-negative integer"},
		{"compatibleFrom above the version", []string{`compatibleFrom: "0.1"`, `compatibleFrom: "0.2"`}, RuleDist, "compatibleFrom 0.2"},
		{"compatibleFrom of a stable major", []string{"        suite: 1\n        classes:\n          notifiers: {provider: bound, consumer: open}",
			"        compatibleFrom: \"1.0\"\n        suite: 1\n        classes:\n          notifiers: {provider: bound, consumer: open}"}, RuleDist, "compatibleFrom 1.0"},
		{"compatibleFrom of another major", []string{`compatibleFrom: "0.1"`, `compatibleFrom: "1.0"`}, RuleDist, "compatibleFrom 1.0"},
		{"a date", []string{`"2027-11-23"`, `"23 Nov 2027"`}, RuleDist, "deprecated removeNotBefore"},
		{"a successor of the same major", []string{"successor: v1", "successor: v0"}, RuleDist, "deprecated successor"},
		{"a successor with a leading zero", []string{"successor: v1", "successor: v01"}, RuleDist, "deprecated successor"},
		{"a successor that is no major", []string{"successor: v1", "successor: x"}, RuleDist, "deprecated successor"},
		{"no since", []string{"since: 0.4.0, ", ""}, RuleDist, "deprecated states since"},
		{"an empty since", []string{"since: 0.4.0", `since: ""`}, RuleDist, "deprecated since"},
	} {
		src := strings.NewReplacer(c.edits...).Replace(distFile())
		if src == distFile() {
			t.Fatalf("%s: the edit changed nothing", c.name)
		}
		_, err := ReadDistribution("dist.yaml", []byte(src))
		var e *Error
		if !errors.As(err, &e) || e.Rule != c.rule || e.File != "dist.yaml" || !strings.Contains(e.Msg, c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	for _, c := range []struct {
		data string
		rule Rule
	}{{strings.Repeat("#", MaxDistBytes+1), RuleLimit}, {"a: [", RuleSyntax}, {"[]", RuleType}, {"plugins: {}", RuleDist}} {
		var e *Error
		if _, err := ReadDistribution("dist.yaml", []byte(c.data)); !errors.As(err, &e) || e.Rule != c.rule {
			t.Errorf("%.20q: %v", c.data, err)
		}
	}
}

// Table reads the module table from the signed manifest as profile render does: each
// entry from its manifest, with the module that holds its package and the ordinal of
// each declared stage in its hook's chain; a manifest that does not read leaves the id
// alone, which check 1 denies (ADR-060 decisions 7 and 14).
func TestDistributionTable(t *testing.T) {
	d := newTestDist()
	d.modules = "[{path: " + testModule + ", licence: Apache-2.0}, {path: " + testModule + "/sub, version: v1.2.0, sum: \"" + sumA + "\", licence: MIT}]"
	d.packageOf = func(id string) string { return testModule + "/sub/" + id }
	d.definitions = `{h: [{version: "0.1", suite: 1, classes: {}, hooks: {h/x: {stages: [early, late], access: open, remote: "no"}}}]}`
	d.add(manifestText("o/a", "hooks: [{hook: h/x, stage: late}, {hook: h/y, stage: default}]",
		`configSchema: {$schema: "https://json-schema.org/draft/2020-12/schema", properties: {n: {type: string, x-aiops-volatile: true}}}`), "")
	d.plugins["o/bad"] = [2]string{"id: o/bad\n", "-"}
	dist, err := ReadDistribution("d", d.text())
	if err != nil {
		t.Fatal(err)
	}
	tab := dist.Table()
	a := tab["o/a"]
	want := []HookDecl{{Hook: "h/x", Stage: 1, StageName: "late"}, {Hook: "h/y", Stage: -1, StageName: "default"}}
	if a.ID != "o/a" || a.Module != testModule+"/sub" || a.Version != "v1.2.0" || a.Sum != sumA || !reflect.DeepEqual(a.Hooks, want) ||
		!reflect.DeepEqual(a.Volatile, []string{"config.n"}) || a.Schema["properties"] == nil {
		t.Errorf("o/a: %+v", a)
	}
	if b := tab["o/bad"]; b.ID != "o/bad" || b.Module != testModule+"/sub" || b.Provides != nil || b.Hooks != nil || b.Schema != nil {
		t.Errorf("o/bad: %+v", b)
	}
	// Placing a table of Go values leaves a plugin the manifest does not list as it is.
	other := ModuleEntry{ID: "o/other", Module: "m", Version: "v1"}
	if got := dist.place(ModuleTable{"o/other": other}); !reflect.DeepEqual(got["o/other"], other) {
		t.Errorf("o/other: %+v", got["o/other"])
	}
}
