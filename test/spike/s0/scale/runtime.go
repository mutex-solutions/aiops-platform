// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	co "github.com/mutex-solutions/aiops-platform/test/spike/s0/composition"
	cc "github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
	cm "github.com/mutex-solutions/aiops-platform/test/spike/s0/contextmodel"
)

// Backend owns a fresh real candidate root per run; it never supplies expected
// lifecycle results or source-owner counters. Hooks and native deadlines remain
// enabled. The final source/command identity is supplied by the parent driver.
type Backend struct {
	Name string
	New  func() cc.Runtime
}

func Candidates() []Backend {
	return []Backend{{Name: "reconciler", New: cc.NewReconciler}, {Name: "graph-lock", New: cc.NewGraphLock}}
}

// Resources is fixture-owned accounting, not inferred from state or queue sizes.
// Each Apply owns one Defer registration and enters/releases its actual native
// activation lease, directly or through the composition bridge.
type Resources struct {
	Active atomic.Int64
	Leases atomic.Int64
	Loads  atomic.Int64
}

type liveContext struct {
	raw      *cc.Context
	composed *co.Context
}

// RealCandidateRuntime compiles declarative data to actual native/composition
// specs. Realm/tree fixtures use contextmodel's real opaque labels and typed
// composition; graph keys use the native string boundary to avoid a 10k-key
// context registry per chain node. There is no parallel mock lifecycle model.
type RealCandidateRuntime struct {
	Runtime        cc.Runtime
	Fixture        Fixture
	Resources      Resources
	EmptyRoot      Checkpoint // After real root creation, before spec/realm compilation.
	IDs            []uint64
	BatchCalls     int // Successfully acknowledged native data-only batches.
	MountBatches   int // Real BatchMount groups, including composed starts.
	UpdateBatches  int
	DisposeBatches int

	specs     []cc.Spec
	composed  []co.Spec
	authority *co.RootAuthority
	parents   map[int]*co.Context
	entry     cm.Plugin
	keys      map[string]cm.Key[int]
	providers map[RawKey]int
	mu        sync.Mutex
	live      []liveContext
}

func NewRealCandidateRuntime(backend Backend, f Fixture) (*RealCandidateRuntime, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	if backend.New == nil || backend.Name == "" {
		return nil, errors.New("named real backend factory required")
	}
	r := &RealCandidateRuntime{Runtime: backend.New(), Fixture: f, IDs: make([]uint64, len(f.Nodes)),
		providers: make(map[RawKey]int), live: make([]liveContext, len(f.Nodes))}
	if _, ok := r.Runtime.(cc.Batcher); !ok {
		cleanup, cancel := context.WithTimeout(context.Background(), TrialGuard)
		defer cancel()
		return nil, errors.Join(errors.New("real candidate lacks native Batch"), r.Runtime.Close(cleanup))
	}
	if _, ok := r.Runtime.(cc.ObserverRuntime); !ok {
		cleanup, cancel := context.WithTimeout(context.Background(), TrialGuard)
		defer cancel()
		return nil, errors.Join(errors.New("real candidate lacks ObserverRuntime"), r.Runtime.Close(cleanup))
	}
	baseline, err := r.Checkpoint(context.Background(), true)
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), TrialGuard)
		defer cancel()
		return nil, errors.Join(err, r.Runtime.Close(cleanup))
	}
	r.EmptyRoot = baseline
	for _, node := range f.Nodes {
		for _, key := range node.Provides {
			r.providers[key] = node.Index
		}
	}
	if f.Topology == Realms || f.Topology == Trees {
		if err := r.compileRealms(); err != nil {
			// Construction has not started fixture callbacks; release the real root.
			cleanup, cancel := context.WithTimeout(context.Background(), TrialGuard)
			defer cancel()
			return nil, errors.Join(err, r.Runtime.Close(cleanup))
		}
	} else {
		r.compileNative()
	}
	return r, nil
}

func nativeKey(key RawKey) string { return fmt.Sprintf("%q/realm:%d", key.Name, key.Realm) }

