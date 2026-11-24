// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"archive/tar"
	"bytes"
	"cmp"
	"compress/gzip"
	"crypto/sha256"
	_ "embed" // spdx-corpus.tar.gz
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode"

	classifier "github.com/google/licenseclassifier/v2"

	"github.com/mutex-solutions/aiops-platform/boot"
)

// The SPDX License List data of the corpus (ADR-044 decision 26): the module, its
// version and the h1 sum of its module zip, from which tools/spdxcorpus writes
// spdx-corpus.tar.gz.
const (
	SPDXModule  = "github.com/spdx/license-list-data"
	SPDXVersion = "v3.29.0+incompatible"
	SPDXSum     = "h1:zLVVtcoiXca+aMQmv9wyCUL3xfOXLv1LLjJow34CQnA="
)

//go:embed spdx-corpus.tar.gz
var corpus []byte

// Kinds of the files the scan reads (ADR-044 decision 3).
const (
	notLicence = iota
	licenceFile
	noticeFile
)

// kindOf returns the kind of a regular file by its name: a licence file has license,
// licence, copying or unlicense as one of the parts its name splits into at ".", "-"
// and "_", compared without case; a notice file has notice, patents or copyright; a
// file that ends in .go, .s, .c, .h or .syso is neither.
func kindOf(name string) int {
	lower := strings.ToLower(name)
	for _, ext := range []string{".go", ".s", ".c", ".h", ".syso"} {
		if strings.HasSuffix(lower, ext) {
			return notLicence
		}
	}
	kind := notLicence
	for _, part := range strings.FieldsFunc(lower, func(r rune) bool { return r == '.' || r == '-' || r == '_' }) {
		switch part {
		case "license", "licence", "copying", "unlicense":
			return licenceFile
		case "notice", "patents", "copyright":
			kind = noticeFile
		}
	}
	return kind
}

// threshold is the confidence below which the classifier names no licence, and
// leftover the most words a licence file may hold outside the texts it names and
// outside the lines of those texts that a match leaves out, such as a title: room for
// a line that says which files a text covers, and none for a clause.
const (
	threshold = 0.9
	leftover  = 12
)

// twins are the ids of the SPDX list whose text is the text of another id, so that a
// licence file alone cannot tell them apart, with the id the scan reads them as and
// why. A tie between two ids that are not here is unknown.
var twins = map[string]string{
	// The list publishes the text of MPL-2.0 twice; the two differ by the notice of
	// Exhibit B in a source file, which no licence file carries.
	"MPL-2.0-no-copyleft-exception": "MPL-2.0",
	// The FSF licences have one text for -only and -or-later, which differ by the
	// notice in a source file; the scan reads the text as -only, the fewer rights.
	"GPL-1.0-or-later": "GPL-1.0-only", "GPL-2.0-or-later": "GPL-2.0-only", "GPL-3.0-or-later": "GPL-3.0-only",
	"LGPL-2.0-or-later": "LGPL-2.0-only", "LGPL-2.1-or-later": "LGPL-2.1-only", "LGPL-3.0-or-later": "LGPL-3.0-only",
	"AGPL-1.0-or-later": "AGPL-1.0-only", "AGPL-3.0-or-later": "AGPL-3.0-only",
}

// reading is one recorded reading of a licence file: the file of a module at a
// version, keyed by the SHA-256 of its bytes so that a changed file is classified
// afresh, the licence it is read as, "" for a file that governs no package, and the
// package whose presence in the graph voids it.
type reading struct {
	module, version, file, sha256 string
	licence, unless, why          string
}

