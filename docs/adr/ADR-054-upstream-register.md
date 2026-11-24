# ADR-054: The upstream rule: register, checks, exception list and base providers

- Status: **accepted 2026-11-23 by the project owner.** The owner answered all nine
  questions on the same date with the recommended option of each; the section "Owner
  answers of 2026-11-23" records them as decisions 17 to 25.
- Scope: ADR-054 of `00` §9 (`00:788`) and of the open ADR index (`08:1185`), due at S2a
  (`08:694`): the register schema of `tools/upstream-components.yaml`, which of the
  checks U1 to U8 run from S2a and which before the first community release, the
  exception list and the base provider allow-list (ADR-060 decisions 33 and 51). The
  home rule is decided (W8, `00:112`) and is not reopened. This ADR changes no decision
  of `00` by itself: where an answer edits `00`, the question names the lines, and `00`
  is edited in the commit that records the answer (`08:996`).
- Citations: `00`, `01`, `03`, `04`, `05`, `06`, `07`, `08` and `09` are the files of
  `docs/architecture/` with those numbers, `README` is `docs/architecture/README.md`,
  and `00:267` is line 267 of `00` at commit `8e1f7b9`. "harness", "licence" and
  "s0-pins" are `docs/design/harness-and-upstreams-2026-11.md`,
  `licence-audit-2026-11.md` and `s0-reference-pins.md`, cited the same way; `ci.yml`,
  `restart-full.yml` and `property-full.yml` are the files of `.github/workflows/`.
  "Upstreams design" is `docs/design/harness-r5/upstreams-as-plugins.md`, a proposal
  cited by section; the harness record wins over it (harness:3). A licence read for this
  ADR is cited by the module, its version and the file read inside it, as
  `module@version/LICENSE`; a file of the Go toolchain is cited by its path inside
  `golang.org/toolchain@v0.0.1-go1.26.8.linux-amd64`.

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
it. Harness record 7.2 also writes decision sentences, credential classes, saga stages
and acceptance scripts by role, an upstream appearing only after "default:" or in the
Technology and Rationale columns (harness:213; `README:115`). The register holds one
entry per upstream and per content source with the fields that L3 and the licence record
list (`08:1332`; licence:210). The register check U7 fails when a deployed image, linked
module, packaged or nested chart, baked-in tool or shipped content directory has no
entry, and on an `unverified` entry (`08:1268`; harness:231, where U7 replaces L3). U1
fails where 7.2 forbids a name (harness:227). U1 to U4 and U7 run before the first
community release, and U5, U6 and U8 wait for the triggers of `00:629` (harness:227-232).
U1 to U3 are T0 checks of every commit (`00:297`; `07:1398`) and run against the register
(`07:50`).

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
- Cordis is named in 75 files of `sdk/kernel`, whose `NOTICE` and the root `NOTICE` carry
  both MIT notices of L12 (`08:1341`). L12 makes `loader/` a port as well, but `loader/`
  holds no `NOTICE` file and none of its files carries either notice, so L12's condition
  is not met there. The design
  reference dsh is named in a comment of the definition `sdk/def/reload/v0/reload.go:184`
  and of `loader/apply.go:77`.
- Two other upstreams are named in those trees: Prometheus, in the endpoint
  `https://prometheus.example.internal` at line 3 of the demo profile's `aiops.patch.yml`
  in the test home of `boot/testdata/basic` and at `boot/testdata/basic.golden:10`; and
  the Kubernetes API, whose token `k8s` is a label path in
  `sdk/def/loader/v0/volatile_test.go:67`, `:69` and `:70`.
- The project names of two upstreams are words the repository's own files carry: `go`
  appears as a word in 79 files of `sdk/`, `boot/` and `loader/` and ends every Go file
  path, and the licence header of L1 (`08:1330`) puts `SPDX` in 121 files of those trees.
- Outside the Go module graph the repository already uses: the oracle of the
  differential test, which copies five npm packages under
  `test/differential/oracle/vendor/` (`test/differential/NOTICE`); Node 24.22.1, which
  `ci.yml:234` fetches by checksum; gitleaks v8.18.4, which `ci.yml:37` installs; the image
  `postgres:16.15-trixie` by digest, which `ci.yml:134` and `restart-full.yml:22` run; and
  the actions `checkout`, `setup-go` and `upload-artifact`, pinned by commit (`ci.yml:25`,
  `:28`; `restart-full.yml:47`). s0-pins records readings for the oracle packages, Node
  and the image (s0-pins:9-42, :62-72, :81-104). The licence record and `08` §5.3 name
  neither gitleaks nor `@standard-schema/spec`, L12 names only `cosmokit` and
  `schemastery` beside the port (`08:1341`), and licence:56 and `08:1383` still list the
  oracle's JavaScript runtime as not chosen.

**Gaps.** Reading the written rule against the code and against ADR-060 shows:

- L3 and the licence record list the fields and enumerate `form`, `mode` and `mark`
  (licence:210) and the verdicts (`08:1324`), but give no key name, type or required
  rule, and the checks need records that neither lists: where the exception list and the
  allow-list live (U1 reads the allow-list from the register, `00:651`), what a `ship`
  entry records for the SBOM and source route that `08:1332` asks for, and which side of
  an `A OR B` licence is taken (`08:1336` L7; `08:1340` L11).
- The modes of licence:210 (`network`, `pull`, `ship`, `link`, `copy`) have none for a
  tool CI runs, while cosign needs an entry (ADR-060 decision 34) and the licence record
  rates it "tool (CI only)" (licence:49); the record's own rows also use "ideas", "user
  installs" and "implement" (licence:36, :37, :57).
- U2 fails on a module "imported outside that entry's plugin directories" (harness:228),
  and accepted decisions put libraries in `boot/`, `tools/`, `sdk/testkit` and packages
  that are not plugins (question Q1).
- "One entry per upstream" and "a linked module has no entry" (`08:1332`) leave open
  whether every module of the package graph is an entry, while ADR-060 decision 34 reads
  the chosen module and scans its transitive licences (question Q2).
- The exceptions are listed in `00:267` and restated, shorter, in `00:651` and `08:1365`,
  and as an open list in `07:520` (question Q3).
- Decision 51 fixes the least the register holds at S2a; `00:651` reads the status of the
  working ids from the register "written at S2a" (question Q4).
- The project names of Go and SPDX are words that decided files carry, and `00:267`
  excepts neither (question Q5).
- Harness record 7.2 names documentation places that `00:267` does not list
  (question Q6).
- Written decisions put upstream libraries in rows that `00:267` keeps free of them: pgx
  in the participants of `sqlDriver`, and the Prometheus client and the OTel SDK in
  `telemetry` and `log-export` (question Q7).
- `00:267` asks the register entry to allow a wrapping row's suffix, and `00:651` lists
  the allow-list rows among its exceptions (question Q8).
- `00:267` names the allow-list as "the protocol providers and `jobs-river`" and
  enumerates no row (decision 10), and `signal-ingest-alert-webhook`, a row of
  `aiops-base`, parses two upstream payload contracts that neither list names
  (question Q9).
- The licence record calls the five uses U1 to U5 (licence:17), the names of five checks;
  `08:1306` calls them use 1 to use 5.

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
   stays in `00:167`, and U7 compares the table of `00` §3.2 with it (decision 12).

