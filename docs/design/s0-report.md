# S0 exit report

- Date: 2026-11-20; final-port gates recorded 2026-11-22.
- Status: **S0 closed on 2026-11-22** by the project owner's approval of this report.
- For: the project owner, who decides whether S0 is closed and whether S1 may start.
- Sources: the roadmap S0 row and ADR table in [`08-profiles-roadmap-adrs.md`](../architecture/08-profiles-roadmap-adrs.md), the frozen criteria in [`s0-spike-criteria.md`](s0-spike-criteria.md), ADR-034, ADR-035, ADR-036 (accepted 2026-11-22) and ADR-045, and `test/spike/s0/results/`.
- Status vocabulary: **met**, **met with recorded exception**, **pending** (a run or decision is still needed), **not met** (a recorded failure stands).

## 1. Verdict

| # | S0 exit criterion | Status | Evidence | Commit |
|---|---|---|---|---|
| 1 | Differential scenarios match the reference | **met with recorded exceptions**. Port: 31 of 32 cases match, plus diagnostics D06 and D23. D25 is an owner exception. D05 and D26 are not carried. | [`ADR-035`](../adr/ADR-035-cordis-tracking.md) (parity section); [`s0-causal-coverage.md`](s0-causal-coverage.md); `test/spike/s0/results/causal-kernel-port/` | `e34047e` |
| 2 | Property test and `-race` clean | **met**. The property workload (1,000 seeds × 1,000 operations, `-race`) passes on three backends, the final port included, from a frozen checkout. `go vet` and `go test -race -count=3 ./...` pass in all three modules. | `test/spike/s0/results/property-full-final-port/`; `test/spike/s0/results/final-port-checks/race-count3.log` | fbcba92; 83b2746 |
| 3 | `sdk/kernel` has no `require` line | **met**. A search of the module file finds no `require` line. | `sdk/kernel/go.mod`; CI step `kernelcheck modfile` (`.github/workflows/ci.yml`, line 71) | current tree |
| 4 | Line budget fixed | **met**. The owner fixed the cap at 8,200 lines on 2026-11-20; 7,451 lines measured in 19 files, one package; CI enforces it. | ADR-034 ("Line budget, frozen 2026-11-20"); `.github/workflows/ci.yml` | f96a00c |
| 5 | Transaction spike (ADR-045) | **closed by design change**. The raw-SQL authority gate failed and stays recorded as failed. Under the rule "a missed threshold is a design change by ADR", the owner adopted typed operations as the plugin transaction surface on 2026-11-19. The §5 numeric thresholds of the raw surface are not recorded and no longer apply to it. | [`ADR-045`](../adr/ADR-045-cross-plugin-transaction.md); `test/spike/s0/results/typedops-pg-arbitration.json` | f2ea89f |
| 6 | Restart spike | **met**. 100 of 100 cycles at frozen scale with the resources owned by fibers of the final kernel, section 10 accounting without findings, recovery 252 to 454 ms. The library-owned run passed earlier. The unowned-handler snooze and the two-client case pass in the worker probe. | `test/spike/s0/results/restart-full-kernel-owned/`; `restart-full-section10/`; `worker-functional-corrected.json` | d683ebe |
| 7 | Both cores on the corpus | **met with recorded exceptions**. Prototype: 64 of 68 pairs match, in 100 of 100 runs under race and under two processors. The port is in row 1. | `docs/design/s0-causal-coverage.md`; ADR-035 | not recorded |
| 8 | Both cores on a 10,000-fiber bench | **met**. The final-port run passed with no reasons on the reconciler, graph-lock and `kernel` backends: every bound met and no bucket above the noise limit. The prototype run before it was invalid by the noise rule. | `test/spike/s0/results/scale-full-final-port/invocation.json`; §4.4 | `8ec2d8c` |
| 9 | 500 tenant trees | **met**. Same run as row 8. Cold tree mount at most 13.8 ms on `kernel` (bound 50 ms); retained heap for 500 trees of width 4 at most 29.2 MiB (bound 32 MiB). The owner decided the tenancy figures of ADR-036 on 2026-11-22 (§6(b)). | `test/spike/s0/results/scale-full-final-port/invocation.json`; §4.4; [`ADR-036`](../adr/ADR-036-tenancy-cost.md) | `8ec2d8c` |
| 10 | Cold mount on the ack path | **met**. The final-port run passed with no reasons on all three backends, `kernel` included: 180 runs, every request answered. | `test/spike/s0/results/coldmount-full-final-port/`; §4.5 | `8ec2d8c` |
| 11 | `sdk/testkit/kerneltest` | **met**. Built as a separate module and run in CI. | `sdk/testkit/kerneltest/`; `.github/workflows/ci.yml` lines 60 to 65 | current tree |
| 12 | Coverage: 100% statements per production file | **met**. Every production file of `sdk/kernel` is at 100.0%; CI enforces it per file on every push. | `test/spike/s0/results/final-port-checks/coverage.txt`; CI step "Kernel structural checks" | 83b2746 |
| 13 | Fuzzing, 60 s per selected entry point | **met**. Five targets ran 60 s each with no kernel defect. One target's own oracle was wrong under load and was corrected; its failing inputs are kept as seeds. Fuzzing is run by hand, not in CI. | `test/spike/s0/results/final-port-checks/fuzz.log` and `invocation.json` | b596374 |
| 14 | Notices | **met** for `sdk/kernel`. The NOTICE names the reference and keeps both copyright notices. Oracle package notices are in `s0-reference-pins.md` and were not re-checked here. | `sdk/kernel/LICENSE`; `sdk/kernel/NOTICE`; `docs/design/s0-reference-pins.md` | current tree |

