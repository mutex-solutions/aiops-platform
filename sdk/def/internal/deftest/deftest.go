// SPDX-License-Identifier: Apache-2.0

// Package deftest holds helpers shared by the tests of the definitions. It
// imports the standard library and sdk/kernel only. The bound kernel it builds
// is kernel.New with binding maps, which kerneltest never builds
// (07-c4-plugin-sdk.md:564), so a test can assert key classes.
package deftest

import (
	"context"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Config is a kernel.Config with test deadlines and the given binding maps.
func Config(providers, consumers kernel.Bindings) kernel.Config {
	const n = 64
	return kernel.Config{
		LoadDeadline: 10 * time.Second, DrainDeadline: 10 * time.Second, DisposeDeadline: 10 * time.Second,
		ArchiveLimit: n, EventWindow: n, ObserverQueue: n, OwnerInbox: n, TerminalLimit: n,
		Providers: providers, Consumers: consumers,
	}
}

// Bound builds a kernel with kernel.New, closed when the test ends.
func Bound(tb testing.TB, cfg kernel.Config) *kernel.Root {
	tb.Helper()
	r, err := kernel.New(cfg)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = r.Close(context.Background()) })
	return r
}

// Failure awaits f and returns its error; the fiber must be FAILED.
func Failure(tb testing.TB, f *kernel.Fiber) error {
	tb.Helper()
	err := f.Await(tb.Context())
	st, _ := f.State(tb.Context())
	if st != kernel.Failed || err == nil {
		tb.Fatalf("state = %s, err = %v, want FAILED with an error", st, err)
	}
	return err
}

// Active awaits f and fails the test unless it is ACTIVE.
func Active(tb testing.TB, f *kernel.Fiber) {
	tb.Helper()
	err := f.Await(tb.Context())
	st, _ := f.State(tb.Context())
	if st != kernel.Active || err != nil {
		tb.Fatalf("state = %s, err = %v, want ACTIVE", st, err)
	}
}

// Plugin builds a defined plugin whose Apply is apply.
func Plugin(id string, inject []kernel.Dep, provides []kernel.AnyKey, hooks []kernel.HookUse, apply func(*kernel.Context) error) kernel.Plugin {
	return kernel.Define(kernel.Plugin{ID: id, Inject: inject, Provides: provides, Hooks: hooks,
		Apply: func(c *kernel.Context, _ any) error { return apply(c) }})
}
