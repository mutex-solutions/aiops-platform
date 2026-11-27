// SPDX-License-Identifier: Apache-2.0

package plugin_test

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

var (
	keyB     = kernel.NewKey[string]("test.b", 1)
	keyA     = kernel.NewKey[string]("test.a", 0)
	registry = kernel.NewKey[string]("test.registry", 0)
	hookZ    = kernel.NewEvent[int]("test/z", kernel.OwnedBy(keyA), kernel.Stages("first", "second"))
)

// The fields of Manifest are those of schema 1.0, in the order of ADR-048 decisions
// 4 and 6 to 12: the hand-written half, then the seven generated fields.
func TestManifestHoldsEveryFieldOfSchema10(t *testing.T) {
	want := []string{"manifestVersion", "id", "version", "title", "description", "license", "publisher",
		"source", "support", "stability", "scope", "runtimes", "capabilities", "integrates", "instantiable",
		"uses", "provides", "registers", "hooks", "configSchema", "durable", "requires"}
	var got []string
	var walk func(reflect.Type)
	walk = func(t reflect.Type) {
		for i := range t.NumField() {
			name, opts, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
			if opts == "inline" {
				walk(t.Field(i).Type)
				continue
			}
			got = append(got, name)
		}
	}
	walk(reflect.TypeFor[plugin.Manifest]())
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Manifest fields\n got %v\nwant %v", got, want)
	}
}

// Generate writes each set sorted, tells a Need from a Want, reads Registers and the
// config schema from the Spec, and names the pluginApi the code is built against.
func TestGenerateReadsTheSpec(t *testing.T) {
	var applied, decoded bool
	p := plugin.Define(plugin.Spec[config]{
		ID:        "test/generate",
		Inject:    []kernel.Dep{kernel.Need(keyB), kernel.Want(kernel.LoggerKey), kernel.Need(keyA), kernel.Need(registry)},
		Provides:  []kernel.AnyKey{keyB, keyA},
		Registers: []kernel.AnyKey{registry},
		Hooks:     []kernel.HookUse{hookZ.Use("second"), kernel.InternalStatus.Use(kernel.DefaultStage), hookZ.Use("first")},
		Validate:  func(*config) error { decoded = true; return nil },
		Apply:     func(*kernel.Context, *config) error { applied = true; return nil },
	})
	g, err := plugin.Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	want := plugin.Generated{
		Uses: []plugin.Use{{Key: "logger", Major: 0, As: "want"}, {Key: "test.a", Major: 0, As: "need"},
			{Key: "test.b", Major: 1, As: "need"}, {Key: "test.registry", Major: 0, As: "need"}},
		Provides:  []plugin.KeyRef{{Key: "test.a", Major: 0}, {Key: "test.b", Major: 1}},
		Registers: []plugin.KeyRef{{Key: "test.registry", Major: 0}},
		Hooks: []plugin.HookRef{{Hook: "internal/status", Stage: "default"}, {Hook: "test/z", Stage: "first"},
			{Hook: "test/z", Stage: "second"}},
		Durable:  []any{},
		Requires: plugin.Requires{PluginAPI: plugin.APIVersion},
	}
	info, err := plugin.Describe[config]()
	if err != nil {
		t.Fatal(err)
	}
	var schema bytes.Buffer
	if err := json.Compact(&schema, info.Schema); err != nil {
		t.Fatal(err)
	}
	if got, err := json.Marshal(g.ConfigSchema); err != nil || string(got) != schema.String() {
		t.Fatalf("configSchema %s, want the derived schema %s", got, schema.Bytes())
	}
	g.ConfigSchema = nil
	if !reflect.DeepEqual(g, want) {
		t.Fatalf("Generate\n got %+v\nwant %+v", g, want)
	}
	if applied || decoded {
		t.Fatal("Generate ran Apply or decoded a config")
	}
}

