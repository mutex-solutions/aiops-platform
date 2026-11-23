# ADR-048: Manifest schema 1.0 (the S2a part)

- Status: **proposed 2026-11-23, awaiting the project owner.** Nothing in this ADR is in
  force. It changes no line of `00-decisions.md` by itself: where an answer would change
  one, the question names the line, and `00` is edited in the commit that records the
  answer (`08:996`).
- Scope: the S2a part of ADR-048 (`08:1179`; `00:782`), manifest schema 1.0, which S2a
  delivers (`08:694`). The S6a part of the same ADR, index format 1.0 with the trust roots
  for indexes, `renamedTo`, the `compat` statuses and the re-certification job and its
  trigger (`08:1179`), is not proposed here and stays open. This ADR is due at S2a under
  ADR-060 decision 2 and settles what ADR-060 decision 33 lists for it.
- Citations: `00`, `07` and `08` are the files of `docs/architecture/` with those numbers,
  and `07:120` is line 120 of `07` at commit `8e1f7b9`. "community", "harness" and
  "licence" are `docs/design/community-first-2026-11.md`, `harness-and-upstreams-2026-11.md`
  and `licence-audit-2026-11.md`, cited the same way. The plugin record,
  `docs/design/plugin-system-2026-11.md`, is cited by section. ADR-060 cites lines at
  `491a518`; between that commit and `8e1f7b9` the lines of `08` from 1188 on moved down
  by one, and no other cited line moved. Code is cited by file at `8e1f7b9`.

## Context

The manifest holds what the launcher, the index and the gate read without running code
(`07:79`; `00:207`). `00:207`, `07:118-139`, community:167-180 and plugin record §4 name
its fields and the half each belongs to. The YAML of `07:81-116`, `07:145-163` and
`07:1662-1687` is a plausible layout only: the inner shape of `uses`, `registers`, `hooks`,
`requires` and `integrates`, the `license` entry of `integrates` included, is fixed with
schema 1.0 at S2a by this ADR (`07:66` R8). ADR-060 decision 33 asks it to settle every
field of schema 1.0, those five inner shapes, where `registers` comes from (ADR-060
decision 11), whether `scope` is a list or one value (`07:94`, `:128`), the reserved
owners, and the strict reader of `plugin check` beside the lenient reader of the gate
(`07:120`).

Reading the sources against each other and against the code at `8e1f7b9` shows:

- `registers` is generated (`00:207`; `07:135`), but the `Spec` of `07:215-223` has no
  field it could come from, and a `Need` on a registry key reads the same for a consumer
  and a contributor (ADR-060 decision 11). The gate reads it from S2a: check 10 asks a
  suite revision for each definition a plugin provides or contributes to (`08:506`;
  ADR-060 decision 56). A Python author declares it with `@contributes(definition)`
  (`07:1201`).
- `scope` is "`process`, `tenant` or both" (`00:207`; `07:128`; plugin record §4).
  `07:94`, `07:153`, `07:1673` and the five S1 manifests write a list; the prose writes one
  value (`07:477-478`, "`scope: process` (default)"; `08:504`). Nothing says what "both"
  does to a row.
- `aiops` and `contrib` are reserved to artifacts built by the release pipeline (`07:121`;
  `00:657`; community:171), and step 1 of `plugin check` fails on "a reserved owner
  outside the release pipeline" (`07:1567`). CI runs `plugin check` on every plugin under
  `plugins/`, whose owner is `aiops` (ADR-060 decision 32), and a contributor runs it on a
  plugin that `plugin new --contrib` wrote with the owner `contrib` (`08:407`; `07:1526`).
  Nothing says how the check tells them from a third party.
- A reader ignores unknown fields of a higher minor, and `plugin check` is strict
  (`07:120`; community:149; `07:1567`). Neither says what the gate does with an unknown
  field at a minor it knows, and no clause of check 1 denies a manifest whose known fields
  are absent or malformed (`08:497`).

What boot reads at `8e1f7b9` is not schema 1.0:

