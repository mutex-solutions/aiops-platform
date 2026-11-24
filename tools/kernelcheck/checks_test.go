package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

const mp = modPath

func imp(paths ...string) string {
	s := "package p\n\nimport (\n"
	for _, p := range paths {
		s += "\t_ \"" + p + "\"\n"
	}
	return s + ")\n"
}

func noList(string, string) ([]string, error) { return nil, nil }

// testRegister holds what U2 reads of the upstream register (ADR-054 decision 13): the
// YAML library with its importers, pgx and River for the S0 probes, an upstream that a
// plugin wraps, and two entries where one module path holds the other.
var testRegister = []upstream{
	{ID: "go-yaml", Modules: []string{"go.yaml.in/yaml/v3"}, Importers: dirs("boot", "tools", "internal/plugingen")},
	{ID: "river", Modules: []string{"github.com/riverqueue/river", "github.com/riverqueue/river/rivertype"}, Importers: dirs("plugins/jobs-river")},
	{ID: "pgx", Modules: []string{"github.com/jackc/pgx/v5"}, Importers: dirs("plugins/jobs-river")},
	{ID: "wrapped", Modules: []string{"example.org/wrapped"}, Plugins: []string{"aiops/wrapper"}},
	{ID: "a", Modules: []string{"example.org/a"}, Importers: dirs("internal/serve")},
	{ID: "b", Modules: []string{"example.org/a/b"}, Importers: dirs("tools")},
}

func dirs(d ...string) []importDir {
	var out []importDir
	for _, x := range d {
		out = append(out, importDir{Dir: x})
	}
	return out
}

