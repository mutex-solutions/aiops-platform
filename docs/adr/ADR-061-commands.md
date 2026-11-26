# ADR-061: The `commands` definition, the `cli` profile and the operations plugin

- Status: **accepted 2026-11-23 by the project owner.** The owner answered all nine
  questions on the same date with the recommended option of each: (a) for Q1 to Q7 and
  Q9, (b) for Q8. The section "Owner answers of 2026-11-23" records them as decisions 33
  to 41, with the boot changes this ADR names. ADR-060 decision 53 had it proposed before
  the first command row merges, and ADR-060 decision 2 required this acceptance before
  that code merges into `main`. Brought to ADR-062 on 2026-11-24, as its decision 16
  asks: decisions 10, 16, 17 and 32 and the scope name its transport and return the
  outcomes of its decision 11.
- Scope: ADR-060 decision 53 (its question Q17 (a)) and decision 41 (its question Q5
  (a)): the `commands` definition; the `cli` row, its dispatcher; the `aiops-cli` bundle
  and the `cli` profile; `cmd/aiopsctl` as the launcher of the `cli` profile, with
  `profile render` as a command row; the definition and the plugin that hold the
  operations library, with the profile write lock (`08:369`; `03:71`); how command rows
  reach the operations library, the builder and the gate; and where `plugingen` runs. It
  also settles the point that ADR-048 decision 20 leaves to it: how the strict reading
  reaches the `plugin check` row (ADR-048 decision 15). The transport by which `aiopsctl`
  submits a generation to a running process is ADR-062, the ADR that ADR-060 decision 48
  asks for, and is not settled here. This ADR changes no decision of `00` by itself: where
  an answer edits `00`, the question names the lines, and `00` is edited in the commit
  that records the answer (`08:997`).
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
  `08:73` (row 31) carries and this ADR records as adapted (Edits on acceptance).
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
  profile write lock, and every operation is a submission to the reload queue, is
  audited and emits `plugin-manager/changed` (`08:369`; `03:71`; plugin record §12). In
  dsh the CLI is "a thin pnpm forwarder", the in-process `ctx.pluginManager` "does the
  same through `operations.ts`", the two share "a profile write lock", and a failure
  restores the package files from a snapshot taken before pnpm ran (dsh §4.2).
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
   (`07:1400`; harness:58). The operations plugin reaches OCI registries. `dialer` is
   grouped with `http` (`00:217`), which arrives at S3 (`08:696`), and its provider is a
   row of `aiops-base` (`00:277`), which the `cli` profile does not list (`08:253`).

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
   holds the positional arguments, the flag values by name, the three streams, the
   working directory `Dir` and a lookup of the environment `Env`, both from `cmdline`
   (decision 13), and the home that the launcher resolved (decision 25), empty when there
   is none. A command resolves a relative path against `Dir`, as S1 resolved it against
   `$PWD` (`cmd/aiopsctl/run.go`), and reads the environment only through `Env`, so it
   mounts alone in a test with the values the test gives (`03:81`). `Run` returns `nil`
   on success, an error that matches `ErrUsage` for a usage error, and any other error
   for a denial, a failed step or `rebuild-required`. It returns when `ctx` ends.

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
     returns the exit code of decision 10. It recovers a panic of `Run` and answers it
     as an error, so a panicking command still ends the process with a code instead of
     leaving the `cli` row's goroutine FAILED and the process waiting. The deadline, the
     concurrency cap and the breaker of the owner-side guard (M2, `07:511`) are question
     Q7. `admin-api`, which plugin record §5 lists as a consumer, could call it from S4a;
     what it may dispatch is S4a's to decide.
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
   - **Obligations.** What a command owes cannot be decided for an arbitrary command
     without cases of its own, so each obligation reads its cases from the
     contribution's fixtures, at `fixtures/commands/<name>.json` for the obligation
     `commands/<name>`, as a pack reads its cases (ADR-047 decision 12). A missing file
     fails the obligation with its id and the path it read. The suite dispatches each
     case's argv through the recording fake (decision 4).
     - `commands/paths`: the file lists the paths the contribution registers, each with
       its summary. The registered paths equal that list and each keeps the word rule.
       The name lint of `plugin check` step 1 reads the same file (decision 21), so every
       word of a registered path passes the deny-list of T1, which names "CLI command"
       (`08:1358`).
     - `commands/run`: each case gives an argv, an optional stdin, the expected outcome
       (`ok`, `usage` or `error`), the exact stdout and a text that stderr contains. The
       outcome is `usage` exactly when `Run` returned an error that matches `ErrUsage`,
       so `ErrUsage` is returned only for a usage error on the paths the cases run.
       Each registered path has at least one `ok` case. While a case runs, the suite
       holds the process's own stdout and stderr and fails when anything reaches them,
       so `Run` writes only to the streams of its invocation on those paths.
     - `commands/cancel`: each case gives an argv and a delay. The context of the
       invocation ends after the delay, and `Run` returns within 5 seconds of that with
       an error that matches the context's error. Five seconds is half the drain
       deadline of ADR-059 decision 30, inside which the `cli` row must return at a
       signal (decision 17).

     A stub that returns "not implemented" fails `commands/run` and `commands/cancel`
     once their cases exist, since neither outcome matches, and fails every obligation
     while no case exists.
   - Guard: each parsing and help rule of decisions 8 and 9; each row of the exit table
     of decision 10; a taken or overlapping path refused; a late registration refused
     with `ErrInactiveEffect`; a call into a command whose fiber is quiescing refused with
     `ErrInactive`; every method of a released registry refused; and `Inventory()` back
     at its baseline after a contributor is disposed. The guard half runs against the
     `cli` row and against the fake (`07:514`).
   - The two-tenant and disabled-for-tenant cases that both halves carry (`07:1465`)
     join at S4a with a raised suite revision, as for the toy registries (ADR-060
     decision 43), once S4a decides what `admin-api` may dispatch (decision 32).
     `commands` holds no tenant data, and the S2a revision holds no skipped case.
   - Note of 2026-11-25: each of the five command rows runs `conformance.Run` on the
     cases of its `fixtures/commands/`; `command-plugin` gained `run.json`, a case of
     outcome `ok` for each of its thirteen commands, and `cancel.json`, so `plugin
     check` passes step 5 on it and its attestation names the obligations half that ran
     (ADR-060 decisions 32 and 50).

6. **A template.** S2a writes the Go template of `commands` in `templates/`, and `plugin
   new commands <id> --lang go` writes it, because CI generates a plugin from the
   template of every registry and holds it to the state of `07:1563`: it compiles,
   passes T0 and the generated tests, and fails conformance with exactly one line per
   obligation (`08:1265`; ADR-060 decision 24). Its `Apply` registers one path,
   `example`, whose `Run` returns "not implemented", and its `fixtures/` holds a README
   that names the three case files of decision 5 and no case, so each obligation fails
   on its missing file (`07:1541`). ADR-060 decision 24 gains it. The template serves the
   authors of first-party and `contrib/` command rows, which reach the `cli` profile
   through `aiops-cli` (decision 29), and the author of any other plugin as far as
   question Q8 lets its rows reach that profile. Note of 2026-11-25, a departure
   recorded as built: `fixtures/commands/` holds the README, which names the three case
   files, and `run.json` and `cancel.json` with one case each of `example`, which the
   stub fails with "not implemented"; `paths.json` is the author's to write, so
   `commands/paths` fails on its missing file. Each obligation still fails once, as the
   template job holds (ADR-060 decision 24), and the two case files show their form.

### The `cli` row, the dispatcher

7. **`aiops/cli`** lives in `plugins/cli/` and is the row `cli` of `aiops-cli`. The bundle
   marks it required, so a `cli` that is not ACTIVE stops the boot of `aiopsctl` with a
   reason instead of leaving the process waiting (`08:459`). It provides `commands` and
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
   command's synopsis and flags, then the three exit codes with the note that a Go
   panic also exits 2 (ADR-059 decision 29), then each row of the
   composition that needs `commands` and is not ACTIVE, with its state and what it waits
   for, read from `composition` and `catalog`. Help that was asked for goes to stdout and
   exits 0. `aiopsctl` with no argument prints the same list to stderr and exits 2, as the
   S1 usage text does (`cmd/aiopsctl/run.go`).

10. **Exit codes** are the three of ADR-059 decision 29 (ADR-060 decision 23).

    | Outcome | Code | stderr |
    |---|---|---|
    | `Run` returned `nil`; help that was asked for; a write whose submission ends in a row of the table of ADR-062 decision 11 that exits 0, or that answers `not-running`, or `restart-required` with the reason `not-submitted` or `image` (decision 17) | 0 | Warnings, and `restart-required: <reason>` where that is the outcome |
    | A usage error: of parsing, an error of `Run` that matches `ErrUsage`, or a path that no row registered while every row that needs `commands` is ACTIVE | 2 | The error and the synopsis |
    | A denial, a failed step, `rebuild-required`, any other error of `Run`, or a panic of `Run`, which `Dispatch` recovers (decision 3) | 1 | `aiopsctl: <path>: <reason>`, with the stack for a panic |
    | A write whose submission ends in a row of the table of ADR-062 decision 11 that exits 1; the files are restored or kept as that row says (decision 17) | 1 | What that row prints: the result, the error line, `restart-required`, or each id and the last status; and whether the files were restored |
    | A path that no row registered while a row that needs `commands` is not ACTIVE | 1 | That row, its state and what it waits for |
    | A signal before the command returned: launcher code 8 (decision 13) | 1 | `interrupted`, and whether the snapshot was restored or left for the next writer (decision 17) |
    | The boot of the `cli` profile failed (launcher codes 1 and 3 to 7), or after the command returned a fiber leaked or the shutdown overran its bound (launcher code 4) | 1 | The launcher's reason |
    | A Go panic that no fiber recovers | 2 | The Go runtime's trace, which tells it from a usage error (ADR-059 decision 29) |

    ADR-059 decision 29 records the table in `cmd/aiopsctl/run.go`. From this ADR it is
    in the help text of the `cli` row and in the exit mapping of `cmd/aiopsctl`
    (decision 25). The mapping reads the launcher's code under decision 13: 0 and 2 are
    the command's, 8 is a signal, and 1 and 3 to 7 are the launcher's. A panic exits 2
    from the Go runtime, outside the mapping, as ADR-059 decision 29 records; the
    kernel recovers a panic in a tracked goroutine (`08` §1.6), so only a panic outside
    every fiber reaches it.

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
      command row mount alone in a test (`03:81`). It also gains `Dir()` and
      `Environ()`, the working directory and the environment of the process, which the
      `cli` row copies into each `Invocation` (decision 2). Only the launcher of
      `aiopsctl` fills them; in `aiops` both are empty, so no server row reads the
      environment through `cmdline`, and the `${env:}` reference still reads `Env`
      alone (`08:162`).
    - `Ready` gains `Booted()`, a channel that the launcher closes once the tree has
      settled and the run-time required audit has passed, the point where `boot` prints
      its boot line today (`boot/run.go`). dsh's launcher marks the app ready when the
      root is ACTIVE and the loader exists (dsh §7; `08:74`, row 32).
    - `ExitRequest` gains `Code`, read only with the new reason `ExitCommand`: the
      launcher stops under its shutdown bound and returns that code, which is 0, 1 or 2.
      Any other code with that reason is a launcher failure.

    Whatever question Q2 decides, `boot.RunOptions` gains `Command`, which `cmd/aiopsctl`
    sets and `cmd/aiops` does not. Under it, a stop that an `ExitCommand` request began
    returns the request's code when the tree disposed within the shutdown bound with no
    leak, and `ExitLeak` otherwise; a stop that a signal or the end of `ctx` began returns
    `ExitInterrupted`, a new launcher code 8; and where boot would return `ExitUsage` it
    returns `ExitInternal`. A launcher code of 0 or 2 is then always a command's. At
    `a3c85ae` a signal returns `ExitOK`, the code of a command's success, and a leak
    replaces only `ExitOK` (`boot/run.go`), so `cmd/aiopsctl` could tell neither a signal
    from a success nor a leak after a failed command. The codes of `aiops` do not change.

