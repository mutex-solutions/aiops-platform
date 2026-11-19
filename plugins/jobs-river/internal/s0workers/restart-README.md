# Bounded S0 ordinary-functional restart diagnostic

**Authored and compile-checked only; the authoring child has not executed SQL,
opened a database, read a private DSN, or executed the test binary.** This adds
only `restart_test.go`, `restart_report_test.go`, and this document. Existing
worker fixtures, migrations, module pins, and original report are unchanged.
The corrected worker log records the four existing scope/revocation cases as
passing; it contains **no restart measurements**.

**The sections up to "Full restart gate" describe the smoke
(`TestWorkerRestartDiagnostic`); the table below is the smoke's gap list. The
gaps are closed, with the interpretations listed under "Implementation notes", by
the opt-in `TestWorkerRestartFullGate` (see that section).**

**Default `smoke1cycle !=30/1000load`. No full S0 acceptance claim.** The original
role-membership/writable-GUC fixture remains **UNSAFE / authority FAIL**; trusted
jobs intentionally sees all tenants. This does not repair authority, demonstrate
final kernel effects, simulate a process/DB crash, or prove exactly-once delivery.

## Opt-in and bounds

`TestWorkerRestartDiagnostic` skips before opening a fixture unless
`S0_WORKER_RESTART_RUN=1`. `S0_WORKER_RESTART_CLIENT_CYCLES` defaults to `1` and
accepts only `1..30`. Even selecting `30` runs **client-only smoke cycles**, not
full-load acceptance. No environment option weakens the frozen budgets.

| Frozen approved §6 criterion | This bounded diagnostic |
| --- | --- |
| Same-process pool recreation, client recreation, listener recreation: **30 each** | Only **client recreation**, 1 default / up to 30 explicitly selected |
| **10 combined** cycles | Unsupported; not implemented or counted |
| Recover **<=5s** | From before revoke/StopAndCancel through a fresh client's new-ID HTTP ACK, actual worker result, and persisted River completion |
| Acknowledged operation completed **<=30s** | From the HTTP client's observed 2xx+decoded ID to causal SQL observation of real result and completed state |
| **1000 new IDs at 100/s per recovery** | **One new probe ID per client recovery**; no load/rate/performance claim |

One client, `MaxWorkers=1`, runs at a time. Each case uses the original fixture:
admin max 2, caller max 6, worker max 6 configured connections; River's listener
hijacks its own configured connection. Client lifetimes are sequential, not
additional pools. There is no path approaching the fixture connection cap of 64.
Cleanup stops/cancels the live client before its worker pool closes. No DROP,
container/DB restart, session termination, PID targeting, or private endpoint
inspection is introduced. Only the process's own PID is recorded/compared.

## Implemented causal cases

- `restart_client_recreation`: `newWorker` creates the original client and stable
  dispatcher; `enqueue` creates a seed; a real loopback HTTP duplicate request
  provides an independent client ACK. Each cycle revokes/drains the handler,
  calls `StopAndCancel` with a 5s budget, constructs a new client on the **same
  worker pool**, registers that same dispatcher once in a fresh Workers registry,
  reinstalls the handler, and starts the client with a case-lifetime child
  context. An `AfterFunc` temporarily links recovery expiry to that lifetime,
  then detaches on success before the recovery context is canceled. A fresh
  tagged ID must ACK and finish inside the 5s recovery window. Both pools remain
  separate and unchanged. Stop/Start of the old client is not called recreation.
- `restart_ack_loss_idempotency_controls`: an actual `InsertTx` is committed
  before a server-side barrier, but response bytes are withheld. Only then does
  the HTTP client cancel and observe an error/no 2xx. The real worker's durable
  result and completed job are observed **before any client ACK for that ID**.
  A same-ID retry must return 2xx, `UniqueSkippedAsDuplicate`, and the original
  job ID. Explicit `JobRetry` then forces real handler redelivery with
  `inserted=false`; SQL requires exactly one job and one result for the ID.
  This is result-ledger idempotency, not crash/rescue/ambiguous-COMMIT proof.
