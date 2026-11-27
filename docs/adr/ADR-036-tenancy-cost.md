# ADR-036: Tenancy cost (S0 reading)

- Status: **accepted 2026-11-22 by the project owner (S0 reading); S4b cost
  model and leak budget open.** This record gives the S0 reading only. The S4b
  cost model (one pod per tenant and remote plugin) and the numeric leak budget
  per replica are out of scope and stay **open**.
- Date: 2026-11-20; read against the final-port runs on 2026-11-22; accepted
  2026-11-22.
- Scope: the S0 bench of the tenancy budget in the plugin record §10
  ([`plugin-system-2026-11.md`](../design/plugin-system-2026-11.md)), measured
  on the prototype cores and on the kernel port. Criteria: §8.2 and §9 of
  [`s0-spike-criteria.md`](../design/s0-spike-criteria.md).
- Evidence status: **final-port evidence.** The scale and cold-mount gates
  passed with no reasons on the reconciler, graph-lock and `kernel` backends
  (code of `8ec2d8c`). The prototype runs (`225ffbe`), which predate the
  2026-11-19 hazard fixes, are kept below as history.

## Context

Plugin record §10 (lines 379 to 395) puts each loaded tenant under its own
child context and mounts a tenant tree per loaded tenant. Tree mounts are
cached, not eager. Line 395 states the budget, "at 300 tenants, to be beaten
or the design changes (ADR-036)":

| Budget line (plugin record §10, line 395) | Quoted text |
|---|---|
| Fibers | "about 97 shared fibers plus 2 to 4 per loaded tenant (at most about 1,200)" |
| Boot | "no tenant tree at boot" |
| In-process mount | "a tree of in-process rows mounts in under 50 ms and holds zero goroutines while idle" |
| Remote mount | "a tree with remote rows mounts within the handshake deadline (2 s against a running instance, the definition's cold budget against one scaled to zero) and holds one stream per remote row" |
| Restart | "a root provider restart settles inside the drain deadline" |
| Pods | "one pod per (tenant, remote plugin)" |

Three points in the budget need owner attention:

1. **Tenant count.** The ADR table names a 500-tenant benchmark. The budget
   text in §10 is written for 300 tenants. The criteria (§8.2) run 500 loaded
   trees and an additional 300-tree run "to compare the original design
   budget".
2. **Fiber total.** 97 + 4 × 300 = 1,297, which is above "at most about
   1,200". The record does not say which figure governs. This ADR does not
   choose one.
3. **Numeric deadlines.** §10 names the drain and handshake deadlines without
   values. This reading uses the criteria values: load 2 s, drain 5 s,
   dispose 2 s (criteria §4). The 2 s handshake figure is from §10 itself.

The owner decided points 1 and 2 on 2026-11-22 (Decision, items 1 and 2).

## Measurements

### Host and commits

| Run | Evidence | Commit | Host class | Notes |
|---|---|---|---|---|
| Scale gate, final port, 1,000 roots and 500 or 300 trees | [`scale-full-final-port/invocation.json`](../../test/spike/s0/results/scale-full-final-port/invocation.json), `scale-identity.json`, `scale.log` | `583e1bd` (code of `8ec2d8c`; the later commits add only documents and result files) | Dedicated KVM guest, 24 vCPU Intel Xeon Platinum 8481C, 245 GiB RAM, no swap, `GOMAXPROCS=8` | 16,200 runs, 32,434 s. Reconciler, graph-lock and `kernel` backends. Classification pass, `reasons` empty. |
| Cold-mount gate, final port | [`coldmount-full-final-port/invocation.json`](../../test/spike/s0/results/coldmount-full-final-port/invocation.json), `report.json`, `cold-identity.json`, `cold.log` | `8ec2d8c` | Same host as the final-port scale run | 180 runs, 27,060 s. Same three backends. Classification `pass`, `reasons` empty. |
| Scale gate, prototype (history) | [`scale-full-first/invocation.json`](../../test/spike/s0/results/scale-full-first/invocation.json) | `225ffbe` | Dedicated KVM guest, 24 vCPU, `GOMAXPROCS=8` | 10,800 runs, 22,372 s. Prototype cores only. |
| Cold-mount gate, prototype (history) | [`coldmount-full-first/report.json`](../../test/spike/s0/results/coldmount-full-first/report.json), `cold-identity.json`, `cold.log` | `225ffbe` | Same host as the prototype scale run | 120 runs, 18,039 s. No `invocation.json` in this directory. |
| Restart gate, leak accounting | [`restart-full-section10/invocation.json`](../../test/spike/s0/results/restart-full-section10/invocation.json) | `e34047e` | Dedicated KVM guest, 24 vCPU | Library-level probe, not the kernel. |

