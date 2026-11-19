// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoProductionFileImportsUnsafe(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob: %v, %d files", err, len(files))
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			if imp.Path.Value == `"unsafe"` {
				t.Errorf("%s imports unsafe", name)
			}
		}
	}
}

func TestNoFileImportsUnsafeIsDetected(t *testing.T) {
	// The check must see an import: parse a file that has one.
	dir := t.TempDir()
	name := filepath.Join(dir, "x.go")
	if err := os.WriteFile(name, []byte("package x\n\nimport \"unsafe\"\n\nvar _ unsafe.Pointer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
	if err != nil || len(f.Imports) != 1 || f.Imports[0].Path.Value != `"unsafe"` {
		t.Fatalf("parse: %v %v", err, f)
	}
}

// entryKernel is a kernel with a consumer-bound key that only listed lists as
// allowed, a provider of it, and the Need that reads it.
type entryKernel struct {
	r      *Root
	secret Key[string]
	need   []Dep
}

func newEntryKernel(t *testing.T, listed ...Plugin) *entryKernel {
	t.Helper()
	secret := NewKey[string]("secret", 0, ConsumerBound())
	r := newBound(t, nil, Bindings{secret: listed})
	want(t, r.Plugin(plug("provider", nil, []AnyKey{secret}, func(c *Context) error { return secret.Provide(c, "s") }), nil), Active)
	return &entryKernel{r: r, secret: secret, need: []Dep{Need(secret)}}
}

func TestDefineMintsANewEntryEachTime(t *testing.T) {
	apply := func(*Context, any) error { return nil } // The one Apply value of both.
	a, b := Define(Plugin{ID: "same", Apply: apply}), Define(Plugin{ID: "same", Apply: apply})
	if a.entry == nil || b.entry == nil || a.entry == b.entry {
		t.Fatal("two Define results share an entry")
	}
	c := a // A copy keeps the token.
	if c.entry != a.entry {
		t.Fatal("a copy lost the entry")
	}
	if again := Define(a); again.entry == a.entry || again.entry == nil {
		t.Fatal("defining a defined value must mint a new entry")
	}
	if a.entry == nil || (Plugin{}).entry != nil {
		t.Fatal("Define changed its argument or a bare Plugin has an entry")
	}
	if Define(Plugin{}).entry == nil {
		t.Fatal("the zero Plugin cannot be defined")
	}
	if (*entryToken)(nil) == new(entryToken) || new(entryToken) == new(entryToken) {
		t.Fatal("tokens compare equal")
	}
}

func TestABindingAdmitsOnlyItsOwnToken(t *testing.T) {
	var reads int
	apply := func(c *Context, _ any) error { reads++; _ = c; return nil }
	admitted := Define(Plugin{ID: "x/reader", Apply: apply})
	other := Define(Plugin{ID: "x/reader", Apply: apply}) // Same ID, same Apply.

	k := newEntryKernel(t, admitted)
	admitted.Inject, other.Inject = k.need, k.need // Inject is not part of the entry.

	want(t, k.r.Plugin(admitted, nil), Active)
	copyOfAdmitted := admitted
	want(t, k.r.Plugin(copyOfAdmitted, nil), Active)
	if err := failure(t, k.r.Plugin(other, nil)); !errors.Is(err, ErrBinding) {
		t.Fatalf("the other Define of the same Apply: %v", err)
	}
	if reads != 2 {
		t.Fatalf("Apply ran %d times, want 2", reads)
	}
	// Defining the admitted value again is a new entry: not admitted either.
	if err := failure(t, k.r.Plugin(Define(admitted), nil)); !errors.Is(err, ErrBinding) {
		t.Fatalf("a redefined value: %v", err)
	}
}

func TestAHandBuiltPluginCopyingAnIDGainsNothing(t *testing.T) {
	admitted := plug("aiops/approval", nil, nil, nil)
	k := newEntryKernel(t, admitted)
	forged := Plugin{ID: admitted.ID, Inject: k.need, Apply: admitted.Apply}
	// Mounted by the launcher it has the launcher's entry, which no map lists.
	if err := failure(t, k.r.Plugin(forged, nil)); !errors.Is(err, ErrBinding) {
		t.Fatalf("forged from the root: %v", err)
	}
	// Mounted from an Apply it has that entry: here a plugin that is not listed.
	var child *Fiber
	parent := k.r.Plugin(plug("stranger", nil, nil, func(c *Context) error { child = c.Plugin(forged, nil); return nil }), nil)
	want(t, parent, Active)
	if err := failure(t, child); !errors.Is(err, ErrBinding) {
		t.Fatalf("forged from a stranger: %v", err)
	}
}

func TestAChildMountedFromApplyInheritsTheMountingEntry(t *testing.T) {
	var read string
	var undefined, defined *Fiber
	secret := NewKey[string]("secret", 0, ConsumerBound())
	child := Plugin{ID: "aiops/approval", Inject: []Dep{Need(secret)}, Apply: func(c *Context, _ any) error { read = secret.Of(c); return nil }}
	parent := Define(Plugin{ID: "x/parent", Apply: func(c *Context, _ any) error {
		undefined = c.Plugin(child, nil)       // Never defined: inherits the parent's entry.
		defined = c.Plugin(Define(child), nil) // Defined: has an entry of its own, which no map lists.
		return nil
	}})
	r := newBound(t, nil, Bindings{secret: {parent}})
	want(t, r.Plugin(plug("provider", nil, []AnyKey{secret}, func(c *Context) error { return secret.Provide(c, "s") }), nil), Active)
	want(t, r.Plugin(parent, nil), Active)
	want(t, undefined, Active)
	if read != "s" {
		t.Fatalf("inherited child read %q", read)
	}
	if err := failure(t, defined); !errors.Is(err, ErrBinding) {
		t.Fatalf("defined unlisted child: %v", err)
	}
	if r.ctx.Extend().entry != r.host {
		t.Fatal("Extend lost the entry")
	}
}