| What boot reads | From | In schema 1.0 |
|---|---|---|
| `ModuleEntry.ID` | `kernel.Plugin.ID` (`dist/dist.go`), or the key of `modules.yaml` | `id` |
| `Module`, `Version`, `Sum` | The constants of `dist/dist.go`, or `modules.yaml` | Not manifest fields: the module table of the signed distribution manifest (ADR-060 decisions 19 and 20) |
| `Provides` | The key names of `kernel.Plugin.Provides` | `provides`, with the major of each key |
| `Needs`, `Wants` | `kernel.Plugin.Inject`, split by `Optional()` | `uses`, with the major and `as` |
| `Hooks` | `kernel.Plugin.Hooks` with the stage name and ordinal 0; `modules.yaml` with an ordinal and a name | `hooks`, with the stage by name; the ordinal from served definition data |
| `Volatile`, dotted paths under `config.` | `dist.Module.Volatile` by hand, or `modules.yaml` | Marks in `configSchema`; a path is a list of keys |
| `Classes` | `modules.yaml` only | Not manifest data: served definition data (ADR-060 decisions 14 and 15) |
| `DistManifest` | `dist/dist.yaml` | Not a plugin manifest: the signed distribution manifest (ADR-060 decision 20) |

No Go code reads `plugin.yaml` at `8e1f7b9`. The five manifests under `plugins/` are
hand-written in S1 fields: `needs` where 1.0 has `uses`; `provides` and `hooks` written by
hand; no `registers`, `configSchema`, `durable` or `requires`; a header that says the
generated half comes with `plugingen`. Their `hooks` give the stage by name
(`stage: default`). The S1 reader of `modules.yaml` refuses one hook listed twice
(`boot/dist.go`), while the kernel accepts one hook at two stages and refuses only a
repeated pair (`checkUses`, `sdk/kernel/hooks.go`). The layer parser reads `${` in a value
as a closed reference (`boot/parse.go`).

## Decision

### The file and its versions

1. **One file, `plugin.yaml`, one YAML document**, in every plugin directory and every
   artifact (`07:79`; `08:391`). It holds both halves. The halves are told apart by field
   name, never by a comment: the seven generated fields are `uses`, `provides`,
   `registers`, `hooks`, `configSchema`, `durable` and `requires` (`00:207`;
   `07:133-139`), and every other field is written by hand. A generator rewrites only the
   seven, after the hand-written fields and in that order, and keeps every other line and
   comment (ADR-060 question Q5 and decision 41). The generators are `plugingen` for Go,
   `plugin check --write` for a pack with no code and, from S2b, `aiops-sdk manifest` for
   Python (`07:79`). Parity compares the seven fields as read, not the text (`07:1568`;
   `08:1258`), so a comment or a hand-written field never fails it. The manifest digest of
   `plugins.lock`, of the handshake and of the index (`07:1121`, `:1905`) names this one
   file; where the file sits in an artifact is ADR-047's (ADR-060 decision 33).
2. **`manifestVersion` is the string `"1.0"`** (`07:84`, `:120`), written
   `"<major>.<minor>"` and quoted, so YAML never reads it as a number.
   - The schema is frozen-additive from the release that first ships it (`07:120`;
     `00:207`), read here as the first community release. The S2a test release is built
     inside its job and published nowhere (ADR-060 decision 30), so it ships nothing.
     Until the first
     community release, schema 1.0 changes only by an amendment of this ADR, and the
     commit that makes it regenerates every manifest of the repository.
   - From that release, a minor adds optional fields and nothing else. A minor never adds
     a value to a closed set of values, never changes a type and never makes a field
     required, because a lenient reader cannot ignore what it reads in a field it knows.
     Each of those is a new major, which check 1 denies (`07:141`; `08:497`, `:508`).
3. **Rules of every reading**, in both modes of decision 15:
   - the YAML rules of the layer parser: one document; no anchor, alias, explicit tag or
     merge key; no duplicate key; its typing of plain scalars; its bounds on size and
     depth (`boot/parse.go`; ADR-059 decision 6);
   - a manifest holds no closed reference: `${` in a manifest is text, and nothing
     resolves it;
   - every field has one type, and a field with a closed set of values takes only those;
   - a required field is present; an optional field may be absent, and an optional
     string that is empty reads as absent;
   - a list that this ADR calls a set holds no entry twice and is written sorted.

