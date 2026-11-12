# Plugin system, round 3: the contributor and operator proposal

Status: proposal, 2026-11-10. One of four independent round-3 proposals. Lens: the people who live with the plugin system. It does not edit `docs/architecture/00-decisions.md`; if adopted, it replaces principle 3, row 1 of section 2.1, section 4 and the reload rule of section 6.1, and it moves the ecosystem items of P3 to the front of the order of work.

Sources read for this proposal:

- Current plan: `00-decisions.md` sections 1, 2.1, 2.2, 4, 6, 7; `03-c3-kernel.md` sections 2, 7, 8, 9; `07-c4-plugin-sdk.md`; `08-profiles-roadmap-adrs.md` section 1.
- dsh and Cordis, from the installed package `@deepseek-ai/dsh` 0.2.2-rc.1 (cordis 4.1.0, cordis-plugin-loader 1.1.0; the research note read 0.2.1-rc.2 with cordis 4.0.5, so where the two differ the source read today wins): `cordis/src/{context,fiber,reflect,registry,events,service,utils}.ts`, `cordis/README.md`, `cordis-plugin-loader/README.md` and `src/config/{entry,isolate}.ts`, `dsh-app-boot/README.md`, `dsh-plugin-manager/README.md`, `dsh-hmr/README.md`, `dsh-plugin-market/README.md`, `dsh-plugin-market-index/README.md`, `dsh-official-bundles/README.md`, `dsh-package-manifest/README.md`, `dsh-user-approval/README.md`, `dsh-scope/README.md`, and the documented `dsh web --dump-default-config` composition surface. Machine-specific row counts are not retained evidence for this public baseline. The test-support packages are development dependencies and are not installed; what is said about them comes from `docs/research/dsh-architecture.md` section 5.
- Go prior art, primary pages fetched today: the Caddy architecture page (reload), the containerd `plugin` package, the Krew plugin manifest guide, the cosign verification guide, the OpenTelemetry Collector Builder README, the go-plugin README. Everything else about prior art is taken from `docs/research/plugin-architecture.md`.

Words used here. A **definition** is a named service contract (dsh: Service Definition). A **provider** is a plugin that provides it. A **registry** is a definition whose service accepts contributions (dsh plural keys such as `ctx.tools`). A **contribution** is what a plugin registers into a registry. A **row** is one configured mount of a plugin in a profile. A **fiber** is the runtime instance of a row. A **generation** is one immutable composed row tree.

---

## 0. Three journeys, and what they force

The design below was written backwards from these three walks. Each step names the command or artefact that must exist.

### Journey A: a third-party author, through plugin interfaces

The same author ships three things. They differ where the table says so.

| | A new notifier | A new datasource | A new store provider |
|---|---|---|---|
| What it is | A contribution to the `notifiers` registry | A contribution to the `datasources` registry (a metrics, logs, traces or profiles query client) | A provider of a singular infrastructure definition: `store.blob`, `store.kv` or `store.sql` |
| Runtime | `grpc` or `go` | `grpc` or `go` | `go` only: it sits in the transaction path |
| Where it mounts | Tenant scope or process scope | Tenant scope or process scope | Process scope |
| Install | `aiopsctl plugin install`, no rebuild | The same; the `go` form needs a distribution build | `aiopsctl dist build`, always |
| Takes effect | Live, one row | Live, one row | At restart (Q9, infrastructure class) |
| Hosted | Out of process, one instance per tenant | Out of process, one instance per tenant | First-party or operator-signed only |

| # | Step | What the author uses | Designed in |
|---|---|---|---|
| 1 | Scaffold | `aiopsctl plugin new notifier --lang go` writes `plugin.yaml`, a config schema, one test on fakes, `conformance.yml` and a signing workflow | Q2, Q14 |
| 2 | Write | Imports only the SDK module: `sdk/kernel` and `sdk/def/notifiers/v1`. Nothing from the kernel repository | Q5 |
| 3 | Test alone | `go test`: the real kernel on a bare root context, with the fakes that ship beside each definition | Q14 tier 1 |
| 4 | Prove the contract | `aiopsctl plugin check ./`: manifest, import boundary, residue test, the definition's conformance suite in every runtime the manifest lists | Q14 tier 2 |
| 5 | Prove it composes | `aiopsctl plugin smoke ./ --against <released aiops image>`: boots the released artifact with a generated test profile that adds this plugin | Q14 tier 3 |
| 6 | Publish | `aiopsctl plugin pack` builds an OCI artifact (manifest, image digest or Go module reference, conformance report), the CI workflow signs it, one pull request adds one file to the plugin index | Q11 |
| 7 | Install | A self-hoster runs `aiopsctl plugin install acme/notifier-teams@1.2.0 --profile self-host-full`: signature and trust policy, compatibility gate, `plugins.lock`, the deployment unit, a disabled row | Q11, Q12 |
| 8 | Enable | `aiopsctl plugin enable acme/notifier-teams`: one generation, applied live, rolled back if it does not settle | Q9 |

### Journey B: a Python author, out of process

| # | Step | What the author uses |
|---|---|---|
| 1 | `pip install aiops-sdk`, then `aiopsctl plugin new investigator --lang python` | A package generated from the same protos as the Go remote adapter |
| 2 | Subclass `investigators.Investigator` (or `tools.Tool` for a pillar tool) and call `serve()` | The process serves a lifecycle service and the definition's service on `AIOPS_PLUGIN_ADDR` |
| 3 | `pytest` with `aiops_sdk.testing` | Python fakes of the host callbacks, and replayed model cassettes |
| 4 | `aiopsctl plugin check ./` | Starts the process on a temporary socket and runs the same conformance suite a Go provider runs, through the remote adapter |
| 5 | `aiopsctl plugin smoke`, pack, sign, index, install | Identical to journey A steps 5 to 8 |

The Python author never needs a Go toolchain, a kernel checkout or a database. The investigator holds no credential except its virtual model key: tools are dispatched by the kernel, the model is reached through the gateway, budget and slots are held on the kernel side (Q11).

### Journey C: the hosted operator

| # | Moment | What happens | Blast radius |
|---|---|---|---|
| 1 | Admit a plugin to hosted | `aiopsctl plugin install --profile hosted`: the signer must be in the hosted trust policy, the gate passes, the conformance suite is run again against the pulled image, the version is pinned by digest in the lock. The row exists, disabled, marked tenant scope | None |
| 2 | Enable for one tenant | `aiopsctl plugin enable acme/notifier-teams --tenant acme-prod`: a generation whose scope is that tenant's subtree. The sidecar reconciler creates one pod in namespace `t-acme-prod` with a network policy rendered from the manifest. The tenant's remote row goes PENDING, handshakes, becomes ACTIVE and registers into `notifiers` for that tenant only. No process restarts | One tenant, one plugin |
| 3 | Upgrade | `aiopsctl plugin update acme/notifier-teams 1.3.0 --cohort canary`: check, start the new pod beside the old one, re-point the row, judge (handshake, a conformance probe, error rate over the bake window), then promote. The old version stays pinned in the lock for the bake period | Canary tenants |
| 4 | 03:00, version 1.3.0 hangs on `Send` | The registry guard has a deadline, a concurrency cap and a breaker per (tenant, row). The breaker opens, calls fail fast, the `notify` dispatcher falls back to the next target of the incident, and the timeline says so. Paging never depended on it. An alert carries row, tenant, version, generation and last-known-good generation | One tenant, one plugin; the incident still pages |
| 5 | Automatic | The failure started inside the bake window of a generation that changed this row, so the composer rolls the row back to the last-known-good version for the affected tenants | Shrinks to zero |
| 6 | Manual, if needed | One command each: `plugin rollback --tenant`, `plugin disable --tenant`, or `plugin quarantine acme/notifier-teams@1.3.0`, which denies that exact version in the gate for every tenant. Every one is a generation, audited and reversible with `aiopsctl profile rollback --to <generation>` | Chosen by the operator |

### What the journeys force

1. The kernel ships in the SDK module, so a plugin is tested on the real runtime, not on an imitation of the host.
2. Third-party code contributes to registries, and a contribution swaps without restarting its consumers.
3. The remote protocol exists from the first registry, not from P3.
4. Install uses supported plugin interfaces: a remote plugin needs no rebuild, an in-process plugin needs a generated distribution. Neither path changes the core source.
5. A tenant is a subtree, so enabling a plugin for one tenant reloads one subtree.
6. Every change of composition is a recorded generation with a last-known-good.
7. Signature, index and compatibility are checked at the boundaries the platform owns: build, install, boot, reload, handshake.
8. Policy sits on the calling side of every contribution, so a broken or hostile contribution meets a deadline, a breaker and a fallback.

---

## Q1. Kernel runtime and language

**Choice: a Go port of the Cordis model, complete, published inside the SDK module.** Not TypeScript on Cordis.

| Option | Verdict | Reason |
|---|---|---|
| TypeScript on Cordis (the literal dsh kernel) | No | Every plugin runs in one Node process with full trust: the dsh sources say twice that their checks "are not a sandbox against malicious package code" and that installed Host code "executes in-process outside the workspace sandbox". That is right for a personal agent and wrong for a multi-tenant server that admits third-party code. The Cordis README still says its API may change without notice. The upstream stack, the agent and their contributors are Go. Nothing in journeys A to C becomes easier in Node except module hot swap, which a server does not need |
| Go port of the Cordis model | Yes | The model is small: Cordis core is about 2,800 lines including its logger. What we port is the semantics (context tree, named services, epochs, effects, waterfall hooks, isolate, loader, gate, failure policy, test tiers), and we port all of it. What cannot cross is the JavaScript mechanics (the checklist at the end) |
| Go compile-time registry with a fixed boot order (the current plan) | No longer | It was chosen to protect dates. It gives composition but not activation, reload or isolation |

**What the kernel contains.** Three pieces, mirroring dsh, where Cordis is the runtime, `dsh-app-boot` is the launcher library and the Loader is a plugin.

| Piece | Path | Contains | dsh equivalent |
|---|---|---|---|
| Kernel | `sdk/kernel` (SDK module, standard library only) | Context tree; service store keyed by (name, scope); fibers with states and epochs; effects and disposers; tracked goroutines; hook chains; scope values; config validation hook; the lifecycle loop; a logger | `cordis` |
| Launcher | `boot/` (main module) | Profile discovery; layer composition; dumps; the module table; admission (compatibility, signature, sealed definitions, licence); the mandatory and required policy; fail-loud; readiness and bounded shutdown; the `prepare` step that provides launch facts before any row mounts | `dsh-app-boot`, `profile-boot` |
| Loader | `loader/` (a plugin) | The entry tree: create, update, remove, resolve, await, locate; the `group` and `include` builtins; volatile config commits | `cordis-plugin-loader`, `cordis-plugin-include`, `cordis-plugin-group` |

**What the kernel and the launcher must not contain.** No database driver, no HTTP server, no job runner, no tenant type, no approval, audit or scrub logic, no domain type, no gRPC. A CI check fails the build when `sdk/kernel` imports anything outside the standard library, or when `boot/` or `loader/` imports a package under `sdk/def/` or `plugins/`.

**The one thing Go changes.** Cordis relies on JavaScript run-to-completion: a transition cannot be interleaved with another. Go has real parallelism, so the kernel owns one lifecycle goroutine that decides every fiber transition. `Apply` and disposers run on worker goroutines, independent fibers load in parallel under a bound, and a fiber has at most one transition in flight (the Cordis `inertia` rule, `fiber.ts`). Request goroutines never touch fiber state: they hold service values captured at `Apply`, and an unload drains them before it disposes (Q3).

---

## Q2. The context model

Carried from Cordis as is: a root context, child contexts that inherit, services looked up by name in the caller's scope, `provide` and `inject`, one provider per name per scope, optional reads, per-name isolation, and a fiber per plugin application. Adapted: names carry a Go type, and a service can hand each consumer a view bound to the consumer's own context, which replaces the Cordis proxy that traces `this.ctx`.

