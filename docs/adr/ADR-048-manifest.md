# ADR-048: Manifest schema 1.0 (the S2a part)

- Status: **accepted by the project owner: the S2a part on 2026-11-23, the S2b part on
  2026-11-27.** The owner answered the questions of each part on its date with the
  recommended option of each: "Owner answers of 2026-11-23" records decisions 21 to 24,
  "Owner answers of 2026-11-27" decisions 43 to 47. The S6a part (Scope) and the points
  of decisions 20 and 42 stay open. Where an answer changes a line of `00-decisions.md`,
  the question names it, and `00` is edited in the commit that records it (`08:996`).
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

## 2026-11-27 proposed addition: the S2b part

- Status: **accepted 2026-11-27 by the project owner, for the S2b part.** The owner
  answered questions Q5 to Q9 on the same date with the recommended option of each; the
  section "Owner answers of 2026-11-27" records them as decisions 43 to 47. The S2a part
  above stands as accepted. `00` is edited in the commit that records the answers
  (`08:999`), and the same commit records the S2b part in the row of this ADR at `00:786`.
- Scope: the S2b part of ADR-048. Decision 20 left the Python parts to S2b, ADR-063
  decision 2 has each S2b part written as a dated addition to its ADR, and ADR-063
  decision 34 lists what this one settles. The rows of this ADR at `00:786` and `08:1182`
  name an S2a part and an S6a part and no S2b part, and both gain it on acceptance
  (Consequences). The addition is proposed before the code it governs and accepted before
  that code merges, and it keeps every written requirement it touches (ADR-063 decision
  2). The code it governs is `aiops-sdk manifest` and the Python template, the Python
  output of the adapter generator as far as decision 31 binds it, the `remoteProtocol`
  output of `plugingen`, the contribution-set clause of gate check 9 and the unit renderer
  as far as decisions 28 to 30 bind it. The S6a part, index format 1.0, stays open.
- Citations: `00`, `07` and `08` are the files of `docs/architecture/` with those numbers,
  and in this addition `07:148` is line 148 of `07` at commit `bbfd839`. A path with a
  line, as `boot/manifest.go:188`, is a file of the repository at the same commit. The S2a
  part above cites `8e1f7b9`: `07:129`, `:137` and `:143` there are `07:134`, `:142` and
  `:148` here, `07:1201` and `:1567` there are `07:1208` and `:1574` here, and `08:389`,
  `:497` and `:505` there are `08:391`, `:499` and `:507` here. ADR-063 cites `50665fa`:
  the lines of `00` and `07` cited here did not move since, and `08:1263` there is
  `08:1264` here. A line of this file is cited as `ADR-048:433`, as ADR-063 cites it; the
  addition is appended, so no line above it moves. "community" is
  `docs/design/community-first-2026-11.md`, and the plugin record,
  `docs/design/plugin-system-2026-11.md`, is cited by section. RFC 1035 is cited by
  section. The decisions continue the numbering of this ADR from 25, and the questions
  from Q5. This addition states no licence fact: no licence was read for it, and decision
  26 names what is still to be read.

### Context of the S2b part

Decision 20 left the Python parts to S2b, and ADR-063 decision 34 lists what this addition
settles: every `plugin.toml` key; whether `plugin.yaml` carries `entrypoint`, with the
closed grammar of that field and of the id as a deployment unit takes them (ADR-063
decision 25); how `@contributes` names a key; where `configSchema`, `uses` and the hook
stages come from in Python, and which library, if any, derives the schema; the constant of
spoken majors that `plugingen` reads; what a manifest that lists `inproc` and `remote`
packs and installs; and which fields the contribution-set clause of check 9 reads beyond
those of ADR-048:433.

Reading the sources against each other and against the code at `bbfd839` shows:

- **What S2a built.** The reader takes `runtimes: [remote]` and asks for
  `requires.remoteProtocol` exactly then, with at least one major (`boot/manifest.go:188`,
  `:499-518`; decision 12). `plugin check`, `plugin pack` and `plugin install` refuse
  `remote` (`plugins/operations/internal/operations/author.go:248-249`,
  `artifact.go:90-91`, `install.go:122-123`). No generator writes `remoteProtocol`, and
  `plugin.Generate` writes `pluginApi` for every value (`sdk/plugin/manifest.go:154`).
- **`entrypoint`.** It "is what `aiopsctl plugin check` and the deployment unit start"
  (`07:148`), and the sketch writes it as one string (`07:160`). Plugin record §14 has
  `aiopsctl` start the plugin "as a local process from the `entrypoint` of `plugin.toml`".
  `00:207` lists the hand-written fields of `plugin.yaml` without it, and decision 9 reads
  that list as closed. An artifact carries `plugin.yaml` and no `plugin.toml` (`08:393`),
  and install renders the unit from the artifact with three inputs, the third being the
  `entrypoint` "if the S2b part of ADR-048 keeps one in `plugin.yaml`" (ADR-063 decision
  25). `aiopsctl` parses no Python (ADR-063 decision 17) and links no reader of TOML.
- **Two starts of one string.** Step 4 of `plugin check` starts the entrypoint as a local
  process on the author's machine, with no container runtime, in every language
  (`07:1570`, `:1577`; ADR-063 decision 17; ADR-063 decision 54, reading 5). The unit
  starts it inside the plugin's image, and the image of a remote Go plugin is one static
  binary on no base layer (ADR-063 decisions 19 and 25). No line says how the first word
  is found in either place, in which directory either process starts, or whether the
  unit's words replace the command that the image configures.
- **What reaches a unit.** Nothing of the artifact reaches the unit but its three inputs:
  no mount, no variable of the environment, no port, no user, no capability and no network
  name (ADR-063 decision 25). The systemd form runs the image through the container
  runtime of the host (ADR-063 decision 50), whose command line takes options of its own.
- **The declarations of a Python plugin.** `aiops-sdk manifest` "reads `plugin.toml` and
  the decorators" (`07:79`). The decorators name a definition, and a hook with a stage,
  and are written `@contributes(definition)` and `@listens(hook, stage)` in `07:1208-1209`
  and in plugin record §4. No part of the SDK (`07:1205-1213`) names a callback that a
  contribution calls or a config type, while the sketch reads a secret through the call
  context (`07:1227`) and `configSchema` is "derived from the config type" (`07:142`).
  ADR-063 decision 4 admits no departure from the plugin record without the owner's word.
- **The objects that a declaration can name.** The adapter generator writes the Python
  module of each definition marked `contrib` (ADR-063 decisions 13 and 18; `07:1203`,
  `:1207`). A callback belongs to a definition marked `callback`, and a remote hook may
  belong to any definition. At `bbfd839` every `definition.yaml` under `sdk/def` says
  `remote: "no"`, for the definition and for each of its hooks; S2b marks the two toy
  registries `contrib` (ADR-063 decision 14), neither of which owns a hook, and proves the
  callback path and the remote listener path on one test definition of which nothing ships
  in a release table (ADR-063 decision 51).
- **The names of the Python parts.** R8 gives "the method names of the Python base classes
  and of the test fixture" to this ADR, "the Python parts at S2b" (`07:66`). The sketch
  already shows names that differ from the Go ones: `describe`, `invoke`,
  `run_conformance` and `calls` (`07:1223-1243`).
- **The schema keywords.** `configSchema` uses only the keywords of the Go derivation
  (decision 9), and the validator of the `plugin` package refuses any other keyword
  (`sdk/plugin/validate.go:35-40`). It is the validator that step 3 of `plugin check` runs
  on a Python plugin, on the config examples of `testdata/config/*.json` (`07:1576`;
  ADR-063 decision 54, reading 6).
- **The registry key in `uses`.** `plugingen` writes one `uses` entry per dependency of
  `Spec.Inject` (decision 6), and a Go contribution injects the registry it registers with
  (`templates/echoers/plugin.go.tmpl:25-26`). Step G1 of the S2b exit takes that `Plugin`
  value, unchanged, as remote code through step 7 of `plugin check`, which runs the gate
  (ADR-063 decisions 16 and 27). Check 9 denies "a `uses` entry of a key that is not
  marked `callback`, a bound key included" (ADR-063 decision 28; decision 10 there), and
  `echoers` is to be marked `contrib` (ADR-063 decision 14).
- **The spoken majors.** Both sides send "the list of majors they speak" (`07:1127`;
  `sdk/remote/handshake/handshake.proto:16`), and step D1 of the exit builds a plugin
  against a version of the remote modules "in which the one list of spoken majors holds 7
  alone" (ADR-063 decision 27). No Go is generated from the handshake file yet, and no
  such list exists.
- **Two runtimes in one manifest.** `runtimes` is a set (decision 4), `requires` then
  holds both `pluginApi` and `remoteProtocol` (decision 12), and `plugin check` runs T1
  and T2 in every runtime listed (`07:134`). An artifact carries an image digest for
  remote code or a module path, version and sum for in-process code (`08:393`), boot
  resolves a row in the module table or in `plugins.lock` (ADR-063 decision 12), and a
  site holds one digest per plugin id (ADR-047 decision 7). No exit step uses such a
  manifest (ADR-063 decision 34), and no manifest of the repository lists `remote`.
- **The marks check 9 reads.** A definition carries `remote: contrib`, `callback` or `no`,
  one value for the whole definition (`07:538`; `tools/deflint/main.go:335`), which
  `serves` gains per served definition (ADR-063 decision 46). A hook carries `remote: yes`
  or `no`, which `serves` holds since S2a (`tools/deflint/main.go:395`;
  `boot/distribution.go:105-109`). `serves` also holds the provider class and the consumer
  class of each key (`boot/distribution.go:100-101`), and one definition may declare
  several keys of different classes, as `launch` declares six, two of them consumer-bound
  (`sdk/def/launch/v0/definition.yaml`).
