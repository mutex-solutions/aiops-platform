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
