# ADR-041 (provisional): The remote protocol at major 0 and the identity of a remote instance on a single-tenant self-host

- Status: **proposed 2026-11-27, awaiting the project owner.** The ADR is provisional by
  its row in the ADR table (`08:1175`): it is ratified at S5 for the policy callbacks and
  at S4b for one tenant per connection and activations across replicas. It is proposed
  before the first commit of groups 1 to 3 of ADR-063 decision 30 and of the exit
  harness, and it is accepted before that code merges into `main` (ADR-063 decision 2).
  It blocks the protocol packages `sdk/remote` and `sdk/remotehost`, the proxy plugin and
  its authenticated dial, the deployment unit, every command that reads or writes a
  credential (`plugin install`, `start`, `enable`, `disable` and `remove`), `serve()`
  and the fixture of the Python SDK, the stubs of the failure matrix and `test/remote/`.
- Scope: what ADR-063 decision 34 says this ADR settles. The messages of
  `aiops.remote.v0` and which side sends each field of the first message; how the plugin
  learns the chosen major; where the call that carries `Hello` is declared; one stream or
  a service per definition; the encoding of errors across the boundary; the digest of
  the first message (ADR-063 decision 58) and the activation id (decision 63); the
  callback set, with `tools`; the deadlines, the reconnect budget and the bounds on what
  a peer may send; the pinned-key identity of ADR-063 decision 55, inside the points of
  its decision 23; the dial of the proxy under the lists of ADR-062 decisions 13 and 14;
  and the scope of the Python SDK. It changes no decision of `00-decisions.md` by itself:
  each point where an answer would change a written line is a question below and names
  the lines it edits.
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
  stream per connection", and ADR-036 holds "one stream per remote row" (ADR-036:30).
  A harness contribution is itself a server stream of steps (`07:1197`). The texts do
  not say whether "one stream" is one connection or one call.
- The handshake deadline is 2 s against a running instance (plugin record §10;
  ADR-036:30). The load, drain and dispose deadlines are construction data for the whole
  kernel, and a deadline per fiber needs an ADR (ADR-034, owner decisions of 2026-11-19,
  item 4); the launcher's default for each is 10 s, and `limits.yaml` may change them
  (`boot/limits.go:53`, `:92`).
- A proxy disposer never waits on the remote beyond a bound shorter than the dispose
  deadline, and an exhausted reconnect budget unloads the proxy (`07:1196`; `03:493`).
  No line gives either value, and no line says what brings the proxy back.
- `Endpoint(name)` of `sidecars` is available to a caller only while the instance is
  ready, "so its consumer is PENDING and never FAILED" (`03:113`; `07:356`, `:1150`).
  The matrix states PENDING for the boot cells of "Remote unreachable or handshake
  mismatch" and UNLOADING for its cell "while running"
  (`test/failure-matrix/matrix.yaml:360-385`; `08:477`), and a denial at the handshake
  fails the proxy (`08:510`; ADR-063 decision 66). `sidecars-static` reads the endpoint
  table that install writes (ADR-063 decision 39), and no line says how it learns that
  an instance stopped answering.
- A failed fiber is sticky in the kernel, and `restart-policy`, a row of `aiops-core`, is
  the only retry: it calls `Update` on a FAILED fiber with its own backoff and give-up
  rule (`03:66`, `:88`; `07:352`, `:367`).
- Remote listeners run "under a deadline" on the hooks marked remote (`07:450`), and the
  owner of a gate denies when an expected listener is unreachable (`07:448`). No line
  gives the deadline.
- The delivery record asks this ADR for three open doors and decides none: the callback
  set grows additively inside a major; a call deadline is a value of the definition, not
  a constant of the protocol; and the tenant comes from the connection, never from a
  payload field (delivery:366, :367, :370).
- `aiopsctl` writes every file of the home with mode 0600 under a home of mode 0700 that
  the site user owns, and the kernel runs as that user (ADR-047 decision 14). The unit of
  an instance names a user that is neither the site user nor root and binds one
  directory of the home, the one that holds that instance's credential, read-only
  (ADR-063 decision 25). A file of mode 0600 that the site user owns cannot be read by
  that other user.
- Plugin vet refuses a listener outside one list of plugin ids, and the same list holds
  the one exception of the dial rule, `net.DialUnix` of the operations plugin (ADR-062
  decisions 13 and 14; `internal/vet/vet.go:125-131`). The dial rule binds from the step
  that builds `dialer` (ADR-061 decision 38; `07:1405`). The proxy injects `sidecars`
  and no `dialer` (`03:72`), and `dialer` refuses cell ranges after DNS resolution
  (`00:217`, `:450`).
- The Python dependencies that ADR-063 decision 35 read are one wheel of `grpcio` for
  CPython 3.12 on `manylinux2014_x86_64`, one wheel of `protobuf`, `typing-extensions`
  and `pytest`; another interpreter or platform enters the exit only after its files
  are read (ADR-063 decision 27).

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
   `02:336` and ADR-036:30 is then one connection that holds one session. That reading
   is question Q1.
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
   | Manifest digest | Plugin | `Identify.manifestDigest`: decision 11 |
   | Definitions | Plugin | `Identify.definitions`: each `name@major.minor`, one per definition it registers with or listens on |

   No message of `aiops.remote.v0` has a field that names a row or a tenant. Both come
   from the connection (decision 22), and an unknown field that names either is ignored
   like any unknown field (`07:1128`; ADR-063 decision 23; delivery:370).
