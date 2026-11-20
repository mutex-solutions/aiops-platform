// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// ErrInactive reports a call on a context, fiber or lease that no longer accepts
// it (docs/architecture/07-c4-plugin-sdk.md, section 3.3).
var ErrInactive = errors.New("inactive context")

// ErrInactiveEffect identifies a registration on an activation that no longer
// accepts effects: an unloading or disposed fiber, or a late registration from a
// load that passed its deadline (07-c4-plugin-sdk.md, section 3.3 and the Effect
// signature). Such errors also match ErrInactive.
var ErrInactiveEffect = errors.New("cannot create effect on inactive context")

// handle is the per-activation handle given to an apply callback. It describes
// one activation and cannot revive an earlier one. The exported front wraps it;
// every method is safe to call from any goroutine unless stated.
type handle struct {
	core     *core // The core this activation belongs to, set once.
	std      context.Context
	fiber    uint64
	act      *activation // The activation this handle belongs to; nil on an availability-check handle.
	live     atomic.Bool // New effects and leases are accepted only while live.
	released atomic.Bool // Committed reads remain valid during orderly teardown.
	lease    leaseCount  // Own cache lines: enter/leave writes never invalidate live/std/core readers.
	leave    func()      // Allocated once per activation: enter returns it without allocating.
	bindings map[svcKey]provision
	// The functions below are installed by owner.activation, which closes over the
	// owner transport. They are nil on an availability-check handle.
	provideFn     func(key svcKey, label string, value any) error
	effectFn      func(string, disposer) error
	cleanupFn     func(disposer) error // Dispose budget for partial or late setup results.
	scopeFn       func(string, func(*effectScope) error) (*effectHandle, error)
	disposeFn     func() error
	childFn       func(pluginSpec) (uint64, error)
	trackedFn     func(string) (func(error), error)
	leaseChanged  func() // Optional nonblocking wake; installed by the scheduler.
	owned         atomic.Pointer[ownedProvisions]
	cleanupBudget func() (context.Context, context.CancelFunc)
}

// newHandle returns an inactive handle with its lease function allocated.
func newHandle(c *core, std context.Context, fiber uint64, bindings map[svcKey]provision) *handle {
	h := &handle{core: c, std: std, fiber: fiber, bindings: bindings}
	h.leave = h.release
	return h
}

type ownedProvisions struct{ keys map[svcKey]struct{} }

// owns is a lock-free observation of this activation's successful native
// provision reservations. Callers that write derived scope metadata must also
// hold a lease, so quiescence cannot finish while that operation is in flight.
func (c *handle) owns(key svcKey) bool {
	if c.released.Load() {
		return false
	}
	values := c.owned.Load()
	if values == nil {
		return false
	}
	_, ok := values.keys[key]
	return ok
}

// unpublishOwned follows actual native removal. Like publication, native state
// ownership serializes writes; readers observe an immutable COW key set.
func (c *handle) unpublishOwned(key svcKey) {
	previous := c.owned.Load()
	next := &ownedProvisions{keys: make(map[svcKey]struct{}, len(previous.keys))}
	for existing := range previous.keys {
		if existing != key {
			next.keys[existing] = struct{}{}
		}
	}
	c.owned.Store(next)
}

func (c *handle) publishOwned(key svcKey) {
	previous := c.owned.Load()
	next := &ownedProvisions{keys: make(map[svcKey]struct{})}
	if previous != nil {
		for existing := range previous.keys {
			next.keys[existing] = struct{}{}
		}
	}
	next.keys[key] = struct{}{}
	c.owned.Store(next)
}

// provision is the immutable binding record of one provided value.
type provision struct {
	uid   uint64
	owner uint64
	value any
}

// read requires a dependency captured for the current activation.
func (c *handle) read(key svcKey) (any, error) {
	if c.released.Load() {
		return nil, ErrInactive
	}
	p, ok := c.bindings[key]
	if !ok {
		return nil, errUnknown
	}
	return p.value, nil
}

// provide reserves key for this fiber, as an effect that is reverted with it.
// label names the effect in the ledger. The fiber must declare key in its
// pluginSpec.Provides.
func (c *handle) provide(key svcKey, label string, value any) error {
	if !c.live.Load() {
		return ErrInactive
	}
	return c.provideFn(key, label, value)
}

// refusedCall is the panic value of a defer-style or go-style registration that
// the activation refused. The registration signatures return no error
// (07-c4-plugin-sdk.md:200-201); the contract fails the fiber the way Key.Of does
// (07-c4-plugin-sdk.md:180, "panics ... recovered at the fiber boundary"), so the
// call unwinds the caller to the nearest callback boundary (apply, a disposer, a
// tracked goroutine), which fails the activation if it is still the target.
type refusedCall struct{ err error }

