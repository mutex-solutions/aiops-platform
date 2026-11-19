// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"testing"
	"time"
)

// Plugin is one entry of the module table: plain data plus two functions
// (07-c4-plugin-sdk.md:192, :236). Its entry is the token that Define stamps into
// it (ADR-034 owner decision 7), so a Plugin that was never defined has no entry
// of its own.
type Plugin struct {
	ID       string                          // "<owner>/<name>", the fiber's name in diagnostics.
	Inject   []Dep                           // Data; the Needs gate activation and join the epoch.
	Provides []AnyKey                        // Provide of a key not listed here fails.
	Hooks    []HookUse                       // Every hook Apply may listen on, with its stage; an undeclared On fails.
	Decode   func(raw any) (any, error)      // Optional (07-c4-plugin-sdk.md:192; ADR-034 owner decision 9): turns the raw config into what Apply receives. It must be pure: it also runs on an update of an active fiber before the restart.
	Apply    func(c *Context, cfg any) error // Everything it registers is an effect.

	entry entryID // Minted by Define; nil for a Plugin with no entry of its own.
}

// Config is every number the kernel needs, plus the three binding maps. The kernel
// holds no default: the launcher supplies all of it (ADR-034 owner decisions 3
// and 4). The deadlines are kernel-wide.
type Config struct {
	LoadDeadline    time.Duration // Apply and an availability check must return within it.
	DrainDeadline   time.Duration // A quiesced fiber's leases and tracked work must end within it.
	DisposeDeadline time.Duration // The whole effect walk of one fiber, and one scope's rollback.
	ArchiveLimit    int           // Recently disposed fibers kept, oldest evicted first.
	EventWindow     int           // Most recent events kept in the ledger tail.
	ObserverQueue   int           // Events that may wait behind a stalled observer.
	OwnerInbox      int           // Capacity of the owner's inbox: external backpressure.
	TerminalLimit   int           // Diagnostics of past failed fibers that Settle and Close keep.
	ObserverLog     func(error)   // Optional: receives each contained observer failure.
	// LogHandler is the one slog.Handler of the built-in logger (plugin record :55,
	// :540). Optional: nil discards. The contract names no such field: an open point.
	LogHandler slog.Handler

	// Providers lists, per provider-bound key, the entries allowed to provide it;
	// Consumers lists, per consumer-bound or signed key, the entries allowed to
	// resolve it (07-c4-plugin-sdk.md:551). Both are checked by New.
	Providers, Consumers Bindings

	// Listeners lists, per sealed hook, the entries allowed to listen on it: the
	// third binding map, compared by entry token like the other two. The launcher
	// fills it from the distribution manifest. A hook that is not sealed, a nil
	// hook and an entry that was never defined or is listed twice are refused by
	// New. NewForTest ignores it (ADR-034 owner decision 11).
	Listeners map[AnyHook][]Plugin
}

func (c Config) core() coreConfig {
	return coreConfig{
		loadDeadline: c.LoadDeadline, drainDeadline: c.DrainDeadline, disposeDeadline: c.DisposeDeadline,
		archiveLimit: c.ArchiveLimit, eventWindow: c.EventWindow, observerQueue: c.ObserverQueue,
		ownerInbox: c.OwnerInbox, terminalLimit: c.TerminalLimit, observerLog: c.ObserverLog,
	}
}

// Root is the kernel root: it owns the lifecycle core, the three binding maps and
// the root context (07-c4-plugin-sdk.md:1346, :1358).
type Root struct {
	core      *core
	providers bindingSet
	consumers bindingSet
	listeners listenerSet
	unbound   bool
	host      entryID
	ctx       *Context
	aud       Audience // Reaches the listeners of this kernel only: see config.go.
}

