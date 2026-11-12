# Plugin system, round 3: the literal option (TypeScript on Cordis and the dsh loader)

Status: historical round-3 proposal, 2026-11-10; the losing TypeScript option. The accepted record selects the Go port (`docs/architecture/00-decisions.md` §2.1 row 1). This option would maintain modified upstream source, not independently authored kernel and boot code.

Lens: build the control plane in TypeScript on Cordis and the dsh loader themselves, reusing dsh packages where their licence and stability allow. This draft argues that option as a complete design, states its costs before its answers, and ends with the cases in which it is the right choice and the cases in which it is not.

How to read. Section 0 is the position. Section 1 is the evidence read for this draft. Section 2 is what the option costs. Section 3 answers Q1 to Q16 in the order of the brief. Section 4 says when this option is the right answer and gives the spike that decides it. Section 5 lists what the record would have to change. Section 6 is the dsh property checklist. Section 7 lists what was not verified.

Citations. "00 section n" is `docs/architecture/00-decisions.md`; 03, 07 and 08 are the documents beside it. "dsh-architecture section n" and "plugin-architecture section n" are the notes under `docs/research/`. Source paths are relative to the named published `@deepseek-ai/*` packages at the versions below. Public repositories: `https://github.com/deepseek-ai/deepseek-harness` and `https://github.com/cordiverse/cordis`.

Rules obeyed. No product, plugin, package or row name below contains a mark of the observability stack (00 section 8.1). Every deferral names its trigger (00 principle 8).

---

## 0. Position

1. **The kernel reuses upstream source.** `@aiops/kernel` would maintain a local source tree based on pinned Cordis 4.1.0 (context, fiber, reflect, registry, events), the Cordis loader, include, group and timer packages, with the source modifications listed in Q1. `@aiops/boot` would maintain source modifications to `dsh-app-boot` (profiles, bundles, patch layers, dumps, the compatibility gate, the failure audit, fail-loud). The kernel proper is about 5,200 lines of MIT TypeScript; the boot adaptation adds an estimated 4,000 once the parts we do not need are removed (section 1). These remain upstream-derived components with their copyright, licence and change notices, not independent code. Everything else in aiops-platform is a plugin from the first commit of this losing option.
2. **Every dsh property the owner asked for exists on day one, as code and not as a port:** service-availability-driven fibers with epochs, reverse-ordered disposers, `isolate` and `intercept`, waterfall hooks, live recomposition through one queue, volatile config, the plugin manager as a plugin, the compatibility gate, the failure matrix. The three departures the owner withdrew (earned points, compiled-in plugins, fixed boot order) disappear without design work.
3. **Installing through plugin interfaces without core source changes is supported by the language.** A self-hoster runs `aiops plugin --profile self-host-full add <package>`; the loader imports the module at runtime. No distribution builder and no rebuild are needed for TypeScript plugins. This is the largest single advantage over any Go kernel.
4. **Tenancy uses dsh's own scoping primitives.** A tenant is a generated group row with `isolate` realms and `intercept` config. A plugin mounted in a tenant realm reaches the store already bound to that tenant. Enabling a row for one tenant is a real fiber start; disabling it unwinds every effect it made.
5. **Policy that must not be bypassed stays fail-closed by dependency, in the way `dsh-user-approval` does it.** Write-capable hubs inject the policy services, so a missing or failed policy plugin leaves them pending. The floor is enforced inside the policy service before and after its hook chain. Three small things stay in boot code, where dsh keeps them too: the required list, the admission check, the environment kill switches.
6. **The costs are real and are stated first (section 2).** We would own a vendored kernel whose upstream calls its API unstable and released 12 times in 12 weeks. Node gives one event loop per process, no preemption and no enforceable boundary between in-process plugins. River, slack-go, client-go and the Alloy syntax package are replaced. The Go-first contributors of the observability community are not the natural authors. The server ships as an image, not a static binary.
7. **Go and Python remain first-class plugin languages, out of process.** A remote plugin is a proxy fiber in the context tree, so activation, epochs and disposal work the same for a Go executor as for a TypeScript tool.
8. **The verdict is conditional and cheap to settle.** Section 4 gives five conditions and a three-part spike. If the owner ranks fidelity to dsh and runtime-installable plugins above alignment with the Go ecosystem of the data plane, and the spike numbers hold, this is the right answer. If the hosted multi-tenant service, the Go contributor pool and a static binary rank higher, a faithful Go port is the right answer, and this draft is then its executable specification: the vendored Cordis tests become the oracle the Go kernel is differentially tested against.

---

## 1. Evidence

| Fact | Source |
|---|---|
| Source inspection on 2026-11-10 used the published package `@deepseek-ai/dsh` 0.2.2-rc.1, with `@deepseek-ai/cordis` 4.1.0 and `cordis-plugin-loader`, `-include`, `-group` 1.1.0, `-timer` 1.2.0. Our research note of 2026-10-27 read 0.2.1-rc.2 with Cordis 4.0.5 and loader 1.0.6. | `dsh/package.json`, `cordis/package.json`; dsh-architecture header |
| Every package read is MIT. Cordis, loader, include, group, timer, cosmokit and schemastery carry "Copyright (c) 2021-present Shigma"; dsh packages carry "Copyright (c) 2026 DeepSeek". | each `LICENSE` and `package.json` |
| Core sizes: Cordis `src` 2,811 lines; loader 1,224; include 968; timer 153. `dsh-app-boot` ships as one built module of 5,315 lines and `dsh-plugin-manager` as 3,255 (built output, not source; both include parts this draft drops). | `wc -l` on the shipped `src` and `lib` trees |
| `@deepseek-ai/cordis` has 12 published versions between 2026-08-10 and 2026-11-04. Upstream `cordis` on npm is at 4.0.0-rc.12 (2026-10-21): upstream has not released a stable 4.0 and the DeepSeek-maintained source modifications are ahead of it. | `npm view`, read 2026-11-10 |
| Upstream README: "Cordis is under active development. The API is not yet stable and may change without notice." The dsh README: "developer preview ... THERE WILL BE COMPATIBILITY-BREAKING CHANGES." | cordiverse/cordis `packages/core/README.md`; deepseek-harness `README.md` |
| Semantics moved between 4.0.5 and 4.1.0. The epoch is now built from the uid of each injected provision (`epoch += ':' + impl.uid`), where our note recorded the provider fiber uid. 4.1.0 adds: providers retain their implementation until dependents finish unloading; a failed fiber recovers only through `update()`, "including after its dependencies change". | `cordis/src/fiber.ts` `_refresh`, `_unload`; `cordis/README.md` "Lifecycle completion" |
| A context read of a service the fiber did not inject throws `cannot get property "x" without inject`; a second `provide` of a name in one scope throws `service "x" has been registered at <...>`. | `cordis/src/reflect.ts` handler `get`, `provide` |
| `reflect.notify` walks every fiber of every runtime; a state change to or from ACTIVE walks every provision. Cost is linear in the size of the tree per notification. | `cordis/src/reflect.ts` `notify`; `fiber.ts` `_updateState` |
| Events have five dispatch modes: `emit`, `parallel`, `serial`, `bail`, `waterfall`. A waterfall listener that does not call `next()` vetoes the rest, the built-in behaviour included; `next()` twice throws. A service can intercept listener registration through the `internal/listener` bail event. | `cordis/src/events.ts` |
| Loader rows carry `id`, `name`, `config`, `group`, `disabled`, `inject`, `intercept`, `isolate`. `isolate: {name: true}` is an entry-local realm; a string label is a named realm shared by every entry using it. A patch `{id: <group>, insert: [...]}` inserts into that group. | `cordis-plugin-loader/src/config/{entry,isolate}.ts`; `cordis-plugin-include/src/patch.ts` |
| `!!js` row expressions are evaluated with `new Function('ctx','expr', 'with (ctx) { return eval(expr) }')`. | `cordis-plugin-loader/src/config/utils.ts` |
| The loader works without Node internals (the native addon `node-addon-require-builtin` is an optional peer). Module replacement in `dsh-hmr` and the runtime resolution table in `dsh-app-boot` need them: "Runtime resolution depends on Node internals: supported Node versions require the native builtin-access addon." | `cordis-plugin-loader/README.md` and `package.json`; `dsh-app-boot/README.md` Known Limitations; `dsh-hmr/README.md` |
| Reload: module replacements, include refreshes and profile changes share one queue (`runExclusive`, nested transactions rejected); package installs run outside it. "Later configuration HMR does not repeat the required-startup audit and does not roll back the whole update." | `dsh-hmr/README.md`; `dsh-app-boot/README.md` |
| Required list in 0.2.2-rc.1: `agent-loop`, `tool-executor-native`, `tool-executor-ptc`, `webserver`, `web-runtime`, `modules`, `connection`, `headless-runner`, `acp`, `sdk-jsonrpc-server`. "Required ids absent from a profile and disabled required entries do not affect startup." | `dsh-app-boot/README.md` |
| Approval as a plugin: consumers "fail closed when it is absent"; "the `never` policy is enforced inside the service before waterfall dispatch, so a listener registered later with `prepend` cannot bypass the deterministic rejection"; a failed audit append rejects "instead of returning an unlogged decision"; "sibling listener order is not a policy-priority mechanism". | `dsh-user-approval/README.md` |
| dsh documents `--dump-default-config` and composes preset rows with `isolate` groups. Machine-specific output is not a retained public artifact or an S0 measurement. | `dsh-app-boot/README.md`; `dsh-base/cordis.patch.yml`; source inspection 2026-11-10 |
| The test-support packages (`agent-loop-testkit`, `loader-smoke`, `llm-replay`) are not in the installed tree; they are development dependencies in the repository. dsh has no per-definition conformance suite as a named tier. | directory listing; grep over the READMEs |
| Node 26 is LTS since 2026-10-28 (end of life 2029-04-30); Node 24 is in maintenance until 2028-04-30. Single executable applications are "Stability: 1.1 Active development". The permission model is stable since 22.13 and 23.5 and is process-wide. | nodejs/Release `schedule.json`; nodejs.org API docs, read 2026-11-10 |
| Replacement libraries exist under permissive licences: pg-boss 12 (MIT), `@connectrpc/connect-node` 2 (Apache-2.0), `@kubernetes/client-node` 2 (Apache-2.0), `@slack/web-api` 8 and `@slack/socket-mode` 3 (MIT), `@modelcontextprotocol/sdk` 1 (MIT), `jose` 6 (MIT), `@opentelemetry/sdk-node` (Apache-2.0). | `npm view`, read 2026-11-10 |
| TypeScript control planes with in-process plugins exist at scale: Backstage (Apache-2.0, TypeScript) and OneUptime (incident management and on-call, TypeScript). | GitHub API, read 2026-11-10; plugin-architecture section 2.3 |

