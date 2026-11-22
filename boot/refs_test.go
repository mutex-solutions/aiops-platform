package boot

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/fstest"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// refsReadyKey is the configRefsReady of the fake, as config-refs declares its own.
var refsReadyKey = kernel.NewKey[struct{}](configRefsReady, 0, kernel.ProviderBound())

// refsCheckKey is the dry run of the fake, declared as sdk/def/configrefs/v0 declares
// its key: boot reads it by its name and its method, as it reads the real one.
var refsCheckKey = kernel.NewKey[interface {
	Check(context.Context, any, func(any) error) error
}](configRefsDryRun, 0, kernel.ProviderBound())

// errRefsUnresolved stands in for the definition's ErrUnresolved.
var errRefsUnresolved = errors.New("reference does not resolve")

type fakeCheck struct{}

func (fakeCheck) Check(_ context.Context, cfg any, decode func(any) error) error {
	v, err := resolveRefs(cfg)
	if err != nil || decode == nil {
		return err
	}
	return decode(v)
}

// fakeRefs stands in for the config-refs row: it signals that it entered Apply, waits
// for gate, resolves a ${env:X} reference to "resolved-X" and fails a secret one, and
// provides configRefsReady and the dry run of the same resolution.
func fakeRefs(entered chan<- struct{}, gate <-chan struct{}) ModuleEntry {
	p := kernel.Define(kernel.Plugin{
		ID:       ConfigRefsID,
		Provides: []kernel.AnyKey{refsReadyKey, refsCheckKey},
		Hooks:    []kernel.HookUse{kernel.InternalConfig.Use(kernel.DefaultStage)},
		Apply: func(c *kernel.Context, _ any) error {
			entered <- struct{}{}
			select {
			case <-gate:
			case <-c.Std().Done():
				return c.Std().Err()
			}
			kernel.InternalConfig.On(c, kernel.DefaultStage, func(_ context.Context, raw any, _ kernel.Next[any]) (any, error) {
				return resolveRefs(raw)
			})
			return errors.Join(refsCheckKey.Provide(c, fakeCheck{}), refsReadyKey.Provide(c, struct{}{}))
		},
	})
	return ModuleEntry{ID: ConfigRefsID, Module: "example.test/refs", Version: "v0.1.0", Plugin: p,
		Provides: []string{configRefsReady, configRefsDryRun}, Hooks: []HookDecl{{Hook: "internal/config", StageName: "default"}}}
}

func resolveRefs(v any) (any, error) {
	switch x := v.(type) {
	case launch.Ref:
		if x.Scheme != "env" {
			return nil, fmt.Errorf("reference %s:%s: %w", x.Scheme, x.Name, errRefsUnresolved)
		}
		return "resolved-" + x.Name, nil
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			r, err := resolveRefs(e)
			if err != nil {
				return nil, err
			}
			out[k] = r
		}
		return out, nil
	}
	return v, nil
}

// recorder is a plugin that sends the config it was applied with.
func recorder(id string, got chan<- any) ModuleEntry {
	return toy(toySpec{id: id, apply: func(_ *kernel.Context, cfg any) error { got <- cfg; return nil }})
}

func refsFixture(t *testing.T, bundle string, extra ...ModuleEntry) *fixture {
	t.Helper()
	f := newFixture(t, bundle, extra...)
	f.dist.SealedHooks = []HookListeners{{Hook: "internal/config", Plugins: []string{ConfigRefsID}}}
	f.dist.Providers = []Binding{{Key: configRefsDryRun, Plugins: []string{ConfigRefsID}}, {Key: configRefsReady, Plugins: []string{ConfigRefsID}}}
	return f
}

