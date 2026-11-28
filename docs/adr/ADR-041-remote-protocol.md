# ADR-041 (provisional): The remote protocol at major 0 and the identity of a remote instance on a single-tenant self-host

- Status: **answers recorded 2026-11-27 and 2026-11-28; the three runs of decision 38
  recorded 2026-11-27, and the two rows of the third run that were left, with the run
  of the count of decision 56 under load, recorded 2026-11-28; not accepted. Acceptance
  waits for the owner on questions Q22 to Q24 of the section "Runs after the answers of
  2026-11-28", and on question Q24 of the S2b part of ADR-061, which the row of `plugin
  verify` in decision 27 follows.** The project
  owner answered all 14 questions on 2026-11-27, each with its recommended option, Q9
  while nothing is published; the section "Owner answers of 2026-11-27" records them as
  decisions 40 to 53. By the answer to Q14 (decision 53) the ADR is accepted only after
  the three throwaway runs of decision 38 are recorded here, and a run result that
  changes a decision comes back to the owner. The section "Runs of decision 38, recorded
  2026-11-27" holds the runs. The certificate form of decision 20 holds as written. One
  sentence of decision 21 and two of decision 31 did not hold as worded, the modes of
  decision 24 held on rootful Docker and were not run on a rootless runtime or under
  SELinux, and the record of the S2b part of ADR-047 gave the same directory other
  modes. Those points were questions Q15 to Q19 of that section; Q20 was the payload of
  `UpdateConfig`, against ADR-034 decision 21, and Q21 the place of the Unix dial of
  decision 28, against ADR-044 decision 45. The owner answered the seven on 2026-11-28,
  each with its recommended option, and the section "Owner answers of 2026-11-28"
  records them as decisions 54 to 60, with a note of that date at each decision that an
  answer revises. That record does not accept the ADR. By decision 58 the two rows,
  rootless Podman with the user kept and the binds under SELinux labels, were run in a
  throwaway virtual machine on the same date, and the section "Runs after the answers
  of 2026-11-28" records them: both hold under the modes of decision 57. That section
  also holds the run of the count of decision 56 under load. It does not accept the
  ADR either. Two results come back to the owner (decision 53): the room that decision
  56 keeps for a reconnect does not hold while calls are being refused (Q22), and under
  rootless Podman with the user kept the instance holds the site user's group, against
  the words of decision 57 (Q23). Q24 asks the owner to confirm that the field `paths`
  of `UpdateConfig` leaves, which decision 59 words. The condition of the owner's
  answer to question Q17 of the S2b part of ADR-061 was run and is not met on the
  network of a unit, so the row of `plugin verify` in decision 27 is not revised and
  that point is back with the owner as question Q24 of that part. The commit that sets
  the ADR accepted follows the record of those four answers.
  The ADR is provisional by its row in the ADR table
  (`08:1175`): it is ratified at S5 for the policy callbacks and at S4b for one tenant
  per connection and activations across replicas. It was proposed before the first
  commit of groups 1 to 3 of ADR-063 decision 30 and of the exit harness, and it is
  accepted before that code merges into `main` (ADR-063 decision 2). It blocks the
  protocol packages `sdk/remote` and `sdk/remotehost`, the proxy plugin and its
  authenticated dial, the deployment unit, every command that reads or writes a
  credential (`plugin install`, `start`, `enable`, `disable` and `remove`), `serve()`
  and the fixture of the Python SDK, the stubs of the failure matrix and `test/remote/`.
  The proposal was revised on 2026-11-27 after its review.
- Scope: what ADR-063 decision 34 says this ADR settles. The messages of
  `aiops.remote.v0` and which side sends each field of the first message; how the plugin
  learns the chosen major; where the call that carries `Hello` is declared; one stream or
  a service per definition; the encoding of errors across the boundary; the digest of
  the first message (ADR-063 decision 58) and the activation id (decision 63); the
  callback set, with `tools`; the deadlines, the reconnect budget, what brings a proxy
  back and the bounds on what a peer may send; the pinned-key identity of ADR-063
  decision 55, inside the points of its decision 23; the dial of the proxy under the
  lists of ADR-062 decisions 13 and 14; and the scope of the Python SDK. It changes no
  decision of `00-decisions.md` by itself: each point where an answer would change a
  written line is a question below and names the lines it edits.
- Citations: `00`, `01`, `02`, `03`, `05`, `06`, `07` and `08` are the files of
  `docs/architecture/` with those numbers, and `08:1175` is line 1175 of `08` at commit
  `bbfd839`. ADR-063 cites lines at `50665fa`; the lines of `07` it cites are the same
  here, its `08:1292` is `08:1293`, its `00:410-411` is `00:410-413` with the two classes
  of its decision 55, its `00:417` is `00:419` and its `00:777` is `00:779`. "community",
  "harness" and "delivery" are `docs/design/community-first-2026-11.md`,
  `harness-and-upstreams-2026-11.md` and `delivery-bundle-2026-11.md`, cited the same
  way. The plugin record, `docs/design/plugin-system-2026-11.md`, is cited by section. A
  path with a line, as `boot/limits.go:53`, is a file of the repository at the same
  commit. Other ADRs are cited by decision. "Decision n" alone is a decision of this ADR.

## Context

The owner has decided the frame, and this ADR reopens none of it:

- The protocol major is the proto package `aiops.remote.v0`, matched exactly until
  platform 1.0. Inside a major both sides ignore unknown fields, calls and feature names
  (`07:1126`; `00:267`). The additive rule binds from the first community release that
  ships major 0; before it a change is made by an addition to this ADR, with the
  generated code and every plugin of the repository in the same pull request (ADR-063
  decision 62).
- One handshake message, `Hello`, in its own package `aiops.remote.handshake`, opens
  every connection and is frozen-additive. The kernel dials, both sides send the majors
  they speak, the kernel picks the highest common one, and with none both sides log the
  two lists and the kernel closes the stream (`07:1127`). The file
  `sdk/remote/handshake/handshake.proto` is frozen and S2b does not edit it (ADR-063
  decision 6; ADR-060 decision 42).
- The first message of the chosen major carries the plugin id, the version, the manifest
  digest, the activation id, the definitions as `name@major.minor`, feature names and one
  reserved optional field that nothing reads in the community edition. The tenant and the
  row come from the connection certificate, never from a field (`07:1128`).
- The digest is the SHA-256 of the `plugin.yaml` that the plugin was built with, and the
  kernel compares it with the `plugin.yaml` that install kept (ADR-063 decision 58).
- An activation id is (replica id, epoch): the replica id is a random value that the
  process draws at boot, and the epoch is the proxy's own count of its activations in
  that process (ADR-063 decision 63; `07:1161`).
- A volatile config change reaches a remote instance as a protocol message, and this ADR
  names the message (ADR-063 decision 49; ADR-034, G13 point 10).
- The callback path and the remote listener path are built at S2b on a test definition
  marked `callback` and a test hook marked remote; each real callback arrives with its
  definition (ADR-063 decisions 26 and 51).
- On a single-tenant self-host the kernel and each instance are identified by pinned key
  pairs with no issuer. `plugin install` makes one key pair and a self-signed certificate
  for the instance, which names the plugin id, and on first need one for the kernel.
  Each side trusts exactly the other's certificate: the kernel holds the pin of each
  installed instance in the home, and the unit gives the instance its own key and the
  kernel's certificate. `plugin disable` and `remove` revoke by deleting the pin, which
  the proxy reads at every dial, and `remove` deletes the key. No key of the site can
  mint an identity (ADR-063 decision 55). The points of ADR-063 decision 23 hold whatever
  this ADR writes.

What the documents leave to this ADR is the list of ADR-063 decision 34. Reading that
list against the code and the texts at `bbfd839` shows these facts:

- The freeze test of the handshake file accepts an added field and nothing else: an
  added message, an added option and a changed package each fail it
  (`sdk/remote/handshake/handshake_test.go:140-166`). So the file can hold no service,
  and a plugin that speaks only another major must still answer `Hello` (`07:1127`).
- The kernel dials the plugin and never the reverse (`07:1158`), so the plugin process is
  the server of every call, and a callback, which goes from the plugin to the kernel,
  can only be a message on a stream that the kernel opened (`07:1165`; `02:214`).
- `07:1151` has the instance serve "the contribution services of its definitions on one
  stream per connection". "One stream per remote row" stands in `02:213`, `02:290`,
  `02:336`, plugin record §10 and the row of ADR-036 that quotes it (ADR-036:30). A
  harness contribution is itself a server stream of steps (`07:1197`). The texts do not
  say whether "one stream" is one connection or one call.
- The handshake deadline is 2 s against a running instance, and the same sentence has a
  tree with remote rows mount within it (plugin record §10; ADR-036:30). ADR-036 defers
  the measurement to S4b (ADR-036:364). The load, drain and dispose deadlines are
  construction data for the whole kernel, and a deadline per fiber needs an ADR (ADR-034,
  owner decisions of 2026-11-19, item 4); the launcher's default for each is 10 s, and
  `limits.yaml` may change them (`boot/limits.go:53`, `:92`).
- A proxy disposer never waits on the remote beyond a bound shorter than the dispose
  deadline, and an exhausted reconnect budget unloads the proxy (`07:1196`; `03:493`).
  No line gives either value, and no line says what brings the proxy back.
- `Endpoint(name)` of `sidecars` is available to a caller only while the instance is
  ready, "so its consumer is PENDING and never FAILED" (`03:113`; `07:356`, `:1150`;
  plugin record §10). The matrix states PENDING for the boot cells of "Remote
  unreachable or handshake mismatch" and UNLOADING for its cell "while running"
  (`test/failure-matrix/matrix.yaml:360-385`; `08:477`), and a denial at the handshake
  fails the proxy (`08:510`; ADR-063 decision 66). `sidecars-static` reads the endpoint
  table that install writes and injects nothing (ADR-063 decision 39; `03:113`). No line
  says how it learns that an instance stopped answering, and ADR-063 decision 34 names
  no ADR or addition that fixes the definition `sidecars`.
- A provider limits who may use its provision with an availability check, which the
  kernel runs for each consumer (`sdk/kernel/key.go:232-238`). The kernel keeps the
  answer of a check and runs it again only for a new consumer, for a changed provider or
  after a notification for the key (`sdk/kernel/owner.go:744-750`, `:1153-1162`). The
  one entry of that notification is a method of the root, which the launcher holds
  (`sdk/kernel/rootview.go:247-261`). The context that a plugin gets has no such method,
  and nothing under `sdk/def/launch`, `boot/`, `loader/` or `plugins/` calls or exposes
  it. No code outside a test uses an availability check at `bbfd839`. So a provider
  plugin can state a check and cannot make the kernel run it again.
- A fiber that asks for its own disposal is marked for removal and does not become
  PENDING (`sdk/kernel/owner.go:955-966`).
- A failed fiber is sticky in the kernel, and `restart-policy`, a row of `aiops-core`, is
  the only retry: it calls `Update` on a FAILED fiber with its own backoff and give-up
  rule (`03:66`, `:88`; `07:352`, `:367`).
- The loader commits a change in place only when every changed leaf is under a path that
  the `configSchema` marks volatile and the fiber is ACTIVE; any other change restarts
  the row (`loader/apply.go:435-445`; ADR-048 decision 9). It then emits
  `loader/volatile-update`, an event whose payload is the new config with every
  reference left out and the changed leaves, none of which holds a reference
  (`sdk/def/loader/v0/loader.go:223-236`). The commit is done before the event, and the
  loader drops the error of a listener (`loader/apply.go:105`).
- The matrix harness gives each cell 30 s (`test/failure-matrix/harness_test.go:104`),
  runs the binary of the test distribution on kernel deadlines of 1 s
  (`test/dist/limits.yaml`), and passes a cell at the first status snapshot that holds
  the stated state, read every 10 ms (`test/failure-matrix/harness_test.go:346-369`;
  `test/dist/harness/harness.go:38`).
- Remote listeners run "under a deadline" on the hooks marked remote (`07:450`), and the
  owner of a gate denies when an expected listener is unreachable (`07:448`). No line
  gives the deadline.
- The delivery record names this ADR in four rows and decides none: the callback set
  grows additively inside a major; a call deadline is a value of the definition, not a
  constant of the protocol; "a remote harness declares no network capability" stays a
  rule of the `harnesses` definition; and the tenant comes from the connection, never
  from a payload field (delivery:366, :367, :369, :370).
- `aiopsctl` writes every file of the home with mode 0600 under a home of mode 0700 that
  the site user owns, and the kernel runs as that user (ADR-047 decision 14). A kernel
  container gets the home bound read-only (ADR-047 decision 15), and `snapshot/` holds a
  copy of every file that a write replaces or deletes while the command runs (ADR-047
  decision 13). The unit of an instance names a user that is neither the site user nor
  root and binds one directory of the home, the one that holds that instance's
  credential, read-only (ADR-063 decision 25). A file of mode 0600 that the site user
  owns cannot be read by that other user.
- `plugin disable` has more outcomes than a pass and a failure: a pass that does not show
  the change because a later layer keeps the row enabled, no final result, and a process
  that does not answer each keep the files (ADR-062 decision 11).
- The unit is started with `aiopsctl plugin start <id>` on Compose and with `systemctl
  enable --now` on systemd (`08:1293`), and the pair of an instance is "made by
  `aiopsctl plugin install`" (`00:412`; `02:483`).
- Plugin vet refuses a listener outside one list of plugin ids, and the same list holds
  the one exception of the dial rule, `net.DialUnix` of the operations plugin (ADR-062
  decisions 13 and 14; `internal/vet/vet.go:125-131`). The dial rule binds from the step
  that builds `dialer` (ADR-061 decision 38; `07:1405`). The proxy injects `sidecars`
  and no `dialer` (`03:72`). `dialer-guarded` refuses the cell ranges in hosted, and
  link-local only by default in the self-host profiles, "where the operator's own stores
  sit on private addresses" (`00:217`, `:450`).
- ADR-063 decision 35 read four Python files: one wheel of `grpcio` for CPython 3.12 on
  `manylinux2014_x86_64`, one wheel of `protobuf`, one of `typing-extensions` and one of
  `pytest`. It lists as not read, each read before its first use: the licence texts of
  the libraries inside the native extensions of `grpcio` and `protobuf`, which the owner
  made a condition before the first Python dependency merges (ADR-063 decision 52); the
  packages that `pytest` requires; the interpreter and how it is fetched; the base image
  of the Python plugin image; and the build backend of `aiops-sdk`. The licence record
  still has the gRPC runtime of the Python SDK as "Not read" (licence:68). Another
  interpreter or platform enters the exit only after its files are read (ADR-063
  decision 27).

## Decision

### The handshake call and the chosen major

1. **The call that carries `Hello` is declared in a second file of the package
   `aiops.remote.handshake`.** The file `sdk/remote/handshake/call.proto` declares one
   service, `Handshake`, with one unary call, `Hello`, whose request and whose answer
   are the frozen message `Hello`. The frozen file is not edited (ADR-063 decision 6).
   The call belongs to no major, so every SDK of every major serves it, and a plugin
   that speaks only another major still answers it, which `07:1127` requires. The file
   imports the frozen file and nothing else, and it carries no `go_package` option: the
   Go import path is given to the generator, as for the frozen file (ADR-063 decision
   6). Whether the call is frozen with the message is question Q2.
2. **One connection carries one `Hello` exchange.** After the dial of decision 28 the
   kernel sends `Hello` with its majors, the kind `KERNEL` and its diagnostics, and the
   plugin answers with its majors, the kind `PLUGIN` and its diagnostics. It is the
   first call on the connection, and the kernel makes no call of a major before the
   answer has returned (`07:1127`; ADR-063 decision 23). The diagnostics carry the keys
   `sdk`, `sdkVersion` and `release`; they are logged inside the bound of decision 19
   and never read for a decision (`07:1136`; ADR-063 decision 6). A `Hello` whose kind
   is not the other side's is refused: the kernel closes the connection and the plugin
   answers the call with the status `protocol` of decision 10.
3. **The kernel picks the major, and the plugin learns it from the call that follows.**
   The kernel picks the highest major that both lists hold (`07:1127`). The major is the
   proto package (`07:1126`), so the full name of the session call of decision 4 holds
   it, and the plugin learns the chosen major from the service on which the kernel opens
   the session. No message repeats it. With no common major the plugin has both lists
   when it answers `Hello` and logs them, the kernel logs both when the answer returns
   and closes the connection, and the denial names check 3 (`07:1127`; `08:501`;
   ADR-063 decision 27, steps D1 and D2).

### The session and its messages

4. **One connection, one session stream, and a service per definition.** The package
   `aiops.remote.v0` declares one service, `Remote`, with one bidirectional stream,
   `Session`, which the kernel opens after the `Hello` exchange. The session carries the
   first messages, the activation, the registrations, the callbacks and the liveness
   check. A contribution call and a listener call are not session messages: each is a
   call of the service that the proto of its definition declares, generated by the
   adapter generator (ADR-063 decisions 13 and 44), made by the kernel on the same
   connection, unary or a server stream as the definition has it (`07:1197`). A callback
   is a session message, because the plugin can reach the kernel only on a stream that
   the kernel opened (`07:1158`, `:1165`; `02:214`). The "one stream" of `07:1151`,
   `02:213`, `02:290`, `02:336`, plugin record §10 and ADR-036:30 is then one connection
   that holds one session: an idle row holds one connection with one open stream, and
   each call in flight is one more stream of the transport on that connection. That
   reading is question Q1, and it amends plugin record §10.
5. **A connection ends with its session.** One dial carries one `Hello` exchange, one
   session and at most one activation. When the session ends for any reason the kernel
   closes the connection, and a reconnect is a new dial, a new `Hello`, new first
   messages and a new activation (`03:491`: activation is per connection). So gate
   check 3 is evaluated at every connection, as `08:495` has it for every remote
   handshake.
6. **The first message of the chosen major is one exchange, and each field has one
   sender** (`07:1128`). The kernel's first session message is `Open`, and the plugin's
   is `Identify`.

   | Field of `07:1128` | Sender | Message and form |
   |---|---|---|
   | Activation id | Kernel | `Open.activation`: the two parts of decision 12 |
   | Feature names | Each side, its own | `Open.features` and `Identify.features`: lists of names. No name is defined at S2b, and an unknown name is ignored (`07:1126`) |
   | The reserved optional field | Kernel | `Open.record`: optional bytes for a record the kernel passes to the plugin. The community kernel never sets it and no SDK reads it (`07:1128`; `00:267`) |
   | Plugin id | Plugin | `Identify.plugin`: the `id` of its `plugin.yaml` |
   | Version | Plugin | `Identify.version`: the `version` of its `plugin.yaml` |
   | Manifest digest | Plugin | `Identify.manifest_digest`: decision 11 |
   | Definitions | Plugin | `Identify.definitions`: each `name@major.minor`, one per definition it registers with or listens on |

   - No message of `aiops.remote.v0` has a field that names a row or a tenant. Both come
     from the connection (decision 22), and an unknown field that names either is
     ignored like any unknown field (`07:1128`; ADR-063 decision 23; delivery:370).
   - The row, the pin and the kept manifest are chosen before the dial, from the row
     that the generation mounts. No field of `Identify` selects any of them.
   - The kernel compares `Identify.plugin` with the `id`, and `Identify.version` with
     the `version`, of the kept manifest. A difference is a broken rule of the protocol:
     the kernel sends `Bye` with `protocol`, logs both values on its side, and the proxy
     fails (decision 18). A plugin built from another `plugin.yaml` is already denied by
     its digest (decision 11); this comparison refuses a peer that contradicts its own
     digest, and it adds no clause to check 3.
7. **The kernel evaluates the gate on `Identify` before it sends any config.** The proxy
   hands both `Hello` lists, the digest and each definition version to the handshake
   entry of the gate (ADR-063 decisions 16 and 57). On a denial the kernel sends `Bye`
   with the check and the reason and closes the connection, so the reason is logged on
   both sides, and the proxy fails with check 3 named (`08:510`; ADR-063 decision 66).
   `Activate` is sent only after an admitting verdict.
8. **The session messages.** `Session` takes a stream of `KernelFrame` from the kernel
   and returns a stream of `PluginFrame` from the plugin. Every frame carries `id`, a
   number from 1 that its sender chooses and never repeats on that stream, and `re`, the
   `id` of the frame it answers, or 0.

   | Message | Sender | Fields | Answered by |
   |---|---|---|---|
   | `Open` | Kernel | Decision 6 | `Identify` |
   | `Identify` | Plugin | Decision 6 | `Activate`, or `Bye` |
   | `Activate` | Kernel | `activation`, `generation`, `config` (`07:1161`). The generation is a label for logs. The config is the row's config as JSON, which the kernel has validated against the `configSchema` of the kept manifest (ADR-063 decision 10) | `Activated` |
   | `Register` | Plugin | `activation`, `effect_id`, `definition` as `name@major.minor`, and one of two kinds: a contribution, with its name and its spec as the definition's proto has it, or a listener, with its hook and stage (`07:1162`) | `Result` |
   | `Unregister` | Plugin | `activation`, `effect_id`: the plugin disposed that effect | `Result` |
   | `Activated` | Plugin | `activation`, a status: the plugin's `Apply` returned | None |
   | `UpdateConfig` | Kernel | `activation`, `config`, `paths`: the volatile change of ADR-063 decision 49, as the loader's event carries it (decision 16) | `ConfigUpdated` |
   | `ConfigUpdated` | Plugin | `activation`, a status | None |
   | `Callback` | Plugin | `activation`, `definition`, `method`, `payload`, `timeout_ms`, and the optional `handle` of decision 13 | `Result`, with the answer's payload |
   | `Deactivate` | Kernel | `activation` (`07:1166`) | `Deactivated` |
   | `Deactivated` | Plugin | `activation` | None |
   | `Ping`, `Pong` | Kernel, plugin | None: the liveness check of decision 16 | `Pong` |
   | `Result` | Either | A status, an optional payload | None |
   | `Bye` | Kernel | A status: why the kernel ends the session. It is the last message | None |

   The wire form. A field name is its name in the proto; `07:1162` and the plugin record
   write `effect_id` the same way.

   | Message | Field, number and type |
   |---|---|
   | `Status` | `code` 1 `uint32`, the number of the transport's status code; `reason` 2 `string`; `message` 3 `string` |
   | `ActivationId` | `replica` 1 `bytes`, 16 bytes; `epoch` 2 `uint64` |
   | `KernelFrame` | `id` 1 `uint64`; `re` 2 `uint64`; one of `open` 10, `activate` 11, `update_config` 12, `deactivate` 13, `ping` 14, `result` 15, `bye` 16, each the message of that name |
   | `PluginFrame` | `id` 1 `uint64`; `re` 2 `uint64`; one of `identify` 10, `register` 11, `unregister` 12, `activated` 13, `config_updated` 14, `callback` 15, `deactivated` 16, `pong` 17, `result` 18 |
   | `Open` | `activation` 1 `ActivationId`; `features` 2 `repeated string`; `record` 3 `optional bytes` |
   | `Identify` | `plugin` 1 `string`; `version` 2 `string`; `manifest_digest` 3 `string`; `definitions` 4 `repeated string`; `features` 5 `repeated string` |
   | `Activate` | `activation` 1 `ActivationId`; `generation` 2 `string`; `config` 3 `bytes`, one JSON value in UTF-8 |
   | `Register` | `activation` 1 `ActivationId`; `effect_id` 2 `uint64`, chosen by the plugin and never repeated in that activation; `definition` 3 `string`; one of `contribution` 4 (`name` 1 `string`, `spec` 2 `bytes`) and `listener` 5 (`hook` 1 `string`, `stage` 2 `string`) |
   | `Unregister` | `activation` 1 `ActivationId`; `effect_id` 2 `uint64` |
   | `Activated`, `ConfigUpdated` | `activation` 1 `ActivationId`; `status` 2 `Status` |
   | `UpdateConfig` | `activation` 1 `ActivationId`; `config` 2 `bytes`, one JSON value in UTF-8; `paths` 3 `repeated string`, the JSON Pointer of each changed leaf |
   | `Callback` | `activation` 1 `ActivationId`; `definition` 2 `string`, as `name@major.minor`; `method` 3 `string`; `payload` 4 `bytes`; `timeout_ms` 5 `uint64`, in milliseconds; `handle` 6 `optional bytes` |
   | `Deactivate`, `Deactivated` | `activation` 1 `ActivationId` |
   | `Ping`, `Pong` | No field |
   | `Result` | `status` 1 `Status`; `payload` 2 `bytes` |
   | `Bye` | `status` 1 `Status` |

   A frame whose kind the receiver does not know is answered with a `Result` of status
   `unknown` and changes nothing: the stream and the activation stay (`07:1126`; ADR-063
   decision 5). An unknown call of a definition's service is answered with the same
   status by the transport. The names above are the names of major 0 and replace the
   descriptive ones of `07` §5 and of plugin record §11 (`07:67`, R9); before the first
   community release they change only by an addition to this ADR (ADR-063 decision 62).

   Note of 2026-11-28, with the owner's answer to question Q20 (decision 59):
   `UpdateConfig` has the fields `activation` 1 and `config` 2, the committed config
   whole, in the form in which `Activate` carries the config. The field `paths` leaves
   both tables, and the words "as the loader's event carries it" of the first no longer
   hold.
9. **Activation on the wire.** An activation is ACTIVE on the wire from the kernel's
   `Activate` to its `Deactivate` or to the end of the session, whichever is first.
   - The plugin-side host runs one fiber per activation with that activation's config
     (`07:1152`). It ends that fiber when the session ends, with or without a
     `Deactivate`.
   - The kernel accepts a `Register`, an `Unregister` and a `Callback` only when the
     message's activation id equals the id that the kernel sent in `Open` on that
     stream and that activation is ACTIVE on the wire. A `Register` with any other id, of
     a superseded activation or of an earlier boot of the kernel, is refused with
     `inactive-effect`, and any other message with `inactive` (`07:1162`, `:1165`;
     ADR-063 decision 28).
   - The proxy registers each accepted contribution with the dispatcher that serves its
     definition version, as an effect of the proxy fiber (`07:1149`, `:1129`). It
     refuses a `Register` that the manifest's `registers` or `hooks` does not list, one
     for a registry not marked `contrib`, one for a hook not marked remote and one for a
     definition version that is not served (ADR-048 decision 21; ADR-063 decisions 11
     and 16).
   - The proxy's `Apply` returns when `Activated` arrives with no error. An `Activated`
     with an error, or none inside the bound of decision 16, fails the proxy.
   - Every call that the kernel makes on a definition's service carries two entries of
     call metadata: `aiops-activation`, the activation id, and `aiops-effect`, the
     `effect_id` of the `Register` that the call is for, in decimal. One activation may
     hold two contributions of one definition, or two listeners on one hook at two
     stages, so the pair is what names the target. The plugin-side host routes the call
     to that registration of that activation's fiber and answers `inactive` when the
     activation is not ACTIVE on the wire or holds no such registration. Both values are
     the kernel's and the plugin's own and name no row and no tenant.

### Errors across the boundary

10. **An error is a status: a code and a reason from a closed list.** On a call of a
    definition's service the code is the status code of the transport and the reason is
    the trailer `aiops-reason`. In a session message both are fields of the status. No
    error detail message is used, so no further proto package is imported. A receiver
    that does not know a reason, or gets none, acts on the code alone (`07:1126`).

    | Condition | Code | Reason | In Go it is |
    |---|---|---|---|
    | A registration from an activation that is superseded or of an earlier boot | `FAILED_PRECONDITION` | `inactive-effect` | `kernel.ErrInactiveEffect` (`07:1162`) |
    | A call, a callback or another message on an activation that is not ACTIVE on the wire, or a call for a registration it does not hold | `FAILED_PRECONDITION` | `inactive` | `kernel.ErrInactive` (`07:1165`; `03:366`) |
    | The caller's context was cancelled | `CANCELLED` | none | `context.Canceled` |
    | The deadline of the call passed | `DEADLINE_EXCEEDED` | none | `context.DeadlineExceeded` |
    | A registration, a listener or a callback outside the contribution set or the manifest | `PERMISSION_DENIED` | `gate-check-9` | The refusal of the proxy, naming check 9 (`08:507`) |
    | A registration or a callback of a definition version that is not served; a denial at the handshake | `FAILED_PRECONDITION` | `gate-check-3` | The refusal of the proxy, naming check 3 (`08:501`) |
    | A bound of decision 19 is passed | `RESOURCE_EXHAUSTED` | `bound` | The refusal, naming the bound |
    | A message, a call or a callback method that the receiver does not know | `UNIMPLEMENTED` | `unknown` | Reported; nothing changes |
    | A message that breaks an order or a form of this ADR, as a `Hello` of the wrong kind or an `Identify` that differs from the kept manifest | `INVALID_ARGUMENT` | `protocol` | The refusal, naming the rule |
    | The pin is absent, or differs from the certificate that the session was opened with (decision 26) | `UNAUTHENTICATED` | `revoked` | The refusal of the proxy |
    | The instance cannot be reached, or the session is lost and not yet back | `UNAVAILABLE` | none | The error of the kernel side that a dispatcher's breaker counts (`07:1196`) |
    | The contribution's own error | `UNKNOWN` | none | A plain error with the text, or the definition's error value where the trailer below names one |
    | The contribution panicked or raised outside its contract | `INTERNAL` | none | A plain error; the stack stays in the instance's log |

    - The list grows by addition. The text of an error crosses as the status message and
      is logged as data, never as a format string.
    - A definition whose callers must tell one of its own errors from another declares
      the names of those errors in its own proto, which imports nothing (ADR-063 decision
      44). The generated adapter then writes the name into the trailer `aiops-error` on
      the plugin side and turns a name it knows back into the definition's error value
      on the kernel side; an unknown or absent name is the plain error. The two toy
      definitions declare no such error, so the generator gains this with the first
      `contrib` definition that does (`08:680`), and at S2b every error of a
      contribution crosses as `UNKNOWN` with its text.

### The digest and the activation id

11. **The manifest digest is `sha256:` and 64 lower-case hexadecimal digits over the
    bytes of `plugin.yaml`** (ADR-063 decision 58; ADR-048 decision 1). The plugin sends
    the digest of the `plugin.yaml` its program was built with: `sdk/remotehost` takes
    those bytes from the `main` that the template writes, and `serve()` reads the file
    that `aiops-sdk manifest` wrote beside the plugin's package (`07:1210`). The kernel
    compares it with the digest of the bytes of `bundles/<plugin id>/plugin.yaml`, which
    install kept byte for byte and whose descriptor chains to the locked digest through
    `artifact.json` (ADR-047 decisions 5, 8 and 18). A difference denies with check 3
    (`08:501`). The digest is a drift check and authenticates nothing (ADR-063 decision
    58): the authentication is decision 21.
12. **The activation id has two parts** (ADR-063 decision 63; `07:1161`). `replica` is
    16 bytes that the server process draws from the system's random source when it
    boots, the same for every proxy of that process. `epoch` is an unsigned 64-bit
    count that each proxy keeps of its own activations in that process, from 1. The id
    is compared only among the connections of one proxy row, so a `Register` that
    carries the id of an earlier connection or of an earlier boot never equals the id
    of the stream it arrives on (decision 9). The kernel does not change (ADR-063
    decision 3). In a log and in the metadata of decision 9 the id is written as the
    hexadecimal replica, a `/` and the decimal epoch.

### Callbacks and listeners

13. **The callback set of major 0 is the nine callbacks of `07:1171-1180`, and it grows
    only by addition.** A remote plugin has no other way into the server (`07:1169`).

    | Callback | Scope, in the words of `07:1173-1180` | Arrives |
    |---|---|---|
    | Telemetry | "Logs, metrics and traces over OTLP" (`00:213`) | S3 |
    | `secrets` | "Read-only, the instance's own (tenant, plugin, name) scope" (`00:218`) | S3 |
    | `kv`, `blob` | "The plugin's own prefix; each write is its own commit" (`00:215`) | S3 |
    | `tenants` | "The tenant record, read-only" (`00:224`) | S4a |
    | `audit` | "Append, as its own commit" (`00:226`) | S5 |
    | `credentials` | "For an executor only, answered through `remediation` with the `ChangeToken` of the call in flight" (`00:227`) | S5 |
    | `llm` | "Through `llm-gateway`, with the instance's virtual key. For a harness: by tier, each call carrying the run handle, so it is metered and capped under that run" (`00:229`) | S8a |
    | `tools` | "For a harness only: `Specs` and `Invoke` on the offered tool set of the active run, each call carrying the run handle. A call for another run, for a run that ended or for a tool outside the set is refused" (`00:233`; `08:697`) | S8a |

    The steps are those of ADR-063 decision 26. At S2b the path is built and proven on
    one test definition marked `callback`, which ships in no release table (ADR-063
    decision 51).
    - A callback is the session message `Callback`. `definition` names a definition
      marked `callback` as `name@major.minor`. `method` is the name of one call of the
      service that the definition's proto declares, `payload` is the encoded request of
      that call, and the payload of the `Result` is its encoded answer.
    - The proxy checks a callback as it checks a registration. A definition version that
      is not served is refused with `gate-check-3` (the minor rule of `07:1129`). A
      definition that is not marked `callback`, or that the manifest's `uses` does not
      list, is refused with `gate-check-9` (ADR-048 decision 16; ADR-063 decisions 10
      and 16). A method that the served definition does not have is answered `unknown`.
    - The kernel side of a callback, and the client that stands for the definition in
      `sdk/remotehost` and in `aiops-sdk`, are written from the definition's proto. At
      S2b the one definition is the test definition, so both sides of it are test code,
      beside that definition in Go and under the tests of `python/`: no release module
      and no wheel holds them (ADR-063 decision 51). The adapter generator covers the
      `contrib` form at S2b (ADR-063 decision 13) and gains the `callback` form, driven
      by the mark in `definition.yaml`, with the first callback definition that ships,
      at S3 (`08:680`; ADR-063 decision 26).
    - `handle` is optional opaque bytes that the kernel minted and gave the plugin in
      the request of a call in flight: the run handle for `llm` and `tools`, and for
      `credentials` what ties the callback to the `ChangeToken` of that call (`07:62`,
      R4). The kernel side checks it and the plugin never builds one. Its layout is
      fixed with its definition, by an addition to this ADR: the run handle at S8a with
      ADR-051 (`07:74`, R16), that of `credentials` at S5.
    - A callback is accepted only on the stream of the activation that is ACTIVE on the
      wire, so only by the replica that holds it (`07:1165`; `03:492`). It has no
      credential of its own: it rides the authenticated connection (`00:419`; ADR-063
      decision 23).
    - The set grows additively inside the major, a new callback being a new definition
      marked `callback` or a new method of one. "Read" is a property of the tool set
      that `harnesses` offers a run, not of the protocol, so a later callback that
      writes for a run needs no new major (delivery:366).
    - A remote harness declares no network capability (`07:1197`). That stays a rule of
      the `harnesses` definition and its lint, under ADR-051. It is not a rule of the
      protocol or of the gate in general (delivery:369); the network of an instance is
      ADR-063 decisions 24 and 56.
14. **A listener call is a call of the service of the definition that owns the hook.**
    The plugin registers a listener with `Register`, and the proxy puts a listener of
    its own on the chain for it, at the declared stage (`07:1209`; `07:450`). The
    payload is the scrubbed form that the definition's proto declares for the remote
    side (`07:1164`). S2b proves the path on a test hook marked remote (ADR-063
    decision 51); the scrubbed payloads of the product hooks arrive at S5 (ADR-063
    decision 26).
15. **The deadline of a remote listener is the deadline of the dispatch.** The owner of
    a hook marked remote dispatches under a context deadline, which is that
    definition's own value, and the proxy carries what is left of it as the deadline of
    the call (`07:450`; delivery:367). The protocol holds no constant for it. The proxy
    makes no listener call for a dispatch whose context has no deadline: it returns the
    error to the chain. A listener that misses the deadline, or whose session is lost,
    returns `DEADLINE_EXCEEDED` or `UNAVAILABLE` to the owner and never an allow, so on
    a gate the owner's rule for an unreachable expected listener applies and the check
    denies (`07:448`). The gates on the ack path are not marked remote (`07:456`). The
    values for `notify/pre-send`, `approval/gates` and `change/pre-dispatch` are fixed
    with those definitions at S5, where this ADR is ratified for them (`06:805`;
    `05:845`). Whether the protocol should hold a constant or a ceiling is question Q8.

### Deadlines, the reconnect budget and the bounds

16. **The deadlines.**

    | What is bounded | Value | Source |
    |---|---|---|
    | The handshake against a running instance: the dial, the TLS handshake, `Hello`, `Open`, `Identify` and the verdict of the gate | 2 s. At load it is the smaller of 2 s and four fifths of what is left of the kernel's load deadline, so that the proxy can still report before that deadline (decision 18) | Plugin record §10; ADR-036:30 |
    | The activation: from `Activate` to `Activated` | At load, what is left of the kernel's load deadline when `Activate` is sent. On a reconnect, which runs outside `Apply`, the kernel's load deadline in full, and never past the end of the reconnect budget | `07:358`; ADR-034, owner decisions of 2026-11-19, item 4 |
    | A contribution call | The deadline of the caller's context, which the dispatcher sets (`07:783`, `:1153`) | delivery:367 |
    | A listener call | Decision 15 | `07:450` |
    | A callback | `Callback.timeout_ms`, what is left of the deadline of the plugin's own call | delivery:367 |
    | `UpdateConfig` to `ConfigUpdated` | 2 s | This ADR |
    | The liveness check: a `Ping` every 10 s, and its `Pong` within 5 s | A missing `Pong` is a lost session. At each `Ping` the proxy also reads the pin again (decision 26) | This ADR |
    | The proxy disposer's wait on the remote: from `Deactivate` to `Deactivated` | The smaller of 2 s and half the kernel's dispose deadline | `07:1196`; `03:493` |

    - The 2 s is the handshake, up to the verdict of the gate. The activation that
      follows is the plugin's own `Apply`, and it has the load deadline, as an
      in-process `Apply` has, so that one `Plugin` value meets one bound in both
      runtimes (`07:1198`). A row is therefore ACTIVE within 2 s against a running
      instance only when the plugin's `Apply` returns inside what the handshake left of
      the 2 s. "A tree with remote rows mounts within the handshake deadline" of plugin
      record §10 and ADR-036:30 is then a budget that S4b measures (ADR-036:364), not a
      bound that the protocol enforces. That reading is question Q13.
    - The whole of the proxy's `Apply` stays inside the kernel's load deadline, which is
      kernel-wide. The proxy makes no call on a definition's service for a caller whose
      context has no deadline. A call deadline is never a constant of the protocol, so a
      run of tens of minutes on a remote harness is its definition's choice
      (delivery:367). The cold budget against an instance scaled to zero is a value of
      the hosted form and waits for it (ADR-036:30).
    - `UpdateConfig` carries what the loader's event carries: the committed config with
      every reference left out, and the changed leaves as `paths`, none of which holds a
      reference (`sdk/def/loader/v0/loader.go:223-236`). The instance takes the values
      at `paths` and keeps every other value of its activation's config. The loader has
      committed the change before the proxy hears of it, and drops a listener's error
      (`loader/apply.go:105`), so the proxy cannot refuse the change. A `ConfigUpdated`
      with an error, or none inside 2 s, therefore ends the session: the kernel sends
      `Bye`, and the proxy dials again as after a lost session (decision 18). The
      `Activate` of the new activation carries the committed config, so an instance
      never keeps serving a config that the kernel replaced. The row stays ACTIVE and
      the generation's verdict does not change. Note of 2026-11-28 (decision 59): the
      first two sentences of this bullet do not hold. `UpdateConfig` carries the
      committed config whole, with each reference resolved again at that commit, as the
      hand-off of ADR-034 decision 21 gives it to the proxy, which does not hear the
      loader's event. The instance replaces the config of its activation with it and
      merges nothing. From its third sentence on the bullet stands, with one reason
      changed: the proxy cannot refuse the change because the kernel process has
      retained the config before the message is sent (ADR-034, G13 points 13 and 15),
      not because the loader drops a listener's error.
    - The values of this table that this ADR gives, and those of decision 18, are
      construction data of the kernel side of the protocol: the dial function of
      decision 28 takes them in one value whose zero value is the values written here.
      The proxy plugin of a release passes the zero value. The proxy of the test
      distribution and the tests of decision 37 pass shorter ones, as the suites run on
      kernel deadlines of 1 s (`test/dist/limits.yaml`), so a matrix cell stays inside
      the 30 s that the harness gives it. No file, no row config and no key sets them at
      S2b; a site that needs another value asks for an addition to this ADR.
17. **The proxy disposer.** At unload the proxy cancels its calls in flight, sends
    `Deactivate`, waits for `Deactivated` no longer than the bound of decision 16 and
    closes the connection whether or not the answer came. Its registrations are effects
    and are unwound by the kernel in the same walk. A remote that ignores `Deactivate`
    therefore costs the bound and nothing else: the disposer returns inside the dispose
    deadline, the outcome is reported and the process is not replaced (`07:1196`;
    ADR-063 decision 65).
18. **The reconnect budget is 30 s. An instance that does not answer leaves the row
    PENDING and returns when its endpoint is offered again; a denial is a failure.**
    - *Loss.* A session is lost when the stream ends without a `Deactivate` or a `Pong`
      is missing. The proxy stays ACTIVE, marks its contributions unavailable, so each
      call fails at once with `UNAVAILABLE` behind the dispatcher's breaker, and dials
      again: at once, then after 250 ms, doubling to 5 s, each wait changed by up to a
      fifth at random (`07:1196`).
    - *Return inside the budget.* A reconnect is a new connection and a new activation
      with the next epoch (decision 5). A registration of the new activation that has
      the definition and the name, or the hook and the stage, of one that the old
      activation held takes that entry over. An entry that the new activation has not
      registered again when `Activated` arrives is unwound.
    - *A peer that accepts and drops.* A session is held once it has been ACTIVE on the
      wire for 60 s. The wait before a dial returns to zero only after a held session;
      after any other loss it goes on from the last wait. Eight sessions lost within ten
      minutes, none of them held, fail the proxy with the rule named, `flapping`: the
      row is FAILED and `restart-policy` owns the retry. So an authenticated peer that
      completes the handshake and closes costs at most eight activations in ten minutes
      before the row fails, and one log line each.
    - *The budget.* It is exhausted when 30 s have passed since the loss with no session
      ACTIVE on the wire. The instance is then "not answering", as it is when a dial at
      load is refused or gets no answer inside the handshake wait of decision 16. A
      refused dial at load is tried again inside that wait.
    - *The report.* The proxy reports an instance that is not answering to `sidecars`
      and stops dialling. At load it returns no error for it: it reports and waits for
      the kernel to end the load. The provider holds that instance as not ready and
      asks the kernel to run the availability check of `sidecars` again. The check now
      refuses the proxy, so the kernel unloads it, every registration is unwound, and
      the row is PENDING and says what it waits for: the instance and the time of the
      report (`03:113`; `07:356`, `:359-360`, `:1150`, `:1196`). That is the "unload
      after the budget" and the PENDING of the matrix row (`08:477`).
    - *The form of the report.* ADR-063 decision 34 gives the definition `sidecars` to
      no ADR, so this ADR fixes the two parts of its process form that the protocol
      needs, and the S2b part of ADR-047, which owns the endpoint table, fixes the rest
      (decision 39). `Endpoint(name)` returns the endpoint and the attempt value of that
      instance's line of the table. `Report(name, attempt)` says that the consumer got
      no answer at the endpoint it was given with that attempt value. A report carries
      nothing else, and a provider never dials or probes an instance. The attempt value
      is a random value in the line, which `plugin enable` replaces before it submits.
    - *What clears a report.* Two things, and nothing else. (1) Time: the provider
      offers the endpoint again 5 s after the report. The wait doubles, to 60 s, for
      each report that follows an offer by less than 60 s; a later report starts from
      5 s. Each offer is one request to the kernel to check again, which loads the
      proxy, and that load is one dial. (2) A new `plugin enable`: a report holds only
      against the attempt value it names, so the generation of an `enable` always
      dials. A rolled-back `enable` restores the line with the other files of the
      snapshot (ADR-062 decision 23).
    - *What this costs and gives.* An absent instance costs at most one load, one dial
      of at most the handshake wait and one unload a minute. The provider logs a report
      and a return once each, not each offer. An instance that comes back, or that the
      operator starts, is picked up within a minute with no command, which is the
      "reconnect" of the boot cell (`08:477`; `test/failure-matrix/matrix.yaml:362`).
      `plugin start` reaches no running process (ADR-062 decision 15), so it clears
      nothing by itself. A known ceiling: `plugin enable` of a row that is already
      enabled and PENDING changes no composition, so it brings the next offer no nearer.
    - *The exit path.* In step C2 of ADR-063 decision 27, `plugin enable` before
      `plugin start` dials, gets no answer and reports; the row is PENDING, the verdict
      fails and the files, the line among them, are restored. In step C3 `plugin start`
      starts the unit and `plugin enable` writes a new attempt value, so the report of
      C2 no longer holds and the load dials.
    - *The path of the request.* The kernel runs a check again only after a
      notification for the key, and no plugin can send one (Context). The provider
      therefore needs a way to ask. This ADR proposes a consumer-bound key of the
      launch definition, which boot provides and which carries one call that takes no
      argument: "the availability of `sidecars` may have changed". Its one consumer is
      `aiops/sidecars-static`, which then injects that key and `clock`, for its waits.
      The other way is an ADR-034 addition. The choice is question Q10, and it changes
      counts and lines that question Q10 lists.
    - *A denial is not "not ready".* A denial of the gate at the handshake, a peer that
      fails decision 21, a revoked pin, a bound of decision 19 that ends a session and a
      broken rule of the protocol each fail the proxy at once, with the check or the
      rule named and no budget: the row is FAILED (`08:510`; ADR-063 decision 66).
      `restart-policy` retries it like any FAILED fiber, and each retry is a new dial,
      so an instance that the operator starts with the right image is picked up with no
      new generation (`03:66`, `:88`; `07:367`). When `restart-policy` gives up, the
      row stays FAILED until an operator updates it (`07:367`); at S2b that is `plugin
      disable`, `plugin start` and `plugin enable`.

    No kernel change is planned for this with question Q10 (a). Two steps above rest on
    what the kernel does and no test shows yet: that a notification after a report moves
    a consumer that is LOADING or ACTIVE to PENDING, as a withdrawn provision does
    (`07:359-360`; `sdk/kernel/owner.go:1118-1143`), and that an ACTIVE proxy can fail.
    The first failing tests of group 2 prove both. If either needs a kernel change, that
    change is an ADR-034 addition under ADR-063 decision 3, proposed before it merges.
    The choice against the other states and the other ways back is question Q3.
19. **The bounds on what a peer may send.** The peer of the kernel is third-party code,
    so each bound is checked by the receiver before the content is used (ADR-063
    decisions 28 and 34).

    | What | Bound | When it is passed |
    |---|---|---|
    | `Hello` | 4,096 bytes as encoded; at most 16 majors; at most 8 entries of `diagnostics`, a key of at most 64 bytes and a value of at most 256. The bounds on the parts fit inside the bound on the whole | The receiver refuses the `Hello`. The kernel closes the connection; the plugin answers `bound` |
    | Any other message, as encoded, in either direction | 4 MiB | The receiver ends that call or that session with `bound`. A session ended this way fails the proxy; it is not a lost session and has no budget |
    | `Identify` | The plugin id in the grammar of the manifest reader; a version of at most 64 bytes; at most 64 definitions and 64 feature names, each of at most 128 bytes | The kernel sends `Bye` with `bound` and the proxy fails |
    | Registrations held by one activation | 256 | The `Register` is refused with `bound`; the session stays |
    | Callbacks in flight on one activation | 64 | The `Callback` is refused with `bound`; the session stays |
    | Sessions lost by one proxy row that were not held | 8 in ten minutes | The proxy fails with `flapping` (decision 18) |

    - One rule for the log of `Hello`. A receiver that refuses a `Hello` logs the peer's
      identity, the bound that was passed and the sizes, and nothing of the content. A
      receiver that accepts one logs its majors and its entries of `diagnostics`, each
      value quoted with its control characters escaped. So no log ever holds more of
      `diagnostics` than the bound (ADR-063 decision 28).
    - The SDKs apply the size bounds to what the kernel sends as well.
    - A known ceiling: S2b sets no bound on the rate of messages inside a session. A
      peer that floods costs the one goroutine that reads its session, and a rate bound
      is added by an addition to this ADR when S4b measures remote rows (ADR-036).

### The identity of the kernel and of an instance

20. **The form of each identity.** Each is an ECDSA key pair on the curve P-256 and one
    self-signed X.509 certificate, version 3, made with the standard library of Go
    inside the operations plugin. No module is linked for it.

    | Field | The instance's certificate | The kernel's certificate |
    |---|---|---|
    | Name | The subject common name is the plugin id, and one URI name is `aiops://plugin/<plugin id>` (ADR-063 decisions 23 and 55) | The subject common name is `aiops-kernel`, and one URI name is `aiops://kernel` |
    | Basic constraints | Not a certificate authority, critical | The same |
    | Key usage | Digital signature; extended usage: server authentication | Digital signature; extended usage: client authentication |
    | Validity | From one hour before it is made to 9999-12-31T23:59:59Z, the value that RFC 5280 section 4.1.2.5 gives a certificate with no well-defined expiry | The same |
    | Serial | 128 random bits | The same |

    - Neither certificate can issue another, and the check of decision 21 is an exact
      comparison, so no key of the site can mint an identity (ADR-063 decision 55).
    - The kernel side and `sdk/remotehost` are written on the TLS of the Go standard
      library and build no chain: each compares the certificate that the peer presents
      with the one it holds.
    - `serve()` cannot do that alone. The TLS inside `grpcio` verifies the dialler
      against a set of roots before any code of the SDK runs, so `serve()` gives it the
      kernel's certificate as the only root and then compares the presented certificate
      itself (decision 21). The library must therefore accept a certificate that is not
      a certificate authority, and whose key usage has no certificate signing, as its
      own trust root. That has not been run (decision 38). The instance's certificate is
      never verified by that library: only the Go kernel verifies it.
    - If the pinned `grpcio` refuses the form, the kernel's certificate alone changes:
      it is marked a certificate authority with a path length of 0, and its key usage
      gains certificate signing. `serve()` and `sdk/remotehost` still refuse every
      presented certificate that is not equal to the pinned one. A certificate signed by
      the kernel's key would then complete the TLS handshake inside `serve()` and be
      refused by that comparison, before `Hello` is answered. "No key of the site can
      mint an identity" would hold by the comparison and no longer by the form, which
      touches the words of ADR-063 decision 55. The form does not change without the
      owner's word (question Q14).
