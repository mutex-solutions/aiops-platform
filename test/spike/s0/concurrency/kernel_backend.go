// SPDX-License-Identifier: Apache-2.0

package concurrency

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/test/spike/s0/internal/eventqueue"
)

// The kernel backend: Runtime and its optional interfaces over the PUBLIC API of
// sdk/kernel, the final port of the reconciler. It reaches no kernel internal and
// adds no lifecycle behaviour of its own: every operation delegates to a
// kernel.Root, kernel.Fiber or kernel.Context. What the public API does not offer
// is not emulated. It returns ErrKernelUnsupported naming the missing capability,
// so a harness that needs it fails loudly instead of passing on an approximation.
//
// Mapping (spike method -> kernel API):
//
//	Mount, MountChild   -> Root.Plugin / Context.Plugin over kernel.Define(Plugin)
//	Update              -> Root.Batch(UpdateOp)   (restart with the retained config)
//	UpdateConfig        -> Fiber.Update + Fiber.Await
//	Dispose             -> Fiber.Dispose
//	AwaitFiber          -> Fiber.Await
//	Settle, Close       -> Root.Settle, Root.Close
//	Batch               -> Root.Batch(MountOp, UpdateOp, DisposeOp)
//	RootContext         -> Root.Context
//	Snapshots, Inspect  -> Root.Snapshots, Root.Inspect
//	Events              -> Root.Events
//	Notify              -> Root.Notify
//	ObserveEvents*      -> Root.Observe (launcher observers only), AwaitObservers,
//	                       ObserverStatus; RetentionStatus -> Root.RetentionStatus
//	Context.Read        -> Key[any].Of       Context.Provide -> Key[any].Provide
//	Context.Defer       -> Context.Defer     Context.Go      -> Context.Go
//	Context.Effect      -> Context.Defer after the setup (the spike's own sequence)
//	Context.Dispose     -> Context.Dispose   Context.RequestEffectScope -> the same
//	Context.Enter/Leave -> Context.Enter (the leave func of one activation is shared)
//
// Three things are adapter bookkeeping, not kernel behaviour, and each is stated
// where it is done:
//
//   - the kernel installs built-in services and the adapter registers three kernel
//     hook listeners on the root, so the kernel ledger starts after a fixed prefix
//     of events and provisions. The adapter hides that prefix: event sequences and
//     provision UIDs are reported relative to the state after construction;
//   - Record, Spec.Check and Configuration have no kernel counterpart of the same
//     shape, and are mapped as described at record, plan and configure below;
//   - ApplyEffect is an approximation (see plan).

// ErrKernelUnsupported is matched (errors.Is) by every refusal of a capability
// that the public sdk/kernel API does not export.
var ErrKernelUnsupported = errors.New("capability not exported by the public sdk/kernel API")

func kernelGap(what string) error { return fmt.Errorf("%w: %s", ErrKernelUnsupported, what) }

// kernelSentinels pairs each spike sentinel with the kernel's.
var kernelSentinels = []struct{ spike, native error }{
	{ErrInactive, kernel.ErrInactive}, {ErrInactiveEffect, kernel.ErrInactiveEffect},
	{ErrClosed, kernel.ErrClosed}, {ErrUnknown, kernel.ErrUnknown}, {ErrExpired, kernel.ErrExpired},
	{ErrRootDispose, kernel.ErrRootDispose}, {ErrEventsTruncated, kernel.ErrEventsTruncated},
	{ErrObserverOverflow, kernel.ErrObserverOverflow}, {eventqueue.ErrReentrant, kernel.ErrReentrant},
	{ErrScopeSealed, kernel.ErrScopeSealed}, {ErrScopeStarted, kernel.ErrScopeStarted},
	{ErrAlreadyContinued, kernel.ErrNext},
}

// kernelError keeps the kernel's text and identity and also matches the spike
// sentinels, and extracts the spike error types, that the harnesses classify by.
// It is deliberately a leaf for errors.Unwrap and reaches the kernel's chain through
// Is and As: the replay interpreter explains a diagnostic by comparing the leaves
// of its chain with the failures it recorded, which are the very values that the
// adapter returned, so these must be leaves (a group is kept as a group below).
type kernelError struct{ err error }

func (e *kernelError) Error() string { return e.err.Error() }
func (e *kernelError) Is(target error) bool {
	if errors.Is(e.err, target) {
		return true
	}
	for _, pair := range kernelSentinels {
		if target == pair.spike {
			return errors.Is(e.err, pair.native)
		}
	}
	return false
}

func (e *kernelError) As(target any) bool {
	if errors.As(e.err, target) {
		return true
	}
	switch t := target.(type) {
	case **ScopePanicError:
		var native *kernel.ScopePanicError
		if errors.As(e.err, &native) {
			*t = &ScopePanicError{Value: native.Value}
			return true
		}
	case **ObserverOverflow:
		var native *kernel.ObserverOverflow
		if errors.As(e.err, &native) {
			*t = &ObserverOverflow{Label: native.Label, Lost: native.Lost, Through: native.Through, Limit: native.Limit}
			return true
		}
	}
	return false
}

// kernelGroup keeps the structure of a joined error, whose parts are mapped one by one.
type kernelGroup struct {
	err   error
	parts []error
}

func (e *kernelGroup) Error() string   { return e.err.Error() }
func (e *kernelGroup) Unwrap() []error { return e.parts }

// fromKernel is the error a spike caller sees for err. An error that the adapter
// already produced, and a plain leaf that carries no kernel identity (a value that
// the harness itself returned from Apply, say), keep their own identity, which the
// replay compares. A joined error keeps its parts. Any other error becomes a leaf
// that matches both the kernel's and the spike's sentinels.
func fromKernel(err error) error {
	switch err.(type) {
	case nil:
		return nil
	case *kernelError, *kernelGroup:
		return err
	}
	if inactive := spikeInactive(err); inactive != nil {
		return inactive
	}
	if group, ok := err.(interface{ Unwrap() []error }); ok {
		parts := group.Unwrap()
		mapped := make([]error, len(parts))
		for i, part := range parts {
			mapped[i] = fromKernel(part)
		}
		return &kernelGroup{err: err, parts: mapped}
	}
	if !kernelOrigin(err) && !kernelIdentity(err) {
		return err
	}
	if d := diagnosticCause(err); d != nil {
		return &kernelDiagnostic{kernelError{err}, fromKernel(d)}
	}
	return &kernelError{err}
}

// kernelDiagnostic is the "fiber N: text" entry of a Settle or Close error, as the
// prototype's diagnostic is: it unwraps to the failure that it reports, so that a
// caller can recognise a failure it injected, which a plain kernelError hides.
type kernelDiagnostic struct {
	kernelError
	cause error
}

func (e *kernelDiagnostic) Unwrap() error { return e.cause }

