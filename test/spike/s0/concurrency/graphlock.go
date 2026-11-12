// SPDX-License-Identifier: Apache-2.0

package concurrency

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// GraphLock is an experimental graph-mutex scheduler, not a production kernel.
// Only active lifecycle/check transitions have goroutines; idle fibers have none.
// Context bindings are immutable and Read/Enter/Leave never take the graph lock.
type GraphLock struct {
	mu         sync.Mutex
	fibers     map[uint64]*graphFiber
	retired    map[uint64]Snapshot // Value-only inspection history; no activation/closure references.
	retiredErr map[uint64]error    // Preserve sticky startup-error identity for AwaitFiber after disposal.
	services   map[string]graphService
	indexByKey map[string]map[uint64]*graphFiber
	nextFiber  uint64
	nextUID    uint64
	totalSeq   uint64
	events     []Event
	changed    chan struct{}
	closed     bool
	knobs      graphKnobs
}

// These are design knobs, not retry intervals. Cancellation is immediate;
// deadlines report blocked transitions without pretending effects disappeared.
type graphKnobs struct {
	load       time.Duration
	cleanup    time.Duration
	leaseDrain time.Duration
}

type graphFiber struct {
	id       uint64
	spec     Spec
	state    State
	revision uint64
	epoch    []uint64
	failedAt uint64
	failed   bool
	lastErr  error
	awaitErr error // Current startup failure only; cleanup diagnostics are not startup rejection.
	wantGone bool
	worker   bool
	current  *graphActivation
	notice   uint64
	blocked  *graphBlocked
}

type graphBlocked struct {
	revision uint64
	epoch    []uint64
	notice   uint64
}

type graphActivation struct {
	fiber      *graphFiber
	ctx        *Context
	cancel     context.CancelFunc
	revision   uint64
	epoch      []uint64
	checked    uint64
	provided   map[string]provision
	effects    []graphEffect
	loading    bool
	retiring   bool
	leaseWake  chan struct{}
	loadTimer  *time.Timer
	cleanupErr error
}

type graphEffect struct {
	label   string
	dispose Disposer
}

type graphService struct {
	provision
	activation *graphActivation
}

// NewGraphLock returns the second independent S0 scheduler candidate.
func NewGraphLock() Runtime {
	return &GraphLock{
		fibers:     make(map[uint64]*graphFiber),
		retired:    make(map[uint64]Snapshot),
		retiredErr: make(map[uint64]error),
		services:   make(map[string]graphService),
		indexByKey: make(map[string]map[uint64]*graphFiber),
		changed:    make(chan struct{}),
		knobs: graphKnobs{
			load:       2 * time.Second,
			cleanup:    2 * time.Second,
			leaseDrain: 5 * time.Second,
		},
	}
}

func (g *GraphLock) signalLocked() {
	close(g.changed)
	g.changed = make(chan struct{})
}

func (g *GraphLock) appendEventLocked(event Event) {
	g.totalSeq++
	event.Sequence = g.totalSeq
	g.events = append(g.events, event)
	g.signalLocked()
}

func (g *GraphLock) eventLocked(f *graphFiber, kind, key string, uid uint64) {
	g.appendEventLocked(Event{
		Fiber: f.id, Name: f.spec.ID, Kind: kind, State: f.state,
		Key: key, UID: uid,
	})
}

func (g *GraphLock) serviceEventLocked(f *graphFiber, kind, key string, p provision) {
	g.appendEventLocked(Event{
		Fiber: f.id, Name: f.spec.ID, Kind: kind, State: f.state,
		Key: key, UID: p.uid, Value: p.value,
	})
}

func (g *GraphLock) stateLocked(f *graphFiber, state State) {
	if f.state != state {
		from := f.state
		f.state = state
		g.appendEventLocked(Event{
			Fiber: f.id, Name: f.spec.ID, Kind: "state", From: from, State: state,
		})
	}
}

func graphUnique(keys []string) bool {
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		if key == "" || seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

func (g *GraphLock) Mount(ctx context.Context, spec Spec) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if !graphUnique(spec.Needs) || !graphUnique(spec.Provides) {
		return 0, errors.New("needs/provides must contain unique nonempty keys")
	}
	// The declaration is owned by the scheduler after Mount returns.
	spec.Needs = append([]string(nil), spec.Needs...)
	spec.Provides = append([]string(nil), spec.Provides...)
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return 0, ErrClosed
	}
	g.nextFiber++
	f := &graphFiber{id: g.nextFiber, spec: spec, state: Pending}
	g.fibers[f.id] = f
	for _, key := range spec.Needs {
		if g.indexByKey[key] == nil {
			g.indexByKey[key] = make(map[uint64]*graphFiber)
		}
		g.indexByKey[key][f.id] = f
	}
	g.eventLocked(f, "mount", "", 0)
	g.reconcileLocked(f)
	return f.id, nil
}