```go
// Package kernel is the whole runtime a plugin author sees. Standard library only.
package kernel

// A Key names a service and fixes its Go type. Keys are declared in definition packages only.
type Key[T any] struct{ name string }
func NewKey[T any](name string) Key[T]

type Dep struct{ name string }      // an untyped reference to a key, for declarations
func Need[T any](k Key[T]) Dep

// A Plugin is data plus one function. There is one shape, not three.
type Plugin struct {
	ID       string // "<owner>/<name>", equal to the manifest id
	Inject   []Dep  // required: the fiber is PENDING until each is available in its scope
	Provides []Dep  // singular services it provides; Provide of an undeclared key is an error
	Config   Schema // JSON Schema 2020-12, validated before Apply
	Apply    func(ctx *Context, cfg Config) error // everything it registers is an effect on ctx
}

// Services.
func Use[T any](ctx *Context, k Key[T]) T          // k must be in Inject, else ErrNotInjected
func Get[T any](ctx *Context, k Key[T]) (T, bool)  // optional read, no activation coupling (Cordis ctx.get)
func Provide[T any](ctx *Context, k Key[T], impl T, o ...ProvideOption) error
// ProvideFor gives each consumer a view bound to the consumer's context. The plugin id, the row id
// and the scope values (the tenant) of the caller are filled by the kernel, never by the caller.
func ProvideFor[T any](ctx *Context, k Key[T], view func(caller *Context) T, o ...ProvideOption) error

// Optional dependency as a child fiber: body runs while deps exist and unwinds when one vanishes
// (Cordis ctx.inject).
func (ctx *Context) When(deps []Dep, body func(*Context) error) *Fiber

// Effects (Q4).
type Disposer func(context.Context) error
func (ctx *Context) Effect(label string, do func() (Disposer, error)) error
func (ctx *Context) Go(label string, run func(context.Context) error) // tracked: cancelled and awaited on unload
func (ctx *Context) Std() context.Context                              // cancelled when the unload starts
func (ctx *Context) Probe(fn func(context.Context) Health)             // optional health probe, an effect

// Tree.
func (ctx *Context) Plugin(p Plugin, cfg any) *Fiber   // mount a child fiber
func (ctx *Context) Isolate(names ...string) *Context   // a new scope for those names below this context
func (ctx *Context) With(v ScopeValue) *Context         // a typed value that services read from the caller (Cordis intercept)
func (ctx *Context) Config() Config                     // current snapshot; volatile fields change in place
func (ctx *Context) Log() *slog.Logger

type Fiber struct{ /* State, Await, Update, Restart, Dispose, Effects */ }
```

A definition is a small package: a key, an interface, a version.

```go
// sdk/def/notifiers/v1: a registry (plural key).
package notifiers

var Key = kernel.NewKey[Registry]("notifiers")

type Registry interface {
	// Register is an effect on ctx: the contribution is gone when the fiber unloads.
	Register(ctx *kernel.Context, spec Spec, n Notifier) error
}
type Notifier interface {
	Send(ctx context.Context, n *Notification) (*Delivery, error) // idempotent on n.IdempotencyKey
}
```

A third-party plugin, complete:

```go
package teams

var Plugin = kernel.Plugin{
	ID:     "acme/notifier-teams",
	Inject: []kernel.Dep{kernel.Need(notifiers.Key), kernel.Need(secrets.Key)},
	Config: kernel.SchemaFS(files, "config.schema.json"),
	Apply: func(ctx *kernel.Context, cfg kernel.Config) error {
		var c Config
		if err := cfg.Decode(&c); err != nil {
			return err
		}
		n := &notifier{secrets: kernel.Use(ctx, secrets.Key), cfg: c}
		return kernel.Use(ctx, notifiers.Key).Register(ctx, notifiers.Spec{Name: "teams"}, n)
	},
}
```

There is no `Start`, `Shutdown` or `Health` method to implement and no `init()` registry. A distribution's generated `main.go` passes the launcher an explicit module table (`map[string]kernel.Plugin`), which is the dsh "one immutable runtime resolution" in Go form; tests build any table they like.

**Declaration: manifest plus code.** The manifest is what the launcher, the index and the gate read without running code. The code declaration is what the kernel enforces. `aiopsctl plugin check` fails when they disagree; `uses`, `registers` and `hooks` are generated from the code, the rest is written by hand.

```yaml
apiVersion: aiops.dev/v2
kind: Plugin
id: acme/notifier-teams
version: 1.2.0
title: Teams notifier            # display metadata; English required, other locales optional
license: Apache-2.0              # SPDX, required
stability: preview
engines: {aiops: ">=1.4 <2"}     # SDK (kernel API) range, required: an absent range is denied
uses: [notifiers/v1, secrets/v1] # definition majors consumed
provides: []                     # singular services provided
registers: [notifiers]           # registries contributed to
hooks: []                        # hook chains listened on (Q8)
scope: [tenant, process]         # where a row of it may mount; the first is the default
runtimes:
  - {kind: grpc, protocol: 1, image: "registry.example/acme/notifier-teams@sha256:..."}
  - {kind: go, module: example.com/acme/aiops-notifier-teams, package: ./teams}
capabilities:
  network: ["*.webhook.office.com:443"]   # rendered as the egress policy of a remote instance
  secrets: [webhook-url]                  # the secrets provider refuses any other name
configSchema: ./config.schema.json        # x-aiops-tenant and x-aiops-volatile mark fields
conformance: ./conformance.yml
```

**Rows.** A row is `{id, plugin, config, enabled, required, inject, isolate, group}`. `plugin` is the manifest id and replaces today's `type`. `inject` on a row adds requirements to that mount only, which is the dsh idiom for "this row waits for a startup service" and is how roles work (Q15). `isolate` and `group` are the Loader's (Q10).

**Naming.** dsh's convention is kept: a singular key for one engine or policy (`store.sql`, `approval`, `llm`), a plural key for a registry (`notifiers`, `tools`, `executors`).

---

## Q3. Activation

**Choice: service-availability-driven fibers, as in Cordis.** Row order carries no load meaning. The fixed dependency order of the current plan is dropped.

| State | Meaning (Cordis `FiberState`) | Server adaptation |
|---|---|---|
| PENDING | Waiting for every injected service to be available in scope | None |
| LOADING | Config validated, `Apply` running | `Apply` has a deadline (30 s by default) and a cancellable context |
| ACTIVE | Loaded and providing | None |
| FAILED | Config or `Apply` failed. The fiber stays and recovers only through an update | The Loader adds a bounded retry for optional rows (below) |
| UNLOADING | Disposers running | Two phases: drain, then dispose (below) |
| DISPOSED | Removed, cannot restart | None |

**Epochs, carried as is.** A fiber's epoch is its own revision followed by the provision id of each injected service (`fiber.ts` `_refresh`: `epoch += ':' + impl.uid`). A missing service makes the epoch inactive and the fiber unloads or stays pending. A changed provision id means a provider was replaced, and the consumer unloads and loads again. This is how "swap one provider and the product follows" works with no code in the consumer.

**Provider outlives its dependents, carried as is.** A provider that leaves ACTIVE notifies its dependents at once but disposes its own effects only after every dependent has finished unloading (`fiber.ts` `_unload`), so a consumer can still use the service during its own teardown.

**Unload on a server: drain, then dispose.** Cordis has no requests in flight. We add one rule: a registration that admits work (a route, a job handler, a subscription, a registry contribution) has a disposer in two steps. Step one stops admission at once. Step two waits for the calls already inside, up to the drain deadline of the fiber (10 s by default, 30 s for job handlers), then cancels their contexts. Only then do the remaining disposers run. A drain that times out is reported, not ignored (Q13).

**Availability is structural, health is not availability.** Cordis lets a provider pass a `check` predicate so that it is present but unavailable. On a server a predicate tied to liveness would unload half the tree every time the database blinks. The rule: `check` may depend only on configuration and on state the provider owns (for example "no Slack token is configured"), and the provider calls `Notify` when it changes. A database outage does not change any epoch: calls fail, health turns `failed`, readiness fails, and nothing unloads. A remote plugin's lost connection is the same: the proxy fiber stays ACTIVE, the breaker opens (journey C step 4).

**Failure.**

| Event | What the kernel does | What the Loader policy adds |
|---|---|---|
| Config fails the schema | FAILED, error recorded | Nothing; a corrected config recovers it |
| `Apply` returns an error, panics or passes its deadline | Effects made so far are disposed in reverse, FAILED | Optional row: three retries with backoff (1 s, 10 s, 60 s), then quarantined until the next generation or `aiopsctl plugin restart`. Required row at boot: stop. Required row during a reload: the generation is rolled back (Q9) |
| A goroutine started with `ctx.Go` panics | The fiber is unloaded and marked FAILED | Optional row: as above. Required row: fatal, the process exits non-zero and the orchestrator restarts it (dsh: a detached failure is fatal "because only the throw site knows which state is intact") |
| A goroutine started with a bare `go` statement panics | Nothing can recover it: Go ends the process | A lint rule forbids bare `go` in plugin packages. This is one reason third-party code runs out of process in hosted |

**Restart storms.** Four sources, four rules.

| Source | Rule |
|---|---|
| A crash-looping `Apply` | A FAILED fiber never restarts by itself (Cordis). Only the bounded retry above exists, and it ends in quarantine |
| A flapping `check` | A provider may change availability at most once per 30 s. Beyond that the kernel holds it unavailable and reports `provider-flapping` |
| A cascade after a provider swap | The lifecycle loop settles the whole availability graph first, then acts: one unload wave in reverse dependency order and one load wave in dependency order. A fiber restarts at most once per generation |
| A few hundred tenant subtrees after a restart | Tenant subtrees mount lazily, with a concurrency bound and jitter (Q10) |

**What declarative `Inject` buys before boot.** Because requirements are data, the launcher reports at dump time what Cordis can only report after settling: a dependency cycle, and a row whose injected service no composed row provides ("no provider composed for `oncall`", which is expected in `dev`).

---

## Q4. Effects and disposal

**Rule.** A plugin never calls a global. Every registration goes through its context, and every service method that registers something takes the caller's context and wraps the registration in `ctx.Effect`. The disposer is recorded with a label on the caller's fiber. On unload the fiber's disposers run in reverse registration order, one after another. (Cordis starts a fiber's top-level disposers in reverse order but lets asynchronous ones overlap, `fiber.ts` `_unload` with `Promise.all`; a server prefers the deterministic form.) Registering on a disposed fiber is an error, as Cordis's `INACTIVE_EFFECT` is, which is what turns "this test only passes alone" into a defect.

| Registration | Made through | Disposer |
|---|---|---|
| Service | `kernel.Provide` | Unregister, notify dependents, wait for them |
| Route, listener | `http.Handle(ctx, ...)` | Remove the route, drain requests |
| Job handler, periodic schedule | `jobs.Handle(ctx, ...)`, `jobs.Every(ctx, ...)` | Stop fetching that kind on this replica, drain running jobs |
| Event subscription | `events.Subscribe(ctx, ...)` | Stop delivery to this replica, drain |
| Registry contribution (notifier, tool, executor, datasource, signal source, command) | `<registry>.Register(ctx, ...)` | Remove the contribution, drain calls inside it |
| Hook listener | `kernel.On(ctx, hook, ...)` | Remove the listener |
| Metric, collector | `metrics.Register(ctx, ...)` | Unregister |
| Goroutine | `ctx.Go` | Cancel and await |
| Connection, client, cache | `ctx.Effect` | Close |

**Runtime effects and durable effects.** This is the place where a multi-replica server cannot follow Cordis literally. Cordis reverts a component's side effects on removal, and all of them live in one process. Ours do not: a migration, a row, a queued job, a pod, a rendered gateway listener and a Slack installation are shared by every replica and must survive a rolling restart. Unloading a fiber on one replica must not drop a schema or delete a tenant's pod.

| Kind | Examples | Unload of the fiber (restart, reload, disable) | Uninstall or purge |
|---|---|---|---|
| Runtime effect | Everything in the table above | Fully reverted, on that replica | n/a |
| Durable effect | A schema and its migrations, KV keys, blobs, secrets, job kinds and schedules stored in the database, per-tenant pods, rendered upstream configuration | Detached only. The durable state stays | Removed by the platform, by namespace |

