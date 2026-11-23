package boot

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// The S2a clauses of the gate (ADR-060 decisions 14 to 17 and 31): one table case per
// denial clause of checks 1, 2, 4, 5, 6 and 10, of the signed and fake parts of check 7
// and of the installed-artifact clause of check 8, each on the inputs of boot, and the
// clauses each entry point does not evaluate.

const (
	echoRegisters = "registers: [{key: echoers, major: 0}]"
	echoRequires  = `requires: {pluginApi: "0.1", definitions: {echoers: ["0.1"]}}`
	echoAttested  = `{"echoers": {"version": "0.1", "suite": 1, "halves": ["obligations", "guard"]}}`
	servedDefs    = `{echoers: [{version: "0.1", suite: 1, classes: {echoers: {provider: open, consumer: open}}}],
  toy: [{version: "0.2", compatibleFrom: "0.1", suite: 0, classes: {toy.store: {provider: open, consumer: open}, toy.secret: {provider: open, consumer: signed}},
    hooks: {toy/signed: {stages: [default], access: signed, remote: "no"}}}],
  stable: [{version: "1.3", suite: 2, classes: {stable.k: {provider: open, consumer: open}}}]}`
)

// packManifestText is the manifest of the pack id: no code, feeding echoers.
func packManifestText(id string) string {
	return manifestText(id, "runtimes: []", "uses: -", "provides: -", "registers: -", "hooks: -", "configSchema: -", "durable: -",
		`requires: {definitions: {echoers: ["0.1"]}}`)
}

// statementOf writes a statement of subject name at the digest sha256:<hex>, with the
// JSON of definitions and the extra fields of the predicate.
func statementOf(name, hex, definitions, extra string) string {
	return fmt.Sprintf(`{"_type": %q, "subject": [{"name": %q, "digest": {"sha256": %q}}], "predicateType": %q, "predicate": {"definitions": %s%s}}`,
		statementType, name, hex, AttestationType, definitions, extra)
}

// gateFix is what boot holds in one case: the signed manifest, the installed artifacts,
// trust.yaml, the bundles of the profile by name, and the other inputs.
type gateFix struct {
	dist    *testDist
	arts    map[string]Artifact
	trust   Trust
	bundles map[string]string
	in      GateInput
	min     string
	// disabled composes without the patch that enables the artifacts' rows, as install,
	// plugin check and plugin gate --against compose.
	disabled bool
	// unbuilt is an installed in-process plugin that the module table does not hold yet;
	// the signed manifest does not list it either.
	unbuilt string
}

// newGateFix is a clean case: the rows plain, user and prov of in-process plugins of the
// module table, e1 of the pack acme/pack, which inserts a row of the first-party
// instantiable aiops/echo, and e2 of the in-process artifact acme/echoer, both enabled
// by the profile layer, as plugin enable writes it. Each artifact's rows mount under its
// group, so they compose as pack:e1 and echoer:e2 (ADR-047 decision 5).
func newGateFix() *gateFix {
	d := newTestDist()
	d.definitions = servedDefs
	d.add(manifestText("o/plain"), "")
	d.add(manifestText("o/user", "uses: [{key: toy.store, major: 0, as: need}]", `requires: {pluginApi: "0.1", definitions: {toy: ["0.1"]}}`),
		`{"toy": {"version": "0.1"}}`)
	d.add(manifestText("o/prov", "provides: [{key: toy.store, major: 0}]", `requires: {pluginApi: "0.1", definitions: {toy: ["0.2"]}}`),
		`{"toy": {"version": "0.2", "suite": 0}}`)
	d.add(manifestText("aiops/echo", "instantiable: true", echoRegisters, echoRequires), echoAttested)
	echoer := manifestText("acme/echoer", echoRegisters, echoRequires)
	d.add(echoer, echoAttested)
	return &gateFix{dist: d,
		arts: map[string]Artifact{
			"acme/pack": {ID: "acme/pack", Digest: "sha256:" + hexA,
				Kept: Kept{Manifest: []byte(packManifestText("acme/pack")), Bundle: []byte("- insert:\n    - {id: e1, plugin: aiops/echo, enabled: false, config: {reply: hi}}\n")},
				Record: Record{Kind: "artifact", Digest: "sha256:" + hexA, Signer: keyP256ID, ID: "acme/pack", Version: "0.1.0",
					Attestation: []byte(statementOf("acme/pack@0.1.0", hexA, `{"echoers": {"version": "0.1", "suite": 1, "halves": ["obligations"]}}`,
						`, "scenario": {"name": "echo", "version": 1, "halves": ["kernel"]}`))}},
			"acme/echoer": {ID: "acme/echoer", Digest: "sha256:" + hexB,
				Kept: Kept{Manifest: []byte(echoer), Bundle: []byte("- insert:\n    - {id: e2, plugin: acme/echoer, enabled: false}\n")},
				Record: Record{Kind: "artifact", Digest: "sha256:" + hexB, Signer: keyP256ID, ID: "acme/echoer", Version: "0.1.0",
					Runtimes: []string{"inproc"}, Module: &GoModule{Path: testModule}, Attestation: []byte(statementOf("acme/echoer@0.1.0", hexB, echoAttested, ""))}},
		},
		trust:   Trust{Signers: []TrustSigner{{KeyID: keyP256ID, IDs: []string{"acme/*"}, Runtimes: []string{"inproc"}, Signed: []string{"toy/signed"}}}},
		bundles: map[string]string{"b": rows("id: plain, plugin: o/plain", "id: user, plugin: o/user", "id: prov, plugin: o/prov")},
		in:      GateInput{Entry: EntryBoot, Tie: map[string]Tie{"acme/echoer": {Module: GoModule{Path: testModule}}}},
	}
}

