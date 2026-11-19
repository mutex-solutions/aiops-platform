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
	// ErrOverflow identifies an observer detached because it stalled the
	// dispatcher past the backlog bound. See OverflowError.
	ErrOverflow = errors.New("observer backlog overflow")
)

// DefaultBacklog is the bound used when none is given. It is far above the
// largest backlog any in-tree suite or scale fixture produces while observers
// keep up, and it costs memory only while events are actually waiting.
const DefaultBacklog = 1 << 16

// maxOverflowDetails caps the per-observer overflow errors kept for Close and
// OverflowErr; Status.Overflowed always counts every one.
const maxOverflowDetails = 16

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

// Observer runs on the dispatcher worker, never inline in Append. The
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

// OverflowHandler is the terminal signal of an observer detached for overflow.
// It runs at most once, under a real owner lease (skipped when the owner is no
// longer active) and off the state owner, serialized after the observer's own
// last callback: it runs on the goroutine that was inside that callback when
// the bound was hit, as soon as the callback returns, so it never runs
// concurrently with the observer's callback. A callback that never returns never
// sees it; Status and Close report the overflow regardless.
type OverflowHandler func(context.Context, *OverflowError) error

// OverflowError describes one observer detached because the backlog reached its
// bound while the worker was inside that observer.
type OverflowError struct {
	Label   string
	Lost    uint64 // Events captured for the observer that it will never receive.
	Through uint64 // Last sequence addressed to the observer; nothing after it is.
	Limit   int    // The backlog bound that was reached.
}

func (e *OverflowError) Error() string {
	return fmt.Sprintf("observer %q overflowed the backlog bound of %d events: %d events lost through sequence %d, observer detached",
		e.Label, e.Limit, e.Lost, e.Through)
}

func (e *OverflowError) Unwrap() error { return ErrOverflow }

// Logger runs on the same off-owner worker after the callback lease is released.
// Errors and panics from observers are logged once, skipped, and never retried;
// remaining observers and events continue. A nil Logger disables logging.
// Logger must not synchronously join this dispatcher. Its own panic is contained
// and recorded in Status rather than recursively sent back to Logger. Logger
// calls are serialized even when an overflow hands the worker role over.
type Logger func(error)

// Disposer removes a registration from future enqueue-time snapshots. Already
// captured snapshots retain it: a callback that can still enter its real owner
// may run, and a callback already holding a lease completes. Quiescing the owner
// is stronger: Enter rejects queued callbacks even from an older snapshot.
// The runtime, not manual removal, owns the actual lease-drain join.
type Disposer func(context.Context) error

// registration fields after onOverflow are protected by Dispatcher.mu.
type registration[T any] struct {
	owner      Binding
	label      string
	callback   Observer[T]
	onOverflow OverflowHandler
	removed    bool
	first      uint64 // First sequence captured for this observer.
	last       uint64 // Last sequence captured; 0 while it is still registered.
	handled    uint64 // Captured events whose delivery to it has started.
	lost       uint64
	overflowed bool
	notified   bool
}

type envelope[T any] struct {
	sequence  uint64
	event     T
	listeners []*registration[T] // Immutable COW list captured by Append.
}

// Status is a synchronized diagnostic snapshot. Delivered means that every
// listener captured for the event was attempted or skipped due to an inactive
// owner, including all error logging. It does not mean every callback succeeded,
// and an observer detached for overflow is not waited for. Remaining includes an
// in-flight event as well as queued events. A hung callback/logger leaves real
// work and a real worker; it is not declared done.
//
// Backlog is the bound: at most that many accepted events (the in-flight one
// included) may wait undelivered behind a stalled observer before that observer
// is detached. PeakBacklog is the most undelivered events ever seen, for sizing
// and verifying the bound. Overflowed counts detached observers, EventsLost the
// events they were denied, Orphaned the detached observers' goroutines still
// inside a callback, and LastOverflow the text of the latest overflow.
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
	Backlog           int
	PeakBacklog       int
	Overflowed        uint64
	EventsLost        uint64
	Orphaned          int
	LastOverflow      string
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

type pendingReport struct {
	sequence uint64
	label    string
	err      error
}

