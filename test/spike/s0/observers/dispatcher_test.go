// SPDX-License-Identifier: Apache-2.0

package observers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cc "github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
)

var candidates = map[string]func() cc.Runtime{
	"graph-lock": cc.NewGraphLock,
	"reconciler": cc.NewReconciler,
}

// Only an adapter to actual native contexts, never a replacement/fake host.
type nativeBinding struct{ *cc.Context }

func (b nativeBinding) Defer(label string, dispose func(context.Context) error) error {
	return b.Context.Defer(label, cc.Disposer(dispose))
}

func actualRuntime(t *testing.T, factory func() cc.Runtime) (context.Context, cc.Runtime) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	r := factory()
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer closeCancel()
		defer cancel()
		if err := r.Close(closeCtx); err != nil {
			t.Errorf("native runtime close: %v", err)
		}
	})
	return ctx, r
}

func actualOwner(t *testing.T, ctx context.Context, r cc.Runtime, label string) (nativeBinding, uint64) {
	t.Helper()
	ready := make(chan *cc.Context, 1)
	id, err := r.Mount(ctx, cc.Spec{ID: label, Apply: func(c *cc.Context) error {
		ready <- c
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-ready:
		return nativeBinding{c}, id
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return nativeBinding{}, 0
	}
}

func testDispatcher[T any](t *testing.T, logger Logger) *Dispatcher[T] {
	t.Helper()
	d := New[T](logger)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := d.Close(ctx); err != nil {
			t.Errorf("dispatcher close: %v", err)
		}
	})
	return d
}

func awaitSignal(t *testing.T, ctx context.Context, signal <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-signal:
	case <-ctx.Done():
		t.Fatalf("%s: %v", label, ctx.Err())
	}
}

func releaseGate() (<-chan struct{}, func()) {
	gate := make(chan struct{})
	var once sync.Once
	return gate, func() { once.Do(func() { close(gate) }) }
}

func TestAppendRequiresPositiveConsecutiveSequence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	d := testDispatcher[int](t, nil)
	for _, sequence := range []uint64{0, 2, ^uint64(0)} {
		if err := d.Append(sequence, 0); !errors.Is(err, ErrSequenceGap) {
			t.Fatalf("initial sequence %d = %v", sequence, err)
		}
	}
	if got := d.Status(); got.Accepted != 0 || got.Delivered != 0 || got.Queued != 0 {
		t.Fatalf("rejection changed empty queue: %+v", got)
	}
	if err := d.WaitBarrier(ctx, 1); !errors.Is(err, ErrNotAccepted) {
		t.Fatalf("unaccepted barrier = %v", err)
	}
	if err := d.WaitBarrier(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if err := d.Append(1, 1); err != nil {
		t.Fatal(err)
	}
	for _, sequence := range []uint64{0, 1, 3, ^uint64(0)} {
		if err := d.Append(sequence, 0); !errors.Is(err, ErrSequenceGap) {
			t.Fatalf("sequence %d after 1 = %v", sequence, err)
		}
	}
	if err := d.Append(2, 2); err != nil {
		t.Fatal(err)
	}
	if err := d.Barrier(ctx); err != nil {
		t.Fatal(err)
	}
	if got := d.Status(); got.Accepted != 2 || got.Delivered != 2 || got.Remaining != 0 {
		t.Fatalf("delivered prefix = %+v", got)
	}
	if err := d.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := d.Append(3, 3); !errors.Is(err, ErrClosed) {
		t.Fatalf("append after close = %v", err)
	}
}

type opaquePayload struct {
	Number  int
	Data    map[string]any
	Hook    func()
	marshal *atomic.Int64
}

func (p opaquePayload) MarshalJSON() ([]byte, error) {
	p.marshal.Add(1)
	panic("generic observer payload must not be marshaled")
}

func TestAppendCopiesOnlyValueWithoutCallingUserHooks(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			ctx, r := actualRuntime(t, factory)
			owner, _ := actualOwner(t, ctx, r, "owner")
			d := testDispatcher[opaquePayload](t, nil)
			started := make(chan struct{})
			gate, release := releaseGate()
			defer release()
			var marshals, hooks atomic.Int64
			var got []opaquePayload
			if _, err := d.AddObserver(owner, "opaque", func(_ context.Context, value opaquePayload) error {
				if value.Number == 1 {
					close(started)
					<-gate
				}
				got = append(got, value)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := d.Append(1, opaquePayload{Number: 1, marshal: &marshals}); err != nil {
				t.Fatal(err)
			}
			awaitSignal(t, ctx, started, "first observer")
			value := opaquePayload{Number: 2, Data: map[string]any{"stable": "original"}, Hook: func() { hooks.Add(1) }, marshal: &marshals}
			if err := d.Append(2, value); err != nil {
				t.Fatal(err)
			}
			// Reassign the caller's top-level value, never mutate referenced data.
			value.Number, value.Data = 99, map[string]any{"stable": "replacement"}
			release()
			if err := d.WaitBarrier(ctx, 2); err != nil {
				t.Fatal(err)
			}
			if len(got) != 2 || got[1].Number != 2 || got[1].Data["stable"] != "original" || got[1].Hook == nil {
				t.Fatalf("value copy changed: %+v", got)
			}
			if marshals.Load() != 0 || hooks.Load() != 0 {
				t.Fatalf("Append invoked user hooks: marshal=%d hook=%d", marshals.Load(), hooks.Load())
			}
		})
	}
}

