// SPDX-License-Identifier: Apache-2.0

package echoer_test

import (
	"encoding/json"
	"testing"

	"github.com/mutex-solutions/aiops-platform/examples/echoer"
	"github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
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

// The config follows the decode rules of sdk/plugin (plugin.Define), and the
// echoer refuses an empty name.
func TestConfigIsChecked(t *testing.T) {
	for _, tc := range []struct {
		raw  any
		want string
	}{
		{"text", `invalid config: config: want a map, got "text"`},
		{map[string]any{"name": ""}, "invalid config: name is empty"},
		{map[string]any{"name": int64(1)}, "invalid config: config.name: want a string, got 1"},
		{map[string]any{"color": "red"}, "invalid config: config.color: unknown field"},
	} {
		if _, err := echoer.Plugin.Decode(tc.raw); err == nil || err.Error() != tc.want {
			t.Errorf("Decode(%v) = %v, want %q", tc.raw, err, tc.want)
		}
	}
}

// The configSchema carries an example for each field, and the row config made of
// them validates and fixes the reply, as a row of the connector template would
// (ADR-060 decision 24).
func TestSchemaCarriesARowConfig(t *testing.T) {
	info, err := plugin.Describe[echoer.Config]()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Properties map[string]struct{ Examples []any }
	}
	if err := json.Unmarshal(info.Schema, &doc); err != nil {
		t.Fatal(err)
	}
	row := map[string]any{}
	for k, p := range doc.Properties {
		if len(p.Examples) == 0 {
			t.Fatalf("field %s has no example: %s", k, info.Schema)
		}
		row[k] = p.Examples[0]
	}
	v, err := plugin.NewValidator(info.Schema)
	if err == nil {
		err = v.Validate(row)
	}
	if err != nil {
		t.Fatalf("the row config %v: %v", row, err)
	}
	root := kerneltest.New(t)
	rec, p := fake.New()
	kerneltest.Mount(root, p)
	if err := root.Plugin(echoer.Plugin, row).Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	name, _ := row["name"].(string)
	prefix, _ := row["prefix"].(string)
	if r, err := rec.Echo(t.Context(), name, "hi"); r != prefix+"hi" || err != nil {
		t.Errorf("Echo(%s) = %q, %v; want %q", name, r, err, prefix+"hi")
	}
}
