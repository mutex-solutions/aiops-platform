// SPDX-License-Identifier: Apache-2.0
package s0workers

// TestWorkerRestartFullGate runs the frozen S6 in-process resource restart probe
// (docs/design/s0-spike-criteria.md S6, with the S4 protocol). It is opt-in:
//
//	S0_WORKER_RESTART_FULL=1 GOMAXPROCS=8 go test -count=1 -timeout 6h -v -run '^TestWorkerRestartFullGate$' ./plugins/jobs-river/internal/s0workers
//
// plus the fixture contract (S0_ADMIN_DSN, S0_EXPECT_DATABASE). Reduced-scale
// knobs (S0_WORKER_RESTART_FULL_CYCLES, _COMBINED_CYCLES, _WARMUP_SECONDS,
// _WINDOW_SECONDS, _RECOVERY_IDS) only shrink the run; the same bounds apply but
// the report is then labelled NOT acceptance. S0_WORKER_RESTART_FULL_FAILFAST=1
// stops at the first failed cycle.
//
// S0WORKERS_RESTART_OWNER selects who owns the pool, River clients and listener:
// "library" (default; plain library objects) or "kernel" (three kernel fibers, see
// restart_full_kernel_test.go). The owner is in the report, the classification and
// every cycle line.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fullCycleSummary struct {
	ResourceOwner       string          `json:"resource_owner"`
	Arm                 string          `json:"arm"`
	Cycle               int             `json:"cycle"`
	Scheduled           int             `json:"scheduled_ids"`
	Acked               int             `json:"acknowledged_ids"`
	Durable             int             `json:"durably_completed_ids"`
	Unacked             int             `json:"unacknowledged_ids"`
	DurableUnacked      int             `json:"durable_without_ack_ids"`
	TimedOut            int             `json:"operation_timeouts"`
	HTTPAttempts        int             `json:"http_attempts"`
	Retries             int             `json:"http_retries_same_id"`
	Failures            map[string]int  `json:"failed_attempts_by_class"`
	Status503           int             `json:"http_503"`
	TransportErrors     int             `json:"transport_errors"`
	RetryAfterCommit    int             `json:"duplicate_acks_after_retry"`
	DuplicateLogical    int             `json:"duplicate_logical_results"`
	HandledRedelivered  int             `json:"handler_redeliveries"`
	ReplayInsertedFalse int             `json:"handler_replays_result_already_present"`
	RiverRetries        int             `json:"river_retries"`
	RiverErrorEntries   int             `json:"river_error_entries"`
	RiverAttemptsMax    int             `json:"river_attempts_max"`
	ScannerMissed       int             `json:"completion_found_only_by_final_sweep"`
	ScanErrors          int             `json:"scan_errors"`
	RestartRequestedAt  string          `json:"restart_requested_utc"`
	ResourceReadyMs     float64         `json:"resource_recreated_ms"`
	RecoveryMs          float64         `json:"recovery_ms"`
	PriorAckWorstMs     float64         `json:"prior_ack_completion_after_recovery_worst_ms"`
	AckToCompleteMaxMs  float64         `json:"ack_to_completion_max_ms"`
	JobsRemoved         int             `json:"jobs_removed_before_final_sweep"`
	RiverSeqScans       int64           `json:"river_job_seq_scans_in_cycle"`
	RiverSeqTupRead     int64           `json:"river_job_seq_tuples_read_in_cycle"`
	RiverLiveRows       int64           `json:"river_job_live_rows_after_cycle"`
	RiverTotalBytes     int64           `json:"river_job_total_bytes_after_cycle"`
	AckLatencyP50Ms     float64         `json:"ack_latency_from_scheduled_p50_ms"`
	AckLatencyP95Ms     float64         `json:"ack_latency_from_scheduled_p95_ms"`
	AckLatencyP99Ms     float64         `json:"ack_latency_from_scheduled_p99_ms"`
	AckLatencyMaxMs     float64         `json:"ack_latency_from_scheduled_max_ms"`
	ArrivalLagP99Ms     float64         `json:"arrival_start_lag_p99_ms"`
	ArrivalLagMaxMs     float64         `json:"arrival_start_lag_max_ms"`
	ArrivalsOver10ms    int             `json:"arrivals_started_over_10ms_late"`
	RecoveryIDs         recoveryIDStats `json:"recovery_new_ids"`
	MaxConcurrent       int             `json:"max_concurrent_requests"`
	CapDeferred         int64           `json:"concurrency_cap_deferred_attempts"`
	HeldHTTPAtRestart   int64           `json:"held_http_transactions_at_restart_request"`
	HeldWorkerAtRestart int64           `json:"held_worker_handlers_at_restart_request"`
	HeldHTTPTotal       int64           `json:"held_http_transactions_cycle_total"`
	HeldWorkerTotal     int64           `json:"held_worker_handlers_cycle_total"`
	Orphans             int             `json:"orphans"`
	LivePools           int64           `json:"live_pools_after_cycle"`
	LiveClients         int64           `json:"live_worker_clients_after_cycle"`
	LiveListeners       int64           `json:"live_listeners_after_cycle"`
	Goroutines          int             `json:"goroutines_after_cycle"`
	OpenFDs             int             `json:"open_fds_after_cycle"`
	Kernel              *kernelCycle    `json:"kernel,omitempty"`    // the kernel's own record of this cycle's restart (kernel owner only)
	Section10           *s10Snapshot    `json:"section10,omitempty"` // checkpoint taken after this cycle, between cycles
	Bounds              []boundResult   `json:"bounds"`
	Pass                bool            `json:"pass"`
}

