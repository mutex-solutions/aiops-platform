// SPDX-License-Identifier: Apache-2.0

package observers

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cc "github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
)

func TestReentrantAppendUpdateSnapshotsAndSettlePreserveFIFO(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			ctx, r := actualRuntime(t, factory)
			owner, _ := actualOwner(t, ctx, r, "observer owner")
			_, target := actualOwner(t, ctx, r, "update target")
			d := testDispatcher[int](t, nil)
			started := make(chan struct{})
			gate, release := releaseGate()
			defer release()
			failures := make(chan error, 4)
			var got []int
			if _, err := d.AddObserver(owner, "reentrant", func(callbackCtx context.Context, event int) error {
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
				if err := d.Append(3, 3); err != nil {
					failures <- err
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := d.Append(1, 1); err != nil {
				t.Fatal(err)
			}
			awaitSignal(t, ctx, started, "reentrant callback")
			if err := d.Append(2, 2); err != nil {
				t.Fatal(err)
			}
			// Native settle also returns while an unrelated observer is held.
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			release()
			if err := d.WaitBarrier(ctx, 1); err != nil {
				t.Fatal(err)
			}
			if err := d.WaitBarrier(ctx, 3); err != nil {
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
			d := testDispatcher[cc.Event](t, nil)
			var got []cc.Event
			if _, err := d.AddObserver(owner, "ledger", func(callbackCtx context.Context, event cc.Event) error {
				if _, err := r.Snapshots(callbackCtx); err != nil {
					return err
				}
				got = append(got, event)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			provider, err := r.Mount(ctx, cc.Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *cc.Context) error {
				return c.Provide("sample", "observed value")
			}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = r.Mount(ctx, cc.Spec{ID: "consumer", Needs: []string{"sample"}, Apply: func(c *cc.Context) error {
				_, err := c.Read("sample")
				return err
			}})
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			data := map[string]any{"value": "copied by source", "nested": map[string]any{"count": 2}}
			if err := r.(cc.Recorder).Record(ctx, "immutable", data); err != nil {
				t.Fatal(err)
			}
			data["value"] = "caller replacement"
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
				published = published || (event.Kind == "service:publish" || event.Kind == "service" && event.State == cc.Active) && event.UID > 0 && event.Value == "observed value"
				revoked = revoked || (event.Kind == "service:withdraw" || event.Kind == "service" && event.State == cc.Unloading) && event.UID > 0
				fixture = fixture || event.Kind == "fixture" && event.Data["value"] == "copied by source"
				if err := d.Append(event.Sequence, event); err != nil {
					t.Fatal(err)
				}
			}
			if err := d.Barrier(ctx); err != nil {
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
			d := testDispatcher[int](t, nil)
			gate, release := releaseGate()
			defer release()
			started, dependentCleaned, providerCleaned := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var cleanupMu sync.Mutex
			var cleanup []string
			provider, err := r.Mount(ctx, cc.Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *cc.Context) error {
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
			_, err = r.Mount(ctx, cc.Spec{ID: "dependent observer", Needs: []string{"sample"}, Apply: func(c *cc.Context) error {
				if err := c.Defer("dependent resource", func(context.Context) error {
					cleanupMu.Lock()
					cleanup = append(cleanup, "dependent")
					cleanupMu.Unlock()
					close(dependentCleaned)
					return nil
				}); err != nil {
					return err
				}
				b := nativeBinding{c}
				_, err := d.AddObserver(b, "dependent", func(context.Context, int) error {
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
			if err := d.Append(1, 1); err != nil {
				t.Fatal(err)
			}
			awaitSignal(t, ctx, started, "leased dependent callback")
			// This queued snapshot still contains the observer before quiescence.
			if err := d.Append(2, 2); err != nil {
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
			if err := d.WaitBarrier(ctx, 2); err != nil {
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
			status := d.Status()
			if status.Skipped != 1 || status.Observers != 0 || status.ObserverErrors != 0 {
				t.Fatalf("quiesced snapshot / native removal = %+v", status)
			}
			if _, err := d.AddObserver(owner, "late", func(context.Context, int) error { return nil }); !errors.Is(err, ErrInactive) {
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
			d := testDispatcher[int](t, nil)
			callbackContext := make(chan context.Context, 1)
			gate, release := releaseGate()
			defer release()
			var got []int
			var canceledQueued atomic.Int64
			if _, err := d.AddObserver(owner, "uncooperative", func(callbackCtx context.Context, event int) error {
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
			if err := d.Append(1, 1); err != nil {
				t.Fatal(err)
			}
			callbackCtx := <-callbackContext
			const count = 2048
			for sequence := uint64(2); sequence <= count; sequence++ {
				if err := d.Append(sequence, int(sequence)); err != nil {
					t.Fatal(err)
				}
			}
			if callbackCtx.Err() != nil {
				t.Fatal("runtime/barrier caller canceled the independent callback context")
			}
			closeCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			err := d.Close(closeCtx)
			var incomplete *CloseError
			if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &incomplete) {
				t.Fatalf("bounded close = %v", err)
			}
			status := incomplete.Status
			if status.Accepted != count || status.Delivered != 0 || status.Remaining != count || status.Queued != count-1 || !status.InFlight || status.Stopped {
				t.Fatalf("hung callback falsely drained: %+v", status)
			}
			awaitSignal(t, ctx, callbackCtx.Done(), "Close callback cancellation")
			if err := d.Append(count+1, count+1); !errors.Is(err, ErrClosed) {
				t.Fatalf("append after timed-out close = %v", err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			release()
			if err := d.Close(ctx); err != nil {
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
			if status := d.Status(); !status.Stopped || status.Remaining != 0 || status.Delivered != count {
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

func (b heldSetupBinding) Defer(label string, dispose func(context.Context) error) error {
	if b.after {
		if err := b.nativeBinding.Defer(label, dispose); err != nil {
			return err
		}
	}
	close(b.entered)
	<-b.gate
	if b.after {
		return nil
	}
	return b.nativeBinding.Defer(label, dispose)
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
					d := testDispatcher[int](t, nil)
					cleaned := make(chan struct{})
					if err := owner.Defer("native resource", func(context.Context) error { close(cleaned); return nil }); err != nil {
						t.Fatal(err)
					}
					gate, release := releaseGate()
					defer release()
					entered, result := make(chan struct{}), make(chan error, 1)
					var calls atomic.Int64
					go func() {
						_, err := d.AddObserver(heldSetupBinding{nativeBinding: owner, after: after, entered: entered, gate: gate}, "setup race", func(context.Context, int) error { calls.Add(1); return nil })
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
						if !after && !errors.Is(err, cc.ErrInactive) {
							t.Fatalf("quiesced native Defer result = %v", err)
						}
						if after && err != nil {
							t.Fatalf("already-registered effect publication = %v", err)
						}
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					if err := d.Append(1, 1); err != nil {
						t.Fatal(err)
					}
					if err := d.Barrier(ctx); err != nil {
						t.Fatal(err)
					}
					if err := r.Settle(ctx); err != nil {
						t.Fatal(err)
					}
					awaitSignal(t, ctx, cleaned, "setup owner actual cleanup")
					if calls.Load() != 0 || d.Status().Observers != 0 {
						t.Fatalf("quiesced setup published callable/leaked observer: calls=%d status=%+v", calls.Load(), d.Status())
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
			d := testDispatcher[int](t, nil)
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
						dispose, err := d.AddObserver(owner, "racing", func(context.Context, int) error { calls.Add(1); return nil })
						if err != nil {
							if errors.Is(err, ErrInactive) || errors.Is(err, cc.ErrInactive) {
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
					if err := d.Append(sequence, int(sequence)); err != nil {
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
			if err := d.Barrier(ctx); err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			before := calls.Load()
			if err := d.Append(301, 301); err != nil {
				t.Fatal(err)
			}
			if err := d.Barrier(ctx); err != nil {
				t.Fatal(err)
			}
			if status := d.Status(); calls.Load() != before || status.Observers != 0 || status.Delivered != 301 {
				t.Fatalf("disposed callback remained callable: before=%d after=%d status=%+v", before, calls.Load(), status)
			}
		})
	}
}
