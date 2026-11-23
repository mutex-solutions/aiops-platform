package boot

import (
	"archive/tar"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"
)

// The readers of what plugin install wrote into $AIOPS_HOME (ADR-047 decisions 5, 8
// and 16): plugins.lock of a profile, and the files it kept of each artifact.

// LockEntry is one entry of plugins.lock: the plugin id, the digest of the artifact's
// OCI image manifest and, for an artifact with a content layer, that layer's digest.
type LockEntry struct{ ID, Digest, Content string }

// ReadLock reads profiles/<profile>/plugins.lock of home (ADR-047 decisions 8 and 16):
//
//	lockVersion: 1
//	artifacts:                 sorted by id
//	  - {id: <plugin id>, digest: sha256:<hex>, content: sha256:<hex>}   content: optional
//
// In-process code has no entry. With no such file the profile locks nothing.
func ReadLock(home fs.FS, profile string) ([]LockEntry, error) {
	if !okIdent(profile) {
		return nil, &Error{File: profile, Rule: RuleSyntaxID, Msg: "profile name is not a name"}
	}
	r, root, err := readHome(home, path.Join("profiles", profile, "plugins.lock"), RuleLock)
	if r == nil {
		return nil, err
	}
	f := r.fields(root, "plugins.lock", []string{"lockVersion", "artifacts"}, "lockVersion", "artifacts")
	if v := r.num(f["lockVersion"], "lockVersion"); r.err == nil && v != 1 {
		r.fail(f["lockVersion"], "lockVersion %d is unknown: this reader knows 1", v)
	}
	out := []LockEntry{}
	for _, c := range r.list(f["artifacts"], "artifacts") {
		e := r.fields(c, "an entry of artifacts", []string{"id", "digest", "content"}, "id", "digest")
		out = append(out, LockEntry{ID: r.word(e["id"], "id", okManifestID), Digest: r.word(e["digest"], "digest", okDigest),
			Content: r.word(e["content"], "content", okDigest)})
	}
	r.sorted(f["artifacts"], "artifacts", len(out), func(i int) bool { return out[i-1].ID < out[i].ID })
	if r.err != nil {
		return nil, r.err
	}
	return out, nil
}

// The media types of a plugin artifact that boot reads (ADR-047 decisions 1 and 20).
const (
	ociManifest   = "application/vnd.oci.image.manifest.v1+json"
	ociEmpty      = "application/vnd.oci.empty.v1+json"
	artifactType  = "application/vnd.aiops.plugin.v1"
	layerManifest = "application/vnd.aiops.plugin.manifest.v1+yaml"
	layerBundle   = "application/vnd.aiops.plugin.bundle.v1+yaml"
	layerContent  = "application/vnd.aiops.plugin.content.v1.tar"
)

// Kept is what install kept of an artifact: its plugin.yaml and bundle.yaml, and the
// digest of its content layer, "" for an artifact with none, which plugins.lock
// records beside the artifact's digest.
type Kept struct {
	Manifest, Bundle []byte
	Content          string
}

// ReadKept reads what install kept of the artifact id whose OCI image manifest has
// digest: the digest plugins.lock names, or for in-process code the one install
// recorded (ADR-047 decision 18). bundles/<id>/artifact.json must hash to digest, and
// plugin.yaml and bundle.yaml beside it, and the tree content/<id>/ written in the
// form of ADR-047 decision 3, each to its layer of that manifest. A layer of a type
// boot does not read is ignored (ADR-047 decision 1). Any difference is a RuleKept
// error: the files are not those whose signer install verified.
func ReadKept(home fs.FS, id, digest string) (Kept, error) {
	dir := path.Join("bundles", id)
	fail := func(format string, a ...any) (Kept, error) {
		return Kept{}, &Error{File: dir, Rule: RuleKept, Msg: fmt.Sprintf(format, a...)}
	}
	if !okManifestID(id) || !okDigest(digest) {
		return fail("%q at %q is not a plugin id at sha256:<hex>", id, digest)
	}
	art, err := readFile(File{FS: home, Path: dir + "/artifact.json"})
	if err != nil {
		return fail("artifact.json: %v", err)
	}
	if digestOf(art) != digest {
		return fail("artifact.json does not hash to %s, the digest install verified", digest)
	}
	var m struct {
		SchemaVersion           int
		MediaType, ArtifactType string
		Config                  struct{ MediaType string }
		Layers                  []struct{ MediaType, Digest string }
	}
	bad := json.Unmarshal(art, &m) != nil || m.SchemaVersion != 2 || m.MediaType != ociManifest || m.ArtifactType != artifactType ||
		m.Config.MediaType != ociEmpty
	layers := map[string]string{}
	for _, l := range m.Layers {
		_, twice := layers[l.MediaType]
		bad = bad || twice || !okDigest(l.Digest)
		layers[l.MediaType] = l.Digest
	}
	if bad {
		return fail("artifact.json is not the OCI image manifest of a plugin artifact, each layer type once (ADR-047 decision 1)")
	}
	var k Kept
	for _, f := range []struct {
		name, layer string
		out         *[]byte
	}{{"plugin.yaml", layerManifest, &k.Manifest}, {"bundle.yaml", layerBundle, &k.Bundle}} {
		if *f.out, err = readFile(File{FS: home, Path: dir + "/" + f.name}); err != nil {
			return fail("%s: %v", f.name, err)
		}
		if want := layers[f.layer]; digestOf(*f.out) != want {
			return fail("%s does not hash to %q, its layer of the artifact", f.name, want)
		}
	}
	if want, ok := layers[layerContent]; ok {
		h := sha256.New()
		if err := writeContent(h, home, path.Join("content", id)); err != nil {
			return fail("content/%s: %v", id, err)
		}
		if got := "sha256:" + hex.EncodeToString(h.Sum(nil)); got != want {
			return fail("content/%s hashes to %s and its layer of the artifact is %s (ADR-047 decision 3)", id, got, want)
		}
		k.Content = want
	}
	return k, nil
}

// writeContent writes the tree dir of home to w as the tar of ADR-047 decision 3: one
// entry per regular file, sorted by path in byte order, each path relative to dir,
// mode 0644, owner and group 0 with no names, time 0, a ustar header with a pax
// header only for a path that ustar cannot hold, and the two zero blocks that end an
// archive. A file of any other kind fails.
func writeContent(w io.Writer, home fs.FS, dir string) error {
	var files []string
	err := fs.WalkDir(home, dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (p == dir || !d.Type().IsRegular()) {
			err = fmt.Errorf("%s is not a regular file of a directory", p)
		}
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}
		return err
	})
	slices.Sort(files)
	tw := tar.NewWriter(w)
	for _, p := range files {
		if err == nil {
			err = tarFile(tw, home, p, strings.TrimPrefix(p, dir+"/"))
		}
	}
	return cmp.Or(err, tw.Close())
}

// tarFile writes the file p of home to tw under name.
func tarFile(tw *tar.Writer, home fs.FS, p, name string) error {
	f, err := home.Open(p)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err == nil {
		err = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o644, Size: info.Size(), ModTime: time.Unix(0, 0)})
	}
	if err == nil {
		_, err = io.Copy(tw, f)
	}
	return err
}

// digestOf returns "sha256:" and the lowercase hex of the SHA-256 of b.
func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