func (r *RealCandidateRuntime) compileNative() {
	r.specs = make([]cc.Spec, len(r.Fixture.Nodes))
	for i, node := range r.Fixture.Nodes {
		spec := cc.Spec{ID: node.ID, Config: cc.NewConfiguration(node.Index, cc.ConfigHooks{})}
		for _, key := range node.Needs {
			spec.Needs = append(spec.Needs, nativeKey(key))
		}
		for _, key := range node.Provides {
			spec.Provides = append(spec.Provides, nativeKey(key))
		}
		spec.Apply = func(c *cc.Context) error {
			if !c.Enter() {
				return cc.ErrInactive
			}
			r.Resources.Leases.Add(1)
			defer func() { c.Leave(); r.Resources.Leases.Add(-1) }()
			if c.Config() != node.Index {
				return fmt.Errorf("%s: effective config differs", node.ID)
			}
			read := func(key RawKey) error {
				value, err := c.Read(nativeKey(key))
				if err != nil || value != r.providers[key] {
					return fmt.Errorf("%s: actual injected %v, expected provider index %d: %w", node.ID, value, r.providers[key], err)
				}
				return nil
			}
			if err := r.activate(node, read, c.Defer); err != nil {
				return err
			}
			for _, key := range node.Provides {
				if err := c.Provide(nativeKey(key), node.Index); err != nil {
					return err
				}
			}
			r.mu.Lock()
			r.live[node.Index] = liveContext{raw: c}
			r.mu.Unlock()
			return nil
		}
		r.specs[i] = spec
	}
}

func (r *RealCandidateRuntime) compileRealms() error {
	r.keys, r.parents = make(map[string]cm.Key[int]), make(map[int]*co.Context)
	for _, node := range r.Fixture.Nodes {
		for _, raw := range append(slices.Clone(node.Needs), node.Provides...) {
			r.keys[raw.Name] = cm.NewKey[int](raw.Name, 1)
		}
	}
	var keys []cm.AnyKey
	for _, name := range slices.Sorted(mapKeys(r.keys)) {
		keys = append(keys, r.keys[name].Any())
	}
	r.entry = cm.NewEntry("fixture")
	var err error
	r.authority, err = co.NewRuntime(r.Runtime, cm.RootSpec{Entries: []cm.Plugin{r.entry}, Keys: keys})
	if err != nil {
		return err
	}
	r.parents[-1] = r.authority.Context()
	for _, node := range r.Fixture.Nodes {
		if _, exists := r.parents[node.Realm]; exists {
			continue
		}
		// Metadata-only parent: NOT an uncounted runtime/container fiber.
		label := cm.NewLabel(fmt.Sprintf("group-%d", node.Realm))
		parent := r.authority.Context()
		for _, name := range slices.Sorted(mapKeys(r.keys)) {
			if name == "shared" {
				continue
			}
			parent, err = parent.Isolate(r.keys[name].Any(), label)
			if err != nil {
				return err
			}
		}
		r.parents[node.Realm] = parent
	}
	r.composed = make([]co.Spec, len(r.Fixture.Nodes))
	for i, node := range r.Fixture.Nodes {
		spec := co.Spec{Metadata: cm.Spec{ID: node.ID}, Config: cc.NewConfiguration(node.Index, cc.ConfigHooks{})}
		for _, raw := range node.Needs {
			spec.Metadata.Inject = append(spec.Metadata.Inject, cm.Need(r.keys[raw.Name]))
		}
		for _, raw := range node.Provides {
			spec.Metadata.Provides = append(spec.Metadata.Provides, r.keys[raw.Name].Any())
		}
		spec.Apply = func(c *co.Context) (cc.Disposer, error) {
			if !c.Enter() {
				return nil, cc.ErrInactive
			}
			r.Resources.Leases.Add(1)
			defer func() { c.Leave(); r.Resources.Leases.Add(-1) }()
			if c.Config() != node.Index {
				return nil, fmt.Errorf("%s: effective config differs", node.ID)
			}
			read := func(raw RawKey) error {
				value, err := co.Resolve(c, r.keys[raw.Name])
				if err != nil || value != r.providers[raw] {
					return fmt.Errorf("%s: cross-realm/missing value %d, want %d: %w", node.ID, value, r.providers[raw], err)
				}
				return nil
			}
			if err := r.activate(node, read, c.Defer); err != nil {
				return nil, err
			}
			for _, raw := range node.Provides {
				if err := co.Provide(c, r.keys[raw.Name], node.Index, nil); err != nil {
					return nil, err
				}
			}
			r.mu.Lock()
			r.live[node.Index] = liveContext{composed: c}
			r.mu.Unlock()
			return nil, nil
		}
		r.composed[i] = spec
	}
	return nil
}

