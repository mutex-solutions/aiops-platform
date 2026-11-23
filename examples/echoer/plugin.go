// SPDX-License-Identifier: Apache-2.0

// Package echoer is example-echoer, the toy instantiable echoer of the S2a test
// release: it contributes to echoers and nothing else (ADR-060 decision 38). Its row
// config fixes its reply, so a pack with no code instantiates it by rows
// (08-profiles-roadmap-adrs.md:391). It is in no shipped bundle.
//
// Its config is Config, decoded by the rules of sdk/plugin.
package echoer

import (
	"context"
	"errors"

	echoers "github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

// ID is the plugin id.
const ID = "aiops/example-echoer"

// Config is the row config. Its examples make the row of a connector pack
// (ADR-060 decision 24).
type Config struct {
	// Name is the name the echoer registers under, not empty.
	Name string `default:"example" example:"loud"`
	// Prefix is what the reply puts before the message; empty, it is "<name>: ".
	Prefix string `example:"LOUD "`
}

// Plugin is the echoer.
var Plugin = plugin.Define(plugin.Spec[Config]{
	ID:     ID,
	Inject: []kernel.Dep{kernel.Need(echoers.Key)},
	Validate: func(cfg *Config) error {
		if cfg.Name == "" {
			return errors.New("name is empty")
		}
		return nil
	},
	Apply: func(c *kernel.Context, cfg *Config) error {
		e := echo{c: c, prefix: cfg.Prefix}
		if e.prefix == "" {
			e.prefix = cfg.Name + ": "
		}
		return echoers.Key.Of(c).Register(c, cfg.Name, e)
	},
})

// echo answers prefix+msg while its fiber is live.
type echo struct {
	c      *kernel.Context
	prefix string
}

func (e echo) Echo(ctx context.Context, msg string) (string, error) {
	leave, ok := e.c.Enter()
	if !ok {
		return "", kernel.ErrInactive
	}
	defer leave()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return e.prefix + msg, nil
}
