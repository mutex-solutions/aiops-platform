// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// These tests use both actual cores. A missing Batch implementation is a failure,
// not permission to replay operations through the single-operation API.
func batchCandidates(t *testing.T, test func(*testing.T, func() Runtime)) {
	t.Helper()
	for _, candidate := range []struct {
		name string
		make func() Runtime
	}{{"reconciler", NewRecordingReconciler}} {
		t.Run(candidate.name, func(t *testing.T) { test(t, candidate.make) })
	}
}

func batchRuntime(t *testing.T, makeRuntime func() Runtime) (context.Context, Runtime, Batcher) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	r := makeRuntime()
	// Register Close before per-test gates, so even a failed assertion releases
	// its gates before joining the native lifecycle and global observer workers.
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer closeCancel()
		if err := closeRuntime(closeCtx, r); err != nil {
			t.Errorf("Close actual batch runtime: %v", err)
		}
	})
	batcher, ok := r.(Batcher)
	if !ok {
		t.Fatal("actual core does not implement Batcher")
	}
	return ctx, r, batcher
}

func batchGate(t *testing.T) (<-chan struct{}, func()) {
	t.Helper()
	release := make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(release) }) }
	t.Cleanup(open)
	return release, open
}

func batchReceive[T any](t *testing.T, ctx context.Context, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-ctx.Done():
		t.Fatalf("batch lifecycle barrier: %v", ctx.Err())
		var zero T
		return zero
	}
}

