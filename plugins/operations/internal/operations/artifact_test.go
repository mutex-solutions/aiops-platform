// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"archive/tar"
	"bytes"
	"cmp"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/ocitest"
)

// The artifact of ADR-047 decisions 1 to 3 and its attestation and signatures as
// referrers (ADR-020 decisions 2, 9 and 24), file by file and blob by blob: the T1
// cases that the suite cannot see through the definition.

const packManifest = `manifestVersion: "1.0"
id: acme/connector-x
version: 0.1.0
title: Acme connector for X
license: Apache-2.0
publisher: {id: acme}
stability: experimental
scope: [process]
runtimes: []
requires: {definitions: {echoers: ["0.1"]}}
`

// pack is the plugin directory of a pack, by path.
var pack = map[string]string{
	"plugin.yaml":                 packManifest,
	"bundle.yaml":                 "- insert: [{id: logs, plugin: aiops/echoer, roles: [api], enabled: false, config: {endpoint: 'http://x.example.test'}}]\n",
	"content/runbooks/restart.md": "# Restart\n",
	"content/labels.yaml":         "service: x\n",
	"fixtures/echo.json":          "{}\n",
}

// pluginDir writes files under a new directory and returns it.
func pluginDir(t *testing.T, files map[string]string) string {
	t.Helper()
	return writeTree(t, t.TempDir(), files)
}

// writeTree writes files under dir and returns it.
func writeTree(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	for name, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// packBox is the launcher's toolbox of these tests: ReadManifest answers sum and diags, the gate
// answers the clauses of deny and records each request, and Suite and Scenario answer
// suite and scenario, or that they are not carried when nil, as in an aiopsctl built
// without the suite binaries; each records whether its directory held the fixtures.
// Module answers mod at the version asked, as the module zip of the toolbox reads it,
// or that it is not carried when mod names no path, as in aiops.
type packBox struct {
	launch.NoTools
	sum      launch.ManifestSummary
	diags    []launch.Diagnostic
	deny     []launch.Clause
	suite    *launch.SuiteReport
	scenario *launch.ScenarioReport
	mod      launch.Module
	built    launch.Built
	buildErr error
	mu       sync.Mutex
	gates    []launch.GateRequest
	builds   []launch.BuildRequest
	fixtures []bool
}

// Build records each request and answers built and buildErr.
func (b *packBox) Build(_ context.Context, q launch.BuildRequest) (launch.Built, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.builds = append(b.builds, q)
	return b.built, b.buildErr
}

func (b *packBox) Module(ctx context.Context, q launch.ModuleRequest) (launch.Module, error) {
	if b.mod.Path == "" {
		return b.NoTools.Module(ctx, q)
	}
	m := b.mod
	m.Version = q.Version
	return m, nil
}

func (*packBox) Render(context.Context, launch.RenderRequest) (launch.Rendered, error) {
	return launch.Rendered{}, launch.ErrNotCarried
}

func (*packBox) Shipped(context.Context) (map[string][]byte, error) { return nil, nil }

func (*packBox) Release(context.Context, string) (launch.Release, error) {
	return launch.Release{}, launch.ErrNotCarried
}

func (b *packBox) ReadManifest(_ context.Context, q launch.ManifestRequest) (launch.ManifestSummary, []launch.Diagnostic, error) {
	if _, err := os.Stat(filepath.Join(q.Dir, "plugin.yaml")); err != nil {
		return launch.ManifestSummary{}, nil, err
	}
	return b.sum, b.diags, nil
}

func (b *packBox) Gate(_ context.Context, q launch.GateRequest) (launch.Verdict, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.gates = append(b.gates, q)
	return launch.Verdict{Entry: q.Entry, Clauses: b.deny}, nil
}

func (b *packBox) Scenario(ctx context.Context, q launch.ScenarioRequest) (launch.ScenarioReport, error) {
	b.saw(q.Dir)
	if b.scenario == nil {
		return b.NoTools.Scenario(ctx, q)
	}
	return *b.scenario, nil
}

func (b *packBox) Suite(ctx context.Context, q launch.SuiteRequest) (launch.SuiteReport, error) {
	b.saw(q.Dir)
	if b.suite == nil {
		return b.NoTools.Suite(ctx, q)
	}
	return *b.suite, nil
}

func (b *packBox) saw(dir string) {
	_, err := os.Stat(filepath.Join(dir, "fixtures", "echo.json"))
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fixtures = append(b.fixtures, err == nil)
}

func packSummary() launch.ManifestSummary {
	return launch.ManifestSummary{ID: "acme/connector-x", Version: "0.1.0", Definitions: map[string][]string{"echoers": {"0.1"}}}
}

// layout reads the OCI image layout dir: the descriptor of the one manifest in
// index.json of the artifact type of a plugin (ADR-047 decision 1), the manifest, and
// its blobs by digest.
func layout(t *testing.T, dir string) (ocispec.Descriptor, ocispec.Manifest, func(ocispec.Descriptor) []byte) {
	t.Helper()
	blob := func(d ocispec.Descriptor) []byte {
		b, err := os.ReadFile(filepath.Join(dir, "blobs", "sha256", d.Digest.Encoded()))
		if err != nil {
			t.Fatal(err)
		}
		if d.Digest.String() != ocitest.Digest(b) {
			t.Fatalf("the blob %s does not hash to its name", d.Digest)
		}
		return b
	}
	if b, err := os.ReadFile(filepath.Join(dir, "oci-layout")); err != nil || !strings.Contains(string(b), `"imageLayoutVersion":"1.0.0"`) {
		t.Fatalf("oci-layout: %s, %v", b, err)
	}
	var index ocispec.Index
	b, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err == nil {
		err = json.Unmarshal(b, &index)
	}
	var found []ocispec.Descriptor
	for _, d := range index.Manifests {
		var m ocispec.Manifest
		if json.Unmarshal(blob(d), &m) == nil && m.ArtifactType == "application/vnd.aiops.plugin.v1" {
			found = append(found, d)
		}
	}
	if err != nil || len(found) != 1 {
		t.Fatalf("index.json %s, %v; want one manifest of the artifact type of a plugin", b, err)
	}
	var m ocispec.Manifest
	if err := json.Unmarshal(blob(found[0]), &m); err != nil {
		t.Fatal(err)
	}
	return found[0], m, blob
}

// statements returns the referrers of artifactType of the manifest d that the store
// at dir holds, read from every manifest blob whose subject is d.
func layoutReferrers(t *testing.T, dir string, d ocispec.Descriptor, artifactType string) []ocispec.Manifest {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, "blobs", "sha256"))
	if err != nil {
		t.Fatal(err)
	}
	var out []ocispec.Manifest
	for _, e := range entries {
		b, _ := os.ReadFile(filepath.Join(dir, "blobs", "sha256", e.Name()))
		var m ocispec.Manifest
		if json.Unmarshal(b, &m) == nil && m.Subject != nil && m.Subject.Digest == d.Digest && m.ArtifactType == artifactType {
			out = append(out, m)
		}
	}
	return out
}

