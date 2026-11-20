package boot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
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
// package's and an open point.
const (
	ExitOK       = 0 // stopped by a signal or by the caller's context, tree disposed in time
	ExitRefused  = 1 // boot stopped before any row mounted: a layer, the gate, the static graph, the distribution
	ExitUsage    = 2 // bad flags; cmd/aiops returns it (it is also the code of a Go panic)
	ExitRequired = 3 // the required set is not met once the tree settled (03-c3-kernel.md:361)
	ExitLeak     = 4 // replaced: a fiber leaked past its drain or dispose deadline, or shutdown overran its bound
	ExitGaveUp   = 5 // replaced: restart-policy gave up on an infrastructure provider
	ExitInternal = 6 // the launcher failed: the kernel refused its config, a launch key could not be provided
	ExitRestore  = 7 // replaced: a restore failed
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
	return Composition{Rows: append([]ComposedRow{row}, c.Rows...), Digest: c.Digest}
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
	// LoaderConfig builds the config of the loader plugin from the rows to mount
	// and the batch function (loader.Config{Rows, Batch}). boot may not import the
	// loader package (08-profiles-roadmap-adrs.md:1252), so the module table, which
	// may, carries this one function.
	LoaderConfig func(rows []loaderdef.EntrySpec, batch BatchFunc) any
}

// kernelHooks are the sealed hooks of the kernel itself. Only the entries that
// Config.Listeners lists may listen on them (ADR-034, "S1 additions", G5).
func kernelHooks() []kernel.AnyHook {
	return []kernel.AnyHook{kernel.InternalPlugin, kernel.InternalStatus, kernel.InternalService,
		kernel.InternalConfig, kernel.InternalUpdate, kernel.InternalCommit}
}

// Clock is the clock of the launcher's own bounds. It is not the clock key: the
// launcher runs before any row.
type Clock interface {
	// AfterFunc calls f once, d from now, on a goroutine of the clock's own, and
	// returns the function that stops it.
	AfterFunc(d time.Duration, f func()) (stop func() bool)
}

type systemClock struct{}

func (systemClock) AfterFunc(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop }

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
// cmd/aiops writes it to a file in the runtime directory of the home. It is an
// open point.
type ProcessStatus struct {
	Phase   Phase
	Ready   bool
	Profile string
	Role    string
	Digest  string // hex of the composition the process booted
	Exit    int    // set in PhaseStopped
	Detail  string // why the process stopped, one line
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

	Stdout, Stderr io.Writer

	Registry Registry
	Dist     DistManifest
	Limits   Limits
	Clock    Clock

	// Publish receives the status at every change, one call at a time. Optional.
	Publish func(ProcessStatus)
}

// Run boots the composition and runs it until a signal, an exit request of a row
// or the end of ctx, then stops it, and returns the exit code. Boot is the five
// steps of 03-c3-kernel.md:355-361: compose, admit, static graph and required
// audit, prepare and mount the loader, settle and the run-time required audit.
//
// Goroutines: Run starts none. The clock's timers are the bounds of the settle,
// the shutdown and the leak grace period, each stopped by the step that made it.
func Run(ctx context.Context, o RunOptions) int { return newRun(o).main(ctx) }

// main runs and publishes the final status.
func (r *run) main(ctx context.Context) (code int) {
	defer func() {
		r.pub.update(func(s *ProcessStatus) { s.Phase, s.Ready, s.Exit = PhaseStopped, false, code })
	}()
	return r.run(ctx)
}

