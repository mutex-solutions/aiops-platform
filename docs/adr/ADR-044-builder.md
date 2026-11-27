# ADR-044: The builder for a self-hoster on one VM, possibly air-gapped

- Status: **accepted 2026-11-23 by the project owner.** The owner answered all 13
  questions on the same date with the recommended option of each; the section "Owner
  answers of 2026-11-23" records them as decisions 15 to 27. ADR-060 decision 2 required
  this acceptance before the builder code merges into `main`. The S2b part, the last
  section of this file, is **proposed 2026-11-27, awaiting the project owner**: nothing
  in it is decided, and it changes no decision above.
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
   the signed manifest.
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
    is a dated addition proposed apart.

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

- Status: **proposed 2026-11-27, awaiting the project owner.** Decisions 28 to 37 are
  proposals and questions Q14 to Q17 are open; nothing below is decided until the owner
  answers. The code this part governs merges into `main` only after it is accepted
  (ADR-063 decision 2).
- Scope: the S2b part of this ADR, as ADR-063 decision 34 lists it: the release modules
  the builder serves; the suite binaries that drive a remote process; the writer of the
  new `serves` fields, `remoteProtocol` and the `remote` mark (ADR-063 decision 46); and
  the gRPC codec exemption, read again if the pin of `google.golang.org/grpc` moves
  (ADR-020 decision 27). Decisions 1 to 27 stand. This part changes no decision of
  `00-decisions.md`; every line an answer would edit is named in "Edits on acceptance
  of the S2b part".
- It blocks: the builder change that serves `sdk/remote` and `sdk/remotehost`; the
  reader and the writer of the new `serves` fields, which change in one commit, and with
  them the majors clause of check 3 and check 9; and step 5 of `plugin check` for the
  runtime `remote`.
- Citations: in this part `00`, `07` and `08`, and every path with a line, are at commit
  `bbfd839`, where the sections above cite `8e1f7b9`: the builder paragraph is `08:402`
  above and `08:404` here. ADR-020, ADR-048, ADR-060 and ADR-063 are cited by decision.
  The facts on gRPC name `google.golang.org/grpc@v1.82.1`, whose files were read on
  2026-11-27 as `go mod download -json` placed them in the Go module cache, at the sum
  `h1:NnAxzGRA0677vCa4BUkOAnO5+FfQqVl9iUXeD0IqcGE=` that `go.sum:402` records.

### Context of the S2b part

ADR-063 fixes what S2b asks of the builder, and this part reopens none of it:

- From S2b the SDK is five Go modules: `sdk/remote` and `sdk/remotehost` are nested
  modules, each with a closed require list, and the builder serves six modules (ADR-063
  decision 43; `07:23`; `00:263`). Each new module gets a served `go.mod` copy and its
  place in the builder's list of release modules (ADR-063 decision 7). The generated Go
  of a `contrib` definition lives in the module `sdk/remote`, under
  `sdk/remote/def/<name>/v<N>` (ADR-063 decision 44; `07:28`).
- gRPC and Protocol Buffers are imported from S2b by the remote modules, the proxy
  plugin, the adapter generator and the check runner of `aiopsctl` (ADR-063 decision 42;
  `00:268`).
- `serves` gains `remoteProtocol`, the majors the binary speaks, and per served
  definition its `remote` mark, `contrib`, `callback` or `no`. The reader and the
  builder change in one commit, and `distVersion` stays 1 (ADR-063 decision 46). The
  majors clause of check 3 and the contribution-set clause of check 9 read the two
  fields at every entry point but `dist build` (ADR-063 decision 16).
