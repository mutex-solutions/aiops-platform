# S0 comparison amendment 3 — addendum before evidence runs

Status: written after [amendment 3](s0-causal-comparison-amendment-3.md) (SHA-256
`45ae806bd4def4f5ce0a8345aec8e6ac3233913bc2c2a46b74abace0db4ea17f`) and its
[freeze](s0-causal-comparison-freeze-3.json), and **before** any recorded run,
receipt or table that uses the new rules. Amendment 3 itself is not edited. Like the
addendum of amendment 2 this text only adds the ability to prove and weakens no
existing check.

Process, stated plainly. Amendment 3 was committed first (`c3786da`) and the
comparator was written afterwards. While writing it, one rule of the amendment turned
out to be misstated (B1) and several rules the code needs were not named (B2 to B12).
They are named here, before the implementation is committed and before any
recorded run. Development checks ran before this text was committed and are not
evidence: the pinned-oracle traces of the 14 targets compared with themselves, the
unit tests of the next commit, and unrecorded live probes (3 to 5 runs per
scenario, results kept in a scratch directory and discarded). No probe result
changed a rule: every rule below was already in the code when the first probe ran,
except B5 (one more check on the same construct, added after reading the
unrecorded probe output, which had no failure to fix). No receipt in the repository
comes from a probe.

## B1. Correction of K8: a resolved unawaited dispatch still writes its cell

Amendment 3, K8 "Completion events", says that a dispatch with `await:false`
produces no completion events, "resolved or rejected". That is wrong for the
resolved case. In the oracle (`replay.mjs`, `dispatch`) an asynchronous dispatch is
`track(Promise.resolve(result).then(v => put(action.to, v)), task)`: the `put` is
part of the task chain whether or not the root awaits it. The recorded D33 trace
shows it (`output zero-result` is produced by the dispatch with `await:false`).
Corrected rule: a resolved dispatch with `to` produces `output {to}` in the
dispatch lane, awaited or not, after the dispatch finished; a rejected awaited
dispatch with `catch` produces `error {op:"dispatch"}` then `output {to}`; a
rejected unawaited dispatch produces no event (its task is rejected, K6.7 decides
the root-side events). The sentence of amendment 3 that says otherwise is withdrawn
and nothing else of K8 changes.

## B2. Completeness at the end of a trace and at every cut

Amendment 3 maps events onto plans but does not say that the plans must be
completed. Every trace must show: every planned item of every hook and chain lane
(so a listener, gate, mark, output, inner or completion event missing on both sides is
rejected); every command bundle of K5/K6 complete, checked at each checkpoint for
the commands issued up to it and again at the end; every root provision published
once; every expected root `output` event (as many as the script runs the command
that writes the cell, K6.7) and every expected root `error` event (`awaitFiber`,
`awaitTask`, `lateEffect`) consumed. At a settled (`settle`) cut every task issued
so far is complete (K6.6) and no update request is pending (K6.3).

## B3. How an event is attributed to a lane

An event of kind `listener`, `gate`, `mark`, `output`, `inner` or `error
{op:"dispatch"}` whose label, cell or key appears in a planned lane belongs to the
lane that has it as its **next** planned item; among several, to the first whose
requirements hold (predecessor lanes and awaited waits, K8 relations, and for a chain
lane its command's bundle). If no lane has it next it is a proved violation
("repeated, late or out of its planned order"), never an unproved owner. A
`listener`, `mark`, `gate` or `inner` event of a listener the plan does not run (after
a bail value, a veto, a failure, or a key dispatched before it was registered) is a
proved violation. An unplanned label of no listener keeps the old behaviour
(`ErrCausalUnsupported`).

## B4. Lane actors

A hook, dispatch or chain lane is not a fiber: the rule "no event before the fixed
mount publication" does not apply to it. Dispatch lanes and chain lanes are linked to
the root command lane for conflicts; a chain lane is linked to its fiber. Two lanes of
one dispatch are linked only through the K8 relations. No other conflict is added or
removed.

## B5. K3 at a settled cut

A fiber that made its own disposal request must be `DISPOSED` at every following
`settle` checkpoint. (K3.2 and K3.3 already require the teardown to follow the
request; this adds that the settled cut proves it ran.)

## B6. Shapes of root commands that stay outside K5 to K7

An awaited `update` whose continuation can restart the target (default policy, or
`unchanged`) must be followed immediately by `settle` (the amendment-1 witness rule is
kept). An awaited `update` of a fiber with an `internal/update` listener whose chain
reaches a gate is `ErrCausalUnsupported` (the root would wait for a release it has not
issued). A policy update needs the target stable (no unsettled startup or mutation
since the last cut). An `update` after a `rawConfig`/`inspect`/`resolveConfig` of the
same segment, a root `restart` on a new-shape fiber, an `update` configuration that is
not pure JSON on a new-shape fiber, and a root command on a self-disposing fiber are
`ErrCausalUnsupported`. The field set of a root `update` is `op fiber config await
task mode` for every fiber (another field is unsupported); `listen`, `dispatch`,
`awaitTask`, `selfDispose`, `lateEffect` have the field sets of the amendment tables.

## B7. Detail of K5.2, K6.3 and K6.4

For a new-shape fiber an `UNLOADING` from `ACTIVE` or `LOADING` with no pending
request, disposal, own failure or retiring bound provider is a **mismatch** (not
unproved), as is an `UNLOADING -> LOADING` with no pending request. The check that a
loading frame's `config` equals the latest raw configuration, and that the
following `apply` carries the same value, is made only when the plugin has no
`configTransform`; with a transform the `apply` of a restart requested by an
`unchanged` update must carry that update event's `config`. Raw input is retained at
the command's `config` event even under `noSave`; the checkpoint raw configuration is
the last command configuration, and the revision is the number of restart requests.

## B8. Static evaluation of listener programs

Return values and `mark`/`output` values in host and update listeners must be pure
JSON literals. `when` compares two literals, or the candidate configuration path
`{"$config":path}` with a literal, by JSON equality (a missing path is undefined, the
marker `{"$undefined":true}`). `return` ends the body; `fail` ends it as a rejection.
The fall-through value of a non-waterfall body is undefined (not bailed). A bail value
is any value but `null`, `false` and undefined.

## B9. K4 and K2 details

`lateEffect` is accepted in an effect cleanup program of a root-mounted fiber and as a
root command; the `error` event of the cleanup form counts in the effect's cleanup
program order like any other event of that program (single-shot per teardown). A root
`lateEffect` may follow an awaited root disposal (the check of A1 of the amendment 2
addendum is applied to it). A second `service` event for a root-provided address is
handled by the notify rule of amendment 2 (C4.4) and attributed to the root lane.

## What did not change

Capability list, not-added list, out-of-scope list, the 100-of-100 match rule in both
modes and the rerun of all scenarios stay exactly as in amendment 3 and its freeze.
