# ADR-044: The builder for a self-hoster on one VM, possibly air-gapped

- Status: **proposed 2026-11-23, awaiting the project owner.** Nothing in this ADR is in
  force. ADR-060 decision 2 requires it to be accepted before the builder code merges into
  `main`; the owner's answers to the questions below become decisions of this record.
- Scope: ADR-044 as `08:1175` and `00:778` state it (the builder image, the module cache
  format, when `--allow-bump` is acceptable, the content of the `init` deny-list), with
  the points ADR-060 decision 33 adds: the base layer and SBOM tool of the builder image
  and what the image carries; how the licence scan runs the same with a local toolchain;
  where a self-built distribution lists sealed-hook listeners and raises the required
  set. It changes no decision of `00-decisions.md`; every line an answer would edit is
  named in "Edits on acceptance".
- Citations: `00` to `09` are the files of `docs/architecture/` with those numbers, and
  `08:402` is line 402 of `08` at commit `8e1f7b9`. "licence" is
  `docs/design/licence-audit-2026-11.md`, cited the same way. The plugin record,
  `docs/design/plugin-system-2026-11.md`, is cited by section, and its dsh property
  checklist (§15) by row. ADR-059 and ADR-060 are cited by decision. A licence fact names
  the module and version whose files were read, `module@version`, as `go mod download
  -json` placed them in the Go module cache on 2026-11-23.

## Context