### The `operations` definition and plugin

14. **Name** (question Q3 (a)): definition `sdk/def/operations/v0`, key `operations`,
    plugin `aiops/operations` in `plugins/operations/`, and row `operations` of
    `aiops-cli` (ADR-060 decision 41). It is the documents' name for the library
    (`08:369`; `07:1846`; plugin record §12) and dsh's (`operations.ts`, dsh §4.2). A
    plural key for one engine departs from `00:206` and `:656`, as `jobs`, `secrets`,
    `tokens` and `credentials` already do (`00:216`, `:218`, `:224`, `:226`); question Q3
    names the edit and a singular option.

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
    that matches `kernel.ErrInactive` once the provider is released (`07:520`). `Render`
    is added to the list of ADR-060 decision 23 so that the operations plugin is the one
    consumer of the toolbox among the command paths (decision 20), and `Home` lets the
    suite observe every write through the definition (decision 18). The submission to
    the reload queue is a step of each method that writes the home (decision 17), not a
    method of its own.

    ```go
    // sdk/def/operations/v0. launch is sdk/def/launch/v0 at 0.2 (decision 21).
    type Operations interface {
        Home(ctx context.Context, r HomeRequest) (Home, error)
        Render(ctx context.Context, r RenderRequest) (launch.Rendered, error)
        VerifyProfile(ctx context.Context, r RenderRequest) (launch.Rendered, error)
        InitProfile(ctx context.Context, r ProfileRequest) (Change, error)
        SanitizeProfile(ctx context.Context, r ProfileRequest) (Change, error)
        New(ctx context.Context, r NewRequest) (Written, error)
        Check(ctx context.Context, r CheckRequest) (CheckReport, error)
        Smoke(ctx context.Context, r BuildRequest) (launch.Built, error)
        Pack(ctx context.Context, r PackRequest) (Written, error)
        Sign(ctx context.Context, r SignRequest) (Written, error)
        Push(ctx context.Context, r PushRequest) (Pushed, error)
        Inspect(ctx context.Context, r SpecRequest) (Inspected, error)
        Verify(ctx context.Context, r SpecRequest) (Verified, error)
        Gate(ctx context.Context, r GateRequest) (launch.Verdict, error)
        Install(ctx context.Context, r InstallRequest) (Change, error)
        Enable(ctx context.Context, r RowRequest) (Change, error)
        Disable(ctx context.Context, r RowRequest) (Change, error)
        Remove(ctx context.Context, r RemoveRequest) (Change, error)
        AddTrust(ctx context.Context, r TrustRequest) (Change, error)
        GenerateKey(ctx context.Context, r KeyRequest) (Written, error)
        Build(ctx context.Context, r BuildRequest) (launch.Built, error)
        SignDist(ctx context.Context, r SignDistRequest) (Written, error)
        UseDist(ctx context.Context, r UseRequest) (Change, error)
    }
    ```

    The request and result types are types of the definition package, and those that
    the toolbox already defines are the types of `launch` (decision 21):

    | Type | Fields |
    |---|---|
    | `Caller`, embedded in every request | `Env`, the invocation's lookup of the environment (decision 2), the only way the library reads it; `Progress`, the writer of progress lines, stderr in `aiopsctl`. Every path in a request is absolute: a command row resolves it against `Invocation.Dir` |
    | `HomeRequest`, `ProfileRequest` | `Home`, `Profile` |
    | `Home` | `Profiles`; for the profile asked, `Bundles` (its bundle list), `Enabled` (the `enabled` departures of its profile layer by row id) and `Locked` (id, digest and content digest of each entry of its `plugins.lock`); for the site, `Signers` (key id, ids, runtimes, signed), `Distributions` (key ids, and `release` for the default root), `AllowUnsigned`, `Records` (digest, kind, signer, plugin id), `Current` and `Image`; and `Snapshot`, whether an interrupted write left one (decision 17) |
    | `RenderRequest` | `launch.RenderRequest` with `Caller` |
    | `NewRequest` | `Registry` (a registry or `connector`), `ID`, `Lang`, `Contrib`, `Against`, `Plugins`, `Dir` |
    | `CheckRequest`, `CheckReport` | `Dir`, `Write`, `Against`; per step of `07:1565`-`1576` its number, `pass`, `fail`, `not applicable` or `skipped`, and its `launch.Diagnostic`s, and the attestation it would sign |
    | `Smoke` takes `BuildRequest` and returns `launch.Built` | `Plugin`, the plugin directory, `Against`, the release artifact by digest, `ModCache`, `Mirrors` and `Builder` (`image` or `local`); `Built.Rows`, each row of the test-only profile with its state and what it waits for, and its error says why smoke did not pass (ADR-060 decision 58, reading 1) |
    | `PackRequest`, `SignRequest`, `PushRequest`, `Pushed` | `Dir`, `Out`; `Out`, `Key`; `Out`, `Registry`; the spec by digest (ADR-047 decision 1) |
    | `SpecRequest`, `GateRequest` | `Spec`, and for `Verify` `Home`, whose `trust.yaml` it reads, `Builder`, and `Against`, `ModCache` and `Mirrors`, the release artifact by digest and the module source that in-process code builds on (ADR-060 decision 46); `Spec`, `Against` |
    | `Inspected` | The digest; id, version and runtimes; each row with its config and content paths; that no signer was verified (ADR-047 decision 2) |
    | `Verified` | The digest, the verified signer, each suite that ran with its result, and the verdict |
    | `InstallRequest`, `RowRequest`, `RemoveRequest` | `Home`, `Profile` and `Spec`, `Row` or `ID`; `ConnectWait` and `Wait`, the bounds of ADR-062 decisions 9 and 10 |
    | `TrustRequest` | `Home`, `KeyFile`, `IDs`, `Runtimes`, `Signed`, `Distribution` (ADR-020 decision 5) |
    | `KeyRequest`, `SignDistRequest`, `UseRequest` | `Prefix`; `Image`, `Key`; `Home`, `Image` |
    | `BuildRequest` | `launch.BuildRequest` with `Caller` |
    | `Written` | The paths written outside the home |
    | `Change` | `Written`, the home paths written or removed; `Notes`, what the operator is told (a profile copied from the shipped template, an item kept, a snapshot restored); `Outcome` and `Reason`, `not-running` or `restart-required` with its reason, and for the four methods that submit over the socket of ADR-062, `Result`, `Error`, `IDs` and `Last`, its outcome (decision 17); `DistLines`, the `dist.yaml` lines of `rebuild-required` (`08:397`); `Verdict`, the clauses evaluated |

    | Method | Command (decision 30) | Writes the home under the lock | Gate entry point (ADR-060 decision 17) | Toolbox parts (decision 21) | Source |
    |---|---|---|---|---|---|
    | `Home` | None at S2a; `plugin list` reads it from S6a | No | None | None | `08:373`; decision 18 |
    | `Render` | `profile render` | No | Boot | `Render` | `08:359`; `03:63` |
    | `VerifyProfile` | `profile verify` | No | Boot | `Render` | `08:366` |
    | `InitProfile` | `profile init <p>` | Yes | None | `Shipped` | `08:72` row 30; ADR-060 decision 27 |
    | `SanitizeProfile` | `profile sanitize [<p>]` | Yes | None | None | `08:72` row 30; dsh §3.4; question Q9 |
    | `New` | `plugin new <registry>`, `plugin new connector` | No; writes the author's directory | None | `Template`; `Release` for a connector | `07:1551`; ADR-060 decision 24; ADR-047 decision 12 |
    | `Check` | `plugin check <dir>` | No; with `--write`, the author's directory | `plugin check` step 7 | `ReadManifest`, `Generate`, `Vet`, `Suite`, `Scenario`, `Gate` | `07:1565`-`1576`; ADR-060 decision 23 |
    | `Smoke` | `plugin smoke <dir> --against <release>` | No | `dist build`, run by the builder; boot, of the test-only profile | `Build` | `07:1578`; ADR-060 decision 58 |
    | `Pack` | `plugin pack` | No; writes the layout | `plugin pack` | `ReadManifest`, `Suite`, `Scenario`, `Gate` | `07:1859`; ADR-060 decisions 18 and 49 |
    | `Sign`, `Push` | `plugin sign`, `plugin push` | No | None | None | `07:1859`; ADR-020 decision 1; ADR-047 decision 1 |
    | `Inspect` | `plugin inspect <spec>` | No | None | None | `08:373`; ADR-060 decision 58 |
    | `Verify` | `plugin verify <spec>` | No | None | `Build`, `Suite` | ADR-060 decisions 31 and 46 |
    | `Gate` | `plugin gate <spec> --against <dist.yaml>` | No | `plugin gate --against` | `Gate` | `08:493` |
    | `Install` | `plugin install <spec> --profile <p>` | Yes | Both `plugin install` rows | `Gate`, `Render` | `08:374`, `:395`-`397`; ADR-047 decisions 5 to 10 and 17 |
    | `Enable`, `Disable` | `plugin enable <row>`, `plugin disable <row>` | Yes | Boot, on the changed profile | `Render` | `08:375`; ADR-060 decision 47 |
    | `Remove` | `plugin remove <id> --profile <p>` | Yes | Boot, on the changed profile | `Render` | ADR-047 decision 10 |
    | `AddTrust` | Both forms of `trust add` | Yes | None | None | `07:1861`; ADR-020 decisions 5, 6 and 13 |
    | `GenerateKey` | `key generate <prefix>` | No; writes `<prefix>.key` and `<prefix>.pub` | None | None | ADR-020 decisions 4 and 21 |
    | `Build` | `dist build` | No | `dist build`, run by the builder | `Build` | `08:402`; ADR-060 decision 22 |
    | `SignDist` | `dist sign` | No | None | None | `07:1868`; ADR-020 decision 1 |
    | `UseDist` | `dist use <image>` | Yes | None | None | `07:1869`; ADR-020 decision 12; ADR-047 decision 13 |

    `Purge` joins at S3 with `durable` (ADR-060 decision 47).

    Note of 2026-11-25: the sketch and the tables above are edited to the definition as
    built. `Build` and `BuildRequest` arrived at its minor 0.3; at 0.4 `Smoke` takes
    `BuildRequest` and returns `launch.Built`, since smoke is a build of the toolbox's
    builder that also boots the test-only profile, so `SmokeRequest` and `SmokeReport`
    do not exist, and `SpecRequest` gains `Against`, `ModCache` and `Mirrors` for
    `plugin verify` of in-process code. `sdk/def/operations/v0` declares the methods in
    another order, with `ctx` first in each, and its `definition.yaml` records each minor.