// New constructs a kernel with the three binding maps of cfg (07-c4-plugin-sdk.md:1358;
// ADR-034 owner decision 11). It fails on a zero or negative number, and on a
// binding map that names an invalid key or hook, an open key, a hook that is not
// sealed, a key or an entry twice or an entry that was never defined. Close it.
func New(cfg Config) (*Root, error) {
	providers, perr := compileBindings("provider", cfg.Providers, func(k *keyData) bool { return k.provider == classBound })
	consumers, cerr := compileBindings("consumer", cfg.Consumers, func(k *keyData) bool { return k.consumer != classOpen })
	listeners, lerr := compileListeners(cfg.Listeners)
	if err := errors.Join(perr, cerr, lerr); err != nil {
		return nil, err
	}
	return newRoot(context.Background(), cfg.core(), providers, consumers, listeners, false, cfg.LogHandler)
}

// NewForTest constructs a kernel with the three binding maps off, so that a plugin
// can mount alone with a fake of any key and listen on any sealed hook
// (07-c4-plugin-sdk.md:247, :564; ADR-034 owner decisions 1 and 11). It is the only
// constructor that can, and it needs a testing.TB. The bindings of cfg are ignored.
// A plugin must still declare a hook in Plugin.Hooks, and Bind still checks the
// owning key. The kernel is closed when the test ends: a leaked
// fiber fails the test and any other Close diagnostic, such as that of a fiber that
// failed on purpose, is logged. A bad cfg fails the test at once.
func NewForTest(tb testing.TB, cfg Config) *Root {
	tb.Helper()
	r, err := newRoot(context.Background(), cfg.core(), nil, nil, nil, true, cfg.LogHandler)
	if err != nil {
		tb.Fatalf("kernel.NewForTest: %v", err)
	}
	tb.Cleanup(func() {
		switch err := r.Close(context.Background()); {
		case errors.Is(err, errLeaked):
			tb.Errorf("kernel close: %v", err)
		case err != nil:
			tb.Logf("kernel close: %v", err)
		}
	})
	return r
}

func newRoot(ctx context.Context, cfg coreConfig, providers, consumers bindingSet, listeners listenerSet, unbound bool, log slog.Handler) (*Root, error) {
	c, err := newCore(cfg)
	if err != nil {
		return nil, err
	}
	h, err := c.rootHandle(ctx)
	if err != nil {
		_ = c.close(context.Background())
		return nil, err
	}
	r := &Root{core: c, providers: providers, consumers: consumers, listeners: listeners, unbound: unbound}
	r.host = new(entryToken)
	r.ctx = &Context{root: r, h: h, entry: r.host, frame: &frame{id: "root", host: true}}
	r.aud = Audience{match: func(l *Context) bool { return l.root == r }}
	if err := installBuiltins(r, log); err != nil {
		_ = c.close(context.Background())
		return nil, err
	}
	return r, nil
}

// Context returns the root context, the launcher's own. Never hand it to a plugin:
// it may provide any key, including a provider-bound one, as the launcher does for
// the launch keys (07-c4-plugin-sdk.md:1359).
func (r *Root) Context() *Context { return r.ctx }

// Plugin mounts p on the root context (07-c4-plugin-sdk.md:197, :1360).
func (r *Root) Plugin(p Plugin, cfg any) *Fiber { return r.ctx.Plugin(p, cfg) }

// Settle returns when no fiber is mid-transition except fibers blocked past one of
// their own deadlines. It returns nil when nothing is blocked, otherwise one joined
// diagnostic per blocked fiber. If ctx ends first it returns ctx's error joined
// with the diagnostics known so far (docs/design/plugin-system-2026-11.md:57).
func (r *Root) Settle(ctx context.Context) error { return r.core.settle(ctx) }

// Close disposes every fiber, then the root. It waits as Settle does and always
// releases what it can once the kernel has drained, even when ctx ended first.
func (r *Root) Close(ctx context.Context) error { return r.core.close(ctx) }

// BatchOp is one operation of Batch.
type BatchOp struct {
	kind   batchKind
	plugin Plugin
	cfg    any
	fiber  *Fiber
}

// MountOp is the operation that mounts p, like Root.Plugin, as a top-level
// fiber.
func MountOp(p Plugin, cfg any) BatchOp { return BatchOp{kind: batchMount, plugin: p, cfg: cfg} }

// UpdateOp is the operation that restarts f with the config it retains. The
// contract gives it no shape; it runs no config hook and decodes at activation.
func UpdateOp(f *Fiber) BatchOp { return BatchOp{kind: batchUpdate, fiber: f} }

