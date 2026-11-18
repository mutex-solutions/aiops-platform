# S0 D23 native request/completion mapping

Preexecution method record for frozen `test/differential/corpus/D23.json`.
This records the owner-approved [request/handle same-lane API](s0-native-effect-scope-proposal.md),
not a source-equivalence pass or a waiver of checklist row 14. It supplements the
[existing fixture controls](s0-fixture-effect-controls.md), whose D23 limitation
predates the native scope implementation. No frozen script, assertion, threshold,
or comparator is changed.

## Request on the callback lane, observe completion externally

D23 requests both scopes from Apply. `RequestEffectScope` reserves an actual
native ownership slot; Apply must return before that fiber's transition worker
runs scope setup or rollback. The adapter must never call `AwaitSetup` or `Await`
from Apply or from a scope callback on that same lane. It must not move callbacks
to a replay goroutine to imitate synchronous source generator execution.

The minimal parent-owned replay seam is:

1. Forward `RequestEffectScope(label, setup)` from composition's captured native
   context and from `composedActivation`, without adding authority or scheduling.
2. Add `effects *effectRequests` to `composedFrontend`. Its contents are native
   handle references and pending **observations**, never disposer callbacks,
   resource ownership, cleanup state, or a second cleanup ledger.
3. After a successful external native `Settle`, call
   `frontend.flushEffectCompletions()` before capturing that settle checkpoint.
   External `inspect` with `mode: effects` calls the same observer before native
   inspection. Neither seam executes fixture actions on behalf of native cleanup.
4. The observer calls each actual handle's `AwaitSetup` for setup observations and
   `Await` for requested withdrawal observations, with the external harness
   context. Every nonnil native result is retained as a `RuntimeFailure`. A caught
   effect error/output is emitted only after actual setup **and rollback** have
   completed. Unknown errors, cleanup failures, panic causes and cancellation
   must remain visible; a failed wait is never turned into a manufactured success.

The observer does not resume an arbitrary source continuation. A callback action
that needs a caught error output still pending native completion is unsupported;
no guessed error output may be installed to make that continuation execute.
Uncaught asynchronous setup semantics or unsupported handle/task combinations
likewise remain unsupported rather than being presented as source behavior.

## Actual incremental collection and single-shot withdrawal

For `mode: partial`, each frozen cleanup instruction is individually registered
through the actual native scope's `Defer`, in source registration order. The
setup then returns the error constructed from the action's `error` field (or the
source fixture's default), not from an expected assertion. Native scope rollback
runs admitted disposers in reverse order with the native cleanup budget. Native
metadata retains the root and its admitted children until callbacks return.

The ordinary `single-shot` instruction reserves a separate actual native scope
and registers its cleanup with native `Defer`. The fixture name `single` stores
only the returned `*EffectHandle` reference. Each frozen `withdraw` calls that
same handle's real `RequestWithdraw`, including the second call. Idempotence,
cleanup scheduling, once-only execution and removal from ownership belong to the
native core; replay must neither suppress the second request nor run cleanup.
Nested setup, where supported, must use native `EffectScope.Scope`, not an
adapter-composed nested cleanup list.

`inspect effects` selects the actual mounted fiber by its native ID and projects
labels in native `Snapshot.EffectTree` order. An observed empty array is evidence
only when the actual native tree is empty; it is never inferred from a withdrawal
request, fixture labels, handle state, or a golden/expected value.

## Evidence classification and limits

Both cores must run every original D23 checkpoint assertion unchanged: ACTIVE at
`unwound`, actual empty effect ownership, and the actual caught setup error. Tests
must additionally show reverse rollback order and exactly one `once` cleanup,
retained native ownership while cleanup is gated, and no premature caught output.
Full-trace diagnostics retain actual events, actual errors, actual inspections,
the unchanged oracle trace and provenance, and this method record's digest.

The approved adaptation is **callback request / external completion observation**.
Its caught error/output can appear later than the synchronous source generator's
error, including after the native ACTIVE transition or single-shot cleanup. That
is an honest raw ordered-semantic mismatch. No event sorting, checkpoint-value
injection, expected-value reads, scenario exclusion, or raw-difference masking is
permitted. A passing checkpoint test is not a strict oracle parity claim, final
S0 acceptance, or evidence that arbitrary source async continuations are mapped.
