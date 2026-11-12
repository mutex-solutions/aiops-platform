# ADR-057: Repository namespace and private development checkpoint

- Status: accepted by the project owner, 2026-11-12.
- Scope: repository/module identity and private development publication only.
- Supersedes: the initial module namespace of ADR-056, not its other decisions.

## Context

The owner created `https://github.com/mutex-solutions/aiops-platform` and requested
that the Go module name match it. The owner authorized commits and pushes to
`main` through the existing SSH authentication, explicitly selected the reviewed
partial S0 checkpoint, and confirmed that the repository remains private during
development. It may be made public later by the owner, not by this change.

## Decision

1. Use `github.com/mutex-solutions/aiops-platform` for the root Go module and as
   the prefix of the planned nested SDK modules. Update current first-party Go
   imports and current architecture guidance. This changes namespace only; the
   three-module SDK boundary and standard-library-only kernel rule do not change.
2. Set `origin` to `git@github.com:mutex-solutions/aiops-platform.git` and push the
   reviewed commits to `main` without force. Use the existing SSH agent/key
   through Git; do not read, export, upload or replace private key material.
   Verify the remote branch before and after pushing; do not overwrite divergent
   remote work.
3. Preserve frozen criteria, pins, corpus manifests and measured receipts exactly,
   including the historical namespace they recorded. Those bytes describe the
   experiments already run. A changed current Go import path invalidates the
   corresponding current-source execution identity until tests are rerun; it does
   not authorize rewriting old results.
4. Publish only the reviewed development checkpoint. Clearly retain failed SQL
   gates, partial comparison status and not-run acceptance gates. The uncompiled,
   unexecuted capability draft and its unvalidated protocol remain local, outside
   the checkpoint. Do not claim that the checkpoint completes S0.
5. Do not add CI/CD configuration, create workflows, change repository visibility,
   or make a public release. Private development pushes do not close L14 or any
   lawyer, contribution or edition question. Before any later public disclosure,
   record the required rights-holder authorization under the existing gate.

## Consequences

- New Go consumers use the organization namespace. Current first-party source
  cannot import the old prefix accidentally; historical evidence may still name
  it intentionally. The accepted ADR-056 text is annotated as superseded for
  namespace only, rather than erasing its historical decision.
- New commits remain Conventional Commits, with no tool-generated attribution.
  The existing documentation-only first commit stays separate from S0 code.
- Run vet and race tests against the renamed current packages. A focused pass
  does not replace the failed SQL boundary gate, unexecuted capability experiment,
  complete differential/property/scale acceptance or final kernel work.
- Keep credentials, local working notes, unpublished research and unreviewed
  drafts outside Git history. Private visibility is not permission to upload
  ignored material or weaken notice obligations.