3. **The keys of an entry.** L3's "role id" is read as two keys, `id` for the upstream and
   `role` for the role it fills, since a library has no container role and a process
   needs both (upstreams design §1.5). U7 fails on an unknown key or a missing required
   one.

   | Key | Holds | Required | Source |
   |---|---|---|---|
   | `id` | The upstream's lowercase technical name, kebab-case, unique in the file | always | `08:1332` |
   | `role` | For a process or a service, its container id of `00` §3.2 or its `ext-` id of §3.3 | form `process` or `service` | `08:1332`; `00:651` |
   | `names` | The tokens U1 looks for: every name of the upstream project, lowercase: its project name, its lowercase technical name of `00:267` and any other name it is published under. A name shared with an item of `exceptions` stays in the list, and that item's places exempt it (decision 14). The name of a format or a protocol belongs to the upstream that defines it, so Kafka, NATS and Prometheus keep `kafka`, `nats` and `prometheus`; a format defined by another body is not a name of a library that only implements it (`yaml` for the YAML library) | always; empty only for a contract that is an item of `exceptions`, whose tokens the item lists | `08:1332`; `00:267`, `:651` |
   | `form` | Decision 4 | always | licence:210 |
   | `behind` | The definition key the upstream sits behind, or `none` | always | `08:1332` |
   | `plugins` | The ids of the plugins that wrap it behind `behind` | when one exists | `08:1332`; `00:267` |
   | `bundle` | The bundle of those plugins | with `plugins` | `08:1332` |
   | `modules` | The Go module paths the upstream publishes that a build list of the repository holds | when linked | `08:1332` |
   | `images` | Each image as a reference by digest, with the role key a chart or compose file names it under | when an image is pulled or shipped | `08:1332`; `08:1334` L5 |
   | `pin` | The version, tag, commit or digest in use; for a module, the version the build lists of the repository select | when the repository uses it | `08:1332`; ADR-060 decision 34 |
   | `licence` | The SPDX expression as read; `A AND B` where directories of one module carry different licences; a `LicenseRef-` id for a text with no SPDX id, a proprietary EULA included, the form L16 and licence:205 already use; `NONE` for a contract whose text the repository neither copies nor links | always | `08:1332`; `08:1336` L7; `08:1345` L16 |
   | `elected` | The SPDX id of the side taken where `licence` holds `OR` | when `licence` holds `OR` | `08:1336` L7; `08:1340` L11; licence:204 |
   | `verdict` | Decision 4 | always | `08:1324` |
   | `modes` | Decision 4, as a list | always | licence:210 |
   | `blockers` | Per use, numbered 1 to 5 as `08:1306` numbers them, the open question or condition that blocks it | when a use is blocked | `08:1332`; `08:1306` |
   | `conditions` | Each condition, with the L or T row that enforces it | verdict `ok-with-conditions` | `08:1332` |
   | `evidence` | Each reading: the source (a URL, or `module@version` and the file read), the date read and the version, tag or commit read | unless `unverified` | `08:1332`; ADR-060 decision 34 |
   | `mark` | Decision 4; `needs-approval` while no mark policy of the owner is read | always | licence:231; `08:1357` T1 |
   | `contains` | The modules or packages of other projects that the upstream brings in, and the nested charts and image layers of a chart or image, each with its pin and licence | for modules and packages, with question Q2 (b); for nested charts and layers, mode `ship` | `08:1332`; `08:1334` L5 |
   | `default` | `true` when a shipped profile composes it, or when the release image, chart or compose file uses it with no action of the operator | always | `08:1332`; `08:1351` L22 |
   | `sbom`, `source` | Where the SBOM of the artifact that holds it is written, and the route for the notices and the corresponding source of its copyleft parts | mode `ship` | `08:1332`; `08:1334` L5 |
   | `importers` | With question Q1 (a) only: the directories outside its plugins where its modules may be imported and its names may appear, each with the accepted decision that put them there | when one exists | question Q1 |

4. **Enumerations.** `form`: `process`, `library`, `service`, `content`, `contract`, `port`
   (licence:210). `verdict`: `ok`, `ok-with-conditions`, `optional-plugin-only`,
   `replace-or-rebuild`, `unverified` (`08:1324`). `mark`: `suffix-ok`, `needs-approval`,
   `never` (licence:210, :231). `modes`: the five of licence:210, `network`, `pull`,
   `ship`, `link` and `copy`, and five the licence record already uses: `tool`, for a
   program that CI, a test or the lab runs, fetched by pin and never shipped (licence:49,
   :53; `08:1369`), which cosign's entry needs (ADR-060 decision 34); `none`, for an item
   excluded by name that no artifact holds (licence:42, :47; `08:1338` L9), so that U7 can
   refuse it by name; `ideas`, for a design reference from which nothing is copied
   (licence:36, :38, :39); `user-installs`, for a program the user installs and runs
   (licence:37, :76-78); and `implement`, for an interface or format the repository
   implements itself (licence:57, :72). The record's other phrases map onto these: "own
   parser" and "reading" are `implement` (licence:118, :126), "bundle" is `link`
   (licence:95), "fetch from upstream" is `pull` (licence:92), "not adopted" and "never in
   an artifact" are `none` (licence:55, :69), and "the user runs it" and "a contributor
   runs it" are `user-installs` (licence:81, :100); an item "inside" an upstream image is
   a `contains` item of that image's entry (licence:65, :93); a row that records only
   marks ("names", licence:102, :112) sets the `mark` of the entries it names; and "our own
   code" is first-party and no entry (licence:110). A value outside these lists fails U7,
   and a value is added by an amendment of this ADR.

5. **What "has an entry" means.** A Go module has an entry when its path is in the
   `modules` of one, or, with question Q2 (b), in the `contains` of the entry whose module
   brings it in. An image has one when its reference is in an entry's `images` or in the
   `contains` of the image that holds it as a layer. A tool baked into an image has an
   entry with mode `ship` whose `pin` is the tool's version; a tool CI runs, one with mode
   `tool`. A content set has one when an entry of form `content` names its directory or
   embedded file, and a copied or ported tree when an entry of form `port` or mode `copy`
   names its directory. The Go standard library has an entry of its own (decision 8) and no
   module path, so U2 does not reach it; `00:262` already has the kernel import it and
   nothing else.