func (g *GraphLock) Update(ctx context.Context, id uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return ErrClosed
	}
	f, ok := g.fibers[id]
	if !ok {
		return ErrUnknown
	}
	if f.wantGone {
		return ErrInactive
	}
	f.revision++
	f.lastErr = nil
	f.awaitErr = nil
	g.eventLocked(f, "update", "", 0)
	g.reconcileLocked(f)
	return nil
}

func (g *GraphLock) Dispose(ctx context.Context, id uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	f, ok := g.fibers[id]
	if !ok {
		if _, disposed := g.retired[id]; disposed {
			return nil
		}
		return ErrUnknown
	}
	// Command acknowledgement is distinct from completed cleanup. Settle is
	// the external completion barrier; neither request path self-joins Apply.
	g.requestDisposeLocked(f)
	return nil
}

// Notify invalidates cached consumer-bound availability checks, not the
// provider identities. An unchanged true check never reloads an activation.
func (g *GraphLock) Notify(ctx context.Context, keys ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return ErrClosed
	}
	selected := make(map[uint64]*graphFiber)
	if len(keys) == 0 {
		for id, f := range g.fibers {
			selected[id] = f
		}
	} else {
		for _, key := range keys {
			for id, f := range g.indexByKey[key] {
				selected[id] = f
			}
		}
	}
	ids := make([]uint64, 0, len(selected))
	for id := range selected {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		f := selected[id]
		if f.spec.Check == nil || f.wantGone {
			continue
		}
		f.notice++
		g.eventLocked(f, "availability:notify", "", 0)
		g.reconcileLocked(f)
	}
	return nil
}

func (g *GraphLock) requestDisposeLocked(f *graphFiber) {
	if !f.wantGone {
		f.wantGone = true
		g.eventLocked(f, "dispose:request", "", 0)
	}
	g.reconcileLocked(f)
}

func (g *GraphLock) Settle(ctx context.Context) error {
	return g.wait(ctx, func() bool {
		for _, f := range g.fibers {
			if f.worker || f.state == Loading || f.state == Unloading {
				return false
			}
		}
		return true
	})
}

// AwaitFiber joins only the target's current lifecycle work. A stable PENDING
// target resolves without claiming it is ACTIVE; current FAILED work rejects
// with its sticky startup error. Disposed targets remain joinable through the
// archive and retain that same startup-error identity, not cleanup diagnostics.
// Like Settle, it must not be used to self-join an executing callback or lease.
func (g *GraphLock) AwaitFiber(ctx context.Context, id uint64) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		g.mu.Lock()
		f, exists := g.fibers[id]
		if !exists {
			_, disposed := g.retired[id]
			err := g.retiredErr[id]
			g.mu.Unlock()
			if disposed {
				return err
			}
			return ErrUnknown
		}
		if !f.worker && f.state != Loading && f.state != Unloading {
			err := f.awaitErr
			g.mu.Unlock()
			return err
		}
		changed := g.changed
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func (g *GraphLock) wait(ctx context.Context, done func() bool) error {
	for {
		g.mu.Lock()
		if done() {
			g.mu.Unlock()
			return nil
		}
		changed := g.changed
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func (g *GraphLock) Snapshots(ctx context.Context) ([]Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	ids := make([]uint64, 0, len(g.fibers)+len(g.retired))
	for id := range g.fibers {
		ids = append(ids, id)
	}
	for id := range g.retired {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]Snapshot, 0, len(ids))
	for _, id := range ids {
		if archived, disposed := g.retired[id]; disposed {
			archived.Epoch = append([]uint64(nil), archived.Epoch...)
			out = append(out, archived)
			continue
		}
		f := g.fibers[id]
		s := Snapshot{ID: id, Name: f.spec.ID, State: f.state, Revision: f.revision, Epoch: append([]uint64(nil), f.epoch...)}
		if f.lastErr != nil {
			s.Error = f.lastErr.Error()
		}
		if f.current != nil {
			for _, effect := range f.current.effects {
				s.Effects = append(s.Effects, effect.label)
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// Record is test-only instrumentation, not a lifecycle command. Serialization
// runs off-lock: a JSON marshaler is caller code, and the caller must not mutate
// data while it is being copied. Decoding leaves only owned plain JSON values in
// the ledger. The append itself shares the actual state/service linearization.
func (g *GraphLock) Record(ctx context.Context, kind string, data map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	var copied map[string]any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&copied); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if g.closed {
		return ErrClosed
	}
	g.appendEventLocked(Event{Kind: "fixture", Key: kind, Data: copied})
	return nil
}

// graphCopyJSONValue copies only the standard JSON values produced by Record;
// it never invokes a marshaler, method, or other caller code.
func graphCopyJSONValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		if value == nil {
			return map[string]any(nil)
		}
		copied := make(map[string]any, len(value))
		for key, item := range value {
			copied[key] = graphCopyJSONValue(item)
		}
		return copied
	case []any:
		copied := make([]any, len(value))
		for i, item := range value {
			copied[i] = graphCopyJSONValue(item)
		}
		return copied
	default:
		return value
	}
}

func (g *GraphLock) Events(ctx context.Context) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	out := append([]Event(nil), g.events...)
	g.mu.Unlock()
	// Payloads are immutable after append, so readers can copy off-lock too.
	// Service Value retains its actual identity rather than invoking user code.
	for i := range out {
		if out[i].Data != nil {
			out[i].Data = graphCopyJSONValue(out[i].Data).(map[string]any)
		}
	}
	return out, nil
}

func (g *GraphLock) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	g.mu.Lock()
	g.closed = true
	for _, f := range g.fibers {
		g.requestDisposeLocked(f)
	}
	g.mu.Unlock()
	return g.Settle(ctx)
}

