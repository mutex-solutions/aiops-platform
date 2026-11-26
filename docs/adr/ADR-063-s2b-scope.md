# ADR-063: Scope and working rules of S2b (the remote runtime, single-tenant form)

- Status: **proposed, awaiting the project owner.** No S2b code starts before this ADR is
  accepted and the questions marked "before any S2b code" are answered (decision 1).
- Scope: roadmap step S2b (`08-profiles-roadmap-adrs.md` §2.1). It turns the S2b text of
  the roadmap into decidable work and working rules, as ADR-060 did for S2a. It changes no
  decision of `00-decisions.md` by itself: every point where an answer would change one
  is a question below and names the lines it edits, and `00` is edited in the commit that
  records the answer (`08:999`).
- Citations: `00`, `01`, `02`, `03`, `07` and `08` are the files of `docs/architecture/`
  with those numbers, and `08:697` is line 697 of `08` at commit `50665fa`, the head of
  the S2a branch. "community", "harness", "licence" and "s2a-report" are
  `docs/design/community-first-2026-11.md`, `harness-and-upstreams-2026-11.md`,
  `licence-audit-2026-11.md` and `s2a-report.md`, cited the same way. The plugin record,
  `docs/design/plugin-system-2026-11.md`, is cited by section. A path with a line, as
  `boot/gate.go:83`, is a file of the repository at the same commit. The accepted ADRs
  cite an earlier commit, so their numbers for `07` §5 and for `08` differ by a few
  lines: `07:1133` in ADR-060 and ADR-034 is `07:1140` here, and `08:695` is `08:697`.

## Context

S2b builds the remote runtime in its single-tenant form (`08:623`, `:697`; `00:573`):
the adapter generator; the remote protocol at major 0 (`sdk/remote`) and
`sdk/remotehost`; the `remote` proxy row; the Python SDK `aiops-sdk` and its template;
one shared instance per plugin. ADR-041 is due, provisional, "with the instance identity
on a single-tenant self-host and the `tools` callback in the callback set" (`08:697`,
`:1175`; `00:777`). The exit: the same repository publishes a remote Go plugin and a
remote Python plugin; a clean machine installs them from signed artifacts with no
rebuild and calls each; a plugin that offers only an unknown protocol major is refused at
the handshake, with the reason logged on both sides (`08:697`).

The place of S2b is an owner decision that is not reopened: the remote runtime and the
Python SDK ship before the first community release, and the owner's words defer
per-tenant remote plugin instances, not the runtime (W5: `00:109`; `08:14`, `:609`;
community:25, :416). S2b follows S2a because the remote runtime reuses the manifest, the
gate, the lock and the attestation, and its handshake message was frozen at S2a
(`08:644`). It precedes S3 because from S2a a `contrib` definition is written with its
remote adapter (`08:645`). S2a and S2b together pass the round-1 ecosystem exit, a third
party publishing a plugin with no kernel checkout (`08:703`, `:881`).

S2a hands S2b gate checks 3 and 9, `runtimes: [remote]`, `plugin.FromManifest`, the
adapter generator with the `contrib` mark of the toy registries, and the Python template
(s2a-report:482; ADR-060 decision 29). At `50665fa` each is a recorded refusal or an
empty slot:

- Checks 3 and 9 report "not evaluated until S2b" at every entry point (`boot/gate.go:62`,
  `:83`, `:89`), and a row whose plugin is not in the module table is refused with "a
  remote plugin is not read before S2b" (`boot/gate.go:565-575`).
- `plugin check`, `plugin pack` and `plugin install` refuse `remote`
  (`plugins/operations/internal/operations/author.go:249`, `artifact.go:91`,
  `install.go:118`), and `plugin new` refuses `--lang python` (`author.go:40`;
  `internal/authoring/tools.go:140`).
- The manifest reader already takes `runtimes: [remote]` and requires
  `requires.remoteProtocol` exactly then (`boot/manifest.go:500-517`;
  `sdk/plugin/manifest.go:110`), and `trust.yaml` already limits a signer to the
  runtimes `inproc` and `remote` (`boot/trust.go:136`, `:171`). No generator writes
  `remoteProtocol` (ADR-048 decision 12).
- `sdk/remote/handshake/handshake.proto` holds the frozen message `Hello` and its freeze
  test; no Go or Python is generated from it, and the file is a package of the module
  `sdk`. `sdk/remotehost`, a proxy plugin, an adapter, a definition proto and any Python
  do not exist.
- `echoers` and `stampers` are at version `0.1`, `remote: "no"`, suite 1
  (`sdk/def/echoers/v0/definition.yaml`, `sdk/def/stampers/v0/definition.yaml`).
- Five failure-matrix cells are marked `since: S2b`
  (`test/failure-matrix/matrix.yaml:360-386`, `:689-694`), and the harness runs only the
  cells of S1 (`test/failure-matrix/harness_test.go:650-653`).
- The S2a branch is not merged into `main`: the call-path gate on the merge commit and
  the exit caps are open (s2a-report:409-454, :481), and S2b starts only on the owner's
  word (s2a-report:502).

Reading the S2b text against the code at `50665fa` and against the later steps shows
gaps:

- The exit differs by source. `08:697` and community:60 have "with the reason logged on
  both sides"; `00:573` does not, and `08:3` lets `00` win. `01:290` has neither "calls
  each" nor the refusal. community:60 and plugin record §16 name "a remote Go echoer and
  a remote Python stamper", and plugin record §16 adds "into `dev`" and "no policy service
  and no tenant data".
- The `remote` row injects `sidecars` and `tenantMint` (`03:72`), takes its endpoint from
  `sidecars` and is PENDING until that endpoint is available (`07:1150`, `:1158`), and
  `plugin enable` refuses "while the endpoint is missing from the `sidecars` table"
  (`08:398`). `sidecars`, `sidecars-static`, `tenant-handle` and `tenantMint` are S4a
  (`08:699`), after S2b, and no definition of either exists.
- The dial is mutual TLS (`07:1159`; `02:213`), and on a single-tenant self-host neither
  side has an issuer: `00:410-411` holds only the hosted classes, the instance identity
  is open (`02:483`, item 25), and the "cell's kernel certificate" that the replica
  presents does not exist on a self-host either.
- `remote` is one of the eleven rows of `aiops-core` (`00:276`; `08:273`), the row must
  speak gRPC (`07:1104`; `00:44`), and no row of `aiops-core` imports an upstream module
  (ADR-054 decision 23; ADR-062:686-687). The names and the modules of gRPC and Protocol
  Buffers are on no list of stated exceptions (`00:268`).
- A `contrib` definition holds its proto and its generated remote adapter in
  `sdk/def/<name>/v<N>` (`07:28`; `08:1215`), a directory of the module `sdk`, whose
  require list is closed to `sdk/kernel` and pgx v5 (`00:263`; `07:60` R2; ADR-054
  decision 13) and whose definitions import no upstream module (`07:527`;
  `internal/vet/imports.go:195-198`).
- ADR-060 decision 42 leaves the module of `sdk/remote` to S2b and decision 54 says
  `sdk/remote` and `sdk/remotehost` need a nested module. The freeze test refuses an
  added `option go_package` (`sdk/remote/handshake/handshake_test.go:162`), `00:263` and
  `08:1205` count three SDK modules, and the builder serves four modules by name
  (`internal/builder/build.go:94`).
- `serves` has "the same shape" as `requires` (`08:904`; `00:267`), but the type and its
  strict reader hold `pluginApi`, `kernel` and `definitions` only
  (`boot/distribution.go:86-90`, `:241-242`): no protocol majors, and no `remote` mark of
  a definition, which the contribution-set clause of check 9 needs.
- The gate "runs at every remote handshake" (`08:495`), yet the gate is `boot` library
  code, a plugin imports no `boot` package (`internal/vet/imports.go:211-214`), and ADR-060
  decision 17 has no entry point for a handshake.
- The first message's manifest digest "must equal `plugins.lock`" (`07:1128`). The lock
  holds the digest of the artifact's OCI manifest (ADR-047 decision 8;
  `boot/install.go:21-29`), ADR-048 decision 1 says the handshake digest names
  `plugin.yaml`, and an image cannot hold the digest of the artifact that names it.
- The row of an installed artifact is a row of the artifact's own plugin (`08:393`), and
  boot resolves a remote plugin in `plugins.lock` (`03:358`), while the texts speak of
  `aiops/remote` rows (`07:1140`; `08:437`). The loader's plugin values are fixed when it
  mounts (`loader/loader.go:46-50`), `boot/` and `loader/` may not import `sdk/plugin`
  (`08:1260`), and remote code is live (`08:194`).
- The proxy needs bound keys: `tenantMint` is consumer-bound (`03:72`; `00:330`), and the
  launcher's toolbox, which holds the gate, is reached through the consumer-bound
  `profileContext` (`dist/dist.yaml:47`). The kernel compiles the binding lists once,
  when it starts, by entry (`sdk/kernel/root.go:89-97`), and a proxy is built from a
  manifest that arrives later.
