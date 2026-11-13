// SPDX-License-Identifier: Apache-2.0

// Package eventqueue is the stdlib-only, generic off-owner observer dispatcher
// shared by the test-only runtime candidates and their observer regression tests.
// It is not a selected runtime API and has no dependency on either candidate.
package eventqueue

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
)

var (
	ErrClosed      = errors.New("observer dispatcher closing")
	ErrSequenceGap = errors.New("observer sequence must be positive and consecutive")
	ErrNotAccepted = errors.New("observer barrier sequence has not been accepted")
	ErrBinding     = errors.New("observer requires a lifecycle binding")
	ErrObserver    = errors.New("observer requires a label and callback")
	ErrInactive    = errors.New("observer owner inactive")
	ErrReentrant   = errors.New("observer callback cannot join its own dispatcher")
)

// Binding associates registration and invocation with a real activation. Enter
// must refuse a quiescing owner; successful Enter must be paired with Leave.
// The runtime must drain those leases before running effects. Defer registers a
// cleanup effect and must reject setup on an inactive owner. An adapter can
// convert a runtime's named disposer type without introducing an import here.
// Binding methods are called outside the dispatcher's mutation lock.
//
// This is a prototype bridge, not an author-facing context API.
type Binding interface {
	Enter() bool
	Leave()
	Defer(label string, dispose func(context.Context) error) error
}

// Observer runs on the single dispatcher worker, never inline in Append. The
// event, including any referenced maps, slices, pointers, or interface values,
// must remain immutable. No cloning, marshaling, filtering, or event callbacks
// occur in Append.
//
// The context comes from context.Background, carries a private dispatcher
// marker, and is canceled by Close. It contains no activation/product identity.
// Callbacks must not synchronously join their own dispatcher. Passing this
// context (or a derivative preserving its values) to Barrier, WaitBarrier, or
// Close returns ErrReentrant. Passing an unrelated context cannot be detected;
// the caller must avoid that self-join. There is no goroutine-ID detection.
type Observer[T any] func(context.Context, T) error

// Logger runs on the same off-owner worker after the callback lease is released.
// Errors and panics from observers are logged once, skipped, and never retried;
// remaining observers and events continue. A nil Logger disables logging.
// Logger must not synchronously join this dispatcher. Its own panic is contained
// and recorded in Status rather than recursively sent back to Logger.
type Logger func(error)

// Disposer removes a registration from future enqueue-time snapshots. Already
// captured snapshots retain it: a callback that can still enter its real owner
// may run, and a callback already holding a lease completes. Quiescing the owner
// is stronger: Enter rejects queued callbacks even from an older snapshot.
// The runtime, not manual removal, owns the actual lease-drain join.
type Disposer func(context.Context) error

type registration[T any] struct {
	owner    Binding
	label    string
	callback Observer[T]
	removed  bool // Protected by Dispatcher.mu; only used during publication.
}

type envelope[T any] struct {
	sequence  uint64
	event     T
	listeners []*registration[T] // Immutable COW list captured by Append.
}

// Status is a synchronized diagnostic snapshot. Delivered means that every
// listener captured for the event was attempted or skipped due to an inactive
// owner, including all error logging. It does not mean every callback succeeded.
// Remaining includes an in-flight event as well as queued events. A hung
// callback/logger leaves real work and a real worker; it is not declared done.
type Status struct {
	Accepted          uint64
	Delivered         uint64
	Remaining         uint64
	Queued            int
	InFlight          bool
	Observers         int
	Closing           bool
	Stopped           bool
	Callbacks         uint64
	Skipped           uint64
	ObserverErrors    uint64
	LoggerPanics      uint64
	FormattingPanics  uint64
	LastObserverError string
	LastLoggerError   string
}

// CloseError reports a bounded Close that did not join the real worker. Work is
// not deleted or marked delivered; a later Close can join after it actually exits.
type CloseError struct {
	Cause  error
	Status Status
}

func (e *CloseError) Error() string {
	return fmt.Sprintf("observer close incomplete: accepted=%d delivered=%d remaining=%d queued=%d in-flight=%t",
		e.Status.Accepted, e.Status.Delivered, e.Status.Remaining, e.Status.Queued, e.Status.InFlight)
}

func (e *CloseError) Unwrap() error { return e.Cause }

// PanicError preserves the panic value and errors.Is identity when that value
// is an error. Its Error method is used only by off-owner error formatting in
// the dispatcher; Logger receives an ObserverError with cached diagnostic text.
type PanicError struct{ Value any }

func (e *PanicError) Error() string { return fmt.Sprintf("observer callback panicked: %v", e.Value) }
func (e *PanicError) Unwrap() error { cause, _ := e.Value.(error); return cause }

// ObserverError is an off-owner-formatted diagnostic. Error returns cached text
// so a logger retaining the error need not invoke the original Error/Stringer.
type ObserverError struct {
	Sequence uint64
	Label    string
	Cause    error
	text     string
}

