// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"time"
)

var errStub = errors.New("not implemented")

// timeZero is the open end of a key's validity: no key expires at S2a (ADR-020
// decision 15).
var timeZero time.Time

func hexOf(b []byte) string { return hex.EncodeToString(b) }

// GenerateKeyPair returns a cosign key pair.
func GenerateKeyPair(password []byte) (priv, pub []byte, err error) { return nil, nil, errStub }

// LoadPrivateKey decrypts a cosign private key.
func LoadPrivateKey(data, password []byte) (*ecdsa.PrivateKey, error) { return nil, errStub }

// LoadPublicKey reads a public key and its id.
func LoadPublicKey(data []byte) (*ecdsa.PublicKey, string, error) { return nil, "", errStub }

// SignBlob signs data into a bundle.
func SignBlob(k *ecdsa.PrivateKey, data []byte) ([]byte, error) { return nil, errStub }

// SignStatement signs a statement into a bundle.
func SignStatement(k *ecdsa.PrivateKey, statement []byte) ([]byte, error) { return nil, errStub }

// VerifyBlob verifies a bundle over data.
func VerifyBlob(k *ecdsa.PublicKey, raw, data []byte) error { return errStub }

// VerifyStatement verifies a bundle of a statement against a digest.
func VerifyStatement(k *ecdsa.PublicKey, raw, digest []byte) ([]byte, error) { return nil, errStub }
