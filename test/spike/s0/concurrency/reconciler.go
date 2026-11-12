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
	rcUpdateCommand
	rcDisposeCommand
	rcSettleCommand
	rcSnapshotCommand
	rcEventsCommand
	rcCloseCommand
	rcProvideCommand
	rcEffectCommand
	rcLoadComplete
	rcEffectComplete
	rcNotifyCommand
	rcCheckComplete
	rcAwaitCommand
	rcRecordCommand
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
	keys    []string
	check   *rcCheck
	allowed bool
	data    map[string]any
}

type rcReply struct {
	id        uint64
	snapshots []Snapshot
	events    []Event
	err       error
}

// reconcilerRuntime is only transport. All graph fields belong to rcOwner.run.
// The bounded inbox provides external backpressure; intrusive coalesced notices
// let Dispose requests and lease releases wake the owner without blocking callers.
type reconcilerRuntime struct {
	inbox   chan rcMessage
	wake    chan struct{}
	done    chan struct{}
	notices atomic.Pointer[rcNotice]
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
	id            uint64
	spec          Spec
	state         State
	revision      uint64
	act           *rcActivation
	committed     map[string]*rcProvision
	provisions    []*rcProvision
	effects       []rcEffect
	phase         rcPhase
	invalid       bool
	remove        bool
	queued        bool
	failure       error
	awaitErr      error // Startup failure retained by Await, not cleanup diagnostics.
	blocked       error
	deadline      time.Time
	deadlineIndex int
	cleanupCtx    context.Context
	cleanupCancel context.CancelFunc
	effectRunning bool
	checking      *rcCheck
	checkVersion  uint64
	checked       uint64
	checkedEpoch  []uint64
	checkAllowed  bool
	checkError    error
	deniedEpoch   []uint64
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
	r          *reconcilerRuntime
	fibers     map[uint64]*rcFiber
	providers  map[string]*rcProvision
	byKey      map[string]map[uint64]*rcFiber
	queue      []uint64
	deadlines  rcDeadlines
	busy       int
	failures   map[uint64]error
	blocked    map[uint64]error
	nextFiber  uint64
	nextUID    uint64
	events     []Event
	settlers   []rcWaiter
	closers    []rcWaiter
	closing    bool
	terminal   []error
	retired    map[uint64]Snapshot
	retiredErr map[uint64]error
	awaiting   map[uint64][]rcWaiter
	workHead   int
	working    bool
}

// NewReconciler starts one global state owner and no per-idle-fiber goroutine.
// Mount, Update and Dispose acknowledge graph commands; Settle joins lifecycle
// work. Context.Dispose is a separate nonblocking request, never its own join.
func NewReconciler() Runtime {
	r := &reconcilerRuntime{inbox: make(chan rcMessage, 256), wake: make(chan struct{}, 1), done: make(chan struct{})}
	o := &rcOwner{r: r, fibers: make(map[uint64]*rcFiber), providers: make(map[string]*rcProvision), byKey: make(map[string]map[uint64]*rcFiber), failures: make(map[uint64]error), blocked: make(map[uint64]error), retired: make(map[uint64]Snapshot), retiredErr: make(map[uint64]error), awaiting: make(map[uint64][]rcWaiter)}
	go o.run()
	return r
}

func (r *reconcilerRuntime) Mount(ctx context.Context, spec Spec) (uint64, error) {
	spec.Needs = slices.Clone(spec.Needs)
	spec.Provides = slices.Clone(spec.Provides)
	reply := r.exchange(ctx, rcMessage{kind: rcMountCommand, spec: spec})
	return reply.id, reply.err
}