// diagnosticCause returns the failure behind a kernel diagnostic entry, nil for
// any other error. The kernel's type is unexported, so it is recognised by name.
func diagnosticCause(err error) error {
	if t := reflect.TypeOf(err); t.Kind() != reflect.Pointer || t.Elem().Name() != "diagnostic" {
		return nil
	}
	return errors.Unwrap(err)
}

// kernelOrigin reports whether err is a value of a type of the kernel package.
func kernelOrigin(err error) bool {
	t := reflect.TypeOf(err)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.PkgPath() == reflect.TypeFor[kernel.Root]().PkgPath()
}

// spikeInactive maps a refusal that consists of nothing but the kernel's inactive
// sentinels to the same refusal made of the spike's, by identity: the replay
// recognizes a late registration by those exact values. Nil for any other error.
func spikeInactive(err error) error {
	var effect, inactive bool
	var visit func(error) bool
	visit = func(e error) bool {
		switch e {
		case kernel.ErrInactiveEffect:
			effect = true
		case kernel.ErrInactive:
			inactive = true
		default:
			group, ok := e.(interface{ Unwrap() []error })
			if !ok {
				return false
			}
			for _, part := range group.Unwrap() {
				if !visit(part) {
					return false
				}
			}
		}
		return true
	}
	switch {
	case !visit(err):
		return nil
	case effect && inactive:
		return errors.Join(ErrInactiveEffect, ErrInactive)
	case effect:
		return ErrInactiveEffect
	case inactive:
		return ErrInactive
	}
	return nil
}

// kernelIdentity reports whether err is one of the kernel's own sentinels or types.
func kernelIdentity(err error) bool {
	for _, pair := range kernelSentinels {
		if errors.Is(err, pair.native) {
			return true
		}
	}
	var panicErr *kernel.ScopePanicError
	var overflow *kernel.ObserverOverflow
	return errors.As(err, &panicErr) || errors.As(err, &overflow)
}

// fromPanic turns a refused registration (the kernel panics with an error value
// that fails the fiber) into the error the spike Context returns.
func fromPanic(p any) error {
	if err, ok := p.(error); ok {
		// The kernel's refusal wrapper is unexported and repeats the text of its
		// cause: the error the spike sees is that cause.
		if cause := errors.Unwrap(err); cause != nil && cause.Error() == err.Error() {
			err = cause
		}
		return fromKernel(err)
	}
	return fmt.Errorf("kernel refused the call: %v", p)
}

// UnsupportedGap probes r with the read-back capabilities every harness needs
// and reports the ones its adapter refuses with ErrKernelUnsupported. Empty means
// none was refused (the two prototype cores never are, and the kernel backend no
// longer is). A harness uses it to register a backend and skip it with the gap
// printed, never to pass it. S0_RUN_UNSUPPORTED=1 turns the probe off.
func UnsupportedGap(ctx context.Context, r Runtime) string {
	if os.Getenv("S0_RUN_UNSUPPORTED") == "1" {
		return ""
	}
	var gaps []string
	note := func(err error) {
		if errors.Is(err, ErrKernelUnsupported) {
			gaps = append(gaps, err.Error())
		}
	}
	_, err := r.Snapshots(ctx)
	note(err)
	_, err = r.Events(ctx)
	note(err)
	if in, ok := r.(Inspector); ok {
		_, err = in.Inspect(ctx)
		note(err)
	}
	if o, ok := r.(ObserverRuntime); ok {
		note(o.AwaitObservers(ctx))
	}
	return strings.Join(gaps, "; ")
}

// kernelBase is the prefix that a fresh kernel and the adapter's own hook
// listeners put in the ledger and in the provision table. Both are hidden.
type kernelBase struct {
	events uint64 // Ledger events before the first harness operation.
	uids   uint64 // Provision identities before the first harness provision.
}

var (
	kernelBaseOnce sync.Once
	kernelBaseVal  kernelBase
)

// kernelBaseline measures the prefix once, on a throwaway kernel built the same way.
func kernelBaseline() kernelBase {
	kernelBaseOnce.Do(func() {
		ctx := context.Background()
		root := newKernelRoot(kernel.Config{LoadDeadline: rcLoadBudget, DrainDeadline: rcDrainBudget, DisposeDeadline: rcDisposeBudget,
			ArchiveLimit: 1, EventWindow: 1, ObserverQueue: 1, OwnerInbox: 1, TerminalLimit: 1})
		view, err := root.Inspect(ctx)
		if err != nil {
			panic(err)
		}
		kernelBaseVal = kernelBase{events: root.RetentionStatus().EventsTotal, uids: uint64(len(view.Provisions))}
		if err := root.Close(ctx); err != nil {
			panic(err)
		}
	})
	return kernelBaseVal
}

type kernelFiber struct {
	f    *kernel.Fiber
	cell *Configuration // The spike configuration cell of the spec, nil for none.
}

// evictedSet records, one bit per fiber id, the fibers the adapter forgot after
// the kernel evicted their archive record, so a later lookup reports ErrExpired as
// the kernel does, not ErrUnknown.
type evictedSet []uint64

func (e *evictedSet) add(id uint64) {
	for uint64(len(*e))*64 <= id {
		*e = append(*e, 0)
	}
	(*e)[id/64] |= 1 << (id % 64)
}

func (e evictedSet) has(id uint64) bool {
	return id/64 < uint64(len(e)) && e[id/64]&(1<<(id%64)) != 0
}

type kernelRuntime struct {
	root     *kernel.Root
	cfg      kernel.Config
	base     kernelBase
	window   int // The reported event window: the kernel's, less the prefix.
	host     atomic.Pointer[Context]
	hostMu   sync.Mutex
	closed   atomic.Bool
	rootDone atomic.Bool // Close has returned.
	keys     sync.Map    // string -> kernel.Key[any], one per name for this instance.
	mu       sync.RWMutex
	fibers   map[uint64]kernelFiber
	evicted  evictedSet // Ids forgot after the kernel evicted their record.
	pruneAt  int        // Size of fibers at which the next prune runs.

	recMu   sync.Mutex
	records map[uint64]map[string]any // Record payloads by the kernel sequence of their marker.

	// The kernel's events carry no provided value, so the adapter, which performs
	// every Provide, keeps the value of each provision by the provision identity
	// that the kernel gave it and joins it to the events that name that identity.
	valMu    sync.Mutex
	vals     map[uint64]any
	pending  map[string][]pendingValue // Values whose provide event no export has met yet, by key.
	npending int
	keyLocks sync.Map // string -> *sync.Mutex: one Provide per key at a time, so no other provide event of the key falls in a pendingValue's range.
}

// pendingValue is a provided value waiting for its provide event: the event's
// sequence lies in (lo, hi], the ledger totals read just before and just after the
// Provide call that appended it.
type pendingValue struct {
	value  any
	lo, hi uint64
}

