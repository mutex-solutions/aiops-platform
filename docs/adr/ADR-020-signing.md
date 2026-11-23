# ADR-020: Signing and trust of plugin artifacts and distributions

- Status: **proposed 2026-11-23, awaiting the project owner.** Nothing in this ADR is in
  force. ADR-060 decision 2 has it proposed before the first commit that implements it
  and accepted before that code merges into `main`; each question below names the code
  its answer is needed before. `00-decisions.md` is unchanged and wins until the owner
  accepts this ADR, and the lines it edits are listed in "Edits on acceptance" (`08:996`).
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
  <module>@<version>`, never from memory.

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
   (`cmd/cosign/cli/options/predicate.go:65-78`), so cosign attaches an attestation type
   of the project's own: the point `07:1512`, `08:935` and community:203 mark UNVERIFIED.
3. **cosign publishes to a public log by default.** `--tlog-upload` defaults to true,
   deprecated in favour of a signing config that names no transparency log
   (`cmd/cosign/cli/options/sign.go:125-127`, `signblob.go:127-129`). A verification
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
   v1.4.1 still requires rekor-tiles/v2 (`sigstore-go@v1.4.1/go.mod:16`).
7. **sigstore-go's signing package widens the graph.** Importing `pkg/sign` adds eleven
   modules to what verification links, two of them MPL-2.0 (decision 16). `pkg/sign`
   holds the Fulcio client, imports `rekor/pkg/client`, which brings the retrying HTTP
   library, and `sigstore/pkg/oauthflow`, which brings the OIDC browser flow, and reaches a
   command library through `rekor/pkg/pki/pkcs7` (`go mod why -m` on the probe module).
8. **in-toto.** A statement is `https://in-toto.io/Statement/v1`, its DSSE payload type is
   `application/vnd.in-toto+json`, and a predicate type is a URI that SHOULD carry a
   version and MAY be unresolvable
   (`github.com/in-toto/attestation@v1.2.0/spec/v1/statement.md:32`,
   `envelope.md:45-46`, `field_types.md:31-46`).

## Decision

### Library and format

1. **sigstore-go signs and verifies; cosign stays a tool.** `plugin sign`, `dist sign`,
   step 7 of `plugin check`, `plugin install`, `dist use` and the test harness of ADR-060
   decision 28 call one signing and verification package, internal to the operations
   plugin of ADR-060 decision 41, which links `github.com/sigstore/sigstore-go` at the
   version question Q1 fixes. This answers R17 for signing (`07:75`) and keeps the homes
   of verification that `08:404` and `07:1839` state: `aiopsctl`, and from S6a the
   `plugin-manager` side, which consumes the same plugin. `boot/` imports neither cosign
   nor sigstore-go and verifies no signature (`08:404`; `07:1880`). Which sigstore-go
   packages the signing half imports is question Q2; how its `init` registrations meet the
   builder's deny-list is question Q9. cosign is never linked and never baked into an
   image (`08:1380`); the release pipeline and the CI case of ADR-060 decision 36 run
   cosign v3.1.4, the release that requires sigstore-go v1.3.0 (Context item 1), so with
   Q1 (a) both sides of that case run the same verification code. CI installs its
   published release binary, pinned by checksum, because its `go.mod` needs go 1.27.
2. **One signature format: a Sigstore bundle v0.3 in key mode.** Every signature is a
   bundle of media type `application/vnd.dev.sigstore.bundle.v0.3+json` whose
   verification material is the key hint of decision 4, with no certificate, no
   transparency-log entry and no timestamp: the format cosign v3.1.4 writes by default and
   sigstore-go verifies with `WithKey()` and `WithNoObserverTimestamps()` (Context items 2
   and 5). It takes three forms:

   | Signed object | Bundle content | Where the bundle travels | cosign writes it with, and checks it with |
   |---|---|---|---|
   | An OCI artifact (ADR-060 decision 19) | A DSSE envelope of an in-toto Statement v1 with predicate type `https://sigstore.dev/cosign/sign/v1`, an empty predicate and one subject, the artifact's digest (decision 9) | An OCI referrer of the artifact, artifact type the bundle media type, annotation `dev.sigstore.bundle.predicateType` | `cosign sign`; `cosign verify` |
   | The attestation of an OCI artifact | The same, with the predicate type and predicate of decision 9 | An OCI referrer of the artifact (question Q6) | `cosign attest --type <URI>`; `cosign verify-attestation --type <URI>` |
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
7. **Trust for a `signed` key or hook** (`07:558`). The clause of check 7 passes for a
   row whose plugin is first-party, that is of owner `aiops`, which the builder admits
   only from the release table (`07:121`), and for a row of an installed in-process
   artifact whose recorded signer (decision 10) has a covering entry whose `signed` list
   names the key, as `identities`, or the hook, as `http/request`. Only in-process code
   may inject a signed key (`07:558`), so `signed` is read for `inproc` only. Where no
   signer is recorded, as at `dist build`, the clause reports "not evaluated" with that
   reason (ADR-060 decision 17). At S2a only a test definition declares a signed key
   (ADR-060 decision 15).
