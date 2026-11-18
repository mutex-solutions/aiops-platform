// SPDX-License-Identifier: Apache-2.0
package s0workers

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// These tests need no database and always run.

func TestFullConfig(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	c, err := fullConfigFromEnv(env(nil))
	if err != nil || !c.Acceptance() || c.Arrivals() != 15000 || c.RestartAt() != 90*time.Second {
		t.Fatalf("defaults must be the frozen scale: %+v %v", c, err)
	}
	for _, bad := range []map[string]string{
		{"S0_WORKER_RESTART_FULL_CYCLES": "31"},
		{"S0_WORKER_RESTART_FULL_CYCLES": "0"},
		{"S0_WORKER_RESTART_FULL_COMBINED_CYCLES": "11"},
		{"S0_WORKER_RESTART_FULL_WINDOW_SECONDS": "121"},
		{"S0_WORKER_RESTART_FULL_RECOVERY_IDS": "1001"},
		{"S0_WORKER_RESTART_FULL_WARMUP_SECONDS": "x"},
		{"S0_WORKER_RESTART_FULL_WINDOW_SECONDS": "20", "S0_WORKER_RESTART_FULL_RECOVERY_IDS": "1000"},
	} {
		if _, err := fullConfigFromEnv(env(bad)); err == nil {
			t.Fatalf("must reject %v", bad)
		}
	}
	c, err = fullConfigFromEnv(env(map[string]string{"S0_WORKER_RESTART_FULL_CYCLES": "2"}))
	if err != nil || c.Acceptance() {
		t.Fatal("a reduced scale must never be acceptance")
	}
}