- For the runtime `remote`, in every language, step 4 of `plugin check` starts the
  plugin's entrypoint as a local process and drives it over a Unix socket, and step 5
  runs the obligations half of each suite through the adapter against that process,
  with no Go toolchain for a non-Go plugin (ADR-063 decision 17; `07:1577-1578`;
  `00:305`). The subject of the suite is the proxy fiber, and the guard half never runs
  remote (ADR-063 decision 14). One implementation of the kernel side of the protocol
  serves a server and `plugin check` (ADR-063 decision 9). Only the check runner opens
  the socket, to a process that it started, and on it the handshake and check 3 run as
  on a server (ADR-063 decisions 23 and 54, reading 2).

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
  (`internal/builder/manifest.go:80`; `08:404`).
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
- The release `aiopsctl` embeds four test binaries: the pack suites of `commands`,
  `echoers` and `stampers`, and the echo scenario (`suites.go:31-41`, `:163-177`;
  ADR-060 decisions 46 and 49). Each runs the rows of a pack on first-party
  instantiable plugins, as a child process of `aiopsctl` with its input in the
  environment (`internal/authoring/tools.go:475-504`), and none opens a connection. The
  toolbox takes an embedded binary only for a directory that holds no Go code and
  compiles the suite in the author's module otherwise (`tools.go:439-463`). An
  `aiopsctl` built in the repository, or by a site's build, embeds none and answers
  `launch.ErrNotCarried` (`tools.go:476-484`; ADR-060 decision 46).
- The launcher's toolbox is a value of the `aiopsctl` process (`boot/launch.go:226-227`),
  and ADR-063 decisions 16 and 57 have the check runner reach the handshake entry of
  the gate "through the toolbox it already holds". A suite runs in a child process,
  which holds no toolbox.
- One function writes `serves` for every signed manifest, of a release, of a site and
  of a binary built in the repository: `plugingen.Serves`, which `Manifest.Write` calls
  on the `definition.yaml` of each package of the server's graph
  (`internal/plugingen/defs.go:157-167`; `internal/builder/manifest.go:59-70`, `:108`).
  It reads the `remote` mark of a definition and drops it (`defs.go:30`, `:85`,
  `:137-144`), and it takes `pluginApi` from the constant compiled into the builder
  (`defs.go:158`). Nothing in the repository lists the majors a binary speaks. The
  reader refuses an unknown field of `serves` and of a served major
  (`boot/distribution.go:241-242`, `:265-267`). A hook already carries its `remote` mark
  in `serves` (`defs.go:46-50`; `boot/distribution.go:107-110`).
- The exception of the `init` deny-list for gRPC's codec is keyed by package, module
  and version, v1.82.1 (`internal/builder/initscan.go:61-63`). That is the version of
  the root build list, where gRPC is an indirect requirement (`go.mod:102`). The scan
  reads the graph of the server's main package alone (`build.go:161`).

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
    - a release serves each at the release version, with a served `go.mod` that names
      every release module at that version and holds no `replace` line
      (`serve.go:26-45`, `:52-59`; ADR-060 decision 21), and the test of the served
      copies reads the six files (`internal/builder/serve_test.go:21`);
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
    version of each of the six. The second version of the two remote modules that step
    D1 of the S2b exit needs (ADR-063 decision 27) is a second serving, by the exit's
    pipeline, of a changed copy of the tree into the module source of the job. It is in
    no pin of the test release, and the builder asks a module source for exact versions
    only (decision 7), so it changes no build of that release.
30. **A release module that no release binary compiles** has its hashes recorded as
    question Q14 decides. Decision 8 checks a module against recorded hashes, and the
    release's signed manifest records the `go.sum` lines of the release's own build
    (ADR-060 decision 20). No binary that a release links needs a package of
    `sdk/remotehost` (decision 32), so that build writes the hash of its `go.mod` and
    not of its zip. A site's build lists every package of each module of `plugins`
    (`build.go:560-563`). One that holds a `main` on `sdk/remotehost` beside its
    in-process plugin compiles that module, finds no recorded hash for its zip, and
    under decision 8 prints the hash the module source gives and records it as checked
    against nothing, for a module that the release pipeline built.
