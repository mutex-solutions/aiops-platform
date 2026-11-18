// SPDX-License-Identifier: Apache-2.0
package s0txprobe

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
	Classification    string            `json:"classification"`
	OriginalSQLGate   string            `json:"original_sql_gate"`
	Measured          bool              `json:"measured"`
	Command           string            `json:"canonical_command_without_credentials_not_invocation_capture"`
	ExitCode          int               `json:"exit_code"`
	GoVersion         string            `json:"go_version"`
	SourceSHA256      map[string]string `json:"source_sha256"`
	ExecutableSHA256  string            `json:"executable_sha256,omitempty"`
	IdentityError     bool              `json:"identity_error"`
	FixtureID         string            `json:"fixture_id,omitempty"`
	Retained          bool              `json:"fixture_retained"`
	Fsync             string            `json:"fsync,omitempty"`
	SynchronousCommit string            `json:"synchronous_commit,omitempty"`
	Cases             []caseResult      `json:"cases"`
	Limits            []string          `json:"limits"`
}

var report = diagnosticReport{
	OriginalSQLGate: "FAIL unchanged; not remeasured by this package",
	Command:         "go test -mod=readonly -count=1 -v ./plugins/jobs-river/internal/s0txprobe",
	Limits: []string{
		"Atomicity-only diagnostic, NOT SQL-boundary acceptance and NOT full S0 acceptance.",
		"Original tenant/scope-setting tamper and login jobs-role membership remain UNSAFE. No denied operation or authority-elevation mechanism tested.",
		"Provider-owned river_job columns are experimental fixture extensions; stock River has no tenant API and this is not an S3 schema decision.",
		"No jobs worker, fetch/completion/rescue/uniqueness/leader or background identity-delivery test; no 500 Tx/s performance acceptance.",
		"Cancellation uses an explicitly canceled operation then owner rollback; not in-flight cancellation or automatic-abort proof.",
		"Connection-loss case closes its own connection before commit; commit acknowledgement loss/ambiguous commit is NOT covered.",
		"river_job has no RLS and no policy (ADR-045, 2026-11-18): the jobs role is not scope-filtered there and a direct jobs-role insert with a forged tenant column is no longer rejected by a policy check; the caller role has no privilege on it.",
		"Bootstrap observer may bypass RLS for retained row counting; all four transaction writes use only the fresh runtime login and declared roles.",
		"No automatic schema/role cleanup; retain fixture_id for owner inspection and manual cleanup.",
		"Report requires orderly TestMain return; panic, kill, or crash may prevent emission. Source hashes identify on-disk source at report time, not an attested binary build.",
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
		for _, name := range []string{"fixture_test.go", "atomicity_test.go", "report_test.go"} {
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
	report.Classification = "NOT measured: explicit DSN absent or integration test not selected"
	if report.FixtureID != "" {
		report.Classification = "NOT measured: fixture/preflight only; no atomicity case executed"
	}
	if report.Measured {
		report.Classification = "atomicity-only diagnostic selected cases executed; NOT SQL-boundary acceptance"
	}
	if code != 0 {
		report.Classification = "atomicity-only diagnostic FAILED; NOT SQL-boundary acceptance"
	}
	// Fixed command and basename/hash metadata only; never serialize os.Args,
	// DSNs, passwords, database/host names, server errors, or absolute paths.
	data, err := json.Marshal(report)
	if err != nil {
		fmt.Println("S0TX_REPORT serialization_failed")
		os.Exit(1)
	}
	fmt.Printf("S0TX_REPORT %s\n", data)
	os.Exit(code)
}