21. **Each side trusts exactly one certificate.** The connection is TLS 1.3 with both
    certificates presented; the kernel side offers nothing lower, and it is one end of
    every connection. The pin of a peer is the SHA-256 of the DER encoding of its
    certificate.
    - The kernel accepts the instance only when the presented certificate equals the
      pinned one and its URI name is the plugin id of the row that dials. The host name
      of the endpoint plays no part: the endpoint table may change it.
    - `sdk/remotehost` and `serve()` accept a dialler only when the presented
      certificate equals the kernel's certificate in the instance's directory, whatever
      the TLS library has already verified.
    - Both checks finish before `Hello` is answered (ADR-063 decision 23). A peer with
      another certificate, with a certificate signed by either pinned key, or in
      plaintext is refused, and the refusing side logs the fingerprint it saw. Note of
      2026-11-28 (decision 54): the last clause is revised. A side logs the fingerprint
      it saw where its own comparison makes the refusal: the kernel side and
      `sdk/remotehost` always, and `serve()` in its comparison of the bullet above.
      Where the TLS of `grpcio` refuses the dialler first, the line of the library is
      the record, with the reason and no fingerprint. The refusal is unchanged.
    - On success each side logs the identity it verified, once per connection: the
      kernel logs the plugin id and the fingerprint of the instance's certificate, and
      the SDK logs the kernel's name and the fingerprint of its certificate. Step C3 of
      ADR-063 decision 27 reads those two lines.
22. **The row and the tenant come from the connection** (`07:1128`; `03:162`). The row is
    the row of the locked plugin whose pin the dial used, and the tenant is `default`
    (ADR-063 decision 23). From S4b the certificate is issued for (tenant, plugin
    instance) and the same rule reads the tenant from it (`00:410`; delivery:370).
