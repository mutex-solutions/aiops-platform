// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/secure-systems-lab/go-securesystemslib/encrypted"
	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/sigstore/sigstore/pkg/signature"
)

func keyPair(t *testing.T, password string) (*ecdsa.PrivateKey, []byte, []byte) {
	t.Helper()
	priv, pub, err := GenerateKeyPair([]byte(password))
	if err != nil {
		t.Fatal(err)
	}
	k, err := LoadPrivateKey(priv, []byte(password))
	if err != nil {
		t.Fatal(err)
	}
	return k, priv, pub
}

// verifier is sigstore-go in key mode on pub, with no log entry and no timestamp
// (ADR-020 Context item 5).
func verifier(t *testing.T, pub []byte) *verify.Verifier {
	t.Helper()
	b, _ := pem.Decode(pub)
	k, err := x509.ParsePKIXPublicKey(b.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	sv, err := signature.LoadVerifier(k, crypto.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b.Bytes)
	material := root.NewTrustedPublicKeyMaterialFromMapping(map[string]*root.ExpiringKey{
		base64.StdEncoding.EncodeToString(sum[:]): root.NewExpiringKey(sv, timeZero, timeZero)})
	v, err := verify.NewVerifier(material, verify.WithNoObserverTimestamps())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// The key pair is cosign's (ADR-020 decision 4): a PEM block ENCRYPTED SIGSTORE
// PRIVATE KEY holding a PKCS #8 ECDSA P-256 key encrypted with go-securesystemslib's
// encrypted package, and a PEM block PUBLIC KEY. An empty password is allowed, the
// older block type is read, a wrong password and another curve are refused.
func TestKeyPairIsCosigns(t *testing.T) {
	for _, password := range []string{"secret", ""} {
		k, priv, pub := keyPair(t, password)
		b, rest := pem.Decode(priv)
		if b == nil || b.Type != "ENCRYPTED SIGSTORE PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
			t.Fatalf("private key file %q", priv)
		}
		der, err := encrypted.Decrypt(b.Bytes, []byte(password))
		if err != nil {
			t.Fatal(err)
		}
		if parsed, err := x509.ParsePKCS8PrivateKey(der); err != nil || !parsed.(*ecdsa.PrivateKey).Equal(k) {
			t.Fatalf("the private key is not PKCS #8 of the loaded key: %v", err)
		}
		pk, id, err := LoadPublicKey(pub)
		if err != nil || !pk.Equal(&k.PublicKey) {
			t.Fatalf("public key: %v", err)
		}
		pb, _ := pem.Decode(pub)
		sum := sha256.Sum256(pb.Bytes)
		if pb.Type != "PUBLIC KEY" || id != base64.StdEncoding.EncodeToString(sum[:]) {
			t.Fatalf("public key file %q with id %s", pub, id)
		}
		older := pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED COSIGN PRIVATE KEY", Bytes: b.Bytes})
		if o, err := LoadPrivateKey(older, []byte(password)); err != nil || !o.Equal(k) {
			t.Fatalf("the older block type: %v", err)
		}
		if _, err := LoadPrivateKey(priv, []byte(password+"x")); err == nil {
			t.Fatal("a wrong password decrypts")
		}
	}
	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(p384)
	enc, _ := encrypted.Encrypt(der, nil)
	if _, err := LoadPrivateKey(pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED SIGSTORE PRIVATE KEY", Bytes: enc}), nil); err == nil || !strings.Contains(err.Error(), "P-256") {
		t.Fatalf("a P-384 private key: %v, want a refusal naming P-256", err)
	}
	pubDER, _ := x509.MarshalPKIXPublicKey(&p384.PublicKey)
	if _, _, err := LoadPublicKey(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})); err == nil {
		t.Fatal("a P-384 public key loads")
	}
	if _, _, err := LoadPublicKey([]byte("not a key")); err == nil {
		t.Fatal("a file with no PEM block loads")
	}
}

