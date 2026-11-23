// SPDX-License-Identifier: Apache-2.0

package deftest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

type service interface{}

// stages are the stages of ordered, which its declaration and its Hook both take.
var stages = []string{"a", "b", "c"}

var (
	open    = kernel.NewKey[service]("thing", 0)
	bound   = kernel.NewKey[service]("thing", 0, kernel.ProviderBound(), kernel.Signed())
	major1  = kernel.NewKey[service]("thing", 1)
	ordered = kernel.NewEvent[int]("thing/ordered", kernel.OwnedBy(bound), kernel.Stages(stages...))
	sealed  = kernel.NewWaterfall[int, int]("thing/sealed", kernel.OwnedBy(bound), kernel.Sealed())
	foreign = kernel.NewEvent[int]("thing/foreign", kernel.OwnedBy(kernel.NewKey[service]("other", 0)))
)

// file writes src as <tmp>/thing/v0/definition.yaml and returns the directory.
func file(t *testing.T, src string) string {
	dir := filepath.Join(t.TempDir(), "thing", "v0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "definition.yaml"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

const (
	thing     = "key: thing\nversion: \"0.1\"\nstability: preview\n"
	tail      = "remote: \"no\"\nsuite: 1\n"
	openCls   = "class: {provider: open, consumer: open}\n"
	bdCls     = "class: {provider: bound, consumer: signed}\n"
	hookLines = "hooks:\n  thing/ordered: {stages: [a, b, c], access: open, remote: \"no\"}\n  thing/sealed: {stages: [default], access: sealed, remote: \"no\"}\n"
)

func provideBound(c *kernel.Context) error { return bound.Provide(c, nil) }

func TestCheckAgrees(t *testing.T) {
	if p := check(t, file(t, thing+openCls+tail), Decl{Keys: []kernel.AnyKey{open}}); len(p) != 0 {
		t.Errorf("open key: %v", p)
	}
	d := Decl{Keys: []kernel.AnyKey{bound}, Signed: []string{"thing"}, Hooks: []Hook{Event(ordered, stages...), Waterfall(sealed)},
		Provide: provideBound}
	if p := check(t, file(t, thing+bdCls+tail+hookLines), d); len(p) != 0 {
		t.Errorf("bound key with hooks: %v", p)
	}
	d.Signed = append(d.Signed, "thing/ordered")
	if p := check(t, file(t, thing+bdCls+tail+strings.Replace(hookLines, "access: open", "access: signed", 1)), d); len(p) != 0 {
		t.Errorf("a signed hook: %v", p)
	}
	perKey := "key: thing\nversion: \"0.1\"\nstability: preview\nclass:\n  thing: {provider: open, consumer: open}\n" + tail
	if p := check(t, file(t, perKey), Decl{Keys: []kernel.AnyKey{open}}); len(p) != 0 {
		t.Errorf("one class per key: %v", p)
	}
}

func TestCheckFindsEachDisagreement(t *testing.T) {
	d := Decl{Keys: []kernel.AnyKey{bound}, Signed: []string{"thing"}, Hooks: []Hook{Event(ordered, stages...), Waterfall(sealed)},
		Provide: provideBound}
	signedHook := d
	signedHook.Signed = []string{"thing", "thing/ordered"}
	for _, tc := range []struct {
		name, src, want string
		decl            Decl
	}{
		{"no file", "", "no such file", Decl{}},
		{"layout", "key: thing\n", "definition.yaml: field version is missing", Decl{}},
		{"directory major", strings.Replace(thing, "0.1", "1.0", 1) + openCls + tail, "its major is not the one of the directory", Decl{}},
		{"key major", thing + openCls + tail, "major 1", Decl{Keys: []kernel.AnyKey{major1}}},
		{"class", thing + openCls + tail, "kernel.KeyClasses says provider-bound true", Decl{Keys: []kernel.AnyKey{bound}}},
		{"key missing", thing + openCls + tail, "key thing: in definition.yaml, not declared in Go", Decl{}},
		{"key unlisted", "key: other\nversion: \"0.1\"\nstability: preview\n" + openCls + tail, "key thing: not in definition.yaml", Decl{Keys: []kernel.AnyKey{open}}},
		{"per-key name", "key: other\nversion: \"0.1\"\nstability: preview\nclass:\n  thing: {provider: open, consumer: open}\n" + tail, "it names the definition", Decl{Keys: []kernel.AnyKey{open}}},
		{"hook missing", thing + bdCls + tail, "hook thing/ordered: not in definition.yaml", d},
		{"hook unlisted", thing + bdCls + tail + hookLines, "hook thing/sealed: in definition.yaml, not declared in Go",
			Decl{Keys: []kernel.AnyKey{bound}, Signed: []string{"thing"}, Hooks: []Hook{Event(ordered, stages...)}, Provide: provideBound}},
		{"foreign owner", thing + bdCls + tail + "hooks:\n  thing/foreign: {stages: [default], access: open, remote: \"no\"}\n",
			"not owned by a key of the definition", Decl{Keys: []kernel.AnyKey{bound}, Hooks: []Hook{Event(foreign)}}},
		{"seal", thing + bdCls + tail + strings.Replace(hookLines, "access: sealed", "access: signed", 1), "kernel.HookSealed says true", d},
		{"stage order", thing + bdCls + tail + strings.Replace(hookLines, "[a, b, c]", "[a, c, b]", 1), "the kernel called the stages in the order [a b c]", d},
		{"unknown stage", thing + bdCls + tail + strings.Replace(hookLines, "[a, b, c]", "[a, b, c, x]", 1), "the Go declaration [a b c]", d},
		{"stage left out", thing + bdCls + tail + strings.Replace(hookLines, "[a, b, c]", "[a, c]", 1),
			"hook thing/ordered: definition.yaml lists the stages [a c], the Go declaration [a b c]", d},
		{"one stage of three", thing + bdCls + tail + strings.Replace(hookLines, "[a, b, c]", "[b]", 1),
			"hook thing/ordered: definition.yaml lists the stages [b], the Go declaration [a b c]", d},
		{"default stage", thing + bdCls + tail + strings.Replace(hookLines, "[default]", "[a]", 1),
			"hook thing/sealed: definition.yaml lists the stages [a], the Go declaration [default]", d},
		{"signed key written bound", thing + strings.Replace(bdCls, "signed", "bound", 1) + tail + hookLines,
			"key thing: declared signed in Go; definition.yaml says consumer bound", d},
		{"bound key written signed", thing + bdCls + tail, "key thing: definition.yaml says consumer signed; Go declares no signed key",
			Decl{Keys: []kernel.AnyKey{bound}}},
		{"signed hook written open", thing + bdCls + tail + hookLines, "hook thing/ordered: declared signed in Go; definition.yaml says access open", signedHook},
		{"open hook written signed", thing + bdCls + tail + strings.Replace(hookLines, "access: open", "access: signed", 1),
			"hook thing/ordered: definition.yaml says access signed; Go declares it not signed", d},
		{"signed name unknown", thing + openCls + tail, "signed: none is a key or a hook of the definition",
			Decl{Keys: []kernel.AnyKey{open}, Signed: []string{"none"}}},
		{"signed key not consumer-bound", thing + strings.Replace(openCls, "consumer: open", "consumer: signed", 1) + tail,
			"kernel.KeyClasses says provider-bound false, consumer-bound false", Decl{Keys: []kernel.AnyKey{open}, Signed: []string{"thing"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := file(t, tc.src)
			if tc.src == "" {
				dir = t.TempDir()
			}
			p := check(t, dir, tc.decl)
			if !strings.Contains(strings.Join(p, "\n"), tc.want) {
				t.Errorf("problems %q, want one with %q", p, tc.want)
			}
		})
	}
}
