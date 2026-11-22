// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"slices"
	"sync"
)

// Next continues a waterfall with the next listener inward, or past the last
// listener with the zero R. It is valid until its listener returns and may be
// called once (07-c4-plugin-sdk.md:209, :386).
type Next[R any] func() (R, error)

// WaterfallListener is the function of a Waterfall listener. A listener that does
// not call next vetoes the rest of the chain, built-in behaviour included
// (07-c4-plugin-sdk.md:209, :386).
type WaterfallListener[A, R any] func(ctx context.Context, a A, next Next[R]) (R, error)

// Waterfall is a hook whose listeners wrap one another, outermost first
// (07-c4-plugin-sdk.md:209, :386). The owner's built-in behaviour is the owner's
// own innermost listener: the contract gives Run no inner function. Build it with
// NewWaterfall; it must not be copied.
type Waterfall[A, R any] struct {
	def   *hookDef
	table table[WaterfallListener[A, R]]
}

// NewWaterfall declares a waterfall hook named "<owner noun>/<event>"
// (07-c4-plugin-sdk.md:381, :386, :627). It panics on a bad definition, and on one
// with no OwnedBy (ADR-034 owner decision 10). The hook value cannot dispatch: its
// owner calls Bind.
func NewWaterfall[A, R any](name string, opts ...HookOption) *Waterfall[A, R] {
	return newWaterfall[A, R](name, false, opts...)
}

func newWaterfall[A, R any](name string, kernel bool, opts ...HookOption) *Waterfall[A, R] {
	return &Waterfall[A, R]{def: newHookDef(name, kernel, opts)}
}

// Name returns the hook's name.
func (w *Waterfall[A, R]) Name() string { return w.def.name }

// Owner returns the key given to OwnedBy, nil for a kernel hook
// (07-c4-plugin-sdk.md:439; ADR-034 owner decision 10).
func (w *Waterfall[A, R]) Owner() AnyKey {
	if w.def.owner == nil {
		return nil
	}
	return w.def.owner
}

func (w *Waterfall[A, R]) anyHook() *hookDef {
	if w == nil {
		return nil
	}
	return w.def
}

// WaterfallDispatcher runs one Waterfall for the activation that bound it. Only
// Bind makes one, so only the fiber that provides the hook's owning key holds one
// (07-c4-plugin-sdk.md:209, :439; ADR-034 owner decision 10). It dies with that
// activation: once the fiber is quiescing, disposed or restarted, Run returns an
// error that matches ErrInactive and calls no listener. Its zero value is dead. It
// is a small value and safe for concurrent use.
type WaterfallDispatcher[A, R any] struct {
	w *Waterfall[A, R]
	c *Context
}

// Bind returns the dispatcher of w for c's fiber, under the rule of Event.Bind
// (07-c4-plugin-sdk.md:439; ADR-034 owner decision 10).
func (w *Waterfall[A, R]) Bind(c *Context) WaterfallDispatcher[A, R] {
	w.def.bind(c)
	return WaterfallDispatcher[A, R]{w: w, c: c}
}

// Use returns the HookUse that a plugin lists in Plugin.Hooks to listen on stage
// (07-c4-plugin-sdk.md:219, :411).
func (w *Waterfall[A, R]) Use(stage string) HookUse { return w.def.use(stage) }

// On registers fn as a listener of stage, as an effect of c's fiber: it leaves the
// chain with the fiber (07-c4-plugin-sdk.md:209, :369). The refusals are those of
// Event.On.
func (w *Waterfall[A, R]) On(c *Context, stage string, fn WaterfallListener[A, R]) {
	w.table.add(w.def, c, stage, fn, fn == nil)
}

// Run calls the chain reached by aud, outermost first, and returns the result of
// the outermost listener (07-c4-plugin-sdk.md:209, :386, :440). A waterfall fails
// closed (ADR-034 owner decision 12): a listener that errors or panics fails the
// dispatch with a *ListenerError naming it, no later listener and not the owner's
// built-in behaviour runs, and the dispatch never substitutes a successful value
// for a failed call. An empty chain returns the zero R. A dispatcher whose
// activation has ended returns an error that matches ErrInactive and calls no
// listener (decision 10).
func (d WaterfallDispatcher[A, R]) Run(ctx context.Context, aud Audience, a A) (R, error) {
	return d.RunWith(ctx, aud, a, nil)
}

