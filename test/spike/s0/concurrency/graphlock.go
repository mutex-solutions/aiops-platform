// SPDX-License-Identifier: Apache-2.0

package concurrency

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// GraphLock is an experimental graph-mutex scheduler, not a production kernel.
// Only active lifecycle/check transitions have per-fiber goroutines; idle fibers
// have none. One shared off-owner observer dispatcher follows the native ledger.
// Context bindings are immutable and Read/Enter/Leave never take the graph lock.
type GraphLock struct {
	mu         sync.Mutex
	fibers     map[uint64]*graphFiber
	root       *graphFiber                  // Lazy native ID-0 owner, never a user fiber or snapshot row.
	retired    *recordArchive[graphRetired] // Bounded archive of recently disposed incarnations (see retention.go).
	retention  Retention
	ledger     *eventLedger
	services   map[string]graphService
	indexByKey map[string]map[uint64]*graphFiber
	nextFiber  uint64
	nextUID    uint64
	totalSeq   uint64
	observers  *nativeObservers
	closed     bool
	batchDepth int
	batchDirty []*graphFiber
	batchSeen  map[uint64]bool
	knobs      graphKnobs
	pendingSvc []*graphPendingService
	// heldSvc counts, per key, the held service events in pendingSvc.
	heldSvc map[string]int
	// A load worker whose fiber needs a key with a held (or being produced)
	// service event starts only after that event: the oracle records it after
	// the fiber's LOADING and before anything its load does. Pending loads are
	// indexed by every key they need, so releasing a key touches only the loads
	// waiting on it rather than rescanning every pending load.
	loadsByKey map[string]map[*graphPendingLoad]struct{}
	loadSeq    uint64
	waking     map[string]int
	busy       int // User fibers with a worker, or LOADING/UNLOADING (see refreshBusyLocked).
	// settleWaiters are the parked Settle calls. They are woken only when
	// settledLocked becomes true (see notifySettleLocked), never per event.
	settleWaiters map[*graphWaiter]struct{}
	// blocked holds the busy fibers that are blocked (see blockedLocked), the root
	// included; nblocked counts the user fibers among them, so settledLocked
	// stays O(1). Settle's diagnostics are built from this set.
	blocked  map[*graphFiber]struct{}
	nblocked int
	// lateClose is set by a Close that returned before the runtime drained; the
	// observer dispatcher is then closed at the moment it drains (see
	// maybeLateCloseLocked).
	lateClose bool
}

// graphWaiter is one parked AwaitFiber or Settle call. The registry that holds
// it removes it when it wakes it, and a woken waiter always re-checks its
// condition under mu, so a wake-up that is later overtaken is harmless.
type graphWaiter struct{ ch chan struct{} }

func newGraphWaiter() *graphWaiter { return &graphWaiter{ch: make(chan struct{}, 1)} }

// wakeWaiters resumes and unregisters every waiter in set. It never blocks and
// allocates nothing.
func wakeWaiters(set map[*graphWaiter]struct{}) {
	for w := range set {
		delete(set, w)
		select {
		case w.ch <- struct{}{}:
		default:
		}
	}
}

type graphPendingLoad struct {
	seq   uint64 // Insertion order: loads released together start in this order.
	needs []string
	start func()
}

// These are design knobs, not retry intervals. Cancellation is immediate;
// deadlines report blocked transitions without pretending effects disappeared.
type graphKnobs struct {
	load       time.Duration
	cleanup    time.Duration
	leaseDrain time.Duration
}

type graphFiber struct {
	id        uint64
	spec      Spec
	state     State
	revision  uint64
	epoch     []uint64
	counted   bool // Included in GraphLock.busy.
	failedAt  uint64
	failed    bool
	lastErr   error
	awaitErr  error // Current startup failure only; cleanup diagnostics are not startup rejection. The caller's own value.
	awaitDiag error // The same failure as the kernel stores and reports it (see graphDiagnostic).
	wantGone  bool
	worker    bool
	current   *graphActivation
	notice    uint64
	blocked   *graphBlocked
	available *graphAvailability // Last accepted off-lock Check result, not the committed activation.
	pre       *graphPre          // In-flight availability check that precedes any load.
	parent    *graphActivation   // Structural owner: this exact parent activation, not its replacement.
	// waiters are the parked AwaitFiber calls on this fiber; they are woken when
	// the fiber stops being busy, not by unrelated events.
	waiters map[*graphWaiter]struct{}
	// blockedCounted: included in GraphLock.blocked (and nblocked for user fibers).
	blockedCounted bool
}

type graphRetired struct {
	snapshot   Snapshot
	diagnostic error // Format caller-defined Error methods only after releasing mu.
	awaitErr   error // Sticky startup-error identity for AwaitFiber after disposal.
}

type graphAvailability struct {
	epoch  []uint64
	notice uint64
}

// graphPre is an availability check run before a consumer leaves PENDING. A
// consumer whose check is false never reaches LOADING, as in the oracle.
type graphPre struct {
	revision uint64
	notice   uint64
	epoch    []uint64
	cancel   context.CancelFunc
	timer    *time.Timer
	done     bool
	holds    []*graphPendingService // Held service events still waiting on this check.
}

// graphPendingService is a service event held until the pre-checks its
// refresh started have been applied, so dependants move first.
type graphPendingService struct {
	event   Event
	pending int // Checks it still waits for; it may be emitted at zero, in queue order.
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
	children   map[uint64]*graphFiber
	tracked    map[uint64]string // Registered work, retained until its real completion.
	nextTrack  uint64
	effects    []graphEffect
	loading    bool
	retiring   bool
	leaseWake  chan struct{}
	loadTimer  *time.Timer
	cleanupErr error
	disposeErr error // Result of the disposer walk, kept while the unload waits for children.
	// An unload that must wait for other fibers owns no goroutine: the activation
	// is parked here (wait) and the event that satisfies it resumes it.
	wait   graphWait
	blame  []graphBlame // Awaited fibers that are blocked right now; nil unless wait != graphWaitNone.
	blamed map[graphBlame]struct{}
	// stalls are the stages of this activation's own callbacks that are past
	// their deadline and have not returned yet, so the fiber is blocked in them.
	stalls      []string
	loadStalled bool
}

// graphWait is what a retiring activation is parked on.
type graphWait uint8

const (
	graphWaitNone       graphWait = iota
	graphWaitDependants           // Until no activation holds one of its provisions.
	graphWaitChildren             // Until every structural child is disposed.
)

func (w graphWait) stage() string {
	if w == graphWaitChildren {
		return "awaiting children"
	}
	return "awaiting dependants"
}

// graphBlame names one fiber an activation is waiting for that is itself
// blocked, and the stage it is blocked in. The waiter reports it; it claims no
// deadline of its own.
type graphBlame struct {
	id    uint64
	kind  string // "dependant" or "child"
	name  string
	stage string
}

type graphEffect struct {
	label        string
	dispose      Disposer
	child        uint64 // Structural membership only; its disposal is requested at this effect's LIFO slot, joined after the walk.
	provisionKey string
	provisionUID uint64       // Native reservation ownership, ordered with ordinary effects.
	scope        *scopeEffect // Exact native scope identity, never identified by label.
}

type graphService struct {
	provision
	activation *graphActivation
}

// NewGraphLock returns the second independent S0 scheduler candidate.
func NewGraphLock() Runtime { return NewGraphLockWith(Retention{}) }

// NewGraphLockWith is NewGraphLock with explicit history bounds (see Retention).
func NewGraphLockWith(retention Retention) Runtime {
	retention = retention.normalized()
	return &GraphLock{
		fibers:        make(map[uint64]*graphFiber),
		retired:       newRecordArchive[graphRetired](retention.ArchiveLimit),
		retention:     retention,
		ledger:        newEventLedger(retention),
		services:      make(map[string]graphService),
		indexByKey:    make(map[string]map[uint64]*graphFiber),
		heldSvc:       make(map[string]int),
		loadsByKey:    make(map[string]map[*graphPendingLoad]struct{}),
		observers:     newNativeObserversWith(retention.ObserverBacklog),
		settleWaiters: make(map[*graphWaiter]struct{}),
		blocked:       make(map[*graphFiber]struct{}),
		knobs: graphKnobs{
			load:       2 * time.Second,
			cleanup:    2 * time.Second,
			leaseDrain: 5 * time.Second,
		},
	}
}

func (g *GraphLock) appendEventLocked(event Event) {
	g.totalSeq++
	event.Sequence = g.totalSeq
	g.ledger.append(event)
	g.observers.append(event)
}

func (g *GraphLock) eventLocked(f *graphFiber, kind, key string, uid uint64) {
	g.appendEventLocked(Event{
		Fiber: f.id, Name: f.spec.ID, Kind: kind, State: f.state,
		Key: key, UID: uid,
	})
}

func (g *GraphLock) serviceEventLocked(f *graphFiber, kind, key string, p provision) {
	g.queueServiceLocked(Event{
		Fiber: f.id, Name: f.spec.ID, Kind: kind, State: f.state,
		Key: key, UID: p.uid, Value: p.value,
	}, nil)
}

// queueServiceLocked appends a service event now, or behind earlier held ones.
func (g *GraphLock) queueServiceLocked(e Event, waits []*graphPre) {
	if len(waits) == 0 && len(g.pendingSvc) == 0 {
		g.appendEventLocked(e)
		return
	}
	held := &graphPendingService{event: e}
	for _, pre := range waits {
		if !pre.done {
			pre.holds = append(pre.holds, held)
			held.pending++
		}
	}
	g.pendingSvc = append(g.pendingSvc, held)
	g.heldSvc[e.Key]++
}

