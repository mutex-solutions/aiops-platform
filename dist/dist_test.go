// SPDX-License-Identifier: Apache-2.0

package dist

import (
	"slices"
	"testing"
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
