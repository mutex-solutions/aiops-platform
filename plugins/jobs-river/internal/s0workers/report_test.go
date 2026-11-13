// SPDX-License-Identifier: Apache-2.0
package s0workers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type caseResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}
type diagnosticReport struct {
	Classification   string            `json:"classification"`
	OriginalSQLGate  string            `json:"original_sql_gate"`
	WorkerStarted    bool              `json:"real_background_worker_started"`
	Command          string            `json:"canonical_command_without_credentials_not_invocation_capture"`
	GoVersion        string            `json:"go_version"`
	ExitCode         int               `json:"exit_code"`
	Fixtures         []string          `json:"retained_fixture_ids"`
	Cases            []caseResult      `json:"cases"`
	SourceSHA256     map[string]string `json:"source_sha256"`
	ExecutableSHA256 string            `json:"executable_sha256,omitempty"`
	IdentityError    bool              `json:"identity_error"`
	Limits           []string          `json:"limits"`
}

var report = diagnosticReport{
	OriginalSQLGate: "FAIL unchanged; not remeasured by this package",
	Command:         "go test -mod=readonly -count=1 -v ./plugins/jobs-river/internal/s0workers",
	Limits: []string{
		"Ordinary-functional worker scope-data/revocation DIAGNOSTIC, NOT authority-gate acceptance, NOT full S0, NOT a product implementation.",
		"Custom GUC tampering and SET-role membership escape remain known UNSAFE; no denied operation, capability repair, or bypass test.",
		"Trusted jobs role intentionally sees/writes all tenants for River background paths. It does not change frontend caller RLS.",
		"Provider extra columns/defaults, opaque callScope context, and result ledger are test fixtures, not stock River tenant APIs, SDK definitions, or an S3 schema decision.",
		"Only selected completed cases are functional observations. Starting background services does not individually prove leader election, rescue, cleanup, scheduler, uniqueness races, or notification-only wakeup.",
		"Two concurrent clients are two in-process replicas sharing one schema; no cross-process, crash/restart, timing/fairness guarantee, or performance acceptance.",
		"Revocation cancels/drains cooperative plugin contexts under a bounded test budget; it cannot kill noncooperative Go code and is not an SQL policy.",
		"JobRetry causes explicit redelivery; the ledger demonstrates one result per job ID, not crash/ambiguous-commit exactly-once delivery or cross-job idempotency.",
		"Uniqueness tested via sequential InsertTx with UniqueOpts{ByArgs:true} and a tagged ID; no InsertMany or cross-tenant uniqueness-boundary claim.",
		"LISTEN/NOTIFY is enabled but a short poll fallback remains; completion/snooze subscription events are local, buffered, lossy notifications followed by causal SQL assertions.",
		"Bootstrap observer counts retained rows but does not enqueue, deliver, or write results. Fresh fixture schemas/roles are retained; no automatic DROP.",
		"Report requires orderly TestMain return. On-disk source and executable hashes identify artifacts, not an attested build. No credentials/DSN/database/host/errors serialized.",
	},
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func TestMain(m *testing.M) {
	code := m.Run()
	report.GoVersion = runtime.Version()
	report.SourceSHA256 = map[string]string{}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		report.IdentityError = true
	} else {
		for _, name := range []string{"fixture_test.go", "worker_test.go", "diagnostic_test.go", "report_test.go"} {
			hash, err := hashFile(filepath.Join(filepath.Dir(source), name))
			if err != nil {
				report.IdentityError = true
				continue
			}
			report.SourceSHA256[name] = hash
		}
	}
	exe, err := os.Executable()
	if err != nil {
		report.IdentityError = true
	} else {
		report.ExecutableSHA256, err = hashFile(exe)
		if err != nil {
			report.IdentityError = true
		}
	}
	if report.IdentityError {
		code = 1
	}
	report.ExitCode = code
	report.Classification = "NOT measured: explicit DSN absent or worker tests not selected"
	if len(report.Fixtures) > 0 {
		report.Classification = "NOT measured: fixture/preflight only; no worker started"
	}
	if report.WorkerStarted {
		report.Classification = "ordinary-functional worker diagnostic selected cases executed; NOT authority-gate pass"
	}
	if code != 0 {
		report.Classification = "ordinary-functional worker diagnostic FAILED; NOT authority-gate pass"
		if !report.WorkerStarted {
			report.Classification = "worker behavior NOT measured: fixture/preflight FAILED before any worker started; NOT authority-gate pass"
		}
	}
	data, err := json.Marshal(report)
	if err != nil {
		fmt.Println("S0WORKERS_REPORT serialization_failed")
		os.Exit(1)
	}
	fmt.Printf("S0WORKERS_REPORT %s\n", data)
	os.Exit(code)
}
