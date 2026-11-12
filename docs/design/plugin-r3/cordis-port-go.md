# Plugin system, round 3: a faithful port of the Cordis model to Go

Status: proposal, 2026-11-10. One of four independent proposals for the round-3 plugin design. Lens: carry the Cordis semantics (context tree, fibers with epochs, disposable effects, waterfall hooks, isolate) into idiomatic, race-safe Go, and say exactly what changes because Go cannot load code at run time. Nothing here is decided: `00-decisions.md` stays the canonical record until an ADR edits it.

A bare `§n` is `docs/architecture/00-decisions.md`; `03`, `07` and `08` are the files of `docs/architecture/`. "The note" is `docs/research/dsh-architecture.md`. "Round 1" is `docs/research/plugin-architecture.md`. dsh is DeepSeek Harness.

**What was read.**

- The published package `@deepseek-ai/cordis` 4.1.0: `src/context.ts`, `reflect.ts`, `registry.ts`, `fiber.ts`, `events.ts`, `service.ts`, `utils.ts` (2,811 lines with the logger), and its README. Public source: `https://github.com/deepseek-ai/deepseek-harness`; upstream Cordis: `https://github.com/cordiverse/cordis`. The accepted port attribution and both MIT notices are specified in `docs/design/licence-audit-2026-11.md` §2.1.
- `cordis-plugin-loader` 1.1.0 (`index.ts`, `config/entry.ts`, `group.ts`, `isolate.ts`, `tree.ts`, `utils.ts`), `cordis-plugin-include` 1.1.0 (README, `patch.ts`), `cordis-plugin-group`, `cordis-plugin-timer`.
- The READMEs of `dsh-app-boot`, `dsh-hmr`, `dsh-plugin-manager`, `dsh-user-approval`, `dsh-tools`, `dsh-scope` and `dsh-agent-preset` at dsh 0.2.2-rc.1, and `dsh-base/cordis.patch.yml`. Machine-specific composition output is not evidence for this public baseline.
- Go prior art, fetched 2026-11-10: Juju `worker/dependency` (`doc.go`, `interface.go`, `engine.go`; LGPL-3.0 with a linking exception, so it is read as prior art only, never imported, and no code is taken from it), Grafana dskit `modules` and `services` (Apache-2.0), suture v4 (MIT). Caddy, the OTel Collector Builder, Alloy, go-plugin, Dapr, Backstage and Extism are covered by round 1 and are cited from there.

**Version drift.** The note read dsh 0.2.1-rc.2 with Cordis 4.0.5 on 2026-10-27. The source inspection of 2026-11-10 used dsh 0.2.2-rc.1 with Cordis 4.1.0. Where the two differ this document follows that pinned package source. Two differences matter: the note records the epoch as built from the uid of the providing fiber, while 4.1.0 builds it from the uid of the provision (`Impl.uid`) and keeps a provision alive until every dependant has finished unloading (`committed`, `dependents`, `release`); and the required-entry list of `dsh-app-boot` gained `tool-executor-native` and `tool-executor-ptc`. If the note is accurate, that is two weeks of drift in the dependency-tracking core, which bears on Q1.

**Evidence boundary (ADR-056).** This public baseline records the source inspection of 2026-11-10, not a retained implementation result or S0 measurement. The Go sketches and proposed traces below require repository-backed validation at S0. Initial differential scenarios cover waiting for a service, starting when it appears, stopping when it vanishes, restarting on a swap, duplicate providers, isolate, sticky failure, late effects and veto. Compilation, `go vet` on Go 1.26, race checks, stress counts and performance results must be recorded with reproducible artifacts; none is claimed here.

---

## 0. Summary

### 0.1 Position

- **Kernel.** A Go port of the Cordis core: a context tree, typed service keys, fibers with epochs, disposable effects and typed hooks. About 2,500 lines, standard library only. It knows the names of four built-in services and nothing about the product.
- **Everything else is a plugin**: the loader, the store, jobs, the outbox, the HTTP server, tenancy, audit, approval, the change ledger, the LLM gateway, fleet, the plugin manager. Each one that another plugin calls has a service definition with a fake and a conformance suite beside it.
- **Activation is availability-driven.** A row starts when the services it injects have an ACTIVE provider, stops when one vanishes and restarts when a provider is swapped. Row order means nothing. The tree is recomposed live through one serialized reload lane.
- **Policy stays fail-closed without living in the kernel.** Sealed keys, required injects, monotonic guards run by the service that owns each registry, and write credentials that exist only against a ledger token. dsh does the same thing: its tools registry runs monotonic guards after the extensible hook, and its approval service fails closed.
- **Three runtimes behind the same definitions**: in-process Go (a catalog compiled by a distribution builder), an out-of-process fiber over gRPC in any language, and WebAssembly for pure functions. The last two install into a running profile.
- **Not carried, because Go cannot**: loading code into the process at run time, module hot replacement, proxy contexts, run-to-completion. **Not carried, because a multi-tenant server should not**: JavaScript expressions in rows, hook order by registration, permission-granting waterfall answerers, an agent tool that changes the composition, a failed plugin that stays failed with nobody retrying it, and context isolation presented as tenant isolation.

### 0.2 How a plugin lives

This is the expected trace derived from the pinned Cordis source, to be checked against the Go port at S0; it is not a recorded Go result. `consumer` injects `db`; `provA` and `provB` provide it.

| Step | What the runtime does | Status trace |
|---|---|---|
| Mount `consumer` | No provider of `db`: the fiber waits | (stays `PENDING`) |
| Mount `provA` | `provA` loads; when its `Apply` returns, `db` becomes visible; `consumer` gets an epoch and loads | `provA: PENDING > LOADING > ACTIVE`, then `consumer: PENDING > LOADING > ACTIVE` |
| Dispose `provA` | `provA` leaves ACTIVE; `consumer` loses its epoch and unloads first, its disposers run, and only then do `provA`'s | `provA: ACTIVE > UNLOADING`, `consumer: ACTIVE > UNLOADING > PENDING`, `provA: UNLOADING > DISPOSED` |
| Mount `provB` | A different provision, so a different epoch: `consumer` loads again with the new value | `provB: ... > ACTIVE`, `consumer: PENDING > LOADING > ACTIVE` |
| Mount a second provider of `db` | Refused: one provider per name per realm | `provC: LOADING > UNLOADING > FAILED` |
| `flaky` throws in `Apply`, then its provider is swapped | It stays FAILED: a dependency change does not retry a failure. `Update` does | `flaky: LOADING > UNLOADING > FAILED`, later `FAILED > LOADING > ACTIVE` |

No plugin author wrote any of this ordering. The consumer declared `inject: [db]` and registered its work as effects.

### 0.3 What changes against the current plan

| Current plan (round 1, adopted to protect dates) | This proposal |
|---|---|
| Principle 3, "extension points are earned": 12 points have an interface, about 22 kernel rows have one implementation and no definition, several capabilities are "packages, not points" (§4) | A definition exists for every service that another plugin calls, 56 in all (Q5). The test for a definition is dsh's: definition, provider and consumer must all exist. The fake is the second implementation from the first day |
| Plugins compiled in through an `init()` map; third-party plugins, the plugin manager, the compatibility gate and the builder wait for P3 (§2.1 row 1) | The catalog is generated by the distribution builder from the first step. Remote and Wasm plugins, the plugin manager, the gate, OCI install and signing come before the first product feature (Q11, Q12, Q16) |
| Rows start in a fixed dependency order; layers 0 to 4 change on restart; `ctx.isolate` not ported (§6.1, 03 §8.1, 08 §1.1) | Fibers with epochs; no order; live recomposition of layers 1 to 4 through one lane; `isolate` and `intercept` ported and used for tenant trees and tenant overlays (Q3, Q9, Q10) |
| Principle 5, "policy that must not be bypassed is a host invariant" enforced by kernel point guards | The same contracts, enforced by the plugin that owns each registry and by sealed keys (Q7). The kernel holds no policy |

---

## Q1. Kernel runtime and language

**Choice: a Go port of the Cordis model. Not TypeScript on Cordis, and not the current fixed-order Go registry.**

| Option | For | Against | Verdict |
|---|---|---|---|
| TypeScript on Cordis itself (the literal dsh kernel) | Zero translation loss; the loader, include, group and HMR packages exist; in-process install with pnpm works | Cordis went from 4.0.5 to 4.1.0 in the two weeks between this project's two readings, the note's description of the epoch no longer matches the source, and dsh calls itself a developer preview (the note, header). Cordis is correct because JavaScript runs one callback to completion: every tenant's parse, scrub, JWT signature and correlation would share one event loop, which is the shared fate principle 6 forbids. A context is a JavaScript object and cannot be shared with a Worker; `dsh-app-boot` has to re-install even its module resolution in each of its own Workers and gives third-party Workers nothing. PostgreSQL with River (§2.1 row 9), connect-go, slack-go and client-go have no chosen Node equivalents, and the data plane and every contributor to it are Go | Rejected |
| Go, fixed boot order, compile-time registry (the current plan) | Smallest; already designed | It is the departure the owner withdrew: no activation by availability, no live recomposition, no isolate, and the kernel grows a host-service bag and point guards that are not plugins | Rejected |
| **Go port of the Cordis model** | The semantics are small and exact (2,811 lines of TypeScript, about 570 of which are proxy and stack-trace machinery Go does not need). Go gives real parallelism, one static binary, and the libraries the record already chose. Juju's `dependency.Engine` shows the same idea in Go, in use in Juju's agents since 2015: workers declare inputs by name and "will be restarted whenever there is a material change to their accessible dependencies" | We own a concurrency-critical runtime. In-process plugins cannot be installed without a rebuild. The port must be re-verified when Cordis moves | **Chosen** |

Two Go libraries were considered as a base and rejected. dskit `services` has terminal `Terminated` and `Failed` states and `modules` initialises "in the right order of dependencies": it is the fixed-order model. Juju's engine is the right shape but is LGPL-3.0 and has no effect ledger, no realms and no hooks. We write our own and keep it small.

### What the kernel contains

| Part | Cordis source | Go | Size (estimate) |
|---|---|---|---|
| Context tree: parent, owning fiber, isolate map, intercept map, grants | `context.ts` | `kernel/context.go` | 250 lines |
| Service store: keys, realms, provide, resolve, notify | `reflect.ts` | `kernel/reflect.go` | 400 |
| Fibers: states, epochs, transitions, effects, update | `fiber.ts` | `kernel/fiber.go` | 900 |
| Hooks: event, bail, waterfall, gate; ownership and scope filters | `events.ts` | `kernel/hooks.go` | 500 |
| Plugin records and the fiber index | `registry.ts` | `kernel/registry.go` | 200 |
| Logger front (`log/slog`); exporters are plugins | `logger.ts` | `kernel/logger.go` | 150 |

The root context installs four built-in services, as Cordis does: `reflect`, `registry`, `hooks` and `logger`. Cordis calls the third one `events`; we rename it because `events` is already our durable outbox (§4).

### What the kernel must not contain

Checked in CI, not by review:

- `go list -deps ./kernel` lists the standard library only.
- No service name other than the four built-ins appears in the package. No tenant, role, profile, row, YAML, JSON Schema, SQL, HTTP, retry or backoff.
- No policy. The only tunables are three deadlines (`Limits`: load, drain, dispose).
- The package has a line budget; a change that passes it needs an ADR.

### The boot library is not a plugin, and dsh agrees

dsh keeps the launcher outside the tree: `dsh-app-boot` is a library that composes the profile, admits rows, provides a few launch services before any row mounts, mounts the loader, audits the required entries and installs the fail-loud handler. "Everything is a plugin" means everything in the tree; the code that plants the tree is not in it. Our `boot` package (about 1,500 lines) does exactly that and nothing else: parse `--profile`, `--role`, `--patch` and `--dump`; compose the layers; run admission (Q12); provide `profileContext`, `catalog`, `cmdline`, `appExit` and `ready`; mount the `loader` plugin and the root include; wait for the tree to settle; audit required rows (Q13); handle signals and the bounded shutdown.

---

## Q2. The context model

### Carried from Cordis as is

- **One tree.** A context is immutable. `Extend`, `Isolate` and `Intercept` return a child and never change the parent (`context.ts`).
- **Services by name, owned by a fiber.** `Provide` registers an implementation owned by the calling fiber and removes it when that fiber unloads. One provider per name per realm; a second one fails (`reflect.ts` `provide`).
- **Access needs a declaration.** A fiber can read a service only if it, or an ancestor in the same realm, injected or provides it. Anything else is the error "cannot get property without inject" (`reflect.ts` handler `get`). Children inherit what their parents injected.
- **Optional reads.** `ctx.get(name)` reads the store without a declaration and returns nothing when the provider is not ACTIVE.
- **`ctx.inject(deps, callback)`** runs a callback as a child fiber while the named services exist. This is the Cordis idiom for an optional dependency and we keep it.
- **Realms.** `Isolate(name, label)` makes reads and writes of `name` below that context resolve in another realm; two calls with the same label join (`context.ts`, loader `isolate.ts`).
- **Intercept.** `Intercept(name, config)` attaches config that the named service merges, ancestors first, when it serves a caller below that context (`service.ts` `resolveConfig`).

### Changed because Go has no proxies