### The hand-written half

4. **The hand-written fields:**

| Field | Required | Form | Source |
|---|---|---|---|
| `manifestVersion` | Yes | `"1.0"` (decision 2) | `07:120`; `08:497` |
| `id` | Yes | `<owner>/<name>`. Each segment is kebab-case: lowercase ASCII letters and digits in words joined by single hyphens, starting with a letter. Equal to the `ID` of the plugin's `kernel.Plugin` value; for a pack with no code, the pack's own id. The owners of decision 14 are reserved | `07:121`, `:216`; `00:657` |
| `version` | Yes | A Semantic Versioning 2.0.0 version with no leading `v`, such as `0.1.0` | `07:85`, `:122` |
| `title` | Yes | One line of English text | `07:123`; community:170 |
| `description` | No | English text | `07:123` |
| `license` | Yes | An SPDX expression | `07:124`; community:172 |
| `publisher` | Yes | `{id, name, url}`. `id` is required and equals the owner segment of `id`; `name` and `url` are optional | `07:125`; `00:207` |
| `source` | No | An absolute URL | `07:126` |
| `support` | No | `{url, email}`, at least one of the two | `07:126`; community:173 |
| `stability` | Yes | `experimental`, `preview` or `ga` | `07:93`, `:127` |
| `scope` | Yes | Question Q2; decision 17 | `07:94`, `:128` |
| `runtimes` | Yes | A set of `inproc`, `remote` and `wasm`; `[]` is a pack with no code | `07:95`, `:129`; `08:381` |
| `capabilities` | No | `{network, secrets}`, each a set of strings, absent read as `[]`. A `network` entry is a host name, optionally with `:<port>`, or `config:<path>`, where the path names a property of `configSchema` by its keys joined with dots. A `secrets` entry is a name in the plugin's own secret scope | `07:99-100`, `:130`, `:161` |
| `integrates` | No | Decision 5 | `07:131` |
| `instantiable` | No | `true` or `false`, absent read as `false`; decision 14 | `07:101`, `:132` |

5. **`integrates`** is a list of entries `{system, versions, tested, credential,
   license}`, one per external system, with no system twice (`07:131`, `:1678-1683`;
   community:177):
   - `system`, required: the system's lowercase technical name, the form that T7 puts
     after the noun in `connector-<system>` (`08:1363`);
   - `versions`, required: text, the range of the system's versions that the fixtures
     cover, as the publisher states it. No check compares it, since external systems
     number their versions in different ways;
   - `tested`, required: a non-empty set of the versions the fixtures were recorded
     against, as strings;
   - `credential`, required: `read-only` or `broader`. Boot warns on an enabled row of a
     plugin that declares `broader` (`08:391`; ADR-060 decision 14);
   - `license`, optional: an SPDX expression, `LicenseRef-` ids included, naming the
     licence of a process the plugin drives and the user installs, apart from the
     plugin's own `license` (`07:131`; licence:241). It is absent where no such process
     exists, as for a vendor's hosted API.

### The generated half

6. **`uses`** is a set of `{key, major, as}`, one entry per dependency in `Spec.Inject`,
   sorted by key and major (`07:104-106`, `:133`; ADR-060 decision 11).
   - `key` is the key's name, `<key>.Bound` for a bound view, and `major` its major. `as`
     is `need` for `Need` and `NeedWith` and `want` for `Want` (`07:187-189`). The
     intercept config of `NeedWith` is a value, not a declaration, and is not written.
   - The kernel built-ins (`reflect`, `registry`, `hooks`, `logger`) are listed like any
     key; they belong to `pluginApi`, not to a definition (ADR-060 decision 9).
   - A key appears once, since the kernel refuses one key twice in `Inject`
     (`sdk/kernel/root.go`).