Durable effects are declared through the same context (`store.Migrate(ctx, fs)`, `sidecars.Want(ctx, spec)`), so they are owned and listed, but two rules apply. First, every durable namespace is keyed by plugin id: schema `p_<plugin>`, KV prefix, blob prefix, secret scope, job-kind prefix, pod label. Second, anything shared between replicas is reconciled by a leader job from the composition generation, which is data, and never from "which fibers happen to be mounted on this replica". A fiber only binds to what the reconciler made.

**Migrations.** Forward only, expand then contract, run by `aiops migrate` under an advisory lock before a rollout, as the current plan has it. A plugin has no down migration. Its unwind is the purge of its namespace at uninstall. A binary refuses a plugin schema more than one version newer than it knows, so a plugin version can be rolled back by one step at 03:00 without touching data.

**The rule and the tests that prove a plugin leaves nothing behind.**

1. **Unload residue test** (every plugin, in `plugin check` and CI). `kerneltest.AssertNoResidue(t, root)` after disposing the fiber: no effect, no provision, no hook listener, no tracked goroutine, no contribution in any fake registry, no open lease on a fake. The fakes count their registrations, so the check is exact, not a goroutine heuristic.
2. **Registry dispose test** (every definition that accepts registrations, in its conformance suite). Mount a contributing fiber, dispose it, assert the registry is empty and that a call in flight was drained. This is dsh's mandatory test: "Every registry gets an HMR-safety test (dispose the contributing fiber, assert cleanup)".
3. **Uninstall residue test** (every plugin with a durable effect, in the store conformance harness). Install, write, uninstall, then assert that no schema, table, KV key, blob, secret, job kind or schedule owned by the plugin id remains. The plugin supplies no code for this: the platform drops by namespace.
4. **Mount twice test.** Mount, dispose, mount again on the same root, and run the plugin's own tests. A leftover from the first mount fails the second.

---

## Q5. Service definitions

**Rule, taken from dsh word for word:** "Extension plugins depend on Service Definitions, never concrete providers." And its companion, with the punctuation adapted: "A seam has three roles (Service Definition, Service Provider, Consumer) and all three must exist."

**This replaces principle 3.** "Extension points are earned by two real implementations" becomes: a definition is admitted when it has a provider, a consumer, a fake and a conformance suite, and the fake passes the suite. The fake is the second implementation, and it is real in the sense that matters: the same suite judges both. A definition stays at `v0` (free to change, not offered to third parties) until one release has shipped with it; the stability promise starts at `v1`.

**Catalogue.** Singular keys name one engine or policy. Plural keys are registries.

| Layer | Definitions |
|---|---|
| Infrastructure | `metrics`, `tracer`, `store.sql`, `store.kv`, `store.blob`, `jobs`, `events`, `quota`, `http`, `secrets`, `kek`, `loader`, `packages` |
| Platform | `tenants`, `identity`, `authz`, `audit` (with `auditSinks`), `pii`, `approval`, `ledger`, `leases`, `executors`, `llm` (with `llmProviders`), `tokens`, `sidecars`, `gatewayRoutes`, `limitsRenderers`, `pluginManager`, `pluginIndex`, `commands`, `role.edge`, `role.api`, `role.worker` |
| Domain | `signals` (with `signalSources`), `correlate`, `casefile`, `investigators`, `tools`, `toolsetBindings`, `datasources`, `notifiers`, `routing`, `chatSurfaces`, `chatCommands`, `oncall` (with `onCallProviders`), `compliance`, `estate` (with `estateSources`), `content`, `fleet` (with `fleetFragments`), `reportRenderers` |

**Where they live.** One SDK Go module with its own semver, so a third party needs exactly one dependency.

| Path | Holds |
|---|---|
| `sdk/kernel`, `sdk/kerneltest` | The runtime and its test helpers |
| `sdk/def/<name>/v<N>` | The key, the interfaces, the message types, the hooks the definition runs |
| `sdk/def/<name>/v<N>/fake` | An in-memory provider that passes the suite and records its calls |
| `sdk/def/<name>/v<N>/conformance` | The suite every provider must pass, in every runtime it ships in |
| `sdk/remote`, `proto/aiops/def/<name>/v<N>` | The remote protocol and the wire form of each remote-capable definition |
| `sdk/entity` | The entity reference (a library, not a plugin) |

Providers and dispatchers live in the main module under `plugins/<id>/`, each exporting one `var Plugin kernel.Plugin` and keeping everything else under `internal/`.

**How the rule is enforced.**

| Check | Mechanism | Runs in |
|---|---|---|
| A plugin imports only the SDK, the standard library and its own dependencies | `go list -deps` on the plugin package: no import path under the kernel repository except `sdk/` | `aiopsctl plugin check` (works in a third-party repository), CI |
| No plugin imports another plugin | The same listing; and each plugin's code sits under `internal/`, which the Go compiler enforces | compiler, CI |
| The kernel is domain-free | `sdk/kernel` imports the standard library only; `boot/` and `loader/` import no `sdk/def/` or `plugins/` package | CI |
| A plugin uses only services it declared | `kernel.Use` of a key not in `Inject` fails; `Provide` of a key not in `Provides` fails; the manifest must equal the code | kernel, `plugin check` |
| The AGPL deny-list of the current plan | Unchanged | CI |

**Versioning.** Two numbers, deliberately not a matrix.

