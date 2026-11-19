// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var candidates = map[string]func() Runtime{"reconciler": NewRecordingReconciler}

// Only an adapter to actual native contexts, never a replacement/fake host.
type nativeBinding = *handle

func actualRuntime(t *testing.T, factory func() Runtime) (context.Context, Runtime) {
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

func actualOwner(t *testing.T, ctx context.Context, r Runtime, label string) (nativeBinding, uint64) {
	t.Helper()
	ready := make(chan *Handle, 1)
	id, err := r.Mount(ctx, Spec{ID: label, Apply: func(c *Handle) error {
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
		return c, id
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return nil, 0
	}
}

func testDispatcher(t *testing.T, logger logFunc) *dispatcher {
	t.Helper()
	d := newDispatcher(logger, dispatcherOptions{Backlog: 1 << 16})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := d.close(ctx); err != nil {
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
	d := testDispatcher(t, nil)
	for _, sequence := range []uint64{0, 2, ^uint64(0)} {
		if err := appendInt(d, sequence, 0); !errors.Is(err, errSequenceGap) {
			t.Fatalf("initial sequence %d = %v", sequence, err)
		}
	}
	if got := d.snapshot(); got.Accepted != 0 || got.Delivered != 0 || got.Queued != 0 {
		t.Fatalf("rejection changed empty queue: %+v", got)
	}
	if err := d.waitBarrier(ctx, 1); !errors.Is(err, errNotAccepted) {
		t.Fatalf("unaccepted barrier = %v", err)
	}
	if err := d.waitBarrier(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if err := appendInt(d, 1, 1); err != nil {
		t.Fatal(err)
	}
	for _, sequence := range []uint64{0, 1, 3, ^uint64(0)} {
		if err := appendInt(d, sequence, 0); !errors.Is(err, errSequenceGap) {
			t.Fatalf("sequence %d after 1 = %v", sequence, err)
		}
	}
	if err := appendInt(d, 2, 2); err != nil {
		t.Fatal(err)
	}
	if err := d.barrier(ctx); err != nil {
		t.Fatal(err)
	}
	if got := d.snapshot(); got.Accepted != 2 || got.Delivered != 2 || got.Remaining != 0 {
		t.Fatalf("delivered prefix = %+v", got)
	}
	if err := d.close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := appendInt(d, 3, 3); !errors.Is(err, errObserverClosed) {
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
			d := testDispatcher(t, nil)
			started := make(chan struct{})
			gate, release := releaseGate()
			defer release()
			var marshals, hooks atomic.Int64
			var got []opaquePayload
			if _, err := d.addObserver(owner, "opaque", func(_ context.Context, event ledgerEvent) error {
				value := event.Value.(opaquePayload)
				if value.Number == 1 {
					close(started)
					<-gate
				}
				got = append(got, value)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := d.append(1, ledgerEvent{Sequence: 1, Value: opaquePayload{Number: 1, marshal: &marshals}}); err != nil {
				t.Fatal(err)
			}
			awaitSignal(t, ctx, started, "first observer")
			value := opaquePayload{Number: 2, Data: map[string]any{"stable": "original"}, Hook: func() { hooks.Add(1) }, marshal: &marshals}
			if err := d.append(2, ledgerEvent{Sequence: 2, Value: value}); err != nil {
				t.Fatal(err)
			}
			// Reassign the caller's top-level value, never mutate referenced data.
			value.Number, value.Data = 99, map[string]any{"stable": "replacement"}
			release()
			if err := d.waitBarrier(ctx, 2); err != nil {
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
			d := testDispatcher(t, nil)
			started := make(chan struct{})
			gate, release := releaseGate()
			defer release()
			var got []string
			if _, err := addInt(d, owner, "A", func(_ context.Context, event int) error {
				got = append(got, fmt.Sprintf("A%d", event))
				if event == 1 {
					close(started)
					<-gate
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			removeB, err := addInt(d, owner, "B", func(_ context.Context, event int) error {
				got = append(got, fmt.Sprintf("B%d", event))
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := appendInt(d, 1, 1); err != nil {
				t.Fatal(err)
			}
			awaitSignal(t, ctx, started, "A1")
			if err := appendInt(d, 2, 2); err != nil {
				t.Fatal(err)
			}
			if err := removeB(ctx); err != nil {
				t.Fatal(err)
			}
			if err := removeB(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := addInt(d, owner, "C", func(_ context.Context, event int) error {
				got = append(got, fmt.Sprintf("C%d", event))
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := appendInt(d, 3, 3); err != nil {
				t.Fatal(err)
			}
			release()
			if err := d.barrier(ctx); err != nil {
				t.Fatal(err)
			}
			want := []string{"A1", "B1", "A2", "B2", "A3", "C3"}
			if !slices.Equal(got, want) {
				t.Fatalf("enqueue-time snapshots = %v, want %v", got, want)
			}
			if got := d.snapshot(); got.Observers != 2 || got.Callbacks != 6 {
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
			d := testDispatcher(t, nil)
			other := testDispatcher(t, nil)
			failures := make(chan error, 10)
			if _, err := addInt(d, owner, "guard", func(callbackCtx context.Context, _ int) error {
				if _, deadline := callbackCtx.Deadline(); deadline || callbackCtx.Err() != nil {
					failures <- errors.New("callback inherited a caller deadline or cancellation")
				}
				derived, cancel := context.WithCancel(callbackCtx)
				defer cancel()
				for _, marked := range []context.Context{callbackCtx, derived, context.WithValue(derived, struct{}{}, "value")} {
					if err := d.barrier(marked); !errors.Is(err, errReentrant) {
						failures <- fmt.Errorf("own marked Barrier = %v", err)
					}
					if err := d.waitBarrier(marked, 1); !errors.Is(err, errReentrant) {
						failures <- fmt.Errorf("own marked WaitBarrier = %v", err)
					}
				}
				if err := d.close(callbackCtx); !errors.Is(err, errReentrant) {
					failures <- fmt.Errorf("own marked Close = %v", err)
				}
				if err := other.waitBarrier(callbackCtx, 0); err != nil {
					failures <- fmt.Errorf("foreign marker rejected = %v", err)
				}
				unmarked, cancelUnmarked := context.WithCancel(context.Background())
				cancelUnmarked()
				if err := d.waitBarrier(unmarked, 1); !errors.Is(err, context.Canceled) {
					failures <- fmt.Errorf("unmarked own wait used hidden detection = %v", err)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := appendInt(d, 1, 1); err != nil {
				t.Fatal(err)
			}
			if err := d.barrier(ctx); err != nil {
				t.Fatal(err)
			}
			close(failures)
			for err := range failures {
				t.Error(err)
			}
			if d.snapshot().Closing {
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
	leaseHolder
	check func()
}

func (b checkingBinding) enter() (func(), bool) {
	b.check()
	leave, ok := b.leaseHolder.enter()
	if !ok {
		return nil, false
	}
	return func() { b.check(); leave() }, true
}

func (b checkingBinding) tryDefer(label string, dispose disposer) error {
	b.check()
	return b.leaseHolder.tryDefer(label, dispose)
}

func TestCanonicalContainmentAndAllUserCodeOutsideMutationLock(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			ctx, r := actualRuntime(t, factory)
			owner, _ := actualOwner(t, ctx, r, "owner")
			var d *dispatcher
			var diagnostics []error
			var logCalls, formats atomic.Int64
			observerCause, panicCause, loggerCause := errors.New("observer error"), errors.New("observer panic"), errors.New("logger panic")
			d = testDispatcher(t, func(err error) {
				d.snapshot() // Reentry would deadlock if logging held mutation mu.
				if _, err := r.Snapshots(ctx); err != nil {
					panic(err)
				}
				diagnostics = append(diagnostics, err)
				if logCalls.Add(1) == 1 {
					panic(loggerCause)
				}
			})
			checked := checkingBinding{leaseHolder: owner, check: func() { d.snapshot() }}
			var got []string
			if _, err := addInt(d, checked, "error", func(_ context.Context, event int) error {
				d.snapshot()
				got = append(got, fmt.Sprintf("error%d", event))
				return callbackError{error: observerCause, format: func() {
					d.snapshot() // Error formatting is also executable user code.
					formats.Add(1)
					if _, err := r.Snapshots(ctx); err != nil {
						panic(err)
					}
				}}
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := addInt(d, owner, "panic", func(_ context.Context, event int) error {
				got = append(got, fmt.Sprintf("panic%d", event))
				panic(panicCause)
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := addInt(d, owner, "healthy", func(_ context.Context, event int) error {
				got = append(got, fmt.Sprintf("healthy%d", event))
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			for sequence := uint64(1); sequence <= 2; sequence++ {
				if err := appendInt(d, sequence, int(sequence)); err != nil {
					t.Fatal(err)
				}
			}
			if err := d.waitBarrier(ctx, 2); err != nil {
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
					var recovered *panicError
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
			status := d.snapshot()
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
			d := testDispatcher(t, func(err error) { diagnostics = append(diagnostics, err) })
			if _, err := addInt(d, owner, "format", func(context.Context, int) error {
				return callbackError{error: errors.New("hostile error"), format: func() { panic("formatter") }}
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := addInt(d, owner, "nil panic", func(context.Context, int) error { panic(nil) }); err != nil {
				t.Fatal(err)
			}
			var healthy atomic.Int64
			if _, err := addInt(d, owner, "healthy", func(context.Context, int) error { healthy.Add(1); return nil }); err != nil {
				t.Fatal(err)
			}
			if err := appendInt(d, 1, 1); err != nil {
				t.Fatal(err)
			}
			if err := d.barrier(ctx); err != nil {
				t.Fatal(err)
			}
			if len(diagnostics) != 2 || healthy.Load() != 1 || d.snapshot().FormattingPanics != 1 {
				t.Fatalf("format/nil panic containment: diagnostics=%d healthy=%d status=%+v", len(diagnostics), healthy.Load(), d.snapshot())
			}
		})
	}
}

func TestBarrierCanJoinPrefixWhileLaterCallbackIsHeld(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			ctx, r := actualRuntime(t, factory)
			owner, _ := actualOwner(t, ctx, r, "owner")
			d := testDispatcher(t, nil)
			started := make(chan struct{})
			gate, release := releaseGate()
			defer release()
			if _, err := addInt(d, owner, "hold second", func(_ context.Context, event int) error {
				if event == 2 {
					close(started)
					<-gate
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := appendInt(d, 1, 1); err != nil {
				t.Fatal(err)
			}
			if err := appendInt(d, 2, 2); err != nil {
				t.Fatal(err)
			}
			awaitSignal(t, ctx, started, "second callback")
			if err := d.waitBarrier(ctx, 1); err != nil {
				t.Fatal(err)
			}
			canceled, cancel := context.WithCancel(context.Background())
			cancel()
			if err := d.waitBarrier(canceled, 2); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled barrier = %v", err)
			}
			if got := d.snapshot(); got.Delivered != 1 || got.Remaining != 1 || !got.InFlight {
				t.Fatalf("canceled wait lost held work: %+v", got)
			}
			release()
			if err := d.barrier(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReentrantAppendUpdateSnapshotsAndSettlePreserveFIFO(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			ctx, r := actualRuntime(t, factory)
			owner, _ := actualOwner(t, ctx, r, "observer owner")
			_, target := actualOwner(t, ctx, r, "update target")
			d := testDispatcher(t, nil)
			started := make(chan struct{})
			gate, release := releaseGate()
			defer release()
			failures := make(chan error, 4)
			var got []int
			if _, err := addInt(d, owner, "reentrant", func(callbackCtx context.Context, event int) error {
				got = append(got, event)
				if event != 1 {
					return nil
				}
				close(started)
				<-gate
				if err := r.Update(callbackCtx, target); err != nil {
					failures <- err
				}
				if _, err := r.Snapshots(callbackCtx); err != nil {
					failures <- err
				}
				// Settle must not implicitly join the held observer callback.
				if err := r.Settle(callbackCtx); err != nil {
					failures <- err
				}
				if err := appendInt(d, 3, 3); err != nil {
					failures <- err
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := appendInt(d, 1, 1); err != nil {
				t.Fatal(err)
			}
			awaitSignal(t, ctx, started, "reentrant callback")
			if err := appendInt(d, 2, 2); err != nil {
				t.Fatal(err)
			}
			// Native settle also returns while an unrelated observer is held.
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			release()
			if err := d.waitBarrier(ctx, 1); err != nil {
				t.Fatal(err)
			}
			if err := d.waitBarrier(ctx, 3); err != nil {
				t.Fatal(err)
			}
			close(failures)
			for err := range failures {
				t.Error(err)
			}
			if !slices.Equal(got, []int{1, 2, 3}) {
				t.Fatalf("reentrant delivery order = %v", got)
			}
		})
	}
}

func TestRealEventLedgerTotalStateAndServiceOrder(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			ctx, r := actualRuntime(t, factory)
			owner, _ := actualOwner(t, ctx, r, "observer owner")
			d := testDispatcher(t, nil)
			var got []ledgerEvent
			if _, err := d.addObserver(owner, "ledger", func(callbackCtx context.Context, event ledgerEvent) error {
				if _, err := r.Snapshots(callbackCtx); err != nil {
					return err
				}
				got = append(got, event)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			provider, err := r.Mount(ctx, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Handle) error {
				return c.Provide("sample", "observed value")
			}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = r.Mount(ctx, Spec{ID: "consumer", Needs: []string{"sample"}, Apply: func(c *Handle) error {
				_, err := c.Read("sample")
				return err
			}})
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			if err := r.(Recorder).Record(ctx, "immutable", nil); err != nil {
				t.Fatal(err)
			}
			if err := r.Update(ctx, provider); err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			if err := r.Dispose(ctx, provider); err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			events, err := r.Events(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var state, published, revoked, fixture bool
			// Manual fixture feed only. No native publisher integration is claimed.
			for index, event := range events {
				if event.Sequence != uint64(index+1) {
					t.Fatalf("source total order %d = %d", index, event.Sequence)
				}
				state = state || event.Kind == "state" && event.From != "" && event.State != ""
				// The candidates currently use different ledger kind spellings;
				// preserve their actual envelopes rather than normalize/rewrite.
				published = published || (event.Kind == "service:publish" || event.Kind == "service" && event.State == Active) && event.UID > 0 && event.Value == "observed value"
				revoked = revoked || (event.Kind == "service:withdraw" || event.Kind == "service" && event.State == Unloading) && event.UID > 0
				fixture = fixture || markerOf(event) == "immutable"
				if err := d.append(event.Sequence, event); err != nil {
					t.Fatal(err)
				}
			}
			if err := d.barrier(ctx); err != nil {
				t.Fatal(err)
			}
			if !state || !published || !revoked || !fixture || !reflect.DeepEqual(got, events) {
				t.Fatalf("manual ledger feed: state=%v publish=%v revoke=%v fixture=%v equal=%v", state, published, revoked, fixture, reflect.DeepEqual(got, events))
			}
		})
	}
}

func TestDependentLeaseDrainsBeforeNativeRemovalEffects(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			ctx, r := actualRuntime(t, factory)
			d := testDispatcher(t, nil)
			gate, release := releaseGate()
			defer release()
			started, dependentCleaned, providerCleaned := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var cleanupMu sync.Mutex
			var cleanup []string
			provider, err := r.Mount(ctx, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Handle) error {
				if err := c.Defer("provider resource", func(context.Context) error {
					cleanupMu.Lock()
					cleanup = append(cleanup, "provider")
					cleanupMu.Unlock()
					close(providerCleaned)
					return nil
				}); err != nil {
					return err
				}
				return c.Provide("sample", "value")
			}})
			if err != nil {
				t.Fatal(err)
			}
			ready := make(chan nativeBinding, 1)
			var calls atomic.Int64
			_, err = r.Mount(ctx, Spec{ID: "dependent observer", Needs: []string{"sample"}, Apply: func(c *Handle) error {
				if err := c.Defer("dependent resource", func(context.Context) error {
					cleanupMu.Lock()
					cleanup = append(cleanup, "dependent")
					cleanupMu.Unlock()
					close(dependentCleaned)
					return nil
				}); err != nil {
					return err
				}
				b := c
				_, err := addInt(d, b, "dependent", func(context.Context, int) error {
					calls.Add(1)
					close(started)
					<-gate
					_, err := c.Read("sample")
					return err
				})
				ready <- b
				return err
			}})
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			owner := <-ready
			if err := appendInt(d, 1, 1); err != nil {
				t.Fatal(err)
			}
			awaitSignal(t, ctx, started, "leased dependent callback")
			// This queued snapshot still contains the observer before quiescence.
			if err := appendInt(d, 2, 2); err != nil {
				t.Fatal(err)
			}
			if err := r.Dispose(ctx, provider); err != nil {
				t.Fatal(err)
			}
			awaitSignal(t, ctx, owner.Std().Done(), "dependent quiescence")
			if owner.Enter() {
				owner.Leave()
				t.Fatal("quiescent activation admitted a new callback")
			}
			select {
			case <-dependentCleaned:
				t.Fatal("native effect removed while its callback lease was held")
			case <-providerCleaned:
				t.Fatal("provider released while dependent callback was held")
			default:
			}
			release()
			if err := d.waitBarrier(ctx, 2); err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			awaitSignal(t, ctx, dependentCleaned, "dependent cleanup")
			awaitSignal(t, ctx, providerCleaned, "provider cleanup")
			cleanupMu.Lock()
			got := slices.Clone(cleanup)
			cleanupMu.Unlock()
			if !slices.Equal(got, []string{"dependent", "provider"}) || calls.Load() != 1 {
				t.Fatalf("native drain order=%v callbacks=%d", got, calls.Load())
			}
			status := d.snapshot()
			if status.Skipped != 1 || status.Observers != 0 || status.ObserverErrors != 0 {
				t.Fatalf("quiesced snapshot / native removal = %+v", status)
			}
			if _, err := addInt(d, owner, "late", func(context.Context, int) error { return nil }); !errors.Is(err, errObserverInactive) {
				t.Fatalf("old owner registration = %v", err)
			}
		})
	}
}

func TestCloseReportsHungWorkCancelsIndependentContextAndDoesNotDropQueue(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			ctx, r := actualRuntime(t, factory)
			owner, _ := actualOwner(t, ctx, r, "owner")
			d := testDispatcher(t, nil)
			callbackContext := make(chan context.Context, 1)
			gate, release := releaseGate()
			defer release()
			var got []int
			var canceledQueued atomic.Int64
			if _, err := addInt(d, owner, "uncooperative", func(callbackCtx context.Context, event int) error {
				got = append(got, event)
				if event == 1 {
					callbackContext <- callbackCtx
					<-gate // Deliberately ignore cancellation until explicitly released.
				} else if errors.Is(callbackCtx.Err(), context.Canceled) {
					canceledQueued.Add(1)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := appendInt(d, 1, 1); err != nil {
				t.Fatal(err)
			}
			callbackCtx := <-callbackContext
			const count = 2048
			for sequence := uint64(2); sequence <= count; sequence++ {
				if err := appendInt(d, sequence, int(sequence)); err != nil {
					t.Fatal(err)
				}
			}
			if callbackCtx.Err() != nil {
				t.Fatal("runtime/barrier caller canceled the independent callback context")
			}
			closeCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			err := d.close(closeCtx)
			var incomplete *closeError
			if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &incomplete) {
				t.Fatalf("bounded close = %v", err)
			}
			status := incomplete.Status
			if status.Accepted != count || status.Delivered != 0 || status.Remaining != count || status.Queued != count-1 || !status.InFlight || status.Stopped {
				t.Fatalf("hung callback falsely drained: %+v", status)
			}
			awaitSignal(t, ctx, callbackCtx.Done(), "Close callback cancellation")
			if err := appendInt(d, count+1, count+1); !errors.Is(err, errObserverClosed) {
				t.Fatalf("append after timed-out close = %v", err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			release()
			if err := d.close(ctx); err != nil {
				t.Fatal(err)
			}
			if len(got) != count || canceledQueued.Load() != count-1 {
				t.Fatalf("Close dropped queued work: delivered=%d canceled queued=%d", len(got), canceledQueued.Load())
			}
			for index, event := range got {
				if event != index+1 {
					t.Fatalf("Close FIFO at %d = %d", index, event)
				}
			}
			if status := d.snapshot(); !status.Stopped || status.Remaining != 0 || status.Delivered != count {
				t.Fatalf("actual close completion = %+v", status)
			}
		})
	}
}

// heldSetupBinding still uses native Defer and native leases. The gate only
// makes the two real registration/quiescence orderings deterministic.
type heldSetupBinding struct {
	nativeBinding
	after   bool
	entered chan struct{}
	gate    <-chan struct{}
}

func (b heldSetupBinding) tryDefer(label string, dispose disposer) error {
	if b.after {
		if err := b.nativeBinding.tryDefer(label, dispose); err != nil {
			return err
		}
	}
	close(b.entered)
	<-b.gate
	if b.after {
		return nil
	}
	return b.nativeBinding.tryDefer(label, dispose)
}

func TestNativeSetupLeaseSpansDeferAndPublicationDuringQuiescence(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			for _, after := range []bool{false, true} {
				label := "quiesce-before-defer"
				if after {
					label = "quiesce-after-defer"
				}
				t.Run(label, func(t *testing.T) {
					ctx, r := actualRuntime(t, factory)
					owner, id := actualOwner(t, ctx, r, "owner")
					d := testDispatcher(t, nil)
					cleaned := make(chan struct{})
					if err := owner.Defer("native resource", func(context.Context) error { close(cleaned); return nil }); err != nil {
						t.Fatal(err)
					}
					gate, release := releaseGate()
					defer release()
					entered, result := make(chan struct{}), make(chan error, 1)
					var calls atomic.Int64
					go func() {
						_, err := addInt(d, heldSetupBinding{nativeBinding: owner, after: after, entered: entered, gate: gate}, "setup race", func(context.Context, int) error { calls.Add(1); return nil })
						result <- err
					}()
					awaitSignal(t, ctx, entered, "gated native effect registration")
					if err := r.Dispose(ctx, id); err != nil {
						t.Fatal(err)
					}
					awaitSignal(t, ctx, owner.Std().Done(), "setup owner quiescence")
					select {
					case <-cleaned:
						t.Fatal("native cleanup crossed the held setup lease")
					default:
					}
					release()
					select {
					case err := <-result:
						if !after && !errors.Is(err, errObserverInactive) && !errors.Is(err, ErrInactive) {
							t.Fatalf("quiesced native Defer result = %v", err)
						}
						if after && err != nil {
							t.Fatalf("already-registered effect publication = %v", err)
						}
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					if err := appendInt(d, 1, 1); err != nil {
						t.Fatal(err)
					}
					if err := d.barrier(ctx); err != nil {
						t.Fatal(err)
					}
					if err := r.Settle(ctx); err != nil {
						t.Fatal(err)
					}
					awaitSignal(t, ctx, cleaned, "setup owner actual cleanup")
					if calls.Load() != 0 || d.snapshot().Observers != 0 {
						t.Fatalf("quiesced setup published callable/leaked observer: calls=%d status=%+v", calls.Load(), d.snapshot())
					}
				})
			}
		})
	}
}

func TestConcurrentNativeRegistrationDisposalAndAppend(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			ctx, r := actualRuntime(t, factory)
			owner, id := actualOwner(t, ctx, r, "owner")
			d := testDispatcher(t, nil)
			start := make(chan struct{})
			failures := make(chan error, 8)
			var pending sync.WaitGroup
			var calls atomic.Int64
			for range 4 {
				pending.Add(1)
				go func() {
					defer pending.Done()
					<-start
					for range 40 {
						dispose, err := addInt(d, owner, "racing", func(context.Context, int) error { calls.Add(1); return nil })
						if err != nil {
							if errors.Is(err, errObserverInactive) || errors.Is(err, ErrInactive) {
								continue
							}
							failures <- err
							return
						}
						if err := dispose(ctx); err != nil {
							failures <- err
							return
						}
					}
				}()
			}
			pending.Add(1)
			go func() {
				defer pending.Done()
				<-start
				for sequence := uint64(1); sequence <= 300; sequence++ {
					if err := appendInt(d, sequence, int(sequence)); err != nil {
						failures <- err
						return
					}
				}
			}()
			pending.Add(1)
			go func() {
				defer pending.Done()
				<-start
				if err := r.Dispose(ctx, id); err != nil {
					failures <- err
				}
			}()
			close(start)
			pending.Wait()
			close(failures)
			for err := range failures {
				t.Error(err)
			}
			if err := d.barrier(ctx); err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			before := calls.Load()
			if err := appendInt(d, 301, 301); err != nil {
				t.Fatal(err)
			}
			if err := d.barrier(ctx); err != nil {
				t.Fatal(err)
			}
			if status := d.snapshot(); calls.Load() != before || status.Observers != 0 || status.Delivered != 301 {
				t.Fatalf("disposed callback remained callable: before=%d after=%d status=%+v", before, calls.Load(), status)
			}
		})
	}
}

// addInt registers an observer that reads the int payload of the corpus, carried
// in the UID field of the ledgerEvent.
func addInt(d *dispatcher, owner leaseHolder, label string, fn func(context.Context, int) error) (disposer, error) {
	return d.addObserver(owner, label, func(ctx context.Context, e ledgerEvent) error { return fn(ctx, int(e.UID)) })
}

// appendInt appends the int payload of the corpus as a ledgerEvent.
func appendInt(d *dispatcher, sequence uint64, value int) error {
	return d.append(sequence, ledgerEvent{Sequence: sequence, UID: uint64(value)})
}