var (
	_ Runtime                 = (*kernelRuntime)(nil)
	_ Notifier                = (*kernelRuntime)(nil)
	_ RootProvider            = (*kernelRuntime)(nil)
	_ Waiter                  = (*kernelRuntime)(nil)
	_ ConfigUpdater           = (*kernelRuntime)(nil)
	_ ConfigRequester         = (*kernelRuntime)(nil)
	_ Inspector               = (*kernelRuntime)(nil)
	_ Batcher                 = (*kernelRuntime)(nil)
	_ OverflowObserverRuntime = (*kernelRuntime)(nil)
	_ Recorder                = (*kernelRuntime)(nil)
	_ RetentionReporter       = (*kernelRuntime)(nil)
)

// NewKernel is the final kernel with the prototype reconciler's default bounds.
func NewKernel() Runtime { return NewKernelWith(Retention{}) }

// NewRecordingKernel asks for complete history. The kernel has no recorder
// (ADR-034 owner decision 5), so this only widens the event window to the cap.
func NewRecordingKernel() Runtime { return NewKernelWith(Recording()) }

// NewKernelWith maps the spike's Retention onto kernel.Config. The deadlines, the
// owner inbox and the terminal limit are the reconciler's constants. The event
// window is widened by the hidden prefix, so that it holds as many harness events
// as the spike's.
func NewKernelWith(retention Retention) Runtime {
	retention = retention.normalized()
	base := kernelBaseline()
	window := max(retention.EventWindow, retention.RecordLimit)
	cfg := kernel.Config{
		LoadDeadline: rcLoadBudget, DrainDeadline: rcDrainBudget, DisposeDeadline: rcDisposeBudget,
		ArchiveLimit: retention.ArchiveLimit, EventWindow: window + int(base.events),
		ObserverQueue: retention.ObserverBacklog, OwnerInbox: 256, TerminalLimit: rcTerminalLimit,
	}
	return &kernelRuntime{root: newKernelRoot(cfg), cfg: cfg, base: base, window: window,
		fibers: make(map[uint64]kernelFiber), records: make(map[uint64]map[string]any), vals: make(map[uint64]any)}
}

// newKernelRoot builds the kernel. Both binding maps stay empty: every key of this
// adapter is open. The configuration hooks are listened on from the root context,
// which the kernel admits to every sealed hook without a Config.Listeners entry
// (ADR-034 owner decision 11): the adapter is the launcher here, and an entry would
// need a mounted plugin of its own, and with it fibers and events that no fixture
// mounted. The listeners dispatch by the cell that each raw config carries.
func newKernelRoot(cfg kernel.Config) *kernel.Root {
	root, err := kernel.New(cfg)
	if err != nil {
		panic(err)
	}
	rc := root.Context()
	kernel.InternalConfig.On(rc, kernel.DefaultStage, kernelResolveHook)
	kernel.InternalCommit.On(rc, kernel.DefaultStage, kernelCommitHook)
	kernel.InternalUpdate.On(rc, kernel.DefaultStage, kernelUpdateHook)
	return root
}

func (r *kernelRuntime) key(name string) kernel.Key[any] {
	if k, ok := r.keys.Load(name); ok {
		return k.(kernel.Key[any])
	}
	k, _ := r.keys.LoadOrStore(name, kernel.NewKey[any](name, 0))
	return k.(kernel.Key[any])
}

func (r *kernelRuntime) lookup(id uint64) (kernelFiber, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.fibers[id]
	return f, ok
}

// missing is the error for an id the adapter does not hold: ErrExpired for a fiber
// whose kernel record was evicted, ErrUnknown for one never issued.
func (r *kernelRuntime) missing(id uint64) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.evicted.has(id) {
		return ErrExpired
	}
	return ErrUnknown
}

func kernelDuplicates(keys []string) bool {
	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		if k == "" || seen[k] {
			return true
		}
		seen[k] = true
	}
	return false
}

// kernelCheck carries one consumer's Spec.Check to the providers of its needs. It
// is the per-consumer intercept config of each of the consumer's needs, and the
// provider's Available reads the nearest one back from the caller it is given.
type kernelCheck struct {
	check func(*Context) bool
	mu    sync.Mutex
	last  *kernel.Context // One evaluation hands every need the same caller.
	ok    bool
}

var kernelNoCheck = &kernelCheck{}

// allows runs the consumer's predicate once per evaluation, not once per need.
func (c *kernelCheck) allows(r *kernelRuntime, caller *kernel.Context) bool {
	if c.check == nil {
		return true
	}
	c.mu.Lock()
	if c.last == caller {
		defer c.mu.Unlock()
		return c.ok
	}
	c.mu.Unlock()
	ok := c.check(r.context(caller, nil))
	c.mu.Lock()
	c.last, c.ok = caller, ok
	c.mu.Unlock()
	return ok
}

// available is the Available option every provision of this adapter carries.
//
// Spec.Check is consumer-side; the kernel only has the provider-side
// Available(fn(caller)). The mapping is exact for a consumer that has at least one
// need: the kernel's availability check of a consumer is the conjunction, over its
// required needs, of each provider's Available(caller) for that consumer, so a
// predicate that every provider evaluates as "the consumer's own Check" gives the
// conjunction of one predicate, which is that predicate. Check is evaluated once
// per evaluation (kernelCheck.allows), as the spike does. A Check on a consumer
// with no need has no provider to carry it and is refused in plan.
func (r *kernelRuntime) available(key kernel.Key[any]) kernel.ProvideOption {
	return kernel.Available(func(caller *kernel.Context) bool {
		chain := kernel.Intercepts[*kernelCheck](caller, key)
		return len(chain) == 0 || chain[len(chain)-1].allows(r, caller)
	})
}

// kernelCfg is the raw config that the adapter hands to the kernel: the value the
// spike's cell holds, with the cell it belongs to (the kernel hooks carry no fiber)
// and the spike's noSave flag (the kernel has no persistence suppression).
type kernelCfg struct {
	cell   *Configuration
	raw    any
	noSave bool

	mu        sync.Mutex
	prefix    chan struct{} // Closed when the update hook has been entered: the end of the synchronous prefix.
	once      sync.Once
	version   uint64 // The cell's version and publication generation right after this request retained its raw config.
	published uint64
}

func (c *kernelCfg) prefixDone() {
	if c.prefix != nil {
		c.once.Do(func() { close(c.prefix) })
	}
}

// kernelResolved is what Plugin.Decode returns and the update waterfall carries.
type kernelResolved struct {
	cell  *Configuration
	value any
	req   *kernelCfg // The request that produced it, nil on an activation.
}

func kernelDecode(v any) (any, error) {
	switch x := v.(type) {
	case *kernelResolved:
		return x, nil
	case *kernelCfg:
		return &kernelResolved{cell: x.cell, value: x.raw, req: x}, nil
	}
	return nil, fmt.Errorf("kernel backend: unexpected raw configuration %T", v)
}

