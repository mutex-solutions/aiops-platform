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

// The two plugin names that make an entry a group: its fiber mounts the entries
// whose Parent is its id (00-decisions.md:475; 07-c4-plugin-sdk.md:1252). An include row arrives
// from the launcher already resolved, as rows under the include row's id, so it is a
// group with no config of its own.
const (
	GroupPlugin   = "core:group"
	IncludePlugin = "core:include"
)

// The refusals of the entry tree. A refused Apply has no effect.
var (
	// ErrExists reports a Create of an id that has an entry.
	ErrExists = errors.New("loader: entry exists")
	// ErrEmptyID reports an operation whose entry has an empty ID: "" is the Parent
	// of a top-level entry, the root of the tree, and no entry's id.
	ErrEmptyID = errors.New("loader: empty entry id")
	// ErrNotFound reports an Update, Remove or Await of an id with no entry.
	ErrNotFound = errors.New("loader: no such entry")
	// ErrParent reports an entry whose Parent has no entry, or an Update that
	// changes the Parent: the parent is fixed at insert (07-c4-plugin-sdk.md:1307).
	ErrParent = errors.New("loader: no such parent")
	// ErrNotGroup reports a Parent that is not a group.
	ErrNotGroup = errors.New("loader: parent is not a group")
	// ErrNotMounted reports an Await of an entry that has no fiber because it is
	// disabled, of another role or under an entry that is not mounted.
	ErrNotMounted = errors.New("loader: entry is not mounted")
	// ErrUnsupported reports an Apply on a loader that was given no batch function.
	ErrUnsupported = errors.New("loader: no batch function")
	// ErrNotDisposed reports a previous fiber of an id, or of a key the entry provides,
	// that settled in a state other than DISPOSED, so that the entry may not be created
	// (07-c4-plugin-sdk.md:341; Loader).
	ErrNotDisposed = errors.New("loader: previous fiber is not disposed")
	// ErrUnknownKey and ErrUnknownPlugin are why a row's fiber is FAILED: a key name
	// that the catalog does not hold, a plugin id that the module table does not
	// hold. They fail the row, never the loader.
	ErrUnknownKey = errors.New("loader: unknown key name")
	// ErrPatchVetoed is why an entry is FAILED when a listener of
	// loader/patch-context did not call next or returned no context.
	ErrPatchVetoed   = errors.New("loader: patch-context vetoed the entry")
	ErrUnknownPlugin = errors.New("loader: unknown plugin")
)

// EntrySpec is one entry of the tree: a row, as the loader needs it. Plugin, Inject,
// Isolate and Intercept are names: the loader resolves Plugin through the plugin
// values that the launcher gives it, and the key names through the catalog launch
// service (launch.Catalog.Key), because the kernel has no lookup from a name to a
// key. Config is the parsed tree of the row, as plain values. The
// row's inject, intercept and isolate are applied to the entry's context
// (07-c4-plugin-sdk.md:1347, :1375). Only an entry that is Enabled and whose Roles
// include the process role is mounted (03-c3-kernel.md:64; 07-c4-plugin-sdk.md:1360).
type EntrySpec struct {
	ID        string // the composed id, "<group>:<id>" for a child
	Parent    string // composed id of the enclosing group, or ""
	Plugin    string // "<owner>/<name>", GroupPlugin or IncludePlugin
	Config    any
	Enabled   bool
	Roles     []string
	Inject    []string
	Isolate   []string
	Intercept map[string]any // key name -> intercept config
}

// Mount says whether an entry has a fiber, and if not why.
type Mount string

// The four mount states of an entry.
const (
	Mounted         Mount = "mounted"          // the entry has, or is waiting to get, a fiber
	Disabled        Mount = "disabled"         // enabled is false
	OtherRole       Mount = "other-role"       // the process role is not in the entry's roles
	ParentUnmounted Mount = "parent-unmounted" // the entry's group is not mounted
)

