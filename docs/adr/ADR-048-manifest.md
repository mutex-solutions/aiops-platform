# ADR-048: Manifest schema 1.0 (the S2a part)

- Status: **accepted 2026-11-23 by the project owner, for the S2a part.** The owner
  answered the four questions on the same date with the recommended option of each; the
  section "Owner answers of 2026-11-23" records them as decisions 21 to 24. The S6a part
  (Scope) and the other points of decision 20 stay open for their later steps. Where an
  answer changes a line of `00-decisions.md`, the question names the line, and `00` is
  edited in the commit that records the answer (`08:996`).
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
  `491a518`; between that commit and `8e1f7b9` one line was inserted in `08` after line
  1189, so the lines of `08` from 1190 on moved down by one, and no other cited line
  moved. Code is cited by file at `8e1f7b9`.

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
  (`07:1201`), and the first message of the handshake lists "the definitions the plugin
  registers with, each as `name@major.minor`" (`07:1121`).
- `scope` is "`process`, `tenant` or both" (`00:207`; `07:128`; plugin record §4).
  `07:94`, `07:153`, `07:1673` and the five S1 manifests write a list; the prose writes one
  value (`07:477-478`, "`scope: process` (default)"; `08:504`). Nothing says what "both"
  does to a row.
- `aiops` and `contrib` are reserved to artifacts built by the release pipeline (`07:121`;
  `00:657`; community:171), and step 1 of `plugin check` fails on "a reserved owner
  outside the release pipeline" (`07:1567`). CI runs `plugin check` on every plugin under
  `plugins/` and `examples/` (ADR-060 decision 32), and a contributor runs it on a plugin
  that `plugin new --contrib` wrote with the owner `contrib` (`08:407`; `07:1526`).
  Nothing says how the check tells them from a third party. The builder holds the
  reservation for Go modules (`07:1860`); for a pack or remote code, check 5 admits any
  signer that `trust.yaml` lists for the id (`08:501`), and a pattern may cover `aiops/*`.
- A reader ignores unknown fields of a higher minor, and `plugin check` is strict
  (`07:120`; community:149; `07:1567`). Neither says what the gate does with an unknown
  field at a minor it knows, and no clause of check 1 denies a manifest whose known fields
  are absent or malformed (`08:497`).
- The six hooks of the kernel (`internal/plugin`, `internal/status`, `internal/service`,
  `internal/config`, `internal/update`, `internal/commit`) are sealed, have the one stage
  `default` and no owner, and no definition declares them (`sdk/kernel/hooks.go`).
  `readiness` and `restart-policy` listen on `internal/status` and `config-refs` on
  `internal/config` (their `plugin.yaml`; `dist/dist.yaml`). ADR-060 decision 9 gives the
  kernel's four built-in keys to `pluginApi` and says nothing of its hooks, while
  `serves` carries the stages and classes of definitions only.
- When `v<N+1>` of a stable definition ships, `v<N>` stays served through an adapter row
  (`00:266`), a plugin that uses both majors of one definition, and `serves` names each
  served major of a definition (ADR-060 decision 9). `07:115` and community:134 write one
  `"major.minor"` per definition name.
- Every plugin of `test/dist/plugins` is built with `kernel.Define`, takes its config from
  a layer, and `test/dist/dist.go` lists the volatile path `config.note` of
  `aiops-test/toy-consumer` by hand.

What boot reads at `8e1f7b9` is not schema 1.0:

| What boot reads | From | In schema 1.0 |
|---|---|---|
| `ModuleEntry.ID` | `kernel.Plugin.ID` (the table of `dist/dist.go` at `8e1f7b9`, which `go run ./tools/plugingen --write .` now generates into `dist/modules.go`; ADR-060 decision 20), or the key of `modules.yaml` | `id` |
| `Module`, `Version`, `Sum` | `Module` and `Version` from the constants `ModulePath` and `Version` of `dist/dist.go`, or `modules.yaml`; `Sum` from `modules.yaml` only | Not manifest fields: the module table of the signed distribution manifest (ADR-060 decisions 19 and 20) |
| `Provides` | The key names of `kernel.Plugin.Provides` | `provides`, with the major of each key |
| `Needs`, `Wants` | `kernel.Plugin.Inject`, split by `Optional()` | `uses`, with the major and `as` |
| `Hooks` | `kernel.Plugin.Hooks` with the stage name and ordinal 0; `modules.yaml` with an ordinal and a name | `hooks`, with the stage by name; the ordinal from served data |
| `Volatile`, dotted paths under `config.` | `dist.Module.Volatile` by hand, or `modules.yaml` | Marks in `configSchema`; a path is a list of keys |
| `Classes` | `modules.yaml` only | Not manifest data: served data (ADR-060 decisions 14 and 15) |
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
     Until the first community release, schema 1.0 changes only by an amendment of this
     ADR, and the commit that makes it regenerates every manifest of the repository.
   - From that release, a minor adds optional fields and nothing else. A minor never adds
     a value to a closed set of values, never changes a type and never makes a field
     required, because a lenient reader cannot ignore what it reads in a field it knows.
     Each of those is a new major, which check 1 denies (`07:141`; `08:497`, `:508`).
