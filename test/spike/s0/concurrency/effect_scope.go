// SPDX-License-Identifier: Apache-2.0

package concurrency

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

var (
	ErrScopeUnavailable = errors.New("native effect scopes unsupported")
	ErrScopeSealed      = errors.New("effect scope setup is sealed")
	ErrScopeStarted     = errors.New("effect scope setup already started")
)

const (
	ScopePending  = "pending"
	ScopeSetup    = "setup"
	ScopeReady    = "ready"
	ScopeWithdraw = "withdraw"
	ScopeCleaning = "cleaning"
	ScopeDone     = "done"
)

// EffectMeta is a detached snapshot of actual scope/disposer ownership. Children
// retain registration order; their cleanup executes in the opposite order.
// Changing a returned snapshot cannot change the collector or later snapshots.
type EffectMeta struct {
	Label    string
	State    string
	Children []EffectMeta
}

// ScopePanicError retains the original callback panic, including an error cause.
// Formatting is deliberately deferred to Error, never done by metadata readers.
type ScopePanicError struct{ Value any }

func (e *ScopePanicError) Error() string {
	return fmt.Sprintf("effect scope callback panic: %v", e.Value)
}
func (e *ScopePanicError) Unwrap() error {
	cause, _ := e.Value.(error)
	return cause
}

// RequestEffectScope reserves native ownership, not an explicit call-path lease.
// The activation's existing transition worker later runs setup and cleanup. The
// native admission hook revalidates this exact activation after this fast check.
// Completion waits are external-only: a callback must not await its own lane.
func (c *Context) RequestEffectScope(label string, setup func(*EffectScope) error) (*EffectHandle, error) {
	if err := validateScope(label, setup); err != nil {
		return nil, err
	}
	if c == nil || c.scopeRequest == nil {
		return nil, ErrScopeUnavailable
	}
	if !c.live.Load() || (c.std != nil && c.std.Err() != nil) {
		return nil, errors.Join(ErrInactiveEffect, ErrInactive)
	}
	return c.scopeRequest(label, setup)
}

// EffectScope incrementally owns resources acquired by one setup callback.
// Defer remains admissible until setup seals, even if the activation's standard
// context was canceled in the meantime. Scope runs nested setup synchronously on
// the current transition worker; it does not schedule work or start a goroutine.
// Setup callbacks must not move nested Scope calls to another goroutine.
type EffectScope struct {
	effect *scopeEffect
	node   *scopeNode
}

func (s *EffectScope) Defer(label string, d Disposer) error {
	if label == "" || d == nil {
		return errors.New("scope disposer requires a nonempty label and nonnil callback")
	}
	e := s.effect
	e.mu.Lock()
	defer e.mu.Unlock()
	if s.node.sealed {
		return ErrScopeSealed
	}
	s.node.children = append(s.node.children, &scopeNode{label: label, state: ScopeReady, sealed: true, dispose: d})
	e.publishLocked()
	return nil
}

func (s *EffectScope) Scope(label string, setup func(*EffectScope) error) error {
	if err := validateScope(label, setup); err != nil {
		return err
	}
	e := s.effect
	e.mu.Lock()
	if s.node.sealed {
		e.mu.Unlock()
		return ErrScopeSealed
	}
	node := &scopeNode{label: label, state: ScopeSetup}
	s.node.children = append(s.node.children, node)
	e.publishLocked() // Ownership is visible before the nested callback starts.
	e.mu.Unlock()
	return e.setupNode(node, setup)
}

// EffectHandle observes completion of one native scope identity. It contains no
// caller-mutable ownership or scheduling state. AwaitSetup and Await may only be
// called outside callbacks running on the scope's own transition lane.
type EffectHandle struct{ effect *scopeEffect }

func (h *EffectHandle) AwaitSetup(ctx context.Context) error {
	if h == nil || h.effect == nil {
		return ErrScopeUnavailable
	}
	if err := awaitScope(ctx, h.effect.setupDone); err != nil {
		return err
	}
	return h.effect.setupErr
}

// RequestWithdraw only requests native control work, never inline cleanup. The
// native hook is responsible for idempotence and unload/admission serialization.
func (h *EffectHandle) RequestWithdraw() error {
	if h == nil || h.effect == nil || h.effect.requestWithdraw == nil {
		return ErrScopeUnavailable
	}
	return h.effect.requestWithdraw()
}

