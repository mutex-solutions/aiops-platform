// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var overflowCandidates = []struct {
	name string
	make func(func(*coreConfig)) Runtime
}{
	{"reconciler", func(mod func(*coreConfig)) Runtime { return newReconcilerWith(mod) }},
}

// overflowRig is a recording runtime with an explicit observer bound. Unlike
// nativeObserverTestRuntime its cleanup tolerates the overflow error Close reports.
type overflowRig struct {
	t    *testing.T
	ctx  context.Context
	r    Runtime
	obs  OverflowObserverRuntime
	root *Handle
}

func newOverflowRig(t *testing.T, make func(func(*coreConfig)) Runtime, backlog int) *overflowRig {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	r := make(func(c *coreConfig) { c.observerQueue = backlog })
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer closeCancel()
		_ = r.Close(closeCtx)
	})
	root, err := r.(RootProvider).RootContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return &overflowRig{t: t, ctx: ctx, r: r, obs: r.(OverflowObserverRuntime), root: root}
}

// stalledObserver is a fiber-registered observer that blocks inside its callback
// on the event keyed "stall". With cooperative set it first waits for its owner
// to start quiescing (the native cancellation), then for the gate.
type stalledObserver struct {
	fiber       uint64
	started     chan uint64 // Sequence of the stall event.
	canceled    chan struct{}
	gate        chan struct{}
	release     func()
	calls       atomic.Int64
	terminal    chan *ObserverOverflow
	terminals   atomic.Int64
	cooperative bool
	atStall     int64 // Callbacks made up to and including the stalled one.
}

func (rig *overflowRig) mountStalled(label string, cooperative bool) *stalledObserver {
	rig.t.Helper()
	s := &stalledObserver{started: make(chan uint64, 1), canceled: make(chan struct{}, 1), gate: make(chan struct{}),
		terminal: make(chan *ObserverOverflow, 4), cooperative: cooperative}
	var once sync.Once
	s.release = func() { once.Do(func() { close(s.gate) }) }
	rig.t.Cleanup(s.release)
	id, err := rig.r.Mount(rig.ctx, Spec{ID: label, Apply: func(owner *Handle) error {
		_, err := rig.obs.ObserveEventsWith(owner, label, func(callbackCtx context.Context, event Event) error {
			s.calls.Add(1)
			if markerOf(event) == "stall" {
				s.started <- event.Sequence
				if s.cooperative {
					<-callbackCtx.Done()
					s.canceled <- struct{}{}
				}
				<-s.gate
			}
			return nil
		}, func(_ context.Context, failure *ObserverOverflow) error {
			s.terminals.Add(1)
			s.terminal <- failure
			return nil
		})
		return err
	}})
	if err != nil {
		rig.t.Fatal(err)
	}
	if err := rig.r.Settle(rig.ctx); err != nil {
		rig.t.Fatal(err)
	}
	s.fiber = id
	return s
}

type eventCollector struct {
	mu  sync.Mutex
	got []Event
}

func (c *eventCollector) observe(rig *overflowRig, label string) {
	rig.t.Helper()
	if _, err := rig.obs.ObserveEvents(rig.root, label, func(_ context.Context, event Event) error {
		c.mu.Lock()
		c.got = append(c.got, event)
		c.mu.Unlock()
		return nil
	}); err != nil {
		rig.t.Fatal(err)
	}
}

// checkTail verifies the collector holds every ledger event from its first on,
// in order and unmodified.
func (c *eventCollector) checkTail(rig *overflowRig) {
	rig.t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.got) == 0 {
		rig.t.Fatal("observer received nothing")
	}
	ledger, err := rig.r.Events(rig.ctx)
	if errors.Is(err, ErrClosed) {
		// The ledger is gone after Close; the observer must still be gapless to the last accepted event.
		for i, event := range c.got {
			if event.Sequence != c.got[0].Sequence+uint64(i) {
				rig.t.Fatalf("gap at %d: %d", i, event.Sequence)
			}
		}
		if last := c.got[len(c.got)-1].Sequence; last != rig.obs.ObserverStatus().Accepted {
			rig.t.Fatalf("observer stopped at %d, accepted %d", last, rig.obs.ObserverStatus().Accepted)
		}
		return
	}
	if err != nil {
		rig.t.Fatal(err)
	}
	first := int(c.got[0].Sequence)
	if len(c.got) != len(ledger)-first+1 {
		rig.t.Fatalf("observer received %d events from sequence %d, ledger has %d", len(c.got), first, len(ledger))
	}
	for i, event := range c.got {
		want := ledger[first-1+i]
		if event.Sequence != want.Sequence || event.Kind != want.Kind || event.Key != want.Key || event.Fiber != want.Fiber {
			rig.t.Fatalf("event %d: got %+v, ledger %+v", i, event, want)
		}
	}
}

