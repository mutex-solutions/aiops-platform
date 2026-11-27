# ADR-061: The `commands` definition, the `cli` profile and the operations plugin

- Status: **accepted 2026-11-23 by the project owner.** The owner answered all nine
  questions on the same date with the recommended option of each: (a) for Q1 to Q7 and
  Q9, (b) for Q8. The section "Owner answers of 2026-11-23" records them as decisions 33
  to 41, with the boot changes this ADR names. ADR-060 decision 53 had it proposed before
  the first command row merges, and ADR-060 decision 2 required this acceptance before
  that code merges into `main`. Brought to ADR-062 on 2026-11-24, as its decision 16
  asks: decisions 10, 16, 17 and 32 and the scope name its transport and return the
  outcomes of its decision 11.
- Status of the S2b part: **proposed 2026-11-27, awaiting the project owner.** The
  section "S2b part, proposed 2026-11-27" holds it, as decisions 45 to 55 and questions
  Q10 to Q13. ADR-063 decision 2 has it written as a dated addition to this ADR,
  proposed before the code it governs and accepted before that code merges into `main`.
  Until the owner accepts it, decisions 1 to 44 stand as they are.
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
      writers, and `profile render` prints a warning that names the snapshot. Note of
      2026-11-27 (round 2): only where no writer holds `write.lock`; a snapshot under a
      held lock is a live write's, which restores or keeps its files itself, so the
      server boots, as the process that replaces one killed mid-submission must (ADR-062
      decision 10).
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
      config. No cap was raised. Note on the owner's answer of 2026-11-26 (item 6 of
      the S2a report's open questions), as built: suite revision 7 of `operations` adds
      O27, the group clash with an artifact of another owner: with `acme/connector-x`
      installed into `dev`, `plugin install` of `beta/connector-x` is refused naming
      `acme/connector-x`, and writes nothing (ADR-047 decision 17). The mount of the
      operations row reads the id of a plugin directory from its `plugin.yaml`, and the
      fake packs under that id and names the artifact the profile holds; the signed
      manifests and the golden dumps that print their digest are regenerated.
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
      has no hook to stop there. Note of 2026-11-26, the owner's word: the owner accepted
      on 2026-11-26 that departure, the stop after the first rename alone held by the
      generic write test only (open question 8 of the S2a report).

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
    | `GateRequest` | `Entry`; the artifact's `Manifest`, `Bundle` and `Attestation` bytes and its `Digest`; `Signer`, the key id that the caller verified, `unsigned`, or empty where the entry point verifies none; `Home` and `Profile` for install; `Against`, the bytes of a published signed manifest; `Module`, what `module.json` of in-process code records at install, which the record that boot judges holds (note of 2026-11-27, review round over the whole step, round 2: without it checks 5 and 10 passed at install what they deny at the next boot) |
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
    | `plugin new` | `plugin new <registry> <id> --lang go [--contrib]`; `plugin new connector <id> [--contrib] [--against <dist.yaml>] [--plugin <id>]...` | `07:1528`, `:1551`; ADR-047 decision 12; ADR-060 decision 24. `--lang python` arrives at S2b (`07:1551`). Note of 2026-11-27: from S2b the Go form also takes `--runtime remote`, with `inproc` the default (ADR-063 decision 60) |
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
    check for another digest of the id (ADR-047 decision 7). Note of 2026-11-27 (round
    2): with the layers it sets aside read as absent, `profile sanitize` renders the
    profile once more; where that render still holds an error, a `profile.yaml`, a
    bundle or a profile that exists nowhere, it keeps what it set aside and exits 1
    naming the error and what to do, and says that nothing stops the profile only when
    nothing does.

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

## S2b part, proposed 2026-11-27

- Status: **proposed 2026-11-27, awaiting the project owner.** Decisions 45 to 55 are
  proposed and questions Q10 to Q13 are open. ADR-063 decision 2 has this part proposed
  to the owner before the code it governs and accepted before that code merges into
  `main`.
- Scope: what ADR-063 decision 34 has this part settle (`ADR-063:849-854`): `plugin
  start`, with `Start` in the operations definition as a minor with a raised suite
  revision; `plugin new --lang python`; the form of a remote Go plugin, which ADR-063
  decision 60 decided; the text and the exit code of each refusal of `plugin enable`
  (ADR-063 decision 17); what `plugin check` prints for a step that it cannot run for
  want of a container runtime; and the forms of `plugin verify` and `plugin smoke` for
  remote code. It blocks the operations library and the command rows of group 3 of
  ADR-063 decision 30, the remote Go and Python templates, and steps G1, P1, C2 and C3
  of the exit of ADR-063 decision 27. It changes no decision of `00` by itself: where an
  answer edits a line of the architecture files, the question names the line.
- Citations: `00`, `02`, `07` and `08` are the files of `docs/architecture/` with those
  numbers, and `07:1556` is line 1556 of `07` at commit `bbfd839`, the commit this part
  is written against. The sections above cite `a3c85ae`, so a line number here differs
  from theirs. ADR-063 is cited by decision and, inside a long decision, by line at the
  same commit, as `ADR-063:401`. The other ADRs are cited by decision, the plugin
  record by section, and code by file and line at the same commit. A decision number
  with no ADR name is a decision of this ADR.

### Context of the S2b part

**What is written.**

- **`plugin start`.** `plugin install` renders the deployment unit of a remote plugin
  per target, and the compose target is "a fragment and a line in the `sidecars-static`
  table, started with `aiopsctl plugin start <id>`" (`08:1293`; `02:313`; plugin record
  §11). S2b builds the compose fragment, a file of its own under the home that `plugin
  start` runs, and the systemd unit (ADR-063 decision 50). The kernel never launches or
  stops a plugin process (`08:447`, `:1293`; `ADR-063:401-403`). `plugin start` creates
  the network of an instance and attaches the instance and the kernel container to it
  (`ADR-063:626-629`). Decision 32 left the command to S2b.
- **The templates.** `aiopsctl plugin new <registry> --lang python` "writes a plugin
  skeleton with its `plugin.toml`, a test and a row snippet" (`07:1213`), for each
  registry marked `contrib` (`ADR-063:448-451`). A remote Go plugin is made with `plugin
  new <registry> <id> --lang go --runtime remote`, with `inproc` the default (ADR-063
  decision 60; `07:1556`). Each remote template writes the file from which the plugin's
  image is built (`ADR-063:455-459`). CI generates a plugin from every template and
  holds it to one state: it compiles, passes T0 and the generated tests, and fails
  conformance on the stubs (`07:1568`; `ADR-063:451-452`).
