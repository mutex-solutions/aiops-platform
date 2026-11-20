// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// command is the kind of one message to the owner goroutine.
type command uint8

const (
	cmdMount command = iota
	cmdMountChild
	cmdUpdate
	cmdDispose
	cmdSettle
	cmdSnapshot
	cmdInspect
	cmdEvents
	cmdClose
	cmdProvide
	cmdEffect
	cmdTrackedStart
	cmdTrackedDone
	cmdLoadDone
	cmdEffectDone
	cmdNotify
	cmdCheckDone
	cmdAwait
	cmdRoot
	cmdBatch
	cmdSelfDispose
	cmdScopeRequest
	cmdScopeWithdraw
	cmdScopeDone
	cmdFiber
)

// message is one entry of the owner's inbox: a command from a caller or the
// completion of an off-owner callback.
type message struct {
	kind     command
	ctx      context.Context
	ack      chan answer
	claim    *atomic.Uint32 // Admission word of an abandonable command; nil for waits and completions.
	id       uint64
	spec     pluginSpec
	act      *activation
	key      svcKey
	label    string
	value    any
	dispose  disposer
	err      error
	errText  string    // Rendered by the callback worker, never by the graph owner.
	at       time.Time // Completions: when the callback returned, read on the worker (see handshake).
	keys     []svcKey
	check    *availCheck
	allowed  bool
	batch    []batchOp
	scope    *scopeEffect
	setup    func(*effectScope) error
	withdraw bool
}

// answer is the owner's reply to one command.
type answer struct {
	id        uint64
	snapshots []fiberSnap
	view      graphView
	events    []ledgerEvent
	err       error
	root      *handle
	ids       []uint64
	handle    *effectHandle
	run       *handshake // Tracked start: the handshake its completion stamps.
}

type notice struct {
	next   *notice
	queued atomic.Bool
	act    *activation
}

type activation struct {
	ctx         *handle
	cancel      context.CancelFunc
	epoch       []uint64
	selfDispose atomic.Bool
	freed       handshake // First instant the lease count of this quiesced activation reached zero.
	notice      notice
	children    map[uint64]struct{}    // Owner-only lifecycle children of this activation.
	tracked     map[uint64]trackedWork // Owner-only records until actual worker completion.
	nextTracked uint64
}

type availCheck struct {
	ctx        *handle
	cancel     context.CancelFunc
	epoch      []uint64
	bindings   map[svcKey]*liveProvision
	generation uint64
	deadline   time.Time
	run        *handshake // The check callback's handshake.
	timedOut   bool
	done       bool           // Result delivered to the owner; a pending service event may follow.
	holds      []*heldService // Held service events still waiting on this check.
}

// heldService is a service notification whose dependants still have
// availability checks in flight. The checks run off the owner, so the event is
// held (with source cleanup deferred) until their results have been applied.
type heldService struct {
	event   ledgerEvent
	pending int // Checks it still waits for; it may be emitted at zero, in queue order.
}

// pendingLoad is a load worker not yet started: its fiber needs a key whose
// service event is still held, and the oracle records that event after the
// fiber's LOADING and before anything its load does.
type pendingLoad struct {
	seq   uint64 // Insertion order: loads released together start in this order.
	needs []svcKey
	start func()
}

type phase uint8

const (
	phIdle phase = iota
	phApplying
	phDraining
	phDisposing
	phScoping
)

type liveProvision struct {
	key        svcKey
	binding    provision
	dependents map[*handle]*fiber
}

type effectRec struct {
	label     string
	dispose   disposer
	provision *liveProvision
	scope     *scopeEffect
	child     uint64 // Structural child: disposal is requested at this slot of the LIFO walk. Not observable as an effect.
}

// None of these fields are written by Apply, disposers, or call-path users.
type fiber struct {
	id              uint64
	spec            pluginSpec
	state           State
	revision        uint64
	act             *activation
	parent          *activation // Exact owning activation, never a replacement.
	committed       map[svcKey]*liveProvision
	releasing       *handle // Old dependency links survive until the next stable/LOADING state.
	releaseBindings map[svcKey]*liveProvision
	provisions      []*liveProvision
	effects         []effectRec
	phase           phase
	invalid         bool
	remove          bool
	queued          bool
	failure         error
	leaked          bool  // A disposer ran past the dispose deadline (see expire); sticky for the incarnation.
	awaitErr        error // Activation/startup failure retained by Await, not cleanup diagnostics.
	failureText     string
	awaitText       string
	blockedText     string
	checkText       string
	scopeText       string // Cached operation diagnostics do not fail the activation.
	blocked         error
	deadline        time.Time
	due             time.Time  // A passed deadline whose callback finished in time: re-armed if more work follows under the same budget.
	run             *handshake // Handshake of the load, scope or dispose callback in flight.
	deadlineIndex   int
	awaiters        int  // Parked await calls on this fiber.
	dirty           bool // Queued in owner.dirty.
	cleanupCtx      context.Context
	cleanupCancel   context.CancelFunc
	effectRunning   bool
	joining         bool // Effect walk finished; waiting for structural children to be DISPOSED.
	scopeRunning    *scopeEffect
	publishActive   bool // Apply completed; queued scope work precedes ACTIVE.
	checking        *availCheck
	checkVersion    uint64
	checked         uint64
	checkedEpoch    []uint64
	checkAllowed    bool
	checkError      error
	deniedEpoch     []uint64
	// target is the runner's desired epoch, not the activation retained during
	// cleanup. Only lifecycle commands and completed availability checks change it.
	target []uint64
}

