// SPDX-License-Identifier: Apache-2.0
// Lifecycle semantics informed by Cordis, copyright Shigma and DeepSeek.
// The adapted-source MIT notices are preserved in NOTICE.

package concurrency

import (
	"context"
	"errors"
	"fmt"
)

// ErrTrackedUnavailable means that this Context has no native tracked-work bridge.
// Go never substitutes an untracked goroutine or a private diagnostic ledger.
var ErrTrackedUnavailable = errors.New("native tracked work unsupported")

// Go starts work owned by this exact activation, after the scheduler registers
// its label in the native tracked-work ledger. Labels and run must be nonempty
// and nonnil respectively; invalid arguments are programming errors, not an
// availability policy. This error-returning API is an S0 prototype only.
//
// The worker holds an activation lease until run actually exits. Quiescence
// cancels Std and rejects new work, but cannot release effects or dependency
// bindings while an existing worker is blocked. run must cooperate with Std's
// cancellation; there is no timeout that makes a stuck worker cease to exist.
//
// A returned error or recovered panic is reported through the native completion
// hook before releasing the lease. It fails the activation, including one that
// already became ACTIVE. A wrapped context.Canceled is normal completion only
// when Std is itself canceled. Other errors, including DeadlineExceeded, fail;
// workers may instead return nil after handling shutdown themselves.
func (c *Context) Go(label string, run func(context.Context) error) error {
	if label == "" {
		return errors.New("tracked goroutine label must not be empty")
	}
	if run == nil {
		return errors.New("tracked goroutine run must not be nil")
	}
	if !c.Enter() {
		return ErrInactive
	}
	if c.trackedStart == nil {
		c.Leave()
		return ErrTrackedUnavailable
	}
	done, err := c.trackedStart(label)
	if err != nil {
		c.Leave()
		return err
	}
	go func() {
		// Completion is synchronous with the native state owner. Reporting before
		// Leave prevents the final drain wake from outrunning a worker failure.
		defer c.Leave()
		done(runTracked(c.Std(), label, run))
	}()
	return nil
}

// runTracked executes callback code, error matching, and panic formatting on the
// worker, never under a native graph lock or on a reconciler state owner.
func runTracked(ctx context.Context, label string, run func(context.Context) error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("tracked goroutine %q panic: %v", label, p)
		}
	}()
	err = run(ctx)
	if ctx.Err() != nil && errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
