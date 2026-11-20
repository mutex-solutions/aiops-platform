// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
)

// The reasons a hook operation is refused. A refused On fails the fiber with an
// error that matches one of them (07-c4-plugin-sdk.md:136, :439).
var (
	// ErrHook reports a zero HookUse, a nil listener or a nil context.
	ErrHook = errors.New("invalid hook use")
	// ErrHookUndeclared reports an On on a hook, or a stage of it, that the fiber
	// did not declare in Plugin.Hooks (07-c4-plugin-sdk.md:136, :219, :439).
	ErrHookUndeclared = errors.New("hook is not declared in Plugin.Hooks")
	// ErrStage reports a stage that the hook's definition does not declare
	// (07-c4-plugin-sdk.md:438).
	ErrStage = errors.New("unknown hook stage")
	// ErrSealed reports an On on a sealed hook from a fiber that neither is the
	// launcher's own context nor is listed for the hook in Config.Listeners
	// (07-c4-plugin-sdk.md:439, :463; ADR-034 owner decision 11).
	ErrSealed = errors.New("hook is sealed to the launcher")
	// ErrNotOwner reports a Bind by a fiber that does not provide the hook's owning
	// key, and a Bind on a hook with no owner that a plugin may bind
	// (07-c4-plugin-sdk.md:439; ADR-034 owner decision 10).
	ErrNotOwner = errors.New("fiber does not provide the hook's owning key")
	// ErrUnscoped reports a dispatch with Unscoped on a hook that its definition
	// marks as carrying scoped data (07-c4-plugin-sdk.md:440).
	ErrUnscoped = errors.New("hook carries scoped data and refuses an Unscoped dispatch")
	// ErrNext reports a second call of next in one listener
	// (07-c4-plugin-sdk.md:209, :386).
	ErrNext = errors.New("next called more than once")
	// ErrNextExpired reports a call of next after its listener returned. The
	// contract names no such case; the prototype refused it.
	ErrNextExpired = errors.New("next called after its listener returned")
)

// DefaultStage is the one stage of a hook that declares none. The contract
// declares no stage option for kernel hooks (07-c4-plugin-sdk.md:627-628).
const DefaultStage = "default"

// ListenerError attributes a listener's failure to its fiber and keeps the cause.
type ListenerError struct {
	PluginID string
	Cause    error
}

func (e *ListenerError) Error() string { return fmt.Sprintf("listener %s: %v", e.PluginID, e.Cause) }
func (e *ListenerError) Unwrap() error { return e.Cause }

// PanicError is a listener panic, or a panicking audience predicate, contained at
// the dispatch boundary. It matches errors.Is for a panic value that is an error.
type PanicError struct{ Value any }

func (e *PanicError) Error() string { return fmt.Sprintf("hook listener panicked: %v", e.Value) }
func (e *PanicError) Unwrap() error { err, _ := e.Value.(error); return err }

// HookOption is a declaration option of NewEvent and NewWaterfall. The contract
// shows only OwnedBy; the others are the kernel's own and are open points.
type HookOption func(*hookDef)

// OwnedBy records that only the fiber that provides k may dispatch the hook
// (07-c4-plugin-sdk.md:393, :439, :627; ADR-034 owner decision 10). The kernel
// enforces it in Bind: only a fiber for which Bind's rule holds gets a dispatcher.
// A hook declared with NewEvent or NewWaterfall must have an owner: the contract
// shows every hook with OwnedBy and is silent on a hook without one, so the
// kernel fails closed and refuses the definition (an open point).
func OwnedBy(k AnyKey) HookOption {
	return func(d *hookDef) { d.owner, _ = dataOf(k) }
}

// Stages declares the hook's stages in chain order: a listener of an earlier stage
// is outer to one of a later stage (07-c4-plugin-sdk.md:396, :438). Without it the
// hook has the one stage DefaultStage. A name that is empty or repeated panics.
func Stages(names ...string) HookOption {
	return func(d *hookDef) { d.stages = slices.Clone(names) }
}

