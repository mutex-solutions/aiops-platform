// SPDX-License-Identifier: Apache-2.0

// Package failapply is the fault plugin fail-apply of the failure matrix
// (08-profiles-roadmap-adrs.md:451 and test/failure-matrix/matrix.yaml). It is in no
// shipped distribution, and all its waits are bounded by config.
package failapply

import (
	"context"
	"errors"
	"os"
	"sync/atomic"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/test/dist/def"
	toy "github.com/mutex-solutions/aiops-platform/test/dist/def/toy/v0"
)

// ID is the plugin id.
const ID = "aiops-test/fail-apply"

// New returns a plugin whose Apply returns an error: "Apply errors" (matrix cell
// "Apply errors, panics or passes the load deadline"). It provides toy.fault, so that
// a row that injects the key is a dependant of the fault. Config: message (string)
// and failTimes (whole number): with failTimes it fails that many activations and then
// succeeds, so that a retry by restart-policy can be seen to recover; with none it
// always fails. The count is kept per plugin value, which a distribution creates
// once. With trigger (the name of a file in dir, by default the runtime directory of
// the home) it is a provider that fails while running: Apply succeeds and a tracked
// goroutine returns the error once the file exists, which is "a tracked goroutine
// errors", so the fiber is FAILED after it was ACTIVE; while the file exists Apply
// itself fails, so each retry fails again, and once it is removed the next retry
// succeeds: the provider comes back.
func New() kernel.Plugin {
	type cfg struct {
		message      string
		trigger, dir string
		times        int
	}
	var applied atomic.Int64
	return kernel.Define(kernel.Plugin{
		ID:       ID,
		Inject:   []kernel.Dep{kernel.Need(launch.CmdlineKey)},
		Provides: []kernel.AnyKey{toy.FaultKey},
		Decode: func(raw any) (any, error) {
			c := def.Parse(raw)
			out := cfg{message: c.String("message", "fail-apply: apply failed on purpose"), times: c.Int("failTimes", 0),
				trigger: c.String("trigger", ""), dir: c.String("dir", "")}
			return out, c.Err()
		},
		Apply: func(c *kernel.Context, raw any) error {
			cf := raw.(cfg)
			if cf.trigger != "" {
				path, err := def.TriggerPath(c, cf.dir, cf.trigger)
				if err != nil {
					return err
				}
				if _, err := os.Stat(path); err == nil {
					return errors.New(cf.message)
				}
				c.Go("fail-apply-trigger", func(ctx context.Context) error {
					if !def.AwaitFile(path, ctx.Done()) {
						return nil
					}
					return errors.New(cf.message)
				})
				return toy.FaultKey.Provide(c, toy.Fault{Owner: ID})
			}
			if n := applied.Add(1); cf.times == 0 || n <= int64(cf.times) {
				return errors.New(cf.message)
			}
			return toy.FaultKey.Provide(c, toy.Fault{Owner: ID})
		},
	})
}
