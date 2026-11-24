// SPDX-License-Identifier: Apache-2.0

// Package commandkey is the row command-key of aiops-cli: the command key generate,
// which writes a signing key pair through the method GenerateKey of the operations
// library (ADR-061 decisions 16, 20 and 30; ADR-020 decisions 4 and 21). It parses
// nothing itself: it resolves the prefix against the working directory, passes the
// environment of the invocation, from which the library reads the key password, and
// prints what was written.
package commandkey

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	operations "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
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
	Inject:    []kernel.Dep{kernel.Need(commands.Key), kernel.Need(operations.Key)},
	Registers: []kernel.AnyKey{commands.Key},
	Apply: func(c *kernel.Context, _ *Config) error {
		return commands.Key.Of(c).Register(c, spec(), generate{operations.Key.Of(c)})
	},
})

func spec() commands.Spec {
	return commands.Spec{Path: []string{"key", "generate"}, Summary: "Write a signing key pair, <prefix>.key and <prefix>.pub; the password of the private key is AIOPS_KEY_PASSWORD.",
		Args: []string{"<prefix>"}, Min: 1, Max: 1}
}

// generate is key generate.
type generate struct{ ops operations.Operations }

func (g generate) Run(ctx context.Context, inv commands.Invocation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	prefix := inv.Args[0]
	if !filepath.IsAbs(prefix) {
		prefix = filepath.Join(inv.Dir, prefix)
	}
	w, err := g.ops.GenerateKey(ctx, operations.KeyRequest{Caller: operations.Caller{Env: inv.Env, Progress: inv.Err}, Prefix: prefix})
	if errors.Is(err, operations.ErrUsage) {
		return fmt.Errorf("%w: %w", commands.ErrUsage, err)
	}
	if err != nil {
		return err
	}
	for _, p := range w.Paths {
		fmt.Fprintln(inv.Err, "wrote "+p)
	}
	return nil
}
