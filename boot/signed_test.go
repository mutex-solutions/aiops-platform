package boot

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"maps"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"testing/fstest"
)

// The signed distribution manifests of boot's tests, and homes that trust them as the
// harness of ADR-060 decision 28 makes them: a throwaway key listed under
// distributions, and the record of the manifest's digest with that key, as dist use
// writes it (ADR-020 decision 10). Nothing is signed: boot verifies no signature.

// testModule is the main module of the binary that boot's tests describe.
const testModule = "example.test"

// manifestText writes a plugin.yaml of schema 1.0 for id: in-process code at 0.1.0,
// stability ga, that requires the author API 0.1 and no definition. Each of set is
// "<field>: <value>" and replaces that field; a value of "-" drops the field.
func manifestText(id string, set ...string) string {
	owner, _, _ := strings.Cut(id, "/")
	fields := []string{"manifestVersion: \"1.0\"", "id: " + id, "version: 0.1.0", "title: t", "license: Apache-2.0",
		"publisher: {id: " + owner + "}", "stability: ga", "scope: [process]", "runtimes: [inproc]", "uses: []", "provides: []",
		"registers: []", "hooks: []", "configSchema: {}", "durable: []", "requires: {pluginApi: \"0.1\", definitions: {}}"}
	for _, s := range set {
		k, v, _ := strings.Cut(s, ": ")
		i := slices.IndexFunc(fields, func(f string) bool { return strings.HasPrefix(f, k+": ") })
		switch {
		case i < 0:
			fields = append(fields, s)
		case v == "-":
			fields = slices.Delete(fields, i, i+1)
		default:
			fields[i] = s
		}
	}
	return strings.Join(fields, "\n") + "\n"
}

// entryManifest writes the manifest that a module entry of the run tests declares.
func entryManifest(e ModuleEntry) string {
	var uses, provides, hooks []string
	for _, k := range e.Needs {
		uses = append(uses, fmt.Sprintf("{key: %s, major: 0, as: need}", k))
	}
	for _, k := range e.Wants {
		uses = append(uses, fmt.Sprintf("{key: %s, major: 0, as: want}", k))
	}
	for _, k := range e.Provides {
		provides = append(provides, fmt.Sprintf("{key: %s, major: 0}", k))
	}
	for _, h := range e.Hooks {
		hooks = append(hooks, fmt.Sprintf("{hook: %s, stage: %s}", h.Hook, cmpOr(h.StageName, "default")))
	}
	slices.Sort(uses)
	slices.Sort(provides)
	slices.Sort(hooks)
	return manifestText(e.ID, "uses: ["+strings.Join(uses, ", ")+"]", "provides: ["+strings.Join(provides, ", ")+"]",
		"hooks: ["+strings.Join(hooks, ", ")+"]")
}

func cmpOr(a, b string) string {
	if a == "" {
		return b
	}
	return a
}

// attest writes the conformance attestation of a manifest: its subject, and
// definitions as JSON (nil: {}).
func attest(manifest string, definitions string) string {
	m, err := ReadManifest("t", []byte(manifest), ManifestLenient)
	if err != nil {
		panic(err)
	}
	if definitions == "" {
		definitions = "{}"
	}
	return fmt.Sprintf(`{"_type": %q, "subject": [{"name": "%s@%s", "digest": {"sha256": %q}}], "predicateType": %q, "predicate": {"definitions": %s}}`,
		statementType, m.ID, m.Version, strings.TrimPrefix(digestOf([]byte(manifest)), "sha256:"), AttestationType, definitions)
}

// testDist is a signed distribution manifest as text. Plugins maps an id to its
// manifest and statement; a statement of "-" leaves it out. Each other field is the
// YAML text of the field.
type testDist struct {
	plugins                                   map[string][2]string
	modules, packages, bindings, site, accept string
	definitions                               string // serves.definitions
	pluginAPI                                 string
	packageOf                                 func(id string) string
}

func newTestDist() *testDist {
	return &testDist{plugins: map[string][2]string{}, modules: "[{path: " + testModule + ", licence: Apache-2.0}]", packages: "[]",
		bindings: "{}", accept: "[]", definitions: "{}", pluginAPI: "0.1", packageOf: func(id string) string { return testModule + "/" + id }}
}

