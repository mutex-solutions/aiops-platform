// SPDX-License-Identifier: Apache-2.0

package stampers_test

import (
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	stampers "github.com/mutex-solutions/aiops-platform/sdk/def/stampers/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// definition.yaml and the Go declarations agree (ADR-060 decision 7).
func TestDefinitionFile(t *testing.T) {
	deftest.Agree(t, deftest.Decl{Keys: []kernel.AnyKey{stampers.Key}})
}
