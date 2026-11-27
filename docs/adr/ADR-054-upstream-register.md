# ADR-054: The upstream rule: register, checks, exception list and base providers

- Status: **accepted 2026-11-23 by the project owner.** The owner answered all nine
  questions on the same date with the recommended option of each; the section "Owner
  answers of 2026-11-23" records them as decisions 17 to 25.
- S2b part: **proposed 2026-11-27, awaiting the project owner.** The section "2026-11-27
  proposed addition: the S2b part" at the end of this file holds it, as decisions 26 to
  43 and questions Q10 to Q12. No decision above changes before the owner accepts it.
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

   Owner word of 2026-11-26 on the names of the registry image the exit runs (ADR-060
   decision 34): the entry `distribution-registry` keeps the one name `docker-distribution`.
   `distribution` and `registry` are words the repository's own files carry, for the signed
   distribution manifest and for any OCI registry, and they are not added to the entry's
   `names`, so U1 does not look for them.

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
   | With the owner answer of 2026-11-26: the SPDX id `PostgreSQL` on the licence allow-list of gate check 6, an SPDX identifier of decision 21 | `postgresql` | `boot/` | S2a report, open question 7 |

   Note of 2026-11-26, on the owner's answer to open question 7 of the S2a report: the
   SPDX id `PostgreSQL`, which L7 allows, is an SPDX identifier of decision 21, and the
   register's item for it gives the token `postgresql` the place `boot/` alone, so that
   the allow-list of gate check 6 (`boot.Licensed`) holds every licence that L7 allows.
   `00:267` already names SPDX identifiers and is not edited. The database entry keeps its
   names, and its other token, `postgres`, stays refused in `boot/`.

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
    holds unchanged. Note of 2026-11-25: an importer may carry `tests: true`, and then
    names the test files of its directory only, for U2 and U1 alike, so a golden dump
    under it is no place of its names. The YAML library's importers are the list of
    ADR-060 decision 41 as the owner amended it that day: with `internal/vet`,
    `internal/packsuite` and, with `tests: true`, `cmd/aiopsctl`.

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
    Note of 2026-11-27: `00` §4.3 gains one more stated exception, gRPC and Protocol
    Buffers, the wire of the remote protocol, among the protocols with several
    implementations (ADR-063 decision 42). Its item in the table of decision 9, with
    its tokens and places, and the register entries of grpc-go and protobuf-go with
    their importers are the S2b part of this ADR (ADR-063 decision 34), accepted
    before the first import of either module. Decision 23 stands as written: the
    bundle `aiops-core` holds no row of the proxy (ADR-063 decision 41).

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

## 2026-11-27 proposed addition: the S2b part

- Status: **proposed 2026-11-27, awaiting the project owner.** Nothing of it is built.
  ADR-063 decision 2 has the S2b part of this ADR written as a dated addition, proposed
  to the owner before the code it governs and accepted before that code merges into
  `main`. Until it is accepted it blocks the first import of grpc-go or protobuf-go, and
  so all protocol code and all generated Go; the first Python dependency; and the
  extended lints and the register check in CI (ADR-063 decisions 35, 37 and 42).
- Scope: the five points that ADR-063 decision 34 gives this part, with the item that
  the note at decision 21 leaves to it: (1) the register entries and the importers of
  grpc-go and protobuf-go (ADR-063 decision 42) and the item of that exception in the
  table of decision 9; (2) the form of an entry for a Python package, an interpreter and
  a plugin base image; (3) the names of Python, of gRPC and of Protocol Buffers that the
  name lint refuses under `sdk/` and in a proto; (4) the reach of U1, U2 and U3 over
  `python/`, `.proto` imports and generated code (ADR-063 decision 37); (5) the checks
  over the dependency set of `aiops-sdk` (ADR-063 decision 52). It changes no decision
  above and no answer of ADR-063. A point that no written line fixes is a question
  below, and `00` is edited in the commit that records the answer (`08:999`).
- Citations: `00`, `07` and `08` are the files of `docs/architecture/` with those
  numbers, and `00:268` is line 268 of `00` at commit `bbfd839`; the sections above cite
  that line as `00:267`. "licence" and "harness" are
  `docs/design/licence-audit-2026-11.md` and `harness-and-upstreams-2026-11.md` at the
  same commit, where the schema line that the sections above cite as licence:210 is
  licence:222. A path with a line, as `tools/register/names.go:153`, is a file of the
  repository at that commit, and "register:278" is line 278 of
  `tools/upstream-components.yaml`. ADR-063 is cited by decision. A licence read for
  this part is cited as `module@version/FILE`; a reading that ADR-063 decision 35
  records is cited there, with its pin.

### Context of the S2b part

