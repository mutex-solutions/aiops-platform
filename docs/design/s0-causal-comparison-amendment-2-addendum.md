# S0 comparison amendment 2 — addendum before evidence runs

Status: written after [amendment 2](s0-causal-comparison-amendment-2.md) (SHA-256
`214452407b234843ddcb0a1205d81bf9c60837d6048ff21f2656155228976c35`) and its
freeze, and **before** any recorded run, receipt or table that uses the new
rules. Amendment 2 itself is not edited. Development probes of the half-written
comparator (not recorded as evidence, results discarded) ran while this
addendum's facts were being discovered; they are the reason the points below
were found missing from amendment 2, and no receipt in the repository comes from
them. Like amendment 2 this addendum only adds the ability to prove, and
weakens no existing check.

Implementing C1 to C6 on the real scenarios showed that each needs a few more
general rules that amendment 2 left implicit. They are listed here so that every
rule the evidence depends on is named before the evidence exists.

## A1. A root command after an awaited root command inherits its completion (D18)

A root `dispose` with the default `await:true` returns only when the target is
`DISPOSED`. Every root command written after it, whatever its operation, is
issued after that. The existing rule ("a root disposal overtook an awaited
prior disposal") is extended from the next disposal to the next `mount`
publication: the `plugin` event of a root mount that follows an awaited disposal
must come after the `DISPOSED` status of that target. A non-awaited disposal
(`await:false`) imposes nothing on later commands. Rejects: a mount published
while the awaited predecessor is still `UNLOADING`.

## A2. Root commands while startups are in flight (D11, D13, D21, D22)

The frozen compiler refused a root `read`, `dispose` or `snapshot` issued while a
mount of the same segment had not been settled. With C1 such a command is
accepted only when a `waitMark` was issued since the last checkpoint (the
barrier is explicit). Safety comes from existing rules: a root read links the
root lane to every provider of the read cell, so its output event keeps its
position relative to every provider event; the value is compared by content; a
disposal's effects are lane events of the target; segments are compared with
multiplicity. A racy command without a `waitMark` stays `ErrCausalUnsupported`.

## A3. Registered-but-inactive provisions behind a gate (D11)

A `provide` executes during Apply, so a provider parked at a later gate has an
**inactive** provision in the store but no `service` event yet. A checkpoint
taken while that provider is parked (C1.5) must list exactly the addresses
provided before the gate in the body's program order, inactive, owned by that
provider, and no others. Their values are compared between traces, not
recomputed.

## A4. Program order and completeness of a body

For a plugin body without `when`, caught errors, partial effects, cleanup or
asynchronous effects (the "exact" bodies), its `gate`/`mark`/`output`/`child`
events must occur in exactly the program order of the operations, and the body's
events must all have occurred before the fiber becomes `ACTIVE` (Apply) or
leaves `UNLOADING` (cleanup). This is a validity rule on one trace, so two
traces that are wrong in the same way are rejected. Bodies that are not exact
keep the window, lane and content rules only.

## A5. Parked states beyond a fiber's own gate (D21, D22)

A checkpoint may show a fiber `UNLOADING` when its teardown cannot finish
because something it must join is parked at an unreleased, awaited gate: a
descendant with a pending asynchronous effect setup, or a bound consumer whose
own teardown waits at a gate (a provider is retained until its consumers stop).
The witness is the same as in C1.5 (arrival awaited by `waitMark`, release not
yet issued before the cut). Anything else stays `ErrCausalUnsupported`.

## A6. awaitFiber

`awaitFiber F` (root) means every later root command is issued after `F` is
mounted and neither `LOADING` nor `UNLOADING`. With `catch`, its `error` event
`{op:"awaitFiber"}` is a root-lane event that requires `F` to be `FAILED` and
carries the same obligations. A root disposal of `F` ends the wait obligation.
Rejects: a later root command running while `F` is `LOADING`, and an
`awaitFiber` rejection while `F` is not `FAILED`.

## What did not change

Capability list, not-added list (empty), out-of-scope list, the class-fold of the
D04 refusal message (C6.5), the 100-of-100 match rule and the
rerun of all 36 scenarios stay exactly as in amendment 2 and its freeze.