| What | How it versions | What breaks compatibility |
|---|---|---|
| SDK module (kernel API plus every definition's additive growth) | Semver. A minor release only adds: a new definition, a new optional interface, a new hook stage | A change to `sdk/kernel` behaviour or signatures is a major release |
| A definition | Its major is in the import path (`notifiers/v1`). Growth inside a major is by optional interfaces, the Go idiom the current SDK already uses for `Heartbeater` | A breaking change is a new package `v2`. The dispatcher serves `v1` and `v2` side by side for at least one minor release through an adapter, so installed plugins keep working |

The wire form follows the same major (`aiops.def.notifiers.v1`). First-party plugins are compiled together and released in lockstep with the distribution, as dsh's are. Third-party plugins declare a range (Q12).

---

## Q6. What becomes a plugin

Everything that has a lifecycle, state or a registration. What stays a library: code with none of the three, used through an import (`sdk/entity`, the fingerprint arithmetic inside `correlate`, the answer parser inside `casefile`). dsh draws the same line: `dsh-scope` and `dsh-util-*` are libraries.

Plugin ids are under `aiops/`. Bundle names are today's, shortened: base is `aiops-base`, and so on.

| Today (00 section 4) | Row or rows | Provides | Injects | Bundle |
|---|---|---|---|---|
| `loader` | `loader`, with builtins `group` and `include`, mounted by the launcher | `loader` | none | launcher |
| `plugin-host`, row registry | Gone: the kernel and the module table | | | |
| Host services `logger`, `config`, `lifecycle` | Kernel builtins: `ctx.Log`, `ctx.Config`, `ctx.Go`, `ctx.Effect` | | | kernel |
| Host service `discovery` | Dropped: `kernel.Get` and registries replace "find another row" | | | |
| `metrics`, `tracer` | `telemetry` | `metrics`, `tracer` | none | base |
| `store` (SQL and KV) | `store-postgres` | `store.sql`, `store.kv` | none | base |
| `store` (blob) | `store-blob-s3` | `store.blob` | none | base |
| `scheduler` | `jobs-river` | `jobs` | `store.sql` | base |
| `events` | `events-outbox` | `events` | `store.sql`, `jobs` | base |
| Limiters: lease rows and buckets | `quota` | `quota` | `store.sql`, `store.kv` | base |
| `api-server`, `http.router` | `http-server` | `http` | `metrics` | base |
| `secrets` | `secrets` | `secrets` | `store.sql`, `kek` | base |
| `secrets-backend.*` | `kek-local-file`; `kek-kms` | `kek` | none | base; hosted |
| `tenant`, `TenantLimits`, tenant sagas | `tenants` | `tenants` | `store.sql`, `jobs`, `events`, `audit`, `loader` | base |
| `identity.*` | `identity`; contributions `identity-oidc`, `identity-service-token`, `identity-slack-link`, `identity-dashboard-id-token` | `identity` | `http`, `secrets`; optional `tokens` | base; chatops-slack; stack |
| Bindings and roles inside `api-server` and `admin` | `authz` | `authz` | `store.sql`, `identity` | base |
| `admin` | `admin-api` | routes | `http`, `identity`, `authz`, `jobs`, `pluginManager` | base |
| `audit` | `audit-chain` | `audit`, `auditSinks` | `store.sql`, `jobs` | base |
| `audit-sink.*` (P2) | `audit-sink-object-store`, `audit-sink-webhook` | contributions | `auditSinks`, `store.blob` | base |
| `pii` | `pii` | `pii` | `secrets` | base |
| `approval` and its tier table; approval handlers | `approval`; answerers `approval-api`, `approval-slack`, `approval-app` (P2) | `approval` | `store.sql`, `jobs`, `events`, `audit`, `identity`, `authz` | base; chatops-slack |
| `change-hub` and its verification checks | `change-ledger`; `verify-alert-cleared`, `verify-readback` | `ledger` | `store.sql`, `jobs`, `events`, `audit`, `approval`; optional `datasources`, `estate` | base |
| `credential-lease` | `credential-lease` | `leases` | `ledger`, `secrets`, `audit` | base |
| Point guard of `remediation-executor` | `actions` | `executors` | `approval`, `ledger`, `leases`, `audit`, `jobs`, `tenants` | base |
| `remediation-executor.kubernetes`, `.gitops-pr` | `executor-kubernetes`, `executor-gitops-pr` | contributions | `executors`, `leases` | pillars-mvp |
| `llm` gateway | `llm-gateway` | `llm`, `llmProviders` | `http`, `pii`, `quota`, `secrets`, `store.sql`, `tenants` | base |
| `llm-provider.*` | `llm-openai-compatible`, `llm-replay` | contributions | `llmProviders` | base; tests |
| Ingest core and the `signal-ingest` guard | `signals` | `signals`, `signalSources` | `http`, `store.sql`, `jobs`, `events`, `quota`, `tenants` | base |
| `signal-ingest.*` | `signal-alert-webhook`, `signal-generic-json`, `signal-gitops-webhook`, `signal-policyreport` | contributions | `signalSources` | base; pillars-mvp |
| Storm mode | `storm-guard` | a listener on `incident/open` | `signals`, `quota` | base |
| `correlate` | `correlate` | `correlate` | `signals`, `events`, `jobs`; optional `estate` | base |
| `casefile`, context pack, answer check | `casefile` | `casefile` | `store.sql`, `events`, `jobs`, `correlate`; optional `ledger`, `estate` | base |
| Point guard of `investigator` | `investigations` | `investigators` | `casefile`, `llm`, `tools`, `jobs`, `quota` | base |
| `investigator.holmes` | `investigator-holmes` | contribution | `investigators`, `sidecars`, `llm`, `toolsetBindings` | pillars-mvp |
| Point guard of `tool`, MCP facade | `tools` | `tools` | `http`, `pii`, `llm` | base |
| `tool.*` | `tool-change-lookup`, `tool-runbook-fetch`, `tool-oncall-who`, `tool-estate-lookup` (P2) | contributions | `tools`, and `ledger`, `content`, `oncall` or `estate` | pillars-mvp; oncall-goalert |
| `toolset-bindings` | `toolset-bindings` | `toolsetBindings` | `secrets`; optional `tokens` | datasources |
| `datasource/metrics`, `datasource/logs` | `datasources`; `datasource-promql`, `datasource-logql` (P2) | `datasources` | `secrets`; optional `tokens` | datasources |
| Point guard of `notifier` | `notify` | `notifiers` | `pii`, `jobs`, `store.kv`, `events` | base |
| `notifier.*` | `notifier-email`, `notifier-webhook`, `notifier-slack` | contributions | `notifiers` | base; chatops-slack |
| `router` | `routing` | `routing` | `casefile`, `notifiers`; optional `chatSurfaces`, `oncall` | base |
| `chat-surface`, `/aiops` commands | `chat`; `chat-slack`, `chat-commands-core` | `chatSurfaces`, `chatCommands` | `http`, `jobs`, `pii`, `identity` | base; chatops-slack |
| Point guard of `on-call-provider`, page job | `oncall` | `oncall`, `onCallProviders` | `jobs`, `store.kv`, `casefile` | base |
| `on-call-provider.goalert` | `oncall-goalert` | contribution | `onCallProviders`, `sidecars` | oncall-goalert |
| Sidecar supervisor | `sidecars-kubernetes`; `sidecars-static` | `sidecars` | `jobs`, `tenants`, `loader` | hosted; selfhost |
| `compliance` | `compliance` | `compliance` | `store.sql`, `jobs`, `signals`, `casefile` | pillars-mvp |
| `estate` and its reader tables (P2) | `estate`; `estate-source-series` | `estate`, `estateSources` | `store.sql`, `jobs`, `datasources` | base |
| Runbooks, playbooks, rule files (bundle content) | `content` serves them; the files stay content | `content` | none | base |
| `auth` | `auth-endpoints`, `auth-keys` | `tokens` | `store.sql`, `http`, `secrets`, `jobs`, `tenants` | stack |
| `gateway` renderer | `gateway-render` | `gatewayRoutes` | `tokens`, `jobs`, `tenants`, `store.sql` | stack |
| `fleet` core and config templates | `fleet`; one `fleet-fragment-<feature>` row per feature key | `fleet`, `fleetFragments` | `store.sql`, `jobs`, `tenants`, `tokens` | stack; agent bundles |
| `fleet-agentapi`; fleet sweeper | `fleet-agentapi`; `fleet-sweeper` | routes; jobs | `fleet`, `http`; `fleet`, `datasources` | stack |
| Backend provisioning inside `tenant` | `backend-provision` | listeners on `tenant/provision` and `tenant/delete` | `tenants`, `tokens` | stack |
| `limits-renderer.*` (P2) | `limits`; `limits-render-metrics`, `-logs`, `-traces`, `-profiles` | `limitsRenderers` | `tenants`, `jobs` | stack |
| `annotator` (P1), `dashboard-sync` (P2) | `dashboard-annotator`, `dashboard-sync` | subscribers and jobs | `events`, `jobs`, `tenants`, `tokens` | stack |
| `pillar.<name>` data-only rows | One `group` row per pillar, holding the rows it composes | none | row-level `inject` on `tools` or `signalSources` | pillars-mvp, pillars-ext |
| `report-renderer` (P3) | `reports`; `report-pdf` | `reportRenderers` | `store.blob`, `jobs` | pillars-ext |
| `plugin-manager` (P3 today) | `plugin-manager`, `plugin-index`, `config-watch` | `pluginManager`, `pluginIndex` | `loader`, `packages`, `audit`; optional `store.sql` | base |
| `--role edge\|api\|worker\|all` | `role`, the startup row that parses `--role` | `role.edge`, `role.api`, `role.worker`, as selected | none | base |
| Kill switches | Launcher layer 6, unchanged | | | launcher |

**Resulting inventory.** About 40 definitions, about 45 dispatcher and provider rows, about 35 contribution rows at the P1 scope. `self-host-full` composes roughly 80 rows. The comparison is structural: this uses dsh's composed row-tree model; machine-specific dsh row counts are not release evidence.

Three points the table cannot show.

1. **One transaction, three plugins.** "State change, outbox event, audit row and job commit together" stays true by contract: `store.sql` hands out a `Tx`, and `jobs.EnqueueTx(tx, ...)`, `events.EmitTx(tx, ...)` and `audit.AppendTx(tx, ...)` take it. The `jobs`, `events` and `audit` suites each assert "roll back the transaction and nothing was enqueued, emitted or appended". A third-party `jobs` provider that cannot join the store's transaction must implement `EnqueueTx` with an outbox relay, or it fails the suite.
2. **`store.sql` is a dialect contract.** Decision 9 stands: the definition promises PostgreSQL 16 semantics (forced row-level security, `SKIP LOCKED`, jsonb, advisory locks, `LISTEN/NOTIFY`), and its suite is that promise. A second provider is another way to reach or operate a PostgreSQL-compatible engine and is admitted when it passes. A different SQL engine is not what the definition says, and pretending otherwise would let plugin SQL pass tests and fail in production. `store.blob` and `store.kv` are small and freely replaceable, which is why journey A uses them.
3. **A pillar is a group row.** dsh declares an agent preset as a nested row tree. A pillar is the same thing: a `group` row whose children are its persona, toolset, signal sources and executors. The loader rule "a pillar must require a tool or a signal source" becomes a row-level `inject`, checked like any other.

---

## Q7. Policy that must not be bypassed, as plugins

Principle 5 says the approval floor, the change ledger, credential leases, the audit chain and the PII scrub are host invariants. They stay invariants. What changes is what "host" means: it is no longer a block of kernel code, it is a set of sealed first-party plugins held in place by four mechanisms.

**The rule that makes it work: a gate is a dependency, a hook only tightens.** A waterfall chain is fail-open on absence: if the listener that should veto is not mounted, the chain runs straight to the action. dsh knows this and does not gate through listeners alone: its tools pipeline injects the approval service and "fail[s] closed when it is absent". We do the same everywhere.

| Mechanism | What it guarantees |
|---|---|
| 1. Dependency chain | The only plugin that can call an executor is `actions`, and it injects `approval`, `ledger`, `leases` and `audit`. If any is missing, `actions` is PENDING, the `executors` registry does not exist, every executor row is PENDING, and no write path exists. Absence means no action, by construction |
| 2. The guard is on the calling side | A contribution is called only by its dispatcher (`actions`, `notify`, `llm-gateway`, `tools`, `signals`), and the dispatcher applies the invariant before or after the call. A notifier receives text that is already scrubbed. A model provider receives a request that is already scrubbed. An executor receives a write credential only from `leases`, against a token only `ledger` mints, for a plan only `approval` released |
| 3. Sealed definitions | The launcher holds a table of sealed definitions and admits a provider for one only if the provider is in the distribution's own signed set, or carries a conformance attestation signed by a key in the operator's trust policy. A profile layer cannot swap `approval` for an allow-all plugin unnoticed |
| 4. Mandatory rows | The five policy definitions are mandatory in every server profile: absent, disabled or inactive stops boot, and no layer or tenant overlay can change that (Q13) |

**The sealed table.** The five policy definitions `approval`, `ledger`, `leases`, `audit`, `pii`; the dispatchers that apply them, `executors`, `notifiers`, `llm`, `tools`, `signals`; and `secrets`, `tenants`, `identity`, `authz`. Sealing limits who may provide a definition. It does not limit who may contribute to a registry: a third-party notifier contributes to `notifiers`, and the sealed `notify` dispatcher stands between it and everything else.

| Invariant | Sealed provider | Injected by | If it is absent | Its suite proves |
|---|---|---|---|---|
| Approval floor | `approval` | `actions`, every approval answerer | No executor can be called | A result is never below the floor; L3 needs two distinct verified users; the requester cannot approve; a vote lost before dispatch voids the quorum |
| Ledger before a write | `change-ledger` | `actions`, `credential-lease` | No token, so no lease and no `Apply` | `Apply` without a valid token is refused; one verdict per change from the closed list |
| Credential leases | `credential-lease` | `actions`; executors through `leases` | No write credential exists anywhere else: write secrets are a class only this plugin's view of `secrets` can read | A lease is returned only for the action a valid token names; `Plan` and `Verify` get none |
| Audit chain | `audit-chain` | `approval`, `change-ledger`, `credential-lease`, `actions`, `tenants`, `plugin-manager` | All of them are PENDING | Append in the caller's transaction; update and delete are blocked; the export recomputes |
| PII scrub | `pii` | `llm-gateway`, `notify`, `chat`, artifact and outbox sinks | No model call, no notification, no chat post | Nothing reaches a contribution unscrubbed; pseudonyms are stable within a tenant |

**What stays outside plugins, and why dsh would agree.** Four things, all of which dsh also keeps in its launcher or its core, not in a plugin.

| Kept in | What | dsh precedent |
|---|---|---|
| Launcher | Admission: signature, trust policy, compatibility, the sealed table | "Admission happens only at composition boundaries DSH owns" (`dsh-app-boot`) |
| Launcher | The mandatory and required lists and the stop-boot policy | The global required list is owned by app-boot, not by a row |
| Launcher | Fail-loud on a detached failure of a required row | `installFailLoud` |
| Kernel | The caller-bound service view (`ProvideFor`): the plugin id and the tenant a service sees come from the kernel, never from the caller | The Cordis context proxy binds `this.ctx` to the caller |

**The honest limits.**

- In-process Go code shares the address space. A hostile in-process plugin can read memory and environment and bypass any of this, exactly as dsh says of itself ("not a sandbox"). So hosted admits in-process code only from our own signing identity, and every third-party plugin there runs out of process (Q11). On self-host the operator decides, and the distribution builder records what was linked.
- An operator with file access can sign and compose a different provider for a sealed definition. That is their system. The dump and `aiopsctl profile verify` report "sealed definition `approval` is provided by a signer outside the distribution", and each generation's digest and signer set is written to the audit chain, so an auditor sees it.

---

## Q8. Interception

**Choice: typed hook chains with Cordis waterfall semantics.** Listeners run outermost first; each receives `next`; a listener that does not call `next` vetoes the rest of the chain, including the built-in behaviour; calling `next` twice is an error (`events.ts` `waterfall`).

```go
// A Hook is declared in a definition package and run only by that definition's provider.
type Hook[Req, Res any] struct{ /* name, mode, stages, access, remote */ }
type Next[Req, Res any] func(context.Context, Req) (Res, error)

func On[Req, Res any](ctx *Context, h Hook[Req, Res], stage Stage,
	fn func(c context.Context, req Req, next Next[Req, Res]) (Res, error)) error // an effect on ctx
func Run[Req, Res any](ctx *Context, h Hook[Req, Res], c context.Context, req Req, inner Next[Req, Res]) (Res, error)
```

Three adaptations, each stricter than Cordis.

| Cordis | Here | Why |
|---|---|---|
| Order is registration order, with `prepend` | Each hook declares named stages; a listener picks one; inside a stage the order is by plugin id | Activation order is not fixed, so registration order would make policy order depend on timing |
| A throwing listener propagates | Each hook has a mode. `gate`: an error, a panic or a timeout in a listener is a veto. `observe`: it is logged and skipped | A policy chain must fail closed and a metrics listener must not take ingest down |
| Any plugin may listen on any event | Each hook has an access class: `open`, `signed` (the distribution's or the operator's trust root) or `sealed` (named plugins). The manifest lists the hooks a plugin listens on, and the gate checks the class | Third-party code may tighten, never loosen |

A remote plugin may listen only on hooks marked remote-capable, which are off the acknowledgement path.

| Hook | Run by | Mode | Who may listen | Remote | Typical listeners |
|---|---|---|---|---|---|
| `http/request` | `http-server` | gate | signed | no | Identity, tenant resolution, rate limits |
| `signal/admit` | `signals` | gate | signed | no | The per-tenant bucket, body limits. Before any write |
| `signal/normalize` | `signals` | gate | open | yes | Label mapping, `signal_type` lift. Runs in the ingest job, after the ack |
| `incident/open` | `correlate` | gate | signed | no | `storm-guard`, the open-unmapped cap; later the fold rules, which is how the deferred `correlator` point arrives |
| `investigation/context` | `casefile` | observe | open | yes | Context pack contributors: change candidates, estate facts, known issues |
| `investigation/answer` | `casefile` | gate | signed | no | The answer contract check, the finish gate (P2) |
| `llm/request` | `llm-gateway` | gate | signed | no | Budget, routing by tier, extra redaction. The scrub itself is a dependency, not a listener |
| `tool/invoke` | `tools` | gate | signed | no | Effects class, output cap |
| `action/plan` | `actions` | gate | open; a listener may deny or raise the tier, never lower it | yes | Change windows and freezes, blast radius from the estate, a Rego or CEL policy (the deferred `approval-policy` point) |
| `action/verify` | `change-ledger` | observe | signed | yes | The two zero-cost checks; later the paid verification turn |
| `approval/request` | `approval` | gate | signed | no | Answerer routing, as dsh's `approval/request` |
| `notify/send` | `notify` | gate | open; a listener may drop or delay, not change recipients | yes | Digest window, pacing, quiet hours |
| `tenant/provision`, `tenant/delete` | `tenants` | gate, with compensation | signed | no | Backend provisioning, sidecars, dashboards, purge steps |
| `config/resolve`, `fiber/update` | kernel | internal | launcher and loader | no | Reference resolution, volatile commits (Cordis `internal/config`, `internal/update`) |

Kernel hooks are in-process function chains. They are not the `events` service, which is the durable outbox that crosses replicas. Cordis has one event bus because it has one process; we keep the two apart by name.

---

## Q9. Composition and reload

**Composition stays.** Profiles, ordered bundles, id-addressed patch layers 0 to 6, whole-config replace in layers 1 to 4, the field-level tenant layer, `insert`, the dumps with provenance and the kill switches are unchanged from `00-decisions.md` section 6.1. Three things are added to rows.

| Addition | Form | dsh origin |
|---|---|---|
| Row fields | `plugin` (replaces `type`), `inject`, `isolate`, `group` | Entry fields of the Loader |
| References in config, not expressions | `${env:NAME}`, `${secret:name}`, `${service:<key>.<field>}`. Resolved in the `config/resolve` hook, after the row's injections exist | `!!js` expressions, and the rule that a row injects the service its config reads |
| Enablement predicates | `enabledWhen: {service: <key>}`, `{env: NAME}`, `{os: linux}` | `disabled: !!js ...` |

A reference or a predicate is data: the dump prints it verbatim, it can be evaluated without running plugin code, and configuration can never execute code. A reference that fails to resolve is reported as an error on the row and is never read as "disabled", which is dsh's rule for a throwing `disabled` expression.

A profile directory holds `profile.yaml` (the bundle list), `aiops.patch.yml`, `plugins.lock`, `trust.yaml` and `compatibility.yaml`.

**Decision: live recomposition.** The unit of reload is the row. The unit of change is a **generation**: one immutable composed tree with a digest. The restart-only rule of the current plan is dropped for configuration and kept for code.

| Change | How it applies |
|---|---|
| Enable, disable, insert or remove a row whose code is already present (compiled in, or remote) | Live |
| Change a row's config | Live. Fields marked `x-aiops-volatile` commit in place with no restart (dsh volatile config); any other field restarts that row and, through epochs, its consumers |
| Tenant overlay | Live, scoped to that tenant's subtree. Tenant-writable fields are volatile by rule, which replaces the per-call `ConfigReader.For` poll |
| Swap the provider of a restart-class definition (`store.*`, `jobs`, `events`, `kek`, the `http` listeners) | At restart. The kernel can do it live and the tests do; the launcher refuses it in every server profile except `dev`, because durable state does not move between providers by restarting a fiber |
| New or changed in-process code | A new binary and a rolling restart. Go cannot replace code in a running process |
| New or changed remote plugin code | Live: a new image, the row is re-pointed, no kernel restart |

**One serialized reload, in Go.**

```go
// boot/reload. One queue per process, one generation in flight, nested reloads rejected
// (dsh-hmr runExclusive).
type Generation struct {
	ID     uint64
	Scope  Scope      // the process, or one tenant
	Digest [32]byte   // sha256 over the canonical composed tree and the module table
	Tree   *EntryTree // immutable
}
func (q *Queue) Apply(ctx context.Context, next Generation) (Report, error)
```

| # | Step | Does | On failure |
|---|---|---|---|
| 1 | Compose | Layers over the empty root. Pure | Reject; nothing changed |
| 2 | Admit | Compatibility, signature, sealed table, the schema of every changed row, cycles, rows with no composed provider. Pure | Reject; nothing changed |
| 3 | Plan | Diff by row id: removed, changed, added. Compute the affected closure over the inject graph. Refuse a plan that leaves a mandatory or required row inactive, or that swaps a restart-class provider | Reject; nothing changed |
| 4 | Apply | One unload wave in reverse dependency order with drain; volatile commits; one load wave in dependency order | Go to 6 |
| 5 | Judge | Wait for the tree to settle within a deadline. A row that was ACTIVE and is not, or an explicitly enabled target that is not ACTIVE, is a failure. Rows that were already inactive before the change are warnings | Go to 6 |
| 6 | Commit or restore | Success: this generation becomes last-known-good. Failure: the previous generation is applied again to the affected rows | A restore that fails is fatal for a required row and a quarantine for an optional one |
| 7 | Publish | `composition/applied` with the digest and the report, written to the audit chain | n/a |

Two honest notes. dsh does not restore: its later configuration reloads do "not roll back the whole update". A server woken at 03:00 needs the restore, so we add it. And the restore is not Caddy's reload, where "two configs are operational at the same time" for a moment: a row that owns a port or a job kind cannot run twice, so a changed row has a short gap between its old and new instance. A remote contribution has no gap: the new endpoint is handshaken before the old contribution is removed.

**What hosted does.**

| Concern | Mechanism |
|---|---|
| Where the desired generation lives | A table per scope (the process tree and each tenant), written only by the composer, a leader job on `kernel-worker` |
| How replicas converge | Each replica follows the doorbell with a one-minute poll (the mechanism the plan already has for overlays), applies the generation through its own queue and reports (replica, scope, generation, digest, state) |
| Canary | The composer applies a process-scope generation to one replica of each role, judges, then releases it to the rest. A tenant-scope generation is judged on the first replica that mounts that tenant |
| A generation that needs code a replica does not have | The generation records the module table it needs. A replica on the older image reports `module-missing` and keeps the last generation it can serve, so enabling a row before the image has rolled out cannot break the N-1 replicas |
| Rollback | Automatic when the judge fails or when a row's breaker opens inside the bake window of a generation that changed it. Manual with `aiopsctl profile rollback --to <generation>` |
| Drift | Replicas whose digests differ past the convergence deadline raise an alert |

Self-host: the files on disk are the source. `aiopsctl profile reload` asks the running kernel to recompose. The `config-watch` plugin, which watches the profile files, is on in `dev` and off in the server profiles, as dsh keeps its reloader on in `web` and off in `headless`, `sdk` and `acp`.

---

## Q10. Tenancy in the context tree

**Choice: a tenant is a child context that carries a scope value, with its own nested row tree. It is not a fully isolated context.**

| Element | Design | dsh origin |
|---|---|---|
| Tenant context | `root.With(tenant)`: a child that inherits every process-scope service. Created by the `tenants` plugin | `ctx.extend`, `ctx.intercept` |
| Tenant rows | The tenant's enabled rows are a nested entry tree mounted under that context: one tree per tenant, like one tree per agent preset | `dsh-agent-preset` |
| Who a service is talking to | A process-scope service called from a tenant fiber reads the tenant from the caller's scope value through its `ProvideFor` view. `store.sql` sets the row-level-security tenant from it; `secrets` scopes by it; nothing takes the tenant from a plugin's word | The context proxy |
| Tenant-specific contributions | Registries are shared, one provider each. A contribution registered from a tenant context is visible only to calls for that tenant; a process-scope contribution is visible to all; the nearest wins | `dsh-scope`: "child scopes inherit ancestor contributions, with the nearest definition taking precedence" |
| `Isolate` | Kept in the kernel and used where a singular service must differ below one context: a pillar group with its own persona, a tenant that brings its own `kek`, a test. No tenant-wide isolation | `ctx.isolate`, `isolate:` on group rows |

**Two ways a plugin serves tenants**, declared in the manifest `scope` and chosen by the bundle.

| Scope | Runs as | Per-tenant enablement means | Use for |
|---|---|---|---|
| `process` | One fiber; the tenant arrives with each call | A routing entry: the dispatcher offers the contribution to that tenant or not | Stateless code: parsers, the email notifier, kernel-data tools |
| `tenant` | One fiber per tenant, in the tenant's subtree | Mount or unmount in that subtree | Anything that holds a tenant's client, credential, upstream process or remote instance: the investigator binding, the on-call adapter, every third-party remote plugin in hosted |

**Per-tenant enablement at runtime.** A tenant overlay may enable or disable rows whose manifest allows tenant scope, set tenant-writable fields, and enable a plugin the operator has installed and marked available to tenants. It cannot install code, insert an arbitrary row, or touch a mandatory row. The overlay is validated with the loader when it is written, becomes a tenant-scope generation, and reloads one subtree.

**Upstream sidecars and remote instances per tenant.** These are durable effects (Q4). The `sidecars` reconciler computes the wanted set from the generations, on the leader, and creates or removes pods; a tenant fiber only binds to the endpoint and waits PENDING until it is ready. A rolling restart of the kernel therefore never deletes a tenant's GoAlert or HolmesGPT. The rules of the current plan stand: HolmesGPT scales to zero, GoAlert is always on with its ceiling of 40 tenants.

**Cost at a few hundred tenants.** Estimates, to be measured in step S4 (Q16) with a test that mounts and unmounts 500 tenant subtrees.

| Item | Estimate | Control |
|---|---|---|
| A fiber | A struct, a disposer list and no goroutine of its own: on the order of a kilobyte | n/a |
| 300 tenants with 12 tenant-scope rows each, all mounted | About 3,600 fibers, single-digit megabytes per replica. The real cost is what each plugin allocates: clients, caches, connections | Lazy mount: a tenant subtree mounts on first use on a replica and unmounts after an idle period, with a concurrency bound and jitter after a restart |
| Process-scope plugins | One fiber whatever the tenant count | Prefer `process` scope for stateless code |
| Remote third-party plugins | One pod per (tenant, plugin) | Scale to zero where the definition tolerates a cold start (notifiers, tools, report renderers); a billing meter per enabled remote plugin; the standing "at most one always-on pod per tenant" table gains a row |
| Boot | No tenant tree is mounted at boot | Readiness does not wait for tenants |

---

## Q11. Plugin runtimes and installation

| Runtime | Status | Isolation | For | Install |
|---|---|---|---|---|
| In-process Go module | From the start | None: full trust | First-party rows; infrastructure providers; anything in a transaction or on the acknowledgement path | A distribution build |
| Out-of-process over gRPC, any language | From the first registry (step S2) | Process, network policy, one tenant per instance in a multi-tenant profile | Third-party and Python plugins; everything third-party in hosted | `aiopsctl plugin install`, no rebuild |
| WebAssembly | Not built. The manifest reserves `kind: wasm` | Sandbox | Pure functions (parsers, templates, policy expressions) if a need appears | n/a. dsh has no such runtime and no journey above needs one; parsing already runs after the acknowledgement, so the remote runtime covers it |

**Remote plugins.** The first-party `remote` plugin runs in process. For each remote row it dials the plugin, handshakes, and registers the remote contributions into the registries on the plugin's behalf; the fiber and its effects live on the kernel side.

| Rule | Detail |
|---|---|
| Who starts the process | The orchestrator, from the deployment unit that install rendered. The kernel never launches a plugin process. `aiopsctl plugin run ./` starts one on a local socket for development |
| Transport | gRPC over a Unix socket on one host, mutual TLS on Kubernetes |
| Handshake | Protocol version, plugin id and version, the sha256 of the manifest (must equal the lock), the definition majors served. A mismatch makes the row FAILED |
| Identity | The row and the tenant of every callback come from the connection (a per-instance socket or client certificate), never from a request field |
| Tenancy | In a multi-tenant profile one instance serves one tenant. The operator may list stateless plugins that are allowed a shared instance |
| What the plugin can call back | Logs, metrics and traces over OTLP; scoped read-only secrets; KV; `llm` through the gateway with a virtual key; kernel-dispatched `tools.Invoke`; `datasources.Query`; `leases.Write` for an executor. No SQL, no jobs, no routes: a remote plugin returns results and the dispatcher writes state |
| Connectivity loss | Health and the breaker, not availability (Q3) |

**Which definitions get a remote adapter.** Registries whose calls are off the acknowledgement path and outside the store transaction.

| Remote-capable, in this order | Never remote |
|---|---|
| `notifiers` (first, step S2), `tools`, `investigators`, `datasources`, `executors`, `signalSources` (the parse half only), `onCallProviders`, `llmProviders`, `auditSinks`, `reportRenderers`, `estateSources`, and the remote-capable hooks of Q8 | `store.*`, `jobs`, `events`, `http`, `secrets`, `kek`, `quota`, `tenants`, `identity`, `authz`, and every sealed policy definition |

The proto of a definition is written with the definition, and the same conformance suite runs against the in-process and the remote target from the first day, so no contract is frozen untested. This reverses row 1 of the decision table, which wrote each proto only when its first out-of-process row arrived.

**The Python SDK** (`aiops-sdk`) is generated from the protos: base classes per remote-capable definition, `serve()`, and `aiops_sdk.testing` with fakes of the callbacks. Its conformance is the Go suite run through the adapter, so there is one suite, not one per language.

**What "install a plugin into a profile" means through supported interfaces, without core source changes.**

| The plugin is | The self-hoster runs | What changes |
|---|---|---|
| Remote | `aiopsctl plugin install acme/notifier-teams@1.2.0 --profile self-host-full` | A lock entry pinned by digest; a deployment unit (a compose fragment under the profile directory, or a values entry the chart renders into a Deployment, a Service and a NetworkPolicy from the manifest capabilities); a disabled row in the profile layer. No rebuild, no restart. Install is not enable |
| In-process Go | Adds two lines to `dist.yaml`, runs `aiopsctl dist build`, rolls the image | A new binary whose module table contains the plugin; the row is inserted by the profile layer |
| A bundle with no code (rows and content) | `aiopsctl plugin install <bundle>` | The bundle is appended to `profile.yaml`, which can change precedence, as dsh notes for its own bundles |

```yaml
# dist.yaml: a distribution is a release plus the in-process plugins you add.
base: aiops@1.4.2
plugins:
  - module: example.com/acme/aiops-store-blob-azure@v0.3.1
    package: ./azureblob
    sum: "h1:..."            # the go.sum line; a mismatch stops the build
output: {image: registry.example/ops/aiops:1.4.2-acme.1}
```

**The distribution builder** (`aiopsctl dist build`, the Collector Builder and xcaddy pattern) generates a `main.go` holding the explicit module table and compiles it. It verifies each added plugin's signature against the trust policy, runs the gate, checks the licence allow-list over the whole module graph, checks the import boundary, and runs each plugin's conformance suite, which it can do because it has the source. It never runs a build script: `go build` with `-mod=readonly`, cgo off unless `--allow-cgo <module>` names the module, no `go generate`. That is our form of dsh's dependency build-script approval. It emits the binary, the image, a bill of materials and the embedded module table, and signs them with the operator's key. A builder image carries the toolchain and the module cache of the release, so an air-gapped site needs only the modules it adds.

**Prebuilt images.** `aiops` holds the first-party bundles. `aiops-contrib` adds the community in-process plugins that pass conformance in our CI, the Collector contrib model. Most self-hosters never run the builder.

**The plugin index.**

| Property | Design | Origin |
|---|---|---|
| Form | One JSON document listing plugins and versions with display metadata | dsh market index format 1 |
| Source | A Git repository with one file per plugin, changed by pull request; CI composes the document | The Krew index |
| Signed | Yes: a detached signature bundle beside the document, checked before the document is parsed | dsh lists "unsigned indexes" as a known limitation; we do not carry it |
| Per version | Version, publish time, `engines.aiops`, definition majors used, runtimes, the OCI reference with its digest (or the Go module with its sum), the signer identity, licence, a capability summary, conformance results | dsh `MarketVersion` |
| What it is trusted for | Discovery only. The index is the index's claim; the signed artifact and its manifest decide what is installed | dsh says the same |
| Configuration | No index is configured by default; the reader fetches only configured URLs, refuses redirects, and a private index or registry never falls through to a public one | dsh `plugin-market` and the registry plan of `plugin-manager` |
| Status shown | Not installed, installed, update available, different version, provided by the distribution; compatibility per version | dsh |

**Artifacts and signing.**

| What | Signed by | With | Verified |
|---|---|---|---|
| Plugin artifact: an OCI artifact holding the manifest and the image digest, or the Go module reference and sum | The author's release workflow | cosign: keyless, checked with `--certificate-identity` and `--certificate-oidc-issuer`, or a key | At install and at `dist build`. The lock records digest and signer; boot and every reload compare digests (module table, or the handshake's manifest sha256) with the lock and need no network |
| Conformance report | The author, as an attestation on the artifact | cosign attestation | Advisory. The gate runs the suite again: at install against the pulled image, at build against the source |
| Index document | The index maintainers | cosign blob signature | On every fetch |
| Distribution binary and image | Us for releases, the operator for a custom distribution | cosign | By the deployment tooling; the sealed table trusts the distribution's own set |

