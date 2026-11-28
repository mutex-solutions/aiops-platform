# ADR-047: Vendor-neutral connectors, S2a part (the artifact, the install and the home)

- Status: **accepted 2026-11-23 by the project owner, for the S2a part.** The owner
  answered all seven questions on the same date with the recommended option of each; the
  section "Owner answers of 2026-11-23" records them as decisions 16 to 22. The rest of
  ADR-047 stays for S8a start, as the scope below says.
- S2b part: **proposed; answers recorded 2026-11-27 and 2026-11-28, the runs of
  decision 39 recorded 2026-11-27, all ten rows holding, and the runs after the answers
  recorded 2026-11-28; it waits for the owner on questions Q19 to Q23.** The section
  "S2b part" at the
  end of this file holds it, with decisions 23 to 41 and questions Q8 to Q14 (ADR-063
  decisions 2 and 34). The owner answered the seven questions on 2026-11-27 with the
  recommended option of each, Q11 on its condition; the section "Owner answers of
  2026-11-27" records them as decisions 42 to 48. By decision 48 the part stayed
  proposed until each row of decision 39 had a recorded result; the section "Runs of
  decision 39, recorded 2026-11-27" records them, with the four questions that the runs
  raised, Q15 to Q18. The owner answered those four on 2026-11-28 with the recommended
  option of each; the section "Owner answers of 2026-11-28" records them as decisions
  49 to 52, and its decision 53 adds to the endpoint table what the owner's answer of
  the same date on the S2b part of ADR-061 puts there. That record does not mark the
  part accepted. It puts one question to the owner, Q19, which is needed before
  `sidecars-static` merges. The section "Runs after the answers of 2026-11-28" records
  the runs that those answers left: the check of the systemd unit holds, and the bound
  of decision 51 is 5 s. The same runs raise four questions, Q20 to Q23, on the label
  of a bind under SELinux, on the group of an instance under rootless Podman and on a
  unit on Podman. The part waits for the owner on Q19 to Q23. Until the owner accepts
  the part, nothing above changes but the two dated notes that the answers to Q8 and
  Q9 add, at decisions 1 and 14.
- Scope: the S2a part of ADR-047 only, "the pack artifact form and the insert-only rule of
  gate check 8" (`00:781`; `08:1178`; community:438), with every point ADR-060 decision 33
  lists for it.
  - Two parts of the written artifact rule are built at S3 on what this ADR records: the
    secret scope and the purge key of the rows under an artifact's group (`00:478`;
    `08:391`; `07:1817`; community:298), with `secret:` in the artifact's own scope
    (ADR-060 decision 57) and `purge` with `durable` (ADR-060 decisions 29 and 47). Both
    read the origin-artifact record of decision 5.
  - The third-party naming rules apply at S2a as written: the name lint of `plugin check`
    step 1 checks the forms of `00:657` and T7 (`08:1363`; ADR-060 decision 23), and
    decision 12 writes them.
  - The rest of ADR-047 stays for S8a start (`08:1178`; `07:72` R14, `07:73` R15): the
    read contract, `tools` as the read seam, `tool-mcp` and the digest of its recorded
    tool list with the config keys of its rows, the `signals` fields and busy reply, the
    `dialer-guarded` ranges on self-host, what T7 leaves UNVERIFIED (each vendor's naming
    rules, and counsel's review of reading a plugin id as a technical identifier;
    `08:1363`), the context pack footer, the refusals of a default label map, playbooks
    without query text, and whether a `signals` contribution may supply its own signature
    verifier. That part is proposed to the owner at S8a start as an addition to this ADR.
  - `evidenceSources`, which the ADR-047 rows also list (`00:781`; `08:1178`), is fixed at
    P2 start under ADR-028 as ADR-047 re-scopes it (`07:70` R12; `08:1159`), and the S8a
    part says so.

  This ADR changes no decision of `00-decisions.md`; where an answer edits a line of `00`
  or elsewhere, the question or the section "Edits on acceptance" names it.
- Citations: `00`, `02`, `03`, `07` and `08` are the files of `docs/architecture/` with
  those numbers, and `08:391` is line 391 of `08` at commit `8e1f7b9`. "community" and
  "licence" are `docs/design/community-first-2026-11.md` and `licence-audit-2026-11.md`,
  cited the same way. The plugin record, `docs/design/plugin-system-2026-11.md`, is cited
  by section, as the document lint requires, and `dsh-architecture` §n is section n of
  `docs/research/dsh-architecture.md`. "Decision n" alone is a decision of this ADR; a
  decision of another ADR carries its number. An outside module is cited as
  `module@version`, read in that module at that version.

## Context

ADR-060 decision 19 fixes what one artifact holds: the module path, version and sum of
in-process code, or nothing for a pack; `plugin.yaml`; a bundle of rows, all disabled; for
a pack its content and fixtures; the signed attestation; an SBOM (`08:391`; `07:1838`;
plugin record §11). Reading that text and the S1 code against the S2a exit leaves seven
gaps:

- **No layout.** No OCI layout and no media type is written. The files of `07` §8.4 are
  sketches fixed under ADR-047 at S2a (`07:72` R14). The layout is documented for third
  parties with the attestation type (`08:935`; community:203; ADR-060 decision 18) and made
  public only after L14 (`08:1343`).
- **No content digest.** Content is unpacked under `$AIOPS_HOME/content/<plugin id>/`
  (`07:1328`, `:1843`; `08:395`, `:1288`; community:298; `02:313`), "its digest is part of
  the generation digest", and "a changed runbook or label map is a new generation like any
  other change" (`08:1288`). No document says what is digested, or whether anything reads
  the unpacked tree again after install. The `content` row that serves it is S8b (ADR-060
  decision 29).
- **Two homes for `plugins.lock`.** `07:1841` calls it a "Site file". `install` takes
  `--profile`, and a failure restores "a snapshot of the profile definition, `plugins.lock`
  and the profile patch" (`08:374`, `:395`). dsh keeps its manifest, its lockfile and the
  profile patch in one profile directory (dsh-architecture §3.1, §3.2, §4.2) and restores
  the manifest and the lockfile from one snapshot, which leaves the patch out (§4.2).
  ADR-060 decision 26 leaves the choice here.
- **One name, two owners.** "A row id is `<pack name>:<row>` and two packs cannot collide"
  (`08:391`; `07:1817`; community:298), and the group is "named by the artifact"
  (`00:478`). A plugin id is `<owner>/<name>` and a row id is kebab-case (`00:657`), so
  `acme/connector-x` and `beta/connector-x` both name the group `connector-x` (ADR-060
  decision 19).
- **An open template.** ADR-060 decision 24 fixes that `plugin new connector` names no
  plugin id, writes rows of an instantiable plugin of the release it runs against with a
  config from the examples of that plugin's `configSchema`, and runs at S2a against the
  test release, whose one instantiable plugin is the toy echoer. Which plugins, the row
  ids, `roles` (which every server row states, `08:156`) and the other files stay open. The
  worked bundle of `07` §8.4 holds an `${env:}` reference (`07:1697`), which check 8 now
  refuses in the bundle of an installed artifact (ADR-060 decision 57).
- **No home contract for a container.** `$AIOPS_HOME` is the root of layers 2 and 3
  (`00:460-461`; `08:122-123`). At `8e1f7b9` the server takes `--home` or `$AIOPS_HOME`,
  reads `profiles/<p>/` and, by default, `bundles/<name>/` from it, lets a file of the home
  win over one shipped in the binary, and writes `<home>/run/status.json`
  (`internal/serve/serve.go`; `internal/runfile/runfile.go`; ADR-059 decision 31). The test
  control rows read and write their files in `<home>/run/` too (`test/dist/plugins`). D-2
  runs `kernel` from an image that `dist use` rolls (`02:314`), and the exit's clean machine
  holds `aiopsctl` and a container runtime only (ADR-060 decision 30). Whether a `kernel`
  that runs as a container mounts the same host directory is not stated (`02:483`, item
  26).
- **An open field list in check 8.** The installed-artifact clause denies a bundle that
  changes an existing row, inserts a row enabled, or inserts a row of a plugin that is
  neither its own nor instantiable (`08:504`; `07:1891`), and ADR-060 decision 57 adds the
  `${env:}` and `${file:}` references. It says nothing of the other fields an inserted row
  may set, while a bundle layer may raise `required` and set `intercept` on a
  provider-bound key (`08:146`, `:148`), and a required row that is disabled stops boot
  (`08:459`).

## Decision

### The artifact

1. **One OCI image manifest per artifact.** `plugin pack` writes one OCI image manifest
   (`application/vnd.oci.image.manifest.v1+json`) whose `artifactType` is
   `application/vnd.aiops.plugin.v1` and whose `config` is the empty descriptor
   `application/vnd.oci.empty.v1+json`, the form the image specification gives an artifact
   with no config of its own (`github.com/opencontainers/image-spec@v1.1.1`, `manifest.md`,
   "Guidance for an Empty Descriptor" and "Guidelines for Artifact Usage"). The prefix
   `vnd.aiops` is question Q5. The digest of this manifest is the artifact digest: the spec
   `oci://<registry>/<repo>@sha256:<digest>` names it (`07:1852`), `plugins.lock` records it
   (decision 8), and `plugin sign` signs it (ADR-020). Its layers, in this order:

   | Layer | Title annotation | Media type | Present in |
   |---|---|---|---|
   | The manifest | `plugin.yaml` | `application/vnd.aiops.plugin.manifest.v1+yaml` | Every artifact |
   | The bundle | `bundle.yaml` | `application/vnd.aiops.plugin.bundle.v1+yaml` | Every artifact |
   | The module | `module.json` | `application/vnd.aiops.plugin.module.v1+json` | In-process code |
   | The content | `content.tar` | `application/vnd.aiops.plugin.content.v1.tar` | An artifact with a `content/` directory |
   | The fixtures | `fixtures.tar` | `application/vnd.aiops.plugin.fixtures.v1.tar` | A pack |
   | The SBOM | `sbom.spdx.json` | `application/spdx+json` | Every artifact |
   | The attestation | as ADR-020 fixes | as ADR-020 fixes | Every artifact, as a layer or an OCI referrer (ADR-060 decision 18) |

   - The title annotation is `org.opencontainers.image.title`, so a generic OCI client
     lists the layers by file name. Each type appears at most once.
   - Note of 2026-11-27 (review round over the whole step, round 2): ADR-060 decision 19
     gives content to a pack, whose lock entry carries the content digest into the
     generation digest (decision 8). In-process code has no lock entry, so an artifact
     with a `content/` directory is a pack: `plugin pack` refuses in-process code with
     one, install refuses an in-process artifact with a content layer, and boot denies
     one that a home holds, naming ADR-060 decision 19.
   - A reader, `install` and the gate included, ignores a layer type it does not know and
     fetches no blob of it, as a reader of `plugin.yaml` ignores an unknown field of a
     higher minor; `plugin check` refuses one, as it refuses an unknown field (`07:120`).
     The `v1` of a type changes only with a break of that layer's container format; the
     schema of `plugin.yaml` keeps its own `manifestVersion` (ADR-048). Whether the layout
     is a frozen-additive contract is question Q7.
   - The module layer holds `{"path", "version", "sum"}`, the entry form of `plugins` in
     the site's `dist.yaml` (`08:401`), so install prints the `dist.yaml` lines of `08:397`
     from it.
   - The image reference of a remote plugin arrives at S2b with the remote runtime
     (ADR-060 decision 29), as one more layer type under the same `artifactType`.
   - The SBOM is an SPDX 2.3 document in its JSON form, which `aiopsctl` writes itself
     with the standard library's `encoding/json`: `plugin pack` of a pack runs where
     `aiopsctl` is alone, with no builder image and no container runtime (ADR-060
     decision 30, P1), and the SBOM of the builder's images is ADR-044's, not this one.
     Writing it adds no linked module, so ADR-060 decision 34 asks nothing more. SPDX
     matches the SPDX expression of the manifest's `license` (`08:1345`) and the SPDX list
     data that `aiopsctl` embeds (ADR-060 decision 23).
   - A pack's SBOM lists the files of its content and fixtures layers with their digests
     and the pack's `license`. That of in-process code lists the build list of its module,
     each module with its path, version and sum, read with `go list -m -json all` from the
     author's toolchain, which code needs for `plugin check` (`07:1529`); it states no
     licence of those modules (`NOASSERTION`), since the licence scan of a build list is
     the builder's (`08:1345`; check 6).
   - If ADR-020 makes the attestation a layer, its subject cannot be the artifact digest,
     which would contain it; the subject per kind of artifact is ADR-020's.

   **On disk.** `plugin pack` writes the artifact as an OCI image layout
   (`github.com/opencontainers/image-spec@v1.1.1`, `image-layout.md`): the directory
   `artifact/` beside `plugin.yaml`, or the one `--out` names, holding `oci-layout`,
   `blobs/sha256/` with every blob of the table above, and `index.json`, whose descriptor
   of the image manifest carries `org.opencontainers.image.ref.name` set to the manifest's
   `version`. `plugin pack` refuses a directory that exists and is not empty, so a layout
   holds one artifact; `plugin check` step 7 packs into a directory of its own and removes
   it (`07:1573`). `plugin sign` reads that layout and writes into it what ADR-020 fixes
   (the signature, and the attestation where it is a referrer). `plugin push` reads it and
   copies the image manifest, every blob it names and what `sign` added to the registry,
   and prints the spec by digest (`07:1852`). A local-path spec (`07:1852`) names such a
   directory: `inspect`, `gate --against`, `install` and `verify` read it as they read a
   registry, and refuse a layout whose `index.json` does not name exactly one manifest of
   `artifactType` `application/vnd.aiops.plugin.v1`. The layout is written and read with
   the store of `oras-go` (decision 4), package `content/oci`, which implements that
   specification. Note of 2026-11-27 (the owner's answer to question Q8, decision 42):
   install and verify of remote code take a registry spec. Each refuses a local-path
   spec of remote code, names the reason and `plugin push`, and writes nothing;
   `inspect` and `gate --against` read a local path of remote code as they read any
   other.

2. **What each command fetches and verifies.** Every blob is checked against the digest of
   its descriptor before it is used. A command that verifies the signature over the image
   manifest, as ADR-020 fixes, fetches the image manifest and the signature first and uses
   no other blob until the signature verifies.
   - `plugin inspect` fetches the image manifest, `plugin.yaml` and the bundle, and never
     the content, module or image (ADR-060 decision 58; `08:373`; `07:1853`). It verifies
     no signature, since it installs and runs nothing: it prints the artifact digest and
     that no signer was verified, and `install` verifies the signature over that same
     digest. The content paths it prints come from the annotation
     `aiops.plugin.content.paths` (its prefix is question Q5) on the content layer's
     descriptor, a JSON array of the paths in the layer in the order of decision 3. `plugin
     pack` writes it, and install refuses an artifact whose content layer holds other
     paths.
   - `plugin gate --against` fetches what `inspect` fetches and the attestation, and
     verifies no signature: it does not evaluate check 5 (ADR-060 decision 17).
   - `plugin install` verifies the signature, then fetches `plugin.yaml`, the bundle, the
     module and content layers and the attestation; it never fetches the fixtures or the
     SBOM.
   - `plugin verify` verifies the signature and fetches what install fetches and the
     fixtures (`07:1512`); from S6a an index fetches the fixtures too (community:192).

3. **The content layer and the content digest.**
   - `plugin pack` writes the artifact's `content/` directory as one uncompressed tar in a
     canonical form: one entry per regular file, sorted by path in byte order; each path
     relative to `content/`, slash-separated, valid UTF-8 and already clean (no empty, `.`
     or `..` component and no leading `/`); mode 0644, user and group 0 with empty names,
     modification time 0; a ustar header, with a pax header only for a path that the ustar
     fields cannot hold; no other entry type and no extended attribute; the two zero blocks
     that end an archive and nothing after them. A directory is implied by the paths in it.
   - The content digest is the digest of that layer's descriptor. Since the form is
     canonical, writing an unpacked tree again in it gives the same bytes, so one digest
     serves the pull and every later check of the tree (question Q3), and no compressor's
     output has to stay stable across versions. Content is text (`07:1328`), so little is
     lost without compression.
   - Install refuses, before it writes anything, a content layer that is not in that form
     (writing back the entries it read must give the same bytes), that holds a link, a
     device or a directory entry, or that holds a path outside the rules above. It unpacks
     into a staging directory beside `$AIOPS_HOME/content/<plugin id>/` and renames the
     staging directory to that path, which no tree occupies then: decision 7 never
     replaces a tree that a profile names, and install removes a leftover one first
     (decision 10). A reader sees no tree or the whole one.
   - The fixtures layer has the same form; nothing unpacks it into the home. The name lint
     of `plugin check` step 1 already covers content paths (`07:1567`).

4. **The OCI client** that `plugin pack`, `sign`, `push`, `inspect`, `gate --against`,
   `install` and `verify` use is
   `oras.land/oras-go/v2@v2.7.0`, which `08:1403` and licence:43 already list among the
   linked libraries (as ORAS, Apache-2.0, without a version). The operations plugin links it
   (ADR-060 decision 41), and with it both binaries of the release table (`08:359`); `boot`
   links no OCI code. Its register entry (ADR-060 decision 51) and its entry in the licence
   record land with the first commit that links it (ADR-060 decision 34).

   Read in `oras.land/oras-go/v2@v2.7.0` (tag `v2.7.0`, published 2026-10-23): `LICENSE` is
   the Apache License 2.0 with "Copyright 2021 ORAS Authors", and the module has no
   `NOTICE` file. Its `go.mod` requires `go 1.26.0`, as the root module does, and three
   modules, and its packages import only these, each read in that module at that version:
   - `github.com/opencontainers/image-spec@v1.1.1`, the packages `specs-go` and
     `specs-go/v1`: `LICENSE` is the Apache License 2.0 with "Copyright 2016 The Linux
     Foundation". Its `go.mod` also requires two modules that only its `schema` package
     imports, and no package of `oras-go` imports `schema`. They enter the module graph
     and `go.sum` but not the package graph. Both licences are on the L7 allow-list
     (`08:1336`), so the scan passes whether it reads the package graph (L7; licence:204)
     or the build list (L16, `08:1345`):
     - `github.com/russross/blackfriday@v1.6.0`: `LICENSE.txt` is the Simplified BSD
       License (BSD-2-Clause) with "Copyright © 2011 Russ Ross"; its `go.mod` requires
       nothing.
     - `github.com/santhosh-tekuri/jsonschema/v5@v5.3.1`: `LICENSE` is the text of the
       Apache License 2.0, sections 1 to 9, with no copyright line, and the module has no
       `NOTICE` file; its `go.mod` requires nothing.
   - `github.com/opencontainers/go-digest@v1.0.0`: `LICENSE` is the Apache License 2.0
     with "Copyright 2019, 2020 OCI Contributors" and "Copyright 2016 Docker, Inc.". Beside
     it, in the directory of the package `digest`, `LICENSE.docs` is the text of Creative
     Commons Attribution-ShareAlike 4.0 International, and the module's `README.md` says
     that `README.md` and `CONTRIBUTING.md` are under "the Creative Commons Attribution
     4.0 International License" set forth in that file. How the licence scan treats that
     file is question Q6.
   - `golang.org/x/sync@v0.24.0`, the packages `errgroup` and `semaphore`: `LICENSE` is
     BSD-3-Clause with "Copyright 2009 The Go Authors", with the Go `PATENTS` grant. The
     root module already requires it at that version.

   **The exit's registry.** The layout needs from a registry what the image specification
   1.1 adds to a manifest: `artifactType`, an empty config blob, and layer types outside
   the image layer types, all stored byte for byte. It needs the referrers API only if
   ADR-020 makes the signature or the attestation an OCI referrer, and `oras-go` falls
   back to the referrers tag schema where a registry has none (`registry/remote`,
   `SetReferrersCapability`). The exit runs the registry program of
   `github.com/distribution/distribution/v3@v3.1.2` (tag `v3.1.2`, published 2026-09-24),
   read at that version: `LICENSE` is the Apache License 2.0 with its appendix left as the
   template, so it carries no copyright line of its own, and the module has no `NOTICE`
   file. Its handler of an OCI manifest keeps the pushed bytes and checks only that the
   blob of each descriptor exists, whatever its media type (`manifest/ocischema`,
   `registry/storage/ocimanifesthandler.go`), and the module has no referrers API, so the
   exit reaches a referrer form, if ADR-020 picks one, through the tag schema. The image
   that runs it is pinned by digest, with its register entry and the licences of its
   layers, in the commit that adds the `s2a-exit` job, before S2a ends (ADR-060 decision
   34). Note of 2026-11-25: it is pinned ahead of that job. The register entry
   `distribution-registry`, mode `tool`, pins the Docker Official Image
   `registry:3.1.2@sha256:82f6ff4a7f982960c8aa51617fea3aabc485df6e55cce80d22deb1de36f1bc3c`,
   whose `/bin/registry` reports v3.1.2; the module's licence was read again that day,
   and the licences of the layers, Alpine 3.24.4 packages, in the image's
   `/lib/apk/db/installed`, as the licence record row "Test registry of the S2a exit"
   lists them. The layers hold GPL-2.0 packages, so the image is pulled in CI only and
   never shipped (L5).

### The install

5. **The files install keeps, and the group.**
   - Install writes the image manifest, `plugin.yaml` and the bundle, byte for byte, into
     `$AIOPS_HOME/bundles/<plugin id>/` as `artifact.json`, `plugin.yaml` and
     `bundle.yaml`, and appends the plugin id to the profile's bundle list as a direct
     bundle at its end (`07:1855`; `08:121`; plugin record §11). A bundle carried by an
     artifact is addressed by its plugin id (community:296; `00:658`). The attestation and
     the verified signer are kept where ADR-020 puts them, in that directory unless ADR-020
     names another path under the home.
   - A bundle name that holds `/` is a plugin id and never the name of a first-party
     bundle, which is `aiops-<name>` (`00:658`). Boot reads such a bundle, and the
     `plugin.yaml` beside it, from the home only, never from `--bundles` or the bundles
     shipped in the binary (`internal/serve/serve.go`), and applies the installed-artifact
     clause of check 8 to exactly those bundles (decision 11). Upgrade note of 2026-11-26
     (review round over the whole step): S1 read a bundle named `<owner>/<name>` from
     `--bundles` or the home like any other, so an S1 home whose profile lists one boots
     under S2a with each of its rows denied by check 5 and, where they are required, exits
     3. Such a bundle has to be renamed without `/`, in the profile and under `bundles/`,
     before the upgrade; the denial of an artifact with no `artifact.json` and no lock
     entry says so.
   - Boot mounts the rows of that bundle under one `core:group` row that it adds itself,
     whose id is the `<name>` segment of the plugin id, so a row id is `<name>:<row>`
     (`08:159`; `08:391`; `07:1817`). The group row is enabled, holds only the bundle's
     rows, and states every role that one of them states, so it mounts wherever a child
     does. A bundle can neither name its group nor place a row outside it. The loader
     records the plugin id as the origin artifact of each row under the group (ADR-060
     decision 19).
   - `plugin enable <name>:<row>` writes `{id, enabled: true}` for that id into the
     profile layer (`07:1737`; `08:375`).

6. **A second artifact of the same name under another owner** is question Q2. Whatever
   the answer, install decides it before it writes anything (`08:374`). At boot, a group
   whose id equals the id of a row composed before it, such as a first-party row that a
   later release adds under that id, is a denial of check 8 on the bundle of that
   installed artifact, which may insert new ids only (`08:143`, `:504`): boot inserts none
   of that artifact's rows, names the artifact, both ids and check 8 at boot and in the
   dump (`08:508`), and composes the rest of the profile. A composition error, the S1
   handling of an existing id (`boot/compose.go`), would instead stop the boot of every
   profile that holds the artifact after such an upgrade.

7. **One digest per plugin id per site.** The content directory `content/<plugin id>/` is
   one path per site (`07:1843`; `02:313`), and from S2b a remote plugin has one shared
   instance per site (`08:386`; `02:312`), so a site holds one digest of a plugin id. This
   departs from dsh, which installs with pnpm inside each profile directory, so that each
   profile can hold its own version of a package (dsh-architecture §4.2). Install into a
   profile of an id that another profile names, with its kept files (decision 5) at the
   same digest, appends that profile's bundle list and reuses them. Install of an id that
   a profile names at another digest refuses, names the kept digest and the profiles that
   name the id, and writes nothing. A
   new version arrives by `update` at S6b (`08:377`), or by `remove` and `install`. A
   `profile rollback` at S6b that would return a lock to another digest of an id
   (`08:378`) is bound by this rule or needs content kept per digest; S6b chooses, and its
   step names the choice.

8. **`plugins.lock`.** Where it lives is question Q1. It is YAML, read by `boot` and
   written by the operations plugin (ADR-060 decisions 6 and 41), replaced by a rename,
   and holds one entry per locked artifact, sorted by id:

   ```yaml
   lockVersion: 1
   artifacts:
     - id: acme/connector-x
       digest: sha256:<digest of the image manifest>
       content: sha256:<digest of the content layer>   # absent without a content layer
   ```

   In-process code is pinned by the signed distribution manifest and has no entry
   (`07:1841`; `00:478`). Where the verified signer of each digest is kept is ADR-020's
   (`07:1841`, R17; ADR-060 decision 26); this ADR adds no field for it. The locks section
   of the generation digest (`boot/digest.go`; `08:184`) holds, per entry that the composed
   profile reads, the pair (id, digest) and, where the entry has content, the pair
   (`content/<id>`, content digest). A plugin id holds one `/` and `content/<id>` two, so
   the names never meet, and the encoding of `boot/digest.go` does not change.

9. **What install kept, at boot,** is question Q3.

10. **Remove.** `remove <id> --profile <p>` (ADR-060 decision 47) takes the id out of the
    profile's bundle list and drops its lock entry as question Q1 places the lock. In the
    profile layer it drops the `enabled` field of every item whose id is under the
    artifact's group, and an item left with its id alone, so that a later install leaves
    those rows disabled (`00:478`). It keeps every other field there, such as a config or
    a credential reference that the operator set, and prints each item it kept: a later
    install of the same id finds them again, and until then boot warns on each as an
    unknown id (`08:160`). `bundles/<plugin id>/` and `content/<plugin id>/` go when no
    profile names the id. Install writes those directories before it changes the lock and
    the bundle list, and remove deletes them after, so a failure that restores the
    snapshot of `08:374` never leaves a lock entry without its files. A failure between
    those steps can leave either directory with no profile that names the id; install
    removes such a leftover, and says so, after the checks of decision 7 and before it
    writes. Note of 2026-11-23: `remove` deletes the two directories only after a final
    result that shows the change, or when no process runs the profile, and keeps them
    where the verdict stays unknown (ADR-062 decision 11).

### The gate rule

11. **Check 8 on the bundle of an installed artifact.** As written (`08:504`; `07:1891`;
    plugin record §12) and with ADR-060 decision 57, check 8 denies the bundle of an
    installed artifact that holds anything but `insert` items; that inserts a row whose
    `enabled` is not `false`; that inserts a row whose `plugin` is neither the artifact's
    own id nor a first-party plugin whose manifest in the module table says
    `instantiable: true` (`07:132`; ADR-060 decision 19); or that holds an `${env:}` or
    `${file:}` reference. A `core:group` or `core:include` row is not a row of such a
    plugin, so the clause denies it: the group is the launcher's (decision 5), and an
    include would mount a file the artifact does not carry. Which other fields such a row
    may set is question Q4. Each entry point that evaluates check 8 (ADR-060 decision 17)
    evaluates this clause on the bundle it reads.

### The connector template

12. **What `aiopsctl plugin new connector` writes** (`07:1526`, `:1549`):
    - `plugin.yaml`: the hand-written half that ADR-048 requires of a pack, with the id
      the command is given, `<publisher>/connector-<system>` or, with `--contrib`,
      `contrib/connector-<system>` (`00:657`; `08:407`), `version: 0.1.0`,
      `license: Apache-2.0`, `publisher.id` equal to the owner segment,
      `stability: experimental`, `scope` set to process in the form ADR-048 fixes, a list
      or one value (`07:94`, `:128`; ADR-060 decision 33), and `runtimes: []` (`07:129`;
      `08:387`); then the generated half that `plugin check --write` derives,
      `requires.definitions` of the registries the rows feed and no `pluginApi` (`07:139`).
    - `bundle.yaml`: one `insert` item with one row per instantiable plugin of the release
      it runs against (`--against`, by default the release this `aiopsctl` was built from;
      ADR-060 decision 24), or per plugin that `--plugin <id>` names, a flag that may be
      repeated. Each row has `id` the plugin's name, which a row id defaults to (`00:657`);
      `plugin` the plugin's id; `roles: [api, worker]`, as in the worked pack (`07:1694`),
      and never `edge`, so that no row lands in the internet-facing role by omission
      (`08:156`); `enabled: false`; and `config` the first entry of the root `examples` of
      the `configSchema` that the release's signed manifest carries for that plugin
      (ADR-060 decision 20). Against a release that serves no instantiable plugin, as the
      shipped release at S2a, the command fails with that reason and writes nothing.
    - `fixtures/` with a README that names the cases that step 5 and the scenario read,
      and no case. Step 5 runs, for each registry the pack's rows feed, that registry's
      suite on the pack's rows (`07:1571`; ADR-060 decision 46), and for a pack each
      obligation of the suite takes its recorded case from the pack's fixtures, at the
      path the suite fixes for that obligation: `fixtures/<registry>/<obligation id>.json`
      for `echoers` at S2a. An obligation whose case is missing fails with its id and the
      path it read. So a fresh pack fails step 5 with one line per obligation (`07:1561`;
      `08:1264`; ADR-060 decisions 24 and 30, P1), and the fixture of P1 holds one case per
      obligation. The files of the echo scenario sit beside them, in the layout that
      scenario fixes (ADR-060 decision 49).
    - `README.md`: what the pack connects to, the `plugin enable <name>:<row>` lines, and
      the profile-layer patch with which an operator sets a row's config, since a patch
      replaces the whole config (`08:157`) and the bundle may hold no `${env:}` or
      `${file:}` reference (ADR-060 decision 57). `LICENSE`, Apache-2.0, and a CI workflow
      with one job that runs `aiopsctl plugin check ./`, as the Go template has
      (`07:1559`).
    - No `content/` and no `testenv/`: content is optional (`07:1838`), and the test
      environment against which an author records is the author's (`07:1658`).

    The examples are the files `testdata/config/*.json` of the instantiable plugin, which
    step 3 of `plugin check` already validates against the derived schema (`07:1569`):
    `plugingen` writes them, in the byte order of their names, as the root `examples` of
    the `configSchema` it derives, so the release's signed manifest carries them (ADR-060
    decisions 11 and 20), and "the first entry" is the first of them. `examples` is a
    keyword of JSON Schema 2020-12, so this adds no field to the manifest. Two rules follow
    for an instantiable plugin, and `plugin check` enforces them: it has at least one such
    file, and no file holds an `${env:}` or `${file:}` reference, so a pack written from
    it passes check 8. At S2a the template job and step P1 write one row of the toy
    instantiable echoer of the test release (ADR-060 decisions 24 and 38); from S8a the
    same command writes rows of `tool-mcp`, `signal-ingest-generic-json` and
    `notifier-webhook` with no change here.

