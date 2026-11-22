// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

// This file is the seam between the unexported lifecycle core and the exported
// front (context tree, typed keys, hooks, root constructor). The front calls only
// what is listed here and never reaches into owner, fiber or activation:
//
//   - newCore and coreConfig build one core; there is no default for any number.
//   - pluginSpec is what the core mounts; svcKey is how it names a service.
//   - core.rootHandle, core.mount, core.batch, core.settle, core.close,
//     core.snapshots, core.inspect, core.events, core.notifyKeys, core.observe,
//     core.awaitObservers, core.observerStatus and core.retentionStatus are the
//     operations of the core (owner.go, inspect.go, observe.go).
//   - core.fiber returns a fiberRef: state, snapshot, await, update, dispose.
//   - handle is the per-activation handle given to an apply callback: read,
//     provide, deferEffect, effect, goTracked, enter, mountChild, requestDispose,
//     requestScope and owns (effect.go).

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// svcKey names one service to the core. It must be comparable: it is used as a
// map key, and a non-comparable dynamic type is rejected when a pluginSpec is
// validated. The front passes a key pointer, so two keys are equal exactly when
// they are the same declared key. The core never interprets a key, and never
// addresses a service by a string.
type svcKey any

// disposer reverts one successful setup operation. It runs at most once, under the
// dispose deadline, off the owner goroutine.
type disposer = Disposer

// coreConfig is every number the core needs. All of it is required at
// construction and the core holds no default: the launcher supplies the values.
// The deadlines are kernel-wide, not per fiber.
type coreConfig struct {
	loadDeadline    time.Duration // Apply and an availability check must return within it.
	drainDeadline   time.Duration // A quiesced fiber's own leases and tracked work must end within it.
	disposeDeadline time.Duration // The whole effect walk of one fiber, and one scope's setup rollback.
	archiveLimit    int           // Recently disposed incarnations kept; the oldest is evicted first.
	eventWindow     int           // Most recent events kept in the ledger tail.
	observerQueue   int           // Accepted events that may wait undelivered behind a stalled observer.
	ownerInbox      int           // Capacity of the owner's inbox: external backpressure.
	terminalLimit   int           // Diagnostics of past failed incarnations that settle and close keep.
	// observerLog, when non-nil, receives each contained observer failure on the
	// dispatcher worker. It is an option, not a number: nil disables logging.
	observerLog logFunc
	// logHandler, when non-nil, receives one record for a tracked goroutine that
	// fails and one for a fiber marked leaked (logError). It is an option: nil
	// writes nothing.
	logHandler slog.Handler
}

// validate rejects a configuration in which any required number is zero or negative.
func (c coreConfig) validate() error {
	var errs []error
	for _, d := range []struct {
		name  string
		value int64
	}{
		{"loadDeadline", int64(c.loadDeadline)}, {"drainDeadline", int64(c.drainDeadline)}, {"disposeDeadline", int64(c.disposeDeadline)},
		{"archiveLimit", int64(c.archiveLimit)}, {"eventWindow", int64(c.eventWindow)}, {"observerQueue", int64(c.observerQueue)},
		{"ownerInbox", int64(c.ownerInbox)}, {"terminalLimit", int64(c.terminalLimit)},
	} {
		if d.value <= 0 {
			errs = append(errs, fmt.Errorf("coreConfig.%s must be positive, got %d", d.name, d.value))
		}
	}
	return errors.Join(errs...)
}

// newCore validates cfg and starts one owner goroutine and no per-idle-fiber
// goroutine. close stops it. A configuration with a zero or negative number is an
// error and starts nothing.
func newCore(cfg coreConfig) (*core, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	r := &core{cfg: cfg, log: slog.New(logHandlerOf(cfg)), inbox: make(chan message, cfg.ownerInbox), wake: make(chan struct{}, 1), done: make(chan struct{})}
	o := newOwner(r)
	go o.run()
	return r, nil
}

// pluginSpec is what the core mounts: one plugin, as data and three callbacks.
type pluginSpec struct {
	ID string // The plugin id, never a service key.
	// Needs gate activation and join the epoch. Provides lists the keys the fiber
	// may provide; providing another key fails.
	Needs, Provides []svcKey
	// Apply runs on the fiber's goroutine under the load deadline. Everything it
	// registers is an effect of the fiber.
	Apply func(*handle) error
	// ApplyEffect is the form of Apply that also returns a disposer. It is mutually
	// exclusive with Apply. A successful returned disposer is appended after the
	// body effects, even if the body requested self-disposal.
	ApplyEffect func(*handle) (disposer, error)
	// Check is a consumer-bound availability predicate evaluated off the owner.
	// False is PENDING, not a failed activation; notifyKeys retries it. Its handle
	// is never live: it can read the bindings but registers nothing.
	Check func(*handle) bool
}

// fiberRef is the front's handle on one fiber of a core. It holds no state of its
// own, so it is cheap to copy and safe to keep after the fiber is disposed.
type fiberRef struct {
	c  *core
	id uint64
}

// fiber returns the handle on fiber id. No check is made until an operation runs.
func (r *core) fiber(id uint64) fiberRef { return fiberRef{c: r, id: id} }

// snapshot returns the fiber's view. A recently disposed fiber answers from the
// bounded archive, an evicted one with errExpired and a never issued id with
// errUnknown.
func (f fiberRef) snapshot(ctx context.Context) (fiberSnap, error) {
	a := f.c.exchange(ctx, message{kind: cmdFiber, id: f.id})
	if a.err != nil {
		return fiberSnap{}, a.err
	}
	return a.snapshots[0], nil
}

// state returns the fiber's current state.
func (f fiberRef) state(ctx context.Context) (State, error) {
	s, err := f.snapshot(ctx)
	return s.State, err
}

// await returns when the fiber is not mid-transition (or is blocked past a
// deadline of its own), with its retained startup error. It admits no mutation
// and is cancellable at any time. It must not be called from inside the fiber's
// own callbacks: a callback must not wait for its own transition.
func (f fiberRef) await(ctx context.Context) error {
	return f.c.exchange(ctx, message{kind: cmdAwait, id: f.id}).err
}

// update requests a restart of the fiber with a new revision. It returns once the
// request is admitted, not when the restart completes: use await for that.
func (f fiberRef) update(ctx context.Context) error {
	return f.c.exchange(ctx, message{kind: cmdUpdate, id: f.id}).err
}

// dispose requests disposal of the fiber. It is idempotent, returns once the
// request is admitted, and never waits for the cleanup: use await for that.
func (f fiberRef) dispose(ctx context.Context) error {
	return f.c.exchange(ctx, message{kind: cmdDispose, id: f.id}).err
}
