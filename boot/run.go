package boot

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	loaderdef "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The run path: boot steps 1 to 5 and the life of the process after them
// (03-c3-kernel.md:355-361, :363-372; 07-c4-plugin-sdk.md:1340-1365). Run takes
// every input as a value, so a test drives it with an in-memory file system, toy
// plugins, a signal channel it writes to and short limits; cmd/aiops is the only
// place that touches the operating system.

// Exit codes of Run. The documents say only that boot "exits non-zero when the
// required set is not met" (08-profiles-roadmap-adrs.md:520, :538) and that a
// process is replaced in three cases (03-c3-kernel.md:372); the numbers are this
// package's, adopted as written (ADR-059 decision 29). Under RunOptions.Command 0, 1
// and 2 are also the codes of the command, a signal or the end of ctx is
// ExitInterrupted, and the launcher never returns ExitUsage, so 0 and 2 are always the
// command's (ADR-061 decision 13).
const (
	ExitOK       = 0 // stopped by a signal or by the caller's context, tree disposed in time
	ExitRefused  = 1 // boot stopped before any row mounted: a layer, the gate, the static graph, the distribution
	ExitUsage    = 2 // bad flags; cmd/aiops returns it (it is also the code of a Go panic)
	ExitRequired = 3 // the required set is not met once the tree settled (03-c3-kernel.md:361)
	ExitLeak     = 4 // replaced: a fiber leaked past its drain or dispose deadline, or shutdown overran its bound
	ExitGaveUp   = 5 // replaced: restart-policy gave up on an infrastructure provider
	ExitInternal = 6 // the launcher failed: the kernel refused its config, a launch key could not be provided
	ExitRestore  = 7 // replaced: a restore failed
	// ExitInterrupted is the stop by a signal or the end of ctx under RunOptions.Command
	// (ADR-061 decision 13).
	ExitInterrupted = 8
)

// LoaderID is the module-table id of the loader plugin, which boot mounts first
// and which no bundle holds (03-c3-kernel.md:65, :360). LoaderRowID is the id of
// the row that stands for it in the admitted tree.
const (
	LoaderID    = "aiops/loader"
	LoaderRowID = "loader"
)

// WithLoader returns c with the row of the loader first. It is the launcher's copy
// for the gate and the graph only: the row is never given to the loader to mount, and
// the digest is that of the composed tree. A caller that admits a composition as boot
// does, as `aiopsctl profile render` does, must admit this copy, or every row that needs
// the loader or composition key has no provider.
func WithLoader(c Composition) Composition {
	row := ComposedRow{Row: Row{ID: LoaderRowID, Plugin: LoaderID, Enabled: true, Required: true}}
	c.Rows = append([]ComposedRow{row}, c.Rows...)
	return c
}

// BatchFunc is (*kernel.Root).Batch, the one function the loader gets to commit
// operations (07-c4-plugin-sdk.md:1360).
type BatchFunc = func(ctx context.Context, ops ...kernel.BatchOp) ([]*kernel.Fiber, error)

// Registry is the module table as Go values: what a generated dist/ package
// holds (07-c4-plugin-sdk.md:1339, :1355; there is no init() registry).
type Registry struct {
	// Modules maps a plugin id to its entry. Each entry's Plugin holds the
	// kernel.Plugin value returned by kernel.Define: the kernel names an entry by
	// that value, never by an id (03-c3-kernel.md:81).
	Modules ModuleTable
	// Keys adds keys that no plugin of the table declares to the key table. The
	// table is otherwise built from the keys the plugins provide and inject, the
	// launch keys, the keys of the loader and composition definitions and the
	// built-in keys of the kernel; a row names a key by string in inject, isolate
	// and intercept (07-c4-plugin-sdk.md:1244, :1347).
	Keys map[string]kernel.AnyKey
	// Hooks maps the name of every sealed hook that dist.yaml lists to its value. The
	// six hooks of the kernel (internal/plugin, internal/status and so on) are always
	// known and need no entry.
	Hooks map[string]kernel.AnyHook
	// LoaderConfig builds the config of the loader plugin from the rows to mount,
	// the batch function and the plugin values of the module table by id
	// (loader.Config{Rows, Batch, Plugins}). The loader is the one holder of those
	// values: no launch key offers them. boot may not import the loader package
	// (08-profiles-roadmap-adrs.md:1252), so the module table, which may, carries
	// this one function.
	LoaderConfig func(rows []loaderdef.EntrySpec, batch BatchFunc, plugins map[string]kernel.Plugin) any
}

