# ADR-063: Scope and working rules of S2b (the remote runtime, single-tenant form)

- Status: **accepted 2026-11-27 by the project owner.** The owner answered all 32
  questions on the same date with the recommended option of each, Q8 and Q15 each on a
  condition; the section "Owner answers of 2026-11-27" records them as decisions 38 to
  69. S2a is merged into `main` (`d124a40`), and on the owner's word S2b code starts on
  the branch `s2b/integration` in parallel with the call-path gate that runs on that
  merge commit (decision 70).
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
- The dial is mutual TLS, and `00` ties it to S2b: the remote runtime is "dialled by the
  kernel over mutual TLS (S2b, in its single-tenant form)" (`00:44`; `08:449`; `07:1159`;
  `02:213`; `03:490`), the row and the tenant come from the connection certificate
  (`07:1128`), and the callbacks ride that stream with no credential of their own
  (`00:417`). On a single-tenant self-host neither side has an issuer: `00:410-411` holds
  only the hosted classes, the instance identity is open (`02:483`, item 25) and is a
  deliverable of S2b (`08:697`), and the "cell's kernel certificate" that the replica
  presents does not exist on a self-host either. Disable and remove revoke the
  instance's credentials first (`03:495`), and the matrix cells of a disabled or removed
  instance are marked S8a and S4b (`matrix.yaml:409-421`), so no cell covers revocation
  at S2b.
- The isolation of the remote runtime is "Process, and network policy from the manifest
  `capabilities`" (`08:388`; `07:1109`; plugin record §11), and `capabilities.network`,
  a list of hosts or of `config:` pointers, is "rendered as the network policy of a
  remote instance" (`07:135`). No target of `08:1292` takes a list of host names as it
  stands: a compose file has an internal network and no allow-list by host, a systemd
  unit filters by address, and a network policy of Kubernetes by address block.
- `plugin enable` refuses "while the endpoint is missing from the `sidecars` table or
  does not answer the handshake" (`08:398`; `07:1870`), and the dial is the server
  process's (`08:448-449`; `02:213`). `aiopsctl` runs on the host and a kernel may run in
  a container (ADR-047 decision 15), so `aiopsctl` may reach neither the instance nor the
  kernel's key. No line says which process makes the refusal.
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
  decision 17 has no entry point for a handshake. The same line has "boot and every live
  generation evaluate every clause on every row", and two clauses of check 3 need a
  connection.
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
  `profileContext`, which also holds `Commit` and whose consumers are four first-party
  rows (`dist/dist.yaml:36-47`; ADR-059 decision 40). The kernel compiles the binding
  lists once, when it starts, by entry (`sdk/kernel/root.go:89-97`), and a proxy is built
  from a manifest that arrives later. Whoever holds a `kernel.Plugin` value runs code
  under that entry's bindings, so boot gives the values to the loader alone (ADR-059
  decision 41).
- No definition of the callback set exists (`07:1169-1180`): telemetry over OTLP, `kv`,
  `blob` and `secrets` are S3, `tenants` S4a, `audit` and `credentials` S5, `llm` and
  `tools` S8a (`08:629`, `:698-700`). No definition owns a hook marked remote, and
  `sdk/gate` is S5.
- `dist` ships the profile `dev` with the bundle `aiops-core` alone, and each later step
  adds its bundle with its first row (ADR-060 decision 27). `sidecars-static` is a row of
  `aiops-selfhost` (`00:292`), which `dev` lists (`08:264`). At the S2a exit the rows of
  the toy dispatcher and the toy caller reach the clean machine through the profile
  patch of its home (`test/authoring/exit_test.go:779-784`).
- At the S2a exit the test starts the kernel on the job's container runtime with no
  network (`test/authoring/exit_test.go:1062`; s2a-report:472), and the runtime of each
  environment, the one `plugin start` would use, runs with no bridge and no packet
  filter (`test/authoring/env_test.go:134-137`). A kernel that dials an instance needs a
  network that both are on.
- The matrix cell "while running" expects a breaker (`matrix.yaml:381-386`), which is the
  dispatcher's (`07:783`, `:1153`), and the toy dispatcher has none.
- The deployment unit is a compose fragment, a systemd unit or a Helm values entry
  (`08:1292`), and `deploy/compose` and `deploy/helm` arrive at S7 (ADR-060 decision 22).
- Nothing is published outside the private repository (`08:1348` L14; ADR-057 decision
  5), and the Python SDK is a package that an author installs. The gRPC runtime of the
  Python SDK is an unread entry that blocks until read (`08:1347` L13; licence:68). The
  line caps count Go only. The name lint and suite neutrality (U1, U3) read every file
  under `sdk/`, `boot/` and `loader/`, a proto included, and nothing at the repository
  root, where the Python SDK would live (`tools/register/names.go:153-177`); the import
  boundary (U2) reads Go files only (`internal/vet/imports.go:265`). `00:268` and
  `00:299` hold all three over a definition and its proto in any language.
- `sdk/kernel` has 25 lines left under its cap of 8,200 and `loader` 196 under 1,300.
  `boot` measures 9,777 lines at `50665fa`, against an S2a exit cap that is proposed at
  10,800 and not yet settled (s2a-report:409-453).

## Decision

### Order and gating

1. **S2b code starts after four things.** This ADR is accepted; the questions marked
   "before any S2b code" are answered; S2a is merged into `main`, with the call-path gate
   run on the merge commit and the S2a exit caps written into ADR-060, `ci.yml` and the
   Makefile (s2a-report:481, :409-454); and the owner gives the word of s2a-report:502.
   The branch of decision 32 is cut from that `main`. Each other question is answered
   before the code it names merges into `main`. Note of 2026-11-27: the owner accepted
   this ADR, answered every question and gave the word to start on that date, with S2a
   merged into `main` at `d124a40`. On the owner's word the call-path gate on that merge
   commit runs beside the first S2b code and not before it (decision 70).
2. **Each ADR due at S2b is its own ADR.** ADR-041 is proposed to the owner, provisional,
   before the first commit of groups 1 to 3 of decision 30 and of the exit harness: the
   protocol, the proxy, the unit, and every command that reads or writes a credential.
   It is accepted before that code merges into `main` (`08:697`, `:1175`; the rule of
   ADR-060 decision 2), and decision 23 binds its identity part. The S2b parts that
   accepted ADRs left open (ADR-020, ADR-034, ADR-044, ADR-047, ADR-048, ADR-054 and
   ADR-061) are written as dated additions to those ADRs, proposed and accepted the same
   way. Each keeps every written requirement it touches. Decision 34 lists what each must
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
   from that record without the owner's word: one that the work finds necessary is a
   question to the owner before it is built, and each answer below that amends the
   record says so and names the section.

### Packages and deliverables

5. **The remote protocol at major 0** follows the four rules of `07:1126-1129`. The major
   is the proto package `aiops.remote.v0`, matched exactly until platform 1.0 (`08:894`,
   `:921`; community:153). Inside the major both sides ignore unknown fields, calls and
   feature names (`07:1126`; community:159): an unknown call is answered as unknown and
   ends neither the stream nor the activation. From when a change inside the major must
   be additive is question Q25. Every connection opens with the frozen `Hello`
   (`07:1127`, `:1133-1137`). The first message of the chosen major carries the plugin
   id, the version, the manifest digest, the activation id, the definitions as
   `name@major.minor`, feature names and one reserved optional field that nothing reads
   in the community edition (`07:1128`; `08:948`). Which digest that is, and what the
   kernel compares it with, is question Q21. The tenant and the row come from the
   connection certificate, never from a field (`07:1128`; `03:162`; decision 23).
   Activation is per connection, with `Activate`, `Register` and `Deactivate`, and a
   registration from a superseded activation is refused, which is `ErrInactiveEffect`
   across the process boundary (`07:1161-1166`); the form of the activation id is
   question Q26. The message and field names are descriptive (`07:67` R9), and ADR-041
   fixes them (decision 34).
6. **The handshake file stays frozen.** S2b generates Go and Python from
   `sdk/remote/handshake/handshake.proto` without editing it: the freeze test keeps every
   case it has (ADR-060 decision 42), so the Go import path is given to the generator and
   not written in the file. `Hello.diagnostics` carries the SDK name and version and the
   release, is logged and is never read for a decision (`07:1136`). With no common major
   both sides log both lists and the kernel closes the stream (`07:1127`).
7. **The module homes** of `sdk/remote` and `sdk/remotehost` are fixed at S2b (`07:60`
   R2; ADR-060 decisions 42 and 54), as question Q6 decides. With the recommended
   answers the require list of the module `sdk` gains no line (`00:263`; ADR-054 decision
   13) and the check `kernelcheck modfile sdk` keeps its list (`Makefile:167`). Two
   options would add lines to it, Q6 (c) and Q7 (c): either rewrites this decision and
   edits `00:263`, R2, ADR-054 decision 13 and ADR-060 decisions 34 and 54, and each says
   so. Each new module gets a closed require list of its own under the same check, a
   served `go.mod` copy (ADR-060 decision 21) and its place in the builder's list of
   release modules (`internal/builder/build.go:94`).
8. **`sdk/remotehost`** is the kernel module inside a Go plugin process, with definitions
   provided by gRPC callback clients; it runs one fiber per activation with that
   activation's config (`07:1152`). A Go plugin whose `Inject` is within the callback set
   and that registers only `contrib` registries and remote-capable hooks is one `Plugin`
   value that passes the same suite in both runtimes (`07:1198`; plugin record §11). The
   plugin process is the gRPC server: the kernel dials it and never the reverse
   (`07:1158`).
9. **The kernel side of the protocol** (the dial, the handshake, the stream and the
   stubs) is one package of the module that holds `sdk/remote`. The proxy plugin and the
   check runner of `aiopsctl` both use it (`00:305`), so one implementation serves a
   server and `plugin check`. This is a placement change, which question Q6 puts to the
   owner: `07:42` and plugin record §14 name what `aiopsctl` carries "the host side of
   `sdk/remotehost`", and `sdk/remotehost` is the plugin-side host (`07:32`).
10. **`plugin.FromManifest`** arrives in `sdk/plugin` with its first consumer, the proxy
    (`07:29`, `:232`; ADR-060 decision 4), with the signature that `07:232` sketches: a
    manifest and an `apply` function give a `kernel.Plugin`. It builds the plugin from
    one manifest file, whose `configSchema` is inline (ADR-048:242-245), so the `Inject`,
    `Hooks` and registrations of a proxy are that row's own and the hook dump and the
    static graph read them (`07:1140`). The `Inject` of such a value is the fixed needs
    of the proxy code and the definitions marked `callback` that the manifest's `uses`
    names, and nothing else: a `uses` entry of any other key, a bound key included, is
    denied by check 9 before a value is built (ADR-048:433), so no manifest takes a key
    through the bindings of `aiops/remote`. `sdk/plugin` imports the kernel and
    definitions only (`internal/vet/imports.go:199-202`), so the dial is in `apply`,
    which the proxy plugin supplies. The exported API of `sdk/plugin` changes, so
    `pluginApi` rises by a minor (ADR-060 decision 8). How a volatile change reaches a
    remote instance is question Q12 (ADR-034:1829-1833).
11. **The proxy** is the in-process fiber of an out-of-process plugin (`03:72`). It dials
    the instance over mutual TLS (decision 23), handshakes before the row is ACTIVE
    (`08:449`), registers the plugin's contributions with the dispatchers that serve
    their definition versions, carries the contribution calls and serves the callbacks
    on its own stream (`07:1149`, `:1165`).
    It refuses a remote `Register` that the manifest's `registers` does not list (ADR-048
    decision 21). A dispatcher accepts a registration only for a served major under the
    minor rule, the exact minor at major 0 (`07:1129`, `:548`). There is one proxy row per
    remote instance and one shared instance per plugin (`07:1140`, `:1194`; `08:690`;
    `02:19`). A proxy row states its roles like any row, and only replicas of those roles
    dial its instance (`03:571`). Which plugin id the row of an installed artifact names,
    how a proxy takes a bound key and what the `remote` row of `aiops-core` is, is
    question Q4; where its endpoint comes from is question Q2; its `tenantMint` inject is
    question Q3; its imports are question Q5; the key through which it reaches the gate
    is question Q20; how many rows a remote artifact may hold is question Q30.
12. **Boot and the loader resolve a remote row** in `plugins.lock` and the files install
    kept, not in the module table (`03:358`; `07:1853`). A plugin id that is neither in
    the table nor locked is a warning for an optional row and a stop for a required one
    (`08:468`; `07:1363`). `boot/` and `loader/` keep their imports (`08:1260`): the
    launcher hands the loader the function that turns a kept manifest into a plugin
    value. That function, and every value it builds, stay with the loader alone, as the
    values of the module table do: whoever holds a `kernel.Plugin` value runs code under
    that entry's bindings, so no key and no catalog entry exposes either (ADR-059
    decision 41). A remote plugin that is installed and enabled while the process runs
    mounts with no restart (`08:194`; `00:490`), so the loader takes a plugin value for
    an id it did not hold when it mounted, inside its cap of 1,300 lines. What S2b builds
    of the second half of those two lines, the new instance handshaken before the old
    contributions are removed, is question Q32. The generation digest already covers the
    lock digests (`00:482`; `boot/digest.go`).
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
    the concurrency cap and the breaker per (tenant, row) that `07:783` gives every
    dispatcher, and the fallback of `07:1196` and `08:477`: the cell "while running"
    asserts the breaker and the fallback from S2b (`matrix.yaml:381-386`), and the cap is
    the dispatcher's by the same line, although its own cell is marked S4b (`08:479`). At
    S2b the one tenant is `default`, so the key is the row. The guard half of each toy
    suite gains a case for each of the four, which raises the suite revision (ADR-060
    decision 10). Every `contrib` definition passes its suite through its remote adapter
    (community:109, G7): the obligations half runs on the proxy fiber as its subject, and
    the guard half never runs remote, since a remote plugin provides no key (`07:518`,
    `:1115`). That pass is also a condition of any later promotion of a `contrib`
    definition to `v1` (`08:910`).
15. **The gate.** `boot` stays one package and the home of the gate, and builds checks 3
    and 9 as the one gate text has them (`08:501`, `:507`). `serves` gains what the two
    checks read (question Q9). The entry-point table of ADR-060 decision 17 gains the two
    checks and one row, the remote handshake, which runs inside boot, inside a live
    generation and inside step 5 of `plugin check`, as decision 16 says. Each golden
    that prints "not evaluated until S2b" is regenerated and reviewed in the commit that
    builds the check (the rule of ADR-060 decision 28).
