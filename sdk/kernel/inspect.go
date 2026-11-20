// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync/atomic"
)

// State is the observable lifecycle state of a fiber
// (docs/architecture/07-c4-plugin-sdk.md, section 3.3).
type State string

// The six fiber states.
const (
	Pending   State = "PENDING"
	Loading   State = "LOADING"
	Active    State = "ACTIVE"
	Failed    State = "FAILED"
	Unloading State = "UNLOADING"
	Disposed  State = "DISPOSED"
)

// fiberSnap is the immutable view of one fiber incarnation, copied on the owner.
type fiberSnap struct {
	ID         uint64
	Name       string
	State      State
	Revision   uint64
	Epoch      []uint64     // Retained activation dependencies, not the desired target.
	Effects    []string     // Effect labels in registration order.
	EffectTree []effectMeta // Actual native ownership subtrees, detached on inspection.
	Tracked    []string     // Labels of tracked work that has not actually returned.
	Error      string
	Alive      bool // False once disposal is requested, before cleanup.
	// Leaked is set once a disposer ran past the dispose deadline: a goroutine
	// cannot be killed, so the launcher replaces the process (07-c4-plugin-sdk.md:359).
	// It stays set for the incarnation, including its archived snapshot.
	Leaked bool
	Target *epochSnap // Nil when the desired epoch is inactive.
}

// epochSnap is the desired revision and provision-identity tuple. The current
// activation may retain a different committed epoch while it is unloading.
type epochSnap struct {
	Revision uint64
	UIDs     []uint64
}

// provisionSnap is one real reserved provision, including non-ACTIVE
// reservations. Entries follow uid creation order.
type provisionSnap struct {
	Key    svcKey
	UID    uint64
	Owner  uint64
	Name   string
	Value  any
	Active bool
}

// graphView is an owner-turn view of the graph that runs no plugin callback.
type graphView struct {
	Fibers     []fiberSnap
	Provisions []provisionSnap
	// Sequence is the ledger watermark from this same owner turn. It permits
	// observation sidecars without combining state with future events.
	Sequence uint64
}

// ledgerEvent captures the total state and service order, before any observer
// executes. Every event delivered to an observer carries its Sequence, which is
// consecutive from 1 and is the total order of the core.
type ledgerEvent struct {
	Sequence uint64
	Fiber    uint64
	Name     string // pluginSpec.ID, available even before mount returns.
	Kind     string
	From     State
	State    State
	Key      svcKey // The service key, or a label for effect and tracked-work events.
	UID      uint64
	Value    any // Observed service value.
}

// Retention contract.
//
// Fiber IDs are issued from a counter that only grows, and an ID is never
// reused, so "an ID at or below the last issued one that is not live" is
// decidable without a stored record. After a fiber incarnation is DISPOSED and
// its teardown has been joined, the core keeps only:
//
//   - a bounded, insertion-ordered archive of the most recently disposed
//     incarnations (coreConfig.archiveLimit records; the oldest is evicted
//     first). A record carries the DISPOSED snapshot and the retained startup
//     error, so snapshots, inspect, dispose and awaitFiber on a recently
//     disposed fiber behave as for a live one;
//   - for an ID whose record has been evicted, no per-incarnation state at all.
//     Such an ID is "expired": awaitFiber returns errExpired (the startup error
//     is no longer known, so success must not be claimed), dispose is the same
//     idempotent no-op as for an archived ID, any other command answers as for
//     any disposed fiber, and the fiber is absent from snapshots and inspect. An
//     ID that was never issued is still errUnknown.
//
// This mirrors the Cordis oracle, where a disposed fiber leaves the registry
// and is garbage once unreferenced: its state and error stay reachable only
// through a reference the caller holds. A caller that waits on a fiber keeps
// the exact record alive here too: a waiter registered before disposal still
// receives the retained startup error even if the archive evicted the record
// before the waiter was served.
//
// The event ledger is bounded as well: the core keeps the most recent
// coreConfig.eventWindow events. Once older events were dropped, events returns
// the tail with errEventsTruncated, never a silent partial list. The observer
// queue still receives every event.