The host class is a dedicated guest. Its address and name are not recorded
here. Pod and remote costs were not measured in S0.

The cold-mount identity still reads "candidate evidence, never final-port
evidence" in its `mode` field. The invocation records that this text is fixed
in the harness and that the baseline commit and `finalParityCompleted` make the
run the final-port run.

Scale record digests, final port: `timerOverhead` p50 / p95 / p99 / max is
53 / 55 / 56 / 869 ns over 1,000 samples. `reportSHA256`
`d028672ee3345ea1add451fe443ae27c51bd3f343ee04b249706d36a51100b7c` names the
full report (8.8 MB), which stays on the measurement host with the per-run
records. `logSHA256`
`2d70abd40f74f29129dc0177bcfd39c809af63a851ba782b919728eacca6b5b7` matches the
committed `scale.log`. Prototype (history): 53 / 55 / 64 / 471 ns; report
`943737cf8118fbd7d6eba363fc846630c654313c67a4a7707866ff1d8e1b6e35`; log
`c1d5c6ce891d63b50785f5d2ec69cd01200a08f2b4224b0a3d1018d2496c3ac0`.

### Scale gate: 500 and 300 synthetic trees

Trees are 4 fibers (width 4) or 2 fibers (width 2), with 97 shared fixture
fibers (criteria §8.2). Latencies are in milliseconds, from
`summaries[]` in the scale invocation.

Final port (`8ec2d8c`). Each cell is p50 / p95 / p99 / max:

| Metric | Reconciler, 500 × 4 | Graph lock, 500 × 4 | `kernel`, 500 × 4 | Bound |
|---|---|---|---|---|
| Cold tree mount, settle (per tree) | 0.214 / 0.396 / 0.600 / 9.83 | 0.201 / 0.339 / 0.541 / 7.47 | 0.295 / 0.510 / 0.773 / 13.79 | under 50 ms per tree (max reported) |
| Hot tree lookup | 3.50 / 4.37 / 5.38 / 8.91 | 3.49 / 4.39 / 4.88 / 7.19 | 5.02 / 6.05 / 6.68 / 9.45 | not stated in criteria §8.2 |
| Shared provider restart, settle | 89.9 / 98.0 / 151.4 / 174.3 | 102.3 / 109.6 / 113.2 / 199.5 | 90.3 / 96.9 / 100.7 / 183.4 | 5,000 |
| Reverse-order tree dispose, settle | 30.0 / 34.1 / 54.3 / 61.9 | 36.7 / 43.3 / 47.1 / 75.0 | 29.4 / 33.4 / 37.5 / 59.9 | not stated |

| Metric | Reconciler, 300 × 4 | Graph lock, 300 × 4 | `kernel`, 300 × 4 |
|---|---|---|---|
| Cold tree mount, settle | 0.213 / 0.383 / 0.572 / 7.99 | 0.200 / 0.342 / 0.522 / 7.46 | 0.298 / 0.532 / 0.757 / 10.50 |
| Hot tree lookup | 2.12 / 2.67 / 4.09 / 4.87 | 2.13 / 2.76 / 4.05 / 4.80 | 3.04 / 3.82 / 5.81 / 6.45 |
| Shared provider restart, settle | 53.4 / 59.2 / 106.3 / 117.8 | 62.7 / 68.4 / 118.9 / 124.3 | 53.6 / 60.1 / 106.3 / 112.1 |
| Reverse-order tree dispose, settle | 17.0 / 19.4 / 33.6 / 36.3 | 21.5 / 26.8 / 44.8 / 48.2 | 16.8 / 19.6 / 32.9 / 35.0 |

