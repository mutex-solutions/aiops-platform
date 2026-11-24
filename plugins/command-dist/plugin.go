// SPDX-License-Identifier: Apache-2.0

// Package commanddist is the row command-dist of aiops-cli: the commands dist build,
// dist sign and dist use (ADR-061 decision 30; 08-profiles-roadmap-adrs.md:402). The
// commands are declared with their synopses and flags, so the module table and the
// name lint hold them, and answer that they are not built yet: they run through the
// methods Build, SignDist and UseDist of the operations library, which land with that
// library (ADR-061 decision 16).
package commanddist

import (
	"context"
	"fmt"

	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
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

// notBuilt is a command whose operation lands with the operations library.
type notBuilt string

func (n notBuilt) Run(ctx context.Context, _ commands.Invocation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("%s is not built yet: it arrives with the operations library (ADR-061 decision 16)", string(n))
}
