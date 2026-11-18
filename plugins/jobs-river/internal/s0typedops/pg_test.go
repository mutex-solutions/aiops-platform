// SPDX-License-Identifier: Apache-2.0

package s0typedops

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
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// Test-only trusted provider. The domain callback receives *Tx, never this
// fixture, its Host, a pool, pgx.Tx, SQL text, a role, or an issuer callback.
// Role membership/custom GUCs remain unsafe if raw SQL is separately available.
const typedDummyDSN = "postgres://postgres@127.0.0.1:5432/s0typed?sslmode=disable"

var typedRiverTables = []string{"river_job", "river_leader", "river_migration", "river_notification", "river_queue"}

type typedPGEntry struct{ schema, role string }
type typedPGCounts struct{ begins, writes, reads, commits, rollbacks, sql, setupRollbacks int }

// Trusted fixture-only fault selection, never caller SQL or an exported hook.
type typedSetupFault uint8

const (
	typedSetupOK typedSetupFault = iota
	typedSetupSQLAfterRole
	typedSetupCancelAfterScope
)

type typedPGFixture struct {
	admin, runtime                        *pgxpool.Pool
	client                                *river.Client[pgx.Tx]
	host                                  *Host
	id, owner, login, jobs, outbox, audit string
	entries                               [2]Entry
	catalog                               map[*entryToken]typedPGEntry
	counts                                typedPGCounts
	setupFault                            typedSetupFault
}
type typedPGTx struct {
	tx      pgx.Tx
	binding binding
	env     *typedPGFixture
	entry   typedPGEntry
}
type typedJobArgs struct {
	Tenant string `json:"tenant"`
	Scope  string `json:"scope"`
}

func (typedJobArgs) Kind() string { return "s0typedops_closed_record" }

