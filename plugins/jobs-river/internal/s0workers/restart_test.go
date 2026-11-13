// SPDX-License-Identifier: Apache-2.0
package s0workers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

type restartReply struct {
	Key       string `json:"idempotency_id"`
	JobID     int64  `json:"job_id"`
	Duplicate bool   `json:"duplicate"`
}
type restartServerResult struct {
	reply restartReply
	err   error
}
type restartHTTPResult struct {
	reply    restartReply
	status   int
	observed time.Time
	err      error
}

func restartWait[T any](t *testing.T, ctx context.Context, ch <-chan T) T {
	t.Helper()
	select {
	case v, ok := <-ch:
		if !ok {
			t.Fatal("restart causal channel closed")
		}
		return v
	case <-ctx.Done():
		t.Fatal("restart causal barrier exceeded its budget")
		var zero T
		return zero
	}
}

// Mirrors enqueue's unchanged transaction/role ordering. It cannot call enqueue
// directly: testing.Fatal/Goexit is invalid in an HTTP handler goroutine.
func restartInsert(r *http.Request, f *fixture, scope callScope, key, mode string, barrier chan<- restartServerResult) (restartReply, error) {
	reply := restartReply{Key: key}
	tx, err := f.beginCaller(r.Context(), scope)
	if err != nil {
		return reply, err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(r.Context(), "SET LOCAL ROLE "+ident(f.jobs)); err != nil {
		return reply, err
	}
	res, insertErr := f.inserter.InsertTx(r.Context(), tx, workerArgs{Tenant: hostileTenant, IdempotencyID: key}, &river.InsertOpts{Queue: queue, UniqueOpts: river.UniqueOpts{ByArgs: true}})
	_, restoreErr := tx.Exec(r.Context(), "SET LOCAL ROLE "+ident(f.caller))
	if insertErr != nil {
		return reply, safeError("restart insert", insertErr)
	}
	if restoreErr != nil {
		return reply, safeError("restart role restore", restoreErr)
	}
	reply.JobID, reply.Duplicate = res.Job.ID, res.UniqueSkippedAsDuplicate
	if mode == "beforeCommit" {
		barrier <- restartServerResult{reply: reply} // real, still-uncommitted INSERT
		<-r.Context().Done()
		return reply, tx.Rollback(context.Background())
	}
	if err := tx.Commit(r.Context()); err != nil {
		return reply, safeError("restart commit", err)
	}
	barrier <- restartServerResult{reply: reply} // NOT a client ACK
	if mode == "responseLost" {
		<-r.Context().Done() // no response bytes; client cancels only after COMMIT
	}
	return reply, nil
}

// A fresh loopback endpoint per operation deliberately has no production route,
// payload/body-size, proxy, TLS, or real network-fault claim. Scope is provider
// closure data, never an args tenant. Only the client goroutine observes 2xx.
func restartRequest(t *testing.T, ctx context.Context, f *fixture, scope callScope, key, mode string, e *restartEvidence) restartReply {
	t.Helper()
	barrier, finished := make(chan restartServerResult, 1), make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reply, err := restartInsert(r, f, scope, key, mode, barrier)
		if err != nil {
			select {
			case barrier <- restartServerResult{err: err}:
			default:
			}
		}
		finished <- err
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		} else if mode == "ok" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(reply)
		}
	}))
	defer server.Close()
	requestCtx, cancel := context.WithTimeout(ctx, restartCompletionBudget)
	defer cancel() // cancel before Close even when a causal assertion fails
	client := server.Client()
	client.Timeout = restartCompletionBudget
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, server.URL+"/enqueue", nil)
	must(t, safeError("restart HTTP request", err))
	response := make(chan restartHTTPResult, 1)
	go func() {
		res, err := client.Do(req)
		out := restartHTTPResult{err: err}
		if err == nil {
			defer res.Body.Close()
			out.status = res.StatusCode
			out.err = json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&out.reply)
			out.observed = time.Now().UTC() // timestamp belongs to the client observer
		}
		response <- out
	}()
	observed := restartWait(t, ctx, barrier)
	must(t, safeError("restart server transaction", observed.err))
	if mode != "ok" {
		cancel()
	}
	out := restartWait(t, ctx, response)
	must(t, safeError("restart handler finish", restartWait(t, ctx, finished)))
	if mode != "ok" {
		if out.err == nil || (out.status >= 200 && out.status < 300) {
			t.Fatal("controlled lost/canceled response unexpectedly acknowledged")
		}
		if _, acked := e.ACKs[key]; acked {
			t.Fatal("unacknowledged operation appeared in independent ACK ledger")
		}
		return observed.reply
	}
	must(t, safeError("restart HTTP response", out.err))
	if out.status != http.StatusOK || out.reply != observed.reply || out.reply.Key != key {
		t.Fatal("client ACK did not identify the committed operation")
	}
	e.ACKs[key] = restartACK{JobID: out.reply.JobID, Observed: out.observed, Duplicate: out.reply.Duplicate}
	return out.reply
}

