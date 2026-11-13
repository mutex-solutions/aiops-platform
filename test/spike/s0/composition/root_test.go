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

func rootInspect(t *testing.T, ctx context.Context, backend cc.Runtime) cc.Inspection {
	t.Helper()
	inspector, ok := backend.(cc.Inspector)
	if !ok {
		t.Fatal("backend has no native Inspector")
	}
	inspection, err := inspector.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return inspection
}

func rootReceive[T any](t *testing.T, ctx context.Context, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		var zero T
		return zero
	}
}

func rootAddress(t *testing.T, at *co.Context, key cm.AnyKey) string {
	t.Helper()
	address, err := at.NativeAddress(key)
	if err != nil {
		t.Fatal(err)
	}
	return address
}

func TestProvideRootUsesNativeOwnerAndSharedTypedBindings(t *testing.T) {
	both(t, func(t *testing.T, ctx context.Context, backend cc.Runtime) {
		type service struct {
			Name    string
			Version int
		}
		entry := cm.NewEntry("consumer")
		key := cm.NewKey[service]("host-service", 1)
		zero := cm.NewKey[int]("host-zero", 1)
		nilKey := cm.NewKey[any]("host-nil", 1)
		r := root(t, backend, cm.RootSpec{Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{key.Any(), zero.Any(), nilKey.Any()}})
		if got := rootInspect(t, ctx, backend); len(got.Fibers) != 0 || len(got.Provisions) != 0 || got.Sequence != 0 {
			t.Fatalf("construction mounted hidden root work: %+v", got)
		}
		if !reflect.DeepEqual(r.Context().Metadata().Spec(), cm.Spec{}) || r.Context().Entry() != (cm.Plugin{}) {
			t.Fatal("root metadata fabricated an entry or declarations")
		}
		if _, err := co.Resolve(r.Context(), key); !errors.Is(err, cm.ErrBinding) {
			t.Fatalf("ordinary root acquired plugin read authority: %v", err)
		}
		if r.Context().Enter() {
			r.Context().Leave()
			t.Fatal("metadata root exposed a native plugin lease")
		}
		if r.Context().Owns(key.Any()) {
			t.Fatal("metadata root exposed native plugin ownership")
		}
		if err := r.Context().Defer("not-a-native-root", func(context.Context) error { return nil }); !errors.Is(err, cc.ErrInactive) {
			t.Fatalf("metadata root accepted plugin lifecycle work: %v", err)
		}

		var ready atomic.Bool
		var checks atomic.Int32
		value := service{Name: "immutable-host-value", Version: 7}
		if err := co.ProvideRoot(ctx, r, r.Context(), key, value, func(caller *co.Context) bool {
			checks.Add(1)
			return ready.Load() && caller.Entry() == entry
		}); err != nil {
			t.Fatal(err)
		}
		if err := co.ProvideRoot(ctx, r, r.Context(), zero, 0, nil); err != nil {
			t.Fatal(err)
		}
		if err := co.ProvideRoot[any](ctx, r, r.Context(), nilKey, nil, nil); err != nil {
			t.Fatal(err)
		}
		// Service payloads are immutable host data, not containers the frontend can
		// deep-clone. A value-type payload also proves typed provision is by value.
		value.Version = 99
		wantValue := service{Name: "immutable-host-value", Version: 7}
		addresses := []string{rootAddress(t, r.Context(), key.Any()), rootAddress(t, r.Context(), zero.Any()), rootAddress(t, r.Context(), nilKey.Any())}
		wantValues := []any{wantValue, 0, nil}
		inspection := rootInspect(t, ctx, backend)
		if len(inspection.Fibers) != 0 || len(inspection.Provisions) != len(addresses) {
			t.Fatalf("root provision created a hidden user fiber: %+v", inspection)
		}
		uids := make([]uint64, len(addresses))
		for i, p := range inspection.Provisions {
			observed, ok := co.ProvisionObservation(p.Value)
			if p.Key != addresses[i] || p.Owner != 0 || p.Name != "root" || !p.Active || p.UID == 0 || !ok || !reflect.DeepEqual(observed, wantValues[i]) {
				t.Fatalf("actual typed root reservation %d: %+v observation=%#v/%v", i, p, observed, ok)
			}
			uids[i] = p.UID
		}
		if err := co.ProvideRoot(ctx, r, r.Context(), key, service{Name: "duplicate"}, nil); err == nil {
			t.Fatal("root bypassed the native duplicate reservation")
		}
		native, err := backend.(cc.RootProvider).RootContext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		again, err := backend.(cc.RootProvider).RootContext(ctx)
		if err != nil || native != again || !native.Owns(addresses[0]) {
			t.Fatalf("provision did not belong to the one actual native root: %p/%p %v", native, again, err)
		}

		seen := make(chan []any, 2)
		consumers := make([]*co.Fiber, 2)
		for i := range consumers {
			consumers[i] = start(t, ctx, r, r.Context(), entry, co.Spec{Metadata: cm.Spec{ID: fmt.Sprintf("consumer-%d", i), Inject: []cm.Dep{cm.Need(key), cm.Need(zero), cm.Need(nilKey)}}, Apply: func(c *co.Context) (cc.Disposer, error) {
				x, err := co.Resolve(c, key)
				if err != nil {
					return nil, err
				}
				z, err := co.Resolve(c, zero)
				if err != nil {
					return nil, err
				}
				n, err := co.Resolve(c, nilKey)
				if err == nil {
					seen <- []any{x, z, n}
				}
				return nil, err
			}})
			if consumers[i].ID != uint64(i+1) {
				t.Fatalf("native root consumed an ordinary user ID: consumer %d has ID %d", i, consumers[i].ID)
			}
			await(t, ctx, consumers[i])
			state(t, ctx, backend, consumers[i], cc.Pending)
		}
		select {
		case got := <-seen:
			t.Fatalf("unavailable root value ran Apply: %#v", got)
		default:
		}
		ready.Store(true)
		if err := r.Notify(ctx, r.Context(), key.Any()); err != nil {
			t.Fatal(err)
		}
		for _, consumer := range consumers {
			await(t, ctx, consumer)
			state(t, ctx, backend, consumer, cc.Active)
			if got := rootReceive(t, ctx, seen); !reflect.DeepEqual(got, wantValues) {
				t.Fatalf("real consumer resolved %#v, want %#v", got, wantValues)
			}
		}
		inspection = rootInspect(t, ctx, backend)
		if len(inspection.Fibers) != 2 || len(inspection.Provisions) != 3 || checks.Load() < 4 {
			t.Fatalf("typed root bypassed actual consumers/availability: %+v checks=%d", inspection, checks.Load())
		}
		for _, fiber := range inspection.Fibers {
			if fiber.ID == 0 || !reflect.DeepEqual(fiber.Epoch, uids) || fiber.Target == nil || !reflect.DeepEqual(fiber.Target.UIDs, uids) {
				t.Fatalf("consumer did not retain the same actual root provision UIDs: %+v want=%v", fiber, uids)
			}
		}
		events, err := backend.Events(ctx)
		if err != nil {
			t.Fatal(err)
		}
		mounts := 0
		for _, event := range events {
			if event.Kind == "mount" {
				mounts++
				if event.Fiber == 0 || event.Name == "root" {
					t.Fatalf("root provision fabricated a user mount: %+v", event)
				}
			}
		}
		if mounts != 2 {
			t.Fatalf("root provision consumed a hidden user mount: %d", mounts)
		}
		undeclared := start(t, ctx, r, r.Context(), entry, co.Spec{Metadata: cm.Spec{ID: "undeclared"}, Apply: func(c *co.Context) (cc.Disposer, error) {
			if _, err := co.Resolve(c, key); !errors.Is(err, cm.ErrUndeclared) {
				return nil, fmt.Errorf("open root value authorized undeclared read: %v", err)
			}
			return nil, nil
		}})
		await(t, ctx, undeclared)
	})
}

