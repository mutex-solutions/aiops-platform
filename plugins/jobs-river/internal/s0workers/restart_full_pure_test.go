// SPDX-License-Identifier: Apache-2.0
package s0workers

// Pure (database-free) pieces of the full restart gate: configuration, the
// open-loop arrival scheduler, the independent ACK ledger and its
// reconciliation against the durable ledger, and frozen-bound evaluation.
// Every rule cites docs/design/s0-spike-criteria.md section numbers (S4, S6).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"
)

const (
	fullRate     = 100 // S4/S6: 100 requests/second
	fullInterval = time.Second / fullRate
	fullHold     = 200 * time.Millisecond // S6: controlled hold before the restart request
	// S4: operation timeout is 5 s from SCHEDULED arrival, including queue time and
	// any same-ID retry; a timeout stays a failure in the accounting.
	fullOpTimeout     = 5 * time.Second
	fullRetryBackoff  = 100 * time.Millisecond
	fullArrivalLate   = 50 * time.Millisecond // implementation reading of "at 100/s", see README
	fullMaxConcurrent = 32                    // S4: at most 32 concurrent requests
	fullPoolConns     = 8                     // S4: a pool of eight connections
	fullWorkers       = 4
	fullGOMAXPROCS    = 8 // S4

	frozenCyclesPerKind  = 30
	frozenCombinedCycles = 10
	frozenWarmup         = 30 * time.Second
	frozenWindow         = 120 * time.Second
	frozenRecoveryIDs    = 1000
)

// fullConfig only ever shrinks the frozen scale; acceptance requires all frozen values.
type fullConfig struct {
	CyclesPerKind, Combined int
	Warmup, Window          time.Duration
	RecoveryIDs             int
}

func envInt(get func(string) string, name string, def, lo, hi int) (int, error) {
	v := get(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		return 0, fmt.Errorf("%s must be an integer in %d..%d", name, lo, hi)
	}
	return n, nil
}

func fullConfigFromEnv(get func(string) string) (fullConfig, error) {
	var c fullConfig
	var err error
	var warm, win int
	for _, p := range []struct {
		dst           *int
		name          string
		def, lo, high int
	}{
		{&c.CyclesPerKind, "S0_WORKER_RESTART_FULL_CYCLES", frozenCyclesPerKind, 1, frozenCyclesPerKind},
		{&c.Combined, "S0_WORKER_RESTART_FULL_COMBINED_CYCLES", frozenCombinedCycles, 1, frozenCombinedCycles},
		{&warm, "S0_WORKER_RESTART_FULL_WARMUP_SECONDS", int(frozenWarmup / time.Second), 0, int(frozenWarmup / time.Second)},
		{&win, "S0_WORKER_RESTART_FULL_WINDOW_SECONDS", int(frozenWindow / time.Second), 20, int(frozenWindow / time.Second)},
		{&c.RecoveryIDs, "S0_WORKER_RESTART_FULL_RECOVERY_IDS", frozenRecoveryIDs, 1, frozenRecoveryIDs},
	} {
		if *p.dst, err = envInt(get, p.name, p.def, p.lo, p.high); err != nil {
			return c, err
		}
	}
	c.Warmup, c.Window = time.Duration(warm)*time.Second, time.Duration(win)*time.Second
	// After the restart request the schedule must still hold recovery (<=5 s) plus the new IDs.
	if c.Window/2 < restartRecoveryBudget+time.Duration(c.RecoveryIDs)*fullInterval {
		return c, errors.New("window too short to contain recovery plus the requested new IDs")
	}
	return c, nil
}

func (c fullConfig) Acceptance() bool {
	return c.CyclesPerKind == frozenCyclesPerKind && c.Combined == frozenCombinedCycles && c.Warmup == frozenWarmup && c.Window == frozenWindow && c.RecoveryIDs == frozenRecoveryIDs
}
func (c fullConfig) Arrivals() int            { return int((c.Warmup + c.Window) / fullInterval) }
func (c fullConfig) RestartAt() time.Duration { return c.Warmup + c.Window/2 } // S6: measured t=60 s

// ---- open-loop arrival scheduler (S4) ----

type arrivalStats struct {
	Scheduled, Fired int
	Lags             []time.Duration // started - scheduled, one per fired arrival
}

