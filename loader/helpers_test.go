// SPDX-License-Identifier: Apache-2.0

package loader

import (
	"context"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	launchfake "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0/fake"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// probe records what the row plugins of a test do.
type probe struct {
	mu      sync.Mutex
	applies map[string]int
	cfgs    map[string][]any
	live    map[string]int
	maxLive map[string]int
	log     []string // plugin ids, in the order their disposers ran
}

func newProbe() *probe {
	return &probe{applies: map[string]int{}, cfgs: map[string][]any{}, live: map[string]int{}, maxLive: map[string]int{}}
}

// plugin is a defined plugin that counts its applies, its configs and the fibers
// of it that are alive at once, and runs extra.
func (p *probe) plugin(id string, extra func(c *kernel.Context, cfg any) error) kernel.Plugin {
	return kernel.Define(kernel.Plugin{ID: id, Apply: func(c *kernel.Context, cfg any) error {
		p.mu.Lock()
		p.applies[id]++
		p.cfgs[id] = append(p.cfgs[id], cfg)
		p.live[id]++
		p.maxLive[id] = max(p.maxLive[id], p.live[id])
		p.mu.Unlock()
		c.Defer("probe", func(context.Context) error {
			p.mu.Lock()
			defer p.mu.Unlock()
			p.live[id]--
			p.log = append(p.log, id)
			return nil
		})
		if extra != nil {
			return extra(c, cfg)
		}
		return nil
	}})
}

func (p *probe) count(id string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.applies[id]
}

func (p *probe) lastConfig(id string) any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cfgs[id][len(p.cfgs[id])-1]
}

func (p *probe) order() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.log)
}

// fx is a kernel with the launch fake, a loader and a consumer of its keys.
type fx struct {
	t     *testing.T
	root  *kernel.Root
	cat   *launchfake.Launcher
	tree  *tree
	ld    def.Loader
	rd    composition.Reader
	fiber *kernel.Fiber
}

type fxOpt struct {
	role    string
	rows    []def.EntrySpec
	plugins map[string]kernel.Plugin
	keys    map[string]kernel.AnyKey
	modules []launch.CatalogEntry
	pre     []kernel.Plugin // mounted before the loader
	kopts   []kerneltest.Option
	batch   func(*kernel.Root) BatchFunc
	cfg     any // replaces the loader's config
	// bound builds the kernel with kernel.New and the binding maps of a
	// distribution (kerneltest never builds one, 07-c4-plugin-sdk.md:564): the
	// launcher provides the launch keys, listeners are the entries admitted to the
	// three sealed hooks and consumers those admitted to the loader key.
	bound     bool
	listeners []kernel.Plugin
	consumers []kernel.Plugin
	bindings  kernel.Bindings // more consumer bindings, by key
}

// newFx mounts the launch fake, the plugins of o.pre and the loader with o.rows,
// then waits for the kernel to settle.
func newFx(t *testing.T, o fxOpt) *fx {
	t.Helper()
	l, lp := launchfake.New()
	loaderPlugin := New()
	x := &fx{t: t}
	consumer := kernel.Define(kernel.Plugin{ID: "t/consumer", Inject: []kernel.Dep{kernel.Need(def.Key), kernel.Need(composition.Key)},
		Apply: func(c *kernel.Context, _ any) error {
			x.ld, x.rd = def.Key.Of(c), composition.Key.Of(c)
			return nil
		}})
	var root *kernel.Root
	mount := func(ps ...kernel.Plugin) {
		for _, p := range ps {
			f := root.Plugin(p, nil)
			if err := f.Await(t.Context()); err != nil {
				t.Fatalf("%s: %v", p.ID, err)
			}
		}
	}
	if o.bound {
		cfg := kernel.Config{LoadDeadline: 10 * time.Second, DrainDeadline: 10 * time.Second, DisposeDeadline: 10 * time.Second,
			ArchiveLimit: 256, EventWindow: 256, ObserverQueue: 256, OwnerInbox: 256, TerminalLimit: 256,
			Providers: kernel.Bindings{}, Consumers: kernel.Bindings{def.Key: append([]kernel.Plugin{consumer}, o.consumers...)},
			Listeners: map[kernel.AnyHook][]kernel.Plugin{def.PatchContext: o.listeners, def.EntryInit: o.listeners, def.VolatileUpdate: o.listeners}}
		for _, k := range []kernel.AnyKey{launch.ProfileContextKey, launch.CatalogKey, launch.CmdlineKey, launch.ReadyKey, launch.KillSwitchesKey, launch.AppExitKey} {
			cfg.Providers[k] = []kernel.Plugin{lp}
		}
		cfg.Consumers[launch.AppExitKey] = []kernel.Plugin{lp}
		maps.Copy(cfg.Consumers, o.bindings)
		r, err := kernel.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = r.Close(context.Background()) })
		root = r
		mount(lp)
	} else {
		root = kerneltest.New(t, o.kopts...)
		mount = func(ps ...kernel.Plugin) { kerneltest.Mount(root, ps...) }
		mount(lp)
	}
	l.RoleName, l.Plugins, l.Keys, l.Modules = cmp(o.role, "api"), o.plugins, o.keys, o.modules
	mount(o.pre...)
	cfg := o.cfg
	if cfg == nil {
		b := root.Batch
		if o.batch != nil {
			b = o.batch(root)
		}
		cfg = Config{Rows: o.rows, Batch: b}
	}
	x.root, x.cat = root, l
	x.fiber = root.Plugin(loaderPlugin, cfg)
	if err := x.fiber.Await(t.Context()); err != nil {
		t.Fatalf("loader: %v", err)
	}
	mount(consumer)
	x.tree = x.ld.(*tree)
	x.settle()
	return x
}

