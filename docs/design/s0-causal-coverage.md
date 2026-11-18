# S0 causal comparison coverage

Status: evidence record for the owner-approved causal comparison
([ADR-035](../adr/ADR-035-cordis-tracking.md),
[amendment](s0-causal-comparison-amendment.md),
[amendment 2](s0-causal-comparison-amendment-2.md),
[its addendum](s0-causal-comparison-amendment-2-addendum.md) and
[freeze 2](s0-causal-comparison-freeze-2.json)). The corpus and criteria are unchanged; the candidates
changed only by the fixes of 2026-11-18 recorded below. This note does **not** establish S0 acceptance.

The first part is the result of the rerun of all 36 scenarios with the comparator
as extended by amendment 2. The sections after "Before amendment 2" are the
earlier record of the frozen comparator and are kept unchanged for history.

## After amendment 2 and the candidate fixes (clean run)

Process note, stated plainly. The six addendum rules were named **after**
development probes of a half-written comparator (their results were discarded and
no receipt comes from them). The pre-fix run in the next section was made after the
amendment, addendum and implementation were committed, but on candidates that
still had the defects below. The run in this section, receipts
[`causal-amendment-2-after-fixes`](../../test/spike/s0/results/causal-amendment-2-after-fixes/invocation.json),
is the first one made with amendment 2, its addendum and the implementation all
fixed beforehand and with the candidate fixes of 2026-11-18 in place. No comparator
code or rule changed between the rebase onto those fixes and this run. The
pre-fix receipts under `causal-amendment-2` are kept as recorded.

Protocol as before: every pair 100 times under `-race` and 100 times with
`GOMAXPROCS=2` under `-race`, one fresh pinned-oracle execution per scenario;
"match" only at 100 of 100 in both modes.

| ID | Fixture | Strict R / G (race + GOMAXPROCS=2, of 100 each) | Causal R | rate R | Causal G | rate G | Note |
|---|---|---|---|---|---|---|---|
| D01 | D01.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match; unsupported (comparator scope). |
| D02 | D02.json | 16+18 / 16+11 | match | 100+100 of 100+100 | match | 100+100 of 100+100 |  |
| D03 | D03.json | 3+6 / 70+52 | match | 100+100 of 100+100 | match | 100+100 of 100+100 |  |
| D04 | D04.json | 0+0 / 0+0 | match | 100+100 of 100+100 | match | 100+100 of 100+100 | match on both; the refusal text is compared by class (C6.5). Not carried by owner decision 2026-11-18. |
| D05 | D05.json | - / - | not run | - | not run | - | not-carried boundary, not run. |
| D06 | D06.json | 0+0 / 0+0 | MISMATCH | 0+0 of 100+100 | MISMATCH | 0+0 of 100+100 | (b) both, stable (100 of 100 in both modes, 2026-11-19): the child epoch no longer lists the inherited `svc` (fixed, see "D22 and D06 fixes (2026-11-19)"); the remaining difference is the documented realm rule, row 8: the private child's `retained-inherited` reads `undeclared key` where the oracle reads `outer`, one extra `error` record (`record count mismatch: 24 != 25`). |
| D07 | D07.json | 35+31 / 89+86 | match | 100+100 of 100+100 | match | 100+100 of 100+100 |  |
| D08 | D08.json | 43+39 / 95+81 | match | 100+100 of 100+100 | match | 100+100 of 100+100 |  |
| D09 | D09.json | 75+53 / 23+29 | MISMATCH | 98+93 of 100+100 | match | 100+100 of 100+100 | R: was MISMATCH in 9 of 200 runs (second `service` event after the consumer's `config`); fixed 2026-11-18, now match in 100 of 100 under `-race` and 100 of 100 at `GOMAXPROCS=2`, see [`causal-d09-reconciler-fix`](../../test/spike/s0/results/causal-d09-reconciler-fix/invocation.json). G: match. |
| D10 | D10.json | 100+100 / 100+100 | match | 100+100 of 100+100 | match | 100+100 of 100+100 |  |
| D11 | D11.json | 100+100 / 100+100 | match | 100+100 of 100+100 | match | 100+100 of 100+100 |  |
| D12 | D12.json | 100+100 / 100+100 | match | 100+100 of 100+100 | match | 100+100 of 100+100 |  |
| D13 | revision-5/D13.json | 0+0 / 0+0 | match | 100+100 of 100+100 | match | 100+100 of 100+100 | corpus revision 5 (`awaitFiber` on `a`, `d`, `u`); also 100 of 100 in both modes while the host was loaded (see "Revisions 5 and 6 (2026-11-18)"). The revision-3 verdicts (MISMATCH, fixture race) are in the sections below. |
| D14 | D14.json | 47+44 / 98+86 | match | 100+100 of 100+100 | match | 100+100 of 100+100 |  |
| D15 | D15.json | 49+60 / 100+87 | match | 100+100 of 100+100 | match | 100+100 of 100+100 |  |
| D16 | D16.json | 0+0 / 0+0 | match | 100+100 of 100+100 | match | 100+100 of 100+100 |  |
| D17 | D17.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match; unsupported (comparator scope). |
| D18 | D18.json | 47+49 / 99+95 | match | 100+100 of 100+100 | match | 100+100 of 100+100 |  |
| D19 | D19.json | 100+100 / 100+100 | match | 100+100 of 100+100 | match | 100+100 of 100+100 |  |
| D20 | D20.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match; unsupported (comparator scope). |
| D21 | D21.json | 45+41 / 97+90 | match | 100+100 of 100+100 | match | 100+100 of 100+100 |  |
| D22 | revision-4/D22.json | 0+0 / 0+0 | MISMATCH, flaky | 35+38 of 100+100 | MISMATCH, flaky | 99+100 of 100+100 | both candidates now follow the oracle's teardown order (fixed 2026-11-19, see "D22 and D06 fixes (2026-11-19)"); what remains is an unsynchronised `joining-setup` cut: 14 events (the oracle's) in some runs, 15 or 18 in others. |
| D23 | D23.json | 0+0 / 0+0 | MISMATCH | 0+0 of 100+100 | MISMATCH | 0+0 of 100+100 | both MISMATCH, stable: caught `error {op:"effect"}` outside the Apply frame; documented adaptation, see "D06 and D23". |
| D24 | D24.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match; unsupported (comparator scope). |
| D25 | D25.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match; unsupported (comparator scope). |
| D26 | D26.json | - / - | not run | - | not run | - | not-carried boundary, not run. |
| D27 | D27.json | 100+100 / 100+100 | match | 100+100 of 100+100 | match | 100+100 of 100+100 |  |
| D28 | D28.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match; unsupported (comparator scope). |
| D29 | D29.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match; unsupported (comparator scope). |
| D30 | D30.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match; unsupported (comparator scope). |
| D31 | D31.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match; unsupported (comparator scope). |
| D32 | D32.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match; unsupported (comparator scope). |
| D33 | D33.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match; unsupported (comparator scope). |
| D34 | D34.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match; unsupported (comparator scope). |
| D35 | D35.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match; unsupported (comparator scope). |
| D36 | D36.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match; unsupported (comparator scope). |

