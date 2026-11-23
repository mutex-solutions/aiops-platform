# ADR-061: The `commands` definition, the `cli` profile and the operations plugin

- Status: **proposed 2026-11-23, awaiting the project owner.** Nothing in this ADR is in
  force. ADR-060 decision 53 has it proposed before the first command row merges, and
  ADR-060 decision 2 has it accepted before that code merges into `main`; each question
  below names the code its answer is needed before.
- Scope: ADR-060 decision 53 (its question Q17 (a)) and decision 41 (its question Q5
  (a)): the `commands` definition; the `cli` row, its dispatcher; the `aiops-cli` bundle
  and the `cli` profile; `cmd/aiopsctl` as the launcher of the `cli` profile, with
  `profile render` as a command row; the definition and the plugin that hold the
  operations library, with the profile write lock (`08:369`; `03:71`); how command rows
  reach the operations library, the builder and the gate; and where `plugingen` runs. It
  also settles the point that ADR-048 decision 20 leaves to it: how the strict reading
  reaches the `plugin check` row (ADR-048 decision 15). The transport by which `aiopsctl`
  submits a generation to a running process is the ADR that ADR-060 decision 48 asks
  for, and is not settled here. This ADR changes no decision of `00` by itself: where an
  answer edits `00`, the question names the lines, and `00` is edited in the commit that
  records the answer (`08:997`).
- Citations: `00`, `02`, `03`, `06`, `07` and `08` are the files of `docs/architecture/`
  with those numbers, and `08:369` is line 369 of `08` at commit `a3c85ae`. "harness" is
  `docs/design/harness-and-upstreams-2026-11.md`, cited the same way. The plugin record,
  `docs/design/plugin-system-2026-11.md`, is cited by section and row, and "dsh" is
  `docs/research/dsh-architecture.md`, cited by section. ADR-020, ADR-044, ADR-047,
  ADR-048, ADR-054, ADR-059 and ADR-060 are cited by decision. Code is cited by file at
  `a3c85ae`, and the group 1 code of S2a by file on `s2a/integration` at `502a355`.

## Context

### What is written

- **Commands are rows.** `aiopsctl` is the `cli` profile of the same launcher and module
  table as `aiops`: commands are rows, and a plugin contributes its own through
  `commands` (`00:137`, `:515`; `02:95`; `08:271`, `:359`, `:1208`; plugin record §2 row
  15). This is dsh's rule: one launcher, and an app differs from another only by the
  bundles it lists and the startup plugin that parses the leftover argv (dsh §7), which
  `08:73` (row 31) carries.
- **The catalogue.** `commands` is a key of class `-` whose first-party provider row is
  `cli`, with no remote contribution (`00:222`). Its consumers are `admin-api`, `cli` and
  every plugin with a command, and its fake records (plugin record §5). `aiops-cli`
  holds `cli` and the rows that contribute to `commands`, and with `aiops-core` it is the
  `cli` profile, which lists the two bundles in that order (`00:276`, `:515`; `08:253`).
- **The launch keys.** `cli` consumes the launch keys (plugin record §5, row `launch`).
  `cmdline` serves argv, `ready` is the readiness latch and `appExit` requests a process
  exit (`00:212`). A request is not an exit and carries no code, because the launcher
  owns signals, bounded shutdown and exit codes (`07:1344`;
  `sdk/def/launch/v0/launch.go`). The launch keys stay six (ADR-059 decision 35), and
  launch services are definitions with fakes, so their consumers mount alone in a test
  (`03:81`).
- **Exit codes.** `aiopsctl` exits 0, 1 or 2, and a denial, a failed step and
  `rebuild-required` exit 1 with the reason on stderr (ADR-059 decision 29; ADR-060
  decision 23). The launcher exits 0 to 7 (`boot/run.go`).
- **The operations library** is the internal package of one first-party plugin that
  provides it under a consumer-bound key of a new definition, with a fake and a suite.
  The command rows consume it from S2a and `plugin-manager` from S6a. It imports the
  signature library, the OCI client and the YAML library, and it is a row of `aiops-cli`
  (ADR-060 decision 41; ADR-020 decision 1). The CLI and the service share it and one
  profile write lock, and every operation is a submission to the reload queue (`08:369`;
  `03:71`; plugin record §12). dsh's plugin manager and CLI share `operations.ts` and a
  profile write lock, and restore a snapshot on a failure (dsh §4.2).
- **Import boundaries.** A plugin imports the SDK and its own dependencies only (`07:34`,
  `:46`; `08:1257`), and `boot` imports the kernel and three definitions (`08:1256`). The
  gate is `boot` code that `plugin-manager` and the launcher call (`08:493`). `reload`
  reaches compose, admit and plan through the launch key `profileContext`, and a plugin
  never imports `boot` (ADR-059 decisions 7 and 35; `03:84`).
- **What `aiopsctl` links beside its module table.** The builder and `plugingen` are
  root-module packages compiled into `aiopsctl` (ADR-060 decisions 11 and 22), and what
  the builder links enters no server binary (ADR-044, Consequences). The import analyzer
  and plugin vet move into a package that `tools/kernelcheck` and `aiopsctl` share, the
  templates are embedded through a Go package, and the suites of a pack run as test
  binaries that `aiopsctl` embeds (ADR-060 decisions 23, 24 and 46). `plugingen` reads
  `plugin.yaml` with `boot`'s reader (`internal/plugingen/plugingen.go`), and ADR-048
  decision 15 asks that the `plugin check` row run that reader, handed to it by the
  launcher of the `cli` profile, and not a second copy.

### Gaps

1. `commands` has no interface: nothing says who parses arguments, prints help or maps
   an error to an exit code.
