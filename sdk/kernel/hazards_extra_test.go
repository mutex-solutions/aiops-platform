// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

// Findings outside the original review list. See hazards_test.go for conventions.

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// TestHazardX1ErrorMethodPanicsOnSnapshotCaller: an Apply that returns an error
// whose Error method panics. The reconciler renders error text on the worker
// under a recover (errorText); a core that formatted it on the caller's goroutine would let a plugin panic a
// monitoring caller.
func TestHazardX1ErrorMethodPanicsOnSnapshotCaller(t *testing.T) {
	for core, c := range hazardCores {
		t.Run(c.name, func(t *testing.T) {
			var panicked any
			var state State
			var locked bool
			synctest.Test(t, func(t *testing.T) {
				r := hazardCores[core].make(t)
				id, err := r.Mount(context.Background(), Spec{ID: "bad-error", Apply: func(*Handle) error { return panicValue{} }})
				if err != nil {
					t.Fatal(err)
				}
				synctest.Wait()
				func() {
					defer func() { panicked = recover() }()
					snaps, err := r.Snapshots(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					for _, s := range snaps {
						if s.ID == id {
							state = s.State
						}
					}
				}()
				// A panic on the caller must not leave the runtime locked.
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_, err = r.Mount(ctx, Spec{ID: "later"})
				locked = err != nil
			})
			observed := fmt.Sprintf("Snapshots panicked=%v state=%s; later Mount blocked=%v", panicked, state, locked)
			t.Log(observed)
			if locked {
				t.Fatalf("runtime left locked: %s", observed)
			}
			knownHazard(t, panicked != nil, "a panicking Error method panics the monitoring caller of Snapshots/Inspect", observed)
		})
	}
}

type panicValue struct{}

func (panicValue) String() string { panic("String called by the kernel") }
func (panicValue) Error() string  { panic("Error called by the kernel") }

// ---------------------------------------------------------------------------
// G3: wake-all per event.

// wakeCtx counts how often a core looks at its context. The counters are
// a wake-up count that needs no change to the core.
type wakeCtx struct {
	context.Context
	done, err atomic.Int64
}

func (c *wakeCtx) Done() <-chan struct{} { c.done.Add(1); return c.Context.Done() }
func (c *wakeCtx) Err() error            { c.err.Add(1); return c.Context.Err() }

type g3Obs struct {
	doneCalls int64 // Done calls by all waiters, including the one initial iteration each
	errCalls  int64
}

func g3Count(t *testing.T, core int, kind string, waiters, events int) g3Obs {
	var obs g3Obs
	synctest.Test(t, func(t *testing.T) {
		r := hazardCores[core].make(t)
		gate := make(chan struct{})
		var once sync.Once
		t.Cleanup(func() { once.Do(func() { close(gate) }) })
		id, err := r.Mount(context.Background(), Spec{ID: "slow", Apply: func(*Handle) error { <-gate; return nil }})
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		parent, cancel := context.WithCancel(context.Background())
		counters := make([]*wakeCtx, waiters)
		var wg sync.WaitGroup
		for i := range counters {
			counters[i] = &wakeCtx{Context: parent}
			wg.Add(1)
			go func() {
				defer wg.Done()
				if kind == "AwaitFiber" {
					_ = r.(Waiter).AwaitFiber(counters[i], id)
				} else {
					_ = r.Settle(counters[i])
				}
			}()
		}
		synctest.Wait() // every waiter is parked
		recorder := r.(Recorder)
		for i := 0; i < events; i++ {
			if err := recorder.Record(context.Background(), "tick", map[string]any{"i": i}); err != nil {
				t.Fatal(err)
			}
			synctest.Wait() // every woken waiter has looked and parked again
		}
		for _, c := range counters {
			obs.doneCalls += c.done.Load()
			obs.errCalls += c.err.Load()
		}
		cancel()
		wg.Wait()
		once.Do(func() { close(gate) })
	})
	return obs
}
