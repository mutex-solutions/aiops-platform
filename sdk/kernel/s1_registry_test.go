// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestRegistryFindsAFiberByIDAndIsConsumerBound(t *testing.T) {
	var reg *registry
	listed := Define(Plugin{ID: "s1/lister", Inject: []Dep{Need(RegistryKey)}, Apply: func(c *Context, _ any) error {
		reg = RegistryKey.Of(c)
		return nil
	}})
	r := newBound(t, nil, Bindings{RegistryKey: {listed}})
	ctx := frontCtx(t)
	want(t, r.Plugin(listed, nil), Active)
	var count int
	var mu sync.Mutex
	victim := r.Plugin(plug("s1/victim", nil, nil, func(*Context) error { mu.Lock(); count++; mu.Unlock(); return nil }), nil)
	want(t, victim, Active)

	f, ok := reg.Fiber(victim.ID())
	if !ok || f.ID() != victim.ID() {
		t.Fatalf("Fiber(%d) = %v, %v", victim.ID(), f, ok)
	}
	// A fiber found by id restarts with the config its mount gave it.
	if err := f.Update(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.Await(ctx); err != nil {
		t.Fatal(err)
	}
	if mu.Lock(); count != 2 {
		t.Fatalf("victim applied %d times, want 2", count)
	}
	mu.Unlock()
	if err := f.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	if err := victim.Await(ctx); err != nil {
		t.Fatal(err)
	}
	if g, ok := reg.Fiber(victim.ID()); !ok {
		t.Fatal("a disposed fiber that is still archived is not found")
	} else if s, err := g.State(ctx); err != nil || s != Disposed {
		t.Fatalf("archived fiber: %s, %v", s, err)
	}
	for _, id := range []uint64{0, 1 << 40} {
		if g, ok := reg.Fiber(id); ok {
			t.Errorf("Fiber(%d) found %v", id, g)
		}
	}
	// Only the entries that the consumer map lists resolve the registry.
	if err := failure(t, r.Plugin(plug("s1/other", []Dep{Need(RegistryKey)}, nil, nil), nil)); !errors.Is(err, ErrBinding) {
		t.Fatalf("an unlisted entry needs the registry: %v", err)
	}
}

// A fiber found through the registry can restart, but cannot change its config: the
// core does not know its config cell, so every operation that carries a config is
// refused instead of reporting success and retaining nothing.
func TestRegistryFiberRefusesAConfig(t *testing.T) {
	var reg *registry
	lister := Define(Plugin{ID: "s1/lister", Inject: []Dep{Need(RegistryKey)}, Apply: func(c *Context, _ any) error {
		reg = RegistryKey.Of(c)
		return nil
	}})
	r := newKernel(t)
	ctx := frontCtx(t)
	var x cfgFixture
	want(t, r.Plugin(lister, nil), Active)
	v := r.Plugin(x.plugin("s1/v"), "v1")
	want(t, v, Active)
	f, ok := reg.Fiber(v.ID())
	if !ok {
		t.Fatal("not found")
	}
	batch := func(op BatchOp) error { _, err := r.Batch(ctx, op); return err }
	for _, c := range []struct {
		name string
		err  error
	}{
		{"Commit", f.Commit(ctx, "v2")},
		{"Update with a config", f.Update(ctx, "v3")},
		{"UpdateOpWith", batch(UpdateOpWith(f, "v4"))},
		{"CommitOp", batch(CommitOp(f, "v5"))},
	} {
		if !errors.Is(c.err, errRegistryConfig) {
			t.Errorf("%s through a registry fiber: %v, want the refusal", c.name, c.err)
		}
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	// Update without a config restarts the fiber with the config it retains, which the
	// mount's own Fiber still changes.
	if err := f.Update(ctx, nil); err != nil {
		t.Fatal(err)
	}
	want(t, v, Active)
	if err := v.Commit(ctx, "v6"); err != nil {
		t.Fatal(err)
	}
	if err := f.Update(ctx, nil); err != nil {
		t.Fatal(err)
	}
	want(t, v, Active)
	if got := x.got("s1/v"); !reflect.DeepEqual(got, []any{"v1", "v1", "v6"}) {
		t.Fatalf("applied %v", got)
	}
}
