// SPDX-License-Identifier: Apache-2.0

// Command spdxcorpus writes the corpus of the licence classifier of the builder,
// internal/builder/spdx-corpus.tar.gz, from the SPDX License List data at the version
// that internal/builder pins (ADR-044 decision 26): the module
// github.com/spdx/license-list-data at that version, read from the Go module cache
// and checked against its recorded h1 sum. Without -write it fails when the committed
// archive differs from what it writes, so a change of the data or of this program is
// seen.
//
//	go mod download github.com/spdx/license-list-data@v3.29.0+incompatible
//	go run ./tools/spdxcorpus [-write] .
//
// The archive holds, for each license and exception of the list that is not
// deprecated, its text (<category>/<id>/text.txt) and, where it differs in its words,
// the text its template requires (required.txt): the optional parts left out, a
// variable that may be empty left out, and every other variable at its original
// text. It holds the standard header of each license that has one
// (Header/<id>/header.txt). The classifier names each text by its SPDX id, so no table
// maps names (ADR-044 decision 26). The tar is written sorted, with no time, owner or
// mode beyond 0644, and gzip with no name and no time, so the same data gives the same
// bytes.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/mod/sumdb/dirhash"

	"github.com/mutex-solutions/aiops-platform/internal/builder"
)

const out = "internal/builder/spdx-corpus.tar.gz"

func main() {
	write := flag.Bool("write", false, "write the archive")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: spdxcorpus [-write] <repository root>")
		os.Exit(2)
	}
	if err := run(flag.Arg(0), *write); err != nil {
		fmt.Fprintln(os.Stderr, "spdxcorpus:", err)
		os.Exit(1)
	}
}

func run(root string, write bool) error {
	dir, err := moduleDir()
	if err != nil {
		return err
	}
	want, err := Corpus(dir)
	if err != nil {
		return err
	}
	file := filepath.Join(root, out)
	if write {
		return os.WriteFile(file, want, 0o644)
	}
	if got, err := os.ReadFile(file); err != nil || !bytes.Equal(got, want) {
		return fmt.Errorf("%s differs from what spdxcorpus writes from %s@%s; run go run ./tools/spdxcorpus -write . (%v)", out, builder.SPDXModule, builder.SPDXVersion, err)
	}
	return nil
}

// moduleDir returns the directory of the SPDX module in the module cache, checked
// against its recorded sum.
func moduleDir() (string, error) {
	cmd := exec.Command("go", "env", "GOMODCACHE")
	b, err := cmd.Output()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(strings.TrimSpace(string(b)), builder.SPDXModule+"@"+builder.SPDXVersion)
	if _, err := os.Stat(dir); err != nil {
		return "", fmt.Errorf("the module cache holds no %s@%s: go mod download it first", builder.SPDXModule, builder.SPDXVersion)
	}
	sum, err := dirhash.HashDir(dir, builder.SPDXModule+"@"+builder.SPDXVersion, dirhash.Hash1)
	if err != nil {
		return "", err
	}
	if sum != builder.SPDXSum {
		return "", fmt.Errorf("%s@%s in the module cache has the sum %s, not the recorded %s", builder.SPDXModule, builder.SPDXVersion, sum, builder.SPDXSum)
	}
	return dir, nil
}

// Corpus returns the archive written from the module directory dir.
func Corpus(dir string) ([]byte, error) {
	files := map[string]string{}
	var list struct {
		Version  string `json:"licenseListVersion"`
		Licenses []struct {
			ID         string `json:"licenseId"`
			Deprecated bool   `json:"isDeprecatedLicenseId"`
		} `json:"licenses"`
		Exceptions []struct {
			ID         string `json:"licenseExceptionId"`
			Deprecated bool   `json:"isDeprecatedLicenseId"`
		} `json:"exceptions"`
	}
	for _, f := range []string{"licenses.json", "exceptions.json"} {
		b, err := os.ReadFile(filepath.Join(dir, "json", f))
		if err == nil {
			err = json.Unmarshal(b, &list)
		}
		if err != nil {
			return nil, err
		}
	}
	if "v"+list.Version+"+incompatible" != builder.SPDXVersion {
		return nil, fmt.Errorf("the data is SPDX License List %s, not %s", list.Version, builder.SPDXVersion)
	}
	type detail struct {
		Text      string `json:"licenseText"`
		Template  string `json:"standardLicenseTemplate"`
		Header    string `json:"standardLicenseHeader"`
		EText     string `json:"licenseExceptionText"`
		ETemplate string `json:"licenseExceptionTemplate"`
	}
	add := func(category, id string, d detail) {
		text, tmpl := d.Text+d.EText, d.Template+d.ETemplate
		files[category+"/"+id+"/text.txt"] = text
		if r := required(tmpl); words(r) != words(text) && words(r) != "" {
			files[category+"/"+id+"/required.txt"] = r
		}
		if strings.TrimSpace(d.Header) != "" {
			files["Header/"+id+"/header.txt"] = d.Header
		}
	}
	read := func(sub, id string) (detail, error) {
		var d detail
		b, err := os.ReadFile(filepath.Join(dir, "json", sub, id+".json"))
		if err == nil {
			err = json.Unmarshal(b, &d)
		}
		return d, err
	}
	var errs []error
	for _, l := range list.Licenses {
		if !l.Deprecated {
			d, err := read("details", l.ID)
			errs = append(errs, err)
			add("License", l.ID, d)
		}
	}
	for _, e := range list.Exceptions {
		if !e.Deprecated {
			d, err := read("exceptions", e.ID)
			errs = append(errs, err)
			add("Exception", e.ID, d)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	gz, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	tw := tar.NewWriter(gz)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: int64(len(files[n])), Typeflag: tar.TypeReg, Format: tar.FormatPAX}); err != nil {
			return nil, err
		}
		if _, err := tw.Write([]byte(files[n])); err != nil {
			return nil, err
		}
	}
	if err := errors.Join(tw.Close(), gz.Close()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var (
	optional = regexp.MustCompile(`<<beginOptional[^>]*>>|<<endOptional>>`)
	variable = regexp.MustCompile(`(?s)<<var;[^>]*?original="(.*?)";match="(.*?)">>`)
	empty    = regexp.MustCompile(`^\.\{0,[0-9]+\}$`)
)

// required returns the text a template requires: its optional parts, nested or not,
// left out, and each variable at its original text, or left out where its match takes
// the empty text.
func required(tmpl string) string {
	var b strings.Builder
	depth := 0
	rest := tmpl
	for {
		loc := optional.FindStringIndex(rest)
		if loc == nil {
			if depth == 0 {
				b.WriteString(rest)
			}
			break
		}
		if depth == 0 {
			b.WriteString(rest[:loc[0]])
		}
		if strings.HasPrefix(rest[loc[0]:], "<<begin") {
			depth++
		} else if depth > 0 {
			depth--
		}
		rest = rest[loc[1]:]
	}
	return variable.ReplaceAllStringFunc(b.String(), func(v string) string {
		m := variable.FindStringSubmatch(v)
		if empty.MatchString(m[2]) {
			return ""
		}
		return m[1]
	})
}

// words returns the words of s, lowercase, one space apart.
func words(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