func restartComplete(t *testing.T, ctx context.Context, f *fixture, c *workerClient, events <-chan *river.Event, scope callScope, reply restartReply, inserted bool, e *restartEvidence) {
	t.Helper()
	deadline := time.Now().Add(restartCompletionBudget)
	if ack, ok := e.ACKs[reply.Key]; ok {
		deadline = ack.Observed.Add(restartCompletionBudget)
	}
	budget, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	for {
		d := restartWait(t, budget, c.worker.deliveries)
		if d.jobID == reply.JobID && d.stage == "handled" {
			checkDelivery(t, d, scope, reply.Key, inserted)
			break
		}
	}
	for {
		event := restartWait(t, budget, events)
		if event.Job == nil || event.Job.ID != reply.JobID {
			continue
		}
		if event.Kind == river.EventKindJobFailed || event.Kind == river.EventKindJobCancelled {
			t.Fatal("restart job failed or canceled")
		}
		if event.Kind == river.EventKindJobCompleted {
			break
		}
	}
	f.assertState(t, budget, reply.JobID, "completed")
	f.assertResult(t, budget, reply.JobID, scope, reply.Key, 1)
	var jobs, results int
	must(t, f.admin.QueryRow(budget, "SELECT (SELECT count(*) FROM "+table(f.jobs, "river_job")+" WHERE args->>'idempotency_id'=$1),(SELECT count(*) FROM "+table(f.data, "result")+" WHERE idempotency_id=$1)", reply.Key).Scan(&jobs, &results))
	if jobs != 1 || results != 1 || time.Now().After(deadline) {
		t.Fatal("restart unique job/result or completion budget mismatch")
	}
	if ack, ok := e.ACKs[reply.Key]; ok && ack.JobID != reply.JobID {
		t.Fatal("independent ACK and durable ledgers disagree on job ID")
	}
	if prior, exists := e.Durable[reply.Key]; exists {
		if prior.JobID != reply.JobID {
			t.Fatal("durable operation changed job identity")
		}
	} else {
		e.Durable[reply.Key] = restartDurable{JobID: reply.JobID, Observed: time.Now().UTC()}
	}
}

