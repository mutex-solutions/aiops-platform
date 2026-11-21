package boot

import (
	"strings"
	"testing"
)

const reqMods = `
o/loader: {provides: [loader]}
o/sql: {provides: [sql]}
o/auth: {provides: [auth], needs: [sql, vault]}
o/svc: {provides: [svc], needs: [auth]}
o/extra: {provides: [extra]}
o/uses-extra: {needs: [extra, sql]}
o/web: {needs: [svc]}
aiops/config-refs: {provides: [configRefsReady]}
`

func TestRequiredFor(t *testing.T) {
	c := comp(t, rows("id: loader, plugin: o/loader, roles: [api, worker]", "id: sql, plugin: o/sql, roles: [worker]",
		"id: auth, plugin: o/auth, roles: [api], required: true", "id: web, plugin: o/web, roles: [edge], required: true"))
	mods := mustMods(t, reqMods)
	dist := mustDist(t, "requiredRows: [loader, ghost]\nrequiredKeys: [sql, auth, nowhere]")
	for _, tc := range []struct {
		roles      []Role
		rows, keys string
	}{
		{nil, "auth,ghost,loader,web", "auth,nowhere,sql"},
		{[]Role{RoleAPI}, "auth,ghost,loader", "auth,nowhere"},
		{[]Role{RoleWorker}, "ghost,loader", "nowhere,sql"},
		{[]Role{RoleEdge}, "ghost,web", "nowhere"},
	} {
		s := RequiredFor(c, mods, dist, tc.roles)
		if strings.Join(s.Rows, ",") != tc.rows || strings.Join(s.Keys, ",") != tc.keys {
			t.Errorf("roles %v: rows %v keys %v, want %s / %s", tc.roles, s.Rows, s.Keys, tc.rows, tc.keys)
		}
	}
}

func TestAuditStatic(t *testing.T) {
	cases := []struct {
		name   string
		bundle string
		dist   string
		roles  []Role
		want   []string // "rule rows keys"
		closed string   // static closure of the set
	}{
		{name: "met",
			bundle: rows("id: loader, plugin: o/loader", "id: sql, plugin: o/sql", "id: auth, plugin: o/auth"),
			dist:   "requiredRows: [loader]\nrequiredKeys: [sql, auth]", closed: "auth,loader,sql"},
		{name: "required row absent, required key without provider",
			bundle: rows("id: sql, plugin: o/sql"), dist: "requiredRows: [loader]\nrequiredKeys: [sql, auth]",
			want: []string{"required-missing loader ", "required-key  auth"}, closed: "sql"},
		{name: "required row disabled",
			bundle: rows("id: loader, plugin: o/loader, enabled: false"), dist: "requiredRows: [loader]",
			want: []string{"required-missing loader "}},
		{name: "required row inside a disabled group",
			bundle: rows("id: g, plugin: core:group, enabled: false, config: [{id: loader, plugin: o/loader, required: true}]"),
			want:   []string{"required-missing g:loader "}},
		{name: "closure holds a provider outside the required set",
			bundle: rows("id: u, plugin: o/uses-extra, required: true", "id: e, plugin: o/extra", "id: sql, plugin: o/sql"),
			dist:   "requiredKeys: [sql]", want: []string{"required-closure u,e extra"}, closed: "e,sql,u"},
		{name: "a provider in the required set is fine, and observability keys are never in a closure",
			bundle: rows("id: s, plugin: o/svc, required: true", "id: a, plugin: o/auth", "id: sql, plugin: o/sql"),
			dist:   "requiredKeys: [sql, auth]", closed: "a,s,sql"},
		{name: "a key of a private realm does not serve the process",
			bundle: rows("id: g, plugin: core:group, isolate: [sql], config: [{id: sql, plugin: o/sql}]"),
			dist:   "requiredKeys: [sql]", want: []string{"required-key  sql"}},
		// A required row that holds a reference pulls config-refs into the set, by its key
		// and its rows (ADR-059 decision 18); with no config-refs row the key is unmet.
		{name: "a required row with a reference pulls config-refs in",
			bundle: rows("id: refs, plugin: aiops/config-refs", "id: u, plugin: o/sql, required: true, config: {x: \"${env:AIOPS_REF_X}\"}"),
			closed: "refs,u"},
		{name: "a required row with a reference and no config-refs row",
			bundle: rows("id: u, plugin: o/sql, required: true, config: {x: \"${env:AIOPS_REF_X}\"}"),
			want:   []string{"required-key  configRefsReady"}, closed: "u"},
		// Only the config-refs rows that serve the process join the set: active and in
		// the root realm. A spare one that does not serve is not required.
		{name: "a disabled spare config-refs row is not required",
			bundle: rows("id: refs, plugin: aiops/config-refs", "id: spare, plugin: aiops/config-refs, enabled: false",
				"id: u, plugin: o/sql, required: true, config: {x: \"${env:AIOPS_REF_X}\"}"),
			closed: "refs,u"},
		{name: "a config-refs row inside a disabled group is not required",
			bundle: rows("id: refs, plugin: aiops/config-refs", "id: g, plugin: core:group, enabled: false, config: [{id: r2, plugin: aiops/config-refs}]",
				"id: u, plugin: o/sql, required: true, config: {x: \"${env:AIOPS_REF_X}\"}"),
			closed: "refs,u"},
		{name: "a config-refs row of a private realm is not required",
			bundle: rows("id: refs, plugin: aiops/config-refs", "id: g, plugin: core:group, isolate: [configRefsReady], config: [{id: r3, plugin: aiops/config-refs}]",
				"id: u, plugin: o/sql, required: true, config: {x: \"${env:AIOPS_REF_X}\"}"),
			closed: "refs,u"},
		{name: "a required row with a reference and only a disabled config-refs row",
			bundle: rows("id: spare, plugin: aiops/config-refs, enabled: false", "id: u, plugin: o/sql, required: true, config: {x: \"${env:AIOPS_REF_X}\"}"),
			want:   []string{"required-missing spare ", "required-key  configRefsReady"}, closed: "u"},
		// A provider of a required key is inside the set as a required row is, so its
		// reference pulls config-refs in too (ADR-059 decision 18); the closure clause
		// never hides that config-refs is outside the set.
		{name: "a provider of a required key that holds a reference",
			bundle: rows("id: refs, plugin: aiops/config-refs", "id: sql, plugin: o/sql, config: {x: \"${env:AIOPS_REF_X}\"}"),
			dist:   "requiredKeys: [sql]", closed: "refs,sql"},
		{name: "a provider of a required key with a reference and no config-refs row",
			bundle: rows("id: sql, plugin: o/sql, config: {x: \"${env:AIOPS_REF_X}\"}"),
			dist:   "requiredKeys: [sql]", want: []string{"required-key  configRefsReady"}, closed: "sql"},
		{name: "a provider of a required key with a reference and only a disabled config-refs row",
			bundle: rows("id: spare, plugin: aiops/config-refs, enabled: false", "id: sql, plugin: o/sql, config: {x: \"${env:AIOPS_REF_X}\"}"),
			dist:   "requiredKeys: [sql]", want: []string{"required-missing spare ", "required-key  configRefsReady"}, closed: "sql"},
		// A provider that does not serve the process, disabled or in a private realm, is
		// not inside the set, so its reference pulls nothing in.
		{name: "a provider that does not serve the process pulls nothing in",
			bundle: rows("id: sql, plugin: o/sql", "id: off, plugin: o/sql, enabled: false, config: {x: \"${env:AIOPS_REF_X}\"}",
				"id: g, plugin: core:group, isolate: [sql], config: [{id: s2, plugin: o/sql, config: {x: \"${env:AIOPS_REF_X}\"}}]"),
			dist: "requiredKeys: [sql]", closed: "sql"},
		{name: "role drops a row and a key that run elsewhere",
			bundle: rows("id: loader, plugin: o/loader, roles: [worker], required: true", "id: sql, plugin: o/sql, roles: [worker]"),
			dist:   "requiredKeys: [sql]", roles: []Role{RoleAPI}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, mods, dist := comp(t, tc.bundle), mustMods(t, reqMods), mustDist(t, tc.dist)
			g, _, _ := BuildGraph(c, mods, dist, tc.roles)
			var got []string
			for _, e := range AuditStatic(c, mods, dist, g, tc.roles) {
				if e.Stage != "required set" || e.Msg == "" {
					t.Errorf("shape %+v", e)
				}
				got = append(got, string(e.Rule)+" "+strings.Join(e.Rows, ",")+" "+strings.Join(e.Keys, ","))
			}
			if !equalLines(got, tc.want) {
				t.Errorf("got %q want %q", got, tc.want)
			}
			set := RequiredFor(c, mods, dist, tc.roles)
			if cl := strings.Join(RequiredClosure(g, set), ","); cl != tc.closed {
				t.Errorf("closure %q, want %q", cl, tc.closed)
			}
		})
	}
}

