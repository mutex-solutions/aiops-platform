// SPDX-License-Identifier: Apache-2.0

// Package concurrency holds experimental scheduler candidates, not the kernel.
package concurrency

import (
	"context"
	"errors"
	"sync/atomic"
)

// State is the observable lifecycle state shared by both candidates.
type State string

const (
	Pending   State = "PENDING"
	Loading   State = "LOADING"
	Active    State = "ACTIVE"
	Failed    State = "FAILED"
	Unloading State = "UNLOADING"
	Disposed  State = "DISPOSED"
)

var (
	ErrInactive    = errors.New("inactive context")
	ErrClosed      = errors.New("runtime closed")
	ErrUnknown     = errors.New("unknown fiber")
	ErrRootDispose = errors.New("root disposal is owned by Runtime.Close")
)

// Disposer reverts a successful setup operation.
type Disposer func(context.Context) error

// Spec is deliberately neutral fixture data. No product service lives here.
type Spec struct {
	ID       string
	Needs    []string
	Provides []string
	Apply    func(*Context) error
	// ApplyEffect is the oracle-fixture form of an Apply-returned disposer. It is
	// mutually exclusive with Apply. A successful returned disposer is appended
	// after body effects by the runtime, even if that body requested self-disposal.
	// This test contract does not select the final author-facing SDK shape.
	ApplyEffect func(*Context) (Disposer, error)
	// Check is a consumer-bound availability predicate evaluated off the graph
	// state owner. False is PENDING, not a failed activation; Notify retries it.
	Check func(*Context) bool
	// Config is native lifecycle configuration data. A worker prepares it outside
	// the state owner before Apply; a nil cell preserves the narrow fixture API.
	Config *Configuration
}

// Notifier is an experimental availability recheck operation shared by candidates.
// It is separate from Runtime so existing narrow test adapters need not invoke it.
type Notifier interface {
	Notify(context.Context, ...string) error
}

// RootProvider exposes the actual trusted host activation. It is not a mounted
// user fiber or an unchecked plugin read capability; Close owns its lifecycle.
type RootProvider interface {
	RootContext(context.Context) (*Context, error)
}

// Waiter joins one fiber's lifecycle without waiting for unrelated transitions.
type Waiter interface {
	AwaitFiber(context.Context, uint64) error
}

// ConfigUpdater retains and evaluates a new raw input through the native
// configuration pipeline. noSave suppresses persistence notification only.
type ConfigUpdater interface {
	UpdateConfig(context.Context, uint64, any, bool) error
}

// ConfigRequest separates the synchronous resolve/commit/target-change prefix
// from completion of the resulting lifecycle work. Await may be called only
// outside the activation being changed, just like Waiter.AwaitFiber.
type ConfigRequest struct {
	Await func(context.Context) error
}

type ConfigRequester interface {
	RequestUpdateConfig(context.Context, uint64, any, bool) (ConfigRequest, error)
}

// Recorder is test-only instrumentation for actual fixture operations. Candidates
// linearize these immutable envelopes in the same ledger as real state events;
// it is not a production service or a source of expected runtime responses.
type Recorder interface {
	Record(context.Context, string, map[string]any) error
}

// Snapshot is immutable once returned by a scheduler.
type Snapshot struct {
	ID              uint64
	Name            string
	State           State
	Revision        uint64
	Epoch           []uint64 // Retained activation dependencies, not the desired target.
	Effects         []string
	EffectTree      []EffectMeta // Actual native ownership subtrees, detached on inspection.
	Tracked         []string     // Native labels for work that has not actually returned.
	Error           string
	Alive           bool           // False once disposal is requested, before cleanup.
	Target          *EpochSnapshot // Nil when the desired epoch is inactive.
	Configured      bool           // Distinguishes an absent configuration from a nil value.
	ConfigPrepared  bool
	RawConfig       any
	EffectiveConfig any
	ConfigVersion   uint64
}

// EpochSnapshot is the desired revision/provision-identity tuple. The current
// activation may retain a different committed epoch while it is unloading.
type EpochSnapshot struct {
	Revision uint64
	UIDs     []uint64
}

// ProvisionSnapshot observes a real reserved provision, including non-ACTIVE
// reservations. Entries follow UID creation order, never inferred fixture data.
type ProvisionSnapshot struct {
	Key    string
	UID    uint64
	Owner  uint64
	Name   string
	Value  any
	Active bool
}

type Inspection struct {
	Fibers     []Snapshot
	Provisions []ProvisionSnapshot
	// Sequence is the actual ledger watermark from this same owner turn/lock.
	// It permits observation sidecars without combining state with future events.
	Sequence uint64
}

// Inspector observes the actual graph without executing plugin callbacks.
type Inspector interface {
	Inspect(context.Context) (Inspection, error)
}