---

## 2. What the literal option costs

| Cost | What it is, exactly | Mitigation, and what remains |
|---|---|---|
| **Cordis API stability** | Upstream says the API may change without notice; the DeepSeek-maintained package we would start from released 12 times in 12 weeks and changed activation semantics between 4.0.5 and 4.1.0. | Maintain a pinned local source tree, as dsh does (`vendor/cordis`), publish under our scope with the upstream copyright, licence and change notices, and take upstream changes by deliberate sync with the upstream tests as the gate. What remains: we maintain about 9,000 lines of kernel and boot code we did not write. A Go port also owns its translated runtime and tracks upstream semantics without receiving direct TypeScript fixes, so the difference is the direction of risk, not the attribution obligation. |
| **Node for a multi-tenant control plane** | One event loop per process and no preemption: a synchronous loop in any in-process plugin stalls every tenant on that replica until a liveness probe kills it. A deadline cannot interrupt synchronous code. A detached rejection is fatal for the whole process by dsh policy. | The role split and at least two replicas per role (already in 00 row 1); liveness on event-loop delay; a `cpu` worker-pool service for anything above a few milliseconds (PII scrub, large payloads); `no-floating-promises` lint. What remains: shared fate inside a replica is wider than with goroutines. |
| **No enforceable in-process boundary** | Any module can import any installed module, read `process.env`, reach `ctx.root` or `ctx.reflect.store`, or change a shared prototype. The inject check is a correctness guard. dsh says so: "These checks are not a sandbox against malicious package code." | In-process means trusted (first-party or operator-reviewed); everything else runs out of process (00 principle 6, unchanged). Lint bans the escape hatches in first-party code. What remains: a Go kernel fences in-process code better at compile time (unexported state, import-boundary lint, a ban on `unsafe`). Neither is a sandbox. |
| **Types are erased** | A provider that violates its definition's interface is not caught by a compiler at load time; row config is checked only by its schema. | The conformance suite per definition becomes mandatory for every provider, the fake included. What remains: contract drift shows up in CI, not at link time. |
| **Go libraries of the current plan** | River, pgx, connect-go, slack-go, client-go and the Alloy `syntax` package are named in 00 rows 4, 6 and 9 and in 03 section 8.2. | Table below. What remains: one embedded Go artifact (the syntax check) and a rewrite of every River-specific sentence in the record. |
| **Contributor pool** | The people who extend this stack today (collectors, backends, operators) write Go. The owner is a platform engineer. | Tool, notifier and ingest authors in the LLM ecosystem write TypeScript and Python; the dashboard app frontend is TypeScript in any design; Go and Python plugins run out of process with a generated SDK. What remains: a Go engineer cannot contribute an in-process row. |
| **Single-binary self-hosting** | Node single executables are still "active development"; runtime-loaded plugins need a `node_modules` tree on disk. | The server ships as a container image and an npm package. `self-host-full` already needs a container runtime for ten upstream components. `aiopsctl` ships as a bundled single executable of the small `ctl` profile, if the spike confirms it. What remains: no static 30 MB binary; an image larger by the Node runtime and the module tree (on the order of 100 MB, to be measured); a slower CLI cold start. |
| **npm supply chain** | A dependency tree of hundreds of packages, install scripts, a registry as a runtime dependency of plugin installation. | Lockfile and exact pins, pnpm build-script approval (dsh has it), provenance verification, hosted images built in CI with no package manager at runtime. What remains: a wider surface than a Go module graph with a checksum database. |
| **The record** | 07 is written around Go interfaces and an `init()` registry; 00 row 1, section 4 and section 6.1, 03 section 8.1 and 8.2, 08 section 1 and section 4 assume them. | Section 5 lists the edits. |

**Library replacements.**

| Current plan (Go) | Literal option | Gap |
|---|---|---|
| River: jobs, periodic, unique, priorities, leader election, `InsertTx` | pg-boss 12: queues, cron schedules, singleton keys, priority, retry and dead letter; the insert can run on the caller's connection | Joining the caller's transaction is the one property 00 principle 8 depends on; it is in the spike. No leader election API; the audit seal and the single-flight gateway render use a `leases` row instead. |
| pgx, `LISTEN/NOTIFY` | `pg` (node-postgres) | None expected. |
| connect-go for `collector.v1.CollectorService` | `@connectrpc/connect-node` 2 | Same protocol family and vendor. The 50,000-agent `GetConfig` path is one indexed read and a hash compare; measured in the spike. |
| slack-go 0.31 behind `slackapi` | `@slack/web-api` and `@slack/socket-mode`, no Bolt | We parse `authorizations` and Agent Session payloads ourselves; first-party Slack SDKs. |
| client-go for `remediation-executor.kubernetes` | `@kubernetes/client-node` 2 | Official client; the five actions are plain API calls. |
| Alloy `syntax` package: the composer parses its own output | The Apache-2.0 `syntax` package compiled to WebAssembly and called in process, or the pinned agent binary run as `fmt` at compose time (off the serve path, cached by composition key) | The one embedded Go artifact. Decided in the spike by size and speed. |
| `grafana-plugin-sdk-go` for the `aiops-app` backend | Unchanged: the backend stays a small Go program, as that SDK is Go only | None. It is a separate artifact in every design. |
| Go MCP server for `/v1/tools` | `@modelcontextprotocol/sdk` | The reference SDK is TypeScript; this side is ahead. |
| Embedded PostgreSQL for laptops | `embedded-postgres` (npm, beta versions) or testcontainers | Maturity of the embedded package; testcontainers is the fallback. |

**dsh packages: reuse verdict.**

| Package | Verdict | Reason |
|---|---|---|
| `cordis` 4.1.0, `cordis-plugin-loader`, `-include`, `-group`, `-timer`, `cosmokit`, `schemastery` | Maintain pinned upstream source under `vendor/`, with the Q1 source modifications, published in our scope with upstream copyright, licence and change notices | The kernel remains upstream-derived. The local source tree removes the runtime dependency on a registry package that moves weekly. |
| `dsh-app-boot` | Maintain source modifications as `@aiops/boot` | Profile and bundle composition, gate, failure audit and dumps are what we need; the manifest keys (`dsh.*`, `engines.dsh`), `DSH_HOME`, the Electron path and the Node-internals resolution table are not. |
| `dsh-plugin-manager` | Adapt the upstream operations in the local source tree (pnpm forwarder, registry plan, write lock, snapshot and restore, exemptions) | Drop the model tool and the Web metadata; preserve upstream attribution and licence obligations. |
| `dsh-package-manifest` | Maintain source modifications | Manifest readers with our keys; upstream attribution and licence obligations remain. |
| `dsh-hmr` | Take the queue and the config watches; drop module replacement | Module replacement depends on Node loader internals and has a page of documented limits. |
| `dsh-scope` | Vendor | A dependency-free library for scoped contribution sets; it is what tenant-scoped registries need. |
| `dsh-storage`, `dsh-jobs`, `dsh-llm`, `dsh-user-approval`, `dsh-session`, `dsh-tools` and the other service definitions | Do not reuse; copy the shape | They are contracts of a single-user agent application: sessions, turns, in-memory jobs, JSON files, one-shot approvals. They are released in lockstep release candidates. |
| `dsh-mcp-client` | Reference only, until `tool-mcp-bridge` has its trigger (00 section 7.3) | Lockstep release candidate. |
| Test-support packages | Copy the patterns | Not published in the installed tree. |

MIT code can be distributed inside an Apache-2.0 project when the notices are kept. The names "DeepSeek", "dsh" and "Cordis" come with no trademark grant, so no package of ours carries them; the kernel README states its origin. 08 section 5.1 gains one line for these notices.

---

## 3. Answers

### Q1. Kernel runtime and language

**Choice.** TypeScript (strict, ESM only) on Node 26 LTS, with Node 24 supported until its end of life for self-hosters. The kernel is Cordis itself.

**What the kernel contains.**

| Part | Origin | Contents |
|---|---|---|
| `@aiops/kernel` | Cordis 4.1.0 and its loader packages, vendored | The context tree; the four built-in services `reflect`, `registry`, `events`, `logger`; fibers and epochs; effects and disposers; the `loader` service with its entry tree, groups, `isolate` and `intercept` row options; include files with patch composition and write-back; disposal-aware timers. |
| `@aiops/boot` | `dsh-app-boot`, with maintained source modifications | Profile discovery; bundle and patch-layer composition; the dump and the schema dump; the compatibility gate and its exemption file; the admission check (section Q7); the required list and the activation audit; fail-loud; signal handling and bounded shutdown; the `prepare` hook that provides `profileContext`, `cmdline`, `lifecycle` and `pluginPackages` before any row mounts. |
| `aiops` | New, small | The only launcher: argument parsing, profile selection, the `plugin` package forwarder. |

**Maintained source modifications, complete list.** (1) A disposal deadline in `_unload`, with a leaked-fiber mark. (2) `!!js` expressions accepted only in bundle patch files. (3) No path through Node internals: no module replacement, standard module resolution. (4) The admission check in boot. This losing option proposes no other differences from upstream, and would run the upstream test suites against the local source tree. The modified components remain upstream-derived and keep the copyright, licence and change notices.

**What the kernel must not contain.** No HTTP, SQL, queue or clock. No notion of tenant, identity, approval, audit, secret, incident or agent. No registry of row types: a row names a module and the loader imports it. No ordering of rows. dsh states the rule as "no privileged core to patch"; here it is a CI check that fails any change adding an export to `@aiops/kernel` or `@aiops/boot` without a decision record.

### Q2. The context model

The model is Cordis, unchanged.

| Concept | Rule |
|---|---|
| Context tree | Each fiber runs in a child of its parent's context (`parent.extend({fiber})`). `ctx.isolate(name, label)` gives a subtree its own scope for one service name; `ctx.intercept(name, config)` gives the plugins below it config for one service. |
| Named services | `ctx.provide(name, value, check?)` registers a service owned by the current fiber; a class that extends `Service` is its service. Singular keys for one engine (`ctx.store`, `ctx.approval`), plural for registries (`ctx.tools`, `ctx.investigators`), as in the dsh cookbook. |
| Inject | `inject` lists the services that must exist before `apply` runs. Reading an undeclared service throws. A row can add `inject` in its entry, which is how role markers and startup services gate rows (Q15). |
| Optional dependencies | Cordis has no optional flag. An optional part is a nested fiber: `ctx.inject(['estate'], cb)` runs `cb` while `estate` exists and unwinds it when it vanishes. This is the 07 A15 rule ("consumers carry on with not known") expressed in the runtime. |
| Typed access | A definition package merges its key into the `Context` interface, so `ctx.changeHub` is typed wherever the package is imported as a type. |

