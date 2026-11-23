// SPDX-License-Identifier: Apache-2.0

package plugin_test

import (
	"strings"
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

type secretConfig struct {
	Count int
	On    bool
	Code  string `example:"aaa"`
	Tags  []string
}

// A string of a config tree may come from a resolved reference such as
// ${secret:name}, which is resolved before Decode runs, so an error names the field
// and what it wants and never echoes a string value, as the config-refs row never
// did (ADR-060 decision 44).
func TestAnErrorNeverEchoesAStringValue(t *testing.T) {
	p := plugin.Define(plugin.Spec[secretConfig]{ID: "test/secret", Apply: func(*kernel.Context, *secretConfig) error { return nil }})
	for _, tree := range []any{
		"hunter2",
		map[string]any{"count": "hunter2"},
		map[string]any{"on": "hunter2"},
		map[string]any{"tags": "hunter2"},
		map[string]any{"tags": []any{"ok", int64(1)}, "count": "hunter2"},
	} {
		_, err := p.Decode(tree)
		if err == nil || strings.Contains(err.Error(), "hunter2") || !strings.Contains(err.Error(), "a string") {
			t.Errorf("Decode(%v) = %v", tree, err)
		}
	}
	v, err := plugin.NewValidator([]byte(`{"type": "string", "pattern": "^a+$"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Validate("hunter2"); err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("Validate = %v", err)
	}
}