2. `cmdline` serves no stream and `appExit` no code. No launch service says when boot has
   finished, and before the tree settles a command whose row is still mounting cannot be
   told from an unknown command. The launcher prints its boot line on stdout
   (`boot/run.go`), and the front end of `cmd/aiops` writes the status file of the home
   (`internal/serve/serve.go`), which is the readiness channel of the server (ADR-059
   decision 31).
3. Every command needs `boot`, the builder, `plugingen`, the analyzer, the templates or
   the embedded suites, and no plugin may import any of them.
4. The operations definition has no name and no method set, and the write lock has no
   scope and no form.
5. `profile render` is code of `cmd/aiopsctl` that reads the S1 stand-ins `dist.yaml` and
   `modules.yaml` (`cmd/aiopsctl/run.go`), which S2a removes (ADR-060 decision 14).
6. CI generates a plugin from the template of every registry (`08:1265`), and ADR-060
   decision 24 names templates for `echoers` and `stampers` only.
7. The required set of the signed manifest is written for server profiles (`08:455`), and
   the `cli` profile is not one.
8. Plugin vet is to refuse a dial or an HTTP client outside `dialer` in every plugin
   (`07:1400`; harness:58). The operations plugin reaches OCI registries, and `dialer`
   arrives at S3 (`08:696`) as a row of `aiops-base` (`00:277`), which the `cli` profile
   does not list (`08:253`).

## Decision

### The `commands` definition

1. **`sdk/def/commands/v0`, key `commands`.** A plural key, so a registry of
   contributions (`00:206`, `:656`), of class `-` on both sides and with no remote
   contribution (`00:222`). It holds no tenant data and owns no hook. Its
   `definition.yaml`, in the layout of ADR-060 decision 7:

   ```yaml
   key: commands
   version: "0.1"
   stability: preview
   class: {provider: open, consumer: open}
   remote: "no"
   suite: 1
   ```

   `preview` is the stability of the launcher's and the loader's definitions
   (`sdk/def/launch/v0/definition.yaml`, `sdk/def/loader/v0/definition.yaml`).

2. **The contribution is a command.**

   ```go
   type Command interface {
       Run(ctx context.Context, inv Invocation) error
   }
   ```

   A command is registered with a `Spec`: a path of one or more words, each
   `[a-z][a-z0-9-]*` (`profile render`, `plugin check`); a one-line summary; the names and
   the least and greatest number of its positional arguments; and its flags, each with a
   name, a kind (bool, string, or a list that the flag repeats), a default and a usage
   line. `help` belongs to the dispatcher, and no path starts with it. An `Invocation`
   holds the positional arguments, the flag values by name, the three streams, and the
   home that the launcher resolved (decision 25), empty when there is none. `Run` returns
   `nil` on success, an error that matches `ErrUsage` for a usage error, and any other
   error for a denial, a failed step or `rebuild-required`. It returns when `ctx` ends.

3. **The registry** is what the key serves.

   ```go
   type Registry interface {
       Register(c *kernel.Context, s Spec, cmd Command) error
       Dispatch(ctx context.Context, argv []string, s Streams) (code int, err error)
       Inventory() []string
   }
   ```

   - `Register` takes the caller's context first and records the registration as an
     effect of the caller's fiber, labelled `Label(path)`, so the registration leaves
     when that fiber unloads (`07:519`). It returns `ErrPath` for a path that breaks the
     word rule, is taken, or is a prefix of a registered path or has one as its prefix,
     so that every path names one command; and an error that matches
     `kernel.ErrInactiveEffect` when the caller's fiber is unloading.
   - `Dispatch` parses `argv` by decision 8, prints help by decision 9, calls the command
     inside an `Enter` lease on the fiber that registered it (`07:203`, `:511`), and
     returns the exit code of decision 10. `admin-api`, which plugin record §5 lists as a
     consumer, could call it from S4a; what it may dispatch is S4a's to decide.
   - `Inventory` lists the registered paths, sorted. The owning fiber of each is the
     fiber whose ledger holds `Label(path)`, as for the toy registries (`07:521`; ADR-060
     decision 5).
   - Once the registry is released, `Register` and `Dispatch` return an error that
     matches `kernel.ErrInactive` (`07:520`) and `Inventory` is empty. The provided value
     has no method outside the interface (`07:518`).

4. **The fake** is a recording dispatcher, `sdk/def/commands/v0/fake`: a `kernel.Plugin`
   that provides `commands`, dispatches by the same rules, records each registration and
   each dispatch, and exposes `Inventory()` (`07:496`; plugin record §5; ADR-060
   decision 12). A command row mounts alone with it and the fake of `operations`
   (decision 18).

5. **The suite**, `sdk/def/commands/v0/conformance`, runs on `conformance.Run(t, Mount)`
   (ADR-059 decision 32), with an obligations half for a command and a guard half for a
   dispatcher (`07:514`; ADR-060 decision 5). Each case has an id, which `plugin check`
   prints for a failed obligation (ADR-060 decision 5).
   - Obligations: a registered path keeps the word rule; `Run` writes only to the streams
     of its invocation, returns when `ctx` ends, and returns `ErrUsage` only for a usage
     error.
   - Guard: each parsing and help rule of decisions 8 and 9; each row of the exit table
     of decision 10; a taken or overlapping path refused; a late registration refused
     with `ErrInactiveEffect`; a call into a command whose fiber is quiescing refused with
     `ErrInactive`; every method of a released registry refused; and `Inventory()` back
     at its baseline after a contributor is disposed. The guard half runs against the
     `cli` row and against the fake (`07:514`).

6. **A template.** S2a writes the Go template of `commands` in `templates/`, and `plugin
   new commands --lang go` writes it, because CI generates a plugin from the template of
   every registry and holds it to the state of `07:1551` (`08:1265`). ADR-060 decision 24
   gains it.

### The `cli` row, the dispatcher

