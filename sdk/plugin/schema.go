// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// The derivation emits these keywords and no other, and the validator reads
// exactly these (validate.go).
const (
	dialect   = "https://json-schema.org/draft/2020-12/schema"
	tenantKey = "x-aiops-tenant"
	// durationPattern is the syntax of time.ParseDuration, in the subset of
	// regular expressions that RE2 and ECMA-262 read alike.
	durationPattern = `^[-+]?(0|(([0-9]+(\.[0-9]*)?|\.[0-9]+)(ns|us|µs|μs|ms|s|m|h))+)$`
)

var (
	durationType = reflect.TypeFor[time.Duration]()
	volatilePkg  = reflect.TypeFor[Volatile[int]]().PkgPath()
)

// isVolatile reports whether t is an instance of Volatile; its value is field 0.
func isVolatile(t reflect.Type) bool {
	return t.Kind() == reflect.Struct && t.PkgPath() == volatilePkg && strings.HasPrefix(t.Name(), "Volatile[")
}

// field is one exported field of a config struct.
type field struct {
	index  int
	key    string
	def    any // The default as a tree value; nil for none.
	ex     any // The example as a tree value; nil for none.
	nested bool
}

// described is a config type after the derivation: its schema and the
// validator of it, its volatile paths, and the fields of every struct type in it,
// which the decoder reads.
type described struct {
	schema    []byte
	validator Validator
	volatile  [][]string
	fields    map[reflect.Type][]field
}

func describe(t reflect.Type) (*described, error) {
	if t.Kind() != reflect.Struct || isVolatile(t) {
		return nil, fmt.Errorf("config type %s must be a struct", t)
	}
	d := &described{fields: map[reflect.Type][]field{}}
	s, err := d.schemaOf(t, nil, map[reflect.Type]bool{})
	if err != nil {
		return nil, err
	}
	s["$schema"] = dialect
	if d.schema, err = json.MarshalIndent(s, "", "  "); err == nil {
		d.validator, err = NewValidator(d.schema)
	}
	if err != nil {
		return nil, err // unreachable: the derivation emits JSON that the validator reads
	}
	slices.SortFunc(d.volatile, slices.Compare)
	return d, nil
}

// schemaOf returns the schema of t, found at path, and records the volatile
// paths and the struct fields beneath it. open holds the struct types of the
// path, so a recursive type is refused instead of looping.
func (d *described) schemaOf(t reflect.Type, path []string, open map[reflect.Type]bool) (map[string]any, error) {
	if isVolatile(t) {
		d.volatile = append(d.volatile, slices.Clone(path))
		return d.schemaOf(t.Field(0).Type, path, open)
	}
	if t == durationType {
		return map[string]any{"type": "string", "pattern": durationPattern}, nil
	}
	switch k := t.Kind(); k {
	case reflect.Bool:
		return map[string]any{"type": "boolean"}, nil
	case reflect.String:
		return map[string]any{"type": "string"}, nil
	case reflect.Int, reflect.Int64:
		return map[string]any{"type": "integer"}, nil
	case reflect.Int8, reflect.Int16, reflect.Int32:
		return map[string]any{"type": "integer", "minimum": -1 << (t.Bits() - 1), "maximum": 1<<(t.Bits()-1) - 1}, nil
	case reflect.Uint, reflect.Uint64:
		return map[string]any{"type": "integer", "minimum": 0}, nil
	case reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return map[string]any{"type": "integer", "minimum": 0, "maximum": 1<<t.Bits() - 1}, nil
	case reflect.Float32:
		return map[string]any{"type": "number", "minimum": -math.MaxFloat32, "maximum": math.MaxFloat32}, nil
	case reflect.Float64:
		return map[string]any{"type": "number"}, nil
	case reflect.Slice:
		items, err := d.schemaOf(t.Elem(), with(path, "*"), open)
		return map[string]any{"type": "array", "items": items}, err
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("%s: a map key must be a string", t)
		}
		more, err := d.schemaOf(t.Elem(), with(path, "*"), open)
		return map[string]any{"type": "object", "additionalProperties": more}, err
	case reflect.Struct:
		return d.structSchema(t, path, open)
	default:
		return nil, fmt.Errorf("%s: kind %s is not a config type", t, k)
	}
}

