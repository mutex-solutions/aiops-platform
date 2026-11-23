// SPDX-License-Identifier: Apache-2.0

// Package leakygoroutine is the fault plugin leaky-goroutine of the failure matrix
// (08-profiles-roadmap-adrs.md:451 and test/failure-matrix/matrix.yaml). It is in no
// shipped distribution, and all its waits are bounded by config.
package leakygoroutine

import (
	"context"
	"errors"
	"time"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/test/dist/def"
	toy "github.com/mutex-solutions/aiops-platform/test/dist/def/toy/v0"
)

// ID is the plugin id.
const ID = "aiops-test/leaky-goroutine"

// New returns a plugin that starts a tracked goroutine that ignores its context: it
// holds a lease past the drain deadline, so the unload of the fiber passes its drain
// deadline. Config: hold (duration, default 1m; the goroutine ends then) and failApply
// (true: Apply returns an error after it started the goroutine, so the partial effects
// unwind at boot with no recomposition). Otherwise it provides toy.fault.
func New() kernel.Plugin {
	type cfg struct {
		hold      time.Duration
		failApply bool
	}
	return kernel.Define(kernel.Plugin{
		ID:       ID,
		Provides: []kernel.AnyKey{toy.FaultKey},
		Decode: func(raw any) (any, error) {
			c := def.Parse(raw)
			out := cfg{hold: c.Duration("hold", time.Minute), failApply: c.Bool("failApply")}
			return out, c.Err()
		},
		Apply: func(c *kernel.Context, raw any) error {
			cf := raw.(cfg)
			c.Go("leaky", func(context.Context) error {
				def.Wait(cf.hold, nil)
				return nil
			})
			if cf.failApply {
				return errors.New("leaky-goroutine: apply failed after it started its goroutine")
			}
			return toy.FaultKey.Provide(c, toy.Fault{Owner: ID})
		},
	})
}
