package main

import (
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
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
	list := "# c\ntenant # trailing\nriver\n"
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
