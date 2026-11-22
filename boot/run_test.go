package boot

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	loaderdef "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

const happyBundle = `
- insert:
    - {id: store, plugin: t/store, roles: [api]}
    - {id: user, plugin: t/user, roles: [api]}
`

// stop sends the signal and waits for the exit code.
func stopAndWait(t *testing.T, f *fixture, done <-chan int) int {
	t.Helper()
	f.signal <- syscall.SIGTERM
	return <-done
}

func TestRunBootsAndStopsOnSignal(t *testing.T) {
	f := newFixture(t, happyBundle)
	done := f.start(t.Context())
	f.waitReady(t, done)
	if code := stopAndWait(t, f, done); code != ExitOK {
		t.Fatalf("exit %d, stderr:\n%s", code, f.errb.String())
	}
	if got := f.phases(); !slices.Equal(got, []Phase{PhaseStarting, PhaseRunning, PhaseStopping, PhaseStopped}) {
		t.Fatalf("phases %v", got)
	}
	if s := f.lastStatus(); s.Ready || s.Exit != 0 || s.Digest == "" || s.Role != "all" || s.Profile != "p" {
		t.Fatalf("final status %+v", s)
	}
	if !strings.Contains(f.out.String(), "aiops: booted profile=p role=all") || !strings.Contains(f.errb.String(), "aiops: stopping: signal") {
		t.Fatalf("stdout %q stderr %q", f.out.String(), f.errb.String())
	}
}

func TestRunStopsWhenContextEnds(t *testing.T) {
	f := newFixture(t, happyBundle)
	ctx, cancel := context.WithCancel(t.Context())
	done := f.start(ctx)
	f.waitReady(t, done)
	cancel()
	if code := <-done; code != ExitOK {
		t.Fatalf("exit %d, stderr:\n%s", code, f.errb.String())
	}
}

func TestRunRoleFilter(t *testing.T) {
	f := newFixture(t, happyBundle+"    - {id: other, plugin: t/store, roles: [worker]}\n")
	f.opt.Role = "api"
	done := f.start(t.Context())
	f.waitReady(t, done)
	if got := f.lastStatus().Role; got != "api" {
		t.Fatalf("role %q", got)
	}
	if code := stopAndWait(t, f, done); code != ExitOK {
		t.Fatalf("exit %d, stderr:\n%s", code, f.errb.String())
	}
}

// launched records the launch services that a row resolved.
type launched struct {
	mu      sync.Mutex
	profile launch.ProfileContext
	cat     launch.Catalog
	cmd     launch.Cmdline
	ready   launch.Ready
	sw      launch.KillSwitches
	exit    launch.AppExit
}

func probe(l *launched) ModuleEntry {
	return toy(toySpec{id: "t/probe", needs: []kernel.AnyKey{launch.ProfileContextKey, launch.CatalogKey, launch.CmdlineKey,
		launch.ReadyKey, launch.KillSwitchesKey, launch.AppExitKey},
		apply: func(c *kernel.Context, _ any) error {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.profile, l.cat, l.cmd = launch.ProfileContextKey.Of(c), launch.CatalogKey.Of(c), launch.CmdlineKey.Of(c)
			l.ready, l.sw, l.exit = launch.ReadyKey.Of(c), launch.KillSwitchesKey.Of(c), launch.AppExitKey.Of(c)
			return nil
		}})
}

// bindLaunch lets the plugins of ids resolve the two consumer-bound launch keys,
// profileContext and appExit. newFixture lists the loader for profileContext.
func bindLaunch(f *fixture, ids ...string) {
	for i, b := range f.dist.Consumers {
		if b.Key == "profileContext" {
			f.dist.Consumers[i].Plugins = append(slices.Clone(b.Plugins), ids...)
		}
	}
	f.dist.Consumers = append(f.dist.Consumers, Binding{Key: "appExit", Plugins: ids})
}

