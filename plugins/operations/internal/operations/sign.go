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
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/secure-systems-lab/go-securesystemslib/dsse"
	"github.com/secure-systems-lab/go-securesystemslib/encrypted"
	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	protodsse "github.com/sigstore/protobuf-specs/gen/pb-go/dsse"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/sigstore/sigstore/pkg/signature"
)

// The signing library of ADR-020: a key pair in cosign's format (decision 4), and a
// Sigstore bundle v0.3 in key mode (decision 2), built from sigstore-go's bundle type,
// the protobuf-specs messages, dsse.PAE and sigstore's signature package without
// sigstore-go's pkg/sign (decision 20), and verified with sigstore-go before it is
// returned. No bundle carries a certificate, a log entry or a timestamp, and keyless
// signing is not offered (decision 22).

const (
	privateKeyType = "ENCRYPTED SIGSTORE PRIVATE KEY"
	olderKeyType   = "ENCRYPTED COSIGN PRIVATE KEY"
	bundleType     = "application/vnd.dev.sigstore.bundle.v0.3+json"
	statementType  = "application/vnd.in-toto+json"
)

func hexOf(b []byte) string { return hex.EncodeToString(b) }

// GenerateKeyPair returns a key pair as cosign generate-key-pair writes it: an ECDSA
// P-256 key in PKCS #8, encrypted with password by go-securesystemslib's encrypted
// package, as a PEM block ENCRYPTED SIGSTORE PRIVATE KEY, and its public key as a PEM
// block PUBLIC KEY. The password may be empty.
func GenerateKeyPair(password []byte) (priv, pub []byte, err error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		return nil, nil, err
	}
	enc, err := encrypted.Encrypt(der, password)
	if err != nil {
		return nil, nil, err
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: privateKeyType, Bytes: enc}), pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}), nil
}

// LoadPrivateKey decrypts the PEM block ENCRYPTED SIGSTORE PRIVATE KEY, or the older
// ENCRYPTED COSIGN PRIVATE KEY, of data with password. Only ECDSA P-256 is taken
// (ADR-020 decision 2).
func LoadPrivateKey(data, password []byte) (*ecdsa.PrivateKey, error) {
	b, _ := pem.Decode(data)
	if b == nil || b.Type != privateKeyType && b.Type != olderKeyType {
		return nil, fmt.Errorf("not a private key in cosign's format: no PEM block %s (ADR-020 decision 4)", privateKeyType)
	}
	der, err := encrypted.Decrypt(b.Bytes, password)
	if err != nil {
		return nil, fmt.Errorf("decrypt the private key: %w", err)
	}
	k, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, err
	}
	ec, ok := k.(*ecdsa.PrivateKey)
	if !ok || ec.Curve != elliptic.P256() {
		return nil, errors.New("the private key is not an ECDSA P-256 key, the one algorithm taken at S2a (ADR-020 decision 2)")
	}
	return ec, nil
}

// LoadPublicKey reads data, one PEM block PUBLIC KEY of an ECDSA P-256 key and nothing
// else, and returns the key and its id, the base64 of the SHA-256 of its DER form, the
// hint that cosign and sigstore-go write (ADR-020 decision 4).
func LoadPublicKey(data []byte) (*ecdsa.PublicKey, string, error) {
	b, rest := pem.Decode(data)
	if b == nil || b.Type != "PUBLIC KEY" || len(b.Headers) > 0 || len(bytes.TrimSpace(rest)) > 0 {
		return nil, "", errors.New("not a public key file: one PEM block PUBLIC KEY and nothing else (ADR-020 decision 4)")
	}
	k, err := x509.ParsePKIXPublicKey(b.Bytes)
	ec, ok := k.(*ecdsa.PublicKey)
	if err != nil || !ok || ec.Curve != elliptic.P256() {
		return nil, "", errors.New("the public key is not an ECDSA P-256 key, the one algorithm taken at S2a (ADR-020 decision 2)")
	}
	return ec, keyID(b.Bytes), nil
}

func keyID(der []byte) string {
	sum := sha256.Sum256(der)
	return base64.StdEncoding.EncodeToString(sum[:])
}

// SignBlob signs data into a bundle that holds a message signature over the SHA-256
// of data, the form of cosign sign-blob (ADR-020 decision 2), and returns it as JSON.
func SignBlob(k *ecdsa.PrivateKey, data []byte) ([]byte, error) {
	sig, err := signMessage(k, data)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	return seal(k, &protobundle.Bundle{Content: &protobundle.Bundle_MessageSignature{MessageSignature: &protocommon.MessageSignature{
		MessageDigest: &protocommon.HashOutput{Algorithm: protocommon.HashAlgorithm_SHA2_256, Digest: sum[:]}, Signature: sig}}},
		verify.WithArtifact(bytes.NewReader(data)))
}