func TestEnqueueSnapshotSurvivesManualRemovalAndNewRegistration(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			ctx, r := actualRuntime(t, factory)
			owner, _ := actualOwner(t, ctx, r, "owner")
			d := testDispatcher[int](t, nil)
			started := make(chan struct{})
			gate, release := releaseGate()
			defer release()
			var got []string
			if _, err := d.AddObserver(owner, "A", func(_ context.Context, event int) error {
				got = append(got, fmt.Sprintf("A%d", event))
				if event == 1 {
					close(started)
					<-gate
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			removeB, err := d.AddObserver(owner, "B", func(_ context.Context, event int) error {
				got = append(got, fmt.Sprintf("B%d", event))
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := d.Append(1, 1); err != nil {
				t.Fatal(err)
			}
			awaitSignal(t, ctx, started, "A1")
			if err := d.Append(2, 2); err != nil {
				t.Fatal(err)
			}
			if err := removeB(ctx); err != nil {
				t.Fatal(err)
			}
			if err := removeB(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := d.AddObserver(owner, "C", func(_ context.Context, event int) error {
				got = append(got, fmt.Sprintf("C%d", event))
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := d.Append(3, 3); err != nil {
				t.Fatal(err)
			}
			release()
			if err := d.Barrier(ctx); err != nil {
				t.Fatal(err)
			}
			want := []string{"A1", "B1", "A2", "B2", "A3", "C3"}
			if !slices.Equal(got, want) {
				t.Fatalf("enqueue-time snapshots = %v, want %v", got, want)
			}
			if got := d.Status(); got.Observers != 2 || got.Callbacks != 6 {
				t.Fatalf("COW status = %+v", got)
			}
		})
	}
}

func TestCallbackContextBarrierGuardIsExplicitNotGoroutineDetection(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			ctx, r := actualRuntime(t, factory)
			owner, _ := actualOwner(t, ctx, r, "owner")
			d := testDispatcher[int](t, nil)
			other := testDispatcher[int](t, nil)
			failures := make(chan error, 10)
			if _, err := d.AddObserver(owner, "guard", func(callbackCtx context.Context, _ int) error {
				if _, deadline := callbackCtx.Deadline(); deadline || callbackCtx.Err() != nil {
					failures <- errors.New("callback inherited a caller deadline or cancellation")
				}
				derived, cancel := context.WithCancel(callbackCtx)
				defer cancel()
				for _, marked := range []context.Context{callbackCtx, derived, context.WithValue(derived, struct{}{}, "value")} {
					if err := d.Barrier(marked); !errors.Is(err, ErrReentrant) {
						failures <- fmt.Errorf("own marked Barrier = %v", err)
					}
					if err := d.WaitBarrier(marked, 1); !errors.Is(err, ErrReentrant) {
						failures <- fmt.Errorf("own marked WaitBarrier = %v", err)
					}
				}
				if err := d.Close(callbackCtx); !errors.Is(err, ErrReentrant) {
					failures <- fmt.Errorf("own marked Close = %v", err)
				}
				if err := other.WaitBarrier(callbackCtx, 0); err != nil {
					failures <- fmt.Errorf("foreign marker rejected = %v", err)
				}
				unmarked, cancelUnmarked := context.WithCancel(context.Background())
				cancelUnmarked()
				if err := d.WaitBarrier(unmarked, 1); !errors.Is(err, context.Canceled) {
					failures <- fmt.Errorf("unmarked own wait used hidden detection = %v", err)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := d.Append(1, 1); err != nil {
				t.Fatal(err)
			}
			if err := d.Barrier(ctx); err != nil {
				t.Fatal(err)
			}
			close(failures)
			for err := range failures {
				t.Error(err)
			}
			if d.Status().Closing {
				t.Fatal("rejected callback Close stopped dispatcher")
			}
		})
	}
}

type callbackError struct {
	error
	format func()
}

func (e callbackError) Error() string {
	e.format()
	return e.error.Error()
}

func (e callbackError) Unwrap() error { return e.error }

type checkingBinding struct {
	Binding
	check func()
}

func (b checkingBinding) Enter() bool {
	b.check()
	return b.Binding.Enter()
}

func (b checkingBinding) Leave() {
	b.check()
	b.Binding.Leave()
}

func (b checkingBinding) Defer(label string, dispose func(context.Context) error) error {
	b.check()
	return b.Binding.Defer(label, dispose)
}

func TestCanonicalContainmentAndAllUserCodeOutsideMutationLock(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			ctx, r := actualRuntime(t, factory)
			owner, _ := actualOwner(t, ctx, r, "owner")
			var d *Dispatcher[int]
			var diagnostics []error
			var logCalls, formats atomic.Int64
			observerCause, panicCause, loggerCause := errors.New("observer error"), errors.New("observer panic"), errors.New("logger panic")
			d = testDispatcher[int](t, func(err error) {
				d.Status() // Reentry would deadlock if logging held mutation mu.
				if _, err := r.Snapshots(ctx); err != nil {
					panic(err)
				}
				diagnostics = append(diagnostics, err)
				if logCalls.Add(1) == 1 {
					panic(loggerCause)
				}
			})
			checked := checkingBinding{Binding: owner, check: func() { d.Status() }}
			var got []string
			if _, err := d.AddObserver(checked, "error", func(_ context.Context, event int) error {
				d.Status()
				got = append(got, fmt.Sprintf("error%d", event))
				return callbackError{error: observerCause, format: func() {
					d.Status() // Error formatting is also executable user code.
					formats.Add(1)
					if _, err := r.Snapshots(ctx); err != nil {
						panic(err)
					}
				}}
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := d.AddObserver(owner, "panic", func(_ context.Context, event int) error {
				got = append(got, fmt.Sprintf("panic%d", event))
				panic(panicCause)
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := d.AddObserver(owner, "healthy", func(_ context.Context, event int) error {
				got = append(got, fmt.Sprintf("healthy%d", event))
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			for sequence := uint64(1); sequence <= 2; sequence++ {
				if err := d.Append(sequence, int(sequence)); err != nil {
					t.Fatal(err)
				}
			}
			if err := d.WaitBarrier(ctx, 2); err != nil {
				t.Fatal(err)
			}
			want := []string{"error1", "panic1", "healthy1", "error2", "panic2", "healthy2"}
			if !slices.Equal(got, want) || len(diagnostics) != 4 || formats.Load() != 2 || logCalls.Load() != 4 {
				t.Fatalf("containment: calls=%v diagnostics=%d formats=%d logger=%d", got, len(diagnostics), formats.Load(), logCalls.Load())
			}
			for index, diagnostic := range diagnostics {
				cause := observerCause
				if index%2 == 1 {
					cause = panicCause
					var recovered *PanicError
					if !errors.As(diagnostic, &recovered) {
						t.Errorf("panic identity missing: %T", diagnostic)
					}
				}
				if !errors.Is(diagnostic, cause) {
					t.Errorf("original error identity missing: %v", diagnostic)
				}
				before := formats.Load()
				_ = diagnostic.Error()
				_ = diagnostic.Error()
				if formats.Load() != before {
					t.Fatal("retained diagnostic re-executed user Error method")
				}
			}
			status := d.Status()
			if status.Delivered != 2 || status.Callbacks != 6 || status.ObserverErrors != 4 || status.LoggerPanics != 1 || status.LastLoggerError == "" {
				t.Fatalf("contained failure status = %+v", status)
			}
		})
	}
}

func TestErrorFormattingPanicAndPanicNilDoNotStopWorker(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			ctx, r := actualRuntime(t, factory)
			owner, _ := actualOwner(t, ctx, r, "owner")
			var diagnostics []error
			d := testDispatcher[int](t, func(err error) { diagnostics = append(diagnostics, err) })
			if _, err := d.AddObserver(owner, "format", func(context.Context, int) error {
				return callbackError{error: errors.New("hostile error"), format: func() { panic("formatter") }}
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := d.AddObserver(owner, "nil panic", func(context.Context, int) error { panic(nil) }); err != nil {
				t.Fatal(err)
			}
			var healthy atomic.Int64
			if _, err := d.AddObserver(owner, "healthy", func(context.Context, int) error { healthy.Add(1); return nil }); err != nil {
				t.Fatal(err)
			}
			if err := d.Append(1, 1); err != nil {
				t.Fatal(err)
			}
			if err := d.Barrier(ctx); err != nil {
				t.Fatal(err)
			}
			if len(diagnostics) != 2 || healthy.Load() != 1 || d.Status().FormattingPanics != 1 {
				t.Fatalf("format/nil panic containment: diagnostics=%d healthy=%d status=%+v", len(diagnostics), healthy.Load(), d.Status())
			}
		})
	}
}

func TestBarrierCanJoinPrefixWhileLaterCallbackIsHeld(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			ctx, r := actualRuntime(t, factory)
			owner, _ := actualOwner(t, ctx, r, "owner")
			d := testDispatcher[int](t, nil)
			started := make(chan struct{})
			gate, release := releaseGate()
			defer release()
			if _, err := d.AddObserver(owner, "hold second", func(_ context.Context, event int) error {
				if event == 2 {
					close(started)
					<-gate
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := d.Append(1, 1); err != nil {
				t.Fatal(err)
			}
			if err := d.Append(2, 2); err != nil {
				t.Fatal(err)
			}
			awaitSignal(t, ctx, started, "second callback")
			if err := d.WaitBarrier(ctx, 1); err != nil {
				t.Fatal(err)
			}
			canceled, cancel := context.WithCancel(context.Background())
			cancel()
			if err := d.WaitBarrier(canceled, 2); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled barrier = %v", err)
			}
			if got := d.Status(); got.Delivered != 1 || got.Remaining != 1 || !got.InFlight {
				t.Fatalf("canceled wait lost held work: %+v", got)
			}
			release()
			if err := d.Barrier(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}
