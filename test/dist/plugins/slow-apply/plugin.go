// SPDX-License-Identifier: Apache-2.0

// Package slowapply is the fault plugin slow-apply of the failure matrix
// (08-profiles-roadmap-adrs.md:451 and test/failure-matrix/matrix.yaml). It is in no
// shipped distribution, and all its waits are bounded by config.
package slowapply

import (
	"time"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/test/dist/def"
	toy "github.com/mutex-solutions/aiops-platform/test/dist/def/toy/v0"
)

// ID is the plugin id.
const ID = "aiops-test/slow-apply"

// New returns a plugin whose Apply passes the load deadline. Config: delay (duration,
// default 1h). It waits for the delay or for its context, which the kernel cancels at
// the deadline, and returns the context's error; after the delay it provides
// toy.fault.
func New() kernel.Plugin {
	type cfg struct{ delay time.Duration }
	return kernel.Define(kernel.Plugin{
		ID:       ID,
		Provides: []kernel.AnyKey{toy.FaultKey},
		Decode: func(raw any) (any, error) {
			c := def.Parse(raw)
			out := cfg{delay: c.Duration("delay", time.Hour)}
			return out, c.Err()
		},
		Apply: func(c *kernel.Context, raw any) error {
			if !def.Wait(raw.(cfg).delay, c.Std().Done()) {
				return c.Std().Err()
			}
			return toy.FaultKey.Provide(c, toy.Fault{Owner: ID})
		},
	})
}