type fullArmSummary struct {
	ResourceOwner    string  `json:"resource_owner"`
	Arm              string  `json:"arm"`
	CyclesPlanned    int     `json:"cycles_planned"`
	CyclesExecuted   int     `json:"cycles_executed"`
	CyclesPassed     int     `json:"cycles_passed"`
	Scheduled        int     `json:"scheduled_ids"`
	Acked            int     `json:"acknowledged_ids"`
	Durable          int     `json:"durably_completed_ids"`
	DurableUnacked   int     `json:"durable_without_ack_ids"`
	Unacked          int     `json:"unacknowledged_ids"`
	DuplicateLogical int     `json:"duplicate_logical_results"`
	Retries          int     `json:"http_retries_same_id"`
	Status503        int     `json:"http_503"`
	TransportErrors  int     `json:"transport_errors"`
	HandlerRedeliv   int     `json:"handler_redeliveries"`
	RiverRetries     int     `json:"river_retries"`
	RecoveryMsMax    float64 `json:"recovery_ms_max"`
	AckToCompleteMs  float64 `json:"ack_to_completion_max_ms"`
	Section10        string  `json:"section10_label"`
}

type fullReport struct {
	ResourceOwner    string             `json:"resource_owner"`
	Classification   string             `json:"classification"`
	Status           string             `json:"status"`
	Started          time.Time          `json:"started_utc"`
	Ended            time.Time          `json:"ended_utc"`
	PID              int                `json:"pid"`
	GoVersion        string             `json:"go_version"`
	GOMAXPROCS       int                `json:"gomaxprocs"`
	RaceInstrumented bool               `json:"race_instrumented"`
	Config           map[string]any     `json:"config"`
	AcceptanceScale  bool               `json:"frozen_scale_run"`
	WorkerStarted    bool               `json:"real_worker_started"`
	Arms             []fullArmSummary   `json:"arms"`
	Cycles           []fullCycleSummary `json:"cycles"`
	Cases            []caseResult       `json:"cases"`
	Before           map[string]string  `json:"source_before_sha256"`
	After            map[string]string  `json:"source_after_sha256"`
	Frozen           map[string]any     `json:"frozen_requirements"`
	Interpretations  []string           `json:"interpretations"`
	Limits           []string           `json:"limits"`
	Section10Label   string             `json:"section10_label"`
	Section10        *s10Report         `json:"section10"`
}

var fullSourceFiles = []string{"fixture_test.go", "worker_test.go", "diagnostic_test.go", "report_test.go", "restart_test.go", "restart_report_test.go",
	"restart_full_pure_test.go", "restart_full_app_test.go", "restart_full_test.go", "restart_full_unit_test.go", "restart_full_race_test.go", "restart_full_norace_test.go",
	"restart_leak_pure_test.go", "restart_leak_app_test.go", "restart_leak_unit_test.go", "restart_leak_db_test.go",
	"restart_full_kernel_test.go", "restart_full_kernel_unit_test.go", "restart-README.md"}

func fullSource(t *testing.T) map[string]string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("full restart source identity unavailable")
	}
	out := map[string]string{}
	for _, name := range fullSourceFiles {
		hash, err := hashFile(filepath.Join(filepath.Dir(source), name))
		must(t, safeError("full restart source hash", err))
		out[name] = hash
	}
	return out
}

