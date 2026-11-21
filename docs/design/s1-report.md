# S1 exit report

- Date: 2026-11-21.
- For: the project owner, who decides whether S1 is closed.
- Commit: `ab9b405` on branch `s1/integration`, 228 commits ahead of `main` and 4 behind it (`git rev-list --count main..ab9b405` and `ab9b405..main`; merge base `8f8cfe0`).
- Sources: [`08-profiles-roadmap-adrs.md`](../architecture/08-profiles-roadmap-adrs.md) §1.6, §2 and §2.1; [`ADR-059`](../adr/ADR-059-s1-scope.md); [`ADR-034`](../adr/ADR-034-kernel-concurrency.md), "S1 additions to the kernel API"; [`s0-report.md`](s0-report.md); the code; the result files of the four review rounds (`s1-review-result.json`, `s1-fix-result.json`, `s1-round2-result.json`, `s1-round3-result.json`) and the input file of round 2 (`s1-round2-items.json`), which are kept outside the repository.
- Run for this report on `ab9b405`: the three S1 suites once plain (`go test -count=1 -v`) and once race-built (`GOFLAGS='-mod=readonly -race'` with `GORACE=log_path=...`, as `make s1-suites` does); `go test -count=1` on `./boot/...`, `./loader/...`, `./cmd/...`, `./dist/...`, `./plugins/...`, `./internal/...`, `./test/dist/...` and `./test/internal/...`, and `go test -count=1 ./...` in the modules `sdk/def` and `sdk/kernel`; the live oracle comparison (`S0_ORACLE_RUN=1 S0_CAUSAL_RUN=1 GOMAXPROCS=8 go test -race -count=1 -run TestCausal ./test/spike/s0/replay/`); `go run ./tools/kernelcheck lines <dir> 999999`; `wc -l` where a package has subpackages; `go list` for imports and dependencies; `git log`, `git rev-list` and `git diff --stat`.
- Status vocabulary: **met**, **met with recorded exception**, **pending** (a run, an edit or a decision is still needed), **not met**.

## 1. Verdict

