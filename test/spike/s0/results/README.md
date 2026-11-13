# S0 retained results

Frozen criteria SHA-256:
`628bd6ec24e5983bfbe9fcdada096eaa84db3e95227f11e8fd5a7fa33a26f19c`.
No failure record is deleted or converted to pass.

## Current result boundaries

- [Frozen full property workload](property-full-20261112T203000Z/): both real
  candidates passed 1,000 seeds x 1,000 operations under race, exit 0. This is
  source `2a2dc5ce...`, before later native root/observer/LIFO/Batch changes;
  it is not acceptance evidence for those changed statements or a final SDK.
- [Worker delivery/revocation](worker-functional-corrected.json): all four selected
  real-background-worker cases passed under race. The prior
  [42P01 setup failure](worker-functional-first.log) remains retained. Functional
  delivery does not repair the known-unsafe role/GUC SQL boundary.
- [Initial 10k diagnostic failure](scale-diagnostic-initial.md): the first
  graph-lock chain exceeded its unchanged operation guard and left real work.
  Later same-process samples were contaminated, not clean performance results.
  A UID-targeted dependency wake fix was then validated in fresh isolated
  processes; one-trial observations do not meet full performance sampling gates.
- ADR-035's separate causal comparison is owner-approved, not automatically
  validated by a checkpoint assertion. Raw traces and strict results are retained.

- [Reviewed semantic integration](semantic-reviewed-ownership-fixed/invocation.json):
  source-fenced strict D31–D36 hook traces and causal D02/D10/D12/D16 matched on
  both candidates under race after review fixes. Earlier
  [typed-error receipt limitations](semantic-integration-typed-errors/qualification-review.md)
  remain retained; the new run does not rewrite historical failures.

Both full-property passes above predate the latest native Effect/config/ACK
changes and are not current-source property acceptance. Affected full reruns
remain required. None of these rows declares complete S0 or selects a final
concurrency core.

## SQL caller-boundary diagnostic

- `20261112T063704Z-sql-boundary.json` and its log: first diagnostic invocation,
  process exit 1. **Source-identity record unqualified:** an unfinished unrelated
  test draft entered the live source-directory hash after compilation but was not
  in that executable. The observed SQL outcomes are retained, but this run alone
  is not the qualified acceptance evidence. The draft was subsequently parked
  outside the executable source package and no code writer remained active.
- `20261112T064106Z-sql-boundary-qualified.json` and its log: stable three-file
  source and rebuilt executable verified against the report; process exit **1**,
  classification **fail**. Source SHA-256
  `89a9aa34cbb6e176e01ab3ba484c04cb7087b5683299cc5279288ff3d262120e`;
  executable SHA-256
  `d0ad9714e1434810090ef1f003f6946b18c12b74cc2d7e7aa981b4f0184c5afe`.

- `20261112T064330Z-sql-boundary-race.json` and its log: the same stable source
  under the race detector, process exit **1** for the same boundary assertions.
  No race-detector diagnostic was emitted; the test is still **failed**, not a
  clean acceptance run. Focused `go vet` on the probe and concurrency primitives
  completed without diagnostics before this invocation.

- `20261112T064938Z-sql-boundary-controls-race.json` and its log: additional
  unchanged-scope, missing-scope, rollback and reused-backend controls all passed;
  the same five role/scope escapes still make the test **fail**, process exit 1,
  with no race-detector diagnostic. Source SHA-256
  `653f0f7fad9c153fa6619826390da752e573c5716f4d37006d3f24bbd7cdc2ad`;
  executable SHA-256
  `79c9dc19e8eeb8a46ba49f6995da38b5c242b0aa4852cbdd96d9f922822d5523`.
  This is the strongest current boundary record; it does not accept the mechanism.

The runtime was a fresh nonowner NOSUPERUSER/NOBYPASSRLS login; scoped tables used
FORCE RLS; live fsync and synchronous_commit were on. Raw caller transaction SQL
could change to the jobs role, reset role, and alter the scope setting to read or
write another synthetic scope. Direct foreign outbox-schema insertion was denied
with SQLSTATE 42501. Every adversarial transaction was rolled back.

This is a failed SQL-boundary fixture gate, not proof that River InsertTx failed
or that cross-plugin atomic commit is impossible. River insertion, participants,
restart/load and role-switch cost have **not yet been measured**. The fixture
uses the raw-Exec/GUC mechanism being evaluated; the accepted trust boundary and
possible stronger designs require an owner-reviewed ADR response before dependent
implementation continues. There is no approved architecture change in this result.

## Approved follow-up: ordinary PostgreSQL authority mechanisms

The owner authorized a bounded continuation preserving the original guarantees.
The protocol [was frozen separately](../../../../docs/design/s0-sql-authority-followup.md),
SHA-256 `bea36baba4d1e646d05cf1b8ddd5869e9398cfabc0438298eda7ecd477d79678`.

- `20261112T072420Z-authority-matrix.log` and
  `20261112T072528Z-authority-matrix.log` are inconclusive preflight failures, not
  database-hypothesis results. Restarting the owned Docker fixture changed its
  automatically assigned host port; the private state still named the previous
  port. No attack statement ran. The same owned container identity/health and
  current endpoint were verified, and private state corrected without exposure.
- `20261112T072707Z-authority-matrix.json` and its log reproduce all three
  mechanism limits under the race detector; the diagnostic runner exits 0 with
  classification `incompatible-mechanisms-reproduced`, **not a SQL gate pass**.
  REVOKE SET does not prevent an ordinary custom USERSET scope change; a definer
  helper cannot execute SET ROLE (42501); removing jobs SET membership denies
  the identical role switch to caller and trusted direct transaction sites
  (both 42501). This confirms the built-in mechanisms do not distinguish the
  two Go call sites on the same SQL session.

The original SQL boundary failure remains unwaived. No generic PostgreSQL
capability design, extension, broker, River insertion or atomicity has thereby
been proven impossible. A different authority mechanism still needs design and
owner approval if it changes the accepted transaction/role contract.

## Ordinary transaction atomicity — independent of authority

`20261112T174224Z-ordinary-atomicity.json` and its verbatim log record an actual
race-instrumented run of the separate `s0txprobe` fixture: process exit 0, all
21 selected ordinary cases passed. The fixed report command is labelled as
canonical, not captured argv; the parent record adds the actual invocation.

All four writes (caller state, unmodified River InsertTx, outbox and audit) shared
one caller transaction and writer-xid stamp. Cases cover two scoped commits,
precommit invisibility, rollback and SQL errors after each participant boundary,
failed role restoration on an aborted transaction, explicit rollback after
cancellation, own physical-connection loss before commit, missing scope, context
filtering, direct foreign-schema write denial and pool reuse. Exactly two
committed records remained in each participant table. Live durability was on.

This fixture intentionally uses the **original known-unsafe role/GUC boundary**;
it tests ordinary functional atomicity, not a new capability or authority repair.
The original SQL gate remains FAIL. No worker delivery, 500 Tx/s cost, background
identity, restart/load, ambiguous commit-ack loss, or full S0 pass is claimed.
Source and executable hashes plus explicit limitations are in the record.

Credentials and database endpoints are deliberately absent. Reproduction uses
private environment variables documented by the probe, not a committed DSN.