- A separate request inserts but waits **before commit**. After its causal
  inserted barrier the client cancels; the handler explicitly rolls back and
  signals completion. SQL must find no job for that ID and no result, and the
  independent client ACK ledger must contain no entry. No cancellation sleep,
  synthetic result row, direct test-thread result write, or inferred ACK is used.

The loopback endpoint is synthetic, one per operation, bodyless, and bounded by
request/case deadlines. It uses `beginCaller` and the existing `enqueue` ordering
(`SET LOCAL jobs`, real `InsertTx`, restore caller, real commit), because helpers
using `testing.Fatal` cannot safely run inside HTTP handler goroutines. The HTTP
client decodes the committed job ID before recording a 2xx timestamp. Server
commit barriers are **never** counted as ACKs. This does not exercise production
ingress, proxy/TLS/network paths, body-size/cold-path acceptance, or external
fault injection. All handler results are written by the original `recordingHandler`.
Provider row lookup by job ID, not hostile payload tenant, supplies scope.

## Explicit unsupported harness boundaries (smoke only)

- **Pool-only:** `riverpgxv5.New` binds a concrete `*pgxpool.Pool`. Replacing
  `f.callerPool` would not replace the worker's separate pool, and closing the
  worker pool while keeping its direct driver would leave that driver closed.
  Client recreation would conflate cases. No incorrect mutable-pool API or
  architecture/proxy replacement is invented.
- **Listener-only:** the public fixture does not expose a uniquely verified
  listener endpoint/backend PID. Pool `AfterConnect` alone cannot distinguish a
  subsequently hijacked listener. No arbitrary `pg_stat_activity`/LISTEN match,
  role/name heuristic, or backend kill is used. Fresh client creation also
  replaces its listener, but is **not labeled listener-only**. Poll fallback is
  enabled; this is not isolated LISTEN wakeup evidence.
- **Combined/full-load:** not authored here. Parent review and separately
  scheduled harness work must cover the exact frozen counts and load, not
  reinterpret these smoke observations as acceptance.

## Full restart gate (`TestWorkerRestartFullGate`)

Opt-in with `S0_WORKER_RESTART_FULL=1` plus the fixture contract. Without the
switch it skips with "NOT measured". With it, missing fixture environment FAILS
(not skip). It implements S6 and the S4 protocol of
`docs/design/s0-spike-criteria.md`: arms pool, client, listener (30 cycles each),
then combined (10), each cycle a 30 s warm-up plus 120 s window at 100/s
open-loop, the restart request at measured t=60 s, a controlled 200 ms hold
before it, 1,000 new IDs at 100/s after each recovery, <=5 s recovery, <=30 s
ack-to-completion. 3x30+10 = 100 cycles x ~150 s plus drain, about 4.3 hours, plus about three minutes
for the four section 10 warm-ups, baselines, checkpoints and teardown checks.

```text
S0_ADMIN_DSN=... S0_EXPECT_DATABASE=... S0_WORKER_RESTART_FULL=1 GOMAXPROCS=8 \
  go test -count=1 -timeout 6h -v -run '^TestWorkerRestartFullGate$' ./plugins/jobs-river/internal/s0workers
```

