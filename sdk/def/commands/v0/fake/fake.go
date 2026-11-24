// SPDX-License-Identifier: Apache-2.0

// Package fake is the recording dispatcher of the commands definition
// (07-c4-plugin-sdk.md:494; ADR-061 decision 4). Not built yet: it provides a registry
// that refuses every call.
package fake

import (
	"context"

	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Registry is a recording commands.Registry.
type Registry struct {
	Dir     string
	Env     map[string]string
	Home    string
	Pending []commands.Pending
}

var _ commands.Registry = (*Registry)(nil)

// New returns an empty registry and the plugin that provides it under commands.Key.
func New() (*Registry, kernel.Plugin) {
	f := &Registry{}
	return f, kernel.Define(kernel.Plugin{ID: "aiops/commands-fake", Provides: []kernel.AnyKey{commands.Key},
		Apply: func(c *kernel.Context, _ any) error {
			return commands.Key.Provide(c, struct{ commands.Registry }{commands.NewTable(c, commands.Environment{})})
		}})
}

// Register refuses.
func (*Registry) Register(*kernel.Context, commands.Spec, commands.Command) error {
	return kernel.ErrInactive
}

// Dispatch refuses.
func (*Registry) Dispatch(context.Context, []string, commands.Streams) (int, error) {
	return 1, kernel.ErrInactive
}

// Inventory lists nothing.
func (*Registry) Inventory() []string { return nil }

// Calls returns nothing.
func (*Registry) Calls() [][]string { return nil }

// Spec finds nothing.
func (*Registry) Spec(string) (commands.Spec, bool) { return commands.Spec{}, false }
