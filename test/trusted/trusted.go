// SPDX-License-Identifier: Apache-2.0

// Package trusted makes a test home trust a distribution manifest as a site does, so
// that a binary built inside the repository boots on it (ADR-060 decision 28; ADR-020
// decisions 1, 10, 12 and 13). Write mounts the operations row on a kernel of the test
// and calls the key it provides, as every consumer does: key generate makes a
// throwaway key pair inside the test, dist sign signs the manifest with it as the
// referrer of an image in a registry inside the test, and the image, trust add
// lists the key under distributions, and dist use verifies the signature and writes the
// record of the manifest's digest and verified/current. What it writes is what boot
// reads (boot.ReadTrust, boot.ReadRecord, boot.ReadCurrent). No key is committed.
//
// Install writes what plugin install keeps and records of an in-process artifact, in
// the format that boot reads (boot.ReadKept), by hand: until plugin install lands its
// Sigstore bundles (signature.sigstore.json and attestation.sigstore.json) are
// placeholders that say so, and boot verifies no signature. It is test code: no
// shipped distribution links it.
package trusted

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/plugins/operations"
	launchfake "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0/fake"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/ocitest"
)

// newKey returns a throwaway ECDSA P-256 public key as a PEM block, indented for a key
// field of trust.yaml, and its key id, the base64 of the SHA-256 of its DER form.
func newKey() (string, string, error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	der, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		return "", "", err
	}
	id := sha256.Sum256(der)
	pub := strings.TrimSuffix(string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), "\n")
	return strings.ReplaceAll(pub, "\n", "\n      "), base64.StdEncoding.EncodeToString(id[:]), nil
}

// placeholder stands where the record of an artifact holds a Sigstore bundle until
// plugin install lands: nothing is signed.
var placeholder = []byte(`{"placeholder": "not a Sigstore bundle: Install signs nothing until plugin install lands (ADR-020 decision 10)"}` + "\n")

// bundleMedia is the media type of a Sigstore bundle, the layer that holds the bundle
// of a distribution manifest in its referrer (ADR-020 decision 2).
const bundleMedia = "application/vnd.dev.sigstore.bundle.v0.3+json"

// digest returns "sha256:" and the hex of the SHA-256 of b.
func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// statement writes the conformance attestation of the plugin manifest m whose subject
// digest is the hex subject and whose definitions are a JSON object (ADR-020 decision
// 9).
func statement(m boot.Manifest, subject, definitions string) string {
	return fmt.Sprintf(`{"_type": "https://in-toto.io/Statement/v1", "subject": [{"name": "%s@%s", "digest": {"sha256": "%s"}}], "predicateType": %q, "predicate": {"definitions": %s}}`,
		m.ID, m.Version, subject, boot.AttestationType, definitions)
}

// Files returns the files, by slash path under the home, that Write writes into a
// fresh home: trust.yaml, verified/sha256-<hex>/ with record.yaml, the manifest and its
// bundle, verified/current, and write.lock.
func Files(tb testing.TB, manifest []byte) map[string][]byte {
	tb.Helper()
	home := filepath.Join(tb.TempDir(), "home")
	Write(tb, home, manifest)
	files := map[string][]byte{}
	err := filepath.WalkDir(home, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		rel, _ := filepath.Rel(home, p)
		files[filepath.ToSlash(rel)] = b
		return err
	})
	if err != nil {
		tb.Fatal(err)
	}
	return files
}

// signer is the throwaway key pair of the test process and the bundles it signed, one
// per manifest, and the referrer that signs the image, whose digest every call shares,
// with its bundle, held in memory. Key generate and dist sign run once each: the password
// of the private key is stretched with scrypt, which takes seconds under the race
// detector, and every test home trusts the same key. Each caller gets the files in a
// temporary directory of its own test, which the testing package removes.
var signer struct {
	sync.Mutex
	key, pub        []byte
	bundles         map[string][]byte // the digest of a manifest to the bundle of its signature
	image, imageSig []byte            // the referrer that signs the image, and its bundle
}

const password = "a throwaway key's password"

func caller() def.Caller {
	return def.Caller{Env: func(n string) (string, bool) { return password, n == def.KeyPasswordVar }}
}

// mount mounts the operations row on a kernel of tb and returns the key it serves.
func mount(tb testing.TB) def.Operations {
	tb.Helper()
	root := kerneltest.New(tb)
	var ops def.Operations
	_, launcher := launchfake.New() // the operations row needs profileContext; Write calls no part of its toolbox
	kerneltest.Mount(root, launcher, operations.Plugin, kernel.Define(kernel.Plugin{ID: "aiops-test/trusted",
		Inject: []kernel.Dep{kernel.Need(def.Key)}, Apply: func(c *kernel.Context, _ any) error { ops = def.Key.Of(c); return nil }}))
	return ops
}

