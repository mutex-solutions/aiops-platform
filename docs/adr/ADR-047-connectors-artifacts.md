# ADR-047: Vendor-neutral connectors, S2a part (the artifact, the install and the home)

- Status: **proposed 2026-11-23, awaiting the project owner.** Nothing in this ADR is in
  force until the owner accepts it and answers its questions. It is proposed before the
  first commit that implements it and is accepted before that code merges into `main`
  (ADR-060 decision 2).
- Scope: the S2a part of ADR-047 only, "the pack artifact form and the insert-only rule of
  gate check 8" (`00:781`; `08:1178`; community:438), with every point ADR-060 decision 33
  lists for it. The rest of ADR-047 stays for S8a start (`08:1178`; `07:72` R14, `07:73`
  R15): the read contract, `tools` as the read seam, `tool-mcp` and the digest of its
  recorded tool list with the config keys of its rows, the `signals` fields and busy reply,
  `evidenceSources`, the `dialer-guarded` ranges on self-host, the third-party naming rules,
  the context pack footer, the refusals of a default label map, playbooks without query
  text, and whether a `signals` contribution may supply its own signature verifier. That
  part is proposed to the owner at S8a start as an addition to this ADR. This ADR changes
  no decision of `00-decisions.md`; where an answer edits a line elsewhere, the question or
  the section "Edits on acceptance" names it.
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
  and the profile patch" (`08:374`, `:395`), the set dsh keeps in one profile directory and
  restores from one snapshot (dsh-architecture §4.2). ADR-060 decision 26 leaves the choice
  here.
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
   | The SBOM | `sbom.json` | The registered type of the SBOM format that ADR-044 picks for images | Every artifact |
   | The attestation | as ADR-020 fixes | as ADR-020 fixes | Every artifact, as a layer or an OCI referrer (ADR-060 decision 18) |

   - The title annotation is `org.opencontainers.image.title`, so a generic OCI client
     lists the layers by file name. Each type appears at most once.
   - A reader, `install` and the gate included, ignores a layer type it does not know and
     fetches no blob of it, as a reader of `plugin.yaml` ignores an unknown field of a
     higher minor; `plugin check` refuses one, as it refuses an unknown field (`07:120`).
     The layout is frozen-additive on the same terms. The `v1` of a type changes only
     with a break of that layer's container format; the schema of `plugin.yaml` keeps its
     own `manifestVersion` (ADR-048).
   - The module layer holds `{"path", "version", "sum"}`, the entry form of `plugins` in
     the site's `dist.yaml` (`08:401`), so install prints the `dist.yaml` lines of `08:397`
     from it.
   - The image reference of a remote plugin arrives at S2b with the remote runtime
     (ADR-060 decision 29), as one more layer type under the same `artifactType`.
   - A pack's SBOM lists the files of its content and fixtures layers with their digests
     and the pack's `license`; that of in-process code lists the build list of its module.
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

2. **What each command fetches.** Every blob is checked against the digest of its
   descriptor before it is used, after the signature over the image manifest is verified
   as ADR-020 fixes.
   - `plugin inspect` fetches the image manifest, `plugin.yaml` and the bundle and nothing
     else (ADR-060 decision 58; `08:373`; `07:1853`). The content paths it prints come from
     the annotation `aiops.plugin.content.paths` on the content layer's descriptor, a JSON
     array of the paths in the layer in the order of decision 3. `plugin pack` writes it,
     and install refuses an artifact whose content layer holds other paths.
   - `plugin install` also fetches the module and content layers and the attestation; it
     never fetches the fixtures or the SBOM.
   - `plugin gate --against` fetches what `inspect` fetches and the attestation (ADR-060
     decision 17).
   - `plugin verify` and an index also fetch the fixtures (`07:1512`; community:192).

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
     into a staging directory beside `$AIOPS_HOME/content/<plugin id>/` and renames it into
     place, so a reader sees the old tree or the whole new one, as `internal/runfile` does
     for one file.
   - The fixtures layer has the same form; nothing unpacks it into the home. The name lint
     of `plugin check` step 1 already covers content paths (`07:1567`).

