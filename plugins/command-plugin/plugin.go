// SPDX-License-Identifier: Apache-2.0

// Package commandplugin is the row command-plugin of aiops-cli: the commands of the
// authoring path and of the plugins of a profile, plugin new, check, smoke, pack,
// sign, push, inspect, verify, gate, install, enable, disable and remove (ADR-061
// decision 30; 07-c4-plugin-sdk.md:1565, :1578; 08-profiles-roadmap-adrs.md:373-375).
// The commands are declared with their synopses and flags, so the module table and the
// name lint hold them, and answer that they are not built yet: each runs through one
// method of the operations library, which lands with that library (ADR-061 decision
// 16). plugin purge joins at S3 (ADR-060 decision 47).
package commandplugin

import (
	"context"
	"fmt"

	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

// ID is the plugin id.
const ID = "aiops/command-plugin"

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

func spec(path, summary string, args []string, min, max int, flags ...commands.Flag) commands.Spec {
	return commands.Spec{Path: []string{"plugin", path}, Summary: summary, Args: args, Min: min, Max: max, Flags: flags}
}

func required(f commands.Flag) commands.Flag {
	f.Required = true
	return f
}

func specs() []commands.Spec {
	// The flags that several commands share.
	builder := commands.Flag{Name: "builder", Kind: commands.String, Value: "image|local", Default: "image", Usage: "the builder: the builder image, or the local toolchain"}
	out := commands.Flag{Name: "out", Kind: commands.String, Value: "<dir>", Usage: "the directory of the artifact's layout"}
	profile := commands.Flag{Name: "profile", Kind: commands.String, Value: "<p>", Usage: "the profile to change (default: the one that run/status.json of the home names)"}
	against := commands.Flag{Name: "against", Kind: commands.String, Value: "<dist.yaml>", Usage: "the published signed manifest of the release to check against"}
	return []commands.Spec{
		spec("new", "Write a new plugin from the template of a registry, or a connector pack of a release.", []string{"<registry>", "<id>"}, 2, 2,
			commands.Flag{Name: "lang", Kind: commands.String, Value: "go", Usage: "the language of the plugin"},
			commands.Flag{Name: "contrib", Kind: commands.Bool, Usage: "write the plugin under the owner contrib"},
			against, commands.Flag{Name: "plugin", Kind: commands.List, Value: "<id>", Usage: "an instantiable plugin of the release that a connector pack rows"}),
		spec("check", "Run the eight steps of the plugin check on a plugin directory.", []string{"[<dir>]"}, 0, 1,
			commands.Flag{Name: "write", Kind: commands.Bool, Usage: "write the generated half of plugin.yaml and the generated tests"},
			commands.Flag{Name: "json", Kind: commands.Bool, Usage: "print the report as JSON"}, against),
		spec("smoke", "Build a throwaway distribution with the plugin and boot its test-only profile.", []string{"<dir>"}, 1, 1,
			required(commands.Flag{Name: "against", Kind: commands.String, Value: "<release>", Usage: "the release to build against"}), builder),
		spec("pack", "Pack a plugin directory into the layout of an artifact.", []string{"[<dir>]"}, 0, 1, out),
		spec("sign", "Sign the packed artifact and its attestation.", nil, 0, 0,
			required(commands.Flag{Name: "key", Kind: commands.String, Value: "<file>", Usage: "the private key file"}), out),
		spec("push", "Push the signed artifact to a registry.", []string{"<registry>"}, 1, 1, out),
		spec("inspect", "Show what a spec names, fetching only its manifest and its bundle.", []string{"<spec>"}, 1, 1),
		spec("verify", "Verify the signer of an artifact and run its suites.", []string{"<spec>"}, 1, 1, builder),
		spec("gate", "Run the clauses of the gate on an artifact against a published release.", []string{"<spec>"}, 1, 1, required(against)),
		spec("install", "Install an artifact into a profile of the home.", []string{"<spec>"}, 1, 1, profile),
		spec("enable", "Enable a row in a profile of the home.", []string{"<row>"}, 1, 1, profile),
		spec("disable", "Disable a row in a profile of the home.", []string{"<row>"}, 1, 1, profile),
		spec("remove", "Remove an installed plugin from a profile of the home.", []string{"<id>"}, 1, 1, profile),
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