| Cordis | Go | Why |
|---|---|---|
| `ctx.llm` through a `Proxy` and TypeScript declaration merging | A typed key declared by the definition: `llm.Key.Of(ctx)` | No dynamic property interception. The gain is a compile-time type; the loss is the dotted syntax |
| A service receives the caller's context implicitly (the traceable proxy rebinds `this.ctx` to the use site) | Every registration method takes the caller's `*kernel.Context` as its first argument | Same information, passed explicitly, as Go passes `context.Context` |
| A plugin is a function, a class or an object | One shape: `kernel.Define(kernel.Spec[C]{...})` | One is enough |
| `provide` on a plugin is optional metadata | `Provide` is mandatory and checked: providing an undeclared key fails | Plugins are compiled in, so the composer can read every `Inject` and `Provide` without running code. That makes missing providers, duplicates and cycles a `profile render` error (Q3) |
| `ctx.set(name, value)` lets the owner overwrite a value in place | Not carried | Dependants hold the old value across goroutines. Replacing a value is dispose and provide, which changes the epoch and restarts them |
| Accessors, mixins, callable services | Not carried | Sugar that exists to make the proxy pleasant |

### What a plugin author sees

```go
// Package kernel (design excerpt). Compile-checking is an S0 obligation.

// A Key names a service and fixes its Go type. Only definition packages declare keys.
type Key[T any] struct{ /* name, major, sealed */ }

func NewKey[T any](name string, major int, opts ...KeyOption) Key[T]

// Of returns the service. It panics with *InjectError unless the calling fiber, or an ancestor in
// the same realm, injected or provides it. The panic is recovered at the fiber boundary.
func (k Key[T]) Of(ctx *Context) T

// Get is the optional read: present only while the provider is ACTIVE. Refused for sealed keys.
func (k Key[T]) Get(ctx *Context) (T, bool)

// Provide registers v for the calling fiber's realm. It is an effect: the service disappears with
// the fiber. It fails when the key is not in the plugin's Provide list or the realm has a provider.
func (k Key[T]) Provide(ctx *Context, v T, opts ...ProvideOption) error

// Spec declares a plugin. Inject and Provide are data: the composer reads them without running code.
type Spec[C any] struct {
	Name    string
	Inject  []Dep
	Provide []AnyKey
	Apply   func(ctx *Context, cfg C) error
}

// Define derives the config schema from C (JSON Schema 2020-12) and calls C.Validate when present.
func Define[C any](s Spec[C]) Plugin

// Context is immutable. Extend, Isolate and Intercept return children and never change the parent.
func (c *Context) Extend(meta ...any) *Context
func (c *Context) Isolate(k AnyKey, label ...Label) *Context
func (c *Context) Intercept(name string, cfg any) *Context

// Restrict returns a child that holds only the listed grants, intersected with the parent's.
// Grants shrink down the tree and never grow: the root holds all of them, the loader hands each
// row the grants its row lists, and a plugin cannot add one.
func (c *Context) Restrict(grants ...string) *Context

// Plugin starts a child fiber under this context. The child is an effect of the current fiber.
func (c *Context) Plugin(p Plugin, cfg any) *Fiber

// Inject runs apply as a child fiber once deps exist, stops it when one vanishes and runs it again
// when a provider is swapped. This is how a plugin takes an optional dependency.
func (c *Context) Inject(deps []Dep, apply func(ctx *Context) error) *Fiber

// Effect runs setup now and keeps its disposer until the fiber unloads. ErrInactiveEffect when the
// fiber is unloading or disposed.
func (c *Context) Effect(label string, setup func() (Disposer, error)) error

// Go starts a tracked goroutine. Its context is cancelled at unload and the unload waits for it.
// A returned error or a panic fails the fiber instead of the process.
func (c *Context) Go(label string, run func(ctx context.Context) error) error

// Enter takes an in-flight lease for a call served by this fiber. Unload refuses new leases and
// waits for the open ones before any disposer runs.
func (c *Context) Enter() (leave func(), ok bool)
```

A definition is an interface, its key, its value types and its version:

```go
// def/sql/sql.go
package sql

const Version = "1.0.0"

var Key = kernel.NewKey[API]("sql", 1)

type API interface {
	// Tx runs fn in one transaction in the caller's own schema, with the tenant set for RLS.
	Tx(ctx context.Context, tenant string, fn func(Tx) error) error
	// Migrate registers the caller's migrations. The schema is durable: unloading the plugin
	// removes the registration, never the schema (Q4).
	Migrate(kctx *kernel.Context, schema string, files fs.FS) error
}
```

A provider that is also a consumer, with one optional dependency:

```go
// plugins/casefile/plugin.go
type Config struct {
	OpenUnmappedCap kernel.Volatile[int] `json:"openUnmappedCap"` // changes without a restart
	Retention       string               `json:"retention"`
}

var Plugin = kernel.Define(kernel.Spec[Config]{
	Name:    "casefile",
	Inject:  []kernel.Dep{kernel.Need(sql.Key), kernel.Need(audit.Key)},
	Provide: []kernel.AnyKey{incidents.Key},
	Apply:   apply,
})

func apply(ctx *kernel.Context, cfg Config) error {
	s := &service{db: sql.Key.Of(ctx), audit: audit.Key.Of(ctx), cfg: cfg}
	if err := s.db.Migrate(ctx, "casefile", migrations); err != nil {
		return err
	}
	// Optional dependency: this child fiber exists only while an estate provider is ACTIVE.
	ctx.Inject([]kernel.Dep{kernel.Need(estate.Key)}, func(ctx *kernel.Context) error {
		est := estate.Key.Of(ctx)
		return incidents.FirstMessage.On(ctx, 20, func(c context.Context, key string,
			next kernel.Next[string, []incidents.Block]) ([]incidents.Block, error) {
			blocks, err := next(c, key)
			if known, basis, _ := est.Ask(c, "", "dependents", key); known {
				blocks = append(blocks, incidents.Block{Title: "Dependents", Body: basis})
			}
			return blocks, err
		})
	})
	return incidents.Key.Provide(ctx, s)
}
```

The plugin wrote no teardown. The migration registration, the hook listener, the child fiber and the provided service are effects, and they leave with the fiber in reverse order.

### Declaration: code and manifest

In dsh the declaration is split between the module exports (`name`, `inject`, `Config`, `provide`) and `package.json` (version, `engines.dsh` or peer ranges, `dsh.bundle`, display metadata). We keep the split and remove the duplication:

| Fact | Source of truth | Checked |
|---|---|---|
| `Inject`, `Provide`, config schema, `Apply` | The `Spec` in Go | Compile time and `profile render` |
| `id` (`<owner>/<name>`, the catalog key), `version`, `license`, `stability`, `runtime`, `instancing`, `tenantScope`, `capabilities`, `kernelApi`, `definitions` (name to version range), title and description | `plugin.yaml`, embedded | `aiopsctl plugin check`, the compatibility gate |
| Parity: `definitions` covers every key the `Spec` injects or provides | Generated check | `aiopsctl plugin check` |

For a remote plugin the kernel cannot read a Go `Spec`, so the manifest also carries `inject` and `provide`, and the handshake must return the same list (Q11).

---

## Q3. Activation

**Choice: service-availability-driven fibers, as Cordis has them. No boot order anywhere.**

### States (`fiber.ts` `FiberState`, carried as is)

| State | Meaning |
|---|---|
| `PENDING` | Waiting for every injected service to have an ACTIVE provider |
| `LOADING` | `Apply` is running |
| `ACTIVE` | Loaded; what it provides is visible |
| `FAILED` | `Apply` or its config failed. The fiber stays; only `Update` retries it |
| `UNLOADING` | Dependants are leaving and effects are being disposed |
| `DISPOSED` | Removed for good |

### The rules, each with its Cordis source

1. **Epoch.** A fiber's epoch is its revision followed by the uid of the provision bound to each injected name, in declaration order. If one is missing the epoch is INACTIVE (`_refresh`).
2. **Visibility.** A provision is usable only while its providing fiber is ACTIVE and its availability check passes (`_getImpl` strict, `_checkImpl`). What a plugin provides inside `Apply` becomes visible when `Apply` returns.
3. **Notification.** When a provision appears or disappears, or its provider enters or leaves ACTIVE, every fiber that injects that name in the same realm recomputes its epoch (`reflect.notify`, `_updateState`).
4. **One transition at a time.** A changed epoch starts a transition: INACTIVE to a value loads; anything else unloads, and the end of the unload loads again if the epoch is a value. While a transition runs, later changes are only recorded, and the transition reads the latest epoch when it ends (`_setEpoch`: `if (this.inertia) return`).
5. **Load.** Bind the current provisions (`committed`), register as a dependant of each, run `Apply`. An error or panic unwinds the partial effects and leaves the fiber FAILED. A load that was superseded cannot fail the new target (`_reload`).
6. **Unload.** Wait until no fiber depends on what this fiber provides, dispose its effects in reverse, release what it had bound, then settle or load again (`_unload`).
7. **Failure is sticky.** A dependency change does not retry a FAILED fiber; `Update` clears the error and loads (`_setEpoch`: "a failed fiber only recovers through update()"). The differential suite must confirm it at S0.

### What changes because Go is parallel

Cordis is correct because nothing interleaves inside a synchronous section. The port replaces that guarantee with five proposed rules. All five require repository-backed validation at S0; this baseline claims no completed race-detector result.

| Rule | Statement |
|---|---|
| G1. One graph lock | One mutex per root guards the service store, every fiber's state, epoch, bound provisions, provisions and effect list, and the hook tables. No plugin code ever runs while it is held |
| G2. One goroutine owns a fiber in transition | It runs `Apply` or the unload sequence without the lock. This is Cordis's `inertia` |
| G3. The call path takes no lock | A fiber's bound provisions are an immutable map behind an atomic pointer, and hook listener lists are copy-on-write. `Key.Of` and a hook dispatch are one atomic load |
| G4. Unload is quiesce, then dispose | Cancel the fiber's `Std()` context, refuse new `Enter` leases and new effects, wait for open leases and tracked goroutines up to the drain deadline, then dispose in strict reverse order. JavaScript needs no quiesce step; Go does |
| G5. Ordered observation | `internal/status` and `internal/service` are queued under the lock and delivered in order by one dispatcher goroutine, so observers see one total order without running under the lock |

Consequences for authors, stated once: a provided service is called from many goroutines and must be safe for that; `Apply` can run again in the same process, so package-level state is a bug (Q4 tests catch it); a goroutine is started with `ctx.Go`, never with a bare `go`.

A Go-only limit: a goroutine cannot be killed. An `Apply` that exceeds the load deadline makes the fiber FAILED with reason `load timeout`; the goroutine keeps running until it returns, and anything it registers after the deadline gets `ErrInactiveEffect`.

### Failure and restart storms

Cordis leaves a failed fiber failed because a person edits the config and HMR calls `update`. A server has nobody watching, so retrying is needed, and it is a plugin, not a kernel feature.

| Source of churn | Damper | Where it lives |
|---|---|---|
| A burst of provider changes | Coalescing: a fiber in transition looks only at the latest epoch when it finishes. The differential and stress suites must confirm this behaviour and record activation counts at S0; no retained measurement is claimed here | Kernel, rule 4 |
| A row that fails at load or crashes in a tracked goroutine | Retry with exponential backoff and jitter per row, reset after a stable period, give up after a threshold inside a window and leave the row FAILED with a `fiber.gave-up` event. Same shape as Juju's `ErrorDelay`, `BackoffFactor`, `BackoffResetTime`, `MaxDelay` and suture's failure threshold and decay | `restart-policy` plugin: it observes `internal/status` and calls `Update` |
| A provider that bounces and takes its dependants with it | A bounce delay before dependants reload (Juju `BounceDelay`) | `restart-policy`, as a listener on `internal/activate`, a waterfall the kernel runs before every `Apply`. A listener may delay or refuse the load |
| A provider that flaps between available and not | `Available` is for structural facts only (configured, migrated, connected once). Transient upstream health is an error on the call, never an unload | Rule for providers; conformance checks it |
| Breadth: a root provider such as `sql` restarts and most of the tree follows | Cannot be damped. The reload lane computes the blast radius before it applies a change and treats heavy rows differently (Q9) | `reload` plugin |
| Hundreds of tenant trees reacting to one shared provider | Tenant trees load lazily, with a cap on concurrent loads and jitter (Q10) | `tenant-trees` plugin |

`internal/activate` is the one hook this proposal adds to the Cordis set. It is in the spirit of `internal/update`, which lets HMR and persistence veto a restart. Only a row holding the grant `kernel:lifecycle` can register on it.

Without `restart-policy` in the tree a failure is sticky, which is what a unit test wants.

### What the composer catches before anything runs

The kernel is dynamic and never orders anything. Because `Inject` and `Provide` are declared data, `aiopsctl profile render` builds the graph of a composition and refuses, before boot: a cycle (Cordis would leave both fibers PENDING for ever), two enabled providers of one key in one realm, a required row with no possible provider, and a sealed key without exactly one granted provider (Q7). The dump prints, for every row, what it waits for and who would provide it.

---

## Q4. Effects and disposal

**Choice: one effect primitive. Every registration a plugin makes goes through it, on the caller's context.**

### How each kind of registration is tracked and unwound