type owner struct {
	r             *core
	fibers        map[uint64]*fiber
	root          *fiber // Trusted host anchor; never an ordinary plugin fiber.
	providers     map[svcKey]*liveProvision
	byKey         map[svcKey]map[uint64]*fiber
	queue         []uint64
	deadlines     deadlineHeap
	busy          int
	failures      map[uint64]error
	blocked       map[uint64]error
	nextFiber     uint64
	nextUID       uint64
	settlers      []*waiter
	closers       []*waiter
	closing       bool
	terminal      []error
	awaiting      map[uint64][]*waiter
	dirty         []*fiber // Awaited fibers whose state may have changed this turn.
	workHead      int
	working       bool
	noticeDepth   int
	deferredDrain map[uint64]*fiber
	batchDepth    int // Data commit/invalidation may not admit new callbacks.
	pendingSvc    []*heldService
	heldSvc       map[svcKey]int // Held service events per key (pendingSvc).
	// Load workers held back by a held or being-produced service event, indexed
	// by every key they need so a release touches only the loads waiting on it.
	loadsByKey map[svcKey]map[*pendingLoad]struct{}
	loadSeq    uint64
	notifying  map[svcKey]int // Keys whose service event is being produced by notifyService.
}

// core is only transport. All graph fields belong to owner.run. The bounded
// inbox provides external backpressure; intrusive coalesced notices let
// dispose requests and lease releases wake the owner without blocking callers.
type core struct {
	cfg       coreConfig
	lateClose sync.Once // joins a late owner exit after close returned a diagnostic
	inbox     chan message
	wake      chan struct{}
	done      chan struct{}
	notices   atomic.Pointer[notice]
	cancelled atomic.Pointer[waiter] // Parked waiters whose context ended.
	rootErr   error                  // Published before done closes; retained host-lifetime failure.
	observers *observerSet
	// History bounds (inspect.go). The owner is the only writer; status readers
	// use the atomic counters inside these values.
	ledger  *eventLedger
	archive *recordArchive[retired]
	// terminalDropped counts failed-incarnation diagnostics beyond cfg.terminalLimit.
	terminalDropped atomic.Uint64
}

// newOwner builds the owner state and installs the history bounds on r.
func newOwner(r *core) *owner {
	r.ledger, r.archive = newEventLedger(r.cfg.eventWindow), newRecordArchive[retired](r.cfg.archiveLimit)
	r.observers = newObserverSet(r.cfg.observerQueue, r.cfg.observerLog)
	return &owner{r: r, fibers: make(map[uint64]*fiber), providers: make(map[svcKey]*liveProvision), byKey: make(map[svcKey]map[uint64]*fiber),
		failures: make(map[uint64]error), blocked: make(map[uint64]error), awaiting: make(map[uint64][]*waiter),
		heldSvc: make(map[svcKey]int), loadsByKey: make(map[svcKey]map[*pendingLoad]struct{})}
}

// rootHandle returns the one native host activation, not a mounted plugin.
// Only trusted host construction needs this anchor.
func (r *core) rootHandle(ctx context.Context) (*handle, error) {
	a := r.exchange(ctx, message{kind: cmdRoot})
	return a.root, a.err
}

// mount creates one top-level fiber and returns its id.
func (r *core) mount(ctx context.Context, spec pluginSpec) (uint64, error) {
	spec.Needs = slices.Clone(spec.Needs)
	spec.Provides = slices.Clone(spec.Provides)
	a := r.exchange(ctx, message{kind: cmdMount, spec: spec})
	return a.id, a.err
}

// batch commits one validated desired-state prefix before any resulting work is
// admitted. Results have the length and order of operations; only mount
// positions carry new ids. It either returns the ids of a committed prefix or has
// no effect: cancellation before the owner claims the command changes nothing.
func (r *core) batch(ctx context.Context, operations []batchOp) ([]uint64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// An acknowledgment can be lost to caller cancellation. Never leave owner
	// validation reading declarations that caller may mutate after batch returns.
	operations = slices.Clone(operations)
	for i := range operations {
		operations[i].Spec.Needs = slices.Clone(operations[i].Spec.Needs)
		operations[i].Spec.Provides = slices.Clone(operations[i].Spec.Provides)
	}
	a := r.exchange(ctx, message{kind: cmdBatch, batch: operations})
	return a.ids, a.err
}

// notifyKeys asks the dependants of keys to re-run their availability checks and
// records one service event per key.
func (r *core) notifyKeys(ctx context.Context, keys ...svcKey) error {
	return r.exchange(ctx, message{kind: cmdNotify, keys: slices.Clone(keys)}).err
}

// settle is the Settle operation of the core. Its contract, verbatim:
//
// Settle returns when no fiber is mid-transition except fibers blocked past one of their own deadlines. It returns nil when nothing is blocked, otherwise one joined diagnostic per blocked fiber naming the fiber and the stage. It never returns while an unblocked fiber is mid-transition. If ctx ends first it returns ctx's error joined with the diagnostics known so far.
//
// A blocked fiber whose disposer ran past the dispose deadline is reported as
// leaked: its diagnostic wraps errLeaked and its snapshot has Leaked set.
func (r *core) settle(ctx context.Context) error {
	return r.exchange(ctx, message{kind: cmdSettle}).err
}

// snapshots lists live fibers and the bounded archive of recently disposed ones.
func (r *core) snapshots(ctx context.Context) ([]fiberSnap, error) {
	a := r.exchange(ctx, message{kind: cmdSnapshot})
	return a.snapshots, a.err
}

// inspect observes one owner turn without waiting for lifecycle work or running
// availability predicates. Provision records include reserved and retiring ones.
func (r *core) inspect(ctx context.Context) (graphView, error) {
	a := r.exchange(ctx, message{kind: cmdInspect})
	return a.view, a.err
}

// events returns the retained tail of the event ledger, with errEventsTruncated
// once older events were dropped.
func (r *core) events(ctx context.Context) ([]ledgerEvent, error) {
	a := r.exchange(ctx, message{kind: cmdEvents})
	return a.events, a.err
}

