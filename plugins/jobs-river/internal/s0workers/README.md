# S0 ordinary-functional worker scope-data diagnostic

**DIAGNOSTIC ONLY. Auth/SQL gate remains FAIL, unchanged. No functional test has
been executed by the authoring child.** River v0.51.2 and pgx v5.11.0 are the
exact existing module pins. All code here is `_test.go`, under the plugin path;
no SDK/kernel/company imports or production implementation is introduced.

This follows the already approved full S0 work but deliberately retains the
**known-unsafe** original role-membership/custom-GUC fixture. It is not an
authority repair or a boundary acceptance test. A nonowner NOSUPERUSER NOBYPASSRLS
runtime login can SET the caller and trusted jobs roles; writable GUCs are just
scope data. The jobs role is explicitly trusted to access **all tenants**. Its
worker identity does not change frontend RLS or make frontend SQL safe.

## Authored cases (not yet measured)

`TestWorkerScopeDataDiagnostic` contains:

1. `two_scopes_unique_redelivery`: two transaction scopes, malicious args tenant,
   distinct tagged idempotency IDs, sequential `InsertTx` + `UniqueOpts{ByArgs:true}`
   duplicates returning the same job ID, actual background deliveries and
   completion events. Provider extra columns stamp from transaction settings;
   the worker looks them up by job ID via its own jobs pool and passes a local
   opaque `callScope` in context. The callback asserts its result-writing caller
   transaction uses that scope, never the args tenant. An explicit `JobRetry`
   re-delivers through the real worker; `ON CONFLICT(job_id) DO NOTHING RETURNING`
   provides one logical result per job ID. This is not crash/ambiguous-commit or
   cross-job exactly-once proof and not cross-tenant uniqueness proof.
2. `nil_handler_snoozes_then_activates`: queue before Start with an empty atomic
   handler registry; observe an actual missing-handler delivery and River's
   `EventKindJobSnoozed`, then query nonfinal state/no result. Activate the real
   callback without re-registering the Kind and await completion + scoped result.
3. `concurrent_replica_a_unloaded_b_active`: A demonstrably snoozes while unloaded;
   B starts with an active handler while A stays running and eligible for this
   same Kind. Each client has Queues+Workers and MaxWorkers=1 in the same schema.
   A uses a 50ms snooze and a 250ms River fetch cooldown/poll cadence; B uses
   1ms/5ms fetch cadence and must deliver and complete within a 10s channel budget.
   That intentional cadence avoids A monopolizing every due-time fetch; no test
   thread sleeps or client stopping is used to force a particular fetch winner.
   This is two **in-process** replicas, not a multi-process test or a scheduler
   fairness guarantee. It is not Stop(A)/Start(B) sequential handoff.
4. `revoke_during_work_cancels_drains_then_reactivates`: an entered plugin callback
   waits on its cooperative child context. Revocation atomically removes its
   view, cancels and drains admitted calls with a bounded budget; canceled work
   snoozes rather than reporting success. A subsequent actual fetch sees nil;
   later activation completes the retained job. Noncooperative Go code is not
   forcibly stopped, and a drain timeout is not revocation completion.

The stable `WorkerDefaults[workerArgs]` dispatcher is registered once per client
with `AddWorker`; unload/reload changes a typed `atomic.Pointer[handlerView]`,
not River's registry. There is no lifecycle `Sleep` polling. Tests await bounded
channels/events and query state at causal barriers. River's own short poll
fallback remains enabled; LISTEN-only wakeup behavior is not isolated.

## Fixture and exact background policy coverage

Each selected subtest creates fresh `s0wk_<random>_*` schemas and roles. Owner,
bootstrap pool, login, caller role, and jobs role are separate. Unmodified
`rivermigrate.New(..., Config{Schema: jobsSchema})` performs migrations on a
separate owner session. There is no copied upstream migration SQL.

The final unmodified PostgreSQL main migration schema has **five tables**:
`river_job`, `river_leader`, `river_migration`, `river_notification`, `river_queue`.
Migration 005 creates `river_client` and `river_client_queue`, but migration 007
**drops both**. Migration 008 is a PostgreSQL no-op. Before extension DDL, the
fixture requires an exact catalog-name match to those five, not an assumption
based on every historical CREATE. Missing/unexpected tables fail closed; there
is no conditional table skip or recreation of removed upstream tables.

All five migrated tables and the result table have ENABLE+FORCE RLS, with a
separate NOLOGIN owner. The explicit `TO jobs USING(true) WITH CHECK(true)` policy
and SELECT/INSERT/UPDATE/DELETE grants cover `river_job`, `river_leader`,
`river_queue`, `river_notification`. `river_migration` has the same policy but
SELECT-only runtime grant. Jobs gets sequence USAGE/SELECT. Runtime has no schema
CREATE or migration/owner membership. Post-setup verification requires the exact
five River names plus exactly `result` in the data schema, separate owner and
FORCE RLS on all six tables; count alone cannot satisfy that verification.

This intentionally enables all-row fetch, completion, snooze metadata, unique
conflict metadata, leader election/resignation, rescue/cleanup/scheduler scans,
queue metadata and notification-outbox paths. **Granting policies is not
proof each path executed.** Only actual handler fetch, snooze, completion,
sequential uniqueness and explicit redelivery are asserted. No distinct leader,
rescue, scheduled-job, cleanup, metadata isolation or unique-race acceptance is
claimed. Frontend caller SELECT on `river_job` and SELECT/INSERT on `result` are
still tenant-GUC-filtered (known unsafe to raw SQL). Jobs has no result grants.
The result writer sets LOCAL caller role and LOCAL scope in a fresh transaction.

