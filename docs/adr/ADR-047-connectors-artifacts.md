# ADR-047: Vendor-neutral connectors, S2a part (the artifact, the install and the home)

- Status: **accepted 2026-11-23 by the project owner, for the S2a part.** The owner
  answered all seven questions on the same date with the recommended option of each; the
  section "Owner answers of 2026-11-23" records them as decisions 16 to 22. The rest of
  ADR-047 stays for S8a start, as the scope below says.
- S2b part: **proposed 2026-11-27, awaiting the project owner.** The section "S2b part"
  at the end of this file holds it, with decisions 23 to 35 and questions Q8 to Q10
  (ADR-063 decisions 2 and 34). Until the owner accepts it, nothing above changes.
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
   specification.

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
    which says so (upgrade note of the S2a report).

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

- Status: **proposed 2026-11-27, awaiting the project owner.** This part is an addition
  to ADR-047, written as ADR-063 decision 2 has the S2b parts of accepted ADRs written,
  and it is accepted before the code it governs merges into `main`. Three questions are
  open, Q8 to Q10. Decisions 1 to 22 stand as accepted; where a decision of this part
  adds to one of them, it names it.
- Scope: what ADR-063 decision 34 lists for this part, with what its decisions 25 and 27
  leave to it: the layer and media type of the image reference; the image and its digest
  (ADR-063 decision 61); the SBOM of remote code; the one row of a remote bundle (ADR-063
  decision 67); the unit files, the endpoint table and the credentials as rows of the
  home table of decision 13, with the one directory a unit binds; the network of an
  instance and of a kernel container, and how `aiopsctl` names the kernel container.
  - It blocks `plugin pack`, `push` and `install` of remote code, the rendered compose
    fragment and systemd unit, the endpoint table that `sidecars-static` reads, `plugin
    start`, and steps C2 and C3 of the S2b exit (ADR-063 decision 27).
  - Settled by the other S2b parts and by ADR-041 (ADR-063 decision 34), and only named
    here: the form of each identity, the files of a credential directory, their lifetime
    and rotation, the mechanism of the revocation, and how an entrypoint is told its
    listener and its credential directory (ADR-041); whether `plugin.yaml` carries
    `entrypoint`, the grammar of that field and of the id as a unit takes them, and what
    a manifest that lists both `inproc` and `remote` packs and installs (ADR-048); the
    record of a remote artifact, `plugin verify` against a pulled image and
    `allowUnsigned` for the runtime `remote` (ADR-020); `plugin start` as a command and
    the text of each refusal (ADR-061). The content of a unit beyond its paths, its one
    bind, its image reference and its network is the fixed template of ADR-063 decision
    25.
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
  as `module@version`, read in that module at that version.

### Context of the S2b part

ADR-063 fixes what S2b builds and leaves to this part the points that touch the artifact
and the home (ADR-063 decisions 25, 27 and 34). Reading its decisions against decisions 1
to 22 and the code at `bbfd839` leaves eight gaps:

- **No image layer.** The layer table of decision 1 has none: decision 1 says only that
  the image reference arrives at S2b "as one more layer type under the same
  `artifactType`". `plugin check`, `pack` and `install` refuse the runtime `remote`
  (`plugins/operations/internal/operations/author.go:248`, `artifact.go:90`,
  `install.go:122`).
- **An image that no line describes.** `plugin pack` builds the plugin image, "writes
  the image beside the artifact as an OCI layout and records the digest of that image",
  and `plugin push` "pushes the image and then the artifact" (ADR-063 decision 61). No
  line says which file is built, over which bytes the digest is taken, where the layout
  puts the image, or in which repository a unit finds it. An artifact is fixed at `pack`
  and signed before `push` names a registry, and a local path is a spec too (`07:1864`;
  decision 1).
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
  25).
