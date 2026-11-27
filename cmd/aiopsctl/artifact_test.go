package main

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/ocitest"
)

// A pack with a bundle that inserts nothing and with content, which the gate of the
// release this aiopsctl was built from admits: the release has no instantiable plugin
// for a row of a pack to name (ADR-060 decision 38).
var packFiles = map[string]string{
	"plugin.yaml": "manifestVersion: \"1.0\"\nid: acme/connector-x\nversion: 0.1.0\ntitle: Acme connector for X\nlicense: Apache-2.0\n" +
		"publisher: {id: acme}\nstability: experimental\nscope: [process]\nruntimes: []\nrequires: {definitions: {}}\n",
	"bundle.yaml":                 "[]\n",
	"content/runbooks/restart.md": "# Restart\n",
	"content/labels.yaml":         "service: x\n",
	"fixtures/echo.json":          "{}\n",
}

// kept returns, by slash path under a home, what plugin install keeps of the artifact
// in the layout dir (ADR-047 decision 5): bundles/<id>/ with the image manifest as
// artifact.json, plugin.yaml and bundle.yaml, and the content tree under
// content/<id>/, unpacked from the content layer; and the artifact's digest.
func kept(t *testing.T, dir, id string) (fstest.MapFS, string) {
	t.Helper()
	blob := func(digest string) []byte {
		b, err := os.ReadFile(filepath.Join(dir, "blobs", "sha256", strings.TrimPrefix(digest, "sha256:")))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var index struct {
		Manifests []struct{ Digest, ArtifactType string }
	}
	b, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err == nil {
		err = json.Unmarshal(b, &index)
	}
	if err != nil {
		t.Fatal(err)
	}
	home, digest := fstest.MapFS{}, ""
	for _, m := range index.Manifests {
		if m.ArtifactType == "application/vnd.aiops.plugin.v1" {
			digest = m.Digest
		}
	}
	art := blob(digest)
	var m struct {
		Layers []struct{ MediaType, Digest string }
	}
	if err := json.Unmarshal(art, &m); err != nil {
		t.Fatal(err)
	}
	home["bundles/"+id+"/artifact.json"] = &fstest.MapFile{Data: art}
	for _, l := range m.Layers {
		switch l.MediaType {
		case "application/vnd.aiops.plugin.manifest.v1+yaml":
			home["bundles/"+id+"/plugin.yaml"] = &fstest.MapFile{Data: blob(l.Digest)}
		case "application/vnd.aiops.plugin.bundle.v1+yaml":
			home["bundles/"+id+"/bundle.yaml"] = &fstest.MapFile{Data: blob(l.Digest)}
		case "application/vnd.aiops.plugin.content.v1.tar":
			r := tar.NewReader(bytes.NewReader(blob(l.Digest)))
			for {
				h, err := r.Next()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				data, _ := io.ReadAll(r)
				home["content/"+id+"/"+h.Name] = &fstest.MapFile{Data: data}
			}
		}
	}
	return home, digest
}

// What plugin pack writes is what boot reads of an installed artifact (ADR-047
// decisions 1, 3, 5 and 18): the manifest, the bundle and the content tree, kept as
// install keeps them, hash to the artifact's digest and to its layers, the content
// tree in the canonical form of boot/install.go. pack runs the real toolbox: boot's
// reader of plugin.yaml and the gate of plugin check step 7 on the release this
// aiopsctl embeds. sign, push to a registry inside the test, inspect and verify then
// answer through the cli profile.
func TestPackWritesWhatBootReads(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	for name, data := range packFiles {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	env := map[string]string{"PWD": dir, "AIOPS_HOME": home, "AIOPS_KEY_PASSWORD": "test"}
	r := do(t, os.DirFS("/"), env, "plugin", "pack")
	if r.code != 0 || r.out != "" || !strings.Contains(r.errOut, filepath.Join(dir, "artifact")) {
		t.Fatalf("plugin pack: exit %d, stdout %q; stderr:\n%s", r.code, r.out, r.errOut)
	}
	fsys, digest := kept(t, filepath.Join(dir, "artifact"), "acme/connector-x")
	k, err := boot.ReadKept(fsys, "acme/connector-x", digest)
	if err != nil || string(k.Manifest) != packFiles["plugin.yaml"] || string(k.Bundle) != packFiles["bundle.yaml"] || !strings.HasPrefix(k.Content, "sha256:") {
		t.Fatalf("boot reads %+v, %v", k, err)
	}
	for _, args := range [][]string{{"key", "generate", "author"}, {"trust", "add", "author.pub", "--ids", "acme/*"}, {"plugin", "sign", "--key", "author.key"}} {
		if r := do(t, os.DirFS("/"), env, args...); r.code != 0 {
			t.Fatalf("%q: exit %d; stderr:\n%s", args, r.code, r.errOut)
		}
	}
	reg := ocitest.New(t, ocitest.Options{})
	r = do(t, os.DirFS("/"), env, "plugin", "push", reg.Host+"/connector-x")
	if spec := "oci://" + reg.Host + "/connector-x@" + digest; r.code != 0 || r.out != spec+"\n" {
		t.Fatalf("plugin push: exit %d, stdout %q, want %q; stderr:\n%s", r.code, r.out, spec, r.errOut)
	}
	spec := strings.TrimSpace(r.out)
	if r := do(t, os.DirFS("/"), env, "plugin", "inspect", spec); r.code != 0 || !strings.Contains(r.out, "id: acme/connector-x\n") ||
		!strings.Contains(r.out, "content: runbooks/restart.md\n") {
		t.Errorf("plugin inspect: exit %d, stdout %q; stderr:\n%s", r.code, r.out, r.errOut)
	}
	r = do(t, os.DirFS("/"), env, "plugin", "verify", spec)
	if r.code != 1 || !strings.Contains(r.out, "step signature: pass") || !strings.Contains(r.out, "step attestation: pass") ||
		!strings.Contains(r.out, "step suites: not-built") {
		t.Errorf("plugin verify: exit %d, stdout %q; stderr:\n%s", r.code, r.out, r.errOut)
	}
}