// Event captures the total state/service order, before any observer executes.
type Event struct {
	Sequence uint64
	Fiber    uint64
	Name     string // Spec.ID, available even before Mount returns.
	Kind     string
	From     State
	State    State
	Key      string
	UID      uint64
	Value    any            // Observed service value, never an expected response.
	Data     map[string]any // Immutable envelope when Kind is "fixture".
}

// Runtime is the narrow test contract both concurrency cores must implement.
type Runtime interface {
	Mount(context.Context, Spec) (uint64, error)
	Update(context.Context, uint64) error
	Dispose(context.Context, uint64) error
	Settle(context.Context) error
	Snapshots(context.Context) ([]Snapshot, error)
	Events(context.Context) ([]Event, error)
	Close(context.Context) error
}

// Context describes one activation. It cannot revive an earlier activation.
type Context struct {
	host          any // Native runtime identity, set once; never derived from a Spec ID.
	std           context.Context
	fiber         uint64
	live          atomic.Bool // New effects and leases are accepted only while live.
	released      atomic.Bool // Committed reads remain valid during orderly teardown.
	lease         atomic.Int64
	bindings      map[string]provision
	provide       func(string, any) error
	effect        func(string, Disposer) error
	cleanupEffect func(Disposer) error // Native off-owner dispose budget for partial/late setup results.
	scopeRequest  func(string, func(*EffectScope) error) (*EffectHandle, error)
	dispose       func() error
	child         func(Spec) (uint64, error)
	trackedStart  func(string) (func(error), error)
	leaseChanged  func() // Optional nonblocking wake; installed by the scheduler.
	config        any    // Immutable effective input captured for this activation.
	configVersion uint64
	owned         atomic.Pointer[ownedProvisions]
}

type ownedProvisions struct{ keys map[string]struct{} }

// Owns is a lock-free observation of this activation's successful native
// provision reservations. Callers that write derived scope metadata must also
// hold Enter/Leave, so quiescence cannot finish while that operation is in flight.
func (c *Context) Owns(key string) bool {
	if c.released.Load() {
		return false
	}
	values := c.owned.Load()
	if values == nil {
		return false
	}
	_, ok := values.keys[key]
	return ok
}

// unpublishOwned follows actual native removal. Like publication, native state
// ownership serializes writes; readers observe an immutable COW key set.
func (c *Context) unpublishOwned(key string) {
	previous := c.owned.Load()
	if previous == nil {
		return
	}
	next := &ownedProvisions{keys: make(map[string]struct{}, len(previous.keys))}
	for existing := range previous.keys {
		if existing != key {
			next.keys[existing] = struct{}{}
		}
	}
	c.owned.Store(next)
}

func (c *Context) publishOwned(key string) {
	previous := c.owned.Load()
	next := &ownedProvisions{keys: make(map[string]struct{})}
	if previous != nil {
		for existing := range previous.keys {
			next.keys[existing] = struct{}{}
		}
	}
	next.keys[key] = struct{}{}
	c.owned.Store(next)
}

type provision struct {
	uid   uint64
	owner uint64
	value any
}

func (c *Context) Std() context.Context { return c.std }

// Config returns the prepared immutable input owned by this activation, not a
// later raw replacement or another activation's mutable configuration cell.
func (c *Context) Config() any { return c.config }

// Read requires a dependency captured for the current activation.
func (c *Context) Read(key string) (any, error) {
	if c.released.Load() {
		return nil, ErrInactive
	}
	p, ok := c.bindings[key]
	if !ok {
		return nil, ErrUnknown
	}
	return p.value, nil
}

func (c *Context) Provide(key string, value any) error {
	if !c.live.Load() {
		return ErrInactive
	}
	return c.provide(key, value)
}

func (c *Context) Defer(label string, dispose Disposer) error {
	if !c.live.Load() {
		return ErrInactive
	}
	return c.effect(label, dispose)
}

// MountChild creates a real lifecycle child of this exact activation. Native
// ownership invalidates and joins the child before releasing the parent's effects;
// an old context cannot attach children to a replacement activation.
func (c *Context) MountChild(spec Spec) (uint64, error) {
	if !c.live.Load() || c.child == nil {
		return 0, ErrInactive
	}
	return c.child(spec)
}

// Dispose requests disposal without awaiting the activation that called it.
func (c *Context) Dispose() error {
	if c.dispose == nil {
		return ErrInactive
	}
	return c.dispose()
}

// Enter acquires a call-path lease with no graph lock and no allocation.
// The caller must pair a successful Enter with exactly one Leave.
func (c *Context) Enter() bool {
	if !c.live.Load() {
		return false
	}
	c.lease.Add(1)
	if c.live.Load() {
		return true
	}
	c.lease.Add(-1)
	if c.leaseChanged != nil {
		c.leaseChanged()
	}
	return false
}

func (c *Context) Leave() {
	c.lease.Add(-1)
	if c.leaseChanged != nil {
		c.leaseChanged()
	}
}