func TestProvideRootMetadataDerivationSelectsActualRealms(t *testing.T) {
	both(t, func(t *testing.T, ctx context.Context, backend cc.Runtime) {
		entry := cm.NewEntry("realm-reader")
		key := cm.NewKey[string]("realm-service", 1)
		global := cm.NewKey[int]("untouched-service", 1)
		r := root(t, backend, cm.RootSpec{Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{key.Any(), global.Any()}})
		extended := r.Context().Extend()
		label := cm.NewLabel("same-label-text")
		isolated, err := extended.Isolate(key.Any(), label)
		if err != nil {
			t.Fatal(err)
		}
		joined, err := r.Context().Isolate(key.Any(), label)
		if err != nil {
			t.Fatal(err)
		}
		other, err := r.Context().Isolate(key.Any(), cm.NewLabel("same-label-text"))
		if err != nil {
			t.Fatal(err)
		}
		if extended.Parent() != r.Context() || isolated.Parent() != extended || joined.Parent() != r.Context() || !reflect.DeepEqual(isolated.Metadata().Spec(), cm.Spec{}) || isolated.Entry() != (cm.Plugin{}) {
			t.Fatal("metadata-only root derivation changed parentage or invented a frame")
		}
		rootAddr := rootAddress(t, r.Context(), key.Any())
		isolatedAddr := rootAddress(t, isolated, key.Any())
		otherAddr := rootAddress(t, other, key.Any())
		if rootAddress(t, extended, key.Any()) != rootAddr || rootAddress(t, joined, key.Any()) != isolatedAddr || rootAddr == isolatedAddr || rootAddr == otherAddr || isolatedAddr == otherAddr {
			t.Fatal("Extend/Isolate did not preserve opaque selective realm identity")
		}
		for _, provision := range []struct {
			at    *co.Context
			value string
		}{{r.Context(), "root"}, {isolated, "joined"}, {other, "other"}} {
			if err := co.ProvideRoot(ctx, r, provision.at, key, provision.value, nil); err != nil {
				t.Fatal(err)
			}
		}
		if err := co.ProvideRoot(ctx, r, extended, global, 42, nil); err != nil {
			t.Fatal(err)
		}
		globalAddr := rootAddress(t, r.Context(), global.Any())
		rows := make(map[string]cc.ProvisionSnapshot)
		for _, p := range rootInspect(t, ctx, backend).Provisions {
			if p.Owner != 0 || p.Name != "root" || !p.Active {
				t.Fatalf("derived root lost native root ownership: %+v", p)
			}
			rows[p.Key] = p
		}
		if len(rows) != 4 || rows[rootAddr].UID == rows[isolatedAddr].UID || rows[rootAddr].UID == rows[otherAddr].UID || rows[isolatedAddr].UID == rows[otherAddr].UID {
			t.Fatalf("same typed key did not reserve distinct real realms: %+v", rows)
		}
		type read struct {
			name  string
			value string
			other int
		}
		seen := make(chan read, 5)
		consumers := make(map[uint64]string)
		for _, candidate := range []struct {
			name string
			at   *co.Context
			want string
		}{{"root-reader", r.Context(), "root"}, {"extended-reader", extended, "root"}, {"isolated-reader", isolated, "joined"}, {"joined-reader", joined, "joined"}, {"other-reader", other, "other"}} {
			if rootAddress(t, candidate.at, global.Any()) != globalAddr {
				t.Fatal("isolating one key also isolated the untouched key")
			}
			consumer := start(t, ctx, r, candidate.at, entry, co.Spec{Metadata: cm.Spec{ID: candidate.name, Inject: []cm.Dep{cm.Need(key), cm.Need(global)}}, Apply: func(c *co.Context) (cc.Disposer, error) {
				x, err := co.Resolve(c, key)
				if err != nil {
					return nil, err
				}
				y, err := co.Resolve(c, global)
				if err == nil {
					seen <- read{name: c.Metadata().Spec().ID, value: x, other: y}
				}
				return nil, err
			}})
			await(t, ctx, consumer)
			if got := rootReceive(t, ctx, seen); got != (read{name: candidate.name, value: candidate.want, other: 42}) {
				t.Fatalf("actual realm read: %+v", got)
			}
			consumers[consumer.ID] = rootAddress(t, candidate.at, key.Any())
		}
		inspection := rootInspect(t, ctx, backend)
		if len(inspection.Fibers) != 5 || len(inspection.Provisions) != 4 {
			t.Fatalf("realm metadata fabricated lifecycle rows: %+v", inspection)
		}
		for _, fiber := range inspection.Fibers {
			want := []uint64{rows[consumers[fiber.ID]].UID, rows[globalAddr].UID}
			if fiber.State != cc.Active || !reflect.DeepEqual(fiber.Epoch, want) {
				t.Fatalf("realm read bypassed actual native epoch: %+v want=%v", fiber, want)
			}
		}
	})
}

