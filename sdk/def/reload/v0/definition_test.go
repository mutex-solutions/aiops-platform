// SPDX-License-Identifier: Apache-2.0

package reload_test

import (
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// definition.yaml and the Go declarations agree (ADR-060 decision 7), with the two
// events the reload key owns.
func TestDefinitionFile(t *testing.T) {
	deftest.Agree(t, deftest.Decl{
		Keys:    []kernel.AnyKey{reload.Key},
		Hooks:   []deftest.Hook{deftest.Event(reload.Applied), deftest.Event(reload.Operation)},
		Provide: func(c *kernel.Context) error { return reload.Key.Provide(c, nil) },
	})
}
