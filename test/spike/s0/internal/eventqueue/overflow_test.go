// SPDX-License-Identifier: Apache-2.0

package eventqueue

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stubOwner is an always-active Binding unless deactivated. These tests cover
// the dispatcher algorithm; the real lease/cleanup proof for native activations
// is in the concurrency package (observer_overflow_test.go).
type stubOwner struct{ inactive atomic.Bool }

func (o *stubOwner) Enter() bool                                     { return !o.inactive.Load() }
func (o *stubOwner) Leave()                                          {}
func (o *stubOwner) Defer(string, func(context.Context) error) error { return nil }

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
	notes   chan *OverflowError
	notices atomic.Int64
}

func addStalled(t *testing.T, d *Dispatcher[int], owner Binding, label string) *stall {
	t.Helper()
	s := &stall{started: make(chan struct{}, 1), gate: make(chan struct{}), notes: make(chan *OverflowError, 4)}
	var once sync.Once
	s.release = func() { once.Do(func() { close(s.gate) }) }
	t.Cleanup(s.release)
	_, err := d.AddObserverWith(owner, label, func(_ context.Context, event int) error {
		s.calls.Add(1)
		if event == 1 {
			s.started <- struct{}{}
			<-s.gate
		}
		return nil
	}, func(_ context.Context, failure *OverflowError) error {
		s.notices.Add(1)
		s.notes <- failure
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func appendRange(t *testing.T, d *Dispatcher[int], from, to int) {
	t.Helper()
	for i := from; i <= to; i++ {
		if err := d.Append(uint64(i), i); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOverflowDetachesOnlyTheStalledObserver(t *testing.T) {
	const limit = 4
	ctx := testCtx(t)
	d := NewWith[int](nil, Options{Backlog: limit})
	owner := &stubOwner{}
	stalled := addStalled(t, d, owner, "stalled")
	var fast recorder
	if _, err := d.AddObserver(owner, "fast", func(_ context.Context, e int) error { fast.add(e); return nil }); err != nil {
		t.Fatal(err)
	}
	if status := d.Status(); status.Backlog != limit {
		t.Fatalf("bound not reported: %+v", status)
	}
	appendRange(t, d, 1, 1)
	receive(t, ctx, stalled.started, "stalled start")
	// Events 2..limit wait behind the stalled callback: limit undelivered, no loss.
	appendRange(t, d, 2, limit)
	if status := d.Status(); status.Overflowed != 0 || status.Observers != 2 || status.PeakBacklog != limit {
		t.Fatalf("overflowed at exactly the bound: %+v", status)
	}
	// The next event finds limit undelivered events and detaches the stalled
	// observer; the producer does not wait for anything.
	appendRange(t, d, limit+1, limit+1)
	status := d.Status()
	if status.Overflowed != 1 || status.EventsLost != limit-1 || status.Observers != 1 || status.Orphaned != 1 {
		t.Fatalf("overflow status: %+v", status)
	}
	// The observer that keeps up gets every event, the in-flight one included
	// (the new worker resumes it at the next listener), while the stall persists.
	if err := d.WaitBarrier(ctx, limit+1); err != nil {
		t.Fatal(err)
	}
	if want := []int{1, 2, 3, 4, 5}; !slices.Equal(fast.all(), want) {
		t.Fatalf("fast observer got %v, want %v", fast.all(), want)
	}
	if stalled.notices.Load() != 0 {
		t.Fatal("terminal signal ran while the callback was still inside")
	}
	status = d.Status()
	if status.ObserverErrors != 1 || status.LastObserverError == "" || status.LastOverflow == "" || status.Queued != 0 {
		t.Fatalf("overflow not on the failure path: %+v", status)
	}
	stalled.release()
	note := receive(t, ctx, stalled.notes, "terminal signal")
	if note.Label != "stalled" || note.Lost != limit-1 || note.Through != limit || note.Limit != limit || !errors.Is(note, ErrOverflow) {
		t.Fatalf("terminal signal: %+v", note)
	}
	appendRange(t, d, limit+2, limit+3)
	if err := d.Barrier(ctx); err != nil {
		t.Fatalf("barrier must not fail on overflow: %v", err)
	}
	err := d.Close(ctx)
	var failure *OverflowError
	if !errors.Is(err, ErrOverflow) || !errors.As(err, &failure) || failure.Label != "stalled" {
		t.Fatalf("Close did not report the overflow: %v", err)
	}
	if again := d.Close(ctx); !errors.Is(again, ErrOverflow) {
		t.Fatalf("second Close = %v", again)
	}
	if want := []int{1, 2, 3, 4, 5, 6, 7}; !slices.Equal(fast.all(), want) {
		t.Fatalf("fast observer got %v, want %v", fast.all(), want)
	}
	if stalled.calls.Load() != 1 || stalled.notices.Load() != 1 {
		t.Fatalf("detached observer called %d times, notified %d times", stalled.calls.Load(), stalled.notices.Load())
	}
	if final := d.Status(); !final.Stopped || final.Orphaned != 0 || final.Overflowed != 1 || final.Remaining != 0 || final.Queued != 0 {
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
	d := NewWith[int](nil, Options{Backlog: limit})
	owner := &stubOwner{}
	step := make(chan struct{})
	seen := make(chan int, limit+2)
	if _, err := d.AddObserver(owner, "slow", func(_ context.Context, e int) error {
		<-step
		seen <- e
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
	if err := d.Barrier(ctx); err != nil {
		t.Fatal(err)
	}
	if status := d.Status(); status.Overflowed != 0 || status.EventsLost != 0 || status.PeakBacklog != limit || status.Observers != 1 {
		t.Fatalf("loss below the bound: %+v", status)
	}
	if err := d.Close(ctx); err != nil {
		t.Fatalf("Close reported a failure without overflow: %v", err)
	}
}

func TestBacklogOptionAndDefaultAreReported(t *testing.T) {
	d := New[int](nil)
	defer d.Close(context.Background())
	if got := d.Status().Backlog; got != DefaultBacklog || DefaultBacklog < 4096 {
		t.Fatalf("default bound %d (const %d)", got, DefaultBacklog)
	}
	big := NewWith[int](nil, Options{Backlog: 3 * DefaultBacklog})
	defer big.Close(context.Background())
	if got := big.Status().Backlog; got != 3*DefaultBacklog {
		t.Fatalf("explicit bound %d", got)
	}
}

func TestExplicitLargerBoundIsHonoured(t *testing.T) {
	ctx := testCtx(t)
	for _, limit := range []int{4, 40} {
		d := NewWith[int](nil, Options{Backlog: limit})
		stalled := addStalled(t, d, &stubOwner{}, "stalled")
		appendRange(t, d, 1, 1)
		receive(t, ctx, stalled.started, "stall")
		appendRange(t, d, 2, 20)
		overflowed := d.Status().Overflowed
		if want := map[int]uint64{4: 1, 40: 0}[limit]; overflowed != want {
			t.Fatalf("limit %d: Overflowed = %d, want %d", limit, overflowed, want)
		}
		stalled.release()
		_ = d.Close(ctx)
	}
}

// The backlog that built up behind a stalled observer is not charged to the
// observer that takes over: the bound counts events accepted since the last
// detachment, and a second observer that stalls in its turn is detached too.
func TestInheritedBacklogIsNotChargedToTheNextObserver(t *testing.T) {
	const limit = 4
	ctx := testCtx(t)
	d := NewWith[int](nil, Options{Backlog: limit})
	owner := &stubOwner{}
	first := addStalled(t, d, owner, "first")
	second := addStalled(t, d, owner, "second")
	appendRange(t, d, 1, 1)
	receive(t, ctx, first.started, "first stall")
	appendRange(t, d, 2, limit+1) // detaches first; second inherits limit queued events
	if status := d.Status(); status.Overflowed != 1 {
		t.Fatalf("expected exactly the first observer detached: %+v", status)
	}
	// The new worker runs second on the resumed event 1, where it stalls.
	receive(t, ctx, second.started, "second stall")
	// limit-1 further events since the detachment: still below the bound.
	appendRange(t, d, limit+2, 2*limit)
	if status := d.Status(); status.Overflowed != 1 || status.Observers != 1 {
		t.Fatalf("inherited backlog was charged to the second observer: %+v", status)
	}
	appendRange(t, d, 2*limit+1, 2*limit+1)
	if status := d.Status(); status.Overflowed != 2 || status.Observers != 0 || status.Orphaned != 2 {
		t.Fatalf("second stalled observer not detached: %+v", status)
	}
	if err := d.WaitBarrier(ctx, 2*limit+1); err != nil {
		t.Fatal(err)
	}
	first.release()
	second.release()
	for _, s := range []*stall{first, second} {
		receive(t, ctx, s.notes, "terminal")
	}
	err := d.Close(ctx)
	if !errors.Is(err, ErrOverflow) {
		t.Fatalf("Close = %v", err)
	}
	if status := d.Status(); status.Overflowed != 2 || status.Orphaned != 0 || !status.Stopped || first.notices.Load() != 1 || second.notices.Load() != 1 {
		t.Fatalf("final: %+v", status)
	}
}

func TestOverflowOfAnInactiveOwnerReportsButSendsNoCallback(t *testing.T) {
	const limit = 3
	ctx := testCtx(t)
	d := NewWith[int](nil, Options{Backlog: limit})
	owner := &stubOwner{}
	stalled := addStalled(t, d, owner, "stalled")
	appendRange(t, d, 1, 1)
	receive(t, ctx, stalled.started, "stall")
	owner.inactive.Store(true) // its activation was disposed while the callback was inside
	appendRange(t, d, 2, limit+1)
	if status := d.Status(); status.Overflowed != 1 {
		t.Fatalf("overflow not reported for a disposing owner: %+v", status)
	}
	stalled.release()
	err := d.Close(ctx)
	if !errors.Is(err, ErrOverflow) {
		t.Fatalf("Close = %v", err)
	}
	if stalled.notices.Load() != 0 || stalled.calls.Load() != 1 {
		t.Fatalf("callback after disposal: calls=%d notices=%d", stalled.calls.Load(), stalled.notices.Load())
	}
}

func TestOverflowAfterManualRemovalAndRacingClose(t *testing.T) {
	const limit = 3
	ctx := testCtx(t)
	d := NewWith[int](nil, Options{Backlog: limit})
	stalled := addStalled(t, d, &stubOwner{}, "stalled")
	var survivor recorder
	if _, err := d.AddObserver(&stubOwner{}, "survivor", func(_ context.Context, e int) error { survivor.add(e); return nil }); err != nil {
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
	if status := d.Status(); status.Overflowed != 0 {
		t.Fatalf("early overflow: %+v", status)
	}
	// Close races the overflow: the overflowing Append and Close run together.
	var wg sync.WaitGroup
	closeErr := make(chan error, 1)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = d.Append(limit+1, limit+1) // may lose the race and see ErrClosed
	}()
	go func() {
		defer wg.Done()
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		closeErr <- d.Close(canceled)
	}()
	wg.Wait()
	<-closeErr
	stalled.release()
	err := d.Close(ctx)
	status := d.Status()
	if status.Overflowed > 1 || (status.Overflowed == 1) != errors.Is(err, ErrOverflow) {
		t.Fatalf("overflow and Close disagree: overflowed=%d err=%v", status.Overflowed, err)
	}
	if stalled.notices.Load() > 1 || status.Orphaned != 0 || !status.Stopped {
		t.Fatalf("final: %+v notices=%d", status, stalled.notices.Load())
	}
	if got := survivor.all(); !slices.Equal(got, []int{1, 2, 3}) && !slices.Equal(got, []int{1, 2, 3, 4}) {
		t.Fatalf("survivor lost events: %v", got)
	}
}
