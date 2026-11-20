// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// Tests of the leak rule of S1 (ADR-059 decision 13): a drain past its deadline marks
// the fiber leaked as a disposer past its deadline does, the kernel logs the cause of a
// failed tracked goroutine and of a leak, and the observer of the root receives the
// events of Close.

// A tracked goroutine that is still running when the drain deadline falls due marks
// the fiber leaked, in the snapshot and in the Settle diagnostic, and the mark
// outlives the goroutine.
func TestDrainPastTheDeadlineMarksTheFiberLeaked(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewReconciler().(*rt)
		ctx := context.Background()
		gate := make(chan struct{})
		id, err := r.Mount(ctx, Spec{ID: "leaker", Apply: func(c *Handle) error {
			return c.Go("stuck", func(context.Context) error { <-gate; return nil })
		}})
		if err != nil {
			t.Fatal(err)
		}
		bystander, err := r.Mount(ctx, Spec{ID: "bystander"})
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if err := r.Dispose(ctx, id); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if s := snapshotOf(t, r, id); s.Leaked {
			t.Fatal("marked leaked before the drain deadline")
		}
		time.Sleep(testDrain + time.Second)
		synctest.Wait()
		err = r.Settle(ctx)
		if !errors.Is(err, ErrLeaked) || !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "fiber 1 drain deadline") {
			t.Fatalf("Settle = %v, want the drain deadline marked leaked", err)
		}
		if s := snapshotOf(t, r, id); !s.Leaked || !strings.Contains(s.Error, "leaked") {
			t.Fatalf("snapshot = %+v, want Leaked and its diagnostic", s)
		}
		if s := snapshotOf(t, r, bystander); s.Leaked {
			t.Fatal("an unrelated fiber was marked leaked")
		}
		close(gate)
		synctest.Wait()
		if err := r.Settle(ctx); err != nil {
			t.Fatalf("Settle after the goroutine returned = %v", err)
		}
		if s := snapshotOf(t, r, id); s.State != Disposed || !s.Leaked {
			t.Fatalf("archived snapshot = %+v, want DISPOSED and still Leaked", s)
		}
		if err := closeRuntime(ctx, r); err != nil {
			t.Fatal(err)
		}
	})
}

// A lease that is still held at the drain deadline is a leak too, and one that is
// released in time is not.
func TestDrainLeaseAndInTimeRelease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewReconciler().(*rt)
		ctx := context.Background()
		var held, quick *Handle
		stuck, err := r.Mount(ctx, Spec{ID: "stuck", Apply: func(c *Handle) error { held = c; return nil }})
		if err != nil {
			t.Fatal(err)
		}
		prompt, err := r.Mount(ctx, Spec{ID: "prompt", Apply: func(c *Handle) error { quick = c; return nil }})
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if !held.Enter() || !quick.Enter() {
			t.Fatal("no lease")
		}
		if err := r.Dispose(ctx, stuck); err != nil {
			t.Fatal(err)
		}
		if err := r.Dispose(ctx, prompt); err != nil {
			t.Fatal(err)
		}
		time.Sleep(testDrain - time.Second)
		quick.Leave()
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if s := snapshotOf(t, r, stuck); !s.Leaked {
			t.Fatalf("a lease held past the drain deadline did not leak: %+v", s)
		}
		if s := snapshotOf(t, r, prompt); s.Leaked || s.State != Disposed {
			t.Fatalf("a lease released in time was marked leaked: %+v", s)
		}
		held.Leave()
		synctest.Wait()
		if err := closeRuntime(ctx, r); err != nil {
			t.Fatal(err)
		}
	})
}

// leakLog is a slog.Handler that keeps its records.
type leakLog struct {
	mu      sync.Mutex
	records []map[string]string
	panics  bool
}

func (h *leakLog) Enabled(context.Context, slog.Level) bool { return true }
func (h *leakLog) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *leakLog) WithGroup(string) slog.Handler            { return h }
func (h *leakLog) Handle(_ context.Context, r slog.Record) error {
	if h.panics {
		panic("handler panics")
	}
	rec := map[string]string{"msg": r.Message, "level": r.Level.String()}
	r.Attrs(func(a slog.Attr) bool { rec[a.Key] = a.Value.String(); return true })
	h.mu.Lock()
	h.records = append(h.records, rec)
	h.mu.Unlock()
	return nil
}

