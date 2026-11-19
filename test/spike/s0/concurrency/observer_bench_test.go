// SPDX-License-Identifier: Apache-2.0

package concurrency

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// BenchmarkEventEmission measures what one emitted event costs the kernel with
// 0, 1 and 8 keeping-up observers attached: a Recorder.Record call, which appends
// one ledger event and one dispatcher envelope on the state owner. Observers are
// drained (untimed) every 256 events so the figure is the emission path, not a
// backlog. drain-ns/event is the off-owner delivery time per event, reported
// separately. Development aid on a shared host, not acceptance evidence.
func BenchmarkEventEmission(b *testing.B) {
	const batch = 256
	for _, candidate := range nativeObserverConstructors {
		for _, observerCount := range []int{0, 1, 8} {
			b.Run(fmt.Sprintf("%s/observers=%d", candidate.name, observerCount), func(b *testing.B) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
				r := candidate.make()
				defer func() { _ = closeRuntime(context.Background(), r) }()
				root, err := r.(RootProvider).RootContext(ctx)
				if err != nil {
					b.Fatal(err)
				}
				observers := r.(ObserverRuntime)
				for i := range observerCount {
					if _, err := observers.ObserveEvents(root, fmt.Sprintf("bench %d", i), func(context.Context, Event) error { return nil }); err != nil {
						b.Fatal(err)
					}
				}
				recorder := r.(Recorder)
				data := map[string]any{"key": "bench"}
				var drain time.Duration
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := recorder.Record(ctx, "bench", data); err != nil {
						b.Fatal(err)
					}
					if (i+1)%batch == 0 || i == b.N-1 {
						b.StopTimer()
						start := time.Now()
						if err := observers.AwaitObservers(ctx); err != nil {
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
}
