# DeepSeek Harness (dsh) architecture — what to copy for the AIOps plugin model

Research date: 2026-10-27. Evidence for this original note came from the public npm packages `@deepseek-ai/dsh@0.2.1-rc.2` and `@deepseek-ai/cordis@4.0.5`, their shipped source and documentation, the GitHub repository `deepseek-ai/deepseek-harness` (branch `master`), npm registry metadata and the CLI's configuration-dump interfaces. Package-relative paths below identify public upstream material, not a workstation installation. Anything not confirmed against those sources is marked **UNVERIFIED**.

Historical source survey: the current pin and Go adaptation are governed by the [canonical decisions](../architecture/00-decisions.md). The kernel remains a port of the published Cordis model with the applicable MIT notices, not a claim of independent source authorship. Editorial preparation follows [ADR-056](../adr/ADR-056-documentation-baseline.md); it adds no measurement and leaves the publication-rights gate open.

Version context (npm registry, read 2026-10-27): `latest` = `0.2.1-rc.2` (published 2026-10-17), `next` = `0.2.1-rc.4` (2026-10-26), first publish `0.0.1-rc.1` on 2026-08-10. The repository README calls the project a "developer preview" with "COMPATIBILITY-BREAKING CHANGES" expected; the architecture described here is the rc.2 line, and it has changed between rcs (the `.agents/notes` design notes are dated August–September 2026).

---

## 0. One-paragraph summary

dsh is not an agent with a plugin API bolted on; it is a **plugin loader with an agent composed on top**. The only executable is `dsh`, which boots a *profile*: an empty root config over which an ordered list of *bundles* (npm packages whose only payload is a YAML patch list plus code) are applied, followed by the user's own patch layers. Every row in the resulting tree is a Cordis plugin: the model adapter, the tool registry, the session log, the agent loop, telemetry, approval, sandboxing, storage, jobs, HMR, and even the plugin manager itself. Plugins declare which named *services* they need (`inject`) and the Cordis runtime starts them when those services exist and stops them when they vanish; every effect a plugin makes is registered with a disposer so unloading is complete. Apps (`web`, `headless`, `sdk`, `acp`) are nothing but profiles that stack one "mode bundle" over the shared `dsh-base` bundle. This is the model the AIOps platform should copy; the JS-specific mechanics (Proxy contexts, `!!js` YAML, pnpm per-profile `node_modules`, ESM HMR) are the parts to replace.

---

## 1. Cordis: the runtime underneath everything

Source: `vendor/cordis/src/{context,service,registry,fiber,reflect,events}.ts` shipped in `@deepseek-ai/cordis@4.0.5`; Cordis README in the same package; paper arXiv:2608.25512 (submitted 2026-08-26).

### 1.1 Design rationale (the paper)

The Cordis authors (Yifan Shi, Wei Zhang, Tianyi Cui — Peking University / DeepSeek-AI) frame plugin systems as needing two independent properties, and Cordis is "a meta-framework of spatiotemporal composability" that provides both:

| Axis | Definition (paper abstract) | Cordis mechanism |
|---|---|---|
| Temporal composability | "the ability to completely revert a component's side effects upon removal" | *revertible effects*: every context mutation is paired with an inverse the runtime keeps (`ctx.effect`, disposers) |
| Spatial composability | "the ability to declare and reactively manage inter-component dependencies" | *reactive coeffects*: the plugin's `inject` list is a coeffect specification; the runtime activates/deactivates the plugin as the context changes |

Both are mediated by a single `Context` type, which is why dsh can say "no privileged core to patch" (`docs/architecture.md`). The practical consequence: order in a config file carries no load semantics; activation is purely service-availability driven (stated verbatim in `dsh-base/cordis.patch.yml`: "Row order carries no load semantics (activation is service-availability driven); the grouping is for readers.").

### 1.2 Context tree

`Context` (`context.ts`) is a JavaScript `Proxy`. Property reads go through `ReflectService`, so `ctx.llm`, `ctx.tools`, `ctx.storageDomain` resolve to whatever service is currently *provided* under that name in scope. Three operations create child contexts without mutating the parent:

| Call | Effect |
|---|---|
| `ctx.extend(meta)` | child prototypally inherits everything; own props of `meta` shadow |
| `ctx.isolate(name, label?)` | below this child, reads/writes of service `name` resolve in a new scope (two `isolate` calls with the same label join). Used by dsh agent presets: `cordis:group` rows carry `isolate: { planMode: true }` so each agent gets its own plan-mode provider |
| `ctx.intercept(name, config)` | plugins loaded below see `config` merged into that service's per-plugin config (ancestor entries first) |

The root `Context` constructor installs four built-in services: `reflect`, `registry`, `events`, `logger`. The root fiber has uid `0`; `root.fiber.dispose()` is a restart of the whole tree.

### 1.3 Services and `provide`

`ctx.provide(name, value, check?)` (`reflect.ts:277`) registers a service implementation *owned by the current fiber* and returns a disposer; the `check` predicate lets a provider be present but "unavailable" (dependents stay pending). `Service` (`service.ts`) is a base class whose constructor calls `ctx.reflect.provide(name, this, this[Service.check])`, so a class plugin that extends `Service` *is* its service: `class Counter extends Service { constructor(ctx) { super(ctx, 'counter') } }`. Services can be callable (`ctx.logger('x')`) via `Service.invoke`. Removal is automatic: "the service is unregistered automatically when the owning fiber unloads".

Name conventions enforced in the repo (`docs/cookbook/adding-a-package.md`): singular `ctx` keys for one engine/runtime/policy (`ctx.llm`, `ctx.sandbox`), plural for registries (`ctx.tools`, `ctx.agents`, `ctx.sessions`).

### 1.4 Plugins, fibers, and the lifecycle

A plugin is any of three shapes (`registry.ts`): a function `(ctx, config)`, a class `new (ctx, config)`, or an object `{ apply(ctx, config) }`. Shared metadata: `name`, `Config` (a Standard Schema validator), `inject` (array of service names or map name → intercept config), `provide`, `intercept`. `ctx.plugin(plugin, config)` creates a **Fiber**: one runtime instance of one plugin application, with its own child context (`parent.extend({ fiber })`).

Fiber states (`fiber.ts`, `FiberState`):