func TestLaunchServices(t *testing.T) {
	var l launched
	f := newFixture(t, `
- insert:
    - {id: store, plugin: t/store, roles: [api]}
    - {id: user, plugin: t/user, roles: [api], required: true}
    - {id: probe, plugin: t/probe, roles: [api]}
`, probe(&l))
	f.dist.RequiredKeys, f.dist.RequiredRows = []string{"store"}, []string{"loader"}
	bindLaunch(f, "t/probe")
	done := f.start(t.Context())
	f.waitReady(t, done)
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.profile.Profile() != "p" || l.profile.Role() != "all" {
		t.Fatalf("profile %q role %q", l.profile.Profile(), l.profile.Role())
	}
	layers := l.profile.Layers()
	if len(layers) != 1 || layers[0] != (launch.Layer{Number: 1, Source: "bundle:b", File: "bundles/b/bundle.yaml"}) {
		t.Fatalf("layers %+v", layers)
	}
	if c, _ := l.cmd.Flag("home"); c != "/h" {
		t.Fatalf("flag home %q", c)
	}
	if _, ok := l.cmd.Flag("nope"); ok || !slices.Equal(l.cmd.Args(), []string{"--profile", "p"}) {
		t.Fatalf("cmdline %v", l.cmd.Args())
	}
	if !l.sw.On(launch.AgentsPaused) || l.sw.On(launch.TelemetryDisabled) || l.sw.On("unknown") {
		t.Fatal("kill switches")
	}
	if l.ready.IsReady() {
		t.Fatal("boot sets no latch: the readiness row does")
	}
	l.ready.Set() // the row that owns the latch sets it, and the process says so
	f.waitFor(t, done, func(s ProcessStatus) bool { return s.Ready })
	l.ready.Drop()
	f.waitFor(t, done, func(s ProcessStatus) bool { return !s.Ready })
	if env, ok := l.cmd.Env("AIOPS_AGENTS_PAUSED"); !ok || env != "1" {
		t.Fatalf("env %q %v", env, ok)
	}
	if _, ok := l.cmd.Env("AIOPS_NOT_SET"); ok {
		t.Fatal("an unset variable")
	}

	// catalog
	ids := []string{}
	for _, e := range l.cat.Entries() {
		ids = append(ids, e.ID)
	}
	if !slices.Equal(ids, []string{"aiops/loader", "t/probe", "t/store", "t/user"}) {
		t.Fatalf("entries %v", ids)
	}
	if e, ok := l.cat.Lookup("t/user"); !ok || !slices.Equal(e.Needs, []string{"store"}) {
		t.Fatalf("lookup %+v", e)
	}
	if _, ok := l.cat.Lookup("t/none"); ok {
		t.Fatal("lookup of an unknown id")
	}
	// A plugin value runs code under its entry's bindings: only the loader gets the
	// table, in its config, and no catalog value hands one out.
	if _, ok := l.cat.(interface {
		Plugin(string) (kernel.Plugin, bool)
	}); ok {
		t.Fatal("the catalog hands out plugin values")
	}
	for _, name := range []string{"store", "profileContext", "loader", "composition", "registry", "logger"} {
		if k, ok := l.cat.Key(name); !ok || k.Name() != name {
			t.Fatalf("key %q: %v", name, ok)
		}
	}
	if _, ok := l.cat.Key("ghost"); ok {
		t.Fatal("key of an unknown name")
	}

	// The required set: the key and the row a layer marked, with no row "loader".
	if got := l.profile.Required(); !slices.Equal(got.Keys, []string{"store"}) || !slices.Equal(got.Rows, []string{"user"}) {
		t.Fatalf("required %+v", got)
	}

	// compose, admit, plan
	ctx := t.Context()
	if _, err := l.profile.Compose(ctx); err != nil { // composed twice: remembered once
		t.Fatal(err)
	}
	c, err := l.profile.Compose(ctx)
	if err != nil || len(c.Rows) != 3 || c.Digest == (launch.Digest{}) || l.profile.Committed().Digest != c.Digest {
		t.Fatalf("compose: %v %+v", err, c)
	}
	if c.Rows[0].ID != "store" || c.Rows[0].Plugin != "t/store" || !c.Rows[0].Enabled || !slices.Equal(c.Rows[0].Roles, []string{"api"}) {
		t.Fatalf("row %+v", c.Rows[0])
	}
	adm, err := l.profile.Admit(ctx, c)
	if err != nil || len(adm.Denied) != 0 {
		t.Fatalf("admit: %v %+v", err, adm)
	}
	same, err := l.profile.Plan(ctx, c, c)
	if err != nil || same.From != same.To {
		t.Fatalf("plan: %v %+v", err, same)
	}
	for _, e := range same.Entries {
		if e.Action != launch.ActionUnchanged {
			t.Fatalf("entry %+v", e)
		}
	}

	// A new generation: one row added, one optional row denied.
	f.files["bundles/b/bundle.yaml"] = &fstest.MapFile{Data: []byte(happyBundle + `
    - {id: probe, plugin: t/probe, roles: [api]}
    - {id: cached, plugin: t/user, roles: [api]}
    - {id: ghost, plugin: t/ghost, roles: [api]}
`)}
	next, err := l.profile.Compose(ctx)
	if err != nil {
		t.Fatal(err)
	}
	adm, err = l.profile.Admit(ctx, next)
	if err != nil || len(adm.Denied) != 1 || adm.Denied[0].Row != "ghost" || adm.Denied[0].Check != "module table" {
		t.Fatalf("admit: %v %+v", err, adm)
	}
	pl, err := l.profile.Plan(ctx, c, next)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]launch.Action{}
	for _, e := range pl.Entries {
		got[e.ID] = e.Action
	}
	if got["cached"] != launch.ActionCreate || got["store"] != launch.ActionUnchanged || got["ghost"] != launch.ActionCreate {
		t.Fatalf("plan %v", got)
	}

	// A composition that the launcher did not make is refused, and so is a bad file.
	if _, err := l.profile.Admit(ctx, launch.Composition{Digest: launch.Digest{1}}); err == nil {
		t.Fatal("admit of a stranger")
	}
	if _, err := l.profile.Plan(ctx, launch.Composition{Digest: launch.Digest{1}}, c); err == nil {
		t.Fatal("plan from a stranger")
	}
	if _, err := l.profile.Plan(ctx, c, launch.Composition{Digest: launch.Digest{2}}); err == nil {
		t.Fatal("plan to a stranger")
	}
	f.files["bundles/b/bundle.yaml"] = &fstest.MapFile{Data: []byte(happyBundle + "    - {id: loop1, plugin: t/store, roles: [api]}\n    - {id: loop2, plugin: t/store, roles: [api]}\n")}
	bad, err := l.profile.Compose(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.profile.Admit(ctx, bad); err == nil || !strings.Contains(err.Error(), "two-providers") {
		t.Fatalf("admit of two providers: %v", err)
	}
	f.files["bundles/b/bundle.yaml"] = &fstest.MapFile{Data: []byte("")}
	if _, err := l.profile.Compose(ctx); err == nil {
		t.Fatal("compose of an empty file")
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := l.profile.Compose(cctx); err == nil {
		t.Fatal("compose with an ended context")
	}
	if _, err := l.profile.Admit(cctx, c); err == nil {
		t.Fatal("admit with an ended context")
	}
	if _, err := l.profile.Plan(cctx, c, c); err == nil {
		t.Fatal("plan with an ended context")
	}

	// The candidates are bounded: the oldest is forgotten, the committed one is not, and
	// one that is looked up again counts as the newest.
	compose := func(extra string) launch.Composition {
		f.files["bundles/b/bundle.yaml"] = &fstest.MapFile{Data: []byte(happyBundle + extra)}
		c, err := l.profile.Compose(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	first, second := compose(""), compose("    - {id: n0, plugin: t/store, roles: [api]}\n")
	for i := range maxKnown - 2 {
		compose("    - {id: n" + string(rune('a'+i)) + "1, plugin: t/store, roles: [api]}\n")
	}
	if _, err := l.profile.Plan(ctx, first, first); err != nil { // first is in use again
		t.Fatal(err)
	}
	compose("    - {id: x1, plugin: t/store, roles: [api]}\n")
	compose("    - {id: x2, plugin: t/store, roles: [api]}\n")
	if _, err := l.profile.Admit(ctx, second); err == nil {
		t.Fatal("the oldest candidate should be forgotten")
	}
	if _, err := l.profile.Admit(ctx, first); err != nil {
		t.Fatalf("the candidate in use is kept: %v", err)
	}
	if _, err := l.profile.Admit(ctx, c); err != nil {
		t.Fatalf("the committed composition is kept: %v", err)
	}

	if code := stopAndWait(t, f, done); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
}

func TestRunRefusals(t *testing.T) {
	cyc1 := toy(toySpec{id: "t/cyc1", provides: []kernel.AnyKey{storeKey}, needs: []kernel.AnyKey{cacheKey}})
	cyc2 := toy(toySpec{id: "t/cyc2", provides: []kernel.AnyKey{cacheKey}, needs: []kernel.AnyKey{storeKey}})
	tests := []struct {
		name   string
		bundle string
		tweak  func(f *fixture)
		extra  []ModuleEntry
		code   int
		want   string
	}{
		{name: "missing file", bundle: happyBundle, tweak: func(f *fixture) { delete(f.files, "bundles/b/bundle.yaml") }, code: ExitRefused, want: "file-missing"},
		{name: "unknown plugin, required row", bundle: happyBundle + "    - {id: ghost, plugin: t/ghost, roles: [api], required: true}\n",
			code: ExitRefused, want: "not in the module table"},
		{name: "cycle", bundle: "- insert:\n    - {id: a, plugin: t/cyc1, roles: [api]}\n    - {id: b, plugin: t/cyc2, roles: [api]}\n",
			extra: []ModuleEntry{cyc1, cyc2}, code: ExitRefused, want: "static graph"},
		{name: "required key has no provider", bundle: happyBundle, tweak: func(f *fixture) { f.dist.RequiredKeys = []string{"cache"} },
			code: ExitRequired, want: "required key"},
		{name: "reserved row id", bundle: "- insert:\n    - {id: loader, plugin: t/store, roles: [api]}\n", code: ExitRefused, want: "is reserved"},
		{name: "bad role", bundle: happyBundle, tweak: func(f *fixture) { f.opt.Role = "root" }, code: ExitUsage, want: "role \"root\""},
		{name: "bad limits", bundle: happyBundle, tweak: func(f *fixture) { f.opt.Limits.OwnerInbox = 0 }, code: ExitRefused, want: "OwnerInbox"},
		{name: "no loader", bundle: happyBundle, tweak: func(f *fixture) { delete(f.reg.Modules, LoaderID) }, code: ExitRefused, want: "has no aiops/loader"},
		{name: "no loader config", bundle: happyBundle, tweak: func(f *fixture) { f.reg.LoaderConfig = nil }, code: ExitRefused, want: "no LoaderConfig"},
		{name: "plugin is not defined", bundle: happyBundle, tweak: func(f *fixture) {
			e := f.reg.Modules["t/store"]
			e.Plugin = "x"
			f.reg.Modules["t/store"] = e
		}, code: ExitRefused, want: "want a kernel.Plugin"},
		{name: "plugin id differs", bundle: happyBundle, tweak: func(f *fixture) {
			e := f.reg.Modules["t/store"]
			e.ID = "t/store"
			p := e.Plugin.(kernel.Plugin)
			p.ID = "t/other"
			e.Plugin = p
			f.reg.Modules["t/store"] = e
		}, code: ExitRefused, want: "declares the id \"t/other\""},
		{name: "entry and plugin disagree", bundle: happyBundle, tweak: func(f *fixture) {
			e := f.reg.Modules["t/user"]
			e.Needs = []string{"cache"}
			f.reg.Modules["t/user"] = e
		}, code: ExitRefused, want: "differ"},
		{name: "key not in the table", bundle: happyBundle, tweak: func(f *fixture) {
			e := f.reg.Modules["t/user"]
			e.Wants = []string{"ghost"}
			f.reg.Modules["t/user"] = e
		}, code: ExitRefused, want: "not in the key table"},
		{name: "major clash", bundle: happyBundle, tweak: func(f *fixture) {
			f.reg.Keys["store"] = kernel.NewKey[string]("store", 1)
		}, code: ExitRefused, want: "two keys are named \"store\""},
		{name: "binding names an unknown key", bundle: happyBundle, tweak: func(f *fixture) {
			f.dist.Providers = []Binding{{Key: "ghost", Plugins: []string{"t/store"}}}
		}, code: ExitRefused, want: "not in the key table"},
		{name: "sealed hook not registered", bundle: happyBundle, tweak: func(f *fixture) {
			f.dist.SealedHooks = []HookListeners{{Hook: "t/hook", Plugins: []string{"t/store"}}}
		}, code: ExitRefused, want: "not in Registry.Hooks"},
		{name: "sealed hook names an unknown plugin", bundle: happyBundle, tweak: func(f *fixture) {
			f.reg.Hooks["t/hook"] = kernel.NewEvent[string]("t/hook", kernel.OwnedBy(storeKey), kernel.Sealed())
			f.dist.SealedHooks = []HookListeners{{Hook: "t/hook", Plugins: []string{"t/ghost"}}}
		}, code: ExitRefused, want: "not in the module table"},
		{name: "kernel refuses the bindings", bundle: happyBundle, tweak: func(f *fixture) {
			f.dist.Providers = []Binding{{Key: "store", Plugins: []string{"t/store"}}} // an open key
		}, code: ExitInternal, want: "kernel:"},
		{name: "prepare fails", bundle: happyBundle, tweak: func(f *fixture) {}, code: ExitInternal, want: "prepare:"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, tc.bundle, tc.extra...)
			if tc.tweak != nil {
				tc.tweak(f)
			}
			var code int
			if tc.name == "prepare fails" {
				f.opt.Registry, f.opt.Dist = f.reg, f.dist
				r := newRun(f.opt)
				r.o.Kernel = func(root *kernel.Root) { _ = root.Close(context.Background()) }
				code = r.main(t.Context())
			} else {
				code = f.run(t.Context())
			}
			if code != tc.code || !strings.Contains(f.errb.String(), tc.want) {
				t.Fatalf("exit %d, want %d with %q; stderr:\n%s", code, tc.code, tc.want, f.errb.String())
			}
			if s := f.lastStatus(); s.Phase != PhaseStopped || s.Exit != tc.code {
				t.Fatalf("status %+v", s)
			}
		})
	}
}

func TestKernelConfigNamesUnknownPlugins(t *testing.T) {
	f := newFixture(t, happyBundle)
	f.opt.Registry = f.reg
	r := newRun(f.opt)
	cat, err := newCatalog(f.reg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.kernelConfig(cat, DistManifest{
		Providers: []Binding{{Key: "policy", Plugins: []string{"t/ghost"}}},
		Consumers: []Binding{{Key: "appExit", Plugins: []string{"t/ghost2"}}},
	})
	if err == nil || !strings.Contains(err.Error(), `provider binding of key "policy" names plugin "t/ghost"`) || !strings.Contains(err.Error(), `consumer binding of key "appExit" names plugin "t/ghost2"`) {
		t.Fatalf("error: %v", err)
	}
}

func TestRunWithSealedHookAndObserverLog(t *testing.T) {
	f := newFixture(t, happyBundle)
	f.reg.Hooks["t/hook"] = kernel.NewEvent[string]("t/hook", kernel.OwnedBy(storeKey), kernel.Sealed())
	f.dist.SealedHooks = []HookListeners{{Hook: "t/hook", Plugins: []string{"t/store"}}, {Hook: "internal/status", Plugins: []string{"t/user"}}}
	done := f.start(t.Context())
	f.waitReady(t, done)
	if code := stopAndWait(t, f, done); code != ExitOK {
		t.Fatalf("exit %d, stderr:\n%s", code, f.errb.String())
	}
	// The log of the kernel's observers goes to stderr.
	r := newRun(f.opt)
	cat, err := newCatalog(f.reg)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := r.kernelConfig(cat, f.dist)
	if err != nil || len(cfg.Listeners) != 2 {
		t.Fatalf("config: %v %+v", err, cfg.Listeners)
	}
	cfg.ObserverLog(errors.New("slow observer"))
	if !strings.Contains(f.errb.String(), "warning: observer: slow observer") {
		t.Fatalf("stderr:\n%s", f.errb.String())
	}
}

func TestRunWarnings(t *testing.T) {
	// A patch of an unknown id is a warning; an optional row that fails, and one that
	// waits for a key, are one grouped warning; the process still becomes ready.
	bad := toy(toySpec{id: "t/bad", apply: func(*kernel.Context, any) error { return errors.New("boom") }})
	wanter := toy(toySpec{id: "t/wanter", wants: []kernel.AnyKey{storeKey}})
	f := newFixture(t, `
- insert:
    - {id: store, plugin: t/store, roles: [api]}
    - {id: wanter, plugin: t/wanter, roles: [api]}
    - {id: bad, plugin: t/bad, roles: [api]}
    - {id: waits, plugin: t/user, roles: [api]}
    - {id: waits2, plugin: t/user, roles: [api], inject: [cache]}
- {id: nobody, enabled: false}
`, bad, wanter)
	f.dist.RequiredRows = nil
	f.dist.RequiredKeys = []string{"store"}
	done := f.start(t.Context())
	f.waitReady(t, done)
	if code := stopAndWait(t, f, done); code != ExitOK {
		t.Fatalf("exit %d, stderr:\n%s", code, f.errb.String())
	}
	for _, want := range []string{"warning: ", "unknown-id", `"bad" is FAILED`, `"waits2" is PENDING and waits for cache`, "settle:"} {
		if !strings.Contains(f.errb.String(), want) {
			t.Errorf("stderr lacks %q:\n%s", want, f.errb.String())
		}
	}
}

func TestRunRequiredRowFails(t *testing.T) {
	bad := toy(toySpec{id: "t/bad", apply: func(*kernel.Context, any) error { return errors.New("boom") }})
	f := newFixture(t, "- insert:\n    - {id: bad, plugin: t/bad, roles: [api], required: true}\n", bad)
	// The finding is printed as its line, as the static audit prints it, not as a Go value.
	if code := f.run(t.Context()); code != ExitRequired || !strings.Contains(f.errb.String(), "error: required set: required-missing: required row \"bad\" is FAILED, not ACTIVE\n") {
		t.Fatalf("exit %d, stderr:\n%s", code, f.errb.String())
	}
	if s := f.lastStatus(); s.Ready || s.Phase != PhaseStopped || s.Exit != ExitRequired || !strings.Contains(s.Detail, "required set is not met") {
		t.Fatalf("status %+v", s)
	}
}

func TestRunRequiredKeyMissingAtRunTime(t *testing.T) {
	// The provider is optional and fails, so the key has no ACTIVE provider at run time.
	bad := toy(toySpec{id: "t/badstore", provides: []kernel.AnyKey{storeKey}, apply: func(*kernel.Context, any) error { return errors.New("boom") }})
	f := newFixture(t, "- insert:\n    - {id: s, plugin: t/badstore, roles: [api]}\n", bad)
	f.dist.RequiredKeys = []string{"store"}
	if code := f.run(t.Context()); code != ExitRequired || !strings.Contains(f.errb.String(), `required key "store" has no ACTIVE provider`) {
		t.Fatalf("exit %d, stderr:\n%s", code, f.errb.String())
	}
}

func TestRunLoaderFails(t *testing.T) {
	f := newFixture(t, happyBundle)
	f.reg.LoaderConfig = func([]loaderdef.EntrySpec, BatchFunc, map[string]kernel.Plugin) any { return "not a config" }
	code := f.run(t.Context())
	if code != ExitRequired || !strings.Contains(f.errb.String(), "error: loader:") || !strings.Contains(f.errb.String(), `required row "loader" is FAILED`) {
		t.Fatalf("exit %d, stderr:\n%s", code, f.errb.String())
	}
}

func TestRunAppExit(t *testing.T) {
	for _, tc := range []struct {
		reason launch.ExitReason
		code   int
	}{
		{launch.ExitDisposeLeak, ExitLeak}, {launch.ExitGaveUp, ExitGaveUp}, {launch.ExitRestoreFail, ExitRestore}, {"mystery", ExitInternal},
	} {
		t.Run(string(tc.reason), func(t *testing.T) {
			exiter := toy(toySpec{id: "t/exiter", needs: []kernel.AnyKey{launch.AppExitKey}, apply: func(c *kernel.Context, _ any) error {
				x := launch.AppExitKey.Of(c)
				x.RequestExit(launch.ExitRequest{Reason: tc.reason, Detail: "the detail"})
				x.RequestExit(launch.ExitRequest{Reason: launch.ExitRestoreFail}) // dropped: the first request wins
				x.RequestExit(launch.ExitRequest{Reason: launch.ExitRestoreFail})
				return nil
			}})
			f := newFixture(t, happyBundle+"    - {id: exiter, plugin: t/exiter, roles: [api]}\n", exiter)
			bindLaunch(f, "t/exiter")
			code := f.run(t.Context())
			if code != tc.code || !strings.Contains(f.errb.String(), "process replaced: "+string(tc.reason)+": the detail") {
				t.Fatalf("exit %d, want %d; stderr:\n%s", code, tc.code, f.errb.String())
			}
		})
	}
}

// slowDispose is a plugin whose disposer ignores its context until release is closed,
// and which disposes itself when trigger is closed: a fiber that leaks.
func slowDispose(trigger, release chan struct{}) ModuleEntry {
	return toy(toySpec{id: "t/slow", apply: func(c *kernel.Context, _ any) error {
		c.Defer("slow", func(context.Context) error { <-release; return nil })
		c.Go("trigger", func(ctx context.Context) error {
			select {
			case <-trigger:
				return c.Dispose()
			case <-ctx.Done():
				return nil
			}
		})
		return nil
	}})
}

func TestRunLeakedFiberReplacesTheProcess(t *testing.T) {
	trigger, release := make(chan struct{}), make(chan struct{})
	f := newFixture(t, happyBundle+"    - {id: slow, plugin: t/slow, roles: [api]}\n", slowDispose(trigger, release))
	f.opt.Limits.DisposeDeadline = 30 * time.Millisecond
	f.opt.Limits.LeakGrace = 7 * time.Millisecond
	f.opt.Clock = f.clock
	done := f.start(t.Context())
	f.waitReady(t, done)
	close(trigger)
	f.waitFor(t, done, func(s ProcessStatus) bool { return !s.Ready }) // readiness ends at once
	for d := range f.clock.armed {                                     // the grace timer is armed
		if d == 7*time.Millisecond {
			break
		}
	}
	// A second ping while the grace period runs changes nothing.
	f.clock.fire(7 * time.Millisecond)
	close(release) // the leaked disposer ends, so that the close can finish
	if code := <-done; code != ExitLeak {
		t.Fatalf("exit %d, stderr:\n%s", code, f.errb.String())
	}
	if !strings.Contains(f.errb.String(), "error: leaked: fiber ") || !strings.Contains(f.errb.String(), "t/slow passed its dispose deadline") {
		t.Fatalf("stderr:\n%s", f.errb.String())
	}
}

func TestRunShutdownThatLeaksIsNotClean(t *testing.T) {
	trigger, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(release) })
	f := newFixture(t, happyBundle+"    - {id: slow, plugin: t/slow, roles: [api]}\n", slowDispose(trigger, release))
	f.opt.Limits.DisposeDeadline = 30 * time.Millisecond
	f.opt.Limits.ShutdownBound = 300 * time.Millisecond
	done := f.start(t.Context())
	f.waitReady(t, done)
	if code := stopAndWait(t, f, done); code != ExitLeak {
		t.Fatalf("exit %d, stderr:\n%s", code, f.errb.String())
	}
	if !strings.Contains(f.errb.String(), "shutdown did not finish cleanly") {
		t.Fatalf("stderr:\n%s", f.errb.String())
	}
	// The status says why the process asks to be replaced, not the signal that began the stop.
	if s := f.lastStatus(); s.Exit != ExitLeak || !strings.Contains(s.Detail, "shutdown did not finish cleanly") || !strings.Contains(s.Detail, "leaked") {
		t.Fatalf("status %+v", s)
	}
}

