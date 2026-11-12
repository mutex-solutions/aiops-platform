<!-- SPDX-License-Identifier: Apache-2.0 -->
# Test-only S0 differential replay

This directory owns no production kernel, SDK, or candidate. The neutral corpus
contains **D01–D36**, with 35 normal scenarios and D26's bounded negative diagnostic.
It runs unchanged against the unmodified pinned Cordis built runtime. This is
**oracle baseline evidence, not candidate equality or S0 acceptance**.

## Coverage and observations

| Scripts | Executed behavior |
|---|---|
| D01–D04 | Extend metadata/service inheritance; distinct realms; joined equal labels; duplicate provision refusal |
| D05–D08 | Required declaration with sibling provider; inherited committed injection; NeedWith config; ancestor-first Intercept |
| D09–D12 | Consumer-bound availability; public Await settling PENDING; ACTIVE-only visibility; late provider |
| D13–D17 | Six states behind causal barriers; revision/provision epochs; provider swap; diamond invalidation; coalesced updates |
| D18–D22 | Sticky failure; explicit Update recovery; superseded activation failure; isolated provider retained through consumer teardown; child/late-effect cleanup joining |
| D23–D26 | Partial-effect unwind and single-shot disposal; inactive effect refusal; own-disposal request; awaited own-disposal diagnostic |
| D27–D30 | Raw/effective config, config/update/commit hooks, stale/rejected retained updates, explicit unchanged-target veto and changed-target delegation |
| D31–D36 | Emit including symbol keys; all-settled Parallel; Serial bail/error; zero/empty Bail; Waterfall veto/wrapping and repeated-next error |

[Protocol](PROTOCOL.md), [Go types](protocol.go), [parsers](parse.go),
[comparator](compare.go), and [runner](runner.go) are candidate-independent.
Scripts are JSON action data; expected values are checked after execution,
**never** used as handwired plugin responses. Each JSON file is standalone.
Authoring helpers exclusively create absent files; replay consumes JSON only.

### Self-disposal distinction

- [D25 script](corpus/D25.json), [final trace](results/corpus-final/D25.trace.json),
  [receipt](results/corpus-final/D25.trace.json.run.json): requesting own disposal
  without awaiting it lets Apply return, completes cleanup, and settles DISPOSED.
- [D26 script](corpus/D26.json), [final trace](results/corpus-final/D26.trace.json),
  [receipt](results/corpus-final/D26.trace.json.run.json): awaiting own disposal in
  Apply remains LOADING at the **200 ms diagnostic bound**, with null UID and
  inactive epoch. `SELF_DISPOSE_AWAIT_BLOCKED` records observed non-progress, not
  supported-runtime completion. The parent reports owner approval for separating
  request from completion; this oracle preserves the raw pinned behavior instead
  of faking equality for the adapted Go request/completion API.

Normal scenarios have a 5-second watchdog, not timing-based assertions. Script
ordering uses explicit gates, mark arrivals and lifecycle barriers, not sleeps.
Trace arrays retain every semantic event and restart boundary in arrival order.
States, raw/effective config, revision and provision epoch relationships are raw
snapshots. Receipts include exact source/script/runtime/resolver/vendor hashes,
criteria digest, baseline, UTC interval, command, outcome and exit code. These
are semantic replays, **not the full host/resource record for performance gates**.

### Preserved initial failures and provenance revisions

The first D25 interpreter invocation was invalid: mounting tried to resolve its
to-be-created fiber as an existing context, reported `unknown context self`, and
exited 1 before an oracle observation. That interpreter bug was fixed before
recorded results. The [initial validation](results/validation.json) preserves it.

The initial expanded corpus had two **failed fixture assertions**, not hidden
compatibility passes. Both original scripts are preserved under
[initial D05](results/expanded-initial/D05.script.json) and
[initial D06](results/expanded-initial/D06.script.json); [control traces and
receipts](results/controls-v1/) replay them with result `fail`, exit 1:

- D05 originally provisioned at root. A root-owned own provision is inherited by
  descendants without an explicit Need. Corrected D05 uses a sibling plugin
  provider to actually test declaration refusal. It does not invent a stronger
  root-provision contract.
- D06 originally expected dynamic-isolate child access to reject its ancestor's
  committed injection. Pinned lookup checks the inherited fiber's committed
  binding before its parent realm boundary. Corrected D06 **records that retained
  lookup** and independently shows an explicitly required lookup in the private
  realm stays PENDING. Stronger entry-identity/realm restrictions belong to
  separately identified adapted tests, not an erased oracle assertion.