var (
	// errUnknown reports an ID that was never issued.
	errUnknown = errors.New("unknown fiber")
	// errClosed reports a command sent to a core that has closed.
	errClosed = errors.New("runtime closed")
	// errRootDispose reports a dispose request on the root activation.
	errRootDispose = errors.New("root disposal is owned by close")
	// errExpired reports an ID that was issued and disposed, whose archive
	// record has been evicted. It is distinct from errUnknown (never issued).
	errExpired = errors.New("fiber record expired")
	// errEventsTruncated accompanies the retained tail returned by events when
	// older events were dropped.
	errEventsTruncated = errors.New("event history truncated to the event window")
	// errObserverBinding rejects a nil handle or one from another core.
	errObserverBinding = errors.New("observer context belongs to another runtime")
)

// retentionStatus is a lock-free diagnostic of the bounds and their use.
type retentionStatus struct {
	ArchiveLimit    int
	Archived        int    // Records currently held.
	ArchiveEvicted  uint64 // Records dropped to honour ArchiveLimit.
	EventWindow     int
	Windowed        int    // Events currently held in the tail.
	EventsTotal     uint64 // Events ever appended (the last sequence).
	ObserverBacklog int    // Bound on undelivered events behind a stalled observer.
	// TerminalDropped counts diagnostics of past failed incarnations dropped
	// beyond coreConfig.terminalLimit.
	TerminalDropped uint64
}

// retentionStatus reads the bounds and their use without entering the owner.
func (r *core) retentionStatus() retentionStatus {
	return retentionStatus{
		ArchiveLimit: r.cfg.archiveLimit, Archived: int(r.archive.n.Load()), ArchiveEvicted: r.archive.evicted.Load(),
		EventWindow: r.cfg.eventWindow, Windowed: int(r.ledger.wlen.Load()), EventsTotal: r.ledger.total.Load(),
		ObserverBacklog: r.cfg.observerQueue, TerminalDropped: r.terminalDropped.Load(),
	}
}

// recordArchive is an insertion-ordered, size-bounded map of retired records.
// It is not synchronized; the owner serializes writers and only the counters are
// read concurrently.
type recordArchive[T any] struct {
	limit   int
	records map[uint64]T
	order   []uint64 // Ring of IDs, oldest at head once full.
	head    int
	n       atomic.Int64
	evicted atomic.Uint64
}

func newRecordArchive[T any](limit int) *recordArchive[T] {
	return &recordArchive[T]{limit: limit, records: make(map[uint64]T)}
}

func (a *recordArchive[T]) put(id uint64, record T) {
	if _, exists := a.records[id]; !exists {
		if len(a.order) < a.limit {
			a.order = append(a.order, id)
		} else {
			delete(a.records, a.order[a.head])
			a.order[a.head] = id
			a.head = (a.head + 1) % a.limit
			a.evicted.Add(1)
		}
	}
	a.records[id] = record
	a.n.Store(int64(len(a.records)))
}

func (a *recordArchive[T]) get(id uint64) (T, bool) {
	record, ok := a.records[id]
	return record, ok
}

// each visits records in no particular order.
func (a *recordArchive[T]) each(visit func(uint64, T)) {
	for id, record := range a.records {
		visit(id, record)
	}
}

// eventLedger holds the bounded tail. The owner serializes append and events; the
// counters are safe to read concurrently.
type eventLedger struct {
	window []ledgerEvent // Ring, oldest at wstart once full.
	wstart int
	wlimit int
	total  atomic.Uint64
	wlen   atomic.Int64
}

func newEventLedger(window int) *eventLedger { return &eventLedger{wlimit: window} }

