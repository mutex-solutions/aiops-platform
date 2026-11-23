package main

import (
	"go/ast"
	"go/parser"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The repository keeps the standing rule: every definition under sdk/def passes.
func TestRepository(t *testing.T) {
	problems, n, err := lint(filepath.Join("..", ".."), distributions())
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) > 0 || n < 8 {
		t.Fatalf("%d definitions, problems:\n%s", n, strings.Join(problems, "\n"))
	}
}

// fixture writes files under <tmp>/sdk/def/<name>/v0 and returns the definition.
func fixture(t *testing.T, name string, files map[string]string) *definition {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sdk", "def", name, "v0")
	for rel, src := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return &definition{name: name, dir: dir, rel: "sdk/def/" + name + "/v0"}
}

// want fails unless each of subs is in exactly one problem and no problem is left.
func want(t *testing.T, problems []string, subs ...string) {
	t.Helper()
	left := slices.Clone(problems)
	for _, s := range subs {
		i := slices.IndexFunc(left, func(p string) bool { return strings.Contains(p, s) })
		if i < 0 {
			t.Errorf("no problem says %q in %q", s, problems)
			continue
		}
		left = slices.Delete(left, i, i+1)
	}
	if len(left) > 0 {
		t.Errorf("other problems: %q", left)
	}
}

var complete = map[string]string{
	"definition.yaml":            "key: x\n",
	"x.go":                       "package x\n",
	"x_test.go":                  "package x_test\nfunc TestDefinitionFile() { deftest.Agree(t, d) }\n",
	"fake/fake.go":               "package fake\n",
	"conformance/conformance.go": "package conformance\nfunc Run(t *testing.T, m Mount) {}\n",
}

func TestFiles(t *testing.T) {
	want(t, files(fixture(t, "x", complete)))
	want(t, files(fixture(t, "x", nil)), "no definition.yaml", "no Go file", "no test calls deftest.Agree", "no fake", "no suite")
	noRun := map[string]string{}
	for k, v := range complete {
		noRun[k] = v
	}
	noRun["conformance/conformance.go"] = "package conformance\n"
	want(t, files(fixture(t, "x", noRun)), "the suite has no Run")
}

func TestLayout(t *testing.T) {
	const head = "key: x\nversion: \"0.2\"\nstability: preview\nclass: {provider: open, consumer: bound}\nremote: \"no\"\n"
	good := fixture(t, "x", map[string]string{"definition.yaml": head + "compatibleFrom: \"0.1\"\n" +
		"deprecated: {since: \"0.3.0\", removeNotBefore: 2027-11-01, successor: v1}\nsuite: 1\n" +
		"hooks:\n  x/y: {stages: [a, b], access: sealed, remote: \"no\"}\n"})
	want(t, layout(good))
	if !slices.Equal(good.keys, []string{"x"}) {
		t.Errorf("keys = %v", good.keys)
	}
	perKey := fixture(t, "x", map[string]string{"definition.yaml": "key: x\nversion: \"0.1\"\nstability: ga\n" +
		"class:\n  b: {provider: bound, consumer: signed}\n  a: {provider: open, consumer: open}\nremote: contrib\nsuite: 2\n"})
	want(t, layout(perKey))
	if !slices.Equal(perKey.keys, []string{"a", "b"}) {
		t.Errorf("keys = %v", perKey.keys)
	}
	for _, tc := range []struct{ src, problem string }{
		{"- a\n", "not one YAML mapping"},
		{strings.Replace(head, "key: x\n", "", 1) + "key: x\nsuite: 1\n", "out of order"},
		{head + "suite: 1\ncolor: red\n", "out of order"},
		{strings.Replace(head, "0.2", "1.2", 1) + "suite: 1\n", `is not "0.<minor>"`},
		{strings.Replace(head, "preview", "beta", 1) + "suite: 1\n", "stability"},
		{strings.Replace(head, `"no"`, "maybe", 1) + "suite: 1\n", "remote"},
		{strings.Replace(head, "provider: open", "provider: signed", 1) + "suite: 1\n", "class of x"},
		{strings.Replace(head, "key: x\n", "key: \"\"\n", 1) + "suite: 1\n", "no key"},
		{"key: y\nversion: \"0.1\"\nstability: ga\nclass:\n  a: {provider: open, consumer: open}\nremote: contrib\nsuite: 1\n", "it names the definition"},
		{head, "suite revision"},
		{head + "suite: 0\n", "suite revision"},
		{head + "compatibleFrom: \"0.3\"\nsuite: 1\n", "compatibleFrom"},
		{head + "deprecated: {since: \"0.3.0\", removeNotBefore: soon, successor: v1}\nsuite: 1\n", "deprecated"},
		{head + "suite: 1\nhooks:\n  h: {stages: [a, a], access: open, remote: \"no\"}\n", "hook h"},
		{head + "suite: 1\nhooks:\n  h: {stages: [a], access: closed, remote: \"no\"}\n", "hook h"},
	} {
		d := fixture(t, "x", map[string]string{"definition.yaml": tc.src})
		if p := layout(d); !slices.ContainsFunc(p, func(s string) bool { return strings.Contains(s, tc.problem) }) {
			t.Errorf("%q: problems %q, want one with %q", tc.src, p, tc.problem)
		}
	}
}