- **No network in the container form.** Decision 15 names no network, and the S2a exit
  starts the kernel with none (`test/authoring/exit_test.go:1063`). At S2b `plugin start`
  creates the network of an instance and attaches the instance and the kernel container
  to it; how `aiopsctl` names that container, and what the same step does for a kernel
  that runs as a binary on the host, are left to this part (ADR-063 decision 27).

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
      then holds the image digest itself, and `plugin inspect` prints it with no other
      fetch (decision 25).
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
      (`internal/builder/image.go:417`); any runtime that takes the same commands
      qualifies, as decision 14 says of a user mapping. Without one, `plugin pack` of
      remote code refuses and names what it lacks, and step 7 of `plugin check` is
      reported as not run (`07:1570`; ADR-063 decision 17).
    - **What pack does.** It runs the runtime's build on that file, with the plugin
      directory as the build context and nothing else passed in. It takes the built
      image out of the runtime as an OCI image layout
      (`github.com/opencontainers/image-spec@v1.1.1`, `image-layout.md`) and copies the
      image manifest, its config and its layers into `blobs/sha256/` of the layout of
      decision 1. That is "the image beside the artifact as an OCI layout": one directory
      holds the artifact and its image, so `--out`, `plugin sign`, `plugin push` and a
      local-path spec name one directory, as at S2a. The rule of decision 1 stands:
      `index.json` names exactly one manifest of the `artifactType`
      `application/vnd.aiops.plugin.v1`. A reader finds the image by the digest in
      `image.json` and never through `index.json`: a layout may hold a blob that
      `index.json` does not name (`image-layout.md`, "Blobs").
    - **The digest** is the SHA-256 of the bytes of that image manifest as the layout
      holds them. It is never an identifier that the runtime's own store gives an
      image. `plugin push` copies those bytes unchanged (decision 25), so the digest in
      the artifact is the digest under which the registry serves the image (ADR-063
      decision 27, step G2) and by which the runtime of the clean machine pulls and runs
      it (step C3).
    - **One image manifest, one platform.** Pack refuses a build whose export is not
      exactly one `application/vnd.oci.image.manifest.v1+json`: an image index, or a
      manifest of another type. The platform is that of the runtime that built it, and
      the S2b exit runs linux/amd64 alone (ADR-063 decision 27). An image for several
      platforms arrives with its first consumer (`08:680`), in a form that decision 22
      lets a release add.
    - **Not read.** Which commands of the runtime build and export, and with which
      flags, was read in no source for this ADR. The code states them, the runtime of
      the exit is read and pinned before its first use (ADR-063 decision 35), and steps
      G2 and C3 prove them on it. If that runtime exports no OCI image manifest that a
      registry then serves byte for byte, the work stops on that point and the owner is
      asked (ADR-063 decision 33).
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
      image, and holds both in one repository.
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
    - `plugin verify` fetches what install fetches. The image itself is pulled by the
      container runtime, by the reference of decision 31; the rest of its form for
      remote code is the S2b parts of ADR-020 and ADR-061.

26. **The SBOM of remote code** has the form of decision 1: an SPDX 2.3 document in JSON
    that `aiopsctl` writes itself with `encoding/json`, so it adds no linked module and
    ADR-060 decision 34 asks nothing more.
    - It lists the plugin image as one package: the plugin id and version, the digest of
      the image manifest as its checksum, and the manifest's `license` as its declared
      licence. Under it, it lists the image's config and each layer as files, each with
      the SHA-256 and the size that the image manifest gives it.
    - It is not an inventory of what the image contains. It states no licence of the
      contents (`NOASSERTION`) and lists no package of an operating system or of an
      interpreter: `aiopsctl` unpacks no image layer and links no scanner of image
      contents, as the builder writes its own SBOM and holds no SBOM tool (ADR-044
      decision 15). Nothing asks such an inventory of remote code: the licence scan is
      over code linked into a binary (check 6, `08:504`), and a remote plugin is a
      separate program whose licence is unrestricted (L16, `08:1351`).
    - The rule is by layer: the files of a content layer and of a fixtures layer, the
      build list of a module layer (decision 1), and the image of an image layer.