7. **The kernel evaluates the gate on `Identify` before it sends any config.** The proxy
   hands both `Hello` lists, the digest and each definition version to the handshake
   entry of the gate (ADR-063 decisions 16 and 57). On a denial the kernel sends `Bye`
   with the check and the reason and closes the connection, so the reason is logged on
   both sides, and the proxy fails with check 3 named (`08:510`; ADR-063 decision 66).
   `Activate` is sent only after an admitting verdict.
8. **The session messages.** Every session message carries `id`, a number that its
   sender chooses and never repeats on that stream, and an answer carries `re`, the `id`
   it answers, with the status of decision 10.

   | Message | Sender | Fields beside `id` | Answered by |
   |---|---|---|---|
   | `Open` | Kernel | Decision 6 | `Identify` |
   | `Identify` | Plugin | Decision 6 | `Activate`, or `Bye` |
   | `Activate` | Kernel | `activation`, `generation`, `config` (`07:1161`). The generation is a label for logs. The config is the row's config as JSON, which the kernel has validated against the `configSchema` of the kept manifest (ADR-063 decision 10) | `Activated` |
   | `Register` | Plugin | `activation`, `effectId`, `definition` as `name@major.minor`, and one of two kinds: a contribution, with its name and its spec as the definition's proto has it, or a listener, with its hook and stage (`07:1162`) | `Result` |
   | `Unregister` | Plugin | `activation`, `effectId`: the plugin disposed that effect | `Result` |
   | `Activated` | Plugin | `activation`, a status: the plugin's `Apply` returned | None |
   | `UpdateConfig` | Kernel | `activation`, `config`: the volatile change of ADR-063 decision 49 | `ConfigUpdated` |
   | `ConfigUpdated` | Plugin | `activation`, a status | None |
   | `Callback` | Plugin | `activation`, `definition`, `method`, `payload`, `timeout`, and the optional `handle` of decision 13 | `Result`, with the answer's payload |
   | `Deactivate` | Kernel | `activation` (`07:1166`) | `Deactivated` |
   | `Deactivated` | Plugin | `activation` | None |
   | `Ping`, `Pong` | Kernel, plugin | None: the liveness check of decision 16 | `Pong` |
   | `Result` | Either | `re`, a status, an optional payload | None |
   | `Bye` | Kernel | A status: why the kernel ends the session. It is the last message | None |

   A message whose kind the receiver does not know is answered with a `Result` of status
   `unknown` and changes nothing: the stream and the activation stay (`07:1126`; ADR-063
   decision 5). An unknown call of a definition's service is answered with the same
   status by the transport. The names above are the names of major 0 and replace the
   descriptive ones of `07` §5 (`07:67`, R9); before the first community release they
   change only by an addition to this ADR (ADR-063 decision 62).
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
   - Every call that the kernel makes on a definition's service carries the activation
     id as the call metadata `aiops-activation`, and the plugin-side host routes the
     call to that activation's fiber and answers `inactive` when it has none. The id is
     the kernel's own value and names no row and no tenant.

### Errors across the boundary

