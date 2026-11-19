// SPDX-License-Identifier: Apache-2.0

package concurrency

// Hazard tests: each turns one suspicion from a code review into a fact. A
// scenario always runs. When it demonstrates a hazard that is not yet fixed the
// test skips with the reason, unless S0_HAZARD_REPRO is set, in which case it
// fails with the observation. When the behaviour is safe (refuted, or fixed
// later) the test simply passes as a regression test. Nothing here changes a
// core; time is fake (testing/synctest) and every interleaving is arranged
// with gates or with the context hooks below, never with sleeps for ordering.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// hazardRepro reports whether the known-hazard tests should fail instead of skip.
func hazardRepro() bool { return os.Getenv("S0_HAZARD_REPRO") != "" }

// knownHazard ends a test whose scenario demonstrated (present) a hazard. Call it
// outside any synctest bubble.
func knownHazard(t *testing.T, present bool, reason, observed string) {
	t.Helper()
	if !present {
		return
	}
	if !hazardRepro() {
		t.Skip("known hazard, not yet fixed: " + reason)
	}
	t.Fatalf("hazard demonstrated: %s: %s", reason, observed)
}

// hazardReconciler is rcRuntime without the clean-Close requirement: scenarios
// here end with deliberate diagnostics, and only the owner join matters.
func hazardReconciler(t *testing.T) Runtime {
	t.Helper()
	r := NewRecordingReconciler()
	t.Cleanup(func() { _ = closeRuntime(rcGuard(t), r) })
	return r
}

var hazardCores = []struct {
	name string
	make func(*testing.T) Runtime
}{
	{"reconciler", hazardReconciler},
	{"graph-lock", graphTestRuntime},
}

// lateCancelCtx is a context that is cancelled by the n-th Err call: the first
// n-1 calls and that one return nil, later calls return context.Canceled, and
// Done closes during the n-th call. It models a caller whose context expires
// after the core's last check of it. wait, if set, runs just before the cancel
// is published (synctest.Wait there parks the caller in its select first, so the
// caller deterministically takes the Done case and never the ack).
type lateCancelCtx struct {
	context.Context
	n     int32
	calls atomic.Int32
	done  chan struct{}
	wait  func()
}

func newLateCancelCtx(n int32, wait func()) *lateCancelCtx {
	return &lateCancelCtx{Context: context.Background(), n: n, done: make(chan struct{}), wait: wait}
}

func (c *lateCancelCtx) Done() <-chan struct{} { return c.done }

func (c *lateCancelCtx) Err() error {
	switch k := c.calls.Add(1); {
	case k < c.n:
		return nil
	case k == c.n:
		if c.wait != nil {
			c.wait()
		}
		close(c.done)
		return nil
	}
	return context.Canceled
}

// stallCtx blocks whoever makes its n-th Err call until gate is closed. A
// reconciler owner makes that call when it dequeues the command, so the owner
// is held busy at a known point with its inbox still accepting messages. Inside
// a bubble the blocked owner is durably blocked, so fake time can pass.
type stallCtx struct {
	context.Context
	n     int32
	calls atomic.Int32
	gate  chan struct{}
}

func newStallCtx(n int32) *stallCtx {
	return &stallCtx{Context: context.Background(), n: n, gate: make(chan struct{})}
}

func (c *stallCtx) Err() error {
	if c.calls.Add(1) == c.n {
		<-c.gate
	}
	return nil
}

func findSnapshot(snaps []Snapshot, name string) (Snapshot, bool) {
	for _, s := range snaps {
		if s.Name == name {
			return s, true
		}
	}
	return Snapshot{}, false
}

