// SPDX-License-Identifier: Apache-2.0

// Package slowdispose is the fault plugin slow-dispose of the failure matrix
// (08-profiles-roadmap-adrs.md:451 and test/failure-matrix/matrix.yaml). It is in no
// shipped distribution, and all its waits are bounded by config.
package slowdispose

import (
	"context"
	"errors"
	"time"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/test/dist/def"
	toy "github.com/mutex-solutions/aiops-platform/test/dist/def/toy/v0"
)

// ID is the plugin id.
const ID = "aiops-test/slow-dispose"

// New returns a plugin whose disposer ignores its context: it passes the dispose
// deadline, so the fiber is marked leaked and the launcher replaces the process ("A
// process-scope drain or disposer passes its deadline"). Config: delay (duration,
// default 1m; the disposer ends then) and failApply (true: Apply returns an error after
// it registered the disposer, so the partial effects unwind at boot). Otherwise it
// provides toy.fault.
func New() kernel.Plugin {
	type cfg struct {
		delay     time.Duration
		failApply bool
	}
	return kernel.Define(kernel.Plugin{
		ID:       ID,
		Provides: []kernel.AnyKey{toy.FaultKey},
		Decode: func(raw any) (any, error) {
			c := def.Parse(raw)
			out := cfg{delay: c.Duration("delay", time.Minute), failApply: c.Bool("failApply")}
			return out, c.Err()
		},
		Apply: func(c *kernel.Context, raw any) error {
			cf := raw.(cfg)
			c.Defer("slow-dispose", func(context.Context) error {
				def.Wait(cf.delay, nil)
				return nil
			})
			if cf.failApply {
				return errors.New("slow-dispose: apply failed after it registered its disposer")
			}
			return toy.FaultKey.Provide(c, toy.Fault{Owner: ID})
		},
	})
}