**What is decided.** The remote runtime is "out-of-process gRPC in any language"
(`00:44`; `07:1104`), and `00:268` lists "gRPC and Protocol Buffers, the wire of the
remote protocol" among the protocols with several implementations (ADR-063 decision 42).
The register gains entries for grpc-go and protobuf-go "whose importers are the remote
modules of decision 43, the proxy plugin, the adapter generator and the check runner of
`aiopsctl`", each written in the commit that first imports the module, and no definition
package imports either (ADR-063 decisions 42 and 44; `07:50`). The remote modules are
`sdk/remote` and `sdk/remotehost` (`00:263`). The generated Go of a `contrib` definition
lives under `sdk/remote/def/<name>/v<N>`, and its proto, which imports nothing, stays
beside the definition (ADR-063 decision 44; `07:28`; `08:1256`). The proto compiler's own
release generates both languages, with `protoc-gen-go` for the Go wire code, on the
condition that the release is read at its pin before its first use (ADR-063 decision 45).
The Python SDK `aiops-sdk` lives in `python/` (ADR-063 decision 18). Its dependency set
is held to L7, with PSF-2.0 recorded by name for `typing-extensions` and the MPL-2.0 file
under the condition of L8: "Each package has a register entry and a licence-record row,
and a check fails on a package of the set that has none". The owner's condition is that
the licence texts of the libraries bundled inside the two native extensions are read
before the first Python dependency merges (ADR-063 decision 52). U1 and U3 are extended
to `python/`, and U2 to the imports of `.proto` files and of the Python under `python/`,
each with a case that fails on a name or an import outside the exception (ADR-063
decision 37). Nothing merges unread, and what ADR-063 decision 35 read on 2026-11-27
prepares the entries and replaces none.

**What the register and the checks hold at `bbfd839`.**

- grpc-go and protobuf-go are in the build list of the root module at v1.82.1 and
  v1.36.11, as indirect requirements (`go.mod:102-103`), and in the register only in the
  `contains` of the entry `sigstore-go` (register:278-279). No Go file outside a
  `testdata` directory imports a package of either. The licence record rates both `ok`
  in one row that gives no version (licence:48) and holds the gRPC runtime of the Python
  SDK as `unverified`, read before the step that first uses it (licence:68; `08:1348`
  L13).
- The register check reads the package graphs of four modules: the root, `sdk`,
  `sdk/kernel` and `sdk/testkit` (`tools/register/main.go:47`). An entry has no key for a
  package of a language other than Go: only an item of `contains` names a `package`
  (`tools/register/main.go:98-122`, `:136`; register:105-107). An entry of form `process`
  needs a `role` unless its modes are only `tool` or `none`
  (`tools/register/main.go:220-223`). The L7 lists of the check hold no PSF-2.0
  (`tools/register/main.go:161-163`).
- U1 and U3 read every file and every path under `sdk/`, `boot/` and `loader/`, a
  `.proto` included, and nothing under `python/` (`tools/register/names.go:153-177`).
  The lint asks every entry for each of its names. A place is free for a name only
  through an item of `exceptions` that lists the token, or through the plugins and the
  importers of that same entry (`tools/register/names.go:31-49`, `:57-74`).
- U2 reads Go files only and enters no `testdata` directory
  (`internal/vet/imports.go:265`; `internal/vet/module.go:24`). A definition package
  imports the standard library, `sdk/kernel` and other definitions, and nothing else
  (`internal/vet/imports.go:195-198`).
- Run at `bbfd839` with the names of decision 34 added to an entry, the lint finds
  `grpc` in the Decision column of row 1 of `00` §2.1 (`00:44`); `protobuf` at
  `sdk/remote/handshake/handshake_test.go:6`; and `python` in a comment at
  `boot/digest_test.go:13`, in the fake of the `operations` definition
  (`sdk/def/operations/v0/fake/author.go:26`) and in its suite
  (`sdk/def/operations/v0/conformance/conformance.go:457`), where the word is the
  language of a template. With `proto`, `proto2` and `proto3` added as well, it finds
  the two files of `sdk/remote/handshake` by path and by line, the same cell of `00:44`,
  and the identifier `CheckRemoteProto` at `boot/gate.go:52` and `:83`,
  `boot/gate_test.go:271` and `boot/checks_test.go:566`, where the part abbreviates
  "protocol".
- The repository holds four Python scripts, none under a place of U1 and none run by a
  CI job (`scripts/diagrams/build_page.py`, `test/differential/oracle/acquire.py` and
  two files under `test/differential/corpus/`), and the register has no entry for an
  interpreter.

**Gaps.**

- The exception stands in `00:268` and has no row in the table of decision 9 and no
  item under `exceptions`, so no check reads a place for it (decision 26).
- ADR-063 decision 42 names the importers by what they are. The register needs
  directories, and three of the four do not exist yet (decision 27).
- One entry's `contains` pins the two modules today, and two new Go modules will each
  select a version of them (decision 28).
- The schema has keys for Go modules, images and content, and none for a Python package;
  L3 and licence record §7.2 list the keys (`08:1338`; licence:222) (decisions 29 to
  33).
- Two entries will carry the name `grpc` and two the name `protobuf`, with different
  importers (decisions 26 and 34).
- The Python name stands in a fake and a suite, which no list of importers may hold
  (`00:268`) (question Q10).
- `proto` is a word of the repository's own files (question Q11).
- "A check fails on a package of the set that has none" names neither the file that
  states the set nor what the check reads of the licence record (decisions 40 and 41;
  question Q12).

### Decision of the S2b part

#### The item of the exception