**Declaration: manifest plus code.** The manifest is `package.json`, read without importing the module.

```jsonc
// packages/tool/change-lookup/package.json
{
  "name": "@aiops/tool-change-lookup",
  "version": "0.1.0",
  "license": "Apache-2.0",
  "type": "module",
  "exports": { ".": "./lib/index.js", "./package.json": "./package.json" },
  "engines": { "aiops": "^0.1.0" },                  // the compatibility gate reads this first
  "peerDependencies": { "@aiops/kernel": "~4.1.0", "@aiops/tools": "0.1.0", "@aiops/change-hub": "0.1.0" },
  "aiops": {
    "plugin": {
      "kind": "consumer",                            // definition | provider | consumer | bundle | testkit | util
      "inject": ["tools", "changeHub"],              // mirror of the module export; lint keeps them equal
      "provide": [],
      "hooks": [],                                   // protected hook names need an allow-list entry (Q8)
      "stability": "preview",
      "tenantScope": "global",                       // global | per-tenant
      "capabilities": { "network": [], "secrets": [], "crossTenant": false }
    }
  }
}
```

A bundle adds `"aiops": {"bundle": {"patch": "./aiops.patch.yml"}}`; a profile directory's `package.json` holds `"aiops": {"profile": {"bundles": [...]}}`. `locale/en.json` carries title and description.

**What a plugin author sees (in process).**

```ts
// packages/tool/change-lookup/src/index.ts
import type { Context } from '@aiops/kernel'
import { defineTool } from '@aiops/tools'          // a definition package
import type {} from '@aiops/change-hub'            // type only: brings ctx.changeHub into scope
import z from '@aiops/schema'

export const name = 'tool-change-lookup'
export const inject = ['tools', 'changeHub']
export const Config = z.object({ maxCandidates: z.number().default(7) })

export function apply(ctx: Context, config: { maxCandidates: number }) {
  // a registration is an effect on this fiber: it is gone when the fiber unloads
  ctx.tools.register(defineTool({
    name: 'change_lookup',
    effects: 'read',
    parameters: z.object({ incident: z.string() }),
    execute: ({ incident }, call) => ctx.changeHub.candidates(call.tenant, incident, config.maxCandidates),
  }))

  // optional dependency: lives only while a provider of `estate` is active
  ctx.inject(['estate'], (ctx) => {
    ctx.tools.register(defineTool({ name: 'estate_lookup', effects: 'read', /* ... */ }))
  })
}
```

**The Go sketch the brief asks for.** In this option Go is a plugin language, not the kernel language. A Go author sees the same model through the remote SDK (Q11); the kernel holds a proxy fiber for the process, so `Inject`, activation and disposal mean what they mean in process.

```go
// sdk/go: an out-of-process plugin
func main() {
	aiops.Serve(aiops.Plugin{
		Name:   "tool-ticket-lookup",
		Inject: []string{"tools", "secrets"},          // the proxy fiber stays pending until both exist
		Config: aiops.Schema[Config](),
		Apply: func(ctx aiops.Context, cfg Config) error {
			tools := aiops.Use[toolsv1.Tools](ctx, "tools")
			// Register returns a disposer; ctx.Effect ties it to this activation.
			ctx.Effect(tools.Register(aiops.Tool{
				Name:    "ticket_lookup",
				Effects: aiops.Read,
				Execute: lookup,
			}))
			// Optional dependency: runs while `estate` exists, unwinds when it vanishes.
			ctx.Inject([]string{"estate"}, func(ctx aiops.Context) error { return nil })
			return nil
		},
	})
}
```

### Q3. Activation

Service-availability-driven, exactly as Cordis implements it. There is no boot order: a bundle file's row order carries no load semantics.

| State | Meaning |
|---|---|
| `PENDING` | Waiting for every injected service to have an available provider in scope. |
| `LOADING` | Config resolved and validated; the plugin body is running. |
| `ACTIVE` | Loaded; its provisions are visible to dependents. |
| `FAILED` | Config or body threw. The fiber stays and recovers only through `update()`. |
| `UNLOADING` | Dependents have unloaded; disposers run in reverse registration order and are awaited. |
| `DISPOSED` | Removed. |

**Rules taken from the source.**

- The epoch of a fiber is its revision plus the uid of each injected provision. A missing provision makes it inactive; a different provision (a provider swapped) makes it unload and reload. No plugin writes event wiring for this.
- A provider that leaves `ACTIVE` notifies its dependents first and keeps its implementation until they have finished unloading, so a dependent can still use the service in its own teardown.
- A provider may pass a `check` predicate: present but unavailable keeps dependents pending.

**What a server adds, as plugins and rules.**

| Concern | Decision |
|---|---|
| A provider's availability must not follow a live probe | Rule for definitions: `check` may depend only on composition facts (another service present, migrations applied), never on a connection test. A database blip is an error returned by `ctx.store`, not a provider that vanishes; otherwise every blip would unload and reload the whole tree. Health is a separate service (`ctx.health`). |
| Failure of an optional row | The fiber is `FAILED` and stays so; siblings run. The `supervisor` plugin retries it with `update()` on a backoff of 5 s doubling to 5 min, ten times, then leaves it failed and visible in health and the dump. |
| Failure of a required row | At boot: dispose the tree, exit non-zero (dsh policy). After boot: readiness goes false at once; the process exits after a grace period so the orchestrator replaces it. |
| Restart storms | Cordis has no damping, and a failed fiber never retries by itself, so a crash loop cannot start from a failure. A storm can start only from a provider that flaps. `supervisor` listens to `internal/status`: a fiber that changes state more than five times in 60 s is held (its entry is disabled with a recorded reason) for 30 s doubling to 10 min, and an event is written. The numbers are initial values. |
| A disposer that never returns | Source modification 1: unload waits at most the drain deadline (30 s default), then marks the fiber leaked, logs every effect label still open (`getEffects()`), and sets readiness false so the replica is replaced. |
| Detached rejection or uncaught exception | Fatal for the process, as in dsh: "only the throw site knows which state is intact". |

### Q4. Effects and disposal

**The rule.** A plugin may change the world outside its own closure only through `ctx.effect` or through a service method that calls `ctx.effect` on the caller's context. Cordis services see the caller's context (`this.ctx` is traced to the calling fiber), so `ctx.tools.register(...)` made by plugin A is owned by A's fiber even though the registry belongs to another plugin.

| Registration | Made through | What unloading does |
|---|---|---|
| HTTP or RPC route | `ctx.http.route`, `ctx.api.mount` | Removes the route, then waits for its in-flight requests up to the drain deadline. |
| Job handler | `ctx.jobs.work(kind, handler)` | Stops polling for that kind. Queued jobs stay queued for the next handler; they do not fail. |
| Periodic schedule | `ctx.jobs.schedule(key, spec)` | The schedule row is durable state; the disposer does not delete it. |
| Outbox subscription | `ctx.outbox.subscribe(type, handler)` | Stops delivery to this subscriber; the cursor stays. |
| Tool, notifier, executor, parser, verifier, config source | `ctx.<registry>.register` | Removes the entry; dependents of the registry see it gone at the next lookup. |
| Hook listener | `ctx.on(name, listener)` | Removed (Cordis). |
| Service | `ctx.provide` | Dependents unload first, then the provision is deleted (Cordis). |
| Timer | `ctx.timeout`, `ctx.interval` | Cleared (Cordis timer). |
| Metric callback, health probe | `ctx.telemetry.observe`, `ctx.health.probe` | Callback removed. Instrument definitions are idempotent by name and stay. |
| Sidecar claim | `ctx.sidecars.ensure(spec)` | Releases the claim; the instance scales to zero by idleness and is deleted by the tenant delete saga. |
| Migration | Declared in the manifest, applied by the `migrate` profile before a rollout | Nothing. Migrations are forward-only, expand then contract (00 row 9). |

**Process effects and durable effects.** Cordis reverts effects in the context. A server also writes rows, schedules and messages that must outlive a reload. The rule: unloading reverts process effects only; durable state is removed by an explicit path (uninstall, tenant purge) and never by a disposer. A definition's documentation marks each method as one or the other.

**The test that proves a plugin leaves nothing behind.** Each definition ships `leaves-nothing` in its `testing` export, and `aiopsctl plugin check` runs it for every plugin:

1. Mount the definition fakes on a bare context and take a snapshot: every fake registry's contents, the event listener table, the root effect tree (`root.fiber.getEffects()`), and the process's active resources (`process.getActiveResourcesInfo()`).
2. Mount the plugin, wait for `ACTIVE`, exercise it once.
3. Dispose the plugin's fiber and await it.
4. Take the snapshot again; it must equal the first. Then mount and dispose a second time, to catch state kept in module scope.

dsh's version of this rule is "every registry gets an HMR-safety test (dispose the contributing fiber, assert cleanup)"; this is the same test made generic.

### Q5. Service definitions

**The seam rule.** dsh: "A seam has three roles: Service Definition, Service Provider, Consumer, and all three must exist." It replaces principle 3. A definition needs one provider and one consumer, not two providers. What does not get a definition: pure functions and value types. `@aiops/entity` (the entity reference), the fingerprint and priority functions and the correlation ladder stay libraries, as `dsh-scope` and the `dsh-util-*` packages are in dsh.

**Catalogue.**

| Group | Definitions (context key) |
|---|---|
| Infrastructure | `telemetry`, `health`, `cpu`, `http`, `store` (hub with `sql`, `kv`, `blob` backends), `secretsBackend`, `secrets`, `leases`, `jobs`, `outbox`, `sidecars` |
| Platform | `tenants`, `identities`, `authz`, `auth` (enrolment and tokens), `audit`, `approval`, `changeHub`, `credentialLease`, `pii`, `llm` (with its `providers` registry), `tools`, `notifications`, `chat`, `oncall`, `limits`, `gatewayRender`, `fleet` (with its `sources` registry), `metricsQuery`, `logsQuery`, `toolsetBindings`, `dashboards`, `pluginManager` |
| Domain | `signals` (with its parser registry), `correlator`, `casefile`, `investigators`, `remediation`, `compliance`, `estate`, `pillars` |

**Where a definition lives.** One package per definition under `packages/<group>/<name>`, holding: the TypeScript interface and the `Context` merge; the hub or registry plugin when the definition is registry-shaped (as `dsh-storage` holds its hub); the hook names it dispatches, typed; stable error codes; a `contract` number; and a `./testing` export with an in-memory fake, the conformance suite and the leak test. Providers are sibling packages (`store` and `store-postgres`, `jobs` and `jobs-pgboss`).

