# ADR-045: Caller transactions and SQL authority — S0 findings

- Status: **S0 continuation scope approved by the owner, 2026-11-12; implementation decision remains proposed.** Option A is authorized for further evidence only.
- Date: 2026-11-12.
- Scope: S0 correctness evidence and the next experiment, not an accepted change
  to the canonical SQL contract or the later S3 definitions.

## Context

The accepted design uses a caller-owned PostgreSQL transaction, one role/schema
per plugin, FORCE ROW LEVEL SECURITY, and unmodified River InsertTx with a
provider-only role switch. Only the infrastructure participants join that
transaction. The kernel holds no database policy. In-process code is trusted,
not a sandbox against hostile code; the SQL view must nevertheless make its
actual authority clear.

The [frozen criteria](../design/s0-spike-criteria.md) require explicit negative
probes for role/scope-setting changes through the caller's SQL capability. They
are not replaced by a successful direct cross-schema permission denial.

## Measured S0 evidence

On PostgreSQL 16.15, a dedicated fixture used a fresh nonowner runtime login with
NOSUPERUSER, NOBYPASSRLS, NOINHERIT and no role/database creation. It was allowed
to assume the caller and jobs no-login roles, as needed by the mechanism under
test. Scoped tables used ENABLE and FORCE RLS; live fsync and synchronous_commit
were on. The caller transaction entered its own role and a synthetic scope.

The control-enhanced [raw result](../../test/spike/s0/results/20261112T064938Z-sql-boundary-controls-race.json)
and [test log](../../test/spike/s0/results/20261112T064938Z-sql-boundary-controls-race.log)
record process exit **1** under the race detector and classification **fail**.
Before setting tampering, the own scope was visible, the foreign scope was not,
and a missing scope saw zero rows. Rollback left no attack row; the reused pool
backends returned to login role with no scope setting. The earlier qualified
three-file run and its repetition are also retained. The attack observations are:

| Caller statement/capability | Observation |
|---|---|
| SET LOCAL ROLE jobs | Entered the jobs role |
| RESET ROLE | Returned to the login principal |
| set_config of the scope followed by a scoped SELECT | Read the other synthetic scope under FORCE RLS |
| set_config of the scope followed by INSERT | Wrote the other scope; transaction rolled back |
| SET LOCAL scope setting | Changed the setting used by the policy |
| Direct outbox-schema INSERT under the caller role | Denied, SQLSTATE 42501 |

Every attack transaction was rolled back. A race-instrumented repetition
[also failed](../../test/spike/s0/results/20261112T064330Z-sql-boundary-race.log)
for the same assertions, with no race-detector diagnostic. Focused vet completed
without diagnostics. These are not passing acceptance results.

The first invocation is retained but has an unqualified live-directory source
hash because a noncompiled draft appeared between build and run. The qualified
rerun used exactly the three compiled source files and a matching executable
hash. See the [evidence index](../../test/spike/s0/results/README.md).

### Limits of the finding

This minimal experiment used raw pgx.Tx SQL with a mutable transaction setting.
It does **not** prove that every SQL-view implementation is unsafe, that River
InsertTx failed, or that atomic cross-plugin participation is impossible. The
fixture uses a string-valued scope setting rather than a complete opaque Handle/
Tx wrapper; that mechanism choice is stated, not attributed to an implemented
product service. The accepted Tx sketch does expose SQL-string Exec/Query methods,
so these operations are exercised through that capability, not sqlDriver access. River
insertion, four-participant atomicity, worker identity, role-switch cost and
restart/load have not been measured. No product SQL definition exists yet.

The result does prove that this role/GUC mechanism alone is not an unforgeable
capability boundary against SQL that deliberately changes the same session
identity/settings. It must not be described as such. PostgreSQL checks SET ROLE
against login-session membership; FORCE RLS does not make an ordinary custom
setting immutable.

## Owner direction for the next experiment (2026-11-12)

The owner selected option A: continue the bounded spike while retaining the
original guarantees. Keep the measured failure and frozen criteria unchanged.
Options B and C below remain unselected alternatives, not permission to weaken
isolation or change atomicity. No final mechanism or S0 completion is accepted.

### A. Continue a bounded transaction-authority design spike (approved scope)

