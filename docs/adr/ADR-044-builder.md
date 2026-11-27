# ADR-044: The builder for a self-hoster on one VM, possibly air-gapped

- Status: **accepted 2026-11-23 by the project owner.** The owner answered all 13
  questions on the same date with the recommended option of each; the section "Owner
  answers of 2026-11-23" records them as decisions 15 to 27. ADR-060 decision 2 required
  this acceptance before the builder code merges into `main`. The S2b part, the last
  section of this file, is **accepted 2026-11-27 by the project owner**: the owner
  answered its six questions, Q14 to Q19, on that date with the recommended option of
  each, and the section "Owner answers of 2026-11-27" at the end of that part records
  them as decisions 40 to 45. It changes no decision above; decision 8 gains a dated
  note.
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
adapted); no run-time install of in-process code, so new in-process code needs a new
binary (row 35, not carried, since Go links statically); a third party in process
through the builder only (row 36, adapted).

Reading this against the code at `8e1f7b9` and the modules read below shows gaps:

- The base layer and the SBOM tool of the builder image are not named and are UNVERIFIED
  until read before S2a ends (`08:402`, `:1342` L13, `:1381`, `:1407`; licence:50,
  :258). The documents put a licence scanner and an SBOM generator into the image, and
  give no mechanism by which the scan runs the same on a machine with a local toolchain,
  where those tools are absent.
- The pinned toolchain carries more licence texts than the Go licence and PATENTS (read
  below), and "every tool in it" covers them. A host's Go can report the pinned version
  and still hold another tree: Ubuntu's `golang-1.26-go` package reports `go1.26.8`, and
  its `GOROOT` holds no `LICENSE` and no `PATENTS`.
- The `aiopsctl` of a release embeds that release's signed manifest (ADR-060 decisions
  17, 20 and 50), and the builder image is to hold that `aiopsctl`, so the signed
  manifest cannot name the builder image by digest.
- `--modcache <dir>` has no format, nothing says how a site fills it, and the project
  publishes no offline bundle (`08:1284`, `:1334` L5). `GOSUMDB=off` holds for the
  builder image (ADR-060 decision 21), so a module's hash needs another source.
- The module table imports `boot`, the loader and the first-party plugins, which
  `08:1202` (assumption 1) places in one root module, so the scratch module requires the
  root module; that requirement is this record's reading. Go ignores the `replace` lines
  of a dependency, and ADR-060 decision 21 serves only the three SDK modules.
- `--allow-bump <module>` has no rule. Plugin record §11 lets it name any pinned module,
  "the SDK included", while row 33 keeps first-party packages in lockstep and check 2 at
  major 0 needs the exact minor (`08:498`).
- The deny-list names four kinds and no function (`08:402`; `07:1860`), and a literal
  reading denies packages that common libraries and the remote runtime of S2b link (read
  below).
- The site's `dist.yaml` has no field for a sealed-hook listener or a raised required
  set, while the release input lists both (ADR-060 decision 20; `08:1221`) and W3 allows
  the raise.

### What was read

Licence files, read in the module cache:

| Module and version | File | As read |
|---|---|---|
| `golang.org/toolchain@v0.0.1-go1.26.8.linux-amd64`, the toolchain the root `go.mod` pins (`toolchain go1.26.8`) | `LICENSE`; `PATENTS` | The BSD-3-Clause text, "Copyright 2009 The Go Authors", naming Google LLC; Google's "Additional IP Rights Grant (Patents)" |
| The same | A walk of the whole tree for licence and notice files (decision 3), the zips under `lib/fips140/` opened | Outside the zips, 36 files named `LICENSE` or `PATENTS`: the root pair; for the seventeen vendored module copies that three `modules.txt` files list (`src/vendor/`, `src/cmd/vendor/` and `src/cmd/compile/internal/ssa/_gen/vendor/`, the last holding `golang.org/x/tools` v0.27.0), fourteen `golang.org/x` copies, each with a `LICENSE` equal to the Go licence (the `x/arch` copy differs in its year only) and a `PATENTS` equal to the Go grant, `github.com/google/pprof` with the Apache-2.0 text, and `github.com/ianlancetaylor/demangle` and `rsc.io/markdown` with BSD-3-Clause texts naming Google Inc.; `src/cmd/vendor/github.com/google/pprof/third_party/svgpan/LICENSE`, three BSD clauses under "Copyright 2009-2017 Andrea Leofreddi" with a closing paragraph on the views of the authors, for `svgpan.js`, which pprof embeds; `src/crypto/internal/boring/LICENSE` (next row); and the MIT text of a runtime test data set (`src/runtime/testdata/testgoroutineleakprofile/goker/LICENSE`). Inside `lib/fips140/v1.0.0-c2097c7c.zip` and `lib/fips140/v1.26.0.zip`, two copies of the `golang.org/fips140` module, one `LICENSE` each, equal to the Go licence. `lib/time/README`: the IANA statement that the time zone database in `zoneinfo.zip` is in the public domain |
| The same | `src/crypto/internal/boring/LICENSE` and the build constraints of that directory | Its first lines put "The Go source code and supporting files in this directory" under the Go licence and say that the rest applies "When building with GOEXPERIMENT=boringcrypto": the BoringSSL terms of the objects `goboringcrypto_linux_amd64.syso` and `_arm64.syso`, that is the OpenSSL License, the original SSLeay License, ISC and an Intel licence. The objects sit in package `crypto/internal/boring/syso`, whose one Go file is constrained to `//go:build boringcrypto`; `boring.go` needs that tag and cgo, and `notboring.go` builds otherwise. With cgo off, `go list -deps ./cmd/aiops` at `8e1f7b9` holds `crypto/internal/boring` and `crypto/internal/boring/sig`, through `crypto/sha256`, which `boot/digest.go` imports; it does not hold the `syso` package |
| The same | Headers of the standard-library files that `go list -deps ./cmd/aiops` compiles at `8e1f7b9` (cgo off, `linux/amd64`) | Sixteen files carry a third-party notice in their header and no licence file of their own: `runtime/memmove_amd64.s`, derived from Inferno, under copyrights of Lucent Technologies and Vita Nuova with the MIT permission text; and fifteen files of `math` (`acosh.go`, `asinh.go`, `atanh.go`, `cbrt.go`, `erf.go`, `exp.go`, `expm1.go`, `j0.go`, `j1.go`, `jn.go`, `lgamma.go`, `log.go`, `log1p.go`, `remainder.go`, `sqrt.go`), from FreeBSD's msun, with Sun Microsystems' notice "Permission to use, copy, modify, and distribute this software is freely granted, provided that this notice is preserved" |
| `github.com/google/licenseclassifier/v2@v2.0.0` | `LICENSE`; `go.mod`; `assets/` | Apache-2.0. Its packages import, beyond the standard library, `github.com/davecgh/go-spew/spew` and `github.com/sergi/go-diff/diffmatchpatch`. Package `assets` embeds, through `//go:embed */*/*` in `assets/embed.go`, 431 files: 321 under `License` (266 directories), 73 under `Header` (43) and 37 under `Supplement` (24). They include the GPL, LGPL, AGPL, SSPL and CC-BY-NC-ND texts. Thirty files in twenty directories carry the FSF's term "Everyone is permitted to copy and distribute verbatim copies of this license document, but changing it is not allowed"; `License/GPL-3.0/license.txt` ends at "END OF TERMS AND CONDITIONS", while `License/GPL-2.0/a.txt` goes on past it with "How to Apply These Terms". The module states no source for the corpus, and its own Apache-2.0 `LICENSE` is the only grant it carries. The directory names are the classifier's names (for example `GPL-2.0-with-GCC-exception`, `LGPLLR`) |
| `github.com/davecgh/go-spew@v1.1.1` | `LICENSE` | ISC |
| `github.com/sergi/go-diff@v1.1.0` | `LICENSE`; `APACHE-LICENSE-2.0`; `README.md` | MIT for the Go port; the Apache-2.0 text of the library it ports, which the README names |
| `golang.org/x/tools@v0.51.0`, `golang.org/x/mod@v0.42.0`, `golang.org/x/sync@v0.24.0`, the versions of the root module's build list at `8e1f7b9`, where no binary links them | `LICENSE`; `PATENTS`; the `go.mod` of `x/tools` | Each `LICENSE` equal to the Go licence and each `PATENTS` equal to the Go grant. `x/tools` requires `go-cmp`, `goldmark`, `x/mod`, `x/net`, `x/sync` and `x/telemetry`; its packages `go/packages`, `go/ssa`, `go/ssa/ssautil`, `go/callgraph` and `go/callgraph/vta` link, of those, `x/mod` and `x/sync` only (`go list -deps` of a module that imports them) |
| `github.com/google/go-licenses/v2@v2.0.1` | `LICENSE`; `go.mod` | Apache-2.0; twenty `require` lines, `licenseclassifier/v2` v2.0.0 and `golang.org/x/tools` among them, whose licences were not read at go-licenses' versions |
| `github.com/anchore/syft@v1.57.2` | `LICENSE`; `COPYRIGHT`; `go.mod` | Apache-2.0, copyright Anchore, Inc.; 339 `require` lines, eight under `github.com/hashicorp/`, whose licences were not read |
| `github.com/CycloneDX/cyclonedx-gomod@v1.12.0` | `LICENSE`; `NOTICE` | Apache-2.0 with "Copyright OWASP Foundation" and a NOTICE |
| `golang.org/x/net@v0.53.0`, the version `google.golang.org/grpc@v1.82.1` requires and the one a module that links only that gRPC selects | `LICENSE`; `PATENTS` | The Go licence and the Go grant, each equal to the toolchain's |

Registration in `init`, read in the same cache:

- `database/sql.Register`: the `init` of `github.com/jackc/pgx/v5/stdlib` at
  `pgx/v5@v5.11.0` registers the drivers `pgx` and `pgx/v5`. The package graph of
  `cmd/aiops` at `8e1f7b9` (`go list -deps`) does not hold that package.
- `net/http.HandleFunc`: the `init` of `expvar` and of `net/http/pprof` in the toolchain
  above registers on the default mux.
- `prometheus/client_golang@v1.23.2`: the `init` of package `prometheus` registers its
  process and Go collectors with its own default registry (`prometheus/registry.go:60-63`),
  and the constructors of `promauto` register with `prometheus.DefaultRegisterer`.
- `google.golang.org/grpc@v1.82.1`: the `init` of `encoding/proto` calls
  `encoding.RegisterCodecV2`, and package `grpc` imports `encoding/proto`. Its file
  `trace_withtrace.go`, built unless the tag `grpcnotrace` is set, imports
  `golang.org/x/net/trace`; `trace_notrace.go` stands in for it under the tag. In a module
  that links only that gRPC, `go list -deps` holds `x/net/trace` without the tag and not
  with it, and a text search of that graph for the calls of decision 11 finds the two
  below and `RegisterCodecV2` only.
- `golang.org/x/net@v0.53.0`: the `init` of package `trace` (`trace/trace.go:120-131`)
  calls `net/http.HandleFunc` for `/debug/requests` and `/debug/events` on the default
  mux.
- `encoding/gob` in the toolchain above: its own `init` registers the basic types
  through `gob.Register` (`registerBasics` in `type.go`). `image/png`: its `init` calls
  `image.RegisterFormat`.
- `google.golang.org/protobuf@v1.36.11`: generated packages such as `timestamppb`
  register their descriptors and types with `protoregistry.GlobalFiles` and
  `GlobalTypes` from `init`.

## Decision

### One builder, two paths

