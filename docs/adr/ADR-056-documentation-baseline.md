# ADR-056: Pre-implementation documentation baseline

- Status: accepted by the project owner, 2026-11-12.
- Scope: editorial preparation of the existing design baseline before its first local commit.
- Implementation scope: S0 only; no code or measured spike result is accepted by this ADR.

## Context

The project is ready to start S0. Its baseline must explain the product, its contracts,
and its publicly attributable inputs without publishing private research details,
local workstation paths, or unnecessary repository-history narrative. A wording
change must not weaken a technical decision or misrepresent the origin or licence
of an upstream component.

The owner approved this editorial scope before changes were made. The owner also
selected `github.com/hiento09/aiops-platform` as the Go module-path prefix. Selecting
a module path neither creates a remote repository nor authorizes publication.

## Decision

1. Present requirements and design rationales on their own technical merits. Remove
   private-source identifiers, inventories, implementation details, measurements,
   and references to unpublished source material from the baseline. Do not turn an
   omitted source into a claim of independent authorship or a new measurement.
2. Express extensibility in terms of supported plugin interfaces and distribution
   builds. Align existing drawing-set summaries with the accepted canonical record;
   this corrects stale descriptions rather than changing that record's decisions.
   Express external-process lifecycle in terms of launching, connecting, revoking
   and stopping. Preserve the actual runtime and data-ownership contracts.
   Technical identifiers such as `rebuild-required` and `replace-or-rebuild` do not
   change.
3. Keep public upstream evidence, exact version pins, copyright notices and licence
   obligations. The Go kernel remains a port of the Cordis model as published in
   the pinned packages. Its MIT attribution is not removed or replaced by a claim
   that it was written without reference to that source.
4. Keep the publication-rights gate, L14, **open**. No repository content or release
   artifact may be disclosed beyond the currently authorized audience until the
   project owner records written publication and contribution authorization from
   every applicable rights holder. This ADR does not expand that audience. Editorial review, module naming and a local commit do not satisfy
   that gate. Unpublished material remains excluded from Git, including history.
   Existing path-specific exclusions are retained in the repository-local
   `.git/info/exclude` rather than publishing those names in `.gitignore`; this
   changes their location, not their coverage or the open publication gate.
   Every existing excluded path is checked before staging. These controls are
   local to this working repository: a new working directory must establish and
   verify its own controls before importing unpublished material. General
   working-output exclusions remain in `.gitignore`.
5. The initial local commit contains only the reviewed documentation baseline,
   its existing diagram assets and helper scripts, and project licensing and
   ignore controls. New S0 criteria, experimental code, kernel code and measured
   evidence are separate changes. Nothing is pushed by this decision.
6. Use `github.com/hiento09/aiops-platform` for the root module prefix and nested
   SDK module paths. The three-module SDK boundary and the standard-library-only
   kernel requirement are unchanged. The prefix does not add a product dependency
   or select a hosting service at run time.

## Consequences

- The canonical record and the rest of the baseline receive coordinated editorial
  changes in the same local change as this ADR. Product scope, key classes,
  contracts, accepted adaptations, phase order, test obligations, and licence or
  legal questions remain unchanged.
- A scan of words or metadata alone is not proof of confidentiality. Review must
  include the surrounding claims and the exact staged path inventory. Ignored
  private working material is neither staged nor force-added.
- Diagram topology is unchanged. If a displayed label changes, the affected source
  is rendered, captured and visually checked before it is included.
- S0 still requires criteria written before measurement, the source-pinned oracle,
  measured ADR-034/035/036/045 evidence, and the kernel exit tests. This ADR supplies
  no performance result and settles none of those choices.

## Verification before the first local commit

- [x] Reviewed the nonignored text baseline for source-identifying narrative and
      accidental secrets; retained truthful public upstream attribution.
- [x] Confirmed that excluded working material is absent from the exact staged
      inventory; the repository has no earlier commits to contain it.
- [x] Checked local document and drawing-set links, technical identifiers and legal
      gates. The generated drawing-set page was captured and visually reviewed;
      diagram topology and the existing diagram images are unchanged.
- [x] Reviewed the staged documentation-only diff and the owner's authorization.
      The initial commit uses `docs: record accepted architecture plan`. It is
      local only; no publication or push is authorized.
