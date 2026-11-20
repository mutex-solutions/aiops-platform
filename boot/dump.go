package boot

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The dumps of `aiopsctl profile render` (08-profiles-roadmap-adrs.md:357-367;
// 07-c4-plugin-sdk.md:438, 1315). They are text, deterministic, with no
// timestamp and no path other than the ones the layer files were read from,
// so CI can keep them as golden files per profile and role
// (08-profiles-roadmap-adrs.md:367). References are printed unresolved.

// RenderDump is --dump: the composed tree grouped by the layer item that
// introduced each row, with a comment naming every later layer that set a
// field of it; the gate result of a row the gate denied; the required set
// with the static closure of its needs; and which row provides each key. The
// capability coverage by signal and read tool needs descriptors that no S1
// plugin declares, so the dump shows the provider of each key instead
// (08-profiles-roadmap-adrs.md:362, 07-c4-plugin-sdk.md:1315).
func RenderDump(a Admission) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# aiopsctl profile render --dump\ndigest %s\nroles %s\n", a.Comp.Digest, roleList(a.Roles, "all"))
	source := ""
	for _, r := range a.Comp.Rows {
		ins, _ := r.Provenance.Of(FieldID)
		if head := fmt.Sprintf("%s (layer %d)", ins.Source, ins.Layer); head != source {
			source = head
			fmt.Fprintf(&b, "\n## %s\n", head)
		}
		writeRow(&b, r, a.Gate)
	}
	set := a.Required
	fmt.Fprintf(&b, "\n## required set\nrows %s\nkeys %s\nclosure %s\n",
		listOrNone(set.Rows), listOrNone(set.Keys), listOrNone(RequiredClosure(a.Graph, set)))
	b.WriteString("\n## providers\n")
	for _, p := range a.Graph.Providers {
		realm := ""
		if p.Realm != "" {
			realm = fmt.Sprintf(" (realm of %s)", p.Realm)
		}
		fmt.Fprintf(&b, "key %s%s: %s\n", p.Key, realm, strings.Join(p.Rows, ", "))
	}
	b.WriteString("\n## gate\n")
	for _, s := range a.Gate.Slots {
		switch {
		case !s.Evaluated:
			fmt.Fprintf(&b, "check %d %s: not evaluated until %s\n", s.N, s.Name, s.Until)
		case len(s.Fails) == 0:
			fmt.Fprintf(&b, "check %d %s: pass%s\n", s.N, s.Name, partial(s))
		default:
			fmt.Fprintf(&b, "check %d %s: %d failed%s\n", s.N, s.Name, len(s.Fails), partial(s))
		}
	}
	b.WriteString("\n## diagnostics\n")
	diags := a.Diagnostics()
	for _, d := range diags {
		b.WriteString(d + "\n")
	}
	if len(diags) == 0 {
		b.WriteString("none\n")
	}
	return b.String()
}

func partial(s Slot) string {
	if s.Partial == "" {
		return ""
	}
	return " (not read yet: " + s.Partial + ")"
}

func roleList(roles []Role, none string) string {
	if len(roles) == 0 {
		return none
	}
	ss := rolesOf(roles)
	sort.Strings(ss)
	return strings.Join(ss, ",")
}

func listOrNone(ss []string) string {
	if len(ss) == 0 {
		return "none"
	}
	return strings.Join(ss, ", ")
}

func writeRow(b *strings.Builder, r ComposedRow, gate GateResult) {
	fmt.Fprintf(b, "\nrow %s plugin=%s enabled=%v required=%v group=%v roles=%s", r.ID, r.Plugin, r.Enabled, r.Required, r.Group, roleList(r.Roles, "-"))
	if r.Parent != "" {
		fmt.Fprintf(b, " parent=%s", r.Parent)
	}
	b.WriteString("\n")
	fmt.Fprintf(b, "  config %s\n", ShowValue(r.Config))
	for _, l := range []struct {
		name string
		v    []string
	}{{"inject", r.Inject}, {"isolate", r.Isolate}} {
		if len(l.v) > 0 {
			fmt.Fprintf(b, "  %s %s\n", l.name, strings.Join(l.v, ", "))
		}
	}
	if len(r.Intercept) > 0 {
		fmt.Fprintf(b, "  intercept %s\n", ShowValue(Value{Kind: KindMap, Map: r.Intercept}))
	}
	for _, u := range r.Refs {
		fmt.Fprintf(b, "  ref %s = %s:%s\n", u.Path, u.Ref.Scheme, u.Ref.Name)
	}
	ins, _ := r.Provenance.Of(FieldID)
	for _, fo := range r.Provenance.Fields {
		if fo.Origin != ins {
			o := fo.Origin
			fmt.Fprintf(b, "  # %s set by layer %d (%s) %s:%d\n", fo.Field, o.Layer, o.Source, o.File, o.Line)
		}
	}
	if f, ok := gate.Denial(r.ID); ok {
		verb := "detached as disabled"
		if f.Stop {
			verb = "denied, stops boot"
		}
		fmt.Fprintf(b, "  # gate: %s: check %d, rule %s: %s\n", verb, f.Check, f.Rule, f.Reason)
	}
}

