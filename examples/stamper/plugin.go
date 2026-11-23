// SPDX-License-Identifier: Apache-2.0

// Package stamper is example-stamper, the toy stamper of the S2a test release: it
// contributes to stampers and nothing else, so the toy caller has a stamper to call
// (ADR-060 decision 37). Its stamp is its name and the CRC-32 of the body. It is in
// no shipped bundle.
//
// Its config is Config, decoded by the rules of sdk/plugin.
package stamper

import (
	"context"
	"errors"
	"fmt"
	"hash/crc32"

	stampers "github.com/mutex-solutions/aiops-platform/sdk/def/stampers/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

// ID is the plugin id.
const ID = "aiops/example-stamper"

// Config is the row config.
type Config struct {
	// Name is the name the stamper registers under, not empty.
	Name string `default:"example" example:"seal"`
}

// Plugin is the stamper.
var Plugin = plugin.Define(plugin.Spec[Config]{
	ID:     ID,
	Inject: []kernel.Dep{kernel.Need(stampers.Key)},
	Validate: func(cfg *Config) error {
		if cfg.Name == "" {
			return errors.New("name is empty")
		}
		return nil
	},
	Apply: func(c *kernel.Context, cfg *Config) error {
		return stampers.Key.Of(c).Register(c, cfg.Name, stamp{c: c, name: cfg.Name})
	},
})

// stamp stamps a body while its fiber is live.
type stamp struct {
	c    *kernel.Context
	name string
}

func (s stamp) Stamp(ctx context.Context, body string) (string, error) {
	leave, ok := s.c.Enter()
	if !ok {
		return "", kernel.ErrInactive
	}
	defer leave()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%08x", s.name, crc32.ChecksumIEEE([]byte(body))), nil
}
