// SPDX-License-Identifier: Apache-2.0

package dist

import (
	"slices"
	"testing"

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
	if h := reg.Modules["aiops/readiness"].Hooks; len(h) != 2 || h[0].Hook != "internal/status" || h[0].StageName != "default" {
		t.Fatalf("hooks %+v", h)
	}
}