26. **The table of decision 9 gains one row**, and `exceptions` in the register one item
    with the same content, in the commit that first imports either module:

    | Item | Tokens | Where they may appear | Source |
    |---|---|---|---|
    | gRPC and Protocol Buffers, the wire of the remote protocol, among the protocols with several implementations | `grpc`, `protobuf`, `protocol-buffers` | `sdk/remote/` and `sdk/remotehost/`; the directory of the proxy plugin, of the adapter generator and of the check runner of `aiopsctl` (decision 27); `python/`; row 1 of `00` §2.1, where the token names the contract; ids, as decision 19 gives every item of the list of `00:268` | `00:44`, `:268`; ADR-063 decisions 18, 42 and 43; decisions 19 and 22 |

    The tokens stand nowhere else: not in `sdk/kernel`, `sdk/plugin`, `sdk/testkit`,
    `boot/` or `loader/`, and not in a definition package, its proto, its fake or its
    suite. No definition package imports either module, and a list of places never
    holds a definition with its fake and suite (`00:268`; ADR-063 decisions 42 and 44).
    No id carries either token at S2b. The item is one of its own and not a part of the
    item of the eight protocols (register:746-749): its places are directories, on
    which the other tokens of that item have no claim. The places come from the item
    and not from the importers of each entry, because two entries carry `grpc` and two
    carry `protobuf` (decision 34), and the lint asks each entry in turn
    (`tools/register/names.go:31-49`).

#### The entries of grpc-go and protobuf-go

27. **Two entries, written in the commit that first imports either module** (ADR-063
    decisions 35 and 42; decision 16), each with a new reading if its pin has moved:

    | Key | `grpc-go` | `protobuf-go` | Source |
    |---|---|---|---|
    | `names` | Decision 34 | Decision 34 | decision 3 |
    | `form`, `behind` | `library`, `none` | The same | No definition declares the wire: the remote protocol is the module `sdk/remote` (`07:32`) |
    | `modules` | `google.golang.org/grpc` | `google.golang.org/protobuf` | ADR-063 decision 35 |
    | `pin` | The one version that the build list of every module of the repository selects; v1.82.1 at `bbfd839` | The same; v1.36.11 | decision 6; `go.mod:102-103` |
    | `licence` | Apache-2.0, in `google.golang.org/grpc@v1.82.1/LICENSE`; the module carries `NOTICE.txt`, "Copyright 2014 gRPC authors." | BSD-3-Clause, in `google.golang.org/protobuf@v1.36.11/LICENSE`, "Copyright (c) 2018 The Go Authors"; a patent grant in `PATENTS` | Read in those files on 2026-11-27; ADR-063 decision 35 |
    | `verdict` | `ok` | `ok` | licence:48; decision 7 |
    | `modes` | `link` | `link`, and `tool` for its command `protoc-gen-go` (decision 33) | licence:24, :26; ADR-063 decisions 35 and 45 |
    | `mark`, `default` | `needs-approval`, `false` | The same | decision 3; no shipped profile composes a proxy row (ADR-063 decisions 22 and 41) |
    | `importers` | The list below | The same list | ADR-063 decision 42 |
    | `contains` | `golang.org/x/net` v0.59.0, `golang.org/x/sys` v0.48.0, `golang.org/x/text` v0.43.0 and `google.golang.org/genproto/googleapis/rpc` v0.0.0-20260523011958-0a33c5d7ca68, the modules that its packages `grpc` and `credentials` reach in the build list of the root module at `bbfd839`, each with the licence that register:271-277 records | None: its packages reach no module of another project | decision 18 |

    The importers are those of ADR-063 decision 42 and no other, and each line names
    that decision:
    - `sdk/remote`: the remote protocol, its kernel side, and the generated Go of the
      handshake, of the protocol and of each `contrib` definition under
      `sdk/remote/def/` (ADR-063 decisions 9, 43 and 44);
    - `sdk/remotehost`: the plugin-side host (ADR-063 decision 43);
    - the directory of the proxy plugin `aiops/remote` under `plugins/` (`07:34`);
    - the directory of the adapter generator (ADR-063 decision 13);
    - the directory of the check runner of `aiopsctl`: the one package that drives a
      plugin process through the kernel side of the protocol for steps 4 and 5 of
      `plugin check` (ADR-063 decisions 9 and 17).

    Each of the last three directories is written into both entries by the commit in
    which a file of it first imports either module (decision 16). Nothing else imports
    either module. A test tree, an example, a template and the stub remote plugins of
    the failure matrix (ADR-063 decision 64) are no importers: they reach the wire
    through the packages of the two remote modules, and one that needs a direct import
    waits for an accepted decision that adds it (decision 17). The licence record gains
    a row for each module with its pin, in place of the two names in licence:48
    (decision 7). The two conditions that the entry `sigstore-go` records for the gRPC
    module, its `NOTICE.txt` in `THIRD_PARTY_LICENSES/` and the build tag `grpcnotrace`
    with the codec exemption at v1.82.1 (register:202, :204), hold for every binary
    that links the module and stay recorded there. A move of the pin reads that
    exemption again, which is the S2b part of ADR-044 (ADR-063 decision 34).

