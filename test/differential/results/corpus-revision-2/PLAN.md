<!-- SPDX-License-Identifier: Apache-2.0 -->
# D13 fixture revision 2 — pre-execution record

This plan is written before any revised D13 replay. It implements the parent's
pre-execution record in `docs/design/s0-corpus-revision-2.md`. Scope is only the
D13 causal fixture repair, not a contract adaptation or acceptance relaxation.

Revision 1 remains immutable:

- Corpus freeze SHA-256:
  `3b40b812ed6df7b98780d80f46396eb2c4bcb685393a5ba0464a04623ae72596`.
- Original D13 script SHA-256:
  `f798d978e0e775c91aa8cf26da0aadf2058a154e41a292d05a3c890effd77e79`.
- Oracle interpreter SHA-256 (unchanged in this revision):
  `b0f4e4474e4ffba2b5c9316680fa21581c2032d699fb845d0a9c6c5d0d6488b1`.
- Frozen criteria SHA-256 (unchanged):
  `628bd6ec24e5983bfbe9fcdada096eaa84db3e95227f11e8fd5a7fa33a26f19c`.

The parent observed a Go integration timeout with revision 1: asynchronous Mount
could cancel the `unloading` activation before it entered Apply and returned its
cleanup. The subsequent wait for the cleanup's `unload` arrival then had no
causal predecessor. The original JS replay passes under its microtask scheduler;
that does not make its implicit cross-fiber scheduling an explicit portable
fixture barrier. The parent retains the failed Go record separately. No new Go
candidate execution or candidate-equality claim is made here.

Preserve exact original D13 bytes in `D13-v1.script.json`. Keep the canonical
revision-1 input and old freeze/tests untouched. Create the versioned input
`corpus/revision-2/D13.json` with exactly two action additions:

1. `unloading.apply` emits `mark("u-ready")`.
2. External steps `waitMark("u-ready")` before `dispose("u")`.

All original state expectations, IDs, action order and cleanup bodies remain.
A mark action runs synchronously; resolving its external waiter schedules later
continuation after the source Apply body returns. The body then returns its
cleanup to Cordis before external disposal proceeds. No fake cleanup, observer
sorting/deletion, candidate-only operation, timer sleep or inferred readiness is
added. Both oracle and candidates must select the same versioned script.

Replay only the revised case on exact isolated Node 24.22.1, retain its complete
new trace/receipt under this directory, and create a revision-2 overlay freeze
binding the v1 base, pre-execution plan, revised script and unchanged executable
inputs. Reuse the unchanged 35 v1 scenario entries without claiming they were
rerun. Repeat only this revised oracle case for deterministic validation. The
parent owns subsequent Go candidate replay. Any further fixture defect requires
its own evidence and pre-execution revision; it is not covered by this plan.
