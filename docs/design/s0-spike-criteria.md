# S0 spike acceptance criteria

Date: 2026-11-12. Status: **frozen before measurement**.
The project owner approved the S0 plan and its proposed numeric budgets on
2026-11-12. This document makes that plan executable without changing the
architecture. Artifact choices, licence evidence, fixture shapes and host
preflight are recorded below. A separate read-only consistency review found no
remaining protocol blocker; it established no experiment or compatibility result.
No measurement had started when this protocol was frozen.

Authority: [canonical decisions](../architecture/00-decisions.md), especially
§4.3, §4.5, §5.2, §7.2 and §9; [plugin record](plugin-system-2026-11.md),
§3, §4, §10, §14–16 and §18; [roadmap](../architecture/08-profiles-roadmap-adrs.md),
§2.1 and §4–5. Editorial baseline: [ADR-056](../adr/ADR-056-documentation-baseline.md).

## 1. Scope and stop conditions

S0 builds experimental evidence, the complete kernel port, and the real-runtime
kernel test kit. It does not build the launcher, a Go loader, an infrastructure
service definition or product provider, a remote runtime, a tenant directory or
cache, profiles, or any other S1-and-later deliverable. Synthetic context trees
are fixtures, not a tenant subsystem. HTTP/database/job fixtures are probes,
not early product implementations.

- Record thresholds before measuring. Do not tune them after observing results.
- A valid missed acceptance threshold is a failed design gate. Preserve the data,
  propose an ADR and obtain owner approval before changing a canonical decision.
  Restart-in-place failure reopens ADR-037; it is not converted into process exit.
- Eliminate a concurrency candidate that violates a correctness invariant. If no
  candidate passes, obtain an approved design response before production porting.
- A pending pin, missing dependency, skipped integration test, insufficient sample
  count or missing raw record is **not measured/inconclusive**, never pass.
- Publication rights remain open under L14. No push or public release is performed.
- Stop after S0 acceptance and report; do not begin S1 automatically.

## 2. Freeze and evidence identity

Before the first measured run, change status to **frozen before measurement** and
complete every preflight item in §3. Compute SHA-256 over the exact UTF-8 bytes of
this document after that edit. Store the digest outside this document to avoid a
self-referential hash. Every result must name that digest and the source/build
hashes of the executable and fixtures it exercised.

A run record contains: run ID and UTC start/end; criteria digest; baseline commit;
source-tree digest including uncommitted experimental files; executable digest;
exact commands and exit codes; dependency and artifact pins; host/resource record;
seeds; topology dimensions; scheduled and observed operation timestamps; request,
transaction and work IDs; state/service trace; resource checkpoints; failures;
and the classification **pass/fail/invalid/inconclusive** with reasons.

Keep raw results as well as summaries. State explicitly whether a record belongs
to a candidate or to the final port. A logic, dependency, fixture or toolchain
change invalidates affected results; rerun them on the new hashes. Results are
stored outside ignored working-output directories, with no credentials or
workstation paths in committed evidence.

The initial documentation-only commit is `2d8d6ec`. No S0 code or measured result
is part of that commit. Further commits require separate owner permission.

## 3. Preflight and pins (complete before freeze)

- [x] Owner approved S0 scope, thresholds and module prefix
      `github.com/hiento09/aiops-platform`.
- [x] Documentation-only baseline committed separately.
- [x] Reference identities: `@deepseek-ai/cordis` 4.1.0 and
      `@deepseek-ai/cordis-plugin-loader` 1.1.0.
- [x] Source-content pin: harness
      `fcc0ad462172522aaf5c93a7b2e01f6b9f0b0159`.
      All 18 Cordis/loader TypeScript files match the reference package source
      bytes at that commit. Public source evidence is recorded separately; this
      is not a signed-build or unique historical-publish-commit claim.
- [x] Oracle support package identities: `@deepseek-ai/cosmokit` 1.9.0,
      `@deepseek-ai/schemastery` 3.19.0, `@standard-schema/spec` 1.1.0.
- [x] Record oracle archive pins and licence identities in
      [S0 reference pins](s0-reference-pins.md). The oracle acquisition step must
      preserve complete source/artifact hash manifests and Shigma, DeepSeek and
      Standard Schema MIT texts and verify their bytes before execution. This is
      a per-run prerequisite, not a claim that vendoring is already complete.
      No oracle code or runtime enters a shipped product artifact.
