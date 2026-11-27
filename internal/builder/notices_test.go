// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Beside every binary the builder writes, and in every image, go THIRD_PARTY_LICENSES/
// with each licence and notice file under its module and path, the NOTICE texts
// aggregated with one source line per MPL-2.0 module, and an SBOM, SPDX 2.3 in JSON,
// of the binary and each module it links, the toolchain module among them, with its
// version, sum and detected licence (ADR-044 decisions 5, 6 and 15; 08:1336 L7, :1337
// L8).
func TestNotices(t *testing.T) {
	mpl := Module{Path: "example.test/mpl", Version: "v0.5.0", Sum: "h1:mpl=", Dir: moduleDir(t, map[string]string{
		"LICENSE": string(corpusFile(t, "License/MPL-2.0/text.txt")), "m.go": "package m\n"})}
	mit := Module{Path: "example.test/mit", Version: "v1.0.0", Sum: "h1:mit=", Dir: moduleDir(t, map[string]string{
		"LICENSE": string(corpusFile(t, "License/MIT/text.txt")), "NOTICE": "Example mit carries this notice.\n", "x/x.go": "package x\n"})}
	std := Module{Path: Toolchain, Version: ToolchainVersion, Sum: ToolchainSum, Dir: moduleDir(t, map[string]string{
		"LICENSE": string(corpusFile(t, "License/BSD-3-Clause/text.txt")), "src/math/acosh.go": "// Copyright 2010 The Go Authors.\n\npackage math\n\n" +
			"// ====\n// Copyright (C) 1993 by Sun Microsystems, Inc. All rights reserved.\n//\n// Permission to use, copy, modify, and distribute this\n" +
			"// software is freely granted, provided that this notice\n// is preserved.\n// ====\n\nfunc Acosh(x float64) float64 { return x }\n",
		"src/math/abs.go": "// Copyright 2009 The Go Authors.\n\npackage math\n"})}
	pkgs := []Package{{ImportPath: mpl.Path, Dir: mpl.Dir, Module: mpl}, pkg(mit, "x"),
		{ImportPath: "math", Dir: filepath.Join(std.Dir, "src", "math"), Standard: true, Module: std, GoFiles: []string{"abs.go", "acosh.go"}}}
	l, err := ScanLicences(pkgs)
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	created := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	kept, err := Notices(out, "aiops", pkgs, l, created)
	if err != nil || len(kept) != 1 || kept[0].Path != mpl.Path {
		t.Fatalf("Notices: %v, MPL-2.0 modules %v", err, kept)
	}
	for _, f := range []string{"example.test/mpl/LICENSE", "example.test/mit/LICENSE", "example.test/mit/NOTICE", Toolchain + "/LICENSE", Toolchain + "/src/math/acosh.go.NOTICE"} {
		if _, err := os.Stat(filepath.Join(out, "THIRD_PARTY_LICENSES", filepath.FromSlash(f))); err != nil {
			t.Errorf("THIRD_PARTY_LICENSES/%s: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "THIRD_PARTY_LICENSES", Toolchain, "src", "math", "abs.go.NOTICE")); err == nil {
		t.Error("a file with the Go Authors' notice alone has a notice file")
	}
	notice, _ := os.ReadFile(filepath.Join(out, "NOTICE"))
	for _, want := range []string{"Example mit carries this notice.", "Sun Microsystems", "example.test/mpl v0.5.0 is under MPL-2.0, linked unmodified; its source is the module zip example.test/mpl@v0.5.0 (h1:mpl=)"} {
		if !strings.Contains(string(notice), want) {
			t.Errorf("NOTICE holds no %q:\n%s", want, notice)
		}
	}
	var sbom struct {
		SPDXVersion  string `json:"spdxVersion"`
		CreationInfo struct{ Created string }
		Packages     []struct {
			Name, VersionInfo, LicenseConcluded, Comment string
		}
	}
	raw, err := os.ReadFile(filepath.Join(out, "aiops.spdx.json"))
	if err == nil {
		err = json.Unmarshal(raw, &sbom)
	}
	if err != nil || sbom.SPDXVersion != "SPDX-2.3" || sbom.CreationInfo.Created != "2026-11-25T10:00:00Z" {
		t.Fatalf("SBOM: %v, %+v", err, sbom)
	}
	var got []string
	for _, p := range sbom.Packages {
		got = append(got, strings.Join([]string{p.Name, p.VersionInfo, p.LicenseConcluded, p.Comment}, " "))
	}
	want := []string{"aiops  NOASSERTION ", "example.test/mit v1.0.0 MIT sum h1:mit=", "example.test/mpl v0.5.0 MPL-2.0 sum h1:mpl=",
		Toolchain + " " + ToolchainVersion + " BSD-3-Clause sum " + ToolchainSum}
	if !slices.Equal(got, want) {
		t.Errorf("SBOM packages\n%q\nwant\n%q", got, want)
	}
}

// The licence report of the binaries built in the repository, aiops and aiopsctl (ADR-060
// decision 36; 08:1381): every module of each graph, the standard library read from the
// tree of the pinned toolchain, is under a licence of L7 with no acceptance, and the
// builder writes for each its THIRD_PARTY_LICENSES/, NOTICE and SBOM as for an image:
// in aiopsctl's, the PATENTS of x/mod, which its toolbox links.
func TestLicenceReport(t *testing.T) {
	tree := toolchainTree(t)
	for _, c := range []struct{ main, name, want string }{
		{"./cmd/aiops", "aiops", "go.opentelemetry.io/otel/LICENSE"},
		{"./cmd/aiopsctl", "aiopsctl", "golang.org/x/mod/PATENTS"},
	} {
		pkgs, err := Graph(context.Background(), Go{Dir: filepath.Join("..", "..")}, c.main)
		if err != nil {
			t.Fatal(err)
		}
		for i, p := range pkgs {
			if p.Standard {
				pkgs[i].Dir = filepath.Join(tree, strings.TrimPrefix(p.Dir, p.Module.Dir))
				pkgs[i].Module.Dir = tree
			}
		}
		l, err := ScanLicences(pkgs)
		if err == nil {
			err = CheckLicences(l, nil)
		}
		if err != nil {
			t.Fatalf("%s: %v", c.main, err)
		}
		out := t.TempDir()
		if _, err := Notices(out, c.name, pkgs, l, time.Now()); err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{"THIRD_PARTY_LICENSES/" + Toolchain + "/LICENSE", "THIRD_PARTY_LICENSES/" + Toolchain + "/PATENTS", "THIRD_PARTY_LICENSES/" + c.want, "NOTICE", c.name + ".spdx.json"} {
			if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(f))); err != nil {
				t.Errorf("%s: %v", c.name, err)
			}
		}
		t.Logf("%s: %d modules, %d licence and notice files, readings %v", c.name, len(l.Modules), len(l.Files), l.Applied)
	}
}

