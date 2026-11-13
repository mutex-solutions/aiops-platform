// SPDX-License-Identifier: Apache-2.0
package s0workers

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

const queue = "s0workers"

// Only this typed, JSON-marshaled shape enters River. Tenant is deliberately
// hostile data. IdempotencyID is unique per fixture scope, NOT scope authority.
type workerArgs struct {
	Tenant        string `json:"tenant"`
	IdempotencyID string `json:"idempotency_id" river:"unique"`
}

func (workerArgs) Kind() string { return "s0workers_scope_data" }

// Provider-local and opaque outside this test package; NOT an SDK definition,
// authorization token, or SQL capability. It carries validated database data.
type callScope struct{ tenant, id string }
type scopeKey struct{}

func scopeFrom(ctx context.Context) (callScope, error) {
	s, ok := ctx.Value(scopeKey{}).(callScope)
	if !ok || s.tenant == "" || s.id == "" {
		return callScope{}, errors.New("missing worker scope data")
	}
	return s, nil
}

type deliveryRow struct {
	jobID int64
	args  workerArgs
}
type handlerFunc func(context.Context, deliveryRow) error

// The atomic slot is the live registry; River's worker registration is stable.
// A view admits calls under a lock, cancels admitted calls on revocation, and
// drains them before revoke returns. Cooperative context use is essential.
type handlerRegistry struct{ live atomic.Pointer[handlerView] }
type handlerView struct {
	mu       sync.Mutex
	active   bool
	ctx      context.Context
	cancel   context.CancelFunc
	inflight sync.WaitGroup
	fn       handlerFunc
}

func (r *handlerRegistry) install(fn handlerFunc) error {
	if fn == nil {
		return errors.New("nil handler")
	}
	ctx, cancel := context.WithCancel(context.Background())
	v := &handlerView{active: true, ctx: ctx, cancel: cancel, fn: fn}
	if !r.live.CompareAndSwap(nil, v) {
		cancel()
		return errors.New("handler already installed")
	}
	return nil
}
func (r *handlerRegistry) revoke(ctx context.Context) error {
	v := r.live.Swap(nil)
	if v == nil {
		return nil
	}
	v.mu.Lock()
	v.active = false
	v.cancel()
	v.mu.Unlock()
	drained := make(chan struct{})
	go func() { v.inflight.Wait(); close(drained) }()
	select {
	case <-drained:
		return nil
	case <-ctx.Done():
		return errors.New("handler drain budget exceeded; revocation not complete")
	}
}
func (v *handlerView) invoke(ctx context.Context, row deliveryRow) (bool, error) {
	v.mu.Lock()
	if !v.active {
		v.mu.Unlock()
		return false, nil
	}
	v.inflight.Add(1)
	cctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(v.ctx, cancel)
	v.mu.Unlock()
	defer v.inflight.Done()
	defer cancel()
	defer stop()
	if err := cctx.Err(); err != nil {
		return true, river.JobSnooze(10 * time.Millisecond)
	}
	err := v.fn(cctx, row)
	if v.ctx.Err() != nil {
		return true, river.JobSnooze(10 * time.Millisecond)
	}
	return true, err
}

type delivery struct {
	stage         string
	jobID         int64
	scope         callScope
	idempotencyID string
	inserted      bool
}
type dispatcher struct {
	river.WorkerDefaults[workerArgs]
	pool       *pgxpool.Pool
	schema     string
	registry   handlerRegistry
	snooze     time.Duration
	deliveries chan delivery
}

func (w *dispatcher) emit(ctx context.Context, d delivery) error {
	select {
	case w.deliveries <- d:
		return nil
	case <-ctx.Done():
		return safeError("delivery report canceled", ctx.Err())
	}
}
func (w *dispatcher) Work(ctx context.Context, job *river.Job[workerArgs]) error {
	var scope callScope
	// Stock River JobRow does not scan provider-added columns. Authoritative
	// scope comes from the schema's row by job ID over this worker's OWN pool.
	err := w.pool.QueryRow(ctx, "SELECT tenant,scope_id FROM "+table(w.schema, "river_job")+" WHERE id=$1", job.ID).Scan(&scope.tenant, &scope.id)
	if err != nil {
		return safeError("provider job scope lookup", err)
	}
	if scope.tenant == "" || scope.id == "" || job.Args.IdempotencyID == "" {
		return errors.New("invalid worker row data")
	}
	ctx = context.WithValue(ctx, scopeKey{}, scope)
	row := deliveryRow{jobID: job.ID, args: job.Args}
	if view := w.registry.live.Load(); view != nil {
		if admitted, err := view.invoke(ctx, row); admitted {
			return err
		}
	}
	if err := w.emit(ctx, delivery{stage: "snooze_missing", jobID: job.ID, scope: scope, idempotencyID: job.Args.IdempotencyID}); err != nil {
		return err
	}
	return river.JobSnooze(w.snooze) // no nil success, discard, or attempt consumption
}

