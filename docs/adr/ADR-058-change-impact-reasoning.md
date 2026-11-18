# ADR-058: Change impact by reasoning over the estate model

- Status: **draft, proposed 2026-11-19, awaiting the project owner.** Nothing in
  this ADR is in force. `docs/architecture/00-decisions.md` is unchanged and wins
  until the owner accepts this ADR and the specific diff it names in "Changes to
  the canonical record".
- Scope: phase P2, with the `estate` plugin. No S0 to S8 work depends on it.
- Design detail: [change impact by reasoning](../design/change-impact-reasoning-2026-11.md).

## Context

The accepted plan already has an estate model (00 §2.1 row 15, ADR-029), a
published vocabulary and rule table whose closure the `estate` plugin computes
with its own queries, an estate blast radius on the approval card with a basis
and coverage gaps (09 §3.3), and `tool-estate-lookup` for a run. It defers "a
rule engine that executes the published table" to a trigger (09 §2.6, §9.4), and
00 §1 lists "a rule evaluator of our own" and "a triple store or ontology server
at runtime" as out of scope.

The owner asked on 2026-11-19 for the Change pillar agent to be built around
reasoning over the estate model: a change plan is translated into deltas, a rule
table is run before and after, and the approver reads lost and gained facts with
a stated basis. That needs three things the accepted plan does not have:

1. A reasoner that runs the published rule table at runtime, for one change
   review at a time.
2. A contract for turning a typed plan step into a delta on the model, and for
   the model to follow a verified change.
3. A defined coupling between that computed impact and the agent harness.

The first of these touches the two out-of-scope items above, so it needs a
decision rather than an edit.

## Decision

1. **A reasoner behind a definition.** Add the service definition
   `estateReasoner` (v0): given a slice of asserted triples and a rule table, it
   returns asserted facts, derived facts and a derivation record, or a typed
   refusal (slice too large, deadline). It ships with a fake and a conformance
   suite whose fixtures are slices with their expected closure. The first-party
   provider `estate-reasoner-horn` is written new: an in-process forward chainer
   over Horn rules that mint no terms, run to fixpoint over a fresh in-memory
   store that is discarded when the call returns. The file we publish is the file
   we run; the closure queries of ADR-029 remain until the provider passes the
   same fixtures and are then removed.
2. **Still no store and no query language.** No triple store, graph database or
   ontology server runs in any profile. No tenant, operator or model is offered a
   query language or the reasoner itself. The estate stays rows in PostgreSQL.
3. **Rules are data with reserved heads.** The rule table and vocabulary are
   published files under `vocabulary/`. A closed list of derived predicates is
   reserved; the plugin refuses to become ACTIVE if a rule head is outside it or a
   rule name repeats, and no reader or free text may assert a reserved predicate.
4. **Deltas come from the executors.** Add the registry `deltaTranslators`. Each
   executor contributes the translator for its own typed operations and declares
   each operation exact, runtime or estimated. An operation with no translator is
   estimated. `change-hub` owns impact (combine deltas, call the reasoner, build
   the report) and follow (apply a verified delta, store its inverse, absorb
   outside changes).
5. **The basis ladder is part of the contract.** `estimated < runtime < computed
   < certified`; a report's basis is its weakest part; a coverage gap on a touched
   entity forces estimated; gaps elsewhere never weaken the basis.
6. **Certification is optional and never first-party-required.** Add the registry
   `derivationCheckers`. With no member the best basis is computed and nothing
   fails. An upstream checker may be wrapped as one contribution in an optional
   bundle, under the rules of the licence record §7; no shipped profile, default
   bundle, conformance suite or release gate needs it.
7. **Harness coupling.** The computed impact reaches a Change run only as fenced
   context built by code on `run/first-message` (group `pillar-change`). The
   Change answer is constrained by a schema and a consistency check on
   `run/answer`: the model's plain-words impact must not contradict the computed
   report. The reasoner, deltas and derivation records are not contributions to
   `tools`. The harness definition does not change, so any harness a pillar
   selects gets the same method.
8. **Advisory and tier-neutral.** Every estate read in this path is advisory: it
   never blocks a release and degrades to an estimate with its reason. The
   reasoned blast radius never raises a tier by itself; 00 §2.2 keeps the
   catalogue tier as the floor, and only dependents over trusted service-layer
   edges raise it.
9. **Approval binding.** The approval digest binds the basis, the intended
   entities and the lost sentences, and never the also-affected list, gained
   facts, timestamps or estate version. **This reverses an accepted decision.**
   00 §2.2 and 09 §3.3 (ADR-029) say the digest does not bind the blast radius,
   so that a reader run never invalidates a parked approval. Binding the lost
   sentences means the approver's vote covers the consequences they read; the
   price is that a later estate read can invalidate a vote when those
   consequences change. The owner may accept decisions 1 to 8 without this one.

## Changes to the canonical record

If accepted, and only then, these edits are made in one commit:

- 00 §1, out of scope: replace "a rule evaluator of our own, a triple store or
  ontology server at runtime" with "a general rule language or query language of
  our own, and a triple store, graph database or ontology server in any profile.
  A fixed-table Horn reasoner that runs the published estate rules over one
  in-memory slice per change review is in scope from P2 (ADR-058)".
- 00 §2.1 row 15 and §2.3: name `estateReasoner`, `deltaTranslators` and
  `derivationCheckers`; move the rule engine from its §7.3 trigger to P2.
- 00 §7.3: remove the rule-engine trigger; add the triggers for a derivation
  checker contribution and for firewall rules.
- 00 §2.2 and 09 §3.3: if decision 9 is accepted, state that the digest binds the
  basis, the intended entities and the lost sentences.
- 09 §2.6 and §9.4: align with the design note.
- 08: add ADR-058 to the ADR table and the rule-parity witness row keeps its
  optional status.

## Consequences

- The approver of a change reads sentences about consequences with a stated
  confidence, not only a list of dependents, and every number on the card is
  reproducible without a model call.
- A new first-party component to maintain: the reasoner and its conformance
  fixtures. It is small by construction (no term minting, a bounded slice), but
  its joins are unindexed; the slice cap and a deadline bound it.
- Every executor must classify its operations for impact. One that does not gets
  estimated impact, which is honest but weak; exact translators are the work that
  makes the feature useful.
- Version 1 does not model egress, deny rules that depend on priority order, or
  ports outside the watched set. These are reported as gaps, never guessed.
- Certified basis may be unavailable on some platforms. The design treats that as
  normal.
- The approval digest now changes when a lost sentence changes. A read of the
  estate between a vote and a release can therefore invalidate a parked approval;
  the held-binding rule of the design note §8 limits that to real changes in
  consequences.
- The Change pillar depends on `estate` being ACTIVE for reasoned impact. Without
  it the change hub reports estimated impact from the catalogue tier, as P1 does.

## Open questions for the owner

0. Decision 9: bind the lost sentences into the approval digest (this note's
   proposal), or keep the accepted rule that the blast radius is never bound?
1. Is P2 the right phase, or should the reasoner wait for the first exact
   translator to exist?
2. Should a derivation checker contribution be attempted at all before a
   customer or an auditor asks for machine-checkable impact?
3. Which exact operations come first: NetworkPolicy, Service and Ingress edits,
   or host firewall rules?