3. **Rules of every reading**, in both modes of decision 15. A rule that needs nothing but
   the manifest is a rule of the schema and is listed here; a rule that needs other data
   is a rule of step 1 of `plugin check` (decision 15).
   - The YAML rules of the layer parser: one document; no anchor, alias, explicit tag or
     merge key; no duplicate key; its typing of plain scalars; its bounds on size and
     depth (`boot/parse.go`; ADR-059 decision 6).
   - A manifest holds no closed reference: `${` in a manifest is text, and nothing
     resolves it.
   - Every field has one type, and a field with a closed set of values takes only those.
   - A required field is present; an optional field may be absent, and an optional
     string that is empty reads as absent.
   - A list that this ADR calls a set holds no entry twice and is written sorted.
   - The rules between fields, each stated with its field below: `publisher.id` equals the
     owner segment of `id` (decision 4); `license` and `integrates[].license` follow the
     SPDX expression grammar (decisions 4, 5 and 19); each `config:` pointer of a plugin
     with code names a property of its own `configSchema` (decision 4); `requires` holds
     `pluginApi` and `remoteProtocol` exactly with their runtimes (decision 12); a pack
     carries no generated field but `requires` (decision 13); and the rules of the
     reserved owners on `instantiable`, `license` and `integrates[].credential`
     (decision 14).

   What the gate does with a manifest that breaks one of these rules is question Q4.

### The hand-written half

4. **The hand-written fields:**

| Field | Required | Form | Source |
|---|---|---|---|
| `manifestVersion` | Yes | `"1.0"` (decision 2) | `07:120`; `08:497` |
| `id` | Yes | `<owner>/<name>`. Each segment is kebab-case: lowercase ASCII letters and digits in words joined by single hyphens, starting with a letter. Equal to the `ID` of the plugin's `kernel.Plugin` value (decision 15); for a pack with no code, the pack's own id. The owners of decision 14 are reserved | `07:121`, `:216`; `00:657` |
| `version` | Yes | A Semantic Versioning 2.0.0 version with no leading `v`, such as `0.1.0` | `07:85`, `:122` |
| `title` | Yes | One line of English text | `07:123`; community:170 |
| `description` | No | English text | `07:123` |
| `license` | Yes | An SPDX expression (decision 19). Under a reserved owner, SPDX list ids only: no `LicenseRef-` or `DocumentRef-` id (decision 14) | `07:124`; community:172 |
| `publisher` | Yes | `{id, name, url}`. `id` is required and equals the owner segment of `id`; `name` and `url` are optional | `07:125`; `00:207` |
| `source` | No | An absolute URL | `07:126` |
| `support` | No | `{url, email}`, at least one of the two | `07:126`; community:173 |
| `stability` | Yes | `experimental`, `preview` or `ga` | `07:93`, `:127` |
| `scope` | Yes | Question Q2; decision 17 | `07:94`, `:128` |
| `runtimes` | Yes | A set of `inproc`, `remote` and `wasm`; `[]` is a pack with no code | `07:95`, `:129`; `08:381` |
| `capabilities` | No | `{network, secrets}`, each a set of strings, absent read as `[]`. A `network` entry is a host name, optionally with `:<port>`, or `config:` followed by a JSON Pointer (RFC 6901) into the config, such as `config:/url`. A pointer is a list of keys: inside a key, `~1` stands for `/` and `~0` for `~`, so a key that holds a dot or a slash is one key (ADR-059 decision 44; ADR-060 decision 4). For a plugin with code the pointer names a property of its own `configSchema`; for a pack, of the `configSchema` of a plugin whose rows its bundle inserts (decision 13). A `secrets` entry is a name in the plugin's own secret scope | `07:99-100`, `:130`, `:161`, `:1675` |
| `integrates` | No | Decision 5 | `07:131` |
| `instantiable` | No | `true` or `false`, absent read as `false`; decision 14 | `07:101`, `:132` |

**A reading of community:180.** community:180 puts "a custom licence reference" among
what schema 1.0 does not hold, beside an edition, an entitlement reference, a price and a
publisher verification mark. It is read as a field of its own that refers to a custom
licence agreement, which the enterprise edition would add with a manifest major
(`00:207`; `07:141`). It is not an SPDX `LicenseRef-` id inside the expression of
`license`: `07:124` admits such an id outside the reserved owners, and community:172
implies it by limiting the SPDX list ids to `aiops`, `aiops-contrib` and the project
index. The commit that builds the reader edits community:180 to "a custom licence
reference field".

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
     plugin that declares `broader` (`08:391`; ADR-060 decision 14), and a manifest under
     the owner `contrib` may not declare it (decision 14);
   - `license`, optional: an SPDX expression (decision 19), `LicenseRef-` ids included,
     naming the licence of a process the plugin drives and the user installs, apart from
     the plugin's own `license` (`07:131`; licence:241). It is absent where no such
     process exists, as for a vendor's hosted API. No owner adds a rule to it in schema
     1.0. A first-party plugin that drives an upstream process names a process that the
     upstream register already holds with its licence (`08:1332` L3).
     The project index does not list a plugin whose driven process is not under an open
     licence (`07:131`; licence:241), and a `contrib` pack is listed in it (`08:412`), so
     that rule reaches a `contrib` pack through the index lint, from S6a with index format
     1.0 (decision 20).

### The generated half

6. **`uses`** is a set of `{key, major, as}`, one entry per dependency in `Spec.Inject`,
   sorted by key and major (`07:104-106`, `:133`; ADR-060 decision 11).
   - `key` is the key's name, `<key>.Bound` for a bound view, and `major` its major. `as`
     is `need` for `Need` and `NeedWith` and `want` for `Want` (`07:187-189`). The
     intercept config of `NeedWith` is a value, not a declaration, and is not written.
   - The kernel built-ins (`reflect`, `registry`, `hooks`, `logger`) are listed like any
     key; they belong to `pluginApi`, not to a definition (ADR-060 decision 9).
   - A pair of key and major appears once, since the kernel refuses one key twice in
     `Inject` (`sdk/kernel/root.go`). Two majors of one key are two entries.
