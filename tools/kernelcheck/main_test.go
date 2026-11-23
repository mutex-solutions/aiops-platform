package main

import (
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestModfile(t *testing.T) {
	ab := []string{"a", "b"}
	for _, tc := range []struct {
		name, mod string
		allowed   []string
		fail      bool
	}{
		{"clean", "module m\n\ngo 1.26.0\n", nil, false},
		{"require", "module m\n\nrequire x v1.0.0\n", nil, true},
		{"require block", "module m\nrequire (\n\tx v1.0.0\n)\n", nil, true},
		{"replace", "module m\nreplace x => ../x\n", nil, true},
		{"exclude", "module m\nexclude x v1.0.0\n", nil, true},
		{"allowed", "module m\n\n// c\nrequire a v0.0.0\n\nrequire (\n\tb v0.0.0 // indirect\n)\n\nreplace (\n\ta => ../a\n\tb => ../b\n)\n", ab, false},
		{"other require", "module m\nrequire (\n\ta v0.0.0\n\tx v1.0.0\n)\n", ab, true},
		{"other replace", "module m\nrequire a v0.0.0\nreplace x => ../x\n", ab, true},
		{"allowed exclude", "module m\nexclude a v1.0.0\n", ab, true},
		{"unparsable", "module m\nrequire\n", ab, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := t.TempDir()
			write(t, d, "go.mod", tc.mod)
			if err := run(append([]string{"modfile", d}, tc.allowed...)); (err != nil) != tc.fail {
				t.Fatalf("fail=%v, err=%v", tc.fail, err)
			}
		})
	}
	if run([]string{"modfile", t.TempDir()}) == nil {
		t.Fatal("missing go.mod accepted")
	}
}

// TestRepositoryModules holds the module layout of ADR-060 decision 54 on the
// repository itself. The import path of every package of every module is its
// directory below the repository root, so moving a module root changes no import
// path; every package under sdk/def belongs to the SDK module rooted at sdk/; and
// the SDK module requires only sdk/kernel and sdk/testkit (00-decisions.md §4.3;
// pgx v5 joins at S3, ADR-060 decision 4).
func TestRepositoryModules(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "list", "-f", "{{.ImportPath}}\t{{.Dir}}\t{{.Module.Path}}\t{{.Module.Dir}}", "./...", mp+"/sdk/...")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	want := func(dir string) string {
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			t.Fatal(err)
		}
		return path.Join(mp, filepath.ToSlash(rel))
	}
	def := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			t.Fatalf("go list line %q", line)
		}
		if f[0] != want(f[1]) {
			t.Errorf("package %s is in directory %s", f[0], f[1])
		}
		if f[2] != want(f[3]) {
			t.Errorf("module %s is rooted at %s", f[2], f[3])
		}
		if hasPath(f[0], mp+"/sdk/def") {
			def++
			if f[2] != mp+"/sdk" {
				t.Errorf("package %s is in module %s, not %s/sdk", f[0], f[2], mp)
			}
		}
	}
	if def == 0 {
		t.Error("go list printed no package under sdk/def")
	}
	if err := run([]string{"modfile", filepath.Join(root, "sdk"), mp + "/sdk/kernel", mp + "/sdk/testkit"}); err != nil {
		t.Error(err)
	}
}

func TestVocab(t *testing.T) {
	list := "# c\ntenant # trailing\nriver\nprofile\n"
	for _, tc := range []struct {
		name, src string
		fail      bool
	}{
		{"clean", "package k\n\n// plain\nvar rivet = \"tenancy\"\n", false},
		{"camel", "package k\nvar tenantID int\n", true},
		{"underscore", "package k\nvar my_River int\n", true},
		{"comment", "package k\n// the Tenant owns it\n", true},
		{"string", "package k\nvar s = \"a river\"\n", true},
		{"acronym", "package k\nvar HTTPTenant int\n", true},
		{"plural", "package k\n// zzProbe counts tenants per row.\n", true},
		{"plural es", "package k\nfunc Rivers() {}\n", true},
		{"inflected", "package k\n// the tenanted rows and the tenanting step\n", true},
		{"digit", "package k\nvar River3 int\n", true},
		{"digit first", "package k\nvar s = \"3river\"\n", true},
		{"cited document", "package k\n// See 08-profiles-roadmap-adrs.md:12.\n", false},
		{"other document", "package k\n// See 09-profiles-plan.md:12.\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := t.TempDir()
			write(t, d, "w.txt", list)
			write(t, d, "a.go", tc.src)
			write(t, d, "a_test.go", "package k\nvar tenantID int\n") // ignored
			if err := checkVocab(d, filepath.Join(d, "w.txt")); (err != nil) != tc.fail {
				t.Fatalf("fail=%v, err=%v", tc.fail, err)
			}
		})
	}
}