1. **A release builds only with its own builder.** The module table the builder
   generates is Go code that the release's root module compiles (`07:1344`), so a
   builder of another release writes code for another `boot`. The release's signed
   manifest names no image, because the release's `aiopsctl` embeds it and the builder
   image holds that `aiopsctl` (decision 4). The image is named one step out: the OCI
   artifact that `release` names, which holds the release's signed manifest (ADR-060
   decision 17), carries the builder image's reference by digest as an annotation of its
   OCI manifest. No binary embeds that OCI manifest, and the digest in `release` covers
   it.
   - On the image path, `dist build`, `plugin smoke --against` (`07:1576`) and `plugin
     verify` of in-process code (ADR-060 decision 46) run the image that annotation
     names. The `aiopsctl` inside the image makes the check of the local path, so an
     image whose `aiopsctl` embeds another release's manifest refuses to build.
   - On the local path, the three commands refuse a release whose signed manifest
     differs, by digest, from the one this `aiopsctl` embeds, and name the version to
     use.
   - `plugin smoke` builds from `./`, a directory with no version or sum (`07:1576`). The
     builder writes it as a module zip at a pseudo-version no tag carries, into a module
     source of its scratch directory placed before the others (decision 7), and enters
     the zip's hash in the scratch `go.sum` and in the smoke output, so the directory
     enters the build as every module does (decision 8). `plugin verify` takes the
     module path, version and sum from the pulled artifact (ADR-060 decision 19) and
     resolves the module through the module source against that sum. Note of
     2026-11-25, as built: the pseudo-version is the zero pseudo-version of the module's
     major, `v0.0.0-00010101000000-000000000000` at major 0; both commands take the
     release artifact as `--against` and the module source as `--modcache`, and on the
     image path the image's `aiopsctl` runs `plugin smoke` on the local path, or `dist
     build` of the throwaway `dist.yaml` that `plugin verify` writes. Second note of
     2026-11-25: `plugin verify` runs step 5 of `plugin check` on the module, which `dist
     build` does not, so it takes the local path; on the image path it builds nothing and
     its step is not built (ADR-060 decision 46). On the image path `plugin smoke` refuses
     a release that `--against` does not name by digest before it runs the image, as the
     local path does.

   Both paths of one release run one builder.
2. **The builder sets the Go environment itself, on both paths.** It starts `go` with an
   environment it builds, not the caller's: the settings of ADR-060 decision 21
   (`GOSUMDB=off`, `GOTOOLCHAIN=local`, `GOFLAGS=-mod=readonly`, `GOPRIVATE` and
   `GONOPROXY` unset); `CGO_ENABLED=0` (`08:402`); `GOEXPERIMENT` unset, so the build
   never takes the `boringcrypto` experiment that decision 3 reads; `GOENV=off` and
   `GOWORK=off`, so no file of the caller's machine changes the build; `GOPROXY` naming
   only the module sources of decision 7, with no `direct`; and `GOCACHE`, `GOMODCACHE`
   and `TMPDIR` inside the scratch directory, so every module is extracted afresh from
   its hashed zip and the scan reads exactly that. It builds with `-trimpath`, so
   neither the scratch directory nor the toolchain's path enters the binary, and both
   paths give the same bytes from the same inputs on the same tree. Its cost is that
   every build compiles from nothing.
3. **The licence scan runs the same on both paths**, because it is the same code
   (decision 1 and question Q1), on the same toolchain tree (question Q2), over the same
   extracted modules and the same package graph (decision 2).
   - *Licence files.* A licence file is a regular file whose name, compared without
     case, has `license`, `licence`, `copying` or `unlicense` as one of the parts it
     splits into at `.`, `-` and `_`, and does not end in `.go`, `.s`, `.c`, `.h` or
     `.syso`: `LICENSE.md`, `COPYING.LESSER` and the `APACHE-LICENSE-2.0` of
     `go-diff@v1.1.0` are licence files. A file named the same way with `notice`,
     `patents` or `copyright` is a notice file: it is copied into
     `THIRD_PARTY_LICENSES/` and aggregated (`08:1336` L7), and not classified.
   - *Governing files.* For each package that `go list -deps` of the server's main
     package lists, the governing licence files are those of the nearest directory,
     from the package's own up to its module root, that holds any ("per directory",
     `08:1336` L7); the classifier of question Q1 names each, and a text it cannot name
     is `unknown` and fails the build (`08:1345` L16). The rules for `A OR B` and `A AND
     B` are L7's, unchanged.
   - *The standard library.* For a standard library package the walk goes up to the
     root of the toolchain tree, so the toolchain's `LICENSE` governs every package with
     no nearer licence file, and a package under `src/vendor/` takes its module's own
     file. The toolchain's texts are read from the tree that question Q2 verifies, never
     from whatever `GOROOT` a host has. One reading is recorded, keyed to the hash of the
     file at the pinned toolchain so that a changed file is classified afresh:
     `crypto/internal/boring` and the packages under it are governed by the Go licence,
     as the first lines of their `LICENSE` say of the Go source of that directory, and
     the BoringSSL terms the file reproduces govern the objects of
     `crypto/internal/boring/syso`, a package built only with `GOEXPERIMENT=boringcrypto`,
     which the builder never sets (decision 2). When that package is in the graph the
     reading does not apply, and the scan classifies the whole file. A notice in a file
     header is not a licence file and the per-directory scan does not read it; what the
     builder does with the sixteen such notices of the server's standard library (What
     was read) is question Q13.
   - The detected licence of each module enters the signed manifest (ADR-060 decision
     20). A CI case runs one build on each path from the same inputs and fails when the
     licence reports differ, when the SBOMs differ in anything but their creation time,
     or when the binaries' digests differ; it runs in the exit's two clean-machine jobs
     (ADR-060 decision 30).

### The builder image

4. **What it holds.** The module `golang.org/toolchain` at the release's pinned version,
   unmodified, as licence:167 holds River, the AGPL servers, the agent and every pulled
   image; the `aiopsctl` of the same release as entry point; and the files of decision 5.
   Its base layer is question Q3, and no other tool is in it unless question Q1 puts one
   there. It holds no C compiler and no version control client: the builder has cgo off
   and resolves modules through a module source only (`08:402`; ADR-060 decision 21).
   Its definition lives under `dist/` (`08:1221`), and the release pipeline writes it
   with the code that writes server images, so its licence files and SBOM come from the
   same code as theirs (`08:1336` L7). An image for another architecture is read at its
   own toolchain module before it ships; this record read `linux-amd64`.
5. **What it carries** (`08:402`; `00:139`; licence:50):
   - the toolchain's `LICENSE` and `PATENTS`;
   - every other licence and notice file of the toolchain tree, in place, as a walk of
     the whole tree with the names of decision 3 finds them, the module zips under
     `lib/fips140/` opened, and the IANA statement of `lib/time/README`, which no file
     name marks. At go1.26.8 `linux-amd64` that is 34 more files named `LICENSE` or
     `PATENTS` and the `LICENSE` inside each of the two `golang.org/fips140` zips (What
     was read): the files of the seventeen vendored module copies, the `svgpan` text,
     the BoringSSL terms of the two objects the image ships although the builder never
     links them, and the MIT text of the test data set. `THIRD_PARTY_LICENSES/` holds a
     copy of each with the path it governs. The release pipeline generates the list from
     that walk at each toolchain pin, and a test fails when a licence or notice file of
     the tree is missing from `THIRD_PARTY_LICENSES/`;
   - the `THIRD_PARTY_LICENSES/`, NOTICE texts and MPL-2.0 source lines of `aiopsctl`,
     written for that binary as for an image (ADR-060 decision 36; `08:1337` L8), over
     its own build list and the build list of each suite test binary it embeds (ADR-060
     decision 46), since each test binary is linked on its own;
   - an image SBOM listing the toolchain module, the vendored modules of all three
     `modules.txt` files and the two `golang.org/fips140` modules; `aiopsctl` with its
     build list; each embedded test binary with its build list; and every package of
     the base layer;
   - a source offer for the copyleft packages of its base layer (`08:1334` L5;
     licence:216 for the forms). With question Q3 (a) the offer states that the image
     holds no package under GPL, LGPL or AGPL.

   The toolchain module and the builder image get entries in the upstream register, as
   ADR-060 decisions 34 and 51 require of a shipped image and a baked-in tool (`08:1332`
   L3).
6. **Every image the builder writes** carries `THIRD_PARTY_LICENSES/`, the aggregated
   NOTICE texts, the MPL-2.0 source lines and an SBOM, in the layout of decision 5
   (`08:402`, `:1336` L7; ADR-060 decision 22); its base layer is question Q4. L8 has the
   release keep an archive of the exact source of every linked MPL-2.0 module and leaves
   to the licence record's Q7 whether a pointer to upstream is enough (`08:1337`;
   licence:41). So:
   - the builder writes, beside the image, the zip of each MPL-2.0 module the binary
     links, as it extracted and checked it (decision 8), and prints that whoever
     distributes the image keeps that archive;
   - the release pipeline keeps that archive for each release image as a release
     artifact (River and its sub-modules where a release image links them,
     licence:167), and a release check fails when a linked MPL-2.0 module of a release
     image has no zip there whose hash equals its pin;
   - for an image a site builds, keeping the archive is the site's duty as its
     distributor (`08:1337` L8; `08:1345` L16 says the same of a copyleft plugin's
     obligations), and `dist build` prints it. Whether a site's module source can stand
     in for the archive is not decided here; it waits for the licence record's Q7.

   Note of 2026-11-25, as built: the release check is `go run ./tools/mplsources <dir>`
   on the output directory of a release's second phase. It reads the signed manifest
   there, whose `modules` record what the release image links with its sum and detected
   licence, and fails, naming the module, when a module under MPL-2.0 has a sum that is
   the h1 hash of no zip under `mpl-sources/`. `TestReleaseAndBuilderImage`, which the
   CI job `builder-image` runs, runs it on the release it builds, and its own test holds
   the MPL-2.0 case of decision 14. It is a tool beside the builder, not builder code,
   since the builder is at its provisional cap (ADR-060 decision 35). The offline case of
   decision 14 still waits for L14 (decision 19).

### Modules on one VM and on an air-gapped site

7. **The module cache format.** `--modcache <dir>` (`08:402`; plugin record §11) names a
   directory in the download layout of the Go module proxy protocol: per module,
   `<escaped module path>/@v/`, holding per version `<version>.mod`, and for a version
   whose packages the build compiles also `<version>.info` and `<version>.zip`. No
   `list` file is needed, since every version the builder asks for is exact, and the
   offline case of decision 14 proves the layout. It is the layout of the
   `cache/download` directory of any Go module cache, so the Go toolchain writes it
   unchanged and any static file server can serve it. What it must hold is decision 9.
   - *Module sources.* The builder passes its sources to `go` as `GOPROXY`, in this
     order: the temporary source of decision 1 for `plugin smoke`; `file://<dir>`; and,
     once L14 clears (below), the first URL of the caller's `GOPROXY` variable, read as a
     value, never `direct`. With no source, the build fails before it resolves anything,
     so no default proxy is ever asked.
   - *Read-only.* The builder extracts into the module cache of its scratch directory,
     never into `<dir>`, which it mounts read-only on the image path; only the fill of
     question Q5 writes to `<dir>`.
   - *The private module path.* Until L14 clears (`08:1343`), every module source is a
     `file://` directory, and `dist build` refuses a proxy URL: the scratch module always
     requires the root module (decision 9), and `go` asks the next proxy for the private
     path whenever `<dir>` lacks a version (ADR-060 decision 21). The exit's file-based
     module proxy (ADR-060 decision 40) is such a directory.
   - A vendor directory is not a format: the build runs with `-mod=readonly` (`08:402`).
8. **What a module is checked against.** With `GOSUMDB=off`, the builder writes every line
   of the scratch `go.sum` before `go` runs, from recorded hashes: per version, the hash
   of its zip and the hash of its `go.mod` (the `/go.mod` line). The sources are the
   release's signed manifest, which records the `go.sum` lines of the release's own build
   beside its pins (ADR-060 decision 20); each entry of `plugins` with its `sum`
   (`08:401`); and the `go.sum` inside the zip of each module of `plugins`, the file its
   author resolved with. The build fails when a file the module source serves differs
   from a recorded hash, or when two sources record different hashes for one version. A
   version that no source records takes its hash from the module source: `dist build`
   prints it, and the signed manifest records it marked as checked against nothing,
   since no field of the site's `dist.yaml` can carry a hash and a refusal would leave
   the site no way to build. The hash of every module enters the SBOM and the pins of
   the signed manifest. Note of 2026-11-27 (S2b part, decision 40, its question Q14
   (a)): from S2b the signed manifest of a release also records, for each of the six
   modules the release serves, the hash of its zip and of its `go.mod` as the release's
   module source holds them, whether or not a release binary compiles the module. The
   pins hold these lines beside the `go.sum` lines of the release's build.
9. **What E7 needs** (`01:285`, `:384`).
   - In the site's registry, by digest: the artifact that `release` names; on the image
     path, the builder image it names (decision 1); the artifact of each in-process
     plugin, whose manifest `plugin install` reads before it answers `rebuild-required`
     (`08:397`); and a repository for the image the build writes. A by-digest reference
     names the release's registry, which an air-gapped site cannot reach: `dist build
     --mirror <prefix>=<site prefix>`, repeatable, rewrites the registry and repository
     prefix of each such reference and keeps its digest, so what is pulled is what the
     release named, and a pulled object whose digest differs fails the build. The script
     of `deploy/mirror/` (`08:1202`, assumption 3; `08:1284`) copies the release artifact
     and the builder image of the release beside the upstream digests.
   - In the module source (decision 7): the `.mod` of every version of the scratch
     module's pruned module graph that `go` loads, and the `.info` and `.zip` of every
     version whose packages the build compiles, the release's own modules included
     (question Q9); on the local path with question Q2 (a), the zip of the toolchain
     module too.
   - No index (none is configured by default, `08:405`) and no checksum database
     (decision 8). The project publishes no offline bundle (`08:1284`, `:1334` L5): the
     operator copies the release's artifacts and fills the module cache from a proxy it
     reaches, for its own use, as it copies the upstream digests.

### Strict versions

10. **`--allow-bump`.** The scratch `go.mod` requires each module the release pins at its
    pinned version, so version selection never lowers one. The builder compares the
    build list that selection gives with the pins of the release's signed manifest and
    fails on each pinned module whose version changed (`08:402`; `07:1860`; plugin
    record §11). `--allow-bump <module>` admits the raise of that one module when:
    - the module is not built by the release pipeline (question Q6);
    - the raise keeps the major version: a raise from `v0` to `v1`, which share one
      module path, or one that changes the major of a `+incompatible` version, fails
      with or without the flag;
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
    when the scan of decision 12 finds it reachable from an `init` function or a
    package-level variable initializer of any package that `go list -deps` of the server's
    main package lists ("a transitive `init`", `08:402`; `07:1860`), the package that
    declares the registry included. That is the written rule, and it holds unless the
    owner answers question Q10 otherwise. So `expvar`, `net/http/pprof`, `pgx/v5/stdlib`,
    `golang.org/x/net/trace`, the package `prometheus` of the Prometheus client (its own
    collectors) and `encoding/gob` (its basic types) each fail the build where the server
    links them, unless an exception of question Q7 names the call. None of them is in the
    package graph of `cmd/aiops` at `8e1f7b9`.

    | Kind (`08:402`) | A call of | Registry |
    |---|---|---|
    | A `database/sql` driver | `database/sql.Register` | The driver registry of `database/sql` |
    | A default-mux handler | `net/http.Handle`, `net/http.HandleFunc`; the methods `Handle` and `HandleFunc` of `*net/http.ServeMux` on `net/http.DefaultServeMux` | `DefaultServeMux` |
    | A default-registry collector | `Register` and `MustRegister` of `github.com/prometheus/client_golang/prometheus`, and the same methods on its `DefaultRegisterer`; the constructors of its `promauto` package that register with `DefaultRegisterer` | The Prometheus client's default registry |
    | A global codec | `RegisterCodec`, `RegisterCodecV2` and `RegisterCompressor` of `google.golang.org/grpc/encoding`; `encoding/gob.Register` and `RegisterName` | gRPC's codec and compressor registries; `gob`'s type registry |

12. **The scan.** It builds the call graph of the whole program with `golang.org/x/tools`
    at v0.51.0, the version of the root module's build list, with `x/mod` and `x/sync`
    (What was read), and follows the calls that question Q11 decides; until the owner
    answers, it follows static calls, interface method calls and calls through function
    values, the reading nearest to "finds a transitive `init`". No static scan follows a
    call made through reflection or `unsafe`. The scan stops a mistaken dependency, not
    hostile code, as the gate does (`08:508`). A first-party plugin has no `init` at all
    (`08:1257`).

### A self-built distribution's listeners and required set

13. **Where a site lists them** is question Q8. A sealed hook is "sealed to the
    distribution" (`07:439`), which is why a distribution the site builds may list
    listeners on it; W3 names the binding maps and the required set only (`08:403`;
    `00:257`). Whatever the answer, a site adds and never removes: the release's
    listeners, binding entries and required set stay in the merged manifest (`08:403`,
    `:451`). The builder evaluates check 7 on the merged set (`08:503`; ADR-060 decision
    17) and adds nothing to it: a raised key or row whose static need closure holds a
    provider outside the merged required set is denied by check 7, which names that
    provider, and the site raises it too. The signed manifest marks each site entry, so
    boot and `profile verify` print it with the departures from the release bindings
    without the release file.

### Tests

14. Beside the builder cases of ADR-060 decision 31, each failing on its base (ADR-060
    decision 32):
    - one builder: a `release` whose signed manifest is not the one `aiopsctl` embeds, on
      the local path, and an image whose `aiopsctl` embeds another release's manifest, on
      the image path; `plugin smoke` on a local directory, with the zip hash it prints;
      the comparison of decision 3;
    - the scan: a binary that links `crypto/sha256` passes, with `crypto/internal/boring`
      read as the Go licence, and a package graph that holds
      `crypto/internal/boring/syso` fails on the BoringSSL terms; `APACHE-LICENSE-2.0` of
      `go-diff@v1.1.0` is a licence file and a file `notice.go` is not; a toolchain tree
      that does not match the recorded hash is refused (question Q2); a licence or
      notice file of the toolchain tree missing from `THIRD_PARTY_LICENSES/`, the
      `svgpan` text among the cases; an embedded test binary's build list missing from
      the image SBOM; with question Q13 (a), a compiled `math` file whose notice is
      missing;
    - modules: a zip in `<dir>` whose hash differs from the recorded one, and one that
      differs from the `go.sum` of the `plugins` module that lists it; a `go.mod` whose
      hash differs from its `/go.mod` line; a version no source records, printed and
      recorded; a proxy URL in the module source before L14; an offline case that fills
      `<dir>` through the answer to question Q5, on the image path, and then builds from
      it with no network; a reference rewritten by `--mirror` whose pulled digest
      differs;
    - versions: `--allow-bump` on an SDK module (with question Q6 (a)), on an unchanged
      module and on a raise from `v0` to `v1`; a bump printed by `dist build`, recorded
      and printed at boot;
    - the deny-list: per kind, a fixture package that fails; the Prometheus client's and
      `gob`'s own registrations, which fail with question Q10 (a) and pass with an
      exception of question Q7; a registration reached through an interface method and
      one reached through a function value, which fail with question Q11 (a); a module
      that links gRPC v1.82.1, which fails on the default-mux kind without the tag and
      passes with `-tags grpcnotrace` and the `encoding/proto` exception (question Q7
      (a));
    - MPL-2.0: a release image whose archive lacks the zip of a linked MPL-2.0 module
      fails the release check;
    - the site's entries: each kind of site entry of question Q8, printed at boot and by
      `profile verify`; a raise whose need closure stays inside the merged required set
      passes, and one whose closure holds a provider outside it is denied by check 7,
      which names that provider.

## Consequences

- With the recommended answers, the builder image is the toolchain tree and `aiopsctl`
  on an empty base. The L13 items "the base layer and the SBOM tool of the builder image"
  close: no base layer and no third-party SBOM tool (`08:1342`).
- `aiopsctl` links `licenseclassifier/v2` with `go-spew` and `go-diff` for the scan, and
  `golang.org/x/tools` with `x/mod` and `x/sync` for the deny-list scan and the module zip
  of `plugin smoke`, and embeds the classifier corpus that question Q12 chooses, which
  gets a register entry as an embedded data set (ADR-060 decision 34). None enters a
  server binary. The builder package keeps its provisional cap of 3,000 lines (ADR-060
  decision 35).
- The artifact that `release` names carries the digest of the release's builder image;
  the signed manifest names no image. The signed manifest of a self-built distribution
  records each bump beside the release's pin and each hash checked against nothing, and
  marks each site entry of question Q8; boot prints the bumps and the site entries.
- With question Q2 (a), a host needs no installed Go on either path, and the local path
  runs the tree the image ships.
- An air-gapped site needs a connected machine once for each change of its build list,
  to fill the module cache; for the local path with question Q2 (a), the cache holds the
  toolchain module too.
- Every build compiles from an empty cache. A shared build cache can be added when build
  time matters, with the comparison of decision 3 kept as its proof.

### Edits on acceptance

The commit that records the owner's answers edits, for the recommended options: `08:401`
(the builder image named by the artifact that `release` names, decision 1; the shape of
`bindings`, question Q8); `08:402` (the `--modcache` format of decision 7, `-trimpath`,
`--mirror`, and the deny-list "as ADR-044 lists it"); `08:403` and `07:1874` (site
listeners, raises and bumps printed with the departures); `08:1284` (the release artifact
and the builder image in the list of the mirror script); `08:1342` (L13), `:1381` and
`:1407` (questions Q1, Q3 and Q12); plugin record §11 (`release`, the shape of
`bindings`, and "the SDK included" with question Q6); and `08:1175` and `00:778`
(status). In the same commit: ADR-060 decision 20 (the toolchain module with its hash,
the `go.sum` lines of the release's build, the bumps, the hashes checked against nothing
and the marked site entries in the signed manifest); decision 21 (the served root module,
question Q9; a file-based module source only until L14, decision 7); and decision 40 (the
root module beside the SDK modules in the exit's module proxy, question Q9). Question Q10
(b) or Q11 (b) would also edit `08:402`, `07:1860` and plugin record §11 to the narrower
reading. An option the owner did not choose edits nothing. The licence record keeps its
date; `08` §5 carries the reading.

## Questions for the owner

Each question gives its options, the recommended one, why, and when the answer is needed.

**Q1. Where do the licence scanner and the SBOM writer live?** The documents put a
licence scanner (go-licenses) and an SBOM generator into the builder image (`08:1381`,
`:1407`; licence:50), and ADR-060 decision 33 asks how the scan runs the same with a
local toolchain, where those tools are absent.
- (a) In the builder package. Detection links `github.com/google/licenseclassifier/v2`
  (Apache-2.0 at v2.0.0; it links `go-spew` v1.1.1, ISC, and `go-diff` v1.1.0, MIT with
  the Apache-2.0 text of the library it ports) with the corpus of question Q12. The SBOM
  is SPDX 2.3 in JSON, written with the standard library from the build list and the
  layer inputs. Both paths and the CI licence report (`08:1245`; ADR-060 decision 36) run
  the same code, and the image holds no scanner or SBOM tool.
- (b) The tools as written: go-licenses (Apache-2.0 at v2.0.1; twenty `require` lines)
  and syft for the image SBOM (Apache-2.0 at v1.57.2; 339 `require` lines, eight under
  `github.com/hashicorp/`), as binaries in the image; the local path refuses unless both
  are on the `PATH` at the pinned versions. Every module those binaries link travels in
  the image with its notices, and none of their transitive licences is read yet.
- (c) As (b), with cyclonedx-gomod (Apache-2.0 with a NOTICE at v1.12.0) for the binary's
  SBOM; it reads the Go build list only, so a base layer needs a second tool.
- Recommended: (a). The scan is the same by construction, the local path needs nothing
  installed, three linked modules replace hundreds of shipped ones (beside the three
  that the deny-list scan links whatever the answer, decision 12), and the SBOM item of
  L13 closes with first-party code. Needed before the licence scan merges.
- **Answered 2026-11-23: (a)**, decision 15.

**Q2. Which toolchain does the local path run?** The image holds "the pinned Go
toolchain" (`00:139`), `toolchain go1.26.8` in the root `go.mod` at `8e1f7b9`, and
`GOTOOLCHAIN=local` (ADR-060 decision 21) forbids a download by `go` itself. A host's Go
can report that version and hold another tree (Context), so a version check alone
proves neither the licence texts of decision 3 nor the binary.
- (a) The pinned toolchain module, never the host's. The local path takes
  `golang.org/toolchain` at the pinned version for the host's system and architecture
  from the module source, checks it against the hash that the signed manifest records,
  extracts it under the builder's own cache and runs its `go` with `GOROOT` set to it.
  This is the tree the image ships, so the licence texts and, with `-trimpath`, the
  binary are the same on both paths. The module zip is about 72 MB at go1.26.8
  `linux-amd64`, fetched once per release, and an air-gapped cache holds it.
- (b) The host's toolchain when its tree matches: the local path fetches the module as
  in (a) and compares every file of the module with the same path under the host's
  `GOROOT`, and refuses on any difference, a missing `LICENSE` included. An installation
  can hold directories the module lacks (Ubuntu's package has `api`, `misc` and `test`),
  which the comparison ignores. It costs (a)'s fetch plus the comparison, and still
  needs Go installed.
- (c) Any toolchain whose `go version` reports the pinned version, with the toolchain's
  licence texts taken from the module fetched as in (a), never from `GOROOT`. The binary
  can differ from the image's, so the comparison of decision 3 compares reports only.
- Recommended: (a). Both paths run one verified tree, the local job of the exit (ADR-060
  decision 30) then proves the image path byte for byte, and a self-built binary links
  the standard library the release tested. Needed before the local path merges.
- **Answered 2026-11-23: (a)**, decision 16.

**Q3. What is the base layer of the builder image?** It is UNVERIFIED until named and read
(`08:1342` L13; licence:258), and the image owes a source offer for the copyleft packages
in it (`08:402`).
- (a) None: the toolchain tree, `aiopsctl` and the files of decision 5 on an empty base.
  Nothing in a base layer to read or to offer source for. The image holds no shell and
  no CA bundle: where the module source is an `https` URL, `dist build` mounts the host's
  trust store read-only and points `SSL_CERT_FILE` at it, a variable that `crypto/x509`
  of the pinned toolchain reads; an air-gapped build needs no network. The builder runs
  the suites on the source (`08:402`), which at S2a are Go code built with cgo off. From
  S3, `sdk/testkit` gains embedded PostgreSQL and testcontainers-go (ADR-060 decision
  29): the PostgreSQL binaries are a download made at run time (licence:54), which an
  air-gapped site cannot make, and testcontainers-go needs a container runtime; whether
  either runs on an empty base is not read here, so the base is asked again before the
  S3 suites run in the builder.
- (b) A minimal base layer of distribution packages that holds a CA bundle. Its package
  licences are not read here; each is read from the package's copyright file in the
  image at its pinned digest before it ships, and the offer covers each copyleft one.
  The S3 question above stays open with it.
- (c) The official Go image, which holds git, a GNU userland and a C compiler (licence:50)
  that the builder never runs, and needs the largest offer.
- Recommended: (a). It satisfies every duty of `08:402` with nothing left to read, and at
  S2a the builder runs no program a base layer would bring; the S3 suites reopen the
  question with the base they need read then. Needed before the image definition merges
  under `dist/`.
- **Answered 2026-11-23: (a)**, decision 17.

**Q4. What is the base layer of the server images the builder writes?** ADR-060 decision
34 has it read before S2a ends, and no document names it.
- (a) None at S2a: the server binary, built with cgo off, and the files of decision 6.
  No first-party row of the S2a images dials a TLS endpoint (ADR-060 decisions 27 and 38
  list them), so a trust store, time zone data or any other file enters the image with
  the first row that needs it, under its own register entry, as an input arrives with
  its consumer (`08:678`). A site image may hold a third-party in-process row (W3,
  `08:403`) that needs a trust store or zone data. `crypto/x509` and `time` of the pinned
  toolchain read `SSL_CERT_FILE` and `ZONEINFO` at run time, so the site mounts both
  files and sets both variables in its own deployment files, which `dist use` does not
  write yet (ADR-060 decision 29).
- (b) The base of question Q3 (b), from S2a.
- Recommended: (a). Nothing is shipped before a row reads it, the image owes no source
  offer, and a site's third-party row gets its files at run time without a second image.
  Needed before the builder writes its first image.
- **Answered 2026-11-23: (a)**, decision 18.

**Q5. How does a site fill the module cache?** `08:402` gives the flag and no way to fill
it, and the project publishes no offline bundle (`08:1284`). Until L14 clears nothing is
published (ADR-060 decision 32), every module source is a directory (decision 7), and the
exit fills its own.
- (a) One flag, once L14 clears. With `--modcache <dir>` and a proxy URL in the caller's
  `GOPROXY`, the builder's sources are `file://<dir>` and then that proxy (decision 7);
  `<dir>` must then be writable, the image path mounts it read-write in that case only,
  and after a successful build the builder copies into `<dir>` the files the build
  fetched, in the layout of decision 7, so `<dir>` holds what decision 9 asks. A build
  on a connected machine fills the cache that the air-gapped machine then reads with no
  proxy. Without a proxy URL, `<dir>` is read-only and never written.