// readings are the recorded readings (ADR-044 decision 3; ADR-020 decision 30, reading
// 4). Toolchain is the module of the standard library.
var readings = []reading{
	{Toolchain, ToolchainVersion, "src/crypto/internal/boring/LICENSE", "56210f826b8f0fbac3160dfe55c97f4019eb6cdda3963b5a0eab8a8bdb62360e",
		"BSD-3-Clause", "crypto/internal/boring/syso",
		"its first lines put the Go source of the directory under the Go licence; the BoringSSL terms it reproduces govern the objects of crypto/internal/boring/syso, built only with GOEXPERIMENT=boringcrypto (ADR-044 decision 3)"},
	{"github.com/opencontainers/go-digest", "v1.0.0", "LICENSE.docs", "a1d1adec3ba691ef6e377a78bbe9e9e2501f2f4a1723d08857da1ac6564e4fed",
		"", "", "CC-BY-SA-4.0 for README.md and CONTRIBUTING.md only, which no binary links (ADR-020 decision 30, reading 4; ADR-047 decision 21)"},
	{"go.yaml.in/yaml/v3", "v3.0.6", "LICENSE", "d18f6323b71b0b768bb5e9616e36da390fbd39369a81807cca352de4e4e6aa0b",
		"Apache-2.0 AND MIT", "", "its prose puts the files ported from libyaml under the MIT text it holds and the rest under the Apache-2.0 notice it holds (licence record §2.1, read 2026-11-21)"},
}

// corpusTexts holds the texts of the corpus by id, and ownLines the lines of each, as
// the classifier normalizes them, once read; apacheText is the text of Apache-2.0.
var (
	corpusTexts = map[string][][]byte{}
	ownLines    sync.Map // id -> map[string]bool
	apacheText  []byte
)

// apache returns the text of Apache-2.0 from the corpus: the licence of the code a
// build generates, which the scan then reads as it reads any module.
func apache() []byte {
	_, _ = corpusClassifier()
	return apacheText
}

// lines returns the normalized lines of the texts of id.
func lines(c *classifier.Classifier, id string) map[string]bool {
	if v, ok := ownLines.Load(id); ok {
		return v.(map[string]bool)
	}
	set := map[string]bool{}
	for _, t := range corpusTexts[id] {
		for _, l := range strings.Split(string(c.Normalize(t)), "\n") {
			set[l] = true
		}
	}
	ownLines.Store(id, set)
	return set
}

// corpusClassifier is the classifier over the embedded corpus, built once.
var corpusClassifier = sync.OnceValues(func() (*classifier.Classifier, error) {
	c := classifier.NewClassifier(threshold)
	zr, err := gzip.NewReader(bytes.NewReader(corpus))
	if err != nil {
		return nil, fmt.Errorf("the licence corpus: %w", err)
	}
	tr := tar.NewReader(zr)
	n := 0
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("the licence corpus: %w", err)
		}
		b, err := io.ReadAll(tr)
		parts := strings.Split(h.Name, "/")
		if err != nil || len(parts) != 3 {
			return nil, fmt.Errorf("the licence corpus: %s: %v", h.Name, err)
		}
		c.AddContent(parts[0], parts[1], strings.TrimSuffix(parts[2], ".txt"), b)
		corpusTexts[parts[1]] = append(corpusTexts[parts[1]], b)
		if h.Name == "License/Apache-2.0/text.txt" {
			apacheText = b
		}
		n++
	}
	if n == 0 {
		return nil, errors.New("the licence corpus is empty")
	}
	return c, nil
})

