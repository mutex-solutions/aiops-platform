// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"context"
	"sync/atomic"

	"github.com/mutex-solutions/aiops-platform/test/spike/s0/contextmodel"
)

// Result distinguishes no returned value from a valid zero R. Present means a
// value was supplied, NOT that the built-in behavior ran: an outer listener may
// return its own result without next, exactly as the source waterfall permits.
type Result[R any] struct {
	Value   R
	Present bool
}

type Next[R any] func() (Result[R], error)
type Middleware[A, R any] func(context.Context, A, Next[R]) (Result[R], error)

// Waterfall composes a dispatch-local snapshot outermost first. It must not be
// copied after first use. The owner supplies the innermost behavior explicitly.
// This Go continuation is synchronous; asynchronous transformations must await
// their work inside the callback. It does not detach an implicit Promise chain.
type Waterfall[A, R any] struct {
	definition definition
	listeners  table[Middleware[A, R]]
}

func NewWaterfall[A, R any](spec Definition) (*Waterfall[A, R], error) {
	d, err := newDefinition(spec)
	if err != nil {
		return nil, err
	}
	return &Waterfall[A, R]{definition: d}, nil
}

func (w *Waterfall[A, R]) Use(stage string) (Use, error) { return w.definition.use(stage) }

func (w *Waterfall[A, R]) On(b Binding, stage string, fn Middleware[A, R]) (Disposer, error) {
	if fn == nil {
		return nil, ErrBinding
	}
	return w.listeners.add(&w.definition, b, stage, fn)
}

func (w *Waterfall[A, R]) Run(ctx context.Context, owner Binding, audience contextmodel.Audience, a A, inner func(context.Context, A) (R, error)) (Result[R], error) {
	if err := w.definition.checkOwner(owner); err != nil {
		return Result[R]{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result[R]{}, err
	}
	if inner == nil {
		return Result[R]{}, ErrBinding
	}
	if !owner.Runtime.Enter() {
		return Result[R]{}, ErrInactive
	}
	defer owner.Runtime.Leave()
	listeners, err := w.listeners.selectAudience(audience)
	if err != nil {
		return Result[R]{}, err
	}
	return waterfallCall(ctx, listeners, a, inner)
}

func waterfallCall[A, R any](ctx context.Context, listeners []*registration[Middleware[A, R]], a A, inner func(context.Context, A) (R, error)) (Result[R], error) {
	if err := ctx.Err(); err != nil {
		return Result[R]{}, err
	}
	for len(listeners) > 0 {
		r := listeners[0]
		listeners = listeners[1:]
		if !r.enter(ctx) {
			continue
		}
		defer r.runtime.Leave()
		// 0=available, 1=running, 2=finished, 3=expired. Atomic admission prevents
		// two goroutines from entering the rest of the chain, not just recursion.
		var state atomic.Uint32
		done := make(chan struct{})
		next := Next[R](func() (Result[R], error) {
			if !state.CompareAndSwap(0, 1) {
				if state.Load() == 3 {
					return Result[R]{}, ErrNextExpired
				}
				return Result[R]{}, ErrNext
			}
			defer func() { state.Store(2); close(done) }()
			return waterfallCall(ctx, listeners, a, inner)
		})
		value, err := invoke(func() (Result[R], error) { return r.callback(ctx, a, next) })
		if !state.CompareAndSwap(0, 3) {
			// A callback that launches next concurrently still owns that work. Do
			// not release its lease before that accepted continuation has exited.
			// A callback ignoring cancellation is bounded by runtime drain policy,
			// not by killing its goroutine or falsely releasing this lease.
			<-done
		}
		if err != nil {
			return Result[R]{}, &ListenerError{PluginID: r.pluginID, Cause: err}
		}
		if err := ctx.Err(); err != nil {
			return Result[R]{}, err
		}
		return value, nil
	}
	value, err := invoke(func() (R, error) { return inner(ctx, a) })
	if err != nil {
		return Result[R]{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result[R]{}, err
	}
	return Result[R]{Value: value, Present: true}, nil
}