// configure maps ConfigHooks onto the kernel hooks internal/config (Resolve),
// internal/commit (Commit) and internal/update (Update). One difference is the
// kernel's and is not hidden: internal/update runs its listener to completion
// inside Fiber.Update, so UpdateAsync, whose waiter outlives the listener, is
// refused in plan.
func kernelResolveHook(ctx context.Context, a any, next kernel.Next[any]) (any, error) {
	c, ok := a.(*kernelCfg)
	if !ok || c.cell.hooks.Resolve == nil {
		return next()
	}
	value, err := c.cell.ResolveCandidate(ctx, c.raw)
	if err != nil {
		return nil, err
	}
	return &kernelResolved{cell: c.cell, value: value, req: c}, nil
}

func kernelCommitHook(ctx context.Context, a any, next kernel.Next[bool]) (bool, error) {
	c, ok := a.(*kernelCfg)
	if !ok {
		return next()
	}
	if !c.noSave && c.cell.hooks.Commit != nil {
		// A failing Commit aborts the update before anything is retained, as the
		// spike's cell does (ADR-034 owner decision 15).
		if err := c.cell.hooks.Commit(ctx, c.raw); err != nil {
			return false, err
		}
	}
	// The kernel retains the raw config right after this waterfall: mirror it in
	// the cell, which is what Snapshots reports.
	version, publication := c.cell.retain(c.raw)
	c.mu.Lock()
	c.version, c.published = version, publication
	c.mu.Unlock()
	return next()
}

func kernelUpdateHook(ctx context.Context, a any, next kernel.Next[any]) (any, error) {
	res, ok := a.(*kernelResolved)
	if !ok || (res.cell.hooks.Update == nil && res.cell.hooks.UpdateAsync == nil) {
		return next()
	}
	var continued atomic.Bool
	nextOnce := func() error {
		continued.Store(true)
		_, err := next()
		return fromKernel(err)
	}
	var err error
	if hook := res.cell.hooks.UpdateAsync; hook != nil {
		// The spike's hook returns after its synchronous prefix and completes later,
		// possibly calling next then. The kernel's listener may call next until it
		// returns, so it waits here for the completion; Fiber.Update is then run on a
		// goroutine of the adapter (RequestUpdateConfig), which returns at this point.
		var wait func(context.Context) error
		wait, err = hook(ctx, res.value, nextOnce)
		res.req.prefixDone()
		if err == nil {
			err = wait(ctx)
		}
	} else {
		err = res.cell.hooks.Update(ctx, res.value, nextOnce)
	}
	if err == nil && !continued.Load() {
		// A veto publishes the candidate, as the spike's cell does, unless a later
		// raw retention or publication made this request stale.
		res.req.mu.Lock()
		version, publication := res.req.version, res.req.published
		res.req.mu.Unlock()
		res.cell.publishIf(version, publication, res.value)
	}
	return nil, err
}

// ownListener removes the kernel's naming of the adapter's own listener, which is
// registered on the root (ADR-034 owner decision 12: a waterfall fails with an
// error that names its listener). The hook is the spike's, and its error is what
// the spike's cell returns.
func ownListener(err error) error {
	var named *kernel.ListenerError
	if errors.As(err, &named) && named.PluginID == "root" {
		return named.Cause
	}
	return err
}

// publishIf is publish if no raw retention and no publication came after the request.
func (c *Configuration) publishIf(version, publication uint64, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.version == version && c.publication == publication {
		c.effective, c.prepared = value, true
		c.publication++
	}
}

// publish stores value as the effective config, as Prepare does after a resolve.
func (c *Configuration) publish(value any) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.effective, c.prepared = value, true
	c.publication++
	return c.version
}

// plan turns one Spec into what Root.Plugin takes: the defined plugin and its raw
// config.
func (r *kernelRuntime) plan(spec Spec) (kernel.Plugin, any, error) {
	switch {
	case spec.Apply != nil && spec.ApplyEffect != nil:
		return kernel.Plugin{}, nil, errors.New("Apply and ApplyEffect are mutually exclusive")
	case kernelDuplicates(spec.Needs) || kernelDuplicates(spec.Provides):
		return kernel.Plugin{}, nil, errors.New("empty or duplicate declaration")
	case spec.Check != nil && len(spec.Needs) == 0:
		return kernel.Plugin{}, nil, kernelGap("Spec.Check without a need: the kernel evaluates availability per required key, through the provider's Available, so a consumer with no need has no check")
	}
	holder := kernelNoCheck
	if spec.Check != nil {
		holder = &kernelCheck{check: spec.Check}
	}
	p := kernel.Plugin{ID: spec.ID}
	for _, n := range spec.Needs {
		k := r.key(n)
		p.Inject = append(p.Inject, kernel.NeedWith(k, holder))
	}
	for _, n := range spec.Provides {
		p.Provides = append(p.Provides, r.key(n))
	}
	var raw any
	cell := spec.Config
	if cell != nil {
		cfgRaw, _, _ := cell.RawSnapshot()
		raw = &kernelCfg{cell: cell, raw: cfgRaw}
		p.Decode = kernelDecode
	}
	// activation is the config of one activation: the cell publishes the effective
	// value before Apply, as the spike's Prepare does.
	activation := func(kc *kernel.Context, cfg any) *Context {
		if cell == nil {
			return r.context(kc, nil)
		}
		res := cfg.(*kernelResolved)
		c := r.context(kc, res.value)
		c.configVersion = cell.publish(res.value)
		return c
	}
	switch {
	case spec.Apply != nil:
		p.Apply = func(kc *kernel.Context, cfg any) error { return spec.Apply(activation(kc, cfg)) }
	case spec.ApplyEffect != nil:
		// APPROXIMATION. The core collects a returned disposer after the load
		// completes, even for a superseded or self-disposing load. The public API
		// has no such return, so the disposer is registered as the last effect of
		// Apply: the same ledger slot, but registered inside Apply, so a load that
		// was invalidated meanwhile runs it at once instead of in the teardown
		// walk (needs Plugin.ApplyEffect for the exact behaviour). When the activation
		// is already inactive, which a body that requested its own disposal made it,
		// Effect refuses before it runs setup: the adapter then runs the disposer at
		// once, as Effect does when it refuses after setup, and reports no error, as
		// the prototypes do for a returned disposer of an invalidated load. The
		// disposer then runs before the unload events, not after them.
		p.Apply = func(kc *kernel.Context, cfg any) error {
			d, err := spec.ApplyEffect(activation(kc, cfg))
			if err != nil || d == nil {
				return err
			}
			ran := false
			err = kc.Effect("apply", func() (kernel.Disposer, error) { ran = true; return kernel.Disposer(d), nil })
			if err == nil {
				return nil
			}
			if !ran && spikeInactive(err) != nil {
				return r.cleanup(d)
			}
			if spikeInactive(err) != nil {
				return nil
			}
			return err
		}
	}
	return kernel.Define(p), raw, nil
}

