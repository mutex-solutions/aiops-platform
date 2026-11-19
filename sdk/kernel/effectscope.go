// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import "context"

// The native effect scope: the request/handle extension that the owner accepted
// (ADR-034, "Accepted native effect-scope request/handle extension"; the design
// note docs/design/s0-native-effect-scope-proposal.md). Effect, Defer and Go
// remain. A scope request is nonblocking: setup, incremental rollback and cleanup
// run in the fiber's one transition lane, and the external AwaitSetup and Await
// observe their real completion. No call here waits for its own lane.

// EffectScope is the scope handed to a setup callback. It owns incrementally what
// that callback acquires. Defer is admissible until setup seals, and Scope runs a
// nested setup on the current transition worker: setup must not move nested calls
// to another goroutine.
type EffectScope = effectScope

// EffectHandle observes one requested scope. It holds no caller-mutable state.
// AwaitSetup and Await must be called outside callbacks that run on the scope's
// own transition lane.
type EffectHandle = effectHandle

// EffectMeta is a detached snapshot of the ownership tree of a scope: a label, a
// state ("pending", "setup", "ready", "withdraw", "cleaning" or "done") and the
// children in registration order. Cleanup runs in the opposite order.
type EffectMeta = effectMeta

// ScopePanicError is the error of a scope callback that panicked, with the panic
// value; it unwraps to the value when that is an error.
type ScopePanicError = scopePanicError

// The states of an EffectMeta.
const (
	ScopePending  = scopePending
	ScopeSetup    = scopeSetup
	ScopeReady    = scopeReady
	ScopeWithdraw = scopeWithdraw
	ScopeCleaning = scopeCleaning
	ScopeDone     = scopeFinished
)

var (
	// ErrScopeSealed reports a registration on a scope whose setup has sealed.
	ErrScopeSealed = errScopeSealed
	// ErrScopeStarted reports a scope whose setup already started.
	ErrScopeStarted = errScopeStarted
)

// RequestEffectScope admits setup as a scope of c's fiber and returns its handle
// at once, without running setup. A request from Apply is not awaited there:
// setup runs after Apply returns, before the fiber is ACTIVE. It returns an error
// that matches ErrInactiveEffect and ErrInactive on an inactive activation.
func (c *Context) RequestEffectScope(label string, setup func(*EffectScope) error) (*EffectHandle, error) {
	return c.h.requestScope(label, setup)
}

// Defer takes d into the scope as one owned child; it is refused with
// ErrScopeSealed once setup has sealed.
func (s *EffectScope) Defer(label string, d Disposer) error { return s.tryDefer(label, d) }

// Scope runs setup as a nested scope of s, synchronously on the current
// transition worker. Its failure rolls the nested scope back in reverse order.
func (s *EffectScope) Scope(label string, setup func(*EffectScope) error) error {
	return s.scope(label, setup)
}

// AwaitSetup returns when setup has finished, with its error, rollback errors
// included.
func (h *EffectHandle) AwaitSetup(ctx context.Context) error { return h.awaitSetup(ctx) }

// Await returns when every owned disposer has actually returned, with the joined
// cleanup errors.
func (h *EffectHandle) Await(ctx context.Context) error { return h.await(ctx) }

// RequestWithdraw asks for the scope's cleanup. It is idempotent and never runs
// cleanup inline; use Await for its completion.
func (h *EffectHandle) RequestWithdraw() error { return h.requestWithdrawal() }

// Metadata returns a detached snapshot of the scope's ownership tree.
func (h *EffectHandle) Metadata() EffectMeta { return h.metadata() }

// Dispose requests disposal of c's fiber and returns once the request is accepted,
// never when it completes: a transition does not wait for itself, so a plugin that
// calls it from Apply or a callback returns, and an external caller awaits the
// result with Fiber.Await (ADR-034, "Accepted clarification (owner confirmation,
// 2026-11-12)"). On the root context it returns ErrRootDispose, and on an activation
// that has ended it returns ErrInactive.
func (c *Context) Dispose() error { return c.h.requestDispose() }