func TestImports(t *testing.T) {
	const kernel, testkit = `"` + modPath + `/sdk/kernel"`, `"` + modPath + `/sdk/testkit/kerneltest"`
	d := fixture(t, "x", map[string]string{
		"x.go":             "package x\nimport (\"context\"\n" + kernel + "\n\"" + modPath + "/sdk/def/y/v0\")\n",
		"x_test.go":        "package x\nimport " + testkit + "\n",
		"y.go":             "package x\nimport \"github.com/riverqueue/river\"\n",
		"fake/fake.go":     "package fake\nimport " + testkit + "\n",
		"conformance/c.go": "package conformance\nimport \"go.yaml.in/yaml/v3\"\n",
	})
	want(t, imports(d), "y.go imports github.com/riverqueue/river", "fake/fake.go imports "+modPath+"/sdk/testkit/kerneltest",
		"conformance/c.go imports go.yaml.in/yaml/v3")
}

func TestRoles(t *testing.T) {
	a := &definition{rel: "a", keys: []string{"ka"}}
	b := &definition{rel: "b", keys: []string{"kb", "kb2"}}
	c := &definition{rel: "c", keys: []string{"kc"}}
	dup := &definition{rel: "dup", keys: []string{"ka"}}
	plugins := []plugin{{id: "p1", provides: []string{"ka"}, consumes: []string{"kb2", "other"}}, {id: "p2", provides: []string{"kb"}}}
	want(t, roles([]*definition{a, b, c}, plugins), "a: no plugin", "c: no plugin", "c: no plugin")
	// The later of two definitions of one key owns it.
	want(t, roles([]*definition{a, b, dup}, plugins), "dup: key ka is declared by a too", "a: no plugin", "a: no plugin", "dup: no plugin")
}

const source = `package x

import (
	"context"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

var Key = kernel.NewKey[Registry]("xs", 0)

type Thing interface{ Do(ctx context.Context, s string) (string, error) }

type Registry interface {
	Register(c *kernel.Context, name string, t Thing) error
	Do(ctx context.Context, name, s string) (string, error)
	Inventory() []string
}

var Plain = kernel.NewKey[Service]("plain", 0)

type Service interface {
	Now() int
	After(c *kernel.Context, d int) error
}
`

const bad = `
var (
	Concrete = kernel.NewKey[*impl]("c", 0)
	Foreign  = kernel.NewKey[context.Context]("f", 0)
	BadKey   = kernel.NewKey[Bad]("bad", 0)
)

type Bad interface {
	Register(c *kernel.Context, name string, t Loose) error
	RegisterLate(name string) error
	Late(ctx context.Context, s string, c *kernel.Context) error
	Call(name string) error
}

type Loose interface{ Do(s string) error }
`

