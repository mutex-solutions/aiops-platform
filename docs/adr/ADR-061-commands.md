# ADR-061: The `commands` definition, the `cli` profile and the operations plugin

- Status: **accepted 2026-11-23 by the project owner.** The owner answered all nine
  questions on the same date with the recommended option of each: (a) for Q1 to Q7 and
  Q9, (b) for Q8. The section "Owner answers of 2026-11-23" records them as decisions 33
  to 41, with the boot changes this ADR names. ADR-060 decision 53 had it proposed before
  the first command row merges, and ADR-060 decision 2 required this acceptance before
  that code merges into `main`. Brought to ADR-062 on 2026-11-24, as its decision 16
  asks: decisions 10, 16, 17 and 32 and the scope name its transport and return the
  outcomes of its decision 11.
- Status of the S2b part: **accepted 2026-11-27 by the project owner.** The owner
  answered questions Q10 to Q16 on the same date with the recommended option of each,
  (a) in every case: Q11 with the rule on what a peer supplies, Q13 for all four
  readings and Q15 on the condition that it names. The section "S2b part, proposed
  2026-11-27" holds the part as decisions 45 to 59, and its section "Owner answers of
  2026-11-27" records the answers as decisions 60 to 66. ADR-063 decision 2 had it
  written as a dated addition to this ADR, proposed before the code it governs and
  accepted before that code merges into `main`. The owner answered the questions of
  ADR-041 and of the S2b parts of ADR-020, ADR-044, ADR-047 and ADR-048 on the same
  date, and each is recorded in its own ADR.
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
        Start(ctx context.Context, r StartRequest) (Started, error) // from S2b
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
    | `Home` | `Profiles`; for the profile asked, `Bundles` (its bundle list), `Enabled` (the `enabled` departures of its profile layer by row id) and `Locked` (id, digest and content digest of each entry of its `plugins.lock`); for the site, `Signers` (key id, ids, runtimes, signed), `Distributions` (key ids, and `release` for the default root), `AllowUnsigned`, `Records` (digest, kind, signer, plugin id), `Current` and `Image`; and `Snapshot`, whether an interrupted write left one (decision 17). From S2b `Remote`: per installed remote plugin id, whether the home holds its unit, its line of the endpoint table and its pin (decision 46) |
    | `RenderRequest` | `launch.RenderRequest` with `Caller` |
    | `NewRequest` | `Registry` (a registry or `connector`), `ID`, `Lang`, `Contrib`, `Against`, `Plugins`, `Dir`. From S2b `Runtime`, `inproc` or `remote`; empty is the default of the language (decision 50) |
    | `CheckRequest`, `CheckReport` | `Dir`, `Write`, `Against`; per step of `07:1565`-`1576` its number, `pass`, `fail`, `not applicable` or `skipped`, and its `launch.Diagnostic`s, and the attestation it would sign. From S2b a fifth outcome of a step, `not run`, and `NotRun`, the steps that did not run for want of a container runtime; `Passed` keeps its meaning, no step failed (decision 53) |
    | `Smoke` takes `BuildRequest` and returns `launch.Built` | `Plugin`, the plugin directory, `Against`, the release artifact by digest, `ModCache`, `Mirrors` and `Builder` (`image` or `local`); `Built.Rows`, each row of the test-only profile with its state and what it waits for, and its error says why smoke did not pass (ADR-060 decision 58, reading 1) |
    | `PackRequest`, `SignRequest`, `PushRequest`, `Pushed` | `Dir`, `Out`; `Out`, `Key`; `Out`, `Registry`; the spec by digest (ADR-047 decision 1) |
    | `SpecRequest`, `GateRequest` | `Spec`, and for `Verify` `Home`, whose `trust.yaml` it reads, `Builder`, and `Against`, `ModCache` and `Mirrors`, the release artifact by digest and the module source that in-process code builds on (ADR-060 decision 46); `Spec`, `Against` |
    | `Inspected` | The digest; id, version and runtimes; each row with its config and content paths; that no signer was verified (ADR-047 decision 2). From S2b `Image`, the image reference of remote code, empty for other code (decision 56) |
    | `Verified` | The digest, the verified signer, each suite that ran with its result, and the verdict. From S2b the step `image`, and the outcome `not-run` of a step beside `pass`, `fail` and `not-built` (decision 54) |
    | `InstallRequest`, `RowRequest`, `RemoveRequest` | `Home`, `Profile` and `Spec`, `Row` or `ID`; `ConnectWait` and `Wait`, the bounds of ADR-062 decisions 9 and 10 |
    | `StartRequest`, from S2b | `Caller`, `Home` and `ID`, the plugin id (decision 46) |
    | `Started`, from S2b | `ID`; `Outcome`, `started`, `restarted` or `running`; `Image`, the image reference by digest that the running container has; `Target`, `compose` or `systemd` (decision 48); `Kernel`, `attached`, `already-attached` or `none`, with the name of the kernel container; and `Change`, whose `Written` and `Notes` hold what the write phase wrote (decision 46) |
    | `TrustRequest` | `Home`, `KeyFile`, `IDs`, `Runtimes`, `Signed`, `Distribution` (ADR-020 decision 5) |
    | `KeyRequest`, `SignDistRequest`, `UseRequest` | `Prefix`; `Image`, `Key`; `Home`, `Image` |
    | `BuildRequest` | `launch.BuildRequest` with `Caller` |
    | `Written` | The paths written outside the home |
    | `Change` | `Written`, the home paths written or removed; `Notes`, what the operator is told (a profile copied from the shipped template, an item kept, a snapshot restored); `Outcome` and `Reason`, `not-running` or `restart-required` with its reason, and for the four methods that submit over the socket of ADR-062, `Result`, `Error`, `IDs` and `Last`, its outcome (decision 17); `DistLines`, the `dist.yaml` lines of `rebuild-required` (`08:397`); `Verdict`, the clauses evaluated. From S2b `Unit`, for `Disable` and `Remove`: `stopped`, `not-running`, `left` or `not-stopped`, with the reason of the last; empty where the method stopped no unit (decision 49) |

    | Method | Command (decision 30) | Writes the home under the lock | Gate entry point (ADR-060 decision 17) | Toolbox parts (decision 21) | Source |
    |---|---|---|---|---|---|
    | `Home` | None at S2a; `plugin list` reads it from S6a | No | None | None | `08:373`; decision 18 |
    | `Render` | `profile render` | No | Boot | `Render` | `08:359`; `03:63` |
    | `VerifyProfile` | `profile verify` | No | Boot | `Render` | `08:366` |
    | `InitProfile` | `profile init <p>` | Yes | None | `Shipped` | `08:72` row 30; ADR-060 decision 27 |
    | `SanitizeProfile` | `profile sanitize [<p>]` | Yes | None | None | `08:72` row 30; dsh §3.4; question Q9 |
    | `New` | `plugin new <registry>`, `plugin new connector`; from S2b `plugin new <registry> <id> [--lang go\|python] [--runtime inproc\|remote]` | No; writes the author's directory | None | `Template`; `Release` for a connector | `07:1551`; ADR-060 decision 24; ADR-047 decision 12; decision 50 |
    | `Check` | `plugin check <dir>` | No; with `--write`, the author's directory | `plugin check` step 7; from S2b, for remote code also the remote handshake, inside step 5 (`ADR-063:334-336`) | `ReadManifest`, `Generate`, `Vet`, `Suite`, `Scenario`, `Gate`; from S2b, for remote code `Image` | `07:1565`-`1576`; ADR-060 decision 23; decisions 51 and 53 |
    | `Smoke` | `plugin smoke <dir> --against <release>` | No | `dist build`, run by the builder; boot, of the test-only profile. From S2b, for remote code: `plugin pack` and `plugin install`, on the throwaway home; boot, of the test-only profile; and the remote handshake inside that boot | `Build`; from S2b `Build` for `inproc`, and `Image`, `Unit` and `Boot` for `remote` | `07:1578`; ADR-060 decision 58; decision 55 |
    | `Pack` | `plugin pack` | No; writes the layout | `plugin pack` | `ReadManifest`, `Suite`, `Scenario`, `Gate`; from S2b, for remote code `Image` | `07:1859`; ADR-060 decisions 18 and 49; ADR-063 decision 61 |
    | `Sign`, `Push` | `plugin sign`, `plugin push` | No | None | None | `07:1859`; ADR-020 decision 1; ADR-047 decision 1 |
    | `Inspect` | `plugin inspect <spec>` | No | None | None | `08:373`; ADR-060 decision 58; decision 56 |
    | `Verify` | `plugin verify <spec>` | No | None; from S2b, for remote code the remote handshake, inside the step `suites` | `Build`, `Suite`; from S2b, for remote code `Unit` and `Suite` | ADR-060 decisions 31 and 46; decision 54 |
    | `Gate` | `plugin gate <spec> --against <dist.yaml>` | No | `plugin gate --against` | `Gate` | `08:493` |
    | `Install` | `plugin install <spec> --profile <p>` | Yes | Both `plugin install` rows | `Gate`, `Render` | `08:374`, `:395`-`397`; ADR-047 decisions 5 to 10 and 17 |
    | `Enable` | `plugin enable <row>` | Yes | Boot, on the changed profile | `Render` | `08:375`; ADR-060 decision 47; decision 52 |
    | `Disable` | `plugin disable <row>` | Yes | Boot, on the changed profile | `Render`; from S2b, for a remote row `Unit`, after the lock is released | `08:375`; ADR-060 decision 47; decision 49 |
    | `Remove` | `plugin remove <id> --profile <p>` | Yes | Boot, on the changed profile | `Render`; from S2b, for remote code `Unit`, after the lock is released | ADR-047 decision 10; decision 49 |
    | `Start`, from S2b | `plugin start <id>` | Yes, in its write phase alone: the pin of the id with its key pair, where the pin is absent, and the line of the id in the endpoint table, where the table lacks it (decision 60) | None | `Unit` | `08:1293`; `ADR-063:401-403`; decision 45 |
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

    Note of 2026-11-27, on the acceptance of the S2b part: the sketch and the two tables
    above gain what decision 46 adds, each marked "from S2b": the method `Start` with
    `StartRequest` and `Started`, `NewRequest.Runtime`, the outcome `not run` of a step
    with `CheckReport.NotRun`, the step `image` and the outcome `not-run` of `plugin
    verify`, `Inspected.Image`, `Change.Unit`, `Home.Remote`, and each row of its method
    table. The error `ErrNotStopped` joins the errors of the definition with them, and
    the refusals of `remote` in `Check`, `Pack` and `Install` end (ADR-063 decision 17).
    All of it arrives with the next minor of the definition and a raised suite revision
    (decision 46); until that minor merges, `sdk/def/operations/v0` holds none of it. A
    line that an addition cites, as `08:1293` or `ADR-063:401-403`, is a line at
    `bbfd839`, the commit that the S2b part cites.

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
      behind is removed by the next install (ADR-047 decision 10). Note of 2026-11-27, on
      the acceptance of the S2b part (decision 49): for a remote row the stop of the unit
      follows the removal of the snapshot and the release of the lock, so no failure of
      the stop restores the pin, and none turns a kept change into a refusal.
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
    the submission, to the removal or the restore of the snapshot (`08:374`; decision 17).
    Note of 2026-11-27, on the acceptance of the S2b part (decision 46): `Start` writes
    the home and holds the lock for its write phase alone, from before the checks that
    read the home, through the snapshot and the renames, to the removal of the snapshot.
    It releases the lock before the container runtime pulls or starts anything, since a
    pull of an image has no bound. `Disable` and `Remove` stop a unit after they release
    the lock (decision 49).
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
    | `SuiteRequest`, `SuiteReport` | `Dir`, the definitions with their versions, the halves and the fixtures directory; per definition its suite revision, the halves that ran and each failed obligation by id. From S2b `SuiteRequest` also holds the runtime that a suite runs in (decision 47) |
    | `TemplateRequest` | `Registry`, `ID`, `Lang`, `Contrib`; from S2b `Runtime` (decision 50) |
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
    | `Generate` | `plugingen`; from S2b, for a Python plugin, it runs `aiops-sdk manifest` (decision 51) | `cmd/aiopsctl` | `Check` | ADR-060 decision 11; decision 47 |
    | `Vet` | The import analyzer and plugin vet | `cmd/aiopsctl` | `Check` | ADR-060 decision 23 |
    | `Suite` | Runs a suite: an embedded test binary, or a module compiled with the toolchain. From S2b, for the runtime `remote`, it starts the subject and runs the embedded binary that drives a remote process: in `plugin check` the `entrypoint` of the manifest, as a local process, and in `plugin verify` the container of decision 65 | `cmd/aiopsctl` | `Check`, `Pack`, `Verify` | ADR-060 decision 46; decision 47 |
    | `Template` | The templates | `cmd/aiopsctl` | `New` | ADR-060 decision 24 |
    | `Scenario` | Runs a named, versioned scenario, the echo scenario at S2a, on a pack's rows replayed from its fixtures | `cmd/aiopsctl` | `Check`, as step 8; `Pack`, for the attestation | ADR-060 decision 49; `07:1576` |
    | `Unit`, from S2b | For a home and a plugin id, one of four actions on the unit that install rendered: `state`, `pull`, `start` and `stop`, as decision 47 lists them | `cmd/aiopsctl` | `Start`, `Disable`, `Remove`, `Verify`, `Smoke` | Decisions 45 and 47 to 49 |
    | `Image`, from S2b | Builds the plugin image of a plugin directory from its build file on the container runtime and returns it as an OCI layout with the digest of its manifest; for a Go plugin it compiles the binary first (decision 51) | `cmd/aiopsctl` | `Pack`; `Check`, in step 7; `Smoke` | ADR-063 decision 61; decision 47 |
    | `Boot`, from S2b | Pulls the image of a release by digest, boots a profile of a home on it in the container form of ADR-047 decision 15, with no network, reads each row's state and what it waits for from `run/status.json`, and stops and removes the container | `cmd/aiopsctl` | `Smoke` | Decisions 47 and 55 |
| `Module` | Reads the module of in-process code for the module layer and the SBOM of `plugin pack`: the h1 sum of its module zip and its build list | `cmd/aiopsctl` | `Pack` | ADR-047 decision 1 |

    Note of 2026-11-25: the sketch and the two tables are edited to launch 0.2 as built,
    whose `Tools` has a seventh method, `Module`, which `plugin pack` of in-process code
    calls for the module layer and the SBOM (ADR-047 decision 1); in `aiops` it answers
    `ErrNotCarried` as the other six do.

    Note of 2026-11-27, on the acceptance of the S2b part (decision 47): the two tables
    above gain the parts `Unit`, `Image` and `Boot`, the form of `Suite` for the runtime
    `remote`, the form of `Generate` for a Python plugin, the field
    `TemplateRequest.Runtime` and the runtime of `SuiteRequest`, each marked "from S2b".
    The same minor of `launch` adds the error `ErrNoRuntime`, which a part that needs the
    container runtime returns where the host has none or none that answers, and `Unit`
    also where the host has no Compose. The sketch above stays launch 0.2 as built: the
    three new methods of `Tools` take their signatures with the minor that adds them, and
    in `aiops` each answers `ErrNotCarried` as the other parts do.

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
    | `command-plugin` | `plugin new`, `check`, `smoke`, `pack`, `sign`, `push`, `inspect`, `verify`, `gate --against`, `install`, `enable`, `disable`, `remove`; from S2b `start` | `00:137`; `07:1565`, `:1578`; `08:373`-`375`; ADR-060 decisions 23 and 47; decision 45 |
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
    | `plugin new` | `plugin new <registry> <id> --lang go [--contrib]`, and from S2b `plugin new <registry> <id> [--lang go\|python] [--runtime inproc\|remote] [--contrib]`; `plugin new connector <id> [--contrib] [--against <dist.yaml>] [--plugin <id>]...` | `07:1528`, `:1551`; ADR-047 decision 12; ADR-060 decision 24. `--lang python` arrives at S2b (`07:1551`). Note of 2026-11-27: from S2b the Go form also takes `--runtime remote`, with `inproc` the default (ADR-063 decision 60). Note of 2026-11-27, on the acceptance of the S2b part: `--lang` defaults to `go`, and `--runtime` to `inproc` for Go and to `remote` for Python (decision 50) |
    | `plugin check` | `plugin check [<dir>] [--write] [--json] [--against <dist.yaml>]` | `07:1531`, `:1543`, `:1565`; ADR-060 decision 23 |
    | `plugin smoke` | `plugin smoke <dir> --against <release> [--builder image\|local]`; from S2b `plugin smoke <dir> --against <release> [--modcache <dir>] [--mirror <prefix>=<site prefix>]... [--builder image\|local]` | `07:1578`; ADR-044 decision 1; decision 55: `--modcache` is required where the manifest lists `inproc`, and none of the three flags is read for a manifest that lists `remote` alone |
    | `plugin pack` | `plugin pack [<dir>] [--out <dir>]` | `07:1533`; ADR-047 decision 1 |
    | `plugin sign` | `plugin sign --key <file> [--out <dir>]` | `07:1859`; ADR-020 decision 4; ADR-047 decision 1 |
    | `plugin push` | `plugin push <registry> [--out <dir>]` | `07:1859`; ADR-047 decision 1 |
    | `plugin inspect` | `plugin inspect <spec>` | `08:373`; `07:1860` |
    | `plugin verify` | `plugin verify <spec> [--builder image\|local]` | ADR-060 decisions 31 and 46; ADR-044 decision 1 |
    | `plugin gate` | `plugin gate <spec> --against <dist.yaml>` | `08:493` |
    | `plugin install` | `plugin install <spec> [--profile <p>]` | `08:395`-`397` |
    | `plugin enable`, `disable` | `plugin enable <row> [--profile <p>]`; `plugin disable <row> [--profile <p>]` | `08:375`, `:395` |
    | `plugin remove` | `plugin remove <id> [--profile <p>]` | ADR-047 decision 10 |
    | `plugin start`, from S2b | `plugin start <id>` | `08:1293` at `bbfd839`; decision 45 |
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

    Note of 2026-11-27, on the acceptance of the S2b part: the row of `command-plugin`
    gains `plugin start`, its fourteenth command, and the CLI spec gains the row of
    `plugin start` and the S2b forms of `plugin new` and `plugin smoke` (decisions 45, 50
    and 55). The help goldens change with the code that builds each.

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
      per plugin (`08:695`). Note of 2026-11-27: settled by the S2b part, accepted on that
      date, in decisions 45 to 49, with the systemd target as decision 64 answers it.
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

