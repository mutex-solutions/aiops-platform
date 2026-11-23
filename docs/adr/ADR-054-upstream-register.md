# ADR-054: The upstream rule: register, checks, exception list and base providers

- Status: **proposed 2026-11-23, awaiting the project owner.** Nothing in this ADR is in
  force. ADR-060 decision 2 has it proposed before the first commit that implements it
  and accepted before that code merges into `main`; each question below names the code
  its answer is needed before.
- Scope: ADR-054 of `00` §9 (`00:788`) and of the open ADR index (`08:1185`), due at S2a
  (`08:694`): the register schema of `tools/upstream-components.yaml`, which of the
  checks U1 to U8 run from S2a and which before the first community release, the
  exception list and the base provider allow-list (ADR-060 decisions 33 and 51). The
  home rule is decided (W8, `00:112`) and is not reopened. This ADR changes no decision
  of `00` by itself: where an answer edits `00`, the question names the lines, and `00`
  is edited in the commit that records the answer (`08:996`).
- Citations: `00`, `03`, `04`, `07` and `08` are the files of `docs/architecture/`
  with those numbers, and `00:267` is line 267 of `00` at commit `8e1f7b9`. "harness"
  and "licence" are `docs/design/harness-and-upstreams-2026-11.md` and
  `licence-audit-2026-11.md`, cited the same way. "Upstreams design" is
  `docs/design/harness-r5/upstreams-as-plugins.md`, a proposal cited by section; the
  harness record wins over it (harness:3). A licence read for this ADR is cited by the
  module, its version and the file read inside it, as `module@version/LICENSE`.

## Context

**What is written.** Principle 9: every upstream, "a process, a library, a service,
content or a contract", enters through one provider or contribution behind a definition
whose fake and suite name no upstream, and nothing in the core names or needs one
(`00:32`; the glossary, `00:708`). `00:267` turns the principle into rules: no upstream
name in a definition, hook, proto, fake or suite, in `sdk/`, `boot/` or `loader/`, in a
dispatcher, a policy row or a row of `aiops-core`, or in a container id, compose service,
chart key, database or metric; the row that wraps an upstream, and a bundle whose rows
all wrap it, carry its name once, as the suffix, when its register entry allows it; the
stated exceptions are contracts, not defaults; a row of `aiops-base` may wrap an upstream
only if it is on the base provider allow-list of the register, "the protocol providers
and `jobs-river`"; and an upstream library is imported only inside the plugin that wraps
it. The register holds one entry per upstream and per content source with the fields
that L3 and the licence record list (`08:1332`; licence:210), and the register check U7
fails on a missing, `unverified` or misplaced entry (`08:1268`; harness:231, where U7
replaces L3). U1 to U4 and U7 run before the first community release, and U5, U6 and U8
wait for the triggers of `00:629` (harness:227-232). U1 to U3 are T0 checks of every
commit (`00:297`; `07:1398`) and run against the register (`07:50`).

**What is decided for S2a.** S2a writes the register with an entry for every module,
image and tool that S2a links, ships or bakes in, and runs U7 on it; U1 to U3 start when
this ADR says (ADR-060 decision 51). A new linked module, baked-in tool, image or embedded
data set merges only with its licence read at a primary source at the pinned version, its
transitive licences scanned, an entry in the register "as ADR-054 sets it" and, if linked,
a licence on the L7 allow-list (ADR-060 decision 34). The register check is a CI job
(ADR-060 decision 36). The SPDX list data embedded in `aiopsctl` and goleak each get an
entry, and cosign is pinned as a CI tool with its entry (ADR-060 decisions 23 and 34).

**What the code holds at `8e1f7b9`.**

- The only third-party module a shipped binary links is `go.yaml.in/yaml/v3@v3.0.6`,
  which `boot/` imports and which `cmd/aiops` and `cmd/aiopsctl` therefore link.
- River at `v0.51.2` and pgx at `v5.11.0` are imported only by the `_test.go` files of the
  S0 probes under `plugins/jobs-river/internal/`, and bring nine modules of other projects
  into the graph of those tests (decision 8).
- `tools/kernelcheck imports` holds the third-party rule by hand: the map
  `pluginThirdParty` grants `plugins/jobs-river` pgx and River
  (`tools/kernelcheck/imports.go:34`), and a list of YAML packages is refused outside
  `boot/` and `tools/` (`:41`, `:118`).