// A row whose config holds a reference is not applied before config-refs is ACTIVE,
// and then sees the resolved string.
func TestRowWithAReferenceWaitsForConfigRefs(t *testing.T) {
	entered, gate := make(chan struct{}, 1), make(chan struct{})
	plain, withRef := make(chan any, 1), make(chan any, 1)
	f := refsFixture(t, `
- insert:
    - {id: refs, plugin: aiops/config-refs, roles: [api]}
    - {id: plain, plugin: t/plain, roles: [api]}
    - {id: user, plugin: t/rec, roles: [api], config: {note: "${env:AIOPS_REF_NOTE}", n: 1}}
`, fakeRefs(entered, gate), recorder("t/plain", plain), recorder("t/rec", withRef))
	done := f.start(t.Context())
	<-entered
	<-plain // a row with no reference applied; the row with one has had the same chance
	select {
	case cfg := <-withRef:
		t.Fatalf("the row with a reference was applied before config-refs was ACTIVE, with %v", cfg)
	default:
	}
	close(gate)
	cfg := <-withRef
	if want := map[string]any{"note": "resolved-AIOPS_REF_NOTE", "n": int64(1)}; !mapsEqual(cfg, want) {
		t.Fatalf("config %v", cfg)
	}
	f.waitReady(t, done)
	if code := stopAndWait(t, f, done); code != ExitOK {
		t.Fatalf("exit %d, stderr:\n%s", code, f.errb.String())
	}
	// The observer of the root sees the events of the shutdown with the context of the
	// shutdown: config-refs unloading is not a failure of the observer.
	if strings.Contains(f.errb.String(), "warning: observer") {
		t.Errorf("the shutdown made the observer fail:\n%s", f.errb.String())
	}
}

func mapsEqual(got any, want map[string]any) bool {
	m, ok := got.(map[string]any)
	if !ok || len(m) != len(want) {
		return false
	}
	for k, v := range want {
		if m[k] != v {
			return false
		}
	}
	return true
}

// A reference that does not resolve fails the row: an optional row is one grouped
// warning and boot goes on, a required row stops boot (08-profiles-roadmap-adrs.md:468).
func TestUnresolvedReference(t *testing.T) {
	for _, tc := range []struct {
		name     string
		required string
		code     int
	}{{"optional", "", -1}, {"required", ", required: true", ExitRequired}} {
		t.Run(tc.name, func(t *testing.T) {
			entered, gate := make(chan struct{}, 1), make(chan struct{})
			close(gate)
			got := make(chan any, 1)
			f := refsFixture(t, `
- insert:
    - {id: refs, plugin: aiops/config-refs, roles: [api]}
    - {id: user, plugin: t/rec, roles: [api], config: {token: "${secret:x}"}`+tc.required+`}
`, fakeRefs(entered, gate), recorder("t/rec", got))
			// A required row that holds a reference pulls config-refs into the required set
			// (ADR-059 decision 18): the distribution does not list it.
			if tc.code < 0 {
				done := f.start(t.Context())
				f.waitReady(t, done)
				if code := stopAndWait(t, f, done); code != ExitOK {
					t.Fatalf("exit %d", code)
				}
			} else if code := f.run(t.Context()); code != tc.code {
				t.Fatalf("exit %d, stderr:\n%s", code, f.errb.String())
			}
			if !strings.Contains(f.errb.String(), `"user" is FAILED`) && !strings.Contains(f.errb.String(), `required row "user" is FAILED`) {
				t.Fatalf("stderr:\n%s", f.errb.String())
			}
			select {
			case cfg := <-got:
				t.Fatalf("the plugin was applied with %v", cfg)
			default:
			}
		})
	}
}

// With no config-refs row at all, a row with a reference stays PENDING: it is never
// applied with an unresolved reference in its config.
func TestReferenceWithoutConfigRefsStaysPending(t *testing.T) {
	got := make(chan any, 1)
	f := newFixture(t, "- insert:\n    - {id: user, plugin: t/rec, roles: [api], config: {note: \"${env:AIOPS_REF_X}\"}}\n", recorder("t/rec", got))
	done := f.start(t.Context())
	f.waitReady(t, done)
	if code := stopAndWait(t, f, done); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(f.errb.String(), `"user" is PENDING`) {
		t.Fatalf("stderr:\n%s", f.errb.String())
	}
	select {
	case cfg := <-got:
		t.Fatalf("applied with %v", cfg)
	default:
	}
}