func (d *described) structSchema(t reflect.Type, path []string, open map[reflect.Type]bool) (map[string]any, error) {
	if open[t] {
		return nil, fmt.Errorf("%s: a recursive type is not a config type", t)
	}
	open[t] = true
	defer delete(open, t)
	props := map[string]any{}
	var fields []field
	for i := range t.NumField() {
		sf := t.Field(i)
		f, err := fieldOf(sf)
		if err != nil || f == nil {
			if err != nil {
				return nil, fmt.Errorf("%s.%s: %w", t, sf.Name, err)
			}
			continue
		}
		if _, dup := props[f.key]; dup {
			return nil, fmt.Errorf("%s.%s: key %q names two fields", t, sf.Name, f.key)
		}
		f.index = i
		s, err := d.schemaOf(sf.Type, with(path, f.key), open)
		if err != nil {
			return nil, err
		}
		if sf.Tag.Get("aiops") == "tenant" {
			s[tenantKey] = true
		}
		if f.def != nil {
			s["default"] = f.def
		}
		if f.ex != nil {
			s["examples"] = []any{f.ex}
		}
		props[f.key] = s
		fields = append(fields, *f)
	}
	switch {
	case len(fields) > 0:
	case t.NumField() == 1 && t.Field(0).PkgPath == volatilePkg:
		return nil, fmt.Errorf("%s is defined over plugin.Volatile, which keeps none of its methods; use plugin.Volatile[T]", t)
	case path != nil:
		return nil, fmt.Errorf("%s has no config field (an exported field), so it holds nothing; time.Time is not a config type, use a string or a time.Duration", t)
	}
	d.fields[t] = fields
	return map[string]any{"type": "object", "properties": props, "additionalProperties": false}, nil
}

// fieldOf reads the key and the tags of one field: nil for a field that is not
// config.
func fieldOf(sf reflect.StructField) (*field, error) {
	if sf.Anonymous {
		return nil, errors.New("an embedded field is not config; name it")
	}
	tag, hasTag := sf.Tag.Lookup("json")
	name, _, hasOpts := strings.Cut(tag, ",")
	if !sf.IsExported() || tag == "-" {
		return nil, nil
	}
	if hasTag && (name == "" || hasOpts) {
		return nil, fmt.Errorf("json:%q: give a name and no option", tag)
	}
	if name == "*" || strings.Contains(name, ".") {
		return nil, fmt.Errorf(`json:%q: a key is not "*" and holds no ".", which a config path cannot name`, tag)
	}
	f := &field{key: name}
	if name == "" {
		f.key = keyOf(sf.Name)
	}
	inner := sf.Type
	for isVolatile(inner) {
		inner = inner.Field(0).Type
	}
	f.nested = inner.Kind() == reflect.Struct && inner != durationType
	switch a := sf.Tag.Get("aiops"); {
	case a == "tenant" && !isVolatile(sf.Type):
		return nil, errors.New(`aiops:"tenant" marks a tenant-writable field, which must be a plugin.Volatile`)
	case a != "" && a != "tenant":
		return nil, fmt.Errorf(`aiops:%q: the only value is "tenant"`, a)
	}
	for _, t := range []struct {
		tag string
		out *any
	}{{"default", &f.def}, {"example", &f.ex}} {
		if s, ok := sf.Tag.Lookup(t.tag); ok {
			var err error
			if *t.out, err = scalarOf(inner, s, t.tag); err != nil {
				return nil, fmt.Errorf("%s:%q: %w", t.tag, s, err)
			}
		}
	}
	return f, nil
}

// keyOf lowers the leading capitals of a field name, keeping the last of them
// when a lower-case letter follows: URL is url, URLPath is urlPath.
func keyOf(name string) string {
	r := []rune(name)
	n := 0
	for n < len(r) && unicode.IsUpper(r[n]) {
		n++
	}
	if n > 1 && n < len(r) && unicode.IsLower(r[n]) {
		n--
	}
	for i := range n {
		r[i] = unicode.ToLower(r[i])
	}
	return string(r)
}

// scalarOf reads s, the value of the default or example tag of a field of type t,
// as the tree value that the field takes, and checks that the field can take it.
func scalarOf(t reflect.Type, s, tag string) (any, error) {
	var v any
	var err error
	switch k := t.Kind(); {
	case t == durationType, k == reflect.String:
		v = s
	case k == reflect.Bool && (s == "true" || s == "false"):
		v = s == "true"
	case k >= reflect.Int && k <= reflect.Uint64:
		v, err = strconv.ParseInt(s, 10, 64)
	case k == reflect.Float32 || k == reflect.Float64:
		v, err = strconv.ParseFloat(s, 64)
	case k == reflect.Bool:
		return nil, fmt.Errorf("a bool %s is true or false", tag)
	default:
		return nil, fmt.Errorf("only a scalar field takes the tag %s", tag)
	}
	if err != nil {
		return nil, err
	}
	return v, (&described{}).decode(nil, v, reflect.New(t).Elem())
}
