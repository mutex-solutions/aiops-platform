# ADR-059: Scope and working rules of S1 (loader and launcher)

- Status: **accepted 2026-11-20** by the project owner.
- Scope: roadmap step S1 (`08-profiles-roadmap-adrs.md` §2.1). It settles what
  the roadmap leaves open for S1 and changes no decision of `00-decisions.md`.

## Context

S1 builds the launcher (`boot`), the `loader` plugin, the core rows `reload`,
`restart-policy`, `readiness` and `config-refs`, and the two suites that are
its exit: the failure matrix (process-scope columns) and the recomposition
suite, both on the built binary. Reading the S1 text against the later steps
shows gaps: some matrix cells need parts that later steps build; the gate and
`plugin.Define` are S2a; `secrets` is S3; `clock` is in no step; the loader
needs five small kernel additions; and nothing says how a test drives the built
binary. S0 was not yet closed when S1 work began.

## Decision

1. **S1 starts beside the last S0 gates.** S0 closes only on the owner's
   approval of the S0 report. No kernel change reaches the main branch until
   the S0 gates on the final port are recorded. A kernel change forced by a
   failing gate takes priority over S1 code.
2. **Five kernel additions are approved**, developed on a branch and merged
   after the S0 gates are recorded, then verified by the differential corpus,
   the property workload and the call-path gate again:
   a child mount as a batch operation; an update operation that carries a new
   config; a dependency built from an untyped key; the intercept config type of
   a key; and the three kernel events `internal/plugin`, `internal/status` and
   `internal/service` dispatched with the ledger event as payload, with a
   fiber lookup on the `registry` built-in. Their exact shapes are recorded in
   ADR-034 when they merge. The kernel stays inside its 8,200-line cap.
3. **Matrix cells carry the step they start at.** Each cell of
   `test/failure-matrix/matrix.yaml` has a `since` field. S1 must pass every
   cell marked S1 and may skip none of them; a cell of a later step reports
   "not yet" and never counts as a pass. A test keeps the file and the table
   of `08` §1.6 in agreement.
4. **The gate has ten named slots from S1.** S1 implements the checks that
   read S1 data (bindings and layer rules). The others report "not evaluated"
   until the step that builds them.
5. **The recomposition suite runs four of its five cases at S1.** The
   promotion case waits for S4b.
6. **One YAML library, in `boot/` only**: `go.yaml.in/yaml/v3`, pinned and
   entered in the licence register. An analyzer forbids it outside `boot/` and
   `tools/`. The loader and the kernel see only the parsed tree.
7. **`reload` reaches composition through a launch service** that `boot`
   implements (compose, admit, plan). A plugin never imports `boot`.
8. **A minimal `clock` definition** (`sdk/def/clock/v0`, with a fake and one
   row) is built at S1 so that `restart-policy` is testable in time.
9. **Tests drive the built binary through two test-only rows** of the test
   distribution: one submits a generation named by a control file, one writes
   the committed state as JSON. Neither row exists in a shipped distribution.
10. **Volatile fields are listed in the module table entry** until S2a derives
    them. A `secret:` reference fails with "does not resolve" until S3.
11. **Line budgets of `boot/` and `loader/`** are fixed at the S1 exit as the
    measured count plus ten per cent, each one package, enforced like the
    kernel's.

## Consequences

- The S1 exit is decidable: a fixed list of cells and four recomposition cases.
- `08` §1.6 and §2.1 are edited with the first S1 commit that adds the matrix
  file, so the file and the table cannot disagree.
- The kernel grows by about 230 lines and is measured again after the merge.
- The root module gains its first parsing dependency; the kernel module still
  has none.

## Decisions of 2026-11-20 on the failure matrix

The failure matrix (`test/failure-matrix/matrix.yaml`) ran red at the S1 exit:
twelve cells did not do what the table says. The project owner decided each
case on 2026-11-20, and the matrix passes every cell marked S1 with no known
deviation.

12. **A live generation whose verdict fails is rolled back.** `reload` applies
    the previous composition again with one inverse plan through the loader,
    settles, judges that the previous rows are ACTIVE again and reports the
    operation as `rolled back`, with the failed verdict and the rollback outcome
    on the result. If the rollback itself fails the status is `rollback failed`
    with both verdicts, it is not tried again and readiness follows the real
    tree. There is still no committer, lock or audit (S4a), and
    `composition/applied` is published for the failed generation as N and for
    the restored one as N+1.

13. **A drain past its deadline is a leak, and a leak replaces the process.**
    The kernel marks a fiber leaked when its drain deadline passes with a tracked
    goroutine or a lease still open, as it does for a disposer past the dispose
    deadline, and exports `ErrLeaked` to match it. The launcher drops readiness on
    either kind of leak and exits to be replaced after its grace period, in every
    column of the table; the kernel details are in ADR-034, "S1 additions".

14. **At boot an unmet required set stops boot.** The launcher keeps exiting
    non-zero at the required audit, so the cells of the column "Required at boot"
    for a failed tracked goroutine, a leak and a `restart-policy` that gave up say
    "Stop". "Readiness fails" is the outcome of the column "While running" only,
    and of a required row of a live generation that fails after the generation
    passed.