| Registration | Call | Disposer does | What stays (durable) |
|---|---|---|---|
| Provided service | `key.Provide(ctx, v)` | Removes the provision, notifies dependants, waits for them to unload | Nothing |
| Child plugin | `ctx.Plugin`, `ctx.Inject` | Disposes the child fiber and its effects | Nothing |
| Hook listener | `hook.On(ctx, ...)` | Removes the listener from the copy-on-write list | Nothing |
| HTTP route | `http.Handle(ctx, listener, pattern, h)` | Removes the route; a call that arrives during the gap gets 503 with `Retry-After` | Nothing |
| Job handler, periodic job | `jobs.Handle(ctx, kind, fn)`, `jobs.Every(ctx, spec)` | Deregisters the worker for that kind in this process and waits for running jobs up to the drain deadline | Queued jobs stay in PostgreSQL and run when a handler returns |
| Outbox subscription | `events.Subscribe(ctx, type, fn)` | Stops delivery to this process | The subscriber cursor |
| Tool, executor, notifier, signal source, any registry entry | `tools.Register(ctx, t)` and the like | Removes the entry from the registry | Nothing |
| Metric | `metrics.Register(ctx, collector)` | Unregisters the collector, so a second activation does not panic on a duplicate | Nothing |
| Goroutine, timer, ticker | `ctx.Go`, `clock.Every(ctx, ...)` | Cancels and waits | Nothing |
| Upstream process | `sidecars.Ensure(ctx, spec)` | Releases the reference; the provider decides scale-down | The process, per the provider's idle policy |
| Migration | `sql.Migrate(ctx, schema, files)` | Removes the registration | The schema and its data |

The last row is the honest limit of "revertible effects". Cordis reverts effects on the context. Durable state is not an effect on the context, in dsh either: disposing `dsh-session` does not delete the session log. Unloading a plugin detaches it from its schema; removing the data is a separate, explicit, audited operation (`aiopsctl plugin purge`, and the tenant delete saga). Migrations stay expand-then-contract (§2.1 row 9).

### Rules

1. **A registration method takes the caller's context first.** `Register(kctx *kernel.Context, ...)`. A definition whose registration method has no context parameter fails the definition lint.
2. **Strict reverse order.** A fiber's effects are disposed last in, first out, one at a time, each under the dispose deadline. Cordis starts a fiber's top-level disposers in reverse order but lets asynchronous ones overlap (`_unload` maps them through `Promise.all`); the port is stricter because overlapping teardown in a parallel runtime is a source of bugs with no benefit here.
3. **Providers outlive their dependants.** Rule 6 of Q3. A consumer can still call the store inside its own disposer.
4. **A disposer error is logged with its effect label and does not stop the unload.** It is counted (`aiops_fiber_dispose_errors_total`).
5. **Late registration fails.** An effect on an unloading or disposed fiber returns `ErrInactiveEffect`. In dsh this is what makes "a spec that passes only alone" a defect.
6. **No bare `go`, no globals.** In `plugins/**` a `go vet` analyzer refuses the `go` statement outside `ctx.Go`, package-level mutable variables, `init` functions, the default Prometheus registry, `http.DefaultServeMux`, `signal.Notify` and `os.Setenv`.

### The test that proves a plugin leaves nothing behind

`kerneltest.LeaveNothing(t, root, fiber)` is mandatory for every plugin and is what dsh calls the HMR-safety test ("dispose the contributing fiber, assert cleanup"):

1. Snapshot every fake mounted in the test (each fake implements `Snapshot()`: its registered routes, handlers, subscriptions, registry entries, collectors).
2. Dispose the fiber. Assert: every fake equals its snapshot from before the mount; `fiber.Effects()` is empty; no tracked goroutine is left; `goleak` finds no other goroutine.
3. Mount the plugin again with the same fakes and dispose it again. This catches package-level state and double registration.
4. Assert the durable footprint (schemas, blob prefixes, job kinds) is a subset of what the manifest declares.

The whole suite runs with `-race`. The same check runs once more against the built binary: the recomposition suite (Q14) disables and enables every optional row of each shipped profile and compares the effect ledger printed by `aiopsctl tree --effects` before and after.

---

## Q5. Service definitions

**Rule, taken from dsh (`packages/README.md`, the note §2.3): plugins depend on service definitions, never on a provider.** And the dsh test for a seam replaces principle 3: "A seam has three roles, Service Definition, Service Provider, Consumer, and all three must exist." A definition is created when a second plugin needs to call the first, not when a second implementation appears. A plugin that nobody calls provides nothing and has no definition.

### Where a definition lives and what it holds

```
kernel/                     the runtime; standard library only
def/<name>/                 one definition
    <name>.go               interface, Key, value types, typed hooks, Version
    <name>.proto            only when the definition has a remote adapter (Q11)
    <name>fake/             the fake: a plugin that provides the key in memory
    <name>conformance/      the suite every provider, and the fake, must pass
plugins/<id>/               one plugin
    plugin.go               the exported Plugin value and nothing else
    plugin.yaml             the manifest
    internal/...            all other code
bundles/<name>/bundle.yaml  data only: an ordered patch list
dist/<name>/dist.yaml       a distribution: which plugins are in the catalog (Q11)
```

### The catalogue

Shape follows the dsh naming rule (the note §1.3): a singular key is one engine or policy, a plural key is a registry of contributions. "Sealed" is defined in Q7. "Remote": `contrib` means a third party can contribute to the registry from another process, `host` means a remote plugin can call it, a dash means in-process only.

**Infrastructure**

| Key | Promise (one line) | First providers | Sealed | Remote |
|---|---|---|---|---|
| `logger` (built in) | Per-fiber structured logger | kernel; `log-export` exporters | no | host |
| `metrics` | Registerer scoped to the fiber; series leave with it | `metrics` | no | host |
| `tracer` | Tracer scoped to the fiber | `tracer` | no | host |
| `clock` | Now, timers and tickers as effects | `clock`; the fake is a manual clock | no | - |
| `sql` | `Tx` in the caller's schema with the RLS tenant set; `Migrate` | `sql-postgres` | yes | - |
| `platformSql` | Cross-tenant transactions under the platform role | `sql-platform` (worker role only) | yes | - |
| `kv` | Small keyed state with TTL | `kv-postgres` | no | host |
| `blob` | Objects under `aiops/<tenant>/<plugin>/` | `blob-s3` | no | host |
| `jobs` | Handlers as effects; enqueue inside a `Tx`; unique, delayed, periodic, snooze | `jobs-river` | no | - |
| `events` | `Emit` inside a `Tx`; at-least-once `Subscribe` as an effect | `events-outbox`; `events-nats` when ADR-007 says so | no | host |
| `limiter` | Lease rows for money and concurrency; per-replica buckets | `limiter` | no | - |
| `http` | Routes as effects on the public, internal and edge listeners; identity runs before every handler | `http-server` | yes | - |
| `dialer` | Outbound dial that refuses cell ranges after DNS resolution (§5.7) | `dialer-guarded` | yes | - |
| `kek` | Wrap and unwrap tenant DEKs | `kek-local-file`, `kek-kms` | yes | - |
| `secrets` | Secrets scoped `(tenant, plugin, name)` | `secrets` | yes | host |
| `sidecars` | Ensure a shared or per-tenant upstream process, as an effect | `sidecars-static`, `sidecars-kubernetes` | no | - |

**Platform**

| Key | Promise | First providers | Sealed | Remote |
|---|---|---|---|---|
| `loader` | The entry tree: create, update, remove, await, locate | `loader` (mounted by boot) | yes | - |
| `reload` | The one reload lane: `RunExclusive`, `Recompose` | `reload` | yes | - |
| `pluginInventory` | Rows, states, health, versions, effects, provenance | `plugin-inventory` | no | - |
| `pluginManager` | Toggle, select bundles, install, remove, exemptions | `plugin-manager` | yes | - |
| `tenants` | Tenant record, scope, limits document, overlays | `tenant-directory` | yes | host |
| `authz` | `Check(subject, tenant, permission)` from kernel bindings | `tenant-directory` | yes | - |
| `tenantLifecycle` | Create, suspend and delete sagas built from contributed steps | `tenant-lifecycle` | no | - |
| `identities` | Verifiers; `Verify` returns `(tenant, subject, verified)` | `identity` plus `identity-*` rows | yes | - |
| `tokens` | Mint and verify agent, service and admin-scope JWTs; JWKS | `auth` | yes | - |
| `audit` | Append in the caller's `Tx`; export | `audit-chain` | yes | host |
| `auditSinks` | Export targets for the chain | rows, P2 scope | no | contrib |
| `approval` | Request, `Decide`, floor, quorum | `approval` | yes | - |
| `changes` | The change ledger: plan digest, `ChangeToken`, candidates, verdict | `change-hub` | yes | - |
| `credentials` | One write credential for the one action a valid token names | `credential-lease` plus `lease-*` rows | yes | host |
| `pii` | Salted, correlation-preserving pseudonyms | `pii-pseudonym` | yes | - |
| `llm` | Budgeted completions, virtual keys, the OpenAI-compatible endpoint | `llm-gateway` | yes | host |
| `llmProviders` | Upstream model providers | `llm-provider-*` rows | no | contrib |

**Domain**

| Key | Promise | First providers | Sealed | Remote |
|---|---|---|---|---|
| `signals` | Sources; admit, dedupe, raw group, ack; ingest tokens | `signal-core` plus `signal-ingest-*` rows | yes | contrib |
| `correlator` | Correlation key and priority for a signal | `correlate` | no | - |
| `incidents` | Incident machine, timeline, transcripts, artifacts | `casefile` | no | - |
| `investigators` | Run an investigation under slot and budget leases with a read-only toolset | `investigations` plus `investigator-*` rows | yes | contrib |
| `tools` | Describe, classify effects, cap and scrub output, invoke | `tools` plus `tool-*` rows | yes | contrib |
| `executors` | Plan; apply only with a valid `ChangeToken` | `remediation` plus `remediation-executor-*` rows | yes | contrib |
| `notifiers` | Scrub at egress, digest window, pacing | `notifications` plus `notifier-*` rows | yes | contrib |
| `oncallProviders` | Page once per incident, reconcile, 60 s cache | `paging` plus `on-call-provider-*` rows | no | contrib |
| `chatSurfaces` | Normalised envelopes, tenant resolution, dedupe | `chat` plus `chat-surface-*` rows | yes | - |
| `routing`, `pillars` | Pillar and target for an incident or a chat turn; the pillar registry | `router` plus `pillar-*` rows | no | - |
| `compliance` | Controls, findings, score | `compliance` | no | - |
| `estate`, `estateSources` | Advisory answers with a basis; the readers behind them | `estate` plus `estate-source-*` rows | no | contrib |
| `fleet`, `fleetTemplates` | Agent inventory and served config; template fragments by feature key | `fleet` plus template rows | no | - |
| `gateway` | Render and publish the tenant gateway config | `gateway-renderer` | yes | - |
| `metricsQuery`, `logsQuery` | Read-only queries for kernel-side readers | `metrics-query-http`, `logs-query-http` | no | - |
| `toolsetBindings` | Endpoints and credentials for the investigator's native toolsets | `toolset-bindings` | no | - |
| `annotations` | Write an annotation for an incident or a change | `annotator`, later `dashboard-sync` | no | - |
| `limitsRenderers` | Backend override blocks from `TenantLimits` | `limits` plus `limits-renderer-*` rows | no | contrib |
| `reportRenderers` | Render a report artifact | rows, P3 scope | no | contrib |

56 keys. Several items that §7.3 holds behind a trigger stop being kernel changes and become "add a row": `events.nats` is a second provider of `events`; `fleet.config-source` is the `fleetTemplates` registry; `estate-source` is `estateSources`; the `correlator` point and the fold rules are listeners on a hook that already exists (Q8); `approval-policy` is a gate that already exists (Q8). Their triggers still decide when the row is written. They no longer decide when the seam is cut.

### The rule and how it is enforced

| Boundary | Enforced by |
|---|---|
| `kernel` imports the standard library only | `go list -deps` check in CI |
| `def/<x>` imports `kernel`, other `def/*` and a short allow-list; never `plugins/*` | The same check; from the module split on, `def` is its own Go module whose `go.mod` cannot name the plugins |
| `plugins/A` imports `kernel`, `def/*` and third-party libraries; never `plugins/B` | Go `internal/` hides everything but `plugin.go`; an analyzer refuses the remaining import of another plugin's `Plugin` value anywhere except a generated distribution `main` and tests |
| A plugin cannot reach a provider's extra methods by a local interface assertion | Conformance: the method set of a provided value must be a subset of the definition's interface |
| A plugin cannot use a service it did not declare | Run time: `Key.Of` panics without an inject, in the kernel and on a bare test context alike |
| Manifest and code agree | `aiopsctl plugin check`: `definitions` in `plugin.yaml` covers every key the `Spec` names |

dsh enforces the same rule with `pnpm run constraints` over `peerDependencies`. Go's import graph does it more strongly: a violation does not compile in CI.

### Versioning

- Each definition carries a semver `Version` and a stability label. The key carries the major: `kernel.NewKey[API]("sql", 1)`.
- A breaking change is a new major, a new package path (`def/sql/v2`) and a new key. Both majors can be provided at once, by the provider or by an adapter plugin (dsh ships `session-format-v0-to-v1` and its siblings the same way).
- A plugin manifest states a range per definition. The compatibility gate (Q12) compares the ranges with the versions in the running binary. For an in-process plugin the Go build already proves it; for a remote or Wasm plugin the gate and the handshake are the only proof.
- This departs from dsh, which releases every first-party package in lockstep and pins exact versions (the note §4.1). First-party plugins in one distribution keep lockstep. Per-definition ranges exist because a third-party remote plugin outlives kernel releases.

---

## Q6. What becomes a plugin

Every row and package of §4, with what it turns into. "Injects" lists required services; optional ones, taken through `ctx.Inject`, are in brackets. Roles are `e` edge, `a` api, `w` worker; no entry means every role.

### Composition and runtime