// bareKernel is a kernel with the launch fake mounted.
func bareKernel(t *testing.T) (*kernel.Root, *launchfake.Launcher) {
	t.Helper()
	root := kerneltest.New(t)
	l, lp := launchfake.New()
	l.RoleName = "api"
	kerneltest.Mount(root, lp)
	return root, l
}

func cmp(a, b string) string {
	if a == "" {
		return b
	}
	return a
}

func (x *fx) settle() {
	x.t.Helper()
	// Settle also reports each FAILED fiber; the tests assert states instead.
	_ = x.root.Settle(x.t.Context())
	if err := x.t.Context().Err(); err != nil {
		x.t.Fatalf("settle: %v", err)
	}
}

// state awaits the entry and returns its state.
func (x *fx) state(id string) kernel.State {
	x.t.Helper()
	st, err := x.ld.Await(x.t.Context(), id)
	if err != nil {
		x.t.Fatalf("Await(%s): %v", id, err)
	}
	return st
}

func (x *fx) want(id string, st kernel.State) {
	x.t.Helper()
	if got := x.state(id); got != st {
		x.t.Fatalf("%s is %s, want %s", id, got, st)
	}
}

func (x *fx) apply(ops ...def.Op) []def.Result {
	x.t.Helper()
	res, err := x.ld.Apply(x.t.Context(), ops)
	if err != nil {
		x.t.Fatalf("Apply: %v", err)
	}
	x.settle()
	return res
}

// failure is the diagnostic of the fiber that the entry's row mounted as a stub:
// the snapshot error of the fiber named for the row.
func (x *fx) failure(plugin string) string {
	x.t.Helper()
	in, err := x.root.Inspect(x.t.Context())
	if err != nil {
		x.t.Fatal(err)
	}
	for _, f := range in.Fibers {
		if f.Plugin == plugin && f.State == kernel.Failed {
			return f.Error
		}
	}
	x.t.Fatalf("no FAILED fiber of %s", plugin)
	return ""
}

// consistent fails the test unless each mounted row of the composition names the
// one fiber of its plugin that is not DISPOSED, or none while it has no fiber: the
// tree and the kernel agree. Each row of a test that calls it has its own plugin.
func (x *fx) consistent() {
	x.t.Helper()
	g, _ := x.rd.Committed(composition.ScopeProcess)
	in, err := x.root.Inspect(x.t.Context())
	if err != nil {
		x.t.Fatal(err)
	}
	live := map[string][]uint64{}
	for _, f := range in.Fibers {
		if f.State != kernel.Disposed {
			live[f.Plugin] = append(live[f.Plugin], f.ID)
		}
	}
	for _, r := range g.Rows {
		var want []uint64
		if r.Fiber != 0 {
			want = []uint64{r.Fiber}
		}
		if !slices.Equal(live[r.Plugin], want) {
			x.t.Errorf("row %s (%s) names fiber %d, the kernel has %v of %s", r.ID, r.State, r.Fiber, live[r.Plugin], r.Plugin)
		}
	}
}

// ledger records the ledger events of a kernel from the moment it is made.
type ledger struct {
	mu  sync.Mutex
	evs []kernel.LedgerEvent
}

