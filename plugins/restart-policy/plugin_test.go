// SPDX-License-Identifier: Apache-2.0

package restartpolicy_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	restartpolicy "github.com/mutex-solutions/aiops-platform/plugins/restart-policy"
	clockfake "github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0/fake"
	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	compfake "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0/fake"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	launchfake "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0/fake"
	loader "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	reloadfake "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// entry is one record of the kernel's logger.
type entry struct {
	msg   string
	attrs map[string]any
}

// logTap is the kernel's log handler: it hands every record to a channel, which is
// how a test waits for what restart-policy decided without sleeping.
type logTap struct{ ch chan entry }

func (h logTap) Enabled(context.Context, slog.Level) bool { return true }
func (h logTap) Handle(_ context.Context, r slog.Record) error {
	e := entry{msg: r.Message, attrs: map[string]any{}}
	r.Attrs(func(a slog.Attr) bool { e.attrs[a.Key] = a.Value.Any(); return true })
	select {
	case h.ch <- e:
	default:
	}
	return nil
}
func (h logTap) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h logTap) WithGroup(string) slog.Handler      { return h }

// flaky is a row whose Apply fails while failing is set or its config is "bad".
// attempts counts Apply calls and signals each on tried. An Apply that works starts
// a tracked goroutine that fails, and so fails the fiber, when the test sends on die.
type flaky struct {
	id       string
	failing  atomic.Bool
	attempts atomic.Int32
	tried    chan int32
	die      chan struct{}
}

func newFlaky(id string, failing bool) *flaky {
	f := &flaky{id: id, tried: make(chan int32, 64), die: make(chan struct{})}
	f.failing.Store(failing)
	return f
}

func (f *flaky) plugin(needs ...kernel.Dep) kernel.Plugin {
	return kernel.Define(kernel.Plugin{ID: f.id, Inject: needs, Apply: func(c *kernel.Context, cfg any) error {
		n := f.attempts.Add(1)
		f.tried <- n
		if f.failing.Load() || cfg == "bad" {
			return errors.New("apply failed")
		}
		c.Go("flaky work", func(ctx context.Context) error {
			select {
			case <-f.die:
				return errors.New("crashed")
			case <-ctx.Done():
				return nil
			}
		})
		return nil
	}})
}

// crash fails the ACTIVE fiber f from its own goroutine, once Apply n has run and
// restart-policy has heard that f is ACTIVE.
func (w *world) crash(fl *flaky, f *kernel.Fiber, n int32) {
	w.t.Helper()
	fl.attempt(w.t, n)
	if err := f.Await(w.t.Context()); err != nil {
		w.t.Fatal(err)
	}
	w.heard()
	fl.die <- struct{}{}
}

// attempt waits for the n-th Apply of f to have started.
func (f *flaky) attempt(t *testing.T, n int32) {
	t.Helper()
	for got := range f.tried {
		if got == n {
			return
		}
	}
}

type world struct {
	t      *testing.T
	root   *kernel.Root
	launch *launchfake.Launcher
	clk    *clockfake.Clock
	comp   *compfake.Reader
	reload *reloadfake.Reload
	logs   chan entry
	plugin kernel.Plugin // the restart-policy row: a fiber mounted from it again shares its memory
	rp     *kernel.Fiber
}

func conf(max int, window, min, maxd time.Duration, factor int, stable time.Duration) restartpolicy.Config {
	return restartpolicy.Config{MaxFailures: max, Window: window, BackoffMin: min, BackoffMax: maxd, Factor: factor,
		Stable: stable, Infrastructure: []string{"sql"}}
}

func newWorld(t *testing.T, cfg any, opts ...kerneltest.Option) *world {
	t.Helper()
	w := &world{t: t, logs: make(chan entry, 4096)}
	opts = append([]kerneltest.Option{kerneltest.WithConfig(func(c *kernel.Config) { c.LogHandler = logTap{w.logs} })}, opts...)
	w.root = kerneltest.New(t, opts...)
	var lp, kp, cp, rp kernel.Plugin
	w.launch, lp = launchfake.New()
	w.launch.Modules = []launch.CatalogEntry{{ID: "aiops/sql", Provides: []string{"sql"}}, {ID: "aiops/other"}}
	w.clk, kp = clockfake.New(time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC))
	w.comp, cp = compfake.New()
	w.reload, rp = reloadfake.New()
	kerneltest.Mount(w.root, lp, kp, cp, rp)
	w.plugin = restartpolicy.New()
	w.rp = w.root.Plugin(w.plugin, cfg)
	if err := w.rp.Await(t.Context()); err != nil {
		t.Fatalf("restart-policy: %v", err)
	}
	return w
}

// logged waits for the next record with this message.
func (w *world) logged(msg string) entry {
	w.t.Helper()
	for {
		select {
		case e := <-w.logs:
			if e.msg == msg {
				return e
			}
		case <-w.t.Context().Done():
			w.t.Fatalf("no %q was logged", msg)
		}
	}
}

// decision waits for what restart-policy decided next: a retry or a give-up.
func (w *world) decision() entry {
	w.t.Helper()
	for {
		select {
		case e := <-w.logs:
			if e.msg == "fiber.retry" || e.msg == "fiber.gave-up" {
				return e
			}
		case <-w.t.Context().Done():
			w.t.Fatal("neither a retry nor a give-up")
		}
	}
}

// retryLogged waits for a retry and checks the delay and the count it names.
func (w *world) retryLogged(failures int, delay time.Duration) {
	w.t.Helper()
	e := w.logged("fiber.retry")
	if e.attrs["failures"] != int64(failures) || e.attrs["delay"] != delay {
		w.t.Fatalf("retry = %v, want failure %d after %v", e.attrs, failures, delay)
	}
}

// heard waits until every listener has seen what the kernel did so far.
func (w *world) heard() {
	w.t.Helper()
	if err := w.root.AwaitObservers(w.t.Context()); err != nil && !errors.Is(err, kernel.ErrObserverOverflow) {
		w.t.Fatal(err)
	}
}

