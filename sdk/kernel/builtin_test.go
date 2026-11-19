// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

var builtinNames = []string{"hooks", "logger", "reflect", "registry"}

// recordHandler is a slog.Handler that keeps what it is given.
type recordHandler struct {
	mu    *sync.Mutex
	attrs []slog.Attr
	out   *[]string
}

func newRecordHandler() *recordHandler {
	return &recordHandler{mu: new(sync.Mutex), out: new([]string)}
}

func (h *recordHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	line := r.Message
	for _, a := range h.attrs {
		line += " " + a.String()
	}
	r.Attrs(func(a slog.Attr) bool { line += " " + a.String(); return true })
	*h.out = append(*h.out, line)
	return nil
}

func (h *recordHandler) WithAttrs(as []slog.Attr) slog.Handler {
	return &recordHandler{mu: h.mu, out: h.out, attrs: append(slices.Clone(h.attrs), as...)}
}

func (h *recordHandler) WithGroup(string) slog.Handler { return h }

func (h *recordHandler) lines() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(*h.out)
}

func TestTheRootProvidesExactlyTheFourBuiltIns(t *testing.T) {
	r := newKernel(t)
	view, err := r.core.inspect(frontCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range view.Provisions {
		if !p.Active || p.Name != "root" {
			t.Errorf("provision of %s: active %v", p.Name, p.Active)
		}
		names = append(names, p.Key.(*address).key.name)
	}
	slices.Sort(names)
	if !slices.Equal(names, builtinNames) {
		t.Fatalf("root provisions %v, want %v", names, builtinNames)
	}
	for _, k := range []AnyKey{ReflectKey, RegistryKey, HooksKey, LoggerKey} {
		if k.Major() != 0 || !slices.Contains(builtinNames, k.Name()) || k.anyKey().provider != classOpen || k.anyKey().consumer != classOpen {
			t.Errorf("key %s@%d is not an open key of major 0 named by the contract", k.Name(), k.Major())
		}
	}
}

func TestNoFifthServiceNameIsDeclaredInTheKernel(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			fn := call.Fun
			if ix, ok := fn.(*ast.IndexExpr); ok {
				fn = ix.X
			}
			if id, ok := fn.(*ast.Ident); !ok || id.Name != "NewKey" {
				return true
			}
			if lit, ok := call.Args[0].(*ast.BasicLit); ok {
				s, _ := strconv.Unquote(lit.Value)
				found = append(found, s)
			} else {
				t.Errorf("%s: NewKey with a name that is not a literal", fset.Position(call.Pos()))
			}
			return true
		})
	}
	slices.Sort(found)
	if !slices.Equal(found, builtinNames) {
		t.Fatalf("service names declared in the kernel: %v, want %v", found, builtinNames)
	}
}

func TestAPluginMayNeedEveryBuiltInAndNoneMayProvideOne(t *testing.T) {
	h := newRecordHandler()
	cfg := frontConfig()
	cfg.LogHandler = h
	r := NewForTest(t, cfg)
	var log Logger
	var got []any
	f := r.Plugin(plug("x/user", []Dep{Need(ReflectKey), Need(RegistryKey), Need(HooksKey), Need(LoggerKey)}, nil, func(c *Context) error {
		got = []any{ReflectKey.Of(c), RegistryKey.Of(c), HooksKey.Of(c)}
		log = LoggerKey.Of(c)
		log.For(c).Info("hello", "n", 1)
		log.For(nil).Info("bare")
		return nil
	}), nil)
	want(t, f, Active)
	if len(got) != 3 || got[0] == nil || got[1] == nil || got[2] == nil {
		t.Fatalf("services %v", got)
	}
	lines := h.lines()
	if len(lines) != 2 || lines[0] != "hello plugin=x/user n=1" || lines[1] != "bare" {
		t.Fatalf("logged %q", lines)
	}

	// A plugin cannot provide a key the root provides.
	var perr error
	thief := r.Plugin(plug("x/thief", nil, []AnyKey{LoggerKey}, func(c *Context) error {
		perr = LoggerKey.Provide(c, log)
		return perr
	}), nil)
	var ie *InjectError
	if err := failure(t, thief); !errors.As(err, &ie) || !errors.As(perr, &ie) {
		t.Fatalf("a plugin provided the logger: %v", err)
	}
	// Nor can the root install them twice.
	if err := r.installBuiltins(nil); err == nil {
		t.Fatal("the built-ins were installed twice")
	}
}

func TestADiscardingLoggerWorksWithoutAHandler(t *testing.T) {
	r := newKernel(t)
	var done bool
	want(t, r.Plugin(plug("x/quiet", []Dep{Need(LoggerKey)}, nil, func(c *Context) error {
		LoggerKey.Of(c).For(c).Info("dropped")
		done = true
		return nil
	}), nil), Active)
	if !done {
		t.Fatal("Apply did not run")
	}
}

func TestNewFailsAndStopsTheKernelWhenTheBuiltInsCannotBeInstalled(t *testing.T) {
	boom := errors.New("refused")
	old := installBuiltins
	installBuiltins = func(*Root, slog.Handler) error { return boom }
	t.Cleanup(func() { installBuiltins = old })
	if r, err := New(frontConfig()); !errors.Is(err, boom) || r != nil {
		t.Fatalf("New = %v, %v", r, err)
	}
}