// Await returns cleanup errors after every owned callback has actually returned.
// Setup errors (including its rollback errors) are observed through AwaitSetup.
func (h *EffectHandle) Await(ctx context.Context) error {
	if h == nil || h.effect == nil {
		return ErrScopeUnavailable
	}
	if err := awaitScope(ctx, h.effect.done); err != nil {
		return err
	}
	return h.effect.cleanupErr
}

func (h *EffectHandle) Metadata() EffectMeta {
	if h == nil || h.effect == nil {
		return EffectMeta{}
	}
	return h.effect.metadata()
}

func awaitScope(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	default:
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// scopeEffect is shared work, not a scheduler. Native owners reserve and remove
// its exact pointer in their ledger and bind requestWithdraw before publication.
// They run work only on the fiber's existing, nonoverlapping transition lane.
// No queued scope holds an Enter lease or owns a per-scope goroutine.
type scopeEffect struct {
	handle          *EffectHandle
	requestWithdraw func() error
	withdraw        atomic.Bool
	started         atomic.Bool
	cleaning        atomic.Bool
	setupDone       chan struct{}
	done            chan struct{}
	setupErr        error // Immutable after setupDone closes.
	cleanupErr      error // Immutable after done closes.
	setup           func(*EffectScope) error
	mu              sync.Mutex
	root            *scopeNode
	meta            atomic.Pointer[EffectMeta]
	run             *scopeRun // Used only by the current setup worker.
}

type scopeNode struct {
	label    string
	state    string
	sealed   bool
	children []*scopeNode
	dispose  Disposer
}

// The factory is called lazily at the first setup failure. Nested and root
// rollback share that one native budget, rather than renewing it per resource.
type scopeRun struct {
	factory func() (context.Context, context.CancelFunc)
	ctx     context.Context
	cancel  context.CancelFunc
}

func newScopeEffect(label string, setup func(*EffectScope) error) *scopeEffect {
	e := &scopeEffect{setup: setup, root: &scopeNode{label: label, state: ScopePending},
		setupDone: make(chan struct{}), done: make(chan struct{})}
	e.handle = &EffectHandle{effect: e}
	e.publishLocked()
	return e
}

// runSetup runs exactly once. Setup uses the captured activation context, not a
// newly exposed scope context; the first argument records the native work seam.
// On failure, actual rollback finishes before setupDone is published. The native
// caller supplies its existing disposal-budget factory, not a new timeout knob.
func (e *scopeEffect) runSetup(_ context.Context, cleanupFactory func() (context.Context, context.CancelFunc)) error {
	if !e.started.CompareAndSwap(false, true) {
		return ErrScopeStarted
	}
	if cleanupFactory == nil {
		e.setup = nil
		e.setupErr = ErrScopeUnavailable
		e.setState(e.root, ScopeDone, true)
		e.cleanupErr = ErrScopeUnavailable
		e.cleaning.Store(true)
		close(e.done)
		close(e.setupDone)
		return e.setupErr
	}
	e.run = &scopeRun{factory: cleanupFactory}
	defer func() {
		if e.run.cancel != nil {
			e.run.cancel()
		}
		e.run = nil
	}()
	e.setState(e.root, ScopeSetup, false)
	err := invokeScope(func() error { return e.setup(&EffectScope{effect: e, node: e.root}) })
	e.setup = nil // A retained handle must not retain the completed setup closure.
	e.setState(e.root, ScopeReady, true)
	if err != nil {
		err = errors.Join(err, e.cleanupOnce(e.rollbackContext()))
	}
	e.setupErr = err
	close(e.setupDone)
	return err
}

func (e *scopeEffect) setupNode(node *scopeNode, setup func(*EffectScope) error) error {
	err := invokeScope(func() error { return setup(&EffectScope{effect: e, node: node}) })
	e.setState(node, ScopeReady, true)
	if err != nil {
		err = errors.Join(err, e.cleanupNode(e.rollbackContext(), node))
	}
	return err
}

func (e *scopeEffect) rollbackContext() context.Context {
	if e.run.ctx == nil {
		e.run.ctx, e.run.cancel = e.run.factory()
	}
	return e.run.ctx
}

// cancelPending seals an admitted scope whose activation quiesced before setup.
// The native worker calls this instead of running a late setup callback. A false
// result means setup already started and must retain ownership until it returns.
func (e *scopeEffect) cancelPending(err error) bool {
	if !e.started.CompareAndSwap(false, true) {
		return false
	}
	if err == nil || err == ErrInactive || err == ErrInactiveEffect {
		err = errors.Join(ErrInactiveEffect, ErrInactive, err)
	}
	e.setup = nil
	e.setupErr, e.cleanupErr = err, err
	e.cleaning.Store(true)
	e.setState(e.root, ScopeDone, true)
	close(e.done)
	close(e.setupDone)
	return true
}

// runCleanup is idempotent but must not be scheduled concurrently by a native
// owner. A duplicate invocation joins real completion instead of releasing the
// ownership slot early. All callbacks receive the one context supplied by core.
func (e *scopeEffect) runCleanup(ctx context.Context) error {
	<-e.setupDone
	return e.cleanupOnce(ctx)
}

func (e *scopeEffect) cleanupOnce(ctx context.Context) error {
	if !e.cleaning.CompareAndSwap(false, true) {
		<-e.done
		return e.cleanupErr
	}
	e.cleanupErr = e.cleanupNode(ctx, e.root)
	close(e.done)
	return e.cleanupErr
}

func (e *scopeEffect) cleanupNode(ctx context.Context, node *scopeNode) error {
	e.mu.Lock()
	if node.state == ScopeDone {
		e.mu.Unlock()
		return nil
	}
	node.state, node.sealed = ScopeCleaning, true
	dispose := node.dispose
	children := append([]*scopeNode(nil), node.children...)
	e.publishLocked()
	e.mu.Unlock()
	var result error
	for i := len(children) - 1; i >= 0; i-- {
		result = errors.Join(result, e.cleanupNode(ctx, children[i]))
	}
	if dispose != nil {
		result = errors.Join(result, invokeScope(func() error { return dispose(ctx) }))
	}
	// Cancellation is diagnostic, never evidence that callbacks have returned.
	result = errors.Join(result, ctx.Err())
	e.mu.Lock()
	node.dispose = nil // Keep metadata, not the resource's completed callback.
	node.state = ScopeDone
	e.publishLocked()
	e.mu.Unlock()
	return result
}

func invokeScope(fn func() error) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = &ScopePanicError{Value: value}
		}
	}()
	return fn()
}