- Status: **accepted 2026-11-27 by the project owner.** Decisions 45 to 59 are accepted,
  and the owner answered questions Q10 to Q16 on the same date, each with its
  recommended option; "Owner answers of 2026-11-27" below records the answers as
  decisions 60 to 66. ADR-063 decision 2 had this part proposed to the owner before the
  code it governs and accepted before that code merges into `main`. The text of
  decisions 45 to 59 is the proposal as corrected after its review of the same date.
- Scope: what ADR-063 decision 34 has this part settle (`ADR-063:849-854`): `plugin
  start`, with `Start` in the operations definition as a minor with a raised suite
  revision; `plugin new --lang python`; the form of a remote Go plugin, which ADR-063
  decision 60 decided; the text and the exit code of each refusal of `plugin enable`
  (ADR-063 decision 17); what `plugin check` prints for a step that it cannot run for
  want of a container runtime; and the forms of `plugin verify` and `plugin smoke` for
  remote code. Decision 21 makes the toolbox this ADR's, so this part also names every
  part of `Tools` that S2b adds, and it gives the form of `plugin disable`, `remove` and
  `inspect` where remote code changes it. It blocks the operations library and the
  command rows of group 3 of ADR-063 decision 30, the remote Go and Python templates,
  and steps G1, P1, P2, C1, C2, C3 and D2 of the exit of ADR-063 decision 27.
- Edits of the architecture files. Four are made whatever the answers, and the
  acceptance of this part is the owner's word for them: `00:137`, `07:1556`, `07:1566`
  and `07:1570` ("Edits on acceptance" below). Every other edit follows an answer, and
  the question names each line. This part changes no other line of `00`.
- Acceptance order. This part rests on ADR-041 and on the S2b parts of ADR-020,
  ADR-044, ADR-047 and ADR-048, which are proposed on the same date and are not
  accepted. Decision 59 lists each point that it takes from one of them and each point
  that one of them hands to it. This part is accepted together with those parts or
  after them. Where the accepted text of one differs from what decision 59 assumes,
  this part is corrected by a dated note before the code that the point governs merges.
  Note of 2026-11-27: the owner answered the questions of those parts on the same date
  as the questions of this one, so the parts are accepted together. Decision 59 is read
  against their accepted text from that date. Decision 65 names one point where two of
  those texts differ from this part, and the record of the answers leaves the comparison
  of the other rows to a later commit.
- Citations: `00`, `02`, `07` and `08` are the files of `docs/architecture/` with those
  numbers, and `07:1556` is line 1556 of `07` at commit `bbfd839`, the commit this part
  is written against. The sections above cite `a3c85ae`, so a line number here differs
  from theirs. ADR-063 is cited by decision and, inside a long decision, by line at the
  same commit, as `ADR-063:401`. The other ADRs are cited by decision, the plugin
  record by section, "licence" is `docs/design/licence-audit-2026-11.md` by line, and
  code is cited by file and line at the same commit. A decision number with no ADR name
  is a decision of this ADR.

### Context of the S2b part

**What is written.**

- **`plugin start`.** `plugin install` renders the deployment unit of a remote plugin
  per target: "Compose: a fragment and a line in the `sidecars-static` table, started
  with `aiopsctl plugin start <id>`. systemd: a unit and `systemctl enable --now`"
  (`08:1293`; `02:313`; plugin record §11). S2b builds the compose fragment, a file of
  its own under the home that `plugin start` runs, and the systemd unit, and runs both
  (ADR-063 decision 50). The kernel never launches or stops a plugin process (`08:447`,
  `:1293`; `ADR-063:401-403`). `plugin start` creates the network of an instance, which
  holds that instance and the kernel container and nothing else, and attaches both
  (`ADR-063:626-629`). Decision 32 left the command to S2b.
- **The stop.** `plugin disable` and `remove` revoke the instance's credential first,
  then unload the proxy, and "`aiopsctl plugin disable` run on the host also stops" the
  unit (`ADR-063:419-421`, `:520-523`; `07:1195`; `08:1293`). Neither line names a
  target. A shared unit stops only when no enabled row of any profile of the site names
  its plugin (ADR-063 decision 68), and the case of `ADR-063:739-740` reads: "After
  `plugin disable`, and after `plugin remove`, the unit is stopped, and an instance
  started again with the old credential opens no stream".
- **The credential.** The kernel holds the pin of each installed instance in the home,
  and `plugin disable` and `remove` revoke by deleting the pin, which the proxy reads
  at every dial (ADR-063 decision 55). Both sides are authenticated before `Hello`, and
  a log names an identity and never key material (`ADR-063:504-507`, `:516-518`).
- **The templates.** `aiopsctl plugin new <registry> --lang python` "writes a plugin
  skeleton with its `plugin.toml`, a test and a row snippet" (`07:1213`), for each
  registry marked `contrib` (`ADR-063:448-451`). A remote Go plugin is made with `plugin
  new <registry> <id> --lang go --runtime remote`, with `inproc` the default (ADR-063
  decision 60; `07:1556`). Each remote template writes the file from which the plugin's
  image is built: the Go one "puts one static binary on no base layer", "so it names no
  base image", and the Python one names a base image by digest (`ADR-063:455-459`). CI
  generates a plugin from every template and holds it to one state: it compiles, passes
  T0 and the generated tests, and fails conformance on the stubs (`07:1568`;
  `ADR-063:451-452`).
- **The refusal of `plugin enable`.** "`plugin enable` refuses, printing the exact
  command, while the endpoint is missing from the `sidecars` table or does not answer
  the handshake" (`08:398`; `07:1870`; `02:326`). Plugin record §11 writes the same
  sentence directly after the start command of each target. The two halves come from
  two places (`ADR-063:404-418`; ADR-063 decision 54, reading 4). `aiopsctl` reads the
  endpoint table and refuses a row whose endpoint is missing before it submits anything.
  For a listed endpoint it submits, the server process dials, and an instance that does
  not answer or a denied handshake ends in the final result `rolled back`, the files
  restored and exit 1, "with the row, the reason the server gave and, where the
  instance did not answer, the exact command `aiopsctl plugin start <id>`". `aiopsctl`
  never dials an instance.
- **`plugin check` without a container runtime.** Steps 1 to 6 and step 8 need no
  container runtime in any language, and "Step 7 needs one when it packs an image"
  (`07:1570`). Without one the check "reports the step as not run and names what it
  lacks" (`ADR-063:382-384`), and step P2 of the exit reads a `--json` report that shows
  steps 1 to 6 passed and step 7 not run (`ADR-063:660-663`). The gate at the entry
  point `plugin check` runs in step 7 (decision 16; `ADR-063:360`), and a remote
  manifest that declares a `provides` entry, an unmarked registry or an unmarked hook
  is "denied with check 9 at `plugin check`" (`ADR-063:731-735`).
- **`plugin inspect`.** Step C1 of the exit has `plugin inspect` print "each spec's
  rows, configs and image reference" and fetch no image (`ADR-063:667-669`).
- **`plugin verify` and `plugin smoke`.** `aiopsctl plugin verify <spec>` is "the
  explicit, optional re-run of the suite against a pulled image; it is not part of
  install" (plugin record §11). `plugin smoke ./ --against <release>` runs T3 per
  runtime, and "For `remote` it attaches the plugin's image to the released image"
  (`07:1583`). Both are proofs of conformance at S2b, and both have cases
  (`ADR-063:424-427`, `:756-760`; `08:940`). The one exception to the authenticated
  dial is "The Unix socket of `plugin check`", which "is opened by the check runner
  alone, to a process that it started" (`ADR-063:512-515`).

**What the code holds at `bbfd839`.**

- `sdk/def/operations/v0` is at version 0.4 with `compatibleFrom: "0.1"` and suite
  revision 7, and its interface holds no `Start`
  (`sdk/def/operations/v0/definition.yaml:25-30`;
  `sdk/def/operations/v0/operations.go:453`).
- `plugin new` refuses every language but Go
  (`plugins/operations/internal/operations/author.go:39-40`;
  `internal/authoring/tools.go:139-140`). Neither its request nor the template request
  of the toolbox holds a runtime (`sdk/def/operations/v0/operations.go:267`;
  `sdk/def/launch/v0/toolbox.go:294-297`). `templates/` holds the Go templates of three
  registries and the connector template, and the Go package name of a plugin is its
  name without the hyphens (`templates/templates.go:30-34`, `:42`, `:66`). The CI
  workflow that every template writes pins one action that checks the repository out
  and one that sets up Go (`templates/shared/.github/workflows/check.yml.tmpl:18-20`).
- A step of `plugin check` ends as `pass`, `fail`, `not applicable` or `skipped`, and a
  report passes when no step failed (`sdk/def/operations/v0/operations.go:292-295`,
  `:359`). Step 7 packs, signs with a throwaway key and takes the verdict of the gate at
  the entry point `plugin check` on the result, and the attestation that the check
  prints has the digest of the packed artifact as its subject
  (`plugins/operations/internal/operations/artifact.go:203-218`; ADR-020 decision 9).
  `plugin check` writes the author's directory only with `--write`
  (`plugins/operations/internal/operations/author.go:28`).
- `plugin verify` verifies the signature and the attestation first, and a failure of
  either is a refusal with no step printed. It then prints the steps `signature`,
  `attestation` and `suites`, each `pass`, `fail` or `not-built`, and exits 1 unless
  each passed (`plugins/operations/internal/operations/artifact.go:577-596`;
  `plugins/command-plugin/plugin.go:325-340`). An id that `allowUnsigned` lists is
  admitted with the signer `unsigned` and goes on to the step `suites`
  (`plugins/operations/internal/operations/artifact.go:674-675`, `:594`).
- What `plugin inspect` returns holds no image reference
  (`sdk/def/operations/v0/operations.go:234-240`).
- `plugin smoke` requires `--against` and `--modcache` and builds a throwaway
  distribution in every case (`plugins/command-plugin/plugin.go:96`;
  `internal/authoring/smoke.go:80-84`).
- The one place where `aiopsctl` runs a container runtime is the builder, a part of
  `Tools`, and the program it runs is `docker` (`internal/builder/image.go:417`;
  `sdk/def/launch/v0/toolbox.go:46-49`).
- The release artifact that `--against` names is a referrer of the release image: that
  image is its subject (`internal/builder/image.go:46-49`, `:281-299`).
- A generation that fails reaches `aiopsctl` as a result whose report holds, per row, an
  id, an outcome, a state and a text, and no cause
  (`sdk/def/reload/v0/wire.go:194-199`). `aiopsctl` prints that text as it came, after
  "rolled back at generation" (`plugins/operations/internal/operations/install.go:474`,
  `:498-513`). What an artifact supplies is printed elsewhere through a function that
  quotes a string with a control character
  (`plugins/command-plugin/plugin.go:279-284`).

**What was run and what was read.** No container runtime, no Compose and no service
manager was run for this proposal, and no licence text was read for it. Each behaviour
of a runtime that a decision below relies on is marked as an assumption, with the exit
step or the case that proves it, and decision 57 lists each unread licence beside what
it blocks. On 2026-11-27 the owner offered to provide a sandbox environment in which
such runs can be made; none was made for this text.

**Gaps.**

1. No line names the command that the refusal of a missing endpoint prints.
   `ADR-063:415-416` names `aiopsctl plugin start <id>` for an instance that did not
   answer; install, and not `plugin start`, writes the endpoint into the table
   (`ADR-063:630-631`; ADR-063 decision 39); and the case of `ADR-063:753-755` wants
   "the exact command".
2. `aiopsctl` prints the exact command only "where the instance did not answer" and
   makes no dial, and the result gives it the server's reason as a text, with nothing
   that tells an instance that did not answer from a denied handshake or from a peer
   whose identity was refused.
3. No line says how `plugin check` exits when step 7 did not run, nor what becomes of
   the gate that step 7 evaluates.
4. `07:1583` words the test-only profile of smoke for S2a, "the release's `aiops-core`
   bundle plus the plugin's bundle with its rows enabled". A proxy row waits for
   `sidecars` (`07:1150`), whose provider ships in `aiops-selfhost` (ADR-063 decision
   39). And smoke requires `--modcache`, while a remote plugin adds no module to a
   build.
5. No line says through which code the operations library reaches the container
   runtime, for a unit, for the build of an image, for a pulled image or for the boot
   of a released image. From S6a `plugin-manager` consumes `operations` in a server
   process (decision 15), and the kernel never launches or stops a plugin process
   (`08:447`).
6. No line says which command makes a credential again after `plugin disable` deleted
   the pin. The case of `ADR-063:739-740` forbids the old one, and `08:398` gives the
   operator "start the instance" and `plugin enable` and nothing else.
7. The systemd target has a start command that is not a command of `aiopsctl`
   (`08:1293`), while one "exact command" is printed (`08:398`) and `plugin disable`
   "also stops" the unit. No line says what `aiopsctl` does on a site that runs the
   systemd unit, and no file of the home says which target a site runs.
8. No line says where the stop of a unit stands in the order of `plugin disable`:
   before or after the submission, under the write lock or not, and what a stop that
   fails does to a revocation that decision 17 would restore from the snapshot.
9. No line says how `plugin verify` reaches the container that it starts. The
   exception of `ADR-063:512-515` names the socket of `plugin check` alone.
10. No line says how `aiopsctl` finds `aiops-sdk manifest` for step 2 (`07:1575`), who
    compiles the binary of a remote Go plugin before step 4 starts it and before its
    image is built, or what `plugin check --write` does on a Python plugin.
11. With no process on the profile nothing dials, and `08:398` has `plugin enable`
    refuse "while the endpoint ... does not answer the handshake" with no condition.

### Decision of the S2b part

#### `plugin start` and `Start`