// RunWith is Run with the owner's built-in behaviour as the innermost step
// (ADR-059 decision 2; 07-c4-plugin-sdk.md:386: "Listeners run outermost first. A
// listener that does not call `next` vetoes the rest of the chain and the built-in
// behaviour"). The next of the last listener calls inner with ctx and a, and with
// no listener inner runs alone; a listener that does not call next means inner does
// not run. The result is the outermost listener's. An error of inner fails the
// dispatch as an error of a listener does, as a *ListenerError naming the owner's
// fiber, and inner is never skipped for a fiber that is quiescing. A nil inner is
// Run: past the last listener the result is the zero R.
func (d WaterfallDispatcher[A, R]) RunWith(ctx context.Context, aud Audience, a A, inner func(context.Context, A) (R, error)) (R, error) {
	if d.w == nil {
		var zero R
		return zero, ErrInactive
	}
	leave, err := d.w.def.enter(d.c)
	if err != nil {
		var zero R
		return zero, err
	}
	defer leave()
	var own *listener[WaterfallListener[A, R]]
	if inner != nil {
		own = &listener[WaterfallListener[A, R]]{id: d.c.frame.id, stage: len(d.w.def.stages), ctx: d.c, own: true,
			fn: func(ctx context.Context, a A, _ Next[R]) (R, error) { return inner(ctx, a) }}
	}
	return d.w.run(ctx, aud, a, own)
}

// run is Run with no check of a dispatcher and an optional innermost listener: the
// kernel root's own path (config.go), which plugins cannot reach. The listener is the kernel's own behaviour, which
// the next of the last listener reaches and which runs alone on an empty chain.
func (w *Waterfall[A, R]) run(ctx context.Context, aud Audience, a A, inner *listener[WaterfallListener[A, R]]) (R, error) {
	var zero R
	if err := w.def.dispatchable(ctx, aud); err != nil {
		return zero, err
	}
	list, err := w.table.pick(aud, nil)
	if err != nil {
		return zero, err
	}
	if inner != nil {
		list = append(slices.Clip(list), inner)
	}
	return walk[A, R](ctx, list, a)
}

const (
	stepReady = iota
	stepRunning
	stepDone
	stepExpired
)

// step is the state of one listener's next.
type step[A, R any] struct {
	ctx   context.Context
	rest  []*listener[WaterfallListener[A, R]]
	a     A
	mu    sync.Mutex
	cond  sync.Cond
	state int
}

func (s *step[A, R]) next() (R, error) {
	var zero R
	s.mu.Lock()
	state := s.state
	if state == stepReady {
		s.state = stepRunning
	}
	s.mu.Unlock()
	switch state {
	case stepReady:
	case stepExpired:
		return zero, ErrNextExpired
	default:
		return zero, ErrNext
	}
	defer func() {
		s.mu.Lock()
		s.state = stepDone
		s.mu.Unlock()
		s.cond.Broadcast()
	}()
	return walk[A, R](s.ctx, s.rest, s.a)
}

// expire ends the listener's next: a later call is refused, and a call that is
// still running on another goroutine is waited for, so that the listener's lease
// outlives the work it started.
func (s *step[A, R]) expire() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == stepReady {
		s.state = stepExpired
		return
	}
	for s.state == stepRunning {
		s.cond.Wait()
	}
}

// walk calls the first listener of list that can be entered; the others are its
// next.
func walk[A, R any](ctx context.Context, list []*listener[WaterfallListener[A, R]], a A) (R, error) {
	var zero R
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	for i, r := range list {
		leave, ok := r.lease()
		if !ok {
			continue // Its fiber is quiescing.
		}
		defer leave()
		s := &step[A, R]{ctx: ctx, rest: list[i+1:], a: a}
		s.cond.L = &s.mu
		v, err := callWaterfall(ctx, r, a, s.next)
		s.expire()
		if err != nil {
			return zero, &ListenerError{PluginID: r.id, Cause: err}
		}
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		return v, nil
	}
	return zero, nil
}

func callWaterfall[A, R any](ctx context.Context, r *listener[WaterfallListener[A, R]], a A, next Next[R]) (v R, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = &PanicError{Value: p}
		}
	}()
	return r.fn(ctx, a, next)
}
