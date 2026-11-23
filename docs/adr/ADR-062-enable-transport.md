# ADR-062: The enable transport: how `aiopsctl` submits a generation before `admin-api`

- Status: **proposed 2026-11-23, awaiting the project owner.** Nothing in this ADR is in
  force. `00-decisions.md` is unchanged and wins until the owner accepts this ADR and the
  edits it lists in "Edits on acceptance" land in the commit that records the answers
  (`08:996`).
- Scope: the transport that ADR-060 decision 48 (question Q12, answer (c)) leaves open:
  how `aiopsctl plugin enable`, `disable`, `remove` and `install` submit one generation to
  the running process's reload queue before `admin-api` exists. Decision 48 asks for the
  transport, its plugin vet rule and how it relates to `admin-api` at S4a, written in an
  ADR before the S2a exit test. This ADR also settles the row that serves the transport
  and the edit of `00:275` that a new row needs, the wire format, the authentication by
  socket permissions and peer credentials, the timeouts, what the reply carries, what
  `aiopsctl` does with it, and the tests.
- Citations: `00`, `02`, `03`, `07` and `08` are the files of `docs/architecture/` with
  those numbers, and `08:446` is line 446 of `08` at commit `a3c85ae`. ADR-060 cites lines
  at `491a518`; of the lines it cites for decision 48, only `07:1848` moved, and it is
  `07:1855` here. "harness" is `docs/design/harness-and-upstreams-2026-11.md`, cited the
  same way, and `dsh-architecture` §n is section n of `docs/research/dsh-architecture.md`.
  ADR-034, ADR-047, ADR-048, ADR-054, ADR-059 and ADR-060 are cited by decision or
  section. Code is cited by file at `a3c85ae`. "Decision n" alone is a decision of this
  ADR.

## Context

- **The path is written; the transport is not.** "`aiopsctl plugin enable <row>` | A
  submission to the `reload` queue" (`07:1855`); "`aiopsctl` -> Server process | `plugin
  enable <row>` submits one generation to the reload queue" (`08:446`);
  `reload-watch-files` is "off in server profiles, where `aiopsctl` submits" (`08:240`);
  "file watches, the generation follower, the manager and `aiopsctl` all submit to it"
  (`00:490`); every operation of the manager is "a submission to the reload queue"
  (`08:369`); enabling a row whose code is present is live (`08:190`). ADR-060 decision 48
  takes this path for S2a and decision 47 builds `enable`, `disable` and `remove` at S2a.
- **Nothing carries the submission before S6a.** `admin-api` arrives at S4a with bindings
  and platform users only; plugin operations reach it at S6a, performed by
  `plugin-manager` (`03:145`; `08:697`, `:723`). From S6a "every plugin operation goes
  through `plugin-manager`", over `kernel-api` with "an OIDC subject or service token"
  (`02:208`; `03:71`).
- **The `reload` key.** It is consumer-bound, class `C` (`00:221`;
  `sdk/def/reload/v0/reload.go`), so only a row the distribution lists as its consumer
  resolves it, and the release manifest lists none yet: "those that submit are added with
  their plugin" (`dist/dist.yaml`). `Submit(ctx, op)` returns an operation id at once and
  never waits; `Result(id)` and `Await(ctx, id)` read the outcome; `Op` carries only
  `Source`, a label that is not authenticated (ADR-059 decision 45). The activations of
  the `reload` row share one memory, so ids and final results survive a restart of the
  row until a bounded history evicts them (`plugins/reload/plugin.go`). A queued operation
  is abandoned when the `reload` row unloads, and its submitter submits again (ADR-059
  decision 48).
- **Submitters are rows.** The followers `reload-generation` and `reload-watch-files` are
  rows that consume `reload` (`03:75`), as `plugin-manager` is (`03:71`). In dsh the
  reload service and its submitters, the plugin manager and the config editor, are
  separate packages (dsh-architecture §6, "Reload"), and "one queue serialises reloads and
  manager writes" is carried (`08:79`, row 37). The S1 test row `test-submit` submits on a
  control file and exists in no shipped distribution (ADR-059 decision 9;
  `test/dist/plugins/testsubmit/testsubmit.go`).