// SignStatement signs an in-toto Statement v1 into a bundle that holds a DSSE envelope
// of it, the form of cosign sign (ADR-020 decisions 2 and 9), and returns it as JSON.
// The statement names one sha256 subject, against which the bundle is verified.
func SignStatement(k *ecdsa.PrivateKey, statement []byte) ([]byte, error) {
	digest, err := subject(statement)
	if err != nil {
		return nil, err
	}
	sig, err := signMessage(k, dsse.PAE(statementType, statement))
	if err != nil {
		return nil, err
	}
	return seal(k, &protobundle.Bundle{Content: &protobundle.Bundle_DsseEnvelope{DsseEnvelope: &protodsse.Envelope{
		Payload: statement, PayloadType: statementType, Signatures: []*protodsse.Signature{{Sig: sig}}}}},
		verify.WithArtifactDigest("sha256", digest))
}

func signMessage(k *ecdsa.PrivateKey, message []byte) ([]byte, error) {
	s, err := signature.LoadECDSASigner(k, crypto.SHA256)
	if err != nil {
		return nil, err
	}
	return s.SignMessage(bytes.NewReader(message))
}

// seal gives pb, which holds its content, the media type and the key hint of k,
// verifies it with sigstore-go against the public key of k and the artifact, and
// returns its JSON.
func seal(k *ecdsa.PrivateKey, pb *protobundle.Bundle, artifact verify.ArtifactPolicyOption) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		return nil, err
	}
	pb.MediaType, pb.VerificationMaterial = bundleType, &protobundle.VerificationMaterial{
		Content: &protobundle.VerificationMaterial_PublicKey{PublicKey: &protocommon.PublicKeyIdentifier{Hint: keyID(der)}}}
	b, err := bundle.NewBundle(pb)
	if err != nil {
		return nil, err
	}
	if err := check(&k.PublicKey, b, artifact); err != nil {
		return nil, fmt.Errorf("the bundle just built does not verify: %w", err)
	}
	return b.MarshalJSON()
}

// VerifyBlob verifies raw, a bundle of a message signature, over data with k.
func VerifyBlob(k *ecdsa.PublicKey, raw, data []byte) error {
	b, err := load(raw)
	if err != nil {
		return err
	}
	if b.GetMessageSignature() == nil {
		return errors.New("the bundle holds no message signature")
	}
	return check(k, b, verify.WithArtifact(bytes.NewReader(data)))
}

// VerifyStatement verifies raw, a bundle of a DSSE envelope of an in-toto statement,
// with k against the sha256 digest of the artifact, and returns the statement.
func VerifyStatement(k *ecdsa.PublicKey, raw, digest []byte) ([]byte, error) {
	b, err := load(raw)
	if err != nil {
		return nil, err
	}
	env := b.GetDsseEnvelope()
	if env == nil || env.PayloadType != statementType {
		return nil, errors.New("the bundle holds no DSSE envelope of an in-toto statement")
	}
	if err := check(k, b, verify.WithArtifactDigest("sha256", digest)); err != nil {
		return nil, err
	}
	return env.Payload, nil
}

// load reads a bundle and refuses one whose verification material is not a key hint.
func load(raw []byte) (*bundle.Bundle, error) {
	var b bundle.Bundle
	if err := b.UnmarshalJSON(raw); err != nil {
		return nil, fmt.Errorf("not a Sigstore bundle: %w", err)
	}
	if b.VerificationMaterial.GetPublicKey() == nil {
		return nil, errors.New("the bundle carries a certificate: keyless signing is not offered at S2a, only key mode (ADR-020 decision 22)")
	}
	return &b, nil
}

// check verifies b with sigstore-go in key mode on k: the key named by the bundle's
// hint must be k, and no log entry or timestamp is expected (ADR-020 Context item 5).
func check(k *ecdsa.PublicKey, b *bundle.Bundle, artifact verify.ArtifactPolicyOption) error {
	der, err := x509.MarshalPKIXPublicKey(k)
	if err != nil {
		return err
	}
	sv, err := signature.LoadECDSAVerifier(k, crypto.SHA256)
	if err != nil {
		return err
	}
	// The validity of the key is open at both ends: no key expires at S2a (ADR-020
	// decision 15).
	material := root.NewTrustedPublicKeyMaterialFromMapping(map[string]*root.ExpiringKey{keyID(der): root.NewExpiringKey(sv, time.Time{}, time.Time{})})
	v, err := verify.NewVerifier(material, verify.WithNoObserverTimestamps())
	if err != nil {
		return err
	}
	_, err = v.Verify(b, verify.NewPolicy(artifact, verify.WithKey()))
	return err
}

// subject returns the sha256 digest of the one subject of an in-toto Statement v1.
func subject(statement []byte) ([]byte, error) {
	var st struct {
		Type    string `json:"_type"`
		Subject []struct{ Digest map[string]string }
	}
	if err := json.Unmarshal(statement, &st); err != nil {
		return nil, err
	}
	if st.Type != "https://in-toto.io/Statement/v1" || len(st.Subject) != 1 {
		return nil, errors.New("not an in-toto Statement v1 with one subject (ADR-020 decision 9)")
	}
	d, err := hex.DecodeString(st.Subject[0].Digest["sha256"])
	if err != nil || len(d) != sha256.Size || strings.ToLower(st.Subject[0].Digest["sha256"]) != st.Subject[0].Digest["sha256"] {
		return nil, errors.New("the subject of the statement has no sha256 digest")
	}
	return d, nil
}
