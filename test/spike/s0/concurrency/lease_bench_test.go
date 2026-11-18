// SPDX-License-Identifier: Apache-2.0

package concurrency

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
)

// leaseOwner mounts one activation on the candidate and returns its Context.
func leaseOwner(tb testing.TB, makeRuntime func() Runtime) (context.Context, Runtime, uint64, *Context) {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	r := makeRuntime()
	tb.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer closeCancel()
		_ = r.Close(closeCtx)
		cancel()
	})
	contexts := make(chan *Context, 1)
	id, err := r.Mount(ctx, Spec{ID: "lease-owner", Apply: func(c *Context) error { contexts <- c; return nil }})
	if err != nil {
		tb.Fatal(err)
	}
	if err := r.(Waiter).AwaitFiber(ctx, id); err != nil {
		tb.Fatal(err)
	}
	return ctx, r, id, <-contexts
}

// BenchmarkEnterLeave drives the complete call-path lease on ONE activation
// from 1, 8 and 32 goroutines. wall-ns/op is wall time per Enter+Leave across
// all callers; p50-ns/p99-ns are sampled (every 16th op) single Enter+Leave
// latencies including one timer read. Development aid, not acceptance.
func BenchmarkEnterLeave(b *testing.B) {
	for name, makeRuntime := range effectCandidates() {
		for _, callers := range []int{1, 8, 32} {
			b.Run(fmt.Sprintf("%s/callers=%d", name, callers), func(b *testing.B) {
				_, _, _, c := leaseOwner(b, makeRuntime)
				per := b.N/callers + 1
				samples := make([][]int64, callers)
				var start, done sync.WaitGroup
				start.Add(1)
				for g := range callers {
					done.Add(1)
					go func() {
						defer done.Done()
						mine := make([]int64, 0, per/16+1)
						start.Wait()
						for i := range per {
							if i%16 == 0 {
								t0 := time.Now()
								if !c.Enter() {
									panic("owner refused lease")
								}
								c.Leave()
								mine = append(mine, int64(time.Since(t0)))
								continue
							}
							if !c.Enter() {
								panic("owner refused lease")
							}
							c.Leave()
						}
						samples[g] = mine
					}()
				}
				b.ReportAllocs()
				b.ResetTimer()
				start.Done()
				done.Wait()
				b.StopTimer()
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(per*callers), "wall-ns/op")
				all := slices.Concat(samples...)
				slices.Sort(all)
				b.ReportMetric(float64(all[len(all)/2]), "p50-ns")
				b.ReportMetric(float64(all[len(all)*99/100]), "p99-ns")
			})
		}
	}
}
