// SPDX-License-Identifier: Apache-2.0

package dist

import (
	"slices"
	"testing"
)

func TestLoadHoldsTheLoaderAndItsFiles(t *testing.T) {
	reg, man, lim, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	e, ok := reg.Modules["aiops/loader"]
	if !ok || !slices.Contains(e.Provides, "loader") || !slices.Contains(e.Provides, "composition") {
		t.Fatalf("loader entry %+v", e)
	}
	if len(reg.Modules) != 1 || reg.LoaderConfig == nil || len(reg.Hooks) != 3 {
		t.Fatalf("registry %+v", reg)
	}
	if !slices.Equal(man.RequiredRows, []string{"loader"}) || lim.LoadDeadline == 0 || lim.ShutdownBound == 0 {
		t.Fatalf("manifest %+v limits %+v", man, lim)
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
}
