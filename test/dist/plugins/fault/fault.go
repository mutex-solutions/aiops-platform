// SPDX-License-Identifier: Apache-2.0

// Package fault holds the seven fault plugins of the failure matrix
// (08-profiles-roadmap-adrs.md:451 and test/failure-matrix/matrix.yaml): fail-apply,
// panic-apply, slow-apply, bad-config, rogue-provider, leaky-goroutine and
// slow-dispose. Each is driven by its row's config, so one binary serves every
// cell, and each but rogue-provider provides toy.fault, so that a row that injects
// the key is a dependant of the fault ("dependant" in the matrix).
//
// None of them is in a shipped distribution. All waits are bounded by config.
package fault

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/test/dist/def"
)

// The plugin ids of the faults.
const (
	FailApplyID      = "aiops-test/fail-apply"
	PanicApplyID     = "aiops-test/panic-apply"
	SlowApplyID      = "aiops-test/slow-apply"
	BadConfigID      = "aiops-test/bad-config"
	RogueProviderID  = "aiops-test/rogue-provider"
	LeakyGoroutineID = "aiops-test/leaky-goroutine"
	SlowDisposeID    = "aiops-test/slow-dispose"
)

func provideFault(c *kernel.Context, owner string) error {
	return def.FaultKey.Provide(c, def.Fault{Owner: owner})
}

// FailApply is a plugin whose Apply returns an error: "Apply errors" (matrix cell
// "Apply errors, panics or passes the load deadline"). Config: message (string) and
// failTimes (whole number): with failTimes it fails that many activations and then
// succeeds, so that a retry by restart-policy can be seen to recover; with none it
// always fails. The count is kept per plugin value, which a distribution creates
// once.
func FailApply() kernel.Plugin {
	type cfg struct {
		message string
		times   int
	}
	var applied atomic.Int64
	return kernel.Define(kernel.Plugin{
		ID:       FailApplyID,
		Provides: []kernel.AnyKey{def.FaultKey},
		Decode: func(raw any) (any, error) {
			c := def.Parse(raw)
			out := cfg{message: c.String("message", "fail-apply: apply failed on purpose"), times: c.Int("failTimes", 0)}
			return out, c.Err()
		},
		Apply: func(c *kernel.Context, raw any) error {
			cf := raw.(cfg)
			if n := applied.Add(1); cf.times == 0 || n <= int64(cf.times) {
				return errors.New(cf.message)
			}
			return provideFault(c, FailApplyID)
		},
	})
}

// PanicApply panics. Config: mode (string): "apply" panics in Apply, which the kernel
// contains as a FAILED fiber; "tracked" panics in a tracked goroutine (Context.Go),
// which the kernel contains as FAILED and not fatal; "untracked" panics in a
// goroutine the kernel does not know, which ends the process ("An untracked
// goroutine panics: Fatal"). The untracked goroutine is made by a timer callback,
// which the runtime runs on a goroutine of its own, so that no plugin of the test
// distribution holds a bare go statement.
func PanicApply() kernel.Plugin {
	type cfg struct{ mode string }
	return kernel.Define(kernel.Plugin{
		ID:       PanicApplyID,
		Provides: []kernel.AnyKey{def.FaultKey},
		Decode: func(raw any) (any, error) {
			c := def.Parse(raw)
			out := cfg{mode: c.String("mode", "apply")}
			if out.mode != "apply" && out.mode != "tracked" && out.mode != "untracked" {
				return nil, fmt.Errorf("invalid config: mode %q is not apply, tracked or untracked", out.mode)
			}
			return out, c.Err()
		},
		Apply: func(c *kernel.Context, raw any) error {
			switch raw.(cfg).mode {
			case "tracked":
				if err := provideFault(c, PanicApplyID); err != nil {
					return err
				}
				c.Go("panic-tracked", func(context.Context) error { panic("panic-apply: tracked goroutine panics on purpose") })
			case "untracked":
				time.AfterFunc(0, func() { panic("panic-apply: untracked goroutine panics on purpose") })
			default:
				panic("panic-apply: Apply panics on purpose")
			}
			return nil
		},
	})
}

