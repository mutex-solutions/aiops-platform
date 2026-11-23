// SPDX-License-Identifier: Apache-2.0

// Package trusted writes into a test home what dist use writes once it has verified a
// distribution manifest, so that a binary built inside the repository boots on it
// (ADR-060 decision 28; ADR-020 decisions 10, 12 and 13): trust.yaml with a throwaway
// key under distributions, the record of the manifest's digest with that key as its
// signer, and verified/current. It also writes what plugin install keeps and records of
// an in-process artifact (Install). Each record holds the files of ADR-020 decision 10,
// in the format that boot reads (boot.Trust, boot.Record, boot.ReadCurrent,
// boot.ReadKept).
//
// It signs nothing: the key pair is made in the test and dropped, and boot verifies no
// signature, so each Sigstore bundle of a record (manifest.sigstore.json,
// signature.sigstore.json and attestation.sigstore.json) is a placeholder that says so.
// The signing code of dist sign, dist use and plugin install replaces this package once
// that code exists, so that the harness signs and records as those commands do. It is
// test code: no shipped distribution links it.
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
	"os"
	"path/filepath"
	"strings"

	"github.com/mutex-solutions/aiops-platform/boot"
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

// placeholder stands where a record holds a Sigstore bundle: nothing is signed.
var placeholder = []byte(`{"placeholder": "not a Sigstore bundle: the test harness signs nothing (ADR-020 decision 10)"}` + "\n")

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

// Files returns the files, by slash path under the home, by which a home trusts the
// distribution manifest: trust.yaml that lists a throwaway ECDSA P-256 key under
// distributions, verified/sha256-<hex>/record.yaml of kind distribution whose signer
// is that key, beside it the manifest as dist use keeps it and the placeholder of its
// bundle, and verified/current.
func Files(manifest []byte) (map[string][]byte, error) {
	pub, id, err := newKey()
	if err != nil {
		return nil, err
	}
	d := digest(manifest)
	dir := "verified/" + strings.Replace(d, ":", "-", 1)
	return map[string][]byte{
		"trust.yaml":                    []byte("distributions:\n  - key: |\n      " + pub + "\n"),
		dir + "/record.yaml":            fmt.Appendf(nil, "kind: distribution\ndigest: %s\nsigner: %s\n", d, id),
		dir + "/manifest":               manifest,
		dir + "/manifest.sigstore.json": placeholder,
		"verified/current":              []byte(d + "\n"),
	}, nil
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

// Write writes Files into the directory home.
func Write(home string, manifest []byte) error {
	files, err := Files(manifest)
	if err != nil {
		return err
	}
	return write(home, files)
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
