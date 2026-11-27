# ADR-020: Signing and trust of plugin artifacts and distributions

- Status: **accepted 2026-11-23 by the project owner.** The owner answered all 12
  questions on the same date with the recommended option of each; the section "Owner
  answers of 2026-11-23" records them as decisions 19 to 30. The commit that records the
  answers edits `00-decisions.md` and the other lines they name (`08:996`), as that
  section lists.
- Scope: the S2a part of ADR-020 (`08:1151`), as ADR-060 decision 33 lists it: the
  signing library of `plugin sign` and `dist sign`; the signature format that cosign
  verifies in key mode; keyless signing; the key format and the id patterns of `trust
  add`; trust for a `signed` key; the attestation type and its subject per kind of
  artifact; where the verified signer and the attestation are recorded and how both are
  protected until boot; how boot ties the module table it runs to the signed manifest
  that `dist use` verified; the default release root of decision 50; and the transitive
  licences of sigstore-go. The index signature is S6a and is touched only in decision 17.
- Citations: `00`, `01`, `07` and `08` are the files of `docs/architecture/` with those
  numbers, and `08:404` is line 404 of `08` at commit `8e1f7b9`. "community" and
  "licence" are `docs/design/community-first-2026-11.md` and
  `licence-audit-2026-11.md`, cited the same way. The plugin record,
  `docs/design/plugin-system-2026-11.md`, is cited by section. ADR-060 is cited by
  decision number. An upstream file is cited as `<module>@<version>/<path>:<line>`; each
  was read on 2026-11-23 in the Go module cache after `go mod download -json
  <module>@<version>`, never from memory. A file of the Go toolchain is cited as
  `go1.26.8/src/<path>:<line>`, read in that toolchain's own source tree.

## Context

### What the documents already fix

- Signing is cosign in key mode, as a tool of the release pipeline that is never baked
  into an image of the project. Verification runs in `aiopsctl` and on the
  `plugin-manager` side and links sigstore-go; `boot/` imports neither, and no signature
  verification is written by hand (`08:404`; `08:1380`; `00:757`; licence:49).
- R17 leaves open what `plugin sign` and `dist sign` run on an author's or a site's
  machine, and how the gate in `boot` learns that a signature was verified; section 9 of
  `07` fixes only that both commands write the format the release pipeline writes, and
  that check 5 compares the signer recorded beside a digest with `trust.yaml`
  (`07:75`, `:1846`, `:1852`). Where that record is kept and how it is protected until
  boot is open under ADR-020 (`07:1841`; ADR-060 decision 26).
- The gate reads manifests and what install recorded beside each digest, and verifies
  no signature itself (`07:1880`; `08:493`). Check 5 denies when no verified signer is
  recorded beside the digest, when that signer is outside `trust.yaml` for the id and
  runtime, and for a Go module absent from a trusted distribution manifest
  (`07:1888`; `08:501`). Check 7 denies a `signed` key or hook used by an untrusted signer
  (`07:1890`; `08:503`). Check 10 denies a missing attestation or one that names another
  subject digest (`07:1893`; `08:506`).
- A signed key is injected only by first-party plugins or by an in-process plugin whose
  signer `trust.yaml` trusts for that key (`07:558`; `00:245`).
- `trust.yaml` holds `signers`, each limited to id patterns and runtimes,
  `distributions` and `allowUnsigned` from S2a (`08:404`, `:938`; community:206; ADR-060
  decision 26). `requireAttestations` per signer is this ADR's (ADR-060 decision 26).
- The attestation is a typed statement whose type carries its version (`08:920`;
  community:154), with the fields of ADR-060 decision 18. Its subject is the digest of
  what the gate admits, and this ADR fixes that digest per kind of artifact, the type URI,
  and whether the statement is a layer of the artifact or an OCI referrer (ADR-060
  decision 18).
- The release binary carries the release key as a default distribution root and its own
  signed manifest as recorded with that key; boot prints it at every boot, and a site
  removes it in `trust.yaml`; checks 5 and 10 run on every row (ADR-060 decision 50;
  `07:1861`; `08:404`).
- In-process third-party code enters only through the builder and a signed distribution
  manifest (community:111, G9). Boot ties the module table it runs to the manifest that
  `dist use` verified by the digest of the manifest the binary embeds, and denies every Go
  module when they differ (ADR-060 decisions 31 and 33).
- The S1 suites and the exit sign with throwaway keys made inside the test, and no key is
  committed (ADR-060 decisions 28 and 30). CI runs cosign against `aiopsctl` in both
  directions in key mode (ADR-060 decision 36). Nothing is published outside the private
  repository (ADR-057 decision 5; `08:1343` L14).
- A new linked module merges only with its licence read at the pinned version, its
  transitive licences scanned on every shipped artifact that links it, `aiopsctl`
  included, and a register entry (ADR-060 decision 34; `08:1332` L3, `:1342` L13). The
  transitive licences of sigstore-go are read before S2a ends (`08:1337` L8; `:1342` L13).
- The plugin system keeps the dsh model: admission reads manifests only at composition
  boundaries, the index is signed where dsh's is not, and three keys are `signed`
  (plugin record §15, rows 34, 41 and 45). Signing is an addition outside the kernel, and
  no kernel change is planned (ADR-060 decision 3).

### What the pinned sources show

1. **Versions.** sigstore-go v1.4.1 (tagged 2026-11-22) and v1.4.0 (2026-10-29) declare
   `go 1.27.0`; v1.3.0 (2026-07-30) declares `go 1.25.8`
   (`github.com/sigstore/sigstore-go@v1.3.0/go.mod:3`). The repository builds with
   `toolchain go1.26.8` under `GOTOOLCHAIN=local` (`go.mod`; ADR-060 decision 21). The
   latest cosign, v3.1.4 (2026-10-20), requires sigstore-go v1.3.0 and declares `go 1.27`
   (`github.com/sigstore/cosign/v3@v3.1.4/go.mod:3`, `:40`).
2. **cosign writes Sigstore bundles by default.** In v3.1.4 `--new-bundle-format`
   defaults to true and is deprecated as "the only supported format in future versions"
   (`github.com/sigstore/cosign/v3@v3.1.4/cmd/cosign/cli/options/sign.go:161-162`; the
   paths of items 2 to 4 are in that module unless named). `cosign sign` signs an in-toto
   Statement v1 whose one subject is the image's digest and whose predicate type is
   `https://sigstore.dev/cosign/sign/v1`, in a DSSE envelope inside a bundle, and pushes
   the bundle as an OCI referrer of the image (`cmd/cosign/cli/sign/sign.go:158-229`;
   `pkg/types/predicate.go:19`; `pkg/oci/remote/write.go:314-421`). `cosign attest
   --type` accepts any URI as the predicate type
   (`cmd/cosign/cli/options/predicate.go:65-78`), but for a URI it does not know it writes
   an in-toto Statement v0.1, `_type` `https://in-toto.io/Statement/v0.1`, whose one
   subject is named by the image's repository (`cmd/cosign/cli/attest/attest.go:113-118`;
   `pkg/cosign/attestation/attestation.go:242-266`;
   `github.com/in-toto/in-toto-golang@v0.11.0/in_toto/attestations.go:14`). So cosign
   attaches a predicate type of the project's own, the point `07:1512`, `08:935` and
   community:203 mark UNVERIFIED, but not in the Statement v1 of decision 9.
3. **cosign uploads to a public log by default.** `--tlog-upload` defaults to true,
   deprecated in favour of a signing config that names no transparency log
   (`cmd/cosign/cli/options/sign.go:125-127`, `signblob.go:127-129`), and the default log
   is `https://rekor.sigstore.dev` (`cmd/cosign/cli/options/rekor.go:22`). A verification
   requires a log entry unless `--insecure-ignore-tlog` is given (`options/verify.go:55`),
   and with it and a key cosign verifies with sigstore-go's `WithNoObserverTimestamps()`
   (`pkg/cosign/verify.go:265-287`).
4. **Key files.** `cosign generate-key-pair` writes an ECDSA P-256 key
   (`pkg/cosign/keys.go:95-101`) as a PEM block `ENCRYPTED SIGSTORE PRIVATE KEY`, a PKCS #8
   key encrypted with `github.com/secure-systems-lab/go-securesystemslib/encrypted`, and
   a PEM `PUBLIC KEY` (`keys.go:42-44`, `:199-245`); it still reads the older type
   `ENCRYPTED COSIGN PRIVATE KEY` (`keys.go:280-296`). Both tools put the same key hint in
   a bundle, the base64 of the SHA-256 of the DER public key
   (`github.com/sigstore/sigstore-go@v1.3.0/pkg/sign/keys.go:102-108`;
   `github.com/sigstore/cosign/v3@v3.1.4/internal/key/svkeypair.go:56-57`).
5. **sigstore-go verifies key mode offline.** `verify.WithKey()` requires a key and no
   certificate, `verify.WithNoObserverTimestamps()` expects no log entry and no timestamp,
   and `verify.WithArtifactDigest` matches the digest against the subjects of a DSSE
   statement (`github.com/sigstore/sigstore-go@v1.3.0/pkg/verify/signed_entity.go:233-238`,
   `:470-480`, `:560`; `pkg/verify/signature.go:334-380`).
6. **sigstore-go links gRPC.** Its `pkg/verify` and `pkg/bundle` import its `pkg/tlog`,
   which imports `github.com/sigstore/rekor-tiles/v2@v2.3.0/pkg/generated/protobuf`
   (`sigstore-go@v1.3.0/pkg/tlog/entry.go:37`), which imports
   `google.golang.org/grpc@v1.82.1`. gRPC imports
   `golang.org/x/net/trace`, whose `init` registers two handlers on
   `http.DefaultServeMux` (`golang.org/x/net@v0.57.0/trace/trace.go:120-131`), unless the
   build tag `grpcnotrace` is set (`google.golang.org/grpc@v1.82.1/trace_notrace.go:1`,
   `:23`); and `google.golang.org/grpc/encoding/proto`, whose `init` registers a codec
   (`encoding/proto/proto.go:35-37`). The builder's `init` deny-list refuses a transitive
   `init` that registers a default-mux handler or a global codec (`07:1860`; `08:402`).
   v1.4.1 requires rekor-tiles/v2 v2.4.2 (`sigstore-go@v1.4.1/go.mod:16`), which was not
   read.
7. **sigstore-go's signing package widens the graph.** Importing `pkg/sign` adds eleven
   modules to what verification links, two of them MPL-2.0 (decision 16). `pkg/sign`
   holds the Fulcio client, imports `rekor/pkg/client`, which brings the retrying HTTP
   library, and `sigstore/pkg/oauthflow`, which brings the OIDC browser flow, and reaches a
   command library through `rekor/pkg/pki/pkcs7` (`go mod why -m` on the program of
   decision 16).
8. **in-toto.** A statement is `https://in-toto.io/Statement/v1`, its DSSE payload type is
   `application/vnd.in-toto+json`, and a predicate type is a URI that SHOULD carry a
   version and MAY be unresolvable
   (`github.com/in-toto/attestation@v1.2.0/spec/v1/statement.md:32`,
   `envelope.md:45-46`, `field_types.md:31-46`).
9. **What the Go toolchain records in a binary.** The build information that
   `runtime/debug.ReadBuildInfo` returns holds a path and a version per module, and a sum
   only for a module with a version and no `replace`. The main module gets the version
   `(devel)`, or one derived from version control, with `+dirty` for an uncommitted tree,
   and no sum; a replaced module gets its replacement and no sum of its own
   (`go1.26.8/src/cmd/go/internal/load/pkg.go:2335-2349`, `:2613`). At `8e1f7b9` the
   repository has one `go.mod` at its root, whose module holds `plugins/`, `boot/`,
   `dist/` and `test/`, and the three SDK modules enter it through `replace` lines. In a
   build inside the repository the first-party and test plugins are packages of the main
   module and the SDK modules are replaced, so none of them has a sum.

## Decision

### Library and format