// A tracked goroutine that ignores its context past the drain deadline leaks the fiber as
// a disposer past the dispose deadline does: readiness ends and the process is replaced.
func slowDrain(trigger, release chan struct{}) ModuleEntry {
	return toy(toySpec{id: "t/drain", apply: func(c *kernel.Context, _ any) error {
		c.Go("stuck", func(context.Context) error { <-release; return nil })
		c.Go("trigger", func(ctx context.Context) error {
			select {
			case <-trigger:
				return c.Dispose()
			case <-ctx.Done():
				return nil
			}
		})
		return nil
	}})
}

func TestRunDrainPastItsDeadlineReplacesTheProcess(t *testing.T) {
	trigger, release := make(chan struct{}), make(chan struct{})
	f := newFixture(t, happyBundle+"    - {id: drain, plugin: t/drain, roles: [api]}\n", slowDrain(trigger, release))
	f.opt.Limits.DrainDeadline = 30 * time.Millisecond
	f.opt.Limits.LeakGrace = 7 * time.Millisecond
	f.opt.Clock = f.clock
	done := f.start(t.Context())
	f.waitReady(t, done)
	close(trigger)
	for d := range f.clock.armed { // the grace timer is armed
		if d == 7*time.Millisecond {
			break
		}
	}
	f.clock.fire(7 * time.Millisecond)
	close(release) // the goroutine ends, so that the close can finish
	if code := <-done; code != ExitLeak {
		t.Fatalf("exit %d, stderr:\n%s", code, f.errb.String())
	}
	for _, want := range []string{"error: leaked: fiber ", "t/drain passed its drain deadline", "fiber leaked"} {
		if !strings.Contains(f.errb.String(), want) {
			t.Errorf("stderr lacks %q:\n%s", want, f.errb.String())
		}
	}
}

