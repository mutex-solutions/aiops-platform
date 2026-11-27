// SPDX-License-Identifier: Apache-2.0

package plugin_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

type rule struct {
	Name  string
	Until plugin.Volatile[time.Duration]
}

// webhook is the config of the worked example of 07-c4-plugin-sdk.md:305, grown by
// one field of each shape the derivation names.
type webhook struct {
	Timeout time.Duration           `default:"5s"`
	URL     plugin.Volatile[string] `aiops:"tenant"`
	Retries uint8                   `default:"3"`
	Burst   int32
	Ratio   float32
	Rules   []rule
	Headers map[string]plugin.Volatile[string]
	Auth    struct {
		Token plugin.Volatile[string] `aiops:"tenant"`
		Realm string
	}
	Windows plugin.Volatile[[]rule] `aiops:"tenant"`
}

// The derived schema is JSON Schema 2020-12, byte for byte the committed golden
// file, so plugingen's output and the manifest parity check are deterministic.
func TestDescribeSchemaGolden(t *testing.T) {
	info, err := plugin.Describe[webhook]()
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/webhook.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(append(info.Schema, '\n'), want) {
		t.Fatalf("schema differs from testdata/webhook.schema.json:\n%s", info.Schema)
	}
	again, _ := plugin.Describe[webhook]()
	if !bytes.Equal(again.Schema, info.Schema) {
		t.Fatal("two derivations differ")
	}
}

// The volatile paths are derived from the type, sorted, each a list of keys with
// "*" for a list index or a map key; a Volatile inside a Volatile is listed too.
// Each is the path of a subschema marked "x-aiops-volatile": true, the mark that the
// generated manifest carries, and a field marked tenant is marked volatile too
// (ADR-048 decision 9).
func TestDescribeVolatilePaths(t *testing.T) {
	info, err := plugin.Describe[webhook]()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"auth", "token"},
		{"headers", "*"},
		{"rules", "*", "until"},
		{"url"},
		{"windows"},
		{"windows", "*", "until"},
	}
	if !reflect.DeepEqual(info.Volatile, want) {
		t.Fatalf("volatile paths %q, want %q", info.Volatile, want)
	}
	var schema any
	if err := json.Unmarshal(info.Schema, &schema); err != nil {
		t.Fatal(err)
	}
	var marked [][]string
	var walk func(s any, path []string)
	walk = func(s any, path []string) {
		m, _ := s.(map[string]any)
		if m["x-aiops-volatile"] == true {
			marked = append(marked, path)
		}
		if m["x-aiops-tenant"] == true && m["x-aiops-volatile"] != true {
			t.Errorf("the tenant field %q is not marked volatile", path)
		}
		props, _ := m["properties"].(map[string]any)
		for k, sub := range props {
			walk(sub, append(slices.Clone(path), k))
		}
		for _, kw := range []string{"items", "additionalProperties"} {
			if sub, ok := m[kw].(map[string]any); ok {
				walk(sub, append(slices.Clone(path), "*"))
			}
		}
	}
	walk(schema, nil)
	slices.SortFunc(marked, slices.Compare)
	if !reflect.DeepEqual(marked, want) {
		t.Fatalf("subschemas marked volatile %q, want %q", marked, want)
	}
	none, err := plugin.Describe[struct{ A int }]()
	if err != nil || none.Volatile != nil || bytes.Contains(none.Schema, []byte("x-aiops-volatile")) {
		t.Fatalf("a type with no Volatile field: %q, %v", none.Volatile, err)
	}
	if _, err := plugin.Describe[int](); err == nil || !strings.Contains(err.Error(), "must be a struct") {
		t.Fatalf("Describe[int]: %v", err)
	}
}

