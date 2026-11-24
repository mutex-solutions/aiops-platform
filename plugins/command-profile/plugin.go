// SPDX-License-Identifier: Apache-2.0

// Package commandprofile is the row command-profile of aiops-cli: the commands profile
// render, profile verify, profile init and profile sanitize (ADR-061 decisions 27, 30
// and 41). Not built yet: it registers nothing.
package commandprofile

import (
	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

// ID is the plugin id.
const ID = "aiops/command-profile"

// Config is the config of the row, which has none.
type Config struct{}

// Plugin is the row.
var Plugin = plugin.Define(plugin.Spec[Config]{
	ID:        ID,
	Inject:    []kernel.Dep{kernel.Need(commands.Key), kernel.Need(launch.ProfileContextKey)},
	Registers: []kernel.AnyKey{commands.Key},
	Apply:     func(*kernel.Context, *Config) error { return nil },
})