10. **An error is a status: a code and a reason from a closed list.** On a call of a
    definition's service the code is the status code of the transport and the reason is
    the trailer `aiops-reason`. In a session message both are fields of the status. No
    error detail message is used, so no further proto package is imported. A receiver
    that does not know a reason acts on the code alone (`07:1126`).

    | Condition | Code | Reason | In Go it is |
    |---|---|---|---|
    | A registration from an activation that is superseded or of an earlier boot | `FAILED_PRECONDITION` | `inactive-effect` | `kernel.ErrInactiveEffect` (`07:1162`) |
    | A call, a callback or another message on an activation that is not ACTIVE on the wire | `FAILED_PRECONDITION` | `inactive` | `kernel.ErrInactive` (`07:1165`; `03:366`) |
    | The caller's context was cancelled | `CANCELLED` | none | `context.Canceled` |
    | The deadline of the call passed | `DEADLINE_EXCEEDED` | none | `context.DeadlineExceeded` |
    | A registration, a listener or a callback outside the contribution set or the manifest | `PERMISSION_DENIED` | `gate-check-9` | The refusal of the proxy, naming check 9 (`08:507`) |
    | A registration of a definition version that is not served; a denial at the handshake | `FAILED_PRECONDITION` | `gate-check-3` | The refusal of the proxy, naming check 3 (`08:501`) |
    | A bound of decision 19 is passed | `RESOURCE_EXHAUSTED` | `bound` | The refusal, naming the bound |
    | A message, a call or a callback method that the receiver does not know | `UNIMPLEMENTED` | `unknown` | Reported; nothing changes |
    | A message that breaks an order or a form of this ADR, as a `Hello` of the wrong kind | `INVALID_ARGUMENT` | `protocol` | The refusal, naming the rule |
    | The instance cannot be reached, or the session is lost and not yet back | `UNAVAILABLE` | none | The error of the kernel side that a dispatcher's breaker counts (`07:1196`) |
    | The contribution's own error | The code its definition's proto maps it to, or `UNKNOWN` | none | The definition's error value, or a plain error with the text |
    | The contribution panicked or raised outside its contract | `INTERNAL` | none | A plain error; the stack stays in the instance's log |

    The list grows by addition. The text of an error crosses as the status message and
    is logged as data, never as a format string.

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

    | Callback | Scope, as written | Arrives |
    |---|---|---|
    | Telemetry | Logs, metrics and traces over OTLP (`07:1173`; `00:213`) | S3 |
    | `secrets` | Read-only, the instance's own (tenant, plugin, name) scope (`07:1174`; `00:218`) | S3 |
    | `kv`, `blob` | The plugin's own prefix; each write is its own commit (`07:1175`; `00:215`) | S3 |
    | `tenants` | The tenant record, read-only (`07:1179`; `00:224`) | S4a |
    | `audit` | Append, as its own commit (`07:1176`; `00:226`) | S5 |
    | `credentials` | For an executor only, answered through `remediation` with the `ChangeToken` of the call in flight (`07:1180`; `00:227`) | S5 |
    | `llm` | Through `llm-gateway`, with the instance's virtual key; for a harness each call carries the run handle (`07:1177`; `00:229`) | S8a |
    | `tools` | For a harness only: `Specs` and `Invoke` on the offered tool set of the active run, each call carrying the run handle. A call for another run, for a run that ended or for a tool outside the set is refused (`07:1178`; `00:233`; `08:697`) | S8a |

    The steps are those of ADR-063 decision 26. At S2b the path is built and proven on
    one test definition marked `callback`, which ships in no release table (ADR-063
    decision 51).
    - A callback is the session message `Callback`. `definition` and `method` name a
      method of a definition marked `callback`, and `payload` is that method's request
      as the definition's proto has it. The proxy refuses, with `gate-check-9`, a
      callback of a definition that is not marked `callback` or that the manifest's
      `uses` does not list (ADR-048 decision 16; ADR-063 decisions 10 and 16).
    - `handle` is optional opaque bytes that the kernel minted and gave the plugin in
      the request of a call in flight: the run handle for `llm` and `tools`, and for
      `credentials` what ties the callback to the `ChangeToken` of that call (`07:62`,
      R4). The kernel side checks it and the plugin never builds one. Its layout is
      fixed with its definition: the run handle at S8a under ADR-051 (`07:74`, R16),
      that of `credentials` at S5.
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
    | The handshake against a running instance: the dial, the TLS handshake, `Hello`, `Open`, `Identify` and the verdict of the gate | 2 s | Plugin record §10; ADR-036:30 |
    | The activation: from `Activate` to `Activated` | What is left of the kernel's load deadline when `Activate` is sent | `07:358`; ADR-034, owner decisions of 2026-11-19, item 4 |
    | A contribution call | The deadline of the caller's context, which the dispatcher sets (`07:783`, `:1153`) | delivery:367 |
    | A listener call | Decision 15 | `07:450` |
    | A callback | `Callback.timeout`, the deadline of the plugin's own call context | delivery:367 |
    | `UpdateConfig` to `ConfigUpdated` | 2 s; an error or no answer is the error of the proxy's listener on `loader/volatile-update` | This ADR |
    | The liveness check: a `Ping` every 10 s, and its `Pong` within 5 s | A missing `Pong` is a lost session | This ADR |
    | The proxy disposer's wait on the remote: from `Deactivate` to `Deactivated` | The smaller of 2 s and half the kernel's dispose deadline | `07:1196`; `03:493` |

    The whole of the proxy's `Apply` stays inside the kernel's load deadline, which is
    kernel-wide, so where `limits.yaml` sets it under 2 s the handshake has what is left
    of it. The proxy makes no call on a definition's service for a caller whose context
    has no deadline. A call deadline is never a constant of the protocol, so a run of
    tens of minutes on a remote harness is its definition's choice (delivery:367). The
    cold budget against an instance scaled to zero is a value of the hosted form and
    waits for it (ADR-036:30).
17. **The proxy disposer.** At unload the proxy cancels its calls in flight, sends
    `Deactivate`, waits for `Deactivated` no longer than the bound of decision 16 and
    closes the connection whether or not the answer came. Its registrations are effects
    and are unwound by the kernel in the same walk. A remote that ignores `Deactivate`
    therefore costs the bound and nothing else: the disposer returns inside the dispose
    deadline, the outcome is reported and the process is not replaced (`07:1196`;
    ADR-063 decision 65).
18. **The reconnect budget is 30 s. An instance that does not answer is not ready, and a
    denial is a failure.**
    - A session is lost when the stream ends without a `Deactivate` or a `Pong` is
      missing. The proxy stays ACTIVE, marks its contributions unavailable, so each call
      fails at once with `UNAVAILABLE` behind the dispatcher's breaker, and dials
      again: at once, then after 250 ms, doubling to 5 s, each wait changed by up to a
      fifth at random (`07:1196`).
    - A reconnect inside the budget is a new connection and a new activation with the
      next epoch (decision 5). A registration of the new activation that has the
      definition and the name, or the hook and the stage, of one that the old
      activation held takes that entry over. An entry that the new activation has not
      registered again when `Activated` arrives is unwound.
    - The budget is exhausted when 30 s have passed since the loss with no session
      ACTIVE on the wire. The instance is then "not answering", as it is when a dial at
      load is refused or gets no answer inside the handshake deadline.
    - The proxy reports an instance that is not answering to `sidecars` and stops
      dialling. `sidecars` no longer holds that endpoint as available, so the kernel
      unloads the proxy, every registration is unwound and the row is PENDING and says
      what it waits for (`03:113`; `07:356`, `:360`, `:1150`, `:1196`). That is the
      "unload after the budget" and the PENDING of the matrix row (`08:477`;
      `test/failure-matrix/matrix.yaml:360-385`).
    - What brings the proxy back is the endpoint, available again: the kernel loads the
      proxy, and that load is the next dial. A proxy whose endpoint is not available
      dials nothing. When the provider holds an instance as ready again after a report
      is the provider's rule, fixed with the definition `sidecars` at S2b (ADR-063
      decision 39). This ADR needs one thing of that definition: that a consumer can
      report the instance behind an endpoint as not answering.
    - A denial is not "not ready". A denial of the gate at the handshake, a peer that
      fails decision 21, a bound of decision 19 and a broken rule of the protocol each
      fail the proxy at once, with the check or the rule named and no budget: the row
      is FAILED (`08:510`; ADR-063 decision 66). `restart-policy` retries it like any
      FAILED fiber, and each retry is a new dial, so an instance that the operator
      starts with the right image is picked up with no new generation (`03:66`, `:88`;
      `07:367`). When `restart-policy` gives up, the row stays FAILED until an operator
      updates it (`07:367`); at S2b that is `plugin disable`, `plugin start` and
      `plugin enable`.

    The budget and the waits are constants of the kernel side of the protocol. No
    kernel change is planned for this; if a load that reported its instance cannot end
    PENDING without one, that change is an ADR-034 addition under ADR-063 decision 3.
    The choice against the other ways back is question Q3.