func (e *ObserverError) Error() string { return e.text }
func (e *ObserverError) Unwrap() error { return e.Cause }

type callbackKey struct{}
type callbackMarker struct{ nonzero byte }

// Dispatcher serializes events and listeners on exactly one root-owned worker.
// Append is control-path bookkeeping only: its short mutation lock never spans
// lifecycle methods, observer callbacks, error formatting, or logging. It never
// waits for a callback or for queue capacity. Queue memory is intentionally
// unbounded: a hung observer can retain arbitrarily many accepted events.
//
// Use New to construct a Dispatcher. A Dispatcher must not be copied.
type Dispatcher[T any] struct {
	mu        sync.Mutex
	listeners []*registration[T]
	queue     []envelope[T]
	head      int
	status    Status
	progress  chan struct{}
	wake      chan struct{}
	done      chan struct{}
	callback  context.Context
	cancel    context.CancelFunc
	marker    *callbackMarker
	logger    Logger
}

// New starts one worker. Its callback context is independent of the context of
// any runtime operation, observer owner, barrier, or Close caller.
func New[T any](logger Logger) *Dispatcher[T] {
	base, cancel := context.WithCancel(context.Background())
	marker := &callbackMarker{}
	d := &Dispatcher[T]{
		progress: make(chan struct{}), wake: make(chan struct{}, 1), done: make(chan struct{}),
		callback: context.WithValue(base, callbackKey{}, marker), cancel: cancel, marker: marker, logger: logger,
	}
	go d.run()
	return d
}

// AddObserver registers a native lifecycle cleanup effect before publishing a
// COW listener list. Setup holds a real owner lease, preventing cleanup from
// finishing between Defer and publication. No Binding method runs under mu.
// Listener order within an event is registration-publication order.
func (d *Dispatcher[T]) AddObserver(owner Binding, label string, callback Observer[T]) (Disposer, error) {
	if owner == nil {
		return nil, ErrBinding
	}
	if label == "" || callback == nil {
		return nil, ErrObserver
	}
	if !owner.Enter() {
		return nil, ErrInactive
	}
	defer owner.Leave()
	r := &registration[T]{owner: owner, label: label, callback: callback}
	dispose := Disposer(func(context.Context) error {
		d.mu.Lock()
		defer d.mu.Unlock()
		r.removed = true
		if index := slices.Index(d.listeners, r); index >= 0 {
			next := make([]*registration[T], 0, len(d.listeners)-1)
			next = append(next, d.listeners[:index]...)
			next = append(next, d.listeners[index+1:]...)
			d.listeners = next
		}
		return nil
	})
	if err := owner.Defer("observer "+label, dispose); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.status.Closing {
		return nil, ErrClosed
	}
	if r.removed {
		return nil, ErrInactive
	}
	d.listeners = append(slices.Clone(d.listeners), r)
	return dispose, nil
}

// Append accepts only 1, 2, 3, ... in the same linearized order as the source
// state/service ledger. Zero, a duplicate, a backwards value, a gap, and uint64
// wraparound return ErrSequenceGap without modifying the queue or watermark.
// On success the event remains queued in memory until delivery; Close does not
// delete it. This is not crash persistence. T is copied by value only, with no
// caller-provided hooks.
//
// The source state owner may call this while it owns its ledger lock/turn. The
// listener list is captured here, not later when the worker reaches the event.
func (d *Dispatcher[T]) Append(sequence uint64, event T) error {
	d.mu.Lock()
	if d.status.Closing {
		d.mu.Unlock()
		return ErrClosed
	}
	if sequence == 0 || d.status.Accepted == ^uint64(0) || sequence != d.status.Accepted+1 {
		d.mu.Unlock()
		return ErrSequenceGap
	}
	d.queue = append(d.queue, envelope[T]{sequence: sequence, event: event, listeners: d.listeners})
	d.status.Accepted = sequence
	d.mu.Unlock()
	d.poke()
	return nil
}

// Barrier joins the accepted watermark captured at this call, not future
// appends. Runtime Settle must remain independent; an external harness can call
// Settle and then explicitly call this barrier (DrainObservers) when needed.
func (d *Dispatcher[T]) Barrier(ctx context.Context) error {
	return d.WaitBarrier(ctx, d.Status().Accepted)
}