28. **One pin over every module of the repository.** The register check reads the
    package graph of each Go module of the repository, so its list of modules gains
    `sdk/remote` and `sdk/remotehost` when each gets its `go.mod`
    (`tools/register/main.go:47`; decision 6: "the build list of each module of the
    repository, tests included"). U7 then fails when the build list of any module selects
    another version of grpc-go or of protobuf-go than the entry's `pin`. The two lines
    stay in the `contains` of `sigstore-go` at the same version, as `golang.org/x/mod`
    stands in an entry of its own and in that `contains` (register:270, :410-414). The
    licence report job scans each artifact again on the packages that the direct import
    adds (ADR-063 decision 35; decision 12), and a module of another project that the
    import brings into a package graph is added to the `contains` of the entry that
    brings it in (decision 18).

#### The forms of an entry

29. **The entry gains two keys**, as an amendment of decision 3. U7 fails on either where
    its column "Required" says it is missing.

    | Key | Holds | Required | Source |
    |---|---|---|---|
    | `packages` | The names of the Python packages that the upstream publishes and that a pin file of `python/` holds (decision 40), each as the package index writes it | When a Python package is linked or run as a tool | ADR-063 decision 52 |
    | `imports` | What U2 matches in the two languages whose imports are not Go module paths: under `python`, the top-level module names that the entry's packages provide; under `proto`, the path prefixes of the `.proto` files that the upstream publishes | `python` with `packages`; `proto` when a `.proto` file of the repository imports a file of the upstream | ADR-063 decision 37 |

    A Python package "has an entry" (decision 5) when its name is in the `packages` of
    exactly one entry. A package of another language gets its form with the first one
    that lands (`08:680`).

30. **A Python package.** Each package of the dependency set has an entry of its own:
    ADR-063 decision 52 says "Each package", and it names `typing-extensions`, a package
    that the gRPC runtime requires. A package that another one requires is therefore an
    entry, not an item of `contains`, which is where decision 18 puts a Go module of
    another project.

    | Key | Value | Source |
    |---|---|---|
    | `id`, `names` | The package name in kebab-case; decision 34 | decision 3 |
    | `form`, `behind` | `library`, `none` | licence:222 |
    | `packages`, `imports` | The one package name; under `python`, its top-level modules | decision 29 |
    | `pin` | The version that the pin file selects | decision 6 |
    | `licence` | The expression as read in the licence files inside each file of the package at the pin, `A AND B` where one file holds several texts | decision 3 |
    | `modes` | `link` for a package of the dependency set, which a package of the project imports; `tool` for one that only builds, generates or checks | licence:24, :26; ADR-063 decision 52 |
    | `evidence` | One item for each file read: the file name with its tags, its SHA-256 and the files read inside it, the form of the table of ADR-063 decision 35 | ADR-063 decisions 27 and 35 |
    | `contains` | What the files of the package bundle from other projects: each library built into a native extension and each data file under another licence, with the version that the release bundles and its licence, each read by hand, with the reading in `evidence` | The owner's condition of ADR-063 decision 52 |
    | `conditions` | For MPL-2.0, one that cites L8; for PSF-2.0, on `typing-extensions` alone, one that cites L7 and says that the owner recorded the licence by name | `08:1342`, `:1343`; ADR-063 decision 52 |
    | `importers` | `python/`, or the one directory under it that imports the package, by ADR-063 decision 18 | decision 17 |
    | `mark`, `default` | `needs-approval`, `false` | decision 3 |

    What the entries hold as far as ADR-063 decision 35 read, at the pins it read; a
    moved pin is read again (decision 6):

    | Package and pin | `licence` as read | `conditions` and `contains` | Read before the entry merges |
    |---|---|---|---|
    | `grpcio` 1.85.4 | `Apache-2.0 AND BSD-3-Clause AND MPL-2.0`, the three texts of its one licence file | MPL-2.0 covers `roots.pem`: a condition that cites L8. `contains`: the nine libraries under `third_party/` of its sdist | The licence text of each of the nine at the bundled version |
    | `typing-extensions` 4.16.0 | `PSF-2.0` | A condition that cites L7: recorded by name | Nothing more |
    | `protobuf` 7.36.2 | `BSD-3-Clause` | `contains`: `utf8_range`; the headers of `upb` name the same BSD licence | The text that the headers of `utf8_range` name, which the sdist does not carry |
    | `pytest` 9.2.0 | `MIT` | None | The four packages it requires on CPython 3.12 (`iniconfig`, `packaging`, `pluggy`, `pygments`), each an entry |

    Reading 5 of question Q12 says which artifacts the condition that cites L8 binds.

31. **An interpreter.** One entry for each interpreter release that CI or the exit runs,
    since an entry has one `pin`; the exit runs CPython 3.12 on glibc, and ADR-041 fixes
    the supported versions (ADR-063 decisions 27 and 34). Its form is that of the entry
    `nodejs` (register:531-546): `form: process`, `behind: none`, `pin` the release
    version, `licence` the expression over the complete licence file of that release,
    and `evidence` that names the archive with its checksum, or the image with its
    digest in `images`, and the file read in it. Its modes are `tool`, for what CI, the
    Python job and the exit run, fetched by pinned version and checksum or by image
    digest (`08:1375`), and `user-installs`, for the interpreter that an author installs
    and for the four scripts that a contributor runs (decision 4; licence:29). It names
    no `role` (reading 3 of question Q12). Its names are in decision 34. A CI action
    that sets up the interpreter gets an entry like the three actions
    (register:632-669), pinned by commit.

32. **A plugin base image.** The Python template names a base image by digest, read
    before the template merges (ADR-063 decisions 19 and 35), and the Go template names
    none. Its entry has `form: process`; `role: remote-plugin`, the container of `00`
    §3.1 that runs it (`00:138`), as the builder entries name `builder`, a container of
    the same table (register:41, :73); `images` with the reference by digest and, as its
    key, the name under which the build file of the template holds it; `pin` the tag or
    version; `licence` the expression over the packages of its layers as read; and
    `evidence` that names what was read inside the image at that digest, the form of
    the entry `distribution-registry` (register:614-630; `08:1348` L13). Its modes are
    `pull`, since a file of the project names the image by digest and the author's
    machine pulls it from its publisher, and `tool`, for the exit, whose pipeline
    fetches it by digest (licence:22, :26; ADR-063 decision 27). It carries a condition
    that cites L5: named by digest, never mirrored, and in no image that the project
    publishes (`08:1340`). If a release ever publishes an image built on it, the entry
    gains the mode `ship` with `sbom`, `source` and its layers in `contains` (decisions
    3 and 12). Reading 4 of question Q12 asks the owner to confirm the modes and the
    reach of L5.

33. **The generators.** `protoc-gen-go` is a command of the module of the entry
    `protobuf-go` and has no entry of its own: that entry carries the mode `tool`
    beside `link`, and the command runs at the entry's pin (ADR-063 decisions 35 and
    45). The proto compiler release has an entry of form `process` and mode `tool`,
    with the keys of the entry `nodejs`: `pin` the release version, `licence` the
    expression over the texts of the release, those of what its binary bundles
    included, and `evidence` that names the archive, its SHA-256 and the files read in
    it (`08:1375`). That reading is the owner's condition of ADR-063 decision 45 and is
    made before the entry merges; this part fixes the form only. Where a committed
    generated file carries the name of the compiler, the name stands there because the
    compiler's entry lists that directory as an importer, by this decision: `sdk/remote`,
    `python/` and the directory of the adapter generator. `protoc-gen-go` writes Go
    only, into `sdk/remote`, which its entry lists already (decision 27).

