// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"fmt"
	"go/token"
	"os"
	"slices"
	"strings"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/vta"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"github.com/mutex-solutions/aiops-platform/internal/plugingen"
)

// The init deny-list (ADR-044 decisions 11, 12, 21, 24 and 25; 08:402): a call of a
// function of registrations, or of a method of a registry on its default value, that
// the call graph reaches from the init function of any package of the server's graph,
// which runs the package-level variable initializers and every init, the package that
// declares the registry included, is a finding unless exceptions names it.
var registrations = map[string]string{
	"database/sql.Register": "a database/sql driver",
	"net/http.Handle":       "a default-mux handler",
	"net/http.HandleFunc":   "a default-mux handler",
	"github.com/prometheus/client_golang/prometheus.Register":                       "a default-registry collector",
	"github.com/prometheus/client_golang/prometheus.MustRegister":                   "a default-registry collector",
	"google.golang.org/grpc/encoding.RegisterCodec":                                 "a global codec",
	"google.golang.org/grpc/encoding.RegisterCodecV2":                               "a global codec",
	"google.golang.org/grpc/encoding.RegisterCompressor":                            "a global codec",
	"encoding/gob.Register":                                                         "a global codec",
	"encoding/gob.RegisterName":                                                     "a global codec",
	"(*net/http.ServeMux).Handle@net/http.DefaultServeMux":                          "a default-mux handler",
	"(*net/http.ServeMux).HandleFunc@net/http.DefaultServeMux":                      "a default-mux handler",
	"Register@github.com/prometheus/client_golang/prometheus.DefaultRegisterer":     "a default-registry collector",
	"MustRegister@github.com/prometheus/client_golang/prometheus.DefaultRegisterer": "a default-registry collector",
}

// promauto is the package whose constructors, its package-level New functions,
// register with the Prometheus client's DefaultRegisterer.
const promauto = "github.com/prometheus/client_golang/prometheus/promauto"

// exception is one registration the scan admits: the registering package, the module
// and the version it is read at, which a bump reads again, the function it calls, and
// why (ADR-044 decision 21). The table changes only by a reviewed pull request, and a
// site adds none.
type exception struct {
	pkg, module, version, call, why string
}

var exceptions = []exception{
	{"google.golang.org/grpc/encoding/proto", "google.golang.org/grpc", "v1.82.1", "google.golang.org/grpc/encoding.RegisterCodecV2",
		"gRPC's proto codec, registered by every binary that links gRPC, which sigstore-go links; with the tag grpcnotrace no other gRPC registration is in the graph (ADR-020 decision 27)"},
	// sigstore-go links go-openapi, which links encoding/gob: gob's own init registers its
	// basic types, which counts (ADR-044 decision 24), and two go-openapi packages
	// register the generic map and slice types from init. Each enters only gob's own
	// type registry, which no row of a server uses.
	{"encoding/gob", Toolchain, ToolchainVersion, "encoding/gob.Register",
		"gob's init registers its basic types with its own type registry (registerBasics), in every binary that links gob"},
	{"github.com/go-openapi/loads", "github.com/go-openapi/loads", "v0.25.0", "encoding/gob.Register",
		"its init registers map[string]any and []any with gob's type registry (spec.go)"},
	{"github.com/go-openapi/spec", "github.com/go-openapi/spec", "v0.22.9", "encoding/gob.Register",
		"its init registers map[string]any and []any with gob's type registry (operation.go)"},
}

// Finding is one registration the scan found: its kind, the call, the function that
// makes it and the init it is reached from.
type Finding struct {
	Kind, Call, In, From string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s: %s in %s, reached from the init of %s", f.Kind, f.Call, f.In, f.From)
}