// Scoped marks the hook as carrying scoped data: a dispatch with Unscoped is
// refused with ErrUnscoped (07-c4-plugin-sdk.md:440, "a kernel check").
func Scoped() HookOption { return func(d *hookDef) { d.scoped = true } }

// Sealed marks the hook sealed to the distribution: only the launcher's own
// context and the entries that Config.Listeners lists for the hook may listen on
// it (07-c4-plugin-sdk.md:439, :463; ADR-034 owner decision 11).
func Sealed() HookOption { return func(d *hookDef) { d.sealed = true } }

// hookDef is the definition shared by Event and Waterfall.
type hookDef struct {
	name   string
	owner  *keyData
	stages []string
	scoped bool
	sealed bool
	kernel bool // A hook of the kernel (internal/...): its first listener starts the relay.
}

const kernelPrefix = "internal/"

// newHookDef checks and builds a definition. A bad definition is a programming
// error and panics, as NewKey does. The prefix internal/ belongs to the kernel,
// and only a kernel hook may have no owner: the kernel root dispatches it.
func newHookDef(name string, kernel bool, opts []HookOption) *hookDef {
	d := &hookDef{name: name, kernel: kernel}
	for _, o := range opts {
		o(d)
	}
	if kernel {
		d.sealed = true
	} else if strings.HasPrefix(name, kernelPrefix) {
		panic(fmt.Sprintf("kernel: hook %q: the prefix %q is reserved to the kernel", name, kernelPrefix))
	} else if d.owner == nil {
		panic(fmt.Sprintf("kernel: hook %q: no owner, declare it with OwnedBy", name))
	}
	if name == "" {
		panic("kernel: hook with no name")
	}
	if len(d.stages) == 0 {
		d.stages = []string{DefaultStage}
	}
	for i, s := range d.stages {
		if s == "" || slices.Contains(d.stages[:i], s) {
			panic(fmt.Sprintf("kernel: hook %q: stage %q is empty or repeated", name, s))
		}
	}
	return d
}

// HookUse is one declared use of a hook: the hook and one of its stages, as listed
// in Plugin.Hooks (07-c4-plugin-sdk.md:219, :411). Its zero value is invalid.
type HookUse struct {
	def   *hookDef
	stage int
}

// Hook returns the hook's name, empty for a zero HookUse.
func (u HookUse) Hook() string {
	if u.def == nil {
		return ""
	}
	return u.def.name
}

// Stage returns the stage's name, empty for a zero or invalid HookUse.
func (u HookUse) Stage() string {
	if u.def == nil || u.stage < 0 {
		return ""
	}
	return u.def.stages[u.stage]
}

// use returns the HookUse of stage; an unknown stage gives one that a mount
// refuses with ErrStage, so that a definition package never panics at init.
func (d *hookDef) use(stage string) HookUse {
	return HookUse{def: d, stage: slices.Index(d.stages, stage)}
}

// checkUses reports every refused entry of a Plugin.Hooks list.
func checkUses(uses []HookUse) error {
	var err error
	for i, u := range uses {
		switch {
		case u.def == nil:
			err = errors.Join(err, fmt.Errorf("hook use %d: %w", i, ErrHook))
		case u.stage < 0:
			err = errors.Join(err, fmt.Errorf("hook %s: %w", u.def.name, ErrStage))
		case slices.Contains(uses[:i], u):
			err = errors.Join(err, fmt.Errorf("hook %s/%s: %w", u.def.name, u.Stage(), ErrDuplicate))
		}
	}
	return err
}

func (d *hookDef) errf(err error) error { return fmt.Errorf("hook %s: %w", d.name, err) }

