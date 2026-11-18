// SPDX-License-Identifier: Apache-2.0
package s0txprobe

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

type fixture struct {
	admin, runtime                                *pgxpool.Pool
	client                                        *river.Client[pgx.Tx]
	id, login, owner, caller, jobs, events, audit string
}

// These are data, not an SQL-authority capability. Raw caller SQL can tamper with
// the custom GUCs and switch to jobs. This known-failed boundary is NOT repaired.
type callScope struct{ tenant, id string }
type payload struct {
	Tenant string `json:"tenant"`
}

func (payload) Kind() string { return "s0txprobe_atomicity" }

const hostileBody = `{"tenant":"malicious-payload-tenant"}`

func ident(s string) string            { return pgx.Identifier{s}.Sanitize() }
func qualified(s, table string) string { return pgx.Identifier{s, table}.Sanitize() }
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
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
func dbError(stage string, err error) error {
	if err == nil {
		return nil
	}
	// Never interpolate server error detail, connection strings, or credentials.
	return fmt.Errorf("%s: SQLSTATE %s", stage, sqlState(err))
}

// Fresh names only. Closing connections is the ONLY automatic cleanup. The
// fixture ID is emitted before DDL so partial bootstrap can also be reclaimed.
func openFixture(t testing.TB, ctx context.Context) *fixture {
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
	cfg.MaxConns = 2
	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("cannot open bootstrap pool (DSN suppressed)")
	}
	t.Cleanup(admin.Close)
	var actual string
	if err := admin.QueryRow(ctx, "SELECT current_database()").Scan(&actual); err != nil {
		t.Fatal(dbError("verify database", err))
	}
	if actual != expected {
		t.Fatal("live database != S0_EXPECT_DATABASE")
	}
	id, err := randomHex(6)
	if err != nil {
		t.Fatal("cannot generate fixture ID")
	}
	prefix := "s0tx_" + id + "_"
	f := &fixture{admin: admin, id: id, login: prefix + "login", owner: prefix + "owner", caller: prefix + "caller", jobs: prefix + "jobs", events: prefix + "events", audit: prefix + "audit"}
	report.FixtureID, report.Retained = id, true
	t.Logf("retained fixture id=%s prefix=%s; NO automatic DROP", id, prefix)
	exec := func(sql string) {
		t.Helper()
		if _, err := admin.Exec(ctx, sql); err != nil {
			t.Fatal(dbError("bootstrap", err))
		}
	}
	for _, role := range []string{f.owner, f.caller, f.jobs, f.events, f.audit} {
		exec("CREATE ROLE " + ident(role) + " NOLOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS")
	}
	password, err := randomHex(24)
	if err != nil {
		t.Fatal("cannot generate runtime credential")
	}
	// Generated hex only, never logged/persisted; PostgreSQL role DDL cannot bind.
	exec("CREATE ROLE " + ident(f.login) + " LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '" + password + "'")
	// Exactly the ORIGINAL KNOWN-FAILED membership mechanism, not an elevation fix.
	for _, role := range []string{f.caller, f.jobs} {
		exec("GRANT " + ident(role) + " TO " + ident(f.login) + " WITH INHERIT FALSE, SET TRUE")
	}
	for _, schema := range []string{f.caller, f.jobs, f.events, f.audit} {
		exec("CREATE SCHEMA " + ident(schema) + " AUTHORIZATION " + ident(f.owner))
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	migrationCfg := cfg.Copy()
	migrationCfg.ConnConfig.RuntimeParams = map[string]string{"search_path": "pg_catalog"}
	migrationCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE "+ident(f.owner))
		return dbError("migration owner", err)
	}
	migrations, err := pgxpool.NewWithConfig(ctx, migrationCfg)
	if err != nil {
		t.Fatal("cannot open separate migration pool")
	}
	defer migrations.Close()
	migrator, err := rivermigrate.New(riverpgxv5.New(migrations), &rivermigrate.Config{Schema: f.jobs, Logger: logger})
	if err != nil {
		t.Fatal("cannot construct pinned River migrator")
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		t.Fatal(dbError("River package migrations", err))
	}
	if err := f.setupTables(ctx); err != nil {
		t.Fatal(err)
	}
	runtimeCfg := cfg.Copy()
	runtimeCfg.ConnConfig.User, runtimeCfg.ConnConfig.Password = f.login, password
	runtimeCfg.ConnConfig.RuntimeParams = map[string]string{"search_path": "pg_catalog", "application_name": "s0txprobe"}
	runtimeCfg.MaxConns = 1 // deterministic connection reuse, not a load result
	f.runtime, err = pgxpool.NewWithConfig(ctx, runtimeCfg)
	if err != nil {
		t.Fatal("cannot open fresh runtime pool (DSN suppressed)")
	}
	t.Cleanup(f.runtime.Close)
	if err := f.verifyRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	f.client, err = river.NewClient(riverpgxv5.New(f.runtime), &river.Config{Schema: f.jobs, Logger: logger})
	if err != nil {
		t.Fatal("cannot construct insert-only River client")
	}
	return f
}

