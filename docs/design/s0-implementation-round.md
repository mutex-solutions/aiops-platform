# S0 resumed implementation — work in progress

Scope remains the approved S0 only. This note describes current work, not a new
acceptance threshold, selected kernel architecture or completed release. The
original criteria and historical experiment records are not edited by this work.
No CI/CD or public visibility change is being introduced.

## Native lifecycle evidence instead of adapter guesses

The two experimental schedulers are gaining optional native inspection of the
actual reserved provision store and desired epoch. Desired revision/bindings
are separate from the activation's retained committed bindings during teardown.
Disposal-request liveness is separate from cleanup completion. Inspection must
not invoke availability predicates, marshal service values or call plugin error
methods while owning graph state. Reserved, ACTIVE and retiring provisions retain
their actual value and UID, in creation order, rather than being reconstructed
from script expectations.

The replay adapter consumes that native observation. Raw JSON needs order is
preserved before a map loses it. A missing observation API is unsupported;
complete observations that differ from the oracle are a **semantic mismatch**,
not an unsupported API or a pass. All actual state/service events remain in the
ordered comparison. No eager scheduling difference is erased by sorting. A strict
comparison run
of the currently supported ordinary lifecycle subset still exits 1: sibling
callback scheduling and some teardown event order differ from the pinned
reference. Several isolated cases now match complete traces, including ordinary
configuration cases, but no all-corpus or repeatable full parity is claimed.

The composed runner now uses the real common metadata/lifecycle frontend for
isolation, caller-bound views, intercepts and native children. Its checkpoint
assertions run on both candidates; its strict complete-trace mode also records
actual mismatches rather than treating successful checkpoints as equality.
Ordinary immutable context values and consumer-specific availability are now
wired through the actual metadata tree and native Notify/Check path, including
committed reads during availability-driven teardown. Root-level reference
provisions now use an actual lazy ID-0 native host activation, without synthetic
user fibers or startup trace rows. Trusted host metadata has a root-local opaque
entry and no bound-key bypass. Unchecked plugin reads remain unsupported because
the accepted typed Go contract does not carry them.

## Configuration and returned cleanup

A common configuration cell implements source-derived raw/effective identity,
candidate resolution, persistence notification, update veto/continuation and
superseded publication guards. Actual workers prepare configuration before Apply;
callbacks run outside graph ownership. Native config update retains new raw input
before the update hook, even on rejection, while invalid resolution aborts before
retention. A veto may publish a new effective value without a runtime revision.
This is not JSON schema, a loader or volatile-field implementation.

The old replay cleanup approximation pre-registered a slot before body effects.
That put its cleanup before Provide in the ledger, so strict Go LIFO could remove
a provision before the fixture's source-returned cleanup. The prototype now has
an explicit native Apply-returned-disposer fixture entry. It is appended after
body effects on success, including a successful self-disposal request. No fake
cleanup mark or fabricated retained provision is used to mask the difference.
This fixture form does not choose a new final author-facing SDK API.

The first native config integration accidentally joined lifecycle completion in
an `await:false` script operation, blocking its teardown-release step. A separate
native request handle now performs the real resolve/commit/target-change prefix
synchronously and defers only completion. A shared two-core regression verifies
latest raw input is retained while old cleanup is gated, then only that latest
input applies after release. The original timeout is not counted as a pass.

Pending disposal also preserves the source's UNLOADING transition before DISPOSED,
even without an executed Apply body. It does not invent plugin cleanup effects.

## Independent primitives and transaction diagnostic

The hook prototype tests copy-on-write listener snapshots, explicit audience,
real runtime leases/effect disposal, stages, event dispatch and waterfall rules.
Raw listener errors and owner-level error containment are kept distinct. Async
waterfall, authoritative runtime-to-metadata binding and access-class integration
remain explicit work rather than presumed support.