func (h *leakLog) all() []map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.records)
}

// A tracked goroutine that returns an error or panics leaves one record with the
// fiber, the plugin, the label and the error text; one that ends because its context
// was cancelled leaves none.
func TestTrackedFailureIsLogged(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := &leakLog{}
		r := newReconcilerWith(func(c *coreConfig) { c.logHandler = h })
		ctx := context.Background()
		failing, err := r.Mount(ctx, Spec{ID: "failing", Apply: func(c *Handle) error {
			return c.Go("worker", func(context.Context) error { return errors.New("boom") })
		}})
		if err != nil {
			t.Fatal(err)
		}
		panicking, err := r.Mount(ctx, Spec{ID: "panicking", Apply: func(c *Handle) error {
			return c.Go("panicker", func(context.Context) error { panic("kaboom") })
		}})
		if err != nil {
			t.Fatal(err)
		}
		calm, err := r.Mount(ctx, Spec{ID: "calm", Apply: func(c *Handle) error {
			return c.Go("quiet", func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })
		}})
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if err := r.Dispose(ctx, calm); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		_ = r.Settle(ctx)
		got := h.all()
		if len(got) != 2 {
			t.Fatalf("records = %v, want one for each failing goroutine", got)
		}
		byPlugin := map[string]map[string]string{}
		for _, rec := range got {
			byPlugin[rec["plugin"]] = rec
		}
		if rec := byPlugin["failing"]; rec["msg"] != "tracked goroutine failed" || rec["level"] != "ERROR" ||
			rec["label"] != "worker" || rec["error"] != "boom" || rec["fiber"] != "1" {
			t.Errorf("record of the error = %v", rec)
		}
		if rec := byPlugin["panicking"]; rec["label"] != "panicker" || rec["fiber"] != "2" ||
			!strings.Contains(rec["error"], "panic: kaboom") {
			t.Errorf("record of the panic = %v (fibers %d, %d)", rec, failing, panicking)
		}
		_ = closeRuntime(ctx, r) // the failed fibers are its diagnostic
	})
}

// A fiber marked leaked, for a drain and for a disposer, leaves one record naming the
// label of what still runs, the tracked goroutines past the drain deadline or the
// disposer past the dispose deadline, with the stage in its error (08 §1.6, "Logged
// with its label"). A drain held by a lease alone has no label and names its stage.
func TestLeakIsLogged(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := &leakLog{}
		r := newReconcilerWith(func(c *coreConfig) { c.logHandler = h })
		ctx := context.Background()
		gate := make(chan struct{})
		var held *Handle
		leased, err := r.Mount(ctx, Spec{ID: "leased", Apply: func(c *Handle) error { held = c; return nil }})
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if !held.Enter() {
			t.Fatal("no lease")
		}
		drain, err := r.Mount(ctx, Spec{ID: "drainer", Apply: func(c *Handle) error {
			if err := c.Go("returned", func(context.Context) error { return nil }); err != nil {
				return err
			}
			if err := c.Go("leaky-b", func(context.Context) error { <-gate; return nil }); err != nil {
				return err
			}
			return c.Go("leaky-a", func(context.Context) error { <-gate; return nil })
		}})
		if err != nil {
			t.Fatal(err)
		}
		dispose, err := r.Mount(ctx, Spec{ID: "disposer", Apply: func(c *Handle) error {
			if err := c.Defer("quick", func(context.Context) error { return nil }); err != nil {
				return err
			}
			return c.Defer("slow-dispose", func(context.Context) error { <-gate; return nil })
		}})
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		_ = r.Dispose(ctx, drain)
		_ = r.Dispose(ctx, dispose)
		_ = r.Dispose(ctx, leased)
		time.Sleep(testDrain + testDispose + time.Second)
		synctest.Wait()
		got := h.all()
		byPlugin := map[string]map[string]string{}
		for _, rec := range got {
			byPlugin[rec["plugin"]] = rec
		}
		if len(got) != 3 || byPlugin["drainer"]["label"] != "leaky-a,leaky-b" || byPlugin["disposer"]["label"] != "slow-dispose" ||
			byPlugin["leased"]["label"] != "drain" {
			t.Errorf("records = %v, want one for each leak, each with its label", got)
		}
		if rec := byPlugin["drainer"]; rec["msg"] != "fiber marked leaked" || rec["level"] != "ERROR" || rec["fiber"] != "2" ||
			!strings.Contains(rec["error"], "fiber 2 drain deadline") || !strings.Contains(rec["error"], "fiber leaked") {
			t.Errorf("record of the drain = %v", rec)
		}
		if rec := byPlugin["disposer"]; !strings.Contains(rec["error"], "fiber 3 dispose deadline") {
			t.Errorf("record of the disposer = %v", rec)
		}
		close(gate)
		held.Leave()
		synctest.Wait()
		if err := closeRuntime(ctx, r); err != nil {
			t.Fatal(err)
		}
	})
}