- **`aiops-core` runs in `aiopsctl` too.** It holds "the process basics shared by the
  server, the CLI and tests" (`00:275`; `08:271`), and `aiopsctl` is the `cli` profile of
  the same launcher, `aiops-core` plus `aiops-cli` (`00:515`; ADR-060 decision 53). A
  row of `aiops-core` therefore mounts in `aiopsctl` unless the `cli` profile disables it.
  No row of `aiops-core` imports an upstream module (ADR-054 decision 23).
- **The home.** `run/` is the one directory the kernel writes: `status.json` and the files
  of the test rows (ADR-047 decision 13; ADR-059 decision 31). One user, the site user,
  owns the home; `aiopsctl` creates the home and `run/` with mode 0700; the kernel runs
  with the uid of the home's owner, as a binary or as a container whose `run/` is bound
  read-write at the same path (ADR-047 decisions 14 and 15). Decision 14 there already
  names "the socket in `run/` that ADR-060 decision 48 names as the candidate". At
  `a3c85ae` `internal/serve` still creates `run/` with mode 0755
  (`internal/serve/serve.go`); ADR-047 changes it to 0700. `status.json` carries the
  phase (`starting`, `running`, `stopping`, `stopped`) and the profile (`boot/run.go`).
- **The server runs on Linux** (`02:294`), as a binary or from the release images.
- **Plugin vet has no rule on listeners.** It refuses a bare `go`, `time.AfterFunc`,
  `context.AfterFunc`, `WaitGroup.Go`, the default mux and an `http.Server` without a
  `Handler` (`tools/kernelcheck/vet.go`; `07:471`; `08:1258`). ADR-059 decision 52
  revisits the `Handler` rule "at S3 if the `http-server` provider must assemble its
  server in steps or another row needs a listener", and `vet.go` notes that at S3
  "serving may be confined to the provider of the http key". Inbound traffic otherwise
  enters through `http-server`, behind an `identities` verifier and an `authz` check
  (`03:109`). Separately, plugin vet "also refuses a dial or an HTTP client outside
  `dialer`" (`07:1400`), "for every plugin" (harness:58); `vet.go` does not build that
  rule at `a3c85ae`.
- **No kernel change is planned** for S2a (ADR-060 decision 3). A tracked goroutine is
  awaited at unload and must return when its context is cancelled (`07:201`), "so a
  server's accept loop stops at quiesce" (ADR-034, "2026-11-20 findings from the restart
  gate on kernel-owned resources").

## Decision

### The row

1. **A new row `reload-socket` of `aiops-core` carries the submission** (question Q1). Its
   plugin is `aiops/reload-socket` under `plugins/reload-socket`. It provides nothing and
   injects `reload`, as a bound consumer, and `cmdline`, for the `home` flag
   (`--home` or `$AIOPS_HOME`, `internal/serve/serve.go`). The release manifest and the
   test manifests list `aiops/reload-socket` among the consumers of `reload`
   (`dist/dist.yaml`; ADR-060 decision 28). Its bundle row states the roles of the
   `reload` row it serves (`08:156`). It is not required: it is in no required set and its
   bundle row does not raise `required` (`08:146`), so a failure of the row leaves the
   server, `reload` and the required set as they are, and `aiopsctl` reports that nothing
   answered (decision 9). It has no config: every path and bound below is fixed. It
   imports the standard library, `sdk/kernel`, `sdk/def/launch/v0` and
   `sdk/def/reload/v0` only, so ADR-054 decision 23 holds, and adds no dependency, so the
   licence rule of ADR-060 decision 34 adds nothing. The edit of `00:275` is in "Edits on
   acceptance".
2. **Where it runs.** Every server profile that lists `aiops-core` runs the row, `dev`
   included, which keeps to `aiops-core` at S2a (ADR-060 decisions 27 and 48). The `aiops-cli` bundle disables it
   with a bundle-layer patch (`enabled: false`, `08:145`), so `aiopsctl` never listens.
   Under question Q7 (a) the row leaves `aiops-core` at S6a, before `hosted` exists
   (S9b); under Q7 (b) `aiops-hosted` disables it, since in hosted the profile layer is a
   platform table written only through `plugin-manager` (`00:491`).

### The socket and who may open it

3. **Two fixed paths.** The socket is `<home>/run/reload.sock` and its lock file
   `<home>/run/reload.lock`, so `aiopsctl` derives both from the home it writes. A
   socket path holds at most 107 bytes on Linux (108 with its terminating NUL); the row
   fails at apply on a longer one and names the limit, and `aiopsctl` checks the same
   before it connects.