**Overall.** Every row is met, met with a recorded exception, or closed by a recorded design change (row 5). The scale, cold-mount and call-path gates passed on the final port on 2026-11-22 (§4.3 to §4.5). The owner approved this report on 2026-11-22, and S0 is closed on that date; the owner's answers to the items of §6(b) are recorded there. Updated 2026-11-22.

## 2. What was built

### 2.1 `sdk/kernel`

One package, `kernel`, in module `github.com/mutex-solutions/aiops-platform/sdk/kernel` (`go 1.26.0`, no `require` line). Standard library only, as checked here: the non-test files that import a package use `context`, `reflect` or `testing`. `testing` is imported only by `root.go`, for the test constructor (ADR-034, decision 1). A full import listing was not produced.

Line counts are newline counts of the 19 non-test `.go` files, the rule used by `kernelcheck lines`. The tool was not run for this report. Confirm with `go run ./tools/kernelcheck lines sdk/kernel 8500` before the budget is frozen.

| File | Lines | Identifiers that show its role |
|---|---:|---|
| `owner.go` | 1,479 | owner goroutine and inbox (reconciler core) |
| `effect.go` | 997 | effects, leases, `ErrInactive` |
| `observe.go` | 850 | observer queue and overflow |
| `transition.go` | 611 | transitions, deadline heap |
| `hooks.go` | 455 | hook definitions and options (`OwnedBy`, `Stages`, `Scoped`, `Sealed`) |
| `key.go` | 444 | `NewKey`, `Define`, `Need`, `Want`, `Available`, `Intercepts` |
| `inspect.go` | 671 | `State` and inspection |
| `context.go` | 368 | `Context`, `Isolate`, `Intercept`, `Enter`, `Defer`, `Go` |
| `root.go` | 378 | `New`, `NewForTest`, `Root`, `Batch`, `Fiber` |
| `rootview.go` | 258 | snapshots, `Inspect`, `Events`, `Observe`, `Notify` |
| `config.go` | 137 | `Fiber.Update`, `ErrCommitVetoed` |
| `waterfall.go` | 222 | `Waterfall`, `NewWaterfall` |
| `event.go` | 225 | `Event`, `NewEvent` |
| `effectscope.go` | 94 | `EffectScope`, `EffectHandle`, `Context.Dispose` |
| `seam.go` | 151 | not described in this pass |
| `builtin.go` | 65 | the four built-in keys |
| `logger.go` | 30 | `Logger` |
| `registry.go` | 10 | registry built-in |
| `doc.go` | 3 | package documentation |
| **Total** | **7,448** | CI cap 8,500 |

The four built-ins are `reflect`, `registry`, `hooks` and `logger` (`ReflectKey`, `RegistryKey`, `HooksKey`, `LoggerKey`, in `builtin.go`).

