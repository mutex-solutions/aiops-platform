// SPDX-License-Identifier: Apache-2.0

// Package vet is the import analyzer and plugin vet: step 3 of plugin check, T0
// static, and the import boundary and plugin vet of make check (ADR-060 decision 23;
// 07-c4-plugin-sdk.md:1569). tools/kernelcheck runs Imports and PluginVet on this
// repository; aiopsctl runs Module on the plugin of an author, in any module, with no
// checkout of this repository.
package vet

import (
	"bufio"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// skipDirs are the directories that no walk reads.
var skipDirs = map[string]bool{".git": true, ".claude": true, "node_modules": true, "testdata": true}

// Finding is one finding of step 3: its file, relative to the plugin directory, its
// line and its text.
type Finding struct {
	File string
	Line int
	Text string
}

func (f Finding) String() string { return fmt.Sprintf("%s:%d: %s", f.File, f.Line, f.Text) }

// findingOf reads a report line of this package, "<file>:<line>: <text>".
func findingOf(s string) Finding {
	file, rest, _ := strings.Cut(s, ":")
	n, text, _ := strings.Cut(rest, ": ")
	line, _ := strconv.Atoi(n)
	return Finding{File: file, Line: line, Text: text}
}

// moduleOf returns the root of the module that holds dir and its path, read from the
// module line of its go.mod.
func moduleOf(dir string) (root, path string, err error) {
	for d := dir; ; d = filepath.Dir(d) {
		f, err := os.Open(filepath.Join(d, "go.mod"))
		if errors.Is(err, os.ErrNotExist) {
			if filepath.Dir(d) == d {
				return "", "", fmt.Errorf("%s: no go.mod at or above it", dir)
			}
			continue
		}
		if err != nil {
			return "", "", err
		}
		defer f.Close()
		s := bufio.NewScanner(f)
		for s.Scan() {
			if p, ok := strings.CutPrefix(strings.TrimSpace(s.Text()), "module "); ok {
				return d, strings.Trim(strings.TrimSpace(p), `"`), nil
			}
		}
		return "", "", fmt.Errorf("%s: no module line", filepath.Join(d, "go.mod"))
	}
}

// moduleImport is the import rule of a plugin outside this repository: the standard
// library, the SDK and any module outside this repository, and of its own module the
// packages of the plugin and those of no other plugin; a fake or sdk/testkit package in
// tests only. U2 reads the register of this repository and judges no other module.
func moduleImport(root, mod, dir string, test bool, imp string) []string {
	var out []string
	add := func(rule, doc string) { out = append(out, rule+" ("+doc+")") }
	pkg := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(imp, mod)))
	own := hasPath(imp, mod) && (pkg == dir || strings.HasPrefix(pkg, dir+string(filepath.Separator)))
	other := false
	switch {
	case own: // the plugin's own packages, in this repository too
	case hasPath(imp, modPath):
		other = !(isKernel(imp) || isDef(imp) || isSDKPlugin(imp) || test && isTestkit(imp))
	case hasPath(imp, mod):
		_, err := os.Stat(filepath.Join(pkg, "plugin.yaml"))
		other = err == nil
	}
	if other {
		add("a plugin imports the SDK, its own packages and modules outside the platform only; never another plugin, boot, loader, cmd, test, dist", docPlugin)
	}
	if !test && isFakePath(imp) {
		add("no fake or sdk/testkit package in a non-test file", docFake)
	}
	return out
}

// Module runs step 3 on the plugin in dir, a directory of any Go module: the import rule
// of a plugin, read against the path of the module that holds dir, the layout rule, and
// plugin vet with the listener list of the plugin's id, on every Go file under dir
// outside testdata, the standard library type-checked with std. A file that does not
// parse is an error, and so is a standard library that std cannot type-check, which
// would leave the goMethods check silent.
func Module(dir string, std Std) ([]Finding, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	root, mod, err := moduleOf(dir)
	if err != nil {
		return nil, err
	}
	imp := &treeImporter{root: dir, pkgs: map[string]*types.Package{}, std: std.importer()}
	if _, err := imp.Import("sync"); err != nil {
		return nil, fmt.Errorf("plugin vet type-checks no standard library: %w", err)
	}
	id := manifestID(dir, dir)
	var out []Finding
	pkgs := map[string]map[string]string{}
	err = walkGo(dir, func(p, rel string, test bool) error {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, parser.ImportsOnly|parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, im := range f.Imports {
			path, _ := strconv.Unquote(im.Path.Value)
			for _, r := range moduleImport(root, mod, dir, test, path) {
				out = append(out, Finding{File: rel, Line: fset.Position(im.Pos()).Line, Text: "imports " + path + ": " + r})
			}
		}
		if test {
			return nil
		}
		if rel != "plugin.go" && !strings.HasPrefix(rel, "internal/") {
			out = append(out, Finding{File: rel, Line: 1, Text: "everything of a plugin but plugin.go is under internal/ (" + docPlugin + ")"})
		}
		r, err := vetFile(p, rel, false, id)
		for _, v := range r.violations {
			out = append(out, findingOf(v))
		}
		d := filepath.Dir(p)
		if pkgs[d] == nil {
			pkgs[d] = map[string]string{}
		}
		pkgs[d][p] = rel
		return err
	})
	if err != nil {
		return nil, err
	}
	for _, files := range pkgs {
		r, err := vetMethods(files, imp, false, id)
		if err != nil {
			return nil, err
		}
		for _, v := range r.violations {
			out = append(out, findingOf(v))
		}
	}
	return out, nil
}
