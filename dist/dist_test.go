// SPDX-License-Identifier: Apache-2.0

package dist

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/fstest"

	"github.com/mutex-solutions/aiops-platform/boot"
	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	compfake "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0/fake"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	launchfake "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0/fake"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	reloadfake "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
	"github.com/mutex-solutions/aiops-platform/test/trusted"
)

// bindings returns the bindings and the required set of the signed manifest.
func bindings(t *testing.T, signed []byte) boot.DistManifest {
	t.Helper()
	d, err := boot.ReadDistribution("distribution.yaml", signed)
	if err != nil {
		t.Fatal(err)
	}
	return d.Bindings()
}

func TestLoadHoldsTheCoreRowsAndTheirBindings(t *testing.T) {
	reg, signed, lim, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	man := bindings(t, signed)
	for _, id := range []string{"aiops/loader", "aiops/clock", "aiops/config-refs", "aiops/readiness", "aiops/reload", "aiops/restart-policy",
		"aiops/cli", "aiops/command-profile", "aiops/command-plugin", "aiops/command-trust", "aiops/command-dist", "aiops/command-key"} {
		if _, ok := reg.Modules[id]; !ok {
			t.Errorf("%s is missing from the module table", id)
		}
	}
	if len(reg.Modules) != 12 || reg.LoaderConfig == nil || len(reg.Hooks) != 3 {
		t.Fatalf("%d modules, %d hooks", len(reg.Modules), len(reg.Hooks))
	}
	if !slices.Equal(man.RequiredRows, []string{"loader", "readiness", "reload", "restart-policy"}) || !slices.Equal(man.RequiredKeys, []string{"clock"}) {
		t.Fatalf("required set %+v", man)
	}
	if lim.LoadDeadline == 0 || lim.ShutdownBound == 0 {
		t.Fatalf("limits %+v", lim)
	}
	listeners := map[string][]string{}
	for _, h := range man.SealedHooks {
		listeners[h.Hook] = h.Plugins
	}
	if !slices.Equal(listeners["internal/config"], []string{"aiops/config-refs"}) || !slices.Contains(listeners["internal/status"], "aiops/readiness") {
		t.Fatalf("sealed hooks %v", listeners)
	}
	// appExit is consumer-bound, and the cli row requests the exit of aiopsctl with the
	// code of its command (ADR-061 decision 7).
	if i := slices.IndexFunc(man.Consumers, func(b boot.Binding) bool { return b.Key == "appExit" }); i < 0 || !slices.Contains(man.Consumers[i].Plugins, "aiops/cli") {
		t.Fatalf("consumers %+v", man.Consumers)
	}
	// command-profile reaches the toolbox through profileContext until the operations
	// library carries Render, InitProfile and SanitizeProfile (ADR-061 decision 20).
	if i := slices.IndexFunc(man.Consumers, func(b boot.Binding) bool { return b.Key == "profileContext" }); i < 0 ||
		!slices.Contains(man.Consumers[i].Plugins, "aiops/command-profile") {
		t.Fatalf("consumers %+v", man.Consumers)
	}
}

// Gate check 7 on the release manifest detaches an optional row that the kernel would
// refuse with ErrBinding or ErrSealed, instead of letting it fail and be given up on:
// reload has no consumer yet, and the loader's hooks are sealed by their definition.
func TestGateDetachesARowOutsideTheReleaseBindings(t *testing.T) {
	reg, signed, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	man := bindings(t, signed)
	reg.Modules["x/needreload"] = boot.ModuleEntry{ID: "x/needreload", Needs: []string{"reload"}}
	reg.Modules["x/entryinit"] = boot.ModuleEntry{ID: "x/entryinit", Hooks: []boot.HookDecl{{Hook: "loader/entry-init"}}}
	c := boot.Composition{Rows: []boot.ComposedRow{
		{Row: boot.Row{ID: "needreload", Plugin: "x/needreload", Enabled: true}},
		{Row: boot.Row{ID: "entryinit", Plugin: "x/entryinit", Enabled: true}},
	}}
	g := boot.GateWith(c, boot.GateInput{Entry: boot.EntryBoot, Mods: reg.Modules, Bindings: man})
	if !slices.Equal(g.Detached, []string{"entryinit", "needreload"}) || g.Refused {
		t.Fatalf("detached %v, refused %v: %+v", g.Detached, g.Refused, g.Slots[boot.CheckKeyHookClass-1].Fails)
	}
}

// releaseBundle is a profile of the release distribution: the five core rows, with
// restart-policy, a required row of the release manifest, holding a reference.
const releaseBundle = `- insert:
    - {id: clock, plugin: aiops/clock, roles: [api]}
    - {id: refs, plugin: aiops/config-refs, roles: [api], config: {maxFileBytes: 65536}%s}
    - {id: readiness, plugin: aiops/readiness, roles: [api]}
    - {id: restart-policy, plugin: aiops/restart-policy, roles: [api], config: {maxFailures: 3, window: "%s", backoffMin: 20ms, backoffMax: 100ms, factor: 2, infrastructure: [clock]}}
    - {id: reload, plugin: aiops/reload, roles: [api], config: {queue: 4, history: 16}}
`