// Transition machinery below deliberately never calls Apply or a disposer
// while holding mu. A retiring activation remains the sole current writer
// until all its callbacks and effects really finish.

var _ Runtime = (*GraphLock)(nil)
var _ Notifier = (*GraphLock)(nil)
var _ Waiter = (*GraphLock)(nil)
var _ Recorder = (*GraphLock)(nil)

func (g *GraphLock) bindingsLocked(f *graphFiber) (map[string]provision, []uint64, bool) {
	bindings := make(map[string]provision, len(f.spec.Needs))
	epoch := make([]uint64, 0, len(f.spec.Needs))
	for _, key := range f.spec.Needs {
		service, ok := g.services[key]
		if !ok || service.activation.retiring || service.activation.fiber.state != Active {
			return nil, nil, false
		}
		bindings[key] = service.provision
		epoch = append(epoch, service.uid)
	}
	return bindings, epoch, true
}

func graphSameEpoch(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (g *GraphLock) targetMatchesLocked(a *graphActivation) bool {
	f := a.fiber
	if f.wantGone || f.revision != a.revision {
		return false
	}
	_, epoch, ready := g.bindingsLocked(f)
	return ready && graphSameEpoch(a.epoch, epoch)
}

func (g *GraphLock) reconcileLocked(f *graphFiber) {
	if f.state == Disposed {
		return
	}
	if f.current != nil {
		a := f.current
		if !a.retiring && !g.targetMatchesLocked(a) {
			g.retireLocked(a)
		} else if !a.retiring && !a.loading && f.spec.Check != nil && a.checked != f.notice && !f.worker {
			f.worker = true
			go g.runActiveCheck(a)
		}
		return
	}
	if f.wantGone {
		g.stateLocked(f, Disposed)
		g.eventLocked(f, "dispose:complete", "", 0)
		archive := Snapshot{ID: f.id, Name: f.spec.ID, State: Disposed, Revision: f.revision, Epoch: append([]uint64(nil), f.epoch...)}
		if f.lastErr != nil {
			archive.Error = f.lastErr.Error()
		}
		g.retired[f.id] = archive
		if f.awaitErr != nil {
			g.retiredErr[f.id] = f.awaitErr
		}
		for _, key := range f.spec.Needs {
			delete(g.indexByKey[key], f.id)
			if len(g.indexByKey[key]) == 0 {
				delete(g.indexByKey, key)
			}
		}
		delete(g.fibers, f.id)
		return
	}
	// Failures are revision-sticky; service arrival/removal is not a retry.
	if f.failed && f.failedAt == f.revision {
		g.stateLocked(f, Failed)
		return
	}
	bindings, epoch, ready := g.bindingsLocked(f)
	if !ready {
		g.stateLocked(f, Pending)
		return
	}
	if blocked := f.blocked; blocked != nil && blocked.revision == f.revision && blocked.notice == f.notice && graphSameEpoch(blocked.epoch, epoch) {
		g.stateLocked(f, Pending)
		return
	}
	g.startLoadLocked(f, bindings, epoch)
}

func (g *GraphLock) wakeKeyLocked(key string) {
	ids := make([]uint64, 0, len(g.indexByKey[key]))
	for id := range g.indexByKey[key] {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		g.reconcileLocked(g.indexByKey[key][id])
	}
}

func (g *GraphLock) startLoadLocked(f *graphFiber, bindings map[string]provision, epoch []uint64) {
	std, cancel := context.WithCancel(context.Background())
	a := &graphActivation{
		fiber: f, cancel: cancel, revision: f.revision, epoch: epoch, checked: f.notice,
		provided: make(map[string]provision), loading: true,
		leaseWake: make(chan struct{}, 1),
	}
	c := &Context{std: std, fiber: f.id, bindings: bindings}
	c.leaseChanged = func() {
		select {
		case a.leaseWake <- struct{}{}:
		default:
		}
	}
	c.provide = func(key string, value any) error { return g.provide(a, key, value) }
	c.effect = func(label string, dispose Disposer) error { return g.deferEffect(a, label, dispose) }
	c.dispose = func() error {
		g.mu.Lock()
		defer g.mu.Unlock()
		// A captured old context cannot dispose a replacement activation.
		if f.current != a || a.retiring || !c.live.Load() {
			return ErrInactive
		}
		g.requestDisposeLocked(f)
		return nil
	}
	a.ctx = c
	c.live.Store(true)
	f.current = a
	f.epoch = append([]uint64(nil), epoch...)
	f.lastErr = nil
	f.worker = true
	g.stateLocked(f, Loading)
	a.loadTimer = time.AfterFunc(g.knobs.load, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if f.current != a || !a.loading {
			return
		}
		deadlineErr := errors.New("load deadline exceeded; still awaiting callback")
		if !a.retiring {
			f.failedAt = a.revision
			f.failed = true
			f.awaitErr = deadlineErr
		}
		f.lastErr = deadlineErr
		g.eventLocked(f, "deadline:load", "", 0)
		g.retireLocked(a)
	})
	go g.runLoad(a)
}

