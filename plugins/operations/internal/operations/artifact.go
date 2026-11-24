// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"go.yaml.in/yaml/v3"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
)

// The artifact of ADR-047 decisions 1 to 3, with the prefix of its decision 20, and its
// attestation and signatures as referrers in the bundle form that cosign writes
// (ADR-020 decisions 2, 9, 24 and 25).
const (
	artifactType   = "application/vnd.aiops.plugin.v1"
	layerManifest  = "application/vnd.aiops.plugin.manifest.v1+yaml"
	layerBundle    = "application/vnd.aiops.plugin.bundle.v1+yaml"
	layerModule    = "application/vnd.aiops.plugin.module.v1+json"
	layerContent   = "application/vnd.aiops.plugin.content.v1.tar"
	layerFixtures  = "application/vnd.aiops.plugin.fixtures.v1.tar"
	layerSBOM      = "application/spdx+json"
	contentPaths   = "aiops.plugin.content.paths"
	attestationURI = "https://github.com/mutex-solutions/aiops-platform/attestation/conformance/v1"
	signURI        = "https://sigstore.dev/cosign/sign/v1"
	predicateKey   = "dev.sigstore.bundle.predicateType"
	statementV1    = "https://in-toto.io/Statement/v1"
)

func title(name string) map[string]string { return map[string]string{ocispec.AnnotationTitle: name} }

// say writes a progress line of the caller.
func say(c def.Caller, format string, a ...any) {
	if c.Progress != nil {
		fmt.Fprintf(c.Progress, format+"\n", a...)
	}
}

// Pack is plugin pack (ADR-047 decision 1; ADR-060 decisions 18 and 19): the suites
// of plugin check that the attestation records, then step 7 of plugin check into the
// layout, with the artifact tagged by its version and the statement, unsigned, as its
// referrer, and the gate at its entry. A denial refuses, naming the suite binaries
// where they did not run (ADR-060 decisions 23 and 46), and a refusal leaves no layout.
func (l Library) Pack(ctx context.Context, r def.PackRequest) (def.Written, error) {
	if err := errors.Join(absolute("the plugin directory", r.Dir), absolute("the layout", r.Out)); err != nil {
		return def.Written{}, err
	}
	entries, err := os.ReadDir(r.Out)
	switch {
	case err == nil && len(entries) > 0:
		return def.Written{}, refused("%s exists and is not empty: a layout holds one artifact (ADR-047 decision 1)", r.Out)
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return def.Written{}, err
	}
	existed := err == nil
	sum, diags, err := l.Box.ReadManifest(ctx, launch.ManifestRequest{Dir: r.Dir})
	if err != nil {
		return def.Written{}, refused("plugin.yaml of %s: %v", r.Dir, err)
	}
	for _, d := range diags {
		if !d.Warning {
			return def.Written{}, refused("plugin.yaml: %s", d.Text)
		}
	}
	if slices.Contains(sum.Runtimes, "remote") {
		return def.Written{}, refused("a remote plugin is packed from S2b, with the remote runtime (ADR-060 decision 29)")
	}
	c := &checkRun{l: l, ctx: ctx, r: def.CheckRequest{Caller: r.Caller, Dir: r.Dir}, sum: sum, code: len(sum.Runtimes) > 0}
	ran, ds, err := c.attested()
	switch {
	case err != nil:
		return def.Written{}, err
	case hasError(ds):
		return def.Written{}, refused("plugin check fails: %s", texts(ds))
	case !ran:
		say(r.Caller, "pack: %s, so the attestation names no suite revision and no scenario", notCarried)
	}
	v, err := c.seal(r.Out)
	var denied []string
	for _, c := range v.Clauses {
		if c.Outcome == launch.OutcomeDeny {
			denied = append(denied, fmt.Sprintf("check %d %s: %s", c.Check, c.Clause, c.Reason))
		}
	}
	if err == nil && len(denied) > 0 {
		if !ran { // the suites that write what check 10 reads did not run
			denied = append(denied, notCarried)
		}
		err = refused("the gate at plugin pack denies: %s", strings.Join(denied, "; "))
	}
	if err != nil {
		err = errors.Join(err, os.RemoveAll(r.Out))
		if existed {
			err = errors.Join(err, os.Mkdir(r.Out, 0o755))
		}
		return def.Written{}, err
	}
	return def.Written{Paths: []string{r.Out}}, nil
}

