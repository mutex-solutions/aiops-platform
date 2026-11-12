<!-- SPDX-License-Identifier: Apache-2.0 -->
# D13 revision 2 result

The [pre-execution plan](PLAN.md) and [timestamped pre-execution
record](preexecution.json) preceded all revised replay. The [v2 overlay
freeze](../corpus-freeze-v2.json) selects the new D13 input and explicitly inherits
the other 35 unchanged v1 cases. It does not overwrite or relabel the original
[freeze](../corpus-freeze.json), scripts, traces, receipts, or validation results.

The versioned input is [D13 revision 2](../../corpus/revision-2/D13.json), SHA-256
`b285c1deb72ccccefe14184cadfe8269684580e70c4c6ba8a0ecf39f3bd161c2`.
[Original D13 bytes](D13-v1.script.json) remain SHA-256
`f798d978e0e775c91aa8cf26da0aadf2058a154e41a292d05a3c890effd77e79`.
Only two fixture actions were added: `mark u-ready` in the unloading plugin Apply,
and external `waitMark u-ready` immediately before disposing that fiber. All
six original state expectations remain. Interpreter/schema/vendor/runtime/criteria
bytes are unchanged; no semantic event is filtered, reordered, or normalized away.

Three oracle replays exited 0, met all six state assertions and retained the same
complete ordered trace, SHA-256
`d85d6db4262d3e3c1ff126850e8a2131e3c0fda059f4726e30bcb2aa9ba709de`:

- [Replay 1](D13.replay-1.trace.json), [receipt 1](D13.replay-1.trace.json.run.json).
- [Replay 2](D13.replay-2.trace.json), [receipt 2](D13.replay-2.trace.json.run.json).
- [Replay 3](D13.replay-3.trace.json), [receipt 3](D13.replay-3.trace.json.run.json).

Native attempts were zero. The additional [revision tests](../../revision2_test.go)
verify exact v1 bytes/freeze, exact two-action delta, unchanged assertions/source,
provenance hashes, u-ready before disposal, and ordered equality of all replays.
`go test ./test/differential`, `go vet ./test/differential`, and explicit live
`go test -race -count=3 ./test/differential -run TestD13Revision2` all exited 0.
The last command ran three additional live revised D13 cases; it did not rerun
the other 35 scenarios. It does **not** claim Go candidate equality.

## Receipt limitation and actual reproduction

The frozen interpreter's receipt `command` array is a **symbolic template**, not
an exact actual invocation: it always prints `corpus/D13.json`, even when given a
versioned path. This known metadata limitation is not corrected by editing old
receipts or silently changing interpreter bytes. Script SHA-256 fields correctly
bind the versioned input. The v2 freeze supplies the actual command and exact
selected script path/hash. Do not treat the embedded command template alone as
complete reproducibility metadata.

From repository root, with environmental `S0_NODE` naming the verified isolated
Node 24.22.1 binary:

```sh
env -u NODE_OPTIONS -u NODE_PATH -u CORDIS_SHARED "$S0_NODE" \
  --import ./test/differential/oracle/register.mjs \
  ./test/differential/oracle/replay.mjs \
  ./test/differential/corpus/revision-2/D13.json
```

A fresh last argument records another trace/receipt exclusively; never overwrite
these files. The parent retains the original Go candidate timeout evidence and
owns subsequent candidate replay. Revision 2 is a causal fixture repair, not a
threshold change, accepted-contract adaptation, or S0 completion claim.