func typedIdent(s string) string            { return pgx.Identifier{s}.Sanitize() }
func typedTable(schema, name string) string { return pgx.Identifier{schema, name}.Sanitize() }
func typedHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func typedSQLState(err error) string {
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
func typedMust(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		var catalog *typedCatalogMismatch
		if errors.As(err, &catalog) {
			t.Fatalf("fixture catalog check failed: %s", catalog.check)
		}
		t.Fatalf("fixture operation failed: SQLSTATE=%s", typedSQLState(err))
	}
}
func typedContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func (f *typedPGFixture) begin(ctx context.Context, b binding) (transactionBackend, error) {
	f.counts.begins++
	entry, ok := f.catalog[b.entry]
	if !ok || b.entry == nil || b.scope == nil || b.schema != entry.schema || b.entry.schema != entry.schema {
		return nil, ErrCatalog
	}
	tx, err := f.runtime.Begin(ctx)
	f.counts.sql++
	if err != nil {
		return nil, err
	}
	// Both the role and SQL identifiers come only from the trusted catalog.
	f.counts.sql++
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE "+typedIdent(entry.role)); err == nil && f.setupFault == typedSetupSQLAfterRole {
		f.counts.sql++
		_, err = tx.Exec(ctx, "SELECT 1/0") // Fixed provider diagnostic: SQLSTATE 22012.
	}
	if err == nil {
		f.counts.sql++
		_, err = tx.Exec(ctx, "SELECT set_config('s0typed.tenant',$1,true),set_config('s0typed.scope_id',$2,true)", b.scope.tenant, b.scope.id)
	}
	if err == nil && f.setupFault == typedSetupCancelAfterScope {
		work, cancel := context.WithCancel(ctx)
		cancel() // Deterministic cancellation after real role and scope setup.
		f.counts.sql++
		_, err = tx.Exec(work, "SELECT 1")
	}
	if err != nil {
		// Host.Run cannot register its ordinary cleanup until begin succeeds.
		// Never let canceled work suppress or indefinitely extend this cleanup.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		f.counts.setupRollbacks++
		f.counts.sql++
		rollbackErr := tx.Rollback(cleanup)
		if errors.Is(rollbackErr, pgx.ErrTxClosed) {
			rollbackErr = nil
		}
		return nil, errors.Join(err, rollbackErr)
	}
	return &typedPGTx{tx: tx, binding: b, env: f, entry: entry}, nil
}
func (p *typedPGTx) write(ctx context.Context, key, value string) error {
	p.env.counts.writes++
	p.env.counts.sql++
	_, err := p.tx.Exec(ctx, "INSERT INTO "+typedTable(p.entry.schema, "records")+" (tenant,scope_id,key,value) VALUES ($1,$2,$3,$4) ON CONFLICT (tenant,scope_id,key) DO UPDATE SET value=EXCLUDED.value,writer_xid=pg_current_xact_id()", p.binding.scope.tenant, p.binding.scope.id, key, value)
	return err
}
func (p *typedPGTx) read(ctx context.Context, key string) (string, bool, error) {
	p.env.counts.reads++
	p.env.counts.sql++
	var value string
	err := p.tx.QueryRow(ctx, "SELECT value FROM "+typedTable(p.entry.schema, "records")+" WHERE tenant=$1 AND scope_id=$2 AND key=$3", p.binding.scope.tenant, p.binding.scope.id, key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}
func (p *typedPGTx) commit(ctx context.Context) error {
	p.env.counts.commits++
	p.env.counts.sql++
	return p.tx.Commit(ctx)
}
func (p *typedPGTx) rollback(ctx context.Context) error {
	p.env.counts.rollbacks++
	p.env.counts.sql++
	err := p.tx.Rollback(ctx)
	if errors.Is(err, pgx.ErrTxClosed) {
		return nil // Expected after an already-closed/failed commit, not a confirmed rollback.
	}
	return err
}

// Fresh PostgreSQL is provisioned by the parent OS sandbox, not by Go. Accept
// only its exact dummy loopback DSN; never discover a service or host credential.
func openTypedPG(t *testing.T, ctx context.Context) *typedPGFixture {
	t.Helper()
	dsn := os.Getenv("S0_TYPED_DSN")
	if dsn == "" {
		t.Skip("NOT measured: explicit S0_TYPED_DSN absent")
	}
	if dsn != typedDummyDSN || os.Getenv("S0_TYPED_EXPECT_DATABASE") != "s0typed" {
		t.Fatal("require the fixed isolated dummy fixture contract (DSN suppressed)")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	typedMust(t, err)
	cfg.MaxConns = 2
	cfg.ConnConfig.Tracer = nil
	cfg.ConnConfig.RuntimeParams = map[string]string{"search_path": "pg_catalog", "application_name": "s0typedops_bootstrap"}
	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	typedMust(t, err)
	t.Cleanup(admin.Close)
	var database, user, fsync, sync string
	typedMust(t, admin.QueryRow(ctx, "SELECT current_database(),current_user,current_setting('fsync'),current_setting('synchronous_commit')").Scan(&database, &user, &fsync, &sync))
	if database != "s0typed" || user != "postgres" || fsync != "on" || sync != "on" {
		t.Fatal("wrong fixture identity/durability")
	}
	id, err := typedHex(6)
	typedMust(t, err)
	prefix := "s0typed_" + id + "_"
	f := &typedPGFixture{admin: admin, id: id, owner: prefix + "owner", login: prefix + "login", jobs: prefix + "jobs", outbox: prefix + "outbox", audit: prefix + "audit", catalog: make(map[*entryToken]typedPGEntry)}
	typedPGReport.Fixtures = append(typedPGReport.Fixtures, id)
	t.Cleanup(func() {
		c := f.counts
		typedPGReport.ProviderCounts[id] = map[string]int{"begin_attempts": c.begins, "put_attempts": c.writes, "get_attempts": c.reads, "commit_attempts": c.commits, "rollback_attempts": c.rollbacks, "begin_setup_rollback_attempts": c.setupRollbacks, "private_driver_call_attempts_not_expanded_River_SQL": c.sql}
	})
	t.Logf("disposable fixture id=%s; no fixture-authored DROP, container teardown is parent-owned", id)
	f.host, err = NewHost(f)
	typedMust(t, err)
	roles := []string{f.owner, f.jobs, f.outbox, f.audit}
	for i := range f.entries {
		schema := fmt.Sprintf("%sentry%d", prefix, i)
		f.entries[i], err = f.host.RegisterEntry(fmt.Sprintf("entry%d", i), schema)
		typedMust(t, err)
		f.catalog[f.entries[i].token] = typedPGEntry{schema: schema, role: schema}
		roles = append(roles, schema)
	}
	exec := func(sql string) { t.Helper(); _, err := admin.Exec(ctx, sql); typedMust(t, err) }
	for _, role := range roles {
		exec("CREATE ROLE " + typedIdent(role) + " NOLOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS")
	}
	password, err := typedHex(24)
	typedMust(t, err)
	// Hex generated here, not user input; PostgreSQL role DDL cannot bind it.
	exec("CREATE ROLE " + typedIdent(f.login) + " LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '" + password + "'")
	for _, role := range []string{f.entries[0].token.schema, f.entries[1].token.schema, f.jobs} {
		exec("GRANT " + typedIdent(role) + " TO " + typedIdent(f.login) + " WITH INHERIT FALSE, SET TRUE")
	}
	for _, schema := range []string{f.entries[0].token.schema, f.entries[1].token.schema, f.jobs, f.outbox, f.audit} {
		exec("CREATE SCHEMA " + typedIdent(schema) + " AUTHORIZATION " + typedIdent(f.owner))
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	migrationCfg := cfg.Copy()
	migrationCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE "+typedIdent(f.owner))
		return err
	}
	migrations, err := pgxpool.NewWithConfig(ctx, migrationCfg)
	typedMust(t, err)
	func() {
		defer migrations.Close()
		migrator, err := rivermigrate.New(riverpgxv5.New(migrations), &rivermigrate.Config{Schema: f.jobs, Logger: logger})
		typedMust(t, err)
		_, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
		typedMust(t, err)
	}()
	f.setup(t, ctx)
	runtimeCfg := cfg.Copy()
	runtimeCfg.MaxConns = 1
	runtimeCfg.ConnConfig.User, runtimeCfg.ConnConfig.Password = f.login, password
	runtimeCfg.ConnConfig.RuntimeParams = map[string]string{"search_path": "pg_catalog", "application_name": "s0typedops_runtime"}
	f.runtime, err = pgxpool.NewWithConfig(ctx, runtimeCfg)
	typedMust(t, err)
	t.Cleanup(f.runtime.Close)
	f.client, err = river.NewClient(riverpgxv5.New(f.runtime), &river.Config{Schema: f.jobs, Logger: logger})
	typedMust(t, err)
	f.verify(t, ctx)
	return f
}

func (f *typedPGFixture) setup(t *testing.T, ctx context.Context) {
	t.Helper()
	tx, err := f.admin.Begin(ctx)
	typedMust(t, err)
	defer tx.Rollback(context.Background())
	var inventory []string
	typedMust(t, tx.QueryRow(ctx, "SELECT coalesce(array_agg(c.relname::text ORDER BY c.relname::text),ARRAY[]::text[]) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relkind IN ('r','p')", f.jobs).Scan(&inventory))
	if !slices.Equal(inventory, typedRiverTables) {
		t.Fatal("unexpected pinned River migration inventory")
	}
	var sql []string
	stamp := "tenant text NOT NULL DEFAULT nullif(current_setting('s0typed.tenant',true),''), scope_id text NOT NULL DEFAULT nullif(current_setting('s0typed.scope_id',true),''), writer_xid xid8 NOT NULL DEFAULT pg_current_xact_id()"
	using := "tenant=nullif(current_setting('s0typed.tenant',true),'') AND scope_id=nullif(current_setting('s0typed.scope_id',true),'')"
	for _, e := range f.entries {
		schema := e.token.schema
		q := typedTable(schema, "records")
		sql = append(sql, "CREATE TABLE "+q+" ("+stamp+", key text NOT NULL, value text NOT NULL, PRIMARY KEY(tenant,scope_id,key))", "ALTER TABLE "+q+" OWNER TO "+typedIdent(f.owner), "ALTER TABLE "+q+" ENABLE ROW LEVEL SECURITY", "ALTER TABLE "+q+" FORCE ROW LEVEL SECURITY", "CREATE POLICY provider_scope ON "+q+" TO "+typedIdent(schema)+" USING ("+using+") WITH CHECK ("+using+" AND writer_xid=pg_current_xact_id())", "GRANT USAGE ON SCHEMA "+typedIdent(schema)+" TO "+typedIdent(schema), "GRANT SELECT, INSERT, UPDATE ON "+q+" TO "+typedIdent(schema))
	}
	job := typedTable(f.jobs, "river_job")
	sql = append(sql, "ALTER TABLE "+job+" ADD COLUMN tenant text NOT NULL DEFAULT nullif(current_setting('s0typed.tenant',true),''), ADD COLUMN scope_id text NOT NULL DEFAULT nullif(current_setting('s0typed.scope_id',true),''), ADD COLUMN writer_xid xid8 NOT NULL DEFAULT pg_current_xact_id()", "GRANT USAGE ON SCHEMA "+typedIdent(f.jobs)+" TO "+typedIdent(f.jobs), "GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA "+typedIdent(f.jobs)+" TO "+typedIdent(f.jobs))
	for _, name := range typedRiverTables {
		q := typedTable(f.jobs, name)
		privileges := "SELECT, INSERT, UPDATE, DELETE"
		if name == "river_migration" {
			privileges = "SELECT"
		}
		// ADR-045 (2026-11-18): River's tables have no RLS and no policy. Only the
		// trusted jobs role holds privileges and it may see all rows; entry roles hold
		// none. No worker/background path is exercised here.
		sql = append(sql, "GRANT "+privileges+" ON "+q+" TO "+typedIdent(f.jobs))
	}
	for _, spec := range []struct{ schema, name string }{{f.outbox, "outbox"}, {f.audit, "audit"}} {
		q := typedTable(spec.schema, spec.name)
		fn := typedTable(spec.schema, "append_scoped")
		sql = append(sql, "CREATE TABLE "+q+" ("+stamp+", body jsonb NOT NULL)", "ALTER TABLE "+q+" OWNER TO "+typedIdent(f.owner), "ALTER TABLE "+q+" ENABLE ROW LEVEL SECURITY", "ALTER TABLE "+q+" FORCE ROW LEVEL SECURITY", "CREATE POLICY provider_scope ON "+q+" TO "+typedIdent(spec.schema)+" USING ("+using+") WITH CHECK ("+using+" AND writer_xid=pg_current_xact_id())", "GRANT USAGE, CREATE ON SCHEMA "+typedIdent(spec.schema)+" TO "+typedIdent(spec.schema), "GRANT INSERT ON "+q+" TO "+typedIdent(spec.schema), "CREATE FUNCTION "+fn+"(jsonb) RETURNS void LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $s0typed$ INSERT INTO "+q+" (body) VALUES ($1) $s0typed$", "ALTER FUNCTION "+fn+"(jsonb) OWNER TO "+typedIdent(spec.schema), "REVOKE ALL ON FUNCTION "+fn+"(jsonb) FROM PUBLIC", "REVOKE CREATE ON SCHEMA "+typedIdent(spec.schema)+" FROM "+typedIdent(spec.schema))
		for _, e := range f.entries {
			sql = append(sql, "GRANT USAGE ON SCHEMA "+typedIdent(spec.schema)+" TO "+typedIdent(e.token.schema), "GRANT EXECUTE ON FUNCTION "+fn+"(jsonb) TO "+typedIdent(e.token.schema))
		}
	}
	for i, statement := range sql {
		if _, err := tx.Exec(ctx, statement); err != nil {
			t.Fatalf("trusted DDL statement %d: SQLSTATE=%s", i, typedSQLState(err))
		}
	}
	typedMust(t, tx.Commit(ctx))
}

// Called only by the trusted same-package participant closure after domain work.
// The raw adapter cannot be recovered through any exported Tx/View operation.
func (p *typedPGTx) participant(ctx context.Context, boundary int) error {
	switch boundary {
	case 1:
		p.env.counts.sql++
		if _, err := p.tx.Exec(ctx, "SET LOCAL ROLE "+typedIdent(p.env.jobs)); err != nil {
			return err
		}
		p.env.counts.sql++
		_, insertErr := p.env.client.InsertTx(ctx, p.tx, typedJobArgs{Tenant: "malicious-payload-tenant", Scope: "malicious-payload-scope"}, nil)
		p.env.counts.sql++
		_, restoreErr := p.tx.Exec(ctx, "SET LOCAL ROLE "+typedIdent(p.entry.role))
		if insertErr != nil || restoreErr != nil {
			return fmt.Errorf("private InsertTx SQLSTATE=%s restore SQLSTATE=%s", typedSQLState(insertErr), typedSQLState(restoreErr))
		}
		return nil
	case 2, 3:
		schema := p.env.outbox
		if boundary == 3 {
			schema = p.env.audit
		}
		p.env.counts.sql++
		_, err := p.tx.Exec(ctx, "SELECT "+typedTable(schema, "append_scoped")+"($1::jsonb)", `{"tenant":"malicious-payload-tenant","scope":"malicious-payload-scope"}`)
		return err
	default:
		return ErrInvalid
	}
}

func (f *typedPGFixture) verify(t *testing.T, ctx context.Context) {
	t.Helper()
	roles := []string{f.login, f.jobs, f.outbox, f.audit, f.entries[0].token.schema, f.entries[1].token.schema}
	var good bool
	typedMust(t, f.runtime.QueryRow(ctx, `SELECT count(*)=6 AND bool_and(NOT r.rolsuper AND NOT r.rolbypassrls AND NOT r.rolcreatedb AND NOT r.rolcreaterole AND NOT r.rolreplication AND NOT r.rolinherit AND r.rolcanlogin=(r.rolname=$2) AND NOT EXISTS(SELECT 1 FROM pg_class c WHERE c.relowner=r.oid) AND NOT EXISTS(SELECT 1 FROM pg_namespace n WHERE n.nspowner=r.oid)) FROM pg_roles r WHERE r.rolname=ANY($1)`, roles, f.login).Scan(&good))
	if !good {
		t.Fatal("runtime roles must be confined nonowners")
	}
	typedMust(t, f.runtime.QueryRow(ctx, `SELECT count(*)=3 AND count(DISTINCT r.rolname)=3 AND bool_and(r.rolname=ANY($2) AND NOT m.admin_option AND NOT m.inherit_option AND m.set_option) FROM pg_auth_members m JOIN pg_roles r ON r.oid=m.roleid JOIN pg_roles member ON member.oid=m.member WHERE member.rolname=$1`, f.login, []string{f.jobs, f.entries[0].token.schema, f.entries[1].token.schema}).Scan(&good))
	if !good {
		t.Fatal("unexpected known-unsafe runtime membership shape")
	}
	schemas := []string{f.jobs, f.outbox, f.audit, f.entries[0].token.schema, f.entries[1].token.schema}
	typedMust(t, f.admin.QueryRow(ctx, `SELECT count(*)=9 AND count(*) FILTER (WHERE n.nspname=$3)=5 AND bool_and(CASE WHEN n.nspname=$3 THEN NOT c.relrowsecurity AND NOT c.relforcerowsecurity ELSE c.relrowsecurity AND c.relforcerowsecurity END AND r.rolname=$2 AND NOT r.rolsuper AND NOT r.rolbypassrls AND NOT r.rolcanlogin) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_roles r ON r.oid=c.relowner WHERE n.nspname=ANY($1) AND c.relkind IN ('r','p')`, schemas, f.owner, f.jobs).Scan(&good))
	if !good {
		t.Fatal("require exact four forced tables, five River tables without RLS, and a separate confined owner")
	}
	typedMust(t, f.admin.QueryRow(ctx, `SELECT count(*)=2 AND bool_and(p.prosecdef AND p.proowner=r.oid AND NOT r.rolcanlogin AND NOT r.rolbypassrls AND NOT r.rolsuper AND p.proconfig=ARRAY['search_path=pg_catalog, pg_temp'] AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE')) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace JOIN pg_roles r ON r.rolname=n.nspname WHERE n.nspname=ANY($1) AND p.proname='append_scoped'`, []string{f.outbox, f.audit}).Scan(&good))
	if !good {
		t.Fatal("narrow definer function owner/search_path/ACL mismatch")
	}
	var user, fsync, sync string
	typedMust(t, f.runtime.QueryRow(ctx, "SELECT current_user,current_setting('fsync'),current_setting('synchronous_commit')").Scan(&user, &fsync, &sync))
	if user != f.login || fsync != "on" || sync != "on" {
		t.Fatal("runtime login/durability mismatch")
	}
	typedMust(t, f.verifyCatalog(ctx, f.admin))
}

// This verifier is fixed-fixture preflight, not a production migration admission
// or concurrent DDL invalidation protocol. Query support is private to trusted code.
type typedCatalogReader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}
type typedCatalogMismatch struct{ check string }

func (e *typedCatalogMismatch) Error() string { return "fixture catalog mismatch: " + e.check }
func (e *typedCatalogMismatch) Unwrap() error { return ErrCatalog }

type typedCatalogColumn struct {
	name, kind, def string
	required        bool
}
type typedCatalogTable struct {
	schema, name, role, grants string
	columns                    []typedCatalogColumn
	key                        []string
}

func (f *typedPGFixture) catalogTables() []typedCatalogTable {
	identity := []typedCatalogColumn{{"tenant", "text", "NULLIF(current_setting('s0typed.tenant'::text, true), ''::text)", true}, {"scope_id", "text", "NULLIF(current_setting('s0typed.scope_id'::text, true), ''::text)", true}, {"writer_xid", "xid8", "pg_current_xact_id()", true}}
	var tables []typedCatalogTable
	for _, e := range f.entries {
		columns := append([]typedCatalogColumn{}, identity...)
		columns = append(columns, typedCatalogColumn{"key", "text", "", true}, typedCatalogColumn{"value", "text", "", true})
		tables = append(tables, typedCatalogTable{e.token.schema, "records", e.token.schema, "INSERT,SELECT,UPDATE", columns, []string{"tenant", "scope_id", "key"}})
	}
	for _, s := range []struct{ schema, name string }{{f.outbox, "outbox"}, {f.audit, "audit"}} {
		columns := append([]typedCatalogColumn{}, identity...)
		columns = append(columns, typedCatalogColumn{"body", "jsonb", "", true})
		tables = append(tables, typedCatalogTable{s.schema, s.name, s.schema, "INSERT", columns, nil})
	}
	job := []typedCatalogColumn{
		{"id", "bigint", "@sequence:river_job_id_seq", true}, {"state", "@job_state", "@available", true}, {"attempt", "smallint", "0", true}, {"max_attempts", "smallint", "25", true},
		{"attempted_at", "timestamp with time zone", "", false}, {"created_at", "timestamp with time zone", "now()", true}, {"finalized_at", "timestamp with time zone", "", false}, {"scheduled_at", "timestamp with time zone", "now()", true},
		{"priority", "smallint", "1", true}, {"args", "jsonb", "", true}, {"attempted_by", "text[]", "", false}, {"errors", "jsonb[]", "", false}, {"kind", "text", "", true},
		{"metadata", "jsonb", "'{}'::jsonb", true}, {"queue", "text", "'default'::text", true}, {"tags", "character varying(255)[]", "'{}'::character varying[]", true}, {"unique_key", "bytea", "", false}, {"unique_states", "bit(8)", "", false},
	}
	job = append(job, identity...)
	tables = append(tables, typedCatalogTable{f.jobs, "river_job", f.jobs, "DELETE,INSERT,SELECT,UPDATE", job, []string{"id"}},
		typedCatalogTable{f.jobs, "river_leader", f.jobs, "DELETE,INSERT,SELECT,UPDATE", []typedCatalogColumn{{"elected_at", "timestamp with time zone", "", true}, {"expires_at", "timestamp with time zone", "", true}, {"leader_id", "text", "", true}, {"name", "text", "'default'::text", true}}, []string{"name"}},
		typedCatalogTable{f.jobs, "river_migration", f.jobs, "SELECT", []typedCatalogColumn{{"line", "text", "", true}, {"version", "bigint", "", true}, {"created_at", "timestamp with time zone", "now()", true}}, []string{"line", "version"}},
		typedCatalogTable{f.jobs, "river_notification", f.jobs, "DELETE,INSERT,SELECT,UPDATE", []typedCatalogColumn{{"id", "bigint", "@sequence:river_notification_id_seq", true}, {"created_at", "timestamp with time zone", "now()", true}, {"payload", "text", "", true}, {"topic", "text", "", true}}, []string{"id"}},
		typedCatalogTable{f.jobs, "river_queue", f.jobs, "DELETE,INSERT,SELECT,UPDATE", []typedCatalogColumn{{"name", "text", "", true}, {"created_at", "timestamp with time zone", "now()", true}, {"metadata", "jsonb", "'{}'::jsonb", true}, {"paused_at", "timestamp with time zone", "", false}, {"updated_at", "timestamp with time zone", "CURRENT_TIMESTAMP", true}}, []string{"name"}})
	return tables
}

func (f *typedPGFixture) verifyCatalog(ctx context.Context, reader typedCatalogReader) error {
	schemas := []string{f.jobs, f.outbox, f.audit, f.entries[0].token.schema, f.entries[1].token.schema}
	mismatch := func(check string) error { return &typedCatalogMismatch{check} }
	for _, table := range f.catalogTables() {
		rows, err := reader.Query(ctx, `SELECT a.attname,format_type(a.atttypid,a.atttypmod),a.attnotnull,coalesce(pg_get_expr(d.adbin,d.adrelid),''),a.attidentity::text,a.attgenerated::text FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE n.nspname=$1 AND c.relname=$2 AND a.attnum>0 AND NOT a.attisdropped ORDER BY a.attnum`, table.schema, table.name)
		if err != nil {
			return err
		}
		i := 0
		for rows.Next() {
			var name, kind, def, identity, generated string
			var required bool
			if err := rows.Scan(&name, &kind, &required, &def, &identity, &generated); err != nil {
				rows.Close()
				return err
			}
			if i >= len(table.columns) {
				rows.Close()
				return mismatch("column_inventory")
			}
			expected := table.columns[i]
			wantType, wantDefault := expected.kind, expected.def
			if wantType == "@job_state" {
				wantType = typedTable(f.jobs, "river_job_state")
			}
			if wantDefault == "@available" {
				wantDefault = "'available'::" + typedTable(f.jobs, "river_job_state")
			}
			if strings.HasPrefix(wantDefault, "@sequence:") {
				wantDefault = "nextval('" + typedTable(f.jobs, strings.TrimPrefix(wantDefault, "@sequence:")) + "'::regclass)"
			}
			// Identifier quotes vary with generated names; all actual names are trusted
			// random lower-case ASCII. Do not normalize arbitrary executable text.
			if name != expected.name || strings.ReplaceAll(kind, "\"", "") != strings.ReplaceAll(wantType, "\"", "") || required != expected.required || strings.ReplaceAll(def, "\"", "") != strings.ReplaceAll(wantDefault, "\"", "") || identity != "" || generated != "" {
				rows.Close()
				return mismatch("column_type_nullability_default")
			}
			i++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if i != len(table.columns) {
			return mismatch("column_inventory")
		}
		rows, err = reader.Query(ctx, `SELECT coalesce(array_agg(a.attname::text ORDER BY k.position),ARRAY[]::text[]) FROM pg_constraint p JOIN pg_class c ON c.oid=p.conrelid JOIN pg_namespace n ON n.oid=c.relnamespace CROSS JOIN LATERAL unnest(p.conkey) WITH ORDINALITY k(attnum,position) JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum=k.attnum WHERE n.nspname=$1 AND c.relname=$2 AND p.contype='p'`, table.schema, table.name)
		if err != nil {
			return err
		}
		var key []string
		if !rows.Next() {
			rows.Close()
			return mismatch("primary_key")
		}
		err = rows.Scan(&key)
		rows.Close()
		if err != nil {
			return err
		}
		if !slices.Equal(key, table.key) {
			return mismatch("primary_key")
		}
		rows, err = reader.Query(ctx, `SELECT p.polname,p.polcmd::text,p.polpermissive,ARRAY(SELECT r.rolname::text FROM pg_roles r WHERE r.oid=ANY(p.polroles) ORDER BY r.rolname),coalesce(pg_get_expr(p.polqual,p.polrelid),''),coalesce(pg_get_expr(p.polwithcheck,p.polrelid),'') FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname=$2`, table.schema, table.name)
		if err != nil {
			return err
		}
		i = 0
		for rows.Next() {
			var name, command, using, check string
			var permissive bool
			var roles []string
			if err := rows.Scan(&name, &command, &permissive, &roles, &using, &check); err != nil {
				rows.Close()
				return err
			}
			if table.schema == f.jobs {
				// River's tables must carry no policy at all (ADR-045, 2026-11-18).
				rows.Close()
				return mismatch("river_table_has_policy")
			}
			wantName := "provider_scope"
			wantUsing := "((tenant = NULLIF(current_setting('s0typed.tenant'::text, true), ''::text)) AND (scope_id = NULLIF(current_setting('s0typed.scope_id'::text, true), ''::text)))"
			wantCheck := "((tenant = NULLIF(current_setting('s0typed.tenant'::text, true), ''::text)) AND (scope_id = NULLIF(current_setting('s0typed.scope_id'::text, true), ''::text)) AND (writer_xid = pg_current_xact_id()))"
			if name != wantName || command != "*" || !permissive || !slices.Equal(roles, []string{table.role}) || using != wantUsing || check != wantCheck {
				rows.Close()
				return mismatch("policy_expression_role_command")
			}
			i++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		wantPolicies := 1
		if table.schema == f.jobs {
			wantPolicies = 0
		}
		if i != wantPolicies {
			return mismatch("policy_inventory")
		}
		var rls, forced bool
		rows, err = reader.Query(ctx, `SELECT c.relrowsecurity,c.relforcerowsecurity FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname=$2`, table.schema, table.name)
		if err != nil {
			return err
		}
		if !rows.Next() {
			rows.Close()
			return mismatch("rls_flags")
		}
		err = rows.Scan(&rls, &forced)
		rows.Close()
		if err != nil {
			return err
		}
		if rls != (table.schema != f.jobs) || forced != rls {
			return mismatch("rls_flags")
		}
		rows, err = reader.Query(ctx, `SELECT r.rolname,a.privilege_type,a.is_grantable FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace CROSS JOIN LATERAL aclexplode(coalesce(c.relacl,acldefault('r',c.relowner))) a LEFT JOIN pg_roles r ON r.oid=a.grantee WHERE n.nspname=$1 AND c.relname=$2 AND a.grantee<>c.relowner ORDER BY r.rolname,a.privilege_type`, table.schema, table.name)
		if err != nil {
			return err
		}
		var grants []string
		for rows.Next() {
			var role *string
			var privilege string
			var grantable bool
			if err := rows.Scan(&role, &privilege, &grantable); err != nil {
				rows.Close()
				return err
			}
			if role == nil || *role != table.role || grantable {
				rows.Close()
				return mismatch("table_acl_principal")
			}
			grants = append(grants, privilege)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if strings.Join(grants, ",") != table.grants {
			return mismatch("table_acl_privileges")
		}
		rows, err = reader.Query(ctx, `SELECT count(*) FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname=$2 AND a.attacl IS NOT NULL`, table.schema, table.name)
		if err != nil {
			return err
		}
		var columnACLs int
		if !rows.Next() {
			rows.Close()
			return mismatch("column_acl")
		}
		err = rows.Scan(&columnACLs)
		rows.Close()
		if err != nil {
			return err
		}
		if columnACLs != 0 {
			return mismatch("unexpected_column_acl")
		}
	}
	// Exact schema ownership and nonowner USAGE grants; CREATE is never retained.
	for _, schema := range schemas {
		rows, err := reader.Query(ctx, `SELECT owner.rolname,ARRAY(SELECT CASE WHEN a.grantee=0 THEN 'PUBLIC' ELSE gr.rolname::text END||':'||a.privilege_type||':'||a.is_grantable::text FROM aclexplode(coalesce(n.nspacl,acldefault('n',n.nspowner))) a LEFT JOIN pg_roles gr ON gr.oid=a.grantee WHERE a.grantee<>n.nspowner ORDER BY 1) FROM pg_namespace n JOIN pg_roles owner ON owner.oid=n.nspowner WHERE n.nspname=$1`, schema)
		if err != nil {
			return err
		}
		var owner string
		var grants []string
		if !rows.Next() {
			rows.Close()
			return mismatch("schema_inventory")
		}
		err = rows.Scan(&owner, &grants)
		rows.Close()
		if err != nil {
			return err
		}
		want := []string{schema + ":USAGE:false"}
		if schema == f.outbox || schema == f.audit {
			want = append(want, f.entries[0].token.schema+":USAGE:false", f.entries[1].token.schema+":USAGE:false")
			slices.Sort(want)
		}
		if owner != f.owner || !slices.Equal(grants, want) {
			return mismatch("schema_owner_acl")
		}
	}
	rows, err := reader.Query(ctx, `SELECT c.relname,owner.rolname,ARRAY(SELECT CASE WHEN a.grantee=0 THEN 'PUBLIC' ELSE gr.rolname::text END||':'||a.privilege_type||':'||a.is_grantable::text FROM aclexplode(coalesce(c.relacl,acldefault('S',c.relowner))) a LEFT JOIN pg_roles gr ON gr.oid=a.grantee WHERE a.grantee<>c.relowner ORDER BY 1) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_roles owner ON owner.oid=c.relowner WHERE n.nspname=ANY($1) AND c.relkind='S' ORDER BY c.relname`, schemas)
	if err != nil {
		return err
	}
	sequenceNames := []string{}
	for rows.Next() {
		var name, owner string
		var grants []string
		if err := rows.Scan(&name, &owner, &grants); err != nil {
			rows.Close()
			return err
		}
		if owner != f.owner || !slices.Equal(grants, []string{f.jobs + ":SELECT:false", f.jobs + ":USAGE:false"}) {
			rows.Close()
			return mismatch("sequence_owner_acl")
		}
		sequenceNames = append(sequenceNames, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !slices.Equal(sequenceNames, []string{"river_job_id_seq", "river_notification_id_seq"}) {
		return mismatch("sequence_inventory")
	}
	rows, err = reader.Query(ctx, `SELECT count(*) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=ANY($1)`, schemas)
	if err != nil {
		return err
	}
	var triggers int
	if !rows.Next() {
		rows.Close()
		return mismatch("trigger_inventory")
	}
	err = rows.Scan(&triggers)
	rows.Close()
	if err != nil {
		return err
	}
	if triggers != 0 {
		return mismatch("unexpected_trigger")
	}
	for _, spec := range []struct{ schema, name string }{{f.outbox, "outbox"}, {f.audit, "audit"}} {
		rows, err := reader.Query(ctx, `SELECT p.proname,pg_get_function_identity_arguments(p.oid),pg_get_function_result(p.oid),l.lanname,p.prosecdef,p.provolatile::text,p.prokind::text,p.proconfig,p.prosrc,r.rolname,ARRAY(SELECT CASE WHEN a.grantee=0 THEN 'PUBLIC' ELSE gr.rolname::text END||':'||a.privilege_type||':'||a.is_grantable::text FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a LEFT JOIN pg_roles gr ON gr.oid=a.grantee ORDER BY 1) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace JOIN pg_language l ON l.oid=p.prolang JOIN pg_roles r ON r.oid=p.proowner WHERE n.nspname=$1`, spec.schema)
		if err != nil {
			return err
		}
		count := 0
		for rows.Next() {
			var name, args, result, language, volatility, kind, source, owner string
			var definer bool
			var config, acl []string
			if err := rows.Scan(&name, &args, &result, &language, &definer, &volatility, &kind, &config, &source, &owner, &acl); err != nil {
				rows.Close()
				return err
			}
			wantACL := []string{spec.schema + ":EXECUTE:false", f.entries[0].token.schema + ":EXECUTE:false", f.entries[1].token.schema + ":EXECUTE:false"}
			slices.Sort(wantACL)
			if name != "append_scoped" || args != "jsonb" || result != "void" || language != "sql" || !definer || volatility != "v" || kind != "f" || owner != spec.schema || !slices.Equal(config, []string{"search_path=pg_catalog, pg_temp"}) || source != " INSERT INTO "+typedTable(spec.schema, spec.name)+" (body) VALUES ($1) " || !slices.Equal(acl, wantACL) {
				rows.Close()
				return mismatch("append_function_definition_acl")
			}
			count++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if count != 1 {
			return mismatch("append_function_inventory")
		}
	}
	return nil
}

func (f *typedPGFixture) view(t *testing.T, entry int, id, tenant string) (View, Scope) {
	t.Helper()
	s, err := f.host.MintScope(id, tenant)
	typedMust(t, err)
	v, err := f.host.Bind(f.entries[entry], s)
	typedMust(t, err)
	return v, s
}
func typedWantError(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("expected typed refusal %s; received SQLSTATE=%s", want.Error(), typedSQLState(got))
	}
}
func typedFollowup(t *testing.T, ctx context.Context, f *typedPGFixture, v View) {
	t.Helper()
	typedMust(t, f.host.Run(ctx, v, func(tx *Tx) error {
		if err := tx.Put(ctx, "followup", "ok"); err != nil {
			return err
		}
		value, found, err := tx.Get(ctx, "followup")
		if err == nil && (!found || value != "ok") {
			t.Fatal("positive follow-up changed data")
		}
		return err
	}))
}
func (f *typedPGFixture) total(t *testing.T, ctx context.Context) int {
	t.Helper()
	n := 0
	for _, spec := range []struct{ schema, name string }{{f.entries[0].token.schema, "records"}, {f.entries[1].token.schema, "records"}, {f.jobs, "river_job"}, {f.outbox, "outbox"}, {f.audit, "audit"}} {
		var count int
		typedMust(t, f.admin.QueryRow(ctx, "SELECT count(*) FROM "+typedTable(spec.schema, spec.name)).Scan(&count))
		n += count
	}
	return n
}

func TestPGScopeRecords(t *testing.T) {
	typedTrack(t)
	ctx := typedContext(t)
	f := openTypedPG(t, ctx)
	key := "'; SET LOCAL ROLE jobs; --"
	data := `{"scope":{"replace":"tenant-b"},"sql":"SELECT set_config('s0typed.tenant','tenant-b',true)"}`
	type route struct {
		v     View
		scope Scope
		entry int
		value string
	}
	var routes []route
	for entry := 0; entry < 2; entry++ {
		for tenant := 0; tenant < 2; tenant++ {
			v, s := f.view(t, entry, fmt.Sprintf("scope-%d", tenant), fmt.Sprintf("tenant-%c", 'a'+tenant))
			routes = append(routes, route{v, s, entry, fmt.Sprintf("entry-%d/tenant-%d/%s", entry, tenant, data)})
		}
	}
	// Same tenant, different scope ID must not alias the first scope.
	v, s := f.view(t, 0, "scope-other", "tenant-a")
	routes = append(routes, route{v, s, 0, "different-scope/" + data})
	for _, r := range routes {
		typedMust(t, f.host.Run(ctx, r.v, func(tx *Tx) error {
			_, found, err := tx.Get(ctx, key)
			if err != nil {
				return err
			}
			if found {
				t.Fatal("unwritten route saw another entry/scope")
			}
			return tx.Put(ctx, key, r.value)
		}))
	}
	for _, r := range routes {
		typedMust(t, f.host.Run(ctx, r.v, func(tx *Tx) error {
			value, found, err := tx.Get(ctx, key)
			if err == nil && (!found || value != r.value) {
				t.Fatal("closed operation leaked or altered SQL-looking data")
			}
			return err
		}))
		var count int
		typedMust(t, f.admin.QueryRow(ctx, "SELECT count(*) FROM "+typedTable(f.entries[r.entry].token.schema, "records")+" WHERE tenant=$1 AND scope_id=$2 AND key=$3 AND value=$4", r.scope.token.tenant, r.scope.token.id, key, r.value).Scan(&count))
		if count != 1 {
			t.Fatal("observer did not find exact own row")
		}
	}
	if f.total(t, ctx) != 5 {
		t.Fatal("unexpected scope-record cardinality")
	}
	// Host issuance refusals are tested with no SQL fallback, then a live control.
	foreign, err := NewHost(f)
	typedMust(t, err)
	foreignScope, err := foreign.MintScope("scope-0", "tenant-a")
	typedMust(t, err)
	for _, tc := range []struct {
		e    Entry
		s    Scope
		want error
	}{{Entry{}, routes[0].scope, ErrInvalid}, {f.entries[0], Scope{}, ErrInvalid}, {f.entries[0], foreignScope, ErrForeign}} {
		before := f.counts
		_, err := f.host.Bind(tc.e, tc.s)
		typedWantError(t, err, tc.want)
		if f.counts != before || f.total(t, ctx) != 5 {
			t.Fatal("refused issuance changed provider or rows")
		}
		typedMust(t, f.host.Run(ctx, routes[0].v, func(tx *Tx) error {
			value, found, err := tx.Get(ctx, key)
			if err == nil && (!found || value != routes[0].value) {
				t.Fatal("follow-up own record missing")
			}
			return err
		}))
	}
}

func TestPGAllParticipantsCommit(t *testing.T) {
	typedTrack(t)
	ctx := typedContext(t)
	f := openTypedPG(t, ctx)
	for i, tenant := range []string{"tenant-a", "tenant-b"} {
		v, s := f.view(t, i, "commit-"+tenant, tenant)
		typedMust(t, f.host.run(ctx, v, func(tx *Tx) error { return tx.Put(ctx, "commit", "ordinary state") }, func(native transactionBackend) error {
			p, ok := native.(*typedPGTx)
			if !ok {
				return ErrInvalid
			}
			for boundary := 1; boundary <= 3; boundary++ {
				if err := p.participant(ctx, boundary); err != nil {
					return err
				}
			}
			var role string
			typedMust(t, p.tx.QueryRow(ctx, "SELECT current_user").Scan(&role))
			if role != p.entry.role {
				t.Fatal("private participant failed to restore entry role")
			}
			if got := f.total(t, ctx); got != i*4 {
				t.Fatalf("precommit observer saw writes: total=%d", got)
			}
			return nil
		}))
		var xid string
		for participant, spec := range []struct{ schema, name string }{{f.entries[i].token.schema, "records"}, {f.jobs, "river_job"}, {f.outbox, "outbox"}, {f.audit, "audit"}} {
			var count int
			var writer string
			typedMust(t, f.admin.QueryRow(ctx, "SELECT count(*),coalesce(min(writer_xid::text),'') FROM "+typedTable(spec.schema, spec.name)+" WHERE tenant=$1 AND scope_id=$2", s.token.tenant, s.token.id).Scan(&count, &writer))
			if count != 1 || writer == "" {
				t.Fatal("participant did not commit exactly one scoped row")
			}
			if participant == 0 {
				xid = writer
			} else if writer != xid {
				t.Fatal("participants used different real writer XIDs")
			}
		}
		var malicious bool
		typedMust(t, f.admin.QueryRow(ctx, "SELECT args->>'tenant'='malicious-payload-tenant' AND args->>'scope'='malicious-payload-scope' AND tenant=$1 AND scope_id=$2 FROM "+typedTable(f.jobs, "river_job")+" WHERE tenant=$1 AND scope_id=$2", tenant, s.token.id).Scan(&malicious))
		if !malicious {
			t.Fatal("job payload improperly selected provider identity")
		}
	}
	if f.total(t, ctx) != 8 {
		t.Fatal("all-four commit cardinality mismatch")
	}
}

func TestPGRollbackAfterEachBoundary(t *testing.T) {
	typedTrack(t)
	ctx := typedContext(t)
	f := openTypedPG(t, ctx)
	failure := errors.New("fixture requested rollback")
	for boundary, label := range []string{"state", "river", "outbox", "audit"} {
		t.Run(label, func(t *testing.T) {
			v, _ := f.view(t, 0, "rollback-"+label, "tenant-a")
			before := f.counts
			err := f.host.run(ctx, v, func(tx *Tx) error {
				if err := tx.Put(ctx, "rollback", "pending"); err != nil {
					return err
				}
				if boundary == 0 {
					return failure
				}
				return nil
			}, func(native transactionBackend) error {
				p, ok := native.(*typedPGTx)
				if !ok {
					return ErrInvalid
				}
				for i := 1; i <= boundary; i++ {
					if err := p.participant(ctx, i); err != nil {
						return err
					}
				}
				if f.total(t, ctx) != 0 {
					t.Fatal("rollback participant became visible before commit")
				}
				return failure
			})
			typedWantError(t, err, failure)
			if f.total(t, ctx) != 0 || f.counts.commits != before.commits || f.counts.rollbacks != before.rollbacks+1 {
				t.Fatal("failed boundary retained writes or attempted commit")
			}
			// Positive live operation also rolls back, preserving the zero-row check.
			err = f.host.Run(ctx, v, func(tx *Tx) error {
				if err := tx.Put(ctx, "control", "ok"); err != nil {
					return err
				}
				value, found, err := tx.Get(ctx, "control")
				if err == nil && (!found || value != "ok") {
					t.Fatal("rollback follow-up failed")
				}
				if err != nil {
					return err
				}
				return failure
			})
			typedWantError(t, err, failure)
			if f.total(t, ctx) != 0 {
				t.Fatal("positive rollback control persisted")
			}
		})
	}
	good, _ := f.view(t, 1, "rollback-final-positive", "tenant-b")
	typedFollowup(t, ctx, f, good)
	if f.total(t, ctx) != 1 {
		t.Fatal("final committed recovery control missing")
	}
}

func TestPGLifetimeAndCatalogRefusals(t *testing.T) {
	typedTrack(t)
	ctx := typedContext(t)
	f := openTypedPG(t, ctx)
	v, s := f.view(t, 0, "lifetime", "tenant-a")
	good, _ := f.view(t, 1, "lifetime-good", "tenant-b")
	var old *Tx
	typedMust(t, f.host.Run(ctx, v, func(tx *Tx) error { old = tx; return tx.Put(ctx, "original", "ok") }))
	typedMust(t, f.host.Run(ctx, good, func(tx *Tx) error {
		before := f.counts
		typedWantError(t, old.Put(ctx, "reuse", "bad"), ErrInactive)
		_, _, err := old.Get(ctx, "original")
		typedWantError(t, err, ErrInactive)
		if f.counts != before {
			t.Fatal("completed Tx reached provider during a fresh transaction")
		}
		return tx.Put(ctx, "reuse-control", "ok")
	}))
	checkRefusal := func(v View, want error) {
		t.Helper()
		before := f.counts
		rows := f.total(t, ctx)
		typedWantError(t, f.host.Run(ctx, v, func(*Tx) error { t.Fatal("refused callback ran"); return nil }), want)
		if f.counts != before || f.total(t, ctx) != rows {
			t.Fatal("refusal opened provider or changed rows")
		}
		typedFollowup(t, ctx, f, good)
	}
	// Trusted same-package fault injection, not a capability the caller possesses:
	// retain token identity but change its declared schema against the host catalog.
	original := v.token.binding
	v.token.binding.schema = f.entries[1].token.schema
	checkRefusal(v, ErrCatalog)
	v.token.binding = original
	typedFollowup(t, ctx, f, v)
	typedMust(t, f.host.Dispose(v))
	checkRefusal(v, ErrInactive)
	v, err := f.host.Bind(f.entries[0], s)
	typedMust(t, err)
	typedMust(t, f.host.RevokeScope(s))
	checkRefusal(v, ErrInactive)
	v, _ = f.view(t, 0, "entry-revoked", "tenant-a")
	typedMust(t, f.host.RevokeEntry(f.entries[0]))
	checkRefusal(v, ErrInactive)
}

// Observe the sole pooled connection only after failed begin has released it.
// Protocol TxStatus 'I' is the ReadyForQuery idle state, not merely no visible rows.
func (f *typedPGFixture) idleRuntime(t *testing.T, ctx context.Context, wantPID uint32) uint32 {
	t.Helper()
	conn, err := f.runtime.Acquire(ctx)
	typedMust(t, err)
	defer conn.Release()
	native := conn.Conn().PgConn()
	pid := native.PID()
	if native.TxStatus() != 'I' || wantPID != 0 && pid != wantPID {
		t.Fatal("runtime connection not reused in ReadyForQuery idle state")
	}
	var user, tenant, scope string
	typedMust(t, conn.QueryRow(ctx, "SELECT current_user,coalesce(current_setting('s0typed.tenant',true),''),coalesce(current_setting('s0typed.scope_id',true),'')").Scan(&user, &tenant, &scope))
	if user != f.login || tenant != "" || scope != "" || native.TxStatus() != 'I' {
		t.Fatal("failed setup retained role/scope or an open transaction")
	}
	return pid
}

func TestPGCatalogChecks(t *testing.T) {
	typedTrack(t)
	ctx := typedContext(t)
	f := openTypedPG(t, ctx)
	typedMust(t, f.verifyCatalog(ctx, f.admin))
	before := f.counts
	// Trusted bootstrap-only metadata fault inside an uncommitted transaction;
	// no caller operation accepts policy text, and no poisoned view is admitted.
	tx, err := f.admin.Begin(ctx)
	typedMust(t, err)
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := tx.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			typedMust(t, err)
		}
	}()
	_, err = tx.Exec(ctx, "ALTER POLICY provider_scope ON "+typedTable(f.entries[0].token.schema, "records")+" USING (false)")
	typedMust(t, err)
	err = f.verifyCatalog(ctx, tx)
	var mismatch *typedCatalogMismatch
	if !errors.Is(err, ErrCatalog) || !errors.As(err, &mismatch) || mismatch.check != "policy_expression_role_command" {
		t.Fatalf("trusted catalog mutation was not rejected: SQLSTATE=%s", typedSQLState(err))
	}
	if f.counts != before {
		t.Fatal("catalog inspection reached runtime provider")
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	typedMust(t, tx.Rollback(cleanup))
	cancel()
	typedMust(t, f.verifyCatalog(ctx, f.admin))
	// ADR-045 (2026-11-18): each way of putting RLS or a caller privilege back on
	// River's tables is a catalog mismatch, found before any view is admitted.
	entry := typedIdent(f.entries[0].token.schema)
	job := typedTable(f.jobs, "river_job")
	for _, fault := range []struct{ sql, check string }{
		{"ALTER TABLE " + job + " ENABLE ROW LEVEL SECURITY", "rls_flags"},
		{"ALTER TABLE " + job + " ENABLE ROW LEVEL SECURITY, FORCE ROW LEVEL SECURITY", "rls_flags"},
		{"CREATE POLICY stray ON " + job + " USING (true)", "river_table_has_policy"},
		{"GRANT SELECT ON " + job + " TO " + entry, "table_acl_principal"},
		{"GRANT SELECT ON " + typedTable(f.jobs, "river_queue") + " TO PUBLIC", "table_acl_principal"},
		{"GRANT USAGE ON SCHEMA " + typedIdent(f.jobs) + " TO " + entry, "schema_owner_acl"},
	} {
		ftx, err := f.admin.Begin(ctx)
		typedMust(t, err)
		_, err = ftx.Exec(ctx, fault.sql)
		typedMust(t, err)
		err = f.verifyCatalog(ctx, ftx)
		fclean, fcancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		typedMust(t, ftx.Rollback(fclean))
		fcancel()
		if !errors.Is(err, ErrCatalog) || !errors.As(err, &mismatch) || mismatch.check != fault.check {
			t.Fatalf("river fault %q not rejected as %s: got %v", fault.sql, fault.check, err)
		}
	}
	typedMust(t, f.verifyCatalog(ctx, f.admin))
	v, _ := f.view(t, 0, "catalog-followup", "tenant-a")
	typedFollowup(t, ctx, f, v)
	if f.total(t, ctx) != 1 {
		t.Fatal("catalog recovery own-row control missing")
	}
}

func TestPGBeginCleanupAndReuse(t *testing.T) {
	typedTrack(t)
	ctx := typedContext(t)
	f := openTypedPG(t, ctx)
	v, _ := f.view(t, 0, "setup-cleanup", "tenant-a")
	pid := f.idleRuntime(t, ctx, 0)
	for _, tc := range []struct {
		name  string
		fault typedSetupFault
		state string
	}{
		{"sql-after-role", typedSetupSQLAfterRole, "22012"},
		{"cancel-after-scope", typedSetupCancelAfterScope, "context_canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, rows := f.counts, f.total(t, ctx)
			f.setupFault = tc.fault
			err := f.host.Run(ctx, v, func(*Tx) error { t.Fatal("failed begin admitted domain callback"); return nil })
			f.setupFault = typedSetupOK
			if typedSQLState(err) != tc.state {
				t.Fatalf("begin failure SQLSTATE=%s; expected %s", typedSQLState(err), tc.state)
			}
			if tc.fault == typedSetupCancelAfterScope && !errors.Is(err, context.Canceled) {
				t.Fatal("primary cancellation cause not retained")
			}
			if f.counts.setupRollbacks != before.setupRollbacks+1 || f.counts.commits != before.commits || f.counts.writes != before.writes || f.total(t, ctx) != rows {
				t.Fatal("failed begin did not clean up without domain writes")
			}
			pid = f.idleRuntime(t, ctx, pid)
			typedFollowup(t, ctx, f, v)
			pid = f.idleRuntime(t, ctx, pid)
			if f.total(t, ctx) != 1 {
				t.Fatal("positive setup-cleanup follow-up missing or duplicated")
			}
		})
	}
}

// TestPGEntryRolesHaveNoRiverPrivileges asserts the guarantee that replaced
// row-level security on River's tables (ADR-045, 2026-11-18): an entry (caller)
// role holds no privilege on any River table or on the River schema, so SELECT,
// INSERT, UPDATE and DELETE are each refused with insufficient_privilege, while
// the trusted jobs role (positive control) can read the same tables. Together
// with TestPGScopeRecords, which refuses zero and foreign scopes through the
// typed surface, a caller reaches no other tenant's job row. The statements here
// are fixture-level SQL under the entry role, not an operation of the Tx surface.
func TestPGEntryRolesHaveNoRiverPrivileges(t *testing.T) {
	typedTrack(t)
	ctx := typedContext(t)
	f := openTypedPG(t, ctx)
	column := map[string]string{"river_job": "priority", "river_leader": "name", "river_migration": "version", "river_notification": "topic", "river_queue": "name"}
	refused := func(role, statement string) string {
		tx, err := f.runtime.Begin(ctx)
		typedMust(t, err)
		defer func() {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = tx.Rollback(cleanup)
		}()
		_, err = tx.Exec(ctx, "SET LOCAL ROLE "+typedIdent(role))
		typedMust(t, err)
		_, err = tx.Exec(ctx, statement)
		return typedSQLState(err)
	}
	for _, name := range typedRiverTables {
		q := typedTable(f.jobs, name)
		if got := refused(f.jobs, "SELECT count(*) FROM "+q); got != "" {
			t.Fatalf("positive control: jobs role cannot read %s: SQLSTATE=%s", name, got)
		}
		for _, e := range f.entries {
			role := e.token.schema
			for op, statement := range map[string]string{
				"select": "SELECT count(*) FROM " + q,
				"insert": "INSERT INTO " + q + " DEFAULT VALUES",
				"update": "UPDATE " + q + " SET " + column[name] + "=" + column[name],
				"delete": "DELETE FROM " + q,
			} {
				if got := refused(role, statement); got != "42501" {
					t.Fatalf("entry role %s on %s not refused with insufficient_privilege: SQLSTATE=%s", op, name, got)
				}
			}
		}
	}
}
