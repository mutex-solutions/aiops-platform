package boot

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// fullManifest uses every field of schema 1.0 (ADR-048 decisions 4 to 12), a ${ that
// is text in a value and a key, and a JSON Pointer with an escaped "/".
const fullManifest = `# The hand-written half.
manifestVersion: "1.0"
id: aiops/notifier-webhook
version: 1.2.3-rc.1+build.5
title: Webhook notifier
description: Sends a notification to the ${tenant} webhook.
license: Apache-2.0 OR (MIT WITH Classpath-exception-2.0 AND BSD-3-Clause+)
publisher: {id: aiops, name: The project, url: https://example.org}
source: https://example.org/src
support: {url: https://example.org/issues, email: help@example.org}
stability: preview
scope: [process, tenant]
runtimes: [inproc, remote]
capabilities:
  network: ["config:/endpoint/u~1rl", example.org, "hooks.example.org:8443"]
  secrets: [webhook-url]
integrates:
  - {system: zeta, versions: ">=1.0 <2", tested: ["1.0.0", "1.2.0"], credential: read-only, license: AGPL-3.0-only}
  - {system: alpha, versions: any, tested: ["3"], credential: broader}
# The generated half.
uses:
  - {key: dialer, major: 0, as: need}
  - {key: notifiers, major: 0, as: need}
  - {key: notifiers, major: 1, as: want}
provides:
  - {key: own.ready, major: 0}
  - {key: own.state, major: 0}
registers:
  - {key: notifiers, major: 1}
hooks:
  - {hook: internal/status, stage: default}
  - {hook: reload/operation, stage: default}
  - {hook: reload/operation, stage: late}
configSchema:
  $schema: https://json-schema.org/draft/2020-12/schema
  type: object
  properties:
    endpoint:
      type: object
      properties:
        u/rl: {type: string, default: "${x}"}
        "${k}": {type: string}
    n: {type: integer, minimum: 0, maximum: 2.5}
durable: []
requires:
  pluginApi: "0.2"
  remoteProtocol: [1, 2]
  definitions:
    notifiers: ["0.1", "1.0"]
    reload: ["0.1"]
`

func fullValue() Manifest {
	return Manifest{
		ManifestVersion: "1.0", ID: "aiops/notifier-webhook", Version: "1.2.3-rc.1+build.5", Title: "Webhook notifier",
		Description: "Sends a notification to the ${tenant} webhook.",
		License:     "Apache-2.0 OR (MIT WITH Classpath-exception-2.0 AND BSD-3-Clause+)",
		Publisher:   ManifestPublisher{ID: "aiops", Name: "The project", URL: "https://example.org"},
		Source:      "https://example.org/src",
		Support:     &ManifestSupport{URL: "https://example.org/issues", Email: "help@example.org"},
		Stability:   "preview", Scope: []string{"process", "tenant"}, Runtimes: []string{"inproc", "remote"},
		Capabilities: &ManifestCapabilities{Network: []string{"config:/endpoint/u~1rl", "example.org", "hooks.example.org:8443"},
			Secrets: []string{"webhook-url"}},
		Integrates: []ManifestIntegration{
			{System: "zeta", Versions: ">=1.0 <2", Tested: []string{"1.0.0", "1.2.0"}, Credential: "read-only", License: "AGPL-3.0-only"},
			{System: "alpha", Versions: "any", Tested: []string{"3"}, Credential: "broader"},
		},
		Uses:      []ManifestUse{{"dialer", 0, "need"}, {"notifiers", 0, "need"}, {"notifiers", 1, "want"}},
		Provides:  []ManifestKey{{"own.ready", 0}, {"own.state", 0}},
		Registers: []ManifestKey{{"notifiers", 1}},
		Hooks:     []ManifestHook{{"internal/status", "default"}, {"reload/operation", "default"}, {"reload/operation", "late"}},
		ConfigSchema: map[string]any{"$schema": jsonSchemaDialect, "type": "object", "properties": map[string]any{
			"endpoint": map[string]any{"type": "object", "properties": map[string]any{
				"u/rl": map[string]any{"type": "string", "default": "${x}"}, "${k}": map[string]any{"type": "string"}}},
			"n": map[string]any{"type": "integer", "minimum": int64(0), "maximum": 2.5}}},
		Durable: []any{},
		Requires: ManifestRequires{PluginAPI: "0.2", RemoteProtocol: []int{1, 2},
			Definitions: map[string][]string{"notifiers": {"0.1", "1.0"}, "reload": {"0.1"}}},
	}
}

