// SPDX-License-Identifier: Apache-2.0

package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/internal/authoring"
	"github.com/mutex-solutions/aiops-platform/internal/builder"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/ocitest"
)

// The release build in two phases, the builder image and dist build on the image path
// (ADR-020 decision 13; ADR-044 decisions 1 to 6 and 17; ADR-060 decisions 21, 22 and
// 36). The first phase writes the manifest of the release from the repository's modules
// served at testVersion and stops; the second takes it back with its bundle, the
// release public key and the definition of the builder image under dist/, and writes
// the release server and the release aiopsctl, which embed the three, with their
// THIRD_PARTY_LICENSES/, NOTICE and SBOM; an OCI image layout that holds the release
// image with no base layer, the builder image, and the release artifact, the referrer
// of the release image with the manifest and its bundle, annotated with the digest of
// the builder image. The release aiopsctl boots on its built-in record with no
// trust.yaml. Once the layout is in a registry, a site's dist build of an echoer module
// on that release runs, on the image path, the builder image the release artifact names,
// and writes the same server, site aiopsctl and manifest as the local path from the same
// inputs, the same licence report and the same SBOM but its creation time (ADR-044
// decision 3). It needs the pinned toolchain module and a container runtime, so -short
// skips it and the CI job builder-image runs it.
func TestReleaseAndBuilderImage(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the release twice and a site on both paths with the pinned toolchain; the CI job builder-image runs it")
	}
	host := goEnv(t, "GOMODCACHE")
	if _, err := os.Stat(filepath.Join(host, "cache", "download", builder.Toolchain, "@v", builder.ToolchainVersion+".zip")); err != nil {
		t.Skipf("the module cache holds no %s@%s: go mod download it to run this case (the CI job builder-image does)", builder.Toolchain, builder.ToolchainVersion)
	}
	if err := exec.Command("docker", "version").Run(); err != nil {
		t.Skipf("no container runtime answers docker version (%v): the image path runs the builder image in one; the CI job builder-image has one", err)
	}
	ctx, repo := context.Background(), abs(t, filepath.Join("..", ".."))
	src, cache, scratch := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("GOPROXY", "")
	if err := builder.Serve(repo, testVersion, src); err != nil {
		t.Fatal(err)
	}
	fill(t, repo, filepath.Join(host, "cache", "download"), src)
	echo := plugin(t, src, scratch)
	input, err := os.ReadFile(filepath.Join(repo, "dist", "dist.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	def, err := os.ReadFile(filepath.Join(repo, "dist", "builder", "image.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	var progress bytes.Buffer
	q := builder.Request{Input: input, Root: testVersion, ModCache: src, Cache: filepath.Join(cache, "aiopsctl"), Progress: &progress}
	q.Out = filepath.Join(t.TempDir(), "first")
	first, err := builder.Build(ctx, q)
	if err != nil {
		t.Fatalf("the first phase: %v\n%s", err, progress.String())
	}
	if entries, _ := os.ReadDir(q.Out); len(entries) > 0 {
		t.Errorf("the first phase wrote %d entries: it stops at the manifest", len(entries))
	}
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	sum := sha256.Sum256(der)
	keyID := base64.StdEncoding.EncodeToString(sum[:])
	bundle := []byte(`{"mediaType": "application/vnd.dev.sigstore.bundle.v0.3+json"}`)
	q.Out = filepath.Join(t.TempDir(), "release")
	q.Signed = &builder.Signed{Manifest: first.Manifest, Bundle: bundle, Key: pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), Definition: def}
	second, err := builder.Build(ctx, q)
	if err != nil {
		t.Fatalf("the second phase: %v\n%s", err, progress.String())
	}
	if !bytes.Equal(second.Manifest, first.Manifest) {
		t.Error("the second phase wrote another manifest")
	}
	d, err := boot.ReadDistribution("the release manifest", first.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"aiops/aiops", "aiops/NOTICE", "aiops/aiops.spdx.json", "aiops/THIRD_PARTY_LICENSES/golang.org/toolchain/LICENSE",
		"aiopsctl/aiopsctl", "aiopsctl/NOTICE", "aiopsctl/aiopsctl.spdx.json", "aiopsctl/THIRD_PARTY_LICENSES/golang.org/x/mod/PATENTS", "image/index.json"} {
		if _, err := os.Stat(filepath.Join(q.Out, filepath.FromSlash(f))); err != nil {
			t.Errorf("the second phase wrote no %s: %v", f, err)
		}
	}
	cmd := exec.Command(filepath.Join(q.Out, "aiopsctl", "aiopsctl"), "profile", "verify")
	cmd.Env = []string{"AIOPS_HOME=" + t.TempDir(), "HOME=" + scratch, "PATH=" + os.Getenv("PATH")}
	b, err := cmd.CombinedOutput()
	if want := fmt.Sprintf("the default release root, key %s, is trusted; the built-in record of %s is in use", keyID, d.Digest); err != nil || !strings.Contains(string(b), want) {
		t.Errorf("the release aiopsctl with no trust.yaml: %v, want %q\n%s", err, want, b)
	}

	layout := filepath.Join(q.Out, "image")
	reg := ocitest.New(t, ocitest.Options{})
	refs := pushLayout(t, reg, "aiops/release", layout)
	img, bld, rel := refs["latest"], refs["builder"], refs[""]
	if img.digest == "" || bld.digest == "" || rel.digest == "" {
		t.Fatalf("the layout holds the release image %q, the builder image %q and the release artifact %q", img.digest, bld.digest, rel.digest)
	}
	if rel.subject != img.digest || rel.annotations[builder.BuilderAnnotation] != bld.digest || !sameList(rel.layers, "application/vnd.aiops.distribution.manifest.v1+yaml", "application/vnd.dev.sigstore.bundle.v0.3+json") {
		t.Errorf("the release artifact %+v: a referrer of %s with the manifest and its bundle, annotated with the builder image %s", rel, img.digest, bld.digest)
	}
	for _, c := range []struct {
		m    ref
		want []string
	}{
		{img, []string{"aiops", "NOTICE", "aiops.spdx.json", "THIRD_PARTY_LICENSES/golang.org/toolchain/LICENSE"}},
		{bld, []string{"aiopsctl", "NOTICE", "builder.spdx.json", "SOURCE-OFFER", "cache/aiopsctl/toolchain/" + builder.ToolchainVersion + "/bin/go",
			"THIRD_PARTY_LICENSES/golang.org/toolchain/src/cmd/vendor/github.com/google/pprof/third_party/svgpan/LICENSE"}},
	} {
		if len(c.m.layers) != 1 {
			t.Errorf("%s has %d layers: one, on no base layer", c.m.digest, len(c.m.layers))
			continue
		}
		names := layerNames(t, layout, c.m.layerDigests[0])
		for _, w := range c.want {
			if !names[w] {
				t.Errorf("the image %s holds no %s", c.m.digest, w)
			}
		}
	}

	build := func(builderPath string) (string, launch.Built) {
		site := t.TempDir()
		yaml := fmt.Sprintf("release: %s/aiops/release@%s\nplugins:\n  - {module: %s, version: v0.1.0, sum: %s}\n", reg.Host, rel.digest, echo.path, echo.sum)
		if err := os.WriteFile(filepath.Join(site, "dist.yaml"), []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}
		var progress bytes.Buffer
		tools := authoring.New()
		tools.Signed, tools.Progress = first.Manifest, &progress
		b, err := tools.Build(ctx, launch.BuildRequest{DistYAML: filepath.Join(site, "dist.yaml"), ModCache: src, Builder: builderPath})
		if err != nil {
			t.Fatalf("dist build --builder %s: %v\n%s", builderPath, err, progress.String())
		}
		return filepath.Join(site, "build"), b
	}
	t.Cleanup(func() { _ = exec.Command("docker", "image", "rm", reg.Host+"/aiops/release@"+bld.digest).Run() })
	imageOut, built := build("image")
	localOut, _ := build("local")
	if built.Image != filepath.Join(imageOut, "image") || built.Aiopsctl != filepath.Join(imageOut, "aiopsctl", "aiopsctl") || len(built.Manifest) == 0 {
		t.Errorf("the image path built %+v", built)
	}
	a, l := tree(t, imageOut), tree(t, localOut)
	for name, sum := range l {
		if a[name] != sum {
			t.Errorf("%s differs between the image path and the local path", name)
		}
	}
	if len(a) != len(l) {
		t.Errorf("the image path wrote %d files and the local path %d", len(a), len(l))
	}
}