func TestRefsInject(t *testing.T) {
	c := comp(t, `
- insert:
    - {id: plain, plugin: t/store, roles: [api], inject: [b]}
    - {id: ref, plugin: t/store, roles: [api], inject: [b, a], config: {x: "${env:AIOPS_REF_X}"}}
    - {id: again, plugin: t/store, roles: [api], inject: [configRefsReady], config: {x: "${env:AIOPS_REF_X}"}}
    - {id: refs, plugin: aiops/config-refs, roles: [api], config: {x: "${env:AIOPS_REF_X}"}}
`)
	got := map[string][]string{}
	for _, r := range c.Rows {
		got[r.ID] = refsInject(r)
	}
	want := map[string][]string{"plain": {"b"}, "ref": {"a", "b", "configRefsReady"}, "again": {"configRefsReady"}, "refs": nil}
	for id, w := range want {
		if !slices.Equal(got[id], w) && !(len(w) == 0 && len(got[id]) == 0) {
			t.Errorf("%s: got %v, want %v", id, got[id], w)
		}
	}
	// The loader gets it through the entry, reload through the launch row.
	for _, e := range entrySpecs(c) {
		if e.ID == "ref" && !slices.Contains(e.Inject, "configRefsReady") {
			t.Errorf("entry %+v", e)
		}
	}
	for _, r := range launchComposition(c).Rows {
		if r.ID == "ref" && !slices.Contains(r.Inject, "configRefsReady") {
			t.Errorf("launch row %+v", r)
		}
	}
}

// refsRun boots config-refs, a row whose config holds a reference and a row that reads the
// registry, with an observer of the test registered ahead of boot's own. hold makes that
// observer stop the event stream at the first event it matches, until release is closed.
type refsRun struct {
	f    *fixture
	r    *run
	root *kernel.Root
	reg  interface {
		Fiber(uint64) (*kernel.Fiber, bool)
	}
	done    chan int
	hold    atomic.Pointer[func(kernel.LedgerEvent) bool]
	held    chan struct{}
	release chan struct{}
	gate    chan struct{}
	once    sync.Once
}

func startRefsRun(t *testing.T, bundle string, extra ...ModuleEntry) *refsRun {
	t.Helper()
	// config-refs applies once per token of gate: the boot's is there, a restart's is not.
	entered, gate := make(chan struct{}, 16), make(chan struct{}, 1)
	gate <- struct{}{}
	regs := make(chan interface {
		Fiber(uint64) (*kernel.Fiber, bool)
	}, 1)
	reader := toy(toySpec{id: "t/registry", needs: []kernel.AnyKey{kernel.RegistryKey}, apply: func(c *kernel.Context, _ any) error {
		regs <- kernel.RegistryKey.Of(c)
		return nil
	}})
	f := refsFixture(t, bundle, append([]ModuleEntry{fakeRefs(entered, gate), reader}, extra...)...)
	f.dist.Consumers = append(f.dist.Consumers, Binding{Key: "registry", Plugins: []string{"t/registry"}})
	f.opt.Registry, f.opt.Dist = f.reg, f.dist
	rr := &refsRun{f: f, r: newRun(f.opt), done: make(chan int, 1), held: make(chan struct{}), release: make(chan struct{}), gate: gate}
	t.Cleanup(rr.unhold)
	roots := make(chan *kernel.Root, 1)
	rr.r.o.Kernel = func(root *kernel.Root) {
		_, err := root.Observe("test-hold", func(_ context.Context, e kernel.LedgerEvent) error {
			if h := rr.hold.Load(); h != nil && (*h)(e) {
				rr.hold.Store(nil)
				close(rr.held)
				<-rr.release
			}
			return nil
		})
		if err != nil {
			t.Error(err)
		}
		roots <- root
	}
	go func() { rr.done <- rr.r.main(t.Context()) }()
	rr.root = <-roots
	f.waitReady(t, rr.done)
	rr.reg = <-regs
	return rr
}

