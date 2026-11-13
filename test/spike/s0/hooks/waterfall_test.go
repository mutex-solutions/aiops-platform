// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/mutex-solutions/aiops-platform/test/spike/s0/contextmodel"
)

func makeWaterfall(t testing.TB, f *fixture) (*Waterfall[int, int], Use) {
	t.Helper()
	w, err := NewWaterfall[int, int](Definition{Name: "test/waterfall", Owner: f.key, Stages: []string{"outer", "inner"}})
	if err != nil {
		t.Fatal(err)
	}
	use, err := w.Use("outer")
	if err != nil {
		t.Fatal(err)
	}
	return w, use
}

func TestWaterfallCompositionVetoAndZeroResult(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, factory)
			w, use := makeWaterfall(t, f)
			var order []string
			for _, id := range []string{"b", "a"} {
				f.mount(f.metadata(id, id, ""), []Use{use}, func(b Binding) error {
					_, err := w.On(b, "outer", func(ctx context.Context, a int, next Next[int]) (Result[int], error) {
						order = append(order, id+" before")
						value, err := next()
						order = append(order, id+" after")
						value.Value++
						return value, err
					})
					return err
				})
			}
			value, err := w.Run(f.ctx, f.owner, contextmodel.Audience{}, 3, func(context.Context, int) (int, error) { order = append(order, "inner"); return 0, nil })
			if err != nil || !value.Present || value.Value != 2 {
				t.Fatalf("result = %+v, %v", value, err)
			}
			if !reflect.DeepEqual(order, []string{"a before", "b before", "inner", "b after", "a after"}) {
				t.Fatal(order)
			}

			veto, use := makeWaterfall(t, f)
			var inner atomic.Int64
			b, _ := f.mount(f.metadata("a", "a", ""), []Use{use}, nil)
			dispose, err := veto.On(b, "outer", func(context.Context, int, Next[int]) (Result[int], error) { return Result[int]{}, nil })
			if err != nil {
				t.Fatal(err)
			}
			value, err = veto.Run(f.ctx, f.owner, contextmodel.Audience{}, 0, func(context.Context, int) (int, error) { inner.Add(1); return 42, nil })
			if err != nil || value.Present || inner.Load() != 0 {
				t.Fatalf("no-next veto = %+v, %v, inner=%d", value, err, inner.Load())
			}
			if err := dispose(f.ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := veto.On(b, "outer", func(context.Context, int, Next[int]) (Result[int], error) {
				return Result[int]{Value: 0, Present: true}, nil
			}); err != nil {
				t.Fatal(err)
			}
			value, err = veto.Run(f.ctx, f.owner, contextmodel.Audience{}, 0, func(context.Context, int) (int, error) { inner.Add(1); return 42, nil })
			if err != nil || !value.Present || value.Value != 0 || inner.Load() != 0 {
				t.Fatalf("outer zero result = %+v, %v", value, err)
			}
		})
	}
}

