// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"bytes"
	"cmp"
	_ "embed" // stdnotices.txt
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"golang.org/x/mod/module"
)

// Notices writes into dir what every binary the builder writes carries beside it,
// and every image it writes holds (ADR-044 decisions 5 and 6; 08:1336 L7, :1337 L8):
//
//   - THIRD_PARTY_LICENSES/<module path>/<path from the module root>: each licence and
//     notice file of the scan, and the header notice of each file of the standard
//     library that the graph compiles, from the list of the pin (stdnotices.txt), as
//     <file>.NOTICE under the toolchain module;
//   - NOTICE: the notice files aggregated, each under the module and path it comes
//     from, then one line per MPL-2.0 module with its exact version and its source,
//     the module zip that the build checked (MPL-2.0 §3.2);
//   - <name>.spdx.json: an SBOM, SPDX 2.3 in JSON, of the binary name and each module
//     it links, the toolchain module among them, with its version, sum and detected
//     licence, created at created.
//
// It returns the MPL-2.0 modules, whose zips the distributor of the binary keeps.
func Notices(dir, name string, pkgs []Package, l Licences, created time.Time) ([]Module, error) {
	std, err := stdNoticeFiles(pkgs)
	if err != nil {
		return nil, err
	}
	files := slices.Concat(l.Files, std)
	var notice bytes.Buffer
	notice.WriteString("Notices of the third-party components that " + name + " links, as their files state them.\n")
	for _, f := range files {
		p := filepath.Join(dir, "THIRD_PARTY_LICENSES", filepath.FromSlash(f.Module), filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, f.Data, 0o644); err != nil {
			return nil, err
		}
		if f.Notice {
			fmt.Fprintf(&notice, "\n==== %s@%s %s ====\n\n%s", f.Module, f.Version, f.Path, bytes.TrimRight(f.Data, "\n"))
			notice.WriteString("\n")
		}
	}
	mods := modulesOf(pkgs)
	var mpl []Module
	for _, path := range sortedKeys(mods) {
		if m := mods[path]; slices.Contains(strings.Split(l.Modules[path], " "), "MPL-2.0") {
			mpl = append(mpl, m)
			fmt.Fprintf(&notice, "\n%s %s is under MPL-2.0, linked unmodified; its source is the module zip %s@%s (%s), which whoever distributes %s keeps beside it.\n",
				m.Path, m.Version, m.Path, m.Version, m.Sum, name)
		}
	}
	sbom, err := spdxOf(name, pkgs, l, created)
	if err == nil {
		err = errors.Join(os.WriteFile(filepath.Join(dir, "NOTICE"), notice.Bytes(), 0o644),
			os.WriteFile(filepath.Join(dir, name+".spdx.json"), sbom, 0o644))
	}
	return mpl, err
}

// spdxOf returns the SBOM of the binary name: one package for it, which it contains
// and the document describes, and one per module of its graph.
func spdxOf(name string, pkgs []Package, l Licences, created time.Time) ([]byte, error) {
	type pkg struct {
		SPDXID           string `json:"SPDXID"`
		Name             string `json:"name"`
		VersionInfo      string `json:"versionInfo,omitempty"`
		DownloadLocation string `json:"downloadLocation"`
		FilesAnalyzed    bool   `json:"filesAnalyzed"`
		LicenseConcluded string `json:"licenseConcluded"`
		LicenseDeclared  string `json:"licenseDeclared"`
		CopyrightText    string `json:"copyrightText"`
		ExternalRefs     []any  `json:"externalRefs,omitempty"`
		Comment          string `json:"comment,omitempty"`
	}
	type rel struct {
		Element string `json:"spdxElementId"`
		Type    string `json:"relationshipType"`
		Related string `json:"relatedSpdxElement"`
	}
	const none = "NOASSERTION"
	doc := map[string]any{"spdxVersion": "SPDX-2.3", "dataLicense": "CC0-1.0", "SPDXID": "SPDXRef-DOCUMENT", "name": name,
		"documentNamespace": "https://github.com/mutex-solutions/aiops-platform/spdx/" + name + "/" + created.UTC().Format("20060102T150405Z"),
		"creationInfo":      map[string]any{"created": created.UTC().Format(time.RFC3339), "creators": []string{"Tool: aiopsctl"}}}
	packages := []pkg{{SPDXID: "SPDXRef-Package-0", Name: name, DownloadLocation: none, LicenseConcluded: none, LicenseDeclared: none, CopyrightText: none}}
	rels := []rel{{"SPDXRef-DOCUMENT", "DESCRIBES", "SPDXRef-Package-0"}}
	mods := modulesOf(pkgs)
	for _, p := range pkgs {
		if p.Standard {
			mods[Toolchain] = p.Module
		}
	}
	for i, path := range sortedKeys(mods) {
		m := mods[path]
		p := pkg{SPDXID: fmt.Sprintf("SPDXRef-Package-%d", i+1), Name: path, VersionInfo: m.Version, DownloadLocation: none,
			LicenseConcluded: cmp.Or(l.Modules[path], none), LicenseDeclared: none, CopyrightText: none}
		if m.Version != "" {
			if esc, err := module.EscapePath(path); err == nil {
				p.DownloadLocation = "https://proxy.golang.org/" + esc + "/@v/" + m.Version + ".zip"
			}
			p.ExternalRefs = []any{map[string]string{"referenceCategory": "PACKAGE-MANAGER", "referenceType": "purl",
				"referenceLocator": "pkg:golang/" + path + "@" + m.Version}}
		}
		if m.Sum != "" {
			p.Comment = "sum " + m.Sum
		}
		packages = append(packages, p)
		rels = append(rels, rel{"SPDXRef-Package-0", "CONTAINS", p.SPDXID})
	}
	doc["packages"], doc["relationships"] = packages, rels
	return json.MarshalIndent(doc, "", "  ")
}