func (rig *overflowRig) record(key string) {
	rig.t.Helper()
	nativeObserverRecord(rig.t, rig.ctx, rig.r, key)
}

func receiveWithin[T any](t *testing.T, ctx context.Context, ch <-chan T, what string) T {
	t.Helper()
	return nativeObserverReceive(t, ctx, ch)
}

func TestObserverOverflowDetachesOnlyTheStalledObserver(t *testing.T) {
	const backlog = 8
	for _, candidate := range overflowCandidates {
		t.Run(candidate.name, func(t *testing.T) {
			rig := newOverflowRig(t, candidate.make, backlog)
			stalled := rig.mountStalled("stalled", false)
			var keepsUp eventCollector
			keepsUp.observe(rig, "keeps up")
			if got := rig.r.(RetentionReporter).RetentionStatus().ObserverBacklog; got != backlog {
				t.Fatalf("RetentionStatus.ObserverBacklog = %d", got)
			}
			if got := rig.obs.ObserverStatus().Backlog; got != backlog {
				t.Fatalf("ObserverStatus.Backlog = %d", got)
			}
			rig.record("stall")
			stallSeq := receiveWithin(t, rig.ctx, stalled.started, "stall start")
			for i := 1; i <= backlog; i++ { // the backlog-th event trips the bound
				rig.record("burst")
			}
			// The runtime keeps running and settles; the stall is only visible in status.
			if err := rig.r.Settle(rig.ctx); err != nil {
				t.Fatal(err)
			}
			status := rig.obs.ObserverStatus()
			if status.Overflowed != 1 || status.EventsLost != backlog-1 || status.Orphaned != 1 || status.LastOverflow == "" {
				t.Fatalf("overflow not visible in status: %+v", status)
			}
			// Await joins the keeps-up observer's full delivery and reports the loss.
			err := rig.obs.AwaitObservers(rig.ctx)
			var failure *ObserverOverflow
			if !errors.Is(err, ErrObserverOverflow) || !errors.As(err, &failure) {
				t.Fatalf("AwaitObservers = %v", err)
			}
			if failure.Label != "stalled" || failure.Lost != backlog-1 || failure.Through != stallSeq+backlog-1 || failure.Limit != backlog {
				t.Fatalf("overflow detail %+v (stall at %d)", failure, stallSeq)
			}
			if status := rig.obs.ObserverStatus(); status.ObserverErrors != 1 || status.LastObserverError == "" {
				t.Fatalf("overflow not on the failure path: %+v", status)
			}
			keepsUp.checkTail(rig)
			if got := keepsUp.got[len(keepsUp.got)-1].Sequence; got != rig.obs.ObserverStatus().Accepted {
				t.Fatalf("keeps-up observer stopped at %d", got)
			}
			if stalled.terminals.Load() != 0 {
				t.Fatal("terminal signal before the stalled callback returned")
			}
			stalled.release()
			note := receiveWithin(t, rig.ctx, stalled.terminal, "terminal signal")
			if *note != *failure {
				t.Fatalf("terminal %+v differs from reported %+v", note, failure)
			}
			rig.record("after")
			if err := rig.r.Settle(rig.ctx); err != nil {
				t.Fatal(err)
			}
			if err := rig.obs.AwaitObservers(rig.ctx); !errors.Is(err, ErrObserverOverflow) {
				t.Fatalf("AwaitObservers after overflow = %v", err)
			}
			keepsUp.checkTail(rig) // Every event, including the one after the overflow.
			calls := stalled.calls.Load()
			err = rig.r.Close(rig.ctx)
			if !errors.Is(err, ErrObserverOverflow) {
				t.Fatalf("Close did not report the overflow: %v", err)
			}
			final := rig.obs.ObserverStatus()
			if !final.Stopped || final.Orphaned != 0 || final.Queued != 0 || final.Remaining != 0 || final.Overflowed != 1 {
				t.Fatalf("goroutine or queue retained: %+v", final)
			}
			if stalled.calls.Load() != calls || stalled.terminals.Load() != 1 {
				t.Fatalf("detached observer saw further events (%d -> %d) or %d terminals", calls, stalled.calls.Load(), stalled.terminals.Load())
			}
		})
	}
}

