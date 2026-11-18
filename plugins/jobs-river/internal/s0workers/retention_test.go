// SPDX-License-Identifier: Apache-2.0
package s0workers

import (
	"context"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// TestRiverCleanerBoundedRetention shows, on the fixture's role design (ADR-045,
// 2026-11-18: no RLS on River's tables, jobs role without ownership), that
// River's own cleaner enforces bounded retention of finished jobs when it runs
// under the elected leader as the jobs role: old completed, cancelled and
// discarded jobs are removed, a recently finished job and unfinished jobs are
// kept, and the caller-owned result table, which the cleaner has no privilege on,
// is untouched. Retention applies per state through River's configuration.
func TestRiverCleanerBoundedRetention(t *testing.T) {
	ctx, cancel := caseContext(t)
	defer cancel()
	f := openFixture(t, ctx)
	const retention = 10 * time.Minute
	job := table(f.jobs, "river_job")
	kind := workerArgs{}.Kind()
	// Finalized an hour ago (older than retention): one per finished state.
	for _, state := range []string{"completed", "cancelled", "discarded"} {
		_, err := f.admin.Exec(ctx, "INSERT INTO "+job+" (state,queue,kind,args,max_attempts,attempt,finalized_at,created_at,scheduled_at,tenant,scope_id) VALUES ($1::"+ident(f.jobs)+".river_job_state,'idle',$2,jsonb_build_object('idempotency_id','old-'||$1::text),25,1,now()-interval '1 hour',now()-interval '1 hour',now()-interval '1 hour','t','s')", state, kind)
		must(t, safeError("seed old "+state, err))
	}
	// Kept: finished a minute ago (inside retention) and two unfinished jobs, however old.
	_, err := f.admin.Exec(ctx, "INSERT INTO "+job+" (state,queue,kind,args,max_attempts,attempt,finalized_at,created_at,scheduled_at,tenant,scope_id) VALUES ('completed','idle',$1,jsonb_build_object('idempotency_id','recent'),25,1,now()-interval '1 minute',now()-interval '1 hour',now()-interval '1 hour','t','s')", kind)
	must(t, safeError("seed recent", err))
	_, err = f.admin.Exec(ctx, "INSERT INTO "+job+" (state,queue,kind,args,max_attempts,attempt,created_at,scheduled_at,tenant,scope_id) VALUES ('available','idle',$1,jsonb_build_object('idempotency_id','pending'),25,0,now()-interval '1 day',now()-interval '1 day','t','s'),('retryable','idle',$1,jsonb_build_object('idempotency_id','retry'),25,1,now()-interval '1 day',now()+interval '1 day','t','s')", kind)
	must(t, safeError("seed unfinished", err))
	_, err = f.admin.Exec(ctx, "INSERT INTO "+table(f.data, "result")+" (job_id,tenant,scope_id,idempotency_id) SELECT id,tenant,scope_id,args->>'idempotency_id' FROM "+job+" WHERE finalized_at IS NOT NULL")
	must(t, safeError("seed results", err))

	pool := f.workerPool(t, ctx)
	d := &dispatcher{pool: pool, schema: f.jobs, snooze: 10 * time.Millisecond, deliveries: make(chan delivery, 16)}
	workers := river.NewWorkers()
	river.AddWorker(workers, d)
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Schema: f.jobs, ID: "s0wk-" + f.id + "-cleaner", Logger: quiet, Workers: workers,
		Queues:        map[string]river.QueueConfig{queue: {MaxWorkers: 1}},
		FetchCooldown: time.Millisecond, FetchPollInterval: 5 * time.Millisecond,
		JobTimeout: 10 * time.Second, SoftStopTimeout: 2 * time.Second,
		RescueStuckJobsAfter: time.Minute, TestOnly: true,
		CancelledJobRetentionPeriod: retention, CompletedJobRetentionPeriod: retention, DiscardedJobRetentionPeriod: retention,
		ReindexerIndexNames: []string{},
	})
	must(t, safeError("cleaner client", err))
	must(t, safeError("start cleaner client", client.Start(ctx)))
	report.WorkerStarted = true
	defer func() {
		stop, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := client.StopAndCancel(stop); err != nil {
			t.Error("cleaner client stop did not finish: ", sqlState(err))
		}
	}()

	// The cleaner runs only in the leader; this client is the only one. Its first
	// pass follows leadership at once (no staggered start under TestOnly).
	deadline := time.Now().Add(30 * time.Second)
	var finished int
	for {
		must(t, safeError("count finished", f.admin.QueryRow(ctx, "SELECT count(*) FROM "+job+" WHERE state IN ('completed','cancelled','discarded') AND finalized_at < now()-interval '10 minutes'").Scan(&finished)))
		if finished == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cleaner did not remove %d finished jobs older than the retention within 30 s", finished)
		}
		time.Sleep(100 * time.Millisecond)
	}
	var kept []string
	must(t, safeError("kept ids", f.admin.QueryRow(ctx, "SELECT coalesce(array_agg(args->>'idempotency_id' ORDER BY args->>'idempotency_id'),ARRAY[]::text[]) FROM "+job).Scan(&kept)))
	if len(kept) != 3 || kept[0] != "pending" || kept[1] != "recent" || kept[2] != "retry" {
		t.Fatalf("retained jobs after cleaning: %v; want exactly the recent finished job and the two unfinished ones", kept)
	}
	var results int
	must(t, safeError("results", f.admin.QueryRow(ctx, "SELECT count(*) FROM "+table(f.data, "result")).Scan(&results)))
	if results != 4 {
		t.Fatalf("caller-owned result rows changed by the cleaner: %d, want 4", results)
	}
}