The documents fix most of the builder. `dist.yaml` has four fields: `release`, an OCI
reference by digest to the release's own `dist.yaml`; `plugins`, a list of `{module,
version, sum}`; `bindings: {providers, consumers}`; and `acceptLicenses` (`08:401`;
`07:1844`). `aiopsctl dist build`, or the builder image where there is no local
toolchain, with `--modcache <dir>` for an air-gapped site, generates the module table
and `go.mod` in a scratch directory and builds with `-mod=readonly`, cgo off and no `go
generate`; it fails on a changed pin without `--allow-bump <module>`, on a module outside
the release that declares an `aiops/` or `contrib/` id, on a `fake` package in the graph,
on a transitive `init` of four kinds and on the licence scan of L16; it runs the import
boundary and conformance on the source and writes `THIRD_PARTY_LICENSES/`, the NOTICE
texts and an SBOM into the image (`08:402`; `07:1860`; plugin record §11). The builder
image carries the Go licence and PATENTS text, the licence text of every tool in it, an
image SBOM and a source offer for the copyleft packages of its base layer (`08:402`;
`00:139`; licence:50). W3 lets a distribution the operator builds and signs add entries
to either binding map and raise, never lower, the required set (`08:403`; `00:257`).

ADR-060 places the builder: a root-module package compiled into `aiopsctl`, run with a
local toolchain or through the builder image, whose definition lives under `dist/`
(decision 22; `08:1221`); three files named `dist.yaml` with distinct roles (decision
20); the Go environment of the builder image (decision 21); the gate clauses `dist build`
evaluates (decision 17); and the refusals its tests prove (decision 31). The builder
follows the dsh model as plugin record §15 carries it: first-party packages in lockstep
with exact pins within one distribution (row 33, as is); no build scripts (row 34,
adapted); a new binary for new in-process code (row 35); a third party in process
through the builder only (row 36).

Reading this against the code at `8e1f7b9` and the modules read below shows gaps:

- The base layer and the SBOM tool of the builder image are not named and are UNVERIFIED
  until read before S2a ends (`08:402`, `:1342` L13, `:1381`, `:1407`; licence:50,
  :258). The documents put a licence scanner and an SBOM generator into the image, and
  give no mechanism by which the scan runs the same on a machine with a local toolchain,
  where those tools are absent.
- The pinned toolchain carries more licence texts than the Go licence and PATENTS (read
  below), and "every tool in it" covers them.
- `--modcache <dir>` has no format, nothing says how a site fills it, and the project
  publishes no offline bundle (`08:1284`, `:1334` L5). `GOSUMDB=off` holds for the
  builder image (ADR-060 decision 21), so a module's hash needs another source.
- The scratch module requires the root module, which holds `boot`, the loader and the
  first-party plugins (`08:1202`, assumption 1), and Go ignores the `replace` lines of a
  dependency. ADR-060 decision 21 serves only the three SDK modules.
- `--allow-bump <module>` has no rule. Plugin record §11 lets it name any pinned module,
  "the SDK included", while row 33 keeps first-party packages in lockstep and check 2 at
  major 0 needs the exact minor (`08:498`).
- The deny-list names four kinds and no function (`08:402`; `07:1860`), and a literal
  reading denies packages that every Go program or the remote runtime links (read
  below).
- The site's `dist.yaml` has no field for a sealed-hook listener or a raised required
  set, while the release input lists both (ADR-060 decision 20; `08:1221`) and W3 allows
  the raise.

### What was read

Licence files, read in the module cache:

| Module and version | File | As read |
|---|---|---|
| `golang.org/toolchain@v0.0.1-go1.26.8.linux-amd64`, the toolchain the root `go.mod` pins (`toolchain go1.26.8`) | `LICENSE`; `PATENTS` | The BSD-3-Clause text, "Copyright 2009 The Go Authors", naming Google LLC; Google's "Additional IP Rights Grant (Patents)" |
| The same | `src/vendor/modules.txt`, `src/cmd/vendor/modules.txt` | Sixteen vendored module copies (`x/sys` and `x/text` are in both lists): thirteen `golang.org/x` copies, each with a `LICENSE` equal to the Go licence (`x/arch` differs in its year only) and a `PATENTS` equal to the Go grant; `github.com/google/pprof`, the Apache-2.0 text; `github.com/ianlancetaylor/demangle` and `rsc.io/markdown`, BSD-3-Clause texts naming Google Inc. |
| The same | `src/crypto/internal/boring/LICENSE` | The Go licence for the Go source; the objects `goboringcrypto_linux_amd64.syso` and `_arm64.syso` under the BoringSSL terms it reproduces: the OpenSSL License, the original SSLeay License, ISC and an Intel licence. They link only with `GOEXPERIMENT=boringcrypto` and cgo (`boring.go` imports `C`) |
| The same | `src/runtime/testdata/testgoroutineleakprofile/goker/LICENSE`; `lib/time/README` | MIT for a runtime test data set; the IANA statement that the time zone database in `zoneinfo.zip` is in the public domain |
| `github.com/google/licenseclassifier/v2@v2.0.0` | `LICENSE`; `go.mod`; `assets/` | Apache-2.0. Its packages import, beyond the standard library, `github.com/davecgh/go-spew/spew` and `github.com/sergi/go-diff/diffmatchpatch`. Package `assets` embeds 266 licence texts under `assets/License`, with no statement on them beyond the module's `LICENSE` |
| `github.com/davecgh/go-spew@v1.1.1` | `LICENSE` | ISC |
| `github.com/sergi/go-diff@v1.1.0` | `LICENSE`; `APACHE-LICENSE-2.0`; `README.md` | MIT for the Go port; the Apache-2.0 text of the library it ports, which the README names |
| `github.com/google/go-licenses/v2@v2.0.1` | `LICENSE`; `go.mod` | Apache-2.0; twenty `require` lines, `licenseclassifier/v2` v2.0.0 and `golang.org/x/tools` among them, whose licences were not read |
| `github.com/anchore/syft@v1.57.2` | `LICENSE`; `COPYRIGHT`; `go.mod` | Apache-2.0, copyright Anchore, Inc.; 339 `require` lines, eight under `github.com/hashicorp/`, whose licences were not read |
| `github.com/CycloneDX/cyclonedx-gomod@v1.12.0` | `LICENSE`; `NOTICE` | Apache-2.0 with "Copyright OWASP Foundation" and a NOTICE |

Registration in `init`, read in the same cache:

- `database/sql.Register`: the `init` of `github.com/jackc/pgx/v5/stdlib` at
  `pgx/v5@v5.11.0` registers the drivers `pgx` and `pgx/v5`. The package graph of
  `cmd/aiops` at `8e1f7b9` (`go list -deps`) does not hold that package.
- `net/http.HandleFunc`: the `init` of `expvar` and of `net/http/pprof` in the toolchain
  above registers on the default mux.
- `prometheus/client_golang@v1.23.2`: the `init` of package `prometheus` registers its
  process and Go collectors with its own default registry, and the constructors of
  `promauto` register with `prometheus.DefaultRegisterer`.
- `google.golang.org/grpc@v1.82.1`: the `init` of `encoding/proto` calls
  `encoding.RegisterCodecV2`, and package `grpc` imports `encoding/proto`, so every
  binary that links gRPC registers that codec.
- `encoding/gob` in the toolchain above: its own `init` registers the basic types
  through `gob.Register`. `image/png`: its `init` calls `image.RegisterFormat`.
- `google.golang.org/protobuf@v1.36.11`: generated packages such as `timestamppb`
  register their descriptors and types with `protoregistry.GlobalFiles` and
  `GlobalTypes` from `init`.

## Decision

### One builder, two paths

1. **A release builds only with its own builder.** The module table the builder
   generates is Go code that the release's root module compiles (`07:1344`), so a
   builder of another release writes code for another `boot`. A release's signed manifest
   therefore names its builder image by digest, and that image holds the `aiopsctl` of
   the same release as its entry point (decision 4). On the image path, `dist build` and
   `plugin smoke --against` (`07:1576`) run the builder image that the signed manifest of
   the target release names. On the local path, `dist build` refuses a `dist.yaml` whose
   `release` is not the release this `aiopsctl` was built from, and names the version to
   use. Both paths of one release run one builder.
2. **The builder sets the Go environment itself, on both paths.** It starts `go` with an
   environment it builds, not the caller's: the settings of ADR-060 decision 21
   (`GOSUMDB=off`, `GOTOOLCHAIN=local`, `GOFLAGS=-mod=readonly`, `GOPRIVATE` and
   `GONOPROXY` unset); `CGO_ENABLED=0` (`08:402`); `GOENV=off` and `GOWORK=off`, so no
   file of the caller's machine changes the build; `GOPROXY` naming only the module
   source of decision 7, with no `direct`; and `GOCACHE`, `GOMODCACHE` and `TMPDIR`
   inside the scratch directory, so every module is extracted afresh from its hashed zip
   and the scan reads exactly that. Its cost is that every build compiles from nothing.
3. **The licence scan runs the same on both paths**, because it is the same code
   (decision 1 and question Q1), on the same toolchain (question Q2), over the same
   extracted modules and the same package graph (decision 2). For each package of `go
   list -deps` of the server's main package, the governing licence files are those of
   the nearest directory, from the package's own up to its module root, that holds any
   ("per directory", `08:1336` L7); the classifier of question Q1 names each, and a text
   it cannot name is `unknown` and fails the build (`08:1345` L16). Standard library
   packages are scanned like any other: the governing file is the toolchain's `LICENSE`,
   and a package vendored under `src/vendor/` has its own. The rules for `A OR B` and `A
   AND B` are L7's, unchanged. The detected licence of each module enters the signed
   manifest (ADR-060 decision 20). A CI case runs one build on each path from the same
   inputs and fails when the licence reports differ, or the SBOMs differ in anything but
   their creation time; it runs in the exit's two clean-machine jobs (ADR-060 decision
   30).

### The builder image

4. **What it holds.** The module `golang.org/toolchain` at the release's pinned version,
   unmodified, as the licence record keeps every upstream the project runs
   (licence:167); the `aiopsctl` of the same release as entry point; and the files of
   decision 5. Its base layer is question Q3, and no other tool is in it unless question
   Q1 puts one there. It holds no C compiler and no version control client: the builder
   has cgo off and resolves modules through a module source only (`08:402`; ADR-060
   decision 21). Its definition
   lives under `dist/` (`08:1221`), and the release pipeline writes it with the code that
   writes server images, so its licence files and SBOM come from the same code as theirs
   (`08:1336` L7). An image for another architecture is read at its own toolchain module
   before it ships; this record read `linux-amd64`.
5. **What it carries** (`08:402`; `00:139`; licence:50):
   - the toolchain's `LICENSE` and `PATENTS`;
   - every other licence file of the toolchain tree, in place: the thirteen `golang.org/x`
     copies of the Go licence and grant, the Apache-2.0 text of `pprof`, the BSD-3-Clause
     texts of `demangle` and `markdown`, the BoringSSL terms of the two objects the image
     ships although the builder never links them, the MIT text of the test data set and
     the IANA statement of `zoneinfo.zip`. `THIRD_PARTY_LICENSES/` holds a copy of each
     with the path it governs;
   - the `THIRD_PARTY_LICENSES/`, NOTICE texts and MPL-2.0 source lines of `aiopsctl`,
     written for that binary as for an image (ADR-060 decision 36; `08:1337` L8);
   - an image SBOM listing the toolchain module with the vendored modules of both
     `modules.txt` files, `aiopsctl` with its build list, and every package of the base
     layer;
   - a source offer for the copyleft packages of its base layer (`08:1334` L5;
     licence:216 for the forms). With question Q3 (a) the offer states that the image
     holds no package under GPL, LGPL or AGPL.

   The toolchain module and the builder image get entries in the upstream register, as
   ADR-060 decisions 34 and 51 require of a shipped image and a baked-in tool (`08:1332`
   L3).
6. **Every image the builder writes** carries `THIRD_PARTY_LICENSES/`, the aggregated
   NOTICE texts, the MPL-2.0 source lines and an SBOM, in the layout of decision 5
   (`08:402`, `:1336` L7; ADR-060 decision 22); its base layer is question Q4. The module
   source of decision 7 holds the exact zip of every linked MPL-2.0 module, the archive
   that L8 asks of whoever distributes the image (`08:1337`, `:1345` L16).

### Modules on one VM and on an air-gapped site

7. **The module cache format.** `--modcache <dir>` (`08:402`; plugin record §11) names a
   directory in the download layout of the Go module proxy protocol: per module,
   `<escaped module path>/@v/` holding `list` and, per version, `<version>.info`,
   `<version>.mod` and `<version>.zip`. It is the `cache/download` directory of any Go
   module cache, so the Go toolchain writes it unchanged and any static file server can
   serve it. The builder passes it as `GOPROXY=file://<dir>` and extracts into the module
   cache of its scratch directory, never into `<dir>`, which may be read-only and is
   mounted read-only on the image path. Without `--modcache`, the module source is the
   first URL of the caller's `GOPROXY` variable; with neither, the build fails before it
   resolves anything, so no default proxy is ever asked. A vendor directory is not a
   format: the build runs with `-mod=readonly` (`08:402`). How a site fills `<dir>` is
   question Q5.
