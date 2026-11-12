// SPDX-License-Identifier: Apache-2.0

package s0probe

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fixture struct {
	admin                                             *pgxpool.Pool
	runtime                                           *pgxpool.Pool
	id, database                                      string
	login, owner, caller, jobs, events, audit, worker string
	callerSchema, eventsSchema, auditSchema           string
}

func ident(name string) string              { return pgx.Identifier{name}.Sanitize() }
func qualified(schema, table string) string { return pgx.Identifier{schema, table}.Sanitize() }

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// openFixture creates only new names in an explicitly named dedicated database.
// It never drops databases, schemas, roles, or files. The owner must reclaim the
// dedicated DB after inspecting the retained identity in the report.
func openFixture(t testing.TB, ctx context.Context) *fixture {
	t.Helper()
	dsn := os.Getenv("S0_ADMIN_DSN")
	if dsn == "" {
		t.Skip("not measured: S0_ADMIN_DSN absent; dedicated database required")
	}
	expected := os.Getenv("S0_EXPECT_DATABASE")
	if expected == "" {
		t.Fatal("S0_EXPECT_DATABASE must explicitly name the verified dedicated database")
	}
	adminConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("parse bootstrap configuration (DSN suppressed)")
	}
	if adminConfig.ConnConfig.Database != expected {
		t.Fatal("bootstrap DSN does not name S0_EXPECT_DATABASE")
	}
	adminConfig.MaxConns = 2
	admin, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatal("open bootstrap pool (DSN suppressed)")
	}
	t.Cleanup(admin.Close)
	var actual string
	if err := admin.QueryRow(ctx, "SELECT current_database()").Scan(&actual); err != nil {
		t.Fatal("bootstrap database verification failed")
	}
	if actual != expected {
		t.Fatal("live bootstrap database differs from explicit expected database")
	}
	id, err := randomHex(6)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "s0_" + id + "_"
	f := &fixture{admin: admin, id: id, database: actual,
		login: prefix + "runtime", owner: prefix + "owner", caller: prefix + "caller",
		jobs: prefix + "jobs", events: prefix + "events", audit: prefix + "audit", worker: prefix + "worker",
		callerSchema: prefix + "caller", eventsSchema: prefix + "events", auditSchema: prefix + "audit"}
	for _, role := range []string{f.owner, f.caller, f.jobs, f.events, f.audit, f.worker} {
		if _, err := admin.Exec(ctx, "CREATE ROLE "+ident(role)+" NOLOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS"); err != nil {
			t.Fatal("create confined fixture role: ", sqlState(err))
		}
	}

	var runtimeConfig *pgxpool.Config
	if runtimeDSN := os.Getenv("S0_RUNTIME_DSN"); runtimeDSN != "" {
		runtimeConfig, err = pgxpool.ParseConfig(runtimeDSN)
		if err != nil {
			t.Fatal("parse runtime configuration (DSN suppressed)")
		}
		if runtimeConfig.ConnConfig.Host != adminConfig.ConnConfig.Host || runtimeConfig.ConnConfig.Port != adminConfig.ConnConfig.Port || runtimeConfig.ConnConfig.Database != expected {
			t.Fatal("runtime DSN must name the same verified dedicated endpoint and database")
		}
		f.login = runtimeConfig.ConnConfig.User
	} else {
		password, err := randomHex(24)
		if err != nil {
			t.Fatal(err)
		}
		// Password bytes are locally generated hexadecimal, never caller input,
		// logged, exported, or persisted. PostgreSQL role DDL has no bind params.
		if _, err := admin.Exec(ctx, "CREATE ROLE "+ident(f.login)+" LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD '"+password+"'"); err != nil {
			t.Fatal("create fixture runtime login: ", sqlState(err))
		}
		runtimeConfig = adminConfig.Copy()
		runtimeConfig.ConnConfig.User, runtimeConfig.ConnConfig.Password = f.login, password
	}
	runtimeConfig.MaxConns = 8
	for _, role := range []string{f.caller, f.jobs} {
		if _, err := admin.Exec(ctx, "GRANT "+ident(role)+" TO "+ident(f.login)+" WITH INHERIT FALSE, SET TRUE"); err != nil {
			t.Fatal("grant explicit role-switch capability: ", sqlState(err))
		}
	}
	f.runtime, err = pgxpool.NewWithConfig(ctx, runtimeConfig)
	if err != nil {
		t.Fatal("open runtime pool (DSN suppressed)")
	}
	t.Cleanup(f.runtime.Close)
	if err := f.verifyRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.setupBoundaryTables(ctx); err != nil {
		t.Fatal(err)
	}
	t.Logf("retained fixture id=%s database=%s; no automatic destructive cleanup", id, actual)
	return f
}