6. **A pin moves with its reading.** U7 fails when the version that a build list selects
   for a module differs from the `pin` of its entry or from its version in `contains`: the
   build list of each module of the repository, tests included, and that of each shipped
   artifact. A version bump therefore reads the licence again in the same commit, as a new
   module is read, whether the module reaches a binary or only a test (ADR-060 decision
   34; licence:44, "the pin changes only by a reviewed bump"; `08:1369`, "re-read when a
   pin moves").

7. **The register follows the licence record.** The licence record supplies the verdicts
   (`00:167`). The commit that adds an entry or changes its verdict, licence or mark
   changes the row of the licence record for the same upstream, and the two agree (ADR-060
   decision 34, first item). The record is prose that U7 does not read, so review holds
   them equal (decision 16); the register is the copy the checks read.

8. **The entries at S2a.** With question Q4 (a) or (b) the register holds at S2a every
   upstream in use at `8e1f7b9`, in the table below, and each one S2a adds; with question
   Q4 (c), River and pgx stay on the hand list of `tools/kernelcheck` until S3 and enter
   then. Each item S2a adds enters with its licence read when it merges (ADR-060 decision
   34): the signature library and its graph (sigstore-go v1.3.0, which ADR-020, accepted
   on 2026-11-23, chooses), whose protobuf runtime, sigstore protobuf specifications and
   in-toto attestation module are read at the versions its build list selects, in the
   commit that adds it, since licence:43 names protobuf-go with no version and licence:7
   reads "at a tag or at the default branch"; goleak in `sdk/testkit` (ADR-060 decision
   12); the OCI client; the SPDX list data (ADR-060 decision 23); the base layer, licence
   scanner and SBOM tool of the builder image and the base layer of the server image the
   builder writes (ADR-044); cosign and the registry image the exit runs, with mode `tool`
   (ADR-060 decision 34). Each enters with its `names`, and a name that is a word the
   repository's own files must carry goes to the owner, as question Q5 does, before the
   entry merges.

   The Go project is two entries, as L2 makes an Apache-2.0 sub-tree of an AGPL project an
   entry of its own: L7 applies to the standard library that every binary links and not to
   the toolchain that the builder image ships. The upstreams in use at `8e1f7b9`, read on
   2026-11-23 at the version in use: Go modules in their own files at the version the build
   list selects; the oracle packages in their vendored `LICENSE` files, which match the npm
   archives file for file (s0-pins:14-16):

   | Upstream | Pin | Licence as read | Modes | Names | Mark | Used at `8e1f7b9` | Licence record |
   |---|---|---|---|---|---|---|---|
   | Go standard library | go1.26.8, read as `golang.org/toolchain@v0.0.1-go1.26.8.linux-amd64` | BSD-3-Clause in `LICENSE`, with a patent grant in `PATENTS`; the same text in each module vendored under `src/vendor/` | `link` | `go`, `golang` (question Q5) | `needs-approval` | Every binary | licence:43 |
   | Go toolchain | As above | `BSD-3-Clause AND Apache-2.0 AND OpenSSL AND ISC`: BSD-3-Clause in `LICENSE`, in each `golang.org/x` module under `src/cmd/vendor/`, in `ianlancetaylor/demangle` and `rsc.io/markdown` and, of another holder, in `google/pprof/third_party/svgpan`; Apache-2.0 in `src/cmd/vendor/github.com/google/pprof/LICENSE`; the OpenSSL licence with the original SSLeay licence, ISC and an Intel BSD-3-Clause text in `src/crypto/internal/boring/LICENSE`, for `goboringcrypto_linux_amd64.syso` and `goboringcrypto_linux_arm64.syso`. The race detector objects under `src/runtime/race`, built from LLVM compiler-rt as its `README` says, carry no licence file in the module | `tool`; `ship` from S2a, in the builder image (ADR-044) | `go`, `golang` | `needs-approval` | CI, through `setup-go` | licence:50, which records BSD-3-Clause only |
   | Cordis port and its oracle | `@deepseek-ai/cordis` 4.1.0 and `@deepseek-ai/cordis-plugin-loader` 1.1.0 at the source-content pin `fcc0ad4621` (licence:35); in `contains`, `@deepseek-ai/cosmokit` 1.9.0, `@deepseek-ai/schemastery` 3.19.0 and `@standard-schema/spec` 1.1.0 | MIT in the `LICENSE` of each of the five packages; MIT for the repository that holds the modifications, in `test/differential/oracle/notices/DeepSeek-MIT.txt` | `copy`, form `port` | `cordis`, `deepseek`, `dsh` | `never` (licence:35, :36) | The port in `sdk/kernel` and `loader/`, in every binary, and the test-only adaptations under `test/spike/s0/`; the copies under `test/differential/oracle/vendor/`, in no artifact | licence:35; `08:1341` L12, which omits `@standard-schema/spec` |
   | YAML library | `go.yaml.in/yaml/v3@v3.0.6` | `Apache-2.0 AND MIT`: its `LICENSE` puts eight files ported from libyaml under MIT and the rest under Apache-2.0, and it carries a `NOTICE` | `link` | `go-yaml` | `needs-approval` | `boot/`; linked into `aiops` and `aiopsctl` | licence:44 |
   | River | `github.com/riverqueue/river`, `.../river/riverdriver`, `.../river/riverdriver/riverpgxv5`, `.../river/rivershared` and `.../river/rivertype`, each `@v0.51.2` | `MPL-2.0 AND MIT`: MPL-2.0 in the `LICENSE` of each of the five; MIT in `rivershared@v0.51.2/levenshtein/License.txt` | `link`, tests only | `river`, `riverqueue` | `needs-approval` (question Q8) | The `_test.go` files of the S0 probes under `plugins/jobs-river/internal/`; no binary | licence:41 |
   | pgx | `github.com/jackc/pgx/v5@v5.11.0` | MIT in `LICENSE` | `link`, tests only | `pgx` | `needs-approval` | As River | licence:43 |
   | In the `contains` of River | `github.com/tidwall/gjson@v1.20.0`, `.../match@v1.2.0`, `.../pretty@v1.2.1`, `.../sjson@v1.2.5` | MIT in the `LICENSE` of each | `link`, tests only | | | Through River | no row |
   | In the `contains` of pgx and River | `github.com/jackc/pgpassfile@v1.0.0`, `.../pgservicefile@v0.0.0-20240606120523-5a60cdf6a761`, `.../puddle/v2@v2.2.3`; `golang.org/x/sync@v0.24.0`, `golang.org/x/text@v0.43.0` | MIT in the `LICENSE` of each of the first three; BSD-3-Clause in `LICENSE`, with a patent grant in `PATENTS`, for the two others | `link`, tests only | | | Through pgx and River | no row |
   | Node.js | v24.22.1 for linux-x64, archive SHA-256 `6c172a9a…95e6`, source commit `ce7aaf1c` (s0-pins:62-66) | The complete licence file at that commit, read for S0 and recorded by its hash: a composite in which bundled components keep their own terms (s0-pins:67-70). Its SPDX expression is entered from that file when the register is written | `tool` | `nodejs`, `node.js` | `needs-approval` | The oracle job, `ci.yml:234` | licence:56 and `08:1383` still read "not chosen yet" |
   | gitleaks | `github.com/zricethezav/gitleaks/v8@v8.18.4` | MIT in `LICENSE` | `tool` | `gitleaks` | `needs-approval` | The hygiene job, `ci.yml:37` | no row; not in `08` §5.3 |
   | PostgreSQL test image | `postgres:16.15-trixie@sha256:43f9275df91e012c3da3d86bea5d7bb5e67a473296cc906c5f9e80b8176b1a37`, the Docker Official Image | The PostgreSQL licence in the core `COPYRIGHT` and MIT for the packaging, each read at a commit and recorded by its hash (s0-pins:92-97); the base layers are not read, and the image is "not PostgreSQL/MIT-only" (s0-pins:99) | `tool` | `postgres`, `postgresql` | `needs-approval`, the owner asking approval for product names (licence:40, :231); an id carries the token only as the contract name of decision 9 | The `postgres` job, `ci.yml:134`; `restart-full.yml:22` | licence:40 |
   | CI actions | `actions/checkout`, `actions/setup-go` and `actions/upload-artifact`, each pinned by commit | Not read; read at the pinned commit when the register is written | `tool` | `checkout`, `setup-go`, `upload-artifact` | `needs-approval` | `ci.yml:25`, `:28`; `restart-full.yml:47`; `property-full.yml:48` | no row |

   The conditions these readings carry: the builder image's `THIRD_PARTY_LICENSES/` holds
   every text of the toolchain row, with the acknowledgments the OpenSSL and SSLeay
   licences ask of a redistribution (`08:1336` L7). OpenSSL is outside the L7 allow-list,
   so no binary links the BoringSSL objects; they need `GOEXPERIMENT=boringcrypto` and cgo
   (`src/crypto/internal/boring/boring.go:5`, `:15`), and the builder runs with cgo off
   (ADR-060 decision 22). The race objects need cgo too (`src/runtime/race/race.go:20`),
   and their licence is read at its own source before the builder image merges. The port
   entry records L12 as a condition, which `loader/` does not meet at `8e1f7b9`: the
   commit that writes the register adds `loader/NOTICE` with both MIT notices. A tool image
   records no layers, since `contains` asks for layers in mode `ship` only.

### The exception list

9. **The exception list is the list of `00:267`**, recorded under `exceptions` with, for
   each item, its tokens and the places where they may appear. Which items reach ids is
   question Q3. The list lives in `00`, so an item is added or removed by an ADR that edits
   `00:267`. U1 applies an item to a token of an entry's `names` (decision 14); a token of
   a contract item that no other entry carries is the contract's name, which `00:267`
   excepts, and U1 does not look for it.

   | Item | Tokens | Where they may appear | Source |
   |---|---|---|---|
   | The PostgreSQL dialect behind `sql`, `sql.Bound`, `platformSql` and `sqlDriver` | `postgres`, `postgresql`, `pgx` | Those definitions and their suites; the `sql` fake and `sdk/testkit/sqlfake`, the same engine in a throwaway schema; an import of pgx v5 in `sdk/def/sql`; the participants of `sqlDriver`, as question Q7 decides; the suffix of a row on the allow-list that wraps it | `00:26`, `:106` (W2), `:214`, `:262`, `:267`, `:651`; `07:28`, `:33`, `:50` |
   | The two query dialect keys, `metricsQuery` and `logsQuery` | For `metricsQuery`, `promql` and `prometheus`; for `logsQuery`, `logql` and `loki` | The definition of its key, its fake and its suite: `promql` and `prometheus` in prose, doc comments and query text; `logql` and `loki` in prose only, and a Markdown file that names them carries the T3 statement (`08:1359`). Never in an id, a type, a config key or a path, since `00:649` and T1 keep Loki and LogQL out of every name, and licence:231 puts Prometheus on a mark list whose usage page says a mark is not combined, hyphenated or abbreviated | `00:239`, `:267`, `:649`; `08:1357` T1; licence:231 |
   | The protocols with several implementations: the S3 API, the OpenAI-compatible API, OIDC, SMTP, OCI, OTLP, the Kubernetes API and MCP | `s3`, `openai`, `oidc`, `smtp`, `oci`, `otlp`, `kubernetes`, `k8s`, `mcp` | The definitions, fakes and suites that declare the protocol; ids, as question Q3 decides | `00:267`, `:651`; `08:1365` T9 |
   | The Cordis semantics of the kernel port | `cordis`, `deepseek`, `dsh` | `sdk/kernel/`, `loader/`, `test/differential/` and `test/spike/s0/`, their notices included | `00:267`; `08:1341` L12 |
   | The default stack bundle (`aiops-stack`, `agent-base` and the agent bundles) | The `names` of every entry whose `plugins` sit in one of those bundles, read from the register | The code and content of those plugins and their config in the golden dumps, as the wire names, syntax and file paths those upstreams define; never an id, where the marks of `00:649` and T1 hold | `00:267`, `:649`, `:651`; `04:9`, `:14`; harness:211; upstreams design §1.1 |
   | With question Q5 (a): the Go language and SPDX identifiers | `go`, `golang`, `spdx` | Every place | question Q5 |

### The base provider allow-list

10. **The allow-list is the eight rows that `00:267` and `03:93` describe**, each the
    provider of a contract of decision 9 or `jobs-river`, as upstreams design §1.1
    enumerates them:

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
    when that verdict is lost, the trigger of U5 fires (`00:629`). A row on the list is an
    exception to the rule that no id of a listed bundle names an upstream (`00:651`); its
    suffix still needs its entry's `mark` as question Q8 decides. Any other row of
    `aiops-base` may name no upstream and import no module of an entry (U1, U2), apart
    from the placements questions Q7 and Q9 decide. One more row wraps upstream contracts
    as written today: `signal-ingest-alert-webhook` parses the Alertmanager v4 and
    dashboard-alerting v1 payload families (`06:38`; `01:192`; `03:252`), which neither
    `00:267` nor this list names and which upstreams design §1.1 leaves out; question Q9
    decides where they are parsed. A row that would need a library, for example a JWT and
    JWKS library, which L13 lists as not chosen yet (`08:1342`), uses the standard library
    or joins the list by an ADR that edits `00:267` before it merges. `tool-mcp` is in the
    module table with no row in `aiops-base` (`00:277`) and is not on the list. The list
    is data in the register from S2a; the clauses of U1, U2 and U7 that read it have no
    case before the first row of `aiops-base` merges (S3) and land with it (`08:678`).

### The checks

11. **When each check runs.**

    | Check | Fails when | Runs | From | Source |
    |---|---|---|---|---|
    | U1 name lint | Decision 14 | T0: every commit, `make check` and CI | S2a, once questions Q1, Q3, Q5 and Q6 are answered | harness:227; `00:267`, `:297`, `:651` |
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
    - an entry breaks decision 3 or 4, a `licence` that holds `OR` with no `elected`
      included (`08:1336` L7; `08:1340` L11);
    - a module in the package graph of a shipped binary has no entry (decision 5). At S2a
      these are `aiops` and `aiopsctl` with the test binaries `aiopsctl` embeds (ADR-060
      decision 46; licence:54), and the build list of each image the builder writes, which
      its signed manifest records (ADR-060 decision 20);
    - a base layer or tool that an image the repository builds bakes in has no entry with
      mode `ship`: at S2a the builder image and the server image, whose definitions name
      each by digest or version in the form ADR-044 fixes (ADR-060 decision 22);
    - a data set a shipped artifact embeds has no entry: at S2a the SPDX list data;
    - a pin differs from a build list (decision 6);
    - an entry is `unverified` (`08:1332`);
    - an entry with `default: true` is not `ok` or `ok-with-conditions`;
    - a `replace-or-rebuild` entry appears in any `dist/*.yaml`;
    - an entry with an AGPL licence has `modules`; an Apache-2.0 sub-tree of an AGPL project
      is an entry of its own (`08:1331` L2);
    - a `ship` entry has no `sbom` or `source`;
    - an entry with mode `link` has a licence, read with `elected`, that holds an id outside
      the allowed list of L7, or one of its conditional ids (MPL-2.0, CC0-1.0, the MCP
      composite) with no condition that cites L7 or L8 (`08:1336` L7, `:1337` L8; ADR-060
      decision 34, fourth item);
    - a component of the table of `00` §3.2 has an AGPL licence and is missing from the
      AGPL boundary list of `00:167`, the clause of `00:167` and `08:1332`. U7 reads the ids
      of that table, takes each licence from the entry whose `role` is the id or, while none
      exists, from the licence record row that names the id (licence:87), and reads the
      boundary list from that paragraph, so the clause has cases from S2a;
    - a row on the allow-list is in the `plugins` of no entry whose verdict is `ok` or
      `ok-with-conditions` (decision 10, from S3).

    Deployed images, charts, nested charts and shipped content directories enter with their
    first file (S7 and later), and so do the cases of their clauses. Two things stay with
    review: whether every tool CI runs has an entry, as `08:1390` makes the table of `08`
    §5.3, the register and ADR-027 the licence control of tools that never ship; and
    whether the licence record row agrees with the entry (decision 7). The transitive
    licence scan is the licence report job of ADR-060 decision 36 (`08:1381`), not U7.

13. **U2 from S2a.** U2 fails when a Go file of the repository, tests included, imports a
    package of a module that is in no entry's `modules`, or of a module of an entry outside
    the directories of that entry's `plugins` and the places question Q1 allows, the pgx
    import of decision 9 apart; and when the require lists of the SDK modules leave what
    `00:262` and ADR-060 decision 4 allow: none for `sdk/kernel`, and for `sdk` only
    `sdk/kernel`, pgx v5 from S3 and `sdk/testkit` for its tests. Whatever question Q7
    decides, no client-library type other than pgx v5 enters `sdk/def`, and this clause
    holds it. U2 replaces the third-party part of `tools/kernelcheck imports`: the map
    `pluginThirdParty` and the YAML rule become reads of the register, and the rules on
    the SDK, the definitions, `boot/` and `loader/` stay. The import analyzer moves into a
    package that `tools/kernelcheck` and `aiopsctl` share and that works from any module
    root (ADR-060 decision 23); that package takes the register as an input.
    `tools/kernelcheck` passes `tools/upstream-components.yaml`; `plugin check` in another
    module, which has no register, passes none, and the rule of `07:34` holds there
    instead: a plugin imports the SDK and its own dependencies, never another plugin. At
    `8e1f7b9` River and pgx are imported only under `plugins/jobs-river/` (harness:219),
    and the YAML library only in `boot/`, which question Q1 decides.

14. **U1 from S2a, over the places that exist.** U1 fails when a token of an entry's
    `names` appears, case-insensitively, as a whole word, a kebab-case or snake-case
    segment or a camel-case part:
    - in a file or a path under `sdk/`, `boot/` or `loader/`, which covers every
      definition, `definition.yaml`, hook, fake and suite (`00:267`, `:651`);
    - in the id or schema of a dispatcher, a policy row or a row of `aiops-core`
      (`00:267`), and in the id of a row of a bundle that a shipped profile lists
      (`00:651`);
    - in a golden dump of a shipped profile (`00:651`; `08:1266`);

    outside the places that an item of decision 9 listing the token gives it, the
    directories of that entry's `plugins` and, with question Q1 (a), its `importers`; and
    when an id carries a token other than once, as the suffix of a plugin in that entry's
    `plugins` or of that plugin's own bundle, while the entry's `mark` is `suffix-ok`
    (`00:267`, `:651`; licence:231), a row on the allow-list included as question Q8
    decides. At S2a its places are the SDK tree, `boot/`, `loader/`, the rows of
    `aiops-core` and `aiops-cli` and the dumps of `dev` and `cli` (ADR-060 decisions 27
    and 53). Container ids, compose services, chart and image keys, databases, metrics,
    rows of `aiops-base` and `test/acceptance/` enter U1 in the commit that creates the
    first of each (`08:678`), and the documentation places of harness record 7.2 as
    question Q6 decides.

    At `8e1f7b9`, with the names of decision 8 and the recommended answers to questions Q1
    and Q5, U1 finds one name: the comment at `sdk/def/reload/v0/reload.go:184` names dsh
    in a definition, outside the places of the port item, and is reworded in the commit
    that adds U1. The mentions of Cordis, DeepSeek and dsh in `sdk/kernel/` and its
    `NOTICE` and at `loader/apply.go:77` stand under the port item. Without question Q5
    (a), `go` fails in 79 files and every Go file path, and `spdx` in 121 files once the
    SPDX data enters. Two other names stand in U1's places that U1 does not find at S2a,
    since no entry carries them under question Q4 (a): `prometheus` in the two files of
    `boot/testdata` named in the context, and `k8s`, a token of the protocol item, as a
    label path in `sdk/def/loader/v0/volatile_test.go`, outside that item's places. The
    commit that adds U1 rewords both to role names, an endpoint such as
    `https://metrics.example.internal` and a label such as `app.pod`, so they hold under
    question Q4 (b) as well, and Prometheus enters the register with its first use.

15. **U3 from S2a.** U3 fails when a file under the `fake/` or `conformance/` directory of
    a definition (`07:28`) or under `sdk/testkit/sqlfake` (`00:262`; `07:33`) imports a
    package of a module of an entry or holds a token of one, outside the PostgreSQL and
    query dialect items of decision 9 and, with question Q1 (a), the `importers` of an
    entry; and when a cassette of an upstream sits outside the directory of a plugin or a
    pack in that entry's `plugins` and outside `fixtures/<plugin>/` for one of them
    (`07:520`, `:1402`, `:1479`; `08:1222`; harness:229). The file form of a cassette is the
    one the record-and-replay tier reads (`07:1402`), fixed with the first cassette at S8a
    (`07:1479`); the clause has no case before it. The definitions of S2a and the S1 suites
    of ADR-060 decision 44 are the first cases of the rest.

### Working rule

16. **A dependency lands in one commit with its entry.** The commit that adds a module to
    a `go.mod`, a base layer or tool to an image definition, a tool to CI or a data set to
    an artifact adds or extends its entry with the reading at the pinned version and the
    row of the licence record (decision 7) and, for a module imported outside the plugin
    that wraps it, the decision that places it (question Q1). Of the four items of ADR-060
    decision 34, U2 and U7 check the entry, its pin and, for a linked module, its licence
    against L7 (decisions 6, 12 and 13), and the licence report job of ADR-060 decision 36
    scans the transitive licences (`08:1381`). Review checks the reading, the licence
    record row and that a tool CI runs has an entry (decision 12).

## Consequences

- S2a gains the register with the entries of decision 8, U7 as the register check of CI,
  and U1 to U3 at T0, all in `tools/`. With the recommended answers no production package
  of `sdk/kernel`, `boot/` or `loader/` changes and no line budget moves. The commit that
  adds U1 rewords the comment of `sdk/def/reload/v0`, the endpoint in two files of
  `boot/testdata` and the labels of one test of `sdk/def/loader/v0` (decision 14).
- The commit that writes the register adds `loader/NOTICE` with both MIT notices (L12) and
  brings the licence record and `08` to the entries of decision 8 (decision 7): licence:35
  and `08:1341` name `@standard-schema/spec`; licence:50 records the per-directory
  licences of the toolchain; licence:56 and `08:1383` name Node 24.22.1; licence record
  §2.1 and `08` §5.3 gain gitleaks and the three actions.
- On acceptance, licence:210 and `08:1332` (L3) gain the keys and values of decisions 3
  and 4: `id` and `role`, `names` as decision 3 reads it, `elected`, `sbom`, `source`,
  `importers`, `contains` for modules and packages, and the modes `tool`, `none`, `ideas`,
  `user-installs` and `implement`. They change in the commit that records the acceptance
  (`08:996`), so harness:223 keeps naming the schema of licence record §7.2.
- The hand-kept third-party rules of `tools/kernelcheck imports` give way to the register
  (decision 13).
- Every version bump of a module with an entry reads its licence again in the same commit,
  a module of a test included (decision 6).
- With question Q1 (a), `00:267`, `07:37`, `07:50` and harness:210 gain the importers
  rule; with question Q3 (a), `00:651` and `08:1365` cite the list of `00:267` instead of
  restating it and `07:520` closes its list; with question Q5 (a), `00:267` gains the Go
  and SPDX item; questions Q7, Q8 and Q9 name the lines their answers edit. Each edit
  lands in the commit that records the answer (`08:996`).
- U4 starts with the first shipped profile that lists an upstream bundle; U5, U6 and U8
  keep their triggers (`00:629`). Leg F stays the release test of upstream independence.
- This ADR settles the S2a part of UP-1, the register (`08:790`). The bundle moves and the
  role ids of UP-1 stay at S8b, and the 36-row list that holds UP-1 stays pending the owner
  (`08:752`).
- On acceptance the rows of `00:788` and `08:1185` record ADR-054 as decided.

## Questions for the owner

Each question gives its options, the recommended one, why, and when the answer is needed.

**Q1. Where may an upstream library be imported outside the plugin that wraps it?**
`00:267` imports an upstream library "only inside the plugin that wraps it" and keeps its
name out of `sdk/`, `boot/` and `loader/`; `07:50` and harness:210 allow no upstream module
there, in a dispatcher, a policy row or a row of `aiops-core`, with pgx in `sdk/def/sql` the
one stated exception; U2 fails on a module "imported outside that entry's plugin
directories" (harness:228). The glossary counts a library as an upstream (`00:708`).
Accepted decisions place libraries elsewhere: the YAML library in `boot/` and `tools/`
(ADR-059 decision 6) and in the operations plugin, `plugingen` and the builder (ADR-060
decision 41); goleak in `sdk/testkit` (ADR-060 decision 12), which `07:33` lets import
"anything", and with it testcontainers-go and the embedded PostgreSQL wrapper (licence:54;
`08:1382`); and the OCI client, which `aiopsctl` and the builder use (ADR-060 decisions 19,
22 and 34). ADR-020, accepted on 2026-11-23, places the signature library in the
operations plugin, linked into `aiopsctl` and the server image, and from S6a on the
`plugin-manager` side, a plugin of `aiops-core` (`00:275`). The participants of
`sqlDriver` and the telemetry rows are question Q7.
- (a) An entry may list `importers`: directories outside its plugins where its modules may
  be imported and its names may appear, each with the accepted decision that put them
  there. At S2a: the YAML library in `boot/`, `tools/`, the operations plugin, `plugingen`
  and the builder; goleak in `sdk/testkit`; the OCI client where ADR-047 places it; the
  signature library where ADR-020 places it, once accepted. U1 and U2 fail on any other
  place, and any importer other than the entry's own plugins needs an accepted decision.
  `sdk/kernel`, `loader/` and the definitions with their fakes and suites never appear,
  the pgx import of W2 apart. `00:267`, `07:37`, `07:50` and harness:210 gain "or where its
  register entry lists it by an accepted decision", for the name and for the import.
- (b) The rule as written: a library sits only in the plugin that wraps it behind a
  definition. ADR-059 decision 6 and ADR-060 decisions 6, 12, 14 and 41 are reopened:
  `boot` reads its YAML inputs without the library, since it imports no domain definition
  (`00:262`), and goleak leaves `sdk/testkit`.
- (c) U1 and U2 cover only the plugin trees (`plugins/`, `contrib/`, `examples/`,
  `templates/`), and the hand rules of `tools/kernelcheck` keep the rest, so the written
  rule stays unchecked in `boot/`, `tools/` and `sdk/testkit`.
- Recommended: (a). Every placement the owner accepted stays and is checked, U1 and U2
  still fail on a placement no decision made, and no code moves. (b) undoes accepted work
  for a rule those decisions already departed from; (c) leaves part of the rule unchecked.
  Needed before U1 or U2 merges.
- **Answered 2026-11-23: (a)**, decision 17.

**Q2. Is every module of the package graph an entry of its own?** L3 fails the release
when a "linked module ... has no entry", and the register holds "one entry per upstream"
(`08:1332`; licence:210). ADR-060 decision 34 reads a new module's licence at a primary
source and scans "its transitive licences", and names three modules of sigstore-go's graph
to read before it merges: the protobuf runtime, the sigstore protobuf specifications and the
in-toto attestation module. River's packages at `v0.51.2` bring in six modules of other
projects: the four tidwall modules, `puddle/v2` and `x/sync`; pgx brings in `puddle/v2`,
`pgpassfile`, `pgservicefile` and `x/text`; and River's module `rivershared` carries an MIT
directory (decision 8).
- (a) One entry per module, each with every key of decision 3 and a licence read by hand
  at its pin.
- (b) An entry's `modules` holds the modules its upstream publishes; each module of another
  project that they bring into the package graph is listed in that entry's `contains`, with
  its version and the licence the scan detects, and a module that a package of the
  repository imports has an entry of its own. A contained module is read by hand at its pin,
  and the reading recorded in `evidence`, where a decision names it (the three of ADR-060
  decision 34) or where its detected licence is unknown or outside L7. U7 fails when a
  module of the graph is in neither or its version moved (decision 6).
- (c) Only the modules the repository requires directly have entries; the rest is left to
  the licence scan of L7 and L16.
- Recommended: (b). Every linked module has an entry, as L3 asks, and the work splits as
  decision 34 splits it: the chosen module and the modules decision 34 names are read by
  hand before they merge, and the rest of the graph is scanned. (a) multiplies entries that
  no check reads more closely than (b); (c) drops L3's clause for every transitive module.
  Needed before U7 merges.
- **Answered 2026-11-23: (b)**, decision 18.

**Q3. Which list of exceptions governs ids?** `00:267` lists the stated exceptions: the
PostgreSQL dialect, the two query dialect keys, eight protocols (the S3 API, the
OpenAI-compatible API, OIDC, SMTP, OCI, OTLP, the Kubernetes API, MCP), the Cordis
semantics of the kernel and the default stack bundle. `00:651`, the rule for ids, opens
"outside the stated exceptions of §4.3" and restates them as six contract names, without
SMTP, OCI and OTLP; T9 restates the same six (`08:1365`). `07:520` restates the eight
protocols after "such as", which reads as an open list. The row
`fleet-template-otlp-receiver` of `agent-base` and its feature key `otlpReceiver` carry
`otlp` (`04:191`; `00:281`).
- (a) The list of `00:267` governs every place, ids included, as decision 9 records it;
  `00:651` and `08:1365` cite it instead of restating it, and `07:520` drops "such as".
- (b) The six names of `00:651` govern ids; the row and the feature key that carry `otlp`
  take role names, and `04:191` and `00:281` are edited; `07:520` drops "such as".
- Recommended: (a). Both restatements open with "outside the stated exceptions of §4.3",
  so their parentheses read as restatements, and (b) renames for a list that no decision
  chose to shorten. Needed before U1 merges, which then reads one list.
- **Answered 2026-11-23: (a)**, decision 19.

**Q4. Which entries does the register hold at S2a?** ADR-060 decision 51 asks for every
module, image and tool that S2a links, ships or bakes in. `00:651` reads the `suffix-ok`
status of the working ids from the register "written at S2a", and U1 knows only the names
of the entries it reads. A part lands with its first consumer (`08:678`). L3 fails the
release on an `unverified` entry, and L13 says such an entry "blocks the release that
first ships it" (`08:1332`, `:1342`). L9 and L15 exclude items by name (`08:1338`,
`:1344`); the licence record reads the licence of five of them: River Pro and the SeaweedFS
Enterprise Edition (licence:42), MinIO (licence:47), LiteLLM `enterprise/` (licence:67) and
the LangGraph Platform server (licence:79).
- (a) At S2a the entries of decision 8, and those five with mode `none` and the licence the
  record reads, a `LicenseRef-` id for a proprietary EULA, which the clause of U7 on
  `dist/*.yaml` reads. The other items L9 and L15 name, the pattern "any other `ee/` or
  `enterprise/` directory" and the `-enterprise` images, stay rules of the release checks
  of L9 and L15 and of the per-directory scan of L7, not entries. Every other upstream
  enters with the commit that first uses, wraps or names it, and U1 learns its names then;
  until that commit, a new name is caught by review. Every entry is in use or refused by
  name, so L3's clause on `unverified` stands as written.
