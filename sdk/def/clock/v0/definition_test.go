// SPDX-License-Identifier: Apache-2.0

package clock_test

import (
	"testing"

	clock "github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// definition.yaml and the Go declarations agree (ADR-060 decision 7).
func TestDefinitionFile(t *testing.T) {
	deftest.Agree(t, deftest.Decl{Keys: []kernel.AnyKey{clock.Key}})
}