- **The id.** It is two kebab-case segments with no bound on their length
  (`boot/manifest.go:580-590`), while boot's reader bounds an identifier of a layer at 128
  bytes (`boot/parse.go:301-303`). Each segment is already a path component under the home
  (ADR-047 decision 13). The endpoint of an instance is "a name that the runtime resolves"
  on the instance's network (ADR-063 decision 27), the certificate of an instance names
  the plugin id (ADR-063 decision 55), and the unit, the network and the credential
  directory of one instance must never be another's (ADR-063 decisions 23 to 25).
- **What the accepted part says of S2b.** Decision 1 says that the manifest digest of
  `plugins.lock` and of the handshake names `plugin.yaml` (ADR-048:111-113), while the
  lock holds the digest of the artifact's OCI manifest (ADR-047 decision 8); ADR-063
  decision 58 settles the two. The table of decision 16 does not yet list `runtimes` for
  check 8, which reads it from S2b for the one row of a remote bundle (`08:506`; ADR-063
  decision 67), and its install paragraph (ADR-048:437-441) lists neither
  `capabilities.network`, which install reads to refuse an entry (ADR-063 decision 56),
  nor `entrypoint`.
- **The receiving documents.** At `bbfd839` no file `docs/adr/ADR-041*` exists, and
  ADR-020, ADR-044, ADR-047 and ADR-061 hold no S2b part. A point that this addition
  leaves to one of them cannot be checked as taken up, and several such points are in no
  list of ADR-063 decision 34.

### Decision of the S2b part

#### `plugin.toml`

25. **`plugin.toml` is the hand-written half of a Python plugin** (`07:148`, `:151`): one
    TOML document in the plugin directory, beside the `plugin.yaml` that `aiops-sdk
    manifest` writes from it. Its keys are the hand-written fields of decision 4, each
    under the same name and with the same form and rules, the key `entrypoint`, and one
    table that only the generator reads. The table under this decision lists them all.
    - `instantiable` is not a key. A Python plugin is always remote (`07:1203`), and check
      8 reads `instantiable` from a manifest of the module table only (`08:506`), so the
      key would have no reader. `aiops-sdk manifest` writes no `instantiable`, an absent
      one reads as `false` (decision 4), and decision 39 gives a remote manifest written
      in Go the same rule.
    - A key, a table or a TOML form outside that table is refused by `aiops-sdk manifest`,
      as the strict reading refuses an unknown field (`07:125`; decision 15).
    - An artifact carries `plugin.yaml` and no `plugin.toml` (`08:393`), so install, the
      gate and boot never read the file.
    - The Python template (`07:1213`; ADR-063 decision 19) writes a `plugin.toml` with the
      required keys, `scope = ["process"]`, `runtimes = ["remote"]`, an `entrypoint` and
      the `[python]` table, and no `[capabilities]`. It writes no `plugin.yaml`:
      `aiops-sdk manifest` writes it, as step P1 of the exit has it (ADR-063 decision 27).

| Key of `plugin.toml` | TOML form | Rule | Source |
|---|---|---|---|
| `manifestVersion`, `id`, `version`, `title`, `description`, `license`, `source`, `stability` | A string each | Decisions 2 and 4; decision 29 for the id | `07:152-157` |
| `scope` | An array of strings | Decision 22. The template writes `["process"]` | `07:158`; ADR-063 decision 54, reading 1 |
| `runtimes` | An array of strings | Exactly `["remote"]`: a Python plugin is always remote | `07:159`, `:1203` |
| `entrypoint` | A string | Required; decision 28 | `07:148`, `:160` |
| `[publisher]` | A table of `id`, `name` and `url` | Decision 4 | `07:162-163` |
| `[support]` | A table of `url` and `email` | Decision 4 | `07:131` |
| `[capabilities]` | A table of `network` and `secrets`, each an array of strings | Decision 4. Install refuses a remote manifest that lists a `network` entry until the allow-list is built (ADR-063 decision 56) | `07:135`, `:165-167` |
| `[[integrates]]` | An array of tables of `system`, `versions`, `tested`, `credential` and `license` | Decision 5 | `07:136` |
| `[python]` | A table with the key `module`, a string, and with question Q9 (b) the three keys that option names | Decision 26. Written to no file | `07:79` |

26. **`aiops-sdk manifest` writes both halves of `plugin.yaml`** from `plugin.toml` and
    the decorators (`07:79`, `:1210`).
    - **The module.** `[python] module` is the dotted name of the one module that the
      command imports to read the decorators. The module must import without starting the
      plugin: it calls `serve()` only when it is run as the program, as the sketch does
      (`07:1231-1232`). The command runs no `serve()` and opens no connection.
    - **The whole file.** Decision 1 words the rewrite for a file whose hand-written half
      lives in `plugin.yaml`. A Python plugin keeps that half in `plugin.toml` and never
      writes `plugin.yaml` (`07:148`), so the command writes the whole file: the
      hand-written fields as `plugin.toml` holds them, in the order of the table of
      decision 4, and then the seven generated fields in the order of decision 1. A
      comment lives in `plugin.toml`.
    - **What it checks, and what it does not.** The command checks the TOML form of each
      key of decision 25, the value of `runtimes`, which decides the shape of the
      generated half, and what the decorators declare (decisions 31 to 36). It checks no
      rule of the schema. It copies `id`, `entrypoint`, `license` and every other
      hand-written value as the string or the list that `plugin.toml` holds, and the
      encoding of decision 27 can write any string. Step 1 of `plugin check` then reads
      the written file with the one reader (decision 15; ADR-061 decision 22), which holds
      the only copy of the id rules, of the grammar of decision 28 and of every other rule
      of decision 3. A `plugin.toml` whose `entrypoint` is outside the grammar therefore
      gives a `plugin.yaml` that step 1 refuses, and no second copy of the grammar exists
      in Python.
    - **How `aiopsctl` knows a Python plugin.** A plugin directory that holds a file named
      `plugin.toml` is the directory of a Python plugin. `aiopsctl` tests for the file,
      reads no key of it and parses no Python (ADR-063 decision 17). A Go plugin and a
      pack hold no file of that name, and `plugin new` writes one only with `--lang
      python`. In such a directory step 2 of `plugin check` compares with `aiops-sdk
      manifest` and never with `plugingen` (`07:1575`), and `plugin check --write` runs
      the command, which writes the whole `plugin.yaml`. It writes no generated test: the
      two generated tests belong to a Go plugin (`07:1563`).
    - **Parity over both halves.** Decision 1 compares the seven generated fields. For a
      Python plugin this addition adds a rule to it: step 2 has the command produce the
      document again, reads both documents with the one reader and fails on a difference
      in any field, hand-written or generated, since the command writes both halves and no
      hand edits the file (`07:148`, `:1575`; `08:1264`; `00:299`).
    - **No library, and what that rests on.** The command takes no library for TOML, for
      YAML or for the schema (decision 35). It reads `plugin.toml` with the TOML reader of
      the interpreter's standard library and writes the closed shape of `plugin.yaml`
      itself (decision 27), which ADR-063 decision 35 foresees. Two things carry that, and
      neither is settled here. First, ADR-041 sets the supported interpreter versions
      (ADR-063 decision 34), and this addition asks it for a lowest version whose standard
      library reads TOML, which ADR-063 decision 35 puts at 3.11. The wheels of `grpcio`,
      `protobuf` and `pytest` that ADR-063 read ask for 3.10 or later, and at a floor of
      3.10 the command would need a TOML library, read first under that decision; `tomli`,
      which `pytest` requires below 3.11, is among the packages it lists as unread.
      Second, the interpreter itself is among what that decision lists as not read. Its
      licence is read at a primary source at the pinned version before `aiops-sdk
      manifest` merges. No licence was read for this addition.
27. **The bytes of the written file** are fixed, because the first message of the protocol
    carries their digest (ADR-063 decision 58) and because boot's reader types every plain
    scalar (decision 3; `boot/parse.go:33-49`).
    - **One layout.** The file is one YAML document in UTF-8 that holds ASCII alone, with
      LF line ends, no byte order mark, no document marker and one LF after the last line.
      Its first line is a comment that names the command and `plugin.toml`, and it holds
      no other comment. The fields stand in the order of decision 26. Every mapping and
      every list is in block style, with one key or one entry to a line and each nested
      level indented by two spaces. An empty list is `[]` and an empty mapping is `{}`.
      The file holds no other flow form, no anchor, alias or tag, no block scalar and no
      folded line: a scalar stands on the line of its key or of its list entry.
    - **No plain string.** A key that the schema names is written plain. Every key that is
      data, a definition name under `requires.definitions` or a key inside `configSchema`,
      and every string value are written double-quoted, with `\"` for the quote, `\\` for
      the backslash and `\u` with four hexadecimal digits, or `\U` with eight, for each
      character outside the printable ASCII range U+0020 to U+007E. No string is ever
      plain, so the reader never types one as a number, a boolean, a null or a comment,
      whatever `title`, `description`, `publisher.name`, an entry of `integrates` or a
      `default` or an example inside `configSchema` holds.
    - **The other scalars.** A boolean is `true` or `false`. An integer is decimal, with
      no leading zero and no `+`, and fits 64 bits. A float is finite and decimal, holds a
      `.` or an exponent, and is the shortest form that reads back as the same value. No
      field takes a null. The command refuses a value that it cannot write so.
    - **The same bytes every time.** A set is written sorted, as decision 3 has it, and
      the keys of `requires.definitions` and of every mapping inside `configSchema` stand
      in the order of their code points. The same `plugin.toml`, module and SDK give the
      same bytes on every run and on every machine.
    - **The digest.** These bytes are what `plugin pack` puts in the artifact, what
      install keeps and what the digest of the first message covers: the SHA-256 of the
      `plugin.yaml` that the plugin was built with, which the kernel compares with the
      file that install kept (ADR-063 decision 58; decision 40). Parity compares fields as
      read and the digest compares bytes. A file that passes step 2 and differs in one
      byte from the file that the image holds is caught by the digest clause of check 3 at
      the handshake (ADR-063 decision 16). How an instance takes that digest from its
      image is ADR-041's (decision 41).
    - **The test.** Running the writer twice cannot see a scalar that it encodes wrongly,
      so the Python job (ADR-063 decision 37) holds the counterpart of the test of
      decision 15. A corpus of `plugin.toml` files, decorator sets and config classes is
      written by the command, and each written file is read by boot's strict reader
      through `aiopsctl` and compared field by field with the input of its case. The
      corpus holds at least: a string with ` #`, with `: `, with a leading and with a
      trailing space, with a quote, a backslash, a tab, a line break and a character
      outside ASCII; the strings `yes`, `no`, `on`, `off`, `true`, `null`, `~`, `1.0`,
      `0755`, `0x1F`, `1e3`, `2001-12-14` and `${env:X}`; each of those in a
      `description`, in a `tested` entry of `integrates` and in a `default` of
      `configSchema`; an empty optional string, which reads as absent (decision 3); a
      default of each scalar type; and one file with every key of decision 25. A case
      fails when the reader refuses the file or returns a value other than the input.
      Through which command of `aiopsctl` the job reads a manifest is asked of ADR-041
      (decision 41).

