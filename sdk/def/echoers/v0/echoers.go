// SPDX-License-Identifier: Apache-2.0

package echoers

import (
	"context"
	"errors"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Key is the echoers registry, class `-` (open) on both sides, at major 0
// (plugin-system-2026-11.md §5).
var Key = kernel.NewKey[Registry]("echoers", 0)

// The refusals of the registry. A call on a registry or an echoer whose fiber is
// released returns an error that matches kernel.ErrInactive (07-c4-plugin-sdk.md:518).
var (
	// ErrName refuses a registration under an empty name or a name already registered.
	ErrName = errors.New("echoers: the name is empty or already registered")
	// ErrUnknown reports a call to a name that no echoer holds, or whose echoer's
	// fiber is unloading.
	ErrUnknown = errors.New("echoers: no echoer of that name")
)

// Echoer is the contribution: it answers a message.
type Echoer interface {
	// Echo returns a reply that ends with msg, the same reply each time for the same
	// msg; the part before msg is the echoer's own. It returns ctx's error when ctx is
	// done, and an error that matches kernel.ErrInactive once the fiber that
	// registered it is released.
	Echo(ctx context.Context, msg string) (string, error)
}

// Registry is what Key serves: the dispatcher, the only caller of an echoer.
type Registry interface {
	// Register adds e under name as an effect of c's fiber, labelled Label(name), so
	// the registration leaves when that fiber unloads (07-c4-plugin-sdk.md:517). It
	// returns ErrName for an empty or taken name, and an error that matches
	// kernel.ErrInactiveEffect when c's fiber is unloading.
	Register(c *kernel.Context, name string, e Echoer) error
	// Echo calls the echoer registered under name inside a lease on its fiber and
	// returns its reply and its error unchanged, or ErrUnknown.
	Echo(ctx context.Context, name, msg string) (string, error)
	// Inventory lists the registered names, sorted. The fiber that owns each is the
	// one that holds Label(name) in its ledger (kernel.Fiber.Effects): a
	// *kernel.Context gives the registry no fiber identity to record, so this is the
	// reading of "registrations by owning fiber" (07-c4-plugin-sdk.md:519; ADR-060
	// decision 5). It is empty once the registry is released.
	Inventory() []string
}

// Label is the ledger label of the registration of name.
func Label(name string) string { return "echoers:" + name }
