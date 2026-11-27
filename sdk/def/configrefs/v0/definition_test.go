// SPDX-License-Identifier: Apache-2.0

package configrefs_test

import (
	"testing"

	configrefs "github.com/mutex-solutions/aiops-platform/sdk/def/configrefs/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// definition.yaml and the Go declarations agree (ADR-060 decision 7).
func TestDefinitionFile(t *testing.T) {
	deftest.Agree(t, deftest.Decl{Keys: []kernel.AnyKey{configrefs.Key}})
}