A separate ordinary transaction diagnostic uses unmodified River migrations and
InsertTx for four-participant commit/rollback tests with a nonowner runtime and
FORCE RLS. It deliberately retains the original known-failed role/GUC mechanism
and cannot pass the SQL authority gate. It was authored and compiled with exact
linked-module and nested upstream notice review, then the parent ran the ordinary functional cases on the owned
fixture. All 21 selected cases passed under the race detector; the
[retained result](../../test/spike/s0/results/20261112T174224Z-ordinary-atomicity.json)
records source/executable identity, durability, limits and actual invocation.
No SQL authority, background delivery, restart or latency acceptance is claimed.
Exact package and nested source licence checks precede execution.

The previously tool-rejected capability operation is not retried or routed
through these ordinary tests. Its draft remains local and unexecuted. Functional
atomicity evidence already obtained does not waive the failed authority gate.

## Frozen candidate lifecycle property workload — pass

The full candidate property workload is fixed at 1,000 seeds x 1,000 operations
per backend under the race detector and GOMAXPROCS=8. Its first invocation reached
the outer 30-minute Go package alarm while still evaluating graph-lock model
assertions; it exited 1 without a completed end-source fence or full summary.
No prior partial progress is promoted to acceptance. The incomplete run is
[preserved](../../test/spike/s0/properties/evidence/initial-full-timeout.md).

The identical frozen source/workload was restarted with only a longer 90-minute
outer process watchdog. The native 2/5/2-second lifecycle deadlines, per-operation
assertions, seed counts and criteria remain unchanged. A provider bootstrap launch
failure before test execution contributed no result. A later invocation was
[interrupted by SIGTERM](../../test/spike/s0/properties/evidence/retry-full-cancelled.md),
not a completed result. The owner subsequently confirmed that the Stop action
was accidental and explicitly requested resumption. The fresh invocation has now
completed **PASS, exit 0**, for both real candidates: 1,000 seeds x 1,000 operations
each under `-race`, GOMAXPROCS=8, source digest
`2a2dc5ce8fe8cd609cbc0d7ae7a0322c828b6455532260f44e5c1f1a9aee1471`.
Its full [summary/log/invocation](../../test/spike/s0/results/property-full-20261112T203000Z/)
are retained without replacing the original output. Total test time was 2383.64s;
reconciler 1292.68s and graph-lock 1090.83s. This is correctness evidence, not a
performance comparison. End-source validation and final teardown completed.
Previous timeout/cancellation records remain incomplete. The result belongs to
this exact experimental closure/workload, not future core changes, the final SDK
port, full differential/scale/SQL or complete S0 acceptance.

## Subsequent native work and independent diagnostics

Both candidates now enqueue every actual native ledger event into one shared
off-owner observer worker. Registration authenticates the native runtime identity,
uses real setup/callback leases and lifecycle removal, and preserves exact event
envelopes/order. Await is explicit, not hidden in Settle. Cooperative observer
cancellation follows the activation's real Std context; queue shutdown follows
actual lifecycle drain. Queue memory remains unbounded in this prototype, so no
complete retention/workload-budget claim follows from functional G5 tests.

The native root joins all ordinary user cleanup during explicit Close. Provisions
unwind at real strict-LIFO slots, including successful Apply-returned cleanup;
Owns is cleared at actual reservation removal. A diamond regression also fixed
reconciler dependency links being released before every consumer's stable state.

A data-only native Batch validates the whole prefix before mutation, commits all
desired targets and invalidates the old provider closure before admitting cold
loads/checks. Shared race-tested conformance covers invalid suffix rollback,
revision coalescing, copied declarations, actual new UID binding and held Apply.
Configuration-callback batching remains explicitly outside this prototype.