const providers = `
type impl struct{}

func (impl) Do(ctx context.Context, s string) (string, error) { return s, nil }
func (impl) Extra()                                         {}

type clean struct{}

func (clean) Do(ctx context.Context, s string) (string, error) { return s, nil }

type reg struct{}

func (reg) Register(*kernel.Context, string, Thing) error            { return nil }
func (reg) Do(context.Context, string, string) (string, error)     { return "", nil }
func (reg) Inventory() []string                                    { return nil }
func (reg) Debug()                                                 {}

func provide(c *kernel.Context, r Registry) error {
	_ = Key.Provide(c, reg{})
	_ = Key.Provide(c, r)
	_ = r.Register(c, "a", impl{})
	return r.Register(c, "b", clean{})
}
`

// typed type-checks src as the package sdk/def/x/v0.
func typed(t *testing.T, src string) func() []string {
	t.Helper()
	pkgs, err := list(filepath.Join("..", ".."), kernelPath, "context")
	if err != nil {
		t.Fatal(err)
	}
	c := newChecker(pkgs)
	f, err := parser.ParseFile(c.fset, filepath.Join(t.TempDir(), "x.go"), src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	pkg, info, err := c.check(modPath+"/sdk/def/x/v0", []*ast.File{f})
	if err != nil {
		t.Fatal(err)
	}
	return func() []string {
		return append(shapes(pkg), sideDoors(c.fset, filepath.Dir(c.fset.Position(f.Pos()).Filename), info, []*ast.File{f})...)
	}
}

func TestShapes(t *testing.T) {
	problems := typed(t, source)
	want(t, problems())
	problems = typed(t, source+bad+"type impl struct{}\n")
	want(t, problems(), "key Concrete serves", "key Foreign serves", "Bad.RegisterLate registers without",
		"Bad.Late takes a *kernel.Context after", "Bad.Call does not take the caller's context first",
		"registry Bad has no Inventory()", "contribution Loose.Do does not take")
}

// The side-door check judges every package of the root module and of the SDK module
// outside tools/, and lists the fake and the suite of each definition, so that what a
// fake gives to Provide is judged too.
func TestSideDoorScope(t *testing.T) {
	got := patterns([]*definition{{rel: "sdk/def/x/v0"}})
	for _, p := range []string{"./dist", "./test/dist", "./examples", modPath + "/sdk/def/x/v0", modPath + "/sdk/def/x/v0/fake", modPath + "/sdk/def/x/v0/conformance"} {
		if !slices.Contains(got, p) {
			t.Errorf("patterns = %q, want %s among them", got, p)
		}
	}
	mod := func(path string) *struct{ Path string } { return &struct{ Path string }{path} }
	for _, tc := range []struct {
		p    listed
		want bool
	}{
		{listed{ImportPath: modPath + "/examples/a", Module: mod(modPath)}, true},
		{listed{ImportPath: modPath + "/sdk/def/x/v0/fake", Module: mod(modPath + "/sdk")}, true},
		{listed{ImportPath: modPath + "/sdk/plugin", Module: mod(modPath + "/sdk")}, true},
		{listed{ImportPath: modPath + "/tools/x", Module: mod(modPath)}, false},
		{listed{ImportPath: modPath + "/sdk/kernel", Module: mod(modPath + "/sdk/kernel")}, false},
		{listed{ImportPath: "go.yaml.in/yaml/v3", Module: mod("go.yaml.in/yaml/v3")}, false},
		{listed{ImportPath: "fmt"}, false},
	} {
		if got := judged(&tc.p); got != tc.want {
			t.Errorf("judged(%s) = %v, want %v", tc.p.ImportPath, got, tc.want)
		}
	}
}

func TestSideDoors(t *testing.T) {
	problems := typed(t, source+providers)
	want(t, problems(), "Provide has methods beyond "+modPath+"/sdk/def/x/v0.Registry: Debug",
		"Register has methods beyond "+modPath+"/sdk/def/x/v0.Thing: Extra")
}
