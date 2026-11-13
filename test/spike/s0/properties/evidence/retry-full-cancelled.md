# Second full property invocation — user-cancelled, not acceptance

Managed invocation `bash-47745027b512440a` used the same frozen source digest
`2a2dc5ce8fe8cd609cbc0d7ae7a0322c828b6455532260f44e5c1f1a9aee1471`,
1,000 seeds x 1,000 operations for each backend, -race and GOMAXPROCS=8. Only the
outer Go package watchdog was extended to 90 minutes after the first incomplete
30-minute attempt. The actual command output is retained in full-race-retry.log.

The job was terminated by **SIGTERM, cancelled by the user** before a completed
summary/end-source check was reported. It is incomplete, not a passing property
result. No partial backend progress is merged into acceptance, and no claim of
an observed model violation is made merely from cancellation.

The cancelled workload will not be restarted automatically. The full property
gate remains open; other local implementation/tests may proceed independently.
Any later full run requires an intentional new invocation against its own frozen
current-source identity, while preserving the original criteria and workload.