- (b) As (a), and one entry per upstream of licence record §2 and of `00` §3.2, with the
  verdict and evidence the record holds, so U1 knows every name from its first run. An
  unused entry may be `unverified`, and L3's clause is read as L13: U7 fails on an
  `unverified` entry only where the artifact under check links, ships or bakes it in.
- (c) Only decision 51's minimum; River and pgx stay on the hand list of `tools/kernelcheck`
  until S3, and U2 reads the register from S3.
- Recommended: (a). It meets decision 51 and the rule that a part lands with its first
  consumer, keeps L3 as written, needs no reading the record lacks, and U1 holds for each
  upstream from the commit that first brings it, which is before the first community
  release (`00:267`). (b) transcribes an entry for every row of the licence record and
  narrows L3; (c) keeps two lists of one rule until S3. Needed before the register merges,
  which is before the first new dependency of S2a (ADR-060 Q15).
- **Answered 2026-11-23: (a)**, decision 20.

**Q5. Are the Go language and SPDX identifiers stated exceptions?** Decision 3 keeps every
project name of an upstream in its `names`. Go's project name is the word `go`, which 79
files of `sdk/`, `boot/` and `loader/` hold and every Go file path ends in; `00:262` and
`07:27` make the kernel a Go module that imports the standard library, and each definition
is a package of a Go module that holds its interface and message types (`07:28`). SPDX's
project name heads the licence header that L1 puts on every source file (`08:1330`), 121
files of those trees, and the manifest licence field is an SPDX expression (`08:1345`
L16); the SPDX list data enters at S2a (ADR-060 decision 23). `00:267` excepts neither.
- (a) `00:267` gains one stated exception: the Go language, in which definitions are
  declared, and SPDX identifiers, which L1 and L16 require. Decision 9 records it with the
  tokens `go`, `golang` and `spdx`, allowed in every place, and the entries keep those
  names.
