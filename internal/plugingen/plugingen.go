// SPDX-License-Identifier: Apache-2.0

// Package plugingen writes the generated half of a Go plugin's plugin.yaml and the
// two tests beside it (07-c4-plugin-sdk.md:42, :1432; ADR-060 decisions 11 and 12).
// It is a package of the root module that aiopsctl compiles in and runs as
// aiopsctl plugin check --write, so an author runs it with no checkout; in this
// repository tools/plugingen runs it and the parity check of CI.
//
// It reads the Spec without a kernel change, without an init registry and without
// running Apply: a driver program, written into a go build overlay and never onto
// disk, imports the plugin package and prints what plugin.Generate reads from its
// value. The package declares the value as var Plugin, or, where a binary makes one
// value of a plugin that holds memory of its own, as func New() kernel.Plugin.
// requires.definitions comes from the definition.yaml of each definition in the
// plugin's package graph (ADR-060 decision 9).
//
// It reads plugin.yaml with boot's reader in the strict mode, the reading of plugin
// check, plugin check --write and the parity check: one reader, not a second copy
// (ADR-048 decision 15).
package plugingen

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

const modPath = "github.com/mutex-solutions/aiops-platform"

// target is the plugin package plugingen reads.
type target struct {
	Dir, ImportPath, Name string
	Accessor              string // "Plugin" or "New()"
	ID                    string
}

// Result is what plugingen writes for one plugin directory.
type Result struct {
	Generated plugin.Generated
	Manifest  []byte            // plugin.yaml with its generated half rewritten
	Files     map[string][]byte // the generated tests by file name
}

// Generate reads the plugin package in dir and returns its plugin.yaml with the
// generated half rewritten and the generated tests. dir holds the hand-written half
// in plugin.yaml.
func Generate(dir string) (Result, error) {
	t, err := readTarget(dir)
	if err != nil {
		return Result{}, err
	}
	g, err := runDriver(&t)
	if err != nil {
		return Result{}, err
	}
	defs, err := definitions(t)
	if err != nil {
		return Result{}, err
	}
	if err := Requires(&g, defs); err != nil {
		return Result{}, fmt.Errorf("%s: %w", t.ID, err)
	}
	src, err := os.ReadFile(filepath.Join(dir, "plugin.yaml"))
	if err != nil {
		return Result{}, fmt.Errorf("%s: write the hand-written half of plugin.yaml first: %w", t.ID, err)
	}
	r := Result{Generated: g, Files: map[string][]byte{}}
	if r.Manifest, err = Rewrite(src, g); err != nil {
		return Result{}, err
	}
	// The strict reading of plugin check --write (ADR-048 decision 15): the
	// hand-written half as written, the generated half as rewritten.
	m, err := boot.ReadManifest(filepath.Join(dir, "plugin.yaml"), r.Manifest, boot.ManifestStrict)
	if err != nil {
		return Result{}, err
	}
	if m.ID != t.ID {
		return Result{}, fmt.Errorf("%s/plugin.yaml: id %q is not the ID %q of the plugin's value", dir, m.ID, t.ID)
	}
	config, err := cycleConfig(dir)
	if err != nil {
		return Result{}, err
	}
	var fakes []Definition
	for _, d := range defs {
		if slices.ContainsFunc(g.Uses, func(u plugin.Use) bool { _, ok := d.Classes[u.Key]; return ok }) {
			fakes = append(fakes, d)
		}
	}
	var registers []string
	for _, k := range g.Registers {
		registers = append(registers, k.Key)
	}
	if r.Files[CycleFile], err = cycleTest(t, fakes, registers, config); err != nil {
		return Result{}, err
	}
	if slices.Contains(m.Scope, "process") {
		if r.Files[TenantsFile], err = tenantsTest(t); err != nil {
			return Result{}, err
		}
	}
	return r, nil
}