// Result rows are the application idempotency ledger, keyed by authoritative
// River job ID, not by the untrusted args tenant. A retry cannot apply twice.
func (f *fixture) record(ctx context.Context, row deliveryRow) (bool, error) {
	scope, err := scopeFrom(ctx)
	if err != nil {
		return false, err
	}
	tx, err := f.beginCaller(ctx, scope)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.Background())
	var tenant, id, user, xid string
	if err := tx.QueryRow(ctx, "SELECT current_setting('s0wk.tenant'),current_setting('s0wk.scope_id'),current_user,pg_current_xact_id()::text").Scan(&tenant, &id, &user, &xid); err != nil {
		return false, safeError("handler transaction scope", err)
	}
	if tenant != scope.tenant || id != scope.id || user != f.caller || tenant == row.args.Tenant {
		return false, errors.New("handler transaction did not use authoritative callScope")
	}
	var gotTenant, gotID, gotXID string
	err = tx.QueryRow(ctx, "INSERT INTO "+table(f.data, "result")+" (job_id,idempotency_id) VALUES ($1,$2) ON CONFLICT (job_id) DO NOTHING RETURNING tenant,scope_id,writer_xid::text", row.jobID, row.args.IdempotencyID).Scan(&gotTenant, &gotID, &gotXID)
	inserted := !errors.Is(err, pgx.ErrNoRows)
	if err != nil && inserted {
		return false, safeError("handler ledger insert", err)
	}
	if inserted {
		if gotTenant != scope.tenant || gotID != scope.id || gotXID != xid {
			return false, errors.New("result defaults lost worker transaction scope")
		}
	} else {
		var same bool
		if err := tx.QueryRow(ctx, "SELECT tenant=$2 AND scope_id=$3 AND idempotency_id=$4 FROM "+table(f.data, "result")+" WHERE job_id=$1", row.jobID, scope.tenant, scope.id, row.args.IdempotencyID).Scan(&same); err != nil {
			return false, safeError("idempotency read", err)
		}
		if !same {
			return false, errors.New("idempotency row mismatch")
		}
	}
	return inserted, safeError("handler ledger commit", tx.Commit(ctx))
}
func (f *fixture) recordingHandler(w *dispatcher) handlerFunc {
	return func(ctx context.Context, row deliveryRow) error {
		inserted, err := f.record(ctx, row)
		if err != nil {
			return err
		}
		scope, err := scopeFrom(ctx)
		if err != nil {
			return err
		}
		return w.emit(ctx, delivery{stage: "handled", jobID: row.jobID, scope: scope, idempotencyID: row.args.IdempotencyID, inserted: inserted})
	}
}

type workerClient struct {
	client  *river.Client[pgx.Tx]
	worker  *dispatcher
	started bool
}

func (f *fixture) newWorker(t *testing.T, ctx context.Context, label string, snooze time.Duration, fetchCadence ...time.Duration) *workerClient {
	t.Helper()
	cooldown, poll := time.Millisecond, 5*time.Millisecond
	if len(fetchCadence) > 0 {
		cooldown, poll = fetchCadence[0], fetchCadence[0]
	}
	pool := f.workerPool(t, ctx)
	d := &dispatcher{pool: pool, schema: f.jobs, snooze: snooze, deliveries: make(chan delivery, 1024)}
	workers := river.NewWorkers()
	river.AddWorker(workers, d) // ONCE for this stable Kind; never remove/re-register
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Schema: f.jobs, ID: "s0wk-" + f.id + "-" + label, Logger: quiet, Workers: workers,
		Queues:        map[string]river.QueueConfig{queue: {MaxWorkers: 1}},
		FetchCooldown: cooldown, FetchPollInterval: poll,
		JobTimeout: 10 * time.Second, SoftStopTimeout: 2 * time.Second,
		RescueStuckJobsAfter: time.Minute, TestOnly: true,
		CancelledJobRetentionPeriod: -1, CompletedJobRetentionPeriod: -1, DiscardedJobRetentionPeriod: -1,
		ReindexerIndexNames: []string{},
	})
	must(t, safeError("worker client", err))
	c := &workerClient{client: client, worker: d}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := d.registry.revoke(cleanup); err != nil {
			t.Error("handler drain did not finish")
		}
		if c.started {
			if err := c.client.StopAndCancel(cleanup); err != nil {
				t.Error("worker stop did not finish: ", sqlState(err))
			}
		}
	})
	return c
}
func (c *workerClient) start(t *testing.T, ctx context.Context) {
	t.Helper()
	must(t, safeError("start worker", c.client.Start(ctx)))
	c.started = true
	report.WorkerStarted = true
}
func (c *workerClient) stop(t *testing.T, ctx context.Context) {
	t.Helper()
	stop, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	must(t, safeError("graceful worker stop", c.client.Stop(stop)))
	// A nil return is required. A timeout is never treated as completed stop.
}