**Plugins depend on definitions, never on a provider.** Enforced four ways:

1. Manifest lint (the dsh `constraints` check): a package of kind `consumer` or `provider` may name in `dependencies` and `peerDependencies` only packages of kind `definition`, `util` and the kernel. Only a `bundle` may depend on providers; only `devDependencies` may name a `testkit`.
2. Import lint generated from the `kind` fields: importing a provider from a consumer fails CI.
3. TypeScript project references: a consumer's project does not reference provider projects, so the import does not compile.
4. At run time the context refuses a read of a service that was not injected.

The current rule that `aiops-base` and `aiops-pillars-mvp` never import stack packages (00 section 6.3) becomes an instance of 1 and 2.

**Versioning.** First-party packages release in lockstep with the platform version and pin each other exactly, as dsh does. A third-party plugin declares `engines.aiops`; that range alone decides. Without it, every peer range on `@aiops/*` must contain the running version. A definition's `contract` number changes only when its interface or its conformance suite changes incompatibly; the remote handshake compares it, and a proto package carries it as its major (`aiops.def.tools.v1`). Before 1.0 every minor may break, and the gate treats it so.

### Q6. What becomes a plugin

Every row of 00 section 4 becomes a loader row that names a package. "Injects" lists required services; a name in brackets is optional (a nested fiber). Role markers are in Q15.

| Row id | Was (00 section 4) | Provides | Injects | Bundle |
|---|---|---|---|---|
| (kernel) | `loader`, `plugin-host` | `loader` and the built-ins | | kernel, not a row |
| `server-startup` | `--role` flag | `roleEdge`, `roleApi`, `roleWorker`, `roleAny` | `cmdline` | `aiops-base` |
| `telemetry`, `health`, `cpu` | host services `logger`, `metrics`, `tracer`, `lifecycle` | same keys | | `aiops-base` |
| `supervisor` | plugin-host guarantees | | `loader`, `health` | `aiops-base` |
| `store` | kernel row and package `store` | `store` (hub) | | `aiops-base` |
| `store-postgres`, `store-blob-s3` | same | backends `sql`, `kv`; `blob` | `store`, `telemetry` | `aiops-base` |
| `secrets-backend-local-file` | seam `secrets-backend` | `secretsBackend` | | `aiops-base` |
| `secrets` | host service `secrets` | `secrets` | `secretsBackend`, `store` | `aiops-base` |
| `leases` | lease rows in `events.tenant_lease` | `leases` | `store` | `aiops-base` |
| `jobs` | `scheduler` | `jobs` | `store`, `leases` | `aiops-base` |
| `outbox` | `events` | `outbox` | `store`, `jobs` | `aiops-base` |
| `http` | `http.router` | `http` (public and internal listeners) | `telemetry` | `aiops-base` |
| `identities`, `identity-oidc`, `identity-service-token` | seam `identity` | `identities`; entries | `store` | `aiops-base` |
| `authz` | bindings in `auth` and `approval` | `authz` | `store` | `aiops-base` |
| `tenants` | `tenant` | `tenants` (registry, sagas, realms) | `store`, `outbox`, `jobs`, `loader`, `secrets` | `aiops-base` |
| `api` | `api-server` | `api` | `http`, `identities`, `authz`, `tenants` | `aiops-base` |
| `api-incidents`, `api-changes`, `api-approvals`, `api-findings`, `api-audit`, `api-stream`, `api-admin`, `api-plugin-manager` | route families of 03 section 8.8, `admin` | | `api` and the service each serves | `aiops-base` |
| `audit` | `audit` | `audit` | `store`, `jobs`, `leases` | `aiops-base` |
| `audit-sink-object-store`, `audit-sink-webhook` (P2) | seam `audit-sink` | sink entries | `audit` | `aiops-base` |
| `approval` | `approval` | `approval` | `store`, `audit`, `identities`, `authz`, `outbox` | `aiops-base` |
| `approval-tiers` | tier table package | listener on `approval/evaluate` | `approval` | `aiops-base` |
| `change-hub` | `change-hub` | `changeHub` (ledger, token, verifier registry) | `store`, `audit`, `approval`, `outbox`, `jobs`, `secrets` | `aiops-base` |
| `verifier-alert-cleared`, `verifier-read-back` | verification checks package | verifier entries | `changeHub` | `aiops-base` |
| `credential-lease` | host service `credential-lease` | `credentialLease` | `changeHub`, `secrets`, `audit` | `aiops-base` |
| `pii` | `pii` | `pii` | `secrets`, `cpu` | `aiops-base` |
| `llm` | `llm` gateway | `llm` (gateway, `providers` registry) | `pii`, `leases`, `store`, `http`, `audit`, `tenants` | `aiops-base` |
| `llm-provider-openai-compatible`, `llm-provider-replay` | seam `llm-provider` | provider entries | `llm` | `aiops-base`; test bundles |
| `signals` | `signal-ingest` core | `signals` (admission, dedupe, raw group, ack; parser registry) | `http`, `store`, `outbox`, `jobs`, `tenants` | `aiops-base` |
| `signal-ingest-alert-webhook`, `signal-ingest-generic-json` | point rows | parser entries | `signals` | `aiops-base` |
| `correlator` | package `correlate` | `correlator` | `signals`, `store`, (`estate`) | `aiops-base` |
| `storm-mode` | kernel code in the ingest path | listener on `incident/open` | `casefile`, `store` | `aiops-base` |
| `casefile` | `casefile` | `casefile` | `store`, `outbox`, `correlator`, `audit`, `jobs`, (`estate`) | `aiops-base` |
| `pager` | page job in `notify` | | `casefile`, `jobs`, (`oncall`) | `aiops-base` |
| `notifications`, `notifier-email`, `notifier-webhook` | point `notifier` | `notifications`; entries | `pii`, `jobs`, `store` | `aiops-base` |
| `chat` | seam `chat-surface`, `router` | `chat` (surface registry, routing) | `identities`, `tenants`, `approval` | `aiops-base` |
| `tools` | point `tool`, dispatch | `tools` | `pii`, `audit`, `tenants` | `aiops-base` |
| `investigators` | point `investigator` | `investigators` | `tools`, `llm`, `casefile`, `leases`, `jobs` | `aiops-base` |
| `remediation` | point `remediation-executor`, point guard | `remediation` | `approval`, `changeHub`, `credentialLease`, `audit`, `jobs` | `aiops-base` |
| `sidecars` | sidecar supervisor | `sidecars` (hub) | `tenants` | `aiops-base` |
| `estate` (P2) | `estate` | `estate` (with a `sources` registry) | `store`, `metricsQuery` | `aiops-base` |
| `plugin-manager` | `plugin-manager` | `pluginManager` | `loader`, `profileContext`, `audit` | `aiops-base` |
| `toolset-bindings` | `toolset-bindings` | `toolsetBindings` | `secrets`, `tenants` | `aiops-datasources` |
| `metrics-query`, `logs-query` (P2) | `datasource/metrics`, `datasource/logs` | `metricsQuery`, `logsQuery` | `toolsetBindings` | `aiops-datasources` |
| `auth` | `auth` | `auth` (enrolment, tokens, key set) | `store`, `secrets`, `http`, `tenants` | `aiops-stack` |
| `gateway-render` | `gateway` renderer | `gatewayRender` | `auth`, `jobs`, `leases`, `tenants` | `aiops-stack` |
| `fleet` | `fleet` core | `fleet` (revisions, composer, `sources` registry) | `store`, `jobs`, `tenants` | `aiops-stack` |
| `fleet-agentapi` | `fleet-agentapi` | | `fleet`, `http`, `auth` | `aiops-stack` |
| `fleet-source-host-metrics`, `-host-logs`, `-otlp-receiver`, `-ebpf-traces`, `-ebpf-profiles`, `-self-monitoring` | fleet config templates | source entries | `fleet` | `agent-base`, composed by `aiops-stack` |
| `backend-provisioner` | backend provisioning in `tenant` | listeners on `tenant/create`, `tenant/delete` | `tenants`, `secrets` | `aiops-stack` |
| `limits`, `limits-renderer-metrics`, `-logs`, `-traces`, `-profiles` (P2) | seam `limits-renderer` | `limits`; entries | `tenants`, `jobs` | `aiops-stack` |
| `annotator`; `dashboard-sync` (P2) | `annotator`, `dashboard-sync` | `dashboards` | `outbox`, `secrets`, `tenants` | `aiops-stack` |
| `chat-slack`, `notifier-slack`, `approval-answerer-slack`, `chat-commands` | `chat-surface.slack`, `notifier.slack`, Slack approval handler, `/aiops` | entries | `chat`, `notifications`, `approval`, `secrets` | `aiops-chatops-slack` |
| `oncall-goalert`, `sidecar-spec-goalert`, `tool-oncall-who` | `on-call-provider.goalert`, `tool.oncall-who` | `oncall`; spec; tool | `sidecars`, `secrets`; `tools`, `oncall` | `aiops-oncall-goalert` |
| `tools-mcp-facade` | `/v1/tools` facade | | `tools`, `http`, `llm` | `aiops-pillars-mvp` |
| `investigator-holmes`, `sidecar-spec-holmes` | `investigator.holmes` | investigator entry; spec | `investigators`, `sidecars`, `toolsetBindings`, `llm` | `aiops-pillars-mvp` |
| `tool-change-lookup`, `tool-runbook-fetch`; `tool-estate-lookup` (P2) | `tool.*` | tools | `tools`, `changeHub`; `tools`; `tools`, `estate` | `aiops-pillars-mvp` |
| `remediation-kubernetes`; `remediation-gitops-pr` (P2) | `remediation-executor.*` | executor entries | `remediation`, `credentialLease` | `aiops-pillars-mvp` |
| `signal-ingest-gitops-webhook`, `signal-ingest-policyreport` | point rows | parser entries | `signals` | `aiops-pillars-mvp` |
| `compliance` | `compliance` | `compliance` | `store`, `signals`, `casefile`, `jobs` | `aiops-pillars-mvp` |
| `pillars`, `pillar-incident`, `pillar-change`, `pillar-compliance` | `pillar.<name>` data rows | `pillars`; one preset each (a nested row list) | `tools`, `signals` | `aiops-pillars-mvp` |
| `sidecars-kubernetes`, `secrets-backend-kms`, `billing-meters`, `cohorts`, `llm-tenant-limits`, `fleet-serve-cache`, `fleet-canary` | `aiops-hosted` contents | provider entries and listeners | as each needs | `aiops-hosted` |
| `sidecars-static` | shared instancing | provider entry | `sidecars` | `aiops-selfhost` |

