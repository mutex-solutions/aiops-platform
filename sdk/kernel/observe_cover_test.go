// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
)

// hookOwner is a leaseHolder whose registration step runs a test action, so the
// two orderings of a close or removal racing the registration are deterministic.
type hookOwner struct {
	stubOwner
	onDefer func(dispose disposer) error
}

func (o *hookOwner) tryDefer(_ string, dispose disposer) error { return o.onDefer(dispose) }

func TestObserverRegistrationArgumentAndRaceGuards(t *testing.T) {
	ctx := testCtx(t)
	callback := func(context.Context, ledgerEvent) error { return nil }

	t.Run("nil owner", func(t *testing.T) {
		d := testDispatcher(t, nil)
		if _, err := d.addObserver(nil, "x", callback); !errors.Is(err, errBinding) {
			t.Fatalf("nil owner = %v", err)
		}
	})
	t.Run("closed while registering", func(t *testing.T) {
		d := newDispatcher(nil, dispatcherOptions{Backlog: 8})
		owner := &hookOwner{onDefer: func(disposer) error { return d.close(ctx) }}
		if _, err := d.addObserver(owner, "x", callback); !errors.Is(err, errObserverClosed) {
			t.Fatalf("close during registration = %v", err)
		}
		if status := d.snapshot(); status.Observers != 0 || !status.Stopped {
			t.Fatalf("a refused registration stayed listed: %+v", status)
		}
	})
	t.Run("removed while registering", func(t *testing.T) {
		d := testDispatcher(t, nil)
		owner := &hookOwner{onDefer: func(dispose disposer) error { return dispose(ctx) }}
		if _, err := d.addObserver(owner, "x", callback); !errors.Is(err, errObserverInactive) {
			t.Fatalf("cleanup during registration = %v", err)
		}
		if status := d.snapshot(); status.Observers != 0 {
			t.Fatalf("a removed registration stayed listed: %+v", status)
		}
	})
}

// stallRound registers one observer that blocks on its first event and detaches
// it by overflowing a backlog bound of one. It returns the next free sequence.
func stallRound(t *testing.T, d *dispatcher, owner leaseHolder, label string, seq uint64, started chan struct{}, gate <-chan struct{}, onOverflow overflowHandler) uint64 {
	t.Helper()
	if _, err := d.addObserverWith(owner, label, func(context.Context, ledgerEvent) error {
		started <- struct{}{}
		<-gate
		return nil
	}, onOverflow); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		seq++
		if err := d.append(seq, ledgerEvent{Sequence: seq}); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			<-started
		}
	}
	return seq
}

func TestCloseSummarisesOverflowsBeyondTheRetainedDetails(t *testing.T) {
	d := newDispatcher(nil, dispatcherOptions{Backlog: 1})
	owner := &stubOwner{}
	gate, release := releaseGate()
	defer release()
	started := make(chan struct{}, 1)
	const extra = 3
	var seq uint64
	for i := 0; i < maxOverflowDetails+extra; i++ {
		seq = stallRound(t, d, owner, fmt.Sprintf("o%d", i), seq, started, gate, nil)
	}
	err := d.overflowErr()
	if !errors.Is(err, errObserverOverflow) || !strings.Contains(err.Error(), fmt.Sprintf("%d further observers detached", extra)) {
		t.Fatalf("overflow summary = %v", err)
	}
	if got := strings.Count(err.Error(), "overflowed the backlog bound"); got != maxOverflowDetails {
		t.Fatalf("retained %d overflow details, want %d", got, maxOverflowDetails)
	}
	release()
	if closeErr := d.close(testCtx(t)); !errors.Is(closeErr, errObserverOverflow) {
		t.Fatalf("close = %v", closeErr)
	}
}

func TestBoundedCloseReportsRemainingWorkAndOverflow(t *testing.T) {
	d := newDispatcher(nil, dispatcherOptions{Backlog: 1})
	gate, release := releaseGate()
	defer release()
	started := make(chan struct{}, 1)
	stallRound(t, d, &stubOwner{}, "stalled", 0, started, gate, nil)

	ended, cancel := context.WithCancel(context.Background())
	cancel()
	err := d.close(ended)
	var incomplete *closeError
	if !errors.As(err, &incomplete) || !errors.Is(err, context.Canceled) || !errors.Is(err, errObserverOverflow) {
		t.Fatalf("bounded close = %v", err)
	}
	want := fmt.Sprintf("observer close incomplete: accepted=%d delivered=%d remaining=%d queued=%d in-flight=%t",
		incomplete.Status.Accepted, incomplete.Status.Delivered, incomplete.Status.Remaining, incomplete.Status.Queued, incomplete.Status.InFlight)
	if incomplete.Error() != want || incomplete.Status.Accepted != 2 || incomplete.Status.Stopped {
		t.Fatalf("closeError = %q, status %+v", incomplete.Error(), incomplete.Status)
	}
	release()
	if err := d.close(testCtx(t)); !errors.Is(err, errObserverOverflow) || errors.As(err, &incomplete) {
		t.Fatalf("joined close = %v", err)
	}
}