#### `entrypoint`, and the id as a unit takes it

28. **`entrypoint`** is the command that starts the plugin's process: what step 4 of
    `plugin check` starts as a local process, in every language, and what the deployment
    unit starts (`07:148`, `:1577`; ADR-063 decisions 17 and 25; ADR-063 decision 54,
    reading 5). It is a hand-written key of `plugin.toml` (`07:148`). Whether
    `plugin.yaml` carries it, and so which of the two starts it, is question Q5. Whatever
    the answer, its form, its grammar and the way its first word is found are these. The
    one reader of decision 15 checks the form and the grammar wherever it reads the field
    in `plugin.yaml`; `aiops-sdk manifest` copies the string and checks neither (decision
    26).
    - **Form.** It is one string of words separated by single spaces, as `07:160` writes
      it. It is split at the spaces into an argument list. No shell reads it, and no
      reader expands, quotes or joins a word.
    - **Grammar.** The grammar is closed. A word holds ASCII letters, digits and the six
      characters `_`, `.`, `/`, `-`, `:` and `=`, and nothing else: no other whitespace,
      no quote, no backslash, no `$`, no `%` and no other punctuation. The first word
      begins with a letter, a digit, `.` or `/`. The string has no leading or trailing
      space and no two spaces in a row. A later word may begin with `-`, as `-m` does in
      `07:160`, so the grammar alone does not keep a word from looking like an option;
      decision 30 says where a word may be written.
    - **Bounds.** It holds at most 16 words, and a word at most 128 bytes, the bound that
      boot's reader already gives an identifier (`boot/parse.go:301-303`). Both numbers
      are proposed here; no written line gives one.
    - **The first word, in both starts.** A first word that holds a `/` is a path,
      absolute or relative to the working directory of the process. A first word that
      holds none is looked up on the search path of the environment in which the process
      starts. No start adds a directory to that path, tries a second name or passes the
      string to a shell.
    - **Step 4 of `plugin check`** starts the process in the plugin directory, with the
      environment of the runner and what ADR-041 adds to it (decision 41).
    - **The unit** starts the process with the working directory and the environment that
      the image configures: it sets neither from the artifact (ADR-063 decision 25). The
      words are the whole command of the container (decision 30).
    - **What makes one string valid in both places** is the build file that each
      first-party template writes (ADR-063 decision 19), not a rule of the schema. The
      Python template writes `python -m <package>`, as `07:160` does: its build file names
      a base image whose search path holds `python` and sets the working directory of the
      image to the directory into which it copies the plugin directory, so `-m` finds the
      package in both places. The template of a remote Go plugin writes `./<name>`: the
      build that precedes step 4 writes the binary at that path in the plugin directory,
      which is asked of the ADR-061 part (decision 41), and the template's build file puts
      the one static binary at the same path under the working directory of the image. An
      absolute path of the image, such as `/<name>`, does not exist on the author's
      machine, so no first-party template writes one.
    - **What each start proves.** Step 4 and step 5 prove the string on the author's
      machine. That it resolves inside the image is proven where the image runs: by
      `plugin verify` and `plugin smoke` (ADR-063 decision 17) and by steps C3 and C4 of
      the exit (ADR-063 decision 27). An entrypoint that resolves in one place and not in
      the other fails the proof of that place; the schema cannot decide it.
    - **Nothing of the site.** It carries the command and nothing of the site: no listen
      address, no socket path and no path of a credential. The unit and the check runner
      give those, from the first-party template and from the runner alone (ADR-063
      decision 25). How a started process learns them is asked of ADR-041 (decision 41).
29. **The id as a unit takes it** is the `id` of decision 4: exactly one `/` between two
    kebab-case segments (`boot/manifest.go:580-590`), so it holds lowercase ASCII letters,
    digits, single hyphens inside a segment and the one slash, and nothing else. That
    closed set of characters is the grammar that ADR-063 decision 25 asks for.
    - **One bound, which is the reader's and not a unit's.** This addition adds one rule
      of the schema, for every manifest: a segment holds at most 128 bytes, the bound that
      boot's reader gives an identifier of a layer (`boot/parse.go:301-303`). The reader
      had none, and each segment is already a path component under the home (ADR-047
      decision 13). No id of the repository is longer, the longest segment being 19 bytes,
      so the rule changes no manifest (decision 2). The bound is not derived from any
      target of a unit and is not offered as one. A DNS label holds at most 63 octets (RFC
      1035, section 2.3.4), and the endpoint of an instance is a name that the runtime
      resolves on the instance's network (ADR-063 decision 27), so a name that holds both
      segments as they are can pass the limit of a target long before it passes this
      bound.
    - **No unit takes the id as text.** A unit takes the id as a value, never as text to
      splice (ADR-063 decision 25), and a slash cannot stand in a file name, so no name of
      a unit is the id itself. How the unit file, the network, the endpoint and the
      credential directory are named from the id is the S2b part of ADR-047, with the
      paths of the home (ADR-063 decisions 25 and 34), and the image reference, the first
      of the three inputs, is that part's too.
    - **Two things are asked of that part** (decision 41). The names are an injective
      function of the id: two ids never share a unit file, a network, an endpoint or a
      credential directory, since those of one instance must never be another's (ADR-063
      decisions 23 to 25). Joining the two segments with a hyphen is not such a function,
      as `a-b/c` and `a/b-c` show. And each name is inside the length and the character
      limits of its target for every id that the schema admits, with each limit cited at
      its source in that addition. Where a target cannot take a name of every admitted id,
      that addition either maps the id to a name of fixed length or proposes a tighter
      bound as an amendment of this ADR, before the first community release (decision 2).
    - The certificate of an instance names the plugin id (ADR-063 decision 55). Which
      field holds it, and that the field takes every id that the schema admits, is asked
      of ADR-041 (decision 41).
30. **What the unit renderer may do with the two values.** With question Q5 (a) the
    renderer takes the id and the `entrypoint` from the artifact; with (b) or (c) it takes
    the id alone, and the rules on the words below have nothing to bind.
    - **The grammar check is the reading.** The renderer writes only an id and an
      `entrypoint` that it took, in the same command, from a manifest that the one reader
      of decision 15 accepted (ADR-061 decision 22). Both grammars are rules of the schema
      (decision 3), so that reading is the check, in both modes, and gate check 1 denies a
      manifest that breaks either at every entry point (decision 24; `08:499`). A value
      from any other source is not written, and with a refused manifest no unit is
      rendered and nothing is written (ADR-063 decisions 17 and 28). Each value is written
      by the encoder of the target's format (ADR-063 decision 25).
    - **The words are the whole command of the container.** The unit gives the container
      the argument list of decision 28 as its whole command line, in place of both the
      entrypoint and the command that the image configures: no word of the image stands
      before or after the words of the manifest, and the process that the unit starts has
      the arguments that step 4 of `plugin check` gave it.
    - **A word is only ever an argument.** Each word reaches the target as one value: one
      element of a list of the target's format, or one argument of the command line that
      starts the container, in a position where the target reads the value of the
      entrypoint or an argument of the process, and never an option, a directive or a
      specifier of its own. On a command line the words after the first stand after the
      image reference, in the position where the container runtime takes the arguments of
      the process and reads no option, and the first word, which cannot begin with `-`
      (decision 28), is the value of the entrypoint. No word stands before the image
      reference as an argument of its own, and nothing else in the unit is computed from a
      word.
    - **Why the grammar is not enough alone.** The grammar admits `--privileged`,
      `--network=host`, `--volume=/:/host`, `--user=0:0` and `--cap-add=ALL` as words
      after the first. Under the rule above each is an argument of the plugin's own
      program and changes nothing of the unit: the user, the mounts, the capabilities and
      the network stay those of the first-party template (ADR-063 decisions 24 and 25).
      The grammar holds no quote, no backslash, no `$` and no `%`, so that a word holds no
      character that the reader of a target would expand or unquote. Which characters each
      target reads so, with its source at the pinned version of that target, is asked of
      the ADR-047 part (decision 41).
    - **Two cases** join those of ADR-063 decision 28, each on the compose fragment and on
      the systemd unit (ADR-063 decision 50). An entrypoint whose later words are
      `--privileged`, `--network=host` and `--volume=/:/host` renders a unit whose user,
      mounts, capabilities and network equal those of the same artifact with a one-word
      entrypoint, and the process that the unit starts receives the three words as its
      arguments. And two artifacts whose ids are `a-b/c` and `a/b-c`, installed on one
      site, get two unit files, two networks, two endpoints and two credential
      directories.

#### The generated half of a Python plugin