1. **sigstore-go signs and verifies; cosign stays a tool.** `plugin sign`, `dist sign`,
   step 7 of `plugin check`, `plugin install` and `dist use` call one signing and
   verification package, internal to the operations plugin of ADR-060 decision 41, which
   links `github.com/sigstore/sigstore-go` at the version question Q1 fixes; an upstream
   module is imported only inside the plugin that wraps it (`07:50`). Go's `internal` rule
   and `07:46` keep every other package from importing it, so the test harness of ADR-060
   decision 28 reaches it as every consumer does: through the key the operations plugin
   provides, on a kernel the test mounts with `kerneltest`, whose binding maps are off
   (`07:565`). No boundary rule changes. `boot`'s own tests need no signing code, since
   boot reads records and never a bundle (decision 12); they write record and
   `trust.yaml` fixtures with a key from `crypto/ecdsa`. This answers R17 for signing
   (`07:75`) and keeps the homes of verification that `08:404` and `07:1839` state:
   `aiopsctl`, and from S6a the `plugin-manager` side, which consumes the same plugin.
   The operations plugin, which imports the signature library, the OCI client and the
   YAML library, is a row of the `aiops-cli` bundle of the `cli` profile at S2a, not of
   `aiops-core`, so ADR-054 decision 23 ("no row of `aiops-core` imports an upstream
   module") holds unchanged; where `plugin-manager` sits from S6a is decided by the ADR
   of S6a (owner, 2026-11-23). `boot/` imports neither cosign
   nor sigstore-go and verifies no signature (`08:404`; `07:1880`). Which sigstore-go
   packages the signing half imports is question Q2; how its `init` registrations meet the
   builder's deny-list is question Q9. cosign is never linked and never baked into an
   image (`08:1380`); the release pipeline and the CI case of ADR-060 decision 36 run
   cosign v3.1.4, the release that requires sigstore-go v1.3.0 (Context item 1). With Q1
   (a) both sides of that case use the same sigstore-go version and differ in its
   dependencies: cosign selects sigstore v1.11.1, protobuf-specs v0.5.2,
   go-securesystemslib v0.11.1, gRPC v1.84.0 and protobuf v1.36.12
   (`cosign/v3@v3.1.4/go.mod:35-39`, `:60`, `:288`), and `aiopsctl` the versions of
   decision 16. CI installs cosign's published release binary, pinned by checksum, because
   its `go.mod` needs go 1.27; its licence is read in decision 16. Note of 2026-11-25:
   CI builds cosign v3.1.4 from its module with `go install` instead, by the toolchain
   go1.27.4 that its `go.mod` needs, each fetched through the module proxy and checked
   against the checksum database, so the job and `make cosign` build the same binary
   (ADR-060 decision 36, its note of 2026-11-25).
2. **One signature format: a Sigstore bundle v0.3 in key mode.** Every signature is a
   bundle of media type `application/vnd.dev.sigstore.bundle.v0.3+json` whose
   verification material is the key hint of decision 4, with no certificate, no
   transparency-log entry and no timestamp: the format cosign v3.1.4 writes by default and
   sigstore-go verifies with `WithKey()` and `WithNoObserverTimestamps()` (Context items 2
   and 5). It takes three forms:

   | Signed object | Bundle content | Where the bundle travels | cosign writes it with, and checks it with |
   |---|---|---|---|
   | An OCI artifact (ADR-060 decision 19) | A DSSE envelope of an in-toto Statement v1 with predicate type `https://sigstore.dev/cosign/sign/v1`, an empty predicate and one subject, the artifact's digest (decision 9) | An OCI referrer of the artifact, artifact type the bundle media type, annotation `dev.sigstore.bundle.predicateType` | `cosign sign`; `cosign verify` |
   | The attestation of an OCI artifact | The same, with the predicate type and predicate of decision 9 | Where question Q6 puts it | Not written by cosign: `cosign attest --type` writes a Statement v0.1 whose subject is named by the repository (Context item 2), so only `plugin sign` writes it. The CI case of ADR-060 decision 36 covers rows 1 and 3 |
   | A blob: at S2a the signed distribution manifest | A message signature over the SHA-256 of the exact bytes | Decision 12 | `cosign sign-blob`; `cosign verify-blob --bundle` |

   The key is ECDSA P-256 with SHA-256, the default of both tools (Context item 4;
   `sigstore-go@v1.3.0/pkg/sign/keys.go:59`), and `aiopsctl` refuses another algorithm at
   S2a. No bundle carries a log entry or a timestamp: an entry in a public log publishes
   the digest and the key, which L14 and ADR-057 decision 5 rule out (`08:1343`), and the
   exit's environments reach only the job's module proxy and registry (ADR-060 decision
   30). The release pipeline and the CI case therefore sign with a cosign signing config
   that names no transparency log and verify with `--insecure-ignore-tlog` (Context item
   3). Without a trusted time a key has no validity window; decision 15 states the
   consequence.
3. **Keyless signing** is offered at S2a or not as question Q4 decides. Under its
   recommended answer `aiopsctl` signs and verifies in key mode only and refuses a bundle
   that holds a certificate, naming keyless as not offered. Keyless arrives additively:
   the bundle carries a certificate in that mode, and `trust.yaml` gains an identity form
   of signer entry.

### Keys and `trust.yaml`

4. **Key files are cosign's.** A public key is the PEM `PUBLIC KEY` block of the `.pub`
   file that `cosign generate-key-pair` writes beside its `.key` file
   (`cosign/v3@v3.1.4/cmd/cosign/cli/generate/generate_key_pair.go:47-48`); a private key
   is the PEM `ENCRYPTED SIGSTORE PRIVATE KEY` block of that `.key` file, or the older
   `ENCRYPTED COSIGN PRIVATE KEY`, decrypted with go-securesystemslib's
   `encrypted` package, which adds no module to what verification links (decision 16). A
   password may be empty, as cosign allows. One key file therefore serves cosign in the
   release pipeline and `aiopsctl` on an author's or a site's machine. A key's id is the
   base64 of the SHA-256 of its DER public key, the hint both tools write (Context item
   4). `trust.yaml` holds the PEM text and each reader derives the id; `boot` does so with
   `encoding/pem` and `crypto/sha256`, which reads a key and verifies nothing. How an
   author who has only `aiopsctl` gets a key pair (`07:51`, `:1531`) is question Q3.
5. **`trust.yaml` at S2a** is one file per site under `$AIOPS_HOME` (`07:1840`) with the
   three fields of ADR-060 decision 26. Every entry holds a key; the word `release` in
   place of a PEM key names the default root of decision 13.

   ```yaml
   signers:
     - key: |
         -----BEGIN PUBLIC KEY-----
         ...
         -----END PUBLIC KEY-----
       ids: [acme/*]          # decision 6
       runtimes: [inproc]     # inproc, remote; empty covers packs only
       signed: []             # signed keys and hooks, decision 7
   distributions:
     - key: release           # the default release root, decision 13
     - key: |
         -----BEGIN PUBLIC KEY-----
         ...
   allowUnsigned: []          # decision 8
   ```

   `aiopsctl trust add <public key file> --ids <pattern>[,...] [--runtimes ...]
   [--signed ...]` appends a signer, and `trust add <public key file> --distribution`
   appends a distribution root, both under the profile write lock (`08:369`). The other
   fields of `08:404` and community:206 wait: `indexes` for S6a (ADR-060 decision 26) and
   `requireAttestations` per signer as question Q8 decides.
6. **Id patterns and runtimes.** A pattern is an exact id `<owner>/<name>` or
   `<owner>/*`, with owner and name under the id rules (`07:121`); no other wildcard
   exists, so a signer is tied to the owners it is listed for, as `07:125` requires.
   `trust add` refuses the owners `aiops` and `contrib`, which are reserved to artifacts
   the release pipeline builds (`07:121`; `08:416`) and reach a site at S2a only through
   the release table (decision 13). An entry covers an artifact when its id matches a
   pattern and every runtime its manifest lists is in `runtimes`. A pack lists none
   (`07:129`), so `trust add <key> --ids <pattern>` with no `--runtimes` covers packs
   only, as `08:395` writes it, and the remote and in-process forms name their runtime
   (`08:396`; `07:1854`; ADR-060 decision 30, step C1). `wasm` is refused, since check 1
   denies it (ADR-060 decision 58).
7. **Trust for a `signed` key or hook** (`07:558`, `:443`). The clause of check 7 passes
   for a row whose plugin is first-party, that is of owner `aiops`, which the builder
   admits only from the release table (`07:121`), and for a row of an installed artifact
   whose recorded signer (decision 10) has a covering entry whose `signed` list names the
   key, as `identities`, or the hook, as `http/request`. A key in `signed` counts for an
   `inproc` artifact only, since only in-process code may inject a signed key
   (`07:558`). A hook in `signed` counts for every runtime the entry covers, since a field
   the definition marks raw puts a remote listener in the `signed` class (`07:443`); the
   remote runtime arrives at S2b. Where no signer is recorded, as at `dist build`, the
   clause passes a first-party row and reports "not evaluated" with that reason for any
   other (question Q12, reading 3). At S2a only a test definition declares a signed key
   (ADR-060 decision 15).
8. **`allowUnsigned`** lists exact ids. An artifact whose id it lists and that carries no
   signature a covering entry verifies passes the signature clause of check 5 with the
   signer recorded as `unsigned`, printed at every boot (`08:404`). Check 10 still applies
   (ADR-060 decision 16): the attestation of such an artifact is the unsigned statement
   that `plugin pack` recorded (decision 9), accepted for a listed id only (question Q12,
   reading 2). A row recorded `unsigned` is never trusted for a signed key or hook.

### The attestation

9. **Type, predicate and subject.** The attestation is an in-toto Statement v1 in a DSSE
   envelope inside the bundle of decision 2; a reader refuses another `_type`, the
   Statement v0.1 of `cosign attest` included (Context item 2). Its predicate type is the
   URI of question Q7, ending in `/v1` so that the type carries its version (`08:920`;
   community:154). The predicate is frozen-additive, as the manifest schema is
   (`08:915`): a reader ignores an unknown field, and a change to an existing field is a
   `v2`. Its fields are those of ADR-060 decision 18:

   ```json
   {
     "_type": "https://in-toto.io/Statement/v1",
     "subject": [{"name": "acme/echoer@0.1.0", "digest": {"sha256": "<hex>"}}],
     "predicateType": "<the URI of question Q7>",
     "predicate": {
       "definitions": {
         "echoers": {"version": "0.1", "suite": 1, "halves": ["obligations", "guard"]},
         "<a definition it only consumes>": {"version": "0.1"}
       },
       "notRun": ["<name of a generated test that did not run>"]
     }
   }
   ```

   `definitions` names every definition of `requires` with its `major.minor`, and adds the
   suite revision and the halves that ran for each definition the plugin provides,
   contributes to or, for a pack, feeds (ADR-060 decisions 10 and 56). A pack adds
   `"scenario": {"name": "echo", "version": 1, "halves": ["kernel"]}` (ADR-060 decision 49).
   `notRun` lists the generated tests that did not run (ADR-060 decision 43). The harness
   level of `07:1512` is an added field at S8a. The subject is the digest of what the gate
   admits, per kind:

   | Kind | Subject | Where the statement is | Signed by |
   |---|---|---|---|
   | An OCI artifact of any runtime: in-process code, a remote plugin from S2b, a pack | Name `<id>@<version>`. The digests follow question Q6: under (a), one subject, the SHA-256 of the artifact's OCI manifest, the digest install pulls by and, for a remote plugin or a pack, the lock names; under (b), one subject per other layer of the artifact | Under Q6 (a) an OCI referrer of the artifact, under Q6 (b) a layer of it | The artifact's signer, with `plugin sign` |
   | An entry of a release's module table: a first-party plugin (ADR-060 decision 50), or a plugin of a test distribution (ADR-060 decisions 28 and 55) | Name `<id>@<version>`; digest the SHA-256 of the plugin's generated manifest as the signed distribution manifest holds it | Inside the signed distribution manifest, beside that entry | The signer of that manifest |

   The artifact's own fields pin its code: the module path, version and sum of in-process
   code, the image digest of a remote plugin (ADR-060 decision 19), so the statement
   covers the code those fields name. For in-process code, boot holds those fields equal
   to the module the binary runs (decision 12). A module-table entry's code is tied to the
   binary by decision 12: by path, version and sum in a builder build, and as question Q10
   decides in a build inside the repository. A site's `dist build` copies the statements
   of the release's first-party entries from the release's signed manifest that `release`
   names (`08:401`) into the manifest it writes, with the entries' manifests unchanged.
   `plugin pack` records the statement unsigned where Q6 puts it, and `plugin sign` adds
   the signature of decision 2 and replaces the statement with its signed bundle; under
   Q6 (b) that rewrites a layer and gives the artifact a new digest. `plugin sign` refuses
   a statement whose subjects are not those of Q6's rule. Check 10 is then decidable at
   every entry point: under Q6 (a) the subject equals the digest of the artifact held,
   under Q6 (b) the subjects equal the digests of its other layers, and for a module-table
   entry the subject equals the digest of the manifest the module table holds.

### Records and how boot reads them

10. **Where the verified signer and the attestation are recorded.** Each digest that a
    verification by `aiopsctl` accepted has one directory,
    `$AIOPS_HOME/verified/sha256-<hex>/`, written only by `plugin install` and by `dist
    use`, and, for the S1 suites and the `aiopsctl` that the repository builds (decision
    13), by the test harness of ADR-060 decision 28 through the same code (decision 1).
    Each is written under the profile write lock (`08:369`), after every clause the entry
    point evaluates has passed (ADR-060 decision 17), and restored with `plugins.lock` on a
    failure (`08:374`). Beside the directories, `$AIOPS_HOME/verified/current` holds the
    digest of the distribution manifest that `dist use` verified last (decision 12).

    | Record of | Files |
    |---|---|
    | An artifact | `record.yaml` with `kind: artifact`, the digest, the signer's key id or `unsigned`, the plugin id, version and runtimes, and for in-process code its module path, version and sum; `signature.sigstore.json`, and `attestation.sigstore.json` as pulled from where question Q6 puts it; `attestation.json`, the statement that was verified |
    | A distribution manifest | `record.yaml` with `kind: distribution`, the digest of the manifest's bytes and the signer's key id; `manifest`, those bytes; `manifest.sigstore.json` |

    The record of an in-process artifact is its attestation recorded until boot, as
    ADR-060 decision 33 asks. The first `plugin install` of in-process code changes no file
    and the run after `dist use` writes the record (ADR-060 decision 30, step C2).
    `plugins.lock`, whose place ADR-047 fixes, names the digest of every installed remote
    plugin and pack with no code. In-process code has no entry in it, since the signed
    manifest pins the module (ADR-047 decisions 8 and 18; owner, 2026-11-23). Boot finds
    the record of an in-process row by its plugin id in the records install wrote under
    `verified/`, since `record.yaml` carries the plugin id, and not through the lock: the
    id of the row's origin artifact, which the loader records (ADR-060 decision 19), or
    the row's own plugin id for a row with no origin artifact (decision 12). Boot reads
    records and never writes
    them, and reads `trust.yaml` at every boot, so a key removed from it denies at the next
    boot without a record being touched.
11. **How the records are protected until boot** is question Q5. Under its recommended
    answer a record, and `current`, are as strong as `trust.yaml`, which nothing protects
    beyond the site's file ownership either: they are written only by `aiopsctl` running
    as the site's user; a record is named by the digest it vouches for, so it cannot vouch
    for other content; and it keeps the bundles it was verified from, so `aiopsctl` can
    verify it again. ADR-060 decision 33 leaves to ADR-047 whether `trust.yaml` and
    `verified/` are mounted read-only or read-write into a kernel that runs as a
    container; this ADR requires read-only (decision 18). A process that runs as the
    site's user can rewrite a record, and can rewrite `trust.yaml` too; on self-host a
    gate check is an honest refusal and not a lock (`08:508`).
12. **How boot ties the module table it runs to the verified manifest** (`07:1861`;
    community:111, G9; ADR-060 decisions 31 and 33).
    - The binary embeds the signed distribution manifest it was built with (ADR-060
      decision 20). Boot computes the SHA-256 of the embedded bytes.
    - `dist use <image>` writes the digest it verified into `$AIOPS_HOME/verified/current`
      (decision 10); a later `dist use` replaces it, and the records of earlier
      distributions stay for audit only. The embedded manifest is trusted when its digest
      is the current one and that record's signer is a key of `distributions`, or when the
      binary's built-in record of decision 13 names that digest and `release` is a trusted
      root (question Q12, reading 5). Rolling back to a site image used earlier therefore
      takes `dist use` again, which verifies it against the present `trust.yaml`.
    - Each entry of the generated module table carries the import path of the package that
      declares its plugin value, which the generator writes from the import the generated
      file makes (`boot.ModuleEntry.Module` holds a module path set by hand at
      `8e1f7b9`). Boot reads `runtime/debug.ReadBuildInfo` and takes as the entry's module
      the main module or the dependency whose path is the longest prefix of that import
      path, with its replacement where one is recorded (Context item 9).
    - In a builder build the trusted manifest lists that module with the path, version and
      sum that build information records, and boot compares all three. The builder's main
      module is the one it generates in its scratch directory, which holds the module table
      and no plugin, and its `go.mod` holds no `replace` line (decision 18), so every
      entry's module is a dependency with a sum. In a build inside the repository, that is
      the S1 suites, `test/dist`, and `go build` of `cmd/aiops` or `cmd/aiopsctl`, the root
      module is the main module and the SDK modules are replaced, so no entry has a sum;
      what the manifest records and boot compares for such a module is question Q10.
    - A difference denies the rows of that module by check 5.
    - When the embedded manifest is not trusted, every Go module is absent from a trusted
      distribution manifest, so check 5 denies every row (ADR-060 decision 31). Each row is
      detached and a required row stops boot (`08:467`); the refusal names the embedded
      digest, the current digest and `aiopsctl dist use <image>`, and prints the recovery
      of decision 13.
    - For a row of an installed in-process artifact, the record of its origin artifact, or
      for a row with none the record of its plugin id, each found by plugin id in the
      records under `verified/` and not through `plugins.lock`, holds the module path,
      version and sum the artifact pins (decision 10). They must equal the
      module that boot resolved for the row's module-table entry, which the trusted
      manifest lists. Otherwise the row is denied by check 10, as an attestation of another
      subject (`07:1893`): the statement covers the artifact, and the artifact pins other
      code than the binary runs, as after a site rebuilds with another version of the
      module and runs `dist use` without installing it again.
    - `boot`'s table tests, which ADR-060 decision 31 asks for, give the tie a
      `debug.BuildInfo` of each form: a builder build whose entry equals the manifest's,
      and one whose version or sum differs; a build inside the repository, under the answer
      to Q10; a site-built binary whose manifest has a record that is not current, which is
      denied; a release binary with `release` trusted and with it removed; and an
      in-process row whose origin record pins another version of the module than the
      binary runs, denied by check 10. The S1 suites boot the in-repository form for real
      (ADR-060 decision 28), and the exit the builder form (ADR-060 decision 30).
    - `dist sign <image>` signs the manifest as a blob and pushes the manifest and its
      bundle as one OCI referrer of the image, whose media types ADR-047 names with the
      rest of the OCI layout; the release pipeline signs with `cosign sign-blob` and
      pushes the same referrer, and `release` in a site's `dist.yaml` names it by digest
      (`08:401`). `dist use <image>` reads that referrer, verifies the bundle with each
      key of `distributions`, and writes the record and `current`, or refuses and writes
      nothing.
    - Note of 2026-11-24: the distribution manifest names no image, and the subject of
      its referrer is not signed, so a signed referrer copied onto another image verified
      as well, and `dist use` wrote that image into `image.yaml`, from which the host
      starts the kernel container with the home bound (ADR-047 decisions 13 and 15); a
      check inside the image cannot help, since the image is the attacker's. `dist sign`
      therefore also signs the image's digest as `plugin sign` signs an artifact's: a
      DSSE envelope of an in-toto statement of predicate type
      `https://sigstore.dev/cosign/sign/v1` whose one subject is the digest of the image
      manifest, pushed as one more referrer of the image in the bundle form of cosign,
      the form `cosign sign --key` writes, so the release pipeline signs the release
      image with it. `dist use` writes nothing unless the key of `distributions` that
      verifies the manifest's bundle also verifies such a signature over the digest of
      the image it resolved. The record of decision 10 keeps the manifest's bundle only:
      the signature of the image vouches for `image.yaml`, not for the manifest. An image
      signed with the same key whose own embedded manifest is not the one `dist use`
      made current boots no kernel, since boot trusts the embedded manifest only where
      its digest is current.

### The default release root

13. **The release key as a default distribution root** (ADR-060 decision 50).
    - The release build takes the release public key as an input and runs in two phases,
      which decision 18 asks of ADR-044: the builder writes the manifest and stops, the
      release pipeline signs it with `cosign sign-blob`, and the builder then links the
      release binaries, the server of the release image and the release `aiopsctl`, which
      embed the manifest, the release public key and the manifest's bundle. Their built-in
      record is the digest of the embedded manifest with the release key as signer.
    - With no `trust.yaml`, or one without a `distributions` field, the trusted roots are
      `release`. Once the field exists, the entry `key: release` stands for the default;
      `trust add --distribution` writes that entry first when it creates the field, so
      adding a site key never drops the default, and a site removes the default by
      deleting the entry.
    - Boot prints at every boot whether the default root is trusted, with its key id, and
      whether the built-in record is the one in use. Boot does not verify the built-in
      bundle (`08:404`): the release pipeline verifies it with `cosign verify-blob` before
      it publishes, and `dist use` of the release image verifies it with sigstore-go.
    - A binary built without a release key, by `dist build` for a site or by `go build`
      inside the repository, carries no default root and no built-in record, and boots on
      the current distribution record only. The S1 suites write that record with a
      throwaway key (ADR-060 decision 28). The `aiopsctl` that `make check` and CI build
      inside the repository, to run `plugin check` on every plugin and the template job
      (ADR-060 decisions 32 and 36), starts the same way: a Go test builds it, signs the
      manifest it embeds with a throwaway key through decision 28's harness, writes a
      `trust.yaml` that lists the key under `distributions` and the record and `current`
      into a throwaway `$AIOPS_HOME`, and runs that `aiopsctl` against it. No exemption
      narrows checks 5 or 10; the tie of such a binary is the in-repository form of
      decision 12. Note of 2026-11-25: `TestPluginCheckOnEveryPlugin` and
      `TestBootOfEachPluginsRow` of `cmd/aiopsctl`, which `make plugin-check` and the CI
      step of that name run, build it so and write the home with `test/trusted`; check 10
      at its boot reads statements that name the suites that ran (ADR-060 decision 50).
      The template job still runs the toolbox in its own test process.
    - When no trusted root covers the embedded manifest, boot prints the recovery beside
      the refusal: `- key: release` under `distributions` restores the default root, and an
      `aiopsctl` built from the current distribution boots on its record (question Q11).
    - First-party rows pass check 5 through the signer of the trusted manifest and check
      10 through the statements it holds (decision 9), so both checks run on every row
      and the release starts as `08:398` and `01:292` state.

### Each entry point

14. **What each entry point verifies, and which check names a failure.**

    | Entry point | Verifies | A failure is |
    |---|---|---|
    | `plugin check` step 7, `plugin pack` | Signs with a throwaway key held in memory and verifies the bundle and the attestation against it (`07:1573`) | A failed step 7 |
    | `plugin install`, every kind | For each signer entry that covers the id and runtimes, the signature bundle against that key and the artifact's digest; the first that verifies gives the signer. Then the attestation bundle against the same key | No verifying signature: check 5. No verifying attestation: check 10, missing. One that verifies and names another digest: check 10, another subject (ADR-060 decision 30, step D2) |
    | `dist use` | The manifest's bundle against each key of `distributions`, and with the key that verifies it a signature of the image's digest (decision 12, note of 2026-11-24) | A refusal; no record and no `image.yaml` is written |
    | `plugin gate --against` | No signature, since check 5 is not evaluated there (ADR-060 decision 17); it reads the statement from the attestation bundle's payload | Check 10, on the statement as read |
    | `dist build` | No signature; checks 5 and 10 are not evaluated there (ADR-060 decision 17) | None of this ADR |
    | Boot and every live generation | No signature (`08:404`): records, `trust.yaml` and the embedded manifest, as decisions 10 and 12 state | Check 5, 7 or 10 |

### Key lifetime and rotation

15. **No key expires at S2a, and rotation is by `trust.yaml`.** With no timestamp in a
    bundle (decision 2), a signature has no time a verifier can trust, so a plugin signer
    key and a distribution key are valid while `trust.yaml` lists them. How this reads the
    lifetime "publisher-managed; set by ADR-020" of `00:411` is reading 1 of question Q12;
    it keeps `00:410`. A publisher rotates by adding the new key with `trust add`, signing
    again with `plugin sign`, which adds a signature referrer to the same digest, and
    having sites install again; a site rotates its distribution key by `dist sign` and
    `dist use` with the new key. Deleting the old
    entry then makes boot deny, by check 5, every row whose recorded signer it was. A
    compromised key is revoked the same way, and until it is deleted it signs artifacts
    that every site trusting it admits.

### Licences of sigstore-go at the pin

16. **Read at `github.com/sigstore/sigstore-go@v1.3.0`**, the recommended answer to
    question Q1, from the licence file at the root of each module in the module cache,
    and in each linked package's directory where one sits there. The package graph is
    that of a program importing sigstore-go's `pkg/verify`, `pkg/bundle` and `pkg/root`,
    sigstore's `pkg/signature` and `pkg/cryptoutils`, in-toto's `go/v1`, and
    go-securesystemslib's `encrypted` and `dsse`, beside the root module's present
    requirements under `GOTOOLCHAIN=local` with go1.26.8. It selects no other version of a
    module the root module links today, and its highest `go` line is 1.26.0. A licence file
    is classified by the texts it holds: the Apache-2.0 heading, the MPL-2.0 heading, the
    MIT grant, the ISC grant, or the BSD redistribution conditions, read as BSD-3-Clause
    when they hold the no-endorsement clause and as BSD-2-Clause otherwise. A file that
    holds several texts is classified under each, and every second licence file at a
    module's root is read too.
    - sigstore-go itself is Apache-2.0 (`LICENSE`; `COPYRIGHT.txt` names the Sigstore
      Authors).
    - Verification and the key format add 70 modules to the package graph of `aiopsctl`
      and of the server image, which at `8e1f7b9` link only `sdk/def`, `sdk/kernel` and
      `go.yaml.in/yaml/v3` (`go list -deps ./cmd/...`). The package graph of the program
      above holds those 70 and the YAML library. `golang.org/x/sync` and
      `golang.org/x/text` are in the root module's graph today only through pgx in the tests
      of `jobs-river` (`go mod why -m`), and reach a shipped binary only through
      sigstore-go (`go list -deps` on the program
      above: `go-openapi/runtime` imports `x/sync/errgroup`, and `go-openapi/strfmt` and
      `x/net/idna` import `x/text`):
      - Apache-2.0: `github.com/cyberphone/json-canonicalization@v0.0.0-20241213102144-19d51d7fe467`,
        `github.com/go-logr/logr@v1.4.4`, `github.com/go-logr/stdr@v1.2.2`,
        `github.com/go-openapi/analysis@v0.25.5`, `github.com/go-openapi/errors@v0.22.8`,
        `github.com/go-openapi/jsonpointer@v1.0.0`, `github.com/go-openapi/jsonreference@v1.0.0`,
        `github.com/go-openapi/loads@v0.25.0`, `github.com/go-openapi/runtime@v0.33.0`,
        `github.com/go-openapi/runtime/server-middleware@v0.30.0`,
        `github.com/go-openapi/spec@v0.22.9`, `github.com/go-openapi/strfmt@v0.27.0`,
        `github.com/go-openapi/swag@v0.26.1`, `github.com/go-openapi/swag/cmdutils@v0.27.0`,
        `github.com/go-openapi/swag/conv@v0.27.3`, `github.com/go-openapi/swag/fileutils@v0.27.3`,
        `github.com/go-openapi/swag/jsonname@v0.26.1`, `github.com/go-openapi/swag/jsonutils@v0.27.3`,
        `github.com/go-openapi/swag/loading@v0.27.3`, `github.com/go-openapi/swag/mangling@v0.27.3`,
        `github.com/go-openapi/swag/netutils@v0.27.0`, `github.com/go-openapi/swag/pools@v0.27.3`,
        `github.com/go-openapi/swag/stringutils@v0.27.3`, `github.com/go-openapi/swag/typeutils@v0.27.3`,
        `github.com/go-openapi/swag/yamlutils@v0.27.3`, `github.com/go-openapi/validate@v0.26.1`,
        `github.com/google/certificate-transparency-go@v1.3.3`,
        `github.com/google/go-containerregistry@v0.21.7`, `github.com/in-toto/attestation@v1.2.0`,
        `github.com/in-toto/in-toto-golang@v0.11.0`, `github.com/oklog/ulid/v2@v2.1.1`,
        `github.com/opencontainers/go-digest@v1.0.0`, `github.com/shibumi/go-pathspec@v1.3.0`,
        `github.com/sigstore/protobuf-specs@v0.5.1`, `github.com/sigstore/rekor@v1.5.3`,
        `github.com/sigstore/rekor-tiles/v2@v2.3.0`, `github.com/sigstore/sigstore@v1.10.8`,
        `github.com/sigstore/sigstore-go@v1.3.0`, `github.com/sigstore/timestamp-authority/v2@v2.1.3`,
        `github.com/theupdateframework/go-tuf/v2@v2.4.2`, `github.com/transparency-dev/formats@v0.1.1`,
        `github.com/transparency-dev/merkle@v0.0.2`, `go.opentelemetry.io/auto/sdk@v1.2.1`,
        `go.opentelemetry.io/otel@v1.44.0`, `go.opentelemetry.io/otel/metric@v1.44.0`,
        `go.opentelemetry.io/otel/trace@v1.44.0`,
        `google.golang.org/genproto/googleapis/api@v0.0.0-20260526163538-3dc84a4a5aaa`,
        `google.golang.org/genproto/googleapis/rpc@v0.0.0-20260523011958-0a33c5d7ca68`,
        `google.golang.org/grpc@v1.82.1`, `k8s.io/klog/v2@v2.140.0` (50). The three
        OpenTelemetry modules hold a BSD-3-Clause text after Apache-2.0 in the same file,
        and `go-pathspec` holds a second file, `GO-LICENSE` (BSD-3-Clause, "Copyright (c)
        2012 The Go Authors"); the four are recorded as `Apache-2.0 AND BSD-3-Clause`.
      - BSD-3-Clause: `github.com/google/uuid@v1.6.0`,
        `github.com/grpc-ecosystem/grpc-gateway/v2@v2.29.0`, `golang.org/x/crypto@v0.54.0`,
        `golang.org/x/mod@v0.42.0`, `golang.org/x/net@v0.57.0`, `golang.org/x/sync@v0.24.0`,
        `golang.org/x/sys@v0.47.0`, `golang.org/x/term@v0.45.0`, `golang.org/x/text@v0.43.0`,
        `google.golang.org/protobuf@v1.36.11` (10).
      - BSD-2-Clause: `github.com/digitorus/timestamp@v0.0.0-20231217203849-220c5c2851b7`,
        `github.com/pkg/errors@v0.9.1` (2).
      - MIT: `github.com/asaskevich/govalidator@v0.0.0-20230301143203-a9d515a09cc2`,
        `github.com/blang/semver@v3.5.1+incompatible`, `github.com/cenkalti/backoff/v5@v5.0.3`,
        `github.com/cespare/xxhash/v2@v2.3.0`,
        `github.com/digitorus/pkcs7@v0.0.0-20230818184609-3a137a874352`,
        `github.com/go-viper/mapstructure/v2@v2.5.0`,
        `github.com/secure-systems-lab/go-securesystemslib@v0.11.0`,
        `github.com/youmark/pkcs8@v0.0.0-20240726163527-a2c0da244d78` (8).
    - `pkg/sign`, if question Q2 imports it, adds 11 more: Apache-2.0
      `github.com/coreos/go-oidc/v3@v3.17.0`, `github.com/go-jose/go-jose/v4@v4.1.4`,
      `github.com/sassoftware/relic@v7.2.1+incompatible`, `github.com/spf13/cobra@v1.10.2`;
      BSD-3-Clause `github.com/spf13/pflag@v1.0.10`,
      `github.com/theupdateframework/go-tuf@v0.7.0`, `golang.org/x/oauth2@v0.36.0`;
      BSD-2-Clause `github.com/pkg/browser@v0.0.0-20240102092130-5ac0b6a4141c`; MIT
      `github.com/jedisct1/go-minisign@v0.0.0-20211028175153-1c139d1cc84b`; and MPL-2.0
      `github.com/hashicorp/go-cleanhttp@v0.5.2` and
      `github.com/hashicorp/go-retryablehttp@v0.7.8`, which L8 then covers (`08:1337`).
      go-jose's `json/` directory carries its own BSD-3-Clause file.
    - Every licence above is on the in-process allow-list, MPL-2.0 with the condition of L8
      (`08:1336` L7). Conditions for
      `THIRD_PARTY_LICENSES/` and the NOTICE texts of every artifact that links them,
      `aiopsctl` and the server image both, since they share one module table (`08:359`):
      the NOTICE files of `go-openapi/jsonpointer`, `go-openapi/jsonreference`,
      `go-openapi/runtime`, `go-tuf/v2` and `grpc` (`NOTICE.txt`), and of `go-oidc/v3` with
      `pkg/sign`, are aggregated (Apache-2.0 §4(d)); the PATENTS files of `x/crypto`,
      `x/mod`, `x/net`, `x/sync`, `x/sys`, `x/term`, `x/text` and `protobuf` are copied;
      the licence files of `json-canonicalization`, `in-toto/attestation` and
      `in-toto-golang` hold only the Apache-2.0 notice, so the full text is added; the
      licence files of the three OpenTelemetry modules are copied whole, and
      `go-pathspec`'s `GO-LICENSE` beside its `LICENSE`; and
      `go-openapi/runtime/middleware/denco` carries its own MIT file. `go-digest`'s
      `LICENSE.docs`, CC-BY-SA-4.0, sits in the directory of a linked package, and its
      `README.md:91-93` limits it to `README.md` and `CONTRIBUTING.md`, which no binary
      links; L7 denies CC-BY-SA until it is recorded by name, and how it is recorded is
      reading 4 of question Q12.
    - The module graph, that is the build list of the root module with sigstore-go
      v1.3.0 added, holds 382 modules: the repository's three SDK modules and 379 upstream
      modules. `sdk/kernel` holds an Apache-2.0 `LICENSE`; `sdk/def` and `sdk/testkit` hold
      no licence file at their root, and inside the repository the root `LICENSE` covers
      them, but the copies that ADR-060 decision 21 serves hold no licence file unless the
      release step adds one. By the main licence file at the root of each upstream module
      (`LICENSE`, `LICENSE.txt`, `LICENSE.md`, `LICENCE`, `COPYING` or `License`): 183
      Apache-2.0, 106 MIT, 44 BSD-3-Clause, 19 MPL-2.0, 11 BSD-2-Clause and 2 ISC, and 14
      files that hold more than one text: 10 Apache-2.0 and BSD-3-Clause (nine
      OpenTelemetry modules and `sigs.k8s.io/json`), 2 Apache-2.0 and MIT (the two YAML v3
      modules) and 2 Apache-2.0, MIT and BSD-3-Clause (`klauspost/compress`,
      `sigs.k8s.io/yaml`). `DATA-DOG/go-sqlmock` and `ulikunitz/xz` count as BSD-3-Clause
      by their no-endorsement clause. Five modules hold a second licence file: `go-pathspec`
      (`GO-LICENSE`, BSD-3-Clause), `go-digest` (`LICENSE.docs`, CC-BY-SA-4.0),
      `bgentry/speakeasy` (`LICENSE_WINDOWS`, Apache-2.0) and the two YAML v2 modules
      (`LICENSE.libyaml`, MIT). Every upstream module has a licence file, and none holds a
      GPL, LGPL, AGPL, BUSL, SSPL, Elastic, PolyForm or Commons Clause text beyond the
      names the MPL-2.0 text itself cites. The MPL-2.0 modules outside the package graph
      include `github.com/hashicorp/vault/api@v1.22.0`, whose own licence file is MPL-2.0,
      and `github.com/letsencrypt/boulder@v0.20260309.0`; the five River modules are the
      root module's own (L8). A module outside the package graph is in no binary, so the
      allow-list of L7 does not reach it.
    - cosign, a CI tool that enters no artifact (ADR-060 decision 34; `08:1380`), read at
      `github.com/sigstore/cosign/v3@v3.1.4`: its `LICENSE` is Apache-2.0, and its `go.mod`
      requires ten HashiCorp modules (`go.mod:190-199`), `errwrap`, `go-cleanhttp`,
      `go-multierror`, `go-retryablehttp`, `go-rootcerts`, `go-secure-stdlib/parseutil`,
      `go-secure-stdlib/strutil`, `go-sockaddr`, `hcl` and `vault/api` at v1.23.0. Each is
      MPL-2.0 by its own licence file at that pin
      (`github.com/hashicorp/vault/api@v1.23.0/LICENSE`; the other nine at the versions of
      the build list above). Not read: the licence of the repository that holds
      `vault/api`, which licence:49 calls BUSL-1.1, and which of these modules the cosign
      binary links. No artifact carries cosign, so L7 and L8 do not reach its graph. Its
      register entry records these reads with the version and the date, and the edit of
      licence:49 adds the pin and these reads and replaces "scanned before S2a ends" with
      this decision.
    - Not read: the graph of v1.4.1 beyond its `go.mod` (question Q1), and any
      vulnerability database for these pins. The register entry of sigstore-go (ADR-060
      decision 34; `08:1332` L3) records the reads above with the classification rule, and
      the licence scan of ADR-060 decision 36 repeats them on the built binaries.

### What waits

17. Not built at S2a:

    | Part | Step | Why |
    |---|---|---|
    | The index's detached signature, a blob bundle of decision 2 over the composed document, and `indexes` in `trust.yaml` | S6a | `07:1899`; ADR-060 decision 26 |
    | The release key as a signer of `contrib/*` data packs that release CI publishes | S6a | `07:1583`; decision 6 |
    | Keyless signing | Question Q4 | Decision 3 |
    | `requireAttestations` | Question Q8 | ADR-060 decision 26 |
    | `trust.yaml` and the records in platform tables, for hosted | S4b | `07:1840` |
    | The audit record of each generation's digest and signer set | S5 | ADR-060 decision 29 |

### What this ADR asks of ADR-044 and ADR-047

18. ADR-060 decision 33 lists what each of these ADRs settles; the decisions above need
    the following of them, and each is proposed with that ADR.
    - ADR-044, the builder:
      - the release build in two phases: the builder writes the distribution manifest and
        stops, the release pipeline signs it outside the builder, and the builder links
        the release binaries with the manifest, its bundle and the release public key
        embedded (decision 13); `dist build` for a site takes no release key and links in
        one phase;
      - a main module the builder generates in its scratch directory, holding the module
        table and no plugin, with no `replace` line in its `go.mod`, and every module of
        the table, the root module included, reached at a version, so that build
        information records a sum for each (decision 12);
      - with question Q9 (a), the one exemption of its `init` deny-list; with question
        Q11 (a), the site's `aiopsctl` as an output of `dist build`; and a licence scan
        that honours the record of question Q12, reading 4.
    - ADR-047, the `$AIOPS_HOME` contract: `verified/`, with its records and `current`
      (decisions 10 and 12), among the paths it fixes, and `trust.yaml` and `verified/`
      mounted read-only into a kernel that runs as a container (decision 11; question Q5).

## Consequences

- `aiopsctl` and the server image link sigstore-go and the 70 modules of decision 16 from
  S2a, the gRPC runtime among them, since both binaries carry the operations plugin
  (`08:359`; ADR-060 decisions 41 and 53).
- `boot` gains readers of `trust.yaml`, of the records and `current`, and of the binary's
  build information, and no signature code; its provisional cap is ADR-060 decision 35's.
- No signature of the project reaches a public log, so nothing is disclosed, and no
  signature carries a time: revocation is deleting a key from `trust.yaml`, and a leaked
  key signs admissible artifacts until then (decision 15).
- A record is exactly as strong as `trust.yaml` (decision 11).
- A site image used before boots again only after `dist use` verifies it again, and an
  in-process row whose artifact pins another version of its module than the binary runs
  is denied (decision 12).
- Every binary that boots rows needs a trusted manifest first, the `aiopsctl` that the
  repository builds for `make check` and CI included; each gets it from a throwaway key,
  never from an exemption (decision 13).
- cosign v3.1.4 and `aiopsctl` write the same signatures, rows 1 and 3 of decision 2, so
  the release pipeline signs with cosign and a site or an author with `aiopsctl`, and
  either verifies the other's. Only `aiopsctl` writes the attestation (Context item 2).
- Removing the default root untrusts the release binaries' own manifests, so the release
  `aiopsctl` then refuses its own rows; which `aiopsctl` a site runs after that is
  question Q11, and boot prints the recovery in either case (decision 13).
- `plugin gate --against` reads an attestation it does not verify, as decision 17 of
  ADR-060 has it evaluate check 10 without `trust.yaml`; its verdict is about
  compatibility, not trust.

## Edits on acceptance

The commit that records the owner's answers edits, besides the ADR table of `08` §3:
`00:757`; `01:280` and `:381` (keyless not offered at S2a, question Q4); `02:204` (a
Sigstore bundle in key mode, decision 2); `07:75` (R17), `:1512`, `:1839`, `:1840` and
community:206 (the `signed` list of decision 5), `07:1841` (the record and the lock entry
of in-process artifacts, decision 10), `:1846`, `:1852`, `:1854`, `:1855` (install writes
the lock entry of in-process code too, decision 10) and `:1862` (`dist use` writes
`current`, decision 12); `08:404` and `:935`; `08:1337` L8 and `:1342` L13 (the licences
read); community:203 and :458; licence:49 (cosign's pin and reads, and the reads of
decision 16) and :258; and the plugin record in §11 ("keyless where it works") and §18.
Answers Q3, Q6, Q9, Q10, Q11 and Q12 also edit the lines their options name.

## Questions for the owner

Each question gives its options, the recommended one, why, and when the answer is needed.

**Q1. Which sigstore-go version is pinned?** The latest, v1.4.1, and v1.4.0 declare
`go 1.27.0`; the repository builds with go1.26.8 under `GOTOOLCHAIN=local` (ADR-060
decision 21). v1.3.0 declares `go 1.25.8`, and cosign v3.1.4 requires it (Context item 1).
- (a) v1.3.0.
- (b) v1.4.1, with the toolchain line of every module, the builder image and the exit's Go
  environment moved to go 1.27. Its `go.mod` moves pins that decision 16 lists, among them
  rekor v1.5.3 to v1.6.1, rekor-tiles/v2 v2.3.0 to v2.4.2, sigstore v1.10.8 to v1.11.4,
  protobuf-specs v0.5.1 to a v0.5.3 pseudo-version, timestamp-authority/v2 v2.1.3 to
  v2.2.0, certificate-transparency-go v1.3.3 to v1.4.0, x/crypto v0.54.0 to v0.59.0,
  protobuf v1.36.11 to v1.36.12, go-securesystemslib v0.11.0 to v0.11.1 and gRPC to
  v1.85.0 (`sigstore-go@v1.4.1/go.mod`). Decision 16, Context item 6 and the `init` scan
  of question Q9 are then read again at the versions it selects; rekor-tiles/v2 v2.4.2
  was not read.
- Recommended: (a). It builds on the repository's toolchain, cosign v3.1.4 requires the
  same version (decision 1), and every fact of decision 16 was read at it. Needed before
  sigstore-go merges.
- **Answered 2026-11-23: (a)**, decision 19.

**Q2. Does signing import sigstore-go's `pkg/sign`?** It adds 11 modules to `aiopsctl` and
the server image, two of them MPL-2.0, and code key mode never calls: the OIDC browser
flow, the Fulcio and Rekor clients and a command library (decision 16; Context item 7).
- (a) Import it: `sign.Bundle` with a key pair over the loaded key.
- (b) Build the bundle from sigstore-go's `bundle` types, the protobuf-specs messages,
  go-securesystemslib's `dsse.PAE`
  (`github.com/secure-systems-lab/go-securesystemslib@v0.11.0/dsse/envelope.go:46`) and
  sigstore's `signature` package; none of these adds a module to what verification links.
  The signing function verifies every bundle it builds with sigstore-go against the public
  key before it writes it, and the CI case of ADR-060 decision 36 has cosign verify the
  same bytes.
- Recommended: (b). It keeps the two MPL-2.0 modules, the OIDC browser flow and the
  Fulcio client out of both binaries, signing in key mode is a few dozen lines on library
  primitives, and no verification is written by hand (`08:404`). Needed before `plugin
  sign` merges.
- **Answered 2026-11-23: (b)**, decision 20.

**Q3. How does an author with only `aiopsctl` get a key pair?** A pack author needs
`aiopsctl` alone (`07:51`), publishing needs a signing key (`07:1531`), and no command of
ADR-060 decision 23 or `00:137` makes one. The exit's pack author machine holds only
`aiopsctl` (ADR-060 decision 30, step P1).
- (a) A command of `aiopsctl`, named by the CLI spec (`00:137`), writes a key pair in the
  format of decision 4, so the pair works with cosign too. ADR-060 decision 23 and
  `00:137` gain it.
- (b) The author makes the pair with cosign or another tool; `07:51` then holds only for an
  author who already has a key, and the exit places a key on the pack author's machine.
- (c) `plugin sign` without `--key` makes a pair per artifact and prints the public key;
  every artifact then has a new signer, which no `trust.yaml` entry can follow.
- Recommended: (a). It keeps `07:51` whole with one small command, and the key format is
  cosign's either way. Needed before the exit test is written.
- **Answered 2026-11-23: (a)**, decision 21.

**Q4. Is keyless signing offered at S2a?** Plugin record §11 says "keyless where it works";
`08:404`, `07:1852` and `01:280` call keyless verification at an air-gapped site
unconfirmed, `01:381` states that point as a subject of this ADR, and the terms of the
public instances are UNVERIFIED (licence:49). In keyless mode the bundle carries a
certificate (decision 3), and cosign uploads an entry to the public log
`rekor.sigstore.dev` by default (Context item 3); what the public instances record and
publish of it was not read and is UNVERIFIED. An upload outside the job is ruled out by
L14 and ADR-057 decision 5 (`08:1343`), and the exit's networks reach nothing outside the
job.
- (a) Not at S2a: key mode only, and keyless is taken up when a public release is planned
  after L14 clears; decision 3 keeps it additive. Whether keyless verification works at
  an air-gapped site stays unconfirmed until then, and `01:280`, `01:381`, `07:1852` and
  `08:404` say that keyless is not offered at S2a.
- (b) Keyless against a private instance that runs inside the exit job: an OIDC issuer, a
  certificate authority and a log, trusted through a root file in `trust.yaml`.
- (c) Keyless against the public instances. Listed only to be refused under L14.
- Recommended: (a). Nothing at S2a can use keyless signing without the public instances,
  whose terms are unread and to which L14 forbids an upload, or a private instance that no
  site has, and (a) changes no format. Needed before `plugin sign` merges.
- **Answered 2026-11-23: (a)**, decision 22.

**Q5. What protects the records of install and `dist use` until boot?** Boot verifies no
signature and imports neither cosign nor sigstore-go (`08:404`), and the records sit in
`$AIOPS_HOME` beside `trust.yaml`, which the same user writes (`07:1840`, `:1841`).
- (a) As decision 11: written only by `aiopsctl` under the write lock, named by digest,
  holding the verified bundles, and read against the current `trust.yaml` at every boot;
  and a requirement on ADR-047, which ADR-060 decision 33 leaves to decide read-only or
  read-write, that its table mount `trust.yaml` and `verified/` read-only into a kernel
  that runs as a container (decision 18). A process running as the site's user is not
  stopped, as it is not stopped from editing `trust.yaml` (`08:508`).
- (b) Boot checks each record's signature with the standard library's `crypto/ecdsa` over
  the DSSE encoding. It edits "no signature verification is written by hand" and the
  verification home of `08:404`, and adds nothing against a writer of `trust.yaml`.
- (c) `aiopsctl` seals each record with a keyed hash under a site secret that boot reads.
  The same user reads the secret, so it adds nothing over (a), and it is a new credential
  class (`00:379`).
- Recommended: (a). It keeps `08:404` whole, and (b) and (c) protect nothing that
  `trust.yaml` itself does not leave open. Needed before the record writer of install or
  `dist use` merges.
- **Answered 2026-11-23: (a)**, decision 23.

**Q6. Is the attestation an OCI referrer or a layer of the artifact?** ADR-060 decision 18
leaves the choice here. `08:391`, `07:1838` and ADR-060 decision 19 describe one OCI
artifact that holds the signed attestation.
- (a) A referrer, beside the signature referrer of decision 2, with the artifact's
  manifest digest as subject, where `cosign attest` places its own. `08:391`, `07:1838` and
  ADR-060 decision 19 are edited to read "one OCI artifact with its signature and
  attestation as referrers". The exit's registry (ADR-060 decision 34) is read for the
  OCI 1.1 referrers API or the tag fallback of the distribution spec.
- (b) A layer of the artifact, whose subjects are the digests of the artifact's other
  layers. The artifact's digest then covers the attestation, check 10 compares a set of
  digests, the signature stays a referrer (it signs the manifest digest), and `cosign
  verify-attestation` cannot find the statement.
- (c) Both, the layer and the referrer.
- Recommended: (a). One digest binds the statement and the code, the gate's comparison is
  one digest, and `plugin sign` works after `plugin pack` without changing the digest.
  Needed before `plugin pack` merges.
- **Answered 2026-11-23: (a)**, decision 24.

**Q7. What is the predicate type URI?** A type URI is a frozen name that SHOULD carry a
version and MAY be unresolvable (Context item 8); the project has no public domain, and
nothing is published before L14 (`08:1343`).
- (a) `https://github.com/mutex-solutions/aiops-platform/attestation/conformance/v1`, under
  the module prefix ADR-057 approved, which every import path already carries.
- (b) A URI under a domain the owner names for the project.
- (c) A URN such as `urn:aiops:attestation:conformance:v1`, whose namespace nobody holds.
- Recommended: (a). It adds no name the project does not already carry, and it needs no
  resolver. Needed before `plugin pack` merges; it can change without a migration until
  the first artifact is published.
- **Answered 2026-11-23: (a)**, decision 25.

**Q8. Is `requireAttestations` per signer built at S2a?** `08:404` lets a signer or an index
require named attestations, and ADR-060 decision 26 puts the per-index field at S6a and
the per-signer field here. The seam serves review and curation later (`08:949`). At S2a
the only attestation type is the conformance attestation, which check 10 already requires
of every row.
- (a) At S2a: a signer entry gains `requireAttestations: [<type URI>]`, and check 10 denies
  an artifact of that signer that lacks a named attestation.
- (b) At S6a with the per-index field, when a second type can exist; the `trust.yaml`
  reader refuses the field until then.
- Recommended: (b). At S2a the field could name only a type every row must carry anyway,
  and both forms then land with their first use. Needed before the `trust.yaml` reader
  merges.
- **Answered 2026-11-23: (b)**, decision 26.

**Q9. How does sigstore-go meet the builder's `init` deny-list?** Through gRPC it links an
`init` that registers two default-mux handlers and an `init` that registers a codec
(Context item 6), both of which the deny-list refuses (`07:1860`; `08:402`). The builder
writes `aiopsctl` and the release image (ADR-060 decision 21), and both carry the plugin
that links sigstore-go (`08:359`). Every generated protobuf package also registers its
types in the protobuf runtime's registry at `init`, as
`github.com/sigstore/protobuf-specs@v0.5.1/gen/pb-go/bundle/v1/sigstore_bundle.pb.go:454`
does, which the deny-list as written does not name. Its content is ADR-044's (ADR-060
decision 33).
- (a) The builder builds with the tag `grpcnotrace`, which drops `golang.org/x/net/trace`
  and its handlers, and ADR-044's deny-list exempts by package and pinned version the
  codec registration of `google.golang.org/grpc/encoding/proto`, which fills only gRPC's
  own codec table; the register entry records the exemption and it is read again at
  every version bump. This relaxes a written requirement: `08:402` and `07:1860` refuse
  any transitive `init` that registers a global codec, and both lines, with the deny-list
  content that ADR-060 decision 33 gives ADR-044, gain the exemption by package and
  version.
- (b) A `replace` of rekor-tiles/v2 by a copy without its gRPC files, maintained by the
  project and applied to every site's build.
- (c) Verification with sigstore's `signature` package and go-securesystemslib's DSSE
  verifier, without sigstore-go. It departs from "links sigstore-go" (`08:404`;
  `00:757`) and writes the bundle checks by hand.
- Recommended: (a). It keeps sigstore-go and keeps the deny-list whole for every other
  package: the default-mux handlers leave the graph, and the one exemption is gRPC's own
  default codec, registered by gRPC itself and by no plugin. It costs one
  build tag and one recorded exemption. Needed before sigstore-go or the deny-list
  merges, whichever is first.
- **Answered 2026-11-23: (a)**, decision 27.

**Q10. What does boot's module tie compare for a module with no recorded sum?** `07:1861`
and plugin record §11 state check 5 for a Go module as "path, version and sum equal an
entry" of a trusted manifest. The toolchain records no sum for the main module or for a
replaced module, and records the main module's version as `(devel)` or as one derived from
the checkout (Context item 9). Every build inside the repository is such a build: the S1
suites, `test/dist`, the `profile render` goldens and the `aiopsctl` of `make check` and
CI (decision 13), all of which pass the same gate (ADR-060 decision 28). A builder build
has no such module (decision 12).
- (a) Every build that boots rows goes through the builder: the S1 suites, the goldens and
  `make check` build their binaries from a module the test generates, against a module
  proxy inside the test that serves the working tree and the SDK modules at one
  pre-release version, as the exit serves the SDK modules (ADR-060 decision 40).
  `07:1861` holds whole; each
  such run packs the tree and builds through the proxy, and the S1 suites wait for the
  builder.
- (b) For the main module and a replaced module the manifest records the path, and for a
  replaced module its replacement as build information records it, with no version and
  no sum. Boot compares those, accepts a missing sum only where build information records
  none, and prints at every boot each module it tied without a sum, as it prints
  `allowUnsigned` (`08:404`). Such a module's code is bound only by the digest of the
  embedded manifest and its signer. The builder never writes such an entry (decision 18),
  so every image a site runs is compared on path, version and sum. `07:1861` and plugin
  record §11 gain "and sum, where the toolchain records one".
- Recommended: (b). No build inside the repository can meet the written rule, since the
  toolchain records no sum there; (a) meets it at the cost of a module proxy in every
  test run and in `make check`, and (b) keeps it whole for every builder build and prints
  each place where it does not hold. This narrows `07:1861` for builds inside the
  repository, which is why it is asked. Needed before the module clause of gate check 5
  merges.
- **Answered 2026-11-23: (b)**, decision 28.

**Q11. What `aiopsctl` does a site run once it removes the default root?** A site removes
`release` in `trust.yaml` (ADR-060 decision 50). The release `aiopsctl` boots the rows of
its `cli` profile on its built-in record (decision 13), which is then untrusted, so it
refuses every command, `trust add` included. ADR-060's Q14 chose (b) so that `aiopsctl`
works without an exemption, and ADR-060 decision 33 lists no site-built `aiopsctl` among
the items of ADR-044.
- (a) `dist build` also writes the site's `aiopsctl` for the host's platform, the `cli`
  profile of the same module table (`08:359`), embedding the same manifest, so after `dist
  use` it boots on `current` (decision 12). ADR-044 gains that output (decision 18), and
  `08:402` and the `Builder` row of `07:1845` name it.
- (b) The lockout stands: boot prints the recovery of decision 13, and to run the release
  `aiopsctl` again the site restores `- key: release` by hand. No line is edited.
- Recommended: (a). `08:359` already makes `aiopsctl` the `cli` profile of the same
  module table, so a site whose in-process plugins contribute commands needs its own
  `aiopsctl` in any case, and (a) lets a site remove the default without losing the CLI.
  Needed before `dist build` merges.
- **Answered 2026-11-23: (a)**, decision 29.

**Q12. Five readings of written text.** Each reading below is what S2a would build, and
each edits the lines it names in the commit that records the answer.
1. **Key lifetime.** `00:411` gives plugin signer keys and the `trust.yaml` roots the
   lifetime "publisher-managed; set by ADR-020". Reading: no bundle carries a trusted time
   (decision 2), so no key expires at S2a; a key is valid while `trust.yaml` lists it, and
   its publisher rotates it as decision 15 says. Edits `00:411`.
2. **An unsigned attestation.** `08:493`, `08:895` and `07:1512` call the attestation
   signed, and check 10 runs on every row (ADR-060 decision 16), a row that
   `allowUnsigned` admits included. Reading: for an id that `allowUnsigned` lists, check
   10 reads the statement that `plugin pack` recorded, unsigned, and every other clause of
   check 10 applies (decision 8); without the reading `allowUnsigned` admits no row.
   Edits `08:493`, `08:895` and `07:1512`.
3. **The signed clause where no signer is known.** ADR-060 decision 17 has `dist build`,
   step 7 of `plugin check` and `plugin gate --against` evaluate check 7. None of them
   has a signer recorded for what it reads, and the last two have no `trust.yaml`
   (ADR-060 decision 17). Reading: there the signed clause passes a
   first-party row and reports "not evaluated", with that reason, for any other row that
   provides or injects a `signed` key or listens on a `signed` hook; install and boot
   evaluate it (decision 7). Edits no line; the S2a report records the reading beside
   ADR-060 decision 17.
4. **go-digest's documentation licence.** `github.com/opencontainers/go-digest@v1.0.0`
   holds `LICENSE.docs`, CC-BY-SA-4.0, in the directory of a linked package, and L7
   denies CC-BY-SA until it is recorded by name (`08:1336`). Its `README.md:91-93` limits
   that licence to `README.md` and `CONTRIBUTING.md`, which no binary links (decision 16).
   Reading: the licence record names CC-BY-SA-4.0 for those two files of go-digest only,
   and the per-directory scan of L16, whose content is ADR-044's (ADR-060 decision 33),
   honours that record by module, version and file (decision 18). Edits licence:49 and the
   register entry of go-digest.
5. **The built-in record beside `dist use`.** ADR-060 decision 31 denies every Go module
   when the embedded manifest "differs from the one `dist use` recorded", and decision 50
   gives the release binary "its own signed manifest as recorded with that key". After a
   site runs `dist use` of its own image, a release binary meets both. Reading: the
   built-in record is a record of its own, so a release binary boots while `release` is
   trusted, and the denial of decision 31 applies to every other binary whose manifest is
   not `current` (decision 12); a site that wants release binaries refused removes
   `release` (question Q11). Edits `07:1861` and `08:404`, which gain "or, for a binary
   with no built-in record, the distribution that `dist use` made current".
- (a) Accept the five readings.
- (b) Answer each item on its own.
- Recommended: (a). Each reading is one the code can build without dropping a written
  check. Without reading 2 `allowUnsigned` admits nothing; without reading 3 three entry
  points would deny for want of an input they do not have; and without reading 5 the
  release image refuses at every site that ran `dist use`, against `08:398`, and so does
  the test release's `aiopsctl` in the exit's step C2, which runs `plugin install` again
  after `dist use` (ADR-060 decision 30). Needed before the code each reading names
  merges.
- **Answered 2026-11-23: (a), all five readings**, decision 30.

## Owner answers of 2026-11-23

The project owner accepted this ADR on 2026-11-23 and answered every question of the
section "Questions for the owner" on the same date, each with its recommended option.
Each decision below names the question it answers. Where a decision above leaves a point
to a question, the decision here settles it.

19. **sigstore-go is pinned at v1.3.0** (Q1 (a)). It builds on the repository's
    toolchain, go1.26.8 under `GOTOOLCHAIN=local`, and cosign v3.1.4 requires the same
    version (decision 1). Decision 16 is the licence read at this pin.

20. **Signing does not import sigstore-go's `pkg/sign`** (Q2 (b)). The signing function
    builds the bundle from sigstore-go's `bundle` types, the protobuf-specs messages,
    go-securesystemslib's `dsse.PAE` and sigstore's `signature` package, none of which
    adds a module to what verification links. It verifies every bundle it builds with
    sigstore-go against the public key before it writes it, and the CI case of ADR-060
    decision 36 has cosign verify the same bytes. The 11 modules that `pkg/sign` adds
    (decision 16), the two MPL-2.0 modules among them, are linked by neither `aiopsctl`
    nor the server image.

21. **`aiopsctl` writes a key pair** (Q3 (a)). A command of `aiopsctl`, named by the CLI
    spec (`00:137`), writes a key pair in the format of decision 4, so the pair works with
    cosign too, and `07:51` holds whole. ADR-060 decision 23 and `00:137` gain it.

22. **Keyless signing is not offered at S2a** (Q4 (a)). `aiopsctl` signs and verifies in
    key mode only and refuses a bundle that holds a certificate, naming keyless as not
    offered (decision 3). Keyless is taken up when a public release is planned after L14
    clears, and decision 3 keeps it additive. Whether keyless verification works at an
    air-gapped site stays unconfirmed until then. `01:280`, `01:381`, `07:1852` and
    `08:404` say that keyless is not offered at S2a.

23. **The records of install and `dist use` are protected as decision 11 states** (Q5
    (a)): written only by `aiopsctl` under the profile write lock, named by the digest
    they vouch for, holding the verified bundles, and read against the current
    `trust.yaml` at every boot. ADR-047's table mounts `trust.yaml` and `verified/`
    read-only into a kernel that runs as a container (decision 18). A process running as
    the site's user is not stopped, as it is not stopped from editing `trust.yaml`
    (`08:508`). Boot verifies no signature (`08:404`).

24. **The attestation is an OCI referrer** (Q6 (a)), beside the signature referrer of
    decision 2, with the digest of the artifact's OCI manifest as its one subject, where
    `cosign attest` places its own. One digest binds the statement and the code, check 10
    compares one digest, and `plugin sign` works after `plugin pack` without changing the
    digest. `08:391`, `07:1838` and ADR-060 decision 19 read "one OCI artifact with its
    signature and attestation as referrers". The exit's registry (ADR-060 decision 34) is
    read for the OCI 1.1 referrers API or the tag fallback of the distribution spec.

25. **The predicate type URI is
    `https://github.com/mutex-solutions/aiops-platform/attestation/conformance/v1`** (Q7
    (a)), under the module prefix ADR-057 approved. It adds no name the project does not
    already carry and needs no resolver; it can change without a migration until the
    first artifact is published.

26. **`requireAttestations` per signer is built at S6a** (Q8 (b)), with the per-index
    field of ADR-060 decision 26, when a second attestation type can exist. The
    `trust.yaml` reader refuses the field until then.

27. **The builder builds with the tag `grpcnotrace` and exempts gRPC's own codec** (Q9
    (a)). The tag drops `golang.org/x/net/trace` and its default-mux handlers from the
    graph, and ADR-044's `init` deny-list exempts, by package and pinned version, the codec
    registration of `google.golang.org/grpc/encoding/proto`, which fills only gRPC's own
    codec table. The register entry records the exemption, and it is read again at every
    version bump. The deny-list stays whole for every other package. `08:402` and
    `07:1860`, with the deny-list content that ADR-060 decision 33 gives ADR-044, gain
    the exemption by package and version. Note of 2026-11-25: on the owner's word, the
    deny-list also exempts the three registrations with `encoding/gob.Register` that
    sigstore-go brings in through go-openapi (decision 16): the `init` of `encoding/gob`
    itself at the toolchain pin, and the `init` of `github.com/go-openapi/loads` v0.25.0
    and of `github.com/go-openapi/spec` v0.22.9. Each fills only gob's own type registry
    and is keyed by package and version in the reviewed exceptions table of ADR-044
    decision 21, whose note of the same date records them, and a version change needs a
    new review. The register entry of sigstore-go records them.

