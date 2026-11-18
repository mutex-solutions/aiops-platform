// SPDX-License-Identifier: Apache-2.0

package s0typedops

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

type typedPGCase struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}
type typedReport struct {
	Classification   string                    `json:"classification"`
	OriginalSQLGate  string                    `json:"original_sql_gate"`
	CanonicalCommand string                    `json:"canonical_command_not_invocation_capture"`
	GoVersion        string                    `json:"go_version"`
	ExitCode         int                       `json:"exit_code"`
	Fixtures         []string                  `json:"disposable_fixture_ids"`
	Cases            []typedPGCase             `json:"cases"`
	ProviderCounts   map[string]map[string]int `json:"provider_operation_counts_by_fixture"`
	SourceBefore     map[string]string         `json:"source_before_sha256"`
	SourceAfter      map[string]string         `json:"source_after_sha256"`
	ExecutableSHA256 string                    `json:"executable_sha256"`
	IdentityError    bool                      `json:"identity_error"`
	Limits           []string                  `json:"limits"`
}

var typedPGReport = typedReport{
	ProviderCounts:   make(map[string]map[string]int),
	OriginalSQLGate:  "FAIL unchanged; raw SQL role/GUC authority escape not re-run by this package",
	CanonicalCommand: "go test -mod=readonly -count=1 -v ./plugins/jobs-river/internal/s0typedops",
	Limits: []string{
		"Closed-surface fixture diagnostic only; not an adopted mechanism, original raw-SQL gate pass, S0 acceptance, performance/restart evidence, or S1 approval.",
		"Caller authority is only exported Tx.Put/Get with opaque View; trusted Host/provider/participant code alone holds issuance and native pgx.Tx. This is not protection against arbitrary native Go, unsafe/reflection, independent raw SQL or stolen host references.",
		"Custom GUC stamps and SET-role memberships remain known unsafe if raw SQL is independently available. Trusted jobs policy intentionally sees all rows. No worker or background River service is started.",
		"Fixed fixture catalog preflight checks reviewed columns/types/defaults/keys, policy expressions/roles, table/schema/sequence grants, append definitions/ACLs and absent triggers; one trusted transactional policy fault is rejected. This is not production migration admission, concurrent DDL invalidation, caller DDL or unrestricted query grammar.",
		"Begin-failure controls check SQLSTATE 22012 or context cancellation, fresh five-second cleanup, same physical PID, ReadyForQuery idle, cleared role/scope and own-operation recovery. Unexpected cleanup errors are joined; a live cleanup-failure fault is not forced or claimed measured.",
		"Selected commit/revocation schedules use actual PostgreSQL transactions and trusted barrier wrappers; quiescence waits must include real commit/rollback return. These bounded schedules do not establish every concurrency case, server-side unforgeability or S0 acceptance; first identity failure and original raw-SQL failure remain unchanged history.",
		"Parent must enforce a fresh network-none OS sandbox, empty allowlisted environment, read-only sources/toolchain, scratch-only writes, bounded CPU/memory/PIDs/file/disk/wall time and disposable dummy database. Go cannot attest those controls.",
		"No Go server/container provisioning or process spawning; no fixture-authored DROP or cleanup. Unmodified River migration 007 removes only its own transient fresh-schema client tables. Go closes pools; parent destroys the entire disposable container/server and records remaining resources/teardown.",
		"Before/after source hashes plus executable hash identify on-disk inputs and executable, not an attested build. Full module/SQL source closure, licenses, actual invocation and sandbox limits are recorded independently by the parent.",
		"Only passing non-skipped named PG cases are measured. Missing explicit dummy DSN is SKIP, never SQL evidence. Raw errors, DSNs, credentials, environment dumps and server details are never serialized.",
	},
}

func typedTrack(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		status := "PASS"
		if t.Skipped() {
			status = "SKIP_NOT_MEASURED"
		}
		if t.Failed() {
			status = "FAIL"
		}
		typedPGReport.Cases = append(typedPGReport.Cases, typedPGCase{t.Name(), status})
	})
}
func typedHashFile(path string) (string, error) {
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
func typedSourceHashes() map[string]string {
	result := make(map[string]string)
	_, source, _, ok := runtime.Caller(0)
	if !ok || !filepath.IsAbs(source) || filepath.Base(source) != "pg_report_test.go" {
		typedPGReport.IdentityError = true
		return result
	}
	dir := filepath.Dir(filepath.Clean(source))
	root := filepath.Clean(filepath.Join(dir, "../../../.."))
	if filepath.Join(root, "plugins/jobs-river/internal/s0typedops") != dir {
		typedPGReport.IdentityError = true
		return result
	}
	paths := map[string]string{}
	for _, name := range []string{"api.go", "api_test.go", "pg_test.go", "pg_arbitration_test.go", "pg_report_test.go", "README.md", "NOTICE", "RIVER-LICENSE.txt", "LEVENSHTEIN-LICENSE.txt"} {
		paths[name] = filepath.Join(dir, name)
	}
	for _, name := range []string{"go.mod", "go.sum", "docs/design/s0-sql-typed-operations-proposal.md", "docs/design/s0-typed-operations-protocol.md", "docs/design/s0-typed-operations-correctness-followup.md"} {
		paths[name] = filepath.Join(root, name)
	}
	for name, path := range paths {
		value, err := typedHashFile(path)
		if err != nil {
			typedPGReport.IdentityError = true
		} else {
			result[name] = value
		}
	}
	return result
}
func TestMain(m *testing.M) {
	typedPGReport.GoVersion = runtime.Version()
	typedPGReport.SourceBefore = typedSourceHashes()
	exe, err := os.Executable()
	if err != nil {
		typedPGReport.IdentityError = true
	} else {
		typedPGReport.ExecutableSHA256, err = typedHashFile(exe)
		if err != nil {
			typedPGReport.IdentityError = true
		}
	}
	code := m.Run()
	typedPGReport.SourceAfter = typedSourceHashes()
	if len(typedPGReport.SourceBefore) != len(typedPGReport.SourceAfter) {
		typedPGReport.IdentityError = true
	}
	for name, value := range typedPGReport.SourceBefore {
		if typedPGReport.SourceAfter[name] != value {
			typedPGReport.IdentityError = true
		}
	}
	if typedPGReport.IdentityError {
		code = 1
	}
	typedPGReport.ExitCode = code
	passed, skipped := 0, 0
	for _, c := range typedPGReport.Cases {
		if c.Status == "PASS" {
			passed++
		}
		if c.Status == "SKIP_NOT_MEASURED" {
			skipped++
		}
	}
	typedPGReport.Classification = "PostgreSQL NOT measured: no completed PG case"
	if passed > 0 {
		typedPGReport.Classification = "closed-surface fixture diagnostic: selected PG cases passed; NOT adopted or original raw-SQL gate pass"
	}
	if skipped > 0 && passed == 0 {
		typedPGReport.Classification = "PostgreSQL NOT measured: selected PG cases skipped"
	}
	if code != 0 {
		typedPGReport.Classification = "closed-surface fixture diagnostic FAILED or identity failure; NOT authority-gate pass"
	}
	data, err := json.Marshal(typedPGReport)
	if err != nil {
		fmt.Println("S0TYPEDOPS_REPORT serialization_failed")
		os.Exit(1)
	}
	fmt.Printf("S0TYPEDOPS_REPORT %s\n", data)
	os.Exit(code)
}
