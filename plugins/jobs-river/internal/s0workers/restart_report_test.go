// SPDX-License-Identifier: Apache-2.0
package s0workers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

const restartRecoveryBudget = 5 * time.Second
const restartCompletionBudget = 30 * time.Second

// ACKs originate only in the HTTP client; durable observations originate only
// after the real worker's result commit AND River's completed state are read.
type restartACK struct {
	JobID     int64     `json:"job_id"`
	Observed  time.Time `json:"observed_2xx_utc"`
	Duplicate bool      `json:"duplicate"`
}
type restartDurable struct {
	JobID    int64     `json:"job_id"`
	Observed time.Time `json:"observed_result_and_completed_utc"`
}
type restartRecovery struct {
	Started time.Time `json:"started_utc"`
	Ready   time.Time `json:"fresh_probe_completed_utc"`
}
type restartEvidence struct {
	Classification string                    `json:"classification"`
	Status         string                    `json:"status"`
	Started        time.Time                 `json:"started_utc"`
	Ended          time.Time                 `json:"ended_utc"`
	PID            int                       `json:"pid"`
	WorkerStarted  bool                      `json:"real_worker_started"`
	CyclesPlanned  int                       `json:"client_cycles_planned"`
	Recoveries     []restartRecovery         `json:"client_recoveries"`
	ACKs           map[string]restartACK     `json:"client_ack_ledger"`
	Durable        map[string]restartDurable `json:"durable_completion_ledger"`
	Unacked        []string                  `json:"durable_without_ack_before_retry"`
	RolledBack     []string                  `json:"before_commit_cancel_no_job_no_result_no_ack"`
	Cases          []caseResult              `json:"cases"`
	Before         map[string]string         `json:"source_before_sha256"`
	After          map[string]string         `json:"source_after_sha256"`
	Contract       map[string]any            `json:"frozen_acceptance_not_claimed"`
	Gaps           []string                  `json:"gaps"`
}

func restartSource(t *testing.T) map[string]string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("restart source identity unavailable")
	}
	out := map[string]string{}
	for _, name := range []string{"fixture_test.go", "worker_test.go", "diagnostic_test.go", "report_test.go", "restart_test.go", "restart_report_test.go", "restart-README.md"} {
		hash, err := hashFile(filepath.Join(filepath.Dir(source), name))
		must(t, safeError("restart source hash", err))
		out[name] = hash
	}
	return out
}

func restartReport(t *testing.T, cycles int) *restartEvidence {
	e := &restartEvidence{
		Classification: "ordinary-functional restart DIAGNOSTIC; NOT full S0 acceptance; authority UNSAFE/FAIL unchanged",
		Started:        time.Now().UTC(), PID: os.Getpid(), CyclesPlanned: cycles,
		ACKs: map[string]restartACK{}, Durable: map[string]restartDurable{}, Before: restartSource(t),
		Contract: map[string]any{"cycles_each_pool_client_listener": 30, "combined_cycles": 10, "recovery_seconds": 5, "ack_to_completion_seconds": 30, "new_ids_per_recovery": 1000, "new_ids_per_second": 100, "diagnostic_new_ids_per_client_recovery": 1},
		Gaps: []string{
			"Default smoke1cycle !=30/1000load; even 30 client cycles alone is NOT full acceptance or performance evidence.",
			"Pool-only unsupported: riverpgxv5 captures a concrete pool; worker/caller pools are retained, not replaced.",
			"Listener-only unsupported: no verified public listener endpoint; client replacement also replaces its listener. No backend termination; combined cycles not implemented.",
			"Loopback synthetic HTTP only; controlled withheld response + client cancellation, not production ingress/network/crash ambiguous commit.",
			"Trusted jobs sees all tenants; known-unsafe roles/GUCs unchanged. One result per job ID, not final kernel effects or durable exactly-once delivery proof.",
			"Bootstrap SQL only observes; actual fixture worker writes results. On-disk hashes are not attested builds; reports require orderly return.",
		},
	}
	firstCase := len(report.Cases)
	t.Cleanup(func() {
		e.Ended, e.After = time.Now().UTC(), restartSource(t)
		for name, before := range e.Before {
			if e.After[name] != before {
				t.Error("restart source changed during run")
			}
		}
		if os.Getpid() != e.PID {
			t.Error("restart process identity changed")
		}
		e.Cases = report.Cases[firstCase:]
		e.Status = "NOT measured: no restart worker started"
		if e.WorkerStarted {
			e.Status = "selected diagnostic cases executed; not acceptance"
		}
		if t.Failed() {
			e.Status = "FAILED; not acceptance"
		}
		data, err := json.Marshal(e)
		must(t, safeError("restart report serialization", err))
		fmt.Printf("S0WORKERS_RESTART_REPORT %s\n", data)
	})
	return e
}