func releaseHome(refs, window string) fstest.MapFS {
	return fstest.MapFS{
		"profiles/p/profile.yaml": {Data: []byte("bundles: [b]")},
		"bundles/b/bundle.yaml":   {Data: fmt.Appendf(nil, releaseBundle, refs, window)},
	}
}

func admitRelease(t *testing.T, home fstest.MapFS) boot.Admission {
	t.Helper()
	reg, signed, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	man := bindings(t, signed)
	c, _, err := boot.Compose(boot.Sources{Profile: "p", Home: home, Bundles: home}, boot.Options{Server: true, Modules: reg.Modules})
	if err != nil {
		t.Fatal(err)
	}
	return boot.AdmitWith(boot.WithLoader(c), boot.GateInput{Entry: boot.EntryBoot, Mods: reg.Modules, Bindings: man}, nil)
}

// A required row of the release set that holds a reference pulls config-refs into the
// required set (ADR-059 decision 18), so the profile is admitted, and it boots: the
// required audit and the readiness latch see config-refs ACTIVE.
func TestReleaseProfileWithAReferenceInARequiredRowBoots(t *testing.T) {
	a := admitRelease(t, releaseHome("", "${env:AIOPS_REF_WINDOW}"))
	if a.Refused() {
		t.Fatalf("refused:\n%s", strings.Join(a.Diagnostics(), "\n"))
	}
	if !slices.Contains(a.Required.Keys, "configRefsReady") || !slices.Contains(a.Required.Rows, "refs") {
		t.Fatalf("required set %+v", a.Required)
	}
	reg, signed, lim, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	home := releaseHome("", "${env:AIOPS_REF_WINDOW}")
	files, err := trusted.Files(signed)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		home[name] = &fstest.MapFile{Data: data}
	}
	bi, _ := debug.ReadBuildInfo()
	sig, ready := make(chan os.Signal, 1), make(chan struct{})
	var once sync.Once
	var stderr strings.Builder
	var mu sync.Mutex
	done := make(chan int, 1)
	go func() {
		done <- boot.Run(t.Context(), boot.RunOptions{Profile: "p", Home: home, Bundles: home, Signals: sig,
			Env: func(n string) string {
				if n == "AIOPS_REF_WINDOW" {
					return "1m"
				}
				return ""
			},
			Stdout: io.Discard, Stderr: writerFunc(func(p []byte) (int, error) { mu.Lock(); defer mu.Unlock(); return stderr.Write(p) }),
			Registry: reg, Signed: signed, BuildInfo: bi, Limits: lim, Clock: boot.SystemClock(),
			Publish: func(s boot.ProcessStatus) {
				if s.Ready {
					once.Do(func() { close(ready) })
				}
			}})
	}()
	select {
	case <-ready:
	case code := <-done:
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("exit %d before ready; stderr:\n%s", code, stderr.String())
	}
	sig <- syscall.SIGTERM
	if code := <-done; code != boot.ExitOK {
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("exit %d; stderr:\n%s", code, stderr.String())
	}
}

// The test distribution runs the release manifest plus the bindings of its own plugins:
// every binding, listener list and required key or row of the release is in it as it
// is, so no suite runs with a release entry changed or dropped (ADR-059 decision 18).
func TestTestManifestHoldsTheReleaseManifest(t *testing.T) {
	_, signed, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	rel := bindings(t, signed)
	data, err := os.ReadFile("../test/dist/dist.yaml")
	if err != nil {
		t.Fatal(err)
	}
	tst, err := boot.ParseDist("dist.yaml", data)
	if err != nil {
		t.Fatal(err)
	}
	listeners := func(hs []boot.HookListeners) []boot.Binding {
		var out []boot.Binding
		for _, h := range hs {
			out = append(out, boot.Binding{Key: h.Hook, Plugins: h.Plugins})
		}
		return out
	}
	for _, side := range []struct {
		name     string
		rel, tst []boot.Binding
	}{{"providers", rel.Providers, tst.Providers}, {"consumers", rel.Consumers, tst.Consumers},
		{"sealedHooks", listeners(rel.SealedHooks), listeners(tst.SealedHooks)}} {
		for _, b := range side.rel {
			i := slices.IndexFunc(side.tst, func(x boot.Binding) bool { return x.Key == b.Key })
			if i < 0 || !includes(side.tst[i].Plugins, b.Plugins) {
				t.Errorf("%s %s: release %v, test %v", side.name, b.Key, b.Plugins, side.tst)
			}
		}
	}
	if !includes(tst.RequiredKeys, rel.RequiredKeys) || !slices.Equal(tst.RequiredRows, rel.RequiredRows) {
		t.Errorf("required set: release %v %v, test %v %v", rel.RequiredKeys, rel.RequiredRows, tst.RequiredKeys, tst.RequiredRows)
	}
	if slices.Contains(tst.RequiredKeys, "configRefsReady") {
		t.Error("the test manifest requires configRefsReady, which hides what the release requires")
	}
}