| Today (§4, 03) | Becomes | Provides | Injects | Bundle |
|---|---|---|---|---|
| `loader`, `validator` | `loader`, mounted by boot before any row, with the `core:include` and `core:group` builtins | `loader` | - | boot |
| `plugin-host` | Nothing: it is the kernel. Panic recovery is the fiber boundary. Point guards move to the plugin that owns each registry | | | |
| `registry` (`init()` map) | The catalog, a table the distribution builder generates into `main`; boot exposes it as `catalog` | | | boot |
| (new) | `restart-policy` | - | `clock` | `aiops-core` |
| (new) | `reload`; `reload-watch-files` (dev, self-host); `reload-generation` (hosted) | `reload` | `loader`; the hosted row also `sql`, `events` | `aiops-core`; mode bundles |
| `plugin-manager` (P3) | `plugin-inventory`, `plugin-manager` | `pluginInventory`, `pluginManager` | `loader`, `reload`, `audit` | `aiops-core` |
| `supervisor` (package of `plugin-host`) | `sidecars-static`, `sidecars-kubernetes` | `sidecars` | `tenants`, (`tokens`) | mode bundles |
| `logger`, `metrics`, `tracer` host services | `log-export`, `metrics`, `tracer`, `telemetry-export`; `clock` (new, so tests can swap in a manual clock) | `metrics`, `tracer`, `clock` | - | `aiops-core` |
| `config` host service (`ConfigReader.For`) | Gone. `tenant.ConfigFor` reads the tenant's intercept (Q10); `kernel.Volatile` fields change in place | | | |
| `discovery`, `lifecycle` host services | Gone. Inject is discovery; `ctx.Go` and effects are lifecycle | | | |

### State

| Today | Becomes | Provides | Injects | Bundle |
|---|---|---|---|---|
| `store` | `sql-postgres`; `sql-platform` (`w`); `kv-postgres`; `blob-s3` | `sql`, `platformSql`, `kv`, `blob` | `metrics`, `clock` | `aiops-base` |
| `scheduler` | `jobs-river` | `jobs` | `sql`, `clock` | `aiops-base` |
| `events` (outbox, doorbell) | `events-outbox` | `events` | `sql`, `jobs` | `aiops-base` |
| Limiters of 03 §8.2 | `limiter` | `limiter` | `sql`, `kv` | `aiops-base` |
| `secrets`, `secrets-backend.*` | `secrets`; `kek-local-file`; `kek-kms` | `secrets`, `kek` | `sql`, `kek` | `aiops-base`; `aiops-hosted` swaps the KEK row |
| Guarded dialer (§5.7) | `dialer-guarded` | `dialer` | - | `aiops-base` |

### Surfaces, identity and tenancy

| Today | Becomes | Provides | Injects | Bundle |
|---|---|---|---|---|
| `api-server`, `http.router` | `http-server` | `http` | `metrics`, (`identities`) | `aiops-base` |
| `identity` seam and rows | `identity`; `identity-oidc`; `identity-service-token`; `identity-slack-link`; `identity-dashboard-token` | `identities` | `tenants`; the service-token row injects `tokens`, so in `aiops-only` it stays PENDING and verifies nothing, which is the behaviour 03 A13 specifies by hand | `aiops-base`; the Slack row in `aiops-chatops-slack` |
| `admin` | `tenant-directory`; `admin-api` (`a`, internal listener) | `tenants`, `authz` | `sql`, `events`; the API row `http`, `jobs` | `aiops-base` |
| `tenant` (sagas, reconciler) | `tenant-lifecycle` (`w`); `tenant-trees` | `tenantLifecycle` | `tenants`, `jobs`, `platformSql`; `loader` | `aiops-base` |
| Backend provisioning inside `tenant` | `stack-provisioning` (`w`): contributes create and purge steps | - | `tenantLifecycle`, `gateway`, `tokens` | `aiops-stack` |
| `auth` | `auth`; `auth-endpoints` (`e`); `auth-keys` (`w`) | `tokens` | `sql`, `tenants`; `http`; `jobs`, `gateway` | `aiops-stack` |
| `gateway` (renderer) | `gateway-renderer` (`w`) | `gateway` | `tokens`, `jobs`, `tenants` | `aiops-stack` |
| `fleet`, `fleet-agentapi`, fleet templates | `fleet`; `fleet-agentapi` (`e`); `fleet-sweeper` (`w`); `fleet-templates-base`; later the `agent-security` template row | `fleet`, `fleetTemplates` | `sql`, `tenants`, `jobs`; `http`, `identities`; `metricsQuery` | `aiops-stack`; templates from `agent-base` |
| `limits-renderer.*` | `limits` (`w`); four `limits-renderer-<signal>` rows | `limitsRenderers` | `tenants`, `jobs` | `aiops-stack` |
| `annotator`, `dashboard-sync` | `annotator`; later `dashboard-sync` (`w`) replaces it | `annotations` | `events`, `incidents`, `changes` | `aiops-stack` |
| `datasource/metrics`, `datasource/logs` | `metrics-query-http`; `logs-query-http` | `metricsQuery`, `logsQuery` | `dialer`, `secrets`, (`tokens`) | `aiops-datasources` |
| `toolset-bindings` | `toolset-bindings` | `toolsetBindings` | `secrets`, `tenants`, (`tokens`) | `aiops-datasources` |

### Policy

| Today | Becomes | Provides | Injects | Bundle |
|---|---|---|---|---|
| `audit`, `audit-sink` | `audit-chain`; `audit-sink-*` rows | `audit`, `auditSinks` | `sql`, `jobs` | `aiops-base` |
| `approval` and its tier table | `approval`; `approval-surface-api`; `approval-surface-slack`; `approval-surface-app` | `approval` | `sql`, `audit`, `authz`, `identities`, `events`, `jobs` | `aiops-base`; surfaces with their bundle |
| `change-hub` and its two checks | `change-hub`; `verify-alert-cleared`; `verify-readback` | `changes` | `sql`, `audit`, `approval`, `events`, `jobs`, (`estate`) | `aiops-base` |
| `credential-lease` | `credential-lease`; `lease-kubernetes` | `credentials` | `changes`, `secrets`, `audit` | `aiops-base`; the minter with its executor |
| `pii` | `pii-pseudonym` | `pii` | `secrets` | `aiops-base` |
| `llm` gateway, `llm-provider.*` | `llm-gateway`; `llm-provider-openai-compatible`; `llm-provider-replay` | `llm`, `llmProviders` | `pii`, `limiter`, `sql`, `secrets`, `http` | `aiops-base` |

### Domain

| Today | Becomes | Provides | Injects | Bundle |
|---|---|---|---|---|
| Host side of `signal-ingest`; its rows | `signal-core`; `signal-ingest-alert-webhook`, `-generic-json`, `-gitops-webhook`, `-policyreport` | `signals` | `sql`, `jobs`, `events`, `http`, `limiter`, `audit` | `aiops-base`; change and compliance sources in `aiops-pillars-mvp` |
| `correlate`, storm mode | `correlate`; `storm-mode` | `correlator` | `sql`, `signals`, (`estate`); `limiter` | `aiops-base` |
| `casefile`, context pack builder, RCA parser | `casefile`; `context-pack`; `rca-contract` | `incidents` | `sql`, `events`, `jobs`, `audit`, `correlator`, (`oncallProviders`), (`notifiers`), (`investigators`) | `aiops-base` |
| `investigator` point and guard | `investigations`; `investigator-holmes` | `investigators` | `incidents`, `llm`, `tools`, `jobs`, `limiter`; the row `sidecars`, `toolsetBindings` | `aiops-base`; the row in `aiops-pillars-mvp` |
| `tool` point, guard, MCP facade | `tools`; `tools-mcp-facade`; `tool-change-lookup`, `tool-runbook-fetch`, `tool-oncall-who`, `tool-estate-lookup` | `tools` | `pii`, `audit`; the facade `http`, `llm` | `aiops-base`; rows with their bundle |
| `remediation-executor` point and guard | `remediation`; `remediation-executor-kubernetes`; later `-gitops-pr` | `executors` | `changes`, `credentials`, `audit` | `aiops-base`; rows in `aiops-pillars-mvp` |
| `notifier` point and guard | `notifications`; `notifier-email`, `-webhook`, `-slack` | `notifiers` | `pii`, `kv`, `jobs`, `dialer` | `aiops-base`; the Slack row in its bundle |
| `on-call-provider` point and guard | `paging`; `on-call-provider-goalert` | `oncallProviders` | `incidents`, `jobs`, `kv`; the row `sidecars`, `secrets` | `aiops-base`; the row in `aiops-oncall-goalert` |
| `chat-surface` seam | `chat`; `chat-surface-slack`; `slack-commands` | `chatSurfaces` | `tenants`, `jobs`, `kv`, `pii` | `aiops-chatops-slack` |
| `router`, pillar rows | `router`; `pillar-incident`, `-change`, `-compliance` (one plugin, `aiops/pillar`, mounted once per pillar) | `routing`, `pillars` | `tenants`; pillar rows `tools`, `investigators`, `routing` | `aiops-base`; pillar rows in `aiops-pillars-mvp` |
| `compliance` | `compliance` | `compliance` | `sql`, `signals`, `incidents`, `jobs` | `aiops-pillars-mvp` |
| `estate`, readers, closure queries | `estate`; `estate-source-series`; `estate-rules-closure` | `estate`, `estateSources` | `sql`, `jobs`; the reader `metricsQuery` | `aiops-base`, P2 scope |
| Billing meters (hosted) | `billing-meter` | - | `events`, `platformSql` | `aiops-hosted` |

### Resulting inventory

| Bundle | Rows (P1 scope) | Note |
|---|---|---|
| `aiops-core` (new) | 9 | Process basics shared by the server, the CLI and tests: `restart-policy`, `reload`, `plugin-inventory`, `plugin-manager`, `clock`, `log-export`, `metrics`, `tracer`, `telemetry-export` |
| `aiops-base` | 45 | State, tenancy, policy, the registries and the incident core |
| `aiops-datasources` | 2 | `logs-query-http` joins in P2 scope |
| `aiops-stack` | 10 | Absent in `aiops-only`, as today |
| `aiops-chatops-slack` | 6 | |
| `aiops-oncall-goalert` | 2 | |
| `aiops-pillars-mvp` | 11 | |
| Mode bundle (`aiops-selfhost`, `aiops-hosted`, `aiops-dev`) | 2 to 5 | `sidecars-*`, the reload trigger, KEK swap, per-tenant instancing patches |

`self-host-full` composes 87 rows at P1 scope, against the 23-row P1 plus about 22 kernel rows today. Its structure follows dsh's composed row-tree model; the comparison does not rely on a machine-specific dsh row count. What disappears from the kernel: the plugin host, the host-service bag, the point guards, `ConfigReader`, the role filter in the loader and the loader's special case for pillar rows (a pillar row is now an ordinary row whose row-level `inject` names `tools` and `investigators`).

---

## Q7. Policy that must not be bypassed, as plugins

**Position: every contract of §2.2 stays, and none of it is in the kernel. Principle 5 is restated: policy that must not be bypassed is an invariant of the service that owns the seam, the kernel makes that service impossible to shadow, and a consumer cannot exist without it.**

dsh is built this way. Cordis has no policy at all. The dsh tools registry runs "registered monotonic guards" after its extensible `tools/pre-execute` waterfall: "a returned reason denies the call, and no later listener can turn that denial back into permission" (`dsh-tools` README). The dsh approval service returns `unavailable` when no answerer is composed, and its consumers "fail closed when it is absent" (`dsh-user-approval` README). The required-entry list is owned by `dsh-app-boot`, a library, not by a plugin.

### Five mechanisms

| # | Mechanism | What it guarantees |
|---|---|---|
| M1 | **Required inject.** A consumer injects the policy service. | Without an ACTIVE provider the consumer does not exist. If the provider fails at run time the consumer unloads. This is fail-closed by construction, and it is safety, where "required row" (Q13) is only availability |
| M2 | **Owner-side guard.** The plugin that owns a registry calls the policy before it dispatches to any contribution. Contributions are never reachable except through the registry | The guard of 07 §4.1, moved from kernel code into `tools`, `remediation`, `notifications`, `investigations`, `signal-core` |
| M3 | **Sealed keys and grants.** A sealed key cannot be read optionally, cannot be isolated, and can be provided only by a fiber whose context holds the grant `provide:<key>`. Grants shrink down the tree (`Context.Restrict`) and are set in layers 1 to 4, never by a tenant | Nobody can mount a permissive `approval` in an isolated group, read `approval` with a fallback when it is missing, or register a second provider in a restart gap |
| M4 | **No ambient authority.** Write credentials come only from `credentials.Write(token)`, secrets are scoped `(tenant, plugin, name)`, the runtime database role owns no table, RLS is forced, and the tenant gateway injects the tenant header | An executor that ignored every guard still has nothing to write with. The strongest fences are outside the process |
| M5 | **Monotonic gates.** Extension of a policy decision is a `kernel.Gate`: every listener runs, any can deny or raise, none can grant (Q8) | A plugin can add a freeze window or a stricter tier. It cannot approve |

### Each contract