// A blob is signed into a Sigstore bundle v0.3 in key mode: a message signature over
// the SHA-256 of its bytes, the key id as hint, no certificate, no log entry and no
// timestamp; sigstore-go verifies it, and VerifyBlob refuses other bytes, another key
// and a bundle that carries a certificate (ADR-020 decisions 2, 3 and 22).
func TestBlobBundle(t *testing.T) {
	k, _, pub := keyPair(t, "pw")
	data := []byte("distVersion: 1\n")
	raw, err := SignBlob(k, data)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		MediaType            string
		VerificationMaterial map[string]json.RawMessage
		MessageSignature     struct {
			MessageDigest struct{ Algorithm, Digest string }
		}
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	_, id, _ := LoadPublicKey(pub)
	sum := sha256.Sum256(data)
	hint := struct{ Hint string }{}
	_ = json.Unmarshal(doc.VerificationMaterial["publicKey"], &hint)
	if doc.MediaType != "application/vnd.dev.sigstore.bundle.v0.3+json" || hint.Hint != id || len(doc.VerificationMaterial) != 1 ||
		doc.MessageSignature.MessageDigest.Algorithm != "SHA2_256" ||
		doc.MessageSignature.MessageDigest.Digest != base64.StdEncoding.EncodeToString(sum[:]) {
		t.Fatalf("bundle %s", raw)
	}
	var b bundle.Bundle
	if err := b.UnmarshalJSON(raw); err != nil {
		t.Fatal(err)
	}
	if _, err := verifier(t, pub).Verify(&b, verify.NewPolicy(verify.WithArtifact(bytes.NewReader(data)), verify.WithKey())); err != nil {
		t.Fatalf("sigstore-go: %v", err)
	}
	pk, _, _ := LoadPublicKey(pub)
	if err := VerifyBlob(pk, raw, data); err != nil {
		t.Fatal(err)
	}
	if err := VerifyBlob(pk, raw, []byte("distVersion: 2\n")); err == nil {
		t.Fatal("the bundle verifies other bytes")
	}
	other, _, _ := keyPair(t, "pw")
	if err := VerifyBlob(&other.PublicKey, raw, data); err == nil {
		t.Fatal("the bundle verifies with another key")
	}
	b.VerificationMaterial = &protobundle.VerificationMaterial{Content: &protobundle.VerificationMaterial_Certificate{
		Certificate: &protocommon.X509Certificate{RawBytes: []byte{1}}}}
	keyless, err := b.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyBlob(pk, keyless, data); err == nil || !strings.Contains(err.Error(), "keyless") {
		t.Fatalf("a bundle with a certificate: %v, want a refusal naming keyless", err)
	}
}

// A statement is signed into a bundle that holds a DSSE envelope of the in-toto
// Statement v1, whose subject sigstore-go matches against the artifact's digest
// (ADR-020 decisions 2 and 9).
func TestStatementBundle(t *testing.T) {
	k, _, pub := keyPair(t, "pw")
	digest := sha256.Sum256([]byte("an OCI image manifest"))
	st := []byte(`{"_type": "https://in-toto.io/Statement/v1", "subject": [{"name": "acme/echoer@0.1.0", "digest": {"sha256": "` +
		hexOf(digest[:]) + `"}}], "predicateType": "https://sigstore.dev/cosign/sign/v1", "predicate": {}}`)
	raw, err := SignStatement(k, st)
	if err != nil {
		t.Fatal(err)
	}
	var b bundle.Bundle
	if err := b.UnmarshalJSON(raw); err != nil {
		t.Fatal(err)
	}
	env := b.GetDsseEnvelope()
	if env == nil || env.PayloadType != "application/vnd.in-toto+json" || !bytes.Equal(env.Payload, st) {
		t.Fatalf("bundle %s", raw)
	}
	if _, err := verifier(t, pub).Verify(&b, verify.NewPolicy(verify.WithArtifactDigest("sha256", digest[:]), verify.WithKey())); err != nil {
		t.Fatalf("sigstore-go: %v", err)
	}
	pk, _, _ := LoadPublicKey(pub)
	if got, err := VerifyStatement(pk, raw, digest[:]); err != nil || !bytes.Equal(got, st) {
		t.Fatalf("VerifyStatement: %q, %v", got, err)
	}
	other := sha256.Sum256([]byte("another manifest"))
	if _, err := VerifyStatement(pk, raw, other[:]); err == nil {
		t.Fatal("the statement verifies against another digest")
	}
}
