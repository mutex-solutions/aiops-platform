// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"

	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
)

// layoutImage writes the OCI image layout that dist build writes: one image tagged
// latest with its distribution manifest as a referrer (ADR-044 decision 1).
func layoutImage(t *testing.T, manifest []byte) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "image")
	ctx := context.Background()
	s, err := oci.NewWithContext(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	push := func(mediaType string, data []byte) ocispec.Descriptor {
		d := content.NewDescriptorFromBytes(mediaType, data)
		if err := s.Push(ctx, d, bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
		return d
	}
	layer, cfg := push(ocispec.MediaTypeImageLayer, []byte("the server")), push(ocispec.MediaTypeImageConfig, []byte(`{"architecture":"amd64","os":"linux"}`))
	img, err := oras.PackManifest(ctx, s, oras.PackManifestVersion1_1, "", oras.PackManifestOptions{Layers: []ocispec.Descriptor{layer}, ConfigDescriptor: &cfg})
	if err == nil {
		err = s.Tag(ctx, img, "latest")
	}
	if err == nil {
		_, err = oras.PackManifest(ctx, s, oras.PackManifestVersion1_1, def.DistributionType,
			oras.PackManifestOptions{Subject: &img, Layers: []ocispec.Descriptor{push(def.DistributionLayer, manifest)}})
	}
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// dist sign and dist use take the OCI image layout that dist build writes as the image
// (ADR-044 decision 1; operations 0.3): sign writes the manifest's bundle and the
// image's signature into the layout as referrers, and use verifies both with a key of
// distributions and names the image by its path and digest in image.yaml. A path that
// is no layout is refused.
func TestDistSignAndUseOfALayout(t *testing.T) {
	l := Library{LockWait: time.Second}
	home, prefix := siteHome(t, l, t.TempDir())
	dir := layoutImage(t, []byte("distVersion: 1\n"))
	w, err := l.SignDist(t.Context(), def.SignDistRequest{Caller: caller(), Image: dir, Key: prefix + ".key"})
	if err != nil || len(w.Paths) != 2 || !strings.HasPrefix(w.Paths[0], dir+"@sha256:") {
		t.Fatalf("SignDist = %v, %v", w, err)
	}
	ch, err := l.UseDist(t.Context(), def.UseRequest{Caller: caller(), Home: home, Image: dir})
	if err != nil || ch.Outcome != def.RestartRequired {
		t.Fatalf("UseDist = %+v, %v", ch, err)
	}
	if b, _ := os.ReadFile(filepath.Join(home, "image.yaml")); !strings.HasPrefix(string(b), "image: "+dir+"@sha256:") {
		t.Errorf("image.yaml %q", b)
	}
	if _, err := l.SignDist(t.Context(), def.SignDistRequest{Caller: caller(), Image: t.TempDir(), Key: prefix + ".key"}); err == nil || !strings.Contains(err.Error(), "not an OCI image layout") {
		t.Errorf("a directory that is no layout: %v", err)
	}
}
