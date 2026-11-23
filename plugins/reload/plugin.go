// SPDX-License-Identifier: Apache-2.0

// Package reload is the reload row: the one queue that changes the tree
// (03-c3-kernel.md:65). Submit returns an operation id at once; one goroutine, started
// with Context.Go, composes, admits, plans, applies in one loader batch, settles,
// judges and publishes composition/applied for each submission in order
// (03-c3-kernel.md:374-382). The pipeline is in internal/engine.
//
// The reload key is consumer-bound and loader is too: the distribution lists this row
// among the consumers of loader and the rows that submit among those of reload
// (07-c4-plugin-sdk.md:557).
package reload

import (
	"context"
	"errors"

	"github.com/mutex-solutions/aiops-platform/plugins/reload/internal/engine"
	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	loader "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

// Config is the config of the row, decoded by the rules of sdk/plugin. The docs ask
// for a bounded queue and a bounded history and give no number.
type Config struct {
	Queue   int // the most operations that wait; a Submit beyond it is refused. At least 1.
	History int // the results kept of final operations. At least 1.
}

// id is the plugin id of the reload row.
const id = "aiops/reload"

// New returns the reload row. Its activations share one engine.Memory, so a restart of
// the row (a generation that changes its config) goes on numbering operations and
// generations and keeps the results of the final ones. A binary makes one value, which
// its module table holds and its binding maps list.
func New() kernel.Plugin {
	m := engine.NewMemory()
	return plugin.Define(plugin.Spec[Config]{
		ID: id,
		Inject: []kernel.Dep{kernel.Need(loader.Key), kernel.Need(launch.ProfileContextKey),
			kernel.Need(launch.CatalogKey)},
		Provides: []kernel.AnyKey{def.Key},
		Validate: validate,
		Apply:    func(c *kernel.Context, cfg *Config) error { return apply(c, *cfg, m) },
	})
}

func apply(c *kernel.Context, conf Config, m *engine.Memory) error {
	applied, operation := def.Applied.Bind(c), def.Operation.Bind(c)
	e := engine.New(engine.Deps{
		Profile: launch.ProfileContextKey.Of(c), Catalog: launch.CatalogKey.Of(c), Loader: loader.Key.Of(c),
		Publish: func(ctx context.Context, a composition.Applied) error {
			return applied.Emit(ctx, kernel.Unscoped, a)
		},
		Progress: func(ctx context.Context, r def.Result) error {
			return operation.Emit(ctx, kernel.Unscoped, r)
		},
		Self: id,
		Go: func(label string, run func(context.Context) error) (err error) {
			defer func() {
				if p := recover(); p != nil {
					err = p.(error) // Context.Go refuses an unloading row with a panic of an error
				}
			}()
			c.Go(label, run)
			return nil
		},
	}, m, conf.Queue, conf.History)
	c.Go("reload", e.Run)
	return def.Key.Provide(c, served{e})
}

// served is what the reload key serves: the methods of def.Reload and no other, so no
// consumer reaches the engine's loop by a type assertion (07-c4-plugin-sdk.md:516).
type served struct{ e *engine.Engine }

func (s served) Submit(ctx context.Context, op def.Op) (def.ID, error) { return s.e.Submit(ctx, op) }
func (s served) Result(id def.ID) (def.Result, bool)                   { return s.e.Result(id) }
func (s served) Await(ctx context.Context, id def.ID) (def.Result, error) {
	return s.e.Await(ctx, id)
}

// validate holds the bounds to at least 1.
func validate(conf *Config) error {
	if conf.Queue < 1 || conf.History < 1 {
		return errors.New("reload: queue and history must be at least 1")
	}
	return nil
}