### The home

13. **The paths of `$AIOPS_HOME` and how a `kernel` mounts them.** `aiopsctl` runs on the
    host and writes the home; a `kernel` reads it, as a binary on the host or as a
    container (`02:294`, `:307`, `:314`):

    | Path under `$AIOPS_HOME` | Written by | The kernel mounts it | Source |
    |---|---|---|---|
    | `profiles/<p>/profile.yaml`, the bundle list | `aiopsctl` `profile init`, `install`, `remove` | Read-only | `08:121`; ADR-060 decision 27 |
    | `profiles/<p>/aiops.patch.yml`, layer 2 | The operator; `aiopsctl` `enable`, `disable`, `remove` (decision 10) | Read-only | `08:122`, `:375`; ADR-060 decision 57 |
    | `aiops.patch.yml`, layer 3 | The operator | Read-only | `08:123` |
    | `plugins.lock`, where question Q1 puts it | `aiopsctl` `install`, `remove` | Read-only | `07:1841` |
    | `bundles/<plugin id>/` | `aiopsctl` `install`, `remove` | Read-only | Decision 5 |
    | `content/<plugin id>/` | `aiopsctl` `install`, `remove` | Read-only | `07:1843`; `02:313` |
    | `trust.yaml` | `aiopsctl` `trust add` | Read-only | `07:1840`; ADR-060 decision 26 |
    | The records of `dist use` and of install: the verified signer and the digest | `aiopsctl` `dist use`, `install` | Read-only; their names and form are ADR-020's, under the home and outside `run/` | ADR-060 decisions 26 and 33 |
    | `image.yaml`: one field, `image`, the reference by digest of the image that `dist use` verified | `aiopsctl` `dist use` | Read-only, and the kernel does not read it: the host starts the container from it (decision 15) | ADR-060 decision 22 |
    | `write.lock`, the one write lock of the home | `aiopsctl`: every method of the operations library that writes the home holds it | Read-only; the kernel takes no lock and does not read it at S2a | ADR-061 decision 19 |
    | `snapshot/`: a copy of every file a write replaces or deletes and the list of the paths it creates | `aiopsctl`: every method of the operations library that writes the home, which removes it after the submission or restores it | Read-only; the kernel does not read it at S2a | ADR-061 decisions 17 and 19 |
    | `run/` | The kernel: `status.json`, the files of the test control rows, and `reload.sock` and `reload.lock` of `reload-socket` (ADR-062) | Read-write | ADR-059 decision 31; `internal/runfile/runfile.go` |

    At S2a the kernel writes nothing outside `run/`. Later paths join the table with their
    step: `tenants/` at S4b and `profiles/<p>/compatibility.yaml` at S6b (`08:508`). From
    S6a `plugin-manager` writes the profile files from inside the kernel (`08:369`;
    `03:71`), so the step that builds it changes the mount of the paths it writes.

14. **Ownership.** One user of the host, the site user, owns the home and everything in
    it. `aiopsctl` creates the home and `run/` with mode 0700, writes each file with mode
    0600 and replaces it by a rename in its directory, except `write.lock`, which it
    creates once with mode 0600 and never renames or deletes, since the lock binds to one
    inode (ADR-061 decision 19), and refuses to write into a home whose owner is not its
    effective user, so a run as another user cannot leave a file that the kernel cannot
    read. The kernel, as a binary or as a container, runs with the uid and gid of the
    home's owner as the host sees them. For a container that is a property of the
    runtime's user mapping, not of the image: the container runtime of the exit's clean
    machine (ADR-060 decision 30) is rootful Docker, run with `--user <uid>:<gid>` of the
    owner, and any other runtime and mode qualifies whose mapping gives the same, such as
    rootless Podman with `--userns=keep-id`. A mapping to a subordinate uid of the host
    cannot read a 0600 file of the home, and boot then fails on the first file it reads.
    On a host with SELinux enforcing, each bind of decision 15 also needs a label the
    container may read (the `z` option of the bind in Docker and Podman), which the exit
    does not test. Only the site user can then open `run/reload.sock`, the socket of
    ADR-062, and no other unprivileged user of the host reads or changes a file of the
    home between install and boot. Any protection of the records of install and `dist use`
    beyond that is ADR-020's (`07:75` R17). Note of 2026-11-27 (review round over the
    whole step, round 2): the server holds the home to the same rule. It refuses a home
    or a `run/` whose owner is not its effective user, before it creates
    `run/process.lock`, and names a lock file it cannot open instead of calling it held
    by another process. A `run/` of another mode, as the S1 server created it with mode
    0755, is set to 0700 at the start of the server and at a write of `aiopsctl`, each of
    which says so (upgrade note of the S2a report). Note of 2026-11-27 (the owner's
    answer to question Q9, decision 43): one directory is a stated exception of the mode
    0600. The directory of an instance of remote code, `remote/instances/<plugin id>/`,
    has the mode 0755 and its files the mode 0644, so that the user of a unit reads the
    one directory the unit binds; the home and the three directories above that one are
    of mode 0700, which `aiopsctl` checks and sets at every install of remote code and
    every `plugin start` (decision 30).

15. **The container form.** A `kernel` container gets the home bound at the same path it
    has on the host, read-only; `run/` bound over it at the same path, read-write; the
    user of decision 14; and these variables of the environment that starts it:
    `AIOPS_HOME` set to that path; every `AIOPS_REF_*` variable, which `${env:}` reads
    (`08:162`) and which an operator's profile or site layer may name for a pack row
    (ADR-060 decision 57); and `AIOPS_AGENTS_PAUSED` and `AIOPS_TELEMETRY_DISABLED` where
    they are set, which boot reads (`08:126`). The form needs no other variable. `run/`
    exists before the container starts, since `aiopsctl` creates it with the home. The
    same path on both sides lets every dump, status and message name one path, and it is
    the Helm form of `02:313` ("mounted into `kernel` at the same path"). Directories are
    bound and never single files, because `aiopsctl` replaces a file by a rename, which a
    bind of one file does not follow. The directory of `--refs-dir`, and the directory of
    each file that `--patch` or `--limits` names, given by an absolute path, are bound
    read-only at their own paths, outside the home (`internal/serve/serve.go`). On the
    exit's clean machine the test starts the image that `image.yaml` names in this form
    (decision 13; ADR-060 decisions 22 and 30), on the home of step C3, whose files are
    0600, and step C4 passes only when the kernel has composed `dev` from those files and
    written `run/status.json`, which the host sees owned by the home's owner. How a chart
    delivers the home to a cluster, and to several replicas, stays with `deploy/helm` and
    ADR-038 (`02:479`, item 22).

## Consequences

- The S2a code gains, in the operations plugin, the writer and reader of decisions 1 to 3
  on `oras-go`, for a registry and for the local layout of decision 1, and the checks of
  decisions 3, 6, 7 and 10 at install. `boot` gains the reader of `bundles/<plugin id>/`,
  the group row of decision 5, the boot rule of decision 6, the clause of decision 11 and
  the lock entries of decision 8; the bundle names of `profile.yaml` admit a plugin id;
  `internal/serve` creates `run/` with mode 0700 and finds it present under a read-only
  home.
- The exit gains the container case of decision 15: the kernel reads the 0600 files of
  the home and writes `run/` under the user mapping of decision 14.
- A site cannot hold two digests of one plugin id, and, with question Q2 (a), two
  artifacts of one name in one profile, so two publishers' packs for one system never
  share a profile.
- The exit's pack fixture of step P1 carries at least one content file, so that step C1
  prints a content path and step C3 unpacks one (ADR-060 decision 30).
- The layout, the media types and the annotation key are documented in the repository for
  third parties with the attestation type (ADR-060 decision 18). Their first publication
  waits for L14 (`08:1343`); with question Q7 (a) they are frozen-additive from then.
- The root module gains `oras-go` and two modules through it, and two more in its module
  graph and `go.sum` only; `golang.org/x/sync` is already in its graph. The licence report
  of both binaries lists them (ADR-060 decision 36). The release builds pass only once
  question Q6 is answered and the scanner follows the answer.
- The exit runs a registry of Distribution v3.1.2, whose image is pinned with the job.
- At S6a the read-only mount changes for the paths `plugin-manager` writes (decision 13).

## Edits on acceptance

Made in the commit that records the owner's answers, as `08:996` has an ADR's edits land
in the same change:

- `02:483` (item 26): settled by decisions 13 to 15. `02:313`: in D-2 the directory is
  bound into a `kernel` container at the same path, read-only.
- `07:72` (R14): this ADR fixes the artifact form, the local layout, the layer forms of
  the content and the fixtures, and the path of a pack's conformance case per obligation
  (decisions 1, 3 and 12). A `bundle.yaml` is a bundle layer (`08:143-148`) with the
  clause of decision 11. The rest of the fixture directory is fixed with the scenario
  that reads it, the echo scenario at S2a (ADR-060 decision 49) and the connector
  scenario at S8a start, and the config keys of `tool-mcp` rows stay for S8a start.
- `07:1650-1660` and `07:1690-1708`: the layout of decisions 1 and 3, and a bundle with no
  `${env:}` reference at `07:1697`, whose endpoint the operator sets in the profile layer
  as decision 12 describes.
- `07:1841`, as question Q1 is answered.
- `08:121` (layer 1) and `08:430` (profile files): the bundle of an installed artifact is
  `$AIOPS_HOME/bundles/<plugin id>/bundle.yaml`.
- The one gate text of ADR-060 decision 16: the clause of decision 11, the boot rule of
  decision 6, and the clauses that the answers to questions Q3 and Q4 add.
- The lines that the chosen option of question Q2 names.
- The lines that the chosen option of question Q6 names, and with question Q7 (a) `00:266`
  and the table of `08:912-920`.

## Questions for the owner

Each question gives its options, the recommended one, why, and when the answer is needed.

**Q1. Is `plugins.lock` one file per site or one per profile?** `07:1841` says "Site
file". `install --profile` and its restore rule treat it as a file of the profile
(`08:374`, `:395`), as dsh does: its lock sits in the profile directory and is restored
with the profile's manifest from one snapshot (dsh-architecture §4.2), the install that
`08:76` (row 34) adapts. Either way one site holds one digest per plugin id, and an S6b
rollback that changes a lock digest is bound by that rule (decision 7).
- (a) One per site, `$AIOPS_HOME/plugins.lock`, as `07:1841` has it. A profile's
  generation digest takes the entries whose id its bundle list names, and an entry goes
  when no profile names the id.
- (b) One per profile, `$AIOPS_HOME/profiles/<p>/plugins.lock`, beside the two files it is
  restored with. A profile's generation digest takes its own file whole. Install reads
  the other profiles' locks for the rule of decision 7. `07:1841` becomes "Profile file".
- Recommended: (b). It is the dsh shape for the lockfile, the snapshot of `08:374` becomes
  one profile's three files, and a lock has the scope of the generation digest it feeds;
  the cost is one edited line of `07` and a read of the other locks at install and
  remove. Needed before the first commit that writes `plugins.lock` merges.
- **Answered 2026-11-23: (b)**, decision 16.

**Q2. What does install do when an artifact of the same name under another owner enters
a profile?** The group of `acme/connector-x` and that of `beta/connector-x` are both
`connector-x` (`08:391`; `00:657`). The naming rule makes this the common case for packs:
every third-party pack for one system is named `connector-<system>` or `<contribution
noun>-<system>` (`00:657`; T7, `08:1363`), so two publishers' packs for VictoriaLogs are
both `connector-victorialogs`. dsh names a bundle by its scoped package name
(`@deepseek-ai/dsh-base`; dsh-architecture §3.1, §3.2), so two owners' bundles of one
short name coexist there as packages, and a profile here names a bundle by its plugin id
under every option (community:296). dsh mounts no group per bundle (§3.2), so the group,
and a clash of its id, depart from dsh under every option (`00:478`).
- (a) Install composes the target profile with the artifact's bundle appended, as boot
  would, and refuses when the group id equals the id of any row of that composition, the
  group of the other artifact included. It names both, writes nothing and exits 1 with the
  reason (ADR-060 decision 23). "Two packs cannot collide" then reads "two artifacts of
  one name never compose together: the second install is refused", so two publishers'
  packs for one system never share a profile; the operator removes one or installs the
  other into another profile. A first-party row that a later release adds under a group's
  id is caught at boot by decision 6, which leaves the artifact's rows out and names the
  clash, and the operator then removes the artifact. Edits `08:391`, `07:1817` and
  community:298 to that reading.
- (b) The group carries the owner, with a separator outside kebab-case so that no two ids
  meet, for example `acme.connector-x:logs`. Two publishers' packs for one system share a
  profile, and no first-party row id, which is kebab-case, can take a group's id. Edits
  `00:657` (a row id is kebab-case), `08:391`, `07:1737`, `07:1817` and community:298.
- (c) `install --as <group>` names the group when the default is taken, and the profile
  records the alias. Departs from "one group named by the artifact" (`00:478`).
- Recommended: (a). It keeps the written row id `<pack name>:<row>` and the `plugin enable
  <name>:<row>` lines of the worked pack (`07:1737`), and adds no name form and no flag.
  Its cost is the one stated above: two packs for one system never share a profile, and a
  release that takes a group's id leaves that artifact's rows out until the operator
  acts. (b) removes both costs and edits `00:657`. Needed before `install` merges.
- **Answered 2026-11-23: (a)**, decision 17.

**Q3. Does boot check what install kept against the locked digest?** Install verifies
every blob it pulls against its descriptor, and the signature over the image manifest. The
kept files then sit in the home until boot, and nothing in the documents reads them
against a digest again, while a generation "also pins plugin and pack versions" (`08:184`)
and the content digest "is part of the generation digest" (`08:1288`).
- (a) At every composition, boot hashes `bundles/<plugin id>/artifact.json` and compares
  it with the digest in `plugins.lock`, then hashes `plugin.yaml`, `bundle.yaml` and the
  content tree in the form of decision 3 and compares each with its descriptor in that
  manifest. A mismatch detaches every row of the artifact as disabled and names check 5,
  since the files are not those whose signer install verified; the clause enters check 5
  of the one gate text (ADR-060 decision 16). For in-process code, which has no lock
  entry, the same check starts from the digest that install recorded (ADR-020). ADR-020
  then has only `plugins.lock` and the records of install to protect until boot.
- (b) Install alone verifies. The read-only mount and the ownership of decisions 13 and 14
  keep the files; a change made to them in place runs, undetected, under the generation
  digest of the lock it no longer matches.
- Recommended: (a). With it, no tree that differs from the lock runs under that lock's
  generation digest, so `08:184` holds for what runs and not only for what was installed.
  The cost is hashing a few small files per artifact at each composition. Needed before
  the boot reader of `plugins.lock` merges.
- **Answered 2026-11-23: (a)**, decision 18.

**Q4. Which fields may a row of an installed artifact's bundle set?** Check 8 restricts
`enabled`, `plugin` and references (decision 11). A bundle layer may also raise
`required`, which no layer can lower (`08:146`), and a required row that is disabled stops
boot (`08:459`); it may set `inject`, `isolate` and `group` (`08:147`), and `intercept` on
a provider-bound key (`08:148`).
- (a) Only `id`, `plugin`, `roles`, `enabled: false` and `config`. Check 8 denies
  `required`, `inject`, `intercept`, `isolate` and `group` on such a row; an operator who
  wants one sets it in the profile or site layer, which the operator writes, as ADR-060
  decision 57 leaves credential references to the operator.
- (b) As written: those fields follow the bundle layer's column of `08:143-148`.
- Recommended: (a). An artifact a third party wrote then cannot stop a site's boot or
  change what another row's needs resolve to, and the clause sits in the check that holds
  the insert-only rule. The rows of a data pack set none of these fields (`07:1690-1708`).
  Needed before the installed-artifact clause of check 8 merges.
- **Answered 2026-11-23: (a)**, decision 19.

**Q5. Which prefix do the media types and the annotation key take?** They are written into
every artifact, and question Q7 asks whether they then stay frozen. The image
specification says that annotation keys "SHOULD be named using a reverse domain notation"
and reserves the prefix `org.opencontainers` (`github.com/opencontainers/image-spec@v1.1.1`,
`annotations.md`).
- (a) `application/vnd.aiops.plugin.*` and `aiops.plugin.content.paths`: the id of the
  system and of its binaries (`00:198`), the first-party owner (`00:657`), and the prefix
  of the digest encoding in the code (`aiops/composition/v1`, `boot/digest.go`). The key
  departs from that SHOULD: the documents name no domain of the project, and only
  `aiopsctl` reads the key, which a SHOULD leaves room for with a stated reason.
- (b) `application/vnd.mutex-solutions.aiops.plugin.*`, and the key in reverse-domain form
  of the module path that the owner chose (ADR-057),
  `com.github.mutex-solutions.aiops.plugin.content.paths`. It follows the SHOULD and no
  other producer uses it, and it writes the host of the repository into every artifact.
- Recommended: (a). It matches every other id of the platform. Neither prefix is
  registered, and nothing is published before L14 (`08:1343`), so the answer can change at
  no cost until then. Needed before `plugin pack` merges.
- **Answered 2026-11-23: (a)**, decision 20.

**Q6. How does the licence scan treat `LICENSE.docs` of
`github.com/opencontainers/go-digest@v1.0.0`?** The directory of the package `digest`,
which `oras-go` links, holds `LICENSE` (Apache-2.0) and `LICENSE.docs`, a
Creative Commons Attribution-ShareAlike 4.0 text for two documentation files (decision 4).
No file under that text is compiled into a binary. L7 denies CC-BY-SA "until reviewed and
recorded by name" and runs per directory (`08:1336`; licence:203, :204); check 6 and L16
deny on the licence the scan detects, and the release distributions `aiops` and
`aiops-contrib` take no acceptance (`08:502`, `:1345`). ADR-044 picks the scanner
(`08:1381`), so what it reports for that directory is not read yet; if it reports
CC-BY-SA-4.0, both release binaries fail their build, and no register entry can pass them.
- (a) Record it by name in L7 among the licences allowed with a recorded condition:
  CC-BY-SA-4.0 in `LICENSE.docs` of `github.com/opencontainers/go-digest`, for
  documentation that enters no artifact. The record is an entry of the allow-list, as the
  MCP composite is, and not an acceptance of a distribution; the scanner honours it, and
  check 6 and L16 cite it. A case of the scanner's tests under ADR-044 scans that module.
  Edits `08:1336` and licence:202.
- (b) A general rule: per directory, the scan reads the licence files that cover the files
  compiled into the binary, and records without scanning a licence file that the module's
  own `LICENSE` or `README.md` limits to documentation. Edits `08:1336`, `:1345` and
  licence:204. It covers the next module laid out the same way, and it narrows the
  per-directory scan that exists to catch a second licence beside code.
- Recommended: (a). The per-directory scan stays whole and admits one named file, so any
  other CC-BY-SA file still fails the build. Needed before the first commit that links
  `oras-go` merges (ADR-060 decision 34).
- **Answered 2026-11-23: (a)**, decision 21.

**Q7. Is the artifact layout a frozen-additive contract?** Decision 1 has a reader ignore
a layer type it does not know and `plugin check` refuse one, and changes the `v1` of a
type only with a break of its container format. `00:266` (ADR-040) names the
frozen-additive contracts, the manifest schema, the index format and the handshake
message, and the versioned things of `08:912-920` do not list the layout, while `08:935`
publishes it for third parties at S2a.
- (a) Frozen-additive from its first publication, after L14 (`08:1343`): a later release
  may add a layer type, a media type or an annotation, and changes the layout otherwise
  only under a new `artifactType`, which a reader of `v1` refuses. Edits `00:266` and
  adds a row "Artifact layout" to the table of `08:912-920`.
- (b) No promise before platform 1.0: the layout may change at a minor release with a
  migration note, as the author API may (`08:918`), and `artifactType` names its version.
- Recommended: (a). A third party that publishes packs, or runs an index, needs the
  layout as fixed as the manifest it carries; the readers of decision 1 already behave so,
  and the edit is one clause of `00` and one row of `08`. Needed before `plugin pack`
  merges.
- **Answered 2026-11-23: (a)**, decision 22.

## Owner answers of 2026-11-23

The project owner accepted this ADR, the S2a part of ADR-047, on 2026-11-23 and answered
every question of the section "Questions for the owner" on the same date, each with its
recommended option. Each decision below names the question it answers. Where a decision
above leaves a point to a question, the decision here settles it.