func TestLogHandlerPanicIsContained(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := &leakLog{panics: true}
		r := newReconcilerWith(func(c *coreConfig) { c.logHandler = h })
		ctx := context.Background()
		if _, err := r.Mount(ctx, Spec{ID: "failing", Apply: func(c *Handle) error {
			return c.Go("worker", func(context.Context) error { return errors.New("boom") })
		}}); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if err := r.Settle(ctx); err != nil && !strings.Contains(err.Error(), "boom") {
			t.Fatalf("Settle = %v", err)
		}
		_ = closeRuntime(ctx, r)
	})
}

// The observer of the root receives every event accepted before Close completes,
// the deadline of a leaking fiber among them: the root quiesces when Close begins,
// and its lease no longer decides.
func TestObserverOfTheRootSeesTheEventsOfClose(t *testing.T) {
	cfg := frontConfig()
	cfg.DisposeDeadline = 30 * time.Millisecond
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var seq []uint64
	var leaked bool
	sawLeak := make(chan struct{})
	if _, err := r.Observe("watch", func(_ context.Context, e LedgerEvent) error {
		mu.Lock()
		defer mu.Unlock()
		seq = append(seq, e.Sequence)
		if e.Kind == "deadline" && e.Label == "dispose" && !leaked {
			leaked = true
			close(sawLeak)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	r.Plugin(Define(Plugin{ID: "slow", Apply: func(c *Context, _ any) error {
		c.Defer("stuck", func(context.Context) error { <-gate; return nil })
		return nil
	}}), nil)
	if err := r.Settle(frontCtx(t)); err != nil {
		t.Fatal(err)
	}
	closed := make(chan error, 1)
	go func() { closed <- r.Close(context.Background()) }()
	select {
	case <-sawLeak:
	case <-time.After(10 * time.Second):
		t.Fatal("the observer of the root did not see the deadline of the leaking fiber during Close")
	}
	// Close returns the diagnostic of the leak while the disposer still runs: every event
	// accepted until then has been delivered.
	if err := <-closed; !errors.Is(err, ErrLeaked) {
		t.Fatalf("Close = %v, want ErrLeaked", err)
	}
	st := r.ObserverStatus()
	mu.Lock()
	seen := slices.Clone(seq)
	mu.Unlock()
	if st.Accepted != st.Delivered || st.Remaining != 0 || st.Skipped != 0 || st.EventsLost != 0 {
		t.Fatalf("status after Close = %+v, want every accepted event delivered and none skipped", st)
	}
	if len(seen) == 0 || seen[len(seen)-1] != st.Accepted {
		t.Fatalf("the observer saw %d events, the dispatcher accepted %d", len(seen), st.Accepted)
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] != seen[i-1]+1 {
			t.Fatalf("the observer saw a gap at index %d: %v", i, seen[i-1:i+1])
		}
	}
	close(gate)
	// The disposer returns: Close now ends with no diagnostic.
	for end := time.Now().Add(10 * time.Second); r.Close(context.Background()) != nil; {
		if time.Now().After(end) {
			t.Fatal("Close did not complete after the disposer returned")
		}
		time.Sleep(time.Millisecond)
	}
}
