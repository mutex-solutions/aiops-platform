// SPDX-License-Identifier: Apache-2.0

// Package retry is the retry rule of restart-policy: "retries a FAILED fiber in place
// with backoff and gives up after a number of failures in a window; giving up on an
// infrastructure provider is one of the three cases that replace the process"
// (03-c3-kernel.md:66, :372; 07-c4-plugin-sdk.md:345, :360).
//
// A fiber is told by its id: a fiber that is replaced or removed has a new id or none,
// and the id of a disposed one answers DISPOSED, so a timer that fires for an id that
// is gone retries nothing. Every timer is an effect of the restart-policy fiber, and
// every goroutine is tracked work of it.
package retry

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	clock "github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0"
	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Fibers finds a fiber by id: the lookup of the registry built-in (ADR-059
// decision 2; ADR-034, "S1 additions"). kernel.RegistryKey serves it.
type Fibers interface {
	Fiber(id uint64) (*kernel.Fiber, bool)
}

// Policy is every number of the rule. The plugin takes them from its config.
type Policy struct {
	MaxFailures int           // a fiber is given up on at the failure after this many in Window
	Window      time.Duration // failures older than this no longer count
	BackoffMin  time.Duration // the delay before the first retry
	BackoffMax  time.Duration // the delay never grows past this
	Factor      int           // each further failure in the window multiplies the delay
	Stable      time.Duration // a fiber ACTIVE this long has its failures forgotten; 0 never: only Window counts
	// Infrastructure lists the key names whose providers are infrastructure providers
	// (03-c3-kernel.md:371: `sql`, `jobs`, `events`, `http`).
	Infrastructure []string
}

// Deps is what the rule reads and calls.
type Deps struct {
	Context *kernel.Context // the restart-policy fiber's: timers and goroutines belong to it
	Clock   clock.Clock
	Fibers  Fibers
	Catalog launch.Catalog
	Exit    launch.AppExit
	Rows    composition.Reader
	Log     *slog.Logger
}

// Retrier is the rule at work. Status and Operation are its inputs.
type Retrier struct {
	Policy
	Deps
	*Memory
}

// Memory is what the rule knows of the fibers of one kernel: the records and the
// reload operation in flight. It outlives an activation of restart-policy, so that a
// restart of the row (its config changed, its clock restarted) keeps every give-up and
// count: a fiber given up on stays FAILED until someone updates it
// (07-c4-plugin-sdk.md:360), and a hold lasts until the operation is final.
type Memory struct {
	mu   sync.Mutex
	recs map[uint64]*record
	op   context.Context // the reload goroutine's, while an operation is in flight; nil when none
}

// NewMemory returns a memory with no record.
func NewMemory() *Memory { return &Memory{recs: map[uint64]*record{}} }

// record is what is known of one fiber that failed, since restart-policy last saw
// someone else restart it.
type record struct {
	plugin   string
	fails    []time.Time
	pending  bool      // a retry is scheduled and has not fired
	retried  bool      // restart-policy updated the fiber and has not yet seen it leave FAILED
	held     bool      // past the limit while a reload operation was in flight: judged at its end
	gaveUp   bool      // the fiber stays FAILED until someone updates it
	activeAt time.Time // when the fiber was last seen ACTIVE; zero while it is not
}

// New returns a retrier that keeps its records in m.
func New(p Policy, d Deps, m *Memory) *Retrier {
	return &Retrier{Policy: p, Deps: d, Memory: m}
}

// Resume takes over the records of an activation that has ended: each retry it had
// scheduled, whose timer went with it, is scheduled again with its count.
func (r *Retrier) Resume() {
	type due struct {
		id  uint64
		rec *record
		n   int
	}
	var all []due
	r.mu.Lock()
	for id, rec := range r.recs {
		if rec.pending {
			all = append(all, due{id, rec, len(rec.fails)})
		}
	}
	r.mu.Unlock()
	for _, d := range all {
		r.schedule(d.id, d.rec, d.n)
	}
}

// Operation handles one reload/operation event. From the event of an operation that
// is about to apply a generation until the event that it is final, the rows of the
// tree are not judged, and a failure among them is rolled back when the verdict fails
// (08-profiles-roadmap-adrs.md:470, :473): a fiber past its limit is held, not given
// up on, and the exit that giving up on an infrastructure provider asks for would
// abandon the operation. When the operation is final every held fiber is judged
// again. ctx is the reload goroutine's: an operation whose final event never comes
// is over once ctx is done (see Status).
func (r *Retrier) Operation(ctx context.Context, final bool) {
	if final {
		r.end(ctx)
		return
	}
	r.mu.Lock()
	r.op = ctx
	r.mu.Unlock()
}

// end ends the operation in flight and judges the fibers it held: one still FAILED
// past its limit is given up on now; one restarted meanwhile is left to its events.
func (r *Retrier) end(ctx context.Context) {
	r.mu.Lock()
	r.op = nil
	held := map[uint64]*record{}
	for id, rec := range r.recs {
		if rec.held {
			rec.held = false
			held[id] = rec
		}
	}
	r.mu.Unlock()
	for id, rec := range held {
		if !r.isFailed(ctx, id) {
			continue
		}
		r.mu.Lock()
		if r.recs[id] != rec || rec.gaveUp || rec.pending || rec.held {
			r.mu.Unlock() // a live event decided meanwhile
			continue
		}
		r.decide(id, rec, r.Clock.Now())
	}
}

// Status handles one event of internal/status, which is a state change or the notice
// that events were lost (ADR-034, "S1 additions"). An operation of reload whose
// goroutine ended before its final event, when the reload fiber unloaded, ends here.
func (r *Retrier) Status(ctx context.Context, e kernel.LedgerEvent) {
	r.mu.Lock()
	abandoned := r.op != nil && r.op.Err() != nil
	r.mu.Unlock()
	if abandoned {
		r.end(ctx)
	}
	switch {
	case e.Kind == "overflow":
		r.resync(ctx)
	case e.State == kernel.Failed:
		r.failed(ctx, e.Fiber, e.Plugin)
	case e.State == kernel.Active:
		r.active(e.Fiber)
	case e.State == kernel.Disposed:
		r.forget(e.Fiber)
	default:
		r.updated(e)
	}
}

// failed counts one failure of fiber id, then retries it, holds it or gives up.
func (r *Retrier) failed(ctx context.Context, id uint64, plugin string) {
	if !r.isFailed(ctx, id) {
		return // a stale event: the fiber has moved on, and its own events say where
	}
	now := r.Clock.Now()
	r.mu.Lock()
	rec := r.recs[id]
	if rec == nil {
		rec = &record{plugin: plugin}
		r.recs[id] = rec
	}
	if rec.gaveUp || rec.pending || rec.held {
		r.mu.Unlock() // a repeated event, from a resync that crossed a live one
		return
	}
	r.endActive(rec, now)
	rec.retried = false // this failure ends the retry, whose restart a lost event may hide
	rec.fails = append(rec.fails, now)
	r.decide(id, rec, now)
}

// isFailed reports whether fiber id is FAILED, and forgets one that is gone.
func (r *Retrier) isFailed(ctx context.Context, id uint64) bool {
	f, ok := r.Fibers.Fiber(id)
	if !ok {
		r.forget(id)
		return false
	}
	st, err := f.State(ctx)
	return err == nil && st == kernel.Failed
}

// decide retries fiber id, holds it while a reload operation is in flight, or gives
// up on it, by the failures of rec in the window. r.mu is held; decide releases it.
func (r *Retrier) decide(id uint64, rec *record, now time.Time) {
	rec.fails = slices.DeleteFunc(rec.fails, func(t time.Time) bool { return now.Sub(t) > r.Window })
	n := len(rec.fails)
	switch {
	case n <= r.MaxFailures:
		rec.pending = true
		r.mu.Unlock()
		r.schedule(id, rec, n)
	case r.op != nil:
		rec.held = true
		r.mu.Unlock()
		r.Log.Info("fiber.give-up-held", "fiber", id, "plugin", rec.plugin, "failures", n,
			"why", "a reload operation is in flight")
	default:
		rec.gaveUp = true
		r.mu.Unlock()
		r.giveUp(id, rec.plugin, n)
	}
}

// schedule retries fiber id after the backoff of failure n.
func (r *Retrier) schedule(id uint64, rec *record, n int) {
	delay := r.backoff(n)
	t, err := r.Clock.After(r.Context, delay)
	if err != nil {
		r.clearPending(id, rec) // the restart-policy fiber is unloading
		return
	}
	r.Log.Info("fiber.retry", "fiber", id, "plugin", rec.plugin, "failures", n, "delay", delay)
	r.Context.Go("restart-policy retry", func(ctx context.Context) error {
		select {
		case <-t.C():
			r.retry(ctx, id, rec)
		case <-ctx.Done():
		}
		return nil
	})
}

// backoff is BackoffMin multiplied by Factor for each failure after the first, up to
// BackoffMax.
func (r *Retrier) backoff(n int) time.Duration {
	d := r.BackoffMin
	for i := 1; i < n && d < r.BackoffMax; i++ {
		d *= time.Duration(r.Factor)
	}
	return min(d, r.BackoffMax)
}

// retry runs when the timer of a failure of rec fires.
func (r *Retrier) retry(ctx context.Context, id uint64, rec *record) {
	if !r.clearPending(id, rec) {
		r.Log.Debug("fiber.retry-skipped", "fiber", id, "why", "forgotten")
		return
	}
	f, ok := r.Fibers.Fiber(id)
	if !ok {
		r.forget(id)
		r.Log.Debug("fiber.retry-skipped", "fiber", id, "why", "gone")
		return
	}
	if st, err := f.State(ctx); err != nil || st != kernel.Failed {
		r.Log.Debug("fiber.retry-skipped", "fiber", id, "why", "not failed", "state", st)
		return
	}
	// The update is admitted or refused at once; the restart that follows is the next
	// event of the fiber, which updated tells from a restart by someone else. A
	// refusal means the fiber was disposed since the check.
	r.mu.Lock()
	rec.retried = true
	r.mu.Unlock()
	err := f.Update(ctx, nil)
	r.Log.Debug("fiber.retried", "fiber", id, "err", err)
}

// clearPending ends the wait of rec and reports whether rec is still the record of
// fiber id: a record is dropped when its fiber is disposed, gone or restarted by
// someone else.
func (r *Retrier) clearPending(id uint64, rec *record) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.recs[id] != rec || rec.gaveUp {
		return false
	}
	rec.pending = false
	return true
}