8. **What a module is checked against.** With `GOSUMDB=off`, the builder writes the
   `go.sum` of its scratch module from recorded hashes: the release's pins from its
   signed manifest and each entry of `plugins` with its `sum` (`08:401`). A module
   neither names takes its hash from the module source, and that hash is checked against
   the `go.sum` inside the zip of every module of `plugins` that lists the same version,
   the file its author resolved with; any difference fails the build. The hash of every
   module enters the SBOM and the pins of the signed manifest.
9. **What E7 needs** (`01:285`, `:384`). In the site's registry, by digest: the release's
   `dist.yaml` that `release` names; on the image path, the builder image of that
   release; the artifact of each in-process plugin, whose manifest `plugin install` reads
   before it answers `rebuild-required` (`08:397`); and a repository for the image the
   build writes. In the module source: every module of the build list, the release's own
   modules included (question Q9). No index (none is configured by default, `08:405`)
   and no checksum database (decision 8). The project publishes no offline bundle
   (`08:1284`, `:1334` L5): the operator copies the release's artifacts and fills the
   module cache from a proxy it reaches, for its own use, as it copies the upstream
   digests.

### Strict versions

10. **`--allow-bump`.** The builder compares the build list that version selection gives
    with the pins of the release's signed manifest and fails on each pinned module whose
    version changed (`08:402`; `07:1860`; plugin record §11). Selection never lowers a
    version and never changes a major, which is part of the module path, so a change is
    a raise. `--allow-bump <module>` admits the raise of that one module when:
    - the module is not built by the release pipeline (question Q6);
    - every other check of the build passes on the raised build list, the suites on the
      source included (ADR-060 decision 22), so the first-party rows run their suites
      against what the binary links; and
    - the flag names a module whose version did change; a flag that names an unchanged
      module fails the build, so a stale flag cannot admit a later raise.

    `dist build` prints each raise with the release's version, the built version and the
    module of `plugins` whose requirement caused it. The signed manifest records the
    release's version beside the built one, and boot and `profile verify` print it with
    the departures from the release bindings (`08:366`, `:403`; ADR-060 decision 14). A
    module that the release does not pin is not a bump; the licence scan and the
    deny-list cover it.

