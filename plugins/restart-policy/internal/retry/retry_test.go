// SPDX-License-Identifier: Apache-2.0

package retry

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	clockfake "github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0/fake"
	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	compfake "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0/fake"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	launchfake "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// registry is a Fibers that holds what the test puts in it.
type registry struct {
	mu sync.Mutex
	m  map[uint64]*kernel.Fiber
}

func (r *registry) Fiber(id uint64) (*kernel.Fiber, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.m[id]
	return f, ok
}

func (r *registry) put(f *kernel.Fiber) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[f.ID()] = f
	return f.ID()
}

func (r *registry) drop(id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, id)
}

type tap struct{ ch chan string }

func (h tap) Enabled(context.Context, slog.Level) bool { return true }
func (h tap) Handle(_ context.Context, r slog.Record) error {
	select {
	case h.ch <- r.Message:
	default:
	}
	return nil
}
func (h tap) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h tap) WithGroup(string) slog.Handler      { return h }

type rig struct {
	t    *testing.T
	root *kernel.Root
	r    *Retrier
	reg  *registry
	clk  *clockfake.Clock
	comp *compfake.Reader
	logs chan string
}

func newRig(t *testing.T, p Policy) *rig {
	t.Helper()
	g := &rig{t: t, root: kerneltest.New(t), reg: &registry{m: map[uint64]*kernel.Fiber{}}, logs: make(chan string, 256)}
	var kp, cp kernel.Plugin
	g.clk, kp = clockfake.New(time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC))
	g.comp, cp = compfake.New()
	var c *kernel.Context
	holder := kernel.Define(kernel.Plugin{ID: "aiops/holder", Apply: func(hc *kernel.Context, _ any) error { c = hc; return nil }})
	kerneltest.Mount(g.root, kp, cp, holder)
	l, _ := launchfake.New()
	l.Modules = []launch.CatalogEntry{{ID: "aiops/sql", Provides: []string{"sql"}}}
	rows, ok := any(g.comp).(composition.Reader)
	if !ok {
		t.Fatal("not a reader")
	}
	g.r = New(p, Deps{Context: c, Clock: g.clk, Fibers: g.reg, Catalog: l, Exit: l, Rows: rows,
		Log: slog.New(tap{g.logs})})
	return g
}

var policy = Policy{MaxFailures: 5, Window: time.Hour, BackoffMin: time.Second, BackoffMax: time.Minute, Factor: 2,
	Infrastructure: []string{"sql"}}

// failing mounts a row that is FAILED, active mounts one that is ACTIVE.
func (g *rig) failing() *kernel.Fiber {
	g.t.Helper()
	f := g.root.Plugin(kernel.Define(kernel.Plugin{ID: "aiops/f",
		Apply: func(*kernel.Context, any) error { return errors.New("no") }}), nil)
	_ = f.Await(g.t.Context())
	return f
}

func (g *rig) active() *kernel.Fiber {
	g.t.Helper()
	f := g.root.Plugin(kernel.Define(kernel.Plugin{ID: "aiops/a", Apply: func(*kernel.Context, any) error { return nil }}), nil)
	if err := f.Await(g.t.Context()); err != nil {
		g.t.Fatal(err)
	}
	return f
}

func (g *rig) waitLog(msg string) {
	g.t.Helper()
	for {
		select {
		case m := <-g.logs:
			if m == msg {
				return
			}
		case <-g.t.Context().Done():
			g.t.Fatalf("no %q was logged", msg)
		}
	}
}

func (g *rig) tracked() int {
	g.r.mu.Lock()
	defer g.r.mu.Unlock()
	return len(g.r.recs)
}

func failedEvent(id uint64) kernel.LedgerEvent {
	return kernel.LedgerEvent{Kind: "state", Fiber: id, Plugin: "aiops/f", State: kernel.Failed}
}

func TestEventForAFiberTheRegistryDoesNotKnowIsForgotten(t *testing.T) {
	g := newRig(t, policy)
	g.r.Status(t.Context(), failedEvent(77))
	if g.tracked() != 0 || g.clk.Pending() != 0 {
		t.Errorf("tracked = %d, timers = %d", g.tracked(), g.clk.Pending())
	}
}

