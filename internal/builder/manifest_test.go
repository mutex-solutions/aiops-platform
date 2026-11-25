// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/internal/plugingen"
	testdist "github.com/mutex-solutions/aiops-platform/test/dist"
)

// The signed distribution manifest of the test binary is the builder's (ADR-060
// decisions 20 and 28), and boot reads it: its module table is the binary's, each
// plugin with its manifest byte for byte and a statement whose subject is that
// manifest; a test plugin names its test definition at suite revision 0, with no half
// (ADR-060 decision 55); every module of the repository is listed by its path with no
// version and no sum (ADR-020 decision 28), under the licence the scan detects; and the
// pins hold the toolchain module with its sum (ADR-044 decision 16).
func TestDistributionOfTheTestBinary(t *testing.T) {
	root := filepath.Join("..", "..")
	input, err := os.ReadFile(filepath.Join(root, "test", "dist", "dist.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	out, err := Distribution(ctx, Go{Dir: root}, "./test/dist/cmd/aiops-test", input)
	if err != nil || !bytes.Equal(out, testdist.Signed) {
		t.Fatalf("%v: test/dist/distribution.yaml is not what the builder writes", err)
	}
	d, err := boot.ReadDistribution("distribution.yaml", out)
	if err != nil {
		t.Fatal(err)
	}
	mods := testdist.Modules()
	if len(d.Plugins) != len(mods) {
		t.Fatalf("%d plugins in the manifest, %d in the module table", len(d.Plugins), len(mods))
	}
	for _, m := range mods {
		p := d.Plugins[m.Plugin.ID]
		st, err := boot.ReadAttestation(m.Plugin.ID, p.Attestation)
		if err != nil || !bytes.Equal(p.Manifest, m.Manifest) || st.Digest != fmt.Sprintf("sha256:%x", sha256.Sum256(p.Manifest)) || !strings.Contains(p.Package, "/") {
			t.Fatalf("%s: %v, statement %+v", m.Plugin.ID, err, st)
		}
	}
	st, err := boot.ReadAttestation("aiops-test/toy-provider", d.Plugins["aiops-test/toy-provider"].Attestation)
	if toy := st.Definitions["toy"]; err != nil || toy.Suite == nil || *toy.Suite != 0 || toy.Halves != nil {
		t.Errorf("toy-provider attests %+v (%v)", toy, err)
	}
	// The statement of a first-party plugin names exactly the suites and the halves that
	// ran on its package (ADR-060 decisions 18 and 50): the guard half on a dispatcher,
	// the obligations half on a contribution, both halves of a provider's suite.
	for id, want := range map[string]map[string][]string{
		"aiops/example-echoer":   {"echoers": {"obligations"}},
		"aiops/example-dispatch": {"echoers": {"guard"}, "stampers": {"guard"}},
		"aiops/cli":              {"commands": {"guard"}},
		"aiops/command-plugin":   {"commands": {"obligations"}},
		"aiops/clock":            {"clock": {"obligations", "guard"}},
		"aiops/readiness":        {},
	} {
		st, err := boot.ReadAttestation(id, d.Plugins[id].Attestation)
		got := map[string][]string{}
		for name, a := range st.Definitions {
			if a.Suite != nil {
				got[name] = a.Halves
			}
		}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s attests the halves %v, and the suites that ran on it %v (%v)", id, got, want, err)
		}
	}
	for _, m := range d.Modules {
		if strings.HasPrefix(m.Path, rootModule) && (m.Version != "" || m.Sum != "" || m.Licence != "Apache-2.0") {
			t.Errorf("module %+v", m)
		}
	}
	if p := pinOf(d.Pins, Toolchain, ToolchainVersion); p.Sum != ToolchainSum {
		t.Errorf("the toolchain pin %+v", p)
	}
	for _, bad := range []struct{ input, main string }{{"bogus: 1", "./test/dist/cmd/aiops-test"}, {string(input), "./no/such/package"}} {
		if _, err := Distribution(ctx, Go{Dir: root}, bad.main, []byte(bad.input)); err == nil {
			t.Errorf("Distribution took %q", bad)
		}
	}
}

// A first-party plugin whose test fails has no statement: the build that would attest it
// fails with the test's lines (ADR-060 decision 50).
func TestASuiteThatFailsAttestsNothing(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("testdata", "suites", "fails"))
	if err != nil {
		t.Fatal(err)
	}
	pkg := Package{ImportPath: rootModule + "/internal/builder/testdata/suites/fails", Dir: dir, Module: Module{Path: rootModule}}
	if ran, err := suitesRan(t.Context(), Go{Dir: filepath.Join("..", "..")}, io.Discard, "", []Package{pkg}); err == nil || !strings.Contains(err.Error(), "a suite that fails") {
		t.Errorf("suitesRan = %v, %v; want the failure of the test", ran, err)
	}
}

