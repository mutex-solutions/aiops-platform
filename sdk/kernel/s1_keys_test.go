// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"errors"
	"reflect"
	"testing"
)

// Tests of NeedKey and InterceptType (ADR-059 decision 2).

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