| Contract (§2.2, principle 5) | Where it lives | Why it cannot be bypassed | Proof |
|---|---|---|---|
| Approval floor, quorum, tiers that only rise | `approval` (sealed). `approval.Decide` is the only entry; surfaces are thin rows. The tier table is its config; a tenant's `raise` arrives as intercept config (Q10) and is validated as monotone | M1: `change-hub` injects `approval`. M3: it cannot be shadowed. M5: `approval/gates` only tightens. A stored raise that stops validating denies every write for that tenant, as today | `approvalconformance`: never below the floor; L3 needs two distinct verified users; an invalid overlay denies |
| Change ledger before a write | `change-hub` (sealed) mints the record and signs the `ChangeToken`. `remediation` (sealed) owns `executors`; its `Apply` path exists only for a token that `changes.Verify` accepts against the ledger row | M2: no executor is reachable outside the registry. The signing key is in the provider's memory | `executorsconformance`, guard half: `Apply` without a valid token is refused; a changed digest is refused |
| Credential leases | `credential-lease` (sealed) returns a write credential only for the action a valid token names | M4 | The conformance of 07 §8.2 unchanged: a call from `Plan` or `Verify` gets none |
| Audit chain | `audit-chain` (sealed). Append happens in the caller's `Tx`; a database trigger blocks update and delete | The append-only property is the database's. M1: every state-changing provider injects `audit` | `auditconformance`; the store suite |
| PII scrub | `pii-pseudonym` (sealed). `llm-gateway` scrubs on the request path, `notifications` and `chat` at egress, `tools` on results | M1 and M2: `llm` is the only model path (plugins hold no provider key), and each registry owner injects `pii` | `llmconformance`, `notifiersconformance`: a raw fixture leaves scrubbed |
| Investigators and chat hold no write tool | `tools` classifies effects from `Describe`; `investigations` builds the toolset from read tools only | M2 | `toolsconformance`, guard half |
| Tenant fence | `sql` sets the RLS tenant on every transaction; `http` runs `identities` before every handler | M3: both keys are sealed. M4: the fence itself is RLS and the gateway | The store suite: a transaction with no tenant sees zero rows |

### What happens when a policy provider is missing or fails

- **At compose time.** A composition with an enabled executor row and no possible provider of `approval`, `changes`, `credentials`, `audit` or `pii` is refused by `profile render`, because the static graph shows the consumer can never start. No policy list is configured anywhere: the closure follows from the injects.
- **At boot.** The providers of `sql`, `audit`, `approval` and `pii`, and with any executor row those of `changes`, `credentials` and `executors`, are required rows (Q13), so a server never comes up without its policy spine. The other sealed providers need not be required: without one, its consumers do not exist.
- **At run time.** If `approval` fails, `change-hub` unloads, then `remediation`, then every executor row. Dispatch routes answer 503, readiness fails, the platform alert fires. Ingest, correlation, investigation and paging never injected `approval` and keep running, which is the rule of §2.1 row 5 that withholding one thing never withholds a page.

### What stays outside plugins, and why dsh would agree

| Stays outside | Where | dsh equivalent |
|---|---|---|
| Composition admission, the required-row audit, layer 6 kill switches | `boot` | `dsh-app-boot` owns admission and the required list; the launcher owns the `DSH_TELEMETRY_DISABLED` layer |
| Fatal handling of a panic outside any fiber | The Go runtime and `boot` | `installFailLoud` |
| The grant rule (grants only shrink) and the sealed flag on a key | `kernel`, about 40 lines, with no knowledge of what any grant means | None. dsh is single-user and trusts every plugin; this is an addition that a multi-tenant server with third-party rows forces, and it is listed as such in the checklist |
| RLS, the append-only trigger, the tenant gateway | PostgreSQL and Envoy | None needed in dsh |

One limit, as in dsh ("These checks are not a sandbox against malicious package code"): an in-process Go plugin runs with the process. Import boundaries, sealed keys and lint stop mistakes and casual bypass, not hostile code. Hostile or unreviewed code runs out of process or in Wasm, where M4 is all it can reach (Q11).

The composition-level proof is one suite against the built binary, `test/policy-bypass/`: compose without `approval`; disable it in the profile layer; lower a tier in a tenant overlay; add a row that declares `Provide: approval`; isolate `approval` on a group; fail `approval` at run time. Each must end in a refusal or in writes being impossible.

---

## Q8. Interception

**Choice: typed hooks declared by definitions. Cordis's five dispatch modes map onto three Go types.**

| Cordis (`events.ts`) | Go | Semantics |
|---|---|---|
| `emit`, `parallel` | `kernel.Event[A]` | Observers. They cannot change the outcome; a failing observer is logged |
| `bail`, `serial` | Not carried | No definition in Q5 needs first-answer-wins dispatch; a registry that needs it iterates its own contributions |
| `waterfall` | `kernel.Waterfall[A, R]` | Carried as is: listeners run outermost first; "a listener that does not call `next()` vetoes the rest of the chain, including the built-in behavior"; calling `next` twice is an error |
| `ctx.tools.guard()` in dsh | `kernel.Gate[A]` | Monotonic: every listener runs; any can deny or raise; none can grant; an error, a panic or a timeout denies |

```go
var PreExecute = kernel.NewGate[Call]("tools/pre-execute",
	kernel.OwnedBy(Key), kernel.Grant("hook:tools/pre-execute"))

var FirstMessage = kernel.NewWaterfall[string, []Block]("investigation/first-message",
	kernel.OwnedBy(Key))
```

### Four rules that differ from Cordis

1. **Order is declared, not registered.** Cordis orders listeners by registration, with `prepend`. Under availability-driven activation in a parallel runtime that order would change from boot to boot. A listener states a phase (the definition names the phases); ties break by row id. The dump prints every chain in order.
2. **Only the owner dispatches.** `OwnedBy(key)` makes `Run`, `Check` and `Emit` fail unless the caller is the fiber that provides the key. In Cordis any plugin can dispatch any event.
3. **Registration can need a grant.** A hook declared with `Grant(name)` accepts listeners only from a context holding that grant. Grants are row data in layers 1 to 4 and appear in the dump.
4. **A decision that grants permission is never a waterfall.** dsh's approval answerers are waterfall listeners that may return "allowed" without calling `next`. That is right for one user at one terminal and wrong here. Approval is `approval.Decide` plus a gate.

Carried without change: a listener is an effect of the fiber that registered it; a listener registered from a tenant tree sees only that tenant's dispatches (Cordis `Context.filter`, which dsh uses to scope listeners to one agent).

### The hooks

| Hook | Kind | Owner | Who may register | Used for |
|---|---|---|---|---|
| `internal/plugin`, `internal/status`, `internal/service` | event | kernel | grant `kernel:observe` | `plugin-inventory`, `restart-policy`, metrics |
| `internal/config`, `internal/update`, `internal/commit` | waterfall, waterfall, event | kernel | grant `kernel:lifecycle` | The loader resolves config, logs reloads and persists programmatic updates, as in Cordis |
| `internal/activate` (new) | waterfall | kernel | grant `kernel:lifecycle` | Bounce delay and backoff |
| `loader/patch-context`, `loader/entry-init`, `loader/volatile-update` | waterfall, event, event | `loader` | grant `kernel:lifecycle`; `volatile-update` goes to the owning fiber only | Isolate and intercept handling; volatile fields |
| `composition/recomposed` | event | `reload` | any | Drift detection across replicas, inventory |
| `signal/admit` | gate | `signal-core` | grant | Per-row buckets and body limits; the per-tenant bucket is the owner's own inner check |
| `incident/correlate` | waterfall | `correlate` | grant | Label maps today; the fold rules of §7.3 when their trigger fires |
| `incident/investigate` | gate | `casefile` | grant | Storm mode, `AIOPS_AGENTS_PAUSED`, budget exhausted, per-incident run cap |
| `investigation/first-message` | waterfall | `casefile` | any | Context pack blocks, in declared order |
| `investigation/answer` | waterfall | `casefile` | grant | The RCA contract check and its flags |
| `llm/request` | gate, then waterfall | `llm-gateway` | grant | Budget and rate gates; routing by tier. The scrub is the owner's inner step and no listener can skip it |
| `tools/pre-execute`, `tools/post-execute` | gate, waterfall | `tools` | grant | Effects check; output cap. The scrub is the owner's |
| `notify/pre-send` | waterfall | `notifications` | grant | Digest and pacing. The scrub is the owner's |
| `approval/gates` | gate | `approval` | grant | Change windows and freezes (P2 scope), a sole-approver rule, a tenant policy engine if one is ever asked for |
| `change/pre-dispatch` | gate | `change-hub` | grant | Freeze, quorum re-check extensions |
| `tenant/create`, `tenant/purge` | ordered steps | `tenant-lifecycle` | any plugin that owns durable tenant state | Replaces the hard-coded purge order of §5.6 with steps that state what they run after |

Two paths are deliberately hook-free: paging (nothing a plugin registers can withhold a page) and the audit append.

Remote plugins may register only on hooks whose definition marks them remote-capable. Each call carries a deadline; a gate that times out denies.

---

## Q9. Composition and reload

**Profiles, bundles and patch layers stay exactly as §6.1 and the note §3.2 have them. What changes: layers 1 to 4 are recomposed live, and the tenant layer becomes intercept config plus a tenant tree.**

### Row format

| Field | dsh entry field | Rule |
|---|---|---|
| `id` | `id` | Unique in the composition; patches address it |
| `plugin` | `name` | The catalog key, `<owner>/<name>`. On a patch it is an assertion, never a rebind |
| `config` | `config` | A patch replaces it whole; no deep merge |
| `enabled` | `disabled` | A disabled group stops its children |
| `required` | (a global list in `dsh-app-boot`) | Availability policy only (Q13) |
| `group` | `group` | The row's `config` is a child entry list; child ids are `parent:child` |
| `inject` | `inject` | Extra required services for this row |
| `isolate` | `isolate` | Per service name: `true` for a realm private to this row and its children, or a label shared by every row that names it. Refused for sealed keys |
| `roles` (ours) | `disabled: !!js process.platform === 'win32'` | A compose-time gate from a closed vocabulary (`edge`, `api`, `worker`). A row outside the process's role is detached as disabled, with the reason in the dump |
| `grants` (ours) | none | Hook and provide rights (Q7, Q8). Not settable by a tenant |

Rows still carry no expressions (§6.1). dsh uses `!!js` for four things, and each has a closed replacement: a platform gate is `roles`; a presence gate (`disabled: !ctx.get('profileContext')`) is a row-level `inject`, which leaves the row PENDING instead of disabled; a value read from a startup service is read by the plugin itself through an inject; a path or a secret is a reference resolved by the plugin (`{secret: name}`).

### Layers

| # | Layer | Reload |
|---|---|---|
| 0 | Empty root | - |
| 1 | Bundle patches, in the profile's bundle order; only direct bundles | Live when the bundle list changes; new code needs a new binary |
| 2 | Profile layer | Live |
| 3 | Site layer | Live |
| 4 | `--patch` overlays | Fixed for the life of the process |
| 5 | Tenant overlay | Live, per tenant, without touching the shared tree (Q10) |
| 6 | Kill switches, launcher-owned | Fixed for the life of the process |

### The unit of reload

The entry. The loader diffs the candidate tree against the running one by row id and, per row, does the least it can (`entry.ts` `update`):

| Change | Effect |
|---|---|
| Only `Volatile` fields of `config` | Committed in place into the running references; `loader/volatile-update` tells the owning fiber. No restart |
| `config` otherwise | `fiber.Update`: the fiber restarts. Its dependants restart only if it provides something, through their epochs |
| `inject`, `isolate`, `grants` | The row's context is patched (`loader/patch-context`), then the fiber restarts against the new bindings |
| `enabled: false`, or the row is removed | Disposed; consumers of what it provided go PENDING |
| A new row | Created; it starts when its injects are met |

### One serialized reload

dsh puts module reloads, include refreshes and plugin-manager writes in one queue (`dsh-hmr` `runExclusive`, which "rejects nested transactions") and keeps package installs outside it. The `reload` plugin is that queue: one goroutine reads one channel. File watches, the generation poller, the plugin manager and `aiopsctl` all submit to it.

One recomposition, in order (`dsh-app-boot` `recomposeProfile`, adapted):

1. **Read and compose** layers 0 to 4 into a candidate tree. Pure; no running state is touched.
2. **Preflight**, still pure: every changed row's config against its schema and `Validate`; the compatibility gate and signature policy; the static graph (cycles, duplicate providers, sealed keys, required rows that could never start); the blast radius, which is the set of fibers that will restart.
3. **Reject the whole generation** if preflight fails: "malformed or invalid live patches are rejected without changing the running configuration."
4. **Apply the diff**: remove what is no longer listed and wait for disposal; update; create.
5. **Settle**: `loader.Await` until no fiber is in transition, under a deadline.
6. **Judge**: a new inactive row or a changed diagnostic is a failure of the operation and is reported; unchanged inactive rows are warnings. If a required row is not ACTIVE, **roll back** by applying the previous generation.
7. **Publish** `composition/recomposed` with the digest of the composition, the generation number and the outcome.

Step 6's rollback is ours. dsh says a later configuration reload "does not repeat the required-startup audit and does not roll back the whole update", which suits a person at a keyboard and not a server.

What a reload cannot do: add in-process Go code (a new binary and a rolling restart, as today), change kernel limits, or change layers 4 and 6.

### What happens to work in flight

During a row's restart, its routes answer 503 with `Retry-After`; its job kinds have no worker in this process and stay queued in PostgreSQL; its outbox subscriptions pause and resume from the cursor. In-flight calls finish or are cancelled at the drain deadline (rule G4). Agents retry from their WAL and Alertmanager re-sends at the next group interval, which is the contract a process restart already relies on (03 A11).

### What hosted does

| Change | Hosted | Self-host and dev |
|---|---|---|
| Tenant overlay, per-tenant enablement | Live, on every replica, through the doorbell and a version poll | Same |
| A toggle or config change through the plugin manager or the site layer | A new **generation** row in PostgreSQL. Each replica's `reload-generation` submits it to its own lane. Replicas take a lease so that a bounded number recompose at once; the generation is marked bad for all if the first replicas roll it back | `reload-watch-files` watches `profile.yaml` and the two patch files and submits on change |
| A change whose blast radius contains a heavy row (`sql-postgres`, `jobs-river`, `events-outbox`, `http-server`, `signal-core`, `auth`; their manifests say `reload: drain`) | The replica fails readiness, waits for the load balancer to drain it, applies, waits for the required rows, and returns. One replica at a time: a rolling restart without new pods | Applied in place; the gap is seconds and senders retry |
| New code, a new bundle version, a new kernel | Image change and rolling restart; kernel N and N-1 coexist (§2.1 row 9) | Restart |