// A pack with no code carries requires as its whole generated half, and a config:
// pointer of a pack names a property of another plugin's schema (ADR-048 decision 13).
const packManifest = `manifestVersion: "1.0"
id: acme/connector-examplesys
version: 0.1.0
title: Acme connector for Examplesys
license: LicenseRef-acme-1 OR DocumentRef-spdx-tool-1.2:LicenseRef-MIT-Style-2
publisher: {id: acme}
stability: experimental
scope: [process]
runtimes: []
capabilities:
  network: ["config:/endpoint"]
requires:
  definitions: {tools: ["0.1"]}
`

// minimalManifest is a manifest of code with every optional field absent.
const minimalManifest = `manifestVersion: "1.0"
id: test/minimal
version: 0.1.0
title: Minimal
license: Apache-2.0
publisher: {id: test}
stability: experimental
scope: [process]
runtimes: [inproc]
uses: []
provides: []
registers: []
hooks: []
configSchema: {}
durable: []
requires:
  pluginApi: "0.2"
  definitions: {}
`

var modes = []ManifestMode{ManifestLenient, ManifestStrict}

func TestReadManifest(t *testing.T) {
	pack := Manifest{ManifestVersion: "1.0", ID: "acme/connector-examplesys", Version: "0.1.0",
		Title: "Acme connector for Examplesys", License: "LicenseRef-acme-1 OR DocumentRef-spdx-tool-1.2:LicenseRef-MIT-Style-2",
		Publisher: ManifestPublisher{ID: "acme"}, Stability: "experimental", Scope: []string{"process"}, Runtimes: []string{},
		Capabilities: &ManifestCapabilities{Network: []string{"config:/endpoint"}},
		Requires:     ManifestRequires{Definitions: map[string][]string{"tools": {"0.1"}}}}
	minimal := Manifest{ManifestVersion: "1.0", ID: "test/minimal", Version: "0.1.0", Title: "Minimal", License: "Apache-2.0",
		Publisher: ManifestPublisher{ID: "test"}, Stability: "experimental", Scope: []string{"process"}, Runtimes: []string{"inproc"},
		Uses: []ManifestUse{}, Provides: []ManifestKey{}, Registers: []ManifestKey{}, Hooks: []ManifestHook{},
		ConfigSchema: map[string]any{}, Durable: []any{}, Requires: ManifestRequires{PluginAPI: "0.2", Definitions: map[string][]string{}}}
	for _, c := range []struct {
		src  string
		want Manifest
	}{{fullManifest, fullValue()}, {packManifest, pack}, {minimalManifest, minimal}} {
		for _, mode := range modes {
			m, err := ReadManifest("plugin.yaml", []byte(c.src), mode)
			if err != nil {
				t.Fatalf("mode %d: %v", mode, err)
			}
			if !reflect.DeepEqual(m, c.want) {
				t.Errorf("mode %d:\n got %+v\nwant %+v", mode, m, c.want)
			}
		}
	}
}

// At a minor the reader knows an unknown field makes the manifest malformed in both
// modes; at a higher minor the lenient mode ignores it at every depth and the strict
// mode fails (ADR-048 decisions 15 and 24; 07-c4-plugin-sdk.md:120).
func TestReadManifestUnknownFields(t *testing.T) {
	higher := strings.NewReplacer(`manifestVersion: "1.0"`, "manifestVersion: \"1.3\"\nflavour: x",
		"publisher: {id: aiops,", "publisher: {id: aiops, badge: y,",
		"{key: dialer, major: 0, as: need}", "{key: dialer, major: 0, as: need, why: x}",
		"  pluginApi:", "  extra: [1]\n  pluginApi:").Replace(fullManifest)
	m, err := ReadManifest("plugin.yaml", []byte(higher), ManifestLenient)
	want := fullValue()
	want.ManifestVersion = "1.3"
	if err != nil || !reflect.DeepEqual(m, want) {
		t.Fatalf("lenient at 1.3: %v\n got %+v\nwant %+v", err, m, want)
	}
	known := strings.Replace(higher, `"1.3"`, `"1.0"`, 1)
	for _, c := range []struct {
		src  string
		mode ManifestMode
	}{{higher, ManifestStrict}, {known, ManifestStrict}, {known, ManifestLenient}} {
		var e *Error
		if _, err := ReadManifest("plugin.yaml", []byte(c.src), c.mode); !errors.As(err, &e) || e.Rule != RuleUnknownField {
			t.Errorf("mode %d: %v", c.mode, err)
		}
	}
	if _, err := ReadManifest("plugin.yaml", []byte(strings.Replace(fullManifest, `"1.0"`, `"1.3"`, 1)), ManifestStrict); err != nil {
		t.Errorf("strict at 1.3 with known fields only: %v", err)
	}
}