// Settle returns while a fiber that is blocked past its load deadline is still UNLOADING:
// that is its contract. The audit waits for the row to leave UNLOADING, so that it
// judges a row that is FAILED, within the bound of an unload.
func TestRunAuditWaitsForARowToLeaveUnloading(t *testing.T) {
	slow := toy(toySpec{id: "t/slowapply", apply: func(c *kernel.Context, _ any) error {
		<-c.Std().Done() // the kernel cancels it at the load deadline
		time.Sleep(100 * time.Millisecond)
		return c.Std().Err()
	}})
	f := newFixture(t, "- insert:\n    - {id: bad, plugin: t/slowapply, roles: [api], required: true}\n", slow)
	f.opt.Limits.LoadDeadline = 30 * time.Millisecond
	code := f.run(t.Context())
	if code != ExitRequired || !strings.Contains(f.errb.String(), `required row "bad" is FAILED`) || strings.Contains(f.errb.String(), "UNLOADING") {
		t.Fatalf("exit %d, stderr:\n%s", code, f.errb.String())
	}
}

// A row that is still UNLOADING when the bound of an unload passes is judged as it is.
func TestRunAuditJudgesARowThatStaysUnloading(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	stuck := toy(toySpec{id: "t/stuck", apply: func(c *kernel.Context, _ any) error {
		<-c.Std().Done()
		<-release
		return c.Std().Err()
	}})
	f := newFixture(t, "- insert:\n    - {id: bad, plugin: t/stuck, roles: [api], required: true}\n", stuck)
	f.opt.Limits.LoadDeadline, f.opt.Limits.DrainDeadline, f.opt.Limits.DisposeDeadline = 30*time.Millisecond, 20*time.Millisecond, 20*time.Millisecond
	f.opt.Limits.ShutdownBound = 200 * time.Millisecond
	code := f.run(t.Context())
	if code != ExitRequired || !strings.Contains(f.errb.String(), `required row "bad" is UNLOADING`) {
		t.Fatalf("exit %d, stderr:\n%s", code, f.errb.String())
	}
}

