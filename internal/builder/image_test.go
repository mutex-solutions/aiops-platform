// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/ocitest"
)

// definitionFile is the definition of the builder image (ADR-044 decision 4).
var definitionFile = filepath.Join("..", "..", "dist", "builder", "image.yaml")

// The tree the builder extracted is used again with no module source, as the builder
// image ships it, extracted where the builder's cache keeps it; an extraction recorded
// with another sum is not, and neither is one whose files no longer hash to the sum: a
// toolchain tree that does not match the recorded hash is refused, and extracted again
// where the module source serves the module (ADR-044 decisions 4, 14 and 16).
func TestTreeReusesExtraction(t *testing.T) {
	if runtime.GOOS+"-"+runtime.GOARCH != "linux-amd64" {
		t.Skip("the pin is the toolchain of linux-amd64, and TestTree covers another host")
	}
	modcache, sum := toolchainZip(t)
	cache, empty := t.TempDir(), t.TempDir()
	dir, err := Tree(modcache, cache, sum)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Tree(empty, cache, sum); err != nil || got != dir {
		t.Errorf("Tree with the extraction of the sum: %q, %v", got, err)
	}
	if _, err := Tree(empty, cache, "h1:y="); err == nil {
		t.Error("Tree took an extraction recorded with another sum")
	}
	licence := filepath.Join(dir, "LICENSE")
	if err := errors.Join(os.Chmod(licence, 0o644), os.WriteFile(licence, []byte("not the Go licence\n"), 0o644)); err != nil {
		t.Fatal(err)
	}
	if got, err := Tree(empty, cache, sum); err == nil {
		t.Errorf("Tree took an extraction whose LICENSE changed: %q", got)
	}
	if got, err := Tree(modcache, cache, sum); err != nil || got != dir {
		t.Errorf("Tree with the module source again: %q, %v", got, err)
	}
	if b, err := os.ReadFile(licence); err != nil || string(b) != "a licence\n" {
		t.Errorf("the extraction again holds %q, %v", b, err)
	}
}

