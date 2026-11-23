# ADR-060: Scope and working rules of S2a (the authoring path)

- Status: **accepted 2026-11-23 by the project owner.** The owner answered all 22
  questions on the same date with the recommended option of each; the section "Owner
  answers of 2026-11-23" records them as decisions 37 to 58. S2a starts with this scope
  (s1-report:29).
- Scope: roadmap step S2a (`08-profiles-roadmap-adrs.md` §2.1). It turns the S2a text of
  the roadmap into decidable work and working rules, as ADR-059 did for S1. It changes no
  decision of `00-decisions.md` by itself: every point where an answer would change one
  is a question below and names the lines it edits, and `00` is edited in the commit that
  records the answer (`08:996`).
- Citations: `00`, `01`, `02`, `03`, `07` and `08` are the files of `docs/architecture/`
  with those numbers, and `08:694` is line 694 of `08` at commit `491a518`. "community",
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
- No release exists. The SDK modules are untagged `v0.0.0` behind `replace` lines in a
  private repository (ADR-057; `08:1342` L14), and Go ignores the `replace` lines of a
  dependency, so outside the repository `sdk/def` cannot resolve `sdk/kernel v0.0.0`.
  "No kernel checkout" and "a clean machine" have no stated mechanism.
- `aiopsctl` is the `cli` profile of the same launcher, with commands as rows (`00:515`;
  `08:271`, `:359`), and S1 built its one command, `profile render`, as code of
  `cmd/aiopsctl`.
- The gate runs at six entry points with different inputs (`08:493`), and neither gate
  table says which clauses each entry point can evaluate.
- Once the gate evaluates checks 1, 2, 5, 6 and 10 at boot, the S1 test distribution, an
  unsigned table of toy, fault and control rows (s1-report:82), has no admission path.
- `boot` has 627 lines left under its cap of 6,800 (ADR-059, "Line budgets at the S1
  exit"), and the YAML library is allowed in `boot/` and `tools/` only (ADR-059
  decision 6), while the gate, the manifest, the lock, the trust file and the CLI all
  read or write YAML.
- The gate tables of `08:495-506` and `07:1882-1893` differ in checks 1, 3, 5 and 10.
- "`dist.yaml`" names three files: the site's input (`08:401`), the release's manifest
  (`08:1220`) and the S1 stand-in (`boot/dist.go`).
- No shipped bundle or profile exists: the repository has no `bundles/` or `profiles/`
  directory, and `cmd/aiops` passes no `Shipped` file system (`internal/serve`).
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
   written requirement it touches. Decision 33 lists what each must settle.
3. **No kernel change is planned.** The kernel stays at 8,043 of its 8,200 lines
   (s1-report:65). Stage ordinals and the `signed` class come from definition data
   (decisions 7 and 15), not from a new kernel accessor. A kernel change that S2a finds
   necessary, for example to hand a value committed in place to a running
   `plugin.Volatile[T]` field (`07:1375`), is proposed to the owner as an ADR-034
   addition before it merges, stays inside the cap and is verified as ADR-059 decision 2
   requires: the differential corpus, the property workload and the call-path gate.

### Packages and deliverables

4. **The authoring surface is the package `plugin` of the SDK module** that holds the
   definitions (`07:29`; `00:262`). Its directory, and so the import path every author
   compiles against, is question Q18. `sdk/gate` and `sdk/entity` follow the same answer
   when their consumers create them. The package holds:
   - `Spec[C]` with `ID`, `Inject`, `Provides`, `Hooks`, `Validate` and `Apply`, and
     `Define[C](Spec[C]) kernel.Plugin` (`07:212-226`; `00:207`). `Durable` is added to
     `Spec` at S3 with the `durable` definition (decision 29).
   - `Volatile[T]`. The volatile paths are derived from `C` and carried in the generated
     manifest, so the hand lists in `dist.Module.Volatile` and `boot.ModuleEntry.Volatile`
     go (ADR-059 decision 10). A path is a list of keys, as in `VolatileUpdateArgs.Paths`
     (ADR-059 decision 44).
   - JSON Schema 2020-12 derived from `C` with the standard library, behind a validator
     interface, with a validator of exactly the keywords the derivation emits (`07:29`,
     `:137`; `08:59`). The require list of the SDK module stays the one `00:262` allows,
     `sdk/kernel` and, from S3, pgx v5, with `sdk/testkit` for its tests only (`07:33`,
     `:51`), so no validator library enters it. The tag `aiops:"tenant"` marks a field
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
   failed obligation with its id (`07:1561`, `:1563`). `Inventory()` lists the
   registered names, and the fiber that owns each is the one whose ledger holds its
   label (`kernel.Fiber.Effects`): a `*kernel.Context` gives a registry no fiber
   identity to record, and decision 3 plans no kernel change, so this is the reading
   of "registrations by owning fiber" (`07:519`) at S2a. Both definitions are `remote:
   "no"` at S2a; S2b marks them `contrib` with the adapter generator, as a minor of each
   (plugin record §5; `08:643`, `:695`). The dispatcher `example-dispatch` provides both
   registries and the toy caller of question Q1 consumes both; these and the other toy
   rows of the exit live where question Q2 puts them.
6. **YAML** stays one library, `go.yaml.in/yaml/v3` at its pinned version (ADR-059
   decision 6; licence:44). Where else it may be read and written is question Q5.
7. **`definition.yaml` gets its fixed layout** (`07:71` R13): `key`, `version`,
   `stability`, `class` (one class, or one per key where a definition declares several
   keys, as `launch` does), `remote`, `compatibleFrom`, `deprecated`, the `suite`
   revision (decision 10) and, for each hook the definition owns, its stages in order,
   its access class (`open`, `signed` or `sealed`) and its remote mark (`07:439`,
   `:443`). `sdk/def/reload/v0` gains its file. A test in each definition package proves
   that the file and the Go declarations agree: the key majors, the bound classes against
   `kernel.KeyClasses`, the seals against `kernel.HookSealed`, and the stage order by
   dispatching through the kernel. S2a fixes the field format and the stages of the hooks
   of the definitions that exist at S2a, which the dumps need (s1-report:17, :211). The
   stage vocabulary per definition, and whether golden hook dumps catch a wrong stage,
   stay with ADR-043 at S5 (`00:777`; `08:1174`; `07:68` R10), which may change these
   stages under the `v0` rule.
8. **`pluginApi`** is the version of the author-facing surface of `sdk/kernel` and the
   `plugin` package, declared once in the `plugin` package as `major.minor` at `0.y`
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
    suite adds tests inside a minor (`07:540`). The attestation names it (decision 18).
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
    check 1 denies a manifest without one (`08:497`), and nothing in a generated half is
    written by hand (`08:902`; `00:266`). The loader keeps `kernel.Define`, since
    `loader/` imports only the kernel and three definitions (`08:1254`); `plugingen`
    writes its generated half from its `kernel.Plugin` value, which carries `Inject`,
    `Provides` and `Hooks`, and the parity check compares the committed file with that
    output as for every other plugin.
