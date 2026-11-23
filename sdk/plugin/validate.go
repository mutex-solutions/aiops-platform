// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
)

// Validator checks a parsed config tree against a JSON Schema 2020-12 document
// (07-c4-plugin-sdk.md:29; ADR-060 decision 4). NewValidator is the one this
// package carries; a caller with a full validator puts it behind the same
// interface.
type Validator interface {
	// Validate returns nil when tree conforms, else every violation, each named
	// by its path from the root "config".
	Validate(tree any) error
}

// node is one compiled schema.
type node struct {
	typ      string
	props    map[string]*node
	more     *node // additionalProperties as a schema.
	closed   bool  // additionalProperties: false.
	items    *node
	min, max *float64
	pattern  *regexp.Regexp
}

// NewValidator compiles schema, a JSON Schema 2020-12 document that uses only the
// keywords the derivation emits: $schema at the root, type with one name,
// properties, additionalProperties, items, minimum, maximum and pattern, and the
// annotations default, examples and x-aiops-tenant. Any other keyword is refused rather
// than ignored, so the validator never passes a tree that a constraint it does
// not know would refuse.
//
// It reads the tree types of the package documentation. integer admits an int64
// and number an int64 or a float64: the launcher keeps integers and floats
// apart, so a float64 is never an integer here, even 5.0, where the standard
// admits 5.0 as an integer. A pattern is matched with Go's regexp, which agrees
// with ECMA-262 on the patterns the derivation emits.
func NewValidator(schema []byte) (Validator, error) {
	var doc any
	if err := json.Unmarshal(schema, &doc); err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	return compile(doc, true)
}

var typeNames = map[string]bool{"object": true, "array": true, "string": true, "integer": true, "number": true, "boolean": true}

func compile(doc any, root bool) (*node, error) {
	m, ok := doc.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema: %s is not a schema object", show(doc))
	}
	n := &node{}
	for _, k := range slices.Sorted(maps.Keys(m)) {
		v := m[k]
		var err error
		switch k {
		case "$schema":
			if !root || v != dialect {
				err = fmt.Errorf("$schema is %s at the root only", dialect)
			}
		case "type":
			n.typ, _ = v.(string)
			if !typeNames[n.typ] {
				err = fmt.Errorf("type %s is not one of object, array, string, integer, number, boolean", show(v))
			}
		case "properties":
			props, isMap := v.(map[string]any)
			if !isMap {
				err = errors.New("properties is not an object")
			}
			n.props = map[string]*node{}
			for _, p := range slices.Sorted(maps.Keys(props)) {
				if n.props[p], err = compile(props[p], false); err != nil {
					break
				}
			}
		case "additionalProperties":
			if b, isBool := v.(bool); isBool {
				n.closed = !b
			} else {
				n.more, err = compile(v, false)
			}
		case "items":
			n.items, err = compile(v, false)
		case "minimum", "maximum":
			f, isNum := v.(float64)
			if !isNum {
				err = fmt.Errorf("%s %s is not a number", k, show(v))
			} else if k == "minimum" {
				n.min = &f
			} else {
				n.max = &f
			}
		case "pattern":
			s, _ := v.(string)
			if n.pattern, err = regexp.Compile(s); err != nil || s == "" {
				err = fmt.Errorf("pattern %s is not a regular expression", show(v))
			}
		case "examples":
			if _, isList := v.([]any); !isList {
				err = fmt.Errorf("examples %s is not a list", show(v))
			}
		case "default", tenantKey:
		default:
			err = fmt.Errorf("keyword %q is not one this validator reads", k)
		}
		if err != nil {
			return nil, err
		}
	}
	return n, nil
}

// Validate implements Validator.
func (n *node) Validate(tree any) error {
	var errs []error
	n.check(nil, tree, &errs)
	return errors.Join(errs...)
}

func (n *node) check(path []string, v any, errs *[]error) {
	fail := func(path []string, format string, a ...any) { *errs = append(*errs, pathErr(path, format, a...)) }
	if !n.admits(v) {
		fail(path, "want %s, got %s", n.want(), show(v))
		return
	}
	switch x := v.(type) {
	case int64:
		n.bounds(path, float64(x), x, fail)
	case float64:
		n.bounds(path, x, x, fail)
	case string:
		if n.pattern != nil && !n.pattern.MatchString(x) {
			fail(path, "want %s, got %s", n.want(), show(x))
		}
	case []any:
		for i, e := range x {
			if n.items != nil {
				n.items.check(with(path, fmt.Sprint(i)), e, errs)
			}
		}
	case map[string]any:
		for _, k := range slices.Sorted(maps.Keys(x)) {
			switch p := n.props[k]; {
			case p != nil:
				p.check(with(path, k), x[k], errs)
			case n.more != nil:
				n.more.check(with(path, k), x[k], errs)
			case n.closed:
				fail(with(path, k), "unknown field")
			}
		}
	}
}

func (n *node) bounds(path []string, f float64, shown any, fail func([]string, string, ...any)) {
	if n.min != nil && f < *n.min {
		fail(path, "%v is below the minimum %v", shown, *n.min)
	}
	if n.max != nil && f > *n.max {
		fail(path, "%v is above the maximum %v", shown, *n.max)
	}
}

// admits reports whether v has the type of n.
func (n *node) admits(v any) bool {
	switch v.(type) {
	case nil:
		return n.typ == ""
	case map[string]any:
		return n.typ == "" || n.typ == "object"
	case []any:
		return n.typ == "" || n.typ == "array"
	case string:
		return n.typ == "" || n.typ == "string"
	case int64:
		return n.typ == "" || n.typ == "integer" || n.typ == "number"
	case float64:
		return n.typ == "" || n.typ == "number"
	case bool:
		return n.typ == "" || n.typ == "boolean"
	}
	return false
}

// want says what n admits, in an error.
func (n *node) want() string {
	if n.pattern != nil && n.pattern.String() == durationPattern {
		return wantDuration
	}
	switch n.typ {
	case "object":
		return "a map"
	case "array":
		return "a list"
	case "string":
		if n.pattern != nil {
			return "a string that matches " + n.pattern.String()
		}
		return "a string"
	case "integer":
		return "an integer"
	case "number":
		return "a number"
	case "boolean":
		return "true or false"
	}
	return "a tree value"
}