// fakeTree writes a toolchain tree with the kinds of licence and notice files that the
// tree of the pin holds (ADR-044, What was read): the root pair, a vendored module of a
// modules.txt with its third-party svgpan text, a module zip under lib/fips140/ and
// the IANA statement of lib/time/README.
func fakeTree(t *testing.T) string {
	t.Helper()
	bsd, apache := string(corpusFile(t, "License/BSD-3-Clause/text.txt")), string(corpusFile(t, "License/Apache-2.0/text.txt"))
	vendor := "src/cmd/vendor/"
	dir := moduleDir(t, map[string]string{"LICENSE": bsd, "PATENTS": "Additional IP Rights Grant (Patents)\n", "bin/go": "go\n",
		vendor + "modules.txt":                                        "# github.com/google/pprof v0.0.0-20251007162407-5df77e3f7d1d\n## explicit\ngithub.com/google/pprof/profile\n",
		vendor + "github.com/google/pprof/LICENSE":                    apache,
		vendor + "github.com/google/pprof/third_party/svgpan/LICENSE": "Copyright 2009-2017 Andrea Leofreddi. All rights reserved.\n",
		"lib/time/README":                                             "The time zone database is in the public domain.\n",
		"src/math/abs.go":                                             "package math\n",
	})
	var z bytes.Buffer
	zw := zip.NewWriter(&z)
	for name, data := range map[string]string{"golang.org/fips140@v1.0.0/LICENSE": bsd, "golang.org/fips140@v1.0.0/fips.go": "package fips140\n"} {
		w, err := zw.Create(name)
		if err == nil {
			_, err = io.WriteString(w, data)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := errors.Join(zw.Close(), os.MkdirAll(filepath.Join(dir, "lib", "fips140"), 0o755), os.WriteFile(filepath.Join(dir, "lib", "fips140", "v1.0.0.zip"), z.Bytes(), 0o444),
		os.Chmod(filepath.Join(dir, "bin", "go"), 0o555)); err != nil {
		t.Fatal(err)
	}
	return dir
}

// layoutImage reads the image tagged tag of the OCI image layout dir: its manifest, its
// config and, by name, the header and bytes of each entry of its layers.
func layoutImage(t *testing.T, dir, tag string) (ocispec.Manifest, ocispec.Image, map[string]*tar.Header, map[string][]byte) {
	t.Helper()
	ctx := context.Background()
	s, err := oci.NewWithContext(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Resolve(ctx, tag)
	if err != nil {
		t.Fatalf("the layout holds no %s: %v", tag, err)
	}
	var m ocispec.Manifest
	var cfg ocispec.Image
	raw, err := content.FetchAll(ctx, s, d)
	if err == nil {
		err = json.Unmarshal(raw, &m)
	}
	if err == nil {
		raw, err = content.FetchAll(ctx, s, m.Config)
	}
	if err == nil {
		err = json.Unmarshal(raw, &cfg)
	}
	if err != nil {
		t.Fatal(err)
	}
	headers, files := map[string]*tar.Header{}, map[string][]byte{}
	for _, l := range m.Layers {
		raw, err := content.FetchAll(ctx, s, l)
		if err != nil {
			t.Fatal(err)
		}
		tr := tar.NewReader(bytes.NewReader(raw))
		for {
			h, err := tr.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			b, rerr := io.ReadAll(tr)
			if err = errors.Join(err, rerr); err != nil {
				t.Fatal(err)
			}
			headers[h.Name], files[h.Name] = h, b
		}
	}
	return m, cfg, headers, files
}

// The builder image has no base layer: the toolchain tree, extracted where the builder's
// cache keeps it, aiopsctl as entry point with its THIRD_PARTY_LICENSES/, NOTICE and
// SBOM, every licence and notice file of the tree copied under THIRD_PARTY_LICENSES/,
// the zips under lib/fips140/ opened, the IANA statement, an image SBOM and the source
// offer; its environment is the Go environment of ADR-060 decision 21 that the
// definition under dist/ names (ADR-044 decisions 4, 5 and 17). A definition that names
// another toolchain, or a field it does not know, is refused.
func TestBuilderImage(t *testing.T) {
	ctx := context.Background()
	def, err := os.ReadFile(definitionFile)
	if err != nil {
		t.Fatal(err)
	}
	tree := fakeTree(t)
	mit := Module{Path: "example.test/mit", Version: "v1.0.0", Sum: "h1:mit=", Dir: moduleDir(t, map[string]string{"LICENSE": string(corpusFile(t, "License/MIT/text.txt")), "m.go": "package m\n"})}
	pkgs := []Package{{ImportPath: mit.Path, Dir: mit.Dir, Module: mit}}
	l, err := ScanLicences(pkgs)
	if err != nil {
		t.Fatal(err)
	}
	ctl := moduleDir(t, map[string]string{"aiopsctl": "binary\n", "NOTICE": "Notices of aiopsctl.\n", "aiopsctl.spdx.json": "{}\n",
		"THIRD_PARTY_LICENSES/example.test/mit/LICENSE": "MIT\n"})
	if err := os.Chmod(filepath.Join(ctl, "aiopsctl"), 0o755); err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	for _, c := range []struct{ name, def, want string }{
		{"another toolchain", strings.Replace(string(def), "version: go1.26.8", "version: go1.27.4", 1), "go1.27.4"},
		{"an unknown field", string(def) + "base: debian\n", "base"},
	} {
		s, err := oci.NewWithContext(ctx, t.TempDir())
		if err == nil {
			_, err = builderImage(ctx, s, []byte(c.def), tree, ctl, pkgs, l, created)
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("a definition with %s: %v", c.name, err)
		}
	}
	dir := t.TempDir()
	s, err := oci.NewWithContext(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	d, err := builderImage(ctx, s, def, tree, ctl, pkgs, l, created)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tag(ctx, d, "builder"); err != nil {
		t.Fatal(err)
	}
	m, cfg, headers, files := layoutImage(t, dir, "builder")
	if len(m.Layers) != 1 || len(cfg.RootFS.DiffIDs) != 1 || cfg.OS != "linux" || cfg.Architecture != "amd64" {
		t.Errorf("the image has %d layers and %d diff ids for %s/%s: one layer and no base layer", len(m.Layers), len(cfg.RootFS.DiffIDs), cfg.OS, cfg.Architecture)
	}
	if !slices.Equal(cfg.Config.Entrypoint, []string{"/aiopsctl"}) {
		t.Errorf("entry point %q", cfg.Config.Entrypoint)
	}
	for _, e := range []string{"GOTOOLCHAIN=local", "GOSUMDB=off", "GOFLAGS=-mod=readonly", "XDG_CACHE_HOME=/cache"} {
		if !slices.Contains(cfg.Config.Env, e) {
			t.Errorf("the image's environment %q lacks %s", cfg.Config.Env, e)
		}
	}
	if slices.ContainsFunc(cfg.Config.Env, func(e string) bool {
		return strings.HasPrefix(e, "GOPRIVATE=") || strings.HasPrefix(e, "GONOPROXY=") || strings.HasPrefix(e, "GOPROXY=")
	}) {
		t.Errorf("the image's environment %q sets GOPRIVATE, GONOPROXY or GOPROXY, which dist build passes", cfg.Config.Env)
	}
	tl, root := "THIRD_PARTY_LICENSES/"+Toolchain+"/", "cache/aiopsctl/toolchain/"+ToolchainVersion
	for name, mode := range map[string]int64{"aiopsctl": 0o755, root + "/bin/go": 0o755, root + "/LICENSE": 0o644, root + "/src/math/abs.go": 0o644} {
		if h := headers[name]; h == nil || h.Mode != mode {
			t.Errorf("%s: %+v, want mode %o", name, h, mode)
		}
	}
	if got := string(files[root+".sum"]); got != ToolchainSum {
		t.Errorf("the recorded sum of the extraction is %q", got)
	}
	for _, name := range []string{tl + "LICENSE", tl + "PATENTS", tl + "src/cmd/vendor/github.com/google/pprof/LICENSE",
		tl + "src/cmd/vendor/github.com/google/pprof/third_party/svgpan/LICENSE", tl + "lib/fips140/v1.0.0.zip/golang.org/fips140@v1.0.0/LICENSE",
		tl + "lib/time/README", "THIRD_PARTY_LICENSES/example.test/mit/LICENSE", "NOTICE", "aiopsctl.spdx.json"} {
		if _, ok := files[name]; !ok {
			t.Errorf("the image holds no %s", name)
		}
	}
	if offer := string(files["SOURCE-OFFER"]); !strings.Contains(offer, "no package under GPL, LGPL or AGPL") {
		t.Errorf("SOURCE-OFFER: %q", offer)
	}
	var sbom struct {
		Packages []struct{ Name, VersionInfo, LicenseConcluded string }
	}
	if err := json.Unmarshal(files["builder.spdx.json"], &sbom); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range sbom.Packages {
		got = append(got, strings.Join(strings.Fields(p.Name+" "+p.VersionInfo+" "+p.LicenseConcluded), " "))
	}
	for _, want := range []string{"builder NOASSERTION", Toolchain + " " + ToolchainVersion + " BSD-3-Clause", "aiopsctl NOASSERTION", "example.test/mit v1.0.0 MIT",
		"github.com/google/pprof v0.0.0-20251007162407-5df77e3f7d1d Apache-2.0", "golang.org/fips140 v1.0.0 BSD-3-Clause"} {
		if !slices.Contains(got, want) {
			t.Errorf("the image SBOM lacks %q: %q", want, got)
		}
	}
}

// The licence and notice files of the tree of the pin, as a walk with the names of
// ADR-044 decision 3 finds them, with the two module zips under lib/fips140/ opened and
// the IANA statement of lib/time/README (ADR-044 decision 5, What was read): 36 files
// outside the zips, the svgpan text among them, one LICENSE in each zip, and the README.
func TestToolchainLicenceFiles(t *testing.T) {
	files, err := toolchainFiles(toolchainTree(t))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		got = append(got, f.Path)
	}
	want := []string{"LICENSE", "PATENTS", "lib/time/README", "src/crypto/internal/boring/LICENSE",
		"src/cmd/vendor/github.com/google/pprof/third_party/svgpan/LICENSE", "src/runtime/testdata/testgoroutineleakprofile/goker/LICENSE",
		"src/cmd/compile/internal/ssa/_gen/vendor/golang.org/x/tools/LICENSE",
		"lib/fips140/v1.0.0-c2097c7c.zip/golang.org/fips140@v1.0.0-c2097c7c/LICENSE", "lib/fips140/v1.26.0.zip/golang.org/fips140@v1.26.0/LICENSE"}
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("the walk finds no %s", w)
		}
	}
	if len(got) != 39 {
		t.Errorf("the walk finds %d files, want 39 (36, two in the zips and the README):\n%s", len(got), strings.Join(got, "\n"))
	}
}

// The id of the release key that a release binary carries is the id that boot reads
// from trust.yaml for the same key (ADR-020 decisions 4 and 13); a key that is not
// ECDSA P-256 is refused.
func TestReleaseKeyID(t *testing.T) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	pub := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	tr, err := boot.ReadTrust(fstest.MapFS{"trust.yaml": {Data: fmt.Appendf(nil, "distributions:\n  - key: |\n%s", indent(string(pub)))}})
	if err != nil {
		t.Fatal(err)
	}
	if id, err := keyID(pub); err != nil || id != tr.Distributions[0] {
		t.Errorf("keyID %q, %v; boot reads %q", id, err, tr.Distributions[0])
	}
	k384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	der, _ = x509.MarshalPKIXPublicKey(&k384.PublicKey)
	if _, err := keyID(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})); err == nil {
		t.Error("a P-384 key is taken as the release key")
	}
}