func TestRepeatedFailureEventSchedulesOneRetry(t *testing.T) {
	g := newRig(t, policy)
	id := g.reg.put(g.failing())
	g.r.Status(t.Context(), failedEvent(id))
	g.r.Status(t.Context(), failedEvent(id)) // as a resync that crossed a live event gives
	if g.clk.Pending() != 1 {
		t.Errorf("timers = %d, want 1", g.clk.Pending())
	}
	if n := len(g.r.recs[id].fails); n != 1 {
		t.Errorf("failures counted = %d, want 1", n)
	}
}

func TestStaleFailureEventOfAFiberThatWorksIsIgnored(t *testing.T) {
	g := newRig(t, policy)
	id := g.reg.put(g.active())
	g.r.Status(t.Context(), failedEvent(id))
	if g.tracked() != 0 || g.clk.Pending() != 0 {
		t.Errorf("tracked = %d, timers = %d", g.tracked(), g.clk.Pending())
	}
}

func TestRetryOfAFiberThatLeftTheRegistry(t *testing.T) {
	g := newRig(t, policy)
	id := g.reg.put(g.failing())
	g.r.Status(t.Context(), failedEvent(id))
	g.reg.drop(id) // evicted: the record of a long disposed fiber
	g.clk.Advance(time.Second)
	g.waitLog("fiber.retry-skipped")
	if g.tracked() != 0 {
		t.Error("the record of a fiber that is gone was kept")
	}
}

func TestRetryUpdatesAFailedFiber(t *testing.T) {
	g := newRig(t, policy)
	f := g.failing()
	id := g.reg.put(f)
	g.r.Status(t.Context(), failedEvent(id))
	g.clk.Advance(time.Second)
	g.waitLog("fiber.retried")
}