28. **Boot ties a module with no recorded sum by path** (Q10 (b)). For the main module
    and a replaced module the manifest records the path, and for a replaced module its
    replacement as build information records it, with no version and no sum. Boot
    compares those, accepts a missing sum only where build information records none, and
    prints at every boot each module it tied without a sum, as it prints `allowUnsigned`
    (`08:404`). Such a module's code is bound only by the digest of the embedded manifest
    and its signer. The builder never writes such an entry (decision 18), so every image
    a site runs is compared on path, version and sum. `07:1861` and plugin record §11
    gain "and sum, where the toolchain records one", which narrows `07:1861` for builds
    inside the repository.

29. **`dist build` also writes the site's `aiopsctl`** (Q11 (a)) for the host's platform,
    the `cli` profile of the same module table (`08:359`), embedding the same manifest,
    so after `dist use` it boots on `current` (decision 12) and a site can remove the
    default root without losing the CLI. ADR-044 gains that output (decision 18), and
    `08:402` and the `Builder` row of `07:1845` name it.

30. **The five readings of Q12 are accepted** (Q12 (a)):
    1. **Key lifetime.** No bundle carries a trusted time (decision 2), so no key expires
       at S2a; a key is valid while `trust.yaml` lists it, and its publisher rotates it as
       decision 15 says. It edits `00:411`.
    2. **An unsigned attestation.** For an id that `allowUnsigned` lists, check 10 reads
       the statement that `plugin pack` recorded, unsigned, and every other clause of
       check 10 applies (decision 8). It edits `08:493`, `08:895` and `07:1512`.
    3. **The signed clause where no signer is known.** At `dist build`, step 7 of `plugin
       check` and `plugin gate --against`, the signed clause of check 7 passes a
       first-party row and reports "not evaluated", with that reason, for any other row
       that provides or injects a `signed` key or listens on a `signed` hook; install and
       boot evaluate it (decision 7). It edits no line; the S2a report records the
       reading beside ADR-060 decision 17.
    4. **go-digest's documentation licence.** The licence record names CC-BY-SA-4.0 for
       `README.md` and `CONTRIBUTING.md` of `github.com/opencontainers/go-digest@v1.0.0`
       only, and the per-directory scan of L16, whose content is ADR-044's, honours that
       record by module, version and file (decision 18). It edits licence:49 and the
       register entry of go-digest.
    5. **The built-in record beside `dist use`.** The built-in record is a record of its
       own, so a release binary boots while `release` is trusted, and the denial of
       ADR-060 decision 31 applies to every other binary whose manifest is not `current`
       (decision 12); a site that wants release binaries refused removes `release`
       (decision 29). It edits `07:1861` and `08:404`, which gain "or, for a binary with
       no built-in record, the distribution that `dist use` made current".

