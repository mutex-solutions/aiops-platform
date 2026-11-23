// Command pluginapi is the pluginApi check of ADR-060 decision 8. It lists the
// exported API of sdk/kernel and sdk/plugin, compares the list with the copy
// recorded in api.txt beside it, and fails when the API changed and
// plugin.APIVersion did not. With -write it records the current API, and refuses
// to when the API changed under the recorded version or the version went down.
//
// The list is built from the packages' export data with go/types: each exported
// name with its type, and each exported method and field of an exported type.
// Parameter names, unexported fields and comments are not in it, so changing them
// needs no new pluginApi.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/constant"
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	modPath    = "github.com/mutex-solutions/aiops-platform"
	recordFile = "tools/pluginapi/api.txt"
)

// packages are the author-facing surface that pluginApi versions; the version is
// the constant APIVersion of the second.
var packages = []string{modPath + "/sdk/kernel", modPath + "/sdk/plugin"}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "pluginapi:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	write := len(args) == 2 && args[0] == "-write"
	if !write && len(args) != 1 {
		return errors.New("usage: pluginapi [-write] <repository root>")
	}
	root := args[len(args)-1]
	pkgs, err := load(root, packages...)
	if err != nil {
		return err
	}
	c, ok := pkgs[packages[1]].Scope().Lookup("APIVersion").(*types.Const)
	if !ok || c.Val().Kind() != constant.String {
		return errors.New("sdk/plugin declares no string constant APIVersion")
	}
	cur := record{version: constant.StringVal(c.Val())}
	for _, p := range packages {
		cur.lines = append(cur.lines, list(pkgs[p])...)
	}
	file := filepath.Join(root, recordFile)
	var old record
	data, err := os.ReadFile(file)
	switch {
	case err == nil:
		if old, err = parse(data); err != nil {
			return fmt.Errorf("%s: %w", recordFile, err)
		}
	case !write || !errors.Is(err, fs.ErrNotExist):
		return err
	}
	if err := decide(old, cur, write); err != nil {
		return err
	}
	if write {
		return os.WriteFile(file, cur.format(), 0o644)
	}
	fmt.Printf("pluginapi: the exported API of sdk/kernel and sdk/plugin is the one recorded at pluginApi %s\n", cur.version)
	return nil
}

// record is a recorded API: the version and the sorted lines of the list.
type record struct {
	version string
	lines   []string
}

const header = `# The exported API of sdk/kernel and sdk/plugin (ADR-060 decision 8), written by
# go run ./tools/pluginapi -write .; raise plugin.APIVersion before recording a change.
`

func (r record) format() []byte {
	return []byte(header + "pluginApi " + r.version + "\n" + strings.Join(r.lines, "\n") + "\n")
}

func parse(data []byte) (record, error) {
	var r record
	for _, l := range strings.Split(string(data), "\n") {
		switch v, isVersion := strings.CutPrefix(l, "pluginApi "); {
		case l == "" || strings.HasPrefix(l, "#"):
		case r.version == "" && isVersion && v != "":
			r.version = v
		case r.version == "":
			return r, errors.New("the first line is not pluginApi <version>")
		default:
			r.lines = append(r.lines, l)
		}
	}
	if r.version == "" {
		return r, errors.New("no pluginApi line")
	}
	return r, nil
}

// decide applies the rule: the current API may differ from the recorded one only
// under a raised version, and a check passes only when both equal the record.
func decide(old, cur record, write bool) error {
	if _, err := versionOf(cur.version); err != nil {
		return err
	}
	same := slices.Equal(old.lines, cur.lines)
	switch {
	case old.version == cur.version && same:
		return nil
	case old.version == cur.version:
		return fmt.Errorf("the exported API of sdk/kernel or sdk/plugin changed and pluginApi did not (still %s): raise plugin.APIVersion, then record the API with -write\n%s",
			cur.version, diff(old.lines, cur.lines))
	case !write:
		return fmt.Errorf("pluginApi is %s and the record is at %s: record the API with -write", cur.version, old.version)
	case old.version != "":
		o, _ := versionOf(old.version)
		if c, _ := versionOf(cur.version); slices.Compare(c, o) <= 0 {
			return fmt.Errorf("pluginApi %s is not above the recorded %s", cur.version, old.version)
		}
	}
	return nil
}

// versionOf reads major.minor, each a number with no leading zero.
func versionOf(v string) ([]int, error) {
	var out []int
	for _, s := range strings.Split(v, ".") {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 || strconv.Itoa(n) != s {
			return nil, fmt.Errorf("pluginApi %q is not major.minor", v)
		}
		out = append(out, n)
	}
	if len(out) != 2 {
		return nil, fmt.Errorf("pluginApi %q is not major.minor", v)
	}
	return out, nil
}