- (b) A command of its own, `aiopsctl dist fetch <dist.yaml> --modcache <dir>`, on a
  connected machine; `dist build --modcache` never fetches. It adds a command to those
  of `00:137` and ADR-060 decision 23.
- (c) No support in `aiopsctl`: the site fills `<dir>` with the Go toolchain on a
  connected machine, from a `go.mod` and `go.sum` that `dist build` prints.
- Recommended: (a). The flag stays as written, no command is added, and the fill is the
  resolution the build itself makes, so it holds the graph versions' `go.mod` files that
  an offline build needs and a copy of the build list alone would miss. Needed before
  `--modcache` merges.
- **Answered 2026-11-23: (a)**, decision 19.

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
- **Answered 2026-11-23: (a)**, decision 20.

**Q7. What does "a global codec" cover, and where do exceptions live?** gRPC registers
its own proto codec from `init` in every binary that links it, and links
`golang.org/x/net/trace`, whose `init` puts two handlers on the default mux, unless the
build sets the tag `grpcnotrace` (What was read). The remote runtime of S2b links gRPC
into the server (`07:1097`). Every generated protobuf package registers its types with
`protoregistry` from `init`, and `image/png` registers an image format.
- (a) The table of decision 11, with `gob`'s type registry under "a global codec".
  Exceptions are a table in the builder package, one entry per registering package and
  function with its reason, changed only by a reviewed pull request; a site adds none.
  It is empty at S2a. When gRPC enters the server binary, the builder builds every
  server binary with `-tags grpcnotrace`, which takes `x/net/trace` and its two handlers
  out of the graph, and the table gains `google.golang.org/grpc/encoding/proto` with
  `RegisterCodecV2`. The cost: `grpc.EnableTracing` does nothing in a server binary, and
  the tag reaches every module of the build. Image format and protobuf type registries
  are not on the list.
- (b) As (a), with no tag: the table also gains `golang.org/x/net/trace` with its two
  `net/http.HandleFunc` calls, so every server binary that links gRPC holds
  `/debug/requests` and `/debug/events` on its default mux.
- (c) As (a), with `image.RegisterFormat` and the protobuf registries on the list; every
  generated protobuf package then needs an exception.
- (d) No exceptions. The server binary then cannot link gRPC, which contradicts the
  remote runtime; listed only to be refused.
- Recommended: (a). It denies the four kinds as written, keeps the release buildable
  with one exception where (b) needs two, and leaves no default-mux handler in any server
  binary. Needed before the deny-list scan merges.
- **Answered 2026-11-23: (a)**, decision 21.

**Q8. Where does a self-built distribution list sealed-hook listeners and raise the
required set?** `dist.yaml` has four fields and `bindings` is `{providers, consumers}`
(`08:401`). A sealed hook is "sealed to the distribution" (`07:439`), and every sealed
hook is a kernel hook (`internal/plugin`, `internal/status`, `internal/service`,
`internal/config`, `internal/update`, `internal/commit`) or a loader hook
(`loader/patch-context`, `loader/entry-init`, `loader/volatile-update`) (`07:464`; plugin
record §8); three of the kernel's are waterfalls, and a failing `internal/commit`
listener aborts an update. The release input lists the listeners and the required set
(ADR-060 decision 20), ADR-060 decision 33 asks where a site lists both, and W3 allows
the raise (`08:403`). ADR-059 decision 20 reads the listeners as "who is allowed", with
the bindings.
- (a) `bindings` gains `sealedHooks`, the listeners a site adds per hook, and `required`,
  the keys and rows it adds; the four fields stay. Edits `08:401` and plugin record §11.
- (b) Two new top-level fields, `sealedHooks` and `required`; "four fields" becomes six
  in `08:401`, `07:1844`, `02:207`, plugin record §11 and ADR-060 decisions 20 and 22.
- (c) No site field. A site raises the required set only with `required: true` on a row
  in its profile layer (`08:146`, `:451`), which names rows and not keys, and adds a
  listener only through a release-form input that restates the whole release. A raise
  by key is then unavailable, which narrows W3.
- Recommended: (a). The four fields hold, the listeners sit with the other "who is
  allowed" lists, and both the raise that W3 allows and the listeners that `07:439`
  leaves to the distribution have a place. Needed before the builder reads the site's
  `dist.yaml`.
- **Answered 2026-11-23: (a)**, decision 22.

**Q9. Is the root module served beside the SDK modules?** The scratch module requires the
root module at the release's version (Context, this record's reading), and the root
`go.mod` at `8e1f7b9` requires the SDK modules at `v0.0.0` behind `replace` lines, which
Go ignores in a dependency. ADR-060 decision 21 serves the three SDK modules only, and
decision 40 lists what the exit's module proxy serves.
- (a) Yes. A release serves the root module at the release version, from the same module
  source as the SDK modules, with a served `go.mod` that names the SDK version and holds
  no `replace` line, under the test of ADR-060 decision 21. Until L14 it is served only
  where the SDK modules are, inside the exit job (ADR-060 decision 40). Edits ADR-060
  decisions 21 and 40.
- (b) The release's `dist.yaml` artifact carries the root module's zip as a layer, and the
  builder adds it to its module source.
- Recommended: (a). Every module a build resolves comes through one module source, and
  the exit's test release needs it either way (ADR-060 decision 38). Needed before the
  first build from a scratch directory.
- **Answered 2026-11-23: (a)**, decision 23.

**Q10. Does a registry package's own registration count?** `08:402` fails the build on "a
transitive `init` that registers ... a default-registry collector or a global codec",
with no exception for the package that declares the registry. The `init` of the
Prometheus client's package `prometheus` registers its process and Go collectors with its
default registry (`client_golang@v1.23.2`, `prometheus/registry.go:60-63`), and
`encoding/gob` registers its basic types with its own type registry. Neither is in the
server's package graph at `8e1f7b9`.
- (a) It counts, as written (decision 11). A server binary that links either fails
  unless an exception of question Q7, approved in review, names the call; the Prometheus
  client gets one when a first-party row first links it.
- (b) It does not count: a call from the package that declares the registry is exempt,
  since that package fills only its own registry and cannot be linked without doing so.
  This narrows `08:402`, `07:1860` and plugin record §11, which are edited to say so.
- Recommended: (a). It keeps the written requirement, S2a links neither package, and
  each such registration becomes a reviewed exception. Needed before the deny-list scan
  merges.
- **Answered 2026-11-23: (a)**, decision 24.

**Q11. Which calls does the deny-list scan follow?** "Finds a transitive `init`"
(`08:402`) sets no limit, and a static scan cannot follow a call made through reflection
or `unsafe`.
- (a) Static calls, interface method calls and calls through function values, on the
  call graph that `golang.org/x/tools/go/callgraph/vta` builds over the SSA form of the
  whole program (decision 12). An edge that over-approximates a dynamic call and reaches
  a registration is a finding, cleared only by an exception of question Q7.
- (b) Static calls only: a registration reached through an interface or a function value
  passes. This narrows `08:402`, `07:1860` and plugin record §11.
- Recommended: (a). It is the reading nearest to the written text, and a false finding is
  visible and cleared in review. Needed before the deny-list scan merges.
- **Answered 2026-11-23: (a)**, decision 25.

**Q12. Which licence texts does the classifier carry?** With question Q1 (a) the
classifier needs a corpus, and ADR-060 decision 34 admits an embedded data set only with
its licence read at a primary source. The module's own corpus (What was read) is 431
files with no stated source; thirty state the FSF's verbatim-copy term, and the others
state no terms. L7 denies "any custom text" in a shipped artifact (`08:1336`) and does not
say whether a licence text carried as data, to recognise that licence, is one.
- (a) The module's corpus, all 431 files, linked through its package `assets`. L7 is read
  as governing the code a binary links, not a licence document carried to recognise it,
  and the owner records that reading by name. The register entry records the FSF term of
  the thirty texts, the `GPL-3.0` copy that ends at "END OF TERMS AND CONDITIONS", and
  that the other texts state no terms. A table in the builder package maps the
  classifier's names to the SPDX ids of L16.
- (b) A corpus built from the SPDX License List data at a pinned version and embedded by
  the builder package; the classifier is linked without its package `assets`, and its
  names are SPDX ids, so nothing maps them. The data's terms are read at a primary source
  before the classifier merges; ADR-060 decision 34 already has the SPDX list data read
  before S2a ends. The header and supplement variants of the module's corpus are not
  carried, so more texts classify as `unknown`, which fails the build (L16), the safe
  direction.
- (c) The texts of the L7 allow-list only. A text outside it is `unknown`, so a site
  cannot accept a licence by name in `acceptLicenses` (L16); listed only to be refused,
  since it narrows L16.
- Recommended: (b). The corpus then has a source whose terms are read, as decision 34
  asks, and its names are the ids that L16 records. Needed before the licence scan
  merges.
- **Answered 2026-11-23: (b)**, decision 26.