- **The refusal of `plugin enable`.** "`plugin enable` refuses, printing the exact
  command, while the endpoint is missing from the `sidecars` table or does not answer
  the handshake" (`08:398`; `07:1870`; `02:326`). The two halves come from two places
  (`ADR-063:404-418`; ADR-063 decision 54, reading 4). `aiopsctl` reads the endpoint
  table and refuses a row whose endpoint is missing before it submits anything. For a
  listed endpoint it submits, the server process dials, and an instance that does not
  answer or a denied handshake ends in the final result `rolled back`, the files
  restored and exit 1, "with the row, the reason the server gave and, where the
  instance did not answer, the exact command `aiopsctl plugin start <id>`". `aiopsctl`
  never dials an instance.
- **`plugin check` without a container runtime.** Steps 1 to 6 and step 8 need no
  container runtime in any language, and "Step 7 needs one when it packs an image"
  (`07:1570`). Without one the check "reports the step as not run and names what it
  lacks" (`ADR-063:382-384`), and step P2 of the exit reads a `--json` report that shows
  steps 1 to 6 passed and step 7 not run (`ADR-063:660-663`).
- **`plugin verify` and `plugin smoke`.** `aiopsctl plugin verify <spec>` is "the
  explicit, optional re-run of the suite against a pulled image; it is not part of
  install" (plugin record §11). `plugin smoke ./ --against <release>` runs T3 per
  runtime, and "For `remote` it attaches the plugin's image to the released image"
  (`07:1583`). Both are proofs of conformance at S2b, and both have cases
  (`ADR-063:424-427`, `:756-760`; `08:940`).

**What the code holds at `bbfd839`.**

- `sdk/def/operations/v0` is at version 0.4 with `compatibleFrom: "0.1"` and suite
  revision 7, and its interface holds no `Start`
  (`sdk/def/operations/v0/definition.yaml:25-30`;
  `sdk/def/operations/v0/operations.go:453`).
- `plugin new` refuses every language but Go
  (`plugins/operations/internal/operations/author.go:39-40`;
  `internal/authoring/tools.go:139-140`). Neither its request nor the template request
  of the toolbox holds a runtime (`sdk/def/operations/v0/operations.go:267`;
  `sdk/def/launch/v0/toolbox.go:294-297`), and `templates/` holds the Go templates of
  three registries and the connector template (`templates/templates.go:30-34`).
- A step of `plugin check` ends as `pass`, `fail`, `not applicable` or `skipped`, and a
  report passes when no step failed (`sdk/def/operations/v0/operations.go:292-295`,
  `:359-361`). The attestation that the check prints is made in step 7, with the digest
  of the packed artifact as its subject
  (`plugins/operations/internal/operations/artifact.go:209-218`; ADR-020 decision 9).
- `plugin verify` prints the steps `signature`, `attestation` and `suites`, each `pass`,
  `fail` or `not-built`, and exits 1 unless each passed
  (`plugins/operations/internal/operations/artifact.go:592-596`, `:693`;
  `plugins/command-plugin/plugin.go:334-340`).
- `plugin smoke` requires `--against` and `--modcache` and builds a throwaway
  distribution in every case (`plugins/command-plugin/plugin.go:96`;
  `internal/authoring/smoke.go:80-84`).
- The one place where `aiopsctl` runs a container runtime is the builder, a part of
  `Tools` (`internal/builder/image.go:417`; `sdk/def/launch/v0/toolbox.go:46-49`).
- The release artifact that `--against` names is a referrer of the release image: that
  image is its subject (`internal/builder/image.go:46-49`, `:281-299`).
- A generation that fails reaches `aiopsctl` as a result whose report holds, per row, an
  id, an outcome, a state and a text, and no cause
  (`sdk/def/reload/v0/wire.go:194-199`). `aiopsctl` prints that text after "rolled back
  at generation" (`plugins/operations/internal/operations/install.go:474`, `:498-513`).

**Gaps.**

1. No line names the command that the refusal of a missing endpoint prints.
   `ADR-063:415-416` names `aiopsctl plugin start <id>` for an instance that did not
   answer; install, and not `plugin start`, writes the endpoint into the table
   (`ADR-063:630-631`; ADR-063 decision 39); and the case of `ADR-063:753-755` wants
   "the exact command".
2. `aiopsctl` prints the exact command only "where the instance did not answer" and
   makes no dial, and the result gives it the server's reason as a text, with nothing
   that tells an instance that did not answer from a denied handshake.
3. No line says how `plugin check` exits when step 7 did not run.
4. `07:1583` words the test-only profile of smoke for S2a, "the release's `aiops-core`
   bundle plus the plugin's bundle with its rows enabled". A proxy row waits for
   `sidecars` (`07:1150`), whose provider ships in `aiops-selfhost` (ADR-063 decision
   39). And smoke requires `--modcache`, while a remote plugin adds no module to a
   build.
5. No line says through which code the operations library reaches the container
   runtime. From S6a `plugin-manager` consumes `operations` in a server process
   (decision 15), and the kernel never launches or stops a plugin process (`08:447`).

### Decision of the S2b part

#### `plugin start` and `Start`

