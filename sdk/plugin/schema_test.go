// SPDX-License-Identifier: Apache-2.0

package plugin_test

import (
	"bytes"
	"os"
	"reflect"
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
	none, err := plugin.Describe[struct{ A int }]()
	if err != nil || none.Volatile != nil {
		t.Fatalf("a type with no Volatile field: %q, %v", none.Volatile, err)
	}
	if _, err := plugin.Describe[int](); err == nil || !strings.Contains(err.Error(), "must be a struct") {
		t.Fatalf("Describe[int]: %v", err)
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
			"s": {"type": "string", "pattern": "^a+$", "x-aiops-tenant": true},
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
			"config.extra: want an integer, got \"e\"\nconfig.l.1: want true or false, got \"x\"\nconfig.n: 0.25 is below the minimum 0.5\nconfig.s: want a string that matches ^a+$, got \"b\""},
		{map[string]any{"n": int64(3)}, "config.n: 3 is above the maximum 2"},
		{"x", `config: want a map, got "x"`},
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