4. **Activation.** At apply, in one effect (`Context.Effect`), the row:
   - checks `<home>/run` with `Lstat`: a directory, not a symlink, owned by the process's
     effective uid, with no group or other permission bit (mode 0700, as ADR-047
     decision 14 makes it), and fails naming the path, the owner and the mode otherwise;
   - opens the lock file with mode 0600 and takes an exclusive, non-blocking `flock`
     (`syscall.Flock`); a lock held elsewhere fails the row with "another process serves
     `<home>`". The operating system drops the lock when the process ends, so a crash
     leaves none;
   - removes a socket file that a process left without running its disposer, only once it
     holds the lock, so it never removes a live socket;
   - listens with `net.ListenUnix("unix", ...)` and sets the socket file to mode 0600;
   - records a disposer that closes the listener, which unlinks the socket file (package
     `net` unlinks a socket it created), and closes the lock file.

   It then runs, with `Context.Go`, one accept loop; one goroutine that closes the
   listener when the activation's context is cancelled, so that `Accept` returns and the
   loop ends at quiesce; and one goroutine per connection, at most eight at a time, the
   ninth getting the `busy` refusal. A `Context.Go` that the kernel refuses because the
   row is unloading is recovered and ends the loop, as `plugins/reload/plugin.go` does.
   A connection goroutine returns nil on every client error, since a tracked goroutine
   that returns an error fails the activation (`sdk/kernel/effect.go`). No connection
   holds a lease: `Submit` never waits, and `Await` returns when the context ends, so a
   drain never waits for a client.
5. **Authentication.** Every check below applies; none replaces another.
   - The directory: only the site user can enter a `run/` of mode 0700 owned by it, and
     the kernel runs as that user on the host or in a container (ADR-047 decisions 14 and
     15).
   - The socket file: mode 0600. Linux requires write permission on a stream socket to
     connect to it. Between the bind and the `chmod` the file has the mode the umask
     leaves, and the 0700 directory is the guard; the row does not change the umask,
     which belongs to the process.
   - Peer credentials: for each accepted connection the row reads `SO_PEERCRED`
     (`syscall.GetsockoptUcred`, standard library) before it reads a request, and serves
     only a peer whose uid equals the process's effective uid. Any other peer, root
     included, gets the `peer` refusal and the connection closes. Root is refused because
     `aiopsctl` refuses a home whose owner is not its effective user (ADR-047 decision
     14), so a root `aiopsctl` cannot have written the files it would submit. Linux
     reports a peer's uid as mapped into the user namespace of the reader: the mappings
     that ADR-047 decision 14 admits (rootful Docker with `--user <uid>:<gid>` of the
     owner, rootless Podman with `--userns=keep-id`) give the same uid on both sides of
     the bind, and a peer with no mapping reads as the overflow uid and is refused.
   - The row serves on Linux only, the platform of the server (`02:294`). On any other
     system it fails at apply with that reason rather than serve with fewer checks; it
     still compiles there, since `aiopsctl` links the same module table (ADR-060
     decision 53).

   `Op.Source` stays a label (ADR-059 decision 45): the peer check decides who may
   submit, and the source says which command did.

### The wire format

6. **JSON lines, version 1** (question Q2). The socket is a stream socket. Each message
   is one JSON object in UTF-8 on one line, ended by a newline. The client sends one
   request; the row answers with one or more reply lines and closes.

   | Request field | Value |
   |---|---|
   | `v` | `1` |
   | `op` | `submit` or `await` |
   | `command` | `submit` only: `plugin enable`, `plugin disable`, `plugin remove` or `plugin install`. The row submits `reload.Op{Source: "aiopsctl " + command}` |
   | `id` | `await` only: the operation id that a `submit` answered |

   A request holds at most 4,096 bytes with its newline. Each reply line is either
   `{"v":1,"result":{...}}` or `{"v":1,"error":{"text":"...","causes":[...]}}`. After a
   `submit` the row writes the result as it stands once `Submit` returned (status
   `queued` or later), then the final result. After an `await` it writes the result as it
   stands and, when that is not final, the final result. An error line is always the last
   line. A final result is one whose status is final (`reload.Status.Done`).