func newFullReport(t *testing.T, cfg fullConfig, owner string) *fullReport {
	r := &fullReport{
		ResourceOwner: owner,
		Started:       time.Now().UTC(), PID: os.Getpid(), GoVersion: runtime.Version(), GOMAXPROCS: runtime.GOMAXPROCS(0), RaceInstrumented: raceInstrumented,
		AcceptanceScale: cfg.Acceptance() && !raceInstrumented && runtime.GOMAXPROCS(0) == fullGOMAXPROCS, Before: fullSource(t),
		Config: map[string]any{"cycles_per_kind": cfg.CyclesPerKind, "combined_cycles": cfg.Combined, "warmup_s": cfg.Warmup.Seconds(), "window_s": cfg.Window.Seconds(),
			"restart_at_s_from_start": cfg.RestartAt().Seconds(), "recovery_ids": cfg.RecoveryIDs, "diagnostic_pregrow_rows": cfg.PregrowRows, "diagnostic_login_role_enable_seqscan_off": cfg.JobsNoSeqScan, "job_retention_s": cfg.JobRetention.Seconds(), "rate_per_s": fullRate, "hold_ms": fullHold.Milliseconds(),
			"operation_timeout_s": fullOpTimeout.Seconds(), "pool_conns": fullPoolConns, "worker_max": fullWorkers, "resource_owner": owner},
		Frozen: map[string]any{"cycles_each_pool_client_listener": frozenCyclesPerKind, "combined_cycles": frozenCombinedCycles, "recovery_seconds": 5,
			"ack_to_completion_seconds": 30, "new_ids_per_recovery": frozenRecoveryIDs, "new_ids_per_second": fullRate, "warmup_s": 30, "window_s": 120, "restart_at_s": 60, "hold_ms": 200},
		Interpretations: fullInterpretationsFor(owner), Limits: fullLimitsFor(owner), Section10: &s10Report{Rule: s10Rules},
	}
	if owner == resOwnerKernel {
		r.Config["kernel_config"] = kernelConfigReport()
	}
	firstCase := len(report.Cases)
	t.Cleanup(func() {
		r.Ended, r.After = time.Now().UTC(), fullSource(t)
		for name, before := range r.Before {
			if r.After[name] != before {
				t.Error("full restart source changed during run")
			}
		}
		r.Cases = report.Cases[firstCase:]
		r.WorkerStarted = report.WorkerStarted
		executed, passed := 0, 0
		for _, a := range r.Arms {
			executed += a.CyclesExecuted
			passed += a.CyclesPassed
		}
		var labels []string
		for _, a := range r.Section10.Arms {
			labels = append(labels, a.Label)
		}
		for i := range r.Arms {
			for _, sa := range r.Section10.Arms {
				if sa.Arm == r.Arms[i].Arm {
					r.Arms[i].Section10 = sa.Label
				}
			}
		}
		r.Section10Label = s10OverallLabel(labels, 4)
		r.Section10.Label = r.Section10Label
		want := 3*cfg.CyclesPerKind + cfg.Combined
		switch {
		case executed == 0:
			r.Classification, r.Status = "NOT measured: no full-gate cycle executed", "NOT measured"
		case t.Failed() || executed != want || passed != executed:
			r.Classification = "S0 restart full gate FAILED or INCOMPLETE; not acceptance"
			r.Status = fmt.Sprintf("FAIL: cycles passed %d, executed %d, required %d", passed, executed, want)
		case r.AcceptanceScale:
			r.Classification = "S0 restart full gate: all frozen bounds met at frozen scale; candidate evidence only, acceptance is the reviewer's decision"
			r.Status = fmt.Sprintf("PASS at frozen scale: %d/%d cycles", passed, want)
		default:
			r.Classification = "REDUCED-SCALE or non-conforming run; same bounds met but NOT acceptance"
			r.Status = fmt.Sprintf("reduced pass: %d/%d cycles", passed, want)
		}
		r.Classification = "[resource_owner=" + owner + "] " + r.Classification
		for i := range r.Arms {
			r.Arms[i].ResourceOwner = owner
		}
		data, err := json.Marshal(r)
		must(t, safeError("full restart report serialization", err))
		fmt.Printf("S0WORKERS_RESTART_FULL_REPORT %s\n", data)
	})
	return r
}

var fullInterpretations = []string{
	"S6 'listener' is the HTTP listener on the same loopback address; River's LISTEN connection is replaced only by client/pool recreation.",
	"pool: both pgx pools (caller and trusted-jobs) are closed and recreated, which necessarily recreates the River clients bound to them (riverpgxv5 binds a concrete pool). client: both River clients (insert and worker) are recreated on unchanged pools. combined: listener stop, pools+clients recreation, listener start.",
	"Order: pool arm, client arm, listener arm, then combined; each arm has its own fixture (fresh schema, roles and ledgers) and every cycle its own ledger and ID prefix; IDs persist across same-ID retries.",
	"Each cycle: warm-up then window with arrivals running throughout at 100/s; the restart request is at warm-up+window/2 (measured t=60 s); the hold flag is raised 200 ms before it so HTTP transactions and worker handlers started in that interval are in flight at the request.",
	"S4 operation timeout: 5 s from scheduled arrival including same-ID retries (100 ms backoff). A request not acknowledged by then is recorded as an operation timeout; it stays in the accounting and is never retried afterwards.",
	"Recovery time: from the restart request to the earliest durable completion (result row plus River completed state, read by SQL) of an ID whose client ACK carries the epoch published only by the recreated resources.",
	"Prior acknowledgements: every ID acknowledged before recovery completed must be durably complete within 30 s of its own ACK and no later than 30 s after the recovery instant.",
	"1,000 new IDs: the first RecoveryIDs IDs scheduled at or after the recovery instant must each be acknowledged on the first attempt with no failed attempt of any class, and complete within 30 s of their ACK. Arrival start lag over 50 ms for any of them fails the cycle.",
	"Ack-to-completion is measured from the client-observed 2xx to the first SQL observation of completion (50 ms poll plus a final sweep), so it overestimates by at most the poll interval; completion seen before the ACK counts as 0.",
	"Duplicate logical result: more than one job or one result row per idempotency ID. Duplicate attempts (HTTP retries, River retries/errors, handler redeliveries, replays finding the result present) are reported, not failed; only duplicate logical results are zero-bound.",
	"At most 32 concurrent requests is enforced structurally by a gate in front of every HTTP attempt (a request that finds no free slot waits inside its own 5 s operation timeout and its latency from the scheduled instant; each such wait is reported as concurrency_cap_deferred_attempts) and the in-flight maximum is still observed independently; an eight-connection pool per role is applied. GOMAXPROCS must be 8; race-instrumented runs are labelled not frozen-scale.",
	"Orphans: failed handler drain, StopAndCancel, pool close or listener shutdown within 5 s, plus live pool/client/listener counts and idle-in-transaction sessions of the fixture login role after each cycle.",
	"Section 10: per arm a warmed baseline (after warm-up traffic, drain, quiescence and two GCs), checkpoints after cycles 0/10/20/30 (combined 0/10; reduced arms 0 and the last cycle) taken between cycles, and a teardown check against the pre-application harness baseline. Goroutines are compared per owner/role/generation/entry, descriptors per class, entries per generation, heap against max(1 MiB, 5%); pool idle/total connections and their PostgreSQL sockets/sessions are bounded by exact relations, not by baseline equality. Label: passed only at frozen scale with everything taken, otherwise partially evaluated (reduced) or not evaluated.",
}

