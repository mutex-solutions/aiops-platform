// SPDX-License-Identifier: Apache-2.0

package deftest

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// File is a definition.yaml in its fixed layout (07-c4-plugin-sdk.md:71 R13, :525-534;
// ADR-060 decisions 7 and 10). The fields come in this order, each at most once:
//
//	key: <name>                 # the key; with one class per key, the definition's name
//	version: "<major>.<minor>"
//	stability: experimental | preview | ga
//	class: {provider: open | bound, consumer: open | bound | signed}
//	remote: contrib | callback | "no"
//	compatibleFrom: "<major>.<minor>"                                  # optional
//	deprecated: {since: <release>, removeNotBefore: <date>, successor: v<N>}   # optional
//	suite: <revision>
//	hooks:                                                             # optional
//	  <hook>: {stages: [<stage>, ...], access: open | signed | sealed, remote: "yes" | "no"}
//
// A definition that declares several keys, as launch does, writes class as a block with
// one line "<key>: {provider: ..., consumer: ...}" per key. A comment starts with "#"
// at the start of a line or after a space. A scalar may be double-quoted, with no
// escapes. Nothing else is read: the SDK module requires no YAML library (ADR-060
// decision 41), so this reader takes the layout and refuses any other form.
type File struct {
	Key, Version, Stability, Remote string
	// Class maps each key name to its class. With one class, the one key is Key.
	Class map[string]Class
	// PerKey reports class written as one line per key.
	PerKey         bool
	CompatibleFrom string
	// Deprecated holds since, removeNotBefore and successor; nil when absent.
	Deprecated map[string]string
	Suite      int
	// Hooks maps each hook the definition owns to its declaration.
	Hooks map[string]HookDecl
}

// Class is the provider class and the consumer class of a key (07-c4-plugin-sdk.md:553).
type Class struct{ Provider, Consumer string }

// HookDecl is a hook as definition.yaml declares it: its stages in chain order, its
// access class and whether remote listeners may listen (07-c4-plugin-sdk.md:439, :443).
type HookDecl struct {
	Stages         []string
	Access, Remote string
}

var (
	order      = []string{"key", "version", "stability", "class", "remote", "compatibleFrom", "deprecated", "suite", "hooks"}
	optional   = []string{"compatibleFrom", "deprecated", "hooks"}
	majorMinor = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	enums      = map[string][]string{
		"stability":  {"experimental", "preview", "ga"},
		"provider":   {"open", "bound"},
		"consumer":   {"open", "bound", "signed"},
		"remote":     {"contrib", "callback", "no"},
		"access":     {"open", "signed", "sealed"},
		"hookRemote": {"yes", "no"},
	}
)