// Commit makes a composition that Compose returned the committed one: Committed, Layers
// and Required follow it (a row a layer marked required is in the set), and the one it
// replaces is remembered, so that a rollback commits it again.
func TestCommitFollowsTheAppliedGeneration(t *testing.T) {
	var l launched
	f := newFixture(t, "- insert:\n    - {id: store, plugin: t/store, roles: [api]}\n    - {id: user, plugin: t/user, roles: [api], required: true}\n    - {id: probe, plugin: t/probe, roles: [api]}\n", probe(&l))
	f.dist.RequiredKeys = []string{"store"}
	bindLaunch(f, "t/probe")
	done := f.start(t.Context())
	f.waitReady(t, done)
	l.mu.Lock()
	defer l.mu.Unlock()
	pc := l.profile
	booted := pc.Committed()
	if got := pc.Required().Rows; !slices.Equal(got, []string{"user"}) {
		t.Fatalf("required rows at boot = %v", got)
	}
	f.files["bundles/b/bundle.yaml"] = &fstest.MapFile{Data: []byte("- insert:\n    - {id: store, plugin: t/store, roles: [api]}\n    - {id: user, plugin: t/user, roles: [api], required: true}\n    - {id: probe, plugin: t/probe, roles: [api]}\n    - {id: later, plugin: t/user, roles: [api], required: true}\n")}
	next, err := pc.Compose(t.Context())
	if err != nil || next.Digest == booted.Digest {
		t.Fatalf("compose: %v", err)
	}
	// RequiredOf reads a candidate's set before it is committed.
	if got, err := pc.RequiredOf(next); err != nil || !slices.Equal(got.Rows, []string{"later", "user"}) || !slices.Equal(pc.Required().Rows, []string{"user"}) {
		t.Errorf("RequiredOf the candidate = %v, %v; Required = %v", got.Rows, err, pc.Required().Rows)
	}
	if _, err := pc.RequiredOf(launch.Composition{Digest: launch.Digest{9}}); err == nil {
		t.Error("RequiredOf a stranger: no error")
	}
	if err := pc.Commit(next); err != nil || pc.Committed().Digest != next.Digest {
		t.Fatalf("Commit = %v, committed %x", err, pc.Committed().Digest)
	}
	if got := pc.Required().Rows; !slices.Equal(got, []string{"later", "user"}) {
		t.Errorf("required rows after the commit = %v: the set is the applied generation's", got)
	}
	if err := pc.Commit(booted); err != nil || pc.Committed().Digest != booted.Digest || !slices.Equal(pc.Required().Rows, []string{"user"}) {
		t.Errorf("Commit of the previous composition = %v, required %v", err, pc.Required().Rows)
	}
	if err := pc.Commit(launch.Composition{Digest: launch.Digest{9}}); err == nil || pc.Committed().Digest != booted.Digest {
		t.Errorf("Commit of a stranger = %v: it must be refused and change nothing", err)
	}
	if code := stopAndWait(t, f, done); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
}