45. **`plugin start <id>` is a command of `command-plugin`**, the row of its group
    (decision 30). The row then registers fourteen commands.
    - **Form.** `plugin start <id>`: one argument, the plugin id of an installed remote
      artifact. It takes no `--profile`, because the unit and its instance belong to the
      site and not to a profile: one shared instance per plugin serves the site
      (`08:1293`), and a site holds one digest of a plugin id (ADR-047 decision 7). It
      takes no flag that changes what runs: the unit is rendered from a fixed template
      with three inputs and no other (ADR-063 decision 25). It has no flag at S2b. If
      the S2b part of ADR-047 needs one for a kernel that runs as a binary on the host
      (`ADR-063:631-632`), that flag enters the CLI spec by a dated note. The home is
      `$AIOPS_HOME`; with none, the command is a usage error and exits 2 (decisions 10
      and 17).
    - **What it does, in order.**
      1. It asks the container runtime for the state of the unit of the id (decision
         47). A host with no container runtime ends here, with nothing written.
      2. **The write phase**, under the write lock. It reads the home, refuses an id
         for which the home holds no unit, and holds the unit file that it is about to
         run to the locked artifact (below). Then it writes what the home lacks. Where
         the pin of the id is absent, it makes a new key pair and a new pin for the
         instance, as ADR-041 proposes. With question Q10 (a), where the endpoint table
         lacks the line of the id, it writes the line that install writes for that id.
         It writes nothing else, submits nothing, and releases the lock.
      3. **The run phase**, with no lock. On the compose target it creates the network
         of the instance, starts the instance from the compose fragment and attaches
         the kernel container of the home to that network (`ADR-063:626-629`). On a
         site that runs the systemd unit it does what decision 48 says. Where the write
         phase made a new pair while a container of the unit ran, it stops that
         container and starts the unit again, since a running instance holds the old
         key.
      4. It reads from the container runtime the digest of the image that the running
         container has, and holds it to the digest that the locked artifact pins. Where
         they differ it stops the container and refuses.
    - **The unit is held to the locked artifact.** ADR-063 makes the image digest that
      the unit runs one of the two things that authenticate an instance (ADR-063
      decision 58), and step C3 asserts "the image digest that the signed artifact
      pins" (`ADR-063:683-684`). So `plugin start` does not run a file of the home as
      it finds it. It renders the unit again from the fixed template of install and the
      three inputs, taken from the kept files of the artifact, whose chain to the
      locked digest boot verifies at every composition (ADR-047 decisions 5 and 18),
      and it refuses where the unit file of the home differs from that render in any
      byte. The repository of the image reference is the one value that no kept file
      holds, since the home does not keep the spec of an install (ADR-047 decision 5):
      it is taken from the unit file itself, and the digest never is. An added mount,
      port, user, variable or network, or another digest, is therefore a refusal.
    - **The network.** `plugin start` creates the network as ADR-063 decision 24 has
      it: internal, with the instance and the kernel container as its only members, and
      with no port of the instance published. Where the runtime does not create it so,
      `plugin start` starts nothing and refuses; it never starts an instance on another
      network. The form of the network is the S2b part of ADR-047.
    - **The kernel container.** How `aiopsctl` finds the kernel container of a home,
      and what the step does for a kernel that runs as a binary on the host, are the
      S2b part of ADR-047 (`ADR-063:631-632`). The output and the exit code are this
      part's. The attach has three outcomes, and each is exit 0: `attached`; `already
      attached`; and no kernel container of the home runs on this runtime, in which
      case the instance is started and the output says that the same command attaches a
      kernel container that starts later. A run on an instance that already runs still
      attaches a kernel container that is not attached, which is the case of a kernel
      that the operator replaced after `dist use`.
    - **What it does not do.** It enables no row, writes no profile and submits nothing:
      `08:398` has "start the instance" and `plugin enable <row>` as two steps. It makes
      no dial and does not wait for the listener of the instance, since `aiopsctl` never
      dials an instance and needs no route to one (`ADR-063:406-407`): it returns when
      the container runtime reports the container of the unit running. An instance that
      is still starting when a generation dials it falls under the deadlines that
      ADR-041 fixes for the proxy (`ADR-063:809-812`).
    - **Output**, on stderr, with nothing on stdout (decision 11). One line for the
      instance: `started <id>: <image reference by digest>`, or `running <id>: ...` for
      an instance that already ran and was left, or `restarted <id>: ...` where a new
      pair was made. One line for the kernel container: `kernel container <name>:
      attached`, `kernel container <name>: already attached`, or `no kernel container
      of <home> runs on this container runtime: run "aiopsctl plugin start <id>" again
      once it runs`. Before both, one line per file that the write phase wrote, as
      every write of the home prints it (`plugins/command-plugin/plugin.go:441`), and
      the note `a new credential was made for <id>` or `the endpoint of <id> was
      written again`. The image reference is the one that the running container has.
    - **Refusals**, each with exit 1 (decision 10). The first three write nothing and
      start nothing. A later one keeps what the write phase wrote and says so: a new
      pin and its key are a consistent state, and the next run finds them and writes
      nothing.

      | Refusal | What stderr holds |
      |---|---|
      | The host has no container runtime, or none that answers, or no Compose | The text of `ErrNoRuntime` (decision 47) |
      | The home holds no unit of the id | `<home> holds no unit of <id>: no remote artifact of that id is installed` |
      | The unit file differs from what install renders for the locked artifact | `the unit of <id> in <home> is not what install renders for the locked artifact: <what differs>: remove the plugin from each profile that names it and install it again` |
      | A container of the unit's name runs and is not this unit: another image digest, another network, a published port or another bind | `a container named <unit name> runs and is not the unit of <id> of <home>: <what differs>`. Nothing is started and nothing is stopped |
      | The runtime does not create the network as ADR-063 decision 24 has it | The error of the runtime, and that nothing was started |
      | The runtime does not start the unit, or the running container has another digest than the pinned one | The error of the runtime, or both digests |
      | A refusal that the S2b part of ADR-047 fixes for the kernel container or the network, as two containers that bind the `run/` of one home | The text of that part |
      | The service manager refuses, on a site that runs the systemd unit | Decision 48 |

      The list is open to the S2b part of ADR-047 in its seventh row and closed
      otherwise.
    - **Assumptions**, each read in no source and run on no runtime for this proposal:
      that the runtime attaches to a network a container that started with none; that
      Compose reports a container as running and the runtime reports the digest of its
      image, its networks, its published ports and its binds; and that an internal
      network is created as ADR-063 decision 24 needs. Step C3 of the exit proves each
      on the runtime of the clean machine (`ADR-063:683-694`). A behaviour that the
      runtime does not give stops the work on that point, and the owner is asked
      (ADR-063 decision 33).

46. **`Start` joins the `operations` definition in its next minor**, the minor after
    the version that `main` holds when the change merges, with `compatibleFrom: "0.1"`
    kept: for a consumer a new method, a new field and a new value of an outcome are
    additive changes (`07:547`). The suite revision rises (ADR-060 decision 10;
    `ADR-063:849-850`). The pull request that raises the minor regenerates the
    `requires` of the five command rows (ADR-060 decision 32, rule 4). The minor carries
    every change below, and no other change of the definition is planned by this part.

    | Change of the definition | What it is | Decision |
    |---|---|---|
    | The method `Start`, with `StartRequest` and `Started` | Below | 45 |
    | The error `ErrNotStopped` | Matches the error of a `Disable` or a `Remove` that kept its change and did not stop the unit | 49 |
    | `NewRequest.Runtime` | `inproc` or `remote`; empty is the default of the language | 50 |
    | A fifth outcome of a step of `plugin check`, `not run`, and `CheckReport.NotRun` | The steps that did not run for want of a container runtime. `Passed` keeps its meaning, no step failed, so a caller that must not pass without step 7 reads both | 53 |
    | The step `image` of `plugin verify`, and the outcome `not-run` of a `VerifyStep` | Beside `pass`, `fail` and `not-built` | 54 |
    | `Inspected.Image` | The image reference of remote code; empty for other code | 56 |
    | `Change.Unit`, for `Disable` and `Remove` | `stopped`, `not-running`, `left` or `not-stopped`, with the reason of the last; empty where the method stopped no unit | 49 |
    | `Home.Remote` | Per installed remote plugin id: whether the home holds its unit, its line of the endpoint table and its pin. The suite reads the writes of `Start` and the revocation of `Disable` through it, since it reads no file (decision 18) | 45, 49 |
    | The end of the refusals of `remote` in `Check`, `Pack` and `Install` | ADR-063 decision 17 lifts them (`ADR-063:366-367`). No type changes | |

    | Type | Fields |
    |---|---|
    | `StartRequest` | `Caller`, `Home` and `ID`, the plugin id |
    | `Started` | `ID`; `Outcome`, `started`, `restarted` or `running`; `Image`, the image reference by digest that the running container has; `Target`, `compose` or `systemd` (decision 48); `Kernel`, `attached`, `already-attached` or `none`, with the name of the kernel container; and `Change`, whose `Written` and `Notes` hold what the write phase wrote |

    `Smoke` and `Verify` keep their requests and results but for the step and the
    outcome above. The rows of the method table of decision 16 that change:

    | Method | Command (decision 30) | Writes the home under the lock | Gate entry point (ADR-060 decision 17) | Toolbox parts (decision 21) | Source |
    |---|---|---|---|---|---|
    | `Start` | `plugin start <id>` | Yes, in its write phase alone: the pin of the id with its key pair, where the pin is absent, and with question Q10 (a) the line of the id in the endpoint table, where the table lacks it | None | `Unit` | `08:1293`; `ADR-063:401-403`; decision 45 |
    | `New` | `plugin new <registry> <id> [--lang go\|python] [--runtime inproc\|remote]`, `plugin new connector` | No; writes the author's directory | None | `Template`; `Release` for a connector | Decision 50 |
    | `Check` | `plugin check <dir>` | No; with `--write`, the author's directory | `plugin check` step 7; for remote code also the remote handshake, inside step 5 (`ADR-063:334-336`) | As at S2a, and for remote code `Image` | Decisions 51 and 53 |
    | `Smoke` | `plugin smoke <dir> --against <release>` | No | For remote code: `plugin pack` and `plugin install`, on the throwaway home; boot, of the test-only profile; and the remote handshake inside that boot | `Build` for `inproc`; `Image`, `Unit` and `Boot` for `remote` | Decision 55 |
    | `Pack` | `plugin pack` | No; writes the layout | `plugin pack` | As at S2a, and for remote code `Image` | ADR-063 decision 61 |
    | `Inspect` | `plugin inspect <spec>` | No | None | None | Decision 56 |
    | `Verify` | `plugin verify <spec>` | No | For remote code the remote handshake, inside the step `suites` | `Build` and `Suite`; for remote code `Unit` and `Suite` | Decision 54 |
    | `Enable` | `plugin enable <row>` | Yes | Boot, on the changed profile | `Render` | Decision 52 |
    | `Disable` | `plugin disable <row>` | Yes | Boot, on the changed profile | `Render`; for a remote row `Unit`, after the lock is released | Decision 49 |
    | `Remove` | `plugin remove <id> --profile <p>` | Yes | Boot, on the changed profile | `Render`; for remote code `Unit`, after the lock is released | Decision 49 |

    The row of `Install` changes with the unit that install renders, in the S2b part of
    ADR-047.

    Rules every provider keeps, beside those of decision 17:
    - `Start` refuses an id for which the home holds no unit, with an error that
      matches `ErrRefused`, and starts nothing and writes nothing. With no home it
      returns an error that matches `ErrUsage` (decision 17).
    - Note of 2026-11-27: before it starts anything, `Start` evaluates check 5 on the
      installed artifact as boot does, with no signature verified: the kept files
      against the locked digest, the record against the kept manifest, the
      reserved-owner clause, and the recorded signer against the present `trust.yaml`
      for the id and the runtime `remote`. Where the check denies, it starts nothing,
      names check 5 and `plugin start` exits non-zero. It does not evaluate the tie of
      the entry `aiops/remote`, which needs the server binary (ADR-020 decision 46).
      The text of the refusal is this part's, as ADR-020 decision 40 asks.
    - **The lock.** `Start` is a method that writes the home, and decision 19 holds the
      lock "for the whole of each method that writes the home", through bounded steps
      only. A pull of an image has no bound. So `Start` holds the lock for its write
      phase alone: from before the checks that read the home, through the snapshot of
      decision 17 and the renames, to the removal of the snapshot. It takes the
      decision to write under the lock, on what it reads there, and never on what it
      read before. It releases the lock before the container runtime pulls or starts
      anything. Decision 19 gains a dated note that says so. A `plugin disable` that
      takes the lock between the two phases deletes the pin again; the instance that
      `Start` then starts opens no stream, and the next `plugin start` makes a new
      pair.
    - Neither write needs a submission or a restart, so `Start` answers no `Outcome`.
      The proxy reads the pin at every dial (ADR-063 decision 55). For the line of the
      endpoint table, see question Q10.
    - It passes on the error of decision 47 where the host has no container runtime.

    **Fake and suite** (decision 18). The fake takes from its config what it cannot
    compute without a container runtime, as it takes each signature verdict: the ids
    whose unit, endpoint and pin the home holds, the target of each, and what the
    runtime answers for each: started, already running, the outcome of the attach, a
    stop that fails, or no runtime. It records each call in order. The plugin subject
    of the suite takes the same from the launch fake's toolbox, whose parts `Unit`,
    `Image` and `Boot` answer from its config and record their calls (decision 47). The
    new suite revision adds a case for each rule that the methods can show:
    - `Start` of an id with no unit is refused, starts nothing and writes nothing;
    - `Start` of an installed id answers `started`, the image by digest and the outcome
      of the attach, once `attached` and once `none`;
    - a second `Start` answers `running` and starts nothing, and attaches a kernel
      container that is not attached;
    - `Start` of an id whose pin is absent writes the pin, which `Home` shows, while a
      second writer waits, and answers `restarted` where the instance ran;
    - `Start` of an id whose unit differs from the locked artifact is refused and
      starts nothing;
    - `Start` with no container runtime returns the error that names what the host
      lacks and writes nothing; `Start` with no home is a usage error; `Start` after
      release returns `ErrInactive`;
    - with question Q10 (a), `Start` writes a missing line of the endpoint table again;
    - with question Q14 (a), `Start` of an id whose target is systemd runs no fragment;
    - `Enable` of a remote row whose endpoint the table lacks, and of one whose pin is
      absent, is refused, writes nothing and submits nothing (decision 52);
    - `Disable` of the last enabled row of a remote plugin deletes the pin, submits and
      then stops the unit, in that order; with the submission rolled back the pin is
      back and no stop was called; with a stop that fails the files are kept, the pin
      stays deleted and the error matches `ErrNotStopped`; while another profile's
      enabled row names the plugin the pin is kept and `Change.Unit` is `left`; and
      `Remove` the same (decision 49);
    - each refusal of `New` in decision 50 writes nothing;
    - `Check` of remote code with no container runtime reports step 7 `not run`, holds
      no attestation and names the step in `NotRun`; with a manifest that check 9
      denies it reports step 7 `fail` (decision 53);
    - `Verify` of remote code with no container runtime reports `image` and `suites`
      `not-run` and does not pass; of an artifact with no verified signature it runs
      neither step (decision 54);
    - `Inspect` of remote code answers the image reference (decision 56).

    The fake passes the suite. The cases that need the files or a container runtime
    are T1 tests of `plugins/operations` and steps of the exit (decision 58).

47. **The container runtime is reached through `Tools`, and only there.** At `bbfd839`
    the one place where `aiopsctl` runs a container runtime is the builder, a part of
    `Tools` (`internal/builder/image.go:417`; `sdk/def/launch/v0/toolbox.go:46-49`).
    S2b follows it. `launch` gains at S2b a minor with a raised suite revision
    (`ADR-063:351-352`), and that minor adds to `Tools` the three parts below and to
    two parts a second form. `cmd/aiopsctl` implements each.

    | Part | What it does | Called by (decision 16) |
    |---|---|---|
    | `Unit`, new | For a home and a plugin id, one of four actions on the unit that install rendered. `state`: the target (decision 48), whether the container of the unit runs, the digest of its image, its networks, published ports and binds, its address on the network of the instance, and whether the kernel container of the home is found and attached. `pull`: the image of the unit, by its digest. `start`: creates the network, starts the instance and attaches the kernel container. `stop`: stops the instance | `Start`, `Disable`, `Remove`, `Verify`, `Smoke` |
    | `Image`, new | Builds the plugin image of a plugin directory from its build file on the container runtime and returns it as an OCI layout with the digest of its manifest (ADR-063 decision 61). For a Go plugin it compiles the binary first (decision 51). The name of the file, the build context, the export and the digest are the S2b part of ADR-047 | `Pack`; `Check`, in step 7; `Smoke` |
    | `Boot`, new | Pulls the image of a release by digest, boots a profile of a home on it in the container form of ADR-047 decision 15, with no network, reads each row's state and what it waits for from `run/status.json`, and stops and removes the container | `Smoke` |
    | `Suite`, the form for the runtime `remote` | Starts the subject and runs the embedded binary that drives a remote process, which the S2b part of ADR-044 names. In `plugin check` the subject is the `entrypoint` of the manifest, started once as a local process for steps 4 and 5, with a socket in a directory of mode 0700 that the part made (`ADR-063:369-374`); the part starts and stops it, and the binary only dials. In `plugin verify` the subject is the container of question Q15 | `Check`, in steps 4 and 5; `Verify` |
    | `Generate`, the form for a Python plugin | Runs `aiops-sdk manifest` (decision 51) | `Check`, in step 2 and with `--write` |

    - In `aiops` each answers `ErrNotCarried`, as every part of `Tools` does (decision
      21; `sdk/def/launch/v0/toolbox.go:20`). So no server process starts or stops a
      unit, builds an image or boots one, whoever consumes `operations` there: the
      kernel never launches or stops a plugin process (`08:447`, `:1293`), and from S6a
      `plugin-manager` consumes `operations` in a server process (decision 15).
    - **The program.** The container runtime is the program `docker` on the search
      path of the invocation, the program that the builder runs today
      (`internal/builder/image.go:417`). Compose is its subcommand `compose`. A runtime
      that takes the same commands under that name qualifies. No flag and no variable
      names another program at S2b. Decision 57 holds the licence gate of both.
    - **`ErrNoRuntime`.** The same minor adds the error `ErrNoRuntime` to `launch`. A
      part that needs the container runtime returns an error that matches it where the
      host has none or none that answers, and `Unit` also where the host has no
      Compose. Its text names what the host lacks, as ADR-063 has `plugin check` name
      it (`ADR-063:382-384`): `no container runtime: <program>: <error>`, with the
      program that the part ran, `docker` or `docker compose`, and the error of that
      run. A command decides on the error and prints the text:

      | Command | On `ErrNoRuntime` |
      |---|---|
      | `plugin start`, `plugin smoke`, `plugin pack` of remote code | Exit 1 with the text; nothing written |
      | `plugin check` of remote code | Step 7 is `not run` (decision 53) |
      | `plugin verify` of remote code | The steps `image` and `suites` are `not-run`, and the command exits 1 (decision 54) |
      | `plugin disable`, `plugin remove` of remote code | The change is kept, the unit is `not-stopped`, and the command exits 1 (decision 49) |

    - `TemplateRequest` gains the field `Runtime` (decision 50), and `SuiteRequest` the
      runtime that a suite runs in.
    - **One minor or two.** The same minor of `launch` may carry the consumer-bound key
      of ADR-063 decision 57, which is of group 2 of ADR-063 decision 30, while these
      parts are of group 3. Where group 2 merges first, the parts arrive in the minor
      after it. Each change is additive, `compatibleFrom: "0.1"` is kept in both, and
      nothing in this part needs the two to share a number.
    - The tables of decision 21 gain each part and each form above.

#### The systemd target, and the stop of a unit

48. **On a site that runs the systemd unit, `aiopsctl` does what question Q14 decides.**
    The lines are in gap 7: `08:1293` gives that target a start command of the service
    manager, `08:398` has one "exact command" printed, `07:1195` has `plugin disable`
    stop the unit on any target, and ADR-063 decision 50 builds and runs both targets.
    With the recommended answer, Q14 (a):
    - **The target of an id is found on the host, not stored.** Install renders both
      unit files for every id, and they name the same container, so one runs at a time
      (the S2b part of ADR-047). The target of an id is `systemd` where the service
      manager of the host holds a unit of the unit's name, and `compose` otherwise. A
      host with no service manager of that kind has the compose target. `Unit` asks the
      service manager through its program `systemctl`.
    - **The first start is the operator's**, with `systemctl enable --now` and the path
      of the unit file, as `08:1293` writes it. `aiopsctl` neither enables nor disables
      a unit in the service manager: which target a site runs, and whether a unit
      starts with the host, stay the operator's, as removing a unit from an
      orchestrator does (ADR-063 decision 68).
    - **`plugin start <id>` on that target** runs the write phase of decision 45 as on
      every target, since it is the one command that makes a credential again. It never
      runs the compose fragment there. It starts the unit through the service manager
      where the unit is not active, starts it again where the write phase made a new
      pair, attaches the kernel container, and prints the lines of decision 45. So the
      command that `plugin enable` prints is true on both targets.
    - **`plugin disable` and `plugin remove`** stop the unit through the service
      manager (decision 49) and leave it enabled there. A host that restarts then
      starts an instance whose credential is revoked: it opens no stream (ADR-063
      decision 55), and the inventory of S6a flags it (`07:1195`).
    - **Rights.** `aiopsctl` runs as the site user (decision 17). Where the service
      manager refuses that user, the command exits 1 and prints the command of the
      service manager that it ran, for the operator to run with the rights that the
      unit asks. What `plugin start` wrote before that stays written, and what `plugin
      disable` revoked stays revoked.
    - **Assumptions**, read in no source and run on no host for this proposal: that the
      service manager answers whether it holds a unit of a name and whether that unit
      is active, and starts and stops it by name. The job of ADR-063 decision 50 that
      runs the systemd unit proves each, with the cases of decision 58.

49. **`plugin disable` and `plugin remove` stop the unit after the change is kept, and
    outside the lock.**
    - **When.** Only where they revoke: when no enabled row of any profile of the site
      names the plugin once the change is made (ADR-063 decision 68). That is read
      under the lock, before any write, from the render of the changed profile, which
      decision 17 already makes, and from a render of each other profile of the home.
      While a row of the plugin stays enabled, in another profile or in this one under
      a later layer that overrides the change, they delete no pin and stop nothing, and
      `Change.Unit` is `left`.
    - **The order** is revoke, unload, stop (`ADR-063:419-421`, `:520-523`).
      1. Under the lock: the checks of decision 17, then the snapshot, which holds the
         pin. The pin is deleted with the other changes of the method, before the
         submission.
      2. The submission, which unloads the proxy and ends its session.
      3. Where the outcome restores the files (decision 17), the pin is back with them,
         the revocation did not happen and the unit is not stopped.
      4. Where the outcome keeps the files, the snapshot is removed and the lock is
         released. That is every row of the table of decision 17 that reads "Kept": a
         final result `passed`, `not-running`, and the outcomes whose verdict is
         unknown. The pin stays deleted in each, so the revocation stands.
      5. Then the method stops the unit through `Unit`, with no lock: a stop waits for
         the runtime, and a holder holds the lock through bounded steps only (decision
         19). `Remove` stops the unit before it deletes the files that install
         rendered.
    - **A stop never undoes the revocation.** It runs after the snapshot is gone, so no
      failure of the stop restores the pin, and none turns a kept change into a
      refusal.
    - **Outcomes.** `Change.Unit` is `stopped`; `not-running`, where no container of
      the unit ran; `left`; or `not-stopped`, with its reason: no container runtime
      (`ErrNoRuntime`), an error of the runtime, or a refusal of the service manager
      (decision 48). In a server process `Unit` answers `ErrNotCarried`: nothing is
      stopped, `Change.Notes` says that the unit is stopped only by the command run on
      the host (`07:1195`), and that is no error.
    - **Exit and text.** `stopped`, `not-running` and `left` exit as the submission
      does (decision 17). `not-stopped` returns the `Change` and an error that matches
      `ErrNotStopped`, and the command exits 1 after the lines of the submission:

      ```text
      aiopsctl: plugin disable: operations: the row <row> is disabled and the credential of <id> is revoked; its unit was not stopped: <reason>: run "<stop command>"
      ```

      `<stop command>` is the stop of the target by the unit's name: `docker stop <unit
      name>` on the compose target and `systemctl stop <unit name>` on the systemd
      target. For `plugin remove` the line begins `aiopsctl: plugin remove: operations:
      <id> is removed from <p> and its credential is revoked`. The kernel refuses the
      stream of such an instance whatever it does (ADR-063 decision 55).
    - Decision 17 gains a dated note at "Order and snapshot": for a remote row the stop
      of the unit follows the removal of the snapshot and the release of the lock.

#### The templates

50. **`plugin new` gains the language `python` and the flag `--runtime`.** The CLI spec
    of decision 30 reads `plugin new <registry> <id> [--lang go|python] [--runtime
    inproc|remote] [--contrib]`; the connector form does not change.

    | Form | What it writes | Source |
    |---|---|---|
    | `plugin new <registry> <id>`, with or without `--lang go` and `--runtime inproc` | The Go template of S2a, unchanged | ADR-063 decision 60; ADR-060 decision 24 |
    | `plugin new <registry> <id> --lang go --runtime remote` | The remote Go form of decision 51 | ADR-063 decision 60; `07:1556` |
    | `plugin new <registry> <id> --lang python`, with or without `--runtime remote` | The Python template of decision 51 | `07:1213`; `ADR-063:448-451` |

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

51. **What each remote template writes, and what builds it.** Both write a plugin of
    one registry that declares `scope: [process]` (ADR-063 decision 54, reading 1) and a
    bundle of exactly one row, disabled (`08:393`; ADR-063 decision 67).

    | File | The remote Go form | The Python template | Source |
    |---|---|---|---|
    | The plugin code | The files of the Go template: the same plugin package, with one stub per method of the definition | A package whose `__main__.py` holds one class per contribution, marked with `@contributes`, each method a stub that fails as "not implemented", and the call of `serve()` when it is run as the program | `07:1560-1561`; ADR-063 decision 60; `07:1208`, `:1211`, `:1216-1232` |
    | The program that the unit starts | `cmd/<name>/main.go`: a `main` that serves the package's `Plugin` value through `sdk/remotehost` and holds nothing else, so that value is the one that passes the suite in both runtimes | The package itself | ADR-063 decision 60; `07:1198`; `07:148` |
    | The hand-written manifest half | `plugin.yaml`, with `runtimes: [remote]` and the `entrypoint` below | `plugin.toml`, with the `entrypoint` below; no `plugin.yaml`, which `aiops-sdk manifest` writes | ADR-063 decision 60; `07:148`, `:1210`, `:1213` |
    | `entrypoint` | `./<name>` | `python -m <package>` | `07:148`, `:160`; the S2b part of ADR-048 |
    | Dependencies | `go.mod`, which requires the SDK module, the test kit and the two remote modules | The file that names `aiops-sdk` as the one run-time dependency of the plugin, and the testing part of `aiops-sdk` with its test runner for the template's test, which no plugin image needs (`ADR-063:433-436`), in the form that ADR-041 fixes | `07:51`, `:1566`; `ADR-063:822-825` |
    | The test | `plugin_test.go` of the Go template | `tests/test_<package>.py`, which runs the suite through `aiops_sdk.testing.host()` | `07:1562`; `07:1212`, `:1236-1243` |
    | The image | The build file: no base image and no build stage. It copies the one static binary that the toolbox compiled to `/<name>` and sets the working directory `/` | The build file: a base image by digest, read under ADR-063 decision 35 before the template merges; the package, and `aiops-sdk` with its run-time dependencies | `ADR-063:455-459` |
    | The rest | `bundle.yaml`, `fixtures/`, `README.md` with the row snippet, `LICENSE` and a CI workflow, as the Go template has them | The same, with a CI workflow that sets up the interpreter where the Go one sets up the toolchain | `07:1564-1566`; `07:1213` |

    - **The Python package name.** `<package>` is the owner and the name of the plugin
      id joined by an underscore, with each hyphen written as an underscore: the id
      `acme/stamper-utc` gives `acme_stamper_utc`. With the two segments it is no
      reserved word of the language, and it is the form of the sketch of `07:1216`. The
      Go package name keeps its rule, the name without the hyphens
      (`templates/templates.go:66`).
    - **Who compiles the Go binary.** The toolbox does, with the author's Go toolchain,
      which a Go plugin needs for its check in any case (`07:1536`). An `entrypoint`
      whose first word is `./<name>`, in a plugin directory that holds a `go.mod`,
      names the main package `./cmd/<name>` of that module.
      - For step 4 of `plugin check`, `Suite` compiles that package into a temporary
        directory and starts the binary there in the place of the first word, with the
        other words as its arguments (`ADR-063:369-371`; ADR-063 decision 54, reading
        5). Any other first word is started as written.
      - For the image, `Image` compiles the same package as one static binary for
        Linux and the architecture of the container runtime, and the build file copies
        it. The build file has no build stage, so "it names no base image"
        (`ADR-063:456-458`) holds, and no toolchain image is read or fetched.
      - `plugin check` without `--write` writes nothing into the author's directory
        (`plugins/operations/internal/operations/author.go:28`), so the binary of step
        7 is compiled outside it. Where the build file then finds it, in a build
        context that the toolbox assembles, is fixed with the build file by the S2b
        part of ADR-047 (decision 59).
    - **How step 2 runs `aiops-sdk manifest`.** `aiopsctl` reads no key of
      `plugin.toml` and parses no Python (`ADR-063:381-382`): a directory that holds the
      file `plugin.toml` is a Python plugin.
      - `Generate` runs the program that the variable `AIOPS_SDK` of the invocation's
        environment names, or with none `aiops-sdk` on its search path, with the
        command `manifest`.
      - Without `--write` it has the command write into a temporary directory, and
        step 2 compares that document with the committed `plugin.yaml`, both read by
        the one reader (decision 22). It fails on a difference and on a missing
        `plugin.yaml` (`07:1575`).
      - With `--write` it has the command write `plugin.yaml` in the plugin directory.
        That is all that `plugin check --write` writes on a Python plugin: the two
        generated tests of decision 23 are Go.
      - Where the program is not found or does not exit 0, step 2 is `fail`, with one
        diagnostic of check `2`: `aiops-sdk manifest: <program>: <error>: install
        aiops-sdk, or set AIOPS_SDK to its command`. It is no `not run`: a Python
        author needs `aiops-sdk` (`07:51`).
      - `plugin check` makes two calls of the command and no other: one that names the
        plugin directory and writes its `plugin.yaml`, and one that names the plugin
        directory and a path to write the document to, and leaves the directory as it
        was. The command line of `aiops-sdk` is the Python SDK's, whose scope ADR-041
        fixes (`07:1203`; ADR-063 decision 18), and no proposal fixes it yet (decision
        59).
    - **The state after `plugin new`.** The remote Go form, after `plugin check
      --write`, compiles and holds a `requires` with `remoteProtocol` and `definitions`
      and no `pluginApi` (ADR-063 decision 60; ADR-048 decision 12). The Python template
      holds its `plugin.yaml` after `aiops-sdk manifest` (step P1, `ADR-063:656-659`).
      Each then passes steps 1 to 4 of `plugin check` and fails at step 5 with exactly
      one line per obligation of the suite (`07:1568`; `ADR-063:450-452`, `:658-659`),
      step 5 run in the runtime `remote` (`ADR-063:368-374`). That state needs no
      container runtime.
    - **The template job** generates a plugin from every template and asserts that
      state (`07:1568`; `ADR-063:451-452`, `:986`): at S2b the three Go templates, the
      remote Go form and the Python template of `echoers` and of `stampers`, and the
      connector template. Steps 4 and 5 of a remote plugin need the suite binary of a
      release build, so the job runs the `aiopsctl` of the test release, as the
      connector case does today (`ADR-063:1000-1001`). It also holds a Go toolchain and
      the Python interpreter with `aiops-sdk` and its testing part.
    - The keys of `plugin.toml`, whether `plugin.yaml` carries `entrypoint` and its
      grammar are the S2b part of ADR-048 (`ADR-063:833-836`). The names of the Python
      base classes, of their methods and of the fixture are fixed with the Python parts
      at S2b (`07:66` R8; `ADR-063:822-825`). The name and the format of the build file
      are the S2b part of ADR-047 (ADR-063 decision 61; `ADR-063:826-827`). The
      templates write each in the form that those parts fix.

#### The refusals of `plugin enable`

52. **Each refusal exits 1 and leaves the home as it was** (decision 10; ADR-059
    decision 29). `plugin enable` of a remote row keeps its S2a form, `plugin enable
    <row> [--profile <p>] [--connect-wait <duration>] [--wait <duration>]`. `Enable`
    reaches no instance and no container runtime: its one toolbox part stays `Render`
    (decision 16), so "`aiopsctl` made no dial" holds by construction (step C2,
    `ADR-063:678-682`). A row is a remote row when it sits under the group of an
    installed artifact whose kept `plugin.yaml` lists the runtime `remote`, and its
    plugin id is that artifact's (ADR-047 decision 5; ADR-063 decision 41).

    | Refusal | Who decides it, and when | Files | Exit | What stderr holds |
    |---|---|---|---|---|
    | 1. The endpoint of the row's plugin is missing from the endpoint table | `aiopsctl`, under the write lock, among the checks that read the home, before the snapshot, any rename and any submission (decision 17; `ADR-063:408-410`) | Nothing written | 1 | The home, the plugin id, the row and the exact command that question Q10 gives |
    | 2. The home holds no pin of the row's plugin: its credential was revoked by an earlier `plugin disable` and not made again | `aiopsctl`, in the same checks, before any submission, as ADR-041 proposes | Nothing written | 1 | The home, the plugin id, the row and the exact command `aiopsctl plugin start <id>`, which makes a new credential (decision 45) |
    | 3. The instance did not answer: the dial of the proxy was refused, found no address, or got no answer inside the handshake deadline that ADR-041 fixes | The server process: its proxy dials inside the generation that `aiopsctl` submitted, the row is not ACTIVE and the verdict fails (`ADR-063:410-416`; `08:477`) | Restored from the snapshot (ADR-062 decision 11) | 1 | The final result `rolled back`, the row, the reason the server gave and the exact command `aiopsctl plugin start <id>`, which reaches the output as question Q11 decides |
    | 4. The identity is refused. Something answers at the endpoint, and the two sides are not authenticated: the peer presents another certificate than the pin; the peer refuses the kernel's certificate; or the peer does not complete the TLS handshake, as a listener in plaintext does not (`ADR-063:504-507`, `:736-738`) | The server process, in the same generation, before any `Hello` | Restored from the snapshot | 1 | The final result `rolled back`, the row and which of the three it was, with the plugin id and the fingerprint of the certificate that the kernel expected and of the one it saw. It names no check, since no check of the gate ran, holds no key material (`ADR-063:516-518`), and holds no command to start the instance, since something runs there |
    | 5. The handshake is denied: no common protocol major, another clause of check 3 or of check 9 that the handshake evaluates, a bound of ADR-041 that the peer passed, or a rule of the protocol that it broke | The server process, in the same generation, after both sides are authenticated (ADR-063 decisions 16 and 66) | Restored from the snapshot | 1 | The final result `rolled back`, the row and the denial as the server gave it, which names the check and, for no common major, holds both lists of majors (`ADR-063:345-347`, `:706-713`). It holds no command to start the instance, which runs |
    | 6. The changed profile is not admitted: a clause that boot evaluates on the manifest denies the row, from S2b the majors clause of check 3 and check 9 among them | `aiopsctl`, in the render of the changed profile before any rename (decision 17; ADR-063 decision 16) | Nothing written | 1 | The denial with its check, as at S2a |

    - **Where each identity case falls.** A process at the endpoint with another
      identity, and one that listens in plaintext, are row 4 (`ADR-063:736-738`). An
      instance that holds a stale copy of the kernel's certificate is row 4. A pin that
      the home lacks is row 2, decided by `aiopsctl` before it submits; a proxy that
      finds no pin at a dial, as at the boot of a profile in which an edit of a file
      enabled the row, opens no connection and fails with the reason "revoked", as
      ADR-041 proposes. None of them is reported as "did not answer", so none tells
      the operator to start an instance that runs.
    - **Row 2 is open until ADR-041 is accepted.** ADR-063 decision 34 gives ADR-041
      the mechanism of the revocation. This part takes from its proposal that a
      revoked pair never returns, that `plugin start` makes the new one and that
      `plugin enable` refuses before it submits; the text and the exit code are this
      part's.

    **The texts.** Each is in the one line that the `cli` row prints for a failed
    command, `aiopsctl: plugin enable: <error>` (decision 10). `<home>`, `<id>`, `<row>`
    and `<p>` stand for the home, the plugin id, the row and the profile.

    - Row 1, with question Q10 (a):

      ```text
      aiopsctl: plugin enable: operations: refused: the endpoint table of <home> holds no endpoint of <id>, the plugin of the row <row>: run "aiopsctl plugin start <id>"
      ```

      With Q10 (b) the text after the last colon is `run "aiopsctl plugin remove <id>
      --profile <p>", then install it again`.
    - Row 2:

      ```text
      aiopsctl: plugin enable: operations: refused: <home> holds no pin of <id>, the plugin of the row <row>: its credential was revoked: run "aiopsctl plugin start <id>"
      ```

    - Rows 3 to 5. The line is the one that a failed generation prints at S2a, which
      holds the result, the generation and one part per failed row
      (`plugins/operations/internal/operations/install.go:474`, `:498-513`). The part
      of the row reads, for row 3:

      ```text
      row <row> fail: the instance of <id> did not answer: run "aiopsctl plugin start <id>"
      ```

      for row 4, one of:

      ```text
      row <row> fail: the peer at the endpoint of <id> is not the pinned instance: expected <fingerprint>, saw <fingerprint>
      row <row> fail: the peer at the endpoint of <id> refused the kernel's certificate <fingerprint>
      row <row> fail: the peer at the endpoint of <id> did not complete the TLS handshake
      ```

      and for row 5 `row <row> fail:` and then the denial that the server gave. With
      question Q11 (a) the words after `fail:` are the reason that the proxy gave, and
      this decision fixes them for rows 3 and 4.
    - **What a peer supplies never reads as a command.** The words `run "aiopsctl` are
      written by first-party code alone, as the last clause of a reason. A reason
      holds nothing that the peer supplied but numbers, as the two lists of majors, a
      fingerprint that the kernel computed, and a value from a closed list of ADR-041.
      A free text of the peer, as `Hello.diagnostics`, is logged and enters no reason
      (`ADR-063:813-816`). `aiopsctl` prints the text of a row through the function
      that quotes a control character (`plugins/command-plugin/plugin.go:279-284`),
      which the line of a failed generation does not use today. A case of decision 58
      holds a peer whose messages hold the words of a command to a reason that holds
      none.

    Where no process runs the profile, nothing dials, and what `plugin enable` of a
    remote row then does is the reading of question Q16. Every other outcome of a
    submission keeps the table of decision 17.