// close is the Close operation of the core. It disposes every fiber, then the
// root. Its contract, verbatim:
//
// Close follows the same rule as Settle for its wait, and always releases what it can (the observer dispatcher included) once the runtime has drained, even when its own ctx ended first.
func (r *core) close(ctx context.Context) error {
	select {
	case <-r.done:
		return errors.Join(r.rootErr, r.observers.close(ctx))
	default:
	}
	err := r.exchange(ctx, message{kind: cmdClose}).err
	if errors.Is(err, errClosed) {
		err = nil
	}
	// A bounded lifecycle diagnostic can precede actual worker completion. Do
	// not close the queue until the owner exits: later state events still belong
	// to this same native ledger and must not be dropped by an early close.
	select {
	case <-r.done:
		return errors.Join(err, r.rootErr, r.observers.close(ctx))
	default:
	}
	if err != nil {
		// The diagnostic is returned while the owner may still be alive. The
		// owner replies before it closes done, so join that exit off this call
		// path; otherwise nobody closes the observer queue.
		r.releaseWhenDrained()
		return err
	}
	select {
	case <-r.done:
		return errors.Join(r.rootErr, r.observers.close(ctx))
	case <-ctx.Done():
		r.releaseWhenDrained()
		return ctx.Err()
	}
}

// releaseWhenDrained closes the observer dispatcher once the owner has exited,
// for a close that returned (diagnostic or context error) before that. A close
// always releases what it can once the core has drained, whoever waits.
func (r *core) releaseWhenDrained() {
	r.lateClose.Do(func() {
		go func() {
			<-r.done
			_ = r.observers.close(context.Background())
		}()
	})
}

// Admission states of one command, the only word shared by caller and owner.
const (
	admQueued    uint32 = iota // Sent; neither side has decided.
	admClaimed                 // The owner took it: it will apply it and reply.
	admAbandoned               // The caller gave up first: the owner must not apply it.
)

// isWait reports the commands whose reply is a wait for lifecycle progress.
// They admit no mutation, may be answered long after the owner dequeued them,
// and so stay cancellable at any time.
func isWait(kind command) bool {
	return kind == cmdSettle || kind == cmdClose || kind == cmdAwait
}

// exchange sends one command and waits for its reply.
//
// Admission is atomic between caller and owner. Every command that is not a wait
// carries a claim word that moves out of admQueued exactly once: the owner CASes
// it to admClaimed immediately before it applies the command, and a caller that
// gives up (context done, runtime closed) CASes it to admAbandoned. Whoever wins
// decides the outcome for both: if the caller wins the owner drops the message
// untouched, so the caller's error means "no effect"; if the owner wins the
// caller no longer abandons but takes the reply, so "applied" always comes with
// the real result and never with a context error. The wait after a claim is
// bounded because the owner runs no callbacks and replies in the same turn,
// before it can close done. A command is therefore cancellable exactly while it
// is still queued, and there is no instant at which both "the caller got an
// error" and "the command was applied" hold.
func (r *core) exchange(ctx context.Context, msg message) answer {
	if err := ctx.Err(); err != nil {
		return answer{err: err}
	}
	msg.ctx, msg.ack = ctx, make(chan answer, 1)
	if !isWait(msg.kind) {
		msg.claim = new(atomic.Uint32)
	}
	select {
	case r.inbox <- msg:
	case <-ctx.Done():
		return answer{err: ctx.Err()}
	case <-r.done:
		return answer{err: errClosed}
	}
	select {
	case reply := <-msg.ack:
		return reply
	case <-ctx.Done():
		return r.abandon(msg, ctx.Err())
	case <-r.done:
		return r.abandon(msg, errClosed)
	}
}

// abandon ends the caller's wait with cause, unless the owner already claimed
// the command, in which case its reply is the outcome.
func (r *core) abandon(msg message, cause error) answer {
	if msg.claim != nil {
		if msg.claim.CompareAndSwap(admQueued, admAbandoned) {
			return answer{err: cause}
		}
		return <-msg.ack // Claimed: the owner replies in the turn it claimed it.
	}
	if msg.kind == cmdSettle || msg.kind == cmdClose {
		// The owner answers a finished context with its error joined with the
		// diagnostics known so far. It never blocks, so this wait is bounded.
		select {
		case reply := <-msg.ack:
			return reply
		case <-r.done:
		}
	}
	select {
	case reply := <-msg.ack:
		return reply
	default:
		return answer{err: cause}
	}
}

func (r *core) poke() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *core) notify(n *notice) {
	if !n.queued.CompareAndSwap(false, true) {
		return
	}
	for {
		head := r.notices.Load()
		n.next = head
		if r.notices.CompareAndSwap(head, n) {
			break
		}
	}
	r.poke()
}

func (r *core) completed(msg message) {
	select {
	case r.inbox <- msg:
	case <-r.done:
	}
}
func (o *owner) run() {
	timer := time.NewTimer(time.Hour)
	timer.Stop() // A fresh hour timer has not fired; since Go 1.23 Stop leaves no stale tick.
	defer timer.Stop()
	for {
		o.consumeNotices()
		o.consumeCancelled()
		o.expire(time.Now())
		o.work()
		o.flushServices()
		o.finishWaiters()
		if o.closing && len(o.fibers) == 0 && (o.root == nil || o.root.act == nil) {
			o.replyAll(&o.closers, errors.Join(o.terminalErrors()...))
			close(o.r.done)
			return
		}
		var tick <-chan time.Time
		if next := o.nextDeadline(); !next.IsZero() {
			timer.Reset(max(time.Until(next), 0))
			tick = timer.C
		}
		select {
		case msg := <-o.r.inbox:
			if tick != nil {
				timer.Stop()
			}
			if msg.claim != nil && !msg.claim.CompareAndSwap(admQueued, admClaimed) {
				break // The caller abandoned it while queued: no effect, nobody waits.
			}
			if !msg.at.IsZero() {
				// A completion and its timer can both be ready, and select picks at
				// random: let the verdict on the deadline come before the message.
				o.expire(time.Now())
			}
			reply, deferred := o.handle(msg)
			o.consumeNotices()
			o.work()
			o.flushServices()
			if !deferred && msg.ack != nil {
				msg.ack <- reply
			}
		case <-o.r.wake:
			if tick != nil {
				timer.Stop()
			}
		case <-tick:
		}
	}
}