// ScanInits loads the program of the main packages mains in dir with env, with the build
// flags of every binary, builds its SSA form and the call graph of the whole program
// that golang.org/x/tools/go/callgraph/vta builds, which follows static calls,
// interface method calls and calls through function values, and returns every
// registration reachable from an init that no exception names. A call through
// reflection or unsafe is not followed (ADR-044 decision 12).
func ScanInits(ctx context.Context, dir string, env []string, mains ...string) ([]Finding, error) {
	// go/packages runs the go that the process's PATH names, not env's, so env's PATH
	// stands for it while the graph loads: the builder's go is the pinned toolchain's,
	// never the host's (ADR-044 decision 16), and the builder image has no other.
	// ponytail: process-wide while it loads, which suits aiopsctl, one command per
	// process; a go/packages driver of its own if the scan ever runs beside other work.
	for _, kv := range env {
		if p, ok := strings.CutPrefix(kv, "PATH="); ok {
			old := os.Getenv("PATH")
			defer os.Setenv("PATH", old)
			os.Setenv("PATH", p)
		}
	}
	cfg := &packages.Config{Context: ctx, Dir: dir, Env: env, BuildFlags: plugingen.BuildFlags,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps |
			packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedTypesSizes | packages.NeedModule}
	pkgs, err := packages.Load(cfg, mains...)
	if err != nil {
		return nil, err
	}
	var errs []string
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			errs = append(errs, e.Error())
		}
	})
	if len(errs) > 0 {
		return nil, fmt.Errorf("load %s: %s", strings.Join(mains, " "), strings.Join(errs, "; "))
	}
	versions := map[string]string{}
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		versions[p.PkgPath] = Toolchain + "@" + ToolchainVersion
		if p.Module != nil {
			versions[p.PkgPath] = p.Module.Path + "@" + p.Module.Version
		}
	})
	prog, _ := ssautil.AllPackages(pkgs, ssa.InstantiateGenerics)
	prog.Build()
	cg := vta.CallGraph(ssautil.AllFunctions(prog), nil)
	var out []Finding
	seen := map[*callgraph.Node]string{}
	var queue []*callgraph.Node
	for _, p := range prog.AllPackages() {
		if n := cg.Nodes[p.Func("init")]; n != nil && seen[n] == "" {
			seen[n] = p.Pkg.Path()
			queue = append(queue, n)
		}
	}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, e := range n.Out {
			if kind, call := registration(e); kind != "" {
				f := Finding{Kind: kind, Call: call, In: e.Caller.Func.String(), From: seen[n]}
				if !excepted(e.Caller.Func, call, versions) && !slices.Contains(out, f) {
					out = append(out, f)
				}
				continue // the registry's own body is not followed
			}
			if seen[e.Callee] == "" {
				seen[e.Callee] = seen[n]
				queue = append(queue, e.Callee)
			}
		}
	}
	return out, nil
}

// registration returns the kind of the registration that the call of e makes, and the
// call, or "" for none.
func registration(e *callgraph.Edge) (string, string) {
	callee := e.Callee.Func
	name := callee.String()
	if callee.Pkg != nil && callee.Pkg.Pkg.Path() == promauto && callee.Signature.Recv() == nil && strings.HasPrefix(callee.Name(), "New") {
		return "a default-registry collector", name
	}
	if k, ok := registrations[name]; ok {
		return k, name
	}
	if e.Site == nil {
		return "", ""
	}
	c := e.Site.Common()
	recv := c.Value
	if !c.IsInvoke() && len(c.Args) > 0 && callee.Signature.Recv() != nil {
		recv = c.Args[0]
	}
	method := name
	if c.IsInvoke() {
		method = c.Method.Name()
	}
	if u, ok := recv.(*ssa.UnOp); ok && u.Op == token.MUL {
		if g, ok := u.X.(*ssa.Global); ok && g.Pkg != nil {
			key := method + "@" + g.Pkg.Pkg.Path() + "." + g.Name()
			if k, ok := registrations[key]; ok {
				return k, key
			}
		}
	}
	return "", ""
}

// excepted reports whether an exception names the registration call made in fn, at the
// version of its module that versions records.
func excepted(fn *ssa.Function, call string, versions map[string]string) bool {
	if fn.Pkg == nil {
		return false
	}
	path := fn.Pkg.Pkg.Path()
	return slices.ContainsFunc(exceptions, func(x exception) bool {
		return x.pkg == path && x.call == call && versions[path] == x.module+"@"+x.version
	})
}
