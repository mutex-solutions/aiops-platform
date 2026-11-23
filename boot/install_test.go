package boot

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

const lockFile = `lockVersion: 1
artifacts:
  - {id: acme/connector-x, digest: "sha256:` + hexA + `", content: "sha256:` + hexB + `"}
  - {id: acme/remote-y, digest: "sha256:` + hexC + `"}
`

func TestReadLock(t *testing.T) {
	home := fstest.MapFS{"profiles/dev/plugins.lock": {Data: []byte(lockFile)}}
	got, err := ReadLock(home, "dev")
	want := []LockEntry{{"acme/connector-x", "sha256:" + hexA, "sha256:" + hexB}, {"acme/remote-y", "sha256:" + hexC, ""}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("%v\n got %+v\nwant %+v", err, got, want)
	}
	// One file per profile (ADR-047 decision 16): another profile locks nothing.
	if got, err := ReadLock(home, "cli"); got != nil || err != nil {
		t.Errorf("cli: %+v, %v", got, err)
	}
	if got, err := ReadLock(fstest.MapFS{"profiles/dev/plugins.lock": {Data: []byte("lockVersion: 1\nartifacts: []\n")}}, "dev"); err != nil || len(got) != 0 {
		t.Errorf("empty: %+v, %v", got, err)
	}
	for _, c := range []struct {
		name  string
		edits []string
		rule  Rule
		want  string
	}{
		{"another version", []string{"lockVersion: 1", "lockVersion: 2"}, RuleLock, "lockVersion 2 is unknown"},
		{"a version that is text", []string{"lockVersion: 1", `lockVersion: "1"`}, RuleType, "lockVersion must be a non-negative integer"},
		{"no artifacts", []string{"artifacts:", "locked:"}, RuleUnknownField, `"locked"`},
		{"no version", []string{"lockVersion: 1\n", ""}, RuleLock, "plugins.lock states lockVersion"},
		{"unsorted", []string{"acme/remote-y", "acme/a"}, RuleLock, "artifacts is a set"},
		{"an id twice", []string{"acme/remote-y", "acme/connector-x"}, RuleLock, "artifacts is a set"},
		{"an id", []string{"acme/remote-y", "remote-y"}, RuleLock, `id "remote-y"`},
		{"a digest", []string{`"sha256:` + hexC + `"`, hexC}, RuleLock, "digest"},
		{"a content digest", []string{`"sha256:` + hexB + `"`, `"sha256:B"`}, RuleLock, "content"},
		{"no digest", []string{`, digest: "sha256:` + hexC + `"`, ""}, RuleLock, "an entry of artifacts states digest"},
		{"a field of an entry", []string{"{id: acme/remote-y,", "{id: acme/remote-y, signer: x,"}, RuleUnknownField, `"signer"`},
	} {
		src := strings.NewReplacer(c.edits...).Replace(lockFile)
		if src == lockFile {
			t.Fatalf("%s: the edit changed nothing", c.name)
		}
		_, err := ReadLock(fstest.MapFS{"profiles/dev/plugins.lock": {Data: []byte(src)}}, "dev")
		var e *Error
		if !errors.As(err, &e) || e.Rule != c.rule || e.File != "profiles/dev/plugins.lock" || !strings.Contains(e.Msg, c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	for _, c := range []struct {
		profile string
		data    []byte
		rule    Rule
	}{{"../x", nil, RuleSyntaxID}, {"dev", make([]byte, MaxFileBytes+1), RuleLimit}, {"dev", []byte("lockVersion: 1\nartifacts: {}\n"), RuleType}} {
		var e *Error
		if _, err := ReadLock(fstest.MapFS{"profiles/dev/plugins.lock": {Data: c.data}}, c.profile); !errors.As(err, &e) || e.Rule != c.rule {
			t.Errorf("%s: %v", c.rule, err)
		}
	}
}

// contentTree is a content directory whose paths sort differently by byte order of
// the whole path and by directory ("a-b.txt" before "a/c.txt"), with a file name that
// a ustar header cannot hold.
func contentTree(dir string) fstest.MapFS {
	return fstest.MapFS{
		dir + "/b.txt":                       {Data: []byte("b\n")},
		dir + "/a/c.txt":                     {Data: []byte("c\n"), Mode: 0o755},
		dir + "/a-b.txt":                     {Data: []byte("ab\n"), ModTime: time.Unix(1e9, 0)},
		dir + "/" + strings.Repeat("l", 120): {Data: []byte("long\n")},
	}
}

// The content layer has one canonical form (ADR-047 decision 3), which the golden
// digest pins; reading it back shows each property the decision names.
func TestWriteContent(t *testing.T) {
	var b bytes.Buffer
	if err := writeContent(&b, contentTree("content/acme/x"), "content/acme/x"); err != nil {
		t.Fatal(err)
	}
	const golden = "sha256:9262f01cbf6b3a3854452d965f99cabb46eb6952c1d27e49ea4cf3eb8ffe1a6a"
	if got := digestOf(b.Bytes()); got != golden {
		t.Errorf("digest %s, want %s", got, golden)
	}
	if b.Len()%512 != 0 || !bytes.Equal(b.Bytes()[b.Len()-1024:], make([]byte, 1024)) {
		t.Errorf("the archive does not end with two zero blocks: %d bytes", b.Len())
	}
	tr := tar.NewReader(&b)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
		format := tar.FormatUSTAR
		if len(h.Name) > 100 {
			format = tar.FormatPAX
		}
		if h.Typeflag != tar.TypeReg || h.Mode != 0o644 || h.Uid != 0 || h.Gid != 0 || h.Uname != "" || h.Gname != "" ||
			!h.ModTime.Equal(time.Unix(0, 0)) || h.Format != format || len(h.PAXRecords) > 0 && !reflect.DeepEqual(h.PAXRecords, map[string]string{"path": h.Name}) {
			t.Errorf("header %+v", h)
		}
	}
	if want := []string{"a-b.txt", "a/c.txt", "b.txt", strings.Repeat("l", 120)}; !reflect.DeepEqual(names, want) {
		t.Errorf("names %q", names)
	}
}

const keptDir = "bundles/acme/connector-x/"

// keptHome returns a home in which install kept the pack acme/connector-x, with
// content and a layer of a type boot does not read, and the digest of its artifact.json
// after edit.
func keptHome(t *testing.T, edit func(art map[string]any)) (fstest.MapFS, string) {
	t.Helper()
	home := contentTree("content/acme/connector-x")
	home[keptDir+"plugin.yaml"] = &fstest.MapFile{Data: []byte(packManifest)}
	home[keptDir+"bundle.yaml"] = &fstest.MapFile{Data: []byte("- insert: []\n")}
	h := sha256.New()
	if err := writeContent(h, home, "content/acme/connector-x"); err != nil {
		t.Fatal(err)
	}
	layer := func(typ, digest string) map[string]any {
		return map[string]any{"mediaType": typ, "digest": digest, "size": 1}
	}
	art := map[string]any{"schemaVersion": 2, "mediaType": ociManifest, "artifactType": artifactType,
		"config": map[string]any{"mediaType": ociEmpty, "digest": "sha256:" + hexA, "size": 2},
		"layers": []any{layer(layerManifest, digestOf([]byte(packManifest))), layer(layerBundle, digestOf([]byte("- insert: []\n"))),
			layer(layerContent, "sha256:"+hex.EncodeToString(h.Sum(nil))), layer("application/spdx+json", "sha256:"+hexB)},
		"annotations": map[string]any{"org.opencontainers.image.ref.name": "0.1.0"}}
	if edit != nil {
		edit(art)
	}
	data, err := json.Marshal(art)
	if err != nil {
		t.Fatal(err)
	}
	home[keptDir+"artifact.json"] = &fstest.MapFile{Data: data}
	return home, digestOf(data)
}

// failOpen refuses to open one file that its directory lists.
type failOpen struct {
	fstest.MapFS
	name string
}

func (f failOpen) Open(name string) (fs.File, error) {
	if name == f.name {
		return nil, fs.ErrPermission
	}
	return f.MapFS.Open(name)
}

func TestReadKept(t *testing.T) {
	home, digest := keptHome(t, nil)
	got, err := ReadKept(home, "acme/connector-x", digest)
	layers := map[string]string{}
	var art struct {
		Layers []struct{ MediaType, Digest string }
	}
	_ = json.Unmarshal(home[keptDir+"artifact.json"].Data, &art)
	for _, l := range art.Layers {
		layers[l.MediaType] = l.Digest
	}
	want := Kept{Manifest: []byte(packManifest), Bundle: []byte("- insert: []\n"), Content: layers[layerContent]}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("%v\n got %+v\nwant %+v", err, got, want)
	}
	// An artifact with no content layer reads no content.
	noContent := func(art map[string]any) { art["layers"] = art["layers"].([]any)[:2] }
	home, digest = keptHome(t, noContent)
	if got, err := ReadKept(home, "acme/connector-x", digest); err != nil || got.Content != "" || got.Manifest == nil {
		t.Errorf("no content: %+v, %v", got, err)
	}
	set := func(k string, v any) func(map[string]any) { return func(art map[string]any) { art[k] = v } }
	layer := func(i int, k, v string) func(map[string]any) {
		return func(art map[string]any) { art["layers"].([]any)[i].(map[string]any)[k] = v }
	}
	content := "content/acme/connector-x/"
	for _, c := range []struct {
		name   string
		edit   func(map[string]any)
		change func(home fstest.MapFS) fs.FS
		want   string
	}{
		{"schemaVersion", set("schemaVersion", 1), nil, "not the OCI image manifest"},
		{"mediaType", set("mediaType", "application/vnd.oci.image.index.v1+json"), nil, "not the OCI image manifest"},
		{"artifactType", set("artifactType", "application/vnd.aiops.plugin.v2"), nil, "not the OCI image manifest"},
		{"config", set("config", map[string]any{"mediaType": "application/json"}), nil, "not the OCI image manifest"},
		{"not JSON", set("layers", "x"), nil, "not the OCI image manifest"},
		{"a layer twice", layer(3, "mediaType", layerBundle), nil, "not the OCI image manifest"},
		{"a layer digest", layer(3, "digest", "sha256:B"), nil, "not the OCI image manifest"},
		{"no manifest layer", layer(0, "mediaType", "application/x"), nil, `plugin.yaml does not hash to ""`},
		{"another bundle", nil, func(h fstest.MapFS) fs.FS { h[keptDir+"bundle.yaml"].Data = []byte("[]\n"); return h }, "bundle.yaml does not hash to"},
		{"no plugin.yaml", nil, func(h fstest.MapFS) fs.FS { delete(h, keptDir+"plugin.yaml"); return h }, "plugin.yaml: open"},
		{"no artifact.json", nil, func(h fstest.MapFS) fs.FS { delete(h, keptDir+"artifact.json"); return h }, "artifact.json: open"},
		{"another artifact.json", nil, func(h fstest.MapFS) fs.FS { h[keptDir+"artifact.json"].Data = []byte("{}"); return h },
			"artifact.json does not hash to"},
		{"changed content", nil, func(h fstest.MapFS) fs.FS { h[content+"b.txt"].Data = []byte("B\n"); return h }, "content/acme/connector-x hashes to"},
		{"added content", nil, func(h fstest.MapFS) fs.FS { h[content+"z"] = &fstest.MapFile{}; return h }, "content/acme/connector-x hashes to"},
		{"no content", nil, func(h fstest.MapFS) fs.FS {
			for p := range h {
				if strings.HasPrefix(p, "content/") {
					delete(h, p)
				}
			}
			return h
		}, "content/acme/connector-x: open content/acme/connector-x: file does not exist"},
		{"a link in content", nil, func(h fstest.MapFS) fs.FS { h[content+"a/l"] = &fstest.MapFile{Mode: fs.ModeSymlink}; return h },
			"content/acme/connector-x/a/l is not a regular file"},
		{"content is a file", nil, func(h fstest.MapFS) fs.FS {
			for p := range h {
				if strings.HasPrefix(p, "content/") {
					delete(h, p)
				}
			}
			h["content/acme/connector-x"] = &fstest.MapFile{}
			return h
		}, "content/acme/connector-x is not a regular file"},
		{"a file that does not open", nil, func(h fstest.MapFS) fs.FS { return failOpen{h, content + "a/c.txt"} }, "permission denied"},
	} {
		home, digest := keptHome(t, c.edit)
		var fsys fs.FS = home
		if c.change != nil {
			fsys = c.change(home)
		}
		_, err := ReadKept(fsys, "acme/connector-x", digest)
		var e *Error
		if !errors.As(err, &e) || e.Rule != RuleKept || e.File != "bundles/acme/connector-x" || !strings.Contains(e.Msg, c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	for id, digest := range map[string]string{"connector-x": "sha256:" + hexA, "acme/connector-x": hexA} {
		var e *Error
		if _, err := ReadKept(home, id, digest); !errors.As(err, &e) || e.Rule != RuleKept {
			t.Errorf("%s at %s: %v", id, digest, err)
		}
	}
}