// What a site build adds, boot reads: the site's entries apart from the release's
// bindings, a raised pin beside the release's version, a hash checked against nothing,
// and the accepted licences (ADR-044 decisions 8, 10 and 13; ADR-060 decision 20).
func TestManifestOfASite(t *testing.T) {
	m := Module{Path: "example.test/site", Version: "v1.0.0", Sum: h1("site"), Dir: moduleDir(t, map[string]string{"x/x.go": "package x\n"})}
	release := boot.DistManifest{Consumers: []boot.Binding{{Key: "operations", Plugins: []string{"aiops/command-dist"}}}, RequiredKeys: []string{"clock"}}
	site := boot.DistManifest{Providers: []boot.Binding{{Key: "echoers", Plugins: []string{"acme/echo"}}},
		SealedHooks: []boot.HookListeners{{Hook: "internal/status", Plugins: []string{"acme/echo"}}}, RequiredRows: []string{"echo"}}
	pins := []boot.DistPin{{Path: Toolchain, Version: ToolchainVersion, Sum: ToolchainSum}, {Path: m.Path, Version: "v1.0.0", Sum: m.Sum, Release: "v0.9.0"},
		{Path: "example.test/dep", Version: "v0.1.0", GoMod: h1("dep"), Unchecked: true}}
	out, err := Manifest{Graph: []Package{pkg(m, "x")}, Licences: map[string]string{m.Path: "MPL-2.0"}, Pins: pins, Release: release, Site: site,
		Accept: []string{"MPL-2.0"}}.Write()
	if err != nil {
		t.Fatal(err)
	}
	d, err := boot.ReadDistribution("distribution.yaml", out)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(d.Site.Providers) != 1 || len(d.Site.SealedHooks) != 1 || d.Site.RequiredRows[0] != "echo" || len(d.Release.Providers) != 0 || d.Release.RequiredKeys[0] != "clock" {
		t.Errorf("bindings %+v, site %+v", d.Release, d.Site)
	}
	if p := pinOf(d.Pins, m.Path, "v1.0.0"); p.Release != "v0.9.0" || !pinOf(d.Pins, "example.test/dep", "v0.1.0").Unchecked || len(d.AcceptLicenses) != 1 {
		t.Errorf("pins %+v, accepted %v", d.Pins, d.AcceptLicenses)
	}
	if _, err := (Manifest{Graph: []Package{pkg(m, "x")}}).Write(); err == nil || !strings.Contains(err.Error(), "no detected licence") {
		t.Errorf("a module with no licence: %v", err)
	}
}