// next is the sequence the next appended event must carry.
func (l *eventLedger) next() uint64 { return l.total.Load() + 1 }

func (l *eventLedger) append(e ledgerEvent) {
	l.total.Store(e.Sequence)
	if len(l.window) < l.wlimit {
		l.window = append(l.window, e)
	} else {
		l.window[l.wstart] = e
		l.wstart = (l.wstart + 1) % l.wlimit
	}
	l.wlen.Store(int64(len(l.window)))
}

// events copies the retained tail, with errEventsTruncated when older events
// were dropped.
func (l *eventLedger) events() ([]ledgerEvent, error) {
	tail := make([]ledgerEvent, 0, len(l.window))
	tail = append(tail, l.window[l.wstart:]...)
	tail = append(tail, l.window[:l.wstart]...)
	if l.total.Load() > uint64(len(tail)) {
		return tail, fmt.Errorf("%w: %d events appended, last %d kept", errEventsTruncated, l.total.Load(), len(tail))
	}
	return tail, nil
}

// retired is the bounded record of one disposed incarnation.
type retired struct {
	snapshot fiberSnap
	awaitErr error // Sticky startup-error identity for await after disposal.
}

// waiter is a parked settle, close or await. Its fields belong to the owner,
// except next, which links the cancellation stack (see cancelWaiter).
type waiter struct {
	ctx   context.Context
	ack   chan answer
	stop  func() bool
	kind  command
	fiber *fiber // await only: the waiter's own reference, so disposal never loses the error.
	done  bool   // Replied or pruned.
	next  *waiter
}

// snapshot copies the observable state of f. It runs on the owner.
func (o *owner) snapshot(f *fiber) fiberSnap {
	s := fiberSnap{ID: f.id, Name: f.spec.ID, State: f.state, Revision: f.revision, Alive: alive(f), Leaked: f.leaked}
	if s.Alive && f.target != nil {
		s.Target = &epochSnap{Revision: f.target[0], UIDs: slices.Clone(f.target[1:])}
	}
	if f.act != nil {
		s.Epoch = slices.Clone(f.act.epoch[1:])
		trackedIDs := make([]uint64, 0, len(f.act.tracked))
		for id := range f.act.tracked {
			trackedIDs = append(trackedIDs, id)
		}
		slices.Sort(trackedIDs)
		for _, id := range trackedIDs {
			s.Tracked = append(s.Tracked, f.act.tracked[id].label)
		}
	}
	for _, e := range f.effects {
		if e.child != 0 {
			continue
		}
		s.Effects = append(s.Effects, e.label)
		if e.scope != nil {
			s.EffectTree = append(s.EffectTree, e.scope.metadata())
		} else {
			s.EffectTree = append(s.EffectTree, effectMeta{Label: e.label, State: scopeReady})
		}
	}
	// A load deadline is both the activation failure and the blocked diagnostic:
	// show that line once.
	blockedText := f.blockedText
	if blockedText == f.failureText {
		blockedText = ""
	}
	s.Error = joinText(f.failureText, blockedText, f.checkText, f.awaitText, f.scopeText)
	return s
}

func (o *owner) snapshots() []fiberSnap {
	ss := make([]fiberSnap, 0, len(o.fibers)+int(o.r.archive.n.Load()))
	var waiting map[uint64]string
	if len(o.blocked) != 0 {
		order, via := o.blockedClosure()
		waiting = make(map[uint64]string, len(order))
		for _, f := range order {
			if g := via[f]; g != nil {
				waiting[f.id] = waitingText(f, g)
			}
		}
	}
	for _, id := range o.ids() {
		s := o.snapshot(o.fibers[id])
		if text := waiting[id]; text != "" {
			s.Error = joinText(s.Error, text) // Names the blocked fiber it waits for.
		}
		ss = append(ss, s)
	}
	o.r.archive.each(func(_ uint64, record retired) {
		archived := record.snapshot
		archived.Epoch = slices.Clone(archived.Epoch)
		archived.Effects = slices.Clone(archived.Effects)
		archived.EffectTree = slices.Clone(archived.EffectTree)
		archived.Tracked = slices.Clone(archived.Tracked)
		ss = append(ss, archived)
	})
	slices.SortFunc(ss, func(a, b fiberSnap) int { return cmp.Compare(a.ID, b.ID) })
	return ss
}

