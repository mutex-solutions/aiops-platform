// SPDX-License-Identifier: Apache-2.0

// Package conformance is the suite of the composition definition: what every
// provider of composition.Key and the fake must do (07-c4-plugin-sdk.md §8.2, T2
// "every provider and the fake behave the same"). The obligations half holds a
// provider to the methods of composition.Reader over a committed generation that the
// suite sets up. The guard half holds it to its release and its method set: after
// the provider's release the consumer it served is released, and a read of the key
// fails with kernel.ErrInactive; the provided value has no method outside the
// interface. composition.Reader has no error result, so a call on a Reader kept from
// before the release cannot return kernel.ErrInactive as 07-c4-plugin-sdk.md:518
// asks, and G1 proves only the read of the key; the rule waits for the definition's
// next minor. Each obligation has an id, the first word of its case's name. The
// package imports the standard library, sdk/kernel and definitions only.
package conformance

import (
	"errors"
	"slices"
	"testing"

	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Setup is the generation a provider is to hold as the committed one of the process
// scope: Rows in order, parents before children, each row mounted from
// Plugins[row.Plugin], or a group when its plugin is "core:group". A row carries ID,
// Plugin and Parent only.
type Setup struct {
	Rows    []composition.Row
	Plugins map[string]kernel.Plugin
}

// Mount mounts a provider of composition.Key that holds s on a root of its own, and
// returns the root and the provider's fiber once the provider and every row are
// ACTIVE. The suite mounts its consumers on the root and releases the provider by
// disposing the fiber.
type Mount func(t *testing.T, s Setup) (*kernel.Root, *kernel.Fiber)

// The hooks the rows of the setup declare, owned by a key no row provides. No row
// listens on them: a declaration is what Declaring reads.
var (
	owner  = kernel.NewKey[int]("conformanceHooks", 0)
	first  = kernel.NewEvent[int]("conformance/first", kernel.OwnedBy(owner))
	second = kernel.NewEvent[int]("conformance/second", kernel.OwnedBy(owner))
)

// setup is the generation of every case: a row, a group and a child of the group.
func setup() Setup {
	p := plugin("conformance/p", first)
	q := plugin("conformance/q", first, second)
	return Setup{
		Rows: []composition.Row{
			{ID: "a", Plugin: p.ID},
			{ID: "g", Plugin: "core:group"},
			{ID: "g:b", Plugin: q.ID, Parent: "g"},
		},
		Plugins: map[string]kernel.Plugin{p.ID: p, q.ID: q},
	}
}

func plugin(id string, hooks ...*kernel.Event[int]) kernel.Plugin {
	p := kernel.Plugin{ID: id, Apply: func(*kernel.Context, any) error { return nil }}
	for _, h := range hooks {
		p.Hooks = append(p.Hooks, h.Use(kernel.DefaultStage))
	}
	return kernel.Define(p)
}

// row is what Committed must say of a row of the setup, State and Hooks included.
type row struct {
	ID, Plugin, Parent string
	Hooks              []string
}

var want = []row{
	{"a", "conformance/p", "", []string{first.Name()}},
	{"g", "core:group", "", nil},
	{"g:b", "conformance/q", "g", []string{first.Name(), second.Name()}},
}

// Run runs the suite, each case on a provider of its own.
func Run(t *testing.T, mount Mount) {
	rec := deftest.Ran(t, "composition", "obligations", "guard")
	rec.Run(t, "obligations", func(t *testing.T) {
		rec.Run(t, "composition.O1 committed lists the rows in order, ACTIVE, with their hooks", func(t *testing.T) {
			rd := use(t, mount).rd
			g, ok := rd.Committed(composition.ScopeProcess)
			if !ok || g.Scope != composition.ScopeProcess {
				t.Fatalf("Committed(process) = %+v, %v", g, ok)
			}
			same(t, "Committed(process)", g.Rows, want)
			for _, r := range g.Rows {
				if r.State != kernel.Active {
					t.Errorf("row %s is %s, want ACTIVE", r.ID, r.State)
				}
			}
		})

		rec.Run(t, "composition.O2 declaring lists the rows that declare the hook", func(t *testing.T) {
			rd := use(t, mount).rd
			same(t, "Declaring(first)", rd.Declaring(composition.ScopeProcess, first.Name()), []row{want[0], want[2]})
			same(t, "Declaring(second)", rd.Declaring(composition.ScopeProcess, second.Name()), want[2:])
			same(t, "Declaring of a hook no row declares", rd.Declaring(composition.ScopeProcess, "conformance/none"), nil)
		})

		rec.Run(t, "composition.O3 the tenant scope has no generation", func(t *testing.T) {
			rd := use(t, mount).rd
			if g, ok := rd.Committed(composition.ScopeTenant); ok {
				t.Errorf("Committed(tenant) = %+v, true", g)
			}
			same(t, "Declaring in the tenant scope", rd.Declaring(composition.ScopeTenant, first.Name()), nil)
		})

		rec.Run(t, "composition.O4 what it returns is a copy the caller may keep", func(t *testing.T) {
			rd := use(t, mount).rd
			g, _ := rd.Committed(composition.ScopeProcess)
			d := rd.Declaring(composition.ScopeProcess, second.Name())
			for _, rows := range [][]composition.Row{g.Rows, d} {
				for i := range rows {
					rows[i].ID, rows[i].Plugin = "changed", "changed"
					for j := range rows[i].Hooks {
						rows[i].Hooks[j] = "changed"
					}
				}
			}
			again, _ := rd.Committed(composition.ScopeProcess)
			same(t, "Committed after the caller changed what it got", again.Rows, want)
			same(t, "Declaring after the caller changed what it got", rd.Declaring(composition.ScopeProcess, second.Name()), want[2:])
		})
	})

	rec.Run(t, "guard", func(t *testing.T) {
		rec.Run(t, "composition.G1 after release the key fails with ErrInactive", func(t *testing.T) {
			u := use(t, mount)
			deftest.Dispose(t, u.root, u.provider)
			if err := deftest.ReadError(func() { composition.Key.Of(u.c) }); !errors.Is(err, kernel.ErrInactive) {
				t.Errorf("a read of the key after release = %v, want ErrInactive", err)
			}
		})

		rec.Run(t, "composition.G2 no side door", func(t *testing.T) {
			if doors := deftest.SideDoors[composition.Reader](use(t, mount).rd); len(doors) > 0 {
				t.Errorf("the provided value has methods outside composition.Reader: %v", doors)
			}
		})
	})
}

// same wants rows to be want, by id, plugin, parent and hooks, in order.
func same(t *testing.T, what string, rows []composition.Row, want []row) {
	t.Helper()
	got := make([]row, len(rows))
	for i, r := range rows {
		got[i] = row{r.ID, r.Plugin, r.Parent, r.Hooks}
	}
	if !slices.EqualFunc(got, want, func(a, b row) bool {
		return a.ID == b.ID && a.Plugin == b.Plugin && a.Parent == b.Parent && slices.Equal(a.Hooks, b.Hooks)
	}) {
		t.Errorf("%s = %+v, want %+v", what, got, want)
	}
}

// user is a consumer of the composition key on a mounted provider.
type user struct {
	root     *kernel.Root
	provider *kernel.Fiber
	c        *kernel.Context
	rd       composition.Reader
}

func use(t *testing.T, mount Mount) *user {
	t.Helper()
	u := &user{}
	u.root, u.provider = mount(t, setup())
	_, u.c, u.rd = deftest.Consume(t, u.root, composition.Key)
	return u
}