7. **The reply carries the reload `Result`**, field for field, with two renderings:

   | JSON field | From `reload.Result` |
   |---|---|
   | `id`, `source`, `status`, `stage` | `ID`, `Op.Source`, `Status`, `Stage` |
   | `error` | `Err` as `{text, causes}`; absent when nil |
   | `generation`, `digest` | `Generation`; `Digest` as lowercase hex |
   | `previous` | The digest of `Previous` only, in hex. Its rows hold row configs, which the client already has in its files, and `--dump` prints the composition (`08:361`) |
   | `admission` | `{denied: [{row, check, reason}]}` |
   | `plan` | `{from, to, entries: [{id, action, changed, configPaths}], closure}` |
   | `applied` | `[{id, outcome}]` |
   | `report` | `{pass, rows: [{id, outcome, state, detail}]}` |
   | `rollback` | `{plan, applied, report, generation, error}`; absent when nil |

   `causes` is a closed list. From the `reload` definition: `queue-full`, `closed`,
   `nested`, `unknown`, `abandoned`, `verdict`, `denied`, `rollback` and `state-unknown`,
   one for each exported error of `sdk/def/reload/v0/reload.go` that the error matches.
   From the transport: `peer` (decision 5), `busy` (decision 4), `request` (a malformed
   or oversized request, an unknown `op` or `command`, or no request within its
   deadline) and `version` (a `v` the row does not serve; the text names the versions it
   serves). A client decides on `status` and `causes` and prints `text`.
8. **Where the wire format lives.** The request and reply types, the encoder from
   `reload.Result` and the decoder are a file of `sdk/def/reload/v0`, standard library
   only. The row and the client, which is the operations plugin of ADR-060 decision 41,
   cannot import each other (`08:1257`), and a definition is what both may import. The
   types enter the `reload` definition at the version its first `definition.yaml`
   records (ADR-060 decision 7), or one minor above it if they land later. Within
   version 1 a field is added and never changed or removed, and a reader ignores a field
   it does not know.

### Timeouts and what `aiopsctl` does with the reply

9. **When nothing answers** (question Q6). Before it connects, `aiopsctl` reads
   `<home>/run/status.json`. When the file is absent, says `stopped`, or names another
   profile than the one the command changed, no process runs that profile on this home:
   `aiopsctl` does not connect, keeps the files, prints "no process runs `<profile>` on
   `<home>`; the change applies when it starts" and exits 0. Otherwise it connects; while
   the socket is absent or refuses, it tries again every 100 ms for up to 30 s, the sum of
   the load, drain and dispose deadlines of ADR-059 decision 30, which covers a restart
   of the row. When nothing answers in that window it keeps the files, prints
   `restart-required: <socket> did not answer; the change applies at the next start`
   (`00:490`) and exits 1.
10. **Timeouts.** The row reads the request within 5 s of the accept and writes each reply
    line within 5 s. It notices a client that closed its end within 1 s and stops
    awaiting for it; the operation itself goes on in `reload`. The row sets no other bound
    on an await: every reload operation reaches a final status, `abandoned` when the
    `reload` row unloads first (ADR-059 decision 48), and an unload of `reload-socket` ends every
    connection, since it cancels the context they run under. `aiopsctl` waits for the
    final result up to `--wait`, a flag of the four commands, 10 minutes by default. An
    end of file before a final line means that the row restarted, for example because the
    generation changed the row or restarted `reload`, whose dependant it is; `aiopsctl`
    then connects again within the window of decision 9 and sends `await` with the id.
11. **What each outcome does** (question Q5). The four commands take the profile write
    lock (`08:369`) and the snapshot of `08:374` (profile definition, `plugins.lock`,
    profile patch) before they write, and hold the lock until they have handled the
    outcome.

    | Outcome | Files | Exit (ADR-059 decision 29; ADR-060 decision 23) |
    |---|---|---|
    | Final result `passed` | Kept | 0, with the generation and the digest |
    | Final result `rejected`, `rolled back`, `rollback failed` or `abandoned`, or an error line | Restored from the snapshot | 1, with the result: each denial with its check, each failed row, the rollback outcome. With `state-unknown` the text says to restart the process |
    | No process runs the profile (decision 9) | Kept | 0 |
    | A process runs the profile and nothing answers (decision 9) | Kept | 1, `restart-required` |
    | `--wait` elapsed, or the connection was lost beyond the window, after the operation was accepted | Kept, since the verdict is unknown to `aiopsctl` | 1, with the id and the last status |

    `install` of in-process code that answers `rebuild-required` writes nothing and submits
    nothing (ADR-060 decision 23). Run again after `dist use`, it submits like the others.
    While the process still runs the image from before `dist use`, its module table lacks
    the plugin and `reload` rejects the generation whole (ADR-059 decision 15), so install
    restores its snapshot and says to start the image that `dist use` recorded first.

