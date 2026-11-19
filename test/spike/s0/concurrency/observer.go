// SPDX-License-Identifier: Apache-2.0

package concurrency

import (
	"context"
	"errors"
	"sync"

	"github.com/mutex-solutions/aiops-platform/test/spike/s0/internal/eventqueue"
)

// ErrObserverBinding rejects a nil context or one from another native runtime.
// Ownership is checked by the runtime before registering outside its state owner.
var ErrObserverBinding = errors.New("observer context belongs to another runtime")

// ObserverStatus is a diagnostic snapshot of the real shared dispatcher. An
// accepted event is delivered after every captured listener has run or been
// skipped because its activation is quiescing, including error diagnostics.
type ObserverStatus = eventqueue.Status

// ObserverRuntime exposes the test-only native event stream without making
// Runtime.Settle wait for observers. Each runtime owns one ordered worker; it is
// not a goroutine per activation. Events are the same immutable envelopes as the
// native ledger, in its total Sequence order, without normalization or replay.
//
// ObserveEvents binds a listener to the exact live owner activation. Its disposer
// removes it from future enqueue-time snapshots; already captured callbacks may
// still run while the owner is live. Each invocation's context preserves the
// dispatcher's self-join marker and is canceled when either its native owner
// quiesces or the dispatcher closes. Quiescence rejects new callback leases, and
// native cleanup waits for callbacks already holding a lease to actually return.
//
// AwaitObservers joins only the accepted watermark captured at that call, not
// events appended later. A callback must not synchronously join its own worker;
// passing its supplied context to AwaitObservers returns eventqueue.ErrReentrant.
// Callbacks can request disposal with Context.Dispose and leave joining to an
// external caller. They must not synchronously await their own teardown.
//
// The queue is bounded by Retention.ObserverBacklog (default
// DefaultObserverBacklog events). Because the kernel never waits for an observer,
// the bound cannot push back: when an event arrives while that many accepted
// events are still undelivered, the observer the worker is inside is detached.
// It receives no further event, its lost events are counted, the overflow goes
// through the dispatcher failure path (ObserverStatus.ObserverErrors,
// LastObserverError), ObserverStatus.Overflowed/EventsLost/LastOverflow, the
// error of AwaitObservers and Close (errors.Is(err, ErrObserverOverflow)), and,
// if registered with ObserveEventsWith, one terminal OnOverflow call once its
// callback returns. Every other observer keeps every event, in order, and the
// runtime is unaffected; the detached observer's queue memory is released. This
// bridge is still not a crash-persistence policy.
type ObserverRuntime interface {
	ObserveEvents(*Context, string, func(context.Context, Event) error) (Disposer, error)
	AwaitObservers(context.Context) error
	ObserverStatus() ObserverStatus
}

// OverflowObserverRuntime adds the terminal overflow signal to ObserverRuntime.
// onOverflow may be nil. It runs at most once, under the observer's lease, on the
// goroutine that was inside the detached callback, after that callback returned.
type OverflowObserverRuntime interface {
	ObserverRuntime
	ObserveEventsWith(owner *Context, label string, fn func(context.Context, Event) error, onOverflow func(context.Context, *ObserverOverflow) error) (Disposer, error)
}

// ObserverOverflow describes one observer detached for overflow.
type ObserverOverflow = eventqueue.OverflowError

// ErrObserverOverflow is matched by errors.Is for any observer overflow.
var ErrObserverOverflow = eventqueue.ErrOverflow

// nativeObservers must be constructed once with newNativeObservers, before the
// first native event. Only the core's state owner appends its consecutive ledger
// events. No callback, binding method, or error formatting runs in append.
//
// Admission failures indicate a broken core integration (a sequence gap or an
// early queue close). Retain the first failure even when the core ignores the
// append return value, so external barrier/close callers cannot see false success.
type nativeObservers struct {
	d   *eventqueue.Dispatcher[Event]
	mu  sync.Mutex
	err error
}

func newNativeObservers() *nativeObservers { return newNativeObserversWith(0) }

// newNativeObserversWith bounds the dispatcher backlog; 0 selects the default.
func newNativeObserversWith(backlog int) *nativeObservers {
	return &nativeObservers{d: eventqueue.NewWith[Event](nil, eventqueue.Options{Backlog: backlog})}
}

func (n *nativeObservers) append(event Event) error {
	err := n.d.Append(event.Sequence, event)
	if err != nil {
		n.mu.Lock()
		if n.err == nil {
			n.err = err
		}
		n.mu.Unlock()
	}
	return err
}

// observe deliberately does not authenticate owner.host. The native runtime
// must validate that pointer identity before calling this method outside its
// state owner: registration invokes Context.Defer and can reenter the core.
func (n *nativeObservers) observe(owner *Context, label string, fn func(context.Context, Event) error) (Disposer, error) {
	return n.observeWith(owner, label, fn, nil)
}

// observeWith also registers the optional terminal overflow signal, which gets
// the same owner-bound cancellation as an event callback.
func (n *nativeObservers) observeWith(owner *Context, label string, fn func(context.Context, Event) error, onOverflow func(context.Context, *ObserverOverflow) error) (Disposer, error) {
	if owner == nil {
		return nil, ErrObserverBinding
	}
	var callback eventqueue.Observer[Event]
	if fn != nil {
		callback = func(dispatcherCtx context.Context, event Event) error {
			// This invocation already holds the real native lease and runs off
			// the state owner. Cancel before native lease-drain joins, not only
			// when the dispatcher can finally close after lifecycle teardown.
			ctx, cancel := context.WithCancel(dispatcherCtx)
			stop := context.AfterFunc(owner.Std(), cancel)
			defer stop()
			defer cancel()
			return fn(ctx, event)
		}
	}
	var terminal eventqueue.OverflowHandler
	if onOverflow != nil {
		terminal = func(dispatcherCtx context.Context, failure *eventqueue.OverflowError) error {
			ctx, cancel := context.WithCancel(dispatcherCtx)
			stop := context.AfterFunc(owner.Std(), cancel)
			defer stop()
			defer cancel()
			return onOverflow(ctx, failure)
		}
	}
	dispose, err := n.d.AddObserverWith(nativeObserverBinding{owner}, label, callback, terminal)
	if errors.Is(err, eventqueue.ErrInactive) {
		return nil, ErrInactive
	}
	if err != nil {
		return nil, err
	}
	return Disposer(dispose), nil
}

func (n *nativeObservers) await(ctx context.Context) error {
	// An overflow is a failure of observation, not a successful full drain.
	return errors.Join(n.d.Barrier(ctx), n.failure(), n.d.OverflowErr())
}

func (n *nativeObservers) close(ctx context.Context) error {
	return errors.Join(n.d.Close(ctx), n.failure())
}

func (n *nativeObservers) status() ObserverStatus { return n.d.Status() }

func (n *nativeObservers) failure() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.err
}

// Embedding preserves the real activation's Enter/Leave lease protocol. The
// named disposer conversion is the only adaptation required by eventqueue.
type nativeObserverBinding struct{ *Context }

func (binding nativeObserverBinding) Defer(label string, fn func(context.Context) error) error {
	return binding.Context.Defer(label, Disposer(fn))
}