| # | S1 exit criterion | Status | Evidence | Commit |
|---|---|---|---|---|
| 1 | 08 §2.1, S1 builds: "`loader`, `composition`, `boot`, `sdk/def/launch`, layers 0 to 4 and 6 and their rules (layer 5 is S4b), dumps, references, the module table, `restart-policy`, `reload`, `readiness`, the matrix harness, toy plugins including a heavy spine provider" | **met**. Each part exists and its tests pass. Layer 5 is not built (S4b). | `loader/`; `boot/` (`compose.go` lists layers 0 to 4 and 6); `sdk/def/{launch,composition}/v0`; `plugins/`; `test/dist/plugins/toy` | `ab9b405` |
| 2 | 08 §2.1, S1 exit: "The failure matrix (process-scope columns) ... pass on the built binary" | **met with recorded exception**. 41 of 41 cells marked S1 pass, 0 fail, plain and race-built. The other 43 process-scope cells are 24 "not yet" (later steps, ADR-059 decision 3) and 19 n/a. | `test/failure-matrix/`; §5.1 | `ab9b405` |
| 3 | 08 §2.1, S1 exit: "... and the recomposition suite pass on the built binary" | **met with recorded exception**. Cases (a) to (d) pass. Case (e), promotion, reports "not yet (S4b)" (decision 5). | `test/recomposition/recomposition_test.go`; §5.2 | `ab9b405` |
| 4 | 08 §2.1, S1 exit: "golden dumps" | **met with recorded exception**. Dump, hooks and schema goldens pass for boot's test data and for `aiopsctl profile render` on the test profile `smoke`. The dumps print stage 0 for every hook listener until S2a generates the stage table, so the goldens pin that placeholder. The profile `smoke` has no rows of the roles edge and worker. The test states both. | `boot/testdata/*.golden`; `cmd/aiopsctl/testdata/test-dist/`; `cmd/aiopsctl/golden_test.go` lines 31 to 34 | `ab9b405` |
| 5 | 08 §2, S0 to S1: "The loader and the launcher are built on the kernel and tested with `kerneltest`" | **met with recorded exception**, on the branch kernel. The loader, the core plugins and the definition fakes are tested with `kerneltest`, except the `configrefs` fake, which builds its kernel with `kernel.New` through `sdk/def/internal/deftest`. Some tests also call `kernel.New`. The launcher's tests use no `kerneltest`: they run the production constructor through `boot.Run` (`boot/run.go` line 305). The exception is recorded here. The kernel carries G1 to G12, which are not on `main` (§2). | `loader/helpers_test.go`; `plugins/*/plugin_test.go`; `sdk/def/*/v0/fake/*_test.go` | `ab9b405` |
| 6 | ADR-059 decision 2: five kernel additions on a branch, merged after the S0 gates are recorded, then the corpus, the property workload and the call-path gate again; the kernel inside 8,200 lines | **pending**. Not merged: the S0 gates on the final port are not all recorded (§2). The kernel is 8,043 of 8,200 lines. The branch holds twelve additions, not five. G9 and G12 rest on decisions 13 and 20; G6, G7, G8, G10 and most of G11 rest on no decision (§7(a) 14). Answered: decision 36 approves G6, G7, G8, G10 and G11. | `git diff --stat main ab9b405 -- sdk/kernel`; ADR-034, "S1 additions" | `ab9b405` |
| 7 | ADR-059 decision 3: every cell has `since`; S1 passes every S1 cell and skips none; a test keeps the file and the table in agreement | **met**. 105 cells: 41 S1, 42 of later steps, 22 n/a. `TestMatrixMatchesTable` and `TestS1CellsAreRunnable` pass. | `test/failure-matrix/matrix.yaml`; `test/failure-matrix/parity_test.go` | `ab9b405` |
| 8 | ADR-059 decision 4: ten named gate slots; S1 builds the checks that read S1 data | **met**. Ten slots. Checks 7 and 8 are built; part of each waits for S2a, and part of check 8 for S4b. Checks 1, 2, 4, 5, 6 and 10 report "not evaluated" until S2a, checks 3 and 9 until S2b. | `boot/gate.go`, `GateChecks` | `ab9b405` |
| 9 | ADR-059 decision 5: four of the five recomposition cases at S1 | **met**. As row 3. | `test/recomposition/` | `ab9b405` |
| 10 | ADR-059 decision 6: one YAML library, `go.yaml.in/yaml/v3`, pinned, entered in the licence register, forbidden outside `boot/` and `tools/` | **met**. Pinned at v3.0.6. Imported only by `boot/dist.go`, `boot/matrix.go` and `boot/parse.go` (`go list -f '{{.Imports}}' ./...`); `tools/kernelcheck` names it only as a string, in the analyzer that forbids it. Entered in the licence register on 2026-11-21 from the module's own LICENSE and NOTICE (MIT for the files ported from libyaml, Apache-2.0 for the rest), with the commit that adds this report. | `go.mod`; `tools/kernelcheck/imports.go`; `docs/design/licence-audit-2026-11.md`, row "YAML parser of the launcher" | this report's commit |
| 11 | ADR-059 decision 9: tests drive the binary through two test-only rows that no shipped distribution holds | **met**. `testsubmit` and `teststate` live in `test/dist/plugins`. `go list -deps` of `./cmd/aiops` and of `./cmd/aiopsctl` lists no `test/dist` package. | `test/dist/plugins/{testsubmit,teststate}` | `ab9b405` |
| 12 | ADR-059 decision 11: caps of `boot/` and `loader/` fixed at the S1 exit as the measured count plus ten per cent | **met**. ADR-059, "Line budgets at the S1 exit", records decision 11 as applied: caps of 6,300 and 1,100, set on counts of 5,707 and 1,000. The counts measured now, 6,160 and 1,097, are inside them. §7(a) 5 proposes measuring again at the commit that closes S1. Answered: decision 28. | `kernelcheck lines`; ADR-059 "Line budgets at the S1 exit"; `ci.yml` lines 100 and 105 | `ab9b405` |
| 13 | ADR-059 decisions 12 to 23 (owner decisions of 2026-11-20 and 2026-11-21) | **met** as checked by rounds 2 and 3: 11 checks in round 2 and 7 in round 3, none failing at the end of either round. Three items of the lead, in the text and build notes of these decisions, are pending the owner (§4). Answered: decisions 24 to 26. | round 2 and round 3 result files; `test/failure-matrix/` | `ab9b405` |
| 14 | Structural checks: kernel cap, module file, vocabulary, test constructor, imports, plugin vet, per-file coverage | **met**. Kernel 8,043 of 8,200 lines (measured). The round 3 result file records on `ab9b405`: modfile, vocab, testctor, imports and pluginvet pass (two allow-go exemptions in `boot/run.go`); 100% per-file coverage for `boot`, `loader` and the five core plugins; 20 kernel coverage runs with zero uncovered blocks. A hosted CI run on `ab9b405` is not recorded. | `ci.yml` lines 75 to 107; round 3 result file | `ab9b405` |

**Overall.** S1 is not closed. Every row of §1 is met or met with a recorded exception except row 6, the merge of the kernel additions, which waits for the S0 gates on the final port (§2). The owner questions of §7(a) are open. Among them are three items of the lead that the code already implements or the ADR already states (§4). Answered on 2026-11-21 by ADR-059 decisions 24 to 39, except the round 1 questions with no recorded answer, which are left to the record of the S1 exit (decision 39). The corpus, property and call-path runs follow the merge (§2).

## 2. Dependency on S0

The kernel additions are built on branch `s1/integration` (decision 2). No kernel change reaches `main` until the S0 gates on the final port are recorded (decision 1). S0 itself closes only on the owner's approval of the S0 report. The branch changes 34 files under `sdk/kernel` against `main`, with 3,081 insertions and 167 deletions, tests included (`git diff --stat main ab9b405 -- sdk/kernel`).

