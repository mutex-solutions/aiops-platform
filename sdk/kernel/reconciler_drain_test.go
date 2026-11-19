// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// A fiber's drain budget measures its own draining. Waiting for dependants and
// structural children is bounded by those fibers' own deadlines and is reported
// by naming the fiber that is actually stuck.

func deadlineFibers(events []Event) map[uint64]string {
	out := map[uint64]string{}
	for _, e := range events {
		out[e.Fiber] = keyText(e.Key)
	}
	return out
}

func TestReconcilerDrainWaitingOnHungDependantNamesIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := hazardReconciler(t)
		hang := make(chan struct{})
		var once sync.Once
		t.Cleanup(func() { once.Do(func() { close(hang) }) })
		provider := rcMount(t, r, Spec{ID: "provider", Provides: []string{"k"}, Apply: func(c *Handle) error { return c.Provide("k", 1) }})
		dependant := rcMount(t, r, Spec{ID: "dependant", Needs: []string{"k"}, Apply: func(c *Handle) error {
			return c.Go("hung", func(context.Context) error { <-hang; return nil })
		}})
		rcSettle(t, r)
		if err := r.Dispose(context.Background(), provider); err != nil {
			t.Fatal(err)
		}
		// Far beyond the provider's 5s: the provider has no work of its own, so it
		// has no drain clock; only the dependant's tracked worker overruns.
		time.Sleep(testDrain + time.Second)
		synctest.Wait()
		fibers := deadlineFibers(eventsOf(t, r, "deadline", ""))
		if len(fibers) != 1 || fibers[dependant] != "drain" {
			t.Fatalf("deadline events by fiber = %v, want only the dependant's drain", fibers)
		}
		if s := rcSnapshot(t, r, provider); strings.Contains(s.Error, "deadline") || s.State != Unloading {
			t.Fatalf("provider claims its own deadline: state=%s error=%q", s.State, s.Error)
		}
		err := r.Settle(rcGuard(t))
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Settle = %v, want the dependant's deadline", err)
		}
		text := err.Error()
		for _, want := range []string{"fiber 2 drain deadline", "fiber 1 drain: waiting for fiber 2"} {
			if !strings.Contains(text, want) {
				t.Errorf("Settle diagnostic lacks %q:\n%s", want, text)
			}
		}
		if strings.Contains(text, "fiber 1 drain deadline") {
			t.Errorf("provider reported a deadline of its own:\n%s", text)
		}
	})
}

// Own work still counts: a provider whose own lease outlives the budget is
// reported at its own drain stage, even with dependants waiting on it.
func TestReconcilerDrainOwnLeaseOverrunStillReported(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := hazardReconciler(t)
		var held *Handle
		provider := rcMount(t, r, Spec{ID: "provider", Provides: []string{"k"}, Apply: func(c *Handle) error { held = c; return c.Provide("k", 1) }})
		rcMount(t, r, Spec{ID: "dependant", Needs: []string{"k"}})
		rcSettle(t, r)
		if !held.Enter() {
			t.Fatal("no lease")
		}
		if err := r.Dispose(context.Background(), provider); err != nil {
			t.Fatal(err)
		}
		time.Sleep(testDrain + time.Second)
		synctest.Wait()
		fibers := deadlineFibers(eventsOf(t, r, "deadline", ""))
		if len(fibers) != 1 || fibers[provider] != "drain" {
			t.Fatalf("deadline events by fiber = %v, want the provider's own drain", fibers)
		}
		held.Leave() // later release lets everything finish
		synctest.Wait()
		if err := r.Settle(rcGuard(t)); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	})
}

// A parent's join of its children has no clock; a hung child is named.
func TestReconcilerDrainParentJoinNamesHungChild(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := hazardReconciler(t)
		hang := make(chan struct{})
		var once sync.Once
		t.Cleanup(func() { once.Do(func() { close(hang) }) })
		var child uint64
		parent := rcMount(t, r, Spec{ID: "parent", Apply: func(c *Handle) error {
			id, err := c.MountChild(Spec{ID: "child", Apply: func(cc *Handle) error {
				return cc.Go("hung", func(context.Context) error { <-hang; return nil })
			}})
			child = id
			return err
		}})
		rcSettle(t, r)
		if err := r.Dispose(context.Background(), parent); err != nil {
			t.Fatal(err)
		}
		time.Sleep(testDrain + time.Second)
		synctest.Wait()
		fibers := deadlineFibers(eventsOf(t, r, "deadline", ""))
		if len(fibers) != 1 || fibers[child] != "drain" {
			t.Fatalf("deadline events by fiber = %v, want only the child's drain", fibers)
		}
		err := r.Settle(rcGuard(t))
		if err == nil || !strings.Contains(err.Error(), "fiber 1 drain: waiting for fiber 2") || strings.Contains(err.Error(), "fiber 1 drain deadline") {
			t.Fatalf("Settle = %v, want the parent to name its hung child", err)
		}
	})
}
