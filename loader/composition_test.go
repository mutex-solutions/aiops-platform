// SPDX-License-Identifier: Apache-2.0

package loader

import (
	"context"
	"slices"
	"testing"

	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

func TestCompositionServesTheMountedRows(t *testing.T) {
	gate := kernel.NewKey[int]("gate", 0)
	p := newProbe()
	hooked := kernel.Define(kernel.Plugin{ID: "t/hooked",
		Hooks: []kernel.HookUse{def.EntryInit.Use(kernel.DefaultStage), def.PatchContext.Use(kernel.DefaultStage)},
		Apply: func(c *kernel.Context, _ any) error {
			return c.Effect("registered", func() (kernel.Disposer, error) { return func(context.Context) error { return nil }, nil })
		}})
	g := group("g")
	g.Inject = []string{"gate"}
	off, other, bad := row("off", "t/a"), row("w", "t/a"), row("bad", "t/missing")
	off.Enabled, other.Roles = false, []string{"worker"}
	x := newFx(t, fxOpt{plugins: plugins(hooked, p.plugin("t/a", nil)), keys: map[string]kernel.AnyKey{"gate": gate},
		rows: []def.EntrySpec{row("r", "t/hooked"), g, child("g", "k", "t/a"), off, other, bad}})

	gen, ok := x.rd.Committed(composition.ScopeProcess)
	if !ok || gen.Scope != composition.ScopeProcess {
		t.Fatalf("Committed = %+v, %v", gen, ok)
	}
	var ids []string
	state := map[string]kernel.State{}
	for _, r := range gen.Rows {
		ids = append(ids, r.ID)
		state[r.ID] = r.State
	}
	if !slices.Equal(ids, []string{"r", "g", "g:k", "bad"}) {
		t.Fatalf("rows %v: a row that is not mounted must not be listed", ids)
	}
	if state["r"] != kernel.Active || state["g"] != kernel.Pending || state["g:k"] != kernel.Pending || state["bad"] != kernel.Failed {
		t.Errorf("states %v", state)
	}
	r := gen.Rows[0]
	if r.Plugin != "t/hooked" || !slices.Equal(r.Hooks, []string{"loader/entry-init", "loader/patch-context"}) || !slices.Contains(r.Registrations, "registered") {
		t.Errorf("row r: %+v", r)
	}
	if k := gen.Rows[2]; k.Parent != "g" || len(k.Hooks) != 0 {
		t.Errorf("row g:k: %+v", k)
	}
	if got := x.rd.Declaring(composition.ScopeProcess, "loader/entry-init"); len(got) != 1 || got[0].ID != "r" {
		t.Errorf("Declaring = %v", got)
	}
	if got := x.rd.Declaring(composition.ScopeProcess, "nope"); len(got) != 0 {
		t.Errorf("Declaring(nope) = %v", got)
	}
	// What the reader returns is a copy, and the tenant scope has no generation at S1.
	gen.Rows[0].ID = "changed"
	if again, _ := x.rd.Committed(composition.ScopeProcess); again.Rows[0].ID != "r" {
		t.Error("the reader hands out its own rows")
	}
	if _, ok := x.rd.Committed(composition.ScopeTenant); ok {
		t.Error("tenant scope has a generation")
	}
	if got := x.rd.Declaring(composition.ScopeTenant, "loader/entry-init"); len(got) != 0 {
		t.Errorf("Declaring on the tenant scope = %v", got)
	}
}