// WaitBarrier joins delivery through an already accepted sequence. Zero joins
// the empty prefix. A target beyond Accepted returns ErrNotAccepted. Observer
// errors are diagnostics, not a barrier failure. A canceled wait does not cancel
// callbacks or lose accepted work. See Observer for the explicit self-join rule.
func (d *Dispatcher[T]) WaitBarrier(ctx context.Context, sequence uint64) error {
	if d.fromCallback(ctx) {
		return ErrReentrant
	}
	for {
		d.mu.Lock()
		if sequence <= d.status.Delivered {
			d.mu.Unlock()
			return nil
		}
		if sequence > d.status.Accepted {
			d.mu.Unlock()
			return ErrNotAccepted
		}
		progress := d.progress
		d.mu.Unlock()
		select {
		case <-progress:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Status returns diagnostics without invoking user code.
func (d *Dispatcher[T]) Status() Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	status := d.status
	status.Remaining = status.Accepted - status.Delivered
	status.Queued = len(d.queue) - d.head
	status.Observers = len(d.listeners)
	return status
}

// Close stops admission, cancels the independent callback context, and joins the
// actual worker after it drains every accepted event. Queued callbacks still run
// with the canceled context if their owner lease permits. Cancellation is a
// cooperative request, not permission to kill a callback or report false drain.
// If ctx expires first, CloseError reports the real remaining work. The worker
// remains live until callbacks/logging actually return; a later Close can join.
func (d *Dispatcher[T]) Close(ctx context.Context) error {
	if d.fromCallback(ctx) {
		return ErrReentrant
	}
	d.mu.Lock()
	d.status.Closing = true
	d.mu.Unlock()
	d.cancel()
	d.poke()
	select {
	case <-d.done:
		return nil
	default:
	}
	select {
	case <-d.done:
		return nil
	case <-ctx.Done():
		select {
		case <-d.done:
			return nil
		default:
			return &CloseError{Cause: ctx.Err(), Status: d.Status()}
		}
	}
}

func (d *Dispatcher[T]) fromCallback(ctx context.Context) bool {
	return ctx.Value(callbackKey{}) == d.marker
}

func (d *Dispatcher[T]) poke() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *Dispatcher[T]) signalLocked() {
	close(d.progress)
	d.progress = make(chan struct{})
}

func (d *Dispatcher[T]) run() {
	defer func() {
		d.mu.Lock()
		d.status.Stopped = true
		d.signalLocked()
		d.mu.Unlock()
		close(d.done)
	}()
	for {
		d.mu.Lock()
		if d.head == len(d.queue) {
			closing := d.status.Closing
			d.mu.Unlock()
			if closing {
				return
			}
			<-d.wake
			continue
		}
		event := d.queue[d.head]
		d.queue[d.head] = envelope[T]{}
		d.head++
		d.status.InFlight = true
		// Amortized compaction is a storage algorithm, not a workload budget.
		// Each pop clears references, and an empty queue releases its backing
		// array. No fixed policy threshold or per-event goroutine is introduced.
		if d.head >= len(d.queue)-d.head {
			remaining := copy(d.queue, d.queue[d.head:])
			clear(d.queue[remaining:])
			d.queue = d.queue[:remaining]
			d.head = 0
			if remaining == 0 {
				d.queue = nil
			}
		}
		d.mu.Unlock()

		for _, listener := range event.listeners {
			called, err := invoke(d.callback, listener, event.event)
			d.mu.Lock()
			if called {
				d.status.Callbacks++
			} else {
				d.status.Skipped++
			}
			d.mu.Unlock()
			if err != nil {
				d.report(event.sequence, listener.label, err)
			}
		}
		d.mu.Lock()
		d.status.Delivered = event.sequence
		d.status.InFlight = false
		d.signalLocked()
		d.mu.Unlock()
	}
}

// Only this invocation boundary contains observer/lease panics. In particular,
// nothing is installed around the source state owner or its event publisher.
func invoke[T any](ctx context.Context, r *registration[T], event T) (called bool, err error) {
	defer func() {
		if cause := recover(); cause != nil {
			err = &PanicError{Value: cause}
		}
	}()
	if !r.owner.Enter() {
		return false, nil
	}
	defer r.owner.Leave()
	called = true
	return called, r.callback(ctx, event)
}

func errorText(err error) (text string, panicked bool) {
	defer func() {
		if cause := recover(); cause != nil {
			// Do not format the new panic payload: a second hostile formatter
			// must not turn diagnostic containment into recursive callbacks.
			text, panicked = "observer error formatting panicked", true
		}
	}()
	return err.Error(), false
}

func (d *Dispatcher[T]) report(sequence uint64, label string, cause error) {
	text, formattingPanic := errorText(cause)
	diagnostic := &ObserverError{Sequence: sequence, Label: label, Cause: cause,
		text: fmt.Sprintf("observer %q at sequence %d: %s", label, sequence, text)}
	d.mu.Lock()
	d.status.ObserverErrors++
	d.status.LastObserverError = diagnostic.text
	if formattingPanic {
		d.status.FormattingPanics++
	}
	d.mu.Unlock()
	if d.logger == nil {
		return
	}
	loggerPanic := callLogger(d.logger, diagnostic)
	if loggerPanic != nil {
		// Report the logger failure through Status, never by reentering Logger.
		text, formattingPanic := errorText(loggerPanic)
		d.mu.Lock()
		d.status.LoggerPanics++
		d.status.LastLoggerError = text
		if formattingPanic {
			d.status.FormattingPanics++
		}
		d.mu.Unlock()
	}
}

func callLogger(logger Logger, err error) (panicError error) {
	defer func() {
		if cause := recover(); cause != nil {
			panicError = &PanicError{Value: cause}
		}
	}()
	logger(err)
	return nil
}