Every replica publishes the digest of its composition, as 07 §7.3 already has it, so replicas that disagree show up as drift.

---

## Q10. Tenancy in the context tree

**Choice: a tenant is a scope on the tree, not a copy of the tree. Rows are shared by default. A tenant tree holds only the rows that are per-tenant by nature.**

First, what `isolate` is not. It separates service names into realms. It does not separate memory, goroutines or data: every realm is in one address space. Tenant data isolation stays where §5 put it: RLS with the tenant set per transaction, the tenant gateway, secrets scoped by tenant. Cordis has no notion of a tenant; what follows borrows the two dsh patterns built for per-agent state, nested loader trees (`dsh-agent-preset`) and scoped contributions (`dsh-scope`).

### Three constructs

| Construct | What it is | Cordis primitive |
|---|---|---|
| **Tenant scope** | One child context per active tenant, kept by `tenant-directory`: the tenant id as metadata, and the tenant's overlay fields as intercept config keyed by row id. A call made for a tenant carries the scope in its `context.Context` | `Extend`, `Intercept` |
| **Shared row** (the default) | One fiber serves every tenant. It reads a tenant's fields with `tenant.ConfigFor[C](kctx, ctx)`: the fiber's effective config with that tenant's intercept merged in, validated against the fields its schema marks `x-aiops-tenant`, cached per activation and overlay version | `Service[resolveConfig]`: intercept config merged along the caller's context |
| **Tenant tree** | For rows whose manifest says `instancing: per-tenant` (the HolmesGPT client, the GoAlert adapter, any remote third-party plugin in hosted): `tenant-trees` mounts one group per tenant under that tenant's scope, holding that tenant's instances | A nested loader group, as dsh mounts one tree per agent preset |

Layer 5 keeps its rules (§6.1, 08 §1.7): only rows with `tenantScope: per-tenant`, only `enabled` and the marked fields, validated by the loader before it is stored, never a row insert, never a grant. What changes is the mechanism: `ConfigReader.For` and its one-minute poll become an intercept on the tenant scope.

### How a shared consumer finds a per-tenant instance

It does not cross realms. A per-tenant fiber registers into the shared registry like any contributor (`investigators.Register(ctx, inv)`); the registry reads the tenant from the registering context and files the contribution under that tenant. A lookup for `(tenant, kind)` returns the tenant's contribution, else the shared one. Disposing the tenant's fiber removes it. This is `dsh-scope`: "child scopes inherit ancestor contributions, with the nearest definition taking precedence ... disposing a scope removes everything owned by it."

`isolate` is used inside a tenant tree only when one per-tenant row provides a singular service to its tenant siblings (a tenant's sidecar handle read by that tenant's investigator row). The group row then carries `isolate`, which gives "one `isolate` realm to a provider and its consumers together" (`dsh-app-boot`, design notes). Sealed keys cannot be isolated, so no tenant tree can replace `approval`, `audit` or `sql`.

### Per-tenant enablement at run time

1. A tenant admin submits an overlay. The API composes the tenant view with the loader's own code and refuses anything outside layer 5's rules.
2. The overlay and its outbox event commit in one transaction; the doorbell fires.
3. On each replica `tenant-directory` swaps that tenant's scope for one with the new intercept. Shared rows see the new fields on the next call. Nothing restarts.
4. `tenant-trees` diffs the tenant's entry list by row id and updates that tenant's group: a row enabled for the tenant gets a fiber, a disabled one is disposed. For a shared contribution, the registry owner skips a contribution whose row is disabled in the caller's scope.

### Per-tenant upstream sidecars

`sidecars.Ensure(ctx, spec)` is an effect of the tenant's fiber. `sidecars-kubernetes` creates or scales the workload in `t-<id>`, injects the virtual key and the read-only service JWT, and returns the endpoint when it is ready; its idle policy scales HolmesGPT to zero after 15 minutes (ADR-014 keeps the mechanism). Disposing the fiber releases the reference; the tenant delete saga removes the workload. In self-host `sidecars-static` returns the shared endpoint that compose or Helm started and only probes it. Both pass one conformance suite.

Tenant trees load lazily (first investigation, first page, or enablement), unload after idle, and load under a concurrency cap with jitter, so a boot or a shared-provider swap is never 300 simultaneous activations.

### Cost at 300 tenants

Estimates, to be measured in step S5 of Q16.

| | A full tree per tenant (rejected) | This proposal |
|---|---|---|
| Fibers | 300 x 87, about 26,000 | 87 shared, plus 2 to 4 per loaded tenant: at most about 1,200 |
| `Apply` calls at boot | About 26,000 | 87; tenant trees on first use |
| Job handlers, routes, collectors | Registered 300 times each; metric cardinality multiplies | Registered once; a handful per loaded tenant |
| Kernel bookkeeping | Tens of megabytes, plus every plugin's own state 300 times | 1 to 2 MB (about 1 KB per fiber, under 1 KB per scope) |
| A shared provider is swapped | About 26,000 restarts | 87 and the loaded tenant fibers, under the cap |
| Isolation gained | None: one address space either way | The same |

---

## Q11. Plugin runtimes and installation

**Three runtimes behind the same definitions. A plugin author picks one in the manifest; a consumer cannot tell which.**

| Runtime | For | Isolation | How it gets into a profile | Live |
|---|---|---|---|---|
| In-process Go | First-party rows, infrastructure providers, anything on a hot path, reviewed community plugins | None: trusted code | The distribution builder compiles it into the catalog | No: a new binary |
| Remote fiber over gRPC | Third parties in any language; anything that runs model-chosen actions; per tenant in hosted (principle 6) | Process or container; egress policy rendered from the manifest's `capabilities` | An OCI artifact that carries the manifest and an image reference | Yes |
| WebAssembly (wazero, as round 1 §2.1 found) | Pure functions: signal parsers, limits renderers, report templates, answer checkers, gate predicates | Sandbox: memory cap, no host unless allowed | An OCI artifact that carries the manifest and the `.wasm` blob | Yes |

### The catalog replaces `init()`

dsh activates nothing by scanning: a package is used only when a row names it ("explicit, no spooky scan"). The Go equivalent is a generated `main`, in the manner of the OTel Collector Builder and xcaddy (round 1 §2.1):

```yaml
# dist/aiops/dist.yaml
kernel: v0.4.0
plugins:
  - aiops.dev/plugins/casefile
  - aiops.dev/plugins/sql-postgres
  - example.com/acme/aiops-tool-ticketing v1.4.0   # a third-party Go module, no core source changes
```

`aiopsctl build --manifest dist.yaml` writes `main.go` with one `catalog.Add(id, pkg.Plugin)` line per entry, runs the license allow-list over the module graph (07 §2), and compiles. A plugin package has no `init` and no global registration, so importing it has no effect until a row names it.

### The remote fiber

`aiops/remote` is an ordinary in-process plugin. One row per remote plugin instance; its config is the endpoint and the pinned manifest digest.

- **Handshake.** `Describe` returns the remote's spec: inject, contributions, config schema, protocol version and the sha256 of its manifest, which must equal the installed one.
- **Activation.** The proxy fiber's inject list is the remote's. So a remote plugin is activated when the services it needs exist and unloaded when one vanishes, by the same rules as any fiber.
- **One stream per activation.** The kernel sends config, tenant and `Dispose`. The plugin sends registrations, readiness and failure. Each registration becomes an effect of the proxy fiber.
- **Calls in.** A contribution is called through a gRPC service generated from the definition's proto, behind the registry owner's guard like any other contribution.
- **Calls out.** The injected services the remote may use are served back on the same connection. The kernel takes the row and the tenant from the connection, never from a request field, and in a multi-tenant profile a remote instance serves one tenant (07 §5, kept).
- **Loss.** A short disconnect marks the contributions unavailable, fails calls fast and reconnects with backoff: it does not unload dependants. Closing the stream, or exceeding the reconnect budget, unloads the proxy fiber, and the kernel unwinds everything the remote registered. Revertible effects hold across the process boundary because the proxy owns them.
- **Who starts it.** The orchestrator, or `sidecars` for a per-tenant instance. The kernel never launches a plugin process.

The Go SDK lets one `Spec` build either way: `kernel.Define` in a distribution, or `remote.Serve(spec)` as its own program. Python follows, generated from the same protos.

### Which definitions get a remote adapter

The "Remote" column of Q5. Contributions: `tools`, `executors`, `investigators`, `notifiers`, `signals`, `oncallProviders`, `llmProviders`, `auditSinks`, `limitsRenderers`, `estateSources`, `reportRenderers`. Callable from a remote plugin: `logger`, `metrics` and `tracer` over OTLP, `secrets` (its own scope), `kv`, `blob`, `audit` (append), `credentials`, `llm`, `tenants` (info), `events` (subscribe).

Never remote: `sql` (a transaction cannot span a process boundary; a remote plugin returns results or uses `kv` and `blob`), `jobs`, route registration on `http`, kernel hooks, and the provider side of any sealed key.

**From which step.** The remote runtime and its first adapter, `tools`, are built in step S4, before any product feature. From then on a definition with `contrib` ships its proto, adapter and gRPC conformance leg in the step that introduces it. Round 1 deferred the protos because "freezing twelve wire contracts before any implementation has used them costs more" (§2.1 row 1). That cost is real. It is bounded here by keeping a proto at `v1alpha` until a second implementation or a first third party uses it, and by running the same conformance suite in process and over gRPC so the two cannot drift.

### Installing a plugin through supported interfaces without core source changes

| The self-hoster has | Command | What happens |
|---|---|---|
| A remote plugin | `aiopsctl plugin add oci://registry.example/acme/tool-ticketing:1.4.0 --profile self-host-full` | Resolve; verify the signature against the profile's trust policy; read the manifest; run the compatibility gate; write the digest to `plugins.lock`; insert the row in the profile layer; print the compose service or Helm values for the container. The reload lane recomposes, and the row goes ACTIVE when the process connects. No rebuild |
| A Wasm plugin | The same command | The blob is stored with the profile. No extra process, no rebuild |
| An in-process Go plugin | Add the module to `dist.yaml`; `aiopsctl build`, or run the `aiops-builder` image so no local toolchain is needed | A new image; restart |
| No wish to build | Use a prebuilt image: `aiops` (first-party) or `aiops-contrib` (first-party plus reviewed community in-process plugins, as the collector ships a contrib distribution) | |

- **Index.** A static signed index (a Git repository or an OCI artifact): id, versions, runtime, definitions and ranges, kernel API range, artifact digest, signer identity. `aiopsctl plugin search` reads it. No server of ours is needed at install time.
- **Signing.** cosign, per ADR-020. Verification material travels with the artifact so an air-gapped install works; the trust root is pinned in the profile; `allowUnsigned: [ids]` is the explicit escape, as Grafana's is.
- **Failure handling, carried from `dsh-plugin-manager`.** Snapshot `profile.yaml`, `plugins.lock` and the profile patch before the operation and restore them when the fetch, the verification or the gate fails; one profile write lock shared by the CLI and the service; a result that names the last stage reached and whether the change is `applied` or `restart-required` with a reason.

**The honest gap.** In dsh, `pnpm add` in the profile directory followed by a reload puts new in-process code into the running application. Go cannot do that. Here a live install is a remote or Wasm plugin; in-process code is a build and a restart.

---

## Q12. The plugin manager as a plugin, and the compatibility gate

### `plugin-manager`

A row of `aiops-core` that provides `pluginManager`; `plugin-inventory` provides the read-only view. Carried from dsh:

| Operation | Effect on files | Applied |
|---|---|---|
| Toggle a row | Sets `enabled` in the last matching override of the profile layer, or appends one. An override that holds only `id` and `enabled` is removed when the bundles already give that state, so the profile layer records only departures | Live, through the lane |
| Enable or disable a bundle | Edits the ordered bundle list in `profile.yaml`. Enabling appends at the end, which can change precedence, and the result says so | Live for bundles whose plugins are in the catalog; `restart-required: new-code` otherwise |
| Install, update, remove a remote or Wasm plugin | Q11. Removal deselects, unloads, then deletes | Live |
| Grant or revoke a version exemption | Writes `compatibility.yaml` only | At the next composition |
| List | Rows with state, health, epoch, what each waits for, effects, version, provenance; bundles with skipped ones and why | - |

Rules: it cannot disable itself, a required row or the provider of a sealed key; it cannot change another profile; every operation runs in the reload lane under the profile write lock; every operation is audited and emits `plugin-manager/changed`. A tenant admin reaches a narrower surface, the tenant overlay (Q10), through the same service.

Surfaces: the internal admin API, `aiopsctl plugin`, a read-only Slack command, and later a page in the dashboard app. dsh also exposes `plugin_manager` as a tool to its agent. That is not carried: an investigator must never be able to change the composition of a multi-tenant server.

### Compatibility gate

Admission runs "only at composition boundaries" the platform owns (`dsh-app-boot`): `aiopsctl plugin add`, `aiopsctl build`, boot, every recomposition, and a tenant overlay that enables a row.