| State | Meaning |
|---|---|
| `PENDING` | waiting for every `inject`ed service to be available |
| `LOADING` | plugin callback running (config validated first; `ValidationError` on schema failure) |
| `ACTIVE` | loaded and providing |
| `FAILED` | callback or config threw (error logged, fiber stays, can be retried by update) |
| `UNLOADING` | disposers running (reverse registration order, async awaited) |
| `DISPOSED` | removed; `uid === null`; cannot restart |

The reactive dependency mechanism is an *epoch string*: `_refresh()` concatenates the uid of the fiber providing each injected service (`':' + impl.fiber.uid`). If any is missing the epoch is `INACTIVE` and the fiber unloads/stays pending; if the epoch changes (a provider was replaced by a different fiber), the consumer unloads and reloads. This is how "swapping one provider moves the whole product" works without any event wiring by the plugin author. Transitions emit `internal/status`; a provider's state change notifies dependents through `reflect.notify`.

`ctx.effect(execute, label)` registers a cleanup-aware effect: `execute` returns a disposer, a promise of one, or a (async) iterable yielding several; disposers are collected and run when the returned disposer is called or the fiber unloads, whichever first. `ctx.on(event, listener)` and `ctx.provide` are effects, so they unwind with the fiber. Throwing `CordisError('INACTIVE_EFFECT')` when a disposed fiber tries to register an effect is what makes "a spec passing only alone" a defect in dsh's test policy.

`fiber.update(config, noSave)` validates, then runs the `internal/update` waterfall (HMR and persistence hooks can veto/replace the restart) and restarts the plugin. The vendored Loader additionally supports **volatile config**: schema fields marked `Volatile<T>` are committed into the running references without a restart and announced via `loader/volatile-update` (Cordis README "Volatile configuration").

### 1.5 Events

`EventsService` (`events.ts`) mixes `on/emit/bail/waterfall` onto `ctx`. `emit` runs listeners synchronously; `bail` stops at the first bail value; `waterfall` treats the last argument as innermost `next` — "Listeners run outermost-first; a listener that does not call `next()` vetoes the rest of the chain, including the built-in behavior." dsh builds all its interception seams on waterfall: `agent/pre-step`, `agent/request`, `llm/stream`, `tools/pre-execute`, `approval/request` (`docs/architecture.md`, `dsh-user-approval/README.md`). Listeners are removed when the owning fiber unloads. `serial`/`parallel` dispatch modes are referenced by the architecture doc (`agent/turn-stopping` is serial) — **UNVERIFIED** against source in this pass (only `emit/bail/waterfall/on` were read).

### 1.6 Loader, Include, Group, HMR

The runtime tree is not built by hand; `@deepseek-ai/cordis-plugin-loader` owns an `EntryTree` of entries `{ id, name, config, group, disabled, inject, intercept, isolate }`, imports modules by `name`, applies config, and keeps the running graph in sync with entry updates (`loader.create/update/remove/resolve/await`). `cordis-plugin-include` reads a YAML/JSON file into the tree and writes updates back; `cordis-plugin-group` nests entries (`tools:logger` ids). dsh registers `cordis:include` and `cordis:group` as Loader builtins (`dsh-app-boot` README, "Two Loader builtins"). HMR (`@deepseek-ai/dsh-hmr`, replacing `@cordisjs/plugin-hmr`) watches the profile manifest, both user patch files and optional module roots, and serializes reloads with Plugin Manager writes in one queue; package installs run outside that queue.

---

## 2. How a plugin is declared

Sources: `@deepseek-ai/dsh-time-context/lib/index.js` + `package.json` (a small shipped plugin), `docs/cookbook/adding-a-package.md`, `dsh-tools/README.md`, `--dump-config-schema` output, `dsh-base/cordis.patch.yml`.

### 2.1 Module exports

A dsh plugin module exports exactly the Cordis plugin-object fields. The real `dsh-time-context` entry, abridged:

```js
import z from "@deepseek-ai/schemastery";
const name = "time-context";                       // Loader diagnostics name
const inject = ["agents", "sessionProjections"];   // services required before apply() runs
const Config = z.object({ timeZone: z.string(), refreshIntervalMs: z.number() });
function apply(ctx, config) {
  ctx.sessionProjections.register({ key: "timeContext", stateVersion: 2, stateSchema, init, apply });
  ctx.on("agent/pre-step", async ({ agent, turn, step, signal }, next) => { const d = await next(); /* ... */ return d; }, { prepend: true });
}
export { Config, apply, inject, name };
```

Nothing else is needed: the two registrations above are effects on `ctx`, so they are reverted when the fiber unloads. Service-providing plugins instead `export default class X extends Service` (cookbook: "`src/index.ts` — a service default export or a plugin exposing `name`/`inject`/`apply`/`Config`"). Tool plugins call `ctx.tools.register(defineTool({ name, description, parameters, output, execute }))` and are automatically visible to the model ("Registering a tool is enough to make it visible — the registry feeds its schemas into the system-prompt assembly automatically").

### 2.2 Config schema

`Config` is a `@deepseek-ai/schemastery` schema (the DeepSeek-maintained edition of shigma/schemastery, `~3.18.5`, "Type Driven Schema Validator", serializable). Cordis validates through the Standard Schema interface (`runtime.Config['~standard'].validate`), synchronously only ("Async config validation is not supported"). Because schemastery schemas are serializable, `dsh --dump-config-schema` can import every composed module and emit one JSON Schema 2020-12 document describing the whole entry list plus `$defs.patchList` for overlays; it annotates what it cannot project (`x-cordis` diagnostics such as "transform callback validation and normalization are not executed or projected"). Schema-projector compatibility must be checked against the exact runtime and profile; this note reports no current test result.

### 2.3 Package shape

From `dsh-time-context/package.json` and the cookbook invariants (`pnpm run constraints`):

| Field | Convention |
|---|---|
| `name` | `@deepseek-ai/dsh-<name>`; one capability group folder under `packages/<group>/<pkg>` |
| `type` | `module`; `main: lib/index.js`, `types: lib/types/index.d.ts`, `exports["."]`, `./package.json`, `./locale/*.json`, `./src/*` |
| `files` | exactly `lib/index.js`, `lib/types/**/*.d.ts`, `locale/*.json` (+ declared runtime assets); never `src` or maps |
| `peerDependencies` | `@deepseek-ai/cordis: ~4.0.5` plus every dsh *service definition* consumed (`dsh-agent`, `dsh-llm`, `dsh-session`, `dsh-session-projection` for time-context), pinned to the exact runtime version `0.2.1-rc.2` |
| `devDependencies` | mirror of peers plus test-support (`dsh-agent-loop-testkit`, `dsh-loader-smoke`) |
| `dependencies` | `@deepseek-ai/schemastery`, small utils (`dsh-util-values`), `zod` for durable-state schemas |
| `locale/en.json` … | `meta.title`, `meta.description`, `meta.detail`; first-party must ship `zh ja es fr de ru pt`; optional `icon` ≤ 256 KiB |
| `dsh` manifest field | `dsh.bundle.patch` for bundles, `dsh.profile` for profiles, `dsh.client` for browser plugins (`dsh-package-manifest` readers) |