`trust.yaml` lists the accepted signers (an issuer and identity pattern, or a public key), each limited to plugin id patterns and runtimes, plus `allowUnsigned: [ids]`. Hosted has an empty `allowUnsigned` and accepts in-process code from our identity only. Self-host may list ids there, and every boot prints them. On-prem first: key mode works with no outside service, and keyless verification of a stored bundle against a mirrored trust root is to be confirmed in the ADR-020 spike before it is promised for air-gapped sites.

---

## Q12. The plugin manager as a plugin, and the compatibility gate

**The manager is a plugin** (`plugin-manager`, providing `pluginManager`), protected: it cannot disable or remove itself, a mandatory row, or the rows it needs to work (dsh `PROTECTED_PLUGINS`). The gate is not in the manager: it is launcher code that the manager calls, as dsh keeps admission in `dsh-app-boot`.

| Operation | Does | Taken from dsh |
|---|---|---|
| `list` | Rows with plugin, version, runtime, scope, state, health, signer, compatibility; per tenant with `--tenant` | `listBundles`, the plugin inventory |
| `inspect <spec>` | Says what a spec names before anything is fetched or run: id, version, runtimes, signer, compatibility, or a named problem | `inspect` |
| `install` | Verify, gate, conformance, lock, deployment unit, disabled row. A failure restores the lock and the profile files from a snapshot taken before | `installBundle` and its restore |
| `enable`, `disable` | Writes only a departure from the bundle layers into the profile layer, or into a tenant overlay with `--tenant`; one generation | The plugin toggle |
| `update <id> <version>` | Check, apply to a cohort or a tenant, judge, promote or restore the previous version | `updateBundle` with its `checking`, `removing`, `installing`, `restoring` states |
| `rollback`, `quarantine <id>@<version>` | Previous locked version; or deny one exact version everywhere | Ours |
| `remove` | Deselect, unload and drain, remove the unit and the lock entry. Data stays until `purge`, which drops the plugin's namespaces (Q4) | The removal order of `plugin-manager` |
| `allow-version`, `revoke-version` | Exemptions, below | The same commands |

