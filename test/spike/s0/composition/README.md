<!-- SPDX-License-Identifier: Apache-2.0 -->
# S0 shared composition frontend

`composition` is a private, test-only Go primitive shared by both existing scheduler
candidates. It does not select a kernel or final SDK. `NewRuntime` wraps one backend
and builds the immutable `contextmodel.RootSpec` registry. Keep `RootAuthority` with
the trusted owner: `Start` attaches an actual registered entry; a child can inherit
that identity but cannot replace it by choosing an ID string.

Every mounted object is an actual backend fiber. Metadata dependencies compile to
backend string addresses preserving key name, major, and the opaque label identity
returned by `contextmodel.Context.Realm`. An authority-local construction map merely
encodes labels; contextmodel remains the sole realm/declaration implementation.
`Want` does not gate the parent or authorize reads. `When` mounts a required-dependency
child whose normal lifecycle follows arrival, consumer availability, and withdrawal.

Typed provisions store provider views without invoking them. Consumer availability
runs off-owner through native `Spec.Check` to gate target selection, not each retained
read. `Resolve` uses the actual captured backend binding, immutable per-call metadata
authorization, and the resolving wrapper context. Intercepts, entry identity, and scopes therefore belong
to that caller. `Scope.With` additionally requires successful native provision
ownership and an activation lease; a `Provides` declaration alone is insufficient.
`Spec.Config` and returned Apply disposers go through the native lifecycle APIs.

Child ownership is a native activation relation, not an idle watcher goroutine,
fake provision, or a cleanup callback that would deadlock behind its own dependants.
Parent teardown follows the oracle order: each child is one ordered effect of its
parent, requested for disposal at its slot in the parent's LIFO cleanup (after any
effect registered later, before any registered earlier) and joined after the walk,
so the parent is DISPOSED only after its children.
The gated child/provider cleanup test verifies that captured reads remain usable
while the child's teardown holds the parent in UNLOADING, and an inherited-dependency
child can start while its parent is still LOADING. All tests use target
`Await`, not package-wide `Settle`, and execute against both actual candidates.

These are adapted Go contract assertions, **not raw source parity**. In particular,
D06's oracle retains committed inherited injection after a child changes realm;
this frontend intentionally follows contextmodel's stronger realm-local declaration
rule: that read is undeclared, while an explicit requirement in the new realm stays
PENDING until a matching provider exists. The differential corpus and its assertions
are not changed. Other referenced surfaces are D01–D08 and lifecycle D20–D24; replay,
config/property parity, partial-effect machinery, and final SDK selection remain
outside this narrow package.