func TestImports(t *testing.T) {
	for _, tc := range []struct {
		name, file, src string
		want            string // substring of the report; empty means pass
	}{
		{"kernel std", "sdk/kernel/a.go", imp("fmt", "log/slog"), ""},
		{"kernel third party", "sdk/kernel/a.go", imp("example.com/x"), "sdk/kernel imports the standard library only"},
		{"kernel test third party", "sdk/kernel/a_test.go", imp(mp + "/sdk/testkit/kerneltest"), "sdk/kernel imports the standard library only"},
		{"def kernel", "sdk/def/clock/v0/a.go", imp(mp+"/sdk/kernel", mp+"/sdk/def/clock/v0"), ""},
		{"def third party", "sdk/def/clock/v0/a.go", imp("github.com/jackc/pgx/v5"), "sdk/def imports"},
		{"def test testkit", "sdk/def/clock/v0/a_test.go", imp(mp + "/sdk/testkit/kerneltest"), ""},
		{"def non-test testkit", "sdk/def/clock/v0/a.go", imp(mp + "/sdk/testkit/kerneltest"), "sdk/def imports"},
		{"boot allowed", "boot/a.go", imp("os", "go.yaml.in/yaml/v3", mp+"/sdk/kernel", mp+"/sdk/def/launch/v0", mp+"/sdk/def/loader/v0", mp+"/sdk/def/composition/v0"), ""},
		{"boot other def", "boot/a.go", imp(mp + "/sdk/def/clock/v0"), "boot imports"},
		{"boot plugin", "boot/a.go", imp(mp + "/plugins/clock"), "boot imports"},
		{"boot loader", "boot/a.go", imp(mp + "/loader"), "boot imports"},
		{"boot cmd", "boot/a.go", imp(mp + "/cmd/aiopsctl"), "boot imports"},
		{"boot test dir", "boot/a.go", imp(mp + "/test/x"), "boot imports"},
		{"boot dist", "boot/a.go", imp(mp + "/dist"), "boot imports"},
		{"boot third party", "boot/a.go", imp("github.com/jackc/pgx/v5"), "U2: imported outside the plugins and importers of its register entry pgx"},
		{"boot testdata skipped", "boot/testdata/a.go", imp(mp + "/plugins/clock"), ""},
		{"loader allowed", "loader/a.go", imp("os", mp+"/sdk/kernel", mp+"/sdk/def/launch/v0", mp+"/sdk/def/loader/v0", mp+"/sdk/def/composition/v0"), ""},
		{"loader other def", "loader/a.go", imp(mp + "/sdk/def/reload/v0"), "loader imports"},
		{"loader test other def", "loader/a_test.go", imp(mp + "/sdk/def/clock/v0/fake"), "loader imports"},
		{"loader yaml", "loader/a.go", imp("go.yaml.in/yaml/v3"), "loader imports"},
		{"loader plugin", "loader/a.go", imp(mp + "/plugins/clock"), "loader imports"},
		{"loader test fake", "loader/a_test.go", imp(mp + "/sdk/def/loader/v0/fake"), ""},
		{"loader non-test fake", "loader/a.go", imp(mp + "/sdk/def/clock/v0/fake"), "no fake or sdk/testkit"},
		{"plugin ok", "plugins/a/plugin.go", imp(mp+"/sdk/kernel", mp+"/sdk/def/clock/v0", mp+"/plugins/a/internal/x"), ""},
		{"plugin imports sdk/plugin", "plugins/a/plugin.go", imp(mp+"/sdk/plugin", mp+"/sdk/kernel"), ""},
		{"sdk/plugin allowed", "sdk/plugin/a.go", imp("fmt", mp+"/sdk/kernel", mp+"/sdk/def/launch/v0"), ""},
		{"sdk/plugin third party", "sdk/plugin/a.go", imp("example.com/x"), "sdk/plugin imports the standard library, sdk/kernel and sdk/def only"},
		{"sdk/plugin test testkit", "sdk/plugin/a_test.go", imp(mp + "/sdk/testkit/kerneltest"), ""},
		{"sdk/plugin external test", "sdk/plugin/a_test.go", imp(mp+"/sdk/plugin", mp+"/sdk/kernel"), ""},
		{"sdk/plugin non-test testkit", "sdk/plugin/a.go", imp(mp + "/sdk/testkit/kerneltest"), "sdk/plugin imports"},
		{"sdk/plugin yaml", "sdk/plugin/a.go", imp("go.yaml.in/yaml/v3"), "sdk/plugin imports"},
		{"def imports sdk/plugin", "sdk/def/clock/v0/a.go", imp(mp + "/sdk/plugin"), "sdk/def imports"},
		{"boot imports sdk/plugin", "boot/a.go", imp(mp + "/sdk/plugin"), "boot imports"},
		{"loader imports sdk/plugin", "loader/a.go", imp(mp + "/sdk/plugin"), "loader imports"},
		{"plugin third party", "plugins/a/internal/x/x.go", imp("github.com/jackc/pgx/v5"), "U2: imported outside the plugins and importers of its register entry pgx"},
		{"importer of pgx and River", "plugins/jobs-river/internal/s0/x.go", imp("github.com/jackc/pgx/v5", "github.com/riverqueue/river/rivertype"), ""},
		{"an importer is a directory", "plugins/a/internal/x/x.go", imp("github.com/riverqueue/river"), "U2: imported outside the plugins and importers of its register entry river"},
		{"the plugin of an entry", "plugins/wrapper/internal/x/x.go", imp("example.org/wrapped/client"), ""},
		{"another plugin of no entry", "plugins/other/internal/x/x.go", imp("example.org/wrapped/client"), "U2: imported outside the plugins and importers of its register entry wrapped"},
		{"plugin other plugin", "plugins/a/plugin.go", imp(mp + "/plugins/b"), "a plugin imports"},
		{"plugin boot", "plugins/a/plugin.go", imp(mp + "/boot"), "a plugin imports"},
		{"plugin loader", "plugins/a/plugin.go", imp(mp + "/loader"), "a plugin imports"},
		{"plugin cmd", "plugins/a/plugin.go", imp(mp + "/cmd/x"), "a plugin imports"},
		{"plugin yaml", "plugins/a/plugin.go", imp("go.yaml.in/yaml/v3"), "U2: imported outside the plugins and importers of its register entry go-yaml"},
		{"plugin test testkit", "plugins/a/plugin_test.go", imp(mp + "/sdk/testkit/kerneltest"), ""},
		{"plugin non-test testkit", "plugins/a/plugin.go", imp(mp + "/sdk/testkit/kerneltest"), "no fake or sdk/testkit"},
		{"plugin exported package", "plugins/a/api/api.go", imp("fmt"), "under internal/"},
		{"plugin extra root file", "plugins/a/other.go", imp("fmt"), "under internal/"},
		{"plugin test at root", "plugins/a/zz_cycle_test.go", imp("fmt"), ""},
		{"example sdk", "examples/a/plugin.go", imp("fmt", mp+"/sdk/kernel", mp+"/sdk/def/clock/v0", mp+"/sdk/plugin", mp+"/examples/a/x"), ""},
		{"example internal", "examples/a/plugin.go", imp(mp + "/internal/runfile"), "an example imports"},
		{"example other example", "examples/a/plugin.go", imp(mp + "/examples/b"), "an example imports"},
		{"example plugin", "examples/a/plugin.go", imp(mp + "/plugins/clock"), "an example imports"},
		{"example boot", "examples/a/plugin.go", imp(mp + "/boot"), "an example imports"},
		{"example third party", "examples/a/plugin.go", imp("github.com/jackc/pgx/v5"), "an example imports"},
		{"example test", "examples/a/plugin_test.go", imp(mp+"/sdk/testkit/kerneltest", mp+"/sdk/def/clock/v0/fake", mp+"/examples/b"), ""},
		{"example test internal", "examples/a/plugin_test.go", imp(mp + "/internal/runfile"), "an example imports"},
		{"example list", "examples/examples.go", imp(mp+"/examples/a", mp+"/sdk/kernel"), ""},
		{"example list internal", "examples/examples.go", imp(mp + "/internal/runfile"), "an example imports"},
		{"yaml in tools", "tools/x/x.go", imp("go.yaml.in/yaml/v3"), ""},
		{"module of no entry", "test/x/x_test.go", imp("gopkg.in/yaml.v3"), "U2: its module is in no entry's modules"},
		{"yaml in plugingen", "internal/plugingen/x.go", imp("go.yaml.in/yaml/v3"), ""},
		{"loader test of its own package", "loader/zz_cycle_test.go", imp(mp+"/loader", mp+"/sdk/testkit/cycle", mp+"/sdk/def/launch/v0/fake"), ""},
		{"loader source of its own package", "loader/a.go", imp(mp + "/loader"), "loader imports"},
		{"yaml in another internal package", "internal/serve/x.go", imp("go.yaml.in/yaml/v3"), "U2: imported outside the plugins and importers of its register entry go-yaml"},
		{"module of no entry in a command", "cmd/aiopsctl/x.go", imp("example.org/unlisted"), "U2: its module is in no entry's modules"},
		{"the longest module path names the entry", "tools/x/x.go", imp("example.org/a/b/c"), ""},
		{"the longest module path names the entry elsewhere", "internal/serve/x.go", imp("example.org/a/b/c"), "U2: imported outside the plugins and importers of its register entry b"},
		{"a shorter module path", "internal/serve/x.go", imp("example.org/a/c"), ""},
		{"fake in test tree", "test/x/x.go", imp(mp + "/sdk/def/clock/v0/fake"), ""},
		{"fake in aiopsctl", "cmd/aiopsctl/x.go", imp(mp + "/sdk/def/clock/v0/fake"), ""},
		{"fake in cmd/aiops", "cmd/aiops/x.go", imp(mp + "/sdk/def/clock/v0/fake"), "no fake or sdk/testkit"},
		{"testkit in dist", "dist/x.go", imp(mp + "/sdk/testkit/kerneltest"), "no fake or sdk/testkit"},
		{"commands in aiopsctl", "cmd/aiopsctl/x.go", imp(mp + "/sdk/def/commands/v0"), "no command code in a main"},
		{"operations in aiops", "cmd/aiops/x.go", imp(mp + "/sdk/def/operations/v0"), "no command code in a main"},
		{"commands fake in an aiopsctl test", "cmd/aiopsctl/x_test.go", imp(mp + "/sdk/def/commands/v0/fake"), "no command code in a main"},
		{"launch in aiopsctl", "cmd/aiopsctl/x.go", imp(mp + "/sdk/def/launch/v0"), ""},
		{"commands in a command row", "plugins/command-x/plugin.go", imp(mp + "/sdk/def/commands/v0"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := t.TempDir()
			write(t, d, tc.file, tc.src)
			err := checkImports(d, testRegister, noList)
			switch {
			case tc.want == "" && err != nil:
				t.Fatal(err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("want %q, got %v", tc.want, err)
			}
			if err != nil && !strings.Contains(err.Error(), tc.file+":") {
				t.Errorf("report lacks file:line: %v", err)
			}
		})
	}
}

func TestImportsReportFormatAndOrder(t *testing.T) {
	d := t.TempDir()
	write(t, d, "plugins/b/plugin.go", imp(mp+"/plugins/a", "fmt"))
	write(t, d, "boot/a.go", imp("fmt", mp+"/loader"))
	err := checkImports(d, testRegister, noList)
	if err == nil {
		t.Fatal("no violation")
	}
	lines := strings.Split(err.Error(), "\n")[1:]
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "boot/a.go:5: imports "+mp+"/loader: boot imports") || !strings.HasPrefix(lines[1], "plugins/b/plugin.go:4: ") || !strings.Contains(lines[0], "(07-c4-plugin-sdk.md:") {
		t.Fatalf("unexpected report:\n%s", err)
	}
}

