// SPDX-License-Identifier: Apache-2.0

// Package fake provides toy.spine with a spine whose calls return at once, for the
// generated cycle test of a plugin that injects it (ADR-060 decision 12).
package fake

import (
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	spine "github.com/mutex-solutions/aiops-platform/test/dist/def/spine/v0"
)

// Spine is the fake provider of toy.spine.
type Spine struct{}

// New returns the fake and the plugin that provides toy.spine. Each call returns a
// new plugin value.
func New() (*Spine, kernel.Plugin) {
	return &Spine{}, kernel.Define(kernel.Plugin{
		ID:       "aiops-test/spine-fake",
		Provides: []kernel.AnyKey{spine.Key},
		Apply:    func(c *kernel.Context, _ any) error { return spine.Key.Provide(c, spine.New(0, nil)) },
	})
}

// Inventory is empty: a caller of the spine leaves nothing behind.
func (*Spine) Inventory() []string { return nil }