7. **`provides`** is a set of `{key, major}`, one per key in `Spec.Provides` (`07:134`).
8. **`registers`** is a set of `{key, major}`, one per registry the plugin contributes
   to, sorted by key and major (`07:135`). An entry names the registry's key, as `uses` and
   `provides` do, and not its definition as the sketch of `07:109` does: a definition may
   declare several keys (ADR-060 decision 7), and the definition of each key is in the
   served definition data (ADR-060 decision 9). Where the list comes from is question Q1.
9. **`configSchema`** is the JSON Schema 2020-12 derived from the config type, written
   inline as a mapping whose `$schema` names that dialect (`07:111`, `:137`; ADR-060
   decision 4).
   - It uses only the keywords of the derivation of ADR-060 decision 4, which the
     validator of the `plugin` package implements, and two annotation keywords on the
     subschema of a field: `x-aiops-volatile: true` for a `plugin.Volatile[T]` field and
     `x-aiops-tenant: true` for a field tagged `aiops:"tenant"`. A field marked tenant is
     also marked volatile (`07:137`, `:473`). Validation ignores both.
   - The volatile paths that ADR-060 decision 4 carries in the generated manifest are the
     paths to the subschemas marked volatile, each a list of property names (ADR-059
     decision 44). They are not a field of their own: the field list of `00:207` is
     closed, and `07:137` already puts the tenant mark in `configSchema`.
   - It is inline, not a file beside the manifest as the sketch of `07:111` has it. The
     manifest digest then covers the schema, the module table embeds one file per plugin
     (ADR-060 decision 20), and `FromManifest` builds the proxy row of a remote plugin
     from one file at S2b (`07:1133`).
   - A plugin built with `kernel.Define` has no config type, and its `configSchema` is
     `{}`, the schema that every value satisfies. The loader is the one such plugin
     (ADR-060 decisions 13 and 44), and boot writes its config, never a layer.
10. **`hooks`** is a set of `{hook, stage}`, one per entry of `Spec.Hooks`, sorted by hook
    and stage (`07:136`, `:219`).
    - `stage` is the stage's name, as `kernel.HookUse` carries it. Its ordinal is not
      manifest data: boot reads it from the definition data that `serves` carries
      (ADR-060 decisions 3, 7 and 9), so a stage that ADR-043 moves under the `v0` rule
      changes no manifest.
    - One hook may appear at two stages, since the kernel refuses only a repeated pair
      (`checkUses`, `sdk/kernel/hooks.go`); the pair is what the set holds once.
    - No access class is written: the gate reads it from the served definition data
      (ADR-060 decisions 7 and 15).
11. **`durable`** is a list (`07:138`). Its entry shape is fixed at S3 with
    `Spec.Durable` and the `durable` definition (ADR-060 decision 29). At S2a it is `[]`,
    and a reader refuses an entry.
12. **`requires`** is a mapping, required in every manifest (`07:139`; `08:497`):
    - `pluginApi`: `"<major>.<minor>"`, the author API the code was built against
      (ADR-060 decision 8). Present exactly when `runtimes` holds `inproc`.
    - `remoteProtocol`: a set of non-negative integers, the protocol majors the plugin's
      SDK speaks (`07:139`, `:1127`). Present exactly when `runtimes` holds `remote`. It
      is read from S2b, by check 3 (ADR-060 decision 29); at S2a `plugin check` refuses
      `remote` (ADR-060 decision 23), so no generator writes it.
    - `definitions`: a mapping from a definition name, the `<name>` of
      `sdk/def/<name>/v<N>`, to `"<major>.<minor>"` (ADR-060 decision 9; `07:115`). It
      names every definition whose key or hook the plugin uses, provides, contributes to
      or listens on; the kernel built-ins are not in it. For a pack with no code it names
      the registries its rows feed (`07:139`, `:1686`). Its keys are data, not fields.
13. **A pack with no code** (`runtimes: []`) has `requires` as its whole generated half,
    as `07:1684-1687` shows: its rows are rows of first-party plugins, whose own manifests
    carry their declarations (`07:1630`). A reader refuses the other six generated fields
    on such a manifest. A manifest of code carries all seven, and a generator writes an
    empty set as `[]`.