// A live generation is refused at Compose when it holds a row with the loader's id, as
// boot refuses one: Admit would otherwise see two rows with that id.
func TestComposeRefusesTheReservedRowWhileRunning(t *testing.T) {
	var l launched
	f := newFixture(t, "- insert:\n    - {id: probe, plugin: t/probe, roles: [api]}\n", probe(&l))
	bindLaunch(f, "t/probe")
	done := f.start(t.Context())
	f.waitReady(t, done)
	l.mu.Lock()
	pc := l.profile
	l.mu.Unlock()
	f.files["bundles/b/bundle.yaml"] = &fstest.MapFile{Data: []byte("- insert:\n    - {id: probe, plugin: t/probe, roles: [api]}\n    - {id: loader, plugin: t/probe, roles: [api]}\n")}
	if _, err := pc.Compose(t.Context()); err == nil || !strings.Contains(err.Error(), `the row id "loader" is reserved`) {
		t.Errorf("Compose = %v", err)
	}
	if code := stopAndWait(t, f, done); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
}

func TestRunSettleBound(t *testing.T) {
	hang := toy(toySpec{id: "t/hang", apply: func(c *kernel.Context, _ any) error { <-c.Std().Done(); return c.Std().Err() }})
	f := newFixture(t, "- insert:\n    - {id: hang, plugin: t/hang, roles: [api], required: true}\n", hang)
	f.opt.Limits.SettleBound = 40 * time.Millisecond
	code := f.run(t.Context())
	if code != ExitRequired || !strings.Contains(f.errb.String(), "warning: settle:") || !strings.Contains(f.errb.String(), `required row "hang" is LOADING`) {
		t.Fatalf("exit %d, stderr:\n%s", code, f.errb.String())
	}
}

// A row whose Apply overruns its load deadline is UNLOADING when Settle returns; its
// disposer then hangs past the dispose deadline and the fiber leaks, which the kernel
// reports with a deadline event and no change of state. The leak ends boot's wait for
// the unloads at once: on the launcher's clock, which never fires here, the bound of
// that wait never ends, so boot would never leave starting.
func TestRunLeakEndsTheUnloadWaitOfBoot(t *testing.T) {
	release := make(chan struct{})
	hang := toy(toySpec{id: "t/hang", apply: func(c *kernel.Context, _ any) error {
		c.Defer("hang", func(context.Context) error { <-release; return nil })
		<-c.Std().Done() // the kernel cancels it at the load deadline
		return c.Std().Err()
	}})
	f := newFixture(t, happyBundle+"    - {id: hang, plugin: t/hang, roles: [api]}\n", hang)
	f.opt.Limits.LoadDeadline, f.opt.Limits.DisposeDeadline = 30*time.Millisecond, 300*time.Millisecond
	f.opt.Limits.LeakGrace = 7 * time.Millisecond
	f.opt.Clock = f.clock
	done := f.start(t.Context())
	f.waitFor(t, done, func(s ProcessStatus) bool { return s.Phase != PhaseStarting })
	if s := f.lastStatus(); s.Ready {
		t.Errorf("ready after a leak that boot saw: %+v", s)
	}
	for d := range f.clock.armed { // the grace timer is armed
		if d == 7*time.Millisecond {
			break
		}
	}
	f.clock.fire(7 * time.Millisecond)
	close(release)
	if code := <-done; code != ExitLeak {
		t.Fatalf("exit %d, stderr:\n%s", code, f.errb.String())
	}
}

// Once a fiber leaked, readiness stays down until the process is replaced: the readiness
// row sets the latch on its next event, and the latch does not come back (ADR-059 decision 13).
func TestRunReadinessStaysDownAfterALeak(t *testing.T) {
	var l launched
	trigger, release := make(chan struct{}), make(chan struct{})
	f := newFixture(t, happyBundle+"    - {id: slow, plugin: t/slow, roles: [api]}\n    - {id: probe, plugin: t/probe, roles: [api]}\n",
		slowDispose(trigger, release), probe(&l))
	bindLaunch(f, "t/probe")
	f.opt.Limits.DisposeDeadline = 30 * time.Millisecond
	f.opt.Limits.LeakGrace = 7 * time.Millisecond
	f.opt.Clock = f.clock
	done := f.start(t.Context())
	f.waitReady(t, done)
	l.mu.Lock()
	ready := l.ready
	l.mu.Unlock()
	ready.Set()
	f.waitFor(t, done, func(s ProcessStatus) bool { return s.Ready })
	close(trigger)
	f.waitFor(t, done, func(s ProcessStatus) bool { return !s.Ready }) // the leak ends readiness
	ready.Set()
	if ready.IsReady() || f.lastStatus().Ready {
		t.Errorf("readiness came back during the leak grace: latch %v, status %+v", ready.IsReady(), f.lastStatus())
	}
	for d := range f.clock.armed {
		if d == 7*time.Millisecond {
			break
		}
	}
	f.clock.fire(7 * time.Millisecond)
	close(release)
	if code := <-done; code != ExitLeak {
		t.Fatalf("exit %d, stderr:\n%s", code, f.errb.String())
	}
}