On `kernel` the hot lookup is slower than on the prototype cores (p50
5.02 ms against 3.50 and 3.49 ms at 500 × 4). Criteria §8.2 states no bound
for it.

Prototype (`225ffbe`), kept as history. Each cell is p50 / p99 / max:

| Metric | Reconciler, 500 × 4 | Graph lock, 500 × 4 | Bound |
|---|---|---|---|
| Cold tree mount, settle (per tree) | 0.200 / 0.593 / 9.69 | 0.193 / 0.541 / 9.05 | under 50 ms per tree (max reported) |
| Hot tree lookup | 3.56 / 5.94 / 9.48 | 3.54 / 5.39 / 8.03 | not stated in criteria §8.2 |
| Shared provider restart, settle | 85.3 / 161.8 / 172.4 | 108.4 / 167.1 / 198.7 | 5,000 |
| Reverse-order tree dispose, settle | 29.4 / 60.0 / 66.5 | 43.3 / 77.8 / 94.3 | not stated |

| Metric | Reconciler, 300 × 4 | Graph lock, 300 × 4 |
|---|---|---|
| Cold tree mount, settle | 0.210 / 0.681 / 12.22 | 0.203 / 0.655 / 8.90 |
| Hot tree lookup | 2.20 / 4.49 / 4.97 | 2.17 / 4.41 / 5.27 |
| Shared provider restart, settle | 52.8 / 108.6 / 113.6 | 67.3 / 129.7 / 132.7 |
| Reverse-order tree dispose, settle | 17.1 / 35.3 / 37.0 | 26.5 / 54.5 / 67.7 |

Observation counts, in both runs: 500,000 for each 500-tree cold mount cell,
300,000 for each 300-tree cold mount cell, 1,000 for the other cells. The
prototype record did not explain the cold mount count. The final record's
`plan` accounts for it: repetitions (10) × tree trials (10) × tree cycles (10)
is 1,000, and the cold mount cells hold 1,000 observations per tree while the
other cells hold 1,000 in all.

Retained heap above the shared-only baseline. Each value is the maximum
`maxDeltaBytes` (1 MiB = 1,048,576 bytes).

Final port:

| Tree shape | Reconciler | Graph lock | `kernel` | Limit |
|---|---|---|---|---|
| 500 × 4 | 22,584,512 B = 21.54 MiB | 21,505,680 B = 20.51 MiB | 30,648,024 B = 29.23 MiB | 32 MiB (criteria §8.2) |
| 500 × 2 | 17,328,824 B = 16.53 MiB | 16,301,888 B = 15.55 MiB | 22,065,424 B = 21.04 MiB | 32 MiB |
| 300 × 4 | 13,818,856 B = 13.18 MiB | 13,119,344 B = 12.51 MiB | 19,133,520 B = 18.25 MiB | 32 MiB |
| 300 × 2 | 10,637,480 B = 10.14 MiB | 10,051,560 B = 9.59 MiB | 13,695,280 B = 13.06 MiB | 32 MiB |

Prototype, kept as history:

| Tree shape | Reconciler | Graph lock | Limit |
|---|---|---|---|
| 500 × 4 | 21,333,216 B = 20.34 MiB | 20,280,416 B = 19.34 MiB | 32 MiB (criteria §8.2) |
| 500 × 2 | 16,098,144 B = 15.35 MiB | 15,051,920 B = 14.35 MiB | 32 MiB |
| 300 × 4 | 13,095,952 B = 12.49 MiB | 12,430,552 B = 11.85 MiB | 32 MiB |
| 300 × 2 | 9,925,408 B = 9.47 MiB | 9,513,696 B = 9.07 MiB | 32 MiB |

The `kernel` backend holds more heap than the prototype cores in every tree
shape. At 500 × 4 it uses 91.3% of the limit.

Other tree fields of the final record for `kernel` (bytes, rounded to whole
bytes):

