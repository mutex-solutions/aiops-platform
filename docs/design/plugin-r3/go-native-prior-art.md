# Plugin system, round 3: Go-native prior art

Status: proposal, one of four independent drafts. Date: 2026-11-10.

Lens: map every dsh and Cordis property onto the Go mechanism a contributor to this stack already knows (OpenTelemetry Collector, Caddy, Alloy, fx and dig, Backstage's backend system, Dapr, go-plugin, the standard library), and keep every property the owner asked for. Where Go has no honest equivalent, say so.

How to read. Section 0 is the position and the prior-art map. Sections 1 to 16 answer Q1 to Q16 in the common order. Section 17 is the dsh property checklist. Section 18 lists what this proposal changes in the record. Section 19 lists risks and open questions.

Citations. "00 section n" is `docs/architecture/00-decisions.md`; 03, 07 and 08 are the documents beside it. "dsh-arch section n" and "plugin-arch section n" are the two research notes under `docs/research/`. Cordis source is cited as `cordis/src/<file>`. The research note read cordis 4.0.5 under dsh 0.2.1-rc.2; the source inspection used dsh 0.2.2-rc.1 with cordis 4.1.0 and loader 1.1.0, and every Cordis statement below was re-read in that source today. Go sources fetched today are listed at the end; anything cited from memory is marked there.

Rules obeyed. No name in this proposal contains a mark of the observability stack (00 section 8.1). Import paths (`aiops.dev/...`) are placeholders.

---

## 0. Position

1. **Go kernel, a port of the published Cordis model with upstream attribution.** The Cordis core is 2,811 lines of TypeScript and the loader 1,224. What dsh gets from it is four ideas: one context tree, services by name with declared needs, fibers that activate and deactivate from service availability, and effects that unwind. All four fit in a Go module under 4,000 lines with no dependency outside the standard library.
2. **The kernel has no feature code.** Store, jobs, events, HTTP, tenancy, identity, approval, audit, the change ledger, leases, the PII scrub, the LLM gateway, fleet, the plugin manager and the reloader are plugins. The launcher library (`boot`) composes layers, runs the compatibility gate and applies the failure policy, as `dsh-app-boot` does.
3. **Activation is reactive.** There is no boot order. A fiber loads when every service it needs is provided and ready, unloads when one vanishes, and reloads when a provider is replaced. In Go this is one reconciler goroutine over a deduplicating work queue, the shape every Kubernetes controller author knows.
4. **Every registration is an effect of a context.** A definition's registration call takes the caller's context, so the kernel owns the disposer. There is no `Shutdown` method to forget. A generated test mounts, disposes and remounts every plugin and fails on any residue: registration, goroutine, metric or file descriptor.
5. **Every service has a definition package, always separate from its providers.** In Go the split costs one interface file and makes the rule "plugins import definitions, never plugins" a compiler and linter fact. The definition exists for the consumer's test, not for a second provider; that is the honest replacement for principle 3.
6. **Policy stays fail closed by construction, as plugins.** The dispatcher of every effectful operation needs the policy services, so without them the capability does not exist. Policy keys are sealed: one provider, named by the distribution, not patchable by a profile layer. dsh does the same: approval fails closed when absent, and a monotonic guard runs after the reorderable hook chain.
7. **Tenant is a child context.** Root-scoped plugins run once; tenant-scoped plugins run once per tenant that enables them, under a tenant node that provides tenant-bound views of the store, secrets, settings and audit. A tenant subtree is a cache: it mounts on first use and unloads when idle, because every trigger is durable or request-driven.
8. **Live recomposition is carried, code reload is not.** The unit of reload is a row. One serialized queue applies a validated generation and rolls back to the last good one if a required row is lost. Go cannot replace code in a running process, so a code change is a rolling restart for in-process plugins and a process restart for remote ones.
9. **Installing through plugin interfaces has two honest forms.** A remote plugin is a signed OCI artifact added to a profile with no rebuild. An in-process plugin needs the distribution builder, because Go links statically. Runtime install of in-process code is the one dsh property Go cannot carry.
10. **The model is proven before any feature.** Kernel, loader, builder, both runtimes, reload and the failure matrix run on a toy distribution first. After that, the only place feature code can live is a plugin, because `cmd/*/main.go` is generated.

### 0.1 Prior-art map

| dsh or Cordis property | Go mechanism a contributor knows | What we take | What the Go mechanism lacks |
|---|---|---|---|
| Plugin registers itself | Caddy `caddy.RegisterModule` and Alloy `component.Register` from `init()`; both panic on a bad registration | `kernel.Register` from `init()` | nothing |
| Named, typed services | Backstage `createServiceRef<T>({id, scope})`; Alloy `service.Definition{Name, DependsOn, Stability}` | `kernel.Key[T]` in a definition package | both resolve once at start |
| `inject` gates activation | OTel `extensioncapabilities.Dependent.Dependencies()`, Alloy `DependsOn`, fx constructor parameters | `Need` and `Want` | all are a static start order; none reacts when a provider vanishes |
| Epoch-driven activation | Kubernetes controller-runtime: level-triggered reconcile over a deduplicating work queue | one reconciler goroutine | nothing |
| Context tree and scopes | dig `Scope` (a child inherits the parent's providers; the child's changes do not propagate up); Backstage `root` and `plugin` scope; Alloy `Host.NewController(id)` for a service's own nested components | `Extend`, `Isolate`, root and tenant scope | dig cannot remove a provider |
| Effects, reverse disposal | `t.Cleanup` (last in, first out), `defer`, Caddy `ctx.OnCancel` and `CleanerUpper`, fx `Lifecycle` hooks | `c.Effect`, `c.Defer`, `c.Go` | Caddy and fx unwind a whole config, not one plugin |
| A plugin leaves nothing behind | OTel `mdatagen` generates `TestComponentLifecycle` and `goleak.VerifyTestMain` for every component; Caddy gives each config context its own Prometheus registry | generated cycle test; fiber-scoped metric registry | nothing |
| Waterfall hooks, veto by not calling next | `func(next http.Handler) http.Handler`; gRPC and connect interceptors; Kubernetes admission (mutating, then validating, `failurePolicy: Fail`) | `kernel.Hook`, deny-only guards | nothing |
| Config validated before start | Caddy `Validator`, OTel `Config.Validate`, Alloy zero-value arguments | typed config struct, schema, `Validate` | nothing |
| Config that reads a launcher fact | OTel confmap providers, `${env:NAME}` and `${file:path}` | `${scheme:value}` references | replaces `!!js`, on purpose |
| Live recomposition | Alloy: per-component `Update` and graph re-evaluation. Caddy: the new config is fully provisioned, then swapped, then the old one is cleaned up, with the old config restored on error. OTel Collector: the service is shut down and rebuilt, with a receiver-only partial reload behind a gate | row diff, validate first, last-good rollback | none does per-plugin dependency-driven restart |
| Bundles and distributions | OTel Collector Builder (`builder-config.yaml`, strict versioning), xcaddy; blank imports | a bundle is a Go package that embeds its patch and blank-imports its plugins | no runtime install |
| Compatibility gate | ocb strict versioning, Go minimal version selection, go-plugin `VersionedPlugins` | gate at build, install, boot and recomposition | nothing |
| Out-of-process plugins | Dapr pluggable components (the user starts the process; a Unix socket per component; gRPC reflection finds the implemented APIs). go-plugin (the host launches the plugin process; handshake, checksum, automatic mTLS, a broker for callbacks, reattach) | `remote.<definition>` adapters and two runners | nothing |
| Restart on failure | Kubernetes crash-loop backoff; suture (Erlang-style supervisor trees for Go) | backoff and a restart budget in `boot` | Cordis has none: a failed fiber waits for `update()` |
| Fakes and conformance beside the contract | `testing/fstest.TestFS`, `testing/slogtest`, `net/http/httptest`; Backstage `startTestBackend` and `mockServices` | `def/<x>/<x>test`: a fake and `TestProvider` | nothing |
| Deterministic lifecycle tests | `testing/synctest` (fake clock, `Wait` until every goroutine is durably blocked) | `kerneltest.Run` | nothing |
| Import boundary | Go `internal/` packages, separate modules, golangci-lint `depguard` | three modules, `internal/`, lint, one architecture test | nothing |
| Plugin index | krew-index: a git repository of manifests with a digest per platform | git index, OCI artifacts, cosign | nothing |
| Commands contributed by plugins | Caddy `caddycmd.RegisterCommand` | `cli` definition | nothing |

Reading the map: Go has a proven mechanism for every dsh property except two. No Go project does availability-driven activation with per-plugin unwinding at run time, so the reconciler and the effect ledger are ours to write and to prove. And no Go project loads in-process code at run time, which section 11 treats as not carried.

---

## 1. Q1. Kernel runtime and language

| Option | For | Against | Verdict |
|---|---|---|---|
| A. TypeScript on Cordis, the literal dsh kernel | Exact semantics for free, including code reload, runtime install and the proxy context. Upstream fixes arrive by upgrade. | Every Go asset the record relies on becomes out of process or is replaced: River (00 section 2.1 row 9), the connect-go fleet API, client-go executors, slack-go, pgx. dsh is a developer preview that announces breaking changes: the version the research note read two weeks ago (0.2.1-rc.2, cordis 4.0.5) is at least three releases behind the one installed today (0.2.2-rc.1, cordis 4.1.0). A single event loop on the 250 ms ingest ack path and the 300 req/s token endpoint is workable but gives up preemption per request. The contributors this stack attracts write Go. | rejected |
| B. Go port of the Cordis model | Small. Keeps one language with the data plane. Every mechanism except two has Go prior art (section 0.1). In-process calls between first-party plugins stay function calls. | Correctness must be earned again. Teardown ordering and re-entrancy are subtle; the comments in `cordis/src/fiber.ts` record several repaired races. No code reload, no runtime install of in-process code. | chosen |
| C. Go with a static graph (fx, or the current registry with a fixed order) | Proven and simple. | It is not the dsh model: no reactive activation, no per-plugin unwind at run time, no scopes. The owner withdrew the argument that justified it. | rejected by the owner's direction |

Why not fx as the kernel. fx builds its graph once in `fx.New`. A provider cannot be removed or replaced while the application runs, a module cannot be stopped alone, and reflection-based injection hides the dependency list from the manifest. We take its idea of lifecycle hooks and its `fxtest` habit, not the library. dig's `Scope` has the right inheritance rule for a context tree and the same limitation.

Why not Caddy's module loader as the kernel. Caddy's tree is ownership by configuration: a host module loads its guests with `ctx.LoadModule`. `ctx.App(name)` loads an app on first lookup, which is lazy, not reactive: nothing stops when a dependency goes away. We take `Provision` before `Validate` before use, `CleanerUpper`, the per-context metric registry and `RegisterCommand`.

**What the kernel contains.** Module `aiops.dev/kernel`, standard library only.

| Part | Cordis source | Content |
|---|---|---|
| Context tree | `context.ts` | Immutable nodes; `Extend`, `Isolate`, `Intercept` |
| Keys and service registry | `reflect.ts`, `service.ts` | `Key[T]`, `Provide`, `Get`, `Lookup`, readiness predicate, sealed and scope attributes |
| Fibers and the reconciler | `fiber.ts` | Six states, epoch, inertia, dependents retained through teardown, `Update`, `Restart`, `Await` |
| Effect ledger | `fiber.ts` `effect` | `Effect`, `Defer`, `Go`, labelled effect tree for diagnostics, `ErrInactive` |
| Hooks and events | `events.ts` | Typed waterfall hooks; `emit`, `parallel`, `serial`, `bail`; scope filter; lifecycle events |
| Plugin registry | `registry.ts` | Descriptors registered from `init()`; `Plugin`, `When` |
| Config pipeline | `resolveConfig`, `internal/config` | Decode, resolve references through a hook, validate; candidate resolution without publishing; volatile fields |
| Root logger | `logger.ts` | `log/slog` handler with fiber attributes |

**What sits beside it and is not a plugin.** The `boot` library, as in dsh: layer composition and dumps (pure functions shared with the CLI), the compatibility gate, the launcher (it provides `profile`, `startup` and `packages` before any row mounts, the dsh `prepare` hook), the mount of the loader, the activation audit and failure policy, the restart supervisor, signal handling. These must exist before the first row and must not be patchable by a row.

**What the kernel must not contain.** Any import outside the standard library. Any word of the product domain: tenant, store, HTTP, job, approval, audit, LLM. Any policy. Any provider. Enforced by a `go.mod` with no `require` line and one CI test that fails on a new import.

---

## 2. Q2. The context model

**Tree.** A context is an immutable node. Three calls make a child without touching the parent, as in Cordis.

| Call | Effect | Used for |
|---|---|---|
| `c.Extend()` | The child inherits everything | Every fiber gets one |
| `c.Isolate(key, label)` | Below the child, `key` resolves in a new scope. The same label joins two scopes; an empty label is private | Tenant-bound services (section 10); a group row that carries a provider and its consumers together |
| `c.Intercept(key, cfg)` | Plugins below see `cfg` merged into that service's per-plugin config, ancestors first | A preset that changes a service option for its subtree, for example a statement timeout |

**Services.** A definition package owns a typed key. Go has no property proxy and no generic methods, so lookups are generic functions and each definition exports a one-line accessor.

```go
package kernel

type Key[T any] struct{ /* name, attributes */ }

// NewKey is called once, in the definition package.
// Attributes: Sealed() (one provider per tree, no Isolate, provider named by the distribution),
// TenantBound() (provided inside a tenant scope only), RootOnly() (not injectable by tenant-scoped plugins).
func NewKey[T any](name string, attrs ...KeyAttr) Key[T]

func Need[T any](k Key[T]) Dep // required: gates activation and joins the epoch
func Want[T any](k Key[T]) Dep // optional: never gates; read with Lookup at call time

// Provide registers impl for k, owned by c's fiber. It is an effect: the service is unregistered when
// the fiber unloads, after every dependent has finished unloading. ReadyWhen(fn) lets a provider be
// present but unavailable. Provide fails for a key the plugin did not declare in Provides.
func Provide[T any](c *Context, k Key[T], impl T, opts ...ProvideOpt) error

func Get[T any](c *Context, k Key[T]) T            // panics with ErrNotInjected if k is not a declared Need
func Lookup[T any](c *Context, k Key[T]) (T, bool) // for a declared Want; false when absent

type Spec[C any] struct {
	Name     string   // row type: "<definition>.<impl>" or a bare name
	Scope    Scope    // Root or Tenant
	Inject   []Dep
	Provides []AnyKey // every key Apply may provide
	Hooks    []HookUse // every hook Apply may listen on, with its stage
	Validate func(cfg *C) error                // pure: no I/O, no context
	Apply    func(c *Context, cfg *C) error    // the only entry point; everything it registers is an effect
}

func Define[C any](s Spec[C]) Plugin // derives the JSON Schema of C and a strict decoder
func Register(p Plugin)              // from init(); panics on a duplicate name, a bad identifier,
                                     // or a tenant-scoped plugin that injects a RootOnly key

func (c *Context) Plugin(p Plugin, cfg any) *Fiber                        // child fiber, an effect of c
func (c *Context) When(deps []Dep, fn func(*Context) error) *Fiber        // sub-fiber for an optional feature
func (c *Context) Effect(label string, fn func() (Disposer, error)) error
func (c *Context) Defer(label string, d Disposer)                         // t.Cleanup for plugins
func (c *Context) Go(label string, fn func(context.Context) error)        // a goroutine as an effect
func (c *Context) Std() context.Context                                   // cancelled when unloading starts

type Disposer func(context.Context) error // receives the unload deadline
```

**Declared needs.** `Get` on a key that is not in `Inject` panics, in the kernel and in every fake, like Cordis ("cannot get property without inject"). A panic for a programming error at registration or first use is the Go convention (`regexp.MustCompile`, Caddy, Alloy), and the first unit test hits it.

**Optional dependencies.** `Want` never gates. `Lookup` reads at call time, as `ctx.get(name)` does. A plugin that must react when an optional provider appears or leaves wraps that feature in `c.When(deps, fn)`, the port of `ctx.inject(deps, callback)`: a sub-fiber that loads and unloads on its own.

**Provides are declared.** This is stricter than Cordis, where `provide` is imperative. A static list lets `aiopsctl profile render --check` report an unresolved need, a duplicate provider or a cycle before anything boots, and lets the dump show who provides what. Backstage and fx detect the same errors at start.

**How a plugin is declared.** Code is the source of truth for needs, provides, hooks and the config schema. A manifest carries what code cannot.

| `plugin.yaml` field | Source | Checked |
|---|---|---|
| `id`, `version`, `license`, `stability`, `description` | written by the author | `plugin check`, build |
| `kernelApi` | written: a semver range of the kernel module | gate (section 12) |
| `scope`, `inject`, `provides`, `hooks`, `jobs`, `migrations`, `definitions` (name and version of each definition used), `configSchema` | generated from the Go descriptor by `plugingen`, in the manner of OTel `mdatagen`; CI fails when the committed file differs | build |
| `runtime` (`inproc`, `remote`, `wasm`), `instancing`, `capabilities` (`network`, `secrets`) | written | build, `Apply` (secrets), egress policy (network) |
| `artifact` (OCI reference, signature identity) | written by the release job | install |

**What an author writes.** A tool that reads tickets, tenant-scoped, with one optional dependency.

```go
package ticketing

type Config struct {
	BaseURL string `json:"baseUrl" jsonschema:"format=uri"`
}

var Plugin = kernel.Define(kernel.Spec[Config]{
	Name:   "tool.ticket-lookup",
	Scope:  kernel.Tenant,
	Inject: []kernel.Dep{kernel.Need(tools.Key), kernel.Need(secrets.TenantKey), kernel.Need(dial.Key), kernel.Want(estate.Key)},
	Apply: func(c *kernel.Context, cfg *Config) error {
		cl := newClient(cfg.BaseURL, dial.From(c).Client(c), secrets.From(c))
		return tools.From(c).Register(c, tools.Def{ // an effect of c: gone when this fiber unloads
			Name: "ticket-lookup", Effects: tools.Read, Params: paramSchema,
			Invoke: cl.lookup,
		})
	},
})

func init() { kernel.Register(Plugin) }
```

There is no `Start`, `Shutdown`, `Health` or `Update`. Start is `Apply`. Shutdown is the ledger. Health is a check registered with the `health` definition, itself an effect. A config change restarts the fiber, except for fields the schema marks volatile (section 9).

---

## 3. Q3. Activation

**Decision: service-availability-driven fibers, as in Cordis. The fixed boot order goes.**

**States**, carried one to one from `fiber.ts`:

| State | Meaning | Leaves when |
|---|---|---|
| `PENDING` | A need is missing or not ready | every need has a ready provider |
| `LOADING` | `Apply` is running | it returns |
| `ACTIVE` | Loaded; its provided services are visible | a need vanishes or its provider changes, a config update, a restart, a dispose |
| `FAILED` | Config or `Apply` failed | `Update`, or the supervisor's retry |
| `UNLOADING` | Dependents first, then own disposers in reverse | the ledger is empty |
| `DISPOSED` | Removed; cannot restart | never |

The current plan's `degraded` is not a state. Health is orthogonal and lives in the `health` definition.

**Epoch.** A fiber's epoch is its revision counter plus the identity of the provider of each need. A missing or unready provider makes it inactive. A changed epoch on an active fiber means unload, then load. This is `_refresh` in `fiber.ts`, and it is what makes "swap one provider and the product follows" work with no wiring in any plugin.

**Teardown order.** A provider that leaves `ACTIVE` notifies its dependents at once, and its own disposers wait until every dependent has finished unloading (`_unload` and the `dependents` set in `reflect.ts`). So a connection pool closes after the last consumer has drained, without any plugin naming an order.

**The reconciler in Go.** One goroutine owns every fiber field, so the state machine needs no lock and its order is deterministic. Plugin code never runs on it: `Apply` and disposers run on a goroutine per fiber and report back.

```go
func (k *Kernel) loop() {
	for ev := range k.inbox { // mount, dispose, update, provided, unprovided, ready-changed, load-done, unload-done
		k.handle(ev)
		if k.batchDepth > 0 {
			continue // a recomposition is being applied; evaluate once it closes
		}
		for f := range k.dirty.Drain() { // deduplicated, like a controller work queue
			want := f.epoch()            // "" when a need is missing or not ready
			switch {
			case f.busy(): // LOADING or UNLOADING; re-evaluated when the transition reports back
			case f.loaded != "" && want != f.loaded:
				f.startUnload() // waits for dependents, then runs the ledger in reverse
			case f.loaded == "" && want != "" && f.err == nil:
				f.startLoad(want)
			}
		}
	}
}
```

`kernel.Batch(fn)` holds evaluation until a set of changes is complete. `kernel.Settle(ctx)` returns when no fiber is in transition, the port of `loader.await()`.

**Failure.** `Apply` returning an error or panicking makes the fiber `FAILED`, runs whatever part of its ledger was registered, and leaves siblings alone. Cordis stops there: "a failed fiber requires an explicit `update()` to recover". A server needs more, so the supervisor in `boot` retries a failed fiber with exponential backoff and jitter (1 s doubling to 5 min) inside a restart budget (5 restarts in 10 minutes). When the budget is spent, an optional row stays `FAILED` until its config changes or an operator restarts it, and a required row ends the process (section 13). This is the crash-loop backoff every Kubernetes user has seen, and suture's failure threshold.

**Restart storms.** Four fences.

| Risk | Fence |
|---|---|
| A provider is replaced and each dependent unloads and loads twice | The replacement is applied inside one batch, so each dependent sees one epoch change |
| A provider's readiness flaps and its dependents thrash | `ReadyWhen` carries a hold time: a provider must be unready for the hold before dependents are told. Infrastructure definitions (store, jobs, events) never use `ReadyWhen`; a database blip surfaces as call errors and health, not as 200 unloads |
| One change restarts most of the tree | The restart closure of a recomposition is computed before it is applied and printed. Above a threshold (20 % of active fibers, an initial value) the reloader refuses unless forced, and hosted takes a rolling restart instead |
| A fiber crash-loops | Backoff and the restart budget above |

**Static pre-flight.** Because needs and provides are declared, `aiopsctl profile render --check` reports, with no process running, every row that can never activate (no provider composed, a cycle, a tenant-scoped row needing a root-only key). Cordis leaves such a fiber pending for ever. The check is fx's `ValidateApp` applied to a composed tree.

---

## 4. Q4. Effects and disposal

**Rule.** A registration call takes the caller's context as its first argument and returns nothing the caller must remember. The provider implements it with `c.Effect`. Cordis gets the caller's context implicitly through the proxy; in Go it is an explicit parameter, the same convention as `context.Context`.

| Registration | Call | Unwind | Go trap handled |
|---|---|---|---|
| Service | `kernel.Provide(c, key, impl)` | Unregistered after dependents unload | none |
| HTTP or RPC route | `http.From(c).Handle(c, pattern, h)` | Removed from a copy-on-write route table; in-flight requests get the unload deadline | `http.ServeMux` cannot unregister a pattern |
| Job handler | `jobs.From(c).Handle(c, kind, h)` | Handler entry removed; the kind is unowned and its jobs snooze | River's `Workers` bundle has `AddWorker` and no removal, so kinds are declared in the descriptor and registered once, and each River worker dispatches to a revocable handler table |
| Periodic job | `jobs.From(c).Every(c, spec)` | Schedule entry removed | runs on the leader only |
| Event subscription | `events.From(c).Subscribe(c, type, h)` | Subscriber marked absent; deliveries snooze; the durable cursor stays | none |
| Tool, notifier, executor, parser, investigator, verifier | `<registry>.From(c).Register(c, def)` | Removed from the scoped layer | none |
| Hook listener | `hook.On(c, stage, fn)` | Removed from the chain | none |
| Metric | `metrics.From(c).MustRegister(collectors...)` | The fiber's registerer unregisters them | `prometheus.MustRegister` on the default registry panics at the second activation; a lint forbids it in plugins |
| Goroutine, ticker, watch | `c.Go(label, fn)` | Context cancelled, then awaited | a bare `go` statement in a plugin is a lint error |
| Pool, client, listener, file | `c.Defer(label, close)` | Closed | none |
| Tenant saga step | `tenant.From(c).OnDelete(c, step)` | Removed | none |
| CLI command | `cli.From(c).Command(c, cmd)` | Removed | none |
| Child plugin, sub-fiber | `c.Plugin`, `c.When` | Disposed before the parent's own effects | none |

**Order.** Strictly last in, first out within a fiber, each disposer awaited under the unload deadline (30 s by default). Cordis awaits a fiber's top-level disposers together and runs nested ones in reverse; strict order is simpler to reason about in Go and loses nothing.

**Use after unload.** A Cordis proxy refuses access to a released injection. A Go interface value cannot be revoked. Three things replace the proxy: registering on an unloaded context returns `ErrInactive`; a fiber is not unloaded until every `c.Go` goroutine has returned; and every definition's conformance suite includes "a call after the provider is disposed returns `ErrInactive`", which providers meet by checking their own fiber context.

**Durable state is not an effect.** The paper's reversibility is about the context, not about disk. Tables, enqueued jobs, outbox rows, secrets and blobs survive an unload, as session logs and storage files do in dsh. Three rules keep that state inert and removable:

1. Every durable artifact is namespaced by plugin id: schema, job kind prefix, event subscriber name, secret scope, blob prefix.
2. Work for an absent owner waits; it is never dropped or failed. Jobs and deliveries snooze.
3. `aiopsctl plugin purge <id>` removes the namespaces, under the same approval as a tenant delete. Migrations are declared in the descriptor, so `aiops migrate` reads them without activating anything, and a plugin whose schema is behind stays `PENDING` on `migrate`.

**The proof.** Four checks, the first three generated for every plugin.

| Check | Where | Fails when |
|---|---|---|
| Cycle test | `plugingen` writes it beside every plugin, as `mdatagen` writes `TestComponentLifecycle` | After mount, dispose and remount three times on fakes, any fake's `Inventory()` differs from its baseline, the kernel ledger is not empty, a goroutine remains (`goleak`), a metric remains, or the second mount fails |
| Late registration | same test | A registration on the disposed context does anything but return `ErrInactive` |
| Registry safety | each registry definition's conformance suite | Disposing a contributing fiber leaves an entry. This is the dsh rule "every registry gets an HMR-safety test" |
| Artifact residue | `test/failure-matrix`, on the built binary | After a live disable of each optional row, `aiopsctl status --json` shows any route, handler, subscription, tool or hook still owned by it |

Every registry definition must expose `Inventory()`, the list of registrations by owning fiber. It is what the checks read, and what the plugin manager shows.

---

## 5. Q5. Service definitions

**Shape.** One Go package per definition under `def/<name>`, in the module `aiops.dev/def`. It holds the interface, the key, the value types, the errors, the hooks it declares and an `Unimplemented` base. It imports the kernel and other definitions only. Beside it, `def/<name>/<name>test` holds the fake and the conformance suite, the standard library's own layout (`fstest`, `slogtest`, `httptest`).

```go
package jobs // def/jobs

var Key = kernel.NewKey[Service]("jobs")

type Service interface {
	Handle(c *kernel.Context, kind Kind, h Handler) error // an effect of c
	Every(c *kernel.Context, s Schedule) error             // an effect of c
	Enqueue(ctx context.Context, tx store.Tx, j Spec) (ID, error)
	mustEmbedUnimplemented()
}

type Unimplemented struct{} // providers embed it, so a new method is a minor change (the gRPC convention)

func From(c *kernel.Context) Service { return kernel.Get(c, Key) }

const Version = "1.0" // announced in manifests and remote handshakes
```

**Always split, and why this departs slightly from dsh.** dsh ships some definitions together with their engine (`dsh-tools`, `dsh-session`, `dsh-schedule`) and splits others (`dsh-jobs` and `dsh-jobs-local`). We always split, for two Go reasons: an engine in the definition package would pull pgx or River into every consumer's test binary, and the split is what makes the import rule mechanical.

**The rule and its enforcement.** A plugin imports `kernel`, `def/...` and its own packages. Never another plugin, never `boot`.

| Mechanism | Stops |
|---|---|
| Three modules: `kernel`, `def`, and the product module (`boot`, `plugins`, `bundles`, `cmd`). A third-party plugin's `go.mod` requires only the first two | A third-party plugin linking a provider at all |
| `plugins/<type>/internal/...` holds the implementation; the public package exports one `Plugin` variable | Any other package reaching into a provider (compiler rule) |
| golangci-lint `depguard`: `plugins/**` may not import `plugins/**` or `boot/**`; `def/**` may not import `plugins/**`; `kernel` imports nothing | A first-party plugin importing another's public package |
| One architecture test over `go list -deps`, driven by the bundle files | A bundle's rows importing packages that only another bundle carries (the existing rule of 00 section 6.3) |

**Versioning.** Each definition has a stability label (`alpha`, `beta`, `stable`) and a `Version` constant. Additive change: embed `Unimplemented`, add the method, bump the minor. Breaking change to a stable definition: a new package `def/<name>/v2` beside the old one, plus an adapter plugin that provides the v1 key from a v2 provider, the Kubernetes habit of versions in the package path. The `def` module stays at v0 until the first set is stable. A definition change needs an ADR and its conformance suite updated in the same change.

**A definition is earned by a consumer.** Principle 3 is replaced by: a definition exists when a plugin needs the service and must be testable without the provider. The fake is not extra work; it is the test double every consumer needs anyway, kept honest because the fake must pass the same conformance suite as the real provider (`fstest.MapFS` passes `fstest.TestFS`).

**Catalogue.** "Registry" definitions accept contributions and are named in the plural; "engine" definitions are singular (the dsh naming rule). "Tenant view" means a tenant-bound key exists (section 10). "Remote" means the definition gets a remote adapter (section 11).

| Tier | Definition | Kind | Contract in one line | Tenant view | Remote |
|---|---|---|---|---|---|
| Infrastructure | `metrics`, `tracer` | engine | Fiber-scoped registerer and tracer | no | no |
| | `health` | registry | Checks; readiness and liveness | no | no |
| | `dial` | engine | HTTP clients and dialers behind the egress guard; fixture replay in tests | no | no |
| | `kek` | engine | Wraps and unwraps tenant data keys | no | no |
| | `secrets` | engine | Envelope-encrypted secrets, scope (tenant, plugin, name) | yes | callback |
| | `store.sql`, `store.kv`, `store.blob` | engine | Transactions with RLS, keyed state, blobs | yes | kv only, callback |
| | `migrate` | engine | Schema versions per plugin; gate | no | no |
| | `jobs`, `leader` | engine | Handlers, enqueue in a transaction, periodic jobs, leader | no | no |
| | `events` | engine | Durable outbox: subscribe; emit only on a transaction | no | no |
| | `limits` | engine | Tenant limits, buckets, lease rows | yes | no |
| | `http` | engine | Listeners, removable routes, the request hook | no | no |
| | `cli` | registry | Commands | no | no |
| Platform | `profile`, `startup`, `packages`, `loader` | engine | Launcher facts and the entry tree (provided by `boot`) | no | no |
| | `reload`, `pluginManager` | engine | Serialized recomposition; manage rows and bundles | no | no |
| | `tenant`, `tenants` | engine | Directory and lifecycle; tenant scopes | yes (`settings`) | no |
| | `identity`, `authz` | registry, engine | Verify a caller; bindings and permissions | no | no |
| | `tokens` | engine | Agent and service tokens, key set | no | no |
| | `audit` | engine | Append in the caller's transaction; chain; sinks | yes | callback, append only |
| | `pii` | engine | Per-tenant pseudonyms | yes | no |
| | `approval` | engine | Requests, decisions, the floor | yes | no |
| | `change` | engine | Change ledger, tokens, candidates, verdicts, check registry | yes | no |
| | `credentialLease` | engine | A write credential for one valid token | yes | callback |
| | `llm`, `llmProviders` | engine, registry | Budgeted completions and endpoint; upstream adapters | yes | providers |
| | `sidecars` | engine | Endpoint of an upstream instance for (tenant, kind) | yes | no |
| | `fleet`, `fleetSources` | engine, registry | Agent inventory and config; template fragments | no | no |
| | `gatewayConfig` | engine | Renders and publishes the tenant gateway config | no | no |
| | `datasource.metrics`, `datasource.logs` | engine | Query clients | yes | no |
| Domain | `signals` | registry | Ingest core; payload parsers | yes | parsers |
| | `correlator` | engine | Correlation key and priority | no | no |
| | `casefile` | engine | Incidents, timeline, steps, context pack, answer check | yes | no |
| | `investigators` | registry | Runs, slots, step log; investigator adapters | yes | adapters |
| | `tools` | registry | Tool definitions, dispatcher, the MCP facade | yes | tools |
| | `remediation` | registry | Catalogue and dispatcher; executors | yes | executors |
| | `notifiers` | registry | Dispatch, digest, pacing; notifier adapters | yes | adapters |
| | `oncall` | registry | Page, ack, status; provider adapters | yes | adapters |
| | `chat`, `chatCommands` | registry | Inbound envelope and replies; commands | no | no |
| | `pillars` | registry | Persona, toolset, routing per pillar | yes | no |
| | `toolsetBindings` | engine | Endpoints and credentials for the investigator's native toolsets | yes | no |
| | `compliance` | engine | Findings, controls, score | yes | no |
| | `estate` | engine and registry | Advisory questions; readers | yes | readers |

About fifty definitions. This follows dsh's composed row-tree model; machine-specific upstream composition counts are not evidence for this proposal.

Two host services of the current plan disappear rather than move: `lifecycle` (the ledger replaces it) and `discovery` (`sidecars` covers upstream endpoints and the remote runner covers plugin endpoints). There is no `clock` definition: production code uses `time`, and `testing/synctest` gives tests a fake clock.

---

## 6. Q6. What becomes a plugin

Every kernel row and package of 00 section 4 becomes a row with a definition, except `plugin-host`, which is the kernel itself, and `loader`, which `boot` mounts before any bundle is read (dsh mounts its Loader the same way). The three row classes of 07 section 3 collapse into one: a pillar row is an instance of the plugin type `pillar`, whose `Apply` registers the row's config with `pillars`, as a dsh agent preset is ordinary rows.

Scope: R is root, T is tenant. Needs in brackets are optional.

**Provided by the launcher before any row:** `profile`, `startup`, `packages`, `loader`.

**Bundle `aiops-base`, infrastructure**

| Row id (type) | Provides | Needs | Scope |
|---|---|---|---|
| `reload` | `reload` | `loader`, `profile` | R |
| `plugin-manager` | `pluginManager` | `loader`, `profile`, `packages` (`reload`, `authz`, `audit`) | R |
| `telemetry` (`telemetry.prometheus-otlp`) | `metrics`, `tracer` | none | R |
| `health` | `health` | `http` | R |
| `config-refs` | resolvers on `config/resolve` for `env`, `file`, `secret`, `flag` | `startup` (`secrets`) | R |
| `dial` (`dial.guarded`) | `dial` | none | R |
| `kek` (`kek.local-file`) | `kek` | none | R |
| `store` (`store.postgres`) | `store.sql`, `store.kv` | `metrics` | R |
| `blob` (`blob.s3`) | `store.blob` | `dial` | R |
| `migrate` | `migrate` | `store.sql`, `packages` | R |
| `secrets` (`secrets.envelope`) | `secrets` | `store.sql`, `kek`, `migrate` | R |
| `jobs` (`jobs.river`) | `jobs`, `leader` | `store.sql`, `migrate`, `packages` (`limits`) | R |
| `events` (`events.outbox`) | `events` | `store.sql`, `jobs` | R |
| `limits` (`limits.postgres`) | `limits` | `store.sql`, `store.kv` | R |
| `api-server` (`http.server`) | `http` | `metrics`, `startup` (`identity`) | R |

**Bundle `aiops-base`, platform and domain**

| Row id (type) | Provides | Needs | Scope |
|---|---|---|---|
| `tenant` (`tenant.directory`) | `tenant`, `tenants`, and inside each tenant scope the bound views | `store.sql`, `secrets`, `jobs`, `events`, `limits`, `loader` | R |
| `identity` (`identity.chain`) | `identity` | `http` | R |
| `identity.oidc`, `identity.service-token`, `identity.slack-link` | verifier contributions | `identity` (`tokens`) | R |
| `authz` (`authz.bindings`) | `authz` | `store.sql`, `tenant`, `identity` | R |
| `audit` (`audit.chain`) | `audit`, sink registry | `store.sql`, `jobs`, `leader` | R |
| `pii` (`pii.pseudonym`) | `pii` | `secrets`, `tenant` | R |
| `approval` (`approval.tiered`) | `approval`; the tier table is its config | `store.sql`, `audit`, `identity`, `authz`, `events`, `jobs` (`estate`) | R |
| `approval-surface.api` | routes | `approval`, `http` | R |
| `change` (`change.ledger`), the former `change-hub` | `change` with its check registry | `store.sql`, `audit`, `events`, `jobs`, `kek` (`estate`) | R |
| `verify.alert-cleared`, `verify.read-back` | check contributions (the former package) | `change` (`casefile`) | R |
| `credential-lease` (`lease.inprocess`) | `credentialLease` | `change`, `secrets`, `audit` | R |
| `llm` (`llm.gateway`) | `llm`, `llmProviders`, route `/v1/llm` | `http`, `pii`, `limits`, `secrets`, `tenant`, `store.sql` | R |
| `llm-provider.openai-compatible`, `llm-provider.replay` | provider contributions | `llmProviders`, `dial` | R |
| `signals` (`signals.ingest`) | `signals` | `http`, `store.sql`, `jobs`, `events`, `limits`, `tenant` | R |
| `signal-ingest.alert-webhook`, `signal-ingest.generic-json` | parser contributions | `signals` | T |
| `correlator` (`correlate.rules`), the former package | `correlator` | none (`estate`) | R |
| `storm-mode`, the former ingest-path code | listener on `signals/correlate` | `signals`, `limits` | R |
| `casefile` (`casefile.postgres`) | `casefile`; routes | `store.sql`, `store.blob`, `events`, `jobs`, `audit`, `correlator` (`change`, `estate`) | R |
| `notify` (`notify.dispatch`) | `notifiers` | `store.kv`, `jobs`, `events`, `pii` | R |
| `notifier.email`, `notifier.webhook` | adapter contributions | `notifiers`, `dial`, `secrets` | T |
| `tools` (`tools.registry`) | `tools`, route `/v1/tools` | `http`, `pii`, `audit`, `tenant`, `llm` | R |
| `investigate` (`investigate.runner`) | `investigators` | `jobs`, `casefile`, `tools`, `llm`, `limits`, `events` | R |
| `remediation` (`remediation.dispatch`) | `remediation` | `approval`, `change`, `credentialLease`, `audit`, `jobs`, `casefile`, `events` | R |
| `oncall` (`oncall.dispatch`) | `oncall` | `jobs`, `casefile`, `events`, `store.kv` | R |
| `pillars` (`pillars.registry`), the former `router` | `pillars` | `tenant` | R |
| `chat` (`chat.router`) | `chat`, `chatCommands` | `identity`, `tenant`, `casefile`, `pillars` (`approval`) | R |
| `stream` (`stream.sse`) | route `/v1/stream` | `http`, `events`, `identity` | R |
| `admin` (`admin.api`) | routes `/v1/admin` | `http`, `authz`, `jobs`, `tenant`, `pluginManager` | R |
| `estate` (`estate.projection`), second wave | `estate`, reader registry | `store.sql`, `datasource.metrics`, `jobs` | R |

**Other bundles**

| Bundle | Row id (type) | Provides | Needs | Scope |
|---|---|---|---|---|
| `aiops-datasources` | `toolset-bindings` | `toolsetBindings` | `secrets` (`tokens`) | T |
| | `datasource-metrics` (`datasource.metrics.http`); later `datasource-logs` | `datasource.metrics`, `datasource.logs` | `dial`, `secrets` (`tokens`) | R |
| `aiops-stack` | `auth` (`tokens.issuer`) | `tokens`; enrol, token and key-set routes | `store.sql`, `kek`, `http`, `limits` | R |
| | `gateway` (`gateway.render`) | `gatewayConfig` | `tokens`, `jobs`, `tenant` | R |
| | `fleet` (`fleet.core`) | `fleet`, `fleetSources` | `store.sql`, `jobs`, `tenant` | R |
| | `fleet-agentapi` (`fleet.agentapi`) | collector API routes | `fleet`, `tokens`, `http` | R |
| | `stack-provision` | tenant saga steps: backend provisioning, rule load | `tenant`, `jobs`, `dial` | R |
| | `limits-renderer.metrics`, `.logs`, `.traces`, `.profiles`, later | renderer contributions | `limits`, `jobs` | R |
| | `annotator`; later `dashboard-sync` | listeners and a reconciler | `events`, `casefile`, `dial` (`tenant`, `tokens`) | R |
| `aiops-chatops-slack` | `chat-surface.slack` | surface contribution | `chat`, `http`, `secrets`, `store.kv` | R |
| | `notifier.slack` | adapter contribution | `notifiers`, `secrets` | T |
| | `approval-surface.slack` | chat handlers | `approval`, `chat` | R |
| `aiops-oncall-goalert` | `on-call-provider.goalert` | provider contribution | `oncall`, `sidecars`, `secrets` | T |
| | `tool.oncall-who` | tool contribution | `tools`, `oncall` | T |
| `aiops-pillars-mvp` | `pillar-incident`, `pillar-change`, `pillar-compliance` (type `pillar`) | pillar contributions | `pillars`, `tools`, `signals` | T |
| | `investigator.holmes` | adapter contribution | `investigators`, `sidecars`, `toolsetBindings`, `llm` | T |
| | `tool.change-lookup`, `tool.runbook-fetch`; later `tool.estate-lookup` | tool contributions | `tools`, `change` or `estate` | T |
| | `remediation-executor.kubernetes`; later `remediation-executor.gitops-pr` | executor contributions | `remediation`, `credentialLease`, `dial` | T |
| | `signal-ingest.gitops-webhook`, `signal-ingest.policyreport` | parser contributions | `signals` | T |
| | `compliance` (`compliance.engine`) | `compliance`; routes | `store.sql`, `signals`, `casefile`, `jobs` | R |
| `aiops-hosted` | `kek.kms` in place of `kek.local-file` | `kek` | `dial` | R |
| | `sidecars` (`sidecars.kubernetes`) | `sidecars` | `tenant`, `jobs`, `leader` | R |
| | `cohorts` | listener on `jobs/enqueue` that picks `<cohort>.<kind>` | `jobs`, `tenant` | R |
| | `billing-meters` | event listener | `events`, `store.sql` | R |
| `aiops-selfhost` | `sidecars` (`sidecars.static`) | `sidecars` | none | R |
| `aiops-ctl`, new | `cli` (`cli.root`), command rows, `api-client` | `cli` and command contributions | `startup` | R |
| `agent-base` and the other agent bundles | `fleet-source.<feature>` rows | template contributions | `fleetSources` | R |

About seventy-five rows at the first wave. Rule files, runbooks and playbooks stay bundle content.

---

## 7. Q7. Policy that must not be bypassed, as plugins

**Position.** No policy logic stays in the kernel. Principle 5 keeps its meaning and changes its wording: policy is enforced by the dispatcher of each definition, which is the only path to a contribution, and the dispatcher cannot exist without the policy services. dsh does exactly this: `dsh-user-approval` is a plugin, the tools pipeline and the sandboxed shell tool "fail closed when it is absent", and `ctx.tools.guard()` adds a monotonic check after the reorderable `tools/pre-execute` chain, so that "no later listener can turn that denial back into permission".

**Five mechanisms, each generic.**

| # | Mechanism | Where | What it guarantees |
|---|---|---|---|
| 1 | The dispatcher needs the policy | `remediation` needs `approval`, `change`, `credentialLease` and `audit`; `llm` needs `pii`; `notify` and `tools` need `pii`; every state-changing engine needs `audit` | Without the policy provider the dispatcher is `PENDING`, so no executor, model call or message can be reached. A swap or a failure of the policy unloads the dispatcher first. Absence is closed, not open |
| 2 | Contributions are reachable only through the dispatcher | Executors, tools, notifiers and investigators register with their registry and are provided under no key | No plugin can call an executor directly. An architecture test asserts that no key exposes a raw contribution |
| 3 | Sealed keys | `approval`, `audit`, `change`, `credentialLease`, `pii`, and the dispatchers `remediation`, `tools`, `llm`, `notifiers`, `signals`, `investigators` are created with `Sealed()` | One provider per tree; `Isolate` on a sealed key is refused, so no subtree can see a softer approval. The provider of each sealed key is named in the distribution manifest and checked by `boot`; a profile, site, tenant or `--patch` layer cannot insert another |
| 4 | Fail-closed hooks and deny-only guards | section 8 | A listener that errors denies. A guard can only deny or raise. The tier is combined with a maximum, so a listener cannot lower it |
| 5 | Required rows | section 13 | `approval`, `audit` and `pii` stop boot when they cannot activate, and end readiness when they are lost at run time |

**What stays outside plugins, and why dsh would agree.** Two things, both in `boot` and neither a policy: the binding of sealed keys to providers, and the environment kill switches of layer 6. They sit in the launcher because a row must not be able to patch them. dsh keeps its own switch (`DSH_TELEMETRY_DISABLED`) and its required list in the launcher for the same reason.

**The invariants, one by one.**

| Invariant (00 principle 5) | As plugins | Proof |
|---|---|---|
| Approval floor | `approval.tiered` owns the floor. `remediation` calls `approval.Check` in line before it asks `change` for a token; surfaces call `approval.Decide` only. The `approval/policy` hook is raise-only | `approvaltest.TestProvider`: the result is never below the floor, L3 needs two distinct approvers. Failure matrix: with the `approval` row disabled, `remediation` and every executor are pending and the API answers "capability unavailable" |
| Ledger before a write | `remediation` writes the `ChangeRecord` through `change` and receives the token from it; `Apply` takes the token as an argument | `remediationtest`: `Apply` without a valid token is refused; no code path reaches an executor without a ledger row (recording fake) |
| Credential leases | `credentialLease` returns a write credential only for a token that `change` verifies. Executors need `credentialLease`, never `secrets`, for writes | `leasetest`: a call from `Plan` or `Verify` gets nothing |
| Audit chain | `audit.Append(tx, record)` takes the caller's transaction; `Emit` and `Enqueue` exist only on a transaction, so "the change, its event and its audit row commit together" holds by type | `audittest`; each engine's suite asserts one audit record per state change |
| PII scrub | `pii` is needed by `llm` (request path), `notify` (egress) and `tools` (result path). Contributions return raw output and never scrub | `llmtest`, `notifierstest`, `toolstest`: a seeded identifier never leaves unscrubbed |
| Executor contracts of 00 section 2.2 | The `remediation` dispatcher is the former point guard | `remediationtest.TestExecutor` for executors, plus the dispatcher's own suite |

**The limit, stated plainly.** In-process plugins share an address space. A sealed key stops mistakes and stops configuration from weakening policy; it does not stop hostile Go code, which can use `unsafe`. dsh says the same of its own checks ("not a sandbox against malicious package code") and of scopes ("not a sandbox or an authority boundary"). Third-party code therefore runs out of process, where it sees only what the dispatcher sends it (principle 6 is unchanged).

---

## 8. Q8. Interception

**Two different things, kept apart.** Kernel hooks are synchronous, in process and not durable: they intercept an operation that is happening now. The `events` definition is the durable outbox for facts that have happened. dsh has only the first, plus its session log.

```go
type Next[In, Out any] func(ctx context.Context, in In) (Out, error)

// NewHook is called in the definition package that owns the operation.
// Options: FailClosed() or FailOpen(); who may listen: Open(), FirstParty(), Policy();
// Combine(fn) to make the result monotonic (for example the maximum of two tiers).
func NewHook[In, Out any](name string, opts ...HookOpt) Hook[In, Out]

func (h Hook[In, Out]) On(c *kernel.Context, stage Stage,
	fn func(ctx context.Context, in In, next Next[In, Out]) (Out, error)) error // an effect of c
func (h Hook[In, Out]) Run(c *kernel.Context, ctx context.Context, in In, inner Next[In, Out]) (Out, error)
```

A listener that returns without calling `next` vetoes the rest of the chain and the built-in behaviour, as in `events.ts`; calling `next` twice is an error. The other four dispatch modes (`emit`, `parallel`, `serial`, `bail`) are methods of `kernel.Event[A]`.

**Order.** Cordis orders listeners by registration, with `prepend`. Under reactive activation that order would depend on timing, so we order by a declared stage (`First`, `Early`, `Normal`, `Late`, `Last`) and then by row id. Hook use is declared in the descriptor, so `aiopsctl profile render --dump-hooks` prints every chain before boot.

**Scope.** A listener registered through a tenant context receives only that tenant's dispatches, the port of the context filter and of `dsh-scope`.

| Hook | Owner | Mode | Who may listen | Failure | Typical listeners |
|---|---|---|---|---|---|
| `config/resolve` | kernel | waterfall | first party | closed | `config-refs` schemes |
| `http/request` | `http` | waterfall | first party | closed | identity, tenant resolution, per-tenant rate limit |
| `signals/admit` | `signals` | waterfall | policy | closed | admission bucket, body limits |
| `signals/correlate` | `signals` | waterfall | first party | open | `storm-mode`, later folding rules |
| `investigation/context-pack` | `casefile` | waterfall | open | open | change candidates, estate dependents, known issues |
| `investigation/answer` | `casefile` | waterfall | first party | closed | answer contract check, later the finish gate |
| `llm/request` | `llm` | waterfall | policy | closed | budget, tier routing; the scrub is in-line in the gateway |
| `tools/pre-execute` | `tools` | waterfall, then deny-only guards | open (deny only) | closed | effects class, toolset membership |
| `tools/post-execute` | `tools` | waterfall | first party | closed | output cap; the scrub is in-line |
| `remediation/pre-apply` | `remediation` | waterfall, raise-only | open (raise only) | closed | change windows, freezes |
| `approval/policy` | `approval` | waterfall, raise-only | open (raise only) | closed | a tenant's extra gates; replaces the deferred `approval-policy` point |
| `notify/send` | `notifiers` | waterfall | first party | open | digest, routing |
| `jobs/enqueue` | `jobs` | waterfall | first party | closed | `cohorts` queue choice, fairness |
| `tenant/create`, `tenant/delete` | `tenant` | serial | first party | closed | one saga step per plugin that owns tenant state |
| `internal/plugin`, `internal/status`, `internal/service`, `internal/update` | kernel | emit; `update` is a waterfall | first party | open | loader, reloader, plugin manager, health |

"Raise only" means the hook's combine function keeps the stricter result whatever a listener returns. This is the Kubernetes split of mutating and validating admission, and dsh's guard after its waterfall.

---

## 9. Q9. Composition and reload

**Kept unchanged.** Profiles, ordered bundles, the layer order of 00 section 6.1, patch by id that replaces the whole config, `insert`, type assertions, an unknown id warns, an empty file fails, only direct bundles contribute, dumps with provenance, the schema dump, golden dumps in CI.

**Rows gain four fields**, all from the Cordis loader's entry: `group`, `inject` (extra needs of this row), `intercept`, `isolate`. They also gain `roles` and `scope`. A row is `{id, type, required, enabled, roles, scope, inject, intercept, isolate, group, config}`.

**No expressions, still.** `!!js` is not ported. Two closed forms replace it, both from Go practice:

- A config value may be a reference `${scheme:value}`, resolved by listeners on `config/resolve`, as OTel confmap providers resolve `${env:NAME}`. Schemes: `env`, `file`, `secret`, `flag`. A row that reads a service-backed scheme lists that service in its `inject`, which is the dsh rule that a row injects the service its config reads.
- `enabled` may be `{when: {role: [...], os: [...], service: <key>}}`, a closed set of facts, shown resolved in the dump.

**Decision: live recomposition, with the row as the unit.** A generation is one composed tree. The reloader diffs generation N+1 against N by row id: removed rows are disposed, changed rows get `Update`, added rows are mounted. Everything else follows from activation: dependents restart because their epoch changed.

**Volatile fields.** A schema field marked `x-aiops-volatile` is delivered as a `kernel.Volatile[T]`, an atomic reference. A change that touches only volatile fields is committed in place with no restart, the port of the loader's `equalExceptVolatile`. This replaces `ConfigReader.For`: a tenant-writable field is simply a volatile field of a tenant-scoped row.

**One serialized reload in Go.** The `reload` plugin owns one goroutine and one queue, shared by the file watcher, the plugin manager, the admin API and tenant overlay writes. It is the port of `runExclusive` in `dsh-hmr`.

```go
func (r *reloader) loop(ctx context.Context) {
	for req := range r.queue { // the only writer of the running composition
		gen, err := r.prepare(req) // compose, gate, validate candidates, static graph, restart closure: all pure
		if err != nil {
			req.reply(Rejected(err)) // nothing running was touched
			continue
		}
		res := r.apply(ctx, gen) // kernel.Batch, then Settle under a deadline, then the audit
		if res.RequiredDown() {
			r.apply(ctx, r.lastGood) // roll back through the same path
			res.RolledBack = true
		} else {
			r.lastGood = gen
		}
		r.emit(Recomposed{Generation: gen.N, Digest: gen.Digest, Restarted: res.Restarted, Failed: res.Failed})
		req.reply(res)
	}
}
```

`prepare` validates every changed config as a candidate without publishing it (`fiber.resolveConfig` in Cordis), so a bad value is rejected before any fiber moves: Caddy's "provision the new config fully before touching the old". Rollback when a required row is lost is ours; dsh keeps successful siblings and does not roll back, which is right for a desktop application and wrong for a server.

**Without the `reload` row** nothing recomposes and every change answers `restart-required`, exactly as dsh behaves with HMR disabled. One-shot profiles (`migrate`, `ctl`) disable it.

**What hosted does.**

| Change | Self-host | Hosted |
|---|---|---|
| Tenant overlay (layer 5) | Live | Live, on the replicas that hold that tenant's subtree, by doorbell and poll |
| Profile or site layer, bundle selection among compiled-in bundles | Live through the plugin manager or a file edit | A versioned composition object in `kernel-db` is the source; each replica's reloader applies generation N+1 when it arrives, canary cohort first; a replica whose audit fails halts the rollout. Replicas publish their digest in `profile.recomposed`, so drift is visible |
| New code: kernel, an in-process plugin, a bundle not in the distribution | New binary, restart | New image, rolling restart, N and N-1 coexisting as today |
| Remote plugin version | The runner restarts that plugin's process; its adapter fiber restarts | A rollout of that plugin's Deployment |

Hosted keeps the rolling restart as the default for anything that changes many rows, by the restart-closure threshold of section 3. Live recomposition earns its place there for tenant changes, for disabling a misbehaving row across the fleet in seconds, and for not dropping long-lived connections on a small change.

---

## 10. Q10. Tenancy in the context tree

**Decision: a tenant is a child context with its own subtree, and plugins declare a scope.** This is Backstage's `root` and `plugin` service scopes with "plugin" read as "tenant", built on Cordis `isolate` and `group`, and it is how dsh mounts one preset tree per agent.

| | Root-scoped plugin | Tenant-scoped plugin |
|---|---|---|
| Instances | One per process | One per tenant that enables it |
| Sees | Root services; takes the tenant per call | Root services that are not `RootOnly`, plus the tenant-bound views |
| Examples | `store`, `jobs`, `http`, `signals`, `casefile`, `approval` | pillars, investigators, executors, tools, notifiers, parsers, on-call providers |
| Enabled per tenant | no | yes: the fiber exists in that tenant's subtree or it does not |

**The tenant node.** The `tenant` plugin provides `tenants.Scope(tenantID)`, which returns the tenant's context and mounts its subtree on first use. The node isolates every `TenantBound` key and provides the bound views there: `store.TenantSQL` (a `Tx` with no tenant argument), `secrets.TenantKey`, `settings`, `audit.Tenant`, `llm.Tenant`, `limits.Tenant`. A bound view is a two-word struct over the root engine.

This removes a class of bug. Today a plugin passes a tenant string to every call and review must catch a wrong one. A tenant-scoped plugin has no API that takes a tenant, and `Register` panics if it injects a `RootOnly` key such as the unbound store. The authority boundary is still row-level security, per-tenant keys and process isolation; the scope prevents mistakes, as `dsh-scope` says of itself.

**Presets.** The tenant-scoped rows of a composition form a preset: a group row with `scope: tenant`, mounted once per tenant that selects it. A tenant record names its preset; layer 5 patches that tenant's copy (`enabled` and the tenant-writable fields). This is `dsh-agent-preset`, with tenant for agent.

```yaml
# bundle aiops-pillars-mvp
- insert:
    - id: preset-standard
      type: kernel:group
      scope: tenant
      config:
        - {id: pillar-incident, type: pillar, config: {persona: incident, toolset: [change-lookup, runbook-fetch]}}
        - {id: investigator-holmes, type: investigator.holmes}
        - {id: remediation-executor-kubernetes, type: remediation-executor.kubernetes}
```

**Registries are layered, not copied.** `tools`, `notifiers`, `remediation` and the other registries are root engines with one eager global layer and lazily created tenant layers, the `ScopedLayers` of `dsh-scope`. A registration through a tenant context lands in that tenant's layer and is visible only there; lookup walks tenant, then root.

**Run-time enablement.** A tenant admin toggles a row; the overlay is validated as a candidate, stored with its outbox event, and each replica holding that tenant's subtree reconciles it by the same row diff as section 9. A replica that does not hold the subtree does nothing and gets the new state when it next mounts.

**A subtree is a cache.** Every trigger for tenant work is a request, a durable job or a durable event delivery, and each of them calls `tenants.Scope` first. So a subtree can mount lazily and unload after an idle period (15 minutes, an initial value) with no loss: nothing is scheduled in memory. The rule that makes this true is in the conformance suite: a tenant-scoped plugin's `Apply` does no network I/O and starts no goroutine unless its manifest declares one.

**Upstream sidecars per tenant.** The `sidecars` definition answers "where is the instance of kind K for tenant T". `sidecars.static` (self-host) returns configured addresses. `sidecars.kubernetes` (hosted, worker role, leader) reconciles one instance per tenant and row from the stored composition, with cohort image versions and scale to zero. A sidecar's lifetime is durable state owned by that reconciler; unloading an idle tenant subtree does not touch it.

**Cost at a few hundred tenants.** These are budgets to be measured by the stage 3 benchmark (section 16), not measurements.

| Quantity | Budget | Basis |
|---|---|---|
| Tenant-scoped rows per tenant | about 20 at the first wave, 30 with the second | section 6 |
| Fibers on a replica with 300 tenants all mounted | 6,000 to 9,000 | rows times tenants |
| Memory per idle tenant-scoped fiber, kernel and plugin state | 4 KB or less | a fiber is a struct, two small maps and a context node; no goroutine |
| Memory for 300 mounted tenants | under 40 MB per replica | the product of the above |
| Cold mount of one tenant subtree | under 50 ms, no network | `Apply` without I/O |
| Notification on a root provider change | proportional to dependents of that key, through an index by key and scope | Cordis scans every fiber; we index |
| Goroutines per idle tenant | zero | the conformance rule above |

If the benchmark misses these, the fallback is narrow: keep tenant scope for stateful contributions (investigators, executors, on-call providers) and let stateless ones (parsers, tools) register once at root with a per-tenant enable flag in the registry. The definitions do not change.

---

## 11. Q11. Plugin runtimes and installation

**Three runtimes, one contract.** A contribution's interface, fixtures and conformance suite are the same in every runtime.

| Runtime | For | Mechanism | Order of arrival |
|---|---|---|---|
| In-process Go | First-party and reviewed plugins; everything on a hot path; every engine | Compiled into a distribution by the builder | stage 1 |
| Remote, any language | Third-party code; anything that runs model-chosen actions for a tenant; Python | A `remote.<definition>` adapter plugin proxies the contribution interface to a gRPC service and serves a small callback service back | proven in stage 1 on a toy definition; first real one is `tools` |
| WebAssembly | Pure functions from untrusted authors: parsers, templates, simple tools | Extism on wazero, same messages | on the first real need; no dsh property depends on it, so it is not built ahead |

**Which definitions get a remote adapter.** Registries of contributions do: `tools`, `notifiers`, `signals` parsers, `investigators`, `remediation` executors, `oncall` providers, `llmProviders`, `estate` readers, later report renderers. Engines never do: `store`, `jobs`, `events`, `http`, `tenant`, `identity`, `approval`, `audit`, `change`, `pii` are the trust base and the hot path. Contribution definitions are written proto-first and labelled `v1alpha1` until two providers pass conformance, so nothing is frozen early and in-process and remote plugins share messages and fixtures.

**The remote protocol.** The rules of 07 section 5 stand: one tenant per remote instance in a multi-tenant profile, row and tenant taken from the connection, a handshake that checks protocol, manifest digest and definition versions, deadlines and cancellation, a lost executor call ending `uncertain`. The callback set stays small: telemetry over OTLP, read-only secrets, `store.kv`, audit append, `credentialLease`.

**The same Go plugin runs in either runtime.** The remote SDK for Go is the kernel itself: `remotehost` mounts the plugin on a real kernel whose definitions are provided by gRPC clients to the host callbacks. A Go author writes one `Plugin` and chooses the runtime in the row. Other languages get a generated client and a small context library. A remote plugin process is therefore also a profile: its own row plus callback-backed providers.

**Two runners**, both providers of one `pluginRunner` definition.

| Runner | Model | Where |
|---|---|---|
| `runner.external` | The orchestrator starts the plugin; the kernel dials a configured address or finds sockets in a directory. Dapr's model | Kubernetes, compose, hosted |
| `runner.exec` | The kernel starts a local binary, with handshake, checksum and automatic mTLS. go-plugin's model, used by Terraform, Vault and the dashboard server's backend plugins | A single-binary self-host that wants to add a plugin with no orchestrator |

**What "install a plugin into a profile" means** through the supported interfaces, without changes to the core source.

| Path | Steps | Rebuild |
|---|---|---|
| Remote plugin | `aiopsctl plugin add <oci-ref> --profile <p>`: fetch the manifest, run the gate (section 12), verify the cosign signature against the configured identity or key, pull the artifact (a binary per platform, or an image reference), write `plugins.lock`, add the bundle to `profile.yaml`. With `reload` it activates live; on Kubernetes the chart renders the plugin's Deployment from the lock file | no |
| In-process plugin | Add the bundle's Go module to `dist.yaml`; `aiopsctl build --manifest dist.yaml` (or the builder image, which needs no local toolchain) generates `main.go` with blank imports and a pinned `go.mod`, runs the licence allow-list and the gate, and builds a binary and an SBOM; then add the bundle to the profile | yes |
| Prebuilt distributions | We publish `aiops` (first-party bundles) and `aiops-contrib` (plus vetted community bundles), as the collector project publishes core and contrib | no, if the plugin is in one |

**A bundle is a Go package** that embeds its `bundle.yaml` and blank-imports its plugins, so listing a bundle in `dist.yaml` brings its code, the equivalent of `dsh-base` listing its plugins as dependencies. A bundle of remote plugins is an OCI artifact holding `bundle.yaml` and the plugin references.

**Index.** A git repository of one YAML file per plugin (module path, OCI reference, versions, digests per platform, definitions and versions, stability, licence, signing identity), the krew-index design. It mirrors through Git index retrieval (`git init`, then `git fetch` from the configured index), which suits an air-gapped site; artifacts mirror to any OCI registry. A private index never falls through to the public one, the rule dsh applies to registries.

**Signing.** cosign on every published artifact. Verification is offline against a configured public key or a bundled trust root; `allowUnsigned: [ids]` is the explicit escape, as the dashboard server has.

**Not carried.** dsh installs a package with pnpm and imports it into the running process. Go links statically and its `plugin` package is unusable (plugin-arch section 2.1), so an in-process plugin always needs a build. We do not hide this: the plugin manager answers `rebuild-required` and prints the `dist.yaml` change.

---

## 12. Q12. The plugin manager and the compatibility gate

**The manager is a row**, `plugin-manager` in `aiops-base`, enabled when the launcher provides `profile`. Bundles mark it and `reload` as protected: a toggle cannot disable them or a required row.

| Operation | Writes | Result |
|---|---|---|
| List bundles and rows | nothing | State, health, version, provenance, registrations (`Inventory()`), denials and skips |
| Toggle a row | The last matching override in the profile layer, or the site layer; only departures from the bundles are recorded | `applied`, or `restart-required` with a reason when `reload` is off |
| Enable or disable a bundle | `profile.yaml` bundle list; enabling appends, which the result says can change precedence | `applied` or `restart-required` |
| Install a bundle | Remote: lock file and bundle list after fetch and verify, with streamed log, cancel, and restore of both files on failure. In-process: nothing | `applied`, `restart-required` or `rebuild-required` |
| Remove a bundle | Deselect, wait for unload, delete artifact and lock entry | `applied` |
| Grant or revoke an exemption | `compatibility.yaml` only | applied at the next composition |
| Tenant view | Layer 5 for that tenant's preset rows | live |

Surfaces: `aiopsctl plugin` (against a running server through `admin`, or offline on files under the same lock), a chat command for platform admins, later the app page. Writes take the profile lock: a file lock in self-host, an advisory lock in hosted. Every completed operation emits `plugin-manager.changed` and an audit record.

**Not carried:** dsh's `plugin_manager` model tool. A model never installs or enables code in a multi-tenant server (principle 6).

**The gate** lives in `boot` and runs wherever we own a composition boundary: `aiopsctl build`, `aiopsctl plugin add`, `aiopsctl plugin check`, boot, every recomposition, and a tenant overlay that enables a row.

| # | Rule | In-process | Remote |
|---|---|---|---|
| 1 | `kernelApi` range contains the kernel module version | at build, and again at boot from the embedded manifest and `debug.ReadBuildInfo` | at install and at every handshake |
| 2 | Each definition used: same major; plugin minor not above the host's | by construction at build (one version of `def` is linked) | at install and handshake |
| 3 | `stability` at or above the profile's `minStability` | boot | boot |
| 4 | Licence on the allow-list | at build, over the whole module graph | not applied: a separate program |
| 5 | Signature valid for the configured identity | not applied | at install and at start |
| 6 | Malformed or empty range | deny | deny |

A denied row becomes a detached disabled row in the launcher's copy of the composition; no file is rewritten. A denied bundle is skipped and listed. A denied required row stops boot. Exemptions map an exact `plugin@version` to exact kernel versions, need `--accept-risk`, and are inherited by neither a plugin upgrade nor a kernel upgrade. The gate is not a sandbox.

`packages` is the Go form of dsh's immutable runtime resolution: the descriptors compiled into the binary plus the build info, fixed for the life of the process, and the lock file for remote plugins.

---

## 13. Q13. Failure policy

**Required rows.** A required entry names a row id. The effective required set is the listed ids plus every provider a listed row needs, computed statically and marked in the dump (`required, via approval`).

| Profile or role | Required ids |
|---|---|
| Every server profile | `loader`, `store`, `migrate`, `jobs`, `events`, `api-server`, `tenant`, `identity`, `audit`, `approval`, `pii` |
| `hosted`, in addition | `auth`, `fleet-agentapi`, `signals`, `signal-ingest.alert-webhook`, `gateway` |
| Role `edge` | `store`, `auth`, `fleet-agentapi`, `api-server` |
| Role `api` | the server list without worker-only rows, plus `signals` |
| Role `worker` | `store`, `jobs`, `events`, `tenant`, `audit`, `approval`, `pii` |
| `ctl`, `migrate` | `loader`, and for `migrate` also `store`, `migrate` |

`change` and `credentialLease` are not on the list: without them `remediation` is pending and nothing can be written, which is closed, and the rest of the product (ingest, paging, investigation) keeps running. `pii` is on the list because so many paths need it that its absence should be loud at boot.

**Matrix.** Adapted from the dsh table (dsh-arch section 5.4) with a fourth column, because a server needs a rule for a failure while running.

| Failure | Optional row at boot | Required row at boot | Live recomposition | While running |
|---|---|---|---|---|
| `profile.yaml`, a named bundle or a `--patch` file missing; a layer file malformed or empty | stop | stop | generation rejected; nothing changes | not applicable |
| Row type not in this distribution | warn; inactive | stop | the new row is inactive and reported; the rest applies | not applicable |
| Gate denies | detached as disabled; listed | stop | enabling is refused | not applicable |
| Config fails its schema or `Validate`; a reference does not resolve | warn; inactive | stop | a new row stays inactive; an existing row keeps its instance and config | not applicable |
| A need has no ready provider | `PENDING`, warn; activates when the provider appears | stop | `PENDING`; rollback if the row is required | dependents unload; a required row ends readiness and, after a grace period, the process |
| `Apply` errors or panics | `FAILED`; supervised retry | stop | `FAILED` and retried; siblings continue; rollback if required | not applicable |
| A listener cannot bind | the row fails as above | stop | as above | not applicable |
| Remote plugin unreachable or handshake refused | the adapter row is `FAILED` and retried; its contributions are absent | stop | as above | adapter unloads; contributions vanish from the registry; retry |
| Panic in a request-scoped call: handler, job, hook listener, tool call | not applicable | not applicable | not applicable | recovered; that call fails; counted |
| Error or panic in a goroutine started with `c.Go` | not applicable | not applicable | not applicable | the fiber is `FAILED`, unloaded and retried; its state is discarded |
| Panic in a goroutine no context owns | fatal | fatal | fatal | the process exits non-zero; this is the Go runtime's own rule and the lint exists to keep plugins out of it |
| A disposer exceeds the unload deadline | not applicable | not applicable | the row is reported `leaked`; the result is `restart-required` | readiness ends after the drain window; the orchestrator replaces the process |
| Restart budget spent | stays `FAILED` | exit non-zero | as at boot | optional: stays `FAILED`; required: exit non-zero |
| Row absent or disabled | ignored | covered by "required entry has no enabled row" | ignored | ignored |

Optional failures print once as one grouped diagnostic, with the plugin, the state of each need and the provider it waited for.

**The matrix is a test on the built artifact.** `test/failure-matrix` is table-driven: each case is a fixture profile, a patch that introduces the failure, and the expected exit code, readiness, row states from `aiopsctl status --json` and diagnostic text. The faults come from a test bundle (`aiops-testkit`: rows that fail in `Validate`, fail in `Apply`, panic, leak, never become ready) compiled into a test distribution by the same builder, so the builder is exercised by the same suite. The live column applies each patch to a running process.

---

## 14. Q14. Testing in isolation, in tiers

| Tier | What runs | Fails when | Needs |
|---|---|---|---|
| T0 Static | `aiopsctl plugin check ./`: manifest against generated values, schema and examples, import boundary, static graph of the plugin against its fakes, name and licence lint | manifest drift, a forbidden import, an undeclared need | nothing |
| T1 Bare context | `kerneltest.Run(t, fn)`: a root context with nothing in it, inside a `synctest` bubble. The test provides fakes, mounts the plugin, drives it, disposes it. The generated cycle test is in this tier | wrong behaviour; any residue; a need the plugin did not declare; network use in a tenant-scoped `Apply` | Go; PostgreSQL only if the plugin needs `store.sql` |
| T2 Conformance | `<x>test.TestProvider(t, factory)` for every provider of a definition, and `<x>test.TestContribution(t, plugin)` for every contribution, in each runtime the manifest lists | the provider or contribution breaks the definition's contract | as T1; a remote leg starts the plugin under `remotehost` |
| T3 Real composition | `loadersmoke.Run(t, binary, profile, patches)` boots the built distribution with a test-only profile; the failure matrix; golden dumps of every shipped profile | rows do not compose, activate, reload or fail as the record says | the built binary, PostgreSQL |
| T4 Replay | The exit demo and investigator cassettes with `AIOPS_SNAPSHOT=replay`; only the model, the network and upstream processes are replayed | a request drifts from its cassette | fixtures |
| T5 Live | The lab | not a pull-request gate | the lab |

**Fakes ship beside the definition and pass its suite.** `storetest` is the exception that proves the rule: SQL semantics cannot be faked, so the "fake" for `store.sql` is the real provider on a throwaway schema, as 00 section 2.1 row 9 already decided.

```go
func TestTicketLookup(t *testing.T) {
	kerneltest.Run(t, func(t *testing.T, root *kernel.Context) {
		reg := toolstest.Provide(root)                         // the fake provider of def/tools
		secretstest.Provide(root, secretstest.Tenant("acme", "token", "x"))
		dialtest.Provide(root, dialtest.Fixtures("testdata/fixtures")) // strict replay, no socket
		tc := tenanttest.Scope(root, "acme")                   // a tenant node with the bound views

		f := tc.Plugin(ticketing.Plugin, &ticketing.Config{BaseURL: "https://tickets.example.com"})
		kerneltest.Active(t, f)

		res := reg.Invoke(t, tc, "ticket-lookup", `{"id":"T-1"}`)
		if res.Effects != tools.Read { t.Fatalf("effects = %v", res.Effects) }

		kerneltest.DisposeClean(t, f) // dispose, then assert every fake is at baseline and nothing leaked
	})
}
```

**Rules taken from dsh.**

- A product-visible plugin needs a T3 test. A hand-built mount is not enough.
- Mock only the model, the network and upstream processes. Everything downstream is real.
- Assertions read the world again: database rows, the audit export, the rendered file. Not a log line.
- Every registry gets the registry safety test of section 4.

**Coverage.** dsh gates every package file at 100 %. We carry that gate for the `kernel` module only, with the race detector and a property test that drives random sequences of provide, dispose, update and failure and asserts convergence and an empty ledger. For plugins the gate is the generated cycle test and the conformance suites; a numeric gate there is a later decision.

**What a contributor runs before opening a pull request.**

```text
go test -race ./plugins/tool/ticket-lookup/...   # T1 and T2 for this plugin
aiopsctl plugin check ./plugins/tool/ticket-lookup
go test ./test/loader-smoke -run TicketLookup    # T3, for a product-visible plugin
```

A third party runs the first two against the `kernel` and `def` modules alone, with no checkout of this repository.

---

## 15. Q15. Apps are profiles

Two words, kept apart: a **distribution** is a set of code built by the builder; a **profile** is a composition run from a distribution. `cmd/<distribution>/main.go` is generated and calls `boot.Run`; a CI check fails on any other `main` package, the port of dsh's `verify-application-entrypoints`.

| Distribution | Profiles | Mode bundle and startup |
|---|---|---|
| `aiops` (server) | `hosted`, `self-host-full`, `aiops-only`, `dev` | `aiops-hosted`, `aiops-selfhost` or `aiops-dev`. `--role edge\|api\|worker\|all` is a launcher fact; rows carry `roles`, and the filter is applied at composition, so `aiopsctl profile render --role api --dump` shows exactly what that process mounts |
| | `migrate` (one-shot) | `store`, `migrate`; reads migrations from descriptors, activates nothing else; `reload` off |
| `aiopsctl` (CLI) | `ctl` | `aiops-ctl`: the `cli` definition, command rows contributed by bundles (the Slack bundle contributes `slack manifest`, the stack bundle `agent render`), and an API client. Offline commands (`profile render`, `plugin check`, `build`) link the same `boot` code as the server, so a dump is what the server would boot. `reload` off |
| `remotehost` (SDK) | one per remote Go plugin | The plugin's row plus callback-backed providers (section 11) |
| `aiops-testkit` (test only) | fixtures of the failure matrix | Fault rows |

`aiops ctl ...` is the same `ctl` profile inside the server distribution; `aiopsctl` is a smaller distribution of the same bundles for a laptop.

**The agent side.** Nothing of ours runs in the agent process: it is the unmodified upstream agent, and its own component registry is its plugin system. `agent-host` and `agent-k8s` are profiles whose rows are `fleet-source` contributions: template fragments keyed by feature. They are composed by the same loader and evaluated by the `fleet` composer on the server, or by `aiopsctl agent render` offline. A later host agent of our own would be one more distribution with one more profile, with no change to the model.

**Lab.** Still neither a bundle nor a profile.

---

## 16. Q16. Order of work

No dates. Each stage ends with a proof, and nothing in a later stage may add code outside a plugin.

| Stage | Builds | Proof at exit |
|---|---|---|
| S0 Kernel | `kernel` module: contexts, keys, fibers, reconciler, ledger, hooks, config pipeline. `kerneltest`. A parity suite translated from the behaviours read in `cordis/src` (epoch, dependents retained, late registration, failed until update, volatile) | 100 % coverage, race detector, property test. A benchmark with 10,000 fibers gives the first reading for section 10 |
| S1 Model end to end on a toy | `boot`: compose, dumps, gate, loader, audit, supervisor. `reload`. The builder and generated `main`. `plugingen`. One toy definition (`counter`) with a fake, a suite, an in-process provider and a remote one under `remotehost` with `runner.external`. `test/loader-smoke` and `test/failure-matrix` on the toy distribution | A plugin from another repository, built against `kernel` and `def` only, passes `plugin check`, is added to a profile as a remote artifact and enabled live; the same code is then built in-process by the builder and passes the same suite. The whole failure matrix is green. This is the current P3 exit test, moved to the front |
| S2 Infrastructure | `metrics`, `health`, `dial`, `kek`, `secrets`, `store`, `migrate`, `jobs`, `events`, `limits`, `http`, `cli`, each as definition, fake, suite and provider | Disable and re-enable every row live in the smoke test with no residue; N and N-1 schema rule holds |
| S3 Tenancy and identity | `tenant` with scopes, presets, lazy mount and idle unload; bound views; `identity`, `authz`; `plugin-manager`; the profile lock | The tenant benchmark meets section 10 or the fallback is taken, recorded in an ADR. A tenant-scoped test plugin cannot reach another tenant's data through any injected key |
| S4 Policy | `audit`, `pii`, `approval`, `change`, `credentialLease`, with sealed keys and the distribution binding | The bypass rows of the failure matrix: each policy row disabled or failed in turn, and no write, model call or message gets through |
| S5 Incident loop | `signals` and parsers, `correlator`, `casefile`, `notify`, `tools` (with the first real remote adapter), `llm`, `investigate`, `investigator.holmes`, `remediation` and the first executor, `chat` and the Slack surface, `oncall` and its provider, `pillars` | The six-step demo of 00 section 7.1 with its host leg, replayed, on `self-host-full` and `dev` |
| S6 Stack and agents | `auth`, `gateway`, `fleet`, `fleet-agentapi`, `fleet-source` rows, `stack-provision`, `toolset-bindings`, `datasource-metrics`, `annotator`, `compliance` | A VM install ships four signals through the gateway and appears in inventory; `aiops-only` boots without `aiops-stack` |
| S7 Hosted | `aiops-hosted`: roles, `sidecars.kubernetes`, `kek.kms`, `cohorts`, the composition object and staged recomposition | Two replicas converge on a generation and report one digest; a tenant toggle reaches both; tenant delete runs every saga step |
| S8 Ecosystem | The index, signing policy, builder image, `aiops-contrib`, `runner.exec`, the Python client | An outside author publishes a plugin with no help from us |

Two things keep every later feature a plugin: the generated `main` (there is no other place to wire code) and the import tests on `kernel` and `boot` (neither may grow a domain import).

The cost is stated here once: S0 to S4 deliver no user-visible feature. The existing P0 exit (signals through the gateway) moves to S6, and the incident loop to S5.

---

## 17. dsh property checklist

"As is" means the same semantics in Go syntax. Sources: `cordis/src`, the loader, `dsh-app-boot`, `dsh-hmr`, `dsh-plugin-manager`, `dsh-scope`, `dsh-user-approval` and `dsh-tools` READMEs, and dsh-arch.

**Cordis core**

| # | Property | Status | Note |
|---|---|---|---|
| 1 | One `Context` type; no privileged core | carried as is | kernel has no feature code |
| 2 | Context as a proxy; `ctx.<service>` | adapted | typed keys, `Get`, per-definition `From` |
| 3 | Services typed by declaration merging | adapted | `Key[T]` |
| 4 | `ctx.extend` | carried as is | `Extend` |
| 5 | `ctx.isolate`, private and shared labels | carried as is | `Isolate`; also the tenant scope |
| 6 | `ctx.intercept` | carried as is | `Intercept` |
| 7 | Built-in root services (reflect, registry, events, logger) | adapted | part of the kernel API; logger is `slog` |
| 8 | `provide` owned by a fiber, removed with it | carried as is | `Provide` |
| 9 | Availability predicate (`check`) | adapted | `ReadyWhen` with a hold time; not used by infrastructure |
| 10 | Duplicate provider in one scope is an error | carried as is | |
| 11 | Imperative, undeclared `provide` | adapted, stricter | `Provides` must be declared, for static checks and dumps |
| 12 | `ctx.set`, `accessor`, `mixin`, callable services, method `@Inject` | not carried | JavaScript ergonomics with no Go use |
| 13 | Service base class | adapted | a struct provided in `Apply` |
| 14 | Three plugin shapes | adapted | one descriptor |
| 15 | Plugin metadata: `name`, `Config`, `inject`, `provide`, `intercept` | carried as is | `Spec` |
| 16 | Access without `inject` throws | carried as is | `Get` panics; fakes too |
| 17 | `ctx.get` for optional reads | carried as is | `Want` and `Lookup` |
| 18 | `ctx.inject(deps, cb)` sub-fiber | carried as is | `When` |
| 19 | Six fiber states | carried as is | |
| 20 | Epoch: activate, deactivate, restart on provider swap | carried as is | one reconciler goroutine |
| 21 | Providers retained until dependents finish unloading | carried as is | |
| 22 | A failed fiber recovers only through `update()` | adapted | supervised retry with backoff and a budget; a server cannot wait for a person |
| 23 | `ctx.effect`: disposer, promise or iterable; labelled effect tree | adapted | a function returning a disposer; several effects are several calls; labels kept |
| 24 | Reverse-order disposal, awaited | adapted | strict last in, first out, with a deadline; Cordis awaits top-level disposers together |
| 25 | `INACTIVE_EFFECT` on a disposed fiber | carried as is | `ErrInactive` |
| 26 | A released injection cannot be accessed | adapted | a Go value cannot be revoked; goroutine ownership plus a provider-side check, proven by conformance |
| 27 | `fiber.update` through the `internal/update` waterfall | carried as is | |
| 28 | Volatile config committed without restart | carried as is | `Volatile[T]` |
| 29 | `rawConfig`, `effectiveConfig`, `resolveConfig` (candidate without publishing) | carried as is | used to validate overlays and generations before applying |
| 30 | `internal/config` waterfall | carried as is | `config/resolve` |
| 31 | Config by Standard Schema, synchronous | adapted | a Go struct, a derived JSON Schema, `Validate` |
| 32 | `emit`, `parallel`, `serial`, `bail`, `waterfall` | carried as is | typed |
| 33 | Listener order by registration and `prepend` | adapted | declared stage, then row id, because activation order is not deterministic |
| 34 | Listener context filter; scoped events | carried as is | tenant scope |
| 35 | Listeners removed with their fiber | carried as is | |
| 36 | Lifecycle events `internal/plugin`, `status`, `service`, `update`, `dispatch` | carried as is | |
| 37 | `internal/get`, `internal/set`, `internal/listener` | not carried | they intercept the proxy, which does not exist |
| 38 | `root.fiber.dispose()` restarts the tree | carried as is | used for graceful shutdown |

**Loader, profiles and boot**

| # | Property | Status | Note |
|---|---|---|---|
| 39 | Loader is a plugin with an entry tree; create, update, remove, resolve, await, locate | carried as is | mounted by `boot` |
| 40 | Entry fields `id`, `name`, `config`, `group`, `disabled`, `inject`, `intercept`, `isolate` | carried as is | plus `required`, `roles`, `scope` |
| 41 | Group rows; nested ids | carried as is | `kernel:group`; tenant presets |
| 42 | Include: a file-backed tree with write-back, journal and merge with hand edits | adapted | one writer under a lock and atomic rename; a write is refused if the file changed since it was read |
| 43 | Modules imported by name at run time | not carried | Go links statically; the type is looked up in the compiled registry, or the plugin is remote |
| 44 | `!!js` expressions in `config` and `disabled` | not carried | closed `${scheme:value}` references and `when` facts; a server config must be reviewable as data |
| 45 | A row injects the service its config reads | carried as is | row `inject` |
| 46 | Row order has no load meaning | carried as is | |
| 47 | Volatile-only diff | carried as is | |
| 48 | Empty root, ordered bundles, profile layer, home layer, `--patch`, environment switch | carried as is | plus the tenant layer |
| 49 | Patch by id replaces the whole config; `insert`; name assertion; unknown id warns; empty file fails | carried as is | |
| 50 | Only direct bundles contribute | carried as is | |
| 51 | A bundle is a data-only patch plus dependencies on code | carried as is | a Go package embedding its patch and blank-importing its plugins |
| 52 | Bundle `platforms`; several patch files; presets | adapted | `when` facts; presets become tenant presets |
| 53 | Dump with provenance; schema dump | carried as is | plus the hook dump and the static check |
| 54 | Profile templates; create from a template through a staging directory; `sanitizeProfile` | carried as is | in `aiopsctl profile` |
| 55 | One immutable runtime resolution of packages | adapted | the compiled registry and build info; a lock file for remote plugins |
| 56 | `prepare` provides launcher facts before any entry mounts | carried as is | `profile`, `startup`, `packages` |
| 57 | Ready when the root is active and the loader exists; bounded shutdown | carried as is | deadline per role |
| 58 | Detached rejection or uncaught exception is fatal | adapted | a panic in an unowned goroutine is fatal by the Go runtime; owned work fails its fiber or its request |
| 59 | `.env` layering | not carried | the deployment owns the environment |
| 60 | Environment switch that config cannot undo | carried as is | layer 6 |
| 61 | Apps are profiles; one launcher; no other entry point | carried as is | generated `main` |
| 62 | Global required-entry list | adapted | per profile and role, with the needs closure |
| 63 | Failure matrix on the built CLI | carried as is | with a run-time column |
| 64 | No rollback of a live reload | not carried | a server rolls back to the last good generation when a required row is lost |

**Plugin manager, compatibility, reload**

| # | Property | Status | Note |
|---|---|---|---|
| 65 | The manager is a plugin and cannot remove itself | carried as is | |
| 66 | A toggle records only departures from the bundles | carried as is | |
| 67 | Install through the package manager into the profile | adapted | OCI artifacts and a lock file for remote plugins |
| 68 | Runtime install of in-process code | not carried | Go cannot; `rebuild-required` and the builder |
| 69 | Registry fallbacks; a private registry never falls through | carried as is | index and registry mirrors |
| 70 | Streamed install log, cancel, write lock, restore on failure | carried as is | remote plugins |
| 71 | Build-script approval | not carried | no install scripts exist |
| 72 | `engines.dsh`, else peer ranges; checked at install, boot and recomposition | carried as is | `kernelApi` and definition versions |
| 73 | Exact-version exemptions with accepted risk, not inherited | carried as is | |
| 74 | Denied row detached in the launcher's copy; skipped bundles listed | carried as is | |
| 75 | Lockstep first-party versions | carried as is | one module, one release |
| 76 | `plugin_manager` tool callable by the model | not carried | principle 6 |
| 77 | Localized title, description, icon; seven locales for first party | adapted | title and description in the manifest in English; locale files reserved |
| 78 | Configuration recomposition on one queue shared with the manager | carried as is | `reload` |
| 79 | Code replacement in a running process | not carried | rolling restart, or a remote plugin's process restart |
| 80 | With reload disabled, changes need a restart | carried as is | |

**Seams, scope, testing**

| # | Property | Status | Note |
|---|---|---|---|
| 81 | Definition, provider, consumer; plugins depend on definitions | carried as is | always split; import rule enforced four ways |
| 82 | Singular keys for engines, plural for registries | carried as is | |
| 83 | Approval fails closed when absent | carried as is | by need |
| 84 | Monotonic guard after the waterfall | carried as is | deny-only guards, raise-only combine |
| 85 | `dsh-scope` layered registries; visibility and ownership from one context | carried as is | tenant layers |
| 86 | Agent presets as nested rows | carried as is | tenant presets; pillar rows |
| 87 | Scope and isolate are not an authority boundary | carried as is | stated; RLS, keys and processes are the boundary |
| 88 | In-process plugins are not sandboxed | carried as is | third parties run remote |
| 89 | "Model-visible means logged" | carried as is | operator-visible means logged |
| 90 | Browser-side plugin tree; RPC generated from TypeScript types | not carried | the UI is dashboards, an app plugin and chat; contribution definitions are proto-first |
| 91 | Sandbox policy for agent tools | not carried | no shell tool exists; executors are typed plans |
| 92 | Bare-context unit mount | carried as is | T1 |
| 93 | Every registry gets a dispose-and-assert test | carried as is | and generated for every plugin |
| 94 | Test-support packages: prerequisite mounting, model replay, fault server, loader smoke, snapshots | carried as is | `<x>test`, `llm-provider.replay`, `loadersmoke`, cassettes |
| 95 | Product-visible plugins need a real-composition test | carried as is | T3 |
| 96 | Mock only the non-deterministic boundary; assertions re-read the world | carried as is | |
| 97 | Per-file 100 % coverage | adapted | kernel module only |

Totals: 97 properties; 64 carried as is, 21 adapted, 12 not carried. The twelve fall in three groups. Go cannot: run-time import of in-process code (43, 68) and code replacement (79). A multi-tenant server should not: expressions in config (44), no rollback of a reload (64), a plugin manager the model can call (76). Not applicable here: proxy ergonomics (12, 37), `.env` layering (59), build-script approval (71), the browser plugin tree (90), the shell sandbox (91).

---

## 18. What this proposal changes in the record

Each line needs an ADR that edits the file in the same change.

| File | Change |
|---|---|
| 00 section 1, principle 3 | "Extension points are earned" becomes "a definition is earned by a consumer": every service a plugin needs has a definition, a fake and a suite |
| 00 section 1, principle 5 | "host invariant" becomes "enforced by the dispatcher of each definition, behind sealed keys bound by the distribution" |
| 00 section 2.1, row 1 | Go kernel as a port of the Cordis model; reactive activation; the builder, both runtimes, the gate and the plugin manager move from P3 to the first stages; contribution definitions are proto-first at `v1alpha1` |
| 00 section 4 | The table of twelve points and the lists of packages and kernel rows are replaced by the catalogue of section 5 and the inventory of section 6; one row class |
| 00 section 6.1 | Rows gain `group`, `inject`, `intercept`, `isolate`, `roles`, `scope`; references and `when` replace "rows carry no expressions"; layers 0 to 4 recompose live through `reload`; `ConfigReader.For` is replaced by volatile fields of tenant-scoped rows; the required list of section 13 |
| 00 section 6.2 | `aiops-ctl` and `aiops-testkit` bundles; `sidecars` rows in the mode bundles |
| 00 section 7.2, 7.3 | Phases re-sequenced as section 16; the triggers for hot reload, the remote adapters, the gate and the SDK split are met by design and leave the list |
| 03 section 8.1 | Composition and lifecycle rewritten; no dependency order |
| 07 | Rewritten around `kernel`, `def` and the test kits |
| 08 section 1 | Concept map updated from this checklist; section 1.6 takes the four-column matrix |
| 08 section 4 | Three modules from the start; `kernel/`, `boot/`, `def/`, generated `cmd/` |
| 08 section 5.1 | L7 must admit MPL-2.0 if `runner.exec` uses go-plugin |

---

## 19. Risks and open questions

| # | Risk | Mitigation |
|---|---|---|
| 1 | The kernel port is subtle. Cordis reached its teardown ordering through repaired races; a Go port starts with none of that history | Small kernel; parity suite; `synctest`; property test; S0 ends before anything depends on it |
| 2 | A swap or failure of an infrastructure provider restarts most of the tree, which is an in-process restart with more moving parts than a real one | Restart-closure threshold; infrastructure never uses `ReadyWhen`; hosted keeps the rolling restart for wide changes |
| 3 | Go cannot stop a goroutine. A stuck disposer or a leaked goroutine cannot be reclaimed | Unload deadline, `leaked` state, `restart-required`; the cycle test and `goleak` find most before release |
| 4 | Go libraries hold global state (default metric registry, client pools, informers, sockets) and resist unloading | Each provider wraps them behind the ledger; the three-cycle test catches a second activation that fails |
| 5 | About fifty definitions before the first feature. Some interfaces will be wrong because nothing has used them, which is what principle 3 guarded against | `alpha` labels; a definition needs a consuming test; S0 to S4 are sized as platform work, with no feature promised |
| 6 | Tenant subtrees at a few hundred tenants are unmeasured | Budgets in section 10, a benchmark at S3, a narrow fallback that changes no definition |
| 7 | Live recomposition across replicas can drift | One composition object, digests in `profile.recomposed`, staged apply, halt on a failed audit |
| 8 | Sealed keys and scopes are not a security boundary for in-process code | Said in section 7; third parties run remote; RLS and per-tenant keys stay |
| 9 | dsh and Cordis keep moving (section 1, option A). Our port follows the semantics of one pinned version | The checklist names the version read; a yearly re-read is cheap because the kernel is small |
| 10 | `runner.exec` brings go-plugin (MPL-2.0) or our own process supervision | Licence decision at S8; `runner.external` covers every deployment until then |
| 11 | Hook order by stage and row id is a new rule dsh does not have; a wrong stage is a silent ordering bug | The hook dump is a golden file per shipped profile |

Open questions for the judges.

1. Should `pii` be a required row (this proposal) or only a need of the paths that scrub?
2. Is the 20 % restart-closure threshold a refusal or a warning in self-host?
3. Does the `def` module stay one module, or split by tier once the first definitions are stable?
4. Should rollback on a lost required row also apply at tenant scope, where "required" has no list today?

---

## Sources

Published package sources and public records, read 2026-11-10.

- Published `@deepseek-ai/dsh` 0.2.2-rc.1, source `https://github.com/deepseek-ai/deepseek-harness`; upstream Cordis `https://github.com/cordiverse/cordis`: `@deepseek-ai/cordis` 4.1.0 (`src/context.ts`, `fiber.ts`, `reflect.ts`, `registry.ts`, `events.ts`, `service.ts`, `utils.ts`, `README.md`, `LICENSE`, MIT); `cordis-plugin-loader` 1.1.0 (`README.md`, `src/config/isolate.ts`, `src/config/entry.ts`); `cordis-plugin-group` and `cordis-plugin-include` READMEs; `dsh-app-boot`, `dsh-hmr`, `dsh-plugin-manager`, `dsh-scope`, `dsh-user-approval`, `dsh-tools` READMEs.
- The documented `dsh web --dump-default-config` surface and preset `isolate` groups. Machine-specific output and counts are not retained public evidence.
- `docs/research/dsh-architecture.md`, `docs/research/plugin-architecture.md`, `docs/architecture/00-decisions.md`, `03-c3-kernel.md` section 8, `07-c4-plugin-sdk.md`, `08-profiles-roadmap-adrs.md` sections 1 and 4.

Web sources fetched 2026-11-10 from the default branch of each public repository.

- OpenTelemetry Collector: `extension/extensioncapabilities/interfaces.go` (`Dependent`, `PipelineWatcher`), `component/host.go`, `otelcol/collector.go` (`reloadConfiguration`, partial receiver reload), `cmd/mdatagen/internal/templates/component_test.go.tmpl` and `package_test.go.tmpl` (`TestComponentLifecycle`, `goleak.VerifyTestMain`), `cmd/builder/README.md`.
- Caddy: `modules.go` (`RegisterModule`, `Provisioner`, `Validator`, `CleanerUpper`), `context.go` (`OnCancel`, per-context metric registry, `App`, `AppIfConfigured`), `caddy.go` (`unsyncedDecodeAndRun`: run the new config, swap, stop the old), `cmd/commands.go` (`RegisterCommand`).
- Alloy: `internal/service/service.go` (`Definition`, `Host.GetService`, `Host.NewController`), `internal/component/component.go` (`Run`, `Update`).
- uber-go/fx `lifecycle.go`; uber-go/dig `scope.go`.
- Backstage: `docs/backend-system/architecture/03-services.md` (scopes, default factories, multiton), `04-plugins.md`, `05-extension-points.md`, `building-plugins-and-modules/02-testing.md`.
- Dapr: `pluggable-components-registration.md` (socket folder, gRPC reflection).
- hashicorp/go-plugin `README.md`; thejerf/suture `README.md`; riverqueue/river `worker.go` (`AddWorker`, `AddWorkerSafely`).
- Go: `src/testing/synctest/synctest.go`, `src/testing/fstest/testfs.go`.

Cited from memory and to be checked before the ADR: controller-runtime's work queue semantics; the Kubernetes admission `failurePolicy` field; the krew-index manifest format; golangci-lint `depguard`; `fx.ValidateApp` and `fxtest`; `testing/slogtest`; xcaddy; the go-plugin licence (MPL-2.0, as plugin-arch section 2.1 records).

---

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