// Entry is an entry of the tree. State is the state of its fiber: PENDING while a
// child waits for its group to be active, and empty when Mount is not Mounted.
type Entry struct {
	Spec  EntrySpec
	Mount Mount
	State kernel.State
}

// OpKind names what one operation of Apply does.
type OpKind string

// The three operations of a plan, in the order of 03-c3-kernel.md:379, and OpRestart.
// OpRestart restarts the entry's fiber with the spec the tree holds, as a retry does:
// the kernel keeps a FAILED fiber FAILED until something updates it, and an update that
// changes nothing leaves the fiber as it is. A rollback restarts so a row that failed in
// the generation (ADR-059 decision 19). An entry with no fiber to restart (not mounted,
// under a group that is not ACTIVE, or given a new fiber by the plan) is Unchanged.
const (
	OpCreate  OpKind = "create"
	OpUpdate  OpKind = "update"
	OpRemove  OpKind = "remove"  // only Entry.ID is read
	OpRestart OpKind = "restart" // only Entry.ID is read
)

// Op is one operation of Apply.
type Op struct {
	Kind  OpKind
	Entry EntrySpec
}

// Outcome is what Apply did for one operation.
type Outcome string

// The outcomes. Committed is the in-place commit of volatile values
// (07-c4-plugin-sdk.md:1374); Restarted is a restart with the new config or a fiber
// replaced because the row's plugin, inject, isolate or intercept changed.
const (
	Created   Outcome = "created"   // a fiber was mounted for a new entry
	Recorded  Outcome = "recorded"  // a new entry that is not mounted
	Restarted Outcome = "restarted" // the fiber restarts with the new spec
	Committed Outcome = "committed" // volatile values changed in place, no restart
	Unchanged Outcome = "unchanged"
	Removed   Outcome = "removed"   // the entry and its fiber are gone
	Unmounted Outcome = "unmounted" // the entry stays, its fiber is gone: disabled or another role
)

// Result is the outcome of the operation at the same position of Apply.
type Result struct {
	ID      string
	Outcome Outcome
}

// Loader is the entry tree: "create, update, remove, await, locate"
// (03-c3-kernel.md:64). The docs name the five operations and no signatures;
// these are minimal. reload applies a plan as one kernel.Batch of remove, update
// and create (03-c3-kernel.md:379, :84), so Apply takes the whole plan, and Create,
// Update and Remove are Apply with one operation. The docs say both that the plan
// is one batch and that the loader never creates an entry for an id whose previous
// fiber is not DISPOSED (07-c4-plugin-sdk.md:341); an id that is removed and created
// again therefore has its create in a second batch, after the wait (open point,
// settled before S2a: ADR-059 decision 37).
//
// The rule holds by key too (ADR-059 decision 25): an entry whose plugin provides a
// key, and that the plan creates, enables or replaces or that a group the plan mounts
// mounts, gets its fiber only once every fiber that provides the key in the same realm
// and that this plan or an earlier one removed, disabled or replaced, or that a group
// activation that ended gave up, is DISPOSED, as for an id, because the kernel refuses
// a second provision of a key in a realm while the first stands. The realm of a key at an entry is the nearest entry, the entry
// itself first, whose Isolate names the key, or the root, as in the launcher's static
// graph. A fiber restarted in place keeps its plugin and its realm, so it is not held:
// the launcher admits no composition with two providers of one key in one realm.
type Loader interface {
	// Apply applies the operations in order against the tree as the earlier ones
	// leave it. It validates everything first: a refused operation refuses the whole
	// plan with no effect. An error after the first kernel batch has committed
	// (the context ended while waiting for a disposal) leaves the tree as it is:
	// the entry list is the truth, and an Update of an entry with no fiber mounts it.
	// It is not re-entrant: a hook listener must not call it.
	Apply(ctx context.Context, ops []Op) ([]Result, error)
	// Create mounts a fiber for the entry. It returns ErrExists for a known id.
	Create(ctx context.Context, e EntrySpec) error
	// Update replaces the entry's spec: a change of volatile config paths only
	// commits in place, any other restarts the fiber (07-c4-plugin-sdk.md:1374). One
	// that changes nothing leaves the fiber as it is, FAILED or not (OpRestart
	// restarts it). ErrNotFound for an unknown id.
	Update(ctx context.Context, e EntrySpec) error
	// Remove unloads and disposes the entry's fiber and its children. ErrNotFound
	// for an unknown id.
	Remove(ctx context.Context, id string) error
	// Await waits until the entry's fiber has settled and returns its state
	// (03-c3-kernel.md:67, :380). ErrNotFound for an unknown id, ErrNotMounted for
	// an entry with no fiber by Mount.
	Await(ctx context.Context, id string) (kernel.State, error)
	// Locate returns the entry of an id, mounted or not.
	Locate(id string) (Entry, bool)
}