- Cordis is named in 75 files of `sdk/kernel`, its port with the notices L12 requires
  (`08:1341`). The design reference dsh is named in a comment of the definition
  `sdk/def/reload/v0/reload.go:184` and of `loader/apply.go:77`. No other upstream is
  named in `sdk/`, `boot/` or `loader/`.

**Gaps.** Reading the written rule against the code and against ADR-060 shows:

- L3 and the licence record list the same fields, but no key, type, enumeration or
  required rule, and the checks need records that neither lists: where the exception list
  and the allow-list live (U1 reads the allow-list from the register, `00:651`) and what a
  `ship` entry records for its SBOM and source route (the last clause of `08:1332`).
- The modes of licence:210 (`network`, `pull`, `ship`, `link`, `copy`) have none for a
  tool CI runs, while cosign needs an entry (ADR-060 decision 34) and the licence record
  rates it "tool (CI only)" (licence:49).
- U2 fails on a module "imported outside that entry's plugin directories" (harness:228),
  and accepted decisions put libraries in `boot/`, `tools/`, `sdk/testkit` and packages
  that are not plugins (question Q1).
- "One entry per upstream" and "a linked module has no entry" (`08:1332`) leave open
  whether every module of the package graph is an entry, while ADR-060 decision 34 reads
  the chosen module and scans its transitive licences (question Q2).
- The exceptions are listed in `00:267` and restated, shorter, in `00:651` and `08:1365`
  (question Q3).
- `00:267` names the allow-list as "the protocol providers and `jobs-river`" and
  enumerates no row (decision 10).
- The licence record calls the five uses U1 to U5 (licence:17), the names of five checks;
  `08:1306` calls them use 1 to use 5.
- Decision 51 fixes the least the register holds at S2a; `00:651` reads the status of the
  working ids from the register "written at S2a" (question Q4).

## Decision

### The register

1. **One file, read by checks in `tools/`.** The register is
   `tools/upstream-components.yaml` (`00:167`; `08:1332`; harness:223), one YAML file read
   with the one YAML library, which `tools/` may import (ADR-059 decision 6). U1, U2, U3
   and U7 are checks in `tools/` that read it (`08:1245`), run by `make check` and by CI
   (ADR-060 decisions 36 and 52). They read the module graph with `-mod=readonly` from the
   module cache the job has already filled and look up nothing on the network, so no check
   sends the repository's module path anywhere (`08:1343` L14). The file `upstreams.yaml`
   at the repository root of upstreams design §1.5 is not taken: the harness record names
   the file and the schema (harness:223) and wins over the design (harness:3).

2. **Three lists.** The file holds `entries`, one per upstream and per content source
   (`08:1332`; licence:210); `exceptions`, the stated exceptions of `00:267` (decision 9);
   and `baseProviders`, the base provider allow-list (decision 10). The AGPL boundary list
   stays in `00:167`, and U7 compares it with the entries (decision 12).