// diff lists the lines that went and came, of two sorted lists.
func diff(old, cur []string) string {
	var b strings.Builder
	for _, l := range old {
		if !slices.Contains(cur, l) {
			b.WriteString("- " + l + "\n")
		}
	}
	for _, l := range cur {
		if !slices.Contains(old, l) {
			b.WriteString("+ " + l + "\n")
		}
	}
	return b.String()
}

// load type-checks nothing: it builds the packages with go list -export, run in
// root, and imports each from its export data.
func load(root string, paths ...string) (map[string]*types.Package, error) {
	cmd := exec.Command("go", append([]string{"list", "-export", "-deps", "-f", "{{.ImportPath}}\t{{.Export}}"}, paths...)...)
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list -export: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	exports := map[string]string{}
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		path, file, _ := strings.Cut(l, "\t")
		exports[path] = file
	}
	imp := importer.ForCompiler(token.NewFileSet(), "gc", func(path string) (io.ReadCloser, error) { return os.Open(exports[path]) })
	pkgs := map[string]*types.Package{}
	for _, p := range paths {
		if pkgs[p], err = imp.Import(p); err != nil {
			return nil, err
		}
	}
	return pkgs, nil
}

// lister writes the list of one package. An unexported type that the exported
// API names, such as the service type of an exported key, is listed too, since an
// author reaches its exported fields and methods; one with an exported alias is
// listed and named by the alias.
type lister struct {
	pkg   *types.Package
	lines []string
	alias map[*types.TypeName]string // Unexported type -> its exported alias.
	seen  map[*types.TypeName]bool   // Unexported types listed or queued.
	queue []*types.TypeName
}

func list(pkg *types.Package) []string {
	l := &lister{pkg: pkg, alias: map[*types.TypeName]string{}, seen: map[*types.TypeName]bool{}}
	for _, name := range pkg.Scope().Names() {
		if o, ok := pkg.Scope().Lookup(name).(*types.TypeName); ok && o.Exported() && o.IsAlias() {
			if n, ok := types.Unalias(o.Type()).(*types.Named); ok && n.Obj().Pkg() == pkg && !n.Obj().Exported() {
				l.alias[n.Obj()], l.seen[n.Obj()] = name, true
			}
		}
	}
	for _, name := range pkg.Scope().Names() {
		switch o := pkg.Scope().Lookup(name).(type) {
		case *types.Const:
			if o.Exported() {
				l.add("const %s %s = %s", name, l.typ(o.Type()), o.Val().ExactString())
			}
		case *types.Var:
			if o.Exported() {
				l.add("var %s %s", name, l.typ(o.Type()))
			}
		case *types.Func:
			if o.Exported() {
				l.add("func %s%s", name, l.sig(o.Type().(*types.Signature), true))
			}
		case *types.TypeName:
			if o.Exported() {
				l.typeName(o)
			}
		}
	}
	for len(l.queue) > 0 {
		o := l.queue[0]
		l.queue = l.queue[1:]
		l.typeName(o)
	}
	slices.Sort(l.lines)
	return slices.Compact(l.lines)
}

func (l *lister) add(format string, a ...any) {
	l.lines = append(l.lines, "pkg "+l.pkg.Name()+", "+fmt.Sprintf(format, a...))
}

// typeName lists a type: an alias of a type of another package or of an exported
// type as the alias alone, any other type with its exported fields or methods and
// its method set, under its exported name.
func (l *lister) typeName(o *types.TypeName) {
	name := o.Name()
	t := types.Unalias(o.Type())
	n, named := t.(*types.Named)
	if o.IsAlias() && (!named || n.Obj().Exported() || n.Obj().Pkg() != l.pkg) {
		l.add("type %s = %s", name, l.typ(t))
		return
	}
	recv := name
	if named && n.TypeParams().Len() > 0 {
		var ps, ns []string
		for i := range n.TypeParams().Len() {
			p := n.TypeParams().At(i)
			ps = append(ps, p.Obj().Name()+" "+l.typ(p.Constraint()))
			ns = append(ns, p.Obj().Name())
		}
		name += "[" + strings.Join(ps, ", ") + "]"
		recv += "[" + strings.Join(ns, ", ") + "]"
	}
	switch u := t.Underlying().(type) {
	case *types.Struct:
		l.add("type %s struct", name)
		for i := range u.NumFields() {
			if f := u.Field(i); f.Exported() && f.Embedded() {
				l.add("type %s struct, %s", name, l.typ(f.Type()))
			} else if f.Exported() {
				l.add("type %s struct, %s %s", name, f.Name(), l.typ(f.Type()))
			}
		}
	case *types.Interface:
		l.add("type %s interface", name)
		for i := range u.NumMethods() {
			if m := u.Method(i); m.Exported() {
				l.add("type %s interface, %s%s", name, m.Name(), l.sig(m.Type().(*types.Signature), false))
			} else {
				l.add("type %s interface, unexported methods", name)
			}
		}
		for i := range u.NumEmbeddeds() {
			if e, isUnion := u.EmbeddedType(i).(*types.Union); isUnion {
				l.add("type %s interface, %s", name, l.typ(e))
			}
		}
		return
	default:
		l.add("type %s %s", name, l.typ(u))
	}
	value := types.NewMethodSet(t)
	ptr := types.NewMethodSet(types.NewPointer(t))
	for i := range ptr.Len() {
		m := ptr.At(i).Obj().(*types.Func)
		if !m.Exported() {
			continue
		}
		r := "*" + recv
		if value.Lookup(m.Pkg(), m.Name()) != nil {
			r = recv
		}
		l.add("method (%s) %s%s", r, m.Name(), l.sig(m.Type().(*types.Signature), false))
	}
}