func newRun(o RunOptions) *run {
	r := &run{o: o, exits: make(chan launch.ExitRequest, 1), leakPing: make(chan struct{}, 1)}
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
	ready    *latch
	exits    chan launch.ExitRequest
	leakPing chan struct{}
	leakSeen atomic.Bool
	leakWhy  atomic.Pointer[string] // the first leak the observer saw, as a line for the log
	unload   unloads
	// refsActive says config-refs is ACTIVE: the availability of configRefsReady.
	refsActive atomic.Bool
	reader     atomic.Pointer[reader]
	afterNew   func(*kernel.Root) // a seam of the tests: runs between kernel.New and prepare
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
		return r.fail(ExitUsage, "%v", err)
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
	mods := o.Registry.Modules
	dist := o.Dist

	// 1. Compose layers 0 to 4 (03-c3-kernel.md:355).
	opt := Options{Server: true, Modules: mods}
	src := Sources{Profile: o.Profile, Home: o.Home, Bundles: o.Bundles, Patches: o.Patches}
	comp, warns, err := Compose(src, opt)
	if err != nil {
		return r.fail(ExitRefused, "%v", err)
	}
	for _, w := range warns {
		fmt.Fprintf(r.errw, "warning: %s\n", w)
	}
	if _, clash := findRow(comp, LoaderRowID); clash {
		return r.fail(ExitRefused, "the row id %q is reserved: boot mounts the loader and no bundle holds it (03-c3-kernel.md:65)", LoaderRowID)
	}
	// 2 and 3. Admit, then the static graph and the static required audit
	// (03-c3-kernel.md:357-359). The admitted tree holds the loader as a row, so that
	// the graph sees what it provides and the required row "loader"
	// (08-profiles-roadmap-adrs.md:455) is satisfied by boot's own mount.
	a := Admit(WithLoader(comp), mods, dist, roles)
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
	pc := &profileContext{profile: o.Profile, role: role, src: src, opt: opt, mods: mods, dist: dist, roles: roles,
		committed: comp, known: map[Digest]Composition{}}
	r.ready = &latch{changed: func(v bool) { r.pub.update(func(s *ProcessStatus) { s.Ready = v }) }}
	cfg, err := r.kernelConfig(cat, dist)
	if err != nil {
		return r.fail(ExitRefused, "distribution: %v", err)
	}
	root, err := kernel.New(cfg)
	if err != nil {
		return r.fail(ExitInternal, "kernel: %v", err)
	}
	r.root = root
	if r.afterNew != nil {
		r.afterNew(root)
	}
	if err := errors.Join(r.prepare(pc, cat), r.watch()); err != nil {
		_ = root.Close(context.WithoutCancel(ctx))
		return r.fail(ExitInternal, "prepare: %v", err)
	}
	loader := root.Plugin(loaderPlugin, o.Registry.LoaderConfig(entrySpecs(a.Admitted), root.Batch))
	root.Plugin(r.auditPlugin(), nil)

	// 5. Settle under a bound, then the required audit over the live tree
	// (03-c3-kernel.md:361).
	bctx, cancel := r.bound(ctx, o.Limits.settleBound())
	_ = root.Settle(bctx) // the verdict is the second one's: a blocked fiber is still blocked then
	// config-refs becoming ACTIVE makes configRefsReady available through the observer
	// of watch, which runs apart from the tree: Settle may return before it has told the
	// kernel, with a row that holds a reference still PENDING for the key, and the audit
	// would stop a healthy boot. Wait for the observer, then settle what it set going.
	_ = root.AwaitObservers(bctx) // an overflow only says events were lost; the audit reads the tree itself
	serr := root.Settle(bctx)
	cancel()
	// Settle returns while a fiber that is blocked past its load deadline is still
	// UNLOADING, which is its contract; the row is FAILED once its Apply returns and the
	// partial effects unwind. The audit judges after that, within the bound of an unload.
	uctx, ucancel := r.bound(ctx, o.Limits.DrainDeadline+o.Limits.DisposeDeadline)
	_ = root.AwaitObservers(uctx)
	r.unload.wait(uctx)
	ucancel()
	if serr != nil {
		fmt.Fprintf(r.errw, "warning: settle: %v\n", serr)
	}
	errs, wls := AuditRequired(a.Graph, a.Required, r.view(ctx, loader))
	for _, w := range wls {
		fmt.Fprintf(r.errw, "warning: %s\n", w)
	}
	for _, e := range errs {
		fmt.Fprintf(r.errw, "error: %s\n", e.Error())
	}
	if len(errs) > 0 {
		return r.stop(ctx, ExitRequired, fmt.Sprintf("the required set is not met: %d finding(s)", len(errs)))
	}
	// The latch is the readiness row's, not boot's (03-c3-kernel.md:67): a distribution
	// that links no readiness row is never ready, and says so by its required set.
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
	cfg := kernel.Config{
		LoadDeadline: l.LoadDeadline, DrainDeadline: l.DrainDeadline, DisposeDeadline: l.DisposeDeadline,
		ArchiveLimit: l.ArchiveLimit, EventWindow: l.EventWindow, ObserverQueue: l.ObserverQueue,
		OwnerInbox: l.OwnerInbox, TerminalLimit: l.TerminalLimit,
		ObserverLog: func(err error) { fmt.Fprintf(r.errw, "warning: observer: %v\n", err) },
		LogHandler:  slog.NewTextHandler(r.errw, &slog.HandlerOptions{Level: slog.LevelInfo}),
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
		launch.CmdlineKey.Provide(c, cmdline{args: o.Args, flags: o.Flags, env: envOf(o.Env)}),
		launch.ReadyKey.Provide(c, r.ready),
		launch.KillSwitchesKey.Provide(c, switches(ReadKillSwitches(envOf(o.Env)))),
		launch.AppExitKey.Provide(c, appExit{reqs: r.exits}),
		configRefsKey.Provide(c, struct{}{}, kernel.Available(func(*kernel.Context) bool { return r.refsActive.Load() })),
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
// listed as live, and it has still leaked. A change of state of config-refs makes configRefsReady
// available or not, and the kernel tells the rows that wait for it.
func (r *run) watch() error {
	_, err := r.root.Observe("boot-watch", func(ctx context.Context, e kernel.LedgerEvent) error {
		switch {
		case e.Kind == "state":
			r.unload.set(e.Fiber, e.State == kernel.Unloading)
			if e.Plugin == ConfigRefsID {
				if active := e.State == kernel.Active; r.refsActive.Swap(active) != active {
					err := r.root.Notify(ctx, configRefsKey)
					if ctx.Err() != nil {
						return nil // Close: the observer of the root sees its events with the context of the shutdown
					}
					return err
				}
			}
		case e.Kind == "deadline" && (e.Label == "dispose" || e.Label == "drain"):
			r.unload.set(e.Fiber, false) // the kernel gave up on it: it is not waited for
			why := fmt.Sprintf("fiber %d %s passed its %s deadline", e.Fiber, e.Plugin, e.Label)
			r.leakWhy.CompareAndSwap(nil, &why)
			r.leakSeen.Store(true)
			select {
			case r.leakPing <- struct{}{}:
			default:
			}
		}
		return nil
	})
	return err
}

// unloads is the set of the fibers that are UNLOADING, kept from the state events that
// watch sees, and a way to wait until it is empty.
type unloads struct {
	mu    sync.Mutex
	open  map[uint64]bool
	moved chan struct{} // closed, and replaced, at each change
}

func (u *unloads) set(fiber uint64, unloading bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.open[fiber] == unloading {
		return
	}
	if u.open == nil {
		u.open = map[uint64]bool{}
	}
	if unloading {
		u.open[fiber] = true
	} else {
		delete(u.open, fiber)
	}
	if u.moved != nil {
		close(u.moved)
		u.moved = nil
	}
}

// wait returns when no fiber is UNLOADING, or when ctx ends.
func (u *unloads) wait(ctx context.Context) {
	for {
		u.mu.Lock()
		if len(u.open) == 0 {
			u.mu.Unlock()
			return
		}
		if u.moved == nil {
			u.moved = make(chan struct{})
		}
		moved := u.moved
		u.mu.Unlock()
		select {
		case <-moved:
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
			return r.stop(ctx, ExitOK, "signal")
		case <-ctx.Done():
			return r.stop(ctx, ExitOK, "context ended")
		case req := <-r.exits:
			return r.stop(ctx, exitCodeOf(req.Reason), fmt.Sprintf("process replaced: %s: %s", req.Reason, req.Detail))
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
			return r.stop(ctx, ExitLeak, "process replaced: dispose-leak: a fiber leaked past its drain or dispose deadline")
		}
	}
}

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
// that saw a fiber leak, is a replacement, not a clean stop.
func (r *run) stop(ctx context.Context, code int, why string) int {
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
	if code == ExitOK && (timedOut || leaked) {
		code = ExitLeak
		fmt.Fprintf(r.errw, "aiops: exit %d: shutdown did not finish cleanly: a fiber leaked or the bound of %s passed\n", code, r.o.Limits.ShutdownBound)
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
