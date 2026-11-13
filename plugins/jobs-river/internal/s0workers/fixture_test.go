// SPDX-License-Identifier: Apache-2.0
package s0workers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"
)

// DIAGNOSTIC ONLY. This deliberately retains s0txprobe's known-unsafe login
// memberships and writable custom GUCs. Neither is an authority capability.
// The trusted jobs role sees ALL tenants; frontend writes still use caller RLS.
type fixture struct {
	admin, callerPool                    *pgxpool.Pool
	base                                 *pgxpool.Config
	inserter                             *river.Client[pgx.Tx]
	id, owner, login, caller, jobs, data string
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// Final PostgreSQL main migration inventory at v0.51.2, in catalog sort order.
// Migration 005 creates client tables, but 007 drops both of them; 008 is a no-op.
var riverTables = []string{"river_job", "river_leader", "river_migration", "river_notification", "river_queue"}

func ident(s string) string            { return pgx.Identifier{s}.Sanitize() }
func table(schema, name string) string { return pgx.Identifier{schema, name}.Sanitize() }
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

type redactedError struct{ stage, state string }

func (e *redactedError) Error() string { return e.stage + ": SQLSTATE " + e.state }

func sqlState(err error) string {
	if err == nil {
		return ""
	}
	var safe *redactedError
	if errors.As(err, &safe) {
		return safe.state
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code
	}
	if errors.Is(err, context.Canceled) {
		return "context_canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	return "non_postgres_error"
}
func safeError(stage string, err error) error {
	if err == nil {
		return nil
	}
	return &redactedError{stage: stage, state: sqlState(err)}
}
func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		// Preserve our fixed stage/counter, never the server detail or raw error.
		var safe *redactedError
		if errors.As(err, &safe) {
			t.Fatalf("operation failed: stage=%s SQLSTATE=%s", safe.stage, safe.state)
		}
		t.Fatal("operation failed: ", sqlState(err))
	}
}

// Fresh random names, NO automatic DROP. Bootstrap/migrations never participate
// in enqueue, delivery, or result transactions. Credentials are never reported.
func openFixture(t *testing.T, ctx context.Context) *fixture {
	t.Helper()
	dsn, expected := os.Getenv("S0_ADMIN_DSN"), os.Getenv("S0_EXPECT_DATABASE")
	if dsn == "" {
		t.Skip("NOT measured: explicit S0_ADMIN_DSN absent")
	}
	if expected == "" {
		t.Fatal("S0_EXPECT_DATABASE is required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid bootstrap DSN (suppressed)")
	}
	if cfg.ConnConfig.Database != expected {
		t.Fatal("DSN database != S0_EXPECT_DATABASE")
	}
	cfg.ConnConfig.Tracer = nil
	cfg.MaxConns = 2
	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("cannot open bootstrap pool (suppressed)")
	}
	t.Cleanup(admin.Close)
	var actual string
	must(t, admin.QueryRow(ctx, "SELECT current_database()").Scan(&actual))
	if actual != expected {
		t.Fatal("live database != S0_EXPECT_DATABASE")
	}
	id, err := randomHex(6)
	must(t, err)
	prefix := "s0wk_" + id + "_"
	f := &fixture{admin: admin, id: id, owner: prefix + "owner", login: prefix + "login", caller: prefix + "caller", jobs: prefix + "jobs", data: prefix + "data"}
	report.Fixtures = append(report.Fixtures, id)
	t.Logf("retained fixture id=%s prefix=%s; NO automatic DROP", id, prefix)
	exec := func(sql string) { t.Helper(); must(t, safeError("bootstrap DDL", execSQL(ctx, admin, sql))) }
	for _, role := range []string{f.owner, f.caller, f.jobs} {
		exec("CREATE ROLE " + ident(role) + " NOLOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS")
	}
	password, err := randomHex(24)
	must(t, err)
	// Generated hex only: PostgreSQL password DDL cannot bind parameters.
	exec("CREATE ROLE " + ident(f.login) + " LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '" + password + "'")
	for _, role := range []string{f.caller, f.jobs} {
		exec("GRANT " + ident(role) + " TO " + ident(f.login) + " WITH INHERIT FALSE, SET TRUE")
	}
	for _, schema := range []string{f.jobs, f.data} {
		exec("CREATE SCHEMA " + ident(schema) + " AUTHORIZATION " + ident(f.owner))
	}
	migrationCfg := cfg.Copy()
	migrationCfg.ConnConfig.RuntimeParams = map[string]string{"search_path": "pg_catalog"}
	migrationCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE "+ident(f.owner))
		return safeError("migration owner", err)
	}
	migrations, err := pgxpool.NewWithConfig(ctx, migrationCfg)
	must(t, safeError("migration pool", err))
	defer migrations.Close()
	migrator, err := rivermigrate.New(riverpgxv5.New(migrations), &rivermigrate.Config{Schema: f.jobs, Logger: quiet})
	must(t, safeError("pinned migrator", err))
	_, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	must(t, safeError("unmodified River migrations", err))
	must(t, f.setup(ctx))
	f.base = cfg.Copy()
	f.base.ConnConfig.User, f.base.ConnConfig.Password = f.login, password
	f.base.ConnConfig.RuntimeParams = map[string]string{"search_path": "pg_catalog", "application_name": "s0workers"}
	f.base.MaxConns = 6 // listener hijacks its own configured connection
	f.callerPool, err = pgxpool.NewWithConfig(ctx, f.base.Copy())
	must(t, safeError("runtime pool", err))
	t.Cleanup(f.callerPool.Close)
	must(t, f.verify(ctx))
	f.inserter, err = river.NewClient(riverpgxv5.New(f.callerPool), &river.Config{Schema: f.jobs, Logger: quiet})
	must(t, safeError("insert-only client", err))
	return f
}