// SlowApply is a plugin whose Apply passes the load deadline. Config: delay
// (duration, default 1h). It waits for the delay or for its context, which the
// kernel cancels at the deadline, and returns the context's error.
func SlowApply() kernel.Plugin {
	type cfg struct{ delay time.Duration }
	return kernel.Define(kernel.Plugin{
		ID:       SlowApplyID,
		Provides: []kernel.AnyKey{def.FaultKey},
		Decode: func(raw any) (any, error) {
			c := def.Parse(raw)
			out := cfg{delay: c.Duration("delay", time.Hour)}
			return out, c.Err()
		},
		Apply: func(c *kernel.Context, raw any) error {
			if !def.Wait(raw.(cfg).delay, c.Std().Done()) {
				return c.Std().Err()
			}
			return provideFault(c, SlowApplyID)
		},
	})
}

// BadConfig accepts the config {} and {ok: true} and refuses every other: its Decode
// fails with "invalid config", which is "Config fails schema or Validate". A row
// that starts with a bad config is FAILED; an update to a bad config is rejected and
// the row keeps its instance (matrix, "Live recomposition").
func BadConfig() kernel.Plugin {
	return kernel.Define(kernel.Plugin{
		ID:       BadConfigID,
		Provides: []kernel.AnyKey{def.FaultKey},
		Decode: func(raw any) (any, error) {
			m, _ := raw.(map[string]any)
			if raw != nil && m == nil {
				return nil, fmt.Errorf("invalid config: config is %T, want a map", raw)
			}
			for k, v := range m {
				if k != "ok" || v != true {
					return nil, fmt.Errorf("invalid config: field %q is not accepted; the only value is {ok: true}", k)
				}
			}
			return nil, nil
		},
		Apply: func(c *kernel.Context, _ any) error { return provideFault(c, BadConfigID) },
	})
}

// RogueProvider provides the provider-bound key toy.policy, which the distribution
// binds to toy-provider and not to it: the gate denies it (check 7, "plugin provides
// a provider-bound key and is not in its binding"), and a row of it with isolate on
// toy.policy is a static graph error. Were the gate bypassed, the kernel refuses the
// Provide with ErrBinding and the row is FAILED.
func RogueProvider() kernel.Plugin {
	return kernel.Define(kernel.Plugin{
		ID:       RogueProviderID,
		Provides: []kernel.AnyKey{def.PolicyKey},
		Apply: func(c *kernel.Context, _ any) error {
			return def.PolicyKey.Provide(c, def.Policy{Owner: RogueProviderID})
		},
	})
}

// LeakyGoroutine starts a tracked goroutine that ignores its context: it holds a
// lease past the drain deadline, so the unload of the fiber passes its drain
// deadline. Config: hold (duration, default 1m; the goroutine ends then).
func LeakyGoroutine() kernel.Plugin {
	type cfg struct{ hold time.Duration }
	return kernel.Define(kernel.Plugin{
		ID:       LeakyGoroutineID,
		Provides: []kernel.AnyKey{def.FaultKey},
		Decode: func(raw any) (any, error) {
			c := def.Parse(raw)
			out := cfg{hold: c.Duration("hold", time.Minute)}
			return out, c.Err()
		},
		Apply: func(c *kernel.Context, raw any) error {
			hold := raw.(cfg).hold
			c.Go("leaky", func(context.Context) error {
				def.Wait(hold, nil)
				return nil
			})
			return provideFault(c, LeakyGoroutineID)
		},
	})
}

// SlowDispose has a disposer that ignores its context: it passes the dispose
// deadline, so the fiber is marked leaked and the launcher replaces the process
// ("A process-scope drain or disposer passes its deadline"). Config: delay
// (duration, default 1m; the disposer ends then).
func SlowDispose() kernel.Plugin {
	type cfg struct{ delay time.Duration }
	return kernel.Define(kernel.Plugin{
		ID:       SlowDisposeID,
		Provides: []kernel.AnyKey{def.FaultKey},
		Decode: func(raw any) (any, error) {
			c := def.Parse(raw)
			out := cfg{delay: c.Duration("delay", time.Minute)}
			return out, c.Err()
		},
		Apply: func(c *kernel.Context, raw any) error {
			delay := raw.(cfg).delay
			c.Defer("slow-dispose", func(context.Context) error {
				def.Wait(delay, nil)
				return nil
			})
			return provideFault(c, SlowDisposeID)
		},
	})
}