var fullLimits = []string{
	"Loopback synthetic HTTP in one process; no production ingress, proxy, TLS, kernel leases or final kernel effects (the S6 'kernel leases' hold has no kernel here).",
	"Listener restart uses graceful Shutdown: in-flight requests finish; abrupt connection loss and ambiguous commits are covered only by the separate smoke controls.",
	"Trusted jobs role sees all tenants and the original fixture authority FAIL is unchanged; this gate does not repair or test authority.",
	"S10 resource/leak accounting is enforced for what exists in this probe (see section10 and restart-README.md 'Section 10 requirements'); fibers, provisions, contributions, leases, pending transitions and the effect ledger have no equivalent here. goroutines_after_cycle and open_fds_after_cycle remain unbounded raw observations.",
	"Durable completion is observed by polling SQL, not by a push signal; resolution is the 50 ms poll.",
	"No database crash, power loss or exactly-once delivery claim; result rows are idempotent per job, at-least-once handler attempts are allowed.",
}

// ---- cycle machinery ----

func openFDs() int {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return -1
	}
	return len(entries)
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// scanner polls the durable ledger: a job counts only when River state is
// completed AND the worker's result row exists.
func (a *fullApp) scanner(ctx context.Context, l *opLedger, since time.Time) (stop func() (scanErrors int)) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	errs := 0
	stopCh := make(chan struct{})
	wm := since.Add(-5 * time.Second)
	wg.Add(1)
	go func() {
		defer wg.Done()
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			rows, err := a.f.admin.Query(ctx, "SELECT j.args->>'idempotency_id', j.id, j.finalized_at FROM "+table(a.f.jobs, "river_job")+" j JOIN "+table(a.f.data, "result")+" r ON r.job_id=j.id WHERE j.state='completed' AND j.finalized_at >= $1", wm.Add(-5*time.Second))
			if err == nil {
				type hit struct {
					key string
					id  int64
				}
				var hits []hit
				max := wm
				for rows.Next() {
					var key string
					var id int64
					var fin time.Time
					if err = rows.Scan(&key, &id, &fin); err != nil {
						break
					}
					hits = append(hits, hit{key, id})
					if fin.After(max) {
						max = fin
					}
				}
				if err == nil {
					err = rows.Err()
				}
				rows.Close()
				if err == nil {
					observed := time.Now().UTC()
					for _, h := range hits {
						l.durable(h.key, h.id, observed)
					}
					wm = max
				}
			}
			if err != nil && ctx.Err() == nil {
				mu.Lock()
				errs++
				mu.Unlock()
			}
			select {
			case <-stopCh:
				return
			case <-tick.C:
			}
		}
	}()
	return func() int { close(stopCh); wg.Wait(); mu.Lock(); defer mu.Unlock(); return errs }
}

// sweep reads the full SQL truth for one cycle's IDs. IDs it finds complete that the
// poll never saw are recorded durable at sweep time (late, conservative).
//
// Cost is independent of retained history: job ids come from a sequence, so every
// job and result row of this cycle has an id above idFloor (the largest id at
// cycle start) and both queries are primary-key range scans over this cycle only.
func (a *fullApp) sweep(ctx context.Context, l *opLedger, prefix string, idFloor int64) (map[string]dbRow, int, error) {
	db := map[string]dbRow{}
	rows, err := a.f.admin.Query(ctx, "SELECT j.args->>'idempotency_id', j.id, j.state::text, j.attempt, coalesce(cardinality(j.errors),0) FROM "+table(a.f.jobs, "river_job")+" j WHERE j.id > $2 AND j.args->>'idempotency_id' LIKE $1", prefix+"%", idFloor)
	if err != nil {
		return nil, 0, safeError("full sweep jobs", err)
	}
	for rows.Next() {
		var key, state string
		var id int64
		var attempt, errEntries int
		if err := rows.Scan(&key, &id, &state, &attempt, &errEntries); err != nil {
			rows.Close()
			return nil, 0, safeError("full sweep scan", err)
		}
		r := db[key]
		r.Jobs++
		r.JobID, r.Attempt, r.ErrorEntries = id, max(r.Attempt, attempt), r.ErrorEntries+errEntries
		r.Completed = r.Completed || state == "completed"
		db[key] = r
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, safeError("full sweep rows", err)
	}
	rows, err = a.f.admin.Query(ctx, "SELECT idempotency_id, count(*) FROM "+table(a.f.data, "result")+" WHERE job_id > $2 AND idempotency_id LIKE $1 GROUP BY 1", prefix+"%", idFloor)
	if err != nil {
		return nil, 0, safeError("full sweep results", err)
	}
	for rows.Next() {
		var key string
		var n int
		if err := rows.Scan(&key, &n); err != nil {
			rows.Close()
			return nil, 0, safeError("full sweep result scan", err)
		}
		r := db[key]
		r.Results = n
		db[key] = r
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, safeError("full sweep result rows", err)
	}
	observed, missed := time.Now().UTC(), 0
	for key, r := range db {
		if r.Completed && r.Results >= 1 && l.known(key) && l.durable(key, r.JobID, observed) {
			missed++
		}
	}
	return db, missed, nil
}