**Kernel additions on the branch (ADR-034, "S1 additions").** G1 a child mount in a batch (`MountOp`). G2 an update with a config in a batch (`UpdateOpWith`). G3 `NeedKey`. G4 `InterceptType`. G5 the three kernel events with the ledger event as payload, and `registry.Fiber`. G6 `Commit` and `CommitOp`, a config committed without a restart. G7 a per-fiber config change lock. G8 `RunWith`, the owner's step innermost in a waterfall. G9 a drain past its deadline is a leak (`ErrLeaked`). G10 the kernel logs failed tracked goroutines and leaks. G11 the root observer sees the events of `Close`, with the exported field `ObserverOverflow.Stall`. G12 `KeyClasses`, `HookSealed` and `HookUse.Sealed`. The kernel grew from 7,451 lines at the freeze to 8,043, by 592 lines; ADR-059 foresaw "about 230". ADR-034's S1 addendum gives the steps: 7,900 with G1 to G11, 7,981 and then 7,985 after the fixes of the first review, 8,014 after those of the second, and 8,043 with G12.

**What the merge requires (decisions 1 and 2).** After the S0 gates on the final port are recorded:

- Merge the additions the owner approved: G1 to G5 (decision 2), G9 (decision 13), G12 (decision 20) and the root observer's effect in G11 (decision 21). Merge G6, G7, G8, G10 and the rest of G11 only if the owner approves them (§7(a) 14). Answered: decision 36 approves them.
- Record their exact shapes in ADR-034.
- Reconcile the S0 kernel backend adapter. `main` has 4 commits the branch lacks; two of them (`dcc5f10`, `1f68f32`) change `test/spike/s0/concurrency/kernel_backend.go`, by 146 insertions and 25 deletions since the merge base. The branch changed the same file by 132 insertions and 37 deletions (`git diff --stat ab9b405...main` and `main...ab9b405` on that file). In all, `main` changed 11 files, with 436 insertions and 41 deletions, since the merge base.
- Then run the differential corpus, the property workload and the call-path gate again on the merged kernel, and measure the kernel again (ADR-059, Consequences).

**Evidence on the branch kernel so far.** These runs are not the checks that decision 2 asks for after the merge.

- Differential corpus. The live oracle comparison passed on `ab9b405` at `GOMAXPROCS=8` in 68.4 s of package time (run for this report; the time varies between runs). The round 3 result file records passes at `GOMAXPROCS=8` (72 s) and `GOMAXPROCS=2` (85 s).
- Property workload. The last full pass ran on `4a8f20d`, with G1 to G11: 1,000 seeds × 1,000 operations on three backends, `-race`, exit 0, 5,750.02 s (`test/spike/s0/results/property-full-s1-kernel/invocation.json`). Since then the kernel and the S0 adapter changed: 27 files, 1,457 insertions, 231 deletions (`git diff --stat 4a8f20d ab9b405 -- sdk/kernel test/spike/s0/concurrency test/spike/s0/properties`). No evidence file for `ab9b405` exists under `test/spike/s0/results`. A full property run on `ab9b405`, in a frozen checkout, was in progress when this report was written. Its result is not recorded.
- Call-path gate. Not recorded for the kernel with the S1 additions.

**State of the S0 gates.** On `main`, `s0-report.md` (commit `3715bfe`, updated 2026-11-20) lists rows 8, 9 and 10 as open: the 10,000-fiber bench, the 500 tenant trees and the cold mount are being rerun on the final port, with the call-path gate. Every other row there is met or met with a recorded exception, except row 5, which is closed by a design change. Row 4 is met at a cap of 8,200 lines. The copy at `ab9b405` is older. It lists rows 2, 5, 6, 8, 9, 10, 12 and 13 as open, row 5 as not met and row 4 as awaiting the owner (`git diff --stat main ab9b405 -- docs/design/s0-report.md`: 7 insertions, 7 deletions).

## 3. What was built

Line counts are physical lines of non-test Go, by `go run ./tools/kernelcheck lines <dir> 999999`. That tool refuses a package with subdirectories, so `wc -l` over the non-test files gives the rows marked with an asterisk.

| Package | Role | Files | Lines | Cap |
|---|---|---:|---:|---:|
| `sdk/kernel` | kernel with G1 to G12 | 20 | 8,043 | 8,200 |
| `boot` | launcher library: layers, gate, graph, required set, plan, dumps, run | 18 | 6,160 | 6,300 |
| `loader` | the `loader` plugin and the `composition` service | 3 | 1,097 | 1,100 |
| `plugins/reload` * | the one queue that changes the tree | 2 | 889 | none |
| `plugins/restart-policy` * | retries and give-ups | 2 | 675 | none |
| `plugins/config-refs` * | resolves `${env\|file\|secret\|flag:...}`; `configRefsReady`, `configRefsDryRun` | 2 | 317 | none |
| `plugins/clock` | now and one-shot timers (decision 8) | 1 | 149 | none |
| `plugins/readiness` | the ready latch | 1 | 143 | none |
| `sdk/def` * | separate module: `launch`, `loader` (with its conformance suite), `composition`, `reload`, `clock`, `configrefs`, each with a fake | 21 | 2,785 | none |
| `cmd/aiops`, `internal/serve`, `internal/runfile` | server binary and its command line | 3 | 270 | none |
| `cmd/aiopsctl` | `aiopsctl profile render` | 2 | 210 | none |
| `dist` | release module table, `dist.yaml`, `limits.yaml` | 1 | 121 | none |
| `test/dist` * | test distribution and its binary | 9 | 1,752 | none |