func (e *refusedCall) Error() string { return e.err.Error() }
func (e *refusedCall) Unwrap() error { return e.err }

// refuse panics with a *refusedCall carrying err.
func refuse(err error) { panic(&refusedCall{err: err}) }

// tryDefer registers dispose in this activation's effect ledger and returns the
// refusal, if any. The disposer is owned by the ledger exactly when it returns nil.
func (c *handle) tryDefer(label string, dispose disposer) error {
	if !c.live.Load() {
		return ErrInactive
	}
	return c.effectFn(label, dispose)
}

// deferEffect is the defer-style registration: it returns no error and a refused
// call (inactive activation, disposal started, nil disposer) fails the fiber
// through refuse. The disposer is not run on refusal; the caller keeps ownership.
func (c *handle) deferEffect(label string, dispose disposer) {
	if err := c.tryDefer(label, dispose); err != nil {
		if errors.Is(err, ErrInactive) {
			err = errors.Join(ErrInactiveEffect, err)
		}
		refuse(err)
	}
}

// mountChild creates a real lifecycle child of this exact activation. The child
// is one ordered effect of the parent, as in the oracle: when the parent unloads,
// its cleanup registered after the child runs first, the child is invalidated and
// unloaded when the walk reaches its slot, an effect registered before the child
// runs without waiting for it (the child may still be using those resources), and
// the parent is DISPOSED only after the child is. An old context cannot attach
// children to a replacement activation.
func (c *handle) mountChild(spec pluginSpec) (uint64, error) {
	if !c.live.Load() || c.childFn == nil {
		return 0, ErrInactive
	}
	return c.childFn(spec)
}

// requestDispose requests disposal of this activation's fiber without awaiting
// it. It never joins Apply or cleanup.
func (c *handle) requestDispose() error {
	if c.disposeFn == nil {
		return ErrInactive
	}
	return c.disposeFn()
}

// leaseShards is a power of two. Sixteen padded shards cost 1 KiB per
// activation, enough that 8 to 32 callers rarely share a line.
const (
	leaseShardBits = 4
	leaseShards    = 1 << leaseShardBits
)

// leaseShard owns a full cache line, whatever the alignment of the enclosing
// struct: counters are 64 bytes apart, so no two can share a line.
type leaseShard struct {
	n atomic.Int64
	_ [56]byte
}

// leaseCount is a striped open-lease counter. A leave may land on a different
// shard than its enter (each pick is random), so an
// individual shard can be negative; only the sum is meaningful. The leading and
// trailing pads keep neighbouring handle fields off the first and last shard's
// line.
type leaseCount struct {
	_      [64]byte
	shards [leaseShards]leaseShard
	_      [64]byte
}

// shard picks a shard at random. The generator keeps its state per thread, so the
// pick takes no lock, allocates nothing and shares no cache line. Any shard is
// correct, the choice only spreads contention.
func (l *leaseCount) shard() *atomic.Int64 {
	return &l.shards[rand.Uint32N(leaseShards)].n
}

// Load sums the shards. It reads each shard once, in order, while enter/leave
// may run: a shard only ever moves towards the value it will have when the
// system is quiet except for rolled-back enter attempts, so once live is false
// the sum is >= the true open-lease count (never a false zero) and is exact
// when no enter/leave is in flight. See enter for why that suffices.
func (l *leaseCount) Load() int64 {
	var sum int64
	for i := range l.shards {
		sum += l.shards[i].n.Load()
	}
	return sum
}

