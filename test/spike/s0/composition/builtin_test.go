// SPDX-License-Identifier: Apache-2.0

package composition_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	co "github.com/mutex-solutions/aiops-platform/test/spike/s0/composition"
	cc "github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
	cm "github.com/mutex-solutions/aiops-platform/test/spike/s0/contextmodel"
)

type builtinTestValue struct{ name string }

// This adapter gates a real native inspection; it neither fabricates graph data
// nor replaces the native root context, reservation, ownership, or effect ledger.
type builtinGatedInspector struct {
	cc.Runtime
	cc.RootProvider
	inspector cc.Inspector
	started   chan struct{}
	release   <-chan struct{}
	once      sync.Once
}

func (b *builtinGatedInspector) Inspect(ctx context.Context) (cc.Inspection, error) {
	var gated bool
	b.once.Do(func() {
		gated = true
		close(b.started)
	})
	if gated {
		select {
		case <-b.release:
		case <-ctx.Done():
			return cc.Inspection{}, ctx.Err()
		}
	}
	return b.inspector.Inspect(ctx)
}

type builtinInstallResult struct {
	owner *co.Context
	err   error
}

func TestInstallBuiltinUsesActualRootWithoutUserFibersOrProvisions(t *testing.T) {
	both(t, func(t *testing.T, ctx context.Context, backend cc.Runtime) {
		keys := make([]cm.Key[*builtinTestValue], 4)
		definitions := make([]cm.AnyKey, len(keys))
		for i, name := range []string{"reflect", "registry", "hooks", "logger"} {
			keys[i] = cm.NewKey[*builtinTestValue](name, 0)
			definitions[i] = keys[i].Any()
		}
		r := root(t, backend, cm.RootSpec{Keys: definitions})
		owners := make([]*co.Context, len(keys))
		for i, key := range keys {
			value := &builtinTestValue{name: key.Any().Name()}
			owner, err := co.InstallBuiltin(ctx, r, key, value, nil)
			if err != nil {
				t.Fatal(err)
			}
			owners[i] = owner
			if owner.Parent() != r.Context() || owner.Entry().Name() != "kernel" || !owner.Owns(key.Any()) {
				t.Fatalf("builtin lacks actual private host ownership: %v", key.Any().Name())
			}
			if got := owner.Metadata().Spec(); got.ID != "root" || len(got.Inject) != 0 || !reflect.DeepEqual(got.Provides, []cm.AnyKey{key.Any()}) {
				t.Fatalf("builtin fabricated extra metadata authority: %+v", got)
			}
			if !owner.Enter() {
				t.Fatal("builtin is not attached to a live native root")
			}
			owner.Leave()
			if r.Context().Owns(key.Any()) {
				t.Fatal("metadata-only root acquired resource authority")
			}
			if _, err := co.Resolve(owner, key); !errors.Is(err, cm.ErrUndeclared) {
				t.Fatalf("builtin ownership granted an unchecked service read: %v", err)
			}
			scope := co.NewScope[string](key.Any())
			scoped, err := scope.With(owner, "root-owned")
			if err != nil {
				t.Fatalf("actual builtin could not write its scope: %v", err)
			}
			if got, ok := scope.From(scoped); !ok || got != "root-owned" || !scoped.Owns(key.Any()) {
				t.Fatal("scope derivation lost actual builtin ownership")
			}
		}
		native, err := backend.(cc.RootProvider).RootContext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for i, owner := range owners {
			if native.Owns(rootAddress(t, owner, definitions[i])) {
				t.Fatal("builtin was disguised as an ordinary native service")
			}
			for j, key := range keys {
				if i != j && owner.Owns(key.Any()) {
					t.Fatal("single-key builtin owner acquired another builtin")
				}
			}
		}
		if got := rootInspect(t, ctx, backend); len(got.Fibers) != 0 || len(got.Provisions) != 0 {
			t.Fatalf("builtin fabricated a user fiber or service row: %+v", got)
		}
		events, err := backend.Events(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 4 {
			t.Fatalf("installation did not record exactly four real root effects: %+v", events)
		}
		for _, event := range events {
			if event.Fiber != 0 || event.Name != "root" || (event.Kind != "effect" && event.Kind != "effect:add") {
				t.Fatalf("installation recorded non-root-effect work: %+v", event)
			}
		}
	})
}

func TestInstallBuiltinDuplicateRetainsCallerResourceAndExcludesServices(t *testing.T) {
	both(t, func(t *testing.T, ctx context.Context, backend cc.Runtime) {
		entry := cm.NewEntry("plugin")
		key := cm.NewKey[*builtinTestValue]("hooks", 0)
		logger := cm.NewKey[*builtinTestValue]("logger", 0)
		r := root(t, backend, cm.RootSpec{Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{key.Any(), logger.Any()}})
		var originalDisposals, rejectedDisposals atomic.Int32
		original := &builtinTestValue{name: "original"}
		owner, err := co.InstallBuiltin(ctx, r, key, original, func(_ context.Context, got *builtinTestValue) error {
			if got != original {
				return errors.New("cleanup received a substitute resource")
			}
			originalDisposals.Add(1)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		before := rootInspect(t, ctx, backend)
		rejected := &builtinTestValue{name: "caller-still-owns"}
		if got, err := co.InstallBuiltin(ctx, r, key, rejected, func(context.Context, *builtinTestValue) error {
			rejectedDisposals.Add(1)
			return nil
		}); got != nil || !errors.Is(err, cm.ErrDuplicate) {
			t.Fatalf("duplicate transferred ownership: %v %v", got, err)
		}
		if after := rootInspect(t, ctx, backend); !reflect.DeepEqual(before, after) {
			t.Fatalf("duplicate registered extra native work: before=%+v after=%+v", before, after)
		}
		if originalDisposals.Load() != 0 || rejectedDisposals.Load() != 0 || !owner.Owns(key.Any()) {
			t.Fatal("duplicate disposed a caller value or replaced the actual resource")
		}
		if err := co.ProvideRoot(ctx, r, r.Context(), key, rejected, nil); !errors.Is(err, cm.ErrDuplicate) {
			t.Fatalf("ordinary root provision shadowed a builtin: %v", err)
		}
		if err := co.Provide(owner, key, rejected, nil); !errors.Is(err, cm.ErrDuplicate) {
			t.Fatalf("builtin owner also reserved an ordinary service: %v", err)
		}
		if err := co.ProvideFor(owner, key, func(*co.Context) *builtinTestValue {
			t.Error("registration invoked a provider view")
			return rejected
		}, nil); !errors.Is(err, cm.ErrDuplicate) {
			t.Fatalf("provider view shadowed a builtin: %v", err)
		}
		attempted := make(chan error, 1)
		plugin := start(t, ctx, r, r.Context(), entry, co.Spec{Metadata: cm.Spec{ID: "duplicate-plugin", Provides: []cm.AnyKey{key.Any()}}, Apply: func(c *co.Context) (cc.Disposer, error) {
			attempted <- co.Provide(c, key, rejected, nil)
			return nil, nil
		}})
		await(t, ctx, plugin)
		if err := rootReceive(t, ctx, attempted); !errors.Is(err, cm.ErrDuplicate) {
			t.Fatalf("ordinary plugin shadowed a builtin: %v", err)
		}
		if err := co.ProvideRoot(ctx, r, r.Context(), logger, original, nil); err != nil {
			t.Fatal(err)
		}
		before = rootInspect(t, ctx, backend)
		if got, err := co.InstallBuiltin(ctx, r, logger, rejected, func(context.Context, *builtinTestValue) error {
			rejectedDisposals.Add(1)
			return nil
		}); got != nil || !errors.Is(err, cm.ErrDuplicate) {
			t.Fatalf("builtin displaced an actual service reservation: %v %v", got, err)
		}
		if after := rootInspect(t, ctx, backend); !reflect.DeepEqual(before, after) {
			t.Fatalf("failed builtin install changed actual service work: before=%+v after=%+v", before, after)
		}
		if err := backend.Close(ctx); err != nil {
			t.Fatal(err)
		}
		if originalDisposals.Load() != 1 || rejectedDisposals.Load() != 0 || rejected.name != "caller-still-owns" || owner.Owns(key.Any()) {
			t.Fatalf("ownership transfer/disposal mismatch: original=%d rejected=%d owns=%v", originalDisposals.Load(), rejectedDisposals.Load(), owner.Owns(key.Any()))
		}
	})
}

func TestInstallBuiltinRootCloseDrainsLeaseAndRevokesBeforeActualDisposal(t *testing.T) {
	both(t, func(t *testing.T, ctx context.Context, backend cc.Runtime) {
		key := cm.NewKey[*builtinTestValue]("hooks", 0)
		logger := cm.NewKey[*builtinTestValue]("logger", 0)
		r := root(t, backend, cm.RootSpec{Keys: []cm.AnyKey{key.Any(), logger.Any()}})
		value := &builtinTestValue{name: "actual-resource"}
		var disposed atomic.Int32
		var owner *co.Context
		seen := make(chan *builtinTestValue, 1)
		var err error
		owner, err = co.InstallBuiltin(ctx, r, key, value, func(cleanup context.Context, got *builtinTestValue) error {
			if cleanup == nil || owner.Owns(key.Any()) {
				return errors.New("cleanup retained builtin ownership")
			}
			if inspection, err := backend.(cc.Inspector).Inspect(cleanup); err != nil || len(inspection.Fibers) != 0 || len(inspection.Provisions) != 0 {
				return fmt.Errorf("builtin cleanup lost native root or fabricated service state: %+v %v", inspection, err)
			}
			disposed.Add(1)
			seen <- got
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if !owner.Enter() {
			t.Fatal("actual root lease unavailable")
		}
		var leave sync.Once
		defer leave.Do(owner.Leave)
		closeCtx, cancelClose := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelClose()
		closed := make(chan error, 1)
		go func() { closed <- backend.Close(closeCtx) }()
		rootReceive(t, ctx, owner.Std().Done())
		if !owner.Owns(key.Any()) || disposed.Load() != 0 {
			t.Fatal("quiescence released a resource while its actual root lease was held")
		}
		if owner.Enter() {
			owner.Leave()
			t.Fatal("quiescent builtin accepted a new lease")
		}
		if _, err := co.NewScope[int](key.Any()).With(owner, 1); !errors.Is(err, cm.ErrScope) {
			t.Fatalf("quiescent resource wrote scope without a lease: %v", err)
		}
		select {
		case err := <-closed:
			t.Fatalf("Close did not wait for the actual root lease: %v", err)
		default:
		}
		leave.Do(owner.Leave)
		if err := rootReceive(t, ctx, closed); err != nil {
			t.Fatal(err)
		}
		if got := rootReceive(t, ctx, seen); got != value || disposed.Load() != 1 || owner.Owns(key.Any()) {
			t.Fatal("Close did not dispose and revoke the exact installed value once")
		}
		if late, err := co.InstallBuiltin(ctx, r, logger, value, func(context.Context, *builtinTestValue) error {
			disposed.Add(1)
			return nil
		}); late != nil || !errors.Is(err, cc.ErrClosed) || disposed.Load() != 1 {
			t.Fatalf("late install transferred ownership: %v %v disposals=%d", late, err, disposed.Load())
		}
	})
}

func TestInstallBuiltinPendingReservationIsNotOwnershipAndExcludesConcurrentRegistration(t *testing.T) {
	both(t, func(t *testing.T, ctx context.Context, backend cc.Runtime) {
		key := cm.NewKey[*builtinTestValue]("hooks", 0)
		release := make(chan struct{})
		var releaseOnce sync.Once
		defer releaseOnce.Do(func() { close(release) })
		gated := &builtinGatedInspector{Runtime: backend, RootProvider: backend.(cc.RootProvider), inspector: backend.(cc.Inspector), started: make(chan struct{}), release: release}
		r := root(t, gated, cm.RootSpec{Keys: []cm.AnyKey{key.Any()}})
		declaration, err := r.HostContext(ctx, cm.Spec{Provides: []cm.AnyKey{key.Any()}})
		if err != nil {
			t.Fatal(err)
		}
		original := &builtinTestValue{name: "pending-resource"}
		var disposals atomic.Int32
		installed := make(chan builtinInstallResult, 1)
		go func() {
			owner, err := co.InstallBuiltin(ctx, r, key, original, func(_ context.Context, got *builtinTestValue) error {
				if got != original {
					return errors.New("pending installation substituted its actual value")
				}
				disposals.Add(1)
				return nil
			})
			installed <- builtinInstallResult{owner, err}
		}()
		rootReceive(t, ctx, gated.started)
		if declaration.Owns(key.Any()) {
			t.Fatal("pending reservation became published resource ownership")
		}
		before := rootInspect(t, ctx, backend)
		if len(before.Fibers) != 0 || len(before.Provisions) != 0 || before.Sequence != 0 {
			t.Fatalf("pending reservation fabricated native work: %+v", before)
		}
		loser := &builtinTestValue{name: "loser"}
		if owner, err := co.InstallBuiltin(ctx, r, key, loser, func(context.Context, *builtinTestValue) error {
			t.Error("concurrent loser disposer was run")
			return nil
		}); owner != nil || !errors.Is(err, cm.ErrDuplicate) {
			t.Fatalf("concurrent duplicate bypassed pending reservation: %v %v", owner, err)
		}
		for name, provide := range map[string]func() error{
			"ProvideRoot": func() error { return co.ProvideRoot(ctx, r, r.Context(), key, loser, nil) },
			"Provide":     func() error { return co.Provide(declaration, key, loser, nil) },
			"ProvideFor": func() error {
				return co.ProvideFor(declaration, key, func(*co.Context) *builtinTestValue { return loser }, nil)
			},
		} {
			if err := provide(); !errors.Is(err, cm.ErrDuplicate) {
				t.Fatalf("%s bypassed pending builtin reservation: %v", name, err)
			}
		}
		if after := rootInspect(t, ctx, backend); !reflect.DeepEqual(before, after) {
			t.Fatalf("pending collisions created native work: before=%+v after=%+v", before, after)
		}
		releaseOnce.Do(func() { close(release) })
		result := rootReceive(t, ctx, installed)
		if result.err != nil || result.owner == nil || !result.owner.Owns(key.Any()) {
			t.Fatalf("pending winner did not publish actual root ownership: %v %v", result.owner, result.err)
		}
		if err := backend.Close(ctx); err != nil {
			t.Fatal(err)
		}
		if disposals.Load() != 1 || result.owner.Owns(key.Any()) {
			t.Fatal("concurrent winner did not transfer and release its actual value once")
		}
	})
}

func TestInstallBuiltinCanceledReservationRollsBackWithoutDisposingCallerValue(t *testing.T) {
	both(t, func(t *testing.T, ctx context.Context, backend cc.Runtime) {
		key := cm.NewKey[*builtinTestValue]("hooks", 0)
		release := make(chan struct{})
		var releaseOnce sync.Once
		defer releaseOnce.Do(func() { close(release) })
		gated := &builtinGatedInspector{Runtime: backend, RootProvider: backend.(cc.RootProvider), inspector: backend.(cc.Inspector), started: make(chan struct{}), release: release}
		r := root(t, gated, cm.RootSpec{Keys: []cm.AnyKey{key.Any()}})
		attemptCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		value := &builtinTestValue{name: "caller-retains"}
		var disposals atomic.Int32
		installed := make(chan builtinInstallResult, 1)
		go func() {
			owner, err := co.InstallBuiltin(attemptCtx, r, key, value, func(context.Context, *builtinTestValue) error {
				disposals.Add(1)
				return nil
			})
			installed <- builtinInstallResult{owner, err}
		}()
		rootReceive(t, ctx, gated.started)
		cancel()
		if result := rootReceive(t, ctx, installed); result.owner != nil || !errors.Is(result.err, context.Canceled) {
			t.Fatalf("canceled setup transferred resource ownership: %v %v", result.owner, result.err)
		}
		if got := rootInspect(t, ctx, backend); got.Sequence != 0 || len(got.Provisions) != 0 || len(got.Fibers) != 0 || disposals.Load() != 0 {
			t.Fatalf("failed setup retained native work or disposed caller resource: %+v count=%d", got, disposals.Load())
		}
		owner, err := co.InstallBuiltin(ctx, r, key, value, func(_ context.Context, got *builtinTestValue) error {
			if got != value {
				return errors.New("retry lost original resource")
			}
			disposals.Add(1)
			return nil
		})
		if err != nil || owner == nil || !owner.Owns(key.Any()) {
			t.Fatalf("failed setup stranded the registration reservation: %v %v", owner, err)
		}
		if err := backend.Close(ctx); err != nil {
			t.Fatal(err)
		}
		if disposals.Load() != 1 {
			t.Fatalf("failed setup or retry disposed caller resource %d times", disposals.Load())
		}
	})
}

func TestInstallBuiltinMetadataAndNativeChildrenCannotForgeRootOwnership(t *testing.T) {
	both(t, func(t *testing.T, ctx context.Context, backend cc.Runtime) {
		impostor := cm.NewEntry("kernel")
		key := cm.NewKey[*builtinTestValue]("hooks", 0)
		bound := cm.NewKey[*builtinTestValue]("registry", 0, cm.Classes(cm.ProviderBound, cm.Open))
		wrongMajor := cm.NewKey[*builtinTestValue]("logger", 1)
		ordinary := cm.NewKey[*builtinTestValue]("not-a-builtin", 0)
		r := root(t, backend, cm.RootSpec{Entries: []cm.Plugin{impostor}, Keys: []cm.AnyKey{key.Any(), bound.Any(), wrongMajor.Any(), ordinary.Any()}})
		declaration, err := r.HostContext(ctx, cm.Spec{Provides: []cm.AnyKey{key.Any()}})
		if err != nil {
			t.Fatal(err)
		}
		if declaration.Owns(key.Any()) {
			t.Fatal("host metadata declaration became builtin ownership")
		}
		owner, err := co.InstallBuiltin(ctx, r, key, &builtinTestValue{name: "hooks"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, derived := range []*co.Context{owner.Extend(), owner.WithValues(map[string]any{"entry": "kernel", "provides": key.Any()})} {
			if !derived.Owns(key.Any()) {
				t.Fatal("metadata-only derivation lost the actual root resource")
			}
		}
		isolated, err := owner.Isolate(key.Any())
		if err != nil {
			t.Fatal(err)
		}
		if isolated.Owns(key.Any()) {
			t.Fatal("isolated address acquired an uninstalled builtin resource")
		}
		contexts := make(chan *co.Context, 2)
		plugin := start(t, ctx, r, owner.WithValues(map[string]any{"host": owner}), impostor, co.Spec{Metadata: cm.Spec{ID: "root", Provides: []cm.AnyKey{key.Any()}}, Apply: func(c *co.Context) (cc.Disposer, error) {
			contexts <- c.WithValues(map[string]any{"entry": owner.Entry(), "root": owner})
			return nil, nil
		}})
		await(t, ctx, plugin)
		pluginContext := rootReceive(t, ctx, contexts)
		if plugin.ID != 1 || pluginContext.Entry().Name() != owner.Entry().Name() || pluginContext.Entry() == owner.Entry() || pluginContext.Owns(key.Any()) {
			t.Fatal("same-text user entry acquired the private root resource")
		}
		child, err := owner.Child(co.Spec{Metadata: cm.Spec{ID: "root", Provides: []cm.AnyKey{key.Any()}}, Apply: func(c *co.Context) (cc.Disposer, error) {
			contexts <- c
			return nil, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		await(t, ctx, child)
		childContext := rootReceive(t, ctx, contexts)
		if childContext.Entry() != owner.Entry() || childContext.Owns(key.Any()) {
			t.Fatal("native child inherited root resource authority from host entry alone")
		}
		if _, err := co.NewScope[int](key.Any()).With(childContext, 1); !errors.Is(err, cm.ErrScope) {
			t.Fatalf("native child wrote root-owned scope: %v", err)
		}
		before := rootInspect(t, ctx, backend)
		for _, candidate := range []struct {
			key  cm.Key[*builtinTestValue]
			want error
		}{{bound, cm.ErrBinding}, {wrongMajor, cm.ErrKey}, {ordinary, cm.ErrKey}, {cm.Key[*builtinTestValue]{}, cm.ErrKey}, {cm.NewKey[*builtinTestValue]("reflect", 0), cm.ErrKey}} {
			if got, err := co.InstallBuiltin(ctx, r, candidate.key, &builtinTestValue{}, nil); got != nil || !errors.Is(err, candidate.want) {
				t.Fatalf("invalid builtin %s accepted: %v %v", candidate.key.Any().Name(), got, err)
			}
		}
		if got, err := co.InstallBuiltin(ctx, r, cm.NewKey[int]("hooks", 0), 1, nil); got != nil || !errors.Is(err, cm.ErrCollision) {
			t.Fatalf("builtin type collision accepted: %v %v", got, err)
		}
		if got, err := co.InstallBuiltin(ctx, nil, key, &builtinTestValue{}, nil); got != nil || !errors.Is(err, cm.ErrContext) {
			t.Fatalf("nil authority accepted: %v %v", got, err)
		}
		if after := rootInspect(t, ctx, backend); !reflect.DeepEqual(before, after) {
			t.Fatalf("invalid install registered native work: before=%+v after=%+v", before, after)
		}
	})
}