Counts (68 pairs run): causal match 31, MISMATCH 9, unsupported 28. Pre-fix:
22, 18, 28.

What changed with the candidate fixes (reconciler pairs attributed to the
publication-order defect, and D09):

| Pair | Before | After |
|---|---|---|
| D03, D07, D08, D11, D14, D15, D18, D21 reconciler | MISMATCH, publication before consumer LOADING (D11, D14, D15 200 of 200; the rest 112 to 137 of 200) | match, 100 of 100 in both modes |
| D06 reconciler | MISMATCH, publication order in 126 of 200 plus Needs count | MISMATCH, only `Needs count mismatch`, 200 of 200 (same as graph-lock) |
| D09 graph-lock | MISMATCH, consumer `cb` loads while unavailable, 200 of 200 | match, 100 of 100 in both modes |
| D09 reconciler | MISMATCH, missing second `service` event, 200 of 200 | **was still MISMATCH, flaky (fixed 2026-11-18, see the entry below)**: matches in 191 of 200 runs; 9 of 200 fail with `event segment 2: conflicting order reversed between {"kind":"service","data":{"key":"svc","realm":"root:svc","value":{"ready":"availability"}}} and {"kind":"config","data":{"config":{},"fiber":"cb"}}`. The oracle records `service` after `cb` LOADING and before `cb` `config`; in those runs the reconciler's `Notify` event follows the consumer's `config`. Not a known defect, not excused. |

### D06 and D23 (classification with evidence)

**D06, both candidates: `checkpoint "children" Needs count mismatch`.** The
candidate epoch of fiber `child` lists a binding for `svc`; the oracle epoch of
`child` has none (its plugin declares no needs). Two separate facts:

- The original D06 assertion that fails on both candidates (`retained-inherited`
  reads an `undeclared key` error instead of `outer`) is (b), documented: decision
  record `docs/design/plugin-system-2026-11.md` section 15 row 8, "`inject` as a
  list or a map; `ctx.inject(deps, callback)` as a child fiber. Access without
  inject is an error; children inherit within a realm", status "As is". The
  private child lives in another realm, so by that row access there is an error.
- The extra `svc` binding in the child's epoch is **not** named by any row.
  Row 8 says children inherit within a realm; it does not say an inherited
  declaration enters the child's epoch. No row of
  `docs/architecture/00-decisions.md` mentions it either. The needs are read in
  the replay front end from `context.Metadata().Dependencies()` of the native child
  context (`test/spike/s0/replay/composed.go`), so the cause sits in the shared
  composition/context model that both candidates use, not in either scheduling
  core. Classification: (a), an undocumented difference in that shared model;
  uncertain whether the owner intends it as the realisation of "children inherit".
  Update 2026-11-19: the owner confirmed the oracle behaviour; the extra binding is
  fixed (see "D22 and D06 fixes (2026-11-19)"), only the (b) realm difference remains.

**D23, both candidates: caught effect `error` after ACTIVE, `mark once` before
`output setup-error`.** Documented adaptation, not a candidate difference.
`docs/design/s0-d23-request-mapping.md`, section "Evidence classification and
limits": "The approved adaptation is **callback request / external completion
observation**. Its caught error/output can appear later than the synchronous source
generator's error, including after the native ACTIVE transition or single-shot
cleanup. That is an honest raw ordered-semantic mismatch." The cause is the shared
replay adapter: `observer ... AwaitSetup` after a successful external `Settle`
(`flushEffectCompletions`) and `catchEffect` in
`test/spike/s0/replay/effects_adapter.go` emit the `error` and the output only
after setup and rollback completed. Candidates never see the event. The comparator
reports it as a mismatch, as that note says it must; `s0-effect-shape-adaptation.md`
only says D23 stays unsupported and does not itself declare the ordering.