- No definition of the callback set exists (`07:1169-1180`): `kv`, `blob` and `secrets`
  are S3, `tenants` S4a, `audit` and `credentials` S5, `llm` and `tools` S8a (`08:629`,
  `:698-700`). No definition owns a hook marked remote, and `sdk/gate` is S5.
- The matrix cell "while running" expects a breaker (`matrix.yaml:381-386`), which is the
  dispatcher's (`07:783`, `:1153`), and the toy dispatcher has none.
- The deployment unit is a compose fragment, a systemd unit or a Helm values entry
  (`08:1292`), and `deploy/compose` and `deploy/helm` arrive at S7 (ADR-060 decision 22).
- Nothing is published outside the private repository (`08:1348` L14; ADR-057 decision
  5), and the Python SDK is a package that an author installs. The gRPC runtime of the
  Python SDK is an unread entry that blocks until read (`08:1347` L13; licence:68). The
  line caps and the register checks read Go only.
- `sdk/kernel` has 25 lines left under its cap of 8,200 and `loader` 196 under 1,300.
  `boot` measures 9,777 lines at `50665fa`, against an S2a exit cap that is proposed at
  10,800 and not yet settled (s2a-report:409-453).

## Decision

### Order and gating

1. **S2b code starts after four things.** This ADR is accepted; the questions marked
   "before any S2b code" are answered; S2a is merged into `main`, with the call-path gate
   run on the merge commit and the S2a exit caps written into ADR-060, `ci.yml` and the
   Makefile (s2a-report:481, :409-454); and the owner gives the word of s2a-report:502.
   The branch of decision 28 is cut from that `main`. Each other question is answered
   before the code it names merges into `main`.
2. **Each ADR due at S2b is its own ADR.** ADR-041 is proposed to the owner, provisional,
   before the first commit that implements the protocol and accepted before that code
   merges into `main` (`08:697`, `:1175`; the rule of ADR-060 decision 2). The S2b parts
   that accepted ADRs left open (ADR-020, ADR-034, ADR-044, ADR-047, ADR-048, ADR-054 and
   ADR-061) are written as dated additions to those ADRs, proposed and accepted the same
   way. Each keeps every written requirement it touches. Decision 30 lists what each must
   settle.
3. **No kernel change is planned.** The kernel learns nothing about "remote" (`07:1140`)
   and stays at 8,175 of its 8,200 lines (s2a-report:453). A kernel change that S2b finds
   necessary is proposed to the owner as an ADR-034 addition before it merges, stays
   inside the cap and is verified as ADR-060 decision 3 requires: the differential
   corpus, the property workload and the call-path gate.
4. **W5 and the dsh model stand.** S2b is not moved, split or reduced to fit a date
   (`00:109`; `08:607`). The remote runtime is the adaptation that plugin record §15
   records in rows 35 and 36: a remote plugin is a set of contributions, and a
   third-party plugin is a first-class fiber only in process. S2b adds no other departure
   from that record; one that the work finds necessary is a question to the owner before
   it is built.

### Packages and deliverables

5. **The remote protocol at major 0** follows the four rules of `07:1126-1129`. The major
   is the proto package `aiops.remote.v0`, matched exactly until platform 1.0 (`08:894`,
   `:921`; community:153). Every connection opens with the frozen `Hello`
   (`07:1127`, `:1133-1137`). The first message of the chosen major carries the plugin
   id, the version, the manifest digest, the activation id, the definitions as
   `name@major.minor`, feature names and one reserved optional field that nothing reads
   in the community edition (`07:1128`; `08:948`). The tenant and the row come from the
   connection, never from a field (`07:1128`; `03:162`). Activation is per connection,
   with `Activate`, `Register` and `Deactivate`, and a registration from a superseded
   activation is refused, which is `ErrInactiveEffect` across the process boundary
   (`07:1161-1166`). The message and field names are descriptive (`07:67` R9), and
   ADR-041 fixes them (decision 30).
6. **The handshake file stays frozen.** S2b generates Go and Python from
   `sdk/remote/handshake/handshake.proto` without editing it: the freeze test keeps every
   case it has (ADR-060 decision 42), so the Go import path is given to the generator and
   not written in the file. `Hello.diagnostics` carries the SDK name and version and the
   release, is logged and is never read for a decision (`07:1136`). With no common major
   both sides log both lists and the kernel closes the stream (`07:1127`).
7. **The module homes** of `sdk/remote` and `sdk/remotehost` are fixed at S2b (`07:60`
   R2; ADR-060 decisions 42 and 54), as question Q6 decides. Whatever the answer, the
   require list of the module `sdk` gains no line (`00:263`; ADR-054 decision 13), the
   check `kernelcheck modfile sdk` keeps its list (`Makefile:167`), and each new module
   gets a closed require list of its own under the same check, a served `go.mod` copy
   (ADR-060 decision 21) and its place in the builder's list of release modules
   (`internal/builder/build.go:94`).
8. **`sdk/remotehost`** is the kernel module inside a Go plugin process, with definitions
   provided by gRPC callback clients; it runs one fiber per activation with that
   activation's config (`07:1152`). A Go plugin whose `Inject` is within the callback set
   and that registers only `contrib` registries and remote-capable hooks is one `Plugin`
   value that passes the same suite in both runtimes (`07:1198`; plugin record §11). The
   plugin process is the gRPC server: the kernel dials it and never the reverse
   (`07:1158`).
9. **The kernel side of the protocol** (the dial, the handshake, the stream and the
   stubs) is one package of the module that holds `sdk/remote`. The proxy plugin and the
   check runner of `aiopsctl` both use it (`07:42`; `00:305`), so one implementation
   serves a server and `plugin check`.
10. **`plugin.FromManifest`** arrives in `sdk/plugin` with its first consumer, the proxy
    (`07:29`, `:232`; ADR-060 decision 4), with the signature that `07:232` sketches: a
    manifest and an `apply` function give a `kernel.Plugin`. It builds the plugin from
    one manifest file, whose `configSchema` is inline (ADR-048:242-245), so the `Inject`,
    `Hooks` and registrations of a proxy are that row's own and the hook dump and the
    static graph read them (`07:1140`). `sdk/plugin` imports the kernel and definitions only
    (`internal/vet/imports.go:199-202`), so the dial is in `apply`, which the proxy
    plugin supplies. The exported API of `sdk/plugin` changes, so `pluginApi` rises by a
    minor (ADR-060 decision 8). How a volatile change reaches a remote instance is
    question Q12 (ADR-034:1829-1833).
11. **The proxy** is the in-process fiber of an out-of-process plugin (`03:72`). It dials
    the instance, handshakes before the row is ACTIVE (`08:449`), registers the plugin's
    contributions with the dispatchers that serve their definition versions, carries the
    contribution calls and serves the callbacks on its own stream (`07:1149`, `:1165`).
    It refuses a remote `Register` that the manifest's `registers` does not list (ADR-048
    decision 21). A dispatcher accepts a registration only for a served major under the
    minor rule, the exact minor at major 0 (`07:1129`, `:548`). There is one proxy row per
    remote instance and one shared instance per plugin (`07:1140`, `:1194`; `08:690`;
    `02:19`). A proxy row states its roles like any row, and only replicas of those roles
    dial its instance (`03:571`). Which plugin id the row of an installed artifact names,
    how a proxy takes a bound key and what the `remote` row of `aiops-core` is, is
    question Q4; where its endpoint comes from is question Q2; its `tenantMint` inject is
    question Q3; its imports are question Q5.
12. **Boot and the loader resolve a remote row** in `plugins.lock` and the files install
    kept, not in the module table (`03:358`; `07:1853`). A plugin id that is neither in
    the table nor locked is a warning for an optional row and a stop for a required one
    (`08:468`; `07:1363`). `boot/` and `loader/` keep their imports (`08:1260`): the
    launcher hands them the function that turns a kept manifest into a plugin value. A
    remote plugin that is installed and enabled while the process runs mounts with no
    restart (`08:194`; `00:490`), so the loader takes a plugin value for an id it did not
    hold when it mounted, inside its cap of 1,300 lines. The generation digest already
    covers the lock digests (`00:482`; `boot/digest.go`).
13. **The adapter generator** writes, for each definition marked `contrib`, the remote
    adapter on both sides and the Python module of that definition and major (`08:697`;
    `07:1199`, `:1207`). Its output is committed, since `dist build` runs no `go generate`
    (plugin record §11), and a parity check in CI fails when a committed file differs
    from what the generator writes now. Only maintainers run it: a new definition is a
    core change. What it reads, and with which tools, is question Q8; where its Go output
    lives is question Q7. At S2b it supports the types the two toy registries use and
    nothing more (`08:680`). The definition lint gains the rule of `08:645`: a definition
    marked `contrib` has its proto and its adapter.