19. **The bounds on what a peer may send.** The peer of the kernel is third-party code,
    so each bound is checked by the receiver before the content is used (ADR-063
    decisions 28 and 34).

    | What | Bound | When it is passed |
    |---|---|---|
    | `Hello`, as encoded | 4,096 bytes; at most 16 majors; at most 16 entries of `diagnostics`, a key of at most 64 bytes and a value of at most 256 | The receiver refuses the `Hello` and logs the peer's identity and the size and nothing of the content. The kernel closes the connection; the plugin answers `bound` |
    | Any other message, as encoded, in either direction | 4 MiB | The receiver ends that call or that session with `bound`. A session ended this way fails the proxy; it is not a lost session and has no budget |
    | `Identify` | The plugin id in the grammar of the manifest reader; a version of at most 64 bytes; at most 64 definitions and 64 feature names, each of at most 128 bytes | The kernel sends `Bye` with `bound` and the proxy fails |
    | Registrations held by one activation | 256 | The `Register` is refused with `bound`; the session stays |
    | Callbacks in flight on one activation | 64 | The `Callback` is refused with `bound`; the session stays |

    Each side logs of `diagnostics` only the entries inside the bound, each value
    quoted with its control characters escaped. The SDKs apply the size bounds to what
    the kernel sends as well. A known ceiling: S2b sets no bound on the rate of
    messages. A peer that floods costs the one goroutine that reads its session, and a
    rate bound is added by an addition to this ADR when S4b measures remote rows
    (ADR-036).

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

    Neither certificate can issue another, and no verifier of this protocol builds a
    chain, so no key of the site can mint an identity (ADR-063 decision 55). The curve
    and the form must be accepted by the TLS of the Go standard library and by the TLS
    inside the pinned `grpcio`; the cases of decision 37 prove it in both languages.
21. **Each side trusts exactly one certificate.** The connection is TLS 1.3 with both
    certificates presented; the kernel side offers nothing lower, and it is one end of
    every connection. The pin of a peer is the SHA-256 of the DER encoding of its
    certificate.
    - The kernel accepts the instance only when the presented certificate equals the
      pinned one and its URI name is the plugin id of the row that dials. The host name
      of the endpoint plays no part: the endpoint table may change it.
    - `sdk/remotehost` and `serve()` accept a dialler only when the presented
      certificate equals the kernel's certificate in the instance's directory. Where a
      TLS library can only verify against a set of roots, that certificate is the only
      root and the SDK still compares the presented certificate with it before it
      serves a call.
    - Both checks finish before `Hello` is answered (ADR-063 decision 23). A peer with
      another certificate, with a certificate signed by either pinned key, or in
      plaintext is refused, and the refusing side logs the fingerprint it saw.
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
    | `remote/pins/<plugin id>/cert.pem` | The pin: the certificate that the kernel accepts for that plugin's instance | The proxy, at every dial | Never |
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
      `sdk/remotehost` and `serve()` refuse to start on a TCP address without that
      directory, and neither has an option that serves TCP in plaintext.
24. **The instance's directory is readable through the site user's group.** `aiopsctl`
    writes `remote/instances/<plugin id>/` with mode 0750 and its three files with mode
    0640, under the site user and that user's group. The unit runs the instance with a
    user id that is neither the site user's nor 0 and with the site user's group as its
    only group. On the host the 0700 home keeps every other user out of the directory,
    whatever their group. Inside the instance nothing else of the home is bound, so the
    group reaches no other file, and the reload socket checks the peer's user id, not
    its group (ADR-062 decision 5). This is one exception to "each file with mode 0600"
    of ADR-047 decision 14, and question Q5 holds the other ways.
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
      the last one deletes `remote/kernel/`, and the next install makes a new pair.
      That is its rotation and its revocation at S2b: an instance trusts only the copy
      in its own directory, which install and `plugin start` write from the current
      pair.
    - The kernel's key is a file that only the site user reads, and whoever reads it can
      already write the home and submit a generation. The instance's key is renewed by
      the three commands above. So S2b builds no rotation command; question Q4 holds
      the alternatives.