4. **The OCI client** that `plugin push`, `inspect` and `install` use is
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
     Foundation". Its `go.mod` also requires `github.com/russross/blackfriday@v1.6.0` and
     `github.com/santhosh-tekuri/jsonschema/v5@v5.3.1`, which only its `schema` package
     imports. No package of `oras-go` imports `schema`, so neither module is in the package
     graph that the licence scan reads (licence:204); their licences are not read here.
   - `github.com/opencontainers/go-digest@v1.0.0`: `LICENSE` is the Apache License 2.0
     with "Copyright 2019, 2020 OCI Contributors" and "Copyright 2016 Docker, Inc.". Beside
     it, `LICENSE.docs` is Creative Commons Attribution-ShareAlike 4.0 International, for
     the module's documentation. CC-BY-SA is denied in process until reviewed and recorded
     by name (licence:203), and the scan reads each directory (licence:204). No
     documentation of the module enters a binary, so its register entry records that file
     by name as documentation only; accepting this ADR accepts that record.
   - `golang.org/x/sync@v0.24.0`, the packages `errgroup` and `semaphore`: `LICENSE` is
     BSD-3-Clause with "Copyright 2009 The Go Authors", with the Go `PATENTS` grant. The
     root module already requires it at that version.

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
     clause of check 8 to exactly those bundles (decision 11).
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
   the answer, install decides it before it writes anything (`08:374`), and boot refuses a
   composition in which a group and another row share an id, by the rule that an existing
   id fails in every layer (`08:143`).

7. **One digest per plugin id per site.** The content directory, and from S2b the one
   shared instance of a remote plugin, exist once per site and plugin id (`07:1843`;
   `08:386`; `02:312`). Install of an id already locked at the same digest, into another
   profile, appends that profile's bundle list and reuses the kept files. Install of an id
   locked at another digest refuses, names the locked digest and the profiles that hold
   it, and writes nothing, as dsh refuses to install a bundle that is already installed
   (dsh-architecture §4.4). A new version arrives by `update` at S6b (`08:377`),
   or by `remove` and `install`.

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
    profile's bundle list, drops the profile layer's departures for the rows under its
    group, so that a later install inserts them disabled again (`00:478`), and drops its
    lock entry as question Q1 places the lock. `bundles/<plugin id>/` and
    `content/<plugin id>/` go when no profile names the id. Install writes those
    directories before it changes the lock and the bundle list, and remove deletes them
    after, so a failure that restores the snapshot of `08:374` never leaves a lock entry
    without its files.

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
      `stability: experimental`, `scope: [process]` and `runtimes: []` (`07:129`;
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
    - `fixtures/` with a README that names what step 5 and the scenario replay, and no
      recorded file, so that step 5 fails with one line per obligation (`07:1561`;
      `08:1264`).
    - `README.md`: what the pack connects to, the `plugin enable <name>:<row>` lines, and
      the profile-layer patch with which an operator sets a row's config, since a patch
      replaces the whole config (`08:157`) and the bundle may hold no `${env:}` or
      `${file:}` reference (ADR-060 decision 57). `LICENSE`, Apache-2.0, and a CI workflow
      with one job that runs `aiopsctl plugin check ./`, as the Go template has
      (`07:1559`).
    - No `content/` and no `testenv/`: content is optional (`07:1838`), and the test
      environment against which an author records is the author's (`07:1658`).

    Two rules follow for an instantiable plugin, and `plugin check` enforces them on its
    manifest: its `configSchema` carries at least one root `examples` entry, and no example
    holds an `${env:}` or `${file:}` reference, so a pack written from it passes check 8.
    Where the derivation takes the examples from is ADR-048's. At S2a the template job and
    step P1 write one row of the toy instantiable echoer of the test release (ADR-060
    decisions 24 and 38); from S8a the same command writes rows of `tool-mcp`,
    `signal-ingest-generic-json` and `notifier-webhook` with no change here.

### The home

13. **The paths of `$AIOPS_HOME` and how a `kernel` mounts them.** `aiopsctl` runs on the
    host and writes the home; a `kernel` reads it, as a binary on the host or as a
    container (`02:294`, `:307`, `:314`):

    | Path under `$AIOPS_HOME` | Written by | The kernel mounts it | Source |
    |---|---|---|---|
    | `profiles/<p>/profile.yaml`, the bundle list | `aiopsctl` `profile init`, `install`, `remove` | Read-only | `08:121`; ADR-060 decision 27 |
    | `profiles/<p>/aiops.patch.yml`, layer 2 | `aiopsctl` `enable`, `disable`, `remove` | Read-only | `08:122`, `:375` |
    | `aiops.patch.yml`, layer 3 | The operator | Read-only | `08:123` |
    | `plugins.lock`, where question Q1 puts it | `aiopsctl` `install`, `remove` | Read-only | `07:1841` |
    | `bundles/<plugin id>/` | `aiopsctl` `install`, `remove` | Read-only | Decision 5 |
    | `content/<plugin id>/` | `aiopsctl` `install`, `remove` | Read-only | `07:1843`; `02:313` |
    | `trust.yaml` | `aiopsctl` `trust add` | Read-only | `07:1840`; ADR-060 decision 26 |
    | The records of `dist use` and of install: signer and digest, the image reference | `aiopsctl` `dist use`, `install` | Read-only; their names and form are ADR-020's, under the home and outside `run/` | ADR-060 decisions 22 and 33 |
    | `run/` | The kernel: `status.json`, the files of the test control rows, and the submission socket of ADR-060 decision 48 | Read-write | ADR-059 decision 31; `internal/runfile/runfile.go` |

    At S2a the kernel writes nothing outside `run/`. Later paths join the table with their
    step: `tenants/` at S4b and `profiles/<p>/compatibility.yaml` at S6b (`08:508`). From
    S6a `plugin-manager` writes the profile files from inside the kernel (`08:369`;
    `03:71`), so the step that builds it changes the mount of the paths it writes.

14. **Ownership.** One user of the host, the site user, owns the home and everything in
    it. `aiopsctl` creates the home and `run/` with mode 0700, writes each file with mode
    0600 and replaces it by a rename in its directory, and refuses to write into a home
    whose owner is not its effective user, so a run as another user cannot leave a file
    that the kernel cannot read. The kernel, as a binary or as a container, runs with the
    uid and gid of the home's owner. Only the site user can then open the socket in `run/`
    that ADR-060 decision 48 names as the candidate transport, and no other unprivileged
    user of the host reads or changes a file of the home between install and boot. Any
    protection of the records of install and `dist use` beyond that is ADR-020's (`07:75`
    R17).