7. **`aiops/cli`** lives in `plugins/cli/` and is the row `cli` of `aiops-cli`. The bundle
   marks it required, so a `cli` that is not ACTIVE stops the boot of `aiopsctl` with a
   reason instead of leaving the process waiting (`08:451`). It provides `commands` and
   injects `cmdline`, `ready`, `appExit`, `composition` and `catalog`; `appExit` is
   consumer-bound, so the release manifest lists `aiops/cli` beside
   `aiops/restart-policy` (`00:245`; `dist/dist.yaml`). Its `Apply` provides the registry
   and starts one goroutine with `c.Go` (`07:201`, `:472`). The goroutine waits until
   boot has finished (decision 13), calls `Dispatch` with the argv and the streams of
   `cmdline`, and requests the exit with the code that `Dispatch` returns. It dispatches
   after boot because the tree has then settled: every command row is ACTIVE, or is not
   and will not be without a change, so an unknown path can be told from a path whose row
   is not ACTIVE (decision 10). The `cli` row is the startup plugin that parses the
   leftover argv in dsh §7.

8. **Argument parsing.** The command line is `aiopsctl <word>... [flags and arguments]`.
   The dispatcher takes the longest registered path that `argv` starts with and reads the
   rest against that command's `Spec`.
   - Flags and positional arguments come in any order, as the documents write them:
     `plugin smoke ./ --against <release>` (`07:1578`), `plugin install <spec> --profile
     <p>` (`08:395`), `plugin gate <spec> --against <dist.yaml>` (`08:493`).
   - A flag is `--name value`, `--name=value` or `-name`. A bool flag takes no value
     unless written `--name=false`, a list flag repeats, and `--` ends the flags.
   - An unknown flag, a missing value, or a number of arguments outside the `Spec` is a
     usage error.
   - The parser is standard-library Go inside the `cli` row; no module is added for it.

9. **Help.** `aiopsctl help [<word>...]`, and `--help` or `-h` after any words, print the
   registered commands grouped by their first word with their summaries, or one
   command's synopsis and flags, then the three exit codes, then each row of the
   composition that needs `commands` and is not ACTIVE, with its state and what it waits
   for, read from `composition` and `catalog`. Help that was asked for goes to stdout and
   exits 0. `aiopsctl` with no argument prints the same list to stderr and exits 2, as the
   S1 usage text does (`cmd/aiopsctl/run.go`).

10. **Exit codes** are the three of ADR-059 decision 29 (ADR-060 decision 23).

    | Outcome | Code | stderr |
    |---|---|---|
    | `Run` returned `nil`; help that was asked for | 0 | Warnings only |
    | A usage error: of parsing, an error of `Run` that matches `ErrUsage`, or a path that no row registered while every row that needs `commands` is ACTIVE | 2 | The error and the synopsis |
    | A denial, a failed step, `rebuild-required`, or any other error of `Run` | 1 | `aiopsctl: <path>: <reason>` |
    | A path that no row registered while a row that needs `commands` is not ACTIVE | 1 | That row, its state and what it waits for |
    | A stop by a signal before the command returned | 1 | `interrupted` |
    | The boot of the `cli` profile failed (launcher codes 1 and 3 to 7), or a fiber leaked at shutdown | 1 | The launcher's reason |

    ADR-059 decision 29 records the table in `cmd/aiopsctl/run.go`. From this ADR it is
    in the help text of the `cli` row and in the exit mapping of `cmd/aiopsctl`
    (decision 25).

11. **stdout and stderr.** stdout carries only what a command was asked to print: a dump,
    a report with `--json` (`07:1565`), what `inspect` shows, the `dist.yaml` lines of
    `rebuild-required` (`08:397`), and help that was asked for. stderr carries the rest:
    errors with their reason (ADR-060 decision 23), warnings, progress and the launcher's
    diagnostics. A dump written to a file is then the dump alone, as at S1
    (`cmd/aiopsctl/run.go`).

### The launch definition, minor 0.2

12. **`sdk/def/launch/v0` goes to `0.2` with `compatibleFrom: "0.1"`**, an additive minor
    (`07:534`, `:543`). The six keys and their classes stay (ADR-059 decision 35). The
    launch fake and the launch suite change with it, and `boot` implements it. With
    question Q1 (a), `ProfileContext` gains `Toolbox()` (decision 21).

13. **What the `cli` row reads** (question Q2 (a)):
    - `Cmdline` gains `Stdin()`, `Stdout()` and `Stderr()`: the process's streams in every
      binary, and a test's buffers under the launch fake, so the `cli` row and each
      command row mount alone in a test (`03:81`).
    - `Ready` gains `Booted()`, a channel that the launcher closes once the tree has
      settled and the run-time required audit has passed, the point where `boot` prints
      its boot line today (`boot/run.go`). dsh's launcher marks the app ready when the
      root is ACTIVE and the loader exists (dsh §7; `08:74`, row 32).
    - `ExitRequest` gains `Code`, read only with the new reason `ExitCommand`: the
      launcher stops under its shutdown bound and returns that code, which is 0, 1 or 2.
      Any other code with that reason is a launcher failure.

### The `operations` definition and plugin

14. **Name** (question Q3 (a)): definition `sdk/def/operations/v0`, key `operations`,
    plugin `aiops/operations` in `plugins/operations/`, and row `operations` of
    `aiops-cli` (ADR-060 decision 41). It is the documents' name for the library
    (`08:369`; `07:1846`; plugin record §12) and dsh's (`operations.ts`, dsh §4.2). Keys
    with a plural name for one engine exist: `jobs`, `secrets`, `tokens`, `credentials`
    (`00:216`, `:218`, `:224`, `:226`).

