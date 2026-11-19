// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
)

// The tests in this file reproduce differences between the kernel and the prototype
// reconciler that the acceptance harnesses found. They FAIL on purpose and are
// skipped unless KERNEL_KNOWN_DEFECTS=1, so that the suite stays green while each
// difference waits for an owner decision. A decision either fixes the kernel, and
// the test is then unskipped, or accepts the kernel's behaviour with a new fixture.

func knownDefect(t *testing.T) {
	t.Helper()
	if os.Getenv("KERNEL_KNOWN_DEFECTS") != "1" {
		t.Skip("known difference from the prototype reconciler, awaiting an owner decision: set KERNEL_KNOWN_DEFECTS=1")
	}
}

// The prototype's Provide checks that the activation is live before anything else,
// so a provision attempted on an activation that has ended is refused with
// ErrInactive whatever the key is. The kernel checks the key's class and
// the declaration first (Context.provideKey) and the activation last (handle.provide),
// so an undeclared key on an ended activation is refused with ErrUndeclared and the
// error does not match ErrInactive. The property harness checks exactly this
// ("released context accepts activity") and fails the kernel backend at its first
// released activation. Calling the same Provide with a declared key gives ErrInactive.
func TestProvideOnAnEndedActivationIsInactiveBeforeUndeclared(t *testing.T) {
	knownDefect(t)
	r := newKernel(t)
	declared, undeclared := NewKey[int]("declared", 0), NewKey[int]("undeclared", 0)
	var held *Context
	f := r.Plugin(plug("p/ended", nil, []AnyKey{declared}, func(c *Context) error { held = c; return nil }), nil)
	want(t, f, Active)
	if err := f.Dispose(frontCtx(t)); err != nil {
		t.Fatal(err)
	}
	want(t, f, Disposed)
	if err := declared.Provide(held, 1); !errors.Is(err, ErrInactive) {
		t.Fatalf("declared key on an ended activation = %v, want ErrInactive", err)
	}
	if err := undeclared.Provide(held, 1); !errors.Is(err, ErrInactive) {
		t.Fatalf("undeclared key on an ended activation = %v, want ErrInactive (the prototype checks liveness first)", err)
	}
}

// The prototype resolves the candidate config of an update whenever the fiber has a
// desired target (a LOADING or UNLOADING fiber with a restart pending included),
// so internal/config observes the candidate at the moment of the update, before the
// restart, and corpus cases D17 (update during a blocked teardown) and D20 (update
// during a gated load) record that event. Fiber.Update resolves only an ACTIVE
// fiber (config.go): on any other state the candidate is resolved at the next
// activation, so that event is missing at the update and the traces differ. The
// oracle golden and the prototype agree with each other, and the kernel with
// neither.
func TestUpdateResolvesTheCandidateWhileTheFiberLoads(t *testing.T) {
	knownDefect(t)
	r := newKernel(t)
	var resolved []any
	var mu sync.Mutex
	InternalConfig.On(r.Context(), DefaultStage, func(_ context.Context, raw any, next Next[any]) (any, error) {
		mu.Lock()
		resolved = append(resolved, raw)
		mu.Unlock()
		return next()
	})
	entered, release := make(chan struct{}, 1), make(chan struct{})
	f := r.Plugin(Define(Plugin{ID: "p/loading", Apply: func(*Context, any) error {
		entered <- struct{}{}
		<-release
		return nil
	}}), "v1")
	<-entered
	defer func() { close(release); want(t, f, Active) }()
	if err := f.Update(frontCtx(t), "v2"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(resolved) != 2 || resolved[1] != "v2" {
		t.Fatalf("internal/config saw %v while the fiber was LOADING, want [v1 v2]: the candidate is resolved at the update", resolved)
	}
}