// stdNotices is the list of the files of the standard library at the pinned toolchain
// whose comments carry a notice other than the Go Authors' (ADR-044 decision 27):
// StdNotices of the tree, which the release pipeline writes at each toolchain pin and
// a test holds to the tree.
//
//go:embed stdnotices.txt
var stdNotices string

// copyrightLine is a copyright statement, "Copyright" and a year, with (c) or © between
// them or not, and goAuthors the one of the Go Authors.
var (
	copyrightLine = regexp.MustCompile(`(?i)\bcopyright\s*(?:\(c\)|©)?\s*[0-9]{4}`)
	goAuthors     = regexp.MustCompile(`The Go Authors`)
)

// StdNotices returns, sorted, the path from goroot of each .go and .s file under
// src/, outside cmd/, testdata and the directories the go command ignores, not a test,
// whose comments hold a copyright statement that does not name the Go Authors.
func StdNotices(goroot string) ([]string, error) {
	var out []string
	src := filepath.Join(goroot, "src")
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && p != src && (d.Name() == "testdata" || strings.HasPrefix(d.Name(), "_") || strings.HasPrefix(d.Name(), ".") || p == filepath.Join(src, "cmd")):
			return filepath.SkipDir
		case d.IsDir() || strings.HasSuffix(p, "_test.go") || !strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, ".s"):
			return nil
		}
		b, err := os.ReadFile(p)
		if err == nil && noticeOf(b) != "" {
			rel, _ := filepath.Rel(goroot, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return err
	})
	slices.Sort(out)
	return out, err
}

// noticeOf returns the comment block of src that holds its first copyright line of
// someone other than the Go Authors, "" for none: the run of // lines around it.
func noticeOf(src []byte) string {
	ls := strings.Split(string(src), "\n")
	for i, l := range ls {
		t := strings.TrimSpace(l)
		if !strings.HasPrefix(t, "//") || !copyrightLine.MatchString(t) || goAuthors.MatchString(t) {
			continue
		}
		start, end := i, i
		for start > 0 && strings.HasPrefix(strings.TrimSpace(ls[start-1]), "//") {
			start--
		}
		for end+1 < len(ls) && strings.HasPrefix(strings.TrimSpace(ls[end+1]), "//") {
			end++
		}
		return strings.Join(ls[start:end+1], "\n") + "\n"
	}
	return ""
}

// stdNoticeFiles returns the notices of the files of the list that the standard
// library packages of pkgs compile, each as a notice file of the toolchain module named
// after the file it comes from.
func stdNoticeFiles(pkgs []Package) ([]File, error) {
	listed := map[string]bool{}
	for _, l := range strings.Split(stdNotices, "\n") {
		if l != "" && !strings.HasPrefix(l, "#") {
			listed[l] = true
		}
	}
	var out []File
	for _, p := range pkgs {
		if !p.Standard {
			continue
		}
		for _, f := range slices.Concat(p.GoFiles, p.SFiles) {
			rel, err := filepath.Rel(p.Module.Dir, filepath.Join(p.Dir, f))
			rel = filepath.ToSlash(rel)
			if err != nil || !listed[rel] {
				continue
			}
			b, err := os.ReadFile(filepath.Join(p.Dir, f))
			if err != nil {
				return nil, err
			}
			n := noticeOf(b)
			if n == "" {
				return nil, fmt.Errorf("%s of the toolchain carries no notice, and the list of the pin names it", rel)
			}
			out = append(out, File{Module: Toolchain, Version: p.Module.Version, Path: rel + ".NOTICE", Data: []byte(n), Notice: true})
		}
	}
	return out, nil
}
