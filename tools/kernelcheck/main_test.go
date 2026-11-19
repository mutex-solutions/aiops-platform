package main

import (
	"os"
	"path/filepath"
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
	for _, tc := range []struct {
		name, mod string
		fail      bool
	}{
		{"clean", "module m\n\ngo 1.26.0\n", false},
		{"require", "module m\n\nrequire x v1.0.0\n", true},
		{"require block", "module m\nrequire (\n\tx v1.0.0\n)\n", true},
		{"replace", "module m\nreplace x => ../x\n", true},
		{"exclude", "module m\nexclude x v1.0.0\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := t.TempDir()
			write(t, d, "go.mod", tc.mod)
			if err := checkModfile(d); (err != nil) != tc.fail {
				t.Fatalf("fail=%v, err=%v", tc.fail, err)
			}
		})
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
		fail  bool
	}{
		{"under", 10, false, false},
		{"exact", 5, false, false},
		{"over", 4, false, true},
		{"subdir package", 10, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := t.TempDir()
			write(t, d, "a.go", "package k\n\n\n\n// 5") // 5 lines, no trailing newline
			write(t, d, "a_test.go", strings.Repeat("x\n", 100))
			if tc.sub {
				write(t, d, "sub/b.go", "package sub\n")
			}
			if err := checkLines(d, tc.limit); (err != nil) != tc.fail {
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