func (r *kernelRuntime) register(f *kernel.Fiber, spec Spec) uint64 {
	id := f.ID()
	r.mu.Lock()
	r.fibers[id] = kernelFiber{f: f, cell: spec.Config}
	due := len(r.fibers) >= max(r.pruneAt, 2*r.cfg.ArchiveLimit+1024)
	r.mu.Unlock()
	if due {
		r.pruneFibers()
	}
	return id
}

// pruneFibers forgets the fibers whose kernel record was evicted: the kernel
// answers ErrExpired for them, so holding their handles (and through them their
// plugins and configs) would only grow the adapter's heap with every fiber ever
// mounted, which the reconciler, whose archive is bounded, does not. The next
// prune runs when the map has doubled, so the cost is amortised O(1) per mount.
func (r *kernelRuntime) pruneFibers() {
	r.mu.RLock()
	ids := make([]uint64, 0, len(r.fibers))
	for id := range r.fibers {
		ids = append(ids, id)
	}
	r.mu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), rcLoadBudget)
	defer cancel()
	var gone []uint64
	for _, id := range ids {
		f, ok := r.lookup(id)
		if !ok {
			continue
		}
		if _, err := f.f.State(ctx); errors.Is(err, kernel.ErrExpired) {
			gone = append(gone, id)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range gone {
		delete(r.fibers, id)
		r.evicted.add(id)
	}
	r.pruneAt = 2 * len(r.fibers)
}

// mount mounts spec under parent (the root context for Mount).
func (r *kernelRuntime) mount(parent *kernel.Context, spec Spec) (id uint64, err error) {
	if r.closed.Load() {
		return 0, ErrClosed
	}
	plugin, raw, err := r.plan(spec)
	if err != nil {
		return 0, err
	}
	defer func() {
		if p := recover(); p != nil {
			id, err = 0, fromPanic(p)
		}
	}()
	return r.register(parent.Plugin(plugin, raw), spec), nil
}

func (r *kernelRuntime) Mount(ctx context.Context, spec Spec) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return r.mount(r.root.Context(), spec)
}

func (r *kernelRuntime) Update(ctx context.Context, id uint64) error {
	f, ok := r.lookup(id)
	if !ok {
		return ErrUnknown // The kernel answers an update of an evicted fiber as unknown too.
	}
	if r.closed.Load() {
		return ErrClosed
	}
	_, err := r.root.Batch(ctx, kernel.UpdateOp(f.f))
	return fromKernel(err)
}

func (r *kernelRuntime) Dispose(ctx context.Context, id uint64) error {
	f, ok := r.lookup(id)
	if !ok {
		if errors.Is(r.missing(id), ErrExpired) {
			return nil // Disposing an evicted fiber is a no-op in the kernel.
		}
		return ErrUnknown // Never issued by this runtime.
	}
	return fromKernel(f.f.Dispose(ctx))
}

func (r *kernelRuntime) AwaitFiber(ctx context.Context, id uint64) error {
	f, ok := r.lookup(id)
	if !ok {
		return r.missing(id)
	}
	return fromKernel(f.f.Await(ctx))
}

func (r *kernelRuntime) Settle(ctx context.Context) error { return fromKernel(r.root.Settle(ctx)) }

func (r *kernelRuntime) Close(ctx context.Context) error {
	r.closed.Store(true)
	err := r.root.Close(ctx)
	r.rootDone.Store(true)
	return fromKernel(err)
}

func (r *kernelRuntime) RootContext(context.Context) (*Context, error) {
	if r.closed.Load() {
		return nil, ErrClosed
	}
	r.hostMu.Lock()
	defer r.hostMu.Unlock()
	if c := r.host.Load(); c != nil {
		return c, nil
	}
	c := r.context(r.root.Context(), nil)
	r.host.Store(c)
	return c, nil
}

func (r *kernelRuntime) UpdateConfig(ctx context.Context, id uint64, raw any, noSave bool) error {
	request, err := r.RequestUpdateConfig(ctx, id, raw, noSave)
	if err != nil {
		return err
	}
	return request.Await(ctx)
}

func (r *kernelRuntime) RequestUpdateConfig(ctx context.Context, id uint64, raw any, noSave bool) (ConfigRequest, error) {
	none := ConfigRequest{Await: func(context.Context) error { return nil }}
	f, ok := r.lookup(id)
	switch {
	case !ok:
		return none, r.missing(id)
	case f.cell == nil:
		return none, errors.New("fiber has no configuration")
	case r.closed.Load():
		return none, ErrClosed
	}
	cfg := &kernelCfg{cell: f.cell, raw: raw, noSave: noSave}
	result := func(err error) error { return fromKernel(ownListener(err)) }
	if f.cell.hooks.UpdateAsync == nil {
		if err := result(f.f.Update(ctx, cfg)); err != nil {
			return none, err
		}
		return ConfigRequest{Await: func(w context.Context) error { return fromKernel(f.f.Await(w)) }}, nil
	}
	// An asynchronous update hook outlives the listener's entry (kernelUpdateHook),
	// so Fiber.Update, which runs the listener, runs on its own goroutine here and
	// the request returns once the listener has been entered, or Update has returned.
	cfg.prefix = make(chan struct{})
	finished, updateErr := make(chan struct{}), error(nil)
	go func() {
		updateErr = f.f.Update(ctx, cfg)
		close(finished)
	}()
	select {
	case <-cfg.prefix:
	case <-finished:
		if err := result(updateErr); err != nil {
			return none, err
		}
	case <-ctx.Done():
		return none, ctx.Err()
	}
	return ConfigRequest{Await: func(w context.Context) error {
		select {
		case <-finished:
		case <-w.Done():
			return w.Err()
		}
		if err := result(updateErr); err != nil {
			return err
		}
		return fromKernel(f.f.Await(w))
	}}, nil
}

func (r *kernelRuntime) Batch(ctx context.Context, operations []BatchOperation) ([]uint64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.closed.Load() {
		return nil, ErrClosed
	}
	ops := make([]kernel.BatchOp, len(operations))
	for i, op := range operations {
		switch op.Kind {
		case BatchMount:
			if op.Fiber != 0 || op.Spec.ID == "" {
				return nil, fmt.Errorf("invalid batch Mount at operation %d", i)
			}
			p, raw, err := r.plan(op.Spec)
			if err != nil {
				return nil, fmt.Errorf("invalid batch Mount at operation %d: %w", i, err)
			}
			ops[i] = kernel.MountOp(p, raw)
		case BatchUpdate, BatchDispose:
			f, ok := r.lookup(op.Fiber)
			if op.Fiber == 0 || !ok {
				return nil, fmt.Errorf("batch operation %d: %w", i, ErrUnknown) // As the kernel answers for an evicted fiber.
			}
			if op.Spec.ID != "" || op.Spec.Apply != nil || op.Spec.ApplyEffect != nil || op.Spec.Check != nil ||
				op.Spec.Config != nil || len(op.Spec.Needs) != 0 || len(op.Spec.Provides) != 0 {
				return nil, fmt.Errorf("batch operation %d carries an unused Mount specification", i)
			}
			if op.Kind == BatchUpdate {
				ops[i] = kernel.UpdateOp(f.f)
			} else {
				ops[i] = kernel.DisposeOp(f.f)
			}
		default:
			return nil, errors.New("unknown batch operation kind")
		}
	}
	fibers, err := r.root.Batch(ctx, ops...)
	if err != nil {
		return nil, fromKernel(err)
	}
	ids := make([]uint64, len(operations))
	for i, op := range operations {
		if op.Kind == BatchMount {
			ids[i] = r.register(fibers[i], op.Spec)
		}
	}
	return ids, nil // Only Mount positions carry an ID.
}