func TestProvideRootRejectsBoundForeignAndIncompatibleAuthorityWithoutReservation(t *testing.T) {
	both(t, func(t *testing.T, ctx context.Context, backend cc.Runtime) {
		approved, denied := cm.NewEntry("approved"), cm.NewEntry("denied")
		open := cm.NewKey[string]("registered-open", 1, cm.Config[string]())
		bound := cm.NewKey[string]("registered-bound", 1, cm.Classes(cm.ProviderBound, cm.Open))
		spec := cm.RootSpec{Entries: []cm.Plugin{approved, denied}, Keys: []cm.AnyKey{open.Any(), bound.Any()}, Bindings: []cm.Binding{{Key: bound.Any(), Providers: []cm.Plugin{approved}}}}
		r, foreign := root(t, backend, spec), root(t, backend, spec)
		if f, err := r.Start(ctx, r.Context(), denied, co.Spec{Metadata: cm.Spec{ID: "denied-provider", Provides: []cm.AnyKey{bound.Any()}}}); f != nil || !errors.Is(err, cm.ErrBinding) {
			t.Fatalf("WithEntry bypassed bound provider validation: %v %v", f, err)
		}
		captured := make(chan *co.Context, 1)
		provider := start(t, ctx, r, r.Context(), approved, co.Spec{Metadata: cm.Spec{ID: "approved-provider", Provides: []cm.AnyKey{bound.Any()}}, Apply: func(c *co.Context) (cc.Disposer, error) {
			captured <- c
			return nil, nil
		}})
		await(t, ctx, provider)
		providerContext := rootReceive(t, ctx, captured)
		before := rootInspect(t, ctx, backend)
		if len(before.Provisions) != 0 || len(before.Fibers) != 1 {
			t.Fatalf("declarations reserved root services: %+v", before)
		}
		for _, candidate := range []struct {
			name string
			call func() error
			want error
		}{
			{"bound root", func() error { return co.ProvideRoot(ctx, r, r.Context(), bound, "denied", nil) }, cm.ErrBinding},
			{"bound approved entry context", func() error { return co.ProvideRoot(ctx, r, providerContext, bound, "denied", nil) }, cm.ErrBinding},
			{"nil authority", func() error { return co.ProvideRoot(ctx, nil, r.Context(), open, "denied", nil) }, cm.ErrContext},
			{"nil context", func() error { return co.ProvideRoot(ctx, r, nil, open, "denied", nil) }, cm.ErrContext},
			{"foreign authority", func() error { return co.ProvideRoot(ctx, foreign, r.Context(), open, "denied", nil) }, cm.ErrContext},
			{"foreign context", func() error { return co.ProvideRoot(ctx, r, foreign.Context(), open, "denied", nil) }, cm.ErrContext},
			{"zero context", func() error { return co.ProvideRoot(ctx, r, new(co.Context), open, "denied", nil) }, cm.ErrContext},
			{"zero key", func() error { return co.ProvideRoot(ctx, r, r.Context(), cm.Key[string]{}, "denied", nil) }, cm.ErrKey},
			{"unregistered key", func() error {
				return co.ProvideRoot(ctx, r, r.Context(), cm.NewKey[string]("unknown", 1), "denied", nil)
			}, cm.ErrKey},
			{"unregistered major", func() error {
				return co.ProvideRoot(ctx, r, r.Context(), cm.NewKey[string](open.Any().Name(), 2), "denied", nil)
			}, cm.ErrKey},
			{"incompatible type", func() error {
				return co.ProvideRoot(ctx, r, r.Context(), cm.NewKey[int](open.Any().Name(), 1, cm.Config[string]()), 1, nil)
			}, cm.ErrCollision},
			{"incompatible config", func() error {
				return co.ProvideRoot(ctx, r, r.Context(), cm.NewKey[string](open.Any().Name(), 1, cm.Config[int]()), "denied", nil)
			}, cm.ErrCollision},
			{"incompatible consumer class", func() error {
				return co.ProvideRoot(ctx, r, r.Context(), cm.NewKey[string](open.Any().Name(), 1, cm.Config[string](), cm.Classes(cm.Open, cm.ConsumerBound)), "denied", nil)
			}, cm.ErrCollision},
			{"invalid provider class", func() error {
				return co.ProvideRoot(ctx, r, r.Context(), cm.NewKey[string](open.Any().Name(), 1, cm.Classes(cm.Class(255), cm.Open)), "denied", nil)
			}, cm.ErrBinding},
			{"invalid consumer class", func() error {
				return co.ProvideRoot(ctx, r, r.Context(), cm.NewKey[string](open.Any().Name(), 1, cm.Config[string](), cm.Classes(cm.Open, cm.Class(255))), "denied", nil)
			}, cm.ErrCollision},
		} {
			if err := candidate.call(); !errors.Is(err, candidate.want) {
				t.Fatalf("%s: %v, want %v", candidate.name, err, candidate.want)
			}
			after := rootInspect(t, ctx, backend)
			if len(after.Provisions) != 0 || len(after.Fibers) != len(before.Fibers) || after.Sequence != before.Sequence {
				t.Fatalf("%s touched native reservation or lifecycle state: before=%+v after=%+v", candidate.name, before, after)
			}
		}
		if err := co.ProvideRoot(ctx, r, r.Context(), open, "first-success", nil); err != nil {
			t.Fatal(err)
		}
		if err := co.Provide(providerContext, bound, "approved-entry-only", nil); err != nil {
			t.Fatalf("denied root attempt reserved the bound provider's key: %v", err)
		}
		rows := rootInspect(t, ctx, backend).Provisions
		if len(rows) != 2 || rows[0].UID != 1 || rows[0].Owner != 0 || rows[0].Name != "root" || rows[1].UID != 2 || rows[1].Owner != provider.ID {
			t.Fatalf("validation consumed a successful native reservation: %+v", rows)
		}
	})
}