27. **The bundle of a remote artifact holds exactly one row, the proxy row** (ADR-063
    decisions 41 and 67).
    - Its `insert` items hold, together, exactly one row, and that row's `plugin` is the
      artifact's own id. The other plugin that decision 11 admits, a first-party plugin
      marked `instantiable`, is not open to it: a remote artifact with no row of its own
      plugin would start an instance that no row dials.
    - The row sets the fields of decision 19 and no other. It states its roles (`08:157`),
      and only replicas of those roles dial the instance (`03:571`). It mounts under the
      group of decision 5, so its id is `<name>:<row>`, the id that `plugin enable`
      takes.
    - `plugin check` refuses a remote bundle with no row, with a second row or with a
      row of another plugin, and names the rows. The gate denies it under check 8 at
      every entry point that evaluates check 8 (ADR-060 decision 17), in the clause that
      the gate text carries from S2b (`08:506`).
    - Several rows of one remote plugin are the work of S4b, where the question can be
      reopened with the replicas (ADR-063 decision 67).

#### The home

28. **The rows of the home that a remote plugin adds.** This adds to the table of
    decision 13.

    | Path under `$AIOPS_HOME` | Written by | The kernel mounts it | Source |
    |---|---|---|---|
    | `sidecars.json`, the endpoint table (decision 29) | `aiopsctl` `install`, `remove` | Read-only; the row `sidecars-static` reads it | `08:1293`; `02:215`; ADR-063 decisions 17 and 39 |
    | `remote/units/<plugin id>/`: `compose.yaml`, the compose fragment, and `<unit name>.service`, the systemd unit (decision 31) | `aiopsctl` `install`, `remove` | Read-only, and the kernel does not read it: the host starts the instance from it | ADR-063 decisions 25 and 50 |
    | `remote/kernel/`: the key pair and the certificate of the kernel | `aiopsctl` `install`, on first need | Read-only; the proxy reads it | ADR-063 decisions 23 and 55; `00:413` |
    | `remote/pins/`: the pin of the certificate of each installed instance, one entry per plugin id | `aiopsctl`: `install` writes it, `disable` and `remove` delete it | Read-only; the proxy reads it at every dial | ADR-063 decision 55; `00:412` |
    | `remote/instances/<plugin id>/`: the key pair and the certificate of that instance, and the certificate of the kernel | `aiopsctl` `install`; `remove` deletes it | Read-only, and the kernel does not read it: it is the one directory that the unit of that instance binds (decision 30) | ADR-063 decisions 25 and 55 |

    - A plugin id holds one `/`, so `<plugin id>` is two path segments, as under
      `bundles/` and `content/` (decision 5).
    - Every row is of the site and not of a profile: a site holds one digest and one
      shared instance per plugin id (decision 7; `08:388`).
    - `aiopsctl` writes each of them through the operations library, under `write.lock`
      and with the snapshot of decision 13 (ADR-061 decisions 17 and 19). The kernel
      writes none of them: it still writes nothing outside `run/`. `plugin start` writes
      no file of the home.
    - The kernel's private key is in `remote/kernel/` and nowhere else, and
      `remote/instances/<plugin id>/` holds no private key but that instance's, so the
      kernel's key is in no file that a unit gives an instance (ADR-063 decision 23).
    - The names of the files in the three credential directories, their form, their
      lifetime and rotation, and which command writes a pin again after a `disable`, are
      ADR-041's (ADR-063 decisions 23 and 34). This part fixes the directories, who
      writes them and who may read them.

29. **The endpoint table** is `$AIOPS_HOME/sidecars.json`: one JSON object with
    `tableVersion`, which is 1, and `endpoints`, a list sorted by `name` of objects with
    the two members `name` and `address`.
    - It is JSON and not YAML because its reader in the kernel is a row, a plugin under
      `plugins/` that injects nothing (`03:113`): it reads the file with the standard
      library and adds no importer to the register entry of the YAML library
      (`tools/upstream-components.yaml:124-132`).
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