Retain caller-owned atomic transactions, FORCE RLS, infrastructure-only Tx
participants and unmodified River. Investigate a mechanism that distinguishes
trusted transaction authority from caller SQL, with explicit provenance for the
scope and restricted privileged operations. Validate it on the same negative
cases before selecting an implementation. Possible database capabilities,
separate principals and scoped function paths need their own experiment; none
is asserted to work with River InsertTx yet. Do not use a SQL-text blacklist.

The first deliverable is evidence of feasibility or incompatibility, not a new
accepted contract. Any change in role topology, Tx surface or identity mechanism
requires an owner-approved amendment to the canonical record in the same change.
No third-party dependency is added without its licence check. Before any later
probe can report green, controls must assert the expected denial SQLSTATE and
successful follow-up state; a timeout, connection loss or unrelated SQL error is
not a valid refusal. Positive observed escapes in this run remain failures
regardless of that future validation strengthening.

### B. Explicitly classify role/GUC changes as forbidden trusted-code behavior

Record that the existing mechanism protects against ordinary missing/mistaken
scope and unauthorized direct schema access, not deliberate role/scope rewriting
through raw SQL. Preserve the failing adversarial test as a documented boundary
rather than converting it into a pass. This changes the interpretation of the
SQL guarantee and the frozen acceptance requirement; it therefore requires the
owner's explicit approval and a canonical/criteria amendment before continuing.
It is not a silent simplification justified by schedule.

### C. Require a stronger boundary and permit a different participation contract

If unforgeable isolation through unrestricted caller SQL is required and no
compatible capability mechanism passes, propose a different transaction
participation design with its durability/atomicity tradeoffs. Do not silently
replace same-transaction InsertTx with an outbox bridge or another jobs engine.
Such a change needs a separate owner-approved architecture amendment and
re-baselined tests.

## Approved follow-up observation

The owner authorized option A without weakening the original guarantees. A
separate [pre-execution protocol](../design/s0-sql-authority-followup.md) fixed
three falsifiable hypotheses without modifying the original frozen criteria.
The [recorded matrix](../../test/spike/s0/results/20261112T072707Z-authority-matrix.json)
reproduced their limitations under the race detector:

1. REVOKE SET on the ordinary custom setting does not prevent set_config scope
   tampering; the foreign scoped row remains readable.
2. A SECURITY DEFINER helper attempting SET ROLE is refused with 42501.
3. Removing jobs SET membership denies the identical ordinary role switch to
   both caller SQL and the trusted direct transaction call site, both 42501.

The diagnostic test exits 0 because it correctly records those limits, with
classification `incompatible-mechanisms-reproduced`; the original gate remains
failed. Two earlier follow-up attempts failed before database statements due to
an outdated private endpoint after Docker reassigned an ephemeral published
port. They are retained as inconclusive setup runs, not hypothesis outcomes.

