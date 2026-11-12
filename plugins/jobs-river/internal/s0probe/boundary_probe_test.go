// SPDX-License-Identifier: Apache-2.0

package s0probe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const criteriaSHA256 = "628bd6ec24e5983bfbe9fcdada096eaa84db3e95227f11e8fd5a7fa33a26f19c"
const baselineCommit = "2d8d6ec717bb327739d1630d03ad6787c37660d6"

type boundaryCheck struct {
	Name     string `json:"name"`
	Escaped  bool   `json:"escaped"`
	Effect   string `json:"effect"`
	SQLState string `json:"sqlstate,omitempty"`
}

type boundaryReport struct {
	Kind             string            `json:"kind"`
	RunID            string            `json:"run_id"`
	Started          time.Time         `json:"started_utc"`
	Ended            time.Time         `json:"ended_utc"`
	CriteriaSHA256   string            `json:"criteria_sha256"`
	BaselineCommit   string            `json:"baseline_commit"`
	SourceSHA256     string            `json:"probe_source_sha256,omitempty"`
	ExecutableSHA256 string            `json:"executable_sha256,omitempty"`
	Classification   string            `json:"classification"`
	Database         string            `json:"database"`
	FixtureID        string            `json:"fixture_id"`
	Roles            map[string]string `json:"roles"`
	Checks           []boundaryCheck   `json:"checks"`
	Dependencies     []string          `json:"linked_modules,omitempty"`
	Limitations      []string          `json:"limitations"`
}