45. **`plugin start <id>` is a command of `command-plugin`**, the row of its group
    (decision 30). The row then registers fourteen commands, under its cap of 800 lines
    (ADR-063 decision 36).
    - **Form.** `plugin start <id>`: one argument, the plugin id of an installed remote
      artifact, and no flag. It takes no `--profile`, because the unit and its instance
      belong to the site and not to a profile: one shared instance per plugin serves the
      site (`08:1293`), and a site holds one digest of a plugin id (ADR-047 decision 7).
      It takes no flag that changes what runs: the unit is rendered from a fixed
      template with three inputs and no other (ADR-063 decision 25), and `plugin start`
      runs it as rendered. The home is `$AIOPS_HOME`; with none, the command is a usage
      error and exits 2 (decisions 10 and 17).
    - **What it does.** It runs the compose fragment that `plugin install` rendered for
      that id (`08:1293`; ADR-063 decision 50) on the container runtime of the host,
      with Compose, which the clean machine of the exit holds as "what `plugin start`
      needs" (`ADR-063:620-621`). It creates the network of the instance and attaches
      the instance and the kernel container to it (`ADR-063:626-629`).
    - **What it does not do.** It enables no row, writes no profile and submits nothing:
      `08:398` has "start the instance" and `plugin enable <row>` as two steps. It makes
      no dial and does not wait for the listener of the instance, since `aiopsctl` never
      dials an instance and needs no route to one (`ADR-063:406-407`): it returns when
      the container runtime reports the container of the unit running. An instance that
      is still starting when a generation dials it falls under the deadlines that
      ADR-041 fixes for the proxy (`ADR-063:809-812`). It does not start a systemd unit,
      which `systemctl enable --now` starts (`08:1293`).
    - **Result.** After exit 0 one container of that id runs, with the image by the
      digest that the unit names and by no tag (ADR-063 decision 25; step C3,
      `ADR-063:683-684`). Run on an instance that already runs so, it starts nothing and
      exits 0. It prints `started <id>: <image reference by digest>`, or `running <id>:
      <image reference by digest>`, on stderr, and nothing on stdout (decision 11).
    - **Refusals**, each with exit 1 and nothing started (decision 10): the home holds
      no compose fragment for the id; the host has no container runtime or no Compose,
      with the text of decision 47; or the container runtime does not start the unit,
      with the error it gave.

46. **`Start` joins the `operations` definition in its next minor**, 0.5 where no other
    S2b change takes that number first, with `compatibleFrom: "0.1"` kept: for a
    consumer a new method is an additive change (`07:547`). The suite revision rises
    from 7 (ADR-060 decision 10; `ADR-063:849-850`). The same minor carries the field
    `Runtime` of `NewRequest` (decision 48) and the end of the refusals of `remote` in
    `Check`, `Pack` and `Install`, which ADR-063 decision 17 lifts (`ADR-063:366-367`).
    The pull request that raises the minor regenerates the `requires` of the five
    command rows (ADR-060 decision 32, rule 4).

    | Method or type | What it is |
    |---|---|
    | `Start` | Takes the caller's context first and a `StartRequest`, and returns a `Started`. Once the provider is released it returns an error that matches `kernel.ErrInactive`, as every method does (decision 16) |
    | `StartRequest` | `Caller`, `Home` and `ID`, the plugin id |
    | `Started` | `ID`; `Image`, the image reference by digest that the unit names; `Running`, whether the instance already ran; and with question Q10 (a) the `Change` of the one line it wrote |

    The method table of decision 16 gains the row:

    | Method | Command (decision 30) | Writes the home under the lock | Gate entry point (ADR-060 decision 17) | Toolbox parts (decision 21) | Source |
    |---|---|---|---|---|---|
    | `Start` | `plugin start <id>` | No; with question Q10 (a), the line of the id in the endpoint table, where the table lacks it | None | `Unit` (decision 47) | `08:1293`; `ADR-063:401-403` |

    Rules every provider keeps, beside those of decision 17:
    - `Start` refuses an id for which the home holds no compose fragment, with an error
      that matches `ErrRefused`, and starts nothing. With no home it returns an error
      that matches `ErrUsage` (decision 17).
    - It reads the home as a reader does, with no lock (decision 19), and it holds no
      lock while the container runtime pulls or starts an image: a holder holds the
      write lock only through bounded steps (decision 19). With question Q10 (a) it
      takes the lock for the one write, with the snapshot of decision 17, and releases
      it before it runs the unit.
    - It passes on the error of decision 47 where the host has no container runtime.

    **Fake and suite** (decision 18). The fake takes from its config what it cannot
    compute without a container runtime, as it takes each signature verdict: the ids
    whose unit and whose endpoint the home holds, and what the runtime answers for each,
    started, already running, or no runtime. It records each call. The new suite
    revision adds a case for each rule that the methods can show: `Start` of an id with
    no unit is refused and starts nothing; `Start` of an installed id answers the image
    by digest; a second `Start` answers that the instance runs and starts nothing;
    `Start` with no container runtime returns the error that names what the host lacks;
    `Start` with no home is a usage error; `Start` after release returns `ErrInactive`;
    and `Enable` of a remote row whose endpoint the table lacks is refused, writes
    nothing and submits nothing (decision 50). With question Q10 (a) one more: `Start`
    writes a missing line again under the lock. The fake passes the suite. The cases
    that need the files or a container runtime are T1 tests of `plugins/operations` and
    steps of the exit (decision 54).

47. **The container runtime is reached through `Tools`.** At `bbfd839` the one place
    where `aiopsctl` runs a container runtime is the builder, a part of `Tools`
    (`internal/builder/image.go:417`; `sdk/def/launch/v0/toolbox.go:46-49`). `Start`
    follows it. The minor that `launch` gains at S2b (`ADR-063:351-352`) adds to `Tools`
    one part, `Unit`: it starts or stops the compose fragment of a plugin id of a home on
    the container runtime of the host, and reports whether the container of that unit
    runs and with which image digest. `cmd/aiopsctl` implements it.
    - In `aiops` it answers `ErrNotCarried`, as every part of `Tools` does (decision 21;
      `sdk/def/launch/v0/toolbox.go:20`). So no server process starts or stops a unit,
      whoever consumes `operations` there: the kernel never launches or stops a plugin
      process (`08:447`, `:1293`), and from S6a `plugin-manager` consumes `operations`
      in a server process (decision 15).
    - `Disable` and `Remove` stop the unit through the same part, where ADR-063 decision
      68 has them stop it. In a server process the part is absent, which is why
      `aiopsctl plugin disable` stops a unit only when "run on the host" (`07:1195`).
    - **`ErrNoRuntime`.** The same minor adds the error `ErrNoRuntime` to `launch`. A
      part of `Tools` that needs a container runtime returns an error that matches it
      where the host has none, or none that answers, and `Unit` also where the host has
      no Compose. Its text names what the host lacks, as ADR-063 has `plugin check` name
      it (`ADR-063:382-384`): `no container runtime: <program>: <error>`, with the
      program that the part ran and the error of that run. A command decides on the
      error and prints the text: `plugin start` and `plugin smoke` exit 1 with it,
      `plugin check` reports step 7 as `not run` (decision 51), and `plugin verify`
      reports a step as `not-run` (decision 52).
    - `TemplateRequest` gains the field `Runtime` (decision 48).
    - The parts through which `plugin pack` builds an image and through which the check
      runner drives a remote process are named by the S2b parts of ADR-047 and of
      ADR-044, which settle that code (`ADR-063:826-828`, `:862-863`), in the same minor
      of `launch`. The tables of decision 21 gain `Unit` and each of those.