15. **A live generation with an unknown or denied row is rejected whole, and the
    required set follows the applied generation.** The detach rule applies at boot
    only; `reload` rejects a live generation at the admit stage with nothing
    applied. After a generation passes, and after a rollback, `reload` calls
    `Commit` on `profileContext`, so `Committed` and `Required` are those of the
    applied generation (its rows with `required: true` plus the distribution's
    set); the six launch keys do not change.

## configRefsReady (recorded 2026-11-21)

The launcher gated the rows whose config holds a reference on a key it provided itself
and switched from an observer of the kernel's events. The observer runs apart from the
tree, so the key lagged `config-refs`: a row mounted while `config-refs` restarted got
the unresolved reference, a settled tree still had reference rows PENDING, and the static
graph, the restart closure and the required audit did not know the key. Recorded here:

- `config-refs` provides `configRefsReady` once its listener is on `internal/config`.
  The key is provider-bound to `aiops/config-refs` in the distribution manifest; the six
  launch keys do not change.
- The launcher gives every row whose config holds a reference, other than `config-refs`
  itself, a row-level `inject` of the key. Such a row is applied only while its
  references can resolve and unloads in the owner turn in which `config-refs` quiesces.
- The static graph sees that need. A required row with a reference needs `config-refs`
  in the required set (gate check 7, `08` §1.6), two `config-refs` rows in one realm are
  two providers, and the restart closure of `config-refs` holds the rows with
  references. With no `config-refs` row such a row stays PENDING and waits for the key.

## Line budgets at the S1 exit

Decision 11 is applied. The caps are the measured count at the S1 exit plus ten per
cent, rounded up to the next hundred, in physical lines of production Go (comments
and blank lines included, tests excluded), measured by `tools/kernelcheck lines`
and enforced in CI and in the Makefile; the provisional caps are gone.

| Package | Measured | Files | Cap |
|---------|----------|-------|-----|
| `boot` | 5,707 | 18 | 6,300 |
| `loader` | 1,000 | 4 | 1,100 |

Growth of either package past its cap, or a second package under `boot/` or
`loader/`, needs an ADR.

## Owner decisions of 2026-11-21

The project owner decided the following on 2026-11-21. Where a sentence above, or in
`07-c4-plugin-sdk.md` or `08-profiles-roadmap-adrs.md`, says otherwise, these decisions
win.

16. **A live reference is checked at admission.** `config-refs` provides a dry-run
    definition key that reports whether every reference of a raw config tree resolves and
    never returns a resolved value. `profileContext.Admit` calls it for every row a
    live generation creates, enables or changes, and for the children of a group it
    replaces; a reference that does not resolve, or any reference in those rows while
    `config-refs` is not ACTIVE, rejects the whole generation before anything is touched.
    The live cell "Rejected; the row keeps its instance" of `08` §1.6 therefore holds for
    created, enabled and changed rows, with no kernel API added.

17. **A volatile-only change that holds a reference restarts the row.** When the old or
    the new value of a volatile field holds a reference, the loader does not commit the
    change in place but restarts the row, as it already does. `boot.Diff` and the loader
    read this one rule from the loader definition, so `loader/volatile-update` never
    carries an unresolved reference.

18. **A required row with a reference pulls `config-refs` into the required set.** When a
    required row of the process's role holds a reference, the launcher adds the
    `config-refs` row and the key `configRefsReady` to that process's required set, and
    that need does not count against the required-closure clause of gate check 7. The
    release `dist.yaml` keeps its required set and the test distribution no longer lists
    `configRefsReady`, so the suites run the release behaviour. `configRefsReady` belongs to
    the launcher's wiring of references, not to the bindings a profile chooses among, so
    the last-provider rule never refuses a profile that disables its `config-refs` row;
    this replaces the rule of the section "configRefsReady" above that such a row needs
    `config-refs` in the distribution's required set.

19. **A row that failed during the apply fails the verdict.** If a row that a live
    generation created, changed or enabled is FAILED at any point between the apply and
    the judge, the verdict fails and the generation is rolled back, even when the row is
    ACTIVE when judged. `restart-policy` does not retry the rows of a generation in
    flight: it holds its retries until the operation's final event and then acts on the
    real state after any rollback, so the verdict never depends on retry timing.

20. **A read-only kernel accessor reports key classes and sealed hooks.** The sixth kernel
    addition of S1 exports read-only functions, in the style of `InterceptType`, that
    report the provider-bound and consumer-bound classes a key declared with `NewKey` and
    whether a hook is sealed (ADR-034, S1 addendum). Gate check 7 derives the classes of
    keys and hooks from the module table's definitions through them, so `dist.yaml` lists
    only who is allowed (bindings and listeners), never a class. The kernel stays inside
    its 8,200-line cap and at 100 per cent coverage.