func (o *owner) inspect() graphView {
	view := graphView{Sequence: o.r.ledger.total.Load(), Fibers: o.snapshots(), Provisions: make([]provisionSnap, 0, len(o.providers))}
	for key, p := range o.providers {
		owner := o.lookup(p.binding.owner)
		// The provider table owns each record until its real provision effect
		// removes it. Values are the retained binding, never a fabricated zero.
		entry := provisionSnap{Key: key, UID: p.binding.uid, Owner: p.binding.owner, Value: p.binding.value, Active: providerActive(owner)}
		if owner != nil {
			entry.Name = owner.spec.ID
		}
		view.Provisions = append(view.Provisions, entry)
	}
	slices.SortFunc(view.Provisions, func(a, b provisionSnap) int { return cmp.Compare(a.UID, b.UID) })
	return view
}

func (o *owner) waiter(m message) *waiter {
	w := &waiter{ctx: m.ctx, ack: m.ack, kind: m.kind}
	w.stop = context.AfterFunc(m.ctx, func() { o.r.cancelWaiter(w) })
	return w
}

// cancelWaiter runs on the context's own goroutine: it hands the waiter to the
// owner, which prunes exactly the waiters whose context ended instead of
// re-checking every parked waiter on every turn.
func (r *core) cancelWaiter(w *waiter) {
	for {
		head := r.cancelled.Load()
		w.next = head
		if r.cancelled.CompareAndSwap(head, w) {
			break
		}
	}
	r.poke()
}

// touch records that f changed in a way a parked await must look at.
func (o *owner) touch(f *fiber) {
	if f.awaiters != 0 && !f.dirty {
		f.dirty = true
		o.dirty = append(o.dirty, f)
	}
}

// units is how many o.busy units f holds: one for a transition in flight and
// one for an availability check in flight.
func units(f *fiber) int {
	n := 0
	if f.phase != phIdle {
		n++
	}
	if f.checking != nil {
		n++
	}
	return n
}

// consumeCancelled answers the parked waiters whose context ended. Settle and
// Close get the context's error joined with the diagnostics known so far.
func (o *owner) consumeCancelled() {
	n := o.r.cancelled.Swap(nil)
	if n == nil {
		return
	}
	var known []error
	var haveKnown, settlers, closers bool
	var awaited map[uint64]struct{}
	for ; n != nil; n = n.next {
		if n.done {
			continue
		}
		n.done = true
		err := n.ctx.Err() // Non-nil: only the AfterFunc of a finished context queues a waiter.
		switch n.kind {
		case cmdSettle, cmdClose:
			if !haveKnown {
				known, haveKnown = o.diagnostics(), true
			}
			n.ack <- answer{err: errors.Join(append([]error{err}, known...)...)}
			settlers, closers = settlers || n.kind == cmdSettle, closers || n.kind == cmdClose
		default:
			n.ack <- answer{err: err}
			n.fiber.awaiters--
			if awaited == nil {
				awaited = make(map[uint64]struct{})
			}
			awaited[n.fiber.id] = struct{}{}
		}
	}
	finished := func(w *waiter) bool { return w.done }
	if settlers {
		o.settlers = slices.DeleteFunc(o.settlers, finished)
	}
	if closers {
		o.closers = slices.DeleteFunc(o.closers, finished)
	}
	for id := range awaited {
		if waiters := slices.DeleteFunc(o.awaiting[id], finished); len(waiters) != 0 {
			o.awaiting[id] = waiters
		} else {
			delete(o.awaiting, id)
		}
	}
}