// Dispatcher serializes events and listeners on exactly one active worker.
// Append is control-path bookkeeping only: its short mutation lock never spans
// lifecycle methods, observer callbacks, error formatting, or logging. It never
// waits for a callback or for queue capacity.
//
// Memory is bounded by Options.Backlog. When an Append finds that many accepted
// events still undelivered, the observer the worker is inside (a stalled or too
// slow callback) is detached: it leaves the listener list, its lost events are
// counted, the overflow goes through the ordinary failure path, Status and Close,
// and the worker role passes to a new goroutine that resumes the very same event
// at the next listener. The stalled callback's goroutine finishes that callback
// (and a terminal OverflowHandler) and exits. Observers that keep up lose
// nothing and keep the order. The backlog accumulated behind the stalled observer
// is not charged to the next observer: the bound counts events accepted since
// the last detachment, so the queue is at most Backlog*(1+k) while k detached
// observers' backlogs are still draining.
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
	logMu     sync.Mutex // Serializes Logger calls across worker generations.

	limit     int // Backlog bound; 0 only in algorithm-only literals: no bound.
	floor     uint64
	gen       uint64 // Generation of the one worker allowed to advance the queue.
	workers   int    // Live worker goroutines, including detached ones.
	active    bool   // env is being delivered.
	env       envelope[T]
	idx       int              // Next listener of env.
	running   *registration[T] // Listener being or about to be run by the active worker.
	reports   []pendingReport  // Overflow diagnostics for the active worker to log.
	overflows []*OverflowError // First maxOverflowDetails, for Close.
}

// Options configure a Dispatcher.
type Options struct {
	// Backlog bounds the accepted-but-undelivered events; 0 selects DefaultBacklog.
	Backlog int
}

// New starts one worker with the default backlog bound. Its callback context is
// independent of the context of any runtime operation, observer owner, barrier,
// or Close caller.
func New[T any](logger Logger) *Dispatcher[T] { return NewWith[T](logger, Options{}) }

// NewWith is New with explicit Options.
func NewWith[T any](logger Logger, options Options) *Dispatcher[T] {
	if options.Backlog <= 0 {
		options.Backlog = DefaultBacklog
	}
	base, cancel := context.WithCancel(context.Background())
	marker := &callbackMarker{}
	d := &Dispatcher[T]{
		progress: make(chan struct{}), wake: make(chan struct{}, 1), done: make(chan struct{}),
		callback: context.WithValue(base, callbackKey{}, marker), cancel: cancel, marker: marker, logger: logger,
		limit: options.Backlog,
	}
	d.status.Backlog = options.Backlog
	go d.run()
	return d
}

// AddObserver registers a native lifecycle cleanup effect before publishing a
// COW listener list. Setup holds a real owner lease, preventing cleanup from
// finishing between Defer and publication. No Binding method runs under mu.
// Listener order within an event is registration-publication order.
func (d *Dispatcher[T]) AddObserver(owner Binding, label string, callback Observer[T]) (Disposer, error) {
	return d.AddObserverWith(owner, label, callback, nil)
}

