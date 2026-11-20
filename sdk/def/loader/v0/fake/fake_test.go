// SPDX-License-Identifier: Apache-2.0

package fake_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	loader "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// listener listens on all three hooks and records what it hears.
type listener struct {
	inits     []loader.EntryInitArgs
	volatiles []loader.VolatileUpdateArgs
	patched   []string
	plugin    kernel.Plugin
}

func newListener(id string) *listener {
	l := &listener{}
	l.plugin = kernel.Define(kernel.Plugin{
		ID: id,
		Hooks: []kernel.HookUse{loader.EntryInit.Use(kernel.DefaultStage), loader.VolatileUpdate.Use(kernel.DefaultStage),
			loader.PatchContext.Use(kernel.DefaultStage)},
		Apply: func(c *kernel.Context, _ any) error {
			loader.EntryInit.On(c, kernel.DefaultStage, func(_ context.Context, a loader.EntryInitArgs) (any, error) {
				l.inits = append(l.inits, a)
				return nil, nil
			})
			loader.VolatileUpdate.On(c, kernel.DefaultStage, func(_ context.Context, a loader.VolatileUpdateArgs) (any, error) {
				l.volatiles = append(l.volatiles, a)
				return nil, nil
			})
			loader.PatchContext.On(c, kernel.DefaultStage, func(_ context.Context, a loader.PatchContextArgs, next kernel.Next[*kernel.Context]) (*kernel.Context, error) {
				l.patched = append(l.patched, a.Entry.ID)
				return next()
			})
			return nil
		},
	})
	return l
}

func TestConsumerCallsEveryMethod(t *testing.T) {
	f, provider := fake.New()
	hear := newListener("aiops/listener")
	var patched *kernel.Context
	consumer := kernel.Define(kernel.Plugin{ID: "aiops/consumer", Inject: []kernel.Dep{kernel.Need(loader.Key)}, Apply: func(c *kernel.Context, _ any) error {
		ctx := context.Background()
		l := loader.Key.Of(c)
		spec := loader.EntrySpec{ID: "g:a", Parent: "g", Plugin: "aiops/mod", Config: 1}
		if _, ok := l.Locate("g:a"); ok {
			t.Error("Locate found an entry before Create")
		}
		if err := l.Create(ctx, spec); err != nil {
			t.Errorf("Create: %v", err)
		}
		if err := l.Create(ctx, spec); !errors.Is(err, loader.ErrExists) {
			t.Errorf("second Create: %v", err)
		}
		if st, err := l.Await(ctx, "g:a"); err != nil || st != kernel.Active {
			t.Errorf("Await = %v, %v", st, err)
		}
		spec.Config = 2
		if err := l.Update(ctx, spec); err != nil {
			t.Errorf("Update: %v", err)
		}
		if e, ok := l.Locate("g:a"); !ok || e.Spec.Config != 2 || e.State != kernel.Active {
			t.Errorf("Locate = %v, %v", e, ok)
		}
		if err := l.Remove(ctx, "g:a"); err != nil {
			t.Errorf("Remove: %v", err)
		}
		if err := l.Remove(ctx, "g:a"); !errors.Is(err, loader.ErrNotFound) {
			t.Errorf("second Remove: %v", err)
		}
		if err := l.Update(ctx, spec); !errors.Is(err, loader.ErrNotFound) {
			t.Errorf("Update of a removed entry: %v", err)
		}
		if _, err := l.Await(ctx, "g:a"); !errors.Is(err, loader.ErrNotFound) {
			t.Errorf("Await of a removed entry: %v", err)
		}
		patched = c
		return nil
	}})

	root := kerneltest.New(t)
	kerneltest.Mount(root, provider, hear.plugin, consumer)
	if len(hear.inits) != 1 || hear.inits[0].Entry.ID != "g:a" {
		t.Errorf("entry-init heard %v", hear.inits)
	}
	if len(hear.volatiles) != 1 || hear.volatiles[0].ID != "g:a" || hear.volatiles[0].Config != 2 {
		t.Errorf("volatile-update heard %v", hear.volatiles)
	}
	got, err := f.Patch(context.Background(), loader.EntrySpec{ID: "p"}, patched)
	if err != nil || got == nil || got == patched || len(hear.patched) != 1 {
		t.Errorf("Patch = %v, %v, heard %v", got, err, hear.patched)
	}
}