func TestOpenLoopKeepsAbsoluteSchedule(t *testing.T) {
	clock := time.Unix(1000, 0)
	now := func() time.Time { return clock }
	wait := func(_ context.Context, until time.Time) error {
		if until.After(clock) {
			clock = until
		}
		return nil
	}
	start := clock
	var sched, started []time.Time
	st := runOpenLoop(context.Background(), start, 10*time.Millisecond, 8, now, wait, func(i int, s, a time.Time) {
		sched, started = append(sched, s), append(started, a)
		if i == 2 {
			clock = clock.Add(35 * time.Millisecond) // a slow fire stalls the generator
		}
	})
	if st.Fired != 8 || len(st.Lags) != 8 {
		t.Fatalf("fired=%d", st.Fired)
	}
	for i, s := range sched {
		if s != start.Add(time.Duration(i)*10*time.Millisecond) {
			t.Fatalf("arrival %d was rescheduled", i)
		}
	}
	// Arrivals 3..5 were due while stalled: fired at once, lag recorded, never skipped.
	if st.Lags[3] != 25*time.Millisecond || st.Lags[4] != 15*time.Millisecond || st.Lags[5] != 5*time.Millisecond || st.Lags[6] != 0 {
		t.Fatalf("lags %v", st.Lags)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if st := runOpenLoop(ctx, start, time.Millisecond, 5, time.Now, timerWait, func(int, time.Time, time.Time) {}); st.Fired >= st.Scheduled {
		t.Fatal("canceled schedule must report fewer fired than scheduled")
	}
}

func TestNearestRank(t *testing.T) {
	var d []time.Duration
	for i := 1; i <= 100; i++ {
		d = append(d, time.Duration(i))
	}
	if nearestRank(d, 0.99) != 99 || nearestRank(d, 0.5) != 50 || nearestRank(d, 1) != 100 || nearestRank(nil, 0.5) != 0 {
		t.Fatal("nearest rank")
	}
}

var t0 = time.Unix(2000, 0)

func at(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }

func ledgerWith(t *testing.T) *opLedger {
	l := newLedger()
	for i := 0; i < 6; i++ {
		l.register(fmt.Sprint("k", i), i, at(i*10), at(i*10))
		l.attempt(fmt.Sprint("k", i))
	}
	return l
}

func TestReconcileLedgers(t *testing.T) {
	l := ledgerWith(t)
	l.ack("k0", fullACK{restartACK: restartACK{JobID: 1, Observed: at(100)}, Gen: 1, Attempt: 1})
	l.durable("k0", 1, at(150))
	// k1: acked, durable observed BEFORE the ack is clamped to zero latency.
	l.ack("k1", fullACK{restartACK: restartACK{JobID: 2, Observed: at(200)}, Gen: 1, Attempt: 1})
	l.durable("k1", 2, at(120))
	// k2: acknowledged but never completed -> lost acknowledged work.
	l.ack("k2", fullACK{restartACK: restartACK{JobID: 3, Observed: at(100)}, Gen: 1, Attempt: 1})
	// k3: acknowledged with no job row at all -> false 2xx.
	l.ack("k3", fullACK{restartACK: restartACK{JobID: 4, Observed: at(100)}, Gen: 1, Attempt: 1})
	// k4: lost response, then retry acked as duplicate; durable; two attempts.
	l.attempt("k4")
	l.fail("k4", "timeout")
	l.ack("k4", fullACK{restartACK: restartACK{JobID: 5, Observed: at(300), Duplicate: true}, Gen: 2, Attempt: 2})
	l.durable("k4", 5, at(31000+300)) // 31 s after the ack
	// k5: never acked, but durable (response lost).
	l.durable("k5", 6, at(400))
	// ack/durable disagreement on the job ID is recorded.
	l.durable("k0", 99, at(160))
	recs, mismatch := l.snapshot()
	db := map[string]dbRow{
		"k0": {JobID: 1, Jobs: 1, Results: 1, Completed: true},
		"k1": {JobID: 2, Jobs: 1, Results: 1, Completed: true},
		"k2": {JobID: 3, Jobs: 1, Results: 0},
		"k4": {JobID: 5, Jobs: 1, Results: 2, Completed: true, Attempt: 3, ErrorEntries: 2},
		"k5": {JobID: 6, Jobs: 1, Results: 1, Completed: true},
	}
	r := reconcile(recs, db, mismatch, restartCompletionBudget)
	if r.Scheduled != 6 || r.Acked != 5 || r.Unacked != 1 || r.DurableUnacked != 1 || r.Durable != 4 {
		t.Fatalf("counts %+v", r)
	}
	if len(r.LostAcknowledged) != 1 || r.LostAcknowledged[0] != "k2" || len(r.False2xx) != 1 || r.False2xx[0] != "k3" {
		t.Fatalf("lost=%v false=%v", r.LostAcknowledged, r.False2xx)
	}
	if len(r.DuplicateLogical) != 1 || r.DuplicateLogical[0] != "k4" {
		t.Fatalf("duplicate logical %v", r.DuplicateLogical)
	}
	if len(r.AckToCompleteOver) != 1 || r.AckToCompleteOver[0] != "k4" || r.AckToCompleteMax != 31*time.Second || r.CompletedBeforeAck != 1 {
		t.Fatalf("completion %+v", r)
	}
	if len(r.JobMismatch) != 1 || r.RetryAfterCommitAcks != 1 || r.Retries != 1 || r.Failures["timeout"] != 1 || r.RiverRetries != 2 || r.RiverErrorEntries != 2 || r.HTTPAttempts != 7 {
		t.Fatalf("accounting %+v", r)
	}
	if allPass(evaluateBounds(cycleInputs{Cfg: fullConfig{Warmup: 0, Window: 20 * time.Second}, Rec: r})) {
		t.Fatal("a failing ledger passed the bounds")
	}
	// A first-attempt duplicate ack is a defect, not a retry.
	l2 := newLedger()
	l2.register("a", 0, at(0), at(0))
	l2.attempt("a")
	l2.ack("a", fullACK{restartACK: restartACK{JobID: 1, Observed: at(1), Duplicate: true}, Attempt: 1})
	recs2, _ := l2.snapshot()
	if got := reconcile(recs2, map[string]dbRow{"a": {Jobs: 1}}, nil, restartCompletionBudget); len(got.FirstAttemptDuplicateAck) != 1 {
		t.Fatal("first attempt duplicate ack not flagged")
	}
}

func TestPendingAckedAndKnown(t *testing.T) {
	l := ledgerWith(t)
	l.ack("k0", fullACK{restartACK: restartACK{Observed: at(5)}})
	l.ack("k1", fullACK{restartACK: restartACK{Observed: at(9)}})
	l.durable("k0", 1, at(6))
	if n, last := l.pendingAcked(); n != 1 || !last.Equal(at(9)) {
		t.Fatalf("pending %d %v", n, last)
	}
	if !l.known("k0") || l.known("zz") || l.durable("zz", 1, at(1)) {
		t.Fatal("unknown key must not enter the ledger")
	}
}

func TestRecoveryReadyAndPriorAcks(t *testing.T) {
	l := newLedger()
	for i, key := range []string{"a", "b", "c", "d"} {
		l.register(key, i, at(i*10), at(i*10))
		l.attempt(key)
	}
	// a: acked by the OLD epoch after the request, must not define readiness.
	l.ack("a", fullACK{restartACK: restartACK{JobID: 1, Observed: at(1010)}, Gen: 1, Attempt: 1})
	l.durable("a", 1, at(1020))
	// b: acked by the new epoch, completed at 3400.
	l.ack("b", fullACK{restartACK: restartACK{JobID: 2, Observed: at(3000)}, Gen: 2, Attempt: 1})
	l.durable("b", 2, at(3400))
	// c: new epoch ack but completes later.
	l.ack("c", fullACK{restartACK: restartACK{JobID: 3, Observed: at(3100)}, Gen: 2, Attempt: 1})
	l.durable("c", 3, at(40000))
	recs, _ := l.snapshot()
	ready, ok := recoveryReady(recs, 2, at(1000))
	if !ok || !ready.Equal(at(3400)) {
		t.Fatalf("ready %v %v", ready, ok)
	}
	if _, ok := recoveryReady(recs, 3, at(1000)); ok {
		t.Fatal("no ack from an epoch that never served must mean not ready")
	}
	worst, over := priorAckLate(recs, ready, restartCompletionBudget)
	if worst != 0 || len(over) != 0 { // c acked at 3100 < ready 3400, completes 36.6 s after ready
		if len(over) != 1 || over[0] != "c" {
			t.Fatalf("worst=%v over=%v", worst, over)
		}
	} else {
		t.Fatal("c completed 36.6 s after recovery and must be flagged")
	}
}

func TestRecoveryIDsStrictness(t *testing.T) {
	l := newLedger()
	ready := at(1000)
	for i := 0; i < 8; i++ {
		key := fmt.Sprint("r", i)
		l.register(key, i, at(i*100), at(i*100))
		l.attempt(key)
		l.ack(key, fullACK{restartACK: restartACK{JobID: int64(i + 1), Observed: at(i*100 + 5)}, Attempt: 1})
		l.durable(key, int64(i+1), at(i*100+50))
	}
	recs, _ := l.snapshot()
	// IDs scheduled at 1000.. do not exist (index 0..7 at 0..700): none after ready.
	if st := recoveryIDs(recs, ready, 3, restartCompletionBudget); st.Have != 0 {
		t.Fatalf("%+v", st)
	}
	st := recoveryIDs(recs, at(300), 3, restartCompletionBudget)
	if st.Have != 3 || st.FirstAttemptAcked != 3 || st.Durable != 3 || st.CompletionOver != 0 || st.LateArrivals != 0 {
		t.Fatalf("%+v", st)
	}
	l.fail("r4", "http_503") // one failed attempt among the new IDs breaks strictness
	l.attempt("r4")
	recs, _ = l.snapshot()
	if st := recoveryIDs(recs, at(300), 3, restartCompletionBudget); st.FirstAttemptAcked != 2 {
		t.Fatalf("%+v", st)
	}
	// A late arrival (generator lag over the limit) is counted.
	l.register("late", 20, at(5000), at(5000).Add(2*fullArrivalLate))
	recs, _ = l.snapshot()
	if st := recoveryIDs(recs, at(4000), 1, restartCompletionBudget); st.LateArrivals != 1 {
		t.Fatalf("%+v", st)
	}
}

func goodInputs() cycleInputs {
	cfg := fullConfig{CyclesPerKind: 1, Combined: 1, Warmup: 0, Window: 20 * time.Second, RecoveryIDs: 100}
	n := cfg.Arrivals()
	return cycleInputs{
		Cfg: cfg, Rec: reconcileResult{Scheduled: n, Acked: n}, ReadyOK: true, Recovery: 2 * time.Second,
		RecIDs:   recoveryIDStats{Want: 100, Have: 100, FirstAttemptAcked: 100, Durable: 100},
		Arrivals: arrivalStats{Scheduled: n, Fired: n}, HeldHTTPAtRestart: 3, HeldWorkerAtRestart: 2,
		MaxConcurrent: 20, SamePID: true, GOMAXPROCS: fullGOMAXPROCS, ScanSweepOK: true,
	}
}

func TestEvaluateBoundsEachFrozenLimit(t *testing.T) {
	if !allPass(evaluateBounds(goodInputs())) {
		t.Fatal("good inputs must pass")
	}
	mutations := map[string]func(*cycleInputs){
		"recovery_seconds":                           func(c *cycleInputs) { c.Recovery = 5*time.Second + time.Nanosecond },
		"recovery_seconds ":                          func(c *cycleInputs) { c.ReadyOK = false },
		"ack_to_completion_seconds":                  func(c *cycleInputs) { c.Rec.AckToCompleteOver = []string{"x"} },
		"lost_acknowledged_work":                     func(c *cycleInputs) { c.Rec.LostAcknowledged = []string{"x"} },
		"false_2xx":                                  func(c *cycleInputs) { c.Rec.False2xx = []string{"x"} },
		"duplicate_logical_result":                   func(c *cycleInputs) { c.Rec.DuplicateLogical = []string{"x"} },
		"prior_acks_complete_after_recovery":         func(c *cycleInputs) { c.PriorLate = []string{"x"} },
		"recovery_new_ids_acked_completed":           func(c *cycleInputs) { c.RecIDs.Have = 99 },
		"recovery_new_ids_acked_completed ":          func(c *cycleInputs) { c.RecIDs.FirstAttemptAcked = 99 },
		"recovery_ids_offered_at_100_per_second":     func(c *cycleInputs) { c.RecIDs.LateArrivals = 1 },
		"arrivals_started_on_schedule":               func(c *cycleInputs) { c.LateArrivals = 1 },
		"all_scheduled_arrivals_fired":               func(c *cycleInputs) { c.Arrivals.Fired-- },
		"concurrent_requests_max":                    func(c *cycleInputs) { c.MaxConcurrent = fullMaxConcurrent + 1 },
		"orphan_transaction_worker_handler_listener": func(c *cycleInputs) { c.Orphans = 1 },
		"in_flight_work_held_at_restart_request":     func(c *cycleInputs) { c.HeldWorkerAtRestart = 0 },
		"durable_scan_evidence_complete":             func(c *cycleInputs) { c.ScanErrors = 1 },
		"same_process":                               func(c *cycleInputs) { c.SamePID = false },
		"gomaxprocs":                                 func(c *cycleInputs) { c.GOMAXPROCS = 4 },
		"ack_job_id_matches_durable_job_id":          func(c *cycleInputs) { c.Rec.JobMismatch = []string{"x"} },
		"duplicate_ack_on_first_attempt":             func(c *cycleInputs) { c.Rec.FirstAttemptDuplicateAck = []string{"x"} },
	}
	for name, mutate := range mutations {
		in := goodInputs()
		mutate(&in)
		failed := false
		for _, b := range evaluateBounds(in) {
			if !b.Pass {
				failed = true
			}
		}
		if !failed {
			t.Fatalf("mutation %q did not fail any bound", name)
		}
	}
	// Recovery exactly at the limit passes (<=5 s).
	in := goodInputs()
	in.Recovery = restartRecoveryBudget
	if !allPass(evaluateBounds(in)) {
		t.Fatal("recovery exactly 5s must pass")
	}
	if allPass(nil) {
		t.Fatal("no bounds must not pass")
	}
}

func TestClassifyTransport(t *testing.T) {
	refused := &net.OpError{Op: "dial", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
	for err, want := range map[error]string{
		refused: "connection_refused", fmt.Errorf("w: %w", context.DeadlineExceeded): "timeout",
		os.NewSyscallError("read", syscall.ECONNRESET): "connection_reset", errors.New("x"): "other_transport",
		context.Canceled: "canceled",
	} {
		if got := classifyTransport(err); got != want {
			t.Fatalf("%v: %s want %s", err, got, want)
		}
	}
}

// The S4 cap must be structural: however many attempts are offered at once, no
// more than fullMaxConcurrent are ever inside the gate, the excess is counted as
// deferred, and a waiter whose deadline passes gets an error instead of a slot.
func TestCapGateIsStructural(t *testing.T) {
	g := newCapGate(fullMaxConcurrent)
	var cur, peak atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 5*fullMaxConcurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := g.acquire(context.Background()); err != nil {
				t.Error(err)
				return
			}
			defer g.release()
			n := cur.Add(1)
			for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
			}
			time.Sleep(5 * time.Millisecond)
			cur.Add(-1)
		}()
	}
	wg.Wait()
	if peak.Load() > fullMaxConcurrent || g.deferred.Load() == 0 {
		t.Fatalf("peak %d (cap %d), deferred %d", peak.Load(), fullMaxConcurrent, g.deferred.Load())
	}
	full := newCapGate(1)
	if err := full.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if full.acquire(ctx) == nil || full.deferred.Load() != 1 {
		t.Fatal("a waiter past its deadline must fail and be counted as deferred")
	}
}

func TestFullConfigDiagnosticsAreNeverAcceptance(t *testing.T) {
	get := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	for _, env := range []map[string]string{{"S0_WORKER_RESTART_FULL_PREGROW_ROWS": "10"}, {"S0_WORKER_RESTART_FULL_DIAG_JOBS_NO_SEQSCAN": "1"}} {
		c, err := fullConfigFromEnv(get(env))
		if err != nil || c.Acceptance() {
			t.Fatalf("%v: diagnostic knobs must make the run non-acceptance: %v", env, err)
		}
	}
}
