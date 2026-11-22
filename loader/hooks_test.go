// SPDX-License-Identifier: Apache-2.0

package loader

import (
	"errors"
	"slices"
	"testing"

	def "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

func TestTheThreeHooksFireWithTheirPayloads(t *testing.T) {
	tag := kernel.NewKey[[]any]("tag", 0)
	p := newProbe()
	l := newListener("t/listener")
	var seen syncMap
	tags := kernel.Define(kernel.Plugin{ID: "t/tags", Provides: []kernel.AnyKey{tag}, Apply: func(c *kernel.Context, _ any) error {
		return tag.ProvideFor(c, func(caller *kernel.Context) []any { return kernel.Intercepts[any](caller, tag) })
	}})
	user := kernel.Define(kernel.Plugin{ID: "t/user", Inject: []kernel.Dep{kernel.Need(tag)}, Apply: func(c *kernel.Context, cfg any) error {
		seen.set(cfg.(string), tag.Of(c))
		return nil
	}})
	// The listener wraps the context that the built-in step returns for one row (the
	// row's own link is already in it), keeps another row's context, fails a third,
	// and vetoes a fourth by not calling next, a fifth by returning no context.
	l.patch = func(a def.PatchContextArgs, next kernel.Next[*kernel.Context]) (*kernel.Context, error) {
		switch a.Entry.ID {
		case "patched":
			c, err := next()
			if err != nil {
				return nil, err
			}
			return c.Intercept(tag, "listener"), nil
		case "bad":
			return nil, errors.New("listener refuses")
		case "skip":
			return a.Context, nil
		case "nothing":
			_, err := next()
			return nil, err
		}
		return next()
	}
	user1, user2, bad := row("patched", "t/user"), row("kept", "t/user"), row("bad", "t/user")
	skip, nothing := row("skip", "t/user"), row("nothing", "t/user")
	user1.Config, user2.Config, bad.Config, skip.Config, nothing.Config = "patched", "kept", "bad", "skip", "nothing"
	user1.Intercept = map[string]any{"tag": "row"}
	x := newFx(t, fxOpt{plugins: plugins(user, tags, p.plugin("t/a", nil)), keys: map[string]kernel.AnyKey{"tag": tag},
		pre: []kernel.Plugin{l.plugin}, rows: []def.EntrySpec{row("tags", "t/tags"), user1, user2, bad, skip, nothing}})
	x.want("patched", kernel.Active)
	x.want("kept", kernel.Active)
	x.want("bad", kernel.Failed)
	if got := seen.get("patched"); !deepEqual(got, []any{"row", "listener"}) {
		t.Errorf("the patched context's chain: %v", got)
	}
	if got := seen.get("kept"); len(got.([]any)) != 0 {
		t.Errorf("the kept context's chain: %v", got)
	}
	if err := x.failure("bad"); !contains(err, "loader/patch-context") || !contains(err, "listener refuses") {
		t.Errorf("a failing listener: %q", err)
	}

	// entry-init follows every created entry, initial or applied, with its context.
	for _, id := range []string{"skip", "nothing"} {
		x.want(id, kernel.Failed)
		if err := x.failure(id); !contains(err, def.ErrPatchVetoed.Error()) || !contains(err, "loader/patch-context") {
			t.Errorf("%s: a vetoed entry failed with %q", id, err)
		}
		if seen.get(id) != nil {
			t.Errorf("%s: the plugin of a vetoed entry ran", id)
		}
	}
	if ids := l.initIDs(); !slices.Equal(ids, []string{"tags", "patched", "kept", "bad", "skip", "nothing"}) {
		t.Errorf("entry-init heard %v", ids)
	}
	x.apply(create(group("g")), create(child("g", "k", "t/a")), create(row("late", "t/a")))
	ids := l.initIDs()
	for _, id := range []string{"g", "g:k", "late"} {
		if !slices.Contains(ids, id) {
			t.Errorf("entry-init did not hear %s: %v", id, ids)
		}
	}
	l.mu.Lock()
	for _, in := range l.inits {
		if in.Context == nil || in.Entry.Plugin == "" {
			t.Errorf("entry-init payload %+v", in)
		}
	}
	if !slices.Contains(l.patches, "g:k") || !slices.Contains(l.patches, "late") {
		t.Errorf("patch-context heard %v", l.patches)
	}
	l.mu.Unlock()
}

func TestOnlyListedEntriesMayListen(t *testing.T) {
	ok, bad := newListener("t/listed"), newListener("t/unlisted")
	p := newProbe()
	x := newFx(t, fxOpt{bound: true, listeners: []kernel.Plugin{ok.plugin}, pre: []kernel.Plugin{ok.plugin},
		plugins: plugins(p.plugin("t/a", nil)), rows: []def.EntrySpec{row("a", "t/a")}})
	x.want("a", kernel.Active)
	err := x.root.Plugin(bad.plugin, nil).Await(t.Context())
	if !errors.Is(err, kernel.ErrSealed) {
		t.Errorf("an unlisted listener: %v", err)
	}
	if ids := ok.initIDs(); !slices.Equal(ids, []string{"a"}) {
		t.Errorf("the listed listener heard %v", ids)
	}
	if ids := bad.initIDs(); len(ids) != 0 {
		t.Errorf("the unlisted listener heard %v", ids)
	}
}

// A child that a plugin mounts from its own Apply inherits the entry of that row,
// never the loader's and never one it names for itself (07-c4-plugin-sdk.md:253, :563).
func TestChildOfARowInheritsTheRowsEntry(t *testing.T) {
	secret := kernel.NewKey[int]("secret", 0, kernel.ConsumerBound())
	prov := kernel.Define(kernel.Plugin{ID: "t/prov", Provides: []kernel.AnyKey{secret}, Apply: func(c *kernel.Context, _ any) error { return secret.Provide(c, 1) }})
	var mu syncMap
	// The row mounts an undefined child that claims the id of the allowed row.
	mounts := func(id string) kernel.Plugin {
		return kernel.Define(kernel.Plugin{ID: id, Apply: func(c *kernel.Context, _ any) error {
			f := c.Plugin(kernel.Plugin{ID: "t/allowed", Inject: []kernel.Dep{kernel.Need(secret)}, Apply: func(*kernel.Context, any) error { return nil }}, nil)
			mu.set(id, f)
			return nil
		}})
	}
	allowed, other := mounts("t/allowed"), mounts("t/other")
	g := group("g")
	g.Inject = []string{"secret"}
	x := newFx(t, fxOpt{bound: true, plugins: plugins(allowed, other), pre: []kernel.Plugin{prov},
		keys: map[string]kernel.AnyKey{"secret": secret}, bindings: kernel.Bindings{secret: {allowed}},
		rows: []def.EntrySpec{row("allowed", "t/allowed"), row("other", "t/other"), g}})
	x.want("allowed", kernel.Active)
	x.want("other", kernel.Active)
	if err := mu.get("t/allowed").(*kernel.Fiber).Await(t.Context()); err != nil {
		t.Errorf("the child of the allowed row: %v", err)
	}
	if err := mu.get("t/other").(*kernel.Fiber).Await(t.Context()); !errors.Is(err, kernel.ErrBinding) {
		t.Errorf("the child of a row that is not allowed: %v", err)
	}
	// A group is the loader's own fiber: it has the loader's entry, which the
	// distribution did not admit for the key.
	x.want("g", kernel.Failed)
}
