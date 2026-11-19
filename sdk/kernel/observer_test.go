// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

var nativeObserverConstructors = []struct {
	name string
	make func() Runtime
}{
	{"reconciler", NewRecordingReconciler},
}

func nativeObserverTestRuntime(t *testing.T, makeRuntime func() Runtime) (context.Context, Runtime, ObserverRuntime, *Handle) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	r := makeRuntime()
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer closeCancel()
		if err := closeRuntime(closeCtx, r); err != nil {
			t.Errorf("close native runtime: %v", err)
		}
	})
	observers, ok := r.(ObserverRuntime)
	if !ok {
		t.Fatal("native runtime does not implement ObserverRuntime")
	}
	rootProvider, ok := r.(RootProvider)
	if !ok {
		t.Fatal("native runtime does not expose its real root activation")
	}
	root, err := rootProvider.RootContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, r, observers, root
}

func nativeObserverReceive[T any](t *testing.T, ctx context.Context, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-ctx.Done():
		t.Fatalf("observer test barrier: %v", ctx.Err())
		var zero T
		return zero
	}
}

func nativeObserverRecord(t *testing.T, ctx context.Context, r Runtime, key string) {
	t.Helper()
	recorder, ok := r.(Recorder)
	if !ok {
		t.Fatal("native runtime does not expose fixture instrumentation")
	}
	if err := recorder.Record(ctx, key, map[string]any{"key": key}); err != nil {
		t.Fatal(err)
	}
}

func nativeObserverRelease(t *testing.T) (chan struct{}, func()) {
	t.Helper()
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	return release, unblock
}