15. **Class `C`**: provider open, consumer bound, as `pluginManager` (`00:222`). The
    release manifest lists as its consumers the five command rows of decision 30 and,
    from S6a, `aiops/plugin-manager` (ADR-060 decision 41), so no contributed tool can
    wrap it, as `08:369` says of `pluginManager`. The consumer-bound keys go from 12 to 13
    (`00:245`; `07:559`). Its `definition.yaml`:

    ```yaml
    key: operations
    version: "0.1"
    stability: preview
    class: {provider: open, consumer: bound}
    remote: "no"
    suite: 1
    ```

16. **Method set.** One method per operation. Each takes `ctx` first and returns an error
    that matches `kernel.ErrInactive` once the provider is released (`07:520`); the
    request and the result of each are types of the definition package. `Render` is
    added to the list of ADR-060 decision 23 so that the operations plugin is the one
    consumer of the toolbox (decision 20), and `Submit` carries every write to the
    reload queue (`08:369`).

    | Method | Command (decision 30) | Writes the home under the lock | Gate entry point (ADR-060 decision 17) | Toolbox parts (decision 21) | Source |
    |---|---|---|---|---|---|
    | `Render` | `profile render` | No | Boot | `Render` | `08:359`; `03:63` |
    | `VerifyProfile` | `profile verify` | No | Boot | `Render` | `08:366` |
    | `InitProfile` | `profile init <p>` | Yes | None | `Shipped` | `08:72` row 30; ADR-060 decision 27 |
    | `New` | `plugin new <registry>`, `plugin new connector` | No; writes the author's directory | None | `Template`, `Release` | `07:1551`; ADR-060 decision 24; ADR-047 decision 12 |
    | `Check` | `plugin check <dir>` | No; with `--write`, the author's directory | `plugin check` step 7 | `ReadManifest`, `Generate`, `Vet`, `Suite`, `Gate`, `Release` | `07:1565`-`1576`; ADR-060 decision 23 |
    | `Smoke` | `plugin smoke <dir> --against <release>` | No | None | `Build` | `07:1578`; ADR-060 decision 58 |
    | `Pack`, `Sign`, `Push` | `plugin pack`, `plugin sign`, `plugin push` | No | `plugin pack` | `Suite`, `Gate` (for `Pack`) | `07:1859`; ADR-060 decision 18; ADR-020 decision 1 |
    | `Inspect` | `plugin inspect <spec>` | No | None | None | `08:373`; ADR-060 decision 58 |
    | `Verify` | `plugin verify <spec>` | No | None | `Suite`, `Build` | ADR-060 decisions 31 and 46 |
    | `Gate` | `plugin gate <spec> --against <dist.yaml>` | No | `plugin gate --against` | `Gate` | `08:493` |
    | `Install` | `plugin install <spec> --profile <p>` | Yes | Both `plugin install` rows | `Gate` | `08:374`, `:395`-`397`; ADR-047 decisions 5 to 10 |
    | `Enable`, `Disable` | `plugin enable <row>`, `plugin disable <row>` | Yes | Boot, on the changed profile | `Render` | `08:375`; ADR-060 decision 47 |
    | `Remove` | `plugin remove <id> --profile <p>` | Yes | None | None | ADR-047 decision 10 |
    | `AddTrust` | `trust add <signer> --ids <pattern>` | Yes | None | None | `07:1861`; ADR-020 decisions 5 and 6 |
    | `GenerateKey` | `key generate <prefix>` | No | None | None | ADR-020 decisions 4 and 21 |
    | `Build` | `dist build` | No | `dist build`, run by the builder | `Build` | `08:402`; ADR-060 decision 22 |
    | `SignDist` | `dist sign` | No | None | None | `07:1868`; ADR-020 decision 1 |
    | `UseDist` | `dist use <image>` | Yes | None | None | `07:1869`; ADR-020 decision 12; ADR-047 decision 13 |
    | `Submit` | After each write that changes a profile | No | The running process's, at its recomposition | None | `08:369`, `:446`; ADR-060 decision 48 |

    `Purge` joins at S3 with `durable` (ADR-060 decision 47).

17. **Rules every provider keeps.**
    - `Install` runs every clause of its entry point before it writes, writes nothing on
      a denial, answers `rebuild-required` for in-process code with no file changed, and
      restores a snapshot of the profile definition, `plugins.lock` and the profile patch
      on a failure (`08:374`, `:397`; ADR-060 decision 23).
    - `Enable` and `Disable` write only a departure from the bundle layers into the
      profile layer, with no `--tenant` (`08:375`; ADR-060 decision 47), and only after
      `Render` has admitted the changed profile as boot would (ADR-060 decision 17, boot
      row), so a change that boot would refuse writes nothing.
    - A write to a profile that the home does not hold first copies the shipped template
      with `InitProfile` and says so (ADR-060 decision 27).
    - Each file is replaced by a rename (ADR-047 decision 8). `Install` writes the
      directories of ADR-047 decision 10, then the records of ADR-020 decision 10, then
      `plugins.lock`, then the bundle list; `Remove` deletes in the reverse order. A
      reader that takes no lock therefore sees the state before the operation, the state
      after it, or a state in which only files that no profile names yet have changed.
    - A change that disables or removes a row of `aiops-cli` in the profile `cli`, or
      takes that bundle out of it, is refused. The manager cannot disable itself or a
      required row (`08:369`) and dsh's manager cannot remove itself (dsh §6); without
      the rule, one command could leave `aiopsctl` unable to run the command that undoes
      it. An operator who wants that change edits the file.
    - Every write that changes a profile ends with `Submit` (`08:369`, `:446`; `07:1855`).

18. **Fake and suite.** The fake, `sdk/def/operations/v0/fake`, works on a home directory
    in the layout of ADR-047 decision 13. It takes each signature verdict and each gate
    verdict from its config instead of computing them, writes the same files under the
    same lock, records its calls, and reports nothing from `Inventory()`, since the
    definition takes no registration (ADR-060 decision 12). A command row mounts alone
    with it (`03:81`). The suite, `sdk/def/operations/v0/conformance`, runs on
    `conformance.Run(t, Mount)` against the plugin and the fake on a temporary home, with
    a case for each rule of decisions 17 and 19 and for `ErrInactive` after release; the
    fake passes it (`07:512`, `:520`).