func TestImportsServerClosure(t *testing.T) {
	d := t.TempDir()
	write(t, d, "cmd/aiops/main.go", "package main\n")
	ok := func(string, string) ([]string, error) { return []string{"fmt", mp + "/boot"}, nil }
	if err := checkImports(d, testRegister, ok); err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{mp + "/sdk/def/clock/v0/fake", mp + "/sdk/testkit/kerneltest"} {
		leaky := func(string, string) ([]string, error) { return []string{"fmt", leak}, nil }
		if err := checkImports(d, testRegister, leaky); err == nil || !strings.Contains(err.Error(), "cmd/aiops: dependency closure contains "+leak) {
			t.Errorf("leak %s: %v", leak, err)
		}
	}
	failing := func(string, string) ([]string, error) { return nil, errors.New("boom") }
	if err := checkImports(d, testRegister, failing); err == nil {
		t.Error("list failure ignored")
	}
}

// The link guard of ADR-060 decision 28 (ADR-059 decision 9): no package under test/ or
// examples/ is in the closure of cmd/aiops or cmd/aiopsctl. aiopsctl carries the fakes
// by design, so the fake rule is cmd/aiops's alone.
func TestImportsServerClosureLinksNoTestOrExample(t *testing.T) {
	d := t.TempDir()
	write(t, d, "cmd/aiops/main.go", "package main\n")
	write(t, d, "cmd/aiopsctl/main.go", "package main\n")
	closure := func(deps map[string][]string) listDeps {
		return func(_, pkg string) ([]string, error) { return deps[pkg], nil }
	}
	fake := mp + "/sdk/def/clock/v0/fake"
	if err := checkImports(d, testRegister, closure(map[string][]string{"./cmd/aiopsctl": {"fmt", fake}})); err != nil {
		t.Fatalf("a fake in aiopsctl: %v", err)
	}
	for _, bin := range []string{"aiops", "aiopsctl"} {
		for _, leak := range []string{mp + "/test/dist", mp + "/test/trusted", mp + "/examples/echoer"} {
			err := checkImports(d, testRegister, closure(map[string][]string{"./cmd/" + bin: {"fmt", leak}}))
			if err == nil || !strings.Contains(err.Error(), "cmd/"+bin+": dependency closure contains "+leak+": no package under test/ or examples/") {
				t.Errorf("%s links %s: %v", bin, leak, err)
			}
		}
	}
	// A path that only starts like one of the two directories is neither.
	if err := checkImports(d, testRegister, closure(map[string][]string{"./cmd/aiops": {mp + "/testkitx", mp + "/examplesx"}})); err != nil {
		t.Errorf("a look-alike path: %v", err)
	}
}

func TestImportsParseError(t *testing.T) {
	d := t.TempDir()
	write(t, d, "boot/a.go", "package")
	if checkImports(d, testRegister, noList) == nil {
		t.Fatal("parse error ignored")
	}
}

func TestGoListDeps(t *testing.T) {
	d := t.TempDir()
	write(t, d, "go.mod", "module m\n\ngo 1.26.0\n")
	write(t, d, "a/a.go", "package a\n\nimport _ \"fmt\"\n")
	deps, err := goListDeps(d, "./a")
	if err != nil || len(deps) == 0 {
		t.Fatalf("%v %v", deps, err)
	}
	if _, err := goListDeps(d, "./missing"); err == nil {
		t.Fatal("missing package accepted")
	}
}

