// SPDX-License-Identifier: Apache-2.0

package deftest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Decl is what a definition package declares in Go: its keys, the hooks it owns, and
// how a fiber provides the keys that own those hooks, so that the hooks can be bound.
// The definition lint fails when a package-level key or hook of the package is not
// named in the Decl of its Agree test.
type Decl struct {
	Keys []kernel.AnyKey
	// Signed names the keys declared with kernel.Signed and the hooks whose access
	// is signed. kernel.KeyClasses reports a signed key as consumer-bound and the
	// kernel has no signed hook (ADR-060 decision 15), so the test names them.
	Signed []string
	Hooks  []Hook
	// Provide provides every key that owns one of Hooks, from the context of a fiber
	// that declares them in Provides. Nil when Hooks is empty.
	Provide func(c *kernel.Context) error
}

// Hook is one hook a definition owns, with its stages and what the test needs to
// listen on each of them and to dispatch it once. Event and Waterfall make one.
type Hook struct {
	hook   kernel.AnyHook
	owner  kernel.AnyKey
	stages []string
	use    func(stage string) kernel.HookUse
	listen func(c *kernel.Context, stage string, heard func())
	bind   func(c *kernel.Context) func(context.Context) error
}

// stagesOf is the stage list a hook is declared with: the one given to kernel.Stages,
// or kernel.DefaultStage alone when none is given.
func stagesOf(stages []string) []string {
	if len(stages) == 0 {
		return []string{kernel.DefaultStage}
	}
	return slices.Clone(stages)
}

// Event declares an event the definition owns with the stages it passes to
// kernel.Stages, none for the default stage; its dispatch is Serial, in chain order.
// A definition whose hook has stages exports the list it declares them with.
func Event[A any](e *kernel.Event[A], stages ...string) Hook {
	return Hook{hook: e, owner: e.Owner(), stages: stagesOf(stages), use: e.Use,
		listen: func(c *kernel.Context, stage string, heard func()) {
			e.On(c, stage, func(context.Context, A) (any, error) { heard(); return nil, nil })
		},
		bind: func(c *kernel.Context) func(context.Context) error {
			d := e.Bind(c)
			return func(ctx context.Context) error {
				var a A
				_, err := d.Serial(ctx, kernel.Unscoped, a)
				return err
			}
		}}
}

// Waterfall declares a waterfall the definition owns with the stages it passes to
// kernel.Stages, none for the default stage; each listener calls next.
func Waterfall[A, R any](w *kernel.Waterfall[A, R], stages ...string) Hook {
	return Hook{hook: w, owner: w.Owner(), stages: stagesOf(stages), use: w.Use,
		listen: func(c *kernel.Context, stage string, heard func()) {
			w.On(c, stage, func(_ context.Context, _ A, next kernel.Next[R]) (R, error) { heard(); return next() })
		},
		bind: func(c *kernel.Context) func(context.Context) error {
			d := w.Bind(c)
			return func(ctx context.Context) error {
				var a A
				_, err := d.Run(ctx, kernel.Unscoped, a)
				return err
			}
		}}
}

// Agree fails the test unless the definition.yaml of the working directory, the
// package directory sdk/def/<name>/v<N> of a definition test, agrees with d (ADR-060
// decision 7): the file is in its fixed layout; the major of its version is N and the
// major of every key; its keys are d's keys, and their bound classes are what
// kernel.KeyClasses reports, signed exactly for the keys d.Signed names; its hooks are
// d's hooks, each owned by a key of d, sealed exactly when kernel.HookSealed says so,
// signed exactly when d.Signed names them, and with the stages of the Go declaration
// in its order; and a listener on each stage is called in that order when the kernel
// dispatches the hook.
func Agree(t *testing.T, d Decl) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range check(t, dir, d) {
		t.Error(p)
	}
}

