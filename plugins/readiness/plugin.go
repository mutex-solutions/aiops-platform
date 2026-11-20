// SPDX-License-Identifier: Apache-2.0

// Package readiness is the readiness row: it holds the ready latch while the required
// set is ACTIVE and drops it while it is not (03-c3-kernel.md:67, :392; 08-profiles-
// roadmap-adrs.md:536). A provider that restarts in place is not ACTIVE, and a fiber
// that restart-policy gave up on stays FAILED, so one rule covers "drops it while a
// provider the replica cannot serve without restarts in place" and "readiness fails"
// after a give-up (08-profiles-roadmap-adrs.md:485).
//
// It recomputes on every event of internal/status and on composition/applied. The
// computation reads the live states through the composition key and keeps no event
// state, so the overflow notice, which says events were lost, needs nothing but a
// recomputation: every event, the notice included, does exactly that. It drops the
// latch when it is disposed.
//
// The listener on internal/status is sealed: the distribution lists this row for it in
// Config.Listeners (ADR-034, "S1 additions"; 07-c4-plugin-sdk.md:463).
package readiness

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Plugin is the readiness row. It has no config.
var Plugin = kernel.Define(kernel.Plugin{
	ID: "aiops/readiness",
	Inject: []kernel.Dep{kernel.Need(launch.ProfileContextKey), kernel.Need(launch.CatalogKey),
		kernel.Need(launch.ReadyKey), kernel.Need(composition.Key), kernel.Need(kernel.LoggerKey)},
	Hooks: []kernel.HookUse{kernel.InternalStatus.Use(kernel.DefaultStage), reload.Applied.Use(kernel.DefaultStage)},
	Apply: func(c *kernel.Context, _ any) error {
		r := &latch{
			pc: launch.ProfileContextKey.Of(c), cat: launch.CatalogKey.Of(c), ready: launch.ReadyKey.Of(c),
			rows: composition.Key.Of(c), log: kernel.LoggerKey.Of(c).For(c),
		}
		// Registered first, so that it runs last: no listener can set the latch again
		// after the drop.
		c.Defer("readiness latch", func(context.Context) error { r.ready.Drop(); return nil })
		kernel.InternalStatus.On(c, kernel.DefaultStage, func(context.Context, kernel.LedgerEvent) (any, error) {
			r.evaluate()
			return nil, nil
		})
		reload.Applied.On(c, kernel.DefaultStage, func(context.Context, composition.Applied) (any, error) {
			r.evaluate()
			return nil, nil
		})
		return nil
	},
})

// latch is the one computation. Its listeners run on two goroutines (the kernel's
// observer dispatcher and reload's), so evaluate is serialised.
type latch struct {
	pc    launch.ProfileContext
	cat   launch.Catalog
	ready launch.Ready
	rows  composition.Reader
	log   *slog.Logger

	mu    sync.Mutex
	known bool
	last  string
}

// evaluate sets or drops the latch from the live states and logs a change of what the
// process waits for: "readiness" is the diagnostic of the failure matrix
// (test/failure-matrix/matrix.yaml). It costs one read of the tree per event.
// ponytail: O(rows) per status event; coalesce with a dirty flag if boot shows it.
func (r *latch) evaluate() {
	r.mu.Lock()
	defer r.mu.Unlock()
	g, _ := r.rows.Committed(composition.ScopeProcess)
	states := make([]launch.RowState, len(g.Rows))
	for i, row := range g.Rows {
		states[i] = launch.RowState{ID: row.ID, Plugin: row.Plugin, State: row.State}
	}
	unmet := r.pc.Required().Unmet(states, func(plugin string) []string {
		e, _ := r.cat.Lookup(plugin)
		return e.Provides
	})
	if len(unmet) == 0 {
		r.ready.Set()
	} else {
		r.ready.Drop()
	}
	if now := strings.Join(unmet, " "); !r.known || now != r.last {
		r.known, r.last = true, now
		if len(unmet) == 0 {
			r.log.Info("readiness: ready, the required set is ACTIVE")
		} else {
			r.log.Warn("readiness: not ready, the required set is not ACTIVE", "waiting", unmet)
		}
	}
}