17. **Rules every provider keeps.** `08:369` reads, as plugin record §12 does: "Every
    operation is a submission to the reload queue, is audited and emits
    `plugin-manager/changed`. ... The manager cannot disable itself, a required row, the
    last provider of a bound key or another profile." `03:71` has "audited and submitted
    to `reload`" and the first three of the four. Each clause is kept below, or deferred
    with its trigger.
    - **Install.** `Install` runs every clause of its entry point before it writes,
      writes nothing on a denial and answers `rebuild-required` for in-process code with
      no file changed (`08:374`, `:397`; ADR-060 decision 23). It refuses an artifact
      whose group id clashes with a row of the target profile, read from a `Render` with
      the artifact's bundle appended (ADR-047 decision 17). Into the profile `cli` it
      follows question Q8: with (b) it installs in-process code after `dist use` there,
      its rows disabled, and refuses a pack and remote code, since a pack's rows
      contribute no command and `commands` takes no remote contribution (`00:222`); with
      (a) or (c) it refuses, as `08:269` writes.
    - **Enable and Disable.** They write only a departure from the bundle layers into the
      profile layer, with no `--tenant` (`08:375`; ADR-060 decision 47), and only after
      `Render` has admitted the changed profile as boot would (ADR-060 decision 17, boot
      row), so a change that boot would refuse writes nothing. Note of 2026-11-25, as
      built: under the lock they render the changed profile in a view of the home, a
      temporary directory that holds the changed files and links every other entry into
      the home, and rename nothing until that render admits it; the view is removed
      before they return. Until that date they renamed the change first and restored the
      snapshot on a refusal, so a refused layer stood in the home until the restore.
      Note of 2026-11-26 (review round over the whole step): "only a departure" is
      read against a render of the profile with the profile layer and the site layer
      set aside (`RenderRequest.Aside`). Where the bundle layers already give the row
      the value asked for, the item of the row loses its `enabled` field, or goes when
      only its id stays, and where the layer holds none nothing is written or submitted;
      until that date each command set or appended `{id, enabled}` whatever the bundle
      layers gave. With no process on the profile, a changed profile that still renders
      the row otherwise is named as overridden by the site layer, not as a change that
      applies at the start. Suite revision 6 of `operations` holds the drop.
    - **What the manager cannot disable.** `Disable` and `Remove` refuse, and write
      nothing for, a change that disables or removes: a row of `aiops-cli` in the profile
      `cli`, or that bundle itself, since that is the manager, and without the rule one
      command could leave `aiopsctl` unable to run the command that undoes it (dsh's
      manager cannot remove itself, dsh §6); a row of the profile's required set; or the
      last enabled provider in the profile of a provider-bound or consumer-bound key
      (`00:245`). Both are read from the `Rendered` of the changed profile, since
      disabling the last provider of a key that is not required leaves its consumers
      PENDING (`08:471`) and is no refusal of admission. Note of 2026-11-25, as built, a
      departure: `CannotDisable` and `CannotRemove` read the `Rendered` of the current
      profile, in which the row is still enabled, and count the row, or the artifact's
      rows, as gone, which decides each of these clauses as the changed profile would
      with no second render; the render of the changed profile then decides admission.
      An operation changes only the
      profile its request names, which is the reading of "another profile" for a CLI
      that writes any profile of the home; from S6a `plugin-manager` names only the
      profile of its own process, and the ADR of S6a says so (decision 32). An operator
      who wants a refused change edits the file.
    - **A profile the home does not hold.** A write to it first copies the shipped
      template with `InitProfile` and says so (ADR-060 decision 27).
    - **The home** (ADR-047 decision 14). The library refuses to write into a home whose
      owner is not its effective user, creates the home and `run/` with mode 0700 where
      they are missing, and writes each file with mode 0600, replaced by a rename in its
      directory (ADR-047 decision 8); `write.lock` is the one exception (decision 19).
      With `$AIOPS_HOME` unset or empty there is no home: a method that writes the home
      returns an error that matches `ErrUsage` and names the variable, and `Render`
      composes the shipped profiles alone (decision 25). With `$AIOPS_HOME` naming a
      directory that does not exist, a read finds an empty home and the first write
      creates it, as `trust add` does on the clean machine of ADR-060 decision 30, step
      C1; a missing parent directory fails the write.
    - **Order and snapshot.** A method that writes the home takes the lock (decision 19),
      runs the checks that read the home, then writes into `$AIOPS_HOME/snapshot/` a copy
      of every file it will replace or delete and the list of the paths it will create,
      and syncs it. The set covers the profile definition, `plugins.lock` and the profile
      patch (`08:374`), the records under `verified/` and `verified/current`, which are
      restored with `plugins.lock` (ADR-020 decision 10), `trust.yaml`, `image.yaml`, and
      the directories that install creates under `bundles/` and `content/`. It then
      renames: `Install` the directories of ADR-047 decision 10, then the records, then
      `plugins.lock`, then the bundle list; `Remove` the bundle list, then
      `plugins.lock`; `UseDist` the record, then `current`, then `image.yaml`. Last, it
      submits, removes the snapshot and releases the lock. `Remove` deletes the
      directories that no profile names any more only after a submission that kept the
      change, so the snapshot holds no copy of them, and a directory that a failure leaves
      behind is removed by the next install (ADR-047 decision 10).
    - **Restore.** On a failure, a refused submission or the end of `ctx`, the method
      restores the snapshot, copying each file back and removing each created path. It
      runs the restore under a context detached from `ctx`, bounded to 5 seconds, half
      the drain deadline of ADR-059 decision 30, so that the `cli` row returns inside the
      launcher's drain after a signal (decision 10). When the bound passes, or the process
      dies, the snapshot stays: the next writer, once it holds the lock, restores it
      before it writes and says so, and `Home` reports it. T1 tests of
      `plugins/operations` run an install in a child process that stops after its first
      rename, kill it, and check that the next write first restores the home to its state
      before the install; kill a restore in the middle and check the same; and interrupt
      the install at the same point and check exit 1 with the home restored. Note of
      2026-11-26 (review round over the whole step): the end of `ctx` restores only
      before a request reached the row. After it, the row may have accepted the
      operation and the process may apply it, so the files stay and the outcome is
      `verdict-unknown`, with each id and the last status, as ADR-062 decision 11 has an
      accepted operation whose verdict is unknown keep them. A write marks its snapshot
      before it submits, and the next writer drops a marked snapshot that a killed write
      left, keeps the files and says that the verdict is unknown, instead of restoring
      it; a write whose submission it restores unmarks it first, so a stopped restore
      goes on. A server process does not boot a home that holds `snapshot/paths.json`:
      it exits 1 and names `aiopsctl profile sanitize <profile>`, whose write finishes
      the stopped one first; `aiopsctl` itself boots, since its commands are those
      writers, and `profile render` prints a warning that names the snapshot.
    - **Submission** (`08:369`, `:446`; `07:1855`). Every method that writes the home ends
      with the outcome below while it holds the lock. `Install`, `Enable`, `Disable` and
      `Remove` submit the profile they changed over `<home>/run/reload.sock`, the
      transport of ADR-062, within the bounds `ConnectWait` and `Wait` of their requests,
      which the command rows take from `--connect-wait` and `--wait` (ADR-062 decisions 9
      and 10). They return the outcome of ADR-062 decision 11, whose table decides the
      files and the exit: `Change.Result`, the final result as the wire carries it; or,
      without one, `Change.Error`, the error line before an id, `Change.Outcome`, or each
      operation id and the last status (ADR-062 decision 16). `Disable` refuses the
      transport's own rows (ADR-062 decision 12). Until S6a version 1 of the wire carries
      those four commands only (ADR-062 decisions 6 and 15), so `InitProfile`,
      `SanitizeProfile` and `AddTrust` submit nothing and answer by `run/status.json`
      alone, for the profile they changed or, for `AddTrust`, the profile that the file
      names, since every composition reads `trust.yaml`. `UseDist` answers
      `restart-required` with the reason `image`, since a new image is a new process
      (`07:1869`). The other methods write no home and submit nothing. Every
      `restart-required` carries its reason, as dsh's does (dsh §4.4).

      | Case | `Change` | Files | Exit (decision 10) |
      |---|---|---|---|
      | Any of them but `UseDist`: no `run/status.json` of the home names a running process of that profile (ADR-062 decision 9) | `Outcome` `not-running`: the change applies at the next start | Kept | 0 |
      | `Install`, `Enable`, `Disable` or `Remove`: a final result `passed` that shows the change | `Result` | Kept | 0, with the generation and the digest |
      | The same: a final result `passed` that does not show the change | `Result` | Kept | 1, naming the row and the later layer that overrides the change |
      | The same: a final result `rejected`, `rolled back` or `rollback failed`, or `abandoned` with `state-unknown` | `Result` | Restored | 1, with the result; with `state-unknown`, the restart text |
      | The same: an error line before an id exists, `peer`, `busy`, `request` or `version`, or a refusal of `Submit` | `Error` | Restored | 1, with the text; with `state-unknown`, the restart text |
      | The same: no final result on the second submission either | `IDs` and `Last` | Restored after `abandoned` without `state-unknown`; kept otherwise | 1, with each id and the last status |
      | The same: `--wait` elapsed, or the connection was lost beyond the connect window, after the operation was accepted | `IDs` and `Last` | Kept | 1, with the id and the last status |
      | The same: a process runs the profile and nothing answers within `--connect-wait` (ADR-062 decision 9) | `Outcome` `restart-required` | Kept | 1, `restart-required: <socket> did not answer` |
      | `InitProfile`, `SanitizeProfile` or `AddTrust` while a process runs the profile | `Outcome` `restart-required`, reason `not-submitted`: the process reads the change at its next start | Kept | 0 |
      | `UseDist` | `Outcome` `restart-required`, reason `image` | Kept | 0 |

    - **Audit.** "Is audited" waits for `audit-chain` (S5, `08:698`), which no profile of
      S2a composes. The step that builds it says how an operation of `aiopsctl` is
      audited, through the submission that the running process audits or otherwise; until
      then no operation is audited, as ADR-060 decision 29 defers the audit of each
      generation (decision 32).
    - **`plugin-manager/changed`.** The hook belongs with `pluginManager`, which lands
      with its first consumer at S6a (`00:206`). From S6a the operations library emits it
      after every completed operation in the process that runs it, as dsh's manager does
      (dsh §4.4), and the ADR of S6a names the definition that owns it (decision 32).
    - **trust add.** `AddTrust` takes both forms of ADR-020 decision 5. It refuses a
      pattern that is neither an exact id nor `<owner>/*`, the owners `aiops` and
      `contrib`, and the runtime `wasm` (ADR-020 decision 6), and with `--distribution`
      it writes `key: release` first when it creates the field `distributions`, so adding
      a site key never drops the default root (ADR-020 decision 13).
    - **Secrets.** Registry credentials come from the Docker-format `config.json` that
      `$DOCKER_CONFIG`, or `.docker/` under `$HOME`, names in the invocation's
      environment, read through the OCI client (ADR-047 decision 4). The password of an
      encrypted key comes from `AIOPS_KEY_PASSWORD`, which may be empty as cosign allows
      (ADR-020 decision 4); with the variable unset, `key generate`, `plugin sign` and
      `dist sign` return an error that matches `ErrUsage` and names it. Neither is written
      into the home, and neither reaches a server process (`00:183`, `:412`): the library
      reads the environment only through `Caller.Env`, which `aiops` never fills (decision
      13). Where `plugin-manager` takes its credentials from S6a is question Q6.