// enter acquires a call-path lease with no lock and no allocation: the returned
// leave function is the one allocated with the activation. The caller must call
// leave exactly once after a successful enter, and never after a refused one.
//
// Quiescence argument. A drainer (quiesce) stores live=false and only then reads
// the lease sum (the owner's drain and the leaseChanged wake). Every atomic here
// is sequentially consistent, so the operations have one total order. enter
// increments, THEN re-checks live:
//
//   - enter's re-check sees live=true: the increment precedes live=false in the
//     order, so every sum read taken after live=false includes it, and the
//     drainer waits for the matching leave. The body runs inside the lease.
//   - The re-check sees live=false: enter decrements the same shard it
//     incremented and, as the decrement may be the move that brings the sum to
//     zero, calls leaseChanged. The body never runs.
//   - The first live check sees false: nothing was touched.
//
// leave decrements, THEN reads live; it calls leaseChanged only when live is
// false. If the drainer's sum read saw this lease still open, that read is
// before this decrement in the total order, and live=false is before that read,
// so leave's live read sees false and wakes the drainer: the last leave cannot
// be missed. If instead leave's decrement came first, the drainer's read sees
// it. If live is still true at leave, no drainer has started: its later
// live=false precedes its later sum read, which sees the decrement. Fast-path
// enter/leave on a live activation therefore make no wake call at all.
//
// Shards: after live=false the only writes are decrements by leave and
// increment/decrement pairs by refused enters, so every shard read in a sum is
// >= the shard's final value, hence the sum is >= the final open count >= 0. A
// zero sum therefore proves no lease is open (no false zero), and a transient
// overcount is repaired by the wake that follows the decrement that fixes it
// (the last decrement, in total order, is followed by a sum that sees all of
// them). The wake re-checks live and the sum on the owner.
func (c *handle) enter() (leave func(), ok bool) {
	if !c.live.Load() {
		return nil, false
	}
	return c.confirm(c.lease.shard())
}

// confirm is the second half of enter: it takes the lease on shard s, then
// re-checks live and rolls the lease back when quiescence began in between.
func (c *handle) confirm(s *atomic.Int64) (leave func(), ok bool) {
	s.Add(1)
	if c.live.Load() {
		return c.leave, true
	}
	s.Add(-1)
	if c.leaseChanged != nil {
		c.leaseChanged()
	}
	return nil, false
}

// release is the body of the leave function. See enter for the wake rule.
func (c *handle) release() {
	c.lease.shard().Add(-1)
	if !c.live.Load() && c.leaseChanged != nil {
		c.leaseChanged()
	}
}

// activation builds the handle and bookkeeping of one load of f.
func (o *owner) activation(f *fiber, epoch []uint64, bindings map[svcKey]*liveProvision) *activation {
	std, cancel := context.WithCancel(context.Background())
	c := newHandle(o.r, std, f.id, make(map[svcKey]provision, len(bindings)))
	plugin := f.spec.ID
	a := &activation{ctx: c, cancel: cancel, epoch: epoch}
	c.act = a
	a.notice.act = a
	for key, p := range bindings {
		c.bindings[key] = p.binding
		p.dependents[c] = f
	}
	// The bindings map is an immutable activation snapshot. Reads and leases
	// never enter the owner, and the transport waker runs only on quiescent exit.
	c.leaseChanged = func() {
		if !c.live.Load() && c.lease.Load() == 0 {
			a.freed.once() // Quiesced exit only: the lease fast path never gets here.
			o.r.notify(&a.notice)
		}
	}
	c.provideFn = func(key svcKey, label string, value any) error {
		// The owner reply decides whether the native record owns this resource.
		// Activation cancellation cannot revoke a successful admission ack.
		return o.r.exchange(context.Background(), message{kind: cmdProvide, act: a, key: key, label: label, value: value}).err
	}
	c.effectFn = func(label string, dispose disposer) error {
		// Registration transfers disposer ownership exactly when the native
		// owner accepts it. Cancellation cannot turn that success into a caller-
		// owned rollback and cause both paths to invoke the same disposer.
		return o.r.exchange(context.Background(), message{kind: cmdEffect, act: a, label: label, dispose: dispose}).err
	}
	c.scopeFn = func(label string, setup func(*effectScope) error) (*effectHandle, error) {
		got := o.r.exchange(context.Background(), message{kind: cmdScopeRequest, act: a, label: label, setup: setup})
		return got.handle, got.err
	}
	c.cleanupBudget = func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), o.r.cfg.disposeDeadline)
	}
	c.cleanupFn = func(dispose disposer) error {
		// Setup rollback is not a new activation effect. Its caller still holds
		// the real setup lease, so even a cancellation-ignoring disposer cannot
		// let native teardown release resources before this callback returns.
		cleanupCtx, cancel := c.cleanupBudget()
		defer cancel()
		err := invokeCallback(func() error { return dispose(cleanupCtx) })
		return errors.Join(err, cleanupCtx.Err())
	}
	c.trackedFn = func(label string) (func(error), error) {
		// The owner decides admission against this activation. Do not abandon an
		// admitted record when Std is canceled between registration and its ack.
		got := o.r.exchange(context.Background(), message{kind: cmdTrackedStart, act: a, label: label})
		if got.err != nil {
			if std.Err() != nil {
				return nil, ErrInactive
			}
			return nil, got.err
		}
		id, run := got.id, got.run
		return func(err error) {
			// run and even Error() belong to the worker. Completion uses a
			// non-canceled transport context so quiescence cannot lose its record.
			at := run.finish()
			text := errorText(err)
			if err != nil {
				// The cause reaches the log before the fiber is FAILED, from the worker.
				o.r.logError("tracked goroutine failed", a.ctx.fiber, plugin, label, text)
			}
			o.r.exchange(context.Background(), message{kind: cmdTrackedDone, act: a, id: id, err: err, errText: text, at: at})
		}, nil
	}
	c.childFn = func(spec pluginSpec) (uint64, error) {
		spec.Needs, spec.Provides = slices.Clone(spec.Needs), slices.Clone(spec.Provides)
		// A committed child must return its actual ID even when the parent is
		// quiesced before the caller receives this ownership-transfer reply.
		got := o.r.exchange(context.Background(), message{kind: cmdMountChild, act: a, spec: spec})
		return got.id, got.err
	}
	c.disposeFn = func() error {
		if f.id == 0 {
			return errRootDispose
		}
		if !c.live.Load() {
			return ErrInactive
		}
		// Dispose joins only command admission, never Apply or cleanup. Exact
		// activation validation prevents a retiring context from disposing the
		// fiber whose desired revision has already moved on.
		return o.r.exchange(context.Background(), message{kind: cmdSelfDispose, act: a}).err
	}
	c.live.Store(true)
	return a
}

