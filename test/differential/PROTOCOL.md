<!-- SPDX-License-Identifier: Apache-2.0 -->
# S0 neutral replay protocol v1

`protocol.go` defines the JSON transport. It is test-only and standard-library-only.
A script runs on a fresh root. `root` is the implicit context; a mounted fiber's
ID also names its plugin context. Contexts are declared parent-first; their
metadata inherits without mutation. Equal isolation labels join **per key**;
missing labels allocate a distinct realm. No executable JavaScript appears in
scripts. Values use JSON, with `{ "$config": "field.path" }`,
`{ "$meta": "field.path" }`, and `{ "$output": "name" }` substitutions.
An empty config/meta path returns the complete object. JSON null remains null;
JavaScript undefined is encoded as `{ "$undefined": true }`, never dropped.

## Actions

All bodies run in array order. Bodies may return a promise only when an action
requires it; synchronous bodies remain synchronous. Actions target `context`,
then `fiber`, then the enclosing plugin context, or `root` externally.

| op | Fields and meaning |
|---|---|
| `context` | `to` creates a dynamic context inheriting the current/explicit `context`, with `meta`, or `mode:"isolate"` and `key`,`label`, or `mode:"intercept"` and `key`,`config`. Never mutates its ancestor. |
| `when` | Compare evaluated `base` and `value` using JSON deep equality; execute `program` only when equal. A fixture branch, not an expected-result response. |
| `mount` / `child` | `fiber`, `plugin`, optional `context`, `config`; mount does not await; child uses the current plugin context. The fiber's context inherits its parent. |
| `settle` | `label`; await every current fiber transition and tracked external task until a fixed point, then capture a checkpoint. PENDING is stable, **not ACTIVE**. Startup failures are captured, not hidden. |
| `snapshot` | `label`; capture now without waiting; used behind an explicit `waitMark` gate to observe LOADING/UNLOADING. |
| `waitMark` | `label`, optional `count` (default one); wait for that many `mark`/`gate` arrivals. |
| `mark` | `label`, optional `value`; append ordered mark and notify waiters. |
| `gate` | `label`; append gate arrival then wait for `release(label)`. |
| `release` | `label`; release that gate permanently (also valid before arrival). |
| `provide` | `key`, `value`; register an ordinary value; optional `to` names its disposer for `withdraw`. `mode: "service"` wraps a tracked service supporting intercept config; `mode: "available"` additionally uses the `value.ready` output map keyed by the calling context's `meta.audience`, read by the availability predicate. |
| `notify` | `keys`; re-evaluate availability through the normal reflection notification API. |
| `set` | `key`, `value`; owner-only service write, normal public API. |
| `output` | `to`, `value`; store script observation, also record an ordered output event. |
| `read` | `key`, `to`, `mode`; default property access enforces inject; `mode: "get"` calls unchecked Get, optional `strict` (default true); `mode: "config"` calls tracked service config merge with optional `base`,`head`. |
| `update` | `fiber`, `config`, optional `await` (default true), `task`; `mode: "noSave"` suppresses commit. Async false starts without waiting, allowing coalescing. |
| `restart` | `fiber`, optional `await` (default true), `task`. |
| `rawConfig` | `fiber`, `config`; retain input without publishing effective config/restarting. |
| `resolveConfig` | `fiber`, `config`, `to`; resolve candidate only, do not publish. |
| `dispose` | `fiber`, optional `await` (default true), `task`. |
| `selfDispose` | inside Apply only, optional `await` (**default false**). Request without await and awaiting are different scripts; awaited risk is a bounded diagnostic, not supported-runtime equality. |
| `effect` | `label`, `program`, `cleanup`; execute body immediately with owned cleanup. Async setup is awaited unless `await:false` starts it tracked for teardown joining; optional `to` names the disposer. Cleanups keep their semantic order as observed, never sorted. `mode:"partial"` yields each disposer then fails, testing partial unwind. |
| `withdraw` | `task` names an effect/provision disposer, optional `await` (default true), `to` for tracking; a second invocation tests single-shot behavior. |
| `awaitFiber` | `fiber`, optional `await` (default true), `task`; invokes Await, which can settle PENDING or reject FAILED, not an ACTIVE assertion. |
| `inspect` | `fiber`,`to`, `mode:"effects"` records owned effect labels; `mode:"configIdentity"` observes whether effective config is the exact last Apply config object. |
| `lateEffect` | `fiber`, `label`; create an effect on a previously disposed/inactive context; expected INACTIVE_EFFECT is observable via `catch`. |
| `fail` | `error`; throw stable fixture error. |
| `listen` | `key`, `label`, `program`, optional `prepend`,`global`,`symbol`; listener executes program and returns the last `return` result. |
| `dispatch` | `key`, `mode` (`emit`,`parallel`,`serial`,`bail`,`waterfall`), `to`, optional `await` (default true), `task`,`symbol`; waterfall's builtin next records `inner` and returns `value`. |
| `return` | `value`; listener return value. |
| `next` | waterfall listener only, `to` optional; call the supplied continuation; a second next must error. |
| `awaitTask` | `task`; await a named external request. |

