# S0 transaction atomicity diagnostic

**Status: ordinary functional atomicity run recorded; SQL authority remains FAIL.**
The actual race-instrumented run passed 21 selected cases; see
[the retained record](../../../../test/spike/s0/results/20261112T174224Z-ordinary-atomicity.json).
This is not SQL-authority, worker, performance or full S0 acceptance.
This is an independent, ordinary-functional diagnostic for the atomicity portion
of [frozen §5](../../../../docs/design/s0-spike-criteria.md#5-transaction-and-database-role-probe-adr-045),
using the [frozen reference pins](../../../../docs/design/s0-reference-pins.md).
It is **not** a denied-capability hypothesis, an SQL-authority mechanism, a fix
for the SQL boundary, full S0 acceptance, or an accepted S3 schema/API.
The original **SQL gate FAIL remains unchanged**.

## What is authored

- [Fixture](fixture_test.go): explicit `S0_ADMIN_DSN` and matching
  `S0_EXPECT_DATABASE` checks; new `s0tx_<random>_` names on every invocation.
  No fixed schema, existing runtime credential, private configuration, or
  container inspection. The generated runtime password is never persisted.
- Separate non-login table/migration owner with no superuser/BYPASSRLS; fresh
  nonowner login and caller/jobs/events/audit roles with `NOINHERIT`,
  `NOSUPERUSER`, `NOBYPASSRLS`, no role/database creation or replication.
  The state, outbox and audit tables use forced RLS. River's `river_job` has **no
  RLS and no policy** and only the jobs role holds privileges on it; the caller
  role has none (ADR-045, 2026-11-18), checked live. Live checks verify these facts plus actual
  `fsync=on`, `synchronous_commit=on` and narrow definer-function ACL/search path.
- The runtime login is a member of caller and jobs with `INHERIT FALSE,
  SET TRUE`: **exactly the original known-failed role-switch mechanism**.
  Custom tenant/scope settings remain caller-tamperable. These are explicit
  unsafe limits, not SQL-isolation proof. No tamper/elevation attack is run.
- `rivermigrate.New(riverpgxv5.New(migrationPool), Config{Schema: freshJobs})`
  executes River's packaged migrations under the separate owner. No upstream
  migration/query SQL is copied and no upstream source is modified.
- Experimental provider `tenant`, `scope_id`, `writer_xid` columns on the real
  migrated `river_job` table are default-stamped from transaction settings and
  `pg_current_xact_id()`, not job arguments. Stock River has no tenant API and
  does not return these extra columns in its job scan.
- One caller-owned `pgx.Tx` writes state, calls unmodified River `InsertTx`,
  invokes narrow outbox/audit `SECURITY DEFINER` functions, then commits or
  rolls back. Definers use fixed `pg_catalog,pg_temp`, fully qualified tables,
  confined per-owner roles, and no PUBLIC execution. Role-restore failure is
  retained and cannot disguise an aborted transaction as success.
- [Tests](atomicity_test.go): commit in two scopes with malicious payload
  identity; precommit invisibility; common identity/transaction stamps;
  rollback and SQL error at boundaries 0–4; canceled operation followed by owner
  rollback; own physical-connection close before commit; no-handle zero reads
  and rejected writes; caller direct outbox/audit write rejection; pool state
  reuse; identifier/error and privilege-predicate unit cases.

No worker is started. Background identity delivery, fetch/completion/rescue,
uniqueness, leader behavior, unsafe SQL attack corpus, performance and cost
control are **not covered**. Cancellation is pre-canceled operation + explicit
rollback, not in-flight interruption. Lost commit acknowledgements are not
covered; connection loss is before commit only.

## Commands for the parent, not an execution receipt

Compile without running any test:

```sh
go test -mod=readonly -c -o /tmp/s0txprobe.test ./plugins/jobs-river/internal/s0txprobe
```

Only after separate owner authorization and licence/package preflight, with an
explicit dedicated disposable database in the environment:

```sh
go test -mod=readonly -count=1 -v ./plugins/jobs-river/internal/s0txprobe
```

Without `S0_ADMIN_DSN`, the integration test skips and reports **NOT measured**;
Go's overall success on a skip is not an atomicity result. The authoring subtask
compiled only; the parent later verified the exact package licences, ran the unit
helpers, and executed the ordinary integration cases on the owned fixture. The
record identifies the actual executable/source and cases; it does not rewrite
the failed authority result.

[Report source](report_test.go) emits one `S0TX_REPORT` JSON line on orderly
`TestMain` return: exit code, per-case pass/fail/not-selected state, classification,
retained fixture ID, durability settings, Go version, hashes of the three owned
source files and executable, and explicit limits. It records a canonical command,
**not** an actual argv capture. It serializes no DSN/password/database/host names,
server error details or absolute paths. Capture the real invocation/exit outside
this package if needed. Panic/kill/crash may prevent report emission. Source
hashes identify on-disk files at reporting time, not attested build provenance.

## Retention and review

Only connections are automatically closed; no schema, role or database is
automatically dropped. The retained fixture ID is emitted before DDL, including
partial bootstrap, for parent-owned inspection and explicit manual cleanup.
Do not commit runtime logs or real credential/endpoint paths. Re-run compile and
review the exact package closure whenever imports/pins change; the package SBOM
is compile-time licence metadata, not a successful runtime measurement.