16. **Where each clause of checks 3 and 9 runs.** Every entry point reports each clause
    it does not evaluate as "not evaluated" with the reason, and a denial names the check
    (ADR-060 decision 17). Two clauses of check 3, the digest and the registrations, need
    a connection, and only the server process and the check runner make one (decision
    17). The handshake is therefore part of boot and of every live generation, and no
    separate pass that could be skipped: each enabled remote row is dialled and
    handshaken inside the generation that mounts it, before the row is ACTIVE
    (`08:449`), and a denial there is in that generation's verdict and names check 3
    (question Q29 for what the row then does). So "boot and every live generation
    evaluate every clause on every row" (`08:495`) holds as written for every enabled
    remote row. A disabled remote row mounts no proxy and `profile render` makes no
    dial; what `08:495` says for those two is question Q22. The proxy reaches the
    handshake entry of the gate through a key of the launch definition, which gains a
    minor and a raised suite revision; which key is question Q20, and the check runner
    of `plugin check` uses the same entry through the toolbox it already holds
    (`sdk/def/launch/v0/toolbox.go:31-33`). `boot` stays the one home of the gate
    (ADR-060 decision 14), and no second copy of check 3 exists outside it.

    | Entry point | Check 3 | Check 9 |
    |---|---|---|
    | `dist build` | Not evaluated: a build list holds no remote code | Not evaluated: the same reason |
    | `plugin check` step 7, `plugin pack`, `plugin gate --against` | The majors of `requires.remoteProtocol` against the served majors, with question Q9 (a). The digest and registration clauses: not evaluated, no connection | The contribution-set clause, on the manifest. The profile clause: not evaluated, no profile |
    | `plugin install` of remote code | As above | The contribution-set clause, on the manifest; the profile clause as question Q10 decides |
    | `profile render` | As above, on every remote row (question Q22) | Both clauses on the manifest, the profile clause as question Q10 decides |
    | Boot and every live generation | The majors, on every remote row. The digest and registration clauses on every enabled remote row, at the handshake of the row below, inside the same generation; on a disabled row as question Q22 decides | Both clauses on the manifest, the profile clause as question Q10 decides |
    | The remote handshake: the proxy inside boot and inside a live generation, and step 5 of `plugin check` | Every clause: the two lists of `Hello`; the digest of the first message, as question Q21 decides; each definition version of the first message and of each `Register` against `serves` | The contribution-set clause on what the connection brings: each `Register` against the registries marked `contrib` and the manifest's `registers`, each listener against the hooks marked remote, each callback against the definitions marked `callback`. The profile clause as question Q10 decides |

17. **The operations library, the commands and the artifact.** The refusals of `remote`
    in `plugin check`, `pack` and `install` are lifted (ADR-060 decision 23).
    - `plugin check` runs T1 and T2 in every runtime the manifest lists (`07:134`,
      `:1577-1578`). For the runtime `remote`, in every language, step 4 starts the
      plugin's entrypoint as a local process and drives it over a Unix socket through the
      handshake, one activation with the definition fakes behind the callbacks, the
      registrations that the manifest lists and the deactivation; step 5 runs the
      obligations half of each suite through the adapter against that process, with no
      Go toolchain for a non-Go plugin (`00:305`; plugin record §14). A Go plugin's own
      tests also run under `-race`, as for `inproc`. `07:1577` words the socket for a
      non-Go plugin only; reading 5 of question Q17 asks the owner to confirm it for Go.
      Step 2 compares with `plugingen` or `aiops-sdk manifest` (`07:1575`); `plugingen`
      writes `remoteProtocol` exactly when `runtimes` holds `remote` and `pluginApi`
      exactly when it holds `inproc` (ADR-048 decision 12). Step 3 on a Python plugin
      validates each config example against the `configSchema` of `plugin.yaml`; its
      clauses on Go imports and plugin vet have nothing to read, and `aiopsctl` parses no
      Python (`07:1576`; reading 6 of question Q17). Steps 1 to 6 need no container
      runtime; step 7 needs one when it packs an image, and without one it reports the
      step as not run and names what it lacks (`07:1570`).
    - `plugin pack` writes the image reference by digest as one more layer type under
      the same `artifactType` (ADR-047:130-131; `08:393`), with `plugin.yaml` in manifest
      schema 1.0, the bundle and an SBOM, as every artifact has (`08:393`; `07:1850`;
      ADR-047:132), and `plugin sign`, `push`, `inspect` and `verify` handle the result.
      Who builds the image, and how `plugin pack` learns its digest, is question Q24.
      The attestation's subject is the digest of the artifact's OCI manifest, and the
      artifact's own field pins the image (ADR-020:326-330).
    - `plugin install` of remote code verifies the signature, the trust policy for this
      id and runtime, the gate and the attestation; runs no third-party code and pulls
      no image; writes the lock entry by digest, the kept files, the verified record and
      the bundle at the end of the profile's bundle list with its rows disabled; and
      renders the deployment unit of decision 25, with the network of decision 24 and
      the credential that question Q18 gives the instance. It needs no rebuild (`08:393`,
      `:398`; `00:480`; `07:1867-1868`). A denial writes nothing. A site holds one
      digest per plugin id (ADR-047 decision 7), and remote code is refused into the
      profile `cli` (`08:273`).
    - `aiopsctl plugin start <id>` starts the unit (ADR-061:1014-1016; `08:1292`). Which
      targets install renders is question Q13. The server never launches or stops a
      plugin process (`08:447`).
    - `plugin enable` refuses, printing the exact command, while the endpoint is missing
      from the table or does not answer the handshake (`08:398`; `07:1870`; `02:326`).
      Only the server process dials an instance (`08:448-449`; `02:213`): `aiopsctl`
      never does, and it holds no kernel credential and needs no route to an instance.
      So the two halves of the refusal come from two places. `aiopsctl` reads the
      endpoint table, a file of the home, and refuses a row whose endpoint is missing
      before it submits anything. For an endpoint that is listed, it submits through the
      reload socket as for any row (`03:374`; ADR-062 decision 11); the server composes
      the generation and its proxy dials; an instance that does not answer, or a denied
      handshake, leaves the row not ACTIVE, so the verdict fails, the final result is
      `rolled back`, the files are restored from the snapshot and `aiopsctl` exits 1
      with the row, the reason the server gave and, where the instance did not answer,
      the exact command `aiopsctl plugin start <id>`. "Logged on both sides" (`08:697`)
      therefore means the log of the server process and the log of the instance.
      Reading 4 of question Q17 asks the owner to confirm the split.
    - `plugin disable` and `remove` revoke the instance's credentials first, then unload
      the proxy, and the operator removes the unit; `disable` run on the host also stops
      it (`03:495`; `07:1195`). Decision 23 says what the revocation must achieve. When
      a shared unit stops, and what `remove` does with the files that install rendered,
      is question Q31.
    - `plugin verify` re-runs the suite against a pulled image, and `plugin smoke`
      attaches the plugin's image to the released image (plugin record §11; `07:1583`);
      both are proofs of conformance at S2b (`08:940`; community:206) and both have
      cases in decision 28.
18. **The Python SDK `aiops-sdk`** ships at S2b with the parts of `07:1205-1213`: one
    module of base classes and message types per `contrib` definition and major,
    generated from its proto; `@contributes`; `@listens`; `aiops-sdk manifest`, which
    writes `plugin.yaml` from `plugin.toml` and the decorators with `requires` holding
    `remoteProtocol` and the definition versions; `serve()`; and the fixture
    `aiops_sdk.testing.host()`, which wraps the `aiopsctl` binary. The fixture is a
    pytest fixture (`07:1212`), so that test runner is a dependency of the testing part
    of the SDK and of the template's test, read under decision 35, and no plugin image
    needs it to run. A Python author needs
    `aiops-sdk` and `aiopsctl` and nothing else: no Go toolchain, no kernel checkout and,
    for T0 to T2, no container runtime (`07:51`, `:1236`, `:1570`), which the exit proves
    on a machine that holds neither (decision 27). Its conformance is the Go
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
    `08:1269`). The documents know `plugin new <registry> --lang go|python` and no form
    that makes a remote Go plugin (`07:1556`; ADR-061:934): that form, the runtimes its
    manifest lists and what writes its `main` are question Q23. Each remote template
    writes the file from which the plugin's image is built. The Go one puts one static
    binary on no base layer, as the server images of S2a have none (ADR-044 decision
    18), so it names no base image. The Python one names a base image by digest, which
    is read under decision 35 before the template merges.
20. **The failure matrix.** The five cells marked `since: S2b` pass on the built binary
    as `matrix.yaml` states them: "Remote unreachable or handshake mismatch" in its four
    process columns, and the live-recomposition cell of "A tenant-scope disposer passes
    its deadline (a remote that ignores `Deactivate`)" (`08:477`, `:486`). The harness
    runs every cell whose step is S1 or S2b. The two stubs, one that hangs a call and one
    that ignores `Deactivate` (`08:463`), are built for it; whether they are images or
    processes is question Q27, on which row the disposer cell runs is question Q28, and
    what a row does after a denial at the handshake is question Q29. A remote plugin
    provides no key, so its loss changes no other fiber's epoch; a short disconnect fails
    fast behind the breaker; an exhausted reconnect budget unloads the proxy and unwinds
    every registration; and a proxy disposer never waits on the remote beyond a bound
    shorter than the dispose deadline (`07:1196`; `03:493`).
21. **The remote-plugin container** is the plugin's own image, in any language, optional
    in the profiles S, A and D, one shared instance per plugin for the tenant `default`
    (`00:138`; `02:191`, `:336`). The S2a and S2b exits install into `dev` (`08:689`).
    Decisions 23 to 25 say what its dial, its network and its unit must hold.
22. **Shipped bundles and profiles.** `dist` ships the bundles `aiops-core` and
    `aiops-cli` and the profiles `dev`, which lists `aiops-core` alone, and `cli`, and
    each later step adds its bundle to `dev` with its first row (ADR-060 decision 27 and
    its note of 2026-11-25).
    - With question Q2 (a), S2b ships the bundle `aiops-selfhost`, the home of
      `sidecars-static` (`00:292`), with that one row, and `dev` lists `aiops-core` and
      then `aiops-selfhost`, the order of `08:253-264`. `cli` does not list it. With Q2
      (b) or (c) no bundle is added.
    - With question Q4 (a) or (b) no shipped bundle holds a row of the proxy:
      `aiops/remote` is a plugin of the release module table and its rows come from the
      bundles of installed artifacts, so `aiops-core` counts ten rows where `00:276`,
      `08:253` and `08:273` count eleven. With Q4 (c) `aiops-core` gains the one row
      `remote` and the count stays.
    - The toy dispatcher, the toy caller and `teststate` are plugins of the test
      release's module table and no shipped bundle holds a toy row. Their rows reach the
      clean machine's composition through the profile patch of its home, as at S2a
      (`test/authoring/exit_test.go:779-784`). The test release's module table also
      holds `aiops/remote` and, with Q2 (a), `aiops/sidecars-static`, and it ships the
      `dev` above.

### Identity, isolation and the deployment unit

23. **The dial is mutually authenticated.** At S2b the proxy dials each instance over
    mutual TLS, in the single-tenant form (`00:44`; `08:449`; `07:1159`; `02:213`,
    `:336`; `03:490`), and the frozen `Hello` is the first message on that connection.
    What identifies each side where no cell certificate authority exists is question
    Q18, and ADR-041 writes the answer (`08:697`; `02:483`, item 25). Whatever the
    answer, the points below hold, and ADR-041 does not relax them:
    - Both sides are authenticated before `Hello`. The instance proves the identity that
      the site holds for that plugin, and the kernel proves the site's kernel identity.
      The proxy refuses an instance that does not, and `sdk/remotehost` and `serve()`
      refuse a dialler that does not.
    - The row and the tenant come from the connection certificate, never from a field
      (`07:1128`). At S2b the certificate of an instance names its plugin id, the row is
      the row of that locked plugin, and the tenant is `default`. A field that names a
      row or a tenant is ignored.
    - A server has no unauthenticated dial: no flag, no config field and no fallback
      opens a stream in plaintext or to a peer the kernel cannot verify. The Unix socket
      of `plugin check` (reading 2 of question Q17) is opened by the check runner alone,
      to a process that it started, and no row of a server profile reaches it.
    - No private key is in an artifact, in an image, in `plugins.lock`, in a dump or in a
      log, and a log names an identity and never key material. The kernel's key is in no
      file that a unit gives an instance.
    - The callbacks ride that stream and have no credential of their own (`00:417`).
    - `plugin disable` and `plugin remove` revoke the instance's credential first and
      then unload the proxy (`03:495`; `07:1195`). From that moment the kernel refuses a
      stream that the old credential opens, on that dial and on every later one, with no
      leader and with no restart of the process.
    - The new credential classes are rows of `00` §5.4 before S2b builds them
      (`00:417`), written in the commit that records the answer to Q18 (`08:999`).

    ADR-041 fixes the rest: the form of each identity, where each key lives under
    ADR-047 decisions 13 to 15 and ADR-062:224-234, its lifetime, its rotation and the
    mechanism of the revocation.
24. **The network policy of an instance.** The isolation of the remote runtime is the
    process and a network policy from the manifest's `capabilities` (`08:388`;
    `07:1109`; plugin record §11): `capabilities.network` is rendered as the network
    policy of a remote instance (`07:135`). S2b builds the deny side whole, on each
    target that question Q13 gives. The unit of an instance whose manifest has no
    `network` entry has no route out: the kernel reaches the instance on its one
    listener, and the instance reaches nothing: no host outside, no other instance, no
    registry, and of the kernel only the stream that the kernel opened, since a remote
    plugin has no other way into the server (`07:1169`). Each instance has a network of
    its own for that. What a unit renders for a manifest that does list
    entries, each a host with an optional port or a `config:` pointer into the row's
    config, is question Q19, since no target takes a list of host names as it stands
    (Context) and the two toy plugins list none. Whatever the answer, install renders no
    unit whose instance has a wider network than its manifest lists.
25. **The deployment unit is rendered from a fixed template.** `plugin install` renders
    the unit from a first-party template that is compiled into `aiopsctl`. A third-party
    artifact gives it three inputs and no other: the image reference by digest, which
    the signed artifact pins (ADR-020:329-330); the plugin id; and the `entrypoint`
    (`07:148`), if the S2b part of ADR-048 keeps one in `plugin.yaml`. Each is checked
    against a closed grammar and written by an encoder of the target's format, never
    spliced into text. Nothing else of the artifact reaches the unit: no mount, no
    variable of the environment, no port, no user, no capability and no network name.
    - The unit runs the image by that digest and by no tag. A compose fragment does so
      through `plugin start`. A systemd unit runs the same image through the container
      runtime of the host, since an artifact holds an image and nothing that runs on a
      host (`08:393`).
    - By default it names a user that is neither the site user nor root, so the instance
      can neither open `run/reload.sock` nor write the home (ADR-062:224-234; ADR-047
      decision 14). It binds no path of the home and not `run/`, except the one
      directory that holds that instance's own credential, read-only (decision 23).
    - Its network is the one of decision 24.

    The paths of the unit files, of the endpoint table and of the credentials under the
    home, and the network of a kernel container, are the S2b part of ADR-047 (decision
    34).

