// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"
)

// decoder is one decode of a config type: cells records by path the cell it gives
// each Volatile field (ADR-034 G13). It is nil for a type with no Volatile field.
type decoder struct {
	*described
	cells map[string]volatileCell
}

// alloc gives the Volatile out a new cell, records it at path and returns the value
// that the cell holds.
func (d *decoder) alloc(path []string, out reflect.Value) reflect.Value {
	p, c := out.Addr().Interface().(interface{ alloc() (any, volatileCell) }).alloc()
	d.cells[fmt.Sprintf("%q", path)] = c
	return reflect.ValueOf(p).Elem()
}

// decode sets out, at path of the tree, from tree by the rules of the package
// documentation. Define validates the tree against the derived schema first, so
// what is left for decode to refuse is what the schema cannot say: a duration
// past the range of time.Duration, an int or uint past a 32-bit platform's
// range. It checks every rule anyway, so it is correct on its own.
func (d *decoder) decode(path []string, tree any, out reflect.Value) error {
	t := out.Type()
	if isVolatile(t) {
		return d.decode(path, tree, d.alloc(path, out))
	}
	if t == durationType {
		s, ok := tree.(string)
		v, err := time.ParseDuration(s)
		if !ok || err != nil {
			return pathErr(path, "want %s, got %s", wantDuration, showValue(tree))
		}
		out.SetInt(int64(v))
		return nil
	}
	switch k := t.Kind(); {
	case k == reflect.Bool:
		b, ok := tree.(bool)
		if !ok {
			return pathErr(path, "want true or false, got %s", showValue(tree))
		}
		out.SetBool(b)
	case k == reflect.String:
		s, ok := tree.(string)
		if !ok {
			return pathErr(path, "want a string, got %s", showValue(tree))
		}
		out.SetString(s)
	case k >= reflect.Int && k <= reflect.Int64:
		n, ok := tree.(int64)
		if !ok || out.OverflowInt(n) {
			return pathErr(path, "want an integer that %s holds, got %s", t, showValue(tree))
		}
		out.SetInt(n)
	case k >= reflect.Uint && k <= reflect.Uint64:
		n, ok := tree.(int64)
		if !ok || n < 0 || out.OverflowUint(uint64(n)) {
			return pathErr(path, "want an integer that %s holds, got %s", t, showValue(tree))
		}
		out.SetUint(uint64(n))
	case k == reflect.Float32 || k == reflect.Float64:
		f, ok := tree.(float64)
		if n, isInt := tree.(int64); isInt {
			f, ok = float64(n), true
		}
		if !ok || out.OverflowFloat(f) {
			return pathErr(path, "want a number that %s holds, got %s", t, showValue(tree))
		}
		out.SetFloat(f)
	case k == reflect.Slice:
		l, ok := tree.([]any)
		if !ok {
			return pathErr(path, "want a list, got %s", showValue(tree))
		}
		s := reflect.MakeSlice(t, len(l), len(l))
		for i, e := range l {
			if err := d.decode(with(path, fmt.Sprint(i)), e, s.Index(i)); err != nil {
				return err
			}
		}
		out.Set(s)
	case k == reflect.Map:
		m, ok := tree.(map[string]any)
		if !ok {
			return pathErr(path, "want a map, got %s", showValue(tree))
		}
		mv := reflect.MakeMapWithSize(t, len(m))
		for _, key := range slices.Sorted(maps.Keys(m)) {
			e := reflect.New(t.Elem()).Elem()
			if err := d.decode(with(path, key), m[key], e); err != nil {
				return err
			}
			mv.SetMapIndex(reflect.ValueOf(key).Convert(t.Key()), e)
		}
		out.Set(mv)
	default:
		return d.decodeStruct(path, tree, out)
	}
	return nil
}

// decodeStruct sets each field of the struct out from its key of tree, or from
// its default when the key is absent. An absent Volatile field with no default
// gets a cell of the zero value, which a commit can set.
func (d *decoder) decodeStruct(path []string, tree any, out reflect.Value) error {
	m, ok := tree.(map[string]any)
	if !ok {
		return pathErr(path, "want a map, got %s", showValue(tree))
	}
	fields := d.fields[out.Type()]
	for _, key := range slices.Sorted(maps.Keys(m)) {
		if !slices.ContainsFunc(fields, func(f field) bool { return f.key == key }) {
			return pathErr(with(path, key), "unknown field")
		}
	}
	for _, f := range fields {
		v, in := m[f.key]
		switch {
		case in:
		case f.def != nil:
			v = f.def
		case f.nested:
			v = map[string]any{}
		case isVolatile(out.Field(f.index).Type()):
			d.alloc(with(path, f.key), out.Field(f.index))
			continue
		default:
			continue
		}
		if err := d.decode(with(path, f.key), v, out.Field(f.index)); err != nil {
			return err
		}
	}
	return nil
}

const wantDuration = "a duration such as 50ms or 1h30m"

// pathErr is an error at path, named from the root "config".
func pathErr(path []string, format string, a ...any) error {
	return fmt.Errorf("%s: %s", strings.Join(append([]string{"config"}, path...), "."), fmt.Sprintf(format, a...))
}

func with(path []string, seg string) []string { return append(slices.Clone(path), seg) }

// showValue names a value of a config tree in an error. A string is never echoed: it
// may come from a resolved reference such as ${secret:name}.
func showValue(v any) string {
	if _, ok := v.(string); ok {
		return "a string"
	}
	return show(v)
}

// show names a value of a schema document in an error.
func show(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return fmt.Sprintf("%q", x)
	case bool, int64, float64:
		return fmt.Sprint(x)
	case []any:
		return "a list"
	case map[string]any:
		return "a map"
	}
	return fmt.Sprintf("%v of type %T", v, v)
}
