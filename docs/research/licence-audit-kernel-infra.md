# Licence audit: kernel and infrastructure dependencies

Read on 2026-11-11. Scope: the kernel's own dependencies and the infrastructure the plan deploys or tests with. The agent harness, the on-call provider, the observability backends, the agent and sensor, the LLM gateway and the dashboard server are audited in sibling notes.

This is engineering analysis, not legal advice. Section 6 lists the questions a lawyer must answer.

Method. Every licence below was read at the primary source today: the licence file at the HEAD commit of the default branch (`git ls-remote`, then `raw.githubusercontent.com/<repo>/<commit>/<file>`), cross-checked for eleven repositories against the GitHub repository API (`spdx_id`, `archived`), and for vendor terms the vendor's own page. Commits are given as the first 12 hex digits. The earlier research notes were used as leads only. Anything not read today is marked UNVERIFIED.

The five uses, used as column letters below:

- **C** community edition: an Apache-2.0 repository and release artifacts (binary, images, chart, compose file) that link, bundle or pull the upstream.
- **H** our hosted multi-tenant service running the upstream unmodified.
- **E** a later commercial enterprise edition on the same core, with closed plugins in the same binary or as separate processes.
- **P** third parties selling plugins that wrap or call the upstream.
- **N** the upstream's name in a plugin id or description.

## 1. Result

No open-source component of this group forces a rebuild from scratch. Three things are excluded outright, and the plan already excludes all three: MinIO (replaced by SeaweedFS or a customer endpoint, DR-15), River Pro and the SeaweedFS Enterprise Edition (L9). The audit confirms those exclusions against today's sources and adds conditions the plan does not yet state:

1. **The Cordis pin is the DeepSeek republication, not an upstream release.** Version 4.1.0 of Cordis and 1.1.0 of the loader exist only as `@deepseek-ai/cordis` and `@deepseek-ai/cordis-plugin-loader`, published from `vendor/cordis` and `vendor/loader` of the harness repository. Upstream `cordis` on npm is at 4.0.0-rc.12 and the upstream loader at 1.0.0-rc.9. Both vendored directories carry `MIT, Copyright (c) 2021-present Shigma`; the harness repository itself is `MIT, Copyright (c) 2026 DeepSeek`. L12 must name the exact source (repository, directory, commit) and carry the Shigma notice for the ported code, plus the DeepSeek notice for anything ported from harness-authored files.
2. **River's SQL migrations are MPL-2.0 files.** Run them through River's own migration package. Copying them into our migration tree puts MPL-2.0 files in the Apache-2.0 repository and makes every later edit a published MPL-2.0 modification.
3. **cosign as a linked library is not a small dependency.** `sigstore/cosign/v3` has a `go.mod` of about 300 lines, with ten HashiCorp modules among the indirect ones (for example `vault/api`, `go-retryablehttp`, `go-multierror`, `hcl`). HashiCorp libraries are MPL-2.0 by convention; the licence of each of the ten is UNVERIFIED today. Either run the unmodified cosign binary as a tool, or link the narrower `sigstore-go` for verification, or accept the extra L8 source lines. Decide in ADR-020.
4. **An offline bundle is distribution.** A chart or compose file that pins an upstream image by digest distributes a pointer. A mirror, an air-gapped tarball or an image of ours that contains the upstream image distributes the image and everything in its base layer. On-prem first makes the offline bundle likely, so it needs an image-level SBOM and a source offer for copyleft base-layer packages (L5 already says this for the GPL-2.0 BPF objects and the AGPL services; it applies equally to the PostgreSQL, gateway and object-store images).
5. **Plugin ids that contain a mark are a naming question, not a licence question.** No licence in this group grants trademark rights. The PostgreSQL policy requires prior approval for a product name that contains the mark or a variant. The Linux Foundation policy allows "<own name> plug-in for <mark>" and rejects a mark inside a product name or hyphenated. First-party ids such as `sql-postgres`, `events-nats` and `blob-s3` are technical identifiers; whether a marketplace listing turns an id into a product name is a lawyer question (section 6).
6. **L13 can be closed for two entries.** Envoy and connect-go are Apache-2.0 at today's HEAD.

## 2. Mechanics for an Apache-2.0 core with a later commercial edition

### 2.1 Four reaches of copyleft, and the licences that are not open source

| Reach | Licences | What triggers it | What it covers | Effect on this product |
|---|---|---|---|---|
| None (permissive) | Apache-2.0, MIT, BSD-2-Clause, BSD-3-Clause, ISC, PostgreSQL, Zlib, 0BSD | Distribution of source or binary | Keep the copyright and licence text; Apache-2.0 adds the NOTICE file (section 4(d)), a statement of changes in modified files, and a patent grant that ends for a licensee who sues | All five uses allowed. The only work is `THIRD_PARTY_LICENSES/` and the aggregated NOTICE |
| File level | MPL-2.0 | Distribution of the covered files in source or executable form | Only files that contain MPL code. A new file with no MPL code is not a Modification (section 1.10; FAQ Q11). An executable may carry any licence that does not limit the recipient's rights in the MPL source (section 3.2(b)), and the distributor tells recipients where the source is (section 3.2(a)) | Linking into the Apache-2.0 binary and into a closed enterprise binary is allowed (section 3.3 Larger Work; FAQ Q11 says static linking into proprietary software is allowed). Hosted use distributes nothing (FAQ Q5, Q17) |
| Library level | LGPL-2.1, LGPL-3.0 | Distribution of a binary that links the library | The library, plus a duty to let the recipient relink against a changed library | A static Go binary can meet this only by shipping source or relinkable objects of everything else. A closed enterprise binary cannot, so LGPL stays off the list (L7) |
| Whole work | GPL-2.0, GPL-3.0 | Distribution of a binary that links the code | The entire linked binary | Blocks E in process. Apache-2.0 code may be combined into a GPL-3.0 work but not a GPL-2.0-only one. Acceptable only as a separate program we do not link |
| Network | AGPL-3.0; SSPL-1.0 (not an OSI licence) | AGPL section 13: users interacting with a modified version over a network. SSPL: offering the program as a service | AGPL: source of the modified program to its network users. SSPL: the whole service stack | AGPL code linked into the kernel would put the kernel, and closed plugins in the same binary, under section 13 in H. As an unmodified separate service it creates no duty for our code, but redistribution of its image carries AGPL section 6. In this group only MinIO is AGPL |
| Use restricted (source available) | BUSL-1.1, Elastic-2.0, RSAL, FSL, PolyForm, Commons Clause, vendor EULAs | Use itself, usually hosting or competing | Whatever the text says | Blocks H or E by design. River Pro and the SeaweedFS Enterprise Edition are of this kind |