// flushServicesLocked emits held service events in order once their checks
// ended, then releases the keys whose last held event went out.
func (g *GraphLock) flushServicesLocked() {
	var released []string
	for len(g.pendingSvc) != 0 && g.pendingSvc[0].pending == 0 {
		head := g.pendingSvc[0]
		g.pendingSvc[0] = nil
		g.pendingSvc = g.pendingSvc[1:]
		g.appendEventLocked(head.event)
		if g.heldSvc[head.event.Key]--; g.heldSvc[head.event.Key] == 0 {
			delete(g.heldSvc, head.event.Key)
		}
		released = append(released, head.event.Key)
	}
	g.releaseLoadsLocked(released)
}

func (g *GraphLock) serviceHeldLocked(keys []string) bool {
	for _, k := range keys {
		if g.waking[k] != 0 || g.heldSvc[k] != 0 {
			return true
		}
	}
	return false
}

// holdLoadLocked parks a load worker until none of its needed keys is held.
func (g *GraphLock) holdLoadLocked(needs []string, start func()) {
	g.loadSeq++
	l := &graphPendingLoad{seq: g.loadSeq, needs: needs, start: start}
	for _, k := range needs {
		if g.loadsByKey[k] == nil {
			g.loadsByKey[k] = make(map[*graphPendingLoad]struct{})
		}
		g.loadsByKey[k][l] = struct{}{}
	}
}

