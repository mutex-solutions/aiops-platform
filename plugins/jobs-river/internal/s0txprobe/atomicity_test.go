// SPDX-License-Identifier: Apache-2.0
package s0txprobe

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal("operation failed: ", sqlState(err))
	}
}
func (f *fixture) assertRows(t testing.TB, ctx context.Context, scope callScope, want int) {
	t.Helper()
	// Bootstrap is ONLY an observer here, never a transaction participant.
	for _, spec := range []struct{ schema, table string }{{f.caller, "state"}, {f.jobs, "river_job"}, {f.events, "outbox"}, {f.audit, "audit"}} {
		var n int
		must(t, f.admin.QueryRow(ctx, "SELECT count(*) FROM "+qualified(spec.schema, spec.table)+" WHERE scope_id=$1", scope.id).Scan(&n))
		if n != want {
			t.Errorf("%s count=%d want=%d", spec.table, n, want)
		}
	}
}
func (f *fixture) assertIdentity(t testing.TB, ctx context.Context, scope callScope, xid string) {
	t.Helper()
	for _, spec := range []struct{ schema, table, body string }{{f.caller, "state", "body"}, {f.jobs, "river_job", "args"}, {f.events, "outbox", "body"}, {f.audit, "audit", "body"}} {
		var tenant, actualXID, bodyTenant string
		must(t, f.admin.QueryRow(ctx, "SELECT tenant,writer_xid::text,"+ident(spec.body)+"->>'tenant' FROM "+qualified(spec.schema, spec.table)+" WHERE scope_id=$1", scope.id).Scan(&tenant, &actualXID, &bodyTenant))
		if tenant != scope.tenant || actualXID != xid || bodyTenant != "malicious-payload-tenant" {
			t.Errorf("%s: identity not stamped from common transaction scope", spec.table)
		}
	}
}
func (f *fixture) assertCleanReuse(t testing.TB, ctx context.Context) {
	t.Helper()
	var user, tenant, id, path string
	must(t, f.runtime.QueryRow(ctx, "SELECT current_user,coalesce(current_setting('s0.tenant',true),''),coalesce(current_setting('s0.scope_id',true),''),current_setting('search_path')").Scan(&user, &tenant, &id, &path))
	if user != f.login || tenant != "" || id != "" || path != "pg_catalog" {
		t.Error("role/scope/search_path leaked into reused pool connection")
	}
}
func (f *fixture) tx(t testing.TB, ctx context.Context, scope *callScope) pgx.Tx {
	t.Helper()
	tx, err := f.beginCaller(ctx, scope)
	must(t, err)
	t.Cleanup(func() {
		clean, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(clean)
	})
	return tx
}
func runCase(t *testing.T, name string, fn func(*testing.T)) {
	t.Helper()
	executed, skipped := false, false
	passed := t.Run(name, func(st *testing.T) {
		executed = true
		report.Measured = true
		defer func() { skipped = st.Skipped() }()
		fn(st)
	})
	status := "not_selected"
	if executed {
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

// Ordinary atomicity diagnostic only. No worker/authority/performance gate is
// exercised and no successful case changes the original SQL-boundary FAIL.
func TestAtomicityDiagnostic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := openFixture(t, ctx)
	for boundary := 0; boundary <= 4; boundary++ {
		runCase(t, fmt.Sprintf("rollback_after_%d", boundary), func(t *testing.T) {
			s := callScope{"scope-a", fmt.Sprintf("rollback-%d", boundary)}
			tx := f.tx(t, ctx, &s)
			for i := 0; i < boundary; i++ {
				must(t, f.write(ctx, tx, i))
			}
			must(t, tx.Rollback(ctx))
			f.assertRows(t, ctx, s, 0)
			f.assertCleanReuse(t, ctx)
		})
	}
	for _, tenant := range []string{"scope-a", "scope-b"} {
		runCase(t, "commit_"+tenant, func(t *testing.T) {
			s := callScope{tenant, "commit-" + tenant}
			tx := f.tx(t, ctx, &s)
			var xid string
			must(t, tx.QueryRow(ctx, "SELECT pg_current_xact_id()::text").Scan(&xid))
			for i := 0; i < 4; i++ {
				must(t, f.write(ctx, tx, i))
				if i == 1 {
					var role string
					must(t, tx.QueryRow(ctx, "SELECT current_user").Scan(&role))
					if role != f.caller {
						t.Error("InsertTx did not restore caller")
					}
				}
			}
			// A separate connection cannot observe any participant before commit.
			f.assertRows(t, ctx, s, 0)
			must(t, tx.Commit(ctx))
			f.assertRows(t, ctx, s, 1)
			f.assertIdentity(t, ctx, s, xid)
			f.assertCleanReuse(t, ctx)
		})
	}
	for boundary := 0; boundary <= 4; boundary++ {
		runCase(t, fmt.Sprintf("sql_error_after_%d", boundary), func(t *testing.T) {
			s := callScope{"scope-a", fmt.Sprintf("sql-error-%d", boundary)}
			tx := f.tx(t, ctx, &s)
			for i := 0; i < boundary; i++ {
				must(t, f.write(ctx, tx, i))
			}
			_, err := tx.Exec(ctx, "SELECT 1/0")
			if sqlState(err) != "22012" {
				t.Fatal("expected division-by-zero SQL error")
			}
			if sqlState(f.restoreCaller(ctx, tx)) != "25P02" {
				t.Fatal("role restoration must fail on aborted transaction")
			}
			if !errors.Is(tx.Commit(ctx), pgx.ErrTxCommitRollback) {
				t.Fatal("aborted transaction must not commit")
			}
			f.assertRows(t, ctx, s, 0)
			f.assertCleanReuse(t, ctx)
		})
	}
	runCase(t, "InsertTx_error_restore_is_not_success", func(t *testing.T) {
		s := callScope{"scope-a", "insert-error"}
		tx := f.tx(t, ctx, nil)
		// Missing handle creates an ordinary NOT NULL/RLS insert failure.
		// No setting-tamper or authority-elevation operation is exercised.
		err := f.insertJob(ctx, tx)
		var failure *insertFailure
		if !errors.As(err, &failure) || failure.Insert == nil || sqlState(failure.Restore) != "25P02" {
			t.Fatal("must retain InsertTx failure AND failed role restoration")
		}
		if !errors.Is(tx.Commit(ctx), pgx.ErrTxCommitRollback) {
			t.Fatal("failed InsertTx transaction must not commit")
		}
		f.assertRows(t, ctx, s, 0)
		f.assertCleanReuse(t, ctx)
	})
	for _, boundary := range []int{1, 2, 4} {
		runCase(t, fmt.Sprintf("canceled_after_%d", boundary), func(t *testing.T) {
			s := callScope{"scope-a", fmt.Sprintf("cancel-%d", boundary)}
			tx := f.tx(t, ctx, &s)
			for i := 0; i < boundary; i++ {
				must(t, f.write(ctx, tx, i))
			}
			canceled, stop := context.WithCancel(ctx)
			stop()
			if _, err := tx.Exec(canceled, "SELECT 1"); !errors.Is(err, context.Canceled) {
				t.Fatal("operation must observe cancellation")
			}
			// Cancellation alone is NOT a claim of automatic transaction abort;
			// owner explicitly rolls back using a live cleanup context.
			must(t, tx.Rollback(ctx))
			f.assertRows(t, ctx, s, 0)
			f.assertCleanReuse(t, ctx)
		})
	}
	runCase(t, "physical_connection_lost_before_commit", func(t *testing.T) {
		s := callScope{"scope-b", "connection-lost"}
		tx := f.tx(t, ctx, &s)
		for i := 0; i < 4; i++ {
			must(t, f.write(ctx, tx, i))
		}
		// Own connection only; no pg_terminate_backend or administrative kill.
		must(t, tx.Conn().Close(ctx))
		if tx.Commit(ctx) == nil {
			t.Fatal("closed connection unexpectedly committed")
		}
		// A fresh ordinary connection provides a server processing barrier.
		must(t, f.runtime.Ping(ctx))
		f.assertRows(t, ctx, s, 0)
		f.assertCleanReuse(t, ctx)
	})
	runCase(t, "scope_reads_are_context_filtered", func(t *testing.T) {
		for _, tenant := range []string{"scope-a", "scope-b"} {
			s := callScope{tenant, "read-" + tenant}
			tx := f.tx(t, ctx, &s)
			var n int
			must(t, tx.QueryRow(ctx, "SELECT count(*) FROM "+qualified(f.caller, "state")).Scan(&n))
			if n != 1 {
				t.Errorf("scoped state count=%d want 1", n)
			}
			_, err := tx.Exec(ctx, "SET LOCAL ROLE "+ident(f.jobs))
			must(t, err)
			must(t, tx.QueryRow(ctx, "SELECT count(*) FROM "+qualified(f.jobs, "river_job")).Scan(&n))
			if n != 1 {
				t.Errorf("scoped jobs count=%d want 1", n)
			}
			must(t, f.restoreCaller(ctx, tx))
			must(t, tx.Rollback(ctx))
		}
	})
	runCase(t, "no_handle_reads_zero_writes_rejected", func(t *testing.T) {
		tx := f.tx(t, ctx, nil)
		var n int
		must(t, tx.QueryRow(ctx, "SELECT count(*) FROM "+qualified(f.caller, "state")).Scan(&n))
		if n != 0 {
			t.Error("no-handle state read exposed rows")
		}
		_, err := tx.Exec(ctx, "SET LOCAL ROLE "+ident(f.jobs))
		must(t, err)
		must(t, tx.QueryRow(ctx, "SELECT count(*) FROM "+qualified(f.jobs, "river_job")).Scan(&n))
		if n != 0 {
			t.Error("no-handle jobs read exposed rows")
		}
		must(t, tx.Rollback(ctx))
		for i := 0; i < 4; i++ {
			tx = f.tx(t, ctx, nil)
			err = f.write(ctx, tx, i)
			if err == nil {
				t.Fatalf("no-handle participant %d write allowed", i)
			}
			state := sqlState(err)
			if state != "42501" && state != "23502" {
				t.Fatalf("unexpected no-handle write SQLSTATE %s", state)
			}
			_ = tx.Rollback(ctx)
		}
		f.assertCleanReuse(t, ctx)
	})
	runCase(t, "caller_cannot_direct_insert_outbox_or_audit", func(t *testing.T) {
		for _, spec := range []struct{ schema, table string }{{f.events, "outbox"}, {f.audit, "audit"}} {
			s := callScope{"scope-a", "direct-" + spec.table}
			tx := f.tx(t, ctx, &s)
			_, err := tx.Exec(ctx, "INSERT INTO "+qualified(spec.schema, spec.table)+" (body) VALUES ('{}')")
			if sqlState(err) != "42501" {
				t.Error("direct cross-schema insert did not fail closed")
			}
			_ = tx.Rollback(ctx)
			f.assertRows(t, ctx, s, 0)
		}
	})
	runCase(t, "all_tables_contain_only_the_two_commits", func(t *testing.T) {
		for _, spec := range []struct{ schema, table string }{{f.caller, "state"}, {f.jobs, "river_job"}, {f.events, "outbox"}, {f.audit, "audit"}} {
			var n int
			must(t, f.admin.QueryRow(ctx, "SELECT count(*) FROM "+qualified(spec.schema, spec.table)).Scan(&n))
			if n != 2 {
				t.Errorf("%s total=%d want only 2 committed rows", spec.table, n)
			}
		}
	})
}

func TestHelpersIdentifiersAndErrors(t *testing.T) {
	if ident(`a"; DROP ROLE x;--`) != `"a""; DROP ROLE x;--"` {
		t.Fatal("unsafe identifier quoting")
	}
	if qualified(`schema"`, "table") != `"schema"""."table"` {
		t.Fatal("unsafe qualified identifier quoting")
	}
	for _, tc := range []struct {
		err  error
		want string
	}{{nil, ""}, {fmt.Errorf("wrap: %w", &pgconn.PgError{Code: "42501", Message: "sensitive"}), "42501"}, {context.Canceled, "context_canceled"}, {context.DeadlineExceeded, "deadline_exceeded"}, {errors.New("sensitive"), "non_postgres_error"}} {
		if sqlState(tc.err) != tc.want {
			t.Errorf("error classification want %s", tc.want)
		}
	}
	if dbError("safe", errors.New("password=secret")).Error() != "safe: SQLSTATE non_postgres_error" {
		t.Fatal("error redaction failed")
	}
}
func TestHelpersPrivilegeChecks(t *testing.T) {
	if !confinedRole(roleFacts{Login: true}, true) {
		t.Fatal("confined login rejected")
	}
	for _, bad := range []roleFacts{{Login: true, Inherit: true}, {Login: true, Super: true}, {Login: true, Bypass: true}, {Login: true, CreateDB: true}, {Login: true, CreateRole: true}, {Login: true, Replication: true}, {Login: true, Owns: true}, {}} {
		if confinedRole(bad, true) {
			t.Error("unsafe runtime facts accepted")
		}
	}
	good := []membership{{Role: "caller", Set: true}, {Role: "jobs", Set: true}}
	if !exactMemberships(good, "caller", "jobs") {
		t.Fatal("declared memberships rejected")
	}
	for _, bad := range [][]membership{nil, good[:1], append(append([]membership{}, good...), membership{Role: "owner", Set: true}), {{Role: "caller", Set: true}, {Role: "jobs", Set: true, Inherit: true}}, {{Role: "caller", Set: true}, {Role: "jobs", Set: true, Admin: true}}, {{Role: "caller", Set: true}, {Role: "jobs"}}, {{Role: "caller", Set: true}, {Role: "caller", Set: true}}} {
		if exactMemberships(bad, "caller", "jobs") {
			t.Error("unsafe memberships accepted")
		}
	}
}
