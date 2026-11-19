// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"os"
	"testing"
	"time"
)

// Regression checks for two candidate defects found while preparing the scale
// run, both fixed in test/spike/s0/concurrency (see ADR-034). They are timing and
// heap based, so they stay opt-in (S0_SCALE_REPRO=1) and are development-host
// observations, not acceptance evidence.

func requireRepro(t *testing.T) {
	t.Helper()
	if os.Getenv("S0_SCALE_REPRO") != "1" {
		t.Skip("timing/heap-based candidate regression check; set S0_SCALE_REPRO=1 to run it")
	}
}

// settleOf mounts a realm fixture and returns the time Runtime.Settle takes.
func settleOf(t *testing.T, backend Backend, groups, total int) time.Duration {
	t.Helper()
	f, err := RealmFixture(groups, total/groups-1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	r, err := NewRealCandidateRuntime(backend, f)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = r.RequestDisposal(ctx, 0, len(f.Nodes))
		_ = r.Runtime.Settle(ctx)
		_ = r.Runtime.Close(ctx)
	}()
	if err := r.Mount(ctx, 0, len(f.Nodes)); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := r.Runtime.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	return time.Since(start)
}

// With the same 10,000 composed fibers, mounting them across 1,000 provider
// groups used to take several times longer than across 10 groups (graph-lock 4.2
// to 8 times, the reconciler up to its own 2 s check deadline): every held
// service event was rescanned for every pending load, once per composed
// pre-check (about pendingLoads x heldEvents per check). Both candidates now index
// held events and pending loads by key, so a check completion touches only the
// loads waiting on the keys it releases. Native string-key graphs have no
// pre-check and were never affected.
func TestReproHeldServiceScanGrowsWithProviders(t *testing.T) {
	requireRepro(t)
	for _, backend := range Candidates() {
		t.Run(backend.Name, func(t *testing.T) {
			best := func(groups int) time.Duration {
				d := settleOf(t, backend, groups, 10_000)
				if d2 := settleOf(t, backend, groups, 10_000); d2 < d {
					d = d2
				}
				return d
			}
			few, many := best(10), best(1000)
			t.Logf("%s: settle 10 groups %v, 1000 groups %v (ratio %.1f)", backend.Name, few, many, float64(many)/float64(few))
			if many > 3*few {
				t.Errorf("mount cost grows with the number of providers at a fixed fiber count: %v -> %v", few, many)
			}
		})
	}
}

// churnRetained mounts and disposes every tree of f, cycles times, on one real
// candidate root and returns the heap retained over the shared-only baseline
// after each cycle's mount-all (with the same GC checkpoint the harness uses). It
// keeps no per-sample trace, so what it measures is the candidate and the
// fixture, not the harness's own growing result.
func churnRetained(t *testing.T, backend Backend, f Fixture, cycles int) []int64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	r, err := NewRealCandidateRuntime(backend, f)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = r.RequestDisposal(ctx, 0, len(f.Nodes))
		_ = r.Runtime.Settle(ctx)
		_ = r.Runtime.Close(ctx)
	}()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(r.Mount(ctx, 0, f.Shared))
	must(r.Runtime.Settle(ctx))
	base, err := r.Checkpoint(ctx, true)
	must(err)
	var deltas []int64
	for cycle := 0; cycle < cycles; cycle++ {
		for tree := 0; tree < f.TreeCount; tree++ {
			start := f.Shared + tree*f.TreeWidth
			must(r.Mount(ctx, start, start+f.TreeWidth))
			must(r.Runtime.Settle(ctx))
		}
		retained, err := r.Checkpoint(ctx, true)
		must(err)
		deltas = append(deltas, int64(retained.Memory.HeapAlloc)-int64(base.Memory.HeapAlloc))
		must(r.BatchUpdate(ctx, []int{0}))
		must(r.Runtime.Settle(ctx))
		var all []int
		for tree := f.TreeCount - 1; tree >= 0; tree-- {
			start := f.Shared + tree*f.TreeWidth
			all = append(all, nodeIndexes(start, start+f.TreeWidth)...)
		}
		must(r.BatchDisposal(ctx, all))
		must(r.Runtime.Settle(ctx))
	}
	return deltas
}

// Retained heap used to grow by about 2.3 MiB per churn cycle per 100 four-fiber
// trees at a constant live set, because every disposed incarnation left an
// archive record and its events in the runtime forever. The runtime now keeps a
// bounded archive and a bounded event tail (concurrency/retention.go), so the
// retained heap is flat once those fill. The first checkpoints still rise while
// the archive and the tail fill, so the comparison starts at the third cycle.
func TestReproRetainedHeapGrowsWithChurnHistory(t *testing.T) {
	requireRepro(t)
	f, err := TreeFixture(100, 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range Candidates() {
		t.Run(backend.Name, func(t *testing.T) {
			deltas := churnRetained(t, backend, f, 8)
			t.Logf("%s: retained over shared-only per cycle %v bytes", backend.Name, deltas)
			settled := deltas[2:]
			first, last := settled[0], settled[len(settled)-1]
			if limit := max(int64(1<<20), first/20); last-first > limit {
				t.Errorf("retained heap grew %d bytes over %d cycles with a constant live set (tolerance %d)", last-first, len(settled)-1, limit)
			}
		})
	}
}

// The sampled run of the same churn must stay inside the runtime's explicit
// bounds and report them: bounded archive and tail, no recorder overflow.
func TestReproChurnStaysInsideRetentionBounds(t *testing.T) {
	requireRepro(t)
	f, err := TreeFixture(100, 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range Candidates() {
		t.Run(backend.Name, func(t *testing.T) {
			result := RunCycles(context.Background(), backend, f, 6)
			if len(result.Errors) != 0 {
				t.Fatalf("run failed: %v", result.Errors)
			}
			status := result.Retention
			if status == nil || status.Archived > status.ArchiveLimit || status.Windowed > status.EventWindow || status.RecorderOverflow || status.ArchiveEvicted == 0 {
				t.Fatalf("history not bounded or not reported: %+v", status)
			}
		})
	}
}