func (f *fixture) setupTables(ctx context.Context) error {
	tx, err := f.admin.Begin(ctx)
	if err != nil {
		return dbError("begin fixture DDL", err)
	}
	defer tx.Rollback(context.Background())
	var statements []string
	identity := "tenant text NOT NULL DEFAULT nullif(current_setting('s0.tenant',true),''), scope_id text PRIMARY KEY DEFAULT nullif(current_setting('s0.scope_id',true),''), writer_xid xid8 NOT NULL DEFAULT pg_current_xact_id()"
	for _, spec := range []struct{ schema, table, role string }{{f.caller, "state", f.caller}, {f.events, "outbox", f.events}, {f.audit, "audit", f.audit}} {
		q := qualified(spec.schema, spec.table)
		statements = append(statements, "CREATE TABLE "+q+" ("+identity+", body jsonb NOT NULL)", "ALTER TABLE "+q+" OWNER TO "+ident(f.owner), "GRANT USAGE ON SCHEMA "+ident(spec.schema)+" TO "+ident(spec.role), "GRANT INSERT ON "+q+" TO "+ident(spec.role))
	}
	// Experimental provider extension only: no upstream SQL copied or modified,
	// and no stock tenant API / accepted S3 schema is implied by these columns.
	job := qualified(f.jobs, "river_job")
	statements = append(statements, "ALTER TABLE "+job+" ADD COLUMN tenant text NOT NULL DEFAULT nullif(current_setting('s0.tenant',true),''), ADD COLUMN scope_id text NOT NULL DEFAULT nullif(current_setting('s0.scope_id',true),''), ADD COLUMN writer_xid xid8 NOT NULL DEFAULT pg_current_xact_id()")
	// ADR-045 (2026-11-18): River's own tables carry no RLS and no policy; only the
	// jobs role holds privileges on them and the caller role holds none.
	for _, spec := range []struct{ schema, table string }{{f.caller, "state"}, {f.events, "outbox"}, {f.audit, "audit"}} {
		q := qualified(spec.schema, spec.table)
		statements = append(statements, "ALTER TABLE "+q+" ENABLE ROW LEVEL SECURITY", "ALTER TABLE "+q+" FORCE ROW LEVEL SECURITY", "CREATE POLICY scope ON "+q+" USING (tenant=nullif(current_setting('s0.tenant',true),'') AND nullif(current_setting('s0.scope_id',true),'') IS NOT NULL) WITH CHECK (tenant=nullif(current_setting('s0.tenant',true),'') AND scope_id=nullif(current_setting('s0.scope_id',true),'') AND writer_xid=pg_current_xact_id())")
	}
	statements = append(statements, "GRANT SELECT, UPDATE ON "+qualified(f.caller, "state")+" TO "+ident(f.caller), "GRANT USAGE ON SCHEMA "+ident(f.jobs)+" TO "+ident(f.jobs), "GRANT SELECT, INSERT, UPDATE ON "+job+" TO "+ident(f.jobs), "GRANT USAGE ON SEQUENCE "+qualified(f.jobs, "river_job_id_seq")+" TO "+ident(f.jobs))
	for _, spec := range []struct{ schema, table, role string }{{f.events, "outbox", f.events}, {f.audit, "audit", f.audit}} {
		fn := qualified(spec.schema, "append_scoped") + "(jsonb)"
		statements = append(statements, "GRANT CREATE ON SCHEMA "+ident(spec.schema)+" TO "+ident(spec.role))
		statements = append(statements, "CREATE FUNCTION "+qualified(spec.schema, "append_scoped")+"(jsonb) RETURNS void LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $s0$ INSERT INTO "+qualified(spec.schema, spec.table)+" (body) VALUES ($1) $s0$", "ALTER FUNCTION "+fn+" OWNER TO "+ident(spec.role), "REVOKE ALL ON FUNCTION "+fn+" FROM PUBLIC", "GRANT EXECUTE ON FUNCTION "+fn+" TO "+ident(f.caller), "GRANT USAGE ON SCHEMA "+ident(spec.schema)+" TO "+ident(f.caller), "REVOKE CREATE ON SCHEMA "+ident(spec.schema)+" FROM "+ident(spec.role))
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return dbError("owned fixture DDL", err)
		}
	}
	return dbError("commit fixture DDL", tx.Commit(ctx))
}