#### The names

34. **The `names` of each entry**, under the rule of decision 3, every name of the
    upstream project, in lowercase:

    | Entry | `names` |
    |---|---|
    | `grpc-go` | `grpc`, `grpc-go` |
    | `protobuf-go` | `protobuf`, `protocol-buffers`, `protobuf-go`, `protoc-gen-go` |
    | The gRPC runtime of the Python SDK, `grpcio` | `grpc`, `grpcio` |
    | The message runtime of the Python SDK, `protobuf` | `protobuf`, `protocol-buffers` |
    | `typing-extensions`, `pytest` and each other package of the dependency set | The package name |
    | An interpreter | `python`, `cpython` |
    | The proto compiler release; a plugin base image | Entered with the reading of each (decisions 32 and 33) |

    A name that is a word the repository's own files must carry goes to the owner
    before its entry merges (decision 8). Two do so here: `python` (question Q10) and
    `proto` with `proto2` and `proto3` (question Q11).

35. **What the name lint refuses.** With the item of decision 26 and these names:
    - `grpc`, `protobuf` and `protocol-buffers` fail in every place of decision 14
      outside the places of decision 26: in every definition package with its proto, in
      its fake and its suite (U3), in `sdk/kernel`, `sdk/plugin` and `sdk/testkit`, in
      `boot/` and `loader/`, in the schema of a row and in a golden dump of a shipped
      profile.
    - A proto beside a definition therefore names neither upstream. It holds its
      `syntax` line, its package, its messages and its service, and it imports nothing
      (ADR-063 decision 44). The frozen handshake file and the protos of the protocol
      lie under `sdk/remote/`, where the tokens may stand.
    - A name that one entry alone carries (`grpc-go`, `protobuf-go`, `protoc-gen-go`,
      `grpcio`, `typing-extensions`, `pytest` and the names of the other packages) fails
      outside the importers of that entry (decision 17).
    - `python` and `cpython` fail wherever the answer to question Q10 gives them no
      place.

    With the recommended answers to questions Q10 and Q11, one line of the repository
    fails at `bbfd839`: the comment at `boot/digest_test.go:13`, which the commit that
    adds the interpreter entry rewords, as decision 14 reworded its findings. `grpc` in
    `00:44` stands in row 1 of `00` §2.1 under the item, and `protobuf` in
    `sdk/remote/handshake/handshake_test.go:6` stands in an importer.

#### The reach of U1, U2 and U3