func (o *owner) handle(m message) (answer, bool) {
	// Settle and Close answer a finished context themselves, with the diagnostics
	// known so far, when their waiter is pruned.
	if m.ctx != nil && m.ctx.Err() != nil && m.kind != cmdSettle && m.kind != cmdClose {
		return answer{err: m.ctx.Err()}, false
	}
	if o.closing && (m.kind == cmdMount || m.kind == cmdMountChild || m.kind == cmdUpdate || m.kind == cmdRoot || m.kind == cmdBatch) {
		return answer{err: errClosed}, false
	}
	switch m.kind {
	case cmdRoot:
		if o.root == nil {
			o.startRoot()
		}
		if o.root.act == nil || !alive(o.root) || o.root.invalid {
			return answer{err: ErrInactive}, false
		}
		return answer{root: o.root.act.ctx}, false
	case cmdMount:
		f, err := o.mount(m.spec, nil)
		if err != nil {
			return answer{err: err}, false
		}
		return answer{id: f.id}, false
	case cmdMountChild:
		parent, err := o.current(m.act)
		if err != nil || m.act.selfDispose.Load() {
			return answer{err: ErrInactive}, false
		}
		f, err := o.mount(m.spec, parent.act)
		if err != nil {
			return answer{err: err}, false
		}
		return answer{id: f.id}, false
	case cmdBatch:
		ids, err := o.batch(m.ctx, m.batch)
		return answer{ids: ids, err: err}, false
	case cmdUpdate:
		f := o.fibers[m.id]
		if f == nil {
			return answer{err: errUnknown}, false
		}
		if f.remove {
			return answer{err: ErrInactive}, false
		}
		o.update(f)
	case cmdSelfDispose:
		f, err := o.current(m.act)
		if err != nil {
			return answer{err: err}, false
		}
		m.act.selfDispose.Store(true)
		o.requestDispose(f)
	case cmdDispose:
		if m.id == 0 {
			return answer{err: errUnknown}, false
		}
		if f := o.fibers[m.id]; f != nil {
			o.requestDispose(f)
		}
	case cmdAwait:
		if record, retired := o.r.archive.get(m.id); retired {
			return answer{err: record.awaitErr}, false
		}
		f := o.fibers[m.id]
		if f == nil {
			if o.issued(m.id) {
				return answer{err: errExpired}, false
			}
			return answer{err: errUnknown}, false
		}
		w := o.waiter(m)
		w.fiber = f
		f.awaiters++
		o.touch(f)
		o.awaiting[m.id] = append(o.awaiting[m.id], w)
		return answer{}, true
	case cmdSettle:
		o.settlers = append(o.settlers, o.waiter(m))
		return answer{}, true
	case cmdClose:
		o.closers = append(o.closers, o.waiter(m))
		o.beginClose()
		return answer{}, true
	case cmdSnapshot:
		return answer{snapshots: o.snapshots()}, false
	case cmdFiber:
		if record, ok := o.r.archive.get(m.id); ok {
			return answer{snapshots: []fiberSnap{record.snapshot}}, false
		}
		if f := o.fibers[m.id]; f != nil {
			return answer{snapshots: []fiberSnap{o.snapshot(f)}}, false
		}
		if o.issued(m.id) {
			return answer{err: errExpired}, false
		}
		return answer{err: errUnknown}, false
	case cmdInspect:
		return answer{view: o.inspect()}, false
	case cmdEvents:
		events, err := o.r.ledger.events()
		return answer{events: events, err: err}, false
	case cmdProvide:
		f, err := o.current(m.act)
		if err != nil {
			return answer{err: err}, false
		}
		if m.key == nil || (f.id != 0 && !slices.Contains(f.spec.Provides, m.key)) {
			return answer{err: fmt.Errorf("undeclared provision %v", m.key)}, false
		}
		if o.providers[m.key] != nil {
			return answer{err: fmt.Errorf("duplicate provision %v", m.key)}, false
		}
		o.nextUID++
		p := &liveProvision{key: m.key, binding: provision{uid: o.nextUID, owner: f.id, value: m.value}, dependents: make(map[*handle]*fiber)}
		o.providers[m.key] = p
		f.act.ctx.publishOwned(m.key)
		f.provisions = append(f.provisions, p)
		f.effects = append(f.effects, effectRec{label: "provide:" + m.label, provision: p})
		o.keyEvent(f, "provide", m.key, p.binding.uid)
		if f.state == Active {
			o.notifyService([]svcKey{m.key}, []ledgerEvent{{Fiber: f.id, Name: f.spec.ID, Kind: "service", State: f.state, Key: m.key, UID: p.binding.uid}})
		}
	case cmdEffect:
		f, err := o.current(m.act)
		if err != nil {
			return answer{err: err}, false
		}
		if m.dispose == nil {
			return answer{err: errors.New("nil disposer")}, false
		}
		f.effects = append(f.effects, effectRec{label: m.label, dispose: m.dispose})
		o.event(f, "effect", m.label, 0)
	case cmdScopeRequest:
		f, err := o.current(m.act)
		if err != nil {
			return answer{err: err}, false
		}
		if err := validateScope(m.label, m.setup); err != nil {
			return answer{err: err}, false
		}
		e := newScopeEffect(m.label, m.setup)
		e.requestWithdraw = func() error {
			if e.cleanupFinished() {
				return nil
			}
			reply := o.r.exchange(context.Background(), message{kind: cmdScopeWithdraw, act: m.act, scope: e})
			if reply.err != nil && e.cleanupFinished() {
				return nil
			}
			return reply.err
		}
		f.effects = append(f.effects, effectRec{label: m.label, scope: e})
		o.event(f, "scope:request", m.label, 0)
		o.enqueue(f)
		return answer{handle: e.handle}, false
	case cmdScopeWithdraw:
		f := o.lookup(m.act.ctx.fiber)
		if f == nil || f.act != m.act || !slices.ContainsFunc(f.effects, func(e effectRec) bool { return e.scope == m.scope }) {
			// A scope leaves its fiber's effects only after its cleanup finished.
			return answer{}, false
		}
		m.scope.markWithdraw()
		o.enqueue(f)
	case cmdScopeDone:
		o.scopeDone(m)
	case cmdTrackedStart:
		f, err := o.current(m.act)
		if err != nil || m.act.selfDispose.Load() {
			return answer{err: ErrInactive}, false
		}
		if m.label == "" {
			return answer{err: errors.New("empty tracked work label")}, false
		}
		if m.act.tracked == nil {
			m.act.tracked = make(map[uint64]trackedWork)
		}
		m.act.nextTracked++
		run := new(handshake)
		m.act.tracked[m.act.nextTracked] = trackedWork{label: m.label, run: run}
		o.event(f, "go:start", m.label, 0)
		return answer{id: m.act.nextTracked, run: run}, false
	case cmdTrackedDone:
		o.trackedDone(m)
	case cmdNotify:
		var events []ledgerEvent
		for _, key := range m.keys {
			for _, f := range o.byKey[key] {
				f.checkVersion++
				f.deniedEpoch = nil
			}
			// The oracle's notify refreshes dependants, then emits one service
			// event per key carrying the current provision, whatever its state.
			if p := o.providers[key]; p != nil {
				if owner := o.lookup(p.binding.owner); owner != nil {
					events = append(events, ledgerEvent{Fiber: owner.id, Name: owner.spec.ID, Kind: "service", State: owner.state, Key: key, UID: p.binding.uid})
				}
			}
		}
		o.notifyService(m.keys, events)
	case cmdCheckDone:
		o.checkedResult(m)
	case cmdLoadDone:
		o.loaded(m)
	case cmdEffectDone:
		o.effectDone(m)
	}
	return answer{}, false
}

