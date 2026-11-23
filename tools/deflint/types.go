package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mutex-solutions/aiops-platform/dist"
	"github.com/mutex-solutions/aiops-platform/examples"
	configrefs "github.com/mutex-solutions/aiops-platform/sdk/def/configrefs/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	testdist "github.com/mutex-solutions/aiops-platform/test/dist"
)

const kernelPath = modPath + "/sdk/kernel"

// distributions returns the plugins of the repository's distributions: the release
// table, the test distribution and the toy plugins of the test release, and the
// launcher, which is no plugin of a module table but provides the six launch keys in
// its prepare step and consumes configRefsDryRun at admission (00-decisions.md:212;
// ADR-059 decision 16).
func distributions() []plugin {
	var ps []kernel.Plugin
	for _, m := range testdist.Modules() { // the release table and the test plugins
		ps = append(ps, m.Plugin)
	}
	for _, m := range dist.Modules() {
		ps = append(ps, m.Plugin)
	}
	ps = append(ps, examples.Plugins()...)
	out := []plugin{{id: "the launcher",
		provides: names(launch.ProfileContextKey, launch.CatalogKey, launch.CmdlineKey, launch.ReadyKey,
			launch.KillSwitchesKey, launch.AppExitKey),
		consumes: names(configrefs.Key)}}
	for _, p := range ps {
		q := plugin{id: p.ID, provides: names(p.Provides...)}
		for _, d := range p.Inject {
			q.consumes = append(q.consumes, d.Key().Name())
		}
		out = append(out, q)
	}
	return out
}

func names(keys ...kernel.AnyKey) []string {
	var out []string
	for _, k := range keys {
		out = append(out, k.Name())
	}
	return out
}

// listed is one package as go list prints it.
type listed struct {
	ImportPath, Dir, Export string
	GoFiles                 []string
	Module                  *struct{ Path string }
}

// list runs go list -deps -export in root and returns the packages by import path.
func list(root string, patterns ...string) (map[string]*listed, error) {
	cmd := exec.Command("go", append([]string{"list", "-deps", "-export", "-json=ImportPath,Dir,Export,GoFiles,Module"}, patterns...)...)
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %v: %s", err, stderr.String())
	}
	pkgs := map[string]*listed{}
	for dec := json.NewDecoder(bytes.NewReader(out)); dec.More(); {
		p := &listed{}
		if err := dec.Decode(p); err != nil {
			return nil, err
		}
		pkgs[p.ImportPath] = p
	}
	return pkgs, nil
}

// checker type-checks a package from its source, with its imports read from the
// export data that go list wrote.
type checker struct {
	fset *token.FileSet
	imp  types.Importer
	pkgs map[string]*listed
}

func newChecker(pkgs map[string]*listed) *checker {
	fset := token.NewFileSet()
	return &checker{fset: fset, pkgs: pkgs, imp: importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		if p := pkgs[path]; p != nil && p.Export != "" {
			return os.Open(p.Export)
		}
		return nil, fmt.Errorf("no export data for %s", path)
	})}
}