### The profile write lock

19. **One lock per home** (question Q5 (a)): the file `$AIOPS_HOME/write.lock`. The
    operations plugin holds it for the whole of each method that writes the home
    (decision 16), from the snapshot to the last rename or the restore (`08:374`).
    - It is the operating system's exclusive advisory lock on that open file, `flock(2)`
      on Linux and macOS through the standard library's `syscall` package, so the system
      releases it when the holder exits and a crash leaves no stale lock. dsh's lock
      needs an idle timeout because it can go stale (dsh §4.2); this one cannot.
    - A second writer waits up to 120 seconds, dsh's lock wait (dsh §4.2), then fails
      with exit 1, naming the holder by the process id and the command that the holder
      writes into the file while it holds the lock.
    - Readers take no lock: boot, `Render` and `Inspect` read files that are replaced by
      a rename in the order of decision 17.
    - The path joins the home contract of ADR-047 decision 13: written by `aiopsctl` and
      mounted read-only into a `kernel` that runs as a container, which takes no lock at
      S2a. From S6a `plugin-manager` writes the home from inside the kernel (ADR-047
      decision 13), and the ADR of S6a says how that process takes this lock.

### How command rows reach the operations library, the builder and the gate

20. **One path.** A command row consumes `commands` and `operations` and no launch key.
    It parses nothing itself (decision 8), calls one method of `operations`, and prints
    the result. The operations plugin reaches the code that is not a plugin through
    `profileContext` (question Q1 (a)), as `reload` reaches compose, admit and plan
    (ADR-059 decisions 7 and 35; `03:84`) and as `08:493` has `plugin-manager` call the
    gate. The consumer bindings of the release manifest gain:

    ```yaml
    consumers:
      appExit: [aiops/restart-policy, aiops/cli]
      profileContext: [aiops/loader, aiops/reload, aiops/readiness, aiops/operations]
      operations: [aiops/command-profile, aiops/command-plugin, aiops/command-dist,
                   aiops/command-trust, aiops/command-key]
    ```

21. **The toolbox** is what `ProfileContext.Toolbox()` returns, an interface of launch
    0.2.

    | Part | What it does | Implemented by | Called by (decision 16) | Source |
    |---|---|---|---|---|
    | `Render` | Composes a named profile from a home, bundles and patch files, admits it with this binary's module table and the manifest that boot trusts, and returns the dumps asked for, the diagnostics and the verdict | `boot` | `Render`, `VerifyProfile`, `Enable`, `Disable` | `08:359`, `:366`; ADR-060 decision 17 |
    | `Gate` | Evaluates the clauses of one entry point of ADR-060 decision 17 on an artifact's manifest, bundle, attestation and digest and the signer verified in the same call, against a profile of a home or a published signed manifest. Each clause passes, is denied with its reason, or is not evaluated with its reason | `boot` | `Install`, `Check`, `Pack`, `Gate` | `08:493`; ADR-060 decision 17 |
    | `ReadManifest` | Boot's reader in strict mode, with the lints of step 1 and the SPDX list data | `boot`, with the list data that `cmd/aiopsctl` embeds | `Check`, `Pack`, `New` | `07:1569`; ADR-048 decisions 15 and 19 |
    | `Shipped`, `Release` | The bundles and profiles that the binary embeds; the signed manifest that it embeds | `boot`, from what `dist` hands it | `InitProfile`; `Check`, `New`, `Gate` | ADR-060 decisions 17, 24 and 27 |
    | `Build` | The builder | `cmd/aiopsctl` | `Build`, `Smoke`, `Verify` | ADR-060 decision 22; ADR-044 |
    | `Generate` | `plugingen` | `cmd/aiopsctl` | `Check` | ADR-060 decision 11 |
    | `Vet` | The import analyzer and plugin vet | `cmd/aiopsctl` | `Check` | ADR-060 decision 23 |
    | `Suite` | Runs a suite: an embedded test binary, or a module compiled with the toolchain | `cmd/aiopsctl` | `Check`, `Pack`, `Verify` | ADR-060 decision 46 |
    | `Template` | The templates | `cmd/aiopsctl` | `New` | ADR-060 decision 24 |

    `cmd/aiopsctl` passes the last five to `boot` as values of the launch types in its
    run options, and `boot` hands them on without importing them (`08:1256`). In `aiops`
    they are absent and each answers `ErrNotCarried`, so no server binary links the
    builder or what it links (ADR-044, Consequences), and `plugin-manager` at S6a, which
    builds nothing, needs none of them.

22. **One reader.** `ReadManifest` is boot's reader, the one that `plugingen` already
    reads with (`internal/plugingen/plugingen.go`). `plugin check` and the gate therefore
    share one reader, and there is no second copy (ADR-048 decision 15).

23. **Where `plugingen` runs.** `aiopsctl plugin check --write` is the row
    `command-plugin` calling `operations.Check` with `write` set, which calls
    `Toolbox().Generate`. `internal/plugingen` then writes the generated half of
    `plugin.yaml` and the two generated tests, and for a pack derives
    `requires.definitions` from the registries its rows feed (`07:42`, `:1570`; ADR-060
    decisions 9 and 11). Without `--write`, step 2 compares the committed half with that
    output. A Go plugin needs a Go toolchain for it, as the check does (`07:1531`); a pack
    does not. In this repository `tools/plugingen` runs the same package for the parity
    check of CI (`tools/plugingen/main.go` at `502a355`), and `make check` runs the built
    `aiopsctl` (ADR-060 decisions 11 and 32).

