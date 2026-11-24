// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
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
	dir := t.TempDir()
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

// packBox is the launcher's toolbox of these tests: ReadManifest answers sum, the gate
// answers the clauses of deny and records each request, and Scenario answers scenario,
// or that it is not carried when it is nil, as no suite is before package check lands.
type packBox struct {
	launch.NoTools
	sum      launch.ManifestSummary
	deny     []launch.Clause
	scenario *launch.ScenarioReport
	mu       sync.Mutex
	gates    []launch.GateRequest
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
	return b.sum, nil, nil
}

func (b *packBox) Gate(_ context.Context, q launch.GateRequest) (launch.Verdict, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.gates = append(b.gates, q)
	return launch.Verdict{Entry: q.Entry, Clauses: b.deny}, nil
}

func (b *packBox) Scenario(ctx context.Context, q launch.ScenarioRequest) (launch.ScenarioReport, error) {
	if b.scenario == nil {
		return b.NoTools.Scenario(ctx, q)
	}
	return *b.scenario, nil
}

func packSummary() launch.ManifestSummary {
	return launch.ManifestSummary{ID: "acme/connector-x", Version: "0.1.0", Definitions: map[string][]string{"echoers": {"0.1"}}}
}

// layout reads the OCI image layout dir: the descriptor of its one manifest in
// index.json, the manifest, and its blobs by digest.
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
	if err != nil || len(index.Manifests) != 1 {
		t.Fatalf("index.json %s, %v; want one manifest", b, err)
	}
	var m ocispec.Manifest
	if err := json.Unmarshal(blob(index.Manifests[0]), &m); err != nil {
		t.Fatal(err)
	}
	return index.Manifests[0], m, blob
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

// The module layer of in-process code holds the path of go.mod, the version of the
// manifest and the sum of the module zip of the directory, as the Go toolchain writes
// it in go.sum; its SBOM lists the module and each requirement of go.mod with its sum
// in go.sum (ADR-047 decision 1). It carries no fixtures layer.
func TestPackOfInProcessCode(t *testing.T) {
	sum := packSummary()
	sum.Runtimes = []string{"inproc"}
	l := Library{Box: &packBox{sum: sum}}
	dir := pluginDir(t, map[string]string{
		"plugin.yaml": strings.Replace(packManifest, "runtimes: []", "runtimes: [inproc]", 1),
		"bundle.yaml": "[]\n",
		"go.mod":      "module example.test/echo\n\ngo 1.26\n\nrequire example.test/dep v1.0.0\n",
		"go.sum":      "example.test/dep v1.0.0 h1:Tlr2Kx8bGO0Ks3JZ4dCAxvsTm5dcDT4QeDdP6Usk9ks=\nexample.test/dep v1.0.0/go.mod h1:1+u3SyWnZnn7sAzwYwRkvCF2GZwPmMd3FQ34Zm6iaNE=\n",
		"echo.go":     "package echo\n",
		"fixtures/x":  "x\n",
	})
	out := filepath.Join(t.TempDir(), "artifact")
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
		Packages []struct{ Name, VersionInfo, LicenseConcluded string }
	}
	if err := json.Unmarshal(blob(m.Layers[3]), &sbom); err != nil || len(sbom.Packages) != 3 || sbom.Packages[1].Name != "example.test/echo" ||
		sbom.Packages[2].Name != "example.test/dep" || sbom.Packages[2].VersionInfo != "v1.0.0" || sbom.Packages[2].LicenseConcluded != "NOASSERTION" {
		t.Errorf("SBOM %+v, %v", sbom, err)
	}
}

// echoSum is the h1 sum of the module example.test/echo at v0.1.0 of
// TestPackOfInProcessCode, as go mod download would record it.
const echoSum = "h1:CuDjptRJ5bw/sRPs2/bfnr6IGglsof5GyvlW0ys9gJs="

// Pack refuses, and leaves no layout, where the gate denies a clause, where a content
// entry is a link, where the manifest lists a remote runtime, and where the out
// directory holds a file. A check 10 denial where no scenario ran is not evaluated,
// since check 10 reads what the scenario records.
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
	if _, err := (Library{Box: b}).Pack(ctx, def.PackRequest{Caller: caller(), Dir: dir, Out: out}); err != nil {
		t.Errorf("a denial of check 10 with no scenario run: %v", err)
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
	other := filepath.Join(t.TempDir(), "other")
	if err := os.CopyFS(other, os.DirFS(out)); err != nil {
		t.Fatal(err)
	}
	for _, r := range layoutReferrers(t, other, desc, "application/vnd.dev.sigstore.bundle.v0.3+json") {
		if r.Annotations["dev.sigstore.bundle.predicateType"] != "https://sigstore.dev/cosign/sign/v1" {
			if err := os.WriteFile(filepath.Join(other, "blobs", "sha256", r.Layers[0].Digest.Encoded()), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := l.Verify(t.Context(), def.SpecRequest{Caller: caller(), Spec: other, Home: home}); !errors.Is(err, def.ErrRefused) || !strings.Contains(err.Error(), "check 10") {
		t.Errorf("an attestation of another subject: %v, want ErrRefused naming check 10", err)
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

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
