// SPDX-License-Identifier: Apache-2.0

package deftest

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The helpers below are shared by the conformance suites of the definitions
// (07-c4-plugin-sdk.md §8.2), which import the standard library, sdk/kernel and
// definitions only.

// RecordVar names the directory into which Ran records each suite that ran: the
// release build and step 5 of plugin check set it, and attest exactly what was
// recorded (ADR-060 decisions 18 and 50).
const RecordVar = "AIOPS_SUITE_RECORD"

// Ran records, when RecordVar names a directory, that the suite of definition ran the
// halves on t: once t and its subtests end with no skip, a file of that directory holds
// a JSON list of the working directory of the test binary, which go test sets to the
// directory of the package under test, the definition and the halves. A suite that
// failed ran too, so step 5 names the half that failed; its failed test fails go test,
// and nothing attests it.
func Ran(t *testing.T, definition string, halves ...string) {
	dir := os.Getenv(RecordVar)
	if dir == "" {
		return
	}
	t.Cleanup(func() {
		wd, err := os.Getwd()
		if t.Skipped() || err != nil {
			return
		}
		f, err := os.CreateTemp(dir, "ran-*.json")
		if err == nil {
			err = json.NewEncoder(f).Encode(append([]string{wd, definition}, halves...))
			err = errors.Join(err, f.Close())
		}
		if err != nil {
			t.Errorf("the record of the suite of %s: %v", definition, err)
		}
	})
}

// Half is the half of a registry's suite that runs on a dispatcher, guard, or on a
// contribution, obligations.
func Half(dispatcher bool) string {
	if dispatcher {
		return "guard"
	}
	return "obligations"
}

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