// configSchema is the derived schema as data: an integer of it is an int64 that keeps
// every digit, above 2^53 too, so the manifest writes the value the derivation holds,
// and a number that is not an integer is a float64 (ADR-048 decision 9).
func TestGenerateKeepsEveryDigitOfAnInteger(t *testing.T) {
	type big struct {
		Big   int64   `default:"9007199254740993"`
		Max   int64   `default:"9223372036854775807"`
		Ratio float64 `default:"0.5"`
		Small int8
	}
	g, err := plugin.Generate(plugin.Define(plugin.Spec[big]{ID: "test/big"}))
	if err != nil {
		t.Fatal(err)
	}
	props, _ := g.ConfigSchema["properties"].(map[string]any)
	for _, c := range []struct {
		field, keyword string
		want           any
	}{
		{"big", "default", int64(9007199254740993)},
		{"max", "default", int64(math.MaxInt64)},
		{"ratio", "default", 0.5},
		{"small", "minimum", int64(-128)},
	} {
		f, _ := props[c.field].(map[string]any)
		if got := f[c.keyword]; got != c.want {
			t.Errorf("%s %s = %v (%T), want %v (%T)", c.field, c.keyword, got, got, c.want, c.want)
		}
	}
}

// A plugin built with kernel.Define has no Spec: its registers are empty and its
// configSchema is {}, the schema every value satisfies (ADR-048 decision 9; ADR-060
// decision 13). Generate reads it even when its own Decode panics on the probe.
func TestGenerateReadsAKernelDefineValue(t *testing.T) {
	for _, decode := range []func(any) (any, error){
		nil,
		func(raw any) (any, error) { return raw, nil },
		func(any) (any, error) { panic("not a config") },
	} {
		p := kernel.Define(kernel.Plugin{ID: "test/kernel-define", Provides: []kernel.AnyKey{keyA}, Decode: decode})
		g, err := plugin.Generate(p)
		if err != nil {
			t.Fatal(err)
		}
		if len(g.Registers) != 0 || g.Registers == nil || g.ConfigSchema == nil || len(g.ConfigSchema) != 0 ||
			!reflect.DeepEqual(g.Provides, []plugin.KeyRef{{Key: "test.a", Major: 0}}) {
			t.Fatalf("Generate = %+v", g)
		}
	}
}

// A set holds no entry twice, and a zero key or hook is no entry (ADR-048 decision 3).
func TestGenerateRefusesWhatTheKernelRefuses(t *testing.T) {
	for name, s := range map[string]plugin.Spec[config]{
		"repeated need":     {Inject: []kernel.Dep{kernel.Need(keyA), kernel.Want(keyA)}},
		"repeated provide":  {Provides: []kernel.AnyKey{keyA, keyA}},
		"repeated register": {Registers: []kernel.AnyKey{registry, registry}},
		"repeated hook":     {Hooks: []kernel.HookUse{hookZ.Use("first"), hookZ.Use("first")}},
		"zero dep":          {Inject: []kernel.Dep{{}}},
		"zero key":          {Registers: []kernel.AnyKey{nil}},
		"zero hook":         {Hooks: []kernel.HookUse{{}}},
		"unknown stage":     {Hooks: []kernel.HookUse{hookZ.Use("third")}},
	} {
		s.ID = "test/refused"
		if _, err := plugin.Generate(plugin.Define(s)); err == nil {
			t.Errorf("%s: Generate passed", name)
		}
	}
}

// Registers is Spec data that kernel.Plugin does not carry, so a value that lists it
// mounts as any other (ADR-048 decision 21).
func TestDefineMountsAPluginThatListsRegisters(t *testing.T) {
	p := plugin.Define(plugin.Spec[config]{ID: "test/registers", Registers: []kernel.AnyKey{registry},
		Apply: func(*kernel.Context, *config) error { return nil }})
	f := kerneltest.New(t).Plugin(p, nil)
	if s, err := await(t, f); s != kernel.Active {
		t.Fatalf("state %s, err %v", s, err)
	}
}
