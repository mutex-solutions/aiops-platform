# S0 SQL authority follow-up protocol

Date: 2026-11-12. Status: **frozen before the follow-up experiment**.
Owner direction: ADR-045 option A, continue a bounded spike without weakening the
accepted guarantees or changing the final transaction contract.

This supplements, but does not edit or replace, the [original frozen criteria](s0-spike-criteria.md)
SHA-256 `628bd6ec24e5983bfbe9fcdada096eaa84db3e95227f11e8fd5a7fa33a26f19c`.
The original failed records remain failures. No code change to the kernel or
product SQL definition is authorized by this diagnostic protocol.

## Question

Can built-in PostgreSQL session/parameter privileges make an ordinary role switch
provider-only or make an ordinary custom scope setting caller-immutable, while
unrestricted caller SQL and the provider use the same transaction/connection?

## Environment and invariants

Use the same digest-pinned PostgreSQL 16.15 dedicated fixture, nonowner runtime
login with NOSUPERUSER/NOBYPASSRLS/NOINHERIT, FORCE RLS, eight-connection maximum,
and actual fsync/synchronous_commit on. Bootstrap may create new random roles,
schemas, functions and grants only in that dedicated fixture. Runtime statements
must not use owner/superuser credentials. Each adversarial transaction rolls back.
No other container, database or workload is touched.

This is a bounded correctness diagnostic, not a performance test. Guard each test
with a 30-second context and a 40-second process timeout. Record original criteria
hash, this protocol hash, source/executable hash, complete checks and SQLSTATE.
A setup error, unexpected SQLSTATE, connection error or timeout is inconclusive,
not successful enforcement. Green diagnostic execution means only that the
stated mechanism limitations were reproduced; it never passes the original
SQL-boundary gate.

## Hypotheses to falsify

1. **REVOKE SET freezes the scope placeholder.** Revoke parameter SET from PUBLIC,
   caller and runtime for the custom setting. Then use actual caller SQL to call
   set_config to another scope and SELECT its known seeded row under forced RLS.
   If the setting changes and foreign row becomes visible, the hypothesis is
   falsified. Preserve the original hard gate failure. Do not claim that session
   rather than LOCAL setting, or parameter ACL alone, is a fix.
2. **SECURITY DEFINER can make ordinary SET ROLE provider-only.** Create a narrowly
   granted definer helper with safe fixed search_path attempting the jobs role
   switch. A specific 42501 refusal falsifies this wrapper hypothesis; do not
   substitute an unrelated runtime/setup failure or elevate to superuser.
3. **Removing jobs SET membership preserves the provider switch.** Revoke that
   membership, and attempt exactly the same ordinary SET LOCAL ROLE from caller
   SQL and from the trusted direct transaction call site. Specific 42501 denials
   in both cases show that the database does not distinguish the Go call sites.
   A caller-only login may improve one fence but is not an atomic InsertTx fix
   until its provider authority is separately proved.

No River insertion is needed to establish the privilege-identity limitation;
therefore this experiment must not claim River atomicity, participants, upgrade
behavior, workers, restart or performance were tested. A different capability
mechanism remains a separate proposed experiment and requires approval if it
changes canonical role topology, Tx surface or identity mechanism.

## Interpretation boundary

If all three limitations reproduce, conclude only that this set of ordinary
built-in mechanisms cannot supply the desired provider-only role switch on one
unrestricted-SQL session. Do not claim all PostgreSQL extensions, brokers,
capability designs or transaction contracts are impossible. Present the remaining
choices and their effect on the accepted contract to the owner; do not silently
choose a weaker SQL surface, blacklist, RLS disablement, owner role or non-atomic
bridge.
