package boot

import (
	"strings"
	"testing"
)

const graphMods = `
o/a: {provides: [ka], needs: [kb]}
o/b: {provides: [kb], needs: [kc]}
o/c: {provides: [kc], needs: [ka]}
o/leaf: {provides: [kl]}
o/user: {needs: [kl]}
o/self: {provides: [ks], needs: [ks]}
o/p1: {provides: [kp]}
o/needp: {needs: [kp]}
o/grp: {provides: [kg]}
o/twokeys: {needs: [kl, kp]}
o/both: {provides: [kl, kp]}
`

func TestGraphFindings(t *testing.T) {
	cases := []struct {
		name   string
		bundle string
		dist   string
		roles  []Role
		errs   []string // rule + rows + keys, one per error
		warn   []string // rows of each grouped warning
		active []string
	}{
		{name: "no findings", bundle: rows("id: l, plugin: o/leaf", "id: u, plugin: o/user"), active: []string{"l", "u"}},
		{name: "cycle of three, printed from its smallest id",
			bundle: rows("id: z, plugin: o/a", "id: y, plugin: o/b", "id: x, plugin: o/c", "id: l, plugin: o/leaf"),
			errs:   []string{"cycle rows=x,z,y keys=ka,kb,kc"}, active: []string{"l", "x", "y", "z"}},
		{name: "self cycle", bundle: rows("id: s, plugin: o/self"), errs: []string{"cycle rows=s keys=ks"}, active: []string{"s"}},
		{name: "two providers in the root realm", bundle: rows("id: q, plugin: o/p1", "id: p, plugin: o/p1"),
			errs: []string{"two-providers rows=p,q keys=kp"}, active: []string{"p", "q"}},
		{name: "a disabled provider is no second provider", bundle: rows("id: q, plugin: o/p1", "id: p, plugin: o/p1, enabled: false"),
			active: []string{"q"}},
		{name: "a provider of another role is no second provider", bundle: rows("id: q, plugin: o/p1, roles: [api]", "id: p, plugin: o/p1, roles: [worker]"),
			roles: []Role{RoleAPI}, active: []string{"q"}},
		{name: "isolate gives each group its own realm",
			bundle: rows("id: g1, plugin: core:group, isolate: [kp], config: [{id: p, plugin: o/p1}, {id: n, plugin: o/needp}]",
				"id: g2, plugin: core:group, isolate: [kp], config: [{id: p, plugin: o/p1}, {id: n, plugin: o/needp}]",
				"id: root, plugin: o/p1"),
			active: []string{"g1", "g1:n", "g1:p", "g2", "g2:n", "g2:p", "root"}},
		{name: "a private realm does not see the root provider",
			bundle: rows("id: root, plugin: o/p1", "id: g, plugin: core:group, isolate: [kp], config: [{id: n, plugin: o/needp}]"),
			warn:   []string{"g:n"}, active: []string{"g", "g:n", "root"}},
		{name: "two providers inside one private realm",
			bundle: rows("id: g, plugin: core:group, isolate: [kp], config: [{id: a, plugin: o/p1}, {id: b, plugin: o/p1}]"),
			errs:   []string{"two-providers rows=g:a,g:b keys=kp"}, active: []string{"g", "g:a", "g:b"}},
		{name: "need with no provider, optional: one grouped warning", bundle: rows("id: u1, plugin: o/user", "id: u2, plugin: o/user"),
			warn: []string{"u1,u2"}, active: []string{"u1", "u2"}},
		{name: "need with no provider, required consumer", bundle: rows("id: u, plugin: o/user, required: true"),
			errs: []string{"no-provider rows=u keys=kl"}, active: []string{"u"}},
		{name: "need with no provider, required key", bundle: rows("id: u, plugin: o/user"), dist: "requiredKeys: [kl]",
			errs: []string{"no-provider rows=u keys=kl"}, active: []string{"u"}},
		// A provider of a required key in the root realm is a required row (ADR-059
		// decision 27): its need of a key whose provider runs in another role refuses the
		// profile, where boot would stop later on that provider left PENDING.
		{name: "need with no provider in the role, provider of a required key",
			bundle: rows("id: a, plugin: o/a, roles: [api]", "id: b, plugin: o/b, roles: [worker]"), dist: "requiredKeys: [ka]",
			roles: []Role{RoleAPI}, errs: []string{"no-provider rows=a keys=kb"}, active: []string{"a"}},
		{name: "need with no provider, provider of a required key in a private realm",
			bundle: rows("id: g, plugin: core:group, isolate: [ka], config: [{id: a, plugin: o/a}]"), dist: "requiredKeys: [ka]",
			warn: []string{"g:a"}, active: []string{"g", "g:a"}},
		{name: "row-level inject is a need", bundle: rows("id: l, plugin: o/leaf, inject: [kp]"),
			warn: []string{"l"}, active: []string{"l"}},
		{name: "group inject is a need of its children",
			bundle: rows("id: g, plugin: core:group, inject: [kp], config: [{id: c, plugin: o/leaf}]"),
			warn:   []string{"g"}, active: []string{"g", "g:c"}},
		{name: "a child that provides what its group injects is a cycle",
			bundle: rows("id: g, plugin: core:group, inject: [kp], config: [{id: c, plugin: o/p1}]"),
			errs:   []string{"cycle rows=g,g:c keys=kp"}, active: []string{"g", "g:c"}},
		{name: "a disabled group takes its children out",
			bundle: rows("id: g, plugin: core:group, enabled: false, config: [{id: c, plugin: o/leaf}]"), active: nil},
		{name: "a group of another role takes its children out",
			bundle: rows("id: g, plugin: core:group, roles: [edge], config: [{id: c, plugin: o/leaf, roles: [api]}]"),
			roles:  []Role{RoleAPI}, active: nil},
		{name: "a row with no roles runs in every role", bundle: rows("id: l, plugin: o/leaf"), roles: []Role{RoleWorker}, active: []string{"l"}},
		{name: "a plugin outside the module table has no declarations", bundle: rows("id: m, plugin: x/none"), active: []string{"m"}},
		{name: "two keys between the same two rows give two edges", bundle: rows("id: b, plugin: o/both", "id: t, plugin: o/twokeys"),
			active: []string{"b", "t"}},
		{name: "binding names a plugin outside the module table", bundle: rows("id: l, plugin: o/leaf"),
			dist: "providers: {kz: [x/gone]}\nconsumers: {ky: [x/gone2]}",
			errs: []string{"binding-broken rows= keys=kz", "binding-broken rows= keys=ky"}, active: []string{"l"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := comp(t, tc.bundle)
			g, errs, warns := BuildGraph(c, mustMods(t, graphMods), mustDist(t, tc.dist), tc.roles)
			var got []string
			for _, e := range errs {
				if e.Stage != "static graph" || !strings.Contains(e.Error(), string(e.Rule)) || e.Msg == "" {
					t.Errorf("error shape: %+v", e)
				}
				got = append(got, string(e.Rule)+" rows="+strings.Join(e.Rows, ",")+" keys="+strings.Join(e.Keys, ","))
			}
			if !equalLines(got, tc.errs) {
				t.Errorf("errors\n got %q\nwant %q", got, tc.errs)
			}
			var gw []string
			for _, w := range warns {
				gw = append(gw, strings.Join(w.Rows, ","))
				if !strings.Contains(w.String(), "waits for") {
					t.Errorf("warning %q does not say what it waits for", w)
				}
			}
			if !equalLines(gw, tc.warn) {
				t.Errorf("warnings got %q want %q", gw, tc.warn)
			}
			if !equalLines(g.Active, tc.active) {
				t.Errorf("active got %q want %q", g.Active, tc.active)
			}
		})
	}
}

