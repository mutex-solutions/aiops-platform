// SPDX-License-Identifier: Apache-2.0

// Package cycle runs the generated cycle test, zz_cycle_test.go, that plugingen
// writes beside every Go plugin (07-c4-plugin-sdk.md:1432-1436; ADR-060 decision
// 12). The generated file imports the fake of each definition the Spec injects and
// hands them to Run, so neither this package nor kerneltest imports a definition.
//
// It is a package of its own, apart from kerneltest, so that goleak enters the
// module graph of a module whose tests run a cycle test, and not that of the SDK
// module, whose require list 00-decisions.md:262 fixes.
package cycle

import (
	"errors"
	"slices"
	"sync/atomic"
	"testing"

	"go.uber.org/goleak"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// Rounds is how many times Run mounts and disposes the plugin.
const Rounds = 3

// Fake is the fake of one definition: its plugin and its Inventory, which lists what
// the plugin's callers left in it (07-c4-plugin-sdk.md:494).
type Fake struct {
	Plugin    kernel.Plugin
	Inventory func() []string
}

// Run mounts the fakes and then mounts, disposes and remounts p with cfg Rounds
// times on a kerneltest root, and fails tb when:
//   - p does not activate;
//   - while p is active, its fiber's ledger holds the label "<key>:<name>" of a name
//     in the Inventory of a fake whose keys registers does not list: a registration
//     in a registry that Spec.Registers does not list (ADR-048 decision 21). The
//     label is the one a registry of the SDK records (echoers.Label);
//   - after a dispose, a fake's Inventory is not back at its baseline, or the
//     fiber's effect ledger is not empty;
//   - a registration through the context of a disposed activation does not return
//     kernel.ErrInactiveEffect;
//   - once the root is closed, goleak finds a goroutine that was not running when
//     Run began.
//
// registers holds the key names of Spec.Registers.
func Run(tb testing.TB, p kernel.Plugin, cfg any, registers []string, fakes ...Fake) {
	tb.Helper()
	leaks := goleak.IgnoreCurrent()
	ctx := tb.Context()
	root := kerneltest.New(tb)
	for _, f := range fakes {
		kerneltest.Mount(root, f.Plugin)
	}
	if tb.Failed() {
		tb.Fatalf("cycle: a fake did not activate")
	}
	base := make([][]string, len(fakes))
	for i, f := range fakes {
		base[i] = f.Inventory()
	}
	var last atomic.Pointer[kernel.Context]
	w := p // the copy keeps p's entry
	w.Apply = func(c *kernel.Context, cfg any) error {
		last.Store(c)
		if p.Apply == nil {
			return nil
		}
		return p.Apply(c, cfg)
	}
	for round := 1; round <= Rounds; round++ {
		f := root.Plugin(w, cfg)
		err := f.Await(ctx)
		if st, serr := f.State(ctx); err != nil || serr != nil || st != kernel.Active {
			tb.Fatalf("cycle round %d: %s is %s, want ACTIVE (await: %v, state: %v)", round, p.ID, st, err, serr)
		}
		labels, err := f.Effects(ctx)
		if err != nil {
			tb.Fatalf("cycle round %d: effects: %v", round, err)
		}
		for _, fk := range fakes {
			for _, k := range fk.Plugin.Provides {
				if slices.Contains(registers, k.Name()) {
					continue
				}
				for _, name := range fk.Inventory() {
					if slices.Contains(labels, k.Name()+":"+name) {
						tb.Errorf("cycle round %d: %s registers %q in %s, which Spec.Registers does not list", round, p.ID, name, k.Name())
					}
				}
			}
		}
		if err := f.Dispose(ctx); err != nil {
			tb.Fatalf("cycle round %d: dispose: %v", round, err)
		}
		_ = f.Await(ctx) // the fiber is disposed; Await waits for its cleanup
		for i, fk := range fakes {
			if got := fk.Inventory(); !slices.Equal(got, base[i]) {
				tb.Errorf("cycle round %d: the Inventory of %s is %q after the dispose and is not back at its baseline %q",
					round, fk.Plugin.ID, got, base[i])
			}
		}
		if labels, err := f.Effects(ctx); err != nil || len(labels) != 0 {
			tb.Errorf("cycle round %d: the effect ledger holds %q after the dispose (%v)", round, labels, err)
		}
		late := func() (kernel.Disposer, error) { return nil, nil }
		if err := last.Load().Effect("cycle: late registration", late); !errors.Is(err, kernel.ErrInactiveEffect) {
			tb.Errorf("cycle round %d: a late registration returned %v, want kernel.ErrInactiveEffect", round, err)
		}
	}
	if err := root.Close(ctx); err != nil {
		tb.Errorf("cycle: close: %v", err)
	}
	if err := goleak.Find(leaks); err != nil {
		tb.Errorf("cycle: a goroutine outlived the plugin: %v", err)
	}
}