### 2.2 Static linking in Go

A Go build compiles every package it imports from every module in the build list into one executable. Shipping that executable distributes all of them in executable form, so:

- The binary inherits the union of the obligations of the modules whose packages are compiled in, not of every line in `go.mod`. The licence scan must run on the package graph of the built binary (`go list -deps`), which is what L7 and L16 specify, and not on the manifest's claim.
- Inlining and generics do not change the analysis for MPL-2.0. The unit is the source file. What changes it is copying: a snippet of an MPL file pasted into one of our files makes that file a file "containing MPLed code" (FAQ Q12). Rule: no copy from River, go-plugin or yamux into our source. Implementing their interfaces and embedding their types in our own files is not a Modification.
- A `replace` directive that selects modified MPL source creates Modifications in the changed files. Those files stay MPL-2.0 and their source is published whenever a binary that contains them is distributed, the enterprise binary included. Patches go upstream or their exact modified source is made available under MPL-2.0.
- `go:embed` content is shipped content. Embedded SQL, rule files and templates follow their own licences and enter the same scan.
- cgo is off in the builder, so no C library enters the binary and LGPL system libraries are not in play.

### 2.3 What a distribution binary assembled from third-party plugin modules inherits

`aiopsctl dist build` produces one binary from the release and the in-process plugins a site selects. For licence purposes:

- **Whoever distributes the binary is the distributor.** A site that builds for its own use distributes nothing and owes nothing under MPL, GPL or LGPL. A site, a reseller or we ourselves who hand that binary to someone else owe everything every linked module requires.
- **One strong-copyleft plugin sets the terms for the whole binary.** A GPL-3.0 in-process plugin makes the distributed binary a GPL-3.0 work, which is lawful for an all-open build and impossible for a build that also links a closed plugin. An AGPL in-process plugin adds the network clause for the operator.
- **Closed in-process plugins need a redistribution right.** A paid plugin linked into a binary that the buyer then passes on (a managed-service provider, an integrator) needs a licence that allows that redistribution. The marketplace terms must say whether a purchased in-process plugin may be redistributed inside a built distribution.
- **Remote plugins are separate programs.** A plugin that runs as its own process and speaks the remote protocol is not linked with the kernel; each side keeps its own licence. The remote SDK that the plugin links is Apache-2.0, which a closed plugin and a GPL-3.0 plugin can both link; a GPL-2.0-only plugin cannot.
- **Container images are aggregates.** An image with our binary and a base layer distributes both. A static binary on a scratch or distroless-static base keeps the image's obligations equal to the binary's.

### 2.4 What the in-process allow-list should contain

L7 is correct as written. Proposed additions, each a small change to the same rule:

| Topic | Rule |
|---|---|
| Allowed without conditions | Apache-2.0, MIT, BSD-2-Clause, BSD-3-Clause, ISC, 0BSD, Zlib, PostgreSQL, Unicode-DFS-2016, Unicode-3.0 (the current id for Unicode data), BlueOak-1.0.0 |
| Allowed with a recorded condition | MPL-2.0: unmodified, or every modified file published; one source line per module in the notices (L8). CC0-1.0: data and content only, because it carries no patent grant |
| Denied until reviewed and recorded by name | GPL, LGPL, AGPL (any version); EPL-1.0, EPL-2.0, CDDL, EUPL (weak copyleft with their own source duties); SSPL-1.0, BUSL-1.1, FSL-1.1, Elastic-2.0, RSAL, PolyForm, Commons Clause; BSD-4-Clause; the JSON licence; CC-BY-SA, CC-BY-NC and CC-BY-ND; any custom text; no licence |
| SPDX expressions | `A OR B` passes when one side is allowed, and the notices record the side taken. `A AND B` passes only when both are allowed |
| Granularity | Detect per package directory, not per module root. Open-core modules keep a permissive root licence and a second licence under `ee/` or `enterprise/` (L9) |
| Enterprise builds | The same list. The enterprise EULA must exclude third-party components from its own restrictions, so that it does not limit a recipient's MPL-2.0 rights in River's source (MPL section 3.2(b)) |
| Closed plugins | A `LicenseRef-` id is acceptable only outside `aiops`, `aiops-contrib` and the project index, and only with an entry in `acceptLicenses`; the built distribution then cannot be redistributed without the plugin owner's permission, and boot prints that |

### 2.5 Using a service, shipping its image, linking its code, copying its content

