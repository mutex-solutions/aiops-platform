package main

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Document references for plugin vet.
const (
	docVet = "plugin-system-2026-11.md:500; 08-profiles-roadmap-adrs.md:1256"
	docGo  = "plugin-system-2026-11.md:500 (bare go); 07-c4-plugin-sdk.md:37 (signals are boot's)"
)

// vetSkip lists directories, as slash path fragments, that plugin vet does not
// read: the S0 spike probes of plugins/jobs-river. They are measurement
// harnesses that open pools and spawn goroutines by design and are not plugin
// code; delete an entry when its directory is deleted.
var vetSkip = []string{
	"plugins/jobs-river/internal/s0probe",
	"plugins/jobs-river/internal/s0txprobe",
	"plugins/jobs-river/internal/s0typedops",
	"plugins/jobs-river/internal/s0workers",
}

// forbiddenCalls maps an import path to the selectors that may not be used.
// A "!" entry names the only permitted selector of the package.
var forbiddenCalls = map[string][]string{
	"time":      {"Sleep"},
	"os/signal": {"Notify", "NotifyContext"},
	"os":        {"Setenv", "Exit"},
	"log":       {"Fatal", "Fatalf", "Fatalln"},
	"net/http":  {"DefaultServeMux", "Handle", "HandleFunc"},
	"github.com/prometheus/client_golang/prometheus":          {"DefaultRegisterer", "DefaultGatherer", "MustRegister", "Register", "Unregister"},
	"github.com/prometheus/client_golang/prometheus/promhttp": {"Handler"},
	"github.com/prometheus/client_golang/prometheus/promauto": {"!With"},
}

// goFuncs start an untracked goroutine like a bare go statement does. Plugin
// code may not call them; boot keeps its own clock on time.AfterFunc.
var goFuncs = map[string]string{"time": "AfterFunc", "context": "AfterFunc"}

// goMethods start an untracked goroutine too. Matching one needs the type of
// the receiver, so plugin code is type-checked per directory (vetMethods).
var goMethods = map[string]bool{"(*sync.WaitGroup).Go": true}

// serveHandler gives the handler argument of the functions that serve
// http.DefaultServeMux when that argument is nil.
var serveHandler = map[string]int{
	"net/http.Serve": 1, "net/http.ServeTLS": 1, "net/http.ListenAndServe": 1, "net/http.ListenAndServeTLS": 3,
	"net/http/fcgi.Serve": 1, "net/http/cgi.Serve": 0,
}

func isNil(e ast.Expr) bool { id, ok := e.(*ast.Ident); return ok && id.Name == "nil" }

// stdOnly imports the standard library and fails any other path: plugin vet
// needs no type of the SDK or of a third party to find a goMethods call.
type stdOnly struct{ types.Importer }

func (s stdOnly) Import(p string) (*types.Package, error) {
	if !isStd(p) {
		return nil, fmt.Errorf("%s: not type-checked by plugin vet", p)
	}
	return s.Importer.Import(p)
}

var stdImporter = stdOnly{importer.Default()}

// vetMethods type-checks the files of one directory, given as path to report
// name, and reports each use of a goMethods method. Type errors are expected,
// as imports outside the standard library stay unresolved; they are ignored.
func vetMethods(files map[string]string) ([]string, error) {
	fset := token.NewFileSet()
	var parsed []*ast.File
	for p := range files {
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, f)
	}
	info := &types.Info{Selections: map[*ast.SelectorExpr]*types.Selection{}}
	conf := types.Config{Importer: stdImporter, Error: func(error) {}}
	_, _ = conf.Check("p", fset, parsed, info)
	var out []string
	for sel, s := range info.Selections {
		if fn, ok := s.Obj().(*types.Func); ok && goMethods[fn.FullName()] {
			pos := fset.PositionFor(sel.Sel.Pos(), false)
			out = append(out, fmt.Sprintf("%s:%d: %s starts an untracked goroutine; use the kernel's tracked goroutines (%s)", files[pos.Filename], pos.Line, fn.FullName(), docVet))
		}
	}
	return out, nil
}

type vetResult struct {
	violations []string
	exemptions []string
}