#### The templates

48. **`plugin new` gains the language `python` and the flag `--runtime`.** The CLI spec
    of decision 30 reads `plugin new <registry> <id> [--lang go|python] [--runtime
    inproc|remote] [--contrib]`; the connector form does not change.

    | Form | What it writes | Source |
    |---|---|---|
    | `plugin new <registry> <id>`, with or without `--lang go` and `--runtime inproc` | The Go template of S2a, unchanged | ADR-063 decision 60; ADR-060 decision 24 |
    | `plugin new <registry> <id> --lang go --runtime remote` | The remote Go form of decision 49 | ADR-063 decision 60; `07:1556` |
    | `plugin new <registry> <id> --lang python`, with or without `--runtime remote` | The Python template of decision 49 | `07:1213`; `ADR-063:448-451` |

    - `--lang` defaults to `go`, as at S2a (`plugins/command-plugin/plugin.go:89`).
      `--runtime` defaults to `inproc` for Go, so the S2a form is unchanged (ADR-063
      decision 60), and to `remote` for Python, its one runtime: "A Python plugin is
      always remote" (`07:1203`).
    - The remote forms exist for each registry marked `contrib` (`ADR-063:448-449`),
      which at S2b are `echoers` and `stampers` at `0.2` (ADR-063 decisions 14 and 48).
    - `New` refuses, with exit 1 and before the first write, as every refusal of `New`
      comes (`plugins/operations/internal/operations/author.go:31-34`): `--lang python`
      with `--runtime inproc`; a remote form of a registry that is not marked `contrib`,
      which at S2b is `commands`, a registry with no remote contribution (`00:222`;
      decision 1); and a language or a runtime outside the two of its flag. The text
      names the registry or the value, and the forms that exist.
    - `--contrib` writes the `contrib` id owner in every form (`07:1533`; `08:409`).
    - `plugin new connector` reads neither flag: a pack has no code and its `runtimes`
      is empty (`07:134`), and at S2a it reads no `--lang`
      (`plugins/operations/internal/operations/author.go:39`).
    - `NewRequest` and `TemplateRequest` each gain the field `Runtime`, in the minors of
      decisions 46 and 47.
    - Every form needs `aiopsctl` and nothing else (`07:1533`): the templates are
      compiled into `aiopsctl` (`07:1556`), and writing the Python template runs no
      Python.

49. **What each remote template writes.** Both write a plugin of one registry that
    declares `scope: [process]` (ADR-063 decision 54, reading 1) and a bundle of exactly
    one row, disabled (`08:393`; ADR-063 decision 67).

    | File | The remote Go form | The Python template | Source |
    |---|---|---|---|
    | The plugin code | The files of the Go template: the same plugin package, with one stub per method of the definition | A package whose `__main__.py` holds one class per contribution, marked with `@contributes`, each method a stub that fails as "not implemented", and the call of `serve()` | `07:1560-1561`; ADR-063 decision 60; `07:1208`, `:1211`, `:1216-1232` |
    | The program that the unit starts | `cmd/<name>/main.go`: a `main` that serves the package's `Plugin` value through `sdk/remotehost` and holds nothing else, so that value is the one that passes the suite in both runtimes | The package itself | ADR-063 decision 60; `07:1198`; `07:148` |
    | The hand-written manifest half | `plugin.yaml`, with `runtimes: [remote]` | `plugin.toml`; no `plugin.yaml`, which `aiops-sdk manifest` writes | ADR-063 decision 60; `07:148`, `:1210`, `:1213` |
    | Dependencies | `go.mod`, which requires the SDK module, the test kit and the two remote modules | The file that names `aiops-sdk` as its one dependency, in the form that ADR-041 fixes | `07:51`, `:1566`; `ADR-063:822-825` |
    | The test | `plugin_test.go` of the Go template | `tests/test_<name>.py`, which runs the suite through `aiops_sdk.testing.host()` | `07:1562`; `07:1212`, `:1236-1243` |
    | The image | The file from which the image is built: one static binary on no base layer, so it names no base image | The file from which the image is built: it names a base image by digest, read under ADR-063 decision 35 before the template merges | `ADR-063:455-459` |
    | The rest | `bundle.yaml`, `fixtures/`, `README.md` with the row snippet, `LICENSE` and a CI workflow, as the Go template has them | The same | `07:1564-1566`; `07:1213` |

    - **The state after `plugin new`.** The remote Go form, after `plugin check
      --write`, compiles and holds a `requires` with `remoteProtocol` and `definitions`
      and no `pluginApi` (ADR-063 decision 60; ADR-048 decision 12). The Python template
      holds its `plugin.yaml` after `aiops-sdk manifest` (step P1, `ADR-063:656-659`).
      Each then passes steps 1 to 4 of `plugin check` and fails at step 5 with exactly
      one line per obligation of the suite (`07:1568`; `ADR-063:450-452`, `:658-659`),
      step 5 run in the runtime `remote` (`ADR-063:368-374`).
    - **The template job** generates a plugin from every template and asserts that
      state (`07:1568`; `ADR-063:451-452`, `:986`): at S2b the three Go templates, the
      remote Go form and the Python template of `echoers` and of `stampers`, and the
      connector template.
    - The keys of `plugin.toml`, and whether `plugin.yaml` carries `entrypoint` and in
      which grammar, are the S2b part of ADR-048 (`ADR-063:833-836`). The names of the
      Python base classes, of their methods and of the fixture are fixed with the
      Python parts at S2b (`07:66` R8; `ADR-063:822-825`). The name and the format of
      the file from which `plugin pack` builds the image are the S2b part of ADR-047
      (ADR-063 decision 61; `ADR-063:826-827`). The templates write each in the form
      that those parts fix.

#### The refusals of `plugin enable`

