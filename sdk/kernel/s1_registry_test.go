// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"errors"
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