func (o *owner) update(f *fiber) {
	f.revision++
	f.awaitErr, f.awaitText = nil, ""
	f.checkVersion++
	f.deniedEpoch = nil
	o.fail(f, nil)
	// Restart refreshes the desired revision using already checked bindings.
	if epoch, _ := o.candidate(f); epoch != nil && f.checkAllowed && slices.Equal(epoch[1:], uids(f.checkedEpoch)) {
		f.checkedEpoch, f.checked = epoch, f.checkVersion
	}
	o.enqueue(f)
}

func (o *owner) batch(ctx context.Context, operations []batchOp) ([]uint64, error) {
	prepared, err := prepareBatch(operations, func(id uint64) bool {
		f := o.fibers[id]
		return f != nil && alive(f)
	})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	results := make([]uint64, len(prepared))
	touched := make([]*fiber, 0, len(prepared))
	o.batchDepth++
	for i, op := range prepared {
		switch op.Kind {
		case batchMount:
			// prepareBatch has validated the same mount declarations. IDs are
			// consumed only now, after the entire prefix has been admitted.
			f, _ := o.mount(op.Spec, nil)
			results[i] = f.id
		case batchUpdate:
			f := o.fibers[op.Fiber]
			o.update(f)
			touched = append(touched, f)
		case batchDispose:
			f := o.fibers[op.Fiber]
			o.requestDispose(f)
			touched = append(touched, f)
		}
	}
	// A cold mount can precede a provider update in input order. Invalidate the
	// existing provider and its complete consumer closure before any queued
	// mount is allowed to run Check or Apply against the old ACTIVE record.
	o.invalidateBatch(touched)
	o.batchDepth--
	return results, nil
}

func (o *owner) invalidateBatch(queue []*fiber) {
	seen := make(map[uint64]bool)
	for i := 0; i < len(queue); i++ {
		f := queue[i]
		if f == nil || seen[f.id] {
			continue
		}
		seen[f.id] = true
		if f.checking != nil {
			f.checking.cancel()
		}
		o.quiesce(f)
		o.enqueue(f)
		for _, provision := range f.provisions {
			for _, consumer := range o.byKey[provision.key] {
				consumer.target = nil
				queue = append(queue, consumer)
			}
		}
	}
}

func (o *owner) lookup(id uint64) *fiber {
	if id == 0 {
		return o.root
	}
	return o.fibers[id]
}

func (o *owner) startRoot() {
	f := &fiber{spec: pluginSpec{ID: "root"}, state: Active, revision: 1, target: []uint64{1}, deadlineIndex: -1}
	o.root = f
	f.act = o.activation(f, slices.Clone(f.target), nil)
}

func (o *owner) beginClose() {
	o.closing = true
	// All user fibers, including non-structural consumers, leave before host
	// resources are released. Root quiescence also invalidates its own children.
	for _, id := range o.ids() {
		o.requestDispose(o.fibers[id])
	}
	if o.root != nil && o.root.act != nil {
		o.requestDispose(o.root)
		o.quiesce(o.root)
	}
}

func (o *owner) mount(spec pluginSpec, parent *activation) (*fiber, error) {
	if err := validateSpec(spec); err != nil {
		return nil, err
	}
	o.nextFiber++
	f := &fiber{id: o.nextFiber, spec: spec, state: Pending, deadlineIndex: -1, parent: parent}
	o.fibers[f.id] = f
	if parent != nil {
		if parent.children == nil {
			parent.children = make(map[uint64]struct{})
		}
		parent.children[f.id] = struct{}{}
		// The child is one ordered effect of its parent, as in the oracle, so
		// its teardown starts at its LIFO slot (see disposeNext).
		if owner := o.lookup(parent.ctx.fiber); owner != nil && owner.act == parent {
			owner.effects = append(owner.effects, effectRec{label: "child", child: f.id})
		}
	}
	for _, key := range f.spec.Needs {
		if o.byKey[key] == nil {
			o.byKey[key] = make(map[uint64]*fiber)
		}
		o.byKey[key][f.id] = f
	}
	o.event(f, "mount", "", 0)
	o.enqueue(f)
	return f, nil
}

// validateSpec checks the declarations of a pluginSpec. Keys must be comparable.
func validateSpec(spec pluginSpec) error {
	if spec.Apply != nil && spec.ApplyEffect != nil {
		return errors.New("Apply and ApplyEffect are mutually exclusive")
	}
	for _, keys := range [][]svcKey{spec.Needs, spec.Provides} {
		seen := make(map[svcKey]bool, len(keys))
		for _, key := range keys {
			if key == nil || !reflect.TypeOf(key).Comparable() {
				return fmt.Errorf("nil or incomparable declaration %v", key)
			}
			if seen[key] {
				return fmt.Errorf("duplicate declaration %v", key)
			}
			seen[key] = true
		}
	}
	return nil
}