| Tree shape | `maxPerFiberBytes` | `maxSlopeBytesPerCheckpoint` | `maxGoroutinesAtLastCheckpoint` |
|---|---|---|---|
| 500 × 4 | 15,324 | 1,160,576 | 4 |
| 500 × 2 | 22,065 | 1,175,284 | 4 |
| 300 × 4 | 15,945 | 743,613 | 4 |
| 300 × 2 | 22,825 | 735,517 | 4 |

The heap record lists `"roots": 100` for every tree workload and
`"roots": 1000` for every 1,000-root workload, on all three backends. The
final record's `plan` explains both. `roots` is the number of runs summarised
per backend and workload, one retained-heap entry per run: repetitions (10) ×
tree trials (10) = 100 for trees, and repetitions (10) × graph trials (100) =
1,000 for the root workloads. Each run is a fresh root; a tree run mounts the
97 shared fibers and then every tree of its workload. The tree counts are
recorded in `plan.treeCounts` (500 and 300). The same plan gives 5,400 runs
per backend (5 root workloads × 1,000 and 4 tree workloads × 100), which
matches the 16,200 runs on three backends. In both runs `maxPerFiberBytes`
equals `maxDeltaBytes` divided by trees × width (2,000 fibers for 500 × 4), so
the per-fiber figure counts tree fibers and leaves out the 97 shared fibers.
Prototype (history): the record has no `plan` and does not explain the
`roots` value by itself. The harness code that sets `roots` (`summarizeHeap`
in `test/spike/s0/scale/measure.go`) is the same at `225ffbe` and `8ec2d8c`,
and the 10,800 prototype runs match the same plan on two backends.