31. **A declaration names an object that `aiops-sdk` holds, and every such object is
    generated.** A decorator takes a definition, a key or a hook as an object of a
    generated module, never as a string, so a name that the SDK does not hold fails when
    the plugin's module is imported. With question Q9 (b) alone a callback key is written
    as a string, in `plugin.toml`, and `aiops-sdk manifest` resolves it to the same
    object or refuses it.
    - **Which definitions get a module.** The adapter generator writes one Python module
      per definition and major (`07:1207`). ADR-063 decisions 13 and 18 give it the
      definitions marked `contrib`. This addition widens its Python output, as an
      extension of decision 13 there, to two more kinds: a definition marked `callback`,
      and a definition that owns a hook marked remote. The same parity check holds every
      generated file to its source (ADR-063 decision 13). The commit that records the
      answers edits `07:1203` and `07:1207` and places a dated note at ADR-063 decisions
      13 and 18 (Consequences).
    - **What a module holds.** For a `contrib` definition: the message classes and the
      base classes of its contributions, and with question Q6 (b) or (c) also one object
      per registry key. For a `callback` definition: the message classes and one object
      per key that a remote plugin may call, carrying the name and the major of the key;
      the calls themselves are reached through the call context, which ADR-041 fixes
      (ADR-063 decision 34). For a hook marked remote: one object per hook, with the stage
      names that the `definition.yaml` declares, in order (ADR-060 decision 7). Every
      module carries the `major.minor` of the `definition.yaml` it was generated from.
    - **The names** follow the table under this decision. R8 puts the method names of the
      Python base classes with this part (`07:66`); they are generated from the proto of
      each definition (`07:1207`; ADR-063 decision 45), so the rule fixes them and no name
      is written by hand. The generator fails, and so does the definition lint, when a
      name that the rule gives is a reserved word of Python or when two names of one
      module are equal. The methods of the test fixture `aiops_sdk.testing.host()`, which
      R8 names beside them, are fixed in ADR-041 with the fixture (ADR-063 decision 34;
      decision 41), and so is the call context that a method receives.
    - **The test definition.** The test definition of ADR-063 decision 51 is marked
      `callback` and owns a test hook marked remote. Its Python module is generated like
      any other, by the same generator and under the same parity check, into the tests of
      `python/`, a directory that the build of the package does not read, so no built
      package holds it, as nothing of that definition ships in a release table. At S2b it
      is the only input of a callback declaration and of `@listens`: the shipped package
      holds the modules of `echoers` and `stampers`, which own no hook and are no
      callback. The Python job (ADR-063 decision 37) exercises decisions 33 and 34 on it:
      `aiops-sdk manifest` on plugins of the test tree that declare the test callback as a
      need and as a want and listen on the test hook at each of its stages, each written
      file read by boot's reader as decision 27 has it and compared with the declarations;
      a declaration of an object that is not generated, and a stage that the hook does not
      declare, each refused; and the callback and the listener driven through the fixture
      (ADR-063 decision 51).

| Generated name | Rule | Source |
|---|---|---|
| The module of a definition and major | `aiops_sdk.defs.<name>_v<N>`, with the `<name>` and the `<N>` of `sdk/def/<name>/v<N>` | `07:1218` |
| A message class | The name of the proto message, unchanged; the proto compiler writes it | ADR-063 decision 45 |
| A base class | The name of the proto declaration that lists the calls of one contribution, unchanged. Which proto declaration that is, a service per definition or the messages of the one stream, is ADR-041's | `07:1222`; ADR-063 decision 34 |
| A method of a base class | The proto name of the call in lower case, with an underscore before each capital that is not the first character and that either follows a lower-case letter or a digit or stands before a lower-case letter: `Describe` gives `describe`, `RunOnce` gives `run_once` and `HTTPGet` gives `http_get` | `07:1223`, `:1226` |
| The object of a key or of a hook | The name of the key or the hook by the same mapping, in upper case, with each run of characters other than letters and digits as one underscore | Decisions 33 and 34 |

32. **`registers`** comes from `@contributes` (`07:1208`): one entry `{key, major}` per
    registry that a decorated class contributes to, written as the registry key that the
    definition declares (decision 8). How the decorator names that key where a definition
    declares more than one registry key is question Q6.
33. **`uses`** is the set of callbacks that the plugin's contributions and listeners call,
    since the contribution set includes the callbacks a remote plugin calls (`08:391`;
    ADR-048:433). Where a Python plugin declares them is question Q9, because the
    recommended source adds arguments to the decorators as plugin record §4 writes them.
    Whatever the answer:
    - a callback is named by its key and not by its definition, as `uses` holds keys
      (decision 6): by the key object of decision 31. A definition marked `callback` that
      declares several keys offers one object per key, and a declaration names each key
      that the plugin calls. The entry takes the name and the major of the key from the
      object;
    - a declared need writes `as: need` and a declared want writes `as: want`, the two
      forms of decision 6 (`07:192-194`). `uses` is the union over the module, and a key
      declared both ways is a need;
    - the call context offers a contribution the callbacks that its plugin declares, and a
      call of another fails in the SDK, as an undeclared `Provide` or `On` fails in Go
      (`07:139`, `:141`). The proxy holds only the declared ones in any case, since its
      `Inject` is built from `uses` (ADR-063 decision 10);
    - the registry that a contribution registers with is not written into `uses`: Python
      has no `Inject`, and `registers` names it. A remote Go manifest does hold it there,
      so the two languages differ on that one entry; question Q8 says what follows from
      it.
34. **`hooks`** comes from `@listens(hook, stage)` (`07:1209`): one entry `{hook, stage}`
    per decorated function, by name (decision 10).
    - The hook is the hook object of decision 31, and the stage is one of the stage names
      that the object carries. No hook name and no stage name of the SDK is written by
      hand.
    - A hook that is not marked remote has no such object, so a Python plugin cannot name
      one (`07:1116`), and `aiops-sdk manifest` refuses a stage that the hook does not
      declare.
    - The ordinal of a stage is served data and is in no manifest (decision 10).
35. **`configSchema`** is derived by `aiops-sdk` itself, with the standard library of the
    language. No schema library is taken, on the terms of decision 26.
    - **The config class.** A plugin has at most one config class, a class that
      `dataclasses.dataclass` of the standard library made. Which declaration names it is
      question Q9. A plugin that names none takes no config: its schema is the closed
      empty object that `plugin.Define` derives from an empty struct
      (`examples/dispatch/plugin.yaml:22-26`), never `{}`, which decision 9 keeps for a
      value of `kernel.Define`.
    - **The type map** is the table under this decision. `aiops-sdk manifest` refuses any
      other annotation of a field: a union, an optional type, `Any`, a tuple, a set, a
      class that is not a dataclass and a class that holds itself. The derivation emits
      `$schema` at the root, `type` with one name, `properties`, `additionalProperties`,
      `items` and the annotations `default`, `examples`, `x-aiops-volatile` and
      `x-aiops-tenant`. At S2b it emits no `minimum`, `maximum` or `pattern`, since no
      type of the map is bounded and none is a duration. Each is a keyword of the Go
      derivation and of its validator (decision 9; ADR-060 decision 4;
      `sdk/plugin/validate.go:35-40`). A type joins the map with its first consumer, by an
      addition to this ADR.
    - **Keys and defaults.** A property takes the name of its field, as written. A field
      whose default is a `bool`, an `int`, a `float` or a `str` writes it as `default`, as
      the Go derivation gives the tag to a scalar field alone
      (`sdk/plugin/schema.go:245-260`); no other default is written. No keyword makes a
      key required, in either language: a key that a row's config leaves out takes the
      default of its field and, where the field has none, the empty value of its type.
    - **The two markers** are `aiops_sdk.Volatile[T]` and `aiops_sdk.Tenant[T]`, each
      written as the annotation of a field in place of `T`. `Volatile[T]` writes the
      schema of `T` with `x-aiops-volatile: true`, as `plugin.Volatile[T]` does in Go.
      `Tenant[T]` marks a tenant-writable field and writes both `x-aiops-tenant: true` and
      `x-aiops-volatile: true`, since every tenant-writable field is volatile (`07:142`,
      `:480`); the Go tag `aiops:"tenant"` is refused on a field that is not volatile
      (`sdk/plugin/schema.go:208`), and here the one marker carries both. The proxy
      forwards a volatile change to the instance and `aiops-sdk` hands the new config to
      the contribution (ADR-063 decision 49); the volatile paths that the proxy reads are
      these marks (decision 9).
    - **The config examples** are the files `testdata/config/*.json` of the plugin
      directory, as for a Go plugin (`07:1405`, `:1576`). Step 3 of `plugin check`
      validates each against the `configSchema` of `plugin.yaml` (ADR-063 decision 54,
      reading 6). `aiops-sdk manifest` writes them, in the byte order of their names, as
      the root `examples` of the schema, as `plugingen` does (ADR-047 decision 12), so one
      config has one manifest form in both languages.
    - **Decoding.** `aiops-sdk` decodes the tree of a row's config into the class by the
      same map. It refuses a key that the class does not hold and a value of another type,
      with one widening: a `float` field takes an integer, as `number` admits one
      (`sdk/plugin/validate.go:42-44`), while an `int` field takes neither a float nor a
      boolean. Composition has validated the tree against the `configSchema` of the
      manifest before the proxy sends it (`07:142`), so a refusal here is a defect of the
      derivation or of the decoder, and it fails the activation with the path of the key.
    - **The tests.** Each row of the map, each refusal and each marker is a table case of
      the Python tests, and the Python job compiles the schema of each case with the
      validator of the `plugin` package, through `aiopsctl` (ADR-063 decisions 18 and 37;
      decision 41). One file of the repository holds the cases that both derivations
      share, a type and the schema expected of it, and the Go test of `sdk/plugin` and the
      Python test both read it, so the two derivations cannot drift on a type that both
      have. Step 3 of `plugin check` compiles the written `configSchema` with that
      validator in any language, so a keyword outside the set fails there (`07:1576`).

