// SPDX-License-Identifier: Apache-2.0

package echoer_test

import (
	"testing"

	"github.com/mutex-solutions/aiops-platform/examples/echoer"
	"github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

var loud = map[string]any{"name": "loud", "prefix": "LOUD "}

// The echoer passes the obligations half, with its default config and with a row
// config of its own.
func TestObligations(t *testing.T) {
	for _, cfg := range []any{nil, loud} {
		conformance.Run(t, func(t *testing.T) (conformance.Subject, *kernel.Root) {
			root := kerneltest.New(t)
			return conformance.Contribution(root.Plugin(echoer.Plugin, cfg)), root
		})
	}
}

// The reply is the one the row config fixes (ADR-060 decision 30, step C5).
func TestRowConfigFixesTheReply(t *testing.T) {
	root := kerneltest.New(t)
	rec, p := fake.New()
	kerneltest.Mount(root, p)
	f := root.Plugin(echoer.Plugin, loud)
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	kerneltest.Mount(root, echoer.Plugin)
	for name, want := range map[string]string{"loud": "LOUD hi", "example": "example: hi"} {
		if r, err := rec.Echo(t.Context(), name, "hi"); r != want || err != nil {
			t.Errorf("Echo(%s) = %q, %v; want %q", name, r, err, want)
		}
	}
}

func TestConfigIsChecked(t *testing.T) {
	for _, raw := range []any{"text", map[string]any{"name": ""}, map[string]any{"name": 1}, map[string]any{"color": "red"}} {
		if _, err := echoer.Plugin.Decode(raw); err == nil {
			t.Errorf("Decode(%v) accepted it", raw)
		}
	}
}
