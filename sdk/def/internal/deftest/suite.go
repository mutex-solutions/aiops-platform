// SPDX-License-Identifier: Apache-2.0

package deftest

import (
	"reflect"
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The helpers below are shared by the conformance suites of the definitions
// (07-c4-plugin-sdk.md §8.2), which import the standard library, sdk/kernel and
// definitions only.

// Consume mounts on root a fiber that needs k and returns the fiber, its context and
// the value k resolved to. The fiber must be ACTIVE.
func Consume[T any](tb testing.TB, root *kernel.Root, k kernel.Key[T]) (*kernel.Fiber, *kernel.Context, T) {
	tb.Helper()
	var c *kernel.Context
	var v T
	f := root.Plugin(kernel.Define(kernel.Plugin{ID: "conformance/consumer", Inject: []kernel.Dep{kernel.Need(k)},
		Apply: func(cc *kernel.Context, _ any) error { c, v = cc, k.Of(cc); return nil }}), nil)
	Active(tb, f)
	return f, c, v
}

// Dispose disposes f and waits until it and the fibers its release unloads have
// settled. A FAILED fiber elsewhere in root is not an error here.
func Dispose(tb testing.TB, root *kernel.Root, f *kernel.Fiber) {
	tb.Helper()
	if err := f.Dispose(tb.Context()); err != nil {
		tb.Fatal(err)
	}
	_ = f.Await(tb.Context()) // Dispose does not wait
	_ = root.Settle(tb.Context())
}

// ReadError runs read and returns the error it panics with, or nil when it returns:
// Key.Of reports a refused read, ErrInactive included, by a panic of an *InjectError.
func ReadError(read func()) (err error) {
	defer func() {
		if p := recover(); p != nil {
			var ok bool
			if err, ok = p.(error); !ok {
				panic(p)
			}
		}
	}()
	read()
	return nil
}

// SideDoors returns the exported methods of v's dynamic type that the interface I
// does not declare. The rule "No side door" wants none: the method set of a provided
// value is a subset of the definition's interface (07-c4-plugin-sdk.md §4.3).
func SideDoors[I any](v any) []string {
	iface, t := reflect.TypeFor[I](), reflect.TypeOf(v)
	var out []string
	for i := range t.NumMethod() {
		if _, ok := iface.MethodByName(t.Method(i).Name); !ok {
			out = append(out, t.Method(i).Name)
		}
	}
	return out
}