// PatchContextArgs is the payload of loader/patch-context, dispatched for each
// fiber the loader mounts: the entry, and Context, the context it is mounted from
// (its group's activation, or the loader's), without the row's isolate and
// intercept. The loader's built-in step is innermost (07-c4-plugin-sdk.md:386;
// ADR-034 G8): the next of the last listener applies the row's isolate and intercept
// to Context and returns the result. A listener calls next and returns what next
// returned, as it is or wrapped, and the entry is mounted from what the chain
// returns. A listener that calls next and then returns Context drops the row's
// isolate and intercept. A listener that does not call next, or returns a nil
// context, vetoes the entry: it is FAILED with ErrPatchVetoed and its plugin never
// runs (fail closed, ADR-059 decision 34). The hook's use is "isolate and intercept
// handling" (plugin-system-2026-11.md §8).
type PatchContextArgs struct {
	Entry   EntrySpec
	Context *kernel.Context
}

// EntryInitArgs is the payload of loader/entry-init, dispatched after an entry's
// fiber is created, vetoed or not, with the context it was mounted from: the one
// loader/patch-context returned, or the parent's for a vetoed entry.
type EntryInitArgs struct {
	Entry   EntrySpec
	Context *kernel.Context
}

// VolatileUpdateArgs is the payload of loader/volatile-update, dispatched when
// the loader commits volatile values in place (07-c4-plugin-sdk.md:1374;
// plugin-system-2026-11.md §8 "volatile commits"). Config is the whole new config
// as the tree holds it with every reference replaced by nil (WithoutRefs), and Paths
// are the changed leaves, not the Volatile fields: each changed leaf path as its list
// of keys, sorted (VolatileOnly). A consumer maps a path to its Volatile field by
// prefix. No value at Paths holds a reference, old or new: such a change restarts the
// fiber, because the tree holds references unresolved and only config-refs resolves
// one, at an activation. A consumer reads the values at Paths; a field that holds a
// reference did not change.
type VolatileUpdateArgs struct {
	ID     string
	Config any
	Paths  [][]string
}

// The three hooks of the loader, all owned by Key and sealed
// (07-c4-plugin-sdk.md:463). PatchContext is a waterfall whose innermost step is the
// loader's own (PatchContextArgs): a listener returns the context, and a listener
// that does not call next vetoes what follows it, the built-in step included
// (07-c4-plugin-sdk.md:386). EntryInit and VolatileUpdate are events
// (plugin-system-2026-11.md §8). None is Scoped: they carry no tenant data at S1.
var (
	PatchContext   = kernel.NewWaterfall[PatchContextArgs, *kernel.Context]("loader/patch-context", kernel.OwnedBy(Key), kernel.Sealed())
	EntryInit      = kernel.NewEvent[EntryInitArgs]("loader/entry-init", kernel.OwnedBy(Key), kernel.Sealed())
	VolatileUpdate = kernel.NewEvent[VolatileUpdateArgs]("loader/volatile-update", kernel.OwnedBy(Key), kernel.Sealed())
)