func TestProvideRootCloseRetainsConsumerBindingsUntilRootLIFOCleanup(t *testing.T) {
	both(t, func(t *testing.T, ctx context.Context, backend cc.Runtime) {
		entry := cm.NewEntry("unit")
		key := cm.NewKey[string]("root-retained", 1)
		r := root(t, backend, cm.RootSpec{Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{key.Any()}})
		native, err := backend.(cc.RootProvider).RootContext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		address := rootAddress(t, r.Context(), key.Any())
		consumerRelease, unrelatedRelease := make(chan struct{}), make(chan struct{})
		var consumerOnce, unrelatedOnce sync.Once
		releaseAll := func() {
			consumerOnce.Do(func() { close(consumerRelease) })
			unrelatedOnce.Do(func() { close(unrelatedRelease) })
		}
		defer releaseAll()
		t.Cleanup(releaseAll)
		var consumerComplete, unrelatedComplete atomic.Bool
		type rootCleanup struct {
			label      string
			inspection cc.Inspection
		}
		rootCleanups := make(chan rootCleanup, 2)
		// Register this before ProvideRoot so native LIFO removes the provision
		// before this final callback, while the native owner can still Inspect.
		if err := native.Defer("root-after", func(cleanup context.Context) error {
			inspection, err := backend.(cc.Inspector).Inspect(cleanup)
			if err != nil {
				return err
			}
			rootCleanups <- rootCleanup{label: "root-after", inspection: inspection}
			if !consumerComplete.Load() || !unrelatedComplete.Load() || len(inspection.Provisions) != 0 {
				return fmt.Errorf("root-after ran before users/provision removal: %+v", inspection)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := co.ProvideRoot(ctx, r, r.Context(), key, "retained-through-cleanup", nil); err != nil {
			t.Fatal(err)
		}
		original := rootInspect(t, ctx, backend).Provisions[0]
		if err := native.Defer("root-check", func(cleanup context.Context) error {
			inspection, err := backend.(cc.Inspector).Inspect(cleanup)
			if err != nil {
				return err
			}
			rootCleanups <- rootCleanup{label: "root-check", inspection: inspection}
			if !consumerComplete.Load() || !unrelatedComplete.Load() || len(inspection.Provisions) != 1 {
				return fmt.Errorf("root resources overtook actual user cleanup: %+v", inspection)
			}
			p := inspection.Provisions[0]
			value, ok := co.ProvisionObservation(p.Value)
			if p.UID != original.UID || p.Owner != 0 || p.Name != "root" || p.Active || !ok || value != "retained-through-cleanup" {
				return fmt.Errorf("root-check lost actual retained root value: %+v value=%v", p, value)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		contexts := make(chan *co.Context, 2)
		entered := make(chan string, 2)
		consumerFinished := make(chan struct{})
		consumer := start(t, ctx, r, r.Context(), entry, co.Spec{Metadata: cm.Spec{ID: "root-consumer", Inject: []cm.Dep{cm.Need(key)}}, Apply: func(c *co.Context) (cc.Disposer, error) {
			if value, err := co.Resolve(c, key); err != nil || value != "retained-through-cleanup" {
				return nil, fmt.Errorf("Apply did not bind actual root value: %v %v", value, err)
			}
			contexts <- c
			return func(context.Context) error {
				if value, err := co.Resolve(c, key); err != nil || value != "retained-through-cleanup" {
					return fmt.Errorf("cleanup lost retained root binding: %v %v", value, err)
				}
				entered <- "consumer"
				<-consumerRelease
				if value, err := co.Resolve(c, key); err != nil || value != "retained-through-cleanup" {
					return fmt.Errorf("held cleanup lost retained root binding: %v %v", value, err)
				}
				consumerComplete.Store(true)
				close(consumerFinished)
				return nil
			}, nil
		}})
		await(t, ctx, consumer)
		consumerContext := rootReceive(t, ctx, contexts)
		unrelated := start(t, ctx, r, r.Context(), entry, co.Spec{Metadata: cm.Spec{ID: "unrelated-user"}, Apply: func(c *co.Context) (cc.Disposer, error) {
			contexts <- c
			return func(context.Context) error {
				entered <- "unrelated"
				<-unrelatedRelease
				unrelatedComplete.Store(true)
				return nil
			}, nil
		}})
		await(t, ctx, unrelated)
		unrelatedContext := rootReceive(t, ctx, contexts)
		closeCtx, cancelClose := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelClose()
		closed, closeFinished := make(chan error, 1), make(chan struct{})
		go func() {
			closed <- backend.Close(closeCtx)
			close(closeFinished)
		}()
		defer func() {
			releaseAll()
			select {
			case <-closeFinished:
			case <-closeCtx.Done():
				t.Error("native Close goroutine did not finish after releasing test gates")
			}
		}()
		rootReceive(t, ctx, native.Std().Done())
		rootReceive(t, ctx, consumerContext.Std().Done())
		rootReceive(t, ctx, unrelatedContext.Std().Done())
		started := map[string]bool{rootReceive(t, ctx, entered): true}
		started[rootReceive(t, ctx, entered)] = true
		if !started["consumer"] || !started["unrelated"] {
			t.Fatalf("Close failed to quiesce both ordinary users: %v", started)
		}
		if native.Enter() {
			native.Leave()
			t.Fatal("closing native root accepted a new lease")
		}
		inspection := rootInspect(t, ctx, backend)
		if len(inspection.Fibers) != 2 || len(inspection.Provisions) != 1 || inspection.Provisions[0].UID != original.UID || inspection.Provisions[0].Owner != 0 || inspection.Provisions[0].Active || !native.Owns(address) {
			t.Fatalf("quiescence removed the still-retained root reservation: %+v", inspection)
		}
		for _, fiber := range inspection.Fibers {
			if fiber.Alive || fiber.State != cc.Unloading || fiber.Target != nil {
				t.Fatalf("Close left an ordinary user live: %+v", fiber)
			}
		}
		if value, err := co.Resolve(consumerContext, key); err != nil || value != "retained-through-cleanup" {
			t.Fatalf("gated real consumer lost its committed root read: %v %v", value, err)
		}
		select {
		case err := <-closed:
			t.Fatalf("Close returned before user gates completed: %v", err)
		default:
		}
		consumerOnce.Do(func() { close(consumerRelease) })
		rootReceive(t, ctx, consumerFinished)
		await(t, ctx, consumer)
		// The unrelated user is not a root-service dependant. It must still be
		// joined before host resources and the root reservation are released.
		select {
		case cleanup := <-rootCleanups:
			t.Fatalf("root cleanup overtook unrelated ordinary user: %+v", cleanup)
		default:
		}
		if got := rootInspect(t, ctx, backend); len(got.Provisions) != 1 || got.Provisions[0].UID != original.UID {
			t.Fatalf("root provision removed while an ordinary user was gated: %+v", got)
		}
		unrelatedOnce.Do(func() { close(unrelatedRelease) })
		if err := rootReceive(t, ctx, closed); err != nil {
			t.Fatal(err)
		}
		first, last := rootReceive(t, ctx, rootCleanups), rootReceive(t, ctx, rootCleanups)
		if first.label != "root-check" || len(first.inspection.Provisions) != 1 || last.label != "root-after" || len(last.inspection.Provisions) != 0 {
			t.Fatalf("root effects did not follow native LIFO/provision-removal order: first=%+v last=%+v", first, last)
		}
		// Reconciler's owner has exited now: use captured cleanup inspections,
		// not a fake post-Close Inspector response, to prove actual deletion.
		if native.Owns(address) {
			t.Fatal("closed native root still owns its released provision")
		}
		if _, err := co.Resolve(consumerContext, key); !errors.Is(err, cc.ErrInactive) {
			t.Fatalf("released consumer retained a root read after Close: %v", err)
		}
		if err := co.ProvideRoot(ctx, r, r.Context(), key, "late", nil); !errors.Is(err, cc.ErrClosed) {
			t.Fatalf("closed native root accepted a new typed provision: %v", err)
		}
	})
}