These results match PostgreSQL's documented [session-user membership rule](https://www.postgresql.org/docs/16/sql-set-role.html)
and [parameter-privilege limits](https://www.postgresql.org/docs/16/ddl-priv.html#DDL-PRIV-SET).
Under these exact assumptions, ordinary SET ROLE on one unrestricted-SQL session
cannot authenticate which Go call site issued it. A private Go context flag,
NOINHERIT, LOCAL versus session scope, a random prepared-statement name or a
privilege grant window does not establish that provenance.

A protected tenant-binding registry or signed capability may be investigated
separately; neither is an implemented solution or a provider-role fix. It must
resist replay, introspection, scope replacement, rollback/savepoints and pool
reuse. If a new authenticated executor, query capability, server extension or
restricted SQL surface is needed, its change to the accepted role/Tx mechanism
must be presented to the owner before adoption. No claim that all same-transaction
InsertTx approaches are impossible follows from this diagnostic.

## Independent ordinary atomicity evidence

A separate ordinary-functional fixture has now exercised state, unmodified River
InsertTx, outbox and audit on the same caller-owned transaction. The
[actual result](../../test/spike/s0/results/20261112T174224Z-ordinary-atomicity.json)
records 21 selected passing cases under the race detector, with fsync and
synchronous_commit on: two scoped commits, precommit invisibility, equal writer
transaction IDs, rollback and SQL errors at participant boundaries, failed restore
on an aborted transaction, explicit rollback after cancellation, own connection
loss before commit, missing scope, direct foreign-schema denial and reuse.

This deliberately uses the original unsafe role/GUC mechanism and a provider-owned
identity-column extension. It is evidence for ordinary atomic participation only,
not SQL-authority acceptance, a new capability mechanism, background identity,
performance or a final S3 contract. The failed authority gate is not waived.

## Unexecuted capability hypothesis and tool boundary

A further role-capability protocol and test-only source artifact were prepared
under option A as local-only excluded drafts; they are not included in this
reviewed checkpoint. The hypothesis uses a
capability-authenticated definer grantor, transactional jobs membership and a
same-transaction provider window, restoring/revoking authority before caller SQL
resumes. It does not claim tenant authenticity or unmodified River compatibility.
Prepared-plan substitution, caller-controlled callbacks/deferred SQL, cross-session
cache visibility and shared-role lock serialization are named falsifiers.

A tool-policy review rejected the subsequent protocol-hash shell operation before
execution. That request was not retried or routed through another tool/child.
The capability artifact is **not formatted, compiled or executed** and has no
experimental result. Public-source reasoning and drafted code are not feasibility
acceptance. This ADR authorizes no workaround for tool restrictions and selects
no new authority mechanism.

## Owner-approved typed-operation investigation (not adoption)

After the existing role/GUC limitations and ordinary-functional evidence were
reported, the owner explicitly approved the bounded
[typed-operation surface spike](../design/s0-sql-typed-operations-proposal.md),
selecting “Duyệt spike typed operations, giữ nguyên guarantee”. This permits
investigating a caller Tx without unrestricted SQL-string Exec/Query, while
infrastructure retains its private raw-driver access for the same transaction
and unmodified River InsertTx. It does not approve a new canonical Tx definition
or a final authority mechanism; adoption requires results and a separate explicit
amendment/confirmation. Isolation, atomicity and all unchanged budgets remain.

The rejected capability operation and local drafts remain excluded and unexecuted.
The new investigation must have its own preexecution protocol and reviewed
closed operation surface, with no raw-SQL escape hatch or blacklist. It cannot
retroactively convert the original raw-SQL gate failure into a pass.

## Consequences while proposed

- The bounded transaction-authority correctness spike may resume under option A;
  dependent infrastructure/performance acceptance remains paused until the
  mechanism passes. No failed threshold is waived. Independent kernel work may
  resume with its accepted self-disposal meaning, but no concurrency choice or
  final-port acceptance is inferred from the observations so far.
- No mitigation is implemented by this ADR, and no raw SQL restriction is
  presented as already enforced.
- S0 remains incomplete. Later S3 ratification of definitions/providers and all
  previously open legal/product decisions remain open.

## Restart gate observation, 2026-11-18

The first frozen-scale run of the [full restart gate](../../plugins/jobs-river/internal/s0workers/restart-README.md)
on an idle dedicated host failed and was stopped in pool cycle 20 of 30. Every
acknowledged ID completed in cycles 1 to 6; from cycle 7 completion fell behind,
and from cycle 10 none of a cycle's own IDs completed in time. The retained job
table grows by 15,000 rows per cycle.

Attribution on PostgreSQL 16.15: River's fetch (`JobGetAvailable`) run as the
jobs role is subject to forced row-level security on `river_job`. For a role
subject to a policy the planner may use column statistics only through leakproof
operators; the enum equality on `state` is not leakproof, so the selectivity of
`state = 'available'` is overestimated and, once the table is large, the plan
changes from the prioritized fetching index to a sequential scan with a sort on
every fetch. The same statement uses the index for a superuser, for a role that
bypasses row-level security, and for an owner without forced row-level security.
The policy shape does not matter. With sequential scans disabled for that role as
a diagnostic, twelve cycles on a 160,000-row table each completed all IDs within
one second.

This is a finding about the transaction-authority design under evaluation here,
not about the harness: forced row-level security on River's own tables does not
scale with retained history. Options not yet chosen: a jobs role that is not
subject to row-level security on River's tables, bounded retention of finished
jobs, or both. No mitigation is implemented. The restart gate remains failed at
frozen scale.

## Owner decision on River's tables: jobs role outside row-level security, bounded retention, 2026-11-18

Status: **decision recorded before implementation.** It answers the finding above
for the probe fixtures. It does not accept the transaction-authority design, does
not change the failed SQL authority gate, and the restart gate stays failed at
frozen scale until it is rerun on the idle host.

The owner decided:

1. The jobs role is **not** subject to row-level security on River's own tables.
   Tenant isolation for job data rests on the typed-operations surface (callers
   never get raw SQL on those tables) and on grants, not on policies over
   `river_job`.
2. Finished jobs get bounded retention through River's own cleaner instead of
   being kept forever.

### Role and ownership design

Smallest change that implements (1): **River's five tables stay owned by the
separate NOLOGIN migration owner, row-level security is disabled on them (never
enabled, so also not forced), and they carry no policies.** The jobs role stays a
NOLOGIN, NOSUPERUSER, NOBYPASSRLS nonowner with exactly the table grants it has
today. Nothing else about the role topology changes.

| Principal | Access to River's schema and tables |
|---|---|
| Migration owner (NOLOGIN) | Owns the five tables and their sequences; used only by the migration session. The runtime login is not a member. |
| Jobs role | USAGE on the River schema; SELECT, INSERT, UPDATE, DELETE on `river_job`, `river_leader`, `river_queue`, `river_notification`; SELECT on `river_migration`; USAGE and SELECT on the sequences. No other privilege, no ownership, no BYPASSRLS. |
| Caller roles (per plugin) | **None.** No schema USAGE and no table privilege of any kind. |
| Runtime login | None of its own. It still holds SET-only, NOINHERIT membership in the caller and jobs roles; that is the recorded known-unsafe mechanism and is unchanged here. |
| PUBLIC | None. |

Because RLS is disabled on those tables, the planner has no security-barrier
qualifier to apply to the jobs role and may use column statistics, which is what
restores the index plan for River's fetch. The five tables are checked in the
fixtures as `relrowsecurity = false`, `relforcerowsecurity = false`, zero rows in
`pg_policy`, owner equals the migration owner.

Where `river_job` gets the experimental `tenant`, `scope_id` and `writer_xid`
columns, they remain defaults stamped from the transaction setting and
`pg_current_xact_id()`. They are provenance written by trusted infrastructure,
no longer filtered by a policy.

### What now carries the isolation guarantee

A caller cannot read or write another tenant's job rows because it cannot touch
`river_job` at all, by three layers that do not depend on RLS: (a) the closed
typed-operations surface in `s0typedops` has no operation that names a River
table, accepts SQL text or returns the underlying transaction, and the River
insert runs only from private infrastructure code; (b) the caller role has no
USAGE on the River schema and no privilege on any River table, so the same SQL
issued under the caller role is refused with SQLSTATE 42501; (c) the only role
with privileges on River's tables is the trusted jobs role, which by design sees
all tenants because fetch, completion, rescue and cleanup must.

Say plainly what RLS did and did not protect on these tables. Only the worker
fixture gave the caller role anything on River tables (a SELECT on `river_job`
filtered by the tenant setting, plus schema USAGE); that grant is removed, since
no caller needs it, and the caller role now has no privilege at all. With no
privilege, a policy for it protects nothing that the grants do not already
refuse. The jobs role needs every row, so its policy was
`USING (true)` in the worker and typed-operations fixtures: it filtered nothing.
The only place a policy constrained the jobs role was the atomicity fixture
(`s0txprobe`), where it was keyed to the same writable transaction setting whose
rewriting is the recorded failed gate.

### What is given up compared with forced RLS

- Defence in depth against a caller that obtains raw SQL **as its own caller
  role**: none was provided by RLS on River tables and none is lost, since the
  grants deny everything first. Isolation of the caller-owned tables is not
  affected (below).
- In fixtures that retain the known-unsafe SET membership, a caller with raw SQL
  can `SET ROLE` to the jobs role and then read and write **all** tenants' job
  rows. Before this change, in `s0txprobe` that additionally required rewriting
  the writable scope setting (which is itself a recorded escape); in the worker
  and typed-operations fixtures it was already possible. This is one fewer step
  in one fixture, not a new class of escape, and it is the same weakness as the
  failed gate: a real design must not let callers enter the jobs role at all.
- In `s0txprobe` the policy `WITH CHECK` on `river_job` (tenant and writer
  transaction equal to the current transaction's) no longer rejects a direct
  insert by the jobs role with a forged tenant column. The integrity of those
  columns now rests on the defaults and on only trusted infrastructure running
  the insert.
- A bug in trusted infrastructure that makes it query another tenant's job row
  as the jobs role would not have been caught by RLS either (the jobs policy
  has to admit every row).

### What stays under row-level security

Nothing outside River's five tables changes. The caller-owned result table
(`result`), the typed-operations records, outbox and audit tables, the
atomicity fixture's state, outbox and audit tables and the SQL-boundary probe's
tables keep ENABLE and FORCE ROW LEVEL SECURITY with their tenant/scope policies,
and the existing tests that assert caller isolation there stay.

### Alternatives rejected

- **Grant BYPASSRLS to the jobs role.** It is a role attribute: it applies to
  every table the role is or will be granted, in the whole cluster, and silently
  disables any policy on them. A later grant to the jobs role on a caller-owned
  table would then expose that table. The fixtures assert NOBYPASSRLS on every
  role and that invariant is kept.
- **Make the jobs role the owner of River's tables, RLS not forced.** It fixes the
  plan too, but turns the runtime role into one that can ALTER, DROP, TRUNCATE,
  GRANT, create triggers and turn RLS back on, and merges the migration and
  runtime roles. The login can SET to it in these fixtures. It buys nothing over
  disabling RLS.
- **Keep policies and make them cheaper.** The policy shape does not matter (see
  the finding): any policy applying to the role disables use of non-leakproof
  statistics.
- **Per-role `enable_seqscan = off`.** Diagnostic only; it hides the symptom and
  disables the index-less plan for legitimate scans.

### Bounded retention

River's cleaner is a maintenance service of the client that holds leadership
(elected through `river_leader`); it is not run by a client that is not leader.
At the pinned v0.51.2 its options on `river.Config` are
`CompletedJobRetentionPeriod`, `CancelledJobRetentionPeriod` and
`DiscardedJobRetentionPeriod` (defaults 24 h, 24 h, 7 d; `-1` disables deletion of
that state; the fixtures so far set `-1`) and `JobCleanerTimeout` (default 30 s).
The pass interval is fixed at 30 s, and each pass deletes in batches of 10,000
rows ordered by `id`, with a short pause between full batches. A job becomes
eligible once `finalized_at` is older than its state's retention.

Harness rule, so that observation cannot race the cleaner: the restart gate reads
durable completion by SQL (a poll joining `river_job` and the caller-owned
`result` table, then a final sweep per cycle). The cleaner deletes only
`river_job` rows; it never touches `result`. The probe app takes the retention
period as a parameter and **refuses a value shorter than twice the upper bound of
one cycle** (warm-up, window, completion budgets and the sweep), so a cycle's final
sweep always precedes any deletion of that cycle's jobs. As a second line, an
acknowledged ID whose result row exists but whose job row is gone at the final
sweep is reported as its own failed bound; it is never counted as completed.

The results of the rerun at frozen scale and the numbers measured on the
development host will be appended here as a separate dated section.

## Effect of the jobs-role and retention change on the development host, 2026-11-19

Status: **observations on a busy shared development host, reduced scale; not the
frozen-scale rerun.** PostgreSQL 16.15, fsync and synchronous_commit on, two runs
sharing the host at times, so absolute latencies mean nothing here and are not
used. The change is implemented in the probe fixtures as decided above (River's
tables owned by the migration owner, RLS disabled, no policies, jobs-only
privileges, no caller privilege; four tests rewritten, see the package READMEs)
and the restart gate takes River's retention as a parameter
(`S0_WORKER_RESTART_FULL_JOB_RETENTION_SECONDS`, default 900 s, refused below
twice the cycle bound, 0 for off).

**Plan.** Same table, same role, same statement (River's fetch shape, 150,006
retained rows, freshly analyzed), only the row-level security switched:

| Fixture design | Plan | Cost | Buffers | Execution |
|---|---|---|---|---|
| No RLS on `river_job` (now) | Index scan, `river_job_prioritized_fetching_index` | 0.42..6.20 | 3 | 0.08 ms |
| Forced RLS plus `provider_jobs USING (true)` (before) | Seq Scan, Sort, LockRows | 9,424.70 | 3,183 | 104.7 ms |

Inside the gate, the plan printed as the jobs role was the fetching index at
150,000 rows (cost 0.42..8.46, 3 buffers, 0.08 ms) and again after twelve cycles at
179,963 rows (0.15 ms).

**Scans and completion, retention off, 150,000 pre-grown rows, 12 pool cycles at
100 ids/s** (5 s warm-up, 20 s window, 100 recovery IDs, no
`DIAG_JOBS_NO_SEQSCAN`): every cycle durably completed 2,500 of 2,500
acknowledged IDs (30,000 of 30,000), every frozen bound of every cycle passed at
this scale (ack-to-completion at most 0.56 s, recovery 0.10 to 0.41 s), `river_job`
grew from 150,000 to 179,963 rows. Sequential scans of `river_job` per cycle:
one in cycle 1 reading exactly 150,000 tuples (the observer's own seeding scan of
the pre-grown rows, whose counters arrived after the baseline read), and **0 scans,
0 tuples in cycles 2 to 12**; totals at the end: 16 scans, 150,000 tuples read,
196,516 index scans. The first frozen run had read 97,389 scans and 6.59 billion
tuples. Below roughly 7,500 rows the planner does choose a sequential scan for the
fetch on its own (small table); from about 10,000 rows it did not.

**Retention on, 30 pool cycles, retention 240 s (the minimum for these shortened
cycles is 230 s):** 75,000 of 75,000 acknowledged IDs completed, every bound passed,
`jobs_removed_before_final_sweep` was 0 in every cycle. Live rows rose to about
24,600 by cycle 10 and stayed between about 22,200 and 24,800 to cycle 30 (rows written
would otherwise be 75,000). Table plus index size went from 3.2 MB to 22.7 MB by
cycle 10 and then crept to 34.3 MB by cycle 30 while live rows stayed flat: dead
tuples wait for autovacuum (13 runs, 9,075 dead tuples at the end), so the size is
bounded by churn, not by history, but it is not constant over this short window.
River's fetch averaged 0.27 ms and 25 buffers per call over 33,888 calls (max
5.5 ms); the old design needed about 4,900 buffers per call at 160,000 rows. River's
own delete (`JobDeleteBefore`) averaged 16.7 ms and 4,803 buffers per call and reads
the retained table sequentially, about once per 30 s pass (1 sequential scan of
about 26,200 tuples per cycle from cycle 10): bounded by the retained size, so at
the 900 s default roughly 90,000 rows per pass.

**Retention on over pre-grown history** (150,000 rows finished an hour earlier,
retention 240 s, 6 cycles): the cleaner removed most of them in the first
cycle (live rows 12,441 after cycle 1, 4,906 after cycle 2), 15,000 of 15,000 IDs
completed and every bound passed; file size stayed at 66 to 72 MB (space is reused,
not returned).

**Not shown.** No frozen-scale run (30 + 30 + 30 + 10 cycles, 30 s warm-up, 120 s
window, 1,000 recovery IDs, about 4.3 hours) was made here; the restart gate
remains failed at frozen scale until it is rerun on an idle host with this change.
The SQL-boundary probe (`s0probe`, `TestSQLViewBoundary`) was not touched and
still fails for its recorded reason (a caller's `set_config` of the scope followed by
a scoped read of the other scope's row under FORCE RLS on its own tables, along with
the role-switch escapes). The role/GUC mechanism, the transaction-authority
decision and the S0 status are unchanged by this section.

## Restart gate rerun at frozen scale, 2026-11-19

With River's tables out of row-level security and finished jobs retained for 900
seconds, the [full restart gate](../../test/spike/s0/results/restart-full-frozen-scale/invocation.json)
was rerun on the idle dedicated host and met every frozen bound in all 100
cycles: 30 pool, 30 client and 30 listener recreations and 10 combined, with
15,000 IDs per cycle at 100 per second. Every acknowledged ID completed, with no
duplicate logical result and no River retry; the longest acknowledgement to
durable completion was about one second and the longest recovery about 0.4
seconds; the job table held at about 93,000 live rows.

This is candidate evidence. It does not change the status of the raw-SQL
authority gate, which remains failed, and acceptance stays with the owner.