31. **One binary drives a remote process**, as question Q15 decides: with (a),
    `suites/runtime-remote.test`. The release build writes its test file into the
    scratch module in both phases, so that its build list is in the release's pins. Its
    second phase compiles it beside the pack binaries, with `go test -c`, `-trimpath`,
    cgo off and the tag `grpcnotrace`, and the release `aiopsctl` embeds it (ADR-060
    decision 46; `suites.go:163-177`; `build.go:349-350`). It carries what steps 4 and
    5 of `plugin check` run in the runtime `remote` (ADR-063 decision 17). A suite
    mounts its subject on the kernel root of `kerneltest` and behind definition fakes,
    which only test files and the check runner import (`suites.go:84`; `07:33`;
    `08:1265`), and step 4 mounts the same proxy fiber behind the same fakes. Both
    steps therefore run in a test binary, as the suites of a pack do. A definition name
    holds no hyphen (`internal/plugingen/defs.go:53`), so the name collides with the
    suite binary of no registry.
32. **What the binary links, and what it does not.**
    - The kernel side of the protocol, the package of the module `sdk/remote` that the
      proxy uses: one implementation serves a server and `plugin check` (ADR-063
      decision 9).
    - The proxy code of the plugin `aiops/remote`, with `plugin.FromManifest` (ADR-063
      decisions 10 and 41). The subject of each suite is a proxy fiber of that code
      (ADR-063 decision 14), and the binary holds no second proxy.
    - For each registry of the builder's list (`suites.go:31`) whose `definition.yaml`,
      in the zip of the module `sdk` at the release version, is marked `contrib`: its
      suite, and its generated adapter of `sdk/remote/def/<name>/v<N>` (ADR-063
      decisions 13 and 44). At S2b these are `echoers` and `stampers`. The release
      build fails when a definition of that zip is marked `contrib` and the list lacks
      it, so no `contrib` registry of a release is without its suite in the binary.
    - The fakes of the definitions that the proxy fiber injects, and of each definition
      marked `callback` (ADR-063 decision 17). No definition of a release is marked
      `callback` at S2b: the test definition of ADR-063 decision 51 ships in no release
      table.
    - `boot`, for the handshake entry of the gate, as question Q16 decides.
    - The dial of a Unix socket. It is code of this binary alone: a server has no
      unauthenticated dial, by no flag, config field or fallback (ADR-063 decision 23).
      The package that holds it is refused in the graph of `cmd/aiops` by the import
      check of `make check`, which refuses the toolbox there today (`Makefile:197-203`).

    It links no package of `sdk/remotehost`: the process under check brings its own
    plugin-side host. It links no code of the plugin under check, in any language.
33. **How the binary is run.** As the pack binaries are: a child process of `aiopsctl`,
    with its input in the environment and a JSON report (`tools.go:475-504`; ADR-060
    decision 46). Its input is the plugin directory, the address of the socket and, with
    question Q16 (a), the signed manifest that `aiopsctl` embeds or that `--against`
    names. For the runtime `remote` the toolbox takes this binary in every language,
    whether or not the directory holds Go code and the machine a Go toolchain (ADR-063
    decision 17); today it takes an embedded binary only where the directory holds no
    Go code (`tools.go:439-463`). The binary runs the obligations half alone, and its
    report names that half (ADR-063 decision 14). The runner alone opens the socket, to
    a process that it started (ADR-063 decision 23). Which of the two processes starts
    the entrypoint, which test of the binary each step runs and the form of the report
    are the S2b part of ADR-061; the grammar of `entrypoint` is the S2b part of
    ADR-048; the command that the Python fixture wraps is ADR-041's (ADR-063 decision
    34).