// DisposeOp is the operation that disposes f, like Fiber.Dispose.
func DisposeOp(f *Fiber) BatchOp { return BatchOp{kind: batchDispose, fiber: f} }

var errForeignFiber = errors.New("fiber belongs to another kernel")

// Batch commits all of ops as one desired state before any resulting work is
// admitted, so two updates cannot start an intermediate activation
// (docs/design/plugin-system-2026-11.md:57). It returns one Fiber per operation, in order, or an
// error and no effect. The contract names Batch but not its shape; this one maps
// the core's batch directly.
func (r *Root) Batch(ctx context.Context, ops ...BatchOp) ([]*Fiber, error) {
	native := make([]batchOp, len(ops))
	cells := make([]*cfgCell, len(ops))
	for i, op := range ops {
		native[i].Kind = op.kind
		if op.kind == batchMount {
			native[i].Spec, cells[i] = r.ctx.mountSpec(op.plugin, op.cfg)
			continue
		}
		if op.fiber == nil {
			return nil, fmt.Errorf("batch operation %d: %w", i, errUnknown)
		}
		if op.fiber.ref.c != r.core {
			return nil, fmt.Errorf("batch operation %d: %w", i, errForeignFiber)
		}
		native[i].Fiber = op.fiber.ref.id
	}
	ids, err := r.core.batch(ctx, native)
	if err != nil {
		return nil, err
	}
	out := make([]*Fiber, len(ops))
	for i, op := range ops {
		if op.kind == batchMount {
			out[i] = &Fiber{ref: r.core.fiber(ids[i]), cell: cells[i]}
		} else {
			out[i] = op.fiber
		}
	}
	return out, nil
}

// Fiber is the running instance of a plugin: six states and an epoch
// (07-c4-plugin-sdk.md:205, :239). It holds no state of its own and stays usable
// after the fiber is disposed, for as long as the kernel keeps its record.
type Fiber struct {
	ref  fiberRef
	cell *cfgCell // The fiber's config; nil for a fiber that takes none (When).
}

// State returns the fiber's current state.
func (f *Fiber) State(ctx context.Context) (State, error) { return f.ref.state(ctx) }

// Await returns when the fiber is not mid-transition, with its retained startup
// error. It must not be called from inside the fiber's own callbacks.
func (f *Fiber) Await(ctx context.Context) error { return f.ref.await(ctx) }

// Dispose requests disposal of the fiber. It is idempotent and does not wait for
// the cleanup; use Await for that.
func (f *Fiber) Dispose(ctx context.Context) error { return f.ref.dispose(ctx) }

// Effects returns the labels of the fiber's effect ledger in registration order
// (07-c4-plugin-sdk.md:343).
func (f *Fiber) Effects(ctx context.Context) ([]string, error) {
	s, err := f.ref.snapshot(ctx)
	return s.Effects, err
}

// Plugin mounts p as a child fiber of c's fiber, an effect of c, and returns it
// (07-c4-plugin-sdk.md:197, :367). The child gets the entry of p when p was defined
// and inherits c's entry when it was not (07-c4-plugin-sdk.md:253, :563; ADR-034
// owner decision 7), so a child that copies the ID of a bound plugin gains nothing. A child whose declarations are refused, such as a Need on a key its
// entry may not resolve, is mounted failed and says why. A mount on an inactive c
// panics and fails c's fiber, with ErrInactiveEffect.
func (c *Context) Plugin(p Plugin, cfg any) *Fiber {
	spec, cell := c.mountSpec(p, cfg)
	return c.spawn(spec, cell)
}

// When mounts a child fiber that runs body once every Need of deps has an active
// provider, the only optional form (07-c4-plugin-sdk.md:198, :470). The contract
// gives it no id; the fiber is named after c's fiber.
func (c *Context) When(deps []Dep, body func(*Context) error) *Fiber {
	return c.spawn(c.compile(c.frame.id+"/when", c.entry, deps, nil, body, c.frame.hooks...), nil)
}