- **Six launch services** (`sdk/def/launch/v0`): `profileContext` (with `Compose`, `Admit` and `Plan` for `reload`, decision 7), `catalog`, `cmdline`, `ready`, `killSwitches`, `appExit`. Each is provider-bound to the launcher; `appExit` is also consumer-bound.
- **Core plugins**: `loader`, `reload`, `restart-policy`, `readiness`, `config-refs`, `clock`. The release `dist.yaml` requires the key `clock` and the rows `loader`, `restart-policy`, `reload` and `readiness`.
- **Two binaries**: `cmd/aiops`, the server, which writes its status to `<home>/run/status.json`; `cmd/aiopsctl`, which renders a profile with `--dump`, `--dump-hooks` and `--dump-config-schema`. A third binary, `test/dist/cmd/aiops-test`, links the test distribution and ships nowhere.
- **Test distribution** (`test/dist`): the release module table plus three toy plugins (provider, consumer, heavy spine provider), seven fault plugins (`fail-apply`, `panic-apply`, `slow-apply`, `bad-config`, `rogue-provider`, `leaky-goroutine`, `slow-dispose`) and the two test-only control rows of decision 9 (`testsubmit`, `teststate`), with bundles, profiles, layer 4 patches and short limits (`test/dist/limits.yaml`: 1 s per kernel deadline, a shutdown bound of 10 s, a leak grace of 500 ms).
- **Suites on the built binary**: `test/failure-matrix` (the matrix, its harness and five parity tests), `test/recomposition`, `test/loader-smoke`. The loader conformance suite runs against the real loader and the fake.
- **CI** (`.github/workflows/ci.yml`): hygiene and secret scan; gofmt, build, vet and `go test -race -short` for the root module, and build, vet and race tests for `sdk/kernel`, `sdk/testkit` and `sdk/def`; kernel checks (modfile, vocab, lines 8,200, testctor, coverage); `kernelcheck imports`; `kernelcheck pluginvet plugins loader boot`; boot checks (lines 6,300, coverage); loader checks (lines 1,100, vocab, coverage); the `s1-suites` job (`make s1-suites`, race-built, fails on any race report); PostgreSQL probes; the oracle comparison.

## 4. Decisions

ADR-059, one line each:

