package main

import (
	"errors"
	"os"
	"path/filepath"
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
		{"boot third party", "boot/a.go", imp("github.com/jackc/pgx/v5"), "boot imports"},
		{"boot testdata skipped", "boot/testdata/a.go", imp(mp + "/plugins/clock"), ""},
		{"loader allowed", "loader/a.go", imp(mp+"/sdk/kernel", mp+"/sdk/def/clock/v0"), ""},
		{"loader yaml", "loader/a.go", imp("go.yaml.in/yaml/v3"), "YAML package"},
		{"loader plugin", "loader/a.go", imp(mp + "/plugins/clock"), "loader imports"},
		{"loader test fake", "loader/a_test.go", imp(mp + "/sdk/def/clock/v0/fake"), ""},
		{"loader non-test fake", "loader/a.go", imp(mp + "/sdk/def/clock/v0/fake"), "no fake or sdk/testkit"},
		{"plugin ok", "plugins/a/plugin.go", imp(mp+"/sdk/kernel", mp+"/sdk/def/clock/v0", mp+"/plugins/a/internal/x"), ""},
		{"plugin third party", "plugins/a/internal/x/x.go", imp("github.com/jackc/pgx/v5"), "a plugin imports"},
		{"grandfathered pgx", "plugins/jobs-river/internal/s0/x.go", imp("github.com/jackc/pgx/v5", "github.com/riverqueue/river/rivertype"), ""},
		{"grandfather is per plugin", "plugins/a/internal/x/x.go", imp("github.com/riverqueue/river"), "a plugin imports"},
		{"plugin other plugin", "plugins/a/plugin.go", imp(mp + "/plugins/b"), "a plugin imports"},
		{"plugin boot", "plugins/a/plugin.go", imp(mp + "/boot"), "a plugin imports"},
		{"plugin loader", "plugins/a/plugin.go", imp(mp + "/loader"), "a plugin imports"},
		{"plugin cmd", "plugins/a/plugin.go", imp(mp + "/cmd/x"), "a plugin imports"},
		{"plugin yaml", "plugins/a/plugin.go", imp("go.yaml.in/yaml/v3"), "YAML package"},
		{"plugin test testkit", "plugins/a/plugin_test.go", imp(mp + "/sdk/testkit/kerneltest"), ""},
		{"plugin non-test testkit", "plugins/a/plugin.go", imp(mp + "/sdk/testkit/kerneltest"), "no fake or sdk/testkit"},
		{"plugin exported package", "plugins/a/api/api.go", imp("fmt"), "under internal/"},
		{"plugin extra root file", "plugins/a/other.go", imp("fmt"), "under internal/"},
		{"plugin test at root", "plugins/a/zz_cycle_test.go", imp("fmt"), ""},
		{"yaml in tools", "tools/x/x.go", imp("go.yaml.in/yaml/v3"), ""},
		{"yaml in test", "test/x/x_test.go", imp("gopkg.in/yaml.v3"), "YAML package"},
		{"fake in test tree", "test/x/x.go", imp(mp + "/sdk/def/clock/v0/fake"), ""},
		{"fake in aiopsctl", "cmd/aiopsctl/x.go", imp(mp + "/sdk/def/clock/v0/fake"), ""},
		{"fake in cmd/aiops", "cmd/aiops/x.go", imp(mp + "/sdk/def/clock/v0/fake"), "no fake or sdk/testkit"},
		{"testkit in dist", "dist/x.go", imp(mp + "/sdk/testkit/kerneltest"), "no fake or sdk/testkit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := t.TempDir()
			write(t, d, tc.file, tc.src)
			err := checkImports(d, noList)
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
	err := checkImports(d, noList)
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
	if err := checkImports(d, ok); err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{mp + "/sdk/def/clock/v0/fake", mp + "/sdk/testkit/kerneltest"} {
		leaky := func(string, string) ([]string, error) { return []string{"fmt", leak}, nil }
		if err := checkImports(d, leaky); err == nil || !strings.Contains(err.Error(), "cmd/aiops: dependency closure contains "+leak) {
			t.Errorf("leak %s: %v", leak, err)
		}
	}
	failing := func(string, string) ([]string, error) { return nil, errors.New("boom") }
	if err := checkImports(d, failing); err == nil {
		t.Error("list failure ignored")
	}
}

func TestImportsParseError(t *testing.T) {
	d := t.TempDir()
	write(t, d, "boot/a.go", "package")
	if checkImports(d, noList) == nil {
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
	pk := func(imports, body string) string { return "package p\n" + imports + body }
	for _, tc := range []struct {
		name, file, src string
		want            string
	}{
		{"clean plugin", "plugins/a/plugin.go", pk(kimp+"import (\"errors\"; \"fmt\")\n", "var Plugin = kernel.Define(kernel.Plugin{})\nvar ErrX = errors.New(\"x\")\nvar ErrY = fmt.Errorf(\"y\")\nvar _ I = (*T)(nil)\nconst c = 1\ntype I interface{}\ntype T struct{}\nvar key = kernel.NewKey[int](\"k\")\n"), ""},
		{"def constructor", "plugins/a/plugin.go", pk("import def \""+mp+"/sdk/def/clock/v0\"\n", "var h = def.NewHook()\n"), ""},
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

func TestPluginVetCountsExemptions(t *testing.T) {
	d := t.TempDir()
	write(t, d, "plugins/a/plugin.go", "package p\n\n//kernelcheck:allow-var one\nvar a int\n\n//kernelcheck:allow-var two\nvar b int\n")
	r, err := vetFile(filepath.Join(d, "plugins/a/plugin.go"), "plugins/a/plugin.go", false)
	if err != nil || len(r.violations) != 0 || len(r.exemptions) != 2 || r.exemptions[0] != "plugins/a/plugin.go:4: allow-var: one" {
		t.Fatalf("%+v %v", r, err)
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

func TestRunNewCommands(t *testing.T) {
	d := t.TempDir()
	write(t, d, "boot/a.go", "package boot\n")
	if err := run([]string{"imports", d}); err != nil {
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
	for _, a := range [][]string{{"pluginvet"}, {"imports"}, {"lines", d, "5", "other"}} {
		if run(a) == nil {
			t.Errorf("run(%v) succeeded", a)
		}
	}
	if _, err := os.Stat(d); err != nil {
		t.Fatal(err)
	}
}