func (r *reconcilerRuntime) Update(ctx context.Context, id uint64) error {
	return r.exchange(ctx, rcMessage{kind: rcUpdateCommand, id: id}).err
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

func (r *reconcilerRuntime) Close(ctx context.Context) error {
	select {
	case <-r.done:
		return nil
	default:
	}
	err := r.exchange(ctx, rcMessage{kind: rcCloseCommand}).err
	if errors.Is(err, ErrClosed) {
		return nil
	}
	return err
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
		if o.closing && len(o.fibers) == 0 {
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
	if o.closing && (m.kind == rcMountCommand || m.kind == rcUpdateCommand) {
		return rcReply{err: ErrClosed}, false
	}
	switch m.kind {
	case rcMountCommand:
		if err := rcValidateSpec(m.spec); err != nil {
			return rcReply{err: err}, false
		}
		o.nextFiber++
		f := &rcFiber{id: o.nextFiber, spec: m.spec, state: Pending, deadlineIndex: -1}
		o.fibers[f.id] = f
		for _, key := range f.spec.Needs {
			if o.byKey[key] == nil {
				o.byKey[key] = make(map[uint64]*rcFiber)
			}
			o.byKey[key][f.id] = f
		}
		o.event(f, "mount", "", 0)
		o.enqueue(f)
		return rcReply{id: f.id}, false
	case rcUpdateCommand:
		f := o.fibers[m.id]
		if f == nil {
			return rcReply{err: ErrUnknown}, false
		}
		if f.remove {
			return rcReply{err: ErrInactive}, false
		}
		f.revision++
		f.awaitErr = nil
		f.checkVersion++
		f.deniedEpoch = nil
		o.fail(f, nil)
		o.enqueue(f)
	case rcDisposeCommand:
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
		o.events = append(o.events, Event{Sequence: uint64(len(o.events) + 1), Kind: "fixture", Key: m.key, Data: m.data})
	case rcSettleCommand:
		o.settlers = append(o.settlers, o.waiter(m))
		return rcReply{}, true
	case rcCloseCommand:
		o.closing = true
		o.closers = append(o.closers, o.waiter(m))
		for _, id := range o.ids() {
			o.requestDispose(o.fibers[id])
		}
		return rcReply{}, true
	case rcSnapshotCommand:
		return rcReply{snapshots: o.snapshots()}, false
	case rcEventsCommand:
		return rcReply{events: slices.Clone(o.events)}, false
	case rcProvideCommand:
		f, err := o.current(m.act)
		if err != nil {
			return rcReply{err: err}, false
		}
		if !slices.Contains(f.spec.Provides, m.key) {
			return rcReply{err: fmt.Errorf("undeclared provision %q", m.key)}, false
		}
		if o.providers[m.key] != nil {
			return rcReply{err: fmt.Errorf("duplicate provision %q", m.key)}, false
		}
		o.nextUID++
		p := &rcProvision{key: m.key, binding: provision{uid: o.nextUID, owner: f.id, value: m.value}, dependents: make(map[*Context]*rcFiber)}
		o.providers[m.key] = p
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

func rcValidateSpec(spec Spec) error {
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
	f := o.fibers[a.ctx.fiber]
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
	o.fail(f, nil)
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
	outer := !o.working
	o.working = true
	for o.workHead < len(o.queue) {
		id := o.queue[o.workHead]
		o.workHead++
		f := o.fibers[id]
		if f == nil {
			continue
		}
		f.queued = false
		epoch, bindings := o.target(f)
		o.cancelCheckIfStale(f, epoch)
		if f.act != nil {
			if f.act.selfDispose.Load() {
				o.requestDispose(f)
			}
			if f.remove || f.failure != nil || epoch == nil || !slices.Equal(epoch, f.act.epoch) {
				o.quiesce(f)
			}
			if f.phase == rcDraining {
				o.drain(f)
			} else if f.state == Active && f.spec.Check != nil {
				o.availability(f, epoch, bindings)
			}
			continue
		}
		if f.remove {
			if f.checking != nil {
				continue
			}
			o.remove(f)
			continue
		}
		if f.failure != nil {
			o.state(f, Failed)
			continue
		}
		if epoch == nil {
			o.state(f, Pending)
			continue
		}
		if !o.availability(f, epoch, bindings) {
			o.state(f, Pending)
			continue
		}
		o.load(f, epoch, bindings)
	}
	if outer {
		clear(o.queue)
		o.queue = o.queue[:0]
		o.workHead, o.working = 0, false
	}
}

func (o *rcOwner) target(f *rcFiber) ([]uint64, map[string]*rcProvision) {
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
		provider := o.fibers[p.binding.owner]
		if provider == nil || provider.state != Active {
			return nil, nil
		}
		epoch = append(epoch, p.binding.uid)
		bindings[key] = p
	}
	return epoch, bindings
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
	c := &Context{std: std, fiber: f.id, bindings: make(map[string]provision, len(bindings)), dispose: func() error { return ErrInactive }}
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
		o.r.completed(rcMessage{kind: rcCheckComplete, id: c.fiber, check: q, allowed: allowed, err: err})
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
		if owner := o.fibers[p.binding.owner]; owner != nil {
			o.enqueue(owner)
		}
	}
	f.checking = nil
	o.busy--
	if f.phase == rcIdle {
		o.deadline(f, time.Time{})
		o.block(f, nil)
	}
	epoch, _ := o.target(f)
	if !f.remove && q.generation == f.checkVersion && slices.Equal(q.epoch, epoch) {
		if q.timedOut {
			m.allowed, m.err = false, context.DeadlineExceeded
		}
		f.checkedEpoch, f.checked = q.epoch, q.generation
		f.checkAllowed = m.allowed && m.err == nil
		f.checkError = m.err
		if !f.checkAllowed {
			f.deniedEpoch = q.epoch
			if f.act != nil {
				o.quiesce(f)
			}
		}
	}
	o.enqueue(f)
}

func (o *rcOwner) load(f *rcFiber, epoch []uint64, bindings map[string]*rcProvision) {
	std, cancel := context.WithCancel(context.Background())
	c := &Context{std: std, fiber: f.id, bindings: make(map[string]provision, len(bindings))}
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
		err := o.r.exchange(std, rcMessage{kind: rcProvideCommand, act: a, key: key, value: value}).err
		if std.Err() != nil {
			return ErrInactive
		}
		return err
	}
	c.effect = func(label string, dispose Disposer) error {
		err := o.r.exchange(std, rcMessage{kind: rcEffectCommand, act: a, key: label, dispose: dispose}).err
		if std.Err() != nil {
			return ErrInactive
		}
		return err
	}
	c.dispose = func() error {
		if c.released.Load() {
			return ErrInactive
		}
		select {
		case <-o.r.done:
			return ErrClosed
		default:
		}
		a.selfDispose.Store(true)
		o.r.notify(&a.notice)
		return nil
	}
	c.live.Store(true)
	f.act, f.committed, f.invalid, f.phase = a, bindings, false, rcApplying
	o.busy++
	o.deadline(f, time.Now().Add(rcLoadBudget))
	o.state(f, Loading)
	apply := f.spec.Apply
	go func() {
		err := rcInvoke(func() error {
			if apply == nil {
				return nil
			}
			return apply(c)
		})
		o.r.completed(rcMessage{kind: rcLoadComplete, id: c.fiber, act: a, err: err})
	}()
}

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
	if m.act.selfDispose.Load() {
		o.requestDispose(f)
	}
	epoch, _ := o.target(f)
	current := !f.invalid && !f.remove && epoch != nil && slices.Equal(epoch, m.act.epoch)
	o.deadline(f, time.Time{})
	o.block(f, nil)
	if current && m.err == nil {
		f.phase = rcIdle
		o.busy--
		o.state(f, Active)
		return
	}
	if current {
		f.awaitErr = m.err
		o.fail(f, m.err)
	}
	o.quiesce(f)
	f.phase = rcDraining
	o.deadline(f, time.Now().Add(rcDrainBudget))
	o.enqueue(f)
}

