// SPDX-License-Identifier: Apache-2.0
// Lifecycle semantics informed by Cordis, copyright Shigma and DeepSeek.
// This experimental scheduler is an independent Go implementation, not the kernel.

package concurrency

import (
	"bytes"
	"container/heap"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

const (
	rcLoadBudget    = 2 * time.Second
	rcDrainBudget   = 5 * time.Second
	rcDisposeBudget = 2 * time.Second
)

type rcCommand uint8

const (
	rcMountCommand rcCommand = iota
	rcMountChildCommand
	rcUpdateCommand
	rcDisposeCommand
	rcSettleCommand
	rcSnapshotCommand
	rcInspectCommand
	rcEventsCommand
	rcCloseCommand
	rcProvideCommand
	rcEffectCommand
	rcTrackedStartCommand
	rcTrackedDoneCommand
	rcLoadComplete
	rcEffectComplete
	rcNotifyCommand
	rcCheckComplete
	rcAwaitCommand
	rcRecordCommand
	rcConfigBindingCommand
	rcConfigClearCommand
	rcConfigCurrentCommand
	rcRootCommand
	rcBatchCommand
	rcSelfDisposeCommand
)

type rcMessage struct {
	kind    rcCommand
	ctx     context.Context
	ack     chan rcReply
	id      uint64
	spec    Spec
	act     *rcActivation
	key     string
	value   any
	dispose Disposer
	err     error
	errText string // Rendered by the callback worker, never by the graph owner.
	keys    []string
	check   *rcCheck
	allowed bool
	data    map[string]any
	binding *rcConfigBinding
	stale   bool
	batch   []BatchOperation
}

type rcReply struct {
	id         uint64
	snapshots  []Snapshot
	inspection Inspection
	events     []Event
	err        error
	binding    *rcConfigBinding
	current    bool
	root       *Context
	ids        []uint64
}

type rcConfigBinding struct {
	cell   *Configuration
	act    *rcActivation
	target []uint64
	loaded bool
}

// reconcilerRuntime is only transport. All graph fields belong to rcOwner.run.
// The bounded inbox provides external backpressure; intrusive coalesced notices
// let Dispose requests and lease releases wake the owner without blocking callers.
type reconcilerRuntime struct {
	inbox     chan rcMessage
	wake      chan struct{}
	done      chan struct{}
	notices   atomic.Pointer[rcNotice]
	rootErr   error // Published before done closes; retained host-lifetime failure.
	observers *nativeObservers
}

type rcNotice struct {
	next   *rcNotice
	queued atomic.Bool
	act    *rcActivation
}

type rcActivation struct {
	ctx         *Context
	cancel      context.CancelFunc
	epoch       []uint64
	selfDispose atomic.Bool
	notice      rcNotice
	children    map[uint64]struct{} // Owner-only lifecycle children of this activation.
	tracked     map[uint64]string   // Owner-only records until actual worker completion.
	nextTracked uint64
}

type rcCheck struct {
	ctx        *Context
	cancel     context.CancelFunc
	epoch      []uint64
	bindings   map[string]*rcProvision
	generation uint64
	deadline   time.Time
	timedOut   bool
}

type rcPhase uint8

const (
	rcIdle rcPhase = iota
	rcApplying
	rcDraining
	rcDisposing
)

type rcProvision struct {
	key        string
	binding    provision
	dependents map[*Context]*rcFiber
}

type rcEffect struct {
	label     string
	dispose   Disposer
	provision *rcProvision
}

// None of these fields are written by Apply, disposers, or call-path users.
type rcFiber struct {
	id              uint64
	spec            Spec
	state           State
	revision        uint64
	act             *rcActivation
	parent          *rcActivation // Exact owning activation, never a replacement.
	committed       map[string]*rcProvision
	releasing       *Context // Old dependency links survive until the next stable/LOADING state.
	releaseBindings map[string]*rcProvision
	provisions      []*rcProvision
	effects         []rcEffect
	phase           rcPhase
	invalid         bool
	remove          bool
	queued          bool
	failure         error
	awaitErr        error // Activation/startup failure retained by Await, not cleanup diagnostics.
	failureText     string
	awaitText       string
	blockedText     string
	checkText       string
	blocked         error
	deadline        time.Time
	deadlineIndex   int
	cleanupCtx      context.Context
	cleanupCancel   context.CancelFunc
	effectRunning   bool
	checking        *rcCheck
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

// The owner keeps one heap entry per pending deadline, never stale entries.
// Idle fibers add neither timer work nor deadline storage.
type rcDeadlines []*rcFiber

func (d rcDeadlines) Len() int           { return len(d) }
func (d rcDeadlines) Less(i, j int) bool { return d[i].deadline.Before(d[j].deadline) }
func (d rcDeadlines) Swap(i, j int) {
	d[i], d[j] = d[j], d[i]
	d[i].deadlineIndex, d[j].deadlineIndex = i, j
}
func (d *rcDeadlines) Push(value any) {
	f := value.(*rcFiber)
	f.deadlineIndex = len(*d)
	*d = append(*d, f)
}
func (d *rcDeadlines) Pop() any {
	n := len(*d) - 1
	f := (*d)[n]
	(*d)[n] = nil
	*d = (*d)[:n]
	f.deadlineIndex = -1
	return f
}

type rcWaiter struct {
	ctx  context.Context
	ack  chan rcReply
	stop func() bool
}

type rcOwner struct {
	r             *reconcilerRuntime
	fibers        map[uint64]*rcFiber
	root          *rcFiber // Trusted host anchor; never an ordinary plugin row.
	providers     map[string]*rcProvision
	byKey         map[string]map[uint64]*rcFiber
	queue         []uint64
	deadlines     rcDeadlines
	busy          int
	failures      map[uint64]error
	blocked       map[uint64]error
	nextFiber     uint64
	nextUID       uint64
	events        []Event
	settlers      []rcWaiter
	closers       []rcWaiter
	closing       bool
	terminal      []error
	retired       map[uint64]Snapshot
	retiredErr    map[uint64]error
	awaiting      map[uint64][]rcWaiter
	workHead      int
	working       bool
	noticeDepth   int
	deferredDrain map[uint64]*rcFiber
	batchDepth    int // Data commit/invalidation may not admit new callbacks.
}

// NewReconciler starts one global state owner and no per-idle-fiber goroutine.
// Mount, Update and Dispose acknowledge graph commands; Settle joins lifecycle
// work. Context.Dispose is a separate nonblocking request, never its own join.
func NewReconciler() Runtime {
	r := &reconcilerRuntime{inbox: make(chan rcMessage, 256), wake: make(chan struct{}, 1), done: make(chan struct{}), observers: newNativeObservers()}
	o := &rcOwner{r: r, fibers: make(map[uint64]*rcFiber), providers: make(map[string]*rcProvision), byKey: make(map[string]map[uint64]*rcFiber), failures: make(map[uint64]error), blocked: make(map[uint64]error), retired: make(map[uint64]Snapshot), retiredErr: make(map[uint64]error), awaiting: make(map[uint64][]rcWaiter)}
	go o.run()
	return r
}

// RootContext returns the one native host activation, not a mounted plugin.
// It is intentionally optional: only trusted host construction needs this anchor.
func (r *reconcilerRuntime) RootContext(ctx context.Context) (*Context, error) {
	reply := r.exchange(ctx, rcMessage{kind: rcRootCommand})
	return reply.root, reply.err
}

func (r *reconcilerRuntime) Mount(ctx context.Context, spec Spec) (uint64, error) {
	spec.Needs = slices.Clone(spec.Needs)
	spec.Provides = slices.Clone(spec.Provides)
	reply := r.exchange(ctx, rcMessage{kind: rcMountCommand, spec: spec})
	return reply.id, reply.err
}

func (r *reconcilerRuntime) Batch(ctx context.Context, operations []BatchOperation) ([]uint64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// An acknowledgment can be lost to caller cancellation. Never leave owner
	// validation reading declarations that caller may mutate after Batch returns.
	operations = slices.Clone(operations)
	for i := range operations {
		operations[i].Spec.Needs = slices.Clone(operations[i].Spec.Needs)
		operations[i].Spec.Provides = slices.Clone(operations[i].Spec.Provides)
	}
	reply := r.exchange(ctx, rcMessage{kind: rcBatchCommand, batch: operations})
	return reply.ids, reply.err
}

func (r *reconcilerRuntime) Update(ctx context.Context, id uint64) error {
	return r.exchange(ctx, rcMessage{kind: rcUpdateCommand, id: id}).err
}

// UpdateConfig runs the shared configuration waterfall on its caller, never on
// the state owner. Owner messages capture and validate the lifecycle binding.
func (r *reconcilerRuntime) UpdateConfig(ctx context.Context, id uint64, raw any, noSave bool) error {
	request, err := r.RequestUpdateConfig(ctx, id, raw, noSave)
	if err != nil {
		return err
	}
	return request.Await(ctx)
}

func (r *reconcilerRuntime) RequestUpdateConfig(ctx context.Context, id uint64, raw any, noSave bool) (ConfigRequest, error) {
	request := ConfigRequest{Await: func(context.Context) error { return nil }}
	reply := r.exchange(ctx, rcMessage{kind: rcConfigBindingCommand, id: id})
	if reply.err != nil {
		return request, reply.err
	}
	binding := reply.binding
	err := binding.cell.ApplyUpdate(ctx, raw, noSave, binding.loaded, func() error {
		if err := r.Update(ctx, id); err != nil {
			return err
		}
		request.Await = func(wait context.Context) error { return r.AwaitFiber(wait, id) }
		return nil
	}, func() bool {
		reply := r.exchange(ctx, rcMessage{kind: rcConfigCurrentCommand, id: id, binding: binding})
		return reply.err == nil && reply.current
	}, func() error {
		// Once Commit succeeds the cell must retain raw and clear the old error,
		// even if that callback canceled its own request context.
		return r.exchange(context.WithoutCancel(ctx), rcMessage{kind: rcConfigClearCommand, id: id, binding: binding}).err
	})
	return request, err
}

func (r *reconcilerRuntime) Dispose(ctx context.Context, id uint64) error {
	return r.exchange(ctx, rcMessage{kind: rcDisposeCommand, id: id}).err
}

func (r *reconcilerRuntime) Notify(ctx context.Context, keys ...string) error {
	return r.exchange(ctx, rcMessage{kind: rcNotifyCommand, keys: slices.Clone(keys)}).err
}

func (r *reconcilerRuntime) AwaitFiber(ctx context.Context, id uint64) error {
	return r.exchange(ctx, rcMessage{kind: rcAwaitCommand, id: id}).err
}

func (r *reconcilerRuntime) Record(ctx context.Context, kind string, data map[string]any) error {
	// Fixture marshalers may contain user code. Never invoke them on the owner.
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	var immutable map[string]any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&immutable); err != nil {
		return err
	}
	return r.exchange(ctx, rcMessage{kind: rcRecordCommand, key: kind, data: immutable}).err
}

func (r *reconcilerRuntime) Settle(ctx context.Context) error {
	return r.exchange(ctx, rcMessage{kind: rcSettleCommand}).err
}

func (r *reconcilerRuntime) Snapshots(ctx context.Context) ([]Snapshot, error) {
	reply := r.exchange(ctx, rcMessage{kind: rcSnapshotCommand})
	return reply.snapshots, reply.err
}

// Inspect observes one owner turn without waiting for lifecycle work or running
// availability predicates. Provision records include reserved and retiring rows.
func (r *reconcilerRuntime) Inspect(ctx context.Context) (Inspection, error) {
	reply := r.exchange(ctx, rcMessage{kind: rcInspectCommand})
	return reply.inspection, reply.err
}

func (r *reconcilerRuntime) Events(ctx context.Context) ([]Event, error) {
	reply := r.exchange(ctx, rcMessage{kind: rcEventsCommand})
	if reply.err != nil {
		return nil, reply.err
	}
	// Ledger envelopes contain JSON values only; clone outside the owner so a
	// caller cannot mutate a future Events response or stall graph mutation.
	for i := range reply.events {
		if reply.events[i].Data == nil {
			continue
		}
		encoded, err := json.Marshal(reply.events[i].Data)
		if err != nil {
			return nil, err
		}
		var data map[string]any
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.UseNumber()
		if err := decoder.Decode(&data); err != nil {
			return nil, err
		}
		reply.events[i].Data = data
	}
	return reply.events, nil
}

func (r *reconcilerRuntime) ObserveEvents(owner *Context, label string, fn func(context.Context, Event) error) (Disposer, error) {
	if owner == nil || owner.host != r {
		return nil, ErrObserverBinding
	}
	return r.observers.observe(owner, label, fn)
}

func (r *reconcilerRuntime) AwaitObservers(ctx context.Context) error {
	return r.observers.await(ctx)
}

func (r *reconcilerRuntime) ObserverStatus() ObserverStatus {
	return r.observers.status()
}

func (r *reconcilerRuntime) Close(ctx context.Context) error {
	select {
	case <-r.done:
		return errors.Join(r.rootErr, r.observers.close(ctx))
	default:
	}
	err := r.exchange(ctx, rcMessage{kind: rcCloseCommand}).err
	if errors.Is(err, ErrClosed) {
		err = nil
	}
	// A bounded lifecycle diagnostic can precede actual worker completion. Do
	// not close the queue until the owner exits: later state events still belong
	// to this same native ledger and must not be dropped by an early Close.
	select {
	case <-r.done:
		return errors.Join(err, r.rootErr, r.observers.close(ctx))
	default:
	}
	if err != nil {
		return err
	}
	select {
	case <-r.done:
		return errors.Join(r.rootErr, r.observers.close(ctx))
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *reconcilerRuntime) exchange(ctx context.Context, msg rcMessage) rcReply {
	if err := ctx.Err(); err != nil {
		return rcReply{err: err}
	}
	msg.ctx, msg.ack = ctx, make(chan rcReply, 1)
	select {
	case r.inbox <- msg:
	case <-ctx.Done():
		return rcReply{err: ctx.Err()}
	case <-r.done:
		return rcReply{err: ErrClosed}
	}
	select {
	case reply := <-msg.ack:
		return reply
	case <-ctx.Done():
		return rcReply{err: ctx.Err()}
	case <-r.done:
		select {
		case reply := <-msg.ack:
			return reply
		default:
			return rcReply{err: ErrClosed}
		}
	}
}

func (r *reconcilerRuntime) poke() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *reconcilerRuntime) notify(n *rcNotice) {
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

func (r *reconcilerRuntime) completed(msg rcMessage) {
	select {
	case r.inbox <- msg:
	case <-r.done:
	}
}

func (o *rcOwner) run() {
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	for {
		o.consumeNotices()
		o.expire(time.Now())
		o.work()
		o.finishWaiters()
		if o.closing && len(o.fibers) == 0 && (o.root == nil || o.root.act == nil) {
			o.replyAll(&o.closers, errors.Join(o.terminal...))
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
			reply, deferred := o.handle(msg)
			o.consumeNotices()
			o.work()
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

func (o *rcOwner) handle(m rcMessage) (rcReply, bool) {
	if m.ctx != nil && m.ctx.Err() != nil {
		return rcReply{err: m.ctx.Err()}, false
	}
	if o.closing && (m.kind == rcMountCommand || m.kind == rcMountChildCommand || m.kind == rcUpdateCommand || m.kind == rcConfigBindingCommand || m.kind == rcRootCommand || m.kind == rcBatchCommand) {
		return rcReply{err: ErrClosed}, false
	}
	switch m.kind {
	case rcRootCommand:
		if o.root == nil {
			o.startRoot()
		}
		if o.root.act == nil || !rcAlive(o.root) || o.root.invalid {
			return rcReply{err: ErrInactive}, false
		}
		return rcReply{root: o.root.act.ctx}, false
	case rcMountCommand:
		f, err := o.mount(m.spec, nil)
		if err != nil {
			return rcReply{err: err}, false
		}
		return rcReply{id: f.id}, false
	case rcMountChildCommand:
		parent, err := o.current(m.act)
		if err != nil || m.act.selfDispose.Load() {
			return rcReply{err: ErrInactive}, false
		}
		f, err := o.mount(m.spec, parent.act)
		if err != nil {
			return rcReply{err: err}, false
		}
		return rcReply{id: f.id}, false
	case rcBatchCommand:
		ids, err := o.batch(m.ctx, m.batch)
		return rcReply{ids: ids, err: err}, false
	case rcUpdateCommand:
		f := o.fibers[m.id]
		if f == nil {
			return rcReply{err: ErrUnknown}, false
		}
		if f.remove {
			return rcReply{err: ErrInactive}, false
		}
		o.update(f)
	case rcConfigBindingCommand:
		f := o.fibers[m.id]
		if f == nil {
			return rcReply{err: ErrUnknown}, false
		}
		if !rcAlive(f) {
			return rcReply{err: ErrInactive}, false
		}
		if f.spec.Config == nil {
			return rcReply{err: errors.New("fiber has no configuration")}, false
		}
		return rcReply{binding: &rcConfigBinding{cell: f.spec.Config, act: f.act, target: slices.Clone(f.target), loaded: f.target != nil}}, false
	case rcConfigClearCommand:
		f := o.fibers[m.id]
		if f == nil || !rcAlive(f) {
			return rcReply{err: ErrInactive}, false
		}
		if m.binding == nil || f.spec.Config != m.binding.cell {
			return rcReply{err: ErrInactive}, false
		}
		f.awaitErr, f.awaitText = nil, ""
		o.fail(f, nil)
	case rcConfigCurrentCommand:
		f := o.fibers[m.id]
		return rcReply{current: f != nil && rcAlive(f) && m.binding != nil && f.spec.Config == m.binding.cell && f.act == m.binding.act && slices.Equal(f.target, m.binding.target)}, false
	case rcSelfDisposeCommand:
		f, err := o.current(m.act)
		if err != nil {
			return rcReply{err: err}, false
		}
		m.act.selfDispose.Store(true)
		o.requestDispose(f)
	case rcDisposeCommand:
		if m.id == 0 {
			return rcReply{err: ErrUnknown}, false
		}
		if f := o.fibers[m.id]; f != nil {
			o.requestDispose(f)
		}
	case rcAwaitCommand:
		if _, retired := o.retired[m.id]; retired {
			return rcReply{err: o.retiredErr[m.id]}, false
		}
		if o.fibers[m.id] == nil {
			return rcReply{err: ErrUnknown}, false
		}
		o.awaiting[m.id] = append(o.awaiting[m.id], o.waiter(m))
		return rcReply{}, true
	case rcRecordCommand:
		o.appendEvent(Event{Sequence: uint64(len(o.events) + 1), Kind: "fixture", Key: m.key, Data: m.data})
	case rcSettleCommand:
		o.settlers = append(o.settlers, o.waiter(m))
		return rcReply{}, true
	case rcCloseCommand:
		o.closers = append(o.closers, o.waiter(m))
		o.beginClose()
		return rcReply{}, true
	case rcSnapshotCommand:
		return rcReply{snapshots: o.snapshots()}, false
	case rcInspectCommand:
		return rcReply{inspection: o.inspect()}, false
	case rcEventsCommand:
		return rcReply{events: slices.Clone(o.events)}, false
	case rcProvideCommand:
		f, err := o.current(m.act)
		if err != nil {
			return rcReply{err: err}, false
		}
		if m.key == "" || (f.id != 0 && !slices.Contains(f.spec.Provides, m.key)) {
			return rcReply{err: fmt.Errorf("undeclared provision %q", m.key)}, false
		}
		if o.providers[m.key] != nil {
			return rcReply{err: fmt.Errorf("duplicate provision %q", m.key)}, false
		}
		o.nextUID++
		p := &rcProvision{key: m.key, binding: provision{uid: o.nextUID, owner: f.id, value: m.value}, dependents: make(map[*Context]*rcFiber)}
		o.providers[m.key] = p
		f.act.ctx.publishOwned(m.key)
		f.provisions = append(f.provisions, p)
		f.effects = append(f.effects, rcEffect{label: "provide:" + m.key, provision: p})
		o.event(f, "provide", m.key, p.binding.uid)
		if f.state == Active {
			o.notifyKey(m.key)
			o.work()
			o.event(f, "service", m.key, p.binding.uid)
		}
	case rcEffectCommand:
		f, err := o.current(m.act)
		if err != nil {
			return rcReply{err: err}, false
		}
		if m.dispose == nil {
			return rcReply{err: errors.New("nil disposer")}, false
		}
		f.effects = append(f.effects, rcEffect{label: m.key, dispose: m.dispose})
		o.event(f, "effect", m.key, 0)
	case rcTrackedStartCommand:
		f, err := o.current(m.act)
		if err != nil || m.act.selfDispose.Load() {
			return rcReply{err: ErrInactive}, false
		}
		if m.key == "" {
			return rcReply{err: errors.New("empty tracked work label")}, false
		}
		if m.act.tracked == nil {
			m.act.tracked = make(map[uint64]string)
		}
		m.act.nextTracked++
		m.act.tracked[m.act.nextTracked] = m.key
		o.event(f, "go:start", m.key, 0)
		return rcReply{id: m.act.nextTracked}, false
	case rcTrackedDoneCommand:
		o.trackedDone(m)
	case rcNotifyCommand:
		for _, key := range m.keys {
			for _, f := range o.byKey[key] {
				f.checkVersion++
				f.deniedEpoch = nil
				o.enqueue(f)
			}
		}
	case rcCheckComplete:
		o.checkedResult(m)
	case rcLoadComplete:
		o.loaded(m)
	case rcEffectComplete:
		o.effectDone(m)
	}
	return rcReply{}, false
}

func (o *rcOwner) update(f *rcFiber) {
	f.revision++
	f.awaitErr, f.awaitText = nil, ""
	f.checkVersion++
	f.deniedEpoch = nil
	o.fail(f, nil)
	// Restart refreshes the desired revision using already checked bindings.
	if epoch, _ := o.candidate(f); epoch != nil && f.checkAllowed && slices.Equal(epoch[1:], rcUIDs(f.checkedEpoch)) {
		f.checkedEpoch, f.checked = epoch, f.checkVersion
	}
	o.enqueue(f)
}

func (o *rcOwner) batch(ctx context.Context, operations []BatchOperation) ([]uint64, error) {
	prepared, err := prepareBatch(operations, func(id uint64) bool {
		f := o.fibers[id]
		return f != nil && rcAlive(f)
	})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	results := make([]uint64, len(prepared))
	touched := make([]*rcFiber, 0, len(prepared))
	o.batchDepth++
	for i, op := range prepared {
		switch op.Kind {
		case BatchMount:
			// prepareBatch has validated the same mount declarations. IDs are
			// consumed only now, after the entire prefix has been admitted.
			f, _ := o.mount(op.Spec, nil)
			results[i] = f.id
		case BatchUpdate:
			f := o.fibers[op.Fiber]
			o.update(f)
			touched = append(touched, f)
		case BatchDispose:
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

func (o *rcOwner) invalidateBatch(queue []*rcFiber) {
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
		if f.act != nil {
			for id := range f.act.children {
				queue = append(queue, o.fibers[id])
			}
		}
	}
}

func (o *rcOwner) lookup(id uint64) *rcFiber {
	if id == 0 {
		return o.root
	}
	return o.fibers[id]
}

func (o *rcOwner) startRoot() {
	f := &rcFiber{spec: Spec{ID: "root"}, state: Active, revision: 1, target: []uint64{1}, deadlineIndex: -1}
	o.root = f
	f.act = o.activation(f, slices.Clone(f.target), nil)
}

func (o *rcOwner) beginClose() {
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

func (o *rcOwner) mount(spec Spec, parent *rcActivation) (*rcFiber, error) {
	if err := rcValidateSpec(spec); err != nil {
		return nil, err
	}
	o.nextFiber++
	f := &rcFiber{id: o.nextFiber, spec: spec, state: Pending, deadlineIndex: -1, parent: parent}
	o.fibers[f.id] = f
	if parent != nil {
		if parent.children == nil {
			parent.children = make(map[uint64]struct{})
		}
		parent.children[f.id] = struct{}{}
	}
	for _, key := range f.spec.Needs {
		if o.byKey[key] == nil {
			o.byKey[key] = make(map[uint64]*rcFiber)
		}
		o.byKey[key][f.id] = f
	}
	o.event(f, "mount", "", 0)
	o.enqueue(f)
	return f, nil
}

func rcValidateSpec(spec Spec) error {
	if spec.Apply != nil && spec.ApplyEffect != nil {
		return errors.New("Apply and ApplyEffect are mutually exclusive")
	}
	for _, keys := range [][]string{spec.Needs, spec.Provides} {
		seen := make(map[string]bool, len(keys))
		for _, key := range keys {
			if key == "" || seen[key] {
				return fmt.Errorf("empty or duplicate declaration %q", key)
			}
			seen[key] = true
		}
	}
	return nil
}

func (o *rcOwner) current(a *rcActivation) (*rcFiber, error) {
	if a == nil {
		return nil, ErrInactive
	}
	f := o.lookup(a.ctx.fiber)
	if f == nil || f.act != a || f.invalid || f.remove || !a.ctx.live.Load() {
		return nil, ErrInactive
	}
	return f, nil
}

func (o *rcOwner) requestDispose(f *rcFiber) {
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

func (o *rcOwner) enqueue(f *rcFiber) {
	if f.queued {
		return
	}
	f.queued = true
	o.queue = append(o.queue, f.id)
}

func (o *rcOwner) notifyKey(key string) {
	ids := make([]uint64, 0, len(o.byKey[key]))
	for id := range o.byKey[key] {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		o.enqueue(o.byKey[key][id])
	}
}

func (o *rcOwner) work() {
	if o.batchDepth != 0 {
		return
	}
	outer := !o.working
	o.working = true
	for o.workHead < len(o.queue) {
		id := o.queue[o.workHead]
		o.workHead++
		f := o.lookup(id)
		if f == nil {
			continue
		}
		f.queued = false
		if f.id == 0 {
			if f.phase == rcDraining {
				if o.noticeDepth != 0 {
					if o.deferredDrain == nil {
						o.deferredDrain = make(map[uint64]*rcFiber)
					}
					o.deferredDrain[0] = f
				} else {
					o.drain(f)
				}
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
			if f.phase == rcDraining {
				if o.noticeDepth != 0 {
					if o.deferredDrain == nil {
						o.deferredDrain = make(map[uint64]*rcFiber)
					}
					o.deferredDrain[f.id] = f
				} else {
					o.drain(f)
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
func (o *rcOwner) candidate(f *rcFiber) ([]uint64, map[string]*rcProvision) {
	if f.remove || f.failure != nil {
		return nil, nil
	}
	epoch := make([]uint64, 1, len(f.spec.Needs)+1)
	epoch[0] = f.revision
	bindings := make(map[string]*rcProvision, len(f.spec.Needs))
	for _, key := range f.spec.Needs {
		p := o.providers[key]
		if p == nil {
			return nil, nil
		}
		provider := o.lookup(p.binding.owner)
		if !rcProviderActive(provider) {
			return nil, nil
		}
		epoch = append(epoch, p.binding.uid)
		bindings[key] = p
	}
	return epoch, bindings
}

func rcAlive(f *rcFiber) bool {
	return !f.remove && (f.act == nil || !f.act.selfDispose.Load())
}

func rcProviderActive(f *rcFiber) bool {
	return f != nil && f.state == Active && !f.invalid && rcAlive(f)
}

func rcUIDs(epoch []uint64) []uint64 {
	if len(epoch) == 0 {
		return nil
	}
	return epoch[1:]
}

func (o *rcOwner) refreshTarget(f *rcFiber, epoch []uint64) {
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
		if f.checkAllowed {
			f.target = slices.Clone(epoch)
		} else {
			f.target = nil
		}
		return
	}
	// Rechecks retain a previous result only for the identical desired epoch.
	// A new provider UID cannot become a target before its Check has succeeded.
	if !slices.Equal(f.target, epoch) {
		f.target = nil
	}
}

func (o *rcOwner) cancelCheckIfStale(f *rcFiber, epoch []uint64) {
	if q := f.checking; q != nil && (epoch == nil || !slices.Equal(epoch, q.epoch) || q.generation != f.checkVersion) {
		q.cancel()
	}
}

func (o *rcOwner) availability(f *rcFiber, epoch []uint64, bindings map[string]*rcProvision) bool {
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
	c := &Context{std: std, host: o.r, fiber: f.id, bindings: make(map[string]provision, len(bindings)), dispose: func() error { return ErrInactive }}
	q := &rcCheck{ctx: c, cancel: cancel, epoch: slices.Clone(epoch), bindings: bindings, generation: f.checkVersion, deadline: time.Now().Add(rcLoadBudget)}
	for key, p := range bindings {
		c.bindings[key] = p.binding
		p.dependents[c] = f
	}
	f.checking = q
	o.busy++
	if f.phase == rcIdle {
		o.deadline(f, q.deadline)
	}
	check := f.spec.Check
	go func() {
		var allowed bool
		err := rcInvoke(func() error { allowed = check(c); return nil })
		o.r.completed(rcMessage{kind: rcCheckComplete, id: c.fiber, check: q, allowed: allowed, err: err, errText: rcErrorText(err)})
	}()
	return false
}

func (o *rcOwner) checkedResult(m rcMessage) {
	f := o.fibers[m.id]
	if f == nil || f.checking != m.check {
		return
	}
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
	o.busy--
	if f.phase == rcIdle {
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

func (o *rcOwner) activation(f *rcFiber, epoch []uint64, bindings map[string]*rcProvision) *rcActivation {
	std, cancel := context.WithCancel(context.Background())
	c := &Context{std: std, host: o.r, fiber: f.id, bindings: make(map[string]provision, len(bindings))}
	a := &rcActivation{ctx: c, cancel: cancel, epoch: epoch}
	a.notice.act = a
	for key, p := range bindings {
		c.bindings[key] = p.binding
		p.dependents[c] = f
	}
	// The bindings map is an immutable activation snapshot. Reads and leases
	// never enter the owner, and the transport waker runs only on quiescent exit.
	c.leaseChanged = func() {
		if !c.live.Load() && c.lease.Load() == 0 {
			o.r.notify(&a.notice)
		}
	}
	c.provide = func(key string, value any) error {
		// The owner reply decides whether the native record owns this resource.
		// Activation cancellation cannot revoke a successful admission ack.
		return o.r.exchange(context.Background(), rcMessage{kind: rcProvideCommand, act: a, key: key, value: value}).err
	}
	c.effect = func(label string, dispose Disposer) error {
		// Registration transfers disposer ownership exactly when the native
		// owner accepts it. Cancellation cannot turn that success into a caller-
		// owned rollback and cause both paths to invoke the same disposer.
		return o.r.exchange(context.Background(), rcMessage{kind: rcEffectCommand, act: a, key: label, dispose: dispose}).err
	}
	c.cleanupEffect = func(dispose Disposer) error {
		// Setup rollback is not a new activation effect. Its caller still holds
		// the real setup lease, so even a cancellation-ignoring disposer cannot
		// let native teardown release resources before this callback returns.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), rcDisposeBudget)
		defer cancel()
		err := rcInvoke(func() error { return dispose(cleanupCtx) })
		return errors.Join(err, cleanupCtx.Err())
	}
	c.trackedStart = func(label string) (func(error), error) {
		// The owner decides admission against this activation. Do not abandon an
		// admitted record when Std is canceled between registration and its ack.
		reply := o.r.exchange(context.Background(), rcMessage{kind: rcTrackedStartCommand, act: a, key: label})
		if reply.err != nil {
			if std.Err() != nil {
				return nil, ErrInactive
			}
			return nil, reply.err
		}
		id := reply.id
		return func(err error) {
			// run and even Error() belong to the worker. Completion uses a
			// non-canceled transport context so quiescence cannot lose its record.
			o.r.exchange(context.Background(), rcMessage{kind: rcTrackedDoneCommand, act: a, id: id, err: err, errText: rcErrorText(err)})
		}, nil
	}
	c.child = func(spec Spec) (uint64, error) {
		spec.Needs, spec.Provides = slices.Clone(spec.Needs), slices.Clone(spec.Provides)
		// A committed child must return its actual ID even when the parent is
		// quiesced before the caller receives this ownership-transfer reply.
		reply := o.r.exchange(context.Background(), rcMessage{kind: rcMountChildCommand, act: a, spec: spec})
		return reply.id, reply.err
	}
	c.dispose = func() error {
		if f.id == 0 {
			return ErrRootDispose
		}
		if !c.live.Load() {
			return ErrInactive
		}
		// Dispose joins only command admission, never Apply or cleanup. Exact
		// activation validation prevents a retiring context from disposing the
		// fiber whose desired revision has already moved on.
		return o.r.exchange(context.Background(), rcMessage{kind: rcSelfDisposeCommand, act: a}).err
	}
	c.live.Store(true)
	return a
}

func (o *rcOwner) load(f *rcFiber, epoch []uint64, bindings map[string]*rcProvision) {
	a := o.activation(f, epoch, bindings)
	c, std := a.ctx, a.ctx.std
	f.act, f.committed, f.invalid, f.phase = a, bindings, false, rcApplying
	o.busy++
	o.deadline(f, time.Now().Add(rcLoadBudget))
	o.state(f, Loading)
	apply, applyEffect, config := f.spec.Apply, f.spec.ApplyEffect, f.spec.Config
	go func() {
		var returned Disposer
		var stale bool
		err := rcInvoke(func() error {
			if config != nil {
				prepared, err := config.Prepare(std)
				if err != nil {
					return err
				}
				if !prepared.Published {
					stale = true
					return nil
				}
				c.config, c.configVersion = prepared.Value, prepared.Version
			}
			if applyEffect != nil {
				var err error
				returned, err = applyEffect(c)
				return err
			}
			if apply == nil {
				return nil
			}
			return apply(c)
		})
		o.r.completed(rcMessage{kind: rcLoadComplete, id: c.fiber, act: a, dispose: returned, stale: stale, err: err, errText: rcErrorText(err)})
	}()
}

// Error formatting can invoke plugin methods just like Check or Apply. Cache it
// on the worker that obtained the error; keep the original object for errors.Is.
func rcErrorText(err error) (text string) {
	if err == nil {
		return ""
	}
	defer func() {
		if recover() != nil {
			text = "error formatting panic"
		}
	}()
	return err.Error()
}

func rcJoinText(texts ...string) string {
	return strings.Join(slices.DeleteFunc(texts, func(text string) bool { return text == "" }), "\n")
}

type rcDiagnostic struct {
	text  string
	cause error
}

func (e *rcDiagnostic) Error() string { return e.text }
func (e *rcDiagnostic) Unwrap() error { return e.cause }

func rcInvoke(fn func() error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("callback panic: %v", p)
		}
	}()
	return fn()
}

func (o *rcOwner) loaded(m rcMessage) {
	f := o.fibers[m.id]
	if f == nil || f.act != m.act || f.phase != rcApplying {
		return
	}
	if m.err == nil && m.dispose != nil {
		// A returned plugin disposer is collected after all body effects, even
		// when the load was superseded or requested its own disposal meanwhile.
		f.effects = append(f.effects, rcEffect{label: "apply", dispose: m.dispose})
		o.event(f, "effect", "apply", 0)
	}
	if m.act.selfDispose.Load() {
		o.requestDispose(f)
	}
	current := !f.invalid && !f.remove && f.target != nil && slices.Equal(f.target, m.act.epoch)
	o.deadline(f, time.Time{})
	o.block(f, nil)
	if current && !m.stale && m.err == nil {
		f.phase = rcIdle
		o.busy--
		o.state(f, Active)
		return
	}
	if current && m.err != nil {
		f.awaitErr, f.awaitText = m.err, m.errText
		o.fail(f, m.err, m.errText)
	}
	o.quiesce(f)
	f.phase = rcDraining
	o.state(f, Unloading)
	o.deadline(f, time.Now().Add(rcDrainBudget))
	o.enqueue(f)
}

func (o *rcOwner) trackedDone(m rcMessage) {
	a := m.act
	if a == nil {
		return
	}
	label, exists := a.tracked[m.id]
	if !exists {
		return
	}
	delete(a.tracked, m.id)
	f := o.lookup(a.ctx.fiber)
	if f == nil || f.act != a {
		return
	}
	o.event(f, "go:done", label, 0)
	if m.err != nil && !f.invalid && !f.remove && !a.selfDispose.Load() && f.target != nil && slices.Equal(f.target, a.epoch) {
		// A detached worker may fail after Apply became ACTIVE. Treat this as an
		// activation failure, but never poison a newer desired epoch with an old
		// completion. LOADING retains its single in-flight Apply until it returns.
		f.awaitErr, f.awaitText = m.err, m.errText
		o.fail(f, m.err, m.errText)
		if f.id == 0 {
			// Host work failure retires only the root-owned lifetime. Ordinary
			// unrelated fibers remain usable until an explicit Runtime.Close.
			o.requestDispose(f)
		}
		o.quiesce(f)
	}
	o.enqueue(f)
}

func (o *rcOwner) quiesce(f *rcFiber) {
	if f.act == nil || f.invalid {
		return
	}
	f.invalid = true
	f.act.ctx.live.Store(false)
	f.act.cancel()
	// Structural ownership is independent of service dependencies. Invalidate
	// children before joining dependents, otherwise a child that never injected a
	// parent service could outlive (or deadlock behind) the parent's cleanup.
	childIDs := make([]uint64, 0, len(f.act.children))
	for id := range f.act.children {
		childIDs = append(childIDs, id)
	}
	slices.Sort(childIDs)
	for _, id := range childIDs {
		if child := o.fibers[id]; child != nil {
			o.requestDispose(child)
		}
	}
	if f.phase == rcIdle {
		o.busy++
		f.phase = rcDraining
		o.deadline(f, time.Now().Add(rcDrainBudget))
	}
	if f.phase != rcApplying {
		o.state(f, Unloading)
	}
}

func (o *rcOwner) drain(f *rcFiber) {
	if f.id == 0 && o.closing && len(o.fibers) != 0 {
		return
	}
	if len(f.act.children) != 0 || len(f.act.tracked) != 0 {
		return
	}
	if f.act.ctx.lease.Load() != 0 {
		return
	}
	if f.checking != nil {
		return
	}
	for _, p := range f.provisions {
		if len(p.dependents) != 0 {
			return
		}
	}
	o.block(f, nil)
	f.phase = rcDisposing
	o.deadline(f, time.Now().Add(rcDisposeBudget))
	f.cleanupCtx, f.cleanupCancel = context.WithDeadline(context.Background(), f.deadline)
	o.disposeNext(f)
}

func (o *rcOwner) disposeNext(f *rcFiber) {
	for len(f.effects) != 0 {
		e := f.effects[len(f.effects)-1]
		o.event(f, "dispose-effect", e.label, 0)
		if e.provision != nil {
			p := e.provision
			if o.providers[p.key] == p {
				delete(o.providers, p.key)
			}
			f.act.ctx.unpublishOwned(p.key)
			f.provisions = slices.DeleteFunc(f.provisions, func(other *rcProvision) bool { return other == p })
			o.event(f, "remove", p.key, p.binding.uid)
			o.notifyKey(p.key)
			f.effects[len(f.effects)-1] = rcEffect{}
			f.effects = f.effects[:len(f.effects)-1]
			continue
		}
		f.effectRunning = true
		act, cleanupCtx, id := f.act, f.cleanupCtx, f.id
		go func() {
			err := rcInvoke(func() error { return e.dispose(cleanupCtx) })
			o.r.completed(rcMessage{kind: rcEffectComplete, id: id, act: act, err: err, errText: rcErrorText(err)})
		}()
		return
	}
	o.unloaded(f)
}

func (o *rcOwner) effectDone(m rcMessage) {
	f := o.lookup(m.id)
	if f == nil || f.act != m.act || f.phase != rcDisposing || !f.effectRunning {
		return
	}
	f.effectRunning = false
	// Disposal has one total deadline for this activation, not a fresh budget
	// per effect. Once it expires, retain the blocked diagnostic until all
	// remaining LIFO cleanup actually returns; the owner timer is already spent.
	if f.cleanupCtx == nil || f.cleanupCtx.Err() == nil {
		o.block(f, nil)
	}
	if m.err != nil {
		o.fail(f, errors.Join(f.failure, m.err), rcJoinText(f.failureText, m.errText))
	}
	f.effects[len(f.effects)-1] = rcEffect{}
	f.effects = f.effects[:len(f.effects)-1]
	o.disposeNext(f)
}

func (o *rcOwner) unloaded(f *rcFiber) {
	f.cleanupCancel()
	f.act.ctx.released.Store(true)
	// A provider may already be queued by a sibling's release. Keep these
	// actual dependent links until this consumer records its next lifecycle
	// state; enqueue order alone cannot stop that provider overtaking it.
	f.releasing, f.releaseBindings = f.act.ctx, f.committed
	f.committed, f.act, f.effects, f.provisions = nil, nil, nil, nil
	f.phase = rcIdle
	o.busy--
	o.deadline(f, time.Time{})
	o.block(f, nil)
	f.cleanupCtx, f.cleanupCancel = nil, nil
	if f.id == 0 {
		// Keep the host's final diagnostics without publishing an ordinary fiber
		// snapshot or restarting its lifetime. All user fibers are already gone.
		if f.failure != nil {
			diagnostic := &rcDiagnostic{text: f.failureText, cause: f.failure}
			o.terminal = append(o.terminal, diagnostic)
			o.r.rootErr = diagnostic
		}
		o.fail(f, nil)
		o.state(f, Disposed)
		o.releaseDependencies(f)
		return
	}
	if f.checking != nil {
		// A new desired provider can be checked while old cleanup is gated.
		// Once cleanup ends its worker still owns a bounded check deadline.
		o.deadline(f, f.checking.deadline)
	}
	// The next work turn publishes PENDING/FAILED/DISPOSED or starts a new
	// LOADING activation before it drops the old binding's dependent records.
	o.enqueue(f)
}

func (o *rcOwner) releaseDependencies(f *rcFiber) {
	if f.releasing == nil {
		return
	}
	for _, p := range f.releaseBindings {
		delete(p.dependents, f.releasing)
		if owner := o.lookup(p.binding.owner); owner != nil {
			o.enqueue(owner)
		}
	}
	f.releasing, f.releaseBindings = nil, nil
}

func (o *rcOwner) remove(f *rcFiber) {
	diagnostic := o.snapshot(f).Error
	if f.failure != nil {
		o.terminal = append(o.terminal, &rcDiagnostic{text: f.failureText, cause: f.failure})
	}
	o.fail(f, nil)
	o.block(f, nil)
	o.deadline(f, time.Time{})
	for _, key := range f.spec.Needs {
		delete(o.byKey[key], f.id)
		if len(o.byKey[key]) == 0 {
			delete(o.byKey, key)
		}
	}
	o.state(f, Disposed)
	archive := o.snapshot(f)
	archive.Error = diagnostic
	archive.Epoch, archive.Effects = nil, nil
	o.retired[f.id] = archive
	if f.awaitErr != nil {
		o.retiredErr[f.id] = f.awaitErr
	}
	delete(o.fibers, f.id)
	if parent := f.parent; parent != nil {
		delete(parent.children, f.id)
		if owner := o.lookup(parent.ctx.fiber); owner != nil && owner.act == parent {
			o.enqueue(owner)
		}
		f.parent = nil
	}
	if o.closing && o.root != nil && o.root.act != nil {
		o.enqueue(o.root)
	}
}

func (o *rcOwner) consumeNotices() {
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

func (o *rcOwner) state(f *rcFiber, state State) {
	old := f.state
	if old == state {
		return
	}
	f.state = state
	o.appendEvent(Event{Sequence: uint64(len(o.events) + 1), Fiber: f.id, Name: f.spec.ID, Kind: "state", From: old, State: state})
	if old == Active || state == Active {
		for _, p := range slices.Clone(f.provisions) {
			// Notification refreshes dependant state synchronously, but source
			// cleanup resumes only after its service notification has completed.
			// Do not let recursive owner work remove/reload the provider before
			// recording the observed retained value.
			o.noticeDepth++
			o.notifyKey(p.key)
			o.work()
			o.appendEvent(Event{Sequence: uint64(len(o.events) + 1), Fiber: f.id,
				Name: f.spec.ID, Kind: "service", State: state, Key: p.key,
				UID: p.binding.uid, Value: p.binding.value})
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
	}
}

func (o *rcOwner) event(f *rcFiber, kind, key string, uid uint64) {
	e := Event{Sequence: uint64(len(o.events) + 1), Fiber: f.id, Name: f.spec.ID, Kind: kind, State: f.state, Key: key, UID: uid}
	if kind == "service" || kind == "provide" {
		if p := o.providers[key]; p != nil && p.binding.uid == uid {
			e.Value = p.binding.value
		}
	}
	o.appendEvent(e)
}

func (o *rcOwner) appendEvent(event Event) {
	o.events = append(o.events, event)
	o.r.observers.append(event)
}

func (o *rcOwner) ids() []uint64 {
	ids := make([]uint64, 0, len(o.fibers))
	for id := range o.fibers {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func (o *rcOwner) snapshot(f *rcFiber) Snapshot {
	s := Snapshot{ID: f.id, Name: f.spec.ID, State: f.state, Revision: f.revision, Alive: rcAlive(f)}
	if config := f.spec.Config; config != nil {
		value := config.Snapshot()
		s.Configured, s.ConfigPrepared = true, value.Prepared
		s.RawConfig, s.EffectiveConfig, s.ConfigVersion = value.Raw, value.Effective, value.Version
	}
	if s.Alive && f.target != nil {
		s.Target = &EpochSnapshot{Revision: f.target[0], UIDs: slices.Clone(f.target[1:])}
	}
	if f.act != nil {
		s.Epoch = slices.Clone(f.act.epoch[1:])
		trackedIDs := make([]uint64, 0, len(f.act.tracked))
		for id := range f.act.tracked {
			trackedIDs = append(trackedIDs, id)
		}
		slices.Sort(trackedIDs)
		for _, id := range trackedIDs {
			s.Tracked = append(s.Tracked, f.act.tracked[id])
		}
	}
	for _, e := range f.effects {
		s.Effects = append(s.Effects, e.label)
	}
	s.Error = rcJoinText(f.failureText, f.blockedText, f.checkText, f.awaitText)
	return s
}

func (o *rcOwner) snapshots() []Snapshot {
	ss := make([]Snapshot, 0, len(o.fibers)+len(o.retired))
	for _, id := range o.ids() {
		ss = append(ss, o.snapshot(o.fibers[id]))
	}
	for _, archived := range o.retired {
		archived.Epoch = slices.Clone(archived.Epoch)
		archived.Effects = slices.Clone(archived.Effects)
		archived.Tracked = slices.Clone(archived.Tracked)
		if archived.Target != nil {
			archived.Target = &EpochSnapshot{Revision: archived.Target.Revision, UIDs: slices.Clone(archived.Target.UIDs)}
		}
		ss = append(ss, archived)
	}
	slices.SortFunc(ss, func(a, b Snapshot) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return ss
}

func (o *rcOwner) inspect() Inspection {
	inspection := Inspection{Sequence: uint64(len(o.events)), Fibers: o.snapshots(), Provisions: make([]ProvisionSnapshot, 0, len(o.providers))}
	for key, p := range o.providers {
		owner := o.lookup(p.binding.owner)
		// The provider table owns each record until its real provision effect
		// removes it. Values are the retained binding, never a fabricated zero.
		row := ProvisionSnapshot{Key: key, UID: p.binding.uid, Owner: p.binding.owner, Value: p.binding.value, Active: rcProviderActive(owner)}
		if owner != nil {
			row.Name = owner.spec.ID
		}
		inspection.Provisions = append(inspection.Provisions, row)
	}
	slices.SortFunc(inspection.Provisions, func(a, b ProvisionSnapshot) int {
		if a.UID < b.UID {
			return -1
		}
		if a.UID > b.UID {
			return 1
		}
		return 0
	})
	return inspection
}

func (o *rcOwner) waiter(m rcMessage) rcWaiter {
	return rcWaiter{ctx: m.ctx, ack: m.ack, stop: context.AfterFunc(m.ctx, o.r.poke)}
}

func (o *rcOwner) finishWaiters() {
	for id, waiters := range o.awaiting {
		f := o.fibers[id]
		if f == nil || (f.phase == rcIdle && f.checking == nil) || f.blocked != nil {
			var err error
			if f != nil {
				err = errors.Join(f.awaitErr, f.blocked)
			} else {
				err = o.retiredErr[id]
			}
			o.replyAll(&waiters, err)
			delete(o.awaiting, id)
		} else {
			o.pruneWaiters(&waiters)
			if len(waiters) == 0 {
				delete(o.awaiting, id)
			} else {
				o.awaiting[id] = waiters
			}
		}
	}
	if len(o.settlers) == 0 && len(o.closers) == 0 {
		return
	}
	if o.busy == 0 || len(o.blocked) != 0 {
		var failures []error
		for id, err := range o.failures {
			failures = append(failures, &rcDiagnostic{text: fmt.Sprintf("fiber %d: %s", id, o.lookup(id).failureText), cause: err})
		}
		for _, err := range o.blocked {
			failures = append(failures, err)
		}
		// Sort diagnostics only: lifecycle events are never reordered.
		sort.Slice(failures, func(i, j int) bool { return failures[i].Error() < failures[j].Error() })
		err := errors.Join(append(failures, o.terminal...)...)
		o.replyAll(&o.settlers, err)
		if len(o.blocked) != 0 {
			o.replyAll(&o.closers, err)
		}
	} else {
		o.pruneWaiters(&o.settlers)
		o.pruneWaiters(&o.closers)
	}
}

func (o *rcOwner) replyAll(waiters *[]rcWaiter, err error) {
	for _, w := range *waiters {
		w.stop()
		if w.ctx.Err() != nil {
			w.ack <- rcReply{err: w.ctx.Err()}
		} else {
			w.ack <- rcReply{err: err}
		}
	}
	clear(*waiters)
	*waiters = nil
}

func (o *rcOwner) pruneWaiters(waiters *[]rcWaiter) {
	*waiters = slices.DeleteFunc(*waiters, func(w rcWaiter) bool {
		if w.ctx.Err() == nil {
			return false
		}
		w.stop()
		w.ack <- rcReply{err: w.ctx.Err()}
		return true
	})
}

func (o *rcOwner) fail(f *rcFiber, err error, text ...string) {
	f.failure = err
	f.failureText = rcJoinText(text...)
	if err != nil {
		f.target = nil
	}
	if err == nil {
		delete(o.failures, f.id)
	} else {
		o.failures[f.id] = err
	}
}

func (o *rcOwner) block(f *rcFiber, err error) {
	f.blocked = err
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

func (o *rcOwner) deadline(f *rcFiber, at time.Time) {
	f.deadline = at
	if at.IsZero() {
		if f.deadlineIndex >= 0 {
			heap.Remove(&o.deadlines, f.deadlineIndex)
		}
	} else if f.deadlineIndex >= 0 {
		heap.Fix(&o.deadlines, f.deadlineIndex)
	} else {
		heap.Push(&o.deadlines, f)
	}
}

func (o *rcOwner) nextDeadline() time.Time {
	if len(o.deadlines) == 0 {
		return time.Time{}
	}
	return o.deadlines[0].deadline
}

func (o *rcOwner) expire(now time.Time) {
	for len(o.deadlines) != 0 && !now.Before(o.deadlines[0].deadline) {
		f := o.deadlines[0]
		o.deadline(f, time.Time{})
		stage := "load"
		if f.checking != nil && f.phase == rcIdle {
			stage = "check"
			f.checking.timedOut = true
			f.checking.cancel()
		}
		if f.phase == rcDraining {
			stage = "drain"
		}
		if f.phase == rcDisposing {
			stage = "dispose"
		}
		o.block(f, fmt.Errorf("fiber %d %s deadline: %w", f.id, stage, context.DeadlineExceeded))
		if f.phase == rcApplying {
			if !f.invalid && !f.remove {
				f.awaitErr, f.awaitText = f.blocked, f.blockedText
				o.fail(f, f.blocked, f.blockedText)
			}
			o.quiesce(f)
			o.state(f, Unloading)
		}
		if f.cleanupCancel != nil {
			f.cleanupCancel()
		}
		o.event(f, "deadline", stage, 0)
	}
}