14. **The toy registries become `contrib`** as a minor of each, `0.2`, with the adapter
    generator (ADR-060 decision 5; plugin record §5). Whether `0.2` carries
    `compatibleFrom` is question Q11. Whoever raises the minor fixes every plugin under
    `plugins/`, `contrib/`, `examples/` and `templates/`, every fixture and the S2a exit
    in the same pull request (`08:678`, `:909`): step D1 of the S2a exit uses `echoers`
    `0.2` as its unserved minor (`test/authoring/exit_test.go:475-496`) and moves to a
    minor that stays unserved. The toy dispatcher `example-dispatch` gains the deadline,
    the breaker and the fallback that `07:783` gives every dispatcher, because the cell
    "while running" asserts them from S2b (`matrix.yaml:381-386`), and the guard half of
    each toy suite gains their cases, which raises the suite revision (ADR-060 decision
    10). Every `contrib` definition passes its suite through its remote adapter
    (community:109, G7): the obligations half runs on the proxy fiber as its subject, and
    the guard half never runs remote, since a remote plugin provides no key (`07:518`,
    `:1115`). That pass is also a condition of any later promotion of a `contrib`
    definition to `v1` (`08:910`).
15. **The gate.** `boot` stays one package and the home of the gate, and builds checks 3
    and 9 as the one gate text has them (`08:501`, `:507`). `serves` gains what the two
    checks read (question Q9). The entry-point table of ADR-060 decision 17 gains the two
    checks and one entry point, the remote handshake, as decision 16 says. Each golden
    that prints "not evaluated until S2b" is regenerated and reviewed in the commit that
    builds the check (the rule of ADR-060 decision 28).
16. **Where each clause of checks 3 and 9 runs.** Every entry point reports each clause
    it does not evaluate as "not evaluated" with the reason, and a denial names the check
    (ADR-060 decision 17). The handshake clauses need a connection, so admission cannot
    evaluate them. The proxy reaches the gate through the launch toolbox
    (`sdk/def/launch/v0/toolbox.go:31-33`), whose request gains the handshake inputs, as a
    minor of `launch` with a raised suite revision; the check runner of `plugin check`
    uses the same entry. Whether the proxy resolves it through `profileContext`, which
    also holds `Commit` (`dist/dist.yaml:36-47`), or through a narrower key is settled
    with that minor, under the binding rule of question Q4. No second copy of check 3
    exists outside `boot` (ADR-060 decision 16).

    | Entry point | Check 3 | Check 9 |
    |---|---|---|
    | `dist build` | Not evaluated: a build list holds no remote code | Not evaluated: the same reason |
    | `plugin check` step 7, `plugin pack`, `plugin gate --against` | The majors of `requires.remoteProtocol` against the served majors, with question Q9 (a). The digest and registration clauses: not evaluated, no connection | The contribution-set clause. The profile clause: not evaluated, no profile |
    | `plugin install` of remote code | As above | The contribution-set clause; the profile clause as question Q10 decides |
    | Boot, every live generation and `profile render` | As above, on every remote row | Both clauses, the profile clause as question Q10 decides |
    | The remote handshake: the proxy, and step 5 of `plugin check` | Every clause: the two lists of `Hello`; the digest of the first message against the lock; each definition version of the first message and of each `Register` against `serves` | Not evaluated: admission evaluated it |

17. **The operations library, the commands and the artifact.** The refusals of `remote`
    in `plugin check`, `pack` and `install` are lifted (ADR-060 decision 23).
    - `plugin check` runs T1 and T2 in every runtime the manifest lists (`07:134`,
      `:1577-1578`). For a remote plugin the runner starts the plugin as a local process
      and drives it over a Unix socket, with no Go toolchain for a non-Go plugin
      (`00:305`; plugin record §14). Step 2 compares with `plugingen` or `aiops-sdk
      manifest` (`07:1575`); `plugingen` writes `remoteProtocol` exactly when `runtimes`
      holds `remote` and `pluginApi` exactly when it holds `inproc` (ADR-048 decision
      12).
    - `plugin pack` writes the image reference by digest as one more layer type under
      the same `artifactType` (ADR-047:130-131; `08:393`), and `plugin sign`, `push`,
      `inspect` and `verify` handle the result. The attestation's subject is the digest
      of the artifact's OCI manifest, and the artifact's own field pins the image
      (ADR-020:326-330).
    - `plugin install` of remote code verifies the signature, the trust policy for this
      id and runtime, the gate and the attestation; runs no third-party code and pulls
      no image; writes the lock entry by digest, the kept files, the verified record and
      the bundle at the end of the profile's bundle list with its rows disabled; and
      renders a deployment unit. It needs no rebuild (`08:393`, `:398`; `00:480`;
      `07:1867-1868`). A denial writes nothing. A site holds one digest per plugin id
      (ADR-047 decision 7), and remote code is refused into the profile `cli`
      (`08:273`).
    - `aiopsctl plugin start <id>` starts the unit (ADR-061:1014-1016; `08:1292`). Which
      targets install renders is question Q13. The server never launches or stops a
      plugin process (`08:447`).
    - `plugin enable` refuses, printing the exact command, while the endpoint is missing
      or does not answer the handshake (`08:398`; `07:1870`; `02:326`). Enabling goes
      through the reload queue and is judged like any row (`03:374`; ADR-062 decision
      11).
    - `plugin disable` and `remove` revoke the instance's credentials first, then unload
      the proxy, and the operator removes the unit; `disable` run on the host also stops
      it (`03:495`; `07:1195`). Which credentials those are follows ADR-041 (`03:495`).
      When a shared unit stops, and what `remove` does with the files that install
      rendered, is reading 7 of question Q17.
    - `plugin verify` re-runs the suite against a pulled image, and `plugin smoke`
      attaches the plugin's image to the released image (plugin record §11; `07:1583`).
18. **The Python SDK `aiops-sdk`** ships at S2b with the parts of `07:1205-1213`: one
    module of base classes and message types per `contrib` definition and major,
    generated from its proto; `@contributes`; `@listens`; `aiops-sdk manifest`, which
    writes `plugin.yaml` from `plugin.toml` and the decorators with `requires` holding
    `remoteProtocol` and the definition versions; `serve()`; and the fixture
    `aiops_sdk.testing.host()`, which wraps the `aiopsctl` binary. A Python author needs
    `aiops-sdk` and `aiopsctl` and nothing else: no Go toolchain, no kernel checkout and,
    for T0 to T2, no container runtime (`07:51`, `:1236`). Its conformance is the Go
    suite run through the adapter, one suite per definition (`07:1246`). A Python plugin
    never writes `plugin.yaml` by hand, and CI fails on a difference (`07:148`, `:1210`;
    `08:1263`). ADR-041 fixes its scope (`07:1203`). What `@listens` and the callback
    clients are tested on at S2b is question Q14. The package lives in `python/` at the
    repository root, which `08:1207` leaves to S2b: under `sdk/` its files would enter
    the zip of the Go module `sdk` that every author downloads. It builds outside the
    tree, and no wheel and no build output is committed (`scripts/ci/hygiene.sh` refuses
    a tracked executable).
19. **The templates.** `aiopsctl plugin new <registry> --lang python` writes a skeleton
    with its `plugin.toml`, a test and a row snippet, for each registry marked `contrib`,
    and it fails conformance with one line per obligation until the obligations are
    written (`07:1213`, `:1556`; ADR-061:934). The template job generates a plugin from
    every template, the Python ones included, and asserts that state (`07:1568`;
    `08:1269`). The form of a remote Go plugin from `plugin new`, and what writes its
    `main`, is settled in the S2b addition of ADR-061.
20. **The failure matrix.** The five cells marked `since: S2b` pass on the built binary
    as `matrix.yaml` states them: "Remote unreachable or handshake mismatch" in its four
    process columns, and the live-recomposition cell of "A tenant-scope disposer passes
    its deadline (a remote that ignores `Deactivate`)" (`08:477`, `:486`). The harness
    runs every cell whose step is S1 or S2b. The two stubs, one that hangs a call and one
    that ignores `Deactivate` (`08:463`), are built for it. A remote plugin provides no
    key, so its loss changes no other fiber's epoch; a short disconnect fails fast behind
    the breaker; an exhausted reconnect budget unloads the proxy and unwinds every
    registration; and a proxy disposer never waits on the remote beyond a bound shorter
    than the dispose deadline (`07:1196`; `03:493`).
21. **The remote-plugin container** is the plugin's own image, in any language, optional
    in the profiles S, A and D, one shared instance per plugin for the tenant `default`
    (`00:138`; `02:191`, `:336`). The S2a and S2b exits install into `dev` (`08:689`).
    An operator keeps an instance off the home and off the reload socket by running it
    as another user than the site user (ADR-062:224-234); which user a rendered unit
    names is settled in the S2b addition of ADR-047.

### What waits, and why

22. The parts below are not built at S2b.

