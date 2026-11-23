// SPDX-License-Identifier: Apache-2.0

// Package conformance is the suite of the configrefs definition: what every provider
// of configrefs.Key and the fake must do (07-c4-plugin-sdk.md §8.2, T2 "every
// provider and the fake behave the same"). The obligations half holds a provider to
// DryRun.Check over trees of ${env:} references, which every provider resolves from
// the environment of the setup. The guard half holds it to its release and its
// method set: after the provider's release the consumer it served is released, a
// read of the key fails with kernel.ErrInactive and so does Check on the value it
// was given ("No use after release", 07-c4-plugin-sdk.md §4.3); the provided value
// has no method outside the interface. Each obligation has an id, the first word of
// its case's name. The package imports the standard library, sdk/kernel and
// definitions only.
package conformance

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	configrefs "github.com/mutex-solutions/aiops-platform/sdk/def/configrefs/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Setup is what a provider resolves from: an ${env:} reference resolves when Env
// holds its name, to the value Env holds. Every name has the prefix
// launch.EnvPrefix.
type Setup struct{ Env map[string]string }

// Mount mounts a provider of configrefs.Key that resolves as s says on a root of its
// own, and returns the root and the provider's fiber, ACTIVE. The suite mounts its
// consumers on the root and releases the provider by disposing the fiber.
type Mount func(t *testing.T, s Setup) (*kernel.Root, *kernel.Fiber)

// The environment of every case: one name holds a value, the other none.
const (
	set   = launch.EnvPrefix + "CONFORMANCE_SET"
	unset = launch.EnvPrefix + "CONFORMANCE_UNSET"
	value = "conformance-value-5e1"
)

func ref(name string) launch.Ref { return launch.Ref{Scheme: "env", Name: name} }

// resolving returns new trees whose references all resolve.
func resolving() []any {
	r := ref(set)
	return []any{
		nil, true, int64(1), 1.5, "literal", r,
		map[string]any{"a": r, "b": "literal"},
		[]any{int64(1), r},
		map[string]any{"m": map[string]any{"l": []any{r, false}}},
	}
}

// failing returns new trees that each hold one reference that does not resolve,
// beside ones that do.
func failing() []any {
	u := ref(unset)
	return []any{
		u,
		map[string]any{"a": ref(set), "b": u},
		[]any{ref(set), u},
		map[string]any{"m": []any{map[string]any{"u": u}}},
	}
}

var errDecode = errors.New("conformance: decode refused")

// Run runs the suite, each case on a provider of its own.
func Run(t *testing.T, mount Mount) {
	t.Run("obligations", func(t *testing.T) {
		t.Run("configrefs.O1 a tree whose references resolve passes", func(t *testing.T) {
			dr := use(t, mount).dr
			for _, tree := range resolving() {
				if err := dr.Check(t.Context(), tree, nil); err != nil {
					t.Errorf("Check(%v) = %v", tree, err)
				}
			}
		})

		t.Run("configrefs.O2 decode gets the resolved tree and its error is the result", func(t *testing.T) {
			dr := use(t, mount).dr
			for i, tree := range resolving() {
				var got any
				err := dr.Check(t.Context(), tree, func(v any) error { got = v; return errDecode })
				if !errors.Is(err, errDecode) {
					t.Errorf("Check(%v) with a decode that refuses = %v, want its error", tree, err)
				}
				if !resolvedFrom(tree, got) {
					t.Errorf("decode of %v got %v: not the same tree with each reference resolved", tree, got)
				}
				if again := resolving()[i]; !reflect.DeepEqual(tree, again) {
					t.Errorf("Check changed the tree it was given: %v, was %v", tree, again)
				}
			}
		})

		t.Run("configrefs.O3 a reference that does not resolve fails with ErrUnresolved", func(t *testing.T) {
			dr := use(t, mount).dr
			for _, tree := range failing() {
				called := false
				err := dr.Check(t.Context(), tree, func(any) error { called = true; return nil })
				if !errors.Is(err, configrefs.ErrUnresolved) || called {
					t.Errorf("Check(%v) = %v, decode called %v; want ErrUnresolved and no decode", tree, err, called)
				}
				if err != nil && strings.Contains(err.Error(), value) {
					t.Errorf("Check(%v) names a value it read: %v", tree, err)
				}
			}
		})
	})

	t.Run("guard", func(t *testing.T) {
		t.Run("configrefs.G1 after release the key and Check fail with ErrInactive", func(t *testing.T) {
			u := use(t, mount)
			deftest.Dispose(t, u.root, u.provider)
			if err := deftest.ReadError(func() { configrefs.Key.Of(u.c) }); !errors.Is(err, kernel.ErrInactive) {
				t.Errorf("a read of the key after release = %v, want ErrInactive", err)
			}
			if err := u.dr.Check(t.Context(), ref(set), nil); !errors.Is(err, kernel.ErrInactive) {
				t.Errorf("Check after release = %v, want ErrInactive", err)
			}
		})

		t.Run("configrefs.G2 no side door", func(t *testing.T) {
			if doors := deftest.SideDoors[configrefs.DryRun](use(t, mount).dr); len(doors) > 0 {
				t.Errorf("the provided value has methods outside configrefs.DryRun: %v", doors)
			}
		})
	})
}

// resolvedFrom reports whether out is in with each reference resolved to a string:
// the same maps, lists and literals.
func resolvedFrom(in, out any) bool {
	switch x := in.(type) {
	case launch.Ref:
		_, ok := out.(string)
		return ok
	case map[string]any:
		m, ok := out.(map[string]any)
		if !ok || len(m) != len(x) {
			return false
		}
		for k, v := range x {
			if o, ok := m[k]; !ok || !resolvedFrom(v, o) {
				return false
			}
		}
		return true
	case []any:
		l, ok := out.([]any)
		if !ok || len(l) != len(x) {
			return false
		}
		for i := range x {
			if !resolvedFrom(x[i], l[i]) {
				return false
			}
		}
		return true
	}
	return in == out
}

// user is a consumer of the dry run on a mounted provider.
type user struct {
	root     *kernel.Root
	provider *kernel.Fiber
	c        *kernel.Context
	dr       configrefs.DryRun
}

func use(t *testing.T, mount Mount) *user {
	t.Helper()
	u := &user{}
	u.root, u.provider = mount(t, Setup{Env: map[string]string{set: value}})
	_, u.c, u.dr = deftest.Consume(t, u.root, configrefs.Key)
	return u
}
