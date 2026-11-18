# S0 causal comparison coverage

Status: evidence record for the owner-approved causal comparison
([ADR-035](../adr/ADR-035-cordis-tracking.md),
[amendment](s0-causal-comparison-amendment.md)). The comparator, corpus, criteria
and candidates are unchanged. This note does **not** establish S0 acceptance.

## What was done

Every scenario that fails the strict comparison on at least one candidate in the
[inventory after D29](../../test/spike/s0/results/corpus-inventory-after-d29/results/inventory.json)
(D05 and D26 excluded, they are explicit not-carried boundaries) was run through
the frozen `CompareCausal` on both candidates against a freshly executed pinned
oracle (Node 24.22.1). D13 used `corpus/revision-3/D13.json`.

- `TestCausalReplayIntegration` now also covers D14 on `graph-lock` (see below).
- `TestCausalDiagnostics` (new) records the 27 candidate/scenario pairs that the
  comparator does not accept, and fails if a verdict differs from the text in this
  note, so a later comparator change forces this note to be revisited. It never
  turns a non-match into a pass.
- Receipts: [`causal-coverage-extension`](../../test/spike/s0/results/causal-coverage-extension/invocation.json).

Repetition: D14 on `graph-lock` matched 100 of 100 runs under `-race` and 100 of
100 with `GOMAXPROCS=2` under `-race`. The D14 `reconciler` verdict was the same
100 of 100 runs. All 27 non-match verdicts were identical in 5 further runs.

## Per-scenario table

Strict status is the inventory class (R reconciler, G graph-lock). Strict status
is schedule dependent: the inventory records that D02 flips between match and
failed from run to run, and D12 on R was strict-match in the first run made here
although the inventory lists it as failed. The
causal columns are the frozen comparator's verdict. Categories: (a) real
candidate difference, (b) listed in the decision record as adapted or not carried
(`docs/design/plugin-system-2026-11.md` section 15 checklist rows), (c)
comparator or interpreter limitation.

| ID | Strict R / G | Causal R / G | Category and evidence |
|---|---|---|---|
| D01 | match / match | not run | strict match |
| D02 | failed / failed | match / match | covered earlier |
| D03 | failed / match | unsupported / unsupported | (c) `output "joined" has ambiguous owner`: two consumers write one output cell in different realms, owner not provable |
| D04 | failed / failed | unsupported / unsupported | (c) `non-unique provider for svc/root:svc`: a refused second provider of one address is not modelled. Also observed, not in the decision record: the refusal error text differs (oracle `service "svc" has been registered at <provider>`, R `duplicate provision ...`, G `provision ... is already owned`) |
| D05 | incomplete / incomplete | not run | (b) row 9, `ctx.get` not carried |
| D06 | failed / failed | unsupported / unsupported | (c) `body operation "child"` is not modelled. Also (b) row 8: inherited declarations are realm-local, so `retained-inherited` reads an undeclared key (one original assertion fails on both, already named in the inventory) |
| D07 | failed / match | unsupported / unsupported | (c) `non-ordinary provision syntax`: provide mode `service`/NeedWith config view |
| D08 | failed / match | unsupported / unsupported | (c) `intercept context "outer"`: ancestor intercept chain not modelled |
| D09 | failed / failed | unsupported / unsupported | (c) `non-ordinary provision syntax`: provide mode `available`. Also (a) observed, see below |
| D10 | match / match | match / match | covered earlier |
| D11 | failed / match | unsupported / unsupported | (c) `body operation "gate"`: gates are not modelled |
| D12 | failed / match | match / match | covered earlier |
| D13 (rev 3) | failed / failed | unsupported / unsupported | (c) `body operation "gate"`. Also fixture race on fiber `d`, see below |
| D14 | failed / match | mismatch / match | R: (a) `last enabling provider "p" published before consumer "c" LOADING`, 100 of 100 runs. G: match, 100 of 100 plus 100 of 100 at `GOMAXPROCS=2` |
| D15 | failed / match | unsupported / unsupported | (c) `non-unique provider for svc/root:svc`: provider swap is not modelled |
| D16 | failed / failed | match / match | covered earlier |
| D17 | match / match | not run | strict match |
| D18 | failed / match | unsupported / unsupported | (c) `body operation "effect"`: effects are not modelled |
| D19, D20 | match / match | not run | strict match |
| D21 | failed / match | unsupported / unsupported | (c) `body operation "mark"`: marks are not modelled |
| D22 | failed / failed | unsupported / unsupported | (c) `body operation "child"`. Also fixture race, see below |
| D23 | failed / failed | unsupported / unsupported | (c) `async/control/catch metadata on "effect"`; effect shapes are adapted in row 14 ([mapping](s0-d23-request-mapping.md)) |
| D24, D25 | match / match | not run | strict match |
| D26 | incomplete / incomplete | not run | (b) awaited self-dispose is an explicit not-carried boundary |
| D27 to D36 | match / match | not run | strict match |