func (c *checker) parseDir(path string) ([]*ast.File, error) {
	p := c.pkgs[path]
	if p == nil {
		return nil, fmt.Errorf("go list did not list %s", path)
	}
	var files []*ast.File
	for _, name := range p.GoFiles {
		f, err := parser.ParseFile(c.fset, filepath.Join(p.Dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

func (c *checker) check(path string, files []*ast.File) (*types.Package, *types.Info, error) {
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
	pkg, err := (&types.Config{Importer: c.imp}).Check(path, c.fset, files, info)
	return pkg, info, err
}

// named returns the named type behind t, through one pointer when ptr is true.
func named(t types.Type, ptr bool) (*types.Named, bool) {
	if p, ok := t.(*types.Pointer); ok && ptr {
		t = p.Elem()
	}
	n, ok := t.(*types.Named)
	return n, ok
}

func is(t types.Type, ptr bool, pkg, name string) bool {
	n, ok := named(t, ptr)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == pkg && n.Obj().Name() == name
}

// kernelContext reports a *kernel.Context and stdContext a context.Context.
func kernelContext(t types.Type) bool {
	_, ptr := t.(*types.Pointer)
	return ptr && is(t, true, kernelPath, "Context")
}

func stdContext(t types.Type) bool { return is(t, false, "context", "Context") }

// definitionInterface reports whether t is an interface declared in a definition.
func definitionInterface(t types.Type) bool {
	n, ok := named(t, false)
	return ok && types.IsInterface(n) && n.Obj().Pkg() != nil && hasPath(n.Obj().Pkg().Path(), modPath+"/sdk/def")
}

// keyService returns T of a kernel.Key[T].
func keyService(t types.Type) (types.Type, bool) {
	n, ok := named(t, false)
	if !ok || !is(n, false, kernelPath, "Key") || n.TypeArgs().Len() != 1 {
		return nil, false
	}
	return n.TypeArgs().At(0), true
}

// shapes checks the static rules on the interfaces of a definition package: every key
// serves an interface of the definition, so a consumer sees no method beyond it; a
// *kernel.Context is only ever a first parameter; a registration method takes the
// caller's *kernel.Context first; and a registry, a key interface with a registration
// method, has Inventory(), and every other method of it and of its contribution
// interfaces takes the caller's context first (07-c4-plugin-sdk.md:514-519).
func shapes(pkg *types.Package) []string {
	var out []string
	bad := func(format string, a ...any) { out = append(out, fmt.Sprintf(format, a...)+" ("+docRules+")") }
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		v, ok := scope.Lookup(name).(*types.Var)
		if !ok {
			continue
		}
		t, ok := keyService(v.Type())
		n, isNamed := named(t, false)
		switch {
		case !ok:
			continue
		case !isNamed || n.Obj().Pkg() != pkg || !types.IsInterface(n):
			bad("key %s serves %s, not an interface of the definition", name, t)
			continue
		}
		iface := n.Underlying().(*types.Interface)
		registry, inventory := false, false
		for i := 0; i < iface.NumMethods(); i++ {
			registry = registry || strings.HasPrefix(iface.Method(i).Name(), "Register")
		}
		for i := 0; i < iface.NumMethods(); i++ {
			m := iface.Method(i)
			params := m.Type().(*types.Signature).Params()
			first := params.Len() > 0 && (kernelContext(params.At(0).Type()) || stdContext(params.At(0).Type()))
			for j := 1; j < params.Len(); j++ {
				if kernelContext(params.At(j).Type()) {
					bad("%s.%s takes a *kernel.Context after its first parameter", n.Obj().Name(), m.Name())
				}
			}
			switch {
			case strings.HasPrefix(m.Name(), "Register") && (params.Len() == 0 || !kernelContext(params.At(0).Type())):
				bad("%s.%s registers without the caller's *kernel.Context first", n.Obj().Name(), m.Name())
			case m.Name() == "Inventory":
				inventory = params.Len() == 0 && m.Type().(*types.Signature).Results().Len() > 0
			case registry && !first:
				bad("%s.%s does not take the caller's context first", n.Obj().Name(), m.Name())
			}
			for j := 1; registry && strings.HasPrefix(m.Name(), "Register") && j < params.Len(); j++ {
				contribution(params.At(j).Type(), pkg, bad)
			}
		}
		if registry && !inventory {
			bad("registry %s has no Inventory()", n.Obj().Name())
		}
	}
	return out
}

// declared checks that every package-level key and hook of the definition package pkg
// is named in a test file of the definition that calls deftest.Agree, as the Decl it
// passes: Agree holds what the Decl names to definition.yaml (ADR-060 decision 7), so
// a key or hook left out of both would pass unseen. files reports a definition with no
// such test.
func declared(d *definition, pkg *types.Package) []string {
	named := map[string]bool{}
	for _, p := range goFiles(d.dir, true) {
		src, err := os.ReadFile(p)
		if err != nil || !bytes.Contains(src, []byte("deftest.Agree(")) {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, src, parser.SkipObjectResolution)
		if err != nil {
			return []string{err.Error()}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				named[id.Name] = true
			}
			return true
		})
	}
	if len(named) == 0 {
		return nil
	}
	var out []string
	for _, name := range pkg.Scope().Names() {
		v, ok := pkg.Scope().Lookup(name).(*types.Var)
		if !ok {
			continue
		}
		kind := ""
		switch t := v.Type(); {
		case is(t, false, kernelPath, "Key"):
			kind = "key"
		case is(t, true, kernelPath, "Event"), is(t, true, kernelPath, "Waterfall"):
			kind = "hook"
		}
		if kind != "" && !named[name] {
			out = append(out, fmt.Sprintf("%s %s is not named in the Decl of the deftest.Agree test, so nothing holds it to definition.yaml (ADR-060 decision 7)", kind, name))
		}
	}
	return out
}

// contribution checks that every method of a contribution interface of pkg takes the
// caller's context first.
func contribution(t types.Type, pkg *types.Package, bad func(string, ...any)) {
	n, ok := named(t, false)
	if !ok || n.Obj().Pkg() != pkg || !types.IsInterface(n) {
		return
	}
	iface := n.Underlying().(*types.Interface)
	for i := 0; i < iface.NumMethods(); i++ {
		params := iface.Method(i).Type().(*types.Signature).Params()
		if params.Len() == 0 || !kernelContext(params.At(0).Type()) && !stdContext(params.At(0).Type()) {
			bad("contribution %s.%s does not take the caller's context first", n.Obj().Name(), iface.Method(i).Name())
		}
	}
}

// sideDoors finds every value given to Key.Provide for a key that serves an
// interface of a definition, and every value given to a registration method of such
// an interface, whose method set holds an exported method the interface lacks: a
// consumer could reach it with a type assertion (07-c4-plugin-sdk.md:516). A value of
// an interface type is not judged.
func sideDoors(fset *token.FileSet, root string, info *types.Info, files []*ast.File) []string {
	var out []string
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			s := info.Selections[sel]
			if s == nil || s.Kind() != types.MethodVal {
				return true
			}
			name := s.Obj().Name()
			var want []types.Type
			var args []ast.Expr
			if t, isKey := keyService(s.Recv()); isKey && name == "Provide" && definitionInterface(t) && len(call.Args) > 1 {
				want, args = []types.Type{t}, call.Args[1:2]
			} else if definitionInterface(s.Recv()) && strings.HasPrefix(name, "Register") {
				params := s.Type().(*types.Signature).Params()
				for i := 0; i < params.Len() && i < len(call.Args); i++ {
					if definitionInterface(params.At(i).Type()) {
						want, args = append(want, params.At(i).Type()), append(args, call.Args[i])
					}
				}
			}
			for i, arg := range args {
				if extra := beyond(info.TypeOf(arg), want[i]); len(extra) > 0 {
					pos := fset.Position(arg.Pos())
					rel, _ := filepath.Rel(root, pos.Filename)
					out = append(out, fmt.Sprintf("%s:%d: the value given to %s has methods beyond %s: %s (%s)",
						filepath.ToSlash(rel), pos.Line, name, want[i], strings.Join(extra, ", "), docRules))
				}
			}
			return true
		})
	}
	return out
}

// beyond lists the exported methods of v that the interface i lacks.
func beyond(v, i types.Type) []string {
	if v == nil || types.IsInterface(v) {
		return nil
	}
	iface := i.Underlying().(*types.Interface)
	var out []string
	ms := types.NewMethodSet(v)
	for k := 0; k < ms.Len(); k++ {
		m := ms.At(k).Obj()
		if obj, _, _ := types.LookupFieldOrMethod(iface, false, m.Pkg(), m.Name()); m.Exported() && obj == nil {
			out = append(out, m.Name())
		}
	}
	return out
}