### Reserved owners

14. **`aiops` and `contrib` are the reserved owners**, both reserved to artifacts built by
    the release pipeline (`07:121`; `00:657`; community:171; `08:1363`): `aiops` for
    first-party plugins and `contrib` for the community plugins kept under `contrib/`
    (`07:1582-1583`). Each place that reads an id holds the reservation as far as its
    inputs decide it:
    - the builder refuses a module outside the release that declares an `aiops/` or
      `contrib/` id (`07:1860`; ADR-060 decision 22);
    - check 5 admits an artifact only from a signer listed for its id and runtime
      (`08:501`), and the release key is the default distribution root (ADR-060 decision
      50). Whether `trust add` refuses, for any other signer, an id pattern that covers a
      reserved owner is ADR-020's, with the id-pattern syntax (ADR-060 decision 33);
    - an index may list only ids inside its patterns, so it cannot shadow `aiops/*` or
      `contrib/*` (`07:1854`), from S6a;
    - step 1 of `plugin check` fails on a reserved owner outside the release pipeline
      (`07:1567`), decided as question Q3 says;
    - `publisher.id` equals the owner (`07:125`), so the reservation covers it;
    - `instantiable: true` is valid only under the owner `aiops`, since it marks a
      first-party plugin, and only with a non-empty `registers` and empty `provides`,
      `hooks` and `durable`, since such a plugin "registers contributions and nothing
      else" (`07:132`; `08:391`);
    - a manifest under a reserved owner takes SPDX list ids only in `license`, since its
      plugin ships in `aiops` or `aiops-contrib` (`07:124`).

    The test distributions use the owner `aiops-test` (`test/dist/plugins`). It is not
    reserved, and the link guard of ADR-060 decision 28 keeps those plugins out of every
    shipped binary.

### The strict and the lenient reader

15. **One reader, in `boot`, with two modes** (ADR-060 decision 14).
    - Boot reads manifests into types of its own, since it may not import the `plugin`
      package (`08:1255`). The same code serves both modes, and `aiopsctl` links it as the
      launcher of the `cli` profile (`08:359`; ADR-060 decision 53), so `plugin check` and
      the gate cannot disagree on a known field. `plugingen` writes from the Go type of
      the schema in the `plugin` package (ADR-060 decision 4), and a test reads each
      generated manifest back through boot's reader and compares the two: the test that
      ADR-060 decision 14 asks for.
    - **Strict mode** is the reading of step 1 of `plugin check`, of `plugin check
      --write` before it rewrites the generated half, and of the parity check
      (`07:1567-1568`; `08:1258`). It fails on an unknown major and on an unknown field at
      any depth (`07:120`, `:1567`): a key of a mapping whose keys the schema names, at
      the top or inside `publisher`, `support`, `capabilities`, `integrates`, `uses`,
      `provides`, `registers`, `hooks` or `requires`. The keys of `requires.definitions`
      and the content of `configSchema` are data, not fields.
    - **Lenient mode** is the reading of the gate at every entry point of ADR-060
      decision 17. It denies under check 1 an absent `manifestVersion` or an unknown
      major (`08:497`), and ignores the unknown fields of a manifest whose minor is higher
      than the reader's (`07:120`; community:149). What it does with an unknown field at a
      minor it knows, and with a known field that is absent or malformed, is question Q4.
    - Beside the strict reading, step 1 runs the rules of `07:1567` that are not schema:
      `publisher.id` against the owner; the reserved owners (question Q3); the SPDX check
      of `license` against the list data that ADR-060 decision 23 embeds in `aiopsctl`;
      every key, hook and stage against the served data of the release it checks
      against; the name lint of T1 and T7 (`08:1357`, `:1363`); the `instantiable` shape
      of decision 14; each `config:<path>` of `capabilities.network` against
      `configSchema`; and the rules for a contributor to `harnesses` (`07:130`;
      harness:58), which apply once that definition lands at S8a. The gate runs none of
      them: they are lints of authoring, and the SPDX data stays out of the server binary.
