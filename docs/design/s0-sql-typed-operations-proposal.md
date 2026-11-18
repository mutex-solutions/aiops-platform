# S0 SQL authority: typed-operation surface proposal

Status: **bounded spike owner-approved; implementation/feasibility and adoption pending**.
The owner explicitly selected “Duyệt spike typed operations, giữ nguyên guarantee”
in the S0 confirmation. This approves investigation of the changed caller Tx
surface, not canonical adoption, a passing authority result or a reduced guarantee.
It neither weakens the failed authority gate nor retries the tool-rejected
capability operation/draft.

## Preserved guarantees

- One caller-owned database transaction commits state, unmodified River InsertTx,
  outbox and audit, or rolls all of them back.
- Runtime principals are nonowners, NOSUPERUSER/NOBYPASSRLS, with forced RLS and
  separate migration ownership.
- Plugin identity is attached by the trusted module-entry/context binding;
  scoped identity comes from the trusted handle, not job args/request payload.
- Only infrastructure participants receive the privileged driver view. Domain
  plugins never receive raw pgx, pools, session authority or cross-domain Tx.
- The original failing role/GUC probe remains failing history. No sample count,
  denial-control requirement, recovery budget or acceptance gate is waived.

## Changed contract proposed for investigation

The caller-facing Tx would no longer expose unrestricted SQL-string Exec/Query.
Instead, it would accept a closed set of typed operations or provider-issued
opaque query capabilities tied to the caller's entry, schema and scope. The SQL
provider would generate the executable statements and bind values as parameters.
Caller values cannot choose a role, setting, SQL statement, function or schema.
An opaque operation's construction/admission is trusted authority, not a caller
claim or a mutable query ID. A SQL-text blacklist is not the proposed boundary.

Privileged transaction participants would keep their private raw-driver path,
allowing unmodified River InsertTx and narrowly owned outbox/audit operations on
the **same** transaction. Trusted role switching would remain an implementation
mechanism, not proof that arbitrary caller SQL cannot do the same thing: caller
SQL would no longer be an available capability.

This is a real author/API restriction. Free-form ad-hoc queries would require a
reviewed definition/catalog extension or a separately scoped privileged provider;
they cannot silently fall back to raw SQL. Full query syntax, joins, migrations,
function/trigger admission and the S3 public definition remain open until tested.
No promise that arbitrary plugin-owned DDL is safe follows from removing Exec.

## Bounded spike, not adoption

1. Define the minimal closed operation/query-capability grammar, trusted issuer
   and entry/scope binding in a preexecution protocol. Explicitly reject any
   operation outside that grammar; do not add a string escape hatch.
2. Exercise ordinary state writes and unmodified same-transaction InsertTx,
   outbox/audit commit/rollback on the owned synthetic PostgreSQL fixture.
3. Verify wrong-entry, wrong-schema, missing/replaced scope, reuse after disposal/
   transaction end and malformed operation refusal with successful follow-up
   controls. The protocol must state the new API trust boundary accurately;
   a different test is not retroactively a passing original unrestricted-SQL test.
4. Review executable SQL, argument/data separation, immutable capability identity,
   provider-owned metadata paths, schema/migration authority and concurrency before
   claiming feasibility. Do not execute excluded drafts or route denied operations
   through another tool/agent.
5. If feasible, present results and an explicit canonical/criteria amendment for
   **adoption**, including restrictions and remaining limits. Only then run the
   protected performance/restart arms and final-port acceptance on that mechanism.

Approval to investigate this proposal is not approval to select it, reduce
isolation, treat in-process plugins as newly sandboxed, change the River pin,
replace atomic InsertTx with an outbox bridge, or start S1. If the restricted
surface is unacceptable, ADR-045 remains unresolved and independent S0 work can
continue without claiming a final authority mechanism.

> Note, 2026-11-19: "forced RLS" above applies to caller-owned and provider data tables. Per the owner decision of 2026-11-18 recorded in ADR-045, River's own five tables carry no RLS and no policy: only the trusted jobs role holds privileges on them and caller roles hold none. Isolation of job data rests on the closed typed surface and those grants.