func (o *rcOwner) quiesce(f *rcFiber) {
	if f.act == nil || f.invalid {
		return
	}
	f.invalid = true
	f.act.ctx.live.Store(false)
	f.act.cancel()
	if f.phase == rcIdle {
		o.busy++
		f.phase = rcDraining
		o.deadline(f, time.Now().Add(rcDrainBudget))
	}
	o.state(f, Unloading)
}

func (o *rcOwner) drain(f *rcFiber) {
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
			o.r.completed(rcMessage{kind: rcEffectComplete, id: id, act: act, err: err})
		}()
		return
	}
	o.unloaded(f)
}

func (o *rcOwner) effectDone(m rcMessage) {
	f := o.fibers[m.id]
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
		o.fail(f, errors.Join(f.failure, m.err))
	}
	f.effects[len(f.effects)-1] = rcEffect{}
	f.effects = f.effects[:len(f.effects)-1]
	o.disposeNext(f)
}

func (o *rcOwner) unloaded(f *rcFiber) {
	f.cleanupCancel()
	f.act.ctx.released.Store(true)
	for _, p := range f.committed {
		delete(p.dependents, f.act.ctx)
		if owner := o.fibers[p.binding.owner]; owner != nil {
			o.enqueue(owner)
		}
	}
	f.committed, f.act, f.effects, f.provisions = nil, nil, nil, nil
	f.phase = rcIdle
	o.busy--
	o.deadline(f, time.Time{})
	o.block(f, nil)
	f.cleanupCtx, f.cleanupCancel = nil, nil
	o.enqueue(f)
}