### Fixture races (D13 revision 3, D22 revision 4)

Fixtures were not changed. Original expectations are not affected: D13 revision 3
asserts only the six states at `six` (`d` DISPOSED, which holds either way), and
D22 revision 4 asserts only states at `joining-cleanup` and `disposed`; none looks
at `joining-setup` or at how `d` got to DISPOSED. Only the causal record counts
differ.

- **D13 revision 3, fiber `d`.** `d` is mounted and disposed with no wait on its
  Apply or ACTIVE, so the oracle (run to completion) shows `d` disposed while
  LOADING and the candidates sometimes show it ACTIVE first. Proposed barrier (not tried): a
  `mark` in the `disposed` plugin's Apply, a root `waitMark` on it and an
  `awaitFiber d` (to ACTIVE) before `dispose d`, so that `d` is ACTIVE in both
  runtimes before its disposal.
- **D22 revision 4, `joining-setup` snapshot.** It follows `dispose parent
  {await:false}` with no wait, so the cut position and the state of the child
  (ACTIVE or already UNLOADING) are schedule dependent. Proposed barriers (not tried): a `mark`
  in the parent cleanup plus a root `waitMark parent-cleanup` before the snapshot,
  and a `mark` in a new child cleanup body with a `waitMark` on it, so both the
  parent cleanup and the child's teardown start have happened at the cut. (The
  child's effect cleanup gate cannot be waited for before `release effect-setup`.)

## D22 and D06 fixes (2026-11-19)

Receipts: [`causal-d22-d06-fixes`](../../test/spike/s0/results/causal-d22-d06-fixes/invocation.json).
Owner decision of 2026-11-18: where a candidate differs from the oracle it is fixed
to match. Both differences below were such cases.

**D22, teardown order (both candidates).** Oracle (`vendor/cordis/src/fiber.ts`:
`this.dispose = parent.fiber.effect(...)`, `_unload`): a child's disposer is one
effect of its parent, so a parent unload runs the parent's disposers in reverse
registration order and the child is disposed when the walk reaches its slot; the
parent is DISPOSED after its children. Recorded trace (events 13 to 22): `plugin
disposed parent`, parent UNLOADING, `cleanup parent`, `mark parent-cleanup`,
`plugin disposed child`, child UNLOADING, effect cleanup gate and mark, child
DISPOSED, parent DISPOSED. Before the fix both candidates requested disposal of
every child when the parent's activation was invalidated and joined it before the
parent's disposers (16 events at the cut, parent cleanup only after the child was
DISPOSED). Now each child is an ordered effect entry on its parent in both cores:
disposal is requested at its slot in the LIFO walk and joined after it. Candidate
mismatch text moved from `record count mismatch: 14 != 16` (400 of 400) to a flaky
cut, and 100-run rates under `-race` are reconciler 35/100 and graph-lock 99/100,
with `GOMAXPROCS=2` reconciler 38/100 and graph-lock 100/100 (receipts). The
event sequences and final states equal the oracle's; the rest is the
`joining-setup` snapshot, taken after a non-awaited disposal with no barrier that
exists in both runtimes. The candidates unload on their own goroutines, the
oracle's cut is synchronous, so the snapshot sees 14, 15 or 18 events. The pair is
therefore not moved into `causalCovered`: it stays a flaky diagnostic with its
verdict text updated.

**D06, child epoch (both candidates).** The oracle's epoch lists
`Object.keys(fiber.inject)` of the fiber itself; a child without its own `inject`
has no needs and reaches a parent's committed binding by walking up the fiber chain
(`reflect.ts`, the `internal/get` handler: `fiber.committed`, `prop in fiber.inject`,
then `fiber.parent.fiber` while the realm label is unchanged). The Go model took
the needs from `Metadata().Dependencies()`, the effective ancestor-first list, which
put the inherited `svc` into the child's native needs, epoch and availability gate.
`contextmodel.Context.OwnDependencies` now returns the frame's own declarations (same
realm filter) and `composition` uses it for native needs and for the replay's epoch;
the read walks to the nearest activation that declares the key (`declarer`), whose
binding is retained until its children are DISPOSED. Cross-realm reads still fail
with `undeclared key`. The `Needs count mismatch` is gone; D06 is still MISMATCH,
stable, because of the realm rule (b) above, one extra `error` record in the
`children` cut (24 against 25 events). It cannot be moved into `causalCovered`.

Accepted pairs (D02, D03, D04, D07, D08, D09, D10, D11, D12, D13, D14, D15, D16, D18,
D19, D21, D27) were re-measured: see the receipts for the counts.

Consequence for plugin authors: [ADR-034](../adr/ADR-034-kernel-concurrency.md),
section "2026-11-19 teardown order of a parent and its children".

## Revisions 5 and 6 (2026-11-18)