// releaseLoadsLocked starts, in insertion order, the pending loads waiting on
// one of keys whose needed keys are all free now. A load becomes startable only
// when a key it needs stops being held, so only those keys are examined.
func (g *GraphLock) releaseLoadsLocked(keys []string) {
	var ready []*graphPendingLoad
	var seen map[*graphPendingLoad]struct{} // Only needed when a load is indexed under several released keys.
	if len(keys) > 1 {
		seen = make(map[*graphPendingLoad]struct{})
	}
	for _, k := range keys {
		for l := range g.loadsByKey[k] {
			if g.serviceHeldLocked(l.needs) {
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
	slices.SortFunc(ready, func(a, b *graphPendingLoad) int { return cmp.Compare(a.seq, b.seq) })
	for _, l := range ready {
		for _, k := range l.needs {
			delete(g.loadsByKey[k], l)
			if len(g.loadsByKey[k]) == 0 {
				delete(g.loadsByKey, k)
			}
		}
	}
	for _, l := range ready {
		l.start()
	}
}

// wakeServiceLocked refreshes the dependants of keys, then records the service
// events. A refresh may start off-lock pre-checks; the events wait for them.
func (g *GraphLock) wakeServiceLocked(keys []string, events []Event) {
	before := make(map[*graphPre]bool)
	for _, key := range keys {
		for _, d := range g.indexByKey[key] {
			if d.pre != nil {
				before[d.pre] = true
			}
		}
	}
	if g.waking == nil {
		g.waking = make(map[string]int)
	}
	for _, key := range keys {
		g.waking[key]++
		g.wakeKeyLocked(key)
	}
	var waits []*graphPre
	for _, key := range keys {
		for _, d := range g.indexByKey[key] {
			if d.pre != nil && !before[d.pre] {
				waits = append(waits, d.pre)
			}
		}
	}
	for _, e := range events {
		g.queueServiceLocked(e, waits)
		waits = nil // Later events follow the first in queue order.
	}
	for _, key := range keys {
		if g.waking[key]--; g.waking[key] == 0 {
			delete(g.waking, key)
		}
	}
	g.releaseLoadsLocked(keys)
}

// busyLocked is the Settle/AwaitFiber notion of unsettled lifecycle work.
func busyLocked(f *graphFiber) bool {
	return f.worker || f.state == Loading || f.state == Unloading
}

// blockedLocked reports whether f is blocked: its current activation has a
// callback past one of its own deadlines that has not returned (a stall), or it
// is parked behind a fiber that is blocked (blame). A blocked fiber is still
// busy, but Settle does not wait for it.
func blockedLocked(f *graphFiber) bool {
	a := f.current
	return a != nil && (len(a.stalls) != 0 || len(a.blame) != 0)
}

// settledLocked is the Settle condition: every fiber that is mid-transition,
// the trusted root included, is blocked.
func (g *GraphLock) settledLocked() bool {
	return g.busy == g.nblocked && (g.root == nil || !busyLocked(g.root) || g.root.blockedCounted)
}

// drainedLocked reports that a closed runtime has no fiber left.
func (g *GraphLock) drainedLocked() bool {
	return g.closed && len(g.fibers) == 0 && (g.root == nil || g.root.state == Disposed)
}

// notifySettleLocked resumes the parked Settle calls once, when their
// condition holds, and closes the observer dispatcher of a Close that returned
// early once the runtime has drained. With no waiter and no late close pending
// it is two comparisons and allocates nothing.
func (g *GraphLock) notifySettleLocked() {
	if len(g.settleWaiters) != 0 && g.settledLocked() {
		wakeWaiters(g.settleWaiters)
	}
	g.maybeLateCloseLocked()
}

// maybeLateCloseLocked is the equivalent of the reconciler's one-time late join:
// a Close that gave up (its context ended, or a fiber was blocked) leaves the
// runtime draining, and nobody may call Close again, so the dispatcher is closed
// here when the last fiber is gone. It never parks a goroutine to wait for that.
func (g *GraphLock) maybeLateCloseLocked() {
	if g.lateClose && g.drainedLocked() {
		g.lateClose = false
		go func() { _ = g.observers.close(context.Background()) }()
	}
}

// refreshBusyLocked keeps g.busy and g.nblocked equal to the number of user
// fibers for which busyLocked, and busyLocked together with blockedLocked, hold,
// so Settle is O(1) per check instead of a scan of every fiber, and wakes
// exactly the waiters whose condition this change can satisfy: the AwaitFiber
// calls on f when f stops being busy, and the Settle calls when every busy
// fiber is blocked. An event that changes neither wakes nobody. Call it after
// any change to worker, state, stalls or blame.
func (g *GraphLock) refreshBusyLocked(f *graphFiber) {
	busy := busyLocked(f)
	if f != g.root { // The trusted root is not in g.fibers; settledLocked checks it separately.
		if busy != f.counted {
			f.counted = busy
			if busy {
				g.busy++
			} else {
				g.busy--
				wakeWaiters(f.waiters)
			}
		}
	}
	if blocked := busy && blockedLocked(f); blocked != f.blockedCounted {
		f.blockedCounted = blocked
		d := 1
		if blocked {
			g.blocked[f] = struct{}{}
		} else {
			delete(g.blocked, f)
			d = -1
		}
		if f != g.root {
			g.nblocked += d
		}
	}
	g.notifySettleLocked()
}

func (g *GraphLock) setWorkerLocked(f *graphFiber, worker bool) {
	f.worker = worker
	g.refreshBusyLocked(f)
}

func (g *GraphLock) stateLocked(f *graphFiber, state State) {
	if f.state != state {
		from := f.state
		f.state = state
		g.appendEventLocked(Event{
			Fiber: f.id, Name: f.spec.ID, Kind: "state", From: from, State: state,
		})
		// After the event: refreshing can close the observer dispatcher of a
		// drained runtime (maybeLateCloseLocked), and no event may follow that.
		g.refreshBusyLocked(f)
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

// RootContext returns the runtime's one trusted native composition context. It
// is not mounted as a user plugin, does not consume a user ID, and starts no idle
// worker. Builtin provision values are supplied by the composition caller.
func (g *GraphLock) RootContext(ctx context.Context) (*Context, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil, ErrClosed
	}
	if g.root == nil {
		f := &graphFiber{spec: Spec{ID: "root"}, state: Active, revision: 1}
		g.root = f
		a := g.activationLocked(f, make(map[string]provision), nil)
		a.ctx.dispose = func() error { return ErrRootDispose }
	} else if g.root.current == nil || g.root.current.retiring || !g.root.current.ctx.live.Load() {
		return nil, ErrInactive
	}
	return g.root.current.ctx, nil
}

func (g *GraphLock) Mount(ctx context.Context, spec Spec) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.mountLocked(spec, nil)
}

// mountLocked is shared by roots and native children. No user callback runs
// here. A child is registered as owned before it can start its own worker.
func (g *GraphLock) mountLocked(spec Spec, parent *graphActivation) (uint64, error) {
	if !graphUnique(spec.Needs) || !graphUnique(spec.Provides) {
		return 0, errors.New("needs/provides must contain unique nonempty keys")
	}
	if spec.Apply != nil && spec.ApplyEffect != nil {
		return 0, errors.New("Apply and ApplyEffect are mutually exclusive")
	}
	if g.closed {
		return 0, ErrClosed
	}
	// The declaration is owned by the scheduler after Mount/MountChild returns.
	spec.Needs = append([]string(nil), spec.Needs...)
	spec.Provides = append([]string(nil), spec.Provides...)
	return g.mountPreparedLocked(spec, parent), nil
}

// mountPreparedLocked accepts scheduler-owned, already validated declarations.
// It is the non-failing commit shared by ordinary Mount and the Batch prefix.
func (g *GraphLock) mountPreparedLocked(spec Spec, parent *graphActivation) uint64 {
	g.nextFiber++
	f := &graphFiber{id: g.nextFiber, spec: spec, state: Pending, parent: parent}
	g.fibers[f.id] = f
	if parent != nil {
		if parent.children == nil {
			parent.children = make(map[uint64]*graphFiber)
		}
		parent.children[f.id] = f
		parent.effects = append(parent.effects, graphEffect{label: "child", child: f.id})
		g.eventLocked(parent.fiber, "effect:add", "child", 0)
	}
	for _, key := range spec.Needs {
		if g.indexByKey[key] == nil {
			g.indexByKey[key] = make(map[uint64]*graphFiber)
		}
		g.indexByKey[key][f.id] = f
	}
	g.eventLocked(f, "mount", "", 0)
	g.reconcileLocked(f)
	return f.id
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
	f.awaitErr, f.awaitDiag = nil, nil
	g.eventLocked(f, "update", "", 0)
	g.reconcileLocked(f)
	return nil
}

// Batch commits data-only desired changes before any new lifecycle/check work
// is admitted. All validation, including the last operation, precedes IDs,
// revisions, disposal requests, and actual ledger events.
func (g *GraphLock) Batch(ctx context.Context, operations []BatchOperation) ([]uint64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil, ErrClosed
	}
	prepared, err := prepareBatch(operations, func(id uint64) bool {
		f := g.fibers[id]
		return f != nil && !f.wantGone
	})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ids := make([]uint64, len(prepared))
	g.batchDepth++
	g.batchSeen = make(map[uint64]bool)
	for i, operation := range prepared {
		switch operation.Kind {
		case BatchMount:
			ids[i] = g.mountPreparedLocked(operation.Spec, nil)
		case BatchUpdate:
			f := g.fibers[operation.Fiber]
			f.revision++
			f.lastErr, f.awaitErr, f.awaitDiag = nil, nil, nil
			g.eventLocked(f, "update", "", 0)
			g.reconcileLocked(f)
		case BatchDispose:
			g.requestDisposeLocked(g.fibers[operation.Fiber])
		}
	}
	// A cold consumer may precede its provider's Update in the input. Invalidate
	// the entire old-target closure first, while reconciliation is still deferred;
	// only afterward can that consumer bind a currently ACTIVE reservation.
	for i := 0; i < len(g.batchDirty); i++ {
		f := g.batchDirty[i]
		if a := f.current; a != nil && !a.retiring && !g.targetMatchesLocked(a) {
			g.retireLocked(a)
		}
	}
	dirty := g.batchDirty
	g.batchDirty, g.batchSeen = nil, nil
	g.batchDepth--
	for _, f := range dirty {
		g.reconcileLocked(f)
	}
	return ids, nil
}

// UpdateConfig runs the configuration cell outside the graph owner. Restart is
// a real revision update followed by a fiber-local join, not an adapter rewrite
// of raw/effective state or a global Settle barrier.
func (g *GraphLock) UpdateConfig(ctx context.Context, id uint64, raw any, noSave bool) error {
	request, err := g.RequestUpdateConfig(ctx, id, raw, noSave)
	if err != nil {
		return err
	}
	return request.Await(ctx)
}

func (g *GraphLock) RequestUpdateConfig(ctx context.Context, id uint64, raw any, noSave bool) (ConfigRequest, error) {
	request := ConfigRequest{Await: func(context.Context) error { return nil }}
	if err := ctx.Err(); err != nil {
		return request, err
	}
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return request, ErrClosed
	}
	f, ok := g.fibers[id]
	if !ok {
		disposed := g.issuedLocked(id) // Archived or expired: either way no longer live.
		g.mu.Unlock()
		if disposed {
			return request, ErrInactive
		}
		return request, ErrUnknown
	}
	if f.wantGone {
		g.mu.Unlock()
		return request, ErrInactive
	}
	cell := f.spec.Config
	if cell == nil {
		g.mu.Unlock()
		return request, errors.New("fiber has no configuration cell")
	}
	committed := f.current
	target := g.targetLocked(f)
	revision := f.revision
	g.mu.Unlock()
	var restarted atomic.Bool
	wait, err := cell.RequestApplyUpdate(ctx, raw, noSave, target != nil,
		func() error {
			if err := g.Update(ctx, id); err != nil {
				return err
			}
			restarted.Store(true)
			return nil
		},
		func() bool {
			g.mu.Lock()
			defer g.mu.Unlock()
			return g.fibers[id] == f && !g.closed && !f.wantGone && f.current == committed && f.revision == revision && graphSameTarget(g.targetLocked(f), target)
		},
		func() error {
			g.mu.Lock()
			defer g.mu.Unlock()
			if g.fibers[id] != f || f.wantGone {
				return ErrInactive
			}
			if g.closed {
				return ErrClosed
			}
			f.failed = false
			f.lastErr = nil
			f.awaitErr, f.awaitDiag = nil, nil
			return nil
		})
	request.Await = func(w context.Context) error {
		if wait != nil {
			if err := wait(w); err != nil {
				return err
			}
		}
		if restarted.Load() {
			return g.AwaitFiber(w, id)
		}
		return nil
	}
	return request, err
}

func (g *GraphLock) Dispose(ctx context.Context, id uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	f, ok := g.fibers[id]
	if !ok {
		if g.issuedLocked(id) {
			return nil // Disposed, archived or expired: idempotent.
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
	before := make(map[*graphPre]bool)
	for _, f := range selected {
		if f.pre != nil {
			before[f.pre] = true
		}
	}
	for _, id := range ids {
		f := selected[id]
		if f.spec.Check == nil || f.wantGone {
			continue
		}
		f.notice++
		g.eventLocked(f, "availability:notify", "", 0)
		g.reconcileLocked(f)
	}
	// The oracle's notify records one service event per key with the current
	// provision (any state), after the dependants were refreshed. Pre-checks
	// that the refresh started are applied first.
	var waits []*graphPre
	for _, id := range ids {
		if pre := selected[id].pre; pre != nil && !before[pre] {
			waits = append(waits, pre)
		}
	}
	for _, key := range keys {
		if service, ok := g.services[key]; ok {
			owner := service.activation.fiber
			g.queueServiceLocked(Event{Fiber: owner.id, Name: owner.spec.ID, Kind: "service", State: owner.state, Key: key, UID: service.uid, Value: service.value}, waits)
			waits = nil
		}
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

// Settle implements the Runtime.Settle contract (types.go). A fiber counts as
// blocked while one of its callbacks is past one of its own deadlines, or while
// it waits for a dependant or child that is blocked (see blockedLocked).
func (g *GraphLock) Settle(ctx context.Context) error {
	var w *graphWaiter
	for {
		g.mu.Lock()
		if g.settledLocked() {
			diagnostics := g.blockedDiagnosticsLocked()
			g.mu.Unlock()
			return errors.Join(diagnostics...)
		}
		if w == nil {
			w = newGraphWaiter()
		}
		g.settleWaiters[w] = struct{}{}
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			g.mu.Lock()
			delete(g.settleWaiters, w)
			diagnostics := g.blockedDiagnosticsLocked()
			g.mu.Unlock()
			if len(diagnostics) == 0 {
				return ctx.Err()
			}
			return errors.Join(append([]error{ctx.Err()}, diagnostics...)...)
		case <-w.ch:
		}
	}
}

// blockedDiagnosticsLocked returns one error per blocked fiber, in fiber order,
// each naming the fiber and the stage. Only strings the kernel stored are used.
func (g *GraphLock) blockedDiagnosticsLocked() []error {
	if len(g.blocked) == 0 {
		return nil
	}
	fibers := make([]*graphFiber, 0, len(g.blocked))
	for f := range g.blocked {
		fibers = append(fibers, f)
	}
	slices.SortFunc(fibers, func(a, b *graphFiber) int { return cmp.Compare(a.id, b.id) })
	out := make([]error, 0, len(fibers))
	for _, f := range fibers {
		a := f.current
		var parts []string
		for _, stage := range a.stalls {
			parts = append(parts, "blocked past its "+stage+" deadline")
		}
		for _, b := range a.blame {
			parts = append(parts, fmt.Sprintf("%s: %s fiber %d (%s) is blocked in %s", a.wait.stage(), b.kind, b.id, b.name, b.stage))
		}
		out = append(out, fmt.Errorf("fiber %d (%s): %s", f.id, f.spec.ID, strings.Join(parts, "; ")))
	}
	return out
}

// AwaitFiber joins only the target's current lifecycle work. A stable PENDING
// target resolves without claiming it is ACTIVE; current FAILED work rejects
// with its sticky startup error. Disposed targets remain joinable through the
// archive and retain that same startup-error identity, not cleanup diagnostics.
// Like Settle, it must not be used to self-join an executing callback or lease.
func (g *GraphLock) AwaitFiber(ctx context.Context, id uint64) error {
	var held *graphFiber // The waiter's own reference, like a Cordis caller's fiber handle.
	var w *graphWaiter
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		g.mu.Lock()
		f, exists := g.fibers[id]
		if !exists {
			var err error
			switch record, archived := g.retired.get(id); {
			case held != nil:
				err = held.awaitErr // Exact even if the archive evicted the record meanwhile.
			case archived:
				err = record.awaitErr
			case g.issuedLocked(id):
				err = ErrExpired
			default:
				err = ErrUnknown
			}
			g.mu.Unlock()
			return err
		}
		held = f
		if !busyLocked(f) {
			err := f.awaitErr
			g.mu.Unlock()
			return err
		}
		if w == nil {
			w = newGraphWaiter()
		}
		if f.waiters == nil {
			f.waiters = make(map[*graphWaiter]struct{})
		}
		f.waiters[w] = struct{}{}
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			g.mu.Lock()
			delete(f.waiters, w)
			g.mu.Unlock()
			return ctx.Err()
		case <-w.ch:
		}
	}
}

func (g *GraphLock) Snapshots(ctx context.Context) ([]Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	out, diagnostics := g.snapshotsLocked()
	g.mu.Unlock()
	graphFormatDiagnostics(out, diagnostics)
	return out, nil
}

// Inspect observes one graph-lock cut, including reserved LOADING provisions
// and retiring provisions still held by dependants or cleanup. Value preserves
// the caller's actual service identity; callers must keep its contents immutable.
// Neither inspection nor its copies execute checks, marshaling, or service code.
func (g *GraphLock) Inspect(ctx context.Context) (Inspection, error) {
	if err := ctx.Err(); err != nil {
		return Inspection{}, err
	}
	g.mu.Lock()
	fibers, diagnostics := g.snapshotsLocked()
	provisions := make([]ProvisionSnapshot, 0, len(g.services))
	for key, service := range g.services {
		a := service.activation
		provisions = append(provisions, ProvisionSnapshot{
			Key: key, UID: service.uid, Owner: service.owner, Name: a.fiber.spec.ID,
			Value: service.value, Active: !a.retiring && a.fiber.state == Active,
		})
	}
	sequence := g.totalSeq
	g.mu.Unlock()
	// UID is assigned at the real successful reservation, not at publication or
	// semantic service notification (which can follow a different key order).
	sort.Slice(provisions, func(i, j int) bool { return provisions[i].UID < provisions[j].UID })
	graphFormatDiagnostics(fibers, diagnostics)
	return Inspection{Sequence: sequence, Fibers: fibers, Provisions: provisions}, nil
}

func (g *GraphLock) snapshotsLocked() ([]Snapshot, []error) {
	ids := make([]uint64, 0, len(g.fibers)+int(g.retired.n.Load()))
	for id := range g.fibers {
		ids = append(ids, id)
	}
	g.retired.each(func(id uint64, _ graphRetired) { ids = append(ids, id) })
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]Snapshot, 0, len(ids))
	diagnostics := make([]error, 0, len(ids))
	for _, id := range ids {
		if archived, disposed := g.retired.get(id); disposed {
			s := archived.snapshot
			s.Epoch = append([]uint64(nil), s.Epoch...)
			out = append(out, s)
			diagnostics = append(diagnostics, archived.diagnostic)
			continue
		}
		f := g.fibers[id]
		s := Snapshot{
			ID: id, Name: f.spec.ID, State: f.state, Revision: f.revision,
			Epoch: append([]uint64(nil), f.epoch...), Alive: !f.wantGone,
			Target: g.targetLocked(f),
		}
		graphCaptureConfig(&s, f.spec.Config)
		if f.current != nil {
			for _, effect := range f.current.effects {
				s.Effects = append(s.Effects, effect.label)
				if effect.scope != nil {
					s.EffectTree = append(s.EffectTree, effect.scope.metadata())
				} else {
					s.EffectTree = append(s.EffectTree, EffectMeta{Label: effect.label, State: ScopeReady})
				}
			}
			tokens := make([]uint64, 0, len(f.current.tracked))
			for token := range f.current.tracked {
				tokens = append(tokens, token)
			}
			sort.Slice(tokens, func(i, j int) bool { return tokens[i] < tokens[j] })
			for _, token := range tokens {
				s.Tracked = append(s.Tracked, f.current.tracked[token])
			}
		}
		out = append(out, s)
		diagnostics = append(diagnostics, f.lastErr)
	}
	return out, diagnostics
}