func (o *rcOwner) remove(f *rcFiber) {
	diagnostic := o.snapshot(f).Error
	if f.failure != nil {
		o.terminal = append(o.terminal, f.failure)
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
}

func (o *rcOwner) consumeNotices() {
	for n := o.r.notices.Swap(nil); n != nil; {
		next := n.next
		n.queued.Store(false)
		a := n.act
		if f := o.fibers[a.ctx.fiber]; f != nil && f.act == a {
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
	o.events = append(o.events, Event{Sequence: uint64(len(o.events) + 1), Fiber: f.id, Name: f.spec.ID, Kind: "state", From: old, State: state})
	if old == Active || state == Active {
		for _, p := range f.provisions {
			o.notifyKey(p.key)
			// Cordis refreshes consumer state before emitting internal/service.
			// Recursive owner work executes no plugin callbacks: workers submit
			// their own follow-up commands only after this owner turn finishes.
			o.work()
			o.event(f, "service", p.key, p.binding.uid)
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
	o.events = append(o.events, e)
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
	s := Snapshot{ID: f.id, Name: f.spec.ID, State: f.state, Revision: f.revision}
	if f.act != nil {
		s.Epoch = slices.Clone(f.act.epoch[1:])
	}
	for _, e := range f.effects {
		s.Effects = append(s.Effects, e.label)
	}
	if err := errors.Join(f.failure, f.blocked, f.checkError, f.awaitErr); err != nil {
		s.Error = err.Error()
	}
	return s
}

func (o *rcOwner) snapshots() []Snapshot {
	ss := make([]Snapshot, 0, len(o.fibers)+len(o.retired))
	for _, id := range o.ids() {
		ss = append(ss, o.snapshot(o.fibers[id]))
	}
	for _, archived := range o.retired {
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
			failures = append(failures, fmt.Errorf("fiber %d: %w", id, err))
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

func (o *rcOwner) fail(f *rcFiber, err error) {
	f.failure = err
	if err == nil {
		delete(o.failures, f.id)
	} else {
		o.failures[f.id] = err
	}
}

func (o *rcOwner) block(f *rcFiber, err error) {
	f.blocked = err
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
				f.awaitErr = f.blocked
				o.fail(f, f.blocked)
			}
			o.quiesce(f)
		}
		if f.cleanupCancel != nil {
			f.cleanupCancel()
		}
		o.event(f, "deadline", stage, 0)
	}
}