// effect runs setup on the calling goroutine and registers its returned disposer
// in this activation's LIFO effect ledger. A nonempty label and nonnil setup are
// required. A nil returned disposer owns no resource and adds nothing.
//
// An activation lease is held before setup and until setup and any immediate
// cleanup actually return. On setup failure, a returned disposer runs
// immediately, before the setup error is returned. If quiescence instead rejects
// registration, the returned disposer likewise runs immediately and the error
// matches both ErrInactiveEffect and ErrInactive (07-c4-plugin-sdk.md:199 and
// section 3.3). Immediate cleanup uses the dispose deadline and a panic
// boundary, outside the owner; setup and cleanup errors are joined without
// losing their identities.
//
// Setup may block the caller. effect creates no goroutine: use goTracked when
// asynchronous setup itself must be owned by the activation. Std cancellation
// does not mean setup or cleanup has returned, and never releases the lease.
//
// A setup panic propagates after releasing the lease to the caller's boundary
// (for example apply or a tracked goroutine). Resources registered individually
// with deferEffect remain owned by the activation and unwind normally. Defers
// registered inside setup precede its returned disposer in the ledger, so that
// disposer runs first.
func (c *handle) effect(label string, setup func() (disposer, error)) error {
	if label == "" {
		return errors.New("effect label must not be empty")
	}
	if setup == nil {
		return errors.New("effect setup must not be nil")
	}
	leave, ok := c.enter()
	if !ok {
		return errors.Join(ErrInactiveEffect, ErrInactive)
	}
	defer leave()
	if c.cleanupFn == nil {
		return errEffectUnavailable
	}
	dispose, err := setup()
	if err != nil {
		if dispose != nil {
			return errors.Join(err, c.cleanupFn(dispose))
		}
		return err
	}
	if dispose == nil {
		return nil
	}
	if err := c.tryDefer(label, dispose); err != nil {
		cleanupErr := c.cleanupFn(dispose)
		if errors.Is(err, ErrInactive) {
			return errors.Join(ErrInactiveEffect, err, cleanupErr)
		}
		return errors.Join(err, cleanupErr)
	}
	return nil
}

// errEffectUnavailable means that this handle has no cleanup bridge (an
// availability-check handle). effect never substitutes a private deadline.
var errEffectUnavailable = errors.New("native effect cleanup unsupported")

