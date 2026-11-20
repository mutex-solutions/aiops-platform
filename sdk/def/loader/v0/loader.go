// SPDX-License-Identifier: Apache-2.0

package loader

import (
	"context"
	"errors"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Key is the loader key: consumer-bound, class C, at major 0
// (03-c3-kernel.md:64; 07-c4-plugin-sdk.md:556).
var Key = kernel.NewKey[Loader]("loader", 0, kernel.ConsumerBound())

// The refusals of the entry tree.
var (
	// ErrExists reports a Create of an id that has an entry. The loader never
	// creates an entry for an id whose previous fiber is not DISPOSED
	// (03-c3-kernel.md:64, :379).
	ErrExists = errors.New("loader: entry exists")
	// ErrNotFound reports an Update, Remove or Await of an id with no entry.
	ErrNotFound = errors.New("loader: no such entry")
)

// EntrySpec is one entry of the tree: a row, as the loader needs it. Plugin, Inject,
// Isolate and Intercept are names, resolved by the loader through the launch keys
// it injects (03-c3-kernel.md:64): the kernel has no lookup from a name to a key
// (open point). Config is the parsed tree of the row, as plain values.
// The row's inject, intercept and isolate are applied to the entry's context
// (07-c4-plugin-sdk.md:1346, :1354).
type EntrySpec struct {
	ID        string // the composed id, "<group>:<id>" for a child
	Parent    string // composed id of the enclosing group, or ""
	Plugin    string // "<owner>/<name>"; docs: "one fiber per row"
	Config    any
	Inject    []string
	Isolate   []string
	Intercept map[string]any // key name -> intercept config
}

// Entry is an entry of the tree and the state of its fiber.
type Entry struct {
	Spec  EntrySpec
	State kernel.State
}

// Loader is the entry tree: "create, update, remove, await, locate"
// (03-c3-kernel.md:64). The docs name the five operations and no signatures;
// these are minimal. reload applies a plan as one kernel.Batch of remove, update
// and create (03-c3-kernel.md:379, :84): the docs do not say how the loader
// takes a batch, so none of the five does (open point).
type Loader interface {
	// Create mounts a fiber for the entry. It returns ErrExists for a known id.
	Create(ctx context.Context, e EntrySpec) error
	// Update replaces the entry's spec: a volatile-only change commits in place,
	// any other restarts the fiber (07-c4-plugin-sdk.md:1371). ErrNotFound for an
	// unknown id.
	Update(ctx context.Context, e EntrySpec) error
	// Remove unloads and disposes the entry's fiber. ErrNotFound for an unknown id.
	Remove(ctx context.Context, id string) error
	// Await waits until the entry's fiber has settled and returns its state
	// (03-c3-kernel.md:67, :380). ErrNotFound for an unknown id.
	Await(ctx context.Context, id string) (kernel.State, error)
	// Locate returns the entry of an id.
	Locate(id string) (Entry, bool)
}

// PatchContextArgs is the payload of loader/patch-context: the entry being
// mounted and the context built for its fiber. The docs name the hook's use,
// "isolate and intercept handling" (plugin-system-2026-11.md:319), and no payload;
// this is the smallest the named listeners need (open point).
type PatchContextArgs struct {
	Entry   EntrySpec
	Context *kernel.Context
}

// EntryInitArgs is the payload of loader/entry-init, dispatched when an entry's
// context is ready, before its fiber is created. Payload not described in the docs;
// minimal (open point).
type EntryInitArgs struct {
	Entry   EntrySpec
	Context *kernel.Context
}

// VolatileUpdateArgs is the payload of loader/volatile-update, dispatched when
// the loader commits volatile values in place (07-c4-plugin-sdk.md:1371;
// plugin-system-2026-11.md:319 "volatile commits"). Paths are the dotted config
// paths that changed. Payload not described in the docs; minimal (open point).
type VolatileUpdateArgs struct {
	ID     string
	Config any
	Paths  []string
}

// The three hooks of the loader, all owned by Key and sealed
// (07-c4-plugin-sdk.md:463). PatchContext is a waterfall: a listener returns the
// context, the built-in behaviour is the isolate and intercept of the row, and a
// listener that does not call next vetoes it (07-c4-plugin-sdk.md:385).
// EntryInit and VolatileUpdate are events (plugin-system-2026-11.md:319). None
// is Scoped: they carry no tenant data at S1.
var (
	PatchContext   = kernel.NewWaterfall[PatchContextArgs, *kernel.Context]("loader/patch-context", kernel.OwnedBy(Key), kernel.Sealed())
	EntryInit      = kernel.NewEvent[EntryInitArgs]("loader/entry-init", kernel.OwnedBy(Key), kernel.Sealed())
	VolatileUpdate = kernel.NewEvent[VolatileUpdateArgs]("loader/volatile-update", kernel.OwnedBy(Key), kernel.Sealed())
)