**Edits made with this record.** `00` is edited in the commit that records the answer
(`08:996`), and that commit makes the edits that "Edits on acceptance" and the chosen
options name: `00:137`, `:411` and `:757`; `01:280` and `:381`; `02:204`; `07:75`,
`:1512`, `:1838`, `:1839`, `:1840`, `:1841`, `:1845`, `:1846`, `:1852`, `:1854`,
`:1860`, `:1861` and `:1862`; `08:391`, `:402`, `:404`, `:493`, `:895`, `:935`,
`:1337` (L8) and `:1342` (L13); community:203, :206 and :458; licence:49 and :258; the
plugin record in §11 and §18; and ADR-060 decisions 19, 23 and 33. The same commit keeps
the lines that restate them consistent: `00:139` (the site's `aiopsctl`); `02:203` (the
attestation as a referrer); `07:1543` and plugin record §2 row 12 (the attestation that
`plugin sign` signs, unsigned only for an id that `allowUnsigned` lists); and plugin
record §11, step 2 (the codec exemption and the site's `aiopsctl`). Left to later
commits: the ADR table of `08` §3, and ADR-054:234 and :511, which call this ADR open, to
the commit that lists the ADRs accepted on this date; the parts of ADR-044 and ADR-047
that decision 18 asks of them, which each records with its own answers; the register
entries of sigstore-go, cosign and go-digest, to the commit that writes
`tools/upstream-components.yaml` (ADR-060 decision 51); and reading 3, to the S2a report.
An option the owner did not choose edits nothing.

**In-process code has no entry in `plugins.lock`** (owner, 2026-11-23). As accepted,
decision 10 gave in-process code an entry in `plugins.lock` that pins nothing, and
decision 12 had boot find, through that entry, the record of a row with no origin
artifact, while ADR-047 decisions 8 and 18, accepted the same day, give that code no
entry. The owner was asked which holds and answered on 2026-11-23: in-process code gets
no entry in `plugins.lock`, as ADR-047 decisions 8 and 18, `07:1841` and `00:478` state.
Decisions 10 and 12 are edited to that answer: boot finds the record of an in-process
row by its plugin id in the records install wrote under `$AIOPS_HOME/verified/`, whose
`record.yaml` carries the plugin id, and not through the lock. `07:1841` and `:1855`, the
row `Lock` and the relationship from `Install` to `Lock` in `07` §9, say that in-process
code has no entry, and neither `07:1841` nor `:1855` gains the lock entry of in-process
code that "Edits on acceptance" names for them. `00:478`, `01:282` and `02:206` lock only a remote plugin or a pack with no code,
so they already agree and are not edited, and ADR-047 decisions 8 and 18 stand as
written.

## 2026-11-27 proposed addition: the S2b part, remote artifacts

- Status: **proposed 2026-11-27, awaiting the project owner.** This section is the S2b
  part of this ADR, the dated addition that ADR-063 decisions 2 and 34 ask for. It is
  proposed before the code it governs and is accepted before that code merges into
  `main`. It blocks the install and the `plugin verify` of remote code, the attestation
  that `plugin pack` writes for remote code, and the trust cases of ADR-063 decision 28.
  Decisions 1 to 30 above stand as written, except where an answer below adds a dated
  note to one; "Edits on acceptance" names each such decision. The decisions of this
  section are numbered from 31 and its questions from Q13, and each question is answered
  before the code it names merges. Decisions 31, 32, 34 and 40 rest in part on S2b parts
  of other ADRs that are not accepted yet, and no code of the remote path of decision 34
  exists; "What this part rests on, and what is not proven" lists both.
- Scope: the five points that ADR-063 decision 34 gives this part: the record of a
  remote artifact, which has no `module` (decision 10); whether the attestation names
  the runtimes its suites ran in; `plugin verify` against a pulled image; the `signed`
  clause for a remote listener on a hook with a raw field (decision 7); and whether
  `allowUnsigned` (decision 8) admits an unsigned artifact of the runtime `remote`. Two
  points that the review of this part found open are added: what a withdrawn trust does
  to a unit that runs, with what `plugin start` evaluates (decision 32; question Q18),
  and which key signs a remote artifact under a reserved owner (decisions 31 and 39).
- Licences: no licence was read for this part. It adds no signature format and no key
  format. `plugin verify` of remote code links no module that `go.mod` does not pin
  (decision 34): it fetches the image with packages of `oras.land/oras-go/v2` v2.7.0
  (`go.mod:24`), whose register entry records the reading of its licence
  (`tools/upstream-components.yaml:353-365`), and it runs the container runtime of the
  host as a process, as the builder runs its image (`internal/builder/image.go:417`).
  That runtime is the program whose build command is on the list "Not read, and each
  read before its first use" of ADR-063 decision 35 (ADR-063:897, :913-915). It is read
  at its pin, at a primary source, before the job that runs the cases of `plugin verify`
  uses it. The licence scan of `aiopsctl` runs again on the packages that the image
  fetch adds to its package graph (ADR-060 decision 36; ADR-063 decision 35). The
  readings of decision 16 were not made again for this part, and what that decision
  lists as not read stays unread. A module that the work finds it needs is read first,
  as ADR-063 decision 35 says.
- Citations: `00`, `01`, `07` and `08` are the files of `docs/architecture/` with those
  numbers, and `08:503` is line 503 of `08` at commit `bbfd839`. The decisions above
  cite an earlier commit: `07:443` there is `07:450` here, and `08:404` there is
  `08:406`. A path with a line, as `boot/trust.go:289`, is a file of the repository at
  `bbfd839`; `artifact.go`, `install.go` and `author.go` are files of
  `plugins/operations/internal/operations/`. The plugin record,
  `docs/design/plugin-system-2026-11.md`, is cited by section, "community" as in the
  header of this ADR, and every ADR by decision number.

### Context

What the documents fix for remote code, and this part keeps:

- **The artifact.** A remote plugin is published as one OCI artifact with its signature
  and attestation as referrers. It holds the image reference by digest, `plugin.yaml`, a
  bundle of exactly one disabled row at S2b and an SBOM (`08:393`; `07:1850`; ADR-063
  decisions 17 and 67). `plugin pack` builds the image, writes it beside the artifact as
  an OCI layout and records its digest, and `plugin push` pushes the image and then the
  artifact (ADR-063 decision 61).
- **Install** verifies the signature, the trust policy for this id and runtime, the gate
  and the attestation. It writes the lock entry by digest, the kept files, the verified
  record, the bundle and the unit, pulls no image and runs no third-party code
  (`08:398`; `07:1842`, `:1851`, `:1868`; `00:482`; ADR-063 decision 17).
- **Trust.** A signer entry covers an artifact when its id matches a pattern and every
  runtime its manifest lists is in the entry's `runtimes`, so a remote artifact needs an
  entry that lists `remote` (decision 6; `07:1866`). Remote code has no module, so under
  `aiops/` or `contrib/` check 5 denies it unless its verified signer is a key of the
  release pipeline that the binary carries: "the release key of ADR-060 decision 50, or
  a key that ADR-020 names for the release's artifacts" (`08:503`; ADR-048 decision 14).
  Decisions 6 and 17 name no such key for remote code: `trust add` refuses both owners,
  and the release key becomes a signer of artifacts at S6a, for `contrib/*` data packs.
- **The attestation.** Its one subject is the digest of the artifact's OCI manifest, and
  the artifact's own field pins the image (decisions 9 and 24; ADR-063 decision 17).
  `plugin pack` records what passed, and `plugin check` runs T1 and T2 in every runtime
  the manifest lists (`07:134`, `:1519`, `:1578`). Through the remote adapter the
  obligations half of a suite runs and the guard half never does, since a remote plugin
  provides no key (ADR-063 decision 14; `07:1115`). Only `aiopsctl` writes the statement
  (`07:1519`).
- **What ties a remote row to its code.** The kernel never launches a plugin process
  (`08:447`), so it never sees which image runs. The unit runs the image by the digest
  that the artifact pins and by no tag (ADR-063 decision 25). The instance proves a key
  pair that install made for that plugin id, and the kernel holds its pin (ADR-063
  decision 55). The first message carries the digest of the `plugin.yaml` that the
  plugin was built with, a drift check that authenticates nothing (ADR-063 decision 58).
  The row names the artifact's own id, and the code it runs in the server is the proxy,
  the entry `aiops/remote` of the module table (ADR-063 decision 41).
- **What stops a unit.** The kernel never launches or stops a plugin process (`08:447`,
  `:1293`). `plugin disable` and `plugin remove` revoke the credential of an instance by
  deleting its pin and stop the unit (ADR-063 decisions 23, 55 and 68). `quarantine`,
  which revokes the instances of one version, is S6b (`07:1894`). A denied row is
  detached in the launcher's copy, and no file is rewritten (`08:510`).
- **`plugin verify`** is "the explicit, optional re-run against a pulled image", is not
  part of install, and is a proof of conformance at S2b with cases of its own
  (`07:1519`; plugin record §11; `08:940`; ADR-063 decisions 17 and 28). A local-path
  spec names an OCI layout, which `verify` reads as it reads a registry (ADR-047
  decision 1).
- **Remote listeners** are allowed only on the hooks marked remote, under a deadline,
  and receive the scrubbed form of the payload; "a field the definition marks raw forces
  the `signed` class" (`07:450`, `:1116`; plugin record §8). A hook in a signer's
  `signed` list counts for every runtime the entry covers (decision 7). S2b builds the
  remote listener path on a test hook marked remote, and the scrubbed payload arrives at
  S5 with `pii` (ADR-063 decisions 26 and 51; `00:74`; `07:1503`).
- **`allowUnsigned`** lists exact ids, is empty in hosted and is printed at every boot
  elsewhere, and on a self-host only the ids the site lists run unsigned (decision 8;
  `08:406`; `01:303`; `07:1884`). `plugin install` "takes a signed OCI artifact"
  (`00:482`), and the S2b exit installs "from signed artifacts" (`08:697`).

What the code holds at `bbfd839`:

- `plugin pack` and `plugin install` refuse `remote` (`artifact.go:91`;
  `install.go:123`), and ADR-063 decision 17 lifts both refusals.
- The verification that install and `plugin verify` share fetches the signatures first
  and tries them against the keys of `signers` in `trust.yaml` and against no other key
  (`artifact.go:638-645`). Once one verifies, or where `allowUnsigned` lists an id, it
  fetches the image manifest and `plugin.yaml`, which names the id and the runtimes that
  the covering signer is held to, and then the attestation, whose subject is held to
  `<id>@<version>` (`artifact.go:613-617`, `:646-664`, `:679`, `:838`). It refuses every
  artifact with no module under a reserved owner, whatever its signature, and records
  `unsigned` for an id that `allowUnsigned` lists, whatever its runtime
  (`artifact.go:668-675`).
- The reader of `record.yaml` takes `runtimes: [remote]` and refuses a record that
  states `module` unless `runtimes` holds `inproc` (`boot/trust.go:236-258`,
  `:289-290`). The writer adds `module` only for in-process code and records the
  signer's key id or `unsigned` (`install.go:367-370`).
- Check 5 holds the record's id, version and runtimes to the kept manifest and the
  signer to `trust.yaml` for those runtimes (`boot/checks.go:315-325`). Its
  reserved-owner clause passes a record whose signer is the release key that the binary
  carries (`boot/checks.go:319`; `boot/trusted.go:175`), and no command writes such a
  record, since install refuses every such artifact. The reader of `trust.yaml` refuses
  a pattern of either owner (`boot/trust.go:227-230`). Check 5 evaluates the module
  clause only on a subject with no artifact, and a row is resolved in the module table
  alone (`boot/checks.go:300-310`; `boot/gate.go:565-575`).
- The `signed` clause of check 7 passes a row whose plugin id is under `aiops/`, and
  counts a hook of a signer's `signed` list for every runtime and a key for `inproc`
  only (`boot/checks.go:363-393`). Check 10 compares the module that an artifact pins
  only where the record holds one (`boot/checks.go:510`).
- Boot, every live generation and `profile render` read `trust.yaml` anew
  (`boot/trusted.go:137-148`; `boot/admit.go:28-45`).
- The suites step of `plugin verify` reads every artifact that lists a runtime as
  in-process code and fetches its module layer (`artifact.go:694-717`). Step 5 of
  `plugin check` passes the suites the directory `fixtures/` of the plugin
  (`author.go:402-403`).
- The statement names definitions, a scenario and the tests that did not run, and no
  runtime (`boot/trust.go:34-62`). Two writers fill it: step 5 of `plugin check`
  (`author.go:402-411`) and the builder (`internal/builder/manifest.go:181`).
- A hook of `definition.yaml` and of `serves` carries its stages, its access class and a
  remote mark of `yes` or `no`, and nothing marks a field raw
  (`tools/deflint/main.go:395-396`; `boot/distribution.go:306-308`; ADR-060 decision 7).

### Decision

31. **A remote artifact is signed and trusted as every artifact is.** `plugin sign`
    signs the digest of the artifact's OCI manifest in the one format of decision 2, and
    signs the attestation, both as referrers (`07:1850`). No format, key form or field
    of `trust.yaml` is added. An entry of `signers` covers a remote artifact when its id
    matches a pattern and its `runtimes` holds `remote` (decision 6): `aiopsctl trust
    add <key> --ids <pattern> --runtimes remote` (`08:398`; `07:1866`). An entry with no
    runtimes covers packs only, and one that lists `inproc` alone does not cover remote
    code.
    - **Reserved owners.** Remote code has no module, so the reserved-owner clause of
      check 5 applies to it as written (`08:503`; ADR-048 decision 14;
      `boot/checks.go:319`). This part names no key "for the release's artifacts" beside
      the release key, and at S2b no key signs a remote artifact under `aiops/` or
      `contrib/` for install: a signature is tried against the keys of `signers` alone,
      and `trust add` and the reader of `trust.yaml` refuse both owners (decision 6;
      `artifact.go:638-645`; `boot/trust.go:227-230`). `plugin install` and `plugin
      verify` therefore refuse every remote artifact under a reserved owner with check
      5, whatever its signature, as the shared verification does today
      (`artifact.go:668-671`), and no pattern of `trust.yaml` admits one. Boot's clause
      passes a record of such an artifact whose signer is the release key that the
      binary carries, and no command of S2b writes that record. The verification of such
      an artifact with the release key is not built at S2b (decision 39), and reading 5
      of question Q15 asks the owner to confirm it.
    - **The image.** The signature covers the image through the artifact: the image
      reference by digest is a layer of the manifest whose digest is signed (decision 9;
      ADR-063 decisions 17 and 25). That layer is the S2b part of ADR-047 (decision 40).
      Whether the image also carries a signature of its own is reading 1 of question
      Q15.
32. **The record of a remote artifact** is the record of decision 10 without `module`.
    `plugin install` writes it, and no other command does: under the profile write lock,
    after every clause that install evaluates has passed, into
    `$AIOPS_HOME/verified/sha256-<hex>/`, named by the digest of the artifact's OCI
    manifest, the digest that `plugins.lock` names (decision 10; `07:1851`, `:1853`). It
    holds `record.yaml` with `kind: artifact`, that digest, the signer's key id, or
    `unsigned` with question Q17 (a), the plugin id, the version and `runtimes:
    [remote]`; `signature.sigstore.json` and `attestation.sigstore.json` as pulled; and
    `attestation.json`, the statement that was verified. It states no `module`, and the
    reader refuses a remote record that states one (`boot/trust.go:289-290`). Whether it
    also states the image digest is question Q13.
    - The test harness of ADR-060 decision 28, `test/trusted`, writes through the same
      code the record, the lock entry, the kept files and the `trust.yaml` entry of a
      remote row that no install made, as decision 10 has it write records: the T3 boot
      of each remote example, whose instance is the test's own process, and the two
      stubs of the matrix (ADR-063 decisions 31 and 64). It signs with a throwaway key,
      its `trust.yaml` entry lists `remote`, and no check is narrowed for such a row.
    - Boot finds the record of a remote row through the digest of its lock entry, not by
      plugin id as for in-process code, which has no entry (decision 10; `07:1853`;
      ADR-047 decision 8).
    - At every composition check 5 holds the kept files to the locked digest, the record
      to the kept manifest by id, version and runtimes, and the recorded signer to the
      present `trust.yaml` for the id and the runtime `remote` (`08:503`; ADR-047
      decision 18; `boot/checks.go:315-325`). A signer entry that loses `remote`, or is
      deleted, denies the row at the next composition, a boot or a live generation, with
      no record touched (decision 10; `boot/trusted.go:137-148`).
    - **What a denial does, and what it does not.** A check 5 or check 10 denial of a
      remote row detaches the row: its proxy is not mounted, so no dial is made and no
      contribution of the plugin is served (`08:510`; ADR-063 decision 16). It stops no
      process. The kernel never launches or stops one (`08:447`, `:1293`), boot writes
      no file (decision 10), and the pin of the instance stays in the home until `plugin
      disable` or `plugin remove` deletes it (ADR-063 decisions 55 and 68). After a
      signer entry is deleted or loses `remote`, an instance that runs keeps running its
      image inside the fence of its unit, with no route out and with nothing dialling it
      (ADR-063 decisions 24, 25 and 56), until the operator runs `plugin disable` or
      `plugin remove`. A denied in-process row runs no code; a denied remote row leaves
      a process, and this is the limit that matches decision 15 for keys. `quarantine`,
      which revokes the instances of a version, is S6b (decision 39). Whether `plugin
      start` starts the unit of an artifact that check 5 denies is question Q18.
    - **The proxy code.** A remote row also runs Go code in the server: the proxy, the
      entry `aiops/remote` of the module table, one plugin value per locked id (ADR-063
      decision 41). The row therefore has two subjects, as a row of installed in-process
      code has (decision 12): the artifact, under its own id, and the entry
      `aiops/remote`. Check 5 evaluates the module clause and the tie of decision 12 on
      that entry for every remote row. It denies each remote row when the entry does not
      tie to the trusted manifest, and every remote row with every other row when the
      embedded manifest is not trusted, so "every row" of decision 12 holds remote rows
      too. Check 10 reads the artifact's statement, from its record, for the row, and
      the statement of the entry `aiops/remote` in the trusted manifest for the proxy
      code. `profile render` does not evaluate the tie, as for any entry (`08:495`). At
      `bbfd839` the gate does neither for a row whose id is not in the module table
      (`boot/checks.go:300-310`; `boot/gate.go:565-575`), so S2b builds it with the
      resolution of ADR-063 decision 12. Reading 6 of question Q15 asks the owner to
      confirm it.
    - **The image.** The module tie of decision 12 has no counterpart for the image.
      Boot cannot hold the pinned image equal to the image that runs, because the kernel
      never launches a plugin process (`08:447`). The record vouches for the artifact,
      and the artifact pins the image. What makes an instance run that image is outside
      boot: the unit that install renders from the verified artifact, by digest and by
      no tag, and the key pair that install makes for that plugin id (ADR-063 decisions
      25 and 55). The digest of the first message is a drift check and authenticates
      nothing (ADR-063 decision 58). For its image a remote row is therefore as strong
      as the files of the home, as a record is (decision 11), and no check of the gate
      claims more.
    - The record of a manifest that lists `inproc` and `remote` follows the reader's
      rule, `module` exactly when `runtimes` holds `inproc`. Whether such a manifest
      packs and installs at all is the S2b part of ADR-048 (ADR-063 decision 34).
33. **The attestation of a remote artifact** is the statement of decision 9. Its one
    subject is `<id>@<version>` with the digest of the artifact's OCI manifest, and it
    is an OCI referrer of the artifact (decisions 9 and 24). The statement names neither
    the image digest nor `requires.remoteProtocol`: the subject digest covers the layer
    that pins the image and the `plugin.yaml` that check 3 reads (ADR-048 decision 16).
    - `definitions` names every definition of `requires` with its version and, for each
      registry the plugin contributes to, the suite revision and `halves:
      ["obligations"]`, the one half that runs through the adapter (ADR-060 decision 56;
      ADR-063 decision 14).
    - `plugin pack` records the statement only when step 5 passed in every runtime that
      the manifest lists: it records what passed, and step 5 fails on a failure in any
      runtime listed (`07:134`, `:1519`, `:1578`).
    - Check 10 evaluates every clause of `08:508` on a remote row as on any row. Its
      clause on a pinned module reads a record that holds one, and so reads nothing of
      the artifact here (`boot/checks.go:510`).
    - Whether the statement also names the runtimes in which the suites ran is question
      Q14.
34. **`plugin verify` of remote code** is the explicit, optional re-run against a pulled
    image (`07:1519`; plugin record §11). It is no part of install, and install still
    pulls no image and runs none (ADR-063 decision 17). No code of its remote path
    exists at `bbfd839`, and none of its claims about a container runtime rests on a run
    ("What this part rests on, and what is not proven").
    - **Order**, as the shared verification has it for every artifact (decision 14;
      ADR-047 decision 2; `artifact.go:613-617`, `:622-681`). It fetches the signature
      referrers and the image manifest, and uses no other blob until a signature over
      the artifact digest verifies with a key of `signers`. It then fetches
      `plugin.yaml`, checked against its descriptor. `plugin.yaml` names the id, the
      version and the runtimes, which the covering signer and the subject of the
      attestation are held to (decision 6), and nothing else of it is used before the
      attestation holds. It then verifies the attestation with the key that verified,
      for the subject `<id>@<version>` at the artifact digest. A failure is a refusal
      that names check 5 or check 10, as at install. Only after both hold does it fetch
      the bundle and the image. Where no signature verifies and `allowUnsigned` lists an
      id, `plugin.yaml` is fetched to learn the id, as today (`artifact.go:646`), and
      what follows is question Q17. That no image is fetched before a signature and the
      attestation hold is reading 2 of question Q15, and option (a1) of question Q17 is
      the one exception that an answer could open.
    - **What it verifies against.** The run uses the `serves` table and the suites of
      the release that this `aiopsctl` embeds: the served majors and definition versions
      of checks 3 and 9, and the suite binaries with their revisions, as step 5 of
      `plugin check` uses them on the same binary (ADR-060 decision 46; ADR-063
      decisions 16 and 17). `--against` and `--modcache` serve the build of in-process
      code (ADR-060 decision 46), and remote code builds nothing, so a run against
      another release is a run of that release's `aiopsctl`. What the command does with
      the two flags for remote code is the S2b part of ADR-061. An `aiopsctl` that
      embeds no suite reports the step as not built, as today.
    - **The image.** It is the one that the artifact pins, by digest and by no tag, and
      its content is checked against that digest before any of it runs. For a registry
      spec `aiopsctl` itself fetches the image manifest and every blob that it names,
      with the OCI client that it links for the artifact (ADR-047 decision 4), and
      checks each against its descriptor before use, as ADR-047 decision 2 has every
      blob. For a local-path spec it reads the image from the OCI layout that `plugin
      pack` wrote beside the artifact, with the same check (ADR-063 decision 61;
      ADR-047 decision 1). It hands the runtime that layout and asks it for the pinned
      digest, and a runtime that cannot run the handed image under that digest fails the
      step. The check of content against digest is then `aiopsctl`'s own, and one path
      serves both kinds of spec. Whether a runtime runs a handed layout under its digest
      is not proven. If it does not, the fallback for a registry spec is the runtime's
      own pull by digest, whose check is then the runtime's and not `aiopsctl`'s, as it
      is for the unit of an installed instance (ADR-063 decision 25). The S2b part of
      ADR-047 says which of the two is built. If neither serves a local-path spec, the
      work stops on that point and the owner is asked (ADR-063 decision 33).
    - **What runs.** The check runner of `aiopsctl` starts a container of that image and
      drives it through the handshake and the obligations half of the suite of each
      definition that the attestation names with a suite revision (ADR-063 decisions 9,
      14 and 17). The handshake evaluates checks 3 and 9 as on a server, the digest
      clause included: the digest of the first message equals the SHA-256 of the
      artifact's `plugin.yaml` (ADR-063 decisions 16, 54, reading 2, and 58).
    - **Fixtures.** The artifact of remote code holds no fixtures layer, which a pack
      alone has (ADR-047 decision 1), and `plugin verify` has no plugin directory, while
      step 5 of `plugin check` passes the suites the directory `fixtures/` of one
      (`author.go:402-403`). What the suites of a remote run read in its place is the
      S2b part of ADR-044. If they need the plugin's own fixtures, the artifact of
      remote code gains the fixtures layer in the S2b part of ADR-047, whose decision 2
      already has `plugin verify` fetch it. This decision is complete only with one of
      the two.
    - **The fence.** `plugin verify` runs a third party's code on the machine of whoever
      runs it, which install never does (`07:1842`). The container that it starts gets
      no more than the unit of ADR-063 decisions 24 and 25 gives an instance, and it is
      rendered from the same fixed template, with the same grammar check on the id and
      the `entrypoint`; that is reading 3 of question Q15. How the runner reaches that
      container and what identifies each side are ADR-041's, inside the points below.
      ADR-041 does not relax them, as it does not relax the points of ADR-063 decision
      23 for the dial of a server:
      - the channel is a Unix socket that only the runner can open, in a directory made
        for the run, or a connection that is mutually authenticated with a key pair made
        for that run, and no run is driven over a channel that is neither;
      - that directory and that key pair are outside the home, serve one run and are
        deleted with the container, and neither the kernel identity of the site nor the
        pin of an installed instance is used;
      - no listener of the container is published on a port of the host, and its network
        holds that container alone;
      - a process other than the container that the runner started cannot answer the
        runner, since its answer would stand as a proof of conformance (`08:940`).

      The flags, the step names and the texts are the S2b part of ADR-061 (ADR-063
      decision 34).
    - **It passes** only when the suites pass and what ran is what the attestation
      names: the definitions, their versions, the suite revisions and the halves, the
      rule that holds `plugin verify` of in-process code (ADR-060 decision 46, second
      note of 2026-11-25; `artifact.go:773-805`), and with question Q14 (b) the
      runtimes.
    - **It fails and names what differs** (ADR-063 decision 28): a blob of the artifact
      or of the image that does not hash to its descriptor, with both digests; a
      manifest rewritten after signing, whose digest no verified signature names, with
      check 5; an image whose manifest does not hash to the pinned digest, with both
      digests; a first message whose digest differs, with check 3 and both digests; and
      suites that ran other than the attestation names, with both lists.
    - **Without a container runtime** the suites step is reported as not run and names
      what it lacks, and `plugin verify` does not pass, since a step that did not run is
      a step that did not pass (`sdk/def/operations/v0/operations.go:502-507`;
      `07:1570`).
    - **What it leaves.** It writes nothing in the home: no record, no lock entry, no
      unit and no credential (ADR-061 decision 21). The record stays install's alone
      (decision 10), so a passed `plugin verify` admits nothing and a failed one removes
      nothing. When the step ends, on a pass, a failure, a refusal or an interrupt, it
      removes the container, its network, the directory and the key pair of the run, and
      the image that it handed to the runtime, unless the runtime held that image before
      the run. The run is bounded: the handshake by its deadline, and the step as a
      whole by a time after which the container is stopped and the step fails. The
      bounds are ADR-041's and the flag that sets one is the S2b part of ADR-061.
    - **No new module.** `plugin verify` links no module that `go.mod` does not pin. The
      image fetch uses the OCI client of ADR-047 decision 4, and the runtime is run as a
      process, as the builder runs its image (`internal/builder/image.go:417`).
      "Licences" above says what is read before the first use.
35. **The `signed` clause for a remote listener** is the clause of decision 7, read on
    the row's own manifest.
    - A remote plugin listens only on a hook marked remote, which check 9 holds, and the
      kernel module refuses an `On` that the manifest's `hooks` does not declare. The
      manifest therefore names every hook a remote row can listen on (`07:141`, `:446`,
      `:1116`; ADR-048 decision 16; ADR-063 decisions 10 and 16).
    - Where such a hook is of the `signed` class, the row passes check 7 only when an
      entry of `signers` that covers its recorded signer for the id and the runtime
      `remote` names the hook in its `signed` list (`08:505`; `boot/checks.go:384`). A
      key in a `signed` list never counts for remote code, which injects no signed key
      (decision 7; `07:565`). A signer recorded `unsigned` is never trusted for one
      (decision 8).
    - The clause reads the plugin id of the row, which is the artifact's own id, and the
      manifest and the record of that artifact (ADR-063 decision 41). That the code of
      every proxy is the first-party `aiops/remote` makes no remote row first-party for
      this clause; reading 4 of question Q15 asks the owner to confirm it.
    - Where no signer is known, at step 7 of `plugin check`, at `plugin pack` and at
      `plugin gate --against`, the clause reports "not evaluated" with that reason for a
      remote row, as for any row that is not first-party (decision 30, reading 3).
    - S2b proves the clause on a remote row. Decision 7 counts a hook of a `signed` list
      "for every runtime the entry covers" and says that "the remote runtime arrives at
      S2b", so S2b is the first step at which the clause meets a remote row, and the
      code that counts the hook is built (`boot/checks.go:384`). No hook in the table of
      `07` §3.4 is both marked remote and of access `signed`, so the proof needs a hook
      of the test definition: a second test hook, marked remote and of access `signed`,
      beside the one of ADR-063 decision 51. It uses the two fields that a hook already
      has and adds no mark, no field and no value to `definition.yaml` or `serves`,
      which sets it apart from option (a) of question Q16. Nothing of it ships in a
      release table. ADR-063 decision 51 names one test hook, so reading 7 of question
      Q15 asks the owner to confirm the second.
    - Decision 7 has a field that the definition marks raw put a remote listener in the
      `signed` class (`07:450`). No definition can say so today. `definition.yaml` and
      `serves` hold no mark for a raw field. The table of `07` §3.4 marks no field raw:
      `07:460`, `:465` and `:466` label part of what crosses as scrubbed and leave the
      rest unlabelled, and whether an unlabelled field is raw is for the ADR of its
      definition. The scrub that separates a raw field from the others is S5 (ADR-060
      decision 7; ADR-063 decision 26; `07:1503`). How the definition marks such a
      field, what the clause then reads and when both are built is question Q16.
36. **`allowUnsigned` and the runtime `remote`.** Whether an id that `allowUnsigned`
    lists is admitted as a remote artifact with no verified signature is question Q17,
    with what `plugin verify` does with such an artifact. Decision 8 and `01:303` name
    no runtime, and from S2b such an artifact is a unit that runs an image for which no
    key vouches (ADR-063 decision 34). Whatever the answer, these hold:
    - `allowUnsigned` is empty in hosted and lists exact ids, and boot prints it at
      every boot (decision 8; `08:406`; `07:1884`; `boot/trusted.go:188-189`).
    - It never admits remote code under `aiops/` or `contrib/`: the reserved-owner
      clause is evaluated first, and `unsigned` is no key of the release pipeline
      (`08:503`; `boot/checks.go:319`; `artifact.go:668-671`).
    - A signer recorded `unsigned` is never trusted for a `signed` hook (decision 8).
    - The S2b exit installs signed artifacts only (`08:697`; ADR-063 decision 38). The
      unsigned path is a case of decision 38, written for the answer either way (ADR-063
      decision 28).
37. **What each entry point verifies for remote code.** The table of decision 14 holds,
    with these rows for remote code.

    | Entry point | Verifies | A failure is |
    |---|---|---|
    | `plugin check` step 7, `plugin pack` | As decision 14, with a throwaway key: the signature, the attestation and the digest of the artifact, whose layer pins the image that `plugin pack` built (`07:1580`; ADR-063 decision 61). With no container runtime the step is not run and names what it lacks (`07:1570`; ADR-063 decision 17) | A failed step 7 |
    | `plugin install` of remote code | As decision 14 for every kind, with a signer that covers `remote`. It fetches no image (ADR-063 decision 17). Under `aiops/` or `contrib/` it refuses, whatever the signature (decision 31) | Check 5 or check 10; nothing is written |
    | `plugin verify` of remote code | The signature, `plugin.yaml` and the attestation in the order of decision 34, as install does; then the image against the pinned digest, the handshake and the suites | A refusal with check 5 or 10 before the image is fetched; after it, a failed verify that names what differs |
    | `plugin gate --against` | No signature (decision 14); the majors of check 3 and check 9 on the manifest, and check 10 on the statement as read (ADR-063 decision 16) | Check 3, 9 or 10 |
    | `profile render` | No signature and no dial: what boot reads, the lock, the kept files, the record and `trust.yaml`, without the module tie of checks 5 and 10 on the entry `aiops/remote`, which needs the binary, and without the two connection clauses of check 3 (`08:495`; `boot/admit.go:28-45`; ADR-063 decisions 16 and 59) | Check 3, 5, 7, 9 or 10, in the admission it prints |
    | `plugin start` | No signature. As question Q18 decides: with (a) no trust clause, and it starts the unit that install rendered from the verified artifact (ADR-063 decisions 17 and 25); with (b) check 5 on the installed artifact first, from the lock, the kept files, the record and `trust.yaml` | With (a), none of this ADR; with (b), check 5, and no unit is started |
    | Boot, every live generation and the remote handshake | No signature (`08:406`): the lock, the kept files, the record and `trust.yaml` for the artifact, and the module tie of the entry `aiops/remote` for the proxy code (decision 32). The handshake reads the kept `plugin.yaml` and writes no record (ADR-063 decisions 16 and 58) | Check 3, 5, 7, 9 or 10. The row is detached, and its unit is not stopped (decision 32) |

38. **The trust cases** of ADR-063 decision 28, as this part words them. Each gets a
    test that fails on its base, committed before the code that makes it pass (ADR-063
    decisions 28 and 29), on the artifacts of the exit or on copies of them. None exists
    at `bbfd839`.
    - A remote artifact whose signer is listed for its id with no runtimes, and one
      whose signer is listed for `inproc` alone: install refuses each with check 5 and
      writes nothing.
    - An installed remote artifact whose signer entry then loses `remote`: the next
      composition denies its row with check 5, the record is unchanged, no proxy is
      mounted and no dial is made, and the unit that ran still runs until `plugin
      disable`. `plugin start` of that id starts the unit or refuses with check 5, as
      question Q18 decides.
    - A remote artifact under `aiops/` and one under `contrib/`, each signed by a key
      that `trust.yaml` lists and that is no release key of the binary: check 5, at
      install and at boot.
    - A remote artifact under `aiops/` signed by the release key that the binary
      carries: install and `plugin verify` refuse it with check 5 (decision 31). A
      record of it that the harness wrote passes the signer clause of check 5 at boot,
      as `08:503` reads, which is the one way such a record exists at S2b.
    - A binary whose embedded manifest is not the current one: every remote row is
      denied with check 5. A binary whose entry `aiops/remote` does not tie to the
      trusted manifest: the same (decision 32).
    - A remote artifact whose attestation names another digest: check 10 at install.
    - An unsigned remote artifact of an id that `allowUnsigned` lists, admitted or
      refused as question Q17 decides, at install and at `plugin verify`; and one of an
      id that it does not list, refused with check 5.
    - A home whose record of a remote digest states other runtimes than the kept
      manifest: boot denies the row with check 5.
    - A remote row whose record, lock entry and `trust.yaml` entry the harness wrote:
      admitted, with every check evaluated (decision 32).
    - A remote row that listens on the test hook of access `signed`: admitted when the
      signer's entry names the hook in `signed`; denied with check 7 when the entry does
      not name it, and when the signer is recorded `unsigned`.
    - `plugin verify`: the cases of ADR-063 decision 28; an artifact whose signature
      does not verify, refused with no image fetched; an image built from another
      `plugin.yaml` than the artifact holds, failed with check 3; a local-path spec,
      verified from the layout beside the artifact; a process other than the started
      container that answers the runner, which does not pass; a run that exceeds its
      time bound, failed with the container stopped; a run with no container runtime,
      which does not pass; and after every run, no container, no network and no image of
      the run in the runtime, and no file of it in the home.
    - With question Q14 (b): a remote artifact whose statement names no runtime, or
      `inproc` alone, denied with check 10 at install, at `plugin gate --against` and at
      boot.
39. **Not built at S2b** by this part:

    | Part | Step | Why |
    |---|---|---|
    | The verification of a remote artifact under `aiops/` or `contrib/` with the release key, at install and at `plugin verify` | With the first release that publishes an artifact with no module under a reserved owner, S6a at the latest, where the release key signs `contrib/*` data packs (decision 17) | Decision 31; ADR-048 decision 14; no exit step and no shipped bundle of S2b holds such an artifact (ADR-063 decisions 22 and 27); `08:680`. At S2b every such artifact is refused with check 5 |
    | The mark of a raw field and the clause that reads it, with question Q16 (b) | With the first definition that owns a hook marked remote, S5 at the latest | Decision 35; ADR-063 decision 26; `08:680` |
    | `quarantine`, which denies one exact version and revokes its instances | S6b | `07:1894` |
    | `trust.yaml` and the records in platform tables, for hosted | S4b | Decision 17 |
    | Keyless signing; `requireAttestations` | As decisions 22 and 26 place them | Decisions 22 and 26 |

40. **What this part asks of the other S2b parts** (ADR-063 decision 34). Each is
    proposed with its own ADR, and none relaxes a decision above. Decisions 31, 32 and
    34 are final only when the part they name here is accepted.
    - ADR-047: the layer that pins the image is a layer of the artifact's OCI manifest,
      so the signed digest covers it (decision 31). Its decision 2 gains what `plugin
      verify` fetches for remote code, the image, after the signature and the
      attestation hold, and who fetches it: `aiopsctl` itself, or the runtime, as
      decision 34 puts the two. It says where a local-path spec finds the image layout
      beside the artifact and how a layout is handed to the runtime, and that the
      directory of a run is outside the home. If the S2b part of ADR-044 needs the
      plugin's own fixtures, the artifact of remote code gains the fixtures layer.
    - ADR-061: the form of `plugin verify` for remote code, its step names, its texts
      and its exit codes, inside decision 34: what it does with `--against` and
      `--modcache`, the flag of its time bound, and what it prints for an unsigned
      artifact under question Q17. The text of a check 5 or check 10 denial of a remote
      row, which says that the unit may still run and names the command that stops it
      (decision 32). With question Q18 (b), the refusal of `plugin start`.
    - ADR-041: how the check runner reaches a container that `plugin verify` started and
      what identifies each side there, inside the points of decision 34 and reading 3 of
      question Q15, with the deadline of that handshake and the bounds of the run.
    - ADR-048: whether a manifest that lists `inproc` and `remote` packs. If it does,
      its signer covers both runtimes (decision 6), its record states `module` (decision
      32) and its statement follows the answer to question Q14.
    - ADR-044: the suite binaries that drive a remote process, which `plugin verify`
      runs, and what they read as fixtures where no plugin directory exists (decision
      34).

### Consequences

- Remote code needs no new trust concept: one flag of `trust add`, the record of
  decision 10 without `module`, and the statement of decision 9.
- Boot proves less for a remote row than for an in-process row. It holds the kept files
  to the locked digest, the signer to `trust.yaml` and the proxy code to the trusted
  manifest, and it cannot hold the running image to the pinned one. The unit by digest
  and the pinned key pair carry that link, and decision 32 says so plainly.
- A denial stops no process. A remote row that boot denies mounts no proxy and is not
  dialled, and its unit runs until the operator runs `plugin disable` or `plugin
  remove`; nothing before `quarantine` at S6b revokes an instance for a withdrawn trust
  (decision 32).
- A remote artifact under `aiops/` or `contrib/` cannot be installed at S2b (decision
  31).
- `plugin verify` is the one command with which an operator runs a third party's image
  before install. It runs it only after a signature and the attestation hold, or, with
  question Q17 (a1), for an id that `allowUnsigned` lists, and inside the fence of
  decision 34. It needs a container runtime on the operator's machine, links no new
  module and leaves nothing behind.
- With question Q14 (b) the predicate gains one field and check 10 one clause. Both
  writers of a statement change, and every statement of the repository is written again,
  which the move of the toy registries to `0.2` requires in any case (ADR-063 decision
  48).
- With question Q16 (b) `definition.yaml`, `serves` and the definition lint do not
  change at S2b.
- With question Q17 (b) no remote code is installed or dialled unless a key that the
  site lists vouches for its artifact, as in-process code enters only through a
  distribution that a key of `distributions` signed. The sentence is about install and
  the dial: an instance that runs when its key is removed keeps running, as above.
- With question Q18 (b) `aiopsctl` starts no unit of an artifact that check 5 denies. A
  unit that an orchestrator starts without `aiopsctl` is not covered.
- The line caps of ADR-063 decision 36 stand. This part names no package they do not
  name.

### What this part rests on, and what is not proven

- **Parts of other ADRs that are not accepted.** ADR-041 does not exist at `bbfd839`,
  and the S2b parts of ADR-044, ADR-047, ADR-048 and ADR-061 are not written there. Four
  decisions rest on them:
  - decision 31, on the S2b part of ADR-047: that the image reference is a layer of the
    artifact's OCI manifest, so that the signed digest covers it;
  - decision 32, on the S2b part of ADR-048, for a manifest that lists both runtimes,
    and on the resolution of a remote row as ADR-063 decision 12 has it built, for the
    two subjects of the row;
  - decision 34, on ADR-041 for the channel and the identity of a run, on the S2b part
    of ADR-047 for the image layer, the layout handed to the runtime and a fixtures
    layer, on that of ADR-044 for the suite binaries and what they read as fixtures, and
    on that of ADR-061 for the form of the command;
  - decision 40, which lists what is asked of each.

  If one of those parts is accepted in another form, the decision that rests on it is
  proposed again.
- **Claims about a container runtime with no run behind them.** No code of the remote
  path of decision 34 exists. That a container runtime runs a handed image under its
  pinned digest, with no route out, as a user that is neither the site user nor root,
  with no path of the home, and reached only by the runner that started it (decision 34;
  reading 3 of question Q15) is written from the lines cited and from no run. Each claim
  is about the runtime and not about code of the project, so each can be tried on a
  machine with a container runtime before any code is written. The owner offered a
  sandbox environment for this work on 2026-11-27. If it holds a container runtime, the
  review of this part runs those claims there, names the runtime and its version, and
  adds the result to this section before question Q15 is answered; a claim that the run
  contradicts is corrected first. This text was written without that run.
- **Tests.** No test of decision 38 exists at `bbfd839`, and neither does the test
  definition of ADR-063 decision 51 or a hook of it.
- **Statements held elsewhere.** Question Q14 (b) assumes that no statement was
  distributed outside the repository. `08:1349` (L14) and ADR-063 decision 32 forbid
  publication; they are rules, and no evidence of what was copied.
- **Licences.** None was read for this part ("Licences" above).

### Edits on acceptance

The commit that records the owner's answers edits this section; the row of this ADR in
the ADR table of `08` §3 (`08:1154`) and its row in `00` (`00:761`), each of which gains
the S2b part with its date; and the lines and decisions that the chosen options name.
`00` is edited in that same commit (`08:999`). With the recommended answers these are:

- Q13 (a): nothing.
- Q14 (b): `07:1519`, `08:508` and `08:897`, where the statement names the runtimes; a
  dated note at decision 9, whose predicate gains the field, and one at ADR-060 decision
  18.
- Q15 (a): no line of `00`, `01`, `07` or `08`. Reading 4 adds a dated note at decision
  7, which names the id that the clause reads; reading 5 one at decision 17, whose table
  gains the first row of decision 39; reading 6 one at decision 12, which gains the two
  subjects of a remote row; and reading 7 one at ADR-063 decision 51, which gains the
  second test hook.
- Q16 (b): `07:450` and plugin record §8, which say when the mark of a raw field
  arrives; the dated note at decision 7 says that its sentence on a raw field waits for
  that mark.
- Q17 (b): a dated note at decision 8, and the lines that define `allowUnsigned` or
  state the unsigned path for an id that it lists, each of which gains that it covers no
  artifact whose manifest lists `remote`: `00:482`, `01:303`, `07:1519`, `07:1852`,
  `08:406`, `08:495`, `08:503`, `08:897`, row 12 of plugin record §2 and plugin record
  §11. Three lines that name `allowUnsigned` or the signers of remote plugins stand as
  written: community:207 and `08:941` list the fields of `trust.yaml` and say nothing of
  what each admits, and `07:1885` names "any signer the site trusts" and no unsigned
  path.
- Q18 (b): no line of `00`, `01`, `07` or `08`. The refusal joins `plugin start` in the
  S2b part of ADR-061, and ADR-063 decision 17 gains a dated note.

With Q13 (b) the table of decision 10 gains the field by a dated note. An option the
owner does not choose edits nothing.

### Questions for the owner

Each question gives its options, the recommended one, why, and what the answer blocks.
An option that changes or defers a written line says so and names the lines it edits,
and each such question holds the option that keeps the text as written.

**Q13. Does the record of a remote artifact state the image digest?** Decision 10 lists
the fields of a record and gives `module` to in-process code alone. A remote artifact
pins its image by digest in a layer that the artifact digest covers, and boot cannot
compare that digest with the image that runs (decision 32).
- (a) No, as decision 10 lists the fields. The record states no image. Install reads the
  image digest where it is pinned, in the artifact, when it renders the unit, and
  `plugin verify` when it fetches the image. The reader stays as it is.
- (b) Yes: `image: sha256:<hex>`, exactly when `runtimes` holds `remote`, as `module`
  exactly when it holds `inproc`. `plugin start` compares the image of the unit with it
  and refuses a unit that names another image. Boot reads the field and compares it with
  nothing. The table of decision 10 and the reader gain the field.
- Recommended: (a). The record vouches for a digest that already covers the image, so
  the field is a second copy of a fact that the kept artifact holds. Option (b) gives it
  a reader, a check at `plugin start`, and that check guards against one thing only: a
  unit file that the site user edited after install. The unit is a file of the home as
  the record is, so the comparison protects nothing that the ownership of the home does
  not (decision 11; ADR-047 decision 14). Blocks: the record that install writes for
  remote code.

**Q14. Does the attestation name the runtimes in which its suites ran?** The statement
names, per definition, the version, the suite revision and the halves (decision 9), and
no runtime. `plugin check` runs T2 in every runtime the manifest lists (`07:134`,
`:1578`), and `halves` does not tell a run through the adapter from a run in process: a
plugin that only contributes runs the obligations half in either. The attestation type
is published and documented for third parties, and `aiopsctl` is its one writer
(`08:938`; `07:1519`; ADR-060 decision 18).
- (a) No field, as decision 9 has the predicate. The runtime is implied: the subject is
  the artifact, its manifest lists the runtimes, and `plugin pack` records a statement
  only when step 5 passed in each (decision 33). A statement then makes no claim about
  where a suite ran.
- (b) One list. The predicate gains `runtimes`, the runtimes in which step 5 ran the
  suites that the statement names, as `["remote"]`. It is an added field of `v1`, which
  decision 9 allows. Every statement of a subject whose manifest lists a runtime carries
  it: `plugin pack` writes it, and the builder writes `["inproc"]` for an entry of a
  module table; a pack's statement carries none. Check 10 gains a clause and denies such
  a subject when the list lacks a runtime that its manifest lists, and `plugin verify`
  holds the list to what it ran. A statement written before the field exists carries
  none, and the new clause denies it for such a subject. Those of the repository are
  written again with the `0.2` minor of the toy registries (ADR-063 decision 48). None
  is published outside it, by `08:1349` (L14) and ADR-063 decision 32, which are rules
  and no evidence of what was copied. `07:1519`, `08:508` and `08:897` gain the
  runtimes, with a dated note at decision 9 and at ADR-060 decision 18.
- (c) One list per definition: each entry that holds a suite revision gains `runtimes`.
  As (b), with a list that could differ by definition, which no definition allows today,
  since step 5 runs every suite of a plugin in every runtime listed.
- Recommended: (b). The statement is the record of what passed (`07:1519`), and where a
  suite passed is part of that: a signer who signs `runtimes: ["remote"]` claims the run
  through the adapter, which (a) lets no statement claim and no check read. It also
  holds as written if the S2b part of ADR-048 lets one manifest list both runtimes. It
  costs one field and one clause. (c) carries the same facts in more places. Blocks: the
  attestation that `plugin pack` writes for remote code, and check 10 on a remote row.

**Q15. Seven readings.** None changes what a line requires. Readings 1 to 4 and 6 fill a
point that the lines leave open for remote code, reading 5 places at a step a
verification that no line places, and reading 7 adds a second test hook to the one that
ADR-063 decision 51 names.
1. **The image carries no signature of its own.** `dist sign` signs the digest of the
   distribution image, because the distribution manifest names no image (decision 12,
   note of 2026-11-24). A plugin artifact names its image by digest in a layer that the
   signed digest covers (decision 31). Reading: `plugin sign` signs the artifact and its
   attestation and nothing else, and no command of S2b looks for a signature on a plugin
   image. A registry policy that asks for a signature on every image finds none on a
   plugin image.
2. **No image before the signature.** ADR-047 decision 2 has a command that verifies use
   no other blob until the signature verifies. The image is not a blob of the artifact.
   Reading: `plugin verify` fetches no image until a signature and the attestation hold,
   and an artifact that fails either is refused with nothing fetched and nothing run
   (decision 34). The reading has one exception only if the owner chooses option (a1) of
   question Q17: an id that `allowUnsigned` lists, once the statement that `plugin pack`
   recorded passes check 10.
3. **The fence of `plugin verify`.** The isolation of the remote runtime is the process
   and a network policy (`08:388`; `07:1109`), and ADR-063 decisions 24 and 25 give a
   unit no route out, a user that is neither the site user nor root, and no path of the
   home but its own credential directory. No line says what fences the container that
   `plugin verify` starts. Reading: that container is rendered from the fixed template
   of ADR-063 decision 25, with the same grammar check on the id and the `entrypoint`.
   It runs the image by the pinned digest, as such a user, with no path of the home, no
   credential of the site and no route out. Only the check runner that started it
   reaches it, as only the check runner opens the socket of `plugin check` (ADR-063
   decisions 23 and 54, reading 2): over a Unix socket that only the runner can open, or
   over a connection mutually authenticated with a key pair made for that run, kept
   outside the home and deleted with the container, and over no channel that is neither.
   No listener is published on a port of the host. The container, its network and what
   the run made are removed when the step ends. ADR-041 fixes the rest inside these
   points and relaxes none (decision 34). No run stands behind this reading yet, and it
   is answered after the run that "What this part rests on, and what is not proven"
   names.
4. **Which id the `signed` clause reads.** For the binding lists each proxy value counts
   as `aiops/remote` (ADR-063 decision 41), and the `signed` clause passes a first-party
   row (decision 7; `boot/checks.go:378`). Reading: the clause reads the row's plugin
   id, the artifact's own, and never passes a row because its proxy code is
   `aiops/remote` (decision 35). A remote row under `aiops/` is first-party for the
   clause, and at S2b none can be installed (reading 5).
5. **A remote artifact under a reserved owner has no install path at S2b.** ADR-048
   decision 14 has check 5 deny such an artifact unless its verified signer is "the
   release key of ADR-060 decision 50, or a key that ADR-020 names for the release's
   artifacts". Decision 17 makes the release key a signer of artifacts at S6a, for
   `contrib/*` data packs alone, and no line says when a release signs remote code.
   Reading: this ADR names no further key. At S2b `plugin install` and `plugin verify`
   refuse every remote artifact under `aiops/` or `contrib/` with check 5, whatever its
   signature, and the verification with the release key is built with the first release
   that publishes an artifact with no module under a reserved owner, S6a at the latest
   (decisions 31 and 39). No exit step and no shipped bundle of S2b holds such an
   artifact (ADR-063 decisions 22 and 27).
6. **Checks 5 and 10 on the proxy code.** Decision 12 has check 5 deny the rows of a
   module whose tie fails, and "every row" when the embedded manifest is not trusted,
   and `08:503` denies "a Go module absent from a trusted distribution manifest". A
   remote row names the artifact's own id, which is in no module table, and runs the Go
   code of the entry `aiops/remote` (ADR-063 decision 41). Reading: a remote row has two
   subjects, the artifact and the entry `aiops/remote`. Check 5 evaluates the module
   clause and the tie on that entry, and denies every remote row when the tie fails or
   the embedded manifest is not trusted. Check 10 reads the artifact's statement for the
   row and the entry's own statement for the proxy code (decision 32).
7. **A second test hook.** ADR-063 decision 51 builds the remote listener path on "a
   test hook marked remote". No hook in the table of `07` §3.4 is both marked remote and
   of access `signed`, so the `signed` clause, which decision 7 places on every runtime,
   meets a remote row only on a hook of the test definition. Reading: the test
   definition owns a second test hook, marked remote and of access `signed`, and the
   cases of decision 38 run on it. It adds no mark and no value to `definition.yaml` or
   `serves`, and nothing of it ships (decision 35).
- (a) Accept the seven readings, each landing with the code that builds it, and reading
  3 after its run.
- (b) Answer each item on its own.
- Recommended: (a). Without reading 2 or 3 a command could run code that no check has
  seen, or run it with the reach of the site user. Without reading 4 every remote row
  would pass a clause that exists to stop it, and without reading 6 a remote row would
  run proxy code of a binary whose manifest no key of the site vouches for. Reading 1
  adds no signature that no check reads, and reading 5 builds no verification that no
  artifact of S2b needs. Reading 7 is the one way to test on a remote row a clause that
  S2a built for every runtime. Blocks: `plugin sign`, `plugin install` and `plugin
  verify` of remote code, and checks 5, 7 and 10 on a remote row.

**Q16. How is a raw field marked, and when is its clause built?** A deferral of written
text under option (b). `07:450` and plugin record §8: a remote listener receives the
scrubbed form of the payload, and "a field the definition marks raw forces the `signed`
class". Decision 7 reads it for the listener: such a field puts a remote listener in the
`signed` class. The hook of a definition carries `access` and `remote: yes|no` and no
mark of a field (ADR-060 decision 7). The table of `07` §3.4 marks no field raw:
`07:460`, `:465` and `:466` label part of what crosses as scrubbed and leave the rest
unlabelled, and whether an unlabelled field is raw is for the ADR of its definition. The
scrub is S5 (ADR-063 decision 26). The clause for a hook whose `access` is `signed` is
built, and is proven on a remote row at S2b under any option, on the second test hook of
decision 35 (reading 7 of question Q15).
- (a) At S2b, on the test definition. The `remote` mark of a hook gains a third value,
  `raw`: remote listeners are allowed and at least one field crosses unscrubbed. The
  definition lint and the reader of `serves` take it, and check 7 puts a listener of an
  artifact that lists `remote` on such a hook in the `signed` class, while an in-process
  listener keeps the hook's `access`. The test definition of ADR-063 decision 51 owns a
  further hook marked `raw`, and decision 38 gains its cases. The layout of ADR-060
  decision 7 gains the value by a dated note, and `08:505` names the clause.
- (b) With the first definition that owns a hook marked remote, S5 at the latest, where
  the scrub that tells a raw field from the others arrives (ADR-063 decision 26;
  `00:74`), by the ADR of that step, in the form of (a) unless that ADR says otherwise.
  Until that ADR is accepted no definition can mark a field, and S2b changes neither
  `definition.yaml` nor `serves`. `07:450` and plugin record §8 gain that step.
- (c) By the access class of the hook. A definition whose remote hook carries a raw
  field declares the hook `access: signed`, a rule for whoever reviews a definition,
  since nothing marks the field. The gate does not change. In-process listeners on that
  hook then need the `signed` trust too, which `07:450` does not ask of them.
- Recommended: (b). The table of `07` §3.4 marks no field raw and nothing scrubs before
  S5, so (a) would add a value to `definition.yaml` and `serves` that no definition uses
  before S5. `08:680` has each later input arrive with its consumer, and ADR-063
  decision 51 builds a path on a test definition only where a written line names the
  part for S2b. The second test hook of decision 35 is not that case: it adds nothing to
  a schema. The clause that a marked hook falls under is already built and is proven at
  S2b. (c) is stricter than the line for in-process listeners and cannot be checked. The
  owner decides, because `07:450` names no step. Blocks: nothing at S2b under (b); the
  definition lint, `serves` and check 7 under (a).

**Q17. Does `allowUnsigned` admit an unsigned remote artifact?** A narrowing of written
text under option (b). Decision 8, `08:406` and `01:303` let a self-host run unsigned
the ids that the site lists, and name no runtime. At S2a that never ran code for which
no key vouched: a pack has no code, and in-process code enters only through a
distribution that the site builds and signs (`07:1873`). From S2b an unsigned remote
artifact is a unit that runs an unsigned image, while `00:482` and the exit speak of
signed artifacts (`08:697`; ADR-063 decision 34). Decision 36 holds under either answer.
- (a) As written. An id that `allowUnsigned` lists is admitted as a remote artifact with
  no verified signature. Its signer is recorded `unsigned`, check 10 reads the statement
  that `plugin pack` recorded (decision 8; decision 30, reading 2), and every other
  clause applies. Install renders the unit, which runs the image by the digest that the
  locked artifact pins, inside the same fence (ADR-063 decisions 24 and 25), and says
  that the unit runs an unsigned image. No line is edited. With (a) the owner also says
  what `plugin verify` does with such an artifact, since reading 2 of question Q15 has
  it fetch no image before a signature verifies:
  - (a1) It runs the image. Once the statement that `plugin pack` recorded passes check
    10, `plugin verify` fetches the image, runs it inside the fence of decision 34 and
    reports the signer `unsigned`. Reading 2 of question Q15 then has that one
    exception.
  - (a2) It runs no unsigned image. `plugin verify` reads the statement, reports the
    signer `unsigned` and the suites step as not run for that reason, and does not pass.
    Reading 2 of question Q15 has no exception, and such an artifact has no proof of
    conformance on the site.
- (b) `allowUnsigned` covers no artifact whose manifest lists `remote`. `plugin install`
  and `plugin verify` refuse such an artifact with check 5, with no image fetched and
  nothing written, and boot denies with check 5 a remote row whose record names the
  signer `unsigned`. Packs and in-process code keep decision 8. The lines that "Edits on
  acceptance" lists for this option say that `allowUnsigned` covers no remote code, with
  a dated note at decision 8.
- Recommended: (b). It keeps for remote code what held at S2a: no code is installed on a
  site unless a key that the site lists vouches for it. An author who holds `aiopsctl`
  alone makes a key pair and signs (decision 21), and the site lists that key with one
  `trust add`, so a site that runs its own remote plugin loses little. The answer can be
  widened later without breaking a site, which a narrowing after (a) could not. (a)
  keeps the text as written and leaves the choice to the operator, who lists an exact id
  and types a digest, on a self-host where a gate check is an honest refusal and not a
  lock (`08:510`). If the owner chooses (a), then (a1): the site that lists the id runs
  that image as a unit inside the same fence, so (a2) protects nothing and takes away
  the one re-run that the operator has. The owner decides, because (b) narrows `01:303`
  and `00:482` is a line of `00`. Blocks: the install and the `plugin verify` of remote
  code and their unsigned cases.

**Q18. Does `plugin start` evaluate trust before it starts a unit?** An addition to a
command under option (b). Boot denies a remote row whose signer `trust.yaml` no longer
lists for `remote`, and the denial detaches the row and stops nothing (decision 32;
`08:510`). The kernel never starts or stops a unit (`08:447`). `plugin start <id>`
starts the unit of a compose fragment and is the operator's one command there, a systemd
unit is started with `systemctl enable --now`, and a Helm entry by `helm upgrade`
(ADR-063 decisions 17, 27 and 50; `08:1293`). `quarantine` is S6b (`07:1894`). No line
says whether `plugin start` reads `trust.yaml`. Under either option a unit that runs
keeps running until `plugin disable` or `plugin remove`, and no option deletes the pin
at a denial: boot writes no file (`08:510`; decision 10), and with no proxy mounted
nothing dials the instance.
- (a) No, as ADR-063 decision 17 words the command: `plugin start <id>` starts the unit
  that install rendered and evaluates no trust clause. After a signer is removed it
  still starts the image of that signer's artifact, inside the fence of the unit, and
  nothing dials it. No line is edited.
- (b) Yes. Before it starts anything, `plugin start` evaluates check 5 on the installed
  artifact as boot does, with no signature verified: the kept files against the locked
  digest, the record against the kept manifest, the reserved-owner clause, and the
  recorded signer against the present `trust.yaml` for the id and the runtime `remote`.
  Where the check denies, it starts nothing, names check 5 and exits non-zero. It does
  not evaluate the tie of the entry `aiops/remote`, which needs the server binary. It
  guards the start through `aiopsctl` alone: a unit that an orchestrator starts without
  `aiopsctl`, a systemd unit or a Helm entry, is not covered. The refusal joins `Start`
  in the S2b part of ADR-061, ADR-063 decision 17 gains a dated note, and no line of
  `00`, `01`, `07` or `08` is edited.
- Recommended: (b). `aiopsctl` then starts no image whose artifact the site's own
  `trust.yaml` denies, from files that it already reads and with gate code that exists
  (`boot/checks.go:294-331`), and the rule of question Q17 (b) holds for the one start
  that `aiopsctl` makes. It is a refusal by the operator's own tool and no lock, and the
  limits of decision 32 stand under both options; (a) states them and adds nothing. The
  owner decides, because (b) adds a refusal to a command that ADR-063 decision 17 words
  without one. Blocks: `plugin start`, and the case of decision 38 on a signer entry
  that loses `remote`.