30. **The one directory a unit binds.** The unit of an instance binds
    `remote/instances/<plugin id>/` of the home, read-only, at `/aiops/identity` in the
    container: the same path in every unit, under no path of the home, so that no path
    inside an instance is a path of the home.
    - It binds nothing else: no other path of the home, not `run/`, no socket of the
      container runtime and no other path of the host (ADR-063 decision 25).
    - The directory is bound, never its files one by one, for the reason of decision 15.
      On a host with SELinux enforcing the bind needs the label of decision 14.
    - The unit names a user that is neither the site user nor root (ADR-063 decision
      25), and decision 14 gives every file of the home the mode 0600 and the site user
      as its owner. How that user reads this one directory is question Q9. Whatever the
      answer, the home keeps its mode 0700 and its one owner, and decision 14 holds for
      every other path.

31. **The unit files, and the image a unit names.**
    - **The unit name** is `remote-plugin-<owner>--<name>`: the role name of the
      container (`00:138`, `:664`) and the two segments of the plugin id, joined by two
      hyphens, which no kebab-case segment holds (`boot/manifest.go:582-590`). Distinct
      ids therefore give distinct names. It names the container of the instance, its
      network and its host on that network (decision 32), and the systemd unit file.
      It has to resolve as a name, so `plugin check` and install refuse an id whose
      unit name is longer than 63 bytes, the length of a DNS label, with no unit
      rendered (ADR-063 decision 28); the S2b part of ADR-048 takes that bound into the
      grammar of the id as a unit takes it (ADR-063 decision 34).
    - **Both targets are rendered at install** (ADR-063 decision 50), into
      `remote/units/<plugin id>/`: `compose.yaml` and `<unit name>.service`. They name
      the same container and the same network, so one of them runs at a time, which is
      the one shared instance of `08:388`. `plugin start` runs the fragment; the
      operator enables the systemd unit by its path (`08:1293`). The form of both
      commands is the S2b part of ADR-061.
    - **The image reference** of a unit is `<registry>/<repository>@sha256:<digest>`:
      the repository from which install read the artifact, and the digest of
      `image.json`. It holds no tag (ADR-063 decision 25). That repository is where
      `plugin push` put the image (decision 25). The runtime pulls it with the registry
      credentials of the site, which the orchestrator holds and the kernel never does
      (`00:416`). A site that copies artifacts into a registry of its own copies the
      artifact and its image into one repository and installs from there. Of the three
      inputs that ADR-063 decision 25 lets an artifact give a unit, the image reference
      by digest is thus the digest that the signed artifact pins; the repository is the
      operator's, through the spec.
    - **A local-path spec** names no repository. What install of remote code does with
      one is question Q8.

#### The network

