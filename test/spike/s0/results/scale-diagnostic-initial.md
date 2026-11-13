# First 10,000-fiber diagnostic — failed; not performance acceptance

The parent executed exactly one optimized trial for each of five 10,000-fiber
shapes on both real candidates. The workload was explicitly diagnostic, with no
sample-floor waiver or final-parity claim. Managed job `bash-03ae8dbe3e114015`
completed with **exit 1**, 95.80s.

- Source closure: `34100923524b3ad5dde3f965ca7837ab9ea206b46664879f1792f097e3f930a6`.
- Executable: `0b297c86e4acbd9ac557d57847854f574dbe97230999b8405fa6411dbc44dc6b`.
- Original evidence: `/tmp/aiops-s0-scale-diagnostic-DUwkkwu2/` (complete command
  log, exact source archive, executable/build identity, raw Events/archives and
  per-trial results; no semantic event truncation).
- Original operation guard: 15 seconds, distinct from the unchanged frozen load,
  drain and disposal deadlines and the performance thresholds.

## Observed findings

Independent graphs on both candidates and the first reconciler chain had no
reported lifecycle/resource errors in this invocation. These are single-trial
functional observations, not p99, performance acceptance or core-selection data.

The first graph-lock chain root-restart/settle reached 15.005428865 seconds and
failed its operation guard. Cleanup then also failed its fresh guard, leaving
9,137 native `awaitDependants` frames, an actual unload worker and the observer
queue worker when the harness inspected stacks. The retained source fence
matched before/after. This is a real incomplete-drain counterexample, not a pass
or an excuse to extend any frozen threshold.

Source investigation identified teardown waiters using the global event change
broadcast: every unrelated state/debug/service event woke every retiring chain
provider, creating a mutex herd. The native fix replaces only dependency-drain
notifications with activation-local, UID-checked release notifications. Its unit
validation is separate; a fresh isolated large-chain run is still required.

## Contamination and correction of the experiment driver

The initial driver ran ten fresh roots in **one process**. After the failed chain
returned while native work was still alive, later trials could overlap that old
work. Their timings are consequently not clean performance observations. Later
graph-lock fanout/diamond function-name stack differences do not by themselves
prove that those later roots leaked; the prior chain remained an overlapping
source of candidate-owned frames. Do not assign those frames to the later root
or promote eventual cleanup into a passing original chain result.

Diagnostic v2 requires exactly one topology/backend per executable process. The
parent must retain all ten separate exits/results, including failures, without
retrying or omitting failed samples. Existing RunT workloads, native deadlines,
operation guards and full acceptance sample floors remain unchanged. No result
from this diagnostic satisfies the full 10-repetition/1,000-observation budgets.