func TestPluginVet(t *testing.T) {
	const kimp = "import \"" + mp + "/sdk/kernel\"\n"
	const pimp = "import \"" + mp + "/sdk/plugin\"\n"
	pk := func(imports, body string) string { return "package p\n" + imports + body }
	for _, tc := range []struct {
		name, file, src string
		want            string
	}{
		{"clean plugin", "plugins/a/plugin.go", pk(kimp+"import (\"errors\"; \"fmt\")\n", "var Plugin = kernel.Define(kernel.Plugin{})\nvar ErrX = errors.New(\"x\")\nvar ErrY = fmt.Errorf(\"y\")\nvar _ I = (*T)(nil)\nconst c = 1\ntype I interface{}\ntype T struct{}\nvar key = kernel.NewKey[int](\"k\")\n"), ""},
		{"def constructor", "plugins/a/plugin.go", pk("import def \""+mp+"/sdk/def/clock/v0\"\n", "var h = def.NewHook()\n"), ""},
		{"plugin.Define", "plugins/a/plugin.go", pk(pimp, "var Plugin = plugin.Define(plugin.Spec[Config]{ID: \"a/b\"})\ntype Config struct{}\n"), ""},
		{"plugin.Define instantiated", "plugins/a/plugin.go", pk(pimp, "var Plugin = plugin.Define[Config](plugin.Spec[Config]{})\ntype Config struct{}\n"), ""},
		{"plugin.Define renamed import", "plugins/a/plugin.go", pk("import sdk \""+mp+"/sdk/plugin\"\n", "var Plugin = sdk.Define(sdk.Spec[struct{}]{})\n"), ""},
		{"other sdk/plugin call", "plugins/a/plugin.go", pk(pimp, "var info, _ = plugin.Describe[struct{}]()\n"), "package-level var info is mutable state"},
		{"standard library plugin package", "plugins/a/plugin.go", pk("import \"plugin\"\n", "var p, _ = plugin.Open(\"x\")\n"), "package-level var p is mutable state"},
		{"standard library plugin.Define is not ours", "plugins/a/plugin.go", pk("import \"plugin\"\n", "var p = plugin.Define()\n"), "package-level var p is mutable state"},
		{"init", "plugins/a/plugin.go", pk("", "func init() {}\n"), "plugin.go:2: init function"},
		{"method named init", "plugins/a/plugin.go", pk("", "type T struct{}\nfunc (T) init() {}\n"), ""},
		{"mutable var", "plugins/a/plugin.go", pk("", "var counter int\n"), "package-level var counter is mutable state"},
		{"mutable var map", "plugins/a/plugin.go", pk("", "var m = map[string]int{}\n"), "mutable state"},
		{"other call var", "plugins/a/plugin.go", pk("import \"strings\"\n", "var r = strings.NewReader(\"\")\n"), "mutable state"},
		{"local var fine", "plugins/a/plugin.go", pk("", "func f() { var x int; _ = x }\n"), ""},
		{"allow-var doc", "plugins/a/plugin.go", pk("", "//kernelcheck:allow-var set once at start\nvar counter int\n"), ""},
		{"allow-var group", "plugins/a/plugin.go", pk("", "var (\n\t// kernelcheck:allow-var guarded by mu\n\tcounter int\n\tother int\n)\n"), "package-level var other"},
		{"allow-var trailing", "plugins/a/plugin.go", pk("", "var counter int //kernelcheck:allow-var one reader\n"), ""},
		{"allow-var no reason", "plugins/a/plugin.go", pk("", "//kernelcheck:allow-var\nvar counter int\n"), "needs a reason"},
		{"bare go", "plugins/a/plugin.go", pk("", "func f() { go f() }\n"), "bare go statement"},
		{"allow-go in plugin does not exempt", "plugins/a/plugin.go", pk("", "func f() {\n\t//kernelcheck:allow-go signal\n\tgo f()\n}\n"), "bare go statement"},
		{"sleep", "loader/a.go", pk("import \"time\"\n", "func f() { time.Sleep(1) }\n"), "time.Sleep is forbidden"},
		{"aliased sleep", "loader/a.go", pk("import t \"time\"\n", "func f() { t.Sleep(1) }\n"), "t.Sleep is forbidden"},
		{"other pkg named time", "loader/a.go", pk("import time \"example.com/time\"\n", "func f() { time.Sleep(1) }\n"), ""},
		{"time after fine", "loader/a.go", pk("import \"time\"\n", "func f() { _ = time.Now(); _ = time.After(1) }\n"), ""},
		{"signal.Notify", "plugins/a/plugin.go", pk("import \"os/signal\"\n", "func f() { signal.Notify(nil) }\n"), "signal.Notify"},
		{"signal.NotifyContext", "plugins/a/plugin.go", pk("import \"os/signal\"\n", "func f() { signal.NotifyContext(nil) }\n"), "signal.NotifyContext"},
		{"setenv", "plugins/a/plugin.go", pk("import \"os\"\n", "func f() { os.Setenv(\"a\", \"b\") }\n"), "os.Setenv"},
		{"getenv fine", "plugins/a/plugin.go", pk("import \"os\"\n", "func f() { os.Getenv(\"a\") }\n"), ""},
		{"exit", "plugins/a/plugin.go", pk("import \"os\"\n", "func f() { os.Exit(1) }\n"), "os.Exit"},
		{"log.Fatal", "plugins/a/plugin.go", pk("import \"log\"\n", "func f() { log.Fatalf(\"x\") }\n"), "log.Fatalf"},
		{"log.Print fine", "plugins/a/plugin.go", pk("import \"log\"\n", "func f() { log.Printf(\"x\") }\n"), ""},
		{"panic fine", "plugins/a/plugin.go", pk("", "func f() { panic(1) }\n"), ""},
		{"default mux", "plugins/a/plugin.go", pk("import \"net/http\"\n", "func f() { _ = http.DefaultServeMux }\n"), "http.DefaultServeMux"},
		{"http.Handle", "plugins/a/plugin.go", pk("import \"net/http\"\n", "func f() { http.Handle(\"/\", nil) }\n"), "http.Handle"},
		{"http.HandleFunc", "plugins/a/plugin.go", pk("import \"net/http\"\n", "func f() { http.HandleFunc(\"/\", nil) }\n"), "http.HandleFunc"},
		{"own mux fine", "plugins/a/plugin.go", pk("import \"net/http\"\n", "func f() { m := http.NewServeMux(); m.Handle(\"/\", nil) }\n"), ""},
		{"prometheus default", "plugins/a/plugin.go", pk("import \"github.com/prometheus/client_golang/prometheus\"\n", "func f() { prometheus.MustRegister(nil) }\n"), "prometheus.MustRegister"},
		{"prometheus default registerer", "plugins/a/plugin.go", pk("import \"github.com/prometheus/client_golang/prometheus\"\n", "func f() { _ = prometheus.DefaultRegisterer }\n"), "DefaultRegisterer"},
		{"prometheus own registry", "plugins/a/plugin.go", pk("import \"github.com/prometheus/client_golang/prometheus\"\n", "func f() { _ = prometheus.NewRegistry() }\n"), ""},
		{"promauto default", "plugins/a/plugin.go", pk("import \"github.com/prometheus/client_golang/prometheus/promauto\"\n", "func f() { promauto.NewCounter(nil) }\n"), "promauto.NewCounter"},
		{"promauto with", "plugins/a/plugin.go", pk("import \"github.com/prometheus/client_golang/prometheus/promauto\"\n", "func f() { promauto.With(nil) }\n"), ""},
		{"test file ignored", "plugins/a/plugin_test.go", pk("", "func init() { go func() {}() }\n"), ""},
		{"spike probe grandfathered", "plugins/jobs-river/internal/s0workers/w.go", pk("", "var n int\nfunc init() {}\n"), ""},
		{"jobs-river elsewhere not grandfathered", "plugins/jobs-river/internal/other/w.go", pk("", "var n int\n"), "mutable state"},
		{"boot bare go", "boot/a.go", pk("", "func f() { go f() }\n"), "boot needs a //kernelcheck:allow-go"},
		{"boot allow-go above", "boot/a.go", pk("", "func f() {\n\t//kernelcheck:allow-go signal goroutine, ends with the context\n\tgo f()\n}\n"), ""},
		{"boot allow-go same line", "boot/a.go", pk("", "func f() {\n\tgo f() //kernelcheck:allow-go signal goroutine\n}\n"), ""},
		{"boot allow-go no reason", "boot/a.go", pk("", "func f() {\n\t//kernelcheck:allow-go\n\tgo f()\n}\n"), "boot needs a //kernelcheck:allow-go"},
		{"boot allow-go far away", "boot/a.go", pk("", "func f() {\n\t//kernelcheck:allow-go why\n\n\tgo f()\n}\n"), "boot needs"},
		{"boot signal.Notify", "boot/a.go", pk("import \"os/signal\"\n", "func f() { signal.Notify(nil) }\n"), "signal.Notify"},
		{"boot init", "boot/a.go", pk("", "func init() {}\n"), "init function"},
		{"time.AfterFunc", "plugins/a/plugin.go", pk("import \"time\"\n", "func f() { time.AfterFunc(1, f) }\n"), "time.AfterFunc starts an untracked goroutine"},
		{"context.AfterFunc", "loader/a.go", pk("import \"context\"\n", "func f() { context.AfterFunc(nil, f) }\n"), "context.AfterFunc starts an untracked goroutine"},
		{"boot time.AfterFunc", "boot/a.go", pk("import \"time\"\n", "func f() { time.AfterFunc(1, f) }\n"), "boot/a.go:3: time.AfterFunc starts an untracked goroutine; boot needs a //kernelcheck:allow-go"},
		{"boot time.AfterFunc with allow-go", "boot/a.go", pk("import \"time\"\n", "func f() {\n\t//kernelcheck:allow-go the clock's timer, stopped by its caller\n\ttime.AfterFunc(1, f)\n}\n"), ""},
		{"blank pprof", "plugins/a/plugin.go", pk("import _ \"net/http/pprof\"\n", ""), "plugin.go:2: import of net/http/pprof registers handlers on http.DefaultServeMux"},
		{"named pprof", "loader/a.go", pk("import pp \"net/http/pprof\"\n", "var _ = pp.Index\n"), "import of net/http/pprof registers"},
		{"blank expvar", "boot/a.go", pk("import _ \"expvar\"\n", ""), "boot/a.go:2: import of expvar registers handlers on http.DefaultServeMux"},
		{"runtime/pprof fine", "plugins/a/plugin.go", pk("import _ \"runtime/pprof\"\n", ""), ""},
		{"WaitGroup.Go", "plugins/a/plugin.go", pk("import \"sync\"\n", "type T struct{ wg sync.WaitGroup }\nfunc (t *T) f() { t.wg.Go(t.f) }\n"), "plugin.go:4: (*sync.WaitGroup).Go starts an untracked goroutine"},
		{"embedded WaitGroup.Go", "plugins/a/internal/x/x.go", pk("import \"sync\"\n", "type T struct{ sync.WaitGroup }\nfunc f(t *T) { g := t.Go; g(nil) }\n"), "(*sync.WaitGroup).Go starts"},
		{"WaitGroup wait fine", "plugins/a/plugin.go", pk("import \"sync\"\n", "func f() { var wg sync.WaitGroup; wg.Add(1); wg.Done(); wg.Wait() }\n"), ""},
		{"kernel Go fine", "plugins/a/plugin.go", pk(kimp, "func f(c *kernel.Context) { c.Go(\"x\", nil) }\n"), ""},
		{"kernel.WithCommit", "plugins/a/plugin.go", pk(kimp, "func f(p kernel.Plugin) kernel.Plugin { return kernel.WithCommit(p, nil) }\n"), "plugin.go:3: kernel.WithCommit is forbidden"},
		{"kernel.WithCommit renamed import", "loader/a.go", pk("import k \""+mp+"/sdk/kernel\"\n", "var w = k.WithCommit\n"), "k.WithCommit is forbidden"},
		{"ListenAndServe nil", "plugins/a/plugin.go", pk("import \"net/http\"\n", "func f() { http.ListenAndServe(\":0\", nil) }\n"), "http.ListenAndServe with a nil handler serves http.DefaultServeMux"},
		{"ListenAndServeTLS nil", "plugins/a/plugin.go", pk("import \"net/http\"\n", "func f() { http.ListenAndServeTLS(\":0\", \"c\", \"k\", nil) }\n"), "http.ListenAndServeTLS with a nil handler"},
		{"Serve nil", "plugins/a/plugin.go", pk("import h \"net/http\"\n", "func f() { h.Serve(nil, nil) }\n"), "h.Serve with a nil handler"},
		{"ServeTLS nil", "boot/a.go", pk("import \"net/http\"\n", "func f() { http.ServeTLS(nil, nil, \"c\", \"k\") }\n"), "http.ServeTLS with a nil handler"},
		{"fcgi nil", "plugins/a/plugin.go", pk("import \"net/http/fcgi\"\n", "func f() { fcgi.Serve(nil, nil) }\n"), "fcgi.Serve with a nil handler"},
		{"serve own mux fine", "plugins/a/plugin.go", pk("import \"net/http\"\n", "func f() { http.Serve(nil, http.NewServeMux()); http.ServeTLS(nil, http.NewServeMux(), \"\", \"\") }\n"), ""},
		{"ListenAndServe own mux listens", "plugins/a/plugin.go", pk("import \"net/http\"\n", "func f() { http.ListenAndServe(\":0\", http.NewServeMux()) }\n"), "plugin.go:3: http.ListenAndServe opens a listener"},
		{"Server without handler", "plugins/a/plugin.go", pk("import \"net/http\"\n", "func f() { _ = &http.Server{Addr: \":0\"} }\n"), "http.Server without a Handler serves http.DefaultServeMux"},
		{"Server nil handler", "plugins/a/plugin.go", pk("import \"net/http\"\n", "func f() { _ = http.Server{Handler: nil} }\n"), "http.Server without a Handler"},
		{"Server with handler fine", "plugins/a/plugin.go", pk("import \"net/http\"\n", "func f() { _ = &http.Server{Addr: \":0\", Handler: http.NewServeMux()} }\n"), ""},
		{"new Server", "plugins/a/plugin.go", pk("import \"net/http\"\n", "func f() error { srv := new(http.Server); srv.Addr = \":0\"; return srv.ListenAndServe() }\n"), "plugin.go:3: new(http.Server) has no Handler"},
		{"zero Server var", "plugins/a/plugin.go", pk("import h \"net/http\"\n", "func f() error { var srv h.Server; return srv.ListenAndServe() }\n"), "plugin.go:3: a h.Server declared without a value has no Handler"},
		{"Server var with a literal fine", "plugins/a/plugin.go", pk("import \"net/http\"\n", "func f() { var srv = http.Server{Handler: http.NewServeMux()}; _ = &srv }\n"), ""},
		{"boot WaitGroup.Go", "boot/a.go", pk("import \"sync\"\n", "func f(wg *sync.WaitGroup) { wg.Go(func() {}) }\n"), "boot/a.go:3: (*sync.WaitGroup).Go starts an untracked goroutine; boot needs a //kernelcheck:allow-go"},
		{"boot context.AfterFunc", "boot/a.go", pk("import \"context\"\n", "func f(ctx context.Context) { context.AfterFunc(ctx, func() {}) }\n"), "boot/a.go:3: context.AfterFunc starts an untracked goroutine; boot needs a //kernelcheck:allow-go"},
		{"boot starters with allow-go", "boot/a.go", pk("import (\"context\"; \"sync\")\n", "func f(ctx context.Context, wg *sync.WaitGroup) {\n\t//kernelcheck:allow-go joined by Close\n\twg.Go(func() {})\n\tcontext.AfterFunc(ctx, func() {}) //kernelcheck:allow-go stops with ctx\n}\n"), ""},
		{"dot import exit", "plugins/a/plugin.go", pk("import . \"os\"\n", "func f() { Exit(1) }\n"), "plugin.go:2: dot import of os"},
		{"dot import signal in boot", "boot/a.go", pk("import . \"os/signal\"\n", "func f() { Notify(nil) }\n"), "dot import of os/signal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := t.TempDir()
			write(t, d, tc.file, tc.src)
			err := checkPluginVet([]string{filepath.Join(d, strings.SplitN(tc.file, "/", 2)[0])})
			switch {
			case tc.want == "" && err != nil:
				t.Fatal(err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

// Plugin vet refuses every call that opens a listener or binds a socket, except those
// that the one list allows a plugin id under the owner aiops (ADR-062 decisions 13
// and 18). The id is that of the plugin.yaml of the plugin that holds the file.
func TestPluginVetListeners(t *testing.T) {
	manifest := func(id string) string { return "manifestVersion: \"1.0\"\nid: " + id + "\nversion: 0.1.0\n" }
	for _, tc := range []struct {
		name   string
		id     string // the id of plugins/a/plugin.yaml; "" writes none
		file   string
		src    string
		want   string
		wantNo bool // no violation
	}{
		{"net.Listen", "aiops/a", "plugins/a/a.go", "import \"net\"\nfunc f() { net.Listen(\"tcp\", \":0\") }\n", "a.go:3: net.Listen opens a listener", false},
		{"net.ListenPacket", "aiops/a", "plugins/a/a.go", "import \"net\"\nfunc f() { net.ListenPacket(\"udp\", \":0\") }\n", "net.ListenPacket opens a listener", false},
		{"net.ListenTCP", "aiops/a", "plugins/a/a.go", "import \"net\"\nfunc f() { net.ListenTCP(\"tcp\", nil) }\n", "net.ListenTCP opens", false},
		{"net.ListenUDP", "aiops/a", "plugins/a/a.go", "import \"net\"\nfunc f() { net.ListenUDP(\"udp\", nil) }\n", "net.ListenUDP opens", false},
		{"net.ListenIP", "aiops/a", "plugins/a/a.go", "import \"net\"\nfunc f() { net.ListenIP(\"ip4:1\", nil) }\n", "net.ListenIP opens", false},
		{"net.ListenUnixgram", "aiops/a", "plugins/a/a.go", "import \"net\"\nfunc f() { net.ListenUnixgram(\"unixgram\", nil) }\n", "net.ListenUnixgram opens", false},
		{"net.ListenMulticastUDP", "aiops/a", "plugins/a/a.go", "import \"net\"\nfunc f() { net.ListenMulticastUDP(\"udp\", nil, nil) }\n", "net.ListenMulticastUDP opens", false},
		{"net.FileListener", "aiops/a", "plugins/a/a.go", "import \"net\"\nfunc f() { net.FileListener(nil) }\n", "net.FileListener opens", false},
		{"net.FilePacketConn", "aiops/a", "plugins/a/a.go", "import \"net\"\nfunc f() { net.FilePacketConn(nil) }\n", "net.FilePacketConn opens", false},
		{"net.Listen as a value", "aiops/a", "plugins/a/a.go", "import n \"net\"\nvar _ = n.Listen\n", "n.Listen opens a listener", false},
		{"tls.Listen", "aiops/a", "plugins/a/a.go", "import \"crypto/tls\"\nfunc f() { tls.Listen(\"tcp\", \":0\", nil) }\n", "tls.Listen opens a listener", false},
		{"http.ListenAndServeTLS", "aiops/a", "plugins/a/a.go", "import \"net/http\"\nfunc f() { http.ListenAndServeTLS(\":0\", \"c\", \"k\", http.NewServeMux()) }\n", "http.ListenAndServeTLS opens a listener", false},
		{"Server.ListenAndServe", "aiops/a", "plugins/a/a.go", "import \"net/http\"\nfunc f(s *http.Server) { s.ListenAndServe() }\n", "a.go:3: (*net/http.Server).ListenAndServe opens a listener", false},
		{"Server.ListenAndServeTLS", "aiops/a", "plugins/a/a.go", "import \"net/http\"\nfunc f(s http.Server) { s.ListenAndServeTLS(\"c\", \"k\") }\n", "(*net/http.Server).ListenAndServeTLS opens", false},
		{"httptest.NewServer outside a test", "aiops/a", "plugins/a/a.go", "import \"net/http/httptest\"\nfunc f() { httptest.NewServer(nil) }\n", "httptest.NewServer opens a listener", false},
		{"httptest.NewTLSServer", "aiops/a", "plugins/a/a.go", "import \"net/http/httptest\"\nfunc f() { httptest.NewTLSServer(nil) }\n", "httptest.NewTLSServer opens", false},
		{"httptest.NewUnstartedServer", "aiops/a", "plugins/a/a.go", "import \"net/http/httptest\"\nfunc f() { httptest.NewUnstartedServer(nil) }\n", "httptest.NewUnstartedServer opens", false},
		{"httptest Server.Start", "aiops/a", "plugins/a/a.go", "import \"net/http/httptest\"\nfunc f(s *httptest.Server) { s.Start() }\n", "(*net/http/httptest.Server).Start opens", false},
		{"httptest Server.StartTLS", "aiops/a", "plugins/a/a.go", "import \"net/http/httptest\"\nfunc f(s *httptest.Server) { s.StartTLS() }\n", "(*net/http/httptest.Server).StartTLS opens", false},
		{"httptest in a test file is not read", "aiops/a", "plugins/a/a_test.go", "import \"net/http/httptest\"\nfunc f() { httptest.NewServer(nil).Start() }\n", "", true},
		{"syscall.Listen", "aiops/a", "plugins/a/a.go", "import \"syscall\"\nfunc f() { syscall.Listen(0, 1) }\n", "syscall.Listen opens a listener", false},
		{"syscall.Bind", "aiops/a", "plugins/a/a.go", "import \"syscall\"\nfunc f() { syscall.Bind(0, nil) }\n", "syscall.Bind opens a listener", false},
		{"unix.Listen", "aiops/a", "plugins/a/a.go", "import \"golang.org/x/sys/unix\"\nfunc f() { unix.Listen(0, 1) }\n", "unix.Listen opens a listener", false},
		{"unix.Bind", "aiops/a", "plugins/a/a.go", "import \"golang.org/x/sys/unix\"\nfunc f() { unix.Bind(0, nil) }\n", "unix.Bind opens a listener", false},
		{"ListenConfig.Listen", "aiops/a", "plugins/a/a.go", "import (\"context\"; \"net\")\nfunc f(ctx context.Context) { var lc net.ListenConfig; lc.Listen(ctx, \"tcp\", \":0\") }\n", "(*net.ListenConfig).Listen opens a listener", false},
		{"ListenConfig.ListenPacket", "aiops/a", "plugins/a/a.go", "import (\"context\"; \"net\")\nfunc f(ctx context.Context) { lc := &net.ListenConfig{}; lc.ListenPacket(ctx, \"udp\", \":0\") }\n", "(*net.ListenConfig).ListenPacket opens", false},
		{"ListenUnix admitted for reload-socket", "aiops/reload-socket", "plugins/a/a.go", "import \"net\"\nfunc f() { net.ListenUnix(\"unix\", nil) }\n", "", true},
		{"ListenUnix admitted in a package below reload-socket", "aiops/reload-socket", "plugins/a/internal/x/x.go", "import \"net\"\nfunc f() { net.ListenUnix(\"unix\", nil) }\n", "", true},
		{"reload-socket may not call net.Listen", "aiops/reload-socket", "plugins/a/a.go", "import \"net\"\nfunc f() { net.Listen(\"unix\", \"s\") }\n", "net.Listen opens a listener", false},
		{"ListenUnix refused for another id", "aiops/other", "plugins/a/a.go", "import \"net\"\nfunc f() { net.ListenUnix(\"unix\", nil) }\n", "a.go:3: net.ListenUnix opens a listener", false},
		{"ListenUnix refused for a third party", "acme/reload-socket", "plugins/a/a.go", "import \"net\"\nfunc f() { net.ListenUnix(\"unix\", nil) }\n", "net.ListenUnix opens a listener", false},
		{"ListenUnix refused with no manifest", "", "plugins/a/a.go", "import \"net\"\nfunc f() { net.ListenUnix(\"unix\", nil) }\n", "net.ListenUnix opens a listener", false},
		{"ListenUnix refused in boot", "", "boot/a.go", "import \"net\"\nfunc f() { net.ListenUnix(\"unix\", nil) }\n", "boot/a.go:3: net.ListenUnix opens a listener", false},
		{"the dial exception admits no listener", "aiops/operations", "plugins/a/a.go", "import \"net\"\nfunc f() { net.ListenUnix(\"unix\", nil) }\n", "net.ListenUnix opens a listener", false},
		{"a dial is not a listener", "aiops/a", "plugins/a/a.go", "import \"net\"\nfunc f() { net.DialUnix(\"unix\", nil, nil); net.Dial(\"tcp\", \"x:1\") }\n", "", true},
		{"a method of another type", "aiops/a", "plugins/a/a.go", "type T struct{}\nfunc (T) Listen() {}\nfunc (T) Start() {}\nfunc f(t T) { t.Listen(); t.Start() }\n", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := t.TempDir()
			if tc.id != "" {
				write(t, d, "plugins/a/plugin.yaml", manifest(tc.id))
			}
			write(t, d, tc.file, "package a\n"+tc.src)
			err := checkPluginVet([]string{filepath.Join(d, strings.SplitN(tc.file, "/", 2)[0])})
			switch {
			case tc.wantNo && err != nil:
				t.Fatal(err)
			case !tc.wantNo && (err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "ADR-062 decision 13")):
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

// The list holds one entry per plugin id, each under the reserved owner aiops: at S2a
// net.ListenUnix for reload-socket and the named exception of the dial rule for the
// operations plugin (ADR-062 decisions 13 and 14).
func TestPluginVetListenList(t *testing.T) {
	want := map[string][]string{"aiops/reload-socket": {"net.ListenUnix"}, "aiops/operations": {"net.DialUnix"}}
	if !reflect.DeepEqual(listenAllow, want) {
		t.Fatalf("list = %v, want %v", listenAllow, want)
	}
	for id := range listenAllow {
		if !strings.HasPrefix(id, "aiops/") {
			t.Errorf("%s is not under the reserved owner aiops", id)
		}
	}
}

func TestPluginVetCountsExemptions(t *testing.T) {
	d := t.TempDir()
	write(t, d, "plugins/a/plugin.go", "package p\n\n//kernelcheck:allow-var one\nvar a int\n\n//kernelcheck:allow-var two\nvar b int\n")
	r, err := vetFile(filepath.Join(d, "plugins/a/plugin.go"), "plugins/a/plugin.go", false, "")
	if err != nil || len(r.violations) != 0 || len(r.exemptions) != 2 || r.exemptions[0] != "plugins/a/plugin.go:4: allow-var: one" {
		t.Fatalf("%+v %v", r, err)
	}
}

// Every goroutine starter that boot exempts with allow-go is counted, as a bare go is.
func TestPluginVetCountsBootStarters(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "boot/a.go")
	write(t, d, "boot/a.go", "package boot\n\nimport (\"context\"; \"sync\")\n\nfunc f(ctx context.Context, wg *sync.WaitGroup) {\n\twg.Go(func() {}) //kernelcheck:allow-go joined by Close\n\tcontext.AfterFunc(ctx, func() {}) //kernelcheck:allow-go stops with ctx\n}\n")
	r, err := vetFile(p, "boot/a.go", true, "")
	if err != nil || len(r.violations) != 0 || !slices.Equal(r.exemptions, []string{"boot/a.go:7: allow-go: stops with ctx"}) {
		t.Fatalf("file: %+v %v", r, err)
	}
	r, err = vetMethods(map[string]string{p: "boot/a.go"}, stdImporter, true, "")
	if err != nil || len(r.violations) != 0 || !slices.Equal(r.exemptions, []string{"boot/a.go:6: allow-go: joined by Close"}) {
		t.Fatalf("methods: %+v %v", r, err)
	}
}

func TestPluginVetWaitGroupTypeInOtherFile(t *testing.T) {
	d := t.TempDir()
	write(t, d, "plugins/a/internal/x/a.go", "package x\n\nimport \"sync\"\n\ntype runner struct{ wg sync.WaitGroup }\n")
	write(t, d, "plugins/a/internal/x/b.go", "package x\n\nfunc (r *runner) start() { r.wg.Go(r.start) }\n")
	err := checkPluginVet([]string{filepath.Join(d, "plugins")})
	if err == nil || !strings.Contains(err.Error(), "internal/x/b.go:3: (*sync.WaitGroup).Go starts an untracked goroutine") {
		t.Fatalf("got %v", err)
	}
}

// A WaitGroup reached through a type or a function of another package of the plugin is
// found too: the packages of the vetted tree are type-checked from their source.
func TestPluginVetWaitGroupTypeInOtherPackage(t *testing.T) {
	d := t.TempDir()
	write(t, d, "plugins/a/internal/x/x.go", "package x\n\nimport \"sync\"\n\ntype Pool struct{ sync.WaitGroup }\n\nfunc New() *sync.WaitGroup { return new(sync.WaitGroup) }\n")
	write(t, d, "plugins/a/plugin.go", "package a\n\nimport \"github.com/mutex-solutions/aiops-platform/plugins/a/internal/x\"\n\nfunc f() { var p x.Pool; p.Go(f); x.New().Go(f) }\n")
	write(t, d, "plugins/a/other.go", "package a\n\nimport \"github.com/mutex-solutions/aiops-platform/plugins/b\"\n\nfunc g() { b.Run(g) }\n")
	err := checkPluginVet([]string{filepath.Join(d, "plugins")})
	if err == nil || strings.Count(err.Error(), "plugins/a/plugin.go:5: (*sync.WaitGroup).Go starts an untracked goroutine") != 2 {
		t.Fatalf("got %v", err)
	}
}

func TestPluginVetErrors(t *testing.T) {
	d := t.TempDir()
	write(t, d, "plugins/a/plugin.go", "package")
	if checkPluginVet([]string{filepath.Join(d, "plugins")}) == nil {
		t.Error("parse error ignored")
	}
	if checkPluginVet([]string{filepath.Join(d, "missing")}) == nil {
		t.Error("missing dir ignored")
	}
}

func TestLinesProvisionalAndTestdata(t *testing.T) {
	d := t.TempDir()
	write(t, d, "a.go", "package k\n")
	write(t, d, "testdata/x/b.go", "package x\n")
	if err := checkLines(d, 5, true); err != nil {
		t.Fatalf("testdata must be skipped: %v", err)
	}
	write(t, d, "sub/c.go", "package sub\n")
	if err := checkLines(d, 5, true); err == nil {
		t.Fatal("subpackage accepted")
	}
}

// The imports command reads the register it is given, with every key of an entry, and
// fails when it cannot.
func TestRunImportsReadsTheRegister(t *testing.T) {
	d, r := t.TempDir(), t.TempDir()
	reg := filepath.Join(r, "register.yaml")
	write(t, r, "register.yaml", "entries:\n  - id: y\n    names: [y]\n    form: library\n    modules: [example.org/y]\n    importers:\n      - {dir: boot, decision: an accepted decision}\nexceptions: []\n")
	write(t, d, "boot/b.go", imp("example.org/y/z"))
	if err := run([]string{"imports", d, reg}); err != nil {
		t.Fatalf("an importer the register lists is refused: %v", err)
	}
	write(t, d, "plugins/p/internal/x/x.go", imp("example.org/y/z"))
	if err := run([]string{"imports", d, reg}); err == nil || !strings.Contains(err.Error(), "register entry y") {
		t.Fatalf("an import outside the importers of the register: %v", err)
	}
	write(t, r, "bad.yaml", "entries: {\n")
	for _, p := range []string{filepath.Join(r, "missing.yaml"), filepath.Join(r, "bad.yaml")} {
		if run([]string{"imports", d, p}) == nil {
			t.Errorf("imports with the register %s succeeded", p)
		}
	}
}

func TestRunNewCommands(t *testing.T) {
	d := t.TempDir()
	write(t, d, "boot/a.go", "package boot\n")
	if err := run([]string{"imports", d, filepath.Join("..", "upstream-components.yaml")}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"pluginvet", d}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"lines", filepath.Join(d, "boot"), "5", "provisional"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"lines", filepath.Join(d, "boot"), "-1"}); err == nil {
		t.Error("negative cap accepted")
	}
	for _, a := range [][]string{{"pluginvet"}, {"imports"}, {"imports", d}, {"lines", d, "5", "other"}} {
		if run(a) == nil {
			t.Errorf("run(%v) succeeded", a)
		}
	}
	if _, err := os.Stat(d); err != nil {
		t.Fatal(err)
	}
}