// An example tag gives the field's examples, the JSON Schema 2020-12 annotation from
// which a template takes a row config (ADR-060 decision 24). A config made of the
// first example of each field validates and decodes, read as the launcher reads a
// row: an integer is an int64.
func TestDescribeExamples(t *testing.T) {
	type row struct {
		Name    string                         `default:"example" example:"loud"`
		Prefix  string                         `example:"LOUD "`
		Every   plugin.Volatile[time.Duration] `example:"1s"`
		Retries uint8                          `example:"3"`
		Ratio   float64                        `example:"0.5"`
		Quiet   bool                           `example:"true"`
		Plain   string
	}
	info, err := plugin.Describe[row]()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Properties map[string]struct{ Examples []any }
	}
	dec := json.NewDecoder(bytes.NewReader(info.Schema))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("%v: %s", err, info.Schema)
	}
	cfg := map[string]any{}
	for k, p := range doc.Properties {
		if len(p.Examples) == 0 {
			continue
		}
		v := p.Examples[0]
		if n, isNum := v.(json.Number); isNum {
			if v, err = n.Int64(); err != nil {
				v, _ = n.Float64()
			}
		}
		cfg[k] = v
	}
	if len(cfg) != 6 {
		t.Fatalf("the schema carries examples for %v, want six fields: %s", cfg, info.Schema)
	}
	v, err := plugin.NewValidator(info.Schema)
	if err == nil {
		err = v.Validate(cfg)
	}
	if err != nil {
		t.Fatalf("the config of the examples %v: %v", cfg, err)
	}
	got, err := plugin.Define(plugin.Spec[row]{ID: "t/examples"}).Decode(cfg)
	if r, _ := plugin.ConfigOf(got).(*row); err != nil || r.Name != "loud" || r.Every.Get() != time.Second || r.Retries != 3 || !r.Quiet {
		t.Fatalf("Decode(%v) = %+v, %v", cfg, got, err)
	}
}

// The validator reads exactly the keywords the derivation emits and refuses any
// other, so it never passes what an unread constraint would refuse.
func TestNewValidatorRefusesWhatItDoesNotRead(t *testing.T) {
	for schema, want := range map[string]string{
		`{"enum": [1]}`:                `keyword "enum"`,
		`{"$ref": "#/a"}`:              `keyword "$ref"`,
		`{"minLength": 1}`:             `keyword "minLength"`,
		`{"type": ["string", "null"]}`: "is not one of",
		`{"type": "null"}`:             "is not one of",
		`{"properties": {"a": true}}`:  "is not a schema object",
		`{"properties": []}`:           "properties is not an object",
		`{"items": {"oneOf": []}}`:     `keyword "oneOf"`,
		`{"minimum": "1"}`:             "minimum \"1\" is not a number",
		`{"maximum": null}`:            "maximum null is not a number",
		`{"pattern": "("}`:             "is not a regular expression",
		`{"pattern": 1}`:               "is not a regular expression",
		`{"examples": "a"}`:            `examples "a" is not a list`,
		`{"items": {"$schema": "x"}}`:  "at the root only",
		`{"$schema": "http://json-schema.org/draft-07/schema#"}`: "at the root only",
		`{"additionalProperties": {"const": 1}}`:                 `keyword "const"`,
		`[]`:                                                     "is not a schema object",
		`{`:                                                      "schema: unexpected end of JSON input",
	} {
		if _, err := plugin.NewValidator([]byte(schema)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("NewValidator(%s): %v, want an error with %q", schema, err, want)
		}
	}
}

// The validator on schemas written by hand, with every keyword it reads.
func TestValidatorOnHandWrittenSchemas(t *testing.T) {
	v, err := plugin.NewValidator([]byte(`{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type": "object",
		"properties": {
			"n": {"type": "number", "minimum": 0.5, "maximum": 2, "default": 1},
			"s": {"type": "string", "pattern": "^a+$", "x-aiops-tenant": true, "examples": ["aa", 1]},
			"any": {},
			"l": {"type": "array", "items": {"type": "boolean"}}
		},
		"additionalProperties": {"type": "integer"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		tree any
		want string // every line of the error, in order; empty for none
	}{
		{map[string]any{"n": 1.5, "s": "aa", "any": nil, "l": []any{true}, "extra": int64(3)}, ""},
		{map[string]any{"any": map[string]any{"x": []any{"y"}}}, ""},
		{map[string]any{"n": 0.25, "s": "b", "l": []any{true, "x"}, "extra": "e"},
			"config.extra: want an integer, got a string\nconfig.l.1: want true or false, got a string\nconfig.n: 0.25 is below the minimum 0.5\nconfig.s: want a string that matches ^a+$, got a string"},
		{map[string]any{"n": int64(3)}, "config.n: 3 is above the maximum 2"},
		{"x", `config: want a map, got a string`},
		{map[string]any{"any": struct{}{}}, "config.any: want a tree value, got {} of type struct {}"},
	} {
		err := v.Validate(tc.tree)
		if got := errString(err); got != tc.want {
			t.Errorf("Validate(%v):\n got %q\nwant %q", tc.tree, got, tc.want)
		}
	}
	open, err := plugin.NewValidator([]byte(`{"type": "object", "additionalProperties": true, "properties": {"a": {"type": "string"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := open.Validate(map[string]any{"b": int64(1)}); err != nil {
		t.Fatalf("additionalProperties true refused a field: %v", err)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