// Every rule of ADR-048 decision 3 holds in both modes, and check 1 denies a manifest
// that breaks one (ADR-048 decision 24).
func TestReadManifestRefusals(t *testing.T) {
	type refusal struct {
		name, base string
		edits      []string // old, new, ...
		rule       Rule
		want       string
	}
	full := func(name string, rule Rule, want string, edits ...string) refusal {
		return refusal{name, fullManifest, edits, rule, want}
	}
	minimal := func(name string, rule Rule, want string, edits ...string) refusal {
		return refusal{name, minimalManifest, edits, rule, want}
	}
	cases := []refusal{
		{"too large", strings.Repeat("#", MaxFileBytes+1), nil, RuleLimit, "larger than"},
		{"not YAML", "a: [\n", nil, RuleSyntax, ""},
		{"not a mapping", "- a\n", nil, RuleType, "must be a mapping"},
		{"no manifestVersion", "id: a/b\n", nil, RuleManifest, "states manifestVersion"},
		full("a number for manifestVersion", RuleType, "must be a string", `manifestVersion: "1.0"`, "manifestVersion: 1.0"),
		full("no minor", RuleManifest, `is not "<major>.<minor>"`, `manifestVersion: "1.0"`, `manifestVersion: "1"`),
		full("an unknown major", RuleManifest, "major 2 is unknown", `manifestVersion: "1.0"`, `manifestVersion: "2.0"`),
		full("no title", RuleManifest, "states title", "title: Webhook notifier\n", ""),
		full("id", RuleManifest, `id "aiops/Notifier" is not valid`, "id: aiops/notifier-webhook", "id: aiops/Notifier"),
		full("version", RuleManifest, "version", "version: 1.2.3-rc.1+build.5", "version: v1.2.3"),
		full("title of two lines", RuleManifest, "title", "title: Webhook notifier", `title: "Webhook\nnotifier"`),
		full("license grammar", RuleManifest, "license", "license: Apache-2.0 OR", "license: Apache-2.0 or"),
		full("license reference under aiops", RuleManifest, "reserved owner aiops", "license: Apache-2.0 OR", "license: LicenseRef-x OR"),
		full("publisher.id", RuleManifest, `publisher.id "acme" is not "aiops"`, "{id: aiops,", "{id: acme,"),
		full("no publisher.id", RuleManifest, "publisher states id", "{id: aiops, name", "{name"),
		full("source", RuleManifest, "source", "source: https://example.org/src", "source: example.org/src"),
		full("empty support", RuleManifest, "support states url or email", "support: {url: https://example.org/issues, email: help@example.org}", "support: {}"),
		full("stability", RuleManifest, "stability", "stability: preview", "stability: beta"),
		full("empty scope", RuleManifest, "scope holds", "scope: [process, tenant]", "scope: []"),
		full("unsorted scope", RuleManifest, "scope is a set", "scope: [process, tenant]", "scope: [tenant, process]"),
		full("scope twice", RuleManifest, "scope is a set", "scope: [process, tenant]", "scope: [tenant, tenant]"),
		full("runtimes not a list", RuleType, "runtimes must be a list", "runtimes: [inproc, remote]", "runtimes: inproc"),
		full("network port", RuleManifest, "capabilities.network entry", ":8443", ":0"),
		full("secret name", RuleManifest, "capabilities.secrets entry", "[webhook-url]", `["webhook url"]`),
		full("pointer to no property", RuleManifest, "names no property", "u~1rl", "url"),
		full("system twice", RuleManifest, `system "zeta" twice`, "system: alpha", "system: zeta"),
		full("nothing tested", RuleManifest, "at least one tested version", `tested: ["3"]`, "tested: []"),
		full("no credential", RuleManifest, "states credential", ", credential: broader}", "}"),
		full("broader under contrib", RuleManifest, "contrib declares no credential: broader",
			"id: aiops/", "id: contrib/", "{id: aiops,", "{id: contrib,"),
		full("integrates license", RuleManifest, "integrates license", "license: AGPL-3.0-only", "license: AGPL 3"),
		full("a pack with uses", RuleManifest, "a pack with no code carries no uses", "runtimes: [inproc, remote]", "runtimes: []"),
		full("code with no hooks", RuleManifest, "a manifest of code states hooks",
			"hooks:\n  - {hook: internal/status, stage: default}\n  - {hook: reload/operation, stage: default}\n  - {hook: reload/operation, stage: late}\n", ""),
		full("unsorted uses", RuleManifest, "uses is a set", "{key: dialer,", "{key: zz,"),
		full("a use twice", RuleManifest, "uses is a set", "{key: notifiers, major: 1, as: want}", "{key: notifiers, major: 0, as: want}"),
		full("as", RuleManifest, "uses as", "as: want", "as: maybe"),
		full("negative major", RuleType, "non-negative integer", "{key: dialer, major: 0,", "{key: dialer, major: -1,"),
		full("major as a string", RuleType, "non-negative integer", "{key: dialer, major: 0,", `{key: dialer, major: "0",`),
		full("a use with no as", RuleManifest, "an entry of uses states as", "{key: dialer, major: 0, as: need}", "{key: dialer, major: 0}"),
		full("a use not a mapping", RuleType, "an entry of uses must be a mapping", "{key: dialer, major: 0, as: need}", "dialer"),
		full("a key name", RuleManifest, "provides key", "{key: own.ready", `{key: "own ready"`),
		full("a key twice", RuleManifest, "provides is a set", "{key: own.state", "{key: own.ready"),
		full("a hook name", RuleManifest, "hooks hook", "{hook: internal/status", `{hook: "/status"`),
		full("a stage name", RuleManifest, "hooks stage", "stage: late", `stage: "la te"`),
		full("instantiable with provides", RuleManifest, "instantiable", "durable: []", "durable: []\ninstantiable: true"),
		full("remoteProtocol unsorted", RuleManifest, "requires.remoteProtocol is a set", "[1, 2]", "[2, 1]"),
		full("duplicate key", RuleDuplicateKey, "", "title: Webhook notifier", "title: Webhook notifier\ntitle: Again"),
		full("anchor", RuleAlias, "", "scope: [process, tenant]", "scope: &s [process, tenant]"),
		full("tag", RuleTag, "", "title: Webhook notifier", "title: !!str Webhook notifier"),
		full("a ${ key is a field name", RuleUnknownField, `"${x}"`, "title: Webhook notifier", "title: Webhook notifier\n${x}: y"),
		minimal("configSchema not a mapping", RuleManifest, "configSchema must be a mapping", "configSchema: {}", "configSchema: []"),
		minimal("configSchema dialect", RuleManifest, "names $schema", "configSchema: {}", "configSchema: {type: object}"),
		minimal("configSchema value", RuleType, "ambiguous", "configSchema: {}", "configSchema: {a: yes}"),
		minimal("a durable entry", RuleManifest, "durable holds no entry", "durable: []", "durable: [x]"),
		minimal("durable not a list", RuleType, "durable must be a list", "durable: []", "durable: {}"),
		minimal("pluginApi", RuleManifest, "requires.pluginApi", `pluginApi: "0.2"`, `pluginApi: "0.2.1"`),
		minimal("no pluginApi", RuleManifest, "pluginApi exactly when runtimes holds inproc", `  pluginApi: "0.2"`+"\n", ""),
		minimal("no remoteProtocol", RuleManifest, "remoteProtocol exactly when runtimes holds remote", "[inproc]", "[inproc, remote]"),
		minimal("no definitions", RuleManifest, "requires states definitions", "  definitions: {}\n", ""),
		minimal("no requires", RuleManifest, "a manifest states requires", "requires:\n  pluginApi: \"0.2\"\n  definitions: {}\n", ""),
		minimal("definitions not a mapping", RuleType, "requires.definitions must be a mapping", "definitions: {}", "definitions: []"),
		minimal("definition name", RuleManifest, "not a definition name", "definitions: {}", `definitions: {Bad_Name: ["0.1"]}`),
		minimal("definition versions unsorted", RuleManifest, "is a set", "definitions: {}", `definitions: {a: ["1.0", "0.1"]}`),
		minimal("definition with no version", RuleManifest, "names no version", "definitions: {}", "definitions: {a: []}"),
		minimal("definition version", RuleManifest, "version", "definitions: {}", `definitions: {a: ["x"]}`),
		minimal("instantiable outside aiops", RuleManifest, "instantiable", "runtimes: [inproc]", "runtimes: [inproc]\ninstantiable: true"),
		minimal("instantiable not a boolean", RuleType, "instantiable must be true or false", "runtimes: [inproc]", "runtimes: [inproc]\ninstantiable: \"yes\""),
	}
	for _, c := range cases {
		src := c.base
		if len(c.edits) > 0 {
			src = strings.NewReplacer(c.edits...).Replace(c.base)
			if src == c.base {
				t.Fatalf("%s: the edit changed nothing", c.name)
			}
		}
		for _, mode := range modes {
			_, err := ReadManifest("plugin.yaml", []byte(src), mode)
			var e *Error
			if !errors.As(err, &e) || e.Rule != c.rule || !strings.Contains(e.Msg, c.want) {
				t.Errorf("%s, mode %d: %v", c.name, mode, err)
			}
		}
	}
}

