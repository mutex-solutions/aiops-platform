// SPDX-License-Identifier: Apache-2.0

// Package toyconsumer is the toy consumer of the test distribution: it needs toy.store
// and toy.spine and provides nothing, so a profile may hold several. It is written with
// plugin.Define, since its note is a volatile field and a plugin written with
// kernel.Define can mark none (ADR-048 decisions 9 and 18).
package toyconsumer

import (
	"context"
	"errors"
	"time"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
	"github.com/mutex-solutions/aiops-platform/test/dist/def"
	spine "github.com/mutex-solutions/aiops-platform/test/dist/def/spine/v0"
	toy "github.com/mutex-solutions/aiops-platform/test/dist/def/toy/v0"
)

// ID is the plugin id.
const ID = "aiops-test/toy-consumer"

// Config is the row config. Every delay is data, never a default sleep.
type Config struct {
	Note       plugin.Volatile[string] // logged when the row applies
	ApplyDelay time.Duration
	// CallEvery puts the spine under load: with it a tracked goroutine calls the spine,
	// holding its lease for CallHold, every CallEvery until the row unloads. 0 is no
	// load.
	CallEvery, CallHold time.Duration
}

// New returns the plugin.
func New() kernel.Plugin {
	return plugin.Define(plugin.Spec[Config]{
		ID:     ID,
		Inject: []kernel.Dep{kernel.Need(toy.StoreKey), kernel.Need(spine.Key), kernel.Need(kernel.LoggerKey)},
		Validate: func(cf *Config) error {
			if cf.ApplyDelay < 0 || cf.CallEvery < 0 || cf.CallHold < 0 {
				return errors.New("applyDelay, callEvery and callHold must not be negative")
			}
			return nil
		},
		Apply: func(c *kernel.Context, cf *Config) error {
			_ = toy.StoreKey.Of(c)
			sp := spine.Key.Of(c)
			if !def.Wait(cf.ApplyDelay, c.Std().Done()) {
				return c.Std().Err()
			}
			if cf.CallEvery > 0 {
				c.Go("toy-consumer-load", func(ctx context.Context) error {
					for {
						_ = sp.Call(cf.CallHold) // what the spine saw of the call is its own record
						if !def.Wait(cf.CallEvery, ctx.Done()) {
							return nil
						}
					}
				})
			}
			kernel.LoggerKey.Of(c).For(c).Info("toy-consumer applied", "note", cf.Note.Get())
			return nil
		},
	})
}