50. **Each refusal exits 1 and leaves the home as it was** (decision 10; ADR-059
    decision 29). `plugin enable` of a remote row keeps its S2a form, `plugin enable
    <row> [--profile <p>] [--connect-wait <duration>] [--wait <duration>]`. `Enable`
    reaches no instance and no container runtime: its one toolbox part stays `Render`
    (decision 16), so "`aiopsctl` made no dial" holds by construction (step C2,
    `ADR-063:678-682`). A row is a remote row when it sits under the group of an
    installed artifact whose kept `plugin.yaml` lists the runtime `remote`, and its
    plugin id is that artifact's (ADR-047 decision 5; ADR-063 decision 41).

    | Refusal | Who decides it, and when | Files | Exit | What stderr holds |
    |---|---|---|---|---|
    | The endpoint of the row's plugin is missing from the endpoint table | `aiopsctl`, under the write lock, among the checks that read the home, before the snapshot, any rename and any submission (decision 17; `ADR-063:408-410`) | Nothing written | 1 | The home, the plugin id, the row and the exact command that question Q10 gives |
    | The instance did not answer | The server process: its proxy dials inside the generation that `aiopsctl` submitted, the row is not ACTIVE and the verdict fails (`ADR-063:410-416`; `08:477`) | Restored from the snapshot (ADR-062 decision 11) | 1 | The final result `rolled back`, the row, the reason the server gave and the exact command `aiopsctl plugin start <id>`, which reaches the output as question Q11 decides |
    | The handshake is denied: no common protocol major, another clause of check 3 or of check 9 that the handshake evaluates, or a peer whose identity the kernel refuses | The server process, in the same generation (ADR-063 decisions 16, 23 and 66) | Restored from the snapshot | 1 | The final result `rolled back`, the row and the denial as the server gave it, which names the check and, for no common major, holds both lists of majors (`ADR-063:345-347`, `:706-713`). It holds no command to start the instance, which runs |
    | The changed profile is not admitted: a clause that boot evaluates on the manifest denies the row, from S2b the majors clause of check 3 and check 9 among them | `aiopsctl`, in the render of the changed profile before any rename (decision 17; ADR-063 decision 16) | Nothing written | 1 | The denial with its check, as at S2a |

    **The texts.** Each is in the one line that the `cli` row prints for a failed
    command, `aiopsctl: plugin enable: <error>` (decision 10). `<home>`, `<id>`, `<row>`
    and `<p>` stand for the home, the plugin id, the row and the profile.

    - The missing endpoint, with question Q10 (a):

      ```text
      aiopsctl: plugin enable: operations: refused: the endpoint table of <home> holds no endpoint of <id>, the plugin of the row <row>: run "aiopsctl plugin start <id>"
      ```

      With Q10 (b) the text after the last colon is `run "aiopsctl plugin remove <id>
      --profile <p>", then install it again`.
    - The instance that did not answer. The line is the one that a failed generation
      prints at S2a, which holds the result, the generation and one part per failed row
      (`plugins/operations/internal/operations/install.go:474`, `:498-513`). The part of
      the row reads:

      ```text
      row <row> fail: the instance of <id> did not answer: run "aiopsctl plugin start <id>"
      ```

      With question Q11 (a) the words after `fail:` are the reason that the server gave.
    - The denied handshake: the same line, whose part of the row reads `row <row> fail:`
      and then the denial that the server gave.

    Where no process runs the profile, `plugin enable` of a remote row whose endpoint is
    listed is no refusal: it keeps the files and exits 0 with `not-running`, as for any
    row (decision 17), and at the next start the row follows the matrix row "Remote
    unreachable or handshake mismatch" (`08:477`; ADR-063 decision 66). Every other
    outcome of a submission keeps the table of decision 17.

#### `plugin check` without a container runtime

51. **A step that cannot run for want of a container runtime is reported `not run`.** A
    step of `plugin check` gains a fifth outcome beside `pass`, `fail`, `not applicable`
    and `skipped` (`sdk/def/operations/v0/operations.go:292-295`).
    - **Which step.** Only step 7, and only on a plugin whose manifest lists `remote`:
      steps 1 to 6 and step 8 need no container runtime in any language, and step 7
      needs one when it packs an image (`07:1570`; `ADR-063:382-384`). For in-process
      code and for a pack, step 7 packs no image and runs as at S2a.
    - **What it prints.** On stderr, in the form of every step
      (`plugins/command-plugin/plugin.go:183`), the line `step 7 pack and verify: not
      run`, and under it the text of decision 47, which names what the host lacks. In
      the `--json` report step 7 holds the outcome `not run` and one diagnostic of check
      `7` with that text (step P2, `ADR-063:660-663`). Step 8 after it is `not
      applicable`, as for all code.
    - **No attestation.** In the place of "the attestation it would sign" the check
      prints the line `no attestation: step 7 did not run`, and the report holds none:
      the subject of the attestation is the digest of the artifact that step 7 packs
      (ADR-020 decision 9; `plugins/operations/internal/operations/artifact.go:209-218`).
    - **`not run` is never `pass`.** On that machine `plugin pack` refuses for want of
      the same runtime, since it builds the image (ADR-063 decision 61), so nothing is
      packed, signed or pushed from a check whose step 7 did not run.
    - A step after a failed step stays `skipped`, and the check still stops at the first
      failed step (`07:1570`). How the command exits when step 7 is `not run` and no
      step failed is question Q12.

#### `plugin verify` and `plugin smoke` for remote code