Receipts: [`causal-fixture-revisions-5-6`](../../test/spike/s0/results/causal-fixture-revisions-5-6/invocation.json).
Comparator, oracle harness and candidates are unchanged; this is fixture data only.
Each rate is 100 runs per candidate against one fresh pinned-oracle (Node 24.22.1)
execution, under `-race`, under `-race` with `GOMAXPROCS=2`, and each of those again
while two other `go test -race -count=200` copies loaded the host.

**D13 (match).** [Corpus revision 5](s0-corpus-revision-5.md) adds three
`awaitFiber` steps to revision 3 (`a` after `waitMark active-apply`, `d` before
`dispose d`, `u` before `dispose u`). Result: `reconciler` and `graph-lock` both
100 of 100 in all four modes. It took three drafts, each kept with its failing
traces: draft 1 (`d` only) failed about 1 in 100 under load because fiber `u`, whose
only barrier is a mark inside its Apply, was disposed while LOADING; draft 2 (`d`,
`u`) failed under load because fiber `a`, asserted ACTIVE at `six`, was still
LOADING at the snapshot, which also failed the **original** expectation
(`D13 six/states/2/state: actual "LOADING" != expected "ACTIVE"`). The previously
proposed `mark` in the `disposed` Apply is not part of the final script: a mark
inside Apply does not order the ACTIVE transition and `awaitFiber d` orders it
alone. The pair moves from MISMATCH to match and is now in `causalCovered`.

**D22 (still MISMATCH, not a fixture race).** No revision 6 was made.
The 14 against 16 of the earlier rows was labelled the wrong way round: the
comparator's `record count mismatch: 14 != 16` is oracle 14, candidate 16, and it
is the same text in 400 of 400 runs per candidate (four modes, 100 each),
not a schedule dependent race.

- Oracle (run to completion): `dispose parent {await:false}` emits `plugin
  disposed parent` and `parent UNLOADING`, and the root's `joining-setup` snapshot
  is taken there (cut after event 14, child still ACTIVE). The parent's own cleanup
  and `mark parent-cleanup` come after the cut, and the child's teardown after that.
- Both candidates join the child before the parent's own disposers
  (`test/spike/s0/concurrency/types.go`, ownership comment at line 276; `graphlock.go`
  line 128 and 1963; `reconciler.go` line 1542). At the cut `plugin disposed child` and `child UNLOADING` are
  already recorded (16 events); the parent cleanup follows only after the child
  reaches DISPOSED, which waits for `release effect-setup`.
- Every barrier the proposal named waits for an event that on the candidates happens
  only after `release effect-setup`: `waitMark parent-cleanup` (drafts a, b) and
  `waitMark child-cleanup` on a new child cleanup body (draft c) each ended in
  `context deadline exceeded` on both candidates in the first run, while the oracle
  accepted all three scripts. `settle` is held open by the gated effect. Any wait
  that includes the child's teardown start in the oracle's cut also includes the
  parent cleanup events there, which the candidates cannot emit before that
  release, so no fixture barrier gives equal cuts. Releasing `effect-setup` before
  the snapshot would change what the snapshot observes and is not a barrier.
- Amendment 2 rule C2.3 leaves teardown order between a parent's cleanup and its
  children unconstrained as an order relation; the difference is only visible
  because the `joining-setup` checkpoint falls between those two orders.