"Unsupported" is the comparator's own `ErrCausalUnsupported`: it reports the
first operation it cannot prove independent, so it says nothing about whether the
candidate is equivalent. The reason is the first one hit; a scenario may contain
further unsupported operations.

## What the comparator is missing (category c)

Each missing capability is general; none was added, because the methodology is
frozen. Required to cover the scenarios above: gate/mark/release continuations
(D11, D13, D21, D22, D23); child fibers (D06, D22); effects with bodies and
async/catch metadata (D18, D23); ancestor intercepts and NeedWith/service/available
provision modes (D07, D08, D09); proof of the owner of an output cell shared across
realms (D03); more than one provider of one address, by refusal or swap (D04, D15).

## Candidate differences (category a)

1. **D14, `reconciler`.** In the candidate's own native ledger the provider goes
   ACTIVE, then its `service` publication is recorded, then dependent consumer `c`
   goes LOADING (native sequence 7, 8, 9). The oracle and `graph-lock` move `c` to
   LOADING before the publication. The code comment at the notification site says
   dependants refresh synchronously before the event, but the refresh runs
   after it here. The frozen comparator rejects exactly this order. Whether it is
   a defect or an acceptable observer order is the owner's call; the comparator
   does not accept it.
2. **D09, `graph-lock`** (20 of 20 runs, informal multiset comparison, evidence not
   retained): consumer `cb` goes PENDING, LOADING, UNLOADING, PENDING while the
   availability check for its context is still false. The oracle keeps it PENDING.
3. **D09, both candidates** (20 of 20): the second `service` event the oracle
   records after `notify` does not occur. Row 21 says `reflect.notify` is adapted
   to a key and realm index but does not say the event is dropped.
4. **D04, both candidates:** refusal error text, see the table. Not in the
   decision record.

## Fixture causality gaps (category c, not candidate bugs)

- **D13 revision 3** fixes the barrier for fiber `f` only. Fiber `d` is mounted and
  disposed with no wait on its Apply or ACTIVE. In 20 further fresh runs the
  candidate outcome varied (no Apply for `d`, or `d` reached ACTIVE first), and the
  event multiset differed from the oracle in 18 of 20 `graph-lock` and 19 of 20
  `reconciler` runs.
- **D22** disposes the parent after the child's effect gate but with no wait for
  the child's ACTIVE. The child was disposed while LOADING in 6 of 20 `graph-lock`
  and 7 of 20 `reconciler` runs, and ACTIVE first in the rest. This follows from
  row 22 (run to completion not carried).

## What this does and does not establish

- It establishes that, of the 14 targeted scenarios (28 candidate pairs), the
  frozen comparator accepts one pair repeatably (D14 on `graph-lock`), rejects one
  for a reproducible ordering difference (D14 on `reconciler`) and cannot judge the
  other 26 pairs.
- It does not establish equivalence for any unsupported scenario, nor that a
  strict failure is harmless. The informal multiset observations above are not the
  comparator and are not receipts.
- It is not S0 acceptance: no criterion is relaxed, no scenario is dropped, and
  D05 and D26 remain open.