**Q13. Do the header notices of the standard library enter `THIRD_PARTY_LICENSES/`?**
Sixteen standard-library files that the server's package graph compiles carry a
third-party notice in their header and no licence file (What was read): the MIT
permission of `runtime/memmove_amd64.s`, which asks that the notice "shall be included
in all copies or substantial portions of the Software", and Sun Microsystems' notice on
fifteen `math` files, granted "provided that this notice is preserved". The per-directory
scan of decision 3 reads licence files only, and the Go licence that governs those
packages reproduces neither notice.
- (a) The release pipeline generates, at each toolchain pin and by a walk of the tree,
  the list of standard-library files whose header carries a notice other than the Go
  Authors'; the builder copies the notice of each such file that a binary's package
  graph compiles into that binary's `THIRD_PARTY_LICENSES/`, as a notice file, not
  classified (decision 3).
- (b) No: the Go licence alone stands for the standard library in
  `THIRD_PARTY_LICENSES/`, and `08` §5 records that reading with its reason.
- Recommended: (a). It carries notices whose own terms ask to be carried, costs one
  generated list per toolchain pin, and does not wait for counsel to be safe. Needed
  before the builder writes its first image.
- **Answered 2026-11-23: (a)**, decision 27.

## Owner answers of 2026-11-23

The project owner accepted this ADR on 2026-11-23 and answered every question of the
section "Questions for the owner" on the same date, each with its recommended option:
(a) for every question but Q12, and (b) for Q12. Each decision below names the question
it answers. Where a decision above leaves a point to a question, the decision here
settles it.

15. **The licence scan and the SBOM writer live in the builder package** (Q1 (a)).
    Detection links `github.com/google/licenseclassifier/v2` at v2.0.0, with `go-spew`
    v1.1.2-0.20180830191138-d8f796af33cc and `go-diff` v1.4.0, the versions the build
    list selects, whose licences were read at those versions (owner, 2026-11-25), over
    the corpus of decision 26. The SBOM is SPDX 2.3 in JSON, written with the standard
    library from the build list and the layer inputs. Both paths and the CI licence
    report (`08:1245`; ADR-060 decision 36) run the same code, and the builder image
    holds no scanner and no SBOM tool.

16. **The local path runs the pinned toolchain module, never the host's** (Q2 (a)). It
    takes `golang.org/toolchain` at the pinned version for the host's system and
    architecture from the module source, checks it against the hash that the signed
    manifest records, extracts it under the builder's own cache and runs its `go` with
    `GOROOT` set to it. Both paths run the tree the image ships, a host needs no
    installed Go, and the module cache of an air-gapped site holds the toolchain module
    for the local path (decision 9).

17. **The builder image has no base layer** (Q3 (a)): the toolchain tree, `aiopsctl` and
    the files of decision 5 on an empty base, with no shell and no CA bundle. Where the
    module source is an `https` URL, `dist build` mounts the host's trust store read-only
    and points `SSL_CERT_FILE` at it. The source offer of decision 5 states that the
    image holds no package under GPL, LGPL or AGPL. The base layer is asked again before
    the S3 suites run in the builder.

18. **The server images the builder writes have no base layer at S2a** (Q4 (a)): the
    server binary, built with cgo off, and the files of decision 6. A trust store, time
    zone data or any other file enters the image with the first row that needs it, under
    its own register entry. A site whose third-party in-process row needs a trust store
    or zone data mounts the files and sets `SSL_CERT_FILE` and `ZONEINFO` in its own
    deployment files.

19. **A site fills the module cache through `--modcache`, once L14 clears** (Q5 (a)).
    With `--modcache <dir>` and a proxy URL in the caller's `GOPROXY`, the module sources
    are `file://<dir>` and then that proxy (decision 7); `<dir>` must then be writable,
    the image path mounts it read-write in that case only, and after a successful build
    the builder copies into `<dir>` the files the build fetched, in the layout of
    decision 7. Without a proxy URL, `<dir>` is read-only and never written. No command
    is added.

20. **`--allow-bump` never names a module the release pipeline builds** (Q6 (a)). The
    three SDK modules and the root module fail strict versions with or without the flag,
    and an author requires the SDK version of the release the plugin targets, which
    `plugin smoke --against` proves before a site builds. Note of 2026-11-27: from
    S2b the SDK modules are five, with the nested modules `sdk/remote` and
    `sdk/remotehost` (ADR-063 decision 43), and the rule holds for each.

21. **"A global codec" and the exceptions** (Q7 (a)). The table of decision 11 holds,
    with `gob`'s type registry under "a global codec"; image format and protobuf type
    registries are not on the list. The exceptions are a table in the builder package,
    one entry per registering package and function with its reason, changed only by a
    reviewed pull request; a site adds none, and the table is empty at S2a. When gRPC
    enters the server binary, the builder builds every server binary with `-tags
    grpcnotrace`, and the table gains `google.golang.org/grpc/encoding/proto` with
    `RegisterCodecV2`; `grpc.EnableTracing` then does nothing in a server binary. Note
    of 2026-11-25: on the owner's word, the table also holds the three registrations
    with `encoding/gob.Register` that sigstore-go brings in through go-openapi (ADR-020
    decision 16): the `init` of `encoding/gob`, which registers gob's basic types
    (`registerBasics` in `type.go`), at the toolchain pin, and the `init` of
    `github.com/go-openapi/loads` at v0.25.0 (`spec.go`) and of
    `github.com/go-openapi/spec` at v0.22.9 (`operation.go`), each registering
    `map[string]any` and `[]any`. Each is keyed by package and version, fills only gob's
    own type registry, which no row of a server uses, and counts under decision 24; a
    version change of the toolchain or of either module needs a new review. The register
    entry of sigstore-go records them, and ADR-020 decision 27 carries the same note.

22. **A self-built distribution lists sealed-hook listeners and its raise of the required
    set in `bindings`** (Q8 (a)). `bindings` gains `sealedHooks`, the listeners a site
    adds per hook, and `required`, the keys and rows it adds; `dist.yaml` keeps its four
    fields.

23. **The root module is served beside the SDK modules** (Q9 (a)). A release serves the
    root module at the release version, from the same module source as the SDK modules,
    with a served `go.mod` that names the SDK version and holds no `replace` line, under
    the test of ADR-060 decision 21. Until L14 clears it is served only where the SDK
    modules are, inside the exit job (ADR-060 decision 40). Note of 2026-11-27: from
    S2b a release serves six modules, the five SDK modules and the root module
    (ADR-063 decision 43). The S2b part of this ADR, which ADR-063 decision 34 lists,
    is a dated addition, the last section of this file, proposed apart and accepted by
    the owner on 2026-11-27.

24. **A registry package's own registration counts** (Q10 (a)), as decision 11 writes. A
    server binary that links the package `prometheus` of the Prometheus client or
    `encoding/gob` fails unless an exception of decision 21 names the call; the
    Prometheus client gets one when a first-party row first links it.

25. **The deny-list scan follows static calls, interface method calls and calls through
    function values** (Q11 (a)), on the call graph that
    `golang.org/x/tools/go/callgraph/vta` builds over the SSA form of the whole program
    (decision 12). An edge that over-approximates a dynamic call and reaches a
    registration is a finding, cleared only by an exception of decision 21. Note of
    2026-11-26 (review round over the whole step): a function or closure that a reached
    call of `time.AfterFunc`, `context.AfterFunc`, `runtime.SetFinalizer` or
    `runtime.AddCleanup` hands to the runtime is reached too, since the runtime calls it
    through no edge of the graph; one that a variable or a parameter carries to that call
    is not. Following every function a reached function refers to as a value was tried
    and refused: it finds closures that inits store as test hooks in gRPC and the HTTP
    server's handler, which no init runs. Note of 2026-11-27 (round 2): so is a sequence
    that a reached call of `iter.Pull` or `iter.Pull2` hands to the runtime's coroutine,
    whose start has no body in the SSA form. Following each function that a reached call
    passes to a function with no body, as suggested, was not taken: such a function need
    not call it.

26. **The classifier carries a corpus built from the SPDX License List data** (Q12 (b)),
    at a pinned version and embedded by the builder package. The classifier is linked
    without its package `assets`, and its names are SPDX ids, so no table maps them. The
    terms of the data are read at a primary source before the classifier merges. The
    header and supplement variants of the module's corpus are not carried; a text the
    corpus cannot name is `unknown` and fails the build (L16). Note of 2026-11-26 (review
    round over the whole step): the classifier names a text at a confidence of 0.9 or
    more with up to a tenth of its words foreign, and its tokenizer drops a copyright
    line whole, so a text is also `unknown` when it holds a word that the text it names
    does not, by the longest common run of their words, other than in place of words of
    a variable of the SPDX template (any words where the variable's match takes any
    text, else words of its match), or a word on a copyright line beyond its copyright
    statements and a holder of ten words. The corpus carries each template's variables,
    their original text and match, for that.

27. **The header notices of the standard library enter `THIRD_PARTY_LICENSES/`** (Q13
    (a)). The release pipeline generates, at each toolchain pin and by a walk of the
    tree, the list of standard-library files whose header carries a notice other than
    the Go Authors'. The builder copies the notice of each such file that a binary's
    package graph compiles into that binary's `THIRD_PARTY_LICENSES/`, as a notice file,
    not classified (decision 3).

**The gRPC codec exemption from S2a, a consequence of ADR-020.** ADR-020, accepted the
same day, has `aiopsctl` and the server image link sigstore-go from S2a, the gRPC runtime
among them (ADR-020 decision 16 and its Consequences), and has the builder build with the
tag `grpcnotrace` and exempt, by package and pinned version, the codec registration of
`google.golang.org/grpc/encoding/proto` (ADR-020 decision 27, its question Q9 (a)).
Decision 21 and question Q7 take gRPC to enter with the remote runtime of S2b. From S2a,
therefore, the exceptions table of decision 21 holds the entry of
`google.golang.org/grpc/encoding/proto`, keyed by package and pinned version, which the
register entry records and which is read again at every version bump, and the tag
`grpcnotrace` applies to every binary the builder writes, `aiopsctl` included. This is
ADR-020's decision; no option of this ADR changes.

**What ADR-020 asks of the builder.** ADR-020, accepted the same day, decides the
following of this ADR (ADR-020 decision 18), and this record carries it as decided there:
- *The release build runs in two phases* (ADR-020 decision 13). It takes the release
  public key as an input. The builder writes the distribution manifest and stops, the
  release pipeline signs it outside the builder with `cosign sign-blob`, and the builder
  then links the release binaries, the server of the release image and the release
  `aiopsctl`, with the manifest, its bundle and the release public key embedded. `dist
  build` for a site takes no release key and links in one phase.
- *`dist build` also writes the site's `aiopsctl`* (ADR-020 decision 29, its question Q11
  (a)) for the host's platform: the `cli` profile of the same module table (`08:359`),
  embedding the same manifest, so that after `dist use` it boots on the current
  distribution record.
- *The scratch main module* (ADR-020 decision 12) holds the module table and no plugin,
  its `go.mod` holds no `replace` line, and every module of the table, the root module
  included, is reached at a version, so that build information records a sum for each.
- *The one exemption of the `init` deny-list* is that of ADR-020 decision 27, as the
  paragraph above states; the note of 2026-11-25 to decision 21 adds the three gob
  registrations that the owner approved.
- *The licence scan* honours the record of ADR-020 decision 30, reading 4: CC-BY-SA-4.0
  for `README.md` and `CONTRIBUTING.md` of `github.com/opencontainers/go-digest@v1.0.0`
  only, by module, version and file.

**Edits made with this record.** The commit that records these answers makes the edits
that "Edits on acceptance" names for the options chosen: `00:778` (status); `07:1874`;
`08:401`, `:402`, `:403`, `:1284`, `:1342` (L13), `:1381` and `:1407`; plugin record
§11, where it gives `release`, the shape of `bindings` and "the SDK included"; and
ADR-060 decisions 20, 21 and 40. With `08:402` it closes the clause that left the base
layer and the SBOM tool of the builder image UNVERIFIED. Two lines that restate edited
text are edited with them: `08:1236`, the layout line of `deploy/mirror/` (decision 9),
and `08:1390`, since the libraries of the licence scan are linked into `aiopsctl`
(decision 15). Left to a later commit: `08:1175`, the row of this ADR in the ADR table of
`08` §3, which the commit that updates that table for the ADRs accepted on 2026-11-23
edits. The licence record keeps its date. An option the owner did not choose edits
nothing.

**The classifier's versions, edited on the owner's word of 2026-11-25.** Decision 15
named `go-spew` v1.1.1 and `go-diff` v1.1.0 as accepted. The build list of the root
module selects `github.com/sergi/go-diff` v1.4.0 and `github.com/davecgh/go-spew`
v1.1.2-0.20180830191138-d8f796af33cc, which `certificate-transparency-go`, linked through
sigstore-go (ADR-020 decision 16), requires; the builder links what the build list
selects. Both licences were read again at those pins, MIT with the `APACHE-LICENSE-2.0`
text of the ported library for go-diff and ISC for go-spew, and the register entry of the
classifier and licence:57 record them. The owner answered on 2026-11-25 that decision 15
names the selected versions, and it is edited to them, with `08:1386`, which restates
it.

## S2b part, proposed 2026-11-27

- Status: **accepted 2026-11-27 by the project owner.** The owner answered questions
  Q14 to Q19 on that date with the recommended option of each, (a); the section "Owner
  answers of 2026-11-27" at the end of this part records the answers as decisions 40 to
  45. ADR-063 decision 2 required this acceptance before the code this part governs
  merges into `main`. The proposal was revised on the same date after its review
  round, before the owner answered: decisions 28 to 37 were corrected, and decisions
  38 and 39 and questions Q18 and Q19 were added. The heading of this part keeps the
  date of the proposal.
- Scope: the S2b part of this ADR, as ADR-063 decision 34 lists it: the release modules
  the builder serves; the suite binaries that drive a remote process; the writer of the
  new `serves` fields, `remoteProtocol` and the `remote` mark (ADR-063 decision 46); and
  the gRPC codec exemption, read again if the pin of `google.golang.org/grpc` moves
  (ADR-020 decision 27). The module table that the builder generates gains the proxy
  plugin at S2b (ADR-063 decisions 12, 22 and 41). No item of that list names it, and
  decision 39 covers it, since the builder writes that table. Decisions 1 to 27 stand.
  This part changes no decision of `00-decisions.md`; every line an answer would edit is
  named in "Edits on acceptance of the S2b part".
- It blocks: the builder change that serves `sdk/remote` and `sdk/remotehost`; the
  generated module table with the proxy plugin; the reader and the writer of the new
  `serves` fields, which change in one commit, and with them the majors clause of check
  3 and check 9; steps 4 and 5 of `plugin check` for the runtime `remote`; and the suite
  step of `plugin verify` for remote code.
- Citations: in this part `00`, `07` and `08`, and every path with a line, are at commit
  `bbfd839`, where the sections above cite `8e1f7b9`: the builder paragraph is `08:402`
  above and `08:404` here. ADR-020, ADR-047, ADR-048, ADR-059, ADR-060, ADR-061,
  ADR-062 and ADR-063 are cited by decision. The facts on gRPC name
  `google.golang.org/grpc@v1.82.1`, whose files were read on 2026-11-27 as `go mod
  download -json` placed them in the Go module cache, at the sum
  `h1:NnAxzGRA0677vCa4BUkOAnO5+FfQqVl9iUXeD0IqcGE=` that `go.sum:402` records. This part
  states no licence fact of its own: the readings of grpc-go and protobuf-go at their
  pins are ADR-063 decision 35's.

### Context of the S2b part

ADR-063 fixes what S2b asks of the builder. This part reopens none of it but the two
points that questions Q16 and Q19 put to the owner:

- From S2b the SDK is five Go modules: `sdk/remote` and `sdk/remotehost` are nested
  modules, each with a closed require list, and the builder serves six modules (ADR-063
  decision 43; `07:23`; `00:263`). Each new module gets a served `go.mod` copy and its
  place in the builder's list of release modules (ADR-063 decision 7). The generated Go
  of a `contrib` definition lives in the module `sdk/remote`, under
  `sdk/remote/def/<name>/v<N>` (ADR-063 decision 44; `07:28`).
- gRPC and Protocol Buffers are imported from S2b by the remote modules, the proxy
  plugin, the adapter generator and the check runner of `aiopsctl` (ADR-063 decision 42;
  `00:268`).
- `aiops/remote` is a plugin of the release module table. One plugin value per locked
  remote id is built with `plugin.FromManifest` and its code, and the launcher hands the
  loader the function that builds them, which stays with the loader alone (ADR-063
  decisions 10, 12, 22 and 41).
