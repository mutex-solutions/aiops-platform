// SPDX-License-Identifier: Apache-2.0

// Package toy holds the toy plugins of the test distribution: a provider, a consumer
// and a heavy spine provider, each with a tiny typed key from test/dist/def. They
// stand for the infrastructure providers and policy services of a real profile, so
// that the launcher, the loader and the kernel are exercised with a realistic
// dependency chain: spine <- provider <- consumer, and spine <- consumer.
//
// Every delay is config data, never a default sleep: a row with no config applies
// and disposes at once.
package toy

import (
	"context"
	"time"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/test/dist/def"
)

// The plugin ids of the toys.
const (
	ProviderID = "aiops-test/toy-provider"
	ConsumerID = "aiops-test/toy-consumer"
	SpineID    = "aiops-test/toy-spine"
)

// Provider provides toy.store and the provider-bound toy.policy. It needs the spine.
// Config: applyDelay (duration, default 0).
func Provider() kernel.Plugin {
	type cfg struct{ applyDelay time.Duration }
	return kernel.Define(kernel.Plugin{
		ID:       ProviderID,
		Inject:   []kernel.Dep{kernel.Need(def.SpineKey)},
		Provides: []kernel.AnyKey{def.StoreKey, def.PolicyKey},
		Decode: func(raw any) (any, error) {
			c := def.Parse(raw)
			out := cfg{applyDelay: c.Duration("applyDelay", 0)}
			return out, c.Err()
		},
		Apply: func(c *kernel.Context, raw any) error {
			_ = def.SpineKey.Of(c)
			if !def.Wait(raw.(cfg).applyDelay, c.Std().Done()) {
				return c.Std().Err()
			}
			if err := def.StoreKey.Provide(c, def.Store{Owner: ProviderID}); err != nil {
				return err
			}
			return def.PolicyKey.Provide(c, def.Policy{Owner: ProviderID})
		},
	})
}

// Consumer needs toy.store and toy.spine and provides nothing, so a profile may hold
// several. Config: note (string, the volatile field of the plugin) and applyDelay
// (duration, default 0).
func Consumer() kernel.Plugin {
	type cfg struct {
		note       string
		applyDelay time.Duration
	}
	return kernel.Define(kernel.Plugin{
		ID:     ConsumerID,
		Inject: []kernel.Dep{kernel.Need(def.StoreKey), kernel.Need(def.SpineKey)},
		Decode: func(raw any) (any, error) {
			c := def.Parse(raw)
			out := cfg{note: c.String("note", ""), applyDelay: c.Duration("applyDelay", 0)}
			return out, c.Err()
		},
		Apply: func(c *kernel.Context, raw any) error {
			_ = def.StoreKey.Of(c)
			_ = def.SpineKey.Of(c)
			if !def.Wait(raw.(cfg).applyDelay, c.Std().Done()) {
				return c.Std().Err()
			}
			return nil
		},
	})
}

// Spine is the heavy spine provider: it provides toy.spine, which the provider and
// the consumers need, holds a number of tracked workers that end only when their
// context does, and has a slow but bounded unload.
//
// Config: applyDelay (default 0), workers (whole number, default 2), drainDelay (how
// long each worker takes to end once its context is cancelled, default 0) and
// disposeDelay (how long its disposer runs, default 0). A delay that is longer than
// the matching deadline of the kernel is cut at the deadline by the context it waits
// on: the spine is slow, never a leak. The leaks are the fault plugins.
func Spine() kernel.Plugin {
	type cfg struct {
		applyDelay, drainDelay, disposeDelay time.Duration
		workers                              int
	}
	return kernel.Define(kernel.Plugin{
		ID:       SpineID,
		Provides: []kernel.AnyKey{def.SpineKey},
		Decode: func(raw any) (any, error) {
			c := def.Parse(raw)
			out := cfg{applyDelay: c.Duration("applyDelay", 0), drainDelay: c.Duration("drainDelay", 0),
				disposeDelay: c.Duration("disposeDelay", 0), workers: c.Int("workers", 2)}
			return out, c.Err()
		},
		Apply: func(c *kernel.Context, raw any) error {
			cf := raw.(cfg)
			if !def.Wait(cf.applyDelay, c.Std().Done()) {
				return c.Std().Err()
			}
			for i := range cf.workers {
				c.Go("spine-worker-"+string(rune('a'+i%26)), func(ctx context.Context) error {
					<-ctx.Done()
					def.Wait(cf.drainDelay, nil)
					return nil
				})
			}
			c.Defer("spine-dispose", func(ctx context.Context) error {
				def.Wait(cf.disposeDelay, ctx.Done())
				return nil
			})
			return def.SpineKey.Provide(c, def.Spine{Workers: cf.workers})
		},
	})
}
