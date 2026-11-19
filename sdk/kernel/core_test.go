// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// The tests drive the core through a thin adapter that gives it the vocabulary of
// the differential corpus (Spec, Context, Runtime). Every method delegates to the
// real core or handle; the adapter adds no behaviour except turning the string
// keys of the corpus into service keys and a refused registration back into an
// error value.

// The deadlines of the test configuration, in the units the corpus uses.
const (
	testLoad    = 2 * time.Second
	testDrain   = 5 * time.Second
	testDispose = 2 * time.Second
)

// testConfig is the configuration of the tests: the core holds no defaults, so the
// tests define theirs. Capacities are large enough that no corpus case reaches
// them unless it sets them itself.
func testConfig() coreConfig {
	return coreConfig{
		loadDeadline: testLoad, drainDeadline: testDrain, disposeDeadline: testDispose,
		archiveLimit: 1024, eventWindow: 1 << 20, observerQueue: 1 << 16, ownerInbox: 256, terminalLimit: 64,
	}
}

type (
	// Handle is the per-activation handle.
	Handle = handle
	// Snapshot is the core's fiber snapshot.
	Snapshot = fiberSnap
	// EpochSnapshot is the desired epoch of a snapshot.
	EpochSnapshot = epochSnap
	// ProvisionSnapshot is one provision of an inspection.
	ProvisionSnapshot = provisionSnap
	// BatchKind is the kind of a batch operation.
	BatchKind = batchKind
)

const (
	BatchMount   = batchMount
	BatchUpdate  = batchUpdate
	BatchDispose = batchDispose
)

var (
	ErrObserverBinding    = errObserverBinding
	ErrScopeUnavailable   = errScopeUnavailable
	ErrEffectUnavailable  = errEffectUnavailable
	ErrTrackedUnavailable = errTrackedUnavailable
	ErrReentrant          = errReentrant
)

// Spec is the corpus form of a pluginSpec: services are named by string.
type Spec struct {
	ID          string
	Needs       []string
	Provides    []string
	Apply       func(*Handle) error
	ApplyEffect func(*Handle) (Disposer, error)
	Check       func(*Handle) bool
}

func keysOf(names []string) []svcKey {
	if names == nil {
		return nil
	}
	out := make([]svcKey, len(names))
	for i, n := range names {
		if n != "" { // The corpus's empty name is the core's nil key.
			out[i] = n
		}
	}
	return out
}

func (s Spec) spec() pluginSpec {
	return pluginSpec{ID: s.ID, Needs: keysOf(s.Needs), Provides: keysOf(s.Provides), Apply: s.Apply, ApplyEffect: s.ApplyEffect, Check: s.Check}
}

// BatchOperation is the corpus form of a batchOp.
type BatchOperation struct {
	Kind  BatchKind
	Fiber uint64
	Spec  Spec
}

// Runtime is the corpus view of a core. The other interfaces are the optional
// capabilities the corpus probes for with a type assertion; *rt has them all.
type Runtime interface {
	Mount(context.Context, Spec) (uint64, error)
	Update(context.Context, uint64) error
	Dispose(context.Context, uint64) error
	Settle(context.Context) error
	Snapshots(context.Context) ([]Snapshot, error)
	Events(context.Context) ([]ledgerEvent, error)
	Close(context.Context) error
}

type (
	Notifier interface {
		Notify(context.Context, ...string) error
	}
	RootProvider interface {
		RootContext(context.Context) (*Handle, error)
	}
	Waiter interface {
		AwaitFiber(context.Context, uint64) error
	}
	Inspector interface {
		Inspect(context.Context) (graphView, error)
	}
	Batcher interface {
		Batch(context.Context, []BatchOperation) ([]uint64, error)
	}
	Recorder interface {
		Record(context.Context, string, map[string]any) error
	}
	ObserverRuntime interface {
		ObserveEvents(*Handle, string, func(context.Context, ledgerEvent) error) (Disposer, error)
		AwaitObservers(context.Context) error
		ObserverStatus() ObserverStatus
	}
	OverflowObserverRuntime interface {
		ObserverRuntime
		ObserveEventsWith(*Handle, string, func(context.Context, ledgerEvent) error, func(context.Context, *ObserverOverflow) error) (Disposer, error)
	}
	RetentionReporter interface{ RetentionStatus() RetentionStatus }
)

type rt struct{ *core }

// NewReconciler starts a core with testConfig. The event window of the test
// configuration is far larger than any case, so events returns complete history.
func NewReconciler() Runtime { return newRuntime(testConfig()) }

// NewRecordingReconciler is NewReconciler under the name of the corpus.
func NewRecordingReconciler() Runtime { return NewReconciler() }

// newRuntime starts a core for cfg.
func newRuntime(cfg coreConfig) *rt {
	c, err := newCore(cfg)
	if err != nil {
		panic(err)
	}
	return &rt{core: c}
}

// newReconcilerWith starts a core for testConfig changed by mod.
func newReconcilerWith(mod func(*coreConfig)) *rt {
	cfg := testConfig()
	mod(&cfg)
	return newRuntime(cfg)
}

// Record puts a named marker in the ledger: an effect registered on the root
// handle, so observers see it, in order, as an "effect" event labelled
// "fixture:"+kind (see markerOf).
func (r *rt) Record(ctx context.Context, kind string, _ map[string]any) error {
	root, err := r.rootHandle(ctx)
	if err != nil {
		return err
	}
	return root.tryDefer("fixture:"+kind, func(context.Context) error { return nil })
}