// giveUp leaves the fiber FAILED, says so with the diagnostic of the failure matrix
// ("fiber.gave-up", 08-profiles-roadmap-adrs.md:485) and, for an infrastructure
// provider, asks the launcher to replace the process (03-c3-kernel.md:372). The exit
// request comes first, so that whoever saw the diagnostic can rely on it.
func (r *Retrier) giveUp(id uint64, plugin string, failures int) {
	infra := r.infrastructure(plugin)
	if infra {
		r.Exit.RequestExit(launch.ExitRequest{Reason: launch.ExitGaveUp, Detail: plugin})
	}
	r.Log.Error("fiber.gave-up", "fiber", id, "plugin", plugin, "failures", failures,
		"window", r.Window, "infrastructure", infra)
}

func (r *Retrier) infrastructure(plugin string) bool {
	e, ok := r.Catalog.Lookup(plugin)
	return ok && slices.ContainsFunc(e.Provides, func(k string) bool { return slices.Contains(r.Infrastructure, k) })
}

// active notes that fiber id is ACTIVE: whoever updated a fiber that was given up on
// has fixed it, and any other record starts its stable period.
func (r *Retrier) active(id uint64) {
	now := r.Clock.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	if rec := r.recs[id]; rec != nil {
		if rec.gaveUp {
			delete(r.recs, id)
			return
		}
		rec.activeAt = now
	}
}