// mapKeys is an iterator so the stdlib alone provides deterministic map order.
func mapKeys[V any](values map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for key := range values {
			if !yield(key) {
				return
			}
		}
	}
}

func (r *RealCandidateRuntime) activate(node Node, read func(RawKey) error, register func(string, cc.Disposer) error) error {
	for _, key := range node.Needs {
		if err := read(key); err != nil {
			return err
		}
	}
	r.Resources.Active.Add(1)
	if err := register("fixture-resource", func(context.Context) error {
		defer r.Resources.Active.Add(-1)
		// Real dependency retention is checked during consumer cleanup as well.
		for _, key := range node.Needs {
			if err := read(key); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		r.Resources.Active.Add(-1)
		return err
	}
	r.Resources.Loads.Add(1)
	return nil
}

// Mount compiles the selected real fixture declarations and admits them through
// ONE actual native Batch. Composition BatchStart preserves its own entry/realm
// compilation; no temporary backend, staging error or provisional ID is used.
func (r *RealCandidateRuntime) Mount(ctx context.Context, start, end int) error {
	if start < 0 || end < start || end > len(r.IDs) {
		return fmt.Errorf("invalid mount interval [%d,%d)", start, end)
	}
	if start == end {
		return nil
	}
	var ids []uint64
	if r.authority == nil {
		operations := make([]cc.BatchOperation, end-start)
		for i := start; i < end; i++ {
			operations[i-start] = cc.BatchOperation{Kind: cc.BatchMount, Spec: r.specs[i]}
		}
		var err error
		ids, err = r.Runtime.(cc.Batcher).Batch(ctx, operations)
		if err != nil {
			return err
		}
	} else {
		requests := make([]co.MountRequest, end-start)
		for i := start; i < end; i++ {
			requests[i-start] = co.MountRequest{Parent: r.parents[r.Fixture.Nodes[i].Realm], Entry: r.entry, Spec: r.composed[i]}
		}
		fibers, err := r.authority.BatchStart(ctx, requests)
		if err != nil {
			return err
		}
		for _, fiber := range fibers {
			ids = append(ids, fiber.ID)
		}
	}
	if len(ids) != end-start {
		return fmt.Errorf("native Batch returned %d IDs for %d mounts", len(ids), end-start)
	}
	for offset, id := range ids {
		if id == 0 {
			return fmt.Errorf("native Batch omitted real mount ID at node %d", start+offset)
		}
		r.IDs[start+offset] = id
	}
	r.BatchCalls++
	r.MountBatches++
	return nil
}

// BatchUpdate commits every selected desired revision through ONE real native
// Batch. It never substitutes an Update loop or joins lifecycle/observer work.
func (r *RealCandidateRuntime) BatchUpdate(ctx context.Context, indexes []int) error {
	operations := make([]cc.BatchOperation, 0, len(indexes))
	for _, index := range indexes {
		if index < 0 || index >= len(r.IDs) || r.IDs[index] == 0 {
			return fmt.Errorf("invalid BatchUpdate node %d", index)
		}
		operations = append(operations, cc.BatchOperation{Kind: cc.BatchUpdate, Fiber: r.IDs[index]})
	}
	if len(operations) == 0 {
		return nil
	}
	if _, err := r.Runtime.(cc.Batcher).Batch(ctx, operations); err != nil {
		return err
	}
	r.BatchCalls++
	r.UpdateBatches++
	return nil
}

// BatchDisposal commits one real native BatchDispose prefix, in the supplied
// fixture-node order. Lifecycle Settle and observer completion stay explicit.
func (r *RealCandidateRuntime) BatchDisposal(ctx context.Context, indexes []int) error {
	operations := make([]cc.BatchOperation, 0, len(indexes))
	for _, index := range indexes {
		if index < 0 || index >= len(r.IDs) || r.IDs[index] == 0 {
			return fmt.Errorf("invalid BatchDispose node %d", index)
		}
		operations = append(operations, cc.BatchOperation{Kind: cc.BatchDispose, Fiber: r.IDs[index]})
	}
	if len(operations) == 0 {
		return nil
	}
	if _, err := r.Runtime.(cc.Batcher).Batch(ctx, operations); err != nil {
		return err
	}
	r.BatchCalls++
	r.DisposeBatches++
	return nil
}

// AwaitObservers joins the actual native dispatcher watermark only. It does not
// alter Runtime.Settle or pretend that retained Events history is queue backlog.
func (r *RealCandidateRuntime) AwaitObservers(ctx context.Context) error {
	observer := r.Runtime.(cc.ObserverRuntime)
	if err := observer.AwaitObservers(ctx); err != nil {
		return err
	}
	status := observer.ObserverStatus()
	if err := validateObserverStatus(status); err != nil {
		return err
	}
	if status.Accepted != status.Delivered || status.Remaining != 0 || status.Queued != 0 || status.InFlight {
		return fmt.Errorf("observer checkpoint still pending after explicit watermark join: %+v", status)
	}
	if status.ObserverErrors != 0 || status.LoggerPanics != 0 || status.FormattingPanics != 0 {
		return fmt.Errorf("observer checkpoint contains callback/logger diagnostics: %+v", status)
	}
	return nil
}

// Checkpoint explicitly completes observers BEFORE any requested GC. Caller
// separately owns lifecycle Settle; a checkpoint never changes its semantics.
func (r *RealCandidateRuntime) Checkpoint(ctx context.Context, gc bool) (Checkpoint, error) {
	guard, cancel := context.WithTimeout(ctx, TrialGuard)
	defer cancel()
	err := r.AwaitObservers(guard)
	// On a failed barrier record the actual live counters without forcing GC or
	// presenting an observer-completed retained-heap checkpoint.
	out := checkpoint(&r.Resources, gc && err == nil)
	status := r.Runtime.(cc.ObserverRuntime).ObserverStatus()
	out.Observers = &status
	return out, errors.Join(err, validateObserverStatus(status))
}

// RequestDisposal is failure-path cleanup: submit ALL individual idempotent
// acknowledgements before joining. A partially mounted/already archived graph
// cannot be passed to native Batch's live-target preflight. Normal teardown uses
// BatchDisposal; this fallback is never counted as a measured successful batch.
func (r *RealCandidateRuntime) RequestDisposal(ctx context.Context, start, end int) error {
	var errs []error
	for i := start; i < end; i++ {
		if r.IDs[i] != 0 {
			errs = append(errs, r.Runtime.Dispose(ctx, r.IDs[i]))
		}
	}
	return errors.Join(errs...)
}

// HotLookup traverses actual bindings. Composed tree values differ per realm;
// successful equality is therefore a real isolation assertion, not a name test.
func (r *RealCandidateRuntime) HotLookup() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, node := range r.Fixture.Nodes {
		if node.Tree < 0 {
			continue
		}
		for _, key := range node.Needs {
			value, err := co.Resolve(r.live[node.Index].composed, r.keys[key.Name])
			if err != nil || value != r.providers[key] {
				return fmt.Errorf("tree %d lookup %q = %d, want %d: %w", node.Tree, key.Name, value, r.providers[key], err)
			}
		}
	}
	return nil
}

// Clear drops frontend-held activation references only AFTER teardown/Close.
// Native archives/event history are never truncated to make heap numbers cheap.
func (r *RealCandidateRuntime) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.live = nil
	r.Runtime, r.authority = nil, nil
	r.specs, r.composed, r.parents = nil, nil, nil
}