Roughly 95 rows compose `hosted`. This follows dsh's composed row-tree model; the comparison does not rely on a machine-specific dsh row count.

Rows whose manifest says `tenantScope: per-tenant` (`investigator-holmes`, `oncall-goalert`, `toolset-bindings`, the chat bindings, tools a tenant enables) are mounted at the root in single-tenant profiles and inside each tenant realm in hosted (Q10). Their consumers reach them through the tenant bridge in both cases, so the two profiles differ only in composition (principle 1).

A pillar becomes what a dsh agent preset is: a row whose config is a nested row list (persona, toolset, channel default), mounted under a group with `isolate: {tools: true}` so each pillar sees its own tool set. The loader refusing a pillar with no tool or ingest dependency (00 row 12) is now the runtime: the preset injects `tools` and `signals` and stays pending without them.

### Q7. Policy that must not be bypassed, as plugins

dsh already ships this pattern in `dsh-user-approval`, and the mechanisms below are taken from it.

| # | Mechanism | How it keeps the invariant |
|---|---|---|
| 1 | **Dependency, not convention** | An executor is an entry in the `remediation` hub, never a service. The hub is the only caller of `Apply`, and it injects `approval`, `changeHub`, `credentialLease` and `audit`. If one of them is absent, disabled or failed, the hub is pending and no write path exists. The same holds for `llm` (injects `pii`), `notifications` (injects `pii`), `tools` (injects `audit`, `pii`). Absence fails closed by activation. |
| 2 | **The floor is inside the service, around the hook chain** | `approval` computes the floor of 03 section 8.3 in its own code, runs the `approval/evaluate` waterfall, then clamps the result to at least the floor. Listeners can raise and can veto; a listener that skips `next()` and returns "allow" changes nothing. This is the dsh rule that `never` is enforced "before waterfall dispatch, so a listener registered later with `prepend` cannot bypass". |
| 3 | **One provider per name** | A second `provide('approval')` in a scope throws. A row cannot shadow a policy service silently. |
| 4 | **No isolation of protected services** | `isolate` would let a subtree bring its own permissive `approval`. Boot's admission check rejects any row, in any layer, whose `isolate` names a service on the protected list (`approval`, `changeHub`, `credentialLease`, `audit`, `pii`, `identities`, `authz`, `tenants`, `secrets`, `store`). Tenant realms are generated by `tenants` and never isolate those names. |
| 5 | **Capability, not ambient authority** | A write credential comes only from `credentialLease.acquire(changeToken)`; the token is signed by `changeHub` at dispatch and names one action. An executor called without a token gets no credential. |
| 6 | **The ledger before the write, in one transaction** | Only a transaction object exposes `emit` and `audit`. `remediation` writes the `ChangeRecord` transition and its audit row, commits, and only then calls the executor. A failed audit append rejects the call "instead of returning an unlogged decision". |
| 7 | **Protected hooks** | `approval/*`, `change/*`, `llm/request`, `tools/pre-execute` and `http/request` accept listeners only from packages on the bundle's allow-list (Q8). |
| 8 | **Conformance and the failure matrix** | The `approval`, `remediation`, `credentialLease`, `audit` and `pii` suites contain the bypass attempts: a prepended allow-listener, a missing provider, an isolated twin, a token for another digest. The failure matrix boots the built artifact with each policy row disabled or failing and asserts that no write and no model call is possible. |

**What stays in boot, and why dsh would agree.** dsh keeps three things out of the plugin tree: the required id list ("a global id list owned by app-boot"), the compatibility admission, and the telemetry switch that "config cannot" override. We keep the same three: the required list with the protected list beside it; the admission check (the gate, mechanism 4, and the rule that layers 2 to 5 cannot disable, rename or re-insert a protected row); and the kill switches `AIOPS_AGENTS_PAUSED` and `AIOPS_TELEMETRY_DISABLED`. They are composition rules, not features, and they must hold before any plugin code runs.

**The honest limit.** These mechanisms hold against a missing plugin, a failed plugin, a mis-composed profile and a well-meaning but wrong plugin. They do not hold against hostile code loaded in process: such code can read the signing key from memory. So the trust rule of principle 6 stays a rule: in process means trusted, and third-party code reaches policy only through a remote adapter, where the kernel side applies mechanisms 1 to 7. dsh draws the same line: installed code "executes in-process outside the workspace sandbox".

### Q8. Interception

Hooks are Cordis events, typed by merging into the `Events` interface of the definition that dispatches them. A listener is an effect of the fiber that registered it.

| Hook | Mode | Owner | Built-in innermost step | Who may register |
|---|---|---|---|---|
| `http/request` | waterfall | `api` | Route handler | Protected: identity, rate limit, audit |
| `signals/admit` | waterfall | `signals` | Admit against the tenant bucket | Platform bundles; not calling `next()` sheds with 429 |
| `signals/parsed` | waterfall | `signals` | Normalise labels, lift `signal_type` | Any plugin |
| `signals/correlate` | waterfall | `correlator` | The key ladder | Any plugin (fold and rerank wrap it) |
| `incident/open` | waterfall | `casefile` | Open and decide `investigate_policy` | Any plugin; `storm-mode` registers here |
| `incident/page` | waterfall | `pager` | Page when the threshold is met | Platform bundles. A veto withholds a page, so the list is closed. |
| `investigation/context` | registry with an explicit `order` | `casefile` | Context pack v1 sections | Any plugin |
| `investigation/answer` | waterfall | `casefile` | RCA contract check | Any plugin; can only add flags |
| `llm/request` | waterfall | `llm` | Budget, scrub, upstream call | Protected |
| `tools/pre-execute` | waterfall | `tools` | Effects check, tenant check, call | Protected |
| `tools/result` | waterfall | `tools` | Output cap and scrub | Any plugin; the cap and scrub are applied after the chain |
| `approval/evaluate` | waterfall, clamped | `approval` | The floor | Protected; raise-only by type |
| `approval/request` | serial | `approval` | none: with no answerer the request waits for the API | Surfaces (Slack, API, app) |
| `change/pre-dispatch` | waterfall | `changeHub` | Re-check quorum and digest, sign the token | Protected; change windows and freezes register here |
| `change/verify` | registry | `changeHub` | The two zero-cost checks | Any plugin |
| `notify/pre-send` | waterfall | `notifications` | Scrub, digest window, pacing | Any plugin; the scrub is applied after the chain |
| `tenant/create`, `tenant/delete` | serial saga steps, each with a compensation | `tenants` | Kernel-store steps | Platform bundles |

**Rules.**

- **Order is never a policy.** Activation order is not deterministic, so listener order is not either. dsh: "sibling listener order is not a policy-priority mechanism". Every protected hook combines listeners monotonically (raise-only, veto-any) and enforces its floor outside the chain. Where order matters for content, the definition uses a registry with an explicit `order` key.
- **Registration is guarded in the runtime.** The owning service answers the `internal/listener` bail event for its protected names: it finds the loader entry of the registering fiber (`loader.locate`) and refuses a package that is not on the allow-list. The manifest's `hooks` field lets the admission check refuse the row before import.
- **Typed.** A listener's arguments and return type come from the definition's `Events` merge; calling `next()` twice throws.
- **Durable events are a different thing.** Hooks are in-process and synchronous with the operation. State changes are outbox rows written in the transaction and delivered at least once through `ctx.outbox`.

### Q9. Composition and reload

**Composition.** Unchanged from dsh and from 00 section 6.1: an empty root; bundle patches in profile order; the profile layer; the site layer; `--patch` overlays; the tenant layer; the environment switches. A patch addresses a row by `id` and replaces its whole `config`; `insert` adds rows; an unknown id warns; an empty file fails boot; only bundles the profile names contribute.

**Live recomposition: yes.** The unit of reload is one row. Cordis then moves exactly the fibers whose epoch changed.

| Change | What happens |
|---|---|
| A row's volatile config fields | Committed into the running object; the fiber gets `loader/volatile-update`; no restart. |
| A row's other config | Candidate validated first; that fiber restarts; its dependents restart only if it provides a service. |
| A row disabled or removed | Its fiber unloads; dependents go pending; optional nested fibers of others unwind. |
| A provider replaced by another row | Dependents unload, then reload against the new provision. |
| Bundle list or package versions | Self-host: recomposition through the plugin manager. Hosted: a new image and a rolling restart. |

**One serialized reload.** dsh serializes reloads and plugin-manager writes in one queue and rejects nested transactions. Tree mutations in Node are single-threaded, so the queue is a promise chain and no lock exists. Names in the sketch are illustrative.

```ts
// @aiops/boot
let tail: Promise<unknown> = Promise.resolve()
export function runExclusive<T>(op: () => Promise<T>): Promise<T> {
  const run = tail.then(op, op)
  tail = run.catch(() => {})
  return run
}

export const recompose = (ctx: Context, reason: string) => runExclusive(async () => {
  const next = composeLayers(await readLayers(ctx.profileContext))  // pure
  const admitted = admit(next, ctx.pluginPackages)                   // gate, protected rows, isolate rule
  await preflight(ctx, admitted)                                     // fiber.resolveConfig(candidate): nothing published
  const previous = currentGeneration(ctx)
  try {
    await applyGeneration(ctx.loader, admitted)                      // loader.create / update / remove, by id
    await ctx.loader.await()
    auditActivation(ctx, admitted)                                   // required rows active, policy rows active
  } catch (error) {
    await applyGeneration(ctx.loader, previous)                      // ours: dsh does not roll back
    throw error
  }
  ctx.emit('boot/recomposed', digest(admitted), reason)
})
```

A Go kernel would need the same single owner: one goroutine that owns the tree, with every mutation sent to it. Cordis relies on run-to-completion between awaits, and its fiber code is dense with re-entrancy cases; this is the part of a port that is hardest to get right.

**What hosted does.**

| Layer | Hosted | Self-host |
|---|---|---|
| Bundles, profile, code (layers 0 to 2) | In the image and the chart values; changed by a rolling restart with kernel N and N-1 coexisting | Files; `aiopsctl profile reload` or the config watch recomposes live |
| Site layer (3) | A table (`admin.site_overlay`) written through the admin API by the plugin manager: toggles of optional rows and their config. Each replica recomposes on the doorbell, at most one replica per role at a time (a `leases` row), and emits `profile.recomposed` with its digest so drift is visible | `$AIOPS_HOME/aiops.patch.yml` |
| Tenant layer (5) | Live, per tenant realm (Q10) | Live; usually absent |

