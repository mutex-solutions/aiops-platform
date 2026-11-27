package main

import (
	"os"
	"path/filepath"
	"slices"
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

// The listing of a package holds each exported name with its type, never a
// parameter name, an unexported field or a comment, so a rename of either or a new
// comment needs no new pluginApi. An unexported type that the exported API names
// is listed with its exported fields and methods, under its exported alias when it
// has one.
func TestListing(t *testing.T) {
	d := t.TempDir()
	write(t, d, "go.mod", "module example.com/m\n\ngo 1.26.0\n")
	write(t, d, "p/p.go", `// Package p is a fixture.
package p

import "context"

// C is a constant.
const C = "v"

const (
	Typed State = "ON"
	hidden      = 1
)

type State string

var ErrX error

var Default = New(1)

// New makes a T.
func New(size int, opts ...Option) *T { return nil }

func Map[K comparable, V any](m map[K]V, f func(key K) (V, error)) []V { return nil }

type Option func(o *opts)

type opts struct {
	N int
	n int
}

func (o *opts) Set(n int) {}

type unreached struct{ X int }

func (unreached) Exported() {}

type T struct {
	Name    string
	Ch      <-chan int
	Fn      func(ctx context.Context, n int) error
	private int
	Embedded
	Other inner
}

type Embedded struct{ Inner [2]map[string]*T }

func (Embedded) Promoted(x int) {}

func (t *T) Close(ctx context.Context) (err error) { return nil }

func (t T) Size() int { return 0 }

func (t *T) unexported() {}

type Box[V any] struct{ v V }

func (b Box[V]) Get() V { return b.v }

type I interface {
	Do(n int) (string, error)
	hidden()
}

type Number interface{ ~int | ~float64 }

type Alias = inner

type inner struct{ Field int }

func (inner) Method() {}

type Pub = T
`)
	pkgs, err := load(d, "example.com/m/p")
	if err != nil {
		t.Fatal(err)
	}
	got := list(pkgs["example.com/m/p"])
	want := []string{
		"pkg p, const C untyped string = \"v\"",
		"pkg p, const Typed State = \"ON\"",
		"pkg p, func Map[K comparable, V any](map[K]V, func(K) (V, error)) []V",
		"pkg p, func New(int, ...Option) *T",
		"pkg p, method (*T) Close(context.Context) error",
		"pkg p, method (*opts) Set(int)",
		"pkg p, method (Alias) Method()",
		"pkg p, method (Box[V]) Get() V",
		"pkg p, method (Embedded) Promoted(int)",
		"pkg p, method (T) Promoted(int)",
		"pkg p, method (T) Size() int",
		"pkg p, type Alias struct",
		"pkg p, type Alias struct, Field int",
		"pkg p, type Box[V any] struct",
		"pkg p, type Embedded struct",
		"pkg p, type Embedded struct, Inner [2]map[string]*T",
		"pkg p, type I interface",
		"pkg p, type I interface, Do(int) (string, error)",
		"pkg p, type I interface, unexported methods",
		"pkg p, type Number interface",
		"pkg p, type Number interface, ~int | ~float64",
		"pkg p, type Option func(*opts)",
		"pkg p, type Pub = T",
		"pkg p, type State string",
		"pkg p, type T struct",
		"pkg p, type T struct, Ch <-chan int",
		"pkg p, type T struct, Embedded",
		"pkg p, type T struct, Fn func(context.Context, int) error",
		"pkg p, type T struct, Name string",
		"pkg p, type T struct, Other Alias",
		"pkg p, type opts struct",
		"pkg p, type opts struct, N int",
		"pkg p, var Default *T",
		"pkg p, var ErrX error",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("listing:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if _, err := load(d, "example.com/m/none"); err == nil {
		t.Fatal("load of a missing package succeeded")
	}
}

func TestDecide(t *testing.T) {
	a, b := []string{"pkg p, func A()"}, []string{"pkg p, func A()", "pkg p, func B()"}
	for _, tc := range []struct {
		name     string
		old, cur record
		write    bool
		want     string // substring of the error; empty for none
	}{
		{"unchanged", record{"0.1", a}, record{"0.1", a}, false, ""},
		{"unchanged, write", record{"0.1", a}, record{"0.1", a}, true, ""},
		{"changed under the same version", record{"0.1", a}, record{"0.1", b}, false, "changed and pluginApi did not (still 0.1)"},
		{"the diff is shown", record{"0.1", b}, record{"0.1", a}, false, "- pkg p, func B()"},
		{"write refuses a change under the same version", record{"0.1", a}, record{"0.1", b}, true, "changed and pluginApi did not"},
		{"raised but not recorded", record{"0.1", a}, record{"0.2", b}, false, "record the API with -write"},
		{"raised and written", record{"0.1", a}, record{"0.2", b}, true, ""},
		{"raised with no API change", record{"0.1", a}, record{"0.2", a}, true, ""},
		{"lowered", record{"0.2", a}, record{"0.1", b}, true, "is not above the recorded 0.2"},
		{"minor compared as a number", record{"0.9", a}, record{"0.10", b}, true, ""},
		{"not major.minor", record{"0.1", a}, record{"0.1.1", b}, true, `"0.1.1" is not major.minor`},
		{"leading zero", record{"0.1", a}, record{"0.01", a}, true, "is not major.minor"},
		{"first record", record{}, record{"0.1", a}, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := decide(tc.old, tc.cur, tc.write)
			if (tc.want == "") != (err == nil) || err != nil && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("decide: %v, want %q", err, tc.want)
			}
		})
	}
}

func TestRecordRoundTrip(t *testing.T) {
	r := record{"0.3", []string{"pkg p, func A()", "pkg q, var B int"}}
	got, err := parse(r.format())
	if err != nil || got.version != r.version || !slices.Equal(got.lines, r.lines) {
		t.Fatalf("parse(format) = %+v, %v", got, err)
	}
	for _, bad := range []string{"", "# only a comment\n", "pkg p, func A()\n", "pluginApi\n"} {
		if _, err := parse([]byte(bad)); err == nil {
			t.Errorf("parse(%q) accepted", bad)
		}
	}
}

// The run on the repository: the committed record is the API of both packages at
// the current pluginApi. TestDecide holds the rules of a change.
func TestRunOnTheRepository(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if err := run([]string{root}); err != nil {
		t.Fatalf("the committed record is not the API: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, recordFile))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if rec.version == "" || !slices.ContainsFunc(rec.lines, func(l string) bool { return strings.HasPrefix(l, "pkg plugin, func Define[") }) ||
		!slices.ContainsFunc(rec.lines, func(l string) bool { return strings.HasPrefix(l, "pkg kernel, func Define(") }) {
		t.Fatalf("the record lacks plugin.Define or kernel.Define:\n%s", data)
	}
	if err := run(nil); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("run with no argument: %v", err)
	}
	if err := run([]string{t.TempDir()}); err == nil {
		t.Fatal("run outside the repository succeeded")
	}
}