- Classification: (a), a real difference between the Go candidates (children joined
  before the parent's ordinary cleanup) and the oracle (parent cleanup first), not
  a fixture causality gap. Fixed 2026-11-19, see "D22 and D06 fixes (2026-11-19)". Whether it is intended is an owner question; the
  original expectations (`joining-cleanup`, `disposed`) pass either way.

Counts relative to the table at the top: D13 moves two pairs from MISMATCH to
match, giving 33 match, 7 MISMATCH (D06 both, D09 reconciler, D22 both, D23 both)
and 28 unsupported, before any other change to the D09 rows. Drafts, harness
verdicts and the captured oracle and candidate traces are in the evidence
directory.

Current tally with the D09 reconciler fix of the same day included: 34 pairs match,
6 are MISMATCH (D06, D22 and D23 on both candidates) and 28 are unsupported.

## Before the candidate fixes (receipts `causal-amendment-2`)

The text of this section is as first recorded.

### After amendment 2, before the candidate fixes

Receipts: [`causal-amendment-2`](../../test/spike/s0/results/causal-amendment-2/invocation.json).
Each scenario was replayed 100 times per candidate under `-race` and 100 times
per candidate with `GOMAXPROCS=2` under `-race`, against one fresh pinned-oracle
(Node 24.22.1) execution per scenario. D13 uses `corpus/revision-3`, D22 uses
`corpus/revision-4`, D10, D12 and D16 run without the composed front end as in
the existing test. D05 and D26 are not-carried boundaries and are not run.

A pair is **match** only when it matched 100 of 100 in both modes, **unsupported**
when the comparator could not prove the construct in every run, and **MISMATCH**
otherwise. Rates are `race + GOMAXPROCS=2` causal matches over runs. Strict is the
existing byte comparison, schedule dependent. Categories: (a) real candidate
difference, (b) decision-record adaptation, (c) fixture or comparator limitation.

| ID | Fixture | Strict R+G (race + GOMAXPROCS=2 of 100 each) | Causal R | rate R | Causal G | rate G | Category and evidence |
|---|---|---|---|---|---|---|---|
| D01 | D01.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match on both. Unsupported: `$meta` runtime metadata expression (not added). |
| D02 | D02.json | 14+13 / 2+10 | match | 100+100 of 100+100 | match | 100+100 of 100+100 | covered earlier; strict fails on schedule (see note), causal match. |
| D03 | D03.json | 0+0 / 48+63 | MISMATCH | 45+41 of 100+100 | match | 100+100 of 100+100 | R: (a) known R defect class, publication before consumer LOADING. G: match. Shared output cell "joined" proved by frame (C5). |
| D04 | D04.json | 0+0 / 0+0 | match | 100+100 of 100+100 | match | 100+100 of 100+100 | both match. Refusal text compared by class (C6.5); strict fails only on that text. |
| D05 | D05.json | - / - | not run | - | not run | - | (b) `ctx.get` not carried, not run. |
| D06 | D06.json | 0+0 / 0+0 | MISMATCH | 0+0 of 100+100 | MISMATCH | 0+0 of 100+100 | (a) both: `checkpoint "children" Needs count mismatch`: the candidate child epoch lists the inherited `svc` need that the oracle does not (decision record row 8, inherited declarations are realm-local); R also shows the publication-order defect in 126 of 200 runs. |
| D07 | D07.json | 0+0 / 58+49 | MISMATCH | 39+47 of 100+100 | match | 100+100 of 100+100 | R: (a) publication order defect. G: match (NeedWith, service mode, C4). |
| D08 | D08.json | 0+0 / 63+49 | MISMATCH | 30+49 of 100+100 | match | 100+100 of 100+100 | R: (a) publication order defect. G: match (ancestor intercepts, C4). |
| D09 | D09.json | 0+0 / 0+0 | MISMATCH | 0+0 of 100+100 | MISMATCH | 0+0 of 100+100 | (a) known: G `consumer "cb" LOADING before every required provider ACTIVE and available` 200 of 200 (consumer churn while availability false); R `record count mismatch: 7 != 6` 200 of 200 (second `service` event missing). |
| D10 | D10.json | 100+100 / 100+100 | match | 100+100 of 100+100 | match | 100+100 of 100+100 | covered earlier. |
| D11 | D11.json | 0+0 / 100+100 | MISMATCH | 0+0 of 100+100 | match | 100+100 of 100+100 | R: (a) publication order defect 200 of 200. G: match (gate, mark, release, C1). |
| D12 | D12.json | 100+100 / 100+100 | match | 100+100 of 100+100 | match | 100+100 of 100+100 | covered earlier. |
| D13 | revision-3/D13.json | 0+0 / 0+0 | MISMATCH | 2+0 of 100+100 | MISMATCH | 0+1 of 100+100 | (c) fixture race on fiber `d` (no barrier on its Apply/ACTIVE): record counts 34..38 vs the oracle 37, order reversal between `d` apply and `d` disposal; a match in 3 of 400 runs by luck is not a match. |
| D14 | D14.json | 0+0 / 35+81 | MISMATCH | 0+0 of 100+100 | match | 100+100 of 100+100 | R: (a) known defect, 200 of 200. G: match. |
| D15 | D15.json | 0+0 / 54+80 | MISMATCH | 0+0 of 100+100 | match | 100+100 of 100+100 | R: (a) publication order defect 200 of 200. G: match (provider swap, C6). |
| D16 | D16.json | 0+0 / 0+0 | match | 100+100 of 100+100 | match | 100+100 of 100+100 | covered earlier; strict fails on schedule. |
| D17 | D17.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match. Unsupported: async `update` with `task` (not added). |
| D18 | D18.json | 0+0 / 35+65 | MISMATCH | 46+42 of 100+100 | match | 100+100 of 100+100 | R: (a) publication order defect in 112 of 200 runs, flaky. G: match (effects, C3). |
| D19 | D19.json | 100+100 / 100+100 | match | 100+100 of 100+100 | match | 100+100 of 100+100 | covered earlier. |
| D20 | D20.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match. Unsupported: async `update` with `task`. |
| D21 | D21.json | 0+0 / 82+59 | MISMATCH | 25+38 of 100+100 | match | 100+100 of 100+100 | R: (a) publication order defect in 137 of 200 runs, flaky. G: match (cleanup gate, retained provider, C1). |
| D22 | revision-4/D22.json | 0+0 / 0+0 | MISMATCH | 0+0 of 100+100 | MISMATCH | 0+0 of 100+100 | (c) fixture race: the `joining-setup` snapshot follows a non-awaited disposal with no wait, record counts 14 vs 16 on both candidates, 200 of 200. |
| D23 | D23.json | 0+0 / 0+0 | MISMATCH | 0+0 of 100+100 | MISMATCH | 0+0 of 100+100 | (a) new, both: the caught `error {op:"effect"}` event is recorded after the fiber is already ACTIVE (and `mark once` before `output setup-error`), outside the Apply frame; the oracle records it inside the body. Same on both candidates, so it is likely in the shared replay adapter, not in candidate scheduling. |
| D24 | D24.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match. Unsupported: `lateEffect` (not added). |
| D25 | D25.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match. Unsupported: `selfDispose` (not added). |
| D26 | D26.json | - / - | not run | - | not run | - | (b) awaited self-dispose, not run. |
| D27 | D27.json | 100+100 / 100+100 | match | 100+100 of 100+100 | match | 100+100 of 100+100 | covered earlier. |
| D28 | D28.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match. Unsupported: update veto policy. |
| D29 | D29.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match. Unsupported: `listen`. |
| D30 | D30.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match. Unsupported: update veto policy. |
| D31 | D31.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match. Unsupported: `listen`/`dispatch` (not added). |
| D32 | D32.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match. Unsupported: `listen`/`dispatch` (not added). |
| D33 | D33.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match. Unsupported: `listen`/`dispatch` (not added). |
| D34 | D34.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match. Unsupported: `listen`/`dispatch` (not added). |
| D35 | D35.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match. Unsupported: `listen`/`dispatch` (not added). |
| D36 | D36.json | 100+100 / 100+100 | unsupported | 0+0 of 100+100 | unsupported | 0+0 of 100+100 | strict match. Unsupported: `listen`/`dispatch` (not added). |

Counts, 36 scenarios, 68 candidate/scenario pairs run (D05 and D26 not run):

- causal match: 22 pairs (D02, D04, D10, D12, D16, D19, D27 on both candidates; D03, D07, D08, D11, D14, D15, D18, D21 on graph-lock only). Before this round 9 pairs were accepted by the comparator (D02, D10, D12, D16 on both, D14 on graph-lock; D19 and D27 were strict matches and not run through it);
- MISMATCH: 18 pairs (reconciler D03, D06, D07, D08, D09, D11, D13, D14, D15, D18, D21, D22, D23; graph-lock D06, D09, D13, D22, D23);
- unsupported: 28 pairs (D01, D17, D20, D24, D25, D28 to D36 on both candidates). All 14 scenarios are strict matches, so this is a comparator scope statement, not a finding.

Reading the mismatches:

1. **Reconciler publication order (known defect, D14 class).** Every reconciler
   MISMATCH of the form `last enabling provider ... published before consumer ... LOADING`
   (D03, D07, D08, D11, D14, D15, D18, D21, and D06 in part) is the defect already
   named for D14: the provider's `service` publication is recorded before a mounted
   consumer reaches LOADING. It is reported, not excused. D03, D07, D08, D18, D21 are
   flaky (the candidate sometimes orders it correctly), D11, D14 and D15 failed in every run.
2. **D09 (known).** graph-lock consumer `cb` loads while its availability is false;
   reconciler lacks the second `service` event after `notify`.
3. **D06 (new, with row 8).** Both candidates show a child epoch with the inherited
   `svc` need the oracle does not have. The inventory already records one failing original
   assertion of D06 on both candidates (row 8); whether this epoch difference is the same
   adaptation is not established here.
4. **D23 (new).** Both candidates emit the caught effect `error` after the fiber is
   ACTIVE. Identical on both, so probably the shared replay adapter.
5. **D13 and D22 (fixture races).** See the section "Fixture causality gaps"; the
   comparator reports the race, it does not repair it.

What amendment 2 does not establish: it does not show equivalence for any unsupported
scenario, it does not recompute values (NeedWith/intercept merges and effect error
payloads are compared between traces), it asserts at most one `service` event per
`notify` command and not that each `notify` produces one, and it does not assert a
cleanup order between different effects of one fiber (it compares that order with the
oracle). None of this is S0 acceptance.

## Before amendment 2 (frozen comparator)

### What was done

Every scenario that fails the strict comparison on at least one candidate in the
[inventory after D29](../../test/spike/s0/results/corpus-inventory-after-d29/results/inventory.json)
(D05 and D26 excluded, they are explicit not-carried boundaries) was run through
the frozen `CompareCausal` on both candidates against a freshly executed pinned
oracle (Node 24.22.1). D13 used `corpus/revision-3/D13.json`.

- `TestCausalReplayIntegration` now also covers D14 on both candidates (on `graph-lock` from the start, on `reconciler` after the fix of 2026-11-18 below).
- `TestCausalDiagnostics` (new) records the candidate/scenario pairs that the
  comparator does not accept, and fails if a verdict differs from the text in this
  note, so a later comparator change forces this note to be revisited. It never
  turns a non-match into a pass.
- Receipts: [`causal-coverage-extension`](../../test/spike/s0/results/causal-coverage-extension/invocation.json).

Repetition (before the fixes of 2026-11-18): D14 on `graph-lock` matched 100 of 100
runs under `-race` and 100 of 100 with `GOMAXPROCS=2` under `-race`. The D14
`reconciler` verdict was a mismatch in 100 of 100 runs. All 27 non-match verdicts
were identical in 5 further runs. The receipts under `causal-coverage-extension`
record that state and are not rewritten.

### Per-scenario table

Strict status is the inventory class (R reconciler, G graph-lock). Strict status
is schedule dependent: the inventory records that D02 flips between match and
failed from run to run, and D12 on R was strict-match in the first run made here
although the inventory lists it as failed. The
causal columns are the frozen comparator's verdict. Categories: (a) real
candidate difference, (b) listed in the decision record as adapted or not carried
(`docs/design/plugin-system-2026-11.md` section 15 checklist rows), (c)
comparator or interpreter limitation.

| ID | Strict R / G | Causal R / G | Category and evidence |
|---|---|---|---|
| D01 | match / match | not run | strict match |
| D02 | failed / failed | match / match | covered earlier |
| D03 | failed / match | unsupported / unsupported | (c) `output "joined" has ambiguous owner`: two consumers write one output cell in different realms, owner not provable |
| D04 | failed / failed | unsupported / unsupported | (c) `non-unique provider for svc/root:svc`: a refused second provider of one address is not modelled. Also observed, not in the decision record: the refusal error text differs (oracle `service "svc" has been registered at <provider>`, R `duplicate provision ...`, G `provision ... is already owned`) |
| D05 | incomplete / incomplete | not run | (b) row 9, `ctx.get` not carried |
| D06 | failed / failed | unsupported / unsupported | (c) `body operation "child"` is not modelled. Also (b) row 8: inherited declarations are realm-local, so `retained-inherited` reads an undeclared key (one original assertion fails on both, already named in the inventory) |
| D07 | failed / match | unsupported / unsupported | (c) `non-ordinary provision syntax`: provide mode `service`/NeedWith config view |
| D08 | failed / match | unsupported / unsupported | (c) `intercept context "outer"`: ancestor intercept chain not modelled |
| D09 | failed / failed | unsupported / unsupported | (c) `non-ordinary provision syntax`: provide mode `available`. The (a) differences observed here are fixed, see 2026-11-18 below; the comparator still cannot judge this scenario |
| D10 | match / match | match / match | covered earlier |
| D11 | failed / match | unsupported / unsupported | (c) `body operation "gate"`: gates are not modelled |
| D12 | failed / match | match / match | covered earlier |
| D13 (rev 3) | failed / failed | unsupported / unsupported | (c) `body operation "gate"`. Also fixture race on fiber `d`, see below |
| D14 | failed / match | match / match (R was mismatch before 2026-11-18) | R: was (a) `last enabling provider "p" published before consumer "c" LOADING`, 100 of 100 runs; fixed 2026-11-18, see below. G: match, 100 of 100 plus 100 of 100 at `GOMAXPROCS=2` before the change |
| D15 | failed / match | unsupported / unsupported | (c) `non-unique provider for svc/root:svc`: provider swap is not modelled |
| D16 | failed / failed | match / match | covered earlier |
| D17 | match / match | not run | strict match |
| D18 | failed / match | unsupported / unsupported | (c) `body operation "effect"`: effects are not modelled |
| D19, D20 | match / match | not run | strict match |
| D21 | failed / match | unsupported / unsupported | (c) `body operation "mark"`: marks are not modelled |
| D22 | failed / failed | unsupported / unsupported | (c) `body operation "child"`. Also fixture race, see below |
| D23 | failed / failed | unsupported / unsupported | (c) `async/control/catch metadata on "effect"`; effect shapes are adapted in row 14 ([mapping](s0-d23-request-mapping.md)) |
| D24, D25 | match / match | not run | strict match |
| D26 | incomplete / incomplete | not run | (b) awaited self-dispose is an explicit not-carried boundary |
| D27 to D36 | match / match | not run | strict match |

"Unsupported" is the comparator's own `ErrCausalUnsupported`: it reports the
first operation it cannot prove independent, so it says nothing about whether the
candidate is equivalent. The reason is the first one hit; a scenario may contain
further unsupported operations.

### What the comparator is missing (category c)

Each missing capability is general; none was added, because the methodology is
frozen. Required to cover the scenarios above: gate/mark/release continuations
(D11, D13, D21, D22, D23); child fibers (D06, D22); effects with bodies and
async/catch metadata (D18, D23); ancestor intercepts and NeedWith/service/available
provision modes (D07, D08, D09); proof of the owner of an output cell shared across
realms (D03); more than one provider of one address, by refusal or swap (D04, D15).

### Candidate differences (category a)

Owner decision, 2026-11-18: where a candidate differs from the oracle in D14 and
D09 it is a candidate bug and is fixed to match the oracle. Items 1 to 3 below are
therefore fixed; the observations are kept as they were first recorded.

1. **D14, `reconciler`.** In the candidate's own native ledger the provider goes
   ACTIVE, then its `service` publication is recorded, then dependent consumer `c`
   goes LOADING (native sequence 7, 8, 9). The oracle and `graph-lock` move `c` to
   LOADING before the publication. The code comment at the notification site says
   dependants refresh synchronously before the event, but the refresh ran
   after it.
2. **D09, `graph-lock`** (20 of 20 runs, informal multiset comparison, evidence not
   retained): consumer `cb` goes PENDING, LOADING, UNLOADING, PENDING while the
   availability check for its context is still false. The oracle keeps it PENDING.
3. **D09, both candidates** (20 of 20): the second `service` event the oracle
   records after `notify` does not occur. Row 21 says `reflect.notify` is adapted
   to a key and realm index but does not say the event is dropped.
4. **D04, both candidates:** refusal error text, see the table. Not in the
   decision record. Recorded as not carried by owner decision 2026-11-18: no
   behaviour change.

### Fixes of 2026-11-18

All three were reproduced first with real traces: the replay of
`corpus/D09.json` and `corpus/D14.json` on each candidate, compared with the
recorded oracle traces in `results/corpus-final`.

- **D14, `reconciler` (item 1).** Cause: a consumer's `Check` runs off the owner
  goroutine, so the refresh that follows the provider going ACTIVE only started the
  check and the consumer reached LOADING after the owner had recorded the
  publication. Fix: the owner records the `service` event after the dependants
  refreshed; when the refresh started a check it keeps the event, and defers the
  provider's own cleanup, until that check result has been applied, without
  waiting on the owner and without running any callback on it. Regression tests:
  `TestDependantLoadingPrecedesServiceEvent` (both candidates) and
  `TestD09D14MatchRecordedOracleOrder`. `TestCausalReplayIntegration` now covers
  D14 on `reconciler`.
- **D09, `graph-lock` (item 2).** Reproduced, 20 of 20 local replays. Cause:
  `graph-lock` moved a consumer to LOADING first and evaluated its availability
  inside the load; a false result then retired the fresh activation, giving
  PENDING, LOADING, UNLOADING, PENDING. Fix: the check now runs off the lock
  before the consumer leaves PENDING (with the load deadline as its bound), a false
  result keeps it PENDING and a true result starts the load without a second
  evaluation, and the provider's `service:publish` waits for the checks its
  refresh started so D14 keeps its order. The `reconciler` does not have this
  defect: it evaluates the check before LOADING and its D09 consumer stayed PENDING.
  Regression test: `TestDeniedCheckKeepsConsumerPendingAndNotifyRecordsService`.
- **D09, both candidates (item 3).** Reproduced: both candidates emitted one
  `service` event where the oracle records two. Decision: carried. Row 21 adapts
  how `reflect.notify` finds its fibers (an index by key and realm), not what it
  emits; the oracle's `internal/service` event after the refresh is observable
  behaviour and no row drops it. `Notify` now records one `service` event per key
  that has a provision, after the dependants refreshed, carrying the provision's
  value in any state, as `_getImpl(name, false)` does. A key with no provision
  would give an oracle event with an undefined value; no scenario exercises it
  and the candidates record nothing for it. Regression test: the same as item 2.
- **D04.** Recorded as not carried by owner decision 2026-11-18. No change.

D09 stays `unsupported` for the frozen comparator, so the fix is shown by the
replay-level comparison with the recorded oracle trace (per-fiber statuses and
service events), not by `CompareCausal`.

### Fix of 2026-11-18: D09, `reconciler` (notify order)

Reproduced with real traces (5 of 100 runs failed under `-race`: 95 of 100
matched before the change). In a failing run the candidate recorded `cb` PENDING,
LOADING, config, apply, mark, ACTIVE and only then the `Notify` `service` event;
the oracle records LOADING, `service`, config, apply. Cause: `Notify` rechecks every
dependant, so the held `service` event waits for the checks of `ca` and `cb`. When
`cb`'s check finished first, its load worker was started at once and recorded
before `ca`'s check result released the event. Fix, in both candidates (the
`graph-lock` has the same defect for a dependant that starts loading while the
event is held by another dependant's check, shown by the new regression test; the
corpus D09 did not hit it): a load worker is not started while a service event for
one of its fiber's needs is held or being produced; it starts when that event has
been recorded. LOADING is still recorded when the transition happens. No callback
runs on the owner or under the lock, and nothing blocks. Regression test:
`TestServiceEventPrecedesDependantLoadRecords` (both candidates; before the fix it
failed on 200 of 200 `reconciler` runs and about 130 of 200 `graph-lock` runs under
`-race`). Rates after the fix, all 16 accepted scenarios, both candidates, 100
runs each, `-race` and `GOMAXPROCS=2`: causal match 100 of 100 in every pair
(receipts
[`causal-d09-reconciler-fix`](../../test/spike/s0/results/causal-d09-reconciler-fix/invocation.json)).
`TestCausalReplayIntegration` now lists D09 on both candidates as covered.