// packed is what a plugin directory packs to before its digest is known.
type packed struct {
	manifest, bundle []byte
	layers           []blob
}

// layers reads the layers of the plugin directory: plugin.yaml and bundle.yaml, the
// module of in-process code, the content and, for a pack, the fixtures as canonical
// tars, and the SBOM.
func (l Library) layers(ctx context.Context, dir string, sum launch.ManifestSummary) (packed, error) {
	var p packed
	var err error
	if p.manifest, err = os.ReadFile(filepath.Join(dir, "plugin.yaml")); err != nil {
		return p, refused("%v", err)
	}
	if p.bundle, err = os.ReadFile(filepath.Join(dir, "bundle.yaml")); err != nil {
		return p, refused("every artifact carries a bundle of its rows, all disabled (ADR-060 decision 19): %v", err)
	}
	var m struct{ License string }
	if err := yaml.Unmarshal(p.manifest, &m); err != nil {
		return p, refused("plugin.yaml: %v", err)
	}
	p.layers = []blob{{layerManifest, p.manifest, title("plugin.yaml")}, {layerBundle, p.bundle, title("bundle.yaml")}}
	doc := newSBOM(sum.ID, sum.Version, m.License)
	if slices.Contains(sum.Runtimes, "inproc") {
		mod, err := moduleOf(dir, "v"+sum.Version, doc)
		if err != nil {
			return p, err
		}
		p.layers = append(p.layers, blob{layerModule, mod, title("module.json")})
	}
	for _, t := range []struct {
		dir, mediaType string
		on             bool
	}{{"content", layerContent, true}, {"fixtures", layerFixtures, len(sum.Runtimes) == 0}} {
		files, err := regularFiles(filepath.Join(dir, t.dir))
		if err != nil {
			return p, refused("%s/: %v (ADR-047 decision 3)", t.dir, err)
		}
		if !t.on || files == nil {
			continue
		}
		b, paths := canonicalTar(files)
		ann := title(t.dir + ".tar")
		if t.dir == "content" {
			j, _ := json.Marshal(paths)
			ann[contentPaths] = string(j)
		}
		p.layers = append(p.layers, blob{t.mediaType, b, ann})
		for _, f := range paths {
			doc.file("./"+t.dir+"/"+f, files[f], m.License)
		}
	}
	sb, err := json.MarshalIndent(doc, "", "  ")
	p.layers = append(p.layers, blob{layerSBOM, sb, title("sbom.spdx.json")})
	return p, errors.Join(err, ctx.Err())
}

// seal is step 7 on the plugin directory of c (07-c4-plugin-sdk.md:1573; ADR-020
// decision 14): its layers packed into the OCI image layout out, the artifact tagged by
// its version with the statement of what c ran, whose subject is its digest, as the
// unsigned referrer; a signature of that digest and the statement signed with a
// throwaway key held in memory, each verified against the digest; and the verdict of
// the gate at the entry plugin check on the artifact (ADR-060 decision 17).
func (c *checkRun) seal(out string) (launch.Verdict, error) {
	a, err := c.l.layers(c.ctx, c.r.Dir, c.sum)
	if err != nil {
		return launch.Verdict{}, err
	}
	d, raw, err := manifestOf(artifactType, nil, nil, a.layers)
	if err != nil {
		return launch.Verdict{}, err
	}
	c.attestation = c.statement(d.Digest.Encoded())
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return launch.Verdict{}, err
	}
	for _, st := range [][]byte{signatureOf(c.sum.ID+"@"+c.sum.Version, d.Digest.Encoded()), c.attestation} {
		b, err := SignStatement(k, st)
		if err == nil {
			_, err = VerifyStatement(&k.PublicKey, b, mustDecode(d.Digest.Encoded()))
		}
		if err != nil {
			return launch.Verdict{}, refused("step 7: the signature and the attestation of %s, signed with a throwaway key, do not verify: %v", d.Digest, err)
		}
	}
	if err := writeLayout(c.ctx, out, d, raw, a.layers, c.sum.Version, c.attestation); err != nil {
		return launch.Verdict{}, err
	}
	der, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	q := launch.GateRequest{Entry: entryCheck, Manifest: a.manifest, Bundle: a.bundle, Attestation: c.attestation, Digest: d.Digest.String(), Signer: keyID(der)}
	if err == nil && c.r.Against != "" {
		q.Against, err = os.ReadFile(c.r.Against)
	}
	if err != nil {
		return launch.Verdict{}, err
	}
	return c.l.Box.Gate(c.ctx, q)
}