// ref is a manifest of a layout: its digest, subject, annotations and layers.
type ref struct {
	digest, subject      string
	annotations          map[string]string
	layers, layerDigests []string
}

func sameList(got []string, want ...string) bool {
	return strings.Join(got, " ") == strings.Join(want, " ")
}

// pushLayout copies every blob and manifest of the OCI image layout dir into the
// repository name of reg, each tag of the layout too, and returns its manifests by tag,
// the referrer of the distribution under "".
func pushLayout(t *testing.T, reg *ocitest.Registry, name, dir string) map[string]ref {
	t.Helper()
	var index struct {
		Manifests []struct {
			Digest      string
			Annotations map[string]string
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err == nil {
		err = json.Unmarshal(raw, &index)
	}
	if err != nil {
		t.Fatal(err)
	}
	tags := map[string]string{}
	for _, m := range index.Manifests {
		if tag := m.Annotations["org.opencontainers.image.ref.name"]; tag != "" {
			tags[m.Digest] = tag
		}
	}
	out := map[string]ref{}
	blobs := filepath.Join(dir, "blobs", "sha256")
	entries, err := os.ReadDir(blobs)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(blobs, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var m struct {
			MediaType, ArtifactType string
			Subject                 *struct{ Digest string }
			Annotations             map[string]string
			Layers                  []struct{ MediaType, Digest string }
		}
		if len(data) > 1<<20 || json.Unmarshal(data, &m) != nil || m.MediaType != ocitest.ManifestType {
			reg.Blob(name, "application/octet-stream", data)
			continue
		}
		digest := "sha256:" + e.Name()
		reg.Manifest(name, tags[digest], m.MediaType, data)
		r := ref{digest: digest, annotations: m.Annotations}
		if m.Subject != nil {
			r.subject = m.Subject.Digest
		}
		for _, l := range m.Layers {
			r.layers, r.layerDigests = append(r.layers, l.MediaType), append(r.layerDigests, l.Digest)
		}
		switch {
		case tags[digest] != "":
			out[tags[digest]] = r
		case m.ArtifactType == "application/vnd.aiops.distribution.v1":
			out[""] = r
		}
	}
	return out
}

// layerNames returns the names of the entries of the layer digest of the layout dir.
func layerNames(t *testing.T, dir, digest string) map[string]bool {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, "blobs", "sha256", strings.TrimPrefix(digest, "sha256:")))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	names := map[string]bool{}
	for tr := tar.NewReader(f); ; {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return names
		}
		if err != nil {
			t.Fatal(err)
		}
		names[strings.TrimSuffix(h.Name, "/")] = true
	}
}

// tree returns the SHA-256 of each file under dir by its path, but the image layout,
// whose layer holds an SBOM with its creation time, and each SBOM read without its
// creation time and the namespace that holds it (ADR-044 decision 3).
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		rel, _ := filepath.Rel(dir, p)
		switch {
		case err != nil:
			return err
		case e.IsDir() && rel == "image":
			return filepath.SkipDir
		case e.IsDir():
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.HasSuffix(p, ".spdx.json") {
			var doc map[string]any
			if err := json.Unmarshal(data, &doc); err != nil {
				return err
			}
			delete(doc, "creationInfo")
			delete(doc, "documentNamespace")
			data, _ = json.Marshal(doc)
		}
		out[filepath.ToSlash(rel)] = fmt.Sprintf("%x", sha256.Sum256(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