No race detector for the frozen run (S4: optimized performance is measured
without it); a `-race` run is labelled not frozen-scale in its report. Reduced
scale knobs (`S0_WORKER_RESTART_FULL_CYCLES`, `_COMBINED_CYCLES`,
`_WARMUP_SECONDS`, `_WINDOW_SECONDS`, `_RECOVERY_IDS`, `_FAILFAST`) only shrink the
run, the same bounds still apply, and the report then says NOT acceptance.
`S0_WORKER_RESTART_FULL_JOB_RETENTION_SECONDS` (default 900; see "Bounded
retention") does not shrink the run; 0 turns River's cleaner off and then the run
is not acceptance either.
Output: one `S0WORKERS_RESTART_FULL_CYCLE` JSON line per cycle (every bound with
limit/observed/pass) and one final `S0WORKERS_RESTART_FULL_REPORT` line with
per-arm totals, interpretations and limits. Section 10 adds the lines
`..._S10_DECLARED`, `..._S10_BASELINE`, `..._S10_CHECKPOINT` (cycle 0) and
`..._S10_TEARDOWN` per arm, a `section10` snapshot in the cycle line that follows a
checkpoint, and `section10_label` plus a `section10` block (declared background set,
every snapshot, series with slopes, failed bounds, checkpoint costs) in the final
report; see "Section 10 requirements". The run FAILS when any bound is
missed, any cycle is not executed, or a ledger/scan error occurs. Pure logic
(config, open-loop scheduler, ledgers, reconciliation, bounds) is covered by
database-free `restart_full_unit_test.go`, which always runs.

Section 10 (resource and leak accounting) is enforced, with the limits listed in
"Section 10 requirements". The recorded frozen-scale pass (commit 9ba2c5a,
`test/spike/s0/results/restart-full-frozen-scale`) predates it: its binary did not take the
checkpoints, so it is "not evaluated" for section 10.

Resources (`restart_full_app_test.go`): loopback HTTP listener; caller pgx pool
plus insert River client; trusted-jobs pgx pool plus worker River client; each
role pool max 8 connections, worker `MaxWorkers=4`. Enqueue is one transaction
(`InsertTx`, hold, commit) and 2xx is written only after COMMIT. While the
caller side is being recreated the endpoint returns 503 (never counted as work)
and worker handlers snooze.

## Implementation notes (interpretations, stricter reading taken)

- Pool: riverpgxv5 binds a concrete pool, so pool recreation recreates both pgx
  pools AND the River clients on them. Client: both River clients recreated on
  unchanged pools (dispatcher reused). Listener: HTTP listener only, same loopback
  address. Combined: all three. "Listener" is not River's LISTEN connection.
  The earlier "unsupported" boundaries refer to the smoke harness and are
  superseded for the full gate only by this explicit mapping.
- Fresh fixture (schema, roles) per arm; fresh ledger and ID prefix per cycle.
- Recovery = request instant to the earliest durable completion (SQL: completed
  state and result row) of an ID whose ACK carries the epoch published only by the
  recreated resources. This includes first accepted work AND consumer completion.
- "Previously acknowledged IDs complete <=30 s after recovery" is checked as
  both "within 30 s of its own ACK" and "no later than ready+30 s".
- "1,000 new IDs at 100/s per cycle": the first 1,000 IDs scheduled at/after the
  recovery instant must ACK on the FIRST attempt with no failed attempt, complete
  <=30 s after ACK, and start within 50 ms of schedule. Stricter than "eventually". Every arrival of the cycle must start within 50 ms of its schedule (a late one fails the cycle; lateness is recorded, the schedule is never shifted).
- Same-ID retries are allowed only inside the S4 5 s operation timeout from
  scheduled arrival; a timeout stays in the accounting.
- Ack-to-completion is measured to the first SQL observation (50 ms poll plus a
  final sweep for anything the poll missed), an upper bound by the poll interval.
- Duplicates: zero-bound is duplicate LOGICAL results (more than one job or result
  per ID) and a duplicate ACK on first attempt. Retries, River retries, handler
  redeliveries are reported only.
- Held work: held HTTP transactions are capped at half the caller pool so the
  consumer is not starved; at least one HTTP transaction AND one worker handler
  must be in flight at the restart request or the cycle fails. The criteria's
  "kernel leases" hold has no kernel here and is not simulated.
- Orphans: failed drain/stop/close/shutdown within 5 s, wrong live pool/client/
  listener count, or idle-in-transaction sessions of the fixture login after drain.
- 30 s completion drain after the window is counted from the last ACK.
- Concurrency cap: at most 32 requests is structural. A gate in front of every
  HTTP attempt takes a slot before the request is made; an attempt that finds none
  waits (inside its own 5 s operation timeout and its latency from the scheduled
  instant; the arrival schedule is never shifted) and is counted as
  `concurrency_cap_deferred_attempts`. The in-flight maximum is still observed
  independently by `concurrent_requests_max`. Before this the cap was only
  observed: arrivals spawned unbounded goroutines, so one frozen-scale cycle read 33.
- Observation cost is independent of retained history. The final sweep used
  `args->>'idempotency_id' LIKE prefix%` over the whole River table (one sequential
  scan of every retained row per cycle) and the same shape on the result table. Both
  are now bounded by `id > floor` where the floor is `max(id)` read (primary-key
  probe) before the cycle starts; job ids come from a sequence, so every job and
  result of the cycle is above it. The 50 ms completion poll was never the cause:
  it is an index range scan on `(state, finalized_at)` plus a primary-key join.
  All observation runs on the admin pool, never on the caller or jobs pools.

- Section 10 (details in "Section 10 requirements"; where the text is ambiguous the
  stricter reading is taken):
  - "Cycles 0/10/20/30": cycle 0 is a control taken after the warmed baseline and
    before any restart; 10/20/30 are taken after those cycles complete. The 10-cycle
    combined arm has the cycles of {0,10,20,30} that exist in it, 0 and 10, and is
    held to those (a 20 or 30 is not invented). A reduced arm additionally gets a
    checkpoint after its last cycle, so a state after a restart is always evaluated.
  - A checkpoint happens between cycles, never inside a load window. It first drains
    (requests, held work, delivery queue, River jobs of this arm, bounded by the S4 30 s),
    closes the load generator's idle connections and settles 250 ms; then GC twice.
    A violating snapshot is re-taken after 250 ms, up to 8 times, because River's own
    5 ms fetch poll and the 1 ms debounce goroutine can legitimately be caught
    mid-flight in a single sample; a leak persists across every re-take, so it fails
    on the last one. A finding that is only a disposed pool/client still reachable is
    re-taken for up to 15 s (River's job timeout is 10 s and library timers and contexts
    keep objects alive until they fire); a leak is still reachable after that. The attempt count and what the first sample still saw (`first_attempt_findings`) are
    in the record.
  - Equality with the warmed baseline is per owner, role/generation and entry
    function, in both directions: a group that disappeared and a group that leaked are
    separate findings and never cancel. Raw totals are only a reported series.
  - The fixed background set is declared in code (`s10Declared`) and printed before
    the first measurement. The warmed baseline itself must equal it (declared set plus
    the pre-application harness baseline), so it can never be extended after a leak.
    Anything outside the six owners is `unclassified` and fails.
  - "Remaining live resources equal the baseline" is applied literally to goroutines,
    non-PostgreSQL descriptors, entries, acquired and constructing pool connections,
    sessions idle in transaction and statements stuck for over 2 s. Idle/total
    connections of a pool, and the PostgreSQL sockets and sessions behind them, are
    demand driven (the pool opens connections lazily: 3 to 8 per pool were observed on
    otherwise identical runs), so equality with a baseline value would fail clean runs.
    They are bounded by exact relations instead: sessions of a pool generation ==
    pool total + 1 for the worker pool (River's LISTEN connection is hijacked from it) <=
    max connections; PostgreSQL sockets == sessions of the fixture login + the harness
    admin pool's connections. A connection leaked from a live pool therefore shows as a
    session above the pool's total, one leaked from a disposed pool as a disposed
    generation owning a session. These two are the only exceptions and are reported as
    information series with a slope.
  - Warm-up opens every connection of both application pools once (`warmPools`, harness
    only, before the baseline and before cycle 1). A pgx connection holds about 0.2 MB
    (statement cache) and the restart burst grows a pool to its maximum, so without this
    the baseline had 9 connections and the checkpoints 13 to 16: a 0.6 to 0.9 MB rise that
    sat just under the 1 MiB tolerance by luck. The cycles' own pool behaviour is not
    changed (a recreated pool still starts with one connection).
  - Retained heap is HeapAlloc after two GCs; the bound is `max(1 MiB, 5% of the warmed
    baseline)` above the baseline at every checkpoint; the series and the least-squares
    slope are reported. A heap in tolerance never excuses a goroutine, descriptor,
    session or entry finding (each is its own bound).
  - Growth: any exact series above the previous checkpoint or the baseline fails,
    whatever the equality bounds said. Slopes are per cycle over the checkpoint cycles.
  - Disposed generations: pools, clients, listeners are numbered per role; goroutines
    carry the generation as pprof labels (set when the harness creates the object, so
    they are inherited by the library's goroutines), server sessions carry it in
    `application_name` (`s0wk-<role>-<generation>`), set only on the harness's own pool
    configuration. Neither changes what is measured. A goroutine, session, route,
    handler or retained object of a generation that is not the live one fails
    `s10_disposed_generations_own_zero`.
  - A stopped River client can stay reachable after client-only recreation:
    pgx's `pgconn` keeps the context of the last query on every connection until that
    connection's next query (`PgConn.resultReader.ctx`, `multiResultReader.ctx`), and
    River's job contexts carry the client. Up to one stopped client per pooled
    connection can therefore be pinned while the pool lives and is released when the pool
    is closed (observed 0 to 2 in short runs). The check allows exactly that bound
    (pinned clients <= connections of the live worker pool) and treats a client of a
    closed pool, or any disposed pool, still reachable as a retained disposed object.
    It is not a probe defect and is not changed here.
  - After full teardown (application closed: listener, insert and worker clients, both
    pools) goroutines, descriptor classes and entries equal the pre-application harness
    baseline, no non-PostgreSQL descriptor above it, nothing of any generation remains.
  - Labels: `passed` only for a frozen-scale run (full cycles, warm-up and window, GOMAXPROCS
    8, no race detector) with every required checkpoint and the teardown check;
    `partially evaluated (reduced)` for any reduced run, `partially evaluated
    (incomplete)` for a frozen-scale run that did not reach them, `FAILED` when any
    bound is missed, `not evaluated` when no baseline was taken, and for any report
    without the field (a binary older than this change). The CI invocation therefore
    reports `partially evaluated (reduced)`, never passed.
  - Probe defect fixed: the delivery tally kept one map entry per delivered job for the
    whole arm (about 450,000 at frozen scale). It is cleared at the end of every cycle
    (`resetTally`); redeliveries are counted within a cycle and cumulatively as before.
    A redelivery of a job that was first delivered in an earlier cycle is now counted as
    a first delivery (the redelivery numbers are reported, not bounded).
  - The delivery observer goroutine now starts with the application (`start`), so a
    harness baseline can be taken before any application goroutine exists.

### Finding: sequential scans of the River table come from the system under test

A frozen-scale run (first 20 pool cycles) read 97,389 sequential scans /
6.59 billion tuples on `river_job` while throughput collapsed past about 90,000
rows. Reproduced with `S0_WORKER_RESTART_FULL_PREGROW_ROWS` (completed jobs and
result rows seeded by the admin role) and `pg_stat_statements` on PostgreSQL 16.15,
each component run in isolation against an 85,000-row table (500 executions each):

| statement (role) | seq scans | seq tuples read | plan |
| --- | --- | --- | --- |
| completion poll, join on result (admin) | 0 | 0 | index scan `(state,finalized_at)` + pkey |
| `SELECT tenant,scope_id ... WHERE id=$1` (jobs role, RLS) | 0 | 0 | primary key |
| River `JobGetAvailable` fetch shape (jobs role, RLS) | **500** | **42,500,000** | Seq Scan, Sort, LockRows |
| final sweep, once per cycle (admin), old shape | 1 | 85,000 | Seq Scan (now pkey range, 0) |

In the whole run `JobGetAvailable` was the only heavy statement (about 4,900
buffers per call at 160,000 rows versus about 20 for `JobInsertFastMany`).

Cause: the fixture forces row-level security on `river_job` with the `provider_jobs`
policy (`true`) for the jobs role. For a role subject to RLS the planner may only use
column statistics through leakproof operators, and `enum_eq` (`state = 'available'`)
is not leakproof. The estimate for `state='available'` therefore falls back to
1/n_distinct, 28,186 rows against 118 as superuser (400 real), and for the fetch
shape the plan flips from the `river_job_prioritized_fetching_index` scan (cost about
8) to a Seq Scan plus Sort (cost about 4,900) once the table is large. The same query
as a superuser, or as the jobs role with `BYPASSRLS`, or as owner without FORCE, uses
the index with cost about 8. Policy shape (`true`) does not matter; any RLS qual on
the table triggers it. This is a finding about the fixture's RLS design, not about
the harness, and it is reported, not hidden: every cycle of a table of 160,000
retained rows fails (cycle 1: 3,647 of 5,000 acked ids durable, ack-to-completion
about 50 s, recovery 35 s; from cycle 4 on none of the cycle's own ids complete).

Fix candidates were: give the jobs role a policy-free path to River's tables
(owner without FORCE, or BYPASSRLS), or keep RLS and accept the growing fetch cost,
or bound the retained history; per-role `enable_seqscan=off` is a diagnostic only.
**Applied 2026-11-18 (ADR-045): River's tables have no RLS and no policy, only the
jobs role holds privileges on them (no BYPASSRLS, no ownership), and finished jobs
are bounded by River's cleaner (see "Bounded retention").** The text above is the
measurement that led to that decision, taken with the previous fixture.

Proof that this is the only cause: `S0_WORKER_RESTART_FULL_DIAG_JOBS_NO_SEQSCAN=1`
(diagnostic, makes the run non-acceptance) sets `enable_seqscan=off` on the fixture
login role. On a 160,000-row table, 12 pool cycles at 100 ids/s (10 s warm-up,
40 s window, 1,000 recovery ids) every cycle durably completed 5,000 of 5,000 acked
ids, ack-to-completion max 648..958 ms, recovery 502..754 ms, and `river_job`
`seq_scan` stayed at 16 (all of them before the first cycle) while the table grew to
about 220,000 rows. Without the diagnostic setting the same run failed from cycle 1.
`S0_WORKER_RESTART_FULL_PREGROW_ROWS` and the diagnostic setting are opt-in, are
recorded in the report config and are never part of an acceptance run.

## Bounded retention (ADR-045, 2026-11-18)

River's own cleaner bounds the retained `river_job` history. The worker client in
the full gate sets `CompletedJobRetentionPeriod`, `CancelledJobRetentionPeriod` and
`DiscardedJobRetentionPeriod` (River v0.51.2) from
`S0_WORKER_RESTART_FULL_JOB_RETENTION_SECONDS`, default **900** (15 minutes); `0`
maps to River's `-1` (no deletion). The cleaner runs in the client that is elected
leader (every 30 s, batches of 10,000 by id, as the jobs role), so with the pool
and client arms recreating the worker client each cycle the leader changes and the
first pass of a new leader follows its election. At about 15,000 rows per
150 s cycle, 900 s keeps about six cycles (about 90,000 rows) instead of 450,000.

Observation cannot race the cleaner, by construction and by detection:

- The value is refused unless it is 0 or at least twice `CycleBound()` (warm-up +
  window + two 30 s budgets + 30 s: 240 s frozen, so at least 480 s; 115 s for the
  CI reduced gate, so at least 230 s). A cycle's own jobs finish no earlier than
  its start and its final sweep (the last reader of `river_job`) completes within
  `CycleBound()` of it, so no job of the cycle is eligible before the sweep. The
  durable result row lives in the caller-owned `result` table, which the cleaner
  has no privilege on and never touches.
- The sweep that finds an acknowledged ID with its result row but no job row
  records it as `job_removed_before_final_sweep`, a failed bound of its own. Such an
  ID is never counted as completed and is not reported as a false 2xx.
  `TestReconcileJobRemovedBeforeSweep` and `TestJobRetentionParameter` cover both.
- `TestRiverCleanerBoundedRetention` (database) shows the cleaner removing old
  completed, cancelled and discarded jobs as the jobs role while keeping a recent
  finished job, unfinished jobs and every result row.

Each cycle line now also carries `river_job_seq_scans_in_cycle`,
`river_job_seq_tuples_read_in_cycle`, `river_job_live_rows_after_cycle` and
`river_job_total_bytes_after_cycle`, and the gate prints
`S0WORKERS_RESTART_FULL_FETCH_PLAN` (EXPLAIN ANALYZE of River's fetch as the jobs
role) before the cycles and after the last one, and fails when the table has 10,000
or more rows and that fetch is not an index scan. With retention on, River's own
delete (`JobDeleteBefore`) reads the retained table sequentially about once per 30 s
pass: bounded by the retained size, not by history.

Measured on the development host (PostgreSQL 16.15, reduced 5 s + 20 s cycles; see
ADR-045 for the numbers): with 150,000 pre-grown rows and retention off, 12 pool
cycles completed every acknowledged ID with no sequential scan of `river_job` per
cycle; with retention on the live rows stayed near 24,600 over 30 cycles.

## Section 10 requirements

`docs/design/s0-spike-criteria.md` section 10 against this harness. "Before" is the
state at commit 2ccb612 (per cycle: `goroutines_after_cycle`, `open_fds_after_cycle`,
live pool/client/listener counters, orphan checks; the earlier README said "S10 leak
accounting is not bounded here"). Bound names are the `s10_*` names in the cycle lines
and the report.

| Section 10 item | Equivalent in this probe | Before | Now | Missing or different |
| --- | --- | --- | --- | --- |
| Warmed baseline, root/pool/River/listener alive | No kernel root. The application is two pgx pools, an insert and a worker River client, the HTTP listener; the harness coordinator and load generator around it | none | `warmed_baseline` per arm after warm-up traffic (the configured warm-up, at least 3 s, at 100/s), drained, quiesced, two GCs; must equal the declared background set (`s10_warmed_baseline_matches_declared_background_set`). A pre-application harness baseline is taken before the application exists | no root generation |
| Cycles 0/10/20/30 after drain, Settle, observer-queue completion, GC | No kernel lifecycle Settle; the quiescence step stands in (nothing in flight, River jobs finished, idle connections closed, 250 ms). The observer queue is the dispatcher's delivery channel to the harness tally | totals every cycle, unquiesced, not compared | checkpoints 0/10/20/30 (combined 0/10; reduced: 0 and the last cycle); `s10_quiescence_before_measurement` | cost: see below |
| Fiber entries | none (kernel concept) | n/a | n/a | accounted in `test/spike/s0/scale` (per-checkpoint `FinalFibers`, idle goroutines per fiber against the empty-root baseline in `classify.go`) |
| Provision entries | none | n/a | n/a | `test/spike/s0/scale` (`FinalProvisions`, `ActiveProvisionsEnd`) |
| Contribution entries | none | n/a | n/a | `test/spike/s0/scale` (contribution resources active/leases in `checkpoint()`) |
| Route entries | one synthetic HTTP route per listener generation | none | `route@http-server#gen` = 1 live, 0 disposed (`s10_entries_equal_warmed_baseline`, `s10_disposed_generations_own_zero`) | no kernel route table |
| Listener entries | the HTTP listener per generation and its socket and accept loop | live count only | `listener@http-server#gen`; exactly one listening descriptor (`listener-socket`), any other LISTEN socket is `listen-socket-unexpected`; accept-loop goroutine per generation | River's LISTEN connection is a session of the worker pool, accounted there |
| Job-handler entries | the handler registry view of each worker client generation | none | `job-handler@river-worker#gen` = 1 live, 0 disposed | none |
| Registry entries | `river.Workers` per client, the pool/client registry of the generation ledger, the delivery tally map | none | `pool@`, `client@`, `listener@`, `route@`, `job-handler@` per generation and `harness-delivery-tally` = 0 at quiescence | the tally map was unbounded (fixed, see Implementation notes) |
| Active/acquired connections by owner and generation | pgx pool statistics per pool and generation (acquired, idle, total, constructing, max); server sessions by state per `application_name` generation | live pool count | `s10_connections_accounted_per_owner_generation`: acquired and constructing equal the baseline, sessions per generation == pool total + allowance <= max, sockets == sessions + admin pool | idle/total are demand driven: relation-bounded, not baseline-equal (Implementation notes) |
| Open transactions by owner and generation | sessions idle in transaction and statements active for over 2 s, per generation | idle-in-transaction of the login, total, as orphans | `s10_no_open_transaction_or_stuck_statement` | none |
| Tracked goroutines | goroutine profile classified into six owners by creating (entry) function, with role/generation from pprof labels | one total | `s10_goroutines_equal_warmed_baseline_per_owner` per owner/role/entry, `s10_disposed_generations_own_zero`, growth | the six tickers of the River leader share one stack and are counted together |
| Contribution leases | none | n/a | n/a | `test/spike/s0/scale` (`Leases`) |
| Pending transitions | none | n/a | n/a | not a separately named counter anywhere I could find; the scale harness records observer accepted/delivered/queued/in-flight status around Settle |
| Effect ledger | none in this package (the result table is the application idempotency ledger, reconciled by the existing bounds) | n/a | n/a | the kernel effect ledger is a final-kernel item (criteria section 6 last paragraph) |
| Live sockets / file descriptors | `/proc/self/fd` classified from the TCP tables: listener, PostgreSQL sockets, HTTP client and server connections, other sockets, epoll, pipes, files | one total | `s10_fds_equal_warmed_baseline` per class (PostgreSQL sockets via the relation above) | the scale harness has no descriptor accounting that I found |
| Retained heap | `runtime.ReadMemStats` HeapAlloc and HeapObjects after two GCs | none | `s10_retained_heap_within_tolerance`: <= baseline + max(1 MiB, 5%); series and slope | the report's own retained cycle lines count (about 5 KB a cycle) |
| Disposed scopes/generations own exactly zero | every pool, client, listener, insert-client generation | none | `s10_disposed_generations_own_zero` (goroutines, sessions, entries, retained disposed objects) | none |
| No cycle-to-cycle growth, series, slope | checkpoint series per exact and informational counter | none | `s10_no_cycle_to_cycle_growth`; `series` in the report (values, least-squares slope per cycle) | needs at least two checkpoints |
| After full teardown, counts return to the harness baseline, no FD growth | teardown of listener, insert and worker clients, pools | none | `s10_teardown_returns_to_harness_baseline`, `s10_teardown_no_fd_growth` | the admin and fixture pools stay open (they are the baseline) |

Cost of a checkpoint on the development host (shared, busy): about 0.3 s when clean
(250 ms settle, two GCs, profile, descriptors, one session query, one primary-key range
read), 0.5 to 0.9 s when a River poll was caught and it was re-taken, about 2.5 s for a
persistent violation (8 re-takes). Per arm: baseline, cycle 0, 10/20/30, teardown, plus
the 3 s to 30 s warm-up traffic. Frozen scale adds about three minutes to 4.3 hours.

Not evaluated, and what the label means: a run with no section 10 block (an older binary,
such as the recorded frozen-scale pass) is `not evaluated`; reduced runs that still take the
baseline, the cycle 0 checkpoint, the last-cycle checkpoint and the teardown check are
`partially evaluated (reduced)`; neither may be cited as section 10 evidence.

## Evidence and review handoff

`S0WORKERS_RESTART_REPORT` is a distinct stdout JSON line with UTC run/recovery
boundaries, same PID, first durable observations, independently client-observed
ACKs, durable-before-ACK IDs, canceled-before-commit IDs, selected case statuses,
and before/after SHA-256 of all four original helpers plus the three new files.
First durable timestamps are preserved across retries. Any source change fails
the diagnostic. No credentials, DSNs, passwords, hosts, database names, raw SQL,
backend queries, or raw errors are serialized. Hashes identify on-disk sources,
not an attested build; orderly cleanup/TestMain return is required. The existing
`runDiagnostic` appends `restart_` case names to the frozen generic report;
`report.WorkerStarted` is set by the real fixture start. Neither report overrides
the original authority FAIL or upgrades diagnostic counts to acceptance.

Child validation is only:

```text
gofmt -w plugins/jobs-river/internal/s0workers/restart_test.go plugins/jobs-river/internal/s0workers/restart_report_test.go
GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local go test -mod=readonly -c -o /dev/null ./plugins/jobs-river/internal/s0workers
```

After source review, the **parent only** may supply the approved private fixture
credentials in environment and run the gated test (never put DSNs in arguments):

```text
S0_WORKER_RESTART_RUN=1 go test -mod=readonly -count=1 -v -run '^TestWorkerRestartDiagnostic$' ./plugins/jobs-river/internal/s0workers
```

Missing credentials still skip via the original fixture and mean NOT measured.
A later reviewed race/full-cycle run is parent-owned. No dependency, CI, commit,
company code, authority mechanism, or production architecture change is included.