// updated handles a fiber that is loading, pending or unloading. A fiber that leaves
// FAILED is restarted by an update: restart-policy's own retry, or someone else's (an
// operator, or reload applying or rolling back a generation, which restarts a fiber
// in place with another config). A live fiber that unloads and loads again without
// waiting PENDING is restarted by someone else too; one that waits PENDING was
// unloaded by the kernel because a provider left, and keeps its count. A fiber that
// someone else restarted is tracked afresh. For any other the ACTIVE period, if any,
// is over.
func (r *Retrier) updated(e kernel.LedgerEvent) {
	now := r.Clock.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	switch rec := r.recs[e.Fiber]; {
	case rec == nil:
	case e.From == kernel.Failed && rec.retried:
		rec.retried = false
	case rec.gaveUp, e.From == kernel.Failed, e.State == kernel.Loading && e.From == kernel.Unloading:
		delete(r.recs, e.Fiber)
	default:
		r.endActive(rec, now)
	}
}

// endActive ends the ACTIVE period of rec at now, if one is running: a period of at
// least Stable forgets the failures. r.mu is held.
func (r *Retrier) endActive(rec *record, now time.Time) {
	if r.Stable > 0 && !rec.activeAt.IsZero() && now.Sub(rec.activeAt) >= r.Stable {
		rec.fails = nil
	}
	rec.activeAt = time.Time{}
}

func (r *Retrier) forget(id uint64) {
	r.mu.Lock()
	delete(r.recs, id)
	r.mu.Unlock()
}

// resync answers the notice that events were lost. It drops the fibers it tracks that
// are gone, notes the ones that are ACTIVE, counts a lost failure of the ones that are
// FAILED, and counts the FAILED fibers of the committed tree that it has not heard of.
// The plugin has no Root, so the tree is read through the composition key, whose rows
// carry the fiber id. Events that follow the notice are all newer than it, and an
// event for a state the fiber has left is ignored by failed.
func (r *Retrier) resync(ctx context.Context) {
	r.mu.Lock()
	tracked := make(map[uint64]string, len(r.recs))
	for id, rec := range r.recs {
		tracked[id] = rec.plugin
	}
	r.mu.Unlock()
	for id, plugin := range tracked {
		f, ok := r.Fibers.Fiber(id)
		if !ok {
			r.forget(id)
			continue
		}
		switch st, err := f.State(ctx); {
		case err != nil || st == kernel.Disposed:
			r.forget(id)
		case st == kernel.Active:
			r.active(id)
		case st == kernel.Failed:
			r.failed(ctx, id, plugin) // one given up on or waiting for its retry is left as it is
		}
	}
	g, _ := r.Rows.Committed(composition.ScopeProcess)
	for _, row := range g.Rows {
		if row.State == kernel.Failed && row.Fiber != 0 {
			r.failed(ctx, row.Fiber, row.Plugin)
		}
	}
}