3. **The keys of an entry.** L3's "role id" is read as two keys, `id` for the upstream and
   `role` for the role it fills, since a library has no container role and a process
   needs both (upstreams design §1.5). U7 fails on an unknown key or a missing required
   one.

   | Key | Holds | Required | Source |
   |---|---|---|---|
   | `id` | The upstream's lowercase technical name, kebab-case, unique in the file | always | `08:1332` |
   | `role` | For a process or a service, its container id of `00` §3.2 or its `ext-` id of §3.3 | form `process` or `service` | `08:1332`; `00:651` |
   | `names` | The tokens U1 looks for: the upstream's project and technical names, lowercase. A format or a protocol the upstream implements is not one of its names; a protocol's name is an item of `exceptions` | always; empty only for form `contract` | `08:1332`; `00:267`, `:651` |
   | `form` | Decision 4 | always | licence:210 |
   | `behind` | The definition key the upstream sits behind, or `none` | always | `08:1332` |
   | `plugins` | The ids of the plugins that wrap it behind `behind` | when one exists | `08:1332`; `00:267` |
   | `bundle` | The bundle of those plugins | with `plugins` | `08:1332` |
   | `modules` | The Go module paths the upstream publishes that a build list of the repository holds | when linked | `08:1332` |
   | `images` | Each image as a reference by digest, with the role key a chart or compose file names it under | when an image is pulled or shipped | `08:1332`; `08:1334` L5 |
   | `pin` | The version, tag, commit or digest in use; for a module, the version the build list selects | when the repository uses it | `08:1332`; ADR-060 decision 34 |
   | `licence` | The SPDX expression as read; `A AND B` where directories of one module carry different licences; `NONE` for a contract whose text the repository neither copies nor links | always | `08:1332`; `08:1336` L7 |
   | `verdict` | Decision 4 | always | `08:1324` |
   | `modes` | Decision 4, as a list | always | licence:210 |
   | `blockers` | Per use, numbered 1 to 5 as `08:1306` numbers them, the open question or condition that blocks it | when a use is blocked | `08:1332`; `08:1306` |
   | `conditions` | Each condition, with the L or T row that enforces it | verdict `ok-with-conditions` | `08:1332` |
   | `evidence` | Each reading: the source (a URL, or `module@version` and the file read), the date read and the version, tag or commit read | unless `unverified` | `08:1332`; ADR-060 decision 34 |
   | `mark` | Decision 4; `needs-approval` while no mark policy of the owner is read | always | licence:231; `08:1357` T1 |
   | `contains` | Nested charts and image layers, each with its pin and licence; and the modules of question Q2 | when one exists | `08:1332` |
   | `default` | `true` when a shipped profile composes it, or when the release image, chart or compose file uses it with no action of the operator | always | `08:1332`; `08:1351` L22 |
   | `sbom`, `source` | Where the SBOM of the artifact that holds it is written, and the route for the notices and the corresponding source of its copyleft parts | mode `ship` | `08:1332`; `08:1334` L5 |
   | `importers` | With question Q1 (a) only: the other directories that may import its modules, each with the accepted decision that put them there | when one exists | question Q1 |

4. **Enumerations.** `form`: `process`, `library`, `service`, `content`, `contract`, `port`
   (licence:210). `verdict`: `ok`, `ok-with-conditions`, `optional-plugin-only`,
   `replace-or-rebuild`, `unverified` (`08:1324`). `mark`: `suffix-ok`, `needs-approval`,
   `never` (licence:210, :231). `modes`: the five of licence:210, `network`, `pull`,
   `ship`, `link` and `copy`, and two the licence record already uses: `tool`, for a
   program that CI, a test or the lab runs, fetched by pin and never shipped (licence:49,
   :53; `08:1369`), which cosign's entry needs (ADR-060 decision 34); and `none`, for an
   item excluded by name that no artifact holds (licence:42, :47; `08:1338` L9), so that
   U7 can refuse it by name. A value outside these lists fails U7, and a value is added by
   an amendment of this ADR.

5. **What "has an entry" means.** A Go module has an entry when its path is in the
   `modules` of one, or, with question Q2 (b), in the `contains` of the entry whose module
   brings it in. An image has one when its reference is in an entry's `images` or in the
   `contains` of the image that holds it as a layer. A tool baked into an image has an
   entry with mode `ship` whose `pin` is the tool's version; a tool CI runs, one with mode
   `tool`. A content set has one when an entry of form `content` names its directory or
   embedded file. The Go standard library belongs to the entry of the Go toolchain and has
   no module path, so U2 does not reach it; `00:262` already has the kernel import it and
   nothing else.