// context builds the spike Context of one activation over its kernel.Context.
func (r *kernelRuntime) context(kc *kernel.Context, cfg any) *Context {
	c := &Context{host: r, std: kc.Std(), config: cfg}
	c.live.Store(true) // The kernel refuses what an inactive activation may not do.
	var leave atomic.Pointer[func()]
	c.fwd = &forward{
		// The kernel returns one leave function per activation (see handle.enter),
		// so the first one seen serves every Leave.
		enter: func() bool {
			l, ok := kc.Enter()
			if ok && leave.Load() == nil {
				keepLeave(&leave, l) // out of line: taking &l here would heap-allocate l on every call
			}
			return ok
		},
		leave: func() { (*leave.Load())() },
		// The kernel owns the worker: it holds the lease, reports the result to the
		// owner and clears the tracked label itself, so a label is gone from the
		// snapshot as soon as the worker has returned, as in the prototypes.
		track: func(label string, run func(context.Context) error) (err error) {
			defer func() {
				if p := recover(); p != nil {
					err = fromPanic(p)
				}
			}()
			kc.Go(label, run)
			return nil
		},
		read: func(key string) (any, error) { return r.read(kc, key) },
		// GAP, approximated here: the kernel keeps the activation's own provisions in
		// an unexported ownership record (handle.owns) and exports no Context.Owns.
		// The adapter keeps its own set of the keys that Provide accepted. A
		// provision stops being the activation's own when its unload starts; the
		// root's does not, it stays until Close has finished.
		owns: func(key string) bool {
			if kc == r.root.Context() {
				if r.closed.Load() && r.rootDone.Load() {
					return false
				}
			} else if kc.Std().Err() != nil {
				return false
			}
			values := c.owned.Load()
			if values == nil {
				return false
			}
			_, ok := values.keys[key]
			return ok
		},
	}
	c.provide = func(key string, value any) (err error) {
		defer func() {
			if p := recover(); p != nil {
				err = fromPanic(p)
			}
		}()
		k := r.key(key)
		lock, _ := r.keyLocks.LoadOrStore(key, new(sync.Mutex))
		lock.(*sync.Mutex).Lock()
		defer lock.(*sync.Mutex).Unlock()
		lo := r.root.RetentionStatus().EventsTotal
		if err := k.Provide(kc, value, r.available(k)); err != nil {
			return fromKernel(err)
		}
		r.keep(key, value, lo, r.root.RetentionStatus().EventsTotal)
		c.publishOwned(key)
		return nil
	}
	c.effect = func(label string, d Disposer) (err error) {
		defer func() {
			if p := recover(); p != nil {
				err = fromPanic(p)
			}
		}()
		kc.Defer(label, kernel.Disposer(d))
		return nil
	}
	c.cleanupEffect = r.cleanup
	c.dispose = func() error { return fromKernel(kc.Dispose()) }
	c.scopeRequest = func(label string, setup func(*EffectScope) error) (*EffectHandle, error) {
		h, err := kc.RequestEffectScope(label, func(s *kernel.EffectScope) error { return setup(kernelScope(s)) })
		if err != nil {
			return nil, fromKernel(err)
		}
		return kernelHandle(h), nil
	}
	c.child = func(spec Spec) (uint64, error) { return r.mount(kc, spec) }
	return c
}

// scopeForward and handleForward carry a spike EffectScope and EffectHandle to the
// kernel's own (effect_scope.go forwards to them when they are set).
type scopeForward struct {
	deferTo func(string, Disposer) error
	nested  func(string, func(*EffectScope) error) error
}

type handleForward struct {
	awaitSetup func(context.Context) error
	await      func(context.Context) error
	withdraw   func() error
	metadata   func() EffectMeta
}

func kernelScope(s *kernel.EffectScope) *EffectScope {
	return &EffectScope{fwd: &scopeForward{
		deferTo: func(label string, d Disposer) error { return fromKernel(s.Defer(label, kernel.Disposer(d))) },
		nested: func(label string, setup func(*EffectScope) error) error {
			return fromKernel(s.Scope(label, func(inner *kernel.EffectScope) error { return setup(kernelScope(inner)) }))
		},
	}}
}

func kernelHandle(h *kernel.EffectHandle) *EffectHandle {
	return &EffectHandle{fwd: &handleForward{
		awaitSetup: func(ctx context.Context) error { return fromKernel(h.AwaitSetup(ctx)) },
		await:      func(ctx context.Context) error { return fromKernel(h.Await(ctx)) },
		withdraw:   func() error { return fromKernel(h.RequestWithdraw()) },
		metadata:   func() EffectMeta { return kernelMeta(h.Metadata()) },
	}}
}

func kernelMeta(m kernel.EffectMeta) EffectMeta {
	out := EffectMeta{Label: m.Label, State: m.State}
	for _, child := range m.Children {
		out.Children = append(out.Children, kernelMeta(child))
	}
	return out
}

// cleanup runs a disposer that setup returned but nothing owns, under the
// kernel's dispose deadline and off every kernel goroutine.
func (r *kernelRuntime) cleanup(d Disposer) (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), r.cfg.DisposeDeadline)
	defer cancel()
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("cleanup panic: %v", p)
		}
	}()
	return d(ctx)
}

func (r *kernelRuntime) read(kc *kernel.Context, name string) (v any, err error) {
	k, ok := r.keys.Load(name)
	if !ok {
		return nil, ErrUnknown
	}
	defer func() {
		if p := recover(); p != nil {
			v, err = nil, fromPanic(p)
			if errors.Is(err, kernel.ErrUndeclared) {
				err = ErrUnknown
			}
		}
	}()
	return k.(kernel.Key[any]).Of(kc), nil
}

// uid reports a provision identity relative to the hidden prefix.
func (r *kernelRuntime) uid(u uint64) uint64 {
	if u <= r.base.uids {
		return 0
	}
	return u - r.base.uids
}