// pregrow seeds retained history before the cycles: n completed jobs with their
// result rows, inserted by the admin role. It exists only to reproduce a long
// run's table size quickly (S0_WORKER_RESTART_FULL_PREGROW_ROWS); it is outside
// every measured cycle.
func (a *fullApp) pregrow(ctx context.Context, n int) error {
	_, err := a.f.admin.Exec(ctx, "INSERT INTO "+table(a.f.jobs, "river_job")+" (state,queue,kind,args,max_attempts,attempt,finalized_at,created_at,scheduled_at,tenant,scope_id) SELECT 'completed',$1,$2,jsonb_build_object('tenant','pregrow','idempotency_id','pregrow-'||g),25,1,now()-interval '1 hour',now()-interval '1 hour',now()-interval '1 hour',$3,$4 FROM generate_series(1,$5) g", queue, workerArgs{}.Kind(), fullScope.tenant, fullScope.id, n)
	if err != nil {
		return safeError("pregrow jobs", err)
	}
	_, err = a.f.admin.Exec(ctx, "INSERT INTO "+table(a.f.data, "result")+" (job_id,tenant,scope_id,idempotency_id) SELECT id,tenant,scope_id,args->>'idempotency_id' FROM "+table(a.f.jobs, "river_job")+" WHERE args->>'tenant'='pregrow'")
	return safeError("pregrow results", err)
}

// idFloor is the largest job id before the cycle starts (primary-key index probe).
func (a *fullApp) idFloor(ctx context.Context) (int64, error) {
	var id int64
	err := a.f.admin.QueryRow(ctx, "SELECT coalesce(max(id),0) FROM "+table(a.f.jobs, "river_job")).Scan(&id)
	return id, safeError("full cycle id floor", err)
}

func (a *fullApp) idleInTransaction(ctx context.Context) (int, error) {
	var n int
	err := a.f.admin.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE usename=$1 AND state LIKE 'idle in transaction%'", a.f.login).Scan(&n)
	return n, safeError("full orphan transaction check", err)
}