52. **`plugin verify <spec>` of remote code re-runs the suites against the pulled
    image** (plugin record §11; `ADR-063:424-427`). It is a command of its own and no
    part of install, which pulls no image and runs none (`ADR-063:392-394`).
    - **Form.** `plugin verify <spec>`, as at S2a. The flags `--builder`, `--against`,
      `--modcache` and `--mirror` name the build of in-process code (ADR-060 decision
      46; ADR-044 decision 1) and are not read for an artifact that holds no module. It
      writes no home and reads the `trust.yaml` of the home, as at S2a.
    - **Steps.** It prints the digest and the signer, then each step as `step <name>:
      <outcome>: <reason>` on stdout (`plugins/command-plugin/plugin.go:334`):
      `signature`, `attestation`, `image` and `suites`.

      | Step | Passes when | Names on a failure |
      |---|---|---|
      | `signature`, `attestation` | As at S2a (ADR-047 decision 2; ADR-020 decision 14) | As at S2a |
      | `image` | The container runtime pulled the image under the digest that the artifact pins (`08:393`; `ADR-063:385-391`) | The pinned digest and what the registry answered |
      | `suites` | The obligations half of each suite passes through the adapter against a container of that image, as step 5 of `plugin check` runs it against a local process (`ADR-063:368-374`). The guard half never runs remote (`ADR-063:328-329`) | Each failed obligation, by its id |

      On a copy whose image or whose layer was changed after signing, the command fails
      and names what differs (`ADR-063:756-758`).
    - **Order.** Nothing but `plugin.yaml` is fetched before the signature and the
      attestation verified (ADR-047 decision 2), so no image is pulled and no code runs
      of a signer that the home does not trust for the id and the runtime `remote`
      (`07:1866`). Whether an unsigned remote artifact is admitted is the S2b part of
      ADR-020 (`ADR-063:845-848`).
    - **The container.** It is started for the `suites` step and removed when the
      command returns. It gets no more than a unit gives an instance, no route out and
      no path of the home (ADR-063 decisions 24 and 25), since the command runs on an
      operator's host the image that install only pins. How the check runner reaches it
      is the S2b part of ADR-044 (`ADR-063:862-863`), under reading 2 of ADR-063
      decision 54.
    - **Outcomes and exit.** A step is `pass`, `fail` or `not-built`, as at S2a
      (`sdk/def/operations/v0/operations.go:257-259`), or `not-run` where the host has
      no container runtime, for `image` and `suites`, with the text of decision 47. The
      command exits 0 only when every step passed and exits 1 otherwise, naming the
      steps (`plugins/command-plugin/plugin.go:334-340`): a step that did not run did
      not pass. So `plugin verify` of remote code needs a container runtime.
    - What the `suites` step compares with the attestation, and whether the attestation
      names the runtimes its suites ran in, is the S2b part of ADR-020
      (`ADR-063:842-844`).

53. **`plugin smoke <dir> --against <release>` of remote code attaches the plugin's
    image to the released image** (`07:1583`). The points that `07:1583` leaves open for
    `remote` are the four readings of question Q13. With them:
    - **Form.** `plugin smoke <dir> --against <release> [--modcache <dir>] [--mirror
      <prefix>=<site prefix>]... [--builder image|local]`. Smoke runs T3 per runtime
      that the manifest lists (`07:1583`). `--modcache`, `--mirror` and `--builder` name
      the module build of in-process code: `--modcache` is required where the manifest
      lists `inproc`, as at S2a (`plugins/command-plugin/plugin.go:96`), and none of the
      three is read for a manifest that lists `remote` alone (reading 3).
    - **What it needs.** A container runtime and what `plugin start` needs (`07:1537`,
      `:1583`; decision 45). Without one it exits 1 with the text of decision 47. No
      step of smoke is `not run`: smoke is not part of the check because it needs a
      container runtime (`07:1549`).
    - **The released image** is the subject of the release artifact that `--against`
      names by digest, pulled by that digest; smoke builds no distribution for `remote`
      (reading 1).
    - **The path.** Smoke packs the plugin directory, which builds the plugin's image
      (ADR-063 decision 61), and signs the artifact with a throwaway key, as step 7 of
      `plugin check` does (`07:1580`). On a throwaway home that trusts that key for the
      id and the runtime `remote` (`07:1866`) it does what `08:398` gives an operator:
      it installs the artifact into the test-only profile, starts the unit and enables
      the row, and it boots the released image on that home in the container form of
      ADR-047 decision 15 (reading 4). The instance therefore runs behind the unit, the
      credential and the network that a site gives it (ADR-063 decisions 23 to 25).
    - **The test-only profile** is the release's `aiops-core` and `aiops-selfhost`
      bundles plus the plugin's bundle with its row enabled (reading 2).
    - **Pass, output and exit.** The rule of ADR-060 decision 58, reading 1, stands:
      smoke passes when every row is ACTIVE or says what it waits for, and a row of the
      plugin that does not mount fails it. It prints each row on stdout as at S2a, `row
      <id>: <state>` with the keys it waits for
      (`plugins/command-plugin/plugin.go:310-313`), and exits 1 unless it passed.
    - **What it leaves.** It removes the containers, the network and the home that it
      made, and writes no home of the site and nothing beside the plugin, as at S2a
      (`sdk/def/operations/v0/operations.go:484`). `Smoke` keeps its request and its
      result (decision 16).

#### Tests

54. Under the working rules of ADR-063 decisions 29 and 31, each with a test that fails
    on its base:
    - the suite cases of decision 46, on the plugin and on the fake;
    - the help goldens of `plugin start`, `plugin new` and `plugin smoke` under
      `testdata/help/` of `command-plugin`, and the cases of `plugin start` in its
      `fixtures/commands/` (decisions 5 and 30);
    - exit codes, beside those of decision 31: `plugin start` with no home exits 2; each
      refusal of decision 45 exits 1; each refusal of decision 50 exits 1 with its text,
      the missing endpoint with nothing submitted (`ADR-063:753-755`); `plugin check`
      with step 7 `not run` exits as question Q12 decides and prints no attestation; and
      `plugin verify` of remote code with no container runtime exits 1;
    - a test that `Unit` answers `ErrNotCarried` in `aiops`, as the other parts of
      `Tools` do;
    - each refusal of `New` in decision 48, with nothing written;
    - the template job with the remote Go form and the Python template of each `contrib`
      registry (decision 49; `ADR-063:986`);
    - the cases of `plugin verify` and `plugin smoke` that ADR-063 decision 28 lists
      (`ADR-063:756-760`).

    The exit takes from this part:

    | Step of ADR-063 decision 27 | What it uses |
    |---|---|
    | G1 | The remote Go form (decisions 48 and 49), and step 7 of `plugin check` run on a machine that holds a container runtime (decision 51) |
    | P1 | The Python template and its state (decisions 48 and 49) |
    | C2 | `plugin enable` before `plugin start`: the refusal of an instance that did not answer, with its text (decision 50) |
    | C3 | `plugin start` (decisions 45 to 47), then `plugin enable` with the final result `passed` |

#### What the S2b part does not settle