| Mode | Example in the plan | Copyright consequence |
|---|---|---|
| Called over the network, run by the customer | A customer's S3-compatible endpoint; a customer's PostgreSQL | None for us. The customer's licence with its vendor governs |
| Unmodified separate process, image pulled from upstream | Envoy, PostgreSQL, SeaweedFS in the chart and compose file | We distribute configuration and a reference. No copyleft reaches our code |
| Unmodified image inside a mirror or offline bundle of ours | An air-gapped install tarball | We distribute the image: licence texts, NOTICE files, and source offers for copyleft packages in the base layer |
| Linked into our binary | pgx, River, connect-go, the OpenTelemetry SDK | Section 2.2 |
| Content copied into our repository | Protobuf definitions, SQL migrations, vendored charts, the Cordis oracle | The copied files keep their licence and notice. Apache-2.0 and MIT files may sit in the repository with their headers; MPL-2.0 files stay MPL-2.0; a port is a derivative and keeps the upstream notice |
| Build or test tool only | Jena, kind, Chaos Mesh, test containers | Nothing reaches a shipped artifact, provided the tool is fetched and never baked into an image of ours (section 5.3 of file 08) |

## 3. Components

Verdicts: `ok`, `ok-with-conditions`, `optional-plugin-only`, `replace-or-rebuild`, `unverified`. "Not named" means the scope of this audit lists the component and the plan's records do not.

