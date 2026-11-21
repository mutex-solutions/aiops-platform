package boot

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

const planMods = `
o/a: {provides: [ka], volatile: [timeout, auth.token, list]}
o/b: {needs: [ka]}
o/c: {}
o/d: {needs: [kd], provides: [kd2]}
`

func plan(t *testing.T, oldB, newB string, roles ...Role) Plan {
	t.Helper()
	old, nw := comp(t, oldB), comp(t, newB)
	g, _, _ := BuildGraph(nw, mustMods(t, planMods), DistManifest{}, roles)
	return Diff(old, nw, g)
}

func summary(p Plan) string {
	var parts []string
	for _, e := range p.Entries {
		s := fmt.Sprintf("%s:%s:%s", e.ID, e.Action, e.Effect)
		if len(e.Changed) > 0 {
			fs := make([]string, len(e.Changed))
			for i, f := range e.Changed {
				fs[i] = string(f)
			}
			s += "[" + strings.Join(fs, ",") + "]"
		}
		if len(e.ConfigPaths) > 0 {
			s += "{" + strings.Join(e.ConfigPaths, ",") + "}"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

func TestDiff(t *testing.T) {
	cases := []struct {
		name         string
		old, new     string
		roles        []Role
		want         string
		closure      string
		batch        string
		sameDigest   bool
		differDigest bool
	}{
		{name: "unchanged", old: rows("id: a, plugin: o/a"), new: rows("id: a, plugin: o/a"),
			want: "a:unchanged:none", sameDigest: true},
		{name: "create enabled and disabled", old: rows("id: a, plugin: o/a"),
			new:  rows("id: a, plugin: o/a", "id: b, plugin: o/b", "id: z, plugin: o/c, enabled: false"),
			want: "a:unchanged:none b:create:mount z:create:none", batch: "create:b create:z"},
		{name: "remove active and disabled", old: rows("id: c, plugin: o/c", "id: z, plugin: o/c, enabled: false"), new: rows("id: x, plugin: o/c"),
			want: "c:remove:unmount x:create:mount z:remove:none", batch: "remove:c remove:z create:x"},
		{name: "volatile only", old: rows("id: a, plugin: o/a, config: {timeout: 1, other: 1}"), new: rows("id: a, plugin: o/a, config: {timeout: 2, other: 1}"),
			want: "a:update:volatile[config]{config.timeout}", batch: "update:a"},
		{name: "volatile nested path and list", old: rows("id: a, plugin: o/a, config: {auth: {token: x, u: 1}, list: [1, 2]}"),
			new:  rows("id: a, plugin: o/a, config: {auth: {token: z1, u: 1}, list: [1, 2, 3]}"),
			want: "a:update:volatile[config]{config.auth.token,config.list}", batch: "update:a"},
		{name: "volatile under a volatile list index", old: rows("id: a, plugin: o/a, config: {list: [1, 2]}"),
			new:  rows("id: a, plugin: o/a, config: {list: [1, 3]}"),
			want: "a:update:volatile[config]{config.list[1]}", batch: "update:a"},
		{name: "non volatile config restarts and its dependants follow", old: rows("id: a, plugin: o/a, config: {mode: m1}", "id: b, plugin: o/b"),
			new:  rows("id: a, plugin: o/a, config: {mode: m2}", "id: b, plugin: o/b"),
			want: "a:update:restart[config]{config.mode} b:unchanged:none", closure: "a,b", batch: "update:a"},
		// The loader's rule (sdk/def/loader/v0 VolatileOnly): a volatile value that is or was
		// a reference restarts the row, and its dependants follow.
		{name: "volatile change to a reference restarts", old: rows("id: a, plugin: o/a, config: {timeout: info}", "id: b, plugin: o/b"),
			new:  rows("id: a, plugin: o/a, config: {timeout: \"${env:AIOPS_REF_LEVEL}\"}", "id: b, plugin: o/b"),
			want: "a:update:restart[config]{config.timeout} b:unchanged:none", closure: "a,b", batch: "update:a"},
		{name: "volatile change from a reference restarts", old: rows("id: a, plugin: o/a, config: {timeout: \"${env:AIOPS_REF_LEVEL}\"}"),
			new:  rows("id: a, plugin: o/a, config: {timeout: info}"),
			want: "a:update:restart[config]{config.timeout}", closure: "a", batch: "update:a"},
		// Decision 17 names the old or new value of the volatile field: a literal beside an
		// unchanged reference is committed in place.
		{name: "volatile change beside an unchanged reference is volatile", old: rows("id: a, plugin: o/a, config: {timeout: 1, note: \"${env:AIOPS_REF_LEVEL}\"}"),
			new:  rows("id: a, plugin: o/a, config: {timeout: 2, note: \"${env:AIOPS_REF_LEVEL}\"}"),
			want: "a:update:volatile[config]{config.timeout}", batch: "update:a"},
		{name: "volatile and not volatile together", old: rows("id: a, plugin: o/a, config: {timeout: 1}"), new: rows("id: a, plugin: o/a, config: {timeout: 2, mode: 1}"),
			want: "a:update:restart[config]{config.mode,config.timeout}", closure: "a", batch: "update:a"},
		{name: "config key removed and kind changed", old: rows("id: a, plugin: o/a, config: {timeout: 1, k: {x: 1}}"), new: rows("id: a, plugin: o/a, config: {k: 5}"),
			want: "a:update:restart[config]{config.k,config.timeout}", closure: "a", batch: "update:a"},
		{name: "config and inject", old: rows("id: c, plugin: o/c, config: {timeout: 1}"), new: rows("id: c, plugin: o/c, config: {timeout: 2}, inject: [kx]"),
			want: "c:update:restart[config,inject]{config.timeout}", closure: "c", batch: "update:c"},
		{name: "intercept isolate group", old: rows("id: c, plugin: o/c"), new: rows("id: c, plugin: o/c, intercept: {k: 1}, isolate: [k], group: true"),
			want: "c:update:restart[intercept,isolate,group]", closure: "c", batch: "update:c"},
		{name: "required and roles only", old: rows("id: c, plugin: o/c, roles: [api]"), new: rows("id: c, plugin: o/c, roles: [api, worker], required: true"),
			want: "c:update:none[required,roles]", batch: "update:c"},
		{name: "disable a provider: consumers restart", old: rows("id: a, plugin: o/a", "id: b, plugin: o/b"), new: rows("id: a, plugin: o/a, enabled: false", "id: b, plugin: o/b"),
			want: "a:update:unmount[enabled] b:unchanged:none", closure: "b", batch: "update:a"},
		{name: "remove a provider: consumers restart", old: rows("id: a, plugin: o/a", "id: b, plugin: o/b"), new: rows("id: b, plugin: o/b"),
			want: "a:remove:unmount b:unchanged:none", closure: "b", batch: "remove:a"},
		{name: "enable a row", old: rows("id: a, plugin: o/a, enabled: false"), new: rows("id: a, plugin: o/a"),
			want: "a:update:mount[enabled]", batch: "update:a"},
		{name: "change of a row that is not mounted", old: rows("id: a, plugin: o/a, enabled: false"), new: rows("id: a, plugin: o/a, enabled: false, config: {x: 1}"),
			want: "a:update:none[config]{config.x}", batch: "update:a"},
		{name: "replaced plugin under the same id", old: rows("id: a, plugin: o/a", "id: b, plugin: o/b"), new: rows("id: a, plugin: o/c", "id: b, plugin: o/b"),
			want: "a:update:replace[plugin] b:unchanged:none", closure: "a,b", batch: "remove:a create:a"},
		{name: "a dependant that the plan creates is not a restart", old: rows("id: a, plugin: o/a"),
			new:  rows("id: a, plugin: o/a, config: {mode: 1}", "id: b, plugin: o/b"),
			want: "a:update:restart[config]{config.mode} b:create:mount", closure: "a", batch: "update:a create:b"},
		{name: "group restart takes its children", old: rows("id: g, plugin: core:group, config: [{id: c, plugin: o/c}]"),
			new:  rows("id: g, plugin: core:group, inject: [kx], config: [{id: c, plugin: o/c}]"),
			want: "g:update:restart[inject] g:c:unchanged:none", closure: "g,g:c", batch: "update:g"},
		{name: "role filter: a row that leaves the role", old: rows("id: c, plugin: o/c, roles: [api]"), new: rows("id: c, plugin: o/c, roles: [worker]"),
			roles: []Role{RoleAPI}, want: "c:update:unmount[roles]", batch: "update:c"},
		{name: "role filter: a change of a row of another role is none", old: rows("id: c, plugin: o/c, roles: [worker]"), new: rows("id: c, plugin: o/c, roles: [worker], config: {x: 1}"),
			roles: []Role{RoleAPI}, want: "c:update:none[config]{config.x}", batch: "update:c"},
		{name: "batch order: remove deep first, update, create parents first",
			old: rows("id: g, plugin: core:group, config: [{id: k, plugin: o/c, config: {v: 1}}, {id: d, plugin: core:group, config: [{id: e, plugin: o/c}]}]",
				"id: u, plugin: o/c, config: {v: 1}"),
			new: rows("id: n, plugin: core:group, config: [{id: m, plugin: o/c}]", "id: u, plugin: o/c, config: {v: 2}",
				"id: g, plugin: core:group, config: [{id: k, plugin: o/c, config: {v: 2}}]"),
			want:    "g:unchanged:none g:d:remove:unmount g:d:e:remove:unmount g:k:update:restart[config]{config.v} n:create:mount n:m:create:mount u:update:restart[config]{config.v}",
			closure: "g:k,u", batch: "remove:g:d:e remove:g:d update:g:k update:u create:n create:n:m"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			old, nw := comp(t, tc.old), comp(t, tc.new)
			g, _, _ := BuildGraph(nw, mustMods(t, planMods), DistManifest{}, tc.roles)
			p := Diff(old, nw, g)
			if got := summary(p); got != tc.want {
				t.Fatalf("entries\n got %s\nwant %s", got, tc.want)
			}
			if got := strings.Join(p.Closure, ","); got != tc.closure {
				t.Errorf("closure %q, want %q", got, tc.closure)
			}
			var steps []string
			for _, s := range p.Batch() {
				steps = append(steps, string(s.Action)+":"+s.ID)
			}
			if got := strings.Join(steps, " "); got != tc.batch {
				t.Errorf("batch %q, want %q", got, tc.batch)
			}
			if p.From != old.Digest || p.To != nw.Digest {
				t.Error("digests of the plan")
			}
			if tc.sameDigest && p.From != p.To {
				t.Error("same composition, different digest")
			}
		})
	}
}

func TestChangedFieldsAndValues(t *testing.T) {
	a := Row{ID: "x", Plugin: "o/a", Enabled: true, Roles: []Role{RoleAPI}, Inject: []string{"i"}, Isolate: []string{"s"},
		Config: Value{Kind: KindMap, Map: []Field{{Key: "k", Value: Value{Kind: KindInt, Int: 1}}}}}
	b := a
	if got := changedFields(a, b); got != nil {
		t.Errorf("equal rows differ in %v", got)
	}
	b = Row{ID: "x", Plugin: "o/b", Enabled: false, Required: true, Roles: []Role{RoleWorker}, Inject: []string{"j"}, Isolate: nil, Group: true,
		Intercept: []Field{{Key: "q", Value: Value{Kind: KindBool}}}}
	want := "plugin,config,enabled,required,roles,inject,intercept,isolate,group"
	var got []string
	for _, f := range changedFields(a, b) {
		got = append(got, string(f))
	}
	if strings.Join(got, ",") != want {
		t.Errorf("changed %v, want %s", got, want)
	}
	i := func(n int64) Value { return Value{Kind: KindInt, Int: n} }
	s := func(x string) Value { return Value{Kind: KindString, Str: x} }
	l := func(vs ...Value) Value { return Value{Kind: KindList, List: vs} }
	m := func(kv ...any) Value {
		v := Value{Kind: KindMap}
		for n := 0; n < len(kv); n += 2 {
			v.Map = append(v.Map, Field{Key: kv[n].(string), Value: kv[n+1].(Value)})
		}
		return v
	}
	for _, tc := range []struct {
		a, b  Value
		equal bool
		paths string
	}{
		{Value{}, Value{}, true, ""},
		{Value{Kind: KindBool, Bool: true}, Value{Kind: KindBool, Bool: false}, false, "p"},
		{Value{Kind: KindBool, Bool: true}, Value{Kind: KindBool, Bool: true}, true, ""},
		{i(1), i(2), false, "p"},
		{i(1), Value{Kind: KindFloat, Float: 1}, false, "p"},
		{Value{Kind: KindFloat, Float: 1.5}, Value{Kind: KindFloat, Float: 1.5}, true, ""},
		{Value{Kind: KindFloat, Float: 1.5}, Value{Kind: KindFloat, Float: 2.5}, false, "p"},
		{s("a"), s("a"), true, ""},
		{s("a"), s("b"), false, "p"},
		{Value{Kind: KindRef, Ref: Ref{RefEnv, "A"}}, Value{Kind: KindRef, Ref: Ref{RefEnv, "B"}}, false, "p"},
		{Value{Kind: KindRef, Ref: Ref{RefEnv, "A"}}, Value{Kind: KindRef, Ref: Ref{RefEnv, "A"}}, true, ""},
		{l(i(1), i(2)), l(i(1), i(2)), true, ""},
		{l(i(1), i(2)), l(i(1), i(3)), false, "p[1]"},
		{l(i(1)), l(i(1), i(2)), false, "p"},
		{m("a", i(1)), m("a", i(1)), true, ""},
		{m("a", i(1)), m("b", i(1)), false, "p.a,p.b"},
		{m("a", i(1)), m("a", i(1), "b", i(2)), false, "p.b"},
		{m("a", m("b", l(i(1)))), m("a", m("b", l(i(2)))), false, "p.a.b[0]"},
		{m("a", i(1)), l(), false, "p"},
	} {
		if got := equalValue(tc.a, tc.b); got != tc.equal {
			t.Errorf("equalValue(%s, %s) = %v", ShowValue(tc.a), ShowValue(tc.b), got)
		}
		if got := strings.Join(configPaths(tc.a, tc.b, "p"), ","); got != tc.paths {
			t.Errorf("configPaths(%s, %s) = %q, want %q", ShowValue(tc.a), ShowValue(tc.b), got, tc.paths)
		}
	}
}

// Applying the plan to the old rows gives the new rows, whatever the two
// compositions are.
func TestDiffThenApplyYieldsNew(t *testing.T) {
	rnd := rand.New(rand.NewSource(7))
	gen := func() string {
		var specs []string
		for _, id := range []string{"a", "b", "c", "d", "e", "f"} {
			if rnd.Intn(3) == 0 {
				continue
			}
			s := fmt.Sprintf("id: %s, plugin: o/%c", id, 'a'+rune(rnd.Intn(4)))
			if rnd.Intn(3) == 0 {
				s += ", enabled: false"
			}
			if rnd.Intn(3) == 0 {
				s += fmt.Sprintf(", config: {timeout: %d, mode: %d}", rnd.Intn(3), rnd.Intn(2))
			}
			if rnd.Intn(4) == 0 {
				s += ", inject: [kx]"
			}
			specs = append(specs, s)
		}
		if len(specs) == 0 {
			return "[]"
		}
		return rows(specs...)
	}
	for i := 0; i < 300; i++ {
		old, nw := comp(t, gen()), comp(t, gen())
		g, _, _ := BuildGraph(nw, mustMods(t, planMods), DistManifest{}, nil)
		p := Diff(old, nw, g)
		have := map[string]Row{}
		for _, r := range old.Rows {
			have[r.ID] = r.Row
		}
		want := map[string]Row{}
		for _, r := range nw.Rows {
			want[r.ID] = r.Row
		}
		for _, s := range p.Batch() {
			switch s.Action {
			case ActionRemove:
				if _, ok := have[s.ID]; !ok {
					t.Fatalf("iteration %d: remove of %s that is not there", i, s.ID)
				}
				delete(have, s.ID)
			case ActionCreate:
				if _, ok := have[s.ID]; ok {
					t.Fatalf("iteration %d: create of %s while the previous one is not disposed", i, s.ID)
				}
				have[s.ID] = want[s.ID]
			case ActionUpdate:
				have[s.ID] = want[s.ID]
			}
		}
		if !reflect.DeepEqual(have, want) {
			t.Fatalf("iteration %d: applying the plan does not give the new rows\n%s", i, summary(p))
		}
		for j := 1; j < len(p.Entries); j++ {
			if p.Entries[j-1].ID >= p.Entries[j].ID {
				t.Fatalf("entries are not sorted by id: %s", summary(p))
			}
		}
	}
}

func TestJudge(t *testing.T) {
	p := Plan{To: Digest{1}, Entries: []PlanEntry{
		{ID: "made", Action: ActionCreate, Effect: EffectMount},
		{ID: "changed", Action: ActionUpdate, Effect: EffectRestart},
		{ID: "live", Action: ActionUpdate, Effect: EffectVolatile},
		{ID: "gone", Action: ActionRemove, Effect: EffectUnmount},
		{ID: "quiet", Action: ActionUpdate, Effect: EffectNone},
	}}
	act := func(ids ...string) map[string]State {
		m := map[string]State{}
		for _, id := range ids {
			m[id] = StateActive
		}
		return m
	}
	merge := func(a map[string]State, kv ...string) map[string]State {
		for i := 0; i < len(kv); i += 2 {
			a[kv[i]] = State(kv[i+1])
		}
		return a
	}
	req := RequiredSet{Rows: []string{"must"}, Keys: []string{"sql"}}
	keys := map[string]bool{"sql": true}
	cases := []struct {
		name    string
		before  StateView
		after   StateView
		settled bool
		wait    string
		pass    bool
		rows    string
	}{
		{name: "everything active",
			before:  StateView{Rows: act("changed", "gone", "must", "quiet", "old")},
			after:   StateView{Rows: act("made", "changed", "live", "must", "quiet", "old"), Providers: keys},
			settled: true, pass: true,
			rows: "changed:pass:ACTIVE key/sql:pass: live:pass:ACTIVE made:pass:ACTIVE must:pass:ACTIVE old:pass:ACTIVE quiet:pass:ACTIVE"},
		{name: "touched, required, regressed and untouched rows",
			before: StateView{Rows: act("changed", "must", "old", "quiet")},
			after: StateView{Rows: merge(act(), "made", "FAILED", "changed", "PENDING", "live", "LOADING", "must", "FAILED", "old", "PENDING",
				"quiet", "FAILED", "idle", "PENDING", "gone", "DISPOSED"), Providers: map[string]bool{}},
			settled: true,
			rows: "changed:fail:PENDING gone:pass:DISPOSED=skip idle:warning:PENDING key/sql:fail: live:fail:LOADING made:fail:FAILED " +
				"must:fail:FAILED old:fail:PENDING quiet:fail:FAILED"},
		{name: "a required row that is missing", before: StateView{}, after: StateView{Rows: act("made", "changed", "live"), Providers: keys},
			settled: true, rows: "changed:pass:ACTIVE key/sql:pass: live:pass:ACTIVE made:pass:ACTIVE must:fail:"},
		{name: "an unloading fiber means wait", before: StateView{},
			after: StateView{Rows: merge(act("made"), "slow", "UNLOADING", "also", "UNLOADING")}, wait: "also,slow"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := Judge(p, req, tc.before, tc.after)
			if j.Settled != tc.settled || strings.Join(j.Waiting, ",") != tc.wait {
				t.Fatalf("settled=%v waiting=%v", j.Settled, j.Waiting)
			}
			if !j.Settled {
				if j.Verdict.Pass || len(j.Verdict.Rows) != 0 {
					t.Errorf("a wait has no verdict: %+v", j.Verdict)
				}
				return
			}
			var got []string
			for _, r := range j.Verdict.Rows {
				got = append(got, fmt.Sprintf("%s:%s:%s", r.ID, r.Outcome, r.State))
				if r.Outcome != OutcomePass && r.Detail == "" {
					t.Errorf("row %s fails without a detail", r.ID)
				}
			}
			want := strings.ReplaceAll(tc.rows, "gone:pass:DISPOSED=skip ", "")
			if g := strings.Join(got, " "); g != want {
				t.Errorf("rows\n got %s\nwant %s", g, want)
			}
			if j.Verdict.Pass != tc.pass || j.Verdict.Plan != p.To {
				t.Errorf("pass=%v plan=%v", j.Verdict.Pass, j.Verdict.Plan)
			}
		})
	}
}