Rule from `packages/README.md`: "Extension plugins depend on Service Definitions, never concrete providers."

### 2.4 Entry rows and `disabled` expressions

A plugin is *used* by a YAML entry row. Field set (from the generated schema `$defs.entryMetadata`): `id`, `name` (module specifier; absolute paths and patch-relative `./` paths are converted to file URLs), `config`, `group`, `disabled` (boolean or `!!js` expression; "The Loader coerces other truthy values as disabled"), `inject`, `intercept`, `isolate`. Real examples from `dsh-base/cordis.patch.yml` and `dsh-web-app/cordis.patch.yml`:

```yaml
- id: plugin-manager
  name: '@deepseek-ai/dsh-plugin-manager'
  disabled: !!js "!ctx.get('profileContext')"          # only when launched by dsh (service present)
- id: bash-sandbox
  name: '@deepseek-ai/dsh-bash-sandbox'
  disabled: !!js process.platform === 'win32'          # platform gate; pwsh twin has the inverse
- id: session-persistence-jsonl
  config: { root: !!js dshHomePath('sessions') }       # helper injected into the YAML dialect
- id: webserver
  name: '@deepseek-ai/dsh-host-webserver'
  inject: [webStartup]                                 # row waits for the startup service...
  config: { host: !!js ctx.webStartup.host ?? '127.0.0.1', port: !!js ctx.webStartup.port ?? 3080 }
```

The `inject` on a *row* (not on the module) is how a mode bundle makes `!!js` config expressions evaluate only after an app-owned startup service exists ("Rows configured from flags inject that service, so Loader resolves their expressions only after it exists"). Failure semantics for expressions are in the table in §5.4.

---

## 3. Profiles: ordered bundles + patch layers, exact precedence

Sources: public `@deepseek-ai/dsh` README and `lib/profile-boot-*.js` JSDoc, `dsh-app-boot/README.md` "Profiles", `dsh-base/README.md`, and the public design note `.agents/notes/implemented/architecture/2026-08-05-profile-plugin-bundles.md`.

### 3.1 On-disk shape

A profile directory contains a package manifest with `dsh.profile.bundles`, a profile patch and the empty root that the launcher prepares. The following is an illustrative layout, not a workstation inventory:

```
package.json          manifest: dsh.profile.bundles names the ordered bundles
cordis.patch.yml      profile-level user patch
cordis.yml            empty root `[]`, prepared by the launcher
pnpm-workspace.yaml   package-manager settings and build-script allow-list
compatibility.json    optional exact-version exemptions, see §4.3
.plugin-manager/      package-operation state and logs
```

`$DSH_HOME` is the documented storage root for the home-level patch and runtime directories such as `sessions/`, `storages/` and `logs/`.

A **bundle** is an npm package with `"dsh": { "bundle": { "patch": "./cordis.patch.yml" } }`; `patch` may be an ordered list of files — `dsh-web-app` declares five (`cordis.patch.yml`, `presets/standard.patch.yml`, `presets/ptc.patch.yml`, `presets/minimal.patch.yml`, `presets/cordis.patch.yml`). `dsh-base/lib/index.js` "carries no runtime API": the bundle *is* the YAML. `dsh-base` lists ~100 plugin packages as `dependencies` so that installing the bundle installs the code; the patch is a single `- insert: [...]` of ~120 rows.

### 3.2 Composition order (precedence, lowest to highest)

Confirmed in three places (CLI README, `profile-boot` JSDoc of `composeProfile`, app-boot README). Later layers win; within one layer, later rows win ("last write wins per row").

| # | Layer | Notes |
|---|---|---|
| 0 | Empty root `[]` (`cordis.yml`) | exists only to anchor the Loader's `baseUrl` at the profile dir |
| 1 | Each bundle's patch list, in `dsh.profile.bundles` order; within a bundle, its `patch` files in declared order | only *direct* bundles contribute ("Transitive bundle auto-application" was rejected) |
| 2 | Profile user layer `$DSH_HOME/profiles/<name>/cordis.patch.yml` | what the Web plugin page and `dsh plugin` toggles write |
| 3 | Home user layer `$DSH_HOME/cordis.patch.yml` | "machine-local preferences that apply to every profile, so it outranks the per-profile layer" |
| 4 | `--patch <file>` overlays, in argv order | launcher-owned; never persisted |
| 5 | Telemetry switch | `DSH_TELEMETRY_DISABLED` (any non-empty value) patches the OTel row `disabled`; "config cannot disable a row" |

Patch semantics that matter when porting:

- A patch row targets an existing row **by `id`** and **replaces the whole `config`** ("does not deep-merge, so a profile override restates the bundle fields it keeps"). `name` on an override row is an assertion, not a rebind.
- `- insert: [rows]` adds rows; inserted relative module paths are anchored beside the patch file.
- A patch naming an unknown id prints a stderr warning; an empty or comments-only patch file **fails boot** ("disable the layer with `[]` instead").
- `preset` patches "operate inside `config.plugins` with the same precedence" (nested Loader trees for agent presets, `docs/architecture.md`).
- The Web plugin-manager toggle records only departures from the bundle layers: an override holding only `id`+`disabled` is removed when the bundles already give the entry that state; enabling a bundle appends it at the end of `dsh.profile.bundles`, "which can change configuration precedence".
- Row order carries no load semantics (see §1.1); `--dump-default-config` groups rows under comments naming the source and overriding layers, e.g. `# == @deepseek-ai/dsh-base, patched by @deepseek-ai/dsh-web-app`. The row set depends on the pinned bundle versions.

### 3.3 Resolution: two anchors, one immutable runtime table

Bundle names in `dsh.profile.bundles` resolve from the dsh installation first, then from the profile's own `node_modules` ("in-box bundles always match the running installation and pnpm never manages them"). Before mounting rows the launcher computes "one immutable runtime resolution from the installation and ordered bundle dependency graphs" and installs it into Node's ESM/CommonJS resolvers; runtime entries occupy `$DSH_HOME/profiles/node_modules` as an ordinary ancestor. A successor table is published after package operations; a package that moved directories needs removal + reinstall (`ResolutionRestartRequired`, `package-moved`).