36. **U1 and U3 over `python/`.** Every file and every path under `python/` joins the
    places of decision 14, read as the files under `sdk/` are: a binary file by its
    path only (`tools/register/names.go:166-168`). A name stands there through the item
    of decision 26, through an importer of its entry, or as question Q10 decides for
    `python`. The pass is the one of today: a finding is U3 where the path is a fake or
    a suite of a definition, and U1 elsewhere (`tools/register/names.go:31-49`,
    `:90-94`). `python/` holds no fake and no suite of a definition at S2b: the fixture
    wraps the `aiopsctl` binary, which carries them, and there is one suite per
    definition, not one per language (`07:1212`, `:1246`). U3 there has the cases of its
    own test, and a directory named `fake` or `conformance` under `python/` is read as
    one under `sdk/def/` is.

37. **U2 over the Python under `python/`.** U2 reads the `import` and `from` statements
    of every Python file under `python/`, a generated one included. An import passes
    when it names a module of the standard library of the pinned interpreter, a module
    of `python/` itself, or a module under `imports.python` of an entry that lists the
    directory of the file among its importers. Any other import fails, an import of a
    package that has no entry included. A name that a statement computes when it runs
    is not read, and review holds it.

38. **U2 over the imports of `.proto` files.** U2 reads the `import` statements of every
    `.proto` file of the repository outside a `testdata` directory.
    - A `.proto` file under `sdk/def/` imports nothing (ADR-063 decision 44; `08:1256`).
    - `sdk/remote/handshake/handshake.proto` imports nothing: its freeze test lets the
      file differ only by fields added to `Hello`
      (`sdk/remote/handshake/handshake_test.go:20-23`; ADR-063 decision 6).
    - In any other `.proto` file, an import that names a `.proto` file of the
      repository passes where the import table of `07` §1.1 lets the directory of the
      importing file import the directory of the imported one (`07:28`, `:32`). An
      import that names no file of the repository names a file that an upstream
      publishes. It passes only when its path has a prefix under `imports.proto` of an
      entry that lists the directory of the importing file among its importers.

    The roots against which an import path is resolved are those that the adapter
    generator gives the compiler (ADR-063 decision 13); this part does not fix them.

39. **Generated code is read as written.** U1, U2 and U3 read a generated file where it
    is committed, as they read any file, and a header that marks it as generated exempts
    nothing. The generated Go of the handshake, of the protocol and of each `contrib`
    definition lies under `sdk/remote/`, an importer of both entries, and the generated
    Python under `python/` (ADR-063 decisions 13, 18 and 44). No generated wire type and
    no adapter lies under `sdk/def/`: there U2 refuses an import of either module
    (`internal/vet/imports.go:195-198`), and U1 and U3 refuse the names (decision 35). A
    golden file of the generator's own tests under a `testdata` directory is data that
    no Go build compiles and U2 does not read (`internal/vet/module.go:24`); U1 reads it
    where it lies under one of its places. Whether a committed file equals what the
    generator writes now is the parity check of ADR-063 decision 13, not a check of
    this ADR.

#### The checks over the dependency set of `aiops-sdk`

40. **The dependency set** is what an installer resolves for `aiops-sdk` with its
    testing part on an interpreter and a platform that ADR-041 supports: the packages
    that `aiops-sdk` needs to run, `pytest` for `aiops_sdk.testing` (`07:1212`; ADR-063
    decision 18), and every package that any of these requires, to the end. One pin file
    under `python/` states it, by package name, version and the SHA-256 of each file.
    ADR-041 fixes the pins and the form of that file (ADR-063 decision 34), and the exit
    installs from those files alone (ADR-063 decision 27). A package that only builds,
    generates or checks, as the build backend of `aiops-sdk` (ADR-063 decision 35), is
    pinned apart and is a tool. Reading 1 of question Q12 asks the owner to confirm the
    line between the two.

41. **The register check gains these clauses** (U7; ADR-063 decisions 37 and 52). It
    reads the pin files and the register and nothing on the network (decision 1). It
    fails when:
    - a package of the dependency set, or a tool package, is in the `packages` of no
      entry, or of more than one (decision 29);
    - a package of the dependency set has no row of the licence record. The row names
      the package in backticks in its first cell, the form that the check already reads
      for the components of `00` §3.2 (`tools/register/main.go:459-478`; reading 2 of
      question Q12);
    - the version that a pin file selects differs from the `pin` of the entry (decision
      6);
    - a pin file lists, for a package, the SHA-256 of a file that the `evidence` of its
      entry does not name: "another interpreter or platform enters the exit only after
      its files are read" (ADR-063 decision 27);
    - the entry of a package of the dependency set does not carry the mode `link`,
      which is what brings it under the L7 clause of decision 12; or an item of its
      `contains` carries no licence, or one outside L7, read as the entry's own licence
      is. No scan of the project reads a wheel, so this clause is the L7 control of what
      a package bundles;
    - an entry carries PSF-2.0 and its `packages` is not `typing-extensions` alone, or it
      has no condition that cites L7 (ADR-063 decision 52). MPL-2.0 keeps the clause of
      decision 12: a condition that cites L7 or L8.

    The clauses of decision 12 hold for these entries as for any other: an `unverified`
    entry fails, and so does an unknown key. L7 does not reach an entry whose only mode
    is `tool` (`08:1342`: shipped artifacts "are scanned separately from build tooling").

