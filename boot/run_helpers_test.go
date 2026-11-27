package boot

import (
	"bytes"
	"context"
	"os"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	loaderdef "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The tests of the run path use a toy loader, so that boot's tests do not import the
// loader package; the real loader runs in test/dist and test/loader-smoke.

var (
	storeKey  = kernel.NewKey[string]("store", 0)
	cacheKey  = kernel.NewKey[string]("cache", 0)
	policyKey = kernel.NewKey[string]("policy", 0, kernel.ProviderBound())
)

type toyLoaderCfg struct {
	rows    []loaderdef.EntrySpec
	batch   BatchFunc
	plugins map[string]kernel.Plugin
}

// toyLoader mounts the enabled rows whose roles name the process role, as children,
// and serves the composition key. A group row is not supported.
func toyLoader() kernel.Plugin {
	return kernel.Define(kernel.Plugin{
		ID:       LoaderID,
		Inject:   []kernel.Dep{kernel.Need(launch.ProfileContextKey), kernel.Need(launch.CatalogKey)},
		Provides: []kernel.AnyKey{composition.Key},
		Decode: func(raw any) (any, error) {
			cfg, ok := raw.(toyLoaderCfg)
			if !ok {
				return nil, os.ErrInvalid
			}
			return cfg, nil
		},
		Apply: func(c *kernel.Context, raw any) error {
			cfg := raw.(toyLoaderCfg)
			cat, role := launch.CatalogKey.Of(c), launch.ProfileContextKey.Of(c).Role()
			var mu sync.Mutex
			var rows []composition.Row
			fibers := map[string]*kernel.Fiber{}
			for _, s := range cfg.rows {
				if !s.Enabled || role != launch.RoleAll && !contains(s.Roles, role) {
					continue
				}
				p := cfg.plugins[s.Plugin]
				for _, n := range s.Inject {
					k, _ := cat.Key(n)
					p.Inject = append(p.Inject[:len(p.Inject):len(p.Inject)], kernel.NeedKey(k))
				}
				fibers[s.ID] = c.Plugin(p, s.Config)
				rows = append(rows, composition.Row{ID: s.ID, Plugin: s.Plugin})
			}
			return composition.Key.Provide(c, toyReader{&mu, rows, fibers})
		},
	})
}

func contains(s []string, x string) bool {
	for _, y := range s {
		if y == x {
			return true
		}
	}
	return false
}

type toyReader struct {
	mu     *sync.Mutex
	rows   []composition.Row
	fibers map[string]*kernel.Fiber
}

func (r toyReader) Committed(s composition.Scope) (composition.Generation, bool) {
	g := composition.Generation{Scope: s}
	for _, row := range r.rows {
		row.State, _ = r.fibers[row.ID].State(context.Background())
		g.Rows = append(g.Rows, row)
	}
	return g, s == composition.ScopeProcess
}

func (r toyReader) Declaring(composition.Scope, string) []composition.Row { return nil }

// fiberOf is a toy plugin: a provider of a key, a consumer of keys, or both, whose
// behaviour is set by the test.
type toySpec struct {
	id       string
	provides []kernel.AnyKey
	needs    []kernel.AnyKey
	wants    []kernel.AnyKey
	apply    func(c *kernel.Context, cfg any) error
}

func toy(s toySpec) ModuleEntry {
	p := kernel.Define(kernel.Plugin{ID: s.id, Provides: s.provides, Apply: func(c *kernel.Context, cfg any) error {
		for _, k := range s.provides {
			if k == storeKey {
				if err := storeKey.Provide(c, s.id); err != nil {
					return err
				}
			}
			if k == cacheKey {
				if err := cacheKey.Provide(c, s.id); err != nil {
					return err
				}
			}
			if k == policyKey {
				if err := policyKey.Provide(c, s.id); err != nil {
					return err
				}
			}
		}
		if s.apply != nil {
			return s.apply(c, cfg)
		}
		return nil
	}})
	for _, k := range s.needs {
		p.Inject = append(p.Inject, kernel.NeedKey(k))
	}
	for _, k := range s.wants {
		p.Inject = append(p.Inject, kernel.Want(k.(kernel.Key[string])))
	}
	e := ModuleEntry{ID: s.id, Module: "example.test/" + s.id, Version: "v0.1.0", Plugin: p}
	for _, k := range s.provides {
		e.Provides = append(e.Provides, k.Name())
	}
	for _, k := range s.needs {
		e.Needs = append(e.Needs, k.Name())
	}
	for _, k := range s.wants {
		e.Wants = append(e.Wants, k.Name())
	}
	return e
}

// fixture is a registry, a distribution and the files of one run.
type fixture struct {
	reg    Registry
	dist   DistManifest
	files  fstest.MapFS
	opt    RunOptions
	out    safeBuf
	errb   safeBuf
	signal chan os.Signal
	clock  *fakeClock

	mu       sync.Mutex
	statuses []ProcessStatus
	changed  chan struct{}
}

// newFixture has the toy loader and two plugins: t/store provides "store" and
// t/user needs it. The profile "p" lists the bundle "b", which the test sets.
func newFixture(t *testing.T, bundle string, extra ...ModuleEntry) *fixture {
	t.Helper()
	f := &fixture{signal: make(chan os.Signal, 1), clock: newFakeClock(), changed: make(chan struct{}, 1)}
	mods := ModuleTable{}
	loader := ModuleEntry{ID: LoaderID, Module: "example.test/loader", Version: "v0.1.0", Plugin: toyLoader(),
		Provides: []string{"composition"}}
	for _, e := range append([]ModuleEntry{loader,
		toy(toySpec{id: "t/store", provides: []kernel.AnyKey{storeKey}}),
		toy(toySpec{id: "t/user", needs: []kernel.AnyKey{storeKey}}),
	}, extra...) {
		mods[e.ID] = e
	}
	f.reg = Registry{Modules: mods, Keys: map[string]kernel.AnyKey{"cache": cacheKey, "policy": policyKey},
		Hooks: map[string]kernel.AnyHook{},
		LoaderConfig: func(rows []loaderdef.EntrySpec, batch BatchFunc, plugins map[string]kernel.Plugin) any {
			return toyLoaderCfg{rows, batch, plugins}
		}}
	// The loader reads the role from profileContext, which is consumer-bound.
	f.dist.Consumers = []Binding{{Key: "profileContext", Plugins: []string{LoaderID}}}
	f.files = fstest.MapFS{
		"profiles/p/profile.yaml": {Data: []byte("bundles: [b]")},
		"bundles/b/bundle.yaml":   {Data: []byte(bundle)},
	}
	f.opt = RunOptions{
		Profile: "p", Home: f.files, Bundles: f.files, Signals: f.signal, Stdout: &f.out, Stderr: &f.errb,
		Args: []string{"--profile", "p"}, Flags: map[string]string{"profile": "p", "home": "/h"},
		Env: func(n string) string {
			switch n {
			case EnvAgentsPaused:
				return "1"
			case "AIOPS_REF_TOKEN":
				return "t"
			}
			return ""
		},
		Limits: shortLimits(), Clock: SystemClock(),
		Publish: func(s ProcessStatus) {
			f.mu.Lock()
			f.statuses = append(f.statuses, s)
			f.mu.Unlock()
			select {
			case f.changed <- struct{}{}:
			default:
			}
		},
	}
	return f
}

func shortLimits() Limits {
	l := DefaultLimits()
	l.LoadDeadline, l.DrainDeadline, l.DisposeDeadline = 2*time.Second, 2*time.Second, 2*time.Second
	l.ShutdownBound = 5 * time.Second
	return l
}

// start runs the launcher in the test's goroutine pool and returns what it needs
// to be waited for. Run has no goroutine of its own, so the test owns this one.
func (f *fixture) start(ctx context.Context) <-chan int {
	f.sign()
	done := make(chan int, 1)
	go func() { done <- Run(ctx, f.opt) }()
	return done
}

// run runs to the end, for a boot that stops by itself.
func (f *fixture) run(ctx context.Context) int {
	f.sign()
	return Run(ctx, f.opt)
}

func (f *fixture) lastStatus() ProcessStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.statuses[len(f.statuses)-1]
}