// The latch publishes the value it holds: a Drop that runs while a Set is still publishing
// does not leave the outside reading ready.
func TestLatchPublishesTheValueItHolds(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	var last bool
	l := &latch{}
	l.changed = func(v bool) {
		if v {
			close(entered)
			<-release
		}
		mu.Lock()
		last = v
		mu.Unlock()
	}
	setDone, dropDone := make(chan struct{}), make(chan struct{})
	go func() { l.Set(); close(setDone) }()
	<-entered
	go func() { l.Drop(); close(dropDone) }()
	// The Drop either publishes now, which is the fault, or waits for the Set to finish
	// publishing; the bound only lets the fault show.
	select {
	case <-dropDone:
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	<-setDone
	<-dropDone
	mu.Lock()
	defer mu.Unlock()
	if last != l.IsReady() {
		t.Fatalf("published ready=%v, the latch holds %v", last, l.IsReady())
	}
}

// The committed composition is what the tree runs: a row the gate detached at boot is
// disabled in it, so a rollback to it does not mount the row.
func TestCommittedHoldsTheAdmittedRows(t *testing.T) {
	var l launched
	f := newFixture(t, happyBundle+"    - {id: probe, plugin: t/probe, roles: [api]}\n    - {id: ghost, plugin: t/ghost, roles: [api]}\n", probe(&l))
	bindLaunch(f, "t/probe")
	done := f.start(t.Context())
	f.waitReady(t, done)
	l.mu.Lock()
	pc := l.profile
	l.mu.Unlock()
	for _, r := range pc.Committed().Rows {
		if r.ID == "ghost" && r.Enabled {
			t.Errorf("the committed composition enables the detached row: %+v", r)
		}
	}
	if code := stopAndWait(t, f, done); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
}

// Admit judges the provenance of the composition Compose just returned, not that of an older
// one with the same digest (the digest leaves provenance out, gate check 8 reads it).
func TestAdmitJudgesTheNewestProvenance(t *testing.T) {
	const moved = "- insert:\n    - {id: store, plugin: t/store, roles: [api]}\n    - {id: user, plugin: t/user, roles: [api]}\n    - {id: probe, plugin: t/probe, roles: [api]}\n"
	const inBundle = "- insert:\n    - {id: store, plugin: t/store, roles: [api]}\n    - {id: user, plugin: t/user, roles: [api], intercept: {policy: p}}\n    - {id: probe, plugin: t/probe, roles: [api]}\n"
	const site = "- {id: user, intercept: {policy: p}}\n"
	for _, tc := range []struct {
		name               string
		bootBundle, bundle string
		bootSite, nextSite bool
		denied             bool
	}{
		{name: "into a patch layer", bootBundle: inBundle, bundle: moved, nextSite: true, denied: true},
		{name: "back into the bundle", bootBundle: moved, bootSite: true, bundle: inBundle},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var l launched
			f := newFixture(t, tc.bootBundle, probe(&l))
			bindLaunch(f, "t/probe")
			f.dist.Providers = []Binding{{Key: "policy", Plugins: []string{"t/store"}}}
			if tc.bootSite {
				f.files["aiops.patch.yml"] = &fstest.MapFile{Data: []byte(site)}
			}
			done := f.start(t.Context())
			f.waitReady(t, done)
			l.mu.Lock()
			pc := l.profile
			l.mu.Unlock()
			booted := pc.Committed()
			f.files["bundles/b/bundle.yaml"] = &fstest.MapFile{Data: []byte(tc.bundle)}
			delete(f.files, "aiops.patch.yml")
			if tc.nextSite {
				f.files["aiops.patch.yml"] = &fstest.MapFile{Data: []byte(site)}
			}
			next, err := pc.Compose(t.Context())
			if err != nil || next.Digest != booted.Digest {
				t.Fatalf("compose: %v; the move must keep the digest", err)
			}
			adm, err := pc.Admit(t.Context(), next)
			if err != nil || (len(adm.Denied) == 1 && adm.Denied[0].Row == "user") != tc.denied || !tc.denied && len(adm.Denied) != 0 {
				t.Fatalf("admit: %v %+v, want denied=%v", err, adm, tc.denied)
			}
			if !tc.denied {
				// The plan from what the tree runs (user detached) enables the row.
				pl, err := pc.Plan(t.Context(), booted, next)
				if err != nil {
					t.Fatal(err)
				}
				var act launch.Action
				for _, e := range pl.Entries {
					if e.ID == "user" {
						act = e.Action
					}
				}
				if act != launch.ActionUpdate {
					t.Errorf("plan of user: %q, want update (enabled); plan %+v", act, pl)
				}
			}
			if code := stopAndWait(t, f, done); code != ExitOK {
				t.Fatalf("exit %d", code)
			}
		})
	}
}

