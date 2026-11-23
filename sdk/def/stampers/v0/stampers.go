// SPDX-License-Identifier: Apache-2.0

package stampers

import (
	"context"
	"errors"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Key is the stampers registry, class `-` (open) on both sides, at major 0
// (plugin-system-2026-11.md §5).
var Key = kernel.NewKey[Registry]("stampers", 0)

// The refusals of the registry. A call on a registry or a stamper whose fiber is
// released returns an error that matches kernel.ErrInactive (07-c4-plugin-sdk.md:518).
var (
	// ErrName refuses a registration under an empty name or a name already registered.
	ErrName = errors.New("stampers: the name is empty or already registered")
	// ErrUnknown reports a call to a name that no stamper holds, or whose stamper's
	// fiber is unloading.
	ErrUnknown = errors.New("stampers: no stamper of that name")
)

// Stamper is the contribution: it stamps a body.
type Stamper interface {
	// Stamp returns the stamp it puts on body: one line, not empty, and the same each
	// time for the same body. It returns ctx's error when ctx is done, and an error
	// that matches kernel.ErrInactive once the fiber that registered it is released.
	Stamp(ctx context.Context, body string) (string, error)
}

// Registry is what Key serves: the dispatcher, the only caller of a stamper.
type Registry interface {
	// Register adds s under name as an effect of c's fiber, labelled Label(name), so
	// the registration leaves when that fiber unloads (07-c4-plugin-sdk.md:517). It
	// returns ErrName for an empty or taken name, and an error that matches
	// kernel.ErrInactiveEffect when c's fiber is unloading.
	Register(c *kernel.Context, name string, s Stamper) error
	// Stamp calls the stamper registered under name inside a lease on its fiber and
	// returns its stamp and its error unchanged, or ErrUnknown.
	Stamp(ctx context.Context, name, body string) (string, error)
	// Inventory lists the registered names, sorted. The fiber that owns each holds
	// Label(name) in its ledger (kernel.Fiber.Effects; 07-c4-plugin-sdk.md:519). It is
	// empty once the registry is released.
	Inventory() []string
}

// Label is the ledger label of the registration of name.
func Label(name string) string { return "stampers:" + name }