| Rule | dsh | Here |
|---|---|---|
| Runtime range | `engines.dsh` alone decides when present | `kernelApi` alone decides when present |
| Otherwise | Every peer range on a dsh package must match the runtime version | Every `definitions` range must contain the version of that definition in this binary |
| Neither declared | No constraint | No constraint for first-party rows of the distribution (lockstep, as in dsh); **denied** for anything else. Stricter than dsh, because a remote plugin outlives kernel releases |
| Malformed or empty range | Incompatible | Incompatible |
| Remote and Wasm | - | `protocol` must be supported; checked again at every handshake |
| Stability | - | At or above the profile's `minStability` |
| Signature | - | Per the profile's trust policy |
| A denied row | Detached as `disabled: true` in the launcher's copy; no file is rewritten | The same; listed in the dump and the inventory with the unmet requirement |
| A denied bundle | Skipped and listed in `skippedBundles` | The same |
| Exemptions | `compatibility.json`: exact `package@version` to exact runtime versions; neither upgrade inherits it; needs `--accept-risk` | `compatibility.yaml`, the same semantics, `aiopsctl plugin allow-version ... --accept-risk` |

The gate is not a sandbox, in dsh or here.

Health is separate from state. A provider may attach a health function to what it provides; the inventory shows `ok`, `degraded` or `failed` beside the fiber state, and the health of a required row feeds readiness. Health never unloads a dependant (Q3).

---

## Q13. Failure policy

**Required rows are an availability policy: they decide whether the process boots or degrades. Safety does not depend on them (Q7, M1).**

Required rows are row data (`required: true`, set by the bundle that inserts the row), not a global list in the launcher as in dsh. Every server profile requires `sql-postgres`, `events-outbox`, `jobs-river`, `http-server`, `tenant-directory`, `audit-chain`, `approval` and `pii-pseudonym`, and, when any executor row is composed, `change-hub`, `credential-lease` and `remediation`. Hosted adds `auth`, `fleet-agentapi`, `signal-ingest-alert-webhook` and `gateway-renderer`. With `--role` the list applies to the rows that role runs. One thing is stricter than dsh: there a disabled required entry is ignored; here "a required entry has no enabled row" stops boot (§6.1, kept).

The one failure matrix, adapted from `dsh-app-boot`:

| Failure | Optional row at boot | Required row at boot | Live recomposition |
|---|---|---|---|
| `profile.yaml`, a named bundle or a `--patch` file is missing; a layer file is malformed or empty | Stop boot | Stop boot | The generation is rejected whole; the running tree is unchanged |
| A row names a plugin that is not in the catalog (dsh: the module import fails) | Warn; row inactive | Stop boot | Rejected in preflight |
| The compatibility gate or the signature policy denies | Row detached as disabled in the composed copy; listed | Stop boot | Rejected, unless the row was already denied |
| Config fails its schema or `Validate` | Warn; row FAILED with the reason | Stop boot | Rejected in preflight; the existing row keeps its instance and config |
| Static graph error: a cycle, two providers of one key in a realm, a sealed key without exactly one granted provider, `isolate` on a sealed key | Stop boot: a composition error, not a row failure | Stop boot | Rejected |
| `Apply` returns an error or panics | Warn; row FAILED after its partial effects are unwound; siblings continue; `restart-policy` retries | Stop boot | Reported; siblings keep running; a required row rolls the generation back |
| `Apply` exceeds the load deadline | The same, reason `load timeout` | Stop boot | The same |
| An injected service has no ACTIVE provider | Row PENDING; one grouped warning; it starts when a provider appears | Stop boot once the tree has settled | The row waits; a required row rolls back |
| A remote plugin is unreachable or fails the handshake | Proxy row degraded; contributions unavailable; reconnect with backoff | Stop boot | The same |
| The provider of a sealed key fails or unloads while running | - | Its consumers unload, so writes stop; readiness fails; the platform alert fires; ingest and paging continue | The same |
| `restart-policy` gives up on a row | FAILED until `Update`; `fiber.gave-up` event | Readiness fails | The same |
| A tracked goroutine returns an error or panics | Row FAILED; not fatal | Readiness fails until it recovers | The same |
| A panic in a goroutine no fiber tracks | Fatal: the process exits non-zero | Fatal | Fatal |
| A disposer errors or exceeds its deadline | Logged with the effect label; the unload continues | The same | The same; the lane reports it |
| Row absent or `enabled: false` | Ignored | Stop boot | A required row cannot be disabled |

Optional failures print once, as one grouped diagnostic, with for each row its state, its error and what it waits for.

Two lines differ from dsh on purpose. dsh makes any detached rejection fatal "because only the throw site knows which state is intact". Go has no unhandled rejection: an error is a value, and `ctx.Go` turns a goroutine's error or panic into a failed fiber, which the effect ledger can unwind. A goroutine nobody tracks still ends the process, and the lint of Q4 keeps plugins from creating one. And dsh does not roll back a live reload; Q9 does.

**The matrix is a test on the built artifact.** `test/failure-matrix/` boots the real binary through every cell, with a test-only profile and fault plugins compiled into a test distribution (`fail-apply`, `panic-apply`, `slow-apply`, `bad-config`, `rogue-provider`, `leaky-goroutine`), and asserts the exit code, the grouped diagnostic, `/readyz` and the dump. This is `web-failure-matrix.expected.e2e.ts` in dsh, which "runs the built CLI through startup failures and native configuration HMR ... without model API calls".

---

## Q14. Testing in isolation, in tiers

| Tier | What it proves | How | Runs |
|---|---|---|---|
| T0 Static | Boundaries hold before any test runs | Import analyzer; the plugin lint of Q4; manifest and `Spec` parity; config schema is draft 2020-12 and the examples validate | Every commit |
| T1 Bare context | The plugin alone: it starts with only the services it names, does its job against fakes, and leaves nothing | `kerneltest.New(t)`, fakes from `def/<x>/<x>fake`, the plugin, `LeaveNothing`; always `-race` | Every commit |
| T2 Conformance | Every provider of a definition behaves the same, and so does the fake | `def/<x>/<x>conformance.Run(t, mount)` for each provider and for the fake; for a registry, an obligations half (what a contribution must do) and a guard half (what the owner must refuse); one leg per runtime the definition supports: in process, gRPC, Wasm | Every commit; providers that need PostgreSQL, S3 or Envoy use a container |
| T3 Real composition | The built artifact with real rows | `loader-smoke` boots a test-only profile from the built binary and mocks only the non-deterministic boundary (the model through `llm-provider-replay`, outbound HTTP through fixtures, the clock). It asserts operator-visible output and durable state by reading them back. Also here: the failure matrix, the policy-bypass suite, the recomposition suite, and a golden dump of every shipped profile | Every pull request |
| T4 Record and replay | Upstream contracts | HolmesGPT cassettes and sender fixtures; `AIOPS_SNAPSHOT=replay` enforced in CI | Every pull request |
| T5 Live | The demo of §7.1 and the lab | Real backends, a real model; self-skips without credentials | Release |

The tier 1 test of the plugin in Q2, complete:

```go
func TestCasefileLeavesNothing(t *testing.T) {
	root := kerneltest.New(t) // a bare root context with short deadlines
	root.Plugin(sqlfake.Plugin, nil)
	root.Plugin(auditfake.Plugin, nil)
	f := root.Plugin(casefile.Plugin, casefile.Config{Retention: "30d"})
	kerneltest.MustActive(t, f)         // Await returns the config or startup error
	kerneltest.LeaveNothing(t, root, f) // dispose, compare, mount and dispose again
}
```

Rules carried from dsh (`docs/testing.md`, the note §5.3):

- **A fake that does not pass its definition's conformance suite is a lie.** The fake is a provider like any other.
- **Every registry gets the dispose test.** It is `LeaveNothing` with a contributing fiber.
- **Tier 1 is necessary, never sufficient.** "Hand-built `ctx.plugin(...)` suites are insufficient": a product-visible plugin needs a tier 3 test.
- **Mock only the expensive or non-deterministic boundary**, and keep everything downstream real.
- **Assertions re-read the world** (rows, files, posted messages), never the plugin's own account of what it did.

One rule is not carried: dsh's per-file 100 % coverage gate. It applies here to `kernel`, `boot` and the loader, which are small and where an untested branch is a concurrency bug waiting; plugins have no numeric gate.

One rule is added for the port itself: a **differential test against Cordis**. A fixed set of scripted scenarios (about thirty, starting from the nine source-derived behaviours listed above) runs on the pinned Cordis version under Node and on the Go kernel, and the settled states and status traces must match. It runs in the kernel's own CI only. It is how "faithful" stays a tested property when Cordis releases a new minor: the scenarios are re-run on the new version, and every difference becomes either a port change or a line in the checklist.

**What a contributor runs before opening a pull request**: `aiopsctl plugin check ./plugins/<id>`. It runs T0, T1 and T2 for every definition the plugin provides or contributes to, in every runtime its manifest lists, and, when the plugin is product-visible, one tier 3 smoke on the `dev` profile with the plugin's own test patch. For a third party it needs only the `kernel` and `def` modules.

---

## Q15. Apps are profiles

dsh: "The `dsh` command is the sole supported Node application launcher ... SDK and ACP are profiles, not separate public bins." The same holds here.

| Surface | Profile | What differs |
|---|---|---|
| Server, self-host and dev | `self-host-full`, `aiops-only`, `dev` | Bundles, as in §6.3 |
| Server, hosted | `hosted`, started three times with `--role edge`, `api` or `worker` | **A role is a compose-time gate on rows, not three profiles and not a filter in the loader.** Each row states `roles` in the bundle that inserts it; `aiopsctl profile render --profile hosted --role api --dump` shows exactly what that process runs, with the other rows detached and the reason given. The security split of §3.1 holds because `sql-platform` and the admin-scope signer are `worker` rows, and their credentials are mounted only into worker pods |
| Migration job | `--profile <p> --mode migrate` | A mode bundle whose runner waits for the tree to settle, runs the migrations the rows registered under one advisory lock, and exits through `appExit`. In normal mode `sql.Migrate` only checks the schema version, and refuses a schema older than the binary (03 §8.2) unless the profile allows auto-migration (dev, self-host) |
| CLI | `cli`: `aiops-core` plus `aiops-cli` | Commands are plugins that contribute to a `commands` registry (`profile render`, `plugin check`, `plugin add`, `agent render`, `tree`). `profile render` mounts the loader and no store, which is why the base is split into `aiops-core` and `aiops-base`. `aiopsctl` is a small distribution of the same launcher, so an operator's laptop does not carry the server. A third-party bundle can add a subcommand |
| Agent side | `agent-host`, `agent-k8s` | These stay bundles of templates and content. The agent is unmodified Alloy with its own component model; there is no kernel process on the host and this proposal does not add one. The templates are contributions to `fleetTemplates`, mounted in the server profile for the fleet composer and in the `cli` profile for `aiopsctl agent render`, so the two render the same bytes |

Readiness is a row: `readiness` injects `loader` with the intercept `await: true`, which Cordis's loader uses to keep a dependant PENDING while entries are still loading (`Loader.Intercept`). It activates when the tree has settled, and `/readyz` then reports the required rows. Shutdown disposes the root fiber under a bounded, escalating deadline; dsh uses five seconds, a server that drains requests needs longer, and the value is boot configuration.

---

## Q16. Order of work with no date pressure

The sequence is chosen so that the model is proven before anything is built on it, and so that from step S5 on no feature can be anything but a plugin. Each step ends in a test, not a date.

| Step | Builds | Exit test |
|---|---|---|
| S0 Kernel | `kernel`: context, keys, realms, fibers, effects, hooks, grants. `kerneltest` | The Cordis quick start runs in Go. The differential scenarios match Cordis 4.1.0. `-race`, leak and fuzz tests pass. The package imports the standard library only |
| S1 Loader and boot | `loader` (entries, groups, isolate, intercept, update, volatile fields), the include and patch code, `boot` (layers, dump with provenance, schema dump, admission stub, required audit, shutdown), the catalog and a minimal distribution builder, `restart-policy`, `reload` | `aiops --profile dev --dump` prints a composed tree. The failure matrix passes on the built binary with fault plugins. Live recomposition passes: toggle, config change, volatile change, insert, remove, rejected patch, rollback |
| S2 First definitions | Infrastructure definitions with fakes and conformance; their PostgreSQL and S3 providers; `http-server`; `metrics`, `tracer`, `clock` | Every provider and every fake passes its suite. Every registry passes the dispose test. `aiops-only` composes without `aiops-stack` |
| S3 Policy spine | `tenant-directory`, `identity`, `audit-chain`, `approval`, `change-hub`, `credential-lease`, `pii-pseudonym`, the registry owners with their guards, sealed keys | The policy-bypass suite passes on the built binary, before any feature exists to bypass |
| S4 Other runtimes and install | The remote fiber protocol, the `tools` adapter, the Go remote SDK, one Wasm contribution, `plugin-manager`, `plugin-inventory`, the compatibility gate, OCI fetch, signing, the index format | A plugin written outside the repository, in another language, is installed into a running `dev` profile with one command, passes `plugin check`, serves a call, and is removed leaving nothing |
| S5 Tenancy | Tenant scopes and intercept overlays, `tenant-trees`, `sidecars` with a fake and the static provider | 300 tenants on one process: the cost table of Q10 measured; one tenant's overlay changes nothing for another |
| S6 Telemetry base | `auth`, `gateway-renderer`, `fleet` and its templates, the host installer | The P0 exit of §7.2: a VM ships four signals through the gateway and appears in inventory; the token endpoint holds its target |
| S7 Incident loop | `signal-core`, `correlate`, `casefile`, `investigations` and `investigator-holmes`, `tools` and the MCP facade, `llm-gateway`, `notifications`, `chat`, `paging`, `remediation` and the Kubernetes executor, the pillar rows | The demo of §7.1 with its host leg |
| S8 Hosted | Role gating in bundles, `reload-generation`, `sidecars-kubernetes`, `kek-kms`, limits, dashboards, cohorts | The P2 exit of §7.2 |

