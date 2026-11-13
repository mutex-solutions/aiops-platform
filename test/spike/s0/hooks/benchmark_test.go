// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"context"
	"testing"

	"github.com/mutex-solutions/aiops-platform/test/spike/s0/contextmodel"
)

// These are local measurements with actual candidate leases, not approved S0
// acceptance workloads or promises about final SDK/kernel cost. Setup is outside
// the timed loop and registration always uses real candidate Apply/Defer.
func BenchmarkEvent(b *testing.B) {
	for name, factory := range candidates {
		b.Run(name, func(b *testing.B) {
			for _, mode := range []string{"emit", "parallel", "serial", "bail"} {
				b.Run(mode, func(b *testing.B) {
					f := newFixture(b, factory)
					e, use := makeEvent(b, f)
					f.mount(f.metadata("a", "a", ""), []Use{use}, func(binding Binding) error {
						_, err := e.On(binding, "outer", func(context.Context, int) (Decision, error) { return Decision{}, nil })
						return err
					})
					dispatch := func() error { return e.Emit(f.ctx, f.owner, contextmodel.Audience{}, 1) }
					switch mode {
					case "parallel":
						dispatch = func() error { return e.Parallel(f.ctx, f.owner, contextmodel.Audience{}, 1) }
					case "serial":
						dispatch = func() error { _, err := e.Serial(f.ctx, f.owner, contextmodel.Audience{}, 1); return err }
					case "bail":
						dispatch = func() error { _, err := e.Bail(f.ctx, f.owner, contextmodel.Audience{}, 1); return err }
					}
					b.ReportAllocs()
					b.ResetTimer()
					for range b.N {
						if err := dispatch(); err != nil {
							b.Fatal(err)
						}
					}
					b.StopTimer()
				})
			}
		})
	}
}

func BenchmarkWaterfall(b *testing.B) {
	for name, factory := range candidates {
		b.Run(name, func(b *testing.B) {
			f := newFixture(b, factory)
			w, use := makeWaterfall(b, f)
			f.mount(f.metadata("a", "a", ""), []Use{use}, func(binding Binding) error {
				_, err := w.On(binding, "outer", func(_ context.Context, _ int, next Next[int]) (Result[int], error) { return next() })
				return err
			})
			inner := func(context.Context, int) (int, error) { return 0, nil }
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := w.Run(f.ctx, f.owner, contextmodel.Audience{}, 1, inner); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
		})
	}
}