#### `plugin check` without a container runtime

53. **A step that cannot run for want of a container runtime is reported `not run`,
    and the gate still reads the manifest.** A step of `plugin check` gains a fifth
    outcome beside `pass`, `fail`, `not applicable` and `skipped`
    (`sdk/def/operations/v0/operations.go:292-295`).
    - **Which step.** Only step 7, and only on a plugin whose manifest lists `remote`:
      steps 1 to 6 and step 8 need no container runtime in any language, and step 7
      needs one when it packs an image (`07:1570`; `ADR-063:382-384`). For in-process
      code and for a pack, step 7 packs no image and runs as at S2a.
    - **The gate is not skipped with the pack.** The gate at the entry point `plugin
      check` runs in step 7 (decision 16), and the clauses that it adds for remote code
      read the manifest and the bundle and nothing that a pack produces: the majors
      clause of check 3, the contribution-set clause of check 9 and the one-row clause
      of check 8 (`ADR-063:360`; ADR-063 decision 67). So where `Image` answers
      `ErrNoRuntime`, step 7 still calls `Gate` at that entry point on the manifest and
      the bundle, with no artifact. Each clause that reads the digest, the signature or
      the attestation is reported "not evaluated: no artifact was packed" (ADR-060
      decision 17). A clause that denies makes step 7 `fail`, with the check named, and
      the check exits 1. So a remote manifest that declares a `provides` entry is
      denied with check 9 at `plugin check` on a machine with no container runtime too
      (`ADR-063:731-735`).
    - **What it prints** where no clause denied. On stderr, in the form of every step
      (`plugins/command-plugin/plugin.go:183`), the line `step 7 pack and verify: not
      run`, and under it the text of decision 47, which names what the host lacks, and
      the words `the gate read the manifest; pack, sign and verify did not run`. In the
      `--json` report step 7 holds the outcome `not run` and one diagnostic of check `7`
      with that text, and `NotRun` names the step (step P2, `ADR-063:660-663`). Step 8
      after it is `not applicable`, as for all code.
    - **No attestation.** In the place of "the attestation it would sign" the check
      prints the line `no attestation: step 7 did not run`, and the report holds none:
      the subject of the attestation is the digest of the artifact that step 7 packs
      (ADR-020 decision 9; `plugins/operations/internal/operations/artifact.go:209-218`).
    - **`not run` is never `pass`.** On that machine `plugin pack` refuses for want of
      the same runtime (decision 47), so nothing is packed, signed or pushed from a
      check whose step 7 did not run. `CheckReport.Passed` answers whether no step
      failed, as today (`sdk/def/operations/v0/operations.go:359`), so it answers yes
      for such a report; `NotRun` is what tells it from a check that ran whole.
    - A step after a failed step stays `skipped`, and the check still stops at the first
      failed step (`07:1570`). How the command exits when step 7 is `not run` and no
      step failed is question Q12.