// kernelHooks are the hooks that boot knows to be sealed by their definition: the
// kernel's own (ADR-034, "S1 additions", G5) and the loader definition's three. Only
// the entries that Config.Listeners lists may listen on them.
func kernelHooks() []kernel.AnyHook {
	return []kernel.AnyHook{kernel.InternalPlugin, kernel.InternalStatus, kernel.InternalService,
		kernel.InternalConfig, kernel.InternalUpdate, kernel.InternalCommit,
		loaderdef.PatchContext, loaderdef.EntryInit, loaderdef.VolatileUpdate}
}

// knownKeys are the keys whose definitions boot holds: the six launch keys, the keys of
// the loader and composition definitions and the built-in keys of the kernel.
func knownKeys() []kernel.AnyKey {
	return []kernel.AnyKey{launch.ProfileContextKey, launch.CatalogKey, launch.CmdlineKey, launch.ReadyKey,
		launch.KillSwitchesKey, launch.AppExitKey, loaderdef.Key, composition.Key, kernel.ReflectKey,
		kernel.RegistryKey, kernel.HooksKey, kernel.LoggerKey}
}

// Clock is the clock of the launcher's own bounds. It is not the clock key: the
// launcher runs before any row.
type Clock interface {
	// AfterFunc calls f once, d from now, on a goroutine of the clock's own, and
	// returns the function that stops it.
	AfterFunc(d time.Duration, f func()) (stop func() bool)
}

type systemClock struct{}

func (systemClock) AfterFunc(d time.Duration, f func()) func() bool {
	//kernelcheck:allow-go the launcher's bounds (settle, unload wait, leak grace, shutdown) belong to no fiber; each is stopped by the step that made it
	return time.AfterFunc(d, f).Stop
}

// SystemClock returns the clock of the operating system.
func SystemClock() Clock { return systemClock{} }

// Phase is the stage of the process that ProcessStatus reports.
type Phase string

// The phases of a process, in order. A process that never reaches PhaseRunning
// ends in PhaseStopped with a non-zero Exit.
const (
	PhaseStarting Phase = "starting"
	PhaseRunning  Phase = "running"
	PhaseStopping Phase = "stopping"
	PhaseStopped  Phase = "stopped"
)

// ProcessStatus is what the launcher tells the outside about the process: the readiness
// latch and the phase. The documents name no mechanism for S1 (readiness is a
// latch that a row sets, 03-c3-kernel.md:67, and the "reports ready" of 07 section
// 7.2 has no channel), so Run hands every change to RunOptions.Publish and
// cmd/aiops writes it to a file in the runtime directory of the home, the readiness
// channel of one process at S1 (ADR-059 decision 31).
type ProcessStatus struct {
	Phase   Phase
	Ready   bool
	Profile string
	Role    string
	Digest  string // hex of the composition the process booted
	Exit    int    // set in PhaseStopped
	Detail  string // why the process stopped, one line
	// Rows is each mounted row's state and what it waits for, set at settle and at every
	// committed generation (ADR-059 decision 31 as amended; ADR-061 decision 21).
	Rows []RowStatus
}

// RowStatus is one row of ProcessStatus: its state, "" while it has no fiber, and the
// keys it needs that have no ACTIVE provider, sorted.
type RowStatus struct {
	ID       string
	State    State
	WaitsFor []string
}