**Exported surface.** Root and constructors: `New`, `NewForTest`, and `Root` with `Context`, `Plugin`, `Batch`, `Settle`, `Close`, `Snapshots`, `Inspect`, `Events`, `Observe`, `Notify`. Context and plugin model: `Context` (`Extend`, `Isolate`, `Intercept`, `Effect`, `Defer`, `Go`, `Std`, `Enter`, `When`), `Plugin`, `Define`, `Config`, and `Fiber` (`State`, `Await`, `Dispose`, `Effects`, `Update`, `ID`). Keys and dependencies: `Key`, `NewKey`, `Need`, `NeedWith`, `Want`, `Available`, `Intercepts`, `ProviderBound`, `ConsumerBound`, `Signed`, `InterceptConfig`. Hooks, waterfalls, events and effect scopes: `HookOption`, `OwnedBy`, `Stages`, `Scoped`, `Sealed`, `Waterfall`, `NewWaterfall`, `Next`, `Event`, `NewEvent`, `EffectScope`, `EffectHandle`. Errors: `InjectError`, `ListenerError`, `PanicError`, `ScopePanicError`, `ErrInactive`, `ErrInactiveEffect`, `ErrCommitVetoed`. Batch operations: `MountOp`, `UpdateOp`, `DisposeOp`. Observers: `ObserverStatus`, `ObserverOverflow`, `RetentionStatus`, `Inspection`, `FiberSnapshot`, `Provision`, `LedgerEvent`, `OnOverflow`.

### 2.2 `sdk/testkit/kerneltest`

A separate test-only module (`kerneltest.go`, `doc.go`, `kerneltest_test.go`) that wraps the kernel test constructor (ADR-034, decision 1). File bodies were not read in this pass. CI builds and tests it on the real runtime.

### 2.3 `tools/kernelcheck` and CI checks

`tools/kernelcheck` has five subcommands: `modfile`, `vocab`, `lines`, `coverage` and `testctor`. The word list is `tools/kernelcheck/vocabulary.txt`. CI runs them in `.github/workflows/ci.yml`:

- Module file (line 71): `kernelcheck modfile sdk/kernel`. No `require` line in the module.
- Vocabulary (line 72): `kernelcheck vocab` against the word list. No product word in kernel source.
- Line count (line 73): `kernelcheck lines sdk/kernel 8500`. Non-test total within cap.
- Test constructor (line 74): `kernelcheck testctor .`. The test constructor has one caller, `kerneltest`.
- Coverage (line 75): `kernelcheck coverage` on the coverage profile. Per-file statement coverage, under the rule named in the criteria; the function body was not read in this pass.

CI also runs `go vet ./...` and `go test -race -count=1 ./...` for the main, kernel and testkit modules (lines 52 to 65).

## 3. Decisions taken in S0

- **Core is `reconciler`.** One owner goroutine holds all lifecycle state. The `graph-lock` core stays in `test/spike/s0/concurrency` as a test implementation and is not ported. (ADR-034, 2026-11-19.)
- **Typed operations are the plugin transaction surface.** The plugin `Tx` has no SQL-string method. Infrastructure keeps a private raw-driver path on the same transaction. The raw-SQL authority gate stays failed. (ADR-045, 2026-11-19.)
- **Jobs role outside row-level security on River's tables, with bounded retention.** River's five tables have RLS disabled and no policies. River's cleaner keeps finished jobs for 900 s in the restart gate. (ADR-045, 2026-11-18 decision.)
- **Parity declared complete, with exceptions.** D06 and D23 are diagnostics. D05 and D26 are not carried. D25 is an owner exception for the port. (ADR-035, 2026-11-19.)
- **Owner decisions 1 to 15 for the port** (ADR-034, 2026-11-19):
  1. One test constructor takes `testing.TB`; `kerneltest.New` wraps it; a check forbids other callers.
  2. `Isolate`, `Intercept`, `Plugin`, `Defer` and `Go` return no error; a refused call fails the fiber.
  3. Capacity bounds (archive limit, event window, observer queue, owner inbox) are required at construction; no kernel defaults.
  4. Load, drain and dispose deadlines are construction data for the whole kernel.
  5. No test recorder in the kernel.
  6. `Enter() (leave func(), ok bool)`, zero allocations per call.
  7. An entry is a token minted by `Define`; no `unsafe`, no identity from a function value.
  8. `Available` takes one argument.
  9. `Plugin.Decode` is `func(raw any) (any, error)`; the kernel knows no configuration format.
  10. A hook owner calls `Bind(c)` once in `Apply`; the hook value cannot dispatch.
  11. Sealed hooks have a third binding map, filled by the launcher, switched off by the test constructor.
  12. A waterfall fails closed; an erroring or panicking listener fails the dispatch. Event listeners are skipped and logged.
  13. `reflect`, `registry` and `hooks` are valueless until a consumer exists; `internal/plugin`, `internal/status` and `internal/service` wait for S1.
  14. D25 is not carried; `Plugin` has no second apply shape.
  15. A failing commit aborts the update; the new config is not retained and the fiber is not restarted.