// ShowValue renders a config value on one line, deterministically: a map is
// {key: value} in key order, a list [a, b], a string quoted, an int as
// int(1), a float as float(1.5), a reference as ${scheme:name}.
func ShowValue(v Value) string {
	switch v.Kind {
	case KindBool:
		return strconv.FormatBool(v.Bool)
	case KindInt:
		return fmt.Sprintf("int(%d)", v.Int)
	case KindFloat:
		return "float(" + strconv.FormatFloat(v.Float, 'g', -1, 64) + ")"
	case KindString:
		return strconv.Quote(v.Str)
	case KindRef:
		return "${" + string(v.Ref.Scheme) + ":" + v.Ref.Name + "}"
	case KindList:
		parts := make([]string, len(v.List))
		for i, x := range v.List {
			parts[i] = ShowValue(x)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case KindMap:
		parts := make([]string, len(v.Map))
		for i, f := range v.Map {
			parts[i] = f.Key + ": " + ShowValue(f.Value)
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return "null"
}

// RenderHooks is --dump-hooks: every hook chain of the active rows in
// dispatch order, which is the declared stage, then the plugin id
// (07-c4-plugin-sdk.md:438; plugin-system-2026-11.md:311). Never by
// registration order. Rows of one plugin on one stage are ordered by row id.
// A chain whose hook is sealed to the distribution is marked, with its
// listener list. A stage is its ordinal in the definition that owns the hook;
// until S2a derives it from the manifest, the module table entry states it.
func RenderHooks(a Admission) string {
	type link struct {
		stage int
		name  string
		plug  string
		row   string
	}
	chains := map[string][]link{}
	for _, id := range a.Graph.Active {
		r, _ := findRow(a.Admitted, id)
		for _, h := range a.Modules[r.Plugin].Hooks {
			chains[h.Hook] = append(chains[h.Hook], link{stage: h.Stage, name: h.StageName, plug: r.Plugin, row: id})
		}
	}
	sealed := map[string][]string{}
	for _, h := range a.Dist.SealedHooks {
		sealed[h.Hook] = h.Plugins
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# aiopsctl profile render --dump-hooks\ndigest %s\nroles %s\n", a.Comp.Digest, roleList(a.Roles, "all"))
	hooks := make([]string, 0, len(chains))
	for h := range chains {
		hooks = append(hooks, h)
	}
	sort.Strings(hooks)
	if len(hooks) == 0 {
		b.WriteString("\nno hook chains\n")
	}
	for _, h := range hooks {
		ls := chains[h]
		sort.Slice(ls, func(i, j int) bool {
			x, y := ls[i], ls[j]
			if x.stage != y.stage {
				return x.stage < y.stage
			}
			if x.plug != y.plug {
				return x.plug < y.plug
			}
			return x.row < y.row
		})
		fmt.Fprintf(&b, "\nhook %s", h)
		if l, ok := sealed[h]; ok {
			fmt.Fprintf(&b, " sealed to %s", listOrNone(l))
		}
		b.WriteString("\n")
		for i, l := range ls {
			name := ""
			if l.name != "" {
				name = " (" + l.name + ")"
			}
			fmt.Fprintf(&b, "  %d. stage %d%s plugin %s row %s\n", i+1, l.stage, name, l.plug, l.row)
		}
	}
	return b.String()
}

// RenderConfigSchema is --dump-config-schema: one JSON Schema document for
// the composition (08-profiles-roadmap-adrs.md:364). The documents merge the
// configSchema of each plugin, which the manifest generator of S2a derives
// (ADR-059 decision 10). Until then it states what S1 has: one property per
// active row, holding the schema inferred from the composed config, the
// plugin, and the volatile paths of its module entry. The inferred schema
// types each key by its composed value and requires nothing. The
// document states the limit in x-aiops-limit.
func RenderConfigSchema(a Admission) string {
	props := map[string]any{}
	for _, id := range a.Graph.Active {
		r, _ := findRow(a.Admitted, id)
		p := map[string]any{"x-aiops-plugin": r.Plugin}
		if r.Plugin != pluginGroup && r.Plugin != pluginInclude {
			p["type"] = "object"
			p["additionalProperties"] = true
			p["properties"] = inferProps(r.Config)
			if v := a.Modules[r.Plugin].Volatile; len(v) > 0 {
				p["x-aiops-volatile"] = v
			}
		}
		props[id] = p
	}
	doc := map[string]any{
		"$schema":    "https://json-schema.org/draft/2020-12/schema",
		"title":      "aiops composition " + a.Comp.Digest.String(),
		"type":       "object",
		"properties": props,
		"x-aiops-limit": "S1: the per-plugin configSchema is not derived until S2a; " +
			"each row's schema is inferred from its composed config",
	}
	// Every value is a string, bool, map or slice of them: Marshal cannot fail.
	out, _ := json.MarshalIndent(doc, "", "  ")
	return string(out) + "\n"
}

func inferProps(v Value) map[string]any {
	out := map[string]any{}
	for _, f := range v.Map {
		out[f.Key] = inferSchema(f.Value)
	}
	return out
}

func inferSchema(v Value) any {
	switch v.Kind {
	case KindBool:
		return map[string]any{"type": "boolean"}
	case KindInt:
		return map[string]any{"type": "integer"}
	case KindFloat:
		return map[string]any{"type": "number"}
	case KindString:
		return map[string]any{"type": "string"}
	case KindRef:
		return map[string]any{"type": "string", "x-aiops-ref": string(v.Ref.Scheme)}
	case KindList:
		return map[string]any{"type": "array"}
	case KindMap:
		return map[string]any{"type": "object", "properties": inferProps(v)}
	}
	return map[string]any{}
}