func TestNativeObserversDeliverVerbatimLedgerInTotalSequenceOrder(t *testing.T) {
	for _, candidate := range nativeObserverConstructors {
		t.Run(candidate.name, func(t *testing.T) {
			ctx, r, observers, root := nativeObserverTestRuntime(t, candidate.make)
			var mu sync.Mutex
			var observed []Event
			_, err := observers.ObserveEvents(root, "ledger", func(_ context.Context, event Event) error {
				mu.Lock()
				observed = append(observed, event)
				mu.Unlock()
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			// Registration itself installs a real native effect before publication.
			// Compare the complete stream from that publication point, not a
			// fabricated cross-backend vocabulary or a filtered event-kind list.
			start := observers.ObserverStatus().Accepted
			value := &struct{ name string }{"actual native value"}
			provider, err := r.Mount(ctx, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Handle) error {
				return c.Provide("sample", value)
			}})
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := r.Mount(ctx, Spec{ID: "consumer", Needs: []string{"sample"}, Apply: func(c *Handle) error {
				got, err := c.Read("sample")
				if err != nil {
					return err
				}
				if got != value {
					return errors.New("native service identity changed")
				}
				return c.Defer("consumer effect", func(context.Context) error { return nil })
			}})
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			nativeObserverRecord(t, ctx, r, "immutable payload")
			if err := r.Update(ctx, provider); err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			for _, id := range []uint64{consumer, provider} {
				if err := r.Dispose(ctx, id); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			if err := observers.AwaitObservers(ctx); err != nil {
				t.Fatal(err)
			}
			ledger, err := r.Events(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var want []Event
			for index, event := range ledger {
				if event.Sequence != uint64(index+1) {
					t.Fatalf("native ledger is not consecutive at %d: %+v", index, event)
				}
				if event.Sequence > start {
					want = append(want, event)
				}
			}
			mu.Lock()
			got := append([]Event(nil), observed...)
			mu.Unlock()
			if len(want) == 0 || !reflect.DeepEqual(got, want) {
				t.Fatalf("observer stream differs from this backend's exact ledger:\ngot  %+v\nwant %+v", got, want)
			}
			status := observers.ObserverStatus()
			if status.Accepted != uint64(len(ledger)) || status.Delivered != status.Accepted || status.Remaining != 0 {
				t.Fatalf("native stream not actually drained: %+v", status)
			}
		})
	}
}

func TestNativeObserversReenterControlPlaneAndRejectSelfBarrier(t *testing.T) {
	for _, candidate := range nativeObserverConstructors {
		t.Run(candidate.name, func(t *testing.T) {
			ctx, r, observers, root := nativeObserverTestRuntime(t, candidate.make)
			id, err := r.Mount(ctx, Spec{ID: "updated from observer"})
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			_, err = observers.ObserveEvents(root, "reentrant", func(callbackCtx context.Context, event Event) error {
				if markerOf(event) != "reenter" {
					return nil
				}
				if _, err := r.Snapshots(callbackCtx); err != nil {
					result <- err
					return nil
				}
				if err := r.Update(callbackCtx, id); err != nil {
					result <- err
					return nil
				}
				if err := r.(Recorder).Record(callbackCtx, "reentered", map[string]any{"native": true}); err != nil {
					result <- err
					return nil
				}
				// A derived callback context preserves the queue's self-join marker.
				derived, cancel := context.WithCancel(callbackCtx)
				defer cancel()
				result <- observers.AwaitObservers(derived)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			nativeObserverRecord(t, ctx, r, "reenter")
			if err := nativeObserverReceive(t, ctx, result); !errors.Is(err, errReentrant) {
				t.Fatalf("callback barrier did not reject self-join: %v", err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			if err := observers.AwaitObservers(ctx); err != nil {
				t.Fatal(err)
			}
			snapshots, err := r.Snapshots(ctx)
			if err != nil || len(snapshots) != 1 || snapshots[0].Revision != 1 || snapshots[0].State != Active {
				t.Fatalf("reentrant update did not settle: %+v / %v", snapshots, err)
			}
		})
	}
}

func TestNativeObserversSettleDoesNotJoinHeldCallback(t *testing.T) {
	for _, candidate := range nativeObserverConstructors {
		t.Run(candidate.name, func(t *testing.T) {
			ctx, r, observers, root := nativeObserverTestRuntime(t, candidate.make)
			release, unblock := nativeObserverRelease(t)
			started := make(chan struct{}, 1)
			_, err := observers.ObserveEvents(root, "held", func(_ context.Context, event Event) error {
				if markerOf(event) == "held" {
					started <- struct{}{}
					<-release
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			nativeObserverRecord(t, ctx, r, "held")
			nativeObserverReceive(t, ctx, started)
			if err := r.Settle(ctx); err != nil {
				t.Fatalf("Settle joined independent observer work: %v", err)
			}
			if _, err := r.Snapshots(ctx); err != nil {
				t.Fatal(err)
			}
			status := observers.ObserverStatus()
			if !status.InFlight || status.Remaining == 0 || status.Delivered >= status.Accepted {
				t.Fatalf("held callback was reported delivered: %+v", status)
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if err := observers.AwaitObservers(canceled); !errors.Is(err, context.Canceled) {
				t.Fatalf("bounded explicit barrier = %v", err)
			}
			unblock()
			if err := observers.AwaitObservers(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNativeObserversRequireExactRuntimeOwnerAndLiveActivation(t *testing.T) {
	for _, candidate := range nativeObserverConstructors {
		t.Run(candidate.name, func(t *testing.T) {
			ctx, r, observers, root := nativeObserverTestRuntime(t, candidate.make)
			callback := func(context.Context, Event) error { return nil }
			for _, owner := range []*Handle{nil, {}} {
				if dispose, err := observers.ObserveEvents(owner, "invalid", callback); !errors.Is(err, ErrObserverBinding) || dispose != nil {
					t.Fatalf("nil/zero owner registration = %v, disposer nil=%t", err, dispose == nil)
				}
			}
			for _, foreignCandidate := range nativeObserverConstructors {
				_, foreign, _, foreignRoot := nativeObserverTestRuntime(t, foreignCandidate.make)
				if dispose, err := observers.ObserveEvents(foreignRoot, "foreign", callback); !errors.Is(err, ErrObserverBinding) || dispose != nil {
					t.Fatalf("foreign %s owner registration = %v, disposer nil=%t", foreignCandidate.name, err, dispose == nil)
				}
				if err := foreign.Settle(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := observers.ObserveEvents(root, "", callback); !errors.Is(err, errObserverArgs) {
				t.Fatalf("empty listener label = %v", err)
			}
			if _, err := observers.ObserveEvents(root, "nil callback", nil); !errors.Is(err, errObserverArgs) {
				t.Fatalf("nil listener callback = %v", err)
			}
			owners := make(chan *Handle, 1)
			id, err := r.Mount(ctx, Spec{ID: "retired owner", Apply: func(c *Handle) error { owners <- c; return nil }})
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			owner := nativeObserverReceive(t, ctx, owners)
			if err := r.Dispose(ctx, id); err != nil {
				t.Fatal(err)
			}
			nativeObserverReceive(t, ctx, owner.Std().Done())
			if dispose, err := observers.ObserveEvents(owner, "quiesced", callback); !errors.Is(err, ErrInactive) || dispose != nil {
				t.Fatalf("quiesced native owner registration = %v, disposer nil=%t", err, dispose == nil)
			}
		})
	}
}

func TestNativeObserversLeaseDelaysDependentAndProviderCleanup(t *testing.T) {
	for _, candidate := range nativeObserverConstructors {
		t.Run(candidate.name, func(t *testing.T) {
			ctx, r, observers, _ := nativeObserverTestRuntime(t, candidate.make)
			release, unblock := nativeObserverRelease(t)
			started := make(chan struct{}, 1)
			callbackResult := make(chan error, 1)
			owners := make(chan *Handle, 1)
			var queuedCalls atomic.Int64
			var mu sync.Mutex
			var cleanup []string
			addCleanup := func(label string) { mu.Lock(); cleanup = append(cleanup, label); mu.Unlock() }
			provider, err := r.Mount(ctx, Spec{ID: "provider", Provides: []string{"leased"}, Apply: func(c *Handle) error {
				if err := c.Defer("provider cleanup", func(context.Context) error { addCleanup("provider"); return nil }); err != nil {
					return err
				}
				return c.Provide("leased", "retained native provision")
			}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = r.Mount(ctx, Spec{ID: "listener", Needs: []string{"leased"}, Apply: func(c *Handle) error {
				if err := c.Defer("listener cleanup", func(context.Context) error {
					value, err := c.Read("leased")
					if err != nil || value != "retained native provision" {
						return errors.New("provider released before dependent observer cleanup")
					}
					addCleanup("listener")
					return nil
				}); err != nil {
					return err
				}
				_, err := observers.ObserveEvents(c, "leased listener", func(_ context.Context, event Event) error {
					if markerOf(event) == "" {
						return nil
					}
					switch markerOf(event) {
					case "held":
						started <- struct{}{}
						<-release
						value, err := c.Read("leased")
						if err == nil && value != "retained native provision" {
							err = errors.New("committed provision identity changed during observer lease")
						}
						callbackResult <- err
					case "queued":
						queuedCalls.Add(1)
					}
					return nil
				})
				owners <- c
				return err
			}})
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			owner := nativeObserverReceive(t, ctx, owners)
			nativeObserverRecord(t, ctx, r, "held")
			nativeObserverReceive(t, ctx, started)
			nativeObserverRecord(t, ctx, r, "queued")
			if err := r.Dispose(ctx, provider); err != nil {
				t.Fatal(err)
			}
			nativeObserverReceive(t, ctx, owner.Std().Done())
			if owner.Enter() {
				owner.Leave()
				t.Fatal("quiesced listener accepted another callback lease")
			}
			mu.Lock()
			premature := append([]string(nil), cleanup...)
			mu.Unlock()
			if len(premature) != 0 {
				t.Fatalf("cleanup ran before actual callback return: %v", premature)
			}
			unblock()
			if err := nativeObserverReceive(t, ctx, callbackResult); err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			if err := observers.AwaitObservers(ctx); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			got := append([]string(nil), cleanup...)
			mu.Unlock()
			if !reflect.DeepEqual(got, []string{"listener", "provider"}) {
				t.Fatalf("native cleanup order after lease return = %v", got)
			}
			if queuedCalls.Load() != 0 || observers.ObserverStatus().Skipped == 0 {
				t.Fatalf("queued callback entered inactive owner: calls=%d status=%+v", queuedCalls.Load(), observers.ObserverStatus())
			}
		})
	}
}

func TestNativeObserversManualRemovalKeepsCapturedSnapshotOnly(t *testing.T) {
	for _, candidate := range nativeObserverConstructors {
		t.Run(candidate.name, func(t *testing.T) {
			ctx, r, observers, root := nativeObserverTestRuntime(t, candidate.make)
			release, unblock := nativeObserverRelease(t)
			started := make(chan struct{}, 1)
			var mu sync.Mutex
			var calls []string
			dispose, err := observers.ObserveEvents(root, "manual removal", func(_ context.Context, event Event) error {
				if markerOf(event) == "" {
					return nil
				}
				mu.Lock()
				calls = append(calls, markerOf(event))
				mu.Unlock()
				if markerOf(event) == "held" {
					started <- struct{}{}
					<-release
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			nativeObserverRecord(t, ctx, r, "held")
			nativeObserverReceive(t, ctx, started)
			nativeObserverRecord(t, ctx, r, "captured")
			if err := dispose(ctx); err != nil {
				t.Fatal(err)
			}
			if err := dispose(ctx); err != nil {
				t.Fatalf("manual removal is not idempotent: %v", err)
			}
			nativeObserverRecord(t, ctx, r, "future")
			unblock()
			if err := observers.AwaitObservers(ctx); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			got := append([]string(nil), calls...)
			mu.Unlock()
			if !reflect.DeepEqual(got, []string{"held", "captured"}) || observers.ObserverStatus().Observers != 0 {
				t.Fatalf("enqueue-time snapshot removal = %v, status=%+v", got, observers.ObserverStatus())
			}
		})
	}
}

func TestNativeObserversErrorsAndPanicsAreDiagnosticsNotBarrierFailures(t *testing.T) {
	for _, candidate := range nativeObserverConstructors {
		t.Run(candidate.name, func(t *testing.T) {
			ctx, r, observers, root := nativeObserverTestRuntime(t, candidate.make)
			failure := errors.New("observer failure")
			_, err := observers.ObserveEvents(root, "failing", func(_ context.Context, event Event) error {
				if markerOf(event) != "" {
					switch markerOf(event) {
					case "error":
						return failure
					case "panic":
						panic(failure)
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			var healthy atomic.Int64
			_, err = observers.ObserveEvents(root, "healthy", func(_ context.Context, event Event) error {
				if markerOf(event) != "" {
					healthy.Add(1)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"error", "panic", "after failure"} {
				nativeObserverRecord(t, ctx, r, key)
			}
			if err := observers.AwaitObservers(ctx); err != nil {
				t.Fatalf("observer failures escaped diagnostic boundary: %v", err)
			}
			status := observers.ObserverStatus()
			if healthy.Load() != 3 || status.ObserverErrors != 2 || status.LastObserverError == "" || status.LoggerPanics != 0 || status.Remaining != 0 {
				t.Fatalf("nil-logger error/panic containment: healthy=%d status=%+v", healthy.Load(), status)
			}
		})
	}
}

func TestNativeObserversAwaitCapturesWatermarkNotFutureEvents(t *testing.T) {
	for _, candidate := range nativeObserverConstructors {
		t.Run(candidate.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, r, observers, root := nativeObserverTestRuntime(t, candidate.make)
				firstRelease, releaseFirst := nativeObserverRelease(t)
				futureRelease, releaseFuture := nativeObserverRelease(t)
				started := make(chan string, 2)
				_, err := observers.ObserveEvents(root, "watermark", func(_ context.Context, event Event) error {
					if markerOf(event) == "" {
						return nil
					}
					switch markerOf(event) {
					case "prefix":
						started <- markerOf(event)
						<-firstRelease
					case "future":
						started <- markerOf(event)
						<-futureRelease
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				nativeObserverRecord(t, ctx, r, "prefix")
				if key := nativeObserverReceive(t, ctx, started); key != "prefix" {
					t.Fatal(key)
				}
				prefix := observers.ObserverStatus().Accepted
				joined := make(chan error, 1)
				go func() { joined <- observers.AwaitObservers(ctx) }()
				// The barrier goroutine is now durably waiting on this exact prefix.
				// No scheduling sleep or implicit Settle-to-observer join is used.
				synctest.Wait()
				nativeObserverRecord(t, ctx, r, "future")
				releaseFirst()
				if key := nativeObserverReceive(t, ctx, started); key != "future" {
					t.Fatal(key)
				}
				if err := nativeObserverReceive(t, ctx, joined); err != nil {
					t.Fatalf("captured prefix joined a future callback: %v", err)
				}
				status := observers.ObserverStatus()
				if status.Delivered != prefix || status.Accepted <= prefix || !status.InFlight {
					t.Fatalf("future callback was not genuinely outstanding: %+v, prefix=%d", status, prefix)
				}
				releaseFuture()
				if err := observers.AwaitObservers(ctx); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func TestNativeObserversCloseTimeoutRetainsWorkAndLaterCloseReallyJoins(t *testing.T) {
	for _, candidate := range nativeObserverConstructors {
		t.Run(candidate.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, r, observers, root := nativeObserverTestRuntime(t, candidate.make)
				release, unblock := nativeObserverRelease(t)
				started := make(chan struct{}, 1)
				_, err := observers.ObserveEvents(root, "close lease", func(_ context.Context, event Event) error {
					if markerOf(event) == "held" {
						started <- struct{}{}
						<-release
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				nativeObserverRecord(t, ctx, r, "held")
				nativeObserverReceive(t, ctx, started)
				nativeObserverRecord(t, ctx, r, "queued")
				bounded, cancel := context.WithTimeout(ctx, time.Second)
				defer cancel()
				if err := r.Close(bounded); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("bounded close did not report real outstanding callback: %v", err)
				}
				status := observers.ObserverStatus()
				if status.Stopped || status.Closing || !status.InFlight || status.Remaining == 0 {
					t.Fatalf("native close prematurely stopped admission before lifecycle completion: %+v", status)
				}
				if root.Enter() {
					root.Leave()
					t.Fatal("close did not quiesce real root owner")
				}
				unblock()
				if err := r.Close(ctx); err != nil {
					t.Fatalf("later close did not join actual returned worker: %v", err)
				}
				final := observers.ObserverStatus()
				if !final.Stopped || !final.Closing || final.Accepted <= status.Accepted || final.Delivered != final.Accepted || final.Remaining != 0 || final.InFlight || final.Queued != 0 {
					t.Fatalf("final native/root events were lost or falsely drained: before=%+v final=%+v", status, final)
				}
				if err := observers.AwaitObservers(ctx); err != nil {
					t.Fatalf("closed, actually drained watermark could not be joined: %v", err)
				}
			})
		})
	}
}

func TestNativeObserversDisposeCancelsCallbackBeforeJoiningLease(t *testing.T) {
	for _, candidate := range nativeObserverConstructors {
		t.Run(candidate.name, func(t *testing.T) {
			ctx, r, observers, _ := nativeObserverTestRuntime(t, candidate.make)
			started := make(chan struct{}, 1)
			type callbackResult struct {
				contextErr error
				ownerErr   error
				selfAwait  error
				closing    bool
			}
			finished := make(chan callbackResult, 1)
			id, err := r.Mount(ctx, Spec{ID: "cooperative observer", Apply: func(owner *Handle) error {
				_, err := observers.ObserveEvents(owner, "cancel on dispose", func(callbackCtx context.Context, event Event) error {
					if markerOf(event) != "cooperative dispose" {
						return nil
					}
					started <- struct{}{}
					<-callbackCtx.Done() // Only native cancellation can release this callback.
					finished <- callbackResult{
						contextErr: callbackCtx.Err(),
						ownerErr:   owner.Std().Err(),
						selfAwait:  observers.AwaitObservers(callbackCtx),
						closing:    observers.ObserverStatus().Closing,
					}
					return nil
				})
				return err
			}})
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			nativeObserverRecord(t, ctx, r, "cooperative dispose")
			nativeObserverReceive(t, ctx, started)
			if err := r.Dispose(ctx, id); err != nil {
				t.Fatal(err)
			}
			result := nativeObserverReceive(t, ctx, finished)
			if !errors.Is(result.contextErr, context.Canceled) || !errors.Is(result.ownerErr, context.Canceled) ||
				!errors.Is(result.selfAwait, errReentrant) || result.closing {
				t.Fatalf("native disposal did not cancel callback before queue close or lost its marker: %+v", result)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatalf("cooperative callback lease did not really return: %v", err)
			}
			if err := observers.AwaitObservers(ctx); err != nil {
				t.Fatal(err)
			}
			status := observers.ObserverStatus()
			if status.Closing || status.Stopped || status.Remaining != 0 || status.Delivered != status.Accepted {
				t.Fatalf("dispose required queue close or falsely drained the callback: %+v", status)
			}
			if err := r.Close(ctx); err != nil {
				t.Fatalf("close after cooperative native disposal: %v", err)
			}
		})
	}
}

func TestNativeObserversCloseCancelsCallbackBeforeClosingQueue(t *testing.T) {
	for _, candidate := range nativeObserverConstructors {
		t.Run(candidate.name, func(t *testing.T) {
			ctx, r, observers, root := nativeObserverTestRuntime(t, candidate.make)
			started := make(chan struct{}, 1)
			type callbackResult struct {
				contextErr error
				ownerErr   error
				closing    bool
			}
			finished := make(chan callbackResult, 1)
			_, err := observers.ObserveEvents(root, "cancel on close", func(callbackCtx context.Context, event Event) error {
				if markerOf(event) != "cooperative close" {
					return nil
				}
				started <- struct{}{}
				<-callbackCtx.Done() // Close must quiesce the owner before joining this lease.
				finished <- callbackResult{callbackCtx.Err(), root.Std().Err(), observers.ObserverStatus().Closing}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			nativeObserverRecord(t, ctx, r, "cooperative close")
			nativeObserverReceive(t, ctx, started)
			before := observers.ObserverStatus()
			if err := r.Close(ctx); err != nil {
				t.Fatalf("cooperative observer prevented native close: %v", err)
			}
			result := nativeObserverReceive(t, ctx, finished)
			if !errors.Is(result.contextErr, context.Canceled) || !errors.Is(result.ownerErr, context.Canceled) || result.closing {
				t.Fatalf("callback was canceled only by queue close rather than native root quiescence: %+v", result)
			}
			final := observers.ObserverStatus()
			if !final.Closing || !final.Stopped || final.Accepted <= before.Accepted || final.Delivered != final.Accepted ||
				final.Remaining != 0 || final.InFlight || final.Queued != 0 {
				t.Fatalf("cooperative close did not join all final native events: before=%+v final=%+v", before, final)
			}
		})
	}
}

func TestNativeObserversRetainFirstQueueAdmissionFailure(t *testing.T) {
	n := newObserverSet(1<<16, nil)
	t.Cleanup(func() { _ = n.close(context.Background()) })
	if err := n.append(Event{Sequence: 2}); !errors.Is(err, errSequenceGap) {
		t.Fatalf("gap admission = %v", err)
	}
	if err := n.append(Event{Sequence: 1}); err != nil {
		t.Fatal(err)
	}
	if err := n.await(context.Background()); !errors.Is(err, errSequenceGap) {
		t.Fatalf("barrier hid a broken native ledger integration: %v", err)
	}
	if err := n.close(context.Background()); !errors.Is(err, errSequenceGap) {
		t.Fatalf("close hid a broken native ledger integration: %v", err)
	}
	if err := n.append(Event{Sequence: 2}); !errors.Is(err, errObserverClosed) {
		t.Fatalf("post-close admission = %v", err)
	}
	if err := n.await(context.Background()); !errors.Is(err, errSequenceGap) || errors.Is(err, errObserverClosed) {
		t.Fatalf("first queue admission failure was replaced: %v", err)
	}
	if status := n.d.snapshot(); !status.Stopped || status.Accepted != 1 || status.Delivered != 1 {
		t.Fatalf("retained failure prevented the actual accepted prefix from draining: %+v", status)
	}
}
