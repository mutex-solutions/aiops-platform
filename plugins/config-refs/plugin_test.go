// SPDX-License-Identifier: Apache-2.0

package configrefs_test

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	configrefs "github.com/mutex-solutions/aiops-platform/plugins/config-refs"
	"github.com/mutex-solutions/aiops-platform/plugins/config-refs/internal/refs"
	refsdef "github.com/mutex-solutions/aiops-platform/sdk/def/configrefs/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

const secretValue = "s3cret-value-9f2"

// target is a row whose config the test reads back as Apply received it.
type target struct{ got atomic.Pointer[any] }

func (g *target) plugin() kernel.Plugin {
	return kernel.Define(kernel.Plugin{ID: "aiops/target", Apply: func(_ *kernel.Context, cfg any) error {
		g.got.Store(&cfg)
		return nil
	}})
}

func (g *target) config() any { return *g.got.Load() }

func ref(scheme, name string) launch.Ref { return launch.Ref{Scheme: scheme, Name: name} }

// mount mounts the launcher fake and config-refs, and returns the root.
func mount(t *testing.T, l *fake.Launcher, lp kernel.Plugin, cfg any) *kernel.Root {
	t.Helper()
	root := kerneltest.New(t)
	kerneltest.Mount(root, lp)
	f := root.Plugin(configrefs.Plugin, cfg)
	if err := f.Await(t.Context()); err != nil {
		t.Fatalf("config-refs: %v", err)
	}
	return root
}

func mountTarget(t *testing.T, root *kernel.Root, raw any) (*target, error) {
	t.Helper()
	g := &target{}
	f := root.Plugin(g.plugin(), raw)
	return g, f.Await(t.Context())
}