// admit is the checks of an On: context, stage, seal, declaration. A sealed hook
// admits the launcher's own context and the entries of Config.Listeners; the test
// constructor admits any entry (ADR-034 owner decision 11). A refusal fails the
// fiber like the other no-error registrations (effect.go refuse).
func (d *hookDef) admit(c *Context, stage string) (idx int) {
	if c == nil {
		refuse(d.errf(ErrHook))
	}
	if idx = slices.Index(d.stages, stage); idx < 0 {
		refuse(d.errf(ErrStage))
	}
	if !c.frame.host {
		if d.sealed && !c.root.unbound && !c.root.listeners.allows(d, c.entry) {
			refuse(d.errf(ErrSealed))
		}
		if !slices.Contains(c.frame.hooks, HookUse{def: d, stage: idx}) {
			refuse(d.errf(ErrHookUndeclared))
		}
	}
	return idx
}

// bind is the check of a Bind (ADR-034 owner decision 10). The fiber of c must
// provide the owning key: c's entry is admitted for it by the provider binding
// map when it is provider-bound, and the fiber declared it in Provides in c's
// realm. It is the predicate Key.Provide applies (Context.mayProvide), so the two
// cannot disagree. The activation must be live. A refusal fails the fiber.
func (d *hookDef) bind(c *Context) {
	if c == nil {
		refuse(d.errf(ErrHook))
	}
	leave, ok := c.h.enter()
	if !ok {
		refuse(d.errf(inactive()))
	}
	defer leave()
	if d.owner == nil || !c.mayProvide(d.owner) {
		refuse(d.errf(ErrNotOwner))
	}
}

// enter takes the lease of the dispatcher's activation for the length of one
// dispatch: a dispatcher dies with the activation that bound it (ADR-034 owner
// decision 10). The error matches ErrInactive for a zero dispatcher and once
// that fiber is quiescing, disposed or restarted.
func (d *hookDef) enter(c *Context) (leave func(), err error) {
	if c != nil {
		if leave, ok := c.h.enter(); ok {
			return leave, nil
		}
	}
	return nil, d.errf(ErrInactive)
}

// AnyHook is a hook of any payload type: the key of Config.Listeners, as AnyKey is
// the key of a binding map (ADR-034 owner decision 11). Every *Event and
// *Waterfall is an AnyHook.
type AnyHook interface {
	Name() string
	anyHook() *hookDef
}

// dispatchable is the checks every dispatch makes before it reads the chain.
func (d *hookDef) dispatchable(ctx context.Context, aud Audience) error {
	if d.scoped && aud.match == nil {
		return d.errf(ErrUnscoped)
	}
	return ctx.Err()
}

// listener is one listener: its fiber's context, its place in the chain and
// the function.
type listener[F any] struct {
	id     string // The fiber's id, the second ordering key.
	stage  int    // Index in the hook's declared stages, the first ordering key.
	ctx    *Context
	scoped bool // The listener's context carries a scope value.
	fn     F
}

func hasScope(c *Context) bool {
	for n := c; n != nil; n = n.parent {
		if n.scope != nil {
			return true
		}
	}
	return false
}

// admits reports whether the audience reaches the listener.
func (r *listener[F]) admits(a Audience) (ok bool, err error) {
	if a.match == nil {
		return !r.scoped, nil
	}
	defer func() {
		if p := recover(); p != nil {
			ok, err = false, &PanicError{Value: p}
		}
	}()
	return a.match(r.ctx), nil
}

// lease takes the in-flight lease of the listener's fiber; ok is false once it
// is quiescing, and the listener is then not called (07-c4-plugin-sdk.md:203, :373).
func (r *listener[F]) lease() (leave func(), ok bool) { return r.ctx.h.enter() }

// fail contains a listener failure of an event: it is skipped and logged
// (07-c4-plugin-sdk.md:385).
func (r *listener[F]) fail(err error) {
	r.ctx.root.logHook(&ListenerError{PluginID: r.id, Cause: err})
}

// table is a hook's listener chain. It is copy-on-write: a dispatch reads the
// published slice with no lock and never mutates it, and only registration and
// removal take mu. No listener runs while mu is held (docs/design/plugin-system-2026-11.md G3).
type table[F any] struct {
	mu   sync.Mutex
	list atomic.Pointer[[]*listener[F]]
}