func execSQL(ctx context.Context, pool *pgxpool.Pool, sql string) error {
	_, err := pool.Exec(ctx, sql)
	return err
}

func (f *fixture) setup(ctx context.Context) error {
	tx, err := f.admin.Begin(ctx)
	if err != nil {
		return safeError("begin DDL", err)
	}
	defer tx.Rollback(context.Background())
	var actualTables []string
	if err := tx.QueryRow(ctx, `SELECT coalesce(array_agg(c.relname::text ORDER BY c.relname::text),ARRAY[]::text[]) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relkind IN ('r','p')`, f.jobs).Scan(&actualTables); err != nil {
		return safeError("fixture migration inventory", err)
	}
	if !slices.Equal(actualTables, riverTables) {
		return &redactedError{stage: "fixture migration inventory", state: "unexpected_table_set"}
	}
	job, result := table(f.jobs, "river_job"), table(f.data, "result")
	statements := []string{
		"ALTER TABLE " + job + " ADD COLUMN tenant text NOT NULL DEFAULT nullif(current_setting('s0wk.tenant',true),''), ADD COLUMN scope_id text NOT NULL DEFAULT nullif(current_setting('s0wk.scope_id',true),''), ADD COLUMN writer_xid xid8 NOT NULL DEFAULT pg_current_xact_id()",
		"CREATE TABLE " + result + " (job_id bigint PRIMARY KEY, tenant text NOT NULL DEFAULT nullif(current_setting('s0wk.tenant',true),''), scope_id text NOT NULL DEFAULT nullif(current_setting('s0wk.scope_id',true),''), writer_xid xid8 NOT NULL DEFAULT pg_current_xact_id(), idempotency_id text NOT NULL)",
		"ALTER TABLE " + result + " OWNER TO " + ident(f.owner),
		"GRANT USAGE ON SCHEMA " + ident(f.jobs) + " TO " + ident(f.jobs) + "," + ident(f.caller),
		"GRANT USAGE ON SCHEMA " + ident(f.data) + " TO " + ident(f.caller),
		"GRANT SELECT, INSERT ON " + result + " TO " + ident(f.caller),
		"GRANT SELECT ON " + job + " TO " + ident(f.caller),
		"GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA " + ident(f.jobs) + " TO " + ident(f.jobs),
	}
	// Every migrated table is forced, including metadata. Trusted jobs access is
	// intentionally global: fetch/completion/snooze, leader/rescue/cleanup, queue,
	// notification outbox, and migration metadata. Dropped client tables are NOT
	// recreated or conditionally skipped. Providing these policies does NOT
	// claim each background path has been individually exercised.
	for _, name := range riverTables {
		q := table(f.jobs, name)
		priv := "SELECT, INSERT, UPDATE, DELETE"
		if name == "river_migration" {
			priv = "SELECT"
		}
		statements = append(statements, "ALTER TABLE "+q+" ENABLE ROW LEVEL SECURITY", "ALTER TABLE "+q+" FORCE ROW LEVEL SECURITY", "GRANT "+priv+" ON "+q+" TO "+ident(f.jobs), "CREATE POLICY provider_jobs ON "+q+" TO "+ident(f.jobs)+" USING (true) WITH CHECK (true)")
	}
	using := "tenant=nullif(current_setting('s0wk.tenant',true),'') AND nullif(current_setting('s0wk.scope_id',true),'') IS NOT NULL"
	statements = append(statements,
		"CREATE POLICY caller_rows ON "+job+" FOR SELECT TO "+ident(f.caller)+" USING ("+using+")",
		"ALTER TABLE "+result+" ENABLE ROW LEVEL SECURITY", "ALTER TABLE "+result+" FORCE ROW LEVEL SECURITY",
		"CREATE POLICY caller_scope ON "+result+" TO "+ident(f.caller)+" USING ("+using+") WITH CHECK ("+using+" AND scope_id=nullif(current_setting('s0wk.scope_id',true),'') AND writer_xid=pg_current_xact_id())")
	for i, sql := range statements {
		if _, err := tx.Exec(ctx, sql); err != nil {
			return safeError(fmt.Sprintf("fixture DDL statement %d/%d", i+1, len(statements)), err)
		}
	}
	return safeError("commit DDL", tx.Commit(ctx))
}