func (o *owner) current(a *activation) (*fiber, error) {
	f := o.lookup(a.ctx.fiber) // a is never nil: every sender is a closure of activation.
	if f == nil || f.act != a || f.invalid || f.remove || !a.ctx.live.Load() {
		return nil, ErrInactive
	}
	return f, nil
}

func (o *owner) requestDispose(f *fiber) {
	if f.remove {
		return
	}
	o.event(f, "dispose:request", "", 0)
	f.remove = true
	f.target = nil
	if f.id != 0 {
		o.fail(f, nil)
	}
	o.enqueue(f)
}

func (o *owner) enqueue(f *fiber) {
	if f.queued {
		return
	}
	f.queued = true
	o.queue = append(o.queue, f.id)
}

func (o *owner) notifyKey(key svcKey) {
	ids := make([]uint64, 0, len(o.byKey[key]))
	for id := range o.byKey[key] {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		o.enqueue(o.byKey[key][id])
	}
}

func (o *owner) work() {
	if o.batchDepth != 0 {
		return
	}
	outer := !o.working
	o.working = true
	for o.workHead < len(o.queue) {
		id := o.queue[o.workHead]
		o.workHead++
		f := o.lookup(id) // Never nil: only work removes a fiber, and only a fiber whose own turn this is.
		f.queued = false
		if f.id == 0 {
			if f.phase == phIdle && !f.invalid && !f.remove {
				o.scopeWork(f)
			}
			if f.phase == phDraining {
				if o.noticeDepth != 0 {
					if o.deferredDrain == nil {
						o.deferredDrain = make(map[uint64]*fiber)
					}
					o.deferredDrain[0] = f
				} else {
					o.drain(f)
				}
			} else if f.phase == phDisposing && f.joining {
				o.disposeNext(f)
			}
			continue
		}
		epoch, bindings := o.candidate(f)
		o.cancelCheckIfStale(f, epoch)
		available := epoch != nil && o.availability(f, epoch, bindings)
		o.refreshTarget(f, epoch)
		if f.act != nil {
			if f.act.selfDispose.Load() {
				o.requestDispose(f)
			}
			if f.remove || f.failure != nil || f.target == nil || !slices.Equal(f.target, f.act.epoch) {
				o.quiesce(f)
			}
			if f.phase == phDraining {
				if o.noticeDepth != 0 {
					if o.deferredDrain == nil {
						o.deferredDrain = make(map[uint64]*fiber)
					}
					o.deferredDrain[f.id] = f
				} else {
					o.drain(f)
				}
			} else if f.phase == phDisposing && f.joining {
				o.disposeNext(f)
			} else if f.phase == phIdle && !f.invalid && !f.remove {
				if !o.scopeWork(f) && f.publishActive {
					f.publishActive = false
					o.state(f, Active)
				}
			}
			continue
		}
		if f.remove {
			if f.checking != nil {
				continue
			}
			// A PENDING fiber has an inactive runner epoch but still undergoes
			// source disposal inertia before its final DISPOSED state.
			o.state(f, Unloading)
			o.remove(f)
			o.releaseDependencies(f)
			continue
		}
		if f.failure != nil {
			o.state(f, Failed)
			o.releaseDependencies(f)
			continue
		}
		if epoch == nil {
			o.state(f, Pending)
			o.releaseDependencies(f)
			continue
		}
		if !available || f.target == nil {
			o.state(f, Pending)
			o.releaseDependencies(f)
			continue
		}
		o.load(f, epoch, bindings)
		o.releaseDependencies(f)
	}
	if outer {
		clear(o.queue)
		o.queue = o.queue[:0]
		o.workHead, o.working = 0, false
	}
}

// candidate resolves strict-active provider identities without executing Check.
// The runner target is cached separately because a pending availability worker
// must not replace the last checked target, and inspection must be read-only.
func (o *owner) candidate(f *fiber) ([]uint64, map[svcKey]*liveProvision) {
	if f.remove || f.failure != nil {
		return nil, nil
	}
	epoch := make([]uint64, 1, len(f.spec.Needs)+1)
	epoch[0] = f.revision
	bindings := make(map[svcKey]*liveProvision, len(f.spec.Needs))
	for _, key := range f.spec.Needs {
		p := o.providers[key]
		if p == nil {
			return nil, nil
		}
		provider := o.lookup(p.binding.owner)
		if !providerActive(provider) {
			return nil, nil
		}
		epoch = append(epoch, p.binding.uid)
		bindings[key] = p
	}
	return epoch, bindings
}

func alive(f *fiber) bool {
	return !f.remove && (f.act == nil || !f.act.selfDispose.Load())
}

func providerActive(f *fiber) bool {
	return f != nil && f.state == Active && !f.invalid && alive(f)
}

// uids is epoch without its revision. epoch is never empty: it is a checked epoch,
// and every epoch starts with its revision.
func uids(epoch []uint64) []uint64 { return epoch[1:] }

func (o *owner) refreshTarget(f *fiber, epoch []uint64) {
	if epoch == nil {
		f.target = nil
		return
	}
	if f.spec.Check == nil {
		f.target = slices.Clone(epoch)
		return
	}
	if f.deniedEpoch != nil && slices.Equal(f.deniedEpoch, epoch) {
		f.target = nil
		return
	}
	if f.checked == f.checkVersion && slices.Equal(f.checkedEpoch, epoch) {
		// A denied result also set deniedEpoch, which was handled above.
		f.target = nil
		if f.checkAllowed {
			f.target = slices.Clone(epoch)
		}
		return
	}
	// Rechecks retain a previous result only for the identical desired epoch.
	// A new provider UID cannot become a target before its Check has succeeded.
	if !slices.Equal(f.target, epoch) {
		f.target = nil
	}
}

func (o *owner) cancelCheckIfStale(f *fiber, epoch []uint64) {
	if q := f.checking; q != nil && (epoch == nil || !slices.Equal(epoch, q.epoch) || q.generation != f.checkVersion) {
		q.cancel()
	}
}

