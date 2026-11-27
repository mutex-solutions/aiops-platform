// SPDX-License-Identifier: Apache-2.0

package vet

import (
	"cmp"
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
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"
)

// Document references for plugin vet.
const (
	docVet    = "plugin-system-2026-11.md §14 (T0 Static); 08-profiles-roadmap-adrs.md:1256"
	docGo     = "plugin-system-2026-11.md §14 (bare go); 07-c4-plugin-sdk.md:37 (signals are boot's)"
	docListen = "ADR-062 decision 13"
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
// A "!" entry names the only permitted selector of the package. kernel.WithCommit
// gives a Plugin the hand-off that runs at every commit, which only sdk/plugin may
// give (ADR-034 G13).
var forbiddenCalls = map[string][]string{
	"time":      {"Sleep"},
	"os/signal": {"Notify", "NotifyContext"},
	"os":        {"Setenv", "Exit"},
	"log":       {"Fatal", "Fatalf", "Fatalln"},
	"net/http":  {"DefaultServeMux", "Handle", "HandleFunc"},
	"github.com/prometheus/client_golang/prometheus":          {"DefaultRegisterer", "DefaultGatherer", "MustRegister", "Register", "Unregister"},
	"github.com/prometheus/client_golang/prometheus/promhttp": {"Handler"},
	"github.com/prometheus/client_golang/prometheus/promauto": {"!With"},
	"github.com/mutex-solutions/aiops-platform/sdk/kernel":    {"WithCommit"},
}

// listenFuncs are the functions that open a listening socket or bind a socket to an
// address, by import path, and listenMethods the methods that do, by the full name
// the type check gives them (ADR-062 decision 13). A listener is a way into the
// process past the identities and authz checks of http-server (03-c3-kernel.md:109),
// so plugin code calls one only where listenAllow names its plugin.
var listenFuncs = map[string][]string{
	"net": {"Listen", "ListenPacket", "ListenTCP", "ListenUDP", "ListenIP", "ListenUnix", "ListenUnixgram",
		"ListenMulticastUDP", "FileListener", "FilePacketConn"},
	"crypto/tls":        {"Listen"},
	"net/http":          {"ListenAndServe", "ListenAndServeTLS"},
	"net/http/httptest": {"NewServer", "NewTLSServer", "NewUnstartedServer"},
	// A raw syscall can be bind(2) or listen(2), named by a constant or a number.
	"syscall":               {"Listen", "Bind", "Syscall", "Syscall6", "RawSyscall", "RawSyscall6"},
	"golang.org/x/sys/unix": {"Listen", "Bind", "Syscall", "Syscall6", "RawSyscall", "RawSyscall6"},
}

var listenMethods = map[string]bool{
	"(*net.ListenConfig).Listen": true, "(*net.ListenConfig).ListenPacket": true,
	"(*net/http.Server).ListenAndServe": true, "(*net/http.Server).ListenAndServeTLS": true,
	"(*net/http/httptest.Server).Start": true, "(*net/http/httptest.Server).StartTLS": true,
}

// listenerTypes are the types that have the listenMethods, as pointers: a call of a
// method of an interface or of a type parameter that one of them implements reaches
// that type's method, so it is refused as that method is.
var listenerTypes = sync.OnceValue(func() []types.Type {
	var out []types.Type
	for _, t := range [][2]string{{"net", "ListenConfig"}, {"net/http", "Server"}, {"net/http/httptest", "Server"}} {
		if p, err := stdImporter.Import(t[0]); err == nil {
			out = append(out, types.NewPointer(p.Scope().Lookup(t[1]).Type()))
		}
	}
	return out
})

// listenMethod returns the full name of the method of listenMethods that the selection
// s of fn reaches: fn itself, or for a method of an interface or a type parameter the
// method of a listener type that implements it; "" when it reaches none.
func listenMethod(s *types.Selection, fn *types.Func) string {
	if listenMethods[fn.FullName()] {
		return fn.FullName()
	}
	iface, ok := s.Recv().Underlying().(*types.Interface)
	if !ok {
		return ""
	}
	for _, t := range listenerTypes() {
		sel := types.NewMethodSet(t).Lookup(nil, fn.Name())
		if sel == nil {
			continue
		}
		if m := sel.Obj().(*types.Func); listenMethods[m.FullName()] && types.Implements(t, iface) {
			return m.FullName()
		}
	}
	return ""
}

// listenAllow is the one list of ADR-062 decisions 13 and 14: each plugin id, under
// the reserved owner aiops that no third party carries (ADR-048 decision 14), with
// the calls it may make, named as "<import path>.<function>" or by a method's full
// name. Vet reads the id from the plugin.yaml of the plugin that holds the file, so a
// file with no manifest above it matches no entry. aiopsctl plugin check uses the
// same list through Module, in the package both share (ADR-060 decision 23); the
// http-server entry joins with its own ADR at S3.
var listenAllow = map[string][]string{
	"aiops/reload-socket": {"net.ListenUnix"},
	// The named exception of the dial rule (07-c4-plugin-sdk.md:1400): the client of
	// the socket of reload-socket, with the network unix (ADR-062 decision 14). Vet
	// does not build that rule yet; the entry admits no listener.
	"aiops/operations": {"net.DialUnix"},
}

// listens reports whether call, a function of listenFuncs or a method of
// listenMethods, is refused to the plugin id.
func listens(id, call string) bool { return !slices.Contains(listenAllow[id], call) }

// listenViolation reports a listener at where that the list does not allow id.
func listenViolation(where, call, id string) string {
	if id == "" {
		id = "a directory with no plugin.yaml"
	}
	return fmt.Sprintf("%s: %s opens a listener or binds a socket, which the list of plugin vet does not allow %s (%s)", where, call, id, docListen)
}

// manifestID returns the id of the plugin that holds dir, a directory under root, the
// directory given to vet: the id of the plugin.yaml at the root of that plugin, which
// is root when it holds one and otherwise the directory under root that holds dir. It
// returns "" when that manifest is missing or does not read. A plugin.yaml further
// down names no plugin, so a directory of a plugin cannot claim another id.
func manifestID(dir, root string) string {
	b, err := os.ReadFile(filepath.Join(root, "plugin.yaml"))
	if err != nil {
		rel, _ := filepath.Rel(root, dir)
		first, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
		b, err = os.ReadFile(filepath.Join(root, first, "plugin.yaml"))
	}
	var m struct{ ID string }
	if err != nil || yaml.Unmarshal(b, &m) != nil {
		return ""
	}
	return m.ID
}

// goFuncs start an untracked goroutine like a bare go statement does. Plugin
// code may not call them; boot may, with an allow-go comment.
var goFuncs = map[string]string{"time": "AfterFunc", "context": "AfterFunc"}

// muxImports register handlers on http.DefaultServeMux in their init, so even a
// blank import serves them on any listener of the default mux.
var muxImports = map[string]bool{"net/http/pprof": true, "expvar": true}

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

// Std is the go command whose export data plugin vet type-checks the standard library
// with: Go names its binary, "go" on PATH where empty, Env its environment, the
// process's where nil, and Dir where it runs. The builder passes its pinned toolchain
// with its own environment and scratch module (ADR-044 decisions 2 and 16), never what
// build.Default.GOROOT names, which is empty in a binary built with -trimpath.
type Std struct {
	Go  string
	Env []string
	Dir string
}

// importer returns an importer of the standard library from the export data that
// go list -export of s writes, each package listed once with its dependencies.
func (s Std) importer() types.Importer {
	exports := map[string]string{}
	return stdOnly{importer.ForCompiler(token.NewFileSet(), "gc", func(p string) (io.ReadCloser, error) {
		if _, ok := exports[p]; !ok {
			cmd := exec.Command(cmp.Or(s.Go, "go"), "list", "-export", "-deps", "-f", "{{.ImportPath}} {{.Export}}", p)
			cmd.Env, cmd.Dir = s.Env, s.Dir
			out, err := cmd.Output()
			if err != nil {
				return nil, fmt.Errorf("%s list -export %s: %w", cmd.Path, p, err)
			}
			for _, l := range strings.Split(string(out), "\n") {
				if path, file, ok := strings.Cut(l, " "); ok {
					exports[path] = file
				}
			}
		}
		if exports[p] == "" {
			return nil, fmt.Errorf("%s: no export data", p)
		}
		return os.Open(exports[p])
	})}
}

// treeImporter imports the standard library with std and the packages of the vetted
// tree, type-checked from their source, so that a WaitGroup reached through a type or a
// function of another package of the plugin has its type. An import path names a
// package of the tree when it holds "/<base of root>/"; what follows is its directory
// under root. Any other import fails, as with stdOnly.
type treeImporter struct {
	root string
	pkgs map[string]*types.Package
	std  types.Importer
}

func (t *treeImporter) Import(p string) (*types.Package, error) {
	_, rest, ok := strings.Cut(p, "/"+filepath.Base(t.root)+"/")
	if isStd(p) || !ok {
		return t.std.Import(p)
	}
	if pkg, ok := t.pkgs[p]; ok {
		return pkg, nil
	}
	dir := filepath.Join(t.root, filepath.FromSlash(rest))
	fset := token.NewFileSet()
	var files []*ast.File
	names, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	for _, n := range names {
		if f, err := parser.ParseFile(fset, n, nil, parser.SkipObjectResolution); err == nil && !strings.HasSuffix(n, "_test.go") {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s: no Go files in %s", p, dir)
	}
	conf := types.Config{Importer: t, Error: func(error) {}}
	pkg, _ := conf.Check(p, fset, files, nil)
	t.pkgs[p] = pkg
	return pkg, nil
}

// vetMethods type-checks the files of one directory of the plugin id, given as path
// to report name, and reports each use of a goMethods method (untracked) and of a
// listenMethods method that the list does not allow id. Type errors are expected, as
// imports outside the standard library and the tree stay unresolved; they are
// ignored.
func vetMethods(files map[string]string, imp types.Importer, boot bool, id string) (vetResult, error) {
	var r vetResult
	fset := token.NewFileSet()
	var parsed []*ast.File
	byPath := map[string]*ast.File{}
	for p := range files {
		f, err := parser.ParseFile(fset, p, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return r, err
		}
		parsed, byPath[p] = append(parsed, f), f
	}
	info := &types.Info{Selections: map[*ast.SelectorExpr]*types.Selection{}}
	conf := types.Config{Importer: imp, Error: func(error) {}}
	_, _ = conf.Check("p", fset, parsed, info)
	for sel, s := range info.Selections {
		fn, ok := s.Obj().(*types.Func)
		if !ok {
			continue
		}
		pos := fset.PositionFor(sel.Sel.Pos(), false)
		if goMethods[fn.FullName()] {
			r.untracked(fset, byPath[pos.Filename], files[pos.Filename], sel.Sel.Pos(), fn.FullName()+" starts an untracked goroutine", boot)
		}
		if m := listenMethod(s, fn); m != "" && listens(id, m) {
			r.violations = append(r.violations, listenViolation(fmt.Sprintf("%s:%d", files[pos.Filename], pos.Line), m, id))
		}
	}
	return r, nil
}

type vetResult struct {
	violations []string
	exemptions []string
}

// untracked records what starts a goroutine no fiber tracks, at pos of f (reported as
// rel). Plugin code may not; boot may with a //kernelcheck:allow-go <reason> comment on
// the line or the line above, which is counted as an exemption.
func (r *vetResult) untracked(fset *token.FileSet, f *ast.File, rel string, pos token.Pos, what string, boot bool) {
	line := fset.Position(pos).Line
	var reason string
	ok := false
	for _, at := range []int{line, line - 1} { // a comment on the line itself first
		for _, g := range f.Comments {
			if !ok && fset.Position(g.End()).Line == at {
				reason, ok = hasMarker(g, "kernelcheck:allow-go")
			}
		}
	}
	switch {
	case boot && ok && reason != "":
		r.exemptions = append(r.exemptions, fmt.Sprintf("%s:%d: allow-go: %s", rel, line, reason))
	case boot:
		r.violations = append(r.violations, fmt.Sprintf("%s:%d: %s; boot needs a //kernelcheck:allow-go <reason> comment (%s)", rel, line, what, docGo))
	default:
		r.violations = append(r.violations, fmt.Sprintf("%s:%d: %s; use the kernel's tracked goroutines (%s)", rel, line, what, docVet))
	}
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
// permitted forms: kernel.Define, kernel.New*, plugin.Define of sdk/plugin,
// errors.New, fmt.Errorf, or any function of a definition package.
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
	case isSDKPlugin(p):
		return sel.Sel.Name == "Define"
	case isDef(p):
		return true
	}
	return false
}

// vetFile checks one non-test file of the plugin pluginID. boot relaxes the bare go
// rule to a //kernelcheck:allow-go comment.
func vetFile(p, rel string, boot bool, pluginID string) (vetResult, error) {
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
		if muxImports[path] {
			bad(im.Pos(), "import of "+path+" registers handlers on http.DefaultServeMux", docVet)
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
	// isServer reports whether e names net/http's Server.
	isServer := func(e ast.Expr) bool {
		sel, ok := e.(*ast.SelectorExpr)
		return ok && pkgOf(sel.X) == "net/http" && sel.Sel.Name == "Server"
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
					bad(vs.Pos(), "package-level var "+vs.Names[0].Name+" is mutable state; allowed: Plugin = kernel.Define or plugin.Define, errors.New, fmt.Errorf, var _ I, definition constructors", docVet)
				}
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.GoStmt:
			r.untracked(fset, f, rel, x.Pos(), "bare go statement", boot)
		case *ast.ValueSpec:
			// A zero http.Server has a nil Handler, so it serves http.DefaultServeMux.
			if len(x.Values) == 0 && isServer(x.Type) {
				bad(x.Pos(), fmt.Sprintf("a %s.Server declared without a value has no Handler and serves http.DefaultServeMux; build it with a literal that sets Handler", x.Type.(*ast.SelectorExpr).X.(*ast.Ident).Name), docVet)
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
			if slices.Contains(listenFuncs[path], x.Sel.Name) && listens(pluginID, path+"."+x.Sel.Name) {
				r.violations = append(r.violations, listenViolation(fmt.Sprintf("%s:%d", rel, fset.Position(x.Pos()).Line), id.Name+"."+x.Sel.Name, pluginID))
			}
			if goFuncs[path] == x.Sel.Name {
				r.untracked(fset, f, rel, x.Pos(), fmt.Sprintf("%s.%s starts an untracked goroutine", id.Name, x.Sel.Name), boot)
			}
		case *ast.CallExpr:
			if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "new" && len(x.Args) == 1 && isServer(x.Args[0]) {
				bad(x.Pos(), "new(http.Server) has no Handler and serves http.DefaultServeMux; build it with a literal that sets Handler", docVet)
			}
			sel, ok := x.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if i, ok := serveHandler[pkgOf(sel.X)+"."+sel.Sel.Name]; ok && i < len(x.Args) && isNil(x.Args[i]) {
				bad(x.Pos(), fmt.Sprintf("%s.%s with a nil handler serves http.DefaultServeMux", sel.X.(*ast.Ident).Name, sel.Sel.Name), docVet)
			}
		case *ast.CompositeLit:
			// The rule is syntactic: a Server held in a struct field or embedded, a type
			// alias of it and a Handler set to a nil variable pass. At S3, serving may be
			// confined to the provider of the http key (ADR-059 decision 52).
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

// PluginVet reads the non-test Go files under each dir and prints each exemption to w.
// A dir whose base name is boot gets the allow-go relaxation for every goroutine
// starter. A file belongs to the plugin whose root holds it (manifestID).
func PluginVet(w io.Writer, dirs []string) error {
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
			r, err := vetFile(p, name, boot, manifestID(filepath.Dir(p), dir))
			res.violations = append(res.violations, r.violations...)
			res.exemptions = append(res.exemptions, r.exemptions...)
			d := filepath.Dir(p)
			if pkgs[d] == nil {
				pkgs[d] = map[string]string{}
			}
			pkgs[d][p] = name
			return err
		})
		if err != nil {
			return err
		}
		imp := &treeImporter{root: dir, pkgs: map[string]*types.Package{}, std: stdImporter}
		for d, files := range pkgs {
			r, err := vetMethods(files, imp, boot, manifestID(d, dir))
			if err != nil {
				return err
			}
			res.violations = append(res.violations, r.violations...)
			res.exemptions = append(res.exemptions, r.exemptions...)
		}
	}
	sort.Strings(res.exemptions)
	for _, e := range res.exemptions {
		fmt.Fprintln(w, "exempt", e)
	}
	fmt.Fprintf(w, "pluginvet: %d exemption(s)\n", len(res.exemptions))
	if len(res.violations) > 0 {
		sort.Strings(res.violations)
		return fmt.Errorf("plugin vet violations:\n%s", strings.Join(res.violations, "\n"))
	}
	return nil
}
