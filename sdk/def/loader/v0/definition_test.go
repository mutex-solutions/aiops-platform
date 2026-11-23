// SPDX-License-Identifier: Apache-2.0

package loader_test

import (
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	loader "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// definition.yaml and the Go declarations agree (ADR-060 decision 7), with the three
// sealed hooks of the loader.
func TestDefinitionFile(t *testing.T) {
	deftest.Agree(t, deftest.Decl{
		Keys:    []kernel.AnyKey{loader.Key},
		Hooks:   []deftest.Hook{deftest.Waterfall(loader.PatchContext), deftest.Event(loader.EntryInit), deftest.Event(loader.VolatileUpdate)},
		Provide: func(c *kernel.Context) error { return loader.Key.Provide(c, nil) },
	})
}
