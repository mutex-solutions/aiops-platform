// SPDX-License-Identifier: Apache-2.0

package launch_test

import (
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// definition.yaml and the Go declarations agree (ADR-060 decision 7): the six launch
// keys, one class each.
func TestDefinitionFile(t *testing.T) {
	deftest.Agree(t, deftest.Decl{Keys: []kernel.AnyKey{launch.ProfileContextKey, launch.CatalogKey,
		launch.CmdlineKey, launch.ReadyKey, launch.KillSwitchesKey, launch.AppExitKey}})
}