// Write writes what Generate returns for dir.
func Write(dir string) error {
	r, err := Generate(dir)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), r.Manifest, 0o644); err != nil {
		return err
	}
	for _, name := range []string{CycleFile, TenantsFile} {
		p := filepath.Join(dir, name)
		if b, ok := r.Files[name]; ok {
			err = os.WriteFile(p, b, 0o644)
		} else if err = os.Remove(p); errors.Is(err, os.ErrNotExist) {
			err = nil
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Check is the parity check (07-c4-plugin-sdk.md:1570; 08-profiles-roadmap-adrs.md:1257):
// it fails when the generated half of dir's plugin.yaml, read as data, differs from
// what plugingen writes, or when a generated test differs from its file.
func Check(dir string) error {
	r, err := Generate(dir)
	if err != nil {
		return err
	}
	src, err := os.ReadFile(filepath.Join(dir, "plugin.yaml"))
	if err != nil {
		return err
	}
	errs := []error{Parity(src, r.Generated)}
	for _, name := range []string{CycleFile, TenantsFile} {
		have, err := os.ReadFile(filepath.Join(dir, name))
		want, ok := r.Files[name]
		switch {
		case ok && err != nil:
			errs = append(errs, fmt.Errorf("%s: missing; run plugingen", name))
		case !ok && err == nil:
			errs = append(errs, fmt.Errorf("%s: plugingen writes no such file for this plugin; run plugingen", name))
		case ok && !bytes.Equal(have, want):
			errs = append(errs, fmt.Errorf("%s: differs from what plugingen writes; run plugingen", name))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("%s: %w", dir, err)
	}
	return nil
}

// readTarget reads the package in dir with go list and finds how it declares its
// value.
func readTarget(dir string) (target, error) {
	out, err := goCmd(dir, "list", "-json", ".")
	if err != nil {
		return target{}, err
	}
	var p struct {
		Dir, ImportPath, Name string
		GoFiles               []string
	}
	if err := json.Unmarshal(out, &p); err != nil {
		return target{}, err
	}
	t := target{Dir: p.Dir, ImportPath: p.ImportPath, Name: p.Name}
	fset := token.NewFileSet()
	for _, f := range p.GoFiles {
		file, err := parser.ParseFile(fset, filepath.Join(p.Dir, f), nil, parser.SkipObjectResolution)
		if err != nil {
			return target{}, err
		}
		for _, d := range file.Decls {
			switch d := d.(type) {
			case *ast.GenDecl:
				for _, s := range d.Specs {
					if v, ok := s.(*ast.ValueSpec); ok && d.Tok == token.VAR && slices.ContainsFunc(v.Names, func(n *ast.Ident) bool { return n.Name == "Plugin" }) {
						t.Accessor = "Plugin"
					}
				}
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.Name == "New" && d.Type.Params.NumFields() == 0 && d.Type.Results.NumFields() == 1 && t.Accessor == "" {
					t.Accessor = "New()"
				}
			}
		}
	}
	if t.Accessor == "" {
		return target{}, fmt.Errorf("%s: the package declares neither var Plugin nor func New() kernel.Plugin", t.ImportPath)
	}
	return t, nil
}

// runDriver builds and runs the driver of t in an overlay and returns what
// plugin.Generate reads from the plugin's value. It sets t.ID.
func runDriver(t *target) (plugin.Generated, error) {
	tmp, err := os.MkdirTemp("", "plugingen-")
	if err != nil {
		return plugin.Generated{}, err
	}
	defer os.RemoveAll(tmp)
	main := fmt.Sprintf(`package main

import (
	"encoding/json"
	"os"

	target %q
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

func main() {
	p := target.%s
	g, err := plugin.Generate(p)
	if err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(struct {
		ID        string
		Generated plugin.Generated
	}{p.ID, g})
}
`, t.ImportPath, t.Accessor)
	mainFile := filepath.Join(tmp, "main.go")
	virtual := filepath.Join(t.Dir, "zz_plugingen", "main.go")
	overlay, err := json.Marshal(map[string]map[string]string{"Replace": {virtual: mainFile}})
	if err == nil {
		err = errors.Join(os.WriteFile(mainFile, []byte(main), 0o644), os.WriteFile(filepath.Join(tmp, "overlay.json"), overlay, 0o644))
	}
	if err != nil {
		return plugin.Generated{}, err
	}
	out, err := goCmd(t.Dir, "run", "-overlay", filepath.Join(tmp, "overlay.json"), "./zz_plugingen")
	if err != nil {
		return plugin.Generated{}, err
	}
	var res struct {
		ID        string
		Generated plugin.Generated
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return plugin.Generated{}, fmt.Errorf("%s: the driver printed %q: %w", t.ImportPath, out, err)
	}
	t.ID = res.ID
	return res.Generated, nil
}

// definitions reads the definition.yaml of each package in t's package graph that
// holds one.
func definitions(t target) ([]Definition, error) {
	out, err := goCmd(t.Dir, "list", "-deps", "-f", "{{.ImportPath}}\t{{.Dir}}", ".")
	if err != nil {
		return nil, err
	}
	var defs []Definition
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		path, dir, _ := strings.Cut(l, "\t")
		src, err := os.ReadFile(filepath.Join(dir, "definition.yaml"))
		if errors.Is(err, os.ErrNotExist) || dir == "" {
			continue
		}
		if err != nil {
			return nil, err
		}
		d, err := ParseDefinition(path, src)
		if err != nil {
			return nil, err
		}
		defs = append(defs, d)
	}
	return defs, nil
}

// cycleConfig returns the Go literal of testdata/cycle.yaml in dir, or "nil".
func cycleConfig(dir string) (string, error) {
	src, err := os.ReadFile(filepath.Join(dir, "testdata", "cycle.yaml"))
	if errors.Is(err, os.ErrNotExist) {
		return "nil", nil
	}
	if err != nil {
		return "", err
	}
	var tree any
	if err := yaml.Unmarshal(src, &tree); err != nil {
		return "", fmt.Errorf("%s/testdata/cycle.yaml: %w", dir, err)
	}
	return goLiteral(tree)
}

func goCmd(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go %s in %s: %v: %s", strings.Join(args, " "), dir, err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