func (r *kernelRuntime) uids(list []uint64) []uint64 {
	if list == nil {
		return nil
	}
	out := make([]uint64, len(list))
	for i, u := range list {
		out[i] = r.uid(u)
	}
	return out
}

// keep files value for the provide event that the Provide call just made: the
// kernel appended that event before Provide returned, so its sequence lies in
// (lo, hi], and the key lock keeps every other provide event of the key out of
// that range. The value is joined to its provision identity when an export first
// meets the provide event (value). Filing costs O(1); reading the ledger on every
// Provide made a mount of n fibers cost O(n x window).
//
// ponytail: a provide event that leaves the window before any export meets it
// leaves its value unreported (nil), as the event itself is lost then; pending
// values that can no longer match are dropped once the pending set outgrows twice
// the window.
func (r *kernelRuntime) keep(key string, value any, lo, hi uint64) {
	r.valMu.Lock()
	defer r.valMu.Unlock()
	if r.pending == nil {
		r.pending = make(map[string][]pendingValue)
	}
	r.pending[key] = append(r.pending[key], pendingValue{value: value, lo: lo, hi: hi})
	r.npending++
	if r.npending <= 2*r.window+256 {
		return
	}
	st := r.root.RetentionStatus()
	oldest := st.EventsTotal - uint64(st.Windowed) // Sequences at or below this are gone.
	r.npending = 0
	for k, list := range r.pending {
		kept := list[:0]
		for _, p := range list {
			if p.hi > oldest {
				kept = append(kept, p)
			}
		}
		if len(kept) == 0 {
			delete(r.pending, k)
			continue
		}
		r.pending[k] = kept
		r.npending += len(kept)
	}
}

// prune drops the values that no event of the window and no live provision can
// name any more. The caller holds valMu. The provisions are read before the events,
// so a provision that ends between the two reads still has its events in the window.
func (r *kernelRuntime) prune() {
	view, _ := r.root.Inspect(context.Background())
	events, _ := r.root.Events(context.Background())
	if len(view.Provisions) == 0 && len(events) == 0 {
		return // The kernel is closed: nothing can be told apart.
	}
	keep := make(map[uint64]bool, len(view.Provisions)+len(events))
	for _, p := range view.Provisions {
		keep[p.UID] = true
	}
	for _, e := range events {
		keep[e.UID] = true
	}
	for uid := range r.vals {
		if !keep[uid] {
			delete(r.vals, uid)
		}
	}
}

// value is the provided value that a service or provide event reports, by
// provision identity. A provide event not met before is first joined to the
// pending value whose range holds its sequence. A service event of a provision whose
// provide event no export has met (an observer registered after the Provide, an
// event that left the window) reads the value of the live provision.
func (r *kernelRuntime) value(e kernel.LedgerEvent) any {
	if e.UID == 0 || (e.Kind != "service" && e.Kind != "provide") {
		return nil
	}
	r.valMu.Lock()
	defer r.valMu.Unlock()
	if v, ok := r.vals[e.UID]; ok {
		return v
	}
	if e.Kind == "service" {
		view, _ := r.root.Inspect(context.Background())
		for _, p := range view.Provisions {
			if p.UID == e.UID {
				r.vals[e.UID] = p.Value
				return p.Value
			}
		}
		return nil
	}
	key := serviceName(e.Key)
	list := r.pending[key]
	for i, p := range list {
		if e.Sequence <= p.lo || e.Sequence > p.hi {
			continue
		}
		r.vals[e.UID] = p.value
		if rest := append(list[:i:i], list[i+1:]...); len(rest) > 0 {
			r.pending[key] = rest
		} else {
			delete(r.pending, key)
		}
		r.npending--
		if len(r.vals) > 4*r.window+256 {
			r.prune()
		}
		return p.value
	}
	return nil
}

// serviceName is the key a service event or provision names: the kernel renders it
// "name@major", and every key of this adapter has major 0.
func serviceName(key string) string { return strings.TrimSuffix(key, "@0") }

func (r *kernelRuntime) snapshot(s kernel.FiberSnapshot) Snapshot {
	out := Snapshot{ID: s.ID, Name: s.Plugin, State: State(s.State), Revision: s.Revision, Epoch: r.uids(s.Epoch),
		Effects: s.Effects, Tracked: s.Tracked, Error: s.Error, Alive: s.Alive}
	for _, m := range s.EffectTree {
		out.EffectTree = append(out.EffectTree, kernelMeta(m))
	}
	if s.Target != nil {
		out.Target = &EpochSnapshot{Revision: s.Target.Revision, UIDs: r.uids(s.Target.UIDs)}
	}
	if f, ok := r.lookup(s.ID); ok && f.cell != nil {
		// The kernel keeps the raw config and gives Apply the decoded one; what the
		// snapshot reports is the cell the adapter maintains beside it.
		v := f.cell.Snapshot()
		out.Configured, out.ConfigPrepared = true, v.Prepared
		out.RawConfig, out.EffectiveConfig, out.ConfigVersion = v.Raw, v.Effective, v.Version
	}
	return out
}

func (r *kernelRuntime) Snapshots(ctx context.Context) ([]Snapshot, error) {
	list, err := r.root.Snapshots(ctx)
	if err != nil {
		return nil, fromKernel(err)
	}
	out := make([]Snapshot, len(list))
	for i, s := range list {
		out[i] = r.snapshot(s)
	}
	return out, nil
}

// builtinKeys are the services that a kernel installs on its root.
var builtinKeys = map[string]bool{"reflect": true, "registry": true, "hooks": true, "logger": true}

func (r *kernelRuntime) Inspect(ctx context.Context) (Inspection, error) {
	view, err := r.root.Inspect(ctx)
	if err != nil {
		return Inspection{}, fromKernel(err)
	}
	out := Inspection{Sequence: view.Sequence - min(view.Sequence, r.base.events)}
	for _, s := range view.Fibers {
		out.Fibers = append(out.Fibers, r.snapshot(s))
	}
	for _, p := range view.Provisions {
		if p.Owner == 0 && p.UID <= r.base.uids && builtinKeys[serviceName(p.Key)] {
			continue
		}
		out.Provisions = append(out.Provisions, ProvisionSnapshot{Key: serviceName(p.Key), UID: r.uid(p.UID), Owner: p.Owner,
			Name: p.Plugin, Value: p.Value, Active: p.Active})
	}
	return out, nil
}

// markerKind reports the kind of a Record marker: a root effect labelled
// fixture:<kind>.
func markerKind(e kernel.LedgerEvent) (string, bool) {
	if e.Fiber != 0 || e.Kind != "effect" {
		return "", false
	}
	return strings.CutPrefix(e.Label, "fixture:")
}