Only bundle patch files may carry `!!js`. Every operator and tenant layer is data-only, because a layer that can be written through an API would otherwise be remote code execution.

### Q10. Tenancy in the context tree

dsh has no tenants. It has two scoping tools, and both fit: agent presets (a nested loader tree per agent, with `isolate` groups) and `dsh-scope` (per-scope contribution sets with inherited lookup).

**Two tiers.**

| Tier | What runs there | How the tenant is known |
|---|---|---|
| Shared (root tree) | Services that are multi-tenant by data: `store`, `jobs`, `outbox`, `http`, `audit`, `approval`, `changeHub`, `llm`, `signals`, `casefile`, the hubs | An explicit tenant argument (`ctx.store.forTenant(t)`), with row-level security underneath. Cross-tenant access (`ctx.store.platform()`) needs the `crossTenant` capability and the worker role. |
| Tenant realm (one child group per active tenant) | Rows with `tenantScope: per-tenant`: the investigator entry, the on-call client, chat bindings, toolset bindings, tenant policy rows, tools a tenant enabled | The realm's `intercept` config. `ctx.store`, `ctx.secrets`, `ctx.jobs` and `ctx.audit` resolve the tenant from it, so a plugin in a realm has no argument through which to name another tenant. |

**A realm is a generated row.** `tenants` creates it with `loader.create` after running the same admission check as boot (dsh does this for preset rows through `prepareProfileEntries`).

```yaml
- id: tenant-acme
  name: cordis:group
  group: true
  inject: [roleWorker]                       # realms with sidecar clients live in the worker role
  isolate: { oncall: true, toolsetBindings: true }
  intercept:
    store:   { tenant: acme }
    secrets: { tenant: acme }
    jobs:    { tenant: acme }
    audit:   { tenant: acme }
  config:                                    # the tenant layer: catalogue rows only, data only
    - id: investigator-holmes
      name: '@aiops/investigator-holmes'
      config: { stepCeiling: 30 }
    - id: approval-tenant-policy
      name: '@aiops/approval-tenant-policy'
      config: { raise: { delete-pod: L2 } }
```

**Rules.**

- **Per-tenant enablement at run time** is `loader.update(id, {disabled})` inside the realm: a real fiber start or a full unwind for that tenant only. There is no `ConfigReader.For` and no routing flag; tenant fields are the config of the tenant's own fibers, and volatile fields change in place.
- **What a tenant may write.** Rows from the catalogue of `tenantScope: per-tenant` packages; `disabled`; the config fields the schema marks tenant-writable. No `name`, `inject`, `isolate` or `intercept`, no expressions, no insert outside the catalogue. Validated before it is stored (08 section 1.7, unchanged).
- **A realm row is visible outside the realm in two ways.** A registry entry made through the realm's context is tagged with the realm's scope and returned only for that tenant (`ctx.investigators.for(tenant)`, `ctx.tools.for(tenant)`): the `dsh-scope` pattern. A service provided in the realm under an isolated name is reached through one bridge: `ctx.tenants.use(tenant, 'oncall')` returns the service resolved in that realm, or throws `unavailable`. In a single-tenant profile the realm isolates nothing and inherits the root provider, so the same call works.
- **Per-tenant behaviour of a shared policy** is a listener registered from the realm, filtered to that tenant (Cordis context filters; the `dsh-scope` pattern). The raised approval tier is the row `approval-tenant-policy`. A realm has its own required list: if a tenant-required row is not active, write-capable hubs refuse that tenant. This is the fail-closed rule of 08 section 1.7 for a stored raise that stops validating.
- **Per-tenant upstream instances.** `investigator-holmes` in a realm calls `ctx.sidecars.ensure({spec: 'holmes'})` as an effect. In hosted the provider `sidecars-kubernetes` creates and scales one instance per tenant; in self-host `sidecars-static` returns the one shared address. Instancing is which provider the mode bundle composes, not a field every row interprets.

**Cost at a few hundred tenants.**

| Item | Estimate | Basis |
|---|---|---|
| Fibers | 300 tenants times about 12 rows is 3,600 fibers per replica if every realm is mounted | Row count above |
| Memory | Tens of megabytes: a fiber is a few objects, closures and two prototype-chain maps | Estimate, not measured |
| Mounting | Each provision notifies by walking every fiber, so mounting all realms costs on the order of ten million cheap checks | `reflect.notify`; estimate |
| A root provider swap | Every dependent in every realm restarts | Epoch rule |

Realms are therefore lazy: mounted on first use, unmounted after 30 minutes idle, and a root provider swap is staged one replica at a time. The spike in section 4 measures 500 realms before anything is built on this.

**What isolation this is.** Name scoping and effect ownership, not security. All tenants of a replica share one heap and one event loop. Tenant data isolation stays where the record puts it: row-level security, per-tenant keys, per-tenant sidecars, the gateway.

### Q11. Plugin runtimes and installation

| Runtime | For | Boundary | Order |
|---|---|---|---|
| In-process module | First-party rows and plugins an operator trusts | None: trusted code | From the first step |
| Worker thread | CPU-heavy first-party steps; a TypeScript plugin that should not share the main event loop | Own heap and loop, resource limits; not a security boundary | Step 3, with the remote protocol |
| Out of process, any language, over gRPC | Third-party plugins, model-chosen actions, Go and Python authors | Process, network policy, per-tenant instance in hosted | Step 3 |
| WebAssembly | Small sandboxed contributions | Sandbox | No date. Trigger: the first sandboxed contribution need (00 section 7.3, unchanged). V8 runs it natively, so the cost is the host API, not an engine. |

**A remote plugin is a proxy fiber.** The kernel mounts a proxy for each remote plugin with the plugin's declared `inject`. When the proxy activates it sends `Activate{epoch, config}`; the plugin's registrations come back as `Register{effect_id, definition, spec}` and become effects of the proxy. When an injected service changes, the proxy unloads: the kernel removes every effect itself and sends `Deactivate{epoch}`. A dead process is an unload, so nothing it registered survives it. The same protocol runs over a worker's message port. One tenant per connection, row and tenant taken from the connection, handshake on protocol, `contract` and manifest digest (07 section 5, unchanged).

**Which definitions get a remote adapter.** Registry-shaped, plugin-facing ones: `tools` first, then `signals` parsers, `notifications`, `remediation`, `investigators`, `oncall`, `changeHub` verifiers, `estate` sources. Infrastructure and policy definitions never do; a remote plugin receives a small host subset instead (logs, metrics and traces, its own secrets read-only, key-value state, audit append). Go and Python SDKs are generated from the protos.

**Installing a plugin into a profile through supported interfaces, without core source changes.**

| Deployment | How |
|---|---|
| Self-host on a VM | `aiops plugin --profile <name> add <spec>`: the gate judges the package before anything downloads, pnpm installs into the profile directory, the bundle is appended to the profile's list, and the running kernel recomposes. Build scripts are blocked until approved. A failure restores the manifest and lockfile. |
| Self-host on compose or Kubernetes | The profile directory is a volume, filled by an init step that runs `aiops plugin install --frozen-lockfile` from the chart's `plugins:` list; or a derived image with the same command in its build. |
| Air-gapped | `aiops plugin pack` writes the resolved packages as one OCI artifact; `aiops plugin add oci://...` installs from it. |
| Hosted | Build time only. The image is built in CI from a lockfile; the plugin manager's package operations are switched off; a tenant enables catalogue rows or attaches a remote plugin. |

No distribution builder is needed for TypeScript plugins. A remote plugin ships as an image.

**Index and signing.** A plugin index is a signed catalogue file (name, versions, `engines.aiops`, stability, capabilities, signer), in the manner of the dsh plugin market index. Registry packages are checked against their provenance attestation; OCI artifacts and remote plugin images are checked with cosign (ADR-020). A private registry never falls through to a public one (dsh rule).

### Q12. The plugin manager and the compatibility gate

**The manager is a row** (`plugin-manager`, providing `ctx.pluginManager`), as in dsh. It cannot remove or disable itself or a protected row.

| Operation | Effect | Surface |
|---|---|---|
| List rows and bundles | Fiber state, health probes, version, stability, the layer that last patched each row, gate denials | `aiopsctl plugin list`, admin API, later a dashboard app page |
| Toggle a row | Writes `disabled` in the layer the caller may write (profile, site or tenant); recorded only as a departure from the bundles | CLI, API |
| Enable or disable a bundle | Edits the profile's bundle list; recomposes | CLI, API; self-host |
| Install, update, remove a bundle | Package operation under the profile write lock, outside the reload queue, then recomposition | CLI, API; self-host only |
| Version exemption | Writes `compatibility.json` | CLI with `--accept-risk`; self-host only |

When the kernel is down the CLI performs the same operations on the profile directory under the same lock, as `dsh plugin` does. No model tool exposes the manager.

**The gate.** One rule, from `engines.dsh`:

1. If the manifest declares `engines.aiops`, that range alone decides.
2. Otherwise every peer range on `@aiops/*` must contain the running version.
3. Tighter than dsh: a package outside the first-party scope with neither is denied. An empty or invalid range denies.
4. `stability` must be at or above the profile's `minStability`. A remote plugin must announce a supported protocol and the definition `contract` numbers, checked again at every handshake.

It runs where we own composition: at install, at boot, at every recomposition, when a tenant enables a row, and at a remote handshake. A denied row becomes a detached disabled row in the launcher's copy; no file is rewritten; the dump and health list it. Exemptions map an exact `package@version` to exact runtime versions and are never inherited by an upgrade. The hosted image must pass with zero exemptions. The gate is not a sandbox.

### Q13. Failure policy

The dsh matrix, with the column for a later change now meaning live recomposition.

| Failure | Optional row at boot | Required row at boot | Live recomposition |
|---|---|---|---|
| Profile, a named bundle or a `--patch` file missing or malformed; an empty layer file | Stop | Stop | Patch rejected; running config unchanged |
| Admission denies (gate, protected row touched, isolate of a protected service) | Row detached as disabled, reported | Stop | Change rejected |
| Module import fails or throws | Warn; continue | Stop | Report; siblings continue; a corrected import activates |
| Config fails its schema | Warn; continue | Stop | New row inactive; an existing row keeps its instance and config |
| A bundle expression throws | Warn; continue | Stop | Report; never read as disabled |
| `apply` throws, now or later in its promise | Warn; continue; `supervisor` retries with backoff | Stop | Report; generation rolled back if the row is required |
| An injected service is unavailable | Warn; the row waits | Stop | The row waits; a provider activates it |
| Waiting only on a role marker | Ignored: the row is not in this role | Ignored | Ignored |
| A listener bind fails | Warn; continue without the endpoint | Stop | Continue without it |
| A required row fails after boot | n/a | Readiness false; exit after the grace period | Generation rolled back |
| A policy row inactive | Its dependents are pending: no write, no model call | Stop | Change rejected |
| A disposer exceeds the drain deadline | Fiber marked leaked; readiness false | Same | Same |
| Remote plugin unreachable or handshake mismatch | Proxy fiber failed; calls fail fast; retried with backoff | Stop | Same as boot |
| Detached rejection or uncaught exception | Fatal: dispose, exit non-zero | Fatal | Fatal |
| Row absent or disabled | Ignored | Stop if it is on the required list of the profile (tighter than dsh, which ignores it) | Rejected for a required row |

