// SPDX-License-Identifier: Apache-2.0

package concurrency

// Findings outside the original review list. See hazards_test.go for conventions.

import (
	"context"
	"fmt"
	"testing"
	"testing/synctest"
	"time"
)

// TestHazardX1ErrorMethodPanicsOnSnapshotCaller: an Apply that returns an error
// whose Error method panics. The reconciler renders error text on the worker
// under a recover (rcErrorText); the graph-lock keeps the error value and calls
// Error() in Snapshots (graphFormatDiagnostics), off the graph lock but on the
// CALLER's goroutine, so a plugin can make a monitoring caller panic.
func TestHazardX1ErrorMethodPanicsOnSnapshotCaller(t *testing.T) {
	for core, c := range hazardCores {
		t.Run(c.name, func(t *testing.T) {
			var panicked any
			var state State
			var locked bool
			synctest.Test(t, func(t *testing.T) {
				r := hazardCores[core].make(t)
				id, err := r.Mount(context.Background(), Spec{ID: "bad-error", Apply: func(*Context) error { return panicValue{} }})
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
			knownHazard(t, panicked != nil, "graph-lock calls a plugin error's Error() inside Snapshots/Inspect on the caller's goroutine, so a panicking Error method panics the monitoring caller", observed)
		})
	}
}