// add adds a plugin with its manifest, attested with no definition but those named.
func (d *testDist) add(manifest, definitions string) *testDist {
	m, err := ReadManifest("t", []byte(manifest), ManifestLenient)
	if err != nil {
		panic(err)
	}
	d.plugins[m.ID] = [2]string{manifest, attest(manifest, definitions)}
	return d
}

func (d *testDist) text() []byte {
	var b strings.Builder
	b.WriteString("distVersion: 1\nplugins:")
	if len(d.plugins) == 0 {
		b.WriteString(" {}")
	}
	b.WriteString("\n")
	for _, id := range slices.Sorted(maps.Keys(d.plugins)) {
		p := d.plugins[id]
		fmt.Fprintf(&b, "  %s:\n    package: %s\n    manifest: %s\n", id, d.packageOf(id), strconv.Quote(p[0]))
		if p[1] != "-" {
			fmt.Fprintf(&b, "    attestation: %s\n", strconv.Quote(p[1]))
		}
	}
	fmt.Fprintf(&b, "modules: %s\npackages: %s\npins: []\nbindings: %s\nacceptLicenses: %s\n", d.modules, d.packages, d.bindings, d.accept)
	if d.site != "" {
		fmt.Fprintf(&b, "site: %s\n", d.site)
	}
	fmt.Fprintf(&b, "serves: {pluginApi: %q, kernel: {keys: {}, hooks: {}}, definitions: %s}\n", d.pluginAPI, d.definitions)
	return []byte(b.String())
}

// bindingsText writes the bindings of a DistManifest as the signed manifest holds them.
func bindingsText(d DistManifest) string {
	m := func(bs []Binding) string {
		var out []string
		for _, b := range bs {
			out = append(out, b.Key+": ["+strings.Join(b.Plugins, ", ")+"]")
		}
		return "{" + strings.Join(out, ", ") + "}"
	}
	var hooks []Binding
	for _, h := range d.SealedHooks {
		hooks = append(hooks, Binding{Key: h.Hook, Plugins: h.Plugins})
	}
	return fmt.Sprintf("{providers: %s, consumers: %s, sealedHooks: %s, required: {keys: [%s], rows: [%s]}}", m(d.Providers), m(d.Consumers), m(hooks),
		strings.Join(d.RequiredKeys, ", "), strings.Join(d.RequiredRows, ", "))
}

// signedFor writes the signed manifest of a module table of the run tests and its
// bindings: every plugin attested with no definition.
func signedFor(mods ModuleTable, dist DistManifest) []byte {
	d := newTestDist()
	d.bindings = bindingsText(dist)
	for _, e := range mods {
		d.add(entryManifest(e), "")
	}
	return d.text()
}

// testKey returns a throwaway public key as PEM and its key id.
func testKey() (string, string) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(der)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), base64.StdEncoding.EncodeToString(sum[:])
}

// trustedHome returns the files by which a home trusts the manifest: trust.yaml that
// lists a throwaway key under distributions, the record of the manifest's digest with
// that key as signer, and verified/current. It returns the key id too.
func trustedHome(manifest []byte) (fstest.MapFS, string) {
	pub, id := testKey()
	digest := digestOf(manifest)
	dir := "verified/" + strings.Replace(digest, ":", "-", 1)
	return fstest.MapFS{
		"trust.yaml":         {Data: []byte("distributions:\n  - key: |\n" + indent(pub, 6) + "\n")},
		dir + "/record.yaml": {Data: []byte(fmt.Sprintf("kind: distribution\ndigest: %s\nsigner: %s\n", digest, id))},
		"verified/current":   {Data: []byte(digest + "\n")},
	}, id
}

// testBuildInfo is the build information of a binary built inside its repository:
// the main module, with no version and no sum.
func testBuildInfo() *debug.BuildInfo {
	return &debug.BuildInfo{Main: debug.Module{Path: testModule, Version: "(devel)"}}
}

// sign gives a run of the fixture what boot reads to trust its module table: the
// signed manifest of its registry and bindings, the build information, and a home
// that trusts the manifest.
func (f *fixture) sign() {
	f.opt.Registry, f.opt.BuildInfo = f.reg, testBuildInfo()
	f.opt.Signed = signedFor(f.reg.Modules, f.dist)
	home, _ := trustedHome(f.opt.Signed)
	maps.Copy(f.files, home)
}