15. **The container form.** A `kernel` container gets the home bound at the same path it
    has on the host, read-only; `run/` bound over it at the same path, read-write;
    `AIOPS_HOME` set to that path; and the user of decision 14. `run/` exists before the
    container starts, since `aiopsctl` creates it with the home. The same path on both
    sides lets every dump, status and message name one path, and it is the Helm form of
    `02:313` ("mounted into `kernel` at the same path"). Directories are bound and never
    single files, because `aiopsctl` replaces a file by a rename, which a bind of one file
    does not follow. The directory of `--refs-dir` (`internal/serve/serve.go`) is bound
    read-only at its own path, outside the home. On the exit's clean machine the test
    starts the image that `dist use` recorded in this form (ADR-060 decisions 22 and 30).
    How a chart delivers the home to a cluster, and to several replicas, stays with
    `deploy/helm` and ADR-038 (`02:479`, item 22).

## Consequences

- The S2a code gains, in the operations plugin, the writer and reader of decisions 1 to 3
  on `oras-go`, for a registry and for the local layout of decision 1, and the checks of decisions 3, 6 and 7 at install. `boot` gains the reader
  of `bundles/<plugin id>/`, the group row of decision 5, the clause of decision 11 and
  the lock entries of decision 8; the bundle names of `profile.yaml` admit a plugin id;
  `internal/serve` creates `run/` with mode 0700 and finds it present under a read-only
  home.
- A site cannot hold two digests of one plugin id, and, with question Q2 (a), two
  artifacts of one name in one profile.
- The exit's pack fixture of step P1 carries at least one content file, so that step C1
  prints a content path and step C3 unpacks one (ADR-060 decision 30).
- The layout, the media types and the annotation key are documented in the repository for
  third parties with the attestation type (ADR-060 decision 18). They become
  frozen-additive at their first publication, which waits for L14 (`08:1343`).
- The root module gains `oras-go` and two modules through it; `golang.org/x/sync` is
  already in its graph. The licence report of both binaries lists them (ADR-060 decision
  36).
- At S6a the read-only mount changes for the paths `plugin-manager` writes (decision 13).

## Edits on acceptance

Made in the commit that records the owner's answers, as `08:996` has an ADR's edits land
in the same change:

- `02:483` (item 26): settled by decisions 13 to 15. `02:313`: in D-2 the directory is
  bound into a `kernel` container at the same path, read-only.
- `07:72` (R14): the artifact form and the files of section 8.4 are fixed by this ADR; the
  config keys of `tool-mcp` rows stay for S8a start.
- `07:1650-1660` and `07:1690-1708`: the layout of decisions 1 and 3, and a bundle with no
  `${env:}` reference at `07:1697`, whose endpoint the operator sets in the profile layer
  as decision 12 describes.
- `07:1841`, as question Q1 is answered.
- `08:121` (layer 1) and `08:430` (profile files): the bundle of an installed artifact is
  `$AIOPS_HOME/bundles/<plugin id>/bundle.yaml`.
- The one gate text of ADR-060 decision 16: the clause of decision 11, and the clauses
  that the answers to questions Q3 and Q4 add.
- The lines that an option of question Q2 other than (a) names.

## Questions for the owner