func TestWorkerRestartDiagnostic(t *testing.T) {
	if os.Getenv("S0_WORKER_RESTART_RUN") != "1" {
		t.Skip("NOT measured: set S0_WORKER_RESTART_RUN=1 explicitly; default smoke1cycle !=30/1000load")
	}
	cycles := 1
	if value := os.Getenv("S0_WORKER_RESTART_CLIENT_CYCLES"); value != "" {
		var err error
		cycles, err = strconv.Atoi(value)
		if err != nil || cycles < 1 || cycles > 30 {
			t.Fatal("restart client cycles must be 1..30; never full acceptance")
		}
	}
	e := restartReport(t, cycles)
	runDiagnostic(t, "restart_client_recreation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cycles)*40*time.Second+45*time.Second)
		defer cancel()
		f := openFixture(t, ctx)
		scope := callScope{"tenant-a", "restart-client"}
		c := f.newWorker(t, ctx, "restart-initial", 10*time.Millisecond)
		d := c.worker // same dispatcher and same pools across ALL client lifetimes
		must(t, d.registry.install(f.recordingHandler(d)))
		events, unsubscribe := subscribe(c)
		defer func() { unsubscribe() }()
		c.start(t, ctx)
		e.WorkerStarted = true
		seedKey := f.id + "-restart-seed"
		seed := f.enqueue(t, ctx, scope, workerArgs{Tenant: hostileTenant, IdempotencyID: seedKey})
		seedACK := restartRequest(t, ctx, f, scope, seedKey, "ok", e)
		if seed.UniqueSkippedAsDuplicate || !seedACK.Duplicate || seed.Job.ID != seedACK.JobID {
			t.Fatal("original enqueue and HTTP retry did not share one job")
		}
		restartComplete(t, ctx, f, c, events, scope, seedACK, true, e)
		for cycle := 0; cycle < cycles; cycle++ {
			started := time.Now().UTC()
			recovery, cancelRecovery := context.WithTimeout(ctx, restartRecoveryBudget)
			must(t, d.registry.revoke(recovery))
			must(t, safeError("restart StopAndCancel", c.client.StopAndCancel(recovery)))
			if c.worker != d {
				t.Fatal("client recreation replaced the stable dispatcher")
			}
			c.started = false
			unsubscribe()
			workers := river.NewWorkers()
			river.AddWorker(workers, d) // once per NEW client, never on handler reload
			fresh, err := river.NewClient(riverpgxv5.New(d.pool), &river.Config{
				Schema: f.jobs, ID: "s0wk-" + f.id + "-restart-" + strconv.Itoa(cycle), Logger: quiet, Workers: workers,
				Queues: map[string]river.QueueConfig{queue: {MaxWorkers: 1}}, FetchCooldown: time.Millisecond, FetchPollInterval: 5 * time.Millisecond,
				JobTimeout: 10 * time.Second, SoftStopTimeout: 2 * time.Second, RescueStuckJobsAfter: time.Minute, TestOnly: true,
				CancelledJobRetentionPeriod: -1, CompletedJobRetentionPeriod: -1, DiscardedJobRetentionPeriod: -1, ReindexerIndexNames: []string{},
			})
			must(t, safeError("restart NewClient", err))
			c.client = fresh
			must(t, d.registry.install(f.recordingHandler(d)))
			events, unsubscribe = subscribe(c)
			lifetime, cancelLifetime := context.WithCancel(ctx)
			defer cancelLifetime()
			stopRecovery := context.AfterFunc(recovery, cancelLifetime)
			must(t, safeError("restart Start", c.client.Start(lifetime)))
			c.started = true
			key := f.id + "-restart-" + strconv.Itoa(cycle)
			probe := restartRequest(t, recovery, f, scope, key, "ok", e)
			if probe.Duplicate {
				t.Fatal("recovery probe ID was not new")
			}
			restartComplete(t, recovery, f, c, events, scope, probe, true, e)
			ready := time.Now().UTC()
			inBudget := stopRecovery() && recovery.Err() == nil
			cancelRecovery() // detached first: successful client stays alive
			if !inBudget || ready.Sub(started) > restartRecoveryBudget || c.worker != d || os.Getpid() != e.PID {
				t.Fatal("same-PID stable-dispatcher recovery exceeded 5s or changed identity")
			}
			e.Recoveries = append(e.Recoveries, restartRecovery{Started: started, Ready: ready})
		}
	})
	runDiagnostic(t, "restart_ack_loss_idempotency_controls", func(t *testing.T) {
		ctx, cancel := caseContext(t)
		defer cancel()
		f := openFixture(t, ctx)
		scope := callScope{"tenant-b", "restart-ack-controls"}
		c := f.newWorker(t, ctx, "restart-ack", 10*time.Millisecond)
		must(t, c.worker.registry.install(f.recordingHandler(c.worker)))
		events, unsubscribe := subscribe(c)
		defer unsubscribe()
		c.start(t, ctx)
		e.WorkerStarted = true
		key := f.id + "-lost-response"
		lost := restartRequest(t, ctx, f, scope, key, "responseLost", e)
		restartComplete(t, ctx, f, c, events, scope, lost, true, e)
		e.Unacked = append(e.Unacked, key) // observed durable BEFORE any client 2xx
		retry := restartRequest(t, ctx, f, scope, key, "ok", e)
		if !retry.Duplicate || retry.JobID != lost.JobID {
			t.Fatal("same-ID retry after lost response created another job")
		}
		_, err := c.client.JobRetry(ctx, retry.JobID) // explicit real redelivery
		must(t, safeError("restart explicit JobRetry", err))
		restartComplete(t, ctx, f, c, events, scope, retry, false, e)
		key = f.id + "-before-commit"
		aborted := restartRequest(t, ctx, f, scope, key, "beforeCommit", e)
		var jobs int
		must(t, f.admin.QueryRow(ctx, "SELECT count(*) FROM "+table(f.jobs, "river_job")+" WHERE args->>'idempotency_id'=$1", key).Scan(&jobs))
		f.assertResult(t, ctx, aborted.JobID, scope, key, 0)
		if jobs != 0 {
			t.Fatal("before-commit canceled operation left a durable job")
		}
		e.RolledBack = append(e.RolledBack, key)
	})
}