func (t *table[F]) load() []*listener[F] {
	if p := t.list.Load(); p != nil {
		return *p
	}
	return nil
}

// add registers fn as an effect of c's fiber: the listener leaves the chain when
// the fiber's effects are reverted (07-c4-plugin-sdk.md:369).
// The lease is taken first, so that an ended activation is refused as inactive
// before any other refusal, and it keeps the fiber's effects from being reverted
// before the listener is published: the unload waits for open leases
// (07-c4-plugin-sdk.md:373). nilFn reports a nil fn, which F cannot compare.
func (t *table[F]) add(d *hookDef, c *Context, stage string, fn F, nilFn bool) {
	if c != nil {
		leave, ok := c.h.enter()
		if !ok {
			refuse(d.errf(inactive()))
		}
		defer leave()
	}
	if nilFn {
		refuse(d.errf(ErrHook))
	}
	idx := d.admit(c, stage)
	r := &listener[F]{id: c.frame.id, stage: idx, ctx: c, scoped: hasScope(c), fn: fn}
	c.h.deferEffect("hook "+d.name+"/"+stage, func(context.Context) error { t.remove(r); return nil })
	t.mu.Lock()
	defer t.mu.Unlock()
	old := t.load()
	i := slices.IndexFunc(old, func(o *listener[F]) bool {
		return o.stage > r.stage || (o.stage == r.stage && o.id > r.id)
	})
	if i < 0 {
		i = len(old)
	}
	next := make([]*listener[F], 0, len(old)+1)
	next = append(append(append(next, old[:i]...), r), old[i:]...)
	t.list.Store(&next)
}

func (t *table[F]) remove(r *listener[F]) {
	t.mu.Lock()
	defer t.mu.Unlock()
	old := t.load()
	if i := slices.Index(old, r); i >= 0 {
		next := make([]*listener[F], 0, len(old)-1)
		next = append(append(next, old[:i]...), old[i+1:]...)
		t.list.Store(&next)
	}
}

// pick returns the listeners the audience reaches, in chain order. It decides
// membership for the whole dispatch before any listener runs. When every
// listener is reached it returns the published slice itself; otherwise it fills
// buf, which the caller may keep on its stack.
func (t *table[F]) pick(aud Audience, buf []*listener[F]) ([]*listener[F], error) {
	all := t.load()
	sel, filtered := all, false
	for i, r := range all {
		ok, err := r.admits(aud)
		if err != nil {
			return nil, fmt.Errorf("listener %s audience: %w", r.id, err)
		}
		if !ok && !filtered {
			sel, filtered = append(buf, all[:i]...), true
		} else if ok && filtered {
			sel = append(sel, r)
		}
	}
	return sel, nil
}

// listenerSet is the checked form of Config.Listeners.
type listenerSet map[*hookDef]map[entryID]struct{}

// compileListeners checks l: it reports a nil hook, a hook that is not sealed, an
// entry with no identity and an entry listed twice for one hook.
func compileListeners(l map[AnyHook][]Plugin) (listenerSet, error) {
	set := make(listenerSet, len(l))
	var errs []error
	for h, entries := range l {
		var d *hookDef
		if h != nil {
			d = h.anyHook()
		}
		if d == nil {
			errs = append(errs, fmt.Errorf("listener binding: %w", ErrHook))
			continue
		}
		if !d.sealed {
			errs = append(errs, fmt.Errorf("listener binding for hook %s that is not sealed: %w", d.name, ErrSealed))
			continue
		}
		ids := make(map[entryID]struct{}, len(entries))
		for _, p := range entries {
			_, dup := ids[p.entry]
			switch {
			case p.entry == nil:
				errs = append(errs, fmt.Errorf("listener binding for %s lists plugin %q that was never defined: %w", d.name, p.ID, ErrBinding))
			case dup:
				errs = append(errs, fmt.Errorf("listener binding for %s lists plugin %q twice: %w", d.name, p.ID, ErrDuplicate))
			}
			ids[p.entry] = struct{}{}
		}
		set[d] = ids
	}
	return set, errors.Join(errs...)
}

