# ADR-060: Scope and working rules of S2a (the authoring path)

- Status: **proposed 2026-11-23, awaiting the project owner.** Nothing in this ADR is in
  force until the owner accepts it and answers the questions marked "before any S2a
  code". No S2a code is written before then (s1-report:29).
- Scope: roadmap step S2a (`08-profiles-roadmap-adrs.md` §2.1). It turns the S2a text of
  the roadmap into decidable work and working rules, as ADR-059 did for S1, and changes
  no decision of `00-decisions.md`. Where an answer of the owner changes one, `00` is
  edited in the commit that records the answer (`08:996`).
- Citations: `00`, `01`, `03`, `07` and `08` are the files of `docs/architecture/` with
  those numbers, and `08:694` is line 694 of `08` at commit `491a518`. "community",
  "harness", "licence" and "s1-report" are `docs/design/community-first-2026-11.md`,
  `harness-and-upstreams-2026-11.md`, `licence-audit-2026-11.md` and `s1-report.md`,
  cited the same way. The plugin record, `docs/design/plugin-system-2026-11.md`, is
  cited by section, as the document lint requires.

## Context

S2a builds the authoring path on two toy registries (`08:620`, `:694`): the registries
`echoers` and `stampers` with fakes and suites; `plugin.Define` and the manifest
generator; manifest schema 1.0; definition versions (`definition.yaml`, generated
`requires` and `serves`); the builder (`dist build`, `dist sign`, `dist use`, strict
versions, licence scan, notices) and its container (`08:688`); the gate and `plugin gate
--against`; the commands `plugin new`, `check`, `smoke`, `pack`, `sign`, `push`,
`inspect`, `install` and `verify`; the templates and the CI job that keeps them true;
`trust add`, `plugins.lock` and the typed attestation; the artifact form with no code,
with gate checks 8 and 10 and the instantiable list worded for `tool-mcp` (W9, `00:113`;
harness:335); and the frozen handshake message. ADR-020, ADR-044, ADR-047 (the artifact
form and the gate rule), ADR-048 (the manifest) and ADR-054 (the upstream register) are
due at S2a (`08:694`). These were round-1 P3 items, and the round-1 ecosystem exit, a
third party publishing a plugin with no kernel checkout, is passed by S2a and S2b
together (`08:701`, `:879`).

S1 hands S2a gate checks 1, 2, 4, 5, 6 and 10, the signed-key and fake parts of check 7,
the insert-only part of check 8, `plugin.Define` with the generated half of
`plugin.yaml`, the derived volatile fields and the hook stage numbers of the dumps
(s1-report:211; ADR-059 decisions 4 and 10), and the rule that every suite follows
`conformance.Run(t, Mount)` (ADR-059 decision 32).

Reading the S2a text against the code at `491a518` and against the later steps shows
gaps:

- The exit "calls both" in `08:694`, `08:620` ("run"), community:59 and plugin record
  §16, but not in `00:571` or `01:290`, and `08:3` lets `00` win.
- A pack with no code may insert only rows of first-party plugins marked `instantiable`
  (`08:391`), and none of the three (`07:132`) exists at S2a.
- No release exists, and the SDK modules are untagged `v0.0.0` behind `replace` lines in
  a private repository (ADR-057; `08:1342` L14), so "no kernel checkout" and "a clean
  machine" have no stated mechanism.
- `boot` has 627 lines left under its cap of 6,800 (ADR-059, "Line budgets at the S1
  exit"), and the YAML library is allowed in `boot/` and `tools/` only (ADR-059
  decision 6), while the gate, the manifest, the lock, the trust file and the CLI all
  read or write YAML.
- The gate tables of `08:495-506` and `07:1882-1893` differ in checks 1, 3, 5 and 10.
- "`dist.yaml`" names three files: the site's input (`08:401`), the release's manifest
  (`08:1220`) and the S1 stand-in (`boot/dist.go`).
- The generated two-tenant test (`07:1435`) needs the `tenants` definition of S4a, and a
  pack's attestation needs a scenario version (`08:506`) whose scenario arrives at S8b.
- The S1 definitions and plugins predate the standing rules of S2a (`08:676`): one
  suite, one definition without `definition.yaml`, five core plugins on `kernel.Define`
  with hand-written manifests.

## Decision

### Order and gating

1. **S2a code starts after this ADR is accepted** and the questions marked "before any
   S2a code" are answered. Each other question is answered before the code it names
   merges into `main`.
2. **Each ADR due at S2a is its own ADR**, proposed to the owner before the first commit
   that implements it and accepted before that code merges into `main`. Each keeps every
   written requirement it touches. Decision 30 lists what each must settle.
3. **No kernel change is planned.** The kernel stays at 8,043 of its 8,200 lines
   (s1-report:65). Stage ordinals and the `signed` class come from definition data
   (decisions 7 and 15), not from a new kernel accessor. A kernel change that S2a finds
   necessary, for example to hand a value committed in place to a running
   `plugin.Volatile[T]` field (`07:1375`), is proposed to the owner as an ADR-034
   addition before it merges, stays inside the cap and is verified as ADR-059 decision 2
   requires: the differential corpus, the property workload and the call-path gate.

### Packages and deliverables

4. **The authoring surface is the package `sdk/def/plugin`** of the module `sdk/def`, not
   a top-level `sdk/plugin` (`07:29`, `:59` R1). `08:1201` (assumption 2) leaves the
   directory to S2a. The module is rooted at `sdk/def`, the import analyzer and plugin
   vet accept every package under it (`tools/kernelcheck/imports.go`), and a root at
   `sdk/` would rewrite every definition import. `sdk/gate` and `sdk/entity` follow the
   same rule when their consumers create them. `07` §1.1 and `08` §4 are edited with the
   first commit. The package holds:
   - `Spec[C]` with `ID`, `Inject`, `Provides`, `Hooks`, `Validate` and `Apply`, and
     `Define[C](Spec[C]) kernel.Plugin` (`07:212-226`; `00:207`). `Durable` is added to
     `Spec` at S3 with the `durable` definition (decision 26).
   - `Volatile[T]`. The volatile paths are derived from `C` and carried in the generated
     manifest, so the hand lists in `dist.Module.Volatile` and `boot.ModuleEntry.Volatile`
     go (ADR-059 decision 10). A path is a list of keys, as in `VolatileUpdateArgs.Paths`
     (ADR-059 decision 44).
   - JSON Schema 2020-12 derived from `C` with the standard library, behind a validator
     interface, with a validator of exactly the keywords the derivation emits (`07:29`,
     `:137`; `08:59`). The require list of `sdk/def` stays the one `00:262` allows,
     `sdk/kernel` and, from S3, pgx v5, with `sdk/testkit` for tests only (`07:60` R2),
     so no validator library enters it. The tag `aiops:"tenant"` marks a field
     tenant-writable, and such a field must be `Volatile` (`07:137`, `:473`); the
     overlay that writes it is S4b.
   - The rules that decode the parsed config tree into `C` (unknown fields, integer and
     duration types, defaults), written in the package documentation and proven by table
     tests. They are part of the author API.
   - The Go type of manifest schema 1.0, both halves, with no YAML code (question Q5).
   - The author API version, `pluginApi` (decision 8).

   Plugin vet accepts `var Plugin = plugin.Define(...)` and refuses what it refuses
   today (`07:469`). `plugin.FromManifest` arrives at S2b with its first consumer, the
   `remote` proxy row (`07:1133`; `08:678`).