26. **Revocation with no leader.** The proxy reads the pin, the kernel's key and the
    kernel's certificate from their files at every dial and keeps none across dials,
    so a change of those files takes effect at the next dial, with no leader and no
    restart of the process (ADR-063 decision 23).
    - `plugin install` makes the kernel's pair on first need, the instance's pair, the
      instance's directory and the pin, in the same write as the lock entry, and a
      denial writes nothing (ADR-063 decisions 17 and 55).
    - `plugin disable` first deletes the pin, where no enabled row of another profile
      of the site names the plugin; then it submits the generation that unloads the
      proxy, which ends the open session; then it stops the unit (ADR-063 decisions 23
      and 68; `03:495`; `07:1195`). While another profile's enabled row names the
      plugin it unloads the proxy of its own profile and touches no credential. A
      generation that is refused or fails restores the pin with the other files of the
      snapshot, and the unit is then not stopped (ADR-062 decision 23).
    - `plugin remove` revokes as `disable` does and, after a result that shows the
      change, deletes `remote/instances/<plugin id>/` and `remote/pins/<plugin id>/`
      with the other kept files, when no profile names the id (ADR-063 decision 68;
      ADR-047 decision 10).
    - A revoked pair never returns. `plugin start` of a plugin that has no pin makes a
      new key pair and certificate, replaces the three files of the instance's
      directory, writes the new pin and only then starts the unit. So an instance
      started again with the old key opens no stream on any later dial, also after the
      row is enabled again (ADR-063 decisions 23 and 28). This is `plugin start`'s on
      every target, since it is the one command of `aiopsctl` between `disable` and
      `enable` (`08:1293`; ADR-063 decision 17); what else it does for a systemd unit
      is the S2b part of ADR-061.
    - `plugin enable` refuses a row whose pin is absent before it submits anything, as
      it does for a missing endpoint, and prints the exact command `aiopsctl plugin
      start <id>` (ADR-063 decision 17). The text and the exit code are the S2b part of
      ADR-061.
    - With the pin absent the proxy makes no connection: it fails with the reason
      "revoked" before any byte is sent.
27. **The Unix socket of `plugin check`** carries the same `Hello`, the same session and
    check 3 with no TLS, and the identity is the process that the runner started
    (ADR-063 decision 54, reading 2). The check runner makes a directory of mode 0700
    for the socket, starts the plugin's entrypoint with `AIOPS_REMOTE_LISTEN` naming a
    socket in it and no credential directory, and is the only dialler. No row of a
    server profile reaches that code path: the proxy dials only through decision 28
    (ADR-063 decision 23). The stubs of the failure matrix are processes with pairs
    made by the same function as install's (ADR-063 decision 64).

### The dial of the proxy and the lists of plugin vet

28. **One dial function, and no listener in a plugin.**
    - The dial is one exported function of the kernel side of the protocol, the package
      of the module `sdk/remote` that ADR-063 decision 9 names. It takes the endpoint
      that `sidecars` gave, the three files of decision 23 and a deadline, and returns
      a connection only after decision 21 has passed. The proxy plugin calls it for a
      TCP endpoint. It has no variant without TLS for TCP. The check runner calls its
      Unix form for the socket of decision 27.
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
      import boundary refuses that import is the S2b part of ADR-054 (ADR-063 decision
      34).
    - The dial rule is not built before `dialer` exists (ADR-061 decision 38). The
      proxy does not dial through `dialer`: its endpoint is a line of a site file that
      install wrote, not a URL that a tenant supplied, the instance is a peer inside
      the ranges that `dialer` exists to refuse, and the proxy's injects hold no
      `dialer` (`03:72`; `00:217`, `:450`). When the rule is built, the list of ADR-062
      decision 14 needs a second named entry, for `aiops/remote` and the function
      above. That changes the words "one named exception" of `07:1405` and harness:58
      and is question Q6.

### The scope of the Python SDK

29. **What `aiops-sdk` holds at S2b** is the parts of `07:1205-1213`, as ADR-063
    decision 18 lists them: one generated module per `contrib` definition and major;
    `@contributes`; `@listens`; `aiops-sdk manifest`; `serve()`; the fixture
    `aiops_sdk.testing.host()`; and the template. At S2b the generated modules are
    those of `echoers` and `stampers`, and the callback clients are those of the test
    definition (ADR-063 decisions 14 and 51). A Python plugin is always remote
    (`07:1203`). The hand-written Python stays under the cap of ADR-063 decision 36.
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
    - It reads the two variables of decision 23, opens the listener, authenticates
      every dialler (decision 21) and serves the `Hello` call, the session and the
      services of the definitions that the plugin's decorators name.
    - One activation per connection (`07:1211`): for each `Activate` it builds one
      instance of every class marked `@contributes`, with that activation's config,
      registers each and every `@listens` function, and answers `Activated`. It drops
      them when the session ends.
    - Each contribution call and each listener call runs on a worker thread of a pool
      that holds 32 threads unless `serve()` is given another size. A call that finds
      no free thread is answered `bound`. A contribution object is therefore called
      from several threads at once, as a provided service is in Go (`07:475`).
    - A contribution is a plain method, as `07:1222-1228` sketches it. Coroutine
      contributions are not part of S2b.
    - A thread cannot be stopped from outside. When the deadline of a call passes or
      the kernel cancels it, the kernel has its answer at once, the call context says
      so, and the method runs until it returns.
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
    sketch of `07:1227`. A callback is a blocking call that carries what is left of the
    call's deadline as its `timeout` (decision 16). The context carries no tenant and
    no row at S2b: both are the connection's (decision 22), and the tenant reaches a
    plugin with the first definition that takes one, where S4b ratifies this ADR. A
    handle of decision 13 is a field of the request of the definition that mints it,
    not of the context.