Goroutines at the last checkpoint, final port: 4 (reconciler), 3 (graph lock)
and 4 (`kernel`). Each backend gives the same count in every workload of the
record, tree and 1,000-root alike, as the prototype did (4 and 3). The count
does not change between 300 and 500 trees or between widths 2 and 4, so in
the range measured it does not grow with trees. The classification also
checks every retained checkpoint of every tree run against the shared-only
cut, taken after the shared fibers mount and before any tree mounts: a
candidate goroutine owner above its count at that cut is a failure ("idle
goroutines must be zero per instance"). The final-port classification has no
reasons, so on all three backends no candidate owner held more goroutines
with 300 or 500 trees mounted than with none. The prototype reasons are noise
findings only, so the same check raised nothing there. The record still gives
no count at zero trees, and it does not enumerate the fixed root workers.

### Cold-mount gate: ack path

Arm A mounts a cold tree on every acknowledged request. Arm B0 is the pinned
baseline. Arm B1 is B0 with background mount churn beside it.

Final port. Values are milliseconds, reconciler / graph lock / `kernel`, from
`latencyAllAttempts` in the final-port `report.json`; each cell holds 120,000
requests over ten repetitions:

| Body and arm | p50 | p99 | max |
|---|---|---|---|
| 100 items, A (cold on ack path) | 1.39 / 1.32 / 1.62 | 2.05 / 1.96 / 2.34 | 9.2 / 8.4 / 14.0 |
| 100 items, B0 (pinned baseline) | 0.81 / 0.81 / 0.86 | 1.35 / 1.34 / 1.40 | 4.1 / 5.0 / 3.3 |
| 100 items, B1 (pinned beside churn) | 0.82 / 0.84 / 0.87 | 1.36 / 1.37 / 1.42 | 8.4 / 7.2 / 11.7 |
| 1 MiB, A | 7.10 / 7.07 / 8.04 | 9.32 / 9.25 / 10.48 | 29.4 / 30.8 / 36.4 |
| 1 MiB, B0 | 5.34 / 5.33 / 6.17 | 6.99 / 6.99 / 7.82 | 22.3 / 23.0 / 23.7 |
| 1 MiB, B1 | 5.34 / 5.33 / 6.20 | 7.04 / 7.03 / 7.90 | 27.0 / 23.4 / 26.2 |

Prototype, kept as history. Values are milliseconds, reconciler / graph lock,
from ADR-034 (2026-11-19 cold-mount section). The per-run values in the
prototype `report.json` fall in the same range.

| Body and arm | p50 | p99 | max |
|---|---|---|---|
| 100 items, A (cold on ack path) | 1.39 / 1.34 | 2.03 / 1.95 | 5.8 / 10.2 |
| 100 items, B0 (pinned baseline) | 0.81 / 0.80 | 1.35 / 1.34 | 3.7 / 2.7 |
| 100 items, B1 (pinned beside churn) | 0.83 / 0.83 | 1.38 / 1.39 | 4.2 / 6.1 |
| 1 MiB, A | 7.28 / 7.35 | 10.0 / 9.9 | 26.7 / 24.2 |
| 1 MiB, B0 | 5.58 / 5.58 | 7.18 / 7.17 | 18.0 / 17.5 |
| 1 MiB, B1 | 5.57 / 5.55 | 7.23 / 7.22 | 22.4 / 22.5 |

Derived values, final port:

- Cold mount added to a small request (arm A minus B0 at p99): 0.70 ms
  (reconciler), 0.62 ms (graph lock) and 0.93 ms (`kernel`,
  2.339 − 1.404).
- Cold mount added to a 1 MiB request: 2.33 ms, 2.26 ms and 2.66 ms
  (`kernel`, 10.476 − 7.819).
- Mount alone (`mountLatency`, arms A and B1): at most 1.24 ms at p99 and
  12.5 ms at worst, both on `kernel` (the p99 in the B1 churn mounts at
  100 items, the worst in arm A at 1 MiB). Reconciler: 1.05 ms and 9.2 ms.
  Graph lock: 0.95 ms and 11.8 ms.
- Churn beside pinned traffic (B1 minus B0 at p99, median and maximum of ten
  matched pairs): 0.014 and 0.061 ms (reconciler), 0.019 and 0.068 ms (graph
  lock), 0.018 and 0.093 ms (`kernel`) at 100 items; 0.057 and 0.098 ms,
  0.045 and 0.090 ms, 0.099 and 0.235 ms at 1 MiB. The worst pair, 0.235 ms
  on `kernel` at 1 MiB, is 4.76 ms under the 5 ms bound.

Derived values, prototype (history):

- Cold mount added to a small request: 0.68 ms at p99 (2.03 − 1.35) and
  0.61 ms (1.95 − 1.34), reconciler and graph lock.
- Cold mount added to a 1 MiB request: 2.82 ms at p99 and 2.73 ms.
- Mount alone: at most 1.0 ms at p99 and 9.5 ms at worst.
- Churn beside warm traffic: p99 moves 0.03 to 0.06 ms in the median matched
  pair and 0.16 ms at worst (ADR-034).

Timeout fixture, final port: recorded per backend in `coldTimeoutFixtures` of
the final-port `report.json`, on all three backends, `kernel` included. The
synchronous path answers 503 with a one-second retry hint and commits
nothing. The job snoozes and commits nothing, and the ledger records no
commit afterwards. No foreign scope contributes, while a tree of another scope
was mounted warm. Every one of the 120,000 scheduled requests in each of the
18 cells was answered `ok`. Arm B makes no cold-ensure call: `coldEnsureCalls`
is 0 in every B0 and B1 cell and 150,000 in every arm A cell (100 requests per
second over ten repetitions of 30 s warm-up and 120 s measurement).

Timeout fixture, prototype (history), on both backends: the synchronous path
answers 503 with a one-second retry hint and commits nothing. The job snoozes
and commits nothing. No foreign scope contributes. Every request in the run
succeeded.

Classification, final port: `pass` with no reasons, from the new
`invocation.json`; `report.json` and `cold.log` agree. The final-port binary
includes the `coldEnsureCalls` fix of `c5ce385`, which precedes `8ec2d8c`.

Harness defect (prototype run): the per-cell field `coldEnsureCalls` reported
a shared counter for arm A in the binary used. It was fixed later in
`c5ce385`. Totals and latencies are not affected (ADR-034). The prototype
classification is "pass, no reasons" as recorded in ADR-034. This ADR does not
re-derive it, because `coldmount-full-first` has no `invocation.json`.

### Restart gate with leak accounting

Recorded in [`restart-full-section10/invocation.json`](../../test/spike/s0/results/restart-full-section10/invocation.json):
100 of 100 cycles on the pool (30), client (30), listener (30) and combined
(10) arms. Every acknowledged ID completed. The section 10 accounting shows
no finding in any arm: teardown returns to the harness baseline, no
file-descriptor growth, disposed generations own nothing, and no open
transaction or stuck statement.

The rerun with the pool, the job clients and the listener owned by fibers of
the final kernel
([`restart-full-kernel-owned/invocation.json`](../../test/spike/s0/results/restart-full-kernel-owned/invocation.json),
`d683ebe`) also passed 100 of 100 cycles, with 0 findings in each of the six
section 10 bounds in every arm.

## Honest limits

- **Prototype cores, then the final port.** The prototype runs (`225ffbe`)
  are kept as history. Both performance gates have now run on the final port
  (`kernel` backend, code of `8ec2d8c`) beside the two prototype cores, and
  both passed with no reasons. The `kernel` figures are measured through the
  S0 adapter, which keeps a handle and a config cell for every fiber the
  kernel still holds (S0 report §4.4). They are not figures for the kernel
  alone. The `kernel` retained heap for 500 trees of width 4 leaves 2.77 MiB
  under the 32 MiB limit, the smallest margin in this reading.
- **Invalid and stopped runs.** The prototype scale run was invalid by its
  noise rule only: fifteen buckets had throughput variation above 10% (10.0%
  to 15.1%), and no latency, heap or goroutine bound failed. The rule was not
  relaxed. The final-port rerun used the unchanged rule and records no reason:
  no bucket above the limit and every bound met on the three backends, so no
  amendment proposal goes to the owner (ADR-034 required one only if the rerun
  was invalid for the same reason). An earlier final-port scale run on
  `dcc5f10` was stopped after 2 h 12 min because its adapter was superseded;
  it produced no classification. The first final-port cold-mount run, also on
  `dcc5f10`, failed on the `kernel` backend because of the S0 adapter, which
  was then fixed (S0 report §4.5).
- **Cold-mount invocation record.** The final-port run has an
  `invocation.json`, and its classification is taken from it. The prototype
  classification still comes from ADR-034.
- **Leak budget per replica: what the evidence shows.**
  - The restart gate's section 10 accounting shows zero residual resources
    after teardown in one process, for pool, client, listener and combined
    restarts, with the resources library-owned (`e34047e`) and owned by fibers
    of the final kernel (`d683ebe`). It does not show multi-replica
    behaviour, remote disposal, or a numeric budget.
  - The retained-heap slope fields (`maxSlopeBytesPerCheckpoint`) are
    recorded. Final port: 9,784 bytes per checkpoint for independent 1,000
    roots and 16,336,736 bytes for chain 1,000 roots on `kernel`; 10,760 and
    18,912,744 bytes on the reconciler; 1,160,576 bytes for 500 trees of
    width 4 on `kernel`. Prototype (history): 9,624 and 19,000,448 bytes
    (reconciler). The harness defines them: a graph run takes two retained
    checkpoints (after the mount and after the root restart), a tree run one
    per tree cycle (10 per run, each after every tree is mounted again), and
    the slope is the least-squares fit over one run's checkpoints, the record
    keeping the maximum over runs (`retainedMetrics` and `runTrees` in
    `test/spike/s0/scale`). The tree slopes are about 1 MiB per cycle at 500
    trees and 0.6 to 0.7 MiB at 300 trees on all three backends alike
    (`kernel` 1,160,576 bytes at 500 × 4), so they do not single out the port;
    ten cycles in one process are too few to tell growth that levels off from
    a leak. These fields do not establish a leak rate. The heap
    maxima show retained heap stayed under its limit at the checkpoints
    recorded. They do not show zero growth.
  - No numeric leak budget appears in the plugin record. The failure table
    in 08 §1.6 (line 484 of
    [`08-profiles-roadmap-adrs.md`](../architecture/08-profiles-roadmap-adrs.md))
    says the replica is "recycled only above the leak budget" and gives no
    value. The budget value is **not recorded**. On 2026-11-22 the owner kept
    it open until S4b (Decision, item 3).
- **Remote and pod costs were not measured.** The criteria state that S0
  provides no remote runtime and no tenant cache (§8.2). The remote handshake,
  the stream per remote row and the pod cost are S4b items.

## Decision

The project owner accepted the S0 reading on 2026-11-22, with the three
decisions under "Owner decisions" below.

The S0 reading of each budget line, with the margin where a number exists.
Pass/fail is on the final-port evidence above. Where a row gives one figure,
it is the worst case over the reconciler, graph-lock and `kernel` backends.
The prototype reading is kept in its own column as history.

| Budget line | S0 reading, final port | Margin | Prototype reading (history) | Reading status |
|---|---|---|---|---|
| Shared fibers and 2 to 4 fibers per tenant | Fixture shapes are 2 and 4 fibers per tree, with 97 shared fibers | not a measured quantity | the same | pass as a fixture shape |
| Total fibers at 300 tenants, "at most about 1,200" | Not decidable as written: 4 fibers per tree gives 1,297 | −97 at width 4 | the same | **decided** 2026-11-22: the ceiling is 97 + 4 × N, 2,097 at 500 tenants (item 1) |
| No tree at boot | No boot measurement in the final-port records | n/a | none in the files read | **not recorded** |
| In-process mount under 50 ms | Max 9.83 ms (reconciler), 7.47 ms (graph lock), 13.79 ms (`kernel`) at 500 × 4; 7.99, 7.46 and 10.50 ms at 300 × 4 | 36.2 ms at worst (`kernel`, 500 × 4) | Max 9.69 ms (500 × 4, reconciler), 9.05 ms (graph lock); 12.22 ms (300 × 4, reconciler); margin 37.8 ms | pass |
| Zero idle goroutines per tree | 4, 3 and 4 goroutines total at the last checkpoint, the same at 300 and 500 trees and at widths 2 and 4; no candidate owner above its shared-only (zero-tree) count at any tree checkpoint | not a per-tree count | 4 (reconciler) and 3 (graph lock) at the last checkpoint, 500 trees | **partial**: no growth from 300 to 500 trees and none above the shared-only cut; the zero-tree count is still not recorded |
| Remote mount within handshake (2 s) and one stream per row | Not measured in S0 | n/a | the same | deferred to S4b |
| Root restart settles inside the drain deadline (5 s) | Max 174.3 ms (reconciler), 199.5 ms (graph lock), 183.4 ms (`kernel`) at 500 × 4 | 4.80 s at worst | Max 172.4 ms (reconciler), 198.7 ms (graph lock); margin 4.80 s | pass |
| Retained heap, 500 trees (criteria) | Max 21.54 MiB (reconciler), 20.51 MiB (graph lock), 29.23 MiB (`kernel`) | 2.77 MiB (`kernel`) | Max 20.34 MiB (reconciler), 19.34 MiB (graph lock); margin 11.66 MiB | pass |
| Cold mount under 50 ms (criteria) | Max 13.79 ms (`kernel`, 500 × 4) | 36.2 ms | Max 9.69 ms; margin 40.3 ms | pass |
| Ack path with cold mount (criteria §9) | Worst B1 p99, both on `kernel`: 1.42 ms against 250 ms at 100 items; 7.90 ms against 1 s at 1 MiB. Worst matched-pair churn delta 0.235 ms against 5 ms | 248.6 ms; 992.1 ms; 4.76 ms | 100-item B1 p99 1.38 ms; 1 MiB B1 p99 7.23 ms; margins 248.6 ms and 992.8 ms | pass |
| Pod per (tenant, remote plugin) | Not measured in S0 | n/a | the same | deferred to S4b |
| Leak budget per replica | No numeric value in the record | n/a | the same | **open until S4b** (owner, 2026-11-22; item 3) |

Note of 2026-11-27, with the owner's answers on ADR-041 (its decisions 40 and
52). The budget line "Remote mount" (line 30) is quoted as the plugin record
had it on 2026-11-20, and the line "Remote mount within handshake (2 s) and one
stream per row" of the table above restates it. Both read as follows. The 2 s
bounds the handshake, up to the verdict of the gate; the mount of a tree with
remote rows within it is the budget that S4b measures, not a bound that the
protocol enforces. "One stream per remote row" is one connection with one
session, so one open stream, while the row is idle, and each call in flight is
one more stream of the transport on that connection. The line stays deferred to
S4b.

