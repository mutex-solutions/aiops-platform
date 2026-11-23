// SPDX-License-Identifier: Apache-2.0

package readiness_test

import (
	"maps"
	"slices"
	"testing"

	readiness "github.com/mutex-solutions/aiops-platform/plugins/readiness"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

// The row is written with plugin.Define, so plugingen derives its configSchema from
// its config type (ADR-060 decision 44; ADR-048 decision 9).
func TestDefinedWithPluginDefine(t *testing.T) {
	g, err := plugin.Generate(readiness.New())
	if err != nil {
		t.Fatal(err)
	}
	props, _ := g.ConfigSchema["properties"].(map[string]any)
	if g.ConfigSchema["type"] != "object" || !slices.Equal(slices.Sorted(maps.Keys(props)), slices.Sorted(slices.Values([]string{}))) {
		t.Fatalf("configSchema = %v", g.ConfigSchema)
	}
}