// The in-toto Statement v1 of a signature (ADR-020 decision 9); the attestation is
// def.Statement's.
type (
	statement struct {
		Type          string      `json:"_type"`
		Subject       []subjectOf `json:"subject"`
		PredicateType string      `json:"predicateType"`
		Predicate     any         `json:"predicate"`
	}
	subjectOf struct {
		Name   string            `json:"name"`
		Digest map[string]string `json:"digest"`
	}
)

// signatureOf is the statement that a signature of the artifact name at the hex digest
// signs: the predicate type of cosign sign, which plugin sign writes (ADR-020 decision 9).
func signatureOf(name, digest string) []byte {
	b, _ := json.Marshal(statement{Type: statementV1, PredicateType: signURI, Predicate: map[string]any{},
		Subject: []subjectOf{{Name: name, Digest: map[string]string{"sha256": digest}}}})
	return b
}

func mustDecode(s string) []byte {
	b, _ := hex.DecodeString(s)
	return b
}

// writeLayout writes the OCI image layout dir: the artifact tagged with version, and
// the statement as its referrer.
func writeLayout(ctx context.Context, dir string, d ocispec.Descriptor, raw []byte, layers []blob, version string, st []byte) error {
	s, err := oci.NewWithContext(ctx, dir)
	if err == nil {
		err = push(ctx, s, d, raw, layers)
	}
	if err == nil {
		err = s.Tag(ctx, d, version)
	}
	if err == nil {
		_, err = pushArtifact(ctx, s, statementType, &d, nil, blob{statementType, st, nil})
	}
	return err
}

// layoutArtifact returns the one manifest of artifactType that index.json of the
// layout dir names (ADR-047 decision 1).
func layoutArtifact(ctx context.Context, src content.Fetcher, dir string) (ocispec.Descriptor, error) {
	var index ocispec.Index
	b, err := os.ReadFile(filepath.Join(dir, ocispec.ImageIndexFile))
	if err == nil {
		err = json.Unmarshal(b, &index)
	}
	if err != nil {
		return ocispec.Descriptor{}, refused("%s is not the OCI image layout of an artifact: %v", dir, err)
	}
	var found []ocispec.Descriptor
	for _, d := range index.Manifests {
		if m, err := manifest(ctx, src, d); err == nil && m.ArtifactType == artifactType && m.Subject == nil {
			found = append(found, d)
		}
	}
	if len(found) != 1 {
		return ocispec.Descriptor{}, refused("index.json of %s names %d manifests of artifactType %s, not one (ADR-047 decision 1)", dir, len(found), artifactType)
	}
	return found[0], nil
}

// source is where a spec names an artifact: a repository or a layout.
type source interface {
	content.ReadOnlyGraphStorage
}

// open returns the source of spec and the descriptor of its artifact.
func open(ctx context.Context, c def.Caller, spec string) (source, ocispec.Descriptor, error) {
	if strings.HasPrefix(spec, "oci://") {
		repo, err := remoteRepo(c, spec)
		if err != nil {
			return nil, ocispec.Descriptor{}, err
		}
		d, err := resolve(ctx, repo)
		return repo, d, err
	}
	return openLayout(ctx, spec)
}

