// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// corpusFile returns the file name of the embedded corpus.
func corpusFile(t *testing.T, name string) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(corpus))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			t.Fatalf("the corpus holds no %s", name)
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Name == name {
			b, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			return b
		}
	}
}

// toolchainTree returns the tree of the pinned toolchain module as the Go module cache
// holds it, read only, or skips: go mod download golang.org/toolchain@<pin> puts it
// there, and the CI job builder does.
func toolchainTree(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	dir := filepath.Join(strings.TrimSpace(string(out)), Toolchain+"@"+ToolchainVersion)
	if _, serr := os.Stat(filepath.Join(dir, "LICENSE")); err != nil || serr != nil {
		t.Skipf("the module cache holds no %s@%s: go mod download it to run this case (the CI job builder does)", Toolchain, ToolchainVersion)
	}
	return dir
}

// A licence file is named license, licence, copying or unlicense in one of the parts its
// name splits into at ".", "-" and "_", without case; notice, patents or copyright makes
// a notice file; a source file is neither (ADR-044 decision 3).
func TestLicenceFiles(t *testing.T) {
	for name, want := range map[string]int{
		"LICENSE": licenceFile, "LICENSE.md": licenceFile, "COPYING.LESSER": licenceFile, "APACHE-LICENSE-2.0": licenceFile,
		"licence.txt": licenceFile, "UNLICENSE": licenceFile, "GO-LICENSE": licenceFile, "LICENSE.docs": licenceFile,
		"NOTICE": noticeFile, "NOTICE.txt": noticeFile, "PATENTS": noticeFile, "COPYRIGHT": noticeFile,
		"notice.go": notLicence, "license.go": notLicence, "LICENSE.s": notLicence, "boring.syso": notLicence,
		"README.md": notLicence, "licensed.txt": notLicence,
	} {
		if got := kindOf(name); got != want {
			t.Errorf("kindOf(%q) = %d, want %d", name, got, want)
		}
	}
}

// The classifier names the SPDX ids of the corpus built from the pinned SPDX data
// (ADR-044 decision 26): a text, its required form, two texts in one file, a twin text
// read as one id, an exception with its licence; and a text it cannot name, or one
// with a clause outside what it names, however short, is unknown, which fails the build
// (L16); a title of the text it names and "All rights reserved" are not clauses.
func TestClassify(t *testing.T) {
	apache := corpusFile(t, "License/Apache-2.0/text.txt")
	mit := corpusFile(t, "License/MIT/text.txt")
	bsd2 := corpusFile(t, "License/BSD-2-Clause/text.txt")
	bsd := corpusFile(t, "License/BSD-3-Clause/text.txt")
	gpl := corpusFile(t, "License/GPL-2.0-only/text.txt")
	classpath := corpusFile(t, "Exception/Classpath-exception-2.0/text.txt")
	terms, _, _ := bytes.Cut(apache, []byte("END OF TERMS AND CONDITIONS"))
	clause := "\nWithout limiting other conditions of the License, the grant of rights under the License does not include the right to " +
		"sell the Software or to offer it to third parties as a hosted service for a fee, and this condition binds every copy and every " +
		"derivative work that anyone makes of the Software.\n"
	for _, c := range []struct {
		name       string
		text       []byte
		want, fail string
	}{
		{"Apache-2.0", apache, "Apache-2.0", ""},
		{"the Apache-2.0 terms without the appendix, as APACHE-LICENSE-2.0 of go-diff@v1.1.0", append(terms, "END OF TERMS AND CONDITIONS\n"...), "Apache-2.0", ""},
		{"MIT under a copyright line", append([]byte("Copyright (c) 2026 Example Authors\n\n"), corpusFile(t, "License/MIT/text.txt")...), "MIT", ""},
		{"Apache-2.0 then BSD-3-Clause in one file", append(append(bytes.Clone(apache), '\n'), bsd...), "Apache-2.0 AND BSD-3-Clause", ""},
		{"MPL-2.0, whose text the list publishes twice", corpusFile(t, "License/MPL-2.0/text.txt"), "MPL-2.0", ""},
		{"GPL-2.0, one text for -only and -or-later", gpl, "GPL-2.0-only", ""},
		{"GPL-2.0 with the Classpath exception", append(append(bytes.Clone(gpl), '\n'), classpath...), "GPL-2.0-only WITH Classpath-exception-2.0", ""},
		{"Apache-2.0 with a clause of its own", append(bytes.Clone(apache), clause...), "", "words outside the texts it names"},
		{"MIT under its title", append([]byte("The MIT License (MIT)\n\nCopyright (c) 2026 Example Authors\n\n"), mit...), "MIT", ""},
		{"BSD-2-Clause under its title, all rights reserved", append([]byte("BSD 2-Clause License\n\nCopyright (c) 2026 Example Authors\nAll rights reserved.\n\n"), bsd2...), "BSD-2-Clause", ""},
		{"MIT with a clause of ten words", append(bytes.Clone(mit), "\nYou may not use the Software for any commercial purpose whatsoever.\n"...), "", "words outside the texts it names"},
		{"Apache-2.0 with a clause of nine words", append(bytes.Clone(apache), "\nThe Software shall not be used by any military organisation.\n"...), "", "words outside the texts it names"},
		{"MIT with a clause of nine words", append(bytes.Clone(mit), "\nNo permission is granted to sell or host the Software.\n"...), "", "words outside the texts it names"},
		{"MIT with a clause of two words", append(bytes.Clone(mit), "\nNon-commercial use.\n"...), "", "words outside the texts it names"},
		{"a text of its own", []byte(clause), "", "names no licence"},
		{"an exception alone", classpath, "", "no licence"},
	} {
		got, err := Classify(c.text)
		if got != c.want || (c.fail == "") != (err == nil) || err != nil && !strings.Contains(err.Error(), c.fail) {
			t.Errorf("%s: %q, %v; want %q, %q", c.name, got, err, c.want, c.fail)
		}
	}
}