// A notice is a comment that holds a copyright line of someone other than the Go
// Authors: a run of // lines, or a /* */ block, as the BSD notice of debug/elf/elf.go
// is (ADR-044 decision 27).
func TestNoticeOf(t *testing.T) {
	const goHeader = "// Copyright 2009 The Go Authors. All rights reserved.\n// Use of this source code is governed by a BSD-style\n// license that can be found in the LICENSE file.\n\n"
	block := "/*\nCopyright (c) 1996-1998 John D. Polstra.  All rights reserved.\nCopyright (c) 2001 David E. O'Brien\n\nRedistribution and use in source and binary forms, with or without\nmodification, are permitted.\n*/"
	for src, want := range map[string]string{
		goHeader + "package math\n":            "",
		goHeader + block + "\n\npackage elf\n": block + "\n",
		goHeader + "// Copyright © 1994-1999 Lucent Technologies Inc.\n// All rights reserved.\n\nTEXT ·f(SB),0,$0\n": "// Copyright © 1994-1999 Lucent Technologies Inc.\n// All rights reserved.\n",
		goHeader + "/* not a notice */\n\npackage x\n":                                                                "",
	} {
		if got := noticeOf([]byte(src)); got != want {
			t.Errorf("noticeOf(%q) = %q, want %q", src, got, want)
		}
	}
}

// The list of the standard library's files with a notice other than the Go Authors' is
// the walk of the tree of the pin (ADR-044 decision 27), and a listed file that a
// binary compiles and that carries no notice fails the build.
func TestStdNotices(t *testing.T) {
	std := Module{Path: Toolchain, Version: ToolchainVersion, Dir: moduleDir(t, map[string]string{"src/math/log.go": "package math\n"})}
	_, err := stdNoticeFiles([]Package{{ImportPath: "math", Dir: filepath.Join(std.Dir, "src", "math"), Standard: true, Module: std, GoFiles: []string{"log.go"}}})
	if err == nil || !strings.Contains(err.Error(), "src/math/log.go of the toolchain carries no notice") {
		t.Errorf("a listed file with no notice: %v", err)
	}
	tree := toolchainTree(t)
	got, err := StdNotices(tree)
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, l := range strings.Split(stdNotices, "\n") {
		if l != "" && !strings.HasPrefix(l, "#") {
			want = append(want, l)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("stdnotices.txt is not the walk of the tree of %s:\n%s", ToolchainVersion, strings.Join(got, "\n"))
	}
	files, err := stdNoticeFiles([]Package{{ImportPath: "runtime", Dir: filepath.Join(tree, "src", "runtime"), Standard: true,
		Module: Module{Path: Toolchain, Version: ToolchainVersion, Dir: tree}, SFiles: []string{"memmove_amd64.s"}}})
	if err != nil || len(files) != 1 || !strings.Contains(string(files[0].Data), "Lucent Technologies") {
		t.Errorf("the notice of runtime/memmove_amd64.s: %v, %v", err, files)
	}
	files, err = stdNoticeFiles([]Package{{ImportPath: "debug/elf", Dir: filepath.Join(tree, "src", "debug", "elf"), Standard: true,
		Module: Module{Path: Toolchain, Version: ToolchainVersion, Dir: tree}, GoFiles: []string{"elf.go"}}})
	if err != nil || len(files) != 1 || !strings.Contains(string(files[0].Data), "John D. Polstra") {
		t.Errorf("the notice of debug/elf/elf.go, in a /* */ block: %v, %v", err, files)
	}
}
