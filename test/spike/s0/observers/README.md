# Off-owner observer dispatcher prototype

This is a **test-only generic dispatcher bridge**, not a selected runtime or
public API. The stdlib-only implementation is shared from
[`internal/eventqueue`](../internal/eventqueue/dispatcher.go); this package keeps
identity-preserving Go type aliases and the real-runtime regression tests. That
layout lets candidates import the queue without importing tests that depend on
the candidates themselves. The extraction does not itself modify either
candidate's event publisher or prove source integration.

Tests here register effects on **real activation contexts in both candidates**
and manually feed immutable events obtained from their real event ledgers. The
private uint64-boundary and popped-storage assertions live beside the queue in
`internal/eventqueue/dispatcher_test.go`; those algorithm-only fixtures make no
lifecycle claim. The lifecycle, error, reentrancy, and drain tests remain here.

## Contract exercised

- A state owner can call `Append(sequence, event)` while it owns its ledger turn
  or lock. Append copies `T` by value and captures an immutable COW listener list;
  it never invokes a listener, lease method, formatter, marshaler, clone hook,
  filter, or logger. The short queue/registration mutex never spans user code.
- The sequence must be positive and consecutive, starting at one. Zero,
  duplicate, backwards, gap, and wraparound input return `ErrSequenceGap` without
  accepting or queueing that event. There is no retry or silent sequence repair.
- Exactly one root-owned worker serializes events, then observers in registration
  order. Reentrant appends join the same FIFO behind already accepted work.
  Delivery completes only after every captured observer has run or been skipped
  because its native activation no longer admits a lease, and logging has ended.
- `Binding` needs only `Enter`, `Leave`, and native `Defer`. A setup lease spans
  effect registration and COW publication, while callback leases span actual
  callback execution. No binding method executes under the mutation mutex.
  Candidate-specific named disposer conversion exists only in the test adapter.
- Manual disposal is idempotent and changes **future enqueue snapshots**, not
  snapshots already captured. Already leased callbacks finish normally. Native
  owner quiescence is stronger: it skips even previously captured callbacks, and
  native dependent/provider cleanup waits for the current callback's lease.
- This is the explicit internal-observer containment boundary: an observer error
  or panic is formatted off-owner, logged once, and skipped; remaining observers
  and events continue. It does **not** change the independent hook prototype's
  raw dispatch modes. Error identity is retained through `Unwrap`, while logger
  diagnostics cache formatted text. A logger panic is contained and exposed in
  `Status.LoggerPanics`/`LastLoggerError`; it never recursively retries logging.
- Callbacks get an independent `context.Background` derivative with a private
  dispatcher marker and no activation identity. `Close` cancels it. A waiter's
  cancellation never cancels the callback or discards accepted events.
- `WaitBarrier(ctx, sequence)` explicitly joins the delivered prefix;
  `Barrier(ctx)` captures the current accepted watermark. Delivery is not an
  assertion that observers all succeeded. A callback **must not synchronously
  join its own dispatcher**. Passing its marked callback context (or a derivative
  preserving values) returns `ErrReentrant`; unrelated contexts cannot be
  detected and can deadlock if misused. There are no goroutine IDs or unsafe
  hidden reentrancy heuristics.
- Native `Settle` must **not implicitly await observers**. A harness may call
  native `Settle`, then perform an explicit separate observer drain using the
  barrier. A callback can call native `Update`, `Snapshots`, and `Settle` for
  unrelated lifecycle work without self-joining the observer queue. It must also
  obey native lease rules: it cannot synchronously join teardown of the very
  activation whose lease it currently holds.
- `Close(ctx)` stops admission, cancels the independent callback context, and
  joins the worker only after all accepted events finish. Queued callbacks still
  run with a canceled context if their owner remains active. An uncooperative
  callback or logger cannot be killed by the scheduler. A bounded Close returns
  `CloseError` with accepted/delivered/remaining/queued/in-flight diagnostics;
  the worker and accepted work remain real. A later Close can join actual exit.

## Immutability and memory limits

Owners must freeze/copy event reference data before passing it to Append, and
observers must not mutate it. Generic deep cloning would invoke arbitrary code
or lose payload identity, so the dispatcher does neither. The real-event test
passes every current field through unchanged: `Sequence`, `Fiber`, `Name`, `Kind`,
`From`, `State`, `Key`, `UID`, `Value`, and `Data`. The candidates currently spell
service notification kinds differently; the test recognizes their existing
spellings without rewriting their event streams.

Queue storage clears popped references and compacts when the consumed prefix is
at least half the slice. This is an amortized data algorithm, not a fixed tuning
budget. The empty queue releases its event backing storage. **The queue is
bounded** (`Options.Backlog`, default `DefaultBacklog`): when an event arrives while
that many are undelivered, the observer the worker is inside is detached, its lost
events are counted, and the worker role passes to a new goroutine that resumes the
same event for the other observers. The overflow is reported through the failure
path, `Status`, an optional terminal `OverflowHandler`, and `Close`. See ADR-034
(bounded observer queue) for the rule, the alternatives and the sizing. A hung
logger still blocks the worker; no retry loop, per-event worker, or new runtime
deadline policy is selected by this probe.

## Scoped validation and remaining proof

```sh
go test ./test/spike/s0/internal/eventqueue ./test/spike/s0/observers -count=1
go test -race ./test/spike/s0/internal/eventqueue ./test/spike/s0/observers -count=50
go vet ./test/spike/s0/internal/eventqueue ./test/spike/s0/observers
```

The original [validation record](validation.json) remains an unchanged historical
receipt for the pre-extraction source hashes, not validation of the new layout.
The [extraction receipt](validation-eventqueue-extraction.json) retains actual
command outputs, exit codes, and hashes for the alias seam and shared queue.
Neither receipt is a complete S0/G5 result artifact.

Tests cover positive/consecutive acceptance, opaque non-marshaled payloads,
enqueue-time COW membership, exact sequential callback order, explicit callback
context reentrancy rejection, callback/error-formatter/logger containment,
reentrant native inspection/update/settle, actual total-order event envelopes,
real dependent/provider lease drain and lifecycle removal, deterministic native
setup-versus-quiescence races, concurrent native registration/disposal/append,
uint64 sequence wraparound rejection, and bounded Close with a deliberately held callback
and a retained multi-thousand-event backlog. These are local prototype tests,
not a source-integration, performance, or whole-gate result.

Remaining: source-owner publication integration for both candidate ledgers,
canonical end-to-end status/service observer trace parity, an explicit separate
harness drain, complete scale/leak/deadline workloads and retention/backpressure
policy decisions. **No whole S0 or G5 proof is claimed.**