### `aiopsctl` as the launcher of the `cli` profile

24. **One module table.** `cmd/aiopsctl` links the module table that the builder
    generates into `dist/` and that `cmd/aiops` links (`07:1346`; ADR-060 decision 20).
    It holds `cli`, `operations` and the command rows, as `08:359` and ADR-020 decision
    29 and its Consequences state, so the server image links them too. "The module table
    of the `cli` profile" in ADR-060 decision 53 is this table. Beside it, `cmd/aiopsctl`
    links the parts of decision 21 that `boot` does not implement, and `cmd/aiops` does
    not.

25. **What its `main` does.** It loads the module table, the signed manifest, the limits
    and the shipped files as `cmd/aiops` does, and runs `boot` with:
    - the profile `cli` and the role `all`, with the server rule off: the `cli` profile
      is not a server profile and its rows state no roles (`boot/run.go` sets the rule
      for every run at `a3c85ae`);
    - the home from `$AIOPS_HOME`, passed as the flag `home` that `cmdline` serves
      (`internal/serve/serve.go` does the same for `aiops`). With no home it composes the
      shipped `cli` profile alone, and a command that needs a home is a usage error;
    - every argument as argv, the three streams, and the toolbox parts of decision 21;
    - nothing that the launcher writes on stdout, a kernel log handler at the warning
      level on stderr, and no status file, since `run/status.json` is the server's
      readiness channel (ADR-059 decision 31).

    It parses no flag of its own, and it maps the launcher's code by decision 10.

26. **Its own boot** passes the gate as every profile does (ADR-060 decision 17, boot
    row). Its first-party rows pass checks 5 and 10 on the release's built-in record
    (ADR-060 decision 50; ADR-020 decision 13), and a site that removed that root runs the
    `aiopsctl` that `dist build` wrote (ADR-020 decision 29). A denied command row is
    detached as disabled (`08:467`) and its commands answer by decision 10; a denied
    `cli`, which is required, stops the boot with exit 1.

27. **`profile render` is a command row.** It is a command of `command-profile` that
    calls `operations.Render`, so it composes with the same `boot` code and this binary's
    module table, and a dump is what the server would compose (`08:359`). Its flags stay
    those of S1 except `--dist`, whose files S2a removes (ADR-060 decision 14;
    `cmd/aiopsctl/run.go`). The goldens of the shipped profiles, `cli` among them, run on
    `cmd/aiopsctl` (`08:367`). The goldens of the test distribution move from
    `cmd/aiopsctl/testdata/test-dist/` to `test/dist/cmd/aiopsctl-test`, the `cli` profile
    over the test distribution's module table and the counterpart of `aiops-test`
    (`cmd/aiops/main.go`). It is signed as ADR-060 decision 28 states and kept out of
    `cmd/aiopsctl` by its link guard, and the commit that moves the goldens regenerates
    and reviews them.

28. **No command code in a `main`.** The import analyzer fails when a package under
    `cmd/` imports `sdk/def/commands` or `sdk/def/operations`, as dsh refuses an entry
    point that bypasses its launcher (dsh §7).

### The `aiops-cli` bundle, the `cli` profile and the command rows

29. **Shipped files** (ADR-060 decision 27). `bundles/aiops-cli/bundle.yaml` inserts
    `cli`, marked required, `operations` and the five command rows of decision 30, none
    of which states roles. `profiles/cli/profile.yaml` lists `aiops-core`, then
    `aiops-cli` (`00:515`; `08:253`). Both are embedded through `dist`, and a file of the
    home wins over a shipped one (`internal/serve/serve.go`). No other shipped profile
    lists `aiops-cli` (`08:253`).

30. **One command row per command group** (question Q4 (a)), named `command-<group>` by
    the rule `<contribution noun>-<impl>` (`00:657`). Each is a plugin under `plugins/`
    that injects `commands` and `operations`, lists `commands` in `Spec.Registers`
    (ADR-048 decision 21), and stays under the cap of 1,500 lines of ADR-060 decision 35.

    | Row | Commands at S2a | Source |
    |---|---|---|
    | `command-profile` | `profile render`, `profile verify`, `profile init <p>` | `00:137`; `08:72` row 30, `:366`; ADR-060 decision 27 |
    | `command-plugin` | `plugin new`, `check`, `smoke`, `pack`, `sign`, `push`, `inspect`, `verify`, `gate --against`, `install`, `enable`, `disable`, `remove` | `00:137`; `07:1565`, `:1578`; `08:373`-`375`; ADR-060 decisions 23 and 47 |
    | `command-trust` | `trust add` | `07:1861`; ADR-020 decision 6 |
    | `command-dist` | `dist build`, `dist sign`, `dist use` | `08:402`; `07:1852`, `:1869` |
    | `command-key` | `key generate <prefix>`, which writes `<prefix>.key` and `<prefix>.pub` in the format of ADR-020 decision 4 | ADR-020 decision 21, which leaves the name to the CLI spec (`00:137`) |

    `plugin purge` joins `command-plugin` at S3 (ADR-060 decision 47). A later command
    joins the row of its group, or the row of the plugin that owns it, as `slack
    manifest` is a contribution of `chat-surface-slack` (`06:312`).

### Tests

31. Beside the suites of decisions 5 and 18, and under the standing rules of ADR-060
    decision 32:
    - T3: CI boots the `cli` profile through the built `aiopsctl`, and `aiopsctl help`
      exits 0 and lists every command of decision 30, which needs every row of
      `aiops-cli` ACTIVE.
    - Exit codes: no argument exits 2; an unknown path exits 2; a command whose row a test
      detaches exits 1 and names the row; a command interrupted by a signal exits 1; a
      denial and `rebuild-required` exit 1 with the reason on stderr and nothing on stdout
      but what decision 11 allows.
    - The lock: of two concurrent writes to one home, the second waits and, with a short
      bound in the test, fails naming the holder; a writer killed while it holds the lock
      leaves a home that the next writer locks at once.
    - The refusal of decision 17 for a row of `aiops-cli` in the profile `cli`.
    - The goldens of decision 27, with the dump and the hook dump of the `cli` profile.