type roleFacts struct {
	Name                                                                   string
	Login, Inherit, Super, Bypass, CreateDB, CreateRole, Replication, Owns bool
}

func confinedRole(r roleFacts, login bool) bool {
	return r.Login == login && !r.Inherit && !r.Super && !r.Bypass && !r.CreateDB && !r.CreateRole && !r.Replication && !r.Owns
}

type membership struct {
	Role                string
	Admin, Inherit, Set bool
}

func exactMemberships(ms []membership, caller, jobs string) bool {
	seen := map[string]bool{}
	for _, m := range ms {
		if (m.Role != caller && m.Role != jobs) || seen[m.Role] || m.Admin || m.Inherit || !m.Set {
			return false
		}
		seen[m.Role] = true
	}
	return len(ms) == 2 && seen[caller] && seen[jobs]
}
func (f *fixture) verifyRuntime(ctx context.Context) error {
	rows, err := f.runtime.Query(ctx, `SELECT r.rolname,r.rolcanlogin,r.rolinherit,r.rolsuper,r.rolbypassrls,r.rolcreatedb,r.rolcreaterole,r.rolreplication,
	 EXISTS(SELECT 1 FROM pg_class WHERE relowner=r.oid) OR EXISTS(SELECT 1 FROM pg_namespace WHERE nspowner=r.oid)
	 FROM pg_roles r WHERE rolname=ANY($1)`, []string{f.login, f.caller, f.jobs, f.events, f.audit})
	if err != nil {
		return dbError("verify roles", err)
	}
	n := 0
	for rows.Next() {
		var r roleFacts
		if err := rows.Scan(&r.Name, &r.Login, &r.Inherit, &r.Super, &r.Bypass, &r.CreateDB, &r.CreateRole, &r.Replication, &r.Owns); err != nil {
			rows.Close()
			return dbError("read role facts", err)
		}
		n++
		if !confinedRole(r, r.Name == f.login) {
			rows.Close()
			return errors.New("runtime roles must be confined nonowners")
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return dbError("read role facts", err)
	}
	if n != 5 {
		return errors.New("missing fixture role")
	}
	rows, err = f.runtime.Query(ctx, `SELECT r.rolname,m.admin_option,m.inherit_option,m.set_option FROM pg_auth_members m JOIN pg_roles r ON r.oid=m.roleid JOIN pg_roles member ON member.oid=m.member WHERE member.rolname=$1`, f.login)
	if err != nil {
		return dbError("verify memberships", err)
	}
	var ms []membership
	for rows.Next() {
		var m membership
		if err := rows.Scan(&m.Role, &m.Admin, &m.Inherit, &m.Set); err != nil {
			rows.Close()
			return dbError("read memberships", err)
		}
		ms = append(ms, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return dbError("read memberships", err)
	}
	if !exactMemberships(ms, f.caller, f.jobs) {
		return errors.New("login memberships differ from original known-failed caller/jobs mechanism")
	}
	var user, fsync, sync string
	if err := f.runtime.QueryRow(ctx, "SELECT current_user,current_setting('fsync'),current_setting('synchronous_commit')").Scan(&user, &fsync, &sync); err != nil {
		return dbError("durability settings", err)
	}
	if user != f.login || fsync != "on" || sync != "on" {
		return errors.New("wrong runtime login or durability settings; require fsync=on synchronous_commit=on")
	}
	report.Fsync, report.SynchronousCommit = fsync, sync
	var good bool
	if err := f.admin.QueryRow(ctx, `SELECT count(*)=4 AND bool_and(CASE WHEN c.relname='river_job' THEN NOT c.relrowsecurity AND NOT c.relforcerowsecurity ELSE c.relrowsecurity AND c.relforcerowsecurity END AND r.rolname=$2 AND NOT r.rolcanlogin AND NOT r.rolbypassrls AND NOT r.rolsuper) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_roles r ON r.oid=c.relowner WHERE n.nspname=ANY($1) AND c.relname IN ('state','river_job','outbox','audit')`, []string{f.caller, f.jobs, f.events, f.audit}, f.owner).Scan(&good); err != nil {
		return dbError("verify RLS/owners", err)
	}
	if !good {
		return errors.New("tenant tables lack separate confined owner/FORCE RLS, or river_job is not RLS-free")
	}
	if err := f.admin.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname='river_job') AND NOT has_schema_privilege($2,$1,'USAGE') AND NOT has_table_privilege($2,quote_ident($1)||'.river_job','SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER')`, f.jobs, f.caller).Scan(&good); err != nil {
		return dbError("verify river_job policy-free and caller-free", err)
	}
	if !good {
		return errors.New("river_job has a policy or the caller role holds privileges on the River schema")
	}
	if err := f.admin.QueryRow(ctx, `SELECT count(*)=2 AND bool_and(p.prosecdef AND p.proowner=r.oid AND NOT r.rolcanlogin AND NOT r.rolbypassrls AND NOT r.rolsuper AND p.proconfig=ARRAY['search_path=pg_catalog, pg_temp'] AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE')) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace JOIN pg_roles r ON r.rolname=n.nspname WHERE n.nspname=ANY($1) AND p.proname='append_scoped'`, []string{f.events, f.audit}).Scan(&good); err != nil {
		return dbError("verify narrow definer functions", err)
	}
	if !good {
		return errors.New("append functions lack narrow confined owners/search_path/ACL")
	}
	return nil
}

func (f *fixture) beginCaller(ctx context.Context, scope *callScope) (pgx.Tx, error) {
	tx, err := f.runtime.Begin(ctx)
	if err != nil {
		return nil, err
	}
	tenant, id := "", ""
	if scope != nil {
		tenant, id = scope.tenant, scope.id
	}
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE "+ident(f.caller)); err == nil {
		_, err = tx.Exec(ctx, "SELECT set_config('s0.tenant',$1,true),set_config('s0.scope_id',$2,true)", tenant, id)
	}
	if err != nil {
		_ = tx.Rollback(context.Background())
		return nil, err
	}
	return tx, nil
}
func (f *fixture) restoreCaller(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, "SET LOCAL ROLE "+ident(f.caller))
	return err
}

