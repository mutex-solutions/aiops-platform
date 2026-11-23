# ADR-059: Scope and working rules of S1 (loader and launcher)

- Status: **accepted 2026-11-20** by the project owner. S1 closed on 2026-11-23 on the
  owner's approval of `docs/design/s1-report.md`.
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

Decision 28 is applied at the commit that closes S1 (2026-11-23), after the merge of
the kernel additions, with the same rule; ADR-059, `ci.yml` and the Makefile change
together. The caps above are history.

| Package | Measured | Files | Cap |
|---------|----------|-------|-----|
| `boot` | 6,173 | 18 | 6,800 |
| `loader` | 1,100 | 3 | 1,300 |

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

22. **`restart-policy` may lose its records across its own restart at S1.** A restart of
    `restart-policy`, because its config changed through `reload` or its clock provider
    restarted, may start its records afresh; durable records come with the durable store
    of a later step. The records of a rolled-back generation's candidate config never reach the
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
  config, decode)` returns an error matching `ErrUnresolved` when a reference of the tree
  does not resolve, and otherwise the error of `decode`, the row plugin's own `Decode`,
  which it runs on the resolved tree inside `config-refs`; it never returns a value.
  `boot` may not import it (`07` §1.1, `kernelcheck imports`), so it reads the active
  provision of the key owned by `aiops/config-refs` through `Root.Inspect` and calls it
  through a local interface with the same method. `Admit` checks every row the generation
  creates, enables or changes in any field (`required` too, though raising it mounts
  nothing again), and every child of a replaced group. A row with no reference is decoded
  as before; one with a reference is decoded on its resolved tree, so a config that fails
  `Decode` beside or through a reference that resolves is rejected too. A generation that
  disables, removes or mounts again the `config-refs` row cannot be checked by the
  instance that is running, so any reference in those rows rejects it, as when
  `config-refs` is not ACTIVE; both errors match `launch.ErrRefsUnchecked`, which the
  launch definition, the one `boot` may import, exports for them.
- **17.** The rule applies to each changed path: a change whose old or new value holds a
  reference restarts the row, and a literal changed beside a reference that did not
  change is committed in place. `loader/volatile-update` carries the whole new config,
  so its `Config` is the tree with every reference replaced by nil
  (`loaderdef.WithoutRefs`); the event never carries one. `loaderdef.VolatileOnly` is the
  rule and `loaderdef.HoldsRef` its predicate; `boot.Diff`, the loader and the fake read
  them, and `reload` reads `HoldsRef`.
- **18.** `RequiredFor` adds the role's `config-refs` rows and `configRefsReady` when a
  required row of the role holds a reference. By decision 24, a row required by key
  counts as a required row here, so a reference held by an active provider of a
  required key in the root realm pulls `config-refs` in too, and a profile that then
  disables `config-refs` is refused at admission, where boot would stop later
  on that provider left PENDING. `configRefsReady` stays in the set when every
  `config-refs` row runs in another role, since `config-refs` serves its own process
  only. The need of `configRefsReady` is exempt from the required-closure clause of gate
  check 7, and `configRefsReady` and `configRefsDryRun` are exempt from the
  last-provider rule of gate check 8, so disabling `config-refs` is an optional choice
  while no row required by row or by key holds a reference.
- **19.** `restart-policy` holds the retries of the rows the in-flight plan lists (its
  entries other than unchanged, plus the closure) and maps a failed fiber to its row
  through the composition key. At the final event a fiber still FAILED is retried or given
  up on, and one restarted meanwhile is tracked afresh. Rows outside the plan are retried
  as before. A FAILED read stays FAILED in the kernel until an update, so the verdict sees
  every failure of the generation. For the same reason a rollback restarts every row that
  was ACTIVE before the generation and is FAILED when it starts, besides the inverse plan
  (a dependant that failed when the generation restarted its provider, a row whose change
  restarted nothing), through the loader's restart operation (`loader.OpRestart`, a
  retry); otherwise one transient failure of such a row would end in `rollback failed`
  and an unknown state. An update that changes nothing leaves a fiber as it is, FAILED or
  not, so a change the loader does not see (`required`, `roles`) never restarts a row.
  A rollback is judged against the state before the generation: every row that was
  ACTIVE before and every required row must be ACTIVE again, and a row that was not
  ACTIVE before (FAILED, given up on; PENDING, its provider absent) is a warning
  whatever the inverse plan did to it, so a failed attempt to change such a row is
  `rolled back`, not `rollback failed`.
- **20.** Gate check 7 takes classes from `kernel.KeyClasses` over the keys `boot` knows
  (launch, loader, composition and the kernel built-ins) and every key a plugin of the
  module table provides or injects, seals from `kernel.HookSealed` over the kernel and
  loader hooks, and from `HookUse.Sealed` over the hooks a plugin of the module table
  lists in `Plugin.Hooks`. An entry of `modules.yaml`, which `aiopsctl` reads and which
  holds no Go value, carries those classes (`classes: {providerBound, consumerBound,
  sealedHooks}`), so a render and the server's gate agree. Only a key or hook that no
  definition and no such entry names falls back to its listing in `dist.yaml`.
- **22.** Each activation of `restart-policy` starts its records afresh; its activations
  share only the reload operation in flight (`retry.Memory`), so the hold of decision 19
  lasts across a restart that the generation gives it. The new activation counts each
  fiber that is FAILED then as at its first failure, so a fiber given up on before the
  restart is retried. The final `reload/operation` event names the rows the operation
  restarted or created, forward and in rollback, and `restart-policy` tracks their fibers
  afresh, so the records of a rolled-back candidate never reach the restored incarnation
  when the generation did not restart `restart-policy` either. Within one activation a
  resync never moves the start of an ACTIVE period it already knows. `stable: 0` means no
  stable period.
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
  reported as `restarted`. By decision 25, a new provider of a key waits for the
  previous provider of that key to be disposed. The loader mounts no row that provides
  a key, whether the plan creates, enables or replaces it or a group
  the plan mounts mounts it, while a fiber that this plan or an earlier one removed,
  disabled or replaced, or that a group activation that ended gave up, still provides
  that key in the same realm; the plan's second batch mounts it, as for an id being
  replaced. The realm is the nearest row, the row itself first, whose
  `isolate` names the key, or the root. A row restarted in place is not held: it keeps
  its plugin and realm, and admission refuses two providers of one key in one realm.
  `loader.OpRestart` restarts the fiber even after an update of the same entry in the
  same plan that changed nothing, which is how a rollback restores a FAILED row whose
  inverse update restarts nothing (item 19).
- **Launcher and vet.** `--refs-dir` must name an absolute existing directory, or the
  command line is refused. Plugin vet refuses any import of `net/http/pprof` and `expvar`,
  and `time.AfterFunc` in `boot` needs a `//kernelcheck:allow-go` reason.