7. **`provides`** is a set of `{key, major}`, one per key in `Spec.Provides` (`07:134`).
8. **`registers`** is a set of `{key, major}`, one per registry the plugin contributes
   to, sorted by key and major (`07:135`). An entry names the registry's key, as `uses` and
   `provides` do, and not its definition as the sketch of `07:109` does: a definition may
   declare several keys (ADR-060 decision 7).
   - Each key belongs to one definition, which the served data names (ADR-060 decision
     9), so the definitions a plugin contributes to are derived from `registers`. Check 10
     asks a suite revision of each (ADR-060 decision 56), and from S2b the first message
     of the handshake lists them as `name@major.minor` (`07:1121`), which stays as written.
   - A Python author marks a contribution with `@contributes(definition)` (`07:1201`;
     plugin record §4), and `aiops-sdk manifest` writes it as the registry key that the
     definition declares. Whether the decorator also names a key, for a definition that
     declares more than one registry key, is a Python part fixed at S2b (`07:66` R8);
     `07:1201` and plugin record §4 stay as written until then.
   - Where the list comes from is question Q1.
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
   - A plugin built with `kernel.Define` has no config type. Its `configSchema` is `{}`,
     the schema that every value satisfies, with no volatile mark, and its config is
     checked by its `Decode` alone. In the release table the loader is the one such
     plugin (ADR-060 decisions 13 and 44), and boot writes its config, never a layer.
     In the test distribution every plugin is built with `kernel.Define` at `8e1f7b9`
     (`test/dist/plugins`) and takes its config from a layer; each keeps `kernel.Define`
     and gets `{}`, its config checked by `Decode` as at S1, except a plugin whose config
     has a volatile field. `kernel.Define` can mark none, and the hand lists go (ADR-060
     decision 4), so such a plugin moves to `plugin.Define`. At `8e1f7b9` that is
     `aiops-test/toy-consumer`, whose `note` is volatile (`test/dist/dist.go`); after the
     move its derived path names the field that the hand list names, so the cells and
     cases that commit the note keep their outcomes (ADR-060 decision 28).
10. **`hooks`** is a set of `{hook, stage}`, one per entry of `Spec.Hooks`, sorted by hook
    and stage (`07:136`, `:219`).
    - `stage` is the stage's name, as `kernel.HookUse` carries it. Its ordinal is not
      manifest data: boot reads it from served data, the definition data of the hook's
      definition or, for a hook of the kernel, the kernel part of decision 12 (ADR-060
      decisions 3, 7 and 9), so a stage that ADR-043 moves under the `v0` rule changes no
      manifest.
    - The six hooks of the kernel are listed like any hook. They belong to `pluginApi`, as
      the kernel's built-in keys do (ADR-060 decision 9), and to no definition.
    - One hook may appear at two stages, since the kernel refuses only a repeated pair
      (`checkUses`, `sdk/kernel/hooks.go`); the pair is what the set holds once.
    - No access class is written: the gate reads it from served data (ADR-060 decisions 7
      and 15; decision 12).
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
      `sdk/def/<name>/v<N>`, to the set of `"<major>.<minor>"` it needs, one per major,
      written as a list sorted by major, such as `{echoers: ["0.1"]}` (ADR-060 decision
      9). One major is the common case; an adapter row that serves `v<N>` over `v<N+1>`
      names both (`00:266`). It names every definition whose key or hook the plugin uses,
      provides, contributes to or listens on. The kernel's built-in keys and its hooks
      belong to `pluginApi` and are not in it. For a pack with no code it names the
      registries its rows feed (`07:139`, `:1686`). Its keys are data, not fields.
    - Check 2 matches each entry by name and major against `serves`, which names each
      served major of a definition (ADR-060 decision 9). The served data also carries a
      kernel part for the `pluginApi` the binary serves: the four built-in keys with
      their classes and the six hooks of the kernel with their stage and seal. The
      builder writes it from `sdk/kernel`, and a test holds it equal to
      `kernel.KeyClasses`, `kernel.HookSealed` and the stage of each hook, as the test of
      ADR-060 decision 7 holds a definition's data equal to its Go declarations. Step 1,
      check 7 and the offline gate read the kernel's keys and hooks there. This adds to
      what ADR-060 decision 9 lists for `serves`.
13. **A pack with no code** (`runtimes: []`) has `requires` as its whole generated half,
    as `07:1684-1687` shows: its rows are rows of first-party plugins, whose own manifests
    carry their declarations (`07:1630`). A reader refuses the other six generated fields
    on such a manifest. A manifest of code carries all seven, and a generator writes an
    empty set as `[]`. A `config:` pointer of a pack names a property of the
    `configSchema` of a plugin whose rows the pack's bundle inserts, read from the
    manifests of the release that `plugin check` runs against (ADR-060 decision 17), and
    covers each row of the pack whose config holds it: in the worked pack of
    `07:1662-1700`, `config:/endpoint` names the `endpoint` of its `aiops/tool-mcp` rows.

### Reserved owners