- `serves` gains `remoteProtocol`, the majors the binary speaks, and per served
  definition its `remote` mark, `contrib`, `callback` or `no`. The reader and the
  builder change in one commit, and `distVersion` stays 1 (ADR-063 decision 46). The
  majors clause of check 3 and the contribution-set clause of check 9 read the two
  fields at every entry point but `dist build` (ADR-063 decision 16). A `uses` entry of
  a key that is not marked `callback`, a bound key included, is denied by check 9
  (ADR-063 decisions 10 and 28).
- For the runtime `remote`, in every language, step 4 of `plugin check` starts the
  plugin's entrypoint as a local process and drives it over a Unix socket, and step 5
  runs the obligations half of each suite through the adapter against that process,
  with no Go toolchain for a non-Go plugin (ADR-063 decision 17; `07:1577-1578`;
  `00:305`). The subject of the suite is the proxy fiber, and the guard half never runs
  remote (ADR-063 decision 14). One implementation of the kernel side of the protocol
  serves a server and `plugin check` (ADR-063 decision 9). Only the check runner opens
  the socket, to a process that it started, and on it the handshake and check 3 run as
  on a server (ADR-063 decisions 23 and 54, reading 2). `plugin verify` re-runs the
  suite against a pulled image (ADR-063 decisions 17 and 28).
- `make plugin-check` and CI run `plugin check` and a T3 boot on every plugin under
  `plugins/` and `examples/`, the remote examples in Go and in Python included (ADR-063
  decision 31), and a case that needs the suite binaries of a release build runs on the
  `aiopsctl` of the test release (ADR-063 decision 37).

The code at `bbfd839` holds none of it yet, and reading it against those decisions
shows what this part must settle:

- The release modules are one list of four: the root module, `sdk`, `sdk/kernel` and
  `sdk/testkit` (`internal/builder/build.go:94`). Five rules read that list: the served
  copy of a `go.mod` and the set of modules served (`internal/builder/serve.go:32`,
  `:54`); the `go.sum` inside each served zip, a source of recorded hashes
  (`build.go:222-224`; decision 8); the lines of a release's own modules, which a
  release build takes as go writes them (`build.go:578`); strict versions
  (`build.go:666-668`, `:677`; decision 20); and the owners `aiops` and `contrib`,
  admitted only from a package of one of these modules
  (`internal/builder/manifest.go:80`; `08:404`). The served copy of a `go.mod` rewrites
  the require lines that the file holds for a release module and adds none
  (`serve.go:31-36`), and one serving writes every module of the list (`serve.go:52-59`).
- `sdk/remote/handshake`, the frozen proto with its freeze test, is a directory of the
  module `sdk` (`Makefile:161`), and a served zip leaves out every directory that holds
  a `go.mod` of its own (`serve.go:87`).
- The pins of a signed manifest are the `go.sum` lines of the build (`build.go:559-586`,
  `:608-645`; ADR-060 decision 20). Go writes the hash of a zip only for a module whose
  packages the build lists, and the hash of a `go.mod` for every module of the graph it
  loads. At S2a a release build lists packages of each of the four modules, `sdk/testkit`
  through its suite binaries (`internal/builder/suites.go:76`; `build.go:560`). At S2b
  the server, `aiopsctl` and their suite binaries drive the process of a plugin and
  hold no plugin-side host, which is what `sdk/remotehost` is (`07:32`).
- A site's build records hashes from three sources: the pins of the release's signed
  manifest (`build.go:256-266`), the `sum` of each entry of `plugins`, and every line
  of the `go.sum` inside the zip of each module of `plugins` (`build.go:402`,
  `:369-386`; decision 8). It marks a hash as checked against nothing only when none of
  the three records it (`build.go:579`), and it fails when two of them differ
  (`build.go:543-544`).
- The release `aiopsctl` embeds four test binaries: the pack suites of `commands`,
  `echoers` and `stampers`, and the echo scenario (`suites.go:31-41`, `:163-177`;
  ADR-060 decisions 46 and 49). Each runs the rows of a pack on first-party
  instantiable plugins, as a child process of `aiopsctl` with its input in three
  variables of the environment (`internal/authoring/tools.go:475-504`), and none opens a
  connection. The toolbox takes an embedded binary only for a directory that holds no
  Go code and compiles the suite in the author's module otherwise
  (`tools.go:439-463`). An `aiopsctl` built in the repository, or by a site's build,
  embeds none and answers `launch.ErrNotCarried` (`tools.go:476-484`; ADR-060 decision
  46).
- The request of the toolbox part that runs a suite names a directory, the definitions,
  the halves and the fixtures, and no runtime (`sdk/def/launch/v0/toolbox.go:265-274`).
- `make plugin-check` builds its `aiopsctl` in the repository and fails on any step of
  any plugin whose outcome is neither a pass nor not applicable (`Makefile:139-150`;
  `cmd/aiopsctl/check_test.go:116-121`).
- The launcher's toolbox is a value of the `aiopsctl` process (`boot/launch.go:226-227`),
  and ADR-063 decisions 16 and 57 have the check runner reach the handshake entry of
  the gate "through the toolbox it already holds (`sdk/def/launch/v0/toolbox.go:31-33`)".
  Those three lines are the method `Gate` of `Toolbox`. A suite runs in a child
  process, which holds no toolbox.
- The generated module table holds, per plugin, a value and a manifest: the builder
  writes `dist.Module{Plugin: pN.<accessor>, Manifest: mN}` (`internal/builder/gen.go:30`;
  `dist/dist.go:46-49`), and it takes a package into the table only when the package
  declares `var Plugin` or `func New()` (`build.go:444-448`;
  `internal/plugingen/plugingen.go:235-254`). The values reach the loader with the
  config of the loader (`boot/run.go:93-99`; `dist/dist.go:95-97`). Nothing carries a
  function that builds a value from a manifest.
- One function writes `serves` for every signed manifest, of a release, of a site and
  of a binary built in the repository: `plugingen.Serves`, which `Manifest.Write` calls
  on the `definition.yaml` of each package of the server's graph
  (`internal/plugingen/defs.go:157-167`; `internal/builder/manifest.go:59-70`, `:108`).
  It reads the `remote` mark of a definition and drops it (`defs.go:30`, `:85`,
  `:137-144`), and it takes `pluginApi` from the constant compiled into the builder
  (`defs.go:158`). Nothing in the repository lists the majors a binary speaks. The
  reader refuses an unknown field of `serves` and of a served major
  (`boot/distribution.go:241-242`, `:265-267`). A hook already carries its `remote` mark
  in `serves` (`defs.go:46-50`; `boot/distribution.go:107-110`). An in-process manifest
  lists built-in keys of the kernel in `uses`, as `logger` and `registry` in
  `plugins/restart-policy/plugin.yaml:20-21`.
- The exception of the `init` deny-list for gRPC's codec is keyed by package, module
  and version, v1.82.1 (`internal/builder/initscan.go:61-63`). That is the version of
  the root build list, where gRPC is an indirect requirement (`go.mod:102`). The scan
  reads the graph of the server's main package alone (`build.go:161`), and a finding
  names the call and the `init` it is reached from, not the version an exception was
  read at (`initscan.go:79-85`).
- The graph of `cmd/aiops`, listed with the tag `grpcnotrace`, already holds
  `google.golang.org/grpc/credentials/insecure` and
  `google.golang.org/grpc/internal/resolver/unix`, through sigstore-go. That a server
  links no gRPC code for a plaintext connection or a Unix address is therefore not a
  property that can be asserted of it.

Read for this part in `google.golang.org/grpc@v1.82.1`:

- `encoding/proto/proto.go:35-37`: the `init` calls `encoding.RegisterCodecV2` with the
  module's own proto codec, and makes no other call.
- `encoding/gzip/gzip.go:40-46`: the `init` calls `encoding.RegisterCompressor`. Outside
  its tests, one file of the module imports that package,
  `benchmark/benchmain/main.go`.
- `trace_withtrace.go:1` carries the build constraint `!grpcnotrace`. Outside its tests
  it is the one file of the module that imports `golang.org/x/net/trace`.
- A search of the module's Go files outside its tests for a call of `RegisterCodec`,
  `RegisterCodecV2` or `RegisterCompressor` finds those two and one more, inside a
  closure that the `init` of package `encoding` stores as a test hook and does not run
  (`encoding/encoding.go:41-45`).

### Decision of the S2b part

28. **The builder serves six release modules, at one version.** The list of
    `internal/builder/build.go:94` gains `sdk/remote` and `sdk/remotehost`, and holds
    the root module and the five SDK modules (ADR-063 decisions 7 and 43; the notes of
    2026-11-27 at decisions 20 and 23). It stays one list, so each rule that reads it
    holds for the two new modules as it holds for the four:
    - a release serves each at the release version, with a served `go.mod` in which
      each require line of a release module names that version and which holds no
      `replace` line (`serve.go:26-45`, `:52-59`; ADR-060 decision 21). The served copy
      rewrites the require lines the file has and adds none (`serve.go:31-36`). The
      test of the served copies reads the six files
      (`internal/builder/serve_test.go:19-21`);
    - the `go.sum` inside each served zip is a source of recorded hashes (decision 8);
    - each is held at the release version, and `--allow-bump` never names one (decision
      20);
    - an id under `aiops/` or `contrib/` is admitted only from a package of one of the
      six (`08:404`).

    No module is served, pinned or protected by a rule of its own. The closed require
    list of each new module and the CI steps of its directory are ADR-063's (decisions
    7 and 37).
29. **What the served zips hold.** A served zip leaves out a nested module
    (`serve.go:87`). From S2b the zip of `sdk` therefore holds no file under `remote/`
    or `remotehost/`: the handshake proto and its freeze test leave the module `sdk` and
    travel in the zip of `sdk/remote`, in the directory they have (ADR-063 decisions 6
    and 43). The six are served at one version and none is raised alone (decision 28),
    so no build list holds that directory in two modules. A test over one serving of
    the tree fails when a file of the tree is in two of the six zips, or when a
    directory that holds a `go.mod` is in the zip of its parent. A release serves one
    version of each of the six.

    The second version that step D1 of the S2b exit needs (ADR-063 decision 27) is a
    second serving of all six modules, by the exit's pipeline, from a changed copy of
    the tree, into the module source of the job. One serving writes every module of the
    list, and each served `go.mod` names that version in its require lines of a release
    module (`serve.go:31-36`, `:52-59`). So `sdk/remote` at the second version requires
    `sdk` and `sdk/kernel` at it, and the two remote modules served alone would not
    resolve. The copy of the echoer requires each release module it uses at the second
    version. That version is in no pin of the test release, and the builder asks a
    module source for exact versions only (decision 7), so it changes no build of that
    release.
30. **A release module that no release binary compiles** has its hashes recorded as
    question Q14 decides: answered (a), decision 40. The facts that the question rests
    on:
    - `sdk/remotehost` is the kernel module inside a Go plugin process, and its
      importer is the `main` that the remote form of the Go template writes (ADR-063
      decisions 8 and 60; `07:32`). This part expects that no binary of a release
      compiles a package of it; a test of decision 37 proves it on the test release.
    - The pins of a release are the `go.sum` lines of its own build (ADR-060 decision
      20), so they then hold no hash of the zip of `sdk/remotehost`. Whether they hold
      the hash of its `go.mod` depends on a root `go.mod` that is not written yet: go
      writes that line only when a `go.mod` of the build's module graph requires the
      module, and a served copy gains no require line that the repository's file lacks
      (`serve.go:31-36`).
    - A site's build lists every package of each module of `plugins`
      (`build.go:560-563`). A module that holds a `main` on `sdk/remotehost` beside its
      in-process plugin makes that build compile `sdk/remotehost`. Its author resolved
      with that module, so the `go.sum` inside the plugin's zip holds the line of the
      zip of `sdk/remotehost`, and the build records every line of that file
      (`build.go:402`, `:369-386`). The hash is therefore recorded, and the build does
      not mark it as checked against nothing (`build.go:579`).
    - So, as built, the only record of the zip of a module that the release pipeline
      serves is the `go.sum` of a third party's plugin. The release's signed manifest
      does not vouch for its own module. A module source that serves another zip under
      that version passes whenever the plugin's `go.sum` agrees with that zip, and the
      signed manifest of the site's build then records the pin as checked. The pin is
      marked as checked against nothing only where the plugin's zip holds no `go.sum`,
      or one without that line.