// tarFiles returns the regular files of the tar b by name, and fails on an entry that
// is not in the canonical form of ADR-047 decision 3.
func tarFiles(t *testing.T, b []byte) map[string]string {
	t.Helper()
	r := tar.NewReader(bytes.NewReader(b))
	out, last := map[string]string{}, ""
	for {
		h, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag != tar.TypeReg || h.Mode != 0o644 || h.Uid != 0 || h.Gid != 0 || h.Uname != "" || h.Gname != "" ||
			!h.ModTime.Equal(time.Unix(0, 0)) || h.Name <= last || len(h.PAXRecords) > 0 {
			t.Errorf("the entry %+v is not in the canonical form", h)
		}
		last = h.Name
		data, _ := io.ReadAll(r)
		out[h.Name] = string(data)
	}
	return out
}

// plugin pack writes one OCI image layout with one manifest of artifactType
// application/vnd.aiops.plugin.v1, named by its version, whose layers are plugin.yaml,
// the bundle, the content and the fixtures as canonical tars and an SPDX 2.3 SBOM, in
// that order, each titled; the content paths in the annotation of the content layer;
// and the unsigned statement as a referrer, whose subject is the artifact's digest. It
// hands the gate the artifact as plugin check step 7 does, and with no scenario run it
// says so and names no scenario. Two packs of one directory give one digest.
func TestPackWritesOneArtifact(t *testing.T) {
	b := &packBox{sum: packSummary()}
	l := Library{Box: b}
	dir := pluginDir(t, pack)
	out := filepath.Join(dir, "artifact")
	var progress bytes.Buffer
	c := caller()
	c.Progress = &progress
	if w, err := l.Pack(t.Context(), def.PackRequest{Caller: c, Dir: dir, Out: out}); err != nil || !slices.Equal(w.Paths, []string{out}) {
		t.Fatalf("Pack = %v, %v", w, err)
	}
	desc, m, blob := layout(t, out)
	if desc.Annotations[ocispec.AnnotationRefName] != "0.1.0" || desc.MediaType != ocispec.MediaTypeImageManifest {
		t.Errorf("the index names %+v; want the image manifest at the ref name 0.1.0", desc)
	}
	if m.ArtifactType != "application/vnd.aiops.plugin.v1" || m.Config.MediaType != "application/vnd.oci.empty.v1+json" || m.Subject != nil {
		t.Errorf("manifest %+v", m)
	}
	var types, titles []string
	for _, l := range m.Layers {
		types, titles = append(types, l.MediaType), append(titles, l.Annotations[ocispec.AnnotationTitle])
	}
	if !slices.Equal(types, []string{"application/vnd.aiops.plugin.manifest.v1+yaml", "application/vnd.aiops.plugin.bundle.v1+yaml",
		"application/vnd.aiops.plugin.content.v1.tar", "application/vnd.aiops.plugin.fixtures.v1.tar", "application/spdx+json"}) ||
		!slices.Equal(titles, []string{"plugin.yaml", "bundle.yaml", "content.tar", "fixtures.tar", "sbom.spdx.json"}) {
		t.Fatalf("layers %v titled %v", types, titles)
	}
	if string(blob(m.Layers[0])) != pack["plugin.yaml"] || string(blob(m.Layers[1])) != pack["bundle.yaml"] {
		t.Error("plugin.yaml or bundle.yaml is not the layer byte for byte")
	}
	content := tarFiles(t, blob(m.Layers[2]))
	if len(content) != 2 || content["labels.yaml"] != pack["content/labels.yaml"] || content["runbooks/restart.md"] != pack["content/runbooks/restart.md"] {
		t.Errorf("the content layer holds %v", content)
	}
	if got := m.Layers[2].Annotations["aiops.plugin.content.paths"]; got != `["labels.yaml","runbooks/restart.md"]` {
		t.Errorf("the content paths annotation is %q", got)
	}
	if f := tarFiles(t, blob(m.Layers[3])); len(f) != 1 || f["echo.json"] != "{}\n" {
		t.Errorf("the fixtures layer holds %v", f)
	}
	var sbom struct {
		SPDXVersion, DataLicense, DocumentNamespace string
		Packages                                    []struct{ Name, VersionInfo, LicenseDeclared string }
		Files                                       []struct {
			FileName  string
			Checksums []struct{ Algorithm, ChecksumValue string }
		}
	}
	if err := json.Unmarshal(blob(m.Layers[4]), &sbom); err != nil || sbom.SPDXVersion != "SPDX-2.3" || sbom.DataLicense != "CC0-1.0" ||
		len(sbom.Packages) != 1 || sbom.Packages[0].Name != "acme/connector-x" || sbom.Packages[0].LicenseDeclared != "Apache-2.0" || len(sbom.Files) != 3 {
		t.Fatalf("SBOM %+v, %v", sbom, err)
	}
	for _, f := range sbom.Files {
		name := strings.TrimPrefix(f.FileName, "./")
		if len(f.Checksums) != 1 || f.Checksums[0].Algorithm != "SHA256" || "sha256:"+f.Checksums[0].ChecksumValue != ocitest.Digest([]byte(pack[name])) {
			t.Errorf("the SBOM lists %+v", f)
		}
	}
	sts := layoutReferrers(t, out, desc, "application/vnd.in-toto+json")
	if len(sts) != 1 || len(sts[0].Layers) != 1 {
		t.Fatalf("the statement referrers %+v; want one", sts)
	}
	statement := blob(sts[0].Layers[0])
	var st struct {
		Type          string `json:"_type"`
		PredicateType string
		Subject       []struct {
			Name   string
			Digest map[string]string
		}
		Predicate struct {
			Definitions map[string]map[string]any
			Scenario    any
		}
	}
	if err := json.Unmarshal(statement, &st); err != nil || st.Type != "https://in-toto.io/Statement/v1" ||
		st.PredicateType != "https://github.com/mutex-solutions/aiops-platform/attestation/conformance/v1" || len(st.Subject) != 1 ||
		st.Subject[0].Name != "acme/connector-x@0.1.0" || "sha256:"+st.Subject[0].Digest["sha256"] != desc.Digest.String() ||
		st.Predicate.Definitions["echoers"]["version"] != "0.1" || st.Predicate.Scenario != nil {
		t.Errorf("the statement %s, %v", statement, err)
	}
	if !strings.Contains(progress.String(), "not built") {
		t.Errorf("progress %q; want a line that says the scenario is not built", progress.String())
	}
	if len(b.gates) != 1 || b.gates[0].Entry != "plugin check" || b.gates[0].Digest != desc.Digest.String() ||
		string(b.gates[0].Manifest) != packManifest || string(b.gates[0].Attestation) != string(statement) {
		t.Errorf("the gate was asked %+v", b.gates)
	}
	again := filepath.Join(t.TempDir(), "again")
	if _, err := l.Pack(t.Context(), def.PackRequest{Caller: caller(), Dir: dir, Out: again}); err != nil {
		t.Fatal(err)
	}
	if d, _, _ := layout(t, again); d.Digest != desc.Digest {
		t.Errorf("a second pack gives %s, the first %s", d.Digest, desc.Digest)
	}
}

