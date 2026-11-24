// SPDX-License-Identifier: Apache-2.0

// Package commandkey is the row command-key of aiops-cli: the command key generate,
// which writes a signing key pair (ADR-061 decision 30; ADR-020 decision 21). The
// command is declared with its synopsis and flags, so the module table and the name
// lint hold it, and answers that it is not built yet: it writes the pair through the
// method GenerateKey of the operations library, which lands with that library
// (ADR-061 decision 16).
package commandkey

import (
	"context"
	"fmt"

	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

// ID is the plugin id.
const ID = "aiops/command-key"

// Config is the config of the row, which has none.
type Config struct{}

// Plugin is the row.
var Plugin = plugin.Define(plugin.Spec[Config]{
	ID:        ID,
	Inject:    []kernel.Dep{kernel.Need(commands.Key)},
	Registers: []kernel.AnyKey{commands.Key},
	Apply: func(c *kernel.Context, _ *Config) error {
		r := commands.Key.Of(c)
		for _, s := range specs() {
			if err := r.Register(c, s, notBuilt(s.Name())); err != nil {
				return err
			}
		}
		return nil
	},
})

func specs() []commands.Spec {
	return []commands.Spec{
		{Path: []string{"key", "generate"}, Summary: "Write a signing key pair, <prefix>.key and <prefix>.pub; the password of the private key is AIOPS_KEY_PASSWORD.",
			Args: []string{"<prefix>"}, Min: 1, Max: 1},
	}
}

// notBuilt is a command whose operation lands with the operations library.
type notBuilt string

func (n notBuilt) Run(ctx context.Context, _ commands.Invocation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("%s is not built yet: it arrives with the operations library (ADR-061 decision 16)", string(n))
}
