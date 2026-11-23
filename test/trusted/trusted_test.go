// SPDX-License-Identifier: Apache-2.0

package trusted

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mutex-solutions/aiops-platform/boot"
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

// What Install writes is what boot reads of an installed in-process artifact: the kept
// files that hash to the digest, the record beside it with its signer and module, the
// statement, and the signer in trust.yaml beside the distribution key of Write.
func TestInstallWritesWhatBootReads(t *testing.T) {
	for _, a := range []Artifact{
		{Manifest: []byte(manifest), Bundle: []byte("- insert: [{id: x, plugin: aiops-test/x, roles: [api], enabled: false}]\n"), Definitions: "{}"},
		{Manifest: []byte(manifest), Bundle: []byte("[]\n"), Definitions: "{}", Unsigned: true, Subject: strings.Repeat("ab", 32)},
	} {
		home := t.TempDir()
		if err := Write(home, []byte("distVersion: 1\n")); err != nil {
			t.Fatal(err)
		}
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
		if err != nil || len(trust.Distributions) != 1 || len(trust.Signers) != 1 || !slices.Equal(trust.Signers[0].IDs, []string{"aiops-test/x"}) {
			t.Fatalf("trust %+v: %v", trust, err)
		}
		if (rec.Signer == boot.Unsigned) != a.Unsigned || !a.Unsigned && rec.Signer != trust.Signers[0].KeyID {
			t.Fatalf("signer %s, trust.yaml %s", rec.Signer, trust.Signers[0].KeyID)
		}
	}
	if err := Install(t.TempDir(), Artifact{Manifest: []byte("x: [")}); err == nil {
		t.Fatal("a malformed manifest installs")
	}
	if err := Install(t.TempDir(), Artifact{Manifest: []byte(manifest)}); err == nil {
		t.Fatal("a home with no trust.yaml installs")
	}
}
