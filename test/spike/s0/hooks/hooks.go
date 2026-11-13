// SPDX-License-Identifier: Apache-2.0

// Package hooks is an independent, test-only typed hook prototype. It adapts the
// pinned Cordis events.ts dispatch algorithms; see NOTICE. It is not sdk/kernel,
// a scheduler selection, or a product gate. See README.md for deliberate limits.
package hooks

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/mutex-solutions/aiops-platform/test/spike/s0/contextmodel"
)

var (
	ErrDefinition  = errors.New("invalid hook definition")
	ErrBinding     = errors.New("hook binding requires metadata, lifecycle ownership and a lease")
	ErrOwner       = errors.New("dispatcher does not provide the hook owning key")
	ErrUndeclared  = errors.New("listener hook or stage is undeclared")
	ErrStage       = errors.New("unknown hook stage")
	ErrInactive    = errors.New("inactive hook owner")
	ErrNext        = errors.New("next() called multiple times")
	ErrNextExpired = errors.New("next called after its listener returned")
)

// Lease is intentionally the existing candidate contract, not a fake Context.
// Enter must be lock-free and refuse an unloading activation. A successful Enter
// is paired with Leave, including error and panic paths. The runtime must drain
// these leases before disposing effects. This package never owns that runtime.
type Lease interface {
	Enter() bool
	Leave()
}

// Binding associates immutable context metadata with one real activation.
// A caller-runtime/front-end adapter must supply the true association and Uses
// from its plugin declaration, not from a listener's unchecked claim. Metadata
// alone is NOT an effect owner. Defer must be concurrency-safe, register cleanup
// with that activation, and reject new effects after quiescing. It must not keep
// cleanup on error. The function shape avoids a dependency on either candidate's
// named disposer type. This is bridge data, not the final Context API.
type Binding struct {
	Metadata *contextmodel.Context
	Runtime  Lease
	Defer    func(label string, dispose func(context.Context) error) error
	Uses     []Use
	// Owns must observe the same activation's native successful reservation.
	// A declaration alone cannot grant hook dispatch ownership.
	Owns func(contextmodel.AnyKey) bool
}

// Definition is definition-owned data. Stages are ordered by this list, not by
// lexical order or activation/registration order. Access classes and tenant-data
// lint are not implemented here and must not be inferred from this constructor.
type Definition struct {
	Name   string
	Owner  contextmodel.AnyKey
	Stages []string
}

type hookToken struct{ marker byte }

// Use is opaque hook identity plus a declared stage. A future frontend must put
// these in its Spec and authorise classes before constructing a Binding.
type Use struct {
	hook  *hookToken
	stage string
}

type definition struct {
	Definition
	token *hookToken
}

func newDefinition(spec Definition) (definition, error) {
	if spec.Name == "" || spec.Owner.Name() == "" || len(spec.Stages) == 0 {
		return definition{}, ErrDefinition
	}
	seen := make(map[string]bool, len(spec.Stages))
	for _, stage := range spec.Stages {
		if stage == "" || seen[stage] {
			return definition{}, ErrDefinition
		}
		seen[stage] = true
	}
	spec.Stages = slices.Clone(spec.Stages)
	return definition{Definition: spec, token: &hookToken{}}, nil
}

func (d *definition) use(stage string) (Use, error) {
	if !slices.Contains(d.Stages, stage) {
		return Use{}, ErrStage
	}
	return Use{hook: d.token, stage: stage}, nil
}

func (b Binding) valid() bool {
	return b.Metadata != nil && b.Metadata.Entry() != (contextmodel.Plugin{}) &&
		b.Metadata.Spec().ID != "" && b.Runtime != nil && b.Defer != nil
}

func (d *definition) checkOwner(b Binding) error {
	if !b.valid() {
		return ErrBinding
	}
	if !slices.Contains(b.Metadata.Spec().Provides, d.Owner) || b.Owns == nil || !b.Owns(d.Owner) {
		return ErrOwner
	}
	return nil
}

func (d *definition) checkListener(b Binding, stage string) (int, error) {
	if !b.valid() {
		return 0, ErrBinding
	}
	index := slices.Index(d.Stages, stage)
	if index < 0 {
		return 0, ErrStage
	}
	if !slices.Contains(b.Uses, Use{hook: d.token, stage: stage}) {
		return 0, ErrUndeclared
	}
	return index, nil
}

// Disposer removes an effect idempotently. It does not wait for a callback that
// already acquired its activation lease; the owning runtime performs that drain.
type Disposer func(context.Context) error

type registration[F any] struct {
	pluginID string
	entry    contextmodel.Plugin
	stage    int
	metadata *contextmodel.Context
	runtime  Lease
	callback F
	removed  atomic.Bool
}

