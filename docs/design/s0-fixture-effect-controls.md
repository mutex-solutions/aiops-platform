# S0 fixture barriers and native Effect mapping

Preexecution method record for the existing frozen scripts; no expected values,
script bytes, numeric thresholds or strict comparator are changed.

## Control barriers are not plugin I/O

The oracle's fixture `gate(label)` records arrival and waits for the explicit
script release. It has no activation-cancellation input. It is a harness control
barrier, not a claim that plugin operations ignore `Std`. The Go fixture gate must
therefore wait for that same release or the external harness context—not substitute
an early activation-cancel result for the action after the gate. Native Std is
still canceled immediately, new effects/leases are refused, and load/drain/dispose
deadlines still report genuinely blocked work without reclaiming it. Separate
native tests prove cooperative cancellation and deadline behavior.

## Accepted Go Effect form

Go `Context.Effect(label, setup)` performs real setup under an activation lease,
registers a returned disposer in the native ledger, and synchronously cleans a
returned partial/late resource using the native disposal budget if ownership
cannot be retained. No fake effect list or guarded pre-owned slot is introduced.
For source async fixture setup, real tracked `Go` owns the work. The action joins
only its synchronous setup prefix through its first gate, so a fixture does not
invent a different prefix ordering merely because Go starts a goroutine. Actual
setup/cleanup completion remains native lifecycle work; no timeout is completion.

D22's original parent/child late-setup/cleanup checkpoint assertions are executed
unchanged; complete trace differences are retained separately. Strict Go LIFO
and quiesce can have explicit per-assertion obligations differing from raw source
parent/child callback ordering, but no whole scenario is excluded. D24's inactive
setup is refused before its callback runs. Only the directly identified native
inactive-effect sentinel set is encoded as `INACTIVE_EFFECT`; an error containing
an additional unknown/cleanup/panic/cancellation cause cannot be projected away.

This does **not** implement source generator incremental collection or the
withdrawable handle of D23. Adapter-composed cleanup is not evidence for those
missing native semantics. The full carried property and final SDK gate remain
open until implemented/proven, with owner review before any contract change.