#### `plugin verify`, `plugin smoke` and `plugin inspect` for remote code

54. **`plugin verify <spec>` of remote code re-runs the suites against the pulled
    image** (plugin record §11; `ADR-063:424-427`). It is a command of its own and no
    part of install, which pulls no image and runs none (`ADR-063:392-394`).
    - **Form.** `plugin verify <spec>`, as at S2a. The flags `--builder`, `--against`,
      `--modcache` and `--mirror` name the build of in-process code (ADR-060 decision
      46; ADR-044 decision 1) and are not read for an artifact that holds no module. It
      writes nothing in the home and reads the `trust.yaml` of the home, as at S2a.
    - **Before the steps.** The signature and the attestation are verified first, and
      a failure of either is a refusal that names check 5 or check 10, exits 1 and
      prints no step, as at S2a
      (`plugins/operations/internal/operations/artifact.go:586-589`). Nothing but
      `plugin.yaml` is fetched before both hold (ADR-047 decision 2), so no image is
      pulled and no code runs of a signer that the home does not trust for the id and
      the runtime `remote` (`07:1866`).
    - **Steps.** It prints the digest and the signer, then each step as `step <name>:
      <outcome>: <reason>` on stdout (`plugins/command-plugin/plugin.go:334`):
      `signature`, `attestation`, `image` and `suites`.

      | Step | Passes when | Names on a failure |
      |---|---|---|
      | `signature`, `attestation` | As at S2a (ADR-047 decision 2; ADR-020 decision 14) | As at S2a |
      | `image` | The layer of the artifact that pins the image hashes to its descriptor; `Unit` pulled the image under the pinned digest (`08:393`; `ADR-063:385-391`); and the image that the container runtime then holds has that digest | For a layer whose bytes do not hash to its descriptor, both digests. For a pull that the registry or the runtime refuses, the pinned digest and the error. For an image that the runtime holds under another digest, both digests |
      | `suites` | The container started, the handshake passed with checks 3 and 9 evaluated as on a server, and the obligations half of each suite passed through the adapter against it, as step 5 of `plugin check` runs it against a local process (`ADR-063:368-374`). The guard half never runs remote (`ADR-063:328-329`) | A denial of the handshake, with its check; a first message whose digest differs, with check 3 and both digests; each failed obligation, by its id |

    - **"Fails and names what differs"** (`ADR-063:756-758`) is therefore three cases.
      A copy whose manifest was rewritten after signing has another digest, which no
      verified signature names: a refusal with check 5, before any step. A copy with a
      changed layer under the signed manifest fails at the fetch of that layer:
      `plugin.yaml` as a refusal before the steps, the layer that pins the image in the
      step `image`, the bundle in the step `suites`, each with both digests. A copy
      whose image was changed fails in the step `image`. The case of decision 58 holds
      one copy of each.
    - **An artifact with no verified signature.** At `bbfd839` an id that
      `allowUnsigned` lists is admitted with the signer `unsigned` and goes on to the
      step `suites` (`plugins/operations/internal/operations/artifact.go:674-675`).
      Whether such an artifact is admitted as remote code is the S2b part of ADR-020
      (`ADR-063:845-848`). Until that part is accepted the default is closed: for an
      artifact of remote code whose signer is `unsigned`, the steps `image` and `suites`
      are `not-run`, with the reason that no key vouches for the image, nothing is
      pulled, nothing runs and the command exits 1. The acceptance of that part is a
      precondition of the merge of `Verify` for remote code; where it admits such an
      artifact, a dated note here says what the two steps then do.
    - **The container.** `Unit` starts it for the step `suites` and removes it, with
      what it made, when the command returns. It runs the image by the pinned digest
      and by no tag, under a user that is neither the invoking user nor root, with no
      route out, no path of the site's home and no credential of the site (ADR-063
      decisions 24 and 25), since the command runs on an operator's host the image that
      install only pins. How the runner reaches it, and what identifies each side, is
      question Q15.
    - **Outcomes and exit.** A step is `pass`, `fail` or `not-built`, as at S2a
      (`sdk/def/operations/v0/operations.go:257-259`), or `not-run` where the host has
      no container runtime, for `image` and `suites`, with the text of decision 47. An
      `aiopsctl` that embeds no suite binary reports `suites` as `not-built`, as today.
      The command exits 0 only when every step passed and exits 1 otherwise, naming
      the steps (`plugins/command-plugin/plugin.go:334-340`): a step that did not run
      did not pass. So `plugin verify` of remote code needs a container runtime.
    - **Assumptions**, read in no source and run on no runtime for this proposal: that
      a pull by digest fails where the content does not hash to the digest, and what a
      registry answers for such a pull. The case of `ADR-063:756-758` proves them.
    - What the `suites` step compares with the attestation, and whether the attestation
      names the runtimes its suites ran in, is the S2b part of ADR-020
      (`ADR-063:842-844`).

55. **`plugin smoke <dir> --against <release>` of remote code attaches the plugin's
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
    - **The path, in order** (reading 4). It is what `08:398` gives an operator, on a
      throwaway home, through the methods of this definition.
      1. `Pack`, which builds the plugin's image through `Image` (ADR-063 decision 61),
         and a signature with a throwaway key, as step 7 of `plugin check` makes one
         (`07:1580`). The throwaway home trusts that key for the id and the runtime
         `remote` (`07:1866`).
      2. `Install` into the test-only profile and `Enable` of the row. No process runs
         that home yet, so each keeps its files and answers `not-running` (decision 17;
         question Q16).
      3. `Start`. No kernel container runs yet: the instance starts and the attach
         answers `none` (decision 45).
      4. `Boot` starts the released image on that home, in the container form of
         ADR-047 decision 15, with no network.
      5. `Start` again: the instance runs, and the kernel container is attached.
      6. Smoke reads the row states from `run/status.json` until the row of the plugin
         is ACTIVE, for no longer than the bound that smoke gives a boot at S2a
         (`internal/authoring/smoke.go:131-132`).

      The boot of step 4 dials before step 5 attaches, so the row first follows the
      matrix row "Remote unreachable or handshake mismatch" for an optional row at boot
      (`08:477`) and becomes ACTIVE by the proxy's own return, which ADR-041 fixes
      (`ADR-063:809-813`). That is the path of a site whose operator starts an instance
      after the kernel. The instance therefore runs behind the unit, the credential and
      the network that a site gives it (ADR-063 decisions 23 to 25).
    - **The test-only profile** is the release's `aiops-core` and `aiops-selfhost`
      bundles plus the plugin's bundle with its row enabled (reading 2). It keeps the
      `--patch` file that disables `reload-socket`: nothing submits in smoke (ADR-062
      decision 27).
    - **Pass, output and exit.** The rule of ADR-060 decision 58, reading 1, stands:
      smoke passes when every row is ACTIVE or says what it waits for, and a row of the
      plugin that does not mount fails it; for remote code the row of the plugin must
      be ACTIVE inside the bound of step 6. It prints each row on stdout as at S2a, `row
      <id>: <state>` with the keys it waits for
      (`plugins/command-plugin/plugin.go:310-313`), and exits 1 unless it passed.
    - **What it leaves.** It removes the containers, the network and the home that it
      made, and writes no home of the site and nothing beside the plugin, as at S2a
      (`sdk/def/operations/v0/operations.go:484`). `Smoke` keeps its request and its
      result (decision 16).