33. **The fixture wraps `aiopsctl plugin check <dir> --json`** (`07:1212`; ADR-061
    decision 30). `aiops_sdk.testing.host(plugin=<dir>)` is a pytest fixture and a
    context manager. It finds the binary in the variable `AIOPSCTL` or on the search
    path, runs the command once and reads its report. `run_conformance()` passes when
    steps 4 and 5 of the report passed and otherwise fails the test with the report's
    one line per obligation (`07:1577-1578`; ADR-063 decision 17). The report is
    available to the test as read. `plugin check` runs no Python test, so the fixture
    cannot call itself (ADR-063 decision 54, readings 5 and 6). The fixture needs no Go
    toolchain and no container runtime (`07:1236`). The arguments that seed a
    definition's fake, as `tenant` and `secrets` in `07:1241`, and `calls()` arrive
    with the first callback definition that ships in a release, at S3, by an addition
    to this ADR; `07:1236-1243` is a sketch (`07:66`, R8).
34. **Version and pins.**
    - The version of `aiops-sdk` is a packaging number that no gate reads (`07:545`).
      Inside the repository it is `0.0.0`, as the Go SDK modules are `v0.0.0` (ADR-060
      decision 21), and a release gives it the version of that release. `serve()`
      sends it as `sdkVersion` in `Hello.diagnostics`.
    - The package declares three run-time dependencies, each at the exact version that
      ADR-063 decision 35 read: `grpcio` 1.85.4, `protobuf` 7.36.2 and
      `typing-extensions` 4.16.0. Its testing part adds `pytest` 9.2.0 and the packages
      that it requires, which no plugin image needs to run (ADR-063 decision 18).
    - One constraints file under `python/` holds every package of the set with the
      SHA-256 of each file that was read. The Python job, the exit and the image build
      of the template install from it with hash checking, and the check of ADR-063
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
    | The layout of each real callback and of its handle | With its definition: S3, S4a, S5 and S8a (decision 13) | ADR-063 decision 26; `07:74`, R16 |
    | The issued certificate, the cell's plugin CA, the virtual key and their revocation by a leader | S4b on the test provider of `sidecars`, S9b in hosted | `00:410-411`; `03:495`; ADR-063 decision 26 |
    | Several activations of one instance; a second replica's connection | S4b | `07:1163`; ADR-063 decision 67 |
    | The handover of new remote code | S6b, with `plugin update` | ADR-063 decision 69 |
    | When a provider of `sidecars` holds an instance as ready again after a report, and the form of the report | With the definition `sidecars`, at S2b | ADR-063 decision 39; decision 18 |
    | The unit template, the endpoint table, the network of an instance and the rows of the home table | The S2b part of ADR-047 | ADR-063 decisions 24, 25 and 34 |
    | The command forms and the refusal texts of `plugin start` and `plugin enable` | The S2b part of ADR-061 | ADR-063 decision 34 |
    | Every `plugin.toml` key and how a decorator names a key | The S2b part of ADR-048 | ADR-063 decision 34 |
    | The register entries, the import boundary over the remote modules and `python/` | The S2b part of ADR-054 | ADR-063 decisions 34 and 42 |
    | A rate bound on session messages; coroutine contributions; a rotation command | An addition to this ADR, when a consumer needs it | Decisions 19, 25 and 31 |

### The tests

37. **Each part has a case that fails on its base** (ADR-063 decision 29). The cases of
    ADR-063 decision 28 stand; these are added, in Go and, where an SDK is named, in
    Python as well:
    - the freeze of `call.proto`, with the form that question Q2 decides;
    - `Hello` of the wrong kind, and a call of a major before `Hello`, each refused;
    - every row of the table of decision 10, sent by one side and read by the other,
      through `sdk/remotehost` and through `serve()`;
    - a `Register` and a `Callback` that carry another activation id, and each after
      `Deactivate`, refused with the reason of decision 9;
    - a lost session that returns inside the budget: the row stays ACTIVE, calls fail
      at once in between, the entries are taken over and an entry not registered again
      is unwound; and one that does not return: the instance is reported, the row is
      PENDING, every registration is gone, and the row is ACTIVE again once the
      endpoint is available and the instance answers;
    - a dial at load that gets no answer: PENDING, not FAILED; and a denial at the
      handshake: FAILED, and ACTIVE after `restart-policy` retries it against an
      instance of the right image;
    - a missing `Pong`, counted as a lost session;
    - a remote that ignores `Deactivate`: the disposer returns at its bound, under
      kernel limits of 10 s and of 1 s;
    - a dispatch with no deadline, refused, and a listener that misses its deadline on
      a gate, denied;
    - each bound of decision 19, in both directions, with the log holding no more of
      `diagnostics` than the bound;
    - a peer whose certificate was signed by the kernel's key, and one signed by the
      instance's key, each refused by the kernel, by `sdk/remotehost` and by `serve()`;
    - the pin and the kernel's key replaced while the process runs: the next dial uses
      the new files with no restart;
    - after `plugin disable`, `plugin start` and `plugin enable`, an instance that
      still holds the old key opens no stream;
    - a rolled-back `plugin disable` leaves the pin and the unit as they were;
    - the modes of decision 24, the instance's user reading its three files and failing
      to open `remote/kernel/`, another directory of the home and `run/reload.sock`;
    - `serve()` and `sdk/remotehost` started on a TCP address with no credential
      directory, each refusing to start;
    - the fixture on a plugin that passes and on one that fails an obligation.