| Annotation of a field | Schema | Its Go counterpart (`sdk/plugin/schema.go:99-126`) |
|---|---|---|
| `bool` | `type: boolean` | `bool` |
| `int` | `type: integer` | `int`, `int64` |
| `float` | `type: number` | `float64` |
| `str` | `type: string` | `string` |
| `list[T]` | `type: array`, with `items` the schema of `T` | A slice |
| `dict[str, T]` | `type: object`, with `additionalProperties` the schema of `T` | A map with string keys |
| A dataclass | `type: object`, with `properties`, one per field, and `additionalProperties: false` | A struct |
| `aiops_sdk.Volatile[T]`, `aiops_sdk.Tenant[T]` | The schema of `T`, with the annotations of the two markers | `plugin.Volatile[T]`; the tag `aiops:"tenant"` |

36. **The other generated fields of a Python manifest.** `provides` is `[]`, since a
    remote plugin provides no key (`07:1115`), and `durable` is `[]` (decision 11).
    `requires` holds `remoteProtocol` (decision 37), `definitions` and no `pluginApi`
    (decision 12; `07:148`). `definitions` names each definition that a declaration names,
    as a contribution, as the owner of a hook or as the owner of a callback key, at the
    `major.minor` that its generated module carries (decision 31; decision 12; `08:1264`).

#### The spoken majors

37. **The spoken majors are declared once.** The module rooted at `sdk/remote` (ADR-063
    decision 43) declares the set of protocol majors that this version of the module
    speaks. At S2b it holds major 0 alone (ADR-063 decision 5). It is what ADR-063
    decision 34 calls the constant of spoken majors that `plugingen` reads.
    - **The declaration** is the function `Majors() []uint32` of the package `remote`, the
      root package of that module, whose import path is the module path. It returns a new
      slice on each call, in ascending order and with no major twice, in the type of
      `Hello.majors` (`sdk/remote/handshake/handshake.proto:16`). Go has no constant of a
      list type, and any importer could change a package-level variable, so the
      declaration is a function. `sdk/remotehost`, the kernel side of the protocol, the
      driver program of `plugingen`, the builder and the pipeline of step D1 all compile
      against that one name.
    - It is the only list. `sdk/remotehost` sends it as `Hello.majors` (`07:1127`), the
      kernel side of the protocol sends it (ADR-063 decision 9), and no package holds a
      second list of majors. Step D1 of the S2b exit rests on that: a version of the
      remote modules whose `Majors` returns 7 alone gives a plugin that offers 7 and a
      manifest that says so (ADR-063 decision 27).
    - `plugingen` reads it as it reads the `Spec`. The driver program that prints what
      `plugin.Generate` reads from the plugin's value
      (`internal/plugingen/plugingen.go:9-13`) also prints what `Majors` returns, from the
      version of the module in the plugin's own build list. So `requires.remoteProtocol`
      is what that build sends in `Hello`. A manifest that lists `remote` while the build
      list of its plugin lacks the module fails `plugingen` with that reason, and the
      driver of a manifest without `remote` does not import the module.
    - `plugingen` writes `remoteProtocol`, sorted, exactly when `runtimes` holds `remote`,
      and `pluginApi` exactly when it holds `inproc` (decision 12; ADR-063 decision 17).
      `plugin.Generate` gives `pluginApi` for every value (`sdk/plugin/manifest.go:154`),
      so `plugingen` leaves it out of a manifest without `inproc`, as step G1 of the exit
      expects (ADR-063 decision 27).
    - `serves.remoteProtocol` is written by the builder (ADR-063 decision 46), and its
      writer is the S2b part of ADR-044 (ADR-063 decision 34). This addition asks one
      thing of it (decision 41): it calls `Majors`, from the build list of the binary, and
      holds no list of its own.
    - In Python the counterpart is one declaration inside `aiops-sdk`: `serve()` sends it
      in `Hello.majors`, and `aiops-sdk manifest` writes it as `requires.remoteProtocol`
      (`07:1210-1211`). No code outside the package reads it.

#### A manifest that lists `inproc` and `remote`

38. **What such a manifest packs and installs is question Q7.** Whatever the answer:
    - decision 12 stands: its `requires` holds both `pluginApi` and `remoteProtocol`;
    - a signer's entry covers it only when the entry lists both runtimes (ADR-020 decision
      6; `07:1866`);
    - a site runs a plugin id in one runtime, since boot resolves a row in the module
      table or in `plugins.lock` (ADR-063 decision 12) and a site holds one digest per
      plugin id (ADR-047 decision 7);
    - one `Plugin` value that passes the same suite in both runtimes (`07:1198`) needs no
      such manifest: the fixture echoers of the S2a and S2b exits prove it with one
      manifest per runtime (ADR-063 decision 28).

#### The contribution-set clause of check 9

39. **What the clause reads.** It reads a manifest whose `runtimes` holds `remote`,
    against the `serves` of the distribution, and denies a declaration outside the
    contribution set (`08:391`, `:507`; `07:1113-1118`). The table under this decision
    says when each field of ADR-048:433 denies. A key or a hook that `serves` does not
    hold carries no mark, so it is denied here as well as by step 1 of `plugin check`
    (`07:1574`). Beyond the table:
    - **A bound or signed key is denied whatever its mark.** The mark has one value per
      definition (`07:538`), while a definition may declare several keys of different
      classes and a `<key>.Bound` view of a key (decision 6). Read by the mark alone, the
      row of `uses` would admit a bound key or a bound view of a definition marked
      `callback`. The proxy value counts as `aiops/remote` for the binding lists and is
      first-party code (ADR-063 decision 41), so such an entry would hand the manifest of
      a third party a key through the bindings or the signer class of the proxy. ADR-063
      decision 10 denies "a `uses` entry of any other key, a bound key included", so that
      no manifest takes a key through the bindings of `aiops/remote`; decision 28 there
      holds the case; and decision 41 there has a value take no key from its manifest that
      those bindings would satisfy.
      The clause therefore denies a `uses` entry that names a `<key>.Bound` view, and one
      whose key has the consumer class `bound` or `signed` in `serves`
      (`boot/distribution.go:100-101`; ADR-060 decisions 14 and 15), before it reads a
      mark, and no answer to question Q8 opens a way around it. The rule costs nothing
      that is planned: `credentials` is consumer-bound and is in the callback set, and a
      remote executor reaches it through `remediation` and never by an inject (`07:62` R4;
      `07:1180`).
    - **One mark for a definition that is both.** `tools` is a `contrib` registry
      (`07:1199`) and is in the callback set (`07:1178`; ADR-063 decision 34), and plugin
      record §5 lists `audit` and `llm`, both callbacks, each in one row with a `contrib`
      registry, `auditSinks` and `llmProviders`. A definition whose keys take both marks
      cannot be written with one value, and this clause, read on such a definition, would
      deny one of its two uses. No definition served at S2b is both: the two toy
      registries are `contrib`, and the test definition of ADR-063 decision 51 is
      `callback`. So the clause denies nothing wrongly at S2b, and no test of S2b reaches
      the case. The step that first marks one definition both ways, S8a at the latest with
      `tools`, settles the form of the mark, a value per key or a fourth value, by an
      addition to this ADR before that definition is marked; `serves`, the definition lint
      and this clause change with it (decision 42).
    - **`instantiable`** belongs to check 8, and this addition adds one rule of the schema
      to it: `instantiable: true` is valid only on a manifest whose `runtimes` holds
      `inproc`. Check 8 reads the field from a manifest of the module table only
      (`08:506`), and at S2b the bundle of a remote artifact holds exactly one row, of its
      own plugin (ADR-063 decision 67), so on a manifest without `inproc` the field has no
      reader, in Go as in Python (decision 25). No manifest of the repository changes
      (decision 2).
    - **`as`** is not read. A `want` is judged as a `need` is, by its key: a callback that
      a plugin can do without is still a callback (decision 6). `FromManifest` carries
      `as` into the `Inject` of the proxy, so a proxy is not PENDING for a callback that
      its manifest wants (`07:194`, `:240`; ADR-063 decision 10).
    - **`durable`** is read from S3. Until then the list is `[]` and check 1 denies an
      entry in any manifest (decision 11; `boot/manifest.go:228-232`), so the clause has
      nothing to read. The addition that fixes the entry shape at S3 names the entries
      that are inside the set: a remote plugin keeps its own `kv` and `blob` state and
      registers no route, job handler or SQL (`07:1117-1118`), and `durable` lists a
      schema, a key prefix, job kinds and a secret scope among its namespaces (`07:143`).
    - **`capabilities`** is not read. The contribution set of `08:391` holds no
      capability. A `network` entry of a remote manifest is refused by install and warned
      of by `plugin check` (ADR-063 decision 56), and the unit never has a wider network
      than the manifest lists (ADR-063 decision 24). A `secrets` entry names what the
      plugin reads in its own scope through the `secrets` callback, which arrives at S3
      (`07:135`, `:1174`; ADR-063 decision 26). The rule on a `harnesses` contributor
      stays with the check that ADR-051 names (decision 15).
    - **The other fields** belong to other checks: `scope` and `runtimes` to the profile
      clause, which is not evaluated before S4a (ADR-048:433; ADR-063 decision 47); the
      grammars of decisions 28 and 29 and every other rule of the schema to check 1;
      `requires` to checks 1 to 3.
    - At the handshake the clause runs on what the connection brings, against the same
      marks and classes and against the manifest's `registers` (ADR-063 decisions 11 and
      16).