// A denied required row rejects the generation, and the admission names it as it names a
// denied optional row, so that reload reports the cause as ErrDenied (ADR-059 decision 15).
func TestAdmitListsADeniedRequiredRow(t *testing.T) {
	var l launched
	f := newFixture(t, happyBundle+"    - {id: probe, plugin: t/probe, roles: [api]}\n", probe(&l))
	bindLaunch(f, "t/probe")
	done := f.start(t.Context())
	f.waitReady(t, done)
	l.mu.Lock()
	pc := l.profile
	l.mu.Unlock()
	f.files["bundles/b/bundle.yaml"] = &fstest.MapFile{Data: []byte(happyBundle + "    - {id: probe, plugin: t/probe, roles: [api]}\n    - {id: ghost, plugin: t/ghost, roles: [api], required: true}\n")}
	next, err := pc.Compose(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	adm, err := pc.Admit(t.Context(), next)
	if err == nil || len(adm.Denied) != 1 || adm.Denied[0].Row != "ghost" || adm.Denied[0].Check != "module table" {
		t.Fatalf("admit: %v %+v", err, adm)
	}
	if code := stopAndWait(t, f, done); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
}

// A stop request during boot ends the boot waits: the process stops cleanly at once, runs
// no audit and never says it booted.
func TestRunStopDuringBoot(t *testing.T) {
	for _, tc := range []struct {
		name   string
		signal bool
	}{{"signal", true}, {"context", false}} {
		t.Run(tc.name, func(t *testing.T) {
			entered := make(chan struct{})
			slow := toy(toySpec{id: "t/slowboot", apply: func(c *kernel.Context, _ any) error {
				close(entered)
				<-c.Std().Done()
				return c.Std().Err()
			}})
			f := newFixture(t, happyBundle+"    - {id: slow, plugin: t/slowboot, roles: [api]}\n", slow)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := f.start(ctx)
			<-entered
			if tc.signal {
				f.signal <- syscall.SIGTERM
			} else {
				cancel()
			}
			if code := <-done; code != ExitOK {
				t.Fatalf("exit %d, stderr:\n%s", code, f.errb.String())
			}
			if strings.Contains(f.out.String(), "booted") || slices.Contains(f.phases(), PhaseRunning) || strings.Contains(f.errb.String(), "error:") {
				t.Fatalf("the boot went on after the stop request: stdout %q phases %v stderr:\n%s", f.out.String(), f.phases(), f.errb.String())
			}
		})
	}
}

// Admit decodes every config that the generation changes: a created, enabled or changed
// row whose config its plugin's Decode refuses rejects the generation with nothing
// applied (03-c3-kernel.md:377), and a row the generation does not change is not judged
// again.
func TestAdmitDecodesTheChangedConfigs(t *testing.T) {
	var l launched
	strict := kernel.Define(kernel.Plugin{ID: "t/strict",
		Decode: func(raw any) (any, error) {
			if m, _ := raw.(map[string]any); m["bad"] == true {
				return nil, errors.New("bad is not allowed")
			}
			if m, _ := raw.(map[string]any); m["panic"] == true {
				panic("boom")
			}
			return raw, nil
		},
		Apply: func(*kernel.Context, any) error { return nil }})
	lax := kernel.Define(kernel.Plugin{ID: "t/lax", Decode: func(raw any) (any, error) { return raw, nil },
		Apply: func(*kernel.Context, any) error { return nil }})
	const base = "- insert:\n    - {id: probe, plugin: t/probe, roles: [api]}\n    - {id: kept, plugin: t/strict, roles: [api], config: {bad: true}}\n    - {id: off, plugin: t/strict, roles: [api], enabled: false, config: {bad: false}}\n"
	f := newFixture(t, base+"    - {id: m, plugin: t/lax, roles: [api], config: {bad: true}}\n", probe(&l),
		ModuleEntry{ID: "t/strict", Module: "example.test/strict", Version: "v0.1.0", Plugin: strict},
		ModuleEntry{ID: "t/lax", Module: "example.test/lax", Version: "v0.1.0", Plugin: lax})
	bindLaunch(f, "t/probe")
	done := f.start(t.Context())
	f.waitReady(t, done)
	l.mu.Lock()
	pc := l.profile
	l.mu.Unlock()
	admit := func(bundle string) error {
		t.Helper()
		f.files["bundles/b/bundle.yaml"] = &fstest.MapFile{Data: []byte(bundle)}
		c, err := pc.Compose(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		_, err = pc.Admit(t.Context(), c)
		return err
	}
	for _, tc := range []struct {
		name, add, want string
	}{
		{"unchanged", "", ""},
		{"created", "    - {id: new, plugin: t/strict, roles: [api], config: {bad: true}}\n", `row "new": config: bad is not allowed`},
		{"panics", "    - {id: new, plugin: t/strict, roles: [api], config: {panic: true}}\n", `row "new": config: Decode panicked: boom`},
		{"created, good", "    - {id: new, plugin: t/strict, roles: [api], config: {bad: false}}\n", ""},
		{"enabled", "- {id: off, enabled: true, config: {bad: true}}\n", `row "off": config: bad is not allowed`},
		{"changed", "- {id: kept, config: {bad: true, n: 1}}\n", `row "kept": config: bad is not allowed`},
		// The row runs this config, and the Decode of its new plugin refuses it.
		{"plugin changed", "    - {id: m, plugin: t/strict, roles: [api], config: {bad: true}}\n", `row "m": config: bad is not allowed`},
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

// boot's own observer can be detached for overflow like any other. It subscribes again and
// reads the tree, so a leak after the detachment still ends readiness and replaces the
// process.
func TestRunActsOnALeakAfterItsObserverOverflowed(t *testing.T) {
	trigger, release := make(chan struct{}), make(chan struct{})
	f := newFixture(t, happyBundle+"    - {id: slow, plugin: t/slow, roles: [api]}\n", slowDispose(trigger, release))
	const queue = 64
	f.opt.Limits.ObserverQueue = queue
	f.opt.Limits.DisposeDeadline = 30 * time.Millisecond
	f.opt.Limits.LeakGrace = 7 * time.Millisecond
	f.opt.Clock = f.clock
	f.opt.Registry, f.opt.Dist = f.reg, f.dist
	r := newRun(f.opt)
	// boot's observer stalls on the first event after the trigger while the test appends
	// more events than the queue holds, so that the kernel detaches it; an observer of the
	// test, ahead of it, sees a later event once it has been, and the deadline event that
	// boot's observer has lost.
	var armed atomic.Bool
	var blocked atomic.Uint64
	stalled, unstall, detached, deadline := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once, onceDeadline sync.Once
	r.onEvent = func(e kernel.LedgerEvent) {
		if armed.CompareAndSwap(true, false) {
			blocked.Store(e.Sequence)
			close(stalled)
			<-unstall
		}
	}
	roots := make(chan *kernel.Root, 1)
	r.o.Kernel = func(root *kernel.Root) {
		roots <- root
		_, err := root.Observe("test", func(_ context.Context, e kernel.LedgerEvent) error {
			if s := blocked.Load(); s > 0 && e.Sequence > s {
				once.Do(func() { close(detached) })
				if e.Kind == "deadline" {
					onceDeadline.Do(func() { close(deadline) })
				}
			}
			return nil
		})
		if err != nil {
			t.Error(err)
		}
	}
	done := make(chan int, 1)
	go func() { done <- r.main(t.Context()) }()
	root := <-roots
	f.waitReady(t, done)
	armed.Store(true)
	close(trigger)
	<-stalled
	for range queue + 1 {
		if err := root.Notify(t.Context(), storeKey); err != nil {
			t.Fatal(err)
		}
	}
	bound := time.After(10 * time.Second)
	select {
	case <-detached:
	case <-bound:
		t.Fatalf("precondition: boot's observer was not detached (stalled at %d)", blocked.Load())
	}
	select {
	case <-deadline:
	case <-bound:
		t.Fatal("precondition: the fiber did not leak")
	}
	close(unstall)
	for armed := false; !armed; {
		select {
		case d := <-f.clock.armed:
			armed = d == 7*time.Millisecond
		case <-bound:
			close(release)
			t.Fatalf("the leak was not acted on; exit %d, stderr:\n%s", stopAndWait(t, f, done), f.errb.String())
		}
	}
	// Only the resync found the leak, so boot-watch has been made again by now: neither
	// observer left an effect on the root (ADR-059 decision 21).
	events, err := root.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.Fiber == 0 && e.Kind == "effect" && e.Label == "observer boot-watch" {
			t.Errorf("boot-watch left an effect on the root: %+v", e)
		}
	}
	f.clock.fire(7 * time.Millisecond)
	close(release)
	if code := <-done; code != ExitLeak {
		t.Fatalf("exit %d, stderr:\n%s", code, f.errb.String())
	}
}