### 3.4 Templates and recovery

`PROFILE_TEMPLATES` ships `web`, `headless`, `sdk`, `sdk-minimal`, `acp`; they auto-initialize on first use. `dsh <custom> --from-default-profile web` copies only the template's bundle list into a staging dir `.<name>.staging-*`, renames it into place, and deletes the previous directory only after the rename; `desktop` is reserved for Electron. `sanitizeProfile()` renames a broken `cordis.patch.yml` to `.bak-<timestamp>` without loading plugins.

---

## 4. Versioning, installation, compatibility, plugin manager

Sources: `@deepseek-ai/dsh/package.json`, npm registry (`npm view @deepseek-ai/dsh`), `dsh-plugin-manager/README.md`, `dsh-app-boot/README.md`, `dsh plugin --help`.

### 4.1 Versioning

All first-party packages are released in lockstep (every `dsh-*` here is `0.2.1-rc.2`; `peerDependencies` pin the exact version, not a range), while the vendored framework pieces use tilde ranges (`@deepseek-ai/cordis ~4.0.5`, `cordis-plugin-loader ~1.0.6`, `schemastery ~3.18.5`). The CLI's `engines.dsh`-equivalent runtime version is read from `dsh-app-boot`'s own manifest (`getDshRuntimeVersion()`), shown by `dsh --version`. Release cadence on npm: ~2 rc/alpha publishes per week since 2026-08-10, three dist-tags (`alpha`, `latest`, `next`).

### 4.2 Installation

`dsh plugin --profile <name> <pnpm args>` forwards verbatim to pnpm inside the profile directory ("a thin pnpm forwarder that initializes a base-backed profile and reconciles `dsh.profile.bundles` with installed bundle declarations"). A package without `dsh.bundle` stays a plain dependency and activates nothing ("explicit, no spooky scan"). The in-process `ctx.pluginManager` service (and the `plugin_manager` tool, disabled unless `danger-full-access`/approval) does the same through `operations.ts` with: registry lookup via `pnpm view` with ordered fallback registries (default fallback `https://registry.npmmirror.com/`; a private registry is never allowed to fall through to a public one), a `git ls-remote` reachability check for GitHub specs, `saveExact: true` for registry targets, `link:` targets for source checkouts, streamed install logs under a `requestId`, cancellation, pnpm 11 build-script approval (`approvedBuilds`), and a profile write lock shared with the CLI (`lockWaitMs` 120 s, `idleTimeoutMs` 600 s). Failure handling restores `package.json` and `pnpm-lock.yaml` from a snapshot taken before pnpm ran.

### 4.3 Compatibility gate (`engines.dsh`)

Exact rule (`dsh-app-boot/README.md`, "Profiles"):

1. If the plugin manifest declares `engines.dsh`, that semver range alone decides; `peerDependencies` are ignored.
2. Otherwise every `peerDependencies` range on `@deepseek-ai/dsh` or `@deepseek-ai/dsh-*` must match the runtime version (`workspace:^|~|*` count as the runtime).
3. Neither present → no constraint. Empty/invalid range, or `workspace:` inside `engines.dsh` → incompatible. Non-object `engines` or non-string `engines.dsh` → malformed manifest.

Where it runs: at install (`add`/`install` with specs — registry and local specs are judged before pnpm runs so nothing downloads; git/tarball specs are judged after fetch with automatic rollback), at every profile startup and recomposition ("Admission happens only at composition boundaries DSH owns"). A denied ordinary row becomes a detached `disabled: true` row in the launcher's copy of the composition (no file is rewritten); a denied bundle is skipped and listed in `skippedBundles`; a native include that reaches a denied plugin is omitted whole. Exemptions: `compatibility.json` maps exact `package@version` → list of exact runtime versions; set with `dsh plugin --profile p allow-version pkg@v --dsh-version r --accept-risk` or the tool's `set_version_exemption` (requires `acceptRisk: true`); neither plugin nor runtime upgrades inherit a grant. "These checks are not a sandbox against malicious package code." Whether a particular published plugin declares `engines.dsh` or relies on peers must be checked in that package's manifest.

### 4.4 Manager operations and HMR interplay

| Operation | Effect on files | With HMR | Without HMR |
|---|---|---|---|
| toggle entry | edits/append `disabled` in last matching override of profile `cordis.patch.yml` | applies live | restart |
| enable/disable bundle | edits `dsh.profile.bundles` (disable keeps dependency) | recomposition replaces running rows | `restart-required` |
| install bundle | pnpm add + append to bundles | selection after pnpm succeeds; `already-installed` refusal | publishes resolution; activation at restart |
| remove bundle | deselect → unload → `pnpm remove` → publish resolution | live | deselected startup bundle keeps running |
| version exemption | writes `compatibility.json` only | `applied` on next composition | `restart-required` |

Every completed operation emits `plugin-manager/changed`. `restart-required` always carries a `restartReason` (`no-hmr`, `package-moved`, or the HMR pending reason).

---

## 5. Testing plugins in isolation

Sources: Cordis README quick start, `packages/test-support/README.md`, `packages/test-support/{agent-loop-testkit,loader-smoke}/README.md`, `docs/testing.md`, `dsh-app-boot/README.md` failure table, `apps/cli` README (web failure matrix).

### 5.1 Unit level: mount the plugin on a bare `Context`

The Cordis quick start is the isolation primitive: `const root = new Context(); await root.plugin(Counter); await root.plugin(greeter); ... await root.fiber.dispose()`. Because `inject` gates activation, a plugin under test is mounted with only the services it declares, and `await ctx.plugin(...)` rejects on config or startup errors. `docs/testing.md` makes one unit test mandatory for every registry-shaped plugin: "Every registry gets an HMR-safety test (dispose the contributing fiber, assert cleanup)." Coverage gate is "per-file 100% on `packages/*/*/src`"; the stated philosophy is "An uncovered line is often dead code the gate flags for deletion, not a missing test to bolt on."

### 5.2 Test-support packages (`packages/test-support/`, "deterministic, keyless ways to exercise the real product")