| Field | The clause denies when | Source |
|---|---|---|
| `provides` | It holds an entry | `07:1115`; `08:391` |
| `registers` | The key of an entry, at its major, is not a key of a served definition marked `remote: contrib` | `07:1115`; ADR-063 decision 46 |
| `hooks` | The hook of an entry is not marked `remote: yes` in `serves`. A hook of the kernel never is (`internal/plugingen/defs.go:124`) | `07:1116`; `boot/distribution.go:105-109` |
| `uses` | An entry names a `<key>.Bound` view; or its key has the consumer class `bound` or `signed` in `serves`, whatever the mark of its definition; or its key, at its major, is not a key of a served definition marked `remote: callback`. For a key of the third kind that `registers` also lists, question Q8 | `08:391`; ADR-063 decisions 10, 28, 41 and 46 |

#### What is read from S2b

40. **Decisions 1 and 16 are read with the notes below.** No line of either moves, since
    ADR-063 cites lines of this file.
    - **Decision 1 and the digest.** Decision 1 says that the manifest digest of
      `plugins.lock`, of the handshake and of the index names the one file `plugin.yaml`
      (ADR-048:111-113). ADR-063 decision 58 settles what that means at S2b. The digest of
      the handshake is the SHA-256 of the bytes of the `plugin.yaml` that the plugin was
      built with (decision 27). `plugins.lock` holds the digest of the artifact's OCI
      manifest and not of `plugin.yaml` (ADR-047 decision 8), and the kernel compares the
      digest of the first message with the `plugin.yaml` that install kept, whose
      descriptor chains to the locked digest through `artifact.json` (ADR-047 decision
      18). So the digest names this one file directly at the handshake and through that
      chain in the lock. The digest of an index entry stays with the S6a part.
    - **The table of decision 16** gains the rows under this decision, each from S2b.
    - **The install paragraph of decision 16** (ADR-048:437-441) gains three readings.
      Install of remote code reads `capabilities.network`, to refuse a manifest that lists
      an entry (ADR-063 decision 56), and `plugin check` reads it for its warning. With
      question Q5 (a), install reads `entrypoint`, the third input of the unit, beside
      `id`, the second (ADR-063 decision 25). And the function that turns a kept manifest
      into a plugin value reads `uses`, `registers`, `hooks` and `configSchema`, through
      `FromManifest` (ADR-063 decisions 10 and 12).

| Check | Fields read from S2b, beyond ADR-048:426-435 | Source |
|---|---|---|
| 1 | With question Q5 (a) or (b), `entrypoint`, whose form and grammar are rules of the schema (decision 28); the bound on a segment of `id` (decision 29); `instantiable` against `runtimes` (decision 39); with question Q7 (b), `runtimes` for the pair of `inproc` and `remote` | Decision 24; `08:499` |
| 3 | `requires.remoteProtocol` against `serves.remoteProtocol`. The manifest digest of ADR-048:428 is no field: it is the SHA-256 of the bytes of the file | ADR-063 decisions 46 and 58 |
| 8 | `runtimes` of the artifact's own manifest, which tells a remote artifact, whose bundle holds exactly one row | `08:506`; ADR-063 decision 67 |
| 9 | The row of ADR-048:433 is read with decision 39: `provides`, `registers`, `hooks` and `uses` for the contribution-set clause, each against the marks and the key classes of `serves`; `scope` and `runtimes` for the profile clause, from S4a | ADR-063 decisions 46 and 47 |

#### What the S2b part asks of other ADRs

41. **Each point that this addition leaves to another ADR is in the table under this
    decision**, with whether the list of ADR-063 decision 34 already holds it. An item
    marked new is a requirement that this addition puts on the receiving ADR or addition,
    not one that ADR-063 gave it. At `bbfd839` none of the receiving documents exists
    (Context), so no item can be checked as taken up. Each receiving ADR or addition
    answers every item that names it, by the number of this decision, and this addition
    asks that the S2b exit report (ADR-063 decision 32) list each item with the decision
    that settled it.

| Asked of | What | In the list of ADR-063 decision 34 |
|---|---|---|
| ADR-041 | The call context, through which a contribution reaches the callbacks that its plugin declares (decision 33), and which each method of a base class receives (decision 31) | Yes |
| ADR-041 | The fixture `aiops_sdk.testing.host()`, with the names of its methods, which R8 puts with the Python parts (decision 31) | Yes: the fixture |
| ADR-041 | Which proto declaration lists the calls of one contribution, to which the naming rule of decision 31 applies | Yes: whether the calls are messages on the one stream or a service per definition |
| ADR-041 | How an instance takes the digest of `plugin.yaml` that it sends (decision 27) | Yes: the fields of the first message and its digest |
| ADR-041 | The lowest supported interpreter is a version whose standard library reads TOML (decision 26) | The versions are listed; the floor is new |
| ADR-041 | How a started process learns its listen address or socket path and the path of its credential, at step 4 of `plugin check` and in the unit (decision 28). It is where the instance's key and the kernel's certificate reach the process | No: new |
| ADR-041 | The field of the instance certificate that names the plugin id takes every id that the schema admits (decision 29) | The identity is listed; the bound is new |
| ADR-041 | The command of `aiopsctl` that the fixture wraps lets a test read a `plugin.yaml` with boot's strict reader and receive the fields as read, and compile a `configSchema` with the validator of the `plugin` package (decisions 27 and 35) | The command is listed; this use is new |
| ADR-047, S2b part | The layer and the media type of the image reference; the unit files, the endpoint table and the credentials as rows of the home table (decision 29) | Yes |
| ADR-047, S2b part | The names of the unit file, the network, the endpoint and the credential directory: an injective function of the id, inside the limits of each target for every admitted id, each limit cited at its source (decision 29) | No: new |
| ADR-047, S2b part | The template of each target keeps the rules of decision 30 on the words, and the addition states which characters the reader of each target expands or unquotes inside a value, with its source at the pinned version | No: new |
| ADR-061, S2b part | `plugin new --lang python` and the form of a remote Go plugin (decisions 25 and 28) | Yes |
| ADR-061, S2b part | What builds the binary of a remote Go plugin before step 4, at the path that the first word of its `entrypoint` names in the plugin directory (decision 28) | No: new |
| ADR-061, S2b part | How `aiopsctl` finds and runs `aiops-sdk manifest`: the program it starts, the interpreter, and the form of the command that prints the document and writes no file, which step 2 needs (decision 26) | No: new |
| ADR-044, S2b part | The writer of `serves.remoteProtocol` calls `Majors` of the module `sdk/remote`, from the build list of the binary, and holds no list of its own (decision 37) | The writer is listed; its source is new |
| ADR-020, S2b part | Whether the attestation names the runtimes its suites ran in | Yes |
| ADR-063 decisions 13 and 18, by a dated note | The Python output of the adapter generator also covers a definition marked `callback` and a definition that owns a hook marked remote (decision 31) | No: an extension of decision 13 there |

#### What the S2b part leaves open

42. Not settled here:
    - the messages of the protocol and the rest of the scope of the Python SDK, which are
      ADR-041's (ADR-063 decision 34), and every item of decision 41 until its receiving
      ADR answers it;
    - the form of the `remote` mark of a definition whose keys take both marks, by an
      addition to this ADR at the step that first marks one, S8a at the latest (decision
      39);
    - the types of the config class beyond the map of decision 35, each with its first
      consumer;
    - the `durable` entries and their place in check 9, at S3 (decision 39);
    - the allow-list side of `capabilities.network`, S5 at the latest (ADR-063 decision
      56), and the profile clause of check 9, at S4a (ADR-063 decision 47);
    - with question Q7 (b), what a manifest of two runtimes packs and installs, with the
      first plugin that is to be published for both;
    - index format 1.0, at S6a (decision 20).

### Consequences of the S2b part

- `aiops-sdk manifest`, the Python template, the Python output of the adapter generator,
  the `remoteProtocol` output of `plugingen`, the contribution-set clause of check 9 and
  the unit renderer each have a written rule, and each waits for the answer it names: Q5
  for the renderer and for step 4 of `plugin check`, Q6 for `@contributes`, Q7 for a
  manifest of two runtimes, Q8 for the `uses` of a remote Go manifest and Q9 for the
  declarations of callbacks and of the config class.
- Schema 1.0 changes before the first community release, by this amendment (decision 2): a
  segment of the id holds at most 128 bytes; `instantiable: true` is valid only with
  `inproc`; and with question Q5 (a) or (b) the hand-written half gains `entrypoint`. No
  manifest of the repository changes by any of the three. A binary built before the change
  denies a manifest that carries `entrypoint`, as an unknown field at a known minor
  (decision 24); no such binary is published (decision 2).
- `aiopsctl` reads `plugin.yaml` alone, with every option but Q5 (c). It tests for a
  `plugin.toml` and reads no key of it, and no reader of TOML enters a Go binary.
- A Python plugin declares in code what a Go plugin declares in its `Spec`: its
  contributions, its listeners with their stages, the callbacks it calls and its config
  type. No library is taken for TOML, for YAML or for the schema, so the list of ADR-063
  decision 35 gains no library from this addition. That rests on the interpreter's
  standard library, at a floor that ADR-041 is asked for, and the interpreter's licence is
  still to be read under that decision before `aiops-sdk manifest` merges.
- The grammar of `entrypoint` and the rules of decision 30 together keep a third party's
  string out of every place where a unit reads an option: the grammar closes the
  characters, and the placement makes each word an argument of the plugin's own program.
- One declaration of the spoken majors feeds `Hello.majors` on both sides,
  `requires.remoteProtocol` and `serves.remoteProtocol`. What a manifest states and what
  its build sends then differ only in a manifest made without `plugingen`, which is the
  copy of step D2 of the exit (ADR-063 decision 27).
- This addition hands seventeen points to other ADRs (decision 41), ten of them, in whole
  or in part, outside the list of ADR-063 decision 34. None can be checked as taken up
  until the receiving documents exist, and the addition is complete only with their
  answers.