14. **`aiops` and `contrib` are the reserved owners**, both reserved to artifacts built by
    the release pipeline (`07:121`; `00:657`; community:171; `08:1363`): `aiops` for
    first-party plugins and `contrib` for the community plugins kept under `contrib/`
    (`07:1582-1583`). Each place that reads an id holds the reservation as far as its
    inputs decide it:
    - the builder refuses a module outside the release that declares an `aiops/` or
      `contrib/` id (`07:1860`; ADR-060 decision 22). A Go module passes check 5 only
      when a trusted distribution manifest holds it (`07:1861`); the release key, a
      default distribution root (ADR-060 decision 50), covers Go modules through
      distribution manifests only;
    - check 5 denies an artifact with no module, a pack or remote code, whose id is under
      `aiops/` or `contrib/` unless its verified signer is a key of the release pipeline
      that the binary carries: the release key of ADR-060 decision 50, or a key that
      ADR-020 names for the release's artifacts. A pattern of `trust.yaml` that covers a
      reserved owner then admits no other signer there. The clause joins check 5 in the
      one gate text (ADR-060 decision 16; `08:501`; `07:1888`). The key format and the
      id-pattern syntax of `trust add` stay ADR-020's (ADR-060 decision 33);
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
      plugin ships in `aiops` or `aiops-contrib` (`07:124`): no `LicenseRef-` or
      `DocumentRef-` id;
    - a manifest under the owner `contrib` declares no `credential: broader` in
      `integrates`, which `07:131` and `08:391` refuse under `contrib/`.

    The test distributions use the owner `aiops-test` (`test/dist/plugins`). It is not
    reserved, and the link guard of ADR-060 decision 28 keeps those plugins out of every
    shipped binary.

    The plugins under `examples/` of the S2a test release, the dispatcher
    `example-dispatch` (ADR-060 decision 5), the toy instantiable echoer and the toy
    caller (ADR-060 decision 38), take the owner `aiops`: the echoer is instantiable, and
    check 8 reads `instantiable: true` from the manifest of a first-party plugin of the
    module table (ADR-060 decision 19). `examples/` is in the root module, so step 1
    admits them under question Q3 (a). Only the test release holds them, so check 8
    reads the echoer from that release alone. The shipped table gains nothing, the link
    guard keeps them out of `cmd/aiops` and `cmd/aiopsctl`, and the instantiable plugins
    at `0.1.0` stay the three of `07:132` (ADR-060 decision 38).

### The strict and the lenient reader

15. **One reader, in `boot`, with two modes** (ADR-060 decision 14).
    - Boot reads manifests into types of its own, since it may not import the `plugin`
      package (`08:1255`). The gate reads with the lenient mode. `plugingen` writes from
      the Go type of the schema in the `plugin` package (ADR-060 decision 4), and a test
      reads each generated manifest back through boot's reader and compares the two: the
      test that ADR-060 decision 14 asks for.
    - **Where the strict mode runs.** `plugin check` and `plugin check --write` are
      command rows of the `cli` profile (ADR-060 decision 53), and a plugin imports only
      the SDK and its own dependencies (`08:1256`), so the row cannot import `boot`. How
      command rows reach the gate, the builder and the operations library is fixed in the
      ADR that proposes the `commands` definition (ADR-060 decision 53). The strict
      reading, the rules of step 1 below and the SPDX list data reach the `plugin check`
      row the same way. This ADR asks one thing of that answer: the row runs boot's
      reader, handed to it by the launcher of the `cli` profile, which links `boot`
      (`08:359`), for example as a value under a launch key, and not a second copy. If
      that ADR places a second reader anywhere, a test holds the two equal on every
      manifest of the repository and on a corpus of malformed ones. That `plugin check`
      and the gate agree on every known field rests on that answer.
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
      minor it knows, and with a known field that is absent or breaks a rule of decision
      3, is question Q4.
    - **The rules of step 1 that need other data** than the manifest (`07:1567`):
      - the reserved owners against where the code is (question Q3);
      - each id of `license` and `integrates[].license` against the SPDX list data that
        ADR-060 decision 23 embeds in `aiopsctl` (decision 19);
      - `id` against the `ID` of the plugin's `kernel.Plugin` value, which `plugingen`
        reads (`07:121`, `:216`). Boot compares the two for every entry of the module
        table, as `boot/launch.go` compares the table's id with that value at `8e1f7b9`;
      - every key, hook and stage against the served data of the release it checks
        against, the kernel part of decision 12 included;
      - for a pack, each `config:` pointer against the `configSchema` of the plugins its
        rows name, in that release (decision 13);
      - the name lint of T1 and T7 (`08:1357`, `:1363`);
      - from S8a, when the `harnesses` definition lands, the rules for a contributor to
        it: `dialer` in its `uses`, a `network` capability on a remote manifest, or a
        `secrets` entry that its declared departures do not explain (`07:130`, `:1567`;
        harness:58).
    - **What the gate runs.** The gate runs the rules of decision 3 as question Q4
      decides, and two rules beyond them: the reserved owners of an artifact, in check 5
      (decision 14), and, from S8a, the refusal of a `harnesses` contributor that
      declares `dialer` in its `uses` or a network capability, which `07:1895`,
      harness:58 and `07:130` give to admission, in the check that ADR-051 names with the
      definition. The other rules of step 1 stay lints of authoring: `07:124`, `07:1567`
      and `08:1363` give them to `plugin check` and the index, and the SPDX data stays
      out of the server binary. community:167 heads its rules "checked by
      `plugin check`, the gate and the index checks"; the gate checks each of them that
      needs only the manifest, `publisher.id` and the SPDX grammar included, through
      check 1 (decision 3), and the list lookup of `license` stays with `plugin check`
      and the index, as `07:124` names them.
16. **What the gate reads.** Each check reads the fields below of a plugin manifest; a
    clause that lands at a later step reads the fields named with that step. Check 6
    reads the build list and the signed distribution manifest, not a plugin manifest
    (`08:502`). Boot also reads `configSchema` for `--dump-config-schema` and the volatile
    paths, `hooks` for the dumps, and `integrates[].credential` for the warning on
    `broader` (ADR-060 decision 14).

