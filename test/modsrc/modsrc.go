// SPDX-License-Identifier: Apache-2.0

// Package modsrc writes the module sources of the tests that build with the builder: a
// directory in the download layout of the module proxy protocol, which the builder reads
// through file:// and which the S2a exit serves as its module proxy (ADR-044 decisions 7
// and 9; ADR-060 decisions 21 and 40). It is test code: no shipped distribution links it.
package modsrc

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/mod/module"
	"golang.org/x/mod/sumdb/dirhash"
	modzip "golang.org/x/mod/zip"

	"github.com/mutex-solutions/aiops-platform/internal/builder"
)

// Fill copies into the module source src, from the download cache of the Go module
// cache of the host, what a site that builds on the release fills its module cache with
// (ADR-044 decision 9): the .info and .mod of every module version of the module graph
// of the repository at repo, since go list -m all of a build reads them for whichever
// version that build selects; the zip of every module of the build list; and the
// toolchain module. It fetches the others into the module cache first, from the module
// proxy when the cache is cold, skips when it cannot, and fails, naming them, when it
// still lacks any.
func Fill(tb testing.TB, repo, src string) {
	tb.Helper()
	gomodcache, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		tb.Fatal(err)
	}
	download := filepath.Join(strings.TrimSpace(string(gomodcache)), "cache", "download")
	repoGo := func(args ...string) []string {
		cmd := exec.Command("go", args...)
		cmd.Dir, cmd.Env = repo, append(os.Environ(), "GOFLAGS=-mod=readonly")
		out, err := cmd.Output()
		if err != nil {
			tb.Fatal(err)
		}
		var mvs []string
		for _, f := range strings.Fields(string(out)) {
			if p, _, ok := strings.Cut(f, "@"); ok && strings.Contains(p, ".") && !strings.HasPrefix(p, "github.com/mutex-solutions/aiops-platform") {
				mvs = append(mvs, f)
			}
		}
		slices.Sort(mvs)
		return slices.Compact(mvs)
	}
	graph, list := repoGo("mod", "graph"), repoGo("list", "-m", "-f", "{{.Path}}@{{.Version}}", "all")
	// go mod download writes the three files of each module of the build list, and go
	// list -m the .info and .mod of each module version of the graph; both run outside
	// the module, so that the repository's go.sum is never written.
	for _, args := range [][]string{append([]string{"mod", "download"}, list...), append([]string{"list", "-m"}, graph...)} {
		cmd := exec.Command("go", args...)
		cmd.Dir = tb.TempDir()
		if out, err := cmd.CombinedOutput(); err != nil {
			tb.Skipf("go %s fails (%v): the case needs the modules of the build list in the module cache or a module proxy that serves them; the CI jobs that run it have one\n%s", args[0]+" "+args[1], err, out)
		}
	}
	type file struct{ mv, ext string }
	want := map[file]bool{}
	for _, ext := range []string{".info", ".mod", ".zip"} {
		want[file{builder.Toolchain + "@" + builder.ToolchainVersion, ext}] = true
	}
	for _, mv := range graph {
		want[file{mv, ".info"}], want[file{mv, ".mod"}] = true, true
	}
	for _, mv := range list {
		want[file{mv, ".zip"}] = true
	}
	var missing []string
	for f := range want {
		path, version, _ := strings.Cut(f.mv, "@")
		esc, err := module.EscapePath(path)
		from, to := filepath.Join(download, esc, "@v", version+f.ext), filepath.Join(src, esc, "@v", version+f.ext)
		if _, serr := os.Stat(from); serr != nil {
			missing = append(missing, f.mv+f.ext)
			continue
		}
		if err := errors.Join(err, os.MkdirAll(filepath.Dir(to), 0o755), Link(from, to)); err != nil {
			tb.Fatal(err)
		}
	}
	if slices.Sort(missing); len(missing) > 0 {
		tb.Fatalf("the module cache lacks %d files of the module source: %s", len(missing), strings.Join(missing, ", "))
	}
	List(tb, src)
}

