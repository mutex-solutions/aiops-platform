// SPDX-License-Identifier: Apache-2.0

// Package toyprovider is the toy provider of the test distribution: it provides
// toy.store and the provider-bound toy.policy and needs the spine (toy-spine).
package toyprovider

import (
	"time"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/test/dist/def"
	spine "github.com/mutex-solutions/aiops-platform/test/dist/def/spine/v0"
	toy "github.com/mutex-solutions/aiops-platform/test/dist/def/toy/v0"
)

// ID is the plugin id.
const ID = "aiops-test/toy-provider"

// New returns the plugin. Config: applyDelay (duration, default 0).
func New() kernel.Plugin {
	type cfg struct{ applyDelay time.Duration }
	return kernel.Define(kernel.Plugin{
		ID:       ID,
		Inject:   []kernel.Dep{kernel.Need(spine.Key)},
		Provides: []kernel.AnyKey{toy.StoreKey, toy.PolicyKey},
		Decode: func(raw any) (any, error) {
			c := def.Parse(raw)
			out := cfg{applyDelay: c.Duration("applyDelay", 0)}
			return out, c.Err()
		},
		Apply: func(c *kernel.Context, raw any) error {
			_ = spine.Key.Of(c)
			if !def.Wait(raw.(cfg).applyDelay, c.Std().Done()) {
				return c.Std().Err()
			}
			if err := toy.StoreKey.Provide(c, toy.Store{Owner: ID}); err != nil {
				return err
			}
			return toy.PolicyKey.Provide(c, toy.Policy{Owner: ID})
		},
	})
}