// RunOptions are the inputs of Run. They are values: nothing here is read from
// the process.
type RunOptions struct {
	Profile string
	// Role is edge, api, worker or all; empty means all (03-c3-kernel.md:547).
	Role string
	// Args and Flags feed the cmdline service. Flags holds what ${flag:name}
	// reads; the caller adds "home" for the home it resolved.
	Args  []string
	Flags map[string]string

	// Home holds profiles/<profile>/; Bundles holds bundles/<name>/bundle.yaml;
	// Patches are the --patch files in argument order (layers 2 to 4).
	Home, Bundles fs.FS
	Patches       []File

	// Env supplies layer 6, the kill switches (08-profiles-roadmap-adrs.md:111).
	Env func(string) string
	// Signals delivers SIGINT and SIGTERM; any value starts a clean stop.
	Signals <-chan os.Signal

	// Stdout and Stderr are where the launcher writes its own lines. Stdio holds the
	// streams of the process that cmdline serves (launch 0.2): in aiops they are the
	// same, and aiopsctl discards what the launcher writes on stdout (ADR-061 decision
	// 25). Dir and Environ are the working directory and the environment that cmdline
	// serves, which only aiopsctl fills (ADR-061 decision 13).
	Stdout, Stderr io.Writer
	Stdio          Stdio
	Dir            string
	Environ        []string

	Registry Registry
	// Signed is the signed distribution manifest the binary embeds (ADR-060 decision
	// 20), and BuildInfo the build information of the binary, which ties the module
	// table to it (ADR-020 decision 12). Release is the release key of a release
	// binary, nil for any other (ADR-020 decision 13).
	Signed    []byte
	BuildInfo *debug.BuildInfo
	Release   *Release
	Limits    Limits
	Clock     Clock

	// Command is set by aiopsctl, whose cli row runs one command and requests the exit
	// with its code (ADR-061 decision 13).
	Command bool
	// Tools are the parts of the toolbox that only aiopsctl links, nil in aiops, where
	// each answers launch.ErrNotCarried; Shipped holds the bundles and profiles that the
	// binary embeds; Files is the file system, rooted at "/", that the toolbox reads the
	// absolute paths of a request from (ADR-061 decision 21). Each may be nil.
	Tools   launch.Tools
	Shipped fs.FS
	Files   fs.FS
	// Lint is the data of the lints of step 1 that the toolbox's ReadManifest runs, which
	// only aiopsctl hands in (ADR-061 decision 21).
	Lint ManifestLint

	// Publish receives the status at every change, one call at a time. Optional.
	Publish func(ProcessStatus)
	// Kernel is called once with the kernel of the run, after kernel.New and before
	// anything is mounted: a seam of the test binary (test/dist/cmd/aiops-test), which
	// records the kernel's fibers beside the state file. Nil in a release binary.
	Kernel func(*kernel.Root)
}

// Stdio are the three streams of the process.
type Stdio struct {
	In       io.Reader
	Out, Err io.Writer
}

// Run boots the composition and runs it until a signal, an exit request of a row
// or the end of ctx, then stops it, and returns the exit code. Boot is the five
// steps of 03-c3-kernel.md:355-361: compose, admit, static graph and required
// audit, prepare and mount the loader, settle and the run-time required audit.
//
// Goroutines: Run starts one, which turns a signal during boot into the end of the
// boot waits and returns when boot does. The clock's timers are the bounds of the
// settle, the shutdown and the leak grace period, each stopped by the step that made it.
func Run(ctx context.Context, o RunOptions) int { return newRun(o).main(ctx) }

// main runs and publishes the final status.
func (r *run) main(ctx context.Context) (code int) {
	defer func() {
		r.pub.update(func(s *ProcessStatus) { s.Phase, s.Ready, s.Exit = PhaseStopped, false, code })
	}()
	return r.run(ctx)
}

func newRun(o RunOptions) *run {
	r := &run{o: o, exits: make(chan launch.ExitRequest, 1), leakPing: make(chan struct{}, 1), moved: make(chan struct{}, 1)}
	r.out, r.errw = &syncWriter{w: o.Stdout}, &syncWriter{w: o.Stderr}
	r.pub = &publisher{fn: o.Publish, st: ProcessStatus{Phase: PhaseStarting, Profile: o.Profile}}
	return r
}

type run struct {
	o        RunOptions
	out      *syncWriter
	errw     *syncWriter
	pub      *publisher
	root     *kernel.Root
	cat      *catalog
	pc       *profileContext
	loader   *kernel.Fiber
	ready    *latch
	exits    chan launch.ExitRequest
	leakPing chan struct{}
	leakSeen atomic.Bool
	leakWhy  atomic.Pointer[string] // the first leak the observer saw, as a line for the log
	moved    chan struct{}          // pinged at each change of state, for waitUnloads
	reader   atomic.Pointer[reader]
	onEvent  func(kernel.LedgerEvent) // a seam of the tests: runs first in boot's observer
}

// reader is the launcher's own view of the tree: the composition key reads the rows
// with the state of each fiber (composition.Reader). The kernel gives the launcher
// the state of fibers by id and name, not by row, and a launcher that holds the
// Root cannot resolve a key, so a fiber of its own does.
type reader struct{ rd composition.Reader }

func (r *run) fail(code int, format string, a ...any) int {
	msg := fmt.Sprintf(format, a...)
	fmt.Fprintf(r.errw, "error: %s\n", msg)
	r.pub.update(func(s *ProcessStatus) { s.Detail = msg })
	return code
}

