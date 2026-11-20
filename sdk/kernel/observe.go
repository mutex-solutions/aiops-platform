// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
)

var (
	errObserverClosed   = errors.New("observer dispatcher closing")
	errSequenceGap      = errors.New("observer sequence must be positive and consecutive")
	errNotAccepted      = errors.New("observer barrier sequence has not been accepted")
	errBinding          = errors.New("observer requires a lifecycle binding")
	errObserverArgs     = errors.New("observer requires a label and callback")
	errObserverInactive = errors.New("observer owner inactive")
	errReentrant        = errors.New("observer callback cannot join its own dispatcher")
	// errObserverOverflow identifies an observer detached because it stalled the
	// dispatcher past the backlog bound. See overflowError.
	errObserverOverflow = errors.New("observer backlog overflow")
)

// maxOverflowDetails caps the per-observer overflow errors kept for close and
// overflowErr; Overflowed always counts every one. It bounds diagnostics only.
const maxOverflowDetails = 16

// leaseHolder associates registration and invocation with a real activation:
// *handle. enter must refuse a quiescing owner; a successful enter is paired with
// exactly one call of the returned leave function. The core drains those leases
// before it runs effects. tryDefer registers a cleanup effect and must reject
// setup on an inactive owner. Its methods are called outside the dispatcher's
// mutation lock.
type leaseHolder interface {
	enter() (leave func(), ok bool)
	tryDefer(label string, dispose disposer) error
}

// lateDelivery is implemented by a leaseHolder whose observer still receives, once
// its lease is refused, every event accepted before the dispatcher closes: the
// observer of the root, which sees the whole of Close (see rootLease).
type lateDelivery interface{ deliverLate() bool }

// rootLease is the leaseHolder of an observer of the root (Root.Observe). The root
// quiesces when Close begins, so with the plain lease every event of Close would be
// skipped, among them the deadline of a leaking fiber, which is what a launcher
// watches for. This lease is the root's while the root is active, and after that it
// delivers without one: the dispatcher drains every accepted event before it stops
// (advanceLocked), and Close joins it. Its cleanup effect is an empty one, so the
// observer is not removed when the root is disposed and sees the events of that too.
// The Disposer that Observe returns still removes it.
type rootLease struct{ *handle }

func (rootLease) deliverLate() bool { return true }

func (l rootLease) tryDefer(label string, _ disposer) error {
	return l.handle.tryDefer(label, func(context.Context) error { return nil })
}

// observerFunc runs on the dispatcher worker, never inline in append. The
// event, including any referenced slices, pointers, or interface values, must
// remain immutable. No cloning, marshaling, filtering, or event callbacks occur
// in append.
//
// The context comes from context.Background, carries a private dispatcher
// marker, and is canceled by close. It contains no activation/product identity.
// Callbacks must not synchronously join their own dispatcher. Passing this
// context (or a derivative preserving its values) to barrier, waitBarrier, or
// close returns errReentrant. Passing an unrelated context cannot be detected;
// the caller must avoid that self-join. There is no goroutine-ID detection.
type observerFunc func(context.Context, ledgerEvent) error

// overflowHandler is the terminal signal of an observer detached for overflow.
// It runs at most once, under a real owner lease (skipped when the owner is no
// longer active) and off the state owner, serialized after the observer's own
// last callback: it runs on the goroutine that was inside that callback when
// the bound was hit, as soon as the callback returns, so it never runs
// concurrently with the observer's callback. A callback that never returns never
// sees it; status and close report the overflow regardless.
type overflowHandler func(context.Context, *overflowError) error

// overflowError describes one observer detached because the backlog reached its
// bound while the worker was inside that observer.
type overflowError struct {
	Label   string
	Lost    uint64 // Events captured for the observer that it will never receive.
	Through uint64 // Last sequence addressed to the observer; nothing after it is.
	Limit   int    // The backlog bound that was reached.
}

func (e *overflowError) Error() string {
	return fmt.Sprintf("observer %q overflowed the backlog bound of %d events: %d events lost through sequence %d, observer detached",
		e.Label, e.Limit, e.Lost, e.Through)
}