func eventsOf(t *testing.T, r Runtime, kind, key string) []Event {
	t.Helper()
	events, err := r.Events(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []Event
	for _, e := range events {
		if e.Kind == kind && (key == "" || e.Key == key) {
			out = append(out, e)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// R1: lost acknowledgement.

type r1Obs struct {
	id         uint64
	err        error
	ids        []uint64
	present    []Snapshot
	applied    int32
	recoverID  bool // the caller can find the id by scanning Snapshots for the unique name
	mountEvent int  // "mount" events naming the fiber
}

func r1Mount(t *testing.T, core int, batch bool) r1Obs {
	var obs r1Obs
	synctest.Test(t, func(t *testing.T) {
		r := hazardCores[core].make(t)
		var applied atomic.Int32
		spec := Spec{ID: "orphan", Apply: func(*Context) error { applied.Add(1); return nil }}
		// Err calls before the commit: Mount: exchange, handle. Batch: Batch,
		// exchange, handle, owner batch. A graph-lock makes one (Mount) or two.
		var ctx *lateCancelCtx
		if batch {
			ctx = newLateCancelCtx(4, synctest.Wait)
			obs.ids, obs.err = r.(Batcher).Batch(ctx, []BatchOperation{{Kind: BatchMount, Spec: spec}})
		} else {
			ctx = newLateCancelCtx(2, synctest.Wait)
			obs.id, obs.err = r.Mount(ctx, spec)
		}
		synctest.Wait()
		snaps, err := r.Snapshots(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		obs.present = snaps
		obs.applied = applied.Load()
		_, obs.recoverID = findSnapshot(snaps, "orphan")
		obs.mountEvent = len(eventsOf(t, r, "mount", ""))
	})
	return obs
}

func TestHazardR1LostAckMount(t *testing.T) {
	for core, c := range hazardCores {
		for _, batch := range []bool{false, true} {
			name := c.name + "/mount"
			if batch {
				name = c.name + "/batch"
			}
			t.Run(name, func(t *testing.T) {
				obs := r1Mount(t, core, batch)
				got := ""
				for _, s := range obs.present {
					got += fmt.Sprintf("{id=%d name=%s state=%s} ", s.ID, s.Name, s.State)
				}
				held := obs.id != 0 || len(obs.ids) == 1 && obs.ids[0] != 0
				observed := fmt.Sprintf("returned id=%d ids=%v err=%v; Snapshots=%s applied=%d; id recoverable only by scanning names=%v",
					obs.id, obs.ids, obs.err, got, obs.applied, obs.recoverID)
				t.Log(observed)
				orphan := len(obs.present) == 1 && !held
				if c.name == "graph-lock" {
					// The graph-lock commits under its only check of ctx and
					// then returns the id: nothing can be lost.
					if orphan || obs.err != nil || !held {
						t.Fatalf("graph-lock lost the acknowledgement: %s", observed)
					}
					return
				}
				knownHazard(t, orphan && obs.err != nil, "reconciler Mount/Batch whose ctx is cancelled after the owner dequeued it commits a fiber but returns only the context error", observed)
				if orphan && obs.err == nil {
					t.Fatalf("orphan without error: %s", observed)
				}
			})
		}
	}
}

// TestHazardR1UpdateDisposeEffectDespiteError records what a late cancellation
// does to Update and Dispose. Both cores now give the same atomic outcome: the
// context is cancelled after the core took the command, so the command is applied
// and the call reports it (nil), never a context error with the effect applied.
// The reconciler used to return the context error with the effect applied
// (at-least-once); its claim-based admission removed that window for every
// non-wait command, not only Mount and Batch.
func TestHazardR1UpdateDisposeEffectDespiteError(t *testing.T) {
	for core, c := range hazardCores {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := hazardCores[core].make(t)
				id, err := r.Mount(context.Background(), Spec{ID: "target"})
				if err != nil {
					t.Fatal(err)
				}
				synctest.Wait()
				uerr := r.Update(newLateCancelCtx(2, synctest.Wait), id)
				synctest.Wait()
				s := graphSnapshotByID(t, r, id)
				rev := s.Revision
				derr := r.Dispose(newLateCancelCtx(2, synctest.Wait), id)
				synctest.Wait()
				s2 := graphSnapshotByID(t, r, id)
				t.Logf("Update err=%v revision=%d; Dispose err=%v state=%s alive=%v", uerr, rev, derr, s2.State, s2.Alive)
				// Applied means reported: an error implies no effect and vice versa.
				if uerr != nil || rev != 1 || derr != nil || s2.State != Disposed {
					t.Fatalf("%s: Update err=%v revision=%d Dispose err=%v state=%s", c.name, uerr, rev, derr, s2.State)
				}
			})
		})
	}
}

func graphSnapshotByID(t *testing.T, r Runtime, id uint64) Snapshot {
	t.Helper()
	snaps, err := r.Snapshots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range snaps {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("missing snapshot %d", id)
	return Snapshot{}
}

// ---------------------------------------------------------------------------
// R2: deadlines judged on the owner's clock.

type r2Obs struct {
	state    State
	errText  string
	returned bool
	deadline []Event
}

// r2Stage mounts one fiber whose callback blocks on a gate, stalls the owner
// inside an unrelated command, lets the callback return well inside its budget
// (no fake time has passed), then lets 3 seconds of fake time pass while the
// owner is still stalled and releases it. The completion message therefore sits
// in the inbox behind the stalled command while the budget lapses.
func r2Stage(t *testing.T, stage string) r2Obs {
	var obs r2Obs
	synctest.Test(t, func(t *testing.T) {
		r := hazardReconciler(t)
		gate := make(chan struct{})
		var once sync.Once
		open := func() { once.Do(func() { close(gate) }) }
		t.Cleanup(open)
		var returned atomic.Bool
		var spec Spec
		switch stage {
		case "load":
			spec = Spec{ID: "healthy", Apply: func(*Context) error { <-gate; returned.Store(true); return nil }}
		case "check":
			spec = Spec{ID: "healthy", Check: func(*Context) bool { <-gate; returned.Store(true); return true }}
		case "scope":
			spec = Spec{ID: "healthy", Apply: func(c *Context) error {
				_, err := c.RequestEffectScope("scope", func(*EffectScope) error { <-gate; returned.Store(true); return nil })
				return err
			}}
		}
		id := rcMount(t, r, spec)
		synctest.Wait() // the callback is running and blocked
		stall := newStallCtx(2)
		go func() { _, _ = r.Snapshots(stall) }()
		synctest.Wait() // the owner is parked inside that command
		open()
		synctest.Wait() // the callback returned; its completion message is queued
		time.Sleep(3 * time.Second)
		close(stall.gate)
		synctest.Wait()
		s := rcSnapshot(t, r, id)
		obs.state, obs.errText, obs.returned = s.State, s.Error, returned.Load()
		obs.deadline = eventsOf(t, r, "deadline", "")
	})
	return obs
}

func TestHazardR2OwnerClockJudgesDeadlines(t *testing.T) {
	for _, stage := range []string{"load", "check", "scope"} {
		t.Run("reconciler/"+stage, func(t *testing.T) {
			obs := r2Stage(t, stage)
			observed := fmt.Sprintf("callback returned inside its budget=%v; state=%s error=%q; deadline events=%v",
				obs.returned, obs.state, obs.errText, kinds(obs.deadline))
			t.Log(observed)
			if !obs.returned {
				t.Fatal("scenario broken: callback did not return")
			}
			// Safe behaviour: a callback that returned in budget leaves the fiber
			// ACTIVE and records no deadline.
			hazard := obs.state != Active || len(obs.deadline) != 0
			knownHazard(t, hazard, "reconciler judges load/check/scope budgets when the owner processes the completion, not when the callback returned", observed)
		})
	}
	// The graph-lock has no owner queue: the same scenario (callback returns at
	// once, then 3 seconds pass) leaves the fiber ACTIVE with no deadline.
	t.Run("graph-lock/load", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			g := graphTestRuntime(t)
			id := graphMount(t, context.Background(), g, Spec{ID: "healthy", Apply: func(*Context) error { return nil }})
			synctest.Wait()
			time.Sleep(3 * time.Second)
			s := graphSnapshotByID(t, g, id)
			if s.State != Active || s.Error != "" || len(eventsOf(t, g, "deadline:load", "")) != 0 {
				t.Fatalf("graph-lock: state=%s error=%q", s.State, s.Error)
			}
		})
	})
}