func (r *run) run(ctx context.Context) int {
	o := r.o
	if err := o.Limits.check(); err != nil {
		return r.fail(ExitRefused, "%v", err)
	}
	role, roles, err := parseRunRole(o.Role)
	if err != nil {
		code := ExitUsage
		if o.Command {
			code = ExitInternal
		}
		return r.fail(code, "%v", err)
	}
	cat, err := newCatalog(o.Registry)
	if err != nil {
		return r.fail(ExitRefused, "distribution: %v", err)
	}
	r.cat = cat
	loaderPlugin, ok := cat.plugins[LoaderID]
	if !ok || o.Registry.LoaderConfig == nil {
		return r.fail(ExitRefused, "distribution: the module table has no %s or no LoaderConfig", LoaderID)
	}
	signed, err := ReadDistribution("the embedded distribution manifest", o.Signed)
	if err != nil {
		return r.fail(ExitRefused, "distribution: %v", err)
	}
	mods := signed.place(o.Registry.Modules)
	dist := signed.Bindings()
	tie, tied := tieModules(signed, o.BuildInfo)

	// 1. Compose layers 0 to 4 (03-c3-kernel.md:355).
	// The server rule holds for every run but that of aiopsctl, whose cli profile is no server
	// profile and whose rows state no roles (ADR-061 decision 25).
	opt := Options{Server: !o.Command, Modules: mods, Locks: lockDigests(o.Home, o.Profile)}
	src := Sources{Profile: o.Profile, Home: o.Home, Bundles: o.Bundles, Patches: o.Patches}
	comp, warns, err := Compose(src, opt)
	if err != nil {
		return r.fail(ExitRefused, "%v", err)
	}
	for _, w := range warns {
		fmt.Fprintf(r.errw, "warning: %s\n", w)
	}
	if err := CheckReserved(comp); err != nil {
		return r.fail(ExitRefused, "%v", err)
	}
	// 2 and 3. Admit, then the static graph and the static required audit
	// (03-c3-kernel.md:357-359). The admitted tree holds the loader as a row, so that
	// the graph sees what it provides and the required row "loader"
	// (08-profiles-roadmap-adrs.md:455) is satisfied by boot's own mount. The gate
	// reads what boot holds (bootInput), whose trust state boot prints at every boot.
	in, lines := bootInput(o.Home, o.Profile, WithLoader(comp), mods, signed, o.Release, tie)
	for _, l := range append(lines, tied...) {
		fmt.Fprintln(r.errw, l)
	}
	a := AdmitWith(WithLoader(comp), in, roles)
	for _, l := range a.Diagnostics() {
		fmt.Fprintln(r.errw, l)
	}
	if a.Refused() {
		if !a.Gate.Refused && allRequiredSet(a.Errors) {
			return ExitRequired
		}
		return ExitRefused
	}
	r.pub.update(func(s *ProcessStatus) { s.Role, s.Digest = role, fmt.Sprintf("%x", comp.Digest[:]) })

	// 4. Build the kernel and prepare (03-c3-kernel.md:360).
	// The committed composition is what the loader mounts: the admitted copy, without
	// the loader's own row.
	committed := a.Admitted
	committed.Rows = committed.Rows[1:]
	pc := &profileContext{profile: o.Profile, role: role, src: src, opt: opt, mods: mods, dist: dist, roles: roles,
		signed: signed, release: o.Release, tie: tie, committed: committed, known: map[Digest]Composition{}, commits: make(chan struct{}, 1)}
	pc.box = &toolbox{Tools: cmp.Or[launch.Tools](o.Tools, launch.NoTools{}), p: pc, files: orNone(o.Files), shipped: orNone(o.Shipped), signed: o.Signed}
	r.ready = &latch{changed: func(v bool) { r.pub.update(func(s *ProcessStatus) { s.Ready = v }) }, booted: make(chan struct{})}
	cfg, err := r.kernelConfig(cat, dist)
	if err != nil {
		return r.fail(ExitRefused, "distribution: %v", err)
	}
	root, err := kernel.New(cfg)
	if err != nil {
		return r.fail(ExitInternal, "kernel: %v", err)
	}
	r.root, pc.root = root, root
	if r.o.Kernel != nil {
		r.o.Kernel(root)
	}
	if err := errors.Join(r.prepare(pc, cat), r.watch()); err != nil {
		_ = root.Close(context.WithoutCancel(ctx))
		return r.fail(ExitInternal, "prepare: %v", err)
	}
	// A stop request during boot ends the boot waits at once: a signal or the end of ctx
	// is not held up by the settle bound, the unload wait or the audit, and no audit
	// judges a tree that is being stopped.
	boot, stopBoot := context.WithCancel(ctx)
	defer stopBoot()
	signalled := make(chan bool, 1)
	//kernelcheck:allow-go the signal watch of boot, which returns when boot ends
	go func() {
		select {
		case <-o.Signals:
			signalled <- true
			stopBoot()
		case <-boot.Done():
			signalled <- false
		}
	}()
	loader := root.Plugin(loaderPlugin, o.Registry.LoaderConfig(entrySpecs(a.Admitted), root.Batch, cat.plugins))
	root.Plugin(r.auditPlugin(), nil)
	r.pc, r.loader = pc, loader

	// 5. Settle under a bound, then the required audit over the live tree
	// (03-c3-kernel.md:361).
	bctx, cancel := r.bound(boot, o.Limits.settleBound())
	serr := root.Settle(bctx)
	cancel()
	// Settle returns while a fiber that is blocked past its load deadline is still
	// UNLOADING, which is its contract; the row is FAILED once its Apply returns and the
	// partial effects unwind. The audit judges after that, within the bound of an unload.
	uctx, ucancel := r.bound(boot, o.Limits.DrainDeadline+o.Limits.DisposeDeadline)
	r.waitUnloads(uctx)
	ucancel()
	stopBoot()
	switch {
	case <-signalled:
		return r.stop(ctx, r.interrupted(), "signal", false)
	case ctx.Err() != nil:
		return r.stop(ctx, r.interrupted(), "context ended", false)
	}
	if serr != nil {
		fmt.Fprintf(r.errw, "warning: settle: %v\n", serr)
	}
	// The audit reads the tree whatever happens to ctx meanwhile: the run loop sees its end.
	// The status carries what it read, whatever the audit finds.
	v := r.publishRows(context.WithoutCancel(ctx), a.Graph)
	errs, wls := AuditRequired(a.Graph, a.Required, v)
	for _, w := range wls {
		fmt.Fprintf(r.errw, "warning: %s\n", w)
	}
	for _, e := range errs {
		fmt.Fprintf(r.errw, "error: %s\n", e.Error())
	}
	if len(errs) > 0 {
		return r.stop(ctx, ExitRequired, fmt.Sprintf("the required set is not met: %d finding(s)", len(errs)), false)
	}
	// The latch is the readiness row's, not boot's (03-c3-kernel.md:67): a distribution
	// that links no readiness row is never ready, and says so by its required set. Boot
	// has ended: Booted closes, and the cli row of aiopsctl dispatches (launch 0.2).
	close(r.ready.booted)
	r.pub.update(func(s *ProcessStatus) { s.Phase = PhaseRunning })
	fmt.Fprintf(r.out, "aiops: booted profile=%s role=%s\n", o.Profile, role)
	return r.loop(ctx)
}