**Required rows.** Owned by boot per profile template; a bundle may add ids and never remove one. Every server profile: `store`, `store-postgres`, `jobs`, `outbox`, `http`, `api`, `tenants`, `audit`, `approval`, `change-hub`, `credential-lease`, `pii`, `secrets`, `supervisor`. Hosted adds `auth`, `fleet-agentapi`, `signal-ingest-alert-webhook`, `gateway-render`. The list is filtered by role.

**The matrix is a test on the built artifact.** `test/failure-matrix` starts the published image and the packed launcher with a temporary home and a test-only bundle of fault rows (throw at import, at validation, in `apply`, after `apply`; inject a service nobody provides; bind a taken port; never finish disposing). It asserts the exit code, readiness, the one grouped diagnostic and, for each policy row, that the write path and the model path are closed.

### Q14. Testing in isolation

| Tier | What it is | Who writes it |
|---|---|---|
| 1. Bare context mount | `const root = new Context(); await root.plugin(Fake...); await root.plugin(plugin, config)`; assert; `await root.fiber.dispose()`. A plugin is mounted with only the services it injects, each a fake from the definition's `testing` export. `await` rejects on a config or startup error. The leak test (Q4) is part of this tier. | Plugin author |
| 2. Conformance per definition | `conformance(makeProvider)` in the definition's `testing` export. Every provider runs it, the fake included, so a tier 1 test is only as loose as the contract. A remote adapter runs the same suite through the proxy fiber over each transport. This tier is ours; dsh has no named equivalent, and erased types make it the only contract check there is. | Definition author; run by provider authors |
| 3. Real composition | `loader-smoke` starts the built launcher on a test-only profile with its own home, mocking only what is external or non-deterministic (model, Slack, the investigator stream, the clock) from recordings, with `AIOPS_SNAPSHOT=replay` enforced in CI. It asserts durable state and outbound requests, not log lines. Golden dumps of every shipped profile. The failure matrix. | Bundle author |
| 4. Live | The lab on `self-host-full` (00 row 18); real-key runs self-skip without a key. | Maintainers |

dsh's policy is kept word for word: "Product-visible plugins require a non-unit REAL-composition test"; "Hand-built `ctx.plugin(...)` suites are insufficient."

**Before a pull request, a contributor runs:**

```sh
pnpm check                 # typecheck, lint, the constraints (kinds, import boundaries, manifest equals exports)
pnpm test --filter ...     # tier 1 and tier 2 for the changed packages, on embedded PostgreSQL
aiopsctl plugin check ./   # manifest, config examples, leak test, conformance of each definition provided,
                           # in each runtime the manifest lists, and the effects sandbox for tools and executors
pnpm smoke                 # tier 3: the dev profile from the built artifact, in replay
```

Coverage: per-file 100 % on definition packages and policy plugins, where an uncovered line is an unproven invariant; no gate elsewhere.

### Q15. Apps are profiles

There is one launcher, `aiops`. dsh's rule holds: no second binary that composes plugins.

| Profile | Bundles | Startup and rows |
|---|---|---|
| `hosted`, `self-host-full`, `aiops-only`, `dev` | As 00 section 6.3 | `server-startup` (in `aiops-base`) parses `--role edge\|api\|worker\|all` and provides the marker services `roleEdge`, `roleApi`, `roleWorker`; `roleAny` always. |
| `ctl` (`aiopsctl` is a shim for `aiops --profile ctl`) | `aiops-ctl` only | `ctl-startup` (argv), a `commands` registry, `api-client`, and one row per command group: `ctl-profile`, `ctl-plugin-check`, `ctl-agent`, `ctl-slack`, `ctl-approvals`, `ctl-incidents`. No server row is composed. A third party adds a command by adding a row. |
| `migrate` | `aiops-migrate` | `store`, `store-postgres`, `migrate-runner`: applies every composed package's migrations under an advisory lock and exits. |
| `agent-host`, `agent-k8s` | `agent-base` plus one | Render profiles. The `fleet-source-*` rows and a `render-runner` turn a features document and host facts into collector config. The same rows are composed by `aiops-stack` behind `fleet`. |

**Roles are services.** A row is placed in a role by its entry: `inject: [roleWorker]`. In an edge process nobody provides `roleWorker`, so worker rows stay pending, and the activation audit ignores a row that waits only on a role marker. A lint requires every inserted server row to name a marker, so a new row cannot land in the internet-facing role by omission. This is how dsh gates rows on a startup service (`inject: [webStartup]`), used for placement. The security boundary itself is unchanged: the api role's process is given no cross-tenant database role and no admin signing key (00 section 3.1).

**The agent side.** No runtime of ours runs on a customer host: the agent is the unmodified collector, our install script and served config (00 row 11). "The agent is a profile" therefore means a render profile, as today. Node is never installed on a customer host.

**Not a profile.** The dashboard app backend (a small Go program, because its SDK is Go) and the lab.

### Q16. Order of work with no date pressure

Each step ends with something that boots from the built artifact. Nothing is added to the kernel after step 1 without a decision record.

| # | Step | Proves |
|---|---|---|
| 0 | **The deciding spike** (section 4): vendor the kernel, boot a three-row profile from an image and a packed launcher; the realm bench; the event-loop probe; the job insert in a caller's transaction; the syntax check artifact | Whether this option proceeds at all |
| 1 | **Kernel, boot and the test kit.** The local upstream-derived source tree with its four maintained source modifications and the upstream tests green; profiles, bundles, layers, dumps, golden files; the constraints lint; the failure matrix on the artifact; `aiopsctl plugin check`. First definitions with fake, conformance and leak test: `telemetry`, `health`, `http`, `store`, `secrets`, `leases`, `jobs`, `outbox` | Bare mount, conformance, real composition, and the three roles of a seam, on infrastructure |
| 2 | **The policy spine as plugins.** `tenants` with realms, `identities`, `authz`, `audit`, `approval`, `change-hub`, `credential-lease`, `pii`, `remediation` with a fake executor; the admission check; the bypass suites; the policy rows of the failure matrix | That invariants stay fail-closed as plugins, before any feature depends on them |
| 3 | **The second and third runtimes.** The proxy fiber protocol over gRPC and worker threads; the `tools` remote adapter; Go and Python SDKs; the plugin manager; the gate; the index. Exit: a third party installs a TypeScript tool into a self-host profile from a registry, and attaches a Go tool as a remote plugin, with no checkout of this repository | The old P3 exit test, moved to the front because it is the model |
| 4 | **Live recomposition.** The queue, generation rollback, the site layer table with staged replicas, tenant enablement in realms, `supervisor` damping | Reload and tenancy under change |
| 5 | **Telemetry base as rows only.** `auth`, `gateway-render`, `fleet` with its sources, the installer, the role markers | The old P0 exit, with zero kernel code added |
| 6 | **The incident loop as rows only.** `signals`, `correlator`, `casefile`, `investigators` and `investigator-holmes`, `tools` and the three tools, `notifications`, `chat-slack`, `oncall-goalert`, `remediation-kubernetes`, the verifiers, `compliance`, the pillar presets | The 00 section 7.1 demo |
| 7 | **Hosted.** `sidecars-kubernetes`, the KMS backend, limits renderers, dashboards, cohorts, billing, the 100-tenant test | Tenancy at size |
| 8 | **Further pillars and triggered items.** | |

What keeps every later feature a plugin: the kernel export freeze; the rule that a feature change adds or changes rows in a bundle and moves a golden dump; and the lint that a consumer never names a provider.

---

## 4. When this is the right answer

**Choose the literal option when all five hold.**

1. Fidelity to dsh ranks above alignment with the Go ecosystem of the data plane: reactive fibers, `isolate`, live recomposition and plugins installed at run time without a rebuild are wanted as dsh has them, not approximated.
2. The team accepts owning about 9,000 lines of vendored kernel and boot code and syncing from an upstream that moves weekly.
3. The trust rule is accepted as it stands: in process means trusted, everything else is remote.
4. The plugin authors the project most wants are tool, ingest and notifier authors from the LLM ecosystem, more than backend engineers of the observability stack.
5. The spike passes.

**Choose a Go port when any of these outranks them.** The hosted multi-tenant service is the main business and shared fate inside a replica must be as narrow as possible; the contributors the project expects write Go; a static single binary for self-hosters matters; or the owner does not want a kernel that tracks an unstable upstream.

**My assessment as the architect of this lens.** The option is sound and it is the shortest path to what the owner asked for in the founding requirement. Its weakest points are the two that matter most to the hosted profile: shared fate on one event loop, and policy invariants that rest on trust instead of on a compiler-checked boundary. For a self-hosted, single-tenant product I would choose it without hesitation. For a product whose first profile is hosted multi-tenant, I would choose it only if the owner confirms conditions 1 and 2 in words, because those are value judgements no spike can make.

**If the panel chooses a Go port, keep three things from this draft.**

1. The vendored Cordis and loader test suites as the differential oracle for the Go kernel: the same scenario scripts run against both, and a difference is a bug in the port or a recorded departure.
2. The service-key catalogue and row inventory of Q6 and the hook table of Q8, which do not depend on the language.
3. The proxy fiber as the remote plugin model, and the realm as a generated group row.

**The spike (step 0).** Small, and it produces numbers instead of opinions.

| Experiment | Measure | Pass |
|---|---|---|
| Vendor the kernel; strip the Node-internals paths; run upstream tests; boot three rows from an image and from a packed single executable | Tests green; cold start of the `ctl` profile; image size; whether a packed launcher can load plugins from a profile directory | Tests green; `ctl` cold start under one second; a recorded answer for the packed launcher |
| Realm bench: 500 realms of 12 rows; mount all, unmount all, swap one root provider | Time to mount; p99 of one notification; heap; time to settle after the swap | Mount of one realm under 50 ms; a root swap settles inside the drain deadline |
| Event-loop probe: the token endpoint at 300 requests per second (00 section 5.5) while a neighbour row scrubs 1 MB prompts, in process and through the `cpu` pool | p99 latency of the token endpoint; event-loop delay | The 00 section 5.5 target holds with the pool |
| Jobs: insert a pg-boss job on the caller's transaction with row-level security on; roll back | The job exists if and only if the transaction committed | Holds; else `jobs-pgboss` is replaced by a small outbox-fed queue |
| Collector config check: the syntax package as WebAssembly against the agent binary as a subprocess | Size, time per check | Either under 50 ms per check |