func TestResolvesEachScheme(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte("file-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "n"), []byte("nested"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, lp := fake.New()
	l.Environ = map[string]string{"AIOPS_REF_DB": "env-value"}
	l.Flags = map[string]string{"site": "flag-value", "refs-dir": dir}
	root := mount(t, l, lp, map[string]any{"maxFileBytes": int64(64)})

	g, err := mountTarget(t, root, map[string]any{
		"env":    ref("env", "AIOPS_REF_DB"),
		"flag":   ref("flag", "site"),
		"file":   ref("file", "token"),
		"nested": ref("file", "sub/n"),
		"keep":   "plain",
		"n":      int64(7),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"env": "env-value", "flag": "flag-value", "file": "file-value\n", "nested": "nested", "keep": "plain", "n": int64(7)}
	got := g.config().(map[string]any)
	if !maps.Equal(got, want) {
		t.Errorf("config = %v, want %v", got, want)
	}
}

// A listener of internal/config inward of config-refs runs, and what it returns is
// resolved: config-refs resolves what next returns ("aiops/z-inner" sorts after
// "aiops/config-refs", so it is inward).
func TestInwardListenersRunAndTheirResultIsResolved(t *testing.T) {
	l, lp := fake.New()
	l.Environ = map[string]string{"AIOPS_REF_A": "a", "AIOPS_REF_B": "b"}
	root := mount(t, l, lp, map[string]any{"maxFileBytes": int64(8)})
	inner := kernel.Define(kernel.Plugin{ID: "aiops/z-inner", Hooks: []kernel.HookUse{kernel.InternalConfig.Use(kernel.DefaultStage)},
		Apply: func(c *kernel.Context, _ any) error {
			kernel.InternalConfig.On(c, kernel.DefaultStage, func(_ context.Context, _ any, next kernel.Next[any]) (any, error) {
				v, err := next()
				m, ok := v.(map[string]any)
				if err != nil || !ok {
					return v, err
				}
				m = maps.Clone(m)
				m["w"] = ref("env", "AIOPS_REF_B")
				return m, nil
			})
			return nil
		}})
	kerneltest.Mount(root, inner)
	g, err := mountTarget(t, root, map[string]any{"v": ref("env", "AIOPS_REF_A")})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := g.config().(map[string]any), map[string]any{"v": "a", "w": "b"}; !maps.Equal(got, want) {
		t.Errorf("config = %v, want %v", got, want)
	}
}

func TestNestedMapsAndListsAreResolvedWithoutChangingTheRaw(t *testing.T) {
	l, lp := fake.New()
	l.Environ = map[string]string{"AIOPS_REF_A": "a", "AIOPS_REF_B": "b"}
	root := mount(t, l, lp, map[string]any{"maxFileBytes": int64(8)})
	raw := map[string]any{
		"outer": map[string]any{"list": []any{"x", ref("env", "AIOPS_REF_A"), map[string]any{"deep": ref("env", "AIOPS_REF_B")}}},
		"other": []any{"plain", int64(1)},
	}
	g, err := mountTarget(t, root, raw)
	if err != nil {
		t.Fatal(err)
	}
	list := g.config().(map[string]any)["outer"].(map[string]any)["list"].([]any)
	if list[0] != "x" || list[1] != "a" || list[2].(map[string]any)["deep"] != "b" {
		t.Errorf("list = %v", list)
	}
	if _, still := raw["outer"].(map[string]any)["list"].([]any)[1].(launch.Ref); !still {
		t.Error("the raw config was changed in place")
	}
}

// A row that needs configRefsReady is applied only once config-refs is ACTIVE, with its
// references resolved, and unloads while config-refs is gone.
func TestReadyKeyGatesTheRowsWithReferences(t *testing.T) {
	l, lp := fake.New()
	l.Environ = map[string]string{"AIOPS_REF_A": "a"}
	root := kerneltest.New(t)
	kerneltest.Mount(root, lp)
	g := &target{}
	p := g.plugin()
	p.Inject = []kernel.Dep{kernel.Need(configrefs.ReadyKey)}
	user := root.Plugin(p, map[string]any{"v": ref("env", "AIOPS_REF_A")})
	if st, _ := user.State(t.Context()); st != kernel.Pending || g.got.Load() != nil {
		t.Fatalf("before config-refs: %s, applied %v", st, g.got.Load() != nil)
	}
	refs := root.Plugin(configrefs.Plugin, map[string]any{"maxFileBytes": int64(8)})
	if err := errors.Join(refs.Await(t.Context()), user.Await(t.Context())); err != nil {
		t.Fatal(err)
	}
	if got := g.config().(map[string]any)["v"]; got != "a" {
		t.Errorf("v = %v, want the resolved value", got)
	}
	if err := refs.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := root.Settle(t.Context()); err != nil {
		t.Fatal(err)
	}
	if st, _ := user.State(t.Context()); st != kernel.Pending {
		t.Errorf("with config-refs gone the row is %s, want PENDING", st)
	}
}

// The row's dry run resolves as its listener does and returns no value: an error names
// the reference, matches the definition's ErrUnresolved and never holds what was read
// (ADR-059 decision 16).
func TestDryRunChecksWithoutReturningValues(t *testing.T) {
	l, lp := fake.New()
	l.Environ = map[string]string{"AIOPS_REF_OK": secretValue, "HOME_SECRET": secretValue}
	root := mount(t, l, lp, map[string]any{"maxFileBytes": int64(8)})
	got := make(chan refsdef.DryRun, 1)
	user := kernel.Define(kernel.Plugin{ID: "t/launcher", Inject: []kernel.Dep{kernel.Need(refsdef.Key)},
		Apply: func(c *kernel.Context, _ any) error { got <- refsdef.Key.Of(c); return nil }})
	f := root.Plugin(user, nil)
	if err := root.Settle(t.Context()); err != nil {
		t.Fatal(err)
	}
	if st, _ := f.State(t.Context()); st != kernel.Active {
		t.Fatalf("a consumer of the dry run is %s once settled, want ACTIVE: config-refs does not provide it", st)
	}
	dr := <-got
	cfg := map[string]any{"a": []any{ref("env", "AIOPS_REF_OK"), "plain"}}
	if err := dr.Check(t.Context(), cfg, nil); err != nil {
		t.Errorf("Check of a reference that resolves = %v", err)
	}
	// The row's Decode runs on the tree as its mount would see it, inside the row, and
	// its error is the result (08 §1.6, "Config fails schema or Validate").
	errDecode := errors.New("a is refused")
	var seen any
	if err := dr.Check(t.Context(), cfg, func(v any) error { seen = v; return errDecode }); err != errDecode {
		t.Errorf("Check with a Decode that refuses = %v, want its error", err)
	}
	if m, _ := seen.(map[string]any); m == nil || m["a"].([]any)[0] != secretValue {
		t.Errorf("Decode saw %v, want the resolved tree", seen)
	}
	for _, bad := range []launch.Ref{ref("secret", "x"), ref("env", "HOME_SECRET"), ref("env", "AIOPS_REF_NONE")} {
		called := false
		err := dr.Check(t.Context(), map[string]any{"a": bad}, func(any) error { called = true; return nil })
		if !errors.Is(err, refsdef.ErrUnresolved) || !strings.Contains(err.Error(), bad.Name) || strings.Contains(err.Error(), secretValue) || called {
			t.Errorf("Check(%v) = %v (Decode called %v), want ErrUnresolved, the reference, no value and no Decode", bad, err, called)
		}
	}
}

func TestConfigWithoutReferencesPassesThrough(t *testing.T) {
	l, lp := fake.New()
	root := mount(t, l, lp, map[string]any{"maxFileBytes": int64(64)})
	for _, raw := range []any{nil, "text", struct{ A int }{1}, map[string]any{"a": []any{"b"}}} {
		g, err := mountTarget(t, root, raw)
		if err != nil {
			t.Fatalf("%v: %v", raw, err)
		}
		if got := g.config(); !equal(got, raw) {
			t.Errorf("config = %v, want %v", got, raw)
		}
	}
}

func equal(a, b any) bool {
	am, aok := a.(map[string]any)
	bm, bok := b.(map[string]any)
	if aok && bok {
		return len(am) == len(bm)
	}
	return a == b
}

func TestUnresolvedReferencesFailTheRow(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "out"), []byte(secretValue), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "big"), []byte(strings.Repeat(secretValue, 10)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "out"), filepath.Join(dir, "link")); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "adir"), 0o700); err != nil {
		t.Fatal(err)
	}
	l, lp := fake.New()
	l.Environ = map[string]string{"AIOPS_REF_OK": secretValue, "HOME_SECRET": secretValue}
	l.Flags = map[string]string{"refs-dir": dir}
	cfg := map[string]any{"maxFileBytes": int64(32)}
	root := mount(t, l, lp, cfg)
	unset, unsetLp := fake.New()
	unsetRoot := mount(t, unset, unsetLp, cfg)
	empty, emptyLp := fake.New()
	empty.Flags = map[string]string{"refs-dir": ""} // the server passes "" when --refs-dir is absent
	emptyRoot := mount(t, empty, emptyLp, cfg)

	for _, tc := range []struct {
		name string
		root *kernel.Root
		ref  launch.Ref
		why  string
	}{
		{"env outside the prefix", root, ref("env", "HOME_SECRET"), "prefix AIOPS_REF_"},
		{"env not set", root, ref("env", "AIOPS_REF_NONE"), "not set"},
		{"flag not set", root, ref("flag", "nope"), "flag is not set"},
		{"secret until S3", root, ref("secret", "db-password"), "before S3"},
		{"unknown scheme", root, ref("shell", "ls"), "unknown scheme"},
		{"file missing", root, ref("file", "missing"), "does not exist"},
		{"file too large", root, ref("file", "big"), "larger than the limit of 32 bytes"},
		{"traversal", root, ref("file", "../"+filepath.Base(outside)+"/out"), "outside the file root"},
		{"absolute path", root, ref("file", filepath.Join(outside, "out")), "outside the file root"},
		{"symlink out of the root", root, ref("file", "link"), "outside the file root"},
		{"directory", root, ref("file", "adir"), "cannot be read"},
		{"file root flag empty", emptyRoot, ref("file", "x"), "the flag refs-dir that names the file root is not set"},
		{"file root flag unset", unsetRoot, ref("file", "x"), "the flag refs-dir that names the file root is not set"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &target{}
			err := tc.root.Plugin(g.plugin(), map[string]any{"a": []any{tc.ref}}).Await(t.Context())
			if !errors.Is(err, refsdef.ErrUnresolved) {
				t.Fatalf("err = %v, want ErrUnresolved", err)
			}
			var re *refs.Error
			if !errors.As(err, &re) || re.Scheme != tc.ref.Scheme || re.Name != tc.ref.Name {
				t.Errorf("err = %#v", err)
			}
			if msg := err.Error(); !strings.Contains(msg, "invalid config") || !strings.Contains(msg, tc.why) || strings.Contains(msg, secretValue) {
				t.Errorf("message %q: want %q and no value", msg, tc.why)
			}
			if g.got.Load() != nil {
				t.Error("Apply ran with a config that did not resolve")
			}
		})
	}
}