// image pushes an image with manifest as its distribution referrer, signed with the
// key of the process, to a registry inside tb, and returns the image's reference and
// the public key file, in a temporary directory of tb.
func image(tb testing.TB, ops def.Operations, manifest []byte) (string, string) {
	tb.Helper()
	signer.Lock()
	defer signer.Unlock()
	dir := tb.TempDir()
	key, pub := filepath.Join(dir, "dist.key"), filepath.Join(dir, "dist.pub")
	if signer.pub == nil {
		if _, err := ops.GenerateKey(tb.Context(), def.KeyRequest{Caller: caller(), Prefix: filepath.Join(dir, "dist")}); err != nil {
			tb.Fatal(err)
		}
		signer.key, signer.pub, signer.bundles = readFile(tb, key), readFile(tb, pub), map[string][]byte{}
	} else if err := write(dir, map[string][]byte{"dist.key": signer.key, "dist.pub": signer.pub}); err != nil {
		tb.Fatal(err)
	}
	reg := ocitest.New(tb, ocitest.Options{})
	d, ref := digest(manifest), reg.Host+"/dist:1"
	img := reg.Artifact("dist", "1", "application/vnd.example.image.v1", nil)
	layers := []ocitest.Layer{{MediaType: def.DistributionLayer, Data: manifest}}
	if b, ok := signer.bundles[d]; ok {
		layers = append(layers, ocitest.Layer{MediaType: bundleMedia, Data: b})
	}
	reg.Artifact("dist", "", def.DistributionType, &img, layers...)
	if len(layers) > 1 {
		reg.Blob("dist", bundleMedia, signer.imageSig)
		reg.Manifest("dist", "", ocitest.ManifestType, signer.image)
		return ref, pub
	}
	w, err := ops.SignDist(tb.Context(), def.SignDistRequest{Caller: caller(), Image: ref, Key: key})
	if err != nil || len(w.Paths) != 2 {
		tb.Fatalf("dist sign: %v, %v", w, err)
	}
	var bundles [2][]byte
	for i, p := range w.Paths {
		_, referrer, _ := strings.Cut(p, "@")
		raw, _ := reg.Fetch("dist", referrer)
		var m struct{ Layers []struct{ Digest string } }
		if err := json.Unmarshal(raw, &m); err != nil || len(m.Layers) == 0 {
			tb.Fatalf("the referrer that dist sign pushed: %s, %v", raw, err)
		}
		bundles[i], _ = reg.Fetch("dist", m.Layers[len(m.Layers)-1].Digest)
		signer.image = raw
	}
	signer.bundles[d], signer.imageSig = bundles[0], bundles[1]
	return ref, pub
}

// Sign returns the bundle of a signature of manifest by the throwaway key of the
// process, as dist sign writes it, in the form of cosign sign-blob, and the public key
// file: what a release pipeline signs the manifest of a release with, and the release
// key its second phase embeds (ADR-020 decision 13; ADR-060 decision 28).
func Sign(tb testing.TB, manifest []byte) (bundle, pub []byte) {
	tb.Helper()
	image(tb, mount(tb), manifest)
	signer.Lock()
	defer signer.Unlock()
	return signer.bundles[digest(manifest)], signer.pub
}

func readFile(tb testing.TB, p string) []byte {
	tb.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		tb.Fatal(err)
	}
	return b
}

// With returns the distribution manifest signed with one more plugin in its module
// table, as the builder would write it for a binary that links the plugin: the import
// path of its package, its manifest and a statement whose subject is the manifest and
// that names definitions, a JSON object (ADR-020 decision 9). A test that adds a plugin
// to a module table adds it here too.
func With(signed []byte, pkg string, manifest []byte, definitions string) ([]byte, error) {
	m, err := boot.ReadManifest("plugin.yaml", manifest, boot.ManifestLenient)
	if err != nil {
		return nil, err
	}
	st := statement(m, strings.TrimPrefix(digest(manifest), "sha256:"), definitions)
	quote := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	entry := fmt.Sprintf("plugins:\n  %s:\n    package: %s\n    manifest: %s\n    attestation: %s\n", m.ID, pkg, quote(string(manifest)), quote(st))
	return []byte(strings.Replace(string(signed), "plugins:\n", entry, 1)), nil
}

