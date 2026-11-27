// SPDX-License-Identifier: Apache-2.0

// Package panicapply is the fault plugin panic-apply of the failure matrix
// (08-profiles-roadmap-adrs.md:451 and test/failure-matrix/matrix.yaml). It is in no
// shipped distribution, and all its waits are bounded by config.
package panicapply

import (
	"context"
	"fmt"
	"time"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/test/dist/def"
	toy "github.com/mutex-solutions/aiops-platform/test/dist/def/toy/v0"
)

// ID is the plugin id.
const ID = "aiops-test/panic-apply"

// New returns a plugin that panics. Config: mode (string): "apply" panics in Apply,
// which the kernel contains as a FAILED fiber; "tracked" panics in a tracked goroutine
// (Context.Go), which the kernel contains as FAILED and not fatal; "untracked" panics
// in a goroutine the kernel does not know, which ends the process ("An untracked
// goroutine panics: Fatal"). The untracked goroutine is made by a timer callback,
// which the runtime runs on a goroutine of its own, so that no plugin of the test
// distribution holds a bare go statement. With trigger (the name of a file in dir, by
// default the runtime directory of the home) the panic of "tracked" and "untracked"
// waits for the file, so that it happens to a running process: Apply returns at once,
// as it should. Where it does not panic in Apply it provides toy.fault.
func New() kernel.Plugin {
	type cfg struct{ mode, trigger, dir string }
	return kernel.Define(kernel.Plugin{
		ID:       ID,
		Inject:   []kernel.Dep{kernel.Need(launch.CmdlineKey)},
		Provides: []kernel.AnyKey{toy.FaultKey},
		Decode: func(raw any) (any, error) {
			c := def.Parse(raw)
			out := cfg{mode: c.String("mode", "apply"), trigger: c.String("trigger", ""), dir: c.String("dir", "")}
			if out.mode != "apply" && out.mode != "tracked" && out.mode != "untracked" {
				return nil, fmt.Errorf("invalid config: mode %q is not apply, tracked or untracked", out.mode)
			}
			return out, c.Err()
		},
		Apply: func(c *kernel.Context, raw any) error {
			cf := raw.(cfg)
			var path string
			if cf.trigger != "" {
				var err error
				if path, err = def.TriggerPath(c, cf.dir, cf.trigger); err != nil {
					return err
				}
			}
			switch cf.mode {
			case "tracked":
				if err := toy.FaultKey.Provide(c, toy.Fault{Owner: ID}); err != nil {
					return err
				}
				c.Go("panic-tracked", func(ctx context.Context) error {
					if path != "" && !def.AwaitFile(path, ctx.Done()) {
						return nil
					}
					panic("panic-apply: tracked goroutine panics on purpose")
				})
			case "untracked":
				time.AfterFunc(0, func() {
					if path != "" {
						def.AwaitFile(path, nil)
					}
					panic("panic-apply: untracked goroutine panics on purpose")
				})
			default:
				panic("panic-apply: Apply panics on purpose")
			}
			return nil
		},
	})
}
