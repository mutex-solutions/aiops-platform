# Typed-operation spike: bounded correctness follow-up

Status: implementation/verification follow-up within the owner-approved typed-
operation investigation; **not adoption or canonical Tx amendment**. Original
raw-SQL failure, first source-identity failure and qualified limited Put/Get
results remain unchanged. This record precedes the follow-up implementation/tests.

## 1. Commit versus revocation

Existing RevokeEntry/RevokeScope/Dispose close future admission only. Preserve
those signatures and explicit meaning; do not retroactively call their return
a drain acknowledgment.

Add a host-owned active-Run registry. Admission and final commit arbitration use
the same short host lock as revocation; no provider/database callback runs under
that lock. A Run must win commit admission while its exact entry/scope/view is
valid. Once revocation wins first, that Run must roll back rather than commit.
If commit admission wins first, database outcome is allowed to resolve, but a
separate host quiescence wait cannot return until that Run and its cleanup have
actually returned. Commit error/unknown outcome is retained, never described as
confirmed rollback just because rollback was attempted.

Provide explicit trusted-host waits for revoked entry, scope or view quiescence.
They are separate from revocation requests. Requests can be made in a domain
callback but must not synchronously wait for that callback's own Run. No goroutine
ID detector or lock held across callbacks/database calls is used. Context timeout
of a quiescence wait reports incomplete work; it is not drain completion.

Tests use barriers, not sleep ordering, for both arbitration winners and all
three revocation forms, including repeated waits, canceled waits, rollback error,
callback panic and simultaneous unrelated views. No caller gets registry/issuer/
raw-driver access.

## 2. Begin cleanup and reused backend controls

If private role/scope setup fails, roll back with a fresh bounded cleanup context
independent of canceled work. Preserve primary and unexpected cleanup errors.
The typed wrapper cannot install its ordinary Run cleanup before begin succeeds,
so provider begin-failure cleanup must be explicit and observable.

In the isolated PostgreSQL fixture, test a real SQL error and cancellation at
controlled private provider boundaries, then verify the reused connection is the
runtime login with cleared scope and no open transaction before a successful
own-entry operation. These are not new caller SQL capabilities. No denied or
excluded capability operation is executed.

## 3. Catalog and teardown

Retain the exact fixed table/query grammar and trusted catalog boundary. Verify
the fixture's reviewed columns, policy expressions, ownership/grants and narrow
participant functions before opening a view. Any broader extensible catalog,
caller DDL or production migration invalidation protocol remains unsupported.
Do not claim that a validated schema string is itself catalog admission.

Parent teardown of the previous three disposable containers is recorded in
`test/spike/s0/results/typedops-fixture-teardown.json`. New runs must again record
source/executable/protocol identities, sandbox resource limits, all expected
outcomes and actual remaining-resource teardown.

Only after this bounded change is independently reviewed and sandbox-validated
can the limited grammar be presented for explicit owner adoption. All frozen
numerical gates, same-transaction River participation and stop-before-S1 remain.
