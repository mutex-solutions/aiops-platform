// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// A command abandoned while it is still queued has no effect and reports the
// context error: the owner drops it at dequeue.
func TestReconcilerAdmissionCancelWhileQueuedHasNoEffect(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := hazardReconciler(t)
		target := rcMount(t, r, Spec{ID: "target"})
		synctest.Wait()
		stall := newStallCtx(2)
		go func() { _, _ = r.Snapshots(stall) }()
		synctest.Wait() // the owner is parked inside that command; everything below queues

		var applied atomic.Int32
		spec := Spec{ID: "never", Apply: func(*Context) error { applied.Add(1); return nil }}
		run := func(name string, call func(context.Context) error) {
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- call(ctx) }()
			synctest.Wait() // parked: the message sits in the inbox
			cancel()
			synctest.Wait()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("%s: err = %v, want context.Canceled", name, err)
				}
			default:
				t.Errorf("%s: still blocked after cancel while queued", name)
			}
		}
		run("mount", func(ctx context.Context) error { _, err := r.Mount(ctx, spec); return err })
		run("batch", func(ctx context.Context) error {
			_, err := r.(Batcher).Batch(ctx, []BatchOperation{{Kind: BatchMount, Spec: spec}})
			return err
		})
		run("update", func(ctx context.Context) error { return r.Update(ctx, target) })
		run("dispose", func(ctx context.Context) error { return r.Dispose(ctx, target) })
		close(stall.gate)
		synctest.Wait()

		snaps, err := r.Snapshots(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(snaps) != 1 || snaps[0].ID != target || snaps[0].Revision != 0 || !snaps[0].Alive || snaps[0].State != Active {
			t.Fatalf("an abandoned queued command took effect: %+v", snaps)
		}
		if applied.Load() != 0 {
			t.Fatalf("abandoned mount ran Apply %d times", applied.Load())
		}
	})
}

// The same race with real goroutines and real timers: whatever the interleaving,
// a call returns without error exactly when its command took effect.
func TestReconcilerAdmissionResultMatchesEffect(t *testing.T) {
	r := NewReconciler()
	t.Cleanup(func() { _ = closeRuntime(rcGuard(t), r) })
	const total, workers = 2000, 16
	var next atomic.Int64
	var mu sync.Mutex
	held := map[string]uint64{}
	failed := map[string]bool{}
	var updates atomic.Int64
	target := rcMount(t, r, Spec{ID: "target"})
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= total {
					return
				}
				// Spread cancellation from before the send to well after the reply.
				ctx, cancel := context.WithTimeout(context.Background(), time.Duration(i%7)*20*time.Microsecond)
				name := fmt.Sprintf("m%d", i)
				var id uint64
				var err error
				switch i % 3 {
				case 0:
					id, err = r.Mount(ctx, Spec{ID: name})
				case 1:
					var ids []uint64
					ids, err = r.(Batcher).Batch(ctx, []BatchOperation{{Kind: BatchMount, Spec: Spec{ID: name}}})
					if err == nil {
						id = ids[0]
					}
				default:
					if err = r.Update(ctx, target); err == nil {
						updates.Add(1)
					}
					cancel()
					continue
				}
				cancel()
				mu.Lock()
				if err == nil {
					held[name] = id
				} else {
					failed[name] = true
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	snaps, err := r.Snapshots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	present := map[string]uint64{}
	for _, s := range snaps {
		if s.ID == target {
			if got := int64(s.Revision); got != updates.Load() {
				t.Errorf("target revision %d, but %d Updates returned nil", got, updates.Load())
			}
			continue
		}
		present[s.Name] = s.ID
	}
	for name, id := range held {
		if present[name] != id {
			t.Errorf("%s returned id %d but is present as %d", name, id, present[name])
		}
	}
	for name := range failed {
		if id, ok := present[name]; ok {
			t.Errorf("%s returned an error but fiber %d exists", name, id)
		}
	}
	if len(held) == 0 || len(failed) == 0 {
		t.Logf("degenerate mix: %d succeeded, %d failed", len(held), len(failed))
	}
}
