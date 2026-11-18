// SPDX-License-Identifier: Apache-2.0

package concurrency

import (
	"context"
	"errors"
	"sync"
)

// These S0 operations adapt Cordis fiber.ts:255-266, 648-679 and 783-818;
// see NOTICE. They are a common configuration cell, not a hook registry, runtime
// wrapper, schema implementation or the final SDK API.

var (
	ErrAlreadyContinued = errors.New("configuration update continued more than once")
	ErrConfigRestart    = errors.New("configuration restart callback is required")
)

// ConfigHooks is immutable after construction. Resolve evaluates a candidate
// without changing running configuration; Commit notifies persistence using raw
// input; Update wraps the restart continuation (not calling next vetoes restart).
// Hooks run without the cell lock. They must return only after any next call has
// completed, and must not retain next for use after Update returns.
//
// All values, including maps, slices and pointers reachable from them, are
// caller-owned and must be treated as immutable by every caller and hook. The
// cell preserves identity and never serializes, clones or compares unknown values.
// A hook that transforms a container must create its own immutable result.
// Resolve is responsible for candidate-only behavior: the cell cannot undo hook
// side effects. Hooks may reenter cell operations and must synchronize their own
// mutable state if shared by concurrent calls.
type ConfigHooks struct {
	Resolve func(context.Context, any) (any, error)
	Commit  func(context.Context, any) error
	Update  func(context.Context, any, func() error) error
	// UpdateAsync, when set, replaces Update. It runs only the synchronous prefix
	// of the hook and returns a waiter for the actual completion; the waiter may
	// be called repeatedly. next must be called at most once and never after the
	// waiter reports completion. The hook runs without the cell lock.
	UpdateAsync func(context.Context, any, func() error) (func(context.Context) error, error)
}

// Configuration stores retained raw input and the last published effective
// value. Its version counts raw replacements, not runtime revisions. A zero
// Configuration is usable with identity resolution and no hooks.
type Configuration struct {
	mu          sync.Mutex
	raw         any
	effective   any
	prepared    bool
	version     uint64
	publication uint64 // Fences effective publications even at the same raw version.
	hooks       ConfigHooks
}

func NewConfiguration(raw any, hooks ConfigHooks) *Configuration {
	return &Configuration{raw: raw, hooks: hooks}
}

// ConfigSnapshot is a coherent observation, not an ownership transfer.
// Prepared distinguishes a successfully published nil from no effective value.
// Its reachable values remain caller-owned and immutable.
type ConfigSnapshot struct {
	Raw       any
	Effective any
	Version   uint64
	Prepared  bool
}

// Snapshot observes cell state atomically without invoking any callbacks.
func (c *Configuration) Snapshot() ConfigSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return ConfigSnapshot{Raw: c.raw, Effective: c.effective, Version: c.version, Prepared: c.prepared}
}

// RawSnapshot returns a coherent snapshot without invoking callbacks. Effective
// is nil until preparation or a successful retained-instance update publishes it.
// Returned references remain caller-owned and immutable, even after unload.
func (c *Configuration) RawSnapshot() (raw, effective any, version uint64) {
	snapshot := c.Snapshot()
	return snapshot.Raw, snapshot.Effective, snapshot.Version
}

// Prepared reports whether an effective value has ever been published, including
// a successfully resolved nil. Like effective configuration itself, it remains
// available after raw replacement or unload. It does not invoke any callbacks.
func (c *Configuration) Prepared() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.prepared
}

// RetainRaw does not resolve, notify, restart or change the effective value.
func (c *Configuration) RetainRaw(raw any) {
	c.retain(raw)
}

func (c *Configuration) retain(raw any) (version, publication uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.raw = raw
	c.version++
	return c.version, c.publication
}

// ResolveCandidate evaluates raw without retaining or publishing anything.
// Absent a resolver, the candidate is the exact input value.
func (c *Configuration) ResolveCandidate(ctx context.Context, raw any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.hooks.Resolve == nil {
		return raw, nil
	}
	return c.hooks.Resolve(ctx, raw)
}

// PreparedConfig is a worker's resolved activation input. Published=false means
// raw changed or another effective value was published during resolution: the
// runtime must discard that activation attempt, not invoke Apply with a stale
// result or retry automatically here.
type PreparedConfig struct {
	Value     any
	Version   uint64
	Published bool
}

// Prepare snapshots raw, its version and the publication generation, resolves
// outside the cell lock, then publishes only if both counters still match. This
// also prevents a late same-version resolution replacing a newer preparation.
// The actual runtime worker calls it before Apply. Publication precedes Apply
// and is not rolled back if Apply fails, matching Cordis activation. Resolve
// errors propagate, including on a superseded attempt; the cell never retries
// or runs Apply itself.
func (c *Configuration) Prepare(ctx context.Context) (PreparedConfig, error) {
	c.mu.Lock()
	raw, version, publication := c.raw, c.version, c.publication
	c.mu.Unlock()
	value, err := c.ResolveCandidate(ctx, raw)
	prepared := PreparedConfig{Value: value, Version: version}
	if err != nil {
		return prepared, err
	}
	if err := ctx.Err(); err != nil {
		return prepared, err
	}
	c.mu.Lock()
	if c.version == version && c.publication == publication {
		c.effective = value
		c.prepared = true
		c.publication++
		prepared.Published = true
	}
	c.mu.Unlock()
	return prepared, nil
}

