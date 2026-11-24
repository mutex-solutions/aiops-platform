# Embedded suite and scenario binaries

The release build writes here, before it builds `aiopsctl`, one test binary per suite
that a pack can feed, `<definition>.test`, and one per scenario, `scenario-<name>.test`,
each compiled with the first-party plugins it runs on (ADR-060 decision 46). `aiopsctl
plugin check` runs each as a child process with the environment `AIOPS_PACK` (the pack
directory), `AIOPS_FIXTURES` (its fixtures) and `AIOPS_REPORT` (the file of its JSON
report, a `SuiteResult` or a `ScenarioReport` of `sdk/def/launch/v0`). The repository
holds no binary: an `aiopsctl` built from it answers that it does not carry them.