23. **Where the key material lives.** All of it is under `$AIOPS_HOME/remote/`, written
    by `aiopsctl` under the write lock of the home (ADR-061 decision 19), and the kernel
    only reads it. The S2b part of ADR-047 enters these paths as rows of its home table
    (ADR-063 decision 34).

    | Path under `$AIOPS_HOME` | Holds | Read by | Given to a unit |
    |---|---|---|---|
    | `remote/kernel/key.pem`, `remote/kernel/cert.pem` | The kernel's key and certificate | The server process, at every dial | Never: no unit binds `remote/kernel/` (ADR-063 decision 23) |
    | `remote/pins/<plugin id>/cert.pem` | The pin: the certificate that the kernel accepts for that plugin's instance | The proxy, at every dial and at every liveness check | Never |
    | `remote/instances/<plugin id>/key.pem`, `cert.pem`, `kernel.pem` | The instance's key, its certificate and a copy of the kernel's certificate | The instance | This directory and no other, read-only (ADR-063 decision 25) |

    - The home is of mode 0700 and the first two rows are files of mode 0600 that the
      site user owns, like every file of the home (ADR-047 decision 14). A kernel
      container reads them through the read-only bind of the home (ADR-047 decision 15).
      The kernel writes none of them, and none is under `run/`, the one directory that
      the kernel writes (ADR-047 decision 13).
    - The instance's user is not the site user (ADR-063 decision 25), so the third row
      needs a mode that this user can read. Decision 24 proposes it and question Q5 puts
      it to the owner.
    - A private key exists in one file. A command of the operations library that
      replaces or deletes one holds its copy in the snapshot of the home only while the
      command runs (ADR-047 decision 13). No key is in an artifact, an image,
      `plugins.lock`, a dump or a log, and a log names a plugin id and a fingerprint
      (ADR-063 decision 23).
    - The SDK learns its two inputs from two variables that the unit template sets and
      no artifact can set (ADR-063 decision 25): `AIOPS_REMOTE_LISTEN`, the address to
      listen on, and `AIOPS_REMOTE_CREDENTIALS`, the path of its directory.
    - `sdk/remotehost` and `serve()` listen in exactly two forms. With TLS:
      `AIOPS_REMOTE_LISTEN` is a TCP address and `AIOPS_REMOTE_CREDENTIALS` names a
      directory that holds the three files. Without TLS, for a check runner (decision
      27): `AIOPS_REMOTE_LISTEN` names a Unix socket, `AIOPS_REMOTE_CREDENTIALS` is not
      set, and the directory of the socket is of mode 0700 and owned by the user of the
      process. Every other combination refuses to start and names what is wrong: no
      listen address, a TCP address with no credential directory, a Unix socket with
      one, a socket directory of another mode or owner, and a credential directory that
      lacks a file. Neither SDK has an option that serves TCP in plaintext.
24. **The instance's directory is readable through the site user's group.** `aiopsctl`
    writes `remote/instances/<plugin id>/` with mode 0750 and its three files with mode
    0640, under the site user and that user's group. The unit runs the instance with a
    user id that is neither the site user's nor 0 and with the site user's group as its
    only group. On the host the 0700 home keeps every other user out of the directory,
    whatever their group. Inside the instance nothing else of the home is bound, so the
    group reaches no other file, and the reload socket checks the peer's user id, not
    its group (ADR-062 decision 5). This is one exception to "each file with mode 0600"
    of ADR-047 decision 14, and question Q5 holds the other ways. Whether the instance's
    user reads the files under each runtime and mapping that ADR-047 decision 14 admits
    has not been run (decision 38). Note of 2026-11-28, with the owner's answer to
    question Q18 (decision 57): the modes, the group and the guard of this decision are
    replaced by those of ADR-047 decision 43. The directory is of mode 0755 and its
    three files of mode 0644, the unit's user is the pair `65532:65532` and holds no
    group of the host, and on the host the guard is the home and the three directories
    of mode 0700 above the instance's directory. The heading and the first two
    sentences no longer hold. The exception to ADR-047 decision 14 stays one, in the
    form of ADR-047 decision 43.
25. **Lifetime and rotation.** A pair has no expiry: it is valid while it is pinned, as
    a signer key is valid while `trust.yaml` lists it (`00:415`). A date in a
    certificate would stop a single host at a time that no leader is there to move.
    - The instance's pair lives from the `plugin install` or the `plugin start` that
      made it to the `plugin disable` or `plugin remove` that revokes it (decision 26).
    - It is rotated by revoking it and making a new one: `plugin disable`, `plugin
      start`, `plugin enable`, with the plugin's contributions absent in between, as a
      new version arrives at S2b (ADR-063 decision 69).
    - The kernel's pair is made by the first `plugin install` that needs it and lives
      while the site holds an installed remote plugin. The `plugin remove` that removes
      the last one deletes `remote/kernel/`, and the next install makes a new pair. An
      instance trusts only the copy in its own directory, which install and `plugin
      start` write from the current pair.
    - Who can read the kernel's key: the site user; code inside a kernel container,
      which reads the home through a read-only bind and cannot write it (ADR-047
      decision 15; `08:510`); and whoever holds a copy, as a backup of the home. A
      reader of the key can stand in for the kernel towards every instance of the site
      (`00:413`), and need not be able to write the home.
    - Without a command, the only renewal of the kernel's pair is to remove every remote
      plugin of the site and install each again. This ADR therefore proposes one
      command that renews the kernel's pair alone: under the write lock it makes a new
      pair, writes `kernel.pem` into every instance's directory, replaces
      `remote/kernel/` and restarts the units it finds running. Each open session is
      lost with its unit and returns inside the reconnect budget on the new pair. It
      submits no generation. Its form is the S2b part of ADR-061. Question Q4 holds the
      choice, with the way that adds no command.
26. **Revocation with no leader.** The proxy reads the pin, the kernel's key and the
    kernel's certificate from their files at every dial and keeps none across dials,
    so a change of those files takes effect at the next dial, with no leader and no
    restart of the process (ADR-063 decision 23).
    - Revocation does not depend on the unload. At every liveness check the proxy reads
      the pin again. When it is absent, or is not the certificate that the open session
      was opened with, the kernel sends `Bye` with `revoked`, closes the connection and
      the proxy fails with that reason. So a session whose pin is gone ends within one
      liveness interval, 10 s by default, whatever the generation did.
    - With the pin absent the proxy makes no connection: it fails with the reason
      `revoked` before any byte is sent.
    - `plugin install` makes the kernel's pair on first need, the instance's pair, the
      instance's directory and the pin, in the same write as the lock entry, and a
      denial writes nothing (ADR-063 decisions 17 and 55).
    - `plugin disable` first deletes the pin, where no enabled row of another profile
      of the site names the plugin; then it submits the generation that unloads the
      proxy, which ends the open session; then it handles the outcome as the table
      below says (ADR-063 decisions 23 and 68; `03:495`; `07:1195`). While another
      profile's enabled row names the plugin it unloads the proxy of its own profile
      and touches no credential.

      The rule of the table: the pin follows the files. Where ADR-062 decision 11
      restores the snapshot, the pin returns with it and the unit is left as it was.
      Where it keeps the files, the pin stays deleted and the unit is stopped.

      | Outcome of ADR-062 decision 11 | The pin | An open session | The unit |
      |---|---|---|---|
      | `passed`, and the result shows the change | Stays deleted | Ended by the unload | Stopped |
      | `passed`, and the result does not show the change: a later layer keeps the row enabled | Stays deleted. The command exits 1 and says that the credential is revoked and which layer keeps the row | Ended with `revoked` at the proxy's next liveness check. The row is then FAILED until the layer is changed and the row is started and enabled again | Stopped |
      | `rejected`, `rolled back`, `rollback failed`, or `abandoned` with `state-unknown` | Restored with the snapshot | Kept, or, where a liveness check fell while the pin was absent, ended with `revoked`: the row is FAILED and returns with the next retry of `restart-policy`, which reads the restored pin | Not stopped |
      | An error line before an id exists | Restored with the snapshot | As the row above | Not stopped |
      | No final result, before the second submission | Stays deleted | Ended with `revoked` at the next liveness check | As the outcome of the second submission |
      | No final result on the second submission: `abandoned` without `state-unknown` | Restored with the snapshot | As for a restored pin above | Not stopped |
      | No final result on the second submission, any other case; `--wait` elapsed; the connection lost after the operation was accepted | Stays deleted | Ended with `revoked` at the next liveness check | Stopped |
      | No process runs the profile | Stays deleted | None exists | Stopped |
      | A process runs the profile and nothing answers | Stays deleted | Ended with `revoked` at the next liveness check of that process | Stopped |

    - `plugin remove` revokes as `disable` does and, after a result that shows the
      change, deletes `remote/instances/<plugin id>/` and `remote/pins/<plugin id>/`
      with the other kept files, when no profile names the id (ADR-063 decision 68;
      ADR-047 decision 10). In every other outcome its pin follows the table.
    - A revoked pair never returns: from the revocation the kernel refuses a stream
      that the old credential opens, "on that dial and on every later one" (ADR-063
      decision 23). A plugin that is enabled again therefore needs a new pair, and some
      command must make it. This ADR proposes `plugin start`: with no pin it makes a
      new key pair and certificate, replaces the three files of the instance's
      directory, writes the new pin and then starts the unit, or restarts it when it
      is already running, so that no instance keeps the old key. With a pin it writes
      nothing and starts the unit when it is not running. So an instance started again
      with the old key opens no stream on any later dial, also after the row is enabled
      again (ADR-063 decisions 23 and 28).
    - That makes `plugin start` a second command that makes an identity, where `00:412`,
      `02:483` and ADR-063 decision 55 name `plugin install` alone. It also changes the
      systemd flow of `08:1293`: after a `plugin disable`, `systemctl enable --now`
      starts the unit with the revoked key, and the operator needs `aiopsctl plugin
      start <id>` there too. The choice of the command, with the lines it edits, is
      question Q11; what `plugin start` runs for a systemd unit is the S2b part of
      ADR-061.
    - `plugin enable` refuses a row whose pin is absent before it submits anything, as
      it does for a missing endpoint, and prints the exact command `aiopsctl plugin
      start <id>` (ADR-063 decision 17). The text and the exit code are the S2b part of
      ADR-061.
27. **Who dials without a site's credential.** Four runners reach a plugin process that
    no site installed. Each uses one of the two forms of decision 23 and no third.

    | Runner | Transport | Identity |
    |---|---|---|
    | `plugin check`, steps 4 and 5; the fixture of decision 33 | A Unix socket, no TLS. The runner makes a directory of mode 0700, starts the plugin's entrypoint with `AIOPS_REMOTE_LISTEN` naming a socket in it and no credential directory, and is the only dialler | The process that the runner started (ADR-063 decision 54, reading 2) |
    | `plugin verify`, against a pulled image | The same Unix socket. The runner starts a container from the image by its digest, with no network, as the runner's own user, with that one directory bound into it | The container that the runner started |
    | `plugin smoke`; the T3 boot of a remote example (ADR-063 decision 31) | TCP with TLS, as on a server: the dial is made by a proxy inside a kernel that the runner boots. The runner makes a throwaway home, and the function that install uses makes the kernel's pair, the instance's pair and the pin in it. `plugin smoke` binds the instance's directory into the plugin's container as a unit does; the T3 runner starts the instance as its own process on a loopback address with that directory | The pairs of the throwaway home |
    | The stubs of the failure matrix; `test/remote/` | TCP with TLS, with pairs made by the same function (ADR-063 decision 64); the exit uses the pairs that `plugin install` made on the clean machine | Those pairs |

    - One function makes a pair, whoever calls it. It writes only into the home it is
      given. A runner that is not a command of a site gives it a throwaway home and
      deletes that home when it ends, so no pair of a test is ever in a site's home.
    - The Unix form carries the same `Hello`, the same session and check 3. No row of a
      server profile reaches it: the proxy dials only through the TLS function of
      decision 28, which refuses a Unix endpoint (ADR-063 decision 23).
    - The command forms of `plugin verify` and `plugin smoke` for remote code are the
      S2b parts of ADR-020 and ADR-061 (decision 39).

### The dial of the proxy and the lists of plugin vet

28. **Two dial functions, and no listener in a plugin.**
    - The kernel side of the protocol, the package of the module `sdk/remote` that
      ADR-063 decision 9 names, exports two dial functions and no other way to open a
      connection. The TLS dial takes the endpoint that `sidecars` gave, the three files
      of decision 23, the timings of decision 16 and a deadline, and returns a
      connection only after decision 21 has passed. It accepts only a TCP endpoint, a
      host and a port. For any other form, a Unix socket among them, it sends no byte
      and the proxy fails with the rule named. It has no variant without TLS. The Unix
      dial takes the path of a socket and nothing of a home.
    - The proxy plugin calls the TLS dial and never the Unix dial. The check runner in
      the operations plugin calls the Unix dial, for the socket of decision 27. So the
      endpoint table, a file of the site, cannot turn a server's dial into a plaintext
      one (ADR-063 decision 23).
    - The list of ADR-062 decision 13 gains no entry at S2b. The proxy opens no
      listener: the kernel dials and never the reverse, and callbacks ride the dialled
      connection (`07:1158`, `:1165`). The listener of an instance is opened by
      `sdk/remotehost` or by `serve()` inside the plugin's own process, so the
      directory of a remote Go plugin holds no call of that decision and vet reads it
      as it reads any plugin.
    - The kernel side of the protocol is in the module `sdk/remote`, so the kernel
      imports nothing that exists to run inside a plugin (ADR-063, question Q6 and
      decision 43). Vet reads calls and not what a library does: an in-process plugin
      that imported `sdk/remotehost` could open a listener through it. Whether the
      import boundary refuses that import is the S2b part of ADR-054 (decision 39).
    - The dial rule is not built before `dialer` exists (ADR-061 decision 38). When it
      is built, a call of either dial function is a dial under the rule, refused in
      every plugin, and the list of ADR-062 decision 14 gains two named entries:
      `aiops/remote` may call the TLS dial, and `aiops/operations` may call the Unix
      dial. That changes the words "one named exception" of `07:1405` and harness:58.
    - The other way is to dial through `dialer`. On a self-host `dialer-guarded` refuses
      link-local addresses only by default (`00:450`), so the address of an instance is
      already admitted there and that way costs nothing in ranges at S2b; the cost
      appears in hosted, where the refused ranges are the cell's, from S4b and S9b.
      What speaks against it at S2b is something else: the proxy's injects hold no
      `dialer` (`03:72`), `dialer` exists for a URL that a tenant supplied (`00:450`)
      and this endpoint is a line of a site file, and the peer is authenticated by its
      pin before any byte of the protocol. The choice is question Q6.

    Note of 2026-11-28, with the owner's answer to question Q21 (decision 60): the
    kernel-side package exports the TLS dial and no Unix dial, so it has one dial
    function where the heading counts two. The last sentence of the first bullet and
    the second sentence of the second do not hold: the check runner brings its own
    connection to the socket of decision 27, on the Unix dial of `internal/remotesuite`
    (ADR-044 decision 45). In the fifth bullet the list of ADR-062 decision 14 gains one
    named entry, `aiops/remote` for the TLS dial, and none for `aiops/operations`. The
    rest of the decision stands.

### The scope of the Python SDK

29. **What `aiops-sdk` holds at S2b** is the parts of `07:1205-1213`, as ADR-063
    decision 18 lists them: one generated module per `contrib` definition and major;
    `@contributes`; `@listens`; `aiops-sdk manifest`; `serve()`; the fixture
    `aiops_sdk.testing.host()`; and the template. At S2b the generated modules are
    those of `echoers` and `stampers` (ADR-063 decision 14). The wheel holds the
    callback envelope of decision 13 and no client of any definition: the client of the
    test definition is test code under `python/` and ships in no wheel (ADR-063
    decision 51). A Python plugin is always remote (`07:1203`). The hand-written Python
    stays under the cap of ADR-063 decision 36. Note of 2026-11-27, with the owner's
    answers on the S2b part of ADR-048: the generated modules are those of three kinds
    of definition, one marked `contrib`, one marked `callback` and one that owns a hook
    marked remote, one module per definition and major (ADR-048 decision 31; `07:1203`
    and `07:1207`; ADR-063 decisions 13 and 18, by notes of that date). The wheel of S2b
    still holds the modules of `echoers` and `stampers` alone, which own no hook and
    are no callback. The module of the test definition, which is marked `callback` and
    owns a hook marked remote, is generated into the tests of `python/` and is in no
    built package (ADR-048 decision 31).
30. **Interpreters.** The lowest interpreter is CPython 3.12, and at S2b the supported
    set is CPython 3.12 on Linux x86-64 with glibc: the one interpreter and platform
    whose wheels ADR-063 decision 35 read and that its decision 27 runs. The standard
    library of that version reads TOML, so `aiops-sdk manifest` needs no TOML package,
    and it writes the closed shape of `plugin.yaml` itself, so it needs no YAML package
    (ADR-063 decision 35). The Python job runs once, on that version (ADR-063 decision
    37). A version or a platform joins the set by an addition to this ADR, after its
    wheels are read and the job runs on it (ADR-063 decisions 27 and 35). Question Q7
    holds the wider set.
31. **`serve()` is synchronous and runs calls on a pool of threads.**
    - It reads the two variables of decision 23, opens the listener in one of the two
      forms, authenticates every dialler (decision 21) and serves the `Hello` call, the
      session and the services of the definitions that the plugin's decorators name.
    - One activation per connection (`07:1211`): for each `Activate` it builds one
      instance of every class marked `@contributes`, with that activation's config,
      registers each and every `@listens` function, and answers `Activated`. It drops
      them when the session ends.
    - `serve()` serves 32 calls at once unless it is given another number. A
      contribution call and a listener call each take one thread of the pool for as
      long as they run, a streamed call for the whole of its stream. The `Session`
      stream takes one thread for as long as its connection is open, and a reconnect
      may overlap the session it replaces. So the pool holds two threads more than the
      number of calls, and the transport's limit on calls at once is set to the same
      sum. Note of 2026-11-28 (decision 56): the transport's limit is one counter over
      sessions and calls (R2-SIZE), so by itself it does not keep the two units for
      sessions. `serve()` counts the calls itself and refuses the call that would be
      number N+1, where N is the number of calls it serves at once. The pool and the
      transport's limit stay at N+2.
    - A call that finds no free thread is refused with `RESOURCE_EXHAUSTED`. The
      transport makes that refusal before any code of the SDK runs, so it may carry no
      `aiops-reason`, and the kernel acts on the code alone (decision 10). That
      behaviour of the pinned `grpcio` has not been run (decision 38). Note of
      2026-11-28 (decisions 55 and 56): the second sentence holds on one condition,
      that `serve()` installs no server interceptor, and it installs none. It is the
      refusal at the transport's limit of N+2. The refusal of call N+1 is made by the
      count of `serve()`, in the generated handlers, which is code of the SDK. Both
      carry `RESOURCE_EXHAUSTED` and no `aiops-reason`, and the kernel acts on the code
      alone.
    - A contribution object is called from several threads at once, as a provided
      service is in Go (`07:475`). A contribution is a plain method, as `07:1222-1228`
      sketches it. Coroutine contributions are not part of S2b.
    - A thread cannot be stopped from outside. When the deadline of a call passes or
      the kernel cancels it, the kernel has its answer at once, the call context says
      so, and the method runs until it returns.
    - On `UpdateConfig` it merges the values at `paths` into the activation's config,
      then calls the method `config_updated(config, paths)` of each live contribution
      object, in the order of their registration, and answers `ConfigUpdated`. The
      generated base class has that method and it does nothing by default. An exception
      of one is the error of `ConfigUpdated`, which ends the session and brings a new
      activation with the committed config (decision 16). Note of 2026-11-28 (decision
      59): `UpdateConfig` carries the committed config whole and no `paths`. `serve()`
      replaces the activation's config with it, merges nothing and calls
      `config_updated(config)`; the method has no argument `paths`. The rest of the
      bullet stands.
    - `sdk/remotehost` applies `UpdateConfig` through the hand-off that the plugin's own
      `Define` has, so the plugin's `Volatile` fields read the new values (ADR-063
      decision 49; ADR-034, G13). A plugin whose kept manifest marks no path volatile
      never gets the message: the loader restarts its row instead. An SDK that gets one
      for a plugin with no hand-off, or for an activation it does not hold, answers
      `ConfigUpdated` with `protocol`, with the same end.
    - On the first termination signal `serve()` stops accepting, lets calls in flight
      finish for at most 5 s and exits 0. With no common major it logs both lists
      (ADR-063 decision 28). It logs to the standard error stream; the telemetry
      callback arrives at S3 (ADR-063 decision 26).
    - It ignores unknown fields, messages, calls and feature names and applies the
      bounds of decision 19 (`07:1126`; ADR-063 decision 28).
32. **The call context.** The first argument of every contribution method and of every
    listener is the call context (`07:1223`). It carries: the activation's config; the
    time left before the deadline, and whether the call was cancelled; the activation
    id, for logs; a logger; and one client per definition marked `callback` that the
    manifest's `uses` lists, under that definition's key, as `ctx.secrets` in the
    sketch of `07:1227`. The config of a context is the config at the moment the call
    began and does not change while the call runs; a call that begins after
    `ConfigUpdated` was sent gets the new config. A callback is a blocking call that
    carries what is left of the call's deadline as its `timeout_ms` (decision 16). The
    context carries no tenant and no row at S2b: both are the connection's (decision
    22), and the tenant reaches a plugin with the first definition that takes one,
    where S4b ratifies this ADR. A handle of decision 13 is a field of the request of
    the definition that mints it, not of the context. How a Python config type marks a
    field volatile is the S2b part of ADR-048 (decision 39).
33. **The fixture wraps `aiopsctl plugin check <dir> --json`** (`07:1212`; ADR-061
    decision 30). `aiops_sdk.testing.host(plugin=<dir>)` is a pytest fixture and a
    context manager. It finds the binary in the variable `AIOPSCTL` or on the search
    path, runs the command once and reads its report. `run_conformance()` passes when
    steps 4 and 5 of the report passed and otherwise fails the test with the report's
    one line per obligation (`07:1577-1578`; ADR-063 decision 17). The report is
    available to the test as read. `plugin check` runs no Python test, so the fixture
    cannot call itself (ADR-063 decision 54, readings 5 and 6). The fixture needs no Go
    toolchain and no container runtime (`07:1236`). S2b fixes these names: `host`, its
    argument `plugin`, and `run_conformance`. The sketch of `07:1236-1243` also shows
    arguments that seed a definition's fake, `tenant` and `secrets`, and `calls()`.
    This ADR proposes that they arrive with the first callback definition that ships in
    a release, at S3, by an addition to this ADR. `07:66` (R8) fixes "the method names
    ... of the test fixture ... the Python parts at S2b", so that is a change of a
    written line and is question Q12.
34. **Version and pins.**
    - The version of `aiops-sdk` is a packaging number that no gate reads (`07:545`).
      Inside the repository it is `0.0.0`, as the Go SDK modules are `v0.0.0` (ADR-060
      decision 21). A release gives it the release's version with the leading `v`
      dropped; a pre-release suffix `-alpha.N`, `-beta.N` or `-rc.N` becomes `aN`, `bN`
      or `rcN`; and the build of the package fails on a release version of any other
      form. `serve()` sends it as `sdkVersion` in `Hello.diagnostics`.
    - The package declares three run-time dependencies, each at the exact version of the
      file that ADR-063 decision 35 read: `grpcio` 1.85.4, `protobuf` 7.36.2 and
      `typing-extensions` 4.16.0. The pins of `grpcio` and `protobuf` are conditional.
      The licence texts of the libraries inside their two native extensions are not
      read, and the owner made that reading a condition before the first Python
      dependency merges (ADR-063 decision 52). If it finds a text that L7 denies until
      reviewed, the dependency does not merge, the owner is asked with that text, and
      question Q15 (c) of ADR-063 is the other way: an `aiops-sdk` without that
      runtime, which rewrites decisions 20, 21 and 31 by an addition.
    - The `protobuf` pin and the pin of the proto compiler of ADR-063 decision 45 are of
      one release of that project and move together, and the compiler's reading is the
      condition of that decision. A case of the Python job imports every generated
      module under the pinned runtime, and fails when the generated code and the runtime
      do not agree.
    - The testing part adds `pytest` 9.2.0, which no plugin image needs to run (ADR-063
      decision 18). On CPython 3.12 on Linux its metadata on the package index requires
      four more packages: `iniconfig` from 2, `packaging` from 24, `pluggy` from 1.6
      below 2, and `pygments` from 2.15. None of the four was read (ADR-063 decision
      35), so none has a pin: each gets its exact version and the SHA-256 of its file
      with its reading, before the testing part merges.
    - Not read either, and each read before its first use (ADR-063 decision 35): the
      interpreter and the form in which CI and the exit fetch it, the base image of the
      Python plugin image, and the build backend of `aiops-sdk`. The licence record
      still has the gRPC runtime of the Python SDK as not read (licence:68); its row is
      written with the commit that first uses it.
    - One constraints file under `python/` holds every package of the set with the
      SHA-256 of each file that was read, and holds no package that was not read. The
      Python job, the exit and the image build of the template install from it with
      hash checking, so nothing unread can be installed, and the check of ADR-063
      decision 52 fails on a package of the set that has no register entry.
    - A pin moves only with a new reading (ADR-063 decision 35). Nothing is published
      before L14 clears (ADR-063 decision 26); whether the published package declares
      ranges is question Q9.

### Provisional, and what waits

35. **What "provisional" means here.** Until the first community release that ships
    major 0, a message, a field, a reason, a value of decisions 16 to 19 or a part of
    decisions 20 to 34 changes by a dated addition to this ADR, proposed and accepted
    as ADR-063 decision 2 has it, with the generated code and every plugin of the
    repository in the same pull request (ADR-063 decision 62). S5 ratifies the policy
    callbacks, `audit` and `credentials`, with the listener deadlines of decision 15 and
    the scrubbed payloads; S4b ratifies one tenant per connection, activations across
    replicas and the issued certificate (`08:1175`; `07:67`, R9).
36. **Not fixed by this ADR.**

    | Part | Where | Why |
    |---|---|---|
    | The layout of each real callback and of its handle | With its definition, by an addition to this ADR: S3, S4a, S5 and S8a (decision 13) | ADR-063 decision 26; `07:74`, R16 |
    | The generated clients of a `callback` definition | S3, with the first one that ships | Decision 13; `08:680` |
    | The issued certificate, the cell's plugin CA, the virtual key and their revocation by a leader | S4b on the test provider of `sidecars`, S9b in hosted | `00:410-411`; `03:495`; ADR-063 decision 26 |
    | Several activations of one instance; a second replica's connection | S4b | `07:1163`; ADR-063 decision 67 |
    | The handover of new remote code | S6b, with `plugin update` | ADR-063 decision 69 |
    | The definition `sidecars` beyond the report of decision 18: its key, its fake, its suite, and how a provider tells which instance a consumer waits for | The S2b part of ADR-047 (decision 39) | ADR-063 decision 39 |
    | The unit template, the endpoint table and its attempt value, the network of an instance and the rows of the home table | The S2b part of ADR-047 | ADR-063 decisions 24, 25 and 34 |
    | The command forms and the refusal texts of `plugin start`, `plugin enable`, `plugin verify` and `plugin smoke`, and the command of question Q4 (b) | The S2b parts of ADR-061 and ADR-020 | ADR-063 decision 34 |
    | Every `plugin.toml` key, how a decorator names a key and how a Python config type marks a volatile field | The S2b part of ADR-048 | ADR-063 decision 34 |
    | The register entries, the import boundary over the remote modules and `python/` | The S2b part of ADR-054 | ADR-063 decisions 34 and 42 |
    | A rate bound on session messages; coroutine contributions; a file or key that sets the timings of decision 16; the trailer `aiops-error` in the generator | An addition to this ADR, when a consumer needs it | Decisions 10, 16, 19 and 31 |