// awaitErr returns the state of a fiber that has settled and its startup error.
func awaitErr(ctx context.Context, f *kernel.Fiber) (kernel.State, error) {
	st, _ := f.State(ctx)
	return st, f.Await(ctx)
}

func allRequiredSet(errs []StaticError) bool {
	for _, e := range errs {
		if e.Stage != "required set" {
			return false
		}
	}
	return true
}

// parseRunRole returns the role string the loader gets and the roles Admit gets: all
// maps to no roles, which selects every row.
func parseRunRole(s string) (string, []Role, error) {
	switch Role(s) {
	case "", Role(launch.RoleAll):
		return launch.RoleAll, nil, nil
	case RoleEdge, RoleAPI, RoleWorker:
		return s, []Role{Role(s)}, nil
	}
	return "", nil, fmt.Errorf("role %q is not one of edge, api, worker, all", s)
}

// bound returns a context that ends d from now on the launcher's clock, with the
// cause context.DeadlineExceeded, or when ctx ends.
func (r *run) bound(ctx context.Context, d time.Duration) (context.Context, func()) {
	c, cancel := context.WithCancelCause(ctx)
	stop := r.o.Clock.AfterFunc(d, func() { cancel(context.DeadlineExceeded) })
	return c, func() { stop(); cancel(nil) }
}

// auditPlugin is the launcher's one fiber: it resolves the composition key, which
// the launcher cannot do from the root, and so reads the state of each row. It is
// PENDING until the loader provides the key.
func (r *run) auditPlugin() kernel.Plugin {
	return kernel.Define(kernel.Plugin{
		ID:     "aiops/boot-audit",
		Inject: []kernel.Dep{kernel.Need(composition.Key)},
		Apply: func(c *kernel.Context, _ any) error {
			r.reader.Store(&reader{rd: composition.Key.Of(c)})
			return nil
		},
	})
}

