package boot

import (
	"encoding/json"
	"fmt"
	"maps"
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
// introduced each row, with the origin artifact of a row that an installed
// artifact's bundle inserted and a comment naming every later layer that set a
// field of it; the gate result of a row the gate denied; the required set
// with the static closure of its needs; and which row provides each key. The
// capability coverage by signal and read tool needs descriptors that no S1
// plugin declares, so the dump shows the provider of each key instead
// (08-profiles-roadmap-adrs.md:362, 07-c4-plugin-sdk.md:1315). The lines boot prints
// at every boot follow: the trust of the distribution, allowUnsigned, the accepted
// licences, each departure from the release bindings and each broader credential
// (08-profiles-roadmap-adrs.md:403).
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
	if len(a.Prints) > 0 {
		fmt.Fprintf(&b, "\n## distribution\n%s\n", strings.Join(a.Prints, "\n"))
	}
	b.WriteString("\n## gate\n")
	for _, s := range a.Gate.Slots {
		switch {
		case s.Until != "":
			fmt.Fprintf(&b, "check %d %s: not evaluated until %s\n", s.N, s.Name, s.Until)
		case !s.Evaluated:
			fmt.Fprintf(&b, "check %d %s: not evaluated at %s: %s\n", s.N, s.Name, a.Gate.Entry, s.Reason)
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

// partial says what an evaluated check left out: the clauses its entry point does not
// evaluate, the rows they would have judged, and the clauses of a later step.
func partial(s Slot) string {
	var parts []string
	if len(s.Clauses) > 0 {
		parts = append(parts, "not evaluated: "+strings.Join(s.Clauses, "; "))
	}
	if len(s.Open) > 0 {
		parts = append(parts, fmt.Sprintf("%d row results not evaluated", len(s.Open)))
	}
	if s.Partial != "" {
		parts = append(parts, "later: "+s.Partial)
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, "; ") + ")"
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
	if o := installedBundle(r); o != "" {
		fmt.Fprintf(b, " origin=%s", o)
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
		fmt.Fprintf(b, "  # gate: %s: %s, rule %s: %s\n", verb, gateCheck(f.Check), f.Rule, f.Reason)
	}
}

// gateCheck names the check of a denial by its number and its name in the one gate
// text, or the module table lookup that runs before the ten checks.
func gateCheck(n int) string {
	if n == checkResolve {
		return "module table lookup"
	}
	return fmt.Sprintf("gate check %d (%s)", n, GateChecks()[n-1].Name)
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
// (07-c4-plugin-sdk.md:438; plugin-system-2026-11.md §8). Never by
// registration order. Rows of one plugin on one stage are ordered by row id.
// A chain whose hook is sealed to the distribution is marked, with its
// listener list. A stage is its ordinal in the chain of the hook, which serves of
// the signed distribution manifest holds (Distribution.Table); -1 is a stage that
// serves does not hold.
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

// RenderConfigSchema is --dump-config-schema: one JSON Schema document for the
// composition, merged from the configSchema of each plugin's manifest
// (08-profiles-roadmap-adrs.md:364; ADR-060 decision 14). It has one property per
// active row: the configSchema of its plugin, under the document's $schema, with
// x-aiops-plugin, the plugin, and x-aiops-volatile, the volatile paths derived from it
// (Manifest.Entry). A row of a group, or of a plugin whose manifest has no
// configSchema, has the two marks alone.
func RenderConfigSchema(a Admission) string {
	props := map[string]any{}
	for _, id := range a.Graph.Active {
		r, _ := findRow(a.Admitted, id)
		e := a.Modules[r.Plugin]
		p := map[string]any{}
		maps.Copy(p, e.Schema)
		delete(p, "$schema")
		p["x-aiops-plugin"] = r.Plugin
		if len(e.Volatile) > 0 {
			p["x-aiops-volatile"] = e.Volatile
		}
		props[id] = p
	}
	doc := map[string]any{
		"$schema":    jsonSchemaDialect,
		"title":      "aiops composition " + a.Comp.Digest.String(),
		"type":       "object",
		"properties": props,
	}
	// Every value is what the YAML reader returns, or a string, bool, map or slice of
	// them: Marshal cannot fail.
	out, _ := json.MarshalIndent(doc, "", "  ")
	return string(out) + "\n"
}