56. **`plugin inspect <spec>` of remote code prints the image reference.** Step C1 of
    the exit reads it there (`ADR-063:667-669`). `Inspected` gains the field `Image`
    (decision 46), and the command prints one more line on stdout, `image: <image
    reference by digest>`, after the runtimes, through the function that quotes what an
    artifact supplies (`plugins/command-plugin/plugin.go:265`, `:279-284`). The command
    fetches no image, no blob of an image and nothing beyond what it fetches at S2a,
    verifies no signer and says so (ADR-047 decision 2). From which part of the
    artifact it reads the reference without fetching a further blob is the S2b part of
    ADR-047 (`ADR-063:826`).

#### Licences

57. **This part asserts no licence text, and each unread item blocks what is named
    beside it.** No licence was read at a primary source for this proposal. ADR-063
    decision 35 lets nothing merge unread: each item below is read at a primary source
    at its pinned version, entered in the licence record and in the upstream register,
    in the commit that first uses it.

    | Unread item | What is written of it | What it blocks in this part |
    |---|---|---|
    | Compose: the subcommand `compose` of the program `docker` (decision 47), at a pinned release | `ADR-063:913-915` lists it as not read; licence:60 lists "Compose" among "Build, test and lab tools", with the mode `tool`, with no version and no date of reading | The first commit that runs it: `Unit`, and with it `plugin start`, the stop of decision 49, `plugin verify` and `plugin smoke` of remote code; the clean machine of the exit, which holds "what `plugin start` needs" (`ADR-063:620-621`); steps C3, C4 and D2 |
    | The container runtime of the exit's environments, with its build command | `ADR-063:913-914`, not read | `Image` in the exit, so steps G2 and P2; `Boot` |
    | The program `systemctl` of the host, which `Unit` runs with question Q14 (a) | No row of the licence record | The systemd form of `Unit` and the systemd job of ADR-063 decision 50 |
    | The base image of the Python plugin image, with its layers | `ADR-063:911`, not read (`08:1347` L13) | The build file of the Python template, so the template itself; step P2 |
    | A CI action that sets up Python | `ADR-063:909-910`, not read | The CI workflow of the Python template, which stands where the Go one pins its two actions (`templates/shared/.github/workflows/check.yml.tmpl:18-20`) |
    | The Python interpreter, and the form in which CI and the exit fetch it | `ADR-063:909`, not read | The template job on the Python templates; steps P1 and P2 |
    | The libraries inside the native extensions of `grpcio` and `protobuf` | `ADR-063:898-899`; the owner's condition of ADR-063 decision 52 | The first Python dependency, so `aiops-sdk` in the template job and the dependency file of the Python template |
    | The six packages that `pytest` requires | `ADR-063:900`, not read | The test of the Python template, and the template job on it |
    | The build backend of `aiops-sdk` | `ADR-063:912`, not read | The package that the template job installs |

    - How the licence record classes a program that a shipped command runs on an
      operator's host, and that the project neither ships nor fetches there, is part
      of the reading of the first and the third row. licence:60 has Compose in a class
      of tools that the project fetches for its own builds, tests and labs. This part
      states no class for it.
    - The Go build file names no base image and has no build stage (decision 51), so
      the remote Go form waits for no image reading.

#### Tests

58. Under the working rules of ADR-063 decisions 29 and 31, each with a test that fails
    on its base:
    - the suite cases of decision 46, on the plugin and on the fake, and a test that
      `Unit`, `Image` and `Boot` each answer `ErrNotCarried` in `aiops`, as the other
      parts of `Tools` do;
    - the help goldens of `plugin start`, `plugin new` and `plugin smoke` under
      `testdata/help/` of `command-plugin`, and the cases of `plugin start` in its
      `fixtures/commands/` (decisions 5 and 30);
    - exit codes, beside those of decision 31: `plugin start` with no home exits 2;
      each refusal of decision 45 exits 1, and each of its three outcomes of the attach
      exits 0; each refusal of decision 52 exits 1 with its text, rows 1 and 2 with
      nothing submitted (`ADR-063:753-755`); `plugin check` with step 7 `not run` exits
      as question Q12 decides and prints no attestation, and with a manifest that
      check 9 denies it exits 1 with no container runtime; `plugin verify` of remote
      code with no container runtime exits 1; and `plugin disable` whose stop failed
      exits 1 with the files kept;
    - T1 tests of `plugins/operations`, on the files: `Start` writes the pin and its
      key under the lock and holds no lock while the unit starts; a unit file with one
      changed byte is refused; `Disable` deletes the pin before it submits and calls
      the stop after the snapshot is gone; a submission that is rolled back restores
      the pin; a stop that fails restores nothing;
    - the peer case of decision 52: a reason holds nothing of a peer that reads as a
      command;
    - each refusal of `New` in decision 50, with nothing written;
    - the template job with the remote Go form and the Python template of each `contrib`
      registry (decision 51; `ADR-063:986`);
    - the cases of `plugin verify` and `plugin smoke` that ADR-063 decision 28 lists
      (`ADR-063:756-760`), with the three copies of decision 54;
    - with question Q14 (a), in the job of ADR-063 decision 50 that runs the systemd
      unit: `plugin start` on a unit that the operator enabled runs no fragment and
      leaves one container; `plugin disable` stops it; and after it a `plugin start`
      and a `plugin enable` give a row that is ACTIVE on a new credential.

    The exit takes from this part:

    | Step of ADR-063 decision 27 | What it uses |
    |---|---|
    | G1 | The remote Go form (decisions 50 and 51), and step 7 of `plugin check` run on a machine that holds a container runtime (decision 53) |
    | P1 | The Python template and its state, with step 2 run through `aiops-sdk manifest` (decisions 50 and 51) |
    | P2 | The report of `plugin check` with step 7 `not run` and the runtime it lacks (decision 53; question Q12) |
    | C1 | `plugin inspect`, which prints the image reference and fetches no image (decision 56) |
    | C2 | `plugin enable` before `plugin start`: refusal 3 of decision 52, with its text |
    | C3 | `plugin start` (decisions 45 to 47), with the assumptions of decision 45, then `plugin enable` with the final result `passed` |
    | D2 | `plugin enable` of a started instance that offers another major: refusal 5 of decision 52, with the check and both lists |

#### What the S2b part takes from the other parts, and what it does not settle

59. **Each point has one owner.** ADR-041 and the S2b parts of ADR-020, ADR-044,
    ADR-047 and ADR-048 are proposed on the same date and none is accepted. The first
    table holds what this part assumes of each, and the second what each, as first
    proposed, hands to this part, with the decision that takes it. A row marked "open"
    is settled by no text at this proposal, or by two that differ.

    | Point | Owner | What this part assumes | State |
    |---|---|---|---|
    | Which command makes a credential again after a revocation, and what `plugin enable` does with no pin | ADR-041 (`ADR-063:816-821`) | `plugin start`, on every target, under the write lock; `plugin enable` refuses before it submits (decisions 45 and 52) | Open: the proposal of ADR-041 says so, and the proposal of the S2b part of ADR-047 says that `plugin start` writes no file of the home. One of the two is corrected before either is accepted |
    | The files of the unit, of the endpoint table and of the credentials under the home; the name of a unit; both targets rendered at install | The S2b part of ADR-047 (`ADR-063:826-832`; for the two targets, ADR-063 decision 50) | A unit name per plugin id that names the container, its network and the systemd unit; both unit files rendered for every id | Proposed there |
    | The file that pins the image among the kept files of an install | The S2b part of ADR-047 | Install keeps it, so that `Start` holds a unit to the locked artifact with no registry (decision 45) | Open: this part needs it kept |
    | The network of an instance; how `aiopsctl` finds the kernel container; a kernel on the host | The S2b part of ADR-047 (`ADR-063:631-632`) | The three outcomes of the attach and the open row of refusals (decision 45) | Proposed there, with a question on the kernel on the host |
    | The build file, the build context and where the compiled Go binary lies in it | The S2b part of ADR-047 (ADR-063 decision 61) | A context that holds the binary without a write into the author's directory (decision 51) | Open: its proposal passes the plugin directory as the context and names no place for the binary |
    | What `plugin verify` of remote code does with a local-path spec, which names no repository to pull from | The S2b part of ADR-047 | What it decides for `plugin install` of one | Open: a question of that part |
    | `entrypoint` in `plugin.yaml`, and its grammar | The S2b part of ADR-048 (`ADR-063:833-836`) | It is in `plugin.yaml`, and its grammar takes `./<name>` and `python -m <package>` (decision 51) | Proposed there, as a question |
    | The command that the fixture wraps; the deadline that makes "did not answer"; the closed list of reasons | ADR-041 (`ADR-063:799-825`) | Decisions 51 and 52 | Proposed there |
    | The command line of `aiops-sdk manifest` | The Python SDK, whose scope ADR-041 fixes (`07:1203`; ADR-063 decision 18) | The two calls of decision 51 | Open: no proposal fixes it |
    | The dial of the check runner to a container of `plugin verify`, with question Q15 (a) | ADR-041, which fixes the one dial of the proxy (`ADR-063:822`) | A dial with a throwaway pair, made as install makes one | Open: no proposal names it |
    | The binary that drives a remote process, and which `aiopsctl` embeds it | The S2b part of ADR-044 (`ADR-063:862-863`) | One binary, embedded by a release build alone (decisions 47 and 51) | Proposed there |
    | What `plugin verify` compares with the attestation; an unsigned remote artifact | The S2b part of ADR-020 (`ADR-063:842-848`) | The closed default of decision 54 | Proposed there, as a question |
    | When `sidecars-static` reads the endpoint table again | The definition `sidecars` and its provider (ADR-063 decision 39). No ADR of ADR-063 decision 34 owns it by name | At every generation or at every lookup (question Q10) | Open: the owner is asked in Q10 which text carries it |

    | Handed to this part | By | Taken in |
    |---|---|---|
    | The text and the exit code of the refusal of `plugin enable` for an absent pin; what `plugin start` does for a systemd unit | ADR-041 | Decisions 52, 45 and 48 |
    | The form of the two commands that start a unit; the output of the attach and of the refusals of the kernel container | The S2b part of ADR-047 | Decisions 45 and 48 |
    | How `aiopsctl` finds `aiops-sdk manifest`; what builds the binary of a remote Go plugin before step 4; the text of each refusal | The S2b part of ADR-048 | Decision 51 |
    | Which process starts the entrypoint; how `plugin verify` starts the image and reaches it | The S2b part of ADR-044 | Decision 47; decision 54 and question Q15 |
    | The flags, the step names, the texts and the exit codes of `plugin verify` for remote code | The S2b part of ADR-020 | Decision 54 |

    Not settled by this part, each with where it is:
    - what a manifest that lists `inproc` and `remote` packs, installs and smokes: the
      S2b part of ADR-048 (`ADR-063:838-840`). Decisions 54 and 55 read a manifest that
      lists `remote` alone;
    - when a shared unit stops and what `remove` deletes: ADR-063 decision 68, which
      decision 49 follows;
    - the Helm entry of a unit: with `deploy/helm` (ADR-063 decision 50);
    - how `plugin-manager` stops a unit from S6a, where `Unit` is not carried: the ADR
      of S6a (decision 32).

### Consequences of the S2b part

- `command-plugin` registers fourteen commands. `operations` holds twenty-four methods
  at its next minor, with the error, the fields and the values of decision 46 and a
  raised suite revision. `launch` gains `Unit`, `Image`, `Boot`, a second form of
  `Suite` and of `Generate`, `ErrNoRuntime` and two fields. No definition and no key is
  added.
- `Start` is a method that writes the home: the pin and its key where the pin is
  absent, and with the recommended answer to Q10 one line of the endpoint table. It
  holds the write lock for that phase alone, and decision 19 says so by a dated note.
- No server binary can start, stop, build or boot a container: every part that reaches
  the container runtime is absent from `aiops`.
- `aiopsctl` runs the container runtime in more places than at S2a, runs Compose for
  the first time and, with the recommended answer to Q14, runs the service manager of
  the host. Each is a program that it runs and does not link. Each waits for its
  reading (decision 57).
- A unit file that was edited after install is not run by `plugin start`, and a
  revoked credential comes back only as a new pair.
- `plugin disable` and `plugin remove` of remote code can exit 1 with their change
  kept, where the unit was not stopped. The revocation stands in every such case.
- `templates/` holds eight templates where S2a holds four, and the template job runs
  the `aiopsctl` of the test release with a Go toolchain and a Python interpreter with
  `aiops-sdk`.
- With the recommended answer to Q10, the endpoint table has two writers, `plugin
  install` and `plugin start`, and its provider reads it again after boot.
- With the recommended answer to Q11, the text with which the proxy fails a row is
  fixed by this ADR for two refusals and names a command of `aiopsctl`.
- With the recommended answer to Q12, `plugin check` on a machine with no container
  runtime exits 0 on remote code and prints no attestation, `make check` needs no
  container runtime, and the CI jobs that check a remote plugin hold one.
- With the recommended answer to Q15, `plugin verify` makes a throwaway home with
  throwaway key pairs for each run, and ADR-063 decision 23 keeps its one exception.
- The line caps of ADR-063 decision 36 stand and are estimates: fourteen commands under
  800 lines and eight templates under 300 lines of Go were measured on no code, and a
  growth past a provisional cap needs the owner's word. The kernel does not change
  (ADR-063 decision 3).

### Edits on acceptance of the S2b part

Made in the commit that records the owner's answers (`08:999`). The first five items
are made whatever the answers.

- This ADR: the two Status lines. In decision 16, the method `Start`, the types and
  fields of decision 46 and each row of its method table. In decision 17, the dated
  note of decision 49. In decision 19, the dated note of decision 46. In decision 21,
  the parts `Unit`, `Image` and `Boot`, the two forms, the error `ErrNoRuntime` and the
  fields of decision 47. In decision 30, `plugin start` in the row of `command-plugin`
  and the CLI spec rows of `plugin start`, `plugin new` and `plugin smoke`. In decision
  32, a dated note at the item of `plugin start`.
- `00:137`: `start` among the `plugin` commands of P0, which `02:313` and `08:1293`
  already name.
- `07:1556`: the sentence "The Python template arrives with the Python SDK at S2b" gains
  the registries that have a remote form and the default runtime of each language
  (decision 50).
- `07:1566`: "the SDK and the test kit only" gains the two remote modules for the
  remote Go form, as `07:51` has them (decision 51).
- `07:1570`: the outcome `not run` of step 7, that the gate still reads the manifest
  and that no attestation is printed (decision 53); and, with the answer to Q12, how
  the check then exits.
- `07:1583`: with the answer to Q13, the released image, the test-only profile of
  `remote`, the flags that `remote` does not read and the order of the path.
- With Q10 (a), the row of the endpoint table in the home table of ADR-047 decision 13
  names `plugin start` beside `plugin install`, in the S2b part of ADR-047, and the
  text that question Q10 names says when the provider reads the table. With Q10 (b),
  `08:398`, `07:1870`, `02:326` and plugin record §11 gain the command of each half.
- With Q11 (b), ADR-062 decision 7 gains a dated note.
- With Q12 (c), `07:1536` gains the container runtime.
- With Q14 (a), `08:1293`, `02:313` and plugin record §11 gain the sentence on a unit
  that the operator enabled. With Q14 (b), `08:398`, `07:1870`, `02:326` and plugin
  record §11 say "the command of the unit's target". With Q14 (c), `08:1293`,
  `07:1195`, `08:398`, `07:1870`, `02:326` and plugin record §11 gain "on the compose
  target".
- With Q15 (b), ADR-063 decisions 23 and 54 gain a dated note that names the socket of
  `plugin verify`.
- With Q16 (a), `08:398`, `07:1870`, `02:326` and plugin record §11 gain "while a
  process runs the profile" for the half that the server decides.
- An option that the owner does not choose edits nothing.

### Questions for the owner on the S2b part

Each question gives its options, the recommended one, why, and what the answer blocks.
An option that changes a written line says so and names every line it edits, and each
such question holds the option that keeps the text as written.