**Reading, accepted 2026-11-22.** The S0 bench passes every measured budget
line on the final port, on all three backends, `kernel` included. The smallest
margin is the `kernel` retained heap for 500 trees of width 4 (2.77 MiB under
32 MiB). One line is partial (zero idle goroutines) and one is not recorded (no
tree at boot). Four items are deferred: the remote mount, the pod cost, the
numeric leak budget, and the S4b cost model.

**Owner decisions, 2026-11-22.** Each item with the evidence that bears on it
and what the owner decided:

1. **Fiber total.** The budget says "at most about 1,200"; at 300 tenants the
   budget's own shapes give 1,297 at width 4 and 697 at width 2. The bench
   ran up to 500 trees of width 4, 2,097 fibers with the shared ones, and met
   every bound on all three backends. The measurement does not choose between
   the figures; the owner chooses the figure the budget states.
   **Decided:** the phrase "at most about 1,200" is replaced by a formula: 97
   shared fibers plus at most 4 fibers per loaded tenant, 97 + 4 × N for N
   loaded tenants, which is 2,097 at 500 tenants.
2. **Tenant count, 500 or 300.** Both counts meet every bound on all three
   backends. On `kernel`, 500 × 4 against 300 × 4: retained heap 29.23 MiB
   against 18.25 MiB (margins 2.77 and 13.75 MiB), cold mount max 13.79 ms
   against 10.50 ms, restart max 183.4 ms against 112.1 ms.
   **Decided:** 500 tenants. The 500-tenant benchmark governs; the run at 300
   trees stays as a comparison. The `kernel` retained heap at 500 trees of
   width 4 is 29.2 MiB against the 32 MiB bound, 2.8 MiB of headroom.
