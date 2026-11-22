// SPDX-License-Identifier: Apache-2.0

package loader

import (
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
)

// Volatile config (07-c4-plugin-sdk.md:1374; 03-c3-kernel.md:64; ADR-059
// decision 10). A loader decides whether an update changed volatile values only,
// on two parsed config trees, with the volatile paths that the catalog lists for the
// plugin (launch.CatalogEntry.Volatile). The rule is here, in the definition, so that
// every loader and the fake decide alike.
//
// The two trees are compared node by node:
//   - two maps compare key by key; a key in one map only is a change at its path;
//   - two lists of one length compare index by index, the index being the path
//     segment; lists of different lengths are one change, at the path of the list;
//   - anything else, including a change of type (a map that became a scalar, an
//     int64 that became a float64), is a change at its path unless it is deeply
//     equal. A change at the root has the empty path, which no volatile path covers.
//
// A volatile path is a dotted list of segments. A segment equals a map key or a
// list index, "*" matches any one, and a path covers every path beneath it, so
// "windows" covers "windows.3.start". A change at a path that is shorter than every
// volatile path above it (an ancestor of a volatile field was replaced) is not
// covered, because the replaced node holds other fields too. A key that contains a
// dot or is "*" cannot be named by a path: only an ancestor covers it.
//
// A change whose old or new value holds a launch.Ref is not volatile either (ADR-059,
// owner decision 17): the tree holds references unresolved and config-refs resolves
// them only when a fiber activates, so such a change restarts the fiber. A literal
// changed beside a reference that did not change is volatile; loader/volatile-update
// carries the new config through WithoutRefs, so the event never carries a reference.
// The launcher's plan (boot Diff) calls VolatileOnly too.

// changedPaths appends to out each path at which a and b differ.
func changedPaths(path []string, a, b any, out [][]string) [][]string {
	switch x := a.(type) {
	case map[string]any:
		if y, ok := b.(map[string]any); ok {
			keys := slices.Sorted(maps.Keys(x))
			for _, k := range slices.Sorted(maps.Keys(y)) {
				if _, in := x[k]; !in {
					keys = append(keys, k)
				}
			}
			for _, k := range keys {
				va, ina := x[k]
				vb, inb := y[k]
				if ina && inb {
					out = changedPaths(with(path, k), va, vb, out)
				} else {
					out = append(out, with(path, k))
				}
			}
			return out
		}
	case []any:
		if y, ok := b.([]any); ok && len(x) == len(y) {
			for i := range x {
				out = changedPaths(with(path, strconv.Itoa(i)), x[i], y[i], out)
			}
			return out
		}
	}
	if !reflect.DeepEqual(a, b) {
		out = append(out, slices.Clone(path))
	}
	return out
}

func with(path []string, seg string) []string { return append(slices.Clone(path), seg) }

// HoldsRef reports whether v, a parsed config tree of plain values, is a launch.Ref
// or holds one beneath it.
func HoldsRef(v any) bool {
	switch x := v.(type) {
	case launch.Ref:
		return true
	case map[string]any:
		return slices.ContainsFunc(slices.Collect(maps.Values(x)), HoldsRef)
	case []any:
		return slices.ContainsFunc(x, HoldsRef)
	}
	return false
}

// WithoutRefs returns a copy of v, a parsed config tree of plain values, in which every
// launch.Ref is replaced by nil: the Config of VolatileUpdateArgs. v is not changed.
func WithoutRefs(v any) any {
	switch x := v.(type) {
	case launch.Ref:
		return nil
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = WithoutRefs(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = WithoutRefs(e)
		}
		return out
	}
	return v
}

// at returns the node of the tree v at path, a path that changedPaths found: a map
// that lacks its key gives nil, and a list gives the element at an index that both
// lists have. A leaf above the end of path is returned as it is.
func at(v any, path []string) any {
	for _, seg := range path {
		switch x := v.(type) {
		case map[string]any:
			v = x[seg]
		case []any:
			if i, err := strconv.Atoi(seg); err == nil && i >= 0 && i < len(x) {
				v = x[i]
			}
		}
	}
	return v
}

// covers reports whether the volatile path pattern covers path.
func covers(pattern, path []string) bool {
	return len(pattern) <= len(path) && slices.EqualFunc(pattern, path[:len(pattern)], func(p, s string) bool { return p == "*" || p == s })
}

// VolatileOnly returns the paths at which the trees differ, each as its list of keys
// and sorted, the Paths of VolatileUpdateArgs, and whether every one of them is
// covered by one of the volatile paths while neither its old nor its new value holds a
// reference. No difference gives no path and true.
func VolatileOnly(old, updated any, volatile []string) (paths [][]string, only bool) {
	patterns := make([][]string, len(volatile))
	for i, v := range volatile {
		patterns[i] = strings.Split(v, ".")
	}
	only = true
	paths = changedPaths(nil, old, updated, nil)
	for _, d := range paths {
		if HoldsRef(at(old, d)) || HoldsRef(at(updated, d)) || !slices.ContainsFunc(patterns, func(p []string) bool { return covers(p, d) }) {
			only = false
		}
	}
	slices.SortFunc(paths, slices.Compare)
	return paths, only
}
