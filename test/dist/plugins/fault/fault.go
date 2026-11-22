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
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/mutex-solutions/aiops-platform/internal/runfile"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
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

// triggerPath is the file whose appearance fires a fault that has a trigger: a name in
// the runtime directory of the home. A test creates it once the process has booted, so
// that the fault happens to a running process at a moment the test chose, not after a
// guessed delay.
func triggerPath(c *kernel.Context, name string) (string, error) {
	home, ok := launch.CmdlineKey.Of(c).Flag("home")
	if !ok {
		return "", errors.New("a trigger needs --home")
	}
	return filepath.Join(runfile.Dir(home), name), nil
}

// await polls for the file until it exists or stop is closed, and reports whether it
// exists. The poll is a timer, not a sleep of a fixed length that a test relies on.
func await(path string, stop <-chan struct{}) bool {
	for {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		if !def.Wait(10*time.Millisecond, stop) {
			return false
		}
	}
}

// FailApply is a plugin whose Apply returns an error: "Apply errors" (matrix cell
// "Apply errors, panics or passes the load deadline"). Config: message (string) and
// failTimes (whole number): with failTimes it fails that many activations and then
// succeeds, so that a retry by restart-policy can be seen to recover; with none it
// always fails. The count is kept per plugin value, which a distribution creates
// once. With trigger (the name of a file in the runtime directory) it is a provider
// that fails while running: Apply succeeds and a tracked goroutine returns the error
// once the file exists, which is "a tracked goroutine errors", so the fiber is FAILED
// after it was ACTIVE; while the file exists Apply itself fails, so each retry fails
// again, and once it is removed the next retry succeeds: the provider comes back.
func FailApply() kernel.Plugin {
	type cfg struct {
		message string
		trigger string
		times   int
	}
	var applied atomic.Int64
	return kernel.Define(kernel.Plugin{
		ID:       FailApplyID,
		Inject:   []kernel.Dep{kernel.Need(launch.CmdlineKey)},
		Provides: []kernel.AnyKey{def.FaultKey},
		Decode: func(raw any) (any, error) {
			c := def.Parse(raw)
			out := cfg{message: c.String("message", "fail-apply: apply failed on purpose"), times: c.Int("failTimes", 0),
				trigger: c.String("trigger", "")}
			return out, c.Err()
		},
		Apply: func(c *kernel.Context, raw any) error {
			cf := raw.(cfg)
			if cf.trigger != "" {
				path, err := triggerPath(c, cf.trigger)
				if err != nil {
					return err
				}
				if _, err := os.Stat(path); err == nil {
					return errors.New(cf.message)
				}
				c.Go("fail-apply-trigger", func(ctx context.Context) error {
					if !await(path, ctx.Done()) {
						return nil
					}
					return errors.New(cf.message)
				})
				return provideFault(c, FailApplyID)
			}
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
// distribution holds a bare go statement. With trigger (the name of a file in the
// runtime directory) the panic of "tracked" and "untracked" waits for the file, so that
// it happens to a running process: Apply returns at once, as it should.
func PanicApply() kernel.Plugin {
	type cfg struct{ mode, trigger string }
	return kernel.Define(kernel.Plugin{
		ID:       PanicApplyID,
		Inject:   []kernel.Dep{kernel.Need(launch.CmdlineKey)},
		Provides: []kernel.AnyKey{def.FaultKey},
		Decode: func(raw any) (any, error) {
			c := def.Parse(raw)
			out := cfg{mode: c.String("mode", "apply"), trigger: c.String("trigger", "")}
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
				if path, err = triggerPath(c, cf.trigger); err != nil {
					return err
				}
			}
			switch cf.mode {
			case "tracked":
				if err := provideFault(c, PanicApplyID); err != nil {
					return err
				}
				c.Go("panic-tracked", func(ctx context.Context) error {
					if path != "" && !await(path, ctx.Done()) {
						return nil
					}
					panic("panic-apply: tracked goroutine panics on purpose")
				})
			case "untracked":
				time.AfterFunc(0, func() {
					if path != "" {
						await(path, nil)
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

// BadConfig accepts the config {}, {ok: true} and a field note whose value is not the
// string "refused", and refuses every other: its Decode fails with "invalid config",
// which is "Config fails schema or Validate". A row that starts with a bad config is
// FAILED; an update to a bad config is rejected and the row keeps its instance (matrix,
// "Live recomposition"). A note that holds a reference decodes as it is, unresolved: only
// the value it resolves to can be refused, which a check of the raw config does not see.
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
				if k == "note" && v != "refused" || k == "ok" && v == true {
					continue
				}
				return nil, fmt.Errorf("invalid config: field %q is not accepted; the values are {ok: true} and a note other than \"refused\"", k)
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
// deadline. Config: hold (duration, default 1m; the goroutine ends then) and failApply
// (true: Apply returns an error after it started the goroutine, so the partial effects
// unwind at boot with no recomposition).
func LeakyGoroutine() kernel.Plugin {
	type cfg struct {
		hold      time.Duration
		failApply bool
	}
	return kernel.Define(kernel.Plugin{
		ID:       LeakyGoroutineID,
		Provides: []kernel.AnyKey{def.FaultKey},
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
			return provideFault(c, LeakyGoroutineID)
		},
	})
}

// SlowDispose has a disposer that ignores its context: it passes the dispose
// deadline, so the fiber is marked leaked and the launcher replaces the process
// ("A process-scope drain or disposer passes its deadline"). Config: delay
// (duration, default 1m; the disposer ends then) and failApply (true: Apply returns an
// error after it registered the disposer, so the partial effects unwind at boot).
func SlowDispose() kernel.Plugin {
	type cfg struct {
		delay     time.Duration
		failApply bool
	}
	return kernel.Define(kernel.Plugin{
		ID:       SlowDisposeID,
		Provides: []kernel.AnyKey{def.FaultKey},
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
			return provideFault(c, SlowDisposeID)
		},
	})
}
