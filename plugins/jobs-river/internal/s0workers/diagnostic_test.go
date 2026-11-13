// SPDX-License-Identifier: Apache-2.0
package s0workers

import (
	"context"
	"testing"
	"time"

	"github.com/riverqueue/river"
)

const hostileTenant = "malicious-args-tenant"

func runDiagnostic(t *testing.T, name string, fn func(*testing.T)) {
	t.Helper()
	selected, skipped := false, false
	passed := t.Run(name, func(t *testing.T) {
		selected = true
		defer func() { skipped = t.Skipped() }()
		fn(t)
	})
	status := "not_selected"
	if selected {
		status = "pass"
		if !passed {
			status = "fail"
		}
		if skipped {
			status = "skipped"
		}
	}
	report.Cases = append(report.Cases, caseResult{Name: name, Status: status})
}
func awaitDelivery(t *testing.T, ctx context.Context, ch <-chan delivery, stage string, id int64) delivery {
	t.Helper()
	budget, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		select {
		case d, ok := <-ch:
			if !ok {
				t.Fatal("delivery channel closed before causal barrier")
			}
			if d.jobID == id && d.stage == stage {
				return d
			}
		case <-budget.Done():
			t.Fatal("delivery barrier timed out: ", stage)
		}
	}
}
func subscribe(c *workerClient) (<-chan *river.Event, func()) {
	return c.client.Subscribe(river.EventKindJobSnoozed, river.EventKindJobCompleted, river.EventKindJobFailed, river.EventKindJobCancelled)
}
func awaitEvent(t *testing.T, ctx context.Context, ch <-chan *river.Event, id int64, want river.EventKind) *river.Event {
	t.Helper()
	budget, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		select {
		case event, ok := <-ch:
			if !ok {
				t.Fatal("subscription closed before event barrier")
			}
			if event.Job == nil || event.Job.ID != id {
				continue
			}
			if event.Kind == river.EventKindJobFailed || event.Kind == river.EventKindJobCancelled {
				t.Fatal("worker failed/cancelled before expected event")
			}
			if event.Kind == want {
				return event
			}
		case <-budget.Done():
			t.Fatal("River event barrier timed out: ", want)
		}
	}
}
func (f *fixture) assertState(t *testing.T, ctx context.Context, id int64, want string) {
	t.Helper()
	var state string
	must(t, f.admin.QueryRow(ctx, "SELECT state::text FROM "+table(f.jobs, "river_job")+" WHERE id=$1", id).Scan(&state))
	if state != want {
		t.Fatalf("job state=%s want=%s", state, want)
	}
}
func (f *fixture) assertNotFinalized(t *testing.T, ctx context.Context, id int64) {
	t.Helper()
	var state string
	var unfinalized bool
	must(t, f.admin.QueryRow(ctx, "SELECT state::text,finalized_at IS NULL FROM "+table(f.jobs, "river_job")+" WHERE id=$1", id).Scan(&state, &unfinalized))
	if !unfinalized || state == "completed" || state == "discarded" || state == "cancelled" {
		t.Fatal("unloaded/revoked handler finalized its job")
	}
}
func (f *fixture) assertResult(t *testing.T, ctx context.Context, id int64, scope callScope, key string, want int) {
	t.Helper()
	var n int
	must(t, f.admin.QueryRow(ctx, "SELECT count(*) FROM "+table(f.data, "result")+" WHERE job_id=$1", id).Scan(&n))
	if n != want {
		t.Fatalf("logical result count=%d want=%d", n, want)
	}
	if want == 0 {
		return
	}
	var correct bool
	must(t, f.admin.QueryRow(ctx, "SELECT tenant=$2 AND scope_id=$3 AND idempotency_id=$4 FROM "+table(f.data, "result")+" WHERE job_id=$1", id, scope.tenant, scope.id, key).Scan(&correct))
	if !correct {
		t.Fatal("logical result scope/idempotency mismatch")
	}
}
func checkDelivery(t *testing.T, d delivery, scope callScope, key string, inserted bool) {
	t.Helper()
	if d.scope != scope || d.idempotencyID != key || d.inserted != inserted {
		t.Fatal("actual handler delivery lost authoritative scope or idempotency")
	}
}