| Part | Step | Why |
|---|---|---|
| One instance per (tenant, plugin); activations across replicas; the test provider of `sidecars` with a test CA; the tenant-tree column of the remote cells | S4b | `08:632`, `:666`; `00:575`; `03:20`; ADR-041 is ratified there for one tenant per connection |
| The bound view of `sidecars`, `tenant-handle` and the tenant handle | S4a | `08:699`; questions Q2 and Q3 |
| The policy callbacks; a notifier as remote Go and remote Python; the `notifiers` adapter; scrubbed payloads for remote listeners | S5 | `08:700`; `00:74`; ADR-041 is ratified there for the policy callbacks |
| `kv`, `blob` and `secrets` as callbacks | S3 | Their definitions are S3 (`08:698`), and a part lands with its first consumer (`08:680`) |
| The `tools` adapter and callback, the remote reference harness, revocation of the virtual key | S8a | `08:629`; `07:1178`, `:1182` |
| A Python tool installed as a remote plugin | S8b | `08:630` |
| The inventory that flags an instance that still answers after revocation; the index | S6a | `07:1195`; `00:574` |
| Quarantine and cohort update of remote versions; `plugin update` | S6b | `08:633`; ADR-047:313 |
| `sidecars-kubernetes` and the cell's plugin CA | S9b | `03:490`, `:495` |
| The Helm form of the unit, with question Q13 (a) | S7 and later | `deploy/helm` does not exist yet (ADR-060 decision 22) |
| Loopback shims in the SDKs | A trigger | `00:632`; `07:1190` |
| `wasm`; third-party plugins on hosted; a reader of the reserved first-message field | The enterprise edition | `00:628`; `07:1104`; `08:948`, `:954-955` |
| Remote protocol major 1 and the served previous major | Platform 1.0 | `08:921` |
| The public package index of `aiops-sdk` | When L14 clears | `08:1348` |

### The exit test, made checkable

