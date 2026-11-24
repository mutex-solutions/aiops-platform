// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
)

// connectorBox is the toolbox of a pack: a release with one instantiable echoer, the
// connector template, and a scenario that fails when failScene is set. Gate denies an
// attestation that names no scenario.
type connectorBox struct {
	box
	sum       launch.ManifestSummary
	failScene bool
	gated     []launch.GateRequest
}

var echoer = launch.ReleasePlugin{ID: "aiops/example-echoer", Example: map[string]any{"name": "loud"},
	Feeds: map[string][]string{"echoers": {"0.1"}}}

func (b *connectorBox) Release(context.Context, string) (launch.Release, error) {
	return launch.Release{Digest: "sha256:ab", Plugins: []launch.ReleasePlugin{echoer}}, nil
}

func (b *connectorBox) Template(_ context.Context, q launch.TemplateRequest) (map[string][]byte, error) {
	if q.Registry != def.Connector {
		return nil, errors.New("no template")
	}
	return map[string][]byte{"plugin.yaml": []byte("id: acme/connector-x\nruntimes: []\n"), "README.md": []byte("a pack\n")}, nil
}

func (b *connectorBox) ReadManifest(context.Context, launch.ManifestRequest) (launch.ManifestSummary, []launch.Diagnostic, error) {
	return b.sum, nil, nil
}

func (b *connectorBox) Vet(context.Context, string) ([]launch.Diagnostic, error) { return nil, nil }

func (b *connectorBox) Suite(_ context.Context, q launch.SuiteRequest) (launch.SuiteReport, error) {
	if q.Definitions["echoers"] != "0.1" {
		return launch.SuiteReport{}, errors.New("the pack's registries are not asked")
	}
	return launch.SuiteReport{Definitions: []launch.SuiteResult{{Name: "echoers", Revision: 1, Halves: []string{"obligations"}}}}, nil
}

func (b *connectorBox) Scenario(_ context.Context, q launch.ScenarioRequest) (launch.ScenarioReport, error) {
	r := launch.ScenarioReport{Name: q.Name, Version: "1", Halves: []string{"kernel"}}
	if b.failScene {
		r.Failures = []string{"echo/reply: the row loud answered nothing"}
	}
	return r, nil
}

func (b *connectorBox) Gate(_ context.Context, q launch.GateRequest) (launch.Verdict, error) {
	b.gated = append(b.gated, q)
	v := launch.Verdict{Entry: q.Entry, Clauses: []launch.Clause{{Check: 5, Clause: "signer", Outcome: launch.OutcomeNotEvaluated, Reason: "no trust.yaml"}}}
	if !strings.Contains(string(q.Attestation), `"scenario":{"name":"echo","version":1`) {
		v.Clauses = append(v.Clauses, launch.Clause{Check: 10, Outcome: launch.OutcomeDeny, Reason: "no scenario version"})
	}
	return v, nil
}

// plugin new connector writes one disabled row per instantiable plugin of the release,
// with the first example as its config, and the requires its rows feed; a release with
// none, or a --plugin it does not serve, is refused and nothing is written (ADR-047
// decision 12).
func TestNewConnector(t *testing.T) {
	b := &connectorBox{}
	l := Library{Box: b}
	dir := filepath.Join(t.TempDir(), "pack")
	w, err := l.New(t.Context(), def.NewRequest{Registry: def.Connector, ID: "acme/connector-x", Dir: dir})
	if err != nil || len(w.Paths) != 3 {
		t.Fatalf("New = %v, %v", w, err)
	}
	bundle, _ := os.ReadFile(filepath.Join(dir, "bundle.yaml"))
	if !strings.Contains(string(bundle), `- {id: example-echoer, plugin: aiops/example-echoer, roles: [api, worker], enabled: false, config: {"name":"loud"}}`) {
		t.Errorf("bundle.yaml:\n%s", bundle)
	}
	man, _ := os.ReadFile(filepath.Join(dir, "plugin.yaml"))
	if !strings.HasSuffix(string(man), packMarker+"\nrequires:\n  definitions:\n    echoers: [\"0.1\"]\n") {
		t.Errorf("plugin.yaml:\n%s", man)
	}
	other := filepath.Join(t.TempDir(), "other")
	if _, err := l.New(t.Context(), def.NewRequest{Registry: def.Connector, ID: "acme/connector-y", Dir: other, Plugins: []string{"aiops/none"}}); !errors.Is(err, def.ErrRefused) {
		t.Errorf("a plugin the release does not serve: %v", err)
	}
	if _, err := os.Stat(other); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused plugin new wrote %s", other)
	}
	if _, _, err := connectorRows(launch.Release{Digest: "sha256:cd"}, nil); !errors.Is(err, def.ErrRefused) || !strings.Contains(err.Error(), "no instantiable plugin") {
		t.Errorf("a release with no instantiable plugin: %v", err)
	}
}