// moduleDir writes the files of a module into a directory and returns it.
func moduleDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func pkg(m Module, rel string) Package {
	return Package{ImportPath: m.Path + "/" + rel, Dir: filepath.Join(m.Dir, filepath.FromSlash(rel)), Module: m}
}

// Each package is governed by the licence files of the nearest directory, from its own
// up to its module root, that holds any, and every notice file on that walk is copied
// (ADR-044 decision 3; 08:1336 L7); the licence of a module joins those of its packages.
// A package that no licence file governs, and a text the classifier cannot name, fail
// the scan; a licence outside L7 fails the check unless dist.yaml accepts it (L16).
func TestScanLicences(t *testing.T) {
	mit, bsd, gpl := string(corpusFile(t, "License/MIT/text.txt")), string(corpusFile(t, "License/BSD-3-Clause/text.txt")), string(corpusFile(t, "License/GPL-2.0-only/text.txt"))
	m := Module{Path: "example.test/m", Version: "v1.0.0", Dir: moduleDir(t, map[string]string{
		"LICENSE": mit, "NOTICE": "Example m includes software of Example Corp.\n", "a/a.go": "package a\n",
		"third/x/LICENSE": bsd, "third/x/y/y.go": "package y\n", "third/x/PATENTS": "A grant of patents.\n"})}
	g := Module{Path: "example.test/gpl", Version: "v1.2.0", Dir: moduleDir(t, map[string]string{"COPYING": gpl, "g.go": "package g\n"})}
	l, err := ScanLicences([]Package{pkg(m, "a"), pkg(m, "third/x/y"), {ImportPath: g.Path, Dir: g.Dir, Module: g}})
	if err != nil {
		t.Fatal(err)
	}
	if l.Modules[m.Path] != "BSD-3-Clause AND MIT" || l.Modules[g.Path] != "GPL-2.0-only" {
		t.Errorf("licences %v", l.Modules)
	}
	var files []string
	for _, f := range l.Files {
		files = append(files, f.Module+" "+f.Path+" "+f.Licence)
	}
	want := "example.test/gpl COPYING GPL-2.0-only|example.test/m LICENSE MIT|example.test/m NOTICE |example.test/m third/x/LICENSE BSD-3-Clause|example.test/m third/x/PATENTS "
	if got := strings.Join(files, "|"); got != want {
		t.Errorf("files\n%s\nwant\n%s", got, want)
	}
	if err := CheckLicences(l, nil); err == nil || !strings.Contains(err.Error(), "example.test/gpl is under GPL-2.0-only") {
		t.Errorf("the check without acceptance: %v", err)
	}
	if err := CheckLicences(l, []string{"GPL-2.0-only"}); err != nil {
		t.Errorf("the check with GPL-2.0-only accepted: %v", err)
	}

	bare := Module{Path: "example.test/bare", Version: "v0.1.0", Dir: moduleDir(t, map[string]string{"b.go": "package b\n", "README.md": "no licence\n"})}
	odd := Module{Path: "example.test/odd", Version: "v0.1.0", Dir: moduleDir(t, map[string]string{"LICENSE": "Use this on Tuesdays only.\n", "o.go": "package o\n"})}
	_, err = ScanLicences([]Package{{ImportPath: bare.Path, Dir: bare.Dir, Module: bare}, {ImportPath: odd.Path, Dir: odd.Dir, Module: odd}})
	if err == nil || !strings.Contains(err.Error(), "example.test/bare is governed by no licence file") || !strings.Contains(err.Error(), "example.test/odd@v0.1.0 LICENSE: unknown") {
		t.Errorf("a module with no licence and one with a text of its own: %v", err)
	}
}