func TestSPDX(t *testing.T) {
	for s, want := range map[string][2]bool{ // {ok, ref}
		"MIT":                                    {true, false},
		"MIT+":                                   {true, false},
		"((MIT OR Apache-2.0)) AND BSD-3-Clause": {true, false},
		"GPL-2.0-only WITH Classpath-exception-2.0": {true, false},
		"LicenseRef-x":                      {true, true},
		"DocumentRef-d:LicenseRef-x WITH e": {true, true},
		"":                                  {false, false},
		"MIT and Apache-2.0":                {false, false},
		"MIT OR":                            {false, false},
		"(MIT":                              {false, false},
		"MIT)":                              {false, false},
		"AND":                               {false, false},
		"MIT WITH":                          {false, false},
		"MIT WITH (":                        {false, false},
		"MIT++":                             {false, false},
		"M!T":                               {false, false},
		"LicenseRef-x+":                     {false, true},
		"DocumentRef-d:MIT":                 {false, false},
		"Doc:LicenseRef-x":                  {false, false},
		"DocumentRef-:LicenseRef-x":         {false, false},
	} {
		if ok, ref := spdx(s); ok != want[0] || ref != want[1] {
			t.Errorf("spdx(%q) = %v, %v", s, ok, ref)
		}
	}
}