18. **Fake and suite.** Neither may import the YAML library, which is never in `sdk/`
    (ADR-060 decision 41), nor any upstream module (`07:522`, U3; ADR-054 decision 17),
    and the SDK module requires `sdk/kernel` and pgx only (`00:262`; ADR-060 decision 4).
    - The fake, `sdk/def/operations/v0/fake`, is an in-memory engine (`07:496`): it holds
      a home in memory, the content that `Home` reports, and writes no file. It takes
      from its config what it cannot compute without the plugin's libraries: each
      signature verdict, each gate verdict, the `Rendered` of each profile, the id,
      digest, runtimes and bundle rows that a spec names, and the outcome of each
      submission. On that model it keeps the rules of decisions 17 and 19 that do not
      concern files: the refusals, the copy of a shipped profile, the forms and
      refusals of `trust add`, the restore on a failure, the outcomes of a submission
      and a lock with its wait. It records its calls and reports nothing from
      `Inventory()`, since the definition takes no registration (ADR-060 decision 12). A
      command row mounts alone with it (`03:81`).
    - The suite, `sdk/def/operations/v0/conformance`, runs on `conformance.Run(t, Mount)`
      against the plugin and the fake. Its cases set up and observe a home only through
      the methods of the definition and their result types, `Home` above all, so they
      read no file. One table configures both subjects: the fake through its config, and
      the plugin through the launch fake's toolbox (decision 21), a fixture artifact in
      an OCI layout (ADR-047 decision 1) and a temporary home. It has a case for each
      rule of decisions 17 and 19 that the methods can show: a denial writes nothing;
      `rebuild-required` changes nothing; a group clash is refused; each refusal of what
      the manager cannot disable; the copy of a shipped profile; each form and refusal
      of `trust add`; each outcome of a submission, with the files kept or restored; a
      write after an interrupted one restores first; a second writer waits and fails
      after its bound; and `ErrInactive` after release (`07:512`, `:520`). The fake
      passes it. Note of 2026-11-26, as built: suite revision 5 of `operations` adds
      O26, the outcomes of a submission with no final result, each set by `Setup.Socket`:
      `refused` restores the files, and `no-final-result`, `verdict-unknown` and
      `restart-required` with the reason `no-answer` keep them, each with
      `ErrSubmission` naming its outcome; and `trust add` while a process runs the
      profile answers `restart-required` with the reason `not-submitted`. The mount of
      the operations row scripts its socket so, and the fake takes `Socket` in its
      config. No cap was raised.
    - The cases that need the files are T1 tests of `plugins/operations`, which may
      import the YAML library (ADR-060 decision 41): the rename order, the content of
      the snapshot and the restore after a kill (decision 17), the modes and the owner
      of decision 17, and the record in `write.lock` (decision 19). Note of 2026-11-26,
      as built: `TestAnInstallKilledOrInterruptedKeepsItsFiles` of `cmd/aiopsctl` runs an
      install in a process of its own, the test binary as the `aiopsctl` of the test
      distribution, against a process that runs `dev` and whose socket takes the request
      and answers nothing, so the install stops after its renames, with its request
      taken: killed, it leaves its snapshot and the next writer keeps the files, drops
      the snapshot and says that the verdict is unknown; interrupted, it keeps them
      itself and exits 1 with `verdict-unknown` (decision 31; the note of 2026-11-26 to
      decision 17, until which the case was `TestAnInstallKilledOrInterruptedIsRestored`
      and wanted the home restored). A stop after the first rename alone stays the
      generic write of `TestKilledWriteIsRestoredByTheNextWriter`, since the built binary
      has no hook to stop there.

### The profile write lock

19. **One lock per home** (question Q5 (a)): the file `$AIOPS_HOME/write.lock`. The
    operations plugin holds it for the whole of each method that writes the home
    (decision 16): from before the checks that read the home, through the renames and
    the submission, to the removal or the restore of the snapshot (`08:374`; decision
    17).
    - It is the operating system's exclusive advisory lock on that open file, `flock(2)`
      on Linux and macOS through the standard library's `syscall` package, so the system
      releases it when the holder exits and a crash leaves no stale lock. A holder holds
      it only through bounded steps, the submission included (decision 17), so it has no
      idle timeout. dsh's lock has a wait of 120 s and an idle timeout of 600 s (dsh
      §4.2), which the record gives no reason for; row 34 of the dsh checklist records
      the adaptation (Edits on acceptance). What is lost: a waiter cannot break the lock
      of a live holder that hangs.
    - `write.lock` is created once, with mode 0600, and is never renamed or deleted:
      `flock(2)` binds to one inode, and a renamed file would let a second writer lock
      the new inode while the first holds the old one. It is the one exception to the
      rename rule of ADR-047 decision 14. The holder writes its record, its process id
      and its command, in place through the locked descriptor, truncating first.
    - A second writer waits up to 120 seconds, dsh's lock wait (dsh §4.2), then fails
      with exit 1, naming the holder from that record. It reads the record without the
      lock and treats a short or empty read as an unknown holder.
    - Readers take no lock: boot, `Render` and `Inspect` read files that are each
      replaced whole by a rename, so each file is read old or new and never torn. A read
      of several files can still pair states:
      - `Install` writes every file that the new bundle list names before the list, so a
        reader that reads the list first composes the old or the new profile. A reader
        that reads `plugins.lock` before the list can pair the new list with the old
        lock, and boot then detaches the artifact's rows by check 5 (ADR-047 decision
        18); those rows were disabled anyway.
      - `Remove` deletes the directories only after the submission, so a running process
        has recomposed without the id before its files go. A boot that read the old
        bundle list while a remove deletes them stops, as `08:465` has for a missing
        bundle file, and a live recomposition in that window is rejected whole and keeps
        the running tree; the next boot or generation composes the new state.

      Readers take no shared lock, because a writer holds the lock through its
      submission while the process that judges the submission reads the home (`03:84`).
    - `write.lock` and `snapshot/` join the home contract of ADR-047 decision 13:
      written by `aiopsctl`, and mounted read-only into a `kernel` that runs as a
      container, which takes no lock and reads neither at S2a. From S6a `plugin-manager`
      writes the home from inside the kernel (ADR-047 decision 13), and the ADR of S6a
      says how that process takes this lock.

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
    0.2. The launch package holds every type the toolbox takes and returns, so `boot`
    implements and hands on the parts without importing anything beyond its three
    definitions (`08:1256`), and the operations plugin uses them without importing
    `boot` (`08:1257`).

    ```go
    // sdk/def/launch/v0, minor 0.2.
    type Toolbox interface {
        // Implemented by boot.
        Render(ctx context.Context, r RenderRequest) (Rendered, error)
        Gate(ctx context.Context, r GateRequest) (Verdict, error)
        ReadManifest(ctx context.Context, r ManifestRequest) (ManifestSummary, []Diagnostic, error)
        Shipped(ctx context.Context) (map[string][]byte, error)
        Release(ctx context.Context, against string) (Release, error)
        Tools // implemented by cmd/aiopsctl and handed to boot
    }

    // Tools are the parts that only aiopsctl links. In aiops each returns an error
    // that matches ErrNotCarried.
    type Tools interface {
        Build(ctx context.Context, r BuildRequest) (Built, error)
        Generate(ctx context.Context, r GenerateRequest) ([]Diagnostic, error)
        Vet(ctx context.Context, dir string) ([]Diagnostic, error)
        Suite(ctx context.Context, r SuiteRequest) (SuiteReport, error)
        Template(ctx context.Context, r TemplateRequest) (map[string][]byte, error)
        Scenario(ctx context.Context, r ScenarioRequest) (ScenarioReport, error)
        Module(ctx context.Context, r ModuleRequest) (Module, error)
    }
    ```

    | Type | Fields |
    |---|---|
    | `Diagnostic` | `Check` (a gate check, a step of `plugin check` or an obligation id), `File`, `Line`, `Text`, `Warning` |
    | `Clause`, `Verdict` | `Check` (1 to 10), `Clause`, `Outcome` (`pass`, `deny` or `not-evaluated`) and `Reason`; `Entry`, the entry point of ADR-060 decision 17, its `Clauses`, and `RebuildRequired` |
    | `RenderRequest` | `Home`, `Profile`, `Bundles`, `Patches`, `Roles`, `Server` and `Dumps`, the flags of `profile render` (decision 27); `Append`, the bundle of an artifact appended as install would append it (ADR-047 decision 17) |
    | `Rendered` | `Composition`, `Admission` and `Required`, the types of launch 0.1; `Keys`, each key with its class and the enabled rows that provide it; `Departures` from the release bindings (`08:366`); `Dumps` by flag; `Diagnostics`; `Distribution`, the digest of the manifest it admitted with (decision 26) |
    | `GateRequest` | `Entry`; the artifact's `Manifest`, `Bundle` and `Attestation` bytes and its `Digest`; `Signer`, the key id that the caller verified, `unsigned`, or empty where the entry point verifies none; `Home` and `Profile` for install; `Against`, the bytes of a published signed manifest |
    | `ManifestRequest`, `ManifestSummary` | `Dir`, `Against`; `ID`, `Version`, `Runtimes`, `Provides`, `Registers` and `Definitions` (`requires.definitions`) |
    | `Release` | `Digest`; `Manifest`, the signed bytes; per instantiable plugin its id and the first entry of its `configSchema` examples (ADR-047 decision 12) |
    | `BuildRequest`, `Built` | `DistYAML`, `ModCache`, `Mirrors`, `AllowBump`, `Builder` (`image` or `local`), and for smoke and verify the plugin directory, or the module path, version and sum, with `Against`; the image, the signed manifest's bytes, the site's `aiopsctl`, the verdict, and for smoke each row's state and what it waits for |
    | `GenerateRequest` | `Dir`, `Write` |
    | `SuiteRequest`, `SuiteReport` | `Dir`, the definitions with their versions, the halves and the fixtures directory; per definition its suite revision, the halves that ran and each failed obligation by id |
    | `TemplateRequest` | `Registry`, `ID`, `Lang`, `Contrib` |
    | `ScenarioRequest`, `ScenarioReport` | `Dir`, `Name`; the scenario's name and version, the halves that ran and each failure (ADR-060 decision 49) |
| `ModuleRequest`, `Module` | `Dir`, the root of the module of in-process code, and `Version`, the version it is packed at; its `Path`, `Version` and h1 `Sum`, and `Deps`, the rest of its build list as `go list -m -json all` reads it with the author's toolchain |

    `launch` gains no type of the manifest schema. `ManifestSummary` holds six fields
    copied from boot's own reading, so the reader stays boot's and there is no second
    copy (ADR-048 decision 15). A test of `boot` holds the summary of every manifest of
    the repository equal to the same fields of `plugingen`'s output, beside the test of
    ADR-060 decision 14 that holds boot's reading equal to `plugingen`'s.

    | Part | What it does | Implemented by | Called by (decision 16) | Source |
    |---|---|---|---|---|
    | `Render` | Composes a named profile from a home, bundles and patch files, admits it with this binary's module table and the manifest that boot trusts, and returns the dumps asked for, the diagnostics and the verdict. Given a home, it first applies decision 26 | `boot` | `Render`, `VerifyProfile`, `Install`, `Enable`, `Disable`, `Remove` | `08:359`, `:366`; ADR-060 decision 17 |
    | `Gate` | Evaluates the clauses of one entry point of ADR-060 decision 17 on an artifact's manifest, bundle, attestation and digest and the signer verified in the same call, against a profile of a home or a published signed manifest. Each clause passes, is denied with its reason, or is not evaluated with its reason. Given a home, it first applies decision 26 | `boot` | `Check`, `Pack`, `Gate`, `Install` | `08:493`; ADR-060 decision 17 |
    | `ReadManifest` | Boot's reader in strict mode, with the lints of step 1 on data that `cmd/aiopsctl` hands in: the SPDX list data, the marks of `00` §8.1 and the forms of T7. The mark and T7 data name upstreams, which `boot`'s word list forbids there (ADR-060 decision 45; `07:1507`, U1), so `boot` holds none of it. For a plugin with `fixtures/commands/paths.json`, the mark lint runs on each word of each path (decision 5) | `boot`, with that data | `Check`, `Pack` | `07:1569`; ADR-048 decisions 15 and 19; `08:1358`, `:1364` |
    | `Shipped` | The bundles and profiles that the binary embeds, by path | `boot`, from what `dist` hands it | `InitProfile` | ADR-060 decision 27 |
    | `Release` | The signed manifest that the binary embeds, or the one that `--against` names, with its instantiable plugins | `boot` | `New` | ADR-060 decisions 17 and 24; ADR-047 decision 12 |
    | `Build` | The builder, on the image path or the local path (ADR-044 decision 1); for smoke it also boots the test-only profile and reads its row states | `cmd/aiopsctl` | `Build`, `Smoke`, `Verify` | ADR-060 decision 22; ADR-044 |
    | `Generate` | `plugingen` | `cmd/aiopsctl` | `Check` | ADR-060 decision 11 |
    | `Vet` | The import analyzer and plugin vet | `cmd/aiopsctl` | `Check` | ADR-060 decision 23 |
    | `Suite` | Runs a suite: an embedded test binary, or a module compiled with the toolchain | `cmd/aiopsctl` | `Check`, `Pack`, `Verify` | ADR-060 decision 46 |
    | `Template` | The templates | `cmd/aiopsctl` | `New` | ADR-060 decision 24 |
    | `Scenario` | Runs a named, versioned scenario, the echo scenario at S2a, on a pack's rows replayed from its fixtures | `cmd/aiopsctl` | `Check`, as step 8; `Pack`, for the attestation | ADR-060 decision 49; `07:1576` |