- [x] Select isolated Node v24.22.1, official Linux x64 archive
      `node-v24.22.1-linux-x64.tar.xz`, SHA-256
      `6c172a9ab6718f7b801fb5a7bd486af2ee5e32758086a04b84fcce2aa77095e6`.
      Verify downloaded bytes before execution and retain the complete runtime
      licence evidence. The host Node 22.22.1 and cached `node:24` (24.22.2) are
      not this pin. The harness requires Node >=24.9.0. Built-JavaScript replay
      is the oracle execution form; TypeScript source is preserved and hashed
      but is not silently transformed into another runtime. No optional native
      dependency is added; the bounded loader tests use supported package APIs.
- [x] Record the kernel-relevant pinned harness regression tests and hashes in
      [S0 reference pins](s0-reference-pins.md). Re-express their operations in
      the attributed deterministic S0 corpus; do not claim the original Vitest
      harness ran. File/native resolver and dispatcher-specific assertions are
      outside S0. A complete original upstream suite is not identified in the
      published package. Availability/reading is not a test-pass result.
- [x] Record the inspected core-provider module/licence inventory in
      [S0 reference pins](s0-reference-pins.md) for the selected probe pins:
      `github.com/riverqueue/river` v0.51.2,
      `github.com/riverqueue/river/riverdriver/riverpgxv5` v0.51.2 and
      `github.com/jackc/pgx/v5` v5.11.0. All River submodules must retain exact
      version pins and MPL-2.0 notices/source route; pgx and other transitive
      licences are checked individually, not assumed MIT. Verify the actual
      linked package closure after compilation and before executing the probe;
      unknown/additional licences are a stop condition. Use unmodified River
      and its migration package. No paid add-on.
- [x] Select Docker Official Image PostgreSQL 16.15 for linux/amd64:
      `docker.io/library/postgres:16.15-trixie@sha256:43f9275df91e012c3da3d86bea5d7bb5e67a473296cc906c5f9e80b8176b1a37`.
      Its selected image config records `PG_VERSION=16.15-1.pgdg13+2`.
      Use and verify this immutable digest, not the different cached mutable
      `postgres:16.15`. Packaging/source evidence is recorded separately. It is
      used locally, not redistributed. No other workload's database is used.
- [x] Record the measurement-host preflight below. Database runtime settings and
      per-run background-load/cgroup deltas are captured when each fixture starts;
      they are preconditions, never inferred from image defaults.
      No performance inference from preflight.
- [x] Define topology shapes, operation counts, caller concurrency and seed
      sequence in §8, then record exact fixture/executable hashes in each run.
      Source may be authored after freeze, but cannot change a frozen topology,
      threshold or workload silently.
- [x] Freeze this protocol before any measurement. Record its exact SHA-256 in
      the freeze receipt and every measured run; do not edit these bytes after
      the receipt without an explicit protocol amendment and new evidence ID.

### Measurement host observed before freeze

| Field | Observation |
|---|---|
| OS/architecture | Linux x86_64 |
| Kernel | 7.0.0-43-generic |
| CPU | Intel Xeon Platinum 8168, 2.70 GHz; 24 visible logical CPUs |
| Process affinity / effective cpuset | CPUs 0–23 |
| Physical memory reported | 96,605,092 KiB total; availability is sampled per run |
| Swap | 8,388,604 KiB total; some host swap is already occupied |
| Go | go1.26.8 linux/amd64 |
| Docker client/server | 29.9.3 / 29.9.3 |
| Host Node (not the oracle pin) | v22.22.1 |
| Cgroup observation | cpu.stat reported nr_throttled=0; CPU/memory max files were not exposed at the inspected root |
| Shared-host constraint | Other workloads remain untouched; no claim of exclusive CPUs or memory |

Pre-existing occupied swap is not itself a measured swapping event. Sample
swap-in/out and throttling deltas during each run; apply the unchanged noise rule
in §4 if activity occurs. Do not invent an unlimited CPU/memory claim from a
missing cgroup file. Per-run records include the limits actually applied to the
probe and database, and actual durability settings.

## 4. Common experimental protocol

