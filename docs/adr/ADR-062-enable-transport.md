# ADR-062: The enable transport: how `aiopsctl` submits a generation before `admin-api`

- Status: **accepted 2026-11-23 by the project owner.** The owner answered all nine
  questions on the same date with the recommended option of each; the section "Owner
  answers of 2026-11-23" records them as decisions 19 to 27. The commit that records the
  answers makes the edits of "Edits on acceptance" for the chosen options,
  `00-decisions.md` among them (`08:997`), as the section of the answers lists.
- Scope: the transport that ADR-060 decision 48 (question Q12, answer (c)) leaves open:
  how `aiopsctl plugin enable`, `disable`, `remove` and `install` submit one generation to
  the running process's reload queue before `admin-api` exists. Decision 48 asks for the
  transport, its plugin vet rule and how it relates to `admin-api` at S4a, written in an
  ADR before the S2a exit test. This ADR also settles the row that serves the transport
  and the edit of `00:275` that a new row needs, the wire format, the authentication by
  socket permissions and peer credentials, the timeouts, what the reply carries, what
  `aiopsctl` does with it, what the transport depends on, and the tests.
- Citations: `00`, `02`, `03`, `07` and `08` are the files of `docs/architecture/` with
  those numbers, and `08:446` is line 446 of `08` at commit `a3c85ae`. ADR-060 cites lines
  at `491a518`; of the lines it cites for decision 48, only `07:1848` moved, and it is
  `07:1855` here. ADR-047 and ADR-060 cite the rule on edits of `00` as `08:996` at their
  own commits; at `a3c85ae` it is `08:997`. "harness" is
  `docs/design/harness-and-upstreams-2026-11.md` and "plugin record" is
  `docs/design/plugin-system-2026-11.md`, cited the same way, and `dsh-architecture` §n is
  section n of `docs/research/dsh-architecture.md`. ADR-034, ADR-047, ADR-048, ADR-054,
  ADR-059 and ADR-060 are cited by decision or section. Code is cited by file at
  `a3c85ae`, except where group 1 of S2a changed it on the branch `s2a/integration` at
  `502a355`, which the citation says: `sdk/plugin`, `internal/plugingen`,
  `sdk/testkit/cycle`, `sdk/def/reload/v0/definition.yaml` and the `reload` fake.
  "Decision n" alone is a decision of this ADR.

## Context

- **The path is written; the transport is not.** "`aiopsctl plugin enable <row>` | A
  submission to the `reload` queue" (`07:1855`); "`aiopsctl` -> Server process | `plugin
  enable <row>` submits one generation to the reload queue" (`08:446`);
  `reload-watch-files` is "off in server profiles, where `aiopsctl` submits" (`08:240`);
  "file watches, the generation follower, the manager and `aiopsctl` all submit to it"
  (`00:490`); every operation of the manager is "a submission to the reload queue"
  (`08:369`); enabling a row whose code is present is live (`08:190`). ADR-060 decision 48
  takes this path for S2a and decision 47 builds `enable`, `disable` and `remove` at S2a,
  and `purge` at S3 with `durable`.
- **Nothing carries the submission before S6a.** `admin-api` arrives at S4a with bindings
  and platform users only; plugin operations reach it at S6a, performed by
  `plugin-manager` (`03:145`; `08:697`, `:723`). From S6a "every plugin operation goes
  through `plugin-manager`", over `kernel-api` with "an OIDC subject or service token"
  (`02:208`; `03:71`).
- **The `reload` key.** It is consumer-bound, class `C` (`00:221`;
  `sdk/def/reload/v0/reload.go`), so only a row the distribution lists as its consumer
  resolves it (`07:559`), and the release manifest lists none yet: "those that submit are
  added with their plugin" (`dist/dist.yaml`). `Submit(ctx, op)` returns an operation id
  at once and never waits; `Result(id)` and `Await(ctx, id)` read the outcome; `Op`
  carries only `Source`, a label that is not authenticated (ADR-059 decision 45). The
  activations of the `reload` row share one memory, so ids and final results survive a
  restart of the row until a bounded history evicts them, and an unknown or evicted id
  answers `ErrUnknown` (`plugins/reload/plugin.go`; `reload.go`). They do not survive a
  replaced process (`08:195`). A queued operation is abandoned when the `reload` row
  unloads, and its submitter submits again; an abandoned operation whose generation was
  applied carries `ErrStateUnknown` (ADR-059 decision 48; `reload.go`, `Abandoned`). At
  `502a355` the definition records version 0.1 (`sdk/def/reload/v0/definition.yaml`), and
  `Await` of its fake returns the scripted result at once (`sdk/def/reload/v0/fake`).
- **Submitters are rows.** The followers `reload-generation` and `reload-watch-files` are
  rows that consume `reload` (`03:75`), as `plugin-manager` is (`03:71`). In dsh the
  reload service and its submitters, the plugin manager and the config editor, are
  separate packages (dsh-architecture §6, "Reload"), and "one queue serialises reloads and
  manager writes" is carried (`08:79`, row 37). Outside callers reach dsh's services
  through RPC controller rows behind `dsh-api-gateway` and `dsh-api-remote-access`
  (dsh-architecture §6, "RPC to the browser"); the manager "cannot disable itself"
  (`08:81`, row 39); and remove deselects, unloads and only then removes the package
  (dsh-architecture §4.4). The S1 test row `test-submit` submits on a control file and
  exists in no shipped distribution (ADR-059 decision 9;
  `test/dist/plugins/testsubmit/testsubmit.go`).
- **`aiops-core` runs in `aiopsctl` too.** It holds "the process basics shared by the
  server, the CLI and tests" (`00:275`; `08:271`), and `aiopsctl` is the `cli` profile of
  the same launcher, `aiops-core` plus `aiops-cli` (`00:515`; ADR-060 decision 53). A
  row of `aiops-core` therefore mounts in `aiopsctl` unless the `cli` profile disables it.
  No row of `aiops-core` imports an upstream module (ADR-054 decision 23). The profile
  that `plugin smoke` boots is the release's `aiops-core` bundle plus the plugin's bundle,
  and smoke passes when every row is ACTIVE or says what it waits for (ADR-060 decision
  58, reading 1; `07:1578`), the rule `dev` must meet at the release (`08:738`).
- **The home.** `run/` is the one directory the kernel writes: `status.json` and the files
  of the test rows (ADR-047 decision 13; ADR-059 decision 31). One user, the site user,
  owns the home; `aiopsctl` creates the home and `run/` with mode 0700; the kernel runs
  with the uid of the home's owner, as a binary or as a container whose `run/` is bound
  read-write at the same path (ADR-047 decisions 14 and 15). Decision 14 there already
  names "the socket in `run/` that ADR-060 decision 48 names as the candidate". At
  `a3c85ae` `internal/serve` still creates `run/` with mode 0755
  (`internal/serve/serve.go`); ADR-047 changes it to 0700. `internal/serve` writes
  `status.json` from `boot.ProcessStatus`, which carries the phase (`starting`,
  `running`, `stopping`, `stopped`) and the profile (`boot/run.go`). ADR-059 decision 31
  makes the file the readiness channel of one process at S1, and the probe an
  orchestrator reads "is decided when the `http` definition exists".
