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
// decision 10). The loader decides whether an update changed volatile values only,
// on two parsed config trees, with the volatile paths that the catalog lists for the
// plugin (launch.CatalogEntry.Volatile).
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
// A change whose old or new value holds a launch.Ref is not volatile either: the
// tree holds references unresolved, config-refs resolves them only when a fiber
// activates, and loader/volatile-update would announce the reference, not its
// value. Such a change restarts the fiber.

// change is a place where two trees differ, and whether either side holds a
// reference there.
type change struct {
	path []string
	ref  bool
}

// changedPaths appends to out each place where a and b differ.
func changedPaths(path []string, a, b any, out []change) []change {
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
					out = append(out, change{with(path, k), holdsRef(va) || holdsRef(vb)})
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
		out = append(out, change{slices.Clone(path), holdsRef(a) || holdsRef(b)})
	}
	return out
}

func with(path []string, seg string) []string { return append(slices.Clone(path), seg) }

// holdsRef reports whether v is a launch.Ref or holds one beneath it.
func holdsRef(v any) bool {
	switch x := v.(type) {
	case launch.Ref:
		return true
	case map[string]any:
		return slices.ContainsFunc(slices.Collect(maps.Values(x)), holdsRef)
	case []any:
		return slices.ContainsFunc(x, holdsRef)
	}
	return false
}

// covers reports whether the volatile path pattern covers path.
func covers(pattern, path []string) bool {
	return len(pattern) <= len(path) && slices.EqualFunc(pattern, path[:len(pattern)], func(p, s string) bool { return p == "*" || p == s })
}

// volatileOnly returns the sorted dotted paths at which the trees differ and
// whether every one of them is covered by one of the volatile paths and holds no
// reference. No difference gives no path and true.
func volatileOnly(old, updated any, volatile []string) (paths []string, only bool) {
	patterns := make([][]string, len(volatile))
	for i, v := range volatile {
		patterns[i] = strings.Split(v, ".")
	}
	only = true
	for _, d := range changedPaths(nil, old, updated, nil) {
		paths = append(paths, strings.Join(d.path, "."))
		if d.ref || !slices.ContainsFunc(patterns, func(p []string) bool { return covers(p, d.path) }) {
			only = false
		}
	}
	slices.Sort(paths)
	return paths, only
}