// kernelConfig builds the kernel config: the limits, the two binding maps and the
// sealed hook listeners of the distribution manifest, resolved from plugin ids and
// key and hook names to values (03-c3-kernel.md:360; 07-c4-plugin-sdk.md:1358).
func (r *run) kernelConfig(cat *catalog, dist DistManifest) (kernel.Config, error) {
	l := r.o.Limits
	// aiopsctl keeps the warnings and the errors of the rows on stderr, and not their
	// progress (ADR-061 decision 25).
	level := slog.LevelInfo
	if r.o.Command {
		level = slog.LevelWarn
	}
	cfg := kernel.Config{
		LoadDeadline: l.LoadDeadline, DrainDeadline: l.DrainDeadline, DisposeDeadline: l.DisposeDeadline,
		ArchiveLimit: l.ArchiveLimit, EventWindow: l.EventWindow, ObserverQueue: l.ObserverQueue,
		OwnerInbox: l.OwnerInbox, TerminalLimit: l.TerminalLimit,
		ObserverLog: func(err error) { fmt.Fprintf(r.errw, "warning: observer: %v\n", err) },
		LogHandler:  slog.NewTextHandler(r.errw, &slog.HandlerOptions{Level: level}),
	}
	var errs []error
	resolve := func(side string, bs []Binding) kernel.Bindings {
		out := kernel.Bindings{}
		for _, b := range bs {
			k, ok := cat.keys[b.Key]
			if !ok {
				errs = append(errs, fmt.Errorf("the %s binding names key %q, which is not in the key table", side, b.Key))
				continue
			}
			for _, id := range b.Plugins {
				p, ok := cat.plugins[id]
				if !ok {
					errs = append(errs, fmt.Errorf("the %s binding of key %q names plugin %q, which is not in the module table", side, b.Key, id))
					continue
				}
				out[k] = append(out[k], p)
			}
		}
		return out
	}
	cfg.Providers, cfg.Consumers = resolve("provider", dist.Providers), resolve("consumer", dist.Consumers)
	cfg.Listeners = map[kernel.AnyHook][]kernel.Plugin{}
	hooks := map[string]kernel.AnyHook{}
	for _, h := range kernelHooks() {
		hooks[h.Name()] = h
	}
	maps.Copy(hooks, r.o.Registry.Hooks)
	for _, h := range dist.SealedHooks {
		hook, ok := hooks[h.Hook]
		if !ok {
			errs = append(errs, fmt.Errorf("the sealed hook %q is not in Registry.Hooks", h.Hook))
			continue
		}
		for _, id := range h.Plugins {
			p, ok := cat.plugins[id]
			if !ok {
				errs = append(errs, fmt.Errorf("the listeners of hook %q name plugin %q, which is not in the module table", h.Hook, id))
				continue
			}
			cfg.Listeners[hook] = append(cfg.Listeners[hook], p)
		}
	}
	return cfg, errors.Join(errs...)
}

// prepare provides the six launch services from the root context, which may
// provide a provider-bound key (07-c4-plugin-sdk.md:1359).
func (r *run) prepare(pc *profileContext, cat *catalog) error {
	o := r.o
	c := r.root.Context()
	return errors.Join(
		launch.ProfileContextKey.Provide(c, pc),
		launch.CatalogKey.Provide(c, cat),
		launch.CmdlineKey.Provide(c, cmdline{args: o.Args, flags: o.Flags, env: envOf(o.Env), stdio: o.Stdio, dir: o.Dir, environ: o.Environ}),
		launch.ReadyKey.Provide(c, r.ready),
		launch.KillSwitchesKey.Provide(c, switches(ReadKillSwitches(envOf(o.Env)))),
		launch.AppExitKey.Provide(c, appExit{reqs: r.exits}),
	)
}

func envOf(f func(string) string) func(string) string {
	if f == nil {
		return func(string) string { return "" }
	}
	return f
}

