package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
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
	imports := map[string]string{}
	for _, im := range f.Imports {
		path, _ := strconv.Unquote(im.Path.Value)
		name := path[strings.LastIndex(path, "/")+1:]
		if im.Name != nil {
			name = im.Name.Name
		}
		imports[name] = path
	}
	bad := func(pos token.Pos, msg, doc string) {
		r.violations = append(r.violations, fmt.Sprintf("%s:%d: %s (%s)", rel, fset.Position(pos).Line, msg, doc))
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
// name is boot gets the allow-go relaxation.
func checkPluginVet(dirs []string) error {
	var res vetResult
	for _, dir := range dirs {
		dir = filepath.Clean(dir)
		boot := filepath.Base(dir) == "boot"
		err := walkGo(dir, func(p, rel string, test bool) error {
			if test || vetSkipped(filepath.Dir(p)) {
				return nil
			}
			r, err := vetFile(p, filepath.ToSlash(filepath.Join(dir, rel)), boot)
			res.violations = append(res.violations, r.violations...)
			res.exemptions = append(res.exemptions, r.exemptions...)
			return err
		})
		if err != nil {
			return err
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
