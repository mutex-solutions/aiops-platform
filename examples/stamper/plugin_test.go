// SPDX-License-Identifier: Apache-2.0

package stamper_test

import (
	"testing"

	"github.com/mutex-solutions/aiops-platform/examples/stamper"
	"github.com/mutex-solutions/aiops-platform/sdk/def/stampers/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/def/stampers/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
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

func TestConfigIsChecked(t *testing.T) {
	for _, raw := range []any{"text", map[string]any{"name": ""}, map[string]any{"name": 1}, map[string]any{"color": "red"}} {
		if _, err := stamper.Plugin.Decode(raw); err == nil {
			t.Errorf("Decode(%v) accepted it", raw)
		}
	}
}
