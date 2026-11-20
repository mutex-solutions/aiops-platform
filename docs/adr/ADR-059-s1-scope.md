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
- Open point: a row that gains its first reference or loses its last changes its
  `inject`, which the loader treats as a replacement.

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
