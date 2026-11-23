// SPDX-License-Identifier: Apache-2.0

// Package fake is the recording dispatcher of the echoers definition.
package fake

import (
	"context"
	"errors"

	echoers "github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

var errTODO = errors.New("fake: not implemented")

// Registry is the recording registry.
type Registry struct{}

// Call is one recorded call of Echo.
type Call struct{ Name, Msg string }

// New returns the registry and the plugin that provides it.
func New() (*Registry, kernel.Plugin) {
	f := &Registry{}
	return f, kernel.Define(kernel.Plugin{ID: "aiops/echoers-fake", Provides: []kernel.AnyKey{echoers.Key},
		Apply: func(c *kernel.Context, _ any) error { return echoers.Key.Provide(c, f) }})
}

func (f *Registry) Register(*kernel.Context, string, echoers.Echoer) error { return errTODO }

func (f *Registry) Echo(context.Context, string, string) (string, error) { return "", errTODO }

func (f *Registry) Inventory() []string { return nil }

func (f *Registry) Calls() []Call { return nil }

func (f *Registry) Contribution(string) (echoers.Echoer, bool) { return nil, false }
