// SPDX-License-Identifier: Apache-2.0

// Package fake is the recording dispatcher of the stampers definition.
package fake

import (
	"context"
	"errors"

	stampers "github.com/mutex-solutions/aiops-platform/sdk/def/stampers/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

var errTODO = errors.New("fake: not implemented")

// Registry is the recording registry.
type Registry struct{}

// Call is one recorded call of Stamp.
type Call struct{ Name, Body string }

// New returns the registry and the plugin that provides it.
func New() (*Registry, kernel.Plugin) {
	f := &Registry{}
	return f, kernel.Define(kernel.Plugin{ID: "aiops/stampers-fake", Provides: []kernel.AnyKey{stampers.Key},
		Apply: func(c *kernel.Context, _ any) error { return stampers.Key.Provide(c, f) }})
}

func (f *Registry) Register(*kernel.Context, string, stampers.Stamper) error { return errTODO }

func (f *Registry) Stamp(context.Context, string, string) (string, error) { return "", errTODO }

func (f *Registry) Inventory() []string { return nil }

func (f *Registry) Calls() []Call { return nil }

func (f *Registry) Contribution(string) (stampers.Stamper, bool) { return nil, false }