// mountSpec returns the spec that mounts p with the config cfg and the cell that
// keeps the config for later updates. Apply gets what the config pipeline makes of
// the cell's raw value at each activation (config.go).
func (c *Context) mountSpec(p Plugin, cfg any) (pluginSpec, *cfgCell) {
	cell := &cfgCell{root: c.root, raw: cfg, decode: p.Decode}
	var apply func(*Context) error
	if p.Apply != nil {
		apply = func(cc *Context) error {
			v, err := cell.prepare(cc.Std())
			if err != nil {
				return err
			}
			return p.Apply(cc, v)
		}
	}
	entry := p.entry
	if entry == nil {
		entry = c.entry
	}
	return c.compile(p.ID, entry, p.Inject, p.Provides, apply, p.Hooks...), cell
}

func (c *Context) spawn(spec pluginSpec, cell *cfgCell) *Fiber {
	id, err := c.h.mountChild(spec)
	if err != nil {
		if errors.Is(err, ErrInactive) {
			err = errors.Join(ErrInactiveEffect, err)
		}
		refuse(err)
	}
	return &Fiber{ref: c.root.core.fiber(id), cell: cell}
}

// compile turns declarations into the spec the core mounts. A refused declaration
// gives a spec that fails its fiber with the refusal and has no Needs or Provides.
func (c *Context) compile(id string, entry entryID, inject []Dep, provides []AnyKey, apply func(*Context) error, hooks ...HookUse) pluginSpec {
	fr := &frame{id: id, inject: append([]Dep(nil), inject...), at: c, hooks: slices.Clone(hooks)}
	probe := &Context{parent: c, root: c.root, entry: entry, frame: fr}
	var needs, required []svcKey
	var icpts []intercept
	var err error
	seen := make(map[*keyData]bool, len(inject))
	for _, d := range inject {
		if d.key == nil {
			err = errors.Join(err, injectErr(nil, ErrKey))
			continue
		}
		if d.hasCfg && checkConfig(d.key, d.config) != nil {
			err = errors.Join(err, injectErr(d.key, ErrConfig))
			continue
		}
		switch {
		case !probe.consumerAllowed(d.key):
			err = errors.Join(err, injectErr(d.key, ErrBinding))
		case seen[d.key]:
			err = errors.Join(err, injectErr(d.key, ErrDuplicate))
		case d.optional && d.key.provider == classBound:
			err = errors.Join(err, injectErr(d.key, ErrWant))
		}
		seen[d.key] = true
		if d.hasCfg {
			icpts = append(icpts, intercept{key: d.key, value: d.config})
		}
		if !d.optional {
			a := d.key.addr(c.realmOf(d.key))
			needs, required = append(needs, a), append(required, a)
		}
	}
	clear(seen)
	for _, k := range provides {
		d, kerr := dataOf(k)
		switch {
		case kerr != nil:
			err = errors.Join(err, injectErr(nil, kerr))
			continue
		case !probe.providerAllowed(d):
			err = errors.Join(err, injectErr(d, ErrBinding))
		case seen[d]:
			err = errors.Join(err, injectErr(d, ErrDuplicate))
		}
		seen[d] = true
		fr.provides = append(fr.provides, d.addr(c.realmOf(d)))
	}
	err = errors.Join(err, checkUses(hooks))
	if err != nil {
		return pluginSpec{ID: id, Apply: func(*handle) error { return err }}
	}
	act := func(h *handle) *Context {
		return &Context{parent: c, root: c.root, h: h, entry: entry, frame: fr, icpts: icpts}
	}
	spec := pluginSpec{ID: id, Needs: needs, Provides: keysOfAddr(fr.provides)}
	if len(required) != 0 {
		spec.Check = func(h *handle) bool {
			caller := act(h)
			for _, a := range required {
				v, rerr := h.read(a)
				p, ok := v.(*provided)
				if rerr != nil || !ok || (p.available != nil && !p.available(caller)) {
					return false
				}
			}
			return true
		}
	}
	if apply != nil {
		spec.Apply = func(h *handle) error { return apply(act(h)) }
	}
	return spec
}

func keysOfAddr(as []*address) []svcKey {
	out := make([]svcKey, len(as))
	for i, a := range as {
		out[i] = a
	}
	return out
}