// Snapshot on the cell only copies immutable identities under its own mutex;
// no cell operation calls graph code while holding that mutex. The archive
// retains this value-only view, never the cell or its caller-supplied hooks.
func graphCaptureConfig(snapshot *Snapshot, cell *Configuration) {
	if cell == nil {
		return
	}
	configuration := cell.Snapshot()
	snapshot.Configured = true
	snapshot.ConfigPrepared = configuration.Prepared
	snapshot.RawConfig = configuration.Raw
	snapshot.EffectiveConfig = configuration.Effective
	snapshot.ConfigVersion = configuration.Version
}

// Error is a caller-defined method too. Capture error identities with the graph
// state, but never format them while holding the graph mutex.
func graphFormatDiagnostics(snapshots []Snapshot, diagnostics []error) {
	for i, diagnostic := range diagnostics {
		if diagnostic != nil {
			snapshots[i].Error = diagnostic.Error()
		}
	}
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
	out, ledgerErr := g.ledger.events()
	g.mu.Unlock()
	// Payloads are immutable after append, so readers can copy off-lock too.
	// Service Value retains its actual identity rather than invoking user code.
	for i := range out {
		if out[i].Data != nil {
			out[i].Data = graphCopyJSONValue(out[i].Data).(map[string]any)
		}
	}
	return out, ledgerErr
}

// RetentionStatus reports the history bounds and their use without the graph lock.
func (g *GraphLock) RetentionStatus() RetentionStatus {
	return retentionStatus(g.retention, g.retired, g.ledger)
}

// issuedLocked reports whether id was ever issued to a user fiber. IDs are
// monotonic and never reused, so an issued id that is not live is disposed,
// whether or not its archive record is still held.
func (g *GraphLock) issuedLocked(id uint64) bool { return id != 0 && id <= g.nextFiber }

// ObserveEvents registers activation-owned observation, never a callback on the
// graph owner. The helper owns its subscription lease and Defer registration, so
// this method must not hold mu while it runs.
func (g *GraphLock) ObserveEventsWith(owner *Context, label string, fn func(context.Context, Event) error, onOverflow func(context.Context, *ObserverOverflow) error) (Disposer, error) {
	if owner == nil || owner.host != g {
		return nil, ErrObserverBinding
	}
	return g.observers.observeWith(owner, label, fn, onOverflow)
}

// ObserveEvents registers activation-owned observation (see ObserveEventsWith).
func (g *GraphLock) ObserveEvents(owner *Context, label string, fn func(context.Context, Event) error) (Disposer, error) {
	if owner == nil || owner.host != g {
		return nil, ErrObserverBinding
	}
	return g.observers.observe(owner, label, fn)
}

// AwaitObservers is an explicit off-owner barrier, intentionally not Settle.
func (g *GraphLock) AwaitObservers(ctx context.Context) error { return g.observers.await(ctx) }

func (g *GraphLock) ObserverStatus() ObserverStatus { return g.observers.status() }

func (g *GraphLock) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	g.mu.Lock()
	g.closed = true
	// Quiesce every user first, including roots not mounted through MountChild.
	// The native root then retains its provisions until all bound consumers and
	// structural descendants have finished their actual teardown.
	ids := make([]uint64, 0, len(g.fibers))
	for id := range g.fibers {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if f := g.fibers[id]; f != nil {
			g.requestDisposeLocked(f)
		}
	}
	if g.root != nil {
		g.root.wantGone = true
		if g.root.current != nil {
			g.retireLocked(g.root.current)
		} else {
			g.reconcileLocked(g.root)
		}
	}
	g.notifySettleLocked()
	g.mu.Unlock()
	if err := g.Settle(ctx); err != nil {
		// Close gives up while the runtime may still be draining (the context
		// ended, or a fiber is blocked past a deadline) and nobody may call it
		// again: close the observer dispatcher when the last fiber is gone.
		g.mu.Lock()
		g.lateClose = true
		g.maybeLateCloseLocked()
		g.mu.Unlock()
		return err
	}
	g.mu.Lock()
	var rootErr error
	if g.root != nil {
		rootErr = errors.Join(g.root.awaitDiag, g.root.lastErr)
	}
	g.mu.Unlock()
	// All actual lifecycle events have now been published. Even a failed root
	// must drain and close the dispatcher; a lifecycle timeout above must not.
	return errors.Join(rootErr, g.ledger.failure(), g.observers.close(ctx))
}

// Transition machinery below deliberately never calls Apply or a disposer
// while holding mu. A retiring activation remains the sole current writer
// until all its callbacks and effects really finish.

var _ Runtime = (*GraphLock)(nil)
var _ Notifier = (*GraphLock)(nil)
var _ Waiter = (*GraphLock)(nil)
var _ Recorder = (*GraphLock)(nil)
var _ Inspector = (*GraphLock)(nil)
var _ ConfigUpdater = (*GraphLock)(nil)
var _ RootProvider = (*GraphLock)(nil)
var _ OverflowObserverRuntime = (*GraphLock)(nil)
var _ OverflowObserverRuntime = (*reconcilerRuntime)(nil)
var _ RetentionReporter = (*GraphLock)(nil)
var _ Batcher = (*GraphLock)(nil)

// targetLocked describes the desired future activation, not the activation
// retained during unload. Availability is resolved solely from current ACTIVE
// provider reservations and already accepted off-lock predicate results. A new
// or notified predicate has no target until that result is accepted; inspection
// itself must not run it. A config-only revision retains accepted binding truth.
func (g *GraphLock) targetLocked(f *graphFiber) *EpochSnapshot {
	if f.wantGone || (f.failed && f.failedAt == f.revision) {
		return nil
	}
	epoch := make([]uint64, 0, len(f.spec.Needs))
	for _, key := range f.spec.Needs {
		service, ok := g.services[key]
		if !ok || service.activation.retiring || service.activation.fiber.state != Active {
			return nil
		}
		epoch = append(epoch, service.uid)
	}
	if f.spec.Check != nil {
		accepted := f.available
		if accepted == nil || accepted.notice != f.notice || !graphSameEpoch(accepted.epoch, epoch) {
			return nil
		}
	}
	return &EpochSnapshot{Revision: f.revision, UIDs: epoch}
}

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

