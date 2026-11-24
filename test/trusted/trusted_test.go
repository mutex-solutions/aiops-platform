// SPDX-License-Identifier: Apache-2.0

package trusted

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mutex-solutions/aiops-platform/boot"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/ocitest"
)

const manifest = `manifestVersion: "1.0"
id: aiops-test/x
version: 0.1.0
title: X
license: Apache-2.0
publisher: {id: aiops-test}
stability: experimental
scope: [process]
runtimes: [inproc]
uses: []
provides: []
registers: []
hooks: []
configSchema: {}
durable: []
requires: {pluginApi: "0.2", definitions: {}}
`

// What Write writes is what boot reads (ADR-020 decisions 10, 12 and 13; ADR-060
// decision 28): trust.yaml with the default root and a key under distributions, the
// record of the manifest's digest, of kind distribution, whose signer is that key, and
// verified/current naming it. The record keeps the manifest and a bundle that dist use
// of the operations row verifies again, as the referrer of an image, in another home
// that trusts the same key.
func TestWriteRecordsWhatBootReads(t *testing.T) {
	signed := []byte("distVersion: 1\n")
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	Write(t, home, signed)
	fsys := os.DirFS(home)
	trust, err := boot.ReadTrust(fsys)
	if err != nil || len(trust.Distributions) != 2 || trust.Distributions[0] != boot.ReleaseRoot || len(trust.Signers) != 0 {
		t.Fatalf("trust.yaml %+v, %v; want the default root and one key under distributions", trust, err)
	}
	sum := sha256.Sum256(signed)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	if cur, err := boot.ReadCurrent(fsys); err != nil || cur != digest {
		t.Fatalf("verified/current %q, %v; want %s", cur, err, digest)
	}
	rec, err := boot.ReadRecord(fsys, digest)
	if err != nil || rec.Kind != "distribution" || rec.Signer != trust.Distributions[1] {
		t.Fatalf("record %+v, %v; want a record of a distribution signed by the key of trust.yaml", rec, err)
	}
	dir := filepath.Join(home, "verified", strings.Replace(digest, ":", "-", 1))
	reg := ocitest.New(t, ocitest.Options{})
	img := reg.Artifact("site", "1", "application/vnd.example.image.v1", nil)
	reg.Artifact("site", "", def.DistributionType, &img, ocitest.Layer{MediaType: def.DistributionLayer, Data: readFile(t, filepath.Join(dir, "manifest"))},
		ocitest.Layer{MediaType: bundleMedia, Data: readFile(t, filepath.Join(dir, "manifest.sigstore.json"))})
	// The signature of the image, which the record does not keep: it signs the image of
	// the same digest that Write used, not the manifest.
	signer.Lock()
	reg.Blob("site", bundleMedia, signer.imageSig)
	reg.Manifest("site", "", ocitest.ManifestType, signer.image)
	signer.Unlock()
	other := filepath.Join(t.TempDir(), "other")
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(home, "trust.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "trust.yaml"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := mount(t).UseDist(t.Context(), def.UseRequest{Home: other, Image: reg.Host + "/site:1"}); err != nil {
		t.Fatalf("dist use of the record's manifest and bundle in a home that trusts the same key: %v", err)
	}
}