- **The deployment views run the server on Linux**: one Linux VM (`02:294`), or a
  container from the release image under the home contract of ADR-047 decision 15. No
  document names another server platform.
- **Plugin vet has no rule on listeners.** It refuses a bare `go`, `time.AfterFunc`,
  `context.AfterFunc`, `WaitGroup.Go`, the default mux and an `http.Server` without a
  `Handler` (`tools/kernelcheck/vet.go`; `07:471`; `08:1258`), and reads no `_test.go`
  file. ADR-059 decision 52 revisits the `Handler` rule "at S3 if the `http-server`
  provider must assemble its server in steps or another row needs a listener", and
  `vet.go` notes that at S3 "serving may be confined to the provider of the http key".
  Inbound traffic otherwise enters through `http-server`, behind an `identities` verifier
  and an `authz` check (`03:109`). Separately, plugin vet "also refuses a dial or an HTTP
  client outside `dialer`" (`07:1400`), "for every plugin" (harness:58); `vet.go` does not
  build that rule at `a3c85ae`.
- **The generated cycle test feeds a row its config and nothing else.** `zz_cycle_test.go`
  mounts the fake of each injected definition and the row with the config of
  `testdata/cycle.yaml` (ADR-060 decision 12; `internal/plugingen/gentests.go` at
  `502a355`). The launch fake's `Flags` start empty, so a row that reads `--home` gets
  none there, and `cycle.Run` fails a round in which the row is not ACTIVE
  (`sdk/testkit/cycle/cycle.go` at `502a355`). `test-submit` takes its directory from an
  optional config field `dir` for that reason (`testsubmit.go`).
- **No kernel change is planned** for S2a (ADR-060 decision 3). A tracked goroutine is
  awaited at unload and must return when its context is cancelled (`07:201`), "so a
  server's accept loop stops at quiesce" (ADR-034, "2026-11-20 findings from the restart
  gate on kernel-owned resources"). It holds the activation's lease until it returns, and
  the unload waits for that lease (`goTracked`, `sdk/kernel/effect.go`).

## Decision

### The row

1. **A new row `reload-socket` of `aiops-core` carries the submission** (question Q1). Its
   plugin is `aiops/reload-socket` under `plugins/reload-socket`, defined with
   `plugin.Define` and a manifest that `plugingen` generates, as every plugin under
   `plugins/` is from S2a (ADR-060 decisions 4, 11 and 44; ADR-048). It provides nothing
   and injects `reload`, as a bound consumer, and `cmdline`, for the `home` flag
   (`--home` or `$AIOPS_HOME`, `internal/serve/serve.go`). The release manifest and the
   test manifests list `aiops/reload-socket` among the consumers of `reload`
   (`dist/dist.yaml`; ADR-060 decision 28). Its bundle row states the roles of the
   `reload` row it serves (`08:156`) and sets no config. Its config has one optional
   field, `dir`, the directory of decision 3, `<home>/run` by default; it exists so that
   the generated cycle test can mount the row (decision 18), as the `dir` of
   `test-submit` does. `aiopsctl` derives the paths from the home it writes and never
   reads `dir`, so a layer that sets `dir` elsewhere takes the socket out of its reach,
   and the command answers as decision 9 says. Every bound below is fixed. The row
   imports the standard library, `sdk/kernel`, `sdk/plugin`, `sdk/def/launch/v0` and
   `sdk/def/reload/v0` only, so ADR-054 decision 23 holds, and adds no dependency, so the
   licence rule of ADR-060 decision 34 adds nothing. The edit of `00:275` is in "Edits on
   acceptance".

   It is not required: it is in no required set and its bundle row does not raise
   `required` (`08:146`). A failure of the row therefore leaves `reload`, the required set
   and the readiness of the server as they are, and `aiopsctl` reports that nothing
   answered (decision 9). It is still a failed row wherever a boot rule asks every row to
   be ACTIVE or to say what it waits for: the boot of `dev` (`08:738`) and `plugin smoke`
   (ADR-060 decision 58, reading 1). In `dev` the home that `aiopsctl` creates meets
   decisions 3 to 5; smoke is decision 2 and question Q9.
2. **Where it runs.** Every server profile that lists `aiops-core` runs the row, `dev`
   included, which keeps to `aiops-core` at S2a (ADR-060 decisions 27 and 48). The
   `aiops-cli` bundle disables it with a bundle-layer patch (`enabled: false`, `08:145`),
   so `aiopsctl` never listens. In the profile that `plugin smoke` boots, where nothing
   submits, the row is as question Q9 decides. Under question Q7 (a) the row leaves
   `aiops-core` at S6a, before a self-host profile may run several processes on one home
   (S4b, `08:241`) and before `hosted` exists (S9b). Under Q7 (b) `aiops-hosted` disables
   it, since in hosted the profile layer is a platform table written only through
   `plugin-manager` (`00:491`); and from S4b, where several processes may run on one home,
   the lock of decision 4 lets the first serve the socket and fails the row in the others,
   so ADR-038 states which process serves it. Note of 2026-11-26 (review round over the
   whole step): until S4b one server process runs on a home. `internal/serve` takes the
   exclusive `flock(2)` of `<home>/run/process.lock` before it writes `status.json`, and
   a second process on the home exits 1 naming that file and writes no status, which
   would otherwise make `aiopsctl` read the first process as stopped or as running
   another profile. S4b, which lets several processes share a home, replaces the rule.
   Note of 2026-11-27 (round 2): a later layer can enable the row in the profile `cli`,
   the site layer among them, so the patch of `aiops-cli` alone does not keep `aiopsctl`
   from listening. The row applies with no effect in a process whose launcher fills the
   working directory of `cmdline`, which only that of `aiopsctl` does (launch 0.2), and
   `plugin enable` refuses the row in the profile `cli`.

### The socket and who may open it

3. **Two fixed paths.** The socket is `<dir>/reload.sock` and its lock file
   `<dir>/reload.lock`, where `<dir>` is `<home>/run` unless the row's config sets `dir`
   (decision 1); `aiopsctl` derives both from the home it writes. A socket path holds at
   most 107 bytes on Linux (108 with its terminating NUL); the row fails at apply on a
   longer one and names the limit, and `aiopsctl` checks the same before it connects.