// Classify names the licence of the text of one licence file, an SPDX expression: the
// licences, headers and exceptions of the corpus it holds, each id once, joined with
// AND in the order they appear, an exception joined with WITH to the one licence of
// the file. It fails, naming the file as unknown (L16), when the text names nothing,
// holds more than leftover words outside what it names, matches two texts equally
// that twins does not join, or holds an exception beside no licence or two.
func Classify(text []byte) (string, error) {
	c, err := corpusClassifier()
	if err != nil {
		return "", err
	}
	norm := c.Normalize(text)
	var ms []*classifier.Match
	covered := map[int]bool{}
	for _, m := range c.Match(norm).Matches {
		for l := m.StartLine; l <= m.EndLine; l++ {
			covered[l] = true
		}
		if m.MatchType != "Copyright" {
			ms = append(ms, m)
		}
	}
	extra := 0
	for i, l := range strings.Split(string(norm), "\n") {
		own := slices.ContainsFunc(ms, func(m *classifier.Match) bool { return lines(c, m.Name)[l] })
		if !covered[i+1] && !own {
			extra += len(strings.FieldsFunc(l, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }))
		}
	}
	if len(ms) == 0 {
		return "", errors.New("unknown: the text names no licence of the corpus")
	}
	if extra > leftover {
		return "", fmt.Errorf("unknown: %d words outside the texts it names", extra)
	}
	// A match inside a longer one, such as the short form of an exception inside its
	// text, is the longer one's.
	ms = slices.DeleteFunc(ms, func(m *classifier.Match) bool {
		return slices.ContainsFunc(ms, func(o *classifier.Match) bool {
			return o.StartTokenIndex <= m.StartTokenIndex && m.EndTokenIndex <= o.EndTokenIndex && o.EndTokenIndex-o.StartTokenIndex > m.EndTokenIndex-m.StartTokenIndex
		})
	})
	slices.SortStableFunc(ms, func(a, b *classifier.Match) int { return a.StartTokenIndex - b.StartTokenIndex })
	var ids, exceptions []string
	prev := ""
	for i, m := range ms {
		name := cmp.Or(twins[m.Name], m.Name)
		if i > 0 && m.StartTokenIndex == ms[i-1].StartTokenIndex && m.EndTokenIndex == ms[i-1].EndTokenIndex && m.Confidence == ms[i-1].Confidence && name != prev {
			return "", fmt.Errorf("unknown: the text is %s and %s alike", ms[i-1].Name, m.Name)
		}
		prev = name
		switch {
		case m.MatchType == "Exception":
			exceptions = append(exceptions, name)
		case !slices.Contains(ids, name):
			ids = append(ids, name)
		}
	}
	switch {
	case len(ids) == 0:
		return "", errors.New("unknown: the text names an exception and no licence")
	case len(exceptions) == 0:
		return strings.Join(ids, " AND "), nil
	case len(ids) == 1 && len(exceptions) == 1:
		return ids[0] + " WITH " + exceptions[0], nil
	}
	return "", fmt.Errorf("unknown: the exceptions %s beside the licences %s", strings.Join(exceptions, ", "), strings.Join(ids, ", "))
}

// File is one licence or notice file of a module that a binary links: the module and
// its version, the file's path from the module root, its bytes, and for a licence file
// the licence it is read as, "" for one that governs no package.
type File struct {
	Module, Version, Path string
	Data                  []byte
	Notice                bool
	Licence               string
	unknown               bool
}

// Licences is the licence scan of a package graph (ADR-044 decision 3; 08:1335 L7,
// :1344 L16): the detected licence of each module, every licence and notice file that
// governs a package of the graph, and the readings it applied.
type Licences struct {
	Modules map[string]string // module path to its detected licence
	Files   []File
	Applied []string
}