func (g *GraphLock) provide(a *graphActivation, key string, value any) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	f := a.fiber
	if f.current != a || a.retiring || !a.ctx.live.Load() {
		return ErrInactive
	}
	declared := false
	for _, candidate := range f.spec.Provides {
		if candidate == key {
			declared = true
			break
		}
	}
	if !declared {
		return fmt.Errorf("undeclared provision %q", key)
	}
	if _, exists := a.provided[key]; exists {
		return fmt.Errorf("duplicate provision %q", key)
	}
	if _, exists := g.services[key]; exists {
		return fmt.Errorf("provision %q is already owned", key)
	}
	g.nextUID++
	p := provision{uid: g.nextUID, owner: f.id, value: value}
	a.provided[key] = p
	// Reservation is immediate; bindingsLocked hides it until ACTIVE.
	g.services[key] = graphService{provision: p, activation: a}
	g.eventLocked(f, "provide", key, p.uid)
	if !a.loading {
		// Synchronous consumer transitions precede the service notification.
		// Check callbacks still run off-lock and can complete after this event.
		g.wakeKeyLocked(key)
		g.serviceEventLocked(f, "service:publish", key, p)
	}
	return nil
}

func (g *GraphLock) deferEffect(a *graphActivation, label string, dispose Disposer) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if a.fiber.current != a || a.retiring || !a.ctx.live.Load() {
		return ErrInactive
	}
	if dispose == nil {
		return errors.New("nil disposer")
	}
	a.effects = append(a.effects, graphEffect{label: label, dispose: dispose})
	g.eventLocked(a.fiber, "effect:add", label, 0)
	return nil
}

