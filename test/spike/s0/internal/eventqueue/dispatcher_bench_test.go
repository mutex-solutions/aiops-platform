// SPDX-License-Identifier: Apache-2.0

package eventqueue

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type benchBinding struct{}

func (benchBinding) Enter() bool                                     { return true }
func (benchBinding) Leave()                                          {}
func (benchBinding) Defer(string, func(context.Context) error) error { return nil }

// BenchmarkAppend is the producer-side cost of one emitted event (what the
// state owner pays under its ledger lock/turn) with 0, 1 and 8 observers that
// keep up. Observers are drained untimed every 256 events; drain-ns/event is the
// worker-side delivery time per event. Development aid, not acceptance evidence.
func BenchmarkAppend(b *testing.B) {
	const batch = 256
	for _, observers := range []int{0, 1, 8} {
		b.Run(fmt.Sprintf("observers=%d", observers), func(b *testing.B) {
			d := New[[16]uint64](nil)
			defer func() { _ = d.Close(context.Background()) }()
			for i := range observers {
				if _, err := d.AddObserver(benchBinding{}, fmt.Sprintf("bench %d", i), func(context.Context, [16]uint64) error { return nil }); err != nil {
					b.Fatal(err)
				}
			}
			ctx := context.Background()
			var drain time.Duration
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := d.Append(uint64(i+1), [16]uint64{}); err != nil {
					b.Fatal(err)
				}
				if (i+1)%batch == 0 || i == b.N-1 {
					b.StopTimer()
					start := time.Now()
					if err := d.Barrier(ctx); err != nil {
						b.Fatal(err)
					}
					drain += time.Since(start)
					b.StartTimer()
				}
			}
			b.ReportMetric(float64(drain.Nanoseconds())/float64(b.N), "drain-ns/event")
		})
	}
}