14. **`boot` stays one package and the home of the gate** (`08:493`; `07:37`). It gains:
    - checks 1, 2, 4, 5, 6 and 10, the signed-key, signed-hook and fake parts of check 7
      and the installed-artifact rule of check 8, each evaluated where decision 17 says.
      Check 7 reads `provides` and `uses` from the generated manifests (`08:503`). Its
      classes come, as ADR-059 decision 20 has them, from `kernel.KeyClasses` and
      `kernel.HookSealed` where the module table holds the Go value, and otherwise from
      the definition data of decision 7 in place of `modules.yaml`; the test of decision
      7 holds the two equal, and `dist.yaml` still lists no class;
    - readers of manifest 1.0, of the signed distribution manifest, of `trust.yaml`, of
      `plugins.lock` and of what install recorded beside each digest (`07:1880`). `boot`
      may not import the `plugin` package (`08:1254`), so it reads manifests into types
      of its own, and a test holds its reading of the schema equal to `plugingen`'s;
    - `minStability` in `profile.yaml`: absent, it sets no minimum; stabilities order
      `experimental < preview < ga` (`07:93`, `:127`);
    - the per-plugin `configSchema`, merged for `--dump-config-schema` in place of the
      S1 stand-in (`08:364`), and the derived volatile paths;
    - the declared stage of every hook listener in the dumps (s1-report:17, :211);
    - the origin artifact of each row in `--dump`, which the loader records (`08:361-362`;
      `03:64`);
    - the print of every departure from the release bindings, of `allowUnsigned` and of
      the accepted licences (`08:403`, `:404`, `:1344`), and a warning on every enabled
      row whose manifest declares `credential: broader` in `integrates` (`08:391`;
      `07:131`).

    The S1 stand-ins `dist/dist.yaml` and `modules.yaml` give way to the signed manifest
    (decision 20). Its provisional cap is in decision 35.
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
    check 10 keeps the scenario version of a pack, and what it asks for a definition the
    plugin only consumes is question Q20. Which check denies `wasm` (`08:381`) is a
    reading of question Q22. Every check runs on every row of every kind; a kind is
    exempt from a clause only where the clause says so ("for in-process code", "for a
    pack"). The commit that builds the gate edits the two tables into one text: `08` §1.6
    holds it and `07` §9 links to it. The same commit checks every copy of checks 8 and
    10 and of the instantiable list (`00` §6.1, `07` §8.4 and §9, community §6.4 and
    §6.5, harness §5.3) against W9, which `07:132` and `08:391` already follow, so the
    wording for `tool-mcp` is done before S2a ends (harness:21, :335).
17. **Where each clause runs.** The gate runs at build, at `plugin check`, at install, at
    boot, at every recomposition and offline (`08:493`), and each entry point evaluates
    the clauses its inputs decide. Every entry point reports each clause it does not
    evaluate as "not evaluated" with the reason, as the slots of later steps did at S1
    (ADR-059 decision 4), and a denial names the check that denied. Checks 3 and 9 are
    S2b everywhere and exemptions S6b.

    | Entry point | Inputs | Evaluated | Not evaluated, and why |
    |---|---|---|---|
    | `dist build` (decision 22) | The manifest in each module's source; the release's signed manifest that `release` names (its module table, pins, bindings, required set and `serves`); the site's `dist.yaml` | 1; 2 against the `serves` of the distribution being built; 6, as the licence scan on detected licences; 7 on the composed module table, with the site's `bindings` | 4 and 8: no profile is composed. 5: the manifest it would read is the build's output, and `dist use` records its signer. 10: `dist.yaml` lists `{module, version, sum}` (`08:401`) and no artifact, so no attestation reaches the builder |
    | `plugin check` step 7 and `plugin pack` | The generated manifest; the attestation just recorded; the signature with the throwaway key; the bundle; the signed manifest of the release this `aiopsctl` was built from, which it embeds as the `cli` profile of the same module table (`08:359`), or the one `--against <dist.yaml>` names | 1; 2; 5 as far as the signature verifies and the manifest digest agrees (`07:1573`); 7 against that release's bindings; 8 on the bundle, with `instantiable` read from that release's manifests; 10 | 4: no profile. The signer and module clauses of 5: no `trust.yaml` and no site distribution. 6: detected licences belong to a build list (`08:1344`) |
    | `plugin gate <spec> --against <dist.yaml>` | The artifact's manifest, bundle, attestation and digest; the published signed manifest | 1; 2; 7; 8; 10 | 4: no profile. 5: no `trust.yaml`. 6: no module source and no build list |
    | `plugin install`, in-process code before the rebuild, and every pack | The artifact and its verified signature; `trust.yaml`; the signed manifest that `dist use` recorded, or with none the release's; the target profile | 1; 2; 4; 5, the signer listed for the id and runtime; 7; 8; 10 | 6 for in-process code: no build list yet. The module clause of 5 for in-process code: when every evaluated clause passes it answers `rebuild-required`, which is not a denial |
    | `plugin install` after `dist use` | As above, with the module table of the recorded distribution | Every clause above, the module clause of 5, and 6 from the detected licences the signed manifest records | None of S2a |
    | Boot and every live generation (`profileContext.Admit`) | The module table and manifests the binary embeds; the signed manifest and the signer and digest that `dist use` recorded (decision 33, ADR-020); `trust.yaml`; `plugins.lock` and what install recorded beside each digest; the composed profile | 1, 2, 4, 5, 6, 7, 8 and 10 on every row | None of S2a |
18. **The attestation** is a typed statement whose type carries its version (`08:920`).
    It names the subject digest; per definition of `requires`, the version; per
    definition whose suite step 5 ran (question Q20), the suite revision and the halves
    that ran (obligations, guard); for a pack the scenario version and the halves of the
    scenario that ran (kernel, harness) (`08:506`, `:895`; `07:1512`; community:182;
    plugin record §12); and the generated tests that did not run, by name (question Q7).
    `plugin pack` records it and `plugin sign` signs it with the artifact. Its subject is
    the digest of what the gate admits, so "an attestation that names another digest" is
    decidable. ADR-020 fixes that digest per kind of artifact, the type URI, and whether
    the statement is a layer of the artifact or an OCI referrer. The attestation type and
    the OCI layout of decision 19 are documented in the repository for third parties
    (`08:935`); making them public waits for L14 (`08:1342`).
19. **The artifact form** (ADR-047): one OCI artifact with the module path, version and
    sum of in-process code, or nothing for a pack; `plugin.yaml`; a bundle of rows, all
    disabled; for a pack its content and fixtures; the signed attestation; an SBOM
    (`08:391`; `07:1838`). Check 8 reads `instantiable: true` from the manifest of a
    first-party plugin of the module table; no list of ids is written in code
    (`07:132`). The rows of an installed artifact mount under one group named by the
    artifact, so a row id is `<pack name>:<row>` (`08:391`; community:298; `03:64`), and
    the loader records each row's origin artifact. A profile names an artifact's bundle
    by its plugin id (community:296). What install does when a second artifact of the
    same name under another owner enters a profile is fixed in ADR-047. Content is
    unpacked under `$AIOPS_HOME/content/<plugin id>/` (`07:1328`); its digest, recorded in
    `plugins.lock`, enters the generation digest through the locks section that
    `boot/digest.go` already encodes (`08:184`, `:1287`).
20. **Three files, three names.** The site's `dist.yaml` has the four fields of
    `08:401`. The repository's `dist/dist.yaml` becomes the builder's input for the
    release: its module list, both binding maps, the sealed-hook listeners and the
    required set (`08:1220`). The signed distribution manifest is what the builder writes
    from either: the module table, mapping each plugin id to its value and its manifest
    (`07:1344`), with each module's sum and detected licence; the packages the binary
    links, which the fake clause of check 7 reads at boot; the pins; both binding maps;
    the sealed-hook listeners; the required set; the accepted licences; and `serves`
    (`07:1846`). The binary embeds the manifest it was built with. A release publishes
    the signed file for `plugin gate --against` (`08:936`). The release module table is
    generated into `dist/` and committed, CI fails when it differs from the generator's
    output, and `go build ./...` keeps building `cmd/aiops`.
21. **SDK module versions outside the repository.** Inside the repository the three SDK
    modules require each other at `v0.0.0` through `replace` lines, and the root module
    reaches them the same way; that stays, for work inside the repository only.
    - A release gives the three SDK modules one version, the SDK version. A release step
      writes the served copy of each SDK `go.mod`: its `require` lines name the SDK
      version and it holds no `replace` line. A test fails when a served copy differs
      from the repository's file in anything else. Where the modules are served is
      question Q4.
    - The release image, the release `aiopsctl` and the release's signed manifest are
      built by the builder from the served modules, never through `replace`, so the pins
      of the signed manifest name the SDK version, and the strict-version check of the
      builder compares a site's build with the numbers an author resolved (plugin record
      §11). `serves` does not carry the SDK version: Go module versions are packaging
      numbers that no gate reads (`08:902`).
    - The Go environment of every exit environment and of the builder image: `GOPROXY`
      names only the exit's module proxy, with no `direct`; `GOSUMDB=off`; `GOPRIVATE`
      and `GONOPROXY` unset, since both send a module path to version control;
      `GOTOOLCHAIN=local`; `GOFLAGS=-mod=readonly`; and a network that reaches only the
      exit's module proxy and registry. No lookup of the private module path leaves the
      job (`08:1342` L14). `dist build` passes the same settings into the builder image.