func (w *world) state(f *kernel.Fiber) kernel.State {
	w.t.Helper()
	st, err := f.State(w.t.Context())
	if err != nil {
		w.t.Fatal(err)
	}
	return st
}

func (w *world) exits() []launch.ExitRequest { return w.launch.Exits() }

func TestRetriesInPlaceWithGrowingBackoffUntilItWorks(t *testing.T) {
	w := newWorld(t, conf(4, time.Hour, time.Second, 3*time.Second, 2, 0))
	fl := newFlaky("aiops/other", true)
	f := w.root.Plugin(fl.plugin(), nil)

	// Four failures, each retried: the delay doubles and then stays at the bound.
	delays := []time.Duration{time.Second, 2 * time.Second, 3 * time.Second, 3 * time.Second}
	w.retryLogged(1, delays[0])
	for i, d := range delays {
		w.clk.Advance(d / 2)
		if w.clk.Pending() != 1 {
			t.Fatalf("retry %d fired before its backoff", i+1)
		}
		if i == len(delays)-1 {
			fl.failing.Store(false)
		}
		w.clk.Advance(d - d/2)
		fl.attempt(t, int32(i+2))
		if i+1 < len(delays) {
			w.retryLogged(i+2, delays[i+1])
		}
	}
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	if st := w.state(f); st != kernel.Active {
		t.Errorf("state = %s, want ACTIVE", st)
	}
	if len(w.exits()) != 0 {
		t.Errorf("exits = %v", w.exits())
	}
}

func TestGivesUpOnAnInfrastructureProviderAndAsksForExit(t *testing.T) {
	w := newWorld(t, conf(2, time.Hour, time.Second, time.Minute, 2, 0))
	fl := newFlaky("aiops/sql", true)
	f := w.root.Plugin(fl.plugin(), nil)

	w.retryLogged(1, time.Second)
	w.clk.Advance(time.Second)
	w.retryLogged(2, 2*time.Second)
	w.clk.Advance(2 * time.Second)
	e := w.logged("fiber.gave-up")
	if e.attrs["fiber"] != f.ID() || e.attrs["plugin"] != "aiops/sql" || e.attrs["failures"] != int64(3) || e.attrs["infrastructure"] != true {
		t.Errorf("gave-up = %v", e.attrs)
	}
	want := []launch.ExitRequest{{Reason: launch.ExitGaveUp, Detail: "aiops/sql"}}
	if got := w.exits(); !slices.Equal(got, want) {
		t.Errorf("exits = %v, want %v", got, want)
	}
	if st := w.state(f); st != kernel.Failed {
		t.Errorf("state = %s: the fiber stays FAILED", st)
	}
	if w.clk.Pending() != 0 {
		t.Error("a retry is still scheduled after the give-up")
	}
	w.clk.Advance(24 * time.Hour)
	if n := fl.attempts.Load(); n != 3 {
		t.Errorf("attempts = %d, want 3", n)
	}
}