## 4. Results

Prototype results are candidate evidence. Port results are candidate evidence until the final-port reruns.

### 4.1 Differential corpus

- Prototype cores: 64 of 68 scenario and candidate pairs match. Each matching pair passes 100 of 100 runs under race and 100 of 100 with two processors. The other four are the D06 and D23 exceptions (ADR-035; `s0-causal-coverage.md`).
- Port (`kernel` backend, `e34047e`): 31 of 32 cases match, plus the D06 and D23 diagnostics. D25 differs and is an owner exception. Logs: `test/spike/s0/results/causal-kernel-port/` (`gomaxprocs8.log`, `gomaxprocs2.log`).

### 4.2 Property workload

1,000 seeds × 1,000 operations under `-race`, exit code 0, on the reconciler, graph-lock and `kernel` backends. Elapsed 4,440.89 s on a shared development host; elapsed time is not a performance result. Evidence: `test/spike/s0/results/property-full-kernel-port/invocation.json`.

### 4.3 Call-path gate (prototype, fourth run; final port, pass)

- Evidence: `test/spike/s0/results/callpath-full-fourth/` (commit `c5f0a79`, 7,224 s, dedicated host). Classification **pass**.
- `Enter` and `Leave` allocate nothing in every aggregate on both prototype cores (`allocsPerOp` 0 for `owner_enter_leave`).
- `lookup_view` and `lookup_view_lease_call` report 3 allocations per operation. The zero-allocation rule covers Enter and leave only. Lookup costs are reported separately (criteria §8.3).
- Six aggregates show throughput variation of 11.5% to 17.0% under the mutation stress arm. Amendment 2 records this as a candidate characteristic, not an invalid run.
- Per-aggregate p99 values are in `report.json` and are not transcribed here. The criteria threshold is p99 at or below 2 µs.

Final port: `test/spike/s0/results/callpath-full-final-port/` (commit `8ec2d8c`, protocol amendment 2, three candidates, 10,834 s, dedicated host, `GOMAXPROCS=8`). Classification **pass**. No stress run overflowed, failed, timed out, ran an inactive body or overran a drain, on any candidate.

| `owner_enter_leave`, baseline arm | 1 caller | 8 callers | 32 callers |
|---|---|---|---|
| `kernel`, p50 / p99 | 151 / 370 ns | 315 / 1,054 ns | 332 / 1,073 ns |
| reconciler, p50 / p99 | 130 / 194 ns | 130 / 631 ns | 130 / 609 ns |
| graph lock, p50 / p99 | 130 / 287 ns | 130 / 367 ns | 130 / 436 ns |
| Allocations per run, every candidate | 0 | 0 | 0 |

- Every `kernel` p99 is inside the 2 µs bound. With 8 and 32 callers the `kernel` lease costs about 2.5 times the prototypes' at p50. The lease counter picks a random shard per call, so concurrent callers share cache lines. Changing that is a kernel change and is left to the owner. The owner kept it as is on 2026-11-22, to be reviewed at the S3 measurements (§6(b); ADR-034).
- Lookups: `lookup_view` and `lookup_view_lease_call` allocate 4 objects per operation on `kernel` against 3 on the prototypes, with p99 up to 3.2 µs at 8 and 32 callers against 2.5 to 2.8 µs. Lookup costs are reported, not bounded (criteria §8.3).
- Ten aggregates show candidate variation of 11.3% to 16.3% with a clean control, as in the fourth run: observations, not an invalid run.
- The first final-port run, on `dcc5f10`, failed: some `kernel` runs at one caller allocated one object. The adapter kept each activation's leave function on its first `Enter`, which allocated. `a4e3d82` keeps it when the Context is built.

### 4.4 Scale gate (prototype, invalid by the noise rule; final port, pass)

Evidence: `test/spike/s0/results/scale-full-first/invocation.json` (commit `225ffbe`, 10,800 runs, 22,372 s, dedicated 24-vCPU guest, `GOMAXPROCS=8`).