// connector writes the pack of TestNewConnector and returns its directory.
func connector(t *testing.T, b *connectorBox) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "pack")
	if _, err := (Library{Box: b}).New(t.Context(), def.NewRequest{Registry: def.Connector, ID: "acme/connector-x", Dir: dir}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func steps(r def.CheckReport) []string {
	var out []string
	for _, s := range r.Steps {
		out = append(out, s.Outcome)
	}
	return out
}

// A pack runs steps 1, 2, 3, 5, 7 and 8: the suite of each registry its rows feed and
// the echo scenario, which runs before step 7 so that the attestation names it; step 4
// and step 6 do not apply. A failed scenario skips step 7.
func TestCheckPack(t *testing.T) {
	sum := launch.ManifestSummary{ID: "acme/connector-x", Version: "0.1.0", Definitions: map[string][]string{"echoers": {"0.1"}}}
	b := &connectorBox{sum: sum}
	dir := connector(t, b)
	r, err := (Library{Box: b}).Check(t.Context(), def.CheckRequest{Dir: dir})
	want := []string{"pass", "pass", "pass", "not applicable", "pass", "not applicable", "pass", "pass"}
	if err != nil || !slices.Equal(steps(r), want) || !strings.Contains(string(r.Attestation), `"halves":["kernel"]`) {
		t.Fatalf("Check = %v, %s, %v; want %v and an attestation that names the scenario", steps(r), r.Attestation, err, want)
	}
	if ds := r.Steps[6].Diagnostics; len(ds) != 1 || !ds[0].Warning || !strings.Contains(ds[0].Text, "not evaluated") {
		t.Errorf("step 7 prints %+v; want each clause it does not evaluate, as a warning", ds)
	}
	if g := b.gated[0]; g.Entry != "plugin check" || g.Digest == "" || g.Signer == "" || !strings.Contains(string(g.Attestation), strings.TrimPrefix(g.Digest, "sha256:")) {
		t.Errorf("the gate request %+v", g)
	}
	b.failScene = true
	r, _ = (Library{Box: b}).Check(t.Context(), def.CheckRequest{Dir: dir})
	want = []string{"pass", "pass", "pass", "not applicable", "pass", "not applicable", "skipped", "fail"}
	if !slices.Equal(steps(r), want) || r.Attestation != nil || len(r.Steps[7].Diagnostics) != 1 {
		t.Errorf("a failed scenario: %v, %+v", steps(r), r.Steps[7])
	}
}

// Step 2 of a pack compares requires with the registries its rows feed, and with --write
// writes it.
func TestCheckPackParity(t *testing.T) {
	b := &connectorBox{sum: launch.ManifestSummary{ID: "acme/connector-x", Version: "0.1.0"}}
	dir := connector(t, b)
	r, err := (Library{Box: b}).Check(t.Context(), def.CheckRequest{Dir: dir})
	if err != nil || r.Steps[1].Outcome != def.StepFail || !strings.Contains(r.Steps[1].Diagnostics[0].Text, "--write") {
		t.Fatalf("a pack whose requires differs: %v, %+v", err, r.Steps[1])
	}
	file := filepath.Join(dir, "plugin.yaml")
	if err := os.WriteFile(file, []byte("id: acme/connector-x\nruntimes: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err = (Library{Box: b}).Check(t.Context(), def.CheckRequest{Dir: dir, Write: true})
	man, _ := os.ReadFile(file)
	if err != nil || r.Steps[1].Outcome != def.StepPass || !strings.Contains(string(man), "echoers: [\"0.1\"]") {
		t.Errorf("--write: %v, %+v\n%s", err, r.Steps[1], man)
	}
}

// checkBox is packBox with what plugin check needs beside it: a release whose
// instantiable aiops/echoer the rows of the pack feed echoers 0.1 through, and T0 and
// plugingen that find nothing.
type checkBox struct{ *packBox }

func (checkBox) Release(context.Context, string) (launch.Release, error) {
	return launch.Release{Digest: "sha256:ab", Plugins: []launch.ReleasePlugin{{ID: "aiops/echoer", Feeds: map[string][]string{"echoers": {"0.1"}}}}}, nil
}

func (checkBox) Vet(context.Context, string) ([]launch.Diagnostic, error) { return nil, nil }

func (checkBox) Generate(context.Context, launch.GenerateRequest) ([]launch.Diagnostic, error) {
	return nil, nil
}

// Step 7 runs plugin pack into a directory of its own and removes it: the gate of plugin
// check reads the digest of the OCI manifest that plugin pack writes of the same
// directory, the attestation names it, and no layout stays behind (07-c4-plugin-sdk.md:1573;
// ADR-047 decision 1).
func TestCheckStep7Packs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	b := &packBox{sum: packSummary(), suite: &launch.SuiteReport{Definitions: []launch.SuiteResult{{Name: "echoers", Revision: 1, Halves: []string{"obligations"}}}},
		scenario: &launch.ScenarioReport{Name: "echo", Version: "1", Halves: []string{"kernel"}}}
	l := Library{Box: checkBox{b}}
	dir := pluginDir(t, pack)
	r, err := l.Check(t.Context(), def.CheckRequest{Caller: caller(), Dir: dir})
	if err != nil || r.Attestation == nil {
		t.Fatalf("Check = %v, %v", steps(r), err)
	}
	if es, _ := os.ReadDir(tmp); len(es) != 0 {
		t.Errorf("step 7 left %v in the temporary directory", es)
	}
	if _, err := os.Stat(filepath.Join(dir, "artifact")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("step 7 wrote a layout into the plugin directory: %v", err)
	}
	out := filepath.Join(t.TempDir(), "artifact")
	if _, err := l.Pack(t.Context(), def.PackRequest{Caller: caller(), Dir: dir, Out: out}); err != nil {
		t.Fatal(err)
	}
	desc, _, _ := layout(t, out)
	var asked []string
	for _, g := range b.gates {
		asked = append(asked, g.Entry+" "+g.Digest)
	}
	if len(b.gates) != 2 || b.gates[0].Digest != desc.Digest.String() || b.gates[1].Digest != desc.Digest.String() || b.gates[0].Signer == "" {
		t.Fatalf("the gate was asked of %q; want check and pack each at the artifact %s", asked, desc.Digest)
	}
	if !strings.Contains(string(r.Attestation), `"sha256":"`+desc.Digest.Encoded()+`"`) || string(r.Attestation) != string(b.gates[1].Attestation) {
		t.Errorf("the attestation of check %s; want the one pack records, of %s", r.Attestation, desc.Digest)
	}
}

// The attestation that plugin pack records for in-process code names the generated
// tests that did not run in step 4, as the one plugin check would sign does (ADR-060
// decisions 18 and 43; ADR-020 decision 9).
func TestPackOfCodeNamesWhatDidNotRun(t *testing.T) {
	sum := packSummary()
	sum.Runtimes = []string{"inproc"}
	b := &packBox{sum: sum, suite: &launch.SuiteReport{Definitions: []launch.SuiteResult{{Skipped: []string{"TestTenants"}}}}}
	l := Library{Box: checkBox{b}}
	dir := pluginDir(t, map[string]string{"plugin.yaml": strings.Replace(packManifest, "runtimes: []", "runtimes: [inproc]", 1),
		"bundle.yaml": "[]\n", "go.mod": "module example.test/echo\n\ngo 1.26\n", "echo.go": "package echo\n"})
	out := filepath.Join(t.TempDir(), "artifact")
	if _, err := l.Pack(t.Context(), def.PackRequest{Caller: caller(), Dir: dir, Out: out}); err != nil {
		t.Fatal(err)
	}
	r, err := l.Check(t.Context(), def.CheckRequest{Caller: caller(), Dir: dir})
	if err != nil || !strings.Contains(string(r.Attestation), `"notRun":["TestTenants"]`) {
		t.Fatalf("Check = %v, %s, %v; want the attestation naming TestTenants", steps(r), r.Attestation, err)
	}
	desc, _, blob := layout(t, out)
	if refs := layoutReferrers(t, out, desc, "application/vnd.in-toto+json"); len(refs) != 1 ||
		!strings.Contains(string(blob(refs[0].Layers[0])), `"notRun":["TestTenants"]`) {
		t.Errorf("the attestation that pack recorded does not name TestTenants as not run")
	}
}

// Step 1 refuses a manifest that lists remote until S2b (ADR-060 decision 23).
func TestCheckRefusesRemote(t *testing.T) {
	b := &connectorBox{sum: launch.ManifestSummary{ID: "acme/x", Version: "0.1.0", Runtimes: []string{"inproc", "remote"}}}
	r, err := (Library{Box: b}).Check(t.Context(), def.CheckRequest{Dir: t.TempDir()})
	if err != nil || r.Steps[0].Outcome != def.StepFail || !strings.Contains(r.Steps[0].Diagnostics[0].Text, "S2b") {
		t.Errorf("remote: %v, %+v", err, r.Steps)
	}
}
