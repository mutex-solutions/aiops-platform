// SPDX-License-Identifier: Apache-2.0

// Package conformance is the suite of the reload definition: what every provider of
// reload.Key and the fake must do (07-c4-plugin-sdk.md §8.2, T2 "every provider and
// the fake behave the same"). The obligations half holds a provider to Submit, Result
// and Await on a tree whose composition does not change, and, from 0.2, to results
// that the wire format of reload-socket carries unchanged (ADR-062 decision 7). The
// guard half holds it to
// its release and its method set: after the provider's release the consumer it
// served is released, a read of the key fails with kernel.ErrInactive, Submit on the
// value it was given fails with ErrClosed, which matches kernel.ErrInactive, and the
// results of final operations stay readable; the provided value has no method
// outside the interface. Each obligation has an id, the first word of its case's
// name. The package imports the standard library, sdk/kernel and definitions only.
package conformance

import (
	"errors"
	"reflect"
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Mount mounts a provider of reload.Key on a root of its own, over a tree whose
// composition does not change, so that every operation passes, and returns the root
// and the provider's fiber, ACTIVE. The suite mounts its consumers on the root and
// releases the provider by disposing the fiber.
type Mount func(t *testing.T) (*kernel.Root, *kernel.Fiber)

var op = reload.Op{Source: "conformance"}

// Run runs the suite, each case on a provider of its own.
func Run(t *testing.T, mount Mount) {
	rec := deftest.Ran(t, "reload", "obligations", "guard")
	rec.Run(t, "obligations", func(t *testing.T) {
		rec.Run(t, "reload.O1 ids count from one", func(t *testing.T) {
			u := use(t, mount)
			for want := reload.ID(1); want <= 3; want++ {
				if id := u.submit(); id != want {
					t.Errorf("Submit = %d, want %d", id, want)
				}
			}
		})

		rec.Run(t, "reload.O2 result and await answer for a submitted operation", func(t *testing.T) {
			u := use(t, mount)
			id := u.submit()
			if r, ok := u.rl.Result(id); !ok || r.ID != id || r.Op != op {
				t.Errorf("Result(%d) = %+v, %v", id, r, ok)
			}
			r, err := u.rl.Await(t.Context(), id)
			if err != nil || r.ID != id || r.Op != op || r.Status != reload.Passed {
				t.Fatalf("Await(%d) = %+v, %v; want it passed", id, r, err)
			}
			if again, ok := u.rl.Result(id); !ok || again.ID != id || again.Status != r.Status {
				t.Errorf("Result(%d) after Await = %+v, %v; want %s", id, again, ok, r.Status)
			}
		})

		rec.Run(t, "reload.O3 an unknown operation", func(t *testing.T) {
			u := use(t, mount)
			if r, ok := u.rl.Result(99); ok {
				t.Errorf("Result of an id never issued = %+v, true", r)
			}
			if _, err := u.rl.Await(t.Context(), 99); !errors.Is(err, reload.ErrUnknown) {
				t.Errorf("Await of an id never issued = %v, want ErrUnknown", err)
			}
		})

		rec.Run(t, "reload.O4 a final result crosses the wire unchanged", func(t *testing.T) {
			u := use(t, mount)
			r, err := u.rl.Await(t.Context(), u.submit())
			if err != nil {
				t.Fatal(err)
			}
			want := reload.EncodeResult(r)
			got, err := reload.DecodeReply(reload.Line(reload.Reply{V: reload.WireVersion, Result: &want}))
			if err != nil || got.Result == nil || !reflect.DeepEqual(*got.Result, want) {
				t.Errorf("result %+v read back as %+v, %v", want, got.Result, err)
			}
			if want.ID != r.ID || want.Source != op.Source || want.Status != reload.Passed || want.Error != nil {
				t.Errorf("encoded %+v from %+v", want, r)
			}
		})
	})

	rec.Run(t, "guard", func(t *testing.T) {
		rec.Run(t, "reload.G1 after release the key and Submit fail with ErrInactive", func(t *testing.T) {
			u := use(t, mount)
			id := u.submit()
			if _, err := u.rl.Await(t.Context(), id); err != nil {
				t.Fatal(err)
			}
			deftest.Dispose(t, u.root, u.provider)
			if err := deftest.ReadError(func() { reload.Key.Of(u.c) }); !errors.Is(err, kernel.ErrInactive) {
				t.Errorf("a read of the key after release = %v, want ErrInactive", err)
			}
			if _, err := u.rl.Submit(t.Context(), op); !errors.Is(err, reload.ErrClosed) || !errors.Is(err, kernel.ErrInactive) {
				t.Errorf("Submit after release = %v, want ErrClosed and ErrInactive", err)
			}
			if r, ok := u.rl.Result(id); !ok || r.Status != reload.Passed {
				t.Errorf("Result(%d) after release = %+v, %v; want it passed", id, r, ok)
			}
		})

		rec.Run(t, "reload.G2 no side door", func(t *testing.T) {
			if doors := deftest.SideDoors[reload.Reload](use(t, mount).rl); len(doors) > 0 {
				t.Errorf("the provided value has methods outside reload.Reload: %v", doors)
			}
		})
	})
}

// user is a consumer of reload on a mounted provider.
type user struct {
	t        *testing.T
	root     *kernel.Root
	provider *kernel.Fiber
	c        *kernel.Context
	rl       reload.Reload
}

func use(t *testing.T, mount Mount) *user {
	t.Helper()
	u := &user{t: t}
	u.root, u.provider = mount(t)
	_, u.c, u.rl = deftest.Consume(t, u.root, reload.Key)
	return u
}

func (u *user) submit() reload.ID {
	u.t.Helper()
	id, err := u.rl.Submit(u.t.Context(), op)
	if err != nil {
		u.t.Fatalf("Submit = %v", err)
	}
	return id
}