func (g *GraphLock) retireLocked(a *graphActivation) {
	if a.retiring {
		return
	}
	a.retiring = true
	// Invalidate before canceling: reentrant calls cannot acquire new leases.
	a.ctx.live.Store(false)
	a.cancel()
	g.stateLocked(a.fiber, Unloading)
	g.eventLocked(a.fiber, "invalidate", "", 0)
	for _, key := range a.fiber.spec.Provides {
		if _, exists := a.provided[key]; exists {
			g.wakeKeyLocked(key)
		}
	}
	if a.loading {
		// The absolute load timer still reports an old Apply that ignores
		// cancellation, but must not mark the newer target as failed.
		return
	}
	if a.loadTimer != nil {
		a.loadTimer.Stop()
	}
	if !a.fiber.worker {
		a.fiber.worker = true
		go g.runUnload(a)
	}
}

func graphCallApply(apply func(*Context) error, ctx *Context) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("Apply panic: %v", recovered)
		}
	}()
	if apply != nil {
		return apply(ctx)
	}
	return nil
}

func graphCallDispose(dispose Disposer, ctx context.Context) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("disposer panic: %v", recovered)
		}
	}()
	return dispose(ctx)
}

// graphCallCheck supplies a read-only, consumer-bound view. A predicate may
// read its captured providers, but cannot register effects, publish, self-
// dispose, or hold a lease that would obstruct its own availability change.
func graphCallCheck(check func(*Context) bool, a *graphActivation) (available bool, err error) {
	c := &Context{std: a.ctx.std, fiber: a.ctx.fiber, bindings: a.ctx.bindings, dispose: func() error { return ErrInactive }}
	defer c.released.Store(true)
	defer func() {
		if recovered := recover(); recovered != nil {
			available = false
			err = fmt.Errorf("availability panic: %v", recovered)
		}
	}()
	return check(c), nil
}

// checkAvailability revalidates every off-lock callback result under mu. It
// repeats only when an explicit Notify superseded the in-flight predicate;
// denied targets are cached by revision, dependency UID epoch, and notice.
func (g *GraphLock) checkAvailability(a *graphActivation, force bool) bool {
	for {
		g.mu.Lock()
		f := a.fiber
		if a.retiring || !g.targetMatchesLocked(a) {
			g.retireLocked(a)
			g.mu.Unlock()
			return false
		}
		notice := f.notice
		if f.spec.Check == nil || (!force && a.checked == notice) {
			a.checked = notice
			g.mu.Unlock()
			return true
		}
		g.mu.Unlock()
		available, err := graphCallCheck(f.spec.Check, a)
		g.mu.Lock()
		if a.retiring || !g.targetMatchesLocked(a) {
			g.retireLocked(a)
			g.mu.Unlock()
			return false
		}
		if notice != f.notice {
			g.eventLocked(f, "availability:stale", "", 0)
			g.mu.Unlock()
			continue
		}
		a.checked = notice
		if !available {
			f.blocked = &graphBlocked{revision: a.revision, epoch: append([]uint64(nil), a.epoch...), notice: notice}
			if err != nil {
				f.lastErr = err
			}
			g.eventLocked(f, "availability:denied", "", 0)
			g.retireLocked(a)
			g.mu.Unlock()
			return false
		}
		f.blocked = nil
		g.eventLocked(f, "availability:accepted", "", 0)
		g.mu.Unlock()
		return true
	}
}

func (g *GraphLock) runActiveCheck(a *graphActivation) {
	g.checkAvailability(a, false)
	g.mu.Lock()
	if a.retiring {
		g.mu.Unlock()
		g.runUnload(a)
		return
	}
	a.fiber.worker = false
	// A Notify may land after the last check and before this lock acquisition.
	g.reconcileLocked(a.fiber)
	g.signalLocked()
	g.mu.Unlock()
}

func (g *GraphLock) runLoad(a *graphActivation) {
	var err error
	if g.checkAvailability(a, true) {
		err = graphCallApply(a.fiber.spec.Apply, a.ctx)
		if err == nil {
			g.checkAvailability(a, false)
		}
	}
	g.mu.Lock()
	for !a.retiring && err == nil && a.fiber.spec.Check != nil && a.checked != a.fiber.notice {
		g.mu.Unlock()
		g.checkAvailability(a, false)
		g.mu.Lock()
	}
	a.loading = false
	if a.loadTimer != nil {
		a.loadTimer.Stop()
	}
	if !a.retiring && !g.targetMatchesLocked(a) {
		g.retireLocked(a)
	}
	if !a.retiring && err != nil {
		a.fiber.failedAt = a.revision
		a.fiber.failed = true
		a.fiber.lastErr = err
		a.fiber.awaitErr = err
		g.eventLocked(a.fiber, "load:failed", "", 0)
		g.retireLocked(a)
	} else if a.retiring && err != nil {
		// A stale result is evidence, not a failure of the newer target.
		g.eventLocked(a.fiber, "load:stale", "", 0)
	}
	if a.retiring {
		g.mu.Unlock()
		g.runUnload(a)
		return
	}
	g.stateLocked(a.fiber, Active)
	a.fiber.worker = false
	for _, key := range a.fiber.spec.Provides {
		if p, exists := a.provided[key]; exists {
			g.services[key] = graphService{provision: p, activation: a}
			// Preserve the real synchronous recursive-notify order; do not move
			// off-lock availability results into an invented earlier position.
			g.wakeKeyLocked(key)
			g.serviceEventLocked(a.fiber, "service:publish", key, p)
		}
	}
	g.signalLocked()
	g.mu.Unlock()
}