func indent(s string) string {
	return "      " + strings.ReplaceAll(strings.TrimSuffix(s, "\n"), "\n", "\n      ") + "\n"
}

// The second phase of a release build (ADR-020 decision 13) refuses, before it builds,
// a release key that is not ECDSA P-256, a bundle that is not a Sigstore bundle, a
// manifest that does not read and a definition of the builder image that names another
// toolchain.
func TestSecondPhaseInputs(t *testing.T) {
	def, err := os.ReadFile(definitionFile)
	if err != nil {
		t.Fatal(err)
	}
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	pub := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	bundle := []byte(`{"mediaType": "application/vnd.dev.sigstore.bundle.v0.3+json"}`)
	input, err := os.ReadFile(filepath.Join("..", "..", "dist", "dist.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join("..", "..", "dist", "distribution.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		s    Signed
		want string
	}{
		{"a key that is not P-256", Signed{Manifest: manifest, Bundle: bundle, Key: []byte("not a key"), Definition: def}, "release public key"},
		{"another bundle", Signed{Manifest: manifest, Bundle: []byte(`{"mediaType": "text/plain"}`), Key: pub, Definition: def}, "Sigstore bundle"},
		{"a manifest that does not read", Signed{Manifest: []byte("plugins: 1\n"), Bundle: bundle, Key: pub, Definition: def}, "manifest"},
		{"another toolchain", Signed{Manifest: manifest, Bundle: bundle, Key: pub, Definition: []byte(strings.Replace(string(def), "go1.26.8", "go1.27.4", 1))}, "go1.27.4"},
	} {
		s := c.s
		_, err := Build(context.Background(), Request{Input: input, Root: "v0.2.0", Signed: &s, ModCache: t.TempDir(), Cache: t.TempDir(), Out: filepath.Join(t.TempDir(), "out")})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

// docker is a stand-in for the container runtime: it records its arguments and writes
// the manifest that the image's aiopsctl writes into the bound output directory.
const docker = `#!/bin/sh
printf '%s\n' "$@" > "$DOCKER_ARGS"
for a in "$@"; do case "$a" in *:/work/build) printf 'manifest\n' > "${a%:/work/build}/distribution.yaml";;
  *:/scratch) find "${a%:/scratch}" -type f > "$DOCKER_ARGS.scratch";; esac; done
`

// On the image path, dist build reads the release artifact that dist.yaml names, by
// digest and through --mirror, takes the builder image that its annotation names in
// the same repository, and runs that image's aiopsctl with the same command and
// --builder local: no network, the caller's user, dist.yaml and the module cache bound
// read-only, the output directory bound read-write, and the Go environment of ADR-060
// decision 21 (ADR-044 decisions 1, 9 and 17; ADR-061 decision 21), with the release
// artifact in its scratch directory, which a mirror names, so that it makes the check of
// the local path with no network. A release artifact with no such annotation is
// refused.
func TestImagePath(t *testing.T) {
	reg := ocitest.New(t, ocitest.Options{})
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(docker), 0o755); err != nil {
		t.Fatal(err)
	}
	args := filepath.Join(t.TempDir(), "args")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKER_ARGS", args)
	builderDigest := "sha256:" + strings.Repeat("b", 64)
	release := func(tag string, annotations map[string]string) string {
		layer := reg.Blob("aiops/release", "application/vnd.aiops.distribution.manifest.v1+yaml", []byte("manifest\n"))
		empty := reg.Blob("aiops/release", ocitest.EmptyType, []byte("{}"))
		m, _ := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": ocitest.ManifestType, "artifactType": "application/vnd.aiops.distribution.v1",
			"config": empty, "layers": []any{layer}, "annotations": annotations})
		return reg.Manifest("aiops/release", tag, ocitest.ManifestType, m).Digest
	}
	site := func(ref string) string {
		dir := t.TempDir()
		p := filepath.Join(dir, "dist.yaml")
		if err := os.WriteFile(p, []byte("release: "+ref+"\nplugins: []\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	modcache := t.TempDir()
	good := release("latest", map[string]string{BuilderAnnotation: builderDigest})
	yaml := site("registry.example/aiops/release@" + good)
	r, err := RunImage(context.Background(), ImageRequest{DistYAML: yaml, ModCache: modcache, AllowBump: []string{"example.com/x"},
		Mirrors: map[string]string{"registry.example/aiops": reg.Host + "/aiops"}, Progress: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(filepath.Dir(yaml), "build")
	if string(r.Manifest) != "manifest\n" || r.Image != filepath.Join(out, "image") || r.Aiopsctl != filepath.Join(out, "aiopsctl", "aiopsctl") {
		t.Errorf("the result %+v", r)
	}
	raw, err := os.ReadFile(args)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(raw)), "\n")
	cmd := strings.Join(got, " ")
	for _, want := range []string{"run --rm --network none", fmt.Sprintf("--user %d:%d", os.Getuid(), os.Getgid()),
		"-v " + yaml + ":/work/dist.yaml:ro", "-v " + modcache + ":/modcache:ro", "-v " + out + ":/work/build ",
		"-e GOPROXY=file:///modcache", "-e GOSUMDB=off", "-e GOTOOLCHAIN=local", "-e GOFLAGS=-mod=readonly",
		reg.Host + "/aiops/release@" + builderDigest + " dist build /work/dist.yaml --modcache /modcache --builder local",
		"--mirror registry.example/aiops/release=/scratch/release --allow-bump example.com/x"} {
		if !strings.Contains(cmd+" ", want) {
			t.Errorf("the container runtime ran without %q:\n%s", want, cmd)
		}
	}
	// The image's aiopsctl reads the release artifact from the scratch directory, through
	// the one mirror it is given, and is given no registry (ADR-044 decision 1).
	scratch, err := os.ReadFile(args + ".scratch")
	if strings.Count(cmd, "--mirror") != 1 || strings.Contains(cmd, "registry.example/aiops=") || err != nil ||
		!strings.Contains(string(scratch), "/release/blobs/sha256/"+strings.TrimPrefix(good, "sha256:")) {
		t.Errorf("the image's aiopsctl is given another mirror, or no release artifact in the scratch directory (%v):\n%s\n%s", err, cmd, scratch)
	}
	for _, c := range []struct{ name, ref, want string }{
		{"no annotation", reg.Host + "/aiops/release@" + release("", nil), BuilderAnnotation},
		{"a digest the registry does not hold", reg.Host + "/aiops/release@sha256:" + strings.Repeat("c", 64), "release"},
	} {
		if _, err := RunImage(context.Background(), ImageRequest{DistYAML: site(c.ref), ModCache: modcache, Progress: io.Discard}); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	// plugin smoke names the release artifact by digest, as dist.yaml does and as the local
	// path refuses otherwise: one named by a tag, which the registry resolves to the same
	// artifact, is refused before any container runs with the plugin directory and the
	// module cache bound (08:401).
	if err := os.Remove(args); err != nil {
		t.Fatal(err)
	}
	tag := reg.Host + "/aiops/release:latest"
	_, err = RunImage(context.Background(), ImageRequest{Smoke: t.TempDir(), Against: tag, ModCache: modcache, Progress: io.Discard})
	if _, serr := os.Stat(args); err == nil || !strings.Contains(err.Error(), "by digest") || !strings.Contains(err.Error(), "08:401") || serr == nil {
		t.Errorf("plugin smoke against %s: %v; the container runtime ran: %v", tag, err, serr == nil)
	}
}