### The `init` deny-list

11. **Content.** A registration is a call of a function in the table below. It counts
    when it is reachable through the static call graph from an `init` function or a
    package-level variable initializer of a package in the package graph of the server
    binary (ADR-060 decision 58, reading 4), and the package whose function makes the
    call is not the package that declares the registry. So `gob`'s registration of its
    basic types and the Prometheus client's own collectors do not count, and `expvar`,
    `net/http/pprof` and `pgx/v5/stdlib` do. The exceptions are question Q7.

    | Kind (`08:402`) | A call of | Registry |
    |---|---|---|
    | A `database/sql` driver | `database/sql.Register` | The driver registry of `database/sql` |
    | A default-mux handler | `net/http.Handle`, `net/http.HandleFunc`; the methods `Handle` and `HandleFunc` of `*net/http.ServeMux` on `net/http.DefaultServeMux` | `DefaultServeMux` |
    | A default-registry collector | `Register` and `MustRegister` of `github.com/prometheus/client_golang/prometheus`, and the same methods on its `DefaultRegisterer`; the constructors of its `promauto` package that register with `DefaultRegisterer` | The Prometheus client's default registry |
    | A global codec | `RegisterCodec`, `RegisterCodecV2` and `RegisterCompressor` of `google.golang.org/grpc/encoding`; `encoding/gob.Register` and `RegisterName` | gRPC's codec and compressor registries; `gob`'s type registry |