func (f *fixture) verify(ctx context.Context) error {
	var good bool
	// The runtime login and both measured roles must be nonowners and lack
	// superuser/bypass/creation/inheritance privileges. Owner remains separate.
	err := f.callerPool.QueryRow(ctx, `SELECT count(*)=3 AND bool_and(NOT r.rolsuper AND NOT r.rolbypassrls AND NOT r.rolcreatedb AND NOT r.rolcreaterole AND NOT r.rolreplication AND NOT r.rolinherit AND r.rolcanlogin=(r.rolname=$2) AND NOT EXISTS(SELECT 1 FROM pg_class c WHERE c.relowner=r.oid) AND NOT EXISTS(SELECT 1 FROM pg_namespace n WHERE n.nspowner=r.oid)) FROM pg_roles r WHERE r.rolname=ANY($1)`, []string{f.login, f.caller, f.jobs}, f.login).Scan(&good)
	if err != nil {
		return safeError("runtime facts", err)
	}
	if !good {
		return errors.New("runtime must be confined nonowner")
	}
	err = f.callerPool.QueryRow(ctx, `SELECT count(*)=2 AND bool_and(r.rolname=ANY($2) AND NOT m.admin_option AND NOT m.inherit_option AND m.set_option) AND count(DISTINCT r.rolname)=2 FROM pg_auth_members m JOIN pg_roles r ON r.oid=m.roleid JOIN pg_roles member ON member.oid=m.member WHERE member.rolname=$1`, f.login, []string{f.caller, f.jobs}).Scan(&good)
	if err != nil {
		return safeError("memberships", err)
	}
	if !good {
		return errors.New("memberships differ from original known-unsafe caller/jobs fixture")
	}
	var actualRiver, actualData []string
	err = f.admin.QueryRow(ctx, `SELECT
	 coalesce(array_agg(c.relname::text ORDER BY c.relname::text) FILTER (WHERE n.nspname=$1),ARRAY[]::text[]),
	 coalesce(array_agg(c.relname::text ORDER BY c.relname::text) FILTER (WHERE n.nspname=$3),ARRAY[]::text[]),
	 coalesce(bool_and(c.relrowsecurity AND c.relforcerowsecurity AND r.rolname=$2 AND NOT r.rolsuper AND NOT r.rolbypassrls AND NOT r.rolcanlogin),false)
	 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_roles r ON r.oid=c.relowner
	 WHERE n.nspname IN ($1,$3) AND c.relkind IN ('r','p')`, f.jobs, f.owner, f.data).Scan(&actualRiver, &actualData, &good)
	if err != nil {
		return safeError("forced tables", err)
	}
	if !good || !slices.Equal(actualRiver, riverTables) || !slices.Equal(actualData, []string{"result"}) {
		return &redactedError{stage: "forced tables: exact five River plus result", state: "unexpected_table_owner_or_rls"}
	}
	var user, fsync, sync string
	if err := f.callerPool.QueryRow(ctx, "SELECT current_user,current_setting('fsync'),current_setting('synchronous_commit')").Scan(&user, &fsync, &sync); err != nil {
		return safeError("durability", err)
	}
	if user != f.login || fsync != "on" || sync != "on" {
		return errors.New("require own runtime login, fsync=on and synchronous_commit=on")
	}
	return nil
}