// Files are the files that Write writes into a home, by slash path.
func TestFilesAreWhatWriteWrites(t *testing.T) {
	signed := []byte("distVersion: 1\n")
	files := Files(t, signed)
	sum := sha256.Sum256(signed)
	dir := "verified/sha256-" + hex.EncodeToString(sum[:]) + "/"
	for _, name := range []string{"trust.yaml", "verified/current", dir + "record.yaml", dir + "manifest", dir + "manifest.sigstore.json"} {
		if files[name] == nil {
			t.Errorf("no %s in the files of a home that trusts the manifest", name)
		}
	}
	if string(files[dir+"manifest"]) != string(signed) {
		t.Errorf("the record keeps %q, want the manifest", files[dir+"manifest"])
	}
	fsys := fstest.MapFS{}
	for name, data := range files {
		fsys[name] = &fstest.MapFile{Data: data}
	}
	if _, err := boot.ReadRecord(fsys, "sha256:"+hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
}

// What Install writes is what boot reads of an installed in-process artifact: the kept
// files that hash to the digest, the record beside it with its signer and module, the
// statement, and the signer in trust.yaml beside the distribution key of Write.
func TestInstallWritesWhatBootReads(t *testing.T) {
	for _, a := range []Artifact{
		{Manifest: []byte(manifest), Bundle: []byte("- insert: [{id: x, plugin: aiops-test/x, roles: [api], enabled: false}]\n"), Definitions: "{}"},
		{Manifest: []byte(manifest), Bundle: []byte("[]\n"), Definitions: "{}", Unsigned: true, Subject: strings.Repeat("ab", 32)},
	} {
		home := t.TempDir()
		Write(t, home, []byte("distVersion: 1\n"))
		if err := Install(home, a); err != nil {
			t.Fatal(err)
		}
		fsys := os.DirFS(home)
		art, err := os.ReadFile(filepath.Join(home, "bundles", "aiops-test", "x", "artifact.json"))
		if err != nil {
			t.Fatal(err)
		}
		d := digest(art)
		kept, err := boot.ReadKept(fsys, "aiops-test/x", d)
		if err != nil || string(kept.Manifest) != manifest || string(kept.Bundle) != string(a.Bundle) {
			t.Fatalf("kept %+v: %v", kept, err)
		}
		rec, err := boot.ReadRecord(fsys, d)
		if err != nil || rec.ID != "aiops-test/x" || *rec.Module != (boot.GoModule{Path: "github.com/mutex-solutions/aiops-platform"}) {
			t.Fatalf("record %+v: %v", rec, err)
		}
		st, err := boot.ReadAttestation("attestation.json", rec.Attestation)
		want := cmp.Or(a.Subject, strings.TrimPrefix(d, "sha256:"))
		if err != nil || st.Name != "aiops-test/x@0.1.0" || st.Digest != "sha256:"+want {
			t.Fatalf("statement %+v: %v", st, err)
		}
		trust, err := boot.ReadTrust(fsys)
		if err != nil || len(trust.Distributions) != 2 || len(trust.Signers) != 1 || !slices.Equal(trust.Signers[0].IDs, []string{"aiops-test/x"}) {
			t.Fatalf("trust %+v: %v", trust, err)
		}
		if (rec.Signer == boot.Unsigned) != a.Unsigned || !a.Unsigned && rec.Signer != trust.Signers[0].KeyID {
			t.Fatalf("signer %s, trust.yaml %s", rec.Signer, trust.Signers[0].KeyID)
		}
		// The record of an artifact holds the files of ADR-020 decision 10.
		for _, name := range []string{"record.yaml", "signature.sigstore.json", "attestation.sigstore.json", "attestation.json"} {
			if _, err := os.Stat(filepath.Join(home, "verified", strings.Replace(d, ":", "-", 1), name)); err != nil {
				t.Errorf("the record of the artifact: %v", err)
			}
		}
	}
	if err := Install(t.TempDir(), Artifact{Manifest: []byte("x: [")}); err == nil {
		t.Fatal("a malformed manifest installs")
	}
	if err := Install(t.TempDir(), Artifact{Manifest: []byte(manifest)}); err == nil {
		t.Fatal("a home with no trust.yaml installs")
	}
}

// childEnv makes TestLeavesNothingInTMPDIR the child that makes one home.
const childEnv = "TRUSTED_TEST_CHILD"

// A test process that makes homes leaves nothing in $TMPDIR when it ends: the key pair
// and the signed images live in the test's own temporary directories, which the
// testing package removes. The child runs Write in a fresh $TMPDIR, and the parent
// reads that directory once the child has exited.
func TestLeavesNothingInTMPDIR(t *testing.T) {
	if os.Getenv(childEnv) != "" {
		Write(t, filepath.Join(t.TempDir(), "home"), []byte("distVersion: 1\n"))
		return
	}
	tmp := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestLeavesNothingInTMPDIR$", "-test.count=1")
	cmd.Env = append(os.Environ(), childEnv+"=1", "TMPDIR="+tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the child: %v\n%s", err, out)
	}
	left, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range left {
		names = append(names, e.Name())
	}
	if len(names) > 0 {
		t.Errorf("the child left %q in $TMPDIR", names)
	}
}