// goTracked starts work owned by this exact activation, after the core registers
// its label in the tracked-work ledger. A refused call (inactive activation,
// disposal started) fails the fiber through refuse, as for deferEffect
// (07-c4-plugin-sdk.md:201 and :180). An empty label or nil run is a programming
// error and panics too.
//
// The worker holds an activation lease until run actually exits. Quiescence
// cancels Std and rejects new work, but cannot release effects or dependency
// bindings while an existing worker is blocked. run must cooperate with Std's
// cancellation; there is no timeout that makes a stuck worker cease to exist.
//
// The lease is released as run returns, before its return is stamped and reported
// through the native completion hook, so a drain judges the lease and the record by
// the same instant (drainInTime). The record keeps the drain open until the owner has
// the report. A returned error or recovered panic fails the activation, including
// one that already became ACTIVE. A wrapped context.Canceled is normal completion
// only when Std is itself canceled. Other errors, including DeadlineExceeded, fail;
// workers may instead return nil after handling shutdown themselves.
func (c *handle) goTracked(label string, run func(context.Context) error) {
	if label == "" {
		refuse(errors.New("tracked goroutine label must not be empty"))
	}
	if run == nil {
		refuse(errors.New("tracked goroutine run must not be nil"))
	}
	leave, ok := c.enter()
	if !ok {
		refuse(errors.Join(ErrInactiveEffect, ErrInactive))
	}
	if c.trackedFn == nil {
		leave()
		refuse(errTrackedUnavailable)
	}
	done, err := c.trackedFn(label)
	if err != nil {
		leave()
		if errors.Is(err, ErrInactive) {
			err = errors.Join(ErrInactiveEffect, err)
		}
		refuse(err)
	}
	go func() {
		// A worker failure cannot be outrun by the drain wake of this leave: the
		// tracked record keeps the activation busy (ownBusy) until done's report.
		err := runTracked(c.std, label, run)
		leave()
		done(err)
	}()
}

// errTrackedUnavailable means that this handle has no tracked-work bridge.
var errTrackedUnavailable = errors.New("native tracked work unsupported")

// runTracked executes callback code, error matching, and panic formatting on the
// worker, never on the owner.
func runTracked(ctx context.Context, label string, run func(context.Context) error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			if cause, ok := p.(error); ok {
				err = fmt.Errorf("tracked goroutine %q panic: %w", label, cause)
			} else {
				err = fmt.Errorf("tracked goroutine %q panic: %v", label, p)
			}
		}
	}()
	err = run(ctx)
	if ctx.Err() != nil && errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

var (
	errScopeUnavailable = errors.New("native effect scopes unsupported")
	errScopeSealed      = errors.New("effect scope setup is sealed")
	errScopeStarted     = errors.New("effect scope setup already started")
)

// The states of one effect scope node, as shown in effectMeta.State.
const (
	scopePending  = "pending"
	scopeSetup    = "setup"
	scopeReady    = "ready"
	scopeWithdraw = "withdraw"
	scopeCleaning = "cleaning"
	scopeFinished = "done"
)

// effectMeta is a detached snapshot of actual scope/disposer ownership. Children
// retain registration order; their cleanup executes in the opposite order.
// Changing a returned snapshot cannot change the collector or later snapshots.
type effectMeta struct {
	Label    string
	State    string
	Children []effectMeta
}

// scopePanicError retains the original callback panic, including an error cause.
// Formatting is deliberately deferred to Error, never done by metadata readers.
type scopePanicError struct{ Value any }

func (e *scopePanicError) Error() string {
	return fmt.Sprintf("effect scope callback panic: %v", e.Value)
}
func (e *scopePanicError) Unwrap() error {
	cause, _ := e.Value.(error)
	return cause
}

// requestScope reserves native ownership, not an explicit call-path lease.
// The activation's existing transition worker later runs setup and cleanup. The
// native admission hook revalidates this exact activation after this fast check.
// Completion waits are external-only: a callback must not await its own lane.
func (c *handle) requestScope(label string, setup func(*effectScope) error) (*effectHandle, error) {
	if err := validateScope(label, setup); err != nil {
		return nil, err
	}
	if c == nil || c.scopeFn == nil {
		return nil, errScopeUnavailable
	}
	if !c.live.Load() || (c.std != nil && c.std.Err() != nil) {
		return nil, errors.Join(ErrInactiveEffect, ErrInactive)
	}
	return c.scopeFn(label, setup)
}

// effectScope incrementally owns resources acquired by one setup callback.
// Defer remains admissible until setup seals, even if the activation's standard
// context was canceled in the meantime. Scope runs nested setup synchronously on
// the current transition worker; it does not schedule work or start a goroutine.
// Setup callbacks must not move nested Scope calls to another goroutine.
type effectScope struct {
	effect *scopeEffect
	node   *scopeNode
}

func (s *effectScope) tryDefer(label string, d disposer) error {
	if label == "" || d == nil {
		return errors.New("scope disposer requires a nonempty label and nonnil callback")
	}
	e := s.effect
	e.mu.Lock()
	defer e.mu.Unlock()
	if s.node.sealed {
		return errScopeSealed
	}
	s.node.children = append(s.node.children, &scopeNode{label: label, state: scopeReady, sealed: true, dispose: d})
	e.publishLocked()
	return nil
}