| Package | Role (quoted from the group README) | Isolation technique |
|---|---|---|
| `agent-loop-testkit` | "shared prerequisite services for tests that exercise the concrete AgentLoop" | `mountAgentLoopTestDependencies(ctx)` mounts six real services in fixed order (LLM, session, session-projection, system-prompt, tools, agents) and stops before `AgentLoop`; "The harness mounts no LLM adapter"; stubs `createInboxStub()`, `unsupportedInbox()`, `unsupportedModel()` |
| `tool-testkit` | "tool fixtures and exercises invocation or executor behavior" | fixtures for the tools pipeline |
| `llm-replay` | "Replays recorded model streams for keyless tests and demos" | an LLM *plugin* that replaces the provider adapter |
| `llm-mock-server` | "scriptable OpenAI-compatible fault server for recovery tests" | network-level fault injection |
| `loader-smoke` | "Boots Loader-composed applications and drives fixture turns for smoke tests" | `runLoaderSmoke({ binScript, configPath, expectedExitCode })` spawns the real bin with its own `DSH_HOME` in a temp dir; `runFixtureTurn(ctx)` drives one turn in-process and "returns the final assistant text plus token usage" |
| `session-snapshot` | "session-log snapshot support and protocol adapters for profile-driven tests" | record/replay tier |
| `remote-mock` | "endpoint-named Typert Remote mock and the Connection carrier face whole-client specs install" | browser-side RPC mock |
| `client-runtime` | "jsdom slot test bench for browser feature specs" | client plugin bench |

### 5.3 Policy: isolation is necessary, composition is required

`docs/testing.md`: "Product-visible plugins require a non-unit REAL-composition test." and "Hand-built `ctx.plugin(...)` suites are insufficient" — the required form is to "boot test-only `cordis.yml` through Loader and app/process, mock only external services or nondeterministic inputs" and assert model-visible requests, durable state, or user output. Mock rule: "Mock only the expensive or non-deterministic boundary (LLM adapter, network, clock); keep everything downstream real." Verification rule: assertions must re-read the world (files, commands), because "a keyword probe on the agent's own output lets a cheating agent pass." Tiers: unit → coverage gate → keyless *expected* process tests (`*.expected.e2e.ts`) → recorded-session *snapshot* tier (`snapshot.yml` declares profile, header class, recording policy; `DSH_SNAPSHOT=replay` enforced in CI) → real-API e2e (self-skip without `DEEPSEEK_API_KEY`; "do not ration real-API tests") → benchmarks → web browser snapshots. Profile-level integration tests live under `apps/cli/tests/profiles/`; the design note records that the keyless web e2e scaffold "boots the same bundle layers over the same empty root with the same package-selection rules, so test/product drift fails loudly."

### 5.4 The failure matrix (what a plugin failure does to the app)

`apps/cli/tests/profiles/web/tests/web-failure-matrix.expected.e2e.ts` "runs the built CLI through startup failures and native configuration HMR … without model API calls". The policy it verifies (`dsh-app-boot/README.md`, condensed):

| Failure | Optional entry at startup | Required entry at startup | Later config HMR |
|---|---|---|---|
| root config / required overlay missing, malformed, invalid entries | stop startup | stop startup | reject patch, keep running config |
| module import fails / evaluation throws | warn; continue | stop | report; keep siblings; corrected import activates |
| config schema validation fails | warn; continue | stop | new entry inactive; existing keeps prior instance |
| `!!js` in config or `disabled` throws | warn; continue | stop | report (never treated as disabled) |
| sync / async `apply()` throws | warn (after settlement); continue | stop | report; keep siblings |
| injected service unavailable | warn; wait | stop | keep waiting; adding provider activates |
| HTTP port bind fails | warn; continue without endpoint | stop | keep running without endpoint |
| detached unhandled rejection / uncaught sync exception | **fatal**: dispose app, exit non-zero | fatal | fatal |
| entry absent or explicitly disabled | ignore | ignore | ignore |

"Required" is a global id list owned by app-boot: `agent-loop`, `webserver`, `web-runtime`, `modules`, `connection`, `headless-runner`, `acp`, `sdk-jsonrpc-server`. Everything else is best-effort: the app starts with every plugin that can activate and prints one grouped diagnostic for the rest. `installFailLoud` turns any detached rejection into exit 1 "because only the throw site knows which state is intact."

---

## 6. Cross-cutting concerns are plugins

Source: `dsh-base/cordis.patch.yml` rows and the README summaries of each package. The recurring shape is the three-role **seam** from `docs/architecture.md`: "A seam has three roles — Service Definition, Service Provider, Consumer — and all three must exist. Swapping one provider moves the whole product."