| Bound (criteria §8.1 to §8.2) | Reconciler | Graph lock | Result |
|---|---|---|---|
| 10,000-fiber mount and settle, p99 ≤ 2 s | 414 to 661 ms (worst: realms) | 298 to 644 ms | within bound |
| 10,000-fiber leaf update, p99 ≤ 10 ms | 0.25 to 0.36 ms | 0.18 to 0.27 ms | within bound |
| 10,000-fiber root restart, ≤ 5 s | max p99 1,020 ms (diamonds) | max p99 1,066 ms (diamonds) | within bound |
| Retained heap, 1,000 roots, ≤ 128 MiB | 73.3 MiB (chain) | 74.3 MiB (chain) | within bound |
| Cold tree mount, 500 × 4, < 50 ms | max 9.69 ms | max 9.05 ms | within bound |
| Shared provider restart, 500 × 4, ≤ 5 s | p99 161.8 ms | p99 167.1 ms | within bound |
| Goroutines at last checkpoint | 4 | 3 | recorded |
| Timer overhead p99 | 64 ns | not applicable | recorded |

Classification: **invalid by the noise rule only.** Fifteen buckets have throughput variation between 10.0% and 15.1%, against the frozen 10% limit. No bound failed on either backend. The gate is repeated unchanged on the final port.

Caveats: the run predates the 2026-11-19 hazard fixes. The retained-heap slope fields are recorded but not interpreted here (ADR-036). The tree heap record lists `"roots": 100`, which the file does not explain.

Final port: `test/spike/s0/results/scale-full-final-port/invocation.json` (16,200 runs on the reconciler, graph-lock and `kernel` backends, 32,434 s, dedicated host, `GOMAXPROCS=8`). The host's working copy was at `583e1bd`, which adds only documents and result files to `8ec2d8c`. Classification **pass, no reasons**: every bound met and no bucket above the 10% noise limit.

| Bound (criteria §8.1 to §8.2) | Reconciler | Graph lock | `kernel` | Result |
|---|---|---|---|---|
| 10,000-fiber mount and settle, p99 ≤ 2 s | 396 to 681 ms (worst: diamonds) | 277 to 611 ms (realms) | 389 to 881 ms (diamonds) | within bound |
| 10,000-fiber leaf update, p99 ≤ 10 ms | 0.21 to 0.33 ms | 0.16 to 0.25 ms | 0.33 to 0.45 ms | within bound |
| 10,000-fiber root restart, ≤ 5 s | max p99 1,052 ms (diamonds) | max p99 1,012 ms (diamonds) | max p99 1,188 ms (diamonds) | within bound |
| Retained heap, 1,000 roots, ≤ 128 MiB | 73.4 MiB (chain) | 73.5 MiB (chain) | 92.1 MiB (chain) | within bound |
| Cold tree mount, 500 and 300 trees, < 50 ms | max 9.83 ms | max 7.66 ms | max 13.79 ms | within bound |
| Retained heap, 500 trees of width 4, ≤ 32 MiB | 21.5 MiB | 20.5 MiB | 29.2 MiB | within bound |
| Shared provider restart, trees, ≤ 5 s | max p99 151.4 ms | max p99 118.9 ms | max p99 106.3 ms | within bound |
| Goroutines at last checkpoint | 4 | 3 | 4 | recorded |
| Timer overhead p99 | 56 ns | not applicable | not applicable | recorded |

The `kernel` backend holds more heap than the prototypes in every workload (92.1 against 73.4 MiB for the chain; 29.2 against 21.5 MiB for 500 trees of width 4), and its 500-tree figure leaves 2.8 MiB under the bound. Part of it is the S0 adapter, which keeps a handle and a config cell for every fiber the kernel still holds. An earlier run of this gate on `dcc5f10` was stopped after 2 h 12 min, unclassified, because the adapter it measured had been replaced (§4.5).

### 4.5 Cold-mount gate (prototype and final port, pass)

Evidence: `test/spike/s0/results/coldmount-full-first/` (commit `225ffbe`, 120 runs, 18,039 s). Classification **pass, no reasons**, per ADR-034. The directory has no `invocation.json`.