// The cycle that is printed is the shortest one, whichever way the rows are
// written, and among equal ones the first by its smallest ids.
func TestShortestCycleIsDeterministic(t *testing.T) {
	mods := mustMods(t, `
o/n1: {provides: [k1], needs: [k2, k3]}
o/n2: {provides: [k2], needs: [k4]}
o/n3: {provides: [k3], needs: [k1, kleaf]}
o/n4: {provides: [k4], needs: [k1]}
o/tail: {needs: [k1]}
o/leafp: {provides: [kleaf]}
o/x1: {provides: [kx1], needs: [kx2]}
o/x2: {provides: [kx2], needs: [kx1]}
`)
	forward := rows("id: a, plugin: o/n1", "id: b, plugin: o/n2", "id: c, plugin: o/n3", "id: d, plugin: o/n4", "id: t, plugin: o/tail", "id: leaf, plugin: o/leafp", "id: y1, plugin: o/x1", "id: y2, plugin: o/x2")
	reverse := rows("id: y2, plugin: o/x2", "id: y1, plugin: o/x1", "id: leaf, plugin: o/leafp", "id: t, plugin: o/tail", "id: d, plugin: o/n4", "id: c, plugin: o/n3", "id: b, plugin: o/n2", "id: a, plugin: o/n1")
	var msgs []string
	for _, b := range []string{forward, reverse} {
		_, errs, _ := BuildGraph(comp(t, b), mods, DistManifest{}, nil)
		if len(errs) != 1 || errs[0].Rule != RuleCycle {
			t.Fatalf("errors: %+v", errs)
		}
		// Two cycles of two rows exist, (a c) and (y1 y2); the first by id wins.
		msgs = append(msgs, errs[0].Msg)
	}
	want := `dependency cycle of 2 rows: "a" -(k3)-> "c" -(k1)-> "a"`
	if msgs[0] != want || msgs[1] != want {
		t.Errorf("got\n%s\n%s\nwant\n%s", msgs[0], msgs[1], want)
	}
}