### Plugin vet

12. **A listener only where a list allows it** (question Q3). Plugin vet refuses, in every
    directory it reads, a call that opens a listening socket: the functions `Listen`,
    `ListenPacket`, `ListenTCP`, `ListenUDP`, `ListenIP`, `ListenUnix`, `ListenUnixgram`,
    `ListenMulticastUDP`, `FileListener` and `FilePacketConn` of `net`; the methods
    `Listen` and `ListenPacket` of `net.ListenConfig`, found with the type check that
    `vetMethods` already runs; `ListenAndServe` and `ListenAndServeTLS` of `net/http`, as
    functions and as methods of `http.Server`; and `syscall.Listen`. One list in the vet
    package maps a plugin id under the reserved owner `aiops` to the calls it may make.
    At S2a it holds one entry: `aiops/reload-socket` may call `net.ListenUnix`. Vet reads
    the id from the manifest of the plugin that holds the file. `tools/kernelcheck` and
    `aiopsctl plugin check` use the same list, since ADR-060 decision 23 moves vet into a
    package both share, and a third party cannot carry the `aiops` owner (ADR-048
    decision 14). The row imports no `net/http`, so the `Handler` rule of ADR-059 decision
    52 does not reach it. This answers the "another row needs a listener" part of that
    revisit; the `http-server` part stays at S3, where its entry joins the list with its
    own ADR. Row 36 of the dsh table ("it may provide, listen and mount children",
    `08:78`) is about hook listeners and does not change.
13. **The client's dial** (question Q4). When the dial rule of `07:1400` and harness:58 is
    built, the same list holds its one exception for this transport: the operations
    plugin of ADR-060 decision 41 may call `net.DialUnix`, with the network `unix`, for
    the socket of decision 3. The rule stays as written for every other plugin and for
    every other dial. The OCI client that ADR-060 decision 41 puts in the same plugin
    meets the same written rule; this ADR does not decide that case.

### `admin-api` at S4a and after

14. **The socket bridges to S6a** (question Q7). At S4a nothing changes for the socket:
    `admin-api` carries bindings and platform users (`03:145`; `08:697`), `reload` becomes
    the committer under one PostgreSQL advisory lock (`03:65`; `08:697`), and a
    submission over the socket enters that queue as any other does. Until S6a the socket
    carries only the submissions of the four commands: it writes no file and runs no
    plugin operation of its own. From S5 the verdict of each of its generations reaches
    the audit chain through `composition/applied`, as for every submitter (`00:490`). At
    S6a the plugin operations go through `admin-api` to `plugin-manager`, which audits and
    submits them (`02:208`; `03:71`; `08:723`); the commit that moves the four commands
    there removes `reload-socket` from `aiops-core`, its consumer binding, its vet entries
    and the wire types, and edits `00:275` back. The S6a ADR fixes that request.

### No kernel change

15. **The kernel does not change.** The listener is an effect, its loop and its
    connections are tracked goroutines that return when the context is cancelled, and no
    lease is held (decision 4): `Context.Effect`, `Context.Go` and the activation's
    context already give this (`sdk/kernel/context.go`; `07:201`), and ADR-034 records the
    accept-loop rule. This ADR therefore adds no section to ADR-034, and `sdk/kernel` stays
    at its line count under its cap of 8,200 (ADR-060 decision 3). `boot` does not change
    either; the mode 0700 of `run/` is ADR-047's change to `internal/serve`, and it merges
    no later than this row, whose directory check depends on it.

### The tests