42. **What stays with the reading and with review.** The check cannot tell that a text
    was read. Before the first Python dependency merges, the licence text of each
    library bundled inside the two native extensions is read at the bundled version and
    recorded in `evidence` and `contains`; if a text that L7 denies until reviewed is
    found, the dependency does not merge and the owner is asked with that text (ADR-063
    decision 52). Review holds the reading and that the row of the licence record
    agrees with the entry (decisions 7 and 16). That the pinned set is closed is proven
    by the exit, whose environments reach the package directory and nothing else
    (ADR-063 decision 27).

#### Order

43. **When each part lands**, each with a case that fails on its base (ADR-063 decisions
    29 and 37):

    | Part | Lands with | Its failing case |
    |---|---|---|
    | The item of decision 26, the two entries, the two remote modules in the list of U7 | The commit that first imports either module | A token of the item in a definition package; an import of either module outside the importers; a remote module that selects another version than the pin |
    | The clause of U2 on `.proto` imports | The first `.proto` file after the handshake file | An import in a proto under `sdk/def/`; an import of an upstream's file outside the places of decision 27 |
    | The keys of decision 29, the entries of the Python packages, the clauses of decision 41, and U1, U2 and U3 over `python/` | The commit that brings the first Python dependency, after the readings of decision 42 | A package of the pin file with no entry, with no row, at another version, or with an unread file; a name and an import outside their places under `python/` |
    | The entry of an interpreter | The commit in which CI first runs it | A `process` entry of the modes `tool` and `user-installs` with no `role`, which passes |
    | The entry of the base image | The commit that adds the Python template | None of its own: the clauses of decision 12 |
    | The entry of the proto compiler release | The commit that first runs it, after its reading | None of its own |

    All of it runs where U1, U2, U3 and U7 run today: `make check` and the check job of
    CI (`Makefile:203`, `:212`; `ci.yml:142-175`). The working rule of decision 16 holds
    for a Python package as for a Go module: the commit that adds a package to a pin
    file adds its entry, its reading and its row.

### Consequences of the S2b part

- The first import of grpc-go or protobuf-go may merge once this part is accepted, with
  the item, the two entries and the wider list of U7 in the same commit. No definition
  package, and nothing of `boot/`, `loader/` or `sdk/kernel`, imports or names either.
- The importers of the two modules are a closed list of five directories. A test tree
  or a stub that needs a direct import needs the owner's word first.
- The register schema gains `packages` and `imports`. On acceptance licence:222 and
  `08:1338` (L3) gain the two keys, and harness:223 keeps naming the schema of licence
  record §7.2.
- On acceptance the table of decision 9 gains the row of decision 26, and the lines
  that give the reach of U1 to U3 name `python/` beside `sdk/`, and the imports of
  `.proto` files and of Python beside Go modules: `00:268`, `07:50`, `:1512`, `08:1272`
  and harness:210, :227-229. With question Q10 (a) or (b), `00:268`, `07:527` and
  harness:217 gain the Python item. Each edit lands in the commit that records the
  answer (`08:999`).
- `08:1342` (L7) and licence:214 gain PSF-2.0 for `typing-extensions`, by name, and
  licence:48, :60 and :68 give way to rows with pins, each in the commit that adds the
  entry (ADR-063 decision 52 and the edits it leaves to later commits).
- The first Python dependency stays blocked after acceptance until the readings of
  decision 42 are made, and the proto compiler until its release is read (ADR-063
  decisions 45 and 52).
- U7 reads six Go modules, the pin files of `python/` and the licence record. U1 and U3
  read `python/`. U2 reads Python and `.proto` imports. All of it is in `tools/` and
  the shared analyzer (decisions 1 and 13), and no production package of `sdk/kernel`,
  `boot/` or `loader/` changes.
- This part leaves three things to others. The entry of Compose comes with its reading
  (ADR-063 decisions 35 and 50), and its name is a word of the repository's own files,
  as in `boot/compose.go`, so it goes to the owner before that entry merges (decision
  8). The supported interpreter versions, the pins and the form of the pin file are
  ADR-041's. A library that derives a config schema, if one is taken, is the S2b part
  of ADR-048 and takes the form of decision 30.
- The row of this ADR in the ADR table of `08` §3 keeps "accepted 2026-11-23"
  (`08:1188`). On acceptance of this part, the status line at the head of this file and
  the note at decision 21 record it.

### Questions for the owner on the S2b part

Each question gives its options, the recommended one, why, and what the answer blocks.
An option that changes a written line says so and names the lines.

**Q10. Is the Python language a stated exception, and where?** `00:268` excepts "the Go
language, in which definitions are declared", and nothing of Python. Decision 3 keeps
every name of an upstream project in `names`, so the entry of an interpreter carries
`python`. The word stands in decided places: the directory `python/` (ADR-063 decision
18), and the fake and the suite of the `operations` definition, where it is the language
that `plugin new <registry> --lang python` names (`07:1213`, `:1556`;
`sdk/def/operations/v0/fake/author.go:26`;
`sdk/def/operations/v0/conformance/conformance.go:457`); the launch definition carries
the same field (`sdk/def/launch/v0/toolbox.go:295`). A list of importers never holds a
definition with its fake and suite (`00:268`), so only a stated exception can give the
word those places.
- (a) The Python language joins the Go language in the last stated exception of
  `00:268`: "the Go language, in which definitions are declared, the Python language, in
  which a remote plugin is written, and SPDX identifiers". The item of decision 9 gains
  the tokens `python` and `cpython`, allowed in every place. `07:527` and harness:217
  restate it. Nothing is reworded, and U1 never looks for the word.