// A timer that falls due before the event of someone else's restart is heard finds a
// fiber that is not FAILED and does nothing.
func TestRetryOfAFiberThatIsNoLongerFailed(t *testing.T) {
	g := newRig(t, policy)
	fixed := false
	f := g.root.Plugin(kernel.Define(kernel.Plugin{ID: "aiops/f", Apply: func(*kernel.Context, any) error {
		if !fixed {
			return errors.New("no")
		}
		return nil
	}}), nil)
	_ = f.Await(t.Context())
	id := g.reg.put(f)
	g.r.Status(t.Context(), failedEvent(id))
	fixed = true
	if err := f.Update(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	g.clk.Advance(time.Second)
	g.waitLog("fiber.retry-skipped")
	g.r.mu.Lock()
	defer g.r.mu.Unlock()
	if rec := g.r.recs[id]; rec == nil || rec.pending || rec.retried {
		t.Errorf("record = %+v: the skipped retry must clear the wait and update nothing", rec)
	}
}

// A fiber that leaves FAILED through restart-policy's own update keeps its record;
// one that someone else updates, or restarts while it is live, is tracked afresh; one
// that returns from PENDING keeps its count.
func TestRestartsBySomeoneElse(t *testing.T) {
	g := newRig(t, policy)
	ev := func(id uint64, from, to kernel.State) {
		g.r.Status(t.Context(), kernel.LedgerEvent{Kind: "state", Fiber: id, From: from, State: to})
	}
	g.r.recs[1] = &record{retried: true}
	g.r.recs[2] = &record{}
	g.r.recs[3] = &record{}
	g.r.recs[4] = &record{}
	ev(1, kernel.Failed, kernel.Loading)    // its own retry
	ev(2, kernel.Failed, kernel.Pending)    // an operator, a rollback
	ev(3, kernel.Unloading, kernel.Loading) // a candidate config of a live fiber
	ev(4, kernel.Pending, kernel.Loading)   // its provider returned
	if rec := g.r.recs[1]; rec == nil || rec.retried {
		t.Errorf("own retry: record = %+v", rec)
	}
	if g.r.recs[2] != nil || g.r.recs[3] != nil || g.r.recs[4] == nil {
		t.Errorf("records = %v", g.r.recs)
	}
}

func TestOtherEventsOfTheLifecycle(t *testing.T) {
	g := newRig(t, policy)
	g.r.recs[1] = &record{gaveUp: true}
	g.r.recs[2] = &record{gaveUp: true}
	g.r.recs[3] = &record{gaveUp: true}
	g.r.recs[4] = &record{}
	ev := func(id uint64, st kernel.State) {
		g.r.Status(t.Context(), kernel.LedgerEvent{Kind: "state", Fiber: id, State: st})
	}
	ev(1, kernel.Active)    // an operator fixed it
	ev(2, kernel.Loading)   // an operator is updating it
	ev(3, kernel.Disposed)  // it is gone
	ev(4, kernel.Unloading) // a record that was not given up on stays
	ev(5, kernel.Pending)   // a fiber that never failed is nothing
	ev(4, kernel.Active)
	if g.tracked() != 1 || g.r.recs[4] == nil || g.r.recs[4].activeAt.IsZero() {
		t.Errorf("records = %v", g.r.recs)
	}
}

func TestResyncDropsWhatIsGoneAndCountsWhatWasLost(t *testing.T) {
	g := newRig(t, policy)
	gone := g.root.Plugin(kernel.Define(kernel.Plugin{ID: "aiops/gone", Apply: func(*kernel.Context, any) error { return nil }}), nil)
	if err := gone.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := gone.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := gone.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	goneID := g.reg.put(gone)
	alive := g.reg.put(g.active())
	lost := g.reg.put(g.failing())
	pending := g.reg.put(g.failing()) // tracked and still FAILED: it keeps its record
	g.r.recs[goneID] = &record{}
	g.r.recs[alive] = &record{}
	g.r.recs[404] = &record{} // not in the registry
	g.r.recs[pending] = &record{pending: true}

	g.comp.Commit(composition.Generation{Scope: composition.ScopeProcess, Rows: []composition.Row{
		{ID: "lost", Plugin: "aiops/f", Fiber: lost, State: kernel.Failed},
		{ID: "pending", Plugin: "aiops/f", Fiber: pending, State: kernel.Failed},
		{ID: "none", Plugin: "aiops/f", State: kernel.Failed},
	}})
	g.r.Status(t.Context(), kernel.LedgerEvent{Kind: "overflow", Sequence: 10})

	if g.r.recs[goneID] != nil || g.r.recs[404] != nil {
		t.Error("a record of a fiber that is gone was kept")
	}
	if rec := g.r.recs[alive]; rec == nil || rec.activeAt.IsZero() {
		t.Error("an ACTIVE fiber was dropped or not noted")
	}
	if rec := g.r.recs[lost]; rec == nil || !rec.pending || len(rec.fails) != 1 {
		t.Errorf("the lost failure was not counted: %+v", rec)
	}
	if rec := g.r.recs[pending]; rec == nil || len(rec.fails) != 0 {
		t.Errorf("a fiber with a retry pending was counted twice: %+v", rec)
	}
	if g.clk.Pending() != 1 {
		t.Errorf("timers = %d, want the one for the lost failure", g.clk.Pending())
	}
}

// The resync finds again a lost failure of a fiber the retrier tracks, whether or
// not the committed tree lists that fiber.
func TestResyncRetriesATrackedFiberWhoseFailureWasLost(t *testing.T) {
	g := newRig(t, policy)
	f := g.failing()
	id := g.reg.put(f)
	g.r.Status(t.Context(), failedEvent(id))
	g.clk.Advance(time.Second)
	g.waitLog("fiber.retried") // the update fails again, and the rig delivers no event
	_ = f.Await(t.Context())
	g.r.Status(t.Context(), kernel.LedgerEvent{Kind: "overflow"})
	g.r.mu.Lock()
	defer g.r.mu.Unlock()
	if rec := g.r.recs[id]; rec == nil || !rec.pending || len(rec.fails) != 2 {
		t.Errorf("record = %+v: the lost second failure was not counted and retried", rec)
	}
}

func TestBackoffNeverPassesTheBound(t *testing.T) {
	r := New(Policy{BackoffMin: time.Second, BackoffMax: 5 * time.Second, Factor: 3}, Deps{})
	for n, want := range []time.Duration{time.Second, 3 * time.Second, 5 * time.Second, 5 * time.Second} {
		if got := r.backoff(n + 1); got != want {
			t.Errorf("backoff(%d) = %v, want %v", n+1, got, want)
		}
	}
}

func TestRetryTimerAfterTheFiberIsUnloading(t *testing.T) {
	g := newRig(t, policy)
	f := g.failing()
	id := g.reg.put(f)
	// Replace the context by one whose fiber is gone: After is refused.
	var dead *kernel.Context
	p := g.root.Plugin(kernel.Define(kernel.Plugin{ID: "aiops/dead", Apply: func(c *kernel.Context, _ any) error { dead = c; return nil }}), nil)
	if err := p.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := p.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := p.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	g.r.Context = dead
	g.r.Status(t.Context(), failedEvent(id))
	if rec := g.r.recs[id]; rec == nil || rec.pending {
		t.Errorf("record = %+v: a retry that could not be scheduled must not stay pending", rec)
	}
}