- The commit that records the answers makes the edits that the chosen options name, and
  these, which no option changes:
  - R8 at `07:66` says that the S2b part of ADR-048 fixes the keys of `plugin.toml` and
    the rule that names the generated Python classes and methods (decision 31), and that
    ADR-041 fixes the methods of the test fixture;
  - the sketch of `07:150-168` gains the `[python]` table;
  - `07:1203` and `07:1207` name the three kinds of definition that get a Python module
    (decision 31), with a dated note at ADR-063 decisions 13 and 18;
  - the `id` row of `07:126` gains the bound of decision 29, and the `instantiable` row of
    `07:137` the rule of decision 39;
  - the rows of checks 1, 3, 8 and 9 in the table of decision 16 and its install paragraph
    (ADR-048:426-441) are read with decisions 39 and 40.
- On acceptance the Status at the head of this ADR, the row of ADR-048 in the ADR table of
  `08` §3 (`08:1182`) and the row of ADR-048 in `00` §9 (`00:786`) record the S2b part, in
  the commit that records the answers (`08:999`), and the S6a part stays open. The Status
  is edited without moving a line, since ADR-063 cites lines of this file.

### Questions for the owner on the S2b part

Each question gives its options, the recommended one, why, and when the answer is needed.
An option that changes a written line names every line it edits, and an option that
changes the plugin record amends it in its §17 and names the section (ADR-063 decision 4).