5. **The toy registries** are built as `08:694` states: two registries with fakes and
   suites, no policy need, no tenant data and class open (plugin record §5; `00:208`).
   Where their definitions live is question Q3. Each has a `definition.yaml`; a plural
   key; registry and contribution interfaces that take the caller's context first,
   return `ErrInactive` after release and expose `Inventory()` (`07:514-519`); a
   recording fake that exposes `Inventory()` (`07:494`); and a suite with an obligations
   half and a guard half, both on `conformance.Run(t, Mount)` (`07:1463`). Each
   obligation has an id, so `plugin check` and a fresh template print one line per
   failed obligation with its id (`07:1561`, `:1563`). Both definitions are `remote:
   "no"` at S2a; S2b marks them `contrib` with the adapter generator, as a minor of each
   (plugin record §5; `08:643`, `:695`). The dispatcher `example-dispatch` provides both
   registries and the toy caller of question Q1 consumes both; these and the other toy
   rows of the exit live where question Q2 puts them.
6. **YAML** stays one library, `go.yaml.in/yaml/v3` at its pinned version (ADR-059
   decision 6; licence:44). Where the CLI reads and writes it is question Q5.
7. **`definition.yaml` gets its fixed layout** (`07:71` R13): `key`, `version`,
   `stability`, `class` (one class, or one per key where a definition declares several
   keys, as `launch` does), `remote`, `compatibleFrom`, `deprecated`, the `suite`
   revision (decision 10) and, for each hook the definition owns, its stages in order,
   its access class (`open`, `signed` or `sealed`) and its remote mark (`07:439`,
   `:443`). `sdk/def/reload/v0` gains its file. A test in each definition package proves
   that the file and the Go declarations agree: the key majors, the bound classes against
   `kernel.KeyClasses`, the seals against `kernel.HookSealed`, and the stage order by
   dispatching through the kernel.
8. **`pluginApi`** is the version of the author-facing surface of `sdk/kernel` and
   `sdk/def/plugin`, declared once in `sdk/def/plugin` as `major.minor` at `0.y`
   (`08:918`; community:152). `plugingen` writes the version a plugin was built against
   into its `requires`, and the builder writes the version a binary carries into
   `serves`. While it is `0.y`, check 1 needs the exact minor (`08:497`, `:918`). A CI
   check compares the exported API of both packages with a recorded copy and fails when
   it changed and the version did not.
9. **`requires` and `serves`.** `requires.definitions` is keyed by definition name, the
   `<name>` of `sdk/def/<name>/v<N>`; `plugingen` maps each key and hook a `Spec` names
   to its definition through the package that declares it. The kernel built-ins
   (`reflect`, `registry`, `hooks`, `logger`) belong to `pluginApi`, not to a definition.
   `serves` names, per definition, each served major with its minor, `compatibleFrom`,
   `deprecated` and the classes and stages of decision 7, so check 2, the deprecation
   warning and the offline gate need no `definition.yaml` (`08:498`, `:936`; `07:538`,
   `:1895`). It lists every definition the binary links, at the version linked; whether
   a provider row is composed is the static graph's question, not check 2's. A pack's
   `requires.definitions` is derived by `plugin check --write` from the registries its
   rows feed (`07:139`; community:134).
10. **The suite revision** is the integer `suite` of `definition.yaml`, raised when a
    suite adds tests inside a minor (`07:540`). The attestation names it (decision 17).
11. **`plugingen` writes the generated half of `plugin.yaml`.** It is a package of the
    root module compiled into `aiopsctl` and run as `aiopsctl plugin check --write`
    (`07:42`, `:1541`), so an author runs it with no checkout. It writes `uses` (with
    `as: need|want`), `provides`, `registers`, `hooks` with their stages,
    `configSchema`, `durable` and `requires` (`07:102-115`), and the tests of decision
    12. It reads the `Spec` without a kernel change, without an `init` registry and
    without running a plugin's `Apply`. Where `registers` comes from is fixed in ADR-048:
    the `Spec` of `07:215-223` has no field for it, a `Need` on a registry key reads the
    same for a consumer and a contributor, and the gate and the tenant-toggleable rule
    read it without running code (`07:135`, `:141`). A CI parity check fails when a
    committed manifest differs from what `plugingen` writes (`08:1257`).
12. **The generated tests.** `zz_cycle_test.go` mounts, disposes and remounts three
    times and asserts that every fake's `Inventory()` is back at its baseline, that the
    effect ledger is empty, that no goroutine leaked and that a late registration returns
    `ErrInactiveEffect`. It imports the fake of each definition `Spec.Inject` names, so
    `kerneltest` imports no definition (`07:1430-1434`). Every fake exposes
    `Inventory()`; a fake of a definition that takes no registration reports what its
    callers left behind, timers for `clock`, or nothing. `sdk/testkit` gains goleak for
    it (`07:33`; licence:54). `zz_tenants_test.go` is question Q7.
13. **The S1 code meets the standing rules** as question Q8 decides. Whatever the answer,
    every plugin of a module table gets a manifest with a generated `requires`, because
    check 1 denies a manifest without one (`08:497`). The loader keeps `kernel.Define`,
    since `loader/` imports only the kernel and three definitions (`08:1254`); its
    manifest is written by hand and the parity check compares it with its
    `kernel.Plugin` value.
