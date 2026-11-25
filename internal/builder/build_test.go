// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mutex-solutions/aiops-platform/boot"
)

// The builder refuses what the local path cannot take before it reads a module (ADR-044
// decision 7; ADR-060 decisions 20 and 21): a GOPROXY that names a proxy URL, in any
// element of its list, since every module source of the builder is a file:// directory
// until L14 clears, where a file:// source, direct and off pass that check; and a
// release input that lists no module, since a release names its module table.
func TestRequestRefusals(t *testing.T) {
	dir := t.TempDir()
	q := Request{ModCache: dir, Cache: dir, Out: filepath.Join(dir, "out"), Input: []byte("requiredKeys: [clock]\n"), Root: "v0.2.0"}
	for _, proxy := range []string{"https://proxy.golang.org", "http://proxy.example.test,direct", "file://" + dir + ",https://proxy.example.test",
		"off|https://proxy.example.test"} {
		q.Proxy = proxy
		if _, err := Build(t.Context(), q); err == nil || !strings.Contains(err.Error(), "GOPROXY names "+proxy+": until L14 clears") {
			t.Errorf("GOPROXY=%s: %v", proxy, err)
		}
	}
	for _, proxy := range []string{"", "file://" + dir, "direct", "off"} {
		q.Proxy = proxy
		if _, err := Build(t.Context(), q); err == nil || strings.Contains(err.Error(), "GOPROXY") || !strings.Contains(err.Error(), "the release input lists no module") {
			t.Errorf("GOPROXY=%q: %v", proxy, err)
		}
	}
}

// The gate of dist build composes a row of each plugin that the build builds and runs
// the clauses of that entry point on it (ADR-060 decisions 17 and 22), here on plugins
// of the test distribution with its signed manifest: check 7 denies the row of
// rogue-provider, which provides a provider-bound key outside its binding, and the module
// table lookup denies a plugin that the manifest does not hold, each denial naming its
// check, while the row of toy-provider passes. Every clause that dist build does not
// evaluate is printed with the reason. The release table on the release's manifest
// passes.
func TestGateAtDistBuild(t *testing.T) {
	read := func(p string) []byte {
		b, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	plugin := func(dir string) tablePlugin {
		return tablePlugin{pkg: rootModule + "/" + dir, manifest: read(dir + "/plugin.yaml")}
	}
	acme := strings.NewReplacer("id: aiops/clock", "id: acme/clock", "publisher: {id: aiops}", "publisher: {id: acme}").Replace(string(read("plugins/clock/plugin.yaml")))
	var progress bytes.Buffer
	b := &build{scratch: t.TempDir(), progress: &progress, plugins: []tablePlugin{plugin("test/dist/plugins/toy-provider"),
		plugin("test/dist/plugins/rogue-provider"), {pkg: "example.test/acme/clock", manifest: []byte(acme)}}}
	err := b.gate(read("test/dist/distribution.yaml"))
	for _, want := range []string{"the gate at dist build denies row aiops-test.rogue-provider by check 7 Key and hook classes (",
		"the gate at dist build denies row acme.clock by the module table lookup, which runs before the ten checks: "} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("the denial lacks %q: %v", want, err)
		}
	}
	if err != nil && strings.Contains(err.Error(), "toy-provider by") {
		t.Errorf("toy-provider is denied: %v", err)
	}
	for _, want := range []string{"gate: check 3 Remote protocol: not evaluated until S2b\n",
		"gate: check 4 Stability: not evaluated at dist build: no profile is composed\n",
		"gate: check 5 Trust: not evaluated at dist build: the manifest it would read is the build's output",
		"gate: check 7 Key and hook classes: not evaluated at dist build: signed: no signer is recorded",
		"gate: check 10 Attestation: not evaluated at dist build: dist.yaml lists {module, version, sum} and no artifact",
		"gate: check 1 Manifest schema and author API: not evaluated until a later step: exemptions"} {
		if !strings.Contains(progress.String(), want) {
			t.Errorf("dist build printed no %q:\n%s", want, progress.String())
		}
	}

	release := read("dist/distribution.yaml")
	d, err := boot.ReadDistribution("distribution.yaml", release)
	if err != nil {
		t.Fatal(err)
	}
	b.plugins = nil
	for _, p := range d.Plugins {
		b.plugins = append(b.plugins, tablePlugin{pkg: p.Package, manifest: p.Manifest})
	}
	if err := b.gate(release); err != nil {
		t.Errorf("the release table: %v", err)
	}
}