func (f *fixture) beginCaller(ctx context.Context, scope callScope) (pgx.Tx, error) {
	tx, err := f.callerPool.Begin(ctx)
	if err != nil {
		return nil, safeError("begin caller", err)
	}
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE "+ident(f.caller)); err == nil {
		_, err = tx.Exec(ctx, "SELECT set_config('s0wk.tenant',$1,true),set_config('s0wk.scope_id',$2,true)", scope.tenant, scope.id)
	}
	if err != nil {
		_ = tx.Rollback(context.Background())
		return nil, safeError("stamp caller", err)
	}
	return tx, nil
}

func (f *fixture) enqueue(t *testing.T, ctx context.Context, scope callScope, args workerArgs) *rivertype.JobInsertResult {
	t.Helper()
	tx, err := f.beginCaller(ctx, scope)
	must(t, err)
	defer tx.Rollback(context.Background())
	_, err = tx.Exec(ctx, "SET LOCAL ROLE "+ident(f.jobs))
	must(t, err)
	res, insertErr := f.inserter.InsertTx(ctx, tx, args, &river.InsertOpts{Queue: queue, UniqueOpts: river.UniqueOpts{ByArgs: true}})
	_, restoreErr := tx.Exec(ctx, "SET LOCAL ROLE "+ident(f.caller))
	// Preserve both classifications if insert aborts the transaction; restoration
	// failure must not erase the original insert failure. Never log err.Error().
	if insertErr != nil || restoreErr != nil {
		t.Fatalf("enqueue SQLSTATE=%s restore SQLSTATE=%s", sqlState(insertErr), sqlState(restoreErr))
	}
	must(t, tx.Commit(ctx))
	return res
}

func (f *fixture) workerPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	cfg := f.base.Copy()
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE "+ident(f.jobs))
		return safeError("trusted jobs session", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	must(t, safeError("worker pool", err))
	t.Cleanup(pool.Close)
	var user, tenant, id string
	must(t, pool.QueryRow(ctx, "SELECT current_user,coalesce(current_setting('s0wk.tenant',true),''),coalesce(current_setting('s0wk.scope_id',true),'')").Scan(&user, &tenant, &id))
	if user != f.jobs || tenant != "" || id != "" {
		t.Fatal("worker pool must have trusted jobs role, not a frontend scope")
	}
	return pool
}

func caseContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	return ctx, cancel
}
