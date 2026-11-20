// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"weak"
)

// gcBlob is a provided value that the collector can track.
type gcBlob struct{ pad [128]byte }

// gcProvider is a plugin body that provides a fresh gcBlob per activation and
// records a weak pointer to each, so a test holds no strong reference of its own.
type gcProvider struct {
	mu    sync.Mutex
	weaks []weak.Pointer[gcBlob]
}

func (g *gcProvider) apply(key Key[*gcBlob]) func(*Context) error {
	return func(c *Context) error {
		v := &gcBlob{}
		g.mu.Lock()
		g.weaks = append(g.weaks, weak.Make(v))
		g.mu.Unlock()
		return key.Provide(c, v)
	}
}

func (g *gcProvider) weak(i int) weak.Pointer[gcBlob] {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.weaks[i]
}

// collected runs the collector, a bounded number of times, until w is cleared.
func collected(w weak.Pointer[gcBlob]) bool {
	for range 100 {
		runtime.GC()
		if w.Value() == nil {
			return true
		}
		runtime.Gosched()
	}
	return false
}

// belowWindow fails the test when the ledger already wrapped: the value would then
// be gone for a reason that has nothing to do with the rule under test.
func belowWindow(t *testing.T, r *Root) {
	t.Helper()
	if st := r.RetentionStatus(); st.EventsTotal >= uint64(st.EventWindow) {
		t.Fatalf("%d events passed the window %d: the test would prove nothing", st.EventsTotal, st.EventWindow)
	}
}

func TestProvidedValueIsCollectableOnceItsProvisionEnded(t *testing.T) {
	for _, withConsumer := range []bool{false, true} {
		t.Run(fmt.Sprintf("consumer=%v", withConsumer), func(t *testing.T) {
			r := viewKernel(t, nil)
			ctx := frontCtx(t)
			key := NewKey[*gcBlob]("blob", 0)
			g := &gcProvider{}
			provider := r.Plugin(plug("p/provider", nil, []AnyKey{key}, g.apply(key)), nil)
			want(t, provider, Active)
			if withConsumer {
				want(t, r.Plugin(plug("p/consumer", []Dep{Need(key)}, nil, func(c *Context) error { _ = key.Of(c); return nil }), nil), Active)
			}
			if err := provider.Dispose(ctx); err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			belowWindow(t, r)
			if !collected(g.weak(0)) {
				t.Fatal("the provided value is still reachable after its provision ended")
			}
			if _, err := r.Events(ctx); err != nil {
				t.Fatal(err) // The kernel is alive and still serves its ledger.
			}
		})
	}
}

func TestProvidedValueOfAnEndedActivationIsCollectableAfterRestart(t *testing.T) {
	r := viewKernel(t, nil)
	ctx := frontCtx(t)
	key := NewKey[*gcBlob]("blob", 0)
	g := &gcProvider{}
	provider := r.Plugin(plug("p/provider", nil, []AnyKey{key}, g.apply(key)), nil)
	want(t, provider, Active)
	if err := provider.Update(ctx, nil); err != nil {
		t.Fatal(err)
	}
	want(t, provider, Active)
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	belowWindow(t, r)
	if !collected(g.weak(0)) {
		t.Fatal("the previous activation's value is still reachable after the restart")
	}
	live := g.weak(1).Value()
	if live == nil {
		t.Fatal("the live activation's value was collected")
	}
	view, err := r.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var reported int
	for _, p := range view.Provisions {
		if p.Key == "blob@0" {
			reported++
			if p.Value != any(live) {
				t.Errorf("the live provision reports %v, want its value", p.Value)
			}
		}
	}
	if reported != 1 {
		t.Errorf("Inspect lists %d provisions of the key, want 1", reported)
	}
	runtime.KeepAlive(live)
}

func TestProvidedValueOfAFailedActivationIsCollectable(t *testing.T) {
	r := viewKernel(t, nil)
	ctx := frontCtx(t)
	key := NewKey[*gcBlob]("blob", 0)
	g := &gcProvider{}
	apply := g.apply(key)
	provider := r.Plugin(plug("p/provider", nil, []AnyKey{key}, func(c *Context) error {
		if err := apply(c); err != nil {
			return err
		}
		return errors.New("startup failed")
	}), nil)
	if err := provider.Await(ctx); err == nil { // The fiber stays FAILED; its provision was withdrawn.
		t.Fatal("startup must fail")
	}
	_ = r.Settle(ctx) // Reports the failed fiber.
	belowWindow(t, r)
	if !collected(g.weak(0)) {
		t.Fatal("the value of a failed activation is still reachable")
	}
}

func TestNoEventAndNoObserverCarriesAProvidedValue(t *testing.T) {
	// The type itself cannot carry a value: every field is a number or a string.
	typ := reflect.TypeFor[LedgerEvent]()
	for f := range typ.Fields() {
		switch f.Type.Kind() {
		case reflect.Uint64, reflect.String:
		default:
			t.Errorf("LedgerEvent.%s is a %s: an event holds the key and the label only", f.Name, f.Type)
		}
	}
	r := viewKernel(t, nil)
	ctx := frontCtx(t)
	var mu sync.Mutex
	var seen []LedgerEvent
	if _, err := r.Observe("collect", func(_ context.Context, e LedgerEvent) error {
		mu.Lock()
		seen = append(seen, e)
		mu.Unlock()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	key := NewKey[*gcBlob]("blob", 0)
	g := &gcProvider{}
	provider := r.Plugin(plug("p/provider", nil, []AnyKey{key}, g.apply(key)), nil)
	want(t, provider, Active)
	want(t, r.Plugin(plug("p/consumer", []Dep{Need(key)}, nil, nil), nil), Active)
	if err := r.Notify(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := provider.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.AwaitObservers(ctx); err != nil {
		t.Fatal(err)
	}
	events, err := r.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	services := 0
	for _, e := range append(events, seen...) {
		if e.Kind == "service" && e.Key == "blob@0" {
			services++
		}
		if s := fmt.Sprintf("%#v", e); strings.Contains(s, "gcBlob") {
			t.Errorf("event %s mentions the value", s)
		}
	}
	if services == 0 || len(seen) == 0 {
		t.Fatalf("saw %d service events and %d observed events, want some", services, len(seen))
	}
}
