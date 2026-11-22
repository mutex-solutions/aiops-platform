// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"sync"
)

// EventListener is the function of an Event listener. Its value matters only to
// Serial and Bail: nil or false lets the chain go on, any other value, zero
// values included, stops it and is the result, as in Cordis. The contract gives
// the listener no shape (07-c4-plugin-sdk.md:208); the prototype's Decision is
// not carried.
type EventListener[A any] func(ctx context.Context, a A) (any, error)

// Event is a hook with four dispatch modes. Observers cannot change an outcome: a
// listener that errors, panics or runs past the dispatch deadline is skipped and
// logged to Config.ObserverLog (07-c4-plugin-sdk.md:208, :385). Build it with
// NewEvent; it must not be copied.
type Event[A any] struct {
	def   *hookDef
	table table[EventListener[A]]
}

// NewEvent declares an event hook named "<owner noun>/<event>"
// (07-c4-plugin-sdk.md:381, :385). It panics on a bad definition, and on one with
// no OwnedBy (ADR-034 owner decision 10). The hook value cannot dispatch: its
// owner calls Bind.
func NewEvent[A any](name string, opts ...HookOption) *Event[A] {
	return newEvent[A](name, false, opts...)
}

func newEvent[A any](name string, kernel bool, opts ...HookOption) *Event[A] {
	return &Event[A]{def: newHookDef(name, kernel, opts)}
}

// Name returns the hook's name.
func (e *Event[A]) Name() string { return e.def.name }

// Owner returns the key given to OwnedBy, nil for a kernel hook
// (07-c4-plugin-sdk.md:439; ADR-034 owner decision 10).
func (e *Event[A]) Owner() AnyKey {
	if e.def.owner == nil {
		return nil
	}
	return e.def.owner
}

func (e *Event[A]) anyHook() *hookDef {
	if e == nil {
		return nil
	}
	return e.def
}

// EventDispatcher dispatches one Event for the activation that bound it. Only
// Bind makes one, so only the fiber that provides the hook's owning key holds one
// (07-c4-plugin-sdk.md:208, :439; ADR-034 owner decision 10). It dies with that
// activation: once the fiber is quiescing, disposed or restarted, every method
// returns an error that matches ErrInactive and calls no listener. Its zero value
// is dead. It is a small value and safe for concurrent use.
type EventDispatcher[A any] struct {
	e *Event[A]
	c *Context
}

// Bind returns the dispatcher of e for c's fiber. The owner calls it once in Apply
// (07-c4-plugin-sdk.md:439; ADR-034 owner decision 10). c's fiber must provide
// the hook's owning key: its entry is admitted for the key by the provider binding
// map when the key is provider-bound, and it declared the key in Plugin.Provides,
// the same two checks Key.Provide makes. A refused call, with ErrNotOwner, ErrHook
// or ErrInactiveEffect, panics and fails the fiber.
func (e *Event[A]) Bind(c *Context) EventDispatcher[A] {
	e.def.bind(c)
	return EventDispatcher[A]{e: e, c: c}
}

// enter takes the activation's lease for one dispatch.
func (d EventDispatcher[A]) enter() (func(), error) {
	if d.e == nil {
		return nil, ErrInactive
	}
	return d.e.def.enter(d.c)
}

// Use returns the HookUse that a plugin lists in Plugin.Hooks to listen on stage
// (07-c4-plugin-sdk.md:219, :411).
func (e *Event[A]) Use(stage string) HookUse { return e.def.use(stage) }

// On registers fn as a listener of stage, as an effect of c's fiber: it leaves the
// chain with the fiber (07-c4-plugin-sdk.md:208, :369, :414). The stage must be
// declared by the hook and listed in the fiber's Plugin.Hooks (:439). A refused
// call, such as an undeclared one, panics and fails the fiber, with ErrStage,
// ErrHookUndeclared, ErrSealed, ErrHook or ErrInactiveEffect.
func (e *Event[A]) On(c *Context, stage string, fn EventListener[A]) {
	e.table.add(e.def, c, stage, fn, fn == nil)
	if e.def.kernel {
		c.root.startRelay()
	}
}