func (r *rt) Mount(ctx context.Context, s Spec) (uint64, error) { return r.mount(ctx, s.spec()) }
func (r *rt) Update(ctx context.Context, id uint64) error       { return r.fiber(id).update(ctx) }
func (r *rt) Dispose(ctx context.Context, id uint64) error      { return r.fiber(id).dispose(ctx) }
func (r *rt) AwaitFiber(ctx context.Context, id uint64) error   { return r.fiber(id).await(ctx) }
func (r *rt) Settle(ctx context.Context) error                  { return r.settle(ctx) }
func (r *rt) Close(ctx context.Context) error                   { return r.close(ctx) }
func (r *rt) Snapshots(ctx context.Context) ([]Snapshot, error) { return r.snapshots(ctx) }
func (r *rt) Inspect(ctx context.Context) (graphView, error)    { return r.inspect(ctx) }
func (r *rt) Events(ctx context.Context) ([]ledgerEvent, error) { return r.events(ctx) }
func (r *rt) RootContext(ctx context.Context) (*Handle, error)  { return r.rootHandle(ctx) }
func (r *rt) AwaitObservers(ctx context.Context) error          { return r.awaitObservers(ctx) }
func (r *rt) ObserverStatus() ObserverStatus                    { return r.observerStatus() }
func (r *rt) RetentionStatus() retentionStatus                  { return r.retentionStatus() }
func (r *rt) Notify(ctx context.Context, keys ...string) error {
	return r.notifyKeys(ctx, keysOf(keys)...)
}
func (r *rt) ObserveEvents(owner *Handle, label string, fn func(context.Context, ledgerEvent) error) (Disposer, error) {
	return r.observe(owner, label, fn, nil)
}

func (r *rt) ObserveEventsWith(owner *Handle, label string, fn func(context.Context, ledgerEvent) error, onOverflow func(context.Context, *ObserverOverflow) error) (Disposer, error) {
	return r.observe(owner, label, fn, onOverflow)
}

func (r *rt) Batch(ctx context.Context, ops []BatchOperation) ([]uint64, error) {
	converted := make([]batchOp, len(ops))
	for i, op := range ops {
		converted[i] = batchOp{Kind: op.Kind, Fiber: op.Fiber, Spec: op.Spec.spec()}
	}
	return r.batch(ctx, converted)
}

// joinOwner waits until the owner goroutine of r has finished. Close can return
// before that: it returns a lifecycle diagnostic or a bounded wait's error while
// the owner is still alive. A test must not return while its owner runs, or the
// owner leaks into the next test.
func joinOwner(ctx context.Context, runtime Runtime) error {
	r := runtime.(*rt)
	select {
	case <-r.done:
		return nil
	default:
	}
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return errors.New("owner still running after Close: " + ctx.Err().Error())
	}
}

// closeRuntime is Close followed by joinOwner; it returns Close's error first.
func closeRuntime(ctx context.Context, r Runtime) error {
	return errors.Join(r.Close(ctx), joinOwner(ctx, r))
}

// The methods below give the corpus the Context vocabulary on top of the handle's
// unexported operations. They live in a test file on purpose: the exported names
// belong to the front, which is written later.

func (c *handle) Std() context.Context { return c.std }

func (c *handle) Read(key string) (any, error) { return c.read(key) }

func (c *handle) Owns(key string) bool { return c.owns(key) }

func (c *handle) Provide(key string, value any) error {
	return c.provide(keysOf([]string{key})[0], key, value)
}

func (c *handle) Dispose() error { return c.requestDispose() }

func (c *handle) MountChild(s Spec) (uint64, error) { return c.mountChild(s.spec()) }

func (c *handle) Effect(label string, setup func() (Disposer, error)) error {
	return c.effect(label, setup)
}

func (c *handle) RequestEffectScope(label string, setup func(*EffectScope) error) (*EffectHandle, error) {
	return c.requestScope(label, setup)
}

// Enter and Leave pair the shape of the corpus with enter's leave function.
func (c *handle) Enter() bool {
	_, ok := c.enter()
	return ok
}

func (c *handle) Leave() { c.leave() }

// Defer returns the refusal that deferEffect raises, so a corpus case can assert
// it as a value.
func (c *handle) Defer(label string, d Disposer) (err error) {
	defer recoverRefusal(&err)
	c.deferEffect(label, d)
	return nil
}

// Go is the same for goTracked.
func (c *handle) Go(label string, run func(context.Context) error) (err error) {
	defer recoverRefusal(&err)
	c.goTracked(label, run)
	return nil
}

func recoverRefusal(err *error) {
	if p := recover(); p != nil {
		refused, ok := p.(*refusedCall)
		if !ok {
			panic(p)
		}
		*err = refused.err
	}
}

// find returns the snapshot of id.
func find(list []Snapshot, id uint64) (Snapshot, bool) {
	i := slices.IndexFunc(list, func(s Snapshot) bool { return s.ID == id })
	if i < 0 {
		return Snapshot{}, false
	}
	return list[i], true
}

// keyText renders the key or label of an event, "" for none.
func keyText(k svcKey) string {
	if k == nil {
		return ""
	}
	return fmt.Sprint(k)
}

// markerOf is the name given to Record, or "" when e is not a Record marker.
func markerOf(e ledgerEvent) string {
	if e.Kind != "effect" {
		return ""
	}
	name, _ := strings.CutPrefix(keyText(e.Key), "fixture:")
	if name == keyText(e.Key) {
		return ""
	}
	return name
}