func (s *effectScope) scope(label string, setup func(*effectScope) error) error {
	if err := validateScope(label, setup); err != nil {
		return err
	}
	e := s.effect
	e.mu.Lock()
	if s.node.sealed {
		e.mu.Unlock()
		return errScopeSealed
	}
	node := &scopeNode{label: label, state: scopeSetup}
	s.node.children = append(s.node.children, node)
	e.publishLocked() // Ownership is visible before the nested callback starts.
	e.mu.Unlock()
	return e.setupNode(node, setup)
}

// effectHandle observes completion of one native scope identity. It contains no
// caller-mutable ownership or scheduling state. AwaitSetup and Await may only be
// called outside callbacks running on the scope's own transition lane.
type effectHandle struct{ effect *scopeEffect }

func (h *effectHandle) awaitSetup(ctx context.Context) error {
	if h == nil || h.effect == nil {
		return errScopeUnavailable
	}
	if err := awaitScope(ctx, h.effect.setupDone); err != nil {
		return err
	}
	return h.effect.setupErr
}

// RequestWithdraw only requests native control work, never inline cleanup. The
// native hook is responsible for idempotence and unload/admission serialization.
func (h *effectHandle) requestWithdrawal() error {
	if h == nil || h.effect == nil || h.effect.requestWithdraw == nil {
		return errScopeUnavailable
	}
	return h.effect.requestWithdraw()
}

// Await returns cleanup errors after every owned callback has actually returned.
// Setup errors (including its rollback errors) are observed through AwaitSetup.
func (h *effectHandle) await(ctx context.Context) error {
	if h == nil || h.effect == nil {
		return errScopeUnavailable
	}
	if err := awaitScope(ctx, h.effect.done); err != nil {
		return err
	}
	return h.effect.cleanupErr
}

func (h *effectHandle) metadata() effectMeta {
	if h == nil || h.effect == nil {
		return effectMeta{}
	}
	return h.effect.metadata()
}