// art edits the installed artifact id.
func (fx *gateFix) art(id string, edit func(a *Artifact)) {
	a := fx.arts[id]
	edit(&a)
	fx.arts[id] = a
}

// reserve moves the pack acme/pack to the plugin id id, signed by keyP256ID.
func (fx *gateFix) reserve(id string) {
	a := fx.arts["acme/pack"]
	delete(fx.arts, "acme/pack")
	a.ID, a.Record.ID, a.Kept.Manifest = id, id, []byte(packManifestText(id))
	a.Record.Attestation = []byte(strings.Replace(string(a.Record.Attestation), "acme/pack@", id+"@", 1))
	fx.arts[id] = a
}

// run composes the profile, whose bundles are b and each installed artifact's, and runs
// the gate on it.
func (fx *gateFix) run(t *testing.T) GateResult {
	t.Helper()
	home := fstest.MapFS{"bundles/b/bundle.yaml": {Data: []byte(fx.bundles["b"])}}
	ids := slices.Sorted(maps.Keys(fx.arts))
	for id := range fx.bundles {
		if id != "b" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		data, ok := fx.bundles[id]
		if !ok {
			data = string(fx.arts[id].Kept.Bundle)
		}
		home["bundles/"+id+"/bundle.yaml"] = &fstest.MapFile{Data: []byte(data)}
	}
	home["profiles/p/profile.yaml"] = &fstest.MapFile{Data: []byte("bundles: [b, " + strings.Join(ids, ", ") + "]")}
	patch := File{FS: fstest.MapFS{"enable.yml": {Data: []byte("- {id: pack:e1, enabled: true}\n- {id: echoer:e2, enabled: true}\n")}}, Path: "enable.yml"}
	patches := []File{patch}
	if fx.disabled {
		patches = nil
	}
	c, _, err := Compose(Sources{Profile: "p", Home: home, Bundles: home, Patches: patches}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	c.MinStability = fx.min
	d, err := ReadDistribution("distribution.yaml", fx.dist.text())
	if err != nil {
		t.Fatal(err)
	}
	in := fx.in
	in.Dist, in.Bindings, in.Trust, in.Artifacts, in.Mods = &d, d.Bindings(), &fx.trust, fx.arts, ModuleTable{}
	if in.Entry == EntryBuild || in.Entry == EntryCheck || in.Entry == EntryOffline {
		in.Trust = nil // these entry points have no trust.yaml
	}
	// The module table holds a Go value of each plugin whatever the signed manifest lists.
	for _, id := range []string{"o/plain", "o/user", "o/prov", "aiops/echo", "aiops/user", "acme/echoer"} {
		if id == fx.unbuilt {
			continue
		}
		in.Mods[id] = ModuleEntry{ID: id}
		if m, err := ReadManifest(id, d.Plugins[id].Manifest, ManifestLenient); err == nil {
			in.Mods[id] = m.Entry()
		}
	}
	return GateWith(c, in)
}

// openLines lists, sorted, the row results that the entry point did not evaluate.
func openLines(g GateResult) []string {
	var out []string
	for _, s := range g.Slots {
		for _, f := range s.Open {
			out = append(out, fmt.Sprintf("%d %s %s open", s.N, f.Row, f.Rule))
		}
	}
	slices.Sort(out)
	return out
}

func TestGateS2aChecksAtBoot(t *testing.T) {
	every := func(check int, rule Rule) []string {
		var out []string
		for _, r := range []string{"pack:e1", "echoer:e2", "plain", "prov", "user"} {
			out = append(out, fmt.Sprintf("%d %s %s stop=false", check, r, rule))
		}
		return out
	}
	art := func(name, digest, defs, extra string) []byte { return []byte(statementOf(name, digest, defs, extra)) }
	for _, tc := range []struct {
		name string
		edit func(fx *gateFix)
		want []string
	}{
		{"clean", func(*gateFix) {}, nil},
		// Check 1.
		{"no manifest in the signed manifest", func(fx *gateFix) { delete(fx.dist.plugins, "o/plain") },
			[]string{"1 plain manifest stop=false", "5 plain module stop=false"}},
		{"a malformed manifest", func(fx *gateFix) {
			fx.dist.plugins["o/plain"] = [2]string{manifestText("o/plain", "stability: bogus"), "-"}
		}, []string{"1 plain manifest stop=false"}},
		{"wasm", func(fx *gateFix) { fx.dist.add(manifestText("o/plain", "runtimes: [inproc, wasm]"), "") }, []string{"1 plain author-api stop=false"}},
		{"another author API", func(fx *gateFix) {
			fx.dist.add(manifestText("o/plain", `requires: {pluginApi: "0.2", definitions: {}}`), "")
		}, []string{"1 plain author-api stop=false"}},
		// Check 2.
		{"a major not served", func(fx *gateFix) {
			fx.dist.add(manifestText("o/plain", `requires: {pluginApi: "0.1", definitions: {toy: ["1.0"]}}`), `{"toy": {"version": "1.0"}}`)
		}, []string{"2 plain unserved-definition stop=false"}},
		{"a minor below compatibleFrom", func(fx *gateFix) {
			fx.dist.add(manifestText("o/plain", `requires: {pluginApi: "0.1", definitions: {toy: ["0.0"]}}`), `{"toy": {"version": "0.0"}}`)
		}, []string{"2 plain unserved-definition stop=false"}},
		{"a minor above the served one at major 0", func(fx *gateFix) {
			fx.dist.add(manifestText("o/plain", `requires: {pluginApi: "0.1", definitions: {toy: ["0.3"], echoers: ["0.1"]}}`),
				`{"toy": {"version": "0.3"}, "echoers": {"version": "0.1"}}`)
		}, []string{"2 plain unserved-definition stop=false"}},
		{"a stable minor above the served one", func(fx *gateFix) {
			fx.dist.add(manifestText("o/plain", `requires: {pluginApi: "0.1", definitions: {stable: ["1.4"]}}`), `{"stable": {"version": "1.4"}}`)
		}, []string{"2 plain unserved-definition stop=false"}},
		{"a stable minor below the served one", func(fx *gateFix) {
			fx.dist.add(manifestText("o/plain", `requires: {pluginApi: "0.1", definitions: {stable: ["1.2"]}}`), `{"stable": {"version": "1.2"}}`)
		}, nil},
		{"a definition not served", func(fx *gateFix) {
			fx.dist.add(manifestText("o/plain", `requires: {pluginApi: "0.1", definitions: {nothere: ["0.1"]}}`), `{"nothere": {"version": "0.1"}}`)
		}, []string{"2 plain unserved-definition stop=false"}},
		// Check 4.
		{"below minStability", func(fx *gateFix) {
			fx.dist.add(manifestText("o/plain", "stability: experimental"), "")
			fx.min = "preview"
		}, []string{"4 plain stability stop=false"}},
		{"at minStability", func(fx *gateFix) { fx.min = "ga" }, nil},
		// Check 5.
		{"the embedded manifest is not trusted", func(fx *gateFix) { fx.in.Trusted = errors.New("no record") }, every(5, RuleModule)},
		{"a module that does not tie", func(fx *gateFix) { fx.in.Tie["o/plain"] = Tie{Err: errors.New("another sum")} },
			[]string{"5 plain module stop=false"}},
		{"kept files that differ", func(fx *gateFix) {
			fx.art("acme/pack", func(a *Artifact) { a.Err = &Error{Rule: RuleKept, Msg: "x"} })
		}, []string{"5 pack:e1 kept stop=false"}},
		{"a broken record", func(fx *gateFix) {
			fx.art("acme/pack", func(a *Artifact) { a.Err = &Error{Rule: RuleRecord, Msg: "x"} })
		}, []string{"5 pack:e1 record stop=false"}},
		{"no record", func(fx *gateFix) {
			fx.art("acme/pack", func(a *Artifact) { a.Err = &Error{Rule: RuleMissing, Msg: "x"} })
		}, []string{"5 pack:e1 signer stop=false"}},
		{"not installed", func(fx *gateFix) {
			fx.bundles["acme/pack"] = string(fx.arts["acme/pack"].Kept.Bundle)
			delete(fx.arts, "acme/pack")
		}, []string{"5 pack:e1 signer stop=false"}},
		{"content that differs from the lock", func(fx *gateFix) { fx.art("acme/pack", func(a *Artifact) { a.Content = "sha256:" + hexC }) },
			[]string{"5 pack:e1 kept stop=false"}},
		{"a record of another id", func(fx *gateFix) { fx.art("acme/pack", func(a *Artifact) { a.Record.ID = "acme/other" }) },
			[]string{"5 pack:e1 record stop=false"}},
		{"a signer that trust.yaml does not list", func(fx *gateFix) { fx.trust.Signers[0].IDs = []string{"beta/*"} },
			[]string{"5 pack:e1 signer stop=false", "5 echoer:e2 signer stop=false"}},
		{"a signer not listed for the runtime", func(fx *gateFix) { fx.trust.Signers[0].Runtimes = nil },
			[]string{"5 echoer:e2 signer stop=false"}},
		{"an exact id", func(fx *gateFix) { fx.trust.Signers[0].IDs = []string{"acme/echoer", "acme/pack"} }, nil},
		{"unsigned", func(fx *gateFix) { fx.art("acme/pack", func(a *Artifact) { a.Record.Signer = Unsigned }) },
			[]string{"5 pack:e1 signer stop=false"}},
		{"unsigned and allowed", func(fx *gateFix) {
			fx.art("acme/pack", func(a *Artifact) { a.Record.Signer = Unsigned })
			fx.trust.AllowUnsigned = []string{"acme/pack"}
		}, nil},
		{"an unsigned pack under a reserved owner, though allowUnsigned lists it", func(fx *gateFix) {
			fx.reserve("contrib/pack")
			fx.art("contrib/pack", func(a *Artifact) { a.Record.Signer = Unsigned })
			fx.trust.AllowUnsigned = []string{"contrib/pack"}
		}, []string{"5 pack:e1 signer stop=false"}},
		{"a module the trusted manifest does not list", func(fx *gateFix) {
			fx.art("acme/echoer", func(a *Artifact) { a.Record.Module = &GoModule{Path: "example.com/acme", Version: "v0.1.0", Sum: sumA} })
		}, []string{"5 echoer:e2 module stop=false", "10 echoer:e2 attestation stop=false"}},
		// Check 6.
		{"a licence outside the allow-list", func(fx *gateFix) {
			fx.dist.modules = "[{path: " + testModule + ", licence: Apache-2.0}, {path: x.test/gpl, version: v1.0.0, sum: \"" + sumA + "\", licence: GPL-3.0-only OR (MIT AND BUSL-1.1)}]"
		}, every(6, RuleLicence)},
		{"an accepted licence", func(fx *gateFix) {
			fx.dist.modules = "[{path: " + testModule + ", licence: Apache-2.0}, {path: x.test/gpl, version: v1.0.0, sum: \"" + sumA + "\", licence: (GPL-3.0-only WITH Classpath-exception-2.0) OR BUSL-1.1}]"
			fx.dist.accept = "[GPL-3.0-only]"
		}, nil},
		{"an acceptance in the release", func(fx *gateFix) { fx.dist.accept = "[GPL-3.0-only]"; fx.in.BuiltIn = true }, every(6, RuleLicence)},
		// Check 7.
		{"a fake in the package graph", func(fx *gateFix) { fx.dist.packages = "[example.test/def/x/fake]" }, every(7, RuleFake)},
		{"sdk/testkit in the package graph", func(fx *gateFix) {
			fx.dist.packages = "[github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest]"
		}, every(7, RuleFake)},
		{"a signed key of a plugin that is not first-party", func(fx *gateFix) {
			fx.dist.bindings = "{consumers: {toy.secret: [o/user]}}"
			fx.dist.add(manifestText("o/user", "uses: [{key: toy.secret, major: 0, as: need}]", `requires: {pluginApi: "0.1", definitions: {toy: ["0.1"]}}`),
				`{"toy": {"version": "0.1"}}`)
		}, []string{"7 user signed-class stop=false"}},
		// An entry is the plugin of its key, never the id its manifest declares
		// (07-c4-plugin-sdk.md:571), so a manifest of another id is malformed.
		{"a signed key of a module-table entry whose manifest names a first-party id", func(fx *gateFix) {
			fx.dist.bindings = "{consumers: {toy.secret: [o/user]}}"
			m := manifestText("aiops/user", "uses: [{key: toy.secret, major: 0, as: need}]", `requires: {pluginApi: "0.1", definitions: {toy: ["0.1"]}}`)
			fx.dist.plugins["o/user"] = [2]string{m, attest(m, `{"toy": {"version": "0.1"}}`)}
		}, []string{"1 user manifest stop=false"}},
		{"a signed key of a first-party plugin", func(fx *gateFix) {
			fx.dist.bindings = "{consumers: {toy.secret: [aiops/user]}}"
			fx.dist.add(manifestText("aiops/user", "uses: [{key: toy.secret, major: 0, as: need}]", `requires: {pluginApi: "0.1", definitions: {toy: ["0.1"]}}`),
				`{"toy": {"version": "0.1"}}`)
			fx.bundles["b"] = rows("id: user, plugin: aiops/user")
		}, nil},
		{"a signed hook of an installed artifact whose signer is trusted for it", func(fx *gateFix) { fx.signedEchoer() }, nil},
		{"a signed hook of an installed artifact whose signer is not trusted for it", func(fx *gateFix) {
			fx.signedEchoer()
			fx.trust.Signers[0].Signed = nil
		}, []string{"7 echoer:e2 signed-class stop=false"}},
		// Check 8.
		{"a patch in an installed bundle", func(fx *gateFix) {
			fx.art("acme/pack", func(a *Artifact) {
				a.Kept.Bundle = append(a.Kept.Bundle, "- {id: e1, config: {reply: ho}}\n"...)
			})
		}, []string{"8 pack:e1 installed-artifact stop=false"}},
		{"an installed row inserted enabled", func(fx *gateFix) {
			fx.art("acme/pack", func(a *Artifact) { a.Kept.Bundle = []byte("- insert:\n    - {id: e1, plugin: aiops/echo}\n") })
		}, []string{"8 pack:e1 installed-artifact stop=false"}},
		{"an installed row of a plugin that is neither its own nor instantiable", func(fx *gateFix) {
			fx.art("acme/pack", func(a *Artifact) {
				a.Kept.Bundle = []byte("- insert:\n    - {id: e1, plugin: o/plain, enabled: false}\n")
			})
		}, []string{"8 pack:e1 installed-artifact stop=false"}},
		{"an installed row of a first-party plugin that is not instantiable", func(fx *gateFix) {
			fx.dist.add(manifestText("aiops/echo", echoRegisters, echoRequires), echoAttested)
		}, []string{"8 pack:e1 installed-artifact stop=false"}},
		{"an installed row that sets required", func(fx *gateFix) {
			fx.art("acme/pack", func(a *Artifact) {
				a.Kept.Bundle = []byte("- insert:\n    - {id: e1, plugin: aiops/echo, enabled: false, required: true}\n")
			})
		}, []string{"8 pack:e1 installed-artifact stop=true"}},
		// ADR-047 decision 19: the field is denied whatever value it holds.
		{"an installed row that sets required: false", func(fx *gateFix) {
			fx.art("acme/pack", func(a *Artifact) {
				a.Kept.Bundle = []byte("- insert:\n    - {id: e1, plugin: aiops/echo, enabled: false, required: false}\n")
			})
		}, []string{"8 pack:e1 installed-artifact stop=false"}},
		{"an installed row that sets empty inject, intercept and isolate and group: false", func(fx *gateFix) {
			fx.art("acme/pack", func(a *Artifact) {
				a.Kept.Bundle = []byte("- insert:\n    - {id: e1, plugin: aiops/echo, enabled: false, inject: [], intercept: {}, isolate: [], group: false}\n")
			})
		}, []string{"8 pack:e1 installed-artifact stop=false"}},
		{"an installed row with an env reference", func(fx *gateFix) {
			fx.art("acme/pack", func(a *Artifact) {
				a.Kept.Bundle = []byte("- insert:\n    - {id: e1, plugin: aiops/echo, enabled: false, config: {k: \"${env:AIOPS_REF_K}\"}}\n")
			})
		}, []string{"8 pack:e1 installed-artifact stop=false"}},
		{"an installed row with a file reference", func(fx *gateFix) {
			fx.art("acme/pack", func(a *Artifact) {
				a.Kept.Bundle = []byte("- insert:\n    - {id: e1, plugin: aiops/echo, enabled: false, config: {k: \"${file:k}\"}}\n")
			})
		}, []string{"8 pack:e1 installed-artifact stop=false"}},
		{"an installed row with a secret reference", func(fx *gateFix) {
			fx.art("acme/pack", func(a *Artifact) {
				a.Kept.Bundle = []byte("- insert:\n    - {id: e1, plugin: aiops/echo, enabled: false, config: {k: \"${secret:k}\"}}\n")
			})
		}, nil},
		{"a group that takes the id of a row composed before it", func(fx *gateFix) {
			fx.bundles["b"] = rows("id: plain, plugin: o/plain", "id: user, plugin: o/user", "id: prov, plugin: o/prov", "id: pack, plugin: o/plain")
		}, []string{"8 acme/pack group-clash stop=false"}},
		// The loader's row id and a required row id are not an artifact's to take.
		{"a group that takes the loader's reserved row id", func(fx *gateFix) { fx.reserve("acme/loader") },
			[]string{"8 acme/loader group-clash stop=false"}},
		{"a group that takes the id of a required row", func(fx *gateFix) { fx.dist.bindings = "{required: {rows: [pack]}}" },
			[]string{"8 pack group-clash stop=true"}},
		{"an installed bundle that does not parse", func(fx *gateFix) {
			fx.bundles["acme/pack"] = string(fx.arts["acme/pack"].Kept.Bundle)
			fx.art("acme/pack", func(a *Artifact) { a.Kept.Bundle = []byte("{}") })
		}, []string{"8 pack:e1 installed-artifact stop=false"}},
		// Check 10.
		{"no statement", func(fx *gateFix) { fx.dist.plugins["o/plain"] = [2]string{manifestText("o/plain"), "-"} },
			[]string{"10 plain attestation stop=false"}},
		{"a statement that does not read", func(fx *gateFix) { fx.dist.plugins["o/plain"] = [2]string{manifestText("o/plain"), "{}"} },
			[]string{"10 plain attestation stop=false"}},
		{"a statement of another plugin", func(fx *gateFix) {
			m := manifestText("o/plain")
			fx.dist.plugins["o/plain"] = [2]string{m, strings.Replace(attest(m, ""), "o/plain@", "o/other@", 1)}
		}, []string{"10 plain attestation stop=false"}},
		{"a statement of another digest", func(fx *gateFix) {
			fx.dist.plugins["o/plain"] = [2]string{manifestText("o/plain"), attest(manifestText("o/plain", "title: u"), "")}
		}, []string{"10 plain attestation stop=false"}},
		{"a statement that omits a definition version", func(fx *gateFix) {
			fx.dist.add(manifestText("o/user", "uses: [{key: toy.store, major: 0, as: need}]", `requires: {pluginApi: "0.1", definitions: {toy: ["0.1"]}}`), "")
		}, []string{"10 user attestation stop=false"}},
		{"a statement that names another definition version", func(fx *gateFix) {
			fx.dist.add(manifestText("o/user", "uses: [{key: toy.store, major: 0, as: need}]", `requires: {pluginApi: "0.1", definitions: {toy: ["0.1"]}}`),
				`{"toy": {"version": "0.2"}}`)
		}, []string{"10 user attestation stop=false"}},
		{"a statement that lacks the suite of a definition provided", func(fx *gateFix) {
			fx.dist.add(manifestText("o/prov", "provides: [{key: toy.store, major: 0}]", `requires: {pluginApi: "0.1", definitions: {toy: ["0.2"]}}`),
				`{"toy": {"version": "0.2"}}`)
		}, []string{"10 prov attestation stop=false"}},
		// Decision 55: an attestation passes when it names the served revision.
		{"a statement of suite revision 0 for a definition served at 1", func(fx *gateFix) {
			fx.dist.add(manifestText("aiops/echo", "instantiable: true", echoRegisters, echoRequires), `{"echoers": {"version": "0.1", "suite": 0}}`)
		}, []string{"10 pack:e1 attestation stop=false"}},
		{"a statement of a positive suite revision for a definition served at 0", func(fx *gateFix) {
			fx.dist.add(manifestText("o/prov", "provides: [{key: toy.store, major: 0}]", `requires: {pluginApi: "0.1", definitions: {toy: ["0.2"]}}`),
				`{"toy": {"version": "0.2", "suite": 7}}`)
		}, []string{"10 prov attestation stop=false"}},
		{"a pack's statement of a stale suite revision of a registry it feeds", func(fx *gateFix) {
			fx.art("acme/pack", func(a *Artifact) {
				a.Record.Attestation = art("acme/pack@0.1.0", hexA, `{"echoers": {"version": "0.1", "suite": 0}}`, `, "scenario": {"name": "echo", "version": 1, "halves": ["kernel"]}`)
			})
		}, []string{"10 pack:e1 attestation stop=false"}},
		{"a pack's statement that lacks the suite of a registry it feeds", func(fx *gateFix) {
			fx.art("acme/pack", func(a *Artifact) {
				a.Record.Attestation = art("acme/pack@0.1.0", hexA, `{"echoers": {"version": "0.1"}}`, `, "scenario": {"name": "echo", "version": 1, "halves": ["kernel"]}`)
			})
		}, []string{"10 pack:e1 attestation stop=false"}},
		{"a pack's statement with no scenario", func(fx *gateFix) {
			fx.art("acme/pack", func(a *Artifact) { a.Record.Attestation = art("acme/pack@0.1.0", hexA, echoAttested, "") })
		}, []string{"10 pack:e1 attestation stop=false"}},
		{"a pack's statement of another digest", func(fx *gateFix) {
			fx.art("acme/pack", func(a *Artifact) { a.Record.Attestation = art("acme/pack@0.1.0", hexC, echoAttested, "") })
		}, []string{"10 pack:e1 attestation stop=false"}},
		// ADR-020 decisions 9 and 10: the signed manifest holds statements for first-party
		// and test-distribution entries only; the install record of a third-party
		// in-process artifact is its attestation.
		{"a third-party in-process entry with no statement in the signed manifest", func(fx *gateFix) {
			fx.dist.plugins["acme/echoer"] = [2]string{fx.dist.plugins["acme/echoer"][0], "-"}
		}, nil},
		{"a third-party in-process entry with no statement anywhere", func(fx *gateFix) {
			fx.dist.plugins["acme/echoer"] = [2]string{fx.dist.plugins["acme/echoer"][0], "-"}
			fx.art("acme/echoer", func(a *Artifact) { a.Record.Attestation = nil })
		}, []string{"10 echoer:e2 attestation stop=false"}},
		{"an in-process artifact that pins another module than the binary runs", func(fx *gateFix) {
			fx.in.Tie["acme/echoer"] = Tie{Module: GoModule{Path: testModule, Version: "v0.2.0", Sum: sumA}}
		}, []string{"10 echoer:e2 attestation stop=false"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := newGateFix()
			tc.edit(fx)
			g := fx.run(t)
			got, want := slices.Sorted(slices.Values(gateLines(g))), slices.Sorted(slices.Values(tc.want))
			if !equalLines(got, want) || len(openLines(g)) > 0 {
				t.Fatalf("failures\n got %q\nwant %q\nopen %q", got, want, openLines(g))
			}
			for _, f := range slices.Concat(g.Slots[CheckTrust-1].Fails, g.Slots[CheckAttestation-1].Fails) {
				if !strings.Contains(f.Reason, "row ") {
					t.Errorf("reason %q names no row", f.Reason)
				}
			}
		})
	}
}

// Check 5 denies an artifact with no module whose id is under a reserved owner unless its
// signer is the release key the binary carries (ADR-048 decision 14). trust.yaml lists no
// signer for aiops or contrib, so such a pack is still denied by the signer clause until
// the release key signs contrib packs at S6a (ADR-020 decision 17).
func TestGateReservedOwners(t *testing.T) {
	for _, tc := range []struct {
		id, release string
		reserved    bool
	}{
		{"aiops/pack", "", true},
		{"contrib/pack", "another key", true},
		{"aiops/pack", keyP256ID, false},
		{"acme/pack", "", false},
	} {
		fx := newGateFix()
		fx.reserve(tc.id)
		fx.in.ReleaseKey = tc.release
		f, denied := fx.run(t).Denial("pack:e1")
		if got := strings.Contains(f.Reason, "ADR-048 decision 14"); got != tc.reserved || denied != (tc.id != "acme/pack") || denied && f.Check != CheckTrust {
			t.Errorf("%s with the release key %q: denied %v by %+v", tc.id, tc.release, denied, f)
		}
	}
}

// A group clash leaves the artifact out, names it, the group's id and check 8, and keeps
// the row composed before it (ADR-047 decision 6). The artifact is a row of the result,
// so a live generation lists it among the denied rows.
func TestGateGroupClashIsNamed(t *testing.T) {
	fx := newGateFix()
	fx.bundles["b"] = rows("id: pack, plugin: o/plain")
	g := fx.run(t)
	want := `warning: the installed artifact acme/pack is left out: gate check 8 (Layer rules), rule group-clash: the group "pack" of the installed artifact acme/pack takes the id of a row composed before it or of the loader's row, so none of its rows is inserted (ADR-047 decision 6)`
	if d := (Admission{Gate: g}).Diagnostics(); !slices.Contains(d, want) || g.Result(CheckLayerRules, "acme/pack").Status != StatusFail ||
		g.Result(CheckLayerRules, "pack").Status != StatusPass {
		t.Fatalf("%q", d)
	}
}

// signedEchoer makes acme/echoer listen on toy/signed, a hook of the signed class.
func (fx *gateFix) signedEchoer() {
	m := manifestText("acme/echoer", echoRegisters, "hooks: [{hook: toy/signed, stage: default}]",
		`requires: {pluginApi: "0.1", definitions: {echoers: ["0.1"], toy: ["0.2"]}}`)
	defs := `{"echoers": {"version": "0.1", "suite": 1, "halves": ["obligations", "guard"]}, "toy": {"version": "0.2"}}`
	fx.dist.add(m, defs)
	fx.art("acme/echoer", func(a *Artifact) {
		a.Kept.Manifest, a.Record.Attestation = []byte(m), []byte(statementOf("acme/echoer@0.1.0", hexB, defs, ""))
	})
}

// Each entry point evaluates the clauses its inputs decide and reports each other as
// not evaluated, with the reason (ADR-060 decision 17): the whole check, a clause of
// it, or, for the signed clause where no signer is recorded, the row it would judge
// (ADR-020 decision 30, reading 3). Checks 3 and 9 are S2b everywhere. An entry point
// given no module tie reports the tie clauses of checks 5 and 10 as not evaluated.
func TestGateEntryPoints(t *testing.T) {
	for _, tc := range []struct {
		entry   Entry
		noTie   bool
		skipped []int    // checks not evaluated at all, besides 3 and 9
		clauses []string // "<check> <clause>" not evaluated
		open    []string
	}{
		{EntryBuild, true, []int{4, 5, 8, 10}, []string{"7 signed"}, []string{"7 user signed-class open"}},
		{EntryCheck, false, []int{4, 6}, []string{"5 module", "5 signer", "7 signed"},
			[]string{"5 echoer:e2 signer open", "5 pack:e1 signer open", "7 user signed-class open"}},
		{EntryOffline, false, []int{4, 5, 6}, []string{"7 signed"}, []string{"7 user signed-class open"}},
		{EntryInstall, false, []int{6}, []string{"5 module"}, nil},
		{EntryInstallUsed, false, nil, nil, nil},
		{EntryInstallUsed, true, nil, []string{"5 tie", "10 tie"}, nil},
		{EntryBoot, false, nil, nil, nil},
		{EntryRender, true, nil, []string{"5 tie", "10 tie"}, nil},
	} {
		t.Run(string(tc.entry), func(t *testing.T) {
			fx := newGateFix()
			fx.dist.add(manifestText("o/user", "uses: [{key: toy.secret, major: 0, as: need}]", `requires: {pluginApi: "0.1", definitions: {toy: ["0.1"]}}`),
				`{"toy": {"version": "0.1"}}`)
			fx.dist.bindings = "{consumers: {toy.secret: [o/user]}}"
			fx.in.Entry = tc.entry
			if tc.noTie {
				fx.in.Tie = nil
			}
			g := fx.run(t)
			var clauses []string
			for _, s := range g.Slots {
				skipped := s.N == CheckRemoteProto || s.N == CheckRemoteScope || slices.Contains(tc.skipped, s.N)
				if s.Evaluated == skipped || !s.Evaluated && s.Until == "" && s.Reason == "" {
					t.Errorf("check %d: evaluated %v, reason %q", s.N, s.Evaluated, s.Reason)
				}
				for _, c := range s.Clauses {
					name, why, _ := strings.Cut(c, ": ")
					clauses = append(clauses, fmt.Sprintf("%d %s", s.N, name))
					if why == "" {
						t.Errorf("clause %q has no reason", c)
					}
				}
			}
			if !equalLines(clauses, tc.clauses) || !equalLines(openLines(g), tc.open) {
				t.Fatalf("clauses %q, open %q", clauses, openLines(g))
			}
			if len(tc.open) > 0 {
				if p := partial(g.Slots[CheckKeyHookClass-1]); !strings.Contains(p, "1 row results not evaluated") {
					t.Errorf("dump of check 7: %q", p)
				}
				if r := g.Result(CheckKeyHookClass, "user"); r.Status != StatusNotEvaluated || !strings.Contains(r.Reason, "reading 3") {
					t.Errorf("Result(7, user) = %+v", r)
				}
			}
			if r := g.Result(CheckAttestation, "plain"); slices.Contains(tc.skipped, CheckAttestation) != (r.Status == StatusNotEvaluated) || g.Entry != tc.entry {
				t.Errorf("Result(10, plain) = %+v at %s", r, g.Entry)
			}
		})
	}
}

// Install, plugin check and plugin gate --against compose the artifact's rows disabled,
// and boot holds them disabled until plugin enable: the gate evaluates the artifact on
// every row its bundle inserted, enabled or not (ADR-060 decision 17). A disabled row
// that no artifact inserted runs no code, and only check 8, which runs on every row,
// evaluates it; Result says so for the others.
func TestGateEvaluatesDisabledArtifactRows(t *testing.T) {
	for _, tc := range []struct {
		entry Entry
		want  []string
	}{
		{EntryInstall, []string{"2 pack:e1 unserved-definition stop=false", "5 pack:e1 signer stop=false", "10 pack:e1 attestation stop=false"}},
		{EntryOffline, []string{"2 pack:e1 unserved-definition stop=false", "10 pack:e1 attestation stop=false"}},
		{EntryBoot, []string{"2 pack:e1 unserved-definition stop=false", "5 pack:e1 signer stop=false", "10 pack:e1 attestation stop=false"}},
	} {
		t.Run(string(tc.entry), func(t *testing.T) {
			fx := newGateFix()
			fx.in.Entry, fx.disabled = tc.entry, true
			fx.bundles["b"] += "    - {id: off, plugin: o/plain, enabled: false}\n"
			fx.art("acme/pack", func(a *Artifact) {
				a.Kept.Manifest = []byte(strings.Replace(packManifestText("acme/pack"), `["0.1"]`, `["0.9"]`, 1))
				a.Record.Signer, a.Record.Attestation = Unsigned, nil
			})
			g := fx.run(t)
			if got := gateLines(g); !equalLines(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(tc.want))) || g.Pass {
				t.Fatalf("failures %q, pass %v", got, g.Pass)
			}
			if r := g.Result(CheckAttestation, "pack:e1"); r.Status != StatusFail {
				t.Errorf("Result(10, pack:e1) = %+v", r)
			}
			if r := g.Result(CheckManifest, "off"); r.Status != StatusNotEvaluated || !strings.Contains(r.Reason, "disabled") {
				t.Errorf("Result(1, off) = %+v", r)
			}
			if r := g.Result(CheckLayerRules, "off"); r.Status != StatusPass {
				t.Errorf("Result(8, off) = %+v", r)
			}
		})
	}
}