// ScanLicences runs the scan on the packages pkgs of a graph, each with its module: for
// each package, the licence files of the nearest directory, from its own up to its
// module root, that holds any, each classified, and the notice files of every directory
// on that walk. The licence of a module joins those of its packages with AND. A text
// that does not classify fails the scan with every such file named.
func ScanLicences(pkgs []Package) (Licences, error) {
	l := Licences{Modules: map[string]string{}}
	graph := map[string]bool{}
	for _, p := range pkgs {
		graph[p.ImportPath] = true
	}
	seen := map[string]*File{}
	perModule := map[string][]string{}
	var errs []error
	read := func(m Module, rel string, notice bool) *File {
		key := m.Path + "\x00" + rel
		if f, ok := seen[key]; ok {
			return f
		}
		b, err := os.ReadFile(filepath.Join(m.Dir, filepath.FromSlash(rel)))
		if err != nil {
			errs = append(errs, err)
		}
		f := &File{Module: m.Path, Version: m.Version, Path: rel, Data: b, Notice: notice, unknown: err != nil}
		if !notice && err == nil {
			f.Licence, err = l.classify(m, rel, b, graph)
			if err != nil {
				f.unknown = true
				errs = append(errs, fmt.Errorf("%s@%s %s: %w", m.Path, m.Version, rel, err))
			}
		}
		seen[key] = f
		l.Files = append(l.Files, *f)
		return f
	}
	for _, p := range pkgs {
		m := p.Module
		rel, err := filepath.Rel(m.Dir, p.Dir)
		if err != nil || strings.HasPrefix(rel, "..") {
			errs = append(errs, fmt.Errorf("package %s lies outside the root of its module %s", p.ImportPath, m.Path))
			continue
		}
		var governing []string
		found := false
		for dir := filepath.ToSlash(rel); ; dir = parent(dir) {
			entries, err := os.ReadDir(filepath.Join(m.Dir, filepath.FromSlash(dir)))
			if err != nil {
				errs = append(errs, err)
				break
			}
			var here []string
			held := false
			for _, e := range entries {
				k := kindOf(e.Name())
				if !e.Type().IsRegular() || k == notLicence {
					continue
				}
				f := read(m, joinRel(dir, e.Name()), k == noticeFile)
				if k == licenceFile && (f.Licence != "" || f.unknown) {
					held = true
					here = append(here, f.Licence)
				}
			}
			if held && !found {
				found, governing = true, here
			}
			if dir == "." {
				break
			}
		}
		if !found {
			errs = append(errs, fmt.Errorf("%s@%s: package %s is governed by no licence file (L7: no licence)", m.Path, m.Version, p.ImportPath))
			continue
		}
		perModule[m.Path] = append(perModule[m.Path], governing...)
	}
	for mod, exprs := range perModule {
		l.Modules[mod] = join(exprs)
	}
	slices.SortFunc(l.Files, func(a, b File) int { return strings.Compare(a.Module+"\x00"+a.Path, b.Module+"\x00"+b.Path) })
	return l, errors.Join(errs...)
}

// classify returns the licence of the licence file rel of m: a recorded reading of
// its bytes where one applies, else Classify.
func (l *Licences) classify(m Module, rel string, b []byte, graph map[string]bool) (string, error) {
	sum := sha256.Sum256(b)
	for _, r := range readings {
		if r.module == m.Path && r.version == m.Version && r.file == rel && (r.sha256 == "" || r.sha256 == hex.EncodeToString(sum[:])) && !graph[r.unless] {
			l.Applied = append(l.Applied, fmt.Sprintf("%s@%s %s: read as %q: %s", m.Path, m.Version, rel, r.licence, r.why))
			return r.licence, nil
		}
	}
	return Classify(b)
}

// join joins licence expressions with AND, each id once, sorted.
func join(exprs []string) string {
	var parts []string
	for _, e := range exprs {
		for _, p := range strings.Split(e, " AND ") {
			if strings.Contains(p, " OR ") {
				p = "(" + p + ")"
			}
			if !slices.Contains(parts, p) {
				parts = append(parts, p)
			}
		}
	}
	slices.Sort(parts)
	return strings.Join(parts, " AND ")
}

// CheckLicences is the licence check of L16 on the scanned graph: every module's
// licence on the L7 allow-list or accepted, by boot's reading of the list
// (boot.Licensed), the one gate text.
func CheckLicences(l Licences, accept []string) error {
	var errs []error
	for _, mod := range sortedKeys(l.Modules) {
		if !boot.Licensed(l.Modules[mod], accept) {
			errs = append(errs, fmt.Errorf("module %s is under %s, outside the L7 allow-list and not in acceptLicenses (L16)", mod, l.Modules[mod]))
		}
	}
	return errors.Join(errs...)
}

func parent(dir string) string {
	if i := strings.LastIndex(dir, "/"); i >= 0 {
		return dir[:i]
	}
	return "."
}

func joinRel(dir, name string) string {
	if dir == "." {
		return name
	}
	return dir + "/" + name
}