Use the same host and toolchain for both candidates/arms, `GOMAXPROCS=8`, one
local dedicated database, a pool of eight connections, and at most 32 concurrent
requests. Use two synthetic data scopes for transaction isolation. Synthetic-tree
workloads use 300 and 500 contexts as specified in §8. Record CPU affinity and
resource limits rather than inferring dedicated hardware from visible CPU count.

Fixture deadlines: load 2 seconds, drain 5 seconds, dispose 2 seconds. Normal
fixtures cooperate with cancellation; timeout/ignored-cancellation fixtures are
separate negative cases. These are the three kernel deadlines, not added retry,
rate-limit or health policy inside the kernel.

For each performance arm use at least ten independent repetitions. Transaction
and HTTP runs have 30 seconds of warm-up followed by a 120-second measured
window. Alternate arm/candidate order. For expensive graph operations collect at
least 1,000 observations **per candidate, topology and operation**, across fresh
runs, not ten aggregate `ns/op` averages. Measure at least 1,000,000 call-path
operations per arm and report timer overhead separately.

Quantiles use nearest-rank on individual operation latencies. Report p50, p95,
p99, maximum, sample count and per-run distributions as well as aggregates. Never
sort lifecycle traces to compute semantic equality: sorting is only for numeric
metric calculation. Do not describe an average Go benchmark as p99. If a probe
has fewer observations than its stated floor, it is inconclusive.

Load arrival is open-loop at the specified rate. Latency starts at **scheduled
arrival**, includes queue/pool wait, and ends when the client observes the response
or the transaction/work completion under test. Record every scheduled arrival,
including overload, error and timeout. An overflowed request cap/backlog, dropped
arrival or inability to sustain the offered rate fails that workload; do not
lower the offered rate after the run. Do not compute a success-only latency tail
that hides failures. Report outcome-class distributions and an all-attempt
service envelope; failed attempts remain in denominators. Request and transaction
operation timeout is 5 seconds from scheduled arrival, including queue time; a
timeout remains a failure and is not removed from latency/accounting data.
After each steady-state measured window stop new arrivals and allow at most
30 seconds for all committed/acknowledged IDs to reach logical completion.
Outstanding IDs or resources beyond that bound fail the run; never wait forever
to turn eventual progress into a pass.

- 500 transactions/second for 120 seconds means 60,000 scheduled transaction IDs.
- 100 requests/second for 120 seconds means 12,000 scheduled request IDs.
- Each normal steady-state arm must achieve at least 99.9% successful commits or
  acknowledgements and eventually account for every committed/acknowledged ID.
- Restart outages have their own 503/transport-error record, not the steady-state
  success requirement while a listener is actually stopped. Every recovery must
  nevertheless acknowledge and complete **1,000 new request IDs at 100/s** before
  it can pass, as well as preserve all earlier acknowledgements.

Optimized performance is measured without the race detector. Run separate race
and stress tests; never present race-instrumented cost as optimized performance.

### Noise and invalid runs

Throughput coefficient of variation greater than 10% across independent runs,
or observed host CPU throttling or swapping, requires a repeat of the unchanged
protocol before a performance conclusion. Preserve the invalid data and reason.
Candidate OOM, panic, deadlock, race, resource leak, unbounded queue or its own cap
violation is a failure, not environmental noise. Do not discard slow runs merely
because they miss a threshold.

## 5. Transaction and database-role probe (ADR-045)

One caller-owned pgx transaction writes caller state, a River `InsertTx` job,
outbox event and audit append. The four writes must share the same commit or
rollback. The fixture carries an opaque call-scope handle; no job argument or
event body is trusted as the transaction's identity. Stamp identity into an
owned database column and verify it at delivery, including malicious payload IDs.
The exact integration with River's tables is a question to prove, not an assumed
S3 schema/API decision. Stock River at the selected version has no tenant column
or tenant-context API: do not claim otherwise. A provider-owned column/default or
mapping must be proved atomic with unmodified InsertTx and forced RLS, with no
payload-derived identity. Background worker authorization and schema/search-path
behavior on every separate pool connection are explicit parts of that proof.

Use one schema and one no-login runtime role per synthetic owner. The login
runtime role owns no table, is not superuser and has no BYPASSRLS. Every scoped
table uses FORCE ROW LEVEL SECURITY. Migrations use a separate owner capability,
never the measured runtime connection. Keep `fsync=on` and
`synchronous_commit=on`; record the actual settings.