| Check | Fields | Source |
|---|---|---|
| 1 | `manifestVersion`; `requires` and `requires.pluginApi`; `runtimes`, for its in-process clause and for `wasm`; every field, for the rules of decision 3, as question Q4 decides | `08:497`; ADR-060 decision 58 |
| 2 | `requires.definitions` | `08:498` |
| 3, from S2b | `requires.remoteProtocol`; the manifest digest | `08:499`; `07:1886` |
| 4 | `stability` | `08:500` |
| 5 | `id`, `runtimes`; `id` for the reserved owners (decision 14) | `08:501` |
| 7 | `provides`, `uses`, `hooks` | `08:503` |
| 8 | `id` and `instantiable` of the plugin a row names; from S4a, `scope` and `uses` for a `scope: tenant` plugin that injects the process form of a bound key; from S4b, `scope` and the fields of the tenant-toggleable test of `07:141`, at least `registers` and `hooks` (decision 20) | `08:504` |
| 9, from S2b | `scope`, `runtimes`, `provides`, `registers`, `hooks`, and `uses`, since the contribution set includes the callbacks a remote plugin calls (`08:389`) | `08:505` |
| 10 | `requires.definitions`; `provides` and `registers`, which name the definitions whose suite revision it asks for; `runtimes`, which tells a pack, whose attestation names a scenario version | `08:506`; ADR-060 decision 56 |
| The check that ADR-051 names, from S8a | `registers`, for a `harnesses` contribution; `uses`; `capabilities.network`; `runtimes` | `07:1895`; harness:58 |

Outside the gate, install reads `id`, for the group its rows mount under and the content
directory (`08:391`; `07:1328`); `version`, for `plugins.lock` (`07:122`); and
`runtimes`, for whether it answers `rebuild-required`, renders a deployment unit or
unpacks content (`07:129`; `08:395-397`). Install reads `publisher` through check 1,
since `publisher.id` against the owner is a rule of decision 3 (`07:125`).

### `scope`

17. **`scope`** takes the form that question Q2 decides. Whatever the answer:
    - its values are `process` and `tenant`; whether "both" is a third value or the set
      of the two, and what it means for a row, is question Q2 (`00:207`; `07:128`);
    - `zz_tenants_test.go` is written for a plugin whose scope holds `process`
      (`07:1435`), and step 4 mounts a plugin whose scope holds `tenant` in two trees
      (`07:1399`);
    - no tenant tree exists before S4a (`08:697`), so step 4 cannot mount one. As ADR-060
      decision 43 does for the two-tenant test, step 4 skips the two-tree mount of such a
      plugin with "not yet (S4a)", never counts it as a pass and prints it, and the
      attestation lists it beside the generated tests that did not run (ADR-060 decision
      18). `plugin check` does not refuse the manifest;
    - the S2a reader fixes the form only.

### What changes in the code at S2a

18. **From the S1 readers to schema 1.0:**
    - the five manifests under `plugins/` get their generated half from `plugingen` and
      lose `needs` (ADR-060 decision 44);
    - every other plugin of a module table gets a `plugin.yaml` in its own directory: the
      loader in `loader/plugin.yaml`, beside `loader/loader.go`, with its hand-written
      half written by hand and its generated half written from its `kernel.Plugin` value
      (ADR-060 decision 13); the plugins of the test release in `examples/<name>/`; and
      the plugins of `test/dist` in `test/dist/plugins/<name>/` (ADR-060 decision 28).
      The packages `toy` and `fault` of `test/dist/plugins`, which define three and seven
      plugins at `8e1f7b9`, are split one plugin per directory in the commit that
      generates their manifests, and `aiops-test/toy-consumer` moves to `plugin.Define`
      (decision 9);
    - `boot.ModuleEntry` is filled from the manifest: `Provides`, `Needs` and `Wants` from
      `provides` and `uses`, `Hooks` from `hooks` with the ordinal from `serves`, and the
      volatile paths from `configSchema`. The hand lists in `dist.Module.Volatile` and
      `boot.ModuleEntry.Volatile` go (ADR-060 decision 4). `Classes` come from served
      data (ADR-060 decision 14), and `modules.yaml` goes with the S1 `dist.yaml`
      (ADR-060 decision 20);
    - hook entries are keyed by hook and stage, not by hook alone;
    - the reader, with the SPDX expression parser and the JSON Pointer of decision 4,
      adds to `boot` under its provisional cap (ADR-060 decision 35).
19. **No new linked module.**
    - Both modes read with `go.yaml.in/yaml/v3` at the version `go.mod` pins, `v3.0.6`
      (ADR-060 decisions 6 and 41). The `LICENSE` file of `go.yaml.in/yaml/v3@v3.0.6`,
      read from the module at that version for this ADR, puts eight files ported from
      libyaml (`apic.go`, `emitterc.go`, `parserc.go`, `readerc.go`, `scannerc.go`,
      `writerc.go`, `yamlh.go`, `yamlprivateh.go`) under MIT, Copyright 2006-2010 and
      2006-2011 Kirill Simonov, and the rest under Apache-2.0, Copyright 2011-2019 Canonical Ltd. The
      module also carries a `NOTICE` file, "Copyright 2011-2016 Canonical Ltd." with the
      Apache-2.0 header, which every artifact that links the module carries with both
      licence texts. Its `go.mod` requires nothing. This matches licence:44, whose usage
      column says "imported by `boot/` only"; since ADR-060 decision 41 the module is
      also imported by `plugingen`, the builder and the operations plugin, and that
      column is updated under ADR-060 decision 34 with those importers.
    - The SPDX expression parser that decision 3 and step 1 use is written in `boot` with
      the standard library, after the license expression grammar of the SPDX
      specification; no module is added for it.
    - The SPDX list data of step 1 is SPDX's published license list data, embedded in
      `aiopsctl` (ADR-060 decision 23). Its release is pinned when it is read, and its
      licence is UNVERIFIED until it is read at a primary source at that release, under
      ADR-060 decision 34, before it merges. Data is not linked code; ADR-060 decision 34
      covers it as an embedded data set.
    - The schema derivation, its validator and the JSON Pointer use the standard library
      (ADR-060 decision 4).