16. **What the gate reads.** Each check reads these fields of a plugin manifest and no
    other; check 6 reads the build list and the signed distribution manifest, not a
    plugin manifest (`08:502`). Boot also reads `configSchema` for
    `--dump-config-schema` and the volatile paths, `hooks` for the dumps, and
    `integrates[].credential` for the warning on `broader` (ADR-060 decision 14).

| Check | Fields | Source |
|---|---|---|
| 1 | `manifestVersion`; `requires` and `requires.pluginApi`; `runtimes`, for `wasm` | `08:497`; ADR-060 decision 58 |
| 2 | `requires.definitions` | `08:498` |
| 3, from S2b | `requires.remoteProtocol`; the manifest digest | `08:499`; `07:1886` |
| 4 | `stability` | `08:500` |
| 5 | `id`, `runtimes` | `08:501` |
| 7 | `provides`, `uses`, `hooks` | `08:503` |
| 8 | `id` and `instantiable` of the plugin a row names; `scope`, in its S4a and S4b clauses | `08:504` |
| 9, from S2b | `scope`, `runtimes`, `provides`, `registers`, `hooks` | `08:505` |
| 10 | `requires.definitions`; `provides` and `registers`, which name the definitions whose suite revision it asks for | `08:506`; ADR-060 decision 56 |

### `scope`

17. **`scope`** takes the form that question Q2 decides. Whatever the answer:
    - its values are `process` and `tenant`, and "both" is a plugin that may run at either
      (`00:207`; `07:128`);
    - `zz_tenants_test.go` is written for a plugin whose scope holds `process`
      (`07:1435`), and step 4 mounts a plugin whose scope holds `tenant` in two trees
      (`07:1399`);
    - no tenant tree exists before S4a (`08:697`), so step 4 cannot mount one, and
      `plugin check` refuses a manifest whose scope holds `tenant` until S4a, as it
      refuses `remote` until S2b (ADR-060 decision 23);
    - how a row of a plugin that may run at either scope takes one is fixed at S4a with
      `tenant-trees`, which mounts the rows of tenant scope (plugin record §10). The S2a
      reader fixes the form only.

### What changes in the code at S2a

18. **From the S1 readers to schema 1.0:**
    - the five manifests under `plugins/` get their generated half from `plugingen` and
      lose `needs` (ADR-060 decision 44);
    - `boot.ModuleEntry` is filled from the manifest: `Provides`, `Needs` and `Wants` from
      `provides` and `uses`, `Hooks` from `hooks` with the ordinal from `serves`, and the
      volatile paths from `configSchema`. The hand lists in `dist.Module.Volatile` and
      `boot.ModuleEntry.Volatile` go (ADR-060 decision 4). `Classes` come from served
      definition data (ADR-060 decision 14), and `modules.yaml` goes with the S1
      `dist.yaml` (ADR-060 decision 20);
    - hook entries are keyed by hook and stage, not by hook alone;
    - the reader adds to `boot` under its provisional cap (ADR-060 decision 35).
19. **No new dependency.** Both modes read with `go.yaml.in/yaml/v3` at the version
    `go.mod` pins, `v3.0.6` (ADR-060 decisions 6 and 41). The `LICENSE` file of
    `go.yaml.in/yaml/v3@v3.0.6`, read from the module at that version for this ADR, puts
    the files ported from libyaml under MIT and the rest under Apache-2.0, as licence:44
    records. The SPDX list data of step 1 belongs to ADR-060 decision 23 and is read under
    ADR-060 decision 34 before it merges. The schema derivation and its validator use the
    standard library (ADR-060 decision 4).

### What stays open

20. Not settled here:
    - index format 1.0, the S6a part of ADR-048 (`08:1179`);
    - the Python parts: every `plugin.toml` key other than `entrypoint`, and whether
      `plugin.yaml` carries `entrypoint`, at S2b (`07:66` R8, `:143`);
    - the entry shape of `durable`, at S3 (decision 11);
    - how a row of a plugin that may run at either scope takes one, at S4a (decision 17);
    - the tenant-toggleable test over the generated half (`07:141`), with layer 5 at
      S4b, where the overlay part of check 8 needs it (ADR-060 decision 29);
    - the stage vocabulary per definition, ADR-043 at S5 (ADR-060 decision 7);
    - the media type of `plugin.yaml` and its place in an artifact, ADR-047 (ADR-060
      decision 33).