### What waits, and why

26. The parts below are not built at S2b.

| Part | Step | Why |
|---|---|---|
| One instance per (tenant, plugin); activations across replicas; the test provider of `sidecars` with a test CA; the tenant-tree column of the remote cells | S4b | `08:632`, `:666`; `00:575`; `03:20`; ADR-041 is ratified there for one tenant per connection |
| The bound view of `sidecars`, `tenant-handle` and the tenant handle | S4a | `08:699`; questions Q2 and Q3 |
| The `tenants` callback | S4a | `07:1179`; the tenant directory is S4a (`08:699`) |
| The telemetry callback over OTLP; `kv`, `blob` and `secrets` as callbacks | S3 | `07:1173-1175`; their definitions are S3 (`08:698`), and a part lands with its first consumer (`08:680`) |
| The policy callbacks, which are `audit` and `credentials`; a notifier as remote Go and remote Python; the `notifiers` adapter; scrubbed payloads for remote listeners | S5 | `07:1176`, `:1180`; `08:700`; `00:74`; ADR-041 is ratified there for the policy callbacks |
| The `llm` callback and the virtual key of an instance | S8a | `07:1177`; `08:629`; `00:411` |
| The `tools` adapter and callback, the remote reference harness, revocation of the virtual key | S8a | `08:629`; `07:1178`, `:1182` |
| A Python tool installed as a remote plugin | S8b | `08:630` |
| The allow-list side of the network policy, with question Q19 (a) | With the first remote plugin that lists a network entry, S5 at the latest | Decision 24; `08:680`; a notifier runs remote at S5 (`08:700`) |
| The matrix cells of a disabled or removed instance | S8a and S4b | `matrix.yaml:409-421`; at S2b the cases of decision 28 cover revocation |
| The inventory that flags an instance that still answers after revocation; the index | S6a | `07:1195`; `00:574` |
| Quarantine and cohort update of remote versions; `plugin update`, and with question Q32 (b) the handover of `08:194` | S6b | `08:633`; ADR-047:313 |
| `sidecars-kubernetes` and the cell's plugin CA | S9b | `03:490`, `:495` |
| The Helm form of the unit, with question Q13 (a) | S7 and later | `deploy/helm` does not exist yet (ADR-060 decision 22) |
| Loopback shims in the SDKs | A trigger | `00:632`; `07:1190` |
| `wasm`; third-party plugins on hosted; a reader of the reserved first-message field | The enterprise edition | `00:628`; `07:1104`; `08:948`, `:954-955` |
| Remote protocol major 1 and the served previous major | Platform 1.0 | `08:921` |
| The public package index of `aiops-sdk` | When L14 clears | `08:1348` |

### The exit test, made checkable

