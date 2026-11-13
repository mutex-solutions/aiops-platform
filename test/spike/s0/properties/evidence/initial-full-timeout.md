# First full property invocation — incomplete, exit 1

Managed invocation `bash-bc6d53c103b046a8` ran the full requested workload under
`-race`, GOMAXPROCS=8, with frozen source digest
`2a2dc5ce8fe8cd609cbc0d7ae7a0322c828b6455532260f44e5c1f1a9aee1471`.
The workload was 1,000 seeds x 1,000 operations for each real candidate; no smaller
count was substituted.

The Go test process hit its 30-minute package alarm and exited **1**. It had
entered the graph-lock subtest 11 minutes earlier and was still computing a
stable-state/model assertion when terminated. No model counterexample or race
report appeared in the collected output before the alarm. That absence is not
a complete property pass; final teardown, the end-source fence and full summary
were not reached. The invocation remains **incomplete/failed by process guard**.

The 30-minute process alarm was an authoring command guard, not one of the frozen
load/drain/dispose deadlines or a performance acceptance threshold. A subsequent
invocation may increase only this package execution guard to allow the same full
workload to finish. It must preserve seed/operation counts, source identity,
-race, all assertions and the original criteria. No previous result is changed
into a pass, and no candidate is selected from this partial reading.