func awaitScope(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	default:
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// scopeEffect is shared work, not a scheduler. Native owners reserve and remove
// its exact pointer in their ledger and bind requestWithdraw before publication.
// They run work only on the fiber's existing, nonoverlapping transition lane.
// No queued scope holds an Enter lease or owns a per-scope goroutine.
type scopeEffect struct {
	handle          *effectHandle
	requestWithdraw func() error
	withdraw        atomic.Bool
	started         atomic.Bool
	cleaning        atomic.Bool
	setupDone       chan struct{}
	done            chan struct{}
	setupErr        error // Immutable after setupDone closes.
	cleanupErr      error // Immutable after done closes.
	setup           func(*effectScope) error
	mu              sync.Mutex
	root            *scopeNode
	meta            atomic.Pointer[effectMeta]
	run             *scopeRun // Used only by the current setup worker.
}

type scopeNode struct {
	label    string
	state    string
	sealed   bool
	children []*scopeNode
	dispose  disposer
}

// The factory is called lazily at the first setup failure. Nested and root
// rollback share that one native budget, rather than renewing it per resource.
type scopeRun struct {
	factory func() (context.Context, context.CancelFunc)
	ctx     context.Context
	cancel  context.CancelFunc
}

func newScopeEffect(label string, setup func(*effectScope) error) *scopeEffect {
	e := &scopeEffect{setup: setup, root: &scopeNode{label: label, state: scopePending},
		setupDone: make(chan struct{}), done: make(chan struct{})}
	e.handle = &effectHandle{effect: e}
	e.publishLocked()
	return e
}

// runSetup runs exactly once. Setup uses the captured activation context, not a
// newly exposed scope context; the first argument records the native work seam.
// On failure, actual rollback finishes before setupDone is published. The native
// caller supplies its existing disposal-budget factory, not a new timeout knob.
func (e *scopeEffect) runSetup(_ context.Context, cleanupFactory func() (context.Context, context.CancelFunc)) error {
	if !e.started.CompareAndSwap(false, true) {
		return errScopeStarted
	}
	if cleanupFactory == nil {
		e.setup = nil
		e.setupErr = errScopeUnavailable
		e.setState(e.root, scopeFinished, true)
		e.cleanupErr = errScopeUnavailable
		e.cleaning.Store(true)
		close(e.done)
		close(e.setupDone)
		return e.setupErr
	}
	e.run = &scopeRun{factory: cleanupFactory}
	defer func() {
		if e.run.cancel != nil {
			e.run.cancel()
		}
		e.run = nil
	}()
	e.setState(e.root, scopeSetup, false)
	err := invokeScope(func() error { return e.setup(&effectScope{effect: e, node: e.root}) })
	e.setup = nil // A retained handle must not retain the completed setup closure.
	e.setState(e.root, scopeReady, true)
	if err != nil {
		err = errors.Join(err, e.cleanupOnce(e.rollbackContext()))
	}
	e.setupErr = err
	close(e.setupDone)
	return err
}

func (e *scopeEffect) setupNode(node *scopeNode, setup func(*effectScope) error) error {
	err := invokeScope(func() error { return setup(&effectScope{effect: e, node: node}) })
	e.setState(node, scopeReady, true)
	if err != nil {
		err = errors.Join(err, e.cleanupNode(e.rollbackContext(), node))
	}
	return err
}

func (e *scopeEffect) rollbackContext() context.Context {
	if e.run.ctx == nil {
		e.run.ctx, e.run.cancel = e.run.factory()
	}
	return e.run.ctx
}

// cancelPending seals an admitted scope whose activation quiesced before setup.
// The native worker calls this instead of running a late setup callback. A false
// result means setup already started and must retain ownership until it returns.
func (e *scopeEffect) cancelPending(err error) bool {
	if !e.started.CompareAndSwap(false, true) {
		return false
	}
	if err == nil || err == ErrInactive || err == ErrInactiveEffect {
		err = errors.Join(ErrInactiveEffect, ErrInactive, err)
	}
	e.setup = nil
	e.setupErr, e.cleanupErr = err, err
	e.cleaning.Store(true)
	e.setState(e.root, scopeFinished, true)
	close(e.done)
	close(e.setupDone)
	return true
}

// runCleanup is idempotent but must not be scheduled concurrently by a native
// owner. A duplicate invocation joins real completion instead of releasing the
// ownership slot early. All callbacks receive the one context supplied by core.
func (e *scopeEffect) runCleanup(ctx context.Context) error {
	<-e.setupDone
	return e.cleanupOnce(ctx)
}

func (e *scopeEffect) cleanupOnce(ctx context.Context) error {
	if !e.cleaning.CompareAndSwap(false, true) {
		<-e.done
		return e.cleanupErr
	}
	e.cleanupErr = e.cleanupNode(ctx, e.root)
	close(e.done)
	return e.cleanupErr
}

func (e *scopeEffect) cleanupNode(ctx context.Context, node *scopeNode) error {
	e.mu.Lock()
	if node.state == scopeFinished {
		e.mu.Unlock()
		return nil
	}
	node.state, node.sealed = scopeCleaning, true
	dispose := node.dispose
	children := append([]*scopeNode(nil), node.children...)
	e.publishLocked()
	e.mu.Unlock()
	var result error
	for i := len(children) - 1; i >= 0; i-- {
		result = errors.Join(result, e.cleanupNode(ctx, children[i]))
	}
	if dispose != nil {
		result = errors.Join(result, invokeScope(func() error { return dispose(ctx) }))
	}
	// Cancellation is diagnostic, never evidence that callbacks have returned.
	result = errors.Join(result, ctx.Err())
	e.mu.Lock()
	node.dispose = nil // Keep metadata, not the resource's completed callback.
	node.state = scopeFinished
	e.publishLocked()
	e.mu.Unlock()
	return result
}

func invokeScope(fn func() error) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = &scopePanicError{Value: value}
		}
	}()
	return fn()
}

func validateScope(label string, setup func(*effectScope) error) error {
	if label == "" || setup == nil {
		return errors.New("effect scope requires a nonempty label and nonnil setup")
	}
	return nil
}

func (e *scopeEffect) setState(node *scopeNode, state string, sealed bool) {
	e.mu.Lock()
	node.state, node.sealed = state, sealed
	e.publishLocked()
	e.mu.Unlock()
}

func (e *scopeEffect) markWithdraw() {
	e.withdraw.Store(true)
	e.mu.Lock()
	e.publishLocked()
	e.mu.Unlock()
}

func (e *scopeEffect) pendingWithdraw() bool { return e.withdraw.Load() }
func (e *scopeEffect) setupFinished() bool   { return scopeClosed(e.setupDone) }
func (e *scopeEffect) cleanupFinished() bool { return scopeClosed(e.done) }