Each question gives its options, the recommended one, why, and when the answer is needed.

**Q1. Is `plugins.lock` one file per site or one per profile?** `07:1841` says "Site
file". `install --profile` and its restore rule treat it as a file of the profile
(`08:374`, `:395`), as dsh does: its lock sits in the profile directory and is restored
with the profile's manifest from one snapshot (dsh-architecture §4.2), the install that
`08:76` (row 34) adapts. Either way one site holds one digest per plugin id (decision 7).
- (a) One per site, `$AIOPS_HOME/plugins.lock`, as `07:1841` has it. A profile's
  generation digest takes the entries whose id its bundle list names, and an entry goes
  when no profile names the id.
- (b) One per profile, `$AIOPS_HOME/profiles/<p>/plugins.lock`, beside the two files it is
  restored with. A profile's generation digest takes its own file whole, and `profile
  rollback --scope process` (S6b, `08:378`) returns one profile's lock and touches no
  other. Install reads the other profiles' locks for the rule of decision 7. `07:1841`
  becomes "Profile file".
- Recommended: (b). It is the dsh shape, the snapshot of `08:374` becomes one profile's
  three files, and a lock has the scope of the generation digest it feeds; the cost is one
  edited line of `07` and a read of the other locks at install and remove. Needed before
  the first commit that writes `plugins.lock` merges.

**Q2. What does install do when an artifact of the same name under another owner enters
a profile?** The group of `acme/connector-x` and that of `beta/connector-x` are both
`connector-x` (`08:391`; `00:657`).
- (a) Install composes the target profile with the artifact's bundle appended, as boot
  would, and refuses when the group id equals the id of any row of that composition, the
  group of the other artifact included. It names both, writes nothing and exits 1 with the
  reason (ADR-060 decision 23). The operator removes one artifact or installs the other
  into another profile. dsh likewise refuses to install a bundle that is already installed
  (dsh-architecture §4.4).
- (b) The group carries the owner, with a separator outside kebab-case so that no two ids
  meet, for example `acme.connector-x:logs`. Edits `00:657` (a row id is kebab-case),
  `08:391`, `07:1737`, `07:1817` and community:298.
- (c) `install --as <group>` names the group when the default is taken, and the profile
  records the alias. Departs from "one group named by the artifact" (`00:478`).
- Recommended: (a). It keeps the written row id and "two packs cannot collide", adds no
  name form and no flag, and also covers a group that meets a first-party row id. Needed
  before `install` merges.

**Q3. Does boot check what install kept against the locked digest?** Install verifies
every blob it pulls against its descriptor, and the signature over the image manifest. The
kept files then sit in the home until boot, and nothing in the documents reads them
against a digest again, while "a changed runbook or label map is a new generation like any
other change" (`08:1288`) and a generation "also pins plugin and pack versions" (`08:184`).
- (a) At every composition, boot hashes `bundles/<plugin id>/artifact.json` and compares
  it with the digest in `plugins.lock`, then hashes `plugin.yaml`, `bundle.yaml` and the
  content tree in the form of decision 3 and compares each with its descriptor in that
  manifest. A mismatch detaches every row of the artifact as disabled and names check 5,
  since the files are not those whose signer install verified; the clause enters check 5
  of the one gate text (ADR-060 decision 16). For in-process code, which has no lock
  entry, the same check starts from the digest that install recorded (ADR-020). ADR-020
  then has only `plugins.lock` and the records of install to protect until boot.
- (b) Install alone verifies. The read-only mount and the ownership of decisions 13 and 14
  keep the files, and `08:1288` is edited to say that a change made to the unpacked files
  is not detected.
- Recommended: (a). It keeps `08:1288` and `08:184` whole: no tree that differs from the
  lock runs under that lock's generation digest. The cost is hashing a few small files per
  artifact at each composition. Needed before the boot reader of `plugins.lock` merges.

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

**Q5. Which prefix do the media types and the annotation key take?** They are written into
every artifact and become frozen-additive at their first publication.
- (a) `application/vnd.aiops.plugin.*` and `aiops.plugin.content.paths`: the id of the
  system and of its binaries (`00:198`), the first-party owner (`00:657`), and the prefix
  of the digest encoding in the code (`aiops/composition/v1`, `boot/digest.go`).
- (b) `application/vnd.mutex-solutions.aiops.plugin.*`, with the annotation key under the
  same name: the organisation of the module prefix that the owner chose (ADR-057), which no
  other producer uses.
- Recommended: (a). It matches every other id of the platform. Neither prefix is
  registered, and nothing is published before L14 (`08:1343`), so the answer can change at
  no cost until then. Needed before `plugin pack` merges.