| Component | Role in the plan | Use mode | Licence verified today | Source (read 2026-11-11) | Verdict |
|---|---|---|---|---|---|
| PostgreSQL 16+ | `kernel-db` in every profile; the only SQL engine | Separate process, unmodified; image pulled from upstream or run by the customer | PostgreSQL | `github.com/postgres/postgres` `COPYRIGHT` at `35def3385998`; `postgresql.org/about/licence/` | ok-with-conditions |
| pgx v5 | `sql-postgres` driver; in the require list of `sdk/def` | Linked | MIT | `github.com/jackc/pgx` `LICENSE` at `21fbf266e84f` | ok |
| River (open source), v0.51.x at HEAD | `jobs-river`: jobs, periodic jobs, uniqueness, leader election | Linked, unmodified | MPL-2.0 | `github.com/riverqueue/river` `LICENSE` at `008bd4ee364e`; API `spdx_id` MPL-2.0 | ok-with-conditions |
| River Pro | Not used; named only to exclude it (L9) | Would be linked | Proprietary EULA, subscription | `riverqueue.com/pro`, `riverqueue.com/pro/license` (EULA revised 2025-03-27) | replace-or-rebuild |
| connect-go | `http-server` and `fleet-agentapi` | Linked | Apache-2.0 | `github.com/connectrpc/connect-go` `LICENSE` at `297fdd604405` | ok |
| alloy-remote-config protobufs | The `collector.v1.CollectorService` definition that `fleet` implements | Linked (generated Go code), or `.proto` content copied | Apache-2.0 | `github.com/grafana/alloy-remote-config` `LICENSE` at `39a09f328602` | ok-with-conditions |
| shepherd | Conformance oracle for `fleet` | Test tool only | Apache-2.0 | `github.com/procoduck/shepherd` `LICENSE` at `7554ba50898e` | ok |
| SeaweedFS (open source) | Bundled `object-store` in `self-host-full` and `dev` | Separate process, unmodified; image pulled from upstream | Apache-2.0 | `github.com/seaweedfs/seaweedfs` `LICENSE` at `9799b82edfc0`; API `spdx_id` Apache-2.0, not archived | ok-with-conditions |
| SeaweedFS Enterprise Edition | Not used; excluded by L9 | Would be a licensed separate process | Proprietary, capacity licence | `seaweedfs.com/docs/pricing/`; README section "Enterprise" at `9799b82edfc0`; EULA text UNVERIFIED | replace-or-rebuild |
| MinIO | Was the first object-store choice; now disabled everywhere (DR-15, L15) | Would be a separate process | AGPL-3.0; repository archived, README "THIS REPOSITORY IS NO LONGER MAINTAINED" | `github.com/minio/minio` `LICENSE` and `README.md` at `7aac2a2c5b7c`; API `archived: true`, last push 2026-04-24 | replace-or-rebuild |
| NATS server and `nats.go` | `events-nats`, an optional hosted-only relay on ADR-007 | Server: separate process. Client: linked | Apache-2.0 (both) | `github.com/nats-io/nats-server` at `0e5b4f72ac27`; `github.com/nats-io/nats.go` at `4880fb84e056` | ok |
| Apache Kafka | `ingest-log` for the metrics and traces backends in hosted cells; never a kernel dependency | Separate process, unmodified | Apache-2.0 | `github.com/apache/kafka` `LICENSE` at `7f81cdf3fa59` | ok |
| franz-go | Not named. Candidate client for the optional Kafka export sink | Would be linked | BSD-3-Clause | `github.com/twmb/franz-go` `LICENSE` at `d6ea02a174bc` | ok |
| cosign | Signing and verifying plugin artifacts, distributions and the index (ADR-020) | Undecided: run as a tool, or linked | Apache-2.0 | `github.com/sigstore/cosign` `LICENSE` and `go.mod` at `c8a4781a3fbc` (module `cosign/v3`) | ok-with-conditions |
| ORAS (`oras-go`, `oras`) | Not named. Candidate OCI client for `plugin push` and pull by digest | Would be linked | Apache-2.0 | `github.com/oras-project/oras-go` at `7b46bab85e19`; `oras` at `02ffd9d895cf` | ok |
| wazero | Not named. The plan refuses a WebAssembly runtime before a consumer exists | Would be linked | Apache-2.0 | `github.com/wazero/wazero` (moved from `tetratelabs/wazero`) `LICENSE` at `ef2dd2f85376` | ok |
| Extism (`extism`, `go-sdk`) | Not named. Same refusal | Would be linked | BSD-3-Clause | `github.com/extism/extism` at `051b765bb6ab`; `extism/go-sdk` at `de6f8ac95721` | ok |
| Apache Jena command line | CI parse check of the vocabulary file (ADR-029) | Build tool only, fetched in CI | Apache-2.0 | `github.com/apache/jena` `LICENSE` at `3dbd5b9775f5` | ok |
| kind | The lab cluster | Lab tool only | Apache-2.0 | `github.com/kubernetes-sigs/kind` `LICENSE` at `46b5d280a425` | ok |
| k3s | Not named | Would be a lab or test tool | Apache-2.0 for the repository; licences of bundled components UNVERIFIED | `github.com/k3s-io/k3s` `LICENSE` at `51d3b7043183` | ok-with-conditions |
| Chaos Mesh and Chaosd | The one fault engine and its host daemon (ADR-027) | Lab tool only | Apache-2.0 (both) | `github.com/chaos-mesh/chaos-mesh` at `8e5a702ee41f`; `chaos-mesh/chaosd` at `f6e3df46648d` | ok |
| testcontainers-go | Not named; the plan says "a PostgreSQL test container" | Test dependency only | MIT | `github.com/testcontainers/testcontainers-go` `LICENSE` at `f31e98d4ee9b` | ok |
| Embedded PostgreSQL for tests (`fergusstrange/embedded-postgres`) | `sdk/testkit/sqlfake` on laptops with no container runtime | Test dependency only; downloads PostgreSQL binaries at test time | MIT (wrapper); Apache-2.0 (`zonkyio/embedded-postgres-binaries` packaging); PostgreSQL (the binaries) | `github.com/fergusstrange/embedded-postgres` at `b505714f34b4`; `zonkyio/embedded-postgres-binaries` at `c56022003f8b` | ok |
| OpenTelemetry Go SDK | Not named as a library; the kernel's own telemetry would use it | Linked | Apache-2.0 (the file also carries a BSD-3-Clause notice for code taken from Go) | `github.com/open-telemetry/opentelemetry-go` `LICENSE` at `0e6a8d77d3e3` | ok |
| Envoy | `gateway` in every profile that runs the backends | Separate process, unmodified; image pulled from upstream | Apache-2.0 | `github.com/envoyproxy/envoy` `LICENSE` at `363c84b1e964` | ok |
| Cordis 4.1.0 and cordis-plugin-loader 1.1.0 | The model `sdk/kernel` and `loader/` are ports of; vendored as the test oracle | Ported (a derivative work); vendored copy is test only | MIT, Copyright (c) 2021-present Shigma | `github.com/deepseek-ai/deepseek-harness` `vendor/cordis/LICENSE`, `vendor/loader/LICENSE`, `THIRD_PARTY_NOTICES.md` at `fcc0ad462172`; npm `@deepseek-ai/cordis@4.1.0` (2026-11-04); `github.com/cordiverse/cordis` `LICENSE` at `261c5fdae6ed` | ok-with-conditions |
| DeepSeek Harness | The design the plugin system follows | Design reference; no code taken | MIT, Copyright (c) 2026 DeepSeek | `github.com/deepseek-ai/deepseek-harness` `LICENSE` at `fcc0ad462172`; API `spdx_id` MIT | ok |
| go-plugin and yamux | Inside the dashboard app backend only, through the dashboard server's Go plugin SDK | Linked into the app backend binary, unmodified | MPL-2.0 (both), Copyright IBM Corp. | `github.com/hashicorp/go-plugin` `LICENSE` at `415e29b9562b`; `hashicorp/yamux` at `98ba9c9ac60f` | ok-with-conditions |
| client-go | Kubernetes API access from the kernel | Linked | Apache-2.0 | `github.com/kubernetes/client-go` `LICENSE` at `5297d7d6d360` | ok |
| slack-go v0.31.0 | `chat-surface-slack` | Linked | BSD-2-Clause | `github.com/slack-go/slack` `LICENSE` at `5d5455cb26de` | ok |
| protobuf-go and grpc-go | Remote plugin protocol; generated code | Linked | BSD-3-Clause; Apache-2.0 | `github.com/protocolbuffers/protobuf-go` at `e60e501a090c`; `github.com/grpc/grpc-go` at `68a36869f9f8` | ok |
| Go toolchain and standard library | The kernel is standard library only; the builder image pins the toolchain | Linked (standard library); tool (compiler) | BSD-3-Clause | `github.com/golang/go` `LICENSE` at `376eae6ac7e1` | ok |
| go-licenses | Generates `THIRD_PARTY_LICENSES/` (L7, "a tool such as") | Build tool only | Apache-2.0 | `github.com/google/go-licenses` `LICENSE` at `3e084b0caf71` | ok |
| Helm and Docker Compose | The user's install tools for our chart and compose file | Tools the user runs; we ship our own files | Apache-2.0 (both) | `github.com/helm/helm` at `6234f6228f31`; `github.com/docker/compose` at `20c7fa589655` | ok |
| S3 client library for `blob-s3` | The kernel's blob provider | Linked | UNVERIFIED: the plan names no library | none | unverified |
| JavaScript runtime for the differential oracle | Runs the vendored Cordis in CI | Build tool only | UNVERIFIED: the plan names none (file 08 section 5.3 defers it to S0) | none | unverified |
| Headless Chromium | `report-renderer-pdf`, a remote contribution, P3 | Separate process in a plugin image | UNVERIFIED (L13 already lists it) | none | unverified |

