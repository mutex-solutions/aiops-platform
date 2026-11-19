// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"os"
	"testing"
	"time"
)

// Minimal reproductions of candidate behaviour found while preparing the scale
// run. The first is fixed in the candidates and now passes; the second still
// FAILS by design. Both are skipped unless S0_SCALE_REPRO=1, because they are
// timing/heap based. They are development-host observations, not acceptance
// evidence.

func requireRepro(t *testing.T) {
	t.Helper()
	if os.Getenv("S0_SCALE_REPRO") != "1" {
		t.Skip("timing/heap-based candidate check; set S0_SCALE_REPRO=1 to run it")
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

// Retained heap grows by several MiB per churn cycle at a constant tree count:
// every disposed incarnation leaves its archive and its events in the runtime,
// which never truncates them (the harness also keeps them on purpose). The
// section 8.2 limit (32 MiB over the shared-only baseline for 500 trees) is
// therefore exceeded from about the third cycle even though the live set is
// constant; the first checkpoint, before any history exists, is well inside it.
func TestReproRetainedHeapGrowsWithChurnHistory(t *testing.T) {
	requireRepro(t)
	f, err := TreeFixture(100, 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range Candidates() {
		t.Run(backend.Name, func(t *testing.T) {
			result := RunCycles(context.Background(), backend, f, 6)
			m := retainedMetrics(result)
			if len(result.Errors) != 0 || len(m.DeltaBytes) != 6 {
				t.Fatalf("run failed: %v", result.Errors)
			}
			first, last := m.DeltaBytes[0], m.DeltaBytes[len(m.DeltaBytes)-1]
			t.Logf("%s: retained over shared-only per cycle %v bytes (slope %.0f B/cycle)", backend.Name, m.DeltaBytes, m.SlopeBytesPerCP)
			if limit := max(int64(1<<20), first/20); last-first > limit {
				t.Errorf("retained heap grew %d bytes over five cycles with a constant live set (tolerance %d)", last-first, limit)
			}
		})
	}
}