55. Each item names where it is settled.
    - The files of the unit, the endpoint table and the credentials under the home;
      which target an install renders for a site; the network of an instance and of a
      kernel container, and how `aiopsctl` names the kernel container: the S2b part of
      ADR-047 (`ADR-063:826-832`).
    - The image that `plugin pack` builds, the file it builds it from and the part of
      `Tools` it uses: the S2b part of ADR-047 (ADR-063 decision 61).
    - `entrypoint`, every `plugin.toml` key, and what a manifest that lists `inproc` and
      `remote` packs, installs and smokes: the S2b part of ADR-048 (`ADR-063:833-841`).
    - The commands of `aiops-sdk`, among them how step 2 of `plugin check` runs
      `aiops-sdk manifest` (`07:1575`) and what `--write` does on a Python plugin; the
      fixture and the command of `aiopsctl` that it wraps; the deadlines of the proxy's
      dial; and what `plugin enable`, `disable` and `remove` write of the instance's
      credential: ADR-041 (`ADR-063:799-825`).
    - What `plugin verify` compares with the attestation, and an unsigned remote
      artifact: the S2b part of ADR-020 (`ADR-063:842-848`).
    - The suite binaries that drive a remote process, at step 5 of `plugin check` and in
      `plugin verify`: the S2b part of ADR-044 (`ADR-063:862-863`).
    - When a shared unit stops and what `remove` deletes: ADR-063 decision 68.
    - Compose, its pin and the form in which `aiopsctl` runs it: the reading that
      ADR-063 decision 35 requires before its first use (`ADR-063:913-915`).
    - The command that starts a systemd unit or applies a Helm entry: the operator's, as
      `08:1293` words it. `plugin enable` names the command of the compose target
      (decision 50).

### Consequences of the S2b part

- `command-plugin` registers fourteen commands. `operations` holds twenty-four methods
  at its next minor, with a raised suite revision, and `launch` gains `Unit`,
  `ErrNoRuntime` and the field `Runtime` in the minor that also brings the key of
  ADR-063 decision 57. No definition and no key is added.
- No server binary can start or stop a plugin process: the one part that reaches the
  container runtime for a unit is absent from `aiops`.
- `aiopsctl` runs a container runtime in more places than at S2a, `Unit` and the parts
  that the S2b parts of ADR-047 and ADR-044 name, and runs Compose for the first time.
  This part links no new module: Compose is run and not linked, and it is read and
  pinned first (ADR-063 decision 35).
- `templates/` holds eight templates where S2a holds four, under its cap of 300 lines of
  Go (ADR-063 decision 36), and the template job needs a Python interpreter with
  `aiops-sdk` for two of them.
- With the recommended answer to Q10, the endpoint table has two writers, `plugin
  install` and `plugin start`, and `Start` takes the write lock for that one write.
- With the recommended answer to Q11, one line of the proxy's failure text is fixed by
  this ADR and names a command of `aiopsctl`.
- With the recommended answer to Q12, `plugin check` on a machine with no container
  runtime exits 0 on remote code and prints no attestation, and every job of this
  repository that checks a remote plugin holds a container runtime.
- The command that `plugin enable` names is the compose target's. A site that runs the
  systemd unit reads `08:1293` for its own.
- The line caps of ADR-063 decision 36 stand, and the kernel does not change (ADR-063
  decision 3).

### Edits on acceptance of the S2b part

Made in the commit that records the owner's answers (`08:999`):

- This ADR: the two Status lines; in decision 16, the row of `Start`, the types
  `StartRequest` and `Started` and the field `Runtime` of `NewRequest`; in decision 21,
  the part `Unit`, the error `ErrNoRuntime` and the field `Runtime` of
  `TemplateRequest`; in decision 30, `plugin start` in the row of `command-plugin` and
  the CLI spec rows of `plugin start`, `plugin new` and `plugin smoke`; and a dated note
  at the item of `plugin start` in decision 32.
- `00:137`: `start` among the `plugin` commands of P0, which `02:313` and `08:1293`
  already name.
- `07:1556`: the sentence "The Python template arrives with the Python SDK at S2b" gains
  the registries that have a remote form and the default runtime of each language
  (decision 48).
- `07:1570`: the outcome `not run` of step 7 and, with the answer to Q12, how the check
  then exits.
- `07:1583`: with the answer to Q13, the released image, the test-only profile of
  `remote` and the flags that `remote` does not read.
- `08:398`, `07:1870`, `02:326` and plugin record §11: with Q10 (b), the command of each
  half. With Q10 (a) none of the four is edited.
- With Q10 (a), the row of the endpoint table in the home table of ADR-047 decision 13
  names `plugin start` beside `plugin install`, in the S2b part of ADR-047. With Q11
  (b), ADR-062 decision 7 gains a dated note. With Q12 (b), `07:1536` gains the
  container runtime. An option that the owner does not choose edits nothing.

### Questions for the owner on the S2b part

Each question gives its options, the recommended one, why, and what the answer blocks.
An option that changes a written line says so and names every line it edits.

**Q10. Which command does `plugin enable` print when the endpoint is missing from the
table?** `08:398`, `07:1870` and `02:326` give one "exact command" for both halves, the
missing endpoint and the instance that does not answer, and plugin record §11 writes the
sentence after the commands that start an instance per target. ADR-063 names `aiopsctl
plugin start <id>` for the instance that did not answer (`ADR-063:415-416`) and wants
"the exact command" for the missing endpoint without naming it (`ADR-063:753-755`).
Install writes the endpoint into the table (`ADR-063:630-631`; ADR-063 decision 39), so
at a site whose install completed the line is there before `plugin start` runs (step C2,
`ADR-063:678-682`), and a missing line is a table that was edited or damaged.
- (a) `aiopsctl plugin start <id>`, one command for both halves, as the three lines word
  it. `plugin start` then makes that command true: where the table lacks the line of an
  id whose unit the home holds, it writes the line that install writes for that id,
  under the write lock and with the snapshot of decision 17, releases the lock and runs
  the unit. The write submits nothing and answers as decision 43 has a write that the
  wire does not carry. The table then has two writers.
- (b) The command that takes the artifact out, printed exactly, `aiopsctl plugin remove
  <id> --profile <p>`, and the words "then install it again": the home does not keep the
  spec that an artifact was installed from (ADR-047 decision 5). Install stays the one
  writer of the table, and `Start` writes no file of the home and takes no lock.
  `08:398`, `07:1870`, `02:326` and plugin record §11 gain the clause that the first
  half names another command than the one that starts the instance. Where another
  profile still names the id, `remove` keeps the rendered files (ADR-063 decision 68),
  so the install that follows has to write a missing line again, which the S2b part of
  ADR-047 would then have to say.