The CLI and the service share one operations library and one write lock on the profile (a file lock on self-host, an advisory lock in hosted), as dsh's CLI and service do. Every operation is audited and emits `plugin-manager/changed`.

**The compatibility gate.** One rule, run at `dist build`, `plugin check`, install, boot, every reload and every remote handshake.

| # | Check | Deny when |
|---|---|---|
| 1 | `engines.aiops` | The range is absent, empty or malformed, or does not contain the running SDK version. dsh treats an absent declaration as "no constraint"; a multi-tenant server does not |
| 2 | Definition majors | The manifest uses a major the distribution does not serve |
| 3 | Remote protocol | No common protocol version at handshake, or the manifest digest differs from the lock |
| 4 | Stability | Below the profile's `minStability` |
| 5 | Trust | The signature is missing or its signer is outside `trust.yaml` for this id and runtime |
| 6 | Licence | An in-process module outside the allow-list (remote plugins are separate programs and exempt) |
| 7 | Sealed definitions and hooks | A provider of a sealed definition, or a listener on a signed or sealed hook, from outside the allowed signer set |
| 8 | Scope | A tenant overlay enabling a plugin not marked available to tenants; a shared remote instance in a multi-tenant profile without the operator's allowance |

A denied row becomes a detached, disabled row in the launcher's own copy of the composition. No profile file is rewritten, the dump still shows the configured row, and the refusal names the plugin, the versions and the unmet check. These are dsh's rules unchanged.

**Exemptions**, as in dsh: `compatibility.yaml` maps an exact `id@version` to a list of exact runtime versions, is written only by `aiopsctl plugin allow-version ... --accept-risk`, and is inherited by neither a plugin upgrade nor a platform upgrade. An exemption covers checks 1, 2 and 4 only: never trust, licence or sealing. A malformed file never stops boot: accepted records apply, rejected ones are reported, and the file becomes read-only to the tool. Hosted sets exemptions to deny.

**Health.** Each fiber has a state (Q3) and each contribution a health value fed by its guard: `ok`, `degraded` (breaker half-open, errors above a threshold) or `failed` (breaker open). A provider of a singular service may add its own probe with `ctx.Probe`. `list` shows all of it, per tenant.

---

## Q13. Failure policy

**Two lists.**