// Major returns the major of a "<major>.<minor>" version, and false for another form.
func Major(version string) (int, bool) {
	m := majorMinor.FindStringSubmatch(version)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

// entry is one top-level field: a value on its line, or a block of indented lines.
type entry struct {
	name, value string
	block       []string
	line        int
}

// Parse reads a definition.yaml in the layout of File.
func Parse(src string) (File, error) {
	var entries []entry
	for i, raw := range strings.Split(src, "\n") {
		line := stripComment(raw)
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, " ") {
			if len(entries) == 0 || entries[len(entries)-1].value != "" {
				return File{}, fmt.Errorf("line %d: indented line outside a block", i+1)
			}
			e := &entries[len(entries)-1]
			e.block = append(e.block, strings.TrimSpace(line))
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok || !slices.Contains(order, name) {
			return File{}, fmt.Errorf("line %d: unknown field %q", i+1, strings.TrimSpace(name))
		}
		entries = append(entries, entry{name: name, value: strings.TrimSpace(value), line: i + 1})
	}
	next := 0
	var f File
	for _, e := range entries {
		at := slices.Index(order, e.name)
		if at < next {
			return File{}, fmt.Errorf("line %d: field %s is repeated or out of order", e.line, e.name)
		}
		for _, skipped := range order[next:at] {
			if !slices.Contains(optional, skipped) {
				return File{}, fmt.Errorf("line %d: field %s is missing", e.line, skipped)
			}
		}
		next = at + 1
		if err := f.set(e); err != nil {
			return File{}, fmt.Errorf("line %d: %s: %w", e.line, e.name, err)
		}
	}
	for _, missing := range order[next:] {
		if !slices.Contains(optional, missing) {
			return File{}, fmt.Errorf("field %s is missing", missing)
		}
	}
	return f, nil
}

func stripComment(line string) string {
	for i := range line {
		if line[i] == '#' && (i == 0 || line[i-1] == ' ') {
			return strings.TrimRight(line[:i], " ")
		}
	}
	return strings.TrimRight(line, " ")
}

func (f *File) set(e entry) (err error) {
	if (e.value == "") == (e.block == nil) || e.block != nil && e.name != "class" && e.name != "hooks" {
		return errors.New("wants one value on its line, or a block for class and hooks")
	}
	switch e.name {
	case "key":
		f.Key, err = scalar(e.value, nil)
	case "version":
		f.Version, err = version(e.value)
	case "stability":
		f.Stability, err = scalar(e.value, enums["stability"])
	case "class":
		f.Class, f.PerKey, err = classes(f.Key, e)
	case "remote":
		f.Remote, err = scalar(e.value, enums["remote"])
	case "compatibleFrom":
		f.CompatibleFrom, err = version(e.value)
	case "deprecated":
		f.Deprecated, err = flowMap(e.value, []string{"since", "removeNotBefore", "successor"})
	case "suite":
		var s string
		if s, err = scalar(e.value, nil); err == nil {
			if f.Suite, err = strconv.Atoi(s); err == nil && f.Suite < 0 {
				err = errors.New("negative revision")
			}
		}
	default: // hooks
		f.Hooks, err = hooks(e.block)
	}
	return err
}

func classes(key string, e entry) (map[string]Class, bool, error) {
	if e.block == nil {
		c, err := class(e.value)
		return map[string]Class{key: c}, false, err
	}
	out := map[string]Class{}
	for _, line := range e.block {
		name, value, ok := strings.Cut(line, ":")
		if !ok || name == "" || out[name] != (Class{}) {
			return nil, true, fmt.Errorf("bad or repeated key line %q", line)
		}
		c, err := class(strings.TrimSpace(value))
		if err != nil {
			return nil, true, fmt.Errorf("%s: %w", name, err)
		}
		out[name] = c
	}
	return out, true, nil
}

func class(s string) (Class, error) {
	m, err := flowMap(s, []string{"provider", "consumer"})
	if err != nil {
		return Class{}, err
	}
	for _, side := range []string{"provider", "consumer"} {
		if !slices.Contains(enums[side], m[side]) {
			return Class{}, fmt.Errorf("%s class %q is not one of %v", side, m[side], enums[side])
		}
	}
	return Class{Provider: m["provider"], Consumer: m["consumer"]}, nil
}

func hooks(block []string) (map[string]HookDecl, error) {
	out := map[string]HookDecl{}
	for _, line := range block {
		name, value, ok := strings.Cut(line, ":")
		if _, seen := out[name]; !ok || name == "" || seen {
			return nil, fmt.Errorf("bad or repeated hook line %q", line)
		}
		m, list, err := flow(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		h := HookDecl{Stages: list["stages"], Access: m["access"], Remote: m["remote"]}
		sorted := slices.Clone(h.Stages)
		slices.Sort(sorted)
		switch {
		case len(m) != 2 || len(list) != 1 || len(h.Stages) == 0:
			err = errors.New("wants exactly stages, access and remote")
		case len(slices.Compact(sorted)) != len(h.Stages):
			err = errors.New("a stage is repeated")
		case !slices.Contains(enums["access"], h.Access) || !slices.Contains(enums["hookRemote"], h.Remote):
			err = fmt.Errorf("access %q or remote %q is not one of %v and %v", h.Access, h.Remote, enums["access"], enums["hookRemote"])
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out[name] = h
	}
	return out, nil
}

func version(s string) (string, error) {
	v, err := scalar(s, nil)
	if _, ok := Major(v); err == nil && !ok {
		err = fmt.Errorf("%q is not <major>.<minor>", v)
	}
	return v, err
}

// scalar reads a bare or double-quoted scalar, which must be one of allowed when that
// is not nil.
func scalar(s string, allowed []string) (string, error) {
	v := s
	if strings.HasPrefix(s, `"`) {
		if len(s) < 2 || !strings.HasSuffix(s, `"`) || strings.ContainsAny(s[1:len(s)-1], `"\`) {
			return "", fmt.Errorf("bad quoted scalar %s", s)
		}
		v = s[1 : len(s)-1]
	} else if strings.ContainsAny(s, `{}[],:"'`) {
		return "", fmt.Errorf("bad scalar %s", s)
	}
	if v == "" || allowed != nil && !slices.Contains(allowed, v) {
		return "", fmt.Errorf("%q is empty or not one of %v", v, allowed)
	}
	return v, nil
}

// flowMap reads "{k: v, ...}" of scalars with exactly the keys named.
func flowMap(s string, keys []string) (map[string]string, error) {
	m, list, err := flow(s)
	if err == nil && (len(list) != 0 || len(m) != len(keys)) {
		err = fmt.Errorf("wants exactly %v", keys)
	}
	for _, k := range keys {
		if _, ok := m[k]; err == nil && !ok {
			err = fmt.Errorf("wants exactly %v", keys)
		}
	}
	return m, err
}

// flow reads "{k: v, k: [a, b], ...}": scalars into m and lists of scalars into list.
func flow(s string) (m map[string]string, list map[string][]string, err error) {
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") {
		return nil, nil, fmt.Errorf("bad flow map %s", s)
	}
	m, list = map[string]string{}, map[string][]string{}
	depth, start, inner := 0, 0, s[1:len(s)-1]
	var parts []string
	for i, r := range inner { // split at the commas outside a list
		switch {
		case r == '[':
			depth++
		case r == ']':
			depth--
		case r == ',' && depth == 0:
			parts, start = append(parts, inner[start:i]), i+1
		}
	}
	for _, part := range append(parts, inner[start:]) {
		k, v, ok := strings.Cut(part, ":")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		_, dup := m[k]
		_, dupList := list[k]
		if !ok || k == "" || dup || dupList || strings.ContainsAny(k, `{}[],"`) {
			return nil, nil, fmt.Errorf("bad or repeated key in flow map %s", s)
		}
		if items, ok := strings.CutPrefix(v, "["); ok && strings.HasSuffix(items, "]") {
			list[k] = []string{}
			for _, item := range strings.Split(items[:len(items)-1], ",") {
				item, err := scalar(strings.TrimSpace(item), nil)
				if err != nil {
					return nil, nil, err
				}
				list[k] = append(list[k], item)
			}
		} else if m[k], err = scalar(v, nil); err != nil {
			return nil, nil, err
		}
	}
	return m, list, nil
}