func hasMarker(g *ast.CommentGroup, marker string) (string, bool) {
	if g == nil {
		return "", false
	}
	for _, c := range g.List {
		t := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(c.Text, "//"), "/*"))
		if r, ok := strings.CutPrefix(t, marker); ok {
			return strings.TrimSpace(r), true
		}
	}
	return "", false
}

// allowedVarCall reports whether a package-level initializer is one of the
// permitted forms: kernel.Define, kernel.New*, errors.New, fmt.Errorf, or any
// function of a definition package.
func allowedVarCall(e ast.Expr, imports map[string]string) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	fun := call.Fun
	switch x := fun.(type) { // kernel.NewKey[T](...)
	case *ast.IndexExpr:
		fun = x.X
	case *ast.IndexListExpr:
		fun = x.X
	}
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	switch p := imports[id.Name]; {
	case p == "errors":
		return sel.Sel.Name == "New"
	case p == "fmt":
		return sel.Sel.Name == "Errorf"
	case isKernel(p):
		return sel.Sel.Name == "Define" || strings.HasPrefix(sel.Sel.Name, "New")
	case isDef(p):
		return true
	}
	return false
}

// vetFile checks one non-test file. boot relaxes the bare go rule to a
// //kernelcheck:allow-go comment.
func vetFile(p, rel string, boot bool) (vetResult, error) {
	var r vetResult
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, p, nil, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return r, err
	}
	bad := func(pos token.Pos, msg, doc string) {
		r.violations = append(r.violations, fmt.Sprintf("%s:%d: %s (%s)", rel, fset.Position(pos).Line, msg, doc))
	}
	imports := map[string]string{}
	for _, im := range f.Imports {
		path, _ := strconv.Unquote(im.Path.Value)
		name := path[strings.LastIndex(path, "/")+1:]
		if im.Name != nil {
			name = im.Name.Name
		}
		if name == "." {
			bad(im.Pos(), "dot import of "+path+"; plugin vet reads a call through its package name", docVet)
		}
		imports[name] = path
	}
	// pkgOf returns the import path of the package that e names, if any.
	pkgOf := func(e ast.Expr) string {
		if id, ok := e.(*ast.Ident); ok {
			return imports[id.Name]
		}
		return ""
	}
	// Comment groups by the line they end on, for allow-go.
	ends := map[int]*ast.CommentGroup{}
	for _, g := range f.Comments {
		ends[fset.Position(g.End()).Line] = g
	}
	for _, d := range f.Decls {
		switch x := d.(type) {
		case *ast.FuncDecl:
			if x.Recv == nil && x.Name.Name == "init" {
				bad(x.Pos(), "init function", docVet)
			}
		case *ast.GenDecl:
			if x.Tok != token.VAR {
				continue
			}
			for _, s := range x.Specs {
				vs := s.(*ast.ValueSpec)
				if vetVarOK(vs, imports) {
					continue
				}
				reason, ok := "", false
				for _, g := range []*ast.CommentGroup{x.Doc, vs.Doc, vs.Comment} {
					if t, found := hasMarker(g, "kernelcheck:allow-var"); found {
						reason, ok = t, true
					}
				}
				line := fset.Position(vs.Pos()).Line
				switch {
				case ok && reason != "":
					r.exemptions = append(r.exemptions, fmt.Sprintf("%s:%d: allow-var: %s", rel, line, reason))
				case ok:
					bad(vs.Pos(), "kernelcheck:allow-var needs a reason", docVet)
				default:
					bad(vs.Pos(), "package-level var "+vs.Names[0].Name+" is mutable state; allowed: Plugin = kernel.Define, errors.New, fmt.Errorf, var _ I, definition constructors", docVet)
				}
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.GoStmt:
			line := fset.Position(x.Pos()).Line
			g := ends[line-1]
			if g == nil {
				g = ends[line]
			}
			reason, ok := hasMarker(g, "kernelcheck:allow-go")
			switch {
			case boot && ok && reason != "":
				r.exemptions = append(r.exemptions, fmt.Sprintf("%s:%d: allow-go: %s", rel, line, reason))
			case boot:
				bad(x.Pos(), "bare go statement; boot needs a //kernelcheck:allow-go <reason> comment", docGo)
			default:
				bad(x.Pos(), "bare go statement; use the kernel's tracked goroutines", docVet)
			}
		case *ast.SelectorExpr:
			id, ok := x.X.(*ast.Ident)
			if !ok {
				return true
			}
			path := imports[id.Name]
			for _, name := range forbiddenCalls[path] {
				only, isOnly := strings.CutPrefix(name, "!")
				if (isOnly && x.Sel.Name != only) || (!isOnly && x.Sel.Name == name) {
					bad(x.Pos(), fmt.Sprintf("%s.%s is forbidden", id.Name, x.Sel.Name), docVet)
					break
				}
			}
			if !boot && goFuncs[path] == x.Sel.Name {
				bad(x.Pos(), fmt.Sprintf("%s.%s starts an untracked goroutine; use the kernel's tracked goroutines", id.Name, x.Sel.Name), docVet)
			}
		case *ast.CallExpr:
			sel, ok := x.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if i, ok := serveHandler[pkgOf(sel.X)+"."+sel.Sel.Name]; ok && i < len(x.Args) && isNil(x.Args[i]) {
				bad(x.Pos(), fmt.Sprintf("%s.%s with a nil handler serves http.DefaultServeMux", sel.X.(*ast.Ident).Name, sel.Sel.Name), docVet)
			}
		case *ast.CompositeLit:
			sel, ok := x.Type.(*ast.SelectorExpr)
			if !ok || pkgOf(sel.X) != "net/http" || sel.Sel.Name != "Server" {
				return true
			}
			for _, e := range x.Elts {
				if kv, ok := e.(*ast.KeyValueExpr); ok && !isNil(kv.Value) {
					if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Handler" {
						return true
					}
				}
			}
			bad(x.Pos(), sel.X.(*ast.Ident).Name+".Server without a Handler serves http.DefaultServeMux; set Handler in the literal", docVet)
		}
		return true
	})
	return r, nil
}

