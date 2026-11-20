// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"fmt"
	"slices"
)

// The launcher's read API. Everything here hangs off *Root, which only the launcher
// holds: a plugin is given a *Context, and no Context method returns the Root or
// anything that reaches it (plugin record, section 3: the launcher owns the root
// context). Every result is a value copied out of the core on the owner's turn;
// nothing returned points into kernel state or can change it, and no function here
// runs plugin code. The kernel adds no query language and no write capability.

var (
	// ErrClosed reports a command sent to a kernel that has closed.
	ErrClosed = errClosed
	// ErrUnknown reports a fiber id that was never issued.
	ErrUnknown = errUnknown
	// ErrExpired reports a fiber that was issued and disposed, whose archived record
	// was evicted (ADR-034, "2026-11-19 bounded history and key-indexed service holds").
	ErrExpired = errExpired
	// ErrRootDispose reports a dispose request on the root context.
	ErrRootDispose = errRootDispose
	// ErrEventsTruncated accompanies the retained tail that Events returns once older
	// events were dropped to honour Config.EventWindow.
	ErrEventsTruncated = errEventsTruncated
	// ErrObserverOverflow matches the error of an observer that was detached because
	// it stalled the dispatcher past Config.ObserverQueue (ADR-034, "2026-11-19
	// bounded observer queue and overflow rule").
	ErrObserverOverflow = errObserverOverflow
	// ErrReentrant is returned by AwaitObservers when it is called with the context
	// of an observer callback: a callback must not join its own dispatcher.
	ErrReentrant = errReentrant
)

// ObserverOverflow describes one observer detached for overflow: its label, the
// events it lost, the last sequence addressed to it and the bound that was reached.
// It matches ErrObserverOverflow.
type ObserverOverflow = overflowError

// ObserverStatus is a synchronized diagnostic snapshot of the observer dispatcher.
type ObserverStatus = observerStatus

// RetentionStatus is a lock-free report of the history bounds and their use.
type RetentionStatus = retentionStatus

// TargetEpoch is the desired revision and provision identities of a fiber.
type TargetEpoch struct {
	Revision uint64
	UIDs     []uint64
}

// FiberSnapshot is the immutable view of one fiber incarnation, live or recently
// disposed (Config.ArchiveLimit).
type FiberSnapshot struct {
	ID         uint64
	Plugin     string       // Plugin.ID.
	State      State        // The six states.
	Revision   uint64       // The restart counter.
	Epoch      []uint64     // Provision identities the activation retains.
	Effects    []string     // Effect labels in registration order.
	EffectTree []EffectMeta // Ownership trees, one per effect.
	Tracked    []string     // Labels of tracked work that has not returned.
	Error      string       // The startup, blocked and cleanup diagnostics, joined.
	Alive      bool         // False once disposal is requested, before cleanup.
	Leaked     bool         // A drain or a disposer ran past its deadline.
	Target     *TargetEpoch // Nil when the desired epoch is inactive.
}

// Provision is one reserved provision, active or not, in creation order. Value is
// the value of a plain Provide, read from the provision at the call and copied out;
// it is nil for ProvideFor, whose view needs its caller. A provision that has ended
// is no longer listed, so Value never outlives it.
type Provision struct {
	Key    string // "name@major", with "/label" in a named realm.
	UID    uint64
	Owner  uint64
	Plugin string // The owner's Plugin.ID.
	Value  any
	Active bool
}

// Inspection is the graph at one owner turn. Sequence is the ledger watermark of
// that same turn, so state is never combined with later events.
type Inspection struct {
	Fibers     []FiberSnapshot
	Provisions []Provision
	Sequence   uint64
}

// LedgerEvent is one event of the total order, before any observer runs. Sequence
// is consecutive from 1. For a service event Key is the service key and Label is
// empty; for every other kind Label is the effect or tracked-work label and Key is
// empty. An event never carries a provided value, only the key and the label: a
// service value must stay collectable once its provision has ended, and the event
// stream is read by observers that must not see it.
type LedgerEvent struct {
	Sequence uint64
	Fiber    uint64
	Plugin   string
	Kind     string
	From     State
	State    State
	Key      string
	Label    string
	UID      uint64
}

// renderKey renders a key of the core: an address as "name@major", a label as itself.
func renderKey(k svcKey) (key, label string) {
	switch v := k.(type) {
	case nil:
		return "", ""
	case *address:
		return v.String(), ""
	case string:
		return "", v
	}
	return fmt.Sprint(k), ""
}

// plainValue unwraps the value that a plain Provide stored.
func plainValue(v any) any {
	if p, ok := v.(*provided); ok {
		return p.value
	}
	return v
}

func exportEvent(e ledgerEvent) LedgerEvent {
	key, label := renderKey(e.Key)
	return LedgerEvent{Sequence: e.Sequence, Fiber: e.Fiber, Plugin: e.Name, Kind: e.Kind, From: e.From, State: e.State,
		Key: key, Label: label, UID: e.UID}
}