12. **Ceiling.** Calls through an interface, a function value or reflection are not
    followed. The scan stops a mistaken dependency, not hostile code, as the gate does
    (`08:508`). A first-party plugin has no `init` at all (`08:1257`). The call graph is
    built with `golang.org/x/tools`, whose version and licence are read when it enters
    `aiopsctl` (ADR-060 decision 34).

### A self-built distribution's listeners and required set

13. **Where a site lists them** is question Q8. Whatever the answer, a site adds and never
    removes: the release's listeners, binding entries and required set stay in the
    merged manifest (`08:403`, `:451`); the builder evaluates check 7 on the merged set
    (`08:503`; ADR-060 decision 17), so a raised required key or row brings the static
    closure of its needs; and the signed manifest marks each site entry, so boot and
    `profile verify` print it with the departures from the release bindings without the
    release file.

### Tests

14. Beside the builder cases of ADR-060 decision 31, each failing on its base (ADR-060
    decision 32): a `dist.yaml` naming another release on the local path; the
    comparison of decision 3; a zip in `<dir>` whose hash differs from the recorded one,
    and one that differs from the `go.sum` of the `plugins` module that lists it;
    `--allow-bump` on an SDK module
    (with question Q6 (a)) and on an unchanged module; a bump printed by `dist build`,
    recorded and printed at boot; per kind of the deny-list, a fixture package outside
    the registry's package that fails, `gob` and the Prometheus client's own
    registrations that pass, and an exception of question Q7 that passes; each kind of
    site entry of question Q8, printed at boot and by `profile verify`.