func TestManifestForms(t *testing.T) {
	for s, want := range map[string]bool{
		"example.org": true, "a.b-c.example:65535": true, "LOCALHOST": true, "config:/a": true, "config:/a~0b~1c": true,
		strings.Repeat("a.", 126) + "a": true, strings.Repeat("a.", 127) + "aa": false, strings.Repeat("a", 64) + ".org": false,
		"": false, "example.org:": false, "example.org:0": false, "example.org:08": false, "example.org:65536": false,
		"-a.org": false, "a-.org": false, "a..org": false, "ex ample.org": false, "config:a": false, "config:/a~2": false,
	} {
		if okNetwork(s) != want {
			t.Errorf("okNetwork(%q) = %v", s, !want)
		}
	}
	for s, want := range map[string]bool{"a/b-c": true, "a1/b2": true, "a": false, "a/B": false, "a/b--c": false, "a/-b": false, "1a/b": false} {
		if okManifestID(s) != want {
			t.Errorf("okManifestID(%q) = %v", s, !want)
		}
	}
	if keys, ok := pointer("/a~01/~1"); !ok || !reflect.DeepEqual(keys, []string{"a~1", "/"}) {
		t.Errorf("pointer = %q, %v", keys, ok)
	}
	schema := fullValue().ConfigSchema
	for p, want := range map[string]bool{"/endpoint/u~1rl": true, "/n": true, "/n/x": false, "/zz": false} {
		if schemaHas(schema, p) != want {
			t.Errorf("schemaHas(%q) = %v", p, !want)
		}
	}
	for s, want := range map[string]bool{"1.0": true, "0.12": true, "01.0": false, "1.": false, "1.0.0": false} {
		if _, _, ok := majorMinor(s); ok != want {
			t.Errorf("majorMinor(%q) = %v", s, ok)
		}
	}
}
