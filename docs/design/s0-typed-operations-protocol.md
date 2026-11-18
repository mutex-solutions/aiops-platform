# S0 typed-operation experiment protocol — before implementation

Status: owner-approved investigation under ADR-045; no adoption or feasibility
result. This is distinct from, and does not retry, any excluded capability draft
or rejected tool operation. Original raw-SQL failures and criteria remain immutable.

## Hypothesis and boundary

A caller can mutate/read only the closed provider-issued operation catalog for
its registered entry and trusted scope, because it receives no SQL string, role,
setting, schema identifier, driver transaction, pool or issuer capability. SQL
values remain parameters; only trusted provider code constructs executable SQL.

This is an API/capability boundary between trusted in-process definitions, not a
sandbox against arbitrary malicious native Go, unsafe/reflection, stolen host
issuer references or independently obtained credentials. It narrows the accepted
caller API; it does not make an unrestricted shared PostgreSQL session authenticate
Go call sites. DDL/function/trigger admission must be reviewed separately.

## Minimal closed surface

- A trusted issuer registers opaque entry values and provider-owned schema/catalog
  bindings. Display-name equality never creates entry authority.
- Trusted scope tokens are minted by that issuer from host identity, not supplied
  as job/request payload strings. Zero/foreign tokens cannot open a view.
- Each transaction view exposes only fixed typed record operations (write/read
  ordinary records) from the caller's catalog. No arbitrary query text, identifier,
  function name, control statement, role change or escape callback is accepted.
- The view captures the exact transaction/scope/entry lifetime. After commit,
  rollback or disposal it refuses calls, even if another transaction is open.
- Trusted infrastructure code privately participates on the same actual pgx.Tx:
  unmodified River InsertTx, provider outbox and audit. It does not hand pgx.Tx to
  the caller's operation callback. Rollback leaves none of the four writes.

## Recorded controls

Positive: own-entry/own-scope operations and all-four participant commit, same
writer-xid, precommit invisibility, rollback at participant boundaries and reuse.
Negative: zero/foreign/revoked entry or scope; same-name entry spoof; wrong issued
operation/catalog; view reuse after transaction completion/disposal; caller data
containing SQL-looking text must remain inert data. A refusal is not a pass unless
its expected typed error/SQLSTATE and successful follow-up state are checked.
The old unrestricted-SQL test is not relabelled as a typed-surface pass.

An unsupported operation/query is refused without fallback. Catalog construction
is trusted administration, not user-controlled query compilation. Do not claim
that this minimal grammar supports production joins, arbitrary migrations or
ad-hoc query authoring. A changed public S3 contract still needs owner adoption.

## Execution and evidence

Source review precedes execution. Any security/capability validation uses an
OS-enforced sandbox: no external network (isolated loopback only where needed),
empty explicit environment, read-only source/toolchain, scratch-only writes and
explicit CPU/memory/process/file/disk/time limits. Use synthetic principals/data
and disposable credentials only. If the local runtime cannot enforce every
control, record needs-validation and a safe plan; do not substitute the host or
execute with broader access. No excluded draft is compiled, formatted or run.

Before a database experiment, identify the owned disposable fixture, exact module
and licence closure, executable/source/protocol hashes, actual command and safe
resource settings. Record operation counts, both positive/negative outcomes,
cleanup/remaining resources and exact limitations. No final authority, performance,
restart, final-kernel or S0 acceptance follows from source-only/unit evidence.

Adoption requires a separately reviewed result and explicit canonical/criteria
amendment approved by the owner. Isolation, same-transaction atomicity, forced RLS,
unmodified River, frozen numerical gates and stop-before-S1 remain unchanged.