| Concern | Definition (service key) | Shipped provider(s) | Consumer / model surface | Notes |
|---|---|---|---|---|
| Telemetry | `dsh-otel` → `ctx.otel` ("Mounting alone creates no transport or identity and sends nothing") | channels created by consumers: `dsh-session-telemetry-otel` (session log, `FEEDBACK_ONLY` default, 4 MB request cap), `dsh-host-product-telemetry-otel` (desktop only) | none model-visible | each channel owns exporter/queue/shutdown; "Register shutdown with the consumer's Cordis fiber" |
| Approval | `dsh-user-approval` → `ctx.approval`; `approval/request` waterfall; policy `ask`/`never`; fails closed (`unavailable`) when no terminal answerer | Web UI answerer (`dsh-client-ui-approval`), headless has none | tools pipeline and sandboxed bash route `ask` decisions here | every request/outcome audited into the session log; "the service itself never prompts a human" |
| Sandbox | `dsh-sandbox` (definition) + `dsh-sandbox-policy` (per-call mode + workspace root, modes `read-only`/`workspace-write`/`danger-full-access`) | `dsh-sandbox-local` (bwrap/Landlock/Seatbelt; Windows ACL via `dsh-sandbox-windows-acl`), `dsh-bash-sandbox`, `dsh-fs-sandbox` | `tool-bash`, `tool-fs`, terminals | `dsh-permission-presets` maps a preset to (sandbox, approval) pairs; mounting `dsh-fs-local` next to `dsh-fs-sandbox` double-registers `ctx.fs` and fails the profile |
| Storage | `dsh-storage` hub (`ctx.storage.<form>`), `dsh-storage-domain` (`ctx.storageDomain`, schema-validated) | `dsh-storage-json` (`root: dshHomePath('storages')`); sqlite backend exists for session query | `dsh-workspace`, `dsh-session-projection-cache`, `dsh-schedule` | per-domain backend routing; stable `StorageError` codes (`backend-not-found`, `duplicate-backend`, …) |
| Background jobs | `dsh-jobs` contract (`ctx.jobs`) | `dsh-jobs-local` (in-process, per-owner concurrency cap 10, ring-buffer retention) | `dsh-tool-jobs` (`job_output`, `job_list`, `job_kill`) | "records are in-memory, so a durable or cross-process backend must implement the same contract differently" |
| Scheduling | `dsh-schedule` service + `schedule` Remote namespace | same package (storage-domain backed; cron/daily/weekly/every/at/after) | `dsh-tool-schedule` tools; `dsh-client-ui-schedule` | mounted only in Web (needs the Session controller to deliver) |
| Credentials / auth | `dsh-credentials` definition, `dsh-authorization` | `dsh-credentials-local` (`$DSH_HOME/.credentials.yaml` → env → `.env` chain), `dsh-deepseek-account-platform` | LLM adapters resolve references per request | headless disables credentials/authorization/account rows |
| Settings / config editing | `dsh-settings`, `dsh-config-editor` (persist through the profile patch) | — | Web settings cards | gated on `ctx.get('profileContext')` |
| Persistence of sessions | `dsh-session` (append-only log; "Model-visible means logged") | `dsh-session-persistence-jsonl`, format migrations `v0→v1…v3→v4` as separate packages | everything | durability checkpoints via `dsh-session-checkpoint-policy` |
| Model access | `dsh-llm` (`ctx.llm`, deep-frozen requests) | `dsh-llm-deepseek-api-key`, `dsh-llm-deepseek-account`, `dsh-llm-pi-ai` (dormant until settings supply providers) | agent loop | retry is its own plugin `dsh-llm-retry`; compaction (`compaction-basic`, `tool-result-pruner`, `image-offload`) and `token-meter` are plugins too |
| Reload | `dsh-hmr` | — | plugin manager, config editor | disabled in headless/sdk/acp bundles |
| Plugin management | `dsh-plugin-manager` → `ctx.pluginManager` | — | `plugin_manager` tool, Web page | cannot remove itself |
| RPC to the browser | `dsh-typert-registry` (generated reflection, lazy Zod factories, fiber-scoped registrations), `dsh-typert-protocol` (`@Remote` decorator), `dsh-api-gateway` (`ctx.typertGateway` / client `ctx.remote`), `dsh-api-remote-access` | — | all `dsh-api-*-controller` rows | Typert artifacts are generated from TypeScript at build time (`scripts/generate-typert.ts`) |

