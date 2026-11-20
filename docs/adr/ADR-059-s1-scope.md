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