4. **Activation.** At apply, in one effect (`Context.Effect`), the row:
   - checks `<dir>` with `Lstat`: a directory, not a symlink, owned by the process's
     effective uid, with no group or other permission bit (mode 0700, as ADR-047
     decision 14 makes `run/`), and fails naming the path, the owner and the mode
     otherwise;
   - opens the lock file with mode 0600 and takes an exclusive, non-blocking `flock`
     (`syscall.Flock`); a lock held elsewhere fails the row with "another process serves
     `<home>`". The operating system drops the lock when the process ends, so a crash
     leaves none;
   - removes a socket file that a process left without running its disposer, only once it
     holds the lock, so it never removes a live socket;
   - listens with `net.ListenUnix("unix", ...)` and sets the socket file to mode 0600;
   - records a disposer that closes the listener, which unlinks the socket file (package
     `net` unlinks a socket it created), treats `net.ErrClosed` from a listener already
     closed as success, and then closes the lock file, so the lock is free only once the
     socket file is gone.

   It then runs, with `Context.Go`, one accept loop; one goroutine that, when the
   activation's context (`Context.Std`) is cancelled, closes the listener, so that
   `Accept` returns and the loop ends, and closes every open connection; and the
   goroutines that serve each connection, at most eight connections at a time, the ninth
   getting the `busy` refusal. A `Context.Go` that the kernel refuses because the row is
   unloading is recovered and ends the loop, as `plugins/reload/plugin.go` does. A
   goroutine of a connection returns nil on every client error, since a tracked goroutine
   that returns an error fails the activation (`sdk/kernel/effect.go`).

   Each of these goroutines holds the activation's lease until it returns, and the unload
   waits for it (`goTracked`, `sdk/kernel/effect.go`). So every blocking call of a
   connection ends when the context is cancelled: `Await` runs under a context derived
   from `Context.Std`, and a read or a write ends because the closing goroutine closes the
   connection. An unload therefore waits for no client, and no deadline of decision 10
   delays it, whatever drain deadline a site sets with `--limits`.
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
   - The row serves on Linux only. That is this ADR's choice: the deployment views run
     the server on Linux (Context), and `SO_PEERCRED` is Linux's. On any other system the
     row fails at apply with that reason rather than serve with fewer checks; it still
     compiles there, since `aiopsctl` links the same module table (ADR-060 decision 53).

   `Op.Source` stays a label (ADR-059 decision 45): the peer check decides who may
   submit, and the source says which command did.

   **What the checks leave.** The boundary is the site user, not `aiopsctl`: any process
   with the site's uid can open the socket. That includes in-process code of the kernel
   until plugin vet builds the dial rule (decision 14), and from S2b a remote plugin
   instance or a sidecar that the operator starts as the site user. The consumer binding
   of `reload` still holds, since only `reload-socket` resolves the key (`07:559`), but
   such a process causes a submission without being listed. That adds nothing it cannot
   already do: it can run `aiopsctl` itself, and it can write every file of the home,
   which the site user owns (ADR-047 decision 14). An `Op` carries no content (ADR-059
   decision 48), so a submission composes only what those files say, and in-process code
   is trusted (`08:93`, row 51). An operator keeps a remote instance on the host from the
   home, and from the socket, by running it as another user.

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

   Within version 1 a field, an `op` value or a `command` value is added and never
   changed or removed. A reader ignores a field it does not know, and a row answers an
   `op` or a `command` it does not serve with `request`, whose text names those it
   serves. The golden test of decision 18 holds every form that version 1 has had.
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
   or oversized request, an `op` or `command` the row does not serve, or no request
   within its deadline) and `version` (a `v` the row does not serve; the text names the
   versions it serves). A client decides on `status` and `causes` and prints `text`.
8. **Where the wire format lives.** The request and reply types, the encoder from
   `reload.Result` and the decoder are a file of `sdk/def/reload/v0`, standard library
   only. The row and the client, which is the operations plugin of ADR-060 decision 41,
   cannot import each other (`08:1257`), and a definition is what both may import. The
   same file holds a type for the two fields of `<home>/run/status.json` that the client
   reads (decision 9), `phase` and `profile`; the client imports no package of `boot` or
   `internal/`. A test of `internal/serve`, which writes the file, decodes each status it
   writes into that type and fails when a field is missing or a value differs.

   At `502a355` the `reload` definition records version 0.1, so the wire types raise it
   to 0.2 (ADR-060 decision 7), and standing rule 4 of ADR-060 decision 32 applies: the
   same pull request fixes every plugin under `plugins/` that the raise touches, its
   generated `requires` included. The fake of the definition gains a scripted result that
   stays as it stands until the test makes it final, and an `Await` that blocks until
   then or until its context ends; the tests of decision 18 need both, and the fake at
   `502a355` answers at once.

### Timeouts and what `aiopsctl` does with the reply

9. **When nothing answers** (question Q6). Before it connects, `aiopsctl` reads
   `<home>/run/status.json` (decision 8). When the file is absent, says `stopped`, or
   names another profile than the one the command changed, no process runs that profile
   on this home: `aiopsctl` does not connect, keeps the files, prints "no process runs
   `<profile>` on `<home>`; the change applies when it starts" and exits 0. Otherwise it
   connects; while the socket is absent or refuses, it tries again every 100 ms within a
   window that `--connect-wait`, a flag of the four commands, sets, 60 s by default. The
   default is the load, drain and dispose deadlines of both `reload` and `reload-socket`
   at the defaults of ADR-059 decision 30: a restart of `reload` unloads its dependant
   first and loads it last. The client cannot read the limits of the process, which
   ADR-059 decision 30 revisits at S3, so a site whose `--limits` raises those deadlines
   raises the flag. When nothing answers in the window it keeps the files, prints
   `restart-required: <socket> did not answer; the change applies at the next start`
   (`00:490`) and exits 1. Note of 2026-11-27 (review round over the whole step, round
   2): a server that was killed leaves a `status.json` that says it runs. So `aiopsctl`
   first tries a shared, non-blocking `flock(2)` of `run/process.lock`, which a server
   holds for its life (decision 2), and releases it at once; where it takes it, no
   process runs and the outcome is not-running, exit 0, as for a status that says
   `stopped`; a server that starts in that instant tries its own lock again for a
   moment. Skipping the window where the profile composes no enabled `reload-socket`
   row was not taken: the render of the home after the write does not show what the
   running process composed, and the window still ends.
10. **Timeouts and a second submission.** The row reads the request within 5 s of the
    accept and writes each reply line within 5 s. It notices a client that closed its end
    within 1 s and stops awaiting for it; the operation itself goes on in `reload`. The
    row sets no other bound on an await: every reload operation reaches a final status,
    `abandoned` when the `reload` row unloads first (ADR-059 decision 48), and an unload
    of `reload-socket` ends every connection (decision 4). `aiopsctl` waits for the final
    result up to `--wait`, a flag of the four commands, 10 minutes by default.

    An end of file before a final line means that the row restarted, for example because
    the generation changed the row or restarted `reload`, whose dependant it is.
    `aiopsctl` then connects again within the window of decision 9 and:
    - with the id that a `submit` answered, sends `await` with that id;
    - with no id, because the end of file came before the first reply line, sends
      `submit` once more.

    It also submits once more when an `await` answers `unknown`, which happens after the
    process was replaced or the bounded history evicted the id (Context), and when the
    final result is `abandoned` without `state-unknown`, which the `reload` definition
    gives an operation whose generation was not applied: one that never touched the tree,
    whose submitter submits again (ADR-059 decision 48). A second submission is safe: an
    `Op` carries no content, so it composes the files as they then stand, and when the
    first operation ran after all, the second judges the tree that the first left. A
    command submits once more at most once; decision 11 says what a second outcome with no
    final result does.