- (b) `00:268` gains a stated exception with named places: the Python language, where
  it names the language of a plugin, of a template or of `aiops-sdk`. Decision 9 records
  the tokens `python` and `cpython` with the places `python/`, `sdk/def/operations/` and
  `sdk/def/launch/`, and never an id. `07:527` and harness:217 restate it. The comment at
  `boot/digest_test.go:13` is reworded. A later place is added by a note at decision 9.
- (c) No exception. The value of `--lang` and the directory take a word that is no name
  of the interpreter, which changes `07:1213`, `:1533` and `:1556`, the table of ADR-061
  decision 30 and ADR-063 decisions 18, 19 and 27.
- Recommended: (b). The exception is as wide as the decided lines need and no wider, so
  U1 still refuses the word in `sdk/kernel`, `boot/`, `loader/` and every id, where
  nothing needs it and the mark policy of its owner is unread. It costs one reworded
  comment. (a) is simpler and is what decision 21 did for Go, whose name every Go file
  carries; (c) changes written lines. Blocks: the entry of an interpreter, and so the
  first CI job that runs Python.

**Q11. Is `proto` a name of Protocol Buffers?** Decision 8 sends to the owner a name
that is a word the repository's own files must carry. The documents use the word as a
common noun for a schema file: "a definition, hook, proto, fake or suite" (`00:268`),
"its proto" (`07:28`), "The proto and adapter" (`00:44`). A proto file ends in `.proto`
and opens with `syntax = "proto3"` (`sdk/remote/handshake/handshake.proto:11`), and
`boot` holds the identifier `CheckRemoteProto`, where the part abbreviates "protocol"
(`boot/gate.go:52`).
- (a) Not a name. The entries carry `protobuf`, `protocol-buffers` and their own
  technical names (decision 34), and `proto`, `proto2` and `proto3` are words of the
  schema language that U1 does not look for, as the owner's word of 2026-11-26 kept
  `distribution` and `registry` out of the names of the registry image (decision 8).
- (b) Names, with the item of decision 26 giving the three tokens every place. U1 then
  never finds them.
- (c) Names, with the places of decision 26 and `sdk/def/` for the proto beside a
  definition. The identifier in `boot` is renamed, and the common noun leaves `00:44`.
- Recommended: (a). The word names a file kind in the documents and abbreviates
  "protocol" in `boot`, and the names that tell the upstream apart stay under the lint.
  Blocks: the entry `protobuf-go`, and so the first import.

**Q12. Five readings.** None changes what a line requires. Each fills a point that the
lines leave open, and each is in the decision that names it.
1. **The dependency set** (decision 40). ADR-063 decision 52 holds "the dependency set
   of `aiops-sdk`" to L7 and does not list it. Reading: it is what an installer resolves
   for `aiops-sdk` with its testing part, so `pytest` and what it requires are in it and
   are held to L7, and a package that only builds, generates or checks is a tool, which
   L7 does not reach (`08:1342`).
2. **"Has none"** (decision 41). ADR-063 decision 52: "Each package has a register
   entry and a licence-record row, and a check fails on a package of the set that has
   none." Reading: the check fails when either is missing, and it reads the row of the
   licence record by the package name in the first cell.
3. **The role of a program that the user installs** (decision 31). Decision 3 requires
   `role` of an entry of form `process`, and the check reads that an entry whose modes
   are only `tool` or `none` fills no container role (`tools/register/main.go:15-17`).
   Reading: the same holds for the mode `user-installs`, since nothing of the project
   ships or deploys such a program (licence:29).
4. **The base image of the Python template** (decision 32). No line gives the mode of
   an image that a template names. Reading: `pull` and `tool`, with the role
   `remote-plugin`; and the licence texts that L5 has charts and compose files ship
   (`08:1340`; licence:22) are asked of those two kinds of file, so the template names
   the image by digest and carries no licence text of it. The register entry holds the
   reading.
5. **L8 for a file that no artifact of the project holds** (decision 30). L8 gives
   every linked MPL-2.0 component one `NOTICE` line "in every artifact" (`08:1343`).
   The MPL-2.0 file of the gRPC runtime is inside that runtime's own package, which
   the installer fetches beside `aiops-sdk`, and the exit installs the two as separate
   packages (ADR-063 decision 27). Reading: the condition binds every artifact of the
   project that holds the file, and the package `aiops-sdk` holds none of its
   dependencies; the entry records the file, its licence and its source.
- (a) Accept the five readings, each landing with the code that builds it.
- (b) Answer each item on its own.
- Recommended: (a). Blocks: the first Python dependency (readings 1, 2 and 5), the
  entry of an interpreter (reading 3) and the Python template (reading 4).