func scopeClosed(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func (e *scopeEffect) publishLocked() {
	meta := scopeMetadata(e.root)
	if e.withdraw.Load() && meta.State != scopeCleaning && meta.State != scopeFinished {
		meta.State = scopeWithdraw
	}
	e.meta.Store(&meta)
}

func scopeMetadata(node *scopeNode) effectMeta {
	meta := effectMeta{Label: node.label, State: node.state}
	for _, child := range node.children {
		meta.Children = append(meta.Children, scopeMetadata(child))
	}
	return meta
}

func (e *scopeEffect) metadata() effectMeta { return cloneEffectMeta(*e.meta.Load()) }

func cloneEffectMeta(meta effectMeta) effectMeta {
	children := meta.Children
	meta.Children = nil
	for _, child := range children {
		meta.Children = append(meta.Children, cloneEffectMeta(child))
	}
	return meta
}
func (o *owner) scopeWork(f *fiber) bool {
	for _, effect := range f.effects {
		e := effect.scope
		if e == nil || e.cleanupFinished() {
			continue
		}
		withdraw := e.setupFinished() && e.pendingWithdraw()
		if e.setupFinished() && !withdraw {
			continue
		}
		f.phase, f.scopeRunning = phScoping, e
		o.touch(f)
		run := new(handshake)
		f.run = run
		o.busy++
		budget := o.r.cfg.loadDeadline
		if withdraw {
			budget = o.r.cfg.disposeDeadline
		}
		o.deadline(f, time.Now().Add(budget))
		a, id := f.act, f.id
		o.event(f, "scope:start", effect.label, 0)
		go func() {
			var err error
			if withdraw {
				ctx, cancel := context.WithTimeout(context.Background(), o.r.cfg.disposeDeadline)
				err = e.runCleanup(ctx)
				cancel()
			} else {
				err = scopeSetupResult(e, e.runSetup(a.ctx.std, func() (context.Context, context.CancelFunc) {
					return context.WithTimeout(context.Background(), o.r.cfg.disposeDeadline)
				}))
			}
			at := run.finish()
			o.r.completed(message{kind: cmdScopeDone, id: id, act: a, scope: e, withdraw: withdraw, err: err, errText: errorText(err), at: at})
		}()
		return true
	}
	return false
}

// scopeSetupResult is what the scope worker reports for the result of runSetup.
// Quiescence can seal an admitted slot between scheduling and worker entry: no
// setup callback ran or failed then, so the worker reports success.
func scopeSetupResult(e *scopeEffect, err error) error {
	if err == errScopeStarted && e.cleanupFinished() {
		return nil
	}
	return err
}

func (o *owner) scopeDone(m message) {
	// f stays in phScoping, with this activation and this scope, until this
	// message: scopeWork starts one scope at a time and only scopeDone leaves the
	// phase. A deadline or a quiescence marks f but never replaces it.
	f := o.lookup(m.id)
	f.scopeRunning, f.run = nil, nil
	f.phase = phIdle
	o.touch(f)
	o.busy--
	o.deadline(f, time.Time{})
	o.block(f, nil)
	if m.err != nil {
		f.scopeText = joinText(f.scopeText, m.errText)
		o.event(f, "scope:failed", m.scope.metadata().Label, 0)
	} else {
		o.event(f, "scope:complete", m.scope.metadata().Label, 0)
	}
	if m.scope.cleanupFinished() {
		f.effects = slices.DeleteFunc(f.effects, func(e effectRec) bool { return e.scope == m.scope })
	}
	if f.invalid || f.remove || f.failure != nil {
		f.publishActive = false
		f.phase = phDraining
		o.touch(f)
		o.busy++
		o.state(f, Unloading)
		o.armDrain(f)
	}
	o.enqueue(f)
}

func (o *owner) trackedDone(m message) {
	// The record exists and its activation is still f's: a worker reports once,
	// and a quiesced activation is drained only after its last record is deleted
	// here (ownBusy), so f is neither replaced nor removed before this message.
	a := m.act
	label := a.tracked[m.id].label
	delete(a.tracked, m.id)
	f := o.lookup(a.ctx.fiber)
	o.event(f, "go:done", label, 0)
	if m.err != nil && !f.invalid && !f.remove && !a.selfDispose.Load() && f.target != nil && slices.Equal(f.target, a.epoch) {
		// A detached worker may fail after Apply became ACTIVE. Treat this as an
		// activation failure, but never poison a newer desired epoch with an old
		// completion. LOADING retains its single in-flight Apply until it returns.
		f.awaitErr, f.awaitText = m.err, m.errText
		o.fail(f, m.err, m.errText)
		if f.id == 0 {
			// Host work failure retires only the root-owned lifetime. Ordinary
			// unrelated fibers remain usable until an explicit close.
			o.requestDispose(f)
		}
		o.quiesce(f)
	}
	o.enqueue(f)
}