// openLayout returns the OCI image layout dir, read only, and its artifact.
func openLayout(ctx context.Context, dir string) (*oci.ReadOnlyStore, ocispec.Descriptor, error) {
	if err := absolute("the layout", dir); err != nil {
		return nil, ocispec.Descriptor{}, err
	}
	s, err := oci.NewFromFS(ctx, os.DirFS(dir))
	if err != nil {
		return nil, ocispec.Descriptor{}, refused("%s is not an OCI image layout: %v", dir, err)
	}
	d, err := layoutArtifact(ctx, s, dir)
	return s, d, err
}

// Inspect is plugin inspect: the image manifest, plugin.yaml and the bundle of the
// artifact, and no other blob; no signer is verified (ADR-047 decision 2; ADR-060
// decision 58, reading 2).
func (l Library) Inspect(ctx context.Context, r def.SpecRequest) (def.Inspected, error) {
	src, d, err := open(ctx, r.Caller, r.Spec)
	if err != nil {
		return def.Inspected{}, err
	}
	m, err := manifest(ctx, src, d)
	if err != nil {
		return def.Inspected{}, err
	}
	man, bun := layer(m, layerManifest), layer(m, layerBundle)
	if m.ArtifactType != artifactType || man == nil || bun == nil {
		return def.Inspected{}, refused("%s is not a plugin artifact with plugin.yaml and a bundle (ADR-047 decision 1)", r.Spec)
	}
	in := def.Inspected{Digest: d.Digest.String()}
	var items []struct {
		Insert []struct {
			ID, Plugin string
			Config     map[string]any
		}
	}
	pm, err := pluginOf(ctx, src, m)
	var bb []byte
	if err == nil {
		bb, err = fetch(ctx, src, *bun)
	}
	if err == nil {
		err = yaml.Unmarshal(bb, &items)
	}
	if c := layer(m, layerContent); err == nil && c != nil {
		err = json.Unmarshal([]byte(c.Annotations[contentPaths]), &in.Content)
	}
	if err != nil {
		return def.Inspected{}, refused("%s: %v", r.Spec, err)
	}
	in.ID, in.Version, in.Runtimes = pm.ID, pm.Version, pm.Runtimes
	for _, it := range items {
		for _, row := range it.Insert {
			in.Rows = append(in.Rows, def.Row{ID: row.ID, Plugin: row.Plugin, Config: row.Config})
		}
	}
	return in, nil
}

// bundles returns the bundle of each referrer of d in src whose predicate type is
// predicateType, in the bundle form of cosign (ADR-020 decision 2).
func bundles(ctx context.Context, src source, d ocispec.Descriptor, predicateType string) ([][]byte, error) {
	ms, err := referrers(ctx, src, d, bundleType)
	var out [][]byte
	for _, m := range ms {
		if l := layer(m.Manifest, bundleType); err == nil && l != nil && m.Annotations[predicateKey] == predicateType {
			var b []byte
			b, err = fetch(ctx, src, *l)
			out = append(out, b)
		}
	}
	return out, err
}

// pluginYAML is what the library reads of the plugin.yaml of an artifact.
type pluginYAML struct {
	ID, Version string
	Runtimes    []string
}

// pluginOf reads plugin.yaml of the artifact m in src.
func pluginOf(ctx context.Context, src content.Fetcher, m ocispec.Manifest) (pluginYAML, error) {
	var pm pluginYAML
	l := layer(m, layerManifest)
	if l == nil {
		return pm, refused("the artifact holds no plugin.yaml (ADR-047 decision 1)")
	}
	b, err := fetch(ctx, src, *l)
	if err == nil {
		err = yaml.Unmarshal(b, &pm)
	}
	return pm, err
}

// statementOf returns the subject name and predicate type of a statement.
func statementOf(st []byte) (name, predicateType string) {
	var s struct {
		PredicateType string
		Subject       []struct{ Name string }
	}
	if json.Unmarshal(st, &s) != nil || len(s.Subject) != 1 {
		return "", ""
	}
	return s.Subject[0].Name, s.PredicateType
}

