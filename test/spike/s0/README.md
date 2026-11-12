# S0 experimental probes

This directory contains test-only experiments, not product providers or service
definitions. The [frozen criteria](../../../docs/design/s0-spike-criteria.md) and
[freeze receipt](../../../docs/design/s0-criteria-freeze.json) precede execution.
The documentation baseline is commit `2d8d6ec`.

- Concurrency candidates live below `concurrency/` and implement the same probe
  contract. They are not `sdk/kernel`; only measured ADR-034 evidence selects the
  final port.
- The database/job experiment lives under `plugins/jobs-river/internal/s0probe/`
  so River imports remain inside its provider boundary. It invokes unmodified
  migrations, caller-owned transactions and least-privilege fixture roles.
- The differential oracle and neutral script corpus live under
  `test/differential/`. Its copied reference code and notices are test-only.
- Credential-free results belong under `results/`; retain failing records, source
  and criteria hashes, command/exit status and reproducibility data. A skipped or
  incomplete workload is not acceptance.

No production launcher, Go loader, tenant cache or infrastructure definition is
implemented by these probes. Do not use another workload's database or mutate its
containers. Runtime credentials stay in private local environment/state and never
in this repository, command transcripts or result files.