What this costs, plainly. Steps S0 to S5 are platform work that the current plan either skips or defers to P3, and they come before the first alert is ingested. The 56 definitions each need a fake and a conformance suite. The kernel is a concurrency-critical runtime that we own. In return, the third-party exit test of P3 is passed at S4, and every row of S6 to S8 is written against definitions that already have fakes, so each is testable alone from its first commit.

A standing rule from S5 on, enforced in CI: no new package under `kernel/` or `boot/`, and no growth past their line budgets, without an ADR.

### Edits this proposal would make to the record

| Where | Edit |
|---|---|
| §1 principle 3 | Replaced: a definition exists when a consumer does; definition, provider and consumer must all exist |
| §1 principle 5 | Reworded as in Q7 |
| §2.1 row 1 | Rewritten: the kernel is the Cordis port; three runtimes; the catalog; no `init()` registry |
| §4 | Becomes the definition catalogue of Q5. "Packages, not points" and "kernel rows that are not extension points" are deleted. The lifecycle `Validate`, `Provision`, `Start`, `Health`, `Shutdown` is replaced by `Apply` and effects |
| §6.1 | Row format of Q9; layers 1 to 4 reload live; `roles`; layer 5 as intercept; required rows as row data |
| §6.2 | `aiops-core` added; rows as in Q6 |
| §7.2 | Re-baselined on the steps above |
| §7.3 | Removed, because the seam now exists: the proto per point, hot reload of the row tree, `events` as a seam, `fleet.config-source`, `estate-source`, the `correlator` point, the `approval-policy` point. Their rows keep their triggers |
| 03 §2 and §8.1, 07, 08 §1 | Rewritten to this model |

---

## dsh property checklist

One row per property found in the sources. "As is" means the semantics are unchanged. "Adapted" names the change. "Not carried" gives the reason.

### Cordis core

| # | Property | Status | How, or why not |
|---|---|---|---|
| 1 | One `Context` type mediates everything; no privileged core | As is | The kernel knows four built-in services and no product |
| 2 | Context is a `Proxy`; `ctx.<service>` property access; TypeScript declaration merging | Adapted | Typed keys declared by definitions: `Key.Of(ctx)`. Go has no property interception |
| 3 | `ctx.extend(meta)`: child inherits, parent untouched | As is | |
| 4 | `ctx.isolate(name, label)`; equal labels join | As is | Plus: refused for sealed keys |
| 5 | `ctx.intercept(name, config)`; ancestors merge first | As is | Used with row ids as names to carry the tenant overlay |
| 6 | Root installs `reflect`, `registry`, `events`, `logger` | Adapted | `events` is named `hooks`, because `events` is our outbox |
| 7 | Root fiber has uid 0; disposing it restarts the tree | As is | Used by tests and by shutdown |
| 8 | `provide(name, value, check)`, owned by the fiber, removed at unload | As is | `Key.Provide` with `Available` |
| 9 | One provider per name per realm; a second one throws | As is | Source-derived; S0 differential check required |
| 10 | `provide` metadata on a plugin is optional | Adapted | Mandatory and enforced, so the composer can analyse the graph without running code |
| 11 | `ctx.set`: the owner overwrites a value in place | Not carried | Dependants hold the old value across goroutines; replace by dispose and provide |
| 12 | `Service` base class; a class plugin is its service; callable services | Adapted | No classes: `Apply` calls `Provide`. No callable services |
| 13 | `accessor`, `mixin` | Not carried | Sugar for the proxy |
| 14 | A plugin is a function, a class or an object | Adapted | One shape, `kernel.Define(Spec[C])` |
| 15 | `inject` as a list, or a map with intercept config per service | As is | `Need`, `NeedWith` |
| 16 | `ctx.inject(deps, callback)` as a child fiber | As is | The idiom for an optional dependency |
| 17 | `ctx.get(name)`: optional read without a declaration | As is | Refused for sealed keys |
| 18 | Access without inject is an error; children inherit ancestors' injects within a realm | As is | The run-time half of the import rule |
| 19 | Six fiber states | As is | |
| 20 | Epoch from the revision and the provision uids; a swapped provider restarts consumers | As is | Source-derived; matching Go traces must be recorded at S0 |
| 21 | Epoch changes during a transition are coalesced | As is | Source-derived; S0 validation required |
| 22 | A provision is visible only while its provider is ACTIVE | As is | |
| 23 | Providers keep their implementations until dependants finish unloading | As is | Plus a quiesce step for in-flight calls, which JavaScript does not need |
| 24 | A failed fiber recovers only through `update()` | As is in the kernel | `restart-policy` adds retry with backoff as a plugin |
| 25 | A superseded load cannot fail the new target | As is | |
| 26 | `ctx.effect`: a disposer, a promise of one, or an iterable of several | Adapted | `Effect(label, setup) error`; several disposers are several effects |
| 27 | Disposers run in reverse; a fiber's top-level asynchronous disposers may overlap | Adapted | Strict last in, first out, one at a time |
| 28 | A disposer is idempotent; an effect on a disposed fiber throws `INACTIVE_EFFECT` | As is | `ErrInactiveEffect` |
| 29 | `getEffects()`: a labelled effect tree | As is | Feeds `aiopsctl tree --effects` and the leak test |
| 30 | Long stack traces that splice the loader entry into errors | Adapted | Errors are wrapped with the row id chain and the layer that defined the row |
| 31 | `fiber.update`, `internal/config`, `internal/commit`, `internal/update` | As is | |
| 32 | `rawConfig`, `effectiveConfig`, `resolveConfig` | As is | |
| 33 | Volatile config committed without a restart | As is | `kernel.Volatile[T]` |
| 34 | Config validated through Standard Schema, synchronously | Adapted | JSON Schema 2020-12 derived from the Go config type, plus `Validate` |
| 35 | `emit`, `parallel`, `serial`, `bail`, `waterfall` | Adapted | `Event`, `Waterfall`, `Gate`; `bail` and `serial` have no consumer |
| 36 | Waterfall: outermost first; no `next()` vetoes; `next()` twice throws | As is | |
| 37 | Listener order by registration, with `prepend` | Not carried | Not deterministic under parallel, availability-driven activation. Declared phase, then row id |
| 38 | Any plugin may dispatch any event | Not carried | `OwnedBy`: only the provider of the owning key dispatches |
| 39 | `Context.filter`: listeners scoped to a subtree | As is | A tenant tree's listeners see that tenant's dispatches |
| 40 | `internal/plugin`, `internal/status`, `internal/service`, `internal/get`, `internal/set`, `internal/listener`, `internal/dispatch` | Adapted | The first three are carried; the last four exist to hook the proxy and the untyped bus |
| 41 | Run to completion on one thread | Not carried | One graph lock, one transition goroutine per fiber, lock-free call path |
| 42 | Logger as a service with exporter plugins | Adapted | `log/slog` front; exporters are plugins |

### Loader, include, group

| # | Property | Status | How, or why not |
|---|---|---|---|
| 43 | The loader is a plugin that owns an entry tree | As is | |
| 44 | Entry fields `id`, `name`, `config`, `group`, `disabled`, `inject`, `intercept`, `isolate` | Adapted | `name` is `plugin` (the catalog key), `disabled` is `enabled`; `roles`, `grants` and `required` added; `intercept` is written by the tenant layer only |
| 45 | Modules imported by specifier at run time; resolver hooks | Not carried | Go cannot load code. The catalog is compiled; remote and Wasm plugins are fetched |
| 46 | `!!js` expressions in `config` and `disabled` | Not carried | Arbitrary code in configuration of a multi-tenant server. Closed replacements: `roles`, row `inject`, references |
| 47 | Row order carries no load semantics | As is | |
| 48 | `cordis:group`: nesting, `a:b` ids, a disabled group stops its children | As is | `core:group` |
| 49 | Group `isolate`: `true` is a realm private to the entry, a label is shared | As is | |
| 50 | `cordis:include`: a file-backed tree with patches | As is | `core:include` |
| 51 | Include writes updates back and reconciles a journal with file edits | Adapted | Only the plugin manager writes, and only the profile layer. No general write-back |
| 52 | `Entry.update` diffs options and restarts or commits volatile values | As is | |
| 53 | Entries move between groups and trees | Not carried | No consumer; a move is a remove and an insert |
| 54 | `loader.await()`; `Loader.Intercept{await}` keeps dependants pending while entries load | As is | Used by `readiness` |
| 55 | A plugin that disposes itself is persisted as `disabled: true` | Adapted | Marked disabled in memory and reported; not written to a file |

### dsh composition, launcher and operations

| # | Property | Status | How, or why not |
|---|---|---|---|
| 56 | A profile is an ordered list of bundles plus a user patch over an empty root | As is | §6.1 |
| 57 | A bundle is a data-only patch list plus code; only direct bundles contribute | As is | |
| 58 | Layer order: bundles, profile, home, `--patch`, launcher-owned switch | As is | Plus the tenant layer, as intercept |
| 59 | A patch targets a row by id and replaces its whole config; `insert`; the name is an assertion; an unknown id warns; an empty file fails boot | As is | |
| 60 | `--dump-default-config` with provenance comments; `--dump-config-schema` | As is | |
| 61 | Apps are profiles; one launcher; no per-app binary | As is | Roles are a compose-time row gate |
| 62 | A startup plugin parses leftover arguments and provides a service that rows inject | Adapted | Kept for `cmdline`; a role is `roles` on the row, because it is static per process |
| 63 | `prepare` provides `profileContext`, the command line, `appExit` and `ready` before any row mounts | As is | Plus `catalog` |
| 64 | Ready when the root is ACTIVE and the loader exists; five-second escalating shutdown | Adapted | Ready when the required rows are ACTIVE; a longer drain |
| 65 | Profile templates, `--from-default-profile`, `sanitizeProfile` | As is | `aiopsctl profile init` and `profile sanitize` |
| 66 | A bundle may declare `platforms` and is skipped elsewhere | Adapted | Needed only for agent bundles (Windows) |
| 67 | First-party packages released in lockstep with exact pins | As is | Within one distribution |
| 68 | `engines.dsh`, else peer ranges; exemptions for exact pairs; admission only at composition boundaries; denied rows detached; denied bundles skipped | Adapted | The same shape; "neither declared" denies for third parties |
| 69 | Install with pnpm into the profile; registry fallback; build-script approval; snapshot and restore | Adapted | OCI artifacts for remote and Wasm plugins, with snapshot and restore. In-process code is a build |
| 70 | Module hot replacement (`dsh-hmr`) | Not carried | Go cannot replace code in a running process |
| 71 | One queue serialises reloads and manager writes; installs run outside it | As is | The `reload` lane |
| 72 | An invalid live patch is rejected and the running configuration kept | As is | |
| 73 | A live reload does not repeat the required audit and does not roll back | Not carried | A server rolls back when a required row is lost |
| 74 | The plugin manager is a plugin; it cannot disable itself; a toggle records only departures; enabling a bundle appends | As is | |
| 75 | `plugin_manager` as an agent tool | Not carried | A model must not change the composition of a multi-tenant server |
| 76 | A global required-id list; a disabled required entry is ignored | Adapted | `required` is row data; a required row with no enabled row stops boot |
| 77 | Best-effort startup with one grouped diagnostic; the failure matrix tested on the built CLI | As is | |
| 78 | A detached rejection or uncaught exception is fatal | Adapted | A tracked goroutine fails its fiber; an untracked panic ends the process |
| 79 | Definition, provider, consumer; "depend on definitions, never providers"; a constraints check | As is | Enforced by the Go import graph |
| 80 | Singular keys for engines, plural for registries | As is | |
| 81 | Monotonic guards after the extensible tool hook | As is | `kernel.Gate`, used for every policy decision |
| 82 | Approval answerers are waterfall listeners that may answer without `next` | Not carried | A decision that grants is never a waterfall here |
| 83 | The approval service fails closed when no answerer exists; consumers fail closed when it is absent | As is, and stronger | Consumers inject it, so they do not exist without it |
| 84 | `dsh-scope`: scoped contributions, nearest wins, disposal by scope | As is | Tenant-tagged contributions in shared registries |
| 85 | Agent presets: one nested loader tree per preset | As is | Tenant trees |
| 86 | "These checks are not a sandbox"; in-process plugins are trusted | As is | Unreviewed code runs remote or in Wasm |
| 87 | Bare-context unit mount; the dispose test for every registry | As is | `kerneltest`, `LeaveNothing` |
| 88 | Product-visible plugins need a real-composition test; mock only the non-deterministic boundary; assertions re-read the world | As is | |
| 89 | Test kits: prerequisite services mounted for a test, replay as a plugin, loader smoke, a fault server | As is | Fakes beside definitions, `llm-provider-replay`, `loader-smoke`, fault plugins |
| 90 | Per-file 100 % coverage gate | Adapted | For `kernel`, `boot` and the loader only |
| 91 | Localised plugin metadata and icons | Adapted | Title and description in the manifest, in English; no localisation until a consumer needs it |
| 92 | A browser-side Cordis tree and generated RPC to it | Not carried | The UI is dashboards, a dashboard app and Slack |
| 93 | A sandbox for agent tools and permission presets | Not carried | There is no shell or file tool; the action policy is the approval tiers of §2.2 |
| 94 | "Model-visible means logged" | As is | "Operator-visible means logged" (principle 8) |
| 95 | Sealed keys and grants that only shrink | Added | Not a dsh property. A multi-tenant server with third-party rows needs it; dsh trusts every plugin of one user |

---

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