Use `SET LOCAL ROLE` for the caller; switch to the jobs role around unmodified
`InsertTx` and restore the caller's role. Outbox/audit functions use SECURITY
DEFINER with least privilege, a fixed safe search path and an owner subject to
forced RLS. A failed SQL transaction cannot be reused by pretending that a failed
role-restore statement succeeded.

Required cases:

- Commit and rollback at every participant boundary, cancellation, connection
  loss and SQL errors before/after job insertion; no partial durable writes.
- Two scopes, missing handle (zero rows and no scoped write), wrong identity in
  payload, direct cross-schema writes, cross-scope reads and writes.
- Role, scope setting and search path do not leak into a reused pool connection.
- Attempt `SET LOCAL ROLE`, `RESET ROLE`, `set_config` scope tampering, unsafe
  function grants and search-path shadowing through the actual caller SQL
  capability. NOLOGIN and FORCE RLS alone are not proof against these operations.
  Distinguish API misuse from hostile in-process code, which is outside the
  accepted trust boundary. If the declared SQL contract cannot maintain its
  boundary, report failure and ask for an ADR; do not add a SQL-text blacklist.
- Exercise insertion, fetch, completion, rescue, uniqueness and leader operations
  using their real fixture roles, not an undisclosed privileged worker. Verify
  how background work without a request-scoped handle is authorized.

Performance acceptance at 500 Tx/s:

| Metric | Pass threshold |
|---|---|
| Partial commit, cross-scope read/write, unauthorized schema write, leaked setting | Zero |
| Jobs-role entry and restoration, complete round-trip cost | p99 <=2 ms |
| Complete caller transaction | p99 <=25 ms |

Use a cost-control arm on a separate otherwise identical database/schema set
with the same data, forced RLS and writes, but a dedicated cost-control role
permitted to perform all four fixture writes without the extra jobs-role round
trip. It is never used to pass the protected arm's isolation assertions. It is
a diagnostic control, not a replacement for the protected acceptance arm. Record absolute and relative overhead.

## 6. In-process resource restart probe (ADR-045; ADR-037 if it fails)

Keep one process/PID alive under 100 requests/second. Stop and recreate the pgx
pool, the River client and the HTTP listener individually (30 cycles per
resource), then all three together (ten cycles). For each cycle run the same
30-second warm-up and 120-second measured window, request restart at measured
t=60 seconds, and keep the arrival schedule running. In the 200 ms interval
before the restart request, hold acquired transactions, handlers and kernel
leases for a controlled 200 ms so restart encounters in-flight work. Inject
resource orders pool, client, listener, then combined, with fresh independent
ledgers per arm and persistent per-run request IDs. Restart the
listener on the same loopback address. The experiment may recreate a library
object if required by that library; it must not replace the process.

An **acknowledgement is a 2xx received by the client**, sent only after successful
durable commit. Maintain independent client ack and database completion ledgers,
joined by request/idempotency ID. A commit whose response was lost may be retried
under the same ID. At-least-once attempts are allowed; the observable logical
result must be idempotent. Never require every request to succeed while the
listener is deliberately stopped, and never count a 503 as accepted work.

| Metric | Pass threshold |
|---|---|
| Lost acknowledged work, false 2xx, duplicate logical result | Zero |
| Recovery of fixture readiness and all affected consumers | <=5 s per cycle |
| Every previously acknowledged ID logically completes after recovery | <=30 s |
| Successful recovery progress | 1,000 new acked/completed IDs at 100/s per cycle |
| Orphan transaction, lease, worker, handler, route or listener | Zero |

Revoke a handler while work is queued: an unowned kind snoozes, is not completed
or discarded. Run two clients within the same process, with a handler absent
on one and present on the other; verify eventual progress. This probes the
future replica case without building a multi-replica product runtime.

After the final kernel exists, rerun this probe with resources owned by its real
effects and actual lease/quiesce behavior; standalone-library restart is not
sufficient final acceptance. This is resource restart, not a claim of database
power-loss recovery or exactly-once delivery.

## 7. Concurrency candidates and semantic corpus (ADR-034/035)