// check returns every disagreement between dir/definition.yaml and d.
func check(tb testing.TB, dir string, d Decl) []string {
	src, err := os.ReadFile(filepath.Join(dir, "definition.yaml"))
	if err != nil {
		return []string{err.Error()}
	}
	f, err := Parse(string(src))
	if err != nil {
		return []string{"definition.yaml: " + err.Error()}
	}
	var out []string
	bad := func(format string, a ...any) { out = append(out, fmt.Sprintf(format, a...)) }
	major, _ := Major(f.Version)
	if dirMajor, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(dir), "v")); err != nil || dirMajor != major {
		bad("version %s: its major is not the one of the directory %s", f.Version, filepath.Base(dir))
	}
	if name := filepath.Base(filepath.Dir(dir)); f.PerKey && f.Key != name {
		bad("key %q: with one class per key it names the definition, %q", f.Key, name)
	}
	var names []string
	for _, k := range d.Keys {
		names = append(names, k.Name())
		c, ok := f.Class[k.Name()]
		pb, cb, valid := kernel.KeyClasses(k)
		signed := slices.Contains(d.Signed, k.Name())
		switch {
		case !ok:
			bad("key %s: not in definition.yaml", k.Name())
		case !valid || k.Major() != major:
			bad("key %s: major %d, definition.yaml has version %s", k.Name(), k.Major(), f.Version)
		case pb != (c.Provider == "bound") || cb != (c.Consumer != "open"):
			bad("key %s: kernel.KeyClasses says provider-bound %v, consumer-bound %v; definition.yaml says %+v", k.Name(), pb, cb, c)
		case signed && c.Consumer != "signed":
			bad("key %s: declared signed in Go; definition.yaml says consumer %s", k.Name(), c.Consumer)
		case !signed && c.Consumer == "signed":
			bad("key %s: definition.yaml says consumer signed; Go declares no signed key", k.Name())
		}
	}
	for name := range f.Class {
		if !slices.Contains(names, name) {
			bad("key %s: in definition.yaml, not declared in Go", name)
		}
	}
	var hooks []string
	for _, h := range d.Hooks {
		hooks = append(hooks, h.hook.Name())
		decl, ok := f.Hooks[h.hook.Name()]
		sealed, _ := kernel.HookSealed(h.hook)
		signed := slices.Contains(d.Signed, h.hook.Name())
		switch {
		case !ok:
			bad("hook %s: not in definition.yaml", h.hook.Name())
		case h.owner == nil || !slices.Contains(names, h.owner.Name()):
			bad("hook %s: not owned by a key of the definition", h.hook.Name())
		case sealed != (decl.Access == "sealed"):
			bad("hook %s: kernel.HookSealed says %v; definition.yaml says access %s", h.hook.Name(), sealed, decl.Access)
		case signed && decl.Access != "signed":
			bad("hook %s: declared signed in Go; definition.yaml says access %s", h.hook.Name(), decl.Access)
		case !signed && decl.Access == "signed":
			bad("hook %s: definition.yaml says access signed; Go declares it not signed", h.hook.Name())
		case !slices.Equal(decl.Stages, h.stages):
			bad("hook %s: definition.yaml lists the stages %v, the Go declaration %v", h.hook.Name(), decl.Stages, h.stages)
		}
	}
	for _, name := range d.Signed {
		if !slices.Contains(names, name) && !slices.Contains(hooks, name) {
			bad("signed: %s is neither a key nor a hook of the definition", name)
		}
	}
	for name := range f.Hooks {
		if !slices.Contains(hooks, name) {
			bad("hook %s: in definition.yaml, not declared in Go", name)
		}
	}
	if len(out) == 0 && len(d.Hooks) > 0 {
		out = stageOrder(tb, f, d)
	}
	return out
}

// stageOrder mounts the owner of the hooks and one listener per stage of each, with
// plugin ids in the reverse of the stage order so that only the stage can put them in
// order, dispatches each hook once and compares the order heard with the file's.
func stageOrder(tb testing.TB, f File, d Decl) []string {
	var (
		mu    sync.Mutex
		heard = map[string][]string{}
		owned []kernel.AnyKey
		bound = make([]func(context.Context) error, len(d.Hooks))
	)
	for _, h := range d.Hooks {
		if !slices.Contains(owned, h.owner) {
			owned = append(owned, h.owner)
		}
	}
	owner := Plugin("deftest/owner", nil, owned, nil, func(c *kernel.Context) error {
		if err := d.Provide(c); err != nil {
			return err
		}
		for i, h := range d.Hooks {
			bound[i] = h.bind(c)
		}
		return nil
	})
	providers, listeners := kernel.Bindings{}, map[kernel.AnyHook][]kernel.Plugin{}
	for _, k := range owned {
		if pb, _, _ := kernel.KeyClasses(k); pb {
			providers[k] = []kernel.Plugin{owner}
		}
	}
	var mount []kernel.Plugin
	for _, h := range d.Hooks {
		stages := h.stages
		for i, stage := range stages {
			p := Plugin(fmt.Sprintf("deftest/listen-%03d-%s", len(stages)-i, h.hook.Name()), nil, nil,
				[]kernel.HookUse{h.use(stage)}, func(c *kernel.Context) error {
					h.listen(c, stage, func() { mu.Lock(); heard[h.hook.Name()] = append(heard[h.hook.Name()], stage); mu.Unlock() })
					return nil
				})
			if sealed, _ := kernel.HookSealed(h.hook); sealed {
				listeners[h.hook] = append(listeners[h.hook], p)
			}
			mount = append(mount, p)
		}
	}
	cfg := Config(providers, nil)
	cfg.Listeners = listeners
	root := Bound(tb, cfg)
	var out []string
	for _, p := range append([]kernel.Plugin{owner}, mount...) {
		fiber := root.Plugin(p, nil)
		err := fiber.Await(tb.Context())
		if st, _ := fiber.State(tb.Context()); st != kernel.Active {
			out = append(out, fmt.Sprintf("%s is %s: %v", p.ID, st, err))
		}
	}
	if len(out) > 0 {
		return out
	}
	for i, h := range d.Hooks {
		if err := bound[i](tb.Context()); err != nil {
			out = append(out, fmt.Sprintf("hook %s: dispatch: %v", h.hook.Name(), err))
		}
		if want := h.stages; !slices.Equal(heard[h.hook.Name()], want) {
			out = append(out, fmt.Sprintf("hook %s: the kernel called the stages in the order %v, definition.yaml says %v", h.hook.Name(), heard[h.hook.Name()], want))
		}
	}
	sort.Strings(out)
	return out
}