// Install of in-process code before the rebuild evaluates the artifact: its rows are
// judged on its own manifest, check 7 included, and the module clause of check 5 is not
// evaluated, so a valid artifact passes and install answers rebuild-required (ADR-060
// decision 17). After dist use, and at boot, a plugin the module table does not hold
// fails that clause.
func TestGateInstallBeforeTheRebuild(t *testing.T) {
	secret := func(fx *gateFix) {
		m := manifestText("acme/echoer", echoRegisters, "uses: [{key: toy.secret, major: 0, as: need}]",
			`requires: {pluginApi: "0.1", definitions: {echoers: ["0.1"], toy: ["0.2"]}}`)
		fx.dist.bindings = "{consumers: {toy.secret: [o/user]}}"
		fx.art("acme/echoer", func(a *Artifact) {
			a.Kept.Manifest = []byte(m)
			a.Record.Attestation = []byte(statementOf("acme/echoer@0.1.0", hexB,
				`{"echoers": {"version": "0.1", "suite": 1, "halves": ["obligations", "guard"]}, "toy": {"version": "0.2"}}`, ""))
		})
	}
	for _, tc := range []struct {
		name  string
		entry Entry
		edit  func(fx *gateFix)
		want  []string
		open  []string
	}{
		{"a valid artifact", EntryInstall, nil, nil, []string{"5 echoer:e2 module open"}},
		{"an artifact outside a consumer binding", EntryInstall, secret,
			[]string{"7 echoer:e2 consumer-binding stop=false", "7 echoer:e2 signed-class stop=false"}, []string{"5 echoer:e2 module open"}},
		{"after dist use", EntryInstallUsed, nil, []string{"5 echoer:e2 module stop=false"}, nil},
		{"at boot", EntryBoot, nil, []string{"5 echoer:e2 module stop=false"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := newGateFix()
			fx.in.Entry, fx.disabled, fx.unbuilt = tc.entry, true, "acme/echoer"
			delete(fx.dist.plugins, fx.unbuilt)
			if tc.edit != nil {
				tc.edit(fx)
			}
			g := fx.run(t)
			got := slices.Sorted(slices.Values(gateLines(g)))
			if !equalLines(got, tc.want) || !equalLines(openLines(g), tc.open) || g.Pass != (len(tc.want) == 0) {
				t.Fatalf("failures %q, open %q, pass %v", got, openLines(g), g.Pass)
			}
			if r := g.Result(CheckTrust, "echoer:e2"); tc.open != nil && (r.Status != StatusNotEvaluated || !strings.Contains(r.Reason, "rebuild-required")) {
				t.Errorf("Result(5, echoer:e2) = %+v", r)
			}
		})
	}
}

