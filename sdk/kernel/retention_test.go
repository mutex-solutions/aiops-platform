// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// retentionCandidates builds a core with explicit bounds.
func retentionCandidates() map[string]func(func(*coreConfig)) Runtime {
	return map[string]func(func(*coreConfig)) Runtime{"reconciler": func(mod func(*coreConfig)) Runtime { return newReconcilerWith(mod) }}
}

func retentionStatusOf(t *testing.T, r Runtime) RetentionStatus {
	t.Helper()
	reporter, ok := r.(RetentionReporter)
	if !ok {
		t.Fatal("candidate does not report RetentionStatus")
	}
	return reporter.RetentionStatus()
}

func retentionCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestRetentionBoundsAreTheConfiguredCapacities(t *testing.T) {
	for name, build := range retentionCandidates() {
		t.Run(name, func(t *testing.T) {
			r := build(func(c *coreConfig) { c.archiveLimit, c.eventWindow, c.observerQueue = 7, 11, 13 })
			defer closeRuntime(retentionCtx(t), r)
			status := retentionStatusOf(t, r)
			if status.ArchiveLimit != 7 || status.EventWindow != 11 || status.ObserverBacklog != 13 {
				t.Fatalf("configured capacities not reported: %+v", status)
			}
		})
	}
}

// N+k disposed incarnations leave at most the documented number of records, the
// newest ones, and an expired id is distinguished from an unknown one.
func TestBoundedArchiveAndExpiredIDContract(t *testing.T) {
	const limit, extra = 8, 5
	for name, build := range retentionCandidates() {
		t.Run(name, func(t *testing.T) {
			ctx := retentionCtx(t)
			r := build(func(c *coreConfig) { c.archiveLimit = limit })
			defer closeRuntime(ctx, r)
			var ids []uint64
			for i := range limit + extra {
				id, err := r.Mount(ctx, Spec{ID: fmt.Sprintf("f%d", i)})
				if err != nil {
					t.Fatal(err)
				}
				if err := r.Dispose(ctx, id); err != nil {
					t.Fatal(err)
				}
				if err := r.Settle(ctx); err != nil {
					t.Fatal(err)
				}
				ids = append(ids, id)
			}
			for i := 1; i < len(ids); i++ {
				if ids[i] <= ids[i-1] {
					t.Fatalf("fiber ids are not strictly increasing: %v", ids)
				}
			}
			status := retentionStatusOf(t, r)
			if status.Archived != limit || status.ArchiveEvicted != extra {
				t.Fatalf("archive holds %d, evicted %d; want %d and %d", status.Archived, status.ArchiveEvicted, limit, extra)
			}
			// Snapshots and Inspect list exactly the newest records, all DISPOSED.
			snapshots, err := r.Snapshots(ctx)
			if err != nil {
				t.Fatal(err)
			}
			inspection, err := r.(Inspector).Inspect(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for what, list := range map[string][]Snapshot{"Snapshots": snapshots, "Inspect": inspection.Fibers} {
				if len(list) != limit {
					t.Fatalf("%s lists %d fibers, want %d", what, len(list), limit)
				}
				for i, s := range list {
					if s.ID != ids[extra+i] || s.State != Disposed || s.Alive {
						t.Fatalf("%s[%d] = %+v, want the disposed record of fiber %d", what, i, s, ids[extra+i])
					}
				}
			}
			waiter := r.(Waiter)
			// Recent ids behave as before.
			if err := waiter.AwaitFiber(ctx, ids[len(ids)-1]); err != nil {
				t.Fatalf("AwaitFiber on a recent disposed fiber = %v, want nil", err)
			}
			if err := r.Dispose(ctx, ids[len(ids)-1]); err != nil {
				t.Fatalf("Dispose on a recent disposed fiber = %v", err)
			}
			// Expired ids: the startup error is gone, so success is not claimed.
			if err := waiter.AwaitFiber(ctx, ids[0]); !errors.Is(err, ErrExpired) || errors.Is(err, ErrUnknown) {
				t.Fatalf("AwaitFiber on an expired id = %v, want ErrExpired", err)
			}
			if err := r.Dispose(ctx, ids[0]); err != nil {
				t.Fatalf("Dispose on an expired id = %v, want an idempotent nil", err)
			}
			// Never-issued ids stay unknown, including 0 (the root, never a user fiber).
			for _, id := range []uint64{0, ids[len(ids)-1] + 1, ids[len(ids)-1] + 100} {
				if err := waiter.AwaitFiber(ctx, id); !errors.Is(err, ErrUnknown) {
					t.Fatalf("AwaitFiber(%d) = %v, want ErrUnknown", id, err)
				}
			}
			// A disposed fiber that is gone from the live table is not revived.
			if _, err := r.Mount(ctx, Spec{ID: "next"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// The startup error of a recently disposed fiber stays joinable, and the same
// error is what an expired record replaces by ErrExpired.
func TestRetainedStartupErrorSurvivesDisposalUntilExpiry(t *testing.T) {
	for name, build := range retentionCandidates() {
		t.Run(name, func(t *testing.T) {
			ctx := retentionCtx(t)
			r := build(func(c *coreConfig) { c.archiveLimit = 2 })
			defer closeRuntime(ctx, r)
			boom := errors.New("startup refused")
			id, err := r.Mount(ctx, Spec{ID: "failing", Apply: func(*Context) error { return boom }})
			if err != nil {
				t.Fatal(err)
			}
			waiter := r.(Waiter)
			if err := waiter.AwaitFiber(ctx, id); !errors.Is(err, boom) {
				t.Fatalf("AwaitFiber before disposal = %v, want the startup error", err)
			}
			if err := r.Dispose(ctx, id); err != nil {
				t.Fatal(err)
			}
			_ = r.Settle(ctx) // The failed activation's diagnostic may be reported here.
			// A waiter that arrives right after disposal still gets the error.
			if err := waiter.AwaitFiber(ctx, id); !errors.Is(err, boom) {
				t.Fatalf("AwaitFiber right after disposal = %v, want the retained startup error", err)
			}
			for i := range 2 {
				other, err := r.Mount(ctx, Spec{ID: fmt.Sprintf("other%d", i)})
				if err != nil {
					t.Fatal(err)
				}
				if err := r.Dispose(ctx, other); err != nil {
					t.Fatal(err)
				}
				_ = r.Settle(ctx)
			}
			if err := waiter.AwaitFiber(ctx, id); !errors.Is(err, ErrExpired) || errors.Is(err, boom) {
				t.Fatalf("AwaitFiber after expiry = %v, want ErrExpired", err)
			}
		})
	}
}

// A waiter registered before disposal keeps the exact record alive: it gets the
// startup error even when the archive evicted the record before the waiter ran.
func TestInFlightWaiterSurvivesArchiveEviction(t *testing.T) {
	const fibers = 40
	for name, build := range retentionCandidates() {
		t.Run(name, func(t *testing.T) {
			ctx := retentionCtx(t)
			r := build(func(c *coreConfig) { c.archiveLimit = 1 })
			defer closeRuntime(ctx, r)
			boom := errors.New("startup refused")
			gate := newGate()
			ids := mountAll(t, ctx, r, fibers, func(i int) Spec {
				// The body fails at once; the disposer then blocks, so the fiber stays
				// UNLOADING and its waiter blocked until the whole group is released.
				return Spec{ID: fmt.Sprintf("w%d", i), Apply: func(c *Context) error {
					if err := c.Defer("hold", func(context.Context) error { <-gate; return nil }); err != nil {
						return err
					}
					return boom
				}}
			})
			// Disposal racing a load records no startup failure, so wait until every
			// failure is committed (UNLOADING behind its blocked disposer).
			for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(time.Millisecond) {
				snapshots, err := r.Snapshots(ctx)
				if err != nil {
					t.Fatal(err)
				}
				unloading := 0
				for _, s := range snapshots {
					if s.State == Unloading {
						unloading++
					}
				}
				if unloading == fibers {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("only %d of %d failed fibers reached UNLOADING", unloading, fibers)
				}
			}
			waiter := r.(Waiter)
			results := make(chan error, fibers)
			for _, id := range ids {
				go func() { results <- waiter.AwaitFiber(ctx, id) }()
			}
			for _, id := range ids {
				if err := r.Dispose(ctx, id); err != nil {
					t.Fatal(err)
				}
			}
			time.Sleep(20 * time.Millisecond) // Let every waiter register before the group is released.
			close(gate)
			for range fibers {
				if err := <-results; !errors.Is(err, boom) {
					t.Fatalf("in-flight waiter = %v, want the startup error even after eviction", err)
				}
			}
			if err := r.Settle(ctx); err != nil && !errors.Is(err, boom) {
				t.Fatal(err)
			}
			if status := retentionStatusOf(t, r); status.Archived != 1 || status.ArchiveEvicted != fibers-1 {
				t.Fatalf("archive %+v, want 1 held and %d evicted", status, fibers-1)
			}
		})
	}
}

func newGate() chan struct{} { return make(chan struct{}) }

func mountAll(t *testing.T, ctx context.Context, r Runtime, n int, spec func(int) Spec) []uint64 {
	t.Helper()
	ids := make([]uint64, n)
	for i := range n {
		id, err := r.Mount(ctx, spec(i))
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = id
	}
	return ids
}

// churn mounts and disposes n plain fibers one after the other.
func churn(t *testing.T, ctx context.Context, r Runtime, n int) {
	t.Helper()
	for i := range n {
		id, err := r.Mount(ctx, Spec{ID: fmt.Sprintf("c%d", i), Apply: func(c *Context) error { return c.Defer("x", func(context.Context) error { return nil }) }})
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Dispose(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := r.Settle(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEventTailIsBoundedAndTruncationIsReported(t *testing.T) {
	const window = 16
	for name, build := range retentionCandidates() {
		t.Run(name, func(t *testing.T) {
			ctx := retentionCtx(t)
			r := build(func(c *coreConfig) { c.eventWindow = window })
			defer closeRuntime(ctx, r)
			// Short history is complete and carries no error.
			id, err := r.Mount(ctx, Spec{ID: "first"})
			if err != nil {
				t.Fatal(err)
			}
			_ = r.Settle(ctx)
			events, err := r.Events(ctx)
			if err != nil || len(events) == 0 || events[0].Sequence != 1 {
				t.Fatalf("short history = %d events, %v; want complete and nil error", len(events), err)
			}
			_ = id
			churn(t, ctx, r, 20)
			events, err = r.Events(ctx)
			if !errors.Is(err, ErrEventsTruncated) {
				t.Fatalf("Events after overflowing the tail = %v, want ErrEventsTruncated", err)
			}
			status := retentionStatusOf(t, r)
			if len(events) != window || status.Windowed != window || status.EventsTotal <= window {
				t.Fatalf("tail has %d events (status %+v), want exactly %d of %d", len(events), status, window, status.EventsTotal)
			}
			for i, e := range events {
				if e.Sequence != status.EventsTotal-uint64(window)+uint64(i)+1 {
					t.Fatalf("tail is not the contiguous newest events: %d at %d, total %d", e.Sequence, i, status.EventsTotal)
				}
			}
		})
	}
}

// The observer queue still receives every event after the ledger window has
// dropped the oldest ones: the window bounds history, not delivery.
func TestObserversSeeEveryEventBeyondTheWindow(t *testing.T) {
	for name, build := range retentionCandidates() {
		t.Run(name, func(t *testing.T) {
			ctx := retentionCtx(t)
			r := build(func(c *coreConfig) { c.eventWindow = 8 })
			defer closeRuntime(ctx, r)
			churn(t, ctx, r, 30)
			if err := r.(ObserverRuntime).AwaitObservers(ctx); err != nil {
				t.Fatal(err)
			}
			status := retentionStatusOf(t, r)
			if accepted := r.(ObserverRuntime).ObserverStatus().Accepted; accepted != status.EventsTotal || status.EventsTotal <= 8 || status.Windowed != 8 {
				t.Fatalf("observer queue accepted %d of %d events, status %+v", accepted, status.EventsTotal, status)
			}
		})
	}
}