func (s listenerSet) allows(d *hookDef, e entryID) bool {
	_, ok := s[d][e]
	return e != nil && ok
}

// hookLogMu serialises the log sink: dispatches run on any goroutine and the sink
// is documented as called one at a time.
var hookLogMu sync.Mutex

// logHook hands a contained listener failure to Config.ObserverLog of the
// listener's kernel. A panic of the sink is contained.
func (r *Root) logHook(err error) {
	log := r.core.cfg.observerLog
	if log == nil {
		return
	}
	hookLogMu.Lock()
	defer hookLogMu.Unlock()
	defer func() { _ = recover() }()
	log(err)
}

// The kernel's own hooks, all sealed (07-c4-plugin-sdk.md:463; plugin record
// section 8: three events, then a waterfall, a waterfall and an event). The
// contract gives no payload type, so the payload is any. They are exported so
// that Config.Listeners can name them from outside the package (ADR-034 owner
// decision 11); they stay sealed, have no owner, and no plugin can Bind one: the
// kernel root alone dispatches them. Treat them as constants: never assign to one.
//
// InternalConfig, InternalUpdate and InternalCommit are dispatched by the config
// pipeline (config.go). InternalCommit is a waterfall of bool: true means the chain
// reached the kernel's own step, so a listener that returns without calling next
// is a veto, and one that fails aborts the update (ADR-034 owner decision 15).
//
// InternalPlugin, InternalStatus and InternalService are dispatched by the kernel
// with the ledger event as payload (ADR-059 decision 2; 07-c4-plugin-sdk.md:208,
// :385, :463; docs/design/plugin-system-2026-11.md:54). Each LedgerEvent reaches
// the hooks below by its Kind; any other Kind reaches none, and the one event that
// maps to two hooks reaches InternalStatus first.
//
//	ledger Kind              hook             meaning
//	"mount"                  InternalPlugin   a fiber was created
//	"state"                  InternalStatus   the fiber changed state (From to State)
//	"state", State DISPOSED  InternalPlugin   the fiber was removed
//	"provide"                InternalService  a provision appeared (Key, UID)
//	"service"                InternalService  a provision changed availability (State is its provider's)
//	"remove"                 InternalService  a provision ended
//	"overflow"               all three        events were lost, see below
//
// The listeners run in the one order of Sequence, one event at a time, on the
// goroutine of the observer dispatcher and never on the owner. A slow, failing or
// panicking listener is skipped and logged to Config.ObserverLog and cannot block
// or fail the kernel. A listener sees the events accepted after the first listener
// of any of the three hooks was registered. The stream is the one of Root.Observe
// and bounded by Config.ObserverQueue: when a listener stalls the dispatcher while
// that many events wait, the kernel's subscription is detached by the overflow rule,
// the events of that time are lost for every listener of the three hooks, and none
// of them is removed. When the stalled listener returns the kernel subscribes
// again. Just before the first event that subscription delivers it dispatches to
// all three hooks one LedgerEvent of Kind "overflow", whose Sequence is the one
// just before that event: an event up to it that a listener did not get will not
// come. A listener resyncs on it by calling Root.Inspect and ignoring every later
// event whose Sequence is not above Inspection.Sequence. With no listener the
// kernel subscribes to nothing and the owner does no work for these hooks.
var (
	InternalPlugin  = newEvent[LedgerEvent](kernelPrefix+"plugin", true)
	InternalStatus  = newEvent[LedgerEvent](kernelPrefix+"status", true)
	InternalService = newEvent[LedgerEvent](kernelPrefix+"service", true)
	InternalConfig  = newWaterfall[any, any](kernelPrefix+"config", true)
	InternalUpdate  = newWaterfall[any, any](kernelPrefix+"update", true)
	InternalCommit  = newWaterfall[any, bool](kernelPrefix+"commit", true)
)