func TestDependants(t *testing.T) {
	mods := mustMods(t, `
o/base: {provides: [kbase]}
o/mid: {provides: [kmid], needs: [kbase]}
o/top: {needs: [kmid]}
o/other: {needs: [kbase]}
o/island: {}
`)
	c := comp(t, rows("id: base, plugin: o/base", "id: mid, plugin: o/mid", "id: top, plugin: o/top", "id: other, plugin: o/other",
		"id: island, plugin: o/island", "id: grp, plugin: core:group, config: [{id: kid, plugin: o/top}]"))
	g, errs, _ := BuildGraph(c, mods, DistManifest{}, nil)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	for id, want := range map[string]string{
		"base":   "grp:kid,mid,other,top",
		"mid":    "grp:kid,top",
		"top":    "",
		"island": "",
		"grp":    "grp:kid",
		"nope":   "",
	} {
		if got := strings.Join(g.Dependants(id), ","); got != want {
			t.Errorf("Dependants(%s) = %q, want %q", id, got, want)
		}
	}
	if got := g.NeedsOf("mid"); !equalStrings(got, []string{"kbase"}) {
		t.Errorf("NeedsOf = %v", got)
	}
	if got := g.ProvidedBy("o/mid"); !equalStrings(got, []string{"kmid"}) {
		t.Errorf("ProvidedBy = %v", got)
	}
	if got := g.ConsumersOfKeys([]string{"kbase", "kmid"}); len(got) != 4 {
		t.Errorf("ConsumersOfKeys = %v", got)
	}
}

func TestActiveRowsAndRoleFilter(t *testing.T) {
	c := comp(t, rows("id: a, plugin: o/leaf, roles: [api]", "id: b, plugin: o/leaf, roles: [edge, worker]", "id: c, plugin: o/leaf, enabled: false"))
	for _, tc := range []struct {
		roles []Role
		want  string
	}{
		{nil, "a,b"},
		{[]Role{RoleAPI}, "a"},
		{[]Role{RoleEdge, RoleAPI}, "a,b"},
		{[]Role{RoleWorker}, "b"},
	} {
		var got []string
		for id, on := range ActiveRows(c, tc.roles) {
			if on {
				got = append(got, id)
			}
		}
		if s := strings.Join(sortedStrings(got), ","); s != tc.want {
			t.Errorf("roles %v: %s, want %s", tc.roles, s, tc.want)
		}
	}
}

func sortedStrings(ss []string) []string {
	m := map[string]bool{}
	for _, s := range ss {
		m[s] = true
	}
	return sortedKeys(m)
}