8. **`allowUnsigned`** lists exact ids. An artifact whose id it lists and that carries no
   signature a covering entry verifies passes the signature clause of check 5 with the
   signer recorded as `unsigned`, printed at every boot (`08:404`). Check 10 still applies
   (ADR-060 decision 16): the attestation of such an artifact is the unsigned statement
   that `plugin pack` recorded (decision 9), accepted for a listed id only. A row recorded
   `unsigned` is never trusted for a signed key or hook.

### The attestation

9. **Type, predicate and subject.** The attestation is an in-toto Statement v1 in a DSSE
   envelope inside the bundle of decision 2. Its predicate type is the URI of question Q7,
   ending in `/v1` so that the type carries its version (`08:920`; community:154). The
   predicate is frozen-additive, as the manifest schema is (`08:915`): a reader ignores an
   unknown field, and a change to an existing field is a `v2`. Its fields are those of
   ADR-060 decision 18:

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
   | An OCI artifact of any runtime: in-process code, a remote plugin from S2b, a pack | Name `<id>@<version>`; digest the SHA-256 of the artifact's OCI manifest, the digest install pulls by and the lock names | An OCI referrer of the artifact, as question Q6 decides | The artifact's signer, with `plugin sign` |
   | An entry of a release's module table: a first-party plugin (ADR-060 decision 50), or a plugin of a test distribution (ADR-060 decisions 28 and 55) | Name `<id>@<version>`; digest the SHA-256 of the plugin's generated manifest as the signed distribution manifest holds it | Inside the signed distribution manifest, beside that entry | The signer of that manifest |

   The artifact's own fields pin its code: the module path, version and sum of in-process
   code, the image digest of a remote plugin (ADR-060 decision 19), so one digest binds
   the statement to both. A module-table entry's code is pinned by the module sums of the
   signed manifest, which decision 12 ties to the binary. A site's `dist build` copies the
   statements of the release's first-party entries from the release's signed manifest that
   `release` names (`08:401`) into the manifest it writes, with the entries' manifests
   unchanged. `plugin pack` records the statement unsigned as the artifact's attestation
   referrer, and `plugin sign` replaces it with the signed bundle and adds the signature of
   decision 2; `plugin sign` refuses a statement whose subject is not the artifact's
   digest. Check 10 is then decidable at every entry point: the subject equals the digest
   of the artifact held, or of the manifest the module table holds.

### Records and how boot reads them

10. **Where the verified signer and the attestation are recorded.** Each digest that a
    verification by `aiopsctl` accepted has one directory,
    `$AIOPS_HOME/verified/sha256-<hex>/`, written only by `plugin install`, by `dist use`
    and by the test harness of ADR-060 decision 28 with the same code, under the profile
    write lock (`08:369`), after every clause the entry point evaluates has passed
    (ADR-060 decision 17), and restored with `plugins.lock` on a failure (`08:374`).

    | Record of | Files |
    |---|---|
    | An artifact | `record.yaml` with `kind: artifact`, the digest, the signer's key id or `unsigned`, the plugin id, version and runtimes, and for in-process code its module path, version and sum; `signature.sigstore.json` and `attestation.sigstore.json` as pulled; `attestation.json`, the statement that was verified |
    | A distribution manifest | `record.yaml` with `kind: distribution`, the digest of the manifest's bytes and the signer's key id; `manifest`, those bytes; `manifest.sigstore.json` |

    The record of an in-process artifact is its attestation recorded until boot, as
    ADR-060 decision 33 asks. The first `plugin install` of in-process code changes no file
    and the run after `dist use` writes the record (ADR-060 decision 30, step C2).
    `plugins.lock`, whose place ADR-047 fixes, names the digest of every installed
    artifact, in-process ones included: their entry pins nothing, since the signed
    manifest pins the module, and lets boot find the record of each row's origin artifact,
    which the loader records (ADR-060 decision 19). Boot reads records and never writes
    them, and reads `trust.yaml` at every boot, so a key removed from it denies at the next
    boot without a record being touched.