### What stays open

20. Not settled here:
    - index format 1.0, the S6a part of ADR-048 (`08:1179`), with the index rule on a
      driven process that is not under an open licence (`07:131`; licence:241);
    - the Python parts: every `plugin.toml` key other than `entrypoint`, whether
      `plugin.yaml` carries `entrypoint`, and how `@contributes` names a key of a
      definition that declares several registry keys, at S2b (`07:66` R8, `:143`;
      decision 8);
    - the entry shape of `durable`, at S3 (decision 11);
    - how a row of a plugin that may run at either scope takes one, at S4a (question Q2);
    - the tenant-toggleable test over the generated half (`07:141`), with layer 5 at
      S4b, where the overlay part of check 8 needs it (ADR-060 decision 29);
    - the stage vocabulary per definition, ADR-043 at S5 (ADR-060 decision 7);
    - the check that carries the refusal of a `harnesses` contributor, ADR-051 at S8a
      (`07:1895`);
    - how the strict reading reaches the `plugin check` row, with the `commands`
      definition (ADR-060 decision 53; decision 15);
    - the media type of `plugin.yaml` and its place in an artifact, ADR-047 (ADR-060
      decision 33).

## Consequences

- Every field of schema 1.0 has one form, one half and a known reader, and the table of
  decision 16 names the fields each check reads.
- `plugin check` and the gate share one reader in `boot` once the `commands` ADR hands
  that reader to the `plugin check` row; they then differ only in what the mode says about
  unknown fields and in the lints of step 1 that need other data.
- The gate reads `registers` from S2a, through check 10, so question Q1 is answered before
  check 10 merges; `plugingen` and the parity check need it before the first generated
  manifest merges.
- The reserved owners hold for every kind of artifact: the builder for Go modules, check
  5 for a pack and for remote code, and step 1 for authoring.
- The commit that builds the reader edits the sketches and lines that the decisions
  replace, with the edits that the answers below name: `07:99` and `:1675`, whose network
  entries become `config:/url` and `config:/endpoint`; `07:109`, where `registers` names
  keys; `07:111`, where `configSchema` is inline; `07:115` and `:1686` and community:134,
  where each definition takes a list of `"major.minor"`; and community:180 (decision 4).