func TestWaterfallRepeatedConcurrentAndLateNext(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, factory)
			w, use := makeWaterfall(t, f)
			var inner atomic.Int64
			b, _ := f.mount(f.metadata("a", "a", ""), []Use{use}, nil)
			dispose, err := w.On(b, "outer", func(_ context.Context, _ int, next Next[int]) (Result[int], error) {
				first, err := next()
				if err != nil {
					return first, err
				}
				_, err = next()
				return first, err
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = w.Run(f.ctx, f.owner, contextmodel.Audience{}, 0, func(context.Context, int) (int, error) { inner.Add(1); return 1, nil })
			if !errors.Is(err, ErrNext) || inner.Load() != 1 {
				t.Fatalf("duplicate = %v, inner=%d", err, inner.Load())
			}
			if err := dispose(f.ctx); err != nil {
				t.Fatal(err)
			}
			var saved Next[int]
			dispose, err = w.On(b, "outer", func(_ context.Context, _ int, next Next[int]) (Result[int], error) {
				saved = next
				return Result[int]{}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Run(f.ctx, f.owner, contextmodel.Audience{}, 0, func(context.Context, int) (int, error) { inner.Add(1); return 1, nil }); err != nil {
				t.Fatal(err)
			}
			if _, err := saved(); !errors.Is(err, ErrNextExpired) || inner.Load() != 1 {
				t.Fatalf("late next = %v, inner=%d", err, inner.Load())
			}
			if err := dispose(f.ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := w.On(b, "outer", func(_ context.Context, _ int, next Next[int]) (Result[int], error) {
				var pending sync.WaitGroup
				failures := make(chan error, 2)
				pending.Add(2)
				for range 2 {
					go func() { defer pending.Done(); _, err := next(); failures <- err }()
				}
				pending.Wait()
				return Result[int]{}, errors.Join(<-failures, <-failures)
			}); err != nil {
				t.Fatal(err)
			}
			_, err = w.Run(f.ctx, f.owner, contextmodel.Audience{}, 0, func(context.Context, int) (int, error) { inner.Add(1); return 1, nil })
			if !errors.Is(err, ErrNext) || inner.Load() != 2 {
				t.Fatalf("concurrent next = %v, inner=%d", err, inner.Load())
			}
		})
	}
}

func TestWaterfallListenerMayHandleRepeatedNext(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, factory)
			w, use := makeWaterfall(t, f)
			var inner atomic.Int64
			f.mount(f.metadata("a", "a", ""), []Use{use}, func(binding Binding) error {
				_, err := w.On(binding, "outer", func(_ context.Context, _ int, next Next[int]) (Result[int], error) {
					value, err := next()
					if err != nil {
						return value, err
					}
					if _, err := next(); !errors.Is(err, ErrNext) {
						return Result[int]{}, errors.New("duplicate next did not report error")
					}
					// The source throws inside next; its listener can catch the error.
					// Do not invent an uncatchable sticky dispatch failure here.
					return value, nil
				})
				return err
			})
			value, err := w.Run(f.ctx, f.owner, contextmodel.Audience{}, 0, func(context.Context, int) (int, error) { inner.Add(1); return 0, nil })
			if err != nil || !value.Present || value.Value != 0 || inner.Load() != 1 {
				t.Fatalf("handled duplicate = %+v err=%v inner=%d", value, err, inner.Load())
			}
		})
	}
}

func TestWaterfallAudiencePanicAndCancellation(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, factory)
			w, use := makeWaterfall(t, f)
			var a, b, unscoped atomic.Int64
			for _, item := range []struct {
				id, scope string
				count     *atomic.Int64
			}{{"a", "A", &a}, {"b", "B", &b}, {"c", "", &unscoped}} {
				f.mount(f.metadata(item.id, item.id, item.scope), []Use{use}, func(binding Binding) error {
					_, err := w.On(binding, "outer", func(_ context.Context, _ int, next Next[int]) (Result[int], error) { item.count.Add(1); return next() })
					return err
				})
			}
			audience, err := f.scope.Audience(f.owner.Metadata, func(value string) bool { return value == "A" })
			if err != nil {
				t.Fatal(err)
			}
			inner := func(context.Context, int) (int, error) { return 7, nil }
			if _, err := w.Run(f.ctx, f.owner, audience, 0, inner); err != nil {
				t.Fatal(err)
			}
			if _, err := w.Run(f.ctx, f.owner, contextmodel.Audience{}, 0, inner); err != nil {
				t.Fatal(err)
			}
			if a.Load() != 1 || b.Load() != 0 || unscoped.Load() != 1 {
				t.Fatalf("A=%d B=%d unscoped=%d", a.Load(), b.Load(), unscoped.Load())
			}
			ctx, cancel := context.WithCancel(f.ctx)
			cancel()
			if _, err := w.Run(ctx, f.owner, audience, 0, inner); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation = %v", err)
			}
			p, use := makeWaterfall(t, f)
			f.mount(f.metadata("a", "a", ""), []Use{use}, func(binding Binding) error {
				_, err := p.On(binding, "outer", func(context.Context, int, Next[int]) (Result[int], error) { panic("broken") })
				return err
			})
			_, err = p.Run(f.ctx, f.owner, contextmodel.Audience{}, 0, inner)
			var panicErr *PanicError
			if !errors.As(err, &panicErr) {
				t.Fatalf("panic = %v", err)
			}
		})
	}
}