func (a *fullApp) runCycle(t *testing.T, ctx context.Context, arm string, cycle int, cfg fullConfig) fullCycleSummary {
	t.Helper()
	sum := fullCycleSummary{ResourceOwner: a.ownerName(), Arm: arm, Cycle: cycle}
	l := newLedger()
	prefix := fmt.Sprintf("fk-%s-%s-c%02d-", a.f.id, arm, cycle)
	floor, floorErr := a.idFloor(ctx)
	must(t, floorErr)
	stats0, statsErr := a.riverStats(ctx)
	must(t, statsErr)
	start := time.Now().UTC().Add(100 * time.Millisecond)
	deferred0 := a.gate.deferred.Load()
	stopScan := a.scanner(ctx, l, start)
	redel0, replay0 := a.tally()
	heldHTTP0, heldWorker0, orphans0 := a.heldHTTP.Load(), a.heldWorker.Load(), a.orphans.Load()
	a.maxInflight.Store(0)

	var requests sync.WaitGroup
	arrivalsDone := make(chan arrivalStats, 1)
	go func() {
		arrivalsDone <- runOpenLoop(ctx, start, fullInterval, cfg.Arrivals(), func() time.Time { return time.Now().UTC() }, timerWait, func(i int, scheduled, started time.Time) {
			key := fmt.Sprintf("%s%05d", prefix, i)
			l.register(key, i, scheduled, started)
			requests.Add(1)
			go func() { defer requests.Done(); a.doRequest(ctx, l, key, scheduled) }()
		})
	}()

	restartAt := start.Add(cfg.RestartAt())
	must(t, safeError("hold timer", timerWait(ctx, restartAt.Add(-fullHold))))
	a.hold.Store(true)
	must(t, safeError("restart timer", timerWait(ctx, restartAt)))
	a.hold.Store(false)
	sum.HeldHTTPAtRestart, sum.HeldWorkerAtRestart = a.heldHTTPNow.Load(), a.heldWorkerNow.Load()
	rs, restartErr := a.restart(ctx, arm)
	must(t, restartErr)

	arrivals := <-arrivalsDone
	requests.Wait()
	// Stop new arrivals (done) and give every acknowledged ID at most 30 s from its ACK.
	for {
		pending, lastAck := l.pendingAcked()
		if pending == 0 || time.Now().After(lastAck.Add(restartCompletionBudget)) || ctx.Err() != nil {
			break
		}
		must(t, safeError("drain wait", timerWait(ctx, time.Now().Add(25*time.Millisecond))))
	}
	scanErrors := stopScan()
	db, missed, sweepErr := a.sweep(ctx, l, prefix, floor)
	recs, mismatch := l.snapshot()
	rec := reconcile(recs, db, mismatch, restartCompletionBudget)
	ready, readyOK := recoveryReady(recs, rs.Epoch, rs.Requested)
	var recovery time.Duration
	var priorLate []string
	var priorWorst time.Duration
	var recIDs recoveryIDStats
	if readyOK {
		recovery = ready.Sub(rs.Requested)
		priorWorst, priorLate = priorAckLate(recs, ready, restartCompletionBudget)
		recIDs = recoveryIDs(recs, ready, cfg.RecoveryIDs, restartCompletionBudget)
	}
	orphans := int(a.orphans.Load() - orphans0)
	if a.livePools.Load() != 2 {
		orphans++
	}
	if a.liveClients.Load() != 1 {
		orphans++
	}
	if a.liveListeners.Load() != 1 {
		orphans++
	}
	idle, idleErr := a.idleInTransaction(ctx)
	orphans += idle
	redel1, replay1 := a.tally()
	a.resetTally() // bounded retained memory: per-job delivery counts only matter within a cycle
	lagMax, lagP99, over := time.Duration(0), time.Duration(0), 0
	sortedLags := sortedCopy(arrivals.Lags)
	if len(sortedLags) > 0 {
		lagMax, lagP99 = sortedLags[len(sortedLags)-1], nearestRank(sortedLags, 0.99)
	}
	late := 0
	for _, lag := range sortedLags {
		if lag > fullInterval {
			over++
		}
		if lag > fullArrivalLate {
			late++
		}
	}
	lat := sortedCopy(rec.AckLatencies)
	in := cycleInputs{
		Cfg: cfg, Rec: rec, ReadyOK: readyOK, Recovery: recovery, PriorLate: priorLate, RecIDs: recIDs, Arrivals: arrivals,
		Orphans: orphans, HeldHTTPAtRestart: sum.HeldHTTPAtRestart, HeldWorkerAtRestart: sum.HeldWorkerAtRestart,
		ScanErrors: scanErrors, ScannerMissed: missed, MaxConcurrent: int(a.maxInflight.Load()), SamePID: os.Getpid() == fullPID,
		GOMAXPROCS: runtime.GOMAXPROCS(0), LateArrivals: late, ScanSweepOK: sweepErr == nil && idleErr == nil,
	}
	bounds := evaluateBounds(in)
	if a.kern != nil { // the kernel's own record of the restart; the library variant has none
		sum.Kernel = a.kern.cycleRecord()
		bounds = append(bounds, sum.Kernel.bounds()...)
	}
	sum.Scheduled, sum.Acked, sum.Durable, sum.Unacked, sum.DurableUnacked = rec.Scheduled, rec.Acked, rec.Durable, rec.Unacked, rec.DurableUnacked
	sum.TimedOut, sum.HTTPAttempts, sum.Retries, sum.Failures = rec.TimedOut, rec.HTTPAttempts, rec.Retries, rec.Failures
	for class, n := range rec.Failures {
		if class == "http_503" {
			sum.Status503 += n
		} else if class == "connection_refused" || class == "connection_reset" || class == "eof" || class == "other_transport" || class == "timeout" {
			sum.TransportErrors += n
		}
	}
	sum.RetryAfterCommit, sum.DuplicateLogical = rec.RetryAfterCommitAcks, len(rec.DuplicateLogical)
	sum.HandledRedelivered, sum.ReplayInsertedFalse = redel1-redel0, replay1-replay0
	sum.RiverRetries, sum.RiverErrorEntries, sum.RiverAttemptsMax = rec.RiverRetries, rec.RiverErrorEntries, rec.RiverAttemptsMax
	sum.ScannerMissed, sum.ScanErrors = missed, scanErrors
	sum.RestartRequestedAt = rs.Requested.Format(time.RFC3339Nano)
	sum.ResourceReadyMs, sum.RecoveryMs, sum.PriorAckWorstMs, sum.AckToCompleteMaxMs = ms(rs.ResourceReady.Sub(rs.Requested)), ms(recovery), ms(priorWorst), ms(rec.AckToCompleteMax)
	sum.JobsRemoved = len(rec.JobRemoved)
	if stats1, err := a.riverStats(ctx); err == nil {
		sum.RiverSeqScans, sum.RiverSeqTupRead = stats1.SeqScan-stats0.SeqScan, stats1.SeqTupRead-stats0.SeqTupRead
		sum.RiverLiveRows, sum.RiverTotalBytes = stats1.LiveTup, stats1.TotalBytes
	}
	sum.AckLatencyP50Ms, sum.AckLatencyP95Ms, sum.AckLatencyP99Ms = ms(nearestRank(lat, 0.5)), ms(nearestRank(lat, 0.95)), ms(nearestRank(lat, 0.99))
	if len(lat) > 0 {
		sum.AckLatencyMaxMs = ms(lat[len(lat)-1])
	}
	sum.ArrivalLagP99Ms, sum.ArrivalLagMaxMs, sum.ArrivalsOver10ms = ms(lagP99), ms(lagMax), over
	sum.RecoveryIDs, sum.MaxConcurrent, sum.CapDeferred = recIDs, in.MaxConcurrent, a.gate.deferred.Load()-deferred0
	sum.HeldHTTPTotal, sum.HeldWorkerTotal = a.heldHTTP.Load()-heldHTTP0, a.heldWorker.Load()-heldWorker0
	sum.Orphans, sum.LivePools, sum.LiveClients, sum.LiveListeners = orphans, a.livePools.Load(), a.liveClients.Load(), a.liveListeners.Load()
	sum.Goroutines, sum.OpenFDs = runtime.NumGoroutine(), openFDs()
	sum.Bounds, sum.Pass = bounds, allPass(bounds)
	return sum
}