- (b) The entries leave these words out of `names`: the Go entries carry `golang` only,
  and the SPDX data entry the name of its data set only; `00:267` is unchanged, and U1 never
  looks for `go` or `spdx`.
- (c) U1 matches `go` and `spdx` only in identifiers and ids, never in Go syntax, file
  paths, `go.mod` files or licence headers.
- Recommended: (a). It states in `00` what decided files already require, keeps decision
  3's rule that all of a project's names are in `names`, and gives U1 no false finding.
  (b) drops a project name by choice, which decision 3 forbids for every other upstream;
  (c) parses three syntaxes for two words and still trips on prose. Needed before U1
  merges.
- **Answered 2026-11-23: (a)**, decision 21.

**Q6. Does U1 read the documentation places of harness record 7.2?** Harness record 7.2
writes decision sentences, credential classes, saga stages and acceptance scripts by role,
an upstream appearing only after "default:" or in the Technology and Rationale columns
(harness:213; `README:115`), and U1 fails where 7.2 forbids a name (harness:227). `00:267`
lists code and id places only. The Decision column of `00` §2.1, the credential table of
§5.4 and the stages of §5.6 exist and name upstreams; the delete saga's first stage waits
out "the Mimir ingester flush" (`00:432`). Acceptance scripts enter U1 with
`test/acceptance/` (decision 14).
- (a) U1 reads the Decision column of `00` §2.1, the classes of the credential table of
  §5.4 and the stages of §5.6 from S2a, outside the text after "default:" and the
  Rationale and Technology columns, with the names the register holds. An item of
  decision 9 also lets its tokens stand there where they name its contract, as W2 names
  PostgreSQL and row 1 of §2.1 the Cordis semantics. Each finding is reworded in the
  commit that adds U1, and later names fail as their entries land.