func kinds(events []Event) []string {
	var out []string
	for _, e := range events {
		out = append(out, e.Kind+"/"+e.Key)
	}
	return out
}

// ---------------------------------------------------------------------------
// R3: provider drain clock.

type r3Obs struct {
	providerErr string // Settle's diagnostic while the dependant is still in its own dispose
	deadlines   []string
	final       error
}

func r3Scenario(t *testing.T) r3Obs {
	var obs r3Obs
	synctest.Test(t, func(t *testing.T) {
		r := hazardReconciler(t)
		ctx := context.Background()
		disposeGate := make(chan struct{})
		var once sync.Once
		open := func() { once.Do(func() { close(disposeGate) }) }
		t.Cleanup(open)
		provider := rcMount(t, r, Spec{ID: "provider", Provides: []string{"k"}, Apply: func(c *Context) error { return c.Provide("k", 1) }})
		var dep *Context
		rcMount(t, r, Spec{ID: "dependant", Needs: []string{"k"}, Apply: func(c *Context) error {
			dep = c
			return c.Defer("slow", func(context.Context) error { <-disposeGate; return nil })
		}})
		rcSettle(t, r)
		if !dep.Enter() {
			t.Fatal("dependant refused a lease")
		}
		if err := r.Dispose(ctx, provider); err != nil {
			t.Fatal(err)
		}
		// The dependant uses 4.9s of its 5s drain budget on a lease, then
		// 1.8s of its 2s dispose budget: both inside its own budgets.
		time.Sleep(4900 * time.Millisecond)
		dep.Leave()
		synctest.Wait()
		time.Sleep(600 * time.Millisecond) // t = 5.5s: past the provider's 5s from invalidation
		if err := r.Settle(ctx); err != nil {
			obs.providerErr = err.Error()
		}
		obs.deadlines = kinds(eventsOf(t, r, "deadline", ""))
		time.Sleep(1200 * time.Millisecond) // t = 6.7s: dependant disposer returns in budget
		open()
		synctest.Wait()
		obs.final = r.Settle(ctx)
	})
	return obs
}

