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
	Stable      time.Duration // a fiber ACTIVE this long has its failures forgotten; 0 forgets at once
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

// Retrier is the rule at work. Status is its one input.
type Retrier struct {
	Policy
	Deps

	mu   sync.Mutex
	recs map[uint64]*record
}

// record is what is known of one fiber that failed.
type record struct {
	fails    []time.Time
	pending  bool      // a retry is scheduled and has not fired
	gaveUp   bool      // the fiber stays FAILED until someone updates it
	activeAt time.Time // when the fiber was last seen ACTIVE; zero while it is not
}

// New returns a retrier with no record.
func New(p Policy, d Deps) *Retrier {
	return &Retrier{Policy: p, Deps: d, recs: map[uint64]*record{}}
}

// Status handles one event of internal/status, which is a state change or the notice
// that events were lost (ADR-034, "S1 additions").
func (r *Retrier) Status(ctx context.Context, e kernel.LedgerEvent) {
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
		r.updated(e.Fiber)
	}
}

// failed counts one failure of fiber id and schedules the retry, or gives up.
func (r *Retrier) failed(ctx context.Context, id uint64, plugin string) {
	f, ok := r.Fibers.Fiber(id)
	if !ok {
		r.forget(id)
		return
	}
	if st, err := f.State(ctx); err != nil || st != kernel.Failed {
		return // a stale event: the fiber has moved on, and its own events say where
	}
	now := r.Clock.Now()
	r.mu.Lock()
	rec := r.recs[id]
	if rec == nil {
		rec = &record{}
		r.recs[id] = rec
	}
	if rec.gaveUp || rec.pending {
		r.mu.Unlock() // a repeated event, from a resync that crossed a live one
		return
	}
	if !rec.activeAt.IsZero() && now.Sub(rec.activeAt) >= r.Stable {
		rec.fails = nil
	}
	rec.activeAt = time.Time{}
	rec.fails = append(slices.DeleteFunc(rec.fails, func(t time.Time) bool { return now.Sub(t) > r.Window }), now)
	n := len(rec.fails)
	if n > r.MaxFailures {
		rec.gaveUp = true
		r.mu.Unlock()
		r.giveUp(id, plugin, n)
		return
	}
	rec.pending = true
	r.mu.Unlock()

	delay := r.backoff(n)
	t, err := r.Clock.After(r.Context, delay)
	if err != nil {
		r.clearPending(id) // the restart-policy fiber is unloading
		return
	}
	r.Log.Info("fiber.retry", "fiber", id, "plugin", plugin, "failures", n, "delay", delay)
	r.Context.Go("restart-policy retry", func(ctx context.Context) error {
		select {
		case <-t.C():
			r.retry(ctx, id)
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

// retry runs when the timer of a failure fires.
func (r *Retrier) retry(ctx context.Context, id uint64) {
	if !r.clearPending(id) {
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
	// event of the fiber. A refusal means the fiber was disposed since the check.
	err := f.Update(ctx, nil)
	r.Log.Debug("fiber.retried", "fiber", id, "err", err)
}

// clearPending ends the wait for fiber id and reports whether the fiber is still
// tracked: a record is dropped when its fiber is disposed or gone.
func (r *Retrier) clearPending(id uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := r.recs[id]
	if rec == nil || rec.gaveUp {
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

// updated handles a fiber that is loading, pending or unloading. One that was given
// up on is being updated by someone else, so it is tracked afresh.
func (r *Retrier) updated(id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rec := r.recs[id]; rec != nil && rec.gaveUp {
		delete(r.recs, id)
	}
}

func (r *Retrier) forget(id uint64) {
	r.mu.Lock()
	delete(r.recs, id)
	r.mu.Unlock()
}

// resync answers the notice that events were lost. It drops the fibers it tracks that
// are gone, notes the ones that are ACTIVE, and counts the FAILED fibers of the committed tree that it has
// not heard of. The plugin has no Root, so the tree is read through the composition
// key, whose rows carry the fiber id. Events that follow the notice are all newer than
// it, and an event for a state the fiber has left is ignored by failed.
func (r *Retrier) resync(ctx context.Context) {
	r.mu.Lock()
	ids := make([]uint64, 0, len(r.recs))
	for id := range r.recs {
		ids = append(ids, id)
	}
	r.mu.Unlock()
	for _, id := range ids {
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
		}
	}
	g, _ := r.Rows.Committed(composition.ScopeProcess)
	for _, row := range g.Rows {
		if row.State == kernel.Failed && row.Fiber != 0 {
			r.failed(ctx, row.Fiber, row.Plugin)
		}
	}
}