Candidate A: a reconciler goroutine owns the graph/fiber fields through an inbox
and deduplicated work queue. Candidate B: a graph lock with per-fiber transition
goroutines. Both use the same declared fixtures, scenario scripts, property
operations, deadlines, lease behavior, observer assertions and performance load.
Do not omit hooks, bindings, cleanup or leases from one candidate's cost.

Hard invariants G1–G5:

1. No plugin callback executes while graph state is being mutated or under the
   graph lock; fiber fields have one writer at a time.
2. At most one transition per fiber; Apply and disposers execute outside the
   state owner/lock, on that fiber's transition goroutine.
3. Service-call paths use immutable published snapshots and copy-on-write listener
   sets, with no graph mutex. Every optional dependency is a real `When` fiber.
4. Unload cancels `Std`, refuses new leases/effects, drains open leases and tracked
   goroutines, then disposes strictly sequentially in reverse. A leased call does
   not synchronously wait for the reload queue.
5. Status/service observers run off the state owner and see one total event order,
   with no missing or duplicated event. Reentrant callbacks cannot deadlock it.

At least 30 stable-ID differential scenarios cover the following groups (more
than one scenario per group where necessary):

| IDs | Required behavior |
|---|---|
| D01–D04 | Extend inheritance; separate isolation realms; equal isolation labels join; duplicate provision refusal |
| D05–D08 | Required declaration; inherited inject in a realm; NeedWith config; ancestor-first Intercept chain |
| D09–D12 | Availability bound to each consumer; PENDING until ready; only ACTIVE provision visible; late provider activation |
| D13–D17 | Six states; revision and provision UID epochs; provider swap; chain/diamond invalidation; coalesced updates |
| D18–D22 | Sticky failure; explicit Update recovery; superseded load error; provider retained through consumer teardown; child teardown |
| D23–D26 | Partial-effect unwind; inactive effect refusal; own-dispose request from Apply; distinct awaited self-dispose diagnostic |
| D27–D30 | Raw/effective config and internal config/update/commit hooks; no unintended restart on an unchanged target |
| D31–D36 | Emit, Parallel, Serial, Bail; Waterfall veto; Waterfall repeated-next error |

The same scripts run against the pinned Cordis oracle and both candidates.
Compare settled states, provision identity/epoch relationships, outputs and
status/service traces at explicit causal barriers. Normalize only timestamps,
opaque UID renaming and the approved built-in `events` -> `hooks` rename. Do not
sort/remove events or erase restart boundaries to create equality. Each excluded
assertion names its exact adapted/not-carried checklist row; never exempt a whole
scenario just because one aspect differs.

Oracle edge cases must be explicit: Await is not an ACTIVE assertion; Serial
stops at bail/error; Parallel is all-settled before aggregate failure; Bail is not
JavaScript truthiness (`0` and the empty string are bail values). Requesting
self-disposal and awaiting it inside one's own Apply are separate scenarios.
The latter has an identified static cycle risk and is not assumed supported
until replayed. Preserve diagnostics and ask before changing an accepted contract.

Separate adaptation/addition tests prove strict sequential LIFO, deadlines,
Enter/quiesce, tracked goroutines, Batch/Settle, provider/consumer entry-identity
bindings and child inheritance, hook owner/declaration/stages/audience, and scoped
values writable only by the owning provider. Unscoped dispatch cannot reach a
scoped listener. Accepted adaptations are not untested omissions.

Correctness acceptance: zero unexplained semantic mismatches, races, deadlocks,
double transitions, stale publication, post-release effects or observer-order
violations. Run property tests for **1,000 seeds x 1,000 operations** (provide,
dispose, update, fail), preserving seed/trace and a minimized regression on failure.
All sequences converge and final ledgers are empty. Run `-race` and deterministic
lifecycle tests using `testing/synctest` or causal barriers, not timing sleeps.

## 8. Graph, synthetic-tree and call-path budgets (ADR-034/036)

### 8.1 Ten-thousand-fiber graphs

Each topology has exactly 10,000 measured fibers, excluding the kernel's root.
Every fixture body performs the same small declared setup, with no network I/O
or persistent goroutine. The epoch-provider edges are:

- Independent: 10,000 zero-Need fibers; update one selected leaf. A separate
  declared fixture provider is counted within the 10,000 when restart is tested.