// A recorded reading applies to its file at its module, version and hash, so a changed
// file is classified afresh: LICENSE.docs of go-digest@v1.0.0 governs no package
// (ADR-020 decision 30, reading 4), and another text under its name is classified.
func TestReadingIsKeyedToTheFile(t *testing.T) {
	apache, cc := string(corpusFile(t, "License/Apache-2.0/text.txt")), string(corpusFile(t, "License/CC-BY-SA-4.0/text.txt"))
	m := Module{Path: "github.com/opencontainers/go-digest", Version: "v1.0.0", Dir: moduleDir(t, map[string]string{"LICENSE": apache, "LICENSE.docs": cc, "d.go": "package d\n"})}
	l, err := ScanLicences([]Package{{ImportPath: m.Path, Dir: m.Dir, Module: m}})
	if err != nil || l.Modules[m.Path] != "Apache-2.0 AND CC-BY-SA-4.0" || len(l.Applied) != 0 {
		t.Errorf("a LICENSE.docs that is not the recorded file: %v, %v, applied %v", err, l.Modules, l.Applied)
	}
}

// The scan reads the standard library from the tree of the pinned toolchain: a binary
// that links crypto/sha256 passes, crypto/internal/boring read as the Go licence by
// the recorded reading of its LICENSE, and a graph that holds
// crypto/internal/boring/syso fails on the BoringSSL terms the whole file then carries
// (ADR-044 decisions 3 and 14).
func TestBoringReading(t *testing.T) {
	tree := toolchainTree(t)
	std := Module{Path: Toolchain, Version: ToolchainVersion, Dir: tree}
	p := func(path string) Package {
		return Package{ImportPath: path, Dir: filepath.Join(tree, "src", path), Standard: true, Module: std}
	}
	pkgs := []Package{p("crypto/sha256"), p("crypto/internal/boring"), p("crypto/internal/boring/sig")}
	l, err := ScanLicences(pkgs)
	if err != nil || l.Modules[Toolchain] != "BSD-3-Clause" || len(l.Applied) != 1 || CheckLicences(l, nil) != nil {
		t.Errorf("crypto/sha256: %v, %v, applied %v", err, l.Modules, l.Applied)
	}
	l, err = ScanLicences(append(pkgs, p("crypto/internal/boring/syso")))
	if err == nil && CheckLicences(l, nil) == nil {
		t.Errorf("a graph with crypto/internal/boring/syso passes: %v", l.Modules)
	}
}
