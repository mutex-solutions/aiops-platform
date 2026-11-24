// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	modzip "golang.org/x/mod/zip"
)

// Served returns the served copy of a go.mod of the repository at version (ADR-060
// decision 21; ADR-044 decision 23): each require line of a module of the repository
// names version, and no replace line is left; nothing else changes.
func Served(gomod []byte, version string) ([]byte, error) {
	f, err := modfile.Parse("go.mod", gomod, nil)
	if err != nil {
		return nil, err
	}
	for _, r := range f.Require {
		if slices.Contains(sdkModules, r.Mod.Path) {
			if err := f.AddRequire(r.Mod.Path, version); err != nil {
				return nil, err
			}
		}
	}
	for _, r := range slices.Clone(f.Replace) {
		if err := f.DropReplace(r.Old.Path, r.Old.Version); err != nil {
			return nil, err
		}
	}
	f.Cleanup()
	return f.Format()
}

// Serve writes into the module source dir, in the download layout of the module proxy
// protocol, the root module of the repository at root and the three SDK modules, each at
// version with its served go.mod: the module source that a release builds from and
// that a site's builds reach, which serves only these until L14 clears (ADR-060
// decisions 21 and 40; ADR-044 decision 23).
func Serve(root, version, dir string) error {
	var errs []error
	for _, m := range sdkModules {
		sub := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(m, rootModule), "/")))
		errs = append(errs, serveModule(module.Version{Path: m, Version: version}, sub, dir))
	}
	return errors.Join(errs...)
}

// serveModule writes the .mod, .info and .zip of the module m at src into dir, the
// zip with the served go.mod in place of the repository's.
func serveModule(m module.Version, src, dir string) error {
	raw, err := os.ReadFile(filepath.Join(src, "go.mod"))
	if err != nil {
		return err
	}
	gomod, err := Served(raw, m.Version)
	if err != nil {
		return err
	}
	esc, err := module.EscapePath(m.Path)
	if err != nil {
		return err
	}
	at := filepath.Join(dir, esc, "@v")
	if err := os.MkdirAll(at, 0o755); err != nil {
		return err
	}
	var files []modzip.File
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		switch {
		case d.IsDir() && p != src && (fileExists(filepath.Join(p, "go.mod")) || strings.HasPrefix(d.Name(), ".") || d.Name() == "vendor"):
			return filepath.SkipDir
		case d.IsDir() || !d.Type().IsRegular() || strings.HasPrefix(d.Name(), "."):
			return nil
		case rel == "go.mod":
			files = append(files, memFile{"go.mod", gomod})
		default:
			files = append(files, diskFile{filepath.ToSlash(rel), p})
		}
		return nil
	})
	if err != nil {
		return err
	}
	var z bytes.Buffer
	if err := modzip.Create(&z, m, files); err != nil {
		return fmt.Errorf("the module zip of %s: %w", m.Path, err)
	}
	info, _ := json.Marshal(map[string]string{"Version": m.Version})
	return errors.Join(os.WriteFile(filepath.Join(at, m.Version+".mod"), gomod, 0o644),
		os.WriteFile(filepath.Join(at, m.Version+".info"), info, 0o644), os.WriteFile(filepath.Join(at, m.Version+".zip"), z.Bytes(), 0o644))
}

// memFile and diskFile are files of a module zip: bytes in memory, and a file on disk.
type (
	memFile struct {
		path string
		data []byte
	}
	diskFile struct{ path, file string }
)

func (f memFile) Path() string                 { return f.path }
func (f memFile) Lstat() (fs.FileInfo, error)  { return memInfo{f}, nil }
func (f memFile) Open() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(f.data)), nil }

func (f diskFile) Path() string                 { return f.path }
func (f diskFile) Lstat() (fs.FileInfo, error)  { return os.Lstat(f.file) }
func (f diskFile) Open() (io.ReadCloser, error) { return os.Open(f.file) }

type memInfo struct{ f memFile }

func (i memInfo) Name() string       { return filepath.Base(i.f.path) }
func (i memInfo) Size() int64        { return int64(len(i.f.data)) }
func (i memInfo) Mode() fs.FileMode  { return 0o644 }
func (i memInfo) ModTime() time.Time { return time.Time{} }
func (i memInfo) IsDir() bool        { return false }
func (i memInfo) Sys() any           { return nil }
