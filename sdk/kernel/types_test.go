// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestDesiredAndCommittedEpochAreDistinctData(t *testing.T) {
	retained := []uint64{7}
	desired := []uint64{9}
	s := Snapshot{ID: 1, State: Unloading, Revision: 3, Epoch: retained,
		Alive: true, Target: &EpochSnapshot{Revision: 3, UIDs: desired}}
	if s.Target.Revision != 3 || s.Epoch[0] == s.Target.UIDs[0] {
		t.Fatal("fixture did not distinguish retained bindings from desired target")
	}
	s.Alive, s.Target = false, nil
	if s.State != Unloading || s.Epoch[0] != 7 {
		t.Fatal("disposal request erased retained activation before cleanup")
	}
}

func TestLeaseRefusesQuiescentActivation(t *testing.T) {
	c := newHandle(nil, context.Background(), 0, nil)
	if c.Enter() {
		t.Fatal("zero activation accepted a lease")
	}
	c.live.Store(true)
	if !c.Enter() || c.lease.Load() != 1 {
		t.Fatal("live activation did not acquire one lease")
	}
	c.live.Store(false)
	if c.Enter() {
		t.Fatal("quiescent activation accepted a new lease")
	}
	c.Leave()
	if got := c.lease.Load(); got != 0 {
		t.Fatalf("outstanding lease count = %d, want 0", got)
	}
}

func TestCallPathLeaseParallel(t *testing.T) {
	c := newHandle(nil, context.Background(), 0, nil)
	c.live.Store(true)
	var group sync.WaitGroup
	for range 32 {
		group.Go(func() {
			for range 1000 {
				if !c.Enter() {
					t.Error("active fixture unexpectedly refused lease")
					return
				}
				c.Leave()
			}
		})
	}
	group.Wait()
	if got := c.lease.Load(); got != 0 {
		t.Fatalf("outstanding leases = %d", got)
	}
	if got := testing.AllocsPerRun(1000, func() {
		if !c.Enter() {
			panic("fixture unexpectedly inactive")
		}
		c.Leave()
	}); got != 0 {
		t.Fatalf("Enter/Leave allocations = %g", got)
	}
}

func TestLeaseReleaseNotifiesOptionalScheduler(t *testing.T) {
	c := newHandle(nil, context.Background(), 0, nil)
	var notified int
	c.leaseChanged = func() { notified++ }
	c.live.Store(true)
	if !c.Enter() {
		t.Fatal("active context refused lease")
	}
	c.Leave()
	// The wake serves a drainer only: a live activation's Leave never calls it.
	if notified != 0 || c.lease.Load() != 0 {
		t.Fatalf("live release wake=%d leases=%d", notified, c.lease.Load())
	}
	if !c.Enter() {
		t.Fatal("active context refused lease")
	}
	c.live.Store(false)
	c.Leave()
	if notified != 1 || c.lease.Load() != 0 {
		t.Fatalf("draining release wake=%d leases=%d", notified, c.lease.Load())
	}
	if c.Enter() || notified != 1 {
		t.Fatal("initially inactive lease changed the ledger")
	}
}

func TestActivationOperationsAndRefusal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var provided, deferred, requested bool
	c := newHandle(nil, ctx, 0, map[svcKey]provision{"sample": {uid: 7, owner: 3, value: "value"}})
	c.provideFn = func(key svcKey, _ string, value any) error {
		provided = key == "sample" && value == "new"
		return nil
	}
	c.effectFn = func(label string, d Disposer) error {
		deferred = label == "cleanup" && d != nil
		return nil
	}
	c.disposeFn = func() error { requested = true; return nil }
	c.live.Store(true)
	if c.Std() != ctx {
		t.Fatal("Std did not return activation context")
	}
	if got, err := c.Read("sample"); err != nil || got != "value" {
		t.Fatalf("Read = %v, %v", got, err)
	}
	if _, err := c.Read("undeclared"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("undeclared Read error = %v", err)
	}
	if err := c.Provide("sample", "new"); err != nil || !provided {
		t.Fatalf("Provide failed: %v", err)
	}
	if err := c.Defer("cleanup", func(context.Context) error { return nil }); err != nil || !deferred {
		t.Fatalf("Defer failed: %v", err)
	}
	if err := c.Dispose(); err != nil || !requested {
		t.Fatalf("self-dispose request failed: %v", err)
	}
	c.live.Store(false)
	if got, err := c.Read("sample"); err != nil || got != "value" {
		t.Fatalf("committed teardown read = %v, %v", got, err)
	}
	c.released.Store(true)
	if _, err := c.Read("sample"); !errors.Is(err, ErrInactive) {
		t.Fatalf("late Read = %v", err)
	}
	if err := c.Provide("sample", "late"); !errors.Is(err, ErrInactive) {
		t.Fatalf("late Provide = %v", err)
	}
	if err := c.Defer("late", nil); !errors.Is(err, ErrInactive) {
		t.Fatalf("late Defer = %v", err)
	}
	cancel()
	if !errors.Is(c.Std().Err(), context.Canceled) {
		t.Fatal("activation context cancellation not observable")
	}
}