3. **Numeric leak budget per replica.** No value exists in the plugin record
   or in 08 §1.6. The restart gate shows zero residual resources after
   teardown in one process, library-owned and kernel-owned. The slope fields
   grow by about 1 MiB per tree cycle on all three backends alike over ten
   cycles and do not establish a leak rate.
   No multi-replica or remote evidence exists. The owner sets the value or
   keeps it open for S4b.
   **Decided:** kept open until S4b. No number is set now; S4b sets it with
   the pod cost model and multi-replica measurements.

**Deferred to S4b.** The pod cost model; the remote handshake and stream costs;
the cold budget of scaled-to-zero definitions; the numeric leak budget per
replica; the tenant directory and cache costs.

## To update after the final-port rerun

The reruns are recorded: scale at `583e1bd` on the code of `8ec2d8c`, cold
mount at `8ec2d8c`. Steps 1 to 7 are applied above, with the prototype values
kept beside them as history. Step 8 is applied: the owner decided on
2026-11-22, and the status line and the ADR-036 row of 08 say so.

1. Scale record: `commit`, `host`, `elapsedSeconds`, `classification` and
   `reasons` in the new `scale-full-*/invocation.json`.
2. Scale record: `summaries[]` entries with `workload` `trees-500-width-4` and
   `trees-300-width-4`, fields `p50NS`, `p95NS`, `p99NS`, `maxNS`, for the
   `kernel` backend (the port's registered backend name).
3. Scale record: `retainedHeap[]` entries for the tree workloads, fields
   `maxDeltaBytes`, `maxPerFiberBytes`, `maxSlopeBytesPerCheckpoint`,
   `maxGoroutinesAtLastCheckpoint`; and the `roots` value that the record
   uses for trees.
4. Scale record: `timerOverhead`, `reportSHA256`, `logSHA256`.
5. Cold-mount record: the 100-item and 1 MiB rows for arms A, B0 and B1; the
   matched-pair B1 minus B0 delta; the timeout fixture result; the
   `classification`, which must come from a new `invocation.json`.
6. The "Derived values" list and the decision table margins, recomputed from
   items 2 and 5.
7. The "Honest limits" bullets on prototype cores and on invalid runs.
8. The status line, only after the owner decides. Then update the ADR-036 row
   of [`08-profiles-roadmap-adrs.md`](../architecture/08-profiles-roadmap-adrs.md).