---

## 5. What the record would change

| Place | Change |
|---|---|
| 00 section 1 | "One Go control-plane binary" becomes "one control-plane launcher on Node"; principle 3 becomes the seam rule (definition, provider, consumer); principle 5 becomes "fail-closed by dependency, with the required list, admission and kill switches in boot". Principles 1, 2, 4, 6, 7 and 8 stand. |
| 00 section 2.1 row 1 | Kernel language and plugin mechanism: this draft. Rows 4, 6 and 9: library names (Slack client, Connect, jobs). |
| 00 section 4 | Replaced by the definition catalogue (Q5) and the row inventory (Q6). |
| 00 section 6.1 | Reload: live recomposition (Q9); layer 5 becomes the realm's row list; required rows per Q13. |
| 00 section 7.2 | Phases re-ordered per Q16; the P3 ecosystem items move to step 3. |
| 03 section 2, 8.1, 8.2, 8.6 | Composition and lifecycle are the kernel's; River wording becomes the `jobs` definition; the remote protocol is the proxy fiber. |
| 07 | Rewritten: manifest, context API, definitions, hooks, test kit. Section 4.3 value types carry over as `@aiops/entity` and schemas. |
| 08 section 1, 4, 5 | Concept map collapses to "as is" for most rows; the repository becomes a pnpm workspace with `vendor/`, `packages/<group>/<name>`, `bundles/`, `apps/`; licence checklist gains the vendored MIT notices, an npm licence allow-list and a ban on registry access at run time in hosted. |
| DR-01 | Reopened. |

---

## 6. dsh property checklist

| # | dsh or Cordis property (source) | Status | How, or why not |
|---|---|---|---|
| 1 | One `Context` type mediates effects and dependencies (paper; `context.ts`) | Carried as is | The vendored kernel |
| 2 | Root context with four built-in services: `reflect`, `registry`, `events`, `logger` | Carried as is | |
| 3 | `ctx.extend`: child contexts by prototype | Carried as is | |
| 4 | `ctx.isolate(name, label)`; row option `isolate` with local and named realms | Carried as is | Used for tenant realms and pillar presets; forbidden for protected services |
| 5 | `ctx.intercept`; row option `intercept` | Carried as is | Carries the tenant binding of a realm |
| 6 | Services by name on `ctx` through a proxy, typed by declaration merging | Carried as is | |
| 7 | `provide` with a `check` predicate | Adapted | `check` may depend only on composition facts, never on a live probe |
| 8 | One provider per name per scope; a second throws | Carried as is | Part of the policy argument |
| 9 | A read without `inject` throws | Carried as is | |
| 10 | `Service` base class; callable services; `mixin`, `accessor` | Carried as is | |
| 11 | Three plugin shapes; `name`, `Config`, `inject`, `provide` | Carried as is | Mirrored in the manifest so the gate needs no import |
| 12 | `ctx.inject(deps, cb)` nested fibers; `@Inject` on methods | Carried as is | The form of an optional dependency |
| 13 | Fiber states `PENDING`, `LOADING`, `ACTIVE`, `FAILED`, `UNLOADING`, `DISPOSED` | Carried as is | |
| 14 | Epoch from the injected provisions; a swap restarts consumers | Carried as is | |
| 15 | Providers keep their implementation until dependents unload | Carried as is | |
| 16 | A failed fiber recovers only through `update()` | Adapted | `supervisor` calls it on a backoff for optional rows |
| 17 | No damping of provider flapping | Adapted | `supervisor` holds a flapping row |
| 18 | `ctx.effect`; disposers in reverse order, awaited; generator effects | Adapted | A disposal deadline; a leaked fiber takes the replica out |
| 19 | `INACTIVE_EFFECT` on a disposed fiber | Carried as is | |
| 20 | Effect labels and `getEffects()` | Carried as is | Shown by `aiopsctl plugin effects` and in the leak report |
| 21 | Events: `emit`, `parallel`, `serial`, `bail`, `waterfall`; `prepend`; context filters | Carried as is | |
| 22 | A waterfall listener that omits `next()` vetoes | Carried as is | Protected hooks clamp the result to the floor |
| 23 | `internal/*` events, `internal/listener` interception | Carried as is | Guards registration on protected hooks |
| 24 | `fiber.update` and the `internal/update` waterfall | Carried as is | |
| 25 | Volatile config committed without a restart | Carried as is | Replaces `ConfigReader.For` |
| 26 | Synchronous Standard Schema validation; serializable schemas | Carried as is | |
| 27 | Loader entry tree: `create`, `update`, `remove`, `resolve`, `await`, `locate` | Carried as is | |
| 28 | Groups; nested ids | Carried as is | |
| 29 | Include files, patch composition, journal write-back | Carried as is | Write-back only to layers an operator owns |
| 30 | Row `inject` gating on a startup service | Carried as is | Role markers |
| 31 | `!!js` expressions in rows | Not carried outside bundles | It is `eval` in process; a layer writable through an API would be remote code execution. Site and tenant layers are data-only. |
| 32 | Row order carries no load semantics | Carried as is | |
| 33 | Profiles as ordered bundles over an empty root | Carried as is | |
| 34 | Bundle: a package whose payload is a patch list; only direct bundles contribute | Carried as is | |
| 35 | Patch semantics: by id, whole config replaced, `insert`, unknown id warns, empty file fails | Carried as is | |
| 36 | Layers: bundles, profile, home, `--patch`, environment switch | Carried as is | Plus the tenant layer, as a realm's row list |
| 37 | `--dump-default-config` with provenance; `--dump-config-schema` | Carried as is | Golden files added |
| 38 | Profile templates, staged creation, `sanitizeProfile` recovery | Carried as is | |
| 39 | Two-anchor resolution through an immutable table installed in Node's resolvers | Not carried | It needs Node internals and a native addon. Standard resolution from the profile directory, plus a boot check that one copy of the kernel and of each definition is loaded. |
| 40 | Lockstep first-party versions, exact pins | Carried as is | |
| 41 | `engines.dsh`, else peer ranges, else no constraint | Adapted | `engines.aiops`; a third-party package with neither is denied |
| 42 | Exact-version exemptions in `compatibility.json`, never inherited | Carried as is | Off in hosted |
| 43 | Admission at install, startup, recomposition; denied rows detached | Carried as is | Plus tenant enablement and the remote handshake |
| 44 | Package installation by pnpm in the profile, registry plan, build-script approval, snapshot and restore | Carried in self-host | Not carried in hosted: images are immutable and no package manager runs there |
| 45 | Plugin manager as a plugin; cannot remove itself | Carried as is | |
| 46 | `plugin_manager` model tool | Not carried | No model manages plugins |
| 47 | One reload queue; package operations outside it | Carried as is | |
| 48 | ESM module replacement | Not carried in server profiles | Node loader internals; hosted runs fixed images. Allowed in `dev` for plugin authors. |
| 49 | Reload without rollback of siblings | Adapted | Generation rollback when a required or policy row fails |
| 50 | Required id list owned by boot; absent required rows ignored | Adapted | Per profile, add-only by bundles; absence stops boot |
| 51 | Failure matrix; optional failures warn; grouped diagnostic | Carried as is | Rows added for policy, roles, leaks, remote |
| 52 | `installFailLoud`: a detached rejection is fatal | Carried as is | Its cost in a multi-tenant replica is stated in section 2 |
| 53 | Readiness when the root fiber is active and the loader exists; bounded shutdown | Adapted | Drain deadline of 30 s under the orchestrator's grace period |
| 54 | Apps are profiles; one launcher | Carried as is | Server profiles, `ctl`, `migrate`, render profiles |
| 55 | Definition, provider, consumer; plugins depend on definitions | Carried as is | Lint, project references and the runtime check |
| 56 | Approval: fail closed when absent, floor before the chain, audited | Adapted | Same mechanisms; quorum and tiers of 03 section 8.3 instead of one-shot |
| 57 | Sandbox for agent tools | Not carried | No shell or process tool exists (00 section 1); executors run typed plans |
| 58 | Agent presets as nested row lists | Adapted | Pillar presets |
| 59 | `dsh-scope` scoped contribution sets | Carried as is | Vendored; backs tenant-scoped registries |
| 60 | "Model-visible means logged" | Adapted | "Operator-visible means logged": the outbox in the transaction |
| 61 | Bare context unit mount; per-registry cleanup test | Carried as is | The cleanup test is generic and runs in `plugin check` |
| 62 | Real-composition tests required; replay enforced in CI | Carried as is | |
| 63 | Per-file 100 % coverage | Adapted | Definitions and policy plugins only |
| 64 | Conformance suite per definition | Added, not in dsh | Erased types make it the contract check |
| 65 | Host and client plugin trees; Typert RPC | Not carried | The UI is the dashboard app plugin and Slack; RPC is Connect from schemas |
| 66 | Locale metadata in eight languages | Adapted | English required; others optional |
| 67 | dsh service-definition packages | Not carried | Single-user agent contracts; shape copied, code not reused |
| 68 | Single-threaded run-to-completion under every lifecycle transition | Carried as is | By using Node; this is the property a port must rebuild |

---

## 7. Not verified in this pass

- Whether a packed single executable on Node 26 can import ESM plugins from a profile directory. dsh mentions packaged executables and a virtual filesystem; the mechanism was not read. The spike answers it.
- Whether pg-boss joins a caller's transaction with row-level security on. Its documentation says an insert can use a supplied connection; not tested here.
- The cost figures in Q10 are estimates from reading `reflect.notify`, not measurements.
- The Cordis guides under `docs/` in the dsh repository and the paper's formal statement were not re-read; lifecycle facts come from the 4.1.0 source.
- Whether Node's public module customization hooks are stable enough to replace dsh's internal resolution table. This draft assumes they are not needed.
- That pnpm can enforce provenance verification at install was not checked; the index and cosign checks do not depend on it.
- OneUptime's licence was not confirmed (the hosting API reports none detected); it is cited only as evidence that the language is used for this kind of product.
- The upstream Cordis and loader test suites are not in the installed package; that they can be vendored and run is assumed from the repository layout.

---

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
