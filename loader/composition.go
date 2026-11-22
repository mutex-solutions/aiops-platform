// SPDX-License-Identifier: Apache-2.0

package loader

import (
	"context"

	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// reader serves the composition key (03-c3-kernel.md:64): the rows the loader
// mounts, with the live state of each fiber, its declared hooks and its
// registrations (the labels of its effect ledger). The loader holds the process
// scope only, so any other scope has no generation, and it holds no generation
// number or digest (reload commits, 03-c3-kernel.md:382): composition.Generation
// is narrowed to the scope and the rows. A row that is not mounted (disabled, of
// another role) is not part of what runs and is not listed.
type reader struct{ t *tree }

var _ composition.Reader = reader{}

// Committed returns the rows of the process scope in creation order.
func (r reader) Committed(scope composition.Scope) (composition.Generation, bool) {
	if scope != composition.ScopeProcess {
		return composition.Generation{}, false
	}
	t := r.t
	t.mu.Lock()
	type held struct {
		e *entry
		f *kernel.Fiber
	}
	var rows []held
	for _, id := range t.order {
		if e := t.entries[id]; t.mount(e) == def.Mounted {
			rows = append(rows, held{e, e.fiber})
		}
	}
	specs := make([]def.EntrySpec, len(rows))
	for i, h := range rows {
		specs[i] = h.e.spec
	}
	t.mu.Unlock()
	g := composition.Generation{Scope: scope, Rows: make([]composition.Row, len(rows))}
	for i, h := range rows {
		row := composition.Row{ID: specs[i].ID, Plugin: specs[i].Plugin, Parent: specs[i].Parent, State: kernel.Pending}
		if p, ok := t.plugins[specs[i].Plugin]; ok {
			for _, u := range p.Hooks {
				row.Hooks = append(row.Hooks, u.Hook())
			}
		}
		if h.f != nil {
			ctx := context.Background()
			row.Fiber = h.f.ID()
			row.State, _ = h.f.State(ctx)
			row.Registrations, _ = h.f.Effects(ctx)
		}
		g.Rows[i] = row
	}
	return g, true
}

// Declaring returns the rows of the process scope that declare hook.
func (r reader) Declaring(scope composition.Scope, hook string) []composition.Row {
	g, _ := r.Committed(scope)
	var out []composition.Row
	for _, row := range g.Rows {
		for _, h := range row.Hooks {
			if h == hook {
				out = append(out, row)
				break
			}
		}
	}
	return out
}