func TestUnreadableFileRootFails(t *testing.T) {
	l, lp := fake.New()
	l.Flags = map[string]string{"refs-dir": filepath.Join(t.TempDir(), "absent")}
	root := mount(t, l, lp, map[string]any{"maxFileBytes": int64(8)})
	_, err := mountTarget(t, root, map[string]any{"a": ref("file", "x")})
	if !errors.Is(err, refsdef.ErrUnresolved) || !strings.Contains(err.Error(), "file root cannot be opened") {
		t.Errorf("err = %v", err)
	}
}

func TestUpdateWithUnresolvedReferenceIsRefused(t *testing.T) {
	l, lp := fake.New()
	l.Environ = map[string]string{"AIOPS_REF_A": "a"}
	root := mount(t, l, lp, map[string]any{"maxFileBytes": int64(8)})
	g := &target{}
	f := root.Plugin(g.plugin(), map[string]any{"a": ref("env", "AIOPS_REF_A")})
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	err := f.Update(t.Context(), map[string]any{"a": ref("env", "AIOPS_REF_GONE")})
	if !errors.Is(err, refsdef.ErrUnresolved) {
		t.Fatalf("Update err = %v", err)
	}
	if st, _ := f.State(t.Context()); st != kernel.Active {
		t.Errorf("state = %s, the row must keep its instance", st)
	}
	if got := g.config().(map[string]any)["a"]; got != "a" {
		t.Errorf("config = %v", got)
	}
}