func includes(set, sub []string) bool {
	return !slices.ContainsFunc(sub, func(s string) bool { return !slices.Contains(set, s) })
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// Disabling the one config-refs row is an optional choice while no row required by row
// or by key holds a reference: the rows that hold one stay PENDING. Its keys are the
// launcher's to require (ADR-059 decision 18), so the last-provider rule of gate check 8
// does not refuse the profile; the required set does where such a row holds one (the
// next test). A row required by key counts as a required row (ADR-059 decision 24).
func TestReleaseProfileWithConfigRefsDisabledIsAdmitted(t *testing.T) {
	if a := admitRelease(t, releaseHome(", enabled: false", "1m")); a.Refused() {
		t.Fatalf("refused:\n%s", strings.Join(a.Diagnostics(), "\n"))
	}
}

// The clock row provides clock, a required key of the release set, so it is inside the
// required set as a required row is: when it holds a reference and config-refs is
// disabled, the static admission refuses the profile, where boot would stop later on a
// clock left PENDING (ADR-059 decision 24).
func TestReleaseProfileWithAReferenceInAProviderOfARequiredKeyNeedsConfigRefs(t *testing.T) {
	home := releaseHome(", enabled: false", "1m")
	f := home["bundles/b/bundle.yaml"]
	f.Data = []byte(strings.Replace(string(f.Data), "plugin: aiops/clock, roles: [api]", `plugin: aiops/clock, roles: [api], config: {x: "${env:AIOPS_REF_X}"}`, 1))
	a := admitRelease(t, home)
	if !a.Refused() || !slices.Contains(a.Required.Keys, "configRefsReady") || !slices.Contains(a.Required.Rows, "refs") {
		t.Fatalf("refused %v, required set %+v:\n%s", a.Refused(), a.Required, strings.Join(a.Diagnostics(), "\n"))
	}
}

// An entry of the module table is boot's lenient reading of the plugin's manifest,
// the gate's (ADR-048 decisions 15 and 18): what it provides, uses and listens on, and
// the volatile paths of its configSchema, so no list of it is written by hand (ADR-060
// decision 4). The loader is written with kernel.Define, so its schema is {} and it
// has no volatile path.
func TestEntryReadsTheManifest(t *testing.T) {
	m := Modules()[0]
	e, err := m.Entry()
	if err != nil {
		t.Fatal(err)
	}
	if e.Module != "" || e.ID != "aiops/loader" || e.Volatile != nil || e.Plugin == nil {
		t.Fatalf("entry %+v", e)
	}
	if !slices.Equal(e.Needs, []string{"catalog", "profileContext"}) || !slices.Equal(e.Provides, []string{"composition", "loader"}) {
		t.Fatalf("needs %v, provides %v", e.Needs, e.Provides)
	}
	reg, _, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if h := reg.Modules["aiops/readiness"].Hooks; len(h) != 3 || h[1].Hook != "internal/status" || h[1].StageName != "default" {
		t.Fatalf("hooks %+v", h)
	}
	// A manifest that the reader refuses fails the entry and the table, naming the plugin.
	m.Manifest = []byte("id: aiops/loader\n")
	if _, err := m.Entry(); err == nil || !strings.Contains(err.Error(), "aiops/loader") {
		t.Fatalf("Entry of a broken manifest: %v", err)
	}
	if _, err := Table(m); err == nil {
		t.Fatal("Table took a broken manifest")
	}
}

// The readiness row of the module table keeps the latch dropped across a restart of the
// row once reload said that the state of the tree is unknown (ADR-059 decision 23): its
// activations share that knowledge.
func TestReadinessRowKeepsAnUnknownStateAcrossARestart(t *testing.T) {
	var p kernel.Plugin
	for _, m := range Modules() {
		if m.Plugin.ID == "aiops/readiness" {
			p = m.Plugin
		}
	}
	root := kerneltest.New(t)
	l, lp := launchfake.New()
	tree, cp := compfake.New()
	r, rp := reloadfake.New()
	l.Needed = launch.RequiredSet{Rows: []string{"db"}}
	kerneltest.Mount(root, lp, cp, rp)
	tree.Commit(composition.Generation{Scope: composition.ScopeProcess, Rows: []composition.Row{{ID: "db", State: kernel.Active}}})
	f := root.Plugin(p, nil)
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	applied := func(g uint64) bool {
		t.Helper()
		if err := r.Publish(t.Context(), composition.Applied{Generation: g, Report: composition.Report{Pass: true}}); err != nil {
			t.Fatal(err)
		}
		return l.IsReady()
	}
	if !applied(1) {
		t.Fatal("not ready with the required set ACTIVE")
	}
	if err := r.Progress(t.Context(), reload.Result{ID: 1, Status: reload.RollbackFailed, Err: reload.ErrStateUnknown}); err != nil {
		t.Fatal(err)
	}
	if err := f.Update(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	if applied(2) {
		t.Fatal("ready after a restart of the row, with the state of the tree unknown")
	}
}