func (f *fixture) verifyRuntime(ctx context.Context) error {
	var user string
	var super, bypass, createdb, createrole, owns bool
	var fsync, sync string
	err := f.runtime.QueryRow(ctx, `SELECT current_user, r.rolsuper, r.rolbypassrls, r.rolcreatedb, r.rolcreaterole,
		EXISTS (SELECT 1 FROM pg_class c WHERE c.relowner=r.oid AND c.relkind IN ('r','p')),
		current_setting('fsync'), current_setting('synchronous_commit') FROM pg_roles r WHERE r.rolname=current_user`).Scan(&user, &super, &bypass, &createdb, &createrole, &owns, &fsync, &sync)
	if err != nil {
		return fmt.Errorf("verify runtime role: SQLSTATE %s", sqlState(err))
	}
	if user != f.login || super || bypass || createdb || createrole || owns {
		return errors.New("runtime must be a nonowner NOSUPERUSER NOBYPASSRLS login without role/database creation")
	}
	if fsync != "on" || sync != "on" {
		return errors.New("fixture requires actual fsync=on and synchronous_commit=on")
	}
	return nil
}

func (f *fixture) setupBoundaryTables(ctx context.Context) error {
	statements := []string{
		"CREATE SCHEMA " + ident(f.callerSchema) + " AUTHORIZATION " + ident(f.owner),
		"CREATE SCHEMA " + ident(f.eventsSchema) + " AUTHORIZATION " + ident(f.owner),
		"CREATE SCHEMA " + ident(f.auditSchema) + " AUTHORIZATION " + ident(f.owner),
		"CREATE TABLE " + qualified(f.callerSchema, "state") + " (request_id text PRIMARY KEY, tenant text NOT NULL DEFAULT nullif(current_setting('s0.tenant', true), ''), body jsonb NOT NULL)",
		"CREATE TABLE " + qualified(f.eventsSchema, "outbox") + " (request_id text PRIMARY KEY, tenant text NOT NULL DEFAULT nullif(current_setting('s0.tenant', true), ''), body jsonb NOT NULL)",
		"CREATE TABLE " + qualified(f.auditSchema, "audit") + " (request_id text PRIMARY KEY, tenant text NOT NULL DEFAULT nullif(current_setting('s0.tenant', true), ''), body jsonb NOT NULL)",
	}
	for _, spec := range []struct{ schema, table, role string }{{f.callerSchema, "state", f.caller}, {f.eventsSchema, "outbox", f.events}, {f.auditSchema, "audit", f.audit}} {
		q := qualified(spec.schema, spec.table)
		statements = append(statements,
			"ALTER TABLE "+q+" OWNER TO "+ident(f.owner),
			"ALTER TABLE "+q+" ENABLE ROW LEVEL SECURITY",
			"ALTER TABLE "+q+" FORCE ROW LEVEL SECURITY",
			"CREATE POLICY scope ON "+q+" USING (tenant=nullif(current_setting('s0.tenant', true), '')) WITH CHECK (tenant=nullif(current_setting('s0.tenant', true), ''))",
			"GRANT USAGE ON SCHEMA "+ident(spec.schema)+" TO "+ident(spec.role),
			"GRANT SELECT, INSERT, UPDATE ON "+q+" TO "+ident(spec.role))
	}
	// Seeding uses bootstrap only; every measured/scoped operation uses runtime.
	statements = append(statements,
		"INSERT INTO "+qualified(f.callerSchema, "state")+" (request_id,tenant,body) VALUES ('seed-a','scope-a','{}'),('seed-b','scope-b','{}')")
	for _, statement := range statements {
		if _, err := f.admin.Exec(ctx, statement); err != nil {
			return fmt.Errorf("setup fixture boundary: SQLSTATE %s", sqlState(err))
		}
	}
	return nil
}

func (f *fixture) beginCaller(ctx context.Context, scope string) (pgx.Tx, error) {
	tx, err := f.runtime.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE "+ident(f.caller)); err == nil {
		_, err = tx.Exec(ctx, "SELECT set_config('s0.tenant',$1,true)", scope)
	}
	if err != nil {
		_ = tx.Rollback(context.Background())
		return nil, err
	}
	return tx, nil
}

func sqlState(err error) string {
	if err == nil {
		return ""
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
