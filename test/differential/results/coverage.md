<!-- SPDX-License-Identifier: Apache-2.0 -->
# Corpus coverage and assertion boundaries

Evidence identity: [freeze manifest](corpus-freeze.json). These are oracle
observations, not candidate or final-port acceptance. The final corpus has 36
stable IDs: 35 ordinary passes and one explicitly bounded diagnostic (D26).
Every semantic status/service event is retained. There is no scenario-level
trace exclusion and no sorted/deleted event projection.

| Frozen criteria group | Concrete JSON scripts | Checks in raw observations |
|---|---|---|
| D01–D04 | D01, D02, D03, D04 | Extend inheritance, separate and joined realms, duplicate refusal without replacement |
| D05–D08 | D05, D06, D07, D08 | Sibling-provider declaration requirement, inherited committed injection, NeedWith/intercept precedence |
| D09–D12 | D09, D10, D11, D12 | Per-consumer availability; public Await settles PENDING; LOADING provision not strictly visible; late activation |
| D13–D17 | D13, D14, D15, D16, D17 | All six states; revision and provision UID epoch changes; swap, diamond restart and latest-target coalescing |
| D18–D22 | D18, D19, D20, D21, D22 | Sticky failure until Update; superseded error recovery; provider lifetime through consumer cleanup; child/late-effect ownership |
| D23–D26 | D23, D24, D25, D26 | Partial unwind, single-shot and inactive refusal; self-disposal request completion; bounded awaited-self cycle |
| D27–D30 | D27, D28, D29, D30 | Raw/effective config, candidate-only resolution, saved/noSave commits, retained/stale/rejected updates, explicit unchanged-target hook |
| D31–D36 | D31, D32, D33, D34, D35, D36 | Four Event modes; all-settled Parallel; Serial bail/error; zero/empty Bail; Waterfall veto/repeated-next error |

JSON checkpoint assertions are independent post-execution checks. Additional
Go assertions verify temporal facts such as exactly two Apply calls under
coalescing, no failure recovery on provider replacement, consumer cleanup before
provider cleanup, no post-bail listener, and single inner Waterfall execution.
The comparator compares complete raw traces, not only these asserted fields.

## Explicit non-equality/adaptation boundaries

No adaptation has been implemented or silently exempted by this directory.
The parent owns the accepted contract checklist and must attach its exact row
identifier to each separately adapted Go assertion before claiming parity:

- **D26 only, awaited completion assertion:** the oracle remains blocked at the
  200 ms bound. The raw diagnostic must remain evidence; adapted request/complete
  APIs cannot be reported as a raw-oracle completion match. All D26 recorded
  events and its diagnostic state remain available for comparison/diagnosis.
- **D06 retained-inherited lookup assertion:** the pinned getter can return an
  ancestor fiber's committed binding after a dynamic isolate view, before the
  parent realm boundary check. A stronger Go entry-identity/realm guarantee
  needs its own explicitly identified adaptation assertion. The independent
  same-realm inheritance and explicit private required-PENDING assertions are
  still carried; the entire D06 scenario is not exempted.
- **D30 unchanged target assertion:** the fixture installs a declared plugin
  hook that vetoes an equal effective target; this is not an implicit Cordis
  guarantee that all equal updates avoid restart. Go must execute that hook or
  identify a separately accepted change, not inject the expected result.
- **Disposal ordering:** raw Cordis outer unload uses concurrent Promise.all;
  strict sequential LIFO, deadlines, Enter/quiesce, tracked work and leases are
  separate added/adapted guarantees. Never sort cleanup events to fake equality.
- **Hook and key contracts:** owner/declaration/stages/audience, scoped-listener
  refusal for unscoped dispatch, typed key classes, provider-only scoped writes,
  entry bindings and child inheritance need separate tests. Event mode tests in
  D31–D36 do not establish those stronger contracts.

No candidate adapter has run in this directory. The million-operation property
suite, graph/tree/call-path performance, original Vitest suites, loader file or
native resolution, original Include assertions, production line/coverage budget
and final-port acceptance remain unproven. Fuzzing the JSON codecs cannot stand
in for lifecycle property or resource-leak tests.