// runOpenLoop fires arrival i at start+i*interval. A late arrival is fired at
// once and its lag recorded; the schedule is never shifted. fire must not block.
func runOpenLoop(ctx context.Context, start time.Time, interval time.Duration, n int, now func() time.Time, waitUntil func(context.Context, time.Time) error, fire func(i int, scheduled, started time.Time)) arrivalStats {
	st := arrivalStats{Scheduled: n, Lags: make([]time.Duration, 0, n)}
	for i := 0; i < n; i++ {
		scheduled := start.Add(time.Duration(i) * interval)
		if now().Before(scheduled) {
			if waitUntil(ctx, scheduled) != nil {
				return st
			}
		} else if ctx.Err() != nil {
			return st
		}
		started := now()
		lag := started.Sub(scheduled)
		if lag < 0 {
			lag = 0
		}
		st.Lags = append(st.Lags, lag)
		fire(i, scheduled, started)
		st.Fired++
	}
	return st
}

func timerWait(ctx context.Context, until time.Time) error {
	d := time.Until(until)
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// nearestRank is the S4 quantile definition.
func nearestRank(sorted []time.Duration, q float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(q*float64(len(sorted)) + 0.999999999)
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

func sortedCopy(d []time.Duration) []time.Duration {
	out := append([]time.Duration(nil), d...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// ---- independent ACK ledger (S6) ----

type fullACK struct {
	restartACK
	Gen     int64 `json:"epoch"`
	Attempt int   `json:"attempt"`
}
type opRecord struct {
	Key                string
	Index              int
	Scheduled, Started time.Time
	Attempts           int
	Failures           map[string]int
	Ack                *fullACK
	Durable            *restartDurable
	TimedOut           bool
}
type opLedger struct {
	mu          sync.Mutex
	recs        map[string]*opRecord
	jobMismatch []string
}

func newLedger() *opLedger { return &opLedger{recs: map[string]*opRecord{}} }

func (l *opLedger) register(key string, idx int, scheduled, started time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.recs[key] = &opRecord{Key: key, Index: idx, Scheduled: scheduled, Started: started, Failures: map[string]int{}}
}
func (l *opLedger) attempt(key string) { l.mu.Lock(); l.recs[key].Attempts++; l.mu.Unlock() }
func (l *opLedger) fail(key, class string) {
	l.mu.Lock()
	l.recs[key].Failures[class]++
	l.mu.Unlock()
}
func (l *opLedger) timedOut(key string) { l.mu.Lock(); l.recs[key].TimedOut = true; l.mu.Unlock() }
func (l *opLedger) ack(key string, a fullACK) {
	l.mu.Lock()
	l.recs[key].Ack = &a
	l.mu.Unlock()
}

// durable records the FIRST durable observation; it reports whether it was new.
func (l *opLedger) durable(key string, jobID int64, observed time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.recs[key]
	if !ok {
		return false
	}
	if (r.Ack != nil && r.Ack.JobID != jobID) || (r.Durable != nil && r.Durable.JobID != jobID) {
		l.jobMismatch = append(l.jobMismatch, key)
	}
	if r.Durable != nil {
		return false
	}
	r.Durable = &restartDurable{JobID: jobID, Observed: observed}
	return true
}
func (l *opLedger) known(key string) bool {
	l.mu.Lock()
	_, ok := l.recs[key]
	l.mu.Unlock()
	return ok
}

// pendingAcked counts acknowledged IDs with no durable observation and the latest ACK time.
func (l *opLedger) pendingAcked() (n int, lastAck time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.recs {
		if r.Ack != nil {
			if r.Ack.Observed.After(lastAck) {
				lastAck = r.Ack.Observed
			}
			if r.Durable == nil {
				n++
			}
		}
	}
	return n, lastAck
}

func (l *opLedger) snapshot() ([]opRecord, []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]opRecord, 0, len(l.recs))
	for _, r := range l.recs {
		c := *r
		c.Failures = map[string]int{}
		for k, v := range r.Failures {
			c.Failures[k] = v
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out, append([]string(nil), l.jobMismatch...)
}

// dbRow is the final SQL view of one idempotency ID.
type dbRow struct {
	JobID                 int64
	Jobs, Results         int
	Completed             bool
	Attempt, ErrorEntries int
}

type reconcileResult struct {
	Scheduled, Acked, Durable, Unacked, DurableUnacked int
	LostAcknowledged, False2xx                         []string
	DuplicateLogical, FirstAttemptDuplicateAck         []string
	JobMismatch                                        []string
	AckToCompleteMax                                   time.Duration
	AckToCompleteOver                                  []string
	CompletedBeforeAck                                 int
	HTTPAttempts, Retries, RetryAfterCommitAcks        int
	TimedOut                                           int
	Failures                                           map[string]int
	RiverRetries, RiverErrorEntries, RiverAttemptsMax  int
	AckLatencies                                       []time.Duration // acked: ack - scheduled
}

func reconcile(recs []opRecord, db map[string]dbRow, mismatch []string, completionBudget time.Duration) reconcileResult {
	out := reconcileResult{Scheduled: len(recs), Failures: map[string]int{}, JobMismatch: mismatch}
	for _, r := range recs {
		row := db[r.Key]
		out.HTTPAttempts += r.Attempts
		if r.Attempts > 1 {
			out.Retries += r.Attempts - 1
		}
		for k, v := range r.Failures {
			out.Failures[k] += v
		}
		if r.TimedOut {
			out.TimedOut++
		}
		if row.Jobs > 1 || row.Results > 1 {
			out.DuplicateLogical = append(out.DuplicateLogical, r.Key)
		}
		if row.Attempt > 1 {
			out.RiverRetries += row.Attempt - 1
		}
		out.RiverErrorEntries += row.ErrorEntries
		if row.Attempt > out.RiverAttemptsMax {
			out.RiverAttemptsMax = row.Attempt
		}
		if r.Durable != nil {
			out.Durable++
		}
		if r.Ack == nil {
			out.Unacked++
			if r.Durable != nil {
				out.DurableUnacked++
			}
			continue
		}
		out.Acked++
		out.AckLatencies = append(out.AckLatencies, r.Ack.Observed.Sub(r.Scheduled))
		if r.Ack.Duplicate {
			if r.Ack.Attempt == 1 {
				out.FirstAttemptDuplicateAck = append(out.FirstAttemptDuplicateAck, r.Key)
			} else {
				out.RetryAfterCommitAcks++
			}
		}
		switch {
		case row.Jobs == 0:
			out.False2xx = append(out.False2xx, r.Key)
		case r.Durable == nil || !row.Completed || row.Results < 1:
			out.LostAcknowledged = append(out.LostAcknowledged, r.Key)
		default:
			d := r.Durable.Observed.Sub(r.Ack.Observed)
			if d < 0 {
				out.CompletedBeforeAck++
				d = 0
			}
			if d > out.AckToCompleteMax {
				out.AckToCompleteMax = d
			}
			if d > completionBudget {
				out.AckToCompleteOver = append(out.AckToCompleteOver, r.Key)
			}
		}
	}
	return out
}

// recoveryReady is the earliest durable completion of an ID whose client ACK came
// from the recreated generation (epoch) after the restart request: the fresh
// resource accepted work AND the consumer completed it.
func recoveryReady(recs []opRecord, epoch int64, requested time.Time) (time.Time, bool) {
	var best time.Time
	for _, r := range recs {
		if r.Ack != nil && r.Ack.Gen == epoch && !r.Ack.Observed.Before(requested) && r.Durable != nil {
			if best.IsZero() || r.Durable.Observed.Before(best) {
				best = r.Durable.Observed
			}
		}
	}
	return best, !best.IsZero()
}

// priorAckLate: IDs acknowledged before recovery completion must be durable by
// ready+budget (S6 row 3). Returns the worst lateness past ready and violators.
func priorAckLate(recs []opRecord, ready time.Time, budget time.Duration) (worst time.Duration, over []string) {
	for _, r := range recs {
		if r.Ack == nil || !r.Ack.Observed.Before(ready) || r.Durable == nil {
			continue
		}
		if late := r.Durable.Observed.Sub(ready); late > 0 {
			if late > worst {
				worst = late
			}
			if late > budget {
				over = append(over, r.Key)
			}
		}
	}
	return worst, over
}

type recoveryIDStats struct {
	Want, Have, FirstAttemptAcked, Durable, CompletionOver, LateArrivals int
}

// recoveryIDs: the first n IDs scheduled at or after ready (S4: 1,000 new IDs at
// 100/s must be acknowledged AND completed). Strict reading: every one is acked on
// its first attempt, with no failed attempt of any class.
func recoveryIDs(recs []opRecord, ready time.Time, n int, completionBudget time.Duration) recoveryIDStats {
	st := recoveryIDStats{Want: n}
	for _, r := range recs {
		if st.Have == n {
			break
		}
		if r.Scheduled.Before(ready) {
			continue
		}
		st.Have++
		if r.Started.Sub(r.Scheduled) > fullArrivalLate {
			st.LateArrivals++
		}
		if r.Ack != nil && r.Ack.Attempt == 1 && r.Attempts == 1 && len(r.Failures) == 0 {
			st.FirstAttemptAcked++
		}
		if r.Ack != nil && r.Durable != nil {
			st.Durable++
			if r.Durable.Observed.Sub(r.Ack.Observed) > completionBudget {
				st.CompletionOver++
			}
		}
	}
	return st
}

// ---- bound evaluation ----

type boundResult struct {
	Name     string `json:"name"`
	Limit    string `json:"limit"`
	Observed string `json:"observed"`
	Pass     bool   `json:"pass"`
}

type cycleInputs struct {
	Cfg                       fullConfig
	Rec                       reconcileResult
	ReadyOK                   bool
	Recovery                  time.Duration
	PriorLate                 []string
	RecIDs                    recoveryIDStats
	Arrivals                  arrivalStats
	Orphans                   int
	HeldHTTPAtRestart         int64
	HeldWorkerAtRestart       int64
	ScanErrors, ScannerMissed int
	MaxConcurrent             int
	SamePID                   bool
	GOMAXPROCS                int
	LateArrivals              int
	ScanSweepOK               bool
}

func evaluateBounds(in cycleInputs) []boundResult {
	var out []boundResult
	add := func(name, limit string, observed any, pass bool) {
		out = append(out, boundResult{name, limit, fmt.Sprint(observed), pass})
	}
	r := in.Rec
	add("lost_acknowledged_work", "0", len(r.LostAcknowledged), len(r.LostAcknowledged) == 0)
	add("false_2xx", "0", len(r.False2xx), len(r.False2xx) == 0)
	add("duplicate_logical_result", "0", len(r.DuplicateLogical), len(r.DuplicateLogical) == 0)
	add("ack_job_id_matches_durable_job_id", "0 mismatches", len(r.JobMismatch), len(r.JobMismatch) == 0)
	add("duplicate_ack_on_first_attempt", "0 (strict)", len(r.FirstAttemptDuplicateAck), len(r.FirstAttemptDuplicateAck) == 0)
	add("recovery_seconds", "<=5s and observed", fmt.Sprintf("%.3f ready=%v", in.Recovery.Seconds(), in.ReadyOK), in.ReadyOK && in.Recovery <= restartRecoveryBudget)
	add("ack_to_completion_seconds", "<=30s for every acknowledged ID", fmt.Sprintf("max=%.3f over=%d acked=%d", r.AckToCompleteMax.Seconds(), len(r.AckToCompleteOver), r.Acked), len(r.AckToCompleteOver) == 0 && r.Acked > 0)
	add("prior_acks_complete_after_recovery", "<=30s after recovery", len(in.PriorLate), in.ReadyOK && len(in.PriorLate) == 0)
	ri := in.RecIDs
	add("recovery_new_ids_acked_completed", fmt.Sprintf("%d, all first-attempt ack and complete<=30s", ri.Want), fmt.Sprintf("have=%d first_attempt_acked=%d durable=%d over=%d", ri.Have, ri.FirstAttemptAcked, ri.Durable, ri.CompletionOver),
		in.ReadyOK && ri.Have == ri.Want && ri.FirstAttemptAcked == ri.Want && ri.Durable == ri.Want && ri.CompletionOver == 0)
	add("recovery_ids_offered_at_100_per_second", fmt.Sprintf("start lag <=%s", fullArrivalLate), ri.LateArrivals, in.ReadyOK && ri.LateArrivals == 0)
	add("arrivals_started_on_schedule", fmt.Sprintf("0 starting >%s late (open loop, recorded not slowed)", fullArrivalLate), in.LateArrivals, in.LateArrivals == 0)
	add("all_scheduled_arrivals_fired", "no dropped arrival", fmt.Sprintf("%d/%d expected=%d", in.Arrivals.Fired, in.Arrivals.Scheduled, in.Cfg.Arrivals()), in.Arrivals.Fired == in.Arrivals.Scheduled && in.Arrivals.Scheduled == in.Cfg.Arrivals() && r.Scheduled == in.Cfg.Arrivals())
	add("concurrent_requests_max", fmt.Sprintf("<=%d", fullMaxConcurrent), in.MaxConcurrent, in.MaxConcurrent <= fullMaxConcurrent)
	add("orphan_transaction_worker_handler_listener", "0", in.Orphans, in.Orphans == 0)
	add("in_flight_work_held_at_restart_request", ">0 http and >0 worker", fmt.Sprintf("http=%d worker=%d", in.HeldHTTPAtRestart, in.HeldWorkerAtRestart), in.HeldHTTPAtRestart > 0 && in.HeldWorkerAtRestart > 0)
	add("durable_scan_evidence_complete", "0 scan errors, final sweep ok", fmt.Sprintf("errors=%d missed_by_poll=%d sweep_ok=%v", in.ScanErrors, in.ScannerMissed, in.ScanSweepOK), in.ScanErrors == 0 && in.ScanSweepOK)
	add("same_process", "same PID", in.SamePID, in.SamePID)
	add("gomaxprocs", fmt.Sprint(fullGOMAXPROCS), in.GOMAXPROCS, in.GOMAXPROCS == fullGOMAXPROCS)
	return out
}

func allPass(b []boundResult) bool {
	for _, x := range b {
		if !x.Pass {
			return false
		}
	}
	return len(b) > 0
}

func classifyTransport(err error) string {
	var ne net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection_refused"
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE):
		return "connection_reset"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "eof"
	}
	return "other_transport"
}