## Owner decisions of 2026-11-21, S1 report

The project owner decided the following on 2026-11-21, on the questions of the S1 exit
report (`docs/design/s1-report.md`, §4 and §7(a)); each names the item it answers. Where
a sentence above says otherwise, these decisions win.

24. **Decision 18 holds by key** (item 1). A row required by key, an active provider of
    a required key in the root realm, counts as a required row: a reference it holds
    pulls the `config-refs` rows that serve the process and `configRefsReady` into the
    required set, and a profile that then disables `config-refs` is refused at
    admission. `configRefsReady` stays in the set when every `config-refs` row runs in
    another role. Disabling `config-refs` stays an optional choice while no row required
    by row or by key holds a reference.

25. **A new provider of a key waits for the previous provider of that key** (item 2).
    The loader mounts no row that provides a key while a fiber that this plan or an
    earlier one removed, disabled or replaced, or that a group activation that ended
    gave up, still provides that key in the same realm; the plan's second batch mounts
    it. A row restarted in place is not held, because admission refuses two providers of
    one key in one realm.

26. **Decision 22 reads "may lose"** (item 3). `restart-policy` may lose its records
    across its own restart at S1 and may start them afresh, so a later step may keep
    them. The text of decision 22 above is the decided text; it replaces the first
    wording, "keeps no records" and "starts its records afresh".

