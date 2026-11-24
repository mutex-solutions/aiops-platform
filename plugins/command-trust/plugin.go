// SPDX-License-Identifier: Apache-2.0

// Package commandtrust is the row command-trust of aiops-cli (ADR-061 decision 30). Not
// built yet: it registers nothing.
package commandtrust

import (
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
	Apply:     func(*kernel.Context, *Config) error { return nil },
})