21. **A launcher observer registers no effect on the root.** `Root.Observe` adds no
    cleanup effect to the root, so a resync after an overflow leaves nothing behind. The
    effect of the root observer is not part of the contract a plugin sees, and the S0
    adapter test no longer compares it with the observer effect of the prototype
    (ADR-034).

22. **`restart-policy` keeps no records across its own restart at S1.** A restart of
    `restart-policy`, because its config changed through `reload` or its clock provider
    restarted, starts its records afresh; durable records come with the durable store of a
    later step. The records of a rolled-back generation's candidate config never reach the
    restored incarnation of a row, even when that generation also restarts
    `restart-policy`, and a restart of `restart-policy` never moves the start of a tracked
    fiber's stable period in a way that changes a give-up decision within one activation.

23. **After a failed rollback or an abandoned applied generation the committed state is
    unknown.** `reload` marks its state unknown after `rollback failed`, or after it
    abandons a generation it already applied: it says so on its `reload/operation` event,
    refuses every later `Submit` with a typed error until the process restarts, and never
    diffs against a composition it is not sure the loader holds. `readiness` drops the
    latch and keeps it dropped while the state is unknown, until the restore of S4a
    replaces this rule.

Also accepted on 2026-11-21: a change of the `reload` row itself is applied after its
generation's verdict passed and is not judged.

## How decisions 16 to 23 are built (recorded 2026-11-21)

- **16.** The dry run is the definition `sdk/def/configrefs/v0`: the key
  `configRefsDryRun`, provider-bound to `aiops/config-refs`, whose `DryRun.Check(ctx,
  config)` returns nil when every reference of the tree resolves and otherwise an error
  matching `ErrUnresolved`, never a value. `boot` may not import it (`07` §1.1, `kernelcheck
  imports`), so it reads the active provision of the key owned by `aiops/config-refs`
  through `Root.Inspect` and calls it through a local interface with the same method.
  `Admit` checks every row the generation mounts anew: created, enabled, changed in a field
  other than `required` or `roles`, or a child of a replaced group. A row with no reference
  is decoded as before. A generation that both changes the `config-refs` row and adds
  references is checked by the instance that is running.
- **17.** The rule applies to the row's whole config: any change of a config whose old or
  new tree holds a reference anywhere restarts the row, because the payload of
  `loader/volatile-update` is the whole config. `loaderdef.VolatileOnly` is that rule and
  `loaderdef.HoldsRef` its predicate; `boot.Diff`, the loader and `reload` read them.
- **18.** `RequiredFor` adds the role's `config-refs` rows and `configRefsReady` when a
  required row of the role holds a reference; the need of `configRefsReady` is exempt from
  the required-closure clause of gate check 7, and `configRefsReady` and `configRefsDryRun`
  are exempt from the last-provider rule of gate check 8.
- **19.** `restart-policy` holds the retries of the rows the in-flight plan lists (its
  entries other than unchanged, plus the closure) and maps a failed fiber to its row
  through the composition key. At the final event a fiber still FAILED is retried or given
  up on, and one restarted meanwhile is tracked afresh. Rows outside the plan are retried
  as before. A FAILED read stays FAILED in the kernel until an update, so the verdict sees
  every failure of the generation.
- **20.** Gate check 7 takes classes from `kernel.KeyClasses` over the keys `boot` knows
  (launch, loader, composition and the kernel built-ins) and every key a plugin of the
  module table provides or injects, and seals from `kernel.HookSealed` over the kernel and
  loader hooks. Where no definition is at hand, an entry of `modules.yaml` or a hook known
  only by its name, a listing in `dist.yaml` stands for the class, which is the only class
  the kernel accepts one for.
- **22.** The final `reload/operation` event names the rows the operation restarted or
  created, forward and in rollback, and `restart-policy` tracks their fibers afresh. After
  a restart of `restart-policy`, a pending, held or given-up record whose fiber is not
  FAILED is forgotten, and the start of an ACTIVE period it already knew is kept. `stable:
  0` means no stable period.
- **23.** `reload.ErrStateUnknown` is on the result and the final event after `rollback
  failed` or an abandon after the apply. Every later `Submit`, and an operation already
  queued, is refused with it; when the unloading activation cannot emit the final event,
  the row's next activation does. `readiness.New()` shares the knowledge across the
  activations of its row, so a restart of `readiness` keeps the latch dropped.
- **The `reload` row's own change.** When the loader refuses it after a passed verdict, the
  operation stays `passed` with the refusal on its result, and the next passing generation
  applies it. Operations queued behind a restart of the `reload` row run in its next
  activation.
- **Loader.** An entry with an empty id is refused with `ErrEmptyID`. A group activation
  never creates an entry whose previous fiber is not DISPOSED; the plan's second batch
  mounts it. The replacement vet covers every fiber a plan retires, the children of a
  replaced group included, and an earlier commit of an entry the replacement remounts is
  reported as `restarted`.
- **Launcher and vet.** `--refs-dir` must name an absolute existing directory, or the
  command line is refused. Plugin vet refuses any import of `net/http/pprof` and `expvar`,
  and `time.AfterFunc` in `boot` needs a `//kernelcheck:allow-go` reason.