- Chain: fiber 0 provides the first key; fiber i needs i-1 and provides key i,
  for i=1..9,999. Restart fiber 0 and update fiber 9,999 separately.
- Fan-out: fiber 0 provides one key; fibers 1..9,999 each need it.
- Diamonds: one root provider plus 3,333 three-fiber groups (two siblings need
  the root and each provide one key; the group's third fiber needs both).
  Total is 1 + 3*3,333 = 10,000.
- Isolated realms: 100 separate realm labels, each with one provider and
  99 consumers; identical key names, independent resolutions. Restart one
  provider (local closure) and all providers in one Batch (full closure).

A missing provision or an invalid graph is never counted as a cheap successful
mount. Every required fixture fiber must reach ACTIVE before a mount settles.
Property/random workload seed is `0x53000000 + seedIndex` for seedIndex 0..999;
record the PRNG algorithm and generated operation stream, not only its seed.
Deterministic operation selection and arrival-order seeds are candidate-independent.
Measure mount/settle, leaf update/settle, provider restart/settle and teardown.
The empty root and post-shared fixture baselines are recorded separately.

| Metric | Pass threshold |
|---|---|
| Mount and settle | p99 <=2 s |
| Leaf update and settle | p99 <=10 ms |
| Root-provider restart and settle | <=5 s |
| Retained heap over the empty/shared baseline | <=128 MiB |
| Idle goroutine per fiber | Zero; fixed root workers recorded separately |
| Teardown | Resource/registry/effect/lease baseline restored |

### 8.2 Five hundred synthetic trees

Start with 97 shared fixture fibers: one generic provider and 96 consumers of
its key. The two-fiber tree has one realm-local provider depending on that shared
key and one consumer of the local key; the four-fiber tree adds two more local
consumers. A scope value distinguishes each tree, with no product vocabulary
inside the kernel. Count every container fiber if the fixture creates one; do
not hide it from the two/four total. Use 500 loaded trees and an additional
300-tree run to compare the original design budget. Execute ten full cycles of
mount-all, hot lookup, one Batch shared-provider restart, reverse-order tree
disposal and remount; independently record each tree's cold mount. These counts
are fixture shapes, not the release's product inventory.

| Metric | Pass threshold |
|---|---|
| Eager tree mount at boot | Zero |
| Each measured in-process cold mount | <50 ms; report maximum, not only mean |
| Idle goroutines that scale with trees | Zero |
| Retained heap above shared-only baseline for 500 trees | <=32 MiB |
| Shared root-provider restart and settle | <=5 s |
| Cross-scope lookup/callback/effect leakage | Zero |
| Churn and final teardown | Empty owned ledger and restored resource baseline |

Remote pod cost model and operational leak budget remain S4b decisions; this
probe supplies no remote runtime, tenant cache or sidecar implementation.

### 8.3 Call path

Measure bound lookup/view creation as applicable, Enter+leave and a contribution
call at caller concurrency 1, 8 and 32, without concurrent graph mutation as the
baseline and with mutation as a separate stress arm. That stress arm submits
100 leaf updates/second across 100 unrelated fixture leaves for the complete
120-second measured interval, with at most eight updates outstanding; overload
fails rather than reducing mutation rate. The leased contribution itself is
updated every 1,000th scheduled mutation to exercise refusal/reacquisition.
Stress outcomes are reported separately; the allocation/latency budget below
applies to the no-mutation baseline, while correctness holds in both arms.
The complete Enter+leave path must have **p99 <=2 microseconds and zero allocation per operation**. Report
lookup/view/dispatch costs too; an isolated atomic operation is not the contract.
There is no mutex on resolve/dispatch paths. Specify who acquires a contribution
lease and who releases it in ADR-034.

## 9. Cold mount on the acknowledgement path

Compare diagnostic arm A (cold mount inline before commit/ack) with arm B (all
ack/page dependencies pinned while other synthetic trees cold-mount and churn).
This does not remove the accepted pinning rule even if arm A is fast.

For each body-size run alternate a pinned-only baseline B0 and pinned-with-churn
B1 on identical data and workload. Each arm uses ten 30-second-warm-up plus
120-second-measured repetitions. B1 schedules 50 fresh four-fiber tree mounts
per second (IDs round-robin across 500 scope labels), at most eight mounts in
flight, then disposes each tree after 100 ms of idle time. B0 runs the same
scheduler without starting mounts. Requests target only the pinned path in
both B arms. Record every scheduled/completed background mount and disposal;
a missed churn arrival or cap/backlog overflow fails B1 rather than making its
latency comparison artificially easy. Pair B0/B1 repetitions in alternating
order and evaluate the <=5 ms delta on each matched pair as well as aggregate.
Arm A uses one fresh four-fiber scope per first-time request and the same
100-request/s schedule; report its complete latency and mount results, not a
pinning exemption.

At 100 requests/second test bodies with 100 synthetic items and a separate 1 MiB
body. Arm B has zero cold-ensure/mount calls in its measured ack path, p99 ack
<=250 ms for the 100-item body and <=1 second at 1 MiB. Background cold churn may
increase arm B p99 by no more than 5 ms over its pinned baseline. Record the
complete mount+transaction latency of arm A rather than subtracting mount cost.

A separate cold-timeout fixture returns 503 with Retry-After on synchronous
lookup or snoozes the simulated job. It never acknowledges an uncommitted write
or falls through to another scope's/process contribution. Do not implement a
product tenant cache to run this fixture.

## 10. Resource and leak accounting

Establish a warmed baseline with root/pool/River/listener alive. At cycles
0/10/20/30, after drain, Settle, observer-queue completion and GC, record:

- Fiber, provision, contribution, route, listener, job-handler and registry entries.
- Active/acquired connections and open transactions by owner and generation.
- Tracked goroutines, contribution leases, pending transitions and effect ledger.
- Live sockets/file descriptors and retained heap.

Disposed scopes/generations must own exactly zero active transactions, acquired
connections, entries, effects and leases. Remaining live resources equal the
warmed baseline with no cycle-to-cycle growth. Compare goroutine owner/stacks;
one disappearing unrelated worker does not cancel out a leaked worker. Fixed
library/root background workers must be enumerated before measurement, not
exempted after a leak is discovered.

After full teardown (including root, pool, River and listener), counts return to
the harness baseline; no live socket/FD growth. After cycle GC the retained heap
must not exceed the warmed baseline by more than `max(1 MiB, 5% of baseline)`.
Report checkpoint series and regression slope. Heap tolerance never excuses a
goroutine, lease, socket or registry leak. Idle fiber/tree goroutine constraints
are hard zero-per-instance constraints, not this heap tolerance.

## 11. Final port acceptance and ADR evidence

The selected final port is rerun through all affected acceptance probes, not
only the prototype's benchmarks. Bind evidence to final source/build hashes.

- ADR-034: concurrency choice, G1–G5, lease owner/cost, self-dispose semantics,
  observer behavior, actual line count and approved hard line budget.
- ADR-035: exact source/content pin, packaging differences, comparison method,
  exclusions, available tests, runtime/toolchain and resync-only-by-ADR rule.
- ADR-036: S0 graph/tree/cold-path readings; leave the S4b cost model open.
- ADR-045: atomic participation, role isolation and cost, identity stamping,
  unowned-handler snooze and in-process restart feasibility; S3 ratification
  obligations remain explicit.

Kernel requirements: all standard-library imports, no `require` line in its
module including test dependencies, four built-ins only, no product vocabulary,
policy or retry mechanism. Testkit is a separate test-only module and uses the
real runtime. Any test requiring testkit lives outside the kernel module rather
than making kernel depend on testkit. A production constructor cannot turn off
both binding classes; only the testing.TB-requiring testkit path may do so.

Run `go vet ./...` and `go test -race -count=3 ./...` for every S0 module/package.
The PostgreSQL and oracle suites must actually run; do not count skips as passes.
Property corpus size is §7. Fuzz each selected lifecycle/codec entry point for at
least 60 seconds and retain failing inputs.

Require 100% statement coverage in **each production Go file** of the kernel,
not a module average. Count production Go physical lines including comments and
blank lines deterministically; list tests, licences and oracle separately. Do
not relocate logic to evade coverage or budget. Freeze actual count and hard cap
in ADR-034 at the S0 exit; later growth/new kernel packages require an ADR.

A final report lists pass/fail/not-run, measured data, reproducible commands and
exit codes, approved ADRs, line budget, licences and real limitations. Only then
mark S0 complete and stop.