// watch subscribes to the events of the kernel. A drain or a dispose that passed its
// deadline marks the fiber Leaked, and a goroutine cannot be killed
// (03-c3-kernel.md:372; 07-c4-plugin-sdk.md:359): the callback keeps what the event
// says, sets a flag and pings the run loop. The event is the record, not the tree: a
// fiber whose goroutine returns after the deadline is archived and is no longer
// listed as live, and it has still leaked. A change of state wakes the wait for the
// unloads of boot, which reads the tree itself. The kernel detaches whichever observer
// it is inside when its queue passes Limits.ObserverQueue (ADR-034, "2026-11-19 bounded
// observer queue and overflow rule"), a fast one too: boot's subscribes again and reads
// the tree for the leaks whose events it lost.
func (r *run) watch() error {
	_, err := r.root.Observe("boot-watch", func(_ context.Context, e kernel.LedgerEvent) error {
		if r.onEvent != nil {
			r.onEvent(e)
		}
		switch {
		case e.Kind == "state":
			ping(r.moved)
		case e.Kind == "deadline" && (e.Label == "dispose" || e.Label == "drain"):
			r.leak(fmt.Sprintf("fiber %d %s passed its %s deadline", e.Fiber, e.Plugin, e.Label))
		}
		return nil
	}, kernel.OnOverflow(func(ctx context.Context, _ *kernel.ObserverOverflow) error { return r.resync(ctx) }))
	return err
}

// resync subscribes boot's observer again after an overflow, then reads the tree: a
// fiber marked Leaked may be a leak whose event was lost. Root.Observe registers no
// effect on the root (ADR-059 decision 21), so a resync leaves nothing behind.
func (r *run) resync(ctx context.Context) error {
	werr := r.watch() // first, so that no event falls between the two
	ins, err := r.root.Inspect(ctx)
	for _, f := range ins.Fibers {
		if f.Leaked {
			r.leak(fmt.Sprintf("fiber %d %s passed its drain or dispose deadline", f.ID, f.Plugin))
		}
	}
	ping(r.moved)
	return errors.Join(werr, err)
}

// leak records the first leak for the log and pings the run loop. A leak changes no
// state, so it also wakes boot's wait for the unloads, which a leaked fiber ends.
func (r *run) leak(why string) {
	r.leakWhy.CompareAndSwap(nil, &why)
	r.leakSeen.Store(true)
	ping(r.leakPing)
	ping(r.moved)
}

func ping(c chan struct{}) {
	select {
	case c <- struct{}{}:
	default:
	}
}

// waitUnloads returns when no fiber is UNLOADING, other than one the kernel gave up on
// (Leaked), or when ctx ends. It reads the tree, and between two reads waits for a
// change of state.
func (r *run) waitUnloads(ctx context.Context) {
	for {
		ins, err := r.root.Inspect(ctx)
		if err != nil || !slices.ContainsFunc(ins.Fibers, func(f kernel.FiberSnapshot) bool { return f.State == kernel.Unloading && !f.Leaked }) {
			return
		}
		select {
		case <-r.moved:
		case <-ctx.Done():
			return
		}
	}
}

// view reads the live tree as the audit wants it: the state of each row by id, and
// the keys that have an ACTIVE provider in the default realm. The six launch keys
// are always provided: the launcher provided them before any row mounted.
func (r *run) view(ctx context.Context, loader *kernel.Fiber) StateView {
	ins, _ := r.root.Inspect(ctx) // an error leaves the view empty, which the audit reports as unmet
	v := StateView{Rows: map[string]State{}, Providers: map[string]bool{}}
	for _, k := range rootKeyNames() {
		v.Providers[k] = true
	}
	for _, p := range ins.Provisions {
		if name, realm, _ := strings.Cut(p.Key, "@"); p.Active && !strings.Contains(realm, "/") {
			v.Providers[name] = true
		}
	}
	// The loader is the row "loader" of the required set; it is not a row it serves.
	st, lerr := awaitErr(ctx, loader)
	v.Rows[LoaderRowID] = State(st)
	if lerr != nil {
		fmt.Fprintf(r.errw, "error: loader: %v\n", lerr)
	}
	if rd := r.reader.Load(); rd != nil {
		g, _ := rd.rd.Committed(composition.ScopeProcess)
		for _, row := range g.Rows {
			v.Rows[row.ID] = State(row.State)
		}
	}
	return v
}