func validateScope(label string, setup func(*EffectScope) error) error {
	if label == "" || setup == nil {
		return errors.New("effect scope requires a nonempty label and nonnil setup")
	}
	return nil
}

func (e *scopeEffect) setState(node *scopeNode, state string, sealed bool) {
	e.mu.Lock()
	node.state, node.sealed = state, sealed
	e.publishLocked()
	e.mu.Unlock()
}

func (e *scopeEffect) markWithdraw() {
	e.withdraw.Store(true)
	e.mu.Lock()
	e.publishLocked()
	e.mu.Unlock()
}

func (e *scopeEffect) pendingWithdraw() bool { return e.withdraw.Load() }
func (e *scopeEffect) setupFinished() bool   { return scopeClosed(e.setupDone) }
func (e *scopeEffect) cleanupFinished() bool { return scopeClosed(e.done) }

func scopeClosed(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func (e *scopeEffect) publishLocked() {
	meta := scopeMetadata(e.root)
	if e.withdraw.Load() && meta.State != ScopeCleaning && meta.State != ScopeDone {
		meta.State = ScopeWithdraw
	}
	e.meta.Store(&meta)
}

func scopeMetadata(node *scopeNode) EffectMeta {
	meta := EffectMeta{Label: node.label, State: node.state}
	for _, child := range node.children {
		meta.Children = append(meta.Children, scopeMetadata(child))
	}
	return meta
}

func (e *scopeEffect) metadata() EffectMeta { return cloneEffectMeta(*e.meta.Load()) }

func cloneEffectMeta(meta EffectMeta) EffectMeta {
	children := meta.Children
	meta.Children = nil
	for _, child := range children {
		meta.Children = append(meta.Children, cloneEffectMeta(child))
	}
	return meta
}