func TestAuditRequired(t *testing.T) {
	c := comp(t, rows("id: loader, plugin: o/loader", "id: sql, plugin: o/sql", "id: auth, plugin: o/auth",
		"id: web, plugin: o/web", "id: svc, plugin: o/svc", "id: off, plugin: o/extra, enabled: false"))
	mods := mustMods(t, reqMods)
	dist := mustDist(t, "requiredRows: [loader, off]\nrequiredKeys: [sql, auth]")
	g, errs, _ := BuildGraph(c, mods, dist, nil)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	set := RequiredFor(c, mods, dist, nil)
	for _, tc := range []struct {
		name  string
		view  StateView
		errs  []string
		warn  string
		stops bool
	}{
		{name: "all active", view: StateView{
			Rows:      map[string]State{"loader": StateActive, "sql": StateActive, "auth": StateActive, "web": StateActive, "svc": StateActive},
			Providers: map[string]bool{"sql": true, "auth": true}}},
		{name: "required row and key unmet, others grouped",
			view: StateView{
				Rows:      map[string]State{"loader": StatePending, "sql": StateActive, "auth": StateFailed, "web": StatePending},
				Providers: map[string]bool{"sql": true, "svc": false}},
			errs: []string{`required row "loader" is PENDING, not ACTIVE`, `required key "auth" has no ACTIVE provider`},
			warn: `3 rows are not ACTIVE: "auth" is FAILED and waits for vault; "svc" is without a fiber and waits for auth; "web" is PENDING and waits for svc`},
		{name: "a required row without a fiber",
			view: StateView{Rows: map[string]State{"sql": StateActive, "auth": StateActive, "svc": StateActive, "web": StateActive},
				Providers: map[string]bool{"sql": true, "auth": true, "vault": true, "svc": true}},
			errs: []string{`required row "loader" is without a fiber, not ACTIVE`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs, warns := AuditRequired(g, set, tc.view)
			var got []string
			for _, e := range errs {
				got = append(got, e.Msg)
			}
			if !equalLines(got, tc.errs) {
				t.Errorf("errors\n got %q\nwant %q", got, tc.errs)
			}
			switch {
			case tc.warn == "" && len(warns) != 0:
				t.Errorf("unexpected warnings %v", warns)
			case tc.warn != "" && (len(warns) != 1 || warns[0].Msg != tc.warn):
				t.Errorf("warnings %v\nwant %s", warns, tc.warn)
			}
		})
	}
}