11. **How the records are protected until boot** is question Q5. Under its recommended
    answer a record is as strong as `trust.yaml`, which nothing protects beyond the site's
    file ownership either: it is written only by `aiopsctl` running as the site's user;
    ADR-047's mount table mounts `trust.yaml` and `verified/` read-only into a kernel that
    runs as a container (ADR-060 decision 33); a record is named by the digest it vouches
    for, so it cannot vouch for other content; and it keeps the bundles it was verified
    from, so `aiopsctl` can verify it again. A process that runs as the site's user can
    rewrite a record, and can rewrite `trust.yaml` too; on self-host a gate check is an
    honest refusal and not a lock (`08:508`).
12. **How boot ties the module table it runs to the verified manifest** (`07:1861`;
    community:111, G9; ADR-060 decision 33).
    - The binary embeds the signed distribution manifest it was built with (ADR-060
      decision 20). Boot computes the SHA-256 of the embedded bytes.
    - That manifest is trusted when a distribution record of that digest exists whose
      signer is a key of `distributions`, or when the binary's built-in record of decision
      13 names that digest and `release` is a trusted root.
    - Every entry of the module table it runs names a module that the trusted manifest
      lists with the path, version and sum the Go toolchain recorded in the binary
      (`runtime/debug.ReadBuildInfo`, the main module included); a difference denies the
      rows of that module by check 5.
    - When the embedded manifest is not trusted, every Go module is absent from a trusted
      distribution manifest, so check 5 denies every row (ADR-060 decision 31). Each row is
      detached and a required row stops boot (`08:467`); the refusal names the embedded
      digest, the digests the records hold and `aiopsctl dist use <image>`.
    - `dist sign <image>` signs the manifest as a blob and pushes the manifest and its
      bundle as one OCI referrer of the image, whose media types ADR-047 names with the
      rest of the OCI layout; the release pipeline signs with `cosign sign-blob` and
      pushes the same referrer, and `release` in a site's `dist.yaml` names it by digest
      (`08:401`). `dist use <image>` reads that referrer, verifies the bundle with each
      key of `distributions`, and writes the record, or refuses and writes nothing.

### The default release root

13. **The release key as a default distribution root** (ADR-060 decision 50).
    - The release build takes the release public key as an input. The release pipeline
      signs the manifest the builder wrote with `cosign sign-blob` before the final link,
      and the release binaries, the server of the release image and the release
      `aiopsctl`, embed the manifest, the release public key and the manifest's bundle.
      Their built-in record is the digest of the embedded manifest with the release key as
      signer.
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
      a distribution record only, as the S1 suites do (ADR-060 decision 28).
    - First-party rows pass check 5 through the signer of the trusted manifest and check
      10 through the statements it holds (decision 9), so both checks run on every row
      and the release starts as `08:398` and `01:292` state.

### Each entry point

14. **What each entry point verifies, and which check names a failure.**

    | Entry point | Verifies | A failure is |
    |---|---|---|
    | `plugin check` step 7, `plugin pack` | Signs with a throwaway key held in memory and verifies the bundle and the attestation against it (`07:1573`) | A failed step 7 |
    | `plugin install`, every kind | For each signer entry that covers the id and runtimes, the signature bundle against that key and the artifact's digest; the first that verifies gives the signer. Then the attestation bundle against the same key | No verifying signature: check 5. No verifying attestation: check 10, missing. One that verifies and names another digest: check 10, another subject (ADR-060 decision 30, step D2) |
    | `dist use` | The manifest's bundle against each key of `distributions` | A refusal; no record is written |
    | `plugin gate --against` | No signature, since check 5 is not evaluated there (ADR-060 decision 17); it reads the statement from the attestation bundle's payload | Check 10, on the statement as read |
    | `dist build` | No signature; checks 5 and 10 are not evaluated there (ADR-060 decision 17) | None of this ADR |
    | Boot and every live generation | No signature (`08:404`): records, `trust.yaml` and the embedded manifest, as decisions 10 and 12 state | Check 5, 7 or 10 |

### Key lifetime and rotation