31. **One binary drives a remote process**, as question Q15 decides: with (a),
    `suites/runtime-remote.test` (answered (a), decision 41). It carries what steps 4
    and 5 of `plugin check` run in the runtime `remote` (ADR-063 decision 17) and what
    `plugin verify` re-runs against a pulled image (decision 38).
    - **Why a test binary.** A suite takes a `*testing.T`
      (`sdk/def/echoers/v0/conformance/conformance.go:69`; ADR-059 decision 32), and
      `kerneltest.New` takes a `testing.TB`, which keeps a kernel with both binding maps
      off, and the fakes it mounts, out of every binary that is not a test (`07:572`).
      Step 4 mounts the same proxy fiber behind the same fakes as step 5 (`07:33`;
      `08:1265`). A runner inside the `aiopsctl` process would need `testing.MainStart`
      or a second constructor of `kerneltest`: the two options that the owner did not
      take at S2a (ADR-060, question Q10 (b) and (c), and decision 46). `07:33` lets
      "the `aiopsctl` check runner" import `sdk/testkit`, and the test binary that
      `aiopsctl` embeds is that importer, as the pack binaries are.
    - **From a release build.** The release build writes the test file of the binary
      into the scratch module in both phases, so that its build list is in the
      release's pins. Its second phase compiles it beside the pack binaries, with `go
      test -c`, `-trimpath`, cgo off and the tag `grpcnotrace`, and the release
      `aiopsctl` embeds it (ADR-060 decision 46; `suites.go:163-177`;
      `build.go:349-350`).
    - **Inside the repository**, as question Q18 decides: answered (a), decision 44.
    - A definition name holds no hyphen (`internal/plugingen/defs.go:53`), so the name
      of the binary collides with the suite binary of no registry.
32. **What the binary links, and what it does not.**
    - **The runner, `internal/remotesuite`.** A package of the root module, beside
      `internal/packsuite`. It holds the Unix dial of decision 33, the mount of a proxy
      fiber on the kernel root that it is handed, the bodies of the two tests and the
      report. Its one importer is the test file of the binary, which the builder
      generates. That file passes in the kernel root of `kerneltest`, the fakes, the
      suites and the adapters, which a non-test file outside `sdk/def`, `sdk/testkit`,
      `test/` and `cmd/aiopsctl` does not import (`internal/vet/imports.go:141`, `:238`),
      as the test files of the pack binaries do for `internal/packsuite`
      (`suites.go:66-88`). It is the directory of the check runner that the importer
      lists of grpc-go and protobuf-go name (ADR-063 decision 42).
    - **Its line cap.** The table of ADR-063 decision 36 has no row for the runner.
      This part proposes one: `internal/remotesuite`, new, 800 lines, provisional, the
      figure that table gives `internal/packsuite`, enforced by `kernelcheck lines`
      from the package's first commit and settled at the S2b exit under the rule of
      that decision. The row of `internal/packsuite` stands: the pack binaries do not
      import the runner, so they link neither the proxy nor a dial.
    - **The kernel side of the protocol**, the package of the module `sdk/remote` that
      the proxy uses: one implementation serves a server and `plugin check` (ADR-063
      decision 9).
    - **The proxy code of the plugin `aiops/remote`**, with `plugin.FromManifest`
      (ADR-063 decisions 10 and 41). The subject of each suite is a proxy fiber of that
      code (ADR-063 decision 14), and the binary holds no second proxy. Decision 39
      names the symbol through which the runner reaches it.
    - **For each definition that the build serves with the mark `contrib`:** its suite
      and its generated adapter of `sdk/remote/def/<name>/v<N>` (ADR-063 decisions 13
      and 44). At S2b these are `echoers` and `stampers`. The builder's list of
      registries gives what the mount of each suite takes (`suites.go:31`).
    - **For each definition that the build serves with the mark `callback`:** its fake,
      the package `fake` beside the definition (`07:28`), which answers the callbacks
      of step 4 (ADR-063 decision 17), and the fake of each definition that the proxy
      fiber injects.
    - **The two marks fail closed.** The marks are those of `serves`, which the same
      build writes from the `definition.yaml` of each definition of the server's graph
      (decision 35). A release build fails when `serves` holds a definition marked
      `contrib` whose suite or adapter the binary does not link, and when it holds one
      marked `callback` whose fake the binary does not link. So from the first real
      callback, at S3, no step 4 runs with a callback that the binary cannot serve.
    - **The callback definition of S2b.** No definition of a release is marked
      `callback` at S2b, so the binary of a release links no callback fake. The test
      definition of ADR-063 decision 51 lives in the test tree, beside the test
      definitions of S2a under `test/dist/def`, with its fake, its proto and its
      generated Go, and not under `sdk/remote/def`, which is served to every author. A
      release serves no definition outside `sdk/def`, which the parity step holds
      (`tools/plugingen/main.go:170-188`; ADR-060 decisions 28 and 55), so no release
      table holds it. The test release, the distribution of ADR-060 decision 38, which
      ships nowhere, serves it through a plugin of its table that provides its key. Its
      binary therefore links that fake, and the callback half of the binary runs on
      the `aiopsctl` that ADR-063 decision 37 names.
    - **`boot`**, for the handshake entry of the gate, as question Q16 decides:
      answered (a), decision 42. With (a) the key through which the proxy reaches that
      entry is provided by `boot`'s code, and the binary links no fake of that key.
    - **No module of its own.** The graph of the binary holds no package of a module
      that the graph of the release's server, of its `aiopsctl` or of a pack binary
      does not hold. The release build fails otherwise, so no linked module enters a
      release through this binary unread (ADR-063 decision 35).

    It links no package of `sdk/remotehost`: the process under check brings its own
    plugin-side host. It links no code of the plugin under check, in any language.

    **The Unix dial and its fence.** A server has no unauthenticated dial, by no flag,
    config field or fallback (ADR-063 decision 23). With question Q19 (a), the
    owner's answer (decision 45):
    - The dial of a Unix socket is code of `internal/remotesuite` and of no other
      package. The list of toolbox packages of the import check gains that package
      (`internal/vet/imports.go:46`), so `make check` refuses it in the dependency
      closure of `cmd/aiops`, as it refuses `internal/packsuite` there today
      (`imports.go:361-362`; `Makefile:197-203`).
    - That check reads the repository's graph. A site's server is built from a scratch
      module whose path lies under the root module's, so that it may import the
      launcher's packages under `internal/` (`build.go:96-98`), and Go admits an import
      of an `internal` package by path. The builder therefore also refuses the package
      in the graph of every server it builds, of a release and of a site, where it
      refuses a fake and a `sdk/testkit` package today (`build.go:727-741`; `08:404`).
    - **The seam.** One implementation serves a server and `plugin check`, so the
      kernel-side package and the proxy package each have one exported function that
      takes its connection from its caller. ADR-041 names those functions and the
      dial (ADR-063 decision 34). This part binds what it does not relax. Neither
      package holds a dial that works without the identities of ADR-063 decision 23,
      for a TCP address or for a Unix path. Neither reads a flag, a config field, a
      variable of the environment or an injected key to choose a connection. In the
      repository the two functions are referenced by their own package and by
      `internal/remotesuite` and by nothing else, which a check of `make check` holds,
      as `kernelcheck testctor` holds the kernel's test constructor to `sdk/kernel` and
      `kerneltest` (`tools/kernelcheck/main.go:84-92`).
    - **What the fence does not cover.** An in-process plugin of a site is code that
      the site compiled into its server. A dial that such code makes for itself is held
      by plugin vet and its dial rule, from the step that builds the rule (`07:1405`;
      ADR-062 decision 14), not by this part.
33. **How the binary is run, and who starts the plugin's process.** In ADR-063
    decisions 9, 16, 17, 23, 42, 54 and 57, "the check runner" is `aiopsctl` running
    `plugin check`. Since the suites run in a test binary (decision 31), it is two
    processes of one run: the part `Suite` of the toolbox, in the `aiopsctl` process,
    and the binary that this part runs as its child (ADR-060 decision 46). The parent
    starts the plugin's process and the child alone dials it. So the socket "is opened
    by the check runner alone, to a process that it started" (ADR-063 decision 23), and
    no third process takes part.
    - **The run directory.** `Suite` makes one directory for each run with
      `os.MkdirTemp`, of mode 0700 and owned by the user who runs `aiopsctl`, as it
      makes the directory of a pack binary today (`tools.go:485`). It takes that
      directory from no flag, no variable of the environment and no file of the plugin.
      The directory holds the binary, a copy of the plugin's `plugin.yaml`, with
      question Q16 (a) the signed manifest, the report, and one directory for the
      socket, which holds the socket and nothing else. When the run ends, passed, failed
      or cancelled, `Suite` stops the process it started and removes the directory.
    - **The start.** `Suite` takes the `entrypoint` of the plugin as the S2b part of
      ADR-048 gives it, split into arguments by that part's grammar and read by no
      shell. It starts it in the plugin's directory, with the environment of `aiopsctl`
      and one addition: the variable by which ADR-041 tells an entrypoint where to
      listen, set to the path of the socket, and no credential directory. The directory
      is new and empty, so nothing exists at that path before the start, and a run that
      finds a file there before it starts the process fails. A process that has not
      opened the socket within a bound of the part fails the step, with its output; the
      figure of the bound is fixed with the code.
    - **The dial.** The binary dials that one socket and nothing else. It is given the
      run directory, in which the socket has a fixed name, and no input of it names an
      address.
    - **The identity** is the process that `Suite` started for this run (ADR-063
      decision 54, reading 2). The run directory is closed to every other user. A
      process of the same user is not told apart, as nowhere in a check that runs the
      author's code as the author.
    - **The contract of the binary.** Its arguments are `-test.count=1` and `-test.run`,
      which selects the test and, for step 5, the definitions. It reads one variable of
      the environment, `AIOPS_REMOTE_RUN`, the run directory, and every file it reads or
      writes has a fixed name in that directory: the copy of `plugin.yaml`, the signed
      manifest, the socket and the report. It reads no other variable for a decision
      and no path outside the directory. No suite of a `contrib` registry of S2b takes
      a fixtures directory (`suites.go:31`).
    - **Two tests.** `TestActivation` is step 4: the handshake, one activation with the
      fakes behind the callbacks, the registrations that the manifest lists and the
      deactivation (ADR-063 decision 17). `TestSuites` is step 5: one subtest per
      definition, named by the definition, each the obligations half of that
      definition's suite through its adapter, with a proxy fiber and a connection of
      its own for each case that the suite mounts. The guard half never runs (ADR-063
      decision 14).
    - **One run per step.** `aiopsctl` runs the binary once for step 4 and once for
      step 5, and each run starts the entrypoint anew, so a failed step 4 leaves
      nothing to step 5. "That process" of ADR-063 decision 17 is the plugin's
      entrypoint as a local process, which each run starts. For a Go plugin step 4 also
      runs the plugin's own tests under `-race` with the toolchain, as today
      (`tools.go:307-308`, `:451-453`; ADR-063 decision 17), and the step passes when
      both pass.
    - **The report** is a `launch.SuiteReport` in JSON, what the part returns
      (`toolbox.go:276-290`). Step 4 gives one result with no name, as the part reports
      step 4 today (`toolbox.go:265-268`). Step 5 gives one result per definition: the
      suite revision, the half `obligations`, each failed obligation by its id and each
      case that skipped by its name.
    - **The toolbox.** The part is `Suite`, and no part is added for it
      (`toolbox.go:54-56`). `SuiteRequest` gains three fields: `Runtime`, empty as today
      or `remote`; `Against`, the path of the signed manifest that the run is judged
      against, empty for the embedded one, as `Toolbox.Release` takes it
      (`toolbox.go:38-40`); and `Image`, empty for a local process and, for `plugin
      verify`, the image by digest (decision 38). The operations library calls the part
      once per step and per runtime that the manifest lists
      (`plugins/operations/internal/operations/author.go:332`, `:402`). The fields
      belong to the one minor that `launch` gains at S2b with the key of ADR-063
      decision 57, `0.3` (ADR-063 decision 16), and its raised suite revision gains the
      case that a `Tools` which carries no binary answers `ErrNotCarried` for `Runtime:
      remote` (`toolbox.go:20`, `:69-80`). In `aiops` every part of `Tools` answers
      that error, so no server process starts a plugin's process (`08:447`).
    - **Which binary the toolbox takes.** For the runtime `remote` it takes this binary
      in every language, whether or not the directory holds Go code (ADR-063 decision
      17); today it takes an embedded binary only where the directory holds no Go code
      (`tools.go:439-463`).
    - **What the other parts settle.** The text and the exit code of each outcome of
      the two steps are the S2b part of ADR-061. The file that holds `entrypoint` and
      its grammar are the S2b part of ADR-048. How a process is told where to listen,
      and the command that the Python fixture wraps, are ADR-041's. What makes the
      entrypoint of a remote Go plugin runnable on the author's machine before step 4
      starts it, since it is compiled from the plugin's `main`, is the S2b part of
      ADR-061, with the form of a remote Go plugin (ADR-063 decisions 34 and 60).
    - **What they may not relax.** A plugin-side host serves without the identities of
      ADR-063 decision 23 only on a Unix path, and only when the process that started
      it set that path: never on a TCP address, and by no default. No unit that install
      renders can put an instance into that form: a rendered unit always gives a
      credential directory and a TCP address, and nothing of an artifact sets a
      variable of a unit (ADR-063 decision 25). Decision 37 holds a case on each
      rendered target.
34. **Which `aiopsctl` carries the binary, and what the binary adds to the reports.**
    - A release build embeds the binary. An `aiopsctl` built in the repository, or by
      a site's build, embeds no suite binary (ADR-060 decision 46). For the runtime
      `remote` it answers `launch.ErrNotCarried` and names the binary, as it does for a
      pack (`tools.go:476-484`), except where question Q18 has it compile the test. A
      case that needs the embedded binary runs on the `aiopsctl` of the test release
      (ADR-063 decision 37).
    - The packages of the binary enter the licence report, `THIRD_PARTY_LICENSES/`, the
      NOTICE texts and the SBOM of `aiopsctl`, and the SBOM of the builder image names
      the binary with its build list (decision 5; `build.go:872`). The scan classifies
      grpc-go and protobuf-go there at each build, as it classifies every module;
      ADR-063 decision 35 holds the reading of both at their pins.
    - The `init` deny-list reads the graph of the server's main package (decision 11;
      `build.go:161`), and the clause of check 7 on a `fake` or `sdk/testkit` package
      reads the graph of a server binary (`08:1265`). Neither reads this binary.
    - Neither `plugin smoke` nor `plugin verify` of remote code runs the builder. Smoke
      attaches the plugin's image to the released image (`07:1583`) and does not use
      this binary. Verify uses it, as decision 38 says.
    - The image of a remote plugin is built by `plugin pack` on the container runtime
      at hand (ADR-063 decision 61), never by the builder. Decisions 3 to 6 do not read
      it, and its SBOM is the S2b part of ADR-047 (ADR-063 decision 34).
35. **The writer of the new `serves` fields** is the one writer of `serves`:
    `plugingen.Serves` under `Manifest.Write`, for the manifest of a release, of a site
    and of each binary built in the repository (`defs.go:157-167`; `manifest.go:108`;
    ADR-060 decisions 9 and 20). It gains two outputs, and the reader of
    `boot/distribution.go:241-289` gains the same two in the same commit (ADR-063
    decision 46).
    - **`remote`**, in each served major of each definition: the mark of that major's
      `definition.yaml`, `contrib`, `callback` or `no` (`tools/deflint/main.go:335`),
      which the writer parses today and drops. The reader requires it in every served
      major and refuses any other value.
    - **`remoteProtocol`**, beside `pluginApi`: the protocol majors the binary speaks, a
      list of non-negative integers in ascending order, none twice, the form of
      `requires.remoteProtocol` (ADR-048 decision 12; `boot/manifest.go:505-510`).
      Whether it is always written is question Q17, answered (a), decision 43: it is
      a required field that holds `[]` for a binary that speaks none.
    - **Where the majors come from.** A binary speaks the protocol exactly when the
      package graph that its manifest is written from, the graph of the server's main
      package (`manifest.go:28-31`), holds the kernel side of the protocol. Its majors
      are then the one declaration of spoken majors of the module `sdk/remote` (ADR-063
      decision 27, step D1), read from the directory of the package that declares it
      in that graph, as the writer reads each `definition.yaml` there
      (`manifest.go:59-70`). The builder holds no list of its own and compiles none in:
      the list it writes is the list of the module that the binary links, with no
      argument about which builder built it. The S2b part of ADR-048 fixes the package
      and the form of the declaration, and `plugingen` reads the same one for
      `requires` (ADR-063 decision 34). This part asks one thing of that form: the
      writer can read it from the files of the package, since `Manifest.Write` runs no
      code of the graph.
    - **A key with no mark is not a callback.** The kernel part of `serves` gains no
      field. Its hooks already carry a `remote` mark, `no` for each
      (`defs.go:113-127`). Its four keys belong to `pluginApi` and have no
      `definition.yaml` to take a mark from (ADR-048 decision 12), so they carry none,
      and every reader takes a key that carries no mark as not `callback`. Check 9
      therefore denies a remote manifest whose `uses` names a built-in key, as it
      denies every `uses` entry of a key that is not marked `callback`, at `plugin
      check`, at `plugin gate --against`, at install and at boot, and `FromManifest`
      builds no value whose `Inject` holds such a key (ADR-063 decisions 10 and 28).
      The reader fails closed: a served major without the field is refused, and no
      default stands in for a missing mark. This is the boundary of the server keys
      that third-party remote code can reach, and this part leaves none of it open.
      What `plugingen` writes into the `uses` of a remote Go plugin whose `Spec` names
      a built-in key, which the kernel module inside its own process provides (ADR-063
      decision 8), is the S2b part of ADR-048.
    - `serves` gains nothing else: no version of a Go module, which no gate reads
      (`08:904`; ADR-060 decision 21), and nothing of the suite binaries, of the
      adapters or of the proxy factory.
    - `distVersion` stays 1 (ADR-063 decision 46). The reader is strict, so it refuses
      a manifest of the earlier writer and names the missing field. The commit
      regenerates every signed manifest and every golden of the repository, which the
      parity check of `make check` holds to the writer (`Makefile:206-209`; ADR-060
      decisions 20 and 28; ADR-063 decision 15). The majors clause of check 3 and the
      contribution-set clause of check 9 are the first readers of the two fields
      (ADR-063 decision 16).
36. **The gRPC codec exemption at S2b.**
    - It stands as read. From S2b the proxy plugin and the remote modules import gRPC
      directly (ADR-063 decision 42), at the version the root build list selects
      today, v1.82.1 (`go.mod:102`), the pin that ADR-063 decision 35 read. Every
      binary the builder writes keeps the tag `grpcnotrace` (`build.go:853`, `:863`;
      `suites.go:167`). The entry gives sigstore-go as the reason gRPC is linked
      (`initscan.go:62-63`); from the commit that first imports gRPC it names the
      remote runtime too. That is a change of the table by a reviewed pull request
      (decision 21) and calls for no new reading.
    - **Where the exemption is recorded.** Today it is a condition of the register
      entry of sigstore-go (`tools/upstream-components.yaml:204`). From the commit that
      first imports gRPC directly, grpc-go has a register entry of its own (ADR-063
      decisions 35 and 42). From that commit the entry of grpc-go carries the condition
      and the version (ADR-020 decision 27), and the condition of the sigstore-go entry
      stays and names the same version, since sigstore-go links gRPC into a binary that
      holds no proxy.
    - A pin moved in the repository cannot pass unread. The entry is keyed by package,
      module and version, and the scan admits the registration at that version only
      (`initscan.go:61-63`, `:234-242`). From the commit that moves the pin of
      `google.golang.org/grpc`, in the root `go.mod` or through the require list of a
      remote module, every build fails at the deny-list scan until the entry changes.
    - **A site cannot raise it.** `--allow-bump google.golang.org/grpc` passes the
      strict-version check, since the release pipeline does not build that module
      (decision 10). The build then fails at the deny-list scan, on "a global codec":
      the exception names the release's version, and a site adds none (decision 21).
      This has held since S2a. From S2b the module carries the first-party wire, so the
      finding says why: a registration that an exception names at another version is
      reported with the version the exception was read at.
    - Read again means four things, in the pull request that moves the pin and at the
      new version as the module cache holds it. (1) The `init` of `encoding/proto`
      still registers gRPC's own proto codec with `RegisterCodecV2` and makes no other
      call. (2) The scan of the server's graph, built with the tag, reports no other
      registration of decision 11. (3) The tag still takes `golang.org/x/net/trace` out
      of that graph, which `TestGRPCNeedsTheTag` proves
      (`internal/builder/initscan_test.go:138`). (4) The entry of the table, the
      condition of the grpc-go entry of the register and the condition of the
      sigstore-go entry name the new version in the same pull request (ADR-020
      decision 27). A reading that finds what the last one found needs that reviewed
      pull request and no more (decision 21). A reading that finds another registration
      adds no entry without the owner's word, as the three gob entries were added on
      2026-11-25 (decision 21, its note).
    - One more registration is one import away. At v1.82.1 the `init` of
      `google.golang.org/grpc/encoding/gzip` registers a compressor (Read for this
      part), a call of the table of decision 11, so a server whose graph holds that
      package fails the build on "a global codec". This part proposes no exception for
      it. If ADR-041 gives the protocol compression, the exception is asked of the
      owner with that ADR.
    - The generated Go of the handshake and of the adapters registers its descriptors
      and types with `protoregistry` from `init`, as every generated protobuf package
      does (What was read). Protobuf type registries are not on the list (decision
      21), so that code needs no exception.
    - No exception names `google.golang.org/protobuf`, so a move of its pin calls for
      no reading under this decision; its licence reading is ADR-063 decision 35's.
    - The deny-list reads server binaries (decision 11). The binary of decision 31, a
      plugin built on `sdk/remotehost` and the process of a remote plugin are none.
37. **Tests.** A test that is committed before its code is marked as ADR-063 decision
    29 says. Each test of the first list fails on its base.
    - **The modules.** The served `go.mod` of each of the six. The zips of decision 29.
      A raise of `sdk/remote` or of `sdk/remotehost`, refused with or without
      `--allow-bump`. With question Q14 (a): the signed manifest of the test release
      holds, for each of the six, a pin with the hash of the zip and of the `go.mod`
      that the module source serves; a site's build whose module source serves a
      changed zip of `sdk/remotehost`, with a plugin whose `go.sum` records the hash of
      that changed zip, fails and names the two sources, where it passes today; and the
      same module source with a plugin whose zip holds no `go.sum` fails on the pin,
      where today it passes and marks the hash as checked against nothing.
    - **The binary.** The `aiopsctl` of the test release embeds it, and the SBOM of the
      builder image names it with its build list. A release build fails on a served
      definition marked `contrib` whose suite or adapter the binary lacks, on one
      marked `callback` whose fake it lacks, and on a package of a module that no other
      binary of the release links. On the `aiopsctl` of the test release a fixture
      plugin whose `uses` names the key of the test definition has its callback served
      through the binary's fake at step 4, and the `serves` of the test release carries
      the mark `callback` for that definition. An `aiopsctl` that embeds no binary
      answers `launch.ErrNotCarried` for the runtime `remote` and names the binary:
      with question Q18 (a) for a directory outside the root module, with (b) for
      every directory.
    - **Standing rule 2.** With question Q18 (a): on the `aiopsctl` built in the
      repository, `TestPluginCheckOnEveryPlugin` passes steps 4 and 5 of every remote
      example under `plugins/` and `examples/`. With Q18 (b): a named test of the job
      that builds the test release passes steps 4 and 5 of every remote example on that
      `aiopsctl`, and fails, and does not skip, when it does not run.
    - **The run.** The run directory has mode 0700 and is gone, with the process, after
      a run that passed, one that failed and one that was cancelled. A file at the
      socket's path before the start fails the run. A binary whose run directory holds
      no socket fails and dials nothing else. An entrypoint that never listens fails
      the step at the bound, with its output. `TestActivation` and `TestSuites` each
      run against a process of their own. The suite of `launch` holds the case of
      `ErrNotCarried` for `Runtime: remote`.
    - **The fence.** The import check fails on `internal/remotesuite` in the closure of
      `cmd/aiops`. A site's build whose plugin module imports that package fails at the
      builder's check of the server's graph. A reference to a function of the seam
      outside its own package and `internal/remotesuite` fails `make check`. With
      question Q19 (a), the dial that the kernel-side package exports refuses a Unix
      path, and a TCP address without the three files of the identities. For each
      target that install renders, the unit gives a credential directory and a TCP
      address and no Unix path.
    - **`plugin verify`.** The conformance case of ADR-063 decision 28 on the artifacts
      of G2 and of P2 passes over the bound socket directory, and asserts of the
      running container: the image digest that the signed artifact pins, no interface
      but loopback, a user that is neither the user of `aiopsctl` nor root, one bind,
      and no variable of the environment of `aiopsctl`. On an artifact whose signature
      does not verify, no image is pulled and no container starts. After a failed
      suite the container and the run directory are gone.
    - **The module table.** The generated table of the test release holds the entry of
      `aiops/remote` and the factory. A table without `aiops/remote` gives no factory,
      and its manifest holds what question Q17 decides. A site's module that declares
      a function of the factory's name adds none. The parity of `dist/modules.go`
      holds.
    - **`serves`.** Each served major carries the mark of its `definition.yaml`. The
      manifest of the test release holds `remoteProtocol: [0]`. A manifest written from
      a graph whose `sdk/remote` declares major 7 alone, the second serving of step D1,
      holds `remoteProtocol: [7]`. The reader refuses a served major with no `remote`
      or with another value, and a `remoteProtocol` that is out of order, holds a major
      twice or holds a negative number. A remote manifest whose `uses` names a built-in
      key of the kernel is denied with check 9 at each entry point of ADR-063 decision
      28. The parity check holds every committed manifest.
    - **gRPC.** The scan reports the codec registration when the build list holds gRPC
      at another version than the entry names, and the text names the version of the
      entry. A site's build with `--allow-bump google.golang.org/grpc` and a build list
      that raises it fails on "a global codec" with that text. A server graph that
      imports `encoding/gzip` fails on "a global codec".

    The guards below pass on their base and must keep passing:
    - no package of `sdk/remotehost` is in the graph of the server, of `aiopsctl` or of
      a suite binary of the test release, which holds the expectation of decision 30;
    - an `aiops/` id declared in a module outside the six is refused, as before;
    - a raise of one of the four modules of S2a is refused, as before;
    - `TestGRPCNeedsTheTag` passes with the proxy in the graph of `cmd/aiops`;
    - the graph of each pack binary holds neither the proxy nor
      `internal/remotesuite`;
    - `kernelcheck lines` holds `internal/builder`, `internal/authoring` and
      `internal/packsuite` to the caps of ADR-063 decision 36.

    The cases that drive the binary against a process are those of ADR-063 decisions
    27 and 28, on the `aiopsctl` of the test release.
38. **The same binary against a container: `plugin verify`.** `plugin verify` of remote
    code re-runs the suite against a pulled image (ADR-063 decisions 17 and 28). It
    runs `TestSuites` of this binary, with this dial, through the same part of the
    toolbox, with `Image` set (decision 33). The link set of decision 32 does not
    change, and the binary gains no second dial.
    - **How the binary reaches the container.** The directory of the socket, inside
      the run directory of decision 33, is the one bind of the container. The
      plugin-side host inside the container listens on the socket in it, told by the
      variable of ADR-041, and the binary dials the same file on the host. No port is
      published and no TCP connection is made. Reading 2 of ADR-063 decision 54 holds
      as for a local process. Note of 2026-11-27: the owner's answer to question Q15 of
      the S2b part of ADR-061, recorded on the same date as its decision 65, gives
      `plugin verify` the other channel, mutual TLS to a container that runs as a unit.
      The difference is with the owner as question Q17 of that part. This bullet and
      the words "the binary gains no second dial" follow the answer, and `Verify` of
      remote code does not merge before it.
    - **"A process that it started"** is, for a container, the container that this run
      of `aiopsctl` created from the image and removes when the run ends. A container
      that exists before the run, or that another command started, is never dialled:
      the socket directory is new for the run and bound into that one container.
    - **What the forms of the command may not relax.** The S2b parts of ADR-020 and
      ADR-061 give `plugin verify` its order, its texts and what it compares with the
      attestation (ADR-063 decision 34). Whatever they give:
      - no image is pulled and no container starts before the signature and the trust
        policy for the id and the runtime `remote` hold (ADR-047 decision 2);
      - the container runs the image by the digest that the signed artifact pins, and
        by no tag;
      - it has no network: no interface but loopback. That is narrower than the unit
        of ADR-063 decision 24, which leaves the stream that the kernel opened; here
        the stream is the socket;
      - its user is neither the user who runs `aiopsctl` nor root (ADR-063 decision
        25);
      - its one bind is the socket directory. No path of the home, no credential and
        no variable of the environment of `aiopsctl` reaches it; its environment holds
        the variable of ADR-041 and nothing else of the host;
      - nothing of the artifact reaches the command of the container runtime but the
        three inputs of ADR-063 decision 25, each written as an argument and never
        through a shell, and the container runs the command that a rendered unit runs;
      - `Suite` removes the container and the run directory when the run ends, passed,
        failed or cancelled.
    - **The modes.** The run directory stays of mode 0700 and of the user who runs
      `aiopsctl`, so no other user of the host reaches the socket directory inside it,
      whatever the mode of that inner directory. The inner directory is opened to the
      user of the container, who must create the socket in it. The fence is the run
      directory and the single bind, and it rests on no mode of the socket. The mode of
      the socket itself is the listener's, which ADR-041 fixes; this part requires of
      it that the user who runs `aiopsctl` can connect, and the case of decision 37
      proves it on a container whose user is not that user.
    - **Without a container runtime** the part fails with the error that the S2b part
      of ADR-061 gives a part of `Tools` that lacks one, and runs nothing.
39. **The generated module table at S2b.** The builder generates the table of a release
    and of a site (`gen.go:21-31`), and `plugingen` generates `dist/modules.go` for the
    repository (ADR-060 decision 20). ADR-063 decisions 12 and 41 carry the factory:
    the launcher hands the loader the function that turns a kept manifest into a plugin
    value.
    - **The entry keeps its shape.** `plugins/remote` holds a `plugin.yaml` with the id
      `aiops/remote` and declares the value of its entry as every plugin of the table
      does, `var Plugin` or `func New()` (`build.go:444-448`; `07:34`). The generators
      write that entry as they write every entry, `dist.Module` gains no field, and the
      table still maps each plugin id to its value and its manifest (ADR-060 decision
      20). The release input `dist/dist.yaml` gains the package (ADR-063 decision 22).
      How a value that the factory builds takes the bindings of that entry is ADR-063
      decision 41's, with an ADR-034 addition if the kernel must change (ADR-063
      decision 3).
    - **The factory is one more accessor.** The package that declares the id
      `aiops/remote` also declares the factory: a package-level function `Proxy` that
      takes the bytes of a kept `plugin.yaml` and returns a `kernel.Plugin` and an
      error. The scan that finds the accessor of a package finds it
      (`internal/plugingen/plugingen.go:235-254`). Both generators write one function
      more into the generated file, beside `Modules`, which returns the factory of the
      table and nil for a table that holds no `aiops/remote`. `dist.Table` and the
      generated `Load` hand it on with the table, and it reaches the loader with the
      config of the loader, the path by which the values of the table reach the loader
      today and on which no key and no catalog entry shows them (`boot/run.go:93-99`;
      `dist/dist.go:95-97`; ADR-059 decision 41).
    - **Only the release's.** The generators take the factory from the package whose
      manifest declares `aiops/remote`, an id that the builder admits only from one of
      the six release modules (decision 28; `08:404`). A function of that name in a
      package of any other id is not read as a factory, in a release's table and in a
      site's, so no site module supplies the code that builds the proxy of every remote
      plugin.
    - **What the signed manifest records.** `Manifest.Write` records `aiops/remote` as
      it records every package of the graph that holds a `plugin.yaml`: its id, its
      package, its manifest byte for byte and the statement of the suites that ran on
      it (`manifest.go:59-83`, `:108-114`). It records nothing of the factory. That a
      binary can build a proxy is what `serves.remoteProtocol` says (decision 35).
    - **The symbol that the check binary uses.** `Proxy` builds its values on the
      mutual-TLS dial. The runner of decision 32 builds its proxy values through the
      second exported function of the same package, which takes the manifest and
      takes its connections from its caller, and on which `Proxy` itself is built.
      That function is one of the two of the seam of decision 32 and is under its
      rule: the generated test file does not reference it. The binary reaches the
      proxy through no constructor made for tests.

### Consequences of the S2b part

- Six modules are served by a change of one list, and no rule of the builder is added
  for the two new ones.
- With question Q14 (a) every signed manifest of a release records a hash for each
  module the release serves, and a site's scratch module, which requires each pin that
  holds a zip hash (`build.go:311-318`), requires each of the six at the release
  version. A site plugin whose `go.sum` holds another hash for a release module than
  the release's pin then fails the build.
- The release `aiopsctl`, and with it the builder image, grows by one test binary that
  links gRPC, the proxy and the suites of the `contrib` registries, and by no module.
  The licence report, the notices and the SBOM cover it with the code they have.
- The root module gains one package, `internal/remotesuite`, with a cap of its own, and
  the table of ADR-063 decision 36 gains its row. The starter of the plugin's process
  and of the container of `plugin verify` is code of `internal/authoring`, and the
  generator of the test file is code of `internal/builder`. This part expects both to
  stay inside their provisional caps of ADR-063 decision 36, 1,500 and 4,400 lines;
  `kernelcheck lines` holds each in `make check`, and growth past one needs the owner's
  word.
- With question Q18 (a) `make plugin-check` runs steps 4 and 5 of every remote example
  on every commit, on the code of the repository, and ADR-063 decision 31 stands whole.
  The Python examples then need, on the machine that runs the target, the interpreter
  that ADR-041 names with `aiops-sdk`; which job holds it is ADR-063 decision 37's.
- `launch` gains three fields of `SuiteRequest` in its S2b minor, and no part of
  `Tools`.
- The generated table gains one entry and the generated file one function. The shape
  of an entry does not change.
- Every signed manifest changes by two fields, and a manifest written before the change
  is refused by the reader after it. No distribution manifest exists outside the tests
  of the repository (ADR-063, question Q9 (a)).
- The test release serves one test definition marked `callback`. The shipped release
  serves none outside `sdk/def`.
- The gRPC exemption is unchanged at S2b, no exception is added, the compressor of
  `encoding/gzip` stays a finding, and a site cannot raise gRPC.
- `plugin verify` of remote code runs a third party's image on the verifier's host. It
  does so only after the signature and the trust policy hold, with no network, no path
  of the home and no credential.
- Nothing else of this ADR changes: the two paths, the builder image and its missing
  base layer, the module cache format, the rule of `--allow-bump`, the content of the
  deny-list and the site's entries are as decisions 1 to 27 have them.

### Edits on acceptance of the S2b part

Nothing outside this file was edited before the owner answered. The commit that
records the answers of 2026-11-27 edits this section, its status and the decisions that
the answers settle, and the status of this ADR in `00:782`, which then names the S2b
part. The row of this ADR in the ADR table of `08` §3 (`08:1178`) is left to the commit
that updates that table ("Edits made with this record", in the section "Owner answers
of 2026-11-27"). The owner chose the recommended option of each question, so the commit
adds a dated note to:

- decision 8 above and ADR-060 decision 20, for the hashes of the served modules in the
  pins (Q14 (a));
- ADR-060 decision 46, for the binary `suites/runtime-remote.test` (Q15 (a)) and for
  the same test compiled inside the repository (Q18 (a));
- ADR-063 decisions 16 and 57, for the path by which the check runner reaches the
  handshake entry (Q16 (a)); question Q16 gives the words of both notes;
- ADR-060 decision 9, for the two fields of `serves` (decision 35; Q17 (a));
- ADR-063 decision 9, for the dial of the kernel-side package (Q19 (a));
- ADR-063 decision 36, for the row of `internal/remotesuite` (decision 32).

With Q18 (b), ADR-063 decision 31 would have gained a dated note in place of the note
for Q18 (a); the owner chose (a), so it gains none. No recommended option edits a line
of `00` to `08` beyond the two status lines. An option the owner did not choose edits
nothing.

### Questions for the owner on the S2b part

Each question gives its options, the recommended one, why, and what the answer blocks.
An option that changes or reads a line of an accepted decision says so, and such a
question marks the option that keeps the text as written.

**Q14. Does the signed manifest record a hash for a release module that no release
binary compiles?** The pins are the `go.sum` lines of the release's own build (ADR-060
decision 20), and go writes the hash of a zip only for a module whose packages the
build lists. At S2b that leaves `sdk/remotehost` with no hash of its zip in the pins
(decision 30). A site's build that compiles it takes the hash from the `go.sum` inside
the zip of the site's plugin, the third source of decision 8, and records the pin as
checked.
- (a) The release build records, for each of the six modules it serves, the hash of its
  zip and of its `go.mod` as its module source holds them, whether or not a release
  binary compiles it. A site's build then checks each against the signed manifest, and
  a plugin's `go.sum` that holds another hash fails as two sources that differ.
  Decision 8 and ADR-060 decision 20 gain a dated note: the pins hold these lines
  beside the `go.sum` lines of the release's build.
- (b) **As built, and the text as written.** The pins are the lines of the release's
  own build. The zip of `sdk/remotehost` is checked against the `go.sum` of the site's
  plugin and against nothing of the release, and the signed manifest of the site's
  build records it as checked. It is marked as checked against nothing only where that
  zip holds no `go.sum`.
- (c) A release binary links it: the binary of decision 31 imports a package of
  `sdk/remotehost`, so the release's build lists it. A package is then linked for its
  hash alone, into a binary that drives the host of another process.
- Recommended: (a). Under (b) a first-party module is vouched for by third-party data
  alone and reported as checked. Under (a) every module the release serves is checked
  against the release's signature by every build that resolves it, and nothing is
  linked that no binary runs. Blocks: the builder change that serves `sdk/remote` and
  `sdk/remotehost`.
- **Answered 2026-11-27: (a)**, decision 40.

**Q15. How many binaries drive a remote process?** ADR-060 decision 46 embeds one test
binary per registry suite, each for the rows of a pack. Step 4 of the runtime `remote`
makes one activation with every registration the manifest lists, and step 5 runs the
suite of each registry the plugin contributes to (ADR-063 decision 17).
- (a) One, `suites/runtime-remote.test`. It links the proxy, the kernel side of the
  protocol and gRPC once, and every `contrib` registry of the release with its suite
  and its adapter (decision 32). `aiopsctl` runs it once for step 4 and once for step 5
  (decision 33).
- (b) One per `contrib` registry, `suites/runtime-remote-<name>.test`. Step 4 registers
  with every registry the manifest lists, so one of them must still link them all, or
  a further binary runs step 4. Each links the proxy and gRPC again.
- (c) No new binary: the pack binary of each `contrib` registry gains the remote tests.
  The suite binary that runs a pack's rows then links the proxy, gRPC and the socket
  dial, and step 4 has the difficulty of (b).
- Recommended: (a). Step 4 needs one binary that holds every `contrib` registry in any
  case, the code that dials is linked once, and the pack binaries stay as ADR-060
  decision 46 has them. Blocks: steps 4 and 5 of `plugin check` for the runtime
  `remote`.
- **Answered 2026-11-27: (a)**, decision 41.

**Q16. Where does the check runner evaluate the handshake?** On the socket of `plugin
check` the handshake and check 3 run as on a server (ADR-063 decision 54, reading 2),
and `boot` is the one home of check 3 (ADR-063 decision 16). Two accepted decisions
word the path. ADR-063 decision 16: "the check runner of `plugin check` uses the same
entry through the toolbox it already holds (`sdk/def/launch/v0/toolbox.go:31-33`)".
ADR-063 decision 57: "The check runner keeps the toolbox that the operations row
already resolves." The cited lines are the method `Gate` of `Toolbox`: "Gate evaluates
the clauses of one entry point of the gate on an artifact, against a profile of a home
or a published signed manifest". `Gate` is boot's part, and the toolbox is a value of
the `aiopsctl` process (`boot/launch.go:226-227`). The suites run in a child process,
because a suite takes a `*testing.T` (decision 31), and the handshake is evaluated per
connection, on what the connection brings.
- (a) **A change of ADR-063 decisions 16 and 57.** The binary links `boot`, as the pack
  binaries do (`internal/packsuite/packsuite.go:31`), and evaluates the handshake entry
  with `boot`'s code in its own process. The path changes from `Toolbox.Gate` in the
  `aiopsctl` process to `boot`'s code in the child. Under it:
  - the manifest is the bytes that `Toolbox.Release` gave that run of `aiopsctl`, the
    embedded one or the one `--against` names (`toolbox.go:38-40`), which `aiopsctl`
    writes as a file of the private run directory; the binary reads a manifest from no
    other path;
  - in the binary the key of ADR-063 decision 57 is provided by `boot`'s handshake
    entry on that manifest. No fake provides it, and the launch fake answers nothing
    for it;
  - no copy of check 3 exists outside `boot`.

  The note at ADR-063 decision 16: "Note of the date of the answer (ADR-044, S2b part,
  Q16 (a)): the check runner is `aiopsctl` and the suite binary that it embeds and runs
  as its child. The binary reaches the handshake entry in `boot`'s code, which it
  links, in its own process, on the signed manifest that `aiopsctl` hands it, and not
  through `Toolbox.Gate` (`toolbox.go:31-33`)." The note at ADR-063 decision 57: "Note
  of the same date: the check runner reaches the handshake entry as the note at
  decision 16 says. In its binary this key is provided by `boot`'s code and by no fake."
- (b) **The text as written.** The entry stays in the toolbox of the `aiopsctl` process,
  and the binary asks its parent for each verdict over a pipe. A second wire format,
  between `aiopsctl` and its own child, then carries what the gate reads of a
  connection, and the request of `Gate` gains those inputs.
- (c) `aiopsctl` links the kernel side of the protocol and makes one handshake itself,
  through its toolbox, before it starts the binary, whose own connections take a fake
  of the entry. The suite then runs on connections whose handshake no gate evaluated,
  against reading 2 of ADR-063 decision 54.
- Recommended: (a). One code evaluates check 3 at every entry point, nothing new
  crosses a process boundary, and the binary links `boot` as its neighbours do. Blocks:
  steps 4 and 5 of `plugin check` for the runtime `remote`, and the handshake row of
  ADR-063 decision 16 at that step.
- **Answered 2026-11-27: (a)**, decision 42.

**Q17. Is `serves.remoteProtocol` always written?** `requires.remoteProtocol` is
present exactly when `runtimes` holds `remote`, with at least one major (ADR-048
decision 12; `boot/manifest.go:516-517`), and `serves` has the shape of `requires`
(`08:904`; `07:545`). A binary speaks the protocol only when it links the kernel side
of it (decision 35).
- (a) Always: a required field of `serves`, which holds `[]` for a binary that speaks
  none. The strict reader then refuses a manifest of the earlier writer by the name of
  the missing field.
- (b) Present exactly when the binary speaks, with at least one major, the rule of
  `requires`. An absent field then means either that the binary speaks no major or
  that the manifest was written before S2b, and the reader cannot tell the two.
- Recommended: (a). Check 3 denies a remote manifest in both cases, and only (a) makes
  the reader refuse a manifest that predates the field. Blocks: the reader and the
  writer of the new `serves` fields, and with them the majors clause of check 3.
- **Answered 2026-11-27: (a)**, decision 43.

**Q18. How does `make plugin-check` run steps 4 and 5 on the remote examples?** ADR-063
decision 31: "`make plugin-check` and CI run `plugin check` and a T3 boot on every
plugin under `plugins/` and `examples/`, the remote examples in Go and in Python
included". The target builds its `aiopsctl` in the repository, and its test fails on
any step that neither passed nor is not applicable (`Makefile:139-150`;
`cmd/aiopsctl/check_test.go:116-121`). Only a release build embeds suite binaries
(ADR-060 decision 46), and steps 4 and 5 are T1 and T2 of the runtime `remote`.
- (a) **The text as written.** An `aiopsctl` that embeds no binary compiles the same
  test with the Go toolchain at hand, when the plugin's directory lies in the root
  module of the repository. One function of the builder gives the text of the test
  file to the release build and to the toolbox, as it gives the test of step 5 to both
  today (`suites.go:190`; `tools.go:370`). The toolbox writes it into a build overlay,
  never onto disk, compiles it in the root module with `go test -c` and the tag, and
  runs the result as it runs an embedded binary: the same start, the same contract and
  the same report (decision 33). This is the split the toolbox has today, an embedded
  binary where no toolchain is needed and a compile in the module otherwise
  (`tools.go:439-463`). Outside the root module the answer stays
  `launch.ErrNotCarried`: the proxy and the runner are packages of the root module,
  which an author's module does not hold. ADR-060 decision 46 gains a dated note.
- (b) **A change of ADR-063 decision 31.** Only a release build carries the binary. On
  the `aiopsctl` built in the repository steps 4 and 5 of a remote example are reported
  as not built, and the test of `make plugin-check` admits that outcome for those two
  steps of those examples. A named test of the job that builds the test release runs
  `plugin check` on every remote example with that `aiopsctl` and requires both steps
  to pass. Decision 31 gains a dated note: `make plugin-check` runs steps 4 and 5 of a
  remote example in that job only, on linux/amd64.
- (c) `make plugin-check` builds the test release first and checks the remote examples
  with its `aiopsctl`. The letter of decision 31 stands, and every run of `make check`
  then makes a two-phase release build, which needs a filled module source and, as the
  exits do, runs on linux/amd64 alone (ADR-063 decision 27).
- Recommended: (a). Standing rule 2 keeps running whole wherever `make check` runs, on
  the code of the commit and not on a release's, and the toolbox gains no second way of
  running a suite, only a second way of obtaining the one binary. Blocks: steps 4 and 5
  of `plugin check` on the remote examples, and with them the merge of the first remote
  example.
- **Answered 2026-11-27: (a)**, decision 44.

**Q19. Does the kernel side of the protocol hold the Unix dial?** ADR-063 decision 9
names "the dial, the handshake, the stream and the stubs" as one package: "The proxy
plugin and the check runner of `aiopsctl` both use it". ADR-063 decision 23: "A
server has no unauthenticated dial: no flag, no config field and no fallback", and the
Unix socket of `plugin check` "is opened by the check runner alone", where "no row of a
server profile reaches it". The proxy links that package into every server whose table
holds `aiops/remote`. The graph of `cmd/aiops` already holds gRPC's plaintext
credentials and its Unix resolver through sigstore-go (Context), so the absence of such
gRPC code cannot be the fence.
- (a) **A reading of ADR-063 decision 9, recorded by a note.** No. "The dial" of
  decision 9 is the mutual-TLS dial. The Unix dial is code of `internal/remotesuite`
  alone, which the import check and the builder refuse in every server graph, and the
  kernel-side package serves a connection that its caller made, under the rule of the
  seam (decision 32). ADR-063 decision 9 gains a dated note: the dial of that package
  is the mutual-TLS dial, and the check runner brings its own connection to the
  handshake, the stream and the stubs.
- (b) **The words as they stand.** Yes: one dial function with two forms in the
  kernel-side package, TLS for a TCP endpoint and plain for a Unix path, which the
  check runner calls. Decision 9 needs no note. Every server that links the proxy then
  links a first-party dial with no credential, and what keeps a row from it is that
  the proxy's code never calls that form, held by a check over call sites.
- Recommended: (a). Decision 23 then holds by what a server links, not by what its code
  leaves uncalled, and one package and two functions are all that a review of the fence
  has to read. The S2b part of ADR-041 names the dial function and is worded to this
  answer. Blocks: the exported functions of the kernel side of the protocol, the proxy
  and the binary.
- **Answered 2026-11-27: (a)**, decision 45.

### Owner answers of 2026-11-27

The project owner accepted the S2b part on 2026-11-27 and answered every question of
"Questions for the owner on the S2b part" on the same date, each with its recommended
option, (a). The decisions below continue the numbering of this ADR, and each names the
question it answers. Where a decision of this part leaves a point to a question, the
decision here settles it. Decisions 1 to 39 stand.

40. **The signed manifest of a release records a hash for each module the release
    serves** (Q14 (a)). The release build records, for each of the six modules it
    serves, the hash of its zip and of its `go.mod` as its module source holds them,
    whether or not a release binary compiles it. A site's build checks each against
    the signed manifest, and a plugin's `go.sum` that holds another hash fails as two
    sources that differ. The pins hold these lines beside the `go.sum` lines of the
    release's build, and decision 8 and ADR-060 decision 20 carry a dated note that
    says so. No package of `sdk/remotehost` is linked into a release binary for its
    hash.

41. **One binary drives a remote process, `suites/runtime-remote.test`** (Q15 (a)). It
    links the proxy, the kernel side of the protocol and gRPC once, and every `contrib`
    registry of the release with its suite and its adapter (decision 32). `aiopsctl`
    runs it once for step 4 and once for step 5 (decision 33). The pack binaries stay
    as ADR-060 decision 46 has them, and that decision carries a dated note.

42. **The check runner evaluates the handshake with `boot`'s code, inside the binary**
    (Q16 (a)), a change of ADR-063 decisions 16 and 57. The binary links `boot`, as the
    pack binaries do, and evaluates the handshake entry with `boot`'s code in its own
    process: the path changes from `Toolbox.Gate` in the `aiopsctl` process to `boot`'s
    code in the child. The manifest is the bytes that `Toolbox.Release` gave that run
    of `aiopsctl`, the embedded one or the one `--against` names, which `aiopsctl`
    writes as a file of the private run directory; the binary reads a manifest from no
    other path. In the binary the key of ADR-063 decision 57 is provided by `boot`'s
    handshake entry on that manifest: no fake provides it, and the launch fake answers
    nothing for it. No copy of check 3 exists outside `boot`. ADR-063 decisions 16 and
    57 carry the two notes in the words that question Q16 gives, dated 2026-11-27.

43. **`serves.remoteProtocol` is always written** (Q17 (a)): a required field of
    `serves`, which holds `[]` for a binary that speaks none. The strict reader refuses
    a manifest of the earlier writer by the name of the missing field. ADR-060 decision
    9 carries a dated note for the two fields that `serves` gains (decision 35).

44. **An `aiopsctl` that embeds no binary compiles the same test inside the root
    module** (Q18 (a)), which keeps ADR-063 decision 31 as written. It compiles the
    test with the Go toolchain at hand when the plugin's directory lies in the root
    module of the repository. One function of the builder gives the text of the test
    file to the release build and to the toolbox. The toolbox writes it into a build
    overlay, never onto disk, compiles it in the root module with `go test -c` and the
    tag, and runs the result as it runs an embedded binary: the same start, the same
    contract and the same report (decision 33). Outside the root module the answer
    stays `launch.ErrNotCarried`. ADR-060 decision 46 carries a dated note. Of the
    tests of decision 37, those it gives for Q18 (a) are built and those it gives for
    Q18 (b) are not.

45. **The kernel side of the protocol holds no Unix dial** (Q19 (a)), a reading of
    ADR-063 decision 9. "The dial" of that decision is the mutual-TLS dial. The Unix
    dial is code of `internal/remotesuite` alone, which the import check and the
    builder refuse in every server graph, and the kernel-side package serves a
    connection that its caller made, under the rule of the seam (decision 32). ADR-063
    decision 9 carries the dated note: the dial of that package is the mutual-TLS dial,
    and the check runner brings its own connection to the handshake, the stream and
    the stubs. Note of 2026-11-27: ADR-041 is not worded to this answer, as question
    Q19 said it was. Its decision 28 has the kernel-side package export a Unix dial
    that the check runner in the operations plugin calls. Its decision 45, the owner's
    answer of the same date to its question Q6, gives `aiops/operations` a named entry
    for that dial on the list of ADR-062 decision 14, and the note at ADR-062 decision
    14, the T0 row of `07` §8.1 and harness:58 follow that answer. The two answers
    cannot both be built as written. The difference is put to the owner as question Q21
    of ADR-041 (ADR-063 decision 33), before the dial functions of the kernel side and
    the dial of `internal/remotesuite` are built. No line of either text is edited for
    it: an option the owner has not chosen edits nothing.

**Edits made with this record.** The commit that records these answers makes the edits
that "Edits on acceptance of the S2b part" names for the options chosen: the status of
this ADR and of this part; `00:782`, which names the S2b part; the dated notes at
decision 8 above, at ADR-060 decisions 9, 20 and 46 and at ADR-063 decisions 9, 16, 36
and 57; and, with the note at ADR-063 decision 36, the row of `internal/remotesuite` in
the table of that decision (decision 32). In this file, lines that restate edited text
are edited with them: the note of 2026-11-27 at decision 23, which called this part
proposed, and the sentences of decisions 30, 31, 32 and 35 that left a point to a
question, each of which now names the answer and its decision. No other line of `00`
to `08` is edited, and the plugin record, the licence record, the community record and
the upstream register are not edited: no chosen option names a line of them. An option
the owner did not choose edits nothing.

Left to later commits, each named:

- `08:1178`, the row of this ADR in the ADR table of `08` §3, with the commit that
  updates that table for the S2b parts accepted on 2026-11-27;
- the lines of the S2b part of ADR-061 that call this part proposed or not accepted
  (its status, its "Acceptance order" and its decision 59 with its two tables), with
  the commit that records the owner's answers on ADR-061 for S2b;
- the cap of `internal/remotesuite` in `ci.yml` and the Makefile, with the package's
  first commit (decision 32);
- the reason of the gRPC entry in the exceptions table of the deny-list and the
  condition of the grpc-go register entry, with the commit that first imports gRPC
  directly (decision 36).

The lines of the S2b parts of ADR-020 and ADR-048 that say this ADR holds no S2b part
at `bbfd839` are not edited: each states what that commit held.
