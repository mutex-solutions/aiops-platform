// SPDX-License-Identifier: Apache-2.0

// Package reload is the reload row: the one queue that changes the tree
// (03-c3-kernel.md:65). Submit returns an operation id at once; one goroutine, started
// with Context.Go, composes, admits, plans, applies in one loader batch, settles,
// judges and publishes composition/applied for each submission in order
// (03-c3-kernel.md:374-382). The pipeline is in internal/engine.
//
// The reload key is consumer-bound and loader is too: the distribution lists this row
// among the consumers of loader and the rows that submit among those of reload
// (07-c4-plugin-sdk.md:556).
package reload

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/mutex-solutions/aiops-platform/plugins/reload/internal/engine"
	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	loader "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Config is the config of the row. The docs ask for a bounded queue and a bounded
// history and give no number.
type Config struct {
	Queue   int // the most operations that wait; a Submit beyond it is refused. At least 1.
	History int // the results kept of final operations. At least 1.
}

// Plugin is the reload row.
var Plugin = kernel.Define(kernel.Plugin{
	ID: "aiops/reload",
	Inject: []kernel.Dep{kernel.Need(loader.Key), kernel.Need(launch.ProfileContextKey),
		kernel.Need(launch.CatalogKey)},
	Provides: []kernel.AnyKey{def.Key},
	Decode:   decode,
	Apply: func(c *kernel.Context, cfg any) error {
		conf := cfg.(Config)
		applied := def.Applied.Bind(c)
		e := engine.New(engine.Deps{
			Profile: launch.ProfileContextKey.Of(c), Catalog: launch.CatalogKey.Of(c), Loader: loader.Key.Of(c),
			Publish: func(ctx context.Context, a composition.Applied) error {
				return applied.Emit(ctx, kernel.Unscoped, a)
			},
		}, conf.Queue, conf.History)
		c.Go("reload", e.Run)
		return def.Key.Provide(c, e)
	},
})

func decode(raw any) (any, error) {
	var conf Config
	switch v := raw.(type) {
	case Config:
		conf = v
	case map[string]any:
		for k, e := range v {
			n, err := integer(e)
			switch k {
			case "queue":
				conf.Queue = n
			case "history":
				conf.History = n
			default:
				err = errors.New("unknown field")
			}
			if err != nil {
				return nil, fmt.Errorf("reload: %s: %w", k, err)
			}
		}
	default:
		return nil, fmt.Errorf("reload: config is %T, want Config or a map", raw)
	}
	if conf.Queue < 1 || conf.History < 1 {
		return nil, errors.New("reload: queue and history must be at least 1")
	}
	return conf, nil
}

// integer reads a whole number from a parsed config value.
func integer(v any) (int, error) {
	switch n := v.(type) {
	case int64:
		if n == int64(int(n)) {
			return int(n), nil
		}
	case int:
		return n, nil
	case float64:
		if n == math.Trunc(n) && math.Abs(n) < 1<<31 {
			return int(n), nil
		}
	}
	return 0, fmt.Errorf("want a whole number, got %v", v)
}
