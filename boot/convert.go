package boot

import (
	"slices"
	"sort"
	"strings"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	loaderdef "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
)

// The run path hands the kernel plain Go values, not Value trees
// (launch.Row: "nil, bool, int64, float64, string, Ref, []any and map[string]any;
// nothing else survives parsing", ADR-059 decision 6). A reference node becomes
// launch.Ref, the wire type that config-refs resolves on internal/config
// (03-c3-kernel.md:68).

// plain converts a Value to plain Go values. The result shares nothing with v.
func plain(v Value) any {
	switch v.Kind {
	case KindBool:
		return v.Bool
	case KindInt:
		return v.Int
	case KindFloat:
		return v.Float
	case KindString:
		return v.Str
	case KindRef:
		return launch.Ref{Scheme: string(v.Ref.Scheme), Name: v.Ref.Name}
	case KindList:
		out := make([]any, len(v.List))
		for i, x := range v.List {
			out[i] = plain(x)
		}
		return out
	case KindMap:
		out := make(map[string]any, len(v.Map))
		for _, f := range v.Map {
			out[f.Key] = plain(f.Value)
		}
		return out
	}
	return nil
}

func plainFields(fs []Field) map[string]any {
	if len(fs) == 0 {
		return nil
	}
	out := make(map[string]any, len(fs))
	for _, f := range fs {
		out[f.Key] = plain(f.Value)
	}
	return out
}

func roleNames(rs []Role) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = string(r)
	}
	return out
}

// launchRow converts a composed row to the row of the launch definition.
func launchRow(r ComposedRow) launch.Row {
	return launch.Row{
		ID: r.ID, Plugin: r.Plugin, Parent: r.Parent, Config: plain(r.Config), Enabled: r.Enabled,
		Required: r.Required, Group: r.Group, Roles: roleNames(r.Roles), Inject: refsInject(r),
		Isolate: slices.Clone(r.Isolate), Intercept: plainFields(r.Intercept),
	}
}

func launchComposition(c Composition) launch.Composition {
	out := launch.Composition{Rows: make([]launch.Row, len(c.Rows)), Digest: launch.Digest(c.Digest)}
	for i, r := range c.Rows {
		out.Rows[i] = launchRow(r)
	}
	return out
}

// entrySpecs converts the rows of a composition to the entries the loader mounts,
// in composition order, parents before children (loader/v0 EntrySpec). A group row
// keeps its plugin id "core:group" or "core:include" and no config of its own.
func entrySpecs(c Composition) []loaderdef.EntrySpec {
	out := make([]loaderdef.EntrySpec, 0, len(c.Rows))
	for _, r := range c.Rows {
		if r.ID == LoaderRowID {
			continue // the loader is the one that mounts, not a row it mounts
		}
		cfg := plain(r.Config)
		if r.Plugin == pluginGroup || r.Plugin == pluginInclude {
			cfg = nil
		}
		out = append(out, loaderdef.EntrySpec{
			ID: r.ID, Parent: r.Parent, Plugin: r.Plugin, Config: cfg, Enabled: r.Enabled,
			Roles: roleNames(r.Roles), Inject: refsInject(r), Isolate: slices.Clone(r.Isolate),
			Intercept: plainFields(r.Intercept),
		})
	}
	return out
}

func launchPlan(p Plan) launch.Plan {
	out := launch.Plan{From: launch.Digest(p.From), To: launch.Digest(p.To), Entries: make([]launch.PlanEntry, len(p.Entries)),
		Closure: slices.Clone(p.Closure)}
	for i, e := range p.Entries {
		changed := make([]string, len(e.Changed))
		for j, f := range e.Changed {
			changed[j] = string(f)
		}
		out.Entries[i] = launch.PlanEntry{ID: e.ID, Action: launch.Action(e.Action), Changed: changed, ConfigPaths: slices.Clone(e.ConfigPaths)}
	}
	return out
}

// volatilePaths turns the volatile paths of a module entry, which are written
// from the row ("config.auth.token", "config.allow[0]"), into the paths of the
// catalog, which are written from the config ("auth.token", "allow.0"): see
// launch.CatalogEntry.Volatile. The path "config" alone names the whole config,
// which no catalog path can say (the loader never covers a change at the root),
// so it is dropped.
func volatilePaths(in []string) []string {
	var out []string
	for _, p := range in {
		p, ok := strings.CutPrefix(p, "config.")
		if !ok {
			continue
		}
		out = append(out, strings.NewReplacer("[", ".", "]", "").Replace(p))
	}
	sort.Strings(out)
	return out
}

// layersOf lists the layers that set a field of a row, in layer order. A layer
// that set no field of any row (a patch of an unknown id) is not listed: the
// composition keeps no layer list of its own.
func layersOf(c Composition) []launch.Layer {
	seen := map[launch.Layer]bool{}
	var out []launch.Layer
	for _, r := range c.Rows {
		for _, f := range r.Provenance.Fields {
			l := launch.Layer{Number: f.Origin.Layer, Source: f.Origin.Source, File: f.Origin.File}
			if !seen[l] {
				seen[l] = true
				out = append(out, l)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Number != out[j].Number {
			return out[i].Number < out[j].Number
		}
		return out[i].Source < out[j].Source
	})
	return out
}