func graphSameTarget(a, b *EpochSnapshot) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Revision == b.Revision && graphSameEpoch(a.UIDs, b.UIDs)
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
	if g.batchDepth != 0 {
		if !g.batchSeen[f.id] {
			g.batchSeen[f.id] = true
			g.batchDirty = append(g.batchDirty, f)
		}
		return
	}
	if f == g.root {
		// Root has no plugin body, retry path, user registry entry, or archive.
		// Its same native activation is terminal after failure or Close.
		if f.current == nil {
			if f.wantGone {
				g.stateLocked(f, Disposed)
			} else {
				g.stateLocked(f, Failed)
			}
		} else if !f.worker && !f.current.retiring && g.scopeWorkLocked(f.current) {
			g.setWorkerLocked(f, true)
			go g.runScopeLane(f.current)
		}
		return
	}
	if f.state == Disposed {
		return
	}
	if f.current != nil {
		a := f.current
		if !a.retiring && !g.targetMatchesLocked(a) {
			g.retireLocked(a)
		} else if !a.retiring && !a.loading && !f.worker {
			if f.spec.Check != nil && a.checked != f.notice {
				g.setWorkerLocked(f, true)
				go g.runActiveCheck(a)
			} else if g.scopeWorkLocked(a) {
				g.setWorkerLocked(f, true)
				go g.runScopeLane(a)
			}
		}
		return
	}
	if f.wantGone {
		if f.pre != nil {
			// The check is read-only and canceled; its completion reconciles again.
			f.pre.cancel()
			g.stateLocked(f, Unloading)
			return
		}
		if f.state != Unloading {
			// Even a never-activated PENDING fiber has an asynchronous unload
			// boundary. Do not skip directly to DISPOSED or run Apply to make one.
			g.stateLocked(f, Unloading)
			g.setWorkerLocked(f, true)
			go func() {
				g.mu.Lock()
				defer g.mu.Unlock()
				g.setWorkerLocked(f, false)
				g.reconcileLocked(f)
			}()
			return
		}
		if f.worker {
			return
		}
		g.stateLocked(f, Disposed)
		g.eventLocked(f, "dispose:complete", "", 0)
		archive := Snapshot{ID: f.id, Name: f.spec.ID, State: Disposed, Revision: f.revision, Epoch: append([]uint64(nil), f.epoch...)}
		graphCaptureConfig(&archive, f.spec.Config)
		g.retired.put(f.id, graphRetired{snapshot: archive, diagnostic: f.lastErr, awaitErr: f.awaitErr})
		for _, key := range f.spec.Needs {
			delete(g.indexByKey[key], f.id)
			if len(g.indexByKey[key]) == 0 {
				delete(g.indexByKey, key)
			}
		}
		delete(g.fibers, f.id)
		if g.closed && g.root != nil && g.root.current != nil {
			g.resumeLocked(g.root.current)
		}
		g.notifySettleLocked()
		if parent := f.parent; parent != nil {
			delete(parent.children, f.id)
			for i, effect := range parent.effects {
				if effect.child == f.id {
					copy(parent.effects[i:], parent.effects[i+1:])
					parent.effects[len(parent.effects)-1] = graphEffect{}
					parent.effects = parent.effects[:len(parent.effects)-1]
					break
				}
			}
			f.parent = nil
			g.resumeLocked(parent)
		}
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
	if f.spec.Check != nil {
		// A false check keeps the consumer PENDING: it is evaluated before LOADING.
		if f.pre != nil {
			return
		}
		if accepted := f.available; accepted == nil || accepted.notice != f.notice || !graphSameEpoch(accepted.epoch, epoch) {
			g.startPreCheckLocked(f, bindings, epoch)
			return
		}
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

// activationLocked installs the same native Provide/effect/child/tracked/lease
// hooks for a user activation and the trusted root. It executes no caller code.
func (g *GraphLock) activationLocked(f *graphFiber, bindings map[string]provision, epoch []uint64) *graphActivation {
	std, cancel := context.WithCancel(context.Background())
	a := &graphActivation{
		fiber: f, cancel: cancel, revision: f.revision, epoch: epoch, checked: f.notice,
		provided:  make(map[string]provision),
		leaseWake: make(chan struct{}, 1),
	}
	c := &Context{host: g, std: std, fiber: f.id, bindings: bindings}
	c.leaseChanged = func() {
		select {
		case a.leaseWake <- struct{}{}:
		default:
		}
	}
	c.provide = func(key string, value any) error { return g.provide(a, key, value) }
	c.effect = func(label string, dispose Disposer) error { return g.deferEffect(a, label, dispose) }
	c.cleanupEffect = func(dispose Disposer) error { return g.cleanupEffect(a, dispose) }
	c.scopeRequest = func(label string, setup func(*EffectScope) error) (*EffectHandle, error) {
		return g.requestScope(a, label, setup)
	}
	c.trackedStart = func(label string) (func(error), error) { return g.startTracked(a, label) }
	c.child = func(spec Spec) (uint64, error) {
		g.mu.Lock()
		defer g.mu.Unlock()
		if f.current != a || a.retiring || !c.live.Load() {
			return 0, ErrInactive
		}
		return g.mountLocked(spec, a)
	}
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
	return a
}

func (g *GraphLock) startLoadLocked(f *graphFiber, bindings map[string]provision, epoch []uint64) {
	a := g.activationLocked(f, bindings, epoch)
	a.loading = true
	g.setWorkerLocked(f, true)
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
			f.awaitErr, f.awaitDiag = deadlineErr, deadlineErr
		}
		f.lastErr = deadlineErr
		g.eventLocked(f, "deadline:load", "", 0)
		g.retireLocked(a)
		g.stateLocked(f, Unloading)
		a.loadStalled = true
		g.stallLocked(a, "load")
	})
	if g.serviceHeldLocked(f.spec.Needs) {
		g.holdLoadLocked(f.spec.Needs, func() { go g.runLoad(a) })
		return
	}
	go g.runLoad(a)
}

func (g *GraphLock) provide(a *graphActivation, key string, value any) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	f := a.fiber
	if f.current != a || a.retiring || !a.ctx.live.Load() {
		return ErrInactive
	}
	if key == "" {
		return errors.New("provision key must not be empty")
	}
	declared := f == g.root
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
	if f == g.root {
		// The trusted root has no user declaration. Successful reservations
		// become its native ordered removal/notification list, never fixtures.
		f.spec.Provides = append(f.spec.Provides, key)
	}
	g.nextUID++
	p := provision{uid: g.nextUID, owner: f.id, value: value}
	a.provided[key] = p
	a.ctx.publishOwned(key)
	// Reservation is immediate; bindingsLocked hides it until ACTIVE.
	g.services[key] = graphService{provision: p, activation: a}
	label := "provide:" + key
	a.effects = append(a.effects, graphEffect{label: label, provisionKey: key, provisionUID: p.uid})
	g.eventLocked(f, "provide", key, p.uid)
	g.eventLocked(f, "effect:add", label, 0)
	if !a.loading {
		// Synchronous consumer transitions precede the service notification.
		// Check callbacks still run off-lock and can complete after this event.
		g.wakeServiceLocked([]string{key}, []Event{{Fiber: f.id, Name: f.spec.ID, Kind: "service:publish", State: f.state, Key: key, UID: p.uid, Value: p.value}})
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

// requestScope reserves an actual ownership slot before exposing its handle.
// Scope callbacks run only on this activation's existing transition lane.
func (g *GraphLock) requestScope(a *graphActivation, label string, setup func(*EffectScope) error) (*EffectHandle, error) {
	if err := validateScope(label, setup); err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if a.fiber.current != a || a.retiring || !a.ctx.live.Load() {
		return nil, errors.Join(ErrInactiveEffect, ErrInactive)
	}
	effect := newScopeEffect(label, setup)
	effect.requestWithdraw = func() error {
		g.mu.Lock()
		defer g.mu.Unlock()
		if effect.cleanupFinished() {
			return nil
		}
		if a.fiber.current != a {
			return errors.Join(ErrInactiveEffect, ErrInactive)
		}
		if !effect.pendingWithdraw() {
			effect.markWithdraw()
			g.eventLocked(a.fiber, "scope:withdraw", label, 0)
		}
		// A retiring activation already has an owning worker; its unload
		// performs the same scope cleanup rather than starting another worker.
		g.reconcileLocked(a.fiber)
		return nil
	}
	a.effects = append(a.effects, graphEffect{label: label, scope: effect})
	g.eventLocked(a.fiber, "effect:add", label, 0)
	g.reconcileLocked(a.fiber)
	return effect.handle, nil
}

func (g *GraphLock) scopeWorkLocked(a *graphActivation) bool {
	for _, effect := range a.effects {
		if effect.scope != nil && (!effect.scope.setupFinished() || (effect.scope.pendingWithdraw() && !effect.scope.cleanupFinished())) {
			return true
		}
	}
	return false
}

func (g *GraphLock) removeScopeLocked(a *graphActivation, scope *scopeEffect) {
	if !scope.cleanupFinished() {
		return
	}
	for i, effect := range a.effects {
		if effect.scope == scope {
			copy(a.effects[i:], a.effects[i+1:])
			a.effects[len(a.effects)-1] = graphEffect{}
			a.effects = a.effects[:len(a.effects)-1]
			g.eventLocked(a.fiber, "scope:gone", effect.label, 0)
			return
		}
	}
}

// scopeCleanupContext supplies the existing native cleanup budget once per
// scope rollback/withdrawal, not once per nested disposer. Expiry is diagnostic;
// the same transition worker and ownership tree remain until real callbacks exit.
func (g *GraphLock) scopeCleanupContext(a *graphActivation) (context.Context, context.CancelFunc) {
	g.mu.Lock()
	budget := g.knobs.cleanup
	g.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	pending, recorded, stalled := true, false, false // Protected by mu, including the timer.
	recordLocked := func(stall bool) {
		if recorded || a.fiber.current != a {
			return
		}
		recorded = true
		diagnostic := errors.New("scope cleanup deadline exceeded; still awaiting disposer")
		a.cleanupErr = errors.Join(a.cleanupErr, diagnostic)
		a.fiber.lastErr = errors.Join(a.fiber.lastErr, diagnostic)
		g.eventLocked(a.fiber, "deadline:scopeCleanup", "", 0)
		if stall { // The disposer is still running: the fiber is blocked in it.
			stalled = true
			g.stallLocked(a, "scope cleanup")
		}
	}
	timer := time.AfterFunc(budget, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if pending {
			recordLocked(true)
		}
	})
	return ctx, func() {
		timer.Stop()
		g.mu.Lock()
		pending = false
		if ctx.Err() != nil {
			recordLocked(false)
		}
		if stalled {
			stalled = false
			g.unstallLocked(a, "scope cleanup")
		}
		g.mu.Unlock()
		cancel()
	}
}