16. **`plugins.lock` is one file per profile** (Q1 (b)):
    `$AIOPS_HOME/profiles/<p>/plugins.lock`, beside the profile definition and the profile
    patch, the two files it is restored with, so the snapshot of `08:374` is one
    profile's three files. In the table of decision 13 its path is
    `profiles/<p>/plugins.lock`. A profile's generation digest takes its own file whole.
    Install reads the other profiles' locks for the rule of decision 7. `07:1841` reads
    "Profile file".

17. **Two artifacts of one name never compose together** (Q2 (a)). Install composes the
    target profile with the artifact's bundle appended, as boot would, and refuses when
    the group id equals the id of any row of that composition, the group of the other
    artifact included. It names both, writes nothing and exits 1 with the reason (ADR-060
    decision 23). Two publishers' packs for one system never share a profile; the
    operator removes one or installs the other into another profile. A first-party row
    that a later release adds under a group's id is caught at boot by decision 6, which
    leaves the artifact's rows out and names the clash, and the operator then removes the
    artifact. "Two packs cannot collide" reads "two artifacts of one name never compose
    together: the second install is refused" in `08:391`, `07:1817` and community:298.
    Note of 2026-11-27 (review round over the whole step, round 2): "as boot would"
    includes the server rule of `08:156`. Install, enable, disable, remove and profile
    sanitize render a profile other than `cli` in server mode, and the gate composes it
    so at install; at `plugin check` and offline it composes a pack's bundle so, since a
    pack installs into server profiles only (ADR-061 decision 40). A pack row that
    states no roles is then denied by check 8 where the author and the installer see it.

18. **Boot checks what install kept against the locked digest** (Q3 (a)). At every
    composition, boot hashes `bundles/<plugin id>/artifact.json` and compares it with the
    digest in `plugins.lock`, then hashes `plugin.yaml`, `bundle.yaml` and the content
    tree in the form of decision 3 and compares each with its descriptor in that manifest.
    A mismatch detaches every row of the artifact as disabled and names check 5, since the
    files are not those whose signer install verified; the clause enters check 5 of the
    one gate text (ADR-060 decision 16). For in-process code, which has no lock entry, the
    same check starts from the digest that install recorded (ADR-020). ADR-020 then has
    only `plugins.lock` and the records of install to protect until boot.

19. **A row of an installed artifact's bundle sets only `id`, `plugin`, `roles`,
    `enabled: false` and `config`** (Q4 (a)). Check 8 denies `required`, `inject`,
    `intercept`, `isolate` and `group` on such a row, in the clause of decision 11; an
    operator who wants one sets it in the profile or site layer, which the operator
    writes.

20. **The media types and the annotation key take the prefix `aiops`** (Q5 (a)): the
    `artifactType` `application/vnd.aiops.plugin.v1` and the layer types
    `application/vnd.aiops.plugin.*` of decision 1, and the annotation key
    `aiops.plugin.content.paths` of decision 2. The key departs from the reverse domain
    notation that the image specification says annotation keys SHOULD use, since the
    documents name no domain of the project and only `aiopsctl` reads the key.

21. **`LICENSE.docs` of `go-digest` is recorded by name in L7** (Q6 (a)). CC-BY-SA-4.0 in
    `LICENSE.docs` of `github.com/opencontainers/go-digest`, for documentation that enters
    no artifact, is among the licences allowed with a recorded condition. The record is
    an entry of the allow-list, as the MCP composite is, and not an acceptance of a
    distribution; the scanner honours it, and check 6 and L16 cite it. A case of the
    scanner's tests under ADR-044 scans that module. `08:1336` and licence:202 hold the
    record.

22. **The artifact layout is frozen-additive from its first publication** (Q7 (a)), after
    L14 (`08:1343`). A later release may add a layer type, a media type or an annotation,
    and changes the layout otherwise only under a new `artifactType`, which a reader of
    `v1` refuses. `00:266` names it among the frozen-additive contracts, and the
    table of `08:912-920` gains the row "Artifact layout".

**Edits made with this record.** `00` is edited in the commit that records the answers
(`08:996`), and that commit makes the edits that the chosen options and the section
"Edits on acceptance" name: `00:266`; `02:313`, and `02:483` (item 26), settled by
decisions 13 to 15; `07:72` (R14), `:1650-1660` (the layout of decisions 1 and 3),
`:1690-1708` (a bundle with no `${env:}` reference, whose endpoint the operator sets in
the profile layer), `:1817` and `:1841`; `08:121`, `:391`, `:430`, `:1336` and the table
of `:912-920`; community:298; and licence:202. Two texts restate an edited one and are
edited with it: `07:546`, which restates `00:266`, and the table of community:146-154,
which restates that of `08:912-920`. Left to the commit that builds the gate and writes
the one gate text of ADR-060 decision 16: the clause of decision 11 with the fields of
decision 19, the boot rule of decision 6 and the check 5 clause of decision 18 in that
text, and the copies of checks 5 and 8 that the commit checks against it (ADR-060
decision 16 names those of check 8): `00:478`; `07:1318`, `:1386`, `:1816`, `:1866`,
`:1888` and `:1891`; and `08:143`, `:501` and `:504`. Left to the commit that marks the
ADRs accepted on 2026-11-23 in the ADR table of `08` §3: the row of ADR-047. An option
the owner did not choose edits nothing.

**In-process code has no entry in `plugins.lock`** (owner, 2026-11-23). Decision 8 gives
in-process code no entry in `plugins.lock`, and decision 18 starts its check of that code
from the digest that install recorded, since the code has no lock entry. As accepted,
ADR-020 decision 10 gave in-process code an entry that pins nothing, through which boot
found the record of a row with no origin artifact (ADR-020 decision 12). The owner was
asked which holds and answered on 2026-11-23: in-process code gets no entry, as
decisions 8 and 18, `07:1841` and `00:478` state, so decisions 8 and 18 stand as written.
ADR-020 decisions 10 and 12 change: boot finds the record of an in-process row by its
plugin id in the records install wrote under `$AIOPS_HOME/verified/`, whose
`record.yaml` carries the plugin id, and not through the lock. `07:1841` and `:1855`, the
row `Lock` and the relationship from `Install` to `Lock` in `07` §9, say that in-process
code has no entry; `00:478` locks only a remote plugin or a pack with no code and is not
edited for this point.

## S2b part: the artifact of remote code, its rows of the home and its network

- Status: **proposed; answers recorded 2026-11-27 and 2026-11-28, the runs of decision
  39 recorded 2026-11-27, all ten rows holding, and the runs after the answers recorded
  2026-11-28; it waits for the owner on questions Q19 to Q23.** This part is an addition to
  ADR-047, written as ADR-063 decision 2 has the S2b parts of accepted ADRs written, and
  it is accepted before the code it governs merges into `main`. The owner answered the
  seven questions, Q8 to Q14, on 2026-11-27, each with its recommended option, Q11 on
  its condition; the section "Owner answers of 2026-11-27" records the answers as
  decisions 42 to 48. By decision 48 the part stayed proposed until each row of decision
  39 had a recorded result. The section "Runs of decision 39, recorded 2026-11-27" holds
  the results: every row holds, and the condition of decision 45 is met. After the runs
  the part stayed proposed for two points. Decision 43 and the answer recorded on
  ADR-041 gave the directory of an instance different modes (Q15), and row 7 showed that
  the order of decision 33 lets an instance start on a network that is not the network
  of decision 32 (Q16). The owner answered both on 2026-11-28, with the two smaller
  points of the runs, Q17 and Q18, each with its recommended option; the section "Owner
  answers of 2026-11-28" at the end of this part records the four answers as decisions
  49 to 52. Its decision 53 adds to the endpoint table of decision 29 what the owner's
  answer of the same date to question Q18 of the S2b part of ADR-061 puts there. That
  record does not mark the part accepted, and it puts one question to the owner, Q19,
  which is needed before `sidecars-static` merges. The section "Runs after the answers
  of 2026-11-28" at the end of this part records the runs that those answers left. The
  systemd unit's check of an existing network (decision 50) holds in a service manager,
  and the bound of decision 51 is read: 5 s. The modes of decision 43 hold under
  rootless Podman and under SELinux enforcing once the bind carries a label. Four
  results come back to the owner as questions Q20 to Q23: the bind forms of decision
  39 cannot carry that label on Docker and Compose (Q20), the `z` option of decision
  14 relabels the credential directories with the home (Q21), under rootless Podman
  with the user kept the instance holds the site user's group (Q22), and on Podman
  the internal network gave the host an address in it, in one reading (Q23). The part
  is marked accepted after the owner's answers to Q19 to Q23 are recorded. Decisions 1
  to 22 stand as accepted; where a decision of this part adds to one of them, it names
  it.
  - No behaviour of a container runtime was run for the proposal of this part, and the
    project pins no container runtime yet. Decision 39 lists what was read, at which
    version, and what a run has to show; question Q14 asks whether the part is accepted
    before those runs, and decision 48 holds the answer: it is not. The runs were made
    on 2026-11-27, after the answers.
  - Values that no written line gives are proposed here, and the owner accepts them
    with the part: the depth of the SBOM of remote code (decision 26); the port 7443
    (decision 29); the path `/aiops/identity`, the numeric user 65532 and the absence
    of a restart policy in both templates (decisions 30 and 31); the options of the
    network of an instance (decision 32). Note of 2026-11-28: the record of that date
    proposes one more such value, the name and the form of the attempt value of a line
    of the endpoint table (decision 53).
- Scope: what ADR-063 decision 34 lists for this part, with what its decisions 25 and 27
  leave to it: the layer and media type of the image reference; the image and its digest
  (ADR-063 decision 61); the SBOM of remote code; the one row of a remote bundle (ADR-063
  decision 67); the unit files, the endpoint table and the credentials as rows of the
  home table of decision 13, with the one directory a unit binds; the network of an
  instance and of a kernel container, and how `aiopsctl` names the kernel container.
  - It blocks `plugin pack`, `push` and `install` of remote code, the rendered compose
    fragment and systemd unit, the endpoint table that `sidecars-static` reads, `plugin
    start`, and steps C2 and C3 of the S2b exit (ADR-063 decision 27).
  - ADR-063 decision 25 renders a unit from a fixed template and names no part for the
    constants of that template. This part fixes those that bear on the home and on the
    network: the paths, the one bind, the image reference, the user, the capabilities
    and the network (decisions 30 to 32). The variables of the environment that tell an
    entrypoint its listener and its credential directory are ADR-041's.
  - Settled by the other S2b parts and by ADR-041 (ADR-063 decision 34), and only named
    here: the form of each identity, the names of the files of a credential directory,
    their lifetime and rotation, the mechanism of the revocation and the command that
    makes a new key pair after one (ADR-041); whether `plugin.yaml` carries
    `entrypoint`, the grammar of that field and of the id, and what a manifest that
    lists both `inproc` and `remote` packs and installs (ADR-048); the record of a
    remote artifact, how `plugin verify` obtains and runs a pulled image and
    `allowUnsigned` for the runtime `remote` (ADR-020); `plugin start`, `plugin verify`
    and `plugin smoke` as commands, and the text of each refusal (ADR-061). Decision 40
    lists what this part asks of each.
  - This part changes no decision of `00-decisions.md`. Where an answer would edit a
    written line, the question names the line, and the section "Edits on acceptance of
    the S2b part" names the rest.
- Citations: in this part `00`, `02`, `03`, `07` and `08` are the files of
  `docs/architecture/` with those numbers, and `08:393` is line 393 of `08` at commit
  `bbfd839`, which holds the acceptance of ADR-063. The part above cites those files at
  `8e1f7b9`, so its line numbers differ. A path with a line, as
  `boot/manifest.go:582`, is a file of the repository at `bbfd839`. "Decision n" alone
  is a decision of this ADR, of the part above or of this one; a decision of another
  ADR carries its number. The plugin record is cited by section, and an outside module
  as `module@version`, read in that module at that version. A program that is run and
  not linked is cited by its repository and release tag, as `moby/moby` at
  `docker-v29.10.0`, read in the source tree of that tag. The S2b parts of ADR-020,
  ADR-048 and ADR-061 and ADR-041 are proposed beside this one and are cited by ADR
  number, as proposed on 2026-11-27; none is accepted, so each such citation holds
  only until that text changes.

### Context of the S2b part

ADR-063 fixes what S2b builds and leaves to this part the points that touch the artifact
and the home (ADR-063 decisions 25, 27 and 34). Reading its decisions against decisions 1
to 22 and the code at `bbfd839` leaves ten gaps:

- **No image layer.** The layer table of decision 1 has none: decision 1 says only that
  the image reference arrives at S2b "as one more layer type under the same
  `artifactType`". `plugin check`, `pack` and `install` refuse the runtime `remote`
  (`plugins/operations/internal/operations/author.go:248`, `artifact.go:90`,
  `install.go:122`).
- **An image that no line describes.** `plugin pack` builds the plugin image, "writes
  the image beside the artifact as an OCI layout and records the digest of that image",
  and `plugin push` "pushes the image and then the artifact" (ADR-063 decision 61). No
  line says which file is built, from which inputs, over which bytes the digest is
  taken, where the layout puts the image, or in which repository a unit finds it. An
  artifact is fixed at `pack` and signed before `push` names a registry, and a local
  path is a spec too (`07:1864`; decision 1).
- **`inspect` prints what it may not fetch.** Step C1 has `plugin inspect` print the
  image reference and fetch no image (ADR-063 decision 27), and decision 2 lets it fetch
  the artifact's manifest, `plugin.yaml` and the bundle only (`07:1865`).
- **An SBOM for two kinds only.** Every artifact carries an SBOM (`08:393`), and decision
  1 says what it lists for a pack and for in-process code.
- **One row, of which plugin.** The bundle of a remote artifact holds exactly one row
  (ADR-063 decision 67; `08:506`), and the row of an installed remote artifact names the
  artifact's own id (ADR-063 decision 41), while check 8 also admits a row of an
  instantiable first-party plugin (decision 11).
- **No row of the home for a remote plugin.** Install renders a unit, writes "a line in
  the `sidecars-static` table" (`08:1293`; `02:313`) and makes key pairs (ADR-063
  decision 55; `00:412-413`). The table of decision 13 has a path for none of them, and
  the endpoint table has no file, no format and no name. Its reader in the kernel is a
  row that injects nothing (`03:113`).
- **One owner, two users.** Decision 14 gives the home one owner and every file the mode
  0600. A unit names "a user that is neither the site user nor root" and binds one
  directory of the home, the one that holds its instance's credential (ADR-063 decision
  25). No line says how that user is chosen, and the code holds an existing home to its
  owner only: it creates a missing home with mode 0700 and never reads the mode of one
  that exists (`plugins/operations/internal/operations/home.go:167-195`;
  `internal/serve/serve.go:216-231`).
- **No network in the container form.** Decision 15 names no network, and the S2a exit
  starts the kernel with `--network none` (`test/authoring/exit_test.go:1063`). At S2b
  `plugin start` creates the network of an instance and attaches the instance and the
  kernel container to it; how `aiopsctl` names that container, and what the same step
  does for a kernel that runs as a binary on the host, are left to this part (ADR-063
  decision 27). The runtime of an S2a environment runs with no bridge and no packet
  filter (`test/authoring/env_test.go:134-137`).
- **No stop.** `plugin disable` stops a shared unit when no enabled row of the site
  names its plugin, and `remove` deletes what install rendered (ADR-063 decision 68;
  `07:1195`; `03:495`). No line says what becomes of the container, of the network and
  of the kernel's attachment.
- **No way to the container runtime.** The operations library runs no program: every
  outside tool is a part of `Tools`, which answers `ErrNotCarried` in `aiops`
  (`sdk/def/launch/v0/toolbox.go:18-20`, `:44-64`). The one part that runs a container
  runtime is the builder (`internal/builder/image.go:417`).

### Decision of the S2b part

#### The artifact of remote code

23. **The image reference is one more layer.** An artifact of remote code, one whose
    `plugin.yaml` lists the runtime `remote` (`07:134`), carries the layer below under
    the same `artifactType`, after the module layer in the order of decision 1 (ADR-063
    decision 17):

    | Layer | Title annotation | Media type | Present in |
    |---|---|---|---|
    | The image reference | `image.json` | `application/vnd.aiops.plugin.image.v1+json` | Remote code |

    - The prefix is that of decision 20. The layer is one JSON object with exactly three
      members, named as the three required properties of an OCI content descriptor
      (`github.com/opencontainers/image-spec@v1.1.1`, `descriptor.md`): `mediaType`,
      always `application/vnd.oci.image.manifest.v1+json`; `digest`, `sha256:` and 64
      lower-case hexadecimal digits, the digest of the plugin image's manifest (decision
      24); and `size`, the length of that manifest in bytes. `aiopsctl` writes it with
      the standard library's `encoding/json`.
    - It names no registry, no repository, no tag and no URL. An artifact is fixed by
      `plugin pack` and signed before `plugin push` names a registry (`07:1864`), so it
      cannot hold one, and no host name from a third party's artifact reaches a unit
      (ADR-063 decision 25). Decision 31 says which repository a unit names.
    - The layer's descriptor in the artifact's manifest carries the annotation
      `aiops.plugin.image.digest`, set to the same digest, as the content layer's
      descriptor carries its paths (decision 2). The manifest that `plugin sign` signs
      then holds the image digest itself, `plugin inspect` prints it with no other
      fetch (decision 25), and the kept `artifact.json` of decision 5 holds it for
      `plugin start` (decision 36).
    - `plugin pack` writes the layer exactly when `runtimes` holds `remote`. Install
      refuses, before it writes anything: an artifact whose manifest lists `remote` and
      that has no such layer; one that has the layer and does not list `remote`; a layer
      that is not the closed form above, read under the bound that the library sets on
      `plugin.yaml` (`plugins/operations/internal/operations/oci.go:146-153`); and an
      annotation that differs from the layer. What an artifact carries whose manifest
      lists both `inproc` and `remote` is the S2b part of ADR-048 (ADR-063 decision 34).
    - A reader that does not know the type ignores it and fetches no blob of it
      (decision 1), so an `aiopsctl` of S2a reads such an artifact as far as its own
      refusal of the runtime `remote`. Boot does not read the layer: it reads the kept
      files of decision 5 and ignores a layer of a type it does not read
      (`boot/install.go:79-80`). A layer type and an annotation are what decision 22
      lets a release add, and nothing is published before L14 (`08:1349`).

24. **The plugin image and its digest** (ADR-063 decision 61).
    - **The build file** is `Containerfile` at the root of the plugin directory, a name
      that names no runtime. Each remote template writes it (ADR-063 decision 19), and
      `plugin pack` refuses remote code that has none.
    - **The container runtime at hand** is the program `docker` of the caller's `PATH`,
      the one that the image path of the builder already runs
      (`internal/builder/image.go:417`), reached through the part `Image` of decision
      38. Another runtime qualifies when it takes the same commands and gives what
      decision 39 lists. Without one, `plugin pack` of remote code refuses and names
      what it lacks, and step 7 of `plugin check` is reported as not run (`07:1570`;
      ADR-063 decision 17).
    - **The inputs of the build** are the build file and the build context, and nothing
      else. `plugin pack` passes no build argument, no secret, no key of the caller and
      no second context. The context is the plugin directory without the directory that
      this pack writes and without any directory that holds an `oci-layout` file, so
      the default output `artifact/` (decision 1) never enters an image and a second
      pack builds from the same bytes as the first.
    - **What the build file names is the author's.** The build has the network that the
      runtime gives a build, and `plugin pack` adds no option to it. A base image and a
      package source are resolved as the `Containerfile` names them: the Python
      template names its base image by digest (ADR-063 decision 19), and an author
      whose machine reaches a registry and a package directory of its own, as in steps
      G2 and P2 of the exit (ADR-063 decision 27), writes those into the file. `plugin
      pack` rewrites no line of it.
    - **The Go template compiles nothing in the build.** Its `Containerfile` copies one
      static binary onto no base layer (ADR-063 decision 19), so the binary exists
      before the build. Which command compiles it is the S2b part of ADR-061, with the
      form of a remote Go plugin and step 4 of `plugin check` (ADR-063 decisions 34 and
      60); this part fixes that it is compiled for the platform of the build.
    - **One image manifest, one platform.** The platform is that of the runtime that
      builds: `plugin pack` reads it from the runtime and names it to the build, and it
      asks for no attestation of the build, since a build that attaches one exports an
      index and not one manifest (decision 39, row 1). It refuses an export that is not
      exactly one `application/vnd.oci.image.manifest.v1+json`: an image index, or a
      manifest of another type. The S2b exit runs linux/amd64 alone (ADR-063 decision
      27). An image for several platforms arrives with its first consumer (`08:680`),
      in a form that decision 22 lets a release add.
    - **What pack does.** It runs the runtime's build on that file and that context,
      takes the result out of the runtime as an OCI image layout
      (`github.com/opencontainers/image-spec@v1.1.1`, `image-layout.md`) and copies the
      image manifest, its config and its layers into `blobs/sha256/` of the layout of
      decision 1. That is "the image beside the artifact as an OCI layout": one
      directory holds the artifact and its image, so `--out`, `plugin sign`, `plugin
      push` and a local-path spec name one directory, as at S2a. Whether `index.json`
      also names the image manifest is question Q13. `plugin pack` creates no tag in
      the store of the runtime, and no command reads the image from that store: what a
      build leaves there is the runtime's own cache.
    - **The digest** is the SHA-256 of the bytes of that image manifest as the layout
      holds them. It is never an identifier that the runtime's own store gives an
      image. `plugin push` copies those bytes unchanged (decision 25), so the digest in
      the artifact is the digest under which the registry serves the image (ADR-063
      decision 27, step G2) and by which the runtime of the clean machine pulls and runs
      it (step C3).
    - **What does not change.** `plugin sign` signs the artifact's manifest and nothing
      else: the image has no signature of its own, because the artifact's own field pins
      it (ADR-020 decision 9). `plugins.lock` keeps the form of decision 8: the lock
      names the artifact's digest, which fixes `image.json`, so a generation digest pins
      the image through it (`08:185`). The blobs of the image are copied as streams by
      `pack` and `push` and by no other command, so the bounds that the library sets on
      a blob it reads into memory do not bind them
      (`plugins/operations/internal/operations/oci.go:46-47`).

25. **What each command does with the image.** This adds to decision 2.
    - `plugin push` copies the image manifest and every blob it names to the repository
      that the command names, then the artifact, then what `sign` added (ADR-063
      decision 61). A registry therefore holds no artifact of remote code without its
      image, and holds both in one repository. Whether the image gets a tag there is
      question Q13.
    - `plugin inspect` fetches what decision 2 lists and prints the image reference from
      the annotation of decision 23: the repository of the spec with the digest, or the
      digest alone for a local path. It fetches neither `image.json` nor a blob of the
      image, so `07:1865` stands as written (ADR-063 decision 27, step C1).
    - `plugin gate --against` fetches what decision 2 lists.
    - `plugin install` fetches `image.json` beside what decision 2 lists and makes the
      checks of decision 23. It fetches no manifest, config or layer of the image, asks
      the registry nothing about them, pulls nothing and runs nothing (ADR-063 decision
      17; step C2). An image that the repository does not hold shows at `plugin start`,
      as the runtime's own error.
    - `plugin verify` fetches of the artifact what install fetches, with the fixtures
      of decision 2. The image is not a blob of the artifact: `plugin verify` has the
      container runtime pull it, by the reference that a unit names (decision 31), only
      after the signature and the attestation hold, and the runtime checks what it
      pulls against that digest. How the command runs that image, and what fences the
      container, are the S2b parts of ADR-020 and ADR-061 (ADR-063 decision 34).
    - Step 7 of `plugin check` packs into a directory of its own, which builds the
      image, verifies the signature and runs the gate on that layout, and removes the
      directory (decision 1; `07:1580`). It pulls no image and runs none.
    - `plugin smoke` starts from a plugin directory and packs it (`07:1583`; ADR-063
      decision 17). What it does with the result is the S2b part of ADR-061.
    - A local-path spec names no repository, so neither `plugin install` nor `plugin
      verify` nor `plugin smoke` finds there an image that a runtime can pull. What
      each does is question Q8.

26. **The SBOM of remote code** has the form of decision 1: an SPDX 2.3 document in JSON
    that `aiopsctl` writes itself with `encoding/json`, so it adds no linked module and
    ADR-060 decision 34 asks nothing more. ADR-063 decision 34 leaves its content to
    this part, and no written line defines it, so its depth is a choice that the owner
    accepts with the part.
    - It lists the plugin image as one package: the plugin id and version, and the
      digest of the image manifest as its checksum. Its declared licence is the
      manifest's `license`, with the comment that this is what the publisher declares
      for the plugin's own code; its concluded licence is `NOASSERTION`. Under it, it
      lists the image's config and each layer as files, each with the SHA-256 and the
      size that the image manifest gives it and with `NOASSERTION` as its licence.
    - It is not an inventory of what the image contains. It lists no package of an
      operating system or of an interpreter: `aiopsctl` unpacks no image layer and links
      no scanner of image contents, as the builder writes its own SBOM and holds no SBOM
      tool (ADR-044 decision 15).
    - That is enough for an image that a third party conveys: the licence scan of the
      gate is over code linked into a binary (check 6, `08:504`), and a remote plugin is
      a separate program whose own licence is unrestricted (L16, `08:1351`).
    - It is not enough for a plugin image that the project itself conveys. Such an
      image falls under L5 (`08:1340`: a published image needs an SBOM and the
      corresponding source, or a written offer, for every GPL, LGPL and AGPL package
      in its layers) and under L13 for its base layers (`08:1348`), and ADR-063
      decisions 35 and 52 hold what a shipped artifact links, and the dependency set
      of `aiops-sdk`, to L7 (`08:1342`); licence:68 lists the Python gRPC runtime as
      "ship in a plugin image". The SBOM of this decision meets none of them. S2b
      conveys no plugin image: the images of the exit and of the examples exist only
      inside the test (ADR-063 decision 32), and nothing is published before L14
      (`08:1349`). The inventory of a first-party plugin image is written before the
      first such image leaves the test, in the ADR of that step, with the register
      entries of its base image and of its packages in the forms that the S2b part of
      ADR-054 fixes (ADR-063 decision 34). The base image of the Python template is
      read under ADR-063 decision 35 before the template merges, whatever this SBOM
      lists.
    - The rule is by layer: the files of a content layer and of a fixtures layer, the
      build list of a module layer (decision 1), and the image of an image layer.

