// SPDX-License-Identifier: Apache-2.0
// Lifecycle semantics informed by Cordis, copyright Shigma and DeepSeek.
// The adapted-source MIT notices are preserved in NOTICE.

package concurrency

import "errors"

// ErrInactiveEffect identifies an Effect whose activation no longer accepts
// effects. Such errors also match ErrInactive; Defer retains its native error.
var ErrInactiveEffect = errors.New("cannot create effect on inactive context")

// ErrEffectUnavailable means that this Context has no native cleanup bridge.
// Effect never substitutes a private cleanup deadline or an unowned disposer.
var ErrEffectUnavailable = errors.New("native effect cleanup unsupported")

// Effect runs setup on the calling goroutine and registers its returned disposer
// in this activation's native LIFO effect ledger. A nonempty label and nonnil
// setup are required; invalid arguments are programming errors. A nil returned
// disposer owns no resource and adds no placeholder to the ledger.
//
// An activation lease is acquired before setup and retained until setup and any
// immediate cleanup actually return. On setup failure, a returned disposer runs
// immediately, before the setup error is returned. If quiescence instead rejects
// registration, the returned disposer likewise runs immediately and the error
// matches both ErrInactiveEffect and ErrInactive. Immediate cleanup uses the
// native runtime's disposal deadline and panic boundary, outside its state owner;
// setup and cleanup errors are joined without losing their error identities.
//
// Setup may block the caller. Effect creates no goroutine: use Context.Go when
// asynchronous setup itself must be owned by the activation. Std cancellation
// does not mean setup or cleanup has returned, and never releases this lease.
//
// A setup panic propagates after releasing the lease to the caller's boundary
// (for example Apply or Context.Go). Ordinary Go cannot recover a disposer that
// setup never returned. Resources registered individually through c.Defer remain
// owned by the activation and unwind normally; Effect is not a generator API or
// a collector for unreturned partial resources. Defers registered inside setup
// precede its returned disposer in the native ledger, so that disposer runs first.
func (c *Context) Effect(label string, setup func() (Disposer, error)) error {
	if label == "" {
		return errors.New("effect label must not be empty")
	}
	if setup == nil {
		return errors.New("effect setup must not be nil")
	}
	if !c.Enter() {
		return errors.Join(ErrInactiveEffect, ErrInactive)
	}
	defer c.Leave()
	if c.cleanupEffect == nil {
		return ErrEffectUnavailable
	}

	dispose, err := setup()
	if err != nil {
		if dispose != nil {
			return errors.Join(err, c.cleanupEffect(dispose))
		}
		return err
	}
	if dispose == nil {
		return nil
	}
	if err := c.Defer(label, dispose); err != nil {
		cleanupErr := c.cleanupEffect(dispose)
		if errors.Is(err, ErrInactive) {
			return errors.Join(ErrInactiveEffect, err, cleanupErr)
		}
		return errors.Join(err, cleanupErr)
	}
	return nil
}
