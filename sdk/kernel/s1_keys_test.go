// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"errors"
	"reflect"
	"testing"
)

// Tests of NeedKey and InterceptType (ADR-059 decision 2), and of KeyClasses and
// HookSealed (ADR-059 decision 20).

func TestNeedKeyIsNeedOfAKeyHeldAsAnyKey(t *testing.T) {
	k := NewKey[int]("s1.needkey", 1)
	if d := NeedKey(k); d.Key().anyKey() != k.k || d.Optional() {
		t.Fatalf("NeedKey(k) = %+v", d)
	}
	if NeedKey(nil).Key() != nil || NeedKey(Key[int]{}).Key() != nil {
		t.Fatal("a nil or zero key gives a Dep with a key")
	}
	r := newKernel(t)
	got := make(chan int, 1)
	want(t, r.Plugin(plug("s1/prov", nil, []AnyKey{k}, func(c *Context) error { return k.Provide(c, 7) }), nil), Active)
	want(t, r.Plugin(plug("s1/cons", []Dep{NeedKey(AnyKey(k))}, nil, func(c *Context) error { got <- k.Of(c); return nil }), nil), Active)
	if v := <-got; v != 7 {
		t.Fatalf("consumer read %d", v)
	}
	if err := failure(t, r.Plugin(plug("s1/nil", []Dep{NeedKey(nil)}, nil, nil), nil)); !errors.Is(err, ErrKey) {
		t.Fatalf("NeedKey(nil): %v", err)
	}
}

func TestInterceptTypeReportsTheDeclaredConfigType(t *testing.T) {
	typed := NewKey[int]("s1.typed", 1, InterceptConfig[string]())
	if ty, ok := InterceptType(typed); !ok || ty != reflect.TypeFor[string]() {
		t.Fatalf("InterceptType(typed) = %v, %v", ty, ok)
	}
	for name, k := range map[string]AnyKey{"untyped": NewKey[int]("s1.untyped", 1), "nil": nil, "zero": Key[int]{}} {
		if ty, ok := InterceptType(k); ok || ty != nil {
			t.Errorf("InterceptType(%s) = %v, %v", name, ty, ok)
		}
	}
}

// KeyClasses reports the classes NewKey declared (ADR-059 decision 20), and they
// are the classes New enforces: a binding map may list a key exactly on a side
// where it is bound.
func TestKeyClassesReportsTheDeclaredClasses(t *testing.T) {
	entry := Define(Plugin{ID: "s1/entry"})
	for _, c := range []struct {
		name               string
		k                  AnyKey
		provider, consumer bool
	}{
		{"open", NewKey[int]("s1.open", 0), false, false},
		{"provider-bound", NewKey[int]("s1.pb", 0, ProviderBound()), true, false},
		{"consumer-bound", NewKey[int]("s1.cb", 0, ConsumerBound()), false, true},
		{"signed", NewKey[int]("s1.signed", 0, Signed()), false, true},
		{"both", NewKey[int]("s1.both", 0, ProviderBound(), Signed()), true, true},
		{"registry", RegistryKey, false, true},
	} {
		p, cons, ok := KeyClasses(c.k)
		if !ok || p != c.provider || cons != c.consumer {
			t.Errorf("KeyClasses(%s) = %v, %v, %v", c.name, p, cons, ok)
		}
		for side, bound := range map[string]bool{"provider": p, "consumer": cons} {
			cfg := frontConfig()
			if side == "provider" {
				cfg.Providers = Bindings{c.k: {entry}}
			} else {
				cfg.Consumers = Bindings{c.k: {entry}}
			}
			r, err := New(cfg)
			if (err == nil) != bound {
				t.Errorf("%s key listed by the %s binding map: New = %v, KeyClasses says bound %v", c.name, side, err, bound)
			}
			if r != nil {
				_ = r.Close(frontCtx(t))
			}
		}
	}
	for name, k := range map[string]AnyKey{"nil": nil, "zero": Key[int]{}} {
		if p, cons, ok := KeyClasses(k); p || cons || ok {
			t.Errorf("KeyClasses(%s) = %v, %v, %v", name, p, cons, ok)
		}
	}
}

// HookSealed reports whether a hook is sealed (ADR-059 decision 20): declared with
// Sealed, or a hook of the kernel. Config.Listeners may list exactly those.
func TestHookSealedReportsTheDeclaredSeal(t *testing.T) {
	owner := NewKey[int]("s1.owner", 0)
	for _, c := range []struct {
		name   string
		h      AnyHook
		sealed bool
	}{
		{"open event", NewEvent[int]("s1/open", OwnedBy(owner)), false},
		{"sealed event", NewEvent[int]("s1/sealed", OwnedBy(owner), Sealed()), true},
		{"sealed waterfall", NewWaterfall[int, int]("s1/sealed-waterfall", OwnedBy(owner), Sealed()), true},
		{"kernel event", InternalStatus, true},
		{"kernel waterfall", InternalCommit, true},
	} {
		sealed, ok := HookSealed(c.h)
		if !ok || sealed != c.sealed {
			t.Errorf("HookSealed(%s) = %v, %v", c.name, sealed, ok)
		}
		cfg := frontConfig()
		cfg.Listeners = map[AnyHook][]Plugin{c.h: {Define(Plugin{ID: "s1/listener"})}}
		r, err := New(cfg)
		if (err == nil) != sealed {
			t.Errorf("%s listed by Config.Listeners: New = %v, HookSealed says sealed %v", c.name, err, sealed)
		}
		if r != nil {
			_ = r.Close(frontCtx(t))
		}
	}
	for name, h := range map[string]AnyHook{"nil": nil, "nil event": (*Event[int])(nil), "zero event": &Event[int]{}} {
		if sealed, ok := HookSealed(h); sealed || ok {
			t.Errorf("HookSealed(%s) = %v, %v", name, sealed, ok)
		}
	}
}