func TestCloseJoinsAWorkerThatFinishesWhileItWaits(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := newDispatcher(nil, dispatcherOptions{Backlog: 8})
		gate := make(chan struct{})
		entered := make(chan struct{})
		if _, err := d.addObserver(&stubOwner{}, "slow", func(context.Context, ledgerEvent) error {
			close(entered)
			<-gate
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := d.append(1, ledgerEvent{Sequence: 1}); err != nil {
			t.Fatal(err)
		}
		<-entered
		result := make(chan error, 1)
		go func() { result <- d.close(context.Background()) }()
		synctest.Wait() // close is parked until the callback returns.
		select {
		case err := <-result:
			t.Fatalf("close returned before the callback did: %v", err)
		default:
		}
		close(gate)
		synctest.Wait()
		if err := <-result; err != nil {
			t.Fatalf("close = %v", err)
		}
		if status := d.snapshot(); !status.Stopped || status.Delivered != 1 {
			t.Fatalf("status after joined close: %+v", status)
		}
	})
}

// A terminal overflow handler may fail or panic: both are reported like any other
// observer failure and never reach the worker.
func TestOverflowHandlerFailuresAreReported(t *testing.T) {
	for name, handler := range map[string]overflowHandler{
		"error": func(context.Context, *overflowError) error { return errors.New("handler failed") },
		"panic": func(context.Context, *overflowError) error { panic("handler panicked") },
	} {
		t.Run(name, func(t *testing.T) {
			ctx := testCtx(t)
			d := newDispatcher(nil, dispatcherOptions{Backlog: 1})
			gate, release := releaseGate()
			defer release()
			started := make(chan struct{}, 1)
			seq := stallRound(t, d, &stubOwner{}, "stalled", 0, started, gate, handler)
			if err := d.waitBarrier(ctx, seq); err != nil {
				t.Fatal(err)
			}
			if status := d.snapshot(); status.ObserverErrors != 1 {
				t.Fatalf("overflow report: %+v", status)
			}
			release()
			if err := d.close(ctx); !errors.Is(err, errObserverOverflow) {
				t.Fatalf("close = %v", err)
			}
			status := d.snapshot()
			if status.ObserverErrors != 2 || !strings.Contains(status.LastObserverError, "handler "+map[string]string{"error": "failed", "panic": "panicked"}[name]) {
				t.Fatalf("handler failure not reported: %+v", status)
			}
		})
	}
}

type hostileError struct{}

func (hostileError) Error() string { panic("hostile formatter") }

func TestObserverErrorWhoseFormattingPanicsIsCounted(t *testing.T) {
	ctx := testCtx(t)
	var logged atomic.Int64
	d := testDispatcher(t, func(error) { logged.Add(1) })
	if _, err := d.addObserver(&stubOwner{}, "hostile", func(context.Context, ledgerEvent) error { return hostileError{} }); err != nil {
		t.Fatal(err)
	}
	if err := d.append(1, ledgerEvent{Sequence: 1}); err != nil {
		t.Fatal(err)
	}
	if err := d.barrier(ctx); err != nil {
		t.Fatal(err)
	}
	status := d.snapshot()
	if status.ObserverErrors != 1 || status.FormattingPanics != 1 || logged.Load() != 1 ||
		!strings.Contains(status.LastObserverError, "observer error formatting panicked") {
		t.Fatalf("formatting panic: %+v logged=%d", status, logged.Load())
	}
}

// nestedPanic is a panic value whose formatting itself panics with a value that
// cannot be formatted either, which fmt does not contain.
type nestedPanic struct{}

func (nestedPanic) Error() string { panic(hostileError{}) }

func TestLoggerPanicWhoseFormattingPanicsIsCounted(t *testing.T) {
	ctx := testCtx(t)
	d := testDispatcher(t, func(error) { panic(nestedPanic{}) })
	if _, err := d.addObserver(&stubOwner{}, "failing", func(context.Context, ledgerEvent) error { return errors.New("observer failed") }); err != nil {
		t.Fatal(err)
	}
	if err := d.append(1, ledgerEvent{Sequence: 1}); err != nil {
		t.Fatal(err)
	}
	if err := d.barrier(ctx); err != nil {
		t.Fatal(err)
	}
	status := d.snapshot()
	if status.ObserverErrors != 1 || status.LoggerPanics != 1 || status.FormattingPanics != 1 ||
		status.LastLoggerError != "observer error formatting panicked" {
		t.Fatalf("logger failure not contained: %+v", status)
	}
}