func (f *fixture) probeSQLBoundary(ctx context.Context) (boundaryReport, error) {
	r := boundaryReport{Kind: "s0-sql-view-boundary", RunID: f.id, Started: time.Now().UTC(),
		CriteriaSHA256: criteriaSHA256, BaselineCommit: baselineCommit, Database: f.database, FixtureID: f.id,
		Classification: "pass", Roles: map[string]string{"runtime": f.login, "caller": f.caller, "jobs": f.jobs, "owner": f.owner},
		Limitations: []string{"correctness diagnostic only; no latency/performance/restart acceptance", "hostile Go is outside the accepted sandbox boundary; these statements use the actual raw caller SQL capability", "fixture objects intentionally retained; no destructive automatic cleanup"}}
	run := func(name string, body func(pgx.Tx) (bool, string, error)) error {
		tx, err := f.beginCaller(ctx, "scope-a")
		if err != nil {
			return fmt.Errorf("begin %s: SQLSTATE %s", name, sqlState(err))
		}
		escaped, effect, err := body(tx)
		r.Checks = append(r.Checks, boundaryCheck{Name: name, Escaped: escaped, Effect: effect, SQLState: sqlState(err)})
		rollbackErr := tx.Rollback(context.Background())
		if rollbackErr != nil {
			return fmt.Errorf("rollback %s: SQLSTATE %s", name, sqlState(rollbackErr))
		}
		if escaped {
			r.Classification = "fail"
		}
		return nil
	}
	// Establish the actual policy before adversarial settings change it. Without
	// these controls a foreign read could be an already-broken fixture policy.
	control, err := f.beginCaller(ctx, "scope-a")
	if err != nil {
		return r, fmt.Errorf("begin policy control: SQLSTATE %s", sqlState(err))
	}
	var ownRows, foreignRows int
	err = control.QueryRow(ctx, "SELECT count(*) FILTER (WHERE tenant='scope-a'), count(*) FILTER (WHERE tenant='scope-b') FROM "+qualified(f.callerSchema, "state")).Scan(&ownRows, &foreignRows)
	rollbackErr := control.Rollback(context.Background())
	if err != nil || rollbackErr != nil || ownRows != 1 || foreignRows != 0 {
		return r, fmt.Errorf("policy control invalid: own=%d foreign=%d SQLSTATE=%s", ownRows, foreignRows, sqlState(err))
	}
	r.Checks = append(r.Checks, boundaryCheck{Name: "unchanged_scope_control", Effect: "own scope row visible; foreign scope row denied before any setting change"})
	missing, err := f.beginCaller(ctx, "")
	if err != nil {
		return r, fmt.Errorf("begin missing-scope control: SQLSTATE %s", sqlState(err))
	}
	var missingRows int
	err = missing.QueryRow(ctx, "SELECT count(*) FROM "+qualified(f.callerSchema, "state")).Scan(&missingRows)
	rollbackErr = missing.Rollback(context.Background())
	if err != nil || rollbackErr != nil || missingRows != 0 {
		return r, fmt.Errorf("missing-scope control invalid: rows=%d SQLSTATE=%s", missingRows, sqlState(err))
	}
	r.Checks = append(r.Checks, boundaryCheck{Name: "missing_scope_control", Effect: "missing transaction scope sees zero rows"})
	if err := run("set_local_role_jobs", func(tx pgx.Tx) (bool, string, error) {
		_, err := tx.Exec(ctx, "SET LOCAL ROLE "+ident(f.jobs))
		if err != nil {
			return false, "role entry denied", err
		}
		var role string
		err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role)
		return err == nil && role == f.jobs, "caller raw Exec entered the provider jobs role", err
	}); err != nil {
		return r, err
	}
	if err := run("reset_role", func(tx pgx.Tx) (bool, string, error) {
		_, err := tx.Exec(ctx, "RESET ROLE")
		if err != nil {
			return false, "role reset denied", err
		}
		var role string
		err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role)
		return err == nil && role == f.login, "caller raw Exec escaped its no-login role back to the login principal", err
	}); err != nil {
		return r, err
	}
	if err := run("set_config_cross_scope_read", func(tx pgx.Tx) (bool, string, error) {
		if _, err := tx.Exec(ctx, "SELECT set_config('s0.tenant','scope-b',true)"); err != nil {
			return false, "scope change denied", err
		}
		var count int
		err := tx.QueryRow(ctx, "SELECT count(*) FROM "+qualified(f.callerSchema, "state")+" WHERE request_id='seed-b'").Scan(&count)
		return err == nil && count > 0, "caller changed the transaction scope and read the other scope's row under FORCE RLS", err
	}); err != nil {
		return r, err
	}
	if err := run("set_config_cross_scope_write", func(tx pgx.Tx) (bool, string, error) {
		if _, err := tx.Exec(ctx, "SELECT set_config('s0.tenant','scope-b',true)"); err != nil {
			return false, "scope change denied", err
		}
		var scope string
		err := tx.QueryRow(ctx, "INSERT INTO "+qualified(f.callerSchema, "state")+" (request_id,body) VALUES ('attack-'||$1,'{}') RETURNING tenant", f.id).Scan(&scope)
		return err == nil && scope == "scope-b", "caller changed the transaction scope and wrote another scope's row; attack transaction rolled back", err
	}); err != nil {
		return r, err
	}
	if err := run("set_local_tenant", func(tx pgx.Tx) (bool, string, error) {
		if _, err := tx.Exec(ctx, "SET LOCAL s0.tenant='scope-b'"); err != nil {
			return false, "SET LOCAL scope denied", err
		}
		var scope string
		err := tx.QueryRow(ctx, "SELECT current_setting('s0.tenant')").Scan(&scope)
		return err == nil && scope == "scope-b", "caller changed the trusted tenant setting through SET LOCAL", err
	}); err != nil {
		return r, err
	}
	if err := run("direct_outbox_write", func(tx pgx.Tx) (bool, string, error) {
		_, err := tx.Exec(ctx, "INSERT INTO "+qualified(f.eventsSchema, "outbox")+" (request_id,body) VALUES ('attack-'||$1,'{}')", f.id)
		return err == nil, "direct foreign-schema write must be denied independently of scope RLS", err
	}); err != nil {
		return r, err
	}
	var retainedAttackRows int
	if err := f.admin.QueryRow(ctx, "SELECT count(*) FROM "+qualified(f.callerSchema, "state")+" WHERE request_id='attack-'||$1", f.id).Scan(&retainedAttackRows); err != nil || retainedAttackRows != 0 {
		return r, fmt.Errorf("rollback control invalid: retained=%d SQLSTATE=%s", retainedAttackRows, sqlState(err))
	}
	// Acquire every connection the pool currently owns simultaneously so a fresh
	// connection cannot accidentally hide leaked settings in a reused backend.
	owned := int(f.runtime.Stat().TotalConns())
	connections := make([]interface{ Release() }, 0, owned)
	for range owned {
		conn, err := f.runtime.Acquire(ctx)
		if err != nil {
			for _, held := range connections {
				held.Release()
			}
			return r, fmt.Errorf("reuse control acquire: SQLSTATE %s", sqlState(err))
		}
		connections = append(connections, conn)
		var currentRole, setting string
		err = conn.QueryRow(ctx, "SELECT current_user, coalesce(current_setting('s0.tenant',true),'')").Scan(&currentRole, &setting)
		if err != nil || currentRole != f.login || setting != "" {
			for _, held := range connections {
				held.Release()
			}
			return r, fmt.Errorf("reuse control invalid: role_restored=%v scope_cleared=%v SQLSTATE=%s", currentRole == f.login, setting == "", sqlState(err))
		}
	}
	for _, held := range connections {
		held.Release()
	}
	r.Checks = append(r.Checks, boundaryCheck{Name: "rollback_and_reuse_controls", Effect: "attack row absent after rollback; every existing pool backend has login role and empty scope"})
	r.Ended = time.Now().UTC()
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			r.Dependencies = append(r.Dependencies, dep.Path+"@"+dep.Version)
		}
		sort.Strings(r.Dependencies)
	}
	return r, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeReport(t testing.TB, r boundaryReport) {
	t.Helper()
	// The Go test working directory is this owned source directory. Hash source
	// bytes deterministically, not credentials, absolute workstation paths, or
	// generated results. The parent records the full source-tree digest too.
	paths, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(h, "%s\x00%d\x00", path, len(data))
		_, _ = h.Write(data)
	}
	r.SourceSHA256 = hex.EncodeToString(h.Sum(nil))
	if exe, err := os.Executable(); err == nil {
		r.ExecutableSHA256, err = hashFile(exe)
		if err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if path := os.Getenv("S0_RESULT_PATH"); path != "" {
		out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			t.Fatal("create exclusive result file: ", err)
		}
		_, writeErr := out.Write(data)
		closeErr := out.Close()
		if writeErr != nil {
			t.Fatal(writeErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	t.Log(string(data))
}