Historical receipts are not overwritten. [Initial interpreter](oracle/history/replay-initial.mjs)
and [first expanded interpreter](oracle/history/replay-expanded-initial.mjs)
retain their original hashes. Historical files are source archives, not alternate
entry points. Results in the earlier directories are tied to their respective
hashes and **do not certify the current interpreter**. The current authoritative
baseline is [final corpus results](results/corpus-final/). D10 gained an explicit
public Await action after [v1 input preservation](results/corpus-v1/D10.script.json);
all 36 final cases were rerun, rather than carrying forward affected receipts.

## Reproduction and tests

Use `S0_NODE` naming independently verified **Node 24.22.1**, never host Node 22.
Node's official archive checksum and complete runtime license are in the
[public reference pins](../../docs/design/s0-reference-pins.md). The parent
retains the isolated runtime and complete license externally; no binary is
redistributed here. No npm/npx, package scripts, installs or builds are needed.

From repository root:

```sh
# S0_NODE must already name the verified isolated executable.
env -u NODE_OPTIONS -u NODE_PATH -u CORDIS_SHARED "$S0_NODE" \
  --import ./test/differential/oracle/register.mjs \
  ./test/differential/oracle/replay.mjs ./test/differential/corpus/D10.json

# Optional last output path must not exist. It also writes OUTPUT.run.json.
# Assertion failures retain the raw trace and a fail/exit-1 receipt.
go test ./test/differential
go vet ./test/differential
S0_ORACLE_RUN=1 S0_NODE="$S0_NODE" go test -race -count=3 ./test/differential
```

Ordinary Go tests validate all recorded scripts, assertions, provenance hashes,
UID invariants, causal event assertions, and parser/comparator invalid inputs.
Live integration explicitly skips unless `S0_ORACLE_RUN=1`; that skip is never
counted as an oracle pass. The explicit integration test runs **all 36 cases**
and compares every semantic event to the final recorded baseline. The final
explicit race run passed three repetitions of all 36 cases (108 live replays).
Separate 60-second script/trace parser fuzz campaigns also passed; exact commands
and execution counts are in [final validation](results/validation-final.json).
These codec fuzz runs are not lifecycle property tests. Error strings
and classes stay exact; only declared opaque UID renaming and the built-in
service-key rename are allowed. User fields called `events`, `timestamp` or
`provisionUID` remain untouched. Need key order must come from the original JSON,
not Go map iteration or sorting; see the protocol's order requirements.

## Byte preservation and licenses

[Manifest](oracle/manifest.json) records **103 unchanged package files plus the
complete DeepSeek MIT notice = 104 copied files**, exact byte lengths/SHA-256,
archive identity/hash, and source URLs for all 26 TypeScript files matching source
pin `fcc0ad462172522aaf5c93a7b2e01f6b9f0b0159`. [Acquisition](oracle/acquire.py)
compared every approved installed package file with the exact npm archive and
all TypeScript sources with the pinned public commit before exclusive-copying.
No install, script, source transformation, or package execution occurs there.
Published manifests retain their bytes, including scripts as inert metadata.
Source/workspace manifests differ from published manifests; there is no signed,
reproducible-build or unique historical-publish-commit claim. Standard Schema's
published package has declarations and built entries but no source directory.

The [resolver](oracle/register.mjs) verifies every manifest entry before replay,
maps five exact built entry points, rejects other bare imports, `.node`, native
helpers and `internal/*`, and confines files to the oracle directory. It never
patches runtime bytes or creates package symlinks. The full corpus executes only
Cordis/Cosmokit; loader, Schemastery and Standard Schema are preserved and licensed
but **not runtime-tested by this corpus**. Native attempts are zero. See
[NOTICE](NOTICE) and complete upstream grants/disclaimers beside each package.
All new source has `SPDX-License-Identifier: Apache-2.0`; no oracle ships in a
product artifact. No third-party Go imports or root module changes are needed.

The identified pinned harness regression tests informed coalescing, superseded
load errors, retention, config publication, effect/child cleanup and Waterfall
fixtures. Attribution URLs are embedded in the corresponding scripts. This is
re-expression of kernel-relevant operations, **not** original Vitest execution.
Loader file/native/Include/dispatcher assertions remain outside scope.

## Remaining acceptance work

Candidate adapters and exact cross-candidate comparison are owned by the parent
and have not run here. Adapted strict sequential LIFO, deadlines, leases, tracked
work, entry bindings, hook key classes/stages/audience and scoped ownership must
be proven independently, with per-assertion checklist attribution, not by deleting
whole oracle scenarios. The 1,000×1,000 property corpus, performance probes,
original upstream suites and final production-port acceptance remain unperformed
here. Parser fuzz campaigns are separate validation, not lifecycle-property or
port-coverage acceptance. There is **no full S0 pass claim**.