15. **No key expires at S2a, and rotation is by `trust.yaml`.** With no timestamp in a
    bundle (decision 2), a signature has no time a verifier can trust, so a plugin signer
    key and a distribution key are valid while `trust.yaml` lists them. This fills the
    lifetime that `00:411` leaves to this ADR and keeps `00:410`. A publisher rotates by
    adding the new key with `trust add`, signing again with `plugin sign`, which adds a
    signature referrer to the same digest, and having sites install again; a site rotates
    its distribution key by `dist sign` and `dist use` with the new key. Deleting the old
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
    module the root module links today, and its highest `go` line is 1.26.0.
    - sigstore-go itself is Apache-2.0 (`LICENSE`; `COPYRIGHT.txt` names the Sigstore
      Authors).
    - Verification and the key format add 68 modules to the package graph:
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
        `google.golang.org/grpc@v1.82.1`, `k8s.io/klog/v2@v2.140.0` (50).
      - BSD-3-Clause: `github.com/google/uuid@v1.6.0`,
        `github.com/grpc-ecosystem/grpc-gateway/v2@v2.29.0`, `golang.org/x/crypto@v0.54.0`,
        `golang.org/x/mod@v0.42.0`, `golang.org/x/net@v0.57.0`, `golang.org/x/sys@v0.47.0`,
        `golang.org/x/term@v0.45.0`, `google.golang.org/protobuf@v1.36.11` (8).
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
      `x/mod`, `x/net`, `x/sys`, `x/term` and `protobuf` are copied; the licence files of
      `json-canonicalization`, `in-toto/attestation` and `in-toto-golang` hold only the
      Apache-2.0 notice, so the full text is added; the files of the three OpenTelemetry
      modules hold Apache-2.0 followed by a BSD-3-Clause text, recorded as `Apache-2.0
      AND BSD-3-Clause`; `go-openapi/runtime/middleware/denco` carries its own MIT file;
      and `go-digest`'s `LICENSE.docs`, CC-BY-SA-4.0, covers only its `README.md` and
      `CONTRIBUTING.md` (`README.md:91-93`), which no binary links, so the per-directory
      scan of L16 records it as a documentation licence and not a licence of linked code.
    - The module graph, that is the build list of the root module with sigstore-go
      v1.3.0 added, holds 382 modules. By the licence file at each module's root: 198
      Apache-2.0, 108 MIT, 42 BSD-3-Clause, 13 BSD-2-Clause, 19 MPL-2.0 and 2 ISC; none
      carries a GPL, LGPL, AGPL, BUSL, SSPL, Elastic, PolyForm or Commons Clause text, and
      none lacks a licence file. The MPL-2.0 modules outside the package graph include
      `github.com/hashicorp/vault/api@v1.22.0`, whose own licence file is MPL-2.0, and
      `github.com/letsencrypt/boulder@v0.20260309.0`; the five River modules are the root
      module's own (L8). A module outside the package graph is in no binary, so the
      allow-list of L7 does not reach it.
    - Not read: what changed between v1.3.0 and v1.4.1, and any vulnerability database
      for these pins. The register entry of sigstore-go (ADR-060 decision 34; `08:1332`
      L3) records the reads above, and the licence scan of ADR-060 decision 36 repeats them
      on the built binaries.

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

## Consequences

- `aiopsctl` and the server image link sigstore-go and the 68 modules of decision 16 from
  S2a, the gRPC runtime among them, since both binaries carry the operations plugin
  (`08:359`; ADR-060 decisions 41 and 53).
- `boot` gains readers of `trust.yaml`, of the records and of the binary's build
  information, and no signature code; its provisional cap is ADR-060 decision 35's.
- No signature of the project reaches a public log, so nothing is disclosed, and no
  signature carries a time: revocation is deleting a key from `trust.yaml`, and a leaked
  key signs admissible artifacts until then (decision 15).
- A record is exactly as strong as `trust.yaml` (decision 11).
- cosign v3.1.4 and `aiopsctl` write the same objects, so the release pipeline signs with
  cosign and a site or an author with `aiopsctl`, and either verifies the other's.
- Removing the default root also untrusts the release `aiopsctl`'s own manifest, so the
  release `aiopsctl` then refuses its own rows; a site that removes it runs an `aiopsctl`
  whose manifest it has recorded, and ADR-044 says whether the builder writes one.
- `plugin gate --against` reads an attestation it does not verify, as decision 17 of
  ADR-060 has it evaluate check 10 without `trust.yaml`; its verdict is about
  compatibility, not trust.

## Edits on acceptance