// sig writes a signature with the types of its parameters and results only.
func (l *lister) sig(s *types.Signature, tparams bool) string {
	var b strings.Builder
	if tp := s.TypeParams(); tparams && tp.Len() > 0 {
		var ps []string
		for i := range tp.Len() {
			ps = append(ps, tp.At(i).Obj().Name()+" "+l.typ(tp.At(i).Constraint()))
		}
		b.WriteString("[" + strings.Join(ps, ", ") + "]")
	}
	var ps []string
	for i := range s.Params().Len() {
		t := s.Params().At(i).Type()
		if s.Variadic() && i == s.Params().Len()-1 {
			ps = append(ps, "..."+l.typ(t.(*types.Slice).Elem()))
		} else {
			ps = append(ps, l.typ(t))
		}
	}
	b.WriteString("(" + strings.Join(ps, ", ") + ")")
	switch r := s.Results(); r.Len() {
	case 0:
	case 1:
		b.WriteString(" " + l.typ(r.At(0).Type()))
	default:
		var rs []string
		for i := range r.Len() {
			rs = append(rs, l.typ(r.At(i).Type()))
		}
		b.WriteString(" (" + strings.Join(rs, ", ") + ")")
	}
	return b.String()
}

// typ writes a type as Go source does, naming another package by its name and
// leaving out the parameter names of every function type within it.
func (l *lister) typ(t types.Type) string {
	switch t := t.(type) {
	case *types.Named:
		return l.qualified(t.Obj(), t.TypeArgs())
	case *types.Alias:
		return l.qualified(t.Obj(), t.TypeArgs())
	case *types.Pointer:
		return "*" + l.typ(t.Elem())
	case *types.Slice:
		return "[]" + l.typ(t.Elem())
	case *types.Array:
		return fmt.Sprintf("[%d]%s", t.Len(), l.typ(t.Elem()))
	case *types.Map:
		return "map[" + l.typ(t.Key()) + "]" + l.typ(t.Elem())
	case *types.Chan:
		return map[types.ChanDir]string{types.SendRecv: "chan ", types.SendOnly: "chan<- ", types.RecvOnly: "<-chan "}[t.Dir()] + l.typ(t.Elem())
	case *types.Signature:
		return "func" + l.sig(t, false)
	case *types.Struct:
		var fs []string
		for i := range t.NumFields() {
			if f := t.Field(i); f.Embedded() {
				fs = append(fs, l.typ(f.Type()))
			} else {
				fs = append(fs, f.Name()+" "+l.typ(f.Type()))
			}
		}
		return "struct{" + strings.Join(fs, "; ") + "}"
	case *types.Interface:
		var ms []string
		for i := range t.NumMethods() {
			ms = append(ms, t.Method(i).Name()+l.sig(t.Method(i).Type().(*types.Signature), false))
		}
		for i := range t.NumEmbeddeds() {
			ms = append(ms, l.typ(t.EmbeddedType(i)))
		}
		return "interface{" + strings.Join(ms, "; ") + "}"
	case *types.Union:
		var ts []string
		for i := range t.Len() {
			tilde := ""
			if t.Term(i).Tilde() {
				tilde = "~"
			}
			ts = append(ts, tilde+l.typ(t.Term(i).Type()))
		}
		return strings.Join(ts, " | ")
	}
	return types.TypeString(t, nil) // a basic type or a type parameter
}

func (l *lister) qualified(o *types.TypeName, args *types.TypeList) string {
	s := o.Name()
	switch {
	case o.Pkg() != nil && o.Pkg() != l.pkg:
		s = o.Pkg().Name() + "." + s
	case l.alias[o] != "":
		s = l.alias[o]
	case o.Pkg() == l.pkg && !o.Exported() && !l.seen[o]:
		l.seen[o] = true
		l.queue = append(l.queue, o)
	}
	if args.Len() > 0 {
		var as []string
		for i := range args.Len() {
			as = append(as, l.typ(args.At(i)))
		}
		s += "[" + strings.Join(as, ", ") + "]"
	}
	return s
}