`catch:true` stores errors as `{name,message,code?}` at `to` and appends an
`error` event instead of throwing. A fixture may deliberately fail startup;
`settle` records the exact FAILED state and error event. Unexpected interpreter
errors fail the run; they do not become compatibility passes.

Plugin `update` is empty/default for the normal restart continuation, `retain`
for an `internal/update` listener veto, or `reject` for stable fixture rejection.
`unchanged` installs a fixture-owned internal/update listener that deep-compares
candidate/effective config, vetoes unchanged input and delegates changed input.
This is an explicit plugin hook policy, not an invented implicit kernel guarantee.
An `internal/update` `listen` body receives candidate config in `$config`.
`configTransform` registers a global internal/config shallow merge targeted only
at this plugin before activation. Config/update/commit observers are recorded.

## Ordered observations

Events are one monotonically numbered array. `plugin`, `status`, `service`,
`apply`, `cleanup`, `mark`, `gate`, `output`, `config`, `update`, `commit`,
`listener`, `inner`, `error`, and `selfDispose` are distinct event kinds. A
status carries `{fiber,from,to}`. Service notifications carry `{key,value}`.
No observer mutates runtime state. Plugin publication identifies fibers by
runtime callback metadata before their initial synchronous event.

Checkpoints list fibers in mount order, provisions in insertion order, and
outputs as a map. They include raw state, revision, the current target epoch
(null when inactive), required provision UIDs and each retained effective config.
The oracle reads diagnostic fields without changing them. Configs and values are
snapshotted, not held as mutable references. `needs` object key order is the
original JSON insertion order (ECMAScript `Object.keys` rules); it becomes epoch
`needs` array order. A Go adapter must preserve this using raw JSON token order
or an ordered decode, not iteration over `map[string]any` or sorted re-encoding.
Registry/fiber insertion order similarly determines notification traversal.
These order requirements are not permission to sort comparison traces. Scripts may assert an RFC 6901 JSON
pointer equals a JSON value at a named checkpoint. Assertions are independently
checked against observations; they are **not** fed into candidate execution.

Normalization is limited to bijective `fiberUID` / `provisionUID` renaming in
first-observation order and the
built-in service key `events` -> `hooks`. It never sorts arrays, removes semantic
events, merges state transitions, normalizes values, strips errors or erases
restart boundaries. Extra/missing events and a changed epoch equality relation
are mismatches. Opaque IDs use distinct namespaces. Error stack/path/timing data
is not injected into semantic traces in the first place.

## Owner-approved causal-comparison sidecar

ADR-035's [preexecution amendment](../../docs/design/s0-causal-comparison-amendment.md)
adds a separate `CompareCausal`, not a replacement for the strict rules above.
Complete raw trace v1 arrays retain their arrival/insertion order. Strict results
and causal results are recorded independently; historical mismatches stay strict
mismatches.

A `s0-causal-witness/v1` sidecar binds exact script/trace bytes by SHA-256 and
records `{label, afterEvent}` at each actual checkpoint observation. The oracle
opt-in is `S0_CAUSAL_WITNESS_PATH`; the Go interpreter records the native owner
watermark from the same Inspect operation and projects only that ledger prefix.
No inline semantic event is fabricated. Missing/invalid hashes, cuts or unsupported
control syntax are not a match. Hashes identify bytes, not signed authenticity.

Version 1 compiles a conservative static resource/dependency model. It checks
complete event content/multiplicity, each fiber's order, root command/conflict
order, states/epochs/config/outputs, global bijective identities and demonstrated
independence for any provision-array interleaving. Two startup exceptions are
proved per trace: pure mount publication can cross unrelated callbacks, and a
readiness notification bracket follows the provider that actually makes a
mounted consumer ready. These exceptions do not erase teardown, dependency or
error obligations. Gate/task/child/availability/dynamic constructs without
sufficient witnesses remain explicitly unsupported; no scenario is silently
removed or replaced by its final state. Negative comparator tests and actual
same-script oracle/candidate runs are separate required evidence.

Historical receipts bind their preserved exact interpreter source under
`oracle/history/`; new executions bind the current interpreter and their own
sidecars. Frozen corpus files/receipts are not overwritten when instrumentation
changes. No source hash alone turns a failed or unexecuted test into acceptance.

## Diagnostic and process boundaries

The required isolated runtime is Node **24.22.1** via environmental `S0_NODE`.
The resolver maps the exact five vendored built packages, rejects every other
bare import, `.node` file and `internal/*` access, and never reads real DSH package
resolution. No npm scripts, source transformation, native loader, mutable symlink
or real DSH tree is executed/modified.

A normal scenario has an external process timeout guard. D26 intentionally
awaits its own disposal from Apply; a bounded watchdog records
`SELF_DISPOSE_AWAIT_BLOCKED` with the still-observed LOADING state. This is an
identified static await cycle, not a claim that the runtime supports awaiting it.
Only D26's diagnostic has a clock bound; lifecycle scripts use gates, never
sleep-based ordering. Parent Go candidates keep adapted strict LIFO, deadlines,
leases, key classes and hook audience tests separate from raw oracle equality.