// vetVarOK reports whether the declaration needs no exemption.
func vetVarOK(vs *ast.ValueSpec, imports map[string]string) bool {
	if vs.Names[0].Name == "_" {
		return true // compile-time assertion, var _ I = ...
	}
	if len(vs.Values) == 0 {
		return false
	}
	for _, v := range vs.Values {
		if !allowedVarCall(v, imports) {
			return false
		}
	}
	return true
}

func vetSkipped(p string) bool {
	s := "/" + filepath.ToSlash(p)
	for _, k := range vetSkip {
		if strings.Contains(s, "/"+k+"/") || strings.HasSuffix(s, "/"+k) {
			return true
		}
	}
	return false
}

// checkPluginVet reads the non-test Go files under each dir. A dir whose base
// name is boot gets the allow-go relaxation and no goroutine-starter checks.
func checkPluginVet(dirs []string) error {
	var res vetResult
	for _, dir := range dirs {
		dir = filepath.Clean(dir)
		boot := filepath.Base(dir) == "boot"
		pkgs := map[string]map[string]string{} // directory -> file path -> report name
		err := walkGo(dir, func(p, rel string, test bool) error {
			if test || vetSkipped(filepath.Dir(p)) {
				return nil
			}
			name := filepath.ToSlash(filepath.Join(dir, rel))
			r, err := vetFile(p, name, boot)
			res.violations = append(res.violations, r.violations...)
			res.exemptions = append(res.exemptions, r.exemptions...)
			if d := filepath.Dir(p); !boot {
				if pkgs[d] == nil {
					pkgs[d] = map[string]string{}
				}
				pkgs[d][p] = name
			}
			return err
		})
		if err != nil {
			return err
		}
		for _, files := range pkgs {
			v, err := vetMethods(files)
			if err != nil {
				return err
			}
			res.violations = append(res.violations, v...)
		}
	}
	sort.Strings(res.exemptions)
	for _, e := range res.exemptions {
		fmt.Println("exempt", e)
	}
	fmt.Printf("pluginvet: %d exemption(s)\n", len(res.exemptions))
	if len(res.violations) > 0 {
		sort.Strings(res.violations)
		return fmt.Errorf("plugin vet violations:\n%s", strings.Join(res.violations, "\n"))
	}
	return nil
}
