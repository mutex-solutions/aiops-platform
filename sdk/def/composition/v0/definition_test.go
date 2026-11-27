// SPDX-License-Identifier: Apache-2.0

package composition_test

import (
	"testing"

	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// definition.yaml and the Go declarations agree (ADR-060 decision 7). The definition
// owns no hook: NewApplied declares composition/applied for the reload definition,
// whose key owns it.
func TestDefinitionFile(t *testing.T) {
	deftest.Agree(t, deftest.Decl{Keys: []kernel.AnyKey{composition.Key}})
}