// runScopeWork is called only by the activation's one transition worker. During
// load it runs after Apply and before ACTIVE; while ACTIVE it uses the same lane
// as availability checks and unload. It never acquires an explicit call lease.
func (g *GraphLock) runScopeWork(a *graphActivation) {
	for {
		g.mu.Lock()
		if a.retiring {
			g.mu.Unlock()
			return
		}
		var selected graphEffect
		for _, effect := range a.effects {
			if effect.scope != nil && (!effect.scope.setupFinished() || (effect.scope.pendingWithdraw() && !effect.scope.cleanupFinished())) {
				selected = effect
				break
			}
		}
		if selected.scope == nil {
			g.mu.Unlock()
			return
		}
		setup := !selected.scope.setupFinished()
		if setup {
			g.eventLocked(a.fiber, "scope:setup", selected.label, 0)
		} else {
			g.eventLocked(a.fiber, "scope:cleanup", selected.label, 0)
		}
		g.mu.Unlock()
		var err error
		if setup {
			err = selected.scope.runSetup(a.ctx.std, func() (context.Context, context.CancelFunc) { return g.scopeCleanupContext(a) })
		} else {
			ctx, cancel := g.scopeCleanupContext(a)
			err = selected.scope.runCleanup(ctx)
			cancel()
		}
		diagnostic := graphDiagnose(err) // Rendered here, off the lock, on the worker.
		g.mu.Lock()
		if setup && err == ErrScopeStarted && a.retiring && selected.scope.cleanupFinished() {
			// Retirement can cancel an unstarted slot after lane selection but
			// before runSetup claims it. Its handle already owns the exact
			// inactive result; do not invent a setup failure for this race.
			err = nil
		}
		if err != nil {
			a.fiber.lastErr = errors.Join(a.fiber.lastErr, diagnostic)
			if !setup || a.retiring {
				a.cleanupErr = errors.Join(a.cleanupErr, diagnostic)
			}
			g.eventLocked(a.fiber, "scope:failed", selected.label, 0)
		}
		g.removeScopeLocked(a, selected.scope)
		g.mu.Unlock()
	}
}

func (g *GraphLock) runScopeLane(a *graphActivation) {
	g.runScopeWork(a)
	g.mu.Lock()
	if a.retiring {
		proceed := g.beginUnloadLocked(a)
		g.mu.Unlock()
		if proceed {
			g.runUnload(a)
		}
		return
	}
	g.setWorkerLocked(a.fiber, false)
	g.reconcileLocked(a.fiber)
	g.mu.Unlock()
}

// cleanupEffect joins one partial or late setup disposer outside the graph
// owner. The Effect helper retains its actual lease until this function returns;
// a deadline records blocked work, never permission to release that ownership.
func (g *GraphLock) cleanupEffect(a *graphActivation, dispose Disposer) error {
	if dispose == nil {
		return nil
	}
	g.mu.Lock()
	budget := g.knobs.cleanup
	g.mu.Unlock()
	cleanupCtx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	pending, recorded, stalled := true, false, false // All protected by g.mu, including timer completion.
	recordDeadlineLocked := func(stall bool) {
		if recorded || a.fiber.current != a {
			return
		}
		recorded = true
		diagnostic := errors.New("effect cleanup deadline exceeded; still awaiting disposer")
		a.cleanupErr = errors.Join(a.cleanupErr, diagnostic)
		a.fiber.lastErr = errors.Join(a.fiber.lastErr, diagnostic)
		g.eventLocked(a.fiber, "deadline:cleanupEffect", "", 0)
		if stall { // The disposer is still running: the fiber is blocked in it.
			stalled = true
			g.stallLocked(a, "effect cleanup")
		}
	}
	timer := time.AfterFunc(budget, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if pending {
			recordDeadlineLocked(true)
		}
	})
	err := graphCallDispose(dispose, cleanupCtx)
	diagnostic := graphDiagnose(err) // Rendered here, on the caller's goroutine, before taking the lock.
	timeout := cleanupCtx.Err()
	timer.Stop()
	g.mu.Lock()
	pending = false
	if timeout != nil {
		// The real callback can observe its context deadline and return before
		// the sibling diagnostic timer acquires mu. Record it at most once.
		recordDeadlineLocked(false)
	}
	if stalled {
		stalled = false
		g.unstallLocked(a, "effect cleanup")
	}
	if err != nil && a.fiber.current == a && a.retiring {
		a.cleanupErr = errors.Join(a.cleanupErr, diagnostic)
	}
	g.mu.Unlock()
	return errors.Join(timeout, err)
}

// startTracked is called after the common Go helper acquires its real lease and
// before it starts caller code. Completion is bookkeeping only: lease Leave is
// still the actual drain barrier, and no extra joining goroutine is necessary.
func (g *GraphLock) startTracked(a *graphActivation, label string) (func(error), error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	f := a.fiber
	if f.current != a || a.retiring || !a.ctx.live.Load() {
		return nil, ErrInactive
	}
	a.nextTrack++
	token := a.nextTrack
	if a.tracked == nil {
		a.tracked = make(map[uint64]string)
	}
	a.tracked[token] = label
	g.eventLocked(f, "go:start", label, token)
	var once sync.Once
	return func(err error) {
		diagnostic := graphDiagnose(err) // Rendered here, on the worker that ran the callback, not under the lock.
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			delete(a.tracked, token)
			g.eventLocked(f, "go:done", label, token)
			if err == nil || f.current != a || a.retiring || !a.ctx.live.Load() || !g.targetMatchesLocked(a) {
				return
			}
			// AwaitFiber keeps the exact error identity; everything the kernel
			// reports uses the text rendered above.
			f.failedAt = a.revision
			f.failed = true
			f.lastErr = diagnostic
			f.awaitErr, f.awaitDiag = err, diagnostic
			g.eventLocked(f, "go:failed", label, token)
			g.retireLocked(a)
		})
	}, nil
}

func (g *GraphLock) retireLocked(a *graphActivation) {
	if a.retiring {
		return
	}
	wasActive := a.fiber.state == Active
	a.retiring = true
	// Invalidate before canceling: reentrant calls cannot acquire new leases.
	a.ctx.live.Store(false)
	a.cancel()
	for _, effect := range a.effects {
		if effect.scope != nil && !effect.scope.started.Load() {
			effect.scope.cancelPending(errors.Join(ErrInactiveEffect, ErrInactive))
		}
	}
	if !a.loading {
		g.stateLocked(a.fiber, Unloading)
	}
	g.eventLocked(a.fiber, "invalidate", "", 0)
	// Structural children are NOT requested here. As in the oracle, a child's
	// disposer is one effect in the parent's ordered list: runUnload requests it
	// at its LIFO slot, after any effect registered later and before any earlier
	// one, and joins it only after the walk. Until then the child stays live.
	// A child that holds one of our service bindings still retires now, by the
	// withdrawal below, which is the separate service-dependency axis.
	for _, key := range a.fiber.spec.Provides {
		if provided, exists := a.provided[key]; exists {
			g.wakeKeyLocked(key)
			if wasActive {
				// Strict availability is withdrawn now, but the real service
				// identity stays reserved through dependent and owner cleanup.
				g.serviceEventLocked(a.fiber, "service:withdraw", key, provided)
			}
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
		g.setWorkerLocked(a.fiber, true)
		if g.beginUnloadLocked(a) {
			go g.runUnload(a)
		}
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

func graphCallApplyEffect(apply func(*Context) (Disposer, error), ctx *Context) (dispose Disposer, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("Apply panic: %v", recovered)
		}
	}()
	return apply(ctx)
}

// callApply collects a successful returned disposer after the body, rather than
// before it or through the now-inactive public Defer path. A self-disposing or
// superseded body remains the sole old activation until this worker unwinds it.
func (g *GraphLock) callApply(a *graphActivation) error {
	if a.fiber.spec.ApplyEffect == nil {
		return graphCallApply(a.fiber.spec.Apply, a.ctx)
	}
	dispose, err := graphCallApplyEffect(a.fiber.spec.ApplyEffect, a.ctx)
	if err != nil || dispose == nil {
		return err
	}
	g.mu.Lock()
	if a.fiber.current == a {
		a.effects = append(a.effects, graphEffect{label: "apply", dispose: dispose})
		g.eventLocked(a.fiber, "effect:add", "apply", 0)
	}
	g.mu.Unlock()
	return nil
}

// graphErrorText renders a plugin-supplied error. The Error method is plugin
// code: it runs under a recover, so a panicking method yields a fixed text.
func graphErrorText(err error) (text string) {
	defer func() {
		if recover() != nil {
			text = "error formatting panic"
		}
	}()
	return err.Error()
}

// graphDiagnostic is a plugin error as the kernel stores and reports it: the
// text was rendered once, where the error entered the kernel, and Unwrap keeps
// errors.Is and errors.As working on the plugin's own value. Its Error method
// returns that text and never calls plugin code, so any goroutine may call it
// and it is safe under mu.
type graphDiagnostic struct {
	text  string
	cause error
}

func (e *graphDiagnostic) Error() string { return e.text }
func (e *graphDiagnostic) Unwrap() error { return e.cause }

// graphDiagnose is the one place a plugin-supplied error value is rendered. Call
// it on the plugin's (worker's) goroutine, off the lock, right where the error
// came back from plugin code; store and report only its result.
func graphDiagnose(err error) error {
	if err == nil {
		return nil
	}
	return &graphDiagnostic{text: graphErrorText(err), cause: err}
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
func graphCallCheck(check func(*Context) bool, std context.Context, fiber uint64, bindings map[string]provision) (available bool, err error) {
	c := &Context{std: std, fiber: fiber, bindings: bindings, dispose: func() error { return ErrInactive }}
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
		available, err := graphCallCheck(f.spec.Check, a.ctx.std, a.ctx.fiber, a.ctx.bindings)
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
			f.available = nil
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
		f.available = &graphAvailability{epoch: append([]uint64(nil), a.epoch...), notice: notice}
		g.eventLocked(f, "availability:accepted", "", 0)
		g.mu.Unlock()
		return true
	}
}

func (g *GraphLock) startPreCheckLocked(f *graphFiber, bindings map[string]provision, epoch []uint64) {
	std, cancel := context.WithCancel(context.Background())
	pre := &graphPre{revision: f.revision, notice: f.notice, epoch: epoch, cancel: cancel}
	f.pre = pre
	g.setWorkerLocked(f, true)
	pre.timer = time.AfterFunc(g.knobs.load, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if f.pre != pre {
			return
		}
		// The callback cannot be forced to return; abandon it as denied.
		g.eventLocked(f, "deadline:check", "", 0)
		g.finishPreCheckLocked(f, pre, false, errors.New("availability deadline exceeded"))
	})
	go func() {
		available, err := graphCallCheck(f.spec.Check, std, f.id, bindings)
		g.mu.Lock()
		defer g.mu.Unlock()
		if f.pre == pre {
			g.finishPreCheckLocked(f, pre, available, err)
		}
	}()
}