### What this ADR does not settle

32. Each item names its trigger.
    - The transport of `Submit` before `admin-api`, its plugin vet rule, and how it
      relates to `admin-api`: the ADR of ADR-060 decision 48, before the exit test. That
      ADR also keeps the `cli` profile's own `reload` row, which `aiopsctl` composes with
      `aiops-core`, from serving the transport.
    - Commands of rows outside `aiops-cli`: `slack manifest` (`00:137`; `06:312`) and
      `oncall override` (`06:638`, `:764`) are contributions of rows of server bundles,
      while the `cli` profile lists `aiops-core` and `aiops-cli` only (`08:253`). The
      step that builds the first of them says how its row reaches the `cli` profile.
    - The required set of the `cli` profile. The signed manifest carries the set of every
      server profile (`08:455`); at S2a it holds the rows `loader`, `restart-policy`,
      `reload` and `readiness` and the key `clock` (`dist/dist.yaml`), all served by
      `aiops-core`, so the `cli` profile meets it. The step that adds a required key that
      no row of `aiops-core` provides, `sql` at S3 (`08:696`), says how the set applies
      to a profile that is not a server profile.
    - How the operations plugin dials once plugin vet refuses a dial outside `dialer`:
      question Q6.
    - What `admin-api` may dispatch (S4a); `--tenant` on `enable` and `disable` (S4b,
      `08:375`); where `plugin-manager` sits from S6a (ADR-060 decision 41).
    - A build of `aiopsctl` for a system without `flock(2)`: no document names one.

## Consequences

- `aiopsctl` and `aiops` share `boot`, the module table and the gate. The code of
  `aiopsctl` itself is its `main`, the adapters of the toolbox and the exit mapping.
- Every `aiopsctl` run boots `aiops-core` and `aiops-cli` and gates their rows. The site
  layer applies to every profile (`08:123`), so each id it patches that the `cli` profile
  does not hold prints a warning on stderr at every run (`08:160`).
- `launch` 0.2 regenerates the `requires` of every plugin that consumes a launch key, in
  the pull request that changes the definition (ADR-060 decision 32, rule 4).
- Two definitions with their fakes and suites, one template, and seven plugins: `cli`,
  `operations` and five command rows. The consumer-bound keys go from 12 to 13.
- `boot` grows by the launch 0.2 services and the toolbox plumbing, under its provisional
  cap of 9,000 lines (ADR-060 decision 35). The kernel does not change, so ADR-060
  decision 3 holds and this ADR proposes no addition to ADR-034.
- No dependency is added: the parser and the lock use the standard library (ADR-060
  decision 34).
- Provisional caps for the new packages, under the rule of ADR-060 decision 35:
  `sdk/def/commands/v0` and `sdk/def/operations/v0`, and the fake and the suite of each,
  800 lines each; `plugins/cli`, 1,500, as a command row's plugin; `plugins/operations`,
  the 3,500 of the operations library.
- The commit that adds `command-profile` removes the S1 code of `cmd/aiopsctl/run.go`,
  `profile render` with `--dist`.
- The design follows dsh as `08` §1.1 carries it: one launcher with typed launch services
  (rows 31 and 32), a startup row that parses the leftover argv (dsh §7), and one
  operations library and one write lock that the CLI and the manager share, with a
  snapshot restored on a failure (dsh §4.2; row 34).

## Edits on acceptance

Made in the commit that records the owner's answers (`08:997`):

- `00` §4.1: a row for `operations` after `00:222`: "The operations library of the CLI
  and the plugin manager: profiles, plugins, trust, distributions and authoring, under
  one write lock | `-`; `C` | | `operations` | no". Plugin record §5 gains the same row,
  with the command rows and, from S6a, `plugin-manager` as consumers and a fake that
  writes a home.
- `00:245`, `07:559` and plugin record §7 (M4): `operations` among the consumer-bound
  keys, now 13.
- `00:276`: `aiops-cli` holds `cli`, `operations` and the rows that contribute to
  `commands`.
- `00:137`: the key-pair command is `key generate` (ADR-020 decision 21).
- `00:212` and plugin record §5, the contract of `launch`: with question Q1 (a) it gains
  "and, through `profileContext`, the launcher's toolbox"; with question Q2 (a) it reads
  "argv and the standard streams", "the readiness latch and the end of boot" and "request
  a process exit, with the code of a command"; with question Q2 (b) only the last.
- `07:1344`: `boot` is called from the `main` of `aiops` and of `aiopsctl`.
- `08:369` and `03:71`: the operations library is the plugin `aiops/operations` under the
  key `operations`, and the write lock is `$AIOPS_HOME/write.lock`.
- ADR-047 decision 13: the row of `write.lock`. ADR-060 decision 24: the template of
  `commands`. ADR-060 decision 35: the caps of the Consequences. ADR-059 decision 29 and
  ADR-060 decision 23: the exit table is recorded in the help of the `cli` row and in the
  exit mapping of `cmd/aiopsctl`.
- The lines that the chosen options of questions Q1 and Q6 name.

## Questions for the owner

Each question gives its options, the recommended one, why, and when the answer is needed.