The commit that records the owner's answers edits, besides the ADR table of `08` §3:
`00:411` (the lifetime, decision 15) and `00:757`; `07:75` (R17), `:1512`, `:1839`,
`:1841` (the record and the lock entry of in-process artifacts, decision 10), `:1846`,
`:1852` and `:1854`; `08:404` and `:935`; `08:1337` L8 and `:1342` L13 (the licences read);
community:203 and :458; licence:49 and :258; and the plugin record in §11 ("keyless where
it works") and §18. Answers Q3 and Q6 also edit the lines their options name.

## Questions for the owner

Each question gives its options, the recommended one, why, and when the answer is needed.

**Q1. Which sigstore-go version is pinned?** The latest, v1.4.1, and v1.4.0 declare
`go 1.27.0`; the repository builds with go1.26.8 under `GOTOOLCHAIN=local` (ADR-060
decision 21). v1.3.0 declares `go 1.25.8`, and cosign v3.1.4 requires it (Context item 1).
- (a) v1.3.0.
- (b) v1.4.1, with the toolchain line of every module, the builder image and the exit's Go
  environment moved to go 1.27.
- Recommended: (a). It builds on the repository's toolchain, it is the code cosign v3.1.4
  verifies with, and every fact of decision 16 was read at it. v1.4.1 still links
  rekor-tiles/v2 and gRPC, so it changes none of decisions 2 and 16 or question Q9; what
  else changed was not read. Needed before sigstore-go merges.

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

**Q4. Is keyless signing offered at S2a?** Plugin record §11 says "keyless where it works";
`08:404` and `07:1852` call keyless verification at an air-gapped site unconfirmed, and the
terms of the public instances are UNVERIFIED (licence:49). The public instances publish the
signer's identity and the digest in a public log (Context item 3), which L14 and ADR-057
decision 5 rule out, and the exit's networks reach nothing outside the job.
- (a) Not at S2a: key mode only, and keyless is taken up when a public release is planned
  after L14 clears; decision 3 keeps it additive.
- (b) Keyless against a private instance that runs inside the exit job: an OIDC issuer, a
  certificate authority and a log, trusted through a root file in `trust.yaml`.
- (c) Keyless against the public instances. Listed only to be refused under L14.
- Recommended: (a). Nothing at S2a can use keyless signing without publishing or without
  running a private instance that no site has, and (a) changes no format. Needed before
  `plugin sign` merges.

**Q5. What protects the records of install and `dist use` until boot?** Boot verifies no
signature and imports neither cosign nor sigstore-go (`08:404`), and the records sit in
`$AIOPS_HOME` beside `trust.yaml`, which the same user writes (`07:1840`, `:1841`).
- (a) As decision 11: written only by `aiopsctl` under the write lock, mounted read-only
  into the kernel by ADR-047's table, named by digest, holding the verified bundles, and
  read against the current `trust.yaml` at every boot. A process running as the site's
  user is not stopped, as it is not stopped from editing `trust.yaml` (`08:508`).
- (b) Boot checks each record's signature with the standard library's `crypto/ecdsa` over
  the DSSE encoding. It edits "no signature verification is written by hand" and the
  verification home of `08:404`, and adds nothing against a writer of `trust.yaml`.
- (c) `aiopsctl` seals each record with a keyed hash under a site secret that boot reads.
  The same user reads the secret, so it adds nothing over (a), and it is a new credential
  class (`00:379`).
- Recommended: (a). It keeps `08:404` whole, and (b) and (c) protect nothing that
  `trust.yaml` itself does not leave open. Needed before the record writer of install or
  `dist use` merges.

**Q6. Is the attestation an OCI referrer or a layer of the artifact?** ADR-060 decision 18
leaves the choice here. `08:391`, `07:1838` and ADR-060 decision 19 describe one OCI
artifact that holds the signed attestation.
- (a) A referrer, beside the signature referrer of decision 2, with the artifact's
  manifest digest as subject, as `cosign attest` writes it. `08:391`, `07:1838` and
  ADR-060 decision 19 are edited to read "one OCI artifact with its signature and
  attestation as referrers". The exit's registry (ADR-060 decision 34) is read for the
  OCI 1.1 referrers API or the tag fallback of the distribution spec.
- (b) A layer of the artifact, whose subjects are the digests of the artifact's other
  layers. The artifact's digest then covers the attestation, check 10 compares a set of
  digests, the signature stays a referrer (it signs the manifest digest), and `cosign
  verify-attestation` cannot find the statement.
- (c) Both, the layer and the referrer.
- Recommended: (a). One digest binds the statement and the code, the gate's comparison is
  one digest, cosign verifies it, and `plugin sign` works after `plugin pack` without
  changing the digest. Needed before `plugin pack` merges.

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
  every version bump.
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
