// SPDX-License-Identifier: Apache-2.0

// Package commandtrust is the row command-trust of aiops-cli: the command trust add, in
// both of its forms, which writes trust.yaml of the home through the method AddTrust of
// the operations library (ADR-061 decisions 16, 17, 20 and 30; ADR-020 decisions 5 and
// 6). It parses nothing itself: it resolves the key file against the working
// directory, splits each comma-separated list, passes the home and the environment of
// the invocation, and prints what was written and the outcome.
package commandtrust

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	operations "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
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
	Inject:    []kernel.Dep{kernel.Need(commands.Key), kernel.Need(operations.Key)},
	Registers: []kernel.AnyKey{commands.Key},
	Apply: func(c *kernel.Context, _ *Config) error {
		return commands.Key.Of(c).Register(c, spec(), add{operations.Key.Of(c)})
	},
})

func spec() commands.Spec {
	return commands.Spec{Path: []string{"trust", "add"}, Summary: "Trust a public key: for the plugins that --ids names, or with --distribution as a root of distributions.",
		Args: []string{"<public key file>"}, Min: 1, Max: 1, Flags: []commands.Flag{
			{Name: "ids", Kind: commands.String, Value: "<pattern>[,...]", Usage: "the plugin ids the key signs: exact ids or <owner>/*"},
			{Name: "runtimes", Kind: commands.String, Value: "<runtime>[,...]", Usage: "the runtimes the key signs, inproc or remote; none for packs only"},
			{Name: "signed", Kind: commands.String, Value: "<key or hook>[,...]", Usage: "the signed keys and hooks the key may serve"},
			{Name: "distribution", Kind: commands.Bool, Usage: "trust the key as a root of distributions instead"},
		}}
}

// add is trust add.
type add struct{ ops operations.Operations }

func (a add) Run(ctx context.Context, inv commands.Invocation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key := inv.Args[0]
	if !filepath.IsAbs(key) {
		key = filepath.Join(inv.Dir, key)
	}
	list := func(flag string) []string {
		if v := inv.Value(flag); v != "" {
			return strings.Split(v, ",")
		}
		return nil
	}
	ch, err := a.ops.AddTrust(ctx, operations.TrustRequest{Caller: operations.Caller{Env: inv.Env, Progress: inv.Err}, Home: inv.Home, KeyFile: key,
		IDs: list("ids"), Runtimes: list("runtimes"), Signed: list("signed"), Distribution: inv.Bool("distribution")})
	if errors.Is(err, operations.ErrUsage) {
		return fmt.Errorf("%w: %w", commands.ErrUsage, err)
	}
	if err != nil {
		return err
	}
	for _, n := range ch.Notes {
		fmt.Fprintln(inv.Err, n)
	}
	for _, p := range ch.Written {
		fmt.Fprintln(inv.Err, "wrote "+p)
	}
	if ch.Outcome == operations.NotRunning {
		fmt.Fprintln(inv.Err, "not-running: the change applies at the next start")
		return nil
	}
	fmt.Fprintf(inv.Err, "%s: %s\n", ch.Outcome, ch.Reason)
	return nil
}