// AddObserverWith is AddObserver with an optional terminal signal for overflow.
func (d *Dispatcher[T]) AddObserverWith(owner Binding, label string, callback Observer[T], onOverflow OverflowHandler) (Disposer, error) {
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
	r := &registration[T]{owner: owner, label: label, callback: callback, onOverflow: onOverflow}
	dispose := Disposer(func(context.Context) error {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.removeLocked(r)
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
	r.first = d.status.Accepted + 1
	d.listeners = append(slices.Clone(d.listeners), r)
	return dispose, nil
}

// removeLocked takes r out of future snapshots; idempotent.
func (d *Dispatcher[T]) removeLocked(r *registration[T]) {
	if !r.removed {
		r.removed = true
		if r.last == 0 {
			r.last = d.status.Accepted
		}
	}
	if index := slices.Index(d.listeners, r); index >= 0 {
		next := make([]*registration[T], 0, len(d.listeners)-1)
		next = append(next, d.listeners[:index]...)
		next = append(next, d.listeners[index+1:]...)
		d.listeners = next
	}
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
// When the backlog bound is reached the event is still accepted: the stalled
// observer is detached instead (see Dispatcher), in constant time.
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
	if d.limit > 0 && d.backlogLocked() >= uint64(d.limit) {
		d.overflowLocked()
	}
	d.queue = append(d.queue, envelope[T]{sequence: sequence, event: event, listeners: d.listeners})
	d.status.Accepted = sequence
	if undelivered := int(sequence - d.status.Delivered); undelivered > d.status.PeakBacklog {
		d.status.PeakBacklog = undelivered
	}
	d.mu.Unlock()
	d.poke()
	return nil
}

// backlogLocked is the number of events accepted since the last detachment and
// not yet delivered.
func (d *Dispatcher[T]) backlogLocked() uint64 {
	return d.status.Accepted - max(d.status.Delivered, d.floor)
}

// overflowLocked detaches the observer the worker is inside and hands the worker
// role to a new goroutine, which resumes the same event at the next listener. If
// the worker is not inside any listener nothing can be blamed and nothing
// changes; the next Append asks again.
func (d *Dispatcher[T]) overflowLocked() {
	l := d.running
	if l == nil || l.overflowed {
		return
	}
	l.overflowed = true
	d.removeLocked(l)
	if l.last >= l.first {
		if captured := l.last - l.first + 1; captured > l.handled {
			l.lost = captured - l.handled
		}
	}
	failure := &OverflowError{Label: l.label, Lost: l.lost, Through: l.last, Limit: d.limit}
	d.status.Overflowed++
	d.status.EventsLost += l.lost
	d.status.LastOverflow = failure.Error()
	if len(d.overflows) < maxOverflowDetails {
		d.overflows = append(d.overflows, failure)
	}
	d.reports = append(d.reports, pendingReport{sequence: failure.Through, label: l.label, err: failure})
	d.floor = d.status.Accepted
	d.running = nil
	d.gen++
	d.workers++
	go d.loop(d.gen)
}

// OverflowErr reports every observer detached so far, or nil. Close joins it.
func (d *Dispatcher[T]) OverflowErr() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.overflowErrLocked()
}

func (d *Dispatcher[T]) overflowErrLocked() error {
	if d.status.Overflowed == 0 {
		return nil
	}
	errs := make([]error, 0, len(d.overflows)+1)
	for _, failure := range d.overflows {
		errs = append(errs, failure)
	}
	if more := d.status.Overflowed - uint64(len(d.overflows)); more > 0 {
		errs = append(errs, fmt.Errorf("%w: %d further observers detached", ErrOverflow, more))
	}
	return errors.Join(errs...)
}

// Barrier joins the accepted watermark captured at this call, not future
// appends. Runtime Settle must remain independent; an external harness can call
// Settle and then explicitly call this barrier (DrainObservers) when needed.
func (d *Dispatcher[T]) Barrier(ctx context.Context) error {
	return d.WaitBarrier(ctx, d.Status().Accepted)
}

// WaitBarrier joins delivery through an already accepted sequence. Zero joins
// the empty prefix. A target beyond Accepted returns ErrNotAccepted. Observer
// errors are diagnostics, not a barrier failure, and neither is an overflow:
// read it from Status or OverflowErr. A canceled wait does not cancel
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
	status.Orphaned = d.workers
	if !status.Stopped && status.Orphaned > 0 {
		status.Orphaned-- // The active worker is not an orphan.
	}
	return status
}