23. **The exit test is `test/remote/`**, run by the CI job `s2b-exit` and by `make
    s2b-exit`. It reuses the harness of `test/authoring/` (ADR-060 decision 30): it first
    builds what a release pipeline would build, with throwaway keys, and publishes
    nothing outside the test (ADR-057 decision 5; `08:1348` L14). It then drives two
    environments, each a set of containers on an internal network of its own. Neither
    holds a repository checkout, a `replace` line or a module cache of the repository.
    - The pipeline of the test serves the root and SDK modules, the remote modules of
      question Q6 included, from the module proxy inside the job; builds the test release
      and its `aiopsctl`; builds the `aiops-sdk` package and puts it, with the pinned
      packages of its dependency set, in a package directory inside the job; and runs the
      pinned registry. Every input from outside the job is fetched here, by version and
      checksum or by digest, before an environment starts.
    - The author's repository is the one of the S2a exit (`08:697`: "the same
      repository"). It holds a Go toolchain, the `aiopsctl` of the test release, a Python
      interpreter and a container runtime, and it reaches the module proxy, the package
      directory and the registry and nothing else. The Python steps run with a `PATH`
      that holds no Go toolchain (`07:51`).
    - The clean machine holds `aiopsctl`, a container runtime and what `plugin start`
      needs, and no Go toolchain and no Python. Its kernel runs in the container form of
      ADR-047 decision 15 on a network that reaches the two instances and nothing else.

    Each step passes only as stated:
    - **G1.** In the author's repository a remote Go echoer is made from the template.
      Its `Plugin` value is the one of step A2 of the S2a exit, unchanged, and its
      manifest lists `remote`. `plugin check --write` writes a `requires` that holds
      `remoteProtocol: [0]` and `echoers` at the served version. `plugin check` passes
      steps 1 to 5 and 7, with steps 4 and 5 run in the remote runtime, and its `--json`
      report is kept.
    - **G2.** `plugin pack`, `sign` and `push` publish one artifact to the test registry.
      It carries the image reference by digest, a bundle of one disabled row and an
      attestation that names the artifact digest and `echoers` at the served version
      with its suite revision and the half that ran.
    - **P1.** `aiopsctl plugin new stampers --lang python` writes the skeleton. `aiops-sdk
      manifest` writes its `plugin.yaml`. `plugin check` fails with exactly one line per
      obligation of the `stampers` suite.
    - **P2.** With the stubs filled in from the test's fixture, `plugin check` passes, and
      the plugin's own test passes the suite through `aiops_sdk.testing.host()`. `plugin
      pack`, `sign` and `push` publish it as G2 does.
    - **C1.** On the clean machine `trust add --runtimes remote` lists the author for its
      ids, and `plugin inspect` prints each spec's rows, configs and image reference and
      fetches no image.
    - **C2.** `plugin install <spec> --profile dev` of each writes `plugins.lock` by
      digest, the verified record and the unit, and appends the bundle with its row
      disabled. The clean machine runs the image of the test release: no install answers
      `rebuild-required`, no `dist build` runs, and the kernel that serves step C4 is the
      process that booted before step C2. `plugin enable` before the instance runs
      refuses and prints the exact command.
    - **C3.** `plugin start` starts each instance. `plugin enable` enables each row while
      the kernel runs; its final result is `passed`; the `teststate` row reports every
      row of the composition ACTIVE, in a generation later than the one the process
      booted with and in the same process.
    - **C4.** Each is called: the toy caller calls `echoers` and `stampers` through the
      dispatcher, and each reply is the one that the instance's code fixes.
    - **C5.** The composition holds no policy row and no tenant row (`08:702`).
    - **D1.** A copy of the Go echoer that offers only protocol major 7: the pipeline
      serves a second version of the remote modules in which the one list of spoken
      majors holds 7 alone, and the copy is built against it. Its manifest says so, and
      it is attested and signed by the trusted author. With question Q9 (a) it is denied
      at `plugin gate --against` and at `plugin install`, each denial names check 3 and
      no other evaluated check, and no site file changes.
    - **D2.** The same image behind a manifest that lists major 0, made without `plugin
      check` and attested and signed by the trusted author, so that install admits it.
      Once its instance is started, `plugin enable` does not end in `passed`: the plugin
      is refused at the handshake, the log of the dialling side and the log of the
      instance each hold both lists of majors, the dialling side closes the stream, the
      denial names check 3, the row is not ACTIVE and the site files are as they were
      before the command (`07:1127`; `08:398`, `:477`).

    The exit texts of `08:623`, `08:697`, `00:573`, `01:290`, community:60 and plugin
    record §16 are edited to one text with the answer to question Q1.
24. **The other refusals are cases, not exit steps.** Each has a test that fails on its
    base: a first message whose manifest digest differs from the lock, and one that
    names a definition version that is not served, each denied with check 3; a `Register`
    outside the manifest's `registers`; a `Register` from a superseded activation; a
    remote manifest that declares a `provides` entry, an unmarked registry or an
    unmarked hook, denied with check 9 at `plugin check`, at `plugin gate --against`, at
    install and at boot; an artifact whose signer is not listed for `remote`; a remote
    artifact under `aiops/` or `contrib/` whose signer is not a release key (`08:503`);
    install into `cli`; install of a second digest of one id; `serve()` of the Python SDK
    against a kernel that offers no common major, which logs both lists; and the fixture
    echoers of the S2a and S2b exits holding one `Plugin` value that passes the `echoers`
    suite in both runtimes (`07:1198`).

### Working rules

25. **Test first.** The acceptance test of each deliverable is committed, failing, before
    the code that makes it pass, and every fix lands with a test that fails on its base.
    The S2a report records this rule as not met for part of its history, with commits
    that fail a check (s2a-report:58, :456). At S2b a commit that adds a failing test
    therefore marks it as expected to fail, in a way that `make check` reports and
    counts, and the commit that makes it pass removes the mark, so no commit on the
    branch fails `make check`.
26. **Review rounds.** An adversarial review round runs on each finished group and once
    more on the whole step, on the commit that the exit report names. Each finding is
    confirmed by a reproduction before it is fixed. A group merges into `main` only when
    its round leaves no open finding. The groups: (1) the protocol, the modules, the
    adapter generator and the `contrib` minor of the toy registries; (2) `FromManifest`,
    the proxy, the resolution of remote rows and gate checks 3 and 9; (3) the operations
    library, the artifact, the unit and the commands; (4) the Python SDK and its
    template; (5) the failure-matrix cells and the exit test.
27. **The standing rules of `08:678` apply as at S2a** (ADR-060 decision 32, items 1 to
    4). `make plugin-check` and CI run `plugin check` and a T3 boot on every plugin under
    `plugins/` and `examples/`, the remote examples in Go and in Python included; the T3
    of a remote example starts its instance as the test's own process, since the kernel
    launches none. Nothing outside the closed vocabularies merges into `sdk/kernel`,
    `boot/` or `loader/`.
28. **Branch, commits, report.** S2b work lands on the branch `s2b/integration` and
    reaches `main` by merge. Commits follow Conventional Commits. Nothing is published
    outside the private repository: the exit's registry, module proxy, package
    directory, images and keys exist only inside the test. S2b closes on the owner's
    approval of an exit report, `docs/design/s2b-report.md`, in the form of the S2a
    report: every decision of this ADR with its status and its evidence.
29. **A written requirement is never weakened.** Where the work finds that a line of the
    documents cannot be built as written, the work stops on that point and the owner is
    asked, with the lines the answer would edit; a note "as built" never stands in for
    the answer.
30. **What each ADR and addition settles**, at least:
    - ADR-041, provisional: the messages of `aiops.remote.v0` and which side sends each
      field of the first message, since `07:1128` lists fields the plugin knows beside
      the activation id, which the kernel assigns; how the plugin learns the chosen
      major; where the call that carries `Hello` is declared, given that the frozen file
      takes no service and that a plugin of another major must still answer it; whether
      contribution calls and callbacks are messages on the one stream of `07:1151` or a
      service per definition; the encoding of errors across the boundary
      (`ErrInactiveEffect`, a cancelled context, `ErrInactive`, a refused registration);
      which digest the first message carries and what the kernel compares it with, given
      ADR-048 decision 1 and ADR-047 decision 8; the callback set, which holds `tools`
      (`08:697`); the deadlines, with the handshake deadline of 2 s against a running
      instance (plugin record §10; ADR-036:30), the bound of the proxy disposer, the
      deadline of a remote listener on a gate hook and the reconnect budget, and what
      brings a proxy back after it unloaded itself; the identity of the instance and of
      the kernel on a single-tenant self-host, its issuer, where the key material lives
      under ADR-047 decision 15 and ADR-062:224-234, and what disable and remove revoke
      where no leader exists (`03:495`), with the new credential classes written into
      `00` §5.4 in the same change (`00:410-411`; `08:999`); the dial of the proxy under
      the lists of ADR-062 decisions 13 and 14; and the scope of the Python SDK: the
      supported interpreter versions, the concurrency model of `serve()`, the call
      context, the fixture and the command of `aiopsctl` it wraps, the version scheme
      and the dependency pins.
    - ADR-047, its S2b part: the layer type and media type of the image reference; who
      builds and pushes the image and how `plugin pack` learns its digest; the SBOM of
      remote code; the rows a remote artifact's bundle may hold; the unit files, the
      endpoint and the credentials as rows of the home table of decision 13; and the
      network of a kernel container (decision 15).
    - ADR-048, its S2b part (decision 20 there): every `plugin.toml` key, whether
      `plugin.yaml` carries `entrypoint`, so that `aiopsctl` needs no reader of
      `plugin.toml`, how `@contributes` names a key, and where `configSchema`, `uses`
      and hook stages come from in Python; the constant of spoken majors that
      `plugingen` reads; what a manifest that lists `inproc` and `remote` packs and
      installs; and which fields the contribution-set clause of check 9 reads beyond
      those of ADR-048:433 (`durable`, `capabilities`, an `as: want` entry).
    - ADR-020, its S2b part: the record of a remote artifact, which has no `module`
      (decision 10); whether the attestation names the runtimes its suites ran in;
      `plugin verify` against a pulled image; and the `signed` clause for a remote
      listener on a hook with a raw field (decision 7).
    - ADR-061, its S2b part: `plugin start`, with `Start` in the operations definition
      as a minor with a raised suite revision; `plugin new --lang python`; the form of a
      remote Go plugin; and the text of the refusal of `plugin enable`.
    - ADR-054, its S2b part: the register entries and importers of question Q5; the
      forms of an entry for a Python package, an interpreter and a plugin base image;
      the names that the name lint would refuse under `sdk/` and in a proto, as the
      names of the Python language, of gRPC and of Protocol Buffers; and the checks over
      the dependency set of `aiops-sdk` that question Q15 decides.
    - ADR-044, its S2b part: the release modules the builder serves, the suite binaries
      that drive a remote process, the writer of the new `serves` fields, and the gRPC
      codec exemption read again if the pin of `google.golang.org/grpc` moves (ADR-020
      decision 27).
    - ADR-034: G13 point 10, as question Q12 decides.

### The licence rule for the proto toolchain and the Python dependencies

31. **Nothing merges unread.** A new linked module, baked-in tool, image or data set
    merges only with its licence read at a primary source at the pinned version and
    entered in the licence record; its transitive licences scanned on every shipped
    artifact that links it; an entry in the upstream register; and, for anything linked,
    a licence on the allow-list of L7 (ADR-060 decision 34; `08:1337`, `:1341`). The
    gRPC runtime of the Python SDK blocks until read (`08:1347` L13; licence:68). A tool
    that CI runs is fetched by pinned version and checksum or image digest, is never
    copied into the repository and is baked into no image of the project (`08:1374`). What
    follows was read on 2026-11-27 for this ADR. It prepares the entries and replaces
    none: the licence-record row and the register entry of each are written, with a new
    reading if the pin moved, in the commit that first uses it.

    | Component, pin and use | Where read | What it holds |
    |---|---|---|
    | `google.golang.org/grpc` v1.82.1 (`h1:NnAxzGRA0677vCa4BUkOAnO5+FfQqVl9iUXeD0IqcGE=`); linked | `LICENSE` and `NOTICE.txt` of the module, as `go mod download -json` places it | Apache-2.0; the notice reads "Copyright 2014 gRPC authors" |
    | `google.golang.org/protobuf` v1.36.11 (`h1:fV6ZwhNocDyBLK0dj+fg8ektcVegBBuEolpbTQyBNVE=`); linked, and its command `cmd/protoc-gen-go` as a generator | `LICENSE` and `PATENTS` of the module | BSD-3-Clause, "Copyright (c) 2018 The Go Authors", with a patent grant |
    | `google.golang.org/grpc/cmd/protoc-gen-go-grpc` v1.6.1 (`h1:/WILD1UcXj/ujCxgoL/DvRgt2CP3txG8+FwkUbb9110=`); a candidate generator | `LICENSE` of the module | Apache-2.0, with no copyright line of its own; its `go.mod` requires grpc v1.70.0 and protobuf v1.36.11 |
    | `github.com/bufbuild/protocompile` v0.14.1 (`h1:iA73zAf/fyljNjQKwYzUHD6AD4R8KMasmwa/FBatYVw=`); a candidate compiler | `LICENSE` of the module | Apache-2.0, "Copyright 2020-2024 Buf Technologies, Inc." |
    | `connectrpc.com/connect` v1.20.0 (`h1:6TNDAB+WeNd2uolWNlYczB5E0KNNaVMNUEx8JEUsPmQ=`); the other library that `00:44` and licence:48 name | `LICENSE` of the module | Apache-2.0, "Copyright 2021-2025 The Connect Authors" |
    | `grpcio` 1.85.4, wheel `cp312` `manylinux2014_x86_64`, SHA-256 `f279c333de857630e12ce2751672cb0825d1df2f6f2ec209d0d40f86a26e4ac3`; the gRPC runtime of the Python SDK | `METADATA` and `licenses/LICENSE` inside the wheel | `License-Expression: Apache-2.0`. The one licence file holds three texts: Apache-2.0, a BSD 3-Clause text ("Copyright 2016, Google Inc.") and MPL-2.0 for `./etc/roots.pem`, which the wheel ships as `grpc/_cython/_credentials/roots.pem`. It requires `typing-extensions~=4.12` and Python 3.10 or later. Its native extension is built from libraries of other projects whose licence texts the wheel does not carry; they are not read |
    | `typing-extensions` 4.16.0, wheel SHA-256 `481caa481374e813c1b176ada14e97f1f67a4539ce9cfeb3f350d78d6370c2e8`; required by `grpcio` | `METADATA` and `licenses/LICENSE` inside the wheel | `License-Expression: PSF-2.0`; the file is the Python licence stack (PSF version 2, BeOpen, CNRI, CWI and Zero-Clause BSD). PSF-2.0 is not on the list of L7 |
    | `protobuf` 7.36.2, wheel `cp310-abi3` `manylinux2014_x86_64`, SHA-256 `89f23aa53c24553a2416fd4fd1ec06f74fa42b14b546d8883128813f775bbfd2`; the message runtime of the Python SDK | `METADATA` and `LICENSE` inside the wheel | "3-Clause BSD License", "Copyright 2008 Google Inc."; it holds a native extension, whose bundled parts are not read. It requires Python 3.10 or later |
    | `grpcio-tools` 1.85.4; a candidate generator | The package index metadata only | Declared Apache-2.0; it requires `protobuf` from 7.35.1 below 8, `grpcio` and `setuptools`. Its wheel, which holds a proto compiler, is not read |

    Not read, and each read before its first use: the libraries inside the native
    extensions of `grpcio` and `protobuf`, in the source tree of each at its tag; the
    Python interpreter and the form in which CI and the exit fetch it; the base image of
    a plugin image that a template names; the build backend of `aiops-sdk`; a test
    runner, if the fixture depends on one; a CI action that sets up Python; and Compose,
    which licence:60 lists with no pin. grpc-go and protobuf-go are in the build list
    today only inside the `contains` of the sigstore-go entry
    (`tools/upstream-components.yaml:278-279`); a module that a package of the repository
    imports needs an entry of its own (ADR-054 decision 18), with the importers that
    question Q5 decides, and the scan of each artifact runs again on the packages the
    direct import adds. Remote plugins are separate programs and their own licences are
    unrestricted (`08:1350` L16); whether the dependency set of `aiops-sdk`, a
    first-party artifact (`08:715`), is held to L7 is question Q15.

### Line budgets

32. Physical lines of production Go, measured by `tools/kernelcheck lines` and enforced in
    CI and the Makefile from each package's first commit, under the rules of ADR-060
    decision 35: growth past a provisional cap before the exit needs the owner's word,
    and at the S2b exit each cap becomes the measured count plus ten per cent, rounded up
    to the next hundred, with this ADR, `ci.yml` and the Makefile changed together. The
    base of each grown package is its S2a exit cap, which the owner settles at the S2a
    close (s2a-report:409-453); the figures below are estimates and are proposed with
    this ADR. Every package the table does not name keeps its S2a exit cap. Whether
    generated files count is question Q16. The hand-written Python of `aiops-sdk` gets a
    provisional cap of 2,500 lines, counted by the Python job, under the same exit rule.

| Package | Measured at `50665fa` | S2a exit cap, proposed | Cap during S2b |
|---|---|---|---|
| `sdk/kernel` | 8,175 | 8,200 | 8,200, unchanged (ADR-034) |
| `loader` | 1,104 | 1,300 | 1,300, unchanged |
| `boot` | 9,777 | 10,800 | 11,800, provisional |
| `sdk/plugin` | 1,183 | 1,400 | 1,800, provisional |
| `internal/plugingen` | 970 | 1,100 | 1,400, provisional |
| `internal/builder` | 3,497 | 3,900 | 4,400, provisional |
| `plugins/operations/internal/operations` | 4,430 | 4,900 | 6,200, provisional |
| `sdk/def/operations/v0`, its fake, its suite | 746, 1,055, 1,094 | 900, 1,200, 1,300 | 1,100, 1,500, 1,600, provisional |
| `plugins/command-plugin` | 466 | 600 | 800, provisional |
| `internal/authoring` | 827 | 1,000 | 1,500, provisional |
| `internal/packsuite` | 310 | 400 | 800, provisional |
| `templates` | 121 | 200 | 300, provisional |
| each toy definition package; its fake; its suite | 67 and 66; 175; 428 | 100; 200; 500 | 100; 300; 700, provisional |
| the kernel side of the protocol, hand-written (decision 9) | new | | 1,500, provisional |
| `sdk/remotehost`, hand-written | new | | 2,000, provisional |
| the proxy plugin, with its internal package | new | | 1,500, provisional |
| the adapter generator | new | | 1,500, provisional |
| with question Q2 (a): `sdk/def/sidecars/v0`, its fake, its suite; `plugins/sidecars-static` | new | | 800 each; 1,000, provisional |

The coverage gate of 100 per cent stays on `sdk/kernel`, `boot` and `loader` (`00:305`),
so it covers checks 3 and 9 and the resolution of remote rows.

### CI jobs

33. Added to `.github/workflows/ci.yml` and mirrored in the Makefile, as at S2a (ADR-060
    decisions 36 and 52): the adapter parity check of decision 13, for the generated Go
    and the generated Python; the handshake freeze test, at the path question Q6 gives
    it; the require list of each new module and its served `go.mod` copy; the definition
    lint with the `contrib` rule; the Python job, on a pinned interpreter, which runs
    the tests of `aiops-sdk`, the fixture against the built `aiopsctl`, the manifest
    parity of every Python plugin of the repository and the Python line count; the
    template job with the Python templates; `plugin check` and the T3 boot on the remote
    examples; the S2b cells of the failure matrix, in the `test` job; the cases of
    decision 24; the register check with the new entries; the caps of decision 32; and
    the job `s2b-exit`, whose Go binaries are race-built and which fails on a race
    report or a skip. A case that needs the suite binaries of a release build runs on
    the `aiopsctl` of the test release, as the connector case of the templates does
    (ADR-060 decision 24). None pushes, publishes or holds a long-lived key. The exit
    jobs need what the S2a exit needs (s2a-report:471-472) and, in the pipeline part,
    the network to fetch the pinned inputs of decision 23.

## Consequences

- The S2b exit is decidable: eleven steps in two environments, two denials of check 3,
  one before install and one at the handshake, the cases of decision 24 and five matrix
  cells.
- `boot` gains checks 3 and 9, the resolution of remote rows and the new `serves`
  fields, and stays one package. The gate part of every dump changes from "not evaluated
  until S2b" to the evaluated checks, and each golden is regenerated and reviewed in the
  commit that makes the change.
- gRPC and Protocol Buffers become direct imports of the repository, in the places
  question Q5 allows; the builder already builds with `grpcnotrace` and exempts the gRPC
  codec at v1.82.1 (ADR-044 decision 21).
- The SDK gains modules (question Q6), and every count of the SDK modules in `00`, `07`,
  `08`, ADR-060 and the builder changes with the answer.
- The one point where a kernel change may prove necessary is named: how a proxy built
  after boot takes a bound key (question Q4). It is gated by decision 3 and has 25 lines.
- `echoers` and `stampers` move to `0.2`; every manifest, fixture and golden that names
  `0.1` is regenerated in that pull request, and the S2a exit stays green.
- Python enters the repository and CI: a directory, a job, a pinned interpreter and
  register entries of new forms.
- `launch` and `operations` each gain a minor with a raised suite revision, and
  `pluginApi` a minor.
- ADR-041 is written, provisional, and each S2b addition of decision 30 is accepted
  before its code merges.
- On acceptance this ADR enters the ADR table of `08` §3 and the next free number there
  becomes ADR-064. A proposed ADR is not listed there.

## Questions for the owner

Each question gives its options, the recommended one, why, and what the answer blocks.

**Q1. What is the exit text?** `08:697` and community:60 have "with the reason logged on
both sides"; `00:573` does not, and `08:3` lets `00` win. `01:290` has neither "calls
each" nor the refusal. community:60 and plugin record §16 say "a remote Go echoer and a
remote Python stamper", and plugin record §16 adds "into `dev`" and "No policy service
and no tenant data are involved".
- (a) The strictest text everywhere: "The same repository publishes a remote Go echoer
  and a remote Python stamper. A clean machine installs them into `dev` from signed
  artifacts with no rebuild and calls each through the toy caller. A plugin that offers
  only an unknown protocol major is refused at the handshake, with the reason logged on
  both sides. No policy service and no tenant data are involved." `00:573`, `01:290`,
  `08:623` and `08:697` gain what they lack.
- (b) As (a), and each language contributes to both registries, so four plugins are
  published and called.
- (c) Follow `00`: drop "with the reason logged on both sides" from `08` and the records.
- Recommended: (a). No written clause is lost, each toy registry passes its suite through
  its adapter once (community:109), and the template job of decision 19 covers both
  registries in both languages. (c) weakens a written exit. Blocks: any S2b code.

**Q2. Where does the proxy's endpoint come from before S4a?** The proxy injects
`sidecars` (`03:72`), takes its endpoint from it and is PENDING until `Endpoint(name)` is
available (`07:1150`, `:1158`; `03:113`), and `plugin enable` and the compose unit name
the `sidecars-static` table (`08:398`, `:1292`). The matrix cell for an optional row at
boot expects PENDING (`matrix.yaml:361-365`). `sidecars` and `sidecars-static` are S4a
(`08:699`; `02:215`), and a definition lands with its first consumer (`00:206`;
`08:680`). `08:646` says `sidecars-static` needs `sql`, `secrets` and `durable`; `03:113`
says it injects none.
- (a) The definition `sidecars`, in its process form, with its fake, its suite and its
  `definition.yaml`, and the provider `sidecars-static`, which reads the endpoint table
  that install writes and injects nothing (`03:113`), move to S2b, where `remote` is the
  first consumer. The bound view, the leader and `sidecars-kubernetes` stay where they
  are. `08:699`, `00:573` and `08:646` are edited.
- (b) Install writes the endpoint into the config of the proxy row, and the proxy tracks
  availability itself until S4a. `03:72`, `07:1150`, `07:1158` and `08:398` gain "from
  S4a", and the proxy is rewritten at S4a.
- (c) S2b waits for S4a.
- Recommended: (a). It builds the texts as written, it is the rule that a definition
  lands with its first consumer, and nothing is rewritten at S4a. (b) weakens four
  written lines and (c) breaks the order of `08:609`. Blocks: the proxy, the unit that
  install renders, the refusal of `plugin enable` and the matrix cells; needed before
  any S2b code.

**Q3. Does the proxy inject `tenantMint` at S2b?** `03:72` lists `tenantMint` (C) among
the proxy's injects, and `00:330` lists `remote` among the mint points. Its provider
`tenant-handle` and the handle are S4a (`08:699`), and the S2b exit holds no tenant data
(`08:702`; plugin record §16).
- (a) The proxy injects no `tenantMint` at S2b and gains it at S4a with the handle, as
  the two-tenant tests wait for S4a (ADR-060 decision 43). `03:72` gains "from S4a" for
  that inject.
- (b) `tenants.Handle`, `tenantMint` and `tenant-handle` move to S2b, and the proxy mints
  the tenant `default` for every call.
- Recommended: (a). No toy definition takes a tenant, so a mint at S2b would have no
  reader, which `08:680` rules out. Blocks: the proxy.

**Q4. Which plugin does the row of an installed remote artifact name, and what is the
`remote` row of `aiops-core`?** The bundle of an installed artifact may insert only rows
of its own plugin or of a first-party plugin marked `instantiable` (`08:393`), and boot
resolves a remote plugin in `plugins.lock` (`03:358`). `07:1140` and `03:72` call the
proxy an `aiops/remote` row, one per remote instance, built from the remote's
`plugin.yaml`, with `Inject`, `Hooks` and registrations of its own; `00:276` and `08:273`
count `remote` among the eleven rows of `aiops-core`, whose bundle file cannot list a
row per installed plugin. The proxy also needs bound keys, and the binding lists name
plugins of the module table and are compiled once, when the kernel starts
(`sdk/kernel/root.go:89-97`; `00:330` lists `remote` among the mint points).
- (a) The row names the artifact's own id and is resolved through the lock. `aiops/remote`
  is the first-party plugin of the module table whose code is the proxy: one plugin value
  per locked remote id is built with `FromManifest` and that code, and for the binding
  lists each such value counts as `aiops/remote`, which the lists name once. The bundle
  `aiops-core` holds no row of it. `00:276`, `08:273` and `03:72` say that `remote` is a
  plugin whose rows come from installed artifacts, and the lines that say "an
  `aiops/remote` row" say "a proxy row". If a value built after boot cannot take the
  bindings of an entry without a kernel change, that change is an ADR-034 addition under
  decision 3.
- (b) Every such row is a row of the plugin `aiops/remote`, which joins the instantiable
  list (`07:132`; `08:393`), with the artifact's id in its config, as `07:1140` words it.
  One plugin id then resolves to a different value per row, the gate reads two manifests
  for one row, and "its own plugin" of check 8 no longer covers a remote artifact.
- (c) Both: `aiops-core` holds one `remote` row that provides the dial under a new
  consumer-bound key, and each installed row names its own id and needs that key. This
  adds a definition that no document names.
- Recommended: (a). It is what `03:358` and check 8 say, the dump shows each remote row
  under its own id and origin, and the third party's id enters no binding list. Its open
  point, how a later value takes the bindings, is named and gated. Blocks: the proxy,
  `FromManifest`, the resolution of remote rows and install; needed before any S2b code.

**Q5. Where may gRPC and Protocol Buffers be imported and named?** The remote runtime is
"remote over gRPC" by decision (`00:44`; `07:1104`). An upstream's name and module appear
nowhere in `sdk/`, in a proto or in a row of `aiops-core` outside the stated exceptions
(`00:268`; `07:50`), which do not hold gRPC or Protocol Buffers; no row of `aiops-core`
imports an upstream module (ADR-054 decision 23); and an importer list never holds a
definition with its fake and suite (ADR-054 decision 17).
- (a) `00:268` gains one stated exception: gRPC and Protocol Buffers, the wire of the
  remote protocol, among the protocols with several implementations. The register gains
  entries for grpc-go and protobuf-go whose importers are the remote modules of question
  Q6, the proxy plugin, the adapter generator and the check runner of `aiopsctl`. With
  question Q4 (a) the bundle `aiops-core` holds no row of the proxy, so ADR-054 decision
  23 stands as written. No definition package imports either (question Q7).
- (b) As (a), with `remote` kept as a row of `aiops-core` and ADR-054 decision 23
  amended to except it.
- (c) The proxy speaks through a neutral stream definition, and a provider row outside
  `aiops-core` wraps gRPC, as decision 23 does for the telemetry libraries.
- Recommended: (a). The exception names a contract that `00` already decided, and both
  rules of ADR-054 stay whole. (c) adds a definition whose only purpose is to hide a
  protocol that the proto packages already name. Blocks: the first import of either
  module, so any protocol code.

**Q6. How many modules hold the remote packages, and where?** R2 keeps both packages out
of the require list of `sdk` and fixes their home at S2b (`07:60`); ADR-060 decision 54
says they need a nested module. Generated Go for the handshake imports protobuf-go, so
the handshake package leaves the module `sdk`, and its freeze step moves
(`.github/workflows/ci.yml:86-90`; `Makefile:161`).
- (a) Two nested modules, rooted at `sdk/remote` and at `sdk/remotehost`, each with a
  closed require list. Every written path stays (`07:32`; `08:1218`). `00:263` and
  `08:1205` say five SDK modules, `07:51` names the two for the author of a remote Go
  plugin, and the builder serves six modules.
- (b) One nested module rooted at `sdk/remote`, with the plugin-side host at
  `sdk/remote/host`. `07:32`, `08:1207`, `08:1218` and plugin record §3 change the path.
- (c) The module `sdk` gains protobuf-go and grpc-go in its require list. This edits
  `00:263`, R2, ADR-054 decision 13 and ADR-060 decision 34, and every author's module
  graph gains both.
- Recommended: (a). It keeps every written path and the closed list of `sdk`, and an
  in-process author's graph gains nothing. Blocks: the first generated Go file.

**Q7. Where does the generated adapter of a `contrib` definition live?** `07:28`,
`07:578` and `08:1215` put the proto, the message types and the generated adapter in
`sdk/def/<name>/v<N>`. That directory is in the module `sdk`, which takes no protobuf
dependency (ADR-060 decision 34; `07:60` R2), and a definition imports no upstream module
(`07:527`), which the import boundary enforces (`internal/vet/imports.go:195-198`).
- (a) The proto, which imports nothing, stays beside the definition, where `00:44` and
  `08:1255` have it written. The generated Go, wire types and adapter, lives in the
  remote module, under `sdk/remote/def/<name>/v<N>`. The Go types of a definition stay
  plain Go, and the adapter converts. `07:28`, `07:578` and `08:1215` are edited.
- (b) The generator writes plain Go with a wire codec of its own into the definition
  package, with no protobuf import, and gRPC stays in the remote module. `07:28` stands;
  the repository then maintains an encoder of the protobuf wire format.
- (c) The definition packages import protobuf-go, by an owner exception to `00:263`,
  `00:268` and ADR-054 decision 13.
- Recommended: (a). It keeps the module rule and the upstream rule, and it uses the
  standard runtime, not a private encoder. Blocks: the adapter generator and the
  `contrib` mark of the toy registries.

**Q8. What is the source of an adapter, and which tools generate it?** `07:1199` says the
proto and the adapter are generated; `00:44`, `07:578` and `08:1255` say the proto is
written with the definition and the message types are generated from it. No document
names a compiler. The frozen handshake file takes no `go_package` option.
- (a) The proto is written by hand with the definition. One proto compiler produces the
  Go and the Python wire code from it: the compiler inside the pinned `grpcio-tools`,
  with `protoc-gen-go` of protobuf-go at the pin the build list already has and
  `protoc-gen-go-grpc`. An in-repository generator writes the adapter glue between the
  Go interface and the wire code and the Python base classes. All of it runs only in the
  parity job and on a maintainer's machine.
- (b) As (a) for the sources, with the Go compiler `protocompile` for the Go side and
  `grpcio-tools` for the Python side only.
- (c) The Go interface is the source: the generator reads it and writes the proto, the
  adapters and the Python modules with its own encoder (question Q7 (b)).
- Recommended: (a). It matches the three lines that have the proto written, both
  languages come from one compiler run on one file, and the only new Go module is a
  generator. Each tool is read under decision 31 before use. Blocks: the adapter
  generator.

**Q9. Does check 3 have a clause that needs no connection, and what does `serves`
gain?** The gate text names the handshake only (`08:501`); ADR-048:428 has check 3 read
`requires.remoteProtocol`; `serves` has the shape of `requires` (`08:904`; `00:267`) but
carries no protocol majors and no `remote` mark of a definition, and its reader refuses
an unknown field (`boot/distribution.go:241-242`).
- (a) `serves` gains `remoteProtocol`, the majors the binary speaks, and per served
  definition its `remote` mark. Check, pack, the offline gate, install and boot deny a
  manifest whose `requires.remoteProtocol` shares no major with it; the handshake
  evaluates every clause (decision 16). The exit then holds both denials, D1 and D2. The
  reader and the builder change in one commit; `distVersion` stays 1, since no
  distribution manifest exists outside the tests of the repository.
- (b) Check 3 runs at the handshake only. Every other entry point reports it "not
  evaluated: no connection", and `serves` gains only the `remote` mark that check 9
  needs.
- Recommended: (a). A plugin that cannot connect is refused before an operator starts it,
  the offline gate answers for remote code as it does for in-process code, and `08:904`
  is built as written. Blocks: checks 3 and 9, and steps D1 and D2 of the exit.

**Q10. What does the profile clause of check 9 read before S4a?** Check 9 denies "a
remote row at process scope in a multi-tenant profile" from S2b (`08:507`; `07:1194`). No
profile declares tenancy before S4a, and the test profile with two tenant trees arrives
at S4a (`08:689`, `:699`). The shared instance of S2b is a row at process scope.
- (a) The clause is reported "not evaluated: no profile holds tenants before S4a", and
  S4a builds it with the tenant directory. The contribution-set clause is built whole at
  S2b.
- (b) S2b adds the datum that says a profile holds tenants, and a test profile sets it.
  The datum has no other reader before S4a.
- Recommended: (a). It is the rule of ADR-060 decision 17 for a clause with no input, and
  (b) stores a field whose reader ships later (`08:680`). The owner decides, because
  `08:507` says "from S2b". Blocks: check 9.

**Q11. Does the `contrib` minor of the toy registries carry `compatibleFrom`?** At major
0 the exact minor is needed unless `definition.yaml` carries `compatibleFrom` (`08:906`;
`07:548`). The minor adds a proto and an adapter and, with decision 14, guard cases; the
Go interface does not change.
- (a) `0.2` with no `compatibleFrom`. Every manifest of the repository is regenerated at
  `0.2` in the same pull request, which manifest parity requires anyway, and the S2a
  exit's unserved minor moves to `0.3`.
- (b) `0.2` with `compatibleFrom: "0.1"`, so an artifact attested on `0.1` is still
  admitted.
- Recommended: (a). Nothing built on `0.1` exists outside the repository, so (b) would
  admit only artifacts that no one holds, and (a) is the plain rule of major 0. Blocks:
  the `contrib` mark.

**Q12. How does a volatile config change reach a remote instance?** ADR-034 leaves to S2b
whether `FromManifest` gives a proxy row a hand-off or the proxy forwards
`loader/volatile-update` to the instance (ADR-034:1829-1833). A hand-off exists only
through `plugin.Define`.
- (a) The proxy forwards the update to the instance as a protocol message. In a Go
  plugin `sdk/remotehost` applies it through the hand-off the plugin's own `Define`
  already has; `aiops-sdk` hands the new config to the contribution. No kernel change.
- (b) `FromManifest` gives the proxy a hand-off, and the proxy restarts the activation
  with the new config. This needs an ADR-034 addition if the kernel must change, inside
  25 lines.
- Recommended: (a). One plugin then behaves the same in both runtimes (`07:1198`), and
  the kernel does not change. Blocks: `FromManifest` and the messages of ADR-041.

**Q13. Which deployment-unit targets does S2b build?** Install renders the unit per
target: a compose fragment started with `plugin start`, a systemd unit, or a Helm values
entry (`08:1292`; `02:313`). `deploy/compose` and `deploy/helm` arrive at S7 (ADR-060
decision 22), and the S2a clean machine holds no Compose.
- (a) Compose and systemd. The compose fragment is a file of its own under the home that
  `plugin start` runs; the exit uses it; the systemd unit is proven as rendered text.
  The Helm entry waits for `deploy/helm`.
- (b) Compose only; systemd and Helm wait.
- (c) All three, with a minimal chart written at S2b.
- Recommended: (a). Both forms of a single-host install are rendered, and nothing is
  written for a chart that does not exist. Compose is read and pinned first (decision
  31). Blocks: the install of remote code, `plugin start` and the exit.

**Q14. What do the callbacks and the remote listeners run on at S2b?** `sdk/remotehost`
provides definitions through callback clients (`07:1152`), `aiops-sdk` has `@listens`
(`07:1209`), and ADR-041 fixes the callback set with `tools` (`08:697`). No definition of
the callback set and no hook marked remote exists before S3, and a part lands with its
first consumer (`08:680`).
- (a) S2b builds the callback path and the remote listener path and proves them on a
  test definition marked `callback` and a test hook marked remote, as ADR-060 decision
  15 proved the signed class on a test definition. Nothing of them ships in a release
  table. The real callbacks arrive with their definitions.
- (b) ADR-041 writes the set, and S2b builds no callback and no listener path; `@listens`
  and the callback clients arrive with the first real definition, at S3 and later.
- (c) `kv`, `blob` and `secrets` move to S2b as the first callbacks.
- Recommended: (a). `07:1152` and `07:1209` are built at the step that names them, the
  protocol is exercised before S5 ratifies it, and no product definition moves. Blocks:
  ADR-041, `sdk/remotehost` and the Python SDK.

**Q15. Which licence rule holds the dependency set of `aiops-sdk`?** L7 lists the
licences allowed in process, and remote plugins stay unrestricted (`08:1341`, `:1350`).
`aiops-sdk` is a first-party artifact (`08:715`) that runs only inside a remote plugin.
Its gRPC runtime requires `typing-extensions` under PSF-2.0, which L7 does not list, and
ships a certificate file under MPL-2.0, which L8 conditions (decision 31).
- (a) The set is held to L7, with PSF-2.0 recorded by name for `typing-extensions` and
  the MPL-2.0 file under the condition of L8. Each package has a register entry and a
  licence-record row, and a check fails on a package of the set that has none.
- (b) L16 covers it: the set is recorded and read, and no allow-list gates it.
- (c) `aiops-sdk` avoids the gRPC runtime that brings both.
- Recommended: (a). A first-party artifact is held to the first-party rule, and an
  exception is recorded by name, as L7 requires. Blocks: the first Python dependency.

**Q16. Do generated files count against a line cap?** `kernelcheck lines` counts every
Go file of a package (ADR-060 decision 35), and S2b commits generated Go for the first
time.
- (a) A file that the adapter parity check regenerates is listed and summed apart and
  counts against no cap. The generator has a cap of its own.
- (b) Generated files count, and the caps are sized for them.
- Recommended: (a). A cap bounds what is written and reviewed by hand, and parity already
  holds a generated file to its source. Blocks: the first generated file.

**Q17. Eleven readings of written text.** Each reading below is what S2b would build,
and each edits the lines it names in the commit that builds it.
1. **Additive from the first release.** `07:1126` says changes inside a major are
   additive, and `07:67` says the protocol is provisional until S5 and S4b. Reading: the
   additive rule binds from the first release that ships major 0; before it, a change of
   `aiops.remote.v0` is made by an ADR-041 addition with the generated code. Edits
   `07:1126`.
2. **The activation id.** `07:1161` gives it as (replica id, epoch), and the kernel
   exposes no epoch to `Apply`. Reading: the proxy mints it, a process id and a counter
   per activation, with no kernel accessor. ADR-041 fixes the form.
3. **Stub remote images.** `08:463` runs the matrix with "stub remote images". Reading:
   in the matrix the two stubs are programs that the harness builds and starts as its
   own processes, since the matrix runs with no container runtime and the kernel
   launches nothing; the exit proves the image path.
4. **The disposer cell.** The cell of `matrix.yaml:689-694` sits in the row of a
   tenant-scope disposer, and every remote row of S2b is at process scope. Reading: at
   S2b it runs on a process-scope remote row, where the proxy's disposer returns at its
   bound (`07:1196`) and the outcome is "Reported"; S4b runs it again in a tenant tree.
5. **A handshake denial.** A denied row is detached as disabled (`08:510`), and the
   matrix row "Remote unreachable or handshake mismatch" expects PENDING or FAILED with
   reconnect (`08:477`). Reading: a denial at admission detaches the row; a denial at the
   handshake fails the proxy with check 3 named and follows the matrix row.
6. **One row.** A bundle holds "one or more rows" (`08:393`), and there is one proxy row
   per remote instance (`07:1140`). Reading: at S2b the bundle of a remote artifact
   holds one row of its own plugin, and `plugin check` refuses another.
7. **Stopping a shared unit.** `plugin disable` on the host also stops the unit
   (`07:1195`), the operator removes it (`03:495`), and one instance serves the site
   (ADR-047 decision 7). Reading: `disable` stops the unit when no enabled row of any
   profile of the site names the plugin; `remove` deletes the files that install
   rendered, with the other kept files of ADR-047 decision 10, when no profile names
   the id; and removing the unit from the orchestrator stays the operator's.
8. **New remote code.** `08:194` says the new instance is handshaken before the old
   contributions are removed. Reading: that is the behaviour of `plugin update` at S6b
   (ADR-047:313); at S2b a new version arrives by `remove` and `install`.
9. **Scope.** In a multi-tenant profile a remote row is `scope: tenant` (`07:133`).
   Reading: the remote toy plugins and the templates of S2b declare `scope: [process]`,
   the shared instance of `07:1194`.
10. **The Unix socket.** `plugin check` drives a plugin over a Unix socket (`07:1577`),
    and the server dials over mutual TLS (`07:1159`). Reading: on the socket the
    handshake and check 3 run as on a server, and the identity is the process the runner
    started.
11. **Two wordings.** `07:42`, `07:1212` and `08:1218` say "host side" for the kernel
    side of the protocol, while `sdk/remotehost` is the plugin-side host (`07:32`); and
    plugin record §16 says the first message "already carries the tenant binding", while
    `07:1128` takes the tenant from the connection. Reading: the three lines say "the
    kernel side of the protocol", and plugin record §16 follows `07:1128`.
- (a) Accept the eleven readings, each edit landing with the code that builds it.
- (b) Answer each item on its own.
- Recommended: (a). Each reading keeps the written requirement and names where it is
  proven. Blocks: the code each reading names.