32. **The network of an instance** (ADR-063 decision 24).
    - Each instance has one network of the container runtime, named by the unit name
      and created by the unit: the compose fragment declares it, and the systemd unit
      creates it before it starts the container. It is an internal network (ADR-063,
      Context: "a compose file has an internal network"): nothing routes from it to
      another network, to an interface of the host or outside.
    - Its members are the instance and the kernel container of decision 33, and nothing
      else. The instance joins no other network and publishes no port. Its name on the
      network is the unit name, and it listens on port 7443 of that network alone, a
      port that the unit's user binds with no capability.
    - What the network has to give, each point asserted on the runtime of the exit
      (ADR-063 decision 27, step C3; decision 28): from inside the instance no
      connection reaches an address outside the network, the registry or another
      instance; the runtime's resolver answers the instance for no name outside the
      network; and of the kernel the instance reaches only the stream that the kernel
      opened. The last holds at S2b because the kernel listens on no network: the row
      `reload-socket` listens on a Unix socket in `run/` (ADR-062 decision 3), and no
      row that S2b ships opens a network listener. The step that builds the first row
      with a network listener keeps that listener off the networks of instances, or
      shows that an instance cannot reach it, in the ADR of that step.
    - How a pinned runtime treats each of these points on an internal network was read
      in no source for this ADR. A point that the runtime of the exit does not give
      stops the work and goes to the owner (ADR-063 decision 33); it is never recorded
      as built.
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
    - **It starts with no network**, as at S2a (`test/authoring/exit_test.go:1063`;
      ADR-063 decision 27), and decision 15 gains that sentence. `plugin start <id>`
      attaches it to the network of that instance, and from then its only networks are
      the networks of instances. The kernel and the instances are containers of one
      runtime (ADR-063 decision 27).
    - **`plugin start` is idempotent.** It creates the network where it is absent,
      starts the instance unless it runs, and attaches the kernel container unless it is
      attached. A kernel container that the operator starts or replaces after the
      instance, as after `dist use`, is attached by the same command, which is the
      exact command that `plugin enable` prints (ADR-063 decision 17).
    - **Where it finds no kernel container,** `plugin start` starts the instance,
      attaches nothing and says that no kernel container of this home runs on this
      runtime and that the same command attaches one that starts later.
    - **Names are per runtime, not per home.** A container of the unit's name that does
      not bind `remote/instances/<plugin id>/` of this home is not this site's
      instance: `plugin start` refuses and names it.
    - **Not read.** Whether the runtime of the exit attaches a container that started
      with no network was read in no source for this ADR. Step C3 proves it; if the
      runtime refuses, the work stops on that point and the owner is asked (ADR-063
      decision 33).
    - Until `deploy/compose` exists (S7; ADR-063, Context) the operator starts the
      kernel container by hand in the form of decision 15. The step that builds
      `deploy/compose` says how a kernel that is a compose service joins the networks of
      instances.

34. **A kernel that runs as a binary on the host** is question Q10. Whatever the answer,
    no unit gets a wider network than decision 32 gives it, and the server dials only
    the address that the endpoint table names.

#### Install and remove of remote code

35. **The order of the writes.** This adds to decision 10.
    - Install writes `remote/units/<plugin id>/`, `remote/instances/<plugin id>/`, the
      pin, `remote/kernel/` on first need and the line of the endpoint table before it
      changes the lock and the bundle list, in the one write of the home that holds the
      kept files of decision 5. A failure that restores the snapshot therefore leaves no
      lock entry without its files, and a denial writes nothing (ADR-063 decision 17).
    - Install into a second profile of an id that the site holds at the same digest
      reuses all of them and writes none again (decision 7).
    - Where `disable` or `remove` revokes the credential (ADR-063 decision 68), it
      deletes the pin first, in the changes that precede the submission that unloads
      the proxy (ADR-063 decision 23; `00:412`). When no profile names the id, `remove`
      deletes the unit files, the directory of the instance with its key, and the line
      of the endpoint table, with the kept files of decision 10 and under its note of
      2026-11-23: only after a final result that shows the change, or when no process
      runs the profile (ADR-063 decision 68).
    - `remote/kernel/` stays: it is the site's and not a plugin's, and its lifetime is
      ADR-041's.
    - A file that a write deletes lies in `snapshot/` until that write drops its
      snapshot (ADR-061 decision 17). A key is deleted once the command has returned
      without a restore.

### Consequences of the S2b part

- The operations plugin gains the writer and the reader of `image.json`, the build and
  the export through the container runtime, the push of an image before its artifact,
  the paths of decision 28 with the writer of `sidecars.json`, and in `plugin start` the
  network of decision 32 and the attachment of decision 33. `plugins/sidecars-static`
  reads `sidecars.json`. `boot` gains from this part only the wording of the one-row
  clause of check 8 (decision 27).
- `plugin pack` of remote code needs a container runtime, and `plugin install` needs
  none.