// The module layer of in-process code holds the path, the version of the manifest and
// the h1 sum that the module zip of the toolbox reads; its SBOM lists that module and
// its build list, each module with its path, its version with its replacement and its
// sum, none for a replacement by a directory (ADR-047 decision 1). It carries no
// fixtures layer. Where the toolbox carries no module zip, as in aiops, pack refuses
// and leaves no layout.
func TestPackOfInProcessCode(t *testing.T) {
	sum := packSummary()
	sum.Runtimes = []string{"inproc"}
	b := &packBox{sum: sum}
	l := Library{Box: b}
	dir := pluginDir(t, map[string]string{
		"plugin.yaml": strings.Replace(packManifest, "runtimes: []", "runtimes: [inproc]", 1),
		"bundle.yaml": "[]\n",
		"go.mod":      "module example.test/echo\n",
		"echo.go":     "package echo\n",
		"fixtures/x":  "x\n",
	})
	out := filepath.Join(t.TempDir(), "artifact")
	if _, err := l.Pack(t.Context(), def.PackRequest{Caller: caller(), Dir: dir, Out: out}); !errors.Is(err, def.ErrRefused) || !strings.Contains(err.Error(), launch.ErrNotCarried.Error()) {
		t.Errorf("Pack with no module zip = %v; want a refusal naming the tool not carried", err)
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused pack left %s: %v", out, err)
	}
	b.mod = launch.Module{Path: "example.test/echo", Sum: echoSum, Deps: []launch.Module{{Path: "example.test/b", Version: "v1.0.0 => ../b"},
		{Path: "example.test/c", Version: "v1.0.0 => ../c"}, {Path: "golang.org/x/sync", Version: "v0.24.0", Sum: "h1:iqiDrGoDgyuvAKrhLQthUEzU/R378N78gXbBjxed7oA="}}}
	if _, err := l.Pack(t.Context(), def.PackRequest{Caller: caller(), Dir: dir, Out: out}); err != nil {
		t.Fatal(err)
	}
	_, m, blob := layout(t, out)
	var types []string
	for _, l := range m.Layers {
		types = append(types, l.MediaType)
	}
	if !slices.Equal(types, []string{"application/vnd.aiops.plugin.manifest.v1+yaml", "application/vnd.aiops.plugin.bundle.v1+yaml",
		"application/vnd.aiops.plugin.module.v1+json", "application/spdx+json"}) {
		t.Fatalf("layers %v", types)
	}
	if got := string(blob(m.Layers[2])); got != `{"path":"example.test/echo","version":"v0.1.0","sum":"`+echoSum+`"}` {
		t.Errorf("module.json %s", got)
	}
	var sbom struct {
		Packages []struct{ Name, VersionInfo, LicenseConcluded, Comment string }
	}
	if err := json.Unmarshal(blob(m.Layers[3]), &sbom); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range sbom.Packages[1:] {
		got = append(got, p.Name+" "+p.VersionInfo+" "+p.LicenseConcluded+" "+p.Comment)
	}
	if want := []string{"example.test/echo v0.1.0 NOASSERTION sum " + echoSum, "example.test/b v1.0.0 => ../b NOASSERTION ", "example.test/c v1.0.0 => ../c NOASSERTION ",
		"golang.org/x/sync v0.24.0 NOASSERTION sum h1:iqiDrGoDgyuvAKrhLQthUEzU/R378N78gXbBjxed7oA="}; !slices.Equal(got, want) {
		t.Errorf("the SBOM lists the modules\n%q\nwant\n%q", got, want)
	}
}

// echoSum is an h1 sum of the module example.test/echo at v0.1.0, as go mod download
// would record it.
const echoSum = "h1:Ryl9hfaUbtcrXqV18lzBbPaztAz29w8OPhu5dRdlIXM="

