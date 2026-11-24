// SPDX-License-Identifier: Apache-2.0

// Package commandtrust is the row command-trust of aiops-cli: the command trust add, in
// both of its forms (ADR-061 decision 30; ADR-020 decisions 5 and 6). The command is
// declared with its synopsis and flags, so the module table and the name lint hold it,
// and answers that it is not built yet: it writes trust.yaml through the method
// AddTrust of the operations library, which lands with that library (ADR-061 decision
// 16).
package commandtrust

import (
	"context"
	"fmt"

	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

// ID is the plugin id.
const ID = "aiops/command-trust"

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
		{Path: []string{"trust", "add"}, Summary: "Trust a public key: for the plugins that --ids names, or with --distribution as a root of distributions.",
			Args: []string{"<public key file>"}, Min: 1, Max: 1, Flags: []commands.Flag{
				{Name: "ids", Kind: commands.String, Value: "<pattern>[,...]", Usage: "the plugin ids the key signs: exact ids or <owner>/*"},
				{Name: "runtimes", Kind: commands.String, Value: "<runtime>[,...]", Usage: "the runtimes the key signs, inproc or remote; none for packs only"},
				{Name: "signed", Kind: commands.String, Value: "<key or hook>[,...]", Usage: "the signed keys and hooks the key may serve"},
				{Name: "distribution", Kind: commands.Bool, Usage: "trust the key as a root of distributions instead"},
			}},
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