27. **Admission refuses a provider of a required key whose own need has no provider**
    (item 4). The no-provider rule of the static graph treats an active provider of a
    required key in the root realm as it treats a required row: a need of that row with
    no provider in its realm, including one whose provider runs only in another role, is
    an error and refuses the profile at admission. Until now it was a warning, and boot
    then stopped at the required audit.

28. **The line budgets of `boot/` and `loader/` are measured again at the commit that
    closes S1** (item 5). That commit, after the merge of the kernel additions, applies
    the rule of decision 11 to the counts it measures, and ADR-059, `ci.yml` and the
    Makefile change together. Until then the caps of "Line budgets at the S1 exit" hold.

29. **The exit codes of the launcher are adopted as written** (item 7): 0 OK, 1 refused
    before any row mounted, 2 usage, 3 required set not met, 4 leak or shutdown overran,
    5 `restart-policy` gave up on an infrastructure provider, 6 launcher failure, 7
    restore failed. Code 2 is also the exit code of a Go panic. `aiopsctl` uses 0, 1 and
    2. They are recorded in the launcher documentation: the usage texts of `aiops` and
    `aiopsctl`, and the tables in `boot/run.go` and `cmd/aiopsctl/run.go`. From ADR-061
    (decision 13), `boot.RunOptions` gains `Command`, which `cmd/aiopsctl` sets and
    `cmd/aiops` does not, and under it the launcher gains code 8, `ExitInterrupted`: a
    stop that a signal or the end of `ctx` began returns 8; a stop that an `ExitCommand`
    request began returns the request's code, 0, 1 or 2, when the tree disposed within
    the shutdown bound with no leak, and 4 otherwise; and where boot would return 2 it
    returns 6, so a launcher code of 0 or 2 is always a command's. The codes of `aiops` do
    not change. From then the table of `aiopsctl` is recorded in the help of the `cli`
    row and in the exit mapping of `cmd/aiopsctl` (ADR-061 decision 10).

30. **The default limits are the release defaults for now** (item 8): 10 s each for
    load, drain and dispose; a shutdown bound of 30 s; a settle bound of their sum; a
    leak grace of the drain deadline; archive 256, event window 1,024, observer queue
    65,536, owner inbox 256, terminal limit 256. They are revisited with the
    restart-under-load measurements of S3.

31. **`<home>/run/status.json` is the readiness channel of one process at S1** (item 9).
    It is written atomically and carries phase, ready, profile, role, digest, exit and
    detail. From ADR-061 (decision 21) it also carries `rows`, each row's state and what
    it waits for, written at settle and at every committed generation; the field is
    additive. The probe that an orchestrator reads is decided when the `http` definition
    exists.

32. **`conformance.Run(t, Mount)` is the pattern of every definition suite from S2a**
    (item 10). `Mount` returns the implementation under test and the `*kernel.Root` it
    runs on, as the loader suite's returns `(loader.Loader, *kernel.Root)`.

33. **`RegistryKey` is consumer-bound** (item 11). Only the entries that
    `Config.Consumers` lists resolve it, since whoever holds the registry can dispose and
    restart any fiber.

34. **A veto of `loader/patch-context` fails closed** (item 12). A listener that does not
    call `next`, or returns a nil context, fails the entry with `ErrPatchVetoed`, and its
    plugin never runs.

35. **`profileContext` carries `Compose`, `Admit` and `Plan`** (item 13). The launch
    service of decision 7 is the key `profileContext`, and the launch keys stay six.