The first 10,000-fiber single-trial diagnostic found graph-lock dependency wake
herding and retained real work; all original failures are kept. Activation-local,
UID-checked wakeups then allowed all ten topology/candidate combinations to finish
in separate fresh processes with zero reported lifecycle/resource errors. These
are diagnostic observations only: no 1,000-sample/10-repetition or final-parity
performance acceptance is inferred. See [the failure record](../../test/spike/s0/results/scale-diagnostic-initial.md).

The corrected ordinary worker fixture passed all four actual race-instrumented
scope-data, snooze, concurrent-partial-replica and revocation cases. Its first
migration-inventory setup failure remains retained. The known-unsafe role/GUC
SQL authority boundary remains failed; no worker test repairs that gate.

The root/observer/LIFO/Batch closure completed its fresh full property workload
**PASS, exit 0**, at source digest
`a3ee6b9546ce7ba9bdb45cff7234aff4d08e58bd816d0270a48e631972eb7679`:
1,000 seeds x 1,000 operations per candidate under race, 2481.19s total. Its
[summary/log/invocation](../../test/spike/s0/results/property-full-native-batch-a3ee6b95/)
are separate from the earlier closure's result. This remains experimental-candidate
correctness evidence, not final SDK acceptance or a performance comparison.
The `2a2dc5ce…` and `a3ee6b95…` full property PASS records remain separate
historical evidence. Both predate the latest native Effect/helper and core
changes; neither is current-source property acceptance. The affected current
closure requires its own full property rerun.

An actual race-instrumented [restart smoke](../../test/spike/s0/results/restart-client-smoke.json)
passed one same-PID client recreation plus postcommit response-loss/idempotency
and precommit cancellation controls. It records independent HTTP ACK and durable
completion ledgers. Pool-only/listener-only/combined and full-load cycle gates
remain unsupported/unmeasured; one client cycle is not 30 cycles/1,000 IDs.

Causal comparator review found four same-invalid-stream false-green groups;
regression-first corrections now reject them. Both startup last-ready and
teardown first-invalidator envelopes are checked per trace. The fresh
[reviewed semantic run](../../test/spike/s0/results/semantic-reviewed-ownership-fixed/invocation.json)
matched causal D02/D10/D12/D16 on both candidates and complete strict D31–D36
hook traces with all original assertions unchanged. Hook receipts bind source
before/after, the actual test executable and separate candidate/oracle origins.
Earlier hook receipts remain retained with their qualification limitation.

Precommit native review then found and corrected three further races: effective
config could overwrite a newer same-version preparation; Provide/MountChild could
lose an admitted ownership acknowledgment during cancellation; and a quiescing
old context could dispose its replacement revision. Regression tests and current
package race/vet passed after the fixes. A separate full rerun then completed
**PASS, exit 0**, on reviewed checkpoint `ad2fa95`, source digest
`8d6834ef1e4b3f1c4dcff8fe5581f56869b34757fca1c47dc1204a1d23833395`:
1,000 seeds x 1,000 operations per candidate under race, 2474.86s total. Its
[summary/log/invocation](../../test/spike/s0/results/property-full-reviewed-ad2fa95/)
cover those native changes. Older passes remain historical; future core/SDK
changes still need affected reruns. This does not establish full corpus,
performance, SQL authority, coverage/line-budget or S0 acceptance.

The accepted Go Effect setup API now uses a real setup lease and native disposal
budget for returned partial/late resources. This does not provide generator
incremental collection, withdrawable handles or complete D23 effect-tree proof.

## Remaining gates

All current components are experimental root-module packages. Complete context,
optional/child ownership, hook/config integration and G5 observer proof still
must be exercised on both candidates, followed by full carried differential
comparison, affected property reruns after core changes, the frozen scale/cold-path
budgets and final-port reruns. ADR-035's causal methodology is owner-approved;
its separate comparator/actual-witness implementation is in progress under the
preexecution amendment, without reclassifying historical strict failures. No production SDK kernel/testkit, line budget or core selection is
accepted by this progress note. Every test result is reported with its actual
scope; incomplete or failed gates remain incomplete or failed.
