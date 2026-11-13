# Independent test-only hook prototype

This is an S0 artifact, **not `sdk/kernel`, not an SDK API decision, not a scheduler
choice, and not S1**. Production code here imports the standard library and the
existing test-only immutable `contextmodel` metadata package. The tests additionally
adapt both real `concurrency` candidates. There is no new module, fake kernel,
product vocabulary, global registry, CI hook, or architecture-record change.

## Evidence and semantics

Read before implementation:

- [Plugin record §4](../../../../docs/design/plugin-system-2026-11.md#4-the-context-api-a-plugin-author-sees),
  [§8](../../../../docs/design/plugin-system-2026-11.md#8-hooks), and
  [§15](../../../../docs/design/plugin-system-2026-11.md#15-dsh-property-checklist).
- [07 §3.4](../../../../docs/architecture/07-c4-plugin-sdk.md#34-hooks-audience-and-gates).
- The preserved [Cordis events source](../../../differential/oracle/vendor/cordis/src/events.ts),
  source-content pin `fcc0ad462172522aaf5c93a7b2e01f6b9f0b0159`, and
  [D31](../../../differential/corpus/D31.json),
  [D32](../../../differential/corpus/D32.json),
  [D33](../../../differential/corpus/D33.json),
  [D34](../../../differential/corpus/D34.json),
  [D35](../../../differential/corpus/D35.json),
  [D36](../../../differential/corpus/D36.json).

These are raw dispatch algorithms; no frozen differential output was rewritten.
Unit tests establish local semantics, **not an integrated corpus parity claim**.

| Mode | Prototype contract |
| --- | --- |
| `Emit` | Invoke prefixes synchronously in snapshot order; ignore decisions and already-started futures; stop on the first synchronous error/panic converted at the invocation boundary. No goroutine is started by the engine. |
| `Parallel` | Invoke all prefixes in snapshot order, then await every started future; return `errors.Join` in snapshot order after settling them, not on first failure. |
| `Serial` | Await each final decision before proceeding; stop at the first explicit bail or synchronous/asynchronous error. |
| `Bail` | Synchronous first explicit bail; a future object itself is a non-null bail result, without awaiting its final value. |
| `Waterfall` | Outermost first; no `next` vetoes inner listeners and the built-in operation; the outer return value is authoritative, including a valid zero. Each continuation admits only one call atomically; a second call returns `ErrNext` and may be handled by the listener just as a source exception may be caught. |

`Decision{Value: 0, Stop: true}` and `Decision{Value: "", Stop: true}` bail.
`Decision{Value: false}` continues. This is an explicit Go representation of
Cordis' null/false/undefined distinction, not Go truthiness. `Result[R].Present`
distinguishes an absent waterfall result from a valid zero. It does **not** say
whether the built-in operation ran; a veto can return an outer value.

### Source/canonical error-policy ambiguity — deliberately not resolved

The pinned raw source `Emit` throws on synchronous failure, `Serial`/`Bail` stop
on error, and `Parallel` throws an aggregate after all settlements. The canonical
07 observer table says event errors/timeouts are skipped and logged. This package
keeps the raw oracle-facing behavior and surfaces errors; it does **not** silently
choose a new canonical policy, swallow failures, claim logging, or decide where a
future owner/frontend should perform observer containment. That integration and
its acceptance remain a reported gap requiring the owning implementation/review.
A `Gate`, its expected-set policy, and fail-closed aggregation are not implemented.

## Lifecycle bridge, identity, declarations, and scope

A `Definition` supplies its owning key and **ordered** stage vocabulary. `Use`
contains a private hook identity plus stage; a declaration for another allocated
hook with the same text does not authorize this hook. `On` checks the hook/stage
against `Binding.Uses`, requires real lease and lifecycle-defer callbacks, and
derives the ordering plugin ID and registered entry token from metadata. Invalid
or undeclared registration is an error, never a quietly omitted listener.

`Binding` is intentionally bridge data rather than another pretend `Context`.
The frontend must construct it from the actual activation and trusted declaration;
plugins must not be able to forge the association. The metadata package does not
own lifecycle or know which provisions are actually ACTIVE. Dispatch now requires
both the authorized `Provides` declaration and an `Owns` check of the same native
activation's successful reservation, followed by its real lease. `hooks.Bind`
derives metadata, ownership, lease and cleanup from one genuine composed context;
the declaration-only dispatch regression is denied before Provide and accepted
after actual native registration. The low-level Binding remains trusted bridge
data: final declaration admission, authoritative ACTIVE publication and hook class
integration are still required. Tests reject same-name entry spoofing rather
than conferring authority through string equality.

The actual candidate adapter is small and lives in tests:

```go
binding := hooks.Binding{
    Metadata: metadata,
    Runtime: candidateContext, // Enter() bool; Leave()
    Uses: declaredUses,
    Owns: func(key contextmodel.AnyKey) bool {
        address, err := composedContext.NativeAddress(key)
        return err == nil && candidateContext.Owns(address)
    },
    Defer: func(label string, dispose func(context.Context) error) error {
        return candidateContext.Defer(label, concurrency.Disposer(dispose))
    },
}
```

Registration takes a setup lease; it registers the real cleanup before publishing
with copy-on-write. The mutation mutex is never held while running listener,
audience, lifecycle setup, or cleanup callbacks. Every dispatch reads one
`atomic.Pointer` snapshot; no engine registration mutex is acquired on the call
path. Owner/listener leases are atomic in both candidate implementations. Arbitrary
listener, scope predicate, or future code may itself lock: this is not a blanket
claim that all user callbacks are lock-free.

Ordering is declared stage index, then plugin ID. Equal stage/ID ties retain
stable registration order; the canonical contract supplies no third key and this
prototype imposes no duplicate-listener quota. Registration/disposal cannot change
a captured dispatch snapshot: manual removal affects the next dispatch. Quiescing
is stronger: an inactive activation's `Enter` refuses even an old snapshot.

Every dispatch takes an explicit `contextmodel.Audience`. Zero matches only
unscoped listeners. Scoped audiences are minted through an authorized
`contextmodel.Scope.Audience(owner, predicate)`, and cannot be widened by a
`global`, bare filter, nil overload, or hook-side wildcard constructor.

## Asynchronous results and cancellation

`Future` means already-started work. A frontend executes the synchronous prefix,
starts/owns its asynchronous remainder, and returns the future. `Completion` is a
small no-goroutine completion bridge; it does not schedule work or make it an
effect. The frontend must actually lifecycle-own/cancel/settle that work.

The hook engine transfers the listener's acquired lease to `Future.OnSettled`.
Consequently `Emit`/`Bail` may return without waiting while teardown still waits
for actual completion. A canceled `Await` is not completion and cannot release
that lease early. `OnSettled` must obey its trusted bridge contract; a panic while
attaching completion is surfaced and conservatively retains the lease rather
than incorrectly treating running work as finished. A real runtime's drain
policy owns that contract failure. A future returns a final, flattened decision;
this is not a JavaScript Promise-resolution implementation.

All callbacks receive the caller's standard context. Cancellation/deadlines are
cooperative: no Go goroutine can be killed. On cancellation, later listeners are
not entered. An already-running callback that ignores cancellation must still be
drained by its runtime. `Parallel` is all-settled for work while the caller context
permits waiting; cancellation may return before actual work settles, but retains
the work's listener leases until its real settlement.

Waterfall currently uses synchronous Go continuations; work must be awaited
inside a callback. A continuation used after that callback returned is rejected,
and an accepted concurrently-running continuation is joined before releasing its
outer activation lease. **A detached asynchronous source waterfall is not yet
represented or proved.** Do not claim full Promise-returning waterfall parity.

## Validation and remaining gaps

Run from the repository root:

```sh
go test ./test/spike/s0/hooks -count=1
go test -race ./test/spike/s0/hooks -count=10
go test ./test/spike/s0/hooks -run '^$' -bench . -benchmem
```

Every registration in lifecycle/semantic tests is made against an actual mounted
candidate activation, with actual `Defer` and `Enter`/`Leave`; there is no fake
runtime replacing teardown. Tests cover COW reentrant registration/disposal,
concurrent mutation/dispatch/disposal, effect drain for blocking callbacks and
already-returned futures, cancellation without false lease release, all modes'
scoping, stage ordering, zero/empty bail representation, panic paths, waterfall
veto/result/continuation admission, and invalid declarations/ownership.

Still **not** established:

- Hook access classes, signed/sealed admission, trusted frontend binding to actual
  ACTIVE provisions, canonical `Spec.Hooks` integration, or tenant-payload
  definitions refusing zero/unscoped audiences. This primitive has no tenant-data
  field and does not pretend those mandatory checks happen.
- Resolution of the raw-source versus canonical observer skip/log policy above;
  implicit future-error logging/observation after `Emit`; a product gate.
- Async waterfall parity, built-in lifecycle/config hooks driving runtime state,
  custom hook catalogue collisions/import identity, remote protocols/deadlines,
  or commonfrontend replay integration of D31–D36.
- S0 acceptance, 10,000-fiber performance, complete benchmark acceptance,
  final-port coverage, line budget or a kernel selection. Local call-path
  measurements do not substitute for the approved full acceptance plan.

Full MIT notices plus original Go SPDX terms are retained in [NOTICE](NOTICE).
