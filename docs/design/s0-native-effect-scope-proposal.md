# Native effect-scope/handle — concrete S0 proposal

Status: **concrete request/handle API and same-lane semantics owner-approved**.
The owner explicitly selected “Duyệt request/handle cùng lane” before native
implementation. Feasibility, full differential/property proof and final SDK
acceptance remain pending; no implementation is selected by this approval.
Existing `Effect(label, setup) error`, `Defer` and `Go` remain unchanged.
This proposal adds actual incremental ownership/rollback, nested metadata and
single-shot withdrawal; it is not a replay-owned disposer table.

## Owner-scheduled scope operations

```go
func (c *Context) RequestEffectScope(
    label string, setup func(*EffectScope) error,
) (*EffectHandle, error)

func (s *EffectScope) Defer(label string, d Disposer) error
func (s *EffectScope) Scope(label string, setup func(*EffectScope) error) error

func (h *EffectHandle) AwaitSetup(ctx context.Context) error
func (h *EffectHandle) RequestWithdraw() error
func (h *EffectHandle) Await(ctx context.Context) error
```

Admission validates the exact live activation and reserves a native ownership
slot/token before returning the handle. The fiber's existing transition worker
executes setup and all cleanup, outside the state owner. A request from Apply is
not synchronously awaited there: pending scope setup/withdrawal runs after Apply
returns, before ACTIVE. An ACTIVE fiber starts one transition-work item when its
worker is idle. `Settle` joins pending scope work. No idle per-scope goroutine.

Completion waits are external-only, following ADR-034's request/completion rule.
Callbacks must not await work queued behind themselves. No goroutine-ID heuristic
or silent callback execution on the caller goroutine is used. Scope setup cannot
secretly spawn tracked work and then call it transition-lane execution.

## Ownership and rollback

Each accepted `Defer` transfers an actual disposer into the scope's native tree.
Nested Scope creates an effect child, not a fiber. Setup seals before completion;
a later registration is refused. Errors or panic roll back every admitted child
in strict reverse order, one at a time, using the existing dispose deadline.
Panic is reported as a typed scope error retaining its value/cause. Cleanup
errors are joined; later cleanup still runs. Deadline expiry reports blocked
work; the worker and metadata stay owned until callbacks really finish.

`RequestWithdraw` is idempotent, marks native work pending and never runs user
cleanup inline. Manual removal and unload share the same slot/state/worker; the
resource is cleaned exactly once. Metadata is removed only after actual cleanup,
not at request acknowledgment. `Await` observes that same real completion.

The scope API owns registration/disposer resources only. It does not withdraw
service provisions or bypass dependent draining. A later provision-handle design
would need its own reviewed contract. Native root/user ownership and entry/key
bindings remain unchanged.

## D23 mapping and validation

The frozen JavaScript syntax remains unchanged. Its collector/throw/withdraw
operations map to owner-scheduled native scopes, not raw synchronous generator
execution. Setup errors are observed through AwaitSetup after the callback that
requested the work has returned. The Go fixture must record this exact adaptation
under checklist row 14 and retain complete raw trace differences. It may not fill
expected outputs before actual setup/rollback completes.

Required tests: incremental setup error and panic after several acquired effects;
reverse order/once-only count; nested actual effect-tree snapshots; withdraw twice
inside Apply without self-await; withdraw versus unload race; late setup refusal;
ignored cleanup cancellation with real ownership retained; multiple scopes with
no callback overlap; Settle joins actual completion; same-label handles have
separate identity; root/child/provider retention. Both schedulers must implement
these obligations before source comparison/final-port acceptance.

## Unchanged constraints

G1–G5, the three deadlines, no call-path mutex, exact error/identity/event proof,
full property/scale/cold-path criteria, standard-library-only final kernel,
per-file coverage and physical-line gate remain. This API extension does not
select a core, waive a scenario, approve SQL authority, or begin S1.