Not in this group and not audited here: Tetragon, the agent, the eBPF child, the backends, the dashboard server, HolmesGPT, GoAlert, LiteLLM, vLLM, Ollama, the rule-parity witness.

## 4. The five uses

### 4.1 Permissive and linked, or permissive and run as an unmodified separate process

This covers every row with verdict `ok`.

| Use | Condition |
|---|---|
| C | Linked: licence text and copyright notice in `THIRD_PARTY_LICENSES/`; for Apache-2.0 modules the upstream NOTICE text in the aggregated notices (section 4(d)). Separate process: pull by digest from upstream; ship the licence text beside the chart and compose file (L5). An offline bundle that contains the image also carries the image's own third-party notices |
| H | None beyond C. Nothing is distributed |
| E | Same as C. Closed plugins in the same binary are allowed |
| P | Same as C, on whoever distributes the plugin or the binary that contains it |
| N | No licence here grants a trademark right (Apache-2.0 section 6 says so in terms). Use the name to say what the plugin works with; section 4.3 |

### 4.2 Components with conditions

| Component | C | H | E | P | N |
|---|---|---|---|---|---|
| PostgreSQL | Pull the image from upstream; licence notice in the bundle. Which image the chart pins is UNVERIFIED | No condition | No condition; closed extensions and source modifications are allowed by the licence | No condition | "Postgres" and "PostgreSQL" are registered marks. Factual statements of compatibility are fair use. A product name that contains the mark or a variant needs prior approval from the mark owner |
| River (open source) | Unmodified; one NOTICE line with the source URL per module (MPL section 3.2(a)); licence text shipped; migrations run through River's own package, not copied | No obligation; nothing is distributed | Allowed in the same binary as closed plugins (section 3.3). The enterprise EULA must not restrict rights in River's source. Any patched River file is published under MPL-2.0 | A paid plugin should call the `jobs` definition, not River. If it imports River directly, the distributor of the binary has the same duties | No trademark grant (section 2.3). No trademark policy found: UNVERIFIED |
| alloy-remote-config protobufs | Import the module, or copy the `.proto` files with their licence and a NOTICE line. No file from an AGPL directory (L2) | No condition | No condition | A third-party fleet plugin may do the same | The protocol and package names stay as upstream wrote them on the wire; our own route and row names carry no mark (A5 already does this) |
| SeaweedFS (open source) | Pin the image built from the Apache-2.0 repository, by digest; no licence key; no enterprise flag or feature; ship the licence text | Not used in hosted (an S3 bucket) | Same as C. An enterprise plugin may not require an Enterprise Edition feature | A third-party blob plugin may target it; the vendor's EULA binds only users of the Enterprise Edition | No trademark policy found: UNVERIFIED |
| cosign | As a tool: fetched, never baked in. As a library: every MPL-2.0 module that the build actually compiles in (the HashiCorp ones are the candidates) joins the L8 list | Signing with a key has no third-party terms. Keyless signing uses the public Fulcio and Rekor instances, whose terms of use are UNVERIFIED | Same as C | Publishers sign with their own keys; no condition on us | "sigstore" and "cosign" are Linux Foundation project marks; section 4.3 |
| k3s | Lab or test only; never in a release asset, a mirror or an image of ours | Not applicable | Not applicable | Not applicable | Not applicable |
| Cordis and its loader | `NOTICE` and the headers of `sdk/kernel` and `loader/` carry `Copyright (c) 2021-present Shigma` and the MIT permission notice, and name the source: `deepseek-ai/deepseek-harness`, `vendor/cordis` and `vendor/loader`, with the commit that carries 4.1.0 and 1.1.0. The vendored oracle keeps its files unchanged and enters no artifact | No extra condition | The MIT notice travels with the closed binary too | A third-party plugin links the SDK; the notice is already in the SDK's licence files | Say "a Go port of the Cordis model". Do not name the product, a module or a plugin after Cordis or after the harness |
| go-plugin and yamux | Unmodified; NOTICE line with the source URL; they are linked only into the app backend binary, so the lines belong in that artifact | No obligation | Same as River | Same as River | Not used in any name |

### 4.3 Naming rule that satisfies the policies read today

The Linux Foundation policy (which covers NATS, Envoy, Kubernetes, sigstore, Chaos Mesh) allows "<own name> plug-in for <mark>" and "<own name> compatible with <mark>", and rejects a mark as part of a product name, a mark in a domain name, and a mark that is hyphenated or abbreviated. The ASF policy (Kafka, Jena) forbids its marks in the primary or secondary branding of a third-party product and asks for the "Apache Kafka" form in text. The PostgreSQL policy requires prior approval for a product, company or domain name that contains the mark or a variant.

Rule T7 of file 08 already gives the safe form for titles: "<own name> connector for <System>", the mark once and after "for", no logo, a non-affiliation sentence. What T7 leaves open is the id itself (`events-nats`, `sql-postgres`, `blob-s3`). Recommendation: keep ids as lowercase technical identifiers, never show an id as the listing title in the index or the marketplace, always show the title form, and add the owner's attribution sentence where its policy gives one (PostgreSQL does). Whether that is enough is question 6 in section 6.

## 5. Reasoning for every verdict that is not plain ok

**PostgreSQL: ok-with-conditions.** The licence allows every use, and the project states it has no plan to change it. The condition is the name. The policy text read today says a product name that contains "Postgres" or "PostgreSQL" needs prior approval, and that the marks are not to be used on products that are not PostgreSQL or derived from it. A first-party provider id is low risk; a third party selling "Something Postgres" in a marketplace we run is the case to settle before the marketplace opens. Second condition: an offline bundle that contains a PostgreSQL image distributes its base layer.