- (b) As (a), from the first community release, by which harness:227 and `00:267` date U1,
  so the rewording of `00` lands once with every name the register then holds.
- (c) These places stay written by role under review, as `README:115` states the
  convention, and harness:227 is edited to say that U1 covers code, ids and dumps.
- Recommended: (a). Decision 14 runs U1 over the places that exist, and these exist; the
  rewording grows with each entry instead of landing in one batch before the release. (b)
  leaves the places unchecked while names are added; (c) narrows a written check. Needed
  before U1 merges.
- **Answered 2026-11-23: (a)**, decision 22.

**Q7. Where do pgx and the telemetry libraries sit?** `sqlDriver` hands
`Pgx(tx) pgx.Tx` and a role switch to the providers of `jobs`, `events`, `audit` and `kv`
(`07:836`; `03:95`, `:120`; `00:245`), so `events-outbox`, which rings a `LISTEN/NOTIFY`
doorbell (`03:107`), and `audit-chain`, a policy row (`00:253`; `03:114`), import pgx, while
`jobs-river` and `kv-postgres` are on the allow-list. ADR-045 (owner decision 2026-11-19)
keeps a raw-driver path for these infrastructure participants (`00:264`). `telemetry` and
`log-export`, rows of `aiops-core`, provide a Prometheus registerer and an OTel tracer
(`03:69`, `:343`), and licence:43 lists the OpenTelemetry Go SDK as linked. `00:267`,
`07:50` and harness:210 allow no upstream module in a policy row or a row of `aiops-core`,
pgx in `sdk/def/sql` apart, and `00:262` lets the SDK module require only `sdk/kernel`
and pgx v5. U2 runs from S2a (decision 13) and would fail these rows when they merge at
S3 (`08:696`).
- (a) Importers by accepted decision, as question Q1 (a): pgx lists the four participants
  of `sqlDriver`, and the Prometheus client and the OTel SDK list `telemetry` and
  `log-export`. `00:267`, `07:50` and harness:210 name these placements.