**Q5. Does `plugin.yaml` carry `entrypoint`, and what starts it?** `07:148` has `plugin
check` and the deployment unit start the `entrypoint`, a key of `plugin.toml`, and plugin
record §14 has `aiopsctl` start the plugin "as a local process from the `entrypoint` of
`plugin.toml`". `00:207` lists the hand-written fields of `plugin.yaml` without it. An
artifact carries `plugin.yaml` and no `plugin.toml` (`08:393`), the unit takes the
`entrypoint` only if `plugin.yaml` keeps one (ADR-063 decision 25), and `aiopsctl` links
no reader of TOML. Decision 28 fixes the grammar and how the first word is found under
every option, and decision 30 what a unit may do with the words.
- (a) `plugin.yaml` carries it, and both start it, as `07:148` says. `entrypoint` is a
  hand-written field of schema 1.0, present exactly when `runtimes` holds `remote`, in
  every language, written after `runtimes`. `aiops-sdk manifest` copies it from
  `plugin.toml`, and the template of a remote Go plugin writes it in `plugin.yaml`
  (ADR-063 decision 60) as `./<name>`, the path at which the build before step 4 and the
  template's build file both put the binary (decision 28). Step 4 of `plugin check` starts
  it from `plugin.yaml`, and the unit takes it as its third input and as the whole command
  of the container (ADR-063 decision 25; decision 30). One string is then what the suites
  drove and what the unit runs; that the string resolves inside the image is proven where
  the image runs, not by step 4 (decision 28). It edits `00:207`, whose list of
  hand-written fields gains `entrypoint` for remote code; the field table of `07` §2,
  which gains a row after `07:134`; the table of community record §4.4 after
  community:176; the manifest paragraph of plugin record §4 and the sentence of plugin
  record §14, which then reads "from the `entrypoint` of `plugin.yaml`, which `aiops-sdk
  manifest` copies from `plugin.toml`", both by an amendment in its §17; the table of
  decision 4, which is read with decision 28; and the install paragraph of decision 16
  (ADR-048:437-441), which is read with decision 40.
- (b) `plugin.yaml` carries it, and only `plugin check` starts it. The unit runs the image
  with the command that the image itself configures, so it takes two inputs, the image
  reference and the id. It edits what (a) edits, plugin record §14 included, except the
  install paragraph; `07:148`, which loses "and the deployment unit"; and, by dated notes,
  ADR-063 decisions 25 and 28. The command that conformance ran is then not the command
  that a site runs, unless the build file of the image repeats it, and no check holds the
  two equal.
- (c) `plugin.yaml` does not carry it. `00:207` stands, and so does plugin record §14,
  which is this option to the letter. `aiopsctl` reads `entrypoint` from `plugin.toml` to
  run step 4, with a reader of TOML: a new linked module, read and registered first
  (ADR-063 decision 35), or a reader of its own, and that reader holds a second copy of
  the grammar of decision 28. A remote Go plugin has no `plugin.toml` and needs another
  rule. The unit runs the image's own command, with the edits of (b) to `07:148` and to
  ADR-063 decisions 25 and 28.
- Recommended: (a). It is `07:148` as written. The string that the suites drove is the
  string that the unit runs, with no word of the image before or after it (decision 30);
  `aiopsctl` and `FromManifest` keep reading one file (decision 1; ADR-063 decision 10); a
  remote Go plugin needs no second rule; and the string that reaches a unit is closed by
  the grammar of decision 28 and placed by the rules of decision 30. Its cost is the
  amendment of plugin record §14, whose words only (c) keeps. The owner decides, because
  it edits `00:207` and the plugin record. Needed before `aiops-sdk manifest`, step 4 of
  `plugin check` for remote code and the unit renderer merge.
- **Answered 2026-11-27: (a)**, decision 43.

**Q6. How does `@contributes` name a key where a definition declares more than one
registry key?** The decorator is `@contributes(definition)` (`07:1208`; plugin record §4),
`registers` names keys and not definitions (decision 8), and a definition may declare
several keys (ADR-060 decision 7). The two toy registries declare one key each.
- (a) By the base class. The generated module of a definition holds one base class per
  registry key of the definition, each carrying the name and the major of its key
  (`07:1207`; decision 31). `@contributes(definition)` takes the key from the base class
  that the decorated class derives from, as the sketch derives from `tools.Tool`
  (`07:1221-1222`), and refuses a class that derives from none of them or from two. No
  line is edited.
- (b) By a second argument, required only where the definition declares more than one
  registry key: `@contributes(definition, key)`, with the key object of decision 31.
  `07:1208` and plugin record §4 gain the argument, the record by an amendment in its §17.
- (c) Always by the key: `@contributes(key)`, with the key object of decision 31. It edits
  `07:1208`, the sketch at `07:1221` and plugin record §4, the record by an amendment in
  its §17.
- Recommended: (a). The decorator stays as written for every definition, the class already
  says which contribution it implements, and a wrong class is refused when the module is
  imported, before a manifest is written. Needed before the `@contributes` of `aiops-sdk`
  and the Python output of the adapter generator merge.
- **Answered 2026-11-27: (a)**, decision 44.

**Q7. What does a manifest that lists `inproc` and `remote` pack and install?** The schema
admits the pair (decision 4), `plugin check` runs T1 and T2 in every runtime listed
(`07:134`, `:1578`), and a version entry of the index names one artifact with its
`runtimes` (`07:1902`). No line says what the artifact holds, which form a site runs or
how the two forms are told apart, no exit step and no case of ADR-063 uses the pair
(ADR-063 decision 34), and a part lands with its first consumer (`08:680`).
- (a) As written, built whole at S2b. One artifact holds both forms: the image reference
  by digest, which `plugin pack` builds (ADR-063 decision 61), and the module path,
  version and sum (`08:393`). `plugin check` runs steps 4 and 5 in both runtimes. A site
  runs one form: `plugin install` takes the runtime as a flag, answers `rebuild-required`
  for `inproc` and locks and renders the unit for `remote`, and a plugin id that is both
  in the module table and locked is refused. The clauses "for in-process code" and "a
  remote manifest" each apply to the manifest. It needs additions to ADR-047 (two code
  layers), ADR-061 (the flag) and ADR-020 (the attestation of two runtimes), and cases for
  each rule.
- (b) Denied until a plugin needs it. The manifest validates, and gate check 1 denies it
  at every entry point, as it denies `wasm` (`08:499`; `boot/checks.go:242-243`):
  "`runtimes` lists `inproc` and `remote` together, a pair that no step serves yet".
  `plugin check` fails on it at step 1, before it runs a suite, so nothing packs or
  installs one. The schema keeps the set, so the pair is built later with no change of
  schema: with the first plugin that is to be published for both runtimes, by an addition
  to this ADR that settles what (a) lists. `08:499` gains the clause, and `07:134` and
  community:176 gain the sentence beside `wasm`.
- Recommended: (b). It is the rule that the owner chose for a `network` entry and for a
  second row of a remote bundle (ADR-063 decisions 56 and 67): nothing is built without a
  consumer, and nothing that is not built is admitted. (a) adds a flag, a layer and a boot
  rule that no test of S2b reaches. The owner decides, because (b) defers a form that the
  schema admits. Needed before gate check 1 and `plugin pack` of remote code merge.
- **Answered 2026-11-27: (b)**, decision 45.

**Q8. Is the registry key that a remote Go manifest holds in `uses` inside the
contribution set?** `plugingen` writes `uses` from `Spec.Inject` (decision 6), and a Go
contribution injects the registry it registers with
(`templates/echoers/plugin.go.tmpl:25-26`). Step G1 of the exit takes that value,
unchanged, as remote code, and `plugin check` passes its step 7, which runs the gate
(ADR-063 decisions 16 and 27). ADR-063 decisions 10 and 28 have check 9 deny a `uses`
entry of a key that is not marked `callback`, and `07:1198`, plugin record §11 and ADR-063
decision 8 word the value as one "whose `Inject` is within the callback set".
- (a) It is inside. The clause admits a `uses` entry whose key and major the same manifest
  lists in `registers`, where the row of `registers` in decision 39 judges it. One
  `Plugin` value then has one `uses` in both runtimes. The entry gives the proxy no key
  that `registers` does not already give it, since the proxy needs each registry of
  `registers` to register the contributions (ADR-063 decision 11). Every other key stays
  outside: a bound view and a key of a bound or signed consumer class, which decision 39
  denies before any mark is read; a key of a definition marked `no`; and a built-in key of
  the kernel, which belongs to `pluginApi` and carries no mark (ADR-060 decision 9). The
  two languages then differ on that entry: a remote Go manifest lists the registry key in
  `registers` and in `uses`, and a Python manifest in `registers` alone (decision 33). A
  reader of `uses` sees two shapes for one contribution, and none takes a decision from
  the difference: check 9 judges the key through `registers`, and `FromManifest` needs
  each registry of `registers` whether `uses` repeats it or not. It edits `07:1198` and
  the same sentence of plugin record §11, the record by an amendment in its §17, each of
  which gains "beyond the registries it registers with", and, by dated notes, ADR-063
  decisions 8, 10 and 28.
- (b) It is outside, to the letter. `plugingen` leaves out of the `uses` of a remote
  manifest each key that its `registers` lists, and decision 6 gains that exception.
  `07:1198`, plugin record §11 and ADR-063 decisions 8, 10 and 28 stand, and both
  languages write the same `uses` for a remote manifest. One value then has two forms of
  `uses`, one per runtime, and under question Q7 (a) the one manifest of both runtimes
  would have to hold both.
- Recommended: (a). Step G1 passes with the value unchanged, the manifest of a value is
  the same in both runtimes, and the proxy gains no key. The difference between the two
  languages has no reader. Needed before check 9 and `plugin.FromManifest` merge.
- **Answered 2026-11-27: (a)**, decision 46.

**Q9. Where does a Python plugin declare the callbacks it calls and its config class?**
`uses` and `configSchema` are generated fields (`00:207`), and `aiops-sdk manifest` reads
`plugin.toml` and the decorators (`07:79`). The decorators are written
`@contributes(definition)` and `@listens(hook, stage)` (`07:1208-1209`; plugin record §4),
and no part of the SDK names a callback or a config type (`07:1205-1213`), while the
sketch calls `ctx.secrets` (`07:1227`). In Go both are parts of the `Spec`: `Inject` and
the config type (ADR-060 decision 4). Decisions 33 and 35 fix what a declaration means
under every option.
- (a) Keyword arguments of the decorators. `@contributes` and `@listens` each take three
  optional arguments: `needs` and `wants`, each a list of key objects (decision 33), and
  `config`, the config class (decision 35). Every decorator of the module that names a
  class names the same one. The declaration stands beside the code that makes the call, as
  `Inject` stands in the `Spec`, and a name that the SDK does not hold fails when the
  module is imported. It edits `07:1208-1209` and the decorators of plugin record §4, the
  record by an amendment in its §17, and the sketch of `07:1221`, which then declares the
  callback that `07:1227` calls.
- (b) Keys of `plugin.toml`. The `[python]` table gains `needs` and `wants`, arrays of
  strings of the form `<key>@<major>`, and `config`, the dotted name of the config class.
  The decorators stay as written, and neither `07:1208-1209` nor plugin record §4 changes:
  the keys are inside what R8 leaves to this part (`07:66`). A generated field then has a
  hand-written source in a data file, a key is a string that only `aiops-sdk manifest`
  checks, and the declaration stands apart from the code that makes the call.
- (c) One call at module level, `aiops_sdk.plugin(needs, wants, config)`, a new part of
  the SDK. The decorators stay as written. The table of `07:1205-1213` gains a row, and
  plugin record §4 gains the part by an amendment in its §17.
- Recommended: (a). A Python plugin then declares in code, beside each contribution, what
  a Go plugin declares in its `Spec`; the declaration is checked at import; and the SDK
  keeps the parts it has. The owner decides, because the plugin record writes the
  decorators and ADR-063 decision 4 admits no departure from that record without the
  owner's word. Needed before the decorators of `aiops-sdk` and `aiops-sdk manifest`
  merge.
- **Answered 2026-11-27: (a)**, decision 47.

### Owner answers of 2026-11-27

The project owner accepted the S2b part of this ADR on 2026-11-27 and answered every
question of the section "Questions for the owner on the S2b part" on the same date, each
with its recommended option. Each decision below names the question it answers. Where a
decision of the S2b part leaves a point to a question, the decision here settles it.

43. **`plugin.yaml` carries `entrypoint`, and both `plugin check` and the deployment unit
    start it** (Q5 (a)), as `07:148` says. `entrypoint` is a hand-written field of schema
    1.0, present exactly when `runtimes` holds `remote`, in every language, and written
    after `runtimes`. `aiops-sdk manifest` copies it from `plugin.toml`, and the template
    of a remote Go plugin writes it in `plugin.yaml` (ADR-063 decision 60) as `./<name>`,
    the path at which the build before step 4 and the template's build file both put the
    binary (decision 28). Step 4 of `plugin check` starts it from `plugin.yaml`, and the
    unit takes it as its third input and as the whole command of the container (ADR-063
    decision 25; decision 30). One string is then what the suites drove and what the unit
    runs; that the string resolves inside the image is proven where the image runs, not
    by step 4 (decision 28). The table of decision 4 is read with decision 28, and the
    install paragraph of decision 16 (ADR-048:437-441) with decision 40. With this answer
    the owner accepted the amendment of plugin record §4 and §14.

44. **`@contributes` takes the key from the base class** (Q6 (a)). The generated module
    of a definition holds one base class per registry key of the definition, each
    carrying the name and the major of its key (`07:1207`; decision 31).
    `@contributes(definition)` takes the key from the base class that the decorated class
    derives from, as the sketch derives from `tools.Tool` (`07:1221-1222`), and refuses a
    class that derives from none of them or from two. The decorator stays as written for
    every definition, and this answer edits no line.

45. **A manifest that lists `inproc` and `remote` is denied until a plugin needs it** (Q7
    (b)). The manifest validates, and gate check 1 denies it at every entry point, as it
    denies `wasm` (`08:499`; `boot/checks.go:242-243`): "`runtimes` lists `inproc` and
    `remote` together, a pair that no step serves yet". `plugin check` fails on it at
    step 1, before it runs a suite, so nothing packs or installs one. The schema keeps
    the set, so the pair is built later with no change of schema: with the first plugin
    that is to be published for both runtimes, by an addition to this ADR that settles
    what option (a) of Q7 lists (decision 42). Check 1 reads `runtimes` for the pair
    (decision 40).

46. **The registry key that a remote Go manifest holds in `uses` is inside the
    contribution set** (Q8 (a)). The contribution-set clause of check 9 admits a `uses`
    entry whose key and major the same manifest lists in `registers`, where the row of
    `registers` in decision 39 judges it. One `Plugin` value then has one `uses` in both
    runtimes, and decision 6 stands with no exception. The entry gives the proxy no key
    that `registers` does not already give it (ADR-063 decision 11). Every other key
    stays outside: a bound view and a key of a bound or signed consumer class, which
    decision 39 denies before any mark is read; a key of a definition marked `no`; and a
    built-in key of the kernel, which belongs to `pluginApi` and carries no mark (ADR-060
    decision 9). The two languages differ on that entry: a remote Go manifest lists the
    registry key in `registers` and in `uses`, and a Python manifest in `registers` alone
    (decision 33). No reader takes a decision from the difference: check 9 judges the key
    through `registers`, and `FromManifest` needs each registry of `registers` whether
    `uses` repeats it or not. With this answer the owner accepted the amendment of plugin
    record §11.

47. **A Python plugin declares the callbacks it calls and its config class as keyword
    arguments of the decorators** (Q9 (a)). `@contributes` and `@listens` each take three
    optional arguments: `needs` and `wants`, each a list of key objects (decision 33),
    and `config`, the config class (decision 35). Every decorator of the module that
    names a config class names the same one. The declaration stands beside the code that
    makes the call, as `Inject` stands in the `Spec`, and a name that the SDK does not
    hold fails when the module is imported. The `[python]` table of `plugin.toml` holds
    the key `module` alone (decision 25), and no callback key is written as a string
    (decision 31). With this answer the owner accepted the amendment of the decorators of
    plugin record §4.

**Edits made with this record.** `00` is edited in the commit that records the answers
(`08:999`), and that commit makes the edits the chosen options name. For decision 43:
`00:207`, whose list of hand-written fields gains `entrypoint` for remote code; a row for
`entrypoint` in the field table of `07` §2, after `07:134`, and in the table of community
record §4.4, after community:176; the manifest paragraph of plugin record §4; and the
sentence of plugin record §14. Decision 44 edits no line. For decision 45: check 1 in
`08:499`, and the sentence beside `wasm` in `07:134` and in community:176. For decision
46: `07:1198` and the same sentence of plugin record §11, and dated notes at ADR-063
decisions 8, 10 and 28. For decision 47: `07:1208-1209`, the decorators of plugin record
§4, and the sketch of `07:1221`, which now declares the callback that `07:1227` calls.
Plugin record §17 holds the amendment of its §4, §11 and §14. The same commit makes the
edits that no option changes (Consequences of the S2b part): R8 at `07:66`; the
`[python]` table in the sketch of `07:150-168`; the three kinds of definition in
`07:1203` and `07:1207`, with dated notes at ADR-063 decisions 13 and 18; the bound of
decision 29 in the `id` row of `07:126` and the rule of decision 39 in the `instantiable`
row of `07:137`; the Status at the head of this ADR, edited without moving a line; and
the row of this ADR in `00` §9 (`00:786`), which records the S2b part. `07:148` and
ADR-063 decision 25 hold as written under decision 43, and the decorators keep the
written forms `@contributes(definition)` and `@listens(hook, stage)` wherever they are
restated, since the keyword arguments of decision 47 are optional. Left to later commits:
the row of ADR-048 in the ADR table of `08` §3 (`08:1182`), to the commit that updates
that table, and with it the two lists that restate the steps of that row, the ADR list
of `03` §10 and the ADR table of community record §9; ADR-041 decision 29, which lists
the generated modules as ADR-063 decision 18 did, and the sentences of the S2b parts of
ADR-020, ADR-047 and ADR-061 that call this part not accepted, proposed or a question,
each to the commit that records the owner's answers on that ADR. An option the owner did
not choose edits nothing.