func TestObserverJustBelowTheBoundWithSlowProgressLosesNothing(t *testing.T) {
	const backlog = 8
	for _, candidate := range overflowCandidates {
		t.Run(candidate.name, func(t *testing.T) {
			rig := newOverflowRig(t, candidate.make, backlog)
			step := make(chan struct{})
			seen := make(chan Event, 4*backlog)
			if _, err := rig.obs.ObserveEvents(rig.root, "slow", func(_ context.Context, event Event) error {
				<-step
				seen <- event
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			// backlog undelivered events (the first in flight) is the largest allowed.
			for range backlog {
				rig.record("burst")
			}
			if err := rig.r.Settle(rig.ctx); err != nil {
				t.Fatal(err)
			}
			var last uint64
			for i := range backlog {
				step <- struct{}{}
				event := receiveWithin(t, rig.ctx, seen, "slow observer")
				if markerOf(event) != "burst" || (i > 0 && event.Sequence != last+1) {
					t.Fatalf("event %d out of order: %+v after %d", i, event, last)
				}
				last = event.Sequence
			}
			if err := rig.obs.AwaitObservers(rig.ctx); err != nil {
				t.Fatalf("loss below the bound: %v", err)
			}
			if status := rig.obs.ObserverStatus(); status.Overflowed != 0 || status.EventsLost != 0 || status.PeakBacklog < backlog {
				t.Fatalf("status: %+v", status)
			}
			close(step) // later events (including shutdown) pass straight through
			if err := rig.r.Close(rig.ctx); err != nil {
				t.Fatalf("Close without overflow: %v", err)
			}
		})
	}
}

func TestObserverBoundIsTheConfiguredCapacityAndIsReported(t *testing.T) {
	for _, bound := range []int{100, 300} {
		t.Run(strconv.Itoa(bound), func(t *testing.T) {
			r := newReconcilerWith(func(c *coreConfig) { c.observerQueue = bound })
			defer r.Close(context.Background())
			if got := r.RetentionStatus().ObserverBacklog; got != bound {
				t.Fatalf("RetentionStatus.ObserverBacklog = %d, want %d", got, bound)
			}
			if got := r.ObserverStatus().Backlog; got != bound {
				t.Fatalf("ObserverStatus.Backlog = %d, want %d", got, bound)
			}
		})
	}
}

func TestObserverExplicitLargerBoundAbsorbsTheSameStall(t *testing.T) {
	for _, candidate := range overflowCandidates {
		t.Run(candidate.name, func(t *testing.T) {
			rig := newOverflowRig(t, candidate.make, 64)
			stalled := rig.mountStalled("stalled", false)
			rig.record("stall")
			receiveWithin(t, rig.ctx, stalled.started, "stall start")
			for range 20 { // trips a bound of 8, not 64
				rig.record("burst")
			}
			if status := rig.obs.ObserverStatus(); status.Overflowed != 0 {
				t.Fatalf("overflow below the explicit bound: %+v", status)
			}
			stalled.release()
			if err := rig.obs.AwaitObservers(rig.ctx); err != nil {
				t.Fatal(err)
			}
			if got := stalled.calls.Load(); got < 21 {
				t.Fatalf("stalled observer received %d events", got)
			}
		})
	}
}

// Overflow while the observer's fiber is being disposed: the callback holds the
// lease, so disposal waits; the overflow is reported once and no callback
// (terminal included) runs after the fiber is gone.
func TestObserverOverflowRacingFiberDisposal(t *testing.T) {
	const backlog = 16
	for _, candidate := range overflowCandidates {
		t.Run(candidate.name, func(t *testing.T) {
			rig := newOverflowRig(t, candidate.make, backlog)
			stalled := rig.mountStalled("stalled", true)
			var keepsUp eventCollector
			keepsUp.observe(rig, "keeps up")
			rig.record("stall")
			receiveWithin(t, rig.ctx, stalled.started, "stall start")
			stalled.atStall = stalled.calls.Load()
			disposed := make(chan error, 1)
			go func() { disposed <- rig.r.Dispose(rig.ctx, stalled.fiber) }()
			receiveWithin(t, rig.ctx, stalled.canceled, "native cancellation of the callback")
			for range backlog {
				rig.record("burst")
			}
			if status := rig.obs.ObserverStatus(); status.Overflowed != 1 {
				t.Fatalf("overflow during disposal: %+v", status)
			}
			stalled.release()
			if err := receiveWithin(t, rig.ctx, disposed, "dispose"); err != nil {
				t.Fatalf("Dispose = %v", err)
			}
			if err := rig.r.Settle(rig.ctx); err != nil {
				t.Fatal(err)
			}
			if err := rig.obs.AwaitObservers(rig.ctx); !errors.Is(err, ErrObserverOverflow) {
				t.Fatalf("AwaitObservers = %v", err)
			}
			keepsUp.checkTail(rig) // Including the disposal events.
			err := rig.r.Close(rig.ctx)
			if !errors.Is(err, ErrObserverOverflow) {
				t.Fatalf("Close = %v", err)
			}
			if stalled.terminals.Load() != 0 {
				t.Fatal("terminal signal after the observer's fiber was disposed")
			}
			if calls := stalled.calls.Load(); calls != stalled.atStall {
				t.Fatalf("callbacks after the stall: %d, at stall %d", calls, stalled.atStall)
			}
			if status := rig.obs.ObserverStatus(); status.Overflowed != 1 || status.Orphaned != 0 || !status.Stopped {
				t.Fatalf("final: %+v", status)
			}
		})
	}
}

// Overflow and Close. The cooperative callback is released by Close's native
// quiescence; the single overflow is reported by Close; nothing is sent twice
// and no terminal callback runs for an owner that is quiescing. "before": the
// bound trips, then Close starts while the stalled callback is still inside
// (deterministic). "racing": events keep being offered while Close runs, so the
// bound may trip before or after the runtime stops admitting; either outcome must
// leave status, Close and the terminal signal consistent.
func TestObserverOverflowRacingClose(t *testing.T) {
	const backlog = 4
	for _, candidate := range overflowCandidates {
		for _, before := range []bool{true, false} {
			name := candidate.name + "/racing"
			if before {
				name = candidate.name + "/before"
			}
			t.Run(name, func(t *testing.T) {
				rig := newOverflowRig(t, candidate.make, backlog)
				stalled := rig.mountStalled("stalled", true)
				rig.record("stall")
				receiveWithin(t, rig.ctx, stalled.started, "stall start")
				if before {
					for range backlog {
						rig.record("burst")
					}
					if status := rig.obs.ObserverStatus(); status.Overflowed != 1 {
						t.Fatalf("no overflow before Close: %+v", status)
					}
				}
				closed := make(chan error, 1)
				go func() { closed <- rig.r.Close(rig.ctx) }()
				receiveWithin(t, rig.ctx, stalled.canceled, "Close quiescing the callback")
				if !before {
					for range backlog {
						if err := nativeObserverTryRecord(rig.ctx, rig.r, "burst"); err != nil {
							break // The runtime stopped admitting; nothing more to overflow.
						}
					}
				}
				stalled.release()
				err := receiveWithin(t, rig.ctx, closed, "Close")
				status := rig.obs.ObserverStatus()
				if status.Overflowed > 1 || (status.Overflowed == 1) != errors.Is(err, ErrObserverOverflow) || (before && status.Overflowed != 1) {
					t.Fatalf("Close and status disagree: overflowed=%d err=%v", status.Overflowed, err)
				}
				if stalled.terminals.Load() != 0 || status.Orphaned != 0 || !status.Stopped || status.Remaining != 0 {
					t.Fatalf("final: %+v terminals=%d", status, stalled.terminals.Load())
				}
			})
		}
	}
}

func nativeObserverTryRecord(ctx context.Context, r Runtime, key string) error {
	return r.(Recorder).Record(ctx, key, map[string]any{"key": key})
}
