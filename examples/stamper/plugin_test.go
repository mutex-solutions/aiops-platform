// SPDX-License-Identifier: Apache-2.0

package stamper_test

import (
	"testing"

	"github.com/mutex-solutions/aiops-platform/examples/stamper"
	"github.com/mutex-solutions/aiops-platform/sdk/def/stampers/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/def/stampers/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// The stamper passes the obligations half.
func TestObligations(t *testing.T) {
	conformance.Run(t, func(t *testing.T) (conformance.Subject, *kernel.Root) {
		root := kerneltest.New(t)
		return conformance.Contribution(root.Plugin(stamper.Plugin, map[string]any{"name": "seal"})), root
	})
}

// The stamp is the row's name and the CRC-32 of the body.
func TestStamp(t *testing.T) {
	root := kerneltest.New(t)
	rec, p := fake.New()
	kerneltest.Mount(root, p, stamper.Plugin)
	if s, err := rec.Stamp(t.Context(), "example", "hi"); s != "example:d8932aac" || err != nil {
		t.Errorf("Stamp = %q, %v", s, err)
	}
}

// The config follows the decode rules of sdk/plugin (plugin.Define), and the
// stamper refuses an empty name.
func TestConfigIsChecked(t *testing.T) {
	for _, tc := range []struct {
		raw  any
		want string
	}{
		{"text", `invalid config: config: want a map, got a string`},
		{map[string]any{"name": ""}, "invalid config: name is empty"},
		{map[string]any{"name": int64(1)}, "invalid config: config.name: want a string, got 1"},
		{map[string]any{"color": "red"}, "invalid config: config.color: unknown field"},
	} {
		if _, err := stamper.Plugin.Decode(tc.raw); err == nil || err.Error() != tc.want {
			t.Errorf("Decode(%v) = %v, want %q", tc.raw, err, tc.want)
		}
	}
	if _, err := plugin.Describe[stamper.Config](); err != nil {
		t.Error(err)
	}
}