## Consequences

- The handshake package gains a second file and, with question Q2 (a), a second frozen
  surface: one service name and one call name.
- The adapter generator writes a service per `contrib` definition, the Go stubs on both
  sides and the Python glue on the public API of `grpcio` (ADR-063 decision 45). The
  session, the activation and the callback envelope are written once, by hand, in the
  kernel side of the protocol, in `sdk/remotehost` and in `aiops-sdk`.
- No kernel change is planned (ADR-063 decision 3). The activation id is the proxy's
  own, a failed proxy returns through `restart-policy`, and a proxy whose instance does
  not answer returns through the availability of its endpoint. The definition
  `sidecars` gains the report of decision 18, with question Q3 (a).
- No module is linked for the identity: key pairs and certificates come from the
  standard library of Go, and `serve()` verifies with the standard library of Python
  and the public API of `grpcio`.
- The home gains the directory `remote/`, with one directory whose mode is not 0700 and
  three files whose mode is not 0600 per installed remote plugin (decision 24).
- `plugin start` writes a credential when the pin is absent, so it takes the write lock
  of the home; `plugin enable` gains one refusal; `plugin remove` of the last remote
  plugin deletes the kernel's pair.
- `00` §5.4 states the lifetime, the rotation and the revocation of the two classes of
  ADR-063 decision 55, and item 25 of `02` §6 is closed.
- The supported Python is one version on one platform until a wider set is read and
  run (decision 30).
- On acceptance the row of this ADR in the tables of `00` §9 and `08` §3 changes from
  open to accepted, provisional.

## Edits on acceptance

Made in the commit that records the owner's answers (`08:999`), for the options the
owner chooses:

- `00:412` and `00:413`: the lifetime, rotation and revocation cells read as decisions
  25 and 26 have them, in place of "fixed by ADR-041".
- `00:779` and `08:1175`: the row of ADR-041 is accepted, provisional, with the date,
  and keeps its ratifications at S5 and S4b. `01:383`, `05:845` and `06:805` name it as
  accepted for S2b.
- `00:267` and `07:553`, with question Q2 (a): "the handshake message and its call".
- `07:67` (R9): the message names of `07` §5 are those of decision 8.
- `07:1127`: the call that carries `Hello` (decision 1), and that the plugin learns the
  major from the session call (decision 3). `07:1128`: the two first messages and the
  sender of each field (decision 6).
- `07:1151`, `02:336` and the row of ADR-036:30 by a dated note, with question Q1 (a):
  one connection with one session, and the contribution services on that connection.
- `07:1161-1166`: `Open`, `Identify`, `Activated`, `Unregister`, `UpdateConfig` and
  `Bye` beside the three messages named there.
- `07:1169`: the set grows additively inside a major (decision 13).
- `07:1196` and `03:493`: the budget of 30 s and, with question Q3 (a), that an
  instance which does not answer leaves the row PENDING through `sidecars` and a denial
  leaves it FAILED for `restart-policy`. `03:113` and `07:1150`: the proxy's report.
- `02:215`, `02:336`, `07:1195` and `08:1293`: the words "the leader can revoke" are
  scoped to the issued certificate, from S4b; for the pinned key pair the lines name
  decision 26 (ADR-063, "Left to later commits").
- `02:483`, item 25, and `03:495`: the credentials are the two pairs of decision 20,
  and the item is closed by this ADR.
- `07:1211` and `07:1212`: the concurrency of `serve()` and the command that the
  fixture wraps (decisions 31 and 33).
- `07:1405` and harness:58, with question Q6 (a): the second named entry of the dial
  rule; a dated note at ADR-062 decision 14.
- ADR-047 decisions 13 and 14, by its S2b part: the rows of `remote/` and the modes of
  decision 24, with question Q5 (a).

## Questions for the owner

Each question gives its options, the recommended one, why, and what the answer blocks.
An option that changes or reads a written line says so and names the line.

**Q1. Is "one stream" one connection with one session, and are contribution calls a
service per definition?** `07:1151`: the instance "serves the contribution services of
its definitions on one stream per connection"; `02:336` and ADR-036:30 hold "one stream
per remote row". ADR-063 decision 34 leaves the choice to this ADR.
- (a) One connection per proxy row carries one session stream, for the activation and
  the callbacks, and each contribution call and listener call is a call of the service
  that the definition's proto declares, on that connection (decision 4). Deadlines,
  cancellation and the server stream of a harness are the transport's own. `07:1151`,
  `02:336` and ADR-036:30 read "one connection, with one session".
- (b) Every call is a message on the one stream, in an envelope that this protocol
  defines. The three lines stand to the letter. The protocol then carries its own call
  ids, cancellation, deadlines, flow control and streamed answers, in Go and in Python.
- Recommended: (a). It is what the generated service glue of ADR-063 decision 45 is
  for, a hung or slow call cannot hold the stream that carries callbacks and
  `Deactivate`, and nothing the transport already does is written twice. Blocks: the
  protocol packages and the adapter generator.

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

