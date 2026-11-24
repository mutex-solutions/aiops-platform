// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
)

// What the methods write, file by file: the T1 cases of ADR-061 decision 18 that the
// suite cannot see through the definition.

func caller() def.Caller {
	return def.Caller{Env: func(n string) (string, bool) { return "pw", n == def.KeyPasswordVar }}
}

// generate writes a key pair under dir and returns its prefix.
func generate(t *testing.T, l Library, dir, name string) string {
	t.Helper()
	prefix := filepath.Join(dir, name)
	if _, err := l.GenerateKey(t.Context(), def.KeyRequest{Caller: caller(), Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	return prefix
}

// key generate writes the private key with mode 0600 and the public key with mode
// 0644, as cosign does, in its format and with the password of the environment.
func TestGenerateKeyWritesCosignFiles(t *testing.T) {
	l := Library{LockWait: time.Second}
	prefix := generate(t, l, t.TempDir(), "author")
	priv, err := os.ReadFile(prefix + ".key")
	if err != nil {
		t.Fatal(err)
	}
	k, err := LoadPrivateKey(priv, []byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	pub, err := os.ReadFile(prefix + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if pk, _, err := LoadPublicKey(pub); err != nil || !pk.Equal(&k.PublicKey) {
		t.Fatalf("the public key file does not hold the public key of the private one: %v", err)
	}
	for name, mode := range map[string]os.FileMode{".key": 0o600, ".pub": 0o644} {
		if st, err := os.Stat(prefix + name); err != nil || st.Mode().Perm() != mode {
			t.Errorf("%s: %v, %v; want mode %v", name, st, err, mode)
		}
	}
}

// trust add writes trust.yaml with mode 0600, keeps what the operator wrote in it, and
// answers by run/status.json: not-running with no running process of the profile it
// names, and with one restart-required with the reason not-submitted, since version 1
// of the wire of ADR-062 carries no trust add (ADR-061 decision 17).
func TestAddTrustKeepsTheFileAndAnswersByStatus(t *testing.T) {
	l := Library{LockWait: time.Second}
	dir, home := t.TempDir(), filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(filepath.Join(home, "run"), 0o700); err != nil {
		t.Fatal(err)
	}
	const operator = "# the site's trust\nallowUnsigned: [acme/legacy]\n"
	if err := os.WriteFile(filepath.Join(home, "trust.yaml"), []byte(operator), 0o600); err != nil {
		t.Fatal(err)
	}
	for i, c := range []struct {
		status  string
		outcome def.Outcome
		reason  string
	}{
		{"", def.NotRunning, ""},
		{`{"phase": "stopped", "profile": "dev"}`, def.NotRunning, ""},
		{`{"phase": "running", "profile": "dev"}`, def.RestartRequired, def.ReasonNotSubmitted},
	} {
		status := filepath.Join(home, "run", "status.json")
		_ = os.Remove(status)
		if c.status != "" {
			if err := os.WriteFile(status, []byte(c.status), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		pub := generate(t, l, dir, "k"+strconv.Itoa(i)) + ".pub"
		ch, err := l.AddTrust(t.Context(), def.TrustRequest{Caller: caller(), Home: home, KeyFile: pub, IDs: []string{"acme/*"}})
		if err != nil || ch.Outcome != c.outcome || ch.Reason != c.reason {
			t.Errorf("status %q: %+v, %v; want %s %s", c.status, ch, err, c.outcome, c.reason)
		}
	}
	b, err := os.ReadFile(filepath.Join(home, "trust.yaml"))
	if err != nil || !strings.HasPrefix(string(b), "# the site's trust\n") {
		t.Fatalf("trust.yaml %q, %v; want the operator's comment kept", b, err)
	}
	h, err := l.Home(t.Context(), def.HomeRequest{Home: home})
	if err != nil || !slices.Equal(h.AllowUnsigned, []string{"acme/legacy"}) || len(h.Signers) != 3 {
		t.Errorf("home %+v, %v; want allowUnsigned kept and three signers", h, err)
	}
	if st, err := os.Stat(filepath.Join(home, "trust.yaml")); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("trust.yaml: %v, %v; want mode 0600", st, err)
	}
}

// dist sign writes the bundle beside the manifest of the local image, and dist use
// writes the record of the distribution, record.yaml, the manifest and its bundle,
// then verified/current, each in the form boot reads (ADR-020 decisions 10 and 12).
func TestDistSignAndUseWriteTheRecord(t *testing.T) {
	l := Library{LockWait: time.Second}
	dir, home := t.TempDir(), filepath.Join(t.TempDir(), "home")
	prefix := generate(t, l, dir, "site")
	if _, err := l.AddTrust(t.Context(), def.TrustRequest{Caller: caller(), Home: home, KeyFile: prefix + ".pub", Distribution: true}); err != nil {
		t.Fatal(err)
	}
	img := filepath.Join(dir, "img")
	manifest := []byte("distVersion: 1\n")
	if err := os.Mkdir(img, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(img, "manifest"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := l.SignDist(t.Context(), def.SignDistRequest{Caller: caller(), Image: img, Key: prefix + ".key"})
	if err != nil || !slices.Equal(w.Paths, []string{filepath.Join(img, "manifest.sigstore.json")}) {
		t.Fatalf("SignDist = %v, %v", w, err)
	}
	bundle, err := os.ReadFile(w.Paths[0])
	if err != nil {
		t.Fatal(err)
	}
	pubPEM, _ := os.ReadFile(prefix + ".pub")
	pub, id, _ := LoadPublicKey(pubPEM)
	if err := VerifyBlob(pub, bundle, manifest); err != nil {
		t.Fatalf("the bundle dist sign wrote: %v", err)
	}
	ch, err := l.UseDist(t.Context(), def.UseRequest{Caller: caller(), Home: home, Image: img})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(manifest)
	d := hex.EncodeToString(sum[:])
	rec := filepath.Join(home, "verified", "sha256-"+d)
	if !slices.Equal(ch.Written, []string{rec, filepath.Join(home, "verified", "current")}) {
		t.Errorf("written %v; want the record, then current", ch.Written)
	}
	want := map[string]string{
		"record.yaml":            "kind: distribution\ndigest: sha256:" + d + "\nsigner: " + id + "\n",
		"manifest":               string(manifest),
		"manifest.sigstore.json": string(bundle),
	}
	for name, data := range want {
		if b, err := os.ReadFile(filepath.Join(rec, name)); err != nil || string(b) != data {
			t.Errorf("%s: %q, %v; want %q", name, b, err, data)
		}
	}
	if b, err := os.ReadFile(filepath.Join(home, "verified", "current")); err != nil || string(b) != "sha256:"+d+"\n" {
		t.Errorf("current: %q, %v", b, err)
	}
}