Two further patterns worth copying: `dsh-scope` (a dependency-free library for per-agent contribution sets with inherited lookups and scoped disposal) and `dsh-agent-preset` (an agent's child plugins declared "in ordinary Cordis YAML", one nested Loader tree per preset; sessions select a preset by id).

---

## 7. Apps are profiles

Sources: `PROFILE_TEMPLATES` usage in `profile-boot`, bundle patches of `dsh-web-app`, `dsh-headless`, `dsh-sdk-app`, `dsh-acp-app`, `dsh-sdk-minimal`, `dsh-cmdline` README, design note 2026-08-05.

The CLI README states it plainly: "The `dsh` command is the sole supported Node application launcher … SDK and ACP are profiles, not separate public bins." The design note records the before/after: compositions were baked into `apps/cli` with three entry modes and a per-surface `AppCLIEntry`; after the change "`apps/cli` shrank to argv parsing, profile machinery, and the pnpm forwarder" and a `verify-application-entrypoints` script rejects any bin that bypasses `dsh`.

| Profile template | Bundles | What the mode bundle does (from its `cordis.patch.yml`) |
|---|---|---|
| `web` | `dsh-base`, `dsh-web-app` | inserts `web-startup` (parses `--port/--host/--no-open`, provides `webStartup`), `webserver` and `web-runtime` rows injecting it, all `dsh-api-*` controllers, all `dsh-client-ui-*` browser plugins, `schedule`, `workspace`; disables base-level tool rows (`tool-bash`, `tool-fs`, …) because the four agent presets (`standard`, `ptc`, `minimal`, `cordis`) re-mount tools per agent; HMR stays on |
| `headless` | `dsh-base`, `dsh-headless` | inserts `headless-startup` (task positional, `--session-id`, `--json`) and `headless-runner` with `inject: [headlessStartup]`; disables `hmr`, `credentials`, `authorization`, `deepseek-account` |
| `sdk` | `dsh-base`, `dsh-sdk-app` | inserts `sdk-app-startup` and `sdk-jsonrpc-server` (`inject: [sdkAppStartup, loader]`); stdout belongs to JSON-RPC; disables `hmr`, `session-title-llm` |
| `acp` | `dsh-base`, `dsh-acp-app` | inserts `acp-app-startup` and `acp` (`inject: [acpAppStartup]`, pinned provider/model); disables `hmr` |
| `sdk-minimal` | `dsh-sdk-minimal` only | "this insert is the complete Cordis tree" — a 20-row standalone composition that does not layer over base |
| `desktop` | Electron-owned | reserved name; the CLI refuses it; Electron bundles its own dsh runtime and uses the same `profile-boot` export |

The shared launcher contract (`runProfile` in `profile-boot`): compose layers → `boot(name, rootConfig, patches, prepare)` where the `prepare` hook, *before any config-tree entry mounts*, provides `profileContext` (locations, started bundles, overlays, telemetry flag), the launch-environment snapshot, the `PluginPackages` runtime resolution, and `cmdlineArgs`/`appExit`/`ready` via `provideCmdline`. Readiness (`appReady.commit()`) is signalled only when the root fiber is `ACTIVE` and the `loader` service exists; SIGINT/SIGTERM run a bounded 5 s escalating shutdown that disposes the fiber tree. An app therefore differs from another app only by (a) which bundles are listed and (b) which startup plugin parses the leftover argv — the design note's rejected alternative was a "pre-boot `context` module in the bundle manifest", refused so that "composition [stays] dumpable and the manifest data-only."

The browser side follows the same model: `dsh-client-ui-*` packages are Cordis plugins that run in the page (separate `tsconfig.client.json` aggregate because "both sides declaration-merge the cordis `Context` interface with different services"), are shipped to the browser by the `modules` row and talk to the Host through Typert RPC over the `connection` row.

---

## 8. What to port to a Go/Python server platform, and what is JS-specific

### 8.1 Principles to port (language-neutral)

1. **One context tree, services by name, declared dependencies drive activation.** Plugins declare `inject` (needs) and `provide` (gives); the runtime computes activation from availability, not from file order. Replacing a provider restarts its consumers automatically. This is the core of "test independently before attaching": a plugin is mountable with just the services it names.
2. **Every effect returns a disposer; disposal is complete and reverse-ordered.** Registering a tool, a listener, an HTTP route, a cron job, a metric exporter all go through one `effect` API so unloading is total (the paper's temporal composability). Mandatory per-registry test: dispose the contributing fiber, assert cleanup.
3. **Configuration is an ordered stack of id-addressed patch layers over an empty root.** Bundles → profile → home → CLI overlays; a patch replaces a row's whole config; a dump command shows the composed result and its provenance before booting; a schema dump shows what each row accepts. No deep-merge magic.
4. **Bundles are data-only manifests + code; apps are profiles.** A "surface" (web, headless, slack bot, API server) is one mode bundle stacked on a shared base, differing only in its startup plugin and rows. No per-app binaries.
5. **Definition / Provider / Consumer seams**, and "extension plugins depend on Service Definitions, never concrete providers." Keep definition packages tiny and stable; ship providers separately (`jobs` vs `jobs-local`, `storage` vs `storage-json`, `llm` vs `llm-deepseek`).
6. **Compatibility is enforced at composition boundaries the platform owns** (install, boot, recomposition) with one rule (`engines.<platform>` else peer ranges), explicit exact-version exemptions, and no silent fallback.
7. **Explicit failure policy**: a small list of *required* entries stops boot; every other failure degrades to a warning; detached exceptions are fatal. Publish that table and test it with a failure matrix that boots the real artifact.
8. **Testing tiers**: bare-context unit mount → real-composition smoke through the loader with a test-only config file → record/replay of the non-deterministic boundary (LLM, network, clock) → real-API e2e that self-skips without keys. "Hand-built suites are insufficient" for product-visible plugins.
9. **"Model-visible means logged"** → for AIOps, *operator-visible means logged*: every fact an agent acts on (alert, metric query result, approval) is an append-only event with projections derived from it; approvals and tool outcomes are audited into the same log.
10. **Plugin metadata is first-class**: localized title/description/icon, per-row config schema, a package inventory service, a plugin manager that is itself a plugin and never removable.

### 8.2 JS-specific parts to replace

| dsh mechanism | Why it is JS-specific | Go/Python replacement |
|---|---|---|
| `Context` as a `Proxy`; services appear as `ctx.<name>` via TypeScript declaration merging | relies on dynamic property interception and TS interface merging | explicit typed lookup: `ctx.Service[T](name)` / `ctx.get("llm")` with an interface assertion in Go; `ctx["llm"]` with Protocol typing in Python |
| `!!js` YAML expressions (`process.platform`, `ctx.webStartup.port`, `dshHomePath()`) | evaluates JavaScript at compose time | a small, sandboxed expression language (CEL, `expr`, Starlark) or only env/service references; keep the "row `inject`s the service that its config reads" rule |
| pnpm-managed `node_modules` per profile + Node resolver hooks for the runtime table | npm ecosystem | Go: plugins compiled into the binary and selected by composition (Go `plugin` is unsuitable), or out-of-process via `hashicorp/go-plugin` (gRPC) when isolation is required; Python: `importlib.metadata` entry points + a per-profile venv/`uv` project. The "install = package manager in the profile dir" pattern still applies (`uv add`, `go get` at build) |
| ESM module HMR (`dsh-hmr`, chokidar) | in-process module replacement | server-side: config-only hot reload (recompose rows) and rolling restarts; module HMR is not needed for a server |
| Schemastery / Standard Schema `Config`; JSON Schema projection | TS-first validators | Go: `cue` or JSON Schema from struct tags (`invopop/jsonschema`); Python: pydantic models with `model_json_schema()` — keep the "schema is exported by the plugin and dumpable for the UI" property |
| Typert (RPC descriptors generated from TypeScript types; `@Remote` decorators) | TypeScript compiler analysis | gRPC/Connect or OpenAPI generated from the same schema the plugin exports |
| Host/Client split with browser-side Cordis plugins | the UI is also a plugin tree | optional; for a Grafana-ecosystem product the UI is Grafana app plugins + Slack, so only the Host tree needs the model |
| In-process plugins with no isolation (sandbox is for *agent tools*, not plugins) | trust model: plugin code runs with the process | same decision applies; third-party plugins in a multi-tenant server should run out-of-process (gRPC) or be reviewed |

### 8.3 Concept map: dsh → AIOps platform

| dsh concept | AIOps platform equivalent (proposal) |
|---|---|
| Cordis `Context` / fiber / `inject` / `provide` | `aiops` kernel: context tree, module lifecycle states, declared `requires`/`provides` of named services, reverse-ordered disposers |
| `ctx.plugin(mod, config)` | `kernel.Mount(module, config)` returning a handle with `Await()`/`Dispose()`/`Update()` |
| Service Definition / Provider / Consumer | `tsdb` (definition) ← `tsdb-mimir`, `tsdb-prometheus` (providers) ← `pillar-incident` (consumer); same for `logs`/`loki`, `traces`/`tempo`, `profiles`/`pyroscope`, `alerts`/`grafana-alerting`, `oncall`, `chat`/`chat-slack` |
| Waterfall hooks (`agent/pre-step`, `tools/pre-execute`, `approval/request`) | middleware chains on `alert/ingest`, `incident/pre-step`, `action/pre-execute`, `approval/request` — a listener that does not call `next()` vetoes |
| `dsh-user-approval` (fail-closed `ask`/`never`) | `approval` service with Slack and Web answerers; remediation actions fail closed when no answerer is composed |
| `dsh-sandbox-policy` + presets | `action-policy`: `read-only` / `remediate-with-approval` / `auto-remediate`, per tenant and per session |
| `dsh-session` append-only log, projections | `casefile`: append-only incident/change/compliance event log per tenant; projections for dashboards and Slack threads |
| `dsh-storage` hub + `storage-json` / sqlite | `store` hub + `store-postgres` / `store-sqlite`; domains per pillar |
| `dsh-jobs` / `jobs-local` + `tool-jobs` | `jobs` contract + in-process provider now, durable queue provider later (the README itself notes this seam) |
| `dsh-schedule` (cron/daily/weekly, durable) | `schedule` for compliance scans, FinOps reports, executive reports |
| `dsh-otel` channels | `telemetry` service exporting the platform's own OTLP to the hosted stack |
| `dsh-llm` + adapters + `llm-retry` | `llm` definition; `llm-openai-compatible`, `llm-anthropic`, `llm-deepseek` adapters; retry as a separate plugin |
| `dsh-tools` registry + `defineTool` | `tools` registry per agent: PromQL/LogQL/TraceQL query tools, krr/robusta/kube tools, runbook tools |
| `dsh-mcp-client` | `mcp-client` for customer-provided MCP servers (Grafana MCP, cloud MCPs) |
| `dsh-skill` + `skill-filesystem` | `runbooks` catalog: SOPs/runbooks from Git, filesystem, remote |
| `dsh-agent-preset` (nested YAML plugin tree per agent) | one preset per pillar agent (incident, change, compliance, secops, finops, improvement, exec-report) selectable per Slack channel / tenant |
| `dsh-subagent` delegation providers | launching specialist agents, with explicit context-inheritance semantics |
| Bundle (`dsh.bundle.patch`) | `aiops.bundle.patch` in the module manifest; e.g. `aiops-base`, `aiops-grafana-stack`, `aiops-slack`, `aiops-oncall`, `aiops-standalone-datasources` |
| Profile (`dsh.profile.bundles` + `patch.yml`) | `$AIOPS_HOME/profiles/<name>`: `server` (hosted multi-tenant), `self-hosted`, `headless` (CI/cron), `slack-bot`, `sdk` |
| `dsh-base` / mode bundles | `aiops-base` + `aiops-server`, `aiops-headless`, `aiops-slack` |
| `--dump-config`, `--dump-config-schema` | `aiops --dump-config`, `--dump-config-schema` (feed the settings UI and docs) |
| `engines.dsh` + `compatibility.json` | `engines.aiops` + exemptions file; checked at install/boot/recompose |
| `dsh-plugin-manager` (service + tool + UI) | `plugin-manager` service, Slack/Web commands, never self-removable |
| `dsh-hmr` | config-only live recomposition; no module HMR |
| `dsh-typert-*` / `api-gateway` | gRPC/Connect gateway generated from exported schemas |
| test-support (`agent-loop-testkit`, `llm-replay`, `loader-smoke`, failure matrix) | `testkit` (mount prerequisite services in order), `llm-replay`, `loader-smoke` (boot a test-only profile from the built binary), startup failure matrix |
| Required-entry list (`agent-loop`, `webserver`, …) | `api-server`, `alert-ingest`, `tenant-router`, `oncall-scheduler` required; everything else best-effort |
| "Model-visible means logged" | "Operator-visible means logged": alerts, queries, decisions, approvals, remediation results are events |

---

## 9. Open questions and unverified items

- `docs/user/develop/{basic,framework,practice}` exist on `master` but their index pages returned 404 at the guessed paths; their content (the "developer practice guides" the CLI README cites) is **UNVERIFIED** and should be read before finalising the plugin-author guide.
- Cordis public docs: the `cordiverse/cordis` README links only to a "cordis-primer" site marked "under construction"; the tutorial/guide paths named in the vendored README (`docs/tutorials/build-a-plugin.md`, `docs/guides/plugin-lifecycle.md`) are repository-internal and were not fetched. Lifecycle facts above come from the vendored source, not from those guides.
- Use of `engines.dsh` in a particular first-party or third-party package — check the manifest at the selected version.
- `serial`/`parallel` event dispatch exist per `docs/architecture.md`; not read in `events.ts` — minor, **UNVERIFIED**.
- Schema projection of nested preset carriers at the chosen runtime version — **UNVERIFIED**.
- Cross-release-candidate profile compatibility is not guaranteed by the project's preview policy; check the exact runtime and bundle pins before relying on it.

---

## Sources

Public package material used in the original 2026-10-27 survey (dsh version 0.2.1-rc.2 unless stated):

- `@deepseek-ai/dsh`: README, package manifest, launcher and profile-boot modules.
- `@deepseek-ai/cordis@4.0.5`: README, manifest and shipped `src/{context,service,registry,fiber,reflect,events}.ts`.
- `@deepseek-ai/cordis-plugin-loader@1.0.6`, include and group packages: README and loader contract.
- `@deepseek-ai/dsh-app-boot`, `dsh-base`, `dsh-web-app`, `dsh-headless`, `dsh-sdk-app`, `dsh-acp-app` and `dsh-sdk-minimal`: public package READMEs, manifests and patch assets.
- The dsh plugin packages named in §§2–7: shipped README and API documentation. `@deepseek-ai/schemastery@3.18.5`: schema contract.
- CLI interfaces consulted: `dsh --version`, `dsh --help`, `dsh plugin --help`, `dsh web --dump-default-config`, `dsh web --dump-config-schema`; npm metadata for version, dist-tags and publish times. No local artifact path or current execution result is part of this note.

Web (fetched 2026-10-27):

- https://github.com/deepseek-ai/deepseek-harness — README ("Everything is a Plugin", developer-preview notice, MIT)
- https://raw.githubusercontent.com/deepseek-ai/deepseek-harness/master/docs/architecture.md
- https://raw.githubusercontent.com/deepseek-ai/deepseek-harness/master/docs/development.md
- https://raw.githubusercontent.com/deepseek-ai/deepseek-harness/master/docs/testing.md
- https://raw.githubusercontent.com/deepseek-ai/deepseek-harness/master/docs/cookbook/adding-a-package.md
- https://github.com/deepseek-ai/deepseek-harness/tree/master/docs/cookbook (listing)
- https://github.com/deepseek-ai/deepseek-harness/tree/master/docs/user and `/docs/user/develop` (listings only)
- https://raw.githubusercontent.com/deepseek-ai/deepseek-harness/master/.agents/notes/implemented/architecture/2026-08-05-profile-plugin-bundles.md
- https://github.com/deepseek-ai/deepseek-harness/tree/master/packages (group README summary)
- https://raw.githubusercontent.com/deepseek-ai/deepseek-harness/master/packages/test-support/README.md
- https://raw.githubusercontent.com/deepseek-ai/deepseek-harness/master/packages/test-support/loader-smoke/README.md
- https://raw.githubusercontent.com/deepseek-ai/deepseek-harness/master/packages/test-support/agent-loop-testkit/README.md
- https://github.com/cordiverse/cordis — README (default branch `main`, "A Meta-Framework of Spatiotemporal Composability")
- https://arxiv.org/abs/2608.25512 — "A Programming Paradigm for Spatiotemporal Composability" (Shi, Zhang, Cui; submitted 2026-08-26)
- https://registry.npmjs.org/@deepseek-ai/dsh — dist-tags and publish times

---

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