- The commit that builds the one gate text (ADR-060 decision 16) adds the reserved-owner
  clause of decision 14 to check 5 in `08:501` and `07:1888`. On acceptance, ADR-060
  decision 9 gains the kernel part of `serves` (decision 12).
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
The `Spec` has no field for it (`07:215-223`; ADR-060 decision 4), a `Need` on a registry
key reads the same for a consumer and a contributor, and the gate and the
tenant-toggleable test read it without running code (ADR-060 decision 11; `07:141`).
Check 10 reads it from S2a (decision 16), check 9 from S2b. A Python author declares it
with `@contributes(definition)` (`07:1201`). In the dsh model, `provide` metadata is
optional, and registering with a registry is a call that nothing declares; the plan made
`Provides` mandatory so that the composer analyses the graph without running code (dsh
row 6, `08:48`; plugin record §15), and every departure from the dsh model is recorded
there with a reason forced by a server with several tenants and several replicas, never
by schedule (`08:39`).
- (a) A `Spec` field, `Registers []kernel.AnyKey`, which the author fills as `Provides`
  is filled. `plugingen` writes `registers` from it. The generated cycle test fails when a
  fake's `Inventory()` holds a registration of the plugin's fiber in a registry the field
  does not list (`07:519`), so it catches an unlisted registration made under the config
  and on the paths that the test runs; a registration made under another config, in a
  `When` body the test does not reach or later from a listener is not seen. From S2b the
  proxy row refuses a remote `Register` outside the list. In process nothing refuses an
  undeclared `Register` at run time, since the field is `Spec` data that `kernel.Plugin`
  does not carry. No kernel change: the field is `Spec` data, as `Durable` and the config
  type already are for `plugingen` (ADR-060 decision 11). It edits `00:44` ("Needs ...,
  provisions, hooks and durable namespaces are declared as data" gains contributions),
  `00:207` (the fields of the `Spec`), `07:215-223`, `07:235`, `07:1434` (the cycle test
  gains the assertion), plugin record §4, the fields of `Spec` in ADR-060 decision 4, and
  the dsh checklist: row 6 of plugin record §15 and of `08:48` stays adapted and gains
  "contributions are declared in `Spec.Registers`, so the gate and the tenant-toggleable
  test read them without running code", a reason forced by tenants (`07:141`) and by
  remote contributions (check 9).
- (b) A dependency form `kernel.Contribute(k)` beside `Need` and `Want`, a `Need` that
  also marks a contribution, so the declaration is part of `Inject`, is carried by the
  `kernel.Plugin` value and also covers a plugin built with `kernel.Define`. A registry
  could then refuse an undeclared registration in process, with a kernel accessor that is
  part of the same addition. It is a kernel change, proposed as an ADR-034 addition and
  verified as ADR-060 decision 3 requires. It edits `00:44`, `07:187-189`, plugin record
  §4, and row 8 of the dsh checklist (plugin record §15; `08:50`), which becomes adapted
  with the same reason as in (a).
- (c) `plugingen` finds the `Register` calls by static analysis of the code reachable
  from `Apply`. Nothing an author writes changes and the dsh checklist keeps its rows, but
  a registration made through a helper in another module, an interface or function value,
  or reflection is missed; check 10 then asks no suite revision for that registry, and
  check 9 reads a short list.
- Recommended: (a). It is the adaptation dsh row 6 already made for `Provides`, recorded
  in the checklist with its reason, it is the Go counterpart of `@contributes`, which
  `aiops-sdk manifest` maps to the same keys (decision 8), and it needs no kernel change,
  which ADR-060 decision 3 plans for. The cycle test catches an omission on the paths it
  runs, and the proxy refuses one from S2b. Needed before `plugin.Define` and `plugingen`
  merge.
- **Answered 2026-11-23: (a)**, decision 21.

**Q2. Is `scope` a list or one value, and what is "both"?** `00:207`, `07:128` and plugin
record §4 say "`process`, `tenant` or both". `07:94`, `07:153`, `07:1673` and the five S1
manifests write a list; `07:477-478`, `08:504` and plugin record §4 and §10 write `scope:
process` and `scope: tenant`, and call process the default. Nothing defines "both", and
nothing says how a row of such a plugin takes one scope: `tenant-trees` mounts the rows of
tenant scope (plugin record §10; `00:329`) and lands at S4a (`08:697`).
- (a) A set: `[process]`, `[tenant]` or `[process, tenant]`, the last being "both", a
  plugin written to run at either scope; required in every manifest; the templates write
  `[process]`, the default of `07:477`. A prose `scope: tenant` reads as a scope that
  holds `tenant`. How a row of a "both" plugin takes one scope is settled by the scope
  ADR of S4a, with `tenant-trees`; until then such a row mounts at process scope, the
  only scope that exists. It edits nothing in `00`; the comment of `07:94` becomes "a set of
  process and tenant".
- (b) One string: `process`, `tenant` or `both`, required, with the same meaning of
  `both` and the same S4a row rule as (a). It edits `07:94`, `07:153`, `07:1673` and the
  five S1 manifests.
- (c) One string, `process` or `tenant`, and no "both": a plugin that runs at both scopes
  ships as two plugins. It edits `00:207`, `07:128` and plugin record §4.
- Recommended: (a). It is the form `07:94`, `07:153`, `07:1673` and the S1 manifests
  already write, "both" needs no third word, it reads like `runtimes`, and `00` stays as
  written. A required field also spares the gate a default it would have to know. The row
  rule is needed only when a second scope exists. Needed before the manifest type of the
  `plugin` package merges.
- **Answered 2026-11-23: (a)**, decision 22.

**Q3. How does step 1 of `plugin check` tell the release pipeline from anyone else?**
Step 1 fails on "a reserved owner outside the release pipeline" (`07:1567`). CI runs the
check on every plugin under `plugins/` and `examples/`, owner `aiops` (ADR-060 decision
32; decision 14); a contributor runs it on a plugin that `plugin new --contrib` wrote,
owner `contrib` (`08:407`; `07:1526`); and nothing in a plugin directory says who built
it. The reservation itself is enforced by the builder for Go modules and by check 5 for
an artifact with no module (decision 14).
- (a) By where the code is. An `aiops` id passes only in a module whose path is the root
  module path of this repository, which admits `plugins/`, `loader/` and `examples/`, and
  a `contrib` id only in a module whose path is under its `contrib/` path or, for a pack
  with no code, in a directory under `contrib/` below the root module's `go.mod`. `plugin
  check` reads both from the `go.mod` files it already reads. A copy that claims those
  paths passes the lint and is stopped where the reservation is enforced: the builder
  (ADR-060 decision 22) and check 5 (decision 14).
- (b) By a flag. Step 1 fails on a reserved owner unless `plugin check --release` runs,
  which the release pipeline, `make check` and this repository's CI pass. A contributor
  who runs `plugin check` in `contrib/<name>` without it gets a failure that is not one.
- (c) Step 1 checks no owner, and `07:1567` loses the clause; the builder, check 5 and
  the index carry the reservation. It removes a written check and is listed to be
  refused.
- Recommended: (a). The clause of `07:1567` stays and is decided from files the check
  already reads, with no flag to forget or pass by mistake, and the security of the
  reservation stays where a signature or the release build decides it: the builder for a
  Go module, the release-pipeline signer of check 5 for a pack or remote code. Needed
  before step 1 of `plugin check` merges.
- **Answered 2026-11-23: (a)**, decision 23.

**Q4. What does the gate do with an unknown field at a minor it knows, and with a known
field that is absent or breaks a rule of decision 3?** A reader ignores unknown fields of
a higher minor (`07:120`; community:149). Check 1 denies an absent `manifestVersion`, an
unknown major, an absent `requires`, an unserved `pluginApi` and `wasm` (`08:497`); no
clause denies a manifest whose `id`, `stability` or `uses` is absent or of the wrong type,
or whose `publisher.id` differs from its owner, and a gate that reads one must deny or
guess. community:167 lists `publisher.id` among the rules the gate checks, and `07:125`
names install among its readers. A manifest that passed `plugin check` has neither
defect, so both reach the gate only from a manifest made or edited without it, as the copy
of exit step D1 is (ADR-060 decision 30).
- (a) At a minor the reader knows, an unknown field makes the manifest malformed. Check 1
  denies a malformed manifest: a required field absent, a known field of the wrong type
  or value, a rule of decision 3 broken, or an unknown field at a known minor. Unknown
  fields are ignored at a higher minor only, as `07:120` says. Check 1 of the one gate
  text (`08:497`; ADR-060 decision 16) gains that clause.
- (b) The gate ignores unknown fields at every minor, and check 1 denies only an absent
  required field, a malformed known field or a rule of decision 3 broken. `08:497` gains
  that narrower clause.
- (c) The gate reads as strictly as `plugin check`. Every binary then denies a plugin
  built against a later minor, which undoes frozen-additive (`07:120`; `00:207`); listed
  to be refused.
- Recommended: (a). It is `07:120` as written: the leniency is the one that frozen-additive
  needs, and a manifest that no generator writes is denied by a named check rather than
  admitted on a guess. It also gives install its reading of `publisher.id` (`07:125`;
  community:167). Exit step D1 is unaffected, since its copy is well formed and check 2
  alone denies it. Needed before gate check 1 merges.
- **Answered 2026-11-23: (a)**, decision 24.

## Owner answers of 2026-11-23

The project owner accepted the S2a part of this ADR on 2026-11-23 and answered every
question of the section "Questions for the owner" on the same date, each with its
recommended option. Each decision below names the question it answers. Where a decision
above leaves a point to a question, the decision here settles it.

21. **`registers` comes from a `Spec` field, `Registers []kernel.AnyKey`** (Q1 (a)),
    which the author fills as `Provides` is filled; `plugingen` writes `registers` from
    it (decision 8). The generated cycle test fails when a fake's `Inventory()` holds a
    registration of the plugin's fiber in a registry the field does not list (`07:519`).
    It sees a registration made under the config and on the paths that the test runs,
    not one made under another config, in a `When` body the test does not reach or later
    from a listener. From S2b the proxy row refuses a remote `Register` outside the list.
    In process nothing refuses an undeclared `Register` at run time, since the field is
    `Spec` data that `kernel.Plugin` does not carry. The kernel does not change. Row 6 of
    the dsh checklist stays adapted and gains "contributions are declared in
    `Spec.Registers`, so the gate and the tenant-toggleable test read them without
    running code", a reason forced by tenants (`07:141`) and by remote contributions
    (check 9).

22. **`scope` is a set** (Q2 (a)): `[process]`, `[tenant]` or `[process, tenant]`, the
    last being "both", a plugin written to run at either scope. It is required in every
    manifest, and the templates write `[process]`, the default of `07:477`. A prose
    `scope: tenant` reads as a scope that holds `tenant`. How a row of a "both" plugin
    takes one scope is settled by the scope ADR of S4a, with `tenant-trees` (decision
    20); until then such a row mounts at process scope, the only scope that exists. `00`
    is not edited, and the comment of `07:94` becomes "a set of process and tenant".

23. **Step 1 of `plugin check` decides a reserved owner by where the code is** (Q3 (a)).
    An `aiops` id passes only in a module whose path is the root module path of this
    repository, which admits `plugins/`, `loader/` and `examples/`. A `contrib` id passes
    only in a module whose path is under its `contrib/` path or, for a pack with no code,
    in a directory under `contrib/` below the root module's `go.mod`. `plugin check`
    reads both from the `go.mod` files it already reads. A copy that claims those paths
    passes the lint and is stopped where the reservation is enforced: the builder
    (ADR-060 decision 22) and check 5 (decision 14). The clause of `07:1567` stays as
    written.

24. **Check 1 denies a malformed manifest** (Q4 (a)). At a minor the reader knows, an
    unknown field makes the manifest malformed. Malformed means a required field absent,
    a known field of the wrong type or value, a rule of decision 3 broken, or an unknown
    field at a known minor. Unknown fields are ignored at a higher minor only, as
    `07:120` says. Check 1 of the one gate text (`08:497`; ADR-060 decision 16) gains
    that clause.

**Edits made with this record.** `00` is edited in the commit that records the answer
(`08:996`), and that commit makes the edits the chosen options name: for decision 21,
`00:44` and `:207`, `07:215-223`, `:235` and `:1434`, the `Spec` of plugin record §4,
the fields of `Spec` in ADR-060 decision 4, and row 6 of the dsh checklist in plugin
record §15 and in `08:48`; for decision 22, the comment of `07:94`; for decision 24,
check 1 in `08:497`, the check of `08` §1.6 that ADR-060 decision 16 makes the one gate
text. Decision 23 edits no line. On acceptance ADR-060 decision 9 gains the kernel part
of `serves` (decision 12). The lines that restate an edited text are edited with it: the
declared needs and provisions of a plugin in `00:684` and in DR-01 of `08` §3
(`08:1001`); the assertions of the cycle test in plugin record §14 and in ADR-060
decision 12; ADR-060 decision 11, which said that the `Spec` has no field for
`registers`; and the worked contributions, which now list their registry in
`Registers`: `07:309-316` and `:1747-1753`, the example of plugin record §4, and the
`plugin.go` row of the Go template in `07:1553`. Left to later commits: check 1 in
`07:1884` and in plugin record §12, to the commit that builds the one gate text (ADR-060
decision 16); the sketches and lines that the Consequences give to the commit that
builds the reader; and the row of ADR-048 in the ADR table of `08` §3, to the commit
that updates that table. An option the owner did not choose edits nothing.