## Consequences

- Every field of schema 1.0 has one form, one half and a known reader, and the gate reads
  only the fields its checks name.
- `plugin check` and the gate share one reader in `boot`, so they differ only in what
  the mode says about unknown fields.
- The gate reads `registers` from S2a, through check 10, so question Q1 is answered before
  check 10 merges; `plugingen` and the parity check need it before the first generated
  manifest merges.
- `07:109` and `07:111` are sketches that decisions 8 and 9 replace: `registers` names
  keys, and `configSchema` is inline. The commit that builds the reader edits both lines,
  with the edits that the answers below name.
- The five S1 manifests, `modules.yaml` and the hand-written volatile lists are replaced
  by the commits that build `plugingen` and the reader.
- A manifest written at S2a changes before the first community release only by an
  amendment of this ADR, with every manifest of the repository regenerated in the same
  commit.
- On acceptance the row of ADR-048 in `08` §3 records its S2a part; the S6a part stays
  open.

## Questions for the owner

Each question gives its options, the recommended one, why, and when the answer is needed.

**Q1. Where does `registers` come from?** It is a generated field (`00:207`; `07:135`).
The `Spec` has no field for it (`07:215-223`), a `Need` on a registry key reads the same
for a consumer and a contributor, and the gate and the tenant-toggleable test read it
without running code (ADR-060 decision 11; `07:141`). Check 10 reads it from S2a (decision
16), check 9 from S2b. A Python author declares it with `@contributes(definition)`
(`07:1201`). In the dsh model, `provide` metadata is optional; the plan made `Provides`
mandatory so that the composer analyses the graph without running code (dsh row 6,
`08:48`; plugin record §15).
- (a) A `Spec` field, `Registers []kernel.AnyKey`, which the author fills as `Provides`
  is filled. `plugingen` writes `registers` from it, and the generated cycle test fails
  when a fake's `Inventory()` holds a registration of the plugin's fiber in a registry the
  field does not list (`07:519`, `:1434`), so a plugin that passes step 4 does not
  under-declare. From S2b the proxy row refuses a remote `Register` outside the list. No kernel change:
  the field is `Spec` data, as `Durable` and the config type already are for `plugingen`
  (ADR-060 decision 11). It edits `00:44` ("Needs ..., provisions, hooks and durable
  namespaces are declared as data" gains contributions), `00:207` (the fields of the
  `Spec`), `07:215-223`, `07:235` and plugin record §4.
- (b) A dependency form `kernel.Contribute(k)` beside `Need` and `Want`, a `Need` that
  also marks a contribution, so the declaration is part of `Inject`, is carried by the
  `kernel.Plugin` value and also covers a plugin built with `kernel.Define`. It is a kernel
  change, proposed as an ADR-034 addition and verified as ADR-060 decision 3 requires. It
  edits `00:44`, `07:187-189` and plugin record §4.
- (c) `plugingen` finds the `Register` calls by static analysis of the code reachable
  from `Apply`. Nothing an author writes changes, but a registration made through a helper
  in another module, an interface or function value, or reflection is missed; check 10
  then asks no suite revision for that registry, and check 9 reads a short list.
- Recommended: (a). It is the adaptation dsh row 6 already made for `Provides`, it
  matches `@contributes` in Python, it needs no kernel change, and a test holds it true.
  Needed before `plugin.Define` and `plugingen` merge.

**Q2. Is `scope` a list or one value?** `00:207`, `07:128` and plugin record §4 say
"`process`, `tenant` or both". `07:94`, `07:153`, `07:1673` and the five S1 manifests
write a list; `07:477-478`, `08:504` and plugin record §4 and §10 write `scope: process`
and `scope: tenant`, and call process the default. Nothing defines "both".
- (a) A set: `[process]`, `[tenant]` or `[process, tenant]`, the last being "both";
  required in every manifest; the templates write `[process]`, the default of `07:477`. A
  prose `scope: tenant` reads as a scope that holds `tenant`. It edits nothing in `00`;
  the comment of `07:94` becomes "a set of process and tenant".
