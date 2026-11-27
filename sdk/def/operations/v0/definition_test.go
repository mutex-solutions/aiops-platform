// SPDX-License-Identifier: Apache-2.0

package operations_test

import (
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	operations "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// definition.yaml and the Go declarations agree (ADR-060 decision 7).
func TestDefinitionFile(t *testing.T) {
	deftest.Agree(t, deftest.Decl{
		Keys:    []kernel.AnyKey{operations.Key},
		Provide: func(c *kernel.Context) error { return operations.Key.Provide(c, nil) },
	})
}