22. **The builder** (ADR-044) is a root-module package compiled into `aiopsctl`, run with
    a local toolchain or through the builder image, whose definition lives under `dist/`
    (`08:402`; `08:1201`, assumption 3). It takes the four fields of the site's
    `dist.yaml`, `bindings` included, and any release input in the form of
    `dist/dist.yaml`, so the S3 exit and `dist/contrib.yaml` at S6a need no new builder
    (`08:643`, `:646`). It runs the gate on the manifests of what it builds as decision 17
    says (`08:493`) and does all of `08:402` and `07:1860`:
    - the module table and `go.mod` generated in a scratch directory; `-mod=readonly`,
      cgo off, no `go generate`; strict versions with `--allow-bump <module>`;
    - refusal of a module outside the release that declares an `aiops/` or `contrib/`
      id; refusal of a `fake` or `sdk/testkit` package in the graph of the binary, read
      as question Q22 decides; the `init` deny-list scan;
    - the licence scan of L16 on detected licences, per directory (`08:1335`, `:1344`);
    - the import boundary and the suites on the source, without `-race`, since the
      builder has cgo off;
    - `THIRD_PARTY_LICENSES/`, the aggregated NOTICE texts, the MPL-2.0 source lines of
      L8 and an SBOM in every image it writes; the release images are built the same way
      (`08:1285`).

    `dist sign` signs the manifest of decision 20 with the site's key, and `dist use
    <image>` verifies it and records its signer and digest (`07:1861-1862`). Rewriting a
    compose file or `image.tag` arrives with `deploy/compose` and `deploy/helm`; until
    then `dist use` records the image reference in the site files and prints it.
23. **The CLI** gains `plugin new`, `check`, `smoke`, `pack`, `sign`, `push`, `inspect`,
    `install`, `verify`, `enable`, `disable`, `remove` and `gate --against`, `trust add`,
    `dist build`, `dist sign`, `dist use`, `profile verify` and `profile init`
    (`00:137`; `02:95`; `08:72` row 30, `:366`, `:694`, `:936`), on an operations library
    that `plugin-manager` later shares, under one profile write lock (`08:369`; `03:71`).
    Whether each command is a row of the `cli` profile or code of `cmd/aiopsctl` is
    question Q17; where the operations library lives is question Q5. `purge` needs
    `durable` and is built at S3 (question Q11).
    - `install` runs every check that decision 17 lists for it before it answers
      `rebuild-required`, writes nothing on a denial, and restores a snapshot of the
      profile definition, `plugins.lock` and the profile patch on a failure (`08:374`).
      `enable` writes only a departure from the bundle layers into the profile layer,
      with no `--tenant` (`08:375`); `remove` detaches the artifact's bundle and its lock
      entry. How an enabled row reaches the running process is question Q12.
    - `inspect` fetches only an artifact's manifest and bundle, never its content, module
      or image, and installs and runs nothing, a reading of question Q22.
    - `plugin check` runs the eight steps of `07:1565-1574`. It runs step 5 itself, for
      the definitions the plugin provides or contributes to and, for a pack, the
      registries its rows feed (`07:1571`; question Q20), so the attestation records the
      suites that ran and not those an author's test happens to call (`07:1512`). Step 3
      runs the import analyzer and plugin vet, which move out of `tools/kernelcheck` (a
      `main` package with repository paths) into a package that `tools/kernelcheck` and
      `aiopsctl` share and that works from any module root (`07:1569`). Step 6 runs for
      `tools` and `executors` contributions, and none exists before S5. Step 7 runs the
      gate as decision 17 says, against the release this `aiopsctl` was built from unless
      `--against` names another. Step 8 is question Q13. A manifest that lists `remote`
      is refused until S2b, since step 5 cannot run in a runtime that does not exist.
      The name lint of step 1 covers the marks of `00` §8.1, the reserved owners and
      words, and the forms of T7 for a plugin whose manifest has `integrates`
      (`08:1362`). The check refuses an artifact that carries a dashboard-server
      `plugin.json` (`08:391`), and checks SPDX expressions against list data embedded in
      `aiopsctl`, which gets its own register entry.
    - T1 runs under `-race`, which needs cgo and a C compiler on the author's machine;
      the template README says so (`07:1399`, `:1529`).
    - How suites run inside `aiopsctl` with no Go toolchain, for a pack and for `plugin
      verify`, is question Q10.
    - `plugin smoke ./ --against <release>` builds a throwaway distribution from the
      release's signed manifest and the plugin with the builder image and boots the
      test-only profile (`07:1576`); what that profile holds at S2a and when smoke passes
      is a reading of question Q22. It needs a container runtime (`07:1530`).
    - The exit codes stay the three of ADR-059 decision 29: a denial, a failed step and
      `rebuild-required` exit 1, with the reason on stderr. The usage text and the table
      in `cmd/aiopsctl/run.go` say so.
24. **Templates** live in `templates/`, embedded in `aiopsctl` through a Go package; the
    template files carry names the Go tool does not build (`07:1549`; `08:1218`). S2a
    writes a Go template for each registry that exists, `echoers` and `stampers`, with
    the files of `07:1551-1559`, and the connector-pack template with `runtimes: []`
    (`08:897`, `:1263`). The connector template names no plugin id: `plugin new
    connector` writes rows of an instantiable plugin of the release it runs against
    (`--against`, by default the release this `aiopsctl` was built from), with a config
    taken from the examples of that plugin's `configSchema`. At S2a the template job and
    step P1 run it against the exit's test release, whose one instantiable plugin is the
    toy echoer of question Q2; from S8a the release table holds `tool-mcp` and the job
    runs it against the release too. No shipped binary depends on a test-only id.
    `--contrib` writes the `contrib` owner (`08:407`). The CI template job generates a
    plugin from each template and asserts that it compiles, passes T0 and the generated
    tests, and fails conformance with exactly one line per obligation (`08:1263`;
    `07:1561`).
25. **The handshake message** is frozen as question Q6 decides. Check 3 stays S2b.
26. **`trust.yaml`** has `signers`, each limited to id patterns and runtimes,
    `distributions` and `allowUnsigned` from S2a; `indexes`, `install --index` and
    `requireAttestations` per index arrive at S6a (`08:938`; community:206).
    `requireAttestations` per signer is ADR-020's. Where the verified signer is kept
    beside a digest, and how that record is protected until boot, is fixed in ADR-020
    (`07:75` R17, `:1841`). `boot/` imports neither cosign nor sigstore-go (`08:404`).
    `trust.yaml` is one file per site under `$AIOPS_HOME` (`07:1840`). Whether
    `plugins.lock` is one file per site (`07:1841`) or per profile, as `install
    --profile` and its restore rule suggest (`08:374`, `:395`), is fixed in ADR-047. The
    credential classes these files hold already exist (`00:410-412`).
27. **Shipped bundles and profiles.** S2a adds `bundles/aiops-core/bundle.yaml`, with
    the rows of the S1 core plugins (`clock`, `config-refs`, `restart-policy`, `reload`,
    `readiness`) and any row that question Q12 adds, and `profiles/dev/profile.yaml`
    (`08:1222`, `:1226`); with question Q17 (a) also `bundles/aiops-cli/bundle.yaml` and
    `profiles/cli/profile.yaml`. They are embedded through `dist` into `cmd/aiops` as
    `serve.Distribution.Shipped`, which `cmd/aiops` leaves nil at `491a518`, and into the
    test release; a file of the home wins over a shipped one (`internal/serve`). `install`,
    `enable`, `disable` and `remove` change a profile only under `$AIOPS_HOME`: when the
    home holds no `profiles/<p>/profile.yaml`, they first copy the shipped template there
    with the operation of `aiopsctl profile init <p>` (`08:72` row 30) and say so. `dev`
    lists only bundles that hold a built row: at S2a `aiops-core`, and `aiops-dev` only if
    question Q12 is answered (a) (`08:687`; `00:275`, `:291`). Each later step adds its
    bundle to `dev` with its first row. The S2a and S2b exits install into `dev`
    (`08:687`).
28. **Test distributions pass the same gate.** Once checks 1, 2, 4, 5, 6 and 10 run at
    boot, `test/dist` and the exit's test release pass them as a site distribution does,
    and the gate code is the same in every binary.
    - Each run of the S1 suites, of the `aiopsctl profile render` goldens and of the exit
      generates a throwaway key pair inside the test, signs the test distribution's
      manifest with the code of `dist sign`, writes into the test home a `trust.yaml` that
      lists the key under `distributions`, and records the signer and digest as `dist
      use` does. No key is committed.
    - Every plugin of a test distribution has a generated manifest, and `test/dist/def`
      gains a `definition.yaml`, so checks 1 and 2 read data. Its keys are fixtures of
      the launcher's tests: a CI check fails when a release's `serves` names a definition
      outside `sdk/def`.
    - A toy row is attested by the suites it passes. A fault row or a control row cannot
      honestly attest a passed suite; how it passes check 10 is question Q19.
    - The server-closure check of `kernelcheck imports` fails when a package under
      `test/` or `examples/` is in the closure of `cmd/aiops` or `cmd/aiopsctl` (ADR-059
      decision 9; s1-report §1 row 11, where it was checked by hand).
    - The 41 S1 cells of the failure matrix and the four recomposition cases keep their
      expected outcomes; the cells of the row "Gate or signature denies" gain faults
      (decision 31). The gate part of the boot goldens and of the `profile render`
      goldens changes from "not evaluated" to the evaluated checks, and each is
      regenerated and reviewed in the commit that builds the check.

### What waits, and why

29. The parts below are not built at S2a.

| Part | Step | Why |
|---|---|---|
| Gate checks 3 and 9; `runtimes: [remote]` and `requires.remoteProtocol`; `plugin.FromManifest`; the adapter generator and the `contrib` mark of the toy registries; the Python template | S2b | The remote runtime is S2b (`08:695`; `07:1549`), and a part lands with its first consumer (`08:678`) |
| `Spec.Durable` and the manifest's `durable` entries; `sqlfake`, embedded PostgreSQL and testcontainers-go in `sdk/testkit`; `plugin purge` | S3 | The `durable` and `sql` definitions are S3 (`08:696`; `07:1444`), and `purge` drops namespaces through `durable` (`08:374`) |
| The assertions of the two-tenant test | S4a | Question Q7 |
| The tenant overlay part of check 8; layer 5 | S4b | ADR-059 decision 4; `08:111` |
| The stage vocabulary per definition | S5 | ADR-043 (`00:777`); decision 7 |
| Real signed keys; the effects step of `plugin check`; `sdk/gate` | S5, S7, S8a | Their definitions arrive then (`07:558`, `:1572`; `08:698`) |
| The index; `indexes` in `trust.yaml`; `install --index`; the index clause of check 5 | S6a | `08:938`; `07:1888` |
| Exemptions | S6b | `08:508` |
| `dist use` rewriting compose and Helm files | S7 and later | Those files do not exist yet (decision 22) |
| `tool-mcp`, the connector scenario, the level in a harness's attestation | S8a, S8b | `08:727`; `07:1477` |
| `content`, the row that serves a pack's unpacked content | S8b | `08:728`; until then a pack's own rows read their config only |
| The audit record of each generation's digest and signer set | S5 | `audit-chain` is S5 (`08:403`, `:698`) |

### The exit test, made checkable

30. **The exit test is `test/authoring/`**, run by the CI job `s2a-exit` and by `make
    s2a-exit`. It first builds what a release pipeline would build, with throwaway keys,
    as decision 21 says, and publishes nothing outside the test (ADR-057 decision 5;
    `08:1342` L14). It then drives three environments. None holds a repository checkout,
    a `replace` line or a module cache of the repository, and each one that resolves Go
    modules does so only through the exit's module proxy, under the Go environment of
    decision 21. Questions Q2 and Q4 fix the release they build on and how they reach the
    modules.
    - The author's repository holds a Go toolchain and the `aiopsctl` of the test
      release, and reaches the SDK modules through the proxy (`07:51`).
    - The pack author's machine holds the `aiopsctl` binary alone: no Go toolchain, no
      SDK module, no container runtime, and a network that reaches only the test
      registry (`07:51`, `:1518-1520`; `00` §7.1, leg C1).
    - The clean machine holds `aiopsctl` and a container runtime and no Go toolchain, and
      builds through the builder image (`08:402`). A second job runs the clean machine's
      steps with a local toolchain.

    Each step passes only as stated:
    - **A1.** `aiopsctl plugin new echoers --lang go` in an empty directory writes the
      skeleton; it compiles, and `plugin check` fails with one line per obligation of the
      `echoers` suite.
    - **A2.** With the stubs filled in from the test's fixture, an in-process Go echoer
      (`08:694`; community:59; plugin record §16), `plugin check` passes steps 1 to 5
      and 7, reports the cases of question Q7 that did not run, and reports steps 6 and 8
      as not applicable. Its `--json` report is kept.
    - **A3.** `plugin pack` and `plugin sign` with the author's key write one artifact.
      Its manifest carries the generated `requires`, its bundle holds one disabled row,
      and its attestation names the module's digest and `echoers` at the served version
      with its suite revision and the halves that ran. `plugin push` sends it to the
      test registry.
    - **P1.** On the pack author's machine, `aiopsctl plugin new connector` writes a pack
      that fails conformance per obligation. Filled in from the test's fixture, it passes
      `plugin check`, and `plugin pack`, `sign` and `push` publish it.
    - **C1.** On the clean machine, `trust add` lists each author for its ids and
      runtimes and the site's key under `distributions`; `plugin inspect` prints each
      spec's rows, configs and content paths (`08:373`).
    - **C2.** `plugin install <echoer>` answers `rebuild-required` after every clause that
      decision 17 lists for it passed, prints the `dist.yaml` lines and changes no file.
      `dist build` passes every check of decision 22 and writes the image with
      `THIRD_PARTY_LICENSES/`, the NOTICE texts and an SBOM, and a signed manifest whose
      `serves` holds the `echoers` version. `dist sign` and `dist use` record the site
      as the signer. `plugin install <echoer>` run again appends its bundle with the row
      disabled (`08:397`; `07:1859-1863`).
    - **C3.** `plugin install <pack> --profile dev` writes `plugins.lock` by digest,
      unpacks the content and appends the bundle with every row disabled, with no rebuild
      (`08:395`), into the home copy of `dev` that decision 27 makes.
    - **C4.** `plugin enable` enables the rows (question Q11) and they reach the running
      process (question Q12). The `teststate` row of the test release (question Q2)
      writes the committed state, and the step passes when every row of the composition
      is ACTIVE in it.
    - **C5.** Both are called (question Q1): each echoer answers through the dispatcher
      with the reply its code or its row config fixes.
    - **C6.** The composition holds no policy row and no tenant row (`08:694`).
    - **D1.** A copy of the echoer whose `requires` names an `echoers` minor the release
      does not serve, made without `plugin check` (whose parity step would refuse it) and
      attested and signed by the trusted author so that only the version differs, is
      denied at `plugin gate --against` the release's signed manifest, at `plugin
      install` and at `dist build`. Each denial names check 2 and no other evaluated
      check of decision 17, and no site file changes.
    - **D2.** A copy whose attestation names another subject digest, signed by the
      trusted author, is denied at `plugin gate --against` and at `plugin install`. Each
      denial names check 10 and no other evaluated check, and no site file changes.
      `dist build` does not evaluate check 10 (decision 17), and boot's denials of check
      10 are table cases of decision 31.

    The exit texts of `08:620`, `08:694`, `00:571`, community:59 and plugin record §16
    are edited to one text with the answer to question Q1.
31. **The other refusals and the commands the exit does not reach** are cases of the
    tests of `boot`, `test/failure-matrix` and `test/authoring/`:
    - Every denial clause of checks 1, 2, 4, 5, 6 and 10 and of the S2a parts of checks 7
      and 8, at boot and at a live generation, including the four refusals that leg D
      reuses at S8b (`00:557`; `08:746`); the denial of every Go module when the manifest
      the binary embeds differs from the one `dist use` recorded (decision 33, ADR-020);
      the warning on a `broader` credential and the origin artifact in `--dump`
      (decision 14).
    - The failure matrix: the three cells of the row "Gate or signature denies" (`08:467`)
      that run at S1 gain three faults beside `rogue-provider`: `unserved-version`
      (check 2), `unsigned-row` (check 5) and `foreign-attestation` (check 10), each a
      fault plugin of `test/dist` whose generated manifest, signature or attestation
      carries that one defect. `fault` is already a list whose entries run as separate
      cases, so the schema, the `since` of each cell, `TestMatrixMatchesTable` and the
      table of `08` §1.6 do not change; each fault and its harness procedure land in the
      commit that builds its check, on the signed test distribution of decision 28.
    - `plugin verify` on the artifact of A3 and on the pack of P1 passes; on a copy whose
      fixture was changed after signing it fails and names what differs.
    - `plugin smoke` on the plugin of A2 against the test release passes, in the job
      that has a container runtime.
    - The builder refuses, each in a case that fails on its base: a version bump of a
      pinned module without `--allow-bump`, and the same build passes with it; a module
      outside the release that declares an `aiops/` or a `contrib/` id; a `fake` package
      and an `sdk/testkit` package in the graph; each of the four `init` kinds of the
      deny-list; a licence outside the allow-list, an unknown licence, and a licence not
      in `acceptLicenses`.
    - A build whose `bindings` adds a consumer of a bound key of a test definition passes;
      boot prints the departure and `profile verify` reports it, as the S3 exit needs
      (`08:643`, `:696`). A build whose `acceptLicenses` names a licence outside the
      allow-list records it in the signed manifest and boot prints it; check 6 denies the
      same module in a distribution whose manifest does not record it.

### Working rules

32. **Branch, review and the standing rules.**
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
         provider and a consumer. A definition lint in `tools/` checks the files; that
         each definition under `sdk/def` has at least one provider and one consumer
         among the plugins of the repository's distributions, the test distributions
         included, counting the launcher where `00:212` and the module table make it one
         (it provides the launch keys and consumes `configRefsDryRun`); and the rules of
         `07:514-520` that a static check can decide (the method set, the caller's
         context first, `Inventory()`, no upstream import). `08:676` asks this per
         definition, so a key whose consumer lands later, as `killSwitches` until S8b,
         is not an error. The suites prove `ErrInactive`.
      2. No plugin merges without `aiopsctl plugin check` and a T3 test: `make check` and
         CI run `plugin check` on every plugin under `plugins/` and `examples/`, and a T3
         suite boots each plugin's row.
      3. Nothing outside the closed vocabularies merges into `sdk/kernel`, `boot/` or
         `loader/`, checked as question Q9 decides.
      4. Whoever changes a `v0` definition fixes every plugin under `plugins/` and
         `contrib/` in the same pull request. Until replay exists (S8a), "as far as
         replay can prove" is read as the build, `plugin check` and every suite staying
         green (`08:676`, `:907`; ADR-059:407-409).
33. **What each ADR due at S2a settles**, at least:
    - ADR-020: the signing library of `plugin sign` and `dist sign` (R17); the signature
      format, which cosign in key mode verifies (`07:75`, `:1852`); whether keyless
      signing is offered at S2a; the key format and the id-pattern syntax that `trust
      add` takes; how `trust.yaml` trusts a signer for a `signed` key (`07:558`); the
      attestation type and its subject per kind of artifact (decision 18); where the
      verified signer is recorded, and the attestation of an in-process artifact, and how
      both are protected until boot; how boot ties the module table it runs to the
      signed manifest that `dist use` verified, by the digest of the manifest the binary
      embeds, and the denial when they differ (`07:1861`; community:111, G9); how
      first-party rows pass checks 5 and 10 (question Q14); the transitive licences of
      sigstore-go (`08:1336` L8, `:1341` L13).
    - ADR-044: the builder image, its base layer and its SBOM tool, read before S2a ends
      (`08:1341`, `:1380`), and what the image itself carries: the Go licence and PATENTS
      texts, the licence of each tool in it, an image SBOM and a source offer for the
      copyleft packages of its base layer (`08:402`); the module cache format; when
      `--allow-bump` is acceptable; the content of the `init` deny-list; how the licence
      scan runs the same with a local toolchain; where a self-built distribution lists
      sealed-hook listeners and raises the required set, since `dist.yaml` has four
      fields and W3 allows a raise (`08:401`, `:403`).
    - ADR-047, its S2a part: the OCI layout and media types; the content layer and its
      digest; where `plugins.lock` lives (decision 26); what install does when two
      artifacts of one name under different owners enter one profile, since both would
      mount under `<pack name>` (decision 19); the rows and configs the connector template
      writes (decision 24); and the `$AIOPS_HOME` contract between `aiopsctl` on the host
      and a `kernel` that runs as a container: which paths it mounts (`content/`,
      `plugins.lock`, `trust.yaml`, the profile files, the records of `dist use` and of
      install), read-only or read-write, and their ownership, as the exit's clean machine
      uses it (`02:483`, item 26).
    - ADR-048, its S2a part: every field of manifest schema 1.0, the inner shapes of
      `uses`, `registers`, `hooks`, `requires` and `integrates` (`07:66` R8), where
      `registers` comes from (decision 11), whether `scope` is a list or one value
      (`07:94`, `:128`), the reserved owners, and the strict reader of `plugin check`
      beside the lenient reader of the gate (`07:120`).
    - ADR-054: the register schema, which of U1 to U8 run from S2a (question Q15), the
      exception list and the base provider allow-list.
34. **The licence rule for new dependencies.** A new linked module, baked-in tool, image
    or embedded data set merges only with:
    - its licence read at a primary source at the pinned version and entered in the
      licence record (`08:1341` L13);
    - its transitive licences scanned on the package graph of every shipped artifact that
      links it, `aiopsctl` included (`08:1381`);
    - an entry in the upstream register as ADR-054 sets it (`08:1331` L3);
    - for anything linked, a licence on the in-process allow-list (`08:1335` L7).

    None merges `unverified`. Read before sigstore-go merges: its module graph, which
    holds the protobuf runtime (`google.golang.org/protobuf`), the sigstore protobuf
    specifications and the in-toto attestation module among others; the protobuf runtime
    is read (licence:43), the rest is not. Read before S2a ends: the builder's base layer
    and SBOM tool, the base layer of the server image the builder writes, the OCI client
    (ORAS is read, licence:43; no other is), the SPDX list data, and the registry image
    the exit runs. cosign is pinned as a CI tool with its register entry and is baked
    into no image (`08:1379`). goleak gets its entry (licence:54). The SDK module gains no
    require line and no protobuf dependency (`07:60` R2); the root module gains the
    protobuf runtime through sigstore-go at S2a, which question Q6 weighs.

### Line budgets

35. Physical lines of production Go, measured by `tools/kernelcheck lines` and enforced in
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
| the `plugin` package | new | 2,500, provisional |
| each toy definition package; its fake and its suite are packages of their own | new | 800 each, provisional |
| `cmd/aiopsctl` | 210 (s1-report:75) | 2,500, provisional |
| each command row's plugin, with question Q17 (a) | new | 1,500 each, provisional |
| the operations library | new | 3,500, provisional |
| the builder | new | 3,000, provisional |
| `plugingen` | new | 1,500, provisional |

The coverage gate of 100 per cent stays on `sdk/kernel`, `boot` and `loader` (`08:95`,
row 53), so it covers the new gate checks.

### CI jobs

36. Added to `.github/workflows/ci.yml` (question Q16) and mirrored in the Makefile:
    manifest parity; the definition lint; the `pluginApi` check; the template job;
    `plugin check` on every plugin; the T3 boot of each plugin's row; the vocabulary
    check of `boot/` (question Q9); the handshake freeze test (question Q6); the test of
    the served SDK `go.mod` copies (decision 21); the link guard of decision 28; the
    builder image build; the licence report of `aiopsctl` and of the release image, with
    `THIRD_PARTY_LICENSES/` and the NOTICE and MPL-2.0 lines written for the `aiopsctl`
    binary as for an image (`08:1381`; L8); the register check; a cosign case in both
    directions, cosign verifying an `aiopsctl`-signed artifact and distribution manifest
    and `aiopsctl` verifying cosign-signed ones in key mode (`07:75`, `:1852`;
    `08:1379`); the `s2a-exit` job and its local-toolchain variant, race-built and
    failing on any race report as `s1-suites` does; the caps of decision 35. None pushes,
    publishes or holds a long-lived key.

## Consequences

- The S2a exit is decidable: ten steps in three environments, two denials at each entry
  point that evaluates their check, and the cases of decision 31.
- `boot` may grow by up to 2,827 lines under its provisional cap and stays one package.
  A second package under `boot/`, the alternative, would need an ADR of its own.
- The hook dumps change from stage 0 to the declared stage, and the gate part of the
  dumps from "not evaluated" to the evaluated checks; the goldens are regenerated and
  reviewed in the commit that makes each change.
- The S1 stand-ins (`dist/dist.yaml` in its S1 schema, `modules.yaml`, the hand-written
  volatile lists, the inferred config schema) are removed by the commits that replace
  them.
- The SDK module gains packages and no require line, and may move its root (question
  Q18); `sdk/testkit` gains goleak; the root module gains the dependencies that ADR-020
  and ADR-044 choose, under decision 34.
- The S1 suites boot a signed test distribution with a throwaway key (decision 28).
- With question Q17 (a), `aiopsctl` becomes the `cli` profile and `profile render`
  becomes a command row.
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
- (b) Keep it, through an `aiopsctl` command that a row contributes to `commands`, which
  needs question Q17 (a).
- (c) Follow `00`: drop "calls both" from `08` and the records.
- Recommended: (a). The stricter text stays, with Q2 (a) nothing ships that a release
  would hold, and a call proves that the contribution runs, which a boot alone does not.
  Needed before any S2a code.
- **Answered 2026-11-23: (a)**, decision 37.

**Q2. What release does the exit build on, and where do the toy rows live?** A pack may
insert only rows of first-party instantiable plugins (`07:132`), and none exists at S2a.
The exit also needs the dispatcher, which the plugin record puts on a test-only
profile (plugin record §5), the caller of Q1 and a way to read row states, which a
release-built image does not offer (`<home>/run/status.json` holds no row state, ADR-059
decision 31; `plugin-inventory` is S6a, `08:373`). No release exists.
- (a) A test release: CI builds, with the S2a builder and a throwaway release key, a
  distribution of the release table plus the dispatcher, a toy instantiable echoer that
  contributes to `echoers` and nothing else, and the toy caller, all under `examples/`,
  plus the control row `teststate` of `test/dist`, and the `aiopsctl` of that
  distribution. Their rows enter the exit's `dev` composition through the site's profile
  layer (`08:131`). The shipped `aiops` table gains nothing, the instantiable plugins at
  `0.1.0` stay the three of `07:132`, and the link guard of decision 28 keeps every
  `examples/` and `test/` package out of `cmd/aiops` and `cmd/aiopsctl`.
- (b) The release table holds the toy modules in no shipped bundle, as W8 does for the
  optional upstream plugins, and the toy instantiable echoer leaves the table at S8a.
- (c) The pack half of the exit waits for `tool-mcp` at S8a.
- Recommended: (a). (b) ships toy code in the image and (c) weakens a written exit; (a)
  proves the same mechanism on a release built by the same steps, and `teststate` is
  the observation channel of step C4. Needed before any S2a code.
- **Answered 2026-11-23: (a)**, decision 38.

**Q3. Where do the toy definitions live?** `08:1219`, `00:208` and `07:36` put them under
`examples/` in the root module. `00:206` and `07:28` put every definition under
`sdk/def/<name>/v<N>`, and `07:51` lets a third party import only the SDK module,
`sdk/testkit` and `aiopsctl`, while the exit's echoer imports the `echoers` definition.
- (a) `sdk/def/echoers/v0` and `sdk/def/stampers/v0` in the SDK module, at stability
  `experimental`; `examples/` keeps the toy plugins of Q2. `00:208` and `08` §4 are
  edited.
- (b) `examples/` becomes a nested module that requires only the SDK module, served to
  third parties as the SDK modules are.
- (c) The root module, which a third party then requires with pgx, River and the YAML
  library in its graph, against `07:51`.
- Recommended: (a). It keeps the definition rule of `00` §4.1 and the author's dependency
  list of `07:51`, and adds no module. Needed before any S2a code.
- **Answered 2026-11-23: (a)**, decision 39.

**Q4. How does a repository with no kernel checkout reach the SDK modules and
`aiopsctl`?** The modules are `v0.0.0` behind `replace` lines in a private repository,
and Go ignores the `replace` lines of a dependency: an author module that resolves
`sdk/def` still needs `sdk/kernel v0.0.0`, which no proxy serves, so `go mod tidy` fails.
L14 gates any disclosure (`08:1342`), ADR-057 decision 5 rules out a public release, SDK
preview tags are not planned (community:409), and a lookup of the private module path on
a public proxy or checksum database would be a disclosure. Decision 21 fixes the served
`go.mod` copies and the Go environment.
- (a) The exit job serves the three modules from a file-based module proxy that exists
  only inside the job, at one pre-release version per run that no tag of the repository
  carries, with the `go.mod` copies of decision 21, together with the author's echoer
  module at the version its artifact names. The test release, its signed manifest and
  its `aiopsctl` are built from those served modules, so the release pins and the
  author's build agree, and both environments get that `aiopsctl`. Nothing is tagged or
  published.
- (b) Version tags in the private repository, fetched with repository credentials; the
  tagged `go.mod` files then carry the tagged versions and no `replace` line, and work
  inside the repository moves to a workspace file.
- (c) Wait for the L14 clearance and public tags; the exit cannot run before.
- Recommended: (a). It proves "no kernel checkout" exactly, with no repository, no
  `replace` line, no credential and no lookup outside the job. Needed before any S2a
  code.
- **Answered 2026-11-23: (a)**, decision 40.

**Q5. Where do the operations library and the YAML writers live?** ADR-059 decision 6
allows the one YAML library in `boot/` and `tools/` only. `aiopsctl` must write
`plugins.lock`, `trust.yaml`, the profile's bundle list and the generated half of
`plugin.yaml`, keeping the hand-written half and its comments, and the builder reads the
site's `dist.yaml` and writes the signed manifest. `08:369` has the CLI and
`plugin-manager` share one operations library and one write lock, but `plugin-manager` is
a plugin of `aiops-core` (`00:275`), and a plugin under `plugins/` imports only the SDK
and its own dependencies (`08:1255`), so it cannot import a package of the root module.
- (a) The operations library is the internal package of one first-party plugin that
  provides it under a consumer-bound key of a new definition, with a fake and a suite;
  the command rows of question Q17 (a) consume it from S2a and `plugin-manager` from
  S6a. The YAML library is allowed in that plugin, in `plugingen` and in the builder,
  and never in `sdk/`, `loader/` or any other plugin. `08:369` and `08:1255` both hold.
- (b) The library is a package of the root module under `internal/`, beside `plugingen`
  and the builder, and the YAML library is allowed in the three. `cmd/aiopsctl` calls
  it, as question Q17 (b) needs; `08:369` then holds at S6a only through an amendment of
  `08:1255` that lets `plugin-manager` import it.
- (c) `boot` exports read and write helpers that the CLI calls, and grows further.
- (d) The CLI writes YAML as text without the library, and `boot` reads it; it cannot
  edit a file and keep its comments.
- Recommended: (a) with question Q17 (a), since both written requirements hold; with Q17
  (b), option (b), recorded as needing the amendment of `08:1255` at S6a. `boot` keeps
  every read the gate needs either way. Needed before the first file writer of the CLI
  merges.
- **Answered 2026-11-23: (a), with Q17 (a)**, decision 41.

**Q6. How is the handshake message frozen?** `08:642`, `:892` and `:1212` freeze it at S2a
in its own package, `aiops.remote.handshake` (`07:1120`). `07:60` (R2) fixes where the
remote packages live at S2b with the protocol and keeps the require list of the SDK
module, `07:67` (R9) calls its field names descriptive, and nothing reads it before S2b.
The root module holds the protobuf runtime from S2a anyway, through sigstore-go
(decision 34).
- (a) S2a commits `sdk/remote/handshake/handshake.proto` with the three fields of
  `07:1126-1130` and a test that fails on any change other than an added field. No
  generated Go until S2b. This also fixes the directory `sdk/remote`, which R2 leaves to
  S2b; whether it is a module of its own, and where `sdk/remotehost` lives, stay with
  S2b.
- (b) As (a), plus generated Go in a module of its own at S2a.
- (c) Freeze the text of `07` §5 only.
- Recommended: (a). The wire contract is frozen and tested from S2a, and the module
  choices stay with S2b. Generated Go would have no reader before S2b, and its module
  home is S2b's choice. Needed before S2a closes.
- **Answered 2026-11-23: (a)**, decision 42.

**Q7. What do the two-tenant tests do at S2a?** `plugingen` writes `zz_tenants_test.go`
for every process-scope plugin (`07:1435`; `00:298`), a template must pass the generated
tests (`07:1561`), and both halves of every registry suite carry a two-tenant test and a
disabled-for-tenant test (`07:1463`; `00:299`). These need `tenantsfake.As`, the `tenants`
definition lands at S4a (`08:697`), and S2a involves no tenant data (`08:694`). Community
record G10 says "the solo test kit is complete: `kerneltest`, fakes, generated tests,
`plugin check` with no kernel checkout", proven by the S2a exit and the template job
(community:112).
- (a) `plugingen` writes the file from S2a, and its cases skip with "not yet (S4a)" and
  never count as a pass. Step 4 passes when every case that runs passes, and prints each
  skipped case; the attestation lists the generated tests that did not run (decision
  18), which check 10 does not read and the index shows. The toy suites gain their
  two-tenant and disabled-for-tenant cases at S4a with a raised suite revision (decision
  10), so an S2a attestation names a revision that holds no skipped case. At S4a the
  generated cases assert and `plugin check --write` regenerates the file. G10 is then
  met at the S2a exit except for the two-tenant assertions, and the S2a report records
  that.
- (b) A minimal `tenants` definition and fake arrive at S2a. G10 holds whole, but a
  definition lands before its consumer (`08:678`).
- (c) The file and the suite cases are written only from S4a; G10 and the template's
  "passes the generated tests" are both partly unmet at S2a.
- Recommended: (a). The file and its place in the author's tree are fixed at S2a, the
  skip is visible, as matrix cells of later steps are (ADR-059 decision 3), and no
  definition lands early. It leaves G10 partly unmet until S4a, which the owner accepts
  or refuses here. Needed before `plugingen` merges.
- **Answered 2026-11-23: (a)**, decision 43.

**Q8. Is the S1 code brought to the standing rules?** `sdk/def/reload/v0` has no
`definition.yaml`, only `sdk/def/loader/v0` has a suite, and `clock`, `config-refs`,
`readiness`, `reload` and `restart-policy` use `kernel.Define` with hand-written manifests
in S1 fields (`needs`, not `uses`).
- (a) All of it at S2a: the `reload` definition file, suites on `conformance.Run(t,
  Mount)` for `clock`, `composition`, `configrefs`, `launch` and `reload`, and the five
  core plugins moved to `plugin.Define` with generated manifests and `plugin check`. The
  loader's generated half comes from its `kernel.Plugin` value (decision 13).
- (b) Generated manifests for every plugin of the module table now (check 1 needs them),
  the suites and `plugin.Define` when each is next changed.
- (c) The rules bind new merges only.
- Recommended: (a). The rules say "no definition merges without", and the gate denies a
  row without `requires`; (a) makes S2a's own distribution meet them, at the cost of five
  suites. Needed before gate check 1 merges.
- **Answered 2026-11-23: (a)**, decision 44.

**Q9. What does the vocabulary rule check?** `00:262` lists the kernel's banned words
(tenant, role, profile, row, SQL, HTTP, job, approval, audit, model, health, gate) and
gives `boot/` and `loader/` "a closed vocabulary" with no list.
`tools/kernelcheck/vocabulary.txt` holds twelve words, only two of them (tenant,
approval) from that list, and runs on `sdk/kernel` and `loader/` only. At `491a518` the
kernel uses role, row, model and gate in other senses, and `boot` uses "incident" once.
- (a) The kernel's list gains every `00` §4.3 word the kernel does not use; the four it
  uses in another sense are listed in the S2a report for review; `boot/` gets a list of
  domain words (incident, alert, approval, dashboard, pillar, harness, remediation,
  estate and the upstream names) and its one occurrence is reworded.
- (b) The `00` §4.3 list as written, with the kernel's comments reworded through
  ADR-034.
- (c) The S1 check stays and the rest is reviewed by hand.
- Recommended: (a). It checks every word a grep can decide and records the rest; (b)
  changes the closed kernel for wording. Needed before the first S2a merge into `main`.
- **Answered 2026-11-23: (a)**, decision 45.

**Q10. How do suites run inside `aiopsctl` without a Go toolchain?** A pack author has
only `aiopsctl` (`07:51`); `plugin check` runs the suites of the registries a pack's rows
feed and its scenario (`07:1571`, `:1574`), and `plugin verify` re-runs suites
(`07:1512`), for a pack and for in-process code, whose module must be compiled first.
Suites take a `*testing.T` (ADR-059 decision 32), and `kerneltest.New` needs a
`testing.TB`, which keeps a kernel with both binding maps off out of server binaries
(`07:565`).
- (a) The release build compiles each suite, with the first-party plugins it runs on,
  into a test binary that `aiopsctl` embeds and runs as a child process with the pack as
  input; its JSON output is the report. `plugin verify` of in-process code compiles the
  pulled module with its suites through the builder image or a local toolchain, as
  `plugin smoke` does, and fails with that reason where it has neither.
- (b) `aiopsctl` calls `testing.MainStart`, which the Go 1 compatibility promise does not
  cover, pinned to the toolchain of `go.mod`.
- (c) Suites take an interface that a runner in `aiopsctl` implements, and `kerneltest`
  gains a second constructor for that runner.
- Recommended: (a). It uses only stable Go interfaces and keeps the `testing.TB` fence of
  `kerneltest` whole; (c) weakens that fence. Needed before `plugin check` for a pack
  merges.
- **Answered 2026-11-23: (a)**, decision 46.

**Q11. Which of `enable`, `disable`, `remove` and `purge` are built at S2a?** `00:137`
lists `plugin enable`, `disable`, `remove` and `purge` among the P0 commands of
`aiopsctl`, and `02:95` lists "the `plugin` commands from `new` and `check` to `install`,
`enable` and `purge`" as P0; `08:3` lets `00` win. `08:694` does not list them, and
`08:374-375` date the manager's operations at S6a, which is P1. Every install path of
`08:395-397` ends in `plugin enable`, and `08:369` and `03:71` have the `aiopsctl plugin`
commands work on the profile files from S2a. `purge` drops a plugin's durable namespaces
through `durable` (`08:374`), which is S3.
- (a) `enable`, `disable` and `remove` at S2a on the operations library, `enable` and
  `disable` writing only a departure from the bundle layers into the profile layer with
  no `--tenant`; `purge` at S3 with `durable`. All four stay in P0, so `00:137` holds
  with no edit.
- (b) `enable` and `disable` at S2a; `remove` and `purge` at S3.
- Recommended: (a). The install paths run as written, `remove` is the inverse of an
  install that holds no durable state yet, and S6a adds only the service around the same
  operations. Needed before the exit test is written.
- **Answered 2026-11-23: (a)**, decision 47.

**Q12. How does an enabled row reach the running process at S2a?** Enabling a row whose
code is present is live (`08:190`, `:386-387`), and every operation is a submission to
the reload queue (`08:369`). The documents name the path: "`aiopsctl plugin enable
<row>` | A submission to the reload queue" (`07:1848`); "`aiopsctl` -> Server process |
`plugin enable <row>` submits one generation to the reload queue" (`08:446`);
`reload-watch-files` is "off in server profiles, where `aiopsctl` submits" (`08:240`);
"a change goes through `aiopsctl` or `plugin-manager`" (`03:75`). Before `admin-api`
(S4a) and `plugin-manager` (S6a), nothing carries that submission.
- (a) `reload-watch-files` is built at S2a as a row of `aiops-dev`, the bundle that turns
  it on (`00:291`), and a bound consumer of `reload`, so the exit's enable is applied
  live through the queue. This departs from `07:1848` and `08:446` for every server
  profile, which runs no watcher (`08:240`), and adds `aiops-dev` to `dev` at S2a.
- (b) The files change, and the next start of the same image applies them: no rebuild,
  one restart. It meets the exit text but not the runtime table of `08:190`.
- (c) The documented path: `aiopsctl plugin enable` submits one generation to the
  running process's queue. What is open is the transport before `admin-api` exists; the
  candidate is that the `reload` row, or a row of `aiops-core` beside it that is a bound
  consumer of `reload`, accepts a submission from `aiopsctl` on a Unix socket under
  `<home>/run/` that only the site's user can open. A row beside `reload` is not in the
  list of `00:275`, so that form edits it. The transport, its plugin vet rule and how it
  relates to `admin-api` at S4a are written in an ADR before the exit test.
- Recommended: (c). It is the path `07`, `08` and `03` state, it gives `self-host-full`
  the same live enable as `dev`, and decision 27 then keeps `dev` to `aiops-core` at S2a.
  Needed before the exit test is written.
- **Answered 2026-11-23: (c)**, decision 48.

**Q13. What scenario does a pack attest at S2a?** Check 10 denies a pack whose attestation
names no scenario version (`08:506`), community:182 adds "which halves ran", and step 8 of
`plugin check` runs the connector scenario (`07:1574`), which needs `tool-mcp` and the
incident loop (S8a, S8b).
- (a) S2a defines a scenario as a named, versioned script run inside `aiopsctl`, with its
  halves named. The S2a pack carries the echo scenario, which has one half, the kernel
  half: replayed from the pack's fixtures, each of its rows is called through the
  `echoers` dispatcher. The attestation names the scenario, its version and that half.
  The connector scenario, with its kernel and harness halves (community:302-305), is the
  second, at S8b.
- (b) The pack clause of check 10 and step 8 report "not evaluated until S8b", and the
  S2a pack is admitted without them.
- Recommended: (a). Check 10 stays whole, and the attestation fields and step 8 exist
  from the first pack. Needed before gate check 10 merges.
- **Answered 2026-11-23: (a)**, decision 49.

**Q14. How do the first-party rows of a release pass checks 5 and 10?** Check 5 denies a
Go module absent from a distribution manifest that `dist use` verified and `trust.yaml`
lists (`07:1861`), and check 10 denies a row with no attestation (`08:506`). The release
image runs without `dist use`, a fresh site has no `trust.yaml`, and `boot` verifies no
signature itself (`08:404`). In every option the release pipeline attests each
first-party module and records the attestations in the release's signed manifest.
- (a) A site lists the release key under `distributions` with `aiopsctl trust add` before
  its first boot, and boot refuses with that instruction otherwise. Costs: the start with
  "nothing to build" of `08:398` and `01:292` gains a mandatory step, and every fresh
  compose or Helm start of the shipped image refuses until it is taken; with question
  Q17 (a) `aiopsctl` boots the first-party rows of its own `cli` profile before any
  command runs, so it would refuse `trust add` itself unless its own rows were exempt.
  The S1 suites are unaffected, since they run on their own key (decision 28).
- (b) The release binary carries the release key as a default distribution root and its
  own signed manifest as recorded with that key; boot prints it at every boot, and a
  site removes it in `trust.yaml`. `07:1861` and `08:404` gain that default.
- (c) Checks 5 and 10 apply to modules outside the release table only, which narrows two
  written checks.
- Recommended: (b). Both checks run on every row, the release starts as `08:398` and
  `01:292` state, `aiopsctl` can run `trust add` on a fresh machine, and a site that
  wants only its own roots removes the default; (a) needs an exemption for `aiopsctl` to
  work at all. The answer goes into ADR-020. Needed before gate check 5 merges.
- **Answered 2026-11-23: (b)**, decision 50.

**Q15. Is the upstream register built at S2a?** `08:790` puts the register at S2a (UP-1,
ADR-054), but the 36-row list that holds UP-1 is pending the owner (`08:752`), and every
dependency S2a adds needs an entry (L3, L13).
- (a) S2a writes `tools/upstream-components.yaml` with an entry for every module, image
  and tool that S2a links, ships or bakes in, and runs the register check (U7) on it; U1
  to U3 start when ADR-054 says.
- (b) The register with entries only; every check waits for S8b.
- (c) Nothing until the owner decides the 36-row list.
- Recommended: (a). The licence rule of decision 34 needs the register now, and U7 is
  the check that makes an entry required. Needed before the first new dependency merges.
- **Answered 2026-11-23: (a)**, decision 51.

**Q16. May S2a add CI jobs?** ADR-057 decision 5 says not to add CI/CD configuration or
create workflows. `.github/workflows/ci.yml` exists since 2026-11-18, and ADR-059
decisions 11 and 28, which the owner accepted, name it.
- (a) Confirm that S2a extends the existing `ci.yml` with the jobs of decision 36, none of
  which pushes or publishes, and that ADR-057 decision 5 still rules out any job that
  publishes or changes the repository's visibility.
- (b) No new jobs: the S2a checks run through Makefile targets only.
- Recommended: (a). The standing rules are CI checks (`08:1253`, `:1260`), and (a) records
  the authority the existing file already rests on. Needed before the first S2a merge
  into `main`.
- **Answered 2026-11-23: (a)**, decision 52.

**Q17. Is the S2a CLI built as the `cli` profile?** `00:515` says "`aiopsctl` is a
profile of the same launcher: commands are rows, and a plugin contributes its own
through `commands`"; `00:137`, `00:276`, `02:95`, `08:271`, `08:359`, `08:1206` and plugin
record §2 (Q15) say the same, dsh row 31 ("Apps are profiles, one launcher") is carried,
and `08:687` lists `cli` among the P0 profiles. `08:694` names no `commands` definition at
S2a, S1 built `profile render` as code of `cmd/aiopsctl`, and `08:3` lets `00` win.
- (a) S2a builds the `commands` definition with its fake, suite and `definition.yaml`,
  the `cli` row as its dispatcher, the `aiops-cli` bundle and the `cli` profile. Every
  S2a command, `profile render` included, is a contribution row, and `cmd/aiopsctl`
  becomes the launcher with the module table of the `cli` profile. An ADR proposes the
  `commands` definition, and how command rows reach the operations library (question
  Q5), the builder and the gate, before the first command row merges.
- (b) S2a writes the commands as code of `cmd/aiopsctl` on the operations library, and
  S6a, which builds `plugin-manager`, moves them to rows; `00:515` and `08:271` gain that
  step.
- (c) The commands stay code, and `commands` carries only what plugins contribute. This
  departs from "commands are rows" (`00:515`).
- Recommended: (a). It is the stated architecture, dsh row 31 stays carried, P0 lists
  `cli`, and under (b) every command written as code at S2a is rewritten at S6a. Needed
  before the first CLI command merges.
- **Answered 2026-11-23: (a)**, decision 53.

**Q18. What import path does an author compile against?** `00:262` and plugin record §3
put the packages `sdk/plugin` and `sdk/gate` in the module `sdk/def`; `07:23` gives the
module path `.../sdk/def`, and `07:29-31` the packages `sdk/plugin`, `sdk/gate` and
`sdk/entity`. A Go package belongs to the module whose root directory holds it, so with
the module rooted at `sdk/def/`, as it is at `491a518`, both cannot hold. `08:1201`
(assumption 2) leaves the directories to S2a, and R1 (`07:59`) reads the module as rooted
so that it holds every SDK package except the nested modules.
- (a) Move the module root to `sdk/`, with module path `.../sdk`. The import paths
  `.../sdk/plugin`, `.../sdk/gate`, `.../sdk/entity` and `.../sdk/def/<name>/v<N>` are
  then all as written, and no Go import changes; `sdk/kernel` and `sdk/testkit` stay
  nested modules. The module's name changes in `00:262`, `07:23`, `07:28-33`, `07:51`,
  `08:1199` and plugin record §3, with the `go.mod` require and replace lines. `sdk/remote`
  and `sdk/remotehost` then need a nested module at S2b, since R2 keeps them out of the
  require list.
- (b) Keep the module at `sdk/def/` and put the packages at `sdk/def/plugin`,
  `sdk/def/gate` and `sdk/def/entity`. The import path an author writes changes in
  `00:262`, `07:29-31`, `08:59`, `08:1211` and plugin record §3 and §15 (row 17).
- Recommended: (a). It keeps every import path the documents and the code use and is the
  layout R1 describes; its cost is a module rename while nothing outside the repository
  requires the module. Needed before any S2a code.
- **Answered 2026-11-23: (a)**, decision 54.

**Q19. How does a fault or control row of a test distribution pass check 10?** Decision 28
signs the test distributions and attests their toy rows. The seven fault plugins and the
control rows `testsubmit` and `teststate` provide keys of `test/dist/def` and pass no
suite; a fault plugin such as `panic-apply` fails one by design.
- (a) `test/dist/def` declares `suite: 0`, "no suite". The attestation of a row that
  provides only test keys names that definition at its version with suite revision 0,
  which is true, and check 10 admits it as it admits any attestation that names the
  served revision. No gate clause and no gate code changes. The definition lint keeps
  every definition under `sdk/def` at a suite revision of 1 or more, and decision 28's
  check fails when a release serves a definition outside `sdk/def`, so no shipped
  distribution can use revision 0.
- (b) A seam in `boot.RunOptions` of the test binary, like the `Kernel` seam of ADR-059
  decision 53, admits test rows without check 10; it is nil in shipped binaries. The
  matrix then does not run the shipped gate on those rows.
- (c) The test pipeline writes attestations of suites the fault rows never passed. Listed
  only to be refused.
- Recommended: (a). The gate in the test binary is the shipped gate, and nothing
  attests what did not run. Needed before gate check 10 merges.
- **Answered 2026-11-23: (a)**, decision 55.

**Q20. What does check 10 ask for a definition the plugin only consumes?** `08:506` asks
the attestation to name, "for a definition the plugin requires, the version and the suite
revision that passed"; `07:1893` says "cover a definition version the plugin requires",
and plugin record §12 "uses". `requires.definitions` names every definition a plugin uses,
consumed ones included (`07:139`), but a suite tests a provider, a contribution or a
dispatcher (`07:1463`), and step 5 runs the suites of what the plugin provides or
contributes to, or of the registries a pack's rows feed (`07:1571`). A consumer of
`tenants` has no `tenants` suite to pass.
- (a) The attestation names every definition of `requires` with its version, and the
  suite revision and the halves that ran for each definition the plugin provides,
  contributes to or, for a pack, feeds. Check 10 denies when a version is missing or
  differs from `requires`, or when one of those suite revisions is missing. The one gate
  text of decision 16 words check 10 so, and `08:506`, `07:1893` and plugin record §12
  are edited to it.
- (b) As written: step 5 and the attestation need a suite for a consumed definition,
  which no definition has.
- Recommended: (a). It is what step 5 can run and what an attestation can truthfully
  say. Needed before gate check 10 merges.
- **Answered 2026-11-23: (a)**, decision 56.

**Q21. What references may the bundle of an installed artifact hold before S3?** The
secret scope of an instantiated row is the artifact's, "so one pack cannot read another
pack's credential" (`08:391`; community:298). At S2a `secret:` does not resolve (ADR-059
decision 10), `${env:AIOPS_REF_*}` reaches one global set of names, and `${file:}` reads
anywhere under `--refs-dir` for every row, "rows that an installed artifact inserts
included" (ADR-059 decision 51). A pack's bundle could therefore name another pack's
credential file.
- (a) Check 8 refuses an `${env:}` or `${file:}` reference in the bundle of an installed
  artifact. The operator still sets a credential reference for a pack row in the profile
  or site layer, which the operator writes. `secret:` in the artifact's own scope arrives
  with S3.
- (b) Such a bundle may name only `${file:<owner>/<name>/...}` under `--refs-dir` and
  `${env:}` names with a prefix derived from its plugin id; check 8 refuses the rest.
- (c) No rule until S3; the S2a report records the gap.
- Recommended: (a). It holds the per-artifact scope with the fewest new rules, and the
  operator, who owns the credentials, keeps every choice. It adds a clause to check 8,
  which the one gate text of decision 16 carries. Needed before the installed-artifact
  rule of check 8 merges.
- **Answered 2026-11-23: (a)**, decision 57.

**Q22. Four readings of written text.** Each reading below is what S2a would build, and
each edits the lines it names in the commit that builds it.
1. **Smoke.** `07:1576` says `plugin smoke` "boots the test-only profile". Reading: at
   S2a that profile is the release's `aiops-core` bundle plus the plugin's bundle with
   its rows enabled, and smoke passes when every row is ACTIVE or says what it waits
   for, the boot rule of `dev` (`08:738`). Edits `07:1576`.
2. **Inspect.** `08:373` and `07:1853` say `inspect` shows what a spec names "before
   anything is fetched or run". An OCI spec cannot be shown unread before S6a, when the
   index can describe it. Reading: `inspect` fetches only the manifest and the bundle,
   never content, module or image, and runs nothing. Edits `08:373` and `07:1853`.
3. **`wasm`.** `08:381` and `07:129` say "the gate denies it". Reading: check 1 denies it,
   as a runtime this schema version does not serve, so the denial names one check. Edits
   the check 1 text of the one gate text (decision 16).
4. **"Module graph".** `00:262`, `07:47` and `08:1258` forbid a `fake` or `sdk/testkit`
   package in "the module graph" of a server binary. The SDK module requires
   `sdk/testkit` for its tests, so every module graph that holds the SDK holds
   `sdk/testkit`. Reading: the package graph of the binary (`go list -deps`), which the
   builder and gate check 7 read. Edits `00:262`, `07:47` and `08:1258`.
- (a) Accept the four readings, each edit landing with the code that builds it.
- (b) Answer each item on its own.
- Recommended: (a). Each reading is the only one the code can decide, and reading 4
  edits `00` by one word. Needed before the code each reading names merges.
- **Answered 2026-11-23: (a), all four readings**, decision 58.

## Owner answers of 2026-11-23

The project owner accepted this ADR on 2026-11-23 and answered every question of the
section "Questions for the owner" on the same date, each with its recommended option.
Each decision below names the question it answers. Where a decision above leaves a point
to a question, the decision here settles it.

37. **The exit calls both plugins, through a toy caller row** (Q1 (a)). A toy caller row
    of the exit's release calls each echoer through the dispatcher when a control file
    asks and writes the replies to a file the test reads, as the control rows of ADR-059
    decision 9 do. `00:571` and `01:290` gain "and calls both".

38. **The exit builds on a test release** (Q2 (a)). CI builds, with the S2a builder and a
    throwaway release key, a distribution of the release table plus the dispatcher, a toy
    instantiable echoer that contributes to `echoers` and nothing else, and the toy caller
    of decision 37, all under `examples/`, plus the control row `teststate` of
    `test/dist`, and the `aiopsctl` of that distribution. Their rows enter the exit's
    `dev` composition through the site's profile layer (`08:131`). The shipped `aiops`
    table gains nothing, the instantiable plugins at `0.1.0` stay the three of `07:132`,
    and the link guard of decision 28 keeps every `examples/` and `test/` package out of
    `cmd/aiops` and `cmd/aiopsctl`.

39. **The toy definitions are `sdk/def/echoers/v0` and `sdk/def/stampers/v0`** (Q3 (a)),
    in the SDK module, at stability `experimental`; `examples/` keeps the toy plugins of
    decision 38. `00:208` and `08` §4 are edited.

40. **The exit serves the SDK modules from a module proxy inside the job** (Q4 (a)). The
    exit job serves the three SDK modules from a file-based module proxy that exists only
    inside the job, at one pre-release version per run that no tag of the repository
    carries, with the `go.mod` copies of decision 21, together with the author's echoer
    module at the version its artifact names. The test release, its signed manifest and
    its `aiopsctl` are built from those served modules, so the release pins and the
    author's build agree, and both environments get that `aiopsctl`. Nothing is tagged or
    published.

41. **The operations library is the internal package of one first-party plugin** (Q5 (a),
    with Q17 (a)). That plugin provides it under a consumer-bound key of a new definition,
    with a fake and a suite; the command rows of decision 53 consume it from S2a and
    `plugin-manager` from S6a. Besides `boot/` and `tools/` (decision 6), the YAML library
    is allowed in that plugin, in `plugingen` and in the builder, and never in `sdk/`,
    `loader/` or any other plugin. `08:369` and `08:1255` both hold, and `boot` keeps
    every read the gate needs.

42. **The handshake message is frozen as a proto file** (Q6 (a)). S2a commits
    `sdk/remote/handshake/handshake.proto` with the three fields of `07:1126-1130` and a
    test that fails on any change other than an added field. No generated Go is written
    until S2b. This fixes the directory `sdk/remote`; whether it is a module of its own,
    and where `sdk/remotehost` lives, stay with S2b.

43. **The generated two-tenant tests skip until S4a** (Q7 (a)). `plugingen` writes
    `zz_tenants_test.go` from S2a, and its cases skip with "not yet (S4a)" and never
    count as a pass. Step 4 passes when every case that runs passes, and prints each
    skipped case; the attestation lists the generated tests that did not run (decision
    18), which check 10 does not read and the index shows. The toy suites gain their
    two-tenant and disabled-for-tenant cases at S4a with a raised suite revision
    (decision 10), so an S2a attestation names a revision that holds no skipped case. At
    S4a the generated cases assert and `plugin check --write` regenerates the file. The
    owner accepts that G10 (community:112) is met at the S2a exit except for the
    two-tenant assertions, and the S2a report records that.

44. **The S1 code meets the standing rules at S2a** (Q8 (a)): the `reload` definition
    file; suites on `conformance.Run(t, Mount)` for `clock`, `composition`, `configrefs`,
    `launch` and `reload`; and the five core plugins moved to `plugin.Define` with
    generated manifests and `plugin check`. The loader's generated half comes from its
    `kernel.Plugin` value (decision 13).

45. **The vocabulary rule** (Q9 (a)). The kernel's list gains every `00` §4.3 word the
    kernel does not use; the four it uses in another sense (role, row, model and gate) are
    listed in the S2a report for review. `boot/` gets a list of domain words (incident,
    alert, approval, dashboard, pillar, harness, remediation, estate and the upstream
    names), and the one occurrence of "incident" in `boot` is reworded.

46. **Suites run in `aiopsctl` as embedded test binaries** (Q10 (a)). The release build
    compiles each suite, with the first-party plugins it runs on, into a test binary that
    `aiopsctl` embeds and runs as a child process with the pack as input; its JSON output
    is the report. `plugin verify` of in-process code compiles the pulled module with its
    suites through the builder image or a local toolchain, as `plugin smoke` does, and
    fails with that reason where it has neither.

47. **`enable`, `disable` and `remove` are built at S2a, `purge` at S3** (Q11 (a)). The
    three run on the operations library; `enable` and `disable` write only a departure
    from the bundle layers into the profile layer, with no `--tenant`. `purge` arrives
    with `durable`. All four stay in P0, so `00:137` holds with no edit.

48. **An enabled row reaches the running process through the reload queue** (Q12 (c)).
    `aiopsctl plugin enable` submits one generation to the running process's queue, the
    path of `07:1848`, `08:446`, `08:240` and `03:75`. The transport before `admin-api`
    is open. The candidate is that the `reload` row, or a row of `aiops-core` beside it
    that is a bound consumer of `reload`, accepts a submission from `aiopsctl` on a Unix
    socket under `<home>/run/` that only the site's user can open; a row beside `reload`
    is not in the list of `00:275`, so that form edits it. The transport, its plugin vet
    rule and how it relates to `admin-api` at S4a are written in an ADR before the exit
    test. Decision 27 keeps `dev` to `aiops-core` at S2a.

49. **A pack attests the echo scenario at S2a** (Q13 (a)). A scenario is a named,
    versioned script run inside `aiopsctl`, with its halves named. The S2a pack carries
    the echo scenario, which has one half, the kernel half: replayed from the pack's
    fixtures, each of its rows is called through the `echoers` dispatcher. The
    attestation names the scenario, its version and that half. The connector scenario,
    with its kernel and harness halves (community:302-305), is the second, at S8b.

50. **The release binary carries the release key as a default distribution root** (Q14
    (b)). The release pipeline attests each first-party module and records the
    attestations in the release's signed manifest. The release binary carries the release
    key as a default distribution root and its own signed manifest as recorded with that
    key; boot prints it at every boot, and a site removes it in `trust.yaml`. Checks 5
    and 10 run on every row, and the release starts as `08:398` and `01:292` state.
    `07:1861` and `08:404` gain that default, and the answer goes into ADR-020.

51. **The upstream register starts at S2a** (Q15 (a)). S2a writes
    `tools/upstream-components.yaml` with an entry for every module, image and tool that
    S2a links, ships or bakes in, and runs the register check (U7) on it; U1 to U3 start
    when ADR-054 says.

52. **S2a extends the existing `ci.yml`** (Q16 (a)) with the jobs of decision 36, none of
    which pushes or publishes. ADR-057 decision 5 still rules out any job that publishes
    or changes the repository's visibility.

53. **The S2a CLI is built as the `cli` profile** (Q17 (a)). S2a builds the `commands`
    definition with its fake, suite and `definition.yaml`, the `cli` row as its
    dispatcher, the `aiops-cli` bundle and the `cli` profile. Every S2a command, `profile
    render` included, is a contribution row, and `cmd/aiopsctl` becomes the launcher with
    the module table of the `cli` profile. An ADR proposes the `commands` definition, and
    how command rows reach the operations library (decision 41), the builder and the
    gate, before the first command row merges.

54. **The SDK module is rooted at `sdk/`** (Q18 (a)), with module path
    `github.com/mutex-solutions/aiops-platform/sdk`. The import paths `.../sdk/plugin`,
    `.../sdk/gate`, `.../sdk/entity` and `.../sdk/def/<name>/v<N>` are then all as
    written, and no Go import changes; `sdk/kernel` and `sdk/testkit` stay nested
    modules. `sdk/remote` and `sdk/remotehost` then need a nested module at S2b, since R2
    keeps them out of the require list. The module's name changes in `00:262`, `07:23`,
    `07:28-33`, `07:51`, `08:1199` and plugin record §3, with the `go.mod` require and
    replace lines.

55. **A fault or control row of a test distribution attests suite revision 0** (Q19
    (a)). `test/dist/def` declares `suite: 0`, "no suite". The attestation of a row that
    provides only test keys names that definition at its version with suite revision 0,
    and check 10 admits it as it admits any attestation that names the served revision.
    No gate clause and no gate code changes. The definition lint keeps every definition
    under `sdk/def` at a suite revision of 1 or more, and the check of decision 28 fails
    when a release serves a definition outside `sdk/def`, so no shipped distribution can
    use revision 0.

56. **Check 10 asks a suite revision only where step 5 runs a suite** (Q20 (a)). The
    attestation names every definition of `requires` with its version, and the suite
    revision and the halves that ran for each definition the plugin provides, contributes
    to or, for a pack, feeds. Check 10 denies when a version is missing or differs from
    `requires`, or when one of those suite revisions is missing. The one gate text of
    decision 16 words check 10 so, and `08:506`, `07:1893` and plugin record §12 are
    edited to it.

57. **Check 8 refuses an `${env:}` or `${file:}` reference in the bundle of an installed
    artifact** (Q21 (a)). The operator still sets a credential reference for a pack row
    in the profile or site layer, which the operator writes. `secret:` in the artifact's
    own scope arrives with S3. The clause is part of check 8 in the one gate text of
    decision 16.

58. **The four readings of Q22 are accepted** (Q22 (a)):
    1. **Smoke.** At S2a the test-only profile that `plugin smoke` boots is the release's
       `aiops-core` bundle plus the plugin's bundle with its rows enabled, and smoke
       passes when every row is ACTIVE or says what it waits for, the boot rule of `dev`
       (`08:738`). It edits `07:1576`.
    2. **Inspect.** `inspect` fetches only the manifest and the bundle, never content,
       module or image, and runs nothing. It edits `08:373` and `07:1853`.
    3. **`wasm`.** Check 1 denies it, as a runtime this schema version does not serve,
       so the denial names one check. It edits the check 1 text of the one gate text
       (decision 16).
    4. **"Module graph".** The graph of a server binary in which `00:262`, `07:47` and
       `08:1258` forbid a `fake` or `sdk/testkit` package is the package graph of the
       binary (`go list -deps`), which the builder and gate check 7 read. It edits
       `00:262`, `07:47` and `08:1258`.

    Option (a) placed each edit with the code that builds it. The edits of the four
    readings are made with this record instead, because reading 4 edits `00` and `00` is
    edited in the commit that records the answer (`08:996`); readings 1 to 3 are made
    with it.

**Edits made with this record.** `00` is edited in the commit that records the answer
(`08:996`), and that commit makes the edits the chosen options name: `00:208`, `:262` and
`:571`; `01:290`; `07:23`, `:28-31`, `:47`, `:51`, `:1576`, `:1853`, `:1861` and
`:1893`; `08:373`, `:404`, `:497` (check 1 of `08` §1.6, which decision 16 makes the one
gate text), `:506`, `:1199`, `:1219` and `:1258`; and the plugin record in §4 and §5,
where it names the module that holds `sdk/plugin` and `sdk/gate` (decision 54), and in
§12. The other lines that named the module `sdk/def` are renamed with decision 54:
`07:59-60` (R1 and R2) and `:213`; `08:1201` (assumption 2) and `:1208` (the layout);
line 16 of `docs/architecture/README.md`; and row 5 of plugin record §2. Dated
proposals, audits and reports, and the amendment list of plugin record §17, keep the
name of their date. The lines that placed the toy definitions under `examples/` are
edited to decision 39: `00:571`, `07:19` and `:36`, `08:694`, and the plugin record in
§5 (the catalogue row) and §16. Readings 2 and 4 of decision 58 also edit the lines
that restate them: `inspect` in plugin record §12, and the package graph in `02:207` and
in plugin record §5 ("Modules and import boundary"). The ADR table of `08` §3 lists
this ADR, and the next free number is ADR-061. Left to later commits: the `go.mod` lines
of decision 54, with the code that moves the module root; the one exit text of decision
30; and the one gate text of decision 16, which carries the check 8 clause of decision
57. An option the owner did not choose edits nothing.