## Consequences

- With the recommended answers, the builder image is the toolchain tree and `aiopsctl`
  on an empty base. The L13 items "the base layer and the SBOM tool of the builder image"
  close: no base layer and no third-party SBOM tool (`08:1342`).
- `aiopsctl` links `licenseclassifier/v2` with `go-spew` and `go-diff`, and
  `golang.org/x/tools` for the deny-list, and embeds the classifier's 266 licence texts,
  which get a register entry as an embedded data set (ADR-060 decision 34). None enters
  a server binary. The builder package keeps its provisional cap of 3,000 lines (ADR-060
  decision 35).
- A release's signed manifest names its builder image. The signed manifest of a
  self-built distribution records each bump beside the release's pin and marks each site
  entry of question Q8, and boot prints both.
- An air-gapped site needs a connected machine once for each change of its build list,
  to fill the module cache.
- Every build compiles from an empty cache. A shared build cache can be added when build
  time matters, with the comparison of decision 3 kept as its proof.

### Edits on acceptance

The commit that records the owner's answers edits, for the recommended options: `08:401`
(the shape of `bindings`, question Q8); `08:402` (the `--modcache` format of decision 7,
and the deny-list "as ADR-044 lists it"); `08:403` and `07:1874` (site listeners, raises
and bumps printed with the departures); `08:1342` (L13), `:1381` and `:1407` (questions
Q1 and Q3); plugin record §11 (the shape of `bindings`, and "the SDK included" with
question Q6); and `08:1175` and `00:778` (status). ADR-060 decision 20 (the builder image
digest of decision 1, the bumps and the marked site entries in the signed manifest) and
decision 21 (the served root module, question Q9) are edited in the same commit. The
licence record keeps its date; `08` §5 carries the reading.

## Questions for the owner

Each question gives its options, the recommended one, why, and when the answer is needed.

**Q1. Where do the licence scanner and the SBOM writer live?** The documents put a
licence scanner (go-licenses) and an SBOM generator into the builder image (`08:1381`,
`:1407`; licence:50), and ADR-060 decision 33 asks how the scan runs the same with a
local toolchain, where those tools are absent.
- (a) In the builder package. Detection links `github.com/google/licenseclassifier/v2`
  (Apache-2.0 at v2.0.0; it links `go-spew` v1.1.1, ISC, and `go-diff` v1.1.0, MIT with
  the Apache-2.0 text of the library it ports) and embeds its corpus. The SBOM is SPDX 2.3
  in JSON, written with the standard library from the build list and the layer inputs.
  Both paths and the CI licence report (`08:1245`; ADR-060 decision 36) run the same code,
  and the image holds no scanner or SBOM tool.
- (b) The tools as written: go-licenses (Apache-2.0 at v2.0.1; twenty `require` lines)
  and syft for the image SBOM (Apache-2.0 at v1.57.2; 339 `require` lines, eight under
  `github.com/hashicorp/`), as binaries in the image; the local path refuses unless both
  are on the `PATH` at the pinned versions. Every module those binaries link travels in
  the image with its notices, and none of their transitive licences is read yet.
- (c) As (b), with cyclonedx-gomod (Apache-2.0 with a NOTICE at v1.12.0) for the binary's
  SBOM; it reads the Go build list only, so a base layer needs a second tool.