// riverStats reads the planner-independent counters of river_job: sequential and
// index scan counts, live row estimate and total size. Read by the admin observer
// in autocommit statements; it never scans river_job itself.
type riverStats struct{ SeqScan, SeqTupRead, IdxScan, LiveTup, TotalBytes int64 }

func (a *fullApp) riverStats(ctx context.Context) (riverStats, error) {
	var s riverStats
	err := a.f.admin.QueryRow(ctx, `SELECT coalesce(s.seq_scan,0), coalesce(s.seq_tup_read,0), coalesce(s.idx_scan,0), coalesce(s.n_live_tup,0), pg_total_relation_size(c.oid)
		FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_stat_user_tables s ON s.relid=c.oid WHERE n.nspname=$1 AND c.relname='river_job'`, a.f.jobs).Scan(&s.SeqScan, &s.SeqTupRead, &s.IdxScan, &s.LiveTup, &s.TotalBytes)
	return s, safeError("river_job stats", err)
}

// planUsesFetchIndex reports whether an EXPLAIN of River's fetch shape reads
// river_job through its prioritized fetching index and never by sequential scan.
func planUsesFetchIndex(lines []string) bool {
	index, seq := false, false
	for _, l := range lines {
		index = index || strings.Contains(l, "river_job_prioritized_fetching_index")
		seq = seq || strings.Contains(l, "Seq Scan on river_job")
	}
	return index && !seq
}

// fetchPlan runs EXPLAIN (ANALYZE, BUFFERS) of the SELECT inside River's
// JobGetAvailable (v0.51.2) as the jobs role, in a rolled-back transaction.
func (a *fullApp) fetchPlan(ctx context.Context) ([]string, error) {
	wc := a.f.base.Copy()
	wc.MaxConns = 1
	wc.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE "+ident(a.f.jobs))
		return safeError("trusted jobs session", err)
	}
	p, err := pgxpool.NewWithConfig(ctx, wc)
	if err != nil {
		return nil, safeError("plan pool", err)
	}
	defer p.Close()
	tx, err := p.Begin(ctx)
	if err != nil {
		return nil, safeError("plan begin", err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	rows, err := tx.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS) SELECT id, args FROM "+table(a.f.jobs, "river_job")+" WHERE state = 'available' AND queue = $1::text AND ($2::text[] IS NULL OR kind = ANY($2::text[])) AND scheduled_at <= coalesce($3::timestamptz, now()) ORDER BY priority ASC, scheduled_at ASC, id ASC LIMIT $4::integer FOR UPDATE SKIP LOCKED", queue, []string(nil), (*time.Time)(nil), 10)
	if err != nil {
		return nil, safeError("plan explain", err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, safeError("plan scan", err)
		}
		lines = append(lines, l)
	}
	return lines, safeError("plan rows", rows.Err())
}

// checkFetchPlan prints the plan and, once the table is large enough for the
// planner to have a real choice, fails the run if River's fetch is not an index scan.
func (a *fullApp) checkFetchPlan(t *testing.T, ctx context.Context, phase string) {
	t.Helper()
	lines, err := a.fetchPlan(ctx)
	must(t, err)
	stats, err := a.riverStats(ctx)
	must(t, err)
	uses := planUsesFetchIndex(lines)
	line, _ := json.Marshal(map[string]any{"resource_owner": a.ownerName(), "phase": phase, "river_job_live_rows": stats.LiveTup, "uses_fetching_index": uses, "plan": lines})
	fmt.Printf("S0WORKERS_RESTART_FULL_FETCH_PLAN %s\n", line)
	if stats.LiveTup >= 10_000 && !uses {
		t.Errorf("River's fetch as the jobs role does not use the fetching index (%s, %d rows)", phase, stats.LiveTup)
	}
}

var fullPID = os.Getpid()