// A row of an installed in-process plugin that no artifact bundle inserted, such as one
// an operator layer adds, is judged on the record that install made for its plugin id
// as a row the bundle inserted is: check 5 on its signer and check 10 on the module tie
// (ADR-020 decision 12).
func TestGateJudgesARowWithNoOriginOnItsPluginsArtifact(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(fx *gateFix)
		want []string
	}{
		{"clean", func(*gateFix) {}, nil},
		{"another module than the binary runs", func(fx *gateFix) {
			fx.in.Tie["acme/echoer"] = Tie{Module: GoModule{Path: testModule, Version: "v0.2.0", Sum: sumA}}
		}, []string{"10 direct attestation stop=false", "10 echoer:e2 attestation stop=false"}},
		{"a signer outside trust.yaml", func(fx *gateFix) { fx.trust.Signers[0].IDs = []string{"beta/*"} },
			[]string{"5 direct signer stop=false", "5 echoer:e2 signer stop=false", "5 pack:e1 signer stop=false"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := newGateFix()
			fx.bundles["b"] += "    - {id: direct, plugin: acme/echoer}\n"
			tc.edit(fx)
			got := slices.Sorted(slices.Values(gateLines(fx.run(t))))
			if !equalLines(got, slices.Sorted(slices.Values(tc.want))) {
				t.Fatalf("failures %q", got)
			}
		})
	}
}

