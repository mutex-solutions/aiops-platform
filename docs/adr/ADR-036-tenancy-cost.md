# ADR-036: Tenancy cost (S0 reading)

- Status: **proposed, awaiting the project owner.** This record gives the S0
  reading only. The S4b cost model (one pod per tenant and remote plugin) and
  the numeric leak budget per replica are out of scope and stay **open**.
- Date: 2026-11-20.
- Scope: the S0 bench of the tenancy budget in the plugin record §10
  ([`plugin-system-2026-11.md`](../design/plugin-system-2026-11.md)), measured
  on the prototype cores and on the kernel port. Criteria: §8.2 and §9 of
  [`s0-spike-criteria.md`](../design/s0-spike-criteria.md).
- Evidence status: **candidate evidence.** The scale and cold-mount runs
  predate the 2026-11-19 hazard fixes. The final-port reruns are pending.

## Context

Plugin record §10 (lines 377 to 393) puts each loaded tenant under its own
child context and mounts a tenant tree per loaded tenant. Tree mounts are
cached, not eager. Line 393 states the budget, "at 300 tenants, to be beaten
or the design changes (ADR-036)":

| Budget line (plugin record §10, line 393) | Quoted text |
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

## Measurements

### Host and commits

| Run | Evidence | Commit | Host class | Notes |
|---|---|---|---|---|
| Scale gate, 1,000 roots and 500 or 300 trees | [`scale-full-first/invocation.json`](../../test/spike/s0/results/scale-full-first/invocation.json) | `225ffbe` | Dedicated KVM guest, 24 vCPU, `GOMAXPROCS=8` | 10,800 runs, 22,372 s. Prototype cores only. |
| Cold-mount gate | [`coldmount-full-first/report.json`](../../test/spike/s0/results/coldmount-full-first/report.json), `cold-identity.json`, `cold.log` | `225ffbe` | Same host as the scale run | 120 runs, 18,039 s. No `invocation.json` in this directory. |
| Restart gate, leak accounting | [`restart-full-section10/invocation.json`](../../test/spike/s0/results/restart-full-section10/invocation.json) | `e34047e` | Dedicated KVM guest, 24 vCPU | Library-level probe, not the kernel. |

The host class is a dedicated guest. Its address and name are not recorded
here. Pod and remote costs were not measured in S0.

### Scale gate: 500 and 300 synthetic trees

Trees are 4 fibers (width 4) or 2 fibers (width 2), with 97 shared fixture
fibers (criteria §8.2). Latencies are in milliseconds, from
`summaries[]` in the scale invocation. Each cell is p50 / p99 / max.

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

Observation counts: 500,000 for each cold mount cell, 1,000 for the other
cells. The count of 500,000 is recorded as is; the record does not explain it.

Retained heap above the shared-only baseline. Each value is the maximum
`maxDeltaBytes` (1 MiB = 1,048,576 bytes):

| Tree shape | Reconciler | Graph lock | Limit |
|---|---|---|---|
| 500 × 4 | 21,333,216 B = 20.34 MiB | 20,280,416 B = 19.34 MiB | 32 MiB (criteria §8.2) |
| 500 × 2 | 16,098,144 B = 15.35 MiB | 15,051,920 B = 14.35 MiB | 32 MiB |
| 300 × 4 | 13,095,952 B = 12.49 MiB | 12,430,552 B = 11.85 MiB | 32 MiB |
| 300 × 2 | 9,925,408 B = 9.47 MiB | 9,513,696 B = 9.07 MiB | 32 MiB |

The heap record lists `"roots": 100` for the tree workloads. The record does
not say how a tree maps to a root. This is **not recorded** as a separate
count of trees, and it is a point to resolve before the final rerun.

Goroutines at the last checkpoint: 4 (reconciler) and 3 (graph lock) for the
tree workloads. The record gives one total per workload. It does not give a
count at zero trees, and it does not enumerate the fixed root workers.

### Cold-mount gate: ack path

Arm A mounts a cold tree on every acknowledged request. Arm B0 is the pinned
baseline. Arm B1 is B0 with background mount churn beside it. Values are
milliseconds, reconciler / graph lock, from ADR-034 (2026-11-19 cold-mount
section). The per-run values in `report.json` fall in the same range.

| Body and arm | p50 | p99 | max |
|---|---|---|---|
| 100 items, A (cold on ack path) | 1.39 / 1.34 | 2.03 / 1.95 | 5.8 / 10.2 |
| 100 items, B0 (pinned baseline) | 0.81 / 0.80 | 1.35 / 1.34 | 3.7 / 2.7 |
| 100 items, B1 (pinned beside churn) | 0.83 / 0.83 | 1.38 / 1.39 | 4.2 / 6.1 |
| 1 MiB, A | 7.28 / 7.35 | 10.0 / 9.9 | 26.7 / 24.2 |
| 1 MiB, B0 | 5.58 / 5.58 | 7.18 / 7.17 | 18.0 / 17.5 |
| 1 MiB, B1 | 5.57 / 5.55 | 7.23 / 7.22 | 22.4 / 22.5 |

Derived values:

- Cold mount added to a small request: 0.68 ms at p99 (2.03 − 1.35) and
  0.61 ms (1.95 − 1.34), reconciler and graph lock.
- Cold mount added to a 1 MiB request: 2.82 ms at p99 and 2.73 ms.
- Mount alone: at most 1.0 ms at p99 and 9.5 ms at worst.
- Churn beside warm traffic: p99 moves 0.03 to 0.06 ms in the median matched
  pair and 0.16 ms at worst (ADR-034).