// Close stops admission, cancels the independent callback context, and joins
// every worker (the active one after it drains each accepted event, and any
// detached observer's goroutine after its callback returns). Queued callbacks
// still run with the canceled context if their owner lease permits. Cancellation
// is a cooperative request, not permission to kill a callback or report false
// drain. If ctx expires first, CloseError reports the real remaining work. The
// workers remain live until callbacks/logging actually return; a later Close can
// join. Once joined, Close also returns every overflow (see OverflowErr).
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
		return d.OverflowErr()
	default:
	}
	select {
	case <-d.done:
		return d.OverflowErr()
	case <-ctx.Done():
		select {
		case <-d.done:
			return d.OverflowErr()
		default:
			var err error = &CloseError{Cause: ctx.Err(), Status: d.Status()}
			if overflow := d.OverflowErr(); overflow != nil {
				err = errors.Join(err, overflow)
			}
			return err
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

// run starts the first worker.
func (d *Dispatcher[T]) run() {
	d.mu.Lock()
	d.workers++
	gen := d.gen
	d.mu.Unlock()
	d.loop(gen)
}

const (
	stepRun = iota
	stepIdle
	stepStop
)

// loop is one worker generation. Only the generation equal to d.gen advances
// the queue; an older one (its observer was detached) finishes its callback,
// lets that observer's terminal handler run, and exits.
func (d *Dispatcher[T]) loop(gen uint64) {
	defer d.exit(gen)
	var prev *registration[T]
	var prevCalled bool
	for {
		d.mu.Lock()
		if prev != nil {
			if prevCalled {
				d.status.Callbacks++
			} else {
				d.status.Skipped++
			}
		}
		if d.gen != gen {
			d.mu.Unlock()
			if prev != nil {
				d.terminal(prev)
			}
			return
		}
		prev = nil
		d.running = nil
		if len(d.reports) > 0 {
			reports := d.reports
			d.reports = nil
			d.mu.Unlock()
			for _, r := range reports {
				d.report(r.sequence, r.label, r.err)
			}
			continue
		}
		l, sequence, event, step := d.advanceLocked()
		d.mu.Unlock()
		switch step {
		case stepStop:
			return
		case stepIdle:
			<-d.wake
			continue
		}
		called, err := invoke(d.callback, l, event)
		if err != nil {
			d.report(sequence, l.label, err)
		}
		prev, prevCalled = l, called
	}
}

// advanceLocked selects the next listener of the event in flight, finishing that
// event and popping the next one as needed.
func (d *Dispatcher[T]) advanceLocked() (l *registration[T], sequence uint64, event T, step int) {
	for {
		if d.active {
			for d.idx < len(d.env.listeners) {
				l = d.env.listeners[d.idx]
				d.idx++
				if l.overflowed {
					continue // Its lost events were counted when it was detached.
				}
				l.handled++
				d.running = l
				return l, d.env.sequence, d.env.event, stepRun
			}
			d.status.Delivered = d.env.sequence
			d.status.InFlight = false
			d.signalLocked()
			d.env, d.active = envelope[T]{}, false
		}
		if d.head == len(d.queue) {
			if d.status.Closing {
				return nil, 0, event, stepStop
			}
			return nil, 0, event, stepIdle
		}
		d.env, d.idx, d.active = d.queue[d.head], 0, true
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
	}
}

// exit retires a worker generation. Stopped is set by the active one; done closes
// when no worker, detached ones included, is left.
func (d *Dispatcher[T]) exit(gen uint64) {
	d.mu.Lock()
	d.workers--
	if gen == d.gen {
		d.status.Stopped = true
		d.signalLocked()
	}
	if d.status.Stopped && d.workers == 0 {
		close(d.done)
	}
	d.mu.Unlock()
}

// terminal delivers the overflow signal of a detached observer, once.
func (d *Dispatcher[T]) terminal(l *registration[T]) {
	d.mu.Lock()
	handler := l.onOverflow
	if handler == nil || l.notified {
		d.mu.Unlock()
		return
	}
	l.notified = true
	failure := &OverflowError{Label: l.label, Lost: l.lost, Through: l.last, Limit: d.limit}
	d.mu.Unlock()
	if err := invokeTerminal(d.callback, l, handler, failure); err != nil {
		d.report(failure.Through, l.label, err)
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

func invokeTerminal[T any](ctx context.Context, r *registration[T], handler OverflowHandler, failure *OverflowError) (err error) {
	defer func() {
		if cause := recover(); cause != nil {
			err = &PanicError{Value: cause}
		}
	}()
	if !r.owner.Enter() {
		return nil
	}
	defer r.owner.Leave()
	return handler(ctx, failure)
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
	d.logMu.Lock()
	loggerPanic := callLogger(d.logger, diagnostic)
	d.logMu.Unlock()
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