34. **Which `aiopsctl` carries the binary, and what the binary adds to the reports.**
    - Only a release build embeds suite binaries. An `aiopsctl` built in the
      repository, or by a site's build, embeds none (ADR-060 decision 46), and for the
      runtime `remote` it answers `launch.ErrNotCarried` and names the binary, as it
      does for a pack (`tools.go:476-484`). A case that needs the binary runs on the
      `aiopsctl` of the test release (ADR-063 decision 37).
    - The packages of the binary enter the licence report, `THIRD_PARTY_LICENSES/`, the
      NOTICE texts and the SBOM of `aiopsctl`, and the SBOM of the builder image names
      the binary with its build list (decision 5; `build.go:872`). The scan classifies
      grpc-go and protobuf-go there at each build, as it classifies every module;
      ADR-063 decision 35 holds the reading of both at their pins.
    - The `init` deny-list reads the graph of the server's main package (decision 11;
      `build.go:161`), and the clause of check 7 on a `fake` or `sdk/testkit` package
      reads the graph of a server binary (`08:1265`). Neither reads this binary.
    - Neither `plugin smoke` nor `plugin verify` of remote code runs the builder. Smoke
      attaches the plugin's image to the released image (`07:1583`), and verify runs
      the suite against a pulled image (ADR-063 decision 17), with this binary. How
      verify starts that image and reaches it is the S2b parts of ADR-020 and ADR-061
      (ADR-063 decision 34).
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
      Whether it is always written is question Q17; with (a) it is a required field
      that holds `[]` for a binary that speaks none.
    - **Where the majors come from.** A binary speaks the protocol exactly when the
      package graph that its manifest is written from, the graph of the server's main
      package (`manifest.go:28-31`), holds the kernel side of the protocol. Its majors
      are then the one list of spoken majors of the module `sdk/remote` (ADR-063
      decision 27, step D1), the constant that `plugingen` reads for `requires`
      (ADR-063 decision 34), compiled into the builder as `pluginApi` is
      (`defs.go:158`; ADR-060 decision 8). A release builds only with its own builder
      and no release module is ever raised (decisions 1 and 28), so the list the
      builder holds is the list the binary links. Which package declares the list is
      settled with the constant, by the S2b part of ADR-048 (ADR-063 decision 34).
    - The kernel part of `serves` gains no field. Its hooks already carry a `remote`
      mark, `no` for each (`defs.go:113-127`), and its four keys belong to `pluginApi`
      and have no `definition.yaml` to take a mark from (ADR-048 decision 12). What
      check 9 answers a remote manifest that uses a built-in key is among the fields
      of that check, which the S2b part of ADR-048 settles (ADR-063 decision 34).
    - `serves` gains nothing else: no version of a Go module, which no gate reads
      (`08:904`; ADR-060 decision 21), and nothing of the suite binaries or of the
      adapters.
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
    - A moved pin cannot pass unread. The entry is keyed by package, module and
      version, and the scan admits the registration at that version only
      (`initscan.go:61-63`, `:234-242`). From the commit that moves the pin of
      `google.golang.org/grpc`, in the root `go.mod` or through the require list of a
      remote module, every build fails at the deny-list scan until the entry changes.
    - Read again means four things, in the pull request that moves the pin and at the
      new version as the module cache holds it. (1) The `init` of `encoding/proto`
      still registers gRPC's own proto codec with `RegisterCodecV2` and makes no other
      call. (2) The scan of the server's graph, built with the tag, reports no other
      registration of decision 11. (3) The tag still takes `golang.org/x/net/trace` out
      of that graph, which `TestGRPCNeedsTheTag` proves
      (`internal/builder/initscan_test.go:138`). (4) The entry of the table and the
      condition of the register entry (`tools/upstream-components.yaml:204`) name the
      new version in the same pull request (ADR-020 decision 27). A reading that finds
      what the last one found needs that reviewed pull request and no more (decision
      21). A reading that finds another registration adds no entry without the owner's
      word, as the three gob entries were added on 2026-11-25 (decision 21, its note).
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
37. **Tests.** Each fails on its base, and a test that is committed before its code is
    marked as ADR-063 decision 29 says.
    - The modules: the served `go.mod` of each of the six; the zips of decision 29; a
      raise of `sdk/remote` or of `sdk/remotehost`, refused with or without
      `--allow-bump`; an `aiops/` id declared in a module outside the six, refused as
      before; with question Q14 (a), a site's build that compiles `sdk/remotehost`
      checks its zip against the release's pin and records nothing as checked against
      nothing, and one whose module source serves another zip fails.
    - The binary: the `aiopsctl` of the test release embeds it, and the SBOM of the
      builder image names it with its build list; a release build fails on a `contrib`
      definition of the SDK zip that the builder's list lacks; an `aiopsctl` built in
      the repository answers `launch.ErrNotCarried` for the runtime `remote` and names
      the binary; the graph of `cmd/aiops` holds no package that dials the runner's
      socket.
    - `serves`: each served major carries the mark of its `definition.yaml`; the
      manifest of the test release holds `remoteProtocol: [0]`, and that of a test
      distribution whose graph holds no kernel side of the protocol holds what
      question Q17 decides; the reader refuses a served major with no `remote` or with
      another value, and a `remoteProtocol` that is out of order, holds a major twice
      or holds a negative number; the parity check holds every committed manifest.
    - gRPC: the scan reports the codec registration when the build list holds gRPC at
      another version than the entry names; a server graph that imports
      `encoding/gzip` fails on "a global codec"; `TestGRPCNeedsTheTag` passes with the
      proxy in the graph of `cmd/aiops`.

    The cases that drive the binary against a process are those of ADR-063 decisions
    27 and 28, on the `aiopsctl` of the test release.