// ApplyUpdate resolves loaded candidates before persistence, retains raw and
// clears the runtime's failure, then runs Update (or restart by default). Pending
// updates skip resolution and Update: restart later prepares the retained raw.
// A successful veto publishes the candidate without a runtime revision change;
// rejection retains raw and the previous effective value. A restart, including a
// failed one, never publishes the candidate: a later Prepare owns publication.
//
// stillCurrent checks the caller-captured runtime binding (alive, epoch and
// committed identity) outside the cell lock. Nil means no external constraint.
// It must not rely on this cell to synchronize runtime state. Raw version and
// publication generation are captured together at retention and checked before
// and after it. A reentrant or concurrent Prepare at the same raw version cannot
// be overwritten by this update. The runtime owns scheduling and binding
// validation, not this data cell.
//
// Cancellation is checked before resolution, persistence and update/restart
// callbacks. Once Commit succeeds, raw retention and clearFailure must complete
// even if the context was canceled by Commit; cancellation then returns honestly
// with those effects retained. clearFailure is unconditional bookkeeping after
// retention, not rollback. Its error likewise leaves the new raw retained. A
// successful Update is not retroactively rejected if its context was canceled:
// publication follows the version and runtime-binding guards, as in the source.
func (c *Configuration) ApplyUpdate(ctx context.Context, raw any, noSave, loaded bool, restart func() error, stillCurrent func() bool, clearFailure func() error) error {
	wait, err := c.RequestApplyUpdate(ctx, raw, noSave, loaded, restart, stillCurrent, clearFailure)
	if err != nil {
		return err
	}
	return wait(ctx)
}

// RequestApplyUpdate is ApplyUpdate split at the hook boundary: it returns after
// the synchronous prefix (resolve, persist, retain, clear failure, hook prefix)
// with a waiter for completion. A veto publishes in the first waiter call that
// observes successful completion; repeated calls return that same result and
// never rerun the hook or publish twice. A waiter canceled before completion
// returns its context error and publishes nothing.
func (c *Configuration) RequestApplyUpdate(ctx context.Context, raw any, noSave, loaded bool, restart func() error, stillCurrent func() bool, clearFailure func() error) (func(context.Context) error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var resolved any
	if loaded {
		var err error
		resolved, err = c.ResolveCandidate(ctx, raw)
		if err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !noSave && c.hooks.Commit != nil {
		if err := c.hooks.Commit(ctx, raw); err != nil {
			return nil, err
		}
	}
	version, publication := c.retain(raw)
	if clearFailure != nil {
		if err := clearFailure(); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// This small independent guard protects concurrent next calls without ever
	// holding either mutex across restart or a hook. The hook joins its next.
	var nextMu sync.Mutex
	continued := false
	restarted := false
	next := func() error {
		nextMu.Lock()
		if continued {
			nextMu.Unlock()
			return ErrAlreadyContinued
		}
		continued = true
		if err := ctx.Err(); err != nil {
			nextMu.Unlock()
			return err
		}
		restarted = true // Must precede restart, even when restart returns an error.
		nextMu.Unlock()
		if restart == nil {
			return ErrConfigRestart
		}
		return restart()
	}
	synchronous := func(context.Context) error { return nil }
	if !loaded || (c.hooks.Update == nil && c.hooks.UpdateAsync == nil) {
		return synchronous, next()
	}
	finish := func(err error) error {
		if err != nil {
			return err
		}
		nextMu.Lock()
		didRestart := restarted
		nextMu.Unlock()
		if didRestart {
			return nil
		}
		c.mu.Lock()
		current := c.version == version && c.publication == publication
		c.mu.Unlock()
		if !current || (stillCurrent != nil && !stillCurrent()) {
			return nil
		}
		c.mu.Lock()
		if c.version == version && c.publication == publication {
			c.effective = resolved
			c.prepared = true
			c.publication++
		}
		c.mu.Unlock()
		return nil
	}
	if c.hooks.UpdateAsync == nil {
		return synchronous, finish(c.hooks.Update(ctx, resolved, next))
	}
	hookWait, err := c.hooks.UpdateAsync(ctx, resolved, next)
	if err != nil {
		return nil, err
	}
	var once sync.Once
	var result error
	return func(wait context.Context) error {
		err := hookWait(wait)
		if err != nil && wait.Err() != nil {
			return err // Wait cancellation is not completion; nothing is finalized.
		}
		once.Do(func() { result = finish(err) })
		return result
	}, nil
}