// Sign is plugin sign: a signature of the artifact's digest and the attestation that
// pack recorded, each signed with the private key file as a bundle and written as a
// referrer in the bundle form of cosign; the unsigned statement is then removed
// (ADR-020 decisions 2, 9 and 24). Signing again, with another key, signs the
// statement that the attestation bundle carries. A statement whose one subject is not
// the artifact's digest, named <id>@<version> of its plugin.yaml, is refused.
func (l Library) Sign(ctx context.Context, r def.SignRequest) (def.Written, error) {
	if err := errors.Join(absolute("the layout", r.Out), absolute("the key file", r.Key)); err != nil {
		return def.Written{}, err
	}
	k, err := privateKey(r.Caller, r.Key)
	if err != nil {
		return def.Written{}, err
	}
	if _, err := os.Stat(filepath.Join(r.Out, ocispec.ImageIndexFile)); err != nil {
		return def.Written{}, refused("%s is not an OCI image layout: %v", r.Out, err)
	}
	s, err := oci.NewWithContext(ctx, r.Out)
	if err != nil {
		return def.Written{}, err
	}
	d, err := layoutArtifact(ctx, s, r.Out)
	if err != nil {
		return def.Written{}, err
	}
	unsigned, err := referrers(ctx, s, d, statementType)
	if err != nil {
		return def.Written{}, err
	}
	var st []byte
	if len(unsigned) > 0 && len(unsigned[0].Layers) == 1 {
		st, err = fetch(ctx, s, unsigned[0].Layers[0])
	} else if bs, berr := bundles(ctx, s, d, attestationURI); berr == nil && len(bs) > 0 {
		b, lerr := load(bs[0])
		if err = lerr; err == nil {
			st = b.GetDsseEnvelope().GetPayload()
		}
	}
	name, pt := statementOf(st)
	if err != nil || pt != attestationURI {
		return def.Written{}, refused("the layout %s holds no attestation that pack recorded: %v", r.Out, err)
	}
	m, err := manifest(ctx, s, d)
	var pm pluginYAML
	if err == nil {
		pm, err = pluginOf(ctx, s, m)
	}
	if got, serr := subject(st); err != nil || serr != nil || name != pm.ID+"@"+pm.Version || hex.EncodeToString(got) != d.Digest.Encoded() {
		return def.Written{}, refused("the attestation in %s names %s at %x, and the artifact is %s@%s at %s (ADR-020 decisions 9 and 24): %v",
			r.Out, name, got, pm.ID, pm.Version, d.Digest, errors.Join(err, serr))
	}
	sig := signatureOf(name, d.Digest.Encoded())
	for _, b := range []struct {
		predicateType string
		statement     []byte
	}{{signURI, sig}, {attestationURI, st}} {
		signed, err := SignStatement(k, b.statement)
		if err != nil {
			return def.Written{}, refused("%v", err)
		}
		if _, err := pushArtifact(ctx, s, bundleType, &d, map[string]string{predicateKey: b.predicateType, "dev.sigstore.bundle.content": "dsse-envelope"},
			blob{bundleType, signed, nil}); err != nil {
			return def.Written{}, err
		}
	}
	for _, u := range unsigned {
		if err := s.Delete(ctx, u.desc); err != nil {
			return def.Written{}, err
		}
	}
	return def.Written{Paths: []string{r.Out}}, nil
}