1. S1 starts beside the last S0 gates; S0 closes only on the owner's approval of the S0 report; no kernel change reaches `main` until the S0 gates on the final port are recorded.
2. Five kernel additions, built on a branch, merged after the S0 gates are recorded, then verified by the corpus, the property workload and the call-path gate; the kernel stays inside 8,200 lines.
3. Each matrix cell carries `since`; S1 passes every S1 cell; later cells report "not yet"; a test keeps the file and the table in agreement.
4. The gate has ten named slots from S1; S1 builds the checks that read S1 data (7 and 8); the others report "not evaluated".
5. The recomposition suite runs four of five cases; promotion waits for S4b.
6. One YAML library, in `boot/` only, pinned and in the licence register; an analyzer forbids it elsewhere.
7. `reload` reaches composition through a launch service of `boot`; a plugin never imports `boot`.
8. A minimal `clock` definition with a fake and one row.
9. Tests drive the binary through two test-only rows that no shipped distribution holds.
10. Volatile fields are listed in the module table until S2a; a `secret:` reference does not resolve until S3.
11. Caps of `boot/` and `loader/`: measured count plus ten per cent at the S1 exit, one package each.
12. A live generation whose verdict fails is rolled back by one inverse plan; `rollback failed` is not retried.
13. A drain past its deadline is a leak, and a leak replaces the process.
14. At boot an unmet required set stops boot; "Readiness fails" is for a running process.
15. A live generation with an unknown or denied row is rejected whole; the required set follows the applied generation.
16. A live reference is checked at admission through a dry-run key of `config-refs`.
17. A volatile-only change that holds a reference restarts the row; one rule in the loader definition.
18. A required row with a reference pulls `config-refs` and `configRefsReady` into the required set.
19. A row that failed during the apply fails the verdict; `restart-policy` holds its retries until the operation ends.
20. A read-only kernel accessor reports key classes and sealed hooks (G12); gate check 7 derives classes from definitions.
21. A launcher observer registers no effect on the root.
22. `restart-policy` may lose its records across its own restart at S1 (the wording since commit `9cf418c`; the owner's text said it keeps none, and the change is pending the owner; answered: decision 26); a rolled-back candidate's records never reach the restored row.
23. After `rollback failed` or an abandoned applied generation the state is unknown: `reload` refuses every `Submit` until restart and readiness stays dropped.

Also accepted on 2026-11-21: a change of the `reload` row itself is applied after its generation's verdict passed and is not judged.

ADR-059 also holds a section "configRefsReady (recorded 2026-11-21)". It is neither a numbered decision nor marked pending. Round 1 asked whether the owner accepts it as a numbered decision (round 1 result file); no answer is recorded. Decision 18 replaced one of its rules. Answered: decision 38.

**Proposed by the lead, pending the owner** (ADR-059, "How decisions 16 to 23 are built", and the round 3 result file). Answered on 2026-11-21 by decisions 24 to 26:

- **Decision 18 by key.** A row required by key counts as a required row: a reference held by an active provider of a required key in the root realm pulls `config-refs` in, and a profile that then disables `config-refs` is refused at admission. `configRefsReady` stays in the set when every `config-refs` row runs in another role. Question: "Does decision 18 also pull config-refs into the required set when an active provider of a required key in the root realm holds a reference, so that disabling config-refs then refuses the profile at admission?" This rule is pending the owner. The code does this today. `TestPendingConfigRefsRuleIsMarked` fails when a paragraph that cites decision 18 states the rule by key in one of four phrasings ('by key', 'a required key', 'of the required set', 'inside the set') without 'pending the owner'. Answered: decision 24.
- **Loader, key wait.** A new provider of a key waits until a previous provider of that key in the same realm is disposed: one that this plan or an earlier one removed, disabled or replaced, or that a group activation that ended gave up. The plan's second batch mounts it. A row restarted in place is not held, because admission refuses two providers of one key in one realm. Two items are recorded. First, confirm the lead's rule by key for one plan, with that exception for a row restarted in place (round 3 result file; ADR-059, "Loader."). Round 2 first put it as: "should the loader or the kernel delay a new provider of a key until the previous provider has withdrawn it?" Second: "Should a new provider of a key wait for a provider of that key in the same realm that an earlier operation retired, or that a group restart gave up, and that is still disposing, as the id rule does for earlier plans?" The code does both today. Answered: decision 25.
- **Decision 22, wording.** The owner's text said that `restart-policy` "keeps no records across its own restart at S1" and "starts its records afresh". Commit `9cf418c` changed it to "may lose its records" and "may start its records afresh". The round 3 result file marks the change as needing the owner: "May restart-policy keep its records across its own restart at S1, so that decision 22 reads MAY lose instead of keeps none?" The code starts the records of each activation afresh (ADR-059, how 22 is built), which meets both wordings. Answered: decision 26.

## 5. Results

### 5.1 Failure matrix

`go test -count=1 ./test/failure-matrix/... -v | grep cells`: `cells: 41 pass, 0 FAIL, 42 not yet, 22 n/a`, 21 rows × 5 columns, 105 cells. The race-built run gave the same counts and wrote no race report.

| Column | pass | not yet | n/a |
|---|---:|---:|---:|
| Optional row at boot | 11 | 4 | 6 |
| Required at boot | 12 | 4 | 5 |
| Live recomposition | 12 | 7 | 2 |
| While running | 6 | 9 | 6 |
| Tenant tree | 0 | 18 | 3 |
| **Total** | **41** | **42** | **22** |

"Not yet" by step, from `since` in `matrix.yaml`: S2b 5, S4b 17, S5 11, S6b 2, S8a 7. The 18 "not yet" cells of the tenant-tree column are 15 of S4b, 2 of S8a (rows 14 and 15) and 1 of S6b (row 20). Rows passing in every process-scope column that applies: 1 to 7, 9, 17, 18, 19 and 21. Rows 8, 10, 16 (S5), 11 (S2b), 12 to 15 and 20 wait for their steps.

### 5.2 Recomposition, loader smoke, golden dumps

- Recomposition (`go test -count=1 -v ./test/recomposition/...`): pass for (a) the spine provider restarts in place under load, (b) a submission from a leased call, (c) the judge waits for an UNLOADING fiber, (d) a newly enabled row that never activates is rolled back, and the extra case "a first reference that does not resolve is rejected" (decision 16). (e) a replica restart during promotion is skipped: "not yet (S4b)". The race-built run gave the same results.
- Loader smoke (`go test -count=1 -v ./test/loader-smoke/...`): `TestLoaderSmoke` passes, plain and race-built. It boots the real launcher, loader, core rows and kernel in one process.
- Golden dumps: `TestComposeGolden`, `TestDumpGoldenS1` and `TestDumpGoldenRich` in `boot`, and `TestGoldenDumpsOfTheTestDistribution` in `cmd/aiopsctl`, pass. They pin stage 0 for every hook listener (§1, row 4). Golden dumps per role come after the first community release (08 §2).
- Unit tests of every S1 package pass (`go test -count=1`, run for this report), in the root module, in `sdk/def` and in `sdk/kernel`. The race runs of `sdk/kernel` are those of the round 3 result file (§5.3).

### 5.3 Kernel with the S1 additions

- Live oracle: pass on `ab9b405` (§2).
- Property workload: last pass on `4a8f20d`; the result of a full run on `ab9b405` is not recorded (§2).
- Call-path gate: not recorded.
- Round 3 result file, on `ab9b405`: `go test -race -count=3` passes at the default `GOMAXPROCS` and at 2; `FuzzOperationSequence` passes for 60 s.

## 6. The review and fix campaign

| Round | Span | Commits | Result |
|---|---|---:|---|
| 0, review | at `e5cf782` | none | A first pass gave 72 findings. A second pass gave 24 more, over six areas the first pass had not examined: boot's observer under queue overflow, `restart-policy` retries racing `reload`'s rollback, a stop during a live generation, whether recomposition case (a) drains a lease, the suites never running race-built, and the readers of the loader's records. 95 survived verification and were merged into 75 by root cause: 17 high, 31 medium, 27 low, none critical. All 75 confirmed, each with a recorded reproduction by running code. One further finding refuted. Grouped into 12 fix clusters. |
| 1, fix | `e5cf782..b63f08a` | 106 | 70 of 75 fixed on re-check; 5 not fully. 20 new confirmed defects in the fixes (6 high, 6 medium, 8 low). These led to owner decisions 16 to 23. |
| 2, fix | `b63f08a..29877cb` | 56 | Decisions 16 to 23 built. Of 11 checks, 6 held at once; the other 5 (D16, D20, D22 and two carried items) were repaired in the round. No failing check. 6 new defects (3 high, 1 medium, 2 low). |
| 3, fix | `29877cb..ab9b405` | 12 | All 6 fixed; 7 checks hold; no failing check. 1 new item: a code comment that cited decision 18 for the rule by key without "pending the owner". The file rates it medium; it changes no behaviour. Fixed in `ab9b405`: the comment in `boot/launch.go` now says the rule is pending the owner, and `TestPendingConfigRefsRuleIsMarked` (added in `4b59955`) now also catches this wording. Answered: decision 24. |

Commit counts are from `git rev-list --count <span>`. Main defect classes and how each was closed:

- **Loader tree bookkeeping against group activations.** A refused plan's restore clobbered a group activation; a group and `Apply` mounted the same child; a second batch mounted from an ending group context. Closed by undoing a refused plan without touching group activations, mounting nothing from an ending activation, leaving child disposal to the kernel, never creating over a disposing fiber, vetting the children of a replaced group, a fixed batch order (vets, mounts, disposes, updates), and the key wait of §4, which is pending the owner (answered: decision 25).
- **Reload judging and rollback.** The judge read rows before the tree settled, treated partial applies as untouched and used the previous required set; a rollback mounted gate-detached rows. Closed by settling the whole generation before judging, judging and committing any apply that touched the tree, committing the admitted copy, decisions 19 and 23, judging a rollback against the state before the generation, and `loader.OpRestart` for a FAILED row whose inverse update restarts nothing.
- **The config-refs readiness gate.** `configRefsReady` was driven by an asynchronous observer that could overflow, so rows saw unresolved references. Closed by having `config-refs` provide the key and every row with a reference inject it, then the admission dry run (decision 16) and the required-set rule (decision 18), with its extension to providers of required keys and to rows in other roles, which is pending the owner (§4; answered: decision 24).
- **Readiness during leak grace.** The latch was set again during the grace window. Closed by keeping readiness down after a leak, publishing in order, letting a leak end boot's unload wait, recomputing off the observer dispatcher, and decision 23.
- **Restart-policy bookkeeping.** Records were keyed by fiber id; `stable` omitted meant a fiber was never given up; candidate failures reached the restored row; the clock kept one effect per timer. Closed by counting by ACTIVE time and incarnation, holding retries while a generation is in flight (decision 19), records per activation (decision 22), and timers on one tracked goroutine.
- **Suites that could not fail.** Cells passed before the behaviour ran (6.1, 7.4, row 9), recomposition (a) and (b) could not detect a broken drain, and the suites never ran race-built. Closed by rewriting those cells and cases, running the suites race-built in CI, and requiring each later fix to land with a test that fails on the base; rounds 2 and 3 also checked the tests by mutation.

## 7. Open items

### (a) Owner questions

1. **Decision 18 by key** (§4), pending the owner. Recommended: yes. Without it a profile whose provider of a required key holds a reference, with `config-refs` disabled, is admitted and boot then stops at the required audit (round 3 result file; `dist/dist_test.go`). **Answered: decision 24.**
2. **Loader key wait, one plan and earlier plans** (§4). Recommended: yes to both, with the exception for a row restarted in place. Without the wait the new provider goes FAILED with "duplicate provision". On `29877cb` each of the four one-plan waiting cases failed 20 of 20 runs, and 80 of 80 under `-race` (round 3 result file). The two earlier-plan cases failed on `27223f4`; the run count is not recorded. In round 2, a plan that removes one provider of a key and creates another failed in 38 of 50 runs with the old batch order and in 24 of 50 with the new one (round 2 result file). Under decision 19 such a generation would be rolled back at random. **Answered: decision 25.**
3. **Decision 22, wording** (§4). Recommended: accept "may lose". It lets a later step keep records, and the code meets both wordings. **Answered: decision 26.**
4. **A provider of a required key whose own need has a provider only in another role.** Admission lets the profile through with a warning, and boot then exits 3. The no-provider rule (`boot/graph.go` lines 207 to 210) makes a missing provider an error only for a required row or a required key. Round 3 closed this for `configRefsReady` alone and left the general case to the owner (round 3 result file). Recommended: decide it with item 1. If item 1 is accepted, make the no-provider rule treat a provider of a required key in the root realm as it treats a required row, so that admission refuses such a profile. **Answered: decision 27.**
5. **Line budgets: a proposal of this report.** ADR-059 records decision 11 as applied, with caps set on counts of 5,707 (`boot`, 18 files) and 1,000 (`loader`, 4 files). Measured on `ab9b405`: 6,160 (18 files) and 1,097 (3 files); `loader` has 3 lines of headroom. Proposed: measure again on the commit that closes S1, after the merge, and apply the rule of decision 11 to that count. On `ab9b405` it gives 6,160 × 1.10 = 6,776, cap **6,800**, and 1,097 × 1.10 = 1,206.7, cap **1,300**. This changes caps that ADR-059 records as applied, so it needs the owner's approval. ADR-059, `ci.yml` and the Makefile change together. **Answered: decision 28.**
6. **Decision 6, register entry.** Done with this report: `go.yaml.in/yaml/v3` v3.0.6 is read and entered in the licence register. No question remains.
7. **Exit codes** (`boot/run.go`): 0 OK, 1 refused before any row mounted, 2 usage, 3 required set not met, 4 leak or shutdown overran, 5 `restart-policy` gave up on an infrastructure provider, 6 launcher failure, 7 restore failed; `aiopsctl` uses 0, 1 and 2. Code 2 is also the exit code of a Go panic (`boot/run.go` line 37), so a usage error and a fatal panic share it. No S1 component requests code 7; only tests do, and decision 23 places the restore at S4a. Recommended: adopt them as written and record them in the launcher documentation. **Answered: decision 29.**
8. **Default limits** (`boot/limits.go`, `dist/limits.yaml`): 10 s each for load, drain and dispose; shutdown bound 30 s; settle bound their sum; leak grace the drain deadline; archive 256, event window 1,024, observer queue 65,536, owner inbox 256, terminal limit 256. Recommended: accept as release defaults for now and revisit with the S3 restart-under-load measurements. **Answered: decision 30.**
9. **Runtime directory and readiness channel.** `<home>/run/status.json`, written atomically, carries phase, ready, profile, role, digest, exit and detail. Recommended: accept for one process at S1, and decide the orchestrator-facing probe when the `http` definition exists. **Answered: decision 31.**
10. **Shape of `conformance.Run`**: `Run(t, Mount)`, with `Mount` returning `(loader.Loader, *kernel.Root)`, chosen because the docs sketch only `Run(t, root)`. Recommended: adopt it as the pattern for every definition suite from S2a. **Answered: decision 32.**
11. **`RegistryKey` consumer-bound** (ADR-034 G5, open point). Whoever holds the registry can dispose and restart any fiber. Recommended: accept. **Answered: decision 33.**
12. **A veto of `loader/patch-context` fails closed** (ADR-034 G8, open point). Recommended: accept. **Answered: decision 34.**
13. **`profileContext` carries `Compose`, `Admit` and `Plan`** (`sdk/def/launch/v0/doc.go`, open point). Decision 7 names compose, admit and plan as the launch service of `reload`, and decision 15 has `reload` call `Commit` on `profileContext` and keeps six launch keys. Recommended: confirm that `Compose`, `Admit` and `Plan` sit on that key, and remove the open-point note from `doc.go`. **Answered: decision 35.**
14. **Kernel additions not named by an ADR-059 decision.** G6 (`Commit`, `CommitOp`) and G8 (`RunWith`) add exported functions; the loader uses both. G7 adds a per-fiber config lock. G10 adds ERROR records to the existing `Config.LogHandler`. G11 delivers the events of `Close` and adds the exported field `ObserverOverflow.Stall` (`sdk/kernel/observe.go` line 92, absent on `main`). Decision 13 covers G9, and decision 21 covers only the root observer's effect in G11. Recommended: approve G6, G7, G8, G10 and G11 in the merge record of ADR-034. **Answered: decision 36.**
15. **Open points that S1 recorded in its own code.** In `sdk/def/launch/v0/launch.go`: `Committed`, `Commit`, `Required` and `RequiredOf` on `profileContext`, added in S1 (decision 15 names the first three); `Catalog.Plugin`, which hands the plugin value to every consumer of the open class; `Cmdline.Env`. In `sdk/def/loader/v0/loader.go`: an id removed and created again goes in a second batch; the minimal payloads of `loader/patch-context`, `loader/entry-init` and `loader/volatile-update`. In `sdk/def/reload/v0/reload.go`: `Op.Source`, and the `ctx` that `Submit` takes. In `sdk/def/composition/v0/composition.go`: the field names of `Row`. Behaviour: a loader wait that is cut short leaves the first batch committed and an entry with no fiber (`loader/apply.go`); `reload` abandons queued operations when its row unloads (`plugins/reload/internal/engine/engine.go`); a waterfall listener cannot pass a changed value to `next`, an open point for the kernel (`plugins/config-refs/plugin.go`). Recommended: settle the definition points before S2a. From S2a, whoever changes a `v0` definition fixes every plugin under `plugins/` and `contrib/` in the same pull request (08 §2). **Answered: decision 37.**
16. **The section "configRefsReady (recorded 2026-11-21)"** (§4). Recommended: accept it as recorded, with the rule that decision 18 replaced. **Answered: decision 38.**
17. **Round 1 questions.** The input file of round 2 lists 15 answers to round 1 questions, which the round 2 result file calls "the default answers". ADR-059 records one as accepted (the change of the `reload` row itself), and its build notes describe five more as built (`--refs-dir` checked at the command line, plugin vet refusing `net/http/pprof` and `expvar`, `time.AfterFunc` in `boot` with an allow-go reason, an empty entry id refused, `stable: 0` meaning no stable period). The other nine are recorded only in that input file: overlapping teardown of a loader's children, the hook limit of the loader's exclusion token, `WaitGroup.Go` in `reload`'s settle, the `reload/operation` event, one retry hold while any operation is in flight, three kernel rules (a child mount after a restart earlier in the batch is refused, a `CommitOp` after a `DisposeOp` of the same fiber is refused, `Close` bounds its observer wait by one drain deadline), the unexported registry config error, the observer queue of 65,536 and test limits of 1 s. Round 1 questions with no recorded answer: the stricter reading of check 8 for a patch on a group; the field that holds per-key intercept provenance; `restart-policy` treating a fiber that goes UNLOADING then LOADING as restarted by someone else; the clock edge case; whether `config-refs` refuses a file root named by another launcher flag; the untracked caller in recomposition case (a); a seam for a kernel-side oracle in the suites; requiring `Handler` in an `http.Server` literal (round 1 result file). Recommended: confirm the default answers and decide the rest in the record of the S1 exit. **Answered: decision 39** confirms the default answers; the questions with no recorded answer are left to the record of the S1 exit.

### (b) Known limits that remain

- A `Remove` that runs before a group's own detach fails with "batch operation 0: unknown fiber" (round 3, seen, not fixed).
- Coverage below 100% outside production S1 files: `sdk/def/loader/v0/conformance/conformance.go` 90.5%, `sdk/def/internal/deftest/deftest.go` 86.4%, and the S0 probe `plugins/jobs-river/internal/s0typedops/api.go` 92.6% (round 3). CI gates coverage for the kernel, `boot` and `loader` only; the 100% of the core plugins is recorded by the rounds, not enforced.
- A rollback restart of a FAILED group row whose only change is its config is tested with a scripted loader only (round 3).
- A FAILED child under a group that the inverse plan replaces gets an `OpRestart` on a fiber the remount retires; the fake checks this, the real loader does not (round 3).
- The conformance restart-counter check is skipped when the root holds no live fiber of the plugin, so a loader that names fibers differently would pass it silently (round 3).
- A rollback drops every operation on the `reload` row itself, an added restart included (round 3).
- Decision 19 rests on `restart-policy` being the only updater of fibers outside the loader; a later component that updates fibers needs the same hold (round 2).
- At the next activation of `reload` after an abandon, readiness may set the latch briefly before the unknown-state event drops it (round 2).
- Cell 21.2 stops at the required audit before any retry, so it shows "Stop" but not a give-up at boot (`matrix.yaml`).

### (c) Obligations handed to later steps

- **Merge, after the S0 gates on the final port are recorded:** merge the approved additions; record the shapes in ADR-034; reconcile the S0 kernel backend adapter with `main`; rerun the corpus, the property workload and the call-path gate; measure the kernel again.
- **S2a:** gate checks 1, 2, 4, 5, 6 and 10, and the parts of check 7 (signed keys and hooks, fakes in a server graph) and check 8 (the insert-only bundle of an installed artifact) that need S2a data (decision 4); `plugin.Define` and the generated half of `plugin.yaml`; volatile fields derived (decision 10); hook stage numbers in dumps.
- **S2b:** gate checks 3 and 9; five matrix cells.
- **S3:** `secret:` references (decision 10). Durable records for `restart-policy` come with the durable store (decision 22 says "a later step"; `durable` is built at S3, 08 §2.1).
- **S4a:** committer, lock and audit (decision 12); the restore that replaces the unknown-state rule (decision 23).
- **S4b:** the promotion case (decision 5); 17 matrix cells, 15 in the tenant-tree column and 2 in "While running" (12.4 and 13.4); the tenant-overlay part of check 8; layer 5.
- **S5, S6b, S8a:** 11, 2 and 7 matrix cells.
- **After the first community release, no step named (08 §2):** golden dumps per role.

## 8. Stop point

S1 stops here. This report, ADR-059 and the S1 addendum of ADR-034 form the S1 exit record. S1 is not closed until the owner approves this report. The kernel additions reach `main` only after the S0 gates on the final port are recorded (decision 1), and are then verified again (decision 2). S0 closes only on the owner's approval of the S0 report. S2a starts only on the owner's word.