**River (open source): ok-with-conditions.** MPL-2.0 is file-level. Linking it unmodified into the Apache-2.0 binary and later into an enterprise binary with closed plugins is allowed, and hosted use creates no duty. Conditions: unmodified or published modifications; the source pointer in every artifact that contains it; no copying of River files (migrations included) into our tree; an enterprise EULA that leaves River's source rights alone. Risks: one vendor (River Software, LLC) with a paid tier that holds the features a multi-tenant product wants (per-queue concurrency limits, workflows, sequences, a dead letter queue, durable periodic jobs, batching, encrypted jobs), so those will not arrive in the open edition and are ours to build behind `jobs`; the plan's own token bucket already covers the first. The vendor's page says "River is open-source software and always will be"; a future relicence would not reach released versions, and the `jobs` definition with its fake and conformance suite keeps a second provider possible. Whether contributors sign a contributor agreement is UNVERIFIED (no such file at HEAD).

**River Pro: replace-or-rebuild.** The EULA is proprietary. It forbids distribution "as part of a product, appliance or virtual server" without a separate Appliance License, forbids "time sharing, hosting, service provider or like purposes", forbids redistribution to a server not under the licensee's control, and counts developers per seat. That blocks C (it cannot be in an Apache-2.0 release at all), P, and E without a negotiated Appliance License; H is at least doubtful under the hosting clause. What we build instead: the kernel's per-tenant token bucket for fairness, and `events-nats` as a work queue when the starvation test of ADR-007 fails. This confirms L9 and adds the reason.

**alloy-remote-config protobufs: ok-with-conditions.** Apache-2.0, and its README says the code "can be used to implement a remote configuration server". The conditions are the two ways to use it: importing the generated module is plain linking; copying the `.proto` files is content copied into our repository and needs the licence text and a NOTICE line. The wire names are the upstream's and stay as they are; names we choose carry no mark.

**SeaweedFS (open source): ok-with-conditions.** Apache-2.0 at HEAD, active (last push today). Conditions come from the open-core split, not from the licence: the vendor sells an Enterprise Edition under a capacity licence whose page says a licence is "Required for production use" and that less than 25 TB is free only for development and testing. That sentence is about the Enterprise Edition; the Apache-2.0 edition has no such limit. A third-party review says the upgrade is a licence file applied to an existing cluster, which, if true, means enterprise code paths may ship in or beside the open build: UNVERIFIED, and the reason the release check must pin the open image by digest and fail on a licence file or an enterprise flag. Risks: one principal maintainer; the features the vendor gates (point-in-time recovery, automatic erasure-coding repair, bitrot scrub, customizable erasure coding, OIDC for the admin UI) include some an operator will want, so `self-host-full` must document a customer-provided S3-compatible endpoint as the equal path, which DR-15 already does.

**SeaweedFS Enterprise Edition: replace-or-rebuild.** Proprietary and priced per terabyte. It cannot be bundled in C, and making any row need it would make the community edition depend on a paid product (L9). Nothing is built in its place: the `blob` definition has one provider that speaks the S3 API, and durability features are the operator's choice of store.

**MinIO: replace-or-rebuild.** AGPL-3.0, and the repository is archived with the notice "THIS REPOSITORY IS NO LONGER MAINTAINED"; the alternatives it names are the vendor's AIStor Free ("free license", terms UNVERIFIED) and AIStor Enterprise. It blocks C as a default bundle on two grounds: redistributing the image in an offline bundle carries AGPL section 6, and an unmaintained store gets no security fixes. It could never be linked (E). The plan's replacement stands: a customer endpoint or the bundled open-source SeaweedFS, with every bundled MinIO subchart disabled and a release check that fails on a MinIO image (L15). A user who already runs MinIO or AIStor points `blob-s3` at it as a customer endpoint, which is use over the network and needs nothing from us.

**cosign: ok-with-conditions.** Apache-2.0. The conditions are about how it is used. Linked, it brings a large graph in which HashiCorp modules appear (MPL-2.0 by convention, each UNVERIFIED today); run as a tool, it brings none. Keyless mode depends on public infrastructure whose terms of use could not be found today (an open upstream issue asks the same question), and the plan already records that keyless verification at an air-gapped site is unconfirmed. Key mode has neither problem.

**k3s: ok-with-conditions.** The repository is Apache-2.0, but a k3s release is a bundle of other projects under their own licences, some of them copyleft userland: UNVERIFIED in detail today. The plan does not name it. If the lab or a test uses it, it is fetched and run, and never redistributed in an asset of ours.

**Cordis and its loader: ok-with-conditions.** MIT permits the port and every later use. A port is a derivative, so the copyright and permission notice must travel with `sdk/kernel` and `loader/` in source and in every binary, the enterprise one included. The finding of this audit is the provenance: the pinned versions are the DeepSeek republication, so the notice must point there and the pin must be a commit, because the directory is vendored source in a repository that says of itself "THERE WILL BE COMPATIBILITY-BREAKING CHANGES". The ported files keep Shigma's copyright; harness-authored files are DeepSeek's.

**go-plugin and yamux: ok-with-conditions.** Same analysis as River, limited to the app backend binary. Both are still MPL-2.0 at HEAD and now carry "Copyright IBM Corp.". They arrive transitively, so the pin of the SDK that brings them decides their version; a relicence of future versions would be handled by holding the pin.

