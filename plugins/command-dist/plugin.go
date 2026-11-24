// SPDX-License-Identifier: Apache-2.0

// Package commanddist is the row command-dist of aiops-cli: the commands dist build,
// dist sign and dist use (ADR-061 decision 30; 08-profiles-roadmap-adrs.md:402). dist
// sign and dist use call the methods SignDist and UseDist of the operations library,
// on the local form of an image, and print what was written and the outcome (ADR-061
// decisions 16 and 20; ADR-020 decision 12). dist build is declared with its synopsis
// and flags, so the module table and the name lint hold it, and answers that it is not
// built yet: it runs through the builder of the toolbox (ADR-044).
package commanddist

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
const ID = "aiops/command-dist"

// Config is the config of the row, which has none.
type Config struct{}

// Plugin is the row.
var Plugin = plugin.Define(plugin.Spec[Config]{
	ID:        ID,
	Inject:    []kernel.Dep{kernel.Need(commands.Key), kernel.Need(operations.Key)},
	Registers: []kernel.AnyKey{commands.Key},
	Apply: func(c *kernel.Context, _ *Config) error {
		r, ops := commands.Key.Of(c), operations.Key.Of(c)
		specs := specs()
		for i, run := range []func(context.Context, operations.Operations, commands.Invocation) error{notBuilt, sign, use} {
			if err := r.Register(c, specs[i], command(func(ctx context.Context, inv commands.Invocation) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				err := run(ctx, ops, inv)
				if errors.Is(err, operations.ErrUsage) {
					return fmt.Errorf("%w: %w", commands.ErrUsage, err)
				}
				return err
			})); err != nil {
				return err
			}
		}
		return nil
	},
})

func specs() []commands.Spec {
	// The image is the builder of a host with no toolchain (08-profiles-roadmap-adrs.md:402).
	builder := commands.Flag{Name: "builder", Kind: commands.String, Value: "image|local", Default: "image", Usage: "the builder: the builder image, or the local toolchain"}
	return []commands.Spec{
		{Path: []string{"dist", "build"}, Summary: "Build the site's distribution, its image and its aiopsctl from dist.yaml.",
			Args: []string{"<dist.yaml>"}, Min: 1, Max: 1, Flags: []commands.Flag{
				{Name: "modcache", Kind: commands.String, Value: "<dir>", Usage: "the module cache the build reads"},
				{Name: "mirror", Kind: commands.List, Value: "<prefix>=<site prefix>", Usage: "read the modules under a prefix from the site's mirror"},
				{Name: "allow-bump", Kind: commands.List, Value: "<module>", Usage: "allow the version of a module above the release's pin"},
				builder,
			}},
		{Path: []string{"dist", "sign"}, Summary: "Sign the distribution manifest of an image with the site's key.",
			Args: []string{"<image>"}, Min: 1, Max: 1, Flags: []commands.Flag{
				{Name: "key", Kind: commands.String, Value: "<file>", Required: true, Usage: "the private key file"},
			}},
		{Path: []string{"dist", "use"}, Summary: "Verify the distribution of an image and record it as the one the home runs.",
			Args: []string{"<image>"}, Min: 1, Max: 1},
	}
}

type command func(ctx context.Context, inv commands.Invocation) error

func (f command) Run(ctx context.Context, inv commands.Invocation) error { return f(ctx, inv) }

// abs resolves p against the working directory of the invocation.
func abs(inv commands.Invocation, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(inv.Dir, p)
}

func caller(inv commands.Invocation) operations.Caller {
	return operations.Caller{Env: inv.Env, Progress: inv.Err}
}

// notBuilt is dist build, whose builder lands with ADR-044.
func notBuilt(context.Context, operations.Operations, commands.Invocation) error {
	return errors.New("dist build is not built yet: it runs through the builder of ADR-044")
}

// sign is dist sign: the bundle of the image's distribution manifest, signed with the
// private key file, written beside the manifest.
func sign(ctx context.Context, ops operations.Operations, inv commands.Invocation) error {
	w, err := ops.SignDist(ctx, operations.SignDistRequest{Caller: caller(inv), Image: abs(inv, inv.Args[0]), Key: abs(inv, inv.Value("key"))})
	if err != nil {
		return err
	}
	for _, p := range w.Paths {
		fmt.Fprintln(inv.Err, "wrote "+p)
	}
	return nil
}

// use is dist use: the image's distribution verified with the keys of distributions in
// trust.yaml, then its record and verified/current written into the home.
func use(ctx context.Context, ops operations.Operations, inv commands.Invocation) error {
	ch, err := ops.UseDist(ctx, operations.UseRequest{Caller: caller(inv), Home: inv.Home, Image: abs(inv, inv.Args[0])})
	for _, n := range ch.Notes {
		fmt.Fprintln(inv.Err, n)
	}
	if err != nil {
		return err
	}
	for _, p := range ch.Written {
		fmt.Fprintln(inv.Err, "wrote "+p)
	}
	fmt.Fprintf(inv.Err, "%s: %s\n", ch.Outcome, ch.Reason)
	return nil
}