// Merge links into the module source src each .info, .mod and .zip of the download
// layout from that src does not hold yet, and writes the list files again.
func Merge(tb testing.TB, from, src string) {
	tb.Helper()
	err := filepath.WalkDir(from, func(p string, e os.DirEntry, err error) error {
		rel, _ := filepath.Rel(from, p)
		to := filepath.Join(src, rel)
		switch ext := filepath.Ext(p); {
		case err != nil || e.IsDir() || filepath.Base(filepath.Dir(p)) != "@v" || ext != ".info" && ext != ".mod" && ext != ".zip":
			return err
		case fileExists(to):
			return nil
		}
		return errors.Join(os.MkdirAll(filepath.Dir(to), 0o755), Link(p, to))
	})
	if err != nil {
		tb.Fatal(err)
	}
	List(tb, src)
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// List writes the list file of each module of the module source src, the versions of
// which it holds a zip, as a module proxy answers a query: go mod tidy asks it for the
// module of a package that the tests of a dependency import.
func List(tb testing.TB, src string) {
	tb.Helper()
	err := filepath.WalkDir(src, func(p string, e os.DirEntry, err error) error {
		if err != nil || !e.IsDir() || e.Name() != "@v" {
			return err
		}
		zips, _ := filepath.Glob(filepath.Join(p, "*.zip"))
		var versions []string
		for _, z := range zips {
			versions = append(versions, strings.TrimSuffix(filepath.Base(z), ".zip"))
		}
		slices.Sort(versions)
		list := strings.Join(versions, "\n")
		if list != "" {
			list += "\n"
		}
		return os.WriteFile(filepath.Join(p, "list"), []byte(list), 0o644)
	})
	if err != nil {
		tb.Fatal(err)
	}
}

// Zip writes the module path at version from the directory dir into the module source
// src, its zip, .mod and .info, and returns the h1 sum of the zip, as go mod download
// records it.
func Zip(tb testing.TB, src, path, version, dir string) string {
	tb.Helper()
	esc, _ := module.EscapePath(path)
	at := filepath.Join(src, esc, "@v")
	if err := os.MkdirAll(at, 0o755); err != nil {
		tb.Fatal(err)
	}
	f, err := os.Create(filepath.Join(at, version+".zip"))
	if err != nil {
		tb.Fatal(err)
	}
	err = errors.Join(modzip.CreateFromDir(f, module.Version{Path: path, Version: version}, dir), f.Close())
	gomod, rerr := os.ReadFile(filepath.Join(dir, "go.mod"))
	info, _ := json.Marshal(map[string]string{"Version": version})
	if err = errors.Join(err, rerr, os.WriteFile(filepath.Join(at, version+".mod"), gomod, 0o644), os.WriteFile(filepath.Join(at, version+".info"), info, 0o644)); err != nil {
		tb.Fatal(err)
	}
	sum, err := dirhash.HashZip(filepath.Join(at, version+".zip"), dirhash.Hash1)
	if err != nil {
		tb.Fatal(err)
	}
	return sum
}

// LinkTree links every file under from into to, at the same path.
func LinkTree(tb testing.TB, from, to string) {
	tb.Helper()
	err := filepath.WalkDir(from, func(p string, e os.DirEntry, err error) error {
		rel, _ := filepath.Rel(from, p)
		switch {
		case err != nil:
			return err
		case e.IsDir():
			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		return Link(p, filepath.Join(to, rel))
	})
	if err != nil {
		tb.Fatal(err)
	}
}

// Link hard-links from at to, or copies it where a link cannot be made.
func Link(from, to string) error {
	if os.Link(from, to) == nil {
		return nil
	}
	r, err := os.Open(from)
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := os.Create(to)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	return errors.Join(err, w.Close())
}
