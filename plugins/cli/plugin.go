// SPDX-License-Identifier: Apache-2.0

// Package cli is the row cli of aiops-cli: the dispatcher of the commands registry and
// the startup row of aiopsctl that parses the leftover argv (ADR-061 decisions 7 to 11).
// Not built yet: it provides an empty registry and dispatches nothing.
package cli

import (
	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

// ID is the plugin id.
const ID = "aiops/cli"

// Config is the config of the row, which has none.
type Config struct{}

// Plugin is the cli row.
var Plugin = plugin.Define(plugin.Spec[Config]{
	ID: ID,
	Inject: []kernel.Dep{kernel.Need(launch.CmdlineKey), kernel.Need(launch.ReadyKey), kernel.Need(launch.AppExitKey),
		kernel.Need(composition.Key), kernel.Need(launch.CatalogKey)},
	Provides: []kernel.AnyKey{commands.Key},
	Apply: func(c *kernel.Context, _ *Config) error {
		return commands.Key.Provide(c, struct{ commands.Registry }{commands.NewTable(c, commands.Environment{})})
	},
})