// diagnostics is everything Settle and Close report: failed activations, one
// entry per blocked fiber, and the retained terminal diagnostics.
func (o *owner) diagnostics() []error {
	var failures []error
	for id, err := range o.failures {
		if o.blocked[id] == err {
			continue // A deadline is the failure and the blocked fiber's diagnostic: once.
		}
		failures = append(failures, &diagnostic{text: fmt.Sprintf("fiber %d: %s", id, o.lookup(id).failureText), cause: err})
	}
	failures = append(failures, o.blockedDiagnostics()...)
	// Sort diagnostics only: lifecycle events are never reordered.
	sort.Slice(failures, func(i, j int) bool { return failures[i].Error() < failures[j].Error() })
	return append(failures, o.terminalErrors()...)
}

// settleReady reports whether Settle may answer now, and whether a fiber is
// blocked. It may answer when no fiber is mid-transition except fibers that are
// blocked: past one of their own deadlines, or waiting on such a fiber (see
// blockedClosure). o.busy counts the units of every mid-transition fiber, so the
// two are compared as totals; with no blocked fiber this is O(1) per turn.
func (o *owner) settleReady() (ready, blocked bool) {
	if o.busy == 0 {
		return true, len(o.blocked) != 0
	}
	if len(o.blocked) == 0 {
		return false, false
	}
	order, _ := o.blockedClosure()
	total := 0
	for _, f := range order {
		total += units(f)
	}
	return total == o.busy, true
}

// finishWaiters answers the waiters whose condition may have changed this turn:
// the awaiters of touched fibers, and Settle/Close when every mid-transition
// fiber is blocked or none is. A parked waiter costs nothing per turn.
func (o *owner) finishWaiters() {
	for _, f := range o.dirty {
		f.dirty = false
		waiters := o.awaiting[f.id]
		if len(waiters) == 0 {
			continue
		}
		gone := o.fibers[f.id] != f
		if gone || (f.phase == phIdle && f.checking == nil) || f.blocked != nil {
			var err error
			if gone {
				err = waiters[0].fiber.awaitErr // The waiter's own reference: exact even after archive eviction.
			} else {
				err = errors.Join(f.awaitErr, f.blocked)
			}
			o.replyAll(&waiters, err)
			delete(o.awaiting, f.id)
			f.awaiters = 0
		}
	}
	clear(o.dirty)
	o.dirty = o.dirty[:0]
	if len(o.settlers) == 0 && len(o.closers) == 0 {
		return
	}
	ready, blocked := o.settleReady()
	if !ready {
		return
	}
	err := errors.Join(o.diagnostics()...)
	o.replyAll(&o.settlers, err)
	if blocked {
		o.replyAll(&o.closers, err)
	}
}

func (o *owner) replyAll(waiters *[]*waiter, err error) {
	for _, w := range *waiters {
		w.done = true
		w.stop()
		switch {
		case w.ctx.Err() == nil:
			w.ack <- answer{err: err}
		case w.kind == cmdSettle || w.kind == cmdClose:
			w.ack <- answer{err: errors.Join(w.ctx.Err(), err)}
		default:
			w.ack <- answer{err: w.ctx.Err()}
		}
	}
	clear(*waiters)
	*waiters = nil
}

func (o *owner) fail(f *fiber, err error, text ...string) {
	f.failure = err
	f.failureText = joinText(text...)
	if err != nil {
		f.target = nil
	}
	if err == nil {
		delete(o.failures, f.id)
	} else {
		o.failures[f.id] = err
	}
}

func (o *owner) block(f *fiber, err error) {
	f.blocked = err
	o.touch(f)
	// Only owner-generated deadline errors enter block; their formatting invokes
	// no plugin behavior. Callback diagnostics arrive with their cached text.
	f.blockedText = ""
	if err != nil {
		f.blockedText = err.Error()
	}
	if err == nil {
		delete(o.blocked, f.id)
	} else {
		o.blocked[f.id] = err
	}
}