- An artifact of remote code and its image live in one repository, and a unit names the
  repository that the operator's spec named.
- The SBOM of remote code says which image the artifact ships and not what is inside
  it. A site that needs an inventory of an image runs a scanner of its own on it.
- Boot checks none of the files of decision 28 against a digest: decision 18 covers the
  kept files of decision 5. The units, the endpoint table and the credentials are as
  strong as `trust.yaml`, which the ownership of the home protects (ADR-020 decision
  11). The image that a unit names is fixed at install, and nothing reads the unit
  against the artifact again.
- One container runtime serves the instances of one home per plugin id, since the unit
  names carry no home (decision 33).
- With question Q8 (a), remote code installs from a registry only. With Q9 (a), one
  directory per instance holds files that others may read, inside a home that others
  cannot enter. With Q10 (a), a kernel that runs as a binary on the host reaches no
  instance that a unit starts until the step that adds the host form.
- The media type and the annotation key of decision 23 enter the artifact layout that
  decision 22 freezes at its first publication. The unit name, the port and the path
  `/aiops/identity` are constants of the unit template and are not part of that layout.

### Edits on acceptance of the S2b part

Made in the commit that records the owner's answers:

- This file: the status of this part; dated notes that name the decisions of this part
  at decision 1 (the layer of decision 23), decision 2 (decision 25), decision 10
  (decision 35), decision 13 (the rows of decision 28), decision 14 (as question Q9 is
  answered) and decision 15 (no network; decision 33).
- `07:1850` (`Artifact`): the image digest is in the layer `image.json`, and the image
  is in the artifact's repository. `07:1865`: for remote code `plugin inspect` prints
  the image reference from the artifact's manifest.
- `07:1854` (`Unit`), `08:1293` and `02:313`: the fragment and the unit are under
  `remote/units/<plugin id>/` of the home, and the table is `sidecars.json`. `08:432`
  (profile files) and `02:316` (composition source): the units, the endpoint table and
  the credential directories are files of the home.
- The row of ADR-047 in the ADR table of `08` §3 (`08:1181`): the S2b part and its date.
- The lines that the chosen option of each question names.

Left to the commit that builds the clause in the one gate text (ADR-060 decision 16):
`08:506` says that the one row of a remote bundle is a row of the artifact's own plugin
(decision 27).

### Questions for the owner on the S2b part

Each question gives its options, the recommended one, why, and when the answer is
needed. An option that changes or defers a written line says so and names the lines.

**Q8. What does `plugin install` of remote code do with a local-path spec?** A spec is an
OCI reference, an `<id>@<version>` through an index, or a local path (`07:1864`), and
decision 1 has `inspect`, `gate --against`, `install` and `verify` read a local layout
"as they read a registry". A unit names its image in a repository by digest (decision
31), install pulls no image and runs none (ADR-063 decision 17), and a layout on disk is
no repository that a container runtime pulls from. No step of the S2b exit and no case
of ADR-063 decision 28 installs remote code from a local path.
- (a) Install of remote code from a local-path spec is refused: it names the reason and
  `plugin push`, and writes nothing. `inspect`, `gate --against` and `verify` read a
  local path of remote code as they read any other. This narrows a written line for one
  kind of artifact: decision 1 and `07:1864` gain "install of remote code takes a
  registry spec".
- (b) Install loads the image from the layout into the store of the container runtime
  at hand, and the unit names it there by digest. Install then needs a container
  runtime and moves an image, where ADR-063 decision 17 has it pull none, and whether a
  runtime's store names a loaded image by the digest of its manifest was read in no
  source.
- (c) Install takes the repository from the operator, who pushed the image there with
  another tool. This adds a flag to the S2b part of ADR-061 and a second place where an
  image may be, beside the artifact's repository.
- Recommended: (a). It rests on no behaviour of a runtime that nobody has read, it keeps
  install free of a container runtime, and nothing at S2b consumes the other forms
  (`08:680`). Needed before `plugin install` of remote code merges.