- Recommended: (a). The scan is the same by construction, the local path needs nothing
  installed, three linked modules replace hundreds of shipped ones, and the SBOM item of
  L13 closes with first-party code. Needed before the licence scan merges.

**Q2. Which toolchain may the local path use?** The image holds "the pinned Go toolchain"
(`00:139`), `toolchain go1.26.8` in the root `go.mod` at `8e1f7b9`. Nothing states the
version on the local path, and `GOTOOLCHAIN=local` (ADR-060 decision 21) forbids a
download.
- (a) The local path refuses unless `go version` reports the pinned toolchain, and prints
  that version. Both paths then build the same binary from the same inputs.
- (b) Any toolchain that the release's `go` line admits; `dist build` prints the version
  used and the signed manifest records it. The licence report stays the same and the
  binary does not, so the comparison of decision 3 compares reports only.
- Recommended: (a). The local job of the exit (ADR-060 decision 30) then proves the image
  path, and a self-built binary links the standard library the release tested. Needed
  before the local path merges.

**Q3. What is the base layer of the builder image?** It is UNVERIFIED until named and read
(`08:1342` L13; licence:258), and the image owes a source offer for the copyleft packages
in it (`08:402`).
- (a) None: the toolchain tree, `aiopsctl` and the files of decision 5 on an empty base.
  Nothing in a base layer to read or to offer source for. The image holds no shell and
  no CA bundle: where the module source is an `https` URL, `dist build` mounts the host's
  trust store read-only and points `SSL_CERT_FILE` at it, a variable Go's `crypto/x509`
  reads; an air-gapped build needs no network.
- (b) A minimal base layer of distribution packages that holds a CA bundle. Its package
  licences are not read here; each is read from the package's copyright file in the
  image at its pinned digest before it ships, and the offer covers each copyleft one.
- (c) The official Go image, which holds git, a GNU userland and a C compiler (licence:50)
  that the builder never runs, and needs the largest offer.
- Recommended: (a). It satisfies every duty of `08:402` with nothing left to read, and
  the builder runs no program a base layer would bring. Needed before the image
  definition merges under `dist/`.

**Q4. What is the base layer of the server images the builder writes?** ADR-060 decision
34 has it read before S2a ends, and no document names it.
- (a) None at S2a: the server binary, built with cgo off, and the files of decision 6.
  No row of the S2a images dials a TLS endpoint (ADR-060 decisions 27 and 38 list them),
  so a trust store, time zone data or any other file enters the image with the first row
  that needs it, under its own register entry, as a definition lands with its first
  consumer (`08:678`).
- (b) The base of question Q3 (b), from S2a.
- Recommended: (a). Nothing is shipped before a row reads it, and the image owes no
  source offer. Needed before the builder writes its first image.

**Q5. How does a site fill the module cache?** `08:402` gives the flag and no way to fill
it, and the project publishes no offline bundle (`08:1284`).
- (a) One flag. `--modcache <dir>` is read first; when the caller's `GOPROXY` names a
  proxy, the builder fetches what `<dir>` lacks and adds it to `<dir>` after the build.
  A build on a connected machine fills the cache that the air-gapped machine then reads
  with no proxy, and the cache holds exactly that build list.
- (b) A command of its own, `aiopsctl dist fetch <dist.yaml> --modcache <dir>`, on a
  connected machine; `dist build --modcache` never fetches. It adds a command to those
  of `00:137` and ADR-060 decision 23.
- (c) No support in `aiopsctl`: the site fills `<dir>` with the Go toolchain on a
  connected machine, from a `go.mod` and `go.sum` that `dist build` prints.
- Recommended: (a). The flag stays as written, no command is added, and the fill is the
  resolution the build itself makes. Needed before `--modcache` merges.