**Q1. How does the operations plugin reach the code that `aiopsctl` links beside its
module table?** Every command needs `boot`'s compose, admission, gate, reader or dumps,
or the builder, `plugingen`, the analyzer, the templates or the embedded suites. No plugin
may import them (`07:46`; `08:1257`), `boot` imports no definition beyond three
(`08:1256`), what the builder links enters no server binary (ADR-044, Consequences), and
the launch keys stay six (ADR-059 decision 35).
- (a) `profileContext` gains `Toolbox()` in launch 0.2 (decision 21), and the release
  manifest lists `aiops/operations` among its consumers. The six keys stay, and the
  toolbox sits beside compose, admit and plan, which the owner placed in
  `profileContext` for `reload`. Cost: the operations plugin can also call `Commit`,
  which only `reload` calls today. In the `cli` process that is its own composition; in
  a server profile from S6a it is a power the manager's path does not need. `00:212` and
  plugin record §5 name the toolbox in the contract of `launch`.
- (b) A seventh launch key, `toolbox`, class `PC`, consumer-bound to `aiops/operations`
  and provided by every launcher, with the parts that only `aiopsctl` carries absent in
  `aiops`. The operations plugin gets nothing else of `profileContext`. "Six" changes to
  "seven" in `00:212`, `03:63`, `07:1348`, plugin record §5 and
  `sdk/def/launch/v0/doc.go`, and the consumer-bound keys go to 14.
- (c) The builder, `plugingen`, the analyzer, the templates and the suite runner become
  internal packages of the operations plugin, and only `boot`'s parts go through
  `profileContext`, as in (a). The server image then links them, against ADR-044's
  Consequences, and ADR-060 decisions 11 and 22 change their homes.
- Recommended: (a). It keeps every written count and boundary and follows the placement
  the owner chose for `reload` (ADR-059 decision 35). Its cost is the reach of `Commit`;
  (b) removes that cost at the price of a seventh key. Needed before the first command
  row merges.

**Q2. Where does the `cli` row get its streams, the end of boot and its exit code?**
`cmdline` serves argv only, `appExit` carries no code, and no launch service says when
boot has finished (`00:212`; `sdk/def/launch/v0/launch.go`).
- (a) Launch 0.2 as decision 13 states: `Cmdline` gains the three streams, `Ready` gains
  `Booted()`, and `ExitRequest` gains `Code` with the reason `ExitCommand`. The `cli` row
  and each command row mount alone with the launch fake and a test's buffers (`03:81`).
- (b) Only `ExitRequest` gains `Code`. The `cli` row uses the streams of the package
  `os`, and dispatches once `composition` has shown no row loading for an interval it
  measures on `clock`. A test of the row replaces the process's streams, and the
  interval is a guess at a fact that the launcher already knows.
- Recommended: (a). The launcher owns the streams, the settle and the exit code
  (`07:1344`), and (a) hands each to the row as a launch service, as dsh's launcher
  provides argv, the exit and readiness before any row mounts (dsh §7). Needed before the
  `cli` row merges.

**Q3. What is the operations definition called?**
- (a) `operations`: definition `sdk/def/operations/v0`, key `operations`, plugin
  `aiops/operations`, row `operations`. It is the documents' word (`08:369`; `07:1846`;
  plugin record §12) and dsh's (dsh §4.2), and keys with a plural name for one engine
  exist (`00:216`, `:218`, `:224`, `:226`).
- (b) `siteOperations`, plugin `aiops/site-operations`, row `site-operations`. It names
  the scope, the site's home, and reads apart from the hook `reload/operation`, at the
  cost of a name the documents do not use.
- Recommended: (a). Needed before the definition merges.

**Q4. How many command rows?**
- (a) One per command group: five at S2a (decision 30). A group is one row that an
  operator can enable or disable, and each stays under its cap.
- (b) One per command: twenty-one at S2a, each with its own manifest, tests and T3 boot.
- (c) One row for every S2a command, whose plugin would pass the cap of 1,500 lines of a
  command row (ADR-060 decision 35).
- Recommended: (a). It is the fewest rows that keep each plugin under its cap. Needed
  before the first command row merges.

**Q5. What does the write lock cover?** `08:369` names "one profile write lock", and dsh
holds one per profile directory (dsh §4.2). Install and `dist use` also write files of
the site: `trust.yaml`, `verified/`, `bundles/` and `content/` (ADR-047 decision 13;
ADR-020 decision 10).
- (a) One lock per home (decision 19). Every write excludes every other, and two profiles
  of one home are never written at once.
- (b) One lock per profile, `profiles/<p>/write.lock`, and one for the site files, taken
  in that order. Two profiles can be written at once, and each method states which locks
  it takes.
- Recommended: (a). An install writes files of both kinds in one operation, and (a) makes
  it exclusive with one lock and no order to get wrong; its cost, serial writes to one
  home, is nothing for one site. Needed before the first method that writes the home
  merges.

**Q6. How does the operations plugin dial once plugin vet refuses a dial outside
`dialer`?** The vet gains "no dial and no HTTP client outside `dialer`" for every plugin
(harness:58; `07:1400`). The operations plugin pulls and pushes OCI artifacts (ADR-060
decision 41; ADR-047). `dialer` arrives at S3 (`08:696`); its one provider,
`dialer-guarded`, is a row of `aiops-base` (`00:277`), which the `cli` profile does not
list; and from S6a `plugin-manager` would pull artifacts in a server process, where the
cell egress fence of `00` §5.7 applies.
- (a) The vet rule binds from the step that builds `dialer`. At S2a the operations plugin
  dials directly, and before the vet rule merges, the step that builds `dialer` says how
  the operations plugin dials in the `cli` profile and in a server profile.
- (b) The vet rule gains a recorded exception for the operations plugin in every profile,
  as the upstream register lists an importer by an accepted decision (ADR-054). A pull by
  `plugin-manager` in a server process then bypasses the cell egress fence.
- Recommended: (a). The rule stays whole and cannot be met before `dialer` exists, while
  (b) opens the egress fence of a server process. Needed before the vet rule of
  harness:58 merges, or before S2a closes if the owner reads that rule as in force at
  S2a.