36. **The kernel additions G6, G7, G8, G10 and G11 are approved** (item 14): `Commit` and
    `CommitOp`, the per-fiber config change lock, `RunWith`, the ERROR records of the
    kernel, and the delivery of the events of `Close` with the field
    `ObserverOverflow.Stall`. They merge to the main branch with G1 to G5, G9 and G12
    after the S0 gates on the final port are recorded (decision 1), and are verified
    with them (decision 2).

37. **The definition points that S1 recorded in its own code are settled before S2a**
    (item 15): `Committed`, `Commit`, `Required` and `RequiredOf` on `profileContext`;
    `Catalog.Plugin`, which hands the plugin value to every consumer of the open class;
    `Cmdline.Env`; the second batch of an id removed and created again; the payloads of
    `loader/patch-context`, `loader/entry-init` and `loader/volatile-update`;
    `reload.Op.Source` and the `ctx` that `Submit` takes; the field names of
    `composition.Row`. They stay open points until then. From S2a, whoever changes a
    `v0` definition fixes every plugin under `plugins/` and `contrib/` in the same pull
    request (`08` §2). This decision settles nothing about the three behaviour points
    that item 15 also lists, on which the report recommends nothing: a loader wait that
    is cut short, `reload` abandoning its queued operations when its row unloads, and a
    waterfall listener that cannot pass a changed value to `next`. They stay open points.
    Note of 2026-11-23: decisions 40 to 49 settle all the points of this decision,
    40 to 46 the definition points and 47 to 49 the behaviour points.

38. **The section "configRefsReady (recorded 2026-11-21)" is accepted as recorded**
    (item 16), with the rule that decision 18 replaced.

39. **The default answers of round 1 are confirmed** (item 17). They are the change of
    the `reload` row itself, accepted above; `--refs-dir` checked at the command line;
    plugin vet refusing `net/http/pprof` and `expvar`; `time.AfterFunc` in `boot` with an
    allow-go reason; an empty entry id refused; `stable: 0` meaning no stable period;
    overlapping teardown of a loader's children; the hook limit of the loader's exclusion
    token; `WaitGroup.Go` in the settle of `reload`; the `reload/operation` event; one
    retry hold while any operation is in flight; three kernel rules (a child mount after
    a restart earlier in the batch is refused, a `CommitOp` after a `DisposeOp` of the
    same fiber is refused, `Close` bounds its observer wait by one drain deadline); the
    unexported registry config error; the observer queue of 65,536; and test limits of
    1 s. The round 1 questions with no recorded answer are decided in the record of the
    S1 exit: the stricter reading of check 8 for a patch on a group; the field that holds
    per-key intercept provenance; `restart-policy` treating a fiber that goes UNLOADING
    then LOADING as restarted by someone else; the clock edge case; whether `config-refs`
    refuses a file root named by another launcher flag; the untracked caller in
    recomposition case (a); a seam for a kernel-side oracle in the suites; requiring
    `Handler` in an `http.Server` literal. Note of 2026-11-23: decisions 50 to 53 settle
    these questions.

Decision 27 is built in the no-provider rule of `boot`'s static graph (`BuildGraph`),
which `Admit` runs at boot and for a live generation: a need with no provider in the
realm of the row is an error when the key is required or the row is required, by row or
by key. A row is required by key when it is active and provides a key of the process's
required set in the root realm; `RequiredFor` reads the same predicate for decision 24.

## Owner decisions of 2026-11-23, S1 open points

The project owner decided the following on 2026-11-23, on the definition and behaviour
points that decision 37 left open and the round 1 questions that decision 39 left to the
record of the S1 exit (`docs/design/s1-report.md`, §7(a), items 15 and 17). Each names
the item it answers and what changed. Where a sentence above says otherwise, these
decisions win.