- (b) Neutral seams: `sqlDriver` hands its participants the raw-driver path through types
  of `sdk/def/sql` that carry no pgx type beyond the row and tag types `07:28` allows, so
  `events-outbox` and `audit-chain` import no pgx module; `metrics` and `tracer` hand out
  neutral types of `sdk/def`, and the two telemetry libraries sit only in a row outside
  `aiops-core` that wraps them, on the allow-list as an OTLP provider or by an ADR that
  edits `00:267`. `03:69`, `:95`, `:107`, `:114`, `:120`, `:343` and `07:836` are edited.
- (c) (a) for pgx and (b) for the telemetry libraries: the PostgreSQL item of decision 9
  covers the four participants of `sqlDriver`, and `00:267`, `07:50` and harness:210 name
  them beside `sdk/def/sql`; no row of `aiops-core` imports an upstream module, and `03:69`
  and `:343` are edited.
- Recommended: (c). pgx in the participants follows from two owner decisions, W2 and
  ADR-045, which `07:836` already writes into the definition, so naming the placement keeps
  them. The telemetry libraries have no such decision, and in `aiops-core` they would sit
  in the core of every profile, which principle 9 refuses (`00:32`). (a) puts them there;
  (b) rewrites a transaction contract the owner decided on 2026-11-19. Whatever the answer,
  no client-library type other than pgx v5 enters `sdk/def` (decision 13). Needed before
  the first S3 row of `sql`, `jobs`, `events`, `audit` or telemetry merges.
- **Answered 2026-11-23: (c)**, decision 23.

**Q8. Does a row on the allow-list need its entry to be `suffix-ok`?** `00:267` gives the
wrapping row the upstream's name "when its register entry allows it (§8.1)". `00:651`
lists the allow-list rows among the exceptions to the rule that no id of a listed bundle
names an upstream, and then lets the name of an upstream outside the default stack appear
as a suffix "only when its entry ... is marked `suffix-ok`"; an entry with no mark policy
read is `needs-approval` (licence:231). Of the eight rows, five carry a contract name, which
`00:651` excepts and question Q3 governs (the mark "S3" in an id is lawyer question Q14,
licence:56), and `sql-platform` and `notifier-email` carry none; `jobs-river` carries
River's name, whose mark policy the licence record does not read (licence:41).
- (a) The suffix needs `suffix-ok`, as `00:267` and the third sentence of `00:651` say;
  the allow-list exception lifts only the ban on names in a listed bundle.
  River's mark policy is read before the `jobs-river` row merges at S3; until then River's
  entry is `needs-approval`, and U1 fails on the id `jobs-river`, which merges only once
  cleared or under a role id.
- (b) The allow-list rows are exceptions of `00:651` whatever the mark; `00:267` gains
  "outside the base provider allow-list", and U1 skips its suffix clause for them.
- Recommended: (a). It keeps both sentences of `00` as decision 14 reads them, and costs
  one reading of a mark policy before S3. (b) puts a library's name in a core id with no
  reading of its owner's terms. Needed before the `jobs-river` row merges (S3).
- **Answered 2026-11-23: (a)**, decision 24.

**Q9. Where are the Alertmanager and dashboard-alerting payload families parsed?**
`signal-ingest-alert-webhook` is a row of `aiops-base` (`00:277`) that parses the
Alertmanager and dashboard-alerting payload families (`06:38`), named as Alertmanager v4
and dashboard-alerting v1 (`01:192`; `03:252`). The glossary counts a contract as an
upstream (`00:708`), and the licence record rates the Alertmanager webhook `ok`
(licence:118). A row of `aiops-base` may wrap an upstream only from the allow-list
(`00:267`), and neither the exceptions of `00:267` nor the allow-list name these two
contracts; upstreams design §1.1 leaves the row out. Dashboard alerting belongs to the
default stack's vendor, whose marks stay out of every name (`00:649`). Every server
profile lists the default connector pack `aiops-datasources` (`08:255`), which holds
"every first-party row and content file that is specific to the default stack's dialects"
(`08:283`).
- (a) The row joins the allow-list, and `00:267` adds the two payload families to the
  stated exceptions as contracts; the row keeps its role id, and the entry of the
  dashboard-alerting contract has the mark `never` (`00:649`; `08:1357` T1).