16. **Each part has a test that fails on its base** (ADR-060 decision 32):
    - `sdk/def/reload/v0`: a golden file of every request and reply form of version 1, and
      a test that fails on any change other than an added field, as the handshake test of
      ADR-060 decision 42 does; a result of each status and with each cause encoded and
      decoded again; digests in lowercase hex.
    - `plugins/reload-socket`, on a kernel root with the `reload` fake
      (`sdk/def/reload/v0/fake`): a `submit` answers the result as it stands and then the
      final one, and the fake records the source `aiopsctl plugin enable`; an `await` of a
      final id answers one line; an evicted or unknown id answers `unknown`; a request over
      4,096 bytes, an unknown `op` or `command`, a `v` of 2 and a client that sends nothing
      for 5 s each get their refusal; a ninth concurrent connection gets `busy`; a client
      that closes during its operation frees its place within 1 s; a `run/` of mode 0750,
      or a symlink, fails the row with the reason; a lock held through another open file
      fails the row with "another process serves"; a socket file left behind is replaced;
      a socket path over 107 bytes fails the row; the socket file has mode 0600; after
      dispose the socket file is gone and the lock is free. The uid comparison has a table
      test, and the real path serves the test's own user. The generated cycle test of
      ADR-060 decision 12 proves that no goroutine is left, and the T3 boot of the row runs
      as for every plugin (ADR-060 decision 32, standing rule 2).
    - Peer credentials, in a CI step whose runner has passwordless `sudo`: a client run as
      root enters the directory and is refused with `peer`, and a client run as another
      unprivileged user cannot open the socket. The step fails, and does not skip, when it
      cannot run them.
    - Plugin vet, in `tools/kernelcheck`: each call of decision 12 refused in a plugin;
      `net.ListenUnix` admitted for `aiops/reload-socket` and refused for any other id;
      the `net.ListenConfig` method refused; and, when the dial rule is built,
      `net.DialUnix` admitted only in the operations plugin.
    - On the built binaries of the test distribution: its bundle gains a `reload-socket`
      row and `test/dist/dist.yaml` lists it among the consumers of `reload`. Through the
      built `aiopsctl`: enabling `extra` answers `passed` with the generation that
      `test-state` reports, and the row is ACTIVE; enabling `flaky`, whose first applies
      fail, answers `rolled back` (ADR-059 decision 19), exits 1 and restores the profile
      patch, so a later composition equals the running tree; a generation that changes
      the row-level `inject` of `reload-socket`, which restarts it during the apply
      (`08:189`), ends the connection, and the client awaits the id again and gets
      `passed`; with the row disabled while the process runs, `enable` answers
      `restart-required` and exits 1; with no `status.json`, it exits 0 and changes the
      files only. `test-submit` stays the submitter of the S1 suites (ADR-059 decision 9).
    - The failure matrix and the recomposition suite gain no cell and no case; the 41 S1
      cells keep their outcomes.
    - The dumps of `dev` and `cli` are regenerated in the commit that adds the row: `dev`
      shows it enabled and `cli` disabled.
    - The S2a exit, step C4 (ADR-060 decision 30): `plugin enable` on the clean machine
      reaches the kernel container through `run/` bound at the same path under the user
      mapping of ADR-047 decisions 14 and 15, so the directory, the socket mode and the
      peer check run across the bind. C4 also passes only when the final result that
      `plugin enable` prints is `passed` and names the generation that `test-state`
      reports.

## Consequences

- `aiops-core` gains `reload-socket` from S2a; under question Q7 (a) it leaves at S6a, so
  the row counts at P1 scope (`08:248`) do not change.
- The release manifest and the test manifests list `aiops/reload-socket` among the
  consumers of `reload`; the required sets do not change.
- `sdk/def/reload/v0` gains the wire types and a golden file; no other definition, and no
  kernel, `boot` or `loader` code, changes.
- `run/` holds two more files while the row runs: `reload.sock` and `reload.lock`.
- Plugin vet gains the listener rule of decision 12 with one entry, and, when the dial
  rule of `07:1400` is built, the exception of decision 13.
- The four commands gain `--wait`, and their outcomes follow decision 11.
- No dependency is added: `encoding/json`, `net` and `syscall` are the standard library.
- On acceptance this ADR enters the ADR table of `08` §3; a proposed ADR is not listed
  there.

## Edits on acceptance

Made in the commit that records the owner's answers (`08:996`), for the options the
owner chooses:

- `00:275`: the list of `aiops-core` gains "`reload-socket` (S2a to S6a, ADR-062)" after
  `reload`; the count stays 11, since counts are at P1 scope (`08:248`), and `08:271`
  adds that `reload-socket` serves from S2a to S6a (ADR-062). With question Q7
  (b): "`reload-socket` (S2a, ADR-062)", the count becomes 12, `08:252` becomes 12,
  "about 101 rows, and 106" at `08:248` becomes "about 102 rows, and 107", and "Three of
  its eleven rows" at `08:271` becomes "Four of its twelve rows", naming `reload-socket`.