### Fixture causality gaps (category c, not candidate bugs)

- **D13 revision 3** fixes the barrier for fiber `f` only. Fiber `d` is mounted and
  disposed with no wait on its Apply or ACTIVE. In 20 further fresh runs the
  candidate outcome varied (no Apply for `d`, or `d` reached ACTIVE first), and the
  event multiset differed from the oracle in 18 of 20 `graph-lock` and 19 of 20
  `reconciler` runs.
- **D22** disposes the parent after the child's effect gate but with no wait for
  the child's ACTIVE. The child was disposed while LOADING in 6 of 20 `graph-lock`
  and 7 of 20 `reconciler` runs, and ACTIVE first in the rest. This follows from
  row 22 (run to completion not carried).

### What this does and does not establish

- It established, before the fixes of 2026-11-18, that of the 14 targeted
  scenarios (28 candidate pairs) the frozen comparator accepted one pair repeatably
  (D14 on `graph-lock`), rejected one for a reproducible ordering difference (D14
  on `reconciler`) and could not judge the other 26 pairs. After the fix it accepts
  both D14 pairs and still cannot judge the other 26.
- It does not establish equivalence for any unsupported scenario, nor that a
  strict failure is harmless. The informal multiset observations above are not the
  comparator and are not receipts.
- It is not S0 acceptance: no criterion is relaxed, no scenario is dropped, and
  D05 and D26 remain open.