- (b) One string: `process`, `tenant` or `both`, required. It edits `07:94`, `07:153`,
  `07:1673` and the five S1 manifests.
- (c) One string, `process` or `tenant`, and no "both". It edits `00:207`, `07:128` and
  plugin record §4.
- Recommended: (a). It is the form `07:94`, `07:153`, `07:1673` and the S1 manifests
  already write, "both" needs no third word, it reads like `runtimes`, and `00` stays as
  written. A required field also spares the gate a default it would have to know. Needed
  before the manifest type of the `plugin` package merges.

**Q3. How does step 1 of `plugin check` tell the release pipeline from anyone else?**
Step 1 fails on "a reserved owner outside the release pipeline" (`07:1567`). CI runs the
check on every plugin under `plugins/`, owner `aiops` (ADR-060 decision 32); a contributor
runs it on a plugin that `plugin new --contrib` wrote, owner `contrib` (`08:407`;
`07:1526`); and nothing in a plugin directory says who built it. The reservation itself is
enforced by the builder and by check 5 (decision 14).
- (a) By where the code is. An `aiops` id passes only in a module whose path is the root
  module path of this repository, and a `contrib` id only in a module whose path is under
  its `contrib/` path or, for a pack with no code, in a directory under `contrib/` below
  the root module's `go.mod`. `plugin check` reads both from the `go.mod` files it already
  reads. A copy that claims those paths passes the lint and is stopped where the
  reservation is enforced: the builder (ADR-060 decision 22) and check 5 (ADR-060
  decision 50).
- (b) By a flag. Step 1 fails on a reserved owner unless `plugin check --release` runs,
  which the release pipeline, `make check` and this repository's CI pass. A contributor
  who runs `plugin check` in `contrib/<name>` without it gets a failure that is not one.
- (c) Step 1 checks no owner, and `07:1567` loses the clause; the builder, check 5 and
  the index carry the reservation. It removes a written check and is listed to be
  refused.
- Recommended: (a). The clause of `07:1567` stays and is decided from files the check
  already reads, with no flag to forget or pass by mistake, and the security of the
  reservation stays where a signature or the release build decides it. Needed before
  step 1 of `plugin check` merges.

**Q4. What does the gate do with an unknown field at a minor it knows, and with a known
field that is absent or malformed?** A reader ignores unknown fields of a higher minor
(`07:120`; community:149). Check 1 denies an absent `manifestVersion`, an unknown major,
an absent `requires`, an unserved `pluginApi` and `wasm` (`08:497`); no clause denies a
manifest whose `id`, `stability` or `uses` is absent or of the wrong type, and a gate that
reads one must deny or guess. A manifest that passed `plugin check` has neither defect, so
both reach the gate only from a manifest made or edited without it, as the copy of exit
step D1 is (ADR-060 decision 30).
- (a) At a minor the reader knows, an unknown field makes the manifest malformed. Check 1
  denies a malformed manifest: a required field absent, a known field of the wrong type or
  value, or an unknown field at a known minor. Unknown fields are ignored at a higher minor
  only, as `07:120` says. Check 1 of the one gate text (`08:497`; ADR-060 decision 16)
  gains that clause.
- (b) The gate ignores unknown fields at every minor, and check 1 denies only an absent
  required field or a malformed known field. `08:497` gains that narrower clause.
- (c) The gate reads as strictly as `plugin check`. Every binary then denies a plugin
  built against a later minor, which undoes frozen-additive (`07:120`; `00:207`); listed
  to be refused.
- Recommended: (a). It is `07:120` as written: the leniency is the one that frozen-additive
  needs, and a manifest that no generator writes is denied by a named check rather than
  admitted on a guess. Exit step D1 is unaffected, since its copy is well formed and check
  2 alone denies it. Needed before gate check 1 merges.
