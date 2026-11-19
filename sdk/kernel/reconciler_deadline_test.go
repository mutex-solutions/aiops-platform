// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// A callback is judged by when it returned. These tests drive both directions of
// that rule with fake time: work that returned inside its budget is never failed
// however late the owner handles its message, and work that returned after the
// budget is failed even when its completion is already queued behind the timer.

func rcGatedSpec(stage string, gate chan struct{}) Spec {
	switch stage {
	case "check":
		return Spec{ID: "w", Check: func(*Handle) bool { <-gate; return true }}
	case "scope":
		return Spec{ID: "w", Apply: func(c *Handle) error {
			_, err := c.RequestEffectScope("scope", func(*EffectScope) error { <-gate; return nil })
			return err
		}}
	}
	return Spec{ID: "w", Apply: func(*Handle) error { <-gate; return nil }}
}

// rcDeadlineRun mounts one gated fiber, optionally stalls the owner, opens the
// gate at openAt after the mount and releases the owner at releaseAt (fake time
// since the mount, the owner stays stalled until then when stalled).
func rcDeadlineRun(t *testing.T, stage string, stalled bool, openAt, releaseAt time.Duration) (Snapshot, []ledgerEvent) {
	var snap Snapshot
	var events []ledgerEvent
	synctest.Test(t, func(t *testing.T) {
		r := hazardReconciler(t)
		gate := make(chan struct{})
		var once sync.Once
		open := func() { once.Do(func() { close(gate) }) }
		t.Cleanup(open)
		id := rcMount(t, r, rcGatedSpec(stage, gate))
		synctest.Wait()
		stall := newStallCtx(2)
		if stalled {
			go func() { _, _ = r.Snapshots(stall) }()
			synctest.Wait()
		}
		time.Sleep(openAt)
		open()
		synctest.Wait()
		time.Sleep(releaseAt - openAt)
		if stalled {
			close(stall.gate)
		}
		synctest.Wait()
		snap = rcSnapshot(t, r, id)
		events = eventsOf(t, r, "deadline", "")
	})
	return snap, events
}

func TestReconcilerDeadlineJudgesReturnInstantNotHandlingInstant(t *testing.T) {
	for _, stage := range []string{"load", "check", "scope"} {
		t.Run(stage, func(t *testing.T) {
			// Returned one nanosecond before the budget lapsed; the owner only
			// looks at the message a second after.
			snap, events := rcDeadlineRun(t, stage, true, testLoad-1, testLoad+time.Second)
			if snap.State != Active || snap.Error != "" || len(events) != 0 {
				t.Fatalf("in-time callback failed: state=%s error=%q deadline events=%v", snap.State, snap.Error, kinds(events))
			}
		})
		for _, stalled := range []bool{true, false} {
			name := stage + "/overrun-free-owner"
			if stalled {
				name = stage + "/overrun-late-completion-queued"
			}
			t.Run(name, func(t *testing.T) {
				// Returned one nanosecond after the budget: genuinely late, whether
				// the owner was free to fire the timer or stalled with the
				// completion already queued next to the due timer.
				snap, events := rcDeadlineRun(t, stage, stalled, testLoad+1, testLoad+time.Second)
				got := kinds(events)
				if len(got) != 1 || got[0] != "deadline/"+stage {
					t.Fatalf("deadline events = %v, want exactly deadline/%s (state=%s error=%q)", got, stage, snap.State, snap.Error)
				}
				if stage != "scope" && snap.State == Active {
					t.Fatalf("overrun %s left the fiber ACTIVE", stage)
				}
				if stage == "load" && (snap.State != Failed || !strings.Contains(snap.Error, "load deadline")) {
					t.Fatalf("overrun load: state=%s error=%q, want FAILED with the load deadline", snap.State, snap.Error)
				}
			})
		}
	}
}