- `03:75`: the row of the followers gains `reload-socket` (`aiops-core`, S2a to S6a),
  "which submits what `aiopsctl` asks on `<home>/run/reload.sock`".
- `07:1855` (`Enable`) and `08:446`: the submission goes "over `<home>/run/reload.sock`
  until S6a (ADR-062)".
- `07:471`, `07:1400` and `08:1258`: plugin vet refuses a listener outside the list of
  decision 12; with question Q4 (a), `07:1400` and harness:58 name the exception of
  decision 13.
- With question Q5 (a), `08:375`: "one generation, forward; a result other than passed
  restores the snapshot of `08:374`".
- ADR-047 decision 13 (the `run/` row): "`reload.sock` and `reload.lock` of
  `reload-socket` (ADR-062)" replaces "the submission socket if the transport ADR of
  ADR-060 decision 48 takes the candidate"; decision 14 names the socket of ADR-062.
- ADR-059 decision 52: a dated note that ADR-062 decision 12 answers the "another row
  needs a listener" part of its revisit.
- ADR-060 decision 48: a dated note that ADR-062 settles the transport; decision 30, step
  C4, gains the check of the final result of decision 16.
- With question Q7 (b), `00:289` (`aiops-hosted`) gains "`reload-socket` disabled", and
  `02:208` gains the socket as the path on the host.

## Questions for the owner

Each question gives its options, the recommended one, why, and when the answer is needed.

**Q1. Which row serves the socket?** ADR-060 decision 48 names two candidates: the `reload`
row itself, or a row of `aiops-core` beside it that is a bound consumer of `reload`, which
edits `00:275`.
- (a) A new row `reload-socket` of `aiops-core` (decisions 1 and 2), bound consumer of
  `reload`, not required, disabled by `aiops-cli`. `00:275` gains it.
- (b) The `reload` row listens when a config field `socket: true` says so, which the server
  profiles set and `aiops-cli` does not. `00:275` does not change. A socket failure then
  fails a row of every required set (`dist/dist.yaml`, `requiredRows`), so a too-long home
  path or a `run/` of the wrong mode stops boot or fails readiness, and a restart of the
  socket is a restart of the queue.
- Recommended: (a). It is the dsh shape, where the reload service and its submitters are
  separate packages (dsh-architecture §6), and the shape of the followers of `03:75`; a
  failure of the transport leaves the queue and the required set alone; and under Q7 (a)
  the row leaves whole at S6a. Its cost is one row and the edit of `00:275`. Needed
  before the row merges.

**Q2. What goes over the socket?** The reply is the reload `Result`, and `Op` carries no
content (ADR-059 decision 48), so a request is small.
- (a) JSON lines on a stream socket, version 1, with `submit` and `await` and the reply
  forms of decisions 6 and 7; the types in `sdk/def/reload/v0` (decision 8).
- (b) HTTP/1.1 with JSON bodies over the same socket. The row then serves with `net/http`
  under the `Handler` rule of ADR-059 decision 52, and the client is "an HTTP client
  outside `dialer`" (`07:1400`).
- (c) gRPC over the socket. It brings the protobuf and gRPC modules into a row of
  `aiops-core`, which ADR-054 decision 23 refuses.
- Recommended: (a). It needs the standard library only, two request kinds cover a submit
  and a reconnect, and the golden test freezes it. Needed before the wire types merge.

**Q3. What does plugin vet say about a listener?** Vet has no listener rule (Context), and
the revisit of ADR-059 decision 52 names "another row needs a listener" as its trigger.
- (a) Vet refuses every call that opens a listener except those a list in the vet package
  allows per plugin id under the reserved owner `aiops`: at S2a `net.ListenUnix` for
  `aiops/reload-socket` (decision 12). `http-server` joins with its own ADR at S3.
- (b) A comment `//kernelcheck:allow-listen <reason>` beside the call, counted and
  printed as an exemption, as `//kernelcheck:allow-go` is in `boot` (`vet.go`), admitted
  in first-party directories only.
- (c) No listener rule: the row uses no `net/http`, and the existing rules cover its
  goroutines.
