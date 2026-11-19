// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stubOwner is an always-active leaseHolder unless deactivated. These tests cover
// the dispatcher algorithm; the real lease/cleanup proof for native activations
// is in the concurrency package (observer_overflow_test.go).
type stubOwner struct{ inactive atomic.Bool }

func (o *stubOwner) enter() (func(), bool)           { return func() {}, !o.inactive.Load() }
func (o *stubOwner) tryDefer(string, disposer) error { return nil }

type recorder struct {
	mu  sync.Mutex
	got []int
}

func (r *recorder) add(v int) { r.mu.Lock(); r.got = append(r.got, v); r.mu.Unlock() }
func (r *recorder) all() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.got)
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func receive[T any](t *testing.T, ctx context.Context, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-ctx.Done():
		t.Fatalf("%s: %v", what, ctx.Err())
		var zero T
		return zero
	}
}

// stall registers an observer whose callback blocks on event 1 until released.
type stall struct {
	started chan struct{}
	gate    chan struct{}
	release func()
	calls   atomic.Int64
	notes   chan *overflowError
	notices atomic.Int64
}

func addStalled(t *testing.T, d *dispatcher, owner leaseHolder, label string) *stall {
	t.Helper()
	s := &stall{started: make(chan struct{}, 1), gate: make(chan struct{}), notes: make(chan *overflowError, 4)}
	var once sync.Once
	s.release = func() { once.Do(func() { close(s.gate) }) }
	t.Cleanup(s.release)
	_, err := d.addObserverWith(owner, label, func(_ context.Context, event ledgerEvent) error {
		s.calls.Add(1)
		if event.Sequence == 1 {
			s.started <- struct{}{}
			<-s.gate
		}
		return nil
	}, func(_ context.Context, failure *overflowError) error {
		s.notices.Add(1)
		s.notes <- failure
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func appendRange(t *testing.T, d *dispatcher, from, to int) {
	t.Helper()
	for i := from; i <= to; i++ {
		if err := d.append(uint64(i), ledgerEvent{Sequence: uint64(i)}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOverflowDetachesOnlyTheStalledObserver(t *testing.T) {
	const limit = 4
	ctx := testCtx(t)
	d := newDispatcher(nil, dispatcherOptions{Backlog: limit})
	owner := &stubOwner{}
	stalled := addStalled(t, d, owner, "stalled")
	var fast recorder
	if _, err := d.addObserver(owner, "fast", func(_ context.Context, e ledgerEvent) error { fast.add(int(e.Sequence)); return nil }); err != nil {
		t.Fatal(err)
	}
	if status := d.snapshot(); status.Backlog != limit {
		t.Fatalf("bound not reported: %+v", status)
	}
	appendRange(t, d, 1, 1)
	receive(t, ctx, stalled.started, "stalled start")
	// Events 2..limit wait behind the stalled callback: limit undelivered, no loss.
	appendRange(t, d, 2, limit)
	if status := d.snapshot(); status.Overflowed != 0 || status.Observers != 2 || status.PeakBacklog != limit {
		t.Fatalf("overflowed at exactly the bound: %+v", status)
	}
	// The next event finds limit undelivered events and detaches the stalled
	// observer; the producer does not wait for anything.
	appendRange(t, d, limit+1, limit+1)
	status := d.snapshot()
	if status.Overflowed != 1 || status.EventsLost != limit-1 || status.Observers != 1 || status.Orphaned != 1 {
		t.Fatalf("overflow status: %+v", status)
	}
	// The observer that keeps up gets every event, the in-flight one included
	// (the new worker resumes it at the next listener), while the stall persists.
	if err := d.waitBarrier(ctx, limit+1); err != nil {
		t.Fatal(err)
	}
	if want := []int{1, 2, 3, 4, 5}; !slices.Equal(fast.all(), want) {
		t.Fatalf("fast observer got %v, want %v", fast.all(), want)
	}
	if stalled.notices.Load() != 0 {
		t.Fatal("terminal signal ran while the callback was still inside")
	}
	status = d.snapshot()
	if status.ObserverErrors != 1 || status.LastObserverError == "" || status.LastOverflow == "" || status.Queued != 0 {
		t.Fatalf("overflow not on the failure path: %+v", status)
	}
	stalled.release()
	note := receive(t, ctx, stalled.notes, "terminal signal")
	if note.Label != "stalled" || note.Lost != limit-1 || note.Through != limit || note.Limit != limit || !errors.Is(note, errObserverOverflow) {
		t.Fatalf("terminal signal: %+v", note)
	}
	appendRange(t, d, limit+2, limit+3)
	if err := d.barrier(ctx); err != nil {
		t.Fatalf("barrier must not fail on overflow: %v", err)
	}
	err := d.close(ctx)
	var failure *overflowError
	if !errors.Is(err, errObserverOverflow) || !errors.As(err, &failure) || failure.Label != "stalled" {
		t.Fatalf("Close did not report the overflow: %v", err)
	}
	if again := d.close(ctx); !errors.Is(again, errObserverOverflow) {
		t.Fatalf("second Close = %v", again)
	}
	if want := []int{1, 2, 3, 4, 5, 6, 7}; !slices.Equal(fast.all(), want) {
		t.Fatalf("fast observer got %v, want %v", fast.all(), want)
	}
	if stalled.calls.Load() != 1 || stalled.notices.Load() != 1 {
		t.Fatalf("detached observer called %d times, notified %d times", stalled.calls.Load(), stalled.notices.Load())
	}
	if final := d.snapshot(); !final.Stopped || final.Orphaned != 0 || final.Overflowed != 1 || final.Remaining != 0 || final.Queued != 0 {
		t.Fatalf("goroutine or queue retained: %+v", final)
	}
	d.mu.Lock()
	retained := d.queue != nil || d.running != nil || d.active
	d.mu.Unlock()
	if retained {
		t.Fatal("queue storage retained after drain")
	}
}

func TestSlowProgressingObserverBelowTheBoundLosesNothing(t *testing.T) {
	const limit = 6
	ctx := testCtx(t)
	d := newDispatcher(nil, dispatcherOptions{Backlog: limit})
	owner := &stubOwner{}
	step := make(chan struct{})
	seen := make(chan int, limit+2)
	if _, err := d.addObserver(owner, "slow", func(_ context.Context, e ledgerEvent) error {
		<-step
		seen <- int(e.Sequence)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	appendRange(t, d, 1, limit) // exactly limit undelivered, one of them in flight
	for want := 1; want <= limit; want++ {
		step <- struct{}{}
		if got := receive(t, ctx, seen, "slow observer"); got != want {
			t.Fatalf("event %d delivered as %d", want, got)
		}
	}
	if err := d.barrier(ctx); err != nil {
		t.Fatal(err)
	}
	if status := d.snapshot(); status.Overflowed != 0 || status.EventsLost != 0 || status.PeakBacklog != limit || status.Observers != 1 {
		t.Fatalf("loss below the bound: %+v", status)
	}
	if err := d.close(ctx); err != nil {
		t.Fatalf("Close reported a failure without overflow: %v", err)
	}
}

func TestBacklogOptionIsReported(t *testing.T) {
	for _, limit := range []int{7, 3 * 4096} {
		d := newDispatcher(nil, dispatcherOptions{Backlog: limit})
		if got := d.snapshot().Backlog; got != limit {
			t.Fatalf("explicit bound %d, want %d", got, limit)
		}
		_ = d.close(context.Background())
	}
}

func TestExplicitLargerBoundIsHonoured(t *testing.T) {
	ctx := testCtx(t)
	for _, limit := range []int{4, 40} {
		d := newDispatcher(nil, dispatcherOptions{Backlog: limit})
		stalled := addStalled(t, d, &stubOwner{}, "stalled")
		appendRange(t, d, 1, 1)
		receive(t, ctx, stalled.started, "stall")
		appendRange(t, d, 2, 20)
		overflowed := d.snapshot().Overflowed
		if want := map[int]uint64{4: 1, 40: 0}[limit]; overflowed != want {
			t.Fatalf("limit %d: Overflowed = %d, want %d", limit, overflowed, want)
		}
		stalled.release()
		_ = d.close(ctx)
	}
}

// The backlog that built up behind a stalled observer is not charged to the
// observer that takes over: the bound counts events accepted since the last
// detachment, and a second observer that stalls in its turn is detached too.
func TestInheritedBacklogIsNotChargedToTheNextObserver(t *testing.T) {
	const limit = 4
	ctx := testCtx(t)
	d := newDispatcher(nil, dispatcherOptions{Backlog: limit})
	owner := &stubOwner{}
	first := addStalled(t, d, owner, "first")
	second := addStalled(t, d, owner, "second")
	appendRange(t, d, 1, 1)
	receive(t, ctx, first.started, "first stall")
	appendRange(t, d, 2, limit+1) // detaches first; second inherits limit queued events
	if status := d.snapshot(); status.Overflowed != 1 {
		t.Fatalf("expected exactly the first observer detached: %+v", status)
	}
	// The new worker runs second on the resumed event 1, where it stalls.
	receive(t, ctx, second.started, "second stall")
	// limit-1 further events since the detachment: still below the bound.
	appendRange(t, d, limit+2, 2*limit)
	if status := d.snapshot(); status.Overflowed != 1 || status.Observers != 1 {
		t.Fatalf("inherited backlog was charged to the second observer: %+v", status)
	}
	appendRange(t, d, 2*limit+1, 2*limit+1)
	if status := d.snapshot(); status.Overflowed != 2 || status.Observers != 0 || status.Orphaned != 2 {
		t.Fatalf("second stalled observer not detached: %+v", status)
	}
	if err := d.waitBarrier(ctx, 2*limit+1); err != nil {
		t.Fatal(err)
	}
	first.release()
	second.release()
	for _, s := range []*stall{first, second} {
		receive(t, ctx, s.notes, "terminal")
	}
	err := d.close(ctx)
	if !errors.Is(err, errObserverOverflow) {
		t.Fatalf("Close = %v", err)
	}
	if status := d.snapshot(); status.Overflowed != 2 || status.Orphaned != 0 || !status.Stopped || first.notices.Load() != 1 || second.notices.Load() != 1 {
		t.Fatalf("final: %+v", status)
	}
}

// The rule blames whichever observer the worker is inside when the bound is
// crossed. An observer that is not at fault, merely still draining the backlog it
// inherited from a stalled one, is detached once limit further events have been
// accepted since the detachment and it has not yet delivered past them. This
// documents the rule (ADR-034 "Unfairness that remains"); it is why a test that
// needs an observer to keep up must wait for it before producing more events.
func TestObserverStillDrainingInheritedBacklogIsDetachedOnItsOwnBacklog(t *testing.T) {
	const limit = 4
	ctx := testCtx(t)
	d := newDispatcher(nil, dispatcherOptions{Backlog: limit})
	owner := &stubOwner{}
	stalled := addStalled(t, d, owner, "stalled")
	gate := make(chan struct{}) // holds the healthy observer inside its first callback
	started := make(chan struct{}, 1)
	var release sync.Once
	open := func() { release.Do(func() { close(gate) }) }
	t.Cleanup(open)
	var healthy recorder
	if _, err := d.addObserver(owner, "healthy", func(_ context.Context, e ledgerEvent) error {
		if e.Sequence == 1 {
			started <- struct{}{}
			<-gate
		}
		healthy.add(int(e.Sequence))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	appendRange(t, d, 1, 1)
	receive(t, ctx, stalled.started, "stall")
	appendRange(t, d, 2, limit+1) // detaches the stalled observer
	receive(t, ctx, started, "healthy observer reached the resumed event")
	appendRange(t, d, limit+2, 2*limit) // below the bound since the detachment
	if status := d.snapshot(); status.Overflowed != 1 {
		t.Fatalf("healthy observer detached below its own bound: %+v", status)
	}
	appendRange(t, d, 2*limit+1, 2*limit+1) // limit events since the detachment, none delivered
	status := d.snapshot()
	if status.Overflowed != 2 || status.Observers != 0 {
		t.Fatalf("healthy observer behind the stalled one not detached: %+v", status)
	}
	open()
	stalled.release()
	if err := d.close(ctx); !errors.Is(err, errObserverOverflow) {
		t.Fatalf("Close = %v", err)
	}
	if got := healthy.all(); !slices.Equal(got, []int{1}) {
		t.Fatalf("healthy observer got %v, want only the event it was inside", got)
	}
}

func TestOverflowOfAnInactiveOwnerReportsButSendsNoCallback(t *testing.T) {
	const limit = 3
	ctx := testCtx(t)
	d := newDispatcher(nil, dispatcherOptions{Backlog: limit})
	owner := &stubOwner{}
	stalled := addStalled(t, d, owner, "stalled")
	appendRange(t, d, 1, 1)
	receive(t, ctx, stalled.started, "stall")
	owner.inactive.Store(true) // its activation was disposed while the callback was inside
	appendRange(t, d, 2, limit+1)
	if status := d.snapshot(); status.Overflowed != 1 {
		t.Fatalf("overflow not reported for a disposing owner: %+v", status)
	}
	stalled.release()
	err := d.close(ctx)
	if !errors.Is(err, errObserverOverflow) {
		t.Fatalf("Close = %v", err)
	}
	if stalled.notices.Load() != 0 || stalled.calls.Load() != 1 {
		t.Fatalf("callback after disposal: calls=%d notices=%d", stalled.calls.Load(), stalled.notices.Load())
	}
}

func TestOverflowAfterManualRemovalAndRacingClose(t *testing.T) {
	const limit = 3
	ctx := testCtx(t)
	d := newDispatcher(nil, dispatcherOptions{Backlog: limit})
	stalled := addStalled(t, d, &stubOwner{}, "stalled")
	var survivor recorder
	if _, err := d.addObserver(&stubOwner{}, "survivor", func(_ context.Context, e ledgerEvent) error { survivor.add(int(e.Sequence)); return nil }); err != nil {
		t.Fatal(err)
	}
	appendRange(t, d, 1, 1)
	receive(t, ctx, stalled.started, "stall")
	// Manual removal first: the in-flight callback is still the stall, and later
	// events (which do not capture it) still make the bound trip.
	d.mu.Lock()
	r := d.running
	d.mu.Unlock()
	d.mu.Lock()
	d.removeLocked(r)
	d.mu.Unlock()
	appendRange(t, d, 2, limit)
	if status := d.snapshot(); status.Overflowed != 0 {
		t.Fatalf("early overflow: %+v", status)
	}
	// Close races the overflow: the overflowing Append and Close run together.
	var wg sync.WaitGroup
	closeErr := make(chan error, 1)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = d.append(limit+1, ledgerEvent{Sequence: limit + 1}) // may lose the race and see ErrClosed
	}()
	go func() {
		defer wg.Done()
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		closeErr <- d.close(canceled)
	}()
	wg.Wait()
	<-closeErr
	stalled.release()
	err := d.close(ctx)
	status := d.snapshot()
	if status.Overflowed > 1 || (status.Overflowed == 1) != errors.Is(err, errObserverOverflow) {
		t.Fatalf("overflow and Close disagree: overflowed=%d err=%v", status.Overflowed, err)
	}
	if stalled.notices.Load() > 1 || status.Orphaned != 0 || !status.Stopped {
		t.Fatalf("final: %+v notices=%d", status, stalled.notices.Load())
	}
	if got := survivor.all(); !slices.Equal(got, []int{1, 2, 3}) && !slices.Equal(got, []int{1, 2, 3, 4}) {
		t.Fatalf("survivor lost events: %v", got)
	}
}

func TestAppendRejectsSequenceWraparoundWithoutQueueMutation(t *testing.T) {
	// A stopped worker makes this arithmetic-only boundary deterministic without
	// synthesizing 2^64 source events. This is not a fabricated lifecycle host.
	d := newDispatcher(nil, dispatcherOptions{Backlog: 1 << 16})
	if err := d.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	d.status.Closing = false
	d.status.Accepted, d.status.Delivered = ^uint64(0), ^uint64(0)
	d.mu.Unlock()
	for _, sequence := range []uint64{0, 1, ^uint64(0)} {
		if err := d.append(sequence, ledgerEvent{}); !errors.Is(err, errSequenceGap) {
			t.Fatalf("wraparound sequence %d = %v", sequence, err)
		}
	}
	status := d.snapshot()
	if status.Accepted != ^uint64(0) || status.Delivered != ^uint64(0) || status.Queued != 0 {
		t.Fatalf("wraparound rejection changed state: %+v", status)
	}
}

func TestPopClearsReferencesAndReleasesDrainedBackingStorage(t *testing.T) {
	// Keep queue storage deterministic until the one real worker starts.
	// These are algorithm-only envelopes with no listeners or lifecycle binding.
	// All lifecycle lease/cleanup proof remains in observers' real-backend tests.
	const count = 2048
	d := &dispatcher{
		progress: make(chan struct{}), wake: make(chan struct{}, 1), done: make(chan struct{}),
		status: observerStatus{Accepted: count, Closing: true},
	}
	d.queue = make([]envelope, count)
	for index := range d.queue {
		value := index
		d.queue[index] = envelope{sequence: uint64(index + 1), event: ledgerEvent{Sequence: uint64(index + 1), Value: &value}}
	}
	backing := d.queue
	go d.run()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case <-d.done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	d.mu.Lock()
	retained := d.queue != nil || d.head != 0
	d.mu.Unlock()
	if retained {
		t.Fatal("drained queue retained event backing storage")
	}
	for index, event := range backing {
		if event.event.Value != nil || event.listeners != nil || event.sequence != 0 {
			t.Fatalf("popped slot %d retained an envelope reference", index)
		}
	}
	if status := d.snapshot(); status.Delivered != count || status.Remaining != 0 || !status.Stopped {
		t.Fatalf("actual algorithm drain = %+v", status)
	}
}