func mountFixture(t *testing.T, ctx context.Context, r Runtime, spec Spec) uint64 {
	t.Helper()
	id, err := r.Mount(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func batchSettle(t *testing.T, ctx context.Context, r Runtime) {
	t.Helper()
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
}

func batchInspect(t *testing.T, ctx context.Context, r Runtime) Inspection {
	t.Helper()
	inspector, ok := r.(Inspector)
	if !ok {
		t.Fatal("actual batch core does not implement Inspector")
	}
	inspection, err := inspector.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return inspection
}

func batchFiber(t *testing.T, inspection Inspection, id uint64) Snapshot {
	t.Helper()
	for _, fiber := range inspection.Fibers {
		if fiber.ID == id {
			return fiber
		}
	}
	t.Fatalf("native inspection missing fiber %d: %+v", id, inspection)
	return Snapshot{}
}

func batchProvision(t *testing.T, inspection Inspection, key string) ProvisionSnapshot {
	t.Helper()
	for _, provision := range inspection.Provisions {
		if provision.Key == key {
			return provision
		}
	}
	t.Fatalf("native inspection missing provision %q: %+v", key, inspection)
	return ProvisionSnapshot{}
}

func batchEvents(t *testing.T, ctx context.Context, r Runtime) []LedgerEvent {
	t.Helper()
	events, err := r.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i, event := range events {
		if event.Sequence != uint64(i+1) {
			t.Fatalf("native ledger sequence[%d] = %d", i, event.Sequence)
		}
	}
	return events
}

func batchUnchanged(t *testing.T, ctx context.Context, r Runtime, before Inspection, events []LedgerEvent) {
	t.Helper()
	after := batchInspect(t, ctx, r)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("rejected batch mutated native fibers/provisions/watermark:\nbefore=%+v\nafter=%+v", before, after)
	}
	if actual := batchEvents(t, ctx, r); !reflect.DeepEqual(actual, events) {
		t.Fatalf("rejected batch mutated native ledger:\nbefore=%+v\nafter=%+v", events, actual)
	}
	if before.Sequence != uint64(len(events)) || after.Sequence != before.Sequence {
		t.Fatalf("rejected batch changed total sequence: before=%d after=%d events=%d", before.Sequence, after.Sequence, len(events))
	}
}

func batchQuiesced(t *testing.T, ctx context.Context, c *Handle) {
	t.Helper()
	batchReceive(t, ctx, c.Std().Done())
	if c.Enter() {
		c.Leave()
		t.Fatal("batch-invalidated activation accepted a new native lease")
	}
}

func TestSharedBatchPreflightAndOwnedMountDeclarations(t *testing.T) {
	batchCandidates(t, func(t *testing.T, makeRuntime func() Runtime) {
		ctx, r, batcher := batchRuntime(t, makeRuntime)
		roots, ok := r.(RootProvider)
		if !ok {
			t.Fatal("actual core does not expose its native host root")
		}
		root, err := roots.RootContext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var baselineApplies, forbiddenCallbacks atomic.Int64
		baseline := mountFixture(t, ctx, r, Spec{ID: "baseline", Provides: []string{"baseline-service"}, Apply: func(c *Handle) error {
			return c.Provide("baseline-service", baselineApplies.Add(1))
		}})
		batchSettle(t, ctx, r)
		if baseline != 1 {
			t.Fatalf("native host root consumed a user mount ID: baseline=%d", baseline)
		}
		before, events := batchInspect(t, ctx, r), batchEvents(t, ctx, r)
		if fiber := batchFiber(t, before, baseline); fiber.State != Active || baselineApplies.Load() != 1 {
			t.Fatalf("baseline is not stable before validation: %+v", fiber)
		}
		probe := Spec{ID: "must-not-commit", Check: func(*Handle) bool {
			forbiddenCallbacks.Add(1)
			return true
		}, Apply: func(*Handle) error { forbiddenCallbacks.Add(1); return nil }}
		// Every invalid operation is LAST. A valid first Mount must not consume an
		// ID, emit an event, or schedule Check/Apply before suffix validation ends.
		for _, invalid := range []struct {
			name string
			ops  []BatchOperation
		}{
			{"unknown-update", []BatchOperation{{Kind: BatchUpdate, Fiber: 999}}},
			{"unknown-dispose", []BatchOperation{{Kind: BatchDispose, Fiber: 999}}},
			{"host-root-update", []BatchOperation{{Kind: BatchUpdate, Fiber: 0}}},
			{"host-root-dispose", []BatchOperation{{Kind: BatchDispose, Fiber: 0}}},
			{"duplicate-need", []BatchOperation{{Kind: BatchMount, Spec: Spec{ID: "bad", Needs: []string{"same", "same"}}}}},
			{"empty-provide", []BatchOperation{{Kind: BatchMount, Spec: Spec{ID: "bad", Provides: []string{""}}}}},
			{"mount-with-target", []BatchOperation{{Kind: BatchMount, Fiber: baseline, Spec: Spec{ID: "bad"}}}},
			{"unused-update-spec", []BatchOperation{{Kind: BatchUpdate, Fiber: baseline, Spec: Spec{ID: "unused"}}}},
			{"two-apply-forms", []BatchOperation{{Kind: BatchMount, Spec: Spec{ID: "bad", Apply: probe.Apply, ApplyEffect: func(*Handle) (Disposer, error) {
				forbiddenCallbacks.Add(1)
				return nil, nil
			}}}}},
			{"unknown-kind", []BatchOperation{{Kind: BatchKind(255)}}},
			{"dispose-then-update", []BatchOperation{{Kind: BatchDispose, Fiber: baseline}, {Kind: BatchUpdate, Fiber: baseline}}},
		} {
			t.Run(invalid.name, func(t *testing.T) {
				ops := append([]BatchOperation{{Kind: BatchMount, Spec: probe}}, invalid.ops...)
				ids, err := batcher.Batch(ctx, ops)
				if err == nil {
					t.Fatalf("invalid suffix accepted: ids=%v operations=%+v", ids, ops)
				}
				last := ops[len(ops)-1]
				if (last.Kind == BatchUpdate || last.Kind == BatchDispose) && (last.Fiber == 0 || last.Fiber == 999) && !errors.Is(err, ErrUnknown) {
					t.Fatalf("unknown user/root target error = %v, want ErrUnknown", err)
				}
				batchUnchanged(t, ctx, r, before, events)
				if forbiddenCallbacks.Load() != 0 || baselineApplies.Load() != 1 {
					t.Fatal("validation invoked a callback or restarted the baseline")
				}
			})
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if ids, err := batcher.Batch(canceled, []BatchOperation{{Kind: BatchMount, Spec: probe}, {Kind: BatchUpdate, Fiber: baseline}}); !errors.Is(err, context.Canceled) {
			t.Fatalf("precommit cancellation: ids=%v error=%v", ids, err)
		}
		batchUnchanged(t, ctx, r, before, events)
		if forbiddenCallbacks.Load() != 0 {
			t.Fatal("precommit cancellation invoked a callback")
		}
		if id := mountFixture(t, ctx, r, Spec{ID: "after-rejections"}); id != 2 {
			t.Fatalf("rejected prefix consumed a real mount ID: next=%d, want 2", id)
		}
		batchSettle(t, ctx, r)

		// Keep BOTH mounts cold until Batch has returned and caller-owned arrays
		// have been overwritten. A real host provision then wakes the copied graph.
		needs, provides, consumerNeeds := []string{"seed"}, []string{"service"}, []string{"service"}
		var consumerApplies, checks atomic.Int64
		var seen atomic.Int64
		ops := []BatchOperation{
			{Kind: BatchMount, Spec: Spec{ID: "consumer", Needs: consumerNeeds, Check: func(c *Handle) bool {
				checks.Add(1)
				if _, err := r.Snapshots(ctx); err != nil { // Must run off the native owner.
					return false
				}
				value, err := c.Read("service")
				return err == nil && value == int64(17)
			}, Apply: func(c *Handle) error {
				consumerApplies.Add(1)
				value, err := c.Read("service")
				if err != nil {
					return err
				}
				seen.Store(value.(int64))
				return nil
			}}},
			{Kind: BatchUpdate, Fiber: baseline},
			{Kind: BatchMount, Spec: Spec{ID: "provider", Needs: needs, Provides: provides, Apply: func(c *Handle) error {
				value, err := c.Read("seed")
				if err != nil {
					return err
				}
				return c.Provide("service", value.(int64)+10)
			}}},
		}
		ids, err := batcher.Batch(ctx, ops)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(ids, []uint64{3, 0, 4}) {
			t.Fatalf("actual Mount-index results = %v, want [3 0 4]", ids)
		}
		needs[0], provides[0], consumerNeeds[0] = "changed-seed", "changed-service", "changed-dependency"
		for i := range ops {
			ops[i] = BatchOperation{Kind: BatchKind(255), Spec: Spec{ID: "caller-overwrite"}}
		}
		cold := batchInspect(t, ctx, r)
		if c, p := batchFiber(t, cold, ids[0]), batchFiber(t, cold, ids[2]); c.Name != "consumer" || c.State != Pending || p.Name != "provider" || p.State != Pending || consumerApplies.Load() != 0 || checks.Load() != 0 {
			t.Fatalf("cold prefix started callbacks or retained caller operation storage: consumer=%+v provider=%+v", c, p)
		}
		if err := root.Provide("seed", int64(7)); err != nil {
			t.Fatal(err)
		}
		batchSettle(t, ctx, r)
		inspection := batchInspect(t, ctx, r)
		consumer, provider, actual := batchFiber(t, inspection, ids[0]), batchFiber(t, inspection, ids[2]), batchProvision(t, inspection, "service")
		if consumer.State != Active || provider.State != Active || consumerApplies.Load() != 1 || seen.Load() != 17 || checks.Load() == 0 || baselineApplies.Load() != 2 {
			t.Fatalf("owned copied declarations did not activate once: consumer=%+v provider=%+v applies=%d seen=%d checks=%d baseline=%d", consumer, provider, consumerApplies.Load(), seen.Load(), checks.Load(), baselineApplies.Load())
		}
		if actual.Owner != ids[2] || !actual.Active || actual.Value != int64(17) || !slices.Equal(consumer.Epoch, []uint64{actual.UID}) || consumer.Target == nil || !slices.Equal(consumer.Target.UIDs, consumer.Epoch) {
			t.Fatalf("batch result IDs/epoch do not identify actual native provisions: consumer=%+v provision=%+v", consumer, actual)
		}
		if notifier, ok := r.(Notifier); !ok {
			t.Fatal("actual core lacks native Notify")
		} else if err := notifier.Notify(ctx, "seed", "service"); err != nil {
			t.Fatal(err)
		}
		batchSettle(t, ctx, r)
		if consumerApplies.Load() != 1 {
			t.Fatal("unchanged native notification restarted copied batch mounts")
		}
	})
}

func TestSharedBatchDoubleUpdateCommitsLatestRevisionAndOneReplacement(t *testing.T) {
	batchCandidates(t, func(t *testing.T, makeRuntime func() Runtime) {
		ctx, r, batcher := batchRuntime(t, makeRuntime)
		release, open := batchGate(t)
		cleanupEntered, oldContext := make(chan struct{}), make(chan *Handle, 1)
		var providerApplies, consumerApplies atomic.Int64
		var mu sync.Mutex
		var seen []int64
		provider := mountFixture(t, ctx, r, Spec{ID: "provider", Provides: []string{"service"}, Apply: func(c *Handle) error {
			generation := providerApplies.Add(1)
			if err := c.Provide("service", generation); err != nil {
				return err
			}
			if generation != 1 {
				return nil
			}
			oldContext <- c
			return c.Defer("held-provider", func(context.Context) error { close(cleanupEntered); <-release; return nil })
		}})
		consumer := mountFixture(t, ctx, r, Spec{ID: "consumer", Needs: []string{"service"}, Check: func(c *Handle) bool {
			_, err := r.Snapshots(ctx)
			_, readErr := c.Read("service")
			return err == nil && readErr == nil
		}, Apply: func(c *Handle) error {
			consumerApplies.Add(1)
			value, err := c.Read("service")
			if err != nil {
				return err
			}
			mu.Lock()
			seen = append(seen, value.(int64))
			mu.Unlock()
			return nil
		}})
		batchSettle(t, ctx, r)
		old := batchReceive(t, ctx, oldContext)
		before := batchInspect(t, ctx, r)
		initial, oldProvision := batchFiber(t, before, provider), batchProvision(t, before, "service")
		if providerApplies.Load() != 1 || consumerApplies.Load() != 1 || !slices.Equal(batchFiber(t, before, consumer).Epoch, []uint64{oldProvision.UID}) {
			t.Fatal("double-update fixture did not settle its real initial binding")
		}
		ids, err := batcher.Batch(ctx, []BatchOperation{{Kind: BatchUpdate, Fiber: provider}, {Kind: BatchUpdate, Fiber: provider}})
		if err != nil || !slices.Equal(ids, []uint64{0, 0}) {
			t.Fatalf("double-update prefix: ids=%v error=%v", ids, err)
		}
		batchQuiesced(t, ctx, old)
		batchReceive(t, ctx, cleanupEntered)
		held := batchInspect(t, ctx, r)
		p, c, reservation := batchFiber(t, held, provider), batchFiber(t, held, consumer), batchProvision(t, held, "service")
		if p.Revision != initial.Revision+2 || p.State != Unloading || p.Target == nil || p.Target.Revision != initial.Revision+2 || !slices.Contains(p.Effects, "held-provider") || c.State != Pending || c.Target != nil {
			t.Fatalf("ack lost latest revision or exposed intermediate replacement: provider=%+v consumer=%+v", p, c)
		}
		if reservation.UID != oldProvision.UID || reservation.Active || reservation.Value != int64(1) || providerApplies.Load() != 1 || consumerApplies.Load() != 1 {
			t.Fatalf("old native reservation/body was not retained until real cleanup: provision=%+v applies=%d/%d", reservation, providerApplies.Load(), consumerApplies.Load())
		}
		open()
		batchSettle(t, ctx, r)
		after := batchInspect(t, ctx, r)
		p, c, reservation = batchFiber(t, after, provider), batchFiber(t, after, consumer), batchProvision(t, after, "service")
		mu.Lock()
		values := slices.Clone(seen)
		mu.Unlock()
		if p.State != Active || p.Revision != initial.Revision+2 || p.Target == nil || p.Target.Revision != p.Revision || c.State != Active || providerApplies.Load() != 2 || consumerApplies.Load() != 2 || !slices.Equal(values, []int64{1, 2}) {
			t.Fatalf("double update ran an intermediate activation: provider=%+v consumer=%+v applies=%d/%d values=%v", p, c, providerApplies.Load(), consumerApplies.Load(), values)
		}
		if reservation.Owner != provider || !reservation.Active || reservation.UID == oldProvision.UID || reservation.Value != int64(2) || !slices.Equal(c.Epoch, []uint64{reservation.UID}) || c.Target == nil || !slices.Equal(c.Target.UIDs, c.Epoch) {
			t.Fatalf("replacement did not retain the actual new provision UID: consumer=%+v provision=%+v", c, reservation)
		}
	})
}

func TestSharedBatchColdConsumerNeverChecksOrAppliesAgainstOldProvider(t *testing.T) {
	batchCandidates(t, func(t *testing.T, makeRuntime func() Runtime) {
		ctx, r, batcher := batchRuntime(t, makeRuntime)
		release, open := batchGate(t)
		cleanupEntered, oldContext := make(chan struct{}), make(chan *Handle, 1)
		var nextValue, providerApplies, consumerApplies atomic.Int64
		nextValue.Store(1)
		provider := mountFixture(t, ctx, r, Spec{ID: "provider", Provides: []string{"service"}, Apply: func(c *Handle) error {
			value := nextValue.Load() // A real immutable value, not a pointer changed by the caller.
			generation := providerApplies.Add(1)
			if err := c.Provide("service", value); err != nil {
				return err
			}
			if generation != 1 {
				return nil
			}
			oldContext <- c
			return c.Defer("held-old-provider", func(context.Context) error { close(cleanupEntered); <-release; return nil })
		}})
		batchSettle(t, ctx, r)
		old := batchReceive(t, ctx, oldContext)
		before := batchInspect(t, ctx, r)
		oldProvision, initial := batchProvision(t, before, "service"), batchFiber(t, before, provider)
		type observation struct {
			value     any
			err       error
			snapshots []Snapshot
		}
		var mu sync.Mutex
		var checked []observation
		var applied []int64
		consumerSpec := Spec{ID: "cold-consumer", Needs: []string{"service"}, Check: func(c *Handle) bool {
			value, readErr := c.Read("service")
			// Reentrant full native snapshots detect callbacks incorrectly executed
			// on the state owner, without trying to join lifecycle work from Check.
			snapshots, err := r.Snapshots(ctx)
			mu.Lock()
			checked = append(checked, observation{value, errors.Join(readErr, err), snapshots})
			mu.Unlock()
			return readErr == nil && err == nil
		}, Apply: func(c *Handle) error {
			consumerApplies.Add(1)
			value, err := c.Read("service")
			if err != nil {
				return err
			}
			mu.Lock()
			applied = append(applied, value.(int64))
			mu.Unlock()
			return nil
		}}
		nextValue.Store(2)
		ids, err := batcher.Batch(ctx, []BatchOperation{{Kind: BatchMount, Spec: consumerSpec}, {Kind: BatchUpdate, Fiber: provider}})
		if err != nil || !slices.Equal(ids, []uint64{2, 0}) {
			t.Fatalf("cold-consumer-before-update prefix: ids=%v error=%v", ids, err)
		}
		batchQuiesced(t, ctx, old)
		batchReceive(t, ctx, cleanupEntered)
		held := batchInspect(t, ctx, r)
		consumer, retained := batchFiber(t, held, ids[0]), batchProvision(t, held, "service")
		mu.Lock()
		checksWhileHeld := len(checked)
		mu.Unlock()
		if consumer.State != Pending || consumer.Target != nil || len(consumer.Epoch) != 0 || consumerApplies.Load() != 0 || checksWhileHeld != 0 || retained.UID != oldProvision.UID || retained.Active || retained.Value != int64(1) {
			t.Fatalf("cold consumer observed precommit provider: consumer=%+v retained=%+v applies=%d checks=%d", consumer, retained, consumerApplies.Load(), checksWhileHeld)
		}
		open()
		batchSettle(t, ctx, r)
		after := batchInspect(t, ctx, r)
		consumer, replacement := batchFiber(t, after, ids[0]), batchProvision(t, after, "service")
		mu.Lock()
		checks, values := slices.Clone(checked), slices.Clone(applied)
		mu.Unlock()
		if providerApplies.Load() != 2 || consumerApplies.Load() != 1 || !slices.Equal(values, []int64{2}) || len(checks) == 0 || consumer.State != Active || replacement.UID == oldProvision.UID || !replacement.Active || !slices.Equal(consumer.Epoch, []uint64{replacement.UID}) || consumer.Target == nil || !slices.Equal(consumer.Target.UIDs, consumer.Epoch) {
			t.Fatalf("cold consumer did not bind only the actual final UID: consumer=%+v provision=%+v applies=%d/%d values=%v checks=%+v", consumer, replacement, providerApplies.Load(), consumerApplies.Load(), values, checks)
		}
		for _, check := range checks {
			if check.err != nil || check.value != int64(2) {
				t.Fatalf("Check ran against an old committed binding or under the owner: %+v", check)
			}
			observed := batchFiber(t, Inspection{Fibers: check.snapshots}, provider)
			if observed.Revision != initial.Revision+1 {
				t.Fatalf("Check saw a partially committed batch prefix: %+v", check.snapshots)
			}
		}
	})
}

func TestSharedBatchInflightApplyAndRepeatedDispose(t *testing.T) {
	batchCandidates(t, func(t *testing.T, makeRuntime func() Runtime) {
		t.Run("inflight-apply", func(t *testing.T) {
			ctx, r, batcher := batchRuntime(t, makeRuntime)
			release, open := batchGate(t)
			started := make(chan *Handle, 1)
			var attempts, bodies, peak, cleanups atomic.Int64
			var oldReturned, overlap atomic.Bool
			id := mountFixture(t, ctx, r, Spec{ID: "inflight", Apply: func(c *Handle) error {
				n := bodies.Add(1)
				defer bodies.Add(-1)
				for current := peak.Load(); n > current && !peak.CompareAndSwap(current, n); current = peak.Load() {
				}
				attempt := attempts.Add(1)
				if attempt != 1 {
					if !oldReturned.Load() {
						overlap.Store(true)
					}
					return nil
				}
				if err := c.Defer("old-body-effect", func(context.Context) error { cleanups.Add(1); return nil }); err != nil {
					return err
				}
				started <- c
				<-release
				oldReturned.Store(true)
				return nil
			}})
			old := batchReceive(t, ctx, started)
			before := batchFiber(t, batchInspect(t, ctx, r), id)
			ids, err := batcher.Batch(ctx, []BatchOperation{{Kind: BatchUpdate, Fiber: id}, {Kind: BatchUpdate, Fiber: id}})
			if err != nil || !slices.Equal(ids, []uint64{0, 0}) {
				t.Fatalf("inflight double-update prefix: ids=%v error=%v", ids, err)
			}
			batchQuiesced(t, ctx, old)
			held := batchFiber(t, batchInspect(t, ctx, r), id)
			if held.Revision != before.Revision+2 || held.Target == nil || held.Target.Revision != held.Revision || (held.State != Loading && held.State != Unloading) || !slices.Contains(held.Effects, "old-body-effect") || attempts.Load() != 1 || bodies.Load() != 1 || cleanups.Load() != 0 {
				t.Fatalf("batch discarded or overlapped a genuinely in-flight old body: %+v attempts=%d bodies=%d cleanups=%d", held, attempts.Load(), bodies.Load(), cleanups.Load())
			}
			open()
			batchSettle(t, ctx, r)
			after := batchFiber(t, batchInspect(t, ctx, r), id)
			if after.State != Active || after.Revision != before.Revision+2 || after.Target == nil || after.Target.Revision != after.Revision || attempts.Load() != 2 || bodies.Load() != 0 || peak.Load() != 1 || overlap.Load() || cleanups.Load() != 1 {
				t.Fatalf("inflight batch did not run latest-only after actual old-body return: %+v attempts=%d bodies=%d peak=%d overlap=%t cleanups=%d", after, attempts.Load(), bodies.Load(), peak.Load(), overlap.Load(), cleanups.Load())
			}
		})
		t.Run("repeated-dispose", func(t *testing.T) {
			ctx, r, batcher := batchRuntime(t, makeRuntime)
			release, open := batchGate(t)
			cleanupEntered, activated := make(chan struct{}), make(chan *Handle, 1)
			var applies, cleanups, forbidden atomic.Int64
			id := mountFixture(t, ctx, r, Spec{ID: "dispose-once", Apply: func(c *Handle) error {
				applies.Add(1)
				activated <- c
				return c.Defer("held-once", func(context.Context) error {
					cleanups.Add(1)
					close(cleanupEntered)
					<-release
					return nil
				})
			}})
			batchSettle(t, ctx, r)
			old := batchReceive(t, ctx, activated)
			ids, err := batcher.Batch(ctx, []BatchOperation{
				{Kind: BatchDispose, Fiber: id},
				{Kind: BatchDispose, Fiber: id},
				{Kind: BatchMount, Spec: Spec{ID: "still-cold", Needs: []string{"missing"}, Apply: func(*Handle) error { forbidden.Add(1); return nil }}},
			})
			if err != nil || !slices.Equal(ids, []uint64{0, 0, 2}) {
				t.Fatalf("live repeated Dispose was not idempotent inside one batch: ids=%v error=%v", ids, err)
			}
			batchQuiesced(t, ctx, old)
			batchReceive(t, ctx, cleanupEntered)
			held := batchFiber(t, batchInspect(t, ctx, r), id)
			if held.Alive || held.Target != nil || held.State != Unloading || !slices.Contains(held.Effects, "held-once") || applies.Load() != 1 || cleanups.Load() != 1 || forbidden.Load() != 0 {
				t.Fatalf("Dispose acknowledgment pretended cleanup had finished: %+v applies=%d cleanups=%d", held, applies.Load(), cleanups.Load())
			}
			open()
			batchSettle(t, ctx, r)
			after := batchInspect(t, ctx, r)
			disposed, cold := batchFiber(t, after, id), batchFiber(t, after, ids[2])
			if disposed.State != Disposed || disposed.Alive || len(disposed.Effects) != 0 || disposed.Target != nil || cold.State != Pending || applies.Load() != 1 || cleanups.Load() != 1 || forbidden.Load() != 0 {
				t.Fatalf("repeated Dispose ran work twice or lost the actual cold mount: disposed=%+v cold=%+v applies=%d cleanups=%d", disposed, cold, applies.Load(), cleanups.Load())
			}
		})
	})
}