- A cold mount on the acknowledged path adds about 0.7 ms at p99 to a 100-item request and about 2.8 ms to a 1 MiB request.
- Churn beside pinned traffic moves p99 by 0.03 to 0.06 ms in the median matched pair and at most 0.16 ms (bound 5 ms).
- Pinned p99: 1.4 ms for 100 items (bound 250 ms) and 7.2 ms for 1 MiB (bound 1 s).
- The timeout fixture answers 503 with a retry hint, commits nothing, and never contributes from a foreign scope.
- Harness defect: `coldEnsureCalls` reported a shared counter for arm A (fixed in `c5ce385`). Totals and latencies are unaffected.

Final port: `test/spike/s0/results/coldmount-full-final-port/` (commit `8ec2d8c`, 180 runs on the reconciler, graph-lock and `kernel` backends, 27,060 s, dedicated host, `GOMAXPROCS=8`). Classification **pass, no reasons**. All 120,000 scheduled requests of every cell were answered `ok`.

| Measure | Reconciler | Graph lock | `kernel` | Bound |
|---|---|---|---|---|
| Cold mount added at p99 (arm A minus arm B0), 100 items | 0.70 ms | 0.62 ms | 0.93 ms | recorded |
| Cold mount added at p99, 1 MiB | 2.33 ms | 2.26 ms | 2.66 ms | recorded |
| Pinned p99 (arm B0), 100 items | 1.35 ms | 1.34 ms | 1.40 ms | 250 ms |
| Pinned p99 (arm B0), 1 MiB | 6.99 ms | 6.99 ms | 7.82 ms | 1 s |
| Churn beside pinned traffic, matched-pair p99 delta, median and max, 100 items | 0.014, 0.061 ms | 0.019, 0.068 ms | 0.018, 0.093 ms | 5 ms |
| The same, 1 MiB | 0.057, 0.098 ms | 0.045, 0.090 ms | 0.099, 0.235 ms | 5 ms |
| Arm A max, 100 items and 1 MiB | 9.2, 29.4 ms | 8.4, 30.8 ms | 14.0, 36.4 ms | recorded |

The first final-port run, on `dcc5f10`, failed on the `kernel` backend only: arm A p99 61.5 ms and max 467 ms, two overflow outcomes and two "disposed handle not archived" teardown failures. The cause was the S0 adapter, not the kernel. It swept every held fiber, one owner round trip each, inline on the mount path, and the sweep threshold rose only when a sweep finished, so mounts that arrived meanwhile each started another sweep. The stalls filled the request cap (overflow) and let stale requests finish after current ones, which pushed a current handle out of the one-record slack of the harness archive. `3bf2ea7` replaces the sweep with a check of at most two held fibers per mount, made only while the kernel has evicted more records than the adapter forgot. The identity's mode text ("candidate evidence, never final-port evidence") is fixed in the harness; the baseline commit and `finalParityCompleted` make this the final-port run.

### 4.6 Restart gate (library probe, frozen scale with leak accounting)

- Evidence: `test/spike/s0/results/restart-full-section10/invocation.json` (commit `e34047e`, 15,169 s, dedicated host). **100 of 100 cycles**: pool 30, client 30, listener 30, combined 10.
- Every acknowledged ID completed. The section 10 accounting passed in all four arms.
- The earlier frozen-scale rerun (`restart-full-frozen-scale`) recorded the longest acknowledgement-to-completion at about one second and the longest recovery at about 0.4 s (ADR-045).
- This gate runs on the standalone library. The rerun with the resources owned by fibers of the final kernel passed 100 of 100 cycles, recovery 252 to 454 ms (`restart-full-kernel-owned/`; row 6).

### 4.7 Transaction authority (ADR-045)

- The raw-SQL authority gate fails and stays failed. The recorded escapes are `SET LOCAL ROLE` to the jobs role, `RESET ROLE`, and `set_config` on the scope setting. Forced RLS did not prevent them.
- Typed operations were adopted on 2026-11-19. The probe (`plugins/jobs-river/internal/s0typedops`) commits and rolls back all participants atomically and refuses cross-scope access. Its result file calls it a "closed-surface fixture diagnostic", not an original gate pass.
- Criteria §5 thresholds (jobs-role round trip p99 at or below 2 ms; full caller transaction p99 at or below 25 ms; cost-control arm) are **not recorded**.
- The SQL boundary probe `TestSQLViewBoundary` still fails for its recorded reason.

## 5. Defects the harnesses found in the port

Found by the adapter backend (`kernel`) on its first run and fixed to match the reference (ADR-034, "the final port under the acceptance harnesses"):