**Q6. May `--allow-bump` name a module the release pipeline builds?** Plugin record §11
lets it name any pinned module, "the SDK included". Row 33 of plugin record §15, carried
as is, keeps first-party packages in lockstep with exact pins within one distribution,
and a raised SDK changes `serves`, so at major 0 check 2 denies every first-party row
that requires a minor no longer served (`08:498`).
- (a) No. The three SDK modules and the root module fail strict versions with or without
  the flag, and an author requires the SDK version of the release the plugin targets,
  which `plugin smoke --against` proves before a site builds. Plugin record §11 is
  edited.
- (b) As written: any pinned module, the SDK included; check 2 then decides at build and
  at boot whether the first-party rows still pass.
- Recommended: (a). It is the dsh rule carried as is, and (b) admits a build that check 2
  denies row by row. Needed before the strict-version check merges.

**Q7. What does "a global codec" cover, and where do exceptions live?** gRPC registers
its own proto codec from `init` in every binary that links it, and the remote runtime of
S2b links gRPC (`07:1097`). Every generated protobuf package registers its types with
`protoregistry` from `init`, and `image/png` registers an image format.
- (a) The table of decision 11. Exceptions are a table in the builder package, one entry
  per registering package and function with its reason, changed only by a reviewed pull
  request; a site adds none. It is empty at S2a and gains
  `google.golang.org/grpc/encoding/proto` with `RegisterCodecV2` when gRPC enters the
  server binary. Image format and protobuf type registries are not on the list.
- (b) As (a), with `image.RegisterFormat` and the protobuf registries on the list; every
  generated protobuf package then needs an exception.
- (c) No exceptions. The server binary then cannot link gRPC, which contradicts the
  remote runtime; listed only to be refused.
- Recommended: (a). It denies the four kinds as written, keeps the release buildable, and
  makes each exception visible in review. Needed before the deny-list scan merges.

**Q8. Where does a self-built distribution list sealed-hook listeners and raise the
required set?** `dist.yaml` has four fields and `bindings` is `{providers, consumers}`
(`08:401`). A sealed hook is "sealed to the distribution" (`07:439`, `:464`), the release
input lists its listeners and its required set (ADR-060 decision 20), and W3 allows a
raise (`08:403`). ADR-059 decision 20 reads the listeners as "who is allowed", with the
bindings.
- (a) `bindings` gains `sealedHooks`, the listeners a site adds per hook, and `required`,
  the keys and rows it adds; the four fields stay. Edits `08:401` and plugin record §11.
- (b) Two new top-level fields, `sealedHooks` and `required`; "four fields" becomes six
  in `08:401`, `07:1844`, `02:207`, plugin record §11 and ADR-060 decisions 20 and 22.
- (c) No site field. A site raises the required set only with `required: true` on a row
  in its profile layer (`08:146`, `:451`), which names rows and not keys, and adds a
  listener only through a release-form input that restates the whole release. A raise
  by key is then unavailable, which narrows W3.
- Recommended: (a). The four fields hold, the listeners sit with the other "who is
  allowed" lists, and every W3 permission has a place. Needed before the builder reads
  the site's `dist.yaml`.

**Q9. Is the root module served beside the SDK modules?** The scratch module requires the
root module at the release's version (`08:1202`, assumption 1), and the root `go.mod` at
`8e1f7b9` requires the SDK modules at `v0.0.0` behind `replace` lines, which Go ignores
in a dependency. ADR-060 decision 21 serves the three SDK modules only.
- (a) Yes. A release serves the root module at the release version, from the same module
  source as the SDK modules, with a served `go.mod` that names the SDK version and holds
  no `replace` line, under the test of ADR-060 decision 21. Until L14 it is served only
  where the SDK modules are, inside the exit job (ADR-060 decision 40).
- (b) The release's `dist.yaml` artifact carries the root module's zip as a layer, and the
  builder adds it to its module source.
- Recommended: (a). Every module a build resolves comes through one module source, and
  the exit's test release needs it either way (ADR-060 decision 38). Needed before the
  first build from a scratch directory.
