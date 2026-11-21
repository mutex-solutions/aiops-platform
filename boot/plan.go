package boot

import (
	"fmt"
	"sort"
	"strings"

	loaderdef "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
)

// Plan and judge: steps 3 and 5 of a reload (03-c3-kernel.md:378-380;
// 08-profiles-roadmap-adrs.md:200-204; plugin-system-2026-11.md §9, steps 3 to 5).
// Both are pure. Diff reads two compositions and a graph; Judge reads a plan
// and two views of the observed states. Apply, settle and commit belong to
// the loader and to reload.

// The effects of a plan entry (type Effect, in types.go).
const (
	// EffectNone: nothing is mounted before or after, or only metadata
	// (required, roles) changed on a row that stays as it is.
	EffectNone Effect = "none"
	// EffectMount: a fiber is created (a new row, or a row enabled).
	EffectMount Effect = "mount"
	// EffectUnmount: the fiber is disposed (a row removed or disabled).
	EffectUnmount Effect = "unmount"
	// EffectRestart: the fiber restarts: any change other than volatile
	// config (08-profiles-roadmap-adrs.md:190).
	EffectRestart Effect = "restart"
	// EffectVolatile: only volatile config changed, committed in place with no
	// restart (08-profiles-roadmap-adrs.md:189; ADR-059 decision 10).
	EffectVolatile Effect = "volatile"
	// EffectReplace: the plugin or the parent of the id changed, so the old
	// fiber is disposed and a new one created under the same id.
	EffectReplace Effect = "replace"
)

// Diff computes the plan from the composition that is running (old) to the
// candidate (new), both as the launcher admitted them: a row the gate detached
// is disabled in them. g is the static graph of new. The roles of g select
// the rows that are mounted on both sides.
//
// Per row id: create (id only in new), remove (id only in old), update (some
// field differs; Changed lists the fields in canonical order and ConfigPaths
// the config paths that differ) or unchanged. The effect says what the loader
// does. An update is volatile, and needs no restart, when only config
// changed, every changed path is a volatile path of the module entry of the
// plugin (ADR-059 decision 10) and neither its old nor its new value holds a
// reference (decision 17), by the loader's own rule (effectOf); any other change
// restarts the row.
// Entries are sorted by id; Batch gives the order of apply.
//
// Closure is the restart closure (08-profiles-roadmap-adrs.md:190, 200): the
// rows that restart, and their dependants, which restart through their epochs
// when a provider restarts or leaves. A dependant that is itself created or
// removed by the plan is not listed.
func Diff(old, nw Composition, g Graph) Plan {
	oldRows, newRows := indexRows(old), indexRows(nw)
	oldAct, newAct := ActiveRows(old, g.Roles), ActiveRows(nw, g.Roles)
	ids := map[string]bool{}
	for id := range oldRows {
		ids[id] = true
	}
	for id := range newRows {
		ids[id] = true
	}
	p := Plan{From: old.Digest, To: nw.Digest}
	var seeds []string  // rows whose restart or departure restarts their dependants
	var gone [][]string // keys provided by rows that leave
	for _, id := range sortedKeys(ids) {
		o, inOld := oldRows[id]
		n, inNew := newRows[id]
		e := PlanEntry{ID: id, Effect: EffectNone}
		switch {
		case !inOld:
			e.Action = ActionCreate
			if newAct[id] {
				e.Effect = EffectMount
			}
		case !inNew:
			e.Action = ActionRemove
			if oldAct[id] {
				e.Effect = EffectUnmount
			}
		default:
			e.Changed = changedFields(o.Row, n.Row)
			e.Action = ActionUnchanged
			if len(e.Changed) > 0 {
				e.Action = ActionUpdate
				e.ConfigPaths = configPaths(o.Config, n.Config, "config")
				e.Effect = effectOf(oldAct[id], newAct[id], e, o, n, g)
			}
		}
		switch e.Effect {
		case EffectRestart:
			seeds = append(seeds, id)
		case EffectReplace, EffectUnmount:
			gone = append(gone, g.ProvidedBy(oldRows[id].Plugin))
			seeds = append(seeds, id)
		}
		p.Entries = append(p.Entries, e)
	}
	p.Closure = closureOf(p, seeds, gone, g)
	return p
}

func indexRows(c Composition) map[string]ComposedRow {
	m := make(map[string]ComposedRow, len(c.Rows))
	for _, r := range c.Rows {
		m[r.ID] = r
	}
	return m
}

// changedFields lists the row fields that differ, in canonical order.
func changedFields(a, b Row) []FieldName {
	var out []FieldName
	diff := map[FieldName]bool{
		FieldPlugin:    a.Plugin != b.Plugin,
		FieldConfig:    !equalValue(a.Config, b.Config),
		FieldEnabled:   a.Enabled != b.Enabled,
		FieldRequired:  a.Required != b.Required,
		FieldRoles:     !equalStrings(rolesOf(a.Roles), rolesOf(b.Roles)),
		FieldInject:    !equalStrings(a.Inject, b.Inject),
		FieldIntercept: !equalValue(Value{Kind: KindMap, Map: a.Intercept}, Value{Kind: KindMap, Map: b.Intercept}),
		FieldIsolate:   !equalStrings(a.Isolate, b.Isolate),
		FieldGroup:     a.Group != b.Group,
	}
	for _, f := range FieldNames() {
		if diff[f] {
			out = append(out, f)
		}
	}
	return out
}

