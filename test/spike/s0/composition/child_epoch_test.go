// SPDX-License-Identifier: Apache-2.0

package composition_test

import (
	"context"
	"errors"
	"testing"

	co "github.com/mutex-solutions/aiops-platform/test/spike/s0/composition"
	cc "github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
	cm "github.com/mutex-solutions/aiops-platform/test/spike/s0/contextmodel"
)

// In the oracle a fiber's epoch lists only its own inject. A child reaches its
// parent's committed injection by walking up the fiber chain within one realm,
// without owning it: it is not a dependant of the service and its epoch has no
// binding for it. The native needs/epoch must say the same, while the child
// still reads the inherited value and the cross-realm refusal stays.
func TestChildEpochListsOnlyOwnDependencies(t *testing.T) {
	both(t, func(t *testing.T, ctx context.Context, backend cc.Runtime) {
		entry := cm.NewEntry("unit")
		inherited := cm.NewKey[string]("inherited", 1)
		own := cm.NewKey[string]("own", 1)
		r := root(t, backend, cm.RootSpec{Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{inherited.Any(), own.Any()}})
		await(t, ctx, start(t, ctx, r, r.Context(), entry, co.Spec{Metadata: cm.Spec{ID: "inherited-source", Provides: []cm.AnyKey{inherited.Any()}},
			Apply: func(c *co.Context) (cc.Disposer, error) { return nil, co.Provide(c, inherited, "outer", nil) }}))
		await(t, ctx, start(t, ctx, r, r.Context(), entry, co.Spec{Metadata: cm.Spec{ID: "own-source", Provides: []cm.AnyKey{own.Any()}},
			Apply: func(c *co.Context) (cc.Disposer, error) { return nil, co.Provide(c, own, "mine", nil) }}))

		reads := map[string]chan error{"plain": make(chan error, 1), "own": make(chan error, 1), "private": make(chan error, 1)}
		values := map[string]chan string{"plain": make(chan string, 1), "own": make(chan string, 1)}
		readInherited := func(name string) func(*co.Context) (cc.Disposer, error) {
			return func(child *co.Context) (cc.Disposer, error) {
				v, err := co.Resolve(child, inherited)
				if values[name] != nil {
					values[name] <- v
				}
				reads[name] <- err
				return nil, nil
			}
		}
		var plain, withOwn, private *co.Fiber
		parent := start(t, ctx, r, r.Context(), entry, co.Spec{Metadata: cm.Spec{ID: "parent", Inject: []cm.Dep{cm.Need(inherited)}}, Apply: func(c *co.Context) (cc.Disposer, error) {
			var err error
			if plain, err = c.Child(co.Spec{Metadata: cm.Spec{ID: "plain-child"}, Apply: readInherited("plain")}); err != nil {
				return nil, err
			}
			if withOwn, err = c.Child(co.Spec{Metadata: cm.Spec{ID: "own-child", Inject: []cm.Dep{cm.Need(own)}}, Apply: readInherited("own")}); err != nil {
				return nil, err
			}
			realm, err := c.Isolate(inherited.Any())
			if err != nil {
				return nil, err
			}
			private, err = realm.Child(co.Spec{Metadata: cm.Spec{ID: "private-child"}, Apply: readInherited("private")})
			return nil, err
		}})
		await(t, ctx, parent)
		for _, f := range []*co.Fiber{plain, withOwn, private} {
			await(t, ctx, f)
		}
		for _, name := range []string{"plain", "own"} {
			if err, v := <-reads[name], <-values[name]; err != nil || v != "outer" {
				t.Fatalf("%s child lost the inherited read: %q %v", name, v, err)
			}
		}
		if err := <-reads["private"]; !errors.Is(err, cm.ErrUndeclared) {
			t.Fatalf("cross-realm read = %v, want undeclared", err)
		}
		snapshots, err := backend.Snapshots(ctx)
		if err != nil {
			t.Fatal(err)
		}
		byID := map[uint64]cc.Snapshot{}
		for _, s := range snapshots {
			byID[s.ID] = s
		}
		if s := byID[plain.ID]; s.State != cc.Active || len(s.Epoch) != 0 {
			t.Fatalf("child with no declaration of its own has epoch %v in %s", s.Epoch, s.State)
		}
		if s := byID[withOwn.ID]; s.State != cc.Active || len(s.Epoch) != 1 {
			t.Fatalf("a child's own declaration must be its only epoch entry: %v in %s", s.Epoch, s.State)
		}
		if s := byID[private.ID]; s.State != cc.Active || len(s.Epoch) != 0 {
			t.Fatalf("private child epoch = %v in %s", s.Epoch, s.State)
		}
		if s := byID[parent.ID]; len(s.Epoch) != 1 {
			t.Fatalf("parent epoch = %v", s.Epoch)
		}
	})
}