// finishPreCheckLocked applies one pre-check result if it is still current,
// then reconciles, which starts the load only for an accepted result.
func (g *GraphLock) finishPreCheckLocked(f *graphFiber, pre *graphPre, available bool, err error) {
	pre.timer.Stop()
	pre.cancel()
	pre.done = true
	for _, held := range pre.holds {
		held.pending--
	}
	pre.holds = nil
	f.pre = nil
	g.setWorkerLocked(f, false)
	_, epoch, ready := g.bindingsLocked(f)
	switch {
	case f.wantGone || !ready || f.revision != pre.revision || !graphSameEpoch(epoch, pre.epoch):
	case f.notice != pre.notice:
		g.eventLocked(f, "availability:stale", "", 0)
	case !available:
		f.available = nil
		f.blocked = &graphBlocked{revision: pre.revision, epoch: append([]uint64(nil), pre.epoch...), notice: pre.notice}
		if err != nil {
			f.lastErr = err
		}
		g.eventLocked(f, "availability:denied", "", 0)
	default:
		f.blocked = nil
		f.available = &graphAvailability{epoch: append([]uint64(nil), pre.epoch...), notice: pre.notice}
		g.eventLocked(f, "availability:accepted", "", 0)
	}
	g.reconcileLocked(f)
	g.flushServicesLocked()
}

func (g *GraphLock) runActiveCheck(a *graphActivation) {
	g.checkAvailability(a, false)
	g.mu.Lock()
	if a.retiring {
		proceed := g.beginUnloadLocked(a)
		g.mu.Unlock()
		if proceed {
			g.runUnload(a)
		}
		return
	}
	g.setWorkerLocked(a.fiber, false)
	// A Notify may land after the last check and before this lock acquisition.
	g.reconcileLocked(a.fiber)
	g.mu.Unlock()
}

func graphCallPrepare(cell *Configuration, ctx context.Context) (prepared PreparedConfig, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("configuration preparation panic: %v", recovered)
		}
	}()
	return cell.Prepare(ctx)
}