### The tests

37. **Each part has a case that fails on its base** (ADR-063 decision 29). The cases of
    ADR-063 decision 28 stand; these are added, in Go and, where an SDK is named, in
    Python as well:
    - the freeze of `call.proto`, with the form that question Q2 decides;
    - `Hello` of the wrong kind, and a call of a major before `Hello`, each refused;
    - an `Identify` whose plugin id, and one whose version, differs from the kept
      manifest while its digest is right: refused with `protocol`, the proxy FAILED,
      and the row, the pin and the kept manifest the ones chosen before the dial;
    - every row of the table of decision 10, sent by one side and read by the other,
      through `sdk/remotehost` and through `serve()`;
    - a `Register` and a `Callback` that carry another activation id, and each after
      `Deactivate`, refused with the reason of decision 9;
    - two contributions of one definition, and two listeners on one hook at two stages,
      in one activation: each call reaches the registration that `aiops-effect` names,
      and a call for an effect that was unregistered is answered `inactive`;
    - a callback of a definition version that is not served, of a definition outside
      `uses` and of an unknown method, each refused with its reason;
    - a lost session that returns inside the budget: the row stays ACTIVE, calls fail
      at once in between, the entries are taken over and an entry not registered again
      is unwound; and one that does not return: the instance is reported, the row is
      PENDING and names the instance, every registration is gone, and the row is ACTIVE
      again at the next offer once the instance answers, with no command;
    - a dial at load that gets no answer: PENDING, not FAILED, reported before the load
      deadline under kernel limits of 10 s and of 1 s; and a denial at the handshake:
      FAILED, and ACTIVE after `restart-policy` retries it against an instance of the
      right image;
    - the path of steps C2 and C3: `plugin enable` before the instance listens reports
      it and rolls back; then, with the instance started, the next `plugin enable`
      dials at once and passes, also when it follows the report by less than the wait;
    - an instance that stays absent: no more than one load a minute once the wait has
      grown, and one log line for the report;
    - a peer that completes the handshake and closes, again and again: the proxy fails
      with `flapping` at the eighth loss, and a session that was held resets the wait;
    - a missing `Pong`, counted as a lost session;
    - a remote that ignores `Deactivate`: the disposer returns at its bound, under
      kernel limits of 10 s and of 1 s;
    - a reconnect whose `Activated` does not come: that attempt ends at its bound and
      the budget still ends at 30 s;
    - a dispatch with no deadline, refused, and a listener that misses its deadline on
      a gate, denied;
    - a volatile change: `sdk/remotehost` applies it through the hand-off and a
      `Volatile` field reads the new value with no new activation; `serve()` calls
      `config_updated`, a call in flight keeps its config and the next call has the new
      one; and an instance that answers with an error, or not at all, gets `Bye` and a
      new activation whose `Activate` carries the committed config, with the row ACTIVE
      throughout;
    - each bound of decision 19, in both directions; a `Hello` over each of its bounds,
      refused with nothing of its content in either log;
    - a peer whose certificate was signed by the kernel's key, and one signed by the
      instance's key, each refused by the kernel, by `sdk/remotehost` and by `serve()`;
    - the two log lines of decision 21 on a successful connection, on each side;
    - the pin and the kernel's key replaced while the process runs: the next dial uses
      the new files with no restart;
    - the pin deleted, and the pin replaced, while a session is open: the session ends
      with `revoked` within one liveness interval and the proxy is FAILED;
    - each row of the table of decision 26, among them a `plugin disable` that the site
      layer overrides: no stream is open after one liveness interval, the unit is
      stopped and the command exits 1; and a rolled-back `plugin disable`, which leaves
      the pin and the unit as they were;
    - after `plugin disable`, `plugin start` and `plugin enable`, an instance that
      still holds the old key opens no stream; `plugin start` with no pin on a unit
      that is running restarts it with the new files, on the compose target and on the
      systemd target; and `plugin enable` with no pin, refused with the exact command;
    - with question Q4 (b), the command that renews the kernel's pair: every instance
      returns inside the reconnect budget, and a dialler that holds the old key is
      refused by each;
    - the modes of decision 24, the instance's user reading its three files and failing
      to open `remote/kernel/`, another directory of the home and `run/reload.sock`, on
      each runtime and mapping that ADR-047 decision 14 admits and on the systemd
      target;
    - a line of the endpoint table that names a Unix socket, and one in any other form
      than a host and a port: the proxy fails with no byte sent;
    - `serve()` and `sdk/remotehost` in each combination of decision 23 that must not
      start, each refusing with its reason: a TCP address with no credential directory,
      a Unix socket with one, no listen address, a socket directory of another mode or
      owner, and a credential directory that lacks a file;
    - `plugin verify` on a pulled image over the Unix socket, with no network in the
      container; `plugin smoke` and the T3 boot of a remote example on pairs of a
      throwaway home, with no file of a site's home read or written and the throwaway
      home gone afterwards;
    - `serve()` with every thread of its pool busy: the next call is refused with
      `RESOURCE_EXHAUSTED` and the session, the liveness check and `Deactivate` still
      work;
    - every generated Python module imported under the pinned `protobuf`;
    - the fixture on a plugin that passes and on one that fails an obligation.

    Note of 2026-11-28, with the owner's answers of that date: four cases are read with
    decisions 54, 56, 57 and 59. The case on a peer whose certificate another key
    signed, where it reads the log of the refusal, finds the fingerprint on the kernel
    side and in `sdk/remotehost`, and for `serve()` the line of the library, with the
    reason and no fingerprint (decision 54). In the case on a full `serve()`, the call
    that is refused is call N+1, by the count of `serve()` (decision 56). The modes of
    the case on the instance's user are those of decision 57. In the case on a volatile
    change, `config_updated` is given the committed config, whole (decision 59).

### What was not run, and what this ADR depends on

38. **Three things this ADR writes were not run.** Each is run before the code that
    rests on it merges, and question Q14 asks whether acceptance waits for them.
    - The certificate form of decision 20 between the TLS 1.3 of the Go standard library
      and `grpcio` 1.85.4, with both sides authenticated: the kernel dialling `serve()`,
      and a dialler with a certificate signed by the kernel's key. If the library
      refuses the kernel's certificate as its own root, the fallback of decision 20
      applies and the owner is asked first.
    - The thread model of decision 31 on the same `grpcio`: that an open `Session`
      stream and a streamed call each hold one thread, and what a full pool answers.
      If it differs, the sizes and the answer of decision 31 change by a revision of
      this proposal, and no message changes.
    - The modes of decision 24 under the user mappings that ADR-047 decision 14 names,
      rootful with a given user and rootless with the user kept, on the compose target
      and on the systemd target, and with SELinux labels where the host enforces them.
      If the group does not reach the files under one of them, question Q5 is asked
      again for that mapping.
    - Note of 2026-11-27: the three were run on that date, and the section "Runs of
      decision 38, recorded 2026-11-27" at the end of this ADR holds the versions, the
      command and the result of each row.
    - Note of 2026-11-28: two rows of the third run were not run on that date. By
      decision 58 both are run before acceptance, on the modes of decision 57.
    - Note of 2026-11-28, after the runs of that date: both rows were run in a
      throwaway virtual machine, and both hold. The section "Runs after the answers of
      2026-11-28" holds the versions, the command and the result of each row.
39. **What this ADR sends to an addition that does not exist yet.** None of the
    additions below exists at the commit that proposes this ADR, so no reference into
    them can be checked. Each is a dependency that blocks the code named beside it, and
    is accepted before that code merges (ADR-063 decision 2). None blocks the acceptance
    of this ADR.

    | Addition | What this ADR needs of it | Blocks |
    |---|---|---|
    | ADR-047, its S2b part | The rows of `remote/` in the home table, with the modes of decision 24 as an exception of its decision 14; the endpoint table, whose line holds the endpoint and the attempt value that `plugin enable` replaces; the definition `sidecars` in its process form, with `Endpoint` and `Report` as decision 18 has them, and how a provider tells which instance a consumer waits for; the variables `AIOPS_REMOTE_LISTEN` and `AIOPS_REMOTE_CREDENTIALS` in the unit template | Groups 2 and 3 |
    | ADR-061, its S2b part | `plugin start`, with what it does on each target and with the pair it makes (question Q11); the refusal of `plugin enable` for an absent pin; with question Q4 (b), the command that renews the kernel's pair; the forms of `plugin verify` and `plugin smoke` for remote code | Group 3 |
    | ADR-020, its S2b part | `plugin verify` against a pulled image, on the Unix socket of decision 27 | Group 3 |
    | ADR-048, its S2b part | How a Python config type marks a field volatile, and the other Python parts of the manifest | Group 4 |
    | ADR-054, its S2b part | Whether the import boundary refuses an import of `sdk/remotehost` by an in-process plugin; the register entries of the Python packages | Groups 1 and 4 |
    | ADR-034, with question Q10 (b) only | The re-check that a provider asks for | Group 2 |

## Consequences

- The handshake package gains a second file and, with question Q2 (a), a second frozen
  surface: one service name and one call name.
- The adapter generator writes a service per `contrib` definition, the Go stubs on both
  sides and the Python glue on the public API of `grpcio` (ADR-063 decision 45). The
  session, the activation and the callback envelope are written once, by hand, in the
  kernel side of the protocol, in `sdk/remotehost` and in `aiops-sdk`.
- The definition `sidecars` gains the report, and `sidecars-static` gains a timer and,
  with question Q10 (a), two injects, where ADR-063 decision 39 has it inject nothing.
  With question Q10 (a) the launch definition gains a second key at S2b and the count
  of consumer-bound keys is 15; with Q10 (b) the kernel changes inside its cap.
- A failed proxy returns through `restart-policy`, and a proxy whose instance does not
  answer returns when its endpoint is offered again. The activation id is the proxy's
  own.
- No module is linked for the identity: key pairs and certificates come from the
  standard library of Go, and `serve()` verifies with the standard library of Python
  and the public API of `grpcio`.
- The home gains the directory `remote/`, with one directory whose mode is not 0700 and
  three files whose mode is not 0600 per installed remote plugin (decision 24).
- `plugin start` writes a credential when the pin is absent, so it takes the write lock
  of the home; `plugin enable` writes the attempt value of the row's line and gains one
  refusal; `plugin remove` of the last remote plugin deletes the kernel's pair; and,
  with question Q4 (b), `aiopsctl` gains one command.
- A revoked session ends within one liveness interval whatever the generation did, so a
  `plugin disable` that a later layer overrides leaves a FAILED row and no open stream.
- `00` §5.4 states the lifetime, the rotation and the revocation of the two classes of
  ADR-063 decision 55, and item 25 of `02` §6 is closed.
- The supported Python is one version on one platform until a wider set is read and
  run (decision 30), and no Python dependency merges before the readings of decision 34.
- On acceptance the row of this ADR in the tables of `00` §9 and `08` §3 changes from
  open to accepted, provisional.

## Edits on acceptance

Made in the commit that records the owner's answers (`08:999`), for the options the
owner chooses. An edit of the plugin record is an amendment of it and is marked so in
the record, with the date and this ADR (ADR-063 decision 4).

Note of 2026-11-27: the owner answered on that date. The section "Owner answers of
2026-11-27" says which of these edits are made with the record of the answers and which
wait, each with the commit that makes it.

With no question:

- `00:779` and `08:1175`: the row of ADR-041 is accepted, provisional, with the date,
  and keeps its ratifications at S5 and S4b. `01:383`, `03:629`, `05:845`, `06:805`,
  community:164 and the row of ADR-041 in plugin record §18 name it as accepted for S2b.
- `07:67` (R9): the message names of `07` §5 are those of decision 8.
- `07:1127`: the call that carries `Hello` (decision 1), and that the plugin learns the
  major from the session call (decision 3). `07:1128`: the two first messages and the
  sender of each field (decision 6).
- `07:1161-1166`: `Open`, `Identify`, `Activated`, `Unregister`, `UpdateConfig` and
  `Bye` beside the three messages named there, and the metadata `aiops-effect` on a
  call. Plugin record §11, amended: the same messages, and the reason `inactive-effect`
  where it writes `INACTIVE_EFFECT`.
- Plugin record §16, amended: in the S2b row, "whose first message already carries the
  tenant binding" reads "whose connection carries the tenant binding" (`07:1128`;
  ADR-063 decision 54, reading 3).
- `07:1169`: the set grows additively inside a major (decision 13).
- `07:1196` and `03:493`: the budget of 30 s.
- `02:215`, `02:336`, `07:1195` and `08:1293`: the words "the leader can revoke" are
  scoped to the issued certificate, from S4b; for the pinned key pair the lines name
  decision 26 (ADR-063, "Left to later commits").
- `00:412`: the revocation cell gains that an open session ends at the proxy's next
  liveness check when the pin is gone. `02:483`, item 25, and `03:495`: the credentials
  are the two pairs of decision 20, and the item is closed by this ADR.
- `07:1211` and `07:1212`: the concurrency of `serve()` and the command that the
  fixture wraps (decisions 31 and 33).
- ADR-034, G13 point 10, by a dated note: the message is `UpdateConfig`, and an
  instance that does not take it is activated again (decision 16).

With question Q1 (a):

- `07:1151`, `02:213`, `02:290` and `02:336`: one connection with one session, and the
  contribution services on that connection. Plugin record §10, amended, and the row of
  ADR-036:30 with ADR-036:364 by a dated note: "holds one stream per remote row" reads
  one connection with one session while idle; a call in flight is one more stream of
  the transport.

With question Q2 (a):

- `00:267` and `07:553`: "the handshake message and its call".

With question Q3:

- (a): `07:1196`, `03:493` and plugin record §11, amended: an instance that does not
  answer leaves the row PENDING through `sidecars` and returns when the endpoint is
  offered again, and a denial leaves it FAILED for `restart-policy`. `03:113` and
  `07:1150`: the report, the offer on a timer and the attempt value. Of the cells of
  `test/failure-matrix/matrix.yaml:360-385`, three stand as written: the optional row
  at boot (PENDING), the required row at boot (a stop, PENDING) and the live
  recomposition (a failed verdict). The cell "while running" at `:381-385` keeps its
  outcome and its diagnostic, and its state reads `{fault: PENDING}`: UNLOADING is the
  transition, which lasts only while the disposer runs and which a snapshot read every
  10 ms can miss, and PENDING is where the row ends. ADR-063 decision 20 has the cells
  pass as stated, so this one edit needs the owner's word; a case of decision 37
  asserts the unload itself. The harness keeps its 30 s for each cell (decision 16).
- (b): as (a), and `08:477` with `matrix.yaml:362`: "reconnect" reads "reconnect at the
  next `plugin enable`".
- (c): `03:113`, `07:356`, `07:1150` and plugin record §10, amended: "PENDING and never
  FAILED" no longer holds for a remote instance that does not answer. The cells at
  `matrix.yaml:364` and `:371` read `{fault: FAILED}`, the cell at `:385` reads
  `{fault: FAILED}`, and the tenant-tree cell at `:392`, due at S4b, reads the same.
  The harness keeps its 30 s for each cell.

With question Q4:

- (a): `00:412` and `00:413`: the lifetime, rotation and revocation cells read as
  decisions 25 and 26 have them, in place of "fixed by ADR-041"; the cell of the
  kernel's pair says that its only renewal is to remove every remote plugin of the
  site and install each again.
- (b): the same cells, with the command in the cell of the kernel's pair.

With question Q5 (a):

- ADR-047 decisions 13 and 14, by its S2b part: the rows of `remote/` and the modes of
  decision 24.

With question Q6:

- (a): `07:1405` and harness:58: the two named entries of the dial rule beside the
  first; a dated note at ADR-062 decision 14.
- (b): `03:72`: the proxy injects `dialer` from the step that builds it.

With question Q10:

- (a): the count of consumer-bound keys, which ADR-063 decision 57 raises to 14, is 15,
  in `00:246`, `07:564`, row 45 of the dsh checklist at `08:87` and row 45 of plugin
  record §15, amended. The count of the launch keys rises by one more in the lines that
  ADR-063 leaves to the commit of its decision 57: `00:212`, `00:250`, `03:63`,
  `07:563`, `07:1367`, `08:86`, plugin record §5, row 44 of plugin record §15 and
  ADR-059 decision 35. `dist/dist.yaml` gains the line with the key. `03:113` and a
  dated note at ADR-063 decision 39: `sidecars-static` injects that key and `clock`.
- (b): a dated addition to ADR-034, and `03:113` with a dated note at ADR-063 decision
  39: `sidecars-static` injects `clock`.

With question Q11 (a):

- `00:412`, the holder cell, and `02:483`: the pair is made by `plugin install` and,
  after a revocation, by `plugin start`. `08:1293`: after a `plugin disable` the unit is
  started with `aiopsctl plugin start <id>` on every target, systemd among them. A
  dated note at ADR-063 decision 55.

With question Q12 (a):

- `07:66` (R8): the fixture's names `host`, `plugin` and `run_conformance` are fixed at
  S2b, and its seeding arguments and `calls()` at S3. `07:1236-1243` stays a sketch.

With question Q13 (a):

- Plugin record §10, amended, and ADR-036:30 by a dated note: the 2 s is the handshake,
  and the mount of a tree with remote rows within it is the budget that S4b measures.

With question Q14, if the form of decision 20 changes: a dated note at ADR-063
decision 55.

## Questions for the owner

Each question gives its options, the recommended one, why, and what the answer blocks.
An option that changes or reads a written line says so and names the line.

**Q1. Is "one stream" one connection with one session, and are contribution calls a
service per definition?** `07:1151`: the instance "serves the contribution services of
its definitions on one stream per connection". `02:213`, `02:290`, `02:336` and plugin
record §10, which ADR-036:30 quotes, hold "one stream per remote row". ADR-063 decision
34 leaves the choice to this ADR.
- (a) One connection per proxy row carries one session stream, for the activation and
  the callbacks, and each contribution call and listener call is a call of the service
  that the definition's proto declares, on that connection (decision 4). Deadlines,
  cancellation and the server stream of a harness are the transport's own. The six
  lines read "one connection, with one session", plugin record §10 is amended, and the
  budget criterion of ADR-036:30 reads: one connection with one open stream while idle,
  and one more stream of the transport per call in flight.
- (b) Every call is a message on the one stream, in an envelope that this protocol
  defines. The lines stand to the letter. The protocol then carries its own call ids,
  cancellation, deadlines, flow control and streamed answers, in Go and in Python.
- Recommended: (a). It is what the generated service glue of ADR-063 decision 45 is
  for, a hung or slow call cannot hold the stream that carries callbacks and
  `Deactivate`, and nothing the transport already does is written twice. Blocks: the
  protocol packages and the adapter generator.
- **Answered 2026-11-27: (a)**, decision 40.

**Q2. Is the handshake call frozen with the message?** `00:267` and `07:553` freeze "the
handshake message" from its first release, and ADR-060 decision 42 holds the file to
that by a test. The call of decision 1 is new, and a plugin of any later major must
find it under the same name.
- (a) Frozen-additive from the commit that adds it: the package, the service name, the
  call name and its two message types never change, a call may be added, and a freeze
  test holds the file to that. `00:267` and `07:553` say "the handshake message and its
  call".
- (b) Frozen from the first community release that ships major 0, as ADR-063 decision
  62 has it for the major. Until then the call changes by an addition to this ADR.
- Recommended: (a). The message it carries is already frozen, the call has no shape of
  its own beyond two names, and a later rename would strand every published plugin at
  the first step. Blocks: the handshake package.
- **Answered 2026-11-27: (a)**, decision 41.

**Q3. What state does an instance that does not answer leave the row in, and what
brings the proxy back?** `07:1196` and `03:493`: an exhausted budget "unloads the proxy
and every registration is unwound". `03:113`, `07:356` and plugin record §10: a consumer
of an endpoint whose instance is not ready is "PENDING and never FAILED". The matrix row
has "PENDING or FAILED; reconnect" at boot and "unload after the budget" while running
(`08:477`), and its cells state PENDING and UNLOADING
(`test/failure-matrix/matrix.yaml:360-385`). No line says how `sidecars-static`, which
reads a table, learns that an instance stopped answering, or when it offers the
endpoint again. Steps C2 and C3 of the exit need both: `plugin enable` before `plugin
start` fails on an instance that does not answer, and the next `plugin enable` must
dial again.
- (a) PENDING, through `sidecars`, with the return of decision 18. The proxy reports,
  the provider withdraws the endpoint and offers it again on a timer, from 5 s to 60 s,
  and a new `plugin enable` dials at once through the attempt value of the table line.
  C2 then C3 pass by the attempt value; the "reconnect" of the boot cell holds by the
  timer, with no command. The definition `sidecars` gains the report, `sidecars-static`
  gains a timer, and the provider needs the path of question Q10. One matrix cell is
  edited, from the transition to the end state (`matrix.yaml:385`).
- (b) As (a) without the timer: the endpoint returns only at a `plugin enable`.
  `sidecars-static` needs no `clock`, and still needs question Q10 for the withdrawal.
  C2 then C3 pass. A row that is PENDING after boot stays so until a command, so the
  "reconnect" of `08:477` and `matrix.yaml:362` is edited to say that.
- (c) FAILED for both, retried by `restart-policy`. `sidecars` gains nothing and
  question Q10 falls away. C2 then C3 pass, since each `enable` mounts a new fiber. An
  unreachable instance is then FAILED where `03:113`, `07:356`, `07:1150` and plugin
  record §10 say never FAILED, four matrix cells are edited, and after `restart-policy`
  gives up, an instance that starts is not picked up until an operator updates the row.
- (d) `sidecars-static` probes each endpoint itself and withdraws it while the instance
  does not answer. The provider then dials, and to tell a hung handshake from a healthy
  one it would need the kernel's key. It needs question Q10 as (a) does.
- Recommended: (a). It is the state that the written lines give, the one dialler stays
  the proxy, and an instance that comes back is picked up by the timer with no command.
  Its cost is real and is stated: a report and a timer in `sidecars`, the path of
  question Q10 and one cell edit. (c) is the cheapest to build and weakens four
  written lines; (b) saves a timer and leaves a row waiting for a command; (d) adds a
  second holder of the kernel's key. Blocks: the proxy, the definition `sidecars`, the
  provider and the matrix cells.
- **Answered 2026-11-27: (a)**, decision 42.

**Q4. What are the lifetime and the rotation of the two pairs?** `00:412-413` leave
both to this ADR, and ADR-063 decision 55 gives no date and no rotation. The instance's
pair is renewed by `plugin disable`, `plugin start` and `plugin enable` in every option
but (d). The options differ on the kernel's pair. A reader of the kernel's key, who may
be code inside a kernel container or the holder of a copy of the home and need not be
able to write the home, can stand in for the kernel towards every instance of the site
(`00:413`).
- (a) No expiry and no command. After a disclosure of the kernel's key the site removes
  every remote plugin and installs each again, and until then the holder of the key can
  stand in for the kernel. The revocation cell of `00:413` says so.
- (b) No expiry, and one command that renews the kernel's pair alone: it rewrites
  `kernel.pem` in every instance's directory and restarts the units, and every instance
  returns inside the reconnect budget (decision 25). Its form is the S2b part of
  ADR-061.
- (c) As (b), with a second command that renews one instance's pair and restarts its
  unit, in place of the three commands.
- (d) A validity of a fixed number of days, as the issued certificate has one day
  (`00:410`). Something must then renew before the date on a host with no leader, and
  an instance stops answering when it does not.
- Recommended: (b). The kernel's pair is the one credential whose disclosure reaches
  every instance, and without a command its only renewal is to take every remote plugin
  of the site out and in again. An instance's pair already has three commands. (d) adds
  an outage that no component is there to prevent. Blocks: `plugin install`, `start`,
  `disable` and `remove`, and with (b) the new command.
- **Answered 2026-11-27: (b)**, decision 43.

**Q5. How does the instance's user read its credential?** The unit names a user that is
neither the site user nor root and binds the one directory of the home that holds the
instance's credential (ADR-063 decision 25). ADR-047 decision 14 writes each file of
the home with mode 0600 under the site user, which that other user cannot read.
- (a) The directory is of mode 0750 and its three files of mode 0640, and the unit gives
  the instance the site user's group as its only group (decision 24). ADR-047 decision
  14 gains the exception by its S2b part.
- (b) The directory is of mode 0755 and the files of mode 0644, readable by any user,
  behind the 0700 home. The unit's user needs no group. A private key is then
  world-readable by its mode, and only the mode of the home protects it.
- (c) `plugin start` copies the three files into storage of the container runtime that
  the instance's user owns. ADR-047 decision 14 stands. The key then exists twice, the
  copy outlives a `plugin remove` unless the command also finds it, and the systemd
  target needs another mechanism.
- Recommended: (a). One file per key, readable by one group more and no new mechanism;
  on the host the home still admits the site user alone. It has not been run under each
  user mapping (decision 38). Blocks: the unit, `plugin install` and `plugin start`.
- **Answered 2026-11-27: (a)**, decision 44.

**Q6. How does the proxy dial once plugin vet refuses a dial outside `dialer`?**
`07:1405` and harness:58 give the dial rule "one named exception", the operations
plugin's `net.DialUnix`, and ADR-062 decision 14 holds every other dial to the rule. The
rule binds from the step that builds `dialer` (ADR-061 decision 38).
- (a) Two more named entries on the same list: `aiops/remote` may call the TLS dial of
  decision 28, and `aiops/operations` the Unix dial. `07:1405` and harness:58 name
  them, and ADR-062 decision 14 gains a dated note.
- (b) The proxy dials through `dialer` from the step that builds it, and its injects
  gain `dialer` (`03:72`). On a self-host this costs nothing in ranges: `dialer-guarded`
  refuses link-local addresses only by default there (`00:450`). In hosted, from S4b and
  S9b, the refused ranges are the cell's, and the address of an instance would have to
  be admitted for every caller of `dialer`. The TLS dial of decision 28 would then take
  its connection from `dialer`, and the Unix dial keeps its own entry.
- (c) Left to the step that builds `dialer`, as ADR-061 decision 38 leaves the registry
  client.