func TestScriptedStatesAndEntries(t *testing.T) {
	f, provider := fake.New()
	root := kerneltest.New(t)
	kerneltest.Mount(root, provider)
	ctx := context.Background()
	for _, id := range []string{"b", "a"} {
		if err := f.Create(ctx, loader.EntrySpec{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if !f.SetState("a", kernel.Failed) || f.SetState("nope", kernel.Failed) {
		t.Fatal("SetState result")
	}
	if st, _ := f.Await(ctx, "a"); st != kernel.Failed {
		t.Errorf("state = %s", st)
	}
	es := f.Entries()
	if len(es) != 2 || es[0].Spec.ID != "a" || es[1].Spec.ID != "b" {
		t.Errorf("Entries = %v", es)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := f.Await(cancelled, "a"); !errors.Is(err, context.Canceled) {
		t.Errorf("Await on a cancelled context: %v", err)
	}
}

func TestPatchWaterfallResultAndFailure(t *testing.T) {
	f, provider := fake.New()
	root := kerneltest.New(t)
	other := root.Context().Extend()
	replace := kernel.Define(kernel.Plugin{ID: "aiops/replace", Hooks: []kernel.HookUse{loader.PatchContext.Use(kernel.DefaultStage)},
		Apply: func(c *kernel.Context, _ any) error {
			loader.PatchContext.On(c, kernel.DefaultStage, func(_ context.Context, _ loader.PatchContextArgs, next kernel.Next[*kernel.Context]) (*kernel.Context, error) {
				if _, err := next(); err != nil {
					return nil, err
				}
				return other, nil
			})
			return nil
		}})
	veto := kernel.Define(kernel.Plugin{ID: "aiops/veto", Hooks: []kernel.HookUse{loader.PatchContext.Use(kernel.DefaultStage)},
		Apply: func(c *kernel.Context, _ any) error {
			loader.PatchContext.On(c, kernel.DefaultStage, func(_ context.Context, a loader.PatchContextArgs, _ kernel.Next[*kernel.Context]) (*kernel.Context, error) {
				return a.Context, nil
			})
			return nil
		}})
	boom := kernel.Define(kernel.Plugin{ID: "aiops/boom", Hooks: []kernel.HookUse{loader.PatchContext.Use(kernel.DefaultStage)},
		Apply: func(c *kernel.Context, _ any) error {
			loader.PatchContext.On(c, kernel.DefaultStage, func(context.Context, loader.PatchContextArgs, kernel.Next[*kernel.Context]) (*kernel.Context, error) {
				return nil, errors.New("boom")
			})
			return nil
		}})
	kerneltest.Mount(root, provider, replace)
	got, err := f.Patch(context.Background(), loader.EntrySpec{ID: "p"}, root.Context())
	if err != nil || got != other {
		t.Fatalf("Patch = %v, %v, want the listener's context", got, err)
	}
	// A listener that does not call next vetoes the entry.
	kerneltest.Mount(root, veto)
	if _, err := f.Patch(context.Background(), loader.EntrySpec{ID: "p"}, root.Context()); !errors.Is(err, loader.ErrPatchVetoed) {
		t.Fatalf("a listener that skips next: %v, want ErrPatchVetoed", err)
	}
	kerneltest.Mount(root, boom)
	if _, err := f.Patch(context.Background(), loader.EntrySpec{ID: "p"}, root.Context()); err == nil {
		t.Fatal("a failing listener did not fail Patch")
	}
}

// loader is consumer-bound (03-c3-kernel.md:64): it resolves only for listed entries.
func TestKeyIsConsumerBound(t *testing.T) {
	_, provider := fake.New()
	need := []kernel.Dep{kernel.Need(loader.Key)}
	listed := deftest.Plugin("aiops/listed", need, nil, nil, func(c *kernel.Context) error { _ = loader.Key.Of(c); return nil })
	stranger := deftest.Plugin("aiops/stranger", need, nil, nil, func(*kernel.Context) error { return nil })
	root := deftest.Bound(t, deftest.Config(nil, kernel.Bindings{loader.Key: {listed}}))
	deftest.Active(t, root.Plugin(provider, nil))
	deftest.Active(t, root.Plugin(listed, nil))
	if err := deftest.Failure(t, root.Plugin(stranger, nil)); !errors.Is(err, kernel.ErrBinding) {
		t.Fatalf("err = %v, want ErrBinding", err)
	}
}

// The three hooks are sealed (07-c4-plugin-sdk.md:463): under kernel.New only the
// entries listed for a hook may listen on it.
func TestHooksAreSealed(t *testing.T) {
	_, provider := fake.New()
	listed := newListener("aiops/listed")
	stranger := newListener("aiops/stranger")
	cfg := deftest.Config(nil, nil)
	cfg.Listeners = map[kernel.AnyHook][]kernel.Plugin{
		loader.PatchContext: {listed.plugin}, loader.EntryInit: {listed.plugin}, loader.VolatileUpdate: {listed.plugin},
	}
	root := deftest.Bound(t, cfg)
	deftest.Active(t, root.Plugin(provider, nil))
	deftest.Active(t, root.Plugin(listed.plugin, nil))
	if err := deftest.Failure(t, root.Plugin(stranger.plugin, nil)); !errors.Is(err, kernel.ErrSealed) {
		t.Fatalf("err = %v, want ErrSealed", err)
	}
	// Each hook alone refuses an unlisted listener.
	on := map[string]func(*kernel.Context){
		"patch-context": func(c *kernel.Context) {
			loader.PatchContext.On(c, kernel.DefaultStage, func(_ context.Context, a loader.PatchContextArgs, n kernel.Next[*kernel.Context]) (*kernel.Context, error) {
				return n()
			})
		},
		"entry-init": func(c *kernel.Context) {
			loader.EntryInit.On(c, kernel.DefaultStage, func(context.Context, loader.EntryInitArgs) (any, error) { return nil, nil })
		},
		"volatile-update": func(c *kernel.Context) {
			loader.VolatileUpdate.On(c, kernel.DefaultStage, func(context.Context, loader.VolatileUpdateArgs) (any, error) { return nil, nil })
		},
	}
	uses := map[string]kernel.HookUse{
		"patch-context": loader.PatchContext.Use(kernel.DefaultStage), "entry-init": loader.EntryInit.Use(kernel.DefaultStage),
		"volatile-update": loader.VolatileUpdate.Use(kernel.DefaultStage),
	}
	for name, register := range on {
		t.Run(name, func(t *testing.T) {
			only := deftest.Plugin("aiops/only-"+name, nil, nil, []kernel.HookUse{uses[name]}, func(c *kernel.Context) error {
				register(c)
				return nil
			})
			if err := deftest.Failure(t, root.Plugin(only, nil)); !errors.Is(err, kernel.ErrSealed) {
				t.Fatalf("err = %v, want ErrSealed", err)
			}
		})
	}
}

func TestHooksHaveNamesAndOwner(t *testing.T) {
	for name, h := range map[string]interface {
		Name() string
		Owner() kernel.AnyKey
	}{
		"loader/patch-context":   loader.PatchContext,
		"loader/entry-init":      loader.EntryInit,
		"loader/volatile-update": loader.VolatileUpdate,
	} {
		if h.Name() != name || h.Owner().Name() != "loader" {
			t.Errorf("%s: name %q owner %q", name, h.Name(), h.Owner().Name())
		}
	}
}

// With no listener the built-in step runs alone: the context it built for the entry,
// not the parent.
func TestPatchWithNoListenerReturnsTheBuiltContext(t *testing.T) {
	f, provider := fake.New()
	root := kerneltest.New(t)
	kerneltest.Mount(root, provider)
	got, err := f.Patch(context.Background(), loader.EntrySpec{ID: "p"}, root.Context())
	if err != nil || got == nil || got == root.Context() {
		t.Fatalf("Patch = %v, %v", got, err)
	}
}

// The built-in step of loader/patch-context is innermost (ADR-034 G8) and returns
// the context built for the entry, never the parent itself. A listener that calls
// next and returns the parent drops the row's isolate and intercept in the real
// loader, and a test of it on the fake sees the parent come back.
func TestPatchShowsAListenerThatDropsWhatNextBuilt(t *testing.T) {
	f, provider := fake.New()
	root := kerneltest.New(t)
	var built *kernel.Context
	l := kernel.Define(kernel.Plugin{ID: "aiops/dropper", Hooks: []kernel.HookUse{loader.PatchContext.Use(kernel.DefaultStage)},
		Apply: func(c *kernel.Context, _ any) error {
			loader.PatchContext.On(c, kernel.DefaultStage, func(_ context.Context, a loader.PatchContextArgs, next kernel.Next[*kernel.Context]) (*kernel.Context, error) {
				var err error
				if built, err = next(); a.Entry.ID == "drops" {
					return a.Context, err
				}
				return built, err
			})
			return nil
		}})
	kerneltest.Mount(root, provider, l)
	parent := root.Context()
	if got, err := f.Patch(t.Context(), loader.EntrySpec{ID: "keeps"}, parent); err != nil || got != built || got == parent {
		t.Errorf("a listener that returns what next built: Patch = %p, %v; next built %p, the parent is %p", got, err, built, parent)
	}
	if got, err := f.Patch(t.Context(), loader.EntrySpec{ID: "drops"}, parent); err != nil || got != parent || got == built {
		t.Errorf("a listener that returns the parent: Patch = %p, %v; next built %p, the parent is %p", got, err, built, parent)
	}
}