40. **`profileContext` keeps `Committed`, `Commit`, `Required` and `RequiredOf`, and is
    consumer-bound** (item 15). The four methods are the `v0` shape: `Committed` is the
    composition the process runs, `Commit` records one that `Compose` returned,
    `Required` is the required set of the committed composition, and `RequiredOf`
    applies the same rule to a candidate, which `reload` judges before it commits it.
    `Commit` changes what the tree runs and the set that readiness judges, so
    `profileContext` is class `PC`, as `appExit` is. The release manifest lists
    `aiops/loader`, `aiops/reload` and `aiops/readiness` as its consumers, and the test
    manifest adds `aiops-test/test-state`. The launch definition, its `definition.yaml`
    and the class cells of `03`, `07` and the design record say so, and the
    consumer-bound keys go from 11 to 12.

41. **The catalog holds no plugin value** (item 15). `Catalog.Plugin` is removed from
    the launch definition and its fake: whoever holds a `kernel.Plugin` value runs code
    under that entry's bindings, so the open catalog key let any consumer act as any
    entry of the module table, `aiops/reload` included. `boot` gives the module table's
    values to the loader alone, in `loader.Config.Plugins`, as it gives the batch
    function, and `Registry.LoaderConfig` takes the table as a third argument. The
    loader still resolves key names through `Catalog.Key`. Decision 40 rests on this.

42. **`Cmdline.Env` serves only names with the prefix `AIOPS_REF_`** (item 15). The
    launch definition holds the prefix as `EnvPrefix`, and the launcher and the fake
    report any other name as not set, the rule that `08-profiles-roadmap-adrs.md:162`
    states for `${env:}` references. `config-refs` takes the prefix from the definition
    and keeps its own check for the text of its error.

43. **A mount that waits for a disposal goes in the plan's second batch** (item 15). A
    plan is one kernel batch. An entry the plan creates, enables or replaces whose id, or
    a key it provides in the same realm (decision 25), still has a previous fiber is
    mounted in a second batch once that fiber is DISPOSED. An id removed and created
    again is one case of this rule. Step 4 of the apply pipeline in `03`, `08` and the
    design record states it.

44. **The payloads of the loader hooks** (item 15). `PatchContextArgs` and
    `EntryInitArgs` stay as they are, with their timing. `VolatileUpdateArgs` keeps `ID`
    and `Config`, and `Paths` becomes `[][]string`: each changed leaf path as its list of
    keys, sorted, so a key that holds a dot is no longer read as a nested field. A path
    names a changed leaf, not a volatile field; a consumer maps a path to its field by
    prefix. Section 8 of the design record states the three payloads.

45. **`Submit(ctx, op)` and `Op.Source` stay** (item 15). A nested submission is one
    made under the context of the reload goroutine's own work, for example from a
    listener of `composition/applied` or of a loader hook; it is refused with
    `ErrNested`, and any other submission only queues. `Op.Source` is a label on the
    result and the `reload/operation` event, not an identity: it is not authenticated,
    and `composition/applied` does not carry it. The key tables of `00`, `03` and the
    design record write `Submit(ctx, op)`.

46. **The field names of `composition.Row` stay** (item 15). `ID`, `Fiber`, `Plugin`,
    `Parent` and `State` are the names that `launch.Row`, `launch.RowState` and
    `kernel.LedgerEvent` give the same values; `Hooks` and `Registrations` are the words
    of `03`. `State` reads PENDING for a row with no fiber.

47. **A loader wait cut short leaves the first batch committed** (item 15). After the
    first kernel batch has committed, the wait for a previous fiber ends early when the
    context ends or the fiber settles in a state other than DISPOSED (`ErrNotDisposed`,
    which includes a fiber blocked past its load deadline), and the second batch may
    fail too. In each case the first batch stays committed, an entry that waited stays in
    the tree with no fiber and reads PENDING, and `Apply` returns the results with the
    error. The loader never mounts over a fiber that is not DISPOSED, and the first
    batch cannot be undone, because the fibers it disposed cannot come back. `reload`
    judges those results: a waiting entry that the plan created, enabled or replaced, or
    that was ACTIVE before or is required, fails the verdict and the rollback of
    decision 12 follows; when the inverse plan meets the same fiber still not DISPOSED,
    the rollback fails and decision 23 applies. An entry held only by the key wait, such
    as a child of a group the plan enables, that was not ACTIVE before and is not
    required, is a warning only: the verdict can pass with `ErrNotDisposed` on the
    result, and that entry stays with no fiber. When the context has ended, no verdict
    is given: the operation is abandoned and the state of the tree is unknown
    (decision 23). The loader definition names the three causes.