- Recommended: (a). `dialer` exists for a URL that a tenant supplied, and this endpoint
  is a line of a site file; the peer is authenticated by its pin before any byte of the
  protocol; and the proxy's injects hold no `dialer` today. (b) is sound on a self-host
  and would be decided again for hosted. Blocks: nothing before the dial rule is built;
  the answer is needed before the proxy merges, so that each dial is one function that
  a list can name.
- **Answered 2026-11-27: (a)**, decision 45.

**Q7. Which interpreters does `aiops-sdk` support at S2b?** ADR-063 decision 35 read the
wheels of one interpreter and platform, and its decision 27 admits another only after
its files are read.
- (a) CPython 3.12 on Linux x86-64 with glibc, and nothing else until more is read
  (decision 30).
- (b) CPython 3.11, 3.12 and 3.13 on the same platform. The wheels of `grpcio` for 3.11
  and 3.13 are read first, and the Python job runs three times.
- Recommended: (a). The supported set is the one whose wheels ADR-063 decision 35 read
  and that the exit runs. That reading covers the four wheels and not what is inside
  the two native extensions, the four packages that `pytest` requires, the interpreter,
  the base image or the build backend (decision 34). Widening the set later is an
  addition that changes no message. Blocks: the Python job and the package metadata.
- **Answered 2026-11-27: (a)**, decision 46.

**Q8. Does the protocol hold a deadline for a remote listener on a gate?** The ADR table
gives this ADR "deadlines for remote listeners on gate hooks" (`08:1175`), `07:450` has
"under a deadline", and the delivery record asks that a call deadline be a value of the
definition (delivery:367).
- (a) No constant: the deadline is that of the owner's dispatch, a dispatch with none
  is refused, and a miss denies on a gate (decision 15). Each product hook states its
  value at S5.
- (b) One constant of the protocol for every remote listener, 2 s.
- (c) As (a), with a ceiling of the protocol that no definition may pass.
- Recommended: (a). A definition is first-party and a core change, so its value is
  already reviewed, and one number cannot fit both a pacing check and a policy engine.
  Blocks: the listener path of the proxy.
- **Answered 2026-11-27: (a)**, decision 47.

**Q9. Does the package pin its dependencies exactly?** ADR-063 decision 52 holds the
dependency set of `aiops-sdk` to L7, each package with a register entry and a reading.
- (a) Exact versions in the package metadata, and one constraints file with hashes
  (decision 34). An author's environment then holds the files that were read and no
  other, once every package of the set is read; today four packages of the testing part
  and the insides of two native extensions are not (decision 34).
- (b) Ranges in the metadata and exact versions only in the constraints file. An
  author's installer may then pick a version that was never read.
- Recommended: (a) while nothing is published. The choice for the public package index
  is made when L14 clears, with that index (ADR-063 decision 26). Blocks: the package
  metadata and the template.
- **Answered 2026-11-27: (a), while nothing is published**, decision 48.

**Q10. How does a provider of `sidecars` ask the kernel to check availability again?**
Options (a), (b) and (d) of question Q3 need it, for the withdrawal after a report and
for the offer that follows. The kernel runs a check again only after a notification for
the key; its one entry is a method of the root, which the launcher holds, and no plugin
can reach it (`sdk/kernel/rootview.go:247-261`; `sdk/kernel/owner.go:744-750`). ADR-063
decision 3 plans no kernel change, and its decision 39 has `sidecars-static` inject
nothing.
- (a) A consumer-bound key of the launch definition, provided by boot, with one call
  that takes no argument and notifies the key `sidecars` alone. Its one consumer is
  `aiops/sidecars-static`; the test provider of S4b and `sidecars-kubernetes` join the
  list with their steps. The kernel does not change. The count of consumer-bound keys
  is 15 where ADR-063 decision 57 makes it 14, the count of the launch keys rises by
  one more, `dist/dist.yaml` gains a line, plugin record §15 is amended, and
  `sidecars-static` injects that key and `clock`, which `03:113` and ADR-063 decision 39
  then say.
- (b) An ADR-034 addition: a call on the context of a plugin by which a provider asks
  for a re-check of a key that it provides. It serves every later provider that has
  readiness. It spends lines of the 25 that the kernel has left under its cap, and it
  is verified as ADR-060 decision 3 requires, with the differential corpus, the
  property workload and the call-path gate. `sidecars-static` then injects `clock`
  alone, and no count moves.
- (c) Neither, which is possible only with question Q3 (c).
- Recommended: (a). It keeps the kernel as it is while the call-path gate still runs on
  the S2a merge (ADR-063 decision 70), it follows the key of ADR-063 decision 57, and
  the call can re-check one key and nothing else. (b) is the general form, and the
  first provider that needs it for a second key is the place to propose it. Blocks:
  `sidecars-static`, the launch definition and the proxy's return.
- **Answered 2026-11-27: (a)**, decision 49.

