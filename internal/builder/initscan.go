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
// function of registrations, or a load of a default registry of defaults, in any
// function that the call graph reaches from the init function of any package of the
// server's graph, which runs the package-level variable initializers and every init,
// the package that declares the registry included, is a finding unless exceptions
// names it. A load counts however init then reaches a method of the registry: through
// an interface value, a method value, a variable or a parameter.
var registrations = map[string]string{
	"database/sql.Register": "a database/sql driver",
	"net/http.Handle":       "a default-mux handler",
	"net/http.HandleFunc":   "a default-mux handler",
	"github.com/prometheus/client_golang/prometheus.Register":     "a default-registry collector",
	"github.com/prometheus/client_golang/prometheus.MustRegister": "a default-registry collector",
	"google.golang.org/grpc/encoding.RegisterCodec":               "a global codec",
	"google.golang.org/grpc/encoding.RegisterCodecV2":             "a global codec",
	"google.golang.org/grpc/encoding.RegisterCompressor":          "a global codec",
	"encoding/gob.Register":                                       "a global codec",
	"encoding/gob.RegisterName":                                   "a global codec",
}

// defaults are the default registries of the table whose methods register: a load of
// one is a finding, as a call of a function of registrations is.
var defaults = map[string]string{
	"net/http.DefaultServeMux": "a default-mux handler",
	"github.com/prometheus/client_golang/prometheus.DefaultRegisterer": "a default-registry collector",
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
	// type registry, which no row of a server uses. The owner approved the three on
	// 2026-11-25 at these literal versions only (ADR-044 decision 21; ADR-020 decision 27).
	{"encoding/gob", Toolchain, "v0.0.1-go1.26.8.linux-amd64", "encoding/gob.Register",
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
// interface method calls and calls through function values, with the functions that a
// reached call hands to the runtime (later), and returns every registration reachable
// from an init that no exception names. A call through reflection or unsafe is not
// followed (ADR-044 decision 12).
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
		for _, g := range loads(n.Func) {
			if f := (Finding{Kind: defaults[g], Call: g, In: n.Func.String(), From: seen[n]}); !slices.Contains(out, f) {
				out = append(out, f)
			}
		}
		for _, e := range n.Out {
			if kind, call := registration(e); kind != "" {
				f := Finding{Kind: kind, Call: call, In: e.Caller.Func.String(), From: seen[n]}
				if !excepted(e.Caller.Func, call, versions) && !slices.Contains(out, f) {
					out = append(out, f)
				}
				continue // the registry's own body is not followed
			}
			for _, next := range append([]*callgraph.Node{e.Callee}, handed(cg, e)...) {
				if next != nil && seen[next] == "" {
					seen[next] = seen[n]
					queue = append(queue, next)
				}
			}
		}
	}
	return out, nil
}

// later are the functions that hand a function value to the runtime, which calls it
// through no edge of the call graph, after they return or, for iter.Pull, as a
// coroutine: a function or closure passed to one of them is reached from the init that
// reaches the call.
// ponytail: the function or closure written in the call only; one that a variable or a
// parameter carries there is not followed. And these functions only: every call that
// passes a function to a body-less function is not followed, since such a function
// need not call it.
var later = map[string]bool{"time.AfterFunc": true, "context.AfterFunc": true, "runtime.SetFinalizer": true, "runtime.AddCleanup": true,
	"iter.Pull": true, "iter.Pull2": true}

// handed returns the nodes of cg of the functions that the call of e hands to the
// runtime, nil for one cg does not hold.
func handed(cg *callgraph.Graph, e *callgraph.Edge) []*callgraph.Node {
	var out []*callgraph.Node
	if name, _, _ := strings.Cut(e.Callee.Func.String(), "["); e.Site == nil || !later[name] {
		return nil
	}
	for _, a := range e.Site.Common().Args {
		if c, ok := a.(*ssa.ChangeType); ok { // to a named function type, as iter.Seq
			a = c.X
		}
		if c, ok := a.(*ssa.MakeClosure); ok {
			a = c.Fn
		}
		if fn, ok := a.(*ssa.Function); ok {
			out = append(out, cg.Nodes[fn])
		}
	}
	return out
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
	return "", ""
}

// loads returns each default registry of defaults that fn loads.
// ponytail: a load, not an address taken (&http.DefaultServeMux), which no package of
// the graph writes; count the address too if one ever does.
func loads(fn *ssa.Function) []string {
	var out []string
	for _, b := range fn.Blocks {
		for _, in := range b.Instrs {
			if u, ok := in.(*ssa.UnOp); ok && u.Op == token.MUL {
				if g, ok := u.X.(*ssa.Global); ok && defaults[g.String()] != "" {
					out = append(out, g.String())
				}
			}
		}
	}
	return out
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