1. **Liveness is decided first.** On an ended activation, `Provide` reported an undeclared key instead of inactive. Every entry point now reports inactive before any other refusal.
2. **An update resolves its config whenever the fiber has a target.** The port resolved only on an ACTIVE fiber. The reference also resolves during a gated load and a blocked teardown (corpus D17 and D20).
3. **D25 is an accepted exception, not a fix.** A load that disposes itself and then returns a disposer from apply has no Go equivalent. A plugin registers cleanup with `Defer` before disposing itself, so cleanup runs before the UNLOADING status. The owner accepted this on 2026-11-19 (ADR-035; decision 14).
4. **A failing commit fails closed.** `internal/commit` is a waterfall. An error aborts the update: the new config is not retained and the fiber is not restarted (decision 15).

Found by the restart harness, not by the port: River's fetch on a forced-RLS table chose a sequential scan once the table was large. The fix is the jobs-role and retention decision (§4.6 and ADR-045).

## 6. Open items at S0 exit

### (a) Pending runs

None. Every run that the earlier versions of this section listed is recorded: the call-path, scale and cold-mount gates on the final port (§4.3 to §4.5), the property workload on the final port (row 2), the restart gate with kernel-owned resources and the worker cases (row 6), `go test -race -count=3 ./...` per module (row 2), fuzzing (row 13) and per-file coverage (row 12). The criteria §5 transaction thresholds no longer apply to the raw-SQL surface (row 5).

### (b) Proposals to the owner

- **Line budget.** Decided: the owner fixed the cap at 8,200 lines on 2026-11-20 (row 4).
- **ADR-036** ([`ADR-036-tenancy-cost.md`](../adr/ADR-036-tenancy-cost.md)). The owner had to decide the fiber total (1,297 against "about 1,200"), the tenant count (500 against 300) and the numeric leak budget. Decided on 2026-11-22, and ADR-036 is accepted (S0 reading): the 500-tenant benchmark governs, and the run at 300 trees stays as a comparison; the phrase "at most about 1,200" is replaced by the ceiling of 97 shared fibers plus at most 4 fibers per loaded tenant, 97 + 4 × N (2,097 at 500 tenants); the numeric leak budget per replica stays open until S4b, which sets it with the pod cost model and multi-replica measurements. The `kernel` retained heap at 500 trees of width 4 is 29.2 MiB against the 32 MiB bound (2.8 MiB headroom).
- **ADR-058** (change impact reasoning). Draft, proposed 2026-11-19, awaiting the owner. Not required for S0 exit.
- **Scale protocol noise rule.** No amendment is needed: the final-port run is within the noise limit.
- **`kernel` lease cost under contention.** At 8 and 32 callers the `kernel` Enter and Leave cost about 2.5 times the prototypes' at p50, inside the 2 µs bound (§4.3). Changing the lease counter's shard choice is a kernel change; the owner decides whether and when. Decided on 2026-11-22: kept as is, with no kernel change now, and reviewed at the S3 measurements. [`ADR-034`](../adr/ADR-034-kernel-concurrency.md) records it as a tracking point.

### (c) Obligations to later steps

- **S1 loader.** Payloads of `internal/plugin`, `internal/status` and `internal/service` are declared but not wired (ADR-034, decision 13). The `reflect`, `registry` and `hooks` surfaces stay valueless until a consumer exists.
- **S3 typed operations.** The Go shape of the operation catalog; how a plugin declares it; review and versioning of catalog changes; the fake and conformance suite; migration admission (ADR-045, "What this does not settle").
- **S4b.** The pod cost model and the numeric leak budget per replica (ADR-036).

### (d) Repository housekeeping for the owner

- Purge of pre-rewrite commits. Requires the hosting provider's support.
- Old CI runs. Owner to review and remove.
- The denylist secret. Owner to decide its handling. The hygiene check reads the denylist from an environment variable and does not store its terms in the repository. The value is not recorded here.

## 7. Stop point

S0 is closed: the owner approved this report on 2026-11-22. This report, ADR-034, ADR-035, ADR-036 and ADR-045 form the S0 exit record. Every run is recorded, and the owner's answers to the proposals of section 6(b) are recorded there; ADR-058 still awaits the owner and is not required for S0 exit. S1 started in parallel on the owner's word of 2026-11-20 (ADR-059), and its branch is merged into main in `fb0a86b`.
