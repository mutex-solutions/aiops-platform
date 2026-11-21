// SPDX-License-Identifier: Apache-2.0

package dist

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/fstest"

	"github.com/mutex-solutions/aiops-platform/boot"
)

func TestLoadHoldsTheCoreRowsAndTheirBindings(t *testing.T) {
	reg, man, lim, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"aiops/loader", "aiops/clock", "aiops/config-refs", "aiops/readiness", "aiops/reload", "aiops/restart-policy"} {
		if _, ok := reg.Modules[id]; !ok {
			t.Errorf("%s is missing from the module table", id)
		}
	}
	if len(reg.Modules) != 6 || reg.LoaderConfig == nil || len(reg.Hooks) != 3 {
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
}

// Gate check 7 on the release manifest detaches an optional row that the kernel would
// refuse with ErrBinding or ErrSealed, instead of letting it fail and be given up on:
// reload has no consumer yet, and the loader's hooks are sealed by their definition.
func TestGateDetachesARowOutsideTheReleaseBindings(t *testing.T) {
	reg, man, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	reg.Modules["x/needreload"] = boot.ModuleEntry{ID: "x/needreload", Needs: []string{"reload"}}
	reg.Modules["x/entryinit"] = boot.ModuleEntry{ID: "x/entryinit", Hooks: []boot.HookDecl{{Hook: "loader/entry-init"}}}
	c := boot.Composition{Rows: []boot.ComposedRow{
		{Row: boot.Row{ID: "needreload", Plugin: "x/needreload", Enabled: true}},
		{Row: boot.Row{ID: "entryinit", Plugin: "x/entryinit", Enabled: true}},
	}}
	g := boot.Gate(c, reg.Modules, man)
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
	reg, man, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := boot.Compose(boot.Sources{Profile: "p", Home: home, Bundles: home}, boot.Options{Server: true, Modules: reg.Modules})
	if err != nil {
		t.Fatal(err)
	}
	return boot.Admit(boot.WithLoader(c), reg.Modules, man, nil)
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
	reg, man, lim, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	home := releaseHome("", "${env:AIOPS_REF_WINDOW}")
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
			Registry: reg, Dist: man, Limits: lim, Clock: boot.SystemClock(),
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
	_, rel, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
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

// Disabling the one config-refs row is an optional choice: the rows that hold a
// reference stay PENDING. Its keys are the launcher's to require (ADR-059 decision
// 18), so the last-provider rule of gate check 8 does not refuse the profile.
func TestReleaseProfileWithConfigRefsDisabledIsAdmitted(t *testing.T) {
	if a := admitRelease(t, releaseHome(", enabled: false", "1m")); a.Refused() {
		t.Fatalf("refused:\n%s", strings.Join(a.Diagnostics(), "\n"))
	}
}

func TestEntryReadsTheDeclarationsOfThePlugin(t *testing.T) {
	m := Modules()[0]
	m.Volatile = []string{"config.x"}
	e := m.Entry()
	if e.Module != ModulePath || e.Version != Version || e.ID != "aiops/loader" || !slices.Equal(e.Volatile, []string{"config.x"}) {
		t.Fatalf("entry %+v", e)
	}
	if !slices.Contains(e.Needs, "profileContext") || !slices.Contains(e.Needs, "catalog") {
		t.Fatalf("needs %v", e.Needs)
	}
	reg, _, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if h := reg.Modules["aiops/readiness"].Hooks; len(h) != 3 || h[0].Hook != "internal/status" || h[0].StageName != "default" {
		t.Fatalf("hooks %+v", h)
	}
}