// TestVocabularyLists holds the word lists of the vocab check to ADR-060
// decision 45 (Q9 (a)). The kernel's list holds every product word of
// 00-decisions.md §4.3 except the four the kernel uses in other senses, which
// the S2a report lists for review: role (the worker role of an observer
// goroutine, observe.go), row (a row of the plugin record, config.go), model (the
// Cordis lifecycle model, every file header) and gate (the Needs gate
// activation, root.go and seam.go). The boot list holds the domain words of
// decision 45 and the upstream names; the loader list adds tenant, as at S1; the
// kernel list holds both, so no layer below boot holds a word boot may not. An
// upstream name leaves the lists in the commit that adds a register entry carrying
// it, from which the name lint U1 reads it over sdk/, boot/ and loader/ (ADR-054
// decisions 14 and 20), and not before.
func TestVocabularyLists(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "architecture", "00-decisions.md"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`no product word \(([^)]*)\)`).FindSubmatch(doc)
	if m == nil {
		t.Fatal("00-decisions.md §4.3 names no product word")
	}
	boot := map[string]bool{}
	for _, w := range []string{"incident", "alert", "approval", "dashboard", "pillar", "harness", "remediation", "estate"} {
		boot[w] = true
	}
	reg, err := os.ReadFile(filepath.Join("..", "upstream-components.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var r struct{ Entries []struct{ Names []string } }
	if err := yaml.Unmarshal(reg, &r); err != nil {
		t.Fatal(err)
	}
	named := map[string]bool{}
	for _, e := range r.Entries {
		for _, n := range e.Names {
			named[n] = true
		}
	}
	for _, n := range upstreamNames {
		if !named[n] {
			boot[n] = true
		}
	}
	loader := maps.Clone(boot)
	loader["tenant"] = true
	kernel := maps.Clone(loader)
	for _, w := range strings.Split(string(m[1]), ", ") {
		if w = strings.ToLower(w); !slices.Contains([]string{"role", "row", "model", "gate"}, w) {
			kernel[w] = true
		}
	}
	for _, l := range []struct {
		name string
		want map[string]bool
	}{{"vocabulary-boot.txt", boot}, {"vocabulary-loader.txt", loader}, {"vocabulary.txt", kernel}} {
		got, err := readWords(l.name)
		if err != nil {
			t.Fatal(err)
		}
		if !maps.Equal(got, l.want) {
			t.Errorf("%s holds %q, want %q", l.name, slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(l.want)))
		}
	}
}

// upstreamNames are the upstream names of 00-decisions.md §3.2 and §4.3, as lowercase
// technical names, that ADR-060 decision 45 puts in the word lists. Words split at
// case changes, so PostgreSQL reads as Postgre and SQL.
var upstreamNames = []string{"envoy", "postgres", "postgresql", "seaweedfs", "minio", "mimir", "alertmanager", "loki",
	"tempo", "pyroscope", "kafka", "grafana", "holmesgpt", "goalert", "tetragon", "nats", "jetstream", "krr", "chromium",
	"river", "pgx"}