14. **`boot` stays one package and the home of the gate** (`08:493`; `07:37`). It gains:
    - checks 1, 2, 4, 5, 6 and 10, the signed-key, signed-hook and fake parts of check 7
      and the installed-artifact rule of check 8. Check 7 reads `provides` and `uses`
      from the generated manifests (`08:503`). Its classes come, as ADR-059 decision 20
      has them, from `kernel.KeyClasses` and `kernel.HookSealed` where the module table
      holds the Go value, and otherwise from the definition data of decision 7 in place
      of `modules.yaml`; the test of decision 7 holds the two equal, and `dist.yaml` still
      lists no class;
    - readers of manifest 1.0, of the signed distribution manifest, of `trust.yaml`, of
      `plugins.lock` and of what install recorded beside each digest (`07:1880`). `boot`
      may not import `sdk/def/plugin` (`08:1254`), so it reads manifests into types of
      its own, and a test holds its reading of the schema equal to `plugingen`'s;
    - `minStability` in `profile.yaml`: absent, it sets no minimum; stabilities order
      `experimental < preview < ga` (`07:93`, `:127`);
    - the per-plugin `configSchema`, merged for `--dump-config-schema` in place of the
      S1 stand-in (`08:364`), and the derived volatile paths;
    - the declared stage of every hook listener in the dumps (s1-report:17, :211);
    - the print of every departure from the release bindings, of `allowUnsigned` and of
      the accepted licences (`08:403`, `:404`, `:1344`).

    The S1 stand-ins `dist/dist.yaml` and `modules.yaml` give way to the signed manifest
    (decision 19). Its provisional cap is in decision 32.
15. **The signed part of gate check 7** reads the `signed` class of a key and the access
    class of a hook from the definition data of decision 7, carried into `serves`.
    `kernel.KeyClasses` reports a signed key as consumer-bound and the kernel has no
    signed hook (`sdk/kernel/key.go`). No definition of S2a declares a signed key, so a
    test definition declares one for the gate cases; `identities`, `approval` and
    `gateway` bring the real ones at S5 and S7 (`07:558`).
16. **One gate text.** Where `08:495-506` and `07:1882-1893` differ, S2a builds every
    clause of both: check 1 compares `requires.pluginApi` with the `serves` of the signed
    manifest of the binary; check 3 keeps its registration clause for S2b; check 5 keeps
    "no verified signer recorded beside the digest" and its index clause waits for S6a;
    check 10 keeps the scenario version of a pack. Check 1 denies `wasm` (`08:381`).
    Every check runs on every row of every kind; a kind is exempt from a clause only where
    the clause says so ("for in-process code", "for a pack"). The commit that builds the
    gate edits the two tables into one text: `08` §1.6 holds it and `07` §9 links to it.
    The same commit checks every copy of checks 8 and 10 and of the instantiable list
    (`00` §6.1, `07` §8.4 and §9, community §6.4 and §6.5, harness §5.3) against W9,
    which `07:132` and `08:391` already follow, so the wording for `tool-mcp` is done
    before S2a ends (harness:21, :335).
17. **The attestation** is a typed statement whose type carries its version (`08:920`).
    It names the subject digest, and per definition the version and the suite revision
    that passed; for a pack also the scenario version (`08:506`, `:895`; `07:1512`).
    `plugin pack` records it and `plugin sign` signs it with the artifact. Its subject is
    the digest of what the gate admits, so "an attestation that names another digest" is
    decidable. ADR-020 fixes that digest per kind of artifact, the type URI, and whether
    the statement is a layer of the artifact or an OCI referrer. The attestation type and
    the OCI layout of decision 18 are documented in the repository for third parties
    (`08:935`); making them public waits for L14 (`08:1342`).
18. **The artifact form** (ADR-047): one OCI artifact with the module path, version and
    sum of in-process code, or nothing for a pack; `plugin.yaml`; a bundle of rows, all
    disabled; for a pack its content and fixtures; the signed attestation; an SBOM
    (`08:391`; `07:1838`). Check 8 reads `instantiable: true` from the manifest of a
    first-party plugin of the module table; no list of ids is written in code
    (`07:132`). The rows of an installed artifact mount under one group derived from the
    whole plugin id, so two owners with the same plugin name never share a group
    (`08:391`), and the loader records each row's origin artifact (`03:64`). Content is
    unpacked under `$AIOPS_HOME/content/<owner>/<name>/` (`07:1328`); its digest,
    recorded in `plugins.lock`, enters the generation digest through the locks section
    that `boot/digest.go` already encodes (`08:184`, `:1287`).
19. **Three files, three names.** The site's `dist.yaml` has the four fields of
    `08:401`. The repository's `dist/dist.yaml` becomes the builder's input for the
    release: its module list, both binding maps, the sealed-hook listeners and the
    required set (`08:1220`). The signed distribution manifest is what the builder writes
    from either: the module table, mapping each plugin id to its value and its manifest
    (`07:1344`), with each module's sum and detected licence; the packages the binary
    links, which the fake clause of check 7 reads at boot; the pins; both binding maps;
    the sealed-hook listeners; the required set; the accepted licences; and `serves`
    (`07:1846`). A release publishes that file for `plugin gate --against`
    (`08:936`). The release module table is generated into `dist/` and committed, CI
    fails when it differs from the generator's output, and `go build ./...` keeps
    building `cmd/aiops`.