// The owner check runs on what the manifest records (ADR-060 decision 22; 08:402): a
// package of a module outside the release that holds a plugin.yaml under aiops/ or
// contrib/ is refused wherever it lies, under testdata/ too, which go builds once a
// package imports it; and two packages that declare one id are refused, not one entry
// kept. A server graph that links a package under testdata/, a fake package or a
// package of sdk/testkit is refused too (ADR-060 decision 31).
func TestManifestOwners(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "plugins", "clock", "plugin.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	acme := strings.NewReplacer("id: aiops/clock", "id: acme/clock", "publisher: {id: aiops}", "publisher: {id: acme}").Replace(string(src))
	sneak := Module{Path: "example.test/sneak", Version: "v0.1.0", Sum: h1("sneak"), Dir: moduleDir(t, map[string]string{
		"plugin.go": "package sneak\n", "plugin.yaml": acme, "internal/testdata/reg/plugin.yaml": string(src), "internal/testdata/reg/reg.go": "package reg\n"})}
	other := Module{Path: "example.test/other", Version: "v0.1.0", Sum: h1("other"), Dir: moduleDir(t, map[string]string{"plugin.go": "package other\n", "plugin.yaml": acme})}
	licences := map[string]string{sneak.Path: "Apache-2.0", other.Path: "Apache-2.0"}
	root := Package{ImportPath: sneak.Path, Dir: sneak.Dir, Module: sneak}
	if _, err := (Manifest{Graph: []Package{root, pkg(sneak, "internal/testdata/reg")}, Licences: licences}).Write(); err == nil ||
		!strings.Contains(err.Error(), "example.test/sneak/internal/testdata/reg declares the id aiops/clock") {
		t.Errorf("an aiops/ id outside the release, under testdata/: %v", err)
	}
	contrib := strings.NewReplacer("id: aiops/clock", "id: contrib/clock", "publisher: {id: aiops}", "publisher: {id: contrib}").Replace(string(src))
	third := Module{Path: "example.test/third", Version: "v0.1.0", Sum: h1("third"), Dir: moduleDir(t, map[string]string{"plugin.go": "package third\n", "plugin.yaml": contrib})}
	if _, err := (Manifest{Graph: []Package{{ImportPath: third.Path, Dir: third.Dir, Module: third}}, Licences: map[string]string{third.Path: "Apache-2.0"}}).Write(); err == nil ||
		!strings.Contains(err.Error(), "example.test/third declares the id contrib/clock: the owner contrib is reserved") {
		t.Errorf("a contrib/ id outside the release: %v", err)
	}
	if _, err := (Manifest{Graph: []Package{root, {ImportPath: other.Path, Dir: other.Dir, Module: other}}, Licences: licences}).Write(); err == nil ||
		!strings.Contains(err.Error(), "acme/clock is declared by example.test/sneak and by example.test/other") {
		t.Errorf("one id declared twice: %v", err)
	}
	if err := fakes([]Package{pkg(sneak, "internal/testdata/reg")}); err == nil || !strings.Contains(err.Error(), "under testdata/") {
		t.Errorf("a package under testdata/ in the server graph: %v", err)
	}
	testkit := Module{Path: rootModule + "/sdk/testkit", Version: "v0.2.0"}
	for _, p := range []Package{pkg(sneak, "internal/fake"), {ImportPath: testkit.Path + "/kerneltest", Module: testkit}} {
		if err := fakes([]Package{p}); err == nil || !strings.Contains(err.Error(), "the server links "+p.ImportPath+", a fake or sdk/testkit package") {
			t.Errorf("%s in the server graph: %v", p.ImportPath, err)
		}
	}
}

// No binary of the repository links golang.org/x/net/trace, whose init registers
// /debug/requests and /debug/events on http.DefaultServeMux: gRPC reaches every binary
// through the signing library, and each build has the tag grpcnotrace (ADR-020
// decisions 6 and 27; ADR-044 decision 21). The graphs of cmd/aiops and cmd/aiopsctl
// hold no such package; make check and CI build with the tag in GOFLAGS; and the
// builder refuses a graph that holds it, so no signed manifest records one.
func TestReleaseBuildsLinkNoTrace(t *testing.T) {
	root, ctx := filepath.Join("..", ".."), context.Background()
	for _, main := range []string{"./cmd/aiops", "./cmd/aiopsctl"} {
		if _, err := Graph(ctx, Go{Dir: root}, main); err != nil {
			t.Errorf("%s: %v", main, err)
		}
	}
	tags := regexp.MustCompile(`GOFLAGS\s*(\?=|:)\s*.*-tags=grpcnotrace`)
	for _, f := range []string{"Makefile", ".github/workflows/ci.yml"} {
		b, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatal(err)
		}
		if !tags.Match(b) {
			t.Errorf("%s sets no GOFLAGS with -tags=grpcnotrace", f)
		}
	}
	defer func(f []string) { plugingen.BuildFlags = f }(plugingen.BuildFlags)
	plugingen.BuildFlags = nil
	if _, err := Graph(ctx, Go{Dir: root, Env: append(os.Environ(), "GOFLAGS=-mod=readonly")}, "./cmd/aiops"); err == nil || !strings.Contains(err.Error(), "grpcnotrace") {
		t.Errorf("a graph with %s: %v, want a refusal that names the tag", tracePackage, err)
	}
}

// h1 is a sum of the h1 form for the text s.
func h1(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "h1:" + base64.StdEncoding.EncodeToString(sum[:])
}