func (f *fixture) phases() []Phase {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Phase
	for _, s := range f.statuses {
		if len(out) == 0 || out[len(out)-1] != s.Phase {
			out = append(out, s.Phase)
		}
	}
	return out
}

// waitFor waits until the last published status satisfies ok, or Run ends, or the
// test's own bound passes. It waits on the publisher's signal, never on a sleep.
func (f *fixture) waitFor(t *testing.T, done <-chan int, ok func(ProcessStatus) bool) {
	t.Helper()
	bound := time.NewTimer(30 * time.Second)
	defer bound.Stop()
	for {
		f.mu.Lock()
		good := len(f.statuses) > 0 && ok(f.statuses[len(f.statuses)-1])
		f.mu.Unlock()
		if good {
			return
		}
		select {
		case code := <-done:
			t.Fatalf("Run ended with %d first; stderr:\n%s", code, f.errb.String())
		case <-f.changed:
		case <-bound.C:
			t.Fatalf("status did not change as expected; stderr:\n%s", f.errb.String())
		}
	}
}

// waitReady waits until the process has booted: the required set was audited and the
// run loop started. The latch itself is the readiness row's, not boot's.
func (f *fixture) waitReady(t *testing.T, done <-chan int) {
	t.Helper()
	f.waitFor(t, done, func(s ProcessStatus) bool { return s.Phase == PhaseRunning })
}

// safeBuf is a buffer that the test may read while Run writes.
type safeBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// fakeClock records the timers of the launcher and fires them on demand.
type fakeClock struct {
	mu     sync.Mutex
	timers []*fakeTimer
	armed  chan time.Duration
}

type fakeTimer struct {
	d     time.Duration
	f     func()
	fired bool
	stop  bool
}

func newFakeClock() *fakeClock { return &fakeClock{armed: make(chan time.Duration, 16)} }

func (c *fakeClock) AfterFunc(d time.Duration, f func()) func() bool {
	t := &fakeTimer{d: d, f: f}
	c.mu.Lock()
	c.timers = append(c.timers, t)
	c.mu.Unlock()
	if c.armed != nil {
		c.armed <- d
	}
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		was := !t.fired && !t.stop
		t.stop = true
		return was
	}
}

// fire runs every live timer of duration d.
func (c *fakeClock) fire(d time.Duration) {
	c.mu.Lock()
	var run []func()
	for _, t := range c.timers {
		if t.d == d && !t.fired && !t.stop {
			t.fired = true
			run = append(run, t.f)
		}
	}
	c.mu.Unlock()
	for _, f := range run {
		f()
	}
}