**S3 client library, JavaScript runtime for the oracle, headless Chromium: unverified.** The plan names no library or runtime for the first two, and L13 already lists the third. Each is verified when it is chosen, before the step that first uses it. The name "S3" in `blob-s3` is Amazon's mark: question 7.

Notes on rows with verdict ok that carry a fact worth recording:

- **NATS.** In April 2025 the original vendor asked to take the project out of its foundation and move the server to a non-open licence. The dispute was settled on 2025-05-01: the server stays Apache-2.0 in the foundation and the marks went to the Linux Foundation (CNCF statement of 2025-05-01; the vendor's statement). The licence at HEAD today is Apache-2.0. The row is optional and hosted-only, and never the source of truth, so exposure is low.
- **Kafka.** Used only by the backends in hosted cells, unmodified. The kernel has no Kafka client.
- **wazero.** The repository moved to its own organisation; the old path redirects. Go import paths that still say `tetratelabs` would need checking if it is ever adopted.
- **Embedded PostgreSQL.** One maintainer, and it downloads binaries built by third parties at test time. This is a supply-chain point for CI, not a licence point; nothing reaches an artifact.
- **Envoy and connect-go.** Both Apache-2.0 at HEAD; the two L13 entries can be closed.

## 6. Questions for a lawyer

1. **MPL-2.0 inside a closed enterprise binary.** Does our enterprise EULA, with its usual limits on reverse engineering and modification, "limit or alter the recipients' rights in the Source Code Form" of River, go-plugin or yamux (MPL-2.0 section 3.2(b)), and what carve-out wording avoids that?
2. **Pointer versus distribution.** Is a chart or compose file that pins an upstream image by digest, pulled by the user from the upstream registry, outside "distribution" or "conveying" in the jurisdictions we sell in? And for an air-gapped bundle that we assemble: which written source offer satisfies GPL-2.0 section 3 and AGPL-3.0 section 6 for packages in the base layers of images we did not build?
3. **The Go port of Cordis.** Is a clean reimplementation in another language, written while reading the source, a derivative work that needs the MIT notice (the plan assumes yes and carries it)? Which copyright lines are required: Shigma for the ported files, and DeepSeek for any behaviour ported from harness-authored code?
4. **Marketplace and in-process paid plugins.** When a buyer builds a distribution that links a paid closed plugin and then passes the binary to a third party (an integrator, a managed-service provider), what must the marketplace terms and the plugin's licence say? Are we, as operator of the marketplace and of the builder image, a distributor of that plugin or of the combined binary?
5. **A copyleft plugin in a self-built distribution.** `acceptLicenses` lets a site link a GPL or AGPL plugin for its own use. If that site is our hosted customer, or we build the binary for it as a service, do we become the distributor of a GPL work, and does AGPL section 13 then reach our closed enterprise plugins in the same binary?
6. **Marks in plugin ids.** Do ids such as `sql-postgres`, `kv-postgres`, `events-nats` and `jobs-river`, shown in a public index and later in a paid marketplace, count as product names under the PostgreSQL trademark policy (prior approval) and the Linux Foundation policy (no mark in a product name, no hyphenation)? Is showing only the "<own name> for <System>" title enough, and are we liable for third-party listings that break those policies?
7. **"S3".** May a provider id and its description use "S3" for a store that is not Amazon's, and what attribution does Amazon require?
8. **River Pro and SeaweedFS Enterprise by customers.** If a customer buys either and enables it under our plugin, does anything in those EULAs (hosting, service-provider and appliance clauses) reach us when we operate that customer's deployment as a managed service?
9. **Keyless signing.** Under what terms may a commercial product, and later a marketplace, rely on the public Fulcio and Rekor instances for plugin signatures? No terms of use were found today.
10. **Patent clauses.** Apache-2.0 section 3 and MPL-2.0 section 5.2 end the patent licence of a party that sues. Does that interact with any patent strategy for the enterprise edition?

## 7. Changes this audit proposes to the records

| Record | Change |
|---|---|
| File 08 section 5.1 L8 | Add: River's migrations are run through River's package and never copied; add the MPL-2.0 modules that a linked cosign compiles in, if ADR-020 chooses linking |
| L9 | Add the reasons read today: River Pro's EULA forbids redistribution in a product and hosting use; the SeaweedFS Enterprise page calls its licence "Required for production use". Add a release check that fails on a SeaweedFS licence file or enterprise image |
| L12 | Name the source of the port as `deepseek-ai/deepseek-harness`, `vendor/cordis` and `vendor/loader`, by commit; state that 4.1.0 and 1.1.0 are versions of the `@deepseek-ai` packages, and that upstream is `cordiverse/cordis` |
| L13 | Close Envoy and connect-go (Apache-2.0, read 2026-11-11). Add the S3 client library and the oracle's JavaScript runtime as open entries |
| L7, L16 | The additions of section 2.4: Unicode-3.0 and BlueOak-1.0.0 allowed; FSL, PolyForm, EPL, CDDL and EUPL named in the denied set; per-directory detection; the rule for SPDX `OR` and `AND` |
| L5 | State that an offline bundle needs an image-level SBOM and source offers for every image in it, not only for the BPF objects and the AGPL services |
| T7 | Add: an id is never shown as a listing title; the PostgreSQL attribution sentence where that mark appears |
| ADR-020 | Decide tool versus library for cosign; record that key mode has no third-party terms and that the public keyless instances' terms are unverified |

## 8. Sources

All read on 2026-11-11.

- Licence files at the commits in section 3, under `https://raw.githubusercontent.com/<owner>/<repo>/<commit>/<file>`.
- GitHub repository API, `https://api.github.com/repos/<owner>/<repo>`, for `riverqueue/river`, `minio/minio`, `seaweedfs/seaweedfs`, `wazero/wazero`, `cordiverse/cordis`, `deepseek-ai/deepseek-harness`, `fergusstrange/embedded-postgres`, `nats-io/nats-server`, `chaos-mesh/chaos-mesh`, `extism/extism`, `twmb/franz-go`.
- npm registry: `https://registry.npmjs.org/cordis`, `https://registry.npmjs.org/@deepseek-ai%2fcordis`, `https://registry.npmjs.org/@deepseek-ai%2fcordis-plugin-loader`, `https://registry.npmjs.org/@cordisjs%2fplugin-loader`.
- `https://riverqueue.com/pro` and `https://riverqueue.com/pro/license`.
- `https://seaweedfs.com/docs/pricing/`.
- `https://www.postgresql.org/about/licence/` and `https://www.postgresql.org/about/policies/trademarks/` (policy dated 2024-11-20).
- `https://www.mozilla.org/en-US/MPL/2.0/FAQ/` (Q5, Q8, Q11, Q12, Q13, Q17) and the MPL-2.0 text in River's `LICENSE`.
- `https://www.linuxfoundation.org/legal/trademark-usage` and `https://www.apache.org/foundation/marks/faq/`.
- `https://www.cncf.io/blog/2025/05/01/protecting-nats-and-the-integrity-of-open-source-cncfs-commitment-to-the-community/` and `https://www.synadia.com/blog/nats-server-next-steps` (found by search; summaries read, not the full pages).
- `https://github.com/sigstore/rekor/issues/2993` (found by search; shows that no terms of use were located, not read in full).

## 9. Verifier corrections applied 2026-11-11

An adversarial verifier read the primary sources of this audit again on 2026-11-11. Its ten findings are applied in the licence record (`docs/design/licence-audit-2026-11.md`), which wins where it and the sections above differ. The sections above are left as audited. This is engineering analysis, not legal advice.

| # | Component | What the audit said | Correction applied | Where |
|---|---|---|---|---|
| 1 | Cordis 4.1.0, its loader, DeepSeek Harness | Only the upstream author's notice on the port; the harness row "no code taken: ok" | The pinned versions are upstream release candidates plus harness-authored changes, so the port carries both MIT notices without condition, the pin is a commit of the harness repository, the oracle vendors `cosmokit` and `schemastery`, and the harness row is ok-with-conditions | Licence record §2.1, Q1 |
| 2 | cosign | Tool mode brings no conditions | Verification links sigstore-go; cosign stays a CI tool and is never baked into an image of ours. Its graph holds ten MPL-2.0 modules, one of them (`vault/api`) inside a BUSL-1.1 repository, so the scan works per directory | §2.1, §7.1 |
| 3 | Builder image, Go toolchain, go-licenses, SBOM generator | Tool only | The `builder` row, ok-with-conditions: the image is a release artifact, so the toolchain and the scanner are redistributed in it; it carries their licence texts, an SBOM and a source offer for its base layer, and a third party that passes it on inherits the duties. Base image and SBOM generator: UNVERIFIED, read before S2a ends | §2.1 |
| 4 | testcontainers-go, embedded PostgreSQL wrapper, goleak | Test dependency only; goleak not listed | Still ok, as libraries linked into `aiopsctl` through `sdk/testkit`: they are in its licence scan and notices. goleak is MIT (read 2026-11-11) | §2.1 |
| 5 | SeaweedFS, both editions | EULA text UNVERIFIED; enterprise code may ship beside the open build | The EULA (updated 2025-11-19) forbids distribution, hosting and service-bureau use, and the Enterprise Edition is a separate image and separate binaries. The release check names them: the image `chrislusf/seaweedfs-enterprise`, artifacts of `seaweedfs/artifactory`, the `-license` flag, `SEAWEED_LICENSE`, `seaweed-license.json`. We do not operate a customer's Enterprise cluster as a managed service | §2.1, Q15 |
| 6 | MinIO | Excluded because AGPL blocks a default bundle | The label stands. The ground is that it is unmaintained and its successor is not open source; as an unmodified separate process it would fall under the same rule as the AGPL servers. A customer who runs it uses `blob-s3` over the network | §2.1, §3 |
| 7 | PostgreSQL image | "Image pulled from upstream"; which image is UNVERIFIED | The project publishes no image. The chart and compose file pin the Docker Official Image `postgres` or a CloudNativePG image by digest; no Bitnami chart or image is a default until its current terms are read | §2.1 |
| 8 | MPL-2.0 source duty (River, go-plugin, yamux) | One `NOTICE` line with the source URL | The line names the exact module version, and the release keeps an archive of the exact source of every linked MPL-2.0 module. `rivershared/levenshtein` is third-party MIT and gets its own notice. A new part of the lawyer question | §2.1, §5 rules 2 and 3, Q7 |
| 9 | Components not listed | No row | New rows: Juju's dependency engine (LGPL-3.0 with a linking exception, prior art only); the kernel libraries not chosen yet (JWT and JWKS, OIDC verifier, KMS client, YAML parser, the gRPC runtime of the Python SDK), `unverified`. The Chaos Mesh daemon image never enters a mirror, bundle or image of ours | §2.1 |
| 10 | Scope | Not applicable | No licence in this group was found changed or misread. Not read again: the ASF trademark FAQ, the 2025 NATS statements, the River Pro pricing page, the bundled parts of k3s, the transitive licences of sigstore-go and of testcontainers-go | §9 |

---

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
