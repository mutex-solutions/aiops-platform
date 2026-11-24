// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/ocitest"
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

// env is a caller whose environment holds the key password and vars.
func env(vars map[string]string) def.Caller {
	return def.Caller{Env: func(n string) (string, bool) {
		if n == def.KeyPasswordVar {
			return "pw", true
		}
		v, ok := vars[n]
		return v, ok
	}}
}

// dockerConfig writes config.json under a new directory with the Basic credentials of
// user for host, as a login writes them, and returns the directory.
func dockerConfig(t *testing.T, host, user, password string) string {
	t.Helper()
	dir := t.TempDir()
	auth := base64.StdEncoding.EncodeToString([]byte(user + ":" + password))
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"auths": {"`+host+`": {"auth": "`+auth+`"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// distImage pushes the image site:1 to reg with its distribution manifest as a
// referrer with no bundle, as the builder leaves it (ADR-044 decision 1).
func distImage(reg *ocitest.Registry, manifest []byte) ocitest.Descriptor {
	img := reg.Artifact("site", "1", "application/vnd.example.image.v1", nil, ocitest.Layer{MediaType: "application/vnd.example.layer.v1", Data: []byte("site")})
	reg.Artifact("site", "", def.DistributionType, &img, ocitest.Layer{MediaType: def.DistributionLayer, Data: manifest})
	return img
}

// siteHome makes a home whose trust.yaml lists the key pair site under dir as a
// distribution root, and returns the home and the prefix of the pair.
func siteHome(t *testing.T, l Library, dir string) (string, string) {
	t.Helper()
	home, prefix := filepath.Join(t.TempDir(), "home"), generate(t, l, dir, "site")
	if _, err := l.AddTrust(t.Context(), def.TrustRequest{Caller: caller(), Home: home, KeyFile: prefix + ".pub", Distribution: true}); err != nil {
		t.Fatal(err)
	}
	return home, prefix
}

// dist sign pushes the manifest and its bundle as one referrer of the image, and a
// signature of the image's digest as another, in the bundle form of cosign sign, with
// the credentials of config.json under DOCKER_CONFIG; dist use verifies both with a key
// of distributions and writes the record of the manifest's digest, verified/current and
// image.yaml, which names the image by digest, in that order (ADR-020 decisions 10 and
// 12; ADR-061 decision 17; ADR-047 decision 13).
func TestDistSignAndUseWriteTheRecord(t *testing.T) {
	l := Library{LockWait: time.Second}
	dir := t.TempDir()
	home, prefix := siteHome(t, l, dir)
	reg := ocitest.New(t, ocitest.Options{User: "site", Password: "secret"})
	manifest := []byte("distVersion: 1\n")
	img := distImage(reg, manifest)
	c := env(map[string]string{"DOCKER_CONFIG": dockerConfig(t, reg.Host, "site", "secret")})
	w, err := l.SignDist(t.Context(), def.SignDistRequest{Caller: c, Image: "oci://" + reg.Host + "/site:1", Key: prefix + ".key"})
	if err != nil || len(w.Paths) != 2 {
		t.Fatalf("SignDist = %v, %v", w, err)
	}
	referrer, ok := strings.CutPrefix(w.Paths[0], reg.Host+"/site@")
	signature, sok := strings.CutPrefix(w.Paths[1], reg.Host+"/site@")
	refs := reg.Referrers("site", img.Digest)
	if !ok || !sok || len(refs) != 3 || !slices.ContainsFunc(refs, func(d ocitest.Descriptor) bool { return d.Digest == referrer && d.ArtifactType == def.DistributionType }) ||
		!slices.ContainsFunc(refs, func(d ocitest.Descriptor) bool {
			return d.Digest == signature && d.ArtifactType == bundleType && d.Annotations["dev.sigstore.bundle.predicateType"] == "https://sigstore.dev/cosign/sign/v1"
		}) {
		t.Fatalf("SignDist wrote %v; the image has the referrers %+v", w.Paths, refs)
	}
	raw, _ := reg.Fetch("site", referrer)
	var m struct {
		Layers  []struct{ MediaType, Digest string }
		Subject struct{ Digest string }
	}
	if err := json.Unmarshal(raw, &m); err != nil || len(m.Layers) != 2 || m.Layers[0].MediaType != def.DistributionLayer ||
		m.Layers[1].MediaType != bundleType || m.Subject.Digest != img.Digest {
		t.Fatalf("the referrer %s: %s, %v", referrer, raw, err)
	}
	bundle, _ := reg.Fetch("site", m.Layers[1].Digest)
	pubPEM, _ := os.ReadFile(prefix + ".pub")
	pub, id, _ := LoadPublicKey(pubPEM)
	if err := VerifyBlob(pub, bundle, manifest); err != nil {
		t.Fatalf("the bundle dist sign pushed: %v", err)
	}
	raw, _ = reg.Fetch("site", signature)
	if err := json.Unmarshal(raw, &m); err != nil || len(m.Layers) != 1 || m.Subject.Digest != img.Digest {
		t.Fatalf("the signature referrer %s: %s, %v", signature, raw, err)
	}
	b, _ := reg.Fetch("site", m.Layers[0].Digest)
	if st, err := VerifyStatement(pub, b, mustHex(t, strings.TrimPrefix(img.Digest, "sha256:"))); err != nil || !strings.Contains(string(st), `"predicateType":"https://sigstore.dev/cosign/sign/v1"`) {
		t.Fatalf("the signature of the image that dist sign pushed: %s, %v", st, err)
	}
	if _, err := l.UseDist(t.Context(), def.UseRequest{Caller: caller(), Home: home, Image: reg.Host + "/site:1"}); err == nil {
		t.Error("dist use with no credentials for a registry that asks for them: no error")
	}
	ch, err := l.UseDist(t.Context(), def.UseRequest{Caller: c, Home: home, Image: reg.Host + "/site:1"})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(manifest)
	d := hex.EncodeToString(sum[:])
	rec := filepath.Join(home, "verified", "sha256-"+d)
	if !slices.Equal(ch.Written, []string{rec, filepath.Join(home, "verified", "current"), filepath.Join(home, "image.yaml")}) {
		t.Errorf("written %v; want the record, then current, then image.yaml", ch.Written)
	}
	want := map[string]string{
		"verified/sha256-" + d + "/record.yaml":            "kind: distribution\ndigest: sha256:" + d + "\nsigner: " + id + "\n",
		"verified/sha256-" + d + "/manifest":               string(manifest),
		"verified/sha256-" + d + "/manifest.sigstore.json": string(bundle),
		"verified/current":                                 "sha256:" + d + "\n",
		"image.yaml":                                       "image: " + reg.Host + "/site@" + img.Digest + "\n",
	}
	for name, data := range want {
		if b, err := os.ReadFile(filepath.Join(home, name)); err != nil || string(b) != data {
			t.Errorf("%s: %q, %v; want %q", name, b, err, data)
		}
	}
	if h, err := l.Home(t.Context(), def.HomeRequest{Home: home}); err != nil || h.Image != reg.Host+"/site@"+img.Digest {
		t.Errorf("Home = %+v, %v; want the image by digest", h, err)
	}
}

// On a registry with no referrers API, as Distribution v3.1.2 has none, the referrer
// that dist sign pushes is listed under the referrers tag schema, where dist use finds
// it (ADR-047 decision 4).
func TestDistOnARegistryWithoutReferrersAPI(t *testing.T) {
	l := Library{LockWait: time.Second}
	home, prefix := siteHome(t, l, t.TempDir())
	reg := ocitest.New(t, ocitest.Options{NoReferrers: true})
	img := distImage(reg, []byte("distVersion: 1\n"))
	// The builder of a registry with no referrers API lists its referrer under the tag
	// schema too.
	reg.Manifest("site", strings.Replace(img.Digest, ":", "-", 1), ocitest.IndexType,
		fmt.Appendf(nil, `{"schemaVersion": 2, "mediaType": %q, "manifests": %s}`, ocitest.IndexType, must(json.Marshal(reg.Referrers("site", img.Digest)))))
	if _, err := l.SignDist(t.Context(), def.SignDistRequest{Caller: caller(), Image: reg.Host + "/site@" + img.Digest, Key: prefix + ".key"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := reg.Fetch("site", strings.Replace(img.Digest, ":", "-", 1)); strings.Count(string(b), `"digest"`) != 3 {
		t.Errorf("the index under the referrers tag schema: %s; want the three referrers", b)
	}
	if _, err := l.UseDist(t.Context(), def.UseRequest{Caller: caller(), Home: home, Image: reg.Host + "/site:1"}); err != nil {
		t.Fatal(err)
	}
}

// releaseBox is a toolbox of a release binary, which carries the release public key.
type releaseBox struct {
	*packBox
	key []byte
}

func (b releaseBox) Release(context.Context, string) (launch.Release, error) {
	return launch.Release{Key: b.key}, nil
}

// dist use verifies an image with the default release root, the key that a release
// binary carries, where trust.yaml trusts it, which it does with no trust.yaml at all,
// and records the signer release; with the root removed from distributions, or in a
// binary that carries no key, the same image is refused (ADR-020 decision 13).
func TestDistUseVerifiesTheReleaseRoot(t *testing.T) {
	dir := t.TempDir()
	prefix := generate(t, Library{LockWait: time.Second}, dir, "release")
	pub, err := os.ReadFile(prefix + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	l := Library{Box: releaseBox{&packBox{}, pub}, LockWait: time.Second}
	reg := ocitest.New(t, ocitest.Options{})
	manifest := []byte("distVersion: 1\n# the release\n")
	distImage(reg, manifest)
	if _, err := l.SignDist(t.Context(), def.SignDistRequest{Caller: caller(), Image: reg.Host + "/site:1", Key: prefix + ".key"}); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(t.TempDir(), "home")
	if _, err := l.UseDist(t.Context(), def.UseRequest{Caller: caller(), Home: home, Image: reg.Host + "/site:1"}); err != nil {
		t.Fatalf("dist use with the release root: %v", err)
	}
	sum := sha256.Sum256(manifest)
	if b, err := os.ReadFile(filepath.Join(home, "verified", "sha256-"+hex.EncodeToString(sum[:]), "record.yaml")); err != nil || !strings.Contains(string(b), "signer: release\n") {
		t.Errorf("the record: %s, %v; want the signer release", b, err)
	}
	other := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(other, 0o755); err != nil || os.WriteFile(filepath.Join(other, "trust.yaml"), []byte("distributions: []\n"), 0o644) != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		l    Library
		home string
	}{"the root removed": {l, other}, "a binary with no key": {Library{Box: releaseBox{&packBox{}, nil}, LockWait: time.Second}, filepath.Join(t.TempDir(), "home")}} {
		if _, err := c.l.UseDist(t.Context(), def.UseRequest{Caller: caller(), Home: c.home, Image: reg.Host + "/site:1"}); !errors.Is(err, def.ErrRefused) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// dist use refuses, and writes nothing for, a referrer whose bundle signs another
// manifest than the one beside it, and an image whose referrers hold no distribution
// manifest; dist sign refuses an image whose referrers hold two (ADR-020 decision 12).
func TestDistUseRefusesWhatDoesNotVerify(t *testing.T) {
	l := Library{LockWait: time.Second}
	home, prefix := siteHome(t, l, t.TempDir())
	reg := ocitest.New(t, ocitest.Options{})
	img := distImage(reg, []byte("distVersion: 1\n"))
	if _, err := l.SignDist(t.Context(), def.SignDistRequest{Caller: caller(), Image: reg.Host + "/site:1", Key: prefix + ".key"}); err != nil {
		t.Fatal(err)
	}
	var bundle []byte
	for _, r := range reg.Referrers("site", img.Digest) {
		raw, _ := reg.Fetch("site", r.Digest)
		var m struct {
			Layers []struct{ MediaType, Digest string }
		}
		_ = json.Unmarshal(raw, &m)
		if len(m.Layers) == 2 {
			bundle, _ = reg.Fetch("site", m.Layers[1].Digest)
		}
	}
	other := reg.Artifact("other", "1", "application/vnd.example.image.v1", nil)
	reg.Artifact("other", "", def.DistributionType, &other, ocitest.Layer{MediaType: def.DistributionLayer, Data: []byte("distVersion: 2\n")},
		ocitest.Layer{MediaType: bundleType, Data: bundle})
	reg.Artifact("bare", "1", "application/vnd.example.image.v1", nil)
	before, _ := l.Home(t.Context(), def.HomeRequest{Home: home})
	for _, ref := range []string{"other:1", "bare:1"} {
		if _, err := l.UseDist(t.Context(), def.UseRequest{Caller: caller(), Home: home, Image: reg.Host + "/" + ref}); !errors.Is(err, def.ErrRefused) {
			t.Errorf("dist use of %s: %v, want ErrRefused", ref, err)
		}
	}
	if after, _ := l.Home(t.Context(), def.HomeRequest{Home: home}); !slices.Equal(after.Records, before.Records) || after.Current != "" {
		t.Errorf("a refused dist use changed the home: %+v", after)
	}
	reg.Artifact("site", "", def.DistributionType, &img, ocitest.Layer{MediaType: def.DistributionLayer, Data: []byte("distVersion: 3\n")})
	if _, err := l.SignDist(t.Context(), def.SignDistRequest{Caller: caller(), Image: reg.Host + "/site:1", Key: prefix + ".key"}); !errors.Is(err, def.ErrRefused) {
		t.Errorf("dist sign of an image with two distribution manifests: %v, want ErrRefused", err)
	}
}

// dist use writes image.yaml only for an image that a key of distributions signed: the
// distribution manifest names no image, so a signed distribution referrer copied onto
// another image is refused, and nothing is written (ADR-020 decision 12; ADR-047
// decision 13).
func TestDistUseRefusesAnImageNoKeySigned(t *testing.T) {
	l := Library{LockWait: time.Second}
	home, prefix := siteHome(t, l, t.TempDir())
	reg := ocitest.New(t, ocitest.Options{})
	img := distImage(reg, []byte("distVersion: 1\n"))
	if _, err := l.SignDist(t.Context(), def.SignDistRequest{Caller: caller(), Image: reg.Host + "/site:1", Key: prefix + ".key"}); err != nil {
		t.Fatal(err)
	}
	var layers []ocitest.Layer
	for _, r := range reg.Referrers("site", img.Digest) {
		raw, _ := reg.Fetch("site", r.Digest)
		var m struct {
			Layers []struct{ MediaType, Digest string }
		}
		if json.Unmarshal(raw, &m) == nil && r.ArtifactType == def.DistributionType && len(m.Layers) == 2 {
			for _, layer := range m.Layers {
				b, _ := reg.Fetch("site", layer.Digest)
				layers = append(layers, ocitest.Layer{MediaType: layer.MediaType, Data: b})
			}
		}
	}
	if len(layers) != 2 {
		t.Fatalf("the signed distribution referrer has the layers %+v", layers)
	}
	evil := reg.Artifact("site", "evil", "application/vnd.example.image.v1", nil, ocitest.Layer{MediaType: "application/vnd.example.layer.v1", Data: []byte("attacker binary")})
	reg.Artifact("site", "", def.DistributionType, &evil, layers...)
	if _, err := l.UseDist(t.Context(), def.UseRequest{Caller: caller(), Home: home, Image: reg.Host + "/site:evil"}); !errors.Is(err, def.ErrRefused) {
		t.Errorf("dist use of an image that only a copied referrer names: %v, want ErrRefused", err)
	}
	if h, err := l.Home(t.Context(), def.HomeRequest{Home: home}); err != nil || h.Image != "" || h.Current != "" || len(h.Records) != 0 {
		t.Errorf("a refused dist use wrote %+v, %v", h, err)
	}
}

// A public key file is one PEM block PUBLIC KEY and nothing else, as boot reads the key
// of trust.yaml (boot.ReadTrust): trust add refuses a file with text, a comment or a
// byte order mark before the block, and writes nothing, since a trust.yaml that boot
// cannot read denies every Go module at the next boot.
func TestAddTrustRefusesTextBeforeTheBlock(t *testing.T) {
	l := Library{LockWait: time.Second}
	dir, home := t.TempDir(), filepath.Join(t.TempDir(), "home")
	pub, err := os.ReadFile(generate(t, l, dir, "site") + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	for i, lead := range []string{"site key of acme\n", "# acme\n", "\xef\xbb\xbf"} {
		data := append([]byte(lead), pub...)
		f := filepath.Join(dir, "lead"+strconv.Itoa(i)+".pub")
		if err := os.WriteFile(f, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := LoadPublicKey(data); err == nil {
			t.Errorf("%q before the block: LoadPublicKey reads the key", lead)
		}
		for _, dist := range []bool{false, true} {
			r := def.TrustRequest{Caller: caller(), Home: home, KeyFile: f, Distribution: dist}
			if !dist {
				r.IDs = []string{"acme/*"}
			}
			if _, err := l.AddTrust(t.Context(), r); !errors.Is(err, def.ErrRefused) {
				t.Errorf("%q before the block, distribution %v: %v; want ErrRefused", lead, dist, err)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(home, "trust.yaml")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a refused trust add wrote trust.yaml: %v", err)
	}
}
