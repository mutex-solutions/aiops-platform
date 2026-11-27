// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/mutex-solutions/aiops-platform/internal/plugingen"
)

// The toolchain module (ADR-044 decision 16): the standard library of a graph is a
// package of this module at this version, whose tree is the GOROOT of the build.
// ToolchainSum is the h1 sum of its module zip, which every signed manifest records
// among its pins and the local path checks the module against (ADR-060 decision 20).
// The pin is linux-amd64, the toolchain the builder image ships (ADR-044 decision 4).
const (
	Toolchain        = "golang.org/toolchain"
	ToolchainVersion = "v0.0.1-go1.26.8.linux-amd64"
	ToolchainSum     = "h1:ZOmGe1OnfREDMIdb1Qi4G9JSuDPBLGExZMQ4nis1RXM="
)

// Go runs a go command: Bin, "go" for the one on the PATH, in Dir with Env, the
// caller's environment when nil.
type Go struct {
	Bin, Dir string
	Env      []string
}

// Run runs go with args and returns its standard output, on a failure too.
func (g Go) Run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, cmp.Or(g.Bin, "go"), args...)
	cmd.Dir, cmd.Env = g.Dir, g.Env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("go %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// Module is a module of a package graph, with the directory that holds its files.
// Replace is set for a module that a replace line of the main module's go.mod maps to
// a directory (ADR-020 decision 28).
type Module struct {
	Path, Version, Sum, GoModSum string
	Dir                          string
	Main                         bool
	Replace                      *Module
}

// Package is one package of a graph: its import path, directory, module and the Go
// and assembly files it compiles.
type Package struct {
	ImportPath, Dir string
	Standard        bool
	Module          Module
	GoFiles, SFiles []string
}

// Graph returns the package graph of the main package main, go list -deps of it with
// the build flags of every binary (plugingen.BuildFlags) and flags, -test for a test
// binary, the standard library with the toolchain module at the GOROOT of g. It refuses
// a graph that links golang.org/x/net/trace, whose init serves two handlers on
// http.DefaultServeMux, so no manifest records a build without the tag grpcnotrace
// (ADR-020 decision 27).
func Graph(ctx context.Context, g Go, main string, flags ...string) ([]Package, error) {
	root, err := g.Run(ctx, "env", "GOROOT")
	if err != nil {
		return nil, err
	}
	out, err := g.Run(ctx, slices.Concat([]string{"list", "-deps", "-json"}, plugingen.BuildFlags, flags, []string{main})...)
	if err != nil {
		return nil, err
	}
	std := Module{Path: Toolchain, Version: ToolchainVersion, Dir: strings.TrimSpace(string(root))}
	var pkgs []Package
	for dec := json.NewDecoder(bytes.NewReader(out)); ; {
		var p struct {
			Package
			Module *Module // go list's, which shadows Package.Module
		}
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
		pkg := p.Package
		if pkg.Module = std; p.Module != nil {
			pkg.Module = *p.Module
		} else if !p.Standard {
			return nil, fmt.Errorf("package %s of %s is in no module", p.ImportPath, main)
		}
		if p.ImportPath == tracePackage {
			return nil, fmt.Errorf("%s links %s, which serves its handlers on http.DefaultServeMux: build it with the tag grpcnotrace (ADR-020 decision 27)", main, tracePackage)
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs, nil
}

const tracePackage = "golang.org/x/net/trace"

// modulesOf returns the modules of the packages of pkgs that are not the standard
// library, by path.
func modulesOf(pkgs []Package) map[string]Module {
	out := map[string]Module{}
	for _, p := range pkgs {
		if !p.Standard {
			out[p.Module.Path] = p.Module
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string { return slices.Sorted(maps.Keys(m)) }

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
