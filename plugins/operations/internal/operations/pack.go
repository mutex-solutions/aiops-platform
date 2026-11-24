// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"
	"unicode/utf8"
)

// The layers that plugin pack reads from a plugin directory (ADR-047 decisions 1 and
// 3): the content and fixtures trees as canonical tars and the SBOM. The module of
// in-process code is the toolbox's (launch.Tools.Module), which aiopsctl alone links.

// regularFiles returns the regular files under dir by slash path relative to it, nil when dir
// does not exist. A link, a device or a path that is not valid UTF-8 and clean fails.
func regularFiles(dir string) (map[string][]byte, error) {
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if !d.Type().IsRegular() || !utf8.ValidString(rel) || !fs.ValidPath(rel) {
			return fmt.Errorf("%q is not a regular file at a clean path", rel)
		}
		files[rel], err = os.ReadFile(p)
		return err
	})
	return files, err
}

// canonicalTar writes files as the tar of ADR-047 decision 3, the form that boot hashes
// a content tree in (boot/install.go): one entry per regular file, sorted by path in
// byte order, mode 0644, owner and group 0 with no names, time 0, a ustar header with
// a pax header only for a path that ustar cannot hold, and the two zero blocks that
// end an archive. It returns the tar and the sorted paths.
func canonicalTar(files map[string][]byte) ([]byte, []string) {
	paths := slices.Sorted(maps.Keys(files))
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	for _, p := range paths {
		// A header of these fields and a write of its size do not fail on a buffer.
		_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: p, Mode: 0o644, Size: int64(len(files[p])), ModTime: time.Unix(0, 0)})
		_, _ = tw.Write(files[p])
	}
	_ = tw.Close()
	return b.Bytes(), paths
}

// untar hands put each regular file of the tar b by its slash path, and refuses an
// entry of another type or a path that leaves the root (ADR-047 decision 3).
func untar(b []byte, put func(name string, data []byte) error) error {
	r := tar.NewReader(bytes.NewReader(b))
	for {
		h, err := r.Next()
		switch {
		case errors.Is(err, io.EOF):
			return nil
		case err != nil:
			return err
		case h.Typeflag != tar.TypeReg || !filepath.IsLocal(filepath.FromSlash(h.Name)):
			return refused("the tar holds %q, which is not a regular file under its root (ADR-047 decision 3)", h.Name)
		}
		data, err := io.ReadAll(r)
		if err == nil {
			err = put(h.Name, data)
		}
		if err != nil {
			return err
		}
	}
}

// sbom is an SPDX 2.3 document in its JSON form, written with encoding/json (ADR-047
// decision 1). It is created at the epoch, so that one directory packs to one digest.
type sbom struct {
	SPDXVersion       string `json:"spdxVersion"`
	DataLicense       string `json:"dataLicense"`
	SPDXID            string `json:"SPDXID"`
	Name              string `json:"name"`
	DocumentNamespace string `json:"documentNamespace"`
	CreationInfo      struct {
		Created  string   `json:"created"`
		Creators []string `json:"creators"`
	} `json:"creationInfo"`
	Packages      []sbomPackage `json:"packages"`
	Files         []sbomFile    `json:"files,omitempty"`
	Relationships []struct {
		Element string `json:"spdxElementId"`
		Type    string `json:"relationshipType"`
		Related string `json:"relatedSpdxElement"`
	} `json:"relationships"`
}

type sbomPackage struct {
	SPDXID           string `json:"SPDXID"`
	Name             string `json:"name"`
	VersionInfo      string `json:"versionInfo"`
	DownloadLocation string `json:"downloadLocation"`
	FilesAnalyzed    bool   `json:"filesAnalyzed"`
	LicenseConcluded string `json:"licenseConcluded"`
	LicenseDeclared  string `json:"licenseDeclared"`
	CopyrightText    string `json:"copyrightText"`
	Comment          string `json:"comment,omitempty"`
}

type sbomFile struct {
	SPDXID    string `json:"SPDXID"`
	FileName  string `json:"fileName"`
	Checksums []struct {
		Algorithm string `json:"algorithm"`
		Value     string `json:"checksumValue"`
	} `json:"checksums"`
	LicenseConcluded string `json:"licenseConcluded"`
	CopyrightText    string `json:"copyrightText"`
}

const noAssertion = "NOASSERTION"

// newSBOM returns the SBOM of the artifact id at version under license, which
// describes the artifact's package.
func newSBOM(id, version, license string) *sbom {
	s := &sbom{SPDXVersion: "SPDX-2.3", DataLicense: "CC0-1.0", SPDXID: "SPDXRef-DOCUMENT", Name: id + "@" + version,
		DocumentNamespace: "https://github.com/mutex-solutions/aiops-platform/spdx/" + id + "/" + version}
	s.CreationInfo.Created, s.CreationInfo.Creators = "1970-01-01T00:00:00Z", []string{"Tool: aiopsctl"}
	s.Packages = []sbomPackage{{SPDXID: "SPDXRef-Package-0", Name: id, VersionInfo: version, DownloadLocation: noAssertion,
		LicenseConcluded: noAssertion, LicenseDeclared: license, CopyrightText: noAssertion}}
	s.Relationships = append(s.Relationships, struct {
		Element string `json:"spdxElementId"`
		Type    string `json:"relationshipType"`
		Related string `json:"relatedSpdxElement"`
	}{"SPDXRef-DOCUMENT", "DESCRIBES", "SPDXRef-Package-0"})
	return s
}

// module adds a Go module with its path, version and sum, none for a replacement by a
// directory, its licence not asserted: the licence scan of a build list is the
// builder's (ADR-047 decision 1).
func (s *sbom) module(path, version, sum string) {
	p := sbomPackage{SPDXID: fmt.Sprintf("SPDXRef-Package-%d", len(s.Packages)), Name: path, VersionInfo: version,
		DownloadLocation: noAssertion, LicenseConcluded: noAssertion, LicenseDeclared: noAssertion, CopyrightText: noAssertion}
	if sum != "" {
		p.Comment = "sum " + sum
	}
	s.Packages = append(s.Packages, p)
}

// file adds a file of the content or fixtures layer with its SHA-256, under license.
func (s *sbom) file(name string, data []byte, license string) {
	f := sbomFile{SPDXID: fmt.Sprintf("SPDXRef-File-%d", len(s.Files)), FileName: name, LicenseConcluded: license, CopyrightText: noAssertion}
	sum := sha256.Sum256(data)
	f.Checksums = append(f.Checksums, struct {
		Algorithm string `json:"algorithm"`
		Value     string `json:"checksumValue"`
	}{"SHA256", hex.EncodeToString(sum[:])})
	s.Files = append(s.Files, f)
}