func TestLines(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit int
		sub   bool
		args  []string
		fail  bool
	}{
		{"under", 10, false, nil, false},
		{"exact", 5, false, nil, false},
		{"over", 4, false, nil, true},
		{"subdir package", 10, true, nil, true},
		{"provisional", 10, false, []string{"provisional"}, false},
		// A definition's fake and suite are packages of their own, each with its own
		// cap (ADR-060 decision 35): a named subdirectory is not counted.
		{"named subpackage", 5, true, []string{"sub"}, false},
		{"named subpackage, provisional", 5, true, []string{"provisional", "sub"}, false},
		{"other subpackage", 10, true, []string{"other"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := t.TempDir()
			write(t, d, "a.go", "package k\n\n\n\n// 5") // 5 lines, no trailing newline
			write(t, d, "a_test.go", strings.Repeat("x\n", 100))
			if tc.sub {
				write(t, d, "sub/b.go", "package sub\n")
				write(t, d, "sub/deep/c.go", "package deep\n")
			}
			if err := run(append([]string{"lines", d, strconv.Itoa(tc.limit)}, tc.args...)); (err != nil) != tc.fail {
				t.Fatalf("fail=%v, err=%v", tc.fail, err)
			}
		})
	}
}

func TestCoverage(t *testing.T) {
	const p = "example.com/m/sdk/kernel"
	for _, tc := range []struct {
		name, prof string
		fail       bool
	}{
		{"full", "mode: set\n" + p + "/a.go:1.1,2.2 2 1\n" + p + "/a.go:3.1,4.2 1 3\n", false},
		{"uncovered block", "mode: set\n" + p + "/a.go:1.1,2.2 2 1\n" + p + "/a.go:3.1,4.2 1 0\n", true},
		{"zero statements", "mode: set\n" + p + "/a.go:1.1,2.2 0 0\n", false},
		{"other package ignored", "mode: set\n" + p + "/a.go:1.1,2.2 1 1\nexample.com/m/other/b.go:1.1,2.2 1 0\n", false},
		{"test file ignored", "mode: set\n" + p + "/a.go:1.1,2.2 1 1\n" + p + "/a_test.go:1.1,2.2 1 0\n", false},
		{"no files", "mode: set\nexample.com/m/other/b.go:1.1,2.2 1 1\n", true},
		{"malformed", "mode: set\ngarbage\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := t.TempDir()
			write(t, d, "c.out", tc.prof)
			if err := checkCoverage(filepath.Join(d, "c.out"), p); (err != nil) != tc.fail {
				t.Fatalf("fail=%v, err=%v", tc.fail, err)
			}
		})
	}
}

func TestTestCtor(t *testing.T) {
	const imp = "import \"github.com/mutex-solutions/aiops-platform/sdk/kernel\"\n"
	const use = "var _ = kernel.NewForTest\n"
	for _, tc := range []struct {
		name, file, src string
		fail            bool
	}{
		{"clean", "plugins/a/a.go", "package a\n" + imp + "var _ = kernel.New\n", false},
		{"no import", "plugins/a/a.go", "package a\nvar kernel struct{ NewForTest int }\nvar _ = kernel.NewForTest\n", false},
		{"plugin violation", "plugins/a/a_test.go", "package a\n" + imp + use, true},
		{"alias violation", "plugins/a/a.go", "package a\nimport k \"github.com/mutex-solutions/aiops-platform/sdk/kernel\"\nvar _ = k.NewForTest\n", true},
		{"dot import violation", "plugins/a/a.go", "package a\nimport . \"github.com/mutex-solutions/aiops-platform/sdk/kernel\"\nvar _ = NewForTest\n", true},
		{"other package aliased as kernel", "plugins/a/a.go", "package a\nimport kernel \"example.com/other\"\nvar _ = kernel.NewForTest\n", false},
		{"kernel itself", "sdk/kernel/root_test.go", "package kernel\n" + imp + use, false},
		{"kerneltest", "sdk/testkit/kerneltest/kerneltest.go", "package kerneltest\n" + imp + use, false},
		{"testdata skipped", "plugins/a/testdata/a.go", "package a\n" + imp + use, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := t.TempDir()
			write(t, d, tc.file, tc.src)
			if err := checkTestCtor(d); (err != nil) != tc.fail {
				t.Fatalf("fail=%v, err=%v", tc.fail, err)
			}
		})
	}
}

func TestRunUsage(t *testing.T) {
	for _, a := range [][]string{nil, {"bogus"}, {"modfile"}, {"lines", "d", "x"}, {"testctor"}} {
		if run(a) == nil {
			t.Errorf("run(%v) succeeded", a)
		}
	}
}