// The dispose budget is one total deadline. An effect that returned in time but
// whose message was handled after the budget lapsed must not hide that the next
// effect starts, and runs, past the budget.
func TestReconcilerDeadlineDisposeBudgetStaysArmedAcrossEffects(t *testing.T) {
	run := func(t *testing.T, second bool) (events []ledgerEvent) {
		synctest.Test(t, func(t *testing.T) {
			r := hazardReconciler(t)
			gate := make(chan struct{})
			var once sync.Once
			open := func() { once.Do(func() { close(gate) }) }
			t.Cleanup(open)
			id := rcMount(t, r, Spec{ID: "w", Apply: func(c *Handle) error {
				if second {
					if err := c.Defer("later", func(context.Context) error { return nil }); err != nil {
						return err
					}
				}
				return c.Defer("blocked", func(context.Context) error { <-gate; return nil })
			}})
			rcSettle(t, r)
			if err := r.Dispose(context.Background(), id); err != nil {
				t.Fatal(err)
			}
			synctest.Wait() // "blocked" runs first (LIFO) and waits on the gate
			stall := newStallCtx(2)
			go func() { _, _ = r.Snapshots(stall) }()
			synctest.Wait()
			time.Sleep(time.Second)
			open() // returns at 1s, inside the 2s dispose budget
			synctest.Wait()
			time.Sleep(2 * time.Second) // the budget lapsed at 2s while the message waited
			close(stall.gate)
			synctest.Wait()
			events = eventsOf(t, r, "deadline", "")
		})
		return events
	}
	t.Run("one-effect-in-time", func(t *testing.T) {
		if events := run(t, false); len(events) != 0 {
			t.Fatalf("in-time disposer failed: %v", kinds(events))
		}
	})
	t.Run("next-effect-starts-after-budget", func(t *testing.T) {
		got := kinds(run(t, true))
		if len(got) != 1 || got[0] != "deadline/dispose" {
			t.Fatalf("deadline events = %v, want deadline/dispose for the effect that ran past the budget", got)
		}
	})
}

// Drain: tracked work that returned inside the drain budget is not blamed for the
// owner's delay; tracked work that returned after it is.
func TestReconcilerDeadlineDrainJudgesTrackedWorkByReturnInstant(t *testing.T) {
	run := func(t *testing.T, workEnds time.Duration) (Snapshot, []ledgerEvent) {
		var snap Snapshot
		var events []ledgerEvent
		synctest.Test(t, func(t *testing.T) {
			r := hazardReconciler(t)
			gate := make(chan struct{})
			var once sync.Once
			open := func() { once.Do(func() { close(gate) }) }
			t.Cleanup(open)
			id := rcMount(t, r, Spec{ID: "w", Apply: func(c *Handle) error {
				return c.Go("work", func(context.Context) error { <-gate; return nil })
			}})
			rcSettle(t, r)
			if err := r.Dispose(context.Background(), id); err != nil {
				t.Fatal(err)
			}
			synctest.Wait() // draining: the tracked worker holds the activation
			stall := newStallCtx(2)
			go func() { _, _ = r.Snapshots(stall) }()
			synctest.Wait()
			time.Sleep(workEnds)
			open()
			synctest.Wait()
			time.Sleep(testDrain + time.Second - workEnds)
			close(stall.gate)
			synctest.Wait()
			snap = rcSnapshot(t, r, id)
			events = eventsOf(t, r, "deadline", "")
		})
		return snap, events
	}
	t.Run("in-time", func(t *testing.T) {
		snap, events := run(t, testDrain-time.Second)
		if len(events) != 0 || snap.State != Disposed || snap.Error != "" {
			t.Fatalf("state=%s error=%q deadline events=%v", snap.State, snap.Error, kinds(events))
		}
	})
	t.Run("overrun", func(t *testing.T) {
		snap, events := run(t, testDrain+time.Millisecond)
		got := kinds(events)
		if len(got) != 1 || got[0] != "deadline/drain" {
			t.Fatalf("deadline events = %v (state=%s error=%q), want deadline/drain", got, snap.State, snap.Error)
		}
	})
}
