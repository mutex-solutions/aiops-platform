# ADR-047: Vendor-neutral connectors, S2a part (the artifact, the install and the home)

- Status: **accepted 2026-11-23 by the project owner, for the S2a part.** The owner
  answered all seven questions on the same date with the recommended option of each; the
  section "Owner answers of 2026-11-23" records them as decisions 16 to 22. The rest of
  ADR-047 stays for S8a start, as the scope below says.
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
    beyond that is ADR-020's (`07:75` R17).

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