func (o *owner) availability(f *fiber, epoch []uint64, bindings map[svcKey]*liveProvision) bool {
	if f.spec.Check == nil {
		return true
	}
	if f.deniedEpoch != nil && slices.Equal(f.deniedEpoch, epoch) {
		return false
	}
	if slices.Equal(f.checkedEpoch, epoch) && f.checked == f.checkVersion {
		return f.checkAllowed
	}
	if f.checking != nil {
		return false
	}
	std, cancel := context.WithCancel(context.Background())
	c := newHandle(o.r, std, f.id, make(map[svcKey]provision, len(bindings)))
	c.disposeFn = func() error { return ErrInactive }
	q := &availCheck{ctx: c, cancel: cancel, epoch: slices.Clone(epoch), bindings: bindings, generation: f.checkVersion, deadline: time.Now().Add(o.r.cfg.loadDeadline), run: new(handshake)}
	for key, p := range bindings {
		c.bindings[key] = p.binding
		p.dependents[c] = f
	}
	f.checking = q
	o.touch(f)
	o.busy++
	if f.phase == phIdle {
		o.deadline(f, q.deadline)
	}
	check := f.spec.Check
	go func() {
		var allowed bool
		err := invokeCallback(func() error { allowed = check(c); return nil })
		at := q.run.finish()
		o.r.completed(message{kind: cmdCheckDone, id: c.fiber, check: q, allowed: allowed, err: err, errText: errorText(err), at: at})
	}()
	return false
}

func (o *owner) checkedResult(m message) {
	m.check.done = true
	for _, held := range m.check.holds {
		held.pending--
	}
	m.check.holds = nil
	// f exists and still owns this check: work does not remove a fiber while its
	// check is in flight, and a fiber has one check at a time.
	f := o.fibers[m.id]
	q := m.check
	q.cancel()
	q.ctx.released.Store(true)
	for _, p := range q.bindings {
		delete(p.dependents, q.ctx)
		if owner := o.lookup(p.binding.owner); owner != nil {
			o.enqueue(owner)
		}
	}
	f.checking = nil
	o.touch(f)
	o.busy--
	if f.phase == phIdle {
		o.deadline(f, time.Time{})
		o.block(f, nil)
	}
	epoch, _ := o.candidate(f)
	if !f.remove && q.generation == f.checkVersion && slices.Equal(q.epoch, epoch) {
		if q.timedOut {
			m.allowed, m.err, m.errText = false, context.DeadlineExceeded, "context deadline exceeded"
		}
		f.checkedEpoch, f.checked = q.epoch, q.generation
		f.checkAllowed = m.allowed && m.err == nil
		f.checkError, f.checkText = m.err, m.errText
		if !f.checkAllowed {
			f.deniedEpoch = q.epoch
			if f.act != nil {
				o.quiesce(f)
			}
		}
	}
	o.enqueue(f)
}

// serviceHeld reports whether a held service event is for one of keys.
func (o *owner) serviceHeld(keys []svcKey) bool {
	for _, k := range keys {
		if o.notifying[k] != 0 || o.heldSvc[k] != 0 {
			return true
		}
	}
	return false
}

// issued reports whether id was ever issued to a user fiber. IDs are monotonic
// and never reused, so an issued id that is not live is disposed, whether or
// not its archive record is still held.
func (o *owner) issued(id uint64) bool { return id != 0 && id <= o.nextFiber }
func (o *owner) consumeNotices() {
	for n := o.r.notices.Swap(nil); n != nil; {
		next := n.next
		n.queued.Store(false)
		a := n.act
		if f := o.lookup(a.ctx.fiber); f != nil && f.act == a {
			if a.selfDispose.Load() {
				o.requestDispose(f)
			}
			o.enqueue(f)
		}
		n = next
	}
}

func (o *owner) state(f *fiber, state State) {
	old := f.state
	if old == state {
		return
	}
	f.state = state
	o.appendEvent(ledgerEvent{Fiber: f.id, Name: f.spec.ID, Kind: "state", From: old, State: state})
	if old == Active || state == Active {
		for _, p := range slices.Clone(f.provisions) {
			o.notifyService([]svcKey{p.key}, []ledgerEvent{{Fiber: f.id, Name: f.spec.ID, Kind: "service", State: state, Key: p.key,
				UID: p.binding.uid}})
		}
	}
}

// notifyService refreshes the dependants of key, then records the service
// event, matching the oracle order (consumers move before the notification).
// Source cleanup resumes only after the event, so recursive owner work cannot
// remove or reload the provider before the observed value is recorded. An
// availability check runs off the owner; when one was started by this refresh
// the event waits for its result (flushServices) instead of blocking the owner.
func (o *owner) notifyService(keys []svcKey, events []ledgerEvent) {
	inflight := make(map[*availCheck]bool)
	for _, key := range keys {
		for _, f := range o.byKey[key] {
			if f.checking != nil {
				inflight[f.checking] = true
			}
		}
	}
	o.noticeDepth += len(events)
	if o.notifying == nil {
		o.notifying = make(map[svcKey]int)
	}
	for _, key := range keys {
		o.notifying[key]++
		o.notifyKey(key)
	}
	o.work()
	for _, key := range keys {
		if o.notifying[key]--; o.notifying[key] == 0 {
			delete(o.notifying, key)
		}
	}
	var waits []*availCheck
	for _, key := range keys {
		for _, f := range o.byKey[key] {
			if q := f.checking; q != nil && !inflight[q] {
				waits = append(waits, q)
			}
		}
	}
	for _, e := range events {
		if len(waits) != 0 || len(o.pendingSvc) != 0 {
			held := &heldService{event: e}
			for _, q := range waits {
				if !q.done {
					q.holds = append(q.holds, held)
					held.pending++
				}
			}
			o.pendingSvc = append(o.pendingSvc, held)
			o.heldSvc[e.Key]++
			waits = nil // Later events follow the first in queue order.
			continue
		}
		o.emitService(e)
	}
	o.releaseLoads(keys)
}