11. **What each outcome does** (question Q5). The four commands take the profile write
    lock (`08:369`) and the snapshot of `08:374` (profile definition, `plugins.lock`,
    profile patch) before they write, and hold the lock until they have handled the
    outcome.

    | Outcome | Files | Exit (ADR-059 decision 29; ADR-060 decision 23) |
    |---|---|---|
    | Final result `passed` that shows the change (below) | Kept | 0, with the generation and the digest |
    | Final result `passed` that does not show the change | Kept, since the tree runs the composition the files give | 1, naming the row and that a later layer, the site layer `<home>/aiops.patch.yml` or a `--patch` file of the process, overrides the change |
    | Final result `rejected`, `rolled back` or `rollback failed`, or `abandoned` with `state-unknown` | Restored from the snapshot | 1, with the result: each denial with its check, each failed row, the rollback outcome. With `state-unknown` the text says to restart the process, which then boots the restored files (`08:244`) |
    | An error line before an id exists: `peer`, `busy`, `request` or `version`, or a refusal of `Submit` (`queue-full`, `closed`, `nested`, `state-unknown`) | Restored from the snapshot | 1, with the text; with `state-unknown`, the restart text |
    | No final result: an end of file before the first reply line, `unknown` on an `await`, or `abandoned` without `state-unknown` | As they are until the second submission of decision 10, whose outcome this table handles | As for that outcome |
    | No final result on the second submission either | Restored after `abandoned` without `state-unknown`, since the tree was not touched; kept otherwise, since the verdict is unknown to `aiopsctl` | 1, with each id and the last status |
    | No process runs the profile (decision 9) | Kept | 0 |
    | A process runs the profile and nothing answers (decision 9) | Kept | 1, `restart-required` |
    | `--wait` elapsed, or the connection was lost beyond the window of decision 9, after the operation was accepted | Kept, since the verdict is unknown to `aiopsctl` | 1, with the id and the last status |

    A `passed` result shows the change when its `report` holds the row of `enable <row>`
    as ACTIVE, and holds neither the row of `disable <row>` nor a row under the group of
    the artifact that `remove` removed. The judge reports every mounted row that was
    ACTIVE before the generation or that the generation touched
    (`plugins/reload/internal/engine/engine.go`, `judge`), so a second submission that
    changes nothing still shows it. `install` enables no row, and a pass shows its change.
    `enable` and `disable` write layer 2 (`08:375`) and a later layer wins (`08:72`, row
    30), so a pass without the change is a pass of the composition that the files give,
    not of the command.

    `remove` deletes `bundles/<plugin id>/` and `content/<plugin id>/`, when no other
    profile names the id (ADR-047 decision 10), only after a final result that shows the
    change, or when no process runs the profile (decision 9). A result that restores the
    snapshot then finds both directories, so it never leaves a lock entry without its
    files, which ADR-047 decision 10 requires; and the rows of the artifact unload before
    their files go, as dsh deselects, unloads and only then removes the package
    (dsh-architecture §4.4). Where the verdict stays unknown the directories stay, and a
    later `install` handles a leftover as ADR-047 decision 10 says.

    `install` of in-process code that answers `rebuild-required` writes nothing and
    submits nothing (ADR-060 decision 23). Run again after `dist use`, it submits like the
    others. While the process still runs the image from before `dist use`, its module
    table lacks the plugin and `reload` rejects the generation whole (ADR-059 decision
    15), so install restores its snapshot and says to start the image that `dist use`
    recorded first.
12. **The transport's own rows.** As `plugin-manager` "cannot disable itself, a required
    row" (`08:369`; dsh row 39, `08:81`), and the operations library is the one it shares
    (ADR-060 decision 23), the library refuses `disable reload-socket`, which would end
    the connection that carries the command and leave no socket to answer, and `disable`
    of a required row, `reload` among them (`00:492`). Each refusal writes nothing, exits 1
    and says to edit the profile layer and restart the process. `enable reload-socket`
    while the row is disabled finds no socket and answers `restart-required` (decision 9),
    which is true. `remove` takes the id of an installed artifact (ADR-047 decision 10), so
    it reaches neither row.

### Plugin vet