**Q3. What state does an instance that does not answer leave the row in, and what
brings the proxy back?** `07:1196` and `03:493`: an exhausted budget "unloads the proxy
and every registration is unwound". `03:113` and `07:356`: a consumer of an endpoint
whose instance is not ready is "PENDING and never FAILED". The matrix row has "PENDING
or FAILED; reconnect" at boot and "unload after the budget" while running (`08:477`),
and its cells state PENDING and UNLOADING (`test/failure-matrix/matrix.yaml:360-385`).
No line says how `sidecars-static`, which reads a table, learns that an instance
stopped answering.
- (a) PENDING, through `sidecars` (decision 18). The proxy reports an instance that
  does not answer, the endpoint is no longer available, the kernel unloads the proxy,
  and the endpoint's return loads it again. A denial is FAILED and `restart-policy`
  retries it. The definition `sidecars` gains the report, and the provider's rule for
  offering the endpoint again is fixed with that definition (ADR-063 decision 39).
- (b) FAILED for both, retried by `restart-policy`. `sidecars` gains nothing. An
  unreachable instance is then FAILED where `03:113` and `07:356` say never FAILED, the
  cells of `matrix.yaml:360-371` that state PENDING are edited, and after
  `restart-policy` gives up, an instance that starts is not picked up until an operator
  updates the row.
- (c) `sidecars-static` probes each endpoint itself and withdraws it while the instance
  does not answer. The provider then dials, and to tell a hung handshake from a
  healthy one it would need the kernel's key.
- Recommended: (a). It is the state the written lines and the matrix give, the one
  dialler stays the proxy, and an instance that comes back is picked up with no
  command. (b) weakens two written lines and (c) adds a second holder of the kernel's
  key. Blocks: the proxy, the definition `sidecars` and the matrix cells.

**Q4. What are the lifetime and the rotation of the two pairs?** `00:412-413` leave
both to this ADR, and ADR-063 decision 55 gives no date and no rotation.
- (a) No expiry, and renewal by revocation with the commands that exist: `plugin
  disable`, `plugin start` and `plugin enable` for an instance, and removal of the last
  remote plugin for the kernel's pair (decisions 25 and 26). No command is added.
- (b) As (a), with a rotation command for each pair: one that renews an instance's pair
  and restarts its unit, and one that renews the kernel's pair, rewrites its copy in
  every instance's directory and restarts every unit. Its forms are the S2b part of
  ADR-061.
- (c) A validity of a fixed number of days, as the issued certificate has one day
  (`00:410`). Something must then renew before the date on a host with no leader, and
  an instance stops answering when it does not.
- Recommended: (a). Whoever can read the kernel's key can already write the home and
  submit a generation, so a command that rotates it guards against a reader who has
  more than the key; an instance's pair is renewed with three commands that exist. (c)
  adds an outage that no component is there to prevent. Blocks: `plugin install`,
  `start`, `disable` and `remove`.

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
  on the host the home still admits the site user alone. Blocks: the unit, `plugin
  install` and `plugin start`.

**Q6. How does the proxy dial once plugin vet refuses a dial outside `dialer`?**
`07:1405` and harness:58 give the dial rule "one named exception", the operations
plugin's `net.DialUnix`, and ADR-062 decision 14 holds every other dial to the rule. The
rule binds from the step that builds `dialer` (ADR-061 decision 38).
- (a) A second named entry on the same list: `aiops/remote` may call the dial function
  of decision 28, and the check runner in `aiops/operations` its Unix form.
  `07:1405` and harness:58 name both, and ADR-062 decision 14 gains a dated note.
- (b) The proxy dials through `dialer` from the step that builds it. Its injects gain
  `dialer` (`03:72`), and the ranges that `dialer-guarded` refuses must then admit the
  network of each instance, which opens the fence for every other caller of `dialer`
  on that host (`00:450`).
- (c) Left to the step that builds `dialer`, as ADR-061 decision 38 leaves the registry
  client.
- Recommended: (a). The endpoint is a site file's line and not a URL that a tenant
  supplied, the peer is authenticated by its pin before any byte of the protocol, and
  the fence of `dialer` stays whole. The entry is written now, so the step that builds
  the rule finds it. Blocks: nothing before the dial rule is built; the answer is
  needed before the proxy merges, so that its dial is one function that a list can
  name.

**Q7. Which interpreters does `aiops-sdk` support at S2b?** ADR-063 decision 35 read the
wheels of one interpreter and platform, and its decision 27 admits another only after
its files are read.
- (a) CPython 3.12 on Linux x86-64 with glibc, and nothing else until more is read
  (decision 30).
- (b) CPython 3.11, 3.12 and 3.13 on the same platform. The wheels of `grpcio` for 3.11
  and 3.13 are read first, and the Python job runs three times.
- Recommended: (a). The supported set is exactly what was read and what the exit runs,
  and widening it later is an addition that changes no message. Blocks: the Python job
  and the package metadata.

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

**Q9. Does the package pin its dependencies exactly?** ADR-063 decision 52 holds the
dependency set of `aiops-sdk` to L7, each package with a register entry and a reading.
- (a) Exact versions in the package metadata, and one constraints file with hashes
  (decision 34). An author's environment holds exactly what was read.
- (b) Ranges in the metadata and exact versions only in the constraints file. An
  author's installer may then pick a version that was never read.
- Recommended: (a) while nothing is published. The choice for the public package index
  is made when L14 clears, with that index (ADR-063 decision 26). Blocks: the package
  metadata and the template.