func record(t *testing.T, root *kernel.Root) *ledger {
	t.Helper()
	l := &ledger{}
	if _, err := root.Observe("t/ledger", func(_ context.Context, e kernel.LedgerEvent) error {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.evs = append(l.evs, e)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return l
}

// of returns the plugin ids of the events of kind, in order.
func (l *ledger) of(t *testing.T, root *kernel.Root, kind string) (plugins []string) {
	t.Helper()
	if err := root.AwaitObservers(t.Context()); err != nil {
		t.Fatal(err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.evs {
		if e.Kind == kind {
			plugins = append(plugins, e.Plugin)
		}
	}
	return plugins
}

func (x *fx) fibers(plugin string) (n int) {
	in, _ := x.root.Inspect(x.t.Context())
	for _, f := range in.Fibers {
		if f.Plugin == plugin && f.State != kernel.Disposed {
			n++
		}
	}
	return n
}

func row(id, plugin string) def.EntrySpec {
	return def.EntrySpec{ID: id, Plugin: plugin, Enabled: true, Roles: []string{"api"}}
}

func child(parent, id, plugin string) def.EntrySpec {
	r := row(parent+":"+id, plugin)
	r.Parent = parent
	return r
}

func group(id string) def.EntrySpec { return row(id, def.GroupPlugin) }

func create(e def.EntrySpec) def.Op { return def.Op{Kind: def.OpCreate, Entry: e} }
func update(e def.EntrySpec) def.Op { return def.Op{Kind: def.OpUpdate, Entry: e} }
func remove(id string) def.Op {
	return def.Op{Kind: def.OpRemove, Entry: def.EntrySpec{ID: id}}
}

func plugins(ps ...kernel.Plugin) map[string]kernel.Plugin {
	m := map[string]kernel.Plugin{}
	for _, p := range ps {
		m[p.ID] = p
	}
	return m
}

// listener is a plugin that listens on the three hooks of the loader and records
// what it hears.
type listener struct {
	mu       sync.Mutex
	inits    []def.EntryInitArgs
	volatile []def.VolatileUpdateArgs
	patches  []string
	patch    func(a def.PatchContextArgs, next kernel.Next[*kernel.Context]) (*kernel.Context, error)
	plugin   kernel.Plugin
}

func newListener(id string) *listener {
	l := &listener{}
	l.plugin = kernel.Define(kernel.Plugin{ID: id,
		Hooks: []kernel.HookUse{def.EntryInit.Use(kernel.DefaultStage), def.VolatileUpdate.Use(kernel.DefaultStage), def.PatchContext.Use(kernel.DefaultStage)},
		Apply: func(c *kernel.Context, _ any) error {
			def.EntryInit.On(c, kernel.DefaultStage, func(_ context.Context, a def.EntryInitArgs) (any, error) {
				l.mu.Lock()
				defer l.mu.Unlock()
				l.inits = append(l.inits, a)
				return nil, nil
			})
			def.VolatileUpdate.On(c, kernel.DefaultStage, func(_ context.Context, a def.VolatileUpdateArgs) (any, error) {
				l.mu.Lock()
				defer l.mu.Unlock()
				l.volatile = append(l.volatile, a)
				return nil, nil
			})
			def.PatchContext.On(c, kernel.DefaultStage, func(_ context.Context, a def.PatchContextArgs, next kernel.Next[*kernel.Context]) (*kernel.Context, error) {
				l.mu.Lock()
				l.patches = append(l.patches, a.Entry.ID)
				fn := l.patch
				l.mu.Unlock()
				if fn == nil {
					return next()
				}
				return fn(a, next)
			})
			return nil
		}})
	return l
}

func (l *listener) initIDs() (ids []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, a := range l.inits {
		ids = append(ids, a.Entry.ID)
	}
	return ids
}

// stateEvents counts the state events of the fibers of plugin id.
type stateEvents struct {
	mu sync.Mutex
	n  map[string]int
}

func watchStates(t *testing.T, root *kernel.Root) *stateEvents {
	t.Helper()
	s := &stateEvents{n: map[string]int{}}
	if _, err := root.Observe("t/states", func(_ context.Context, e kernel.LedgerEvent) error {
		if e.Kind == "state" {
			s.mu.Lock()
			s.n[e.Plugin]++
			s.mu.Unlock()
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s *stateEvents) of(t *testing.T, root *kernel.Root, plugin string) int {
	t.Helper()
	if err := root.AwaitObservers(t.Context()); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n[plugin]
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func deepEqual(a, b any) bool { return reflect.DeepEqual(a, b) }
