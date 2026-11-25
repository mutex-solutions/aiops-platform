package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/mod/sumdb/dirhash"
)

// zipOf writes a module zip of one file at p and returns its h1 hash.
func zipOf(t *testing.T, p, module, text string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	e, err := w.Create(module + "/LICENSE")
	if err == nil {
		_, err = e.Write([]byte(text))
	}
	if err = errors.Join(err, w.Close(), f.Close()); err != nil {
		t.Fatal(err)
	}
	h, err := dirhash.HashZip(p, dirhash.Hash1)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// release writes a release output directory: the signed manifest, whose modules are
// an MPL-2.0 module at sum and an MIT module with no zip, and the archive with a zip
// of the MPL-2.0 module when archived.
func release(t *testing.T, sum func(dir string) string) string {
	t.Helper()
	dir := t.TempDir()
	manifest := fmt.Sprintf(`distVersion: 1
plugins: {}
modules:
  - {path: example.com/mit, version: v1.0.0, sum: "h1:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=", licence: MIT}
  - {path: example.com/mpl, version: v1.2.0, sum: %q, licence: MPL-2.0 AND MIT}
packages: []
pins: []
bindings: {}
acceptLicenses: []
serves: {pluginApi: "0.1", kernel: {keys: {}, hooks: {}}, definitions: {}}
`, sum(dir))
	if err := os.WriteFile(filepath.Join(dir, "distribution.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The MPL-2.0 release check (ADR-044 decisions 6 and 14): a release image whose
// archive lacks the zip of a linked MPL-2.0 module, or holds one of another hash, fails
// it, naming the module; one whose archive holds it passes, and a module under another
// licence needs no zip.
func TestReleaseCheck(t *testing.T) {
	archived := func(dir string) string {
		return zipOf(t, filepath.Join(dir, "mpl-sources", "example.com_mpl@v1.2.0.zip"), "example.com/mpl@v1.2.0", "MPL-2.0")
	}
	for _, c := range []struct {
		name string
		sum  func(dir string) string
		want string // a substring of the one finding; empty means none
	}{
		{"archived", archived, ""},
		{"no zip", func(dir string) string {
			return zipOf(t, filepath.Join(t.TempDir(), "elsewhere.zip"), "example.com/mpl@v1.2.0", "MPL-2.0")
		}, "example.com/mpl v1.2.0"},
		{"a zip of another hash", func(dir string) string {
			archived(dir)
			return zipOf(t, filepath.Join(t.TempDir(), "other.zip"), "example.com/mpl@v1.2.0", "another text")
		}, "example.com/mpl v1.2.0"},
	} {
		t.Run(c.name, func(t *testing.T) {
			bad, err := check(release(t, c.sum))
			if err != nil {
				t.Fatal(err)
			}
			if c.want == "" && len(bad) > 0 || c.want != "" && (len(bad) != 1 || !strings.Contains(bad[0], c.want)) {
				t.Errorf("findings %q, want %q", bad, c.want)
			}
			if slices.ContainsFunc(bad, func(l string) bool { return strings.Contains(l, "example.com/mit") }) {
				t.Errorf("a module under MIT needs no zip: %q", bad)
			}
		})
	}
	if _, err := check(t.TempDir()); err == nil {
		t.Error("a directory with no signed manifest passes")
	}
}
