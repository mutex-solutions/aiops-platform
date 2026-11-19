// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"testing"
	"time"
)

// Small-fixture semantic proof of the two-phase scope construction: the declared
// 97 + trees*width fibers and nothing else, Scope.With on the provider after its
// Provide, consumers mounted as roots under the derived context, a complete
// dependency closure on a shared restart, and realm isolation. No timing.
func TestTreeScopeConstruction(t *testing.T) {
	for _, width := range []int{2, 4} {
		f, err := TreeFixture(6, width)
		if err != nil {
			t.Fatal(err)
		}
		for _, backend := range Candidates() {
			t.Run(backend.Name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				result := RunT(ctx, backend, f)
				if len(result.Errors) != 0 || result.Classification != Inconclusive {
					t.Fatalf("width %d: %v", width, result.Errors)
				}
				// Every incarnation is archived: 97 shared + ten cycles of every tree.
				if got, want := len(result.FinalFibers), SharedFibers+result.Cycles*6*width; got != want {
					t.Fatalf("archived fibers %d, want %d (no container fiber)", got, want)
				}
				// One shared batch, then per cycle and tree: provider batch + consumer batch.
				if want := 1 + result.Cycles*6*2; result.MountBatches != want {
					t.Fatalf("mount batches = %d, want %d", result.MountBatches, want)
				}
				cold := 0
				for _, s := range result.Samples {
					if s.Operation == "cold-tree-mount-settle" {
						cold++
					}
				}
				if cold != MinimumRepetitions*6 {
					t.Fatalf("cold samples %d", cold)
				}
			})
		}
	}
}

// Realm isolation and restart propagation, directly on one runtime: tree values
// differ, a consumer reads its own provider only, and a shared update restarts
// every shared and local fiber while a tree provider restart stays local.
func TestTreeIsolationAndClosure(t *testing.T) {
	f, err := TreeFixture(3, 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range Candidates() {
		t.Run(backend.Name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
			if err := r.Mount(ctx, 0, f.Shared); err != nil {
				t.Fatal(err)
			}
			if err := r.Runtime.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			for tree := 0; tree < f.TreeCount; tree++ {
				start := f.Shared + tree*f.TreeWidth
				if err := r.Mount(ctx, start, start+f.TreeWidth); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.Runtime.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			res := &RunResult{}
			if err := inspectActive(ctx, r, res, 0, len(f.Nodes)); err != nil {
				t.Fatal(err)
			}
			if err := r.HotLookup(); err != nil {
				t.Fatal(err)
			}
			seen := map[int]bool{}
			for _, n := range f.Nodes {
				if n.Tree >= 0 && len(n.Provides) != 0 {
					seen[r.providers[n.Provides[0]]] = true
				}
			}
			if len(seen) != f.TreeCount {
				t.Fatalf("tree provider values not distinct: %v", seen)
			}
			before := r.Resources.Loads.Load()
			if err := r.BatchUpdate(ctx, []int{0}); err != nil {
				t.Fatal(err)
			}
			if err := r.Runtime.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			if got := r.Resources.Loads.Load() - before; got != int64(len(f.Nodes)) {
				t.Fatalf("shared restart reloaded %d, closure is %d", got, len(f.Nodes))
			}
			if err := inspectActive(ctx, r, res, 0, len(f.Nodes)); err != nil {
				t.Fatal(err)
			}
			if err := r.HotLookup(); err != nil {
				t.Fatal(err)
			}
			before = r.Resources.Loads.Load()
			if err := r.BatchUpdate(ctx, []int{f.Shared + f.TreeWidth}); err != nil {
				t.Fatal(err)
			}
			if err := r.Runtime.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			if got := r.Resources.Loads.Load() - before; got != int64(f.TreeWidth) {
				t.Fatalf("one tree provider restart reloaded %d, want its tree of %d", got, f.TreeWidth)
			}
			if err := inspectActive(ctx, r, res, 0, len(f.Nodes)); err != nil {
				t.Fatal(err)
			}
		})
	}
}