- Recommended: (a). It keeps the three lines as written, one command repairs the home
  whichever half refused, and it depends on no other part. Its cost is a second writer
  of the table and a lock in `Start` for one write. The dial stays authenticated
  against the pinned certificate whatever the table holds (ADR-063 decision 55), so a
  line that names another endpoint opens no stream. (b) is less code and changes what
  four written lines mean. Blocks: `Start`, the refusal of `Enable` and its case
  (`ADR-063:753-755`); needed before group 3 of ADR-063 decision 30.

**Q11. How does the exact command reach the output when the instance did not answer?**
`aiopsctl` prints "the row, the reason the server gave and, where the instance did not
answer, the exact command" (`ADR-063:414-416`), and for a denied handshake it prints the
denial (step D2, `ADR-063:706-713`). It makes no dial (`ADR-063:406-407`), and the
result carries per row a text and no cause (`sdk/def/reload/v0/wire.go:194-199`). The
causes of the wire are a closed list on the error of a result, on which "A client
decides on `status` and `causes` and prints `text`" (ADR-062 decision 7).
- (a) The server's reason holds it. Where the dial of a proxy got no answer, the reason
  with which its row fails ends with the words `the instance of <id> did not answer: run
  "aiopsctl plugin start <id>"`. `aiopsctl` prints the reason whole and decides nothing
  on it. The same words are in the log of the server process, at boot too, where an
  optional row waits for its instance (`08:477`). The proxy, a first-party row, then
  names a command of `aiopsctl`, as a server that finds a snapshot names `aiopsctl
  profile sanitize <profile>` (decision 17), and it names the command of the compose
  target at a site that runs another target.
- (b) A cause per row. Each row of the report gains a closed list `causes`, with
  `remote-unreachable` and `remote-denied` at S2b, and `aiopsctl` adds the line `start
  the instance: aiopsctl plugin start <id>` for the first. A field is additive inside
  version 1 of the wire (ADR-062 decision 6). `composition`, whose row report the wire
  renders, and `reload` each gain a minor with a raised suite revision, the proxy's
  failure has to reach the judge as an error of a definition that both import, and
  ADR-062 decision 7 gains a dated note.
- (c) `aiopsctl` adds the line for every row of a remote plugin that a failed generation
  reports, worded "if its instance is not running". No wire changes and no server text
  is fixed. It departs from `ADR-063:415-416`, "where the instance did not answer": the
  output of step D2 would hold the command although the instance runs.
- Recommended: (a). It changes neither the wire nor a definition, `aiopsctl` matches no
  text, and an operator who reads only the server log finds the same command. (b) is the
  form to take once a second client has to decide on the cause. Blocks: the failure text
  of the proxy (group 2 of ADR-063 decision 30), `Enable` and step C2; needed before the
  proxy merges.

**Q12. How does `plugin check` exit when step 7 did not run for want of a container
runtime?** The check "exits non-zero on the first failing step", and step 7 "needs one
when it packs an image" (`07:1570`). The check is "The one command before a pull
request" and needs "A toolchain for code; PostgreSQL only for a plugin with its own
schema" (`07:1536`); smoke "is not part of the check, because it needs a container
runtime" (`07:1549`); and a Python author needs no container runtime for T0 to T2
(`07:51`, `:1236`; `ADR-063:436-439`). ADR-063 has the step reported as not run and
gives no exit code (`ADR-063:382-384`), and step P2 reads the `--json` report
(`ADR-063:660-663`).
- (a) Exit 0. A step that did not run is not a failed step. The report says `not run`
  and holds no attestation (decision 51), and `plugin pack` refuses on that machine. The
  jobs of this repository that run `plugin check` on a remote plugin, `make
  plugin-check` among them (ADR-063 decision 31), read the `--json` report and fail on a
  `not run` step, so each holds a container runtime.
- (b) Exit 1. Every step passes or does not apply, or the check fails. A remote author
  with no container runtime then never gets exit 0 from the one command, and reads the
  report to see that step 7 alone is open. `07:1536` gains the container runtime among
  what the check needs for remote code.
- (c) As (a), with a flag of `plugin check` under which a `not run` step exits 1, for a
  job that must not pass without step 7. The CLI spec gains the flag.
- Recommended: (a). It is what `07:1536` and `07:1549` say the check needs, a `not run`
  step can publish nothing, and the one place that must not pass without step 7, the CI
  of this repository, reads the report that the check already writes. (c) adds a flag
  for what the report shows. Blocks: `Check` on remote code; step P2 of the exit reads
  the report under every option.

**Q13. Four readings of `07:1583` for `remote`.** None changes what smoke requires of
in-process code: each fills a point that the line leaves open for the runtime `remote`.
1. **The released image.** `07:1583` says "the released image", and `--against
   <release>` names the release artifact by digest (ADR-044 decision 1). Reading: the
   released image is the subject of that artifact, which is a referrer of the release
   image (`internal/builder/image.go:46-49`, `:281-299`); smoke pulls it by that digest
   and builds no distribution for `remote`.
2. **The test-only profile.** `07:1583` words it for S2a: "the release's `aiops-core`
   bundle plus the plugin's bundle with its rows enabled". A proxy row waits for
   `sidecars` (`07:1150`), whose provider ships in `aiops-selfhost` (ADR-063 decisions
   22 and 39). Reading: for `remote` the profile is the release's `aiops-core` and
   `aiops-selfhost` bundles, the two that `dev` lists, plus the plugin's bundle with its
   row enabled, and the pass rule of ADR-060 decision 58, reading 1, stands.
3. **The module source.** "Smoke needs a container runtime and a module proxy or cache"
   (`07:1583`), and `--modcache` is required at S2a
   (`plugins/command-plugin/plugin.go:96`). Reading: the module source is what the build
   of in-process code needs; `--modcache` is required where the manifest lists `inproc`
   and is not read for a manifest that lists `remote` alone.
4. **"Attaches".** Reading: smoke attaches the image as an operator does (`08:398`), on
   a throwaway home with a throwaway key: pack, sign, trust, install, start and enable,
   and a boot of the released image on that home (decision 53). No second way to run an
   instance exists beside the unit.
- (a) Accept the four readings, each landing with the code that builds it; `07:1583`
  gains them.
- (b) Answer each item on its own.
- Recommended: (a). Blocks: `Smoke` of remote code and its cases (`ADR-063:758-760`).