func exportSnapshot(s fiberSnap) FiberSnapshot {
	out := FiberSnapshot{ID: s.ID, Plugin: s.Name, State: s.State, Revision: s.Revision, Epoch: slices.Clone(s.Epoch),
		Effects: slices.Clone(s.Effects), EffectTree: slices.Clone(s.EffectTree), Tracked: slices.Clone(s.Tracked),
		Error: s.Error, Alive: s.Alive, Leaked: s.Leaked}
	if s.Target != nil {
		out.Target = &TargetEpoch{Revision: s.Target.Revision, UIDs: slices.Clone(s.Target.UIDs)}
	}
	return out
}

// Snapshots lists the live fibers and the recently disposed ones, ordered by id.
// It is for the launcher: it enters the owner once and runs no plugin code.
func (r *Root) Snapshots(ctx context.Context) ([]FiberSnapshot, error) {
	ss, err := r.core.snapshots(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]FiberSnapshot, len(ss))
	for i, s := range ss {
		out[i] = exportSnapshot(s)
	}
	return out, nil
}

// Inspect is Snapshots plus the provision table and the ledger watermark of the
// same owner turn. It waits for no lifecycle work and runs no availability check.
func (r *Root) Inspect(ctx context.Context) (Inspection, error) {
	v, err := r.core.inspect(ctx)
	if err != nil {
		return Inspection{}, err
	}
	out := Inspection{Fibers: make([]FiberSnapshot, len(v.Fibers)), Provisions: make([]Provision, len(v.Provisions)), Sequence: v.Sequence}
	for i, s := range v.Fibers {
		out.Fibers[i] = exportSnapshot(s)
	}
	for i, p := range v.Provisions {
		key, _ := renderKey(p.Key)
		out.Provisions[i] = Provision{Key: key, UID: p.UID, Owner: p.Owner, Plugin: p.Name, Value: plainValue(p.Value), Active: p.Active}
	}
	return out, nil
}

// Events returns the bounded event ledger: the most recent Config.EventWindow
// events. No event holds a provided value (see LedgerEvent), so a value is
// collectable as soon as its provision has ended, not after EventWindow more events.
// Once older events were dropped it returns the tail with an error that
// matches ErrEventsTruncated, never a silent partial list.
func (r *Root) Events(ctx context.Context) ([]LedgerEvent, error) {
	evs, err := r.core.events(ctx)
	if evs == nil {
		return nil, err
	}
	out := make([]LedgerEvent, len(evs))
	for i, e := range evs {
		out[i] = exportEvent(e)
	}
	return out, err
}

// ObserveOption is an option of Root.Observe.
type ObserveOption func(*observeOptions)

type observeOptions struct {
	onOverflow func(context.Context, *ObserverOverflow) error
}

// OnOverflow registers the terminal signal of an observer that is detached for
// overflow: fn runs once, on the goroutine that was inside the observer's last
// callback, after that callback returned.
func OnOverflow(fn func(context.Context, *ObserverOverflow) error) ObserveOption {
	return func(o *observeOptions) { o.onOverflow = fn }
}

// Observe subscribes fn to the event stream of the kernel, in the one total order
// of Sequence, delivered off the owner (plugin record G5; ADR-034 owner decision 5).
// It is a launcher-level observer bound to the root context: it leaves when the
// kernel closes, or when the returned Disposer is called. Observers never change
// an outcome, and the kernel never waits for one: an observer that stalls past
// Config.ObserverQueue is detached, with the rules of ADR-034, "2026-11-19 bounded
// observer queue and overflow rule". A plugin cannot observe: no Context method
// does, because the records describe launcher observers only.
func (r *Root) Observe(label string, fn func(context.Context, LedgerEvent) error, opts ...ObserveOption) (Disposer, error) {
	var o observeOptions
	for _, opt := range opts {
		opt(&o)
	}
	var callback func(context.Context, ledgerEvent) error
	if fn != nil {
		callback = func(ctx context.Context, e ledgerEvent) error { return fn(ctx, exportEvent(e)) }
	}
	return r.core.observeRoot(r.ctx.h, label, callback, o.onOverflow)
}

// AwaitObservers returns when every observer has been handed the events accepted
// at the call. An overflow is reported as an error that matches ErrObserverOverflow.
// An observer callback must not call it: its own context is refused.
func (r *Root) AwaitObservers(ctx context.Context) error { return r.core.awaitObservers(ctx) }

// ObserverStatus reports the dispatcher's counters without running user code.
func (r *Root) ObserverStatus() ObserverStatus { return r.core.observerStatus() }

// RetentionStatus reports the history bounds and their use without entering the
// owner.
func (r *Root) RetentionStatus() RetentionStatus { return r.core.retentionStatus() }

// Notify tells the kernel that the availability of keys, in the default realm, may
// have changed: the dependants of each key run their availability checks again and
// the kernel records one service event per key (ADR-034, "2026-11-19 bounded history
// and key-indexed service holds"). A nil or zero key is refused with ErrKey.
func (r *Root) Notify(ctx context.Context, keys ...AnyKey) error {
	addrs := make([]svcKey, len(keys))
	for i, k := range keys {
		d, err := dataOf(k)
		if err != nil {
			return fmt.Errorf("notify key %d: %w", i, err)
		}
		addrs[i] = d.def
	}
	return r.core.notifyKeys(ctx, addrs...)
}

// ID returns the fiber's id, the one FiberSnapshot.ID and LedgerEvent.Fiber carry.
func (f *Fiber) ID() uint64 { return f.ref.id }