20. **The builder** (ADR-044) is a root-module package compiled into `aiopsctl`, run with
    a local toolchain or through the builder image, whose definition lives under `dist/`
    (`08:402`; `08:1201`, assumption 3). It takes the four fields of the site's
    `dist.yaml`, `bindings` included, and any release input in the form of
    `dist/dist.yaml`, so the S3 exit and `dist/contrib.yaml` at S6a need no new builder
    (`08:643`, `:646`). It runs the gate on the manifests of what it builds (`08:493`)
    and does all of `08:402` and `07:1860`:
    - the module table and `go.mod` generated in a scratch directory; `-mod=readonly`,
      cgo off, no `go generate`; strict versions with `--allow-bump <module>`;
    - refusal of a module outside the release that declares an `aiops/` or `contrib/`
      id; refusal of a `fake` or `sdk/testkit` package in the package graph of the
      binary, which is how `07:47` and `08:1258` ("module graph") are read, since
      `sdk/def/go.mod` requires `sdk/testkit` for its tests; the `init` deny-list scan;
    - the licence scan of L16 on detected licences, per directory (`08:1335`, `:1344`);
    - the import boundary and the suites on the source, without `-race`, since the
      builder has cgo off;
    - `THIRD_PARTY_LICENSES/`, the aggregated NOTICE texts, the MPL-2.0 source lines of
      L8 and an SBOM in every image it writes; the release images are built the same way
      (`08:1285`).

    `dist sign` signs the manifest of decision 19 with the site's key, and `dist use
    <image>` verifies it and records its signer (`07:1861-1862`). Rewriting a compose
    file or `image.tag` arrives with `deploy/compose` and `deploy/helm`; until then
    `dist use` records the image reference in the site files and prints it.
21. **The CLI** (`cmd/aiopsctl`) gains `plugin new`, `check`, `smoke`, `pack`, `sign`,
    `push`, `inspect`, `install`, `verify` and `gate --against`, `trust add`, `dist
    build`, `dist sign`, `dist use` and `profile verify` (`08:366`, `:694`, `:936`), on
    an operations library that `plugin-manager` later shares, under one profile write
    lock (`08:369`; `03:71`).
    - `install` runs every check it can before it answers `rebuild-required`, writes
      nothing on a denial, and restores a snapshot of the profile definition,
      `plugins.lock` and the profile patch on a failure (`08:374`). `plugin enable` and
      `disable` are question Q11.
    - `inspect` fetches only an artifact's manifest and bundle, never its content, module
      or image, and installs and runs nothing; that is the reading of "before anything
      is fetched or run" (`08:373`), since a remote spec cannot be shown unread.
    - `plugin check` runs the eight steps of `07:1565-1574`. It runs step 5 itself, for
      every definition the generated manifest names, so the attestation records the
      suites that ran and not those an author's test happens to call (`07:1512`,
      `:1571`). Step 3 runs the import analyzer and plugin vet, which move out of
      `tools/kernelcheck` (a `main` package with repository paths) into a package that
      `tools/kernelcheck` and `aiopsctl` share and that works from any module root
      (`07:1569`). Step 6 runs for `tools` and `executors` contributions, and none
      exists before S5. Step 8 is question Q13. A manifest that lists `remote` is
      refused until S2b, since step 5 cannot run in a runtime that does not exist. The
      name lint of step 1 covers the marks of `00` §8.1, the reserved owners and words,
      and the forms of T7 for a plugin whose manifest has `integrates` (`08:1362`). The
      check refuses an artifact that carries a dashboard-server `plugin.json`
      (`08:391`), and checks SPDX expressions against list data embedded in `aiopsctl`,
      which gets its own register entry.
    - T1 runs under `-race`, which needs cgo and a C compiler on the author's machine;
      the template README says so (`07:1399`, `:1529`).
    - How suites run inside `aiopsctl` with no Go toolchain, for a pack and for `plugin
      verify`, is question Q10.
    - `plugin smoke ./ --against <release>` builds a throwaway distribution from the
      release's signed manifest and the plugin with the builder image, boots the
      release's core bundle with the plugin's rows enabled, and passes when every row is
      ACTIVE or says what it waits for (`07:1576`).
    - The exit codes stay the three of ADR-059 decision 29: a denial, a failed step and
      `rebuild-required` exit 1, with the reason on stderr. The usage text and the table
      in `cmd/aiopsctl/run.go` say so.
22. **Templates** live in `templates/`, embedded in `aiopsctl` through a Go package; the
    template files carry names the Go tool does not build (`07:1549`; `08:1218`). S2a
    writes a Go template for each registry that exists, `echoers` and `stampers`, with
    the files of `07:1551-1559`, and the connector-pack template with `runtimes: []`,
    whose rows are rows of an instantiable plugin (`08:897`, `:1263`). `--contrib`
    writes the `contrib` owner (`08:407`). The CI template job generates a plugin from
    each template and asserts that it compiles, passes T0 and the generated tests, and
    fails conformance with exactly one line per obligation (`08:1263`; `07:1561`).
23. **The handshake message** is frozen as question Q6 decides. Check 3 stays S2b.
24. **`trust.yaml`** has `signers`, each limited to id patterns and runtimes,
    `distributions` and `allowUnsigned` from S2a; `indexes`, `install --index` and
    `requireAttestations` per index arrive at S6a (`08:938`; community:206).
    `requireAttestations` per signer is ADR-020's. Where the verified signer is kept
    beside a digest, and how that record is protected until boot, is fixed in ADR-020
    (`07:75` R17, `:1841`). `boot/` imports neither cosign nor sigstore-go (`08:404`).
    `trust.yaml` is one file per site under `$AIOPS_HOME` (`07:1840`). Whether
    `plugins.lock` is one file per site (`07:1841`) or per profile, as `install
    --profile` and its restore rule suggest (`08:374`, `:395`), is fixed in ADR-047. The
    credential classes these files hold already exist (`00:410-412`).
25. **The `dev` profile** exists from S2a as `profiles/dev/profile.yaml`, listing only
    bundles that hold a built row: at S2a `aiops-core` with the S1 rows (`08:687`;
    `00:275`). Each later step adds its bundle to `dev` with its first row. The S2a and
    S2b exits install into `dev` (`08:687`).

### What waits, and why

26. The parts below are not built at S2a.

| Part | Step | Why |
|---|---|---|
| Gate checks 3 and 9; `runtimes: [remote]` and `requires.remoteProtocol`; `plugin.FromManifest`; the adapter generator and the `contrib` mark of the toy registries; the Python template | S2b | The remote runtime is S2b (`08:695`; `07:1549`), and a part lands with its first consumer (`08:678`) |
| `Spec.Durable` and the manifest's `durable` entries; `sqlfake`, embedded PostgreSQL and testcontainers-go in `sdk/testkit` | S3 | The `durable` and `sql` definitions are S3 (`08:696`; `07:1444`) |
| The assertions of the two-tenant test | S4a | Question Q7 |
| The tenant overlay part of check 8; layer 5 | S4b | ADR-059 decision 4; `08:111` |
| Real signed keys; the effects step of `plugin check`; `sdk/gate` | S5, S7, S8a | Their definitions arrive then (`07:558`, `:1572`; `08:698`) |
| The index; `indexes` in `trust.yaml`; `install --index`; the index clause of check 5 | S6a | `08:938`; `07:1888` |
| Exemptions | S6b | `08:508` |
| `dist use` rewriting compose and Helm files | S7 and later | Those files do not exist yet (decision 20) |
| `tool-mcp`, the connector scenario, the level in a harness's attestation | S8a, S8b | `08:727`; `07:1477` |
| `content`, the row that serves a pack's unpacked content | S8b | `08:728`; until then a pack's own rows read their config only |
| The audit record of each generation's digest and signer set | S5 | `audit-chain` is S5 (`08:403`, `:698`) |

### The exit test, made checkable

27. **The exit test is `test/authoring/`**, run by the CI job `s2a-exit` and by `make
    s2a-exit`. It first builds what a release pipeline would build, with throwaway keys,
    and publishes nothing outside the test (ADR-057 decision 5; `08:1342` L14). It then
    drives two environments that hold no repository checkout, no `replace` line and no
    module cache of the repository: the author's repository and the clean machine.
    Questions Q2 and Q4 fix the release they build on and how they reach the modules.
    The clean machine has no Go toolchain and builds through the builder image
    (`08:402`); a second job runs the same steps with a local toolchain. Each step passes
    only as stated:
    - **A1.** `aiopsctl plugin new echoers --lang go` in an empty directory writes the
      skeleton; it compiles, and `plugin check` fails with one line per obligation of the
      `echoers` suite.
    - **A2.** With the stubs filled in from the test's fixture, an in-process Go echoer
      (`08:694`; community:59; plugin record §16), `plugin check` passes steps 1 to 5
      and 7 and reports steps 6 and 8 as not applicable. Its `--json` report is kept.
    - **A3.** `plugin pack` and `plugin sign` with the author's key write one artifact.
      Its manifest carries the generated `requires`, its bundle holds one disabled row,
      and its attestation names the module's digest and `echoers` at the served version
      with its suite revision. `plugin push` sends it to the test registry.
    - **P1.** `aiopsctl plugin new connector` writes a pack that fails conformance per
      obligation. Filled in from the test's fixture, it passes `plugin check` and is
      packed, signed and pushed.
    - **C1.** On the clean machine, `trust add` lists each author for its ids and
      runtimes and the site's key under `distributions`; `plugin inspect` prints each
      spec's rows, configs and content paths (`08:373`).
    - **C2.** `plugin install <echoer>` answers `rebuild-required`, prints the `dist.yaml`
      lines and changes no file. `dist build` passes every check of decision 20 and
      writes the image with `THIRD_PARTY_LICENSES/`, the NOTICE texts and an SBOM, and a
      signed manifest whose `serves` holds the `echoers` version. `dist sign` and `dist
      use` record the site as the signer. `plugin install <echoer>` run again appends its
      bundle with the row disabled (`08:397`; `07:1859-1863`).
    - **C3.** `plugin install <pack> --profile dev` writes `plugins.lock` by digest,
      unpacks the content and appends the bundle with every row disabled, with no rebuild
      (`08:395`).
    - **C4.** The rows are enabled (question Q11) and reach the running process (question
      Q12); every row of the composition is ACTIVE.
    - **C5.** Both are called (question Q1): each echoer answers through the dispatcher
      with the reply its code or its row config fixes.
    - **C6.** The composition holds no policy row and no tenant row (`08:694`).
    - **D1.** A copy of the echoer whose `requires` names an `echoers` minor the release
      does not serve, made without `plugin check` (whose parity step would refuse it) and
      attested and signed by the trusted author so that only the version differs, is
      denied at `plugin gate --against` the release's signed manifest, at
      `plugin install` and at `dist build`. Each denial names check 2 and no other, and
      no site file changes.
    - **D2.** A copy whose attestation names another subject digest, signed by the
      trusted author, is denied at `plugin gate --against` and at `plugin install`. Each
      denial names check 10 and no other, and no site file changes.

    The exit texts of `08:620`, `08:694`, `00:571`, community:59 and plugin record §16
    are edited to one text with the answer to question Q1.
28. **The other refusals** are table cases of the tests of `boot`: every denial clause of
    checks 1, 2, 4, 5, 6 and 10 and of the S2a parts of checks 7 and 8, at boot and at a
    live generation, including the four refusals that leg D reuses at S8b (`00:557`;
    `08:746`). Each cell of the failure-matrix row "Gate or signature denies" (`08:467`)
    that is not n/a gains a trust, a version and an attestation denial beside the binding
    fault of S1, marked `since: S2a`; the table of `08` §1.6 does not change.

### Working rules

29. **Branch, review and the standing rules.**
    - S2a work lands on the branch `s2a/integration` and reaches `main` by merge.
    - Test first: the acceptance test of each deliverable is committed, failing, before
      the code that makes it pass, and every fix lands with a test that fails on its base
      (s1-report §6).
    - An adversarial review round runs on each finished group (the authoring surface and
      the definitions; the gate; the CLI, the builder and signing; the exit test) and
      once more on the whole step. Each finding is confirmed by a reproduction before it
      is fixed. A group merges into `main` only when its round leaves no open finding.
    - Commits follow Conventional Commits. Nothing is published outside the private
      repository; the exit's registry, module proxy and keys exist only inside the test.
    - S2a closes on the owner's approval of an exit report, `docs/design/s2a-report.md`,
      in the form of the S1 report.
    - The standing rules of `08:676` apply from the first S2a merge into `main`:
      1. No definition merges without its fake, its suite, its `definition.yaml`, a
         provider and a consumer: a definition lint in `tools/` checks the files, that a
         plugin of a repository distribution, the test distributions included, provides
         and consumes each key, and the rules of `07:514-520` that a static check can
         decide (the method set, the caller's context first, `Inventory()`, no upstream
         import); the suites prove `ErrInactive`.
      2. No plugin merges without `aiopsctl plugin check` and a T3 test: `make check` and
         CI run `plugin check` on every plugin under `plugins/` and `examples/`, and a T3
         suite boots each plugin's row.
      3. Nothing outside the closed vocabularies merges into `sdk/kernel`, `boot/` or
         `loader/`, checked as question Q9 decides.
      4. Whoever changes a `v0` definition fixes every plugin under `plugins/` and
         `contrib/` in the same pull request. Until replay exists (S8a), "as far as
         replay can prove" is read as the build, `plugin check` and every suite staying
         green (`08:676`, `:907`; ADR-059:407-409).
30. **What each ADR due at S2a settles**, at least:
    - ADR-020: the signing library of `plugin sign` and `dist sign` (R17); the signature
      format; the key format and the id-pattern syntax that `trust add` takes; how
      `trust.yaml` trusts a signer for a `signed` key (`07:558`); the attestation type
      and its subject per kind of artifact (decision 17); where the verified signer is
      recorded, and the attestation of an in-process artifact, and how both are protected
      until boot; how first-party rows pass checks 5 and 10 (question Q14); the
      transitive licences of sigstore-go (`08:1336` L8, `:1341` L13).
    - ADR-044: the builder image, its base layer and its SBOM tool, read before S2a ends
      (`08:1341`, `:1380`), and what the image itself carries: the Go licence and PATENTS
      texts, the licence of each tool in it, an image SBOM and a source offer for the
      copyleft packages of its base layer (`08:402`); the module cache format; when
      `--allow-bump` is acceptable; the content of the `init` deny-list; how the licence
      scan runs the same with a local toolchain; where a self-built distribution lists
      sealed-hook listeners and raises the required set, since `dist.yaml` has four
      fields and W3 allows a raise (`08:401`, `:403`).
    - ADR-047, its S2a part: the OCI layout and media types; the group id of an
      artifact's rows; how a profile's bundle list names an artifact's bundle, which has
      no name of its own (community:296); the content layer and its digest; where
      `plugins.lock` lives (decision 24).
    - ADR-048, its S2a part: every field of manifest schema 1.0, the inner shapes of
      `uses`, `registers`, `hooks`, `requires` and `integrates` (`07:66` R8), where
      `registers` comes from (decision 11), whether `scope` is a list or one value
      (`07:94`, `:128`), the reserved owners, and the strict reader of `plugin check`
      beside the lenient reader of the gate (`07:120`).
    - ADR-054: the register schema, which of U1 to U8 run from S2a (question Q15), the
      exception list and the base provider allow-list.
31. **The licence rule for new dependencies.** A new linked module, baked-in tool, image
    or embedded data set merges only with:
    - its licence read at a primary source at the pinned version and entered in the
      licence record (`08:1341` L13);
    - its transitive licences scanned on the package graph of every shipped artifact that
      links it, `aiopsctl` included (`08:1381`);
    - an entry in the upstream register as ADR-054 sets it (`08:1331` L3);
    - for anything linked, a licence on the in-process allow-list (`08:1335` L7).

    None merges `unverified`. Before S2a ends these are read: the transitive licences of
    sigstore-go, the builder's base layer and SBOM tool, the base layer of the server
    image the builder writes, the OCI client (ORAS is read, licence:43; no other is) and
    the SPDX list data; goleak gets its entry (licence:54). cosign stays a CI tool, baked
    into no image (`08:1379`). `sdk/def` gains no require line, and nothing adds a
    protobuf dependency before S2b (question Q6).

### Line budgets

32. Physical lines of production Go, measured by `tools/kernelcheck lines` and enforced in
    CI and the Makefile from each package's first commit; each package is one directory.
    At the S2a exit each cap becomes the measured count plus ten per cent, rounded up to
    the next hundred (the rule of ADR-059 decision 11), and this ADR, `ci.yml` and the
    Makefile change together. Growth past a provisional cap before the exit needs the
    owner's word.

| Package | Measured at `491a518` | Cap during S2a |
|---|---|---|
| `sdk/kernel` | 8,043 | 8,200, unchanged (ADR-034) |
| `boot` | 6,173 | 9,000, provisional |
| `loader` | 1,100 | 1,300, unchanged |
| `sdk/def/plugin` | new | 2,500, provisional |
| each toy definition package; its fake and its suite are packages of their own | new | 800 each, provisional |
| `cmd/aiopsctl` | 210 (s1-report:75) | 2,500, provisional |
| the operations library | new | 3,500, provisional |
| the builder | new | 3,000, provisional |
| `plugingen` | new | 1,500, provisional |

The coverage gate of 100 per cent stays on `sdk/kernel`, `boot` and `loader` (`08:95`,
row 53), so it covers the new gate checks.

### CI jobs

33. Added to `.github/workflows/ci.yml` (question Q16) and mirrored in the Makefile:
    manifest parity; the definition lint; the `pluginApi` check; the template job;
    `plugin check` on every plugin; the builder image build; the licence report of
    `aiopsctl` and of the release image; the register check; the `s2a-exit` job and its
    local-toolchain variant; the caps of decision 32. None pushes, publishes or holds a
    long-lived key.

## Consequences

- The S2a exit is decidable: ten steps, and two denials at each entry point a copy
  reaches.
- `boot` may grow by up to 2,827 lines under its provisional cap and stays one package.
  A second package under `boot/`, the alternative, would need an ADR of its own.
- The hook dumps change from stage 0 to the declared stage; the goldens are regenerated
  and reviewed in one commit.
- The S1 stand-ins (`dist/dist.yaml` in its S1 schema, `modules.yaml`, the hand-written
  volatile lists, the inferred config schema) are removed by the commits that replace
  them.
- `sdk/def` gains packages and no require line; `sdk/testkit` gains goleak; the root
  module gains the dependencies that ADR-020 and ADR-044 choose, under decision 31.
- On acceptance this ADR enters the ADR table of `08` §3 and the next free number there
  becomes ADR-061. A proposed ADR is not listed there, as ADR-058 is not.

## Questions for the owner

Each question gives its options, the recommended one, why, and when the answer is needed.

**Q1. Does the exit call both plugins, and through what?** `08:694`, `08:620`,
community:59 and plugin record §16 say "calls both"; `00:571` and `01:290` do not, and
`08:3` lets `00` win. No surface calls a plugin at S2a: `http` is S3, `admin-api` S4a, and
the control rows of ADR-059 decision 9 are test-only.
- (a) Keep it. A toy caller row of the exit's release calls each echoer through the
  dispatcher when a control file asks and writes the replies to a file the test reads,
  as the control rows of ADR-059 decision 9 do. `00:571` and `01:290` gain "and calls
  both".
- (b) Keep it, through an `aiopsctl` command that a `commands` row contributes, which
  brings the `cli` rows forward.
- (c) Follow `00`: drop "calls both" from `08` and the records.
- Recommended: (a). The stricter text stays, with Q2 (a) nothing ships that a release
  would hold, and a call proves that the contribution runs, which a boot alone does not.
  Needed before any S2a code.

**Q2. What release does the exit build on, and where do the toy rows live?** A pack may
insert only rows of first-party instantiable plugins (`07:132`), and none exists at S2a.
The exit also needs the dispatcher, which the plugin record puts on a test-only
profile (plugin record §5), and the caller of Q1. No release exists.
- (a) A test release: CI builds, with the S2a builder and a throwaway release key, a
  distribution of the release table plus the dispatcher, a toy instantiable echoer that
  contributes to `echoers` and nothing else, and the toy caller, all under `examples/`,
  and the `aiopsctl` of that distribution. Their rows enter the exit's `dev` composition
  through the site's profile layer (`08:131`). The shipped `aiops` table gains nothing,
  and the instantiable plugins at `0.1.0` stay the three of `07:132`.
- (b) The release table holds the toy modules in no shipped bundle, as W8 does for the
  optional upstream plugins, and the toy instantiable echoer leaves the table at S8a.
- (c) The pack half of the exit waits for `tool-mcp` at S8a.
- Recommended: (a). (b) ships toy code in the image and (c) weakens a written exit; (a)
  proves the same mechanism on a release built by the same steps. Needed before any S2a
  code.

**Q3. Where do the toy definitions live?** `08:1219`, `00:208` and `07:36` put them under
`examples/` in the root module. `00:206` and `07:28` put every definition under
`sdk/def/<name>/v<N>`, and `07:51` lets a third party import only `sdk/def`, `sdk/testkit`
and `aiopsctl`, while the exit's echoer imports the `echoers` definition.
- (a) `sdk/def/echoers/v0` and `sdk/def/stampers/v0` in the SDK module, at stability
  `experimental`; `examples/` keeps the toy plugins of Q2. `00:208` and `08` §4 are
  edited.
- (b) `examples/` becomes a nested module that requires only `sdk/def`, served to third
  parties as the SDK modules are.
- (c) The root module, which a third party then requires with pgx, River and the YAML
  library in its graph, against `07:51`.
- Recommended: (a). It keeps the definition rule of `00` §4.1 and the author's dependency
  list of `07:51`, and adds no module. Needed before any S2a code.

**Q4. How does a repository with no kernel checkout reach the SDK modules and
`aiopsctl`?** The modules are `v0.0.0` behind `replace` lines in a private repository,
L14 gates any disclosure (`08:1342`), ADR-057 decision 5 rules out a public release, and
SDK preview tags are not planned (community:409).
- (a) The exit job serves the three modules, at the commit under test, from a file-based
  module proxy that exists only inside the test, with pseudo-versions, together with the
  author's echoer module at the version its artifact names, and hands both environments
  the `aiopsctl` it built. Nothing is tagged or published.
- (b) Version tags in the private repository, fetched with repository credentials.
- (c) Wait for the L14 clearance and public tags; the exit cannot run before.
- Recommended: (a). It proves "no kernel checkout" exactly, with no repository and no
  `replace` line, and needs no publication and no credential. Needed before any S2a
  code.

**Q5. May YAML be read and written outside `boot/`?** ADR-059 decision 6 allows the one
YAML library in `boot/` and `tools/` only. `aiopsctl` must write `plugins.lock`,
`trust.yaml`, the profile's bundle list and the generated half of `plugin.yaml`, keeping
the hand-written half and its comments, and the operations library is later shared with
`plugin-manager` (`08:369`).
- (a) Amend decision 6: the same library is also allowed in the operations library and
  in `plugingen`, both root-module packages under `internal/`, and never in `sdk/`,
  `loader/` or `plugins/`. `boot` keeps every read the gate needs. Whether
  `plugin-manager` may import the operations library is decided at S6a.
- (b) `boot` exports read and write helpers that the CLI calls, and grows further.
- (c) The CLI writes YAML as text without the library, and `boot` reads it.
- Recommended: (a). One library still, reading in the gate and writing where files are
  written; (b) puts file editing in the launcher, and (c) cannot edit a file and keep
  its comments. Needed before the first file writer of the CLI merges.

**Q6. How is the handshake message frozen?** `08:642`, `:892` and `:1212` freeze it at S2a
in its own package, `aiops.remote.handshake` (`07:1120`). `07:60` (R2) fixes the home of
the remote packages at S2b and keeps the require list of `sdk/def`, `07:67` (R9) calls its
field names descriptive, and nothing reads it before S2b.
- (a) S2a commits `sdk/remote/handshake/handshake.proto` with the three fields of
  `07:1126-1130` and a test that fails on any change other than an added field. No
  generated Go and no protobuf dependency until S2b.
- (b) As (a), plus generated Go in a module of its own, with protobuf-go entering at S2a.
- (c) Freeze the text of `07` §5 only.
- Recommended: (a). The wire contract is frozen and tested from S2a, and the module and
  dependency choices stay with S2b, as R2 says. Needed before S2a closes.

**Q7. What do the two-tenant tests do at S2a?** `plugingen` writes `zz_tenants_test.go`
for every process-scope plugin (`07:1435`; `00:298`), a template must pass the generated
tests (`07:1561`), and both halves of every registry suite carry a two-tenant test and a
disabled-for-tenant test (`07:1463`; `00:299`). These need `tenantsfake.As`, the `tenants`
definition lands at S4a (`08:697`), and S2a involves no tenant data (`08:694`).
- (a) `plugingen` writes the file and the toy suites hold those cases from S2a; until S4a
  they skip with "not yet (S4a)", never count as a pass, and `plugin check`, the template
  job and the attestation report them. At S4a they assert, and `plugin check --write`
  regenerates the file.
- (b) A minimal `tenants` definition and fake arrive at S2a.
- (c) The file and the suite cases are written only from S4a.
- Recommended: (a). The file and its place in the author's tree are fixed at S2a, and
  the skip is visible, as matrix cells of later steps are (ADR-059 decision 3); (b) lands
  a definition before its consumer (`08:678`). Needed before `plugingen` merges.

**Q8. Is the S1 code brought to the standing rules?** `sdk/def/reload/v0` has no
`definition.yaml`, only `sdk/def/loader/v0` has a suite, and `clock`, `config-refs`,
`readiness`, `reload` and `restart-policy` use `kernel.Define` with hand-written manifests
in S1 fields (`needs`, not `uses`).
- (a) All of it at S2a: the `reload` definition file, suites on `conformance.Run(t,
  Mount)` for `clock`, `composition`, `configrefs`, `launch` and `reload`, and the five
  core plugins moved to `plugin.Define` with generated manifests and `plugin check`.
- (b) Generated manifests for every plugin of the module table now (check 1 needs them),
  the suites and `plugin.Define` when each is next changed.
- (c) The rules bind new merges only.
- Recommended: (a). The rules say "no definition merges without", and the gate denies a
  row without `requires`; (a) makes S2a's own distribution meet them, at the cost of five
  suites. Needed before gate check 1 merges.

**Q9. What does the vocabulary rule check?** `00:262` lists the kernel's banned words
(tenant, role, profile, row, SQL, HTTP, job, approval, audit, model, health, gate) and
gives `boot/` and `loader/` "a closed vocabulary" with no list.
`tools/kernelcheck/vocabulary.txt` holds twelve words, only two of them (tenant,
approval) from that list, and runs on `sdk/kernel` and `loader/` only. At `491a518` the kernel uses role, row, model and gate in other senses,
and `boot` uses "incident" once.
- (a) The kernel's list gains every `00` §4.3 word the kernel does not use; the four it
  uses in another sense are listed in the S2a report for review; `boot/` gets a list of
  domain words (incident, alert, approval, dashboard, pillar, harness, remediation,
  estate and the upstream names) and its one occurrence is reworded.
- (b) The `00` §4.3 list as written, with the kernel's comments reworded through
  ADR-034.
- (c) The S1 check stays and the rest is reviewed by hand.
- Recommended: (a). It checks every word a grep can decide and records the rest; (b)
  changes the closed kernel for wording. Needed before the first S2a merge into `main`.

**Q10. How do suites run inside `aiopsctl` without a Go toolchain?** A pack author has
only `aiopsctl` (`07:51`); `plugin check` runs the suites of the registries a pack's rows
feed and its scenario (`07:1571`, `:1574`), and `plugin verify` re-runs suites
(`07:1512`). Suites take a `*testing.T` (ADR-059 decision 32), and `kerneltest.New` needs
a `testing.TB`, which keeps a kernel with both binding maps off out of server binaries
(`07:565`).
- (a) The release build compiles each suite, with the first-party plugins it runs on,
  into a test binary that `aiopsctl` embeds and runs as a child process with the pack as
  input; its JSON output is the report.
- (b) `aiopsctl` calls `testing.MainStart`, which the Go 1 compatibility promise does not
  cover, pinned to the toolchain of `go.mod`.
- (c) Suites take an interface that a runner in `aiopsctl` implements, and `kerneltest`
  gains a second constructor for that runner.
- Recommended: (a). It uses only stable Go interfaces and keeps the `testing.TB` fence of
  `kerneltest` whole; (c) weakens that fence. Needed before `plugin check` for a pack
  merges.

**Q11. Are `plugin enable` and `disable` built at S2a?** `08:694` does not list them and
`08:375` dates the manager's `enable` at S6a, but every install path of `08:395-397` ends
in `plugin enable`, and `08:369` and `03:71` have the `aiopsctl plugin` commands work on
the profile files from S2a.
- (a) `aiopsctl plugin enable` and `disable` at S2a, on the operations library, writing
  only a departure from the bundle layers into the profile layer, with no `--tenant`.
- (b) They wait for S6a; the exit enables its rows with a hand-written profile patch.
- Recommended: (a). The install paths then run as written, and S6a adds only the service
  around the same operation. Needed before the exit test is written.

**Q12. How does an enabled row reach the running process at S2a?** Enabling a row whose
code is present is live (`08:190`, `:386-387`), and every operation is a submission to
the reload queue (`08:369`). Nothing submits at S2a: `admin-api` is S4a, and
`reload-watch-files`, which `dev` turns on (`08:240`), is built by no step.
- (a) `reload-watch-files` is built at S2a as a row of `aiops-dev`, the bundle that turns
  it on (`00:291`), and a bound consumer of `reload`, so the exit's enable is applied
  live through the queue.
- (b) The files change, and the next start of the same image applies them: no rebuild,
  one restart.
- (c) `aiopsctl` submits to the running queue through a local channel built for it.
- Recommended: (a). The exit then proves the live property of the runtime table on the
  profile the exits install into; (b) meets the exit text but not the table, and (c) adds
  a channel that `admin-api` later duplicates. Needed before the exit test is written.

**Q13. What scenario does a pack attest at S2a?** Check 10 denies a pack whose attestation
names no scenario version (`08:506`), and step 8 of `plugin check` runs the connector
scenario (`07:1574`), which needs `tool-mcp` and the incident loop (S8a, S8b).
- (a) S2a defines a scenario as a named, versioned script run inside `aiopsctl`. The S2a
  pack carries the echo scenario: replayed from the pack's fixtures, each of its rows is
  called through the `echoers` dispatcher. The connector scenario is the second, at S8b.
- (b) The pack clause of check 10 and step 8 report "not evaluated until S8b", and the
  S2a pack is admitted without them.
- Recommended: (a). Check 10 stays whole, and the attestation field and step 8 exist
  from the first pack. Needed before gate check 10 merges.

**Q14. How do the first-party rows of a release pass checks 5 and 10?** Check 5 denies a
Go module absent from a distribution manifest that `dist use` verified and `trust.yaml`
lists (`07:1861`), and check 10 denies a row with no attestation (`08:506`). The release
image runs without `dist use`, and a fresh site has no `trust.yaml`.
- (a) The release pipeline attests each first-party module and records the attestations
  in the release's signed manifest; a site lists the release key under `distributions`
  with `aiopsctl trust add` before its first boot, and boot refuses with that instruction
  otherwise.
- (b) As (a), but the release image carries its own key as a default distribution root,
  printed at boot and removable in `trust.yaml`.
- (c) Checks 5 and 10 apply to modules outside the release table only.
- Recommended: (a). Both checks hold for every row and trust stays in `trust.yaml`; (b)
  trusts by default, and (c) narrows two written checks. The answer goes into ADR-020.
  Needed before gate check 5 merges.

**Q15. Is the upstream register built at S2a?** `08:790` puts the register at S2a (UP-1,
ADR-054), but the 36-row list that holds UP-1 is pending the owner (`08:752`), and every
dependency S2a adds needs an entry (L3, L13).
- (a) S2a writes `tools/upstream-components.yaml` with an entry for every module, image
  and tool that S2a links, ships or bakes in, and runs the register check (U7) on it; U1
  to U3 start when ADR-054 says.
- (b) The register with entries only; every check waits for S8b.
- (c) Nothing until the owner decides the 36-row list.
- Recommended: (a). The licence rule of decision 31 needs the register now, and U7 is
  the check that makes an entry required. Needed before the first new dependency merges.

**Q16. May S2a add CI jobs?** ADR-057 decision 5 says not to add CI/CD configuration or
create workflows. `.github/workflows/ci.yml` exists since 2026-11-18, and ADR-059
decisions 11 and 28, which the owner accepted, name it.
- (a) Confirm that S2a extends the existing `ci.yml` with the jobs of decision 33, none of
  which pushes or publishes, and that ADR-057 decision 5 still rules out any job that
  publishes or changes the repository's visibility.
- (b) No new jobs: the S2a checks run through Makefile targets only.
- Recommended: (a). The standing rules are CI checks (`08:1253`, `:1260`), and (a) records
  the authority the existing file already rests on. Needed before the first S2a merge
  into `main`.