func (o *owner) emitService(e ledgerEvent) {
	o.appendEvent(e)
	o.noticeDepth--
	if o.noticeDepth == 0 && len(o.deferredDrain) != 0 {
		ids := make([]uint64, 0, len(o.deferredDrain))
		for id := range o.deferredDrain {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			o.enqueue(o.deferredDrain[id])
		}
		clear(o.deferredDrain)
	}
}

// flushServices emits held service events in order once their checks finished,
// then releases the keys whose last held event went out. It is called after
// every owner message, so it must cost nothing when no event can be emitted.
func (o *owner) flushServices() {
	var released []svcKey
	for len(o.pendingSvc) != 0 && o.pendingSvc[0].pending == 0 {
		head := o.pendingSvc[0]
		o.pendingSvc[0] = nil
		o.pendingSvc = o.pendingSvc[1:]
		o.emitService(head.event)
		if o.heldSvc[head.event.Key]--; o.heldSvc[head.event.Key] == 0 {
			delete(o.heldSvc, head.event.Key)
		}
		released = append(released, head.event.Key)
	}
	if len(released) != 0 {
		o.releaseLoads(released)
	}
}

// holdLoad parks a load worker until none of its needed keys is held.
func (o *owner) holdLoad(needs []svcKey, start func()) {
	o.loadSeq++
	l := &pendingLoad{seq: o.loadSeq, needs: needs, start: start}
	for _, k := range needs {
		if o.loadsByKey[k] == nil {
			o.loadsByKey[k] = make(map[*pendingLoad]struct{})
		}
		o.loadsByKey[k][l] = struct{}{}
	}
}

// releaseLoads launches, in insertion order, the load workers waiting on one of
// keys whose needed keys are all free now. A load becomes startable only when a
// key it needs stops being held, so only those keys are examined.
func (o *owner) releaseLoads(keys []svcKey) {
	var ready []*pendingLoad
	var seen map[*pendingLoad]struct{} // Only needed when a load is indexed under several released keys.
	if len(keys) > 1 {
		seen = make(map[*pendingLoad]struct{})
	}
	for _, k := range keys {
		for l := range o.loadsByKey[k] {
			if o.serviceHeld(l.needs) {
				continue
			}
			if seen != nil {
				if _, dup := seen[l]; dup {
					continue
				}
				seen[l] = struct{}{}
			}
			ready = append(ready, l)
		}
	}
	if len(ready) == 0 {
		return
	}
	slices.SortFunc(ready, func(a, b *pendingLoad) int { return cmp.Compare(a.seq, b.seq) })
	for _, l := range ready {
		for _, k := range l.needs {
			delete(o.loadsByKey[k], l)
			if len(o.loadsByKey[k]) == 0 {
				delete(o.loadsByKey, k)
			}
		}
	}
	for _, l := range ready {
		l.start()
	}
}

// event appends a ledger event whose Key is a label (or empty).
func (o *owner) event(f *fiber, kind, label string, uid uint64) {
	var key svcKey
	if label != "" {
		key = label
	}
	o.keyEvent(f, kind, key, uid)
}

// keyEvent appends a ledger event for a service key.
func (o *owner) keyEvent(f *fiber, kind string, key svcKey, uid uint64) {
	o.appendEvent(ledgerEvent{Fiber: f.id, Name: f.spec.ID, Kind: kind, State: f.state, Key: key, UID: uid})
}

// appendEvent assigns the next sequence number; callers never choose it.
func (o *owner) appendEvent(event ledgerEvent) {
	event.Sequence = o.r.ledger.next()
	o.r.ledger.append(event)
	o.r.observers.append(event)
}

func (o *owner) ids() []uint64 {
	ids := make([]uint64, 0, len(o.fibers))
	for id := range o.fibers {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// batchKind is a desired-state operation, not a plugin callback.
type batchKind uint8

const (
	batchMount batchKind = iota + 1
	batchUpdate
	batchDispose
)

// batchOp is data. A mount uses Spec; the other operations use an existing nonzero
// Fiber. Mount IDs are assigned only at actual commit.
//
// A batch commits one validated desired-state prefix before admitting resulting
// lifecycle and check work. Two updates increment the revision twice but cannot
// start an intermediate activation. Repeated dispose is allowed; an update after
// dispose is rejected before the entire prefix commits. No callback runs.
type batchOp struct {
	Kind  batchKind
	Fiber uint64
	Spec  pluginSpec
}

// prepareBatch copies declarations and checks every operation before any writer
// mutates state. live is native state data read by the owner; it must not invoke
// plugin code. False means absent or already disposing.
func prepareBatch(operations []batchOp, live func(uint64) bool) ([]batchOp, error) {
	prepared := slices.Clone(operations)
	disposed := make(map[uint64]bool)
	for i := range prepared {
		op := &prepared[i]
		switch op.Kind {
		case batchMount:
			if op.Fiber != 0 || op.Spec.ID == "" || validateSpec(op.Spec) != nil {
				return nil, fmt.Errorf("invalid batch Mount at operation %d", i)
			}
			op.Spec.Needs, op.Spec.Provides = slices.Clone(op.Spec.Needs), slices.Clone(op.Spec.Provides)
		case batchUpdate, batchDispose:
			if op.Fiber == 0 || !live(op.Fiber) {
				return nil, fmt.Errorf("batch operation %d: %w", i, errUnknown)
			}
			if op.Spec.ID != "" || op.Spec.Apply != nil || op.Spec.ApplyEffect != nil || op.Spec.Check != nil ||
				len(op.Spec.Needs) != 0 || len(op.Spec.Provides) != 0 {
				return nil, fmt.Errorf("batch operation %d carries an unused Mount specification", i)
			}
			if disposed[op.Fiber] && op.Kind == batchUpdate {
				return nil, fmt.Errorf("batch operation %d updates a disposed target", i)
			}
			if op.Kind == batchDispose {
				disposed[op.Fiber] = true
			}
		default:
			return nil, errors.New("unknown batch operation kind")
		}
	}
	return prepared, nil
}

// trackedWork is one tracked-work record, kept until its actual completion.
type trackedWork struct {
	label string
	run   *handshake
}