func (e *overflowError) Unwrap() error { return errObserverOverflow }

// logFunc runs on the same off-owner worker after the callback lease is released.
// Errors and panics from observers are logged once, skipped, and never retried;
// remaining observers and events continue. A nil logFunc disables logging.
// logFunc must not synchronously join this dispatcher. Its own panic is contained
// and recorded in status rather than recursively sent back to logFunc. logFunc
// calls are serialized even when an overflow hands the worker role over.
type logFunc func(error)

// registration fields after onOverflow are protected by dispatcher.mu.
type registration struct {
	owner      leaseHolder
	label      string
	callback   observerFunc
	onOverflow overflowHandler
	removed    bool
	first      uint64 // First sequence captured for this observer.
	last       uint64 // Last sequence captured; 0 while it is still registered.
	handled    uint64 // Captured events whose delivery to it has started.
	lost       uint64
	overflowed bool
	notified   bool
}

type envelope struct {
	sequence  uint64
	event     ledgerEvent
	listeners []*registration // Immutable COW list captured by append.
}

// observerStatus is a synchronized diagnostic snapshot. Delivered means that every
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
type observerStatus struct {
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

// closeError reports a bounded close that did not join the real worker. Work is
// not deleted or marked delivered; a later close can join after it actually exits.
type closeError struct {
	Cause  error
	Status observerStatus
}

func (e *closeError) Error() string {
	return fmt.Sprintf("observer close incomplete: accepted=%d delivered=%d remaining=%d queued=%d in-flight=%t",
		e.Status.Accepted, e.Status.Delivered, e.Status.Remaining, e.Status.Queued, e.Status.InFlight)
}

func (e *closeError) Unwrap() error { return e.Cause }

// panicError preserves the panic value and errors.Is identity when that value
// is an error. Its Error method is used only by off-owner error formatting in
// the dispatcher; logFunc receives an observerError with cached diagnostic text.
type panicError struct{ Value any }

func (e *panicError) Error() string { return fmt.Sprintf("observer callback panicked: %v", e.Value) }
func (e *panicError) Unwrap() error { cause, _ := e.Value.(error); return cause }

// observerError is an off-owner-formatted diagnostic. Error returns cached text
// so a logger retaining the error need not invoke the original Error/Stringer.
type observerError struct {
	Sequence uint64
	Label    string
	Cause    error
	text     string
}

func (e *observerError) Error() string { return e.text }
func (e *observerError) Unwrap() error { return e.Cause }

type callbackKey struct{}
type callbackMarker struct{ nonzero byte }

type pendingReport struct {
	sequence uint64
	label    string
	err      error
}

// Dispatcher serializes events and listeners on exactly one active worker.
// append is control-path bookkeeping only: its short mutation lock never spans
// lifecycle methods, observer callbacks, error formatting, or logging. It never
// waits for a callback or for queue capacity.
//
// Memory is bounded by dispatcherOptions.Backlog. When an append finds that many accepted
// events still undelivered, the observer the worker is inside (a stalled or too
// slow callback) is detached: it leaves the listener list, its lost events are
// counted, the overflow goes through the ordinary failure path, status and close,
// and the worker role passes to a new goroutine that resumes the very same event
// at the next listener. The stalled callback's goroutine finishes that callback
// (and a terminal overflowHandler) and exits. Observers that keep up lose
// nothing and keep the order. The backlog accumulated behind the stalled observer
// is not charged to the next observer: the bound counts events accepted since
// the last detachment, so the queue is at most Backlog*(1+k) while k detached
// observers' backlogs are still draining.
//
// Use newDispatcher to construct a dispatcher. It must not be copied.
type dispatcher struct {
	mu        sync.Mutex
	listeners []*registration
	queue     []envelope
	head      int
	status    observerStatus
	progress  chan struct{}
	wake      chan struct{}
	done      chan struct{}
	callback  context.Context
	cancel    context.CancelFunc
	marker    *callbackMarker
	logger    logFunc
	logMu     sync.Mutex // Serializes logFunc calls across worker generations.

	limit     int // Backlog bound; 0 only in algorithm-only literals: no bound.
	floor     uint64
	gen       uint64 // Generation of the one worker allowed to advance the queue.
	workers   int    // Live worker goroutines, including detached ones.
	active    bool   // env is being delivered.
	env       envelope
	idx       int              // Next listener of env.
	running   *registration    // Listener being or about to be run by the active worker.
	reports   []pendingReport  // Overflow diagnostics for the active worker to log.
	overflows []*overflowError // First maxOverflowDetails, for close.
}

// dispatcherOptions configure a dispatcher.
type dispatcherOptions struct {
	// Backlog bounds the accepted-but-undelivered events; it must be positive.
	Backlog int
}

// New starts one worker with the default backlog bound. Its callback context is
// independent of the context of any runtime operation, observer owner, barrier,
// or close caller.
// newDispatcher starts one worker. Its callback context is independent of the
// context of any core operation, observer owner, barrier, or close caller.
func newDispatcher(logger logFunc, options dispatcherOptions) *dispatcher {
	base, cancel := context.WithCancel(context.Background())
	marker := &callbackMarker{}
	d := &dispatcher{
		progress: make(chan struct{}), wake: make(chan struct{}, 1), done: make(chan struct{}),
		callback: context.WithValue(base, callbackKey{}, marker), cancel: cancel, marker: marker, logger: logger,
		limit: options.Backlog,
	}
	d.status.Backlog = options.Backlog
	go d.run()
	return d
}

// addObserver registers a native lifecycle cleanup effect before publishing a
// COW listener list. Setup holds a real owner lease, preventing cleanup from
// finishing between Defer and publication. No leaseHolder method runs under mu.
// Listener order within an event is registration-publication order.
func (d *dispatcher) addObserver(owner leaseHolder, label string, callback observerFunc) (disposer, error) {
	return d.addObserverWith(owner, label, callback, nil)
}

// addObserverWith is addObserver with an optional terminal signal for overflow.
func (d *dispatcher) addObserverWith(owner leaseHolder, label string, callback observerFunc, onOverflow overflowHandler) (disposer, error) {
	if owner == nil {
		return nil, errBinding
	}
	if label == "" || callback == nil {
		return nil, errObserverArgs
	}
	leave, ok := owner.enter()
	if !ok {
		return nil, errObserverInactive
	}
	defer leave()
	r := &registration{owner: owner, label: label, callback: callback, onOverflow: onOverflow}
	dispose := disposer(func(context.Context) error {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.removeLocked(r)
		return nil
	})
	if err := owner.tryDefer("observer "+label, dispose); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.status.Closing {
		return nil, errObserverClosed
	}
	if r.removed {
		return nil, errObserverInactive
	}
	r.first = d.status.Accepted + 1
	d.listeners = append(slices.Clone(d.listeners), r)
	return dispose, nil
}

// removeLocked takes r out of future snapshots; idempotent.
func (d *dispatcher) removeLocked(r *registration) {
	if !r.removed {
		r.removed = true
		if r.last == 0 {
			r.last = d.status.Accepted
		}
	}
	if index := slices.Index(d.listeners, r); index >= 0 {
		next := make([]*registration, 0, len(d.listeners)-1)
		next = append(next, d.listeners[:index]...)
		next = append(next, d.listeners[index+1:]...)
		d.listeners = next
	}
}

// append accepts only 1, 2, 3, ... in the same linearized order as the source
// state/service ledger. Zero, a duplicate, a backwards value, a gap, and uint64
// wraparound return errSequenceGap without modifying the queue or watermark.
// On success the event remains queued in memory until delivery; close does not
// delete it. This is not crash persistence. The event is copied by value only, with no
// caller-provided hooks.
//
// The source state owner may call this while it owns its ledger lock/turn. The
// listener list is captured here, not later when the worker reaches the event.
// When the backlog bound is reached the event is still accepted: the stalled
// observer is detached instead (see dispatcher), in constant time.
func (d *dispatcher) append(sequence uint64, event ledgerEvent) error {
	d.mu.Lock()
	if d.status.Closing {
		d.mu.Unlock()
		return errObserverClosed
	}
	if sequence == 0 || d.status.Accepted == ^uint64(0) || sequence != d.status.Accepted+1 {
		d.mu.Unlock()
		return errSequenceGap
	}
	if d.limit > 0 && d.backlogLocked() >= uint64(d.limit) {
		d.overflowLocked()
	}
	d.queue = append(d.queue, envelope{sequence: sequence, event: event, listeners: d.listeners})
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
func (d *dispatcher) backlogLocked() uint64 {
	return d.status.Accepted - max(d.status.Delivered, d.floor)
}

// overflowLocked detaches the observer the worker is inside and hands the worker
// role to a new goroutine, which resumes the same event at the next listener. If
// the worker is not inside any listener nothing can be blamed and nothing
// changes; the next append asks again.
func (d *dispatcher) overflowLocked() {
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
	failure := &overflowError{Label: l.label, Lost: l.lost, Through: l.last, Limit: d.limit}
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

// overflowErr reports every observer detached so far, or nil. close joins it.
func (d *dispatcher) overflowErr() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.overflowErrLocked()
}

func (d *dispatcher) overflowErrLocked() error {
	if d.status.Overflowed == 0 {
		return nil
	}
	errs := make([]error, 0, len(d.overflows)+1)
	for _, failure := range d.overflows {
		errs = append(errs, failure)
	}
	if more := d.status.Overflowed - uint64(len(d.overflows)); more > 0 {
		errs = append(errs, fmt.Errorf("%w: %d further observers detached", errObserverOverflow, more))
	}
	return errors.Join(errs...)
}

// barrier joins the accepted watermark captured at this call, not future
// appends. settle stays independent of observers; a caller that needs both calls
// settle and then this barrier.
func (d *dispatcher) barrier(ctx context.Context) error {
	return d.waitBarrier(ctx, d.snapshot().Accepted)
}

// waitBarrier joins delivery through an already accepted sequence. Zero joins
// the empty prefix. A target beyond Accepted returns errNotAccepted. Observer
// errors are diagnostics, not a barrier failure, and neither is an overflow:
// read it from status or overflowErr. A canceled wait does not cancel
// callbacks or lose accepted work. See Observer for the explicit self-join rule.
func (d *dispatcher) waitBarrier(ctx context.Context, sequence uint64) error {
	if d.fromCallback(ctx) {
		return errReentrant
	}
	for {
		d.mu.Lock()
		if sequence <= d.status.Delivered {
			d.mu.Unlock()
			return nil
		}
		if sequence > d.status.Accepted {
			d.mu.Unlock()
			return errNotAccepted
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

// status returns diagnostics without invoking user code.
func (d *dispatcher) snapshot() observerStatus {
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

// close stops admission, cancels the independent callback context, and joins
// every worker (the active one after it drains each accepted event, and any
// detached observer's goroutine after its callback returns). Queued callbacks
// still run with the canceled context if their owner lease permits. Cancellation
// is a cooperative request, not permission to kill a callback or report false
// drain. If ctx expires first, closeError reports the real remaining work. The
// workers remain live until callbacks/logging actually return; a later close can
// join. Once joined, close also returns every overflow (see overflowErr).
func (d *dispatcher) close(ctx context.Context) error {
	if d.fromCallback(ctx) {
		return errReentrant
	}
	d.mu.Lock()
	d.status.Closing = true
	d.mu.Unlock()
	d.cancel()
	d.poke()
	select {
	case <-d.done:
		return d.overflowErr()
	default:
	}
	select {
	case <-d.done:
	case <-ctx.Done():
	}
	select {
	case <-d.done: // A worker that finished as ctx ended counts as joined.
		return d.overflowErr()
	default:
	}
	var err error = &closeError{Cause: ctx.Err(), Status: d.snapshot()}
	if overflow := d.overflowErr(); overflow != nil {
		err = errors.Join(err, overflow)
	}
	return err
}

func (d *dispatcher) fromCallback(ctx context.Context) bool {
	return ctx.Value(callbackKey{}) == d.marker
}

func (d *dispatcher) poke() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *dispatcher) signalLocked() {
	close(d.progress)
	d.progress = make(chan struct{})
}

// run starts the first worker.
func (d *dispatcher) run() {
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
func (d *dispatcher) loop(gen uint64) {
	defer d.exit(gen)
	var prev *registration
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
func (d *dispatcher) advanceLocked() (l *registration, sequence uint64, event ledgerEvent, step int) {
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
			d.env, d.active = envelope{}, false
		}
		if d.head == len(d.queue) {
			if d.status.Closing {
				return nil, 0, event, stepStop
			}
			return nil, 0, event, stepIdle
		}
		d.env, d.idx, d.active = d.queue[d.head], 0, true
		d.queue[d.head] = envelope{}
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
func (d *dispatcher) exit(gen uint64) {
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
func (d *dispatcher) terminal(l *registration) {
	d.mu.Lock()
	handler := l.onOverflow
	if handler == nil || l.notified {
		d.mu.Unlock()
		return
	}
	l.notified = true
	failure := &overflowError{Label: l.label, Lost: l.lost, Through: l.last, Limit: d.limit}
	d.mu.Unlock()
	if err := invokeTerminal(d.callback, l, handler, failure); err != nil {
		d.report(failure.Through, l.label, err)
	}
}

// Only this invocation boundary contains observer/lease panics. In particular,
// nothing is installed around the source state owner or its event publisher.
func invoke(ctx context.Context, r *registration, event ledgerEvent) (called bool, err error) {
	defer func() {
		if cause := recover(); cause != nil {
			err = &panicError{Value: cause}
		}
	}()
	leave, ok := r.owner.enter()
	if !ok {
		if l, ok := r.owner.(lateDelivery); !ok || !l.deliverLate() {
			return false, nil
		}
		leave = func() {}
	}
	defer leave()
	called = true
	return called, r.callback(ctx, event)
}

func invokeTerminal(ctx context.Context, r *registration, handler overflowHandler, failure *overflowError) (err error) {
	defer func() {
		if cause := recover(); cause != nil {
			err = &panicError{Value: cause}
		}
	}()
	leave, ok := r.owner.enter()
	if !ok {
		return nil
	}
	defer leave()
	return handler(ctx, failure)
}

func describeError(err error) (text string, panicked bool) {
	defer func() {
		if cause := recover(); cause != nil {
			// Do not format the new panic payload: a second hostile formatter
			// must not turn diagnostic containment into recursive callbacks.
			text, panicked = "observer error formatting panicked", true
		}
	}()
	return err.Error(), false
}

func (d *dispatcher) report(sequence uint64, label string, cause error) {
	text, formattingPanic := describeError(cause)
	diagnostic := &observerError{Sequence: sequence, Label: label, Cause: cause,
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
		// Report the logger failure through the status, never by reentering logFunc.
		text, formattingPanic := describeError(loggerPanic)
		d.mu.Lock()
		d.status.LoggerPanics++
		d.status.LastLoggerError = text
		if formattingPanic {
			d.status.FormattingPanics++
		}
		d.mu.Unlock()
	}
}

func callLogger(logger logFunc, err error) (failure error) {
	defer func() {
		if cause := recover(); cause != nil {
			failure = &panicError{Value: cause}
		}
	}()
	logger(err)
	return nil
}

// observerSet is the core's single shared dispatcher. It is constructed once,
// before the first native event, and only the owner appends its consecutive
// ledger events. No callback, binding method, or error formatting runs in append.
//
// Admission failures indicate a broken core integration (a sequence gap or an
// early queue close). The first one is retained even when the owner ignores the
// append result, so external barrier and close callers cannot see false success.
type observerSet struct {
	d   *dispatcher
	mu  sync.Mutex
	err error
}

func newObserverSet(backlog int, log logFunc) *observerSet {
	return &observerSet{d: newDispatcher(log, dispatcherOptions{Backlog: backlog})}
}

func (n *observerSet) append(e ledgerEvent) error {
	err := n.d.append(e.Sequence, e)
	if err != nil {
		n.mu.Lock()
		if n.err == nil {
			n.err = err
		}
		n.mu.Unlock()
	}
	return err
}

// observe registers fn, and the optional terminal overflow signal, for events
// from now on. Both are canceled when owner's Std context is, so a callback never
// outlives its activation's quiescence. The caller has already checked that owner
// belongs to this core: registration calls the owner's tryDefer and can reenter
// the core, so it never runs on the owner goroutine.
func (n *observerSet) observe(owner *handle, root bool, label string, fn func(context.Context, ledgerEvent) error, onOverflow func(context.Context, *overflowError) error) (disposer, error) {
	var callback observerFunc
	if fn != nil {
		callback = func(dispatcherCtx context.Context, event ledgerEvent) error {
			// This invocation already holds the owner's lease and runs off the
			// state owner. Cancel before the lease drain joins, not only when the
			// dispatcher can finally close after lifecycle teardown.
			ctx, cancel := context.WithCancel(dispatcherCtx)
			stop := context.AfterFunc(owner.std, cancel)
			defer stop()
			defer cancel()
			return fn(ctx, event)
		}
	}
	var terminal overflowHandler
	if onOverflow != nil {
		terminal = func(dispatcherCtx context.Context, failure *overflowError) error {
			ctx, cancel := context.WithCancel(dispatcherCtx)
			stop := context.AfterFunc(owner.std, cancel)
			defer stop()
			defer cancel()
			return onOverflow(ctx, failure)
		}
	}
	var lease leaseHolder = owner
	if root {
		lease = rootLease{owner}
	}
	dispose, err := n.d.addObserverWith(lease, label, callback, terminal)
	if errors.Is(err, errObserverInactive) {
		return nil, ErrInactive
	}
	return dispose, err
}

func (n *observerSet) await(ctx context.Context) error {
	// An overflow is a failure of observation, not a successful full drain.
	return errors.Join(n.d.barrier(ctx), n.failure(), n.d.overflowErr())
}

func (n *observerSet) close(ctx context.Context) error {
	return errors.Join(n.d.close(ctx), n.failure())
}

func (n *observerSet) failure() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.err
}

// observe subscribes fn to the total-order event stream, in the order of the
// event sequence. Every event carries its sequence number. It binds the listener
// to owner's exact activation: the disposer removes it from future enqueue-time
// snapshots, quiescence rejects new callback leases, and cleanup waits for a
// callback that already holds a lease. onOverflow may be nil.
//
// The stream is bounded by coreConfig.observerQueue. The kernel never waits for an
// observer, so the bound cannot push back: when an event arrives while that many
// accepted events are still undelivered, the observer the worker is inside is
// detached. It receives no further event, its lost events are counted, the
// overflow goes through the failure path (observerStatus.ObserverErrors), the
// status, the error of awaitObservers and close (errors.Is with
// errObserverOverflow), and, if onOverflow is set, one terminal call once its
// callback returns. Every other observer keeps every event, in order.
func (r *core) observe(owner *handle, label string, fn func(context.Context, ledgerEvent) error, onOverflow func(context.Context, *overflowError) error) (disposer, error) {
	if owner == nil || owner.core != r {
		return nil, errObserverBinding
	}
	return r.observers.observe(owner, false, label, fn, onOverflow)
}

// observeRoot is observe for the observer of the root (Root.Observe): it keeps
// receiving the events of Close (see rootLease).
func (r *core) observeRoot(owner *handle, label string, fn func(context.Context, ledgerEvent) error, onOverflow func(context.Context, *overflowError) error) (disposer, error) {
	return r.observers.observe(owner, true, label, fn, onOverflow)
}

// awaitObservers joins only the accepted watermark captured at the call, not
// events appended later. A callback must not join its own worker: passing its
// supplied context returns errReentrant.
func (r *core) awaitObservers(ctx context.Context) error {
	return r.observers.await(ctx)
}

// observerStatus reports the dispatcher's counters without invoking user code.
func (r *core) observerStatus() observerStatus {
	return r.observers.d.snapshot()
}
