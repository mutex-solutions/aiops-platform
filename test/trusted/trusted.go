// SPDX-License-Identifier: Apache-2.0

// Package trusted writes into a test home what dist use writes once it has verified a
// distribution manifest, so that a binary built inside the repository boots on it
// (ADR-060 decision 28; ADR-020 decisions 10, 12 and 13): trust.yaml with a throwaway
// key under distributions, the record of the manifest's digest with that key as its
// signer, and verified/current. The files have the exact format that boot reads
// (boot.Trust, boot.Record, boot.ReadCurrent).
//
// It signs nothing: the key pair is made in the test and dropped, and boot verifies no
// signature. The signing code of dist sign and dist use replaces it once that code
// exists, so that the harness signs the manifest and records it as dist use does.
// It is test code: no shipped distribution links it.
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

// Files returns the files, by slash path under the home, by which a home trusts the
// distribution manifest: trust.yaml that lists a throwaway ECDSA P-256 key under
// distributions, verified/sha256-<hex>/record.yaml of kind distribution whose signer
// is that key, beside it the manifest as dist use keeps it, and verified/current.
func Files(manifest []byte) (map[string][]byte, error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		return nil, err
	}
	id := sha256.Sum256(der)
	pub := strings.TrimSuffix(string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), "\n")
	sum := sha256.Sum256(manifest)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	dir := "verified/sha256-" + hex.EncodeToString(sum[:])
	return map[string][]byte{
		"trust.yaml":         []byte("distributions:\n  - key: |\n      " + strings.ReplaceAll(pub, "\n", "\n      ") + "\n"),
		dir + "/record.yaml": fmt.Appendf(nil, "kind: distribution\ndigest: %s\nsigner: %s\n", digest, base64.StdEncoding.EncodeToString(id[:])),
		dir + "/manifest":    manifest,
		"verified/current":   []byte(digest + "\n"),
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
	sum := sha256.Sum256(manifest)
	st := fmt.Sprintf(`{"_type": "https://in-toto.io/Statement/v1", "subject": [{"name": "%s@%s", "digest": {"sha256": "%x"}}], "predicateType": %q, "predicate": {"definitions": %s}}`,
		m.ID, m.Version, sum, boot.AttestationType, definitions)
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