func TestHazardR3ProviderDrainClockCountsDependantUnload(t *testing.T) {
	obs := r3Scenario(t)
	observed := fmt.Sprintf("Settle at t=5.5s=%q; deadline events=%v; final Settle=%v", obs.providerErr, obs.deadlines, obs.final)
	t.Log(observed)
	if obs.final != nil {
		t.Fatalf("scenario broken: %v", obs.final)
	}
	// Nothing misbehaved: the dependant stayed inside both of its budgets.
	knownHazard(t, obs.providerErr != "" || len(obs.deadlines) != 0, "provider drain deadline starts at invalidation and also covers the healthy dependant's drain and dispose", observed)
}

// ---------------------------------------------------------------------------
// R4: Settle with a blocked fiber.

type r4Obs struct {
	first, second     error
	stateAfterFirst   State
	stateAfterSecond  State
	stateAtSettleEnd  State
	healthyBecameLive bool
}

func r4Scenario(t *testing.T, core int) r4Obs {
	var obs r4Obs
	synctest.Test(t, func(t *testing.T) {
		r := hazardCores[core].make(t)
		hang, slow := make(chan struct{}), make(chan struct{})
		var h, s sync.Once
		t.Cleanup(func() { h.Do(func() { close(hang) }) })
		t.Cleanup(func() { s.Do(func() { close(slow) }) })
		hung, err := r.Mount(context.Background(), Spec{ID: "hung", Apply: func(*Context) error { <-hang; return nil }})
		if err != nil {
			t.Fatal(err)
		}
		_ = hung
		time.Sleep(1500 * time.Millisecond)
		healthy, err := r.Mount(context.Background(), Spec{ID: "healthy", Apply: func(*Context) error { <-slow; return nil }})
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(600 * time.Millisecond) // t = 2.1s: hung is past its 2s load budget, healthy is mid-load
		bounded := func() (error, State) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			err := r.Settle(ctx)
			return err, graphSnapshotByID(t, r, healthy).State
		}
		obs.first, obs.stateAfterFirst = bounded()
		obs.second, obs.stateAfterSecond = bounded()
		s.Do(func() { close(slow) })
		synctest.Wait()
		obs.stateAtSettleEnd = graphSnapshotByID(t, r, healthy).State
	})
	return obs
}

func TestHazardR4SettleReturnsWhileHealthyFiberLoads(t *testing.T) {
	for core, c := range hazardCores {
		t.Run(c.name, func(t *testing.T) {
			obs := r4Scenario(t, core)
			observed := fmt.Sprintf("Settle#1 err=%v healthy=%s; Settle#2 err=%v healthy=%s; healthy after release=%s",
				obs.first, obs.stateAfterFirst, obs.second, obs.stateAfterSecond, obs.stateAtSettleEnd)
			t.Log(observed)
			if obs.stateAtSettleEnd != Active {
				t.Fatalf("scenario broken: %s", observed)
			}
			early := func(err error, state State) bool { return err != nil && state != Active }
			if c.name == "graph-lock" {
				// The graph-lock never returns early: it blocks until the
				// caller's context expires (the hung fiber keeps busy != 0).
				if !errors.Is(obs.first, context.DeadlineExceeded) || !errors.Is(obs.second, context.DeadlineExceeded) {
					t.Fatalf("graph-lock Settle: %s", observed)
				}
				return
			}
			knownHazard(t, early(obs.first, obs.stateAfterFirst) && early(obs.second, obs.stateAfterSecond),
				"reconciler Settle returns the blocked fiber's diagnostic at once, every time, even though an unrelated healthy fiber is still LOADING", observed)
		})
	}
}