Logs use `slog` to `io.Discard`; SQL errors are classified by SQLSTATE, not raw
`Error()` strings. Runtime tracer is unset. DSNs, generated passwords, hosts,
database names, os.Args, and raw server details are not serialized. Names use
`pgx.Identifier.Sanitize`. The provider-added columns and defaults are fixture
extensions only, not a stock River tenant API, SDK definition, or S3 schema.

## Setup-only failure and source correction

The parent reported all four original cases failed with SQLSTATE `42P01` before
any worker started; its original evidence remains separate in
`test/spike/s0/results/worker-functional-first.log` (not read or changed by this
child). Source review identifies the fixture's attempted ALTER of already-dropped
`river_client` as the first absent relation: statement 21 in the old setup list.
`river_notification` exists. The entire extension/policy/result DDL is one
transaction and rolls back on that failure; absent provider columns/result rows
in those retained fixtures are **failed setup, not observed data loss**.

This revision corrects the table inventory and adds exact catalog assertions.
Redacted failures now preserve a fixed stage and one-based DDL statement counter
without printing SQL, schema/role names, connection details, or server messages.
The prior failed results are not erased or reclassified as passing. Worker
behavior on those attempts was **NOT measured**. This corrected revision is
compile-only until separately reviewed and run by the parent; it is not a
functional success claim and changes no authority mechanism.

## Source API evidence (v0.51.2)

- [`riverdriver/riverpgxv5/migration/main/007_notification_outbox_sqlite_jsonb_and_sql_cleanup.up.sql:5-30`](https://github.com/riverqueue/river/blob/v0.51.2/riverdriver/riverpgxv5/migration/main/007_notification_outbox_sqlite_jsonb_and_sql_cleanup.up.sql#L5-L30):
  creates `river_notification`, then drops `river_client_queue` and `river_client`.
  [`008_job_id_autoincrement.up.sql`](https://github.com/riverqueue/river/blob/v0.51.2/riverdriver/riverpgxv5/migration/main/008_job_id_autoincrement.up.sql)
  is a PostgreSQL no-op. No upstream SQL is copied into this fixture.

- [`error.go:30-47`](https://github.com/riverqueue/river/blob/v0.51.2/error.go#L30-L47):
  `JobSnooze(duration)` retries without consuming attempts, not cancellation.
- [`event.go:34-35`](https://github.com/riverqueue/river/blob/v0.51.2/event.go#L34-L35):
  `EventKindJobSnoozed` exists. [`client.go:1403-1453`](https://github.com/riverqueue/river/blob/v0.51.2/client.go#L1403-L1453):
  subscriptions are local, buffered and nonblocking; they can drop events.
- [`internal/jobexecutor/job_executor.go:425-460`](https://github.com/riverqueue/river/blob/v0.51.2/internal/jobexecutor/job_executor.go#L425-L460):
  short snoozes (10/50ms here) return `available` with a future scheduled time,
  decrement the attempt, and update snooze metadata. No assumption of a special
  snoozed database state or scheduler wakeup is made.
- [`worker.go:79-110`](https://github.com/riverqueue/river/blob/v0.51.2/worker.go#L79-L110):
  `WorkerDefaults`, `AddWorker`; no removal API is used.
- [`insert_opts.go:102-159`](https://github.com/riverqueue/river/blob/v0.51.2/insert_opts.go#L102-L159):
  ByArgs + `river:"unique"` selects the ID field. No InsertMany claim is made.
- [`riverdriver/riverpgxv5/river_pgx_v5_driver.go:1179-1227`](https://github.com/riverqueue/river/blob/v0.51.2/riverdriver/riverpgxv5/river_pgx_v5_driver.go#L1179-L1227):
  listener acquires/hijacks a configured pool connection; explicit Schema makes
  its fixed topic prefix `schema + "."`. This fixture uses simple random safe
  schema names, `PollOnly=false`, explicit Schema, and jobs-role AfterConnect,
  so the listener is not accidentally left on bootstrap or a frontend scope.
- [`client.go:1657-1691`](https://github.com/riverqueue/river/blob/v0.51.2/client.go#L1657-L1691):
  JobRetry explicitly redelivers non-running jobs.

## Review/run handoff

Authoring only formats and compiles (`go test -mod=readonly -c -o /dev/null`). It
never invokes the test binary, opens a connection, executes SQL, provisions a
fixture, reads a private DSN, or tries a denied operation. Module downloading is
disabled during compilation (`GOPROXY=off GOSUMDB=off`). `compile-metadata.json`
records actual nonstandard compiled-package/module closure and license hashes;
`NOTICE` preserves River source routes and the nested Levenshtein MIT notice.

The parent may later review and run the **permitted fixture only** with private
`S0_ADMIN_DSN` plus `S0_EXPECT_DATABASE`; never a real external endpoint. When
missing, DSN causes Skip and TestMain reports **NOT measured**, not a pass. A
mismatched expected database fails before DDL. Credentials must stay in private
environment, never CLI arguments or evidence. Canonical credential-free command:

```text
go test -mod=readonly -count=1 -v ./plugins/jobs-river/internal/s0workers
```

`S0WORKERS_REPORT` reports case selection/status, whether a real worker started,
retained fixture IDs, source/executable hashes and explicit diagnostic limits.
It needs orderly TestMain return; panic/kill need separate failure evidence.
Fresh fixture schemas/roles are **retained**, never automatically dropped; only
connections, subscriptions and workers are cleaned up. This is not a full S0
acceptance, security boundary, performance, crash or durable exactly-once result.