**Q10. Which command does `plugin enable` print when the endpoint is missing from the
table?** `08:398`, `07:1870` and `02:326` give one "exact command" for both halves, the
missing endpoint and the instance that does not answer, and plugin record §11 writes the
sentence after the commands that start an instance per target. ADR-063 names `aiopsctl
plugin start <id>` for the instance that did not answer (`ADR-063:415-416`) and wants
"the exact command" for the missing endpoint without naming it (`ADR-063:753-755`).
Install writes the endpoint into the table (`ADR-063:630-631`; ADR-063 decision 39), so
at a site whose install completed the line is there before `plugin start` runs (step C2,
`ADR-063:678-682`), and a missing line is a table that was edited or damaged. That holds
on both targets, since install writes the line whichever unit the site runs.
- (a) `aiopsctl plugin start <id>`, one command for both halves, as the three lines word
  it. `plugin start` then makes that command true: in its write phase, under the lock
  and on what it reads there, it writes the line that install writes for an id whose
  unit the home holds and whose line the table lacks (decision 45). It submits nothing.
  - How the running process learns the line. The operator runs `plugin enable` next,
    and that command submits a generation. The line is read then only if the provider
    `sidecars-static` reads the table at every generation or at every lookup, and not
    once at boot. The exit needs the same of it whatever this answer: the kernel that
    serves step C4 "is the process that booted before step C2", and install writes the
    line in step C2 (`ADR-063:676-678`). No accepted text says when the provider reads:
    the definition `sidecars` moves to S2b with no ADR named for it (ADR-063 decision
    39). With (a) the owner also says which text carries that rule: the S2b part of
    ADR-047, which holds the table, or a part of its own.
  - What it depends on. The S2b part of ADR-047 fixes the form of the line and names
    the writers of the table; as first proposed it says that `plugin start` writes no
    file of the home, and (a) has it corrected (decision 59).
  - What it costs. The table has two writers. `Start` takes the write lock in any
    case, for the credential, if ADR-041 is accepted as proposed (decision 45).
  - On a site that runs the systemd unit it is the same command, with Q14 (a). With
    Q14 (b) or (c), `plugin start` does not run there, and the missing line has no
    command that writes it again on that target.
- (b) The command that takes the artifact out, printed exactly, `aiopsctl plugin remove
  <id> --profile <p>`, and the words "then install it again": the home does not keep the
  spec that an artifact was installed from (ADR-047 decision 5). Install stays the one
  writer of the table. `08:398`, `07:1870`, `02:326` and plugin record §11 gain the
  clause that the first half names another command than the one that starts the
  instance. Where another profile still names the id, `remove` keeps the rendered files
  (ADR-063 decision 68), so the install that follows has to write a missing line again,
  which the S2b part of ADR-047 would then have to say. It is the same on both targets.
- Recommended: (a). It keeps the three lines as written, and one command repairs the
  home whichever half refused. It rests on the S2b part of ADR-047 and on a provider
  that reads the table after boot, which the exit needs in any case. The dial stays
  authenticated against the pinned certificate whatever the table holds (ADR-063
  decision 55), so a line that names another endpoint opens no stream. (b) is less code
  and changes what four written lines mean. Blocks: `Start`, the refusal of `Enable` and
  its case (`ADR-063:753-755`); needed before group 3 of ADR-063 decision 30.
- **Answered 2026-11-27: (a)**, decision 60.

**Q11. How does the exact command reach the output when the instance did not answer?**
`aiopsctl` prints "the row, the reason the server gave and, where the instance did not
answer, the exact command" (`ADR-063:414-416`), and for a denied handshake it prints the
denial (step D2, `ADR-063:706-713`). It makes no dial (`ADR-063:406-407`), and the
result carries per row a text and no cause (`sdk/def/reload/v0/wire.go:194-199`). The
causes of the wire are a closed list on the error of a result, on which "A client
decides on `status` and `causes` and prints `text`" (ADR-062 decision 7). The peer of
the proxy is third-party code (`ADR-063:813-816`).
- (a) The server's reason holds it. Where the dial of a proxy got no answer, the reason
  with which its row fails ends with the words `the instance of <id> did not answer: run
  "aiopsctl plugin start <id>"`. `aiopsctl` prints the reason whole and decides nothing
  on it. The same words are in the log of the server process, at boot too, where an
  optional row waits for its instance (`08:477`). The proxy, a first-party row, then
  names a command of `aiopsctl`, as a server that finds a snapshot names `aiopsctl
  profile sanitize <profile>` (decision 17). Because the text is printed as an
  instruction, the rule of decision 52 binds the proxy: nothing that a peer supplied
  enters a reason but numbers, a fingerprint that the kernel computed and a value of a
  closed list, and `aiopsctl` quotes a control character in it. A server process
  cannot see which target a site runs, so (a) fits Q14 (a), where the command is the
  same on every target, and does not fit Q14 (b).
- (b) A cause per row. Each row of the report gains a closed list `causes`, with
  `remote-unreachable`, `remote-identity` and `remote-denied` at S2b, and `aiopsctl`
  composes the line `start the instance: aiopsctl plugin start <id>` for the first. No
  text of the server is read as an instruction, and `aiopsctl` can name the command of
  the target. A field is additive inside version 1 of the wire (ADR-062 decision 6).
  `composition`, whose row report the wire renders, and `reload` each gain a minor with
  a raised suite revision, the proxy's failure has to reach the judge as an error of a
  definition that both import, and ADR-062 decision 7 gains a dated note.
- (c) `aiopsctl` adds the line for every row of a remote plugin that a failed generation
  reports, worded "if its instance is not running". No wire changes and no server text
  is fixed. It departs from `ADR-063:415-416`, "where the instance did not answer": the
  output of step D2 would hold the command although the instance runs.
- Recommended: (a), with the rule of decision 52. It changes neither the wire nor a
  definition, `aiopsctl` matches no text, and an operator who reads only the server log
  finds the same command. Its risk, a peer that puts the words of a command into a
  reason, is closed by the rule and held by a test. (b) is the form to take if the
  owner answers Q14 (b), or once a second client has to decide on the cause. Blocks:
  the failure text of the proxy (group 2 of ADR-063 decision 30), `Enable` and step C2;
  needed before the proxy merges.
- **Answered 2026-11-27: (a)**, with the rule of decision 52, decision 61.

**Q12. How does `plugin check` exit when step 7 did not run for want of a container
runtime?** The check "exits non-zero on the first failing step", and step 7 "needs one
when it packs an image" (`07:1570`). The check is "The one command before a pull
request" and needs "A toolchain for code; PostgreSQL only for a plugin with its own
schema" (`07:1536`); smoke "is not part of the check, because it needs a container
runtime" (`07:1549`); and a Python author needs no container runtime for T0 to T2
(`07:51`, `:1236`; `ADR-063:436-439`). ADR-063 has the step reported as not run and
gives no exit code (`ADR-063:382-384`), and step P2 reads the `--json` report
(`ADR-063:660-663`). Under every option the gate reads the manifest and a denial fails
the step (decision 53), so what goes unevaluated on such a machine is the pack, the
signature, the attestation and the clauses of the gate that read them. `make
plugin-check` is the last step of `make check` (`Makefile:251`; decision 23) and runs
`plugin check` on the remote examples from S2b (`ADR-063:783-786`), and the owner chose
process stubs for the failure matrix because it "keeps running wherever `make check`
runs" (`ADR-063:1640-1641`).
- (a) Exit 0, and `make check` needs no container runtime. A step that did not run is
  not a failed step. The report says `not run` and holds no attestation, and `plugin
  pack` refuses on that machine. `make plugin-check` on a machine with no container
  runtime passes and prints each step that did not run. The CI jobs that run `plugin
  check` on a remote plugin, the step of the `test` job that runs `make plugin-check`
  among them, set a switch of the test that runs the check, under which a step named
  in `NotRun` of the report fails the test. Each of those jobs then holds a container
  runtime, and step 7 is proven on every commit.
- (b) As (a) for the command, and `make plugin-check` itself fails on a `not run` step.
  Every machine that runs `make check` then needs a container runtime from S2b, which
  the owner's reason for ADR-063 decision 64 did not want of the matrix.
- (c) Exit 1. Every step passes or does not apply, or the check fails. A remote author
  with no container runtime then never gets exit 0 from the one command, and reads the
  report to see that step 7 alone is open. `07:1536` gains the container runtime among
  what the check needs for remote code.
- (d) As (a), with a flag of `plugin check` under which a `not run` step exits 1, for a
  job that must not pass without step 7. The CLI spec gains the flag.
- Recommended: (a). It is what `07:1536` and `07:1549` say the check needs, a `not run`
  step can publish nothing, the manifest clauses of the gate are evaluated without a
  runtime, and the one place that must not pass without step 7, the CI of this
  repository, reads the report that the check already writes. (d) adds a flag for what
  the report shows. Blocks: `Check` on remote code and `make plugin-check`; step P2 of
  the exit reads the report under every option.
- **Answered 2026-11-27: (a)**, decision 62.

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
   a throwaway home with a throwaway key, in the order of decision 55: pack, sign,
   trust, install and enable with no process running, start the instance, boot the
   released image on that home, and start again, which attaches the kernel container.
   No second way to run an instance exists beside the unit.
- (a) Accept the four readings, each landing with the code that builds it; `07:1583`
  gains them.
- (b) Answer each item on its own.
- Recommended: (a). Blocks: `Smoke` of remote code and its cases (`ADR-063:758-760`).
- **Answered 2026-11-27: (a)**, all four readings, decision 63.

**Q14. What does `aiopsctl` do on a site that runs the systemd unit?** `08:1293` gives
the two targets two start commands, "started with `aiopsctl plugin start <id>`" and "a
unit and `systemctl enable --now`", and plugin record §11 lists them and then says that
`plugin enable` prints "the exact command". `08:398`, `07:1870` and `02:326` have one
exact command, and `ADR-063:415-416` names `aiopsctl plugin start <id>`. `08:1293` and
`07:1195` have "`aiopsctl plugin disable` run on the host" stop the unit and name no
target, and the case of `ADR-063:739-740` wants the unit stopped after `plugin disable`
and after `plugin remove`. ADR-063 decision 50 builds both targets and runs both. A
credential that was revoked is made again by `plugin start` alone (decision 45). Install
renders both unit files for every id, so no file of the home says which one a site runs.
- (a) One command of `aiopsctl` on every target, and the target is found on the host
  (decision 48). The first start of a systemd unit is the operator's `systemctl enable
  --now`, as written. From then `aiopsctl plugin start <id>` finds the unit in the
  service manager, runs no fragment, makes a credential where the pin is absent, starts
  the unit where it is not active and attaches the kernel container; and `plugin
  disable` and `plugin remove` stop it through the service manager. The command that
  `plugin enable` prints is then true on both targets. `aiopsctl` runs `systemctl` and
  never enables or disables a unit. Where the service manager refuses the site user,
  the command exits 1 and prints what to run. `08:1293`, `02:313` and plugin record §11
  gain one sentence: on a site that runs the systemd unit, `aiopsctl plugin start <id>`
  starts the unit that the operator enabled. `08:398`, `07:1870` and `02:326` stand.
- (b) The printed command is the target's. `plugin start` is the compose target's and
  refuses where the service manager holds the unit, naming `systemctl start` with the
  unit. `aiopsctl` composes the command that `plugin enable` prints from the target it
  finds, which for the half that the server decides needs the cause of Q11 (b).
  `plugin disable` and `remove` stop through the service manager as in (a). No command
  then makes a credential again on a systemd site, and ADR-041 has to name one.
  `08:398`, `07:1870`, `02:326` and plugin record §11 say "the command of the unit's
  target".
- (c) `aiopsctl` handles the compose target alone at S2b. On a systemd site `plugin
  enable` prints a command that refuses there, `plugin disable` and `remove` revoke and
  stop nothing, and no command makes a credential again. This weakens `08:1293` and
  `07:1195`, which then say "on the compose target" for the stop, and `08:398`,
  `07:1870`, `02:326` and plugin record §11, which say it for the command; the case of
  `ADR-063:739-740` holds on the compose target alone. It needs the owner's word as a
  deferral of written text (ADR-063 decision 33).
- Recommended: (a). No written line is weakened, one printed command is exact on both
  targets, the stop and the new credential work on both, and the server needs to know
  nothing of the target. Its cost is that `aiopsctl` runs a second program of the host,
  with behaviours that the systemd job of ADR-063 decision 50 has to prove (decision
  48). Blocks: `Unit`, `Start`, the stop of decision 49 and that job; needed before
  group 3 of ADR-063 decision 30.
- **Answered 2026-11-27: (a)**, decision 64.

**Q15. How does `plugin verify` reach the container that it starts?** ADR-063 has a
server dial only over mutual TLS, and its one exception is "The Unix socket of `plugin
check` (reading 2 of question Q17)", "opened by the check runner alone, to a process
that it started" (`ADR-063:512-515`; ADR-063 decision 54, reading 2). `plugin verify`
runs "the suite against a pulled image" (plugin record §11): an image of a third party,
on the host of whoever runs the command. A unit gives an instance no route out, a user
that is neither the site user nor root, and no path of the home but its own credential
directory, read-only (ADR-063 decisions 24 and 25). No line says how the runner reaches
the container, and the parts that were proposed with this one each leave it to another
(decision 59).
- (a) As a unit, over mutual TLS. `plugin verify` makes a throwaway home, renders the
  unit of the artifact into it with the renderer of install, makes a throwaway key pair
  for each side as install makes them, and starts the unit on an internal network of
  its own. The binary that runs the suites is the kernel side: it dials the address of
  the container on that network with the throwaway kernel pair. The container gets
  exactly what a unit gives an instance, the image is driven through the path that a
  site drives it through, and ADR-063 decision 23 keeps its one exception as written.
  It rests on one behaviour that was read in no source: that a process on the host
  reaches an address on an internal network of the container runtime. The case of
  `ADR-063:756-758` proves it on the runtime of the exit; if that runtime does not give
  it, the work stops on that point and the owner is asked again (ADR-063 decision 33),
  with (b) as the other way. ADR-041 gains the dial of the check runner with a
  throwaway pair (decision 59).
- (b) Over a Unix socket, as `plugin check`. The container gets no network at all and
  one bound directory, made for the run, in which it opens the socket; the runner
  dials it with no TLS. No key material is made. The third point of ADR-063 decision
  23 and reading 2 of its decision 54 then name the socket of `plugin verify`, to a
  container that the runner started, by a dated note. The socket is switched on by the
  listen address that the runner gives the container, which the fixed template of a
  unit never gives (ADR-063 decision 25), so no unit of a site reaches that mode. The
  container gets a writable bind, which a unit never gives, and the user of the
  container has to open a socket in a directory that the invoking user can reach,
  which was read in no source either.
- Recommended: (a), on the condition that it names. It keeps the written exception as
  narrow as the owner accepted it and gives the pulled image no more than a site gives
  it. (b) is the tighter fence on the network and extends an exception to the
  authenticated dial onto an operator's host. The owner decides, because (b) edits two
  accepted decisions of ADR-063. Blocks: `Verify` of remote code and its cases
  (`ADR-063:756-758`).
- **Answered 2026-11-27: (a)**, on the condition that it names, decision 65.

**Q16. One reading: `plugin enable` of a remote row while no process runs the
profile.** `08:398`, `07:1870` and `02:326` say with no condition that `plugin enable`
refuses "while the endpoint ... does not answer the handshake". ADR-063 decision 17 has
`aiopsctl` submit "as for any row" and never dial, and reading 4 of its decision 54
makes the refusal for an instance that does not answer "the server's failed verdict".
With no process on the profile there is no verdict, and decision 17 answers
`not-running` and keeps the files for any row. Neither text states this case.
- (a) Reading: the half "does not answer the handshake" is the server's and holds while
  a process runs the profile. With none, `plugin enable` of a remote row whose endpoint
  and pin the home holds (refusals 1 and 2 of decision 52 stand) keeps the files, exits
  0 and answers `not-running`, with one more line on stderr: `the instance of <id> is
  not checked until a process runs <p>: start it with "aiopsctl plugin start <id>"`. At
  the next start the row follows the matrix row "Remote unreachable or handshake
  mismatch" (`08:477`; ADR-063 decision 66). `08:398`, `07:1870`, `02:326` and plugin
  record §11 gain "while a process runs the profile" for that half.
- (b) `plugin enable` of a remote row refuses, exits 1 and writes nothing while no
  process runs the profile, since nothing can show that the instance answers. A remote
  row is then enabled on a running kernel only. Smoke would enable its row through the
  reload socket, which its profile disables today (ADR-062 decision 27), so that
  decision gains a dated note.
- (c) `aiopsctl` asks the container runtime whether the unit runs and refuses where it
  does not, with the start command, and makes no dial. `Enable` then gains the toolbox
  part `Unit` and needs a container runtime on every host where a remote row is
  enabled, and it shows that a container runs, not that it answers the handshake.
- Recommended: (a). It is what the split of ADR-063 decision 17 gives, it keeps
  `aiopsctl` from any dial and `Enable` from any runtime, and the row is still never
  ACTIVE without a handshake. Blocks: `Enable` of a remote row with no process, and the
  order of smoke (decision 55).
- **Answered 2026-11-27: (a)**, decision 66.

### Owner answers of 2026-11-27

The project owner accepted the S2b part of this ADR on 2026-11-27, with decisions 45 to 59
and the edits that "Edits on acceptance of the S2b part" makes whatever the answers, and
answered questions Q10 to Q16 on the same date, each with its recommended option, which is
(a) in every question. Each decision below names the question it answers. Where a decision
of the part leaves a point to a question, the decision here settles it. The owner answered
the questions of ADR-041 and of the S2b parts of ADR-020, ADR-044, ADR-047 and ADR-048 on
the same date, and each is recorded in its own ADR.

60. **`plugin enable` prints `aiopsctl plugin start <id>` when the endpoint is missing
    from the table** (Q10 (a)): one command for both halves, the missing endpoint and the
    instance that does not answer, as `08:398`, `07:1870` and `02:326` word it, and those
    three lines stand. `plugin start` makes the command true: in its write phase, under
    the write lock and on what it reads there, it writes the line that install writes, for
    an id whose unit the home holds and whose line the endpoint table lacks, and it
    submits nothing (decision 45). The text of refusal 1 is the one that decision 52 gives
    for this answer. `plugin start` is therefore a writer of the endpoint table beside
    `plugin install`, and the row of the table in ADR-047 decision 28 says so. The running
    process learns the line at the generation that the next `plugin enable` submits, so
    the provider `sidecars-static` reads the table at every generation or at every lookup,
    and not once at boot, which the exit needs of it whatever this answer
    (`ADR-063:676-678`). On a site that runs the systemd unit the command is the same
    (decision 64). One point of the question stays open. With (a) the owner was also to
    say which text carries the rule on when the provider reads the table, the S2b part of
    ADR-047 or a part of its own, and the answer of 2026-11-27 names none. ADR-047
    decision 29 leaves when the row reads the file again to the row, with the definition
    `sidecars` (ADR-063 decision 39). The owner is asked again before `sidecars-static`
    merges. Until a text carries the rule, it stands here as what this answer requires of
    that provider.

61. **The server's reason holds the exact command** (Q11 (a), with the rule on what a peer
    supplies). Where the dial of a proxy got no answer, the reason with which its row
    fails ends with the words `the instance of <id> did not answer: run "aiopsctl plugin
    start <id>"`. `aiopsctl` prints the reason whole and decides nothing on it, and the
    same words are in the log of the server process, at boot too, where an optional row
    waits for its instance (`08:477`). Decision 52 fixes the words after `fail:` for its
    rows 3 and 4. Because the text is printed as an instruction, the rule of decision 52
    binds the proxy: the words `run "aiopsctl` are written by first-party code alone, as
    the last clause of a reason; a reason holds nothing that the peer supplied but
    numbers, a fingerprint that the kernel computed and a value from a closed list of
    ADR-041; a free text of the peer, as `Hello.diagnostics`, is logged and enters no
    reason; `aiopsctl` prints the text of a row through the function that quotes a control
    character; and the peer case of decision 58 holds the rule. The wire of ADR-062 and
    the definitions `composition` and `reload` do not change, and ADR-062 decision 7 gains
    no note.