27. **The exit test is `test/remote/`**, run by the CI job `s2b-exit` and by `make
    s2b-exit`, on linux/amd64 alone, as the S2a exit is (`Makefile:57`). It reuses the
    harness of `test/authoring/` (ADR-060 decision 30): it first builds what a release
    pipeline would build, with throwaway keys, and publishes nothing outside the test
    (ADR-057 decision 5; `08:1348` L14). It then drives three environments, each a set of
    containers on an internal network of its own. None holds a repository checkout, a
    `replace` line or a module cache of the repository.
    - The pipeline of the test serves the root and SDK modules, the remote modules of
      question Q6 included, from the module proxy inside the job; builds the test release
      and its `aiopsctl`; builds the `aiops-sdk` package and puts it, with the pinned
      packages of its dependency set, in a package directory inside the job; runs the
      pinned registry; and pushes to that registry the base image of the Python plugin
      image, fetched by digest. Every input from outside the job is fetched here, by
      version and checksum or by digest, before an environment starts. The Python parts
      use one interpreter, CPython 3.12 on glibc, and the wheels tagged
      `manylinux2014_x86_64`, the files that decision 35 names; another interpreter or
      platform enters the exit only after its files are read.
    - The author's repository is the one of the S2a exit (`08:697`: "the same
      repository"). It holds a Go toolchain, the `aiopsctl` of the test release, that
      interpreter with `aiops-sdk`, and a container runtime of its own, and it reaches
      the module proxy, the package directory and the registry and nothing else.
    - The Python author's machine holds the `aiopsctl` of the test release and that
      interpreter with `aiops-sdk`, and no Go toolchain and no container runtime (`07:51`,
      `:1236`, `:1570`), as the pack author's machine of the S2a exit holds `aiopsctl`
      alone (ADR-060 decision 30). It reaches the package directory and nothing else.
      The directory of the Python plugin is one directory of the author's repository,
      bound into both environments.
    - The clean machine holds `aiopsctl`, a container runtime of its own and what
      `plugin start` needs, and no Go toolchain and no Python. Its kernel is the image of
      the test release in the container form of ADR-047 decision 15. It is started on
      the clean machine's own runtime, the one that `plugin start` uses, and not on the
      job's runtime as at S2a (`test/authoring/exit_test.go:1062`; s2a-report:472),
      because the kernel and the instances must be containers of one runtime to share a
      network. The kernel container starts with no network, as at S2a. The network of
      each instance (decision 24) holds that instance and the kernel container and
      nothing else; `plugin start` creates it and attaches both, since the server
      launches nothing and `plugin start` is the operator's one command (`08:1292`). The
      endpoint that install writes into the table is a name that the runtime resolves on
      that network. How `aiopsctl` names the kernel container, and what the same step
      does for a kernel that runs as a binary on the host, is the ADR-047 part of
      decision 34. The runtime of an S2a environment runs with no bridge and no packet
      filter (`test/authoring/env_test.go:134-137`); at S2b it changes as far as such a
      network needs, and the environment's own network still reaches the registry and
      nothing else.

    Each step passes only as stated:
    - **G1.** In the author's repository `aiopsctl plugin new`, in the form that question
      Q23 gives, writes a remote Go echoer, and its stubs are filled in from the fixture
      of step A2 of the S2a exit, so its `Plugin` value is that step's, unchanged. Its
      manifest lists `runtimes: [remote]`. `plugin check --write` writes a `requires`
      that holds `remoteProtocol: [0]`, `echoers` at the served version and no
      `pluginApi` (ADR-048 decision 12). `plugin check` passes steps 1 to 5 and 7, with
      steps 4 and 5 run as decision 17 says for the runtime `remote`, and its `--json`
      report is kept.
    - **G2.** `plugin pack` takes the plugin image, built on the author's container
      runtime on no base layer, and records its digest, as question Q24 decides; `plugin
      sign` and `plugin push` publish the image and one artifact to the test registry.
      The artifact carries the image reference by digest, `plugin.yaml` in manifest
      schema 1.0, a bundle of one disabled row and an SBOM, and as referrers the
      signature and an attestation that names the artifact digest and `echoers` at the
      served version with its suite revision and the half that ran (`08:393`;
      `07:1850`). The digest in the artifact is the digest under which the registry
      serves that image.
    - **P1.** On the Python author's machine `aiopsctl plugin new stampers <id> --lang
      python` writes the skeleton and `aiops-sdk manifest` writes its `plugin.yaml`.
      `plugin check` passes steps 1 to 4 and fails at step 5 with exactly one line per
      obligation of the `stampers` suite.
    - **P2.** On the same machine, with the stubs filled in from the test's fixture, the
      plugin's own test passes the suite through `aiops_sdk.testing.host()`, and the
      `--json` report of `plugin check` shows steps 1 to 6 passed and step 7 not run,
      naming the container runtime it lacks. In the author's repository the same
      directory passes `plugin check` whole, and `plugin pack`, `sign` and `push`
      publish it as G2 does, its image built on the base image that the registry of the
      test serves by digest.
    - **C1.** On the clean machine `trust add --runtimes remote` lists the author for its
      ids, and `plugin inspect` prints each spec's rows, configs and image reference and
      fetches no image.
    - **C2.** `plugin install <spec> --profile dev` of each writes `plugins.lock` by
      digest, the verified record, the unit and the credential that question Q18 gives
      the instance, and appends the bundle with its row disabled; it pulls no image and
      runs none. Each unit names the image by the digest in the signed artifact and by
      no tag, a user that is neither the site user nor root, no path of the home but
      that instance's credential directory, and a network with no route out (decisions
      24 and 25). The clean machine runs the image of the test release: no install
      answers `rebuild-required`, no `dist build` runs, and the kernel that serves step
      C4 is the process that booted before step C2. `plugin enable` before `plugin
      start` exits 1: `aiopsctl` made no dial, the final result is `rolled back`, the
      output names the row as not ACTIVE because its instance did not answer and prints
      the exact command `aiopsctl plugin start <id>`, and the site files are restored
      (decision 17; ADR-062 decision 11).
    - **C3.** `plugin start` starts each instance, and the running container of each has
      the image digest that the signed artifact pins. `plugin enable` enables each row
      while the kernel runs; its final result is `passed`; the `teststate` row reports
      every row of the composition ACTIVE, in a generation later than the one the
      process booted with and in the same process. Each stream is mutually
      authenticated (decision 23): the log of the server process names the identity it
      verified for each instance, the log of each instance names the kernel identity it
      verified, and a dial that the test makes on each instance's network without the
      kernel's credential, once in plaintext and once with another key, is refused
      before any `Hello` is answered. From inside each instance the home and
      `run/reload.sock` cannot be opened, and a connection to the registry of the test,
      to the other instance and to an address outside fails.
    - **C4.** Each is called: the toy caller calls `echoers` and `stampers` through the
      dispatcher, and each reply is the one that the instance's code fixes.
    - **C5.** The composition holds no policy row and no tenant row (`08:702`).
    - **D1.** A copy of the Go echoer that offers only protocol major 7: the pipeline
      serves a second version of the remote modules in which the one list of spoken
      majors holds 7 alone, and the copy is built against it. Its manifest says so, and
      it is attested and signed by the trusted author. With question Q9 (a) it is denied
      at `plugin gate --against` and at `plugin install`, each denial names check 3 and
      no other evaluated check, and no site file changes. With Q9 (b) both report check
      3 as "not evaluated: no connection" and admit the copy, and step D2 runs on this
      copy as it is.
    - **D2.** With Q9 (a), the same image behind a manifest that lists major 0, made
      without `plugin check` and attested and signed by the trusted author, so that
      install admits it. Once its instance is started, `plugin enable` exits 1 with the
      final result `rolled back` and prints the denial, which names check 3 and holds
      both lists of majors. The server process made the dial and `aiopsctl` made none
      (decision 17). The log of the server process and the log of the instance each hold
      both lists, the server closes the stream, the row is not ACTIVE and the site files
      are as they were before the command (`07:1127`; `08:398`, `:477`).

    The exit texts of `08:623`, `08:697`, `00:573`, `01:290`, community:60 and plugin
    record §16 are edited to one text with the answer to question Q1.
28. **The other refusals and proofs are cases, not exit steps.** Each has a test that
    fails on its base.
    - The protocol and check 3. A first message whose digest differs from what question
      Q21 has the kernel compare it with, and one that names a definition version that
      is not served, each denied with check 3. A `Register` outside the manifest's
      `registers`. A `Register` from a superseded activation, and one from an activation
      of an earlier boot of the kernel (question Q26). An unknown field, an unknown call
      and an unknown feature name, each ignored by the proxy, by `sdk/remotehost` and by
      the Python `serve()`, with the stream and the activation kept (`07:1126`). A peer
      that passes a bound of ADR-041, on the size of a message, on the number of
      registrations or on the size of `Hello.diagnostics`, refused, with neither side
      logging more of `diagnostics` than its bound. `serve()` against a kernel that
      offers no common major, which logs both lists. With question Q14 (a), a callback
      from an activation that is not ACTIVE, refused (`07:1165`).
    - Check 9. A remote manifest that declares a `provides` entry, an unmarked registry,
      an unmarked hook or a `uses` entry of a key that is not marked `callback`, a bound
      key included, denied with check 9 at `plugin check`, at `plugin gate --against`,
      at install and at boot (ADR-048:433); and `FromManifest` builds no value whose
      `Inject` holds such a key (decision 10).
    - Identity (decision 23). A process at an instance's endpoint with another identity,
      and one that listens in plaintext, each refused by the kernel, with the row not
      ACTIVE. A dialler without the kernel's credential, refused by `sdk/remotehost` and
      by `serve()`. After `plugin disable`, and after `plugin remove`, the unit is
      stopped, and an instance started again with the old credential opens no stream.
      A message that carries a field naming a tenant or a row changes neither
      (`07:1128`). No key material in an artifact, a dump or a log of the exit.
    - Isolation and the unit (decisions 24 and 25). An artifact whose plugin id or
      `entrypoint` is outside the grammar, refused at `plugin check` and at install,
      with no unit rendered. A rendered unit holds nothing of the artifact but the three
      inputs. A manifest that lists a `network` entry, handled as question Q19 decides.
      With question Q13 (a), the systemd unit run in its job with the same assertions as
      steps C2 and C3 make on the compose fragment.
    - Trust and install. An artifact whose signer is not listed for `remote`. A remote
      artifact under `aiops/` or `contrib/` whose signer is not a release key
      (`08:503`). An unsigned remote artifact of an id that `allowUnsigned` lists,
      admitted or refused as the S2b part of ADR-020 decides, with the case either way.
      Install into `cli`. Install of a second digest of one id. `plugin enable` of a
      remote row whose endpoint is missing from the table, refused before anything is
      submitted, with the exact command (decision 17).
    - Conformance. `plugin verify` on the artifacts of G2 and of P2 passes against the
      pulled image; on a copy whose image or whose layer was changed after signing it
      fails and names what differs. `plugin smoke` of the Go plugin and of the Python
      plugin against the test release passes, in the job that has a container runtime
      (`07:1583`; `08:940`; ADR-060 decision 31). The fixture echoers of the S2a and S2b
      exits hold one `Plugin` value that passes the `echoers` suite in both runtimes
      (`07:1198`).

### Working rules

29. **Test first.** The acceptance test of each deliverable is committed, failing, before
    the code that makes it pass, and every fix lands with a test that fails on its base.
    The S2a report records this rule as not met for part of its history, with commits
    that fail a check (s2a-report:58, :456). At S2b a commit that adds a failing test
    therefore marks it as expected to fail, in a way that `make check` reports and
    counts, and the commit that makes it pass removes the mark, so no commit on the
    branch fails `make check`.
30. **Review rounds.** An adversarial review round runs on each finished group and once
    more on the whole step, on the commit that the exit report names. Each finding is
    confirmed by a reproduction before it is fixed. A group merges into `main` only when
    its round leaves no open finding. The groups: (1) the protocol, the modules, the
    adapter generator and the `contrib` minor of the toy registries; (2) `FromManifest`,
    the proxy with its authenticated dial, the resolution of remote rows and gate checks
    3 and 9; (3) the operations library, the artifact, the unit with its credential and
    its network, and the commands; (4) the Python SDK and its template; (5) the
    failure-matrix cells and the exit test.
31. **The standing rules of `08:678` apply as at S2a** (ADR-060 decision 32, items 1 to
    4). `make plugin-check` and CI run `plugin check` and a T3 boot on every plugin under
    `plugins/` and `examples/`, the remote examples in Go and in Python included; the T3
    of a remote example starts its instance as the test's own process, since the kernel
    launches none. Nothing outside the closed vocabularies merges into `sdk/kernel`,
    `boot/` or `loader/`.
32. **Branch, commits, report.** S2b work lands on the branch `s2b/integration` and
    reaches `main` by merge. Commits follow Conventional Commits. Nothing is published
    outside the private repository: the exit's registry, module proxy, package
    directory, images and keys exist only inside the test. S2b closes on the owner's
    approval of an exit report, `docs/design/s2b-report.md`, in the form of the S2a
    report: every decision of this ADR with its status and its evidence.
33. **A written requirement is never weakened.** Where the work finds that a line of the
    documents cannot be built as written, the work stops on that point and the owner is
    asked, with the lines the answer would edit; a note "as built" never stands in for
    the answer.
34. **What each ADR and addition settles**, at least:
    - ADR-041, provisional: the messages of `aiops.remote.v0` and which side sends each
      field of the first message, since `07:1128` lists fields the plugin knows beside
      the activation id, which the kernel assigns; how the plugin learns the chosen
      major; where the call that carries `Hello` is declared, given that the frozen file
      takes no service and that a plugin of another major must still answer it; whether
      contribution calls and callbacks are messages on the one stream of `07:1151` or a
      service per definition; the encoding of errors across the boundary
      (`ErrInactiveEffect`, a cancelled context, `ErrInactive`, a refused registration);
      the digest of the first message, as question Q21 decides, given ADR-048 decision 1
      and ADR-047 decision 8; the form of the activation id, as question Q26 decides;
      the callback set, which holds `tools` (`08:697`); the deadlines, with the
      handshake deadline of 2 s against a running instance (plugin record §10;
      ADR-036:30), the bound of the proxy disposer, the deadline of a remote listener on
      a gate hook and the reconnect budget, and what brings a proxy back after it
      unloaded itself; the bounds on what an authenticated peer may send, since the peer
      is third-party code: the size of a message, the number of registrations of one
      activation, the number of callbacks in flight and the size of `Hello.diagnostics`,
      which both sides log; the identity of the instance and of the kernel on a
      single-tenant self-host, as question Q18 decides and inside the points of decision
      23: the form of each identity, where the key material lives under ADR-047
      decisions 13 to 15 and ADR-062:224-234, its lifetime and rotation, and how disable
      and remove revoke where no leader exists (`03:495`), with the new credential
      classes written into `00` §5.4 in the same change (`00:410-411`; `08:999`); the
      dial of the proxy under the lists of ADR-062 decisions 13 and 14; and the scope of
      the Python SDK: the supported interpreter versions, the concurrency model of
      `serve()`, the call context, the fixture and the command of `aiopsctl` it wraps,
      the version scheme and the dependency pins.
    - ADR-047, its S2b part: the layer type and media type of the image reference; the
      image and its digest, as question Q24 decides; the SBOM of remote code; the rows a
      remote artifact's bundle may hold, as question Q30 decides; the unit files, the
      endpoint table and the credentials as rows of the home table of decision 13
      there, with the one directory a unit binds (decision 25); the network of an
      instance and of a kernel container (decision 15 there; decision 24), and how
      `aiopsctl` names the kernel container.
    - ADR-048, its S2b part (decision 20 there): every `plugin.toml` key; whether
      `plugin.yaml` carries `entrypoint`, so that `aiopsctl` needs no reader of
      `plugin.toml`, and the closed grammar of that field and of the id as a unit
      takes them (decision 25); how `@contributes` names a key; where `configSchema`,
      `uses` and hook stages come from in Python, and which library, if any, derives the
      schema (decision 35); the constant of spoken majors that `plugingen` reads; what a
      manifest that lists `inproc` and `remote` packs and installs, which no exit step
      uses; and which fields the contribution-set clause of check 9 reads beyond those
      of ADR-048:433 (`durable`, `capabilities`, an `as: want` entry).
    - ADR-020, its S2b part: the record of a remote artifact, which has no `module`
      (decision 10 there); whether the attestation names the runtimes its suites ran
      in; `plugin verify` against a pulled image; the `signed` clause for a remote
      listener on a hook with a raw field (decision 7 there); and whether
      `allowUnsigned` (decision 8 there) admits an unsigned artifact of the runtime
      `remote`, which from S2b is a unit that runs an unsigned image, while the exit
      speaks of signed artifacts (`08:697`).
    - ADR-061, its S2b part: `plugin start`, with `Start` in the operations definition
      as a minor with a raised suite revision; `plugin new --lang python`; the form of a
      remote Go plugin, as question Q23 decides; the text and the exit code of each
      refusal of `plugin enable` (decision 17); what `plugin check` prints for a step it
      cannot run for want of a container runtime; and the forms of `plugin verify` and
      `plugin smoke` for remote code.
    - ADR-054, its S2b part: the register entries and importers of question Q5; the
      forms of an entry for a Python package, an interpreter and a plugin base image;
      the names that the name lint would refuse under `sdk/` and in a proto, as the
      names of the Python language, of gRPC and of Protocol Buffers; the reach of the
      name lint, the import boundary and suite neutrality over `python/`, `.proto`
      imports and generated code (decision 37); and the checks over the dependency set
      of `aiops-sdk` that question Q15 decides.
    - ADR-044, its S2b part: the release modules the builder serves, the suite binaries
      that drive a remote process, the writer of the new `serves` fields, and the gRPC
      codec exemption read again if the pin of `google.golang.org/grpc` moves (ADR-020
      decision 27).
    - ADR-034: G13 point 10, as question Q12 decides.

### The licence rule for the proto toolchain and the Python dependencies

35. **Nothing merges unread.** A new linked module, baked-in tool, image or data set
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
    | `grpcio` 1.85.4, wheel `cp312` `manylinux2014_x86_64`, SHA-256 `f279c333de857630e12ce2751672cb0825d1df2f6f2ec209d0d40f86a26e4ac3`; the gRPC runtime of the Python SDK | `METADATA` and `licenses/LICENSE` inside the wheel | `License-Expression: Apache-2.0`. The one licence file holds three texts: Apache-2.0, a BSD 3-Clause text ("Copyright 2016, Google Inc.") and MPL-2.0 for `./etc/roots.pem`, which the wheel ships as `grpc/_cython/_credentials/roots.pem`. It requires `typing-extensions~=4.12` and Python 3.10 or later. Its native extension is built from libraries of other projects whose licence texts the wheel does not carry |
    | `grpcio` 1.85.4, sdist, SHA-256 `45c4efd77dbfbb92cfebe493518c7e2a97ef3f4da2673508f81214b7493d7cca`; the sources of that native extension | `LICENSE` and the file headers under `third_party/` inside the sdist | The one `LICENSE` holds the same three texts. `third_party/` holds the sources of nine libraries of other projects (`abseil-cpp`, `address_sorting`, `boringssl-with-bazel`, `cares`, `re2`, `upb`, `utf8_range`, `xxhash`, `zlib`) and no licence file of any. A search of the file headers finds Apache-2.0 notices in `abseil-cpp` and in 986 of the 1,460 files of `boringssl-with-bazel`; BSD notices in `address_sorting`, `re2`, `upb`, `xxhash` and the test library inside `boringssl-with-bazel`; MIT in `cares` and `utf8_range`; and the notice of zlib in `zlib`. No file names the General Public License, and none carries an advertising clause. The search is not a reading: 286 files of `boringssl-with-bazel` outside its test library, 175 of them assembly, carry no such notice, and no licence text of the nine was read |
    | `typing-extensions` 4.16.0, wheel SHA-256 `481caa481374e813c1b176ada14e97f1f67a4539ce9cfeb3f350d78d6370c2e8`; required by `grpcio` | `METADATA` and `licenses/LICENSE` inside the wheel | `License-Expression: PSF-2.0`; the file is the Python licence stack (PSF version 2, BeOpen, CNRI, CWI and Zero-Clause BSD). PSF-2.0 is not on the list of L7 |
    | `protobuf` 7.36.2, wheel `cp310-abi3` `manylinux2014_x86_64`, SHA-256 `89f23aa53c24553a2416fd4fd1ec06f74fa42b14b546d8883128813f775bbfd2`; the message runtime of the Python SDK | `METADATA` and `LICENSE` inside the wheel | "3-Clause BSD License", "Copyright 2008 Google Inc."; it holds a native extension. It requires Python 3.10 or later |
    | `protobuf` 7.36.2, sdist, SHA-256 `497d0463ff3316681da6c0b9e8d06cb465d61abce00b613ab42226175644d1bb`; the sources of that native extension | `LICENSE` and the file headers inside the sdist | The same BSD 3-Clause text. Beside `python/` and `google/` it holds `upb/`, whose headers name that BSD licence, and `utf8_range/`, whose headers name "an MIT-style license that can be found in the LICENSE file"; the sdist carries no such file, so that text was not read |
    | `grpcio-tools` 1.85.4, wheel `cp312` `manylinux2014_x86_64`, SHA-256 `b24692cf7f11bd0f1fa992405f85d16e1fbb7c3f33f97916a6d54e7aadf892b0`; a candidate generator | `METADATA` inside the wheel, which carries no licence file | `License-Expression: Apache-2.0`, and no licence text at all. The wheel holds one native extension of 8 MB, `grpc_tools/_protoc_compiler`, a proto compiler built from the sources of other projects, and the `.proto` files of the well-known types. It requires `protobuf` from 7.35.1 below 8, `grpcio` from 1.85.4 and `setuptools` from 77.0.1 |
    | `pytest` 9.2.0, wheel SHA-256 `07acdcf88ec174f907dc01af70bf52d49b014bbab03faeeade31c96b18c2e588`; the test runner of the fixture `aiops_sdk.testing.host()` (`07:1212`) and of the template's test | `METADATA` and `licenses/LICENSE` inside the wheel | `License-Expression: MIT`, "Copyright (c) 2004 Holger Krekel and others". It requires `iniconfig`, `packaging`, `pluggy` and `pygments`, and below Python 3.11 also `tomli` and `exceptiongroup`; none of the six was read. It requires Python 3.10 or later |

    Not read, and each read before its first use:
    - the licence text of each library inside the native extensions of `grpcio` and
      `protobuf`, at the version that release bundles;
    - the six packages that `pytest` requires;
    - `setuptools`, if `grpcio-tools` is used;
    - the proto compiler release of question Q8 (d), with what its binary bundles;
    - the TOML reader and the YAML writer of `aiops-sdk manifest`: none is needed if
      ADR-041 sets the lowest interpreter at 3.11, whose standard library reads TOML, and
      the package writes the closed shape of `plugin.yaml` itself; any library taken
      instead is read first;
    - a library that derives the JSON Schema of `configSchema` (`07:142`) from a Python
      config type, if the S2b part of ADR-048 takes one;
    - the Python interpreter and the form in which CI and the exit fetch it, and a CI
      action that sets up Python;
    - the base image of the Python plugin image, with its layers (`08:1347` L13);
    - the build backend of `aiops-sdk`;
    - the tool that builds a plugin image in the exit, which is the build command of
      the environment's container runtime, and Compose, which licence:60 lists with no
      pin.

    Two recommendations below rest on readings that are not made yet, and each says so:
    question Q8 (d) on the compiler release, and question Q15 (a) on the libraries
    inside the two native extensions. grpc-go and protobuf-go are in the build list
    today only inside the `contains` of the sigstore-go entry
    (`tools/upstream-components.yaml:278-279`); a module that a package of the repository
    imports needs an entry of its own (ADR-054 decision 18), with the importers that
    question Q5 decides, and the scan of each artifact runs again on the packages the
    direct import adds. Remote plugins are separate programs and their own licences are
    unrestricted (`08:1350` L16); whether the dependency set of `aiops-sdk`, a
    first-party artifact (`08:715`), is held to L7 is question Q15.

### Line budgets

36. Physical lines of production Go, measured by `tools/kernelcheck lines` and enforced in
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

37. Added to `.github/workflows/ci.yml` and mirrored in the Makefile, as at S2a (ADR-060
    decisions 36 and 52):
    - for each module of question Q6, a step that builds, vets and runs its tests under
      `-race` in its own directory, as each nested module has today
      (`.github/workflows/ci.yml:66-85`), since the root `go test` enters no nested
      module; its require list under `kernelcheck modfile`; and its served `go.mod` copy;
    - the handshake freeze test, at the path question Q6 gives it;
    - the adapter parity check of decision 13, for the generated Go and the generated
      Python;
    - the definition lint with the `contrib` rule;
    - the name lint and suite neutrality (U1, U3), which read every file under `sdk/`
      today, the handshake proto included, extended to `python/`; and the import
      boundary (U2), which reads Go files only, extended to the imports of `.proto`
      files and of the Python under `python/`; each under the exception that question
      Q5 states (`00:268`, `:299`), with a case that fails on a name or an import
      outside it;
    - the Python job, which runs the tests of `aiops-sdk`, the fixture against the built
      `aiopsctl`, the manifest parity of every Python plugin of the repository and the
      Python line count, once on every interpreter version that ADR-041 supports;
    - the template job with the Python templates and the remote Go form;
    - `plugin check` and the T3 boot on the remote examples;
    - the S2b cells of the failure matrix, in the `test` job;
    - the cases of decision 28, the identity and isolation cases among them, and with
      question Q13 (a) the job that runs the systemd unit;
    - the register check with the new entries, and the check over the dependency set of
      `aiops-sdk` that question Q15 decides;
    - the caps of decision 36;
    - the job `s2b-exit`, whose Go binaries are race-built and which fails on a race
      report or a skip. Its Python author's machine holds no Go toolchain and no
      container runtime (decision 27), so steps P1 and P2 prove T0 to T2 without either;
    - the jobs `s2a-exit` and `s2a-exit-local`, which stay green after the `0.2` minor
      of the toy registries (decision 14).

    A case that needs the suite binaries of a release build runs on the `aiopsctl` of
    the test release, as the connector case of the templates does (ADR-060 decision 24).
    None pushes, publishes or holds a long-lived key. The exit jobs need what the S2a
    exit needs (s2a-report:471-472) and, in the pipeline part, the network to fetch the
    pinned inputs of decision 27.

## Consequences

- The S2b exit is decidable: eleven steps in three environments, the cases of decision
  28 and five matrix cells. With question Q9 (a) it holds two denials of check 3, one
  before install and one at the handshake; with Q9 (b) it holds one, at the handshake.
- The exit fails on an unauthenticated dial, on an instance with a route out and on a
  unit that runs another image than the signed artifact pins (decisions 23 to 25; steps
  C2 and C3).
- `boot` gains checks 3 and 9, the resolution of remote rows and the new `serves`
  fields, and stays one package. The gate part of every dump changes from "not evaluated
  until S2b" to the evaluated checks, and each golden is regenerated and reviewed in the
  commit that makes the change.
- gRPC and Protocol Buffers become direct imports of the repository, in the places
  question Q5 allows; the builder already builds with `grpcnotrace` and exempts the gRPC
  codec at v1.82.1 (ADR-044 decision 21).
- The SDK gains modules (question Q6), and every count of the SDK modules changes with
  the answer: in `00`, `07`, `08`, the README of `docs/architecture`, plugin record §2,
  the builder, and by dated additions in ADR-044 and ADR-060.
- New credential classes enter `00` §5.4 with the answer to question Q18, and item 25
  of `02` §6 closes.
- The one point where a kernel change may prove necessary is named: how a proxy built
  after boot takes a bound key (question Q4). It is gated by decision 3 and has 25 lines.
- `echoers` and `stampers` move to `0.2`; every manifest, fixture and golden that names
  `0.1` is regenerated in that pull request, and the S2a exit stays green.
- Python enters the repository and CI: a directory, a job, a pinned interpreter and
  register entries of new forms.
- `launch` and `operations` each gain a minor with a raised suite revision, `launch`
  with one more consumer-bound key under question Q20 (a), and `pluginApi` gains a
  minor.
- `dev` lists a second bundle and `aiops-core` counts one row fewer, with the
  recommended answers to questions Q2 and Q4 (decision 22).
- ADR-041 is written, provisional, and each S2b addition of decision 34 is accepted
  before its code merges.
- On acceptance this ADR enters the ADR table of `08` §3 and the next free number there
  becomes ADR-064. A proposed ADR is not listed there.

## Questions for the owner

Each question gives its options, the recommended one, why, and what the answer blocks. An
option that changes or defers a written line says so and names every line it edits, and
each such question holds the option that keeps the text as written. Six are needed
before any S2b code (decision 1): Q1, Q2, Q4, Q18, Q23 and Q24.

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
- **Answered 2026-11-27: (a)**, decision 38.

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
  first consumer. The provider's row ships in the bundle `aiops-selfhost`, its home in
  `00:292`, which `dev` lists after `aiops-core` (decision 22; `08:264`). The bound
  view, the leader and `sidecars-kubernetes` stay where they are. The lines that place
  the two at S4a are edited: `00:573`, `02:215`, `08:646`, `08:699`, community:45 and
  the S4a row of plugin record §16, which the answer amends (decision 4).
- (b) Install writes the endpoint into the config of the proxy row, and the proxy tracks
  availability itself until S4a. `03:72`, `07:1150`, `07:1158` and `08:398` gain "from
  S4a", and the proxy is rewritten at S4a.
- (c) S2b waits for S4a.
- Recommended: (a). It builds the texts as written, it is the rule that a definition
  lands with its first consumer, and nothing is rewritten at S4a. (b) weakens four
  written lines and (c) breaks the order of `08:609`. Blocks: the proxy, the unit that
  install renders, the refusal of `plugin enable` and the matrix cells; needed before
  any S2b code.
- **Answered 2026-11-27: (a)**, decision 39.

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
- **Answered 2026-11-27: (a)**, decision 40.

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
  lists each such value counts as `aiops/remote`, which the lists name once. The factory
  and every value it builds stay with the loader alone (ADR-059 decision 41; decision
  12), and a value takes no key from its manifest that those bindings would satisfy
  (decision 10). The bundle `aiops-core` holds no row of it and counts ten rows:
  `00:276`, `08:253` and `08:273` change the count and take `remote` out of the rows
  that arrive on the way to the release. The lines that say "an `aiops/remote` row" say
  "a proxy row" of the artifact's own plugin: `00:138`, `02:191`, `03:72`, `03:490`,
  `03:571`, `07:1140`, `07:1149`, `08:437` and plugin record §11, which the answer
  amends (decision 4). If a value built after boot cannot take the bindings of an entry
  without a kernel change, that change is an ADR-034 addition under decision 3.
- (b) Every such row is a row of the plugin `aiops/remote`, which joins the instantiable
  list (`07:137`; `08:393`), with the artifact's id in its config, as `07:1140` words it.
  One plugin id then resolves to a different value per row, the gate reads two manifests
  for one row, and "its own plugin" of check 8 no longer covers a remote artifact.
- (c) Both: `aiops-core` holds one `remote` row that provides the dial under a new
  consumer-bound key, and each installed row names its own id and needs that key. This
  adds a definition that no document names.
- Recommended: (a). It is what `03:358` and check 8 say, the dump shows each remote row
  under its own id and origin, and the third party's id enters no binding list. Its open
  point, how a later value takes the bindings, is named and gated. Blocks: the proxy,
  `FromManifest`, the resolution of remote rows and install; needed before any S2b code.
- **Answered 2026-11-27: (a)**, decision 41.

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
  `aiops-core` wraps gRPC, as ADR-054 decision 23 does for the telemetry libraries.
- Recommended: (a). The exception names a contract that `00` already decided, and both
  rules of ADR-054 stay whole. (c) adds a definition whose only purpose is to hide a
  protocol that the proto packages already name. Blocks: the first import of either
  module, so any protocol code.
- **Answered 2026-11-27: (a)**, decision 42.

**Q6. How many modules hold the remote packages, and where is the kernel side of the
protocol?** R2 keeps both packages out of the require list of `sdk` and fixes their home
at S2b (`07:60`); ADR-060 decision 54 says they need a nested module. Generated Go for
the handshake imports protobuf-go, so the handshake package leaves the module `sdk`, and
its freeze step moves (`.github/workflows/ci.yml:86-90`; `Makefile:161`). The documents
count three SDK modules (`00:263`; `07:23`, `:59` R1; `08:1205`, `:1207`; line 16 of the
README of `docs/architecture`; plugin record §2, row 5), and `07:42` and plugin record
§14 put the kernel side that `aiopsctl` carries in `sdk/remotehost`.
- (a) Two nested modules, rooted at `sdk/remote` and at `sdk/remotehost`, each with a
  closed require list, and the kernel side of the protocol a package of the first
  (decision 9). Every written path stays (`07:32`; `08:1218`). The lines above say five
  SDK modules, `07:51` names the two for the author of a remote Go plugin, `07:42` and
  plugin record §14 say "the kernel side of the protocol", and the builder serves six
  modules. Dated additions say so in ADR-044, whose decisions 20 and 23 count the served
  modules, and in ADR-060, at its decisions 21 and 54. ADR-056 and ADR-057 decision 1
  record the three-module boundary as it stood when each was accepted and are not
  edited. The answer amends plugin record §2 and §14 (decision 4).
- (b) One nested module rooted at `sdk/remote`, with the plugin-side host at
  `sdk/remote/host`. As (a) with four SDK modules, and `07:32`, `08:1207`, `08:1218`
  and plugin record §11, §14 and §16 change the path of `sdk/remotehost`.
- (c) The module `sdk` gains protobuf-go and grpc-go in its require list. This rewrites
  decision 7 and edits `00:263`, R2, ADR-054 decision 13 and ADR-060 decisions 34 and
  54, and every author's module graph gains both.
- (d) As (a), with the kernel side of the protocol kept in the module of
  `sdk/remotehost`, as `07:42` and plugin record §14 word it. The proxy plugin, a
  first-party row, and `aiopsctl` then import the module of the plugin-side host.
- Recommended: (a). It keeps every written path and the closed list of `sdk`, an
  in-process author's graph gains nothing, and the kernel imports nothing that exists to
  run inside a plugin. Blocks: the first generated Go file.
- **Answered 2026-11-27: (a)**, decision 43.

**Q7. Where does the generated adapter of a `contrib` definition live?** `07:28`,
`07:578` and `08:1215` put the proto, the message types and the generated adapter in
`sdk/def/<name>/v<N>`, and `00:44` says "The proto and adapter of a contribution
definition are written with the definition". That directory is in the module `sdk`,
which takes no protobuf dependency (ADR-060 decision 34; `07:60` R2), and a definition
imports no upstream module (`07:527`), which the import boundary enforces
(`internal/vet/imports.go:195-198`).
- (a) The proto, which imports nothing, stays beside the definition (`08:1255`). The
  generated Go, wire types and adapter, lives in the remote module, under
  `sdk/remote/def/<name>/v<N>`. Both are still written in the change that writes the
  definition, which is what `00:44` requires; the Go types of a definition stay plain
  Go, and the adapter converts. `00:44`, `07:28`, `07:578` and `08:1215` say where the
  generated Go lives.
- (b) The generator writes plain Go with a wire codec of its own into the definition
  package, with no protobuf import, and gRPC stays in the remote module. `07:28` stands;
  the repository then maintains an encoder of the protobuf wire format.
- (c) The definition packages import protobuf-go, by an owner exception to `00:263`,
  `00:268`, `07:527` and ADR-054 decision 13. This rewrites decision 7, as Q6 (c) does.
- Recommended: (a). It keeps the module rule and the upstream rule, and it uses the
  standard runtime, not a private encoder. Blocks: the adapter generator and the
  `contrib` mark of the toy registries.
- **Answered 2026-11-27: (a)**, decision 44.

**Q8. What is the source of an adapter, and which tools generate it?** `07:1199` says the
proto and the adapter are generated; `00:44`, `07:578` and `08:1255` say the proto is
written with the definition and the message types are generated from it. No document
names a compiler. The frozen handshake file takes no `go_package` option. A tool that CI
runs is "an unmodified upstream release, fetched by pinned version and checksum or image
digest" (`08:1374`).
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
- (d) As (a) for the sources, with the proto compiler's own release as the one compiler
  for both languages: an unmodified binary fetched by pinned version and checksum. It
  writes the Python message code itself and the Go wire code through `protoc-gen-go`.
  The in-repository generator writes everything else, the Python service glue on the
  public API of `grpcio` included, so neither `grpcio-tools` nor `setuptools` enters the
  repository.
- Recommended: (d), on one condition. It matches the three lines that have the proto
  written, both languages come from one compiler run on one file, it is the form
  `08:1374` gives a tool, and it needs two packages fewer than (a). The wheel of
  `grpcio-tools` at the pin embeds a compiler and carries no licence text (decision
  35). The condition: the compiler release is not read yet. It is read at its pin
  before its first use; if that reading finds a text that the tool rule does not
  admit, the owner is asked again with it, and (a) is the other way, on the same
  condition for its wheel. Blocks: the adapter generator.
- **Answered 2026-11-27: (d), on the condition that the compiler release is read at its
  pin before its first use**, decision 45.

**Q9. Does check 3 have a clause that needs no connection, and what does `serves`
gain?** The gate text names the handshake only (`08:501`); ADR-048:428 has check 3 read
`requires.remoteProtocol`; `serves` has the shape of `requires` (`08:904`; `00:267`) but
carries no protocol majors and no `remote` mark of a definition, and its reader refuses
an unknown field (`boot/distribution.go:241-242`).
- (a) `serves` gains `remoteProtocol`, the majors the binary speaks, and per served
  definition its `remote` mark with its three values, `contrib`, `callback` and `no`
  (`tools/deflint/main.go:335`): check 9 reads `contrib` for a registration and
  `callback` for a `uses` entry (ADR-048:433). Check, pack, the offline gate, install
  and boot deny a manifest whose `requires.remoteProtocol` shares no major with it; the
  handshake evaluates every clause (decision 16). The exit then holds both denials, D1
  and D2. The reader and the builder change in one commit; `distVersion` stays 1, since
  no distribution manifest exists outside the tests of the repository.
- (b) Check 3 runs at the handshake only. Every other entry point reports it "not
  evaluated: no connection", `serves` gains only the `remote` mark, with its three
  values, that check 9 needs, and the exit holds one denial of check 3 (step D1 of
  decision 27).
- Recommended: (a). A plugin that cannot connect is refused before an operator starts it,
  the offline gate answers for remote code as it does for in-process code, and `08:904`
  is built as written. Blocks: checks 3 and 9, and steps D1 and D2 of the exit.
- **Answered 2026-11-27: (a)**, decision 46.

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
- **Answered 2026-11-27: (a)**, decision 47.

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
- **Answered 2026-11-27: (a)**, decision 48.

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
- **Answered 2026-11-27: (a)**, decision 49.

**Q13. Which deployment-unit targets does S2b build, and is each one run?** Install
renders the unit per target: a compose fragment started with `plugin start`, a systemd
unit, or a Helm values entry (`08:1292`; `02:313`). `deploy/compose` and `deploy/helm`
arrive at S7 (ADR-060 decision 22), and the S2a clean machine holds no Compose. The unit
is what keeps an instance off the home, off the reload socket and off the network
(decisions 24 and 25), and the environments of the exit are containers with no systemd.
- (a) Compose and systemd, and both are run. The compose fragment is a file of its own
  under the home that `plugin start` runs, and the exit uses it. The systemd unit runs
  the same image by digest through the container runtime of the host, and a CI job of
  its own starts it on a runner whose init is systemd and asserts what steps C2 and C3
  assert of the fragment: the running digest, the user, the mounts and the network. The
  Helm entry waits for `deploy/helm`.
- (b) Compose only. The systemd unit waits, with the Helm entry, for a step that can run
  it, and `08:1292`, `02:313` and plugin record §11 name that step.
- (c) All three, with a minimal chart written at S2b.
- Recommended: (a). Both forms of a single-host install are rendered and each is proven
  by running it; a unit that is only compared as text is untested as a fence. If no job
  can run a systemd unit, the answer is (b) and not a rendered text. Compose is read and
  pinned first (decision 35). Blocks: the install of remote code, `plugin start` and the
  exit.
- **Answered 2026-11-27: (a)**, decision 50.

**Q14. What do the callbacks and the remote listeners run on at S2b?** `sdk/remotehost`
provides definitions through callback clients (`07:1152`), `aiops-sdk` has `@listens`
(`07:1209`), and ADR-041 fixes the callback set with `tools` (`08:697`). The set has nine
callbacks (`07:1171-1180`): telemetry, `secrets`, `kv`, `blob`, `audit`, `llm`, `tools`,
`tenants` and `credentials`. No definition of any of them and no hook marked remote
exists before S3, and a part lands with its first consumer (`08:680`).
- (a) S2b builds the callback path and the remote listener path and proves them on a
  test definition marked `callback` and a test hook marked remote, as ADR-060 decision
  15 proved the signed class on a test definition. The one test definition stands in
  for all nine callbacks; nothing of it ships in a release table, and each real
  callback arrives with its definition (decision 26).
- (b) ADR-041 writes the set, and S2b builds no callback and no listener path; `@listens`
  and the callback clients arrive with the first real definition, at S3 and later.
- (c) `kv`, `blob` and `secrets` move to S2b as the first callbacks.
- Recommended: (a). `07:1152` and `07:1209` are built at the step that names them, the
  protocol is exercised before S5 ratifies it, and no product definition moves. Blocks:
  ADR-041, `sdk/remotehost` and the Python SDK.
- **Answered 2026-11-27: (a)**, decision 51.

**Q15. Which licence rule holds the dependency set of `aiops-sdk`?** L7 lists the
licences allowed in process, and remote plugins stay unrestricted (`08:1341`, `:1350`).
`aiops-sdk` is a first-party artifact (`08:715`) that runs only inside a remote plugin.
Its gRPC runtime requires `typing-extensions` under PSF-2.0, which L7 does not list,
ships a certificate file under MPL-2.0, which L8 conditions, and is built from nine
libraries of other projects whose licence texts are not read yet (decision 35).
- (a) The set is held to L7, with PSF-2.0 recorded by name for `typing-extensions` and
  the MPL-2.0 file under the condition of L8. Each package has a register entry and a
  licence-record row, and a check fails on a package of the set that has none.
- (b) L16 covers it: the set is recorded and read, and no allow-list gates it.
- (c) `aiops-sdk` avoids the gRPC runtime that brings these.
- Recommended: (a), on one condition. A first-party artifact is held to the first-party
  rule, and an exception is recorded by name, as L7 requires. The condition: the texts of
  the libraries inside the two native extensions are read before the first Python
  dependency merges. The search of their file headers found no notice that L7 denies
  (decision 35), but a search is not a reading. If a text that L7 denies until reviewed
  is found, the dependency does not merge and the owner is asked with that text, with
  (c) as the other way. Blocks: the first Python dependency.
- **Answered 2026-11-27: (a), on the condition that the licence texts of the bundled
  libraries are read before the first Python dependency merges**, decision 52.

**Q16. Do generated files count against a line cap?** `kernelcheck lines` counts every
Go file of a package (ADR-060 decision 35). The repository already commits one generated
Go file, `dist/modules.go`, which `plugingen` writes and a parity check in `make check`
and CI holds to its source (ADR-060 decision 20, note of 2026-11-25); its package has no
cap. S2b commits generated Go inside capped packages for the first time.
- (a) A file that the adapter parity check regenerates is listed and summed apart and
  counts against no cap, as `dist/modules.go` counts against none today. The generator
  has a cap of its own.
- (b) Generated files count, and the caps are sized for them.
- Recommended: (a). A cap bounds what is written and reviewed by hand, parity already
  holds a generated file to its source, and it is the treatment the one generated file
  of S2a has. Blocks: the first generated file.
- **Answered 2026-11-27: (a)**, decision 53.

**Q17. Six readings that reconcile wording.** None changes what a line requires or when
it is built: each says which of two written lines governs a point, or fills a point that
the lines leave open. Every change and every deferral of a written line is a question
of its own, Q25 to Q32.
1. **Scope.** In a multi-tenant profile a remote row is `scope: tenant` (`07:133`).
   Reading: the remote toy plugins and the templates of S2b declare `scope: [process]`,
   the shared instance of `07:1194`.
2. **The Unix socket.** `plugin check` drives a plugin over a Unix socket (`07:1577`),
   and the server dials over mutual TLS (`07:1159`). Reading: on the socket the
   handshake and check 3 run as on a server, the identity is the process that the runner
   started, and only the check runner opens such a socket (decision 23).
3. **Two wordings.** `07:1212` says "the host side of the protocol" for what `aiopsctl`
   carries, the kernel side, and `08:1218` says "the host side of a remote plugin" for
   `sdk/remotehost`, the plugin-side host of `07:32`. Reading: both stand, and this ADR
   says "the kernel side of the protocol" and "the plugin-side host". Plugin record §16
   says the first message "already carries the tenant binding", while `07:1128` takes
   the tenant from the connection certificate. Reading: the binding is the
   connection's, and plugin record §16 follows `07:1128`.
4. **Who refuses at `plugin enable`.** `08:398` has `plugin enable` refuse while the
   endpoint is missing from the table or does not answer the handshake, and `08:448-449`
   and `02:213` have the server process dial. Reading: `aiopsctl` refuses a missing
   endpoint before it submits, and the refusal for an instance that does not answer is
   the server's failed verdict, which `aiopsctl` prints with the exact command.
   `aiopsctl` never dials an instance (decision 17).
5. **T1 in the remote runtime.** `plugin check` runs T1 and T2 in every runtime the
   manifest lists (`07:134`), and `07:1577` words the socket-driven T1 for a non-Go
   plugin. Reading: for the runtime `remote`, step 4 starts the entrypoint and drives it
   over the socket in every language, and a Go plugin's own tests run under `-race` as
   well (decision 17).
6. **Step 3 on a Python plugin.** T0 static names Go imports, plugin vet and the config
   examples (`07:1576`), and `00:299` holds manifest parity and the schema examples in Go
   and Python. Reading: on a Python plugin step 3 validates the config examples against
   `configSchema`, and `aiopsctl` parses no Python (decision 17).
- (a) Accept the six readings, each landing with the code that builds it.
- (b) Answer each item on its own.
- Recommended: (a). Blocks: the code each reading names.
- **Answered 2026-11-27: (a), all six readings**, decision 54.

**Q18. What identifies the kernel and an instance on a single-tenant self-host?** The
dial is mutual TLS and `00` ties it to S2b (`00:44`; `08:449`; `07:1159`), the row and
the tenant come from the connection certificate (`07:1128`), and `08:697` makes "the
instance identity on a single-tenant self-host" a deliverable of S2b. `00:410-411` holds
only the hosted classes, issued by the cell's plugin CA, which no self-host has
(`02:483`, item 25; `02:213`, `:336`; `03:490`). Decision 23 fixes what any answer must
hold, and ADR-041 writes the answer out.
- (a) Pinned key pairs and no issuer. `plugin install` makes one key pair and a
  self-signed certificate for the instance, which names the plugin id, and on first need
  one for the kernel. Each side trusts exactly the other's certificate: the kernel holds
  the pin of each installed instance in the home, and the unit gives the instance its
  own key and the kernel's certificate. `plugin disable` and `remove` revoke by deleting
  the pin, which the proxy reads at every dial, and `remove` deletes the key. No key of
  the site can mint an identity.
- (b) A site issuer. The first install creates a certificate authority under the home,
  which issues the kernel's certificate and one certificate per instance, with the
  lifetime and the rotation of `00:410`. Revocation is a list that the kernel reads at
  every dial. The home then holds a key that can mint any identity of the site. S4b
  brings the same shape with its test CA (`03:490`).
- (c) The operator's issuer. Install takes the paths of certificates that the operator's
  own authority issued, and the project creates none. Every self-host then runs an
  authority before its first remote plugin.
- Recommended: (a). It is the least key material that authenticates both sides,
  revocation is one local write with no leader, and nothing has to be issued or rotated
  by a service that a single host does not run. The issued form arrives where there are
  several tenants and a leader to issue it, at S4b. Every option adds its credential
  classes to `00` §5.4 and closes the open point in `02:213`, `02:336`, `02:483`,
  `03:162`, `03:490` and `07:1159`. Blocks: ADR-041, the proxy, the unit, `plugin
  start`, `disable`, `remove` and the exit; needed before any S2b code.
- **Answered 2026-11-27: (a)**, decision 55.

**Q19. What does a unit render for a manifest that lists network entries?**
`capabilities.network` is "rendered as the network policy of a remote instance"
(`07:135`; `07:1109`; `08:388`; plugin record §11), each entry a host with an optional
port or a `config:` pointer into the row's config. Decision 24 builds the deny side at
S2b. No target takes a list of host names as it stands: a compose file has an internal
network and no allow-list by host, a systemd unit filters by address, and a network
policy of Kubernetes by address block. The two toy plugins list no entry.
- (a) A list is refused until it can be enforced. `plugin install` refuses a remote
  artifact whose manifest lists a `network` entry, names the entry and writes nothing,
  and `plugin check` warns of it. The allow-list side is built with its first consumer,
  S5 at the latest (decision 26), by the ADR of that step. No instance ever has a wider
  network than its manifest lists. This defers half of a written line: `07:135`,
  `07:1109`, `08:388` and plugin record §11 gain the step.
- (b) The allow-list is enforced at S2b on each target of question Q13. Install
  resolves each entry, a `config:` pointer from the row's config, and renders a filter.
  A compose host then needs a filtering component, a new upstream to read and register
  (decision 35), and the unit is rendered again whenever the config or an address
  changes.
- (c) A manifest with entries gets a unit with an open route out. Install and boot print
  the declared list and say that the fence is the operator's, as `07:1188` words it for
  a static unit. This weakens `07:135`, `07:1109`, `08:388` and plugin record §11 for
  the compose and systemd targets.
- Recommended: (a). It is the one option under which no instance runs outside its
  declared network, it builds nothing before a consumer needs it (`08:680`), and it
  leaves the form of the filter to the step that has a plugin to prove it on. (c)
  weakens the written isolation. Blocks: install and the unit; needed before group 3 of
  decision 30.
- **Answered 2026-11-27: (a)**, decision 56.

**Q20. Through which key does the proxy reach the handshake entry of the gate?** The
gate is `boot` code and a plugin imports no `boot` package, so the proxy reaches it
through a key of the launch definition (decision 16). The launcher's toolbox is what
`ProfileContext.Toolbox()` returns (ADR-061 decision 21), and `profileContext` is
consumer-bound, class PC, because its `Commit` changes what the tree runs; its consumers
are four first-party rows (ADR-059 decision 40; `dist/dist.yaml:47`). The proxy is the
fiber that parses what a third-party process sends.
- (a) A new consumer-bound key of the launch definition that carries the handshake
  entry of the gate and nothing else. `boot` provides it, and the release manifest
  lists `aiops/remote` as its one consumer. The count of consumer-bound keys rises by
  one wherever it is stated, as in row 45 of plugin record §15, which the answer amends
  (decision 4), and `dist/dist.yaml` gains the line. The check runner keeps the toolbox
  that the operations row already resolves.
- (b) `aiops/remote` joins the consumers of `profileContext` in `dist/dist.yaml:47`, and
  the request of the toolbox gains the handshake inputs. No key is added, and every
  proxy fiber then holds `Commit`.
- Recommended: (a). The fiber nearest to third-party input holds one function that can
  only answer a verdict, and the owner-accepted consumer list of a key that can commit
  a composition does not grow. Blocks: the proxy and check 3 at the handshake; needed
  before the proxy.
- **Answered 2026-11-27: (a)**, decision 57.

**Q21. Which digest does the first message carry, and what does the kernel compare it
with?** The first message carries the "manifest digest, which must equal `plugins.lock`"
(`07:1128`; `03:489`; `08:501`). The lock holds the digest of the artifact's OCI
manifest (ADR-047 decision 8), ADR-048 decision 1 says the handshake digest names
`plugin.yaml`, and an image cannot hold the digest of the artifact that names it, since
the artifact pins the image.
- (a) The first message carries the SHA-256 of the `plugin.yaml` that the plugin was
  built with. The kernel compares it with the `plugin.yaml` that install kept, whose
  descriptor chains to the locked digest through `artifact.json`, the chain that check
  5 verifies at every composition (ADR-047 decision 18). `07:1128`, `03:489` and
  `08:501` say "must equal the digest of the manifest that `plugins.lock` pins".
- (b) Install hands the locked digest to the instance through the unit, and the
  instance sends it back. The three lines stand as written. The comparison then proves
  only that the unit was rendered for that artifact, not what the image was built from.
- Recommended: (a). It is the one form that an image can hold, and it tells an instance
  built from another manifest than the locked one. Under either answer the digest is a
  drift check and authenticates nothing, since the instance reports it itself: the
  authentication is the credential of decision 23 and the image digest that the unit
  runs (decision 25). Blocks: ADR-041 and the digest clause of check 3.
- **Answered 2026-11-27: (a)**, decision 58.

**Q22. How does `08:495` hold for the clauses of check 3 that need a connection?**
`08:495`: "boot and every live generation evaluate every clause on every row", and
`profile render` "evaluates what boot evaluates" but the module tie of checks 5 and 10.
The digest and registration clauses of check 3 need a dial. Decision 16 makes the
handshake part of boot and of every live generation, which covers every enabled remote
row. A disabled remote row mounts no proxy, and `aiopsctl`, which runs `profile render`,
holds no kernel credential (decisions 17 and 23).
- (a) For a disabled remote row, and in `profile render`, the two clauses are reported
  "not evaluated: no connection", and they are evaluated when the row is enabled, inside
  that generation. `08:495` gains the two exceptions beside the module tie.
- (b) `08:495` stands to the letter: boot dials the instance of every disabled remote
  row to evaluate the clauses, and `profile render` dials with a kernel credential of
  its own. A disabled row then causes a dial, which `08:398` does not expect, and a
  second holder of the kernel's key exists.
- Recommended: (a). No row becomes ACTIVE without every clause evaluated, which is what
  the line protects, and no credential leaves the server. Blocks: checks 3 and 9 at
  boot and the goldens of `profile render`.
- **Answered 2026-11-27: (a)**, decision 59.

**Q23. How is a remote Go plugin made, and which runtimes does its manifest list?** The
documents know `plugin new <registry> --lang go|python` (`07:1556`; ADR-061:934), and
`runtimes` is written by hand (`07:134`). `sdk/remotehost` serves a Go `Plugin` value as
a remote plugin (`07:1198`), and some `main` must call it. No line says what writes that
`main`, and step G1 of the exit needs the command.
- (a) A flag: `plugin new <registry> <id> --lang go --runtime remote`, with `inproc` the
  default, so the S2a form is unchanged. The remote form writes the same plugin package,
  a `main` that serves it through `sdk/remotehost`, a manifest with `runtimes:
  [remote]` and the file from which its image is built (decision 19). Its `requires`
  holds `remoteProtocol` and `definitions` and no `pluginApi` (ADR-048 decision 12).
  `07:1556` and the table of ADR-061 gain the flag.
- (b) One Go template for both runtimes: every Go plugin from the template has the
  `main` and lists `runtimes: [inproc, remote]`. Every Go author's module graph then
  holds the remote modules and gRPC, and what such a manifest packs and installs must
  be settled first (decision 34).
- (c) No template form: the author adds `remote` and writes the `main` by hand from a
  documented example, and the template job proves no remote Go template.
- Recommended: (a). An in-process author's graph gains nothing, the template job covers
  the remote form as `07:1568` requires of every template, and the case of decision 28
  still proves one `Plugin` value in both runtimes. Blocks: the Go template, step G1 and
  the template job; needed before any S2b code, since the exit test is written first
  (decision 29).
- **Answered 2026-11-27: (a)**, decision 60.

**Q24. Who builds the plugin image, and how does `plugin pack` learn its digest?** The
artifact carries the image digest (`08:393`), step 7 of `plugin check` "needs [a
container runtime] when it packs an image" (`07:1570`), and T3 and `pack` need one
(plugin record §14). No line says who builds or pushes the image, and steps G2 and P2
of the exit need both.
- (a) `plugin pack` builds it. It runs the build file of the plugin directory on the
  container runtime at hand, writes the image beside the artifact as an OCI layout and
  records the digest of that image; `plugin push` pushes the image and then the
  artifact. The command that signs for the digest is the one that computed it.
- (b) The author builds and pushes the image with any tool, and `plugin pack --image
  <reference>` resolves its digest at the registry and records it. `pack` then needs the
  registry and no container runtime, and `07:1570` and plugin record §14 change.
- Recommended: (a). It is what the two written lines say, the author runs the same
  three commands as for every other artifact, and no step trusts a digest that another
  tool reported. Blocks: `plugin pack` of remote code and steps G2 and P2; needed before
  any S2b code, for the reason of Q23.
- **Answered 2026-11-27: (a)**, decision 61.

**Q25. From when must a change inside `aiops.remote.v0` be additive?** A change of
written text. `07:1126` and community:159: inside a major, changes are additive.
`07:67` (R9): the protocol is provisional at S2b and ratified at S5 and at S4b. The
frozen handshake is "frozen-additive from its first release" (`07:1127`), and rule 1
names no start.
- (a) As written, from the first commit: every change to `aiops.remote.v0` after S2b
  merges is additive, the ratifications of S5 and S4b included. A ratification that
  cannot be additive would need major 1 before platform 1.0, which `07:1126` rules out,
  so it would come back to the owner.
- (b) The additive rule binds from the first community release that ships major 0.
  Before it a change is made by an ADR-041 addition, with the generated code and every
  plugin of the repository in the same pull request. `07:1126` and community:159 gain
  "from its first release", the words of `07:1127`.
- Recommended: (b). Nothing is published before L14 clears and no plugin exists outside
  the repository, "provisional" means nothing if no message can change, and (a) can
  force a choice between two written lines at S5. Under either answer both sides ignore
  unknown fields, calls and feature names from S2b (decision 5). Blocks: ADR-041.
- **Answered 2026-11-27: (b)**, decision 62.

**Q26. What is the activation id?** A change of what two written lines mean. `07:1161`
and plugin record §11: "An activation id is (replica id, epoch)". The kernel shows no
epoch to a plugin's `Apply`, and no replica id exists before S4b. Decision 5 needs the
id to differ for every activation of every boot, so that a `Register` from a superseded
activation is refused. A process id does not give that: a kernel in the container form
of ADR-047 decision 15 is process 1 at every boot.
- (a) As written, with the kernel's epoch: `Apply` gains an accessor for the fiber's
  epoch, an ADR-034 addition inside the 25 lines of decision 3, and the replica id is a
  random value drawn at boot until S4b defines it.
- (b) The replica id is a random value that the process draws at boot, and the epoch is
  the proxy's own count of its activations in that process. No kernel accessor.
  `07:1161` and plugin record §11 gain the clause, and the record is amended (decision
  4).
- Recommended: (b). It gives the property that the id exists for, across restarts too,
  and the kernel does not change. Blocks: ADR-041 and the proxy.
- **Answered 2026-11-27: (b)**, decision 63.

**Q27. Are the two stubs of the matrix images or processes?** A change of written text.
`08:463` boots the binary through every cell "with fault plugins ... and stub remote
images (one hangs a call, one ignores `Deactivate`)". The matrix runs in the `test` job
and in `make s1-suites` with no container runtime, and the kernel launches nothing.
- (a) As written: the stubs are images, the harness starts them on a container runtime,
  and the matrix jobs need one from S2b.
- (b) The stubs are programs that the harness builds and starts as its own processes,
  each with the identity of decision 23, and `08:463` says "stub remote plugins". The
  image path is proven by the exit.
- Recommended: (b). A cell asserts what the kernel does when a remote misbehaves, which
  an image adds nothing to, and the matrix keeps running wherever `make check` runs.
  Blocks: the matrix cells.
- **Answered 2026-11-27: (b)**, decision 64.

**Q28. On which row does the disposer cell run at S2b?** The cell of
`matrix.yaml:689-694` is the live-recomposition cell of "A tenant-scope disposer passes
its deadline (a remote that ignores `Deactivate`, a slow client close)" (`08:486`). It is
marked `since: S2b`, and every remote row of S2b is at process scope (`07:1194`).
- (a) At S2b it runs on a process-scope remote row whose instance ignores `Deactivate`:
  the proxy's disposer returns at its bound (`07:1196`), the outcome is "Reported" and
  the process is not replaced. S4b runs it again in a tenant tree. No line is edited.
- (b) The cell moves to S4b with the tenant trees, and S2b proves the bound of the
  proxy disposer by a case of decision 28. `matrix.yaml` is edited.
- Recommended: (a). The cell passes from the step it is marked with, and what it asserts
  at S2b, a disposer that does not wait on the remote, is the same at either scope.
  Blocks: the matrix cells.
- **Answered 2026-11-27: (a)**, decision 65.

**Q29. What does a row do after a denial at the handshake?** A denied row is detached as
disabled (`08:510`), and the matrix row "Remote unreachable or handshake mismatch"
expects PENDING or FAILED with reconnect for an optional row at boot, a stop for a
required one, a failed verdict live and a breaker while running (`08:477`).
- (a) A denial at admission detaches the row, as `08:510` says. A denial at the
  handshake fails the proxy with check 3 named, and the row follows the matrix row.
  `08:510` gains that clause.
- (b) A denial at the handshake also detaches the row as disabled, at boot and live.
  The matrix row then covers an unreachable instance alone, and `08:477` and four cells
  of `matrix.yaml` are edited.
- Recommended: (a). The matrix is the one failure text and its four cells are marked S2b
  as they stand, and a mismatch ends when the operator starts the right image, with no
  new generation. Blocks: the proxy and the matrix cells.
- **Answered 2026-11-27: (a)**, decision 66.

**Q30. May the bundle of a remote artifact hold more than one row?** A narrowing of
written text. An artifact holds "a bundle of one or more rows" (`08:393`; `07:1850`;
plugin record §11), and check 8 admits any number of rows of the artifact's own plugin
(`08:506`). There is one proxy row per remote instance (`07:1140`; `03:72`) and one
shared instance per plugin on a self-host (`07:1194`).
- (a) As written: a remote bundle may hold several rows of its plugin. Each is a proxy
  with a connection and an activation of its own to the one shared instance, which the
  protocol allows (`07:1152`), and `07:1140` and `03:72` then say one proxy row per
  connection.
- (b) At S2b the bundle of a remote artifact holds exactly one row. `plugin check`
  refuses another, and so does the gate at every entry point, under check 8, whose text
  gains the clause. `08:393`, `07:1850`, `08:506` and plugin record §11 say so for
  remote code.
- Recommended: (b). Two written lines give one row per instance and one instance per
  plugin, and several activations of one instance are the work of S4b, where the
  question can be reopened with the replicas. Blocks: the bundle of a remote artifact
  and check 8.
- **Answered 2026-11-27: (b)**, decision 67.

**Q31. When does a shared unit stop, and what does `remove` delete?** `plugin disable`
run on the host also stops the unit (`07:1195`; `08:1292`), disable and remove revoke
the instance's credentials first, the operator removes the unit (`03:495`), and one
instance serves the site, where several profiles may name one plugin (ADR-047 decision
7).
- (a) `disable` revokes the credential and stops the unit when no enabled row of any
  profile of the site names the plugin; while another profile's enabled row names it,
  `disable` unloads the proxy of its own profile and leaves the instance. `remove`
  deletes the files that install rendered, the credential among them, with the other
  kept files of ADR-047 decision 10, when no profile names the id. Removing the unit
  from an orchestrator stays the operator's. `07:1195` and `03:495` gain the clause for
  a plugin that another profile still runs.
- (b) To the letter: `disable` always revokes and stops the unit, and a row of another
  profile that names the plugin fails until its operator starts the unit again.
- Recommended: (a). One profile's operator does not stop a plugin that another profile
  runs, and where one profile names the plugin, the usual case, (a) is the written
  behaviour. Blocks: `plugin disable` and `remove` of remote code.
- **Answered 2026-11-27: (a)**, decision 68.

**Q32. Is the handover of new remote code built at S2b?** A deferral of written text.
`00:490`, `03:494` and `08:194`: "Remote: live; the new instance is handshaken before
the old contributions are removed". A site holds one digest per plugin id, and a new
version arrives "by `update` at S6b, or by `remove` and `install`" (ADR-047 decision 7).
- (a) As written, at S2b: a command installs a second digest of an installed id beside
  the first, starts its instance, handshakes it and only then unwinds the old
  registrations. That command is `plugin update`, which ADR-047 decision 7 places at
  S6b with the cohort update of `08:633`, and it needs two digests of one id on a site,
  which the same decision refuses.
- (b) The handover is built with `plugin update` at S6b, and `00:490`, `03:494` and
  `08:194` gain "from S6b, with `plugin update`". At S2b remote code is live in the
  first half of the sentence: it is installed and enabled with no restart (decision
  12). A new version arrives by `remove` and `install`, with its contributions absent
  in between.
- Recommended: (b). The accepted ADR-047 decision 7 already places the command at S6b
  and holds a site to one digest, and (a) would pull `plugin update` forward and reopen
  that decision. The owner decides, because `00:490` is a line of `00`. Blocks: nothing
  at S2b under (b); the commands and the proxy under (a).
- **Answered 2026-11-27: (b)**, decision 69.

## Owner answers of 2026-11-27

The project owner accepted this ADR on 2026-11-27 and answered every question of the
section "Questions for the owner" on the same date, each with its recommended option,
two of them on a condition (decisions 45 and 52). Each decision below names the question
it answers. Where a decision above leaves a point to a question, the decision here
settles it. The six answers that decision 1 needs before any S2b code, Q1, Q2, Q4, Q18,
Q23 and Q24, are decisions 38, 39, 41, 55, 60 and 61.

38. **The exit text is the strictest one, everywhere** (Q1 (a)): "The same repository
    publishes a remote Go echoer and a remote Python stamper. A clean machine installs
    them into `dev` from signed artifacts with no rebuild and calls each through the toy
    caller. A plugin that offers only an unknown protocol major is refused at the
    handshake, with the reason logged on both sides. No policy service and no tenant
    data are involved." `00:573`, `01:290`, `08:623` and `08:697` gain what they lacked,
    and community:60 and plugin record §16 hold the same text.

39. **`sidecars` and `sidecars-static` are built at S2b** (Q2 (a)). The definition
    `sidecars`, in its process form, with its fake, its suite and its `definition.yaml`,
    and the provider `sidecars-static`, which reads the endpoint table that install
    writes and injects nothing (`03:113`), move to S2b, where the proxy is the first
    consumer. The provider's row ships in the bundle `aiops-selfhost`, its home in
    `00:292`, which `dev` lists after `aiops-core` (decision 22; `08:264`). The bound
    view, the leader and `sidecars-kubernetes` stay where they are. The lines that
    placed the two at S4a are edited: `00:573`, `02:215`, `08:646`, `08:699`,
    community:45 and the S4a row of plugin record §16, which the answer amends (decision
    4).

40. **The proxy injects no `tenantMint` at S2b** (Q3 (a)). It gains that inject at S4a
    with the handle, as the two-tenant tests wait for S4a (ADR-060 decision 43). `03:72`
    says "from S4a" for that inject.

41. **The row of an installed remote artifact names the artifact's own id** (Q4 (a)) and
    is resolved through the lock. `aiops/remote` is the first-party plugin of the module
    table whose code is the proxy: one plugin value per locked remote id is built with
    `FromManifest` and that code, and for the binding lists each such value counts as
    `aiops/remote`, which the lists name once. The factory and every value it builds
    stay with the loader alone (ADR-059 decision 41; decision 12), and a value takes no
    key from its manifest that those bindings would satisfy (decision 10). The bundle
    `aiops-core` holds no row of the proxy and counts ten rows: `00:276`, `08:253` and
    `08:273` change the count and take `remote` out of the rows that arrive on the way
    to the release. The lines that said "an `aiops/remote` row" say a proxy row of the
    artifact's own plugin: `00:138`, `02:191`, `03:72`, `03:490`, `03:571`, `07:1140`,
    `07:1149`, `08:437` and plugin record §11, which the answer amends (decision 4). If
    a value built after boot cannot take the bindings of an entry without a kernel
    change, that change is an ADR-034 addition under decision 3.

42. **gRPC and Protocol Buffers are a stated exception** (Q5 (a)). `00:268` gains one
    stated exception: gRPC and Protocol Buffers, the wire of the remote protocol, among
    the protocols with several implementations. The register gains entries for grpc-go
    and protobuf-go whose importers are the remote modules of decision 43, the proxy
    plugin, the adapter generator and the check runner of `aiopsctl`; each entry is
    written in the commit that first imports the module (decision 35). The bundle
    `aiops-core` holds no row of the proxy (decision 41), so ADR-054 decision 23 stands
    as written, and no definition package imports either module (decision 44).

43. **Two nested modules hold the remote packages** (Q6 (a)), rooted at `sdk/remote` and
    at `sdk/remotehost`, each with a closed require list, and the kernel side of the
    protocol is a package of the first (decision 9). Every written path stays (`07:32`;
    `08:1218`). The SDK is five Go modules, and `00:263`, `07:23`, `07:59` (R1),
    `08:1205`, `08:1207`, line 16 of the README of `docs/architecture` and row 5 of
    plugin record §2 say so; `07:51` names the two for the author of a remote Go plugin;
    `07:42` and plugin record §14 say "the kernel side of the protocol"; and the builder
    serves six modules. Dated notes say so in ADR-044, at its decisions 20 and 23, and
    in ADR-060, at its decisions 21 and 54. ADR-056 and ADR-057 decision 1 record the
    three-module boundary as it stood when each was accepted and are not edited. The
    answer amends plugin record §2 and §14 (decision 4).

44. **The generated Go of a `contrib` definition lives in the remote module** (Q7 (a)).
    The proto, which imports nothing, stays beside the definition (`08:1255`). The
    generated Go, wire types and adapter, lives under `sdk/remote/def/<name>/v<N>`. Both
    are written in the change that writes the definition, which is what `00:44`
    requires; the Go types of a definition stay plain Go, and the adapter converts.
    `00:44`, `07:28`, `07:578` and `08:1215` say where the generated Go lives.

45. **The proto is written by hand, and the proto compiler's own release generates both
    languages** (Q8 (d)), on a condition. The proto is written with the definition. One
    compiler, an unmodified binary of the proto compiler's own release that is fetched
    by pinned version and checksum, writes the Python message code itself and the Go
    wire code through `protoc-gen-go`. The in-repository generator writes everything
    else, the Python service glue on the public API of `grpcio` included, so neither
    `grpcio-tools` nor `setuptools` enters the repository. The owner's condition: the
    compiler release is read at its pin before its first use (decision 35). If that
    reading finds a text that the tool rule does not admit, the owner is asked again
    with it, and Q8 (a) is the other way, on the same condition for its wheel.

46. **`serves` gains the protocol majors and the `remote` mark, and check 3 has a clause
    that needs no connection** (Q9 (a)). `serves` gains `remoteProtocol`, the majors the
    binary speaks, and per served definition its `remote` mark with its three values,
    `contrib`, `callback` and `no`: check 9 reads `contrib` for a registration and
    `callback` for a `uses` entry (ADR-048:433). Check, pack, the offline gate, install
    and boot deny a manifest whose `requires.remoteProtocol` shares no major with it,
    and the handshake evaluates every clause (decision 16). The exit holds both denials,
    D1 and D2. The reader and the builder change in one commit, and `distVersion` stays
    1.

47. **The profile clause of check 9 is not evaluated before S4a** (Q10 (a)). It is
    reported "not evaluated: no profile holds tenants before S4a", and S4a builds it
    with the tenant directory. The contribution-set clause is built whole at S2b.
    `08:507` is not edited: the clause is in the gate text from S2b and has no input
    before S4a (ADR-060 decision 17).

48. **The toy registries move to `0.2` with no `compatibleFrom`** (Q11 (a)). Every
    manifest of the repository is regenerated at `0.2` in the same pull request, and the
    unserved minor of the S2a exit moves to `0.3`.

49. **A volatile config change reaches a remote instance as a protocol message** (Q12
    (a)). The proxy forwards the update to the instance. In a Go plugin `sdk/remotehost`
    applies it through the hand-off that the plugin's own `Define` already has, and
    `aiops-sdk` hands the new config to the contribution. The kernel does not change. A
    dated note records the answer at point 10 of G13 in ADR-034.

50. **S2b builds the compose fragment and the systemd unit, and runs both** (Q13 (a)).
    The compose fragment is a file of its own under the home that `plugin start` runs,
    and the exit uses it. The systemd unit runs the same image by digest through the
    container runtime of the host, and a CI job of its own starts it on a runner whose
    init is systemd and asserts what steps C2 and C3 assert of the fragment: the running
    digest, the user, the mounts and the network. The Helm entry waits for
    `deploy/helm`. Compose is read and pinned first (decision 35). The owner chose (a)
    with no condition. A systemd unit that is rendered and only compared as text does
    not meet this decision; if no job can run a systemd unit, the work stops on that
    point and the owner is asked (decision 33).

51. **The callback path and the remote listener path are built at S2b on a test
    definition** (Q14 (a)). S2b proves them on a test definition marked `callback` and a
    test hook marked remote, as ADR-060 decision 15 proved the signed class on a test
    definition. The one test definition stands in for all nine callbacks; nothing of it
    ships in a release table, and each real callback arrives with its definition
    (decision 26).

52. **The dependency set of `aiops-sdk` is held to L7** (Q15 (a)), on a condition.
    PSF-2.0 is recorded by name for `typing-extensions`, and the MPL-2.0 file is under
    the condition of L8. Each package has a register entry and a licence-record row, and
    a check fails on a package of the set that has none. The owner's condition: the
    licence texts of the libraries bundled inside the two native extensions are read
    before the first Python dependency merges (decision 35). If a text that L7 denies
    until reviewed is found, the dependency does not merge and the owner is asked with
    that text, with Q15 (c) as the other way.

53. **A generated file counts against no line cap** (Q16 (a)). A file that the adapter
    parity check regenerates is listed and summed apart, as `dist/modules.go` counts
    against none today. The generator has a cap of its own.

54. **The six readings of Q17 are accepted** (Q17 (a), all six), each landing with the
    code that builds it. (1) The remote toy plugins and the templates of S2b declare
    `scope: [process]`, the shared instance of `07:1194`. (2) On the Unix socket of
    `plugin check` the handshake and check 3 run as on a server, the identity is the
    process that the runner started, and only the check runner opens such a socket
    (decision 23). (3) The wordings of `07:1212` and `08:1218` both stand, and this ADR
    says "the kernel side of the protocol" and "the plugin-side host"; the tenant
    binding is the connection's, and plugin record §16 follows `07:1128`. (4) At `plugin
    enable`, `aiopsctl` refuses a missing endpoint before it submits, and the refusal
    for an instance that does not answer is the server's failed verdict, which
    `aiopsctl` prints with the exact command; `aiopsctl` never dials an instance
    (decision 17). (5) For the runtime `remote`, step 4 of `plugin check` starts the
    entrypoint and drives it over the socket in every language, and a Go plugin's own
    tests run under `-race` as well (decision 17). (6) On a Python plugin step 3
    validates the config examples against `configSchema`, and `aiopsctl` parses no
    Python (decision 17).

55. **Pinned key pairs and no issuer identify the kernel and an instance on a
    single-tenant self-host** (Q18 (a)). `plugin install` makes one key pair and a
    self-signed certificate for the instance, which names the plugin id, and on first
    need one for the kernel. Each side trusts exactly the other's certificate: the
    kernel holds the pin of each installed instance in the home, and the unit gives the
    instance its own key and the kernel's certificate. `plugin disable` and `remove`
    revoke by deleting the pin, which the proxy reads at every dial, and `remove`
    deletes the key. No key of the site can mint an identity. The issued form arrives
    where there are several tenants and a leader to issue it, at S4b. ADR-041 writes the
    answer out, inside the points of decision 23. `00` §5.4 gains the two credential
    classes, and the open point closes in `02:213`, `02:336`, `02:483`, `03:162`,
    `03:490` and `07:1159`.

56. **A remote manifest that lists a `network` entry is refused until the list can be
    enforced** (Q19 (a)). `plugin install` refuses a remote artifact whose manifest
    lists a `network` entry, names the entry and writes nothing, and `plugin check`
    warns of it. The allow-list side is built with its first consumer, S5 at the latest
    (decision 26), by the ADR of that step. No instance ever has a wider network than
    its manifest lists. This defers half of a written line: `07:135`, `07:1109`,
    `08:388` and plugin record §11, which the answer amends (decision 4), gain the step.

57. **The proxy reaches the handshake entry of the gate through a new consumer-bound key
    of the launch definition** (Q20 (a)), which carries that entry and nothing else.
    `boot` provides it, and the release manifest lists `aiops/remote` as its one
    consumer. The count of consumer-bound keys rises by one, to 14, wherever it is
    stated: `00:246`, `07:564`, row 45 of the dsh checklist at `08:87` and row 45 of
    plugin record §15, which the answer amends (decision 4). `dist/dist.yaml` gains the
    line with the key. The check runner keeps the toolbox that the operations row
    already resolves.

58. **The first message carries the digest of `plugin.yaml`** (Q21 (a)): the SHA-256 of
    the `plugin.yaml` that the plugin was built with. The kernel compares it with the
    `plugin.yaml` that install kept, whose descriptor chains to the locked digest
    through `artifact.json`, the chain that check 5 verifies at every composition
    (ADR-047 decision 18). `07:1128`, `03:489` and `08:501` say that it must equal the
    digest of the manifest that `plugins.lock` pins. The digest is a drift check and
    authenticates nothing: the authentication is the credential of decision 23 and the
    image digest that the unit runs (decision 25).

59. **The two connection clauses of check 3 are not evaluated on a disabled remote row
    or in `profile render`** (Q22 (a)). There the digest and registration clauses are
    reported "not evaluated: no connection", and they are evaluated when the row is
    enabled, inside that generation. `08:495` gains the two exceptions beside the module
    tie.

60. **A remote Go plugin is made with a flag** (Q23 (a)): `plugin new <registry> <id>
    --lang go --runtime remote`, with `inproc` the default, so the S2a form is
    unchanged. The remote form writes the same plugin package, a `main` that serves it
    through `sdk/remotehost`, a manifest with `runtimes: [remote]` and the file from
    which its image is built (decision 19). Its `requires` holds `remoteProtocol` and
    `definitions` and no `pluginApi` (ADR-048 decision 12). `07:1556` and the table of
    ADR-061 decision 30 gain the flag.

61. **`plugin pack` builds the plugin image** (Q24 (a)). It runs the build file of the
    plugin directory on the container runtime at hand, writes the image beside the
    artifact as an OCI layout and records the digest of that image; `plugin push` pushes
    the image and then the artifact. The command that signs for the digest is the one
    that computed it.

62. **The additive rule of `aiops.remote.v0` binds from the first community release that
    ships major 0** (Q25 (b)). Before it a change is made by an ADR-041 addition, with
    the generated code and every plugin of the repository in the same pull request.
    `07:1126` and community:159 gain "from its first release", the words of `07:1127`.
    Both sides ignore unknown fields, calls and feature names from S2b (decision 5).

63. **The activation id takes no kernel accessor** (Q26 (b)). The replica id is a random
    value that the process draws at boot, and the epoch is the proxy's own count of its
    activations in that process. `07:1161` and plugin record §11 gain the clause, and
    the record is amended (decision 4).

64. **The two stubs of the matrix are processes** (Q27 (b)): programs that the harness
    builds and starts as its own processes, each with the identity of decision 23.
    `08:463` says "stub remote plugins". The image path is proven by the exit.

65. **The disposer cell runs on a process-scope remote row at S2b** (Q28 (a)), a row
    whose instance ignores `Deactivate`: the proxy's disposer returns at its bound
    (`07:1196`), the outcome is "Reported" and the process is not replaced. S4b runs the
    cell again in a tenant tree. No line is edited.

66. **A denial at the handshake fails the proxy, and the row follows the matrix** (Q29
    (a)). A denial at admission detaches the row, as `08:510` says. A denial at the
    handshake fails the proxy with check 3 named, and the row follows the matrix row
    "Remote unreachable or handshake mismatch". `08:510` gains that clause.

67. **At S2b the bundle of a remote artifact holds exactly one row** (Q30 (b)). `plugin
    check` refuses another, and so does the gate at every entry point, under check 8,
    whose text gains the clause. `08:393`, `07:1850`, `08:506` and plugin record §11,
    which the answer amends (decision 4), say so for remote code. Several activations of
    one instance are the work of S4b, where the question can be reopened with the
    replicas.

68. **A shared unit stops when no enabled row of the site names its plugin** (Q31 (a)).
    `disable` revokes the credential and stops the unit when no enabled row of any
    profile of the site names the plugin; while another profile's enabled row names it,
    `disable` unloads the proxy of its own profile and leaves the instance. `remove`
    deletes the files that install rendered, the credential among them, with the other
    kept files of ADR-047 decision 10, when no profile names the id. Removing the unit
    from an orchestrator stays the operator's. `07:1195` and `03:495` gain the clause
    for a plugin that another profile still runs.

69. **The handover of new remote code is built with `plugin update` at S6b** (Q32 (b)).
    `00:490`, `03:494` and `08:194` gain "from S6b, with `plugin update`". At S2b remote
    code is live in the first half of the sentence: it is installed and enabled with no
    restart (decision 12). A new version arrives by `remove` and `install`, with its
    contributions absent in between.

70. **S2b code starts beside the call-path gate** (owner, 2026-11-27; no question). S2a
    is merged into `main`, and the merge commit is `d124a40`. S2b code starts on the
    branch `s2b/integration` in parallel with the call-path gate that runs on that merge
    commit. A kernel fix that the gate forces takes priority over S2b code. Decision 1
    had the gate run before the first S2b code; this decision replaces that one
    condition, and every other condition of decision 1 stands.

**Edits made with this record.** `00` is edited in the commit that records the answers
(`08:999`), and that commit makes the edits that the chosen options name; each decision
above lists its lines. The ADR table of `08` §3 lists this ADR as accepted, and the next
free number at `08:999` is ADR-064.

Lines that restate edited text are edited with them: the S2b builds of `00:573` and
`08:697`, community:43 and plugin record §16, which name the proxy plugin and `sidecars`
(decisions 39 and 41); the rows that `self-host-full` composes, 100 and 105 with the
Slack bundle, in `00:272`, `08:249` and plugin record §6, with the row `remote` of that
section, and `05:255` (decision 41); the stated exception in `07:50`, `07:527` and
harness:217, with a dated note at ADR-054 decision 21 (decision 42); R2 at `07:60` and
the module count in the builder paragraph of plugin record §11 (decision 43); the digest
rule in `07:1160` and community:161 (decision 58); "stub remote plugins" in `07:1521`,
`08:1244` and plugin record §13 (decision 64); the update rows of `07:1390` and plugin
record §9 (decision 69); and the next free number in section 11 of
`docs/design/delivery-bundle-2026-11.md`.

Left to later commits, each named:

- the six readings of decision 54, each with the code that builds it; among them the
  words "already carries the tenant binding" of plugin record §16, with the first
  message of the protocol (group 1 of decision 30);
- the count of the launch keys, six, and of the provider-bound keys, in `00:212`,
  `03:63`, `07:563`, `07:1367`, `08:86`, plugin record §5, row 44 of plugin record §15
  and ADR-059 decision 35, with the commit that adds the key of decision 57 to the
  launch definition, where the key gets its name; `00:246` and `07:564` take the name
  there, and `dist/dist.yaml` its line;
- the item of gRPC and Protocol Buffers in the table of ADR-054 decision 9 and the
  register entries of grpc-go and protobuf-go, with the S2b part of ADR-054 and the
  first import of either module (decisions 34 and 35);
- the licence-record rows and the register entries of decision 35, each in the commit
  that first uses its component, after the readings that decisions 45 and 52 require;
- the directory `python/` in `08:1207` and in the tree of `08` §4, with the commit that
  creates it (decision 18);
- the words "the leader can revoke" of `02:215`, `02:336`, `07:1195` and `08:1292`, and
  the rows of ADR-041 at `00:777` and `08:1175`, with ADR-041, which writes how disable
  and remove revoke where no leader exists (decision 34);
- the counts of "three SDK modules" in the texts of ADR-020, ADR-044 and ADR-060 outside
  the decisions that carry a dated note, which stand as text of their date.

`08:507`, `08:1292` and `02:313` are not edited: decisions 47 and 50 change no line of
them. ADR-047, ADR-059 and ADR-062 are not edited: no chosen option names a line of
them.
