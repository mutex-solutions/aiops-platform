# S0 closed typed-operation PostgreSQL diagnostic

**Experimental, owner-approved investigation; not adopted.** The approved
[proposal](../../../../docs/design/s0-sql-typed-operations-proposal.md) and
[preexecution protocol](../../../../docs/design/s0-typed-operations-protocol.md)
retain the original guarantees and failing raw-SQL authority history. Source-only
or pure-Go results are not PostgreSQL evidence. This package does not run workers,
performance/restart arms, final authority gates or S1.

## Boundary being investigated

A domain callback receives only `*Tx` with `Put(ctx, key, value)` and
`Get(ctx, key)`. Its opaque `View` has no methods. Neither exposes SQL text,
identifiers, functions, roles, settings, a driver, a pool or the trusted `Host`
issuer. Trusted registration installs a fixed record catalog for each entry;
trusted issuance mints scope identity. Display names and job/request payloads do
not mint authority. Keys and values, including SQL-looking strings and JSON with
`scope.replace`, stay parameters. Records are keyed by `(tenant, scope_id, key)`;
read SQL and RLS both constrain the exact tenant **and** scope ID.

This is a changed API boundary between trusted in-process components, not a
PostgreSQL-session authentication primitive or sandbox against arbitrary native
Go, unsafe/reflection, stolen issuer references or separately acquired raw SQL.
The runtime login still has known-unsafe SET-role memberships and custom GUCs
remain mutable through raw SQL. The original raw-SQL hole is unchanged and is
not re-run or relabelled as a pass. Scope tokens use host-owned opaque identity;
this minimal spike does not introduce a portable cryptographic token protocol.

`pg_test.go` implements the private `backend` as `typedPGFixture.begin` and the
private transaction as `typedPGTx`. Only trusted same-package code recovers that
adapter. A private participant invokes **unmodified River 0.51.2 `InsertTx` on the
same actual `pgx.Tx`**, restores the entry role, then invokes fixed narrow
provider-owned SECURITY DEFINER append functions for outbox/audit. Domain code
never receives this closure, `Host`, or native transaction.

The fixture creates two independent entry schemas/roles and separate migration
ownership. Its runtime login and runtime roles are NOSUPERUSER, NOBYPASSRLS,
NOINHERIT nonowners. Record grants target only each entry's own schema/table.
Every table is FORCE RLS: two records tables, two provider tables and the exact
five final River main tables (`river_job`, `river_leader`, `river_migration`,
`river_notification`, `river_queue`). Trusted jobs policies intentionally see all
rows; this is provider authority, not tenant-specific background-worker evidence.
Only the fixture-owned `river_job` receives extra tenant/scope/writer-xid columns.
Actual upstream migrations run through `rivermigrate.Config.Schema`; no copied or
modified upstream SQL is executed. Migration 007 removes its own transient client
tables in the newly created fixture schema. The fixture authors **no DROP** and
performs no cleanup against any existing schema; the entire server is disposable.

## Authored PostgreSQL cases

- `TestPGScopeRecords`: two entries, two tenants, the same key, and an additional
  same-tenant/different-scope case. Reads never alias another route; SQL-looking
  key/value bytes are preserved and observer counts match exact rows. Zero and
  foreign issuance fail with typed sentinels, no provider call, and positive live
  own-record follow-ups.
- `TestPGAllParticipantsCommit`: all four participants commit once per tenant;
  observer reads see no uncommitted rows, each participant has exactly one row and
  the same writer XID, and malicious job-argument identity remains payload while
  provider columns reflect the issued token. The entry role is restored.
- `TestPGRollbackAfterEachBoundary`: return the exact fixture error after state,
  River, outbox, then audit; each transaction rolls back all writes. A successful
  Put/Get control itself rolls back after each case, preserving the zero-row
  check; the final positive control commits on the other entry.
- `TestPGLifetimeAndCatalogRefusals`: a completed Tx cannot be reused even while
  another transaction is active. Trusted-package malformed schema metadata,
  disposed views, revoked scopes and revoked entries fail before backend work,
  leave live row counts unchanged and have positive controls on a valid view.
  The metadata mutation is explicitly a trusted fault injection, not lower-trust
  caller power. API shape/unit cases cover unsupported/raw SQL surface absence.

## Bounded correctness follow-up (authored; parent validation required)

The [correctness follow-up](../../../../docs/design/s0-typed-operations-correctness-followup.md)
precedes these additional checks. Private begin/setup failure now rolls back with
a fresh five-second context independent of canceled work. It returns the primary
error joined with any unexpected cleanup error; already-closed `pgx.ErrTxClosed`
is normalized, not described as confirmed rollback. No raw driver or fault hook
is added to the exported domain API.

