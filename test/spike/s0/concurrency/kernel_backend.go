// SPDX-License-Identifier: Apache-2.0

package concurrency

import (
	"context"
	"errors"
	"fmt"
	"os"
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
// is not emulated. It returns ErrKernelUnsupported naming the missing export, so
// a harness that needs it fails loudly instead of passing on an approximation.
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
//	Context.Read        -> Key[any].Of       Context.Provide -> Key[any].Provide
//	Context.Defer       -> Context.Defer     Context.Go      -> Context.Go
//	Context.Enter/Leave -> Context.Enter (the leave func of one activation is shared)
//
// Fiber IDs are the adapter's own counter: Fiber has no exported ID.

// ErrKernelUnsupported is matched (errors.Is) by every refusal of a capability
// that the public sdk/kernel API does not export.
var ErrKernelUnsupported = errors.New("capability not exported by the public sdk/kernel API")

func kernelGap(what string) error { return fmt.Errorf("%w: %s", ErrKernelUnsupported, what) }

// kernelError keeps the kernel's text and identity and also matches the spike
// sentinels that the harnesses classify by.
type kernelError struct{ err error }

func (e *kernelError) Error() string { return e.err.Error() }
func (e *kernelError) Unwrap() error { return e.err }
func (e *kernelError) Is(target error) bool {
	switch target {
	case ErrInactive:
		return errors.Is(e.err, kernel.ErrInactive)
	case ErrInactiveEffect:
		return errors.Is(e.err, kernel.ErrInactiveEffect)
	}
	return false
}

func fromKernel(err error) error {
	if err == nil {
		return nil
	}
	return &kernelError{err}
}

// fromPanic turns a refused registration (the kernel panics with an error value
// that fails the fiber) into the error the spike Context returns.
func fromPanic(p any) error {
	if err, ok := p.(error); ok {
		return fromKernel(err)
	}
	return fmt.Errorf("kernel refused the call: %v", p)
}

// UnsupportedGap probes r with the read-back capabilities every harness needs
// and reports the ones its adapter refuses with ErrKernelUnsupported. Empty means
// none was refused (the two prototype cores never are). A harness uses it to
// register a backend and skip it with the gap printed, never to pass it.
// S0_RUN_UNSUPPORTED=1 turns the probe off, so the harness runs the backend and
// fails at its first refused capability (the loud form, for diagnosis).
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

type kernelFiber struct {
	f         *kernel.Fiber
	hasConfig bool
}

type kernelRuntime struct {
	root   *kernel.Root
	cfg    kernel.Config
	host   atomic.Pointer[Context]
	hostMu sync.Mutex
	closed atomic.Bool
	next   atomic.Uint64
	keys   sync.Map // string -> kernel.Key[any], one per name for this instance.
	mu     sync.RWMutex
	fibers map[uint64]kernelFiber
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
)

// NewKernel is the final kernel with the prototype reconciler's default bounds.
func NewKernel() Runtime { return NewKernelWith(Retention{}) }

// NewRecordingKernel asks for complete history. The kernel has no recorder
// (ADR-034 owner decision 5), so this only widens the event window to the cap.
func NewRecordingKernel() Runtime { return NewKernelWith(Recording()) }

// NewKernelWith maps the spike's Retention onto kernel.Config. The deadlines, the
// owner inbox and the terminal limit are the reconciler's constants.
func NewKernelWith(retention Retention) Runtime {
	retention = retention.normalized()
	cfg := kernel.Config{
		LoadDeadline: rcLoadBudget, DrainDeadline: rcDrainBudget, DisposeDeadline: rcDisposeBudget,
		ArchiveLimit: retention.ArchiveLimit, EventWindow: max(retention.EventWindow, retention.RecordLimit),
		ObserverQueue: retention.ObserverBacklog, OwnerInbox: 256, TerminalLimit: rcTerminalLimit,
	}
	// Both binding maps stay empty: every key of this adapter is open.
	root, err := kernel.New(cfg)
	if err != nil {
		panic(err)
	}
	return &kernelRuntime{root: root, cfg: cfg, fibers: make(map[uint64]kernelFiber)}
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

// plan turns one Spec into what Root.Plugin takes: the defined plugin, its raw
// config and the ID the adapter issued for it.
func (r *kernelRuntime) plan(spec Spec) (kernel.Plugin, any, uint64, error) {
	switch {
	case spec.Apply != nil && spec.ApplyEffect != nil:
		return kernel.Plugin{}, nil, 0, errors.New("Apply and ApplyEffect are mutually exclusive")
	case kernelDuplicates(spec.Needs) || kernelDuplicates(spec.Provides):
		return kernel.Plugin{}, nil, 0, errors.New("empty or duplicate declaration")
	case spec.Check != nil:
		return kernel.Plugin{}, nil, 0, kernelGap("Spec.Check: a consumer-side availability predicate; only the provider-side kernel.Available exists (needs Plugin.Check)")
	}
	var raw any
	if cell := spec.Config; cell != nil {
		h := cell.hooks
		if h.Resolve != nil || h.Commit != nil || h.Update != nil || h.UpdateAsync != nil {
			return kernel.Plugin{}, nil, 0, kernelGap("ConfigHooks: the kernel's internal/config, internal/commit and internal/update hooks are not exported to a launcher")
		}
		raw, _, _ = cell.RawSnapshot()
	}
	id := r.next.Add(1)
	p := kernel.Plugin{ID: spec.ID}
	for _, n := range spec.Needs {
		p.Inject = append(p.Inject, kernel.Need(r.key(n)))
	}
	for _, n := range spec.Provides {
		p.Provides = append(p.Provides, r.key(n))
	}
	switch {
	case spec.Apply != nil:
		p.Apply = func(kc *kernel.Context, cfg any) error { return spec.Apply(r.context(id, kc, cfg)) }
	case spec.ApplyEffect != nil:
		// APPROXIMATION. The core collects a returned disposer after the load
		// completes, even for a superseded or self-disposing load. The public API
		// has no such return, so the disposer is registered as the last effect of
		// Apply: the same ledger slot, but registered inside Apply, so a load that
		// was invalidated meanwhile runs it at once instead of in the teardown
		// walk (needs Plugin.ApplyEffect for the exact behaviour).
		p.Apply = func(kc *kernel.Context, cfg any) error {
			d, err := spec.ApplyEffect(r.context(id, kc, cfg))
			if err != nil || d == nil {
				return err
			}
			return kc.Effect("apply", func() (kernel.Disposer, error) { return kernel.Disposer(d), nil })
		}
	}
	return kernel.Define(p), raw, id, nil
}

func (r *kernelRuntime) register(id uint64, f *kernel.Fiber, spec Spec) {
	r.mu.Lock()
	r.fibers[id] = kernelFiber{f: f, hasConfig: spec.Config != nil}
	r.mu.Unlock()
}

// mount mounts spec under parent (the root context for Mount).
func (r *kernelRuntime) mount(parent *kernel.Context, spec Spec) (id uint64, err error) {
	if r.closed.Load() {
		return 0, ErrClosed
	}
	plugin, raw, id, err := r.plan(spec)
	if err != nil {
		return 0, err
	}
	defer func() {
		if p := recover(); p != nil {
			id, err = 0, fromPanic(p)
		}
	}()
	r.register(id, parent.Plugin(plugin, raw), spec)
	return id, nil
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
		return ErrUnknown
	}
	if r.closed.Load() {
		return ErrClosed
	}
	_, err := r.root.Batch(ctx, kernel.UpdateOp(f.f))
	return fromKernel(err)
}

func (r *kernelRuntime) Dispose(ctx context.Context, id uint64) error {
	if id == 0 {
		return ErrUnknown
	}
	f, ok := r.lookup(id)
	if !ok {
		return nil // The reconciler's rule: an unknown or evicted ID is an idempotent no-op.
	}
	return fromKernel(f.f.Dispose(ctx))
}

func (r *kernelRuntime) AwaitFiber(ctx context.Context, id uint64) error {
	f, ok := r.lookup(id)
	if !ok {
		return ErrUnknown
	}
	return fromKernel(f.f.Await(ctx))
}

func (r *kernelRuntime) Settle(ctx context.Context) error { return fromKernel(r.root.Settle(ctx)) }

func (r *kernelRuntime) Close(ctx context.Context) error {
	r.closed.Store(true)
	return fromKernel(r.root.Close(ctx))
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
	c := r.context(0, r.root.Context(), nil)
	c.dispose = func() error { return ErrRootDispose }
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

// RequestUpdateConfig has no commit hook to suppress, so noSave changes nothing.
func (r *kernelRuntime) RequestUpdateConfig(ctx context.Context, id uint64, raw any, _ bool) (ConfigRequest, error) {
	none := ConfigRequest{Await: func(context.Context) error { return nil }}
	f, ok := r.lookup(id)
	switch {
	case !ok:
		return none, ErrUnknown
	case !f.hasConfig:
		return none, errors.New("fiber has no configuration")
	case r.closed.Load():
		return none, ErrClosed
	}
	if err := f.f.Update(ctx, raw); err != nil {
		return none, fromKernel(err)
	}
	return ConfigRequest{Await: func(w context.Context) error { return fromKernel(f.f.Await(w)) }}, nil
}

func (r *kernelRuntime) Batch(ctx context.Context, operations []BatchOperation) ([]uint64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.closed.Load() {
		return nil, ErrClosed
	}
	ops := make([]kernel.BatchOp, len(operations))
	ids := make([]uint64, len(operations))
	specs := make([]Spec, len(operations))
	for i, op := range operations {
		switch op.Kind {
		case BatchMount:
			if op.Fiber != 0 || op.Spec.ID == "" {
				return nil, fmt.Errorf("invalid batch Mount at operation %d", i)
			}
			p, raw, id, err := r.plan(op.Spec)
			if err != nil {
				return nil, fmt.Errorf("invalid batch Mount at operation %d: %w", i, err)
			}
			ops[i], ids[i], specs[i] = kernel.MountOp(p, raw), id, op.Spec
		case BatchUpdate, BatchDispose:
			f, ok := r.lookup(op.Fiber)
			if op.Fiber == 0 || !ok {
				return nil, fmt.Errorf("batch operation %d: %w", i, ErrUnknown)
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
	for i, op := range operations {
		if op.Kind == BatchMount {
			r.register(ids[i], fibers[i], specs[i])
		}
	}
	return ids, nil // Only Mount positions carry an ID.
}

// context builds the spike Context of one activation over its kernel.Context.
func (r *kernelRuntime) context(id uint64, kc *kernel.Context, cfg any) *Context {
	c := &Context{host: r, std: kc.Std(), fiber: id, config: cfg}
	c.live.Store(true) // The kernel refuses what an inactive activation may not do.
	var leave atomic.Pointer[func()]
	c.fwd = &forward{
		// The kernel returns one leave function per activation (see handle.enter),
		// so the first one seen serves every Leave.
		enter: func() bool {
			l, ok := kc.Enter()
			if ok && leave.Load() == nil {
				leave.CompareAndSwap(nil, &l)
			}
			return ok
		},
		leave: func() { (*leave.Load())() },
		read:  func(key string) (any, error) { return r.read(kc, key) },
		// A provision stops being the activation's own when its unload starts.
		owns: func(key string) bool {
			if kc.Std().Err() != nil {
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
		if err := r.key(key).Provide(kc, value); err != nil {
			return fromKernel(err)
		}
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
	c.dispose = func() error {
		return kernelGap("Context.Dispose: self-disposal of an activation (needs an exported activation-bound Context.Dispose)")
	}
	c.child = func(spec Spec) (uint64, error) { return r.mount(kc, spec) }
	c.trackedStart = func(label string) (func(error), error) {
		done := make(chan error, 1)
		var err error
		func() {
			defer func() {
				if p := recover(); p != nil {
					err = fromPanic(p)
				}
			}()
			// The worker the spike Context starts reports through done; the kernel's
			// tracked slot lasts until it does, so a drain waits for the real work.
			kc.Go(label, func(context.Context) error { return <-done })
		}()
		if err != nil {
			return nil, err
		}
		return func(e error) { done <- e }, nil
	}
	return c
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

// The capabilities below are absent from the public API. Each names the export
// that would close it.

func (r *kernelRuntime) Snapshots(context.Context) ([]Snapshot, error) {
	return nil, kernelGap("Snapshots: no exported fiber snapshot (needs Root.Snapshots and Fiber.ID)")
}

func (r *kernelRuntime) Inspect(context.Context) (Inspection, error) {
	return Inspection{}, kernelGap("Inspect: no exported graph view with provisions and a ledger watermark (needs Root.Inspect)")
}

func (r *kernelRuntime) Events(context.Context) ([]Event, error) {
	return nil, kernelGap("Events: no exported event ledger (needs Root.Events or an event observer)")
}

func (r *kernelRuntime) Notify(context.Context, ...string) error {
	return kernelGap("Notify: no exported availability recheck (needs Root.Notify with Plugin.Check)")
}

// Record would be a root effect labelled fixture:<kind>, which orders itself in the
// kernel's ledger, but nothing exported reads that ledger back, so a marker could
// not be ordered against events by anyone. Refused until the stream is exported.
func (r *kernelRuntime) Record(context.Context, string, map[string]any) error {
	return kernelGap("Record: the fixture marker cannot be read back in event order (needs the exported event stream)")
}

func (r *kernelRuntime) ObserveEvents(*Context, string, func(context.Context, Event) error) (Disposer, error) {
	return nil, kernelGap("ObserveEvents: no exported event observer (needs Context.Observe)")
}

func (r *kernelRuntime) ObserveEventsWith(*Context, string, func(context.Context, Event) error, func(context.Context, *ObserverOverflow) error) (Disposer, error) {
	return nil, kernelGap("ObserveEventsWith: no exported event observer with overflow signal (needs Context.Observe)")
}

func (r *kernelRuntime) AwaitObservers(context.Context) error {
	return kernelGap("AwaitObservers: no exported observer barrier (needs Root.AwaitObservers)")
}

// ObserverStatus cannot return an error, so it reports the gap as an observer
// error that every status check rejects.
func (r *kernelRuntime) ObserverStatus() ObserverStatus {
	return eventqueue.Status{ObserverErrors: 1, LastObserverError: kernelGap("ObserverStatus: no exported observer status").Error()}
}

// RetentionReporter is deliberately not implemented: RetentionStatus cannot return
// an error, and a zero report would read as real counters. A harness that probes
// for it finds it absent (needs Root.RetentionStatus).
