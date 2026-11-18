// SPDX-License-Identifier: Apache-2.0

package composition_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	co "github.com/mutex-solutions/aiops-platform/test/spike/s0/composition"
	cc "github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
	cm "github.com/mutex-solutions/aiops-platform/test/spike/s0/contextmodel"
)

func TestBatchStartValidatesEveryEntryBeforeNativeCommit(t *testing.T) {
	both(t, func(t *testing.T, ctx context.Context, backend cc.Runtime) {
		entry := cm.NewEntry("actual module")
		r := root(t, backend, cm.RootSpec{Entries: []cm.Plugin{entry}})
		before, err := backend.(cc.Inspector).Inspect(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var called atomic.Int64
		valid := co.MountRequest{Parent: r.Context(), Entry: entry,
			Spec: co.Spec{Metadata: cm.Spec{ID: "first"}, Apply: func(*co.Context) (cc.Disposer, error) {
				called.Add(1)
				return nil, nil
			}}}
		invalid := co.MountRequest{Parent: r.Context(), Entry: cm.NewEntry(entry.Name()),
			Spec: co.Spec{Metadata: cm.Spec{ID: "spoof"}}}
		if fibers, err := r.BatchStart(ctx, []co.MountRequest{valid, invalid}); !errors.Is(err, cm.ErrEntry) || len(fibers) != 0 {
			t.Fatalf("invalid final entry admitted a prefix: %v %+v", err, fibers)
		}
		after, err := backend.(cc.Inspector).Inspect(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if after.Sequence != before.Sequence || len(after.Fibers) != 0 || called.Load() != 0 {
			t.Fatal("metadata validation changed the actual native graph")
		}
		fibers, err := r.BatchStart(ctx, []co.MountRequest{valid})
		if err != nil || len(fibers) != 1 || fibers[0].ID != 1 {
			t.Fatalf("actual mount ID changed after rejected prefix: %v %+v", err, fibers)
		}
		await(t, ctx, fibers[0])
		if called.Load() != 1 {
			t.Fatal("accepted body did not run exactly once")
		}
	})
}

func TestBatchStartReturnsActualTypedBindings(t *testing.T) {
	both(t, func(t *testing.T, ctx context.Context, backend cc.Runtime) {
		entry := cm.NewEntry("typed batch module")
		key := cm.NewKey[string]("typed batch value", 1)
		r := root(t, backend, cm.RootSpec{Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{key.Any()}})
		seen := make(chan string, 1)
		fibers, err := r.BatchStart(ctx, []co.MountRequest{
			{Parent: r.Context(), Entry: entry, Spec: co.Spec{Metadata: cm.Spec{ID: "consumer", Inject: []cm.Dep{cm.Need(key)}},
				Apply: func(c *co.Context) (cc.Disposer, error) {
					value, err := co.Resolve(c, key)
					if err == nil {
						seen <- value
					}
					return nil, err
				}}},
			{Parent: r.Context(), Entry: entry, Spec: co.Spec{Metadata: cm.Spec{ID: "provider", Provides: []cm.AnyKey{key.Any()}},
				Apply: func(c *co.Context) (cc.Disposer, error) { return nil, co.Provide(c, key, "actual", nil) }}},
		})
		if err != nil || len(fibers) != 2 || fibers[0].ID != 1 || fibers[1].ID != 2 {
			t.Fatalf("native Batch IDs unavailable: %v %+v", err, fibers)
		}
		for _, fiber := range fibers {
			await(t, ctx, fiber)
		}
		select {
		case value := <-seen:
			if value != "actual" {
				t.Fatal(value)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		inspection, err := backend.(cc.Inspector).Inspect(ctx)
		if err != nil || len(inspection.Provisions) != 1 || len(inspection.Fibers) != 2 {
			t.Fatalf("actual graph incomplete: %v %+v", err, inspection)
		}
		for _, fiber := range inspection.Fibers {
			if fiber.ID == fibers[0].ID && (fiber.State != cc.Active || len(fiber.Epoch) != 1 || fiber.Epoch[0] != inspection.Provisions[0].UID) {
				t.Fatalf("consumer did not capture actual native provision UID: %+v", fiber)
			}
		}
	})
}
