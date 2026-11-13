// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/mutex-solutions/aiops-platform/test/spike/s0/contextmodel"
)

// Decision is the explicit Go equivalent of Cordis' non-null/non-false/non-
// undefined bail result. Stop, not Go truthiness or Value's zero value, controls
// stopping: 0 and "" can stop, and a listener may explicitly continue with false.
// Event's single type parameter is its payload; the bail value remains opaque.
// Future, if present, is already-started work: Emit does not await it, Bail stops
// immediately on the Future object, and Serial/Parallel await its final decision.
type Decision struct {
	Value  any
	Stop   bool
	Future Future
}

type Listener[A any] func(context.Context, A) (Decision, error)

// Event stores no payload or mutable dispatch state. Every call takes an explicit
// audience; there is no global/filter-bypass registration or dispatch overload.
// It must not be copied after first use. These are raw source-style dispatches:
// errors surface rather than silently becoming skip-and-log observer success.
type Event[A any] struct {
	definition definition
	listeners  table[Listener[A]]
}

func NewEvent[A any](spec Definition) (*Event[A], error) {
	d, err := newDefinition(spec)
	if err != nil {
		return nil, err
	}
	return &Event[A]{definition: d}, nil
}

func (e *Event[A]) Use(stage string) (Use, error) { return e.definition.use(stage) }

// On requires real lifecycle setup ownership, even when the returned disposer is
// retained by a caller. The Defer callback may not be omitted as a convenience.
func (e *Event[A]) On(b Binding, stage string, fn Listener[A]) (Disposer, error) {
	if fn == nil {
		return nil, ErrBinding
	}
	return e.listeners.add(&e.definition, b, stage, fn)
}

func eventCall[A any](ctx context.Context, r *registration[Listener[A]], a A) (Decision, error) {
	if !r.enter(ctx) {
		return Decision{}, nil
	}
	value, err := invoke(func() (Decision, error) { return r.callback(ctx, a) })
	if value.Future == nil {
		r.runtime.Leave()
	} else {
		// Transfer the lease to completion of the actual operation, not Await's
		// possibly earlier cancellation. The engine starts no waiter goroutine.
		// A custom Future must obey the OnSettled contract (trusted bridge data).
		var once sync.Once
		_, attachErr := invoke(func() (struct{}, error) {
			value.Future.OnSettled(func() { once.Do(r.runtime.Leave) })
			return struct{}{}, nil
		})
		// OnSettled panicking does not establish that work exited. Conservatively
		// retain the lease; a real runtime's drain deadline owns this contract fault.
		err = errors.Join(err, attachErr)
	}
	if err != nil {
		return Decision{}, &ListenerError{PluginID: r.pluginID, Cause: err}
	}
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	return value, nil
}

func await(ctx context.Context, decision Decision) (Decision, error) {
	if decision.Future == nil {
		return decision, nil
	}
	value, err := invoke(func() (Decision, error) { return decision.Future.Await(ctx) })
	// Future promises a final (flattened) result; there is no implicit recursive
	// Promise-resolution implementation or arbitrary nesting limit in this probe.
	if err == nil && value.Future != nil {
		return Decision{}, fmt.Errorf("%w: Future returned another Future", ErrBinding)
	}
	return value, err
}

func (e *Event[A]) enter(ctx context.Context, owner Binding) error {
	if err := e.definition.checkOwner(owner); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !owner.Runtime.Enter() {
		return ErrInactive
	}
	return nil
}

// Emit invokes listener prefixes synchronously in declared stage/plugin order,
// ignores decisions, never awaits a Future, and stops on the first synchronous
// error (the pinned source's exception path). It starts no goroutine and cannot
// preempt a Go callback that blocks instead of returning an already-started
// Future. Canonical observer skip/log containment is a separate documented gap.
func (e *Event[A]) Emit(ctx context.Context, owner Binding, audience contextmodel.Audience, a A) error {
	if err := e.enter(ctx, owner); err != nil {
		return err
	}
	defer owner.Runtime.Leave()
	listeners, err := e.listeners.selectAudience(audience)
	if err != nil {
		return err
	}
	for _, r := range listeners {
		if _, err := eventCall(ctx, r, a); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// Parallel starts listener prefixes in snapshot order, then waits for every
// started Future before joining failures in snapshot order, not settlement order.
// The work represented by Futures runs concurrently; callbacks must return a
// Future rather than block if that is needed. No per-listener waiter is spawned.
func (e *Event[A]) Parallel(ctx context.Context, owner Binding, audience contextmodel.Audience, a A) error {
	if err := e.enter(ctx, owner); err != nil {
		return err
	}
	defer owner.Runtime.Leave()
	listeners, err := e.listeners.selectAudience(audience)
	if err != nil {
		return err
	}
	results := make([]Decision, len(listeners))
	errs := make([]error, len(listeners))
	for i, r := range listeners {
		results[i], errs[i] = eventCall(ctx, r, a)
	}
	for i, result := range results {
		if errs[i] != nil {
			continue
		}
		if _, err := await(ctx, result); err != nil {
			errs[i] = &ListenerError{PluginID: listeners[i].pluginID, Cause: err}
		}
	}
	return aggregateFailures(errs, ctx.Err())
}

// Serial awaits each listener's final result in order and stops at an explicit
// bail or first synchronous/asynchronous error. False/null/undefined map to a
// Decision with Stop false; zero and empty string may explicitly have Stop true.
func (e *Event[A]) Serial(ctx context.Context, owner Binding, audience contextmodel.Audience, a A) (Decision, error) {
	return e.bail(ctx, owner, audience, a, true)
}

// Bail stops immediately on a synchronous Stop or a Future object; like the
// pinned source it does not await that Future to find out its eventual value.
func (e *Event[A]) Bail(ctx context.Context, owner Binding, audience contextmodel.Audience, a A) (Decision, error) {
	return e.bail(ctx, owner, audience, a, false)
}

func (e *Event[A]) bail(ctx context.Context, owner Binding, audience contextmodel.Audience, a A, serial bool) (Decision, error) {
	if err := e.enter(ctx, owner); err != nil {
		return Decision{}, err
	}
	defer owner.Runtime.Leave()
	listeners, err := e.listeners.selectAudience(audience)
	if err != nil {
		return Decision{}, err
	}
	for _, r := range listeners {
		result, err := eventCall(ctx, r, a)
		if err != nil {
			return Decision{}, err
		}
		if serial {
			result, err = await(ctx, result)
			if err != nil {
				return Decision{}, &ListenerError{PluginID: r.pluginID, Cause: err}
			}
		} else if result.Future != nil {
			result.Value, result.Stop = result.Future, true
		}
		if result.Stop {
			return result, nil
		}
	}
	return Decision{}, ctx.Err()
}