**Q11. Which command makes the new pair of an instance after a revocation?** A revoked
pair never returns (ADR-063 decision 23), so a plugin that is disabled and enabled
again needs a new pair. `00:412`, `02:483` and ADR-063 decision 55 have the pair "made
by `aiopsctl plugin install`", and `08:1293` starts a systemd unit with `systemctl
enable --now`.
- (a) `plugin start`, on every target: with no pin it makes the pair, writes the files
  and the pin, and starts the unit or restarts it when it is running (decision 26).
  `00:412`, `02:483` and `08:1293` are edited, with a dated note at ADR-063 decision
  55: after a `plugin disable` the operator of a systemd target also runs `aiopsctl
  plugin start <id>`.
- (b) `plugin install` alone, as written. After a `plugin disable` the way back is
  `plugin remove` and `plugin install`, and `plugin enable` refuses with those two
  commands. No line is edited. A disable is then not a pause: the artifact is fetched
  and verified again, and the row's place in the bundle list and its layer entries are
  written again.
- (c) `plugin enable`: with no pin it makes the pair, restarts the unit and then
  submits. `plugin enable` would then start and stop units, where ADR-063 decision 17
  has it refuse and print `aiopsctl plugin start <id>`, and step C2 of the exit asserts
  that.
- Recommended: (a). The instance reads its key when it starts, so the command that
  starts it is the one place where a new key cannot be missed, and disable stays a
  pause. Blocks: `plugin start`, `plugin enable` and the unit.
- **Answered 2026-11-27: (a)**, decision 50.

**Q12. Are the seeding arguments of the fixture and `calls()` fixed at S2b?** `07:66`
(R8) fixes "the method names of the Python base classes and of the test fixture ... the
Python parts at S2b". The sketch of `07:1236-1243` shows `tenant`, `secrets` and
`calls()`, which seed and read the fake of a callback definition. S2b has one such
definition, the test definition, which ships in no release (ADR-063 decision 51).
- (a) S2b fixes `host`, its argument `plugin`, and `run_conformance`. The seeding
  arguments and `calls()` are fixed at S3, with the first callback definition that
  ships, by an addition to this ADR (decision 33). `07:66` says so.
- (b) All names are fixed at S2b as the sketch has them. `tenant` and `secrets` then
  name a definition that does not exist before S3 and S4a, and at S2b they are
  accepted and do nothing.
- Recommended: (a). A name that seeds nothing is a part with no consumer (`08:680`),
  and the names of a fake belong with its definition. Blocks: the fixture.
- **Answered 2026-11-27: (a)**, decision 51.

**Q13. Does the 2 s bound the handshake or the whole mount?** Plugin record §10 and
ADR-036:30: "a tree with remote rows mounts within the handshake deadline (2 s against
a running instance ...)". A mount ends with the row ACTIVE, which is after the plugin's
own `Apply` has returned.
- (a) The 2 s bounds the handshake, up to the verdict of the gate. The activation has
  what is left of the kernel's load deadline, as an in-process `Apply` has (decision
  16). The mount within 2 s is a budget that S4b measures (ADR-036:364), and plugin
  record §10 and ADR-036:30 say so, the first by an amendment and the second by a dated
  note.
- (b) The 2 s bounds the whole of the proxy's `Apply`, to `Activated`. The two lines
  stand to the letter. A plugin whose `Apply` needs longer then fails as a remote
  plugin and passes in process, under the same suite.
- Recommended: (a). One `Plugin` value should meet one bound in both runtimes
  (`07:1198`), and ADR-036 already defers the figure to a measurement. Blocks: the
  proxy's `Apply`.
- **Answered 2026-11-27: (a)**, decision 52.

**Q14. Does acceptance wait for the three runs of decision 38?** Three things this ADR
writes were not run: the certificate form against the pinned `grpcio`, the thread model
of `serve()` on it, and the file modes under each user mapping. The owner said on
2026-11-27 that a sandbox environment can be provided. The three runs need a Go
toolchain, CPython 3.12 with the pinned wheels, and both container runtimes with
systemd.
- (a) The three are run before acceptance, as throwaway runs that commit nothing. Their
  results are recorded in this ADR by a dated note, and a result that changes a
  decision comes back as a revised proposal.
- (b) The ADR is accepted first. The cases of decision 37 make the runs with the code.
  If the certificate form is refused, the fallback of decision 20 is put to the owner
  then, with a dated note at ADR-063 decision 55; if the modes fail under a mapping,
  question Q5 is asked again.
- Recommended: (a). The certificate form is under four decisions and under the unit,
  and a refusal found after acceptance reopens the identity that ADR-063 decision 55
  settled. The runs are small. Blocks: with (a), acceptance; with (b), group 2 for the
  certificate form and group 3 for the modes.
- **Answered 2026-11-27: (a)**, decision 53.

## Owner answers of 2026-11-27

The project owner answered every question of the section "Questions for the owner" on
2026-11-27, each with its recommended option, one of them for as long as a condition
holds (decision 48). Each decision below names the question it answers. Where a decision
above leaves a point to a question, the decision here settles it. The answers do not
accept this ADR: by decision 53 it is accepted when the three runs of decision 38 are
recorded, and until then its status is "answers recorded".

Note of 2026-11-27: the runs are recorded in the section "Runs of decision 38, recorded
2026-11-27". Three rows fail and two were not run, so the ADR is not accepted by that
record, and the points go to the owner as questions Q15 to Q21 of that section.

Note of 2026-11-28: the owner answered Q15 to Q21 on that date. The section "Owner
answers of 2026-11-28" records the answers as decisions 54 to 60, and each of decisions
8, 16, 21, 24, 28, 31, 37, 38, 44 and 45 carries a note of that date.

40. **"One stream" is one connection with one session, and a contribution call is a call
    of the service of its definition** (Q1 (a)). One connection per proxy row carries
    one session stream, for the activation and the callbacks, and each contribution call
    and each listener call is a call of the service that the definition's proto
    declares, on that connection (decision 4). Deadlines, cancellation and the server
    stream of a harness are the transport's own. `07:1151`, `02:213`, `02:290` and
    `02:336` say one connection with one session. Plugin record §10 is amended (ADR-063
    decision 4), and a dated note in ADR-036 gives the budget line of ADR-036:30, and
    ADR-036:364 with it, this reading: one connection with one open stream while idle,
    and one more stream of the transport per call in flight.

41. **The handshake call is frozen with the message** (Q2 (a)). The call of decision 1
    is frozen-additive from the commit that adds it: the package, the service name, the
    call name and its two message types never change, a call may be added, and a freeze
    test holds `call.proto` to that. `00:267` and `07:553` say "the handshake message
    and its call".

42. **An instance that does not answer leaves the row PENDING, through `sidecars`, and
    the proxy returns as decision 18 has it** (Q3 (a)). The proxy reports, the provider
    withdraws the endpoint and offers it again on a timer, from 5 s to 60 s, and a new
    `plugin enable` dials at once through the attempt value of the table line. A denial
    leaves the row FAILED for `restart-policy`. The definition `sidecars` gains the
    report, `sidecars-static` gains a timer, and the provider asks for the re-check by
    the path of decision 49. `07:1196`, `03:493` and plugin record §11, which the answer
    amends, say so, and `03:113` and `07:1150` hold the report, the offer on a timer and
    the attempt value. One matrix cell is edited, from the transition to the end state:
    the cell "while running" of the row "Remote unreachable or handshake mismatch"
    (`test/failure-matrix/matrix.yaml:381-385`) keeps its outcome and its diagnostic and
    states `{fault: PENDING}`. This answer is the owner's word that the edit needs under
    ADR-063 decision 20. The three other S2b cells of the row, `08:477` and the 30 s
    that the harness gives a cell stand as written.

43. **Neither pair has an expiry, and one command renews the kernel's pair alone**
    (Q4 (b)). The lifetime and the rotation of both pairs are those of decision 25. The
    instance's pair is renewed by `plugin disable`, `plugin start` and `plugin enable`.
    The kernel's pair is renewed by one command of `aiopsctl`: under the write lock it
    makes a new pair, writes `kernel.pem` into every instance's directory, replaces
    `remote/kernel/` and restarts the units it finds running. Every instance returns
    inside the reconnect budget, and the command submits no generation. Its form is the
    S2b part of ADR-061 (decision 39). The lifetime and revocation cells of `00:412` and
    `00:413` read as decisions 25 and 26 have them, in place of "fixed by ADR-041", with
    the command in the cells of the kernel's pair.

44. **The instance's user reads its credential through the site user's group** (Q5 (a)).
    The directory `remote/instances/<plugin id>/` is of mode 0750 and its three files
    are of mode 0640, and the unit gives the instance the site user's group as its only
    group (decision 24). ADR-047 decision 14 gains the exception, and the home table of
    its decision 13 the rows of `remote/`, by the S2b part of ADR-047. The modes have
    not been run under each user mapping. That is the third run of decision 38, and if
    the group does not reach the files under one mapping, question Q5 is asked again for
    that mapping. Note of 2026-11-28: the owner's answer to question Q18 revises this
    decision to ADR-047 decision 43 (decision 57). The directory is of mode 0755, its
    three files are of mode 0644, and the unit gives the instance the user
    `65532:65532` and no group of the host.

45. **The dial rule gains two named entries** (Q6 (a)). When plugin vet refuses a dial
    outside `dialer`, `aiops/remote` may call the TLS dial of decision 28 and
    `aiops/operations` the Unix dial, on the list of ADR-062 decision 14. The proxy does
    not dial through `dialer`, and its injects gain nothing (`03:72`). `07:1405` and
    harness:58 name the two entries beside the first, and ADR-062 decision 14 gains a
    dated note. Note of 2026-11-28: the owner's answer to question Q21 takes the entry
    of `aiops/operations` for the Unix dial out of this decision (decision 60). The dial
    rule gains one named entry, `aiops/remote` for the TLS dial; `07:1405` and
    harness:58 name it beside the first, and ADR-062 decision 14 gains a second dated
    note.

46. **`aiops-sdk` supports one interpreter on one platform at S2b** (Q7 (a)): CPython
    3.12 on Linux x86-64 with glibc, and nothing else until more is read (decision 30).
    No line is edited.

47. **The protocol holds no deadline for a remote listener** (Q8 (a)). The deadline is
    that of the owner's dispatch, a dispatch with none is refused, and a miss denies on
    a gate (decision 15). Each product hook states its value at S5. No line is edited.

48. **The package pins its dependencies exactly, while nothing is published** (Q9 (a)):
    exact versions in the package metadata, and one constraints file with hashes
    (decision 34). The answer holds while nothing is published. The choice for the
    public package index is made when L14 clears, with that index (ADR-063 decision 26).
    No line is edited.

49. **A provider of `sidecars` asks for the re-check through a consumer-bound key of the
    launch definition** (Q10 (a)). Boot provides the key, which carries one call that
    takes no argument and notifies the key `sidecars` alone. Its one consumer is
    `aiops/sidecars-static`; the test provider of S4b and `sidecars-kubernetes` join the
    list with their steps. The kernel does not change. `sidecars-static` injects that
    key and `clock`, which `03:113` and a dated note at ADR-063 decision 39 say. The
    count of consumer-bound keys is 15, where ADR-063 decision 57 made it 14, in
    `00:246`, `07:564`, row 45 of the dsh checklist at `08:87` and row 45 of plugin
    record §15, which the answer amends. The key is named with the minor of `launch`
    that adds it. The count of the launch keys rises by one more, and `dist/dist.yaml`
    gains the line with the key, in the commit that adds the key (listed below).

50. **`plugin start` makes the new pair of an instance after a revocation, on every
    target** (Q11 (a)). With no pin it makes the pair, writes the files and the pin, and
    starts the unit or restarts it when it is running (decision 26). After a `plugin
    disable` the operator of a systemd target also runs `aiopsctl plugin start <id>`.
    The holder cell of `00:412`, `02:483` and `08:1293` say so, and ADR-063 decision 55
    gains a dated note.

51. **S2b fixes three names of the fixture** (Q12 (a)): `host`, its argument `plugin`,
    and `run_conformance`. The seeding arguments and `calls()` are fixed at S3, with the
    first callback definition that ships, by an addition to this ADR (decision 33).
    `07:66` (R8) says so, and `07:1236-1243` stays a sketch.

52. **The 2 s bounds the handshake, up to the verdict of the gate** (Q13 (a)). The
    activation has what is left of the kernel's load deadline, as an in-process `Apply`
    has (decision 16). The mount of a tree with remote rows within 2 s is a budget that
    S4b measures (ADR-036:364). Plugin record §10 says so by an amendment and ADR-036 by
    a dated note.

53. **Acceptance waits for the three runs of decision 38** (Q14 (a)). The certificate
    form of decision 20 against the pinned `grpcio`, the thread model of decision 31 on
    it, and the file modes of decision 24 under each user mapping are run before
    acceptance, as throwaway runs that commit nothing. Their results are recorded in
    this ADR by a dated note, and this ADR is accepted only after that record. The
    answers of decisions 40 to 52 stand as given, and a run result that changes a
    decision comes back to the owner as a revised proposal. Until the runs are recorded
    the status of this ADR is "answers recorded", and its row in the tables of `00` §9
    and `08` §3 stays as it is.

The edits of the section "Edits on acceptance" are made in the commit that records these
answers (`08:999`), for the chosen options and for its list "With no question", except
the lines listed below as left. Lines are cited at `bbfd839`, as above, and no edit of
`docs/architecture/` adds or removes a line. Decisions 40 to 52 name the lines of their
questions. The edits made with no question are:

- `07:67` (R9): the message names of `07` §5 are those of decision 8;
- `07:1127` and `07:1128`: the call that carries `Hello`, how the plugin learns the
  major, and the two first messages with the sender of each field (decisions 1, 3 and
  6);
- `07:1161`, `07:1162` and `07:1164`: `Open`, `Identify`, `Activated`, `UpdateConfig`,
  `Bye` and `Unregister` beside the three messages named there, and the metadata
  `aiops-activation` and `aiops-effect` of a call (decisions 8 and 9); plugin record
  §11, amended, with the reason `inactive-effect` where it wrote `INACTIVE_EFFECT`;
- plugin record §16, amended: in the S2b row the connection carries the tenant binding,
  and the first message the activation id (decisions 6 and 22);
- `07:1169`: the callback set grows by addition inside a major (decision 13);
- `07:1196` and `03:493`: the budget of 30 s (decision 18);
- `02:215`, `02:336`, `07:1195` and `08:1293`: the words "the leader can revoke" are
  scoped to the issued certificate, from S4b, and the sentence on the pinned key pair
  names decision 26;
- `00:412`, the revocation cell: an open session ends at the proxy's next liveness check
  when the pin is gone. `02:483`, item 25, and `03:495`: the credentials are the two
  pairs of decision 20;
- `07:1211` and `07:1212`: the concurrency of `serve()` and the command that the fixture
  wraps (decisions 31 and 33);
- ADR-034, G13 point 10, by a dated note: the message is `UpdateConfig`, and an instance
  that does not take it is activated again (decision 16).

Text that restates an edited line is edited with it: the paragraph of plugin record §11
on starting a remote instance, which restates `08:1293`; `08:646`, which had
`sidecars-static` inject nothing and now has it inject none of the three keys that the
line names; and the list of amendments at the head of the plugin record.

Left to later commits, each named:

- the status of this ADR where another document states it: the rows of ADR-041 at
  `00:779` and `08:1175`; the lines that are to name it as accepted for S2b, `01:383`,
  `03:629`, `05:845`, `06:805` and community:164; the row of ADR-041 in plugin record
  §18; and the closing of item 25 of `02` §6 (`02:483`). Each waits for the commit that
  records the three runs of decision 38 and sets this ADR accepted (decision 53). Until
  then each names an ADR that is not accepted, which is the case;
- the rows of `remote/` in the home table and the exception to "each file with mode
  0600", in ADR-047 decisions 13 and 14, with the record of the owner's answers on the
  S2b part of ADR-047 (decision 44);
- the form of the command that renews the kernel's pair, with the S2b part of ADR-061,
  which names no such command at this commit; it blocks group 3 (decisions 39 and 43).
  `00:413` says what the command does and gives it no name;
- the count of the launch keys, which decision 49 raises by one more, in `00:212`,
  `00:250`, `03:63`, `07:563`, `07:1367`, `08:86`, plugin record §5, row 44 of plugin
  record §15 and ADR-059 decision 35, and the line of `dist/dist.yaml`, with the commit
  that adds the key to the launch definition, where the key gets its name. ADR-063
  leaves the same lines to the commit of its decision 57. Until then these lines count
  the launch definition as it stands, and `00:246` and `07:564` mark both keys "from
  S2b";
- the catalogue rows of `sidecars`, `00:219` and the row of plugin record §5, which
  state `Endpoint(name)` alone, with the S2b part of ADR-047, which fixes the definition
  beyond the report (decision 36);
- a dated note at ADR-063 decision 55 on the form of the kernel's certificate, only if
  the first run of decision 38 changes the form of decision 20, with the revised
  proposal that the owner then answers;
- the row of the gRPC runtime of the Python SDK in the licence record (licence:68), with
  the commit that first uses it (decision 34).

Not edited. `08:477` and the three other S2b cells of its matrix row stand under
decision 42. `03:72` stands: decision 45 gives the proxy no `dialer`. `07:1236-1243`
stays a sketch (decision 51). `03:489`, community:160-161 and `08:894` restate rows 2
and 3 of the table of `07` §5 as they stood and hold nothing that an answer changes. The
words "to 14" of ADR-063 decision 57, the heading "one named exception" of ADR-062
decision 22 and the sentences of the S2b parts of ADR-047 and ADR-061 that cite this ADR
as proposed stand as text of their date. The sections "Decision", "Consequences", "Edits
on acceptance" and "Questions for the owner" of this ADR keep the text of the proposal,
with each question marked answered. The ADR table of `08` is not edited. The community
record and the licence record are not edited: the one line of each that an answer
touches is in the list above.

## Runs of decision 38, recorded 2026-11-27

Note of 2026-11-27. The three runs that decision 53 puts before acceptance were made on
that date, as throwaway runs on the tree of commit `3dc2c41`. They committed nothing, and
every container, key and file that they made was removed. This section is the record
that decision 53 asks for: the versions, the command and the result of each row. A row
"holds" when the run showed what this ADR writes, "fails" when it showed otherwise, and
is "not run" where the host could not make it. Nothing is inferred for a row that was
not run.

**The ADR is not accepted by this record.** The certificate form of decision 20 holds,
and the fallback is not needed. Three rows fail, each on one sentence: the fourth bullet
of decision 21, and the third and the fourth bullet of decision 31. Two rows of the
third run were not run. By decision 53 a result that changes a decision comes back to
the owner, so the status stays "answers recorded", and questions Q15 to Q19 below go to
the owner, each with the lines that an option edits. Question Q20 is not from a run: it
is the point that the record of ADR-034 leaves for this ADR. Question Q21 is not from a
run either: it is a difference between decisions 28 and 45 and the owner's answer on the
S2b part of ADR-044 (its decision 45), which no record of that date named. Decisions 40
to 53 stand as the owner gave them, except where a question below names one.

### Versions

- Go `go1.26.8` on linux/amd64, with the `crypto/tls` of its standard library,
  `google.golang.org/grpc` v1.82.1 at the hash that ADR-063 records, and
  `google.golang.org/protobuf` v1.36.11.
- CPython 3.12.16 on Debian 13 with glibc 2.41, x86-64, from the image
  `python@sha256:191083cdf6c7bda8c76158983163ead5d5cf7b77cf3033e805bb0f5296f51b4b`.
- `grpcio` 1.85.4, `protobuf` 7.36.2 and `typing-extensions` 4.16.0, installed with `pip
  --require-hashes` against the three SHA-256 values that ADR-063 records.
- Docker Engine 29.10.0, rootful, with no remapped user namespace, on the overlay
  filesystem and under the AppArmor profile `docker-default`; Docker Compose v5.8.0;
  systemd 259. The host is Ubuntu 26.04.1 on Linux 7.0.0-43. Its site user is
  `1000:1000`; the home is of mode 0700, and `remote/kernel/*.pem` and the pin are of
  mode 0600.
- The host holds no Podman and no other second runtime, and it does not enforce SELinux.

Two throwaway programs were written for the runs and are in no commit. `probe` is a Go
program on the standard library. `probe gen` makes a home with the pairs of decision 20
(ECDSA P-256, `x509.CreateCertificate`, a serial of 128 random bits, `NotAfter`
9999-12-31T23:59:59Z, one URI name, a PKCS#8 key) and a directory of certificates that
decision 21 has to refuse. `probe dial` dials with TLS 1.3 as its lowest version and no
chain: its `VerifyConnection` accepts the peer only when the presented certificate
equals the pin and its URI name is `aiops://plugin/<id>`. `serve_tls.py` is a `grpcio`
server with `grpc.ssl_server_credentials(((key, cert),), root_certificates=kernel.pem,
require_client_auth=True)`, whose `Hello` handler compares `x509_pem_cert` of the auth
context with `kernel.pem`. `<home>` below is the throwaway home, and `<id>` is
`aiops-d38-echo`.

### Run 1: the certificate form (decisions 20 and 21)

The server of every row: `docker run -d --user 12345:1000 -v
<home>/remote/instances/<id>:/run/aiops/credentials:ro -e
AIOPS_REMOTE_LISTEN=0.0.0.0:50051 -e AIOPS_REMOTE_CREDENTIALS=/run/aiops/credentials
python@sha256:1910... python -I serve_tls.py`.

| Row | What was run | Command | Result | Observed |
|---|---|---|---|---|
| R1-A1 | The kernel in Go dials `serve()` with the certificate form of decision 20 as written: the kernel's certificate self-signed, not a certificate authority (critical), key usage digital signature alone, extended usage client authentication, and given to `grpcio` as its only root. Both sides authenticated | `probe gen -home <home> -rogue <dir> -id <id>`, then `probe dial -addr <address of the container>:50051 -key remote/kernel/key.pem -cert remote/kernel/cert.pem -pin remote/pins/<id>/cert.pem` | holds | Go: code `OK`, the reply received, TLS 1.3 with `TLS_AES_128_GCM_SHA256`, and the instance's certificate equal to the pin, with the URI name `aiops://plugin/aiops-d38-echo`. `serve()`: the presented fingerprint equal to that of `kernel.pem`; the auth context held `transport_security_type` `ssl`, the common name `aiops-kernel`, the URI `aiops://kernel` and `ssl_tls_version` `TLSv1.3`. `grpcio` 1.85.4 accepts a certificate that is not a certificate authority and has no certificate-signing usage as its own trust root. OpenSSL 3.5.5 printed the form: version 3, key usage critical, basic constraints critical with `CA:FALSE`, not after 9999-12-31 23:59:59 GMT. Go marks the key usage critical, and the table of decision 20 does not say. The row was repeated after the refusals below, with the same result |
| R1-A2 | A dialler with a certificate signed by the kernel's key, in two variants: the subject `mallory`, and the subject `aiops-kernel`, equal to the issuer | `probe dial` with each of those pairs; the server ran with `GRPC_VERBOSITY=DEBUG GRPC_TRACE=handshaker,tsi` | holds | Both were refused inside the TLS of `grpcio`, before any code of the SDK ran: no `Hello` reached the handler, and the library wrote `CERTIFICATE_VERIFY_FAILED: unable to get local issuer certificate`. The Go dialler got `Unavailable` with `error reading server preface: EOF`: its own handshake had completed, and it saw no alert and no reason. In the form as written no key of the site mints an identity by the form itself, as decision 20 and ADR-063 decision 55 say |
| R1-A4 | The other peers of decision 21 against `serve()`: another self-signed certificate of the same form, no client certificate, and plaintext | `probe dial` with the other self-signed pair; `probe dial -mode nocert`; `probe dial -mode plaintext` | holds | All three were refused by `grpcio` before any code of the SDK: `CERTIFICATE_VERIFY_FAILED: unable to get local issuer certificate`, `PEER_DID_NOT_RETURN_A_CERTIFICATE` and `WRONG_VERSION_NUMBER`. Go saw `Unavailable` with `error reading server preface: EOF` each time |
| R1-A7 | The kernel side refuses an instance by exact comparison on the Go standard library, with no chain (decision 21, first bullet): the pin is another certificate; the pin is right and the row is another plugin id | `probe dial ... -pin <the other self-signed certificate>`; `probe dial ... -pin <the right pin> -id some-other-plugin` | holds | Go: `Unavailable`, with `presented certificate is not the pin` and with `URI name is not aiops://plugin/some-other-plugin`. No `Hello` reached `serve()`, which logged `SSLV3_ALERT_BAD_CERTIFICATE` for both. The Go side accepted the instance's certificate, which is not a certificate authority, has the extended usage server authentication and expires in 9999, with no chain and no host name check |
| R1-LOG | Decision 21, fourth bullet, "the refusing side logs the fingerprint it saw", for `serve()` where the TLS of `grpcio` makes the refusal | The same server with no `GRPC_*` variable; the refused dials of R1-A2 and R1-A4; the log of the container | fails | On every refusal of R1-A2 and R1-A4 no Python code ran, so the SDK never saw the presented certificate. At the default verbosity the library wrote one line per refusal, with a reason and no fingerprint. In the form as written every dialler that `serve()` refuses is refused at that layer. Whether another interface of `grpcio` hands a refused certificate to Python was not tested. The success line of decision 21 works: `x509_pem_cert` is in the auth context of each call |
| R1-FB | The fallback of decision 20, run for the record: the kernel's certificate a certificate authority with a path length of 0, and its key usage with certificate signing | `probe gen -ca`, the same server and the same nine dials | holds | It behaves as decision 20 describes it. The kernel's pair: `OK` at TLS 1.3. Both certificates signed by the kernel's key completed the TLS handshake and were refused by the comparison in the `Hello` handler: Go got `Unauthenticated`, and `serve()` logged the refusal with the fingerprint it saw. Another self-signed certificate authority of the same form, no certificate and plaintext were refused as in R1-A4. The form as written was not refused, so the fallback is not needed |

### Run 2: the thread model (decision 31)

`threads.py` is a `grpcio` server of the synchronous interface on the same interpreter
and packages, with a client channel of `grpcio` in the same process over the loopback
address. It ran as `docker run --rm --network none --user 12345:1000 ... python -I
threads.py`. It is sized as decision 31 with 4 calls in place of 32: a
`ThreadPoolExecutor` of 6 workers, which counts the tasks that run and that wait, and
`maximum_concurrent_rpcs=6`. Its handlers are `Session`, a stream in both directions
that answers a ping and a deactivate; `Block`, a unary call that waits on an event; and
`Stream`, a server stream that yields one item and waits.

| Row | What was run | Result | Observed |
|---|---|---|---|
| R2-SESSION | An open `Session` stream holds one thread of the pool for as long as it is open, and a streamed call one for the whole of its stream | holds | Idle: no task and no thread. One `Session` open and a ping answered: one task running. A second ping 1 s later: still one task, on the same thread, so reading the requests of the stream takes no second thread. One streamed call with its stream open: a second thread, until its handler returned. Each unary call: one thread. After the release and the close of the client: none running |
| R2-FULL | What a full pool answers, and that the session, the liveness check and `Deactivate` still work (decisions 31 and 37) | holds | With 6 running and the limit at 6, the next call got `RESOURCE_EXHAUSTED` with the details `Concurrent RPC limit exceeded!` and empty trailing metadata, so no `aiops-reason`, after about 1 ms, and its handler did not run. The open `Session` answered the ping and the deactivate with the pool full. After the handlers returned the next call was `OK`. Control: with no `maximum_concurrent_rpcs` a third call on a pool of 2 waited in the queue and ended `DEADLINE_EXCEEDED` at its deadline of 1 s. The refusal comes from the transport's limit, as decision 31 sets it, and not from the pool |
| R2-PRE | "The transport makes that refusal before any code of the SDK runs" (decision 31, fourth bullet) | fails | A `grpc.ServerInterceptor` in the same server recorded each call of `intercept_service`. For each refused call the interceptor ran first, on the one thread of the server that accepts every call, and only then was the call rejected: in `grpc/_server.py` of the wheel, lines 1082-1113, `_handle_call` looks the method handler up, which runs the interceptors, before it tests the limit. No handler ran and nothing was put on the pool, and the refusal carried no trailing metadata. The sentence is therefore true only where `serve()` installs no interceptor |
| R2-SIZE | The sizes of decision 31, third bullet: "serves N calls at once", and two threads more kept for the `Session` and for a reconnect that overlaps it | fails | The limit is one counter over sessions and calls together. With one `Session` open, call N+1, the fifth, was accepted and ran, 6 of 6. With the pool then full, a second `Session`, the overlapping reconnect, was refused with `RESOURCE_EXHAUSTED`. With two sessions open the fifth call was refused, as decision 31 intends. With one session the instance therefore serves N+1 calls, and the room for a reconnect is not reserved: calls can take it. Nothing in the limit of `grpcio` can reserve it; a count of calls kept by `serve()` would |
| R2-CANCEL | A call that the kernel cancels keeps its thread until the method returns (decision 31, sixth bullet) | holds | The client saw `CANCELLED` at once. The pool still had 6 running, and the next call was still refused with `RESOURCE_EXHAUSTED`: the count of the transport falls only when the handler has returned. A method that outlives its deadline or its cancellation keeps one thread and one unit of the limit until it returns |

### Run 3: the modes (decision 24, question Q5 (a))

`modes.py` prints the user, the group and the groups of its process, reads and lists
the bound directory, tries to write into it and tries each other path of the home.

| Row | What was run | Command | Result | Observed |
|---|---|---|---|---|
| R3-DOCKER | Rootful Docker with a given user: the directory of mode 0750, the three files of mode 0640, owner and group the site user's; the instance with a user id that is neither the site user's nor 0 and the site user's group as its only group; that one directory bound read-only | `docker run --rm --network none --user 12345:1000 -v <home>/remote/instances/<id>:/run/aiops/credentials:ro python@sha256:1910... python -I modes.py`; the same with `--user 12345:12345` | holds | User 12345, group 1000, groups 1000 alone. `key.pem`, `cert.pem` and `kernel.pem` were read and the directory listed. Opening `key.pem` for writing: `Permission denied`; creating a file: `Read-only file system`. `remote/kernel/`, `remote/pins/`, the home and `run/reload.sock`, by their paths on the host: no such file, since nothing else is bound. With the group 12345: `Permission denied` on the directory and on the key, so the group is what reaches the files. In run 1 `grpcio` as `12345:1000` loaded the key of mode 0640 through the group and served TLS with it |
| R3-COMPOSE | The same on the compose target | `docker compose -f compose.yaml up`, with a service of `user: "12345:1000"`, `network_mode: none` and one bind of the directory, read-only; then `docker compose down --volumes` | holds | As R3-DOCKER: user 12345, group 1000, groups 1000; the three files read; the write refused; every other path of the home absent |
| R3-SYSTEMD | The same on the systemd target, where the unit runs the image through the container runtime of the host (ADR-063 decision 25) | `systemd-run --user --wait --collect --pipe --quiet docker run --rm --network none --user 12345:1000 -v <home>/remote/instances/<id>:/run/aiops/credentials:ro ... modes.py` | holds | The unit exited 0, with the output of R3-DOCKER. The limits of this row: it was a transient unit of the user's service manager. It was not a unit of the system, which the host could not run without the rights of root, not a unit rendered from the template, which does not exist yet, and not a systemd unit over Podman |
| R3-HOST | "On the host the 0700 home keeps every other user out of the directory, whatever their group" (decision 24), and the three paths of decision 37 that the instance's user must fail to open | A copy of the home under a parent of mode 0755, the parent bound whole; a live Unix socket of mode 0600 at `run/reload.sock`; `docker run` as `12345:1000`, as `12345:12345` and, as the control, as `1000:1000`: list the home, read the instance's key through the home, read `remote/kernel/key.pem`, list `remote/pins`, connect to the socket | holds | As `12345:1000` and as `12345:12345`: the parent listed, then `Permission denied` on the home, on the instance's key through the home, on `remote/kernel/key.pem`, on `remote/pins` and on the connect to the socket. The control as `1000:1000` read all of them and connected. On rootful Docker with no user namespace the user 12345 of the container is the user 12345 in the permission check of the host. No second login user of the host was used |
| R3-PODMAN | Rootless with the user kept, the second mapping of ADR-047 decision 14 (rootless Podman with `--userns=keep-id`), on the compose target and on the systemd target | None | not run | The host holds no Podman, and its Docker is rootful alone. Nothing is known from these runs about whether a user that is neither the site user nor 0, inside a user namespace that keeps the site user's id, has the site user's group on the bound files |
| R3-SELINUX | The binds with SELinux labels where the host enforces them | None | not run | The host does not enforce SELinux. The read-only bind and the read through the group worked under the AppArmor profile `docker-default`, which says nothing of a label of SELinux (the `z` option of ADR-047 decision 14) |

### What the results touch

- **Decision 20 and ADR-063 decision 55, the identity: no change.** `grpcio` 1.85.4
  accepts the kernel's certificate in the form as written as its own root, and refuses
  a certificate signed by the kernel's key inside TLS. The fallback is not needed, the
  owner is not asked about the form, and ADR-063 decision 55 gains no note.
- **Decision 21, fourth bullet,** does not hold for `serve()` as written (R1-LOG): a
  dialler that the TLS of `grpcio` refuses is refused where no code of the SDK runs.
  Question Q15. The same refusal reaches the kernel as `Unavailable` with no reason, so
  under decisions 10 and 18 the kernel acts on the code alone.
- **Decision 31, fourth bullet,** holds only where `serve()` installs no server
  interceptor (R2-PRE). Question Q16. The code and the empty trailers are as written,
  and no message changes.
- **Decision 31, third bullet,** does not hold as written (R2-SIZE): the two threads
  more are not kept for sessions. Question Q17. By decision 38 this is a revision of the
  sizes and of the answer of decision 31, and no message changes.
- **Decision 24 and decision 44** hold on rootful Docker with a given user, on the
  compose target and through a transient systemd unit, and Q5 is not asked again for
  that mapping. They are not determined for rootless with the user kept or under
  SELinux. The record of the owner's answers on the S2b part of ADR-047 gives the same
  directory other modes (its decision 43), and row 10 of its decision 39 holds under
  those. Questions Q18 and Q19.
- `07:1211` restates of decision 31 only that `serve()` is synchronous and runs calls on
  a pool of threads, and no option below edits it.

### Questions for the owner after the runs

Each question gives its options, the recommended one, and why. The ADR is accepted when
Q15 to Q21 are answered and the rows that the answer to Q19 requires are recorded.

**Q15. What does `serve()` log when the TLS of `grpcio` refuses a dialler?** Decision
21, fourth bullet: "the refusing side logs the fingerprint it saw". R1-LOG: in the form
as written every dialler that `serve()` refuses is refused inside the TLS of the
library, which logs a reason and no fingerprint.
- (a) The clause is revised for `serve()`. A side logs the fingerprint it saw where its
  own comparison makes the refusal: the kernel side and `sdk/remotehost` always, and
  `serve()` in its comparison of decision 21. Where the TLS of `grpcio` refuses the
  dialler first, the line of the library is the record: it holds the reason and no
  fingerprint. The refusal itself is unchanged. It edits the fourth bullet of decision
  21 and the case of decision 37 that reads that log, and no line of
  `docs/architecture/`.
- (b) The kernel's certificate takes the fallback form of decision 20, so that a
  certificate signed by the kernel's key reaches the comparison of `serve()` and its
  fingerprint is logged (R1-FB). "No key of the site can mint an identity" then holds by
  the comparison and no longer by the form, which changes the words of ADR-063 decision
  55. Another self-signed certificate, no certificate and plaintext are still refused
  inside TLS with no fingerprint.
- (c) A further run looks for an interface of `grpcio` that hands a refused certificate
  to Python, before acceptance. None was tested, and none is known to exist.
- Recommended: (a). The form as written holds and every refusal is made; only the log
  of one side differs. (b) weakens the form for one of four cases, and (c) delays the
  ADR for a log line.
- **Answered 2026-11-28: (a)**, decision 54.

**Q16. Does `serve()` install a server interceptor?** Decision 31, fourth bullet: "The
transport makes that refusal before any code of the SDK runs". R2-PRE: on `grpcio`
1.85.4 an interceptor runs before the limit is tested, on the one thread that accepts
every call.
- (a) `serve()` installs no server interceptor. The comparison of decision 21 and the
  bounds of decision 19 are made in the handlers that the SDK generates, as run 1 made
  the comparison in the `Hello` handler. The sentence then holds as written, and the
  bullet gains that condition.
- (b) `serve()` may install interceptors, and the sentence is revised: for a call that
  is then refused, the interceptors of the SDK run first, on the thread that accepts
  every call, and must not block. The code and the empty trailers stay as written.
- Recommended: (a). It keeps the sentence that the owner accepted and keeps code of the
  SDK off the one accepting thread. No message changes under either option.
- **Answered 2026-11-28: (a)**, decision 55.

**Q17. How are the sizes of decision 31 held?** Decision 31, third bullet: `serve()`
serves N calls at once, and the pool and the transport's limit hold two more, for the
`Session` and for a reconnect that overlaps it. R2-SIZE: the limit is one counter, so
with one session open N+1 calls are served, and a reconnecting session, with its
`Hello`, is then refused with `RESOURCE_EXHAUSTED`.
- (a) `serve()` counts the calls itself. The pool and the transport's limit stay at N+2,
  and `serve()` refuses the call that would be number N+1 with `RESOURCE_EXHAUSTED` and
  no `aiops-reason`, so the kernel still acts on the code alone (decision 10). The two
  units more are then kept for sessions. That refusal is code of the SDK, where the
  fourth bullet has the transport make it; with Q16 (a) the count is kept in the
  generated handlers. Whether the count keeps the two units free under load was not
  run, and is run before group 4 merges.
- (b) The text says what the transport does. The N+2 units are shared by sessions and
  calls: with one session open the instance serves N+1 calls, and a reconnect that
  finds the instance full is refused and returns inside the reconnect budget of
  decision 18. `serve()` gains no code.
- Recommended: (a). It gives what the bullet states, N calls and room for the reconnect.
  R2-CANCEL shows why the room matters: a cancelled call keeps its unit until its method
  returns, so after a lost session a full instance would refuse the session that
  replaces it for as long as its methods run. No message changes under either option.
- **Answered 2026-11-28: (a)**, decision 56.

**Q18. Which modes does the directory of an instance have?** Two answers of 2026-11-27
differ. Decision 44 (Q5 (a)): the directory of mode 0750, its files of mode 0640, and
the instance with the site user's group as its only group. ADR-047 decision 43 (its Q9
(a)): the directory of mode 0755, its files of mode 0644, and the unit's user
`65532:65532`, with no group of the host. Each ADR has the other take its answer. Run 3
holds under the first on rootful Docker, and row 10 of ADR-047 decision 39 holds under
the second on the same runtime.
- (a) ADR-047 decision 43 holds. Decisions 24 and 44 are revised to it. The key is
  readable by every user by its mode, and on the host the home and the three
  directories of mode 0700 above it are the guard, which `aiopsctl` checks and sets at
  every install of remote code and every `plugin start`. Third-party code holds no
  group of the host. The cost is the one that ADR-047 decision 43 states: a copy that
  keeps the modes and lands outside a closed directory exposes the key.
- (b) Decision 44 holds. ADR-047 decision 43, the note at its decision 14 and the user
  of its decision 30 are revised: the second number of the unit's user is the site
  user's group. The key is readable by one group. By the text of ADR-047 question Q9 it
  fails under a rootless runtime, which was not run (R3-PODMAN), and the instance holds
  the site user's group on the host for as long as it runs.
- Recommended: (a). The modes of the files of the home are ADR-047's (its decisions 13
  and 14), and ADR-063 decision 34 gives that part the credentials as rows of the home
  with the one directory a unit binds. Its option needs nothing of the user mapping and
  gives third-party code no group of the host. Either option was run on rootful Docker
  alone.
- **Answered 2026-11-28: (a)**, decision 57.

**Q19. The two rows of run 3 that were not run.** Decision 53 has the modes run "under
each user mapping" before acceptance. R3-PODMAN and R3-SELINUX were not run: the host
has neither. With Q18 (a) the question of the group falls away, and what is not run is
whether the unit's user reads the bound directory under a rootless runtime and under a
label of SELinux.
- (a) Both rows are run before acceptance, on a host that has rootless Podman and on
  one that enforces SELinux, which the owner provides. Decision 53 stands as answered.
- (b) The ADR is accepted on the mapping that was run. At S2b a unit is supported on a
  rootful runtime with a given user alone; `plugin start` refuses a runtime that
  reports a rootless mode or a remapped user namespace, and the two rows are run before
  the step that lifts that refusal. This narrows ADR-047 decision 14 for the units of
  remote code and changes decision 53, so it needs the owner's word (ADR-063 decision
  33).
- Recommended: (a). It is what the owner answered in Q14, and the ADR waits for Q15 to
  Q18 in any case. (b) is the way if no such host can be had.
- **Answered 2026-11-28: (a), in a throwaway virtual machine and on no host of the
  owner**, decision 58.

**Q20. What does `UpdateConfig` carry?** This is not a result of a run. Decisions 8, 16
and 31 have `UpdateConfig` carry what the loader's event carries: the committed config
with every reference left out and the changed leaves as `paths`, which the instance
merges. The owner's answer on the S2b part of ADR-034 (its decision 21, 2026-11-27) has
the proxy hold the committed config through a hand-off, with each reference resolved
again at that commit, and forward it whole; the proxy does not hear the loader's event.
The two cannot both be built as written.
- (a) ADR-034 decision 21 holds. `UpdateConfig` carries the committed config whole, in
  the form in which `Activate` carries the config, and the instance replaces the config
  of its activation with it. Decisions 8, 16 and 31 are revised: the field `paths` and
  the merge at `paths` leave, or `paths` stays as a hint that the revision words. The
  dated note at ADR-034 G13 point 10 on `UpdateConfig` stands.
- (b) Decisions 8, 16 and 31 hold as written, and the proxy listens to the loader's
  event. That reverses ADR-034 decision 21, which the owner gave on the same date, and
  ADR-034 is asked again.
- Recommended: (a). ADR-034 decision 21 answers a question that set both ways out, and
  this ADR put the payload to the owner in no question. Blocks: the update message, in
  group 2.
- **Answered 2026-11-28: (a)**, decision 59.

**Q21. Which package holds the Unix dial of the remote protocol?** This is not a result
of a run. Two texts whose questions the owner answered on 2026-11-27 differ, and no
record of that date named the difference. Decision 28 has the kernel-side package of
`sdk/remote` export two dial functions, the TLS dial and the Unix dial, and has the
check runner in the operations plugin call the Unix dial. Decision 45 (Q6 (a)) gives
`aiops/operations` a named entry for that dial on the list of ADR-062 decision 14; the
note of 2026-11-27 at ADR-062 decision 14, the T0 row of `07` §8.1 (`07:1405`) and
harness:58 were edited to that answer and name the entry. ADR-044 decision 45 (its Q19
(a)) says: "The kernel side of the protocol holds no Unix dial". There the Unix dial is
code of `internal/remotesuite` alone, which the import check and the builder refuse in
every server graph, the kernel-side package serves a connection that its caller made
(ADR-044 decision 32), and the suite binary alone dials the socket (ADR-044 decision
33). The note of 2026-11-27 at ADR-063 decision 9 follows that answer. Question Q19 of
ADR-044 says that this ADR "is worded to this answer". Decision 28 is not, and question
Q6 asked how the proxy dials under the dial rule, not which package holds the Unix
dial. The two cannot both be built as written.
- (a) ADR-044 decision 45 holds. Decision 28 is revised: the kernel-side package
  exports the TLS dial and no Unix dial, and the check runner brings its own connection
  from `internal/remotesuite`. That package is no plugin, and by ADR-044 decision 33
  the operations plugin makes no dial of the protocol, so the second named entry of
  decision 45, `aiops/operations` for the Unix dial, leaves; `aiops/remote` keeps its
  entry for the TLS dial. The words on that second entry leave the note at ADR-062
  decision 14, the T0 row of `07` §8.1 and harness:58, which then name two entries
  where they name three. Decision 27 and the first row of its table stand: one Unix
  socket, opened by the check runner alone.
- (b) Decisions 28 and 45 hold as written. ADR-044 decision 45 is reversed to option
  (b) of its question Q19: the kernel-side package holds the Unix dial, every server
  that links the proxy links it, and what keeps a row from it is that the proxy never
  calls it, held by a check over call sites. The fence of ADR-044 decision 32 and the
  note at ADR-063 decision 9 are revised, and the S2b part of ADR-044, accepted on
  2026-11-27, is asked again. The note at ADR-062 decision 14, the T0 row of `07` §8.1
  and harness:58 stand.
- Recommended: (a). Question Q19 of ADR-044 set both places out and the owner chose
  one; this ADR put the place of the Unix dial to the owner in no question. With (a)
  ADR-063 decision 23 holds by what a server links and not by what its code leaves
  uncalled. Blocks: the dial functions of the kernel side of the protocol, in group 2,
  and the dial of the suite binary.
- **Answered 2026-11-28: (a)**, decision 60.

**Also open, and no question of this ADR.** Decision 27 gives `plugin verify` the Unix
socket with no network, and the owner's answer to question Q15 of the S2b part of
ADR-061 (its decision 65) gives it mutual TLS as a unit. ADR-061 puts that difference to
the owner (its question Q17), and the row of decision 27 follows the answer. The form of
the command that renews the kernel's pair (decision 43) is in no text yet: the S2b part
of ADR-061 names it as an addition that blocks group 3. The endpoint table of ADR-047
decision 29 holds no attempt value and does not name `plugin enable` as a writer, which
decision 42 needs of it; the revision of that part takes both.

Note of 2026-11-28: the owner answered questions Q17 and Q18 of the S2b part of ADR-061
on that date. The section "Owner answers of 2026-11-28" says where the row of decision
27 and the two other points of this paragraph stand.

### Edits made with this record, and left

Made: the status of this ADR; a note at decision 38 that points here; and this section.
The row of this ADR in the ADR table of `08` §3 says that the answers and the runs are
recorded and that the ADR waits for the owner, and its row in `00` §9 stays as it is
(decision 53). No other line is edited: an option that the owner has not chosen edits
nothing.

Added on 2026-11-27, after the records of that date were read against each other:
question Q21, the range of the questions where the status and this section name it, and
a dated note at ADR-044 decision 45 that points to the question. No line of decision 28
or 45, of ADR-062 decision 14, of `07` or of the harness record is edited for it.
Added with it: the dated note at decision 29 on the three kinds of definition that get
a generated module, which the record of the owner's answers on the S2b part of ADR-048
left to this ADR.

Left, each to the commit that records the owner's answers to Q15 to Q21: the revised
text of decisions 8, 16, 21, 24, 28, 31, 44 and 45 as the answers have it; the note at
ADR-062 decision 14, the T0 row of `07` §8.1 and harness:58, as the answer to Q21 has
them; `07:1211`, which restates decisions 25, 26 and 31, read again against the
revision; and every line that the section "Owner answers of 2026-11-27" leaves to the
commit that sets this ADR accepted.

## Owner answers of 2026-11-28

The project owner answered questions Q15 to Q21 of the section "Questions for the owner
after the runs" on 2026-11-28, each with its recommended option, and Q19 with the place
of its runs named (decision 58). Each decision below names the question it answers.
Text of 2026-11-27 is not rewritten: a decision that an answer revises keeps its words
and gains a note of 2026-11-28 that names the decision here, and where the two differ
the decision here holds. The answers do not accept this ADR. It is accepted after the
two rows of decision 58 are recorded and this record has been read against the records
of the same date on the S2b parts of ADR-047 and ADR-061, and until then its status is
"answers recorded".

54. **A side logs the fingerprint it saw where its own comparison makes the refusal**
    (Q15 (a)). The clause "the refusing side logs the fingerprint it saw" of decision
    21 is revised for `serve()`. The kernel side and `sdk/remotehost` log the
    fingerprint always, and `serve()` logs it in its comparison of decision 21. Where
    the TLS of `grpcio` refuses the dialler first, the line of the library is the
    record: it holds the reason and no fingerprint (R1-LOG). The refusal itself is
    unchanged, and so are the certificate form of decision 20 and the two log lines of
    a successful connection. The clause stands in the third bullet of decision 21,
    which the record of the runs and question Q15 cite as the fourth; that bullet
    gains the note. The case of decision 37 that reads the log of a refusal is read
    with this decision, by the note at decision 37. No line of `docs/architecture/` is
    edited.

55. **`serve()` installs no server interceptor** (Q16 (a)). The comparison of decision
    21 and the bounds of decision 19 are made in the handlers that the SDK generates,
    as the first run made the comparison in the `Hello` handler. The sentence "The
    transport makes that refusal before any code of the SDK runs" of decision 31 then
    holds as written, and its bullet gains the condition by a note. No code of the SDK
    runs on the one thread that accepts every call (R2-PRE). No message changes.

56. **`serve()` counts the calls itself** (Q17 (a)). The pool and the transport's limit
    stay at N+2, where N is the number of calls that `serve()` serves at once, 32
    unless it is given another. `serve()` refuses the call that would be number N+1
    with `RESOURCE_EXHAUSTED` and no `aiops-reason`, so the kernel still acts on the
    code alone (decision 10). The two units more are then kept for sessions: the
    `Session` stream and a reconnect that overlaps it. That refusal is code of the
    SDK, where the fourth bullet of decision 31 has the transport make it, and by
    decision 55 the count is kept in the generated handlers. Whether the count keeps
    the two units free under load was not run; it is run before group 4 merges. The
    third and the fourth bullet of decision 31 gain a note. No message changes. Note of
    2026-11-28, after the runs of that date: the count was run under load. It holds N
    calls, and the two units are kept against calls that run. They are not kept
    against calls that are being refused (row L-BURST-RECONNECT of the section "Runs
    after the answers of 2026-11-28"), so the sentence on the two units is question Q22
    of that section, and this decision is not revised until the owner answers.

57. **The directory of an instance has the modes of ADR-047 decision 43** (Q18 (a),
    which is also question Q15 of the S2b part of ADR-047). ADR-047 decision 43 holds,
    and decisions 24 and 44 are revised to it.
    - `remote/instances/<plugin id>/` is of mode 0755 and its three files are of mode
      0644. The unit's user is the pair `65532:65532` of ADR-047 decision 30. Its
      second number is not the group of the site user, and third-party code holds no
      group of the host.
    - The key is readable by every user by its mode. On the host the guard is the home
      and the three directories of mode 0700 above the directory of the instance,
      `remote/`, `remote/instances/` and `remote/instances/<owner>/`, which `aiopsctl`
      checks and sets at every install of remote code and every `plugin start`
      (ADR-047 decision 30).
    - The cost is the one that ADR-047 decision 43 states: a copy that keeps the modes
      and lands outside a closed directory exposes the key.
    - Where this ADR names the modes of decision 24, in decisions 23, 37 and 38, in the
      row of ADR-047 in decision 39 and in the section "Edits on acceptance", it means
      these modes. The consequence "one directory whose mode is not 0700 and three
      files whose mode is not 0600" stands.
    - The third run of decision 38 stands as the record of the modes it ran, 0750 and
      0640 through the group. Row 10 of ADR-047 decision 39 is the run of the modes
      that hold, on rootful Docker.
    - No line of ADR-047 is edited with this record. The mark at its question Q15 is
      left to the record of the owner's answers of that date on its S2b part.
    - Note of 2026-11-28, after the runs of that date: the modes and the guard hold
      under rootless Podman and under SELinux enforcing (decision 58). The words
      "third-party code holds no group of the host" do not hold under rootless Podman
      with `--userns=keep-id`, where the runtime adds the site user's group to the
      instance. They are question Q23 of the section "Runs after the answers of
      2026-11-28", and they stand as written until the owner answers.

58. **Both rows of the third run that were not run are run before acceptance** (Q19
    (a)). Decision 53 stands as answered. R3-PODMAN, rootless Podman with
    `--userns=keep-id`, and R3-SELINUX, the binds under SELinux labels, are run as
    throwaway runs that commit nothing, and the versions, the command and the result
    of each are recorded in this ADR by a dated note before it is accepted.
    - Option (a) had the owner provide a host for each row. The owner provides none.
      The rows are run in a throwaway virtual machine, a Fedora cloud image with
      SELinux enforcing and rootless Podman, which is built for the run on the local
      machine and deleted after it.
    - With decision 57 the rows no longer ask whether a group reaches the files. They
      show whether the unit's user, `65532:65532`, reads the bound directory of modes
      0755 and 0644 under a rootless runtime that keeps the user, and under a label of
      SELinux.
    - A result that changes a decision comes back to the owner (decision 53). No
      result is recorded with these answers.
    - Note of 2026-11-28, after the runs of that date: the rows are recorded in the
      section "Runs after the answers of 2026-11-28", and both hold. Two results of
      the same runs come back to the owner, as questions Q22 and Q23 of that section.

59. **`UpdateConfig` carries the committed config whole** (Q20 (a)). ADR-034 decision
    21 holds. The proxy holds the committed config through the hand-off of that
    decision, with each reference resolved again at that commit, and forwards it whole;
    it does not hear the loader's event. `UpdateConfig` carries it in the form in which
    `Activate` carries the config, and the instance replaces the config of its
    activation with it. Decisions 8, 16 and 31 are revised, each by a note.
    - Decision 8: `UpdateConfig` has the fields `activation` 1 and `config` 2. Option
      (a) left to the revision whether the field `paths` leaves or stays as a hint. It
      leaves: the hand-off gives the proxy the committed config and no list of changed
      leaves (ADR-034, G13 points 13 and 16), so the proxy would compute a hint that
      nothing reads. Note of 2026-11-28: the option left this choice to the revision,
      and the record made it. It is put to the owner as question Q24 of the section
      "Runs after the answers of 2026-11-28"; until the answer the field leaves.
    - Decision 16: the message no longer carries what the loader's event carries, and
      the instance merges nothing. The bound of 2 s stands, and so does what follows a
      `ConfigUpdated` with an error or none inside it: the kernel sends `Bye`, the
      proxy dials again, and the `Activate` of the new activation carries the committed
      config.
    - Decision 31: `serve()` replaces the config of the activation and calls
      `config_updated(config)` of each live contribution object, in the order of their
      registration; the method has no argument `paths`. `sdk/remotehost` commits the
      forwarded config, whole, on the fiber of the activation, which applies it through
      the hand-off of the plugin's own `Define` (ADR-034, G13 point 16).
    - The dated note at ADR-034 G13 point 10 on `UpdateConfig` stands. The item of
      ADR-034 that left this point to question Q20 gains a note that names this
      decision. `07` names `UpdateConfig` and no field of it, so no line of
      `docs/architecture/` is edited.

60. **The kernel side of the protocol exports the TLS dial and no Unix dial** (Q21
    (a)). ADR-044 decision 45 holds, and decision 28 is revised to it.
    - The package of the module `sdk/remote` exports one dial function, the TLS dial,
      and serves a connection that its caller made (ADR-044 decision 32). The check
      runner brings its own connection from `internal/remotesuite`, which alone holds
      the Unix dial and which the import check and the builder refuse in every server
      graph (ADR-044 decisions 33 and 45).
    - That package is no plugin, and the operations plugin makes no dial of the
      protocol. So the second named entry of decision 45, `aiops/operations` for the
      Unix dial, leaves, and `aiops/remote` keeps its entry for the TLS dial.
    - The dial rule then has two named entries where the answer of 2026-11-27 gave it
      three: the `net.DialUnix` of the operations plugin for the reload socket
      (ADR-062 decision 14), and the TLS dial of `aiops/remote`. ADR-062 decision 14
      gains a note of 2026-11-28, and the T0 row of `07` §8.1 (`07:1405`) and
      harness:58 name the two entries.
    - Decision 27 and the first row of its table stand: one Unix socket, opened by the
      check runner alone.
    - ADR-044 decision 45, whose note of 2026-11-27 put the difference to the owner,
      gains a note with the answer.

**Edits made with this record.** In this ADR: the status; the mark at each of Q15 to
Q21; the notes of 2026-11-28 at decisions 8, 16, 21, 24, 28, 31, 37, 38, 44 and 45, at
the head of the section "Owner answers of 2026-11-27" and after the paragraph "Also
open"; and this section. In other documents: the dated note at ADR-062 decision 14; the
T0 row of `07` §8.1, reworded in place with no line added or removed; harness:58,
amended and marked; the dated note at ADR-044 decision 45; and the note in the item of
ADR-034 that names question Q20 (decision 59). Two lines that state where this ADR
stands are brought to this record: its row in the ADR table of `08` §3, and the
sentence on it in the list of amendments at the head of the plugin record.

**The row of `plugin verify` in decision 27 is not revised with this record.** The
owner answered question Q17 of the S2b part of ADR-061 on 2026-11-28 with (a): its
decision 65 holds, and `plugin verify` reaches the container as a unit over mutual TLS.
Decision 27 here, ADR-044 decision 38 and the lines of ADR-020 that restate them are
then revised, and this ADR gains the dial of the check runner with a throwaway pair.
The answer has a condition, which is run first: that a process on the host reaches a
container on an internal network. If the run shows that the answer cannot be built as
worded, the point comes back to the owner and nothing is revised. Until that run the
row of decision 27, its case in decision 37 and the row of ADR-020 in decision 39
stand as text of their date.

Note of 2026-11-28, after the run of that condition: it is recorded in the S2b part of
ADR-061. On the network of ADR-047 decision 32 a process on the host does not reach the
container, so the answer cannot be built as worded. The point is back with the owner as
question Q24 of that part, and nothing is revised: the row of decision 27, its case in
decision 37 and the row of ADR-020 in decision 39 stand.

Left to later commits, each named:

- the record of R3-PODMAN and R3-SELINUX, by a dated note in this ADR (decision 58);
- the revision of decision 27 for `plugin verify`, after the run of its condition, or
  the point back with the owner (the paragraph above);
- the commit that sets this ADR accepted, after both: the status, and every line that
  the section "Owner answers of 2026-11-27" leaves to it;
- the run of the count of decision 56 under load, before group 4 merges;
- the form of the command that renews the kernel's pair (decision 43), which is still
  in no text: an addition to the S2b part of ADR-061, which blocks group 3;
- the attempt value of a line of the endpoint table and `plugin enable` as its writer
  (decision 42), with the revision of ADR-047 decision 29. By the owner's answer of
  2026-11-28 to question Q18 of the S2b part of ADR-061, that decision also carries the
  rule on when `sidecars-static` reads the table again.

Not edited. The row of `serve()` in `07` §6 (`07:1211`) restates of decision 31 only
that `serve()` is synchronous and runs calls on a pool of threads, and stands. `00:412`
and `00:413` state no mode and no group, and stand. The note of 2026-11-27 at ADR-063
decision 9 follows ADR-044 decision 45, and stands. Questions Q5 and Q6 and decisions
44 and 45 keep the text of 2026-11-27, with the notes above. The lines of the S2b parts
of ADR-047 and ADR-061 that cite this ADR as not accepted stand: it is not accepted. An
option that the owner did not choose edits nothing.

Note of 2026-11-28, after the runs of that date: the section "Runs after the answers of
2026-11-28" holds the record of R3-PODMAN and R3-SELINUX and the run of the count of
decision 56 under load, the first and the fourth item of the list above. The second
item came back to the owner: the condition of ADR-061 decision 67 is not met on the
network of a unit, and the row of decision 27 is not revised. The ADR is not accepted
by that section, which says what it waits for.

## Runs after the answers of 2026-11-28

Note of 2026-11-28. Two throwaway runs were made on that date, after the owner's answers
of the same date, on the tree of commit `84c7deb`. They committed nothing, and every
machine, container, network, key and file that they made was removed. The first is the
record that decision 58 asks for: the two rows of the third run of decision 38 that
were not run, in a throwaway virtual machine. The second is the run that decision 56
leaves open: the count of `serve()` under load. A row "holds" when the run showed what
this ADR writes, "fails" when it showed otherwise, and nothing is inferred for what was
not run. The rows of the first run that bear on the systemd unit and on the network of
an instance are ADR-047's, and its S2b part records them in the section of the same
name. The run of the condition of ADR-061 decision 67 is recorded in that ADR.

**The ADR is not accepted by this record.** Both rows of decision 58 are recorded and
hold: under the modes of decision 57 the user of the unit reads the bound directory and
nothing else under rootless Podman, with the default mapping and with the user kept,
and under SELinux enforcing once the bind carries a label. The count of decision 56
holds N calls. Four points keep the ADR open, and each is the owner's (decision 53):

- the sentence of decision 56, "The two units more are then kept for sessions", does
  not hold against calls that are being refused (row L-BURST-RECONNECT): question Q22;
- the words "no group of the host" of decision 57 do not hold under rootless Podman
  with the user kept (rows R3-PODMAN-43 and R3-PODMAN-TARGETS): question Q23, which is
  also question Q22 of the S2b part of ADR-047;
- the field `paths` of `UpdateConfig`, which option (a) of question Q20 left to the
  revision and which decision 59 has leave: question Q24;
- the row of `plugin verify` in decision 27. The condition of ADR-061 decision 67 was
  run and is not met on the network of a unit, so the point is back with the owner as
  question Q24 of the S2b part of ADR-061, and nothing is revised here.

### The machine, the versions and the stand-ins

- **The virtual machine of decision 58.** Fedora Cloud Base 45, the image
  `Fedora-Cloud-Base-Generic-45-1.4.x86_64.qcow2` with the SHA-256
  `50bdc5e70f6a24f540932d15411c1c6ae9fcfa40fb990d876d83c5cc823b2ad0`, under QEMU 10.2.1
  with KVM on the local machine: user-mode networking with one port forwarded for SSH
  on a loopback address, a cloud-init seed image and no libvirt. The checksum file of
  the release carries a good signature of the Fedora 45 key
  `4F50A6114CD5C6976A7F1179655A4B02F577861E`, and the image hashes to the value in it;
  the bytes came from a mirror of the Fedora mirror list. The machine was powered off
  after the runs, and its disk, its seed image and the downloaded image were deleted.
  No host of the owner and no remote host was used, and nothing was created on a
  container runtime of the local machine.
- **In the machine,** as the image ships it and with no package update: kernel
  7.2.11-300.fc45, systemd 262-3, `selinux-policy-targeted` 45.23-1, `container-selinux`
  2.251.0-1, and Podman 6.1.3 with crun 1.29.1, netavark 2.1.0, aardvark-dns 2.1.0 and
  conmon 2.2.1. `getenforce` answered `Enforcing` with the policy `targeted`, and
  `podman info` reported a rootless runtime with SELinux enabled. Added from Docker's
  package repository for Fedora 45, whose key is
  `060A61C51B558A7F742B77AAC52FEB6B621E9F35`: Docker Engine and client 29.10.0,
  containerd 2.4.2, runc 1.5.3, the Compose plugin 5.8.1 and the build plugin 0.39.1.
  Every container ran the image
  `python@sha256:191083cdf6c7bda8c76158983163ead5d5cf7b77cf3033e805bb0f5296f51b4b`,
  pulled by digest in the machine.
- **Users and homes.** The machine has two unprivileged users: the site user,
  `1000:1000`, whose home directory is of mode 0700 and whose subordinate ids start at
  524288, and another local user, `1001:1001`. The throwaway home, `<home>`, is under
  the site user's home directory and has the SELinux type `user_home_t`. A second copy,
  `<second home>`, is under a parent of mode 0755 outside every home directory and has
  the type `var_t`. The home, `remote/`, `remote/instances/` and
  `remote/instances/<owner>/` are of mode 0700 and owned by the site user. The three
  credential files are PEM files made with `openssl` in the machine. They were read
  and not used: no TLS server loaded the key in these rows.
- **The load run** was made on the local machine: Ubuntu 26.04.1 on Linux 7.0.0-44,
  with Docker Engine 29.10.1, rootful, where the records of 2026-11-27 ran 29.10.0,
  containerd v2.4.2 and runc 1.5.3, under AppArmor and without SELinux. `grpcio`
  1.85.4, `protobuf` 7.36.2 and `typing-extensions` 4.16.0 were installed with `pip
  --require-hashes` against the three SHA-256 values that ADR-063 records, on CPython
  3.12.16 of the same image. The client is built with Go `go1.26.8`,
  `google.golang.org/grpc` v1.82.1 and `google.golang.org/protobuf` v1.36.11, at the
  hashes that ADR-063 records.
- **Stand-ins,** in no commit. `modes.py` is the program of the third run. `count.py`
  is a `grpcio` server of the synchronous interface, sized as decision 56 has it: a
  pool and `maximum_concurrent_rpcs` of N+2, no server interceptor (decision 55), and
  handlers written by hand in the shape of generated handlers on the public interface
  of `grpcio`. The count is a `threading.BoundedSemaphore(N)`. The wrapper of each
  handler takes one unit without blocking as its first statement, on the thread of the
  pool; where it gets none it aborts with `RESOURCE_EXHAUSTED` and no trailing
  metadata; and it gives the unit back in the `finally` of a `try` around the call of
  the plugin's method. `load` is a Go client on `grpc-go`. The generated module of the
  SDK does not exist yet, and no template renders a unit yet.

### Run 3, the two rows that were not run (decision 58)

`<id>` is the plugin id of the throwaway home, `<dir>` is `<home>/remote/instances/<id>`,
and `<image>` is the image above by its digest. Unless a row says otherwise, a container
ran with `--network none --cap-drop ALL --security-opt no-new-privileges`, and `<run>`
stands for those flags. Rows whose name holds `PODMAN` are the row R3-PODMAN of the
third run, and rows whose name holds `SELINUX` the row R3-SELINUX.

| Row | What was run | Versions | Command | Result | Observed |
|---|---|---|---|---|---|
| R3-PODMAN-43 | Rootless Podman as the site user, under the modes of decision 57, the directory 0755 and its files 0644, with five mappings: (1) no `--user`; (2) `--user 65532:65532`, the user of the template; (3) `--userns=keep-id`; (4) `--userns=keep-id --user 65532:65532`; (5) `--userns=keep-id:uid=65532,gid=65532`. Whether the instance process reads its three files and nothing else, which owner and mode the files show inside and outside, and which user the host sees. Each case ran with no label option, with `:z` and with `:Z` (row R3-SELINUX-ROOTLESS) | Podman 6.1.3 rootless, crun 1.29.1, SELinux enforcing | `podman run --rm -i <run> <mapping> -v <dir>:/aiops/identity:ro,Z <image> python -I - <home> <home>/remote/kernel/key.pem <dir>/key.pem < modes.py`. For the host's view the same with `-d` and a sleep, then `Uid`, `Gid` and `Groups` of `/proc/<pid>/status`, and `/proc/<pid>/uid_map` | holds | Outside: `1000:1000`, the directory 0755 and the files 0644. With a label every mapping listed the directory and read `key.pem`, `cert.pem` and `kernel.pem`; creating a file failed with `Read-only file system`; the home, `remote/kernel/key.pem` and the host path of its own key failed with `No such file or directory`. (1) Inside `0:0`, the files shown as `0:0`; the host sees `1000:1000`, the site user; opening the key for writing failed with `Read-only file system`, so the read-only bind alone stops the write. (2) Inside `65532:65532` with the one group 65532, the files shown as `0:0`; the host sees the user and the group 589819, a subordinate id of the site user, no effective capability and `NoNewPrivs` 1; a write to the key failed with `Permission denied`. (3) Inside and outside `1000:1000`, the site user. (4) Inside `65532:65532` with the groups 1000 and 65532, the files shown as `1000:1000`; the host sees the user and the group 589819 and the groups 1000 and 589819; a write failed with `Permission denied`. Under `keep-id` Podman adds the site user's group as a supplementary group for every `--user` that was tried, `12345:12345`, `--passwd=false` and `keep-id:uid=2000,gid=2000` among them. (5) Inside `65532:65532`, the files shown as `65532:65532`, and the host sees `1000:1000`, the site user. So the user of the template reads its files and nothing else under the default rootless mapping and under `keep-id`. Mappings 1, 3 and 5 are the site user on the host, which ADR-047 decision 30 has `plugin start` refuse; for mapping 5 only the read from the host shows it, since the inside shows 65532 |
| R3-PODMAN-44 | For the record, the same under the modes that decisions 24 and 44 had before decision 57: the directory 0750, its files 0640, the group of the site user | Podman 6.1.3 rootless, SELinux enforcing | `chmod` of the directory to 0750 and of the files to 0640, then the command of R3-PODMAN-43 with `<mapping>` one of `--user 65532:65532`, `--user 65532:1000`, `--user 65532:0`, `--userns=keep-id --user 65532:65532`, `--userns=keep-id --user 65532:1000` and `--userns=keep-id --user 12345:1000` | fails | Under the default rootless mapping the number of the site user's group does not reach the files: `--user 65532:65532` and `--user 65532:1000` got `Permission denied` on the directory and on each file, since group 1000 inside is a subordinate group outside. `--user 65532:0` read all three: group 0 inside is group 1000 outside, and the host saw the user 589819 with the group 1000. Under `keep-id`, `--user 65532:1000` and `--user 12345:1000` read the files, and so did `--user 65532:65532`, through the supplementary group 1000 that Podman adds. So no one number for the group gives those modes on rootful, on default rootless and under `keep-id` together: it is the site group's number on the first and the last, and 0 on default rootless. This is what ADR-047 question Q9 said of its option (b), now run, and it agrees with decision 57 |
| R3-OTHER-USER | Whether another local user reads the credentials, on the second home, under both sets of modes: "the guard is the home and the three directories of mode 0700" (decision 57) | Fedora 45; Podman 6.1.3 rootless for the other user; SELinux enforcing | As the user 1001: `ls` of the parent and of `<second home>`; `cat` of the instance's key and of `remote/kernel/key.pem`. As the user 1001 with its own Podman: `podman run --rm --network none -v <second home>/remote/instances/<id>:/aiops/identity:ro <image> ls /aiops/identity`, and the parent bound whole and read-only with `--security-opt label=disable`, then `cat` of the key through it. Control: `chmod 0755` of the home and of the three directories, then `cat` of the key as the user 1001, under each set of modes | holds | The parent listed. `Permission denied` on the home, on the instance's key and directory through the home and on `remote/kernel/key.pem`, under 0755 with 0644 and under 0750 with 0640 alike. The other user's Podman failed with `statfs ...: permission denied`; with the parent bound whole and the label disabled it got `Permission denied` on the home and on the key. Control, with the home and the three directories opened to 0755: under 0755 with 0644 the other user read the key, 241 bytes; under 0750 with 0640 it got `Permission denied`. That is the cost that decision 57 states: the mode of the key is no guard |
| R3-OTHER-INSTANCE | Whether another instance reads the credentials: two instances, each with its own directory bound, and two contrasts in which a third container is handed the first instance's directory, which no template renders | Podman 6.1.3 rootless; repeated on rootful Podman 6.1.3, with the same results (row R3-SELINUX-PODMAN) | Instance A: `podman run -d <run> --user 65532:65532 -v <dir>:/aiops/identity:ro,Z <image> sleep 600`; instance B the same with the directory of a second id; then, in B, a read of A's key by its path on the host. Contrasts: a third container with `-v <dir>:/mnt/a:ro` and a read of `/mnt/a/key.pem`, once while the directory holds the private label of `:Z` and once after a bind with `:z` | holds | Each instance read its own key. From B, the host path of A's key: `No such file or directory`. The two processes had the labels `container_t:s0:c308,c966` and `container_t:s0:c67,c607`, and each directory carried `container_file_t` with the category pair of its own instance. Contrast with the private label: `Permission denied` in the third container. The audit log held no line for it; with the `dontaudit` rules off for one run it held `avc: denied { search } ... scontext=system_u:system_r:container_t:s0:c198,c205 tcontext=system_u:object_r:container_file_t:s0:c612,c888 tclass=dir`. Contrast with the shared label of `:z`, `container_file_t:s0`: the third container read the key. So under the modes of decision 57 the mount namespace is what keeps another instance out; a private label adds a second barrier, and a shared label adds none |
| R3-SELINUX-ROOTLESS | The read-only bind of the credential directory under SELinux enforcing, with no label option, with `:z` and with `:Z`, on rootless Podman under each of the five mappings; and the denials that the audit log holds | Podman 6.1.3 rootless, `container-selinux` 2.251.0-1, `selinux-policy-targeted` 45.23-1 | `restorecon -RF <home>` before each case; the command of R3-PODMAN-43 with `-v <dir>:/aiops/identity:ro`, `:ro,z` and `:ro,Z`; then the new `avc:` lines of the audit log, and `stat -c %C` of the directory and of the key. Extras: `--security-opt label=disable` with no label option, and the same bind from the second home | holds | With no label option, under every mapping, the process ran as `container_t` with a category pair of its own; it could `stat` the directory and nothing in it, with `Permission denied` on the listing and on each file, whatever the modes. Three denials a run: `avc: denied { read } for comm="python" name="key.pem" scontext=system_u:system_r:container_t:s0:c154,c660 tcontext=unconfined_u:object_r:user_home_t:s0 tclass=file`, and the same for `cert.pem` and `kernel.pem`; where the process owns the file, mappings 1, 3 and 5, also `{ append }` for the attempt to write. For the second home the target was `system_u:object_r:var_t:s0`. The denied listing left no line, under a `dontaudit` rule. With `:z` all three were read, and the directory and its files then carry `system_u:object_r:container_file_t:s0` on the host, and keep it. With `:Z` all three were read, and they carry `container_file_t` with the category pair of that container, changed at every start. With `label=disable` the process is `spc_t`, reads with no relabel and is not confined. Only the bound directory was relabelled: the home and `remote/kernel/` kept `user_home_t`. This is what ADR-047 decisions 14 and 30 write: the bind needs a label |
| R3-SELINUX-RENDER | What a unit would have to carry so that the instance reads its files without a relabel of the home that lets another instance read them, and what breaks that label | Podman 6.1.3 rootless; repeated on rootful Podman 6.1.3, with the same results (row R3-SELINUX-PODMAN) | (a) `-v <dir>:/aiops/identity:ro,Z`. (b) `--mount type=bind,src=<dir>,dst=/aiops/identity,ro,relabel=private` of Podman. (c) No label option, after `chcon -R -t container_file_t -l s0 <dir>` run by the site user with no root; also with the type `container_ro_file_t`. (d) `chcon -R -t container_file_t -l s0:c100,c200 <dir>` with `--security-opt label=level:s0:c100,c200` and no label option. Against a running instance: a second container with `-v <dir>:/mnt/a:ro,Z`; a stand-in of the kernel container with `-v <home>:<home>:ro,z -v <home>/run:<home>/run:z`, the form of ADR-047 decisions 14 and 15, and the same with `:Z`; and `restorecon -RF <home>` | holds | (a) and (b) read, and relabel the one directory alone, to a label private to that container. (c) read with either type; the site user's `chcon` exited 0; `restorecon -R` left the type in place, and `restorecon -RF` reset it to `user_home_t`. (d) read at the fixed level, and a container of another level got `Permission denied`. What breaks a private label: a second container that binds the same directory with `:Z` takes the label, and the running instance then gets `Permission denied` on its key. A kernel stand-in that binds the home whole with `:z` relabels every file of the home to `container_file_t:s0`, `remote/kernel/key.pem` and every instance's directory among them: the running instance still reads, its private label is gone, and the kernel's key passes SELinux for every container of the host, so the closed directories and the mount namespace are then the only guard. The same bind with `:Z` gives the whole home the category pair of the kernel container, and the running instance gets `Permission denied` on its key until it is started again; started again with `:Z` it reads, and the kernel stand-in gets `Permission denied` on that one directory, which it does not read. `restorecon -RF` of the home while the instance runs: `Permission denied` on the key until the next start |
| R3-SELINUX-FORMS | Whether the bind forms that the runs of ADR-047 decision 39 recorded can carry the label: `docker run --mount type=bind,...,readonly` (its rows 4 and 9), and the long form of Compose with `create_host_path: false` (its row 7) | Docker Engine 29.10.0 with `selinux-enabled`, the Compose plugin 5.8.1, where that record ran 5.8.0; Podman 6.1.3 through its socket with the same Compose | `docker run --rm --mount type=bind,src=<dir>,dst=/aiops/identity,readonly,relabel=private ...`. `docker compose -f <fragment> up -d` with the volume as: (v0) the long form with `bind: {create_host_path: false, selinux: Z}`; (v1) the long form with `bind: {selinux: Z}`; (v2) the short form `<dir>:/aiops/identity:ro,Z`; (v3) v0 without `selinux` and the directory labelled beforehand; (v5) the short form with a source that does not exist. Then `docker compose logs`, `docker inspect` of `HostConfig.Binds` and of the mode of each mount, and `stat -c %C <dir>` | fails | `docker run --mount` refused the option: `unknown option 'relabel' in 'relabel=private'`, and the same for `bind-selinux`. The plain `--mount` form on a daemon that confines: `Permission denied` on the key, with the denial of R3-SELINUX-ROOTLESS. v0: `config -q` and `up -d` exited 0 and the label was not applied, with no message: `Binds` null, the mode of the mount empty, the directory still `user_home_t`, and the instance got `Permission denied`. The same through the socket of Podman. v1 and v2: `Binds` held `<dir>:/aiops/identity:ro,Z`, the directory was relabelled, and the instance read its key as `65532:65532`. v3: read, with `Binds` null as row 7 of ADR-047 decision 39 recorded. v5: `up -d` exited 0 and the missing source was created on the host, inside the home, as `root:root` of mode 0755, which `create_host_path: false` exists to prevent. So on Docker and Compose the label and `create_host_path: false` cannot both be rendered, and the `--mount` form of the systemd unit cannot carry a label at all; the `--mount` of Podman can |
| R3-SELINUX-PODMAN | Rootful Podman under SELinux enforcing: the same bind with no label option, with `:z` and with `:Z`, under both sets of modes | Podman 6.1.3 rootful, crun 1.29.1 | `sudo podman run --rm -i <run> --user 65532:65532 -v <dir>:/aiops/identity:ro <image> python -I - ... < modes.py`, and with `:ro,z` and `:ro,Z`; also with no `--user`, with `--user 1000:1000` and with `--security-opt label=disable`; under 0750 with 0640, with `--user 65532:65532` and `--user 65532:1000` | holds | As the host sees it the first process is `65532:65532` with the one group 65532, an identity `uid_map`, no effective capability, `NoNewPrivs` 1 and a label `container_t` with a category pair. The files are shown as `1000:1000` inside and outside. With no label option: `Permission denied`, with `avc: denied { read } ... scontext=system_u:system_r:container_t:s0:c408,c578 tcontext=unconfined_u:object_r:user_home_t:s0 tclass=file` for each of the three files. With `:z` and with `:Z` the three files were read and the write refused, with the labels of the rootless rows. The runtime made the bind through the three directories of mode 0700. Under 0750 with 0640: `65532:65532` got `Permission denied`, by the modes, and `65532:1000` read with `:Z` and was denied with no label option. The cases of R3-OTHER-INSTANCE and R3-SELINUX-RENDER gave the same results here |
| R3-SELINUX-DOCKER | Rootful Docker under SELinux enforcing: the same bind, on the daemon as its package installs it, and with `selinux-enabled` set in the `daemon.json` of the machine | Docker Engine and client 29.10.0, containerd 2.4.2, runc 1.5.3, the overlay snapshotter of containerd, cgroup v2 | `sudo docker run --rm -i <run> --user 65532:65532 -v <dir>:/aiops/identity:ro <image> python -I - ... < modes.py`, and with `:ro,z` and `:ro,Z`; `docker info --format '{{json .SecurityOptions}}'`; then `{"selinux-enabled": true}` in the `daemon.json` of the machine, a restart of the daemon, and the same runs | holds | As installed, `docker info` lists seccomp and the cgroup namespace and no SELinux. Every container ran as `spc_t`: the files were read with no label option, `:z` and `:Z` changed no label, and no denial was logged. On that daemon the enforcing host does not confine the instance at all. With `selinux-enabled`, `docker info` lists `name=selinux`, and the results are those of Podman: with no label option `Permission denied`, with a denial as in R3-SELINUX-PODMAN; with `:z` and with `:Z` the files were read; `label=disable` gives `spc_t`. The host sees `65532:65532` with an identity `uid_map` under both settings. Under 0750 with 0640, `65532:1000` reads, with `:Z` where the daemon confines, and `65532:65532` does not. The daemon that does not confine accepts the `Z` option and ignores it |
| R3-PODMAN-TARGETS | The row R3-PODMAN on its two targets: a unit of the user's service manager over rootless Podman, in the form of R3-SYSTEMD, and a compose fragment run by `docker compose` against the socket of the site user's rootless Podman | systemd 262, Podman 6.1.3 rootless, the Compose plugin 5.8.1 | `systemd-run --user --wait --collect --pipe --quiet podman run --rm -i <run> --user 65532:65532 -v <dir>:/aiops/identity:ro,Z <image> python -I - <home> < modes.py`, and the same with `--userns=keep-id`. `systemctl --user start podman.socket`, then `docker compose -f <fragment> up -d` with `DOCKER_HOST` naming that socket; the fragment has `user: "65532:65532"`, `cap_drop: [ALL]`, `no-new-privileges`, the one bind and the network of row 7 of ADR-047 decision 39; once more with `userns_mode: keep-id` | holds | The transient unit exited 0 under both mappings and read the three files; under `keep-id` the groups were 1000 and 65532 again. Compose against the socket of Podman created the container and the network. With a label that takes effect, the short form or the long form without `create_host_path: false`, the instance read its key as `65532:65532`, and the host saw the user and the group 589819, with the groups 1000 and 589819 under `userns_mode: keep-id`. The modes hold on both targets. Two things differ from Docker, and both are recorded in the S2b part of ADR-047: the label of the long form with `create_host_path: false` is dropped here too, and the network that Podman made from the fragment reported `internal` with a gateway address and its resolver on, so the two gateway-mode options of Docker change nothing on Podman. Not run: a unit of the system's service manager over Podman, since the template names the program `docker` |

### The count of decision 56 under load

The server of every row: `docker run -d --network <network of the run> --user 12345:12345
--cap-drop ALL --security-opt no-new-privileges -e N=<N> ... <image> python -I ...
count.py`, on a bridge network made for the run, over gRPC without TLS. The client:
`load -addr <address of the container> -n <N> -burst 10s -tries 200 -w
1@100ms,1@10ms,1,8,64` for N of 32, the default of decision 31, and with `-burst 5s
-tries 100` for N of 4. The widths are the senders of calls that are all refused: one
every 100 ms, one every 10 ms, and 1, 8 and 64 in a tight loop. Every row ran at the
versions of the load run above.

| Row | What was run | Versions | Result | Observed |
|---|---|---|---|---|
| L-SIZE | Decision 56 as worded: N long calls in flight and the session open; call N+1 is refused with `RESOURCE_EXHAUSTED` and no `aiops-reason`, and its method does not run | As above | holds | N of 32: 32 methods running; call 33 got `RESOURCE_EXHAUSTED` with the message `call limit reached` after 1.1 ms; 32 bodies ran and the count made one refusal; the open session answered its ping. The only trailer that `grpc-go` reports is the content type of a reply that is trailers alone, so no `aiops-reason`. N of 4 gave the same for call 5. Control with the count switched off, the transport's limit alone: 33 methods ran with one session open, which is R2-SIZE again |
| L-OVERLAP | A second session that overlaps the first during a reconnect, a new connection with `Hello`, the `Session` stream and one ping, is accepted while N calls are in flight; and what a third dialler gets | As above | holds | N of 32 and of 4: with N methods running the second session was accepted, and both sessions answered a ping. With N calls and two sessions open, N+2 units, the `Hello` of a third dialler and a further call were refused by the transport: `RESOURCE_EXHAUSTED` with `Concurrent RPC limit exceeded!` and no `aiops-reason`. So a refusal has one of two messages, from the count or from the transport, with the same code and no reason, as the note at decision 31 has it. After the methods returned the 32 calls ended `OK`, the next call was `OK` and the count was 0 |
| L-CANCEL | Cancelled calls whose method has not returned keep their unit, and the session that replaces a lost one is still accepted; the same after the old connection is closed outright | As above | holds | N of 32: the client saw `CANCELLED` for all 32 within 290 microseconds; 300 ms later 32 methods were still running, and a new call on the old connection got `RESOURCE_EXHAUSTED` from the count. The replacing session on a new connection was accepted and answered its ping; a call on the new connection was refused, as it must be while the units are held. After the old connection was closed: one session, 32 methods still running, and the new session still answered. After the methods returned a call on the new connection was `OK` and the count was 0. The same at N of 4 |
| L-RAISE | A handler that raises, or that aborts, gives its unit back | As above | holds | N of 32, 320 calls with at most 32 at once, half whose method raises `RuntimeError` and half whose method calls `context.abort`: 160 ended `UNKNOWN` and 160 `FAILED_PRECONDITION`, 320 bodies ran and none was refused. Then 32 blocked calls all ran at once and call 33 was refused, so no unit was lost. The `finally` of the wrapper runs on a return, on an exception of the method and on `context.abort`, which raises. `BoundedSemaphore` raises `ValueError` on a unit given back twice, and none was raised in any run |
| L-STREAM | A streamed call holds its unit for the whole of its stream, also when it is cancelled while its method is blocked, and gives it back when the method raises in the middle of the stream | As above | holds | N of 32: with 31 calls and one open server stream the next call was refused. After the client cancelled the stream the count was still 32 at 300 ms and the next call was still refused; after the methods returned the next call was `OK`. A stream whose method raises after its first item ended `UNKNOWN`, and 32 calls then ran at once. The wrapper of a streamed handler is a generator: the unit is taken at the first item and given back in a `finally` around `yield from`. On a cancellation `grpcio` does not close the generator: the `finally` ran when `grpcio` dropped it after the method yielded, which rests on the reference counting of CPython |
| L-CHURN | The count under a load of short calls: never more than N methods at once, every excess call refused with the code and no reason, the count back at 0, the session alive | As above | holds | N of 4, 16 senders for 5 s, each call a method of 5 ms, with a ping of the session every 10 ms: 3485 `OK` and 26662 `RESOURCE_EXHAUSTED`, 6787 by the count and 19875 by the transport's limit, no other code and none with a reason; at most 4 methods at once; the count 0 at the end; 466 pings, none failed, the slowest 7.2 ms. N of 32, 128 senders for 10 s: 19523 `OK`, 66 refused, at most 32 methods at once, the count 0 at the end, 847 pings and none failed. At N of 32 the Python server was the limit, at about 2000 calls a second, so that run pressed the count far less than the run at N of 4 |
| L-BURST-SESSION | Many refused calls in a burst do not starve the open session: N calls in flight, and 1 to 64 senders of calls that are all refused, on the connection of the session | As above | holds | N of 32, 10 s a burst, at 10, 85, 1967, 6927 and 7454 refused calls a second: 873 to 938 pings a burst on the open session, none failed, the median 0.46 to 0.63 ms and the slowest 11.7 ms. Never more than 32 methods ran, and no refusal carried a reason. An open `Session` stream holds its thread and its unit, so refusals cannot take them |
| L-BURST-RECONNECT | "The two units more are then kept for sessions" (decision 56), for a reconnect, a new connection with `Hello` and `Session`, that arrives while calls beyond N are being refused: 200 reconnects spread over each burst of 10 s, with N of 32 in flight and one session open | As above | fails | A call that the count refuses is first admitted by the transport, and it holds one unit of the transport and one thread of the pool until its handler has aborted: `grpc/_server.py` of the wheel, lines 1271-1287, counts a call up when it is dispatched and down when its task on the pool ends. With one session open there is one spare unit, so the `Hello` or the `Session` of the reconnect was refused by the transport, with `RESOURCE_EXHAUSTED` and `Concurrent RPC limit exceeded!`. N of 32, reconnects refused: 4 of 200 at 10 refused calls a second; 23 of 200 at 85 a second; 187 of 200 with one sender in a tight loop, 1967 a second; 189 of 200 with 8 senders; 182 of 200 with 64. N of 4: none of 100 at 10 a second, 12 of 100 at 81 a second, and 92, 88 and 98 of 100 for the three tight widths. Each refused call holds its unit for about 1.5 ms. With no such calls the reconnect was always accepted (L-OVERLAP, L-CANCEL) |
| L-V1 | A variant, not decision 56 as worded: the pool and the transport's limit at N+2+8, and `serve()` counts sessions too, at most 2. The same bursts | As above | fails | Reconnects accepted: 200 of 200 at 81 refused calls a second, with one tight sender and with 8; with 64 senders the transport's limit refused 7 of 194. Spare units hold only against as many refusals at once as there are spare units. L-SIZE, L-CANCEL and L-CHURN held |
| L-V2 | A variant, not decision 56 as worded: no limit of the transport, a pool of N+3, and `serve()` counts calls, N, and sessions, 2, with `Hello` not counted. The same bursts, then a third session | As above | holds | Reconnects accepted: 199 of 199 at 87 refused calls a second, and 200 of 200 with 1, 8 and 64 tight senders, up to 3743 refused calls a second. The time to an accepted reconnect: a median of 2.1 ms with one sender and 36 ms with 64, the slowest 55 ms in the bursts and one of 179 ms in the paced run. Every excess call was refused by the count and none by the transport, with the code and no reason; no ping of the session failed; never more than 32 methods ran. A third session was refused by the count of sessions, with `RESOURCE_EXHAUSTED` and `session limit reached`. L-SIZE, L-CANCEL and L-CHURN held. Here an excess call waits in the queue of the pool until the one spare thread refuses it, where decision 31 has it refused before any code of the SDK runs. The queue of the pool has no bound, and it was not sized against a flood |
| L-LOCK | Whether the count needs a lock | CPython 3.12.16 of the image, the standard library alone | holds | It needs one. `docker run --rm --network none ... python -I lock.py`, 64 threads of 20000 takes each at N of 4: a test of the count followed by an add, with one call between the two, admitted up to 51 at once and ended at a negative count. The same lines with nothing between them showed no excess on this interpreter, which is a property of where CPython 3.12 switches threads and no guarantee. `threading.BoundedSemaphore(N)` taken without blocking admitted at most 4 and ended at 0, in every run and in every row above |

### What the results touch

- **Decision 58: both rows are recorded, and both hold.** Under the modes of decision
  57 the user of the unit, `65532:65532`, reads its three files and no other path of
  the home under rootless Podman with the default mapping and with `--userns=keep-id`,
  on a transient unit of the user's service manager and through Compose on the socket
  of Podman (R3-PODMAN-43, R3-PODMAN-TARGETS). Under SELinux enforcing it reads them
  once the bind carries a label, on rootless Podman, rootful Podman and a Docker daemon
  that confines, and is denied without one, as ADR-047 decisions 14 and 30 write
  (R3-SELINUX-ROOTLESS, R3-SELINUX-PODMAN, R3-SELINUX-DOCKER). The answers to Q18 and
  Q19 are met for the read.
- **Decisions 24 and 44 as they stood before decision 57** do not hold under the
  default rootless mapping (R3-PODMAN-44). Decision 57 already revises them, and no
  decision changes.
- **Decision 57, the guard,** holds: another local user reads nothing through the home
  and the three closed directories, and the control shows the cost that the decision
  states (R3-OTHER-USER). Another instance is kept out by its mount namespace
  (R3-OTHER-INSTANCE).
- **Decision 57, the words "third-party code holds no group of the host",** and the
  same words in the notes at decisions 24 and 44, do not hold under rootless Podman
  with `--userns=keep-id`: the runtime adds the site user's group to the process of
  the instance, whatever `--user` the unit names (R3-PODMAN-43, mapping 4;
  R3-PODMAN-TARGETS). The read of the credentials is not affected, and in the runs the
  group reached nothing. The cause was not read in the source of Podman. Question Q23.
- **The label of the bind is ADR-047's.** The forms in which the runs of ADR-047
  decision 39 wrote the bind cannot carry a label on Docker and Compose
  (R3-SELINUX-FORMS), and a bind of the whole home with a label option relabels the
  credential directories with it (R3-SELINUX-RENDER). Both touch ADR-047 decisions 14
  and 30 and no decision of this ADR; they are questions Q20 and Q21 of its S2b part.
  The network of a unit on Podman is its question Q23.
- **Decision 56, the count,** holds: `serve()` serves N calls and refuses call N+1 with
  the code and no reason; a cancelled call, a handler that raises or aborts and a
  streamed call keep and give back their unit as decision 31 has it; and refusals do
  not starve an open session (L-SIZE, L-CANCEL, L-RAISE, L-STREAM, L-CHURN,
  L-BURST-SESSION). The room for a reconnect holds against calls that run, cancelled
  or not (L-OVERLAP, L-CANCEL).
- **Decision 56, the sentence "The two units more are then kept for sessions",** does
  not hold as worded with the pool and the transport's limit at N+2
  (L-BURST-RECONNECT). A call that the count refuses holds a unit of the transport
  until its handler has aborted, and a reconnect that arrives then is refused by the
  transport. Question Q22.
- **Readings for the code of `serve()`, and no decision.** The count needs a lock, and
  a semaphore of the standard library taken without blocking is one (L-LOCK). The unit
  of a cancelled streamed call is given back when the interpreter drops the generator,
  which is true of CPython and was run on no other; decision 30 supports CPython 3.12
  alone at S2b.
- **Decision 27, the row of `plugin verify`,** is not revised. The run of the condition
  of ADR-061 decision 67 is recorded in that ADR: on the network of ADR-047 decision 32
  a process on the host does not reach the container, so the answer to question Q17 of
  that part cannot be built as worded, and the point is back with the owner as its
  question Q24. The row, its case in decision 37 and the row of ADR-020 in decision 39
  stand as text of their date until the owner answers.

### Limits of the runs

- One distribution and one machine for the third run: Fedora Cloud Base 45 with the
  packages of the image, the targeted policy, in one guest. No policy of another
  distribution, no MLS policy, no host under AppArmor and no second kernel was run.
  Each case was run once, some twice.
- The rows of the third run read the files with the Python of the image. `grpcio`, the
  Go module of gRPC and the pinned registry were not used in the machine.
- Not run: rootless Docker; Docker with a remapped user namespace; a unit of the
  system's service manager over Podman, the shim that gives Podman the name `docker`,
  and `podman-compose`; a home on a network file system or under another SELinux type
  than `user_home_t` and `var_t`.
- Denials under a `dontaudit` rule left no line in the audit log. One was shown once
  with those rules off, and the rules were restored.
- The Docker daemon of the machine enables no SELinux as its package installs it. The
  results on a daemon that confines come from `selinux-enabled` set in the machine
  alone.
- The load rows ran over gRPC without TLS, from one client process and, for calls, one
  connection. The comparison of decision 21 and the bounds of decision 19 were not
  part of them. Only the churn at N of 4 pressed the count hard.
- L-V1 and L-V2 were run to show what an option gives, and neither is a proposal of
  this record.
- `pip` fetched the three wheels from the public index and checked them with
  `--require-hashes`. The build of the client left what it fetched in the module cache
  and the build cache of the Go toolchain of the local machine.

### Questions for the owner after the runs of 2026-11-28

Each question gives its options, the recommended one, and why. The ADR is accepted when
Q22 to Q24 are answered, the answers are recorded as the records above were, and the
row of `plugin verify` in decision 27 follows the owner's answer to question Q24 of the
S2b part of ADR-061.

**Q22. How is the room for a reconnect kept while calls are being refused?** Decision
56: "The two units more are then kept for sessions". L-BURST-RECONNECT: with the pool
and the transport's limit at N+2, a call that the count refuses holds a unit of the
transport for about 1.5 ms, and a reconnect that arrives then is refused with
`RESOURCE_EXHAUSTED`: 2% of the reconnects at 10 refused calls a second, 12% at 85 a
second, and over 90% under one sender in a tight loop.
- (a) Decision 56 stands, and the sentence is reworded: the two units are kept against
  calls that run. A reconnect that meets calls being refused may get
  `RESOURCE_EXHAUSTED` from the transport; that is a dial that got no session, not a
  denial, and the proxy dials again inside the reconnect budget of decision 18.
  `serve()` gains no code beyond the count. It edits the sentence of decision 56 and
  the note at the third bullet of decision 31.
- (b) `serve()` counts sessions as well, at most 2, sets no limit of the transport and
  has a pool of N+3 (L-V2). Every reconnect was accepted at every width, the slowest
  in 55 ms. This revises "the pool and the transport's limit stay at N+2" of decision
  56 and the fourth bullet of decision 31: every refusal is then code of the SDK, and
  an excess call waits in the queue of the pool until a thread refuses it. That queue
  has no bound and was not sized against a flood.
- (c) The pool and the limit gain spare units beyond N+2, and `serve()` counts
  sessions (L-V1). It holds only against as many refusals at once as there are spare
  units: 8 spare units lost 7 of 194 reconnects under 64 senders.
- (d) A rule of the kernel side: the proxy sends no call on a connection while the
  connection that replaces it dials. It was not run, and it is a new sentence of
  decision 18.
- Recommended: (a). The calls that an instance refuses come from the one kernel that
  dials it, and by decision 18 a proxy that lost its session marks its contributions
  unavailable before it dials again, so calls fail in the kernel and few reach the
  instance while a reconnect is in flight; that is read in decision 18 and was not
  run. A reconnect that is refused once is tried again after 250 ms, well inside the
  30 s. (b) gives the sentence as worded and gives up the refusal before any code of
  the SDK runs, with a queue that has no bound; (c) does not hold; (d) adds a rule
  where (a) needs none. No message changes under any option. Blocks: `serve()`, in
  group 4.

**Q23. What do the words "no group of the host" mean under a rootless runtime that
keeps the user?** This is also question Q22 of the S2b part of ADR-047, and one answer
settles both. Decision 57 and ADR-047 decision 43: "third-party code holds no group of
the host". R3-PODMAN-43 and R3-PODMAN-TARGETS: under rootless Podman 6.1.3 with
`--userns=keep-id` the runtime adds the site user's group to the process of the
instance as a supplementary group, whatever `--user` is given, and the host sees the
groups 1000 and 589819. The read of the credentials is not affected. In the runs the
group reached nothing: the directory of the instance is the one bind, and no other
path of the home has a mode with group bits.
- (a) The sentence is reworded to "the unit names no group of the host", in decision
  57, in the notes at decisions 24 and 44 and in ADR-047 decisions 43 and 49, with a
  note that a runtime that keeps the user adds the site user's group and that the
  group reaches no file through the one bind.
- (b) `plugin start` reads the groups of the first process of the instance with its
  uid, as the host sees them (ADR-047 decision 30), and refuses an instance that holds
  the site user's group. That refuses `--userns=keep-id` for units.
- (c) A unit is refused on every rootless runtime at S2b. That is option (b) of
  question Q19, which the owner did not choose.
- Recommended: (a). What decision 57 protects is that the instance reads nothing of
  the home but its one directory, and the runs show that under this mapping too. (b)
  refuses a mapping that ADR-047 decision 14 names as one that qualifies, for a group
  that opens no file. Blocks: the unit templates and `plugin start`, in group 3.

**Q24. Does the field `paths` of `UpdateConfig` leave?** This is not a result of a run.
Option (a) of question Q20, which the owner chose, left one point to the revision: "the
field `paths` and the merge at `paths` leave, or `paths` stays as a hint that the
revision words". Decision 59 has the field leave: `UpdateConfig` carries `activation`
and `config` alone, and the method of the Python SDK is `config_updated(config)`. The
choice was made by the record and not by the owner, so it is put here.
- (a) The field leaves, as decision 59 and the notes at decisions 8, 16 and 31 have
  it. No line changes.
- (b) The field stays as a hint. The proxy computes the changed leaves by comparing
  the committed config with the one that it held, the instance still replaces its
  config whole, and `config_updated(config, paths)` keeps its second argument.
  Decision 59 and the three notes are reworded.
- Recommended: (a). Under ADR-034 decision 21 the hand-off gives the proxy the
  committed config whole and no list of changed leaves, so a hint is code of the proxy
  for a field that nothing has to read. Nothing of major 0 is published, and a field
  can be added later inside the major if a consumer needs it. Blocks: the update
  message, in group 2.

### Edits made with this record, and left

Made, in this ADR: the status; this section; and notes of 2026-11-28 that point here,
at decision 38, at decisions 56 to 59, after the paragraph on the row of `plugin
verify` and after the list "Left to later commits" of the section "Owner answers of
2026-11-28". No decision is revised: an option that the owner has not chosen edits
nothing. Two lines that state where this ADR stands are brought to this record: its
row in the ADR table of `08` §3, and the sentence on it in the list of amendments at
the head of the plugin record. Its row in `00` §9 stays as it is (decision 53).

Left, each to the commit that records the owner's answers to Q22 to Q24 and to
question Q24 of the S2b part of ADR-061: the sentence of decision 56 and the notes at
decision 31, as the answer to Q22 has them; the words on the group in decisions 24, 44
and 57, as the answer to Q23 has them; decision 59 and its three notes, with Q24 (b)
alone; the row of `plugin verify` in decision 27, its case in decision 37 and the row
of ADR-020 in decision 39; and every line that the section "Owner answers of
2026-11-27" leaves to the commit that sets this ADR accepted. Still left as the section
"Owner answers of 2026-11-28" lists them: the form of the command that renews the
kernel's pair, which the S2b part of ADR-061 proposes as its decision 70 and its
questions Q20 and Q21, and the definition `sidecars` beyond `Endpoint` and `Report`,
with the S2b part of ADR-047.