// Ordinary functional diagnostic for delivery/revocation only. Nothing in this
// package measures or repairs the known-failed SQL authority boundary.
func TestWorkerScopeDataDiagnostic(t *testing.T) {
	runDiagnostic(t, "two_scopes_unique_redelivery", func(t *testing.T) {
		ctx, cancel := caseContext(t)
		defer cancel()
		f := openFixture(t, ctx)
		scopes := []callScope{{"tenant-a", "scope-a"}, {"tenant-b", "scope-b"}}
		ids := make([]int64, 2)
		keys := []string{"idempotency-a", "idempotency-b"}
		for i, scope := range scopes {
			args := workerArgs{Tenant: hostileTenant, IdempotencyID: keys[i]}
			first := f.enqueue(t, ctx, scope, args)
			duplicate := f.enqueue(t, ctx, scope, args)
			if first.UniqueSkippedAsDuplicate || !duplicate.UniqueSkippedAsDuplicate || first.Job.ID != duplicate.Job.ID {
				t.Fatal("InsertTx ByArgs uniqueness did not deduplicate")
			}
			ids[i] = first.Job.ID
			var correct bool
			must(t, f.admin.QueryRow(ctx, "SELECT tenant=$2 AND scope_id=$3 AND args->>'tenant'=$4 FROM "+table(f.jobs, "river_job")+" WHERE id=$1", ids[i], scope.tenant, scope.id, hostileTenant).Scan(&correct))
			if !correct {
				t.Fatal("provider columns were not transaction-default stamped")
			}
		}
		// Caller visibility is tenant-filtered by the original custom GUCs.
		// This is NOT a defense against their known tampering/membership escape.
		for i, scope := range scopes {
			tx, err := f.beginCaller(ctx, scope)
			must(t, err)
			defer tx.Rollback(context.Background())
			var got int64
			var count int
			must(t, tx.QueryRow(ctx, "SELECT count(*),coalesce(min(id),0) FROM "+table(f.jobs, "river_job")).Scan(&count, &got))
			if count != 1 || got != ids[i] {
				t.Error("caller scoped read returned wrong jobs")
			}
			must(t, tx.Rollback(ctx))
		}
		c := f.newWorker(t, ctx, "scope-data", 10*time.Millisecond)
		must(t, c.worker.registry.install(f.recordingHandler(c.worker)))
		events, unsubscribe := subscribe(c)
		defer unsubscribe()
		c.start(t, ctx)
		delivered := map[int64]bool{}
		for range ids {
			select {
			case d := <-c.worker.deliveries:
				i := -1
				for j, id := range ids {
					if d.jobID == id {
						i = j
					}
				}
				if i < 0 || d.stage != "handled" || delivered[d.jobID] {
					t.Fatal("unexpected or repeated initial handler delivery")
				}
				checkDelivery(t, d, scopes[i], keys[i], true)
				delivered[d.jobID] = true
			case <-ctx.Done():
				t.Fatal("two-scope handler barrier timed out")
			}
		}
		completed := map[int64]bool{}
		for len(completed) < len(ids) {
			select {
			case event, ok := <-events:
				if !ok {
					t.Fatal("completion subscription closed")
				}
				if event.Job == nil || !delivered[event.Job.ID] || event.Kind != river.EventKindJobCompleted {
					t.Fatal("unexpected two-scope worker event")
				}
				completed[event.Job.ID] = true
			case <-ctx.Done():
				t.Fatal("two-scope completion barrier timed out")
			}
		}
		for i, id := range ids {
			f.assertState(t, ctx, id, "completed")
			f.assertResult(t, ctx, id, scopes[i], keys[i], 1)
		}
		// Explicit redelivery, NOT rescue/crash simulation. Real worker invokes
		// the same handler again, while ON CONFLICT(job_id) prevents reapplying.
		_, err := c.client.JobRetry(ctx, ids[0])
		must(t, err)
		d := awaitDelivery(t, ctx, c.worker.deliveries, "handled", ids[0])
		checkDelivery(t, d, scopes[0], keys[0], false)
		awaitEvent(t, ctx, events, ids[0], river.EventKindJobCompleted)
		f.assertResult(t, ctx, ids[0], scopes[0], keys[0], 1)
		var jobs, results int
		must(t, f.admin.QueryRow(ctx, "SELECT count(*) FROM "+table(f.jobs, "river_job")).Scan(&jobs))
		must(t, f.admin.QueryRow(ctx, "SELECT count(*) FROM "+table(f.data, "result")).Scan(&results))
		if jobs != 2 || results != 2 {
			t.Fatal("duplicate enqueue or redelivery changed logical dataset cardinality")
		}
		c.stop(t, ctx)
	})
	runDiagnostic(t, "nil_handler_snoozes_then_activates", func(t *testing.T) {
		ctx, cancel := caseContext(t)
		defer cancel()
		f := openFixture(t, ctx)
		scope := callScope{"tenant-a", "nil-handler"}
		key := "nil-handler-id"
		id := f.enqueue(t, ctx, scope, workerArgs{Tenant: hostileTenant, IdempotencyID: key}).Job.ID
		c := f.newWorker(t, ctx, "nil", 10*time.Millisecond)
		events, unsubscribe := subscribe(c)
		defer unsubscribe()
		c.start(t, ctx)
		awaitDelivery(t, ctx, c.worker.deliveries, "snooze_missing", id)
		event := awaitEvent(t, ctx, events, id, river.EventKindJobSnoozed)
		if event.Job.Attempt != 0 {
			t.Error("missing handler snooze consumed an attempt")
		}
		f.assertNotFinalized(t, ctx, id)
		f.assertResult(t, ctx, id, scope, key, 0)
		must(t, c.worker.registry.install(f.recordingHandler(c.worker)))
		d := awaitDelivery(t, ctx, c.worker.deliveries, "handled", id)
		checkDelivery(t, d, scope, key, true)
		awaitEvent(t, ctx, events, id, river.EventKindJobCompleted)
		f.assertState(t, ctx, id, "completed")
		f.assertResult(t, ctx, id, scope, key, 1)
		c.stop(t, ctx)
	})
	runDiagnostic(t, "concurrent_replica_a_unloaded_b_active", func(t *testing.T) {
		ctx, cancel := caseContext(t)
		defer cancel()
		f := openFixture(t, ctx)
		scope := callScope{"tenant-b", "partial-replica"}
		key := "partial-replica-id"
		id := f.enqueue(t, ctx, scope, workerArgs{Tenant: hostileTenant, IdempotencyID: key}).Job.ID
		a := f.newWorker(t, ctx, "replica-a", 50*time.Millisecond, 250*time.Millisecond)
		b := f.newWorker(t, ctx, "replica-b", 10*time.Millisecond)
		must(t, b.worker.registry.install(f.recordingHandler(b.worker)))
		aevents, unsubscribeA := subscribe(a)
		defer unsubscribeA()
		bevents, unsubscribeB := subscribe(b)
		defer unsubscribeB()
		a.start(t, ctx)
		awaitDelivery(t, ctx, a.worker.deliveries, "snooze_missing", id)
		awaitEvent(t, ctx, aevents, id, river.EventKindJobSnoozed)
		f.assertNotFinalized(t, ctx, id)
		// A remains started and eligible to fetch this Kind for the whole B
		// progress window. This is NOT Stop(A)/Start(B) sequential handoff.
		b.start(t, ctx)
		d := awaitDelivery(t, ctx, b.worker.deliveries, "handled", id)
		checkDelivery(t, d, scope, key, true)
		awaitEvent(t, ctx, bevents, id, river.EventKindJobCompleted)
		f.assertState(t, ctx, id, "completed")
		f.assertResult(t, ctx, id, scope, key, 1)
		a.stop(t, ctx)
		b.stop(t, ctx)
	})
	runDiagnostic(t, "revoke_during_work_cancels_drains_then_reactivates", func(t *testing.T) {
		ctx, cancel := caseContext(t)
		defer cancel()
		f := openFixture(t, ctx)
		scope := callScope{"tenant-a", "dispose-inflight"}
		key := "dispose-inflight-id"
		id := f.enqueue(t, ctx, scope, workerArgs{Tenant: hostileTenant, IdempotencyID: key}).Job.ID
		c := f.newWorker(t, ctx, "dispose", 10*time.Millisecond)
		entered, canceled := make(chan callScope, 1), make(chan struct{}, 1)
		must(t, c.worker.registry.install(func(cctx context.Context, row deliveryRow) error {
			got, err := scopeFrom(cctx)
			if err != nil {
				return err
			}
			select {
			case entered <- got:
			case <-cctx.Done():
				return river.JobSnooze(10 * time.Millisecond)
			}
			<-cctx.Done() // cooperative plugin call context, not parent/River stop
			canceled <- struct{}{}
			return river.JobSnooze(10 * time.Millisecond)
		}))
		events, unsubscribe := subscribe(c)
		defer unsubscribe()
		c.start(t, ctx)
		select {
		case got := <-entered:
			if got != scope {
				t.Fatal("inflight scope mismatch")
			}
		case <-ctx.Done():
			t.Fatal("handler did not enter")
		}
		drain, budgetCancel := context.WithTimeout(ctx, 2*time.Second)
		must(t, c.worker.registry.revoke(drain))
		budgetCancel()
		select {
		case <-canceled:
		default:
			t.Fatal("revoke returned without canceling inflight plugin context")
		}
		awaitEvent(t, ctx, events, id, river.EventKindJobSnoozed)
		// An actual subsequent fetch reaches the unchanged worker, now nil.
		awaitDelivery(t, ctx, c.worker.deliveries, "snooze_missing", id)
		f.assertNotFinalized(t, ctx, id)
		f.assertResult(t, ctx, id, scope, key, 0)
		must(t, c.worker.registry.install(f.recordingHandler(c.worker)))
		d := awaitDelivery(t, ctx, c.worker.deliveries, "handled", id)
		checkDelivery(t, d, scope, key, true)
		awaitEvent(t, ctx, events, id, river.EventKindJobCompleted)
		f.assertState(t, ctx, id, "completed")
		f.assertResult(t, ctx, id, scope, key, 1)
		c.stop(t, ctx)
	})
}

// Compile-time signatures only; this does not execute River or a goroutine.
var _ river.Worker[workerArgs] = (*dispatcher)(nil)