62. **`plugin check` exits 0 when step 7 did not run for want of a container runtime and
    no step failed, and `make check` needs no container runtime** (Q12 (a)). A step that
    did not run is not a failed step. The report says `not run` and holds no attestation,
    and `plugin pack` refuses on that machine (decisions 47 and 53). The gate still reads
    the manifest, and a clause that denies fails step 7 with exit 1 (decision 53). `make
    plugin-check` on a machine with no container runtime passes and prints each step that
    did not run. The CI jobs that run `plugin check` on a remote plugin, the step of the
    `test` job that runs `make plugin-check` among them, set a switch of the test that
    runs the check, under which a step named in `NotRun` of the report fails the test.
    Each of those jobs then holds a container runtime, and step 7 is proven on every
    commit. `07:1570` gains the outcome and the exit. `plugin check` gains no flag, and
    `07:1536` is not edited.

63. **The four readings of `07:1583` for `remote` are accepted** (Q13 (a)), each landing
    with the code that builds it, and decision 55 holds with them. (1) The released image
    is the subject of the release artifact that `--against` names by digest; smoke pulls
    it by that digest and builds no distribution for `remote`. (2) For `remote` the
    test-only profile is the release's `aiops-core` and `aiops-selfhost` bundles, the two
    that `dev` lists, plus the plugin's bundle with its row enabled, and the pass rule of
    ADR-060 decision 58, reading 1, stands. (3) The module source is what the build of
    in-process code needs: `--modcache` is required where the manifest lists `inproc` and
    is not read for a manifest that lists `remote` alone. (4) Smoke attaches the image as
    an operator does (`08:398`), on a throwaway home with a throwaway key, in the order of
    decision 55: pack, sign, trust, install and enable with no process running, start the
    instance, boot the released image on that home, and start again, which attaches the
    kernel container. No second way to run an instance exists beside the unit. `07:1583`
    gains the four.

64. **One command of `aiopsctl` on every target, and the target is found on the host**
    (Q14 (a)), as decision 48 states it. The first start of a systemd unit is the
    operator's `systemctl enable --now`, as `08:1293` writes it. From then `aiopsctl
    plugin start <id>` finds the unit in the service manager, runs no fragment, makes a
    credential where the pin is absent, starts the unit where it is not active and
    attaches the kernel container, and `plugin disable` and `plugin remove` stop the unit
    through the service manager. The command that `plugin enable` prints is then true on
    both targets. `aiopsctl` runs `systemctl` and never enables or disables a unit. Where
    the service manager refuses the site user, the command exits 1 and prints what to run.
    `08:1293`, `02:313` and plugin record §11 gain one sentence: on a site that runs the
    systemd unit, `aiopsctl plugin start <id>` starts the unit that the operator enabled.
    `08:398`, `07:1870` and `02:326` stand, and `07:1195` is not weakened. The program
    `systemctl` waits for its reading (decision 57), and the systemd job of ADR-063
    decision 50 proves the assumptions of decision 48.

65. **`plugin verify` reaches the container that it starts as a unit, over mutual TLS**
    (Q15 (a), on the condition that the option names). `plugin verify` makes a throwaway
    home, renders the unit of the artifact into it with the renderer of install, makes a
    throwaway key pair for each side as install makes them, and starts the unit on an
    internal network of its own. The binary that runs the suites is the kernel side: it
    dials the address of the container on that network with the throwaway kernel pair. The
    container gets exactly what a unit gives an instance. ADR-063 decision 23 keeps its
    one exception as written, so ADR-063 decisions 23 and 54 gain no note. The condition:
    the answer rests on one behaviour that was read in no source, that a process on the
    host reaches an address on an internal network of the container runtime. The case of
    `ADR-063:756-758` proves it on the runtime of the exit. If that runtime does not give
    it, the work stops on that point and the owner is asked again (ADR-063 decision 33),
    with option (b), the Unix socket, as the other way. The option has ADR-041 gain the
    dial of the check runner with a throwaway pair (decision 59). That edit is not made
    with this record. Two texts that were proposed beside this part, and whose questions
    the owner answered on the same date, give `plugin verify` the other channel: ADR-041
    decision 27 and ADR-044 decision 38 have the runner reach the container over the Unix
    socket of `plugin check`, with no network in the container, which is option (b) of
    this question, and neither decision was put to the owner as a question. ADR-020
    decision 34 admits either channel. This answer is recorded as the owner gave it, the
    difference is put to the owner with this record, and `Verify` of remote code does not
    merge before the owner's word on it is recorded in the ADRs that it changes.

66. **`plugin enable` of a remote row keeps its files and answers `not-running` while no
    process runs the profile** (Q16 (a)). The half "does not answer the handshake" of
    `08:398`, `07:1870` and `02:326` is the server's and holds while a process runs the
    profile. With none, `plugin enable` of a remote row whose endpoint and pin the home
    holds keeps the files, exits 0 and answers `not-running`, with one more line on
    stderr: `the instance of <id> is not checked until a process runs <p>: start it with
    "aiopsctl plugin start <id>"`. Refusals 1 and 2 of decision 52 stand with no process
    too. At the next start the row follows the matrix row "Remote unreachable or handshake
    mismatch" (`08:477`; ADR-063 decision 66). `08:398`, `07:1870`, `02:326` and plugin
    record §11 gain "while a process runs the profile" for that half. `Enable` gains no
    toolbox part and makes no dial, and ADR-062 decision 27 gains no note.

**Edits made with this record.** `00` is edited in the commit that records the answers
(`08:999`), and that commit makes the edits that "Edits on acceptance of the S2b part"
names for the options chosen:

- this ADR: the two Status lines, with a dated note at the acceptance order of the part;
  in decision 16, the method `Start`, the types and fields of decision 46 and the rows of
  its method table; the dated notes at decisions 17 and 19; in decision 21, the parts
  `Unit`, `Image` and `Boot`, the two forms, the error `ErrNoRuntime` and the fields of
  decision 47; in decision 30, `plugin start` in the row of `command-plugin` and the CLI
  spec rows of `plugin start`, `plugin new` and `plugin smoke`; and the dated note at the
  item of `plugin start` in decision 32;
- `00:137`: `start` among the `plugin` commands of P0;
- `07:1556`: the registries that have a remote form and the default runtime of each
  language (decision 50); `07:1566`: the two remote modules of the remote Go form
  (decision 51); `07:1570`: the outcome `not run` of step 7, the gate that still reads the
  manifest, no attestation, and the exit of decision 62; `07:1583`: the four readings of
  decision 63;
- `08:1293`, `02:313` and plugin record §11: the sentence of decision 64;
- `08:398`, `07:1870`, `02:326` and plugin record §11: "while a process runs the profile"
  for the half that the server decides (decision 66);
- ADR-047 decision 28: the row of the endpoint table names `plugin start` as the writer of
  a missing line (decision 60).

Lines that restate edited text are edited with them: plugin record §14, which restates
what smoke needs from `07:1583` (decision 63).

Not edited: `02:95` names the `plugin` commands by their range, and `02:206` and `08:430`
list a part of them, so none restates the list of `00:137`. `07:1536`, `07:1549` and
`07:1195` stand as written (decisions 62 and 64). ADR-062 and ADR-063 are not edited: no
chosen option names a line of them. The licence record, the community record and the
upstream register are not edited: decision 57 enters each item in the commit that first
uses it. The round records under `docs/design/community-r4/` stand as text of their date.
An option that the owner did not choose edits nothing.

Left to later commits, each named:

- the ADR table of `08` §3, with the commit that updates that table for the parts accepted
  on 2026-11-27;
- the dial of `plugin verify` in ADR-041 decision 27, in the test case and the row of
  ADR-020 that restate it there, and in ADR-044 decision 38, with the commit that records
  the owner's word on the difference of decision 65;
- the text that carries the rule on when `sidecars-static` reads the endpoint table, with
  the commit that records the owner's word on the open point of decision 60;
- the card of the figure `07-install-distribution`, whose source
  `docs/architecture/diagrams/src/07-install-distribution.json` restates `08:398` without
  the condition of decision 66, with the commit that next renders that figure;
- the rows of decision 59 marked "open", and what the answers to the questions of ADR-041
  and of the S2b parts of ADR-020, ADR-044, ADR-047 and ADR-048 change in what decision 59
  assumes, each by a dated note here before the code that the point governs merges, as the
  head of this part says;
- the minors of `operations` and `launch` with their suite revisions and the regenerated
  `requires`, and the help goldens, with the code of group 3 of ADR-063 decision 30
  (decisions 46 and 47);
- the licence-record rows and the register entries of decision 57, each in the commit that
  first uses its item.

### Points for the owner after the records of 2026-11-27

Note of 2026-11-27. The owner's answers on ADR-041 and on the S2b parts of ADR-020,
ADR-034, ADR-044, ADR-047, ADR-048 and ADR-054 were recorded on the same date as the
answers above, and the runs of ADR-041 decision 38 and of ADR-047 decision 39 were
recorded after them. This section holds the two points of this part that wait for the
owner, as questions Q17 and Q18, and what those records ask of this part. The part
stays accepted. `Verify` of remote code waits for Q17, and `sidecars-static` for Q18.

**Q17. Which channel does `plugin verify` use?** This is the difference that decision
65 puts to the owner. Decision 65 (Q15 (a)) has `plugin verify` reach the container as
a unit, over mutual TLS with throwaway pairs. ADR-041 decision 27 and ADR-044 decision
38 have the runner reach it over the Unix socket of `plugin check`, with no network in
the container, which is option (b) of Q15; neither was put to the owner as a question.
ADR-020 decision 34 admits either channel.
- (a) Decision 65 holds. The row of `plugin verify` in ADR-041 decision 27, ADR-044
  decision 38, where the binary "gains no second dial", and the lines of ADR-020 that
  restate them are revised to it, and ADR-041 gains the dial of the check runner with a
  throwaway pair. The condition of the option is run before `Verify` of remote code is
  written. Row 4 of ADR-047 decision 39, run on 2026-11-27, bears on it: on the network
  of ADR-047 decision 32, in the gateway mode `isolated`, the host holds no IPv4
  address in the network. Whether a process on the host then reaches the container was
  not run. Where it does not, the runner dials from a container that joins the network,
  as the kernel container does, or (b) is the other way, as decision 65 says.
- (b) The Unix socket. ADR-041 decision 27 and ADR-044 decision 38 hold as written,
  decision 65 is reversed, and ADR-063 decisions 23 and 54 gain the dated note that Q15
  (b) names. The container has no network and gets one writable bind.
- Recommended: (a), with its condition run first. It is the answer that the owner gave
  to the one question that set both channels out, and (b) edits two accepted decisions
  of ADR-063. The run of the condition is small and decides whether (a) can be built as
  worded. Blocks: `Verify` of remote code and its cases (`ADR-063:756-758`).

**Q18. Which text carries the rule on when `sidecars-static` reads the endpoint
table?** This is the open point of decision 60: the provider reads the table at every
generation or at every lookup, and not once at boot, and the answer of 2026-11-27 names
no text for that rule.
- (a) The S2b part of ADR-047, in its decision 29, which defines the file and now
  leaves "when it reads it again" to the row. That part is still proposed, and its
  revision takes the rule with the attempt value that ADR-041 decision 42 gives a line
  of the table.
- (b) A part of its own, for the definition `sidecars` and its provider (ADR-063
  decision 39).
- Recommended: (a). The rule is about one file, the part that defines the file is open
  for revision now, and no document is added. Blocks: `sidecars-static`.

**What the other records ask of this part,** each by an addition to this ADR or a dated
note here before the code that the point governs merges, and none a question:
- **`plugin smoke` of remote code needs a repository.** The owner's answer to question
  Q8 of the S2b part of ADR-047 (its decision 42) has `plugin install` and `plugin
  verify` of remote code refuse a local-path spec, and has smoke push its throwaway
  artifact to a repository that the author names, "in a form that the S2b part of
  ADR-061 fixes". Decision 55 names no repository and no push. The form is an addition,
  due before `Smoke` of remote code merges. The row of decision 59 on what `plugin
  verify` does with a local-path spec is settled by the same answer: it refuses, naming
  `plugin push`.
- **The command that renews the kernel's pair** (ADR-041 decision 43, its question Q4
  (b)) has no form in this part. The form is an addition, due before group 3 of ADR-063
  decision 30.
- **`plugin enable` writes the attempt value** of the row's line of the endpoint table
  (ADR-041 decisions 18 and 42). Decision 60 names `plugin install` and `plugin start`
  as the writers of that table, and `Enable` joins them with the revision of ADR-047
  decision 29.
- **`plugin start`, decision 45,** follows the answers to questions Q16, Q17 and Q18 of
  the S2b part of ADR-047, which come from the runs of its decision 39: when a network
  that already exists is checked, what follows the first attach of the kernel
  container, and whether a stopped container is started again or replaced.