// The suites step of verify builds in-process code with the builder of the toolbox: the
// module that module.json of the artifact records, at its version and sum, on the
// release artifact and from the module cache of the request, whose tests, the suites
// among them, the builder runs (ADR-060 decision 46; ADR-044 decision 1). With no
// release or no module cache it builds nothing and is not built; a build that fails is
// a step that fails with its reason, and one that the toolbox does not carry is not
// built.
func TestVerifyBuildsInProcessCode(t *testing.T) {
	sum := packSummary()
	sum.Runtimes = []string{"inproc"}
	b := &packBox{sum: sum, mod: launch.Module{Path: "example.test/echo", Sum: echoSum}}
	l := Library{Box: b, LockWait: time.Second}
	out := filepath.Join(t.TempDir(), "artifact")
	dir := pluginDir(t, map[string]string{"plugin.yaml": strings.Replace(packManifest, "runtimes: []", "runtimes: [inproc]", 1),
		"bundle.yaml": "[]\n", "go.mod": "module example.test/echo\n", "echo.go": "package echo\n"})
	if _, err := l.Pack(t.Context(), def.PackRequest{Caller: caller(), Dir: dir, Out: out}); err != nil {
		t.Fatal(err)
	}
	home, prefix := filepath.Join(t.TempDir(), "home"), generate(t, l, t.TempDir(), "author")
	if _, err := l.AddTrust(t.Context(), def.TrustRequest{Caller: caller(), Home: home, KeyFile: prefix + ".pub", IDs: []string{"acme/*"}, Runtimes: []string{"inproc"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Sign(t.Context(), def.SignRequest{Caller: caller(), Out: out, Key: prefix + ".key"}); err != nil {
		t.Fatal(err)
	}
	q := def.SpecRequest{Caller: caller(), Spec: out, Home: home, Builder: "local"}
	v, err := l.Verify(t.Context(), q)
	if err != nil || v.Passed || v.Steps[2].Outcome != "not-built" || !strings.Contains(v.Steps[2].Reason, "--against") || len(b.builds) != 0 {
		t.Fatalf("Verify of in-process code with no release = %+v, %v (%d builds); want the suites not built", v, err, len(b.builds))
	}
	q.Against, q.ModCache, q.Mirrors = "oci://registry.test/aiops/release@sha256:"+strings.Repeat("ab", 32), "/modcache", map[string]string{"registry.test": "/mirror"}
	v, err = l.Verify(t.Context(), q)
	want := launch.BuildRequest{Module: "example.test/echo", Version: "v0.1.0", Sum: echoSum, Against: q.Against, ModCache: "/modcache", Mirrors: q.Mirrors, Builder: "local"}
	if err != nil || !v.Passed || v.Steps[2].Outcome != "pass" || len(b.builds) != 1 || !reflect.DeepEqual(b.builds[0], want) {
		t.Errorf("Verify of in-process code = %+v, %v; builds %+v, want one of %+v", v, err, b.builds, want)
	}
	for _, c := range []struct {
		err     error
		outcome string
	}{{errors.New("the suites of example.test/echo: FAIL"), "fail"}, {launch.ErrNotCarried, "not-built"}} {
		b.buildErr = c.err
		if v, err := l.Verify(t.Context(), q); err != nil || v.Passed || v.Steps[2].Outcome != c.outcome || !strings.Contains(v.Steps[2].Reason, c.err.Error()) {
			t.Errorf("Verify with a build that answers %v = %+v, %v; want %s", c.err, v, err, c.outcome)
		}
	}
}

// plugin smoke hands the plugin directory, the release artifact, the module cache, the
// mirrors and the builder to the builder of the toolbox, and passes when every row of
// the test-only profile it booted is ACTIVE or names the keys it waits for (ADR-060
// decision 58, reading 1); a row that is neither fails it, naming the row, and so does
// a profile with no row. It refuses a relative path and a release that is missing or
// not named by digest (08:401), and builds nothing then.
func TestSmoke(t *testing.T) {
	b := &packBox{built: launch.Built{Rows: []launch.RowWait{{ID: "clock", State: kernel.Active},
		{ID: "echo:echo", State: kernel.Pending, WaitsFor: []string{"echoers"}}}}}
	l := Library{Box: b}
	q := def.BuildRequest{Caller: caller(), BuildRequest: launch.BuildRequest{Plugin: "/work/echo", Against: "oci://registry.test/aiops/release@sha256:" + strings.Repeat("ab", 32),
		ModCache: "/modcache", Mirrors: map[string]string{"registry.test": "/mirror"}, Builder: "image"}}
	if got, err := l.Smoke(t.Context(), q); err != nil || !reflect.DeepEqual(got, b.built) || len(b.builds) != 1 || !reflect.DeepEqual(b.builds[0], q.BuildRequest) {
		t.Errorf("Smoke = %+v, %v; builds %+v", got, err, b.builds)
	}
	b.built.Rows = append(b.built.Rows, launch.RowWait{ID: "echo:broken", State: kernel.Failed})
	if _, err := l.Smoke(t.Context(), q); !errors.Is(err, def.ErrRefused) || !strings.Contains(err.Error(), "echo:broken") || strings.Contains(err.Error(), "echo:echo") {
		t.Errorf("Smoke with a FAILED row = %v; want a refusal naming it alone", err)
	}
	b.built.Rows = nil
	if _, err := l.Smoke(t.Context(), q); !errors.Is(err, def.ErrRefused) {
		t.Errorf("Smoke that booted no row = %v; want a refusal", err)
	}
	for _, bad := range []func(*def.BuildRequest){func(q *def.BuildRequest) { q.Plugin = "echo" }, func(q *def.BuildRequest) { q.ModCache = "" },
		func(q *def.BuildRequest) { q.Against = "" }, func(q *def.BuildRequest) { q.Against = "oci://registry.test/aiops/release:latest" }} {
		r := q
		bad(&r)
		if _, err := l.Smoke(t.Context(), r); !errors.Is(err, def.ErrUsage) {
			t.Errorf("Smoke(%+v) = %v; want ErrUsage", r.BuildRequest, err)
		}
	}
	if len(b.builds) != 3 {
		t.Errorf("a refused smoke built: %d builds", len(b.builds))
	}
}

// Pack refuses, and leaves no layout, where the gate denies a clause, where a content
// entry is a link, where the manifest lists a remote runtime, and where the out
// directory holds a file. A check 10 denial where the suites did not run refuses too,
// naming the suite binaries this binary does not carry: install denies such an artifact
// (ADR-060 decisions 17, 23, 46 and 56).
func TestPackRefuses(t *testing.T) {
	ctx := t.Context()
	b := &packBox{sum: packSummary(), deny: []launch.Clause{{Check: 8, Clause: "installed", Outcome: launch.OutcomeDeny, Reason: "row logs is enabled"}}}
	dir := pluginDir(t, pack)
	out := filepath.Join(t.TempDir(), "artifact")
	if _, err := (Library{Box: b}).Pack(ctx, def.PackRequest{Caller: caller(), Dir: dir, Out: out}); !errors.Is(err, def.ErrRefused) || !strings.Contains(err.Error(), "row logs is enabled") {
		t.Errorf("a denial of check 8: %v, want ErrRefused with its reason", err)
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused pack left %s: %v", out, err)
	}
	b.deny = []launch.Clause{{Check: 10, Outcome: launch.OutcomeDeny, Reason: "the attestation of a pack names no scenario version"}}
	if _, err := (Library{Box: b}).Pack(ctx, def.PackRequest{Caller: caller(), Dir: dir, Out: out}); !errors.Is(err, def.ErrRefused) ||
		!strings.Contains(err.Error(), "no scenario version") || !strings.Contains(err.Error(), "suite binaries") {
		t.Errorf("a denial of check 10 with no scenario run: %v, want ErrRefused naming the suite binaries", err)
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a pack refused by check 10 left %s: %v", out, err)
	}
	b.deny = nil
	if err := os.Symlink("labels.yaml", filepath.Join(dir, "content", "link.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := (Library{Box: b}).Pack(ctx, def.PackRequest{Caller: caller(), Dir: dir, Out: filepath.Join(t.TempDir(), "a")}); !errors.Is(err, def.ErrRefused) {
		t.Errorf("a link in content: %v, want ErrRefused", err)
	}
	b.sum.Runtimes = []string{"remote"}
	if _, err := (Library{Box: b}).Pack(ctx, def.PackRequest{Caller: caller(), Dir: pluginDir(t, pack), Out: filepath.Join(t.TempDir(), "a")}); !errors.Is(err, def.ErrRefused) {
		t.Errorf("a remote runtime: %v, want ErrRefused", err)
	}
}

// signed packs the pack, makes a home that trusts a new key for acme/*, signs the
// artifact with it and returns the layout, the home and the key's prefix.
func signed(t *testing.T) (Library, string, string, string) {
	t.Helper()
	l := Library{Box: &packBox{sum: packSummary()}, LockWait: time.Second}
	out := filepath.Join(t.TempDir(), "artifact")
	if _, err := l.Pack(t.Context(), def.PackRequest{Caller: caller(), Dir: pluginDir(t, pack), Out: out}); err != nil {
		t.Fatal(err)
	}
	home, prefix := filepath.Join(t.TempDir(), "home"), generate(t, l, t.TempDir(), "author")
	if _, err := l.AddTrust(t.Context(), def.TrustRequest{Caller: caller(), Home: home, KeyFile: prefix + ".pub", IDs: []string{"acme/*"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Sign(t.Context(), def.SignRequest{Caller: caller(), Out: out, Key: prefix + ".key"}); err != nil {
		t.Fatal(err)
	}
	return l, out, home, prefix
}

// plugin sign writes the referrers that cosign writes in its bundle format: one of the
// bundle's artifactType for the signature, a DSSE envelope of a statement of predicate
// type https://sigstore.dev/cosign/sign/v1 over the artifact's digest, and one for the
// attestation, of the conformance predicate type, each named in the annotation
// dev.sigstore.bundle.predicateType; the unsigned statement is replaced by its signed
// bundle (ADR-020 decisions 2, 9 and 24).
func TestSignWritesTheReferrersOfCosign(t *testing.T) {
	_, out, _, prefix := signed(t)
	desc, _, blob := layout(t, out)
	pubPEM, _ := os.ReadFile(prefix + ".pub")
	pub, _, _ := LoadPublicKey(pubPEM)
	refs := layoutReferrers(t, out, desc, "application/vnd.dev.sigstore.bundle.v0.3+json")
	var predicates []string
	for _, r := range refs {
		p := r.Annotations["dev.sigstore.bundle.predicateType"]
		predicates = append(predicates, p)
		st, err := VerifyStatement(pub, blob(r.Layers[0]), mustHex(t, desc.Digest.Encoded()))
		if err != nil || r.Layers[0].MediaType != "application/vnd.dev.sigstore.bundle.v0.3+json" || !strings.Contains(string(st), `"predicateType":"`+p+`"`) {
			t.Errorf("the referrer %+v: %s, %v", r, st, err)
		}
	}
	slices.Sort(predicates)
	if !slices.Equal(predicates, []string{"https://github.com/mutex-solutions/aiops-platform/attestation/conformance/v1", "https://sigstore.dev/cosign/sign/v1"}) {
		t.Errorf("the bundle referrers name %v", predicates)
	}
	if left := layoutReferrers(t, out, desc, "application/vnd.in-toto+json"); len(left) != 0 {
		t.Errorf("the unsigned statement is still a referrer: %+v", left)
	}
}

// plugin sign refuses, and writes no referrer, where the statement that pack recorded
// names another digest or another id and version than the artifact's: its one subject
// is the digest of the OCI manifest, named <id>@<version> of its plugin.yaml (ADR-020
// decisions 9 and 24).
func TestSignRefusesAnotherSubject(t *testing.T) {
	l := Library{Box: &packBox{sum: packSummary()}}
	prefix := generate(t, l, t.TempDir(), "author")
	other := ocitest.Digest([]byte("another artifact"))
	for _, st := range []struct{ name, version, digest string }{{"evil/other", "9.9.9", other}, {"evil/other", "9.9.9", ""}, {"acme/connector-x", "0.1.0", other}} {
		out := filepath.Join(t.TempDir(), "artifact")
		if _, err := l.Pack(t.Context(), def.PackRequest{Caller: caller(), Dir: pluginDir(t, pack), Out: out}); err != nil {
			t.Fatal(err)
		}
		desc, _, _ := layout(t, out)
		s, err := oci.NewWithContext(t.Context(), out)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range layoutReferrers(t, out, desc, "application/vnd.in-toto+json") {
			raw, _ := json.Marshal(r)
			if err := s.Delete(t.Context(), content.NewDescriptorFromBytes(ocispec.MediaTypeImageManifest, raw)); err != nil {
				t.Fatal(err)
			}
		}
		digest := strings.TrimPrefix(cmp.Or(st.digest, desc.Digest.String()), "sha256:")
		forged := def.Statement(st.name, st.version, digest, map[string]def.Attested{"echoers": {Version: "0.1"}}, nil, nil)
		if _, err := pushArtifact(t.Context(), s, "application/vnd.in-toto+json", &desc, nil, blob{"application/vnd.in-toto+json", forged, nil}); err != nil {
			t.Fatal(err)
		}
		if _, err := l.Sign(t.Context(), def.SignRequest{Caller: caller(), Out: out, Key: prefix + ".key"}); !errors.Is(err, def.ErrRefused) {
			t.Errorf("sign of a statement of %s@%s at %s: %v, want ErrRefused", st.name, st.version, digest, err)
		}
		if refs := layoutReferrers(t, out, desc, "application/vnd.dev.sigstore.bundle.v0.3+json"); len(refs) != 0 {
			t.Errorf("a refused sign wrote %d bundle referrers", len(refs))
		}
	}
}

// Push copies the artifact and its referrers to a registry with no referrers API,
// which lists them under the referrers tag schema, and verify reads them from it.
func TestPushAndVerifyWithoutReferrersAPI(t *testing.T) {
	l, out, home, _ := signed(t)
	reg := ocitest.New(t, ocitest.Options{NoReferrers: true})
	p, err := l.Push(t.Context(), def.PushRequest{Caller: caller(), Out: out, Registry: reg.Host + "/connector-x"})
	if err != nil {
		t.Fatal(err)
	}
	v, err := l.Verify(t.Context(), def.SpecRequest{Caller: caller(), Spec: p.Spec, Home: home})
	if err != nil || v.Steps[0].Outcome != "pass" || v.Steps[1].Outcome != "pass" || v.Steps[2].Outcome != "not-built" || v.Passed {
		t.Errorf("Verify = %+v, %v; want the signature and the attestation passing and the suites not built", v, err)
	}
}

// inspect fetches the manifest and the bundle of an artifact, and no other blob of it
// (ADR-060 decision 58, reading 2; ADR-047 decision 2).
func TestInspectFetchesTheManifestAndTheBundleOnly(t *testing.T) {
	l, out, _, _ := signed(t)
	_, m, _ := layout(t, out)
	reg := ocitest.New(t, ocitest.Options{})
	p, err := l.Push(t.Context(), def.PushRequest{Caller: caller(), Out: out, Registry: reg.Host + "/connector-x"})
	if err != nil {
		t.Fatal(err)
	}
	before := len(reg.Requests())
	if _, err := l.Inspect(t.Context(), def.SpecRequest{Caller: caller(), Spec: p.Spec}); err != nil {
		t.Fatal(err)
	}
	for _, r := range reg.Requests()[before:] {
		for _, layer := range m.Layers[2:] {
			if strings.HasPrefix(r, "GET ") && strings.HasSuffix(r, layer.Digest.String()) {
				t.Errorf("inspect fetched %s, the %s layer", r, layer.MediaType)
			}
		}
		if strings.Contains(r, "/referrers/") {
			t.Errorf("inspect read the referrers: %s", r)
		}
	}
}

// verify fetches of the layers only those its suites run on, plugin.yaml, the bundle
// and the fixtures, and never the SBOM, which install never fetches either, nor a layer
// type it does not know (ADR-047 decisions 1 and 2).
func TestVerifyFetchesNoSBOM(t *testing.T) {
	l, out, home, _ := signed(t)
	_, m, _ := layout(t, out)
	reg := ocitest.New(t, ocitest.Options{})
	p, err := l.Push(t.Context(), def.PushRequest{Caller: caller(), Out: out, Registry: reg.Host + "/connector-x"})
	if err != nil {
		t.Fatal(err)
	}
	before := len(reg.Requests())
	if _, err := l.Verify(t.Context(), def.SpecRequest{Caller: caller(), Spec: p.Spec, Home: home}); err != nil {
		t.Fatal(err)
	}
	for _, r := range reg.Requests()[before:] {
		for _, layer := range m.Layers {
			if layer.MediaType == "application/spdx+json" && strings.HasPrefix(r, "GET ") && strings.HasSuffix(r, layer.Digest.String()) {
				t.Errorf("verify fetched %s, the %s layer", r, layer.MediaType)
			}
		}
	}
}

// verify refuses an attestation that verifies with the signer's key over the digest
// and names another subject, by check 10 (ADR-020 decision 14).
func TestVerifyRefusesAnotherSubject(t *testing.T) {
	l, out, home, prefix := signed(t)
	desc, _, _ := layout(t, out)
	priv, _ := os.ReadFile(prefix + ".key")
	k, err := LoadPrivateKey(priv, []byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	st := `{"_type":"https://in-toto.io/Statement/v1","subject":[{"name":"acme/other@0.1.0","digest":{"sha256":"` + desc.Digest.Encoded() +
		`"}}],"predicateType":"https://github.com/mutex-solutions/aiops-platform/attestation/conformance/v1","predicate":{"definitions":{}}}`
	b, err := SignStatement(k, []byte(st))
	if err != nil {
		t.Fatal(err)
	}
	// The layout keeps the signature and takes, in place of the attestation, a referrer
	// of the same form whose statement names acme/other.
	s, err := oci.NewWithContext(t.Context(), out)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range layoutReferrers(t, out, desc, "application/vnd.dev.sigstore.bundle.v0.3+json") {
		if r.Annotations["dev.sigstore.bundle.predicateType"] != "https://sigstore.dev/cosign/sign/v1" {
			raw, _ := json.Marshal(r)
			if err := s.Delete(t.Context(), content.NewDescriptorFromBytes(ocispec.MediaTypeImageManifest, raw)); err != nil {
				t.Fatal(err)
			}
		}
	}
	ann := map[string]string{"dev.sigstore.bundle.predicateType": "https://github.com/mutex-solutions/aiops-platform/attestation/conformance/v1"}
	if _, err := pushArtifact(t.Context(), s, "application/vnd.dev.sigstore.bundle.v0.3+json", &desc, ann,
		blob{"application/vnd.dev.sigstore.bundle.v0.3+json", b, nil}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Verify(t.Context(), def.SpecRequest{Caller: caller(), Spec: out, Home: home}); !errors.Is(err, def.ErrRefused) || !strings.Contains(err.Error(), "check 10") {
		t.Errorf("an attestation of another subject: %v, want ErrRefused naming check 10", err)
	}
}

// verify takes, in place of the signature of plugin sign, the one cosign sign writes,
// whose one subject has the artifact's digest and no name, so the signer of an artifact
// that cosign signed is the key that verifies it (ADR-020 decision 2).
func TestVerifyTakesTheSignatureOfCosign(t *testing.T) {
	l, out, home, prefix := signed(t)
	desc, _, _ := layout(t, out)
	priv, _ := os.ReadFile(prefix + ".key")
	k, err := LoadPrivateKey(priv, []byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	// The statement as cosign marshals it
	// (github.com/sigstore/cosign/v3@v3.1.4/cmd/cosign/cli/sign/sign.go:158-179).
	st := `{"_type":"https://in-toto.io/Statement/v1","subject":[{"digest":{"sha256":"` + desc.Digest.Encoded() +
		`"},"annotations":{}}],"predicateType":"https://sigstore.dev/cosign/sign/v1","predicate":{}}`
	b, err := SignStatement(k, []byte(st))
	if err != nil {
		t.Fatal(err)
	}
	s, err := oci.NewWithContext(t.Context(), out)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range layoutReferrers(t, out, desc, "application/vnd.dev.sigstore.bundle.v0.3+json") {
		if r.Annotations["dev.sigstore.bundle.predicateType"] == "https://sigstore.dev/cosign/sign/v1" {
			raw, _ := json.Marshal(r)
			if err := s.Delete(t.Context(), content.NewDescriptorFromBytes(ocispec.MediaTypeImageManifest, raw)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := pushBundle(t.Context(), s, &desc, "https://sigstore.dev/cosign/sign/v1", b); err != nil {
		t.Fatal(err)
	}
	pub, _ := os.ReadFile(prefix + ".pub")
	_, id, _ := LoadPublicKey(pub)
	if v, err := l.Verify(t.Context(), def.SpecRequest{Caller: caller(), Spec: out, Home: home}); err != nil || v.Signer != id || v.Steps[1].Outcome != "pass" {
		t.Errorf("Verify of the signature cosign writes = %+v, %v; want the signer %s and the attestation verified", v, err, id)
	}
}

// An unsigned artifact whose id allowUnsigned lists verifies with the signer unsigned
// and the statement that pack recorded (ADR-020 decision 8).
func TestVerifyOfAnUnsignedArtifactThatAllowUnsignedLists(t *testing.T) {
	l := Library{Box: &packBox{sum: packSummary()}}
	out := filepath.Join(t.TempDir(), "artifact")
	if _, err := l.Pack(t.Context(), def.PackRequest{Caller: caller(), Dir: pluginDir(t, pack), Out: out}); err != nil {
		t.Fatal(err)
	}
	home := pluginDir(t, map[string]string{"trust.yaml": "allowUnsigned: [acme/connector-x]\n"})
	v, err := l.Verify(t.Context(), def.SpecRequest{Caller: caller(), Spec: out, Home: home})
	if err != nil || v.Signer != "unsigned" || v.Steps[1].Outcome != "pass" {
		t.Errorf("Verify = %+v, %v; want the signer unsigned and the attestation read", v, err)
	}
}

// verify refuses by check 5, before allowUnsigned, an artifact with no module whose id
// is under the reserved owner aiops or contrib and whose signer is not the release key
// the binary carries, as the gate does at install and at boot (ADR-048 decision 14).
func TestVerifyRefusesAReservedOwner(t *testing.T) {
	sum := packSummary()
	sum.ID = "aiops/connector-x"
	l := Library{Box: &packBox{sum: sum}}
	files := map[string]string{}
	for name, data := range pack {
		files[name] = strings.ReplaceAll(data, "acme", "aiops")
	}
	out := filepath.Join(t.TempDir(), "artifact")
	if _, err := l.Pack(t.Context(), def.PackRequest{Caller: caller(), Dir: pluginDir(t, files), Out: out}); err != nil {
		t.Fatal(err)
	}
	home := pluginDir(t, map[string]string{"trust.yaml": "allowUnsigned: [aiops/connector-x]\n"})
	if v, err := l.Verify(t.Context(), def.SpecRequest{Caller: caller(), Spec: out, Home: home}); !errors.Is(err, def.ErrRefused) || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("Verify of an unsigned aiops/connector-x = %+v, %v; want ErrRefused naming the reserved owner", v, err)
	}
}

// verify runs the suites of plugin check, through the same runs, on what a pack's
// artifact carries: step 5 on its fixtures and the echo scenario of step 8. The step
// passes when both pass, fails naming a failed obligation, and is not built where the
// binary carries no suite (ADR-060 decisions 46 and 49); pack records in the
// attestation the suite revision that the same runs report.
func TestVerifyRunsTheSuitesOfPluginCheck(t *testing.T) {
	l, out, home, _ := signed(t)
	b := l.Box.(*packBox)
	v, err := l.Verify(t.Context(), def.SpecRequest{Caller: caller(), Spec: out, Home: home})
	if err != nil || v.Passed || v.Steps[2].Outcome != "not-built" || !strings.Contains(v.Steps[2].Reason, "suite") {
		t.Fatalf("Verify with no suite carried = %+v, %v; want the suites not built", v, err)
	}
	b.suite = &launch.SuiteReport{Definitions: []launch.SuiteResult{{Name: "echoers", Revision: 3, Halves: []string{"obligations"}}}}
	b.scenario = &launch.ScenarioReport{Name: "echo", Version: "1", Halves: []string{"kernel"}}
	b.fixtures = nil
	v, err = l.Verify(t.Context(), def.SpecRequest{Caller: caller(), Spec: out, Home: home})
	if err != nil || !v.Passed || len(v.Steps) != 3 || v.Steps[2].Name != "suites" || v.Steps[2].Outcome != "pass" {
		t.Errorf("Verify with the suites carried = %+v, %v; want every step passing", v, err)
	}
	if !slices.Equal(b.fixtures, []bool{true, true}) {
		t.Errorf("the suite and the scenario ran on a directory with the fixtures of the artifact: %v, want both", b.fixtures)
	}
	b.suite.Definitions[0].Failed = []string{"echoers/reply"}
	v, err = l.Verify(t.Context(), def.SpecRequest{Caller: caller(), Spec: out, Home: home})
	if err != nil || v.Passed || v.Steps[2].Outcome != "fail" || !strings.Contains(v.Steps[2].Reason, "echoers/reply") {
		t.Errorf("Verify with a failed obligation = %+v, %v; want the suites failing and naming it", v, err)
	}
	b.suite.Definitions[0].Failed = nil
	out2 := filepath.Join(t.TempDir(), "artifact")
	if _, err := l.Pack(t.Context(), def.PackRequest{Caller: caller(), Dir: pluginDir(t, pack), Out: out2}); err != nil {
		t.Fatal(err)
	}
	desc, _, blob := layout(t, out2)
	refs := layoutReferrers(t, out2, desc, "application/vnd.in-toto+json")
	if len(refs) != 1 || !strings.Contains(string(blob(refs[0].Layers[0])), `"echoers":{"version":"0.1","suite":3,"halves":["obligations"]}`) {
		t.Errorf("the attestation of pack does not record the suite revision 3 that step 5 reports")
	}
}

// The suites step of verify fails, and runs no suite, where boot's strict reader
// refuses the plugin.yaml of the artifact, as it refuses a manifest of a higher minor
// with a field it does not know, and where what it reads is another plugin than the one
// the signature and the attestation name (ADR-048 decision 15; ADR-060 decision 46).
func TestVerifyFailsWhatTheStrictReaderRefuses(t *testing.T) {
	l, out, home, _ := signed(t)
	b := l.Box.(*packBox)
	b.suite = &launch.SuiteReport{Definitions: []launch.SuiteResult{{Name: "echoers", Revision: 1, Failed: []string{"echoers/reply"}}}}
	b.scenario = &launch.ScenarioReport{Name: "echo", Version: "1", Halves: []string{"kernel"}}
	b.sum, b.diags = launch.ManifestSummary{}, []launch.Diagnostic{{Check: "1", File: "plugin.yaml", Text: "x-unknown is not a field of manifest version 1.0"}}
	v, err := l.Verify(t.Context(), def.SpecRequest{Caller: caller(), Spec: out, Home: home})
	if err != nil || v.Passed || v.Steps[2].Outcome != "fail" || !strings.Contains(v.Steps[2].Reason, "x-unknown") {
		t.Errorf("Verify of a manifest the strict reader refuses = %+v, %v; want the suites failing with its diagnostic", v, err)
	}
	b.sum, b.diags = launch.ManifestSummary{ID: "acme/other", Version: "0.1.0"}, nil
	v, err = l.Verify(t.Context(), def.SpecRequest{Caller: caller(), Spec: out, Home: home})
	if err != nil || v.Passed || v.Steps[2].Outcome != "fail" || !strings.Contains(v.Steps[2].Reason, "acme/other") {
		t.Errorf("Verify where the reader reads another plugin = %+v, %v; want the suites failing", v, err)
	}
}

// inspect and verify fetch no plugin.yaml and no bundle larger than the 1 MiB that boot
// reads of a file (boot.MaxFileBytes), and refuse it; verify fetches plugin.yaml of no
// artifact that no key of trust.yaml signed, where allowUnsigned lists no id (ADR-047
// decision 2).
func TestLargeManifestsAreNotFetched(t *testing.T) {
	reg := ocitest.New(t, ocitest.Options{})
	pad := bytes.Repeat([]byte("x"), 2<<20)
	big := map[string][]byte{"manifest": append([]byte("id: acme/x\nversion: 0.1.0\n#"), pad...), "bundle": append([]byte("[]\n#"), pad...)}
	reg.Artifact("manifest", "1", "application/vnd.aiops.plugin.v1", nil, ocitest.Layer{MediaType: "application/vnd.aiops.plugin.manifest.v1+yaml", Data: big["manifest"]},
		ocitest.Layer{MediaType: "application/vnd.aiops.plugin.bundle.v1+yaml", Data: []byte("[]\n")})
	reg.Artifact("bundle", "1", "application/vnd.aiops.plugin.v1", nil, ocitest.Layer{MediaType: "application/vnd.aiops.plugin.manifest.v1+yaml", Data: []byte("id: acme/x\nversion: 0.1.0\n")},
		ocitest.Layer{MediaType: "application/vnd.aiops.plugin.bundle.v1+yaml", Data: big["bundle"]})
	l := Library{Box: &packBox{}}
	fetched := func(name string) bool {
		return slices.ContainsFunc(reg.Requests(), func(r string) bool {
			return strings.HasPrefix(r, "GET ") && strings.HasSuffix(r, ocitest.Digest(big[name]))
		})
	}
	for _, name := range []string{"manifest", "bundle"} {
		spec := "oci://" + reg.Host + "/" + name + ":1"
		if _, err := l.Inspect(t.Context(), def.SpecRequest{Caller: caller(), Spec: spec}); !errors.Is(err, def.ErrRefused) || fetched(name) {
			t.Errorf("inspect of a %s of 2 MiB: %v, fetched %v; want ErrRefused and no fetch", name, err, fetched(name))
		}
	}
	reg = ocitest.New(t, ocitest.Options{})
	reg.Artifact("x", "1", "application/vnd.aiops.plugin.v1", nil, ocitest.Layer{MediaType: "application/vnd.aiops.plugin.manifest.v1+yaml", Data: big["manifest"]},
		ocitest.Layer{MediaType: "application/vnd.aiops.plugin.bundle.v1+yaml", Data: []byte("[]\n")})
	if _, err := l.Verify(t.Context(), def.SpecRequest{Caller: caller(), Spec: "oci://" + reg.Host + "/x:1", Home: t.TempDir()}); !errors.Is(err, def.ErrRefused) || fetched("manifest") {
		t.Errorf("verify of an unsigned artifact: %v, fetched %v; want ErrRefused and no fetch", err, fetched("manifest"))
	}
}

// inspect refuses what boot's reader and install refuse, before it hands it on to be
// printed: content paths outside the rules of ADR-047 decision 3, and an id or a
// version that is not one, each here holding an escape sequence that would rewrite the
// lines a terminal has printed.
func TestInspectRefusesWhatBootRefuses(t *testing.T) {
	reg := ocitest.New(t, ocitest.Options{})
	esc := "\x1b[1A\r\x1b[2Ksigner: release key (verified)"
	quoted, _ := json.Marshal(esc)
	for name, c := range map[string]struct{ manifest, paths string }{
		"path":    {"id: acme/x\nversion: 0.1.0\n", `["a.md` + strings.Trim(string(quoted), `"`) + `"]`},
		"dotdot":  {"id: acme/x\nversion: 0.1.0\n", `["../a.md"]`},
		"id":      {"id: \"acme/x\\e[2K\"\nversion: 0.1.0\n", `["a.md"]`},
		"version": {"id: acme/x\nversion: \"0.1.0\\r\"\n", `["a.md"]`},
	} {
		reg.Artifact(name, "1", "application/vnd.aiops.plugin.v1", nil, ocitest.Layer{MediaType: "application/vnd.aiops.plugin.manifest.v1+yaml", Data: []byte(c.manifest)},
			ocitest.Layer{MediaType: "application/vnd.aiops.plugin.bundle.v1+yaml", Data: []byte("[]\n")},
			ocitest.Layer{MediaType: "application/vnd.aiops.plugin.content.v1.tar", Data: []byte(name), Annotations: map[string]string{"aiops.plugin.content.paths": c.paths}})
		if in, err := (Library{}).Inspect(t.Context(), def.SpecRequest{Caller: caller(), Spec: "oci://" + reg.Host + "/" + name + ":1"}); !errors.Is(err, def.ErrRefused) {
			t.Errorf("inspect of a %s that boot refuses = %+v, %v; want ErrRefused", name, in, err)
		}
	}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