// Rebind returns the distribution manifest signed with its bindings replaced by
// bindings, the YAML of the field in flow style.
func Rebind(signed []byte, bindings string) []byte {
	s := string(signed)
	i, j := strings.Index(s, "\nbindings:"), strings.Index(s, "\nacceptLicenses:")
	return []byte(s[:i] + "\nbindings: " + bindings + s[j:])
}

// Write makes home trust manifest through the operations row: dist sign of the
// manifest with the throwaway key, trust add of the key as a distribution root, and
// dist use, which writes the record and verified/current (ADR-060 decision 28).
func Write(tb testing.TB, home string, manifest []byte) {
	tb.Helper()
	ops := mount(tb)
	img, pub := image(tb, ops, manifest)
	if _, err := ops.AddTrust(tb.Context(), def.TrustRequest{Caller: caller(), Home: home, KeyFile: pub, Distribution: true}); err != nil {
		tb.Fatal(err)
	}
	if _, err := ops.UseDist(tb.Context(), def.UseRequest{Caller: caller(), Home: home, Image: img}); err != nil {
		tb.Fatal(err)
	}
}

func write(home string, files map[string][]byte) error {
	for name, data := range files {
		p := filepath.Join(home, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Artifact is an in-process artifact of a plugin of this repository as plugin install
// pulls it: its generated manifest, its bundle and the definitions its statement
// names, a JSON object. Unsigned and Subject are the defects of a fault of the failure
// matrix (ADR-060 decision 31): no verified signer, and a statement whose subject is
// Subject, the hex of another digest.
type Artifact struct {
	Manifest, Bundle     []byte
	Definitions, Subject string
	Unsigned             bool
}

// Install writes into home, once per home and after Write, what plugin install keeps
// and records of a (ADR-047 decision 18; ADR-020 decision 10): bundles/<id>/ with the
// OCI image manifest artifact.json, plugin.yaml and bundle.yaml; verified/sha256-<hex>/
// of the digest of artifact.json with record.yaml, attestation.json and the placeholders
// of the signature and attestation bundles; and in trust.yaml a throwaway key under
// signers, for the id and the runtime inproc, which signed it. The record pins the
// repository's root module by its path, as the signed manifest of a build inside the
// repository lists it (ADR-020 decision 28). In-process code has no entry in
// plugins.lock.
func Install(home string, a Artifact) error {
	m, err := boot.ReadManifest("plugin.yaml", a.Manifest, boot.ManifestLenient)
	if err != nil {
		return err
	}
	layer := func(media string, b []byte) string {
		return fmt.Sprintf(`{"mediaType": %q, "digest": %q, "size": %d}`, media, digest(b), len(b))
	}
	art := fmt.Appendf(nil, `{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json", "artifactType": "application/vnd.aiops.plugin.v1", "config": %s, "layers": [%s, %s]}`,
		layer("application/vnd.oci.empty.v1+json", []byte("{}")), layer("application/vnd.aiops.plugin.manifest.v1+yaml", a.Manifest),
		layer("application/vnd.aiops.plugin.bundle.v1+yaml", a.Bundle))
	d := digest(art)
	pub, signer, err := newKey()
	if err != nil {
		return err
	}
	if a.Unsigned {
		signer = boot.Unsigned
	}
	subject := a.Subject
	if subject == "" {
		subject = strings.TrimPrefix(d, "sha256:")
	}
	trust, err := os.ReadFile(filepath.Join(home, "trust.yaml"))
	if err != nil {
		return err
	}
	dir, kept := "verified/"+strings.Replace(d, ":", "-", 1), "bundles/"+m.ID
	return write(home, map[string][]byte{
		kept + "/artifact.json": art, kept + "/plugin.yaml": a.Manifest, kept + "/bundle.yaml": a.Bundle,
		dir + "/record.yaml": fmt.Appendf(nil, "kind: artifact\ndigest: %s\nsigner: %s\nid: %s\nversion: %s\nruntimes: [inproc]\nmodule: {path: github.com/mutex-solutions/aiops-platform}\n",
			d, signer, m.ID, m.Version),
		dir + "/attestation.json":        []byte(statement(m, subject, a.Definitions)),
		dir + "/signature.sigstore.json": placeholder, dir + "/attestation.sigstore.json": placeholder,
		"trust.yaml": fmt.Appendf(trust, "signers:\n  - key: |\n      %s\n    ids: [%s]\n    runtimes: [inproc]\n", pub, m.ID),
	})
}