func (rr *refsRun) unhold() { rr.once.Do(func() { close(rr.release) }) }

// fiber returns the live fiber of the plugin id.
func (rr *refsRun) fiber(t *testing.T, plugin string) *kernel.Fiber {
	t.Helper()
	ins, err := rr.root.Inspect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range ins.Fibers {
		if s.Plugin == plugin && s.Alive {
			if f, ok := rr.reg.Fiber(s.ID); ok {
				return f
			}
		}
	}
	t.Fatalf("no live fiber of %s", plugin)
	return nil
}

func (rr *refsRun) stop(t *testing.T) {
	t.Helper()
	rr.unhold()
	rr.f.signal <- syscall.SIGTERM
	if code := <-rr.done; code != ExitOK {
		t.Fatalf("exit %d, stderr:\n%s", code, rr.f.errb.String())
	}
}

const refsBundle = `
- insert:
    - {id: refs, plugin: aiops/config-refs, roles: [api]}
    - {id: reg, plugin: t/registry, roles: [api]}
    - {id: user, plugin: t/rec, roles: [api], config: {note: "${env:AIOPS_REF_NOTE}"}}
`

// Restarting config-refs unloads the rows that hold a reference and brings them back in the
// same settle: once the tree has settled they are ACTIVE, whatever boot's observer has seen.
func TestConfigRefsRestartSettlesWithItsDependants(t *testing.T) {
	rr := startRefsRun(t, refsBundle, recorder("t/rec", make(chan any, 8)))
	refs := rr.fiber(t, ConfigRefsID)
	h := func(e kernel.LedgerEvent) bool {
		return e.Kind == "state" && e.Plugin == ConfigRefsID && e.State == kernel.Active
	}
	rr.hold.Store(&h)
	if err := refs.Update(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	rr.gate <- struct{}{}
	<-rr.held // config-refs is ACTIVE again; boot's observer has not been told yet
	if err := rr.root.Settle(t.Context()); err != nil {
		t.Fatal(err)
	}
	if st, _ := rr.fiber(t, "t/rec").State(t.Context()); st != kernel.Active {
		t.Errorf("the row with a reference is %s once the tree settled, want ACTIVE", st)
	}
	rr.stop(t)
}

// A row mounted in the batch that restarts config-refs is not applied before config-refs is
// ACTIVE again: neither Decode nor Apply ever sees an unresolved reference.
func TestRowMountedWhileConfigRefsRestartsSeesNoReference(t *testing.T) {
	rr := startRefsRun(t, refsBundle, recorder("t/rec", make(chan any, 8)))
	refs := rr.fiber(t, ConfigRefsID)
	k, ok := rr.r.cat.Key("configRefsReady")
	if !ok {
		t.Fatal("configRefsReady is not in the key table")
	}
	var mu sync.Mutex
	var seen []any
	keep := func(v any) {
		mu.Lock()
		seen = append(seen, v)
		mu.Unlock()
	}
	extra := kernel.Define(kernel.Plugin{ID: "t/extra", Inject: []kernel.Dep{kernel.NeedKey(k)},
		Decode: func(raw any) (any, error) { keep(raw); return raw, nil },
		Apply:  func(_ *kernel.Context, cfg any) error { keep(cfg); return nil }})
	h := func(e kernel.LedgerEvent) bool {
		return e.Kind == "state" && e.Plugin == ConfigRefsID && e.State == kernel.Unloading
	}
	rr.hold.Store(&h)
	cfg := map[string]any{"note": launch.Ref{Scheme: "env", Name: "AIOPS_REF_X"}}
	fs, err := rr.root.Batch(t.Context(), kernel.UpdateOp(refs), kernel.MountOp(extra, cfg))
	if err != nil {
		t.Fatal(err)
	}
	<-rr.held // config-refs is quiescing; boot's observer has not been told yet
	_ = fs[1].Await(t.Context())
	rr.gate <- struct{}{} // config-refs may become ACTIVE again
	rr.unhold()
	if err := rr.root.AwaitObservers(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := rr.root.Settle(t.Context()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	for _, v := range seen {
		if m, _ := v.(map[string]any); m != nil {
			if _, raw := m["note"].(launch.Ref); raw {
				t.Errorf("the row saw an unresolved reference: %v (all: %v)", v, seen)
				break
			}
		}
	}
	if len(seen) == 0 {
		t.Error("the row was never applied")
	}
	mu.Unlock()
	rr.stop(t)
}

// liveAdmit boots f and returns a function that admits the generation of a bundle.
func liveAdmit(t *testing.T, f *fixture, l *launched) (func(bundle string) error, <-chan int) {
	t.Helper()
	bindLaunch(f, "t/probe")
	done := f.start(t.Context())
	f.waitReady(t, done)
	l.mu.Lock()
	pc := l.profile
	l.mu.Unlock()
	return func(bundle string) error {
		t.Helper()
		f.files["bundles/b/bundle.yaml"] = &fstest.MapFile{Data: []byte(bundle)}
		c, err := pc.Compose(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		_, err = pc.Admit(t.Context(), c)
		return err
	}, done
}

// Admit dry-runs on config-refs the references of every row the generation mounts with
// a new row: a created, enabled or changed row whose reference does not resolve rejects
// the generation before anything is applied, so the running rows keep their instances
// (ADR-059 decision 16; 08-profiles-roadmap-adrs.md §1.6). A row the generation leaves
// alone is not judged again, though its reference does not resolve.
func TestAdmitDryRunsTheReferencesOfTheRowsItMounts(t *testing.T) {
	var l launched
	entered, gate := make(chan struct{}, 4), make(chan struct{})
	close(gate)
	const base = `- insert:
    - {id: refs, plugin: aiops/config-refs, roles: [api]}
    - {id: probe, plugin: t/probe, roles: [api]}
    - {id: kept, plugin: t/rec, roles: [api], config: {note: "${env:AIOPS_REF_A}"}}
    - {id: plain, plugin: t/rec, roles: [api], config: {note: hello}}
    - {id: bad, plugin: t/rec, roles: [api], config: {note: "${secret:y}"}}
    - {id: off, plugin: t/rec, roles: [api], enabled: false, config: {note: "${secret:x}"}}
`
	f := refsFixture(t, base, fakeRefs(entered, gate), recorder("t/rec", make(chan any, 16)), probe(&l))
	admit, done := liveAdmit(t, f, &l)
	for _, tc := range []struct{ name, add, want string }{
		{"unchanged", "", ""},
		{"created, resolves", "- insert:\n    - {id: new, plugin: t/rec, roles: [api], config: {note: \"${env:AIOPS_REF_B}\"}}\n", ""},
		{"created", "- insert:\n    - {id: new, plugin: t/rec, roles: [api], config: {note: \"${secret:x}\"}}\n", `row "new": reference secret:x`},
		{"enabled", "- {id: off, enabled: true}\n", `row "off": reference secret:x`},
		// The first reference of a running row (notFixed finding 14).
		{"first reference", "- {id: plain, config: {note: \"${secret:x}\"}}\n", `row "plain": reference secret:x`},
		{"changed", "- {id: kept, config: {note: \"${secret:x}\"}}\n", `row "kept": reference secret:x`},
		// Any change of a row is checked, raising required too, though it mounts nothing
		// again: decision 16 names every row the generation changes.
		{"inject changed", "- {id: bad, inject: [cache]}\n", `row "bad": reference secret:y`},
		{"required raised", "- {id: bad, required: true}\n", `row "bad": reference secret:y`},
	} {
		err := admit(base + tc.add)
		switch {
		case tc.want == "" && err != nil, tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: admit = %v, want %q", tc.name, err, tc.want)
		case strings.HasPrefix(tc.want, "row") && !errors.Is(err, errRefsUnresolved):
			t.Errorf("%s: admit = %v, want the dry run's error wrapped", tc.name, err)
		}
	}
	if code := stopAndWait(t, f, done); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
}

// A config that holds a reference is decoded too, on the tree the dry run resolves,
// inside config-refs: a created, enabled or changed row whose reference resolves and
// whose config the plugin's Decode refuses rejects the generation, as one with no
// reference does (08-profiles-roadmap-adrs.md §1.6, "Config fails schema or Validate").
func TestAdmitDecodesAConfigThatHoldsAReference(t *testing.T) {
	var l launched
	entered, gate := make(chan struct{}, 4), make(chan struct{})
	close(gate)
	dec := kernel.Define(kernel.Plugin{ID: "t/dec",
		Decode: func(raw any) (any, error) {
			m, _ := raw.(map[string]any)
			if _, ok := m["level"].(string); !ok {
				return nil, errors.New("level must be a string")
			}
			if n, _ := m["note"].(string); strings.HasPrefix(n, "resolved-AIOPS_REF_BAD") {
				return nil, errors.New("note is not accepted")
			}
			return raw, nil
		},
		Apply: func(*kernel.Context, any) error { return nil }})
	const base = `- insert:
    - {id: refs, plugin: aiops/config-refs, roles: [api]}
    - {id: probe, plugin: t/probe, roles: [api]}
    - {id: run, plugin: t/dec, roles: [api], config: {level: x, note: "${env:AIOPS_REF_A}"}}
    - {id: off, plugin: t/dec, roles: [api], enabled: false, config: {level: 5, note: "${env:AIOPS_REF_A}"}}
`
	f := refsFixture(t, base, fakeRefs(entered, gate), probe(&l), ModuleEntry{ID: "t/dec", Module: "example.test/dec", Version: "v0.1.0", Plugin: dec})
	admit, done := liveAdmit(t, f, &l)
	for _, tc := range []struct{ name, add, want string }{
		{"created, decodes", "    - {id: a, plugin: t/dec, roles: [api], config: {level: x, note: \"${env:AIOPS_REF_A}\"}}\n", ""},
		{"created, no reference", "    - {id: a, plugin: t/dec, roles: [api], config: {level: 5, note: hi}}\n", `row "a": config: level must be a string`},
		{"created", "    - {id: a, plugin: t/dec, roles: [api], config: {level: 5, note: \"${env:AIOPS_REF_A}\"}}\n", `row "a": config: level must be a string`},
		{"created, resolved value refused", "    - {id: a, plugin: t/dec, roles: [api], config: {level: x, note: \"${env:AIOPS_REF_BAD}\"}}\n", `row "a": config: note is not accepted`},
		{"enabled", "- {id: off, enabled: true}\n", `row "off": config: level must be a string`},
		{"changed", "- {id: run, config: {level: 7, note: \"${env:AIOPS_REF_A}\"}}\n", `row "run": config: level must be a string`},
	} {
		err := admit(base + tc.add)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%s: admit = %v, want %q", tc.name, err, tc.want)
		}
	}
	if code := stopAndWait(t, f, done); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
}

// The running config-refs cannot check the references of a generation that also
// disables, removes or changes config-refs: the tree would resolve them with another
// instance, or with none. Such a generation is rejected when a row it creates, enables
// or changes holds a reference (ADR-059 decision 16); one that holds none is admitted.
func TestAdmitRejectsAReferenceWhenTheGenerationChangesConfigRefs(t *testing.T) {
	var l launched
	entered, gate := make(chan struct{}, 4), make(chan struct{})
	close(gate)
	const base = `- insert:
    - {id: refs, plugin: aiops/config-refs, roles: [api]}
    - {id: probe, plugin: t/probe, roles: [api]}
    - {id: kept, plugin: t/rec, roles: [api], config: {note: "${env:AIOPS_REF_A}"}}
`
	const newRef = "- insert:\n    - {id: new, plugin: t/rec, roles: [api], config: {note: \"${env:AIOPS_REF_B}\"}}\n"
	f := refsFixture(t, base, fakeRefs(entered, gate), recorder("t/rec", make(chan any, 16)), probe(&l))
	admit, done := liveAdmit(t, f, &l)
	for _, tc := range []struct{ name, add, want string }{
		{"disabled, created row with a reference", "- {id: refs, enabled: false}\n" + newRef, `row "new": config-refs changes in this generation`},
		{"changed, created row with a reference", "- {id: refs, config: {x: 1}}\n" + newRef, `row "new": config-refs changes in this generation`},
		{"disabled, changed row with a reference", "- {id: refs, enabled: false}\n- {id: kept, config: {note: \"${env:AIOPS_REF_C}\"}}\n", `row "kept": config-refs changes in this generation`},
		{"changed, created row with no reference", "- {id: refs, config: {x: 1}}\n- insert:\n    - {id: new, plugin: t/rec, roles: [api], config: {note: x}}\n", ""},
		{"required raised, created row with a reference", "- {id: refs, required: true}\n" + newRef, ""},
	} {
		err := admit(base + tc.add)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want) || !errors.Is(err, launch.ErrRefsUnchecked)) {
			t.Errorf("%s: admit = %v, want %q (launch.ErrRefsUnchecked)", tc.name, err, tc.want)
		}
	}
	if code := stopAndWait(t, f, done); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
}

// With no config-refs ACTIVE, a generation that mounts a row holding a reference is
// rejected with launch.ErrRefsUnchecked: nothing can check the reference (ADR-059
// decision 16). One with no reference is admitted.
func TestAdmitRejectsAReferenceWhileConfigRefsIsNotActive(t *testing.T) {
	var l launched
	const base = "- insert:\n    - {id: probe, plugin: t/probe, roles: [api]}\n"
	f := newFixture(t, base, probe(&l), recorder("t/rec", make(chan any, 4)))
	admit, done := liveAdmit(t, f, &l)
	err := admit(base + "    - {id: new, plugin: t/rec, roles: [api], config: {note: \"${env:AIOPS_REF_X}\"}}\n")
	if err == nil || !strings.Contains(err.Error(), `row "new": config-refs is not ACTIVE`) || !errors.Is(err, launch.ErrRefsUnchecked) {
		t.Errorf("admit = %v, want launch.ErrRefsUnchecked", err)
	}
	if err := admit(base + "    - {id: new, plugin: t/rec, roles: [api], config: {note: x}}\n"); err != nil {
		t.Errorf("admit without a reference = %v", err)
	}
	if code := stopAndWait(t, f, done); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
}

// A changed group is replaced by the loader with the rows it holds, so they are mounted
// again too; a change of required or roles alone mounts nothing again, and is still a
// change of the row (ADR-059 decision 16).
func TestRemountedHoldsTheChildrenOfAChangedGroup(t *testing.T) {
	const bundle = `- insert:
    - {id: g, plugin: core:group, config: [{id: c, plugin: t/store}, {id: h, plugin: core:group, config: [{id: d, plugin: t/store}]}]}
    - {id: k, plugin: core:group, config: [{id: e, plugin: t/store}]}
    - {id: m, plugin: t/store, roles: [%s]}
    - {id: n, plugin: t/store, enabled: false}
`
	old := comp(t, fmt.Sprintf(bundle, "api"))
	nw := comp(t, fmt.Sprintf(bundle, "api, worker"), "- {id: g, inject: [x]}\n- {id: k, required: true}\n- {id: n, enabled: true}\n")
	changed, got := changedRows(old, nw, nil)
	set := func(m map[string]bool) []string {
		var ids []string
		for id, ok := range m {
			if ok {
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
		return ids
	}
	if ids, want := set(got), []string{"g", "g:c", "g:h", "g:h:d", "n"}; !slices.Equal(ids, want) {
		t.Errorf("remounted %v, want %v", ids, want)
	}
	if ids, want := set(changed), []string{"g", "g:c", "g:h", "g:h:d", "k", "m", "n"}; !slices.Equal(ids, want) {
		t.Errorf("changed %v, want %v", ids, want)
	}
}

// Two config-refs rows are two providers of configRefsReady in one realm: boot stops
// (03-c3-kernel.md:359), instead of following whichever of them changed state last.
func TestTwoConfigRefsRowsStopBoot(t *testing.T) {
	entered, gate := make(chan struct{}, 2), make(chan struct{})
	close(gate)
	f := refsFixture(t, `
- insert:
    - {id: refs, plugin: aiops/config-refs, roles: [api]}
    - {id: refs2, plugin: aiops/config-refs, roles: [api]}
`, fakeRefs(entered, gate))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	pub := f.opt.Publish
	f.opt.Publish = func(s ProcessStatus) {
		pub(s)
		if s.Phase == PhaseRunning {
			cancel()
		}
	}
	if code := f.run(ctx); code != ExitRefused || !strings.Contains(f.errb.String(), "two-providers") || !strings.Contains(f.errb.String(), "configRefsReady") {
		t.Fatalf("exit %d, stderr:\n%s", code, f.errb.String())
	}
}

// The static graph knows that a row with a reference needs config-refs: a required one
// with no config-refs row stops boot before any row mounts, and the restart closure of
// config-refs holds it.
func TestStaticGraphHasConfigRefsReady(t *testing.T) {
	f := newFixture(t, "- insert:\n    - {id: user, plugin: t/rec, roles: [api], required: true, config: {note: \"${env:AIOPS_REF_X}\"}}\n", recorder("t/rec", make(chan any, 1)))
	if code := f.run(t.Context()); code != ExitRefused || !strings.Contains(f.errb.String(), `row "user" needs key "configRefsReady"`) {
		t.Fatalf("exit %d, stderr:\n%s", code, f.errb.String())
	}

	var l launched
	entered, gate := make(chan struct{}, 2), make(chan struct{})
	close(gate)
	const bundle = "- insert:\n    - {id: refs, plugin: aiops/config-refs, roles: [api]%s}\n    - {id: user, plugin: t/rec, roles: [api], config: {note: \"${env:AIOPS_REF_X}\"}}\n    - {id: probe, plugin: t/probe, roles: [api]}\n"
	f = refsFixture(t, fmt.Sprintf(bundle, ""), fakeRefs(entered, gate), recorder("t/rec", make(chan any, 4)), probe(&l))
	bindLaunch(f, "t/probe")
	done := f.start(t.Context())
	f.waitReady(t, done)
	l.mu.Lock()
	pc := l.profile
	l.mu.Unlock()
	f.files["bundles/b/bundle.yaml"] = &fstest.MapFile{Data: []byte(fmt.Sprintf(bundle, ", config: {x: 1}"))}
	next, err := pc.Compose(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	pl, err := pc.Plan(t.Context(), pc.Committed(), next)
	if err != nil || !slices.Contains(pl.Closure, "user") {
		t.Errorf("plan: %v, closure %v: a restart of config-refs restarts the row with a reference", err, pl.Closure)
	}
	if code := stopAndWait(t, f, done); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
}