| `Module` | Reads the module of in-process code for the module layer and the SBOM of `plugin pack`: the h1 sum of its module zip and its build list | `cmd/aiopsctl` | `Pack` | ADR-047 decision 1 |

    Note of 2026-11-25: the sketch and the two tables are edited to launch 0.2 as built,
    whose `Tools` has a seventh method, `Module`, which `plugin pack` of in-process code
    calls for the module layer and the SBOM (ADR-047 decision 1); in `aiops` it answers
    `ErrNotCarried` as the other six do.

    `cmd/aiopsctl` passes one value of `Tools` to `boot` in its run options, and `boot`
    hands it on without importing what implements it (`08:1256`). In `aiops` it is
    absent and each of its methods answers `ErrNotCarried`, so no server binary links
    the builder or what it links (ADR-044, Consequences), and `plugin-manager` at S6a,
    which builds nothing, needs none of them.

    **The builder image and smoke.** On the image path, the host's `aiopsctl` runs the
    image's `aiopsctl`, its entry point (ADR-044 decisions 1 and 4), with the same
    command and arguments and `--builder local`. That run is a command row like any
    other (ADR-060 decision 53): the container has no `$AIOPS_HOME`, so the inner
    `aiopsctl` boots the shipped `cli` profile with no home (decision 25), with the
    inputs that the command names (`dist.yaml`, the module cache, the plugin directory)
    bound read-only and its output directory bound read-write. `dist build`, `plugin
    smoke` and `plugin verify` write no home, so they need none. For smoke, `Build` then
    starts the throwaway image with the test-only profile on a throwaway home, and reads
    each row's state and what it waits for from `run/status.json`, which gains the field
    `rows`, written at settle and at every committed generation. At `a3c85ae` the file
    holds no row state (ADR-059 decision 31) and `plugin-inventory` is S6a (`08:373`), so
    without the field smoke could not apply its rule (ADR-060 decision 58, reading 1).
    The field is additive, and `aiops` writes it too.

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
    `aiopsctl` (ADR-060 decisions 11 and 32). Note of 2026-11-25: `make plugin-check`,
    the last step of `make check`, runs the built `aiopsctl`'s `plugin check` on every
    plugin under `plugins/` and `examples/` (ADR-060 decision 32, standing rule 2).

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
      (`internal/serve/serve.go` does the same for `aiops`). With `$AIOPS_HOME` unset or
      empty there is no home: it composes the shipped `cli` profile alone, and a command
      that needs a home is a usage error (decision 17);
    - every argument as argv, the three streams, the working directory and the
      environment for `cmdline` (decision 13), and the `Tools` of decision 21;
    - `Command` (decision 13);
    - nothing that the launcher writes on stdout, a kernel log handler at the warning
      level on stderr, and no status file, since `run/status.json` is the server's
      readiness channel (ADR-059 decision 31).

    It parses no flag of its own, and it maps the launcher's code by decision 10. When
    the boot of the `cli` profile stops on a layer file of the home that is malformed or
    empty (`08:465`), the recovery is question Q9.