// Push is plugin push: the artifact of the layout and its referrers copied to the
// repository and tagged with its version; the spec by digest (ADR-047 decision 1).
func (l Library) Push(ctx context.Context, r def.PushRequest) (def.Pushed, error) {
	src, d, err := openLayout(ctx, r.Out)
	if err != nil {
		return def.Pushed{}, err
	}
	repo, err := remoteRepo(r.Caller, r.Registry)
	if err != nil {
		return def.Pushed{}, err
	}
	if repo.Reference.Reference != "" {
		return def.Pushed{}, usage("plugin push takes a repository, %s, with no tag or digest", r.Registry)
	}
	if _, err := oras.ExtendedCopy(ctx, src, d.Digest.String(), repo, d.Annotations[ocispec.AnnotationRefName], oras.DefaultExtendedCopyOptions); err != nil {
		return def.Pushed{}, err
	}
	return def.Pushed{Spec: "oci://" + repo.Reference.Registry + "/" + repo.Reference.Repository + "@" + d.Digest.String()}, nil
}

// covers reports whether the signer s covers the plugin id and every runtime of its
// manifest (ADR-020 decision 6).
func covers(s def.Signer, id string, runtimes []string) bool {
	owner, _, _ := strings.Cut(id, "/")
	return (slices.Contains(s.IDs, id) || slices.Contains(s.IDs, owner+"/*")) &&
		!slices.ContainsFunc(runtimes, func(rt string) bool { return !slices.Contains(s.Runtimes, rt) })
}

// Verify is plugin verify (ADR-047 decision 2; ADR-020 decision 14): the signature
// with each signer of trust.yaml, then the attestation with the key that verified it,
// before any other blob but plugin.yaml, which names the runtimes the signer must
// cover; an id that allowUnsigned lists may carry neither and is read with the
// statement that pack recorded. The suites then run as plugin check runs them, and the
// verdict passes when they pass.
func (l Library) Verify(ctx context.Context, r def.SpecRequest) (def.Verified, error) {
	h, err := l.home(r.Home)
	if err != nil {
		return def.Verified{}, err
	}
	t, err := readTrust(h.path("trust.yaml"))
	if err != nil {
		return def.Verified{}, err
	}
	src, d, err := open(ctx, r.Caller, r.Spec)
	if err != nil {
		return def.Verified{}, err
	}
	v := def.Verified{Digest: d.Digest.String()}
	sigs, err := bundles(ctx, src, d, signURI)
	if err != nil {
		return v, err
	}
	type verified struct {
		signer int    // the index of the entry of signers
		name   string // the subject name of the signed statement
	}
	var found []verified
	for i := range t.signers {
		for _, b := range sigs {
			st, err := VerifyStatement(t.signerKeys[i], b, mustDecode(d.Digest.Encoded()))
			if n, pt := statementOf(st); err == nil && pt == signURI {
				found = append(found, verified{i, n})
			}
		}
	}
	m, err := manifest(ctx, src, d)
	var pm pluginYAML
	if err == nil {
		pm, err = pluginOf(ctx, src, m)
	}
	if err != nil {
		return v, err
	}
	id := pm.ID + "@" + pm.Version
	var key *ecdsa.PublicKey
	i := slices.IndexFunc(found, func(f verified) bool { return f.name == id && covers(t.signers[f.signer], pm.ID, pm.Runtimes) })
	switch {
	case i >= 0:
		key, v.Signer = t.signerKeys[found[i].signer], t.signers[found[i].signer].KeyID
	case slices.Contains(t.allow, pm.ID):
		v.Signer = "unsigned"
	default:
		return v, refused("check 5: no signature of %s verifies with a key that trust.yaml lists for %s and the runtimes %v, and allowUnsigned does not list it", d.Digest, id, pm.Runtimes)
	}
	v.Steps = append(v.Steps, def.VerifyStep{Name: "signature", Outcome: "pass", Reason: "signer " + v.Signer})
	if err := attestation(ctx, src, d, key, id); err != nil {
		return v, err
	}
	v.Steps = append(v.Steps, def.VerifyStep{Name: "attestation", Outcome: "pass", Reason: "names " + id + " at " + d.Digest.String()})
	st, err := l.verifySuites(ctx, r.Caller, src, m, pm)
	v.Steps = append(v.Steps, st)
	v.Passed = st.Outcome == "pass"
	return v, err
}