### Consequences of the S2b part

- Six modules are served by a change of one list, and no rule of the builder is added
  for the two new ones.
- With question Q14 (a) every signed manifest of a release records a hash for each
  module the release serves, and a site's scratch module, which requires each pin that
  holds a zip hash (`build.go:311-318`), requires each of the six at the release
  version.
- The release `aiopsctl`, and with it the builder image, grows by one test binary that
  links gRPC, the proxy and the suites of the `contrib` registries. The licence report,
  the notices and the SBOM cover it with the code they have.
- An `aiopsctl` built in the repository runs no step 4 or 5 in the runtime `remote`.
  `make plugin-check` builds its `aiopsctl` in the repository (`Makefile:139-150`) and
  runs `plugin check` on the remote examples (ADR-063 decision 31); the steps that need
  the binary run on the `aiopsctl` of the test release, in the job that builds it
  (ADR-063 decision 37), as the connector case does (ADR-060 decision 24).
- Every signed manifest changes by two fields, and a manifest written before the change
  is refused by the reader after it. No distribution manifest exists outside the tests
  of the repository (ADR-063, question Q9 (a)).
- The gRPC exemption is unchanged at S2b, no exception is added, and the compressor of
  `encoding/gzip` stays a finding.
- `internal/builder` and `internal/packsuite` stay inside the provisional caps of
  ADR-063 decision 36, 4,400 and 800 lines.
- Nothing else of this ADR changes: the two paths, the builder image and its missing
  base layer, the module cache format, the rule of `--allow-bump`, the content of the
  deny-list and the site's entries are as decisions 1 to 27 have them.

### Edits on acceptance of the S2b part

Until the owner answers, nothing outside this file is edited. The commit that records
the answers edits this section, its status and the decisions that the answers settle,
and the status of this ADR in `00:782` and in the ADR table of `08` §3 (`08:1178`),
which then name the S2b part. For the recommended options it adds a dated note to:
decision 8 above and ADR-060 decision 20, for the hashes of the served modules in the
pins (Q14 (a)); ADR-060 decision 46, for the binary `suites/runtime-remote.test` (Q15
(a)); ADR-063 decisions 16 and 57, for the part of the toolbox through which the check
runner reaches the handshake entry (Q16 (a)); and ADR-060 decision 9, for the two
fields of `serves` (decision 35; Q17 (a)). No recommended option edits another line of
`00` to `08`. An option the owner does not choose edits nothing.

### Questions for the owner on the S2b part

Each question gives its options, the recommended one, why, and what the answer blocks.