func TestLicensed(t *testing.T) {
	for e, want := range map[string]bool{
		"MIT": true, "Apache-2.0 AND MIT": true, "GPL-2.0-only": false, "GPL-2.0-or-later OR BSD-3-Clause": true,
		"(MIT OR GPL-2.0-only) AND ISC": true, "MIT AND (GPL-2.0-only OR LGPL-2.1-only)": false, "GPL-2.0+": false,
		"": false, "MIT AND": false, "LicenseRef-x": false,
	} {
		if got := licensed(e, nil); got != want {
			t.Errorf("licensed(%q) = %v", e, got)
		}
	}
	if !licensed("LicenseRef-x OR GPL-3.0-only", []string{"LicenseRef-x"}) {
		t.Error("an accepted id is refused")
	}
}

// The distribution's bindings are the release's with each entry a site added.
func TestDistributionBindingsMergeTheSite(t *testing.T) {
	d := Distribution{
		Release: DistManifest{Providers: []Binding{{Key: "a", Plugins: []string{"o/x"}}}, SealedHooks: []HookListeners{{Hook: "h", Plugins: []string{"o/x"}}},
			RequiredKeys: []string{"a"}, RequiredRows: []string{"r"}},
		Site: DistManifest{Providers: []Binding{{Key: "a", Plugins: []string{"o/y"}}, {Key: "b", Plugins: []string{"o/y"}}},
			Consumers: []Binding{{Key: "c", Plugins: []string{"o/y"}}}, SealedHooks: []HookListeners{{Hook: "h", Plugins: []string{"o/y"}}},
			RequiredKeys: []string{"b"}},
	}
	got := d.Bindings()
	want := DistManifest{Providers: []Binding{{Key: "a", Plugins: []string{"o/x", "o/y"}}, {Key: "b", Plugins: []string{"o/y"}}},
		Consumers: []Binding{{Key: "c", Plugins: []string{"o/y"}}}, SealedHooks: []HookListeners{{Hook: "h", Plugins: []string{"o/x", "o/y"}}},
		RequiredKeys: []string{"a", "b"}, RequiredRows: []string{"r"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}