48. **`reload` abandons its queued operations when its row unloads** (item 15), except
    at the restart that applies the row's own change, whose next activation runs the
    queue (the change of the `reload` row itself, accepted on 2026-11-21). Applying a
    generation while the process disposes the plugin that applies it would leave the
    tree half way with no one to judge it. An abandoned queued operation never touched
    the tree, and a submitter that still wants the change submits again: an `Op` carries
    no content. The `reload` definition says so.

49. **A waterfall listener transforms what `next` returns** (item 15). The kernel stays
    as it is: `next` takes no argument, and the listeners inward of a listener see the
    argument as dispatched. `config-refs` now calls `next` first and resolves its result,
    so every listener inward of it on `internal/config` runs and sees references
    unresolved; `Resolve` copies what it changes, so the kernel's retained raw config is
    never written. Before, it returned the resolved config without calling `next`, and
    those listeners did not run. A hook whose inward listeners must see a changed input
    declares a pointer payload made for that dispatch. The waterfall row of `07` §3.4
    states the rule.

50. **Check 8 reads a patch on a group as a patch of the rows it holds** (item 17). A
    patch of `inject`, `isolate` or `group` is refused on a required row, a provider of
    a bound key, or a group that holds one. The per-layer table at
    `08-profiles-roadmap-adrs.md:147` and its copies in `00`, `07` and the design record
    say so, and a gate case covers a required row beneath a patched group.

51. **`${file:}` reads only the directory that `--refs-dir` names** (item 17).
    `Config.FileRootFlag` is removed from `config-refs`: it let a row config point file
    references at any launcher flag with an absolute value, the home and the bundles
    included, so a reference could read profiles, patches and `run/status.json`. A
    `config-refs` config that sets `fileRootFlag` is refused as an unknown field.
    Passing `--refs-dir` alone enables `${file:}` references for every row of the
    process, rows that an installed artifact inserts included; without it no file
    reference resolves.

52. **The other round 1 questions are answered by the code as built** (item 17):
    - The intercept provenance of each key stays in the `Provenance` entries
      `intercept.<key>`, which `Provenance.Of` also finds.
    - `restart-policy` tracks a fiber that goes UNLOADING then LOADING afresh, as one
      that someone else restarted. A provider change that skipped PENDING would reset
      the counts of its dependants too; at S1 a dependant always waits PENDING.
    - The clock drops a timer whose activation quiesces before the kernel records its
      stop, as after an `Apply` that fails after `After`; the clock definition and its
      fake say so.
    - The untracked goroutine of the toy row stays the non-dependant caller of
      recomposition case (a). A case shaped like a dispatcher is added when S5 builds
      the first real dispatcher.
    - Plugin vet requires a non-nil `Handler` in an `http.Server` literal and refuses a
      server assembled in steps. The rule is syntactic: a struct field, an embedded
      field, a type alias and a nil variable escape it. It is revisited at S3 if the
      `http-server` provider must assemble its server in steps or another row needs a
      listener.

53. **The suites use the kernel seam only to observe** (item 17), beside decision 9. The
    kernel-side oracle reaches the root through the `Kernel` field of `boot.RunOptions`
    and `serve.Distribution`. That field is the one test hook outside the two test-only
    rows; the suites use it only to observe, through `Root.Inspect`, and it is nil in
    every shipped binary.