// A Decode error names the field and the type, never the value: a value may come from
// a resolved reference.
func TestDecodeErrorDoesNotEchoTheValue(t *testing.T) {
	_, lp := fake.New()
	root := kerneltest.New(t)
	kerneltest.Mount(root, lp)
	err := root.Plugin(configrefs.Plugin, map[string]any{"maxFileBytes": "hunter2-secret"}).Await(t.Context())
	if err == nil || strings.Contains(err.Error(), "hunter2") || !strings.Contains(err.Error(), "maxFileBytes") {
		t.Errorf("err = %v", err)
	}
}

func TestDecode(t *testing.T) {
	_, lp := fake.New()
	for _, tc := range []struct {
		name string
		raw  any
		ok   bool
	}{
		{"a Go struct is not a parsed tree", configrefs.Config{MaxFileBytes: 1}, false},
		{"map with int64", map[string]any{"maxFileBytes": int64(1)}, true},
		{"a Go int, which the launcher never parses", map[string]any{"maxFileBytes": 1}, false},
		{"a whole float is not an integer", map[string]any{"maxFileBytes": 2.0}, false},
		{"nil", nil, false},
		{"a string", "x", false},
		{"zero bound", map[string]any{"maxFileBytes": int64(0)}, false},
		{"negative bound", map[string]any{"maxFileBytes": int64(-1)}, false},
		{"fractional bound", map[string]any{"maxFileBytes": 1.5}, false},
		{"string bound", map[string]any{"maxFileBytes": "1"}, false},
		{"a file root flag", map[string]any{"maxFileBytes": int64(1), "fileRootFlag": "refs-dir"}, false},
		{"unknown field", map[string]any{"maxFileBytes": int64(1), "bogus": 1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// One root each: a second config-refs in a realm is a second provider of ReadyKey.
			root := kerneltest.New(t)
			kerneltest.Mount(root, lp)
			err := root.Plugin(configrefs.Plugin, tc.raw).Await(t.Context())
			if (err == nil) != tc.ok {
				t.Errorf("err = %v, want ok = %v", err, tc.ok)
			}
		})
	}
}

// boundKernel is a kernel with the three binding maps on: the launch keys are
// provider-bound and internal/config is sealed to the entries listed.
func boundKernel(t *testing.T, launcher kernel.Plugin, listeners ...kernel.Plugin) *kernel.Root {
	t.Helper()
	const n = 64
	cfg := kernel.Config{
		LoadDeadline: 10 * time.Second, DrainDeadline: 10 * time.Second, DisposeDeadline: 10 * time.Second,
		ArchiveLimit: n, EventWindow: n, ObserverQueue: n, OwnerInbox: n, TerminalLimit: n,
		Providers: kernel.Bindings{
			launch.ProfileContextKey: {launcher}, launch.CatalogKey: {launcher}, launch.CmdlineKey: {launcher},
			launch.ReadyKey: {launcher}, launch.KillSwitchesKey: {launcher}, launch.AppExitKey: {launcher},
			configrefs.ReadyKey: {configrefs.Plugin}, refsdef.Key: {configrefs.Plugin},
		},
		Consumers: kernel.Bindings{launch.AppExitKey: nil},
		Listeners: map[kernel.AnyHook][]kernel.Plugin{kernel.InternalConfig: listeners},
	}
	r, err := kernel.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(t.Context()) })
	return r
}

func TestSealedHookNeedsAListenerEntry(t *testing.T) {
	_, lp := fake.New()
	listed := boundKernel(t, lp, configrefs.Plugin)
	if err := listed.Plugin(lp, nil).Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := listed.Plugin(configrefs.Plugin, map[string]any{"maxFileBytes": int64(1)}).Await(t.Context()); err != nil {
		t.Fatalf("listed config-refs: %v", err)
	}

	unlisted := boundKernel(t, lp)
	if err := unlisted.Plugin(lp, nil).Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	err := unlisted.Plugin(configrefs.Plugin, map[string]any{"maxFileBytes": int64(1)}).Await(t.Context())
	if !errors.Is(err, kernel.ErrSealed) {
		t.Errorf("unlisted config-refs: err = %v, want ErrSealed", err)
	}
}
