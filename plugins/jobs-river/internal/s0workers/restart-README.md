# Bounded S0 ordinary-functional restart diagnostic

**Authored and compile-checked only; the authoring child has not executed SQL,
opened a database, read a private DSN, or executed the test binary.** This adds
only `restart_test.go`, `restart_report_test.go`, and this document. Existing
worker fixtures, migrations, module pins, and original report are unchanged.
The corrected worker log records the four existing scope/revocation cases as
passing; it contains **no restart measurements**.

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

## Explicit unsupported harness boundaries

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