- Recommended: (a). A listener is a way into the process past the `identities` and
  `authz` checks of `http-server` (`03:109`); (a) makes every one a reviewed list entry
  that `plugin check` also enforces for third parties, while (b) lets any first-party
  file add one with a comment and (c) lets any plugin open one. Needed before the row
  merges.

**Q4. How does the client's dial meet the dial rule?** `07:1400` and harness:58 have
plugin vet refuse "a dial or an HTTP client outside `dialer`" for every plugin. The client
of this socket is in the operations plugin of ADR-060 decision 41, which dials
`<home>/run/reload.sock`. The `dialer` key fences network egress after DNS resolution
(`03:110`), and its row is in `aiops-base`, which the `cli` profile does not list
(`08:254`).
- (a) One named exception in the list of decision 12: the operations plugin may call
  `net.DialUnix` for this socket; the rule stays as written for every other plugin and
  dial (decision 13). `07:1400` and harness:58 name the exception.
- (b) Read "dial" in `07:1400` and harness:58 as a dial of a network address (`tcp`,
  `udp`, `ip`): a Unix socket in the site's home reaches no network, so no exception is
  needed, and both lines are edited to "no network dial".
- Recommended: (a). It leaves the written rule whole and names the one departure, where
  (b) narrows the rule for every plugin. Needed before the client merges.

**Q5. What happens to the files when the generation fails?** `08:374` restores a snapshot
on a failure of `install`, `remove` and `purge`; `08:375` says of `enable` and `disable`
only "one generation, forward". dsh carries "snapshot and restore" (`08:76`, row 34) and
"an invalid live patch is rejected and the running tree kept" (`08:79`, row 37).
- (a) All four commands restore the snapshot of `08:374` when the final result is not
  `passed` or the row refused the submission, so the files on disk match the tree that
  runs (decision 11). `08:375` gains that sentence.
- (b) `install` and `remove` restore, as written; `enable` and `disable` keep the
  departure they wrote and report the result.
- Recommended: (a). Under (b) the next start boots a departure that the verdict refused,
  and the boot rules differ from the live ones (ADR-059 decision 15), so the failure
  shows up again in another form. Needed before `enable` merges.

**Q6. What does a command answer when nothing answers on the socket?** `00:490` says
"without the `reload` row every change answers `restart-required`". ADR-060 decision 23
exits 1 for a denial, a failed step and `rebuild-required`. The install paths of
`08:395-397`, and steps C2 and C3 of the S2a exit, run before the kernel starts (ADR-047
decision 15).
- (a) `aiopsctl` reads `status.json` (decision 9): no process on that profile exits 0 and
  says the change applies when it starts; a process on that profile that does not answer
  is `restart-required` and exits 1.
- (b) Exit 0 with a printed line in both cases.
- (c) Exit 1 with `restart-required` in both cases; C2 and C3 then expect exit 1 from a
  complete install.
- Recommended: (a). A script then sees exit 1 exactly when a running process does not run
  what the files say, and an install before the first start exits 0. A process that
  crashed without writing `stopped` reads as running, and `restart-required` is then
  true. Needed before the exit test is written.

**Q7. How does the socket relate to `admin-api`?** At S4a `admin-api` carries bindings and
platform users only (`03:145`; `08:697`). From S6a "every plugin operation goes through
`plugin-manager`" over `kernel-api` with "an OIDC subject or service token" (`02:208`),
while `00:490` lists `aiopsctl` among the submitters.
- (a) The socket bridges to S6a (decision 14): S4a changes nothing for it, and at S6a the
  four commands move to `admin-api` and `plugin-manager` and `reload-socket` leaves
  `aiops-core`.
- (b) The socket stays after S6a as the path on the host to `plugin-manager`, which then
  audits and submits what arrives on it, with the peer check standing in for OIDC on the
  host; `admin-api` serves remote callers. `02:208` gains the socket, and `aiops-hosted`
  disables the row.
- (c) The socket keeps carrying bare submissions beside `admin-api` after S6a. Plugin
  operations from the host then bypass `plugin-manager`, against `02:208`; listed only to
  be refused.
- Recommended: (a). It is the path that `02:208`, `03:71` and `03:145` write, and it adds
  no way to authenticate a plugin operation beside the one written; (b) adds one, which
  the owner may prefer for a single host with no identity provider, at the cost of an
  edit of `02:208`. Needed before the row merges, since (a) and (b) edit `00:275`
  differently.