// awaited lists the fibers f is waiting on once its own work is done: the
// dependants that still hold its provisions, its structural children while it
// joins them, or, for the closing root, every fiber. Sorted by id.
func (o *owner) awaited(f *fiber) []*fiber {
	var out []*fiber
	switch {
	case f.phase == phDraining && !o.ownBusy(f):
		if f.id == 0 && o.closing {
			for _, id := range o.ids() {
				out = append(out, o.fibers[id])
			}
			return out
		}
		seen := make(map[*fiber]bool)
		for _, p := range f.provisions {
			for _, d := range p.dependents {
				if d != f && !seen[d] {
					seen[d] = true
					out = append(out, d)
				}
			}
		}
	case f.phase == phDisposing && f.joining:
		for id := range f.act.children {
			if c := o.fibers[id]; c != nil {
				out = append(out, c)
			}
		}
	}
	slices.SortFunc(out, func(a, b *fiber) int { return cmp.Compare(a.id, b.id) })
	return out
}

// waitersOf lists the fibers that may be waiting on g: the owners of the
// provisions g still holds, its structural parent, and the closing root.
func (o *owner) waitersOf(g *fiber) []*fiber {
	var out []*fiber
	add := func(id uint64) {
		if h := o.lookup(id); h != nil && h != g {
			out = append(out, h)
		}
	}
	for _, p := range g.committed {
		add(p.binding.owner)
	}
	for _, p := range g.releaseBindings {
		add(p.binding.owner)
	}
	if q := g.checking; q != nil {
		for _, p := range q.bindings {
			add(p.binding.owner)
		}
	}
	if g.parent != nil {
		add(g.parent.ctx.fiber)
	}
	if o.closing && o.root != nil && g != o.root {
		out = append(out, o.root)
	}
	return out
}

// blockedClosure returns the fibers that cannot make progress, in discovery
// order, each with the fiber it waits on (nil for a fiber past one of its own
// deadlines). A fiber that waits on a blocked fiber is blocked too, so one hung
// callback is reported once at its own stage and once by name on every fiber
// queued behind it, never as a deadline of the waiters.
func (o *owner) blockedClosure() ([]*fiber, map[*fiber]*fiber) {
	ids := make([]uint64, 0, len(o.blocked))
	for id := range o.blocked {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	via := make(map[*fiber]*fiber, len(ids))
	var order []*fiber
	for _, id := range ids {
		if f := o.lookup(id); f != nil {
			via[f] = nil
			order = append(order, f)
		}
	}
	for i := 0; i < len(order); i++ {
		g := order[i]
		for _, h := range o.waitersOf(g) {
			if _, seen := via[h]; seen || !slices.Contains(o.awaited(h), g) {
				continue
			}
			via[h] = g
			order = append(order, h)
		}
	}
	return order, via
}

// waitingText is what a fiber reports while it waits on a blocked fiber g.
func waitingText(f, g *fiber) string {
	return fmt.Sprintf("fiber %d drain: waiting for fiber %d", f.id, g.id)
}

// blockedDiagnostics is one diagnostic per blocked fiber: its own deadline error,
// or, for a fiber waiting on a blocked one, a diagnostic naming that fiber that
// still unwraps to the awaited fiber's cause.
func (o *owner) blockedDiagnostics() []error {
	order, via := o.blockedClosure()
	errs := make(map[*fiber]error, len(order))
	out := make([]error, 0, len(order))
	for _, f := range order {
		if g := via[f]; g == nil {
			errs[f] = o.blocked[f.id]
		} else {
			errs[f] = &diagnostic{text: waitingText(f, g), cause: errs[g]}
		}
		out = append(out, errs[f])
	}
	return out
}