func inSetField(f FieldName, fs []FieldName) bool {
	for _, x := range fs {
		if x == f {
			return true
		}
	}
	return false
}

func rolesOf(rs []Role) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = string(r)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// equalValue compares two config trees. Maps are sorted by key, so the
// comparison is positional.
func equalValue(a, b Value) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case KindBool:
		return a.Bool == b.Bool
	case KindInt:
		return a.Int == b.Int
	case KindFloat:
		return a.Float == b.Float
	case KindString:
		return a.Str == b.Str
	case KindRef:
		return a.Ref == b.Ref
	case KindList:
		if len(a.List) != len(b.List) {
			return false
		}
		for i := range a.List {
			if !equalValue(a.List[i], b.List[i]) {
				return false
			}
		}
	case KindMap:
		if len(a.Map) != len(b.Map) {
			return false
		}
		for i := range a.Map {
			if a.Map[i].Key != b.Map[i].Key || !equalValue(a.Map[i].Value, b.Map[i].Value) {
				return false
			}
		}
	}
	return true
}

// configPaths lists the paths where two config trees differ, deepest first
// reached, sorted. A path is dotted with list indexes, as RefUse.Path. Two
// maps are compared key by key, two lists of one length index by index; a
// list of another length, or two values of another kind, differ at the path of
// the value itself.
func configPaths(a, b Value, p string) []string {
	if equalValue(a, b) {
		return nil
	}
	switch {
	case a.Kind == KindMap && b.Kind == KindMap:
		am, bm := fieldMap(a), fieldMap(b)
		keys := map[string]bool{}
		for k := range am {
			keys[k] = true
		}
		for k := range bm {
			keys[k] = true
		}
		var out []string
		for _, k := range sortedKeys(keys) {
			av, aok := am[k]
			bv, bok := bm[k]
			if aok && bok {
				out = append(out, configPaths(av, bv, p+"."+k)...)
			} else {
				out = append(out, p+"."+k)
			}
		}
		return out
	case a.Kind == KindList && b.Kind == KindList && len(a.List) == len(b.List):
		var out []string
		for i := range a.List {
			out = append(out, configPaths(a.List[i], b.List[i], fmt.Sprintf("%s[%d]", p, i))...)
		}
		return out
	}
	return []string{p}
}

func fieldMap(v Value) map[string]Value {
	m := make(map[string]Value, len(v.Map))
	for _, f := range v.Map {
		m[f.Key] = f.Value
	}
	return m
}

// effectOf classifies an update. Whether a config change is volatile is the loader's
// rule (loaderdef.VolatileOnly over the catalog's volatile paths), so the plan says what
// the loader does: a change at a path no volatile path covers, or whose old or new value
// holds a reference (loaderdef.VolatileOnly), restarts the row.
func effectOf(wasActive, isActive bool, e PlanEntry, o, n ComposedRow, g Graph) Effect {
	switch {
	case wasActive && !isActive:
		return EffectUnmount
	case !wasActive && isActive:
		return EffectMount
	case !wasActive:
		return EffectNone
	case inSetField(FieldPlugin, e.Changed):
		return EffectReplace
	}
	relevant := 0
	for _, f := range e.Changed {
		if f != FieldRequired && f != FieldRoles {
			relevant++
		}
	}
	switch {
	case relevant == 0:
		return EffectNone
	case relevant == 1 && inSetField(FieldConfig, e.Changed):
		if _, only := loaderdef.VolatileOnly(plain(o.Config), plain(n.Config), volatilePaths(g.mods[n.Plugin].Volatile)); only {
			return EffectVolatile
		}
	}
	return EffectRestart
}

// closureOf builds the restart closure: the rows that restart or are
// replaced, and the dependants of those rows and of the keys that leave, when
// they are mounted before and after the plan.
func closureOf(p Plan, seeds []string, gone [][]string, g Graph) []string {
	effect := map[string]Effect{}
	for _, e := range p.Entries {
		effect[e.ID] = e.Effect
	}
	reach := g.reach(seeds)
	for _, keys := range gone {
		cons := g.ConsumersOfKeys(keys)
		reach = append(append(reach, cons...), g.reach(cons)...)
	}
	set := map[string]bool{}
	for _, id := range append(reach, seeds...) {
		switch effect[id] {
		case EffectNone, EffectVolatile, EffectRestart, EffectReplace:
			set[id] = true
		}
	}
	return sortedKeys(set)
}

// Step is one operation of the batch of a plan.
type Step struct {
	ID     string
	Action Action // ActionRemove, ActionUpdate or ActionCreate
}