// verifySuites is the step suites of plugin verify: the suites of plugin check, run as
// plugin check runs them on the pack that the artifact carries, its manifest, bundle
// and fixtures unpacked into a directory of its own (ADR-060 decisions 46 and 49). It
// fails where boot's strict reader refuses that manifest, or reads it as another plugin
// than pm, the one that the signature and the attestation name.
func (l Library) verifySuites(ctx context.Context, c def.Caller, src source, m ocispec.Manifest, pm pluginYAML) (def.VerifyStep, error) {
	st := def.VerifyStep{Name: "suites", Outcome: "not-built"}
	if len(pm.Runtimes) > 0 {
		st.Reason = "the suites of in-process code run on its module, which plugin verify fetches with the builder of ADR-044, not built yet"
		return st, nil
	}
	dir, err := os.MkdirTemp("", "aiops-verify-")
	if err != nil {
		return st, err
	}
	defer os.RemoveAll(dir)
	for _, d := range m.Layers {
		// No other layer is fetched: not the SBOM, which install never fetches either, nor
		// a type this reader does not know (ADR-047 decisions 1 and 2).
		name := map[string]string{layerManifest: "plugin.yaml", layerBundle: "bundle.yaml", layerFixtures: "fixtures"}[d.MediaType]
		if name == "" {
			continue
		}
		b, err := fetch(ctx, src, d)
		if err == nil && name == "fixtures" {
			err = untar(b, filepath.Join(dir, name))
		} else if err == nil {
			err = os.WriteFile(filepath.Join(dir, name), b, 0o600)
		}
		if err != nil {
			return st, err
		}
	}
	sum, ds, err := l.Box.ReadManifest(ctx, launch.ManifestRequest{Dir: dir})
	switch {
	case err != nil:
		return st, refused("plugin.yaml of the artifact: %v", err)
	case hasError(ds):
		st.Outcome, st.Reason = "fail", texts(ds)
		return st, nil
	case sum.ID != pm.ID || sum.Version != pm.Version:
		st.Outcome, st.Reason = "fail", fmt.Sprintf("boot's reader reads plugin.yaml as %s@%s, and the artifact is %s@%s", sum.ID, sum.Version, pm.ID, pm.Version)
		return st, nil
	}
	ran, ds, err := (&checkRun{l: l, ctx: ctx, r: def.CheckRequest{Caller: c, Dir: dir}, sum: sum}).attested()
	switch {
	case err != nil:
	case hasError(ds):
		st.Outcome, st.Reason = "fail", texts(ds)
	case !ran:
		st.Reason = notCarried
	default:
		st.Outcome, st.Reason = "pass", "step 5 and the echo scenario of plugin check"
	}
	return st, err
}

// attestation checks the attestation of d: with key, the bundle that verifies, and
// with none the statement that pack recorded; its subject must be id at d (check 10).
func attestation(ctx context.Context, src source, d ocispec.Descriptor, key *ecdsa.PublicKey, id string) error {
	var sts [][]byte
	if key == nil {
		ms, err := referrers(ctx, src, d, statementType)
		for _, m := range ms {
			if len(m.Layers) == 1 && err == nil {
				var st []byte
				st, err = fetch(ctx, src, m.Layers[0])
				sts = append(sts, st)
			}
		}
		if err != nil {
			return err
		}
	} else {
		bs, err := bundles(ctx, src, d, attestationURI)
		if err != nil {
			return err
		}
		for _, b := range bs {
			if st, err := VerifyStatement(key, b, mustDecode(d.Digest.Encoded())); err == nil {
				sts = append(sts, st)
			}
		}
	}
	for _, st := range sts {
		name, pt := statementOf(st)
		got, err := subject(st)
		if pt == attestationURI && name == id && err == nil && hex.EncodeToString(got) == d.Digest.Encoded() {
			return nil
		}
		if pt == attestationURI {
			return refused("check 10: the attestation names another subject, %s, and the artifact is %s at %s", name, id, d.Digest)
		}
	}
	return refused("check 10: the conformance attestation of %s is missing or does not verify with the signer's key", id)
}