type insertFailure struct{ Insert, Restore error }

func (e *insertFailure) Error() string {
	return "InsertTx=" + sqlState(e.Insert) + " restore=" + sqlState(e.Restore)
}
func (e *insertFailure) Unwrap() []error {
	var causes []error
	if e.Insert != nil {
		causes = append(causes, e.Insert)
	}
	if e.Restore != nil {
		causes = append(causes, e.Restore)
	}
	return causes
}
func (f *fixture) insertJob(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+ident(f.jobs)); err != nil {
		return err
	}
	_, insertErr := f.client.InsertTx(ctx, tx, payload{Tenant: "malicious-payload-tenant"}, nil)
	restoreErr := f.restoreCaller(ctx, tx)
	if insertErr != nil || restoreErr != nil {
		return &insertFailure{Insert: insertErr, Restore: restoreErr}
	}
	return nil
}
func (f *fixture) write(ctx context.Context, tx pgx.Tx, participant int) error {
	switch participant {
	case 0:
		_, err := tx.Exec(ctx, "INSERT INTO "+qualified(f.caller, "state")+" (body) VALUES ($1::jsonb)", hostileBody)
		return err
	case 1:
		return f.insertJob(ctx, tx)
	case 2, 3:
		schema := f.events
		if participant == 3 {
			schema = f.audit
		}
		_, err := tx.Exec(ctx, "SELECT "+qualified(schema, "append_scoped")+"($1::jsonb)", hostileBody)
		return err
	}
	return errors.New("invalid participant")
}