// loop waits for the end of the process (03-c3-kernel.md:363-372): a signal, a
// request of appExit, the end of ctx, or a leaked fiber, which drops readiness and
// replaces the process after the grace period.
func (r *run) loop(ctx context.Context) int {
	var graceUp <-chan struct{}
	for {
		select {
		case <-r.o.Signals:
			return r.stop(ctx, r.interrupted(), "signal", false)
		case <-ctx.Done():
			return r.stop(ctx, r.interrupted(), "context ended", false)
		case req := <-r.exits:
			if req.Reason == launch.ExitCommand && r.o.Command && req.Code >= ExitOK && req.Code <= ExitUsage {
				return r.stop(ctx, req.Code, fmt.Sprintf("%s: %s", req.Reason, req.Detail), true)
			}
			return r.stop(ctx, exitCodeOf(req.Reason), fmt.Sprintf("process replaced: %s: %s", req.Reason, req.Detail), false)
		case <-r.leakPing:
			if graceUp == nil {
				r.ready.end()
				fmt.Fprintf(r.errw, "error: leaked: %s; readiness ended, the process is replaced after the drain window\n", *r.leakWhy.Load())
				done := make(chan struct{})
				stop := r.o.Clock.AfterFunc(r.o.Limits.leakGrace(), func() { close(done) })
				defer stop()
				graceUp = done
			}
		case <-graceUp:
			return r.stop(ctx, ExitLeak, "process replaced: dispose-leak: a fiber leaked past its drain or dispose deadline", false)
		case <-r.pc.commits:
			r.pc.mu.Lock()
			c := r.pc.committed
			r.pc.mu.Unlock()
			g, _, _ := BuildGraph(WithLoader(c), r.pc.mods, r.pc.dist, r.pc.roles)
			r.publishRows(ctx, g)
		}
	}
}

// publishRows reads the tree and publishes the rows that g mounts with their states,
// and returns what it read.
func (r *run) publishRows(ctx context.Context, g Graph) StateView {
	v := r.view(ctx, r.loader)
	r.pub.update(func(s *ProcessStatus) { s.Rows = rowStates(g, v) })
	return v
}

// interrupted is the code of a stop that a signal or the end of ctx began.
func (r *run) interrupted() int {
	if r.o.Command {
		return ExitInterrupted
	}
	return ExitOK
}

// exitCodeOf is the code of an exit request other than a command's: ExitCommand with no
// Command, or with a code other than 0, 1 and 2, is a launcher failure.
func exitCodeOf(why launch.ExitReason) int {
	switch why {
	case launch.ExitDisposeLeak:
		return ExitLeak
	case launch.ExitGaveUp:
		return ExitGaveUp
	case launch.ExitRestoreFail:
		return ExitRestore
	}
	return ExitInternal
}

// stop is the bounded shutdown: drop readiness, close the kernel under the
// shutdown bound, report. why is one line. A shutdown that overran its bound, or
// that saw a fiber leak, is a replacement, not a clean stop: it replaces ExitOK and the
// code of a command, which command marks.
func (r *run) stop(ctx context.Context, code int, why string, command bool) int {
	r.ready.end()
	r.pub.update(func(s *ProcessStatus) { s.Phase, s.Detail = PhaseStopping, why })
	if code == ExitOK {
		fmt.Fprintf(r.errw, "aiops: stopping: %s\n", why)
	} else {
		fmt.Fprintf(r.errw, "aiops: exit %d: %s\n", code, why)
	}
	bctx, cancel := r.bound(context.WithoutCancel(ctx), r.o.Limits.ShutdownBound)
	err := r.root.Close(bctx)
	timedOut := errors.Is(context.Cause(bctx), context.DeadlineExceeded)
	cancel()
	if err != nil {
		fmt.Fprintf(r.errw, "warning: shutdown: %v\n", err)
	}
	// The observer of the root sees the events of Close, and the diagnostic of Close
	// says it too.
	leaked := r.leakSeen.Load() || errors.Is(err, kernel.ErrLeaked)
	if (code == ExitOK || command) && (timedOut || leaked) {
		// The detail says why the process asks to be replaced, not what began the stop.
		code, why = ExitLeak, fmt.Sprintf("shutdown did not finish cleanly: the bound of %s passed", r.o.Limits.ShutdownBound)
		if leaked {
			why = "shutdown did not finish cleanly: a fiber leaked"
			if w := r.leakWhy.Load(); w != nil {
				why += ": " + *w
			}
		}
		fmt.Fprintf(r.errw, "aiops: exit %d: %s\n", code, why)
	}
	r.pub.update(func(s *ProcessStatus) { s.Detail = why })
	return code
}

// syncWriter serialises writes: the logger, the observer log and Run write to one
// stream from several goroutines.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	if s.w == nil {
		return len(p), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// publisher serialises the calls to RunOptions.Publish and keeps the last status.
type publisher struct {
	mu sync.Mutex
	fn func(ProcessStatus)
	st ProcessStatus
}

func (p *publisher) update(f func(*ProcessStatus)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	f(&p.st)
	if p.fn != nil {
		p.fn(p.st)
	}
}