// Batch returns the operations of the plan in the order of one batch: remove,
// update, create (08-profiles-roadmap-adrs.md:200; 03-c3-kernel.md:379). The
// rule, so that the order is deterministic and a fiber is never created while
// the previous fiber of its id is not disposed:
//
//  1. removals first, children before their parents (the deeper the id, the
//     earlier), then by id; a replaced id is removed here, and so is a row
//     that is unmounted by removal;
//  2. then updates, by id;
//  3. then creations, parents before their children, then by id; a replaced
//     id is created here, after its removal.
//
// An unchanged row has no step. The depth of an id is the number of ":" in it,
// since a composed id is its parent's id, a colon and its own id.
func (p Plan) Batch() []Step {
	var rm, up, cr []Step
	for _, e := range p.Entries {
		switch {
		case e.Effect == EffectReplace:
			rm = append(rm, Step{e.ID, ActionRemove})
			cr = append(cr, Step{e.ID, ActionCreate})
		case e.Action == ActionRemove:
			rm = append(rm, Step{e.ID, ActionRemove})
		case e.Action == ActionUpdate:
			up = append(up, Step{e.ID, ActionUpdate})
		case e.Action == ActionCreate:
			cr = append(cr, Step{e.ID, ActionCreate})
		}
	}
	depth := func(id string) int { return strings.Count(id, ":") }
	sort.SliceStable(rm, func(i, j int) bool { return depth(rm[i].ID) > depth(rm[j].ID) })
	sort.SliceStable(cr, func(i, j int) bool { return depth(cr[i].ID) < depth(cr[j].ID) })
	return append(append(rm, up...), cr...)
}

// Judgement is the outcome of Judge. While Settled is false a fiber is still
// UNLOADING, Waiting names it, and there is no verdict yet.
type Judgement struct {
	Settled bool
	Waiting []string
	Verdict Verdict
}

// Judge is the pure half of "settle, then judge" (03-c3-kernel.md:380;
// 08-profiles-roadmap-adrs.md:203). before is the view when the generation was
// applied and after the view at the settle deadline. Every row the generation
// created, changed or enabled (the entries with effect mount, restart,
// replace or volatile) and every required row must be ACTIVE; a row that was
// ACTIVE and is not removed or unmounted must still be; every required key
// needs an ACTIVE provider (shown as a row "key/<name>"). A row that was not
// ACTIVE and is untouched is a warning. A fiber still UNLOADING means wait:
// the result is not settled and carries no verdict.
func Judge(plan Plan, req RequiredSet, before, after StateView) Judgement {
	touched := map[string]bool{}
	leaving := map[string]bool{}
	for _, e := range plan.Entries {
		switch e.Effect {
		case EffectMount, EffectRestart, EffectReplace, EffectVolatile:
			touched[e.ID] = true
		case EffectUnmount:
			leaving[e.ID] = true
		}
	}
	required := map[string]bool{}
	for _, id := range req.Rows {
		required[id] = true
	}
	var waiting []string
	for id, s := range after.Rows {
		if s == StateUnloading {
			waiting = append(waiting, id)
		}
	}
	if len(waiting) > 0 {
		sort.Strings(waiting)
		return Judgement{Waiting: waiting}
	}
	ids := map[string]bool{}
	for id := range after.Rows {
		ids[id] = true
	}
	for id := range touched {
		ids[id] = true
	}
	for id := range required {
		ids[id] = true
	}
	v := Verdict{Plan: plan.To, Pass: true}
	for _, id := range sortedKeys(ids) {
		s := after.Rows[id]
		rv := RowVerdict{ID: id, State: string(s), Outcome: OutcomePass}
		wasActive := before.Rows[id] == StateActive
		switch {
		case s == StateActive:
		case leaving[id]:
			continue
		case touched[id]:
			rv.Outcome, rv.Detail = OutcomeFail, "created, changed or enabled by this generation and not ACTIVE: failed verdict"
		case required[id]:
			rv.Outcome, rv.Detail = OutcomeFail, "required row is not ACTIVE"
		case wasActive:
			rv.Outcome, rv.Detail = OutcomeFail, "was ACTIVE before this generation and is not now"
		default:
			rv.Outcome, rv.Detail = OutcomeWarning, "inactive before and untouched"
		}
		v.Rows = append(v.Rows, rv)
	}
	for _, k := range req.Keys {
		rv := RowVerdict{ID: "key/" + k, Outcome: OutcomePass}
		if !after.Providers[k] {
			rv.Outcome, rv.Detail = OutcomeFail, "required key has no ACTIVE provider"
		}
		v.Rows = append(v.Rows, rv)
	}
	sort.SliceStable(v.Rows, func(i, j int) bool { return v.Rows[i].ID < v.Rows[j].ID })
	for _, r := range v.Rows {
		v.Pass = v.Pass && r.Outcome != OutcomeFail
	}
	return Judgement{Settled: true, Verdict: v}
}
