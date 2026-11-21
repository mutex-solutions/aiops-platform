// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
)

// registry is the value of the built-in registry service, "plugin records, the
// fiber index" (plugin record :55). The documents give it no interface; its one
// method is the lookup that ADR-059 decision 2 adds for the retry plugin
// (restart-policy, 03-c3-kernel.md:66). The core keeps the records and the index.
type registry struct{ core *core }

// registryCell is the config cell of every Fiber that registry.Fiber returns: the
// core does not keep a fiber's cell, so such a Fiber cannot change the config, and
// each operation that carries one returns errRegistryConfig instead of dropping it.
var (
	registryCell      = new(cfgCell)
	errRegistryConfig = errors.New("a fiber found through the registry cannot change its config")
)

// Fiber returns the fiber with id, the id that FiberSnapshot.ID and
// LedgerEvent.Fiber carry (ADR-059 decision 2; 03-c3-kernel.md:66). ok is false
// for the root's id 0, an id never issued and one whose archived record was
// evicted. A fiber that was disposed and is still archived is returned, and its
// methods answer as for any disposed fiber. The Fiber it returns can Dispose,
// Await and restart that fiber with Update(ctx, nil), which keeps the config the
// fiber retains, so whoever holds the registry holds those powers over every fiber
// of the kernel: the documents are silent on who may, so RegistryKey is
// consumer-bound and only the entries that Config.Consumers lists resolve it (ADR-059
// decision 33). It cannot change the config: Commit, Update with a config,
// UpdateOpWith and CommitOp of it return an error. It asks the owner, so call it
// off the owner and off any callback of the fiber it asks about.
func (r *registry) Fiber(id uint64) (*Fiber, bool) {
	f := r.core.fiber(id)
	_, err := f.snapshot(context.Background())
	return &Fiber{ref: f, cell: registryCell}, err == nil
}