// prepareConfig is worker-owned. User resolution runs off-lock within the same
// absolute load deadline as Apply. A superseded raw version never reaches Apply;
// the old attempt unloads before reconciliation prepares the latest target.
func (g *GraphLock) prepareConfig(a *graphActivation) (bool, error) {
	cell := a.fiber.spec.Config
	if cell == nil {
		return true, nil
	}
	prepared, err := graphCallPrepare(cell, a.ctx.std)
	if err != nil {
		return false, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if a.retiring || !g.targetMatchesLocked(a) || !prepared.Published {
		g.retireLocked(a)
		return false, nil
	}
	a.ctx.config = prepared.Value
	a.ctx.configVersion = prepared.Version
	return true, nil
}

func (g *GraphLock) runLoad(a *graphActivation) {
	var err error
	if g.checkAvailability(a, false) {
		var prepared bool
		prepared, err = g.prepareConfig(a)
		if prepared && err == nil {
			err = g.callApply(a)
			if err == nil {
				g.checkAvailability(a, false)
				g.runScopeWork(a)
			}
		}
	}
	diagnostic := graphDiagnose(err) // Rendered here, on the worker, before taking the lock.
	g.mu.Lock()
	for !a.retiring && err == nil {
		check := a.fiber.spec.Check != nil && a.checked != a.fiber.notice
		scopes := g.scopeWorkLocked(a)
		if !check && !scopes {
			break
		}
		g.mu.Unlock()
		if check {
			g.checkAvailability(a, false)
		}
		g.runScopeWork(a)
		g.mu.Lock()
	}
	a.loading = false
	if a.loadStalled {
		a.loadStalled = false
		g.unstallLocked(a, "load")
	}
	if a.loadTimer != nil {
		a.loadTimer.Stop()
	}
	if !a.retiring && !g.targetMatchesLocked(a) {
		g.retireLocked(a)
	}
	if !a.retiring && err != nil {
		a.fiber.failedAt = a.revision
		a.fiber.failed = true
		a.fiber.lastErr = diagnostic
		a.fiber.awaitErr, a.fiber.awaitDiag = err, diagnostic
		g.eventLocked(a.fiber, "load:failed", "", 0)
		g.retireLocked(a)
	} else if a.retiring && err != nil {
		// A stale result is evidence, not a failure of the newer target.
		g.eventLocked(a.fiber, "load:stale", "", 0)
	}
	if a.retiring {
		g.stateLocked(a.fiber, Unloading)
		proceed := g.beginUnloadLocked(a)
		g.mu.Unlock()
		if proceed {
			g.runUnload(a)
		}
		return
	}
	g.stateLocked(a.fiber, Active)
	g.setWorkerLocked(a.fiber, false)
	for _, key := range a.fiber.spec.Provides {
		if p, exists := a.provided[key]; exists {
			g.services[key] = graphService{provision: p, activation: a}
			// Preserve the real synchronous recursive-notify order; do not move
			// off-lock availability results into an invented earlier position.
			g.wakeServiceLocked([]string{key}, []Event{{Fiber: a.fiber.id, Name: a.fiber.spec.ID, Kind: "service:publish", State: a.fiber.state, Key: key, UID: p.uid, Value: p.value}})
		}
	}
	g.mu.Unlock()
}

// A provider keeps its effects and identity until every activation holding one
// of its provision UIDs has fully unloaded, not merely canceled its context.
func (g *GraphLock) hasDependantsLocked(a *graphActivation) bool {
	if a.fiber == g.root && g.closed && len(g.fibers) != 0 {
		// Explicit shutdown joins every ordinary user, even one with no root
		// service binding or structural parent, before releasing host resources.
		// A contained root failure is narrower: g.closed is still false there.
		return true
	}
	// Structural children are a different axis: they do not retain the
	// provider's effects (awaitChildren joins them after the effect walk).
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

// releaseBindingsLocked resumes only the actual provider activations retained by
// this consumer, never every retiring fiber on each unrelated event. Providers
// cannot remove/reuse a bound UID before this release, but verify it explicitly
// so a newer reservation cannot be mistaken for the old owner.
func (g *GraphLock) releaseBindingsLocked(a *graphActivation) {
	for key, bound := range a.ctx.bindings {
		service, exists := g.services[key]
		if exists && service.uid == bound.uid && service.owner == bound.owner {
			g.resumeLocked(service.activation)
		}
	}
}

// The unload of a retiring activation has two waits on other fibers: for the
// activations that hold one of its provisions (dependants, before anything of
// its own is torn down) and for its structural children (after its effects ran).
// Neither parks a goroutine. The activation records what it waits for (a.wait)
// under mu and the event that can satisfy it, the release of a binding or the
// disposal of a child, resumes it with resumeLocked. Waiting is bounded
// transitively by the awaited fibers' own deadlines, not by one of its own: a
// waiter never claims a deadline it did not miss. While an awaited fiber is
// blocked past a deadline of its own the waiter records which fiber and in
// which stage (see reevalWaitLocked) and counts as blocked for Settle.

// beginUnloadLocked is called by the worker that owns a retiring activation's
// fiber slot, with mu held. It reports whether the caller should continue
// with runUnload on its own goroutine; if the activation must first wait for its
// dependants it is parked instead and the caller must simply return.
func (g *GraphLock) beginUnloadLocked(a *graphActivation) bool {
	if g.hasDependantsLocked(a) {
		g.parkLocked(a, graphWaitDependants)
		return false
	}
	return true
}

// parkLocked records what a is waiting for. The fiber stays busy: its worker
// slot is still held, there is just no goroutine behind it.
func (g *GraphLock) parkLocked(a *graphActivation, w graphWait) {
	a.wait = w
	if g.reevalWaitLocked(a) {
		g.blockedChangedLocked(a.fiber)
	}
}

// resumeLocked continues a parked activation whose wait is over. The disposers
// and lease waits that follow run off the lock, so it starts a goroutine for them.
func (g *GraphLock) resumeLocked(a *graphActivation) {
	switch a.wait {
	case graphWaitDependants:
		if g.hasDependantsLocked(a) {
			return
		}
		g.unparkLocked(a)
		go g.runUnload(a)
	case graphWaitChildren:
		if len(a.children) != 0 {
			return
		}
		g.unparkLocked(a)
		go g.finishUnload(a)
	}
}

func (g *GraphLock) unparkLocked(a *graphActivation) {
	a.wait = graphWaitNone
	if len(a.blame) != 0 {
		a.blame = nil
		g.blockedChangedLocked(a.fiber)
	}
}

// stallLocked marks a's fiber as blocked in stage: a callback of the activation
// is past its own deadline and has not returned. The caller has just recorded
// that deadline.
func (g *GraphLock) stallLocked(a *graphActivation, stage string) {
	if a.fiber.current != a {
		return
	}
	a.stalls = append(a.stalls, stage)
	g.blockedChangedLocked(a.fiber)
}

// unstallLocked ends one stall when its callback finally returned.
func (g *GraphLock) unstallLocked(a *graphActivation, stage string) {
	i := slices.Index(a.stalls, stage)
	if i < 0 {
		return
	}
	a.stalls = slices.Delete(a.stalls, i, i+1)
	if a.fiber.current == a {
		g.blockedChangedLocked(a.fiber)
	}
}

// stageLocked is how a fiber describes where it is blocked to whoever waits for it.
func stageLocked(f *graphFiber) string {
	a := f.current
	if len(a.stalls) != 0 {
		return a.stalls[0]
	}
	return a.wait.stage()
}

// blockedChangedLocked refreshes f's accounting after its stalls or blame
// changed, then re-evaluates every activation parked behind f. A change that
// leaves a waiter's blame as it was stops there, so the walk up the chain of
// waiters is proportional to the waiters that actually change.
func (g *GraphLock) blockedChangedLocked(f *graphFiber) {
	g.refreshBusyLocked(f)
	work := []*graphFiber{f}
	for len(work) != 0 {
		f := work[len(work)-1]
		work = work[:len(work)-1]
		for _, waiter := range g.waitersOnLocked(f) {
			if g.reevalWaitLocked(waiter) {
				work = append(work, waiter.fiber)
			}
		}
	}
}

// waitersOnLocked lists the parked activations that wait for f: the providers
// f's current activation is bound to, its structural parent, and, once the
// runtime is closing, the root (which waits for every user).
func (g *GraphLock) waitersOnLocked(f *graphFiber) []*graphActivation {
	var out []*graphActivation
	if a := f.current; a != nil {
		for key, bound := range a.ctx.bindings {
			if service, ok := g.services[key]; ok && service.uid == bound.uid && service.owner == bound.owner && service.activation.wait == graphWaitDependants {
				out = append(out, service.activation)
			}
		}
	}
	if parent := f.parent; parent != nil && parent.wait == graphWaitChildren {
		out = append(out, parent)
	}
	if r := g.root; r != nil && r != f && g.closed && r.current != nil && r.current.wait == graphWaitDependants {
		out = append(out, r.current)
	}
	return out
}

// reevalWaitLocked recomputes which of the fibers a is waiting for are blocked.
// It reports whether that set changed. A newly blamed fiber is recorded once, on
// a, as a diagnostic naming the fiber and its stage plus a "wait:" event; it is
// not a deadline of a's own and uses no "deadline" event kind.
func (g *GraphLock) reevalWaitLocked(a *graphActivation) bool {
	var blame []graphBlame
	switch a.wait {
	case graphWaitDependants:
		blame = g.blockedDependantsLocked(a)
	case graphWaitChildren:
		for _, child := range a.children {
			if blockedLocked(child) {
				blame = append(blame, graphBlame{id: child.id, kind: "child", name: child.spec.ID, stage: stageLocked(child)})
			}
		}
	}
	slices.SortFunc(blame, func(x, y graphBlame) int { return cmp.Compare(x.id, y.id) })
	if slices.Equal(blame, a.blame) {
		return false
	}
	a.blame = blame
	f := a.fiber
	for _, b := range blame {
		if _, seen := a.blamed[b]; seen {
			continue
		}
		if a.blamed == nil {
			a.blamed = make(map[graphBlame]struct{})
		}
		a.blamed[b] = struct{}{}
		f.lastErr = errors.Join(f.lastErr, fmt.Errorf("%s: %s fiber %d (%s) is blocked in %s", a.wait.stage(), b.kind, b.id, b.name, b.stage))
		g.appendEventLocked(Event{Fiber: f.id, Name: f.spec.ID, Kind: "wait:" + b.kind, State: f.state, Key: b.stage, UID: b.id})
	}
	g.refreshBusyLocked(f)
	return true
}

// blockedDependantsLocked lists the blocked fibers whose activation holds one of
// a's provisions (all blocked users when a is the closing root).
func (g *GraphLock) blockedDependantsLocked(a *graphActivation) []graphBlame {
	var blame []graphBlame
	seen := make(map[uint64]struct{})
	add := func(d *graphFiber) {
		if _, dup := seen[d.id]; dup || !blockedLocked(d) {
			return
		}
		seen[d.id] = struct{}{}
		blame = append(blame, graphBlame{id: d.id, kind: "dependant", name: d.spec.ID, stage: stageLocked(d)})
	}
	for key, provided := range a.provided {
		for _, d := range g.indexByKey[key] {
			if d.current != nil {
				if bound, exists := d.current.ctx.bindings[key]; exists && bound.uid == provided.uid {
					add(d)
				}
			}
		}
	}
	if a.fiber == g.root && g.closed {
		for d := range g.blocked {
			if d != g.root {
				add(d)
			}
		}
	}
	return blame
}

// awaitLeases waits for the activation's own leases to drain: that is its own
// work and its own drain budget. Past the budget the fiber is blocked in
// "lease drain" until the last lease is released.
func (g *GraphLock) awaitLeases(a *graphActivation) {
	timer := time.NewTimer(g.knobs.leaseDrain)
	defer timer.Stop()
	deadline := timer.C
	stalled := false
	for a.ctx.lease.Load() > 0 {
		select {
		case <-a.leaseWake:
		case <-deadline:
			g.mu.Lock()
			if a.fiber.current == a && a.ctx.lease.Load() > 0 {
				a.fiber.lastErr = errors.New("lease drain deadline exceeded; still awaiting leases")
				g.eventLocked(a.fiber, "deadline:lease", "", 0)
				stalled = true
				g.stallLocked(a, "lease drain")
			}
			g.mu.Unlock()
			// Timing out never permits reclamation of still-leased effects.
			deadline = nil
		}
	}
	if stalled {
		g.mu.Lock()
		g.unstallLocked(a, "lease drain")
		g.mu.Unlock()
	}
}

// removeProvision is a native ownership effect, not a caller disposer. All
// dependent activations and leases have already drained. Withdrawal happened at
// quiescence; this is actual LIFO removal, visible before any older custom effect.
func (g *GraphLock) removeProvision(a *graphActivation, effect graphEffect) {
	g.mu.Lock()
	defer g.mu.Unlock()
	provided, exists := a.provided[effect.provisionKey]
	if !exists || provided.uid != effect.provisionUID {
		return
	}
	if service, published := g.services[effect.provisionKey]; published && service.activation == a && service.uid == effect.provisionUID {
		delete(g.services, effect.provisionKey)
	}
	delete(a.provided, effect.provisionKey)
	a.ctx.unpublishOwned(effect.provisionKey)
	for i, owned := range a.effects {
		if owned.provisionUID == effect.provisionUID {
			copy(a.effects[i:], a.effects[i+1:])
			a.effects[len(a.effects)-1] = graphEffect{}
			a.effects = a.effects[:len(a.effects)-1]
			break
		}
	}
	g.wakeKeyLocked(effect.provisionKey)
	g.serviceEventLocked(a.fiber, "service:remove", effect.provisionKey, provided)
}

// runUnload is the unload of an activation whose dependants are all gone: drain
// its leases, run its effects in LIFO order, then finish, or park until its
// children are disposed. Each budget measures only this activation's own work.
func (g *GraphLock) runUnload(a *graphActivation) {
	g.awaitLeases(a)
	g.mu.Lock()
	effects := append([]graphEffect(nil), a.effects...)
	g.mu.Unlock()
	cleanupCtx, cancel := context.WithTimeout(context.Background(), g.knobs.cleanup)
	var walkDone, stalled bool // Protected by mu, including the timer.
	timer := time.AfterFunc(g.knobs.cleanup, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if a.fiber.current == a && a.retiring {
			a.cleanupErr = errors.Join(a.cleanupErr, errors.New("cleanup deadline exceeded; still awaiting disposer"))
			a.fiber.lastErr = errors.Join(a.fiber.lastErr, a.cleanupErr)
			g.eventLocked(a.fiber, "deadline:cleanup", "", 0)
			if !walkDone {
				stalled = true
				g.stallLocked(a, "cleanup")
			}
		}
	})
	var disposeErr error
	for i := len(effects) - 1; i >= 0; i-- {
		if id := effects[i].child; id != 0 {
			// The child's disposer in the parent's LIFO order: invalidate and
			// unload it now, without waiting for it (it is joined below).
			g.mu.Lock()
			if child := g.fibers[id]; child != nil && child.parent == a {
				g.requestDisposeLocked(child)
			}
			g.mu.Unlock()
			continue
		}
		if effects[i].provisionUID != 0 {
			g.removeProvision(a, effects[i])
			continue
		}
		if scope := effects[i].scope; scope != nil {
			if !scope.setupFinished() {
				scope.cancelPending(errors.Join(ErrInactiveEffect, ErrInactive))
			}
			// A never-started canceled slot has no acquired resources and no
			// cleanup error to add to normal unload diagnostics.
			if !scope.cleanupFinished() {
				disposeErr = errors.Join(disposeErr, graphDiagnose(scope.runCleanup(cleanupCtx)))
			}
			g.mu.Lock()
			g.removeScopeLocked(a, scope)
			g.mu.Unlock()
			continue
		}
		disposeErr = errors.Join(disposeErr, graphDiagnose(graphCallDispose(effects[i].dispose, cleanupCtx)))
	}
	timer.Stop()
	cancel()
	g.mu.Lock()
	walkDone = true
	if stalled {
		g.unstallLocked(a, "cleanup")
	}
	a.disposeErr = disposeErr
	if len(a.children) != 0 {
		g.parkLocked(a, graphWaitChildren)
		g.mu.Unlock()
		return
	}
	g.mu.Unlock()
	g.finishUnload(a)
}

// finishUnload ends the unload once nothing is awaited any more.
func (g *GraphLock) finishUnload(a *graphActivation) {
	g.mu.Lock()
	a.cleanupErr = errors.Join(a.cleanupErr, a.disposeErr)
	if a.cleanupErr != nil {
		a.fiber.lastErr = errors.Join(a.fiber.lastErr, a.cleanupErr)
	}
	// Keep committed reads and the effect list valid throughout the unwind.
	a.ctx.released.Store(true)
	a.effects = nil
	g.eventLocked(a.fiber, "effects:gone", "", 0)
	a.fiber.current = nil
	g.setWorkerLocked(a.fiber, false)
	g.releaseBindingsLocked(a)
	g.reconcileLocked(a.fiber)
	g.mu.Unlock()
}