- (b) The parsing of the two families moves into a `signals` contribution of
  `aiops-datasources`; `aiops-base` keeps `signal-core` and `signal-ingest-generic-json`.
  `00:277` and `:278`, `03:252`, `06:38`, `08:283` and `:306` move the row.
- Recommended: (b). It keeps the closed lists of `00:267` and puts the two families where
  `01:192` already puts every other payload family, in a composed pack; every server
  profile keeps the intake. (a) adds a vendor's format to the core's exceptions. Needed
  before the `signal-ingest-alert-webhook` row merges (S8b, `08:728`).
- **Answered 2026-11-23: (b)**, decision 25.

## Owner answers of 2026-11-23

The project owner accepted this ADR on 2026-11-23 and answered every question of the
section "Questions for the owner" on the same date, each with its recommended option.
Each decision below names the question it answers. Where a decision above leaves a point
to a question, the decision here settles it.

17. **An entry may list its importers** (Q1 (a)). An entry may list `importers`:
    directories outside its plugins where its modules may be imported and its names may
    appear, each with the accepted decision that put them there. At S2a: the YAML library
    in `boot/`, `tools/`, the operations plugin, `plugingen` and the builder; goleak in
    `sdk/testkit`; the OCI client where ADR-047 places it; the signature library where
    ADR-020 places it, once accepted. U1 and U2 fail on any other place, and any importer
    other than the entry's own plugins needs an accepted decision. `sdk/kernel`, `loader/`
    and the definitions with their fakes and suites never appear, the pgx import of W2
    apart. `00:267`, `07:37`, `07:50` and harness:210 gain "or where its register entry
    lists it by an accepted decision", for the name and for the import. The operations
    plugin of ADR-060 decision 41, which the entries of the YAML library, the OCI client
    and the signature library list as an importer, is a row of the `aiops-cli` bundle of
    the `cli` profile at S2a, not of `aiops-core` (owner, 2026-11-23), so decision 23
    holds unchanged.

18. **A module of another project is listed in the `contains` of the entry that brings
    it in** (Q2 (b)). An entry's `modules` holds the modules its upstream publishes; each
    module of another project that they bring into the package graph is listed in that
    entry's `contains`, with its version and the licence the scan detects, and a module
    that a package of the repository imports has an entry of its own. A contained module
    is read by hand at its pin, and the reading recorded in `evidence`, where a decision
    names it (the three of ADR-060 decision 34) or where its detected licence is unknown
    or outside L7. U7 fails when a module of the graph is in neither or its version moved
    (decision 6).

19. **The list of `00:267` governs every place, ids included** (Q3 (a)), as decision 9
    records it. `00:651` and `08:1365` cite it instead of restating it, and `07:520` drops
    "such as".

20. **The register at S2a holds the upstreams in use and five items refused by name** (Q4
    (a)). At S2a the register holds the entries of decision 8, and River Pro, the SeaweedFS
    Enterprise Edition, MinIO, LiteLLM `enterprise/` and the LangGraph Platform server
    with mode `none` and the licence the licence record reads, a `LicenseRef-` id for a
    proprietary EULA, which the clause of U7 on `dist/*.yaml` reads. The other items L9
    and L15 name, the pattern "any other `ee/` or `enterprise/` directory" and the
    `-enterprise` images, stay rules of the release checks of L9 and L15 and of the
    per-directory scan of L7, not entries. Every other upstream enters with the commit
    that first uses, wraps or names it, and U1 learns its names then; until that commit, a
    new name is caught by review. L3's clause on `unverified` stands as written.

21. **The Go language and SPDX identifiers are a stated exception** (Q5 (a)). `00:267`
    gains one stated exception: the Go language, in which definitions are declared, and
    SPDX identifiers, which L1 and L16 require. Decision 9 records it with the tokens `go`,
    `golang` and `spdx`, allowed in every place, and the entries keep those names.

22. **U1 reads the documentation places of harness record 7.2 from S2a** (Q6 (a)). U1
    reads the Decision column of `00` §2.1, the classes of the credential table of §5.4
    and the stages of §5.6, outside the text after "default:" and the Rationale and
    Technology columns, with the names the register holds. An item of decision 9 also lets
    its tokens stand there where they name its contract, as W2 names PostgreSQL and row 1
    of §2.1 the Cordis semantics. Each finding is reworded in the commit that adds U1, and
    later names fail as their entries land.

23. **pgx sits in the participants of `sqlDriver`, and the telemetry libraries outside
    `aiops-core`** (Q7 (c)). The PostgreSQL item of decision 9 covers the four
    participants of `sqlDriver`, and `00:267`, `07:50` and harness:210 name them beside
    `sdk/def/sql`. No row of `aiops-core` imports an upstream module: `metrics` and
    `tracer` hand out neutral types of `sdk/def`, and the Prometheus client and the OTel
    SDK sit only in a row outside `aiops-core` that wraps them, on the allow-list as an
    OTLP provider or by an ADR that edits `00:267`. `03:69` and `:343` are edited. No
    client-library type other than pgx v5 enters `sdk/def` (decision 13). The operations
    plugin, which imports the signature library, the OCI client and the YAML library, is
    a row of the `aiops-cli` bundle of the `cli` profile at S2a, not of `aiops-core`, so
    this decision holds unchanged; where `plugin-manager` sits from S6a is decided by the
    ADR of S6a (owner, 2026-11-23).

24. **A row on the allow-list carries its suffix only when its entry is `suffix-ok`**
    (Q8 (a)), as `00:267` and the third sentence of `00:651` say; the allow-list exception
    lifts only the ban on names in a listed bundle. River's mark policy is read before the
    `jobs-river` row merges at S3; until then River's entry is `needs-approval`, and U1
    fails on the id `jobs-river`, which merges only once cleared or under a role id.

25. **The Alertmanager and dashboard-alerting payload families are parsed in
    `aiops-datasources`** (Q9 (b)). Their parsing moves into a `signals` contribution of
    `aiops-datasources`: the row `signal-ingest-alert-webhook` moves there, and
    `aiops-base` keeps `signal-core` and `signal-ingest-generic-json`. `00:277` and
    `:278`, `03:252`, `06:38`, `08:283` and `:306` move the row.

**Edits made with this record.** `00` is edited in the commit that records the answer
(`08:996`), and that commit makes the edits the chosen options name: `00:267` (decisions
17, 21 and 23), `:277` and `:278` (decision 25) and `:651` (decision 19); `03:69` and
`:343` (decision 23) and `:252` (decision 25); `06:38` (decision 25); `07:37` and `:50`
(decisions 17 and 23) and `:520` (decision 19); `08:283` and `:306` (decision 25) and
`:1365` (decision 19); and harness:210 (decisions 17 and 23). On acceptance licence:210
and `08:1332` (L3) gain the keys and values of decisions 3 and 4, and the row of
`00:788` records this ADR as decided. The lines that restate an edited rule are edited
with it: the import rule of decisions 17 and 23 in `03:93` and `07:34`, and the U2 text
in `07:1505`, `08:1266` and harness:228; the stated exception of decision 21 in `07:520`
and harness:217; and the move of decision 25 in `01:156`, `05:80`, `:81`, `:87` and
`:223`, `08:254`, `:255` and `:271`, and `09:103`, with the scope line `08:301` and the
relationship row `08:321` of the figure whose element table holds `08:306`. Left to
later commits: the row of `08:1185`, with the commit that brings the ADR table of `08`
to the ADRs accepted on 2026-11-23; the findings of U1 in `00` §2.1, §5.4 and §5.6
(decision 22), with the commit that adds U1; and the mode table at licence:19, which
rates the five modes of licence:210 only, with the commit that writes the register
(decision 7). The amendment entry at `00:93` and the dated designs under `docs/design/`
other than the harness and licence records keep the text of their date. An option the
owner did not choose edits nothing.