13. **A listener only where a list allows it** (question Q3). Plugin vet refuses, in every
    directory it reads, a call that opens a listening socket or binds a socket to an
    address. The rule covers these packages and calls:
    - `net`: the functions `Listen`, `ListenPacket`, `ListenTCP`, `ListenUDP`, `ListenIP`,
      `ListenUnix`, `ListenUnixgram`, `ListenMulticastUDP`, `FileListener` and
      `FilePacketConn`, and the methods `Listen` and `ListenPacket` of
      `net.ListenConfig`;
    - `crypto/tls`: the function `Listen`;
    - `net/http`: `ListenAndServe` and `ListenAndServeTLS`, as functions and as methods of
      `http.Server`;
    - `net/http/httptest`: the functions `NewServer`, `NewTLSServer` and
      `NewUnstartedServer`, and the methods `Start` and `StartTLS` of `httptest.Server`;
    - `syscall` and `golang.org/x/sys/unix`: the functions `Listen` and `Bind`.

    Methods are found with the type check that `vetMethods` already runs. Vet reads no
    `_test.go` file, as for every rule, so a test may start an `httptest` server. One
    list in the vet package maps a plugin id under the reserved owner `aiops` to the calls
    it may make. At S2a it holds one entry: `aiops/reload-socket` may call
    `net.ListenUnix`. Vet reads the id from the generated manifest, `plugin.yaml`, of the
    plugin that holds the file (ADR-048; ADR-060 decisions 11 and 44); a file with no
    manifest in its plugin's directory matches no entry, so every call of the rule there
    is refused. `tools/kernelcheck` and `aiopsctl plugin check` use the same list, since
    ADR-060 decision 23 moves vet into a package both share, and a third party cannot
    carry the `aiops` owner (ADR-048 decision 14). The row imports no `net/http`, so the
    `Handler` rule of ADR-059 decision 52 does not reach it. This answers the "another row
    needs a listener" part of that revisit; the `http-server` part stays at S3, where its
    entry joins the list with its own ADR. Row 36 of the dsh table ("it may provide,
    listen and mount children", `08:78`) is about hook listeners and does not change.
14. **The client's dial** (question Q4). When the dial rule of `07:1400` and harness:58 is
    built, the same list holds its one exception for this transport: the operations
    plugin of ADR-060 decision 41 may call `net.DialUnix`, with the network `unix`, for
    the socket of decision 3. The rule stays as written for every other plugin and for
    every other dial. The OCI client that ADR-060 decision 41 puts in the same plugin
    meets the same written rule; this ADR does not decide that case.

### `admin-api` at S4a and after

15. **The socket bridges to S6a** (question Q7). At S4a nothing changes for the socket:
    `admin-api` carries bindings and platform users (`03:145`; `08:697`), `reload` becomes
    the committer under one PostgreSQL advisory lock (`03:65`; `08:697`), and a
    submission over the socket enters that queue as any other does. Until S6a the socket
    carries only the submissions of the four commands: it writes no file and runs no
    plugin operation of its own, and `purge` is decision 16. From S5 the verdict of each
    of its generations reaches the audit chain through `composition/applied`, as for every
    submitter (`00:490`). At S6a the plugin operations go through `admin-api` to
    `plugin-manager`, which audits and submits them (`02:208`; `03:71`; `08:723`); the
    commit that moves the four commands there removes `reload-socket` from `aiops-core`,
    its consumer binding, its vet entries and the wire types, and edits `00:275` back. The
    S6a ADR fixes that request.

### What the transport depends on

16. **Dependencies and the order of merges.**
    - ADR-047's mode 0700 of `run/` in `internal/serve` merges no later than the row,
      whose directory check depends on it.
    - The wire types, the status type and the fake of decision 8 merge, with the raise of
      `reload` to 0.2, before the row.
    - The client depends on two pieces that ADR-060 leaves to other work: the ADR of
      ADR-060 decision 53, which proposes the `commands` definition and how command rows
      reach the operations library, and the definition under which the operations plugin
      serves that library (ADR-060 decision 41). That ADR is asked to give the methods of
      the definition for `enable`, `disable`, `remove` and `install` the bounds of
      decisions 9 and 10 as fields of their requests, and to have them return the outcome
      of decision 11: the final result as the wire carries it, or which row of that table
      without a final result applied, with the id and the last status. The transport, from
      the read of `status.json` to the second submission, is internal to the operations
      plugin. The command rows declare `--wait` and `--connect-wait` through `commands`
      and pass them on. The client merges after both, and the four commands reach a
      running process only from then.
    - `status.json` keeps its `phase` and `profile` fields until S6a. The ADR that decides
      the orchestrator's probe when the `http` definition exists (ADR-059 decision 31; S3)
      keeps them, or names what replaces them for this client, in the same change.
    - `purge` arrives at S3 with `durable` (ADR-060 decision 47) and drops a plugin's
      durable namespaces through `durablePurge` (`08:374`), a consumer-bound key served in
      the kernel process (`03:74`; `07:559`). Version 1 carries no such operation, and
      decision 15 keeps the socket to submissions until S6a. The ADR of S3 that builds
      `purge` states how it reaches `durablePurge` before S6a: for example an `op` added
      to version 1 by the rule of decision 6, served by a row that the distribution lists
      among the consumers of `durablePurge`, which edits decision 15; or `purge` waits for
      `plugin-manager` at S6a, which edits ADR-060 decision 47 and needs the owner.

### No kernel change

17. **The kernel does not change.** The listener is an effect; the accept loop, the
    closing goroutine and the goroutines of each connection are tracked goroutines whose
    every blocking call ends when the activation's context is cancelled (decision 4).
    Like every tracked goroutine they hold the activation's lease while they run, and the
    unload waits for them, bounded by that cancellation and not by a deadline of decision
    10. `Context.Effect`, `Context.Go` and `Context.Std` already give this
    (`sdk/kernel/context.go`; `sdk/kernel/effect.go`; `07:201`), and ADR-034 records the
    accept-loop rule. This ADR therefore adds no section to ADR-034, and `sdk/kernel`
    stays at its line count under its cap of 8,200 (ADR-060 decision 3). `boot` does not
    change either; the mode 0700 of `run/` is ADR-047's change to `internal/serve`
    (decision 16).

### The tests

18. **Each part has a test that fails on its base** (ADR-060 decision 32):
    - `sdk/def/reload/v0`: a golden file of every request and reply form of version 1, and
      a test that fails on any change other than an addition, as the handshake test of
      ADR-060 decision 42 does; a result of each status and with each cause encoded and
      decoded again; digests in lowercase hex. In `internal/serve`, each status it writes
      decodes into the status type of decision 8.
    - `plugins/reload-socket`, in hand-written tests on a kernel root with the `reload`
      fake of decision 8 and a temporary directory of mode 0700 as `dir`: a `submit`
      answers the result as it stands and then the final one, and the fake records the
      source `aiopsctl plugin enable`; an `await` of a final id answers one line; an
      evicted or unknown id answers `unknown`; a request over 4,096 bytes, an unknown
      `op` or `command`, a `v` of 2 and a client that sends nothing for 5 s each get their
      refusal; a ninth concurrent connection gets `busy`; a client that closes during its
      operation frees its place within 1 s; a `run/` of mode 0750, or a symlink, fails the
      row with the reason; a lock held through another open file fails the row with
      "another process serves"; a socket file left behind is replaced; a socket path over
      107 bytes fails the row; the socket file has mode 0600; after dispose the socket
      file is gone, the lock is free and the disposer reported no error; an unload while
      one connection awaits a non-final id and another has sent nothing ends within 1 s,
      under a drain deadline of 1 s set for the test. The uid comparison has a table test,
      and the real path serves the test's own user.
    - The generated tests: `testdata/cycle.yaml` sets `dir: testdata/run`, and the
      package's `TestMain` creates that directory with mode 0700 before the tests and
      removes it after them, since Git records no directory mode. The generated cycle test
      of ADR-060 decision 12 then mounts, disposes and remounts the row and proves that no
      goroutine is left, with no change to `plugingen` or `sdk/testkit/cycle`, and the T3
      boot of the row runs as for every plugin (ADR-060 decision 32, standing rule 2). On a
      system other than Linux, `TestMain` prints that the row serves on Linux only and
      runs no test; CI runs on Linux.
    - Peer credentials, in a step of the `test` job of `ci.yml`, whose runner has
      passwordless `sudo`, mirrored by a Makefile target that `make check` does not run,
      since it needs that `sudo`: a client run as root enters the directory and is
      refused with `peer`, and a client run as another unprivileged user cannot open the
      socket. The step fails, and does not skip, when it cannot run them.
    - Plugin vet, in `tools/kernelcheck`: each call of decision 13 refused in a plugin,
      `crypto/tls.Listen`, `httptest.NewServer` in a file that is not a test and
      `unix.Bind` among them; `httptest.NewServer` in a `_test.go` file not read;
      `net.ListenUnix` admitted for `aiops/reload-socket`, refused for any other id and
      refused in a directory with no manifest; the `net.ListenConfig` method refused; and,
      when the dial rule is built, `net.DialUnix` admitted only in the operations plugin.
    - The client, in the operations plugin, against a server in the test that speaks
      version 1 and scripts its lines: an end of file before the first reply line submits
      once more, and the second result decides; an end of file after the id awaits that
      id; `unknown` on that await submits once more; `abandoned` without `state-unknown`
      submits once more, and a second such result restores the snapshot and exits 1;
      `abandoned` with `state-unknown` restores and prints the restart text; each refusal
      before an id restores; a second outcome with no final result after an end of file
      keeps the files and exits 1 with both ids; a `passed` result without the target row
      keeps the files and exits 1 naming the override; `disable reload-socket` and
      `disable reload` write nothing and exit 1; a `remove` whose result is `rolled back`
      leaves the lock, the bundle list and both directories as they were, and one that
      passes deletes both directories after the result.
    - On the built binaries of the test distribution: its bundle gains a `reload-socket`
      row and `test/dist/dist.yaml` lists it among the consumers of `reload`. Through the
      built `aiopsctl`: enabling `extra` answers `passed` with the generation that
      `test-state` reports, and the row is ACTIVE; enabling `flaky`, whose first applies
      fail, answers `rolled back` (ADR-059 decision 19), exits 1 and restores the profile
      patch, so a later composition equals the running tree; a generation that changes
      the row-level `inject` of `reload-socket`, which restarts it during the apply
      (`08:189`), ends the connection, and the client awaits the id again and gets
      `passed`; a generation that changes the `history` of `reload`, which restarts
      `reload` and then its dependant `reload-socket`, answers `passed`, from the line
      before the end of file or by awaiting the id once the client connects again; with
      a site layer that disables `extra`, `enable extra` answers `passed` without the
      row, exits 1 and keeps the files; with the row disabled while the process runs,
      `enable` answers `restart-required` and exits 1; with no `status.json`, it exits 0
      and changes the files only. `test-submit` stays the submitter of the S1 suites
      (ADR-059 decision 9). Note of 2026-11-26, as built:
      `TestEnableOutcomesOnARunningProcess` of `cmd/aiopsctl`, which `make s1-suites`
      runs, holds each case of this bullet, with `extra` and `flaky` in a bundle of the
      home's `dev` and the test binary running as the `aiopsctl` of the test distribution
      in a process of its own; the change of `inject` and of `history` are each asserted
      by the rows that test-state reports the loader restarted in that generation
      (`reload-socket`; `reload`), and the restart of the dependant `reload-socket` after
      the change of `history`, which the kernel makes through its epoch (`08:189`) and
      the loader does not list, by the new socket file that its next activation listens
      on; the case of a disabled `reload-socket` runs on a second process started with a
      patch that disables it. The client case of disabling
      `reload-socket` and `reload` is `TestDisableOfTheRowsOfTheSubmission` of the
      operations plugin.
    - `plugin smoke`: with question Q9 (a), the composition that smoke boots holds
      `reload-socket` disabled by the patch and every other row of `aiops-core` ACTIVE;
      with (b), smoke under the runtime of the exit's clean machine finds the row ACTIVE.
    - The failure matrix and the recomposition suite gain no cell and no case; the 41 S1
      cells keep their outcomes.
    - The dumps of `dev` and `cli` are regenerated in the commit that adds the row: `dev`
      shows it enabled and `cli` disabled.
    - The S2a exit, step C4 (ADR-060 decision 30): `plugin enable` on the clean machine
      reaches the kernel container through `run/` bound at the same path under the user
      mapping of ADR-047 decisions 14 and 15, so the directory, the socket mode and the
      peer check run across the bind. C4 also passes only when the final result that
      `plugin enable` prints is `passed`, shows the change as decision 11 says, and names
      the generation that `test-state` reports.

## Consequences

- `aiops-core` gains `reload-socket` from S2a; under question Q7 (a) it leaves at S6a, so
  the row counts at P1 scope (`08:248`) do not change.
- The release manifest and the test manifests list `aiops/reload-socket` among the
  consumers of `reload`; the required sets do not change.
- `sdk/def/reload/v0` gains the wire types, the status type and a golden file, and its
  fake a result that stays non-final and an `Await` that blocks; `reload` goes to 0.2,
  and standing rule 4 of ADR-060 decision 32 applies. No other definition, and no kernel,
  `boot`, `loader`, `plugingen` or `sdk/testkit` code, changes; `internal/serve` gains a
  test.
- `run/` holds two more files while the row runs: `reload.sock` and `reload.lock`.
- Plugin vet gains the listener rule of decision 13 with one entry, and, when the dial
  rule of `07:1400` is built, the exception of decision 14.
- The four commands gain `--wait` and `--connect-wait`; their outcomes follow decision 11,
  with at most one second submission; `remove` deletes the artifact's directories only
  after a result that shows the change; the library refuses the disables of decision 12.
- The client waits on the ADR of ADR-060 decision 53 and on the operations definition;
  `status.json` keeps its two fields until S6a; `purge` needs the S3 ADR (decision 16).
- In `dev`, once `aiops-dev` enables `reload-watch-files` (`08:240`; `00:291`), the
  watcher sees a command's write too, and one change makes two submissions. Both compose
  the files as they stand, so the later one judges the tree the earlier left; `aiopsctl`
  reports its own, and after a restore the watcher submits the restored files. The result
  is the same as with one submitter, at the cost of a second generation.
- No dependency is added: `encoding/json`, `net` and `syscall` are the standard library.
- On acceptance this ADR enters the ADR table of `08` §3; a proposed ADR is not listed
  there.

## Edits on acceptance

Made in the commit that records the owner's answers (`08:997`), for the options the
owner chooses:

- `00:275`: the list of `aiops-core` gains "`reload-socket` (S2a to S6a, ADR-062)" after
  `reload`; the count stays 11, since counts are at P1 scope (`08:248`), and `08:271`
  adds that `reload-socket` serves from S2a to S6a (ADR-062). With question Q7
  (b): "`reload-socket` (S2a, ADR-062)", the count becomes 12, `08:252` becomes 12, the
  rows that `self-host-full` composes, 101 and 106 with the Slack bundle, become 102 and
  107 at both `00:271` and `08:248`, and "Three of its eleven rows" at `08:271` becomes
  "Four of its twelve rows", naming `reload-socket`.
- `02` §2.3, the relations after `02:207`: a row "`aiopsctl` → `kernel` | A stream Unix
  socket, `<home>/run/reload.sock`; the mode 0700 of `run/`, the mode 0600 of the socket
  and the peer's uid | `plugin enable`, `disable`, `remove` and `install` submit one
  generation to the reload queue, S2a to S6a (ADR-062)". With question Q7 (a) the
  figure `02-plugin-distribution` draws the hosted containers and the path from S6a and
  does not change, and the text under it (`02:185`) names the socket among what it leaves
  out. With Q7 (b) the row reads "from S2a", `02:208` gains the socket as the path on the
  host, and the figure gains the edge `aiopsctl` → `kernel`, is rendered again and is
  checked as rendered against the element count stated under it (`02:64-66`).
- `03:75`: the row of the followers gains `reload-socket` (`aiops-core`, S2a to S6a),
  "which submits what `aiopsctl` asks on `<home>/run/reload.sock`". `03` §9, the row of
  `03:551`, gains `reload-socket` with every role, marked S2a to S6a under Q7 (a).
- `07:1855` (`Enable`) and `08:446`: the submission goes "over `<home>/run/reload.sock`
  until S6a (ADR-062)".
- `07:471`, `07:1400` and `08:1258`: plugin vet refuses a listener outside the list of
  decision 13; with question Q4 (a), `07:1400` and harness:58 name the exception of
  decision 14.
- With question Q5 (a), `08:375`: "one generation, forward; a result that refuses or fails
  the generation restores the snapshot of `08:374` (ADR-062 decision 11)".
- With question Q8 (a), the table of `08` §1.1 and plugin record §15 gain row 64, and
  their counts read 64 rows and 9 added (`08:32-37`; plugin record §15).
- With question Q9 (a), `07:1578`: smoke boots with a `--patch` file that disables
  `reload-socket` (ADR-062).
- ADR-047 decision 10: a dated note that `remove` deletes the directories only after a
  result that shows the change (ADR-062 decision 11). Decision 13 (the `run/` row):
  "`reload.sock` and `reload.lock` of `reload-socket` (ADR-062)" replaces "the submission
  socket if the transport ADR of ADR-060 decision 48 takes the candidate"; decision 14
  names the socket of ADR-062.
- ADR-059 decision 52: a dated note that ADR-062 decision 13 answers the "another row
  needs a listener" part of its revisit.
- ADR-060 decision 48: a dated note that ADR-062 settles the transport; decision 30, step
  C4, gains the check of the final result of decision 18; decision 36 gains the
  peer-credential step of decision 18 in the `test` job.
- With question Q7 (b), `00:289` (`aiops-hosted`) gains "`reload-socket` disabled".

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
- **Answered 2026-11-23: (a)**, decision 19.

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
- **Answered 2026-11-23: (a)**, decision 20.

**Q3. What does plugin vet say about a listener?** Vet has no listener rule (Context), and
the revisit of ADR-059 decision 52 names "another row needs a listener" as its trigger.
- (a) Vet refuses every call of decision 13 that opens a listener or binds a socket,
  except those a list in the vet package allows per plugin id under the reserved owner
  `aiops`: at S2a `net.ListenUnix` for `aiops/reload-socket`. `http-server` joins with
  its own ADR at S3.
- (b) A comment `//kernelcheck:allow-listen <reason>` beside the call, counted and
  printed as an exemption, as `//kernelcheck:allow-go` is in `boot` (`vet.go`), admitted
  in first-party directories only.
- (c) No listener rule: the row uses no `net/http`, and the existing rules cover its
  goroutines.
- Recommended: (a). A listener is a way into the process past the `identities` and
  `authz` checks of `http-server` (`03:109`); (a) makes every listener that plugin code
  opens through the packages of decision 13 a reviewed list entry, which `plugin check`
  also enforces for third parties, while (b) lets any first-party file add one with a
  comment and (c) lets any plugin open one. Needed before the row merges.
- **Answered 2026-11-23: (a)**, decision 21.

**Q4. How does the client's dial meet the dial rule?** `07:1400` and harness:58 have
plugin vet refuse "a dial or an HTTP client outside `dialer`" for every plugin. The client
of this socket is in the operations plugin of ADR-060 decision 41, which dials
`<home>/run/reload.sock`. The `dialer` key fences network egress after DNS resolution
(`03:110`), and its row is in `aiops-base`, which the `cli` profile does not list
(`08:254`).
- (a) One named exception in the list of decision 13: the operations plugin may call
  `net.DialUnix` for this socket; the rule stays as written for every other plugin and
  dial (decision 14). `07:1400` and harness:58 name the exception.
- (b) Read "dial" in `07:1400` and harness:58 as a dial of a network address (`tcp`,
  `udp`, `ip`): a Unix socket in the site's home reaches no network, so no exception is
  needed, and both lines are edited to "no network dial".
- Recommended: (a). It leaves the written rule whole and names the one departure, where
  (b) narrows the rule for every plugin. Needed before the client merges.
- **Answered 2026-11-23: (a)**, decision 22.

**Q5. What happens to the files when the generation fails?** `08:374` restores a snapshot
on a failure of `install`, `remove` and `purge`; `08:375` says of `enable` and `disable`
only "one generation, forward". dsh carries "snapshot and restore" (`08:76`, row 34) and
"an invalid live patch is rejected and the running tree kept" (`08:79`, row 37).
- (a) All four commands restore the snapshot of `08:374` when the row refused the
  submission before it had an id, or the final result is `rejected`, `rolled back`,
  `rollback failed`, or `abandoned` after its generation was applied, so the files on disk
  match the tree that runs. With no final result they submit once more, and where the
  verdict stays unknown they keep the files (decisions 10 and 11). `08:375` gains that
  sentence.
- (b) `install` and `remove` restore, as written; `enable` and `disable` keep the
  departure they wrote and report the result.
- Recommended: (a). Under (b) the next start boots a departure that the verdict refused,
  and the boot rules differ from the live ones (ADR-059 decision 15), so the failure
  shows up again in another form. Needed before `enable` merges.
- **Answered 2026-11-23: (a)**, decision 23.

**Q6. What does a command answer when nothing answers on the socket?** `00:490` says
"without the `reload` row every change answers `restart-required`". ADR-060 decision 23
exits 1 for a denial, a failed step and `rebuild-required`. Steps C2 and C3 of the S2a
exit install before the kernel starts, since step C4 starts it on the home of step C3
(ADR-047 decision 15). The install paths of `08:395-397` also run while a process runs,
which decision 9 handles by connecting.
- (a) `aiopsctl` reads `status.json` (decision 9): no process on that profile exits 0 and
  says the change applies when it starts; a process on that profile that does not answer
  within `--connect-wait` is `restart-required` and exits 1.
- (b) Exit 0 with a printed line in both cases.
- (c) Exit 1 with `restart-required` in both cases; C2 and C3 then expect exit 1 from a
  complete install.
- Recommended: (a). A script then sees exit 1 exactly when a running process does not run
  what the files say, and an install before the first start exits 0. A process that
  crashed without writing `stopped` reads as running, and `restart-required` is then
  true. Needed before the exit test is written.
- **Answered 2026-11-23: (a)**, decision 24.

**Q7. How does the socket relate to `admin-api`?** At S4a `admin-api` carries bindings and
platform users only (`03:145`; `08:697`). From S6a "every plugin operation goes through
`plugin-manager`" over `kernel-api` with "an OIDC subject or service token" (`02:208`),
while `00:490` lists `aiopsctl` among the submitters.
- (a) The socket bridges to S6a (decision 15): S4a changes nothing for it, and at S6a the
  four commands move to `admin-api` and `plugin-manager` and `reload-socket` leaves
  `aiops-core`.
- (b) The socket stays after S6a as the path on the host to `plugin-manager`, which then
  audits and submits what arrives on it, with the peer check standing in for OIDC on the
  host; `admin-api` serves remote callers. `02:208` gains the socket, `aiops-hosted`
  disables the row, and ADR-038 states which process serves it where several run on one
  home (decision 2).
- (c) The socket keeps carrying bare submissions beside `admin-api` after S6a. Plugin
  operations from the host then bypass `plugin-manager`, against `02:208`; listed only to
  be refused.
- Recommended: (a). It is the path that `02:208`, `03:71` and `03:145` write, and it adds
  no way to authenticate a plugin operation beside the one written; (b) adds one, which
  the owner may prefer for a single host with no identity provider, at the cost of an
  edit of `02:208` and a rule for several processes. Needed before the row merges, since
  (a) and (b) edit `00:275` differently.
- **Answered 2026-11-23: (a)**, decision 25.

**Q8. How does the dsh table record the socket?** The table of `08` §1.1, numbered as in
plugin record §15, is how the owner checks the plan against dsh (`08:30`), and "no
adaptation is argued from schedule" (`08:39`). In dsh, outside callers reach the services
through RPC controller rows behind `dsh-api-gateway` and `dsh-api-remote-access`
(dsh-architecture §6), the shape that `admin-api` and `plugin-manager` take at S6a
(`02:208`). The socket is a second inbound path outside `http-server` (`03:109`), forced by
ADR-060 decision 48, which brings a live enable to S2a, before `admin-api` carries plugin
operations.
- (a) Row 64, status added: "A host-local path for `aiopsctl`: the row `reload-socket`
  serves the `reload` queue on a Unix socket in `run/`, checked by the directory, the
  socket mode and the peer's uid", forced by ADR-060 decision 48; from S2a to S6a under
  Q7 (a), from S2a under Q7 (b). The counts become 64 rows and 9 added.
- (b) Row 64, status adapted, under the dsh property of RPC controller rows: the socket is
  one such row with another transport. Under Q7 (a) it is argued from schedule, which
  `08:39` rules out, so (b) also needs that sentence edited.
- (c) No row: under Q7 (a) the socket leaves at S6a, and no profile of the first
  community release composes it.
- Recommended: (a). A listener outside `http-server` belongs in the table while it
  exists, since the owner checks the plan there; "added" claims no dsh property, so no
  adaptation is argued from schedule, and the row says what forces it. Needed before the
  row merges.
- **Answered 2026-11-23: (a)**, decision 26.

**Q9. What does `plugin smoke` do with `reload-socket`?** At S2a smoke boots the release's
`aiops-core` bundle and the plugin's bundle with its rows enabled, and passes when every
row is ACTIVE or says what it waits for (ADR-060 decision 58, reading 1; `07:1578`). It
runs on the author's machine with a container runtime (`07:1532`). `reload-socket` fails at
apply when `run/` is not mode 0700 and owned by the process's user, or when the bind of
`run/` cannot hold a Unix socket (decisions 4 and 5), and nothing submits in smoke.
- (a) Smoke boots with a `--patch` file that disables `reload-socket`. The profile still
  lists `aiops-core`, every other row is judged as written, and `07:1578` names the patch.
- (b) Smoke keeps the row enabled, and its home and container meet decisions 3 to 5:
  smoke creates the home with mode 0700 and runs the container with the user of ADR-047
  decision 14, and a test shows the row ACTIVE under the runtime of the exit's clean
  machine. Under another runtime or mode, such as rootless Podman without
  `--userns=keep-id`, or a bind that cannot hold a socket, smoke then fails on a row the
  author did not write.
- Recommended: (a). The row serves only `aiopsctl` on a server and smoke has no
  submitter, so (a) keeps a smoke result about the plugin under test, while the row's own
  cycle test and T3 boot still run (decision 18). (b) keeps reading 1 to the letter at
  the cost of failures that depend on the author's runtime. Needed before smoke merges.
- **Answered 2026-11-23: (a)**, decision 27.

## Owner answers of 2026-11-23

The project owner accepted this ADR on 2026-11-23 and answered every question of the
section "Questions for the owner" on the same date, each with its recommended option.
Each decision below names the question it answers. Where a decision above leaves a point
to a question, the decision here settles it.

19. **A new row `reload-socket` of `aiops-core` serves the socket** (Q1 (a)). The row of
    decisions 1 and 2 is a bound consumer of `reload`, is in no required set and is
    disabled by the `aiops-cli` bundle. `00:275` gains it.

20. **JSON lines, version 1, go over the socket** (Q2 (a)). The socket is a stream socket
    that carries the requests `submit` and `await` and the reply forms of decisions 6 and
    7, and the wire types are a file of `sdk/def/reload/v0` (decision 8).

21. **Plugin vet refuses a listener outside a list of plugin ids** (Q3 (a)). Vet refuses
    every call of decision 13 that opens a listener or binds a socket, except those that a
    list in the vet package allows per plugin id under the reserved owner `aiops`. At S2a
    the list holds one entry, `net.ListenUnix` for `aiops/reload-socket`; `http-server`
    joins it with its own ADR at S3. `07:471`, `07:1400` and `08:1258` state the rule.

22. **The client's dial is one named exception to the dial rule** (Q4 (a)). When the dial
    rule of `07:1400` and harness:58 is built, the operations plugin of ADR-060 decision
    41 may call `net.DialUnix`, with the network `unix`, for the socket of decision 3; the
    rule stays as written for every other plugin and every other dial (decision 14).
    `07:1400` and harness:58 name the exception.

23. **All four commands restore the snapshot when the generation fails** (Q5 (a)).
    `enable`, `disable`, `remove` and `install` restore the snapshot of `08:374` when the
    row refused the submission before it had an id, or when the final result is
    `rejected`, `rolled back`, `rollback failed`, or `abandoned` after its generation was
    applied, so the files on disk match the tree that runs. With no final result they
    submit once more, and where the verdict stays unknown they keep the files (decisions
    10 and 11). `08:375` gains that sentence.

24. **`status.json` decides what a command answers when nothing answers on the socket**
    (Q6 (a)). When no process runs the command's profile on the home, `aiopsctl` keeps the
    files, says that the change applies when the process starts and exits 0. When a
    process runs that profile and does not answer within `--connect-wait`, the command
    answers `restart-required` and exits 1 (decision 9).

25. **The socket bridges to S6a** (Q7 (a)). S4a changes nothing for the socket. At S6a
    the four commands move to `admin-api` and `plugin-manager`, and the commit that moves
    them removes `reload-socket` from `aiops-core` with its consumer binding, its vet
    entries and the wire types (decision 15). The row is therefore not counted at P1
    scope: the count of `aiops-core` stays 11, and the figure `02-plugin-distribution`
    does not change. What decision 2 and "Edits on acceptance" write for Q7 (b) does not
    apply: `aiops-hosted` does not disable the row, and ADR-038 need not state which
    process serves the socket.

26. **The dsh table records the socket as row 64, status added** (Q8 (a)). Row 64 of
    `08` §1.1 and of plugin record §15 reads "A host-local path for `aiopsctl`: the row
    `reload-socket` serves the `reload` queue on a Unix socket in `run/`, checked by the
    directory, the socket mode and the peer's uid", forced by ADR-060 decision 48, from
    S2a to S6a. The counts read 64 rows and 9 added.

27. **`plugin smoke` boots with a `--patch` file that disables `reload-socket`** (Q9
    (a)). The profile that smoke boots still lists `aiops-core`, every other row is judged
    as written, and `07:1578` names the patch. The row's own cycle test and T3 boot still
    run, and of the smoke test of decision 18 the case for (a) applies.

**Edits made with this record.** `00` is edited in the commit that records the answers
(`08:997`), and that commit makes the edits that "Edits on acceptance" and the chosen
options name: `00:275`; the text under the figure `02-plugin-distribution`
(`02:185-187`), whose list of what is not drawn names the socket, and the relation after
`02:207`; `03:75` and `:551`; `07:471`, `:1400`, `:1578` and `:1855`; `08:271`, `:375`,
`:446` and `:1258`, and row 64 of the table of `08` §1.1 with its counts (`08:32-37`);
harness:58; plugin record §15, its row 64 and its counts; ADR-047 decisions 10, 13 and
14; ADR-059 decision 52; and ADR-060 decisions 30 (step C4), 36 and 48. The same commit
keeps the lines that restate them consistent: the counts of the dsh table at `00:91`;
the lists of vet's rules at `00:297` and in plugin record §14, which restate `08:1258`;
`enable` and `disable` in plugin record §12, which restates `08:375`; and reading 1 of
ADR-060 decision 58, which `07:1578` restates. Left to later commits: the row of this
ADR in the ADR table of `08` §3, and the next free number at `08:997`, to the commit
that brings that table to the ADRs accepted on 2026-11-23; and the lines of ADR-061 that
name the transport ADR, its table of submission outcomes and its item on `tree
--effects`, to the commit that brings ADR-061 to this ADR (decision 16). An option the
owner did not choose edits nothing.
