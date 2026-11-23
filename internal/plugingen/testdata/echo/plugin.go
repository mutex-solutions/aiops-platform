// SPDX-License-Identifier: Apache-2.0

// Package echo is a plugin that plugingen's tests generate from: an echoer written
// with plugin.Define.
package echo

import (
	"context"

	echoers "github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

// Config is the row config.
type Config struct {
	Name string `default:"echo"`
}

// Plugin registers one echoer.
var Plugin = plugin.Define(plugin.Spec[Config]{
	ID:        "test/echo",
	Inject:    []kernel.Dep{kernel.Need(echoers.Key)},
	Registers: []kernel.AnyKey{echoers.Key},
	Apply: func(c *kernel.Context, cfg *Config) error {
		return echoers.Key.Of(c).Register(c, cfg.Name, echo{})
	},
})

type echo struct{}

func (echo) Echo(_ context.Context, msg string) (string, error) { return msg, nil }