// A provider keeps its effects and identity until every activation holding one
// of its provision UIDs has fully unloaded, not merely canceled its context.
func (g *GraphLock) hasDependantsLocked(a *graphActivation) bool {
	for key, provided := range a.provided {
		for _, dependant := range g.indexByKey[key] {
			if dependant.current == nil {
				continue
			}
			if bound, exists := dependant.current.ctx.bindings[key]; exists && bound.uid == provided.uid {
				return true
			}
		}
	}
	return false
}

func (g *GraphLock) awaitDependants(a *graphActivation) {
	for {
		g.mu.Lock()
		if !g.hasDependantsLocked(a) {
			g.mu.Unlock()
			return
		}
		changed := g.changed
		g.mu.Unlock()
		<-changed
	}
}

func (g *GraphLock) awaitLeases(a *graphActivation) {
	timer := time.NewTimer(g.knobs.leaseDrain)
	defer timer.Stop()
	deadline := timer.C
	for a.ctx.lease.Load() > 0 {
		select {
		case <-a.leaseWake:
		case <-deadline:
			g.mu.Lock()
			if a.fiber.current == a && a.ctx.lease.Load() > 0 {
				a.fiber.lastErr = errors.New("lease drain deadline exceeded; still awaiting leases")
				g.eventLocked(a.fiber, "deadline:lease", "", 0)
			}
			g.mu.Unlock()
			// Timing out never permits reclamation of still-leased effects.
			deadline = nil
		}
	}
}

func (g *GraphLock) runUnload(a *graphActivation) {
	g.awaitDependants(a)
	g.awaitLeases(a)
	g.mu.Lock()
	effects := append([]graphEffect(nil), a.effects...)
	g.mu.Unlock()
	cleanupCtx, cancel := context.WithTimeout(context.Background(), g.knobs.cleanup)
	timer := time.AfterFunc(g.knobs.cleanup, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if a.fiber.current == a && a.retiring {
			a.cleanupErr = errors.Join(a.cleanupErr, errors.New("cleanup deadline exceeded; still awaiting disposer"))
			a.fiber.lastErr = errors.Join(a.fiber.lastErr, a.cleanupErr)
			g.eventLocked(a.fiber, "deadline:cleanup", "", 0)
		}
	})
	var disposeErr error
	for i := len(effects) - 1; i >= 0; i-- {
		disposeErr = errors.Join(disposeErr, graphCallDispose(effects[i].dispose, cleanupCtx))
	}
	timer.Stop()
	cancel()
	g.mu.Lock()
	a.cleanupErr = errors.Join(a.cleanupErr, disposeErr)
	if a.cleanupErr != nil {
		a.fiber.lastErr = errors.Join(a.fiber.lastErr, a.cleanupErr)
	}
	// Keep committed reads and the effect list valid throughout the unwind.
	a.ctx.released.Store(true)
	a.effects = nil
	g.eventLocked(a.fiber, "effects:gone", "", 0)
	for _, key := range a.fiber.spec.Provides {
		if provided, exists := a.provided[key]; exists {
			if service, published := g.services[key]; published && service.activation == a {
				delete(g.services, key)
			}
			g.serviceEventLocked(a.fiber, "service:remove", key, provided)
		}
	}
	a.fiber.current = nil
	a.fiber.worker = false
	g.signalLocked()
	g.reconcileLocked(a.fiber)
	for _, key := range a.fiber.spec.Provides {
		if _, exists := a.provided[key]; exists {
			g.wakeKeyLocked(key)
		}
	}
	g.mu.Unlock()
}