26. **Its own boot, and the distribution the server runs.**
    - Its own boot passes the gate as every profile does (ADR-060 decision 17, boot
      row). Its first-party rows pass checks 5 and 10 on the release's built-in record
      (ADR-060 decision 50; ADR-020 decision 13), and a site that removed that root runs
      the `aiopsctl` that `dist build` wrote (ADR-020 decision 29). A denied command row
      is detached as disabled (`08:467`) and its commands answer by decision 10; a
      denied `cli`, which is required, stops the boot with exit 1.
    - The distribution that the server of a home runs is the one that
      `verified/current` names, or the release's where none is current (ADR-020
      decisions 12 and 13). A release `aiopsctl` keeps booting on its built-in record
      after `dist use` of a site image (ADR-020 decision 30, reading 5), but its module
      table lacks the site's in-process modules, so what it composes is not "exactly
      what the server would compose" (`08:359`), and ADR-060 decision 17 evaluates an
      install after `dist use` "with the module table of the recorded distribution".
      So the toolbox's `Render` and `Gate`, given a home, compare the digest of the
      manifest that this binary embeds with that distribution's. When the two differ,
      they refuse with exit 1, naming both digests and the site's `aiopsctl` that `dist
      build` wrote, which boots on `current` (ADR-020 decision 29); `profile render`,
      `profile verify`, `plugin install`, `enable`, `disable` and `remove` then write
      nothing. A command that composes nothing of the home, such as `inspect`, `gate
      --against`, `check`, `trust add` or `dist use`, still runs. A test runs the
      release `aiopsctl` on a home after `dist use` of a site image and checks each
      refusal, then runs the site's `aiopsctl` on the same home and checks that each
      command runs.

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
    and reviews them. Amended by decision 42: `aiopsctl` keeps `--dist`, and the goldens
    of the test distribution stay in `cmd/aiopsctl` and compose through it.

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
    | `command-profile` | `profile render`, `profile verify`, `profile init <p>`, and with question Q9 (a) `profile sanitize [<p>]` | `00:137`; `08:72` row 30, `:366`; ADR-060 decision 27; dsh §3.4 |
    | `command-plugin` | `plugin new`, `check`, `smoke`, `pack`, `sign`, `push`, `inspect`, `verify`, `gate --against`, `install`, `enable`, `disable`, `remove` | `00:137`; `07:1565`, `:1578`; `08:373`-`375`; ADR-060 decisions 23 and 47 |
    | `command-trust` | `trust add` | `07:1861`; ADR-020 decision 6 |
    | `command-dist` | `dist build`, `dist sign`, `dist use` | `08:402`; `07:1852`, `:1869` |
    | `command-key` | `key generate <prefix>`, which writes `<prefix>.key` and `<prefix>.pub` in the format of ADR-020 decision 4 | ADR-020 decision 21, which leaves the name to the CLI spec (`00:137`) |

    `plugin purge` joins `command-plugin` at S3 (ADR-060 decision 47). A later command
    joins the row of its group, or a row of the plugin that owns it, as `slack manifest`
    is a contribution of the Slack plugin (`06:312`), placed as question Q8 decides.

    **The CLI spec.** `00:137` leaves exact names to "the CLI spec", as do ADR-020
    decision 21 and `09:840` (A4). The CLI spec is the help of `aiopsctl`: `aiopsctl help
    <path>` prints each command's synopsis and flags from its `Spec` (decision 2), and a
    golden file of that output per command, under `testdata/help/` of the command row,
    holds it in review, so a change to a name or a flag is a change of that file. Its
    first content is the table below; a flag that the documents write keeps its name.

    | Command | Synopsis | Source |
    |---|---|---|
    | `help` | `help [<word>...]`; `--help` or `-h` after any words | Decision 9 |
    | `profile render` | `profile render --profile <p> [--home <dir>] [--bundles <dir>] [--dist <dir>] [--patch <file>]... [--role <role>]... [--server] [--dump] [--dump-hooks] [--dump-config-schema]` | `08:359`-`364`; `cmd/aiopsctl/run.go` at S1, with `--dist` (decision 42) |
    | `profile verify` | `profile verify` | `08:366` |
    | `profile init` | `profile init <p>` | `08:72` row 30; ADR-060 decision 27 |
    | `profile sanitize` | `profile sanitize [<p>]` | `08:72` row 30; dsh §3.4; question Q9 |
    | `plugin new` | `plugin new <registry> <id> --lang go [--contrib]`; `plugin new connector <id> [--contrib] [--against <dist.yaml>] [--plugin <id>]...` | `07:1528`, `:1551`; ADR-047 decision 12; ADR-060 decision 24. `--lang python` arrives at S2b (`07:1551`) |
    | `plugin check` | `plugin check [<dir>] [--write] [--json] [--against <dist.yaml>]` | `07:1531`, `:1543`, `:1565`; ADR-060 decision 23 |
    | `plugin smoke` | `plugin smoke <dir> --against <release> [--builder image\|local]` | `07:1578`; ADR-044 decision 1 |
    | `plugin pack` | `plugin pack [<dir>] [--out <dir>]` | `07:1533`; ADR-047 decision 1 |
    | `plugin sign` | `plugin sign --key <file> [--out <dir>]` | `07:1859`; ADR-020 decision 4; ADR-047 decision 1 |
    | `plugin push` | `plugin push <registry> [--out <dir>]` | `07:1859`; ADR-047 decision 1 |
    | `plugin inspect` | `plugin inspect <spec>` | `08:373`; `07:1860` |
    | `plugin verify` | `plugin verify <spec> [--builder image\|local]` | ADR-060 decisions 31 and 46; ADR-044 decision 1 |
    | `plugin gate` | `plugin gate <spec> --against <dist.yaml>` | `08:493` |
    | `plugin install` | `plugin install <spec> [--profile <p>]` | `08:395`-`397` |
    | `plugin enable`, `disable` | `plugin enable <row> [--profile <p>]`; `plugin disable <row> [--profile <p>]` | `08:375`, `:395` |
    | `plugin remove` | `plugin remove <id> [--profile <p>]` | ADR-047 decision 10 |
    | `trust add` | `trust add <public key file> --ids <pattern>[,...] [--runtimes <runtime>[,...]] [--signed <key or hook>[,...]]`; `trust add <public key file> --distribution` | ADR-020 decision 5 |
    | `key generate` | `key generate <prefix>` | ADR-020 decision 21, which leaves the name to this spec |
    | `dist build` | `dist build <dist.yaml> [--modcache <dir>] [--mirror <prefix>=<site prefix>]... [--allow-bump <module>]... [--builder image\|local]` | `08:402`; ADR-044 decisions 1, 7, 9 and 10 |
    | `dist sign` | `dist sign <image> --key <file>` | `07:1868`; ADR-020 decision 12 |
    | `dist use` | `dist use <image>` | `07:1869`; ADR-020 decision 12 |

    `--builder` defaults to `image`, the path of a host with no toolchain (`08:402`), and
    the image's own run passes `local` (decision 21). `--profile` on a command that writes
    a profile defaults to the profile that `run/status.json` of the home names, so the
    written forms `plugin enable <row>` and `plugin install <spec>` (`08:395`-`397`;
    `07:1855`) work against a running process; with no such file it is a usage error,
    and a step of the exit test with no running process passes `--profile`, as step C3
    of ADR-060 decision 30 does.

### Tests

31. Beside the suites of decisions 5 and 18, and under the standing rules of ADR-060
    decision 32:
    - T3: CI boots the `cli` profile through the built `aiopsctl`, and `aiopsctl help`
      exits 0 and lists every command of decision 30, which needs every row of
      `aiops-cli` ACTIVE.
    - Exit codes: no argument exits 2; an unknown path exits 2; a command whose row a test
      detaches exits 1 and names the row; a command interrupted by a signal exits 1,
      launcher code 8 under `Command`; a command that returned 2 and then leaked exits
      1; a panicking command exits 1 with its stack; a denial and `rebuild-required` exit
      1 with the reason on stderr and nothing on stdout but what decision 11 allows; each
      outcome of the submission table of decision 17 exits with its code.
    - The lock: of two concurrent writes to one home, the second waits and, with a short
      bound in the test, fails naming the holder; a writer killed while it holds the lock
      leaves a home that the next writer locks at once; `write.lock` keeps its inode
      across writes.
    - The restore of decision 17 after a kill, a killed restore and an interrupt.
    - Each refusal of decision 17: a row of `aiops-cli` in the profile `cli`, a required
      row, the last provider of a bound key, each refused form of `trust add`, and a home
      owned by another user.
    - The distribution rule of decision 26, with a release `aiopsctl` after `dist use` of
      a site image.
    - The goldens of decision 27, with the dump and the hook dump of the `cli` profile,
      and the help goldens of the CLI spec (decision 30).
    - With question Q9 (a), a broken site layer: the first boot stops, the second runs,
      and `profile sanitize` sets the file aside.

### What this ADR does not settle

32. Each item names its trigger.
    - The transport of the submission before `admin-api`, its plugin vet rule, its bound,
      and how it relates to `admin-api`: ADR-062, the ADR of ADR-060 decision 48. The row
      `reload-socket` of `aiops-core` serves `<home>/run/reload.sock` from S2a to S6a, and
      the `aiops-cli` bundle disables it, so the `cli` profile, which `aiopsctl` composes
      with `aiops-core`, serves no socket (ADR-062 decisions 1 and 2). Its version 1
      carries `install`, `enable`, `disable` and `remove`; the other writes of the home
      answer as decision 17 says until S6a, when the commands move to `admin-api` and
      `plugin-manager` (ADR-062 decision 15).
    - Commands of rows outside `aiops-cli`: `slack manifest` (`00:137`; `06:312`) and
      `oncall override` (`06:638`, `:764`) are contributions of rows of server bundles,
      while the `cli` profile lists `aiops-core` and `aiops-cli` only (`08:253`).
      Question Q8 resolves `08:265` and `:269` against `00:137` and `:515`, and the step
      that builds the first of them builds its row by that answer.
    - `aiopsctl agent render` and `agent check` run the `agent-host` or `agent-k8s`
      profile in the same binary (`00:515`, `:516`), at S7 (`00:137`). Decision 25 fixes
      the profile of the process to `cli`; the S7 step adds a toolbox part that composes
      and runs a second profile inside the `aiopsctl` process, an additive minor of
      `launch`, or records another form.
    - `aiopsctl tree --effects` (`07:345`; `08:57` row 15; plugin record §15 row 15)
      prints the effect ledger of a running process. It needs a read of that process.
      The socket of ADR-062 carries only the submissions of four commands until S6a
      (ADR-062 decision 15), so `plugin-inventory` at S6a, whose listing shows effects
      (`08:373`), builds it.
    - `aiopsctl plugin start <id>` (`02:312`; `08:1288`) starts the compose deployment
      unit of a remote plugin: S2b, with the remote runtime and its one shared instance
      per plugin (`08:695`).
    - The guard of M2 for `commands`: question Q7.
    - How an operation of `aiopsctl` is audited: the step that builds `audit-chain`
      (S5, `08:698`; decision 17).
    - The required set of the `cli` profile. The signed manifest carries the set of every
      server profile (`08:455`); at S2a it holds the rows `loader`, `restart-policy`,
      `reload` and `readiness` and the key `clock` (`dist/dist.yaml`), all served by
      `aiops-core`, so the `cli` profile meets it. The step that adds a required key that
      no row of `aiops-core` provides, `sql` at S3 (`08:696`), says how the set applies
      to a profile that is not a server profile.
    - How the operations plugin dials once plugin vet refuses a dial outside `dialer`:
      question Q6.
    - What `admin-api` may dispatch (S4a); `--tenant` on `enable` and `disable` (S4b,
      `08:375`).
    - At S6a: where `plugin-manager` sits (ADR-060 decision 41); where it takes registry
      credentials, which `00:183` and `:412` keep out of the kernel (question Q6); how
      it takes the write lock (decision 19); that it names only the profile of its own
      process; and which definition owns `plugin-manager/changed`, which the library
      emits from then on (decision 17).
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
- `boot` grows by the launch 0.2 services, the toolbox plumbing, `RunOptions.Command`
  with the launcher code 8 (decision 13), the distribution rule of decision 26 and the
  field `rows` of the status (decision 21), under its provisional cap of 9,000 lines
  (ADR-060 decision 35). The kernel does not change, so ADR-060 decision 3 holds and this
  ADR proposes no addition to ADR-034.
- No dependency is added: the parser, the lock and the snapshot use the standard
  library, and the registry credentials are read through the OCI client that ADR-047
  decision 4 already links (ADR-060 decision 34).
- Provisional caps for the new packages, under the rule of ADR-060 decision 35:
  `sdk/def/commands/v0` and `sdk/def/operations/v0`, and the fake and the suite of each,
  800 lines each; `plugins/cli`, 1,500, as a command row's plugin; `plugins/operations`,
  the 3,500 of the operations library.
- The commit that adds `command-profile` removes the S1 code of `cmd/aiopsctl/run.go`,
  `profile render` with `--dist`.
- The design follows dsh as `08` §1.1 carries it: typed launch services (row 31), a
  startup row that parses the leftover argv (dsh §7; row 32), and one write lock that the
  CLI and the manager share, with a snapshot restored on a failure (dsh §4.2; row 34).
  It departs in four places, each recorded in the checklist with its reason or deferred
  with its trigger (Edits on acceptance; decision 32): the two binaries of one launcher
  differ by the toolbox and by what their `main` passes to `boot` (row 31); the lock is
  one per home and has no idle timeout (row 34); `plugin-manager/changed` is emitted from
  S6a (decision 17); and the CLI's commands are rows that reach their tools through a
  launch service, where dsh's CLI forwards to pnpm (row 31).

## Edits on acceptance

Made in the commit that records the owner's answers (`08:997`):

- `00` §4.1: a row for `operations` after `00:222`: "The operations library of the CLI
  and the plugin manager: profiles, plugins, trust, distributions and authoring, under
  one write lock | `-`; `C` | | `operations` | no". Plugin record §5 gains the same row,
  with the command rows and, from S6a, `plugin-manager` as consumers and an in-memory
  fake.
- `00:245`, `07:559` and plugin record §7 (M4): `operations` among the consumer-bound
  keys, now 13.
- `00:276`: `aiops-cli` holds `cli`, `operations` and the rows that contribute to
  `commands`.
- `00:137`: the key-pair command is `key generate` (ADR-020 decision 21).
- `00:212` and plugin record §5, the contract of `launch`: with question Q1 (a) it gains
  "and, through `profileContext`, the launcher's toolbox"; with question Q2 (a) it reads
  "argv, the working directory, the environment of `aiopsctl` and the standard
  streams", "the readiness latch and the end of boot" and "request a process exit, with
  the code of a command"; with question Q2 (b) only the last.
- The dsh checklist, `08` §1.1 and plugin record §15, each with its reason (`08:39`):
  - Row 31 becomes adapted, so the counts of `08:32`-`37` and of the head of plugin
    record §15 read 20 carried and 24 adapted. It gains: "One launcher library, `boot`,
    in two binaries. `aiopsctl` alone carries the toolbox (the builder, `plugingen`, the
    analyzer, the templates, the suites and the scenarios), which its command rows reach
    through `profileContext`, and its `main` turns off the server rule and the status
    file, keeps stdout for commands and maps exit codes. What is lost: one binary for
    every app. Reason: Go links statically (row 35), and what the builder links enters
    no server binary (ADR-044, Consequences), so a server image cannot carry the CLI's
    tools as dsh's one launcher reaches pnpm (ADR-061)."
  - Row 32 gains: "In `aiopsctl` the startup row is `cli`, which parses the leftover argv
    once boot has finished (ADR-061)."
  - Row 34 gains, with question Q5 (a): "One write lock per home, not per profile: a site
    holds one digest per plugin id and the site files that an install writes (ADR-047
    decision 7), since a remote plugin has one shared instance per site (`08:386`). It
    is the system's advisory lock, held only through bounded steps and released when its
    holder exits, so it has no idle timeout; what is lost is that a waiter cannot break
    the lock of a live holder that hangs. The snapshot also covers the verified records
    and stays on disk, so the next writer restores a killed write (ADR-061)."
- ADR-059 decision 29: the launcher code 8, `ExitInterrupted`, under `Command`, and the
  codes that `Command` changes (decision 13). ADR-059 decision 31: `run/status.json`
  gains `rows` (decision 21).
- `07:1344`: `boot` is called from the `main` of `aiops` and of `aiopsctl`.
- `08:369` and `03:71`: the operations library is the plugin `aiops/operations` under the
  key `operations`, and the write lock is `$AIOPS_HOME/write.lock`.
- ADR-047 decision 13: the rows of `write.lock` and `snapshot/`. ADR-047 decision 14:
  `write.lock` is the one file not replaced by a rename (decision 19). ADR-060 decision
  24: the template of `commands`. ADR-060 decision 35: the caps of the Consequences.
  ADR-059 decision 29 and ADR-060 decision 23: the exit table is recorded in the help of
  the `cli` row and in the exit mapping of `cmd/aiopsctl`.
- ADR-060 decision 23: the command `profile sanitize`, with question Q9 (a) or (b).
- The lines that the chosen options of questions Q1, Q3, Q6, Q7, Q8 and Q9 name.

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
  `profileContext` for `reload`. Two costs. The operations plugin can also call
  `Commit`, which only `reload` calls today: in the `cli` process that is its own
  composition, and in a server profile from S6a a power the manager's path does not
  need. And every other consumer of `profileContext`, `loader`, `reload` and
  `readiness` (`dist/dist.yaml`), can call `Build`, `Suite` and every other part, so the
  operations plugin is the one consumer of the toolbox by review only, not by binding;
  in `aiops` the parts of `Tools` answer `ErrNotCarried`, so there that reach covers
  `boot`'s parts alone. `00:212` and plugin record §5 name the toolbox in the contract
  of `launch`.
- (b) A seventh launch key, `toolbox`, class `PC`, consumer-bound to `aiops/operations`
  and provided by every launcher, with the parts that only `aiopsctl` carries absent in
  `aiops`. The operations plugin gets nothing else of `profileContext`, and no other row
  reaches the toolbox. "Six" changes to "seven" in `00:212`, `03:63`, `07:1348`,
  ADR-059 decision 35 ("the launch keys stay six"), plugin record §5, plugin record §15
  row 44 ("6 launch keys", and the 36 provider-bound keys it counts) and
  `sdk/def/launch/v0/doc.go`, and the consumer-bound keys go to 14.
- (c) The builder, `plugingen`, the analyzer, the templates and the suite runner become
  internal packages of the operations plugin, and only `boot`'s parts go through
  `profileContext`, as in (a). The server image then links them, against ADR-044's
  Consequences, and ADR-060 decisions 11 and 22 change their homes.
- Recommended: (a). It keeps every written count and boundary and follows the placement
  the owner chose for `reload` (ADR-059 decision 35). Its costs are the reach of
  `Commit` and the reach of the toolbox from `loader`, `reload` and `readiness`; (b)
  removes both at the price of a seventh key. Needed before the first command row
  merges.
- **Answered 2026-11-23: (a)**, decision 33.

**Q2. Where does the `cli` row get its streams, its working directory and environment,
the end of boot and its exit code?** `cmdline` serves argv only, `appExit` carries no
code, and no launch service says when boot has finished (`00:212`;
`sdk/def/launch/v0/launch.go`). Decision 13's `Command` holds under either option.
- (a) Launch 0.2 as decision 13 states: `Cmdline` gains the three streams, `Dir()` and
  `Environ()`, filled in `aiopsctl` only; `Ready` gains `Booted()`; and `ExitRequest`
  gains `Code` with the reason `ExitCommand`. The `cli` row and each command row mount
  alone with the launch fake and a test's buffers, directory and environment (`03:81`).
- (b) Only `ExitRequest` gains `Code`. The `cli` row uses the streams, the working
  directory and the environment of the package `os`, and dispatches once `composition`
  has shown no row loading for an interval it measures on `clock`. A test of the row
  replaces the process's streams and environment, and the interval is a guess at a fact
  that the launcher already knows.
- Recommended: (a). The launcher owns the streams, the settle and the exit code
  (`07:1344`), and (a) hands each to the row as a launch service, as dsh's launcher
  provides argv, the exit and readiness before any row mounts (dsh §7). Needed before the
  `cli` row merges.
- **Answered 2026-11-23: (a)**, decision 34.

**Q3. What is the operations definition called?** The naming rule reads "A singular key
is one engine or policy. A plural key is a registry of contributions" (`00:206`) and
"singular for one engine or policy, plural for a registry" (`00:656`). The operations
definition is one engine and takes no registration (decision 18), so a plural key
departs from both lines; `jobs`, `secrets`, `tokens` and `credentials` are plural keys
of one engine that `00` already holds (`00:216`, `:218`, `:224`, `:226`).
- (a) `operations`: definition `sdk/def/operations/v0`, key `operations`, plugin
  `aiops/operations`, row `operations`. It is the documents' word for the library
  (`08:369`; `07:1846`; plugin record §12) and dsh's file name (dsh §4.2). It departs
  from `00:206` and `:656`, and `00:656` gains "`operations`, the one operations library
  of `08:369`" beside its examples, as the four keys above already depart.
- (b) `siteOperations`, plugin `aiops/site-operations`, row `site-operations`. It names
  the scope, the site's home, and reads apart from the hook `reload/operation`. It is
  plural too, departs from the same two lines with the same edit, and is a name the
  documents do not use.
- (c) `siteManager`, definition `sdk/def/sitemanager/v0`, plugin `aiops/site-manager`,
  row `site-manager`: a singular key for one engine, as `00:206` and `:656` ask, in the
  form of `pluginManager` (`00:222`). The library keeps the documents' name, "the
  operations library", and only the key and the plugin take the new name; `08:369`,
  `03:71` and plugin record §12 name the plugin.
- Recommended: (a). It is the word every document and dsh use, and the departure it
  needs is one that `00` already makes four times; (c) keeps the naming rule whole at
  the cost of a second name for one thing. Needed before the definition merges.
- **Answered 2026-11-23: (a)**, decision 35.

**Q4. How many command rows?**
- (a) One per command group: five at S2a (decision 30). A group is one row that an
  operator can enable or disable, and each stays under its cap.
- (b) One per command: twenty-one at S2a, twenty-two with `profile sanitize` (question
  Q9), each with its own manifest, tests and T3 boot.
- (c) One row for every S2a command, whose plugin would pass the cap of 1,500 lines of a
  command row (ADR-060 decision 35).
- Recommended: (a). It is the fewest rows that keep each plugin under its cap. Needed
  before the first command row merges.
- **Answered 2026-11-23: (a)**, decision 36.

**Q5. What does the write lock cover?** `08:369` names "one profile write lock", and dsh
calls its lock "a profile write lock shared with the CLI" without stating its scope (dsh
§4.2). Install also writes files of the site, `verified/`, `bundles/` and `content/`;
`dist use` writes `verified/` and `image.yaml`; and `trust add` writes `trust.yaml`
(ADR-047 decision 13; ADR-020 decision 10).
- (a) One lock per home (decision 19). Every write excludes every other, and two profiles
  of one home are never written at once.
- (b) One lock per profile, `profiles/<p>/write.lock`, and one for the site files, taken
  in that order. Two profiles can be written at once, and each method states which locks
  it takes.
- Recommended: (a). An install writes files of both kinds in one operation, and (a) makes
  it exclusive with one lock and no order to get wrong; its cost, serial writes to one
  home, is nothing for one site. Row 34 of the dsh checklist records it with its reason
  (Edits on acceptance). Needed before the first method that writes the home merges.
- **Answered 2026-11-23: (a)**, decision 37.

**Q6. How does the operations plugin dial once plugin vet refuses a dial outside
`dialer`?** The vet gains "no dial and no HTTP client outside `dialer`" for every plugin
(harness:58), and T0 runs plugin vet on every commit (`07:1400`). The operations plugin
pulls and pushes OCI artifacts (ADR-060 decision 41; ADR-047). `dialer` is grouped with
`http` (`00:217`), which arrives at S3 (`08:696`); its one provider, `dialer-guarded`, is
a row of `aiops-base` (`00:277`), which the `cli` profile does not list. From S6a
`plugin-manager` would pull artifacts in a server process, where the cell egress fence of
`00` §5.7 applies, and where registry pull credentials may not be: they belong to "the
orchestrator and `aiopsctl`, never the kernel" (`00:412`; `00:183`).
- (a) A deferral of the dial clause of `07:1400` and harness:58 until `dialer` exists:
  the vet rule binds from the step that builds `dialer`. At S2a the operations plugin
  dials directly, and before the rule merges, the step that builds `dialer` says how the
  operations plugin dials in the `cli` profile and in a server profile, and the ADR of
  S6a says how `plugin-manager` pulls without credentials in the kernel.
- (b) The vet rule gains a recorded exception for the operations plugin in every profile,
  as the upstream register lists an importer by an accepted decision (ADR-054). A pull by
  `plugin-manager` in a server process then bypasses the cell egress fence, and the
  credential conflict with `00:412` stays for S6a.
- (c) S2a brings `sdk/def/dialer/v0` forward, with its fake, its suite and a provider row
  that the `cli` profile composes, `dialer-guarded` in `aiops-cli` or a row of its own
  there, and the operations plugin dials through it; the vet rule then binds from S2a
  with no exception. It adds a definition, a provider and its cell-range rule to S2a,
  which ADR-060 does not list, and the credential conflict with `00:412` stays for S6a.
- Recommended: (a). It defers a rule that cannot be met before `dialer` exists without
  growing S2a, and names the step that ends the deferral; (b) opens the egress fence of a
  server process, and (c) keeps the rule whole at S2a at the price of a definition that
  S2a does not plan. Needed before the vet rule of harness:58 merges, or before S2a
  closes if the owner reads that rule as in force at S2a.
- **Answered 2026-11-23: (a)**, decision 38.

**Q7. What of the owner-side guard does the `commands` dispatcher apply?** `07:511` (M2)
has a dispatcher apply "a deadline, a concurrency cap and a breaker per (tenant, row), an
`Enter` lease on the contribution's fiber, the invariant, then the call". The `cli` row
dispatches one command per process; that command runs as long as its work, minutes for
`dist build` and `plugin smoke`, and the operator ends it with a signal (decision 10).
`commands` holds no tenant data (decision 1).
- (a) At S2a `Dispatch` applies the lease and the invariants of decisions 8 to 10, and
  recovers a panic (decision 3), with no deadline, cap or breaker: with one call per
  process a cap of one and a breaker over one call never act, and a deadline would end a
  build that is still working. The deadline, the cap and the breaker per (tenant, row)
  arrive with the first caller that dispatches more than once in a process, `admin-api`
  at S4a, whose step builds them with their guard cases. This defers three parts of
  `07:511` for `commands` until S4a, and `07:511` gains that sentence.
- (b) `Dispatch` applies the whole guard at S2a: each `Spec` states a deadline, the
  longest for `dist build`, `plugin smoke` and `plugin verify`; a cap of one call per
  row; and a breaker per row that opens after repeated failures. The guard half tests
  each. In `aiopsctl` the cap and the breaker never act, and a deadline set too short
  ends a build that is still working.
- Recommended: (a). The parts it defers have nothing to act on in a process that runs one
  command, and S4a builds them where they act, with tests that can fail. (b) keeps
  `07:511` whole at S2a at the cost of code that never acts in `aiopsctl` and a deadline
  guessed per command. Needed before the `cli` row merges.
- **Answered 2026-11-23: (a)**, decision 39.

**Q8. How does a command of a row outside `aiops-cli` reach the `cli` profile?**
`00:137` makes `slack manifest` "a `commands` contribution of the optional Slack bundle
... present only where that bundle is enabled", `06:312` makes it a contribution of the
row `chat-surface-slack`, and `00:515` and `08:271` say that a plugin contributes its own
commands through `commands`. `08:265` and `:269` mark the optional Slack bundle and the
bundles of installed artifacts "-" in the `cli` column, so neither enters the `cli`
profile. The rows of `aiops-chatops-slack` need keys that only server bundles serve, so
`chat-surface-slack` would stay PENDING in the `cli` profile and never register its
command. dsh installs a plugin into any profile (dsh §4.2).
- (a) Every command row is a row of `aiops-cli`. `slack manifest` becomes a command row
  of the Slack module in `aiops-cli`, present in every `cli` profile; `00:137` and
  `06:312` are edited, and `00:515` and `08:271` read "a first-party or `contrib`
  plugin". A third-party plugin contributes no command, and `08:265` and `:269` hold.
- (b) A bundle that holds a command ships it in a companion bundle of command rows that
  need only what the `cli` profile serves, `aiops-chatops-slack-cli` for `slack
  manifest`, which the `cli` column of the table of `08:265` lists as optional; `06:312`
  names the command row of the Slack module in it. An installed in-process artifact may
  be installed into the profile `cli`, its rows disabled, and the operator enables its
  command row there; the `cli` cell of `08:269` becomes "last". `00:137` and `00:515`
  hold. A third-party command row then runs in `aiopsctl` and sees the invocation's
  environment, registry credentials and key password included (decision 17), though not
  `operations`, which is consumer-bound (decision 15); its signer is trusted for that as
  for any in-process code it installs.
- (c) At S2a `Install` refuses `--profile cli`, as `08:269` writes, the template serves
  first-party and `contrib/` command rows, and the step that builds `slack manifest`
  (P1, `00:137`) chooses between (a) and (b).
- Recommended: (b). It keeps `00:137` and `00:515` whole and follows dsh, where every
  profile takes an installed plugin; its cost at S2a is one rule of `Install` (decision
  17). (a) removes a written extension point, and (c) leaves the template of decision 6
  with no third-party author until P1. Needed before the profile rule of `Install`
  merges.
- **Answered 2026-11-23: (b)**, decision 40.

**Q9. How does `aiopsctl` recover when a layer file of the home stops its own boot?** A
malformed or empty layer stops boot (`08:465`), and the site layer applies to every
profile, `cli` included (`08:123`). A broken `$AIOPS_HOME/aiops.patch.yml` or
`profiles/cli/aiops.patch.yml` therefore stops every `aiopsctl` command, the ones that
would repair the home included. dsh renames a broken patch to `.bak-<timestamp>` without
loading plugins (`sanitizeProfile`, dsh §3.4), and `08:72` and plugin record §15, row 30,
carry "`aiopsctl profile init` and `sanitize`" with no step named.
- (a) S2a builds `profile sanitize [<p>]` as a command of `command-profile`. Under the
  write lock it renames each layer file of the home that is malformed or empty, the site
  layer and the profile layer of `<p>`, to `<name>.bak-<timestamp>`, parsing it and
  loading no plugin (dsh §3.4). When the boot of the `cli` profile stops on such a file,
  `cmd/aiopsctl` boots once more with the shipped `cli` profile alone and the home still
  passed, prints which file stopped the first boot, and dispatches. The first boot stops
  as `08:465` says; the second composes no layer of the home, so a command in it does
  not see the site's patches of the `cli` profile, and stderr says so.
- (b) No second boot: `aiopsctl` exits 1, naming the file and the `mv` that sets it
  aside, and `profile sanitize` is built at S2a for the profile layers of other profiles
  only.
- (c) `profile sanitize` is code of `cmd/aiopsctl` that runs before boot, as dsh's runs in
  its launcher, and ADR-060 decision 53, under which every S2a command is a row, gains
  that exception.
- Recommended: (a). It carries row 30 at S2a, keeps `08:465` for every boot, and keeps
  every command a row; (b) leaves a broken site layer to be found by hand, and (c) breaks
  an accepted decision. Needed before `command-profile` merges.
- **Answered 2026-11-23: (a)**, decision 41.

## Owner answers of 2026-11-23

The project owner accepted this ADR on 2026-11-23 and answered every question of the
section "Questions for the owner" on the same date, each with its recommended option:
(a) for every question but Q8, and (b) for Q8. The boot changes that the ADR names are
accepted with it: `boot.RunOptions.Command` and the launcher code 8, `ExitInterrupted`,
which ADR-059 decision 29 gains (decision 13), and the field `rows` of `run/status.json`,
which ADR-059 decision 31 gains (decision 21). Each decision below names the question it
answers. Where a decision above leaves a point to a question, the decision here settles
it.

33. **The operations plugin reaches the code beside the module table through
    `profileContext`** (Q1 (a)). `ProfileContext` gains `Toolbox()` in launch 0.2, which
    returns the toolbox of decision 21, and the release manifest lists `aiops/operations`
    among the consumers of `profileContext` (decision 20). The launch keys stay six
    (ADR-059 decision 35). Two costs are accepted with it: the operations plugin can call
    `Commit`, and `loader`, `reload` and `readiness` can call every part of the toolbox,
    so the operations plugin is its one consumer by review, not by binding; in `aiops`
    the parts of `Tools` answer `ErrNotCarried`. `00:212` and plugin record §5 name the
    toolbox in the contract of `launch`.

34. **Launch 0.2 hands the `cli` row its streams, its working directory and environment,
    the end of boot and its exit code** (Q2 (a)), as decision 13 states. `Cmdline` gains
    `Stdin()`, `Stdout()`, `Stderr()`, `Dir()` and `Environ()`, the last two filled by
    the launcher of `aiopsctl` only; `Ready` gains `Booted()`, closed once the tree has
    settled and the run-time required audit has passed; and `ExitRequest` gains `Code`,
    read only with the reason `ExitCommand`. The `cli` row and each command row mount
    alone with the launch fake and a test's buffers, directory and environment
    (`03:81`). `00:212` and plugin record §5 name the three in the contract of `launch`.

35. **The operations definition is `operations`** (Q3 (a)): definition
    `sdk/def/operations/v0`, key `operations`, plugin `aiops/operations` in
    `plugins/operations/`, row `operations` of `aiops-cli` (decision 14). The plural key
    of one engine departs from `00:206` and `:656`, as `jobs`, `secrets`, `tokens` and
    `credentials` do, and `00:656` names it beside its examples.

36. **One command row per command group** (Q4 (a)): `command-profile`, `command-plugin`,
    `command-trust`, `command-dist` and `command-key` at S2a (decision 30), each under the
    cap of 1,500 lines of ADR-060 decision 35.

37. **One write lock per home** (Q5 (a)): `$AIOPS_HOME/write.lock`, held for the whole of
    each method that writes the home, as decision 19 states. Every write excludes every
    other, and two profiles of one home are never written at once. Row 34 of the dsh
    checklist records it with its reason.

38. **The dial clause of plugin vet waits for `dialer`** (Q6 (a)). The rule "no dial and
    no HTTP client outside `dialer`" of `07:1400` and harness:58 binds from the step that
    builds `dialer`, and at S2a the operations plugin dials directly. Before that rule
    merges, the step that builds `dialer` says how the operations plugin dials in the
    `cli` profile and in a server profile, and the ADR of S6a says how `plugin-manager`
    pulls without registry credentials in the kernel (`00:183`, `:412`).

39. **At S2a `Dispatch` applies the `Enter` lease and the invariants of decisions 8 to 10,
    and recovers a panic** (Q7 (a)), with no deadline, concurrency cap or breaker. The
    deadline, the cap and the breaker per (tenant, row) of `07:511` (M2) arrive with the
    first caller that dispatches more than once in a process, `admin-api` at S4a, whose
    step builds them with their guard cases. This defers those three parts of `07:511`
    for `commands` until S4a, and `07:511` says so.

40. **A command of a row outside `aiops-cli` reaches the `cli` profile in a companion
    bundle** (Q8 (b)). A bundle that holds a command ships it in a companion bundle of
    command rows that need only what the `cli` profile serves: `aiops-chatops-slack-cli`
    for `slack manifest`, which the `cli` column of the table of `08:265` lists as
    optional, with the command row of the Slack module in it, `command-slack` by the rule
    of decision 30, which `06:312` names. An installed in-process artifact may be
    installed into the profile `cli` after `dist use` there, its rows disabled, and the
    operator enables its command row there; `Install` refuses a pack and remote code into
    that profile, since a pack's rows contribute no command and `commands` takes no
    remote contribution (decision 17; `00:222`). The `cli` cell of `08:269` becomes
    "last", and `00:137` and `00:515` hold. A third-party command row then runs in
    `aiopsctl` and sees the invocation's environment, registry credentials and key
    password included (decision 17), though not `operations`, which is consumer-bound
    (decision 15); its signer is trusted for that as for any in-process code it installs.
    The template of decision 6 serves its author.

41. **S2a builds `profile sanitize [<p>]` as a command of `command-profile`** (Q9 (a)).
    Under the write lock it renames each layer file of the home that is malformed or
    empty, the site layer and the profile layer of `<p>`, to `<name>.bak-<timestamp>`,
    parsing it and loading no plugin (dsh §3.4). When the boot of the `cli` profile stops
    on such a file, `cmd/aiopsctl` boots once more with the shipped `cli` profile alone
    and the home still passed, prints which file stopped the first boot, and dispatches.
    The first boot stops as `08:465` says; the second composes no layer of the home, so a
    command in it does not see the site's patches of the `cli` profile, and stderr says
    so. ADR-060 decision 23 gains the command. Note of 2026-11-25, a departure recorded
    as built: the first boot is predicted, not run. Before it boots, the launcher of
    `internal/aiopsctl` composes the `cli` profile of the home with `boot.Compose`,
    which is what boot runs first, and when that stops on the site layer or the profile
    layer of `cli` it boots once, with the shipped `cli` profile alone and the home
    still passed, and prints the file and the stop; so the output and the outcome are
    those of a first boot that stops as `08:465` says, with one boot less. Note of
    2026-11-26 (review round over the whole step): `profile sanitize <p>` also sets
    aside `profiles/<p>/plugins.lock` where it does not read, whose rows boot detaches,
    so `plugin remove` can take the artifact again; and `plugin install` names, and does
    not stop on, a lock of another profile that does not read, which it then does not
    check for another digest of the id (ADR-047 decision 7).

**Edits made with this record.** `00` is edited in the commit that records the answers
(`08:997`), and that commit makes the edits that "Edits on acceptance" names for the
options chosen: in `00`, the row of `operations` after `:222`, with the class `C` of
decision 15, `operations` among the consumer-bound keys of `:245`, the bundle
`aiops-cli` of `:276`, `key generate` in `:137`, the contract of `launch` in `:212` in
the wording of Q1 (a) and Q2 (a), and the example of `:656` (decision 35); `03:71`;
`06:312` (decision 40); `07:511` (decision 39), `:559`, `:1344` and `:1400` (decision
38); `08:34`-`35`, rows 31, 32 and 34 of the dsh checklist, a row of
`aiops-chatops-slack-cli` after `08:265` and the `cli` cell of `:269` (decision 40), and
`:369`; harness:58 (decision 38); plugin record §5, in the contract of `launch` and with
the row of `operations`, §7 (M4) and §15, in its head and rows 31, 32 and 34; ADR-059
decisions 29 and 31; ADR-060 decisions 23, 24 and 35; and ADR-047 decisions 13 and 14.
Lines that restate edited text are edited with them: the row `operations` among the
consumers of `launch` in plugin record §5 (decision 33); plugin record §12, which
restates `08:369`; row 45 of the dsh checklist in `08` §1.1 and in plugin record §15,
which restates the count of consumer-bound keys of `07:559`; a row of
`aiops-chatops-slack-cli` in `00` §4.4, whose row counts the table of `08` §1.4 carries;
`08:271`, which names the companion bundle and says that install into the profile `cli`
takes in-process code only (decision 40); and the name `key generate` in ADR-060 decision
23, which restates `00:137`. Left to later commits: the ADR table of `08` §3 and the next
free number of `08:997`, which the commit that updates that table for the ADRs accepted on
2026-11-23 edits; `06:764`, where the built-in on-call provider registers
`oncall override` with `commands`, and the lists of optional bundles in `00:519` (§6.3),
`01:254`, `02:178` and `:324`, `07:1880`, and `08:279`, `:399`, `:729` and `:1470`, which
the steps that build `oncall override` and `slack manifest` (P1, `00:137`) edit when they
build their command rows by decision 40 (decision 32). Until then those lists omit
`aiops-chatops-slack-cli`, which `00` §4.4 and `08` §1.4 list. An option the owner did not
choose edits nothing.

## Owner answers of 2026-11-24

The project owner answered on 2026-11-24 three points that the work of S2a raised after
the acceptance of this ADR. Each decision below records one; where a decision above says
otherwise, the decision here holds.

42. **`aiopsctl` keeps `--dist`** (amends decision 27). `profile render` keeps the flag
    `--dist <dir>`, which renders against the signed manifest `distribution.yaml` of a
    directory instead of the one `aiopsctl` embeds. The goldens of the test distribution
    stay in `cmd/aiopsctl/testdata/test-dist/` and compose through `--dist`. The option
    that decision 27 recommended, a separate `aiopsctl-test` binary under
    `test/dist/cmd/` over the test distribution's module table, is declined, so no such
    binary is signed or guarded. The CLI spec of decision 30 lists `--dist` for `profile
    render`.

43. **A write that the wire does not carry answers `restart-required` and exits 0 while
    a process runs.** `profile init`, `profile sanitize` and `trust add` answer
    `Outcome` `restart-required` with the reason `not-submitted` and exit 0 when
    `run/status.json` names a running process of the profile they changed, or for `trust
    add` of the profile the file names, as the table of decision 17 says. Version 1 of
    the wire of ADR-062 carries `plugin install`, `enable`, `disable` and `remove` only
    (ADR-062 decisions 6 and 15), so the running process reads the change at its next
    start; the files are kept.

44. **`Change` carries the submission of the four commands, and `Outcome` only for
    `not-running` and `restart-required`**, as decision 16 says. For `Install`,
    `Enable`, `Disable` and `Remove`, `Change` carries `Result`, the final result as the
    wire carries it, or with none `Error`, the error line before an id, or `IDs` and
    `Last`, each operation id and the last status. `Outcome` is set only where no
    process runs the profile (`not-running`) and where one runs and nothing answers
    (`restart-required`). The rows of the table of ADR-062 decision 11 that end with no
    final result take the names below, which the definition `sdk/def/operations/v0`
    fixes when the four methods join it, and with which the text of the error of each
    begins; `aiopsctl` exits 1 on each.

    | Row of ADR-062 decision 11 | Name | `Change` | Files |
    |---|---|---|---|
    | An error line before an id exists, `peer`, `busy`, `request` or `version`, or a refusal of `Submit` | `refused` | `Error` | Restored |
    | No final result on the second submission either | `no-final-result` | `IDs` and `Last` | Restored after `abandoned` without `state-unknown`; kept otherwise |
    | `--wait` elapsed, or the connection was lost beyond the connect window, after the operation was accepted | `verdict-unknown` | `IDs` and `Last` | Kept |
    | A process runs the profile and nothing answers within `--connect-wait` | `restart-required`, reason `no-answer` | `Outcome` and `Reason` | Kept |

    A final result that does not show the change keeps its status as its name:
    `rejected`, `rolled back`, `rollback failed` and `abandoned` with `state-unknown`
    restore the files, and a `passed` that does not show the change keeps them and names
    the row and the later layer that overrides it. `operations.ErrSubmission` matches
    the error of every row that exits 1, and `operations.ErrRebuildRequired` the
    `rebuild-required` of `Install`, which writes nothing and returns the `dist.yaml`
    lines in `Change.DistLines`.
