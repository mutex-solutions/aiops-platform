// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/testkit/ocitest"
)

// The media types as text, since no package under cmd/ imports the operations
// definition (ADR-061 decision 28).
const (
	distType   = "application/vnd.aiops.distribution.v1"
	distLayer  = "application/vnd.aiops.distribution.manifest.v1+yaml"
	bundleType = "application/vnd.dev.sigstore.bundle.v0.3+json"
	signType   = "https://sigstore.dev/cosign/sign/v1"
	// noServices is a cosign signing config that names no service, so cosign signs in
	// key mode with no transparency-log entry and no timestamp (ADR-020 decision 2).
	noServices = `{"mediaType": "application/vnd.dev.sigstore.signingconfig.v0.2+json"}`
)

// cosign and aiopsctl verify each other's signatures in key mode, both ways (ADR-060
// decision 36; ADR-020 decisions 2 and 4; 07:75, :1852): cosign verifies the artifact
// and the distribution manifest that aiopsctl signs, with the public key file of key
// generate; and aiopsctl verifies an artifact and a distribution that cosign signs, as
// the release pipeline signs them, with the key pair of cosign generate-key-pair. The
// registry is the case's own, on the loopback interface, and the keys are made inside
// the case. It needs a cosign binary, which AIOPS_COSIGN names: make cosign builds the
// pinned one and runs this case, as the CI job cosign does.
func TestCosignInterop(t *testing.T) {
	bin := os.Getenv("AIOPS_COSIGN")
	if bin == "" {
		t.Skip("AIOPS_COSIGN names no cosign binary: make cosign builds the pinned cosign and runs this case, as the CI job cosign does")
	}
	dir, home := t.TempDir(), t.TempDir()
	if err := writeFiles(dir, packFiles); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "signing.json"), []byte(noServices), 0o644); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"PWD": dir, "AIOPS_HOME": home, "AIOPS_KEY_PASSWORD": "test"}
	ctl := func(args ...string) result {
		t.Helper()
		r := do(t, os.DirFS("/"), env, args...)
		if r.code != 0 {
			t.Fatalf("aiopsctl %q: exit %d, stdout %q; stderr:\n%s", args, r.code, r.out, r.errOut)
		}
		return r
	}
	cosign := func(args ...string) error {
		cmd := exec.CommandContext(t.Context(), bin, args...)
		cmd.Dir = dir
		cmd.Env = []string{"COSIGN_PASSWORD=test", "HOME=" + dir, "DOCKER_CONFIG=" + dir, "TUF_ROOT=" + filepath.Join(dir, "tuf")}
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("cosign %q: %v\n%s", args, err, out)
		}
		return nil
	}
	reg := ocitest.New(t, ocitest.Options{})

	// cosign verifies what aiopsctl signs.
	ctl("key", "generate", "author")
	ctl("plugin", "pack")
	ctl("plugin", "sign", "--key", "author.key")
	art := strings.TrimPrefix(strings.TrimSpace(ctl("plugin", "push", reg.Host+"/connector-x").out), "oci://")
	site := image(reg, "site")
	manifest := []byte("distVersion: 1\n# a site\n")
	reg.Artifact("site", "", distType, &site, ocitest.Layer{MediaType: distLayer, Data: manifest})
	ctl("dist", "sign", "oci://"+reg.Host+"/site:1", "--key", "author.key")
	var bundle []byte
	for _, r := range reg.Referrers("site", site.Digest) {
		if layers := layersOf(t, reg, "site", r.Digest); r.ArtifactType == distType && len(layers) == 2 {
			bundle, _ = reg.Fetch("site", layers[1])
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest"), manifest, 0o644); err != nil || os.WriteFile(filepath.Join(dir, "manifest.sigstore.json"), bundle, 0o644) != nil {
		t.Fatalf("the manifest and the bundle that dist sign pushed: %v, %d bytes", err, len(bundle))
	}
	if err := cosign("generate-key-pair", "--output-key-prefix", "cosign"); err != nil {
		t.Fatal(err)
	}
	verify := func(key string) [][]string {
		return [][]string{
			{"verify", "--key", key, "--insecure-ignore-tlog=true", art},
			{"verify", "--key", key, "--insecure-ignore-tlog=true", reg.Host + "/site@" + site.Digest},
			{"verify-blob", "--key", key, "--bundle", "manifest.sigstore.json", "--insecure-ignore-tlog=true", "manifest"},
		}
	}
	for _, args := range verify("author.pub") {
		if err := cosign(args...); err != nil {
			t.Errorf("cosign does not verify what aiopsctl signed: %v", err)
		}
	}
	for _, args := range verify("cosign.pub") {
		if err := cosign(args...); err == nil {
			t.Errorf("cosign %q verifies with a key that signed nothing", args)
		}
	}

	// aiopsctl verifies what cosign signs. Only aiopsctl writes the attestation (ADR-020
	// decision 2): plugin sign writes it with cosign's key, and the signature it writes
	// beside it is deleted, so the signature plugin verify finds is cosign's.
	ctl("trust", "add", "cosign.pub", "--ids", "acme/*")
	ctl("trust", "add", "cosign.pub", "--distribution")
	ctl("plugin", "pack", "--out", "cosigned")
	ctl("plugin", "sign", "--key", "cosign.key", "--out", "cosigned")
	spec := strings.TrimSpace(ctl("plugin", "push", reg.Host+"/cosigned", "--out", "cosigned").out)
	digest := spec[strings.LastIndex(spec, "@")+1:]
	for _, r := range reg.Referrers("cosigned", digest) {
		if r.ArtifactType == bundleType && r.Annotations["dev.sigstore.bundle.predicateType"] == signType {
			remove(t, reg, "cosigned", r.Digest)
		}
	}
	if err := cosign("sign", "--key", "cosign.key", "--signing-config", "signing.json", "-y", reg.Host+"/cosigned@"+digest); err != nil {
		t.Fatal(err)
	}
	id := keyID(t, filepath.Join(dir, "cosign.pub"))
	r := do(t, os.DirFS("/"), env, "plugin", "verify", spec)
	if !strings.Contains(r.out, "step signature: pass: signer "+id+"\n") || !strings.Contains(r.out, "step attestation: pass") {
		t.Errorf("plugin verify of an artifact that cosign signed: exit %d, stdout %q; stderr:\n%s", r.code, r.out, r.errOut)
	}
	release := image(reg, "release")
	manifest = []byte("distVersion: 1\n# a release\n")
	if err := os.WriteFile(filepath.Join(dir, "release"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cosign("sign-blob", "--key", "cosign.key", "--signing-config", "signing.json", "--bundle", "release.sigstore.json", "-y", "release"); err != nil {
		t.Fatal(err)
	}
	bundle, err := os.ReadFile(filepath.Join(dir, "release.sigstore.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.Artifact("release", "", distType, &release, ocitest.Layer{MediaType: distLayer, Data: manifest},
		ocitest.Layer{MediaType: bundleType, Data: bundle})
	if err := cosign("sign", "--key", "cosign.key", "--signing-config", "signing.json", "-y", reg.Host+"/release@"+release.Digest); err != nil {
		t.Fatal(err)
	}
	ctl("dist", "use", reg.Host+"/release:1")
	rec, err := os.ReadFile(filepath.Join(home, "verified", "sha256-"+strings.TrimPrefix(ocitest.Digest(manifest), "sha256:"), "record.yaml"))
	if err != nil || !strings.Contains(string(rec), "signer: "+id+"\n") {
		t.Errorf("dist use of a distribution that cosign signed recorded %q, %v; want the signer %s", rec, err, id)
	}
}

// image pushes an image of one layer to the repository name, tagged 1.
func image(reg *ocitest.Registry, name string) ocitest.Descriptor {
	return reg.Artifact(name, "1", "application/vnd.example.image.v1", nil, ocitest.Layer{MediaType: "application/vnd.example.layer.v1", Data: []byte(name)})
}

// layersOf returns the digests of the layers of the manifest digest of the repository
// name.
func layersOf(t *testing.T, reg *ocitest.Registry, name, digest string) []string {
	t.Helper()
	raw, _ := reg.Fetch(name, digest)
	var m struct{ Layers []struct{ Digest string } }
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range m.Layers {
		out = append(out, l.Digest)
	}
	return out
}

// remove deletes the manifest digest of the repository name.
func remove(t *testing.T, reg *ocitest.Registry, name, digest string) {
	t.Helper()
	q, err := http.NewRequestWithContext(t.Context(), http.MethodDelete, "http://"+reg.Host+"/v2/"+name+"/manifests/"+digest, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(q)
	if err != nil || resp.Body.Close() != nil || resp.StatusCode != http.StatusAccepted {
		t.Fatalf("delete %s@%s: %v, %v", name, digest, resp, err)
	}
}

// keyID returns the id of the public key file p: the base64 of the SHA-256 of its DER
// key, the hint both tools write (ADR-020 decision 4).
func keyID(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	blk, _ := pem.Decode(b)
	if err != nil || blk == nil {
		t.Fatalf("%s: %v", p, err)
	}
	sum := sha256.Sum256(blk.Bytes)
	return base64.StdEncoding.EncodeToString(sum[:])
}