func TestGivesUpOnAnOrdinaryRowWithoutExit(t *testing.T) {
	w := newWorld(t, conf(1, time.Hour, time.Second, time.Minute, 2, 0))
	fl := newFlaky("aiops/other", true)
	f := w.root.Plugin(fl.plugin(), nil)
	w.retryLogged(1, time.Second)
	w.clk.Advance(time.Second)
	e := w.logged("fiber.gave-up")
	if e.attrs["infrastructure"] != false {
		t.Errorf("gave-up = %v", e.attrs)
	}
	if len(w.exits()) != 0 {
		t.Errorf("exits = %v: only an infrastructure provider replaces the process", w.exits())
	}

	// An operator updates the fiber: it is tracked afresh, and a failure is retried.
	if err := f.Update(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	w.retryLogged(1, time.Second)
	// And one that works clears the give-up for good.
	fl.failing.Store(false)
	w.clk.Advance(time.Second)
	fl.attempt(t, 4)
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	w.heard()
	fl.failing.Store(true)
	if err := f.Update(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	w.retryLogged(1, time.Second)
}

func TestNoRetryForAFiberThatWasDisposed(t *testing.T) {
	w := newWorld(t, conf(3, time.Hour, time.Second, time.Minute, 2, 0))
	fl := newFlaky("aiops/other", true)
	f := w.root.Plugin(fl.plugin(), nil)
	w.retryLogged(1, time.Second)

	if err := f.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	_ = f.Await(t.Context()) // it returns the startup error that the fiber failed with
	w.heard()
	w.clk.Advance(time.Second)
	if e := w.logged("fiber.retry-skipped"); e.attrs["why"] != "forgotten" {
		t.Errorf("skipped = %v", e.attrs)
	}
	if n := fl.attempts.Load(); n != 1 {
		t.Errorf("attempts = %d: a disposed fiber was retried", n)
	}
}

func TestNoRetryForAFiberThatIsNotFailedAnymore(t *testing.T) {
	w := newWorld(t, conf(3, time.Hour, time.Second, time.Minute, 2, 0))
	fl := newFlaky("aiops/other", true)
	f := w.root.Plugin(fl.plugin(), nil)
	w.retryLogged(1, time.Second)

	// An operator fixes the row before the timer fires.
	fl.failing.Store(false)
	if err := f.Update(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	fl.attempt(t, 2)
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	w.heard()
	w.clk.Advance(time.Second)
	// The operator's update started a fresh record, so the old timer finds none.
	if e := w.logged("fiber.retry-skipped"); e.attrs["why"] != "forgotten" {
		t.Errorf("skipped = %v", e.attrs)
	}
	if st := w.state(f); st != kernel.Active {
		t.Errorf("state = %s, want ACTIVE", st)
	}
	if n := fl.attempts.Load(); n != 2 {
		t.Errorf("attempts = %d", n)
	}
}

func TestReplacedFiberIsNotRetriedUnderItsOldId(t *testing.T) {
	w := newWorld(t, conf(3, time.Hour, time.Second, time.Minute, 2, 0))
	old := newFlaky("aiops/other", true)
	fo := w.root.Plugin(old.plugin(), nil)
	w.retryLogged(1, time.Second)
	// The row is replaced: the old fiber is disposed and a new one, with a new id,
	// takes its place and works.
	if err := fo.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	_ = fo.Await(t.Context())
	repl := newFlaky("aiops/other", false)
	fn := w.root.Plugin(repl.plugin(), nil)
	if err := fn.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	if fn.ID() == fo.ID() {
		t.Fatal("a replacement reused an id")
	}
	w.heard()
	w.clk.Advance(time.Second)
	w.logged("fiber.retry-skipped")
	if old.attempts.Load() != 1 || repl.attempts.Load() != 1 {
		t.Errorf("attempts = %d and %d", old.attempts.Load(), repl.attempts.Load())
	}
}

// The failures of a fiber are counted from its last stable period and inside the
// window only.
func TestWindowAndStablePeriod(t *testing.T) {
	// The fiber fails, is retried and works, then fails on its own after wait: is that
	// a second failure of the same run (the fiber was not stable) or the first of a new
	// one?
	run := func(t *testing.T, w *world, wait time.Duration) (gaveUp bool) {
		fl := newFlaky("aiops/other", true)
		f := w.root.Plugin(fl.plugin(), nil)
		w.retryLogged(1, time.Second)
		fl.failing.Store(false)
		w.clk.Advance(time.Second)
		fl.attempt(t, 2)
		if err := f.Await(t.Context()); err != nil {
			t.Fatal(err)
		}
		w.heard() // ACTIVE is noted at the clock's time
		w.clk.Advance(wait)
		fl.die <- struct{}{}
		return w.decision().msg == "fiber.gave-up"
	}
	t.Run("with no stable period a failure after a success still counts", func(t *testing.T) {
		if !run(t, newWorld(t, conf(1, time.Hour, time.Second, time.Minute, 2, 0)), 0) {
			t.Error("did not give up: a success forgot the failures although no stable period was asked")
		}
	})
	t.Run("a fiber that did not stay up long enough keeps its count", func(t *testing.T) {
		if !run(t, newWorld(t, conf(1, time.Hour, time.Second, time.Minute, 2, time.Hour)), time.Minute) {
			t.Error("did not give up on a fiber that failed again inside the stable period")
		}
	})
	t.Run("a fiber that stayed up the stable period starts again", func(t *testing.T) {
		if run(t, newWorld(t, conf(1, 24*time.Hour, time.Second, time.Minute, 2, time.Hour)), time.Hour) {
			t.Error("gave up although the fiber stayed up for the stable period")
		}
	})
	t.Run("failures older than the window no longer count", func(t *testing.T) {
		if run(t, newWorld(t, conf(1, 10*time.Second, time.Second, time.Minute, 2, 24*time.Hour)), time.Minute) {
			t.Error("gave up on a failure that left the window")
		}
	})
}

// An infrastructure provider whose goroutine fails each time after its Apply worked
// is given up on after more than N failures in the window, stable omitted
// (03-c3-kernel.md:66, :372).
func TestGivesUpOnAFiberThatFailsAfterItWasActive(t *testing.T) {
	w := newWorld(t, conf(2, time.Hour, time.Second, time.Minute, 2, 0))
	fl := newFlaky("aiops/sql", false)
	f := w.root.Plugin(fl.plugin(), nil)
	for i, d := range []time.Duration{time.Second, 2 * time.Second} {
		w.crash(fl, f, int32(i+1))
		w.retryLogged(i+1, d)
		w.clk.Advance(d)
	}
	w.crash(fl, f, 3)
	if e := w.decision(); e.msg != "fiber.gave-up" || e.attrs["failures"] != int64(3) {
		t.Fatalf("%s %v, want a give-up at failure 3", e.msg, e.attrs)
	}
	want := []launch.ExitRequest{{Reason: launch.ExitGaveUp, Detail: "aiops/sql"}}
	if got := w.exits(); !slices.Equal(got, want) {
		t.Errorf("exits = %v, want %v", got, want)
	}
}

// The stable period is time ACTIVE: time PENDING for a provider after a short ACTIVE
// period does not forget the failures.
func TestTimePendingIsNotStable(t *testing.T) {
	w := newWorld(t, conf(2, time.Hour, time.Second, time.Minute, 2, 10*time.Minute))
	key := kernel.NewKey[int]("test.dep", 0)
	provider := kernel.Define(kernel.Plugin{ID: "aiops/dep", Provides: []kernel.AnyKey{key},
		Apply: func(c *kernel.Context, _ any) error { return key.Provide(c, 1) }})
	p := w.root.Plugin(provider, nil)
	fl := newFlaky("aiops/other", true)
	f := w.root.Plugin(fl.plugin(kernel.Need(key)), nil)
	w.retryLogged(1, time.Second)
	w.clk.Advance(time.Second)
	w.retryLogged(2, 2*time.Second)
	fl.failing.Store(false)
	w.clk.Advance(2 * time.Second)
	fl.attempt(t, 3)
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	w.heard()
	w.clk.Advance(time.Second) // ACTIVE for one second
	if err := p.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := p.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	if st := w.state(f); st != kernel.Pending {
		t.Fatalf("state = %s, want PENDING without its provider", st)
	}
	w.heard()
	w.clk.Advance(15 * time.Minute)
	fl.failing.Store(true)
	w.root.Plugin(provider, nil) // the provider returns and the fiber fails to load
	if e := w.decision(); e.msg != "fiber.gave-up" || e.attrs["failures"] != int64(3) {
		t.Fatalf("%s %v, want a give-up at failure 3: the fiber was ACTIVE for one second only", e.msg, e.attrs)
	}
}

// A fiber that someone else restarts is tracked afresh: the failures of a config
// that reload applied and then rolled back do not count against the config it
// restored.
func TestFailuresOfAReplacedConfigDoNotCarryOver(t *testing.T) {
	w := newWorld(t, conf(3, time.Hour, time.Second, time.Minute, 2, time.Hour))
	fl := newFlaky("aiops/sql", false)
	f := w.root.Plugin(fl.plugin(), "good")
	fl.attempt(t, 1)
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	w.heard()
	// The candidate fails three times and is retried in place each time.
	if err := f.Update(t.Context(), "bad"); err != nil {
		t.Fatal(err)
	}
	for i, d := range []time.Duration{time.Second, 2 * time.Second} {
		w.retryLogged(i+1, d)
		w.clk.Advance(d)
	}
	w.retryLogged(3, 4*time.Second)
	// The rollback restores the previous config while the fiber waits for its retry,
	// which then falls due with nothing to do.
	if err := f.Update(t.Context(), "good"); err != nil {
		t.Fatal(err)
	}
	fl.attempt(t, 5)
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	w.heard()
	w.clk.Advance(4 * time.Second)
	w.logged("fiber.retry-skipped")
	fl.die <- struct{}{} // one transient failure of the restored config
	if e := w.decision(); e.msg != "fiber.retry" || e.attrs["failures"] != int64(1) {
		t.Fatalf("%s %v, want the first failure of the restored config", e.msg, e.attrs)
	}
	if len(w.exits()) != 0 {
		t.Errorf("exits = %v", w.exits())
	}
}

// running and final are the two reload/operation events of one operation.
var (
	running = reload.Result{ID: 1, Status: reload.Running, Stage: reload.StageApply}
	passed  = reload.Result{ID: 1, Status: reload.Passed, Stage: reload.StagePublish}
	undone  = reload.Result{ID: 1, Status: reload.RolledBack, Stage: reload.StagePublish}
)

// progress emits reload/operation as reload does, with ctx as its goroutine's context.
func (w *world) progress(ctx context.Context, r reload.Result) {
	w.t.Helper()
	if err := w.reload.Progress(ctx, r); err != nil {
		w.t.Fatal(err)
	}
}

// failPastTheLimit makes an infrastructure provider fail twice under maxFailures 1
// and returns it once restart-policy has decided on the second failure.
func (w *world) failPastTheLimit() (*flaky, *kernel.Fiber) {
	w.t.Helper()
	fl := newFlaky("aiops/sql", true)
	f := w.root.Plugin(fl.plugin(), nil)
	w.retryLogged(1, time.Second)
	w.clk.Advance(time.Second)
	fl.attempt(w.t, 2)
	_ = f.Await(w.t.Context())
	w.heard()
	return fl, f
}

// gaveUp reports whether restart-policy has logged a give-up so far.
func (w *world) gaveUp() bool {
	for {
		select {
		case e := <-w.logs:
			if e.msg == "fiber.gave-up" {
				return true
			}
		default:
			return false
		}
	}
}

// While reload applies a generation and until it is final, its rows are not judged: a
// failure there is rolled back (08-profiles-roadmap-adrs.md:473). A fiber past its
// limit is held, without the exit; once the operation is final, one still FAILED is
// given up on.
func TestGiveUpWaitsForTheReloadOperation(t *testing.T) {
	w := newWorld(t, conf(1, time.Hour, time.Second, time.Minute, 2, 0))
	w.progress(t.Context(), running)
	_, f := w.failPastTheLimit()
	if w.gaveUp() || len(w.exits()) != 0 {
		t.Fatalf("gave up while the operation was in flight: exits = %v", w.exits())
	}
	w.progress(t.Context(), passed)
	if e := w.decision(); e.msg != "fiber.gave-up" || e.attrs["fiber"] != f.ID() || e.attrs["failures"] != int64(2) {
		t.Fatalf("%s %v, want the give-up once the operation is final", e.msg, e.attrs)
	}
	want := []launch.ExitRequest{{Reason: launch.ExitGaveUp, Detail: "aiops/sql"}}
	if got := w.exits(); !slices.Equal(got, want) {
		t.Errorf("exits = %v, want %v", got, want)
	}
}

// A held fiber that the rollback restores is not given up on, and the process stays.
func TestRolledBackFiberIsNotGivenUpOn(t *testing.T) {
	w := newWorld(t, conf(1, time.Hour, time.Second, time.Minute, 2, 0))
	w.progress(t.Context(), running)
	fl, f := w.failPastTheLimit()
	fl.failing.Store(false)
	if err := f.Update(t.Context(), nil); err != nil { // the rollback
		t.Fatal(err)
	}
	fl.attempt(t, 3)
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	w.heard()
	w.progress(t.Context(), undone)
	if w.gaveUp() || len(w.exits()) != 0 {
		t.Errorf("gave up on a fiber the rollback restored: exits = %v", w.exits())
	}
}

// An operation whose final event never comes, because the reload fiber unloaded,
// ends with the context of its goroutine: the next event ends the hold.
func TestHoldEndsWithTheReloadGoroutine(t *testing.T) {
	w := newWorld(t, conf(1, time.Hour, time.Second, time.Minute, 2, 0))
	op, cancel := context.WithCancel(t.Context())
	w.progress(op, running)
	w.failPastTheLimit()
	if w.gaveUp() {
		t.Fatal("gave up while the operation was in flight")
	}
	cancel()
	w.root.Plugin(newFlaky("aiops/other", false).plugin(), nil) // any event
	if e := w.decision(); e.msg != "fiber.gave-up" {
		t.Fatalf("%s %v, want the give-up", e.msg, e.attrs)
	}
	if len(w.exits()) != 1 {
		t.Errorf("exits = %v", w.exits())
	}
}

// restart restarts restart-policy with cfg, as a reload that changes its config does.
func (w *world) restart(cfg any) {
	w.t.Helper()
	if err := w.rp.Update(w.t.Context(), cfg); err != nil {
		w.t.Fatal(err)
	}
	if err := w.rp.Await(w.t.Context()); err != nil {
		w.t.Fatal(err)
	}
	w.heard()
}

// A restart of restart-policy keeps what it knows: a fiber given up on stays FAILED
// until an operator updates it (07-c4-plugin-sdk.md:360), and a fiber that waits for a
// retry keeps its count.
func TestARestartOfRestartPolicyKeepsItsGiveUpsAndCounts(t *testing.T) {
	w := newWorld(t, conf(2, time.Hour, time.Second, time.Minute, 2, 0))
	x := newFlaky("aiops/other", true)
	xf := w.root.Plugin(x.plugin(), nil)
	w.retryLogged(1, time.Second)
	w.clk.Advance(time.Second)
	w.retryLogged(2, 2*time.Second)
	w.clk.Advance(2 * time.Second)
	if e := w.logged("fiber.gave-up"); e.attrs["fiber"] != xf.ID() {
		t.Fatalf("gave-up = %v", e.attrs)
	}
	y := newFlaky("aiops/other", true)
	yf := w.root.Plugin(y.plugin(), nil)
	w.retryLogged(1, time.Second)
	w.clk.Advance(time.Second)
	w.retryLogged(2, 2*time.Second)
	w.comp.Commit(composition.Generation{Scope: composition.ScopeProcess, Rows: []composition.Row{
		{ID: "x", Plugin: "aiops/other", Fiber: xf.ID(), State: kernel.Failed},
		{ID: "y", Plugin: "aiops/other", Fiber: yf.ID(), State: kernel.Failed},
	}})

	w.restart(conf(2, time.Hour, time.Second, time.Minute, 2, time.Hour))
	if e := w.decision(); e.msg != "fiber.retry" || e.attrs["fiber"] != yf.ID() || e.attrs["failures"] != int64(2) {
		t.Fatalf("%s %v after the restart, want the retry of y at its second failure", e.msg, e.attrs)
	}
	if n := w.clk.Pending(); n != 1 {
		t.Fatalf("pending timers %d after the restart, want y's retry only", n)
	}
	w.clk.Advance(2 * time.Second)
	if e := w.decision(); e.msg != "fiber.gave-up" || e.attrs["fiber"] != yf.ID() || e.attrs["failures"] != int64(3) {
		t.Fatalf("%s %v, want the give-up of y at its third failure", e.msg, e.attrs)
	}
	w.clk.Advance(24 * time.Hour)
	if n := x.attempts.Load(); n != 3 {
		t.Errorf("x was applied %d times, want 3: a fiber given up on is not retried", n)
	}
}

// A restart of restart-policy while reload applies a generation keeps the hold: a
// failure of the generation's rows is not given up on until the operation is final.
func TestARestartOfRestartPolicyKeepsTheReloadOperationInFlight(t *testing.T) {
	w := newWorld(t, conf(1, time.Hour, time.Second, time.Minute, 2, 0))
	w.progress(t.Context(), running)
	w.restart(conf(1, time.Hour, time.Second, time.Minute, 2, time.Hour))
	_, f := w.failPastTheLimit()
	if w.gaveUp() || len(w.exits()) != 0 {
		t.Fatalf("gave up while the operation was in flight: exits = %v", w.exits())
	}
	w.progress(t.Context(), passed)
	if e := w.decision(); e.msg != "fiber.gave-up" || e.attrs["fiber"] != f.ID() {
		t.Fatalf("%s %v, want the give-up once the operation is final", e.msg, e.attrs)
	}
	want := []launch.ExitRequest{{Reason: launch.ExitGaveUp, Detail: "aiops/sql"}}
	if got := w.exits(); !slices.Equal(got, want) {
		t.Errorf("exits = %v, want %v", got, want)
	}
}

// next waits for what restart-policy decided next, a hold included.
func (w *world) next() entry {
	w.t.Helper()
	for {
		select {
		case e := <-w.logs:
			switch e.msg {
			case "fiber.retry", "fiber.gave-up", "fiber.retry-held", "fiber.give-up-held":
				return e
			}
		case <-w.t.Context().Done():
			w.t.Fatal("no decision")
		}
	}
}

// up mounts a row of fl that works and waits until restart-policy heard it ACTIVE.
func (w *world) up(fl *flaky, cfg any) *kernel.Fiber {
	w.t.Helper()
	f := w.root.Plugin(fl.plugin(), cfg)
	if err := f.Await(w.t.Context()); err != nil {
		w.t.Fatal(err)
	}
	w.heard()
	return f
}

// tree commits the rows of the tree as the composition key serves them: a row id, its
// plugin, the fiber and its state.
func (w *world) tree(rows ...composition.Row) {
	w.comp.Commit(composition.Generation{Scope: composition.ScopeProcess, Rows: rows})
}

// applying is the event of an operation about to apply a plan that changes rows.
func applying(rows ...string) reload.Result {
	r := reload.Result{ID: 1, Status: reload.Running, Stage: reload.StageApply}
	for _, id := range rows {
		r.Plan.Entries = append(r.Plan.Entries, launch.PlanEntry{ID: id, Action: launch.ActionUpdate})
	}
	return r
}

// A row that the generation in flight changes is not retried until the operation is
// final, so the verdict sees its failure whatever another row takes to load (D19); a
// row that the plan does not list is retried as ever. Once the operation is final,
// restart-policy acts on what the rollback left: a row still FAILED is retried.
func TestRetriesOfTheRowsOfAGenerationWaitForItsEnd(t *testing.T) {
	w := newWorld(t, conf(3, time.Hour, time.Second, time.Minute, 2, 0))
	x, y := newFlaky("aiops/other", false), newFlaky("aiops/other", false)
	xf, yf := w.up(x, "good"), w.up(y, "good")
	w.tree(composition.Row{ID: "x", Plugin: "aiops/other", Fiber: xf.ID(), State: kernel.Active},
		composition.Row{ID: "y", Plugin: "aiops/other", Fiber: yf.ID(), State: kernel.Active})

	w.progress(t.Context(), applying("x"))
	if err := xf.Update(t.Context(), "bad"); err != nil { // the generation's candidate config fails
		t.Fatal(err)
	}
	if e := w.next(); e.msg != "fiber.retry-held" || e.attrs["fiber"] != xf.ID() || e.attrs["failures"] != int64(1) {
		t.Fatalf("%s %v, want the retry of x held while its generation is in flight", e.msg, e.attrs)
	}
	y.die <- struct{}{} // y is not of the generation
	if e := w.next(); e.msg != "fiber.retry" || e.attrs["fiber"] != yf.ID() {
		t.Fatalf("%s %v, want the retry of y, which the plan does not list", e.msg, e.attrs)
	}
	if n := w.clk.Pending(); n != 1 {
		t.Fatalf("pending timers %d, want y's retry only", n)
	}

	// The rollback failed and left x FAILED: x is retried once the operation is final.
	w.progress(t.Context(), reload.Result{ID: 1, Status: reload.RollbackFailed, Stage: reload.StageRollback})
	if e := w.next(); e.msg != "fiber.retry" || e.attrs["fiber"] != xf.ID() || e.attrs["failures"] != int64(1) {
		t.Fatalf("%s %v, want the retry of x at its first failure once the operation is final", e.msg, e.attrs)
	}
	x.failing.Store(false)
	w.clk.Advance(time.Second)
	x.attempt(t, 3) // 1 good, 2 the candidate, 3 the retry
}

// A retry that falls due while a generation that lists its row is in flight waits for
// the operation's end too, and is decided again on the state the operation left.
func TestARetryThatFallsDueWhileItsGenerationIsInFlightWaits(t *testing.T) {
	w := newWorld(t, conf(3, time.Hour, time.Second, time.Minute, 2, 0))
	x := newFlaky("aiops/other", true)
	xf := w.root.Plugin(x.plugin(), nil)
	w.retryLogged(1, time.Second)
	w.tree(composition.Row{ID: "x", Plugin: "aiops/other", Fiber: xf.ID(), State: kernel.Failed})
	w.progress(t.Context(), applying("x"))
	w.clk.Advance(time.Second)
	if e := w.next(); e.msg != "fiber.retry-held" || e.attrs["fiber"] != xf.ID() {
		t.Fatalf("%s %v, want the retry held while the generation of x is in flight", e.msg, e.attrs)
	}
	if n := x.attempts.Load(); n != 1 {
		t.Fatalf("x was applied %d times, want once: the retry ran during the operation", n)
	}
	w.progress(t.Context(), reload.Result{ID: 1, Status: reload.RollbackFailed, Stage: reload.StageRollback})
	w.retryLogged(1, time.Second)
}

// A retry that an earlier activation scheduled is not scheduled again for a fiber that
// someone else restarted while restart-policy was not listening: it is tracked afresh.
func TestAPendingRetryOfAFiberRestartedMeanwhileIsDropped(t *testing.T) {
	cfg := conf(1, time.Hour, time.Second, time.Minute, 2, 0)
	w := newWorld(t, cfg)
	x := newFlaky("aiops/sql", true)
	xf := w.root.Plugin(x.plugin(), nil)
	w.retryLogged(1, time.Second)
	w.remount(cfg, func() { // an operator fixes x
		x.failing.Store(false)
		if err := xf.Update(t.Context(), nil); err != nil {
			t.Fatal(err)
		}
		x.attempt(t, 2)
		if err := xf.Await(t.Context()); err != nil {
			t.Fatal(err)
		}
	})
	if n := w.clk.Pending(); n != 0 {
		t.Fatalf("pending timers %d: the retry of a fiber that is not FAILED was scheduled again", n)
	}
	x.die <- struct{}{}
	if e := w.decision(); e.msg != "fiber.retry" || e.attrs["failures"] != int64(1) {
		t.Fatalf("%s %v, want the first failure of the fixed fiber", e.msg, e.attrs)
	}
}

// remount ends the restart-policy fiber, runs between, and mounts the row again from
// the same plugin value with cfg, as a restart of the row does: what happens between
// reaches no activation.
func (w *world) remount(cfg any, between func()) {
	w.t.Helper()
	if err := w.rp.Dispose(w.t.Context()); err != nil {
		w.t.Fatal(err)
	}
	if err := w.rp.Await(w.t.Context()); err != nil {
		w.t.Fatal(err)
	}
	between()
	w.rp = w.root.Plugin(w.plugin, cfg)
	if err := w.rp.Await(w.t.Context()); err != nil {
		w.t.Fatal(err)
	}
	w.heard()
}

// The failures of a candidate config do not carry over to the config that its rollback
// restores when restart-policy did not hear the restarts: the generation changed
// restart-policy's own config, so restart-policy restarted in the batch that gave the
// fault row its candidate and again in the rollback's (D22).
func TestCandidateFailuresDoNotCarryOverAcrossRestartsOfRestartPolicy(t *testing.T) {
	cfg := conf(1, time.Hour, time.Second, time.Minute, 2, 0)
	w := newWorld(t, cfg)
	x := newFlaky("aiops/sql", false)
	xf := w.up(x, "good")
	w.progress(t.Context(), applying("x", "restart-policy"))
	w.remount(cfg, func() { // the candidate fails
		if err := xf.Update(t.Context(), "bad"); err != nil {
			t.Fatal(err)
		}
		x.attempt(t, 2)
		_ = xf.Await(t.Context())
		w.tree(composition.Row{ID: "x", Plugin: "aiops/sql", Fiber: xf.ID(), State: kernel.Failed})
	})
	w.remount(cfg, func() { // the rollback restores the previous config
		if err := xf.Update(t.Context(), "good"); err != nil {
			t.Fatal(err)
		}
		x.attempt(t, 3)
		if err := xf.Await(t.Context()); err != nil {
			t.Fatal(err)
		}
		w.tree(composition.Row{ID: "x", Plugin: "aiops/sql", Fiber: xf.ID(), State: kernel.Active})
	})
	if n := w.clk.Pending(); n != 0 {
		t.Fatalf("pending timers %d: x is retried although the rollback restarted it", n)
	}
	restarted := []loader.Result{{ID: "x", Outcome: loader.Restarted}}
	w.progress(t.Context(), reload.Result{ID: 1, Status: reload.RolledBack, Stage: reload.StagePublish, Applied: restarted,
		Rollback: &reload.Rollback{Applied: restarted}})
	x.die <- struct{}{} // one transient failure of the restored config
	if e := w.decision(); e.msg != "fiber.retry" || e.attrs["failures"] != int64(1) {
		t.Fatalf("%s %v, want the first failure of the restored config", e.msg, e.attrs)
	}
	if len(w.exits()) != 0 {
		t.Errorf("exits = %v", w.exits())
	}
}

// A restart of restart-policy does not restart the stable period of a fiber that has
// been ACTIVE all along: a failure past the stable period is a first one (D22).
func TestARestartOfRestartPolicyKeepsTheStablePeriod(t *testing.T) {
	w := newWorld(t, conf(1, 24*time.Hour, time.Second, time.Minute, 2, time.Hour))
	x := newFlaky("aiops/sql", true)
	xf := w.root.Plugin(x.plugin(), nil)
	w.retryLogged(1, time.Second)
	x.failing.Store(false)
	w.clk.Advance(time.Second)
	x.attempt(t, 2)
	if err := xf.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	w.heard() // ACTIVE from now, after one failure
	w.clk.Advance(50 * time.Minute)
	w.restart(conf(1, 24*time.Hour, time.Second, 2*time.Minute, 2, time.Hour))
	w.clk.Advance(20 * time.Minute) // ACTIVE for 70 minutes
	x.die <- struct{}{}
	if e := w.decision(); e.msg != "fiber.retry" || e.attrs["failures"] != int64(1) {
		t.Fatalf("%s %v, want the first failure after a stable period", e.msg, e.attrs)
	}
}

// A reference in a numeric or duration field resolves to text that may be a secret:
// a refusal names the field and the type, never the value.
func TestDecodeErrorsDoNotEchoTheValue(t *testing.T) {
	const secret = "hunter2-db-password"
	w := newWorld(t, conf(1, time.Second, time.Second, time.Second, 1, 0))
	for _, k := range []string{"maxFailures", "factor", "window", "backoffMin", "backoffMax", "stable"} {
		raw := map[string]any{"maxFailures": int64(3), "window": "10m", "backoffMin": "1s", "backoffMax": "1m",
			"factor": int64(2), "infrastructure": []any{"sql"}, k: secret}
		err := w.root.Plugin(restartpolicy.New(), raw).Await(t.Context())
		if err == nil || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), k) {
			t.Errorf("%s: err = %v, want a refusal that names the field and not the value", k, err)
		}
	}
}

// stall holds the observer dispatcher on the first event after it is armed.
type stall struct {
	armed   atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (s *stall) plugin() kernel.Plugin {
	return kernel.Define(kernel.Plugin{ID: "aiops/stall",
		Hooks: []kernel.HookUse{kernel.InternalStatus.Use(kernel.DefaultStage)},
		Apply: func(c *kernel.Context, _ any) error {
			kernel.InternalStatus.On(c, kernel.DefaultStage, func(_ context.Context, e kernel.LedgerEvent) (any, error) {
				if s.armed.CompareAndSwap(true, false) {
					close(s.entered)
					<-s.release
				}
				return nil, nil
			})
			return nil
		}})
}

// A failure whose event was lost is found again from the committed tree.
func TestOverflowNoticeFindsAFailureThatWasLost(t *testing.T) {
	const queue = 4
	w := newWorld(t, conf(3, time.Hour, time.Second, time.Minute, 2, 0),
		kerneltest.WithConfig(func(c *kernel.Config) { c.ObserverQueue = queue }))
	s := &stall{entered: make(chan struct{}), release: make(chan struct{})}
	kerneltest.Mount(w.root, s.plugin())
	w.heard()
	s.armed.Store(true)
	ticks := 0
	tick := func() *kernel.Fiber {
		ticks++
		return w.root.Plugin(kernel.Define(kernel.Plugin{ID: fmt.Sprintf("aiops/tick-%d", ticks),
			Apply: func(*kernel.Context, any) error { return nil }}), nil)
	}
	// Make events until the dispatcher is held inside one. With a queue this small the
	// relay may have been detached by an overflow of its own at setup, and the events of
	// that time reach no listener.
held:
	for {
		select {
		case <-s.entered:
			break held
		case <-t.Context().Done():
			t.Fatal("the dispatcher was never held")
		default:
			if err := tick().Await(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
	}

	lost := newFlaky("aiops/sql", true)
	f := w.root.Plugin(lost.plugin(), nil)
	if err := f.Await(t.Context()); err == nil {
		t.Fatal("the fiber did not fail")
	}
	w.comp.Commit(composition.Generation{Scope: composition.ScopeProcess, Rows: []composition.Row{
		{ID: "lost", Plugin: "aiops/sql", Fiber: f.ID(), State: kernel.Failed},
		{ID: "fine", Plugin: "aiops/other", Fiber: 9999, State: kernel.Active}, // not failed: ignored
		{ID: "unmounted", Plugin: "aiops/other", State: kernel.Failed},         // no fiber: ignored
	}})
	for range queue * 3 { // overflow the queue behind the held event
		if err := tick().Await(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	close(s.release)
	// The kernel subscribes again once the stalled listener returns, and the notice that
	// events were lost comes with the first event after that: make events until
	// restart-policy has found the failure that was lost.
	for {
		select {
		case e := <-w.logs:
			if e.msg == "fiber.retry" && e.attrs["fiber"] == f.ID() {
				if e.attrs["failures"] != int64(1) {
					t.Errorf("retry = %v", e.attrs)
				}
				return
			}
		case <-t.Context().Done():
			t.Fatal("the failure whose event was lost was not found again")
		default:
			if err := tick().Await(t.Context()); err != nil {
				t.Fatal(err)
			}
			w.heard()
		}
	}
}

func TestDecode(t *testing.T) {
	good := map[string]any{"maxFailures": int64(3), "window": "10m", "backoffMin": "1s", "backoffMax": "1m",
		"factor": 2.0, "stable": "30s", "infrastructure": []any{"sql", "http"}}
	with := func(k string, v any) map[string]any {
		m := map[string]any{}
		for kk, vv := range good {
			m[kk] = vv
		}
		if v == nil {
			delete(m, k)
		} else {
			m[k] = v
		}
		return m
	}
	for _, tc := range []struct {
		name string
		raw  any
		ok   bool
	}{
		{"map", good, true},
		{"map with int and no stable", with("stable", nil), true},
		{"struct", conf(1, time.Second, time.Second, time.Second, 1, 0), true},
		{"nil", nil, false},
		{"wrong type", 3, false},
		{"unknown field", with("bogus", 1), false},
		{"no failures allowed", with("maxFailures", int64(0)), false},
		{"fractional count", with("maxFailures", 1.5), false},
		{"count as int", with("maxFailures", 2), true},
		{"count as text", with("maxFailures", "2"), false},
		{"window missing", with("window", nil), false},
		{"window not a string", with("window", 5), false},
		{"window malformed", with("window", "soon"), false},
		{"backoff min zero", with("backoffMin", "0s"), false},
		{"backoff max below min", with("backoffMax", "500ms"), false},
		{"factor zero", with("factor", int64(0)), false},
		{"stable negative", with("stable", "-1s"), false},
		{"no infrastructure", with("infrastructure", nil), false},
		{"infrastructure not a list", with("infrastructure", "sql"), false},
		{"infrastructure item not a name", with("infrastructure", []any{1}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t, conf(1, time.Second, time.Second, time.Second, 1, 0))
			err := w.root.Plugin(restartpolicy.New(), tc.raw).Await(t.Context())
			if (err == nil) != tc.ok {
				t.Errorf("err = %v, want ok = %v", err, tc.ok)
			}
		})
	}
}

// bound is a kernel with the binding maps on. The launch keys are provider-bound;
// appExit and the registry are consumer-bound; internal/status is sealed.
func bound(t *testing.T, launcher kernel.Plugin, consumers, listeners []kernel.Plugin) *kernel.Root {
	t.Helper()
	const n = 64
	r, err := kernel.New(kernel.Config{
		LoadDeadline: 10 * time.Second, DrainDeadline: 10 * time.Second, DisposeDeadline: 10 * time.Second,
		ArchiveLimit: n, EventWindow: n, ObserverQueue: n, OwnerInbox: n, TerminalLimit: n,
		Providers: kernel.Bindings{
			launch.ProfileContextKey: {launcher}, launch.CatalogKey: {launcher}, launch.CmdlineKey: {launcher},
			launch.ReadyKey: {launcher}, launch.KillSwitchesKey: {launcher}, launch.AppExitKey: {launcher},
		},
		Consumers: kernel.Bindings{launch.AppExitKey: consumers, kernel.RegistryKey: consumers},
		Listeners: map[kernel.AnyHook][]kernel.Plugin{kernel.InternalStatus: listeners},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(context.Background()) })
	return r
}

func TestBindingMapsMustListTheRow(t *testing.T) {
	for _, tc := range []struct {
		name               string
		consumers, listens bool
		want               error
	}{
		{"listed everywhere", true, true, nil},
		{"not a registry and appExit consumer", false, true, kernel.ErrBinding},
		{"not a listener of the sealed hook", true, false, kernel.ErrSealed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, lp := launchfake.New()
			_, kp := clockfake.New(time.Time{})
			_, cp := compfake.New()
			rp := restartpolicy.New()
			var consumers, listeners []kernel.Plugin
			if tc.consumers {
				consumers = []kernel.Plugin{rp}
			}
			if tc.listens {
				listeners = []kernel.Plugin{rp}
			}
			root := bound(t, lp, consumers, listeners)
			for _, p := range []kernel.Plugin{lp, kp, cp} {
				if err := root.Plugin(p, nil).Await(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			err := root.Plugin(rp, conf(1, time.Second, time.Second, time.Second, 1, 0)).Await(t.Context())
			if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// A row activates with no order, so a fiber may fail before restart-policy listens:
// its event reached no one, and it must be retried all the same.
func TestRetriesAFiberThatFailedBeforeRestartPolicyWasActive(t *testing.T) {
	logs := make(chan entry, 64)
	root := kerneltest.New(t, kerneltest.WithConfig(func(c *kernel.Config) { c.LogHandler = logTap{logs} }))
	l, lp := launchfake.New()
	l.Modules = []launch.CatalogEntry{{ID: "aiops/other"}}
	clk, kp := clockfake.New(time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC))
	comp, cp := compfake.New()
	kerneltest.Mount(root, lp, kp, cp)
	fl := newFlaky("aiops/other", true)
	f := root.Plugin(fl.plugin(), nil)
	if err := f.Await(t.Context()); err == nil {
		t.Fatal("the flaky row applied")
	}
	comp.Commit(composition.Generation{Scope: composition.ScopeProcess, Rows: []composition.Row{
		{ID: "late", Plugin: "aiops/other", Fiber: f.ID(), State: kernel.Failed}}})

	rp := root.Plugin(restartpolicy.New(), conf(3, time.Hour, time.Second, time.Minute, 2, 0))
	if err := rp.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	for e := range logs {
		if e.msg == "fiber.retry" {
			if e.attrs["fiber"] != f.ID() || e.attrs["failures"] != int64(1) {
				t.Fatalf("retry = %v", e.attrs)
			}
			break
		}
	}
	fl.failing.Store(false)
	clk.Advance(time.Second)
	fl.attempt(t, 2)
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
}