Timeout fixture, on both backends: the synchronous path answers 503 with a
one-second retry hint and commits nothing. The job snoozes and commits
nothing. No foreign scope contributes. Every request in the run succeeded.

Harness defect: the per-cell field `coldEnsureCalls` reported a shared
counter for arm A in the binary used. It was fixed later in `c5ce385`. Totals
and latencies are not affected (ADR-034). The classification is "pass, no
reasons" as recorded in ADR-034. This ADR does not re-derive it, because
`coldmount-full-first` has no `invocation.json`.

### Restart gate with leak accounting

Recorded in [`restart-full-section10/invocation.json`](../../test/spike/s0/results/restart-full-section10/invocation.json):
100 of 100 cycles on the pool (30), client (30), listener (30) and combined
(10) arms. Every acknowledged ID completed. The section 10 accounting shows
no finding in any arm: teardown returns to the harness baseline, no
file-descriptor growth, disposed generations own nothing, and no open
transaction or stuck statement.

## Honest limits

- **Prototype cores, before the port.** Both performance gates ran on the
  prototype cores (`225ffbe`). They are candidate evidence. The port
  (`kernel` backend, `e34047e`) has not been through either gate.
- **Scale gate is invalid by its noise rule only.** Fifteen buckets have
  throughput variation above 10% (10.0% to 15.1%). No latency, heap or
  goroutine bound failed on either backend. The rule was not relaxed. The
  gate is being rerun unchanged on the final port. If it is invalid again for
  the same reason, the owner receives a written amendment proposal first
  (ADR-034).
- **Cold-mount gate has no invocation record.** The classification comes from
  ADR-034. The final-port rerun is pending.
- **Leak budget per replica: what the evidence shows.**
  - The restart gate's section 10 accounting shows zero residual resources
    after teardown in one process, for pool, client, listener and combined
    restarts. It does not show multi-replica behaviour, remote disposal, or
    a numeric budget.
  - The retained-heap slope fields (`maxSlopeBytesPerCheckpoint`) are
    recorded: 9,624 bytes per checkpoint for independent 1,000 roots and
    19,000,448 bytes for chain 1,000 roots (reconciler). The scale record
    does not define the checkpoint spacing or the fit window. These fields
    do not establish a leak rate. The heap maxima show retained heap stayed
    under its limit at the checkpoints recorded. They do not show zero
    growth.
  - No numeric leak budget appears in the plugin record. Section 13 (line
    492) says the replica is "recycled only above the leak budget" and gives
    no value. The budget value is **not recorded**.
- **Remote and pod costs were not measured.** The criteria state that S0
  provides no remote runtime and no tenant cache (§8.2). The remote handshake,
  the stream per remote row and the pod cost are S4b items.

## Decision proposed

The S0 reading of each budget line, with the margin where a number exists.
Pass/fail is on the prototype evidence above.

| Budget line | S0 reading | Margin | Reading status |
|---|---|---|---|
| Shared fibers and 2 to 4 fibers per tenant | Fixture shapes are 2 and 4 fibers per tree, with 97 shared fibers | not a measured quantity | pass as a fixture shape |
| Total fibers at 300 tenants, "at most about 1,200" | Not decidable as written: 4 fibers per tree gives 1,297 | −97 at width 4 | **owner decision on the figure** |
| No tree at boot | No boot measurement in the files read | n/a | **not recorded** |
| In-process mount under 50 ms | Max 9.69 ms (500 × 4, reconciler), 9.05 ms (graph lock); 12.22 ms (300 × 4, reconciler) | 37.8 ms at worst | pass |
| Zero idle goroutines per tree | 4 goroutines total at the last checkpoint, 500 trees | not a per-tree count | **partial**: no zero-tree count |
| Remote mount within handshake (2 s) and one stream per row | Not measured in S0 | n/a | deferred to S4b |
| Root restart settles inside the drain deadline (5 s) | Max 172.4 ms (500 × 4, reconciler), 198.7 ms (graph lock) | 4.80 s at worst | pass |
| Retained heap, 500 trees (criteria) | Max 20.34 MiB (reconciler), 19.34 MiB (graph lock) | 11.66 MiB | pass |
| Cold mount under 50 ms (criteria) | Max 9.69 ms | 40.3 ms | pass |
| Ack path with cold mount (criteria §9) | 100-item B1 p99 1.38 ms against 250 ms; 1 MiB B1 p99 7.23 ms against 1 s | 248.6 ms; 992.8 ms | pass (prototype) |
| Pod per (tenant, remote plugin) | Not measured in S0 | n/a | deferred to S4b |
| Leak budget per replica | No numeric value in the record | n/a | **open** (S4b) |

**Proposed reading.** The S0 bench passes every measured budget line on the
prototype cores. Two lines need the owner's decision (the fiber total and the
tenant count). One line is partial (zero idle goroutines). Four items are
deferred: the remote mount, the pod cost, the numeric leak budget, and the
S4b cost model. Acceptance waits for the final-port reruns.

**Deferred to S4b.** The pod cost model; the remote handshake and stream costs;
the cold budget of scaled-to-zero definitions; the numeric leak budget per
replica; the tenant directory and cache costs.

## To update after the final-port rerun

The reruns are pending. When they run, replace these fields and keep the
prototype values in the ADR history:

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