27. **The bundle of a remote artifact holds exactly one row, the proxy row** (ADR-063
    decisions 41 and 67). This narrows decision 11 for remote code.
    - Its `insert` items hold, together, exactly one row, and that row's `plugin` is the
      artifact's own id. The other plugin that decision 11 admits, a first-party plugin
      marked `instantiable`, is not open to it: a remote artifact with no row of its own
      plugin would start an instance that no row dials.
    - The row sets the fields of decision 19 and no other. It states its roles (`08:157`),
      and only replicas of those roles dial the instance (`03:571`). It mounts under the
      group of decision 5, so its id is `<name>:<row>`, the id that `plugin enable`
      takes.
    - `plugin check` refuses a remote bundle with no row, with a second row or with a
      row of another plugin, and names the rows. The gate denies each of the three
      under check 8 at every entry point that evaluates check 8 (ADR-060 decision 17).
      The clause of `08:506` says only "inserts another"; the commit that builds the
      clause writes all three into the one gate text (section "Edits on acceptance of
      the S2b part").
    - Several rows of one remote plugin are the work of S4b, where the question can be
      reopened with the replicas (ADR-063 decision 67).

#### The home

28. **The rows of the home that a remote plugin adds.** This adds to the table of
    decision 13.

    | Path under `$AIOPS_HOME` | Written by | The kernel mounts it | Source |
    |---|---|---|---|
    | `sidecars.json`, the endpoint table (decision 29) | `aiopsctl` `install`, `remove`; and `plugin start`, which writes a missing line (ADR-061 decisions 45 and 60, its question Q10 (a), answered 2026-11-27); and `plugin enable`, which replaces the attempt value of a line (decision 53, recorded 2026-11-28; ADR-041 decisions 18 and 42) | Read-only; the row `sidecars-static` reads it | `08:1293`; `02:215`; ADR-063 decisions 17 and 39 |
    | `remote/units/<plugin id>/`: `compose.yaml`, the compose fragment, and `<unit name>.service`, the systemd unit (decision 31) | `aiopsctl` `install`, `remove` | Read-only, and the kernel does not read it: the host starts the instance from it | ADR-063 decisions 25 and 50 |
    | `remote/kernel/`: the key pair and the certificate of the kernel | `aiopsctl` `install`, on first need; what deletes it is ADR-041's | Read-only; the proxy reads it | ADR-063 decisions 23 and 55; `00:413` |
    | `remote/pins/`: the pin of the certificate of each installed instance, one entry per plugin id | `aiopsctl`: `install` writes it, `disable` and `remove` delete it; the command that writes a pin again after a revocation is ADR-041's | Read-only; the proxy reads it at every dial | ADR-063 decision 55; `00:412` |
    | `remote/instances/<plugin id>/`: the key pair and the certificate of that instance, and the certificate of the kernel | `aiopsctl` `install`; `remove` deletes it; the command that replaces a revoked pair is ADR-041's | Read-only, and the kernel does not read it: it is the one directory that the unit of that instance binds (decision 30) | ADR-063 decisions 25 and 55 |

    - A plugin id holds one `/`, so `<plugin id>` is two path segments, as under
      `bundles/` and `content/` (decision 5).
    - Every row is of the site and not of a profile: a site holds one digest and one
      shared instance per plugin id (decision 7; `08:388`).
    - Whichever command writes one of them writes it through the operations library,
      under `write.lock` and with the snapshot of decision 13 (ADR-061 decisions 17 and
      19). The kernel writes none of them: it still writes nothing outside `run/`.
    - This part does not close the list of writers. ADR-041, as proposed, has `plugin
      start` make a new key pair and write the pin for a plugin that has none, and the
      S2b part of ADR-061, as proposed, has `plugin start` write a missing line of the
      endpoint table. The edits on acceptance write into this table what the owner
      accepts there. A revoked certificate is never pinned again: ADR-063 decision 23
      has the kernel refuse a stream that the old credential opens on every later dial,
      so whatever restores a pin after a revocation makes a new pair.
    - The kernel's private key is in `remote/kernel/` and nowhere else, and
      `remote/instances/<plugin id>/` holds no private key but that instance's, so the
      kernel's key is in no file that a unit gives an instance (ADR-063 decision 23).
    - The names of the files in the three credential directories, their form, their
      lifetime and their rotation are ADR-041's (ADR-063 decisions 23 and 34). This
      part fixes the directories and who may read them.

29. **The endpoint table** is `$AIOPS_HOME/sidecars.json`: one JSON object with
    `tableVersion`, which is 1, and `endpoints`, a list sorted by `name` of objects with
    the two members `name` and `address`.
    - It is JSON and not YAML because its reader in the kernel is a row, a plugin under
      `plugins/` that injects nothing (`03:113`): it reads the file with the standard
      library and adds no importer to the register entry of the YAML library
      (`tools/upstream-components.yaml:124-132`).
    - A home with no such file has an empty table. `dev` lists `aiops-selfhost`, and
      with it the row `sidecars-static`, before any remote plugin is installed (ADR-063
      decisions 22 and 39), so the row reads an absent file as no endpoint and not as
      an error. The first install of remote code creates the file.
    - `aiopsctl` writes the file whole and replaces it by a rename (decision 14). The
      home is bound as a directory (decision 15), so a running kernel container sees
      the new file.
    - `name` is, for a remote plugin, its plugin id. A name that holds `/` is a plugin
      id and never the role name of an upstream process, such as `harness-sidecar`,
      which holds none (`00:664`; `02:310`). Those names enter the table with their
      plugins.
    - `address` is `<host>:<port>`. For an instance that a unit starts, the host is the
      unit name of decision 31 and the port is 7443, the one port on which every unit
      has its instance listen (decision 32).
    - Install writes the line with the unit (`08:1293`). Between install and `plugin
      start` the endpoint is therefore listed and does not answer, and `plugin enable`
      fails by the server's verdict; `aiopsctl` reads this file for its own refusal of
      an endpoint that is missing (ADR-063 decision 17; step C2). `remove` deletes the
      line (decision 35).
    - How the row learns the path of the file, when it reads it again and what it calls
      ready are the row's, with the `sidecars` definition (ADR-063 decision 39).
    - Note of 2026-11-28 (decision 53). An object of `endpoints` gains a third member,
      `attempt`, and `plugin enable` is a writer of the file. When the row reads the
      file again is no longer left to the row: by the owner's answer to question Q18 of
      the S2b part of ADR-061 this decision carries that rule, and decision 53 words it.
      How the row learns the path of the file and what it calls ready stay the row's.

30. **The one directory a unit binds, and the user of a unit.** The unit of an instance
    binds `remote/instances/<plugin id>/` of the home, read-only, at `/aiops/identity`
    in the container: the same path in every unit, under no path of the home, so that
    no path inside an instance is a path of the home.
    - It binds nothing else: no other path of the home, not `run/`, no socket of the
      container runtime and no other path of the host (ADR-063 decision 25).
    - The directory is bound, never its files one by one, for the reason of decision 15.
      On a host with SELinux enforcing the bind needs the label of decision 14.
    - **The user is a number of the template.** Both templates name the user as the
      numeric pair `65532:65532`, written by the template and never taken from the
      artifact or from a name of the image (ADR-063 decision 25). With question Q9 (b)
      the second number is the group of the site user.
    - **"Neither the site user nor root" is judged as the host sees it** (ADR-063
      decision 25). The reload socket admits a peer by the uid that the host sees
      (ADR-062 decision 5), and decision 14 makes the identity of a container a
      property of the runtime's user mapping. Install refuses to render a unit when the
      uid of the site user is 65532, and names both: under a mapping that changes no
      uid the instance would run as the site user. `plugin start` reads from the
      runtime the uid under which the host sees the first process of the instance, and
      where that is 0 or the uid of the site user it stops the instance, refuses and
      names the uid (decision 36).
    - **No capability and no privilege gain.** Both templates drop every capability and
      forbid a process of the instance to gain a privilege, so the user of the unit
      stays that user. The instance listens on a port above 1023, which needs no
      capability.
    - Decision 14 gives every file of the home the mode 0600 and the site user as its
      owner. How the user of a unit reads this one directory is question Q9. Whatever
      the answer, decision 14 holds for every other path, and the three directories
      above this one hold to the rule below.
    - **The directories above it are closed, and that is checked.** `aiopsctl` creates
      `remote/`, `remote/instances/` and `remote/instances/<owner>/` with mode 0700.
      Install of remote code and `plugin start` read the owner and the mode of the home
      and of these three: they refuse one that the site user does not own, and they set
      one of another mode to 0700 and say so, as decision 14 has `run/` set. A home
      made by hand or carried over from S1 with a wider mode is thereby closed before
      a key is written into it or read from it. The bind does not need those
      directories open: the container sees the bound directory at its own path.

31. **The unit files, and the image a unit names.**
    - **The unit name** is formed from the role name of the container, `remote-plugin`
      (`00:138`, `:664`), and the two segments of the plugin id. It names the container
      of the instance, its network and its host on that network (decision 32), and the
      systemd unit file, so it has to resolve as one DNS label of at most 63 bytes. The
      S2b part of ADR-048, as proposed, bounds a segment of an id at 128 bytes and
      leaves the naming of the unit to this part. How a name of 63 bytes is formed from
      an id that may be longer is question Q12.
    - **Both targets are rendered at install** (ADR-063 decision 50), into
      `remote/units/<plugin id>/`: `compose.yaml` and `<unit name>.service`. They name
      the same container and the same network, so one of them runs at a time, which is
      the one shared instance of `08:388`.
    - **The compose fragment** is a Compose file of one service and one network.
      `plugin start` runs it with the Compose plugin of the program `docker` (`docker
      compose`), which is "what `plugin start` needs" on the clean machine (ADR-063
      decision 27): that machine holds `aiopsctl`, the program `docker` with its
      Compose plugin, and a daemon that creates the network of decision 32. Compose is
      read and pinned before its first use (ADR-063 decisions 35 and 50; decision 39).
    - **The systemd unit** is a unit of the system's service manager that root
      installs by copying the file out of the home into the manager's own directory
      and enabling the copy (`08:1293`: "a unit and `systemctl enable --now`"). It is
      never enabled by its path in the home and never linked from it: the home belongs
      to the unprivileged site user, and a file that root's service manager runs must
      not be one that this user can rewrite. `aiopsctl` runs no command of the service
      manager. The unit creates the network where it is absent and runs the same image
      by digest through the program `docker` (ADR-063 decision 25). After a new install
      the operator copies the new file again; `plugin start` refuses to attach an
      instance that differs from the rendered unit (decision 36). Note of 2026-11-28
      (the owner's answer to question Q16, decision 50): where a network of the unit
      name exists, the unit checks, before it runs the container, that the network is
      internal and that its gateway mode is `isolated`, and it fails to start on a
      network that is not.
    - **Neither template restarts a container by itself at S2b.** The fragment sets no
      restart policy and the unit file no restart directive, so a container that
      `aiopsctl` stops through the runtime stays stopped (decision 37). After a reboot
      of the host the operator starts the instance as the first time, or the service
      manager starts an enabled unit. A restart policy arrives with `deploy/compose` at
      S7, where the kernel is a compose service (ADR-063, Context).
    - **The image reference** of a unit is `<registry>/<repository>@sha256:<digest>`:
      the repository from which install read the artifact, and the digest of
      `image.json`. It holds no tag (ADR-063 decision 25). That repository is where
      `plugin push` put the image (decision 25). The runtime pulls it with the registry
      credentials of the site, which the orchestrator holds and the kernel never does
      (`00:416`). A site that copies artifacts into a registry of its own copies the
      artifact and its image into one repository and installs from there; question Q13
      says what such a copy needs. Of the three inputs that ADR-063 decision 25 lets an
      artifact give a unit, the image reference by digest is thus the digest that the
      signed artifact pins; the repository is the operator's, through the spec.
    - **A local-path spec** names no repository. What install of remote code does with
      one is question Q8.

#### The network

32. **The network of an instance** (ADR-063 decision 24).
    - Each instance has one network of the container runtime, named by the unit name
      and created by the unit: the compose fragment declares it, and the systemd unit
      creates it before it starts the container.
    - It is an internal network in which the host holds no address. On the engine read
      for this part (decision 39) those are two settings of a bridge network: the
      network is internal, and its gateway mode is `isolated` for IPv4 and for IPv6,
      with IPv6 off. The second matters: on an internal network in the default mode
      that engine gives the bridge of the host the gateway address of the network, an
      address of the host inside the network (decision 39, row 4).
    - Its members are the instance and the kernel container of decision 33, and nothing
      else. The instance joins no other network and publishes no port. Its name on the
      network is the unit name, and it listens on port 7443 of that network alone.
    - What the network has to give, each point asserted on the runtime of the exit
      (ADR-063 decision 27, step C3; decision 28), from inside the instance:
      1. no connection reaches an address outside the network, the registry or another
         instance;
      2. no connection reaches a listener of the host, on the gateway address of the
         network or on any other address of the host;
      3. the runtime's resolver answers for no name outside the network;
      4. of the kernel the instance reaches only the stream that the kernel opened.
    - Point 4 holds at S2b because the kernel listens on no network: the row
      `reload-socket` listens on a Unix socket in `run/` (ADR-062 decision 3), and no
      row that S2b ships opens a network listener. The step that builds the first row
      with a network listener keeps that listener off the networks of instances, or
      shows that an instance cannot reach it, in the ADR of that step.
    - Decision 39 says what was read of points 1 to 3 and what a run has to show. The
      runtime of an S2a environment runs with no bridge and no packet filter
      (`test/authoring/env_test.go:134-137`), and ADR-063 decision 27 changes it only
      as far as this network needs, so the run is made on the settings that the clean
      machine of the exit then has. A point that the runtime of the exit does not give
      stops the work and goes to the owner (ADR-063 decision 33); it is never recorded
      as built.
    - The fence does not rest on the runtime of the exit alone: on every host `plugin
      start` checks the network it uses and refuses one that is not this network
      (decision 36).
    - A remote manifest that lists a `network` entry is refused at install (ADR-063
      decision 56), so every unit of S2b has this network and no other.

33. **The kernel container.**
    - **How `aiopsctl` names it.** It gives the kernel container no name and reads none
      from a file: it finds it. The kernel container of a home is the running container,
      on the runtime that `plugin start` uses, that has a read-write bind whose source
      and target are both `<home>/run`. That is the form of decision 15, and no other
      container has it, since one server process runs on a home until S4b (ADR-062
      decision 2, note of 2026-11-26). The form of decision 15 therefore needs no new
      flag, label or file, a kernel container started as at S2a is found, and two homes
      on one host name two containers. Where two running containers have that bind,
      `plugin start` refuses and names both.
    - **Its networks at S2b.** ADR-063 decision 27 has the kernel container start with
      no network, as at S2a (`test/authoring/exit_test.go:1063`), and `plugin start
      <id>` attach it to the network of that instance. On the engine read for this
      part the attachment of a container that started in the no-network mode is
      refused as it stands (decision 39, row 6), so how the kernel container joins is
      question Q11. At S2b its only networks are those of instances. That is a state
      of S2b and not a standing rule: the step that gives the kernel a network of its
      own, S3 with the database, writes that network into decision 15.
    - The kernel and the instances are containers of one runtime (ADR-063 decision 27).
    - **What `plugin start` does,** in this order, after any write that ADR-041 or the
      S2b part of ADR-061 gives it:
      1. It reads the rendered fragment of the id and refuses one whose image digest
         is not the digest of the annotation `aiops.plugin.image.digest` in the kept
         `artifact.json`, read against the lock as decision 18 reads it. A unit is
         thereby read against the artifact at every start.
      2. It looks for a container of the unit name. With none, it runs the fragment,
         which creates the network and the container. One that runs and agrees with
         the rendered unit in its image reference by digest, its user, its one bind
         and its network is left as it is, whichever target started it. One that is
         stopped or differs is replaced by the run of the fragment when the fragment
         made it, and is refused, with the difference named, when it did not.
      3. It makes the checks of decision 36 on the network and on the instance.
      4. It finds the kernel container and attaches it to the network unless it is
         attached, as question Q11 decides.
    - Note of 2026-11-28 (the owner's answers to questions Q16, Q17 and Q18; decisions
      50 to 52). Before step 2, `plugin start` reads a network of the unit name that
      already exists and refuses one that fails a network check of decision 36, so the
      fragment never runs on such a network; the checks of step 3 follow as written. In
      step 2, "replaced by the run of the fragment" reads "started again or replaced by
      the run of the fragment": a stopped container of an unchanged fragment is started
      again as the same container. After an attach of step 4 that takes the kernel
      container out of the no-network mode, `plugin start` waits before it returns.
    - **It is idempotent.** A second run starts nothing and attaches nothing. A kernel
      container that the operator starts or replaces after the instance, as after
      `dist use`, is attached by the same command, which is the exact command that
      `plugin enable` prints (ADR-063 decision 17).
    - **On the systemd target** the service manager starts the instance, and the
      kernel container joins by the same command: after `systemctl enable --now` the
      operator runs `aiopsctl plugin start <id>`, which finds the instance running
      from the rendered unit, starts nothing, makes the checks of decision 36 and
      attaches the kernel container. Without that step the instance is on a network
      that no kernel has joined, and no row dials it. The systemd job of ADR-063
      decision 50 runs it and asserts, with the points of that decision, that the
      network holds the instance and the kernel container and nothing else and that
      `plugin enable` then passes as in step C3.
    - **Where it finds no kernel container,** `plugin start` starts the instance,
      attaches nothing and says that no kernel container of this home runs on this
      runtime and that the same command attaches one that starts later.
    - **Names are per runtime, not per home.** A container of the unit's name that does
      not bind `remote/instances/<plugin id>/` of this home is not this site's
      instance: `plugin start` refuses and names it.
    - Until `deploy/compose` exists (S7; ADR-063, Context) the operator starts the
      kernel container by hand in the form of decision 15. The step that builds
      `deploy/compose` says how a kernel that is a compose service joins the networks of
      instances.

34. **A kernel that runs as a binary on the host** is question Q10. Whatever the answer,
    no unit gets a wider network than decision 32 gives it, and the server dials only
    the address that the endpoint table names.

#### Install, start, stop and remove of remote code

35. **The order of the writes.** This adds to decision 10.
    - Install writes `remote/units/<plugin id>/`, `remote/instances/<plugin id>/`, the
      pin, `remote/kernel/` on first need and the line of the endpoint table before it
      changes the lock and the bundle list, in the one write of the home that holds the
      kept files of decision 5. A failure that restores the snapshot therefore leaves no
      lock entry without its files, and a denial writes nothing (ADR-063 decision 17).
    - Install into a second profile of an id that the site holds at the same digest
      reuses the unit files, the directory of the instance and the line of the endpoint
      table and writes none again (decision 7). Where the pin is absent then, because a
      `disable` in the first profile revoked it, that install restores none: it never
      pins the revoked certificate again, and the row of the second profile cannot be
      enabled until the command that ADR-041 names has made a new pair.
    - Where `disable` or `remove` revokes the credential (ADR-063 decision 68), it
      deletes the pin first, in the changes that precede the submission that unloads
      the proxy (ADR-063 decision 23; `00:412`). When no profile names the id, `remove`
      deletes the unit files, the directory of the instance with its key, and the line
      of the endpoint table, with the kept files of decision 10 and under its note of
      2026-11-23: only after a final result that shows the change, or when no process
      runs the profile (ADR-063 decision 68).
    - `remote/kernel/` is the site's and not a plugin's, and its lifetime is ADR-041's.
      As proposed there, the `remove` of the site's last remote plugin deletes it;
      where that is accepted, the deletion follows the order above, after the files of
      the plugin.
    - A file that a write deletes lies in `snapshot/` until that write drops its
      snapshot (ADR-061 decision 17). A key is deleted once the command has returned
      without a restore.

36. **What `plugin start` requires of the runtime it finds.** ADR-063 decision 24 has
    install render no unit whose instance has a wider network than its manifest lists.
    A rendered file does not fence an instance: the runtime does, and an operator's
    runtime is not the runtime of the exit. So `plugin start` checks what it uses, on
    every host and on both targets, and refuses what it cannot show. It never trusts an
    object of the runtime by its name.
    - **The network.** Before it attaches the kernel container, it reads the network of
      the unit name as the runtime reports it and refuses, naming the network and the
      point: a network that is not internal; one in which the host holds an address;
      and one with a member that is neither the instance nor the kernel container of
      this home. After the attachment it reads the members again. Note of 2026-11-28
      (the owner's answer to question Q16, decision 50): where a network of the unit
      name exists before the fragment runs, the three points are read on it first, and
      a refusal then starts nothing.
    - **The instance.** It never keeps a running container of the unit name whose
      image reference is not the reference by digest of the rendered unit, whose user
      is not the user of the template, or whose binds are not the one bind of decision
      30: such a container is replaced or refused as decision 33 says, with the
      difference named. A container left from an earlier version of the plugin
      therefore never serves: a new version arrives by `remove` and `install` (ADR-063
      decision 69), and `remove` stops the old container (decision 37).
    - **The user as the host sees it,** as decision 30 says.
    - **The directories** of decision 30, their owner and their mode.
    - A refusal after the fragment ran stops what this run started and leaves the
      kernel container unattached. Each refusal is a case of the tests (decision 41),
      and its text is the S2b part of ADR-061.
    - On a runtime where a check cannot be made, because the runtime does not report
      the point, `plugin start` refuses. An operator who runs an instance on another
      runtime by hand, from the rendered file, is outside these checks: the fence of
      that instance is then the operator's.

37. **Stopping a unit and removing it.** `plugin disable` stops a shared unit when no
    enabled row of any profile of the site names its plugin, and `remove` deletes what
    install rendered when no profile names the id (ADR-063 decision 68; `07:1195`;
    `03:495`).
    - **The order of `disable`** is the one of ADR-063 decision 23: the pin is deleted,
      the generation that unloads the proxy is submitted, and only after a result that
      shows the change is the unit stopped. A refused or failed generation restores the
      pin and stops nothing.
    - **Stopping** is three steps through the container runtime, on both targets: the
      container of the unit name that binds `remote/instances/<plugin id>/` of this home
      is stopped and removed; the kernel container of this home is detached from the
      network of the unit; and that network is removed. A later `plugin start` creates
      all three again. The kernel container is detached before the network is removed,
      since a network with a member cannot be removed.
    - **`remove` of an id whose unit still runs** stops it in the same three steps
      before it deletes the unit files, so no container outlives the files that
      describe it.
    - **On the systemd target** the same three steps stop the instance, and the unit
      does not start it again by itself (decision 31). The copy of the unit file that
      root installed stays enabled in the service manager: removing a unit from an
      orchestrator stays the operator's (ADR-063 decision 68), and `disable` and
      `remove` say so where the container they stopped was not made by the fragment.
      An enabled unit that the service manager starts again holds a revoked key and
      opens no stream (ADR-063 decision 28).
    - **Where the command cannot reach a container runtime,** as in a server process,
      whose toolbox carries no such part (decision 38), the revocation stands and the
      unit is not stopped; the command says so (`07:1195`: `disable` "run on the host
      also stops it").
    - The kernel container keeps no attachment to the network of a plugin that is
      disabled or removed. A kernel container that the operator replaces starts with no
      attachment at all and gains each by `plugin start` (decision 33).

38. **The parts of the toolbox that reach the container runtime.** The minor that
    `launch` gains at S2b adds two parts to `Tools`
    (`sdk/def/launch/v0/toolbox.go:44-64`). `cmd/aiopsctl` implements both, each
    answers an error that matches `ErrNotCarried` in `aiops`, as every part of `Tools`
    does, and each answers the error that the S2b part of ADR-061 names where the host
    has no container runtime.
    - `Image` builds the plugin image of decision 24 from a build file and a context
      and returns it as an OCI image layout. `plugin pack`, step 7 of `plugin check`
      and `plugin smoke` reach the build through it.
    - `Unit` is the part that the S2b part of ADR-061, as proposed, names for starting
      and stopping the compose fragment of a plugin id and for reporting whether its
      container runs. This part asks of it every other step of decisions 33, 36 and
      37: to report of a container its image reference, its user, its binds and the
      uid of its first process as the host sees it; to report of a network whether it
      is internal, whether the host holds an address in it and which members it has;
      to find the kernel container of a home by its bind; to attach it to a network
      and to detach it from one, the no-network mode included; and to stop and remove
      a container and a network.
    - No server process builds an image, starts or stops a unit or changes a network,
      whoever consumes `operations` there (`08:447`, `:1293`).

39. **What was read of the container runtime, and what a run has to show.** No program
    below is pinned by the project, and none has a register entry
    (`tools/upstream-components.yaml` holds none; licence:60 lists Compose with no
    pin). Each is run and never linked, is baked into no image that the project ships
    (`08:1375`), and gets its register entry, mode `tool`, and its row of the licence
    record in the commit that first uses it, with a new reading if the pin moved
    (ADR-063 decision 35): the engine, its command-line client, the build plugin of
    that client and Compose, each by name. What follows was read on 2026-11-27 and
    prepares those entries; it replaces none.

    | Program and tag | Where read | What it holds |
    |---|---|---|
    | The engine, `moby/moby` at `docker-v29.10.0` | `LICENSE` and `NOTICE` at the root of the tree | The Apache License 2.0, "Copyright 2013-2018 Docker, Inc."; the notice names two works under the BSD 3-Clause licence and one under the MIT licence |
    | The client, `docker/cli` at `v29.10.0` | `LICENSE` and `NOTICE` | The Apache License 2.0, "Copyright 2013-2017 Docker, Inc."; the notice names one work under the MIT licence |
    | The build plugin, `docker/buildx` at `v0.39.1` | `LICENSE` | The Apache License 2.0 with its appendix left as the template, so no copyright line of its own; no `NOTICE` was read |
    | Compose, `docker/compose` at `v5.8.0` | `LICENSE` and `NOTICE` | The Apache License 2.0 with its appendix left as the template; the notice reads "Copyright 2020 Docker Compose authors" |

    Not read: the licences of the modules that each of the four links, and of what a
    binary release of each bundles. Each is read with its register entry, before its
    first use.

    The behaviours that the decisions above need of the runtime, with what the source
    of those tags says of each. None was run for the proposal. Note of 2026-11-27: each
    row was run once on that date (decision 48). The two last columns hold the version
    and the result, and the section "Runs of decision 39, recorded 2026-11-27" holds
    the commands and what was observed.

    | # | Behaviour, and the decision that needs it | Read | A run has to show | If the runtime does not give it | Version run on 2026-11-27 | Result |
    |---|---|---|---|---|---|---|
    | 1 | A build exports exactly one image manifest (decision 24) | Build plugin, `docs/reference/buildx_build.md`: the output type `oci` writes "the result image or manifest list" as an OCI image layout tarball, and "By default, a minimal provenance attestation will be created for the build result", which `--provenance=false` disables | The layout of a build with no attestation names one `application/vnd.oci.image.manifest.v1+json` and no index | `plugin pack` refuses (decision 24); the work stops on the point | Build plugin v0.39.1 on the default builder (driver `docker`, BuildKit v0.35.0), engine 29.10.0 with the containerd image store | Holds: one image manifest and no index |
    | 2 | The registry serves the pushed manifest byte for byte, and the runtime pulls and runs the image by that digest (decisions 24 and 31; steps G2 and C3) | The registry keeps the pushed bytes of a manifest (decision 4). The pull of the engine was not read | The digest of `image.json` is the digest that the registry serves and that the runtime pulls | The work stops on the point | The pinned registry 3.1.2; engine 29.10.0, containerd v2.4.2, runc 1.5.3 | Holds: the registry served the pushed bytes under the digest of the layout, and the runtime pulled and ran the image by it |
    | 3 | A container reports the reference by digest from which it was created (decisions 33 and 36; step C3) | Not read | The runtime reports that reference for a running container | The work stops on the point | Engine 29.10.0 with the containerd image store; Compose v5.8.0 | Holds: `Image` is the manifest digest; `Config.Image` is the string that the creator passed |
    | 4 | An internal network gives the instance no route out and the host no address in it (decision 32, points 1 and 2) | Engine, `daemon/libnetwork/drivers/bridge/setup_ipv4_linux.go`, `setupBridgeIPv4`: the bridge of the host is given the address of the network unless the gateway mode is `isolated` or an option inhibits the address, and the gateway of the containers is set only when the network is not internal. `bridge_linux.go`: the mode `isolated` "can only be used for an internal network" | Points 1 and 2 of decision 32, from inside an instance, on the daemon settings of the exit's clean machine | The work stops on the point | Engine 29.10.0 with its default settings, and a nested engine 29.10.0 with the flags of the S2a exit | Holds on both, with the gateway mode `isolated` |
    | 5 | The resolver answers the instance for no name outside its network (decision 32, point 3) | Engine, `daemon/libnetwork/sandbox.go`, `hasExternalAccess`, `sandbox_dns_unix.go`, `startResolver`, and `resolver.go`, `forwardExtDNS`: a container whose only networks are internal gets a resolver that does not forward a request from the namespace of the host | Point 3 of decision 32 | The work stops on the point | As row 4 | Holds on both: `SERVFAIL` for every name outside the network |
    | 6 | The kernel container, started in the no-network mode, joins the network of an instance and resolves the unit name there (decision 33) | Engine, `daemon/container_operations.go`, `updateNetworkSettings`: a connect is refused while one of the container's networks is `none`, with "container cannot be connected to multiple networks with one of the networks in private (none) mode". `DisconnectFromNetwork` in the same file detaches a running container from a network and has no exception for `none` | A running container is detached from the no-network mode and then attached, without a restart, and resolves the name of the instance | Question Q11 (b) | As row 4 | Holds on both: detached from `none` and attached with no restart, and the unit name resolved, 2.6 s after the first attach. The condition of decision 45 is met |
    | 7 | Compose runs the fragment: it takes the keys that the template writes, starts nothing on a second run, replaces a container whose fragment changed, and leaves alone a container that it did not create on its network | Not read | Each of the four, and what Compose does with a network or a container of the unit name that it did not create | The work stops on the point (ADR-063 decision 50) | Compose v5.8.0, engine 29.10.0 | Holds, the four. Compose uses a network of the unit name that it did not create, with a warning: question Q16 |
    | 8 | The runtime reports the uid of a container's first process as the host sees it (decisions 30 and 36) | Not read | The report, under the mapping of the exit | `plugin start` refuses a runtime that reports a rootless mode or a remapped user namespace, where the number of the template is not the uid on the host | Engine 29.10.0, rootful, with no remapped user namespace; and the nested engine | Holds: the first process has the user 65532 as the host sees it |
    | 9 | The systemd unit, copied by root, creates the network and runs the container, and a container stopped through the runtime is not started again (decisions 31 and 37) | Not read | Both, in the systemd job | The work stops on the point (ADR-063 decision 50) | systemd 259 as the first process of an unprivileged container, with the client and the socket of engine 29.10.0 | Holds: the unit made the network and ran the container, and a container stopped through the runtime was not started again |
    | 10 | The user of the unit reads the bound directory (decision 30; question Q9) | Not read | The instance opens its key under the answer to Q9, and no file of the home | The owner is asked again with Q9 | Engine 29.10.0, rootful, with no remapped user namespace | Holds under Q9 (a): the unit's user reads the key and no file of the home. Question Q15 |

    The exit proves rows 1 to 8 and 10 in steps G2, C2 and C3 and the systemd job
    proves row 9. A row whose last column stops the work goes to the owner with the
    lines an answer would edit (ADR-063 decision 33). The owner said on 2026-11-27 that
    an isolated sandbox can be provided for this work; whether the rows are run there
    before this part is accepted is question Q14. The runs of 2026-11-27 were throwaway
    runs before any code of this part: the exit and the systemd job still prove the
    rows on what is built.

40. **What this part asks of the other S2b parts** (ADR-063 decision 34). Each is
    proposed with its own ADR, and the texts are made one before any of them goes to
    the owner.
    - ADR-041: its decision on the modes of the instance's directory follows the answer
      to question Q9, which is asked here and nowhere else; it names the command that
      makes a new pair and writes the pin after a revocation, and this part's table of
      decision 28 takes that writer; it fixes the lifetime of `remote/kernel/`
      (decision 35); and it names the variables that the template sets for the
      listener and the credential directory, which is `/aiops/identity` (decision 30).
    - ADR-048: the bound of an id's segment, with the unit name that question Q12
      forms from it; and what an artifact carries whose manifest lists `inproc` and
      `remote`.
    - ADR-020: its decision on `plugin verify` rests on decision 25: the image is
      pulled only after the signature and the attestation hold.
    - ADR-061: `plugin start` leaves alone an instance that the systemd unit started
      and attaches the kernel container to it (decision 33); the part `Unit` carries
      the steps of decision 38; `disable` and `remove` stop a unit as decision 37
      says; the command that compiles the binary of a remote Go plugin compiles it for
      the platform of the build (decision 24); with question Q8 (a), `plugin smoke` of
      remote code is given a repository; and whether `plugin start` writes a missing
      line of the endpoint table.
    - ADR-054: the register entries of the four programs of decision 39, by name,
      before their first use.

41. **Cases.** Beside the cases of ADR-063 decision 28, each of these has a test that
    fails on its base (ADR-063 decision 29):
    - `plugin pack` of remote code with an `artifact/` directory in the plugin
      directory builds the same image as without it; a build that exports an index is
      refused.
    - A remote bundle with no row, with two rows and with a row of another plugin, each
      refused by `plugin check` and denied under check 8 at every entry point.
    - An absent `sidecars.json` is an empty table for `sidecars-static`.
    - Install on a home whose `remote/` has a wider mode sets it to 0700 and says so;
      install where the site user's uid is the number of the template renders nothing.
    - `plugin start` refuses, each time with the object named: a network of the unit
      name that is not internal, one in which the host holds an address, and one with
      a third member; a running container of the unit name with another image digest,
      another user or another bind; a container of the unit name that binds another
      home; a fragment whose digest differs from the kept `artifact.json`. Note of
      2026-11-28 (decision 50): where the network exists before the run, each of the
      three refusals of a network is made before the fragment runs, and the case
      asserts that no container of the unit name was started.
    - From inside an instance no connection reaches a listener that the test opens on
      the host, on the gateway address of the network and on another address of the
      host (decision 32, point 2).
    - After `plugin disable` and after `plugin remove` the container and the network
      of the unit are gone and the kernel container is attached to neither; a second
      `plugin start` creates them again.
    - On the systemd target, `plugin start` after `systemctl enable --now` starts
      nothing and attaches the kernel container.

### Consequences of the S2b part

- The operations plugin gains the writer and the reader of `image.json`, the build and
  the export through the part `Image`, the push of an image before its artifact, the
  paths of decision 28 with the writer of `sidecars.json`, the checks of decisions 30
  and 36, and in `plugin start`, `disable` and `remove` the steps of decisions 33 and
  37 through the part `Unit`. `plugins/sidecars-static` reads `sidecars.json`. `boot`
  gains from this part only the wording of the one-row clause of check 8 (decision 27).
- `plugin pack` of remote code needs a container runtime, and `plugin install` needs
  none. `plugin start`, and the stop of a unit, need the program `docker` with its
  Compose plugin.
- An artifact of remote code and its image live in one repository, and a unit names the
  repository that the operator's spec named.
- The SBOM of remote code says which image the artifact ships and not what is inside
  it. A site that needs an inventory of an image runs a scanner of its own on it, and
  a plugin image that the project conveys needs an inventory that this part does not
  write (decision 26).
- Boot checks none of the files of decision 28 against a digest: decision 18 covers the
  kept files of decision 5. The units, the endpoint table and the credentials are as
  strong as `trust.yaml`, which the ownership of the home protects (ADR-020 decision
  11). `plugin start` reads the image digest of a unit against the kept `artifact.json`
  at every start (decision 33); nothing reads a systemd unit that root copied out of
  the home until `plugin start` compares the running container with it.
- One container runtime serves the instances of one home per plugin id, since the unit
  names carry no home (decision 33).
- A site whose container runtime does not report what decision 36 checks cannot use
  `plugin start`, and the fence of an instance that its operator runs by hand is the
  operator's.
- An instance does not come back by itself after it exits or after a reboot of the
  host (decision 31).
- With question Q8 (a), remote code installs from a registry only and `plugin smoke` of
  remote code needs a repository. With Q9 (a), one directory per instance holds files
  that others may read, under three directories that others cannot enter. With Q10
  (a), a kernel that runs as a binary on the host reaches no instance that a unit
  starts, on either target, until the step that adds the host form. With Q12 (b), the
  name of a unit carries a digest of the plugin id.
- The media type and the annotation key of decision 23 enter the artifact layout that
  decision 22 freezes at its first publication. The unit name, the port, the user and
  the path `/aiops/identity` are constants of the unit template and are not part of
  that layout.

### Edits on acceptance of the S2b part

Made in the commit that marks this part accepted, which by decision 48 follows the
recorded results of decision 39. The lines that the chosen option of a question names
are edited before that, in the commit that records the owner's answers; the section
"Owner answers of 2026-11-27" of this part lists what that commit edits and what it
leaves:

- This file: the status of this part; dated notes that name the decisions of this part
  at decision 1 (the layer of decision 23, and the answer to question Q13), decision 2
  (decision 25), decision 10 (decisions 35 and 37), decision 11 (decision 27: for
  remote code the one row is a row of the artifact's own plugin), decision 13 (the rows
  of decision 28, with the writers that ADR-041 and the S2b part of ADR-061 add),
  decision 14 (the closed directories of decision 30, and the answer to question Q9)
  and decision 15 (the networks of a kernel container at S2b; decision 33 and the
  answer to question Q11).
- `07:1850` (`Artifact`) and `08:393`: for remote code the one row is a row of the
  artifact's own plugin; the image digest is in the layer `image.json`, and the image
  is in the artifact's repository. `07:1865`: for remote code `plugin inspect` prints
  the image reference from the artifact's manifest.
- `07:1854` (`Unit`), `08:1293` and `02:313`: the fragment and the unit are under
  `remote/units/<plugin id>/` of the home, and the table is `sidecars.json`; root
  copies the systemd unit out of the home and enables the copy; on the systemd target
  `aiopsctl plugin start <id>` attaches the kernel container after `systemctl enable
  --now`. `08:432` (profile files) and `02:316` (composition source): the units, the
  endpoint table and the credential directories are files of the home.
- The row of ADR-047 in the ADR table of `08` §3 (`08:1181`): the S2b part and its date.
- The lines that the chosen option of each question names.

Left to the commit that builds the clause in the one gate text (ADR-060 decision 16):
`08:506` says that the bundle of a remote artifact is denied when it holds no row, when
it holds a second row and when its row is not a row of the artifact's own plugin
(decision 27).

### Questions for the owner on the S2b part

Each question gives its options, the recommended one, why, and when the answer is
needed. An option that changes or defers a written line says so and names the lines.

**Q8. What do `plugin install`, `plugin verify` and `plugin smoke` of remote code do
where the spec is a local path?** A spec is an OCI reference, an `<id>@<version>`
through an index, or a local path (`07:1864`), and decision 1 has `inspect`, `gate
--against`, `install` and `verify` read a local layout "as they read a registry". A unit
names its image in a repository by digest (decision 31), install pulls no image and runs
none (ADR-063 decision 17), and a layout on disk is no repository that a container
runtime pulls from. No step of the S2b exit installs remote code from a local path, and
the `plugin verify` cases of ADR-063 decision 28 read the artifacts that G2 and P2
pushed. `plugin smoke` is the one consumer: it starts from a plugin directory
(`07:1583`), and the S2b part of ADR-061, as proposed, has it pack and then install on a
throwaway home, with no registry.
- (a) Remote code runs from a registry only. `plugin install` and `plugin verify` of
  remote code refuse a local-path spec: each names the reason and `plugin push`, and
  writes nothing. `inspect` and `gate --against`, which fetch no image, read a local
  path of remote code as they read any other. `plugin smoke` of remote code pushes its
  throwaway artifact to a repository that the author names, in a form that the S2b part
  of ADR-061 fixes. This narrows two written lines for one kind of artifact: decision 1
  and `07:1864` gain "install and verify of remote code take a registry spec", and
  `07:1583` gains "and, for `remote`, a repository" beside the container runtime.
- (b) The image is loaded from the layout into the store of the container runtime at
  hand, by the command that has a runtime: `plugin start`, `plugin verify` and `plugin
  smoke`. Install still pulls nothing and runs nothing; it records the path of the
  layout beside the unit, and the unit names the image by its digest alone, which the
  runtime resolves in its own store. No written line changes. It adds a file of the
  home and a step to `plugin start`, the systemd unit of such an install finds its
  image only after a `plugin start` has loaded it, and it rests on two behaviours that
  were read in no source and would join decision 39: that the runtime loads an OCI
  layout and keeps the digest of its manifest, and that it runs an image by that digest
  with no repository.
- (c) Install takes the repository from the operator, who pushed the image there with
  another tool. This adds a flag to the S2b part of ADR-061 and a second place where an
  image may be, beside the artifact's repository, and does nothing for `plugin smoke`.
- Recommended: (a). It rests on no behaviour of a runtime that nobody has read, it keeps
  install free of a container runtime and a unit to one form of image reference, and an
  author who publishes remote code has a repository to push to. Its cost is the two
  narrowed lines and a repository for `plugin smoke`, which the S2b part of ADR-061 has
  to take up before either part is accepted. Needed before `plugin install` of remote
  code and `plugin smoke` of remote code merge.
- **Answered 2026-11-27: (a)**, decision 42.

**Q9. How does the user of a unit read the one directory it binds?** Decision 14 gives
every file of the home the mode 0600 and the site user as its owner, and `aiopsctl`
runs as that user and cannot give a file to another. ADR-063 decision 25 has a unit
name a user that is neither the site user nor root and bind
`remote/instances/<plugin id>/` read-only. As written, the two leave that user unable
to read its own key, so no option keeps both to the letter. ADR-041, as proposed, needs
the same answer for the directory of an instance. The point is one question: it is
asked here, because the answer amends decision 14, and ADR-041 takes the answer
(decision 40). Under every option the home and the three directories above this one
are of mode 0700, owned by the site user and checked at install and at `plugin start`
(decision 30).
- (a) The directory has the mode 0755 and its files the mode 0644, as a stated
  exception of decision 14. Inside the container the bind is readable to whatever user
  the unit names, under every user mapping that decision 14 admits, and install needs
  to know nothing of the runtime. On the host the guard is not the mode of the key: it
  is the three directories of mode 0700 above it, which `aiopsctl` checks and sets at
  every install of remote code and every `plugin start`. Every other file of the home
  is guarded by its own mode 0600 as well; this one is not. The cost: a copy that keeps
  the modes and lands outside a closed directory, as an archive, a backup of `remote/`
  or `cp -a`, exposes the key to every user who reads the copy, and so does whatever
  opens the three directories between two commands of `aiopsctl`.
- (b) The directory has the mode 0750 and its files 0640, and the unit names the group
  of the site user as the only group of its user. The key is then readable by one group
  and not by every user. It holds only under a mapping that carries that group into the
  container: it does under a mapping that changes no id, and it does not under a
  rootless runtime, where the group of the template is a subordinate group on the host
  and the instance cannot read its key. Install has no runtime and cannot tell which
  mapping `plugin start` will find, so such a site fails at `plugin start`. The
  instance holds the group of the site user on the host for as long as it runs.
- (c) Decision 14 to the letter, and the unit names the site user. This changes ADR-063
  decision 25 and the assertion of step C2. The instance then runs as the one user that
  `run/reload.sock` serves (ADR-062 decision 5), and only the missing bind keeps it from
  the socket and the home.
- (d) `plugin start` copies the files of the directory into storage of the container
  runtime that the user of the unit owns, and decision 14 stands. The key then exists
  twice, the copy outlives a `plugin remove` unless that command also finds it, and the
  systemd target needs another mechanism.
- Recommended: (a). It is the one option that works under every mapping with no input
  from the runtime, it keeps the user of ADR-063 decision 25 and gives third-party code
  no group of the host, and its guard is checked by the commands that write and use the
  key. (b) is narrower by one mode bit on the host and fails on a rootless runtime.
  ADR-041, as proposed, recommends (b) in its own question on the point; the two texts
  are made one before either goes to the owner, who answers once. Needed before the
  unit and `plugin install` of remote code merge.
- **Answered 2026-11-27: (a)**, decision 43.

**Q10. What does `plugin start` do for a kernel that runs as a binary on the host?**
ADR-063 decision 27 leaves the point here. The network of an instance has no route out,
publishes no port and holds no address of the host (ADR-063 decision 24; decision 32),
and the endpoint that install writes is a name that the runtime resolves on that network
(ADR-063 decision 27), so a process on the host is not served by it. The clean machine
of the exit runs the kernel as a container (ADR-063 decision 27); the T3 of a remote
example and the stubs of the failure matrix start their instances as processes, with no
unit (ADR-063 decisions 31 and 64). The deployment view D-2 runs the `aiops` binary on
the VM beside a remote plugin from a unit (`02:295`, `:313`, `:336`), and `02:191` places
the remote-plugin container at S2b.
- (a) At S2b a unit's instance is reached by a kernel container only, on the compose
  target and on the systemd target alike: the systemd unit of ADR-063 decision 50 is
  built and run, and what dials its instance is a kernel container that `plugin start`
  attached (decision 33). For a kernel on the host `plugin start` does what decision 33
  says where it finds no kernel container, and says that a kernel on the host does not
  reach the network of a unit. The host form is an addition to this ADR, due with
  `deploy/compose` at S7 (ADR-063, Context) and before the first community release,
  with a mechanism shown on a pinned runtime to keep point 2 of decision 32: the host
  dials the instance, and the instance reaches no listener of the host. This defers
  part of a written view. Every line that draws or words the host form gains "for a
  kernel that runs as a container; for a kernel on the host from S7": `02:295`,
  `02:313`, `02:336`, `07:1854` and `08:1293`.
- (b) The host form is built at S2b. The host then needs an address in the network of
  the instance, which point 2 of decision 32 forbids: the network takes the default
  gateway mode, and a rule of the packet filter has to keep the instance from every
  listener of the host. `plugin start` reads the address that the runtime gave the
  instance and writes it into the line of the endpoint table, which then holds an
  address that changes at every start. Neither the rule nor the reach of the host was
  read for any runtime; if the pinned runtime gives only one, the work stops (ADR-063
  decision 33).
- (c) The unit of such a site publishes the port of the instance on the loopback
  interface of the host. The instance then has a way to the host that ADR-063 decision
  24 does not give it, and whether a runtime publishes a port of an internal network
  was read in no source, so this weakens that decision or cannot be built.
- Recommended: (a). The exit and every case of S2b run a kernel container or no unit,
  so nothing at S2b consumes the host form (`08:680`), and (b) trades the one property
  of the network that was read in a source, no address of the host, for a rule that
  nobody has read. The owner decides, because `02:295` draws the host form. Needed
  before `plugin start` merges.
- **Answered 2026-11-27: (a)**, decision 44.

**Q11. How does a kernel container that started with no network join the network of an
instance?** ADR-063 decision 27: "The kernel container starts with no network, as at
S2a", and `plugin start` attaches it. The S2a exit starts it with `--network none`
(`test/authoring/exit_test.go:1063`). On the engine read for this part a container in
that mode cannot be connected to a network as it stands: the connect is refused while
`none` is one of its networks (decision 39, row 6). The kernel that serves step C4 is
the process that booted before step C2 (ADR-063 decision 27), so `plugin start` cannot
start the kernel container again.
- (a) `plugin start` detaches the kernel container from the no-network mode, once, and
  then attaches it to the network of the instance. ADR-063 decision 27 and the
  container form of decision 15 stand as written: the container starts with no
  network, and from the first `plugin start` its only networks are those of instances.
  The source of that engine has a detach of a running container with no exception for
  `none`; that the attachment then works without a restart, and that the kernel
  resolves the unit name after it, was not run (decision 39, row 6).
- (b) The kernel container starts on an internal network of its own, in which the host
  holds no address and which has no other member, and `plugin start` attaches the
  networks of instances beside it. The operator creates that network before the kernel
  container, a step more in what the operator does by hand until `deploy/compose`.
  This changes a written line: ADR-063 decision 27 reads "starts on a network that
  holds it alone", and decision 15 names that network.
- Recommended: (a), on a condition. It keeps the written line and the S2a form of the
  kernel container, and adds one step to `plugin start`. The condition: row 6 of
  decision 39 is run on the pinned runtime before `plugin start` merges. If the run
  fails, (b) is the other way and the owner is asked again with the two lines (ADR-063
  decision 33). Needed before `plugin start` merges.
- **Answered 2026-11-27: (a), on its condition**, decision 45.

**Q12. How is the name of a unit formed from a plugin id that may be too long for it?**
The unit name is the host of the instance on its network (decision 31), so it is one DNS
label of at most 63 bytes. An id is two kebab-case segments (`00:657`;
`boot/manifest.go:582-590`) with no bound in the code, and the S2b part of ADR-048, as
proposed, bounds a segment at 128 bytes. The plain name
`remote-plugin-<owner>--<name>`, joined by two hyphens, which no kebab-case segment
holds, leaves 47 bytes for both segments. Ids are published, and a rule on them is one
of the items that has to be right before plugins are published (`08:680`).
- (a) The plain name, and an id whose unit name is longer than 63 bytes is refused for
  the runtime `remote`, at `plugin check` and at install, with no unit rendered. A pack
  or an in-process plugin of the same id still installs. This narrows the ids of remote
  code below the grammar of the id: `00:657` and the S2b part of ADR-048 gain the bound
  of 47 bytes for the two segments of a remote plugin.
- (b) One derived form for every id: `remote-plugin-`, then `<owner>--<name>` cut to its
  first 32 bytes with any hyphen at its end removed, then `-` and the first 16 hexadecimal
  digits of the SHA-256 of the plugin id; at most 63 bytes. Every id that the grammar
  admits installs. The name is a function of the id alone, an operator reads the start
  of the id in it, and install refuses an id whose unit name another installed id of
  the site already has, so two ids never share a unit. No written line changes.
- Recommended: (b). No id that the manifest schema admits is refused for one runtime,
  and a bound on published ids is not introduced by the length of a host name. The cost
  is a name that is longer than it has to be for a short id. Needed before the unit
  renderer merges.
- **Answered 2026-11-27: (b)**, decision 46.

**Q13. Does `index.json` name the plugin image, and does `plugin push` tag it?** ADR-063
decision 61: `plugin pack` "writes the image beside the artifact as an OCI layout", and
`plugin push` "pushes the image and then the artifact". Decision 24 puts the blobs of
the image into the layout of the artifact. The image specification lets a layout hold a
blob that `index.json` does not name
(`github.com/opencontainers/image-spec@v1.1.1`, `image-layout.md`, "Blobs"), and the
reader of S2a counts in `index.json` only the manifests of the `artifactType` of
decision 1 that have no subject, so it reads a layout whose index also names an image
(`plugins/operations/internal/operations/artifact.go:298-318`).
- (a) `index.json` names the image manifest beside the artifact's manifest, with
  `org.opencontainers.image.ref.name` set to `image-` and the 64 hexadecimal digits of
  its digest, and `plugin push` pushes the image under that tag. A tool that copies a
  layout or a repository by its index or its tags then copies the image with the
  artifact, and a registry that collects untagged manifests keeps it. The rule of
  decision 1 stands as the code reads it: exactly one manifest of the `artifactType`
  `application/vnd.aiops.plugin.v1`. No unit and no command of `aiopsctl` reads the
  tag: a unit names the image by digest (ADR-063 decision 25).
- (b) The image is in `blobs/sha256/` only, and a reader finds it by the digest in
  `image.json`. In the registry it is a manifest with no tag that no manifest refers
  to. A site that copies an artifact to a registry of its own then needs `aiopsctl` or
  two copies by digest, and a registry that collects untagged manifests may drop the
  image under an artifact that still names it.
- Recommended: (a). It is the reading of "beside the artifact" that an OCI tool
  understands, and it costs one descriptor and one tag. Needed before `plugin pack` of
  remote code merges.
- **Answered 2026-11-27: (a)**, decision 47.

**Q14. Is this part accepted before the runs of decision 39?** The decisions of this
part rest on ten behaviours of a container runtime. Four were read in the source or the
documentation of one release, one in part, five nowhere, and none was run. ADR-063
decision 33 stops the work where a written line cannot be built, and steps G2, C2 and
C3 would find each point, but only after the code that rests on it is written. The
owner said on 2026-11-27 that an isolated sandbox can be provided for this work.
- (a) The part stays proposed until each row of decision 39 has a recorded result: the
  programs are pinned, each row is run once in an isolated environment, and the table
  gains the version and the result. A row that fails is settled, by its last column or
  by a question, before the owner accepts. The recommendations of Q8, Q10 and Q11 are
  then read against results.
- (b) The part is accepted on the readings. The exit proves each row, and a row that
  fails stops the work and returns to the owner (ADR-063 decision 33).
- Recommended: (a). The reading of row 6 alone turned a step of decision 33 into
  question Q11, the runs are small, and a failure found after acceptance reopens an
  accepted decision and the code built on it. The owner decides, because (a) waits for an
  environment that only the owner can provide. Needed before this part is accepted.
- **Answered 2026-11-27: (a)**, decision 48.

### Owner answers of 2026-11-27

The project owner answered every question of the section "Questions for the owner on the
S2b part" on 2026-11-27, each with its recommended option, Q11 on its condition. By the
answer to Q14 the part is not accepted on that date: it stays proposed until each row of
decision 39 has a recorded result (decision 48). Each decision below names the question
it answers. Where a decision of this part leaves a point to a question, the decision
here settles it.

42. **Remote code runs from a registry only** (Q8 (a)). `plugin install` and `plugin
    verify` of remote code refuse a local-path spec: each names the reason and `plugin
    push`, and writes nothing. `inspect` and `gate --against`, which fetch no image, read
    a local path of remote code as they read any other. `plugin smoke` of remote code
    pushes its throwaway artifact to a repository that the author names, in a form that
    the S2b part of ADR-061 fixes. Two written lines are narrowed for one kind of
    artifact: decision 1 and `07:1864` gain "install and verify of remote code take a
    registry spec", and `07:1583` gains "and, for `remote`, a repository" beside the
    container runtime. This settles the last point of decision 25 and the local-path
    spec of decision 31.

43. **The directory of an instance has the mode 0755 and its files the mode 0644** (Q9
    (a)), as a stated exception of decision 14. Inside the container the bind of
    `remote/instances/<plugin id>/` is readable to whatever user the unit names, under
    every user mapping that decision 14 admits, and install needs to know nothing of the
    runtime. On the host the guard is not the mode of the key: it is the three
    directories of mode 0700 above it, which `aiopsctl` checks and sets at every install
    of remote code and every `plugin start` (decision 30). Every other file of the home
    is guarded by its own mode 0600 as well; this one is not. The cost stands as the
    option states it: a copy that keeps the modes and lands outside a closed directory,
    as an archive, a backup of `remote/` or `cp -a`, exposes the key to every user who
    reads the copy, and so does whatever opens the three directories between two
    commands of `aiopsctl`. The user of a unit stays the pair `65532:65532` of decision
    30, and its second number is not the group of the site user. ADR-041 takes this
    answer for the directory of an instance (decision 40), and row 10 of decision 39 is
    run under it. Note of 2026-11-28, after the runs of that date: the modes and the
    guard hold under rootless Podman and under SELinux enforcing once the bind carries
    a label. Under rootless Podman with `--userns=keep-id` the runtime adds the site
    user's group to the instance, which is question Q22 of the section "Runs after the
    answers of 2026-11-28"; the label is its questions Q20 and Q21.

44. **At S2b a unit's instance is reached by a kernel container only** (Q10 (a)), on the
    compose target and on the systemd target alike: the systemd unit of ADR-063 decision
    50 is built and run, and what dials its instance is a kernel container that `plugin
    start` attached (decision 33). For a kernel on the host `plugin start` does what
    decision 33 says where it finds no kernel container, and says that a kernel on the
    host does not reach the network of a unit. The host form is an addition to this ADR,
    due with `deploy/compose` at S7 (ADR-063, Context) and before the first community
    release, with a mechanism shown on a pinned runtime to keep point 2 of decision 32:
    the host dials the instance, and the instance reaches no listener of the host. This
    defers part of a written view. Every line that draws or words the host form gains
    "for a kernel that runs as a container; for a kernel on the host from S7": `02:295`,
    `02:313`, `02:336`, `07:1854` and `08:1293`. This settles decision 34.

45. **`plugin start` detaches the kernel container from the no-network mode, once, and
    then attaches it to the network of the instance** (Q11 (a), on its condition).
    ADR-063 decision 27 and the container form of decision 15 stand as written: the
    container starts with no network, and from the first `plugin start` its only
    networks are those of instances. The condition: row 6 of decision 39 is run on the
    pinned runtime before `plugin start` merges. If the run fails, option (b) of Q11 is
    the other way, and the owner is asked again with the two lines it changes, ADR-063
    decision 27 and decision 15 (ADR-063 decision 33). This settles step 4 of `plugin
    start` in decision 33. Note of 2026-11-27: row 6 was run on that date on Docker
    Engine 29.10.0 and holds, so the condition is met, Q11 stays (a) and the owner is not
    asked again. What the run shows of the resolver after the first attach is question
    Q17.

46. **The name of a unit is one derived form for every id** (Q12 (b)): `remote-plugin-`,
    then `<owner>--<name>` cut to its first 32 bytes with any hyphen at its end removed,
    then `-` and the first 16 hexadecimal digits of the SHA-256 of the plugin id; at
    most 63 bytes. Every id that the grammar admits installs, and no bound on the ids of
    remote code enters `00:657` or the S2b part of ADR-048. The name is a function of
    the id alone, an operator reads the start of the id in it, and install refuses an id
    whose unit name another installed id of the site already has, so two ids never share
    a unit. No written line changes. This settles the unit name of decision 31.

47. **`index.json` names the plugin image, and `plugin push` tags it** (Q13 (a)).
    `index.json` names the image manifest beside the artifact's manifest, with
    `org.opencontainers.image.ref.name` set to `image-` and the 64 hexadecimal digits of
    its digest, and `plugin push` pushes the image under that tag. A tool that copies a
    layout or a repository by its index or its tags then copies the image with the
    artifact, and a registry that collects untagged manifests keeps it. The rule of
    decision 1 stands as the code reads it: exactly one manifest of the `artifactType`
    `application/vnd.aiops.plugin.v1`. No unit and no command of `aiopsctl` reads the
    tag: a unit names the image by digest (ADR-063 decision 25). This settles the points
    that decisions 24, 25 and 31 leave to Q13.

48. **This part stays proposed until each row of decision 39 has a recorded result** (Q14
    (a)). The programs are pinned, each row is run once in an isolated environment, and
    the table of decision 39 gains the version and the result. A row that fails is
    settled, by its last column or by a question, before the owner accepts. The
    recommendations of Q8, Q10 and Q11, which decisions 42, 44 and 45 record as
    answered, are then read against results.

**Edits made with this record.** The commit that records the answers makes the edits
that the chosen options name, and no line of `00` among them: the two dated notes in
this file, at decision 1 (decision 42) and at decision 14 (decision 43); `07:1583` and
`07:1864` (decision 42); and `02:295`, `02:313`, `02:336`, `07:1854` and `08:1293`
(decision 44). Lines that restate an edited one are edited with it: the spec forms of
`02:205`, of `01:222` and `01:306` (`01` is the file of `docs/architecture/` with that
number, at the same commit) and of plugin record §11, which restate `07:1864`; what
smoke needs in the row `Smoke` of `07:1537` and in plugin record §14, which restate
`07:1583`; and the start of a remote instance per target in plugin record §11, which
restates `08:1293`. `00:305` and `01:279` name the container runtime as the condition
on which smoke is run and do not list what smoke needs, so they stand. The figure of
`02` §4.2 is not redrawn: its dashed dial to a unit names no form of the kernel. An
option the owner did not choose edits nothing.

Left to later commits, each named:

- the commit that marks this part accepted, once each row of decision 39 has a recorded
  result (decision 48): every edit of the section "Edits on acceptance of the S2b part"
  that no chosen option names. In this file those are the dated notes at decisions 1
  (the layer of decision 23, and decision 47), 2, 10, 11, 13 and 15, and at decision 14
  the closed directories of decision 30. Elsewhere they are `07:1850`, `07:1865` and
  `08:393`; in `07:1854`, `08:1293` and `02:313` the paths under `remote/units/`, the
  name `sidecars.json`, root's copy of the systemd unit and `plugin start` on the
  systemd target; `08:432` and `02:316`; and the row of ADR-047 in the ADR table of
  `08` §3;
- the commits that record the runs of decision 39: the version and the result of each
  row in its table, and for row 6 the condition of decision 45;
- the commits that record the owner's answers on ADR-041, ADR-048, ADR-020 and ADR-061:
  what decision 40 asks of each, in the text of that ADR. As proposed on 2026-11-27,
  four of those texts differ from the answers above, and this record edits none of
  them:
  - ADR-041 decision 24 and option (a) of its question Q5, which it recommends, give
    the instance's directory the mode 0750, its files the mode 0640 and the unit the
    site user's group. That is option (b) of Q9 here, and decision 43 records (a), the
    modes 0755 and 0644 with no group of the host. Decision 40 has ADR-041 take the
    answer to Q9, and ADR-041 has this part take the modes of its decision 24. Where
    the answer recorded on ADR-041 differs from decision 43, the owner is asked which
    holds before either part is accepted;
  - the S2b part of ADR-020 has `plugin verify` read the image of a local-path spec
    from the layout, and decision 42 has the command refuse such a spec of remote code;
  - the S2b part of ADR-061 has `plugin smoke` of remote code pack and install on a
    throwaway home, with no registry, and lists as open what `plugin verify` of remote
    code does with a local-path spec; decision 42 gives smoke a repository, whose form
    that part fixes;
  - the S2b part of ADR-048 asks this part for a unit name that is an injective
    function of the id. The name of decision 46 is a function of the id that two ids
    can share, and install refuses the second of them;
- the commit that builds the clause in the one gate text (ADR-060 decision 16): `08:506`,
  as the section "Edits on acceptance of the S2b part" says.

### Runs of decision 39, recorded 2026-11-27

Note of 2026-11-27. Each row of decision 39 was run once on that date, as a throwaway
run on the tree of commit `3dc2c41`. The runs committed nothing, and every container,
network, image and file that they made was removed. The table of decision 39 holds the
version and the result of each row; this section holds the commands, what was observed
and what the results touch. All ten rows hold, and no row is listed as not run.

**The part stays proposed for two points only**, questions Q15 and Q16 below. Q15 is
the one that the record of the owner's answers already leaves to the owner: decision 43
and the answer recorded on ADR-041 give the directory of an instance different modes.
Q16 comes from row 7: as decision 33 orders the steps, an instance can start on a
network that is not the network of decision 32. Questions Q17 and Q18 are smaller
points of the runs and block `plugin start`, not the part. Decisions 42 and 44 to 47
stand as the owner gave them, and the condition of decision 45 is met: row 6 holds, so
Q11 stays (a), and ADR-063 decision 27 and decision 15 stand as written.

#### Versions and stand-ins

- The client and the engine of `docker` 29.10.0, with containerd v2.4.2 and runc 1.5.3,
  on the containerd image store with the overlay snapshotter; the build plugin v0.39.1
  on the default builder, whose driver is `docker`, with BuildKit v0.35.0; Compose
  v5.8.0; systemd 259. These are the versions that the first table of decision 39
  read. The host is Ubuntu 26.04.1 on Linux 7.0.0-43, linux/amd64, with AppArmor and
  without SELinux, and its engine is rootful with no remapped user namespace.
- The registry is the one that the project pins,
  `registry:3.1.2@sha256:82f6ff4a7f982960c8aa51617fea3aabc485df6e55cce80d22deb1de36f1bc3c`.
  The base image of the plugin image is
  `python@sha256:191083cdf6c7bda8c76158983163ead5d5cf7b77cf3033e805bb0f5296f51b4b`.
- Rows 4, 5, 6 and 8 were run twice: on the engine of the host with its default
  settings, and on a nested engine,
  `docker:29.10.0-dind@sha256:4bbc7223f81db790097c6dea42fb93e05b8fead4f0431fbe6ccaba6e8473201c`,
  started with the flags of the S2a exit, `--bridge none --iptables=false
  --ip6tables=false --userland-proxy=false`. The two agree. The settings of the clean
  machine of the S2b exit are not in the repository yet.
- Stand-ins. The instance is a small Python listener on port 7443, and the kernel is
  one static Go process (`go1.26.8`) as the first process of a container in the S2a
  form: `--network none`, the user of the site, the home bound read-only and `run/`
  read-write. The compose fragment, the systemd unit and the push are renderings of
  decisions 24 and 30 to 32 made for the runs, since the repository holds no template
  and no `plugin push` of remote code yet. The names of the credential files are
  stand-ins too: ADR-041 fixes them.
- Limits. Row 9 ran systemd as the first process of an unprivileged container that
  held the client of `docker` and the socket of the host's engine. It is not a host
  whose service manager is systemd, and root there is the root of that container. Not
  run: a rootless runtime or one with a remapped user namespace; an image store other
  than the containerd one; SELinux.

#### Commands and what was observed

| # | Command | Observed |
|---|---|---|
| 1 | `docker buildx build --platform linux/amd64 --provenance=false -f ctx/Containerfile --output type=oci,dest=a.tar ctx`, then `index.json` of the layout. Contrasts: the same build with no `--provenance` flag, and with `--provenance=true` | `index.json` named one descriptor of the media type `application/vnd.oci.image.manifest.v1+json`, for linux/amd64, and no index. Its blob is 1244 bytes and hashes to its digest, `sha256:ad0139412b40d065427b7e686edd35e2b599357a8c7537e50d7f4b1cb173d86b`. The build made no image and no tag in the store. With no `--provenance` flag this builder exported the same one manifest with no attestation, so the default that the row quotes from the documentation was not added here. With `--provenance=true` it exported an index of the image manifest and an attestation manifest, the case that `plugin pack` refuses |
| 2 | Each blob of the layout was uploaded and the bytes of the manifest were `PUT` to `/v2/<repository>/manifests/sha256:ad01...` of the registry, then read back with `GET`. Then `docker pull <registry>/<repository>@sha256:ad01...` and `docker run -d ... <registry>/<repository>@sha256:ad01...`, and a second image run with no pull before it | The `PUT` answered 201 and the `GET` 200, both with `Docker-Content-Digest: sha256:ad0139412b40...`. The served body was the 1244 bytes of the layout, with that hash. `docker pull` printed that digest, and `docker image inspect` gave it as the id and in `RepoDigests`. Both containers ran from their reference by digest, and the nested engine pulled and ran the image by digest as well. The push was a script of the run and not `plugin push` |
| 3 | `docker inspect <unit> --format '{{.Config.Image}} {{.Image}}'` and `docker ps --no-trunc`. Extra: a tag of the same image, and a container created from the tag | For the containers made by `docker run`, by Compose and by the systemd unit, `Config.Image` was the reference by digest and `Image` the manifest digest, and `docker ps` showed the same reference. The container made from a tag reported the tag in `Config.Image` and the manifest digest in `Image`. `Config.Image` is the string that the creator passed; `Image` is the manifest digest on the containerd image store |
| 4 | `docker network create --internal --ipv6=false -o com.docker.network.bridge.gateway_mode_ipv4=isolated -o com.docker.network.bridge.gateway_mode_ipv6=isolated <unit>`; `docker run -d --name <unit> --network <unit> --user 65532:65532 --cap-drop ALL --security-opt no-new-privileges --mount type=bind,src=<home>/remote/instances/<id>,dst=/aiops/identity,readonly <reference>`; then TCP dials from inside the instance, with a listener on every address of the host | The network reported `Internal=true`, a subnet and no gateway. The bridge of the host held no IPv4 address, only an IPv6 link-local address, which `docker network inspect` does not report. Inside the instance the one route was its own subnet with no gateway, and IPv6 was off on its interface. From inside: its own name on port 7443 connected; the default bridge of the host, the address of the host on its local network and on its overlay network, the registry, the other instance on port 7443, a public address on port 443 and the link-local address of the bridge all failed with `Network is unreachable`. Controls: the same dials connected from a container on a network that is not internal, and the host reached its own listener on the link-local address. Contrast: on an internal network in the default gateway mode the bridge held the gateway address of the network and the instance connected to the listener of the host there, as the row read in the source |
| 5 | From inside the instance, `getaddrinfo` and a raw `A` query to the name server of `/etc/resolv.conf`, for its own name, the other unit, the registry container, two public names and `host.docker.internal` | `resolv.conf` named the resolver of the runtime. The instance's own unit name answered with its address. Every other name answered `SERVFAIL` with no answer. Once the kernel container had joined, its name resolved from the instance, as a member. Control: on a network of the same daemon that is not internal a public name resolved. The kernel container, whose only networks are internal, got `SERVFAIL` for a public name too |
| 6 | `docker run -d --name K --network none ...`; `docker network connect <unit> K`; `docker network disconnect none K`; `docker network connect <unit> K`; `docker inspect K`; `docker logs K`. Extras: a second network, the stop steps of decision 37, an attach from no network at all, `docker restart K` | The connect in the no-network mode failed with the message that the row quotes. The disconnect from `none` succeeded and left no network. The connect then succeeded with no restart: the same process, the same start time, a restart count of 0. The process that booted before the attach resolved the unit name and read the instance's line on port 7443. Two limits. Started in the no-network mode, the kernel container's `resolv.conf` named the upstream of the host; the attach rewrote it to the resolver of the runtime, and the running Go process took 2.6 s to use it, while a later attach of a second network resolved in under 1 s. And `HostConfig.NetworkMode` still read `none` afterwards: `NetworkSettings.Networks` alone shows the networks. Extras: the kernel on two networks of instances dialled both and reached nothing else, and the first instance could not reach the second through it; removing a network with the kernel attached failed with `has active endpoints`, and succeeded after the disconnect; after `docker restart` the container kept the network of the instance |
| 7 | `docker compose -f <home>/remote/units/<id>/compose.yaml config -q`; `up -d` a first time, again unchanged, after a change of the image digest and after `docker stop`, each with a kernel container attached; `down`; `up -d` after `docker run` of a container of the unit name; `up -d` after `docker network create` of the unit name, isolated and then plain | The fragment: a top-level name; one service with `container_name`, the image by digest, `user: "65532:65532"`, `cap_drop: [ALL]`, `security_opt: [no-new-privileges:true]` and one read-only bind with `create_host_path: false`; one network with its name, the driver `bridge`, `internal: true`, `enable_ipv6: false` and the two options of the gateway mode; no restart policy and no port. The four hold. The container had every key of the fragment. A second run printed `Running`, with the same container. A changed digest printed `Recreate`: a new container on the same network, which the kernel process dialled, and the kernel container kept its process and its attachment throughout. Not created by Compose: with a container of the unit name, running or stopped, `up` failed with `Conflict. The container name ... is already in use` and left it untouched; with a network of the unit name, `up` printed a warning, `exists but was not created by compose`, used it and exited 0. On a plain network of that name, not internal, the instance started on it, connected to a public address and resolved a public name. Other: a stopped container that the fragment made is started again as the same container, not replaced; `down` with the kernel attached removed the container and left the network (`Resource is still in use`, exit 0); Compose adds the service name as a second name on the network; the bind is reported under `Mounts`, and `HostConfig.Binds` is null |
| 8 | `docker info --format '{{json .SecurityOptions}}'`; `docker inspect <unit> --format '{{.State.Pid}}'`; `Uid` of `/proc/<pid>/status` and `/proc/<pid>/uid_map`; `docker top <unit>` and `docker top <unit> -eo uid,gid,pid,args` | The security options listed AppArmor, seccomp and the cgroup namespace, with no rootless and no user namespace entry. `/proc/<pid>/status` of the first process showed the user and the group 65532, no effective capability and `NoNewPrivs` 1, and the `uid_map` was the identity. `docker top` showed the user 65532. On the nested engine, whose host has the `ps` of busybox, `docker top <unit> -eo uid` failed: `docker top` runs the `ps` of the host |
| 9 | `install -o root -g root -m 0644 <home>/remote/units/<id>/<unit>.service <the unit directory of the service manager>`; `systemctl daemon-reload`; `systemctl enable --now <unit>.service`; `docker inspect`; then `docker stop -t 5 <unit>`, `docker network disconnect <unit> K`, `docker network rm <unit>`, and `systemctl show` 20 s later | The unit file: `ExecStartPre=-docker network create` with the options of row 4, `ExecStart=docker run --rm` with the flags of the template and the image by digest, `ExecStop=docker stop`, no `Restart=`, `WantedBy=multi-user.target`. `enable --now` exited 0, and the unit was active and enabled with `Restart=no`. It created the network, internal with both isolated options and no gateway, and ran the container with the manifest digest, the user 65532:65532, every capability dropped, no privilege gain and the one read-only bind. The instance read its key and reached nothing outside, and the kernel container attached and dialled it. After `docker stop` the unit was `failed` (exit status 137: the stand-in ignores the termination signal), and 20 s later it was unchanged: no restart, no container, no network. Extras: `systemctl start` made the network and the container again; a start while the network existed worked, because the unit ignores a failed network create; `systemctl stop` left the network in place |
| 10 | The home of mode 0700; `remote/`, `remote/instances/` and `remote/instances/<owner>/` of mode 0700; the directory of the instance of mode 0755 with files of mode 0644, bound read-only at `/aiops/identity`; reads and writes from inside the instance as `65532:65532` with every capability dropped. Contrasts: the whole home bound into a container of the same user; the directory of mode 0700 with a file of mode 0600; the directory of mode 0755 with a file of mode 0600 | The instance listed `/aiops/identity` and read the key and the two certificates. A write failed with `Read-only file system`. No path of the home existed in the container, and its one bind was the directory of the instance, read-only. The daemon made the bind although the three directories above are of mode 0700. With the whole home bound, the user 65532 got `Permission denied` on the home, on the key through the closed directories and on `plugins.lock`. Contrasts: decision 14 to the letter, 0700 and 0600, gave `Permission denied` on the directory and on the key; 0755 with a key of mode 0600 listed the directory and gave `Permission denied` on the key. The same read worked on the nested engine and from the container that the systemd unit started |

#### What the results touch

- **Q11 stays (a).** Row 6 holds on Docker Engine 29.10.0, on the engine of the host
  and on a nested engine with the flags of the S2a exit. The condition of decision 45
  is met, and option (b) is not needed.
- **Decision 33, the order of steps 2 and 3, with decisions 31 and 36** (row 7).
  Compose uses a network of the unit name that it did not create, with a warning and
  exit 0, and on a plain network of that name the instance had a route out. As written,
  the fragment runs before the network is checked, so third-party code would run with a
  route out until the check stops it, against ADR-063 decision 24. The systemd unit of
  decision 31 has the same gap: it creates the network where it is absent, checks
  nothing of one that exists, and starts the instance before any `plugin start`.
  Question Q16.
- **Decision 43** (row 10) holds on rootful Docker, and so do the modes of ADR-041
  decision 24 there (the third run of ADR-041 decision 38). Question Q15.
- **Decision 33 with `plugin enable`** (row 6). After the first attach of a kernel
  container that started in the no-network mode, the running process kept its old
  resolver for 2.6 s. A `plugin enable` that follows `plugin start` at once can
  therefore fail its dial, and step C4 of the exit follows step C3. Question Q17.
- **Decision 33, step 2** (row 7). A stopped container that the fragment made is
  started again as the same container, where the step says "replaced". Question Q18.
- **What the part `Unit` reads** (decisions 33, 36 and 38; rows 3, 6, 7 and 8). These
  are readings of the runtime and change no decision; the code of `Unit` takes them.
  The networks of a container are read from `NetworkSettings.Networks`, since
  `HostConfig.NetworkMode` keeps `none`. The image is read from `Image`, the manifest
  digest, since `Config.Image` is the creator's string. A bind is read from `Mounts`.
  The user as the host sees it is read from the process id of the container and
  `/proc/<pid>/status`, since `docker top` depends on the `ps` of the host.
- **Decisions 32 and 36, "the host holds no address"** (row 4). In the gateway mode
  `isolated` the bridge has no IPv4 address and the network reports no gateway, and the
  bridge still carries an IPv6 link-local address that the runtime does not report and
  the instance cannot reach. Points 1 and 2 of decision 32 hold. The check of decision
  36 reads what the runtime reports: no gateway, and the gateway mode `isolated`.
- **Row 1.** On the default builder a build with no `--provenance` flag exported one
  manifest with no attestation. Decision 24 does not change: `plugin pack` asks for no
  attestation and refuses an index.
- **Decision 37 needs no change.** `docker compose down` with the kernel container
  attached leaves the network behind, so the three steps through the runtime are the
  way to stop. A unit stopped that way ends `failed` in the service manager, which
  `disable` and `remove` may say to the operator.
- **The programs are not pinned by the project yet.** The rows ran at the versions that
  the first table of decision 39 read. Each program still gets its register entry and
  its row of the licence record in the commit that first uses it (decision 39).

#### Questions for the owner after the runs

Each question gives its options, the recommended one, and why.

**Q15. Which modes does the directory of an instance have?** Decision 43 (Q9 (a)): the
directory of mode 0755, its files of mode 0644, the unit's user `65532:65532` and no
group of the host. ADR-041 decision 44 (its Q5 (a)), recorded on the same date: the
directory of mode 0750, its files of mode 0640, and the site user's group as the only
group of the instance, which is option (b) of Q9. Decision 40 has ADR-041 take the
answer to Q9, and ADR-041 has this part take its decision 24. Row 10 holds under the
first and the third run of ADR-041 decision 38 under the second, both on rootful Docker
alone. The question is the same as question Q18 of ADR-041, and one answer settles
both.
- (a) Decision 43 holds, and ADR-041 decisions 24 and 44 are revised to it.
- (b) ADR-041 decision 44 holds. Decision 43, the note at decision 14 and the user of
  decision 30 are revised: the second number of the unit's user is the group of the
  site user. By the text of Q9 it does not hold under a rootless runtime, which was not
  run.
- Recommended: (a), for the reasons of Q9: it needs nothing of the user mapping, gives
  third-party code no group of the host, and its guard, the three closed directories,
  is checked by the commands that write and use the key. Its cost stands as decision 43
  states it. Needed before the unit and `plugin install` of remote code merge, and
  before this part or ADR-041 is accepted.
- **Answered 2026-11-28: (a)**, decision 49.

**Q16. When is a network of the unit name that already exists checked?** Decision 33
runs the fragment in step 2 and makes the checks of decision 36 in step 3, and decision
31 has the systemd unit create the network "where it is absent". Row 7 and row 9: on
both targets a network of the unit name that something else made is used as it is.
- (a) The check comes first, on both targets. `plugin start` reads a network of the
  unit name before it runs the fragment and refuses, with the point named, one that
  fails the network checks of decision 36; with none, the fragment creates it and the
  checks follow as written. The systemd unit checks the same two settings of a network
  that it finds before it runs the container, and fails to start on one that does not
  have them. It edits steps 2 and 3 of decision 33, the systemd bullet of decision 31,
  decision 36 and one case of decision 41. That the unit can make this check in the
  service manager was not run, and is shown in the systemd job.
- (b) Neither template creates the network. The fragment names it as an external
  network, `plugin start` creates it through `Unit` after it has shown that none of
  that name exists, and the systemd unit starts only on a network that `plugin start`
  made. This changes "created by the unit" of decision 32 and the first start of a
  systemd unit in decision 33 and `08:1293`, which then needs `plugin start` before
  `systemctl enable --now`.
- (c) The order stands as written. An instance can then run with a route out between
  its start and the check, and on the systemd target until the first `plugin start`.
  This weakens ADR-063 decision 24 and needs the owner's word (ADR-063 decision 33).
- Recommended: (a). It closes the gap on both targets, keeps the network a thing of the
  unit and keeps the flow of `08:1293`. Needed before this part is accepted and before
  `plugin start` and the templates merge.
- **Answered 2026-11-28: (a)**, decision 50.

**Q17. What follows the first attach of the kernel container?** Row 6: for 2.6 s after
the first attach from the no-network mode the running kernel process still asked its
old resolver, so the dial that a `plugin enable` causes at once can fail. The bound of
that lag was read in no source.
- (a) Nothing in the product changes. The text of `plugin start` says that the first
  `plugin enable` after the first start may be refused once, and the exit waits between
  steps C3 and C4.
- (b) After an attach that takes the kernel container out of the no-network mode,
  `plugin start` waits before it returns, for a bound that is read in the source of the
  Go resolver before the code is written. Later attaches do not wait.
- (c) Option (b) of Q11: the kernel container starts on an internal network of its own,
  and its resolver is the runtime's from the start. This changes ADR-063 decision 27
  and decision 15.
- Recommended: (b). The command that `plugin enable` prints is then true the first time
  it is followed, and no written line changes. Needed before `plugin start` merges; it
  is the S2b part of ADR-061 that words the command.
- **Answered 2026-11-28: (b)**, decision 51.

**Q18. Is a stopped container that the fragment made started again or replaced?**
Decision 33, step 2: "One that is stopped or differs is replaced by the run of the
fragment when the fragment made it". Row 7: Compose starts a stopped container of an
unchanged fragment again as the same container.
- (a) The step says "started again or replaced": a stopped container of an unchanged
  fragment is started again, and the checks of decision 36 are made on it as on any
  other.
- (b) `plugin start` forces a new container at every start of a stopped one.
- Recommended: (a). The checks of decision 36 hold the container to the rendered unit
  either way, and (a) is what the runtime does. Needed before `plugin start` merges.
- **Answered 2026-11-28: (a)**, decision 52.

**Also taken by the revision of this part,** from answers that the owner gave on other
ADRs on 2026-11-27, and no question here:
- the endpoint table of decision 29 gains the attempt value of a line, and `plugin
  enable` as its writer (ADR-041 decision 42);
- the text that carries the rule on when `sidecars-static` reads the table again is
  question Q18 of the S2b part of ADR-061, whose recommended option is decision 29;
- the table of decision 28 names `plugin start` as the command that makes a new pair
  and writes the pin after a revocation (ADR-041 decision 50), and the command that
  renews the kernel's pair when the S2b part of ADR-061 has named it (ADR-041 decision
  43).

Note of 2026-11-28: decision 53 takes the first two. The third stays with the commit
that marks this part accepted, with the other writers of the table of decision 28.

#### Edits made with this record, and left

Made: the status of this part; the two columns and a note in the table of decision 39;
a note at decision 45; and this section. The row of ADR-047 in the ADR table of `08` §3
names the S2b part as open with the owner. The edits of the section "Edits on acceptance
of the S2b part" that no chosen option names are not made: they stay with the commit
that marks the part accepted, after the answers to Q15 and Q16.

### Owner answers of 2026-11-28

The project owner answered the four questions of the section "Questions for the owner
after the runs" on 2026-11-28, each with its recommended option: (a) for Q15, Q16 and
Q18, and (b) for Q17. On the same date the owner answered question Q18 of the S2b part
of ADR-061 with its option (a): this part carries, in its decision 29, the rule on when
`sidecars-static` reads the endpoint table again. Decisions 49 to 52 each name the
question they answer, and decision 53 writes what that fifth answer puts into decision
29. This record does not mark the part accepted, and it holds the result of no run.

49. **The directory of an instance keeps the modes of decision 43** (Q15 (a)). Decision
    43 holds as the owner gave it on 2026-11-27: the directory `remote/instances/<plugin
    id>/` has the mode 0755 and its files the mode 0644, the user of a unit is the pair
    `65532:65532`, the instance holds no group of the host, and the guard on the host is
    the three directories of mode 0700 above it, which `aiopsctl` checks and sets
    (decision 30). The answer is one with the owner's answer to question Q18 of ADR-041,
    option (a): ADR-041 decisions 24 and 44, which gave the directory the mode 0750, its
    files the mode 0640 and the instance the site user's group, are revised to decision
    43, in the record of that answer in ADR-041. No line of this part changes: decision
    43, the dated note at decision 14 and the user of decision 30 stand, and the cost
    stands as decision 43 states it. That revision is what decision 40 asks of ADR-041
    on these modes. Row 10 of decision 39 was run under these modes on rootful Docker
    alone. A rootless runtime with the user kept and a host that enforces SELinux are
    rows of the third run of ADR-041 decision 38, and by the owner's answer to question
    Q19 of ADR-041 they are run before that ADR is accepted. Their record is ADR-041's,
    and a row that these modes do not pass comes back to the owner (ADR-041 decision
    53). Note of 2026-11-28, after the runs of that date: both rows were run and the
    modes pass them. The words "the instance holds no group of the host" do not hold
    under rootless Podman with the user kept, and they stand as written until the
    owner answers question Q22 of the section "Runs after the answers of 2026-11-28".

50. **A network of the unit name that already exists is checked before anything runs on
    it, on both targets** (Q16 (a)).
    - **`plugin start`.** Before it runs the fragment, `plugin start` reads a network of
      the unit name as the runtime reports it and refuses, with the network and the
      point named, one that fails a network check of decision 36: a network that is not
      internal, one in which the host holds an address, and one with a member that is
      neither the instance nor the kernel container of this home. That refusal starts
      nothing. With no network of that name the fragment creates it, and the checks of
      step 3 of decision 33 follow as written, as they do on a network that existed and
      passed.
    - **The systemd unit.** Before it runs the container, the unit checks a network of
      the unit name that it finds for the two settings of decision 32: that the network
      is internal, and that its gateway mode is `isolated`. It fails to start on a
      network that does not have them, and it still creates the network where none
      exists. That a unit can make this check in the service manager was not run, and
      the systemd job of ADR-063 decision 50 shows it. The members of the network are
      read by `plugin start`, which the operator runs after `systemctl enable --now`
      (decision 33). Note of 2026-11-28, after the runs of that date: the check was
      run in a unit of a system's service manager and holds (rows N-UNIT and N-BAD-NET
      of the section "Runs after the answers of 2026-11-28"). The systemd job still
      proves it on the rendered unit.
    - An instance that `plugin start` or the systemd unit starts is then never on a
      network of the unit name that gives it a route out or an address of the host,
      which ADR-063 decision 24 needs and which the written order did not give (rows 7
      and 9 of decision 39). The network stays a thing of the unit (decision 32), and
      the flow of `08:1293` stands.
    - The answer edits steps 2 and 3 of decision 33, the systemd bullet of decision 31,
      the network check of decision 36 and one case of decision 41. Each gains a dated
      note, and no line of `docs/architecture/` changes.

51. **After an attach that takes the kernel container out of the no-network mode,
    `plugin start` waits before it returns** (Q17 (b)). Row 6 of decision 39: for 2.6 s
    after the first attach, the kernel process that started with no network still asked
    its old resolver, so the dial that a `plugin enable` causes at once could fail. After
    an attach that first detached the kernel container from the no-network mode
    (decision 45), `plugin start` therefore waits for a bound and then returns. The
    bound is read in the source of the Go resolver before the code of `plugin start` is
    written. This record holds no value for it: the reading is recorded by a dated note
    at this decision, with the file and the toolchain that were read. An attach of a
    kernel container that already holds a network of an instance does not wait. The
    command that `plugin enable` prints is then true the first time it is followed, and
    step C4 of the exit, which follows step C3, rests on that. No written line changes:
    ADR-063 decision 27, decision 15 and decision 45 stand. What `plugin start` prints
    while it waits is the S2b part of ADR-061's. This adds to step 4 of decision 33.
    Note of 2026-11-28 (the reading, recorded in the section "Runs after the answers of
    2026-11-28", rows G-SRC to G-GRPC): the bound is 5 s.
    - It was read in `net/dnsclient_unix.go` of Go `go1.26.8`, the toolchain that
      `go.mod` names: `tryUpdate` of the resolver's configuration checks `resolv.conf`
      again at most once in 5 s, and only a lookup makes the check. The released
      binary is built with `CGO_ENABLED=0` and always holds that resolver.
    - The 5 s count from the rewrite of `resolv.conf`, which the connect of the
      runtime makes before it returns. So `plugin start` waits a fixed 5 s after the
      connect that takes the kernel container out of the no-network mode has
      returned, and then returns. The lag itself is anywhere from nothing to just
      under 5 s, by when the process last looked a name up; the 2.6 s of row 6 of
      decision 39 is one sample.
    - The hosts file and `nsswitch.conf` have checks of 5 s of their own and do not
      hold the name of an instance.
    - The wait covers the standard resolver. It does not cover a channel of `grpc-go`
      that is already in the backoff of its `dns` resolver, which was read and not
      run. A generation that is submitted after `plugin start` has returned dials
      after the wait and does not meet that case.
    - The value is that of `go1.26.8`.

52. **A stopped container that the fragment made is started again or replaced** (Q18
    (a)). Step 2 of decision 33 reads "started again or replaced" where it says
    "replaced". Compose starts a stopped container of an unchanged fragment again as the
    same container (row 7 of decision 39) and replaces one whose fragment changed, and
    `plugin start` forces no new container. The checks of decision 36 are made on a
    container that was started again as on any other, so it is held to the rendered unit
    in its image reference by digest, its user, its one bind and its network either way.
    A container that the fragment did not make is refused as written.

53. **The endpoint table: the attempt value of a line, its writers, and when
    `sidecars-static` reads the file again.** This adds to decision 29. The owner's
    answer to question Q18 of the S2b part of ADR-061, option (a), has decision 29 carry
    the rule on when `sidecars-static` reads the endpoint table again. ADR-041 decision
    42, with its decisions 18 and 39, needs two things of the table: an attempt value in
    each line, and `plugin enable` as a writer.
    - **The line.** An object of `endpoints` has three members: `name` and `address`, as
      decision 29 has them, and `attempt`, a string of 32 lower-case hexadecimal digits
      that its writer takes from the random source of the system. `tableVersion` stays
      1, since no code reads or writes the file yet. The name and the form of `attempt`
      are values that no written line gives: they are proposed here, and the owner
      accepts them with the part. That the value is random is ADR-041 decision 18, and
      it has to be: a `plugin enable` that is rolled back restores the old value, so a
      counter would give the next `plugin enable` the value of the one that was rolled
      back, and the report made against that value would still hold.
    - **What reads the value.** `Endpoint(name)` of the definition `sidecars` returns
      the address and the attempt value of the line, a consumer that got no answer says
      so with `Report(name, attempt)`, and the provider holds a report only against the
      attempt value that it names (ADR-041 decision 18). A reader compares the value
      for equality and reads nothing else in it. `aiopsctl` writes it and never reads
      it.
    - **The writers.** Install writes the line with a first value. `plugin start`
      writes a missing line, with a new value, and leaves a line that exists as it is
      (ADR-061 decision 60), so it clears no report by itself (ADR-041 decision 18).
      `plugin enable` of a remote row replaces the value in the line of the row's
      plugin before it submits (ADR-041 decisions 18 and 42). It writes the file whole,
      under `write.lock` and with the snapshot, as every writer of the table does
      (decisions 28 and 29), so a `plugin enable` that is rolled back restores the line
      with the other files (ADR-062 decision 23). `remove` deletes the line (decision
      35). The row of the table in decision 28 names `plugin enable`. How `Enable` makes
      the write, and whether it makes it while no process runs the profile (ADR-061
      decision 66), is the S2b part of ADR-061's.
    - The table is of the site (decision 28), so a `plugin enable` in one profile
      replaces the value that the processes of every profile of the site read. A
      process of another profile that holds a report against the old value dials once
      more at its next check.
    - **When the row reads the file again.** `sidecars-static` reads the file after
      boot, "at every generation or at every lookup, and not once at boot" (ADR-061
      decision 60). What that answer and ADR-041 decision 42 require together is fixed
      here, and it does not depend on the point that question Q19 leaves open:
      1. The row reads the file each time the kernel runs the availability check of
         `sidecars` for a consumer: the check that a generation runs, and the check
         that the row itself asked for when it offers an endpoint again on its timer
         (ADR-041 decisions 18 and 49). It answers that check from that read and from
         no earlier one.
      2. A write of the table that returned before a command submitted a generation is
         therefore in every answer that the row gives for that generation, in a
         process that booted before the write: the kernel that serves step C4 of the
         exit booted before install wrote the line in step C2 (ADR-063 decision 27).
      3. At that check the row compares the attempt value of the line with the value
         of a report that it holds for the instance, so the generation of a new
         `plugin enable` dials at once (ADR-041 decision 42).
      4. The row opens the path at each read and keeps no open file from one read to
         the next. `aiopsctl` replaces the file by a rename (decision 29), and a file
         held open would be the one that was replaced. An absent file is an empty
         table at every read, and not at the first alone.
      5. The row does not watch the file. A write that no generation and no check
         follows is read at the next check.
    - Whether `Endpoint(name)` reads the file again or answers from the read of the
      check is question Q19 below. The owner's answer names both points and chooses
      neither, and nothing above needs the choice.
    - Of the three things that the last bullet of decision 29 leaves to the row, this
      decision takes the second. How the row learns the path of the file and what it
      calls ready, with what it answers on a file that it cannot read or parse, stay
      the row's, with the definition `sidecars` (ADR-063 decision 39).
    - **Cases,** beside those of decision 41, each with a test that fails on its base
      (ADR-063 decision 29): a server process that booted on a home whose table lacked
      a line answers from that line at the generation that follows its write; `plugin
      enable` of a remote row leaves the line with another attempt value; and a `plugin
      enable` that is rolled back leaves the value that the line had before it.

#### Question for the owner after the answers of 2026-11-28

The question gives its options, the recommended one, and why.

**Q19. Does `Endpoint(name)` read the endpoint table again, or answer from the read of
the availability check?** The owner's answer to question Q10 of the S2b part of ADR-061
(its decision 60) has `sidecars-static` read the table "at every generation or at every
lookup, and not once at boot". It names two points and chooses neither. Decision 53
fixes the first, which ADR-041 decision 42 needs in any case: the row reads the file at
every availability check of `sidecars`, and a generation runs that check. The lookup,
`Endpoint(name)`, is open.
- (a) At every answer. The row keeps no copy of the table: it reads the file at each
  availability check and again at each `Endpoint(name)`. A consumer that looks an
  endpoint up long after its check gets the line as the file then has it. The cost is
  one read of a small file for each lookup. Where a write falls between a check and the
  lookup that follows it, the two see different files; the report then names the
  attempt value that the lookup gave, so the report still holds against the right
  value.
- (b) At the availability check alone. `Endpoint(name)` answers from the read of the
  check that admitted the consumer, so the address and the attempt value that a
  consumer gets are the ones that the check judged. The row holds a copy of the table
  from one check to the next, and a consumer that looks an endpoint up again later gets
  the line of its check.
- Recommended: (a). The row then holds nothing of the table that can be out of date,
  and needs no rule on when a copy is dropped. It is both halves of the answer as the
  owner worded it, and its cost is the read of one small file. Needed before
  `sidecars-static` merges; it blocks no other code of this part.

#### Edits made with this record, and left

Made, in this file: the status of this part, at the head of the file and at the head of
the part, with a dated note at its list of proposed values; in the table of decision 28,
`plugin enable` in the row of `sidecars.json`; dated notes at decision 29 (decision 53),
at the systemd bullet of decision 31 (decision 50), at the steps of `plugin start` in
decision 33 (decisions 50 to 52), at the network check of decision 36 (decision 50) and
at the case of the refusals of `plugin start` in decision 41 (decision 50); the answer
under each of the questions Q15 to Q18; a note at the list "Also taken by the revision
of this part"; and this section. The row of ADR-047 in the ADR table of `08` §3 names
the record of 2026-11-28 and no longer says that the part awaits the owner on two
points. No other line of `docs/architecture/` is edited: no chosen option names one.

Not edited here, each with the record that edits it:

- ADR-041 decisions 24 and 44, which the answer to Q15 has revised to decision 43: the
  record of the owner's answer to question Q18 of ADR-041, in that ADR;
- the sentence of ADR-061 decision 60 on the rule "until a text carries" it, and the
  row of ADR-061 decision 59 on when `sidecars-static` reads the endpoint table: the
  record of the owner's answer to question Q18 of that part, in that ADR;
- ADR-061 decision 45, which follows decisions 50 to 52 for `plugin start`, with the
  texts of the refusals of decision 50 and what the command prints while it waits: a
  dated note in that ADR before `Start` merges, as its section "Points for the owner
  after the records of 2026-11-27" says.

Left to later commits, each named:

- the commit that marks this part accepted: every edit of the section "Edits on
  acceptance of the S2b part" that no chosen option names, as the record of 2026-11-27
  lists them; and, in the table of decision 28, the writers that the owner's answers on
  ADR-041 add, which are `plugin start` as the command that makes a new pair and writes
  the pin after a revocation (ADR-041 decision 50) and the command that renews the
  kernel's pair, once the S2b part of ADR-061 has named it (ADR-041 decision 43);
- the bound of decision 51, by a dated note there, before the code of `plugin start` is
  written;
- the answer to question Q19, before `sidecars-static` merges;
- the definition `sidecars` beyond `Endpoint` and `Report`: its key, its fake, its
  suite, how a provider tells which instance a consumer waits for, how the row learns
  the path of the file and what it calls ready. ADR-041 decisions 36 and 39 send those
  points to this part, decision 29 leaves the last two to the row, and no text fixes
  them yet. They are an addition to this part, due before `sidecars-static` and the
  proxy merge.

Note of 2026-11-28, after the runs of that date: the section "Runs after the answers of
2026-11-28" holds the bound of decision 51 and the run of the systemd unit's check of
decision 50, the two points that this record left to a reading and to a run. It also
holds four questions that the runs raise, Q20 to Q23.

### Runs after the answers of 2026-11-28

Note of 2026-11-28. Two throwaway runs were made on that date, after the owner's answers
of the same date, on the tree of commit `84c7deb`. They committed nothing, and every
machine, container, network, image and file that they made was removed. The first was
made in the throwaway virtual machine of ADR-041 decision 58 and shows what decision 50
left unrun: that the systemd unit can check a network that it finds, in a service
manager. The second is the reading that decision 51 asks for, the bound of the Go
resolver, with runs that reproduce it. A row "holds" when the run showed what this part
writes, "fails" when it showed otherwise, and nothing is inferred for what was not run.
The rows of the first run that bear on the modes of the directory of an instance are
recorded in ADR-041, in its section of the same name, and are cited here by their names
there.

**The part is not marked accepted by this record.** The owner's answer to Q16 can be
built on the systemd target as worded, and the answer to Q17 can be built as worded:
the bound is 5 s. The modes of decision 43 hold under rootless Podman and under SELinux
enforcing once the bind carries a label (ADR-041, rows R3-PODMAN-43 and
R3-SELINUX-ROOTLESS). Five points keep the part proposed, and each is the owner's:

- question Q19, on the lookup of `sidecars-static`, which is not answered yet;
- the label of SELinux cannot be written into the bind forms that the runs of decision
  39 recorded, so an instance on an enforcing host whose runtime confines cannot read
  its key as those forms stand: question Q20;
- the `z` option of decision 14 on the kernel container's bind of the home relabels
  the credential directories with the rest of the home: question Q21;
- the words "no group of the host" of decision 49 do not hold under rootless Podman
  with the user kept: question Q22, which is also question Q23 of ADR-041;
- on Podman the internal network of a unit gave the host an address in it, in one
  reading: question Q23.

#### Versions and stand-ins

- **The first run** used the machine that the section "Runs after the answers of
  2026-11-28" of ADR-041 describes: Fedora Cloud Base 45 under QEMU with KVM, SELinux
  enforcing with the targeted policy, kernel 7.2.11-300.fc45, and systemd 262-3 as the
  first process of the machine, where row 9 of decision 39 ran systemd 259 as the
  first process of a container. Docker Engine and client 29.10.0, containerd 2.4.2 and
  runc 1.5.3, with `selinux-enabled` set in the machine and with the default network
  settings of the daemon, not the flags of the S2a exit. Podman 6.1.3 with netavark
  2.1.0 and aardvark-dns 2.1.0, for the last row.
- **The instance** of the first run is a stand-in: an image built in the machine from
  `python@sha256:191083cdf6c7bda8c76158983163ead5d5cf7b77cf3033e805bb0f5296f51b4b`, with
  a listener on port 7443 that reads its key at its start, run by its local digest.
  The pinned registry was not used. The unit name is the form of decision 46 for an id
  made for the run.
- **The unit** is the unit of row 9 with one line more, rendered by hand: no template
  exists yet. `Requires=docker.service` and `After=docker.service`; a first
  `ExecStartPre=-/usr/bin/docker network create --internal --ipv6=false -o
  com.docker.network.bridge.gateway_mode_ipv4=isolated -o
  com.docker.network.bridge.gateway_mode_ipv6=isolated <unit>`, whose failure is
  ignored; a second `ExecStartPre`, the check; `ExecStart=/usr/bin/docker run --rm
  --name <unit> --network <unit> --user 65532:65532 --cap-drop ALL --security-opt
  no-new-privileges -v <home>/remote/instances/<id>:/aiops/identity:ro,Z <image by its
  digest>`; `ExecStop=/usr/bin/docker stop <unit>`; no `Restart=`; and
  `WantedBy=multi-user.target`.
- **The check** is a shell line. It reads `docker network inspect --format
  '{{.Driver}}/{{.Internal}}/{{.EnableIPv6}}/{{range $k, $v :=
  .Options}}{{$k}}={{$v}};{{end}}/{{range .IPAM.Config}}{{if
  .Gateway.IsValid}}{{.Gateway}}{{end}}{{end}}' <unit>` and compares the result with
  the one string of a bridge network that is internal, has IPv6 off, has exactly the
  two gateway-mode options and has no gateway. On a difference it writes `network
  <unit> is not the network of the unit: <what it read>` and exits 1.
- **The second run** was made on the local machine: Ubuntu 26.04.1 on Linux 7.0.0-44,
  Docker Engine 29.10.1, rootful, where the runs of decision 39 used 29.10.0, under
  AppArmor and without SELinux. The source read is the package `net` of Go `go1.26.8`,
  the toolchain that `go.mod` names. The probe is a Go program on the standard library
  alone, built with the environment and the flags of `internal/builder/build.go`:
  `CGO_ENABLED=0` and `go build -trimpath -tags=grpcnotrace`. It ran in the root
  filesystem of the Python image above, Debian 13 with `hosts: files dns` in its
  `nsswitch.conf`, and not in the site image that `dist build` writes.

#### The systemd unit and the check of its network (decision 50)

| Row | What was run | Versions | Command | Result | Observed |
|---|---|---|---|---|---|
| N-UNIT | A unit of the machine's service manager, copied by root, creates the network where it is absent, checks the network that it finds, and starts the instance | systemd 262-3 as the init of Fedora 45; Docker Engine 29.10.0 with `selinux-enabled` | `install -o root -g root -m 0644 <home>/remote/units/<id>/<unit>.service <the unit directory of the service manager>`; `systemctl daemon-reload`; `systemctl enable --now <unit>.service` | holds | `systemd-analyze verify` had no complaint. The file in the home is the site user's, of mode 0600 and type `user_home_t`; root's copy is `root:root`, of mode 0644 and type `systemd_unit_file_t`. With no network of the name: `enable --now` exited 0, and the unit was active and enabled. The network was internal, with IPv6 off, both gateway modes `isolated`, a subnet, no gateway and one member. The container ran from the digest of the image with the user `65532:65532`, every capability dropped, no privilege gain and one read-only bind of mode `ro,Z`; the host saw its first process as `65532:65532` with no effective capability, `NoNewPrivs` 1 and a label `container_t` with a category pair; and the journal held the line of the instance that it read its key. With a network of the unit's settings made by hand beforehand: the create line failed with `already exists`, which the unit ignores, the check passed and the unit started. With a stand-in of the kernel container attached, started with `--network none`, detached from `none` and connected, the stand-in dialled the instance; `systemctl restart` with that member on the network passed the check and started, and the stand-in dialled again. The check needs a shell in the unit, and the unit name is written into the shell line. Not tested: the time between the check and `docker run`, which names the network by its name again. The unit does not read the members of the network |
| N-BAD-NET | The unit fails to start on a network of its name that exists before the start and is not the network of decision 32: one that is not internal; one that is internal in the default gateway mode, where the host holds the gateway address; and one that is `isolated` for IPv4 alone, with IPv6 on. Contrast: the unit of row 9 as recorded, with no check | systemd 262-3; Docker Engine 29.10.0 | `docker network create <unit>`; `systemctl start <unit>.service`; `systemctl show <unit>.service -p ActiveState -p SubState -p Result`; `docker ps -a --filter name=<unit>`; `journalctl -u <unit>.service`. The same after `docker network create --internal <unit>`, and after `docker network create --internal --ipv6 -o com.docker.network.bridge.gateway_mode_ipv4=isolated <unit>`. Contrast: the unit without its second `ExecStartPre` line, on the first two networks, then dials from inside the instance | holds | All three: `systemctl start` exited 1, the unit was `failed` with the result `exit-code`, no container was created and `ExecStart` did not run. The journal held the reason each time, the line of the check with what it read: a network that is not internal, with a gateway; one that is internal with no option and a gateway; and one that is internal with IPv6 on, the one option and a gateway for IPv6; then `Control process exited, code=exited, status=1/FAILURE` and `Failed to start`. Contrast, the unit with no check: on the plain network it started, and from inside the instance a listener on the address of the machine, a listener on the address of the default bridge and a public address on port 443 all connected, and a public name resolved; on the internal network in the default gateway mode it started and connected to a listener of the host on the gateway address of the network. This is the gap that question Q16 names, shown on a real init, and the check closes it |
| N-NO-ROUTE | The instance that the unit starts on a good network has no route out (decision 32, points 1 to 3) | Docker Engine 29.10.0 with its default network settings | A listener of the host on every address, port 8099. `docker exec <unit> python -I probe.py` with dials to its own name on port 7443, to the address of the default bridge and the address of the machine on port 8099 and to a public address on port 443, and lookups of its own name, of a public name and of `host.docker.internal`; the addresses of the bridge of the network on the host. Control: the same dials from a container on a network that is not internal | holds | Inside the instance the one route is its own subnet with no gateway, and `resolv.conf` names the resolver of the runtime. Its own name on port 7443 connected, and its name resolved. The address of the default bridge, the address of the machine and the public address: `Network is unreachable`. The public name and `host.docker.internal`: no answer, `Temporary failure in name resolution`. The bridge of the network held no IPv4 address on the host and one IPv6 link-local address, as row 4 of decision 39 recorded. Control: from a container on a plain network the same three dials connected and the public name resolved, and the host reached its own listener |
| N-UNIT-SELINUX | The same unit with the bind in the `--mount` form that rows 4 and 9 of decision 39 recorded, on a daemon that confines; and with the directory labelled beforehand | systemd 262-3; Docker Engine 29.10.0 with `selinux-enabled` | The unit with `--mount type=bind,src=<home>/remote/instances/<id>,dst=/aiops/identity,readonly` in its `ExecStart`; `systemctl start <unit>.service`. Then `chcon -R -t container_file_t -l s0 <home>/remote/instances/<id>` and `systemctl start` again | fails | With the `--mount` form the check passed and the container started; the instance could not read its key and exited 3. The journal held its line with `Permission denied`, the unit ended `failed`, and the audit log held `avc: denied { read } for comm="python" name="key.pem" scontext=system_u:system_r:container_t:s0:c289,c971 tcontext=unconfined_u:object_r:user_home_t:s0 tclass=file`. `systemctl start` itself exited 0, since the failure comes after the start. With the directory labelled beforehand the same unit started and read the key, and with the form `-v ...:ro,Z` of N-UNIT it started and read the key. The form that the runs of decision 39 recorded for the unit does not work on an enforcing host whose runtime confines |
| N-PODMAN | Outside decision 50, one reading: whether an internal network of Podman gives the host an address that an instance reaches (decision 32, point 2) | Podman 6.1.3, netavark 2.1.0, aardvark-dns 2.1.0, rootful and rootless | `sudo podman network create --internal <network>`; `sudo podman run --rm --network <network> <image> python -I -c <dials to the gateway address of the network and to the address of the machine on port 8099, and to a public address on port 443>`, with a listener of the host on every address, port 8099; the same without `sudo` | fails | Both networks reported `internal`, the driver `bridge`, their resolver on and a gateway address. Rootful: the dial to the gateway address connected to the listener of the host; the address of the machine and the public address failed with `Network is unreachable`. Rootless: the gateway address answered `Connection refused`, since that address exists in the network namespace of the rootless runtime, where the listener of the host is not; the two other dials failed with `Network is unreachable`. The network that Podman made from the compose fragment of the run reported the same, a gateway address with its resolver on, so the two gateway-mode options of the fragment change nothing on Podman (ADR-041, row R3-PODMAN-TARGETS). No setting of Podman that removes the address was looked for |

#### The bound of the resolver (decision 51)

`<unit>` is the network and the instance of the run, made with the options of row 4 of
decision 39 and the flags of the unit. Each run row repeats this twelve times: `docker
run -d --network none --user <uid of the site user>:<its gid> -e MODE=<mode> ... <image>
/probe <unit>`, then `docker network disconnect none <container>` and `docker network
connect <unit> <container>`, as row 6 of decision 39.

| Row | What was run | Versions | Result | Observed |
|---|---|---|---|---|
| G-SRC | The bound, read in the source of the Go resolver: `net/dnsclient_unix.go`, `dnsconfig_unix.go`, `dnsconfig.go`, `conf.go`, `hosts.go`, `nss.go` and `cgo_stub.go` | Go `go1.26.8`, the toolchain of `go.mod`; the files are the same bytes in the installed toolchain and in the module `golang.org/toolchain@v0.0.1-go1.26.8.linux-amd64` | holds | The process first reads `resolv.conf` at its first lookup, not at its start (`resolverConfig.init`). Only a lookup reads it again: `getSystemDNSConfig` calls `tryUpdate`, which returns at once if the last check was less than 5 s ago (`dnsclient_unix.go:402`), and otherwise reads the modification time of the file and parses the file again only if that time changed. There is no timer and no watch of the file. The option `no-reload` of `resolv.conf` turns the re-read off. So a lookup that starts less than 5 s after the last check uses the old servers, and every lookup that starts 5 s or more after the rewrite uses the new ones; one that races the lookup that reloads may still take the old configuration. A process that made no lookup in the 5 s before the rewrite has no lag. A lookup already in flight keeps its configuration, for up to 5 s an attempt and two attempts a server and a search name if the old server is silent. The hosts file has its own expiry of 5 s (`hosts.go`), and `nsswitch.conf` its own check of 5 s (`nss.go:59`). The longest time after the rewrite at which a lookup can still start on the old configuration is just under 5 s |
| G-BUILD | Which resolver the released binary holds | `internal/builder/build.go` at `84c7deb`; Go `go1.26.8` | holds | The builder sets `CGO_ENABLED=0` and the one tag `grpcnotrace`, and no tag `netgo`. With cgo off `net/cgo_stub.go` is compiled, so the process always uses the resolver written in Go, with the order files and then DNS, and `GODEBUG=netdns=cgo` is ignored. The probe built with the same environment and flags reports `go1.26.8`, `-tags=grpcnotrace`, `-trimpath=true` and `CGO_ENABLED=0` and is a static executable |
| G-POLL | Row 6 of decision 39 again: a Go process that looks the name of the instance up every 20 ms from its start, in a container started with `--network none` and then attached to the network of decision 32; twelve repeats, the attach after 1 to 9 s chosen at random | The probe above; Docker Engine 29.10.1 | holds | 12 of 12 resolved with no restart. The first lookup that succeeded came 1.87 to 4.62 s after the connect returned: 1.87, 1.94, 2.40, 3.11, 3.16, 3.50, 3.56, 3.98, 4.00, 4.29, 4.40 and 4.62 s. The 2.6 s of row 6 is one sample of this spread. In every repeat the success came 5.005 to 5.033 s or 10.006 to 10.033 s after the first lookup of the process, which is the check of `tryUpdate`. Started with no network, `resolv.conf` named the upstream server of the host; the connect rewrote it to the resolver of the runtime, 32 to 44 ms after the connect started and before it returned, and the connect took 76 to 110 ms. Lookups on the old configuration failed in under 3 ms with `connect: network is unreachable` |
| G-PRIME | The worst case of the bound: one lookup just before the attach, and none after it until the connect has returned; twelve repeats | As G-POLL | holds | 12 of 12: the first success came 4.909 to 4.935 s after the connect returned, 4.958 to 4.982 s after the rewrite of `resolv.conf`, and 5.001 to 5.019 s after the one earlier lookup. About 240 lookups failed in between, each in under 2 ms. This is the bound as read: just under 5 s after the rewrite |
| G-IDLE | A process that makes no lookup before the attach; twelve repeats | As G-POLL | holds | 12 of 12: the first lookup succeeded, 6 to 14 ms after the connect returned and 49 to 92 ms after the rewrite. There is no lag: the process reads `resolv.conf` for the first time at that lookup |
| G-GRPC | What a channel of `grpc-go` adds to the standard resolver when its first resolution falls inside the window | `google.golang.org/grpc` v1.82.1, read in `internal/resolver/dns/dns_resolver.go`, `backoff/backoff.go` and `dialoptions.go` | not run | Read alone. `grpc.NewClient` takes the `dns` scheme by default. The resolver of that scheme resolves again after a failure on its own backoff, from 1 s by a factor of 1.6 to 120 s, and no sooner than 30 s after a success. ADR-041 names no target scheme for the dial of the proxy. A channel whose first resolution failed inside the 5 s can therefore succeed later than 5 s after the attach. A wait in `plugin start` before any dial avoids the case |

#### What the results touch

- **Decision 50, the systemd unit: no change.** A unit of the system's service manager
  checks a network that it finds before it runs the container, and fails to start on
  one that is not the network of decision 32, with the reason in the journal and no
  container created (N-UNIT, N-BAD-NET). The instance that it starts on a good network
  has no route out (N-NO-ROUTE). The point that decision 50 left to the systemd job is
  shown on a real init; the job still proves it on the rendered unit.
- **Decision 50 and ADR-063 decision 25.** The check of the run is a shell line with
  the unit name written into it. ADR-063 decision 25 has every input of a template
  written by an encoder and never spliced into text. The name that the line holds is
  the derived name of decision 46 and no input of the artifact as it stands. Whether a
  shell line with that name fits ADR-063 decision 25, or the check takes another form,
  is for the text of the template in the S2b part of ADR-061; it is named here so that
  the template does not take the line of the run unread.
- **Decision 51: the bound is 5 s.** It is the interval at which the Go resolver of
  the pinned toolchain checks `resolv.conf` again, and only a lookup makes the check
  (G-SRC). The released binary always holds that resolver (G-BUILD). The lag after
  the first attach is anywhere from nothing to just under 5 s, by when the process
  last looked a name up (G-POLL, G-PRIME, G-IDLE), so the 2.6 s of row 6 is a sample
  and no bound. The decision gains the value by a dated note, and no option of Q17 is
  reversed. A poll in place of the fixed wait would need a new way to ask the kernel
  process to look a name up, and the fixed wait is already the worst case.
- **Decision 30 with decision 14, the label of the bind** (ADR-041, rows
  R3-SELINUX-ROOTLESS and R3-SELINUX-FORMS; N-UNIT-SELINUX). Under SELinux enforcing,
  on a runtime that confines, the instance reads its directory only when the bind or
  the directory carries a label, as decisions 14 and 30 write. The forms in which the
  runs of decision 39 wrote the bind cannot carry it on Docker 29.10.0 and Compose
  5.8.1: `docker run --mount` has no option for it, and the long form of Compose drops
  `selinux: Z`, with no message, when `create_host_path: false` is set. As recorded,
  an instance on such a host cannot read its key. Question Q20.
- **Decision 14, "the `z` option", with decision 43** (ADR-041, row
  R3-SELINUX-RENDER). The bind of the home whole with `:z` can be built, and the
  kernel reads. Its effect is in no text: it relabels every file of the home to the
  shared label, the kernel's key, the pins and every instance's directory among them,
  so SELinux then separates no instance from another's key or from the kernel's, and
  a private label of a running instance is removed. The same bind with `:Z` makes
  every running instance lose its key until it is started again. Question Q21.
- **Decision 49, "the instance holds no group of the host"** (ADR-041, rows
  R3-PODMAN-43 and R3-PODMAN-TARGETS). Under rootless Podman with `--userns=keep-id`
  the runtime adds the site user's group to the process of the instance. The read is
  not affected, and the group reached nothing. The words are those of decision 49 and
  of the recommendation of Q9, and decision 43 does not hold them: its sentence, that
  the second number of the unit's pair is not the group of the site user, holds under
  this mapping too, since the runtime adds the group and the unit names none.
  Question Q22.
- **Decisions 14, 32 and 33, a unit on Podman** (N-PODMAN; ADR-041, row
  R3-PODMAN-TARGETS). Decision 14 has rootless Podman with the user kept qualify for
  the kernel container, and decision 33 has the kernel and the instances be
  containers of one runtime. On Podman 6.1.3 an internal network has a gateway
  address, a rootful instance connected to a listener of the host on it, and the two
  gateway-mode options of the template change nothing. By decision 36 `plugin start`
  refuses that network, so as the texts stand a site whose kernel container runs on
  Podman can start no unit. The modes of decision 43 hold on Podman; the network of a
  unit there is shown by no run. This is one reading. Question Q23.
- **Decision 36 reads no setting of SELinux.** Docker as its package installs it on
  Fedora 45 enables no SELinux in the daemon: every container runs unconfined, and the
  enforcing host does not confine the instance at all (ADR-041, row
  R3-SELINUX-DOCKER). No check of decision 36 reads that setting of the runtime, and
  no decision asks it to. It is recorded for the answer to question Q20.
- **Decision 43, the guard, and decision 30, the user as the host sees it: no
  change.** Another local user reads nothing through the closed directories (ADR-041,
  row R3-OTHER-USER). Under three rootless mappings the instance is the site user on
  the host, and under one of them the inside shows 65532, so the read from the host
  that decision 30 gives `plugin start` is what finds it (ADR-041, row R3-PODMAN-43).

#### Limits of the runs

- The limits of the first run are those that the section of ADR-041 lists: one
  distribution, one machine, one policy, each case run once or twice.
- The renderings are stand-ins. No template, no `plugin start` of remote code and no
  kernel image of the exit was used; the kernel container is a sleeping container
  bound to the home.
- Not run: a reboot of the machine with the unit enabled; a unit of the system's
  service manager over Podman; a nested engine with the flags of the S2a exit; the
  check of decision 50 on the compose target, which is `plugin start`'s and has no
  code yet.
- The public targets of the dials were one public address on port 443 and one public
  name.
- The resolver was not run for: a lookup in flight against an old server that drops
  packets without an answer, where the source gives up to 5 s an attempt and two
  attempts; a second attach to a further network; a channel of `grpc-go` inside the
  window (G-GRPC). That a root filesystem with no `nsswitch.conf` gives the same
  order, files and then DNS, was read in `conf.go` and not run.

#### Questions for the owner after the runs of 2026-11-28

Each question gives its options, the recommended one, and why. Question Q19 above is
still open.

**Q20. How does the bind of an instance's directory get its label under SELinux?**
Decision 30: "On a host with SELinux enforcing the bind needs the label of decision
14." ADR-041 row R3-SELINUX-FORMS and row N-UNIT-SELINUX: the forms that the runs of
decision 39 recorded, and that decisions 31 and 33 will render, cannot carry a label on
Docker and Compose, and on an enforcing host whose runtime confines the instance then
cannot read its key.
- (a) `aiopsctl` labels the directory of the instance itself, with the type
  `container_file_t`, at install and at every `plugin start`, where the host has
  SELinux, and both templates keep the forms of decision 39. The site user can set the
  label with no root. `restorecon -R` keeps it; `restorecon -RF` removes it, and the
  instance then fails to read its key until the next `plugin start`. The label is the
  shared one, so SELinux adds no separation between instances for this directory,
  unless a fixed level per instance is also rendered with `--security-opt
  label=level`, which read correctly in the run. It was run with `chcon` by hand and
  not by a command of `aiopsctl`.
- (b) Both templates write the bind in the form that carries the option, `-v
  <dir>:/aiops/identity:ro,Z` in the unit and the short form in the fragment. The
  label is private to the container and renewed at each start, and the runtime does
  the relabel. `create_host_path: false` is lost: in the run a missing source was
  created inside the home as `root:root` of mode 0755. `HostConfig.Binds` is no longer
  null. A daemon that does not confine accepts the option and ignores it. A private
  label is taken by any other container that binds the directory with `:Z`, and by
  the kernel container's bind of the home with a label option (question Q21).
- (c) S2b supports no unit on a runtime that reports SELinux confinement: `plugin
  start` refuses it and says so, and the label is settled in a later step. This defers
  the sentence of decision 30 and needs the owner's word (ADR-063 decision 33).
- Each of the three edits decision 30 and the sentence on SELinux of decision 14.
- Recommended: (a). Both templates stay as they were run for decision 39, with
  `create_host_path: false` in the fragment, and the label does not depend on which
  container started last. Its label is the one
  that the `z` option of decision 14 gives the whole home in any case, and what keeps
  another instance out is then, as under the modes of decision 43, the mount namespace
  and the closed directories. The cost is one write of a label by `aiopsctl`, and a
  host on which an operator resets labels by force until the next `plugin start`.
  Needed before the templates, install of remote code and `plugin start` merge.

**Q21. Which label does the kernel container's bind of the home carry?** Decision 14:
"each bind of decision 15 also needs a label the container may read (the `z` option of
the bind in Docker and Podman), which the exit does not test". ADR-041 row
R3-SELINUX-RENDER: with `:z` every file of the home takes the shared label, the
credential directories among them, and with `:Z` every running instance loses its key
until it is started again.
- (a) The shared label is accepted everywhere. Decisions 14 and 43 gain a sentence:
  on a host with SELinux, the files of the home carry a label that every container of
  the host may read, SELinux adds no separation between instances or between an
  instance and the kernel's key, and the guard is the closed directories and the mount
  namespace, as on a host without SELinux. The option `Z` is never written on a bind
  of the home.
- (b) The directory of an instance has a private label, and the kernel container's
  bind of the home is written with no label option, on a home that `aiopsctl` labelled
  once. Decision 15 gains that rule, and `aiopsctl` gains the labelling of the home.
- (c) The kernel container runs with `label=disable`, which removes its confinement by
  SELinux.
- Not run for any option: what the kernel image of the exit needs.
- Recommended: (a). It is decision 14 as written, with its effect said. The texts
  claim no separation by SELinux today: the guard of decision 43 is the closed
  directories. (b) adds a second mechanism that one bind with a label option undoes,
  and (c) gives up the confinement of the kernel container. With question Q20 (a) the
  two answers are one rule: one shared label, written by the bind of the kernel
  container or by `aiopsctl`. Needed before the container form of decision 15 is
  documented for an enforcing host; it blocks no code of the exit, which does not
  test SELinux.

**Q22. What do the words "no group of the host" mean under a rootless runtime that
keeps the user?** This is question Q23 of ADR-041, and one answer settles both.
Decision 43: the user of a unit is `65532:65532`, "and its second number is not the
group of the site user"; decision 49: "the instance holds no group of the host". Under
rootless Podman 6.1.3 with `--userns=keep-id` the runtime adds the site user's group to
the process of the instance as a supplementary group, whatever `--user` is given. The
read of the credentials is not affected, and in the runs the group reached nothing:
the directory of the instance is the one bind, and no other path of the home has a
mode with group bits. The cause was not read in the source of Podman.
- (a) The sentence is reworded to "the unit names no group of the host", in decision
  49 and in ADR-041 decision 57 and the notes at its decisions 24 and 44, with a note
  that a runtime that keeps the user adds the site user's group and that the group
  reaches no file through the one bind. Decision 43 keeps its sentence, which the
  runs do not contradict, and its note of 2026-11-28 names the answer.
- (b) `plugin start` reads the groups of the first process of the instance with its
  uid, as the host sees them (decision 30), and refuses an instance that holds the
  site user's group. That refuses `--userns=keep-id` for units.
- (c) A unit is refused on every rootless runtime at S2b. That is option (b) of
  question Q19 of ADR-041, which the owner did not choose.
- Recommended: (a). What decision 43 protects is that the instance reads nothing of
  the home but its one directory, and the runs show that under this mapping too. (b)
  refuses a mapping that decision 14 names as one that qualifies, for a group that
  opens no file. Needed before the templates and `plugin start` merge.

**Q23. Does a unit run on Podman at S2b?** Decision 14 has rootless Podman with the
user kept qualify for the kernel container, decision 33 has the kernel and the
instances be containers of one runtime, and decision 32 has the host hold no address
in the network of an instance. N-PODMAN, in one reading: an internal network of Podman
6.1.3 has a gateway address that the host holds, a rootful instance connected to a
listener of the host on it, and the gateway-mode options of the template are Docker's
and change nothing there. Both templates name the program `docker` (decision 31), and
every row of decision 39 was run on Docker. `plugin start` refuses the network that
Podman makes (decision 36), so no instance runs unfenced under any option.
- (a) At S2b a unit runs on Docker alone. `plugin start` says so on Podman, decision
  14 gains that limit for a site with remote code, and the network of a unit on Podman
  is an addition to this part with runs of its own. This narrows decision 14 for
  remote code and needs the owner's word (ADR-063 decision 33).
- (b) A rendering of decision 32 for Podman is found and run before the part is
  accepted, with the unit of the system's service manager over Podman, which was not
  run either.
- (c) Nothing changes, and decision 36 refuses at run time. An operator on Podman
  learns the limit from the refusal of one network.
- Under every option the reading is run again by itself before it is recorded as a
  result.
- Recommended: (a). The fence holds either way, so the choice is where the limit is
  written. No run shows a unit on Podman: the network fails in this reading, the
  system unit was not run, and the templates name another program. A limit that every
  site on Podman meets belongs in the text that the operator reads and in the words of
  `plugin start`, not in the refusal of one network. (b) is the way if units on Podman
  are wanted at S2b; it holds the acceptance of this part until a rendering is found.
  Needed before this part is accepted.

#### Edits made with this record, and left

Made, in this file: the status of this part, at the head of the file and at the head of
the part; this section; the value of the bound, by a dated note at decision 51; and
notes of 2026-11-28 that point here, at decisions 43, 49 and 50 and after the list
"Left to later commits" of the section "Owner answers of 2026-11-28". No decision is
revised but in the value that decision 51 left to its reading: an option that the owner
has not chosen edits nothing. The row of ADR-047 in the ADR table of `08` §3 names this
record and the questions that the part waits on.

Left, each to the commit that records the owner's answers to Q19 to Q23: decision 30 and
the sentence on SELinux of decision 14, as the answers to Q20 and Q21 have them; the
words on the group in decision 49 and the note at decision 43, as the answer to Q22 has
them; decision 14 and the words of `plugin start`, with Q23 (a); and the lookup of
decision 53, as the answer to Q19 has it. Left to the commit that marks this part
accepted, after those answers: every edit of the section "Edits on acceptance of the S2b
part", as the records of 2026-11-27 and of 2026-11-28 list them. Still left as the
section "Owner answers of 2026-11-28" lists it: the definition `sidecars` beyond
`Endpoint` and `Report`, an addition due before `sidecars-static` and the proxy merge.