type snapshot[F any] struct{ items []*registration[F] }

type table[F any] struct {
	// Only registration and disposal acquire this mutex. No lifecycle, scope or
	// listener callback executes while it is held. Published slices never mutate.
	mutation sync.Mutex
	current  atomic.Pointer[snapshot[F]]
}

func (t *table[F]) load() []*registration[F] {
	if current := t.current.Load(); current != nil {
		return current.items
	}
	return nil
}

func (t *table[F]) add(d *definition, b Binding, stage string, callback F) (Disposer, error) {
	index, err := d.checkListener(b, stage)
	if err != nil {
		return nil, err
	}
	// Registering the disposer and publishing the listener form one leased setup.
	// A runtime may quiesce during it, but may not finish cleanup before Leave.
	if !b.Runtime.Enter() {
		return nil, ErrInactive
	}
	defer b.Runtime.Leave()
	r := &registration[F]{pluginID: b.Metadata.Spec().ID, entry: b.Metadata.Entry(),
		stage: index, metadata: b.Metadata, runtime: b.Runtime, callback: callback}
	dispose := Disposer(func(context.Context) error {
		r.removed.Store(true)
		t.mutation.Lock()
		defer t.mutation.Unlock()
		old := t.load()
		index := slices.Index(old, r)
		if index < 0 {
			return nil
		}
		items := make([]*registration[F], 0, len(old)-1)
		items = append(items, old[:index]...)
		items = append(items, old[index+1:]...)
		t.current.Store(&snapshot[F]{items: items})
		return nil
	})
	// Lifecycle registration occurs outside the mutation mutex. Even a runtime
	// that invokes cleanup immediately cannot publish a dead listener afterward.
	if err := b.Defer("hook "+d.Name+"/"+stage, dispose); err != nil {
		return nil, err
	}
	t.mutation.Lock()
	if r.removed.Load() {
		t.mutation.Unlock()
		return nil, ErrInactive
	}
	old := t.load()
	items := slices.Clone(old)
	items = append(items, r)
	// The accepted order says nothing about equal stage+plugin IDs. Preserve
	// existing registration order within that tie; do not invent a quota or a
	// third ordering key. Cross-plugin order is independent of registration.
	slices.SortStableFunc(items, func(a, b *registration[F]) int {
		if a.stage < b.stage {
			return -1
		}
		if a.stage > b.stage {
			return 1
		}
		if a.pluginID < b.pluginID {
			return -1
		}
		if a.pluginID > b.pluginID {
			return 1
		}
		return 0
	})
	t.current.Store(&snapshot[F]{items: items})
	t.mutation.Unlock()
	return dispose, nil
}

// selectAudience freezes audience membership for the whole dispatch, before any
// listener runs, matching the source's _resolve step. A predicate may inspect
// mutable external state, so evaluating it later per listener would change the
// meaning of a captured dispatch. The common all-admitted path reuses the COW
// snapshot; excluded listeners require only a dispatch-local slice, never a lock.
func (t *table[F]) selectAudience(audience contextmodel.Audience) ([]*registration[F], error) {
	items := t.load()
	selected := items
	filtered := false
	for index, r := range items {
		match, err := invoke(func() (bool, error) { return audience.Matches(r.metadata), nil })
		if err != nil {
			return nil, fmt.Errorf("listener %s audience: %w", r.pluginID, err)
		}
		if !match && !filtered {
			selected = slices.Clone(items[:index])
			filtered = true
		} else if match && filtered {
			selected = append(selected, r)
		}
	}
	return selected, nil
}

func (r *registration[F]) enter(ctx context.Context) bool {
	// Manual disposal changes future snapshots, not one already captured by a
	// dispatch. Lifecycle quiescing is different: Enter refuses an inactive owner
	// even when this registration remains in an older immutable snapshot.
	if ctx.Err() != nil || !r.runtime.Enter() {
		return false
	}
	if ctx.Err() != nil {
		r.runtime.Leave()
		return false
	}
	return true
}

// PanicError preserves a listener panic as an error at this explicit invocation
// boundary, including errors.Is when the panic payload is an error. It is not a
// global panic handler and never substitutes successful data for a failed call.
type PanicError struct{ Value any }

func (e *PanicError) Error() string { return fmt.Sprintf("hook listener panicked: %v", e.Value) }
func (e *PanicError) Unwrap() error { err, _ := e.Value.(error); return err }

func invoke[T any](fn func() (T, error)) (value T, err error) {
	defer func() {
		if cause := recover(); cause != nil {
			err = &PanicError{Value: cause}
		}
	}()
	return fn()
}