| List | Meaning | Who owns it |
|---|---|---|
| Mandatory | Must be composed, enabled and ACTIVE in every server profile; nothing can remove or disable it | The launcher: `loader`, `store.sql`, `jobs`, `events`, `http`, `secrets`, `tenants`, `identity`, `authz`, and the sealed policy set `audit`, `pii`, `approval`, `ledger`, `leases` |
| Required | If composed and enabled, it must be ACTIVE (dsh's meaning: an absent or disabled required row is ignored) | The mode bundle, with `required: true`. Hosted adds `auth-endpoints`, `fleet-agentapi`, `signals` with `signal-alert-webhook`, `gateway-render`, `sidecars-kubernetes`, `kek-kms`. A role's required rows are the ones whose row-level `inject` names that role |

Everything else degrades to one grouped warning, and the process starts with every row that can activate.

**The matrix.** It is one file, `test/failure-matrix/matrix.yaml`, which is both the table below and the input of the test that boots the built artifact through every cell. For remote rows the test uses a stub plugin image.

| Failure | Optional row at boot | Required or mandatory row at boot | Live reload | Tenant subtree |
|---|---|---|---|---|
| `profile.yaml`, a named first-party bundle or a `--patch` file missing; a layer file malformed or empty | Stop | Stop | Reject; the running generation stays | Overlay refused at the API |
| An installed third-party bundle is unreadable | Skip the bundle, report it | Stop if it carries a required row | Reject | n/a |
| A mandatory row is absent or disabled | n/a | Stop | Reject | An overlay cannot do this |
| Plugin id not in the module table and not a remote row | Warn; row inactive | Stop | Reject; on a replica with an older image, `module-missing` and keep the last generation | Overlay refused |
| Signature invalid or signer not trusted; compatibility denied | Detached as disabled; reported | Stop | Reject | Overlay refused |
| Config fails the schema; a reference does not resolve | Warn; row FAILED | Stop | A new row stays inactive; an existing row keeps its instance and config | The same, for that tenant |
| `Apply` errors, panics or passes its deadline | FAILED; bounded retry; siblings continue | Stop | Restore the previous generation | FAILED for that tenant; siblings and other tenants continue |
| An injected service has no active provider | PENDING; warn; activates when a provider appears | Stop | A required row left PENDING restores the generation | PENDING |
| A provider is swapped | Consumers restart once | Consumers restart once | One wave; restart-class providers are refused | One wave inside the subtree |
| HTTP bind fails | The row runs without the endpoint | Stop | The process keeps running without it | n/a |
| Remote plugin unreachable at mount | PENDING until its endpoint is ready | Stop | Restore | PENDING for that tenant |
| Remote handshake mismatch | FAILED | Stop | Restore | FAILED for that tenant |
| Remote plugin slow or failing while running | Breaker opens; calls fail fast; the dispatcher falls back; row stays ACTIVE, health `failed` | Readiness fails until it recovers | Auto-rollback inside the bake window | One tenant |
| A hook listener errors or times out | `gate`: the call is vetoed. `observe`: skipped and logged | The same | The same | The same |
| A tracked goroutine panics | The fiber unloads, FAILED | Fatal: exit non-zero | Fatal for a required row | FAILED for that tenant |
| An untracked goroutine panics | Fatal: Go ends the process | Fatal | Fatal | Fatal: every tenant on that replica; other replicas serve |
| A drain passes its deadline | Contexts cancelled; reported; unload continues | The same | The same | The same |
| A disposer errors or hangs | Logged; the fiber is marked leaked and the replica is flagged for restart at the next rollout | The same | The same | The same |
| Health turns `failed` while running | Consumers get errors; nothing unloads | Readiness fails until it recovers; liveness is not tied to rows | n/a | n/a |
| Row absent or disabled | Ignored | Ignored for required, stop for mandatory | Not activated | Ignored |

**Blast radius, in one place.**

| Where the fault is | Who is affected | What contains it |
|---|---|---|
| A remote plugin for one tenant | That tenant, that plugin | Process and network boundary, deadline, breaker, fallback |
| A tenant-scope in-process fiber | That tenant | Panic recovery at the dispatch boundary, tracked goroutines, subtree reload |
| A process-scope in-process row | Every tenant on that replica | Canary replica per role, cohorts, the N and N-1 rollout, image rollback |
| An infrastructure provider | The process | Mandatory list, restart class, conformance |
| A bad generation | Its scope | Judge and restore, last-known-good |

---

## Q14. Testing in isolation, in tiers

The current plan tests a plugin against `plugintest`, a fake host. This proposal removes the fake host: because the kernel ships in the SDK, a plugin is mounted on the real runtime, and only the services around it are fakes. That is the Cordis property ("mount the plugin on a bare `Context`") without the risk that an imitation of the host drifts from the host.

| Tier | What runs | What is fake | Who runs it |
|---|---|---|---|
| 1. Bare context mount | The real kernel, the plugin, and the fakes of the services it injects | Every service around the plugin | Every author, with `go test` or `pytest` |
| 2. Conformance per definition | The definition's suite against a provider or a contribution, in every runtime it ships in | Nothing inside the contract; fixtures at the outside boundary | Every author, through `plugin check`; kernel CI for dispatchers |
| 3. Real composition | The built artifact booted on a test-only profile, driven by a fixture | Only the external or non-deterministic boundary: the model (replay), outbound HTTP (fixtures), the clock | Every author, through `plugin smoke`; CI for every shipped profile |
| 4. Failure matrix | The built artifact through every cell of Q13 | A stub remote plugin | Kernel CI |
| 5. Record and replay | Recorded sessions with `AIOPS_SNAPSHOT=replay` | The recorded boundary | CI |
| 6. Lab | `self-host-full` unchanged, as in the current plan | Nothing | Maintainers |

```go
func TestSendsOnce(t *testing.T) {
	root := kerneltest.New(t) // a real root context; cleanup disposes it and runs the residue check
	sec := secretsfake.Mount(root, secretsfake.Secret("webhook-url", "https://example.invalid/hook"))
	reg := notifiersfake.Mount(root) // applies the dispatcher contract and records calls
	kerneltest.Mount(t, root, teams.Plugin, `{"channel":"ops"}`) // fails on FAILED or PENDING

	n := &notifiers.Notification{IdempotencyKey: "k1", Text: "disk full"}
	if _, err := reg.Send(t.Context(), "teams", n); err != nil {
		t.Fatal(err)
	}
	if got := len(sec.Calls()); got != 1 {
		t.Fatalf("secret reads = %d, want 1", got)
	}
}
```

**Rules that keep the tiers honest.**

| Rule | Why |
|---|---|
| A fake must pass its own definition's conformance suite, in CI | A fake that drifts makes every tier 1 test lie |
| A provider must pass the suite in every runtime its manifest lists | In-process and remote are one contract |
| A registry suite has two halves: what a contribution owes (run by `plugin check`) and what the dispatcher guarantees (run in kernel CI against the real dispatcher and against the fake) | The guard is where the invariants of Q7 live |
| Every registry suite includes the dispose test, and every plugin the residue and mount-twice tests (Q4) | dsh's mandatory test |
| A product-visible plugin needs a tier 3 test; a hand-built mount is not enough | dsh: "Hand-built `ctx.plugin(...)` suites are insufficient" |
| Tier 3 asserts on what an operator can observe: an API answer, a stored row, a message at a sink. Never on a log line | dsh: assertions must re-read the world |
| Outbound HTTP replays fixtures in strict mode; an unmatched request fails | Unchanged from the current plan |

**The kernel's own tests.** A parity suite restates the Cordis behaviours read from its source as table tests: restart on a replaced provider, a provider outliving its dependents, FAILED recovering only through an update, isolation by name and label, a waterfall veto, `next` called twice, an effect on a disposed fiber. A randomized test applies random mount, dispose, swap and update sequences under the race detector and asserts no residue and no deadlock.

**What a contributor runs before opening a pull request.** One command, then one more.

| # | `aiopsctl plugin check ./` | Fails when |
|---|---|---|
| 1 | Manifest | Schema, id rules, a protected mark in a name, a missing or non-SPDX licence, a missing `engines.aiops`, or a manifest that differs from the code's `Inject`, `Provides`, registrations and hooks |
| 2 | Config schema | Not draft 2020-12, or an example under `testdata/config/` does not validate |
| 3 | Import boundary | An import outside the SDK, the standard library and the plugin's own dependencies |
| 4 | Build or serve | The build fails; for a remote runtime, the process does not handshake on a temporary socket |
| 5 | Tier 1 | The plugin does not reach ACTIVE on fakes; the residue test or the mount-twice test fails |
| 6 | Tier 2 | The suite of any definition it provides or contributes to fails in any listed runtime |
| 7 | Effects | A tool or an executor performs a write it did not declare, in the recording sandbox |
| 8 | Pack and verify | The artifact does not pack, or does not verify with a throwaway key |

Then `aiopsctl plugin smoke ./ --against <released image>` for tier 3. A third party needs the SDK module (or `aiops-sdk`), the `aiopsctl` binary and a container runtime: no kernel checkout. Inside the kernel repository `make check` runs the same for the changed plugins, boots `dev`, and runs the failure matrix when `sdk/kernel`, `boot/` or `loader/` changed.

---

## Q15. Apps are profiles

One binary, `aiops`, is the only launcher. An app is a profile: a bundle list and a startup row that parses the remaining arguments and provides a startup service, which other rows inject.

| App | Profile | How it differs |
|---|---|---|
| Hosted edge, api, worker | `hosted`, started with `--role edge`, `--role api` or `--role worker` | The `role` startup row parses `--role` and provides the marker services `role.edge`, `role.api`, `role.worker`. A row that serves one role carries a row-level `inject` on its marker, so it stays PENDING elsewhere. There is no second filtering mechanism: activation does it |
| Self-host | `self-host-full`, `aiops-only`, `dev` | The same rows; `--role all` provides all three markers |
| CLI | `ctl` (`aiopsctl` is `aiops` under another name with this profile) | Commands are contributions to a `commands` registry. `slack manifest` comes from the Slack bundle, `estate export` from `estate`, `llm probe` from `llm-gateway`, `plugin` and `dist` from the manager. `profile render` is the launcher itself, so a dump is exactly what the server would boot |
| Tests | `profile.test.yaml` beside a plugin | The same launcher, a test-only bundle list |

The security boundary of row 1 ("the internet-facing `api` role holds no cross-tenant credential") becomes a property of a dump: `aiopsctl profile render --profile hosted --role api --dump` shows that `auth-keys` and the platform-role rows are not active there, and CI keeps that dump as a golden file.

**The agent side, honestly.** The agent is unmodified Alloy, which has its own component model, and we do not run a context tree on customer hosts. "Everything is a plugin" on the agent side means this: each feature key is a `fleet-fragment-<feature>` row on the server that contributes one template to the `fleetFragments` registry, and the agent bundles (`agent-base`, `agent-security`, `agent-windows`) carry those rows and the installer content. The fragment suite asserts that every feature, tier and pinned agent version composes to a config the agent's own validation accepts and carries the identity stamps. A third party adds a feature key the same way as any in-process plugin. The installer stays a script.

---

## Q16. Order of work, with no date pressure

The order proves the model first, then the ecosystem path, and only then builds product. Each step has an exit test, and no step has a date.

| Step | Builds | Proves | Exit test |
|---|---|---|---|
| S0. Kernel | `sdk/kernel`, `sdk/kerneltest`, the parity suite | The Cordis model in Go | The parity suite and the randomized lifecycle test pass under the race detector with no residue |
| S1. Launcher and loader | `boot/`, `loader`, dumps, references, the mandatory and required policy, the matrix harness, three toy plugins (a provider, a consumer, a registry with a contribution) | Composition, activation and the failure policy on a built artifact | The failure matrix passes on the built binary; golden dumps |
| S2. The ecosystem path, on one registry | The `notifiers` definition with fake and suite; the `notify` dispatcher and its guard; one notifier in three forms (in-process Go, remote Go, remote Python); `sdk/remote`, the `remote` plugin, `aiops-sdk`; `plugin new`, `check`, `smoke`, `pack`, `install`; signature verification, `trust.yaml`, `plugins.lock` | Journeys A and B, before any product feature exists | A repository with no kernel checkout publishes a Go notifier and a Python notifier; a clean machine installs both into `dev` from the signed artifacts and sends through each |
| S3. Infrastructure definitions | `store.sql`, `store.kv`, `store.blob`, `jobs`, `events`, `quota`, `http`, `secrets`, `kek`, telemetry, each with fake and suite; the transaction contract; durable effects and the uninstall residue test; `dist build`; a second `store.blob` provider | The store-provider journey; "leaves nothing behind" for durable state | A third-party `store.blob` provider passes the suite and runs in a distribution built without core source changes |
| S4. Reload and tenancy | Generations, the reload queue with restore, `tenants`, tenant subtrees, lazy mount, scoped contributions, overlays, the composer and replica convergence, `sidecars` | Journey C steps 1 and 2 | A remote notifier is enabled for one of 500 synthetic tenants on three replicas with no restart; the mount cost is measured |
| S5. The policy chain | `identity`, `authz`, `audit`, `pii`, `approval`, `ledger`, `leases`, `actions` with a no-op executor; the sealed table | Q7 | The fail-closed matrix: remove or replace each sealed provider and assert that no write path exists |
| S6. Manager and index | `plugin-manager`, `plugin-index`, the index repository and its signing, cohort updates, quarantine, exemptions, `aiops-contrib` | Journey C steps 3 to 6 | The 03:00 drill as a test: a hanging remote version is contained by the breaker, rolled back automatically, and exactly one tenant is affected |
| S7 onward. Product, in the order of today's P0 and P1 | Edge and fleet; ingest, correlation and casefile; investigation and tools; change; chat and on-call; pillars | Every feature arrives as rows in a bundle | The section 7.1 demo of the current plan, unchanged |

Three standing rules from S2 on. No definition merges without its fake, its suite, a provider and a consumer. No plugin merges without `plugin check` and a tier 3 test. Nothing merges into `sdk/kernel`, `boot/` or `loader/` that names a domain concept; by S7 there is no kernel code a feature could be put into, which is what keeps every later feature a plugin.

What S0 to S6 cost against the current plan: they replace today's P0 loader, lifecycle, `plugintest` and failure matrix, and they bring forward from P3 the remote protocol, the gate, the manager, the builder and signing. The product work of P1 starts after S6 instead of beside P0.

---

## Risks and open points

| # | Risk | Mitigation |
|---|---|---|
| 1 | The Cordis semantics are subtle under real concurrency: a re-entrant unload during a load, a superseded load, a provider released while a dependent drains. `fiber.ts` spends much of its length on exactly these cases | The parity suite and the randomized lifecycle test under the race detector are the exit of S0; the kernel has a size budget of about 3,000 lines and imports nothing |
| 2 | About forty definitions written before their consumers freeze wrong contracts | A definition is `v0` until one release has shipped with it, and it lands with its first consumer (most of them in S7, not S3), together with its fake and suite |
| 3 | Live reload with restore across replicas is machinery dsh does not have | Generations are data, a canary replica judges first, and a restart remains a valid way to apply any generation |
| 4 | Two lifecycles to reason about: runtime effects and durable effects | One rule decides which is which (anything shared between replicas is durable), and the uninstall residue test checks it |
| 5 | A widely injected provider restarts most of the tree when it is replaced | Restart class for infrastructure providers; one wave per generation |
| 6 | In-process third-party code is unconfined | Hosted refuses it; self-host links it only through a signed distribution build that records what was linked |
| 7 | One pod per tenant and remote plugin | Scale to zero, the operator's shared-instance allowance, a billing meter |
| 8 | Keyless signature verification at an air-gapped site is not confirmed | Key mode works without any outside service; the ADR-020 spike settles the rest before it is promised |
| 9 | Product work starts after S6 | Accepted by the owner's direction; S2 is an early milestone that an outside author can try |

Not verified in this pass: the dsh test-support packages (not installed; taken from the research note), the cost figures of Q10 (estimates), and the Go prior art beyond the six pages listed at the top.

---

## dsh property checklist

Status: **as is**, **adapted**, or **not carried**. Sources are files of the installed package, or `dsh-architecture.md` (DA) where the package does not ship them.

### Cordis core

| # | Property | Source | Status | How, or why not |
|---|---|---|---|---|
| 1 | Context tree; child contexts inherit | `context.ts` `extend` | as is | `ctx.Plugin`, `ctx.With` |
| 2 | Services by name, resolved in the caller's scope | `reflect.ts` | adapted | Typed keys replace property access |
| 3 | `provide`: one provider per name per scope, owned by a fiber, removed with it | `reflect.ts` `provide` | as is | `kernel.Provide` |
| 4 | `inject`: a plugin loads only while its services exist | `registry.ts`, `fiber.ts` | as is | `Plugin.Inject` |
| 5 | Epoch: a replaced provider restarts its consumers | `fiber.ts` `_refresh` | as is | Q3 |
| 6 | A provider keeps its implementation until dependents finish unloading | `fiber.ts` `_unload` | as is | Q3 |
| 7 | Optional read without activation coupling | `ctx.get` | as is | `kernel.Get` |
| 8 | `ctx.inject(deps, callback)` as a nested fiber | `registry.ts` | as is | `ctx.When` |
| 9 | `check`: present but unavailable | `reflect.ts`, `service.ts` | adapted | Configuration-time conditions only, rate limited; health is not availability |
| 10 | Fiber states PENDING, LOADING, ACTIVE, FAILED, UNLOADING, DISPOSED | `fiber.ts` | adapted | UNLOADING gains a drain phase |
| 11 | A failed fiber recovers only through an update | `fiber.ts` `_setEpoch`, README | adapted | Kept in the kernel; the Loader adds a bounded retry that ends in quarantine |
| 12 | Effects with disposers in reverse order; generator and async effects | `fiber.ts` `effect` | adapted | Strictly sequential reverse order; one disposer per call |
| 13 | An effect on a disposed fiber is an error | `INACTIVE_EFFECT` | as is | |
| 14 | `fiber.update`, the update waterfall, volatile config | `fiber.ts`, README, Loader README | as is | `x-aiops-volatile`; tenant-writable fields are volatile |
| 15 | `ctx.isolate(name, label)` | `context.ts`, Loader `isolate.ts` | as is | In the kernel; used for groups, a tenant-owned singular service and tests |
| 16 | `ctx.intercept(name, config)` | `context.ts`, `service.ts` | adapted | Typed scope values read through `ProvideFor` |
| 17 | The context is a Proxy; services appear as `ctx.<name>` by declaration merging | `reflect.ts` | not carried | Go has neither; typed keys and explicit `Use` |
| 18 | `ctx.set`, `accessor`, `mixin`, callable services | `reflect.ts`, `service.ts` | not carried | Consumers hold values captured at `Apply`; changing a value is a new provision |
| 19 | Three plugin shapes; the `Service` base class | `registry.ts`, `service.ts` | not carried | One shape: a struct with `Apply` |
| 20 | Dispatch modes `emit`, `parallel`, `serial`, `bail`, `waterfall` | `events.ts` | adapted | Waterfall becomes typed hooks with stages, modes and access classes; lifecycle notifications stay; durable events are the `events` service |
| 21 | Lifecycle events `internal/plugin`, `status`, `service` | `events.ts` | as is | For the Loader, the manager and tests |
| 22 | `internal/get`, `set`, `listener`, `dispatch` | `events.ts` | not carried | They serve the proxy |
| 23 | Synchronous schema validation before load | `fiber.ts` `resolveConfig` | adapted | JSON Schema 2020-12 |
| 24 | Disposing the root fiber restarts the tree | `fiber.ts` | adapted | Root dispose is shutdown; a restart is the orchestrator's |

### Loader, profiles, reload

| # | Property | Source | Status | How, or why not |
|---|---|---|---|---|
| 25 | The Loader is a plugin with an entry tree (create, update, remove, resolve, await, locate) | Loader README | as is | `loader` |
| 26 | `group` and `include` builtins | `dsh-app-boot` | as is | Pillars are group rows |
| 27 | Entry fields `id`, `name`, `config`, `disabled`, `inject`, `intercept`, `isolate`, `group` | Loader README | adapted | `plugin` for `name`, `enabled` for `disabled`, plus `required` |
| 28 | Row order carries no load meaning | DA 1.1 | as is | |
| 29 | `!!js` expressions in config and `disabled` | DA 2.4 | not carried | Typed references and predicates; configuration never executes code |
| 30 | Ordered bundles and patch layers over an empty root; whole-config replace; `insert`; unknown id warns; empty file stops boot | `dsh-app-boot` | as is | Already in the plan |
| 31 | Only direct bundles contribute | DA 3.2 | as is | |
| 32 | Home layer, `--patch` overlays, the telemetry switch | `dsh-app-boot` | as is | Site layer, overlays, kill switches |
| 33 | Dump of the composed tree with provenance; schema dump | `dsh-app-boot` | as is | |
| 34 | A bundle is a data-only patch plus code dependencies; base plus mode bundle | DA 3.1 | as is | First-party bundles are embedded |
| 35 | Profile templates; create from a template through a staging directory; `sanitizeProfile` | `dsh-app-boot`, `dsh-official-bundles` | as is | `aiopsctl profile init --from`, `aiopsctl profile sanitize` |
| 36 | A `prepare` step provides launch facts before any row mounts | DA 7 | as is | Launcher |
| 37 | Ready when the root is active and the Loader exists; bounded shutdown | DA 7 | adapted | Also waits for mandatory rows; the bound is the drain deadline |
| 38 | One immutable runtime resolution table, with successors | `dsh-app-boot` | adapted | The module table of the distribution and the lock for remote plugins; a successor is a generation |
| 39 | Module hot replacement | `dsh-hmr` | not carried | Go cannot replace code in a process; remote plugins are replaced live |
| 40 | One serialized queue for configuration reloads; package operations outside it | `dsh-hmr` `runExclusive` | as is | Q9 |
| 41 | A live update is not rolled back | `dsh-app-boot` | not carried | We restore the previous generation |
| 42 | One process | everywhere | not carried | Generations converge across replicas; anything shared between replicas is a durable effect reconciled from data |
| 43 | Reloader on in `web`, off in `headless`, `sdk`, `acp` | `dsh-hmr` | as is | `config-watch` on in `dev` only |

### Installation, compatibility, manager

| # | Property | Source | Status | How, or why not |
|---|---|---|---|---|
| 44 | Install at runtime with the package manager, into the profile | `dsh-plugin-manager` | adapted | Remote plugins install at runtime; in-process Go needs a distribution build |
| 45 | An installed package activates nothing until selected | DA 4.2 | as is | Install is not enable |
| 46 | `engines.dsh`, else peer ranges; absent means no constraint | `dsh-app-boot` | adapted | `engines.aiops` plus definition majors; absent means denied |
| 47 | Admission only at owned boundaries; a denied row is a detached disabled row; no file rewritten | `dsh-app-boot` | as is | |
| 48 | Exact-version exemptions, not inherited, with an explicit risk acceptance | `dsh-app-boot`, `dsh-plugin-manager` | as is | Never covers trust or sealing; denied in hosted |
| 49 | First-party packages released in lockstep | DA 4.1 | as is | Compiled together; third parties declare ranges |
| 50 | The plugin manager is a plugin, cannot remove itself, protects what it needs | `dsh-plugin-manager`, `dsh-official-bundles` | as is | |
| 51 | Inspect before install; snapshot and restore on failure; toggles record only departures; update with restore; ordered removal | `dsh-plugin-manager` | as is | Q12 |
| 52 | A shared write lock for the CLI and the service | `dsh-plugin-manager` | as is | File lock, or advisory lock in hosted |
| 53 | Ordered registries; a private one never falls through to a public one | `dsh-plugin-manager` | as is | For OCI registries and indexes |
| 54 | Approval of dependency build scripts | `dsh-plugin-manager` | adapted | The builder runs no scripts; cgo needs an explicit allowance |
| 55 | A plugin index: one document, configured URLs only, redirects refused, status and compatibility per version | `dsh-plugin-market`, `dsh-plugin-market-index` | as is | |
| 56 | The index is unsigned | `dsh-plugin-market` limitations | not carried | Signed, and checked before parsing |
| 57 | In-process plugins run with full trust; the checks are not a sandbox | `dsh-app-boot`, `dsh-plugin-manager` | adapted | True for in-process code; hosted admits third-party code only out of process |
| 58 | Localized display metadata and an icon | `dsh-package-manifest`, DA 2.3 | adapted | In the manifest; English required |
| 59 | Platform declaration on a bundle; skipped bundles are listed | `dsh-app-boot` | adapted | `supports: {os, arch}`; a skipped bundle that carries a required row stops boot |

### Failure policy, testing, shape

| # | Property | Source | Status | How, or why not |
|---|---|---|---|---|
| 60 | A global required list; everything else is best effort with one grouped diagnostic | `dsh-app-boot` | adapted | Plus a mandatory list that nothing can disable |
| 61 | The failure matrix as a test of the built artifact | `dsh-app-boot`, DA 5.4 | as is | One file is the table and the test input |
| 62 | A detached failure is fatal | `installFailLoud` | adapted | Tracked goroutines; fatal for required rows; an untracked panic is always fatal |
| 63 | Unit isolation by mounting on a bare context | Cordis README | as is | The real kernel ships in the SDK |
| 64 | Every registry has a dispose test | DA 5.1 | as is | Part of each registry's suite |
| 65 | Test-support packages: prerequisite services, model replay, loader smoke, snapshots | DA 5.2 | as is | Fakes beside definitions, `llm-replay`, `loader-smoke`, `AIOPS_SNAPSHOT=replay` |
| 66 | Product-visible plugins need a real-composition test; mock only the expensive or non-deterministic boundary | DA 5.3 | as is | Tier 3 |
| 67 | Per-file full coverage gate | DA 5.1 | adapted | On `sdk/kernel`, `boot/` and `loader/`; plugins are judged by conformance |
| 68 | Definition, provider, consumer; plugins depend on definitions, never on a provider | DA 6, 2.3 | as is | Replaces principle 3 |
| 69 | Singular keys for engines, plural keys for registries | DA 1.3 | as is | |
| 70 | Cross-cutting concerns are plugins: telemetry, approval, storage, jobs, credentials, model access, plugin management | DA 6, the dump | as is | Q6 |
| 71 | Approval is a plugin that fails closed when absent | `dsh-user-approval` | as is | Q7: a gate is a dependency |
| 72 | Scoped contribution sets with inherited lookup | `dsh-scope` | as is | Tenant-scoped contributions |
| 73 | Agent presets as nested row trees | `dsh-agent-preset`, the dump | adapted | Pillars as group rows; tenants as nested trees |
| 74 | Apps are profiles; one launcher; a startup row parses the remaining arguments | DA 7 | as is | Roles and the CLI |
| 75 | A browser-side plugin tree and generated RPC | DA 7 | not carried | The UI is the dashboard app and chat |
| 76 | Out-of-process plugins | None as a plugin runtime. The nearest things are `dsh-mcp-client` (outside MCP servers contribute tools) and `dsh-sdk-jsonrpc-server` (outside clients drive agents) | ours | The remote runtime, required by tenancy and by Python |

---

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