- `TestPGBeginCleanupAndReuse`: a private enum chooses a fixed SQL division error
  after role setup (exact SQLSTATE `22012`), or deterministic cancellation after
  real role/scope setup (`errors.Is(context.Canceled)`). The callback must not run.
  Each case checks one setup-cleanup attempt and unchanged rows, then reacquires
  the **same physical backend PID** from the single-connection runtime pool,
  verifies ReadyForQuery idle (`I`), runtime login and cleared scope settings,
  and successfully commits an own-entry Put/Get recovery control. This tests
  provider setup cleanup, not a caller SQL escape or a server-stall cancellation
  timing guarantee. Unexpected rollback failure cannot be safely forced here;
  its observable error-joining path remains a separate unit/control concern.
- `TestPGCatalogChecks`: reviewed fixed columns/types/nullability/defaults and
  primary keys, one exact role/expression policy per table, nonowner table grants,
  schema/sequence ownership and grants, exact narrow append-function definitions
  and ACLs, and zero triggers on the nine owned tables are checked before the
  fixture admits a view. A trusted bootstrap transaction changes one record
  policy to `USING (false)`, expects structured `ErrCatalog`, rolls it back,
  verifies the restored catalog and commits a positive own-row control. Caller
  code cannot choose this policy text or perform DDL.

Catalog verification is fixed-fixture startup preflight, not a production
migration/version admission or concurrent-DDL invalidation protocol. The pinned
River migrations remain unmodified; no arbitrary caller catalog is supported.
API commit/revocation arbitration and trusted quiescence waits are a separately
owned part of the bounded follow-up; the PG cases here do not claim to prove their
concurrent ordering or server-side unforgeability. Provider DDL admission,
production joins/migrations/query grammar and canonical S3 adoption remain open.
Original raw-SQL gate failure and first source-identity failure remain history.

## Parent-only execution contract

The author of these files did not execute Bash, builds, tests, PostgreSQL or a
container. The parent reviews all SQL and independently records exact invocation,
source/protocol/module/license closure and executable hashes before execution.
Only then may it build offline and execute in its approved OS sandbox:

- Fresh pinned official PostgreSQL 16.15 image/server, network namespace `none`,
  read-only root/source/toolchain, no host sockets or shared services; loopback
  only. The Go test process never provisions a container/server or spawns a child.
- Empty explicit environment; scratch-local HOME, TMPDIR and database/socket
  directories. Source mounted read-only at its **actual build source path** (for
  `runtime.Caller` identity), all other writable state under `/scratch`.
- Parent plan: 2 CPUs, 1 GiB memory, 128 PIDs, 768 MiB scratch tmpfs; explicit
  nofile/file-size/CPU/wall-time bounds as separately recorded. Per-case Go
  context is 30 seconds, not a frozen latency/recovery acceptance threshold.
- Separate offline build sandbox: read-only Go 1.26.8 toolchain, this exact source
  closure and locally pinned modules; CGO disabled, no fetch/install/upgrade.
- Parent starts new `initdb`/server itself with local-only dummy trust auth and
  `fsync=on`, `synchronous_commit=on`. Bootstrap is `postgres`; migration sessions
  switch to a separate fresh confined owner; runtime sessions use fresh generated
  role credentials. Bootstrap only provisions and observes, never participates in
  measured state/River/outbox/audit writes.

The only application environment inputs are exact safe fixture values:

```text
S0_TYPED_DSN=postgres://postgres@127.0.0.1:5432/s0typed?sslmode=disable
S0_TYPED_EXPECT_DATABASE=s0typed
```

The DSN must match exactly; missing DSN causes **SKIP, PostgreSQL NOT measured**.
This does not authorize connecting to any host/private/shared database. Ambient
PG credentials, services and configuration are not discovered or copied. The
parent launches from `env -i`; no ambient environment capture is appropriate.
The canonical package selection is
`go test -mod=readonly -count=1 -v ./plugins/jobs-river/internal/s0typedops`,
but this line is documentation, not a claim that it ran or an invocation capture.
Build output and a prebuilt test executable can instead be used in the parent
sandbox; exclude unrelated packages and excluded capability drafts entirely.

`TestMain` emits one `S0TYPEDOPS_REPORT` JSON line with per-PG-case PASS/FAIL/SKIP,
before/after hashes for the actual read-only source/approved protocol/module and
license files, and the actual executable hash. A hash is identity, not build
attestation. No DSN, credentials, raw database error, environment dump or server
error detail is emitted; database failures log only SQLSTATE. Reports require
orderly `TestMain` return and parent collection for actual resource/teardown data.
No automatic DROP occurs: pools close, then parent destroys the entire disposable
container after collection and independently records remaining resources.

Licenses and exact source routes are in `NOTICE`; copied license text is unchanged.
Module pins remain pgx 5.11.0 and River 0.51.2. This package does not import any
production platform package or alter the upstream source/license.