6. **A pin moves with its reading.** U7 fails when the version that the build list of a
   shipped artifact selects for a module differs from the `pin` of its entry or from its
   version in `contains`. A version bump therefore reads the licence again in the same
   commit, as a new module is read (ADR-060 decision 34; licence:44, "the pin changes only
   by a reviewed bump"; `08:1369`, "re-read when a pin moves").

7. **The register follows the licence record.** The licence record supplies the verdicts
   (`00:167`). The commit that adds an entry or changes its verdict, licence or mark
   changes the row of the licence record for the same upstream, and the two agree (ADR-060
   decision 34, first item). The record is prose, so review holds them equal; the register
   is the copy the checks read.

8. **The entries at S2a.** Whatever question Q4 decides, the register holds at S2a:
   - the Go toolchain and its standard library, linked into every binary and baked into
     the builder image (ADR-044);
   - the Cordis port, form `port`, in `sdk/kernel` and `loader/` and so in every binary,
     with the facts of licence:35 and the notices of `sdk/kernel/NOTICE` (`08:1341` L12),
     and with the `names` `cordis`, `deepseek` and `dsh`;
   - the YAML library, which `boot/` links into both shipped binaries;
   - River and pgx, which U2 places (decision 13);
   - each module, image, tool and data set S2a adds, entered with its licence read when it
     merges (ADR-060 decision 34): the signature library and its graph (ADR-020 chooses
     it; `08:1151` names sigstore-go), in which the protobuf runtime is read (licence:43)
     and the sigstore protobuf specifications and the in-toto attestation module are not;
     goleak in `sdk/testkit` (ADR-060 decision 12); the OCI client; the SPDX list data
     (ADR-060 decision 23); the base layer, licence scanner and SBOM tool of the builder
     image and the base layer of the server image the builder writes (ADR-044); cosign and
     the registry image the exit runs, with mode `tool` (ADR-060 decision 34).

   The third-party modules in the package graph of the repository at `8e1f7b9`, tests
   included, read on 2026-11-23 from each module's own files at the version the build
   list selects:

   | Upstream | Modules | Licence as read | Used at `8e1f7b9` | Licence record |
   |---|---|---|---|---|
   | Go toolchain and standard library | go1.26.8, as `golang.org/toolchain@v0.0.1-go1.26.8.linux-amd64` | BSD-3-Clause in `LICENSE`, with a patent grant in `PATENTS` | Every binary | licence:43, :50 |
   | YAML library | `go.yaml.in/yaml/v3@v3.0.6` | `Apache-2.0 AND MIT`: its `LICENSE` puts eight files ported from libyaml under MIT and the rest under Apache-2.0, and it carries a `NOTICE` | `boot/`; linked into `aiops` and `aiopsctl` | licence:44 |
   | River | `github.com/riverqueue/river`, `.../river/riverdriver`, `.../river/riverdriver/riverpgxv5`, `.../river/rivershared` and `.../river/rivertype`, each `@v0.51.2` | `MPL-2.0 AND MIT`: MPL-2.0 in the `LICENSE` of each of the five; MIT in `rivershared@v0.51.2/levenshtein/License.txt` | The `_test.go` files of the S0 probes under `plugins/jobs-river/internal/`; no binary | licence:41 |
   | pgx | `github.com/jackc/pgx/v5@v5.11.0` | MIT in `LICENSE` | As River | licence:43 |
   | Brought in by River | `github.com/tidwall/gjson@v1.20.0`, `.../match@v1.2.0`, `.../pretty@v1.2.1`, `.../sjson@v1.2.5` | MIT in the `LICENSE` of each | Through River | no row |
   | Brought in by pgx and River | `github.com/jackc/pgpassfile@v1.0.0`, `.../pgservicefile@v0.0.0-20240606120523-5a60cdf6a761`, `.../puddle/v2@v2.2.3`; `golang.org/x/sync@v0.24.0`, `golang.org/x/text@v0.43.0` | MIT in the `LICENSE` of each of the first three; BSD-3-Clause in `LICENSE`, with a patent grant in `PATENTS`, for the two others | Through pgx and River | no row |

### The exception list

9. **The exception list is the list of `00:267`**, recorded under `exceptions` with, for
   each item, its tokens and the places where they may appear. Which items reach ids is
   question Q3. The list lives in `00`, so an item is added or removed by an ADR that edits
   `00:267`.

   | Item | Tokens | Where they may appear | Source |
   |---|---|---|---|
   | The PostgreSQL dialect behind `sql`, `sql.Bound`, `platformSql` and `sqlDriver` | `postgres`, `postgresql`, `pgx` | Those definitions and their suites; the `sql` fake, which is the same engine in a throwaway schema; an import of pgx v5 in `sdk/def/sql`; the suffix of a row on the allow-list that wraps it | `00:26`, `:106` (W2), `:214`, `:262`, `:267`, `:651`; `07:28`, `:50` |
   | The two query dialect keys, `metricsQuery` and `logsQuery` | The names of the two dialects and of the query APIs they belong to | Those two definitions, their fakes and their suites | `00:239`, `:267` |
   | The protocols with several implementations: the S3 API, the OpenAI-compatible API, OIDC, SMTP, OCI, OTLP, the Kubernetes API and MCP | `s3`, `openai`, `oidc`, `smtp`, `oci`, `otlp`, `kubernetes`, `k8s`, `mcp` | The definitions, fakes and suites that declare the protocol; ids, as question Q3 decides | `00:267`, `:651`; `08:1365` T9 |
   | The Cordis semantics of the kernel port | `cordis`, `deepseek`, `dsh` | `sdk/kernel/`, `loader/` and `test/differential/`, their notices included | `00:267`; `08:1341` L12 |
   | The default stack bundle (`aiops-stack`, `agent-base` and the agent bundles) | The names of the default stack's upstreams | The code and content of the plugins of that bundle and their config in the golden dumps, as the wire names, syntax and file paths those upstreams define; never an id, where the marks of `00:651` and T1 hold | `00:267`, `:651`; `04:9`, `:14`; harness:211; upstreams design §1.1 |

### The base provider allow-list

10. **The allow-list is the eight rows of `aiops-base` that wrap an upstream** (`00:277`),
    each the provider of a contract of decision 9 or `jobs-river`, as `00:267` and `03:93`
    describe it and upstreams design §1.1 enumerates it:

    | Rows | Wrap |
    |---|---|
    | `sql-postgres`, `sql-platform`, `kv-postgres` | The PostgreSQL dialect |
    | `blob-s3` | The S3 API |
    | `llm-provider-openai-compatible` | The OpenAI-compatible API |
    | `identity-oidc` | OIDC |
    | `notifier-email` | SMTP |
    | `jobs-river` | River, a library behind `jobs` |

    `baseProviders` lists the row ids. A row joins the list only while the entry whose
    `plugins` hold its plugin has the verdict `ok` or `ok-with-conditions` (harness:217);
    when that verdict is lost, the trigger of U5 fires (`00:629`). Any other row of
    `aiops-base` names no upstream and imports no module of an entry (U1, U2). A row that
    would need one, for example a JWT and JWKS library, which L13 lists as not chosen yet
    (`08:1342`), uses the standard library or joins the list by an ADR that edits `00:267`
    before it merges. `tool-mcp` is in the module table with no row in `aiops-base`
    (`00:277`) and is not on the list. The list is data in the register from S2a; the
    clauses of U1, U2 and U7 that read it have no case before the first row of `aiops-base`
    merges (S3) and land with it (`08:678`).

### The checks

11. **When each check runs.**

    | Check | Fails when | Runs | From | Source |
    |---|---|---|---|---|
    | U1 name lint | Decision 14 | T0: every commit, `make check` and CI | S2a | harness:227; `00:297`, `:651` |
    | U2 import boundary | Decision 13 | T0 | S2a, once question Q1 is answered | harness:228; `08:1266` |
    | U3 suite neutrality | Decision 15 | T0 | S2a | harness:229; `07:520` |
    | U4 leave-one-out render | A shipped profile without one upstream bundle it lists fails to render, loses a required key, or leaves a row PENDING that its golden file does not list | T3, every pull request | With the first shipped profile that lists an upstream bundle, and before `0.1.0` in any case | harness:230; `08:281`, `:790`, `:1267`; `00:561` |
    | U5 minimal distribution, U6 required-set lint, U8 deployment lint | Upstreams design §1.6 | Where upstreams design §1.6 places each | On the triggers of `00:629`; none is a gate of `0.1.0` unless its trigger fires. Until U8, U1 covers the role names of chart and compose files and review the rest (`08:1269`) | harness:232; `08:869` |
    | U7 register and verdicts | Decision 12 | CI on every pull request, as the register check of ADR-060 decision 36, and at every release | S2a | ADR-060 decision 51; `08:1268`, `:1332` |

    At S2a no shipped profile lists an upstream bundle: `dev` lists `aiops-core` (ADR-060
    decision 27), and an optional bundle is listed by no shipped profile (W8). U4 therefore
    has no case at S2a, and the release test of upstream independence stays leg F
    (`00:561`; `08:748`).

12. **U7 from S2a.** The register check runs from the commit that writes the register. It
    fails when:
    - an entry breaks decision 3 or 4;
    - a module in the package graph of a shipped binary has no entry (decision 5). At S2a
      these are `aiops` and `aiopsctl` with the test binaries `aiopsctl` embeds (ADR-060
      decision 46; licence:54), and the build list of each image the builder writes, which
      its signed manifest records (ADR-060 decision 20);
    - a base layer or tool that an image the repository builds bakes in has no entry with
      mode `ship`: at S2a the builder image and the server image, whose definitions name
      each by digest or version in the form ADR-044 fixes (ADR-060 decision 22);
    - a data set a shipped artifact embeds has no entry: at S2a the SPDX list data;
    - a pin differs from the build list (decision 6);
    - an entry is `unverified` (`08:1332`);
    - an entry with `default: true` is not `ok` or `ok-with-conditions`;
    - a `replace-or-rebuild` entry appears in any `dist/*.yaml`;
    - an entry with an AGPL licence has `modules`; an Apache-2.0 sub-tree of an AGPL project
      is an entry of its own (`08:1331` L2);
    - a `ship` entry has no `sbom` or `source`;
    - an entry with an AGPL licence and form `process` has a `role` that the AGPL boundary
      list of `00:167` does not name, the clause of `00:167` and `08:1332`, which a test
      reads from that paragraph;
    - a row on the allow-list is in the `plugins` of no entry whose verdict is `ok` or
      `ok-with-conditions` (decision 10, from S3).

    Deployed images, charts, nested charts and shipped content directories enter with their
    first file (S7 and later), and so do the cases of their clauses. Whether every tool CI
    runs has an entry is review, as `08:1390` makes the table of `08` §5.3, the register
    and ADR-027 the licence control of tools that never ship.

13. **U2 from S2a.** U2 fails when a Go file of the repository, tests included, imports a
    package of a module that is in no entry's `modules`, or of a module of an entry outside
    the directories of that entry's `plugins` and the places question Q1 allows, the pgx
    import of decision 9 apart; and when the require lists of the SDK modules leave what
    `00:262` and ADR-060 decision 4 allow: none for `sdk/kernel`, and for `sdk` only
    `sdk/kernel`, pgx v5 from S3 and `sdk/testkit` for its tests. It replaces the
    third-party part of `tools/kernelcheck imports`: the map `pluginThirdParty` and the
    YAML rule become reads of the register, and the rules on the SDK, the definitions,
    `boot/` and `loader/` stay. At `8e1f7b9` River and pgx are imported only under
    `plugins/jobs-river/` (harness:219), and the YAML library only in `boot/`, which
    question Q1 decides.

14. **U1 from S2a, over the places that exist.** U1 fails when a token of an entry's
    `names` appears, case-insensitively, as a whole word, a kebab-case segment or a
    camel-case part:
    - in a file or a path under `sdk/`, `boot/` or `loader/`, which covers every
      definition, `definition.yaml`, hook, fake and suite (`00:267`, `:651`);
    - in the id or schema of a dispatcher, a policy row or a row of `aiops-core`, and in the
      id of a row of a bundle that a shipped profile lists (`00:651`);
    - in a golden dump of a shipped profile (`00:651`; `08:1266`);

    outside the items of decision 9 and the rows of decision 10; and when an id carries a
    token other than once, as the suffix of a plugin in that entry's `plugins` or of that
    plugin's own bundle, while the entry's `mark` is `suffix-ok` (`00:267`, `:651`;
    licence:231). At S2a its places are the SDK tree, `boot/`, `loader/`, the rows of
    `aiops-core` and `aiops-cli` and the dumps of `dev` and `cli` (ADR-060 decisions 27
    and 53). Container ids, compose services, chart and image keys, databases, metrics,
    rows of `aiops-base` and `test/acceptance/` enter U1 in the commit that creates the
    first of each (`08:678`). At `8e1f7b9` U1 finds one name: the comment at
    `sdk/def/reload/v0/reload.go:184` names dsh in a definition, outside the places of the
    port item, and is reworded in the commit that adds U1. The mentions of Cordis and dsh in
    `sdk/kernel/` and `loader/` stand under the port item of decision 9.

15. **U3 from S2a.** U3 fails when a file under the `fake/` or `conformance/` directory of
    a definition (`07:28`) imports a package of a module of an entry or holds a token of
    one, outside the PostgreSQL item of decision 9 for the `sql` fake and suite, and when
    a recorded cassette of an upstream sits under `sdk/` (`07:520`, `:1479`;
    harness:229). The definitions of S2a and the S1 suites of ADR-060 decision 44 are its
    first cases.

### Working rule

16. **A dependency lands in one commit with its entry.** The commit that adds a module to
    a `go.mod`, a base layer or tool to an image definition, a tool to CI or a data set to
    an artifact adds or extends its entry with the reading at the pinned version and the
    row of the licence record (decision 7) and, for a module imported outside the plugin
    that wraps it, the decision that places it (question Q1). U2 and U7 fail the commit
    otherwise, so the rule of ADR-060 decision 34 is checked, not reviewed.

## Consequences

- S2a gains the register with the entries question Q4 decides, U7 as the register check of
  CI, and U1 to U3 at T0, all in `tools/`. With the recommended answers no package of
  `sdk/kernel`, `boot/` or `loader/` changes and no line budget moves; one comment of
  `sdk/def/reload/v0` is reworded (decision 14).
- The hand-kept third-party rules of `tools/kernelcheck imports` give way to the register
  (decision 13).
- Every version bump of a linked module reads its licence again in the same commit
  (decision 6).
- With question Q1 (a), `00:267`, `07:37`, `07:50` and harness:210 gain the importers rule;
  with question Q3 (a), `00:651` and `08:1365` cite the list of `00:267` instead of
  restating it. Each edit lands in the commit that records the answer (`08:996`).
- U4 starts with the first shipped profile that lists an upstream bundle; U5, U6 and U8
  keep their triggers (`00:629`). Leg F stays the release test of upstream independence.
- This ADR settles the S2a part of UP-1, the register (`08:790`). The bundle moves and the
  role ids of UP-1 stay at S8b, and the 36-row list that holds UP-1 stays pending the owner
  (`08:752`).
- On acceptance the rows of `00:788` and `08:1185` record ADR-054 as decided.

## Questions for the owner

Each question gives its options, the recommended one, why, and when the answer is needed.

**Q1. Where may an upstream library be imported outside the plugin that wraps it?**
`00:267` imports an upstream library "only inside the plugin that wraps it"; `07:50` and
harness:210 allow no upstream module in `sdk/`, `boot/`, `loader/`, a dispatcher, a policy
row or a row of `aiops-core`, with pgx in `sdk/def/sql` the one stated exception; U2 fails
on a module "imported outside that entry's plugin directories" (harness:228). The glossary
counts a library as an upstream (`00:708`). Accepted decisions place libraries elsewhere:
the YAML library in `boot/` and `tools/` (ADR-059 decision 6) and in the operations plugin,
`plugingen` and the builder (ADR-060 decision 41); goleak in `sdk/testkit` (ADR-060
decision 12), which `07:33` lets import "anything"; and the signature library "in
`aiopsctl`" and on the `plugin-manager` side (`08:1151`), a plugin of `aiops-core`
(`00:275`).
- (a) An entry may list `importers`: directories outside its plugins where its modules may
  be imported, each with the accepted decision that put them there. At S2a: the YAML
  library in `boot/` and `tools/` (ADR-059 decision 6) and in the operations plugin,
  `plugingen` and the builder (ADR-060 decision 41); goleak in `sdk/testkit` (ADR-060
  decision 12); the signature library where ADR-020 puts it. U2 fails on any other import,
  and a new item outside `plugins/` needs an accepted ADR. `sdk/kernel`, `loader/` and the
  definitions with their fakes and suites never appear, the pgx import of W2 apart.
  `00:267`, `07:37`, `07:50` and harness:210 gain "or where its register entry lists it
  by an accepted decision".
- (b) The rule as written: a library sits only in the plugin that wraps it behind a
  definition. ADR-059 decision 6 and ADR-060 decisions 6, 12, 14 and 41 are reopened:
  `boot` reads its YAML inputs without the library, since it imports no domain definition
  (`00:262`), and goleak leaves `sdk/testkit`.
- (c) U2 covers only the plugin trees (`plugins/`, `contrib/`, `examples/`, `templates/`),
  and the hand rules of `tools/kernelcheck` keep the rest, so the written rule stays
  unchecked in `boot/`, `tools/` and `sdk/testkit`.
- Recommended: (a). Every placement the owner accepted stays and is checked, U2 still fails
  on a placement no ADR made, and no code moves. (b) undoes accepted work for a rule those
  decisions already departed from; (c) leaves part of the rule unchecked. Needed before U2
  merges.

**Q2. Is every module of the package graph an entry of its own?** L3 fails the release
when a "linked module ... has no entry", and the register holds "one entry per upstream"
(`08:1332`; licence:210). ADR-060 decision 34 reads a new module's licence at a primary
source and scans "its transitive licences". River at `v0.51.2` brings four modules of
other projects into the graph, and its module `rivershared` carries an MIT directory
(decision 8); sigstore-go brings the protobuf runtime, the sigstore protobuf
specifications and the in-toto attestation module among others (ADR-060 decision 34).
- (a) One entry per module, each with every key of decision 3 and a licence read by hand
  at its pin.
- (b) An entry's `modules` holds the modules its upstream publishes; each module of another
  project that they bring into the package graph is listed in that entry's `contains`, with
  its version and the licence the scan detects, and a module that a package of the
  repository imports has an entry of its own. U7 fails when a module of the graph is in
  neither or its version moved (decision 6). A contained module whose detected licence is
  unknown or outside L7 is read by hand at its pin and the reading recorded in `evidence`.
- (c) Only the modules the repository requires directly have entries; the rest is left to
  the licence scan of L7 and L16.
- Recommended: (b). Every linked module has an entry, as L3 asks, and the work splits as
  decision 34 splits it: the chosen module is read and its graph scanned. (a) multiplies
  entries that no check reads more closely than (b); (c) drops L3's clause for every
  transitive module. Needed before U7 merges.

**Q3. Which list of exceptions governs ids?** `00:267` lists the stated exceptions: the
PostgreSQL dialect, the two query dialect keys, eight protocols (the S3 API, the
OpenAI-compatible API, OIDC, SMTP, OCI, OTLP, the Kubernetes API, MCP), the Cordis
semantics of the kernel and the default stack bundle. `00:651`, the rule for ids, opens
"outside the stated exceptions of §4.3" and restates them as six contract names, without
SMTP, OCI and OTLP; T9 restates the same six (`08:1365`). The row
`fleet-template-otlp-receiver` of `agent-base` and its feature key `otlpReceiver` carry
`otlp` (`04:191`; `00:281`).
- (a) The list of `00:267` governs every place, ids included, as decision 9 records it;
  `00:651` and `08:1365` cite it instead of restating it.
- (b) The six names of `00:651` govern ids; the row and the feature key that carry `otlp`
  take role names, and `04:191` and `00:281` are edited.
- Recommended: (a). Both restatements open with "outside the stated exceptions of §4.3",
  so their parentheses read as restatements, and (b) renames for a list that no decision
  chose to shorten. Needed before U1 merges, which then reads one list.

**Q4. Which entries does the register hold at S2a?** ADR-060 decision 51 asks for every
module, image and tool that S2a links, ships or bakes in. `00:651` reads the `suffix-ok`
status of the working ids from the register "written at S2a", and U1 knows only the names
of the entries it reads. A part lands with its first consumer (`08:678`). L3 fails the
release on an `unverified` entry, and L13 says such an entry "blocks the release that
first ships it" (`08:1332`, `:1342`).
- (a) At S2a the entries of decision 8, and the items excluded by name in L9 and L15
  (`08:1338`, `:1344`) with mode `none`, which the clause of U7 on `dist/*.yaml` reads.
  Every other upstream enters with the commit that first uses, wraps or names it, and U1
  learns its names then. Every entry is in use or refused by name, so L3's clause on
  `unverified` stands as written.
- (b) As (a), and one entry per upstream of licence record §2 and of `00` §3.2, with the
  verdict and evidence the record holds, so U1 knows every name from its first run. An
  unused entry may be `unverified`, and L3's clause is read as L13: U7 fails on an
  `unverified` entry only where the artifact under check links, ships or bakes it in.
- (c) Only decision 51's minimum; River and pgx stay on the hand list of `tools/kernelcheck`
  until S3, and U2 reads the register from S3.
- Recommended: (a). It meets decision 51 and the rule that a part lands with its first
  consumer, keeps L3 as written, and U1 holds for each upstream from the commit that first
  brings it, which is before the first community release (`00:267`). (b) transcribes an
  entry for every row of the licence record and narrows L3; (c) keeps two lists of one rule
  until S3. Needed before the register merges, which is before the first new dependency of
  S2a (ADR-060 Q15).