func TestWorkerRestartFullGate(t *testing.T) {
	if os.Getenv("S0_WORKER_RESTART_FULL") != "1" {
		t.Skip("NOT measured: set S0_WORKER_RESTART_FULL=1 explicitly (full gate is about 4.3 hours; see restart-README.md)")
	}
	cfg, err := fullConfigFromEnv(os.Getenv)
	if err != nil {
		t.Fatal("invalid full restart configuration: ", err)
	}
	if os.Getenv("S0_ADMIN_DSN") == "" || os.Getenv("S0_EXPECT_DATABASE") == "" {
		t.Fatal("full restart gate was requested but the fixture environment is incomplete: NOT measured, and not a skip")
	}
	owner, err := resourceOwnerFromEnv(os.Getenv)
	if err != nil {
		t.Fatal("invalid full restart configuration: ", err)
	}
	s10Active = s10Library
	t.Cleanup(func() { s10Active = nil })
	if owner == resOwnerKernel {
		s10Active = s10KernelSet()
	}
	failFast := os.Getenv("S0_WORKER_RESTART_FULL_FAILFAST") == "1"
	rep := newFullReport(t, cfg, owner)
	arms := []struct {
		name   string
		cycles int
	}{{"pool", cfg.CyclesPerKind}, {"client", cfg.CyclesPerKind}, {"listener", cfg.CyclesPerKind}, {"combined", cfg.Combined}}
	perCycle := cfg.CycleBound()
	for _, arm := range arms {
		runDiagnostic(t, "restart_full_"+arm.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(arm.cycles)*perCycle+10*time.Minute)
			defer cancel()
			f := openFixture(t, ctx)
			if cfg.JobsNoSeqScan {
				_, err := f.admin.Exec(ctx, "ALTER ROLE "+ident(f.login)+" SET enable_seqscan = off")
				must(t, safeError("diagnostic jobs role setting", err))
			}
			app := newFullApp(f, cfg.JobRetention)
			if owner == resOwnerKernel {
				app.kern = newKernelApp(app)
			}
			t.Cleanup(app.teardown)
			acct := newS10Arm(t, app, arm.name, arm.cycles, rep.AcceptanceScale)
			rep.Section10.Arms = append(rep.Section10.Arms, acct.rep)
			acct.harness(ctx)     // before the application exists
			defer acct.teardown() // full teardown, then counts must return to the harness baseline
			must(t, safeError("full app start", app.start(ctx)))
			report.WorkerStarted = true
			if cfg.PregrowRows > 0 {
				must(t, app.pregrow(ctx, cfg.PregrowRows))
			}
			app.checkFetchPlan(t, ctx, "before_cycles")
			sum := fullArmSummary{ResourceOwner: owner, Arm: arm.name, CyclesPlanned: arm.cycles}
			rep.Arms = append(rep.Arms, sum)
			idx := len(rep.Arms) - 1
			floor, floorErr := app.idFloor(ctx)
			must(t, floorErr)
			acct.floor = floor
			app.warmPools(ctx)
			app.warmTraffic(ctx, fmt.Sprintf("fw-%s-%s-warm-", f.id, arm.name), max(cfg.Warmup, 3*time.Second))
			acct.baseline(ctx)
			acct.checkpoint(ctx, 0)
			due := map[int]bool{}
			for _, cy := range s10CheckpointCycles(arm.cycles) {
				due[cy] = true
			}
			for cycle := 1; cycle <= arm.cycles; cycle++ {
				c := app.runCycle(t, ctx, arm.name, cycle, cfg)
				if due[cycle] {
					snap, bounds := acct.checkpoint(ctx, cycle)
					c.Section10 = snap
					c.Bounds = append(c.Bounds, bounds...)
					c.Pass = allPass(c.Bounds)
				}
				line, _ := json.Marshal(c)
				fmt.Printf("S0WORKERS_RESTART_FULL_CYCLE %s\n", line)
				rep.Cycles = append(rep.Cycles, c)
				s := &rep.Arms[idx]
				s.CyclesExecuted++
				s.Scheduled += c.Scheduled
				s.Acked += c.Acked
				s.Durable += c.Durable
				s.DurableUnacked += c.DurableUnacked
				s.Unacked += c.Unacked
				s.DuplicateLogical += c.DuplicateLogical
				s.Retries += c.Retries
				s.Status503 += c.Status503
				s.TransportErrors += c.TransportErrors
				s.HandlerRedeliv += c.HandledRedelivered
				s.RiverRetries += c.RiverRetries
				s.RecoveryMsMax, s.AckToCompleteMs = max(s.RecoveryMsMax, c.RecoveryMs), max(s.AckToCompleteMs, c.AckToCompleteMaxMs)

				if c.Pass {
					s.CyclesPassed++
					continue
				}
				for _, b := range c.Bounds {
					if !b.Pass {
						t.Errorf("%s cycle %d missed frozen bound %s (limit %s, observed %s)", arm.name, cycle, b.Name, b.Limit, b.Observed)
					}
				}
				if failFast {
					t.FailNow()
				}
			}
			app.checkFetchPlan(t, ctx, "after_cycles")
			if rep.Arms[idx].CyclesExecuted != arm.cycles {
				t.Errorf("%s executed %d of %d cycles", arm.name, rep.Arms[idx].CyclesExecuted, arm.cycles)
			}
		})
		if failFast && t.Failed() {
			break
		}
	}
}