// callEvent runs one listener under a lease of its fiber. A fiber that is
// quiescing is skipped without a result and without an error.
func callEvent[A any](ctx context.Context, r *listener[EventListener[A]], a A) (v any, err error) {
	leave, ok := r.lease()
	if !ok {
		return nil, nil
	}
	defer leave()
	defer func() {
		if p := recover(); p != nil {
			v, err = nil, &PanicError{Value: p}
		}
	}()
	return r.fn(ctx, a)
}

func bailed(v any) bool { return v != nil && v != false }

// sequence calls the listeners one after the other in chain order; when bail is
// set it stops at the first stopping value.
func (e *Event[A]) sequence(ctx context.Context, aud Audience, a A, bail bool) (any, error) {
	if err := e.def.dispatchable(ctx, aud); err != nil {
		return nil, err
	}
	var buf [8]*listener[EventListener[A]]
	list, err := e.table.pick(aud, buf[:0])
	if err != nil {
		return nil, err
	}
	for _, r := range list {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		v, err := callEvent(ctx, r, a)
		if err != nil {
			r.fail(err)
			continue
		}
		if bail && bailed(v) {
			return v, nil
		}
	}
	return nil, ctx.Err()
}

// Emit calls every listener reached by aud, one after the other in chain order,
// and ignores their values. It returns ctx's error if ctx ends first
// (07-c4-plugin-sdk.md:208, :385, :440).
func (d EventDispatcher[A]) Emit(ctx context.Context, aud Audience, a A) error {
	leave, err := d.enter()
	if err != nil {
		return err
	}
	defer leave()
	return d.e.emit(ctx, aud, a)
}

// emit is Emit with no check of a dispatcher: the kernel root's own path
// (config.go), which plugins cannot reach.
func (e *Event[A]) emit(ctx context.Context, aud Audience, a A) error {
	_, err := e.sequence(ctx, aud, a, false)
	return err
}

// Parallel calls every listener reached by aud at the same time, each in its own
// goroutine, and returns when all have returned. The goroutines are bounded by the
// dispatch and each runs under its fiber's lease (07-c4-plugin-sdk.md:208, :385).
func (d EventDispatcher[A]) Parallel(ctx context.Context, aud Audience, a A) error {
	leave, err := d.enter()
	if err != nil {
		return err
	}
	defer leave()
	return d.e.parallel(ctx, aud, a)
}

func (e *Event[A]) parallel(ctx context.Context, aud Audience, a A) error {
	if err := e.def.dispatchable(ctx, aud); err != nil {
		return err
	}
	list, err := e.table.pick(aud, nil)
	if err != nil {
		return err
	}
	run := func(r *listener[EventListener[A]]) {
		if _, err := callEvent(ctx, r, a); err != nil {
			r.fail(err)
		}
	}
	if len(list) == 1 {
		run(list[0])
		return ctx.Err()
	}
	var wg sync.WaitGroup
	wg.Add(len(list))
	for _, r := range list {
		go func() { defer wg.Done(); run(r) }()
	}
	wg.Wait()
	return ctx.Err()
}

// Serial calls the listeners one after the other in chain order and stops at the
// first stopping value, which it returns; nil means none stopped
// (07-c4-plugin-sdk.md:208, :385). Listeners are synchronous, so it behaves as Bail
// does: see the open points of the report.
func (d EventDispatcher[A]) Serial(ctx context.Context, aud Audience, a A) (any, error) {
	return d.bail(ctx, aud, a)
}

// Bail calls the listeners one after the other in chain order and stops at the
// first stopping value, which it returns; nil means none stopped
// (07-c4-plugin-sdk.md:208, :385).
func (d EventDispatcher[A]) Bail(ctx context.Context, aud Audience, a A) (any, error) {
	return d.bail(ctx, aud, a)
}

func (d EventDispatcher[A]) bail(ctx context.Context, aud Audience, a A) (any, error) {
	leave, err := d.enter()
	if err != nil {
		return nil, err
	}
	defer leave()
	return d.e.sequence(ctx, aud, a, true)
}