**Q9. How does the user of a unit read the one directory it binds?** Decision 14 gives
every file of the home the mode 0600 and the site user as its owner, and `aiopsctl`
runs as that user and cannot give a file to another. ADR-063 decision 25 has a unit
name a user that is neither the site user nor root and bind
`remote/instances/<plugin id>/` read-only. As written, the two leave that user unable
to read its own key, so no option keeps both to the letter.
- (a) That one directory has the mode 0755 and its files the mode 0644, as a stated
  exception of decision 14. On the host the mode 0700 of the home still keeps every
  other user from the path. Inside the container the bind is readable to whatever user
  the unit names, under every user mapping that decision 14 admits. The cost: a private
  key whose file others may read, protected by the directory above it; a copy of the
  home that loses its modes exposes it.
- (b) The directory has the mode 0750 and its files 0640, and the unit names the group
  of the site user beside its own user. It holds only under a mapping that carries that
  group into the container, so install has to know the mapping, and an instance then
  holds the site's group for any file that a later step makes group-readable.
- (c) Decision 14 to the letter, and the unit names the site user. This changes ADR-063
  decision 25 and the assertion of step C2. The instance then runs as the one user that
  `run/reload.sock` serves (ADR-062 decision 5), and only the missing bind keeps it from
  the socket and the home.
- Recommended: (a). It is the one option that works under every mapping with no input
  from the runtime, it keeps the user of ADR-063 decision 25, and what guards the key
  on the host, the mode of the home, is what guards every other file of the home.
  Needed before the unit and `plugin install` of remote code merge.

**Q10. What does `plugin start` do for a kernel that runs as a binary on the host?**
ADR-063 decision 27 leaves the point here. The network of an instance has no route out
and publishes no port (ADR-063 decision 24; decision 32), and the endpoint that install
writes is a name that the runtime resolves on that network (ADR-063 decision 27), so a
process on the host is not served by it. The clean machine of the exit runs the kernel
as a container (ADR-063 decision 27); the T3 of a remote example and the stubs of the
failure matrix start their instances as processes, with no unit (ADR-063 decisions 31
and 64). The deployment view D-2 runs the `aiops` binary on the VM beside a remote
plugin from a unit (`02:295`, `:313`, `:336`), and `02:191` places the remote-plugin
container at S2b.
- (a) At S2b a unit's instance is reached by a kernel container only. For a kernel on
  the host `plugin start` does what decision 33 says where it finds no kernel container,
  and says that a kernel on the host does not reach the network of a unit. The host
  form is an addition to this ADR, due with `deploy/compose` at S7 (ADR-063, Context)
  and before the first community release, with a mechanism shown on a pinned runtime to
  keep ADR-063 decision 24: the host dials the instance, and the instance reaches no
  listener of the host. This defers part of a written view: `02:313` and `02:336` gain
  "for a kernel that runs as a container; for a kernel on the host from S7".
- (b) The host form is built at S2b. `plugin start` reads the address that the runtime
  gave the instance on its network and writes it into the line of the endpoint table,
  which then has a second writer, holds an address that changes at every start and
  makes `plugin start` a write of the home. The systemd job of ADR-063 decision 50
  proves that the host reaches that address and that the instance reaches no listener
  of the host. Neither was read for any runtime; if the pinned runtime gives only one,
  the work stops (ADR-063 decision 33).
- (c) The unit of such a site publishes the port of the instance on the loopback
  interface of the host. The instance then has a way to the host that ADR-063 decision
  24 does not give it, and whether a runtime publishes a port of an internal network
  was read in no source, so this weakens that decision or cannot be built.
- Recommended: (a). The exit and every case of S2b run a kernel container or no unit,
  so nothing at S2b consumes the host form (`08:680`), and (b) builds it on two
  behaviours of a runtime that nobody has read. The owner decides, because `02:313`
  draws the host form. Needed before `plugin start` merges.