**Q14. Does the signed manifest record a hash for a release module that no release
binary compiles?** The pins are the `go.sum` lines of the release's own build (ADR-060
decision 20), and go writes the hash of a zip only for a module whose packages the
build lists. At S2b that leaves `sdk/remotehost` with the hash of its `go.mod` alone
(decision 30). Decision 8 has a site's build take the hash of a version that no source
records from the module source and record it as checked against nothing.
- (a) The release build records, for each of the six modules it serves, the hash of its
  zip and of its `go.mod` as its module source holds them, whether or not a release
  binary compiles it. A site's build then checks each against the signed manifest.
  Decision 8 and ADR-060 decision 20 gain a dated note: the pins hold these lines
  beside the `go.sum` lines of the release's build.
- (b) As built: the pins are the lines of the release's own build. A site's build that
  compiles `sdk/remotehost` prints its hash and records it as checked against nothing.
- (c) A release binary links it: the binary of decision 31 imports a package of
  `sdk/remotehost`, so the release's build lists it. A package is then linked for its
  hash alone, into a binary that drives the host of another process.
- Recommended: (a). Every module the release serves is then checked by every build that
  resolves it, no first-party module is ever recorded as checked against nothing, and
  nothing is linked that no binary runs. Blocks: the builder change that serves
  `sdk/remote` and `sdk/remotehost`.

**Q15. How many binaries drive a remote process?** ADR-060 decision 46 embeds one test
binary per registry suite, each for the rows of a pack. Step 4 of the runtime `remote`
makes one activation with every registration the manifest lists, and step 5 runs the
suite of each registry the plugin contributes to (ADR-063 decision 17).
- (a) One, `suites/runtime-remote.test`. It links the proxy, the kernel side of the
  protocol and gRPC once, and every `contrib` registry of the release with its suite
  and its adapter (decision 32). `aiopsctl` runs it once for step 4 and once per
  definition for step 5.
- (b) One per `contrib` registry, `suites/runtime-remote-<name>.test`. Step 4 registers
  with every registry the manifest lists, so one of them must still link them all, or
  a further binary runs step 4. Each links the proxy and gRPC again.
- (c) No new binary: the pack binary of each `contrib` registry gains the remote tests.
  The suite binary that runs a pack's rows then links the proxy, gRPC and the socket
  dial, and step 4 has the difficulty of (b).
- Recommended: (a). Step 4 needs one binary that holds every `contrib` registry in any
  case, the code that dials is linked once, and the pack binaries stay as ADR-060
  decision 46 has them. Blocks: step 5 of `plugin check` for the runtime `remote`.

**Q16. Where does the binary get the verdict of the handshake?** On the socket of
`plugin check` the handshake and check 3 run as on a server (ADR-063 decision 54,
reading 2), and `boot` is the one home of check 3 (ADR-063 decision 16). The same
decision, with decision 57, has the check runner reach the handshake entry "through the
toolbox it already holds", the one that the operations row resolves. That toolbox is a
value of the `aiopsctl` process (`boot/launch.go:226-227`), and a suite runs in a child
process (ADR-060 decision 46). The owner decides because two accepted decisions word
the path.
- (a) The binary links `boot`, as the pack binaries do
  (`internal/packsuite/packsuite.go:31`), and reaches the handshake entry there, in its
  own process, on the signed manifest that `aiopsctl` hands it. The words of ADR-063
  decisions 16 and 57 are read as: through the `Suite` part of the toolbox
  (`sdk/def/launch/v0/toolbox.go:54-56`), whose binary runs `boot`'s own code. No copy
  of check 3 exists outside `boot`. Both decisions gain a dated note.
- (b) The entry stays in the `aiopsctl` process, and the binary asks its parent for
  each verdict over a pipe. A second wire format, between `aiopsctl` and its own child,
  then carries what the gate reads of a connection.
- (c) `aiopsctl` links the kernel side of the protocol and makes one handshake itself,
  through its toolbox, before it starts the binary, whose own connections take a fake
  of the entry. The suite then runs on connections whose handshake no gate evaluated,
  against reading 2 of ADR-063 decision 54.
- Recommended: (a). One code evaluates check 3 at every entry point, nothing new
  crosses a process boundary, and the binary links `boot` as its neighbours do. Blocks:
  step 5 of `plugin check` for the runtime `remote`, and the handshake row of ADR-063
  decision 16 at that step.

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