// event reports e in the spike's shape, relative to the hidden prefix. The second
// result is false for an event of the prefix.
func (r *kernelRuntime) event(e kernel.LedgerEvent) (Event, bool) {
	if e.Sequence <= r.base.events {
		return Event{}, false
	}
	out := Event{Sequence: e.Sequence - r.base.events, Fiber: e.Fiber, Name: e.Plugin, Kind: e.Kind, From: State(e.From),
		State: State(e.State), UID: r.uid(e.UID), Value: r.value(e)}
	switch {
	case e.Key != "":
		out.Key = serviceName(e.Key)
	case e.Label != "":
		out.Key = e.Label
	}
	if kind, ok := markerKind(e); ok {
		r.recMu.Lock() // Waits for a Record that has put the marker in the ledger and not yet filed its payload.
		data := r.records[e.Sequence]
		r.recMu.Unlock()
		out = Event{Sequence: out.Sequence, Kind: "fixture", Key: kind, Data: data}
	}
	return out, true
}

func (r *kernelRuntime) Events(ctx context.Context) ([]Event, error) {
	list, err := r.root.Events(ctx)
	if list == nil {
		return nil, fromKernel(err)
	}
	out := make([]Event, 0, len(list))
	for _, e := range list {
		if ev, ok := r.event(e); ok {
			out = append(out, ev)
		}
	}
	// The kernel's window is the spike's plus the hidden prefix, so that it holds at
	// least as many events as the spike's; the tail is cut to the spike's window and
	// truncation is judged on the events after the prefix.
	if len(out) > r.window {
		out = out[len(out)-r.window:]
	}
	if n := len(list); n != 0 && list[n-1].Sequence-min(list[n-1].Sequence, r.base.events) > uint64(len(out)) {
		total := list[n-1].Sequence - r.base.events
		return out, fromKernel(fmt.Errorf("%w: %d events appended, last %d kept", kernel.ErrEventsTruncated, total, len(out)))
	}
	return out, nil
}

func (r *kernelRuntime) Notify(ctx context.Context, names ...string) error {
	keys := make([]kernel.AnyKey, len(names))
	for i, n := range names {
		if n == "" {
			return errors.New("empty notification key")
		}
		keys[i] = r.key(n)
	}
	return fromKernel(r.root.Notify(ctx, keys...))
}

// Record is a root effect labelled fixture:<kind>: it takes its place in the
// kernel ledger in the order in which it was registered, so the order in which
// Events and the observers show markers and kernel events is the true one. The
// payload is kept here, by the sequence of the marker. Calls are serialized so
// that the marker of one call is the only one that is not yet filed.
func (r *kernelRuntime) Record(ctx context.Context, kind string, data map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
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
	r.recMu.Lock()
	defer r.recMu.Unlock()
	before := r.root.RetentionStatus().EventsTotal
	label := "fixture:" + kind
	err = r.root.Context().Effect(label, func() (kernel.Disposer, error) {
		return func(context.Context) error { return nil }, nil
	})
	if err != nil {
		return fromKernel(err)
	}
	seq := before + 1
	if r.root.RetentionStatus().EventsTotal != seq {
		// Other fibers appended meanwhile: take the first marker that is not filed.
		events, err := r.root.Events(ctx)
		if events == nil {
			return fromKernel(err)
		}
		for _, e := range events {
			if _, filed := r.records[e.Sequence]; e.Sequence > before && e.Label == label && e.Fiber == 0 && e.Kind == "effect" && !filed {
				seq = e.Sequence
				break
			}
		}
	}
	r.records[seq] = immutable
	return nil
}

// observe is the launcher observer of the kernel. It serves the root context: the
// records describe launcher observers only, so an observer bound to the
// activation of a fiber, which leaves with that fiber, is not offered by the
// public API and is refused, not emulated.
func (r *kernelRuntime) observe(owner *Context, label string, fn func(context.Context, Event) error, onOverflow func(context.Context, *ObserverOverflow) error) (Disposer, error) {
	switch {
	case owner == nil || owner.host != r:
		return nil, ErrObserverBinding
	case owner != r.host.Load():
		return nil, kernelGap("ObserveEvents on the activation of a fiber: the kernel has launcher observers only (Root.Observe); an observer that leaves with its fiber needs Context.Observe, which no record accepts")
	case label == "" || fn == nil:
		return nil, eventqueue.ErrObserver
	}
	callback := func(ctx context.Context, e kernel.LedgerEvent) error {
		ev, ok := r.event(e)
		if !ok {
			return nil
		}
		return fn(ctx, ev)
	}
	var opts []kernel.ObserveOption
	if onOverflow != nil {
		opts = append(opts, kernel.OnOverflow(func(ctx context.Context, o *kernel.ObserverOverflow) error {
			return onOverflow(ctx, &ObserverOverflow{Label: o.Label, Lost: o.Lost, Through: o.Through, Limit: o.Limit})
		}))
	}
	dispose, err := r.root.Observe(label, callback, opts...)
	if err != nil {
		if errors.Is(err, kernel.ErrInactive) {
			return nil, ErrInactive
		}
		return nil, fromKernel(err)
	}
	return Disposer(dispose), nil
}

func (r *kernelRuntime) ObserveEvents(owner *Context, label string, fn func(context.Context, Event) error) (Disposer, error) {
	return r.observe(owner, label, fn, nil)
}

func (r *kernelRuntime) ObserveEventsWith(owner *Context, label string, fn func(context.Context, Event) error, onOverflow func(context.Context, *ObserverOverflow) error) (Disposer, error) {
	return r.observe(owner, label, fn, onOverflow)
}

func (r *kernelRuntime) AwaitObservers(ctx context.Context) error {
	return fromKernel(r.root.AwaitObservers(ctx))
}

// ObserverStatus is the kernel's, with the accepted and delivered counts relative
// to the hidden prefix.
func (r *kernelRuntime) ObserverStatus() ObserverStatus {
	s := eventqueue.Status(r.root.ObserverStatus())
	s.Accepted -= min(s.Accepted, r.base.events)
	s.Delivered -= min(s.Delivered, r.base.events)
	return s
}

// RetentionStatus is the kernel's, relative to the hidden prefix. The kernel has no
// recorder (ADR-034 owner decision 5), so RecordLimit and what follows are zero.
func (r *kernelRuntime) RetentionStatus() RetentionStatus {
	s := r.root.RetentionStatus()
	return RetentionStatus{ArchiveLimit: s.ArchiveLimit, Archived: s.Archived, ArchiveEvicted: s.ArchiveEvicted,
		EventWindow: r.window, Windowed: int(min(s.EventsTotal-min(s.EventsTotal, r.base.events), uint64(r.window))),
		EventsTotal:     s.EventsTotal - min(s.EventsTotal, r.base.events),
		ObserverBacklog: s.ObserverBacklog, TerminalDropped: s.TerminalDropped}
}

// keepLeave stores the activation's leave function once. It is a separate
// function so the lease path of the adapter allocates nothing after the first call.
//
//go:noinline
func keepLeave(slot *atomic.Pointer[func()], l func()) {
	slot.CompareAndSwap(nil, &l)
}
