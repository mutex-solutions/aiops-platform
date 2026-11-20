package boot

import (
	"strings"
	"testing"
)

const admitMods = `
o/sql: {provides: [sql]}
o/auth: {provides: [auth], needs: [sql]}
o/app: {needs: [auth]}
o/cyc1: {provides: [c1], needs: [c2]}
o/cyc2: {provides: [c2], needs: [c1]}
o/rogue: {provides: [auth]}
o/loader: {provides: [loader]}
`

const admitDist = `
providers: {auth: [o/auth]}
requiredKeys: [sql, auth]
requiredRows: [loader]
`

const admitBase = "id: loader, plugin: o/loader, required: true"

// One case per row of the failure matrix of 08-profiles-roadmap-adrs.md 1.6
// that is decided before any fiber mounts. The diagnostic is the substring the
// matrix file expects, and refused is "boot stops".
func TestAdmitFailureMatrixRows(t *testing.T) {
	ok := []string{admitBase, "id: sql, plugin: o/sql", "id: auth, plugin: o/auth"}
	with := func(more ...string) []string { return append(append([]string(nil), ok...), more...) }
	cases := []struct {
		name       string
		specs      []string
		patch      string
		dist       string
		refused    bool
		diagnostic string
		absent     string
	}{
		{name: "baseline", specs: with("id: app, plugin: o/app")},
		{name: "plugin id not in the module table, optional row",
			specs: with("id: ghost, plugin: x/ghost"), diagnostic: "not in the module table"},
		{name: "plugin id not in the module table, required row",
			specs: with("id: ghost, plugin: x/ghost, required: true"), refused: true, diagnostic: "not in the module table"},
		{name: "gate denies an optional row: detached",
			specs: with("id: rogue, plugin: o/rogue"), diagnostic: "detached as disabled"},
		{name: "gate denies a required row: stop",
			specs: with("id: rogue, plugin: o/rogue, required: true"), refused: true, diagnostic: "denied"},
		{name: "static graph: cycle, optional row",
			specs: with("id: c1, plugin: o/cyc1", "id: c2, plugin: o/cyc2"), refused: true, diagnostic: "static graph: cycle"},
		{name: "static graph: two providers of one key in a realm",
			specs: with("id: sql2, plugin: o/sql"), refused: true, diagnostic: "static graph: two-providers"},
		{name: "static graph: a broken binding",
			specs: ok, dist: "providers: {auth: [o/auth, o/nowhere]}", refused: true, diagnostic: "static graph: binding-broken"},
		{name: "static graph: isolate on a bound key, optional row",
			specs: with("id: iso, plugin: o/app, isolate: [auth]"), refused: true, diagnostic: "static graph"},
		{name: "the last provider of a bound key disabled",
			specs:   []string{admitBase, "id: sql, plugin: o/sql", "id: auth, plugin: o/auth, enabled: false"},
			refused: true, diagnostic: "last-provider"},
		{name: "required key without a provider",
			specs: []string{admitBase, "id: auth, plugin: o/auth"}, refused: true, diagnostic: `required key "sql" has no provider`},
		{name: "required row absent",
			specs: []string{"id: sql, plugin: o/sql", "id: auth, plugin: o/auth"}, refused: true, diagnostic: `required row "loader" is absent`},
		{name: "required row disabled by a patch",
			specs: ok, patch: "- {id: loader, enabled: false}", refused: true, diagnostic: `required row "loader" is disabled`},
		{name: "injected service has no provider, optional rows: one grouped warning",
			specs:      with("id: a1, plugin: o/app, inject: [nope]", "id: a2, plugin: o/app, inject: [nope2]"),
			diagnostic: "2 optional rows would be PENDING", absent: "error:"},
		{name: "injected service has no provider, required row",
			specs: []string{admitBase, "id: sql, plugin: o/sql", "id: app, plugin: o/app, required: true"}, refused: true,
			diagnostic: "static graph: no-provider"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dist := tc.dist
			if dist == "" {
				dist = admitDist
			} else {
				dist = strings.Replace(admitDist, "providers: {auth: [o/auth]}", dist, 1)
			}
			var patches []string
			if tc.patch != "" {
				patches = append(patches, tc.patch)
			}
			c := comp(t, rows(tc.specs...), patches...)
			a := Admit(c, mustMods(t, admitMods), mustDist(t, dist), nil)
			text := strings.Join(a.Diagnostics(), "\n")
			if a.Refused() != tc.refused {
				t.Errorf("Refused = %v, want %v\n%s", a.Refused(), tc.refused, text)
			}
			if tc.diagnostic == "" && text != "" {
				t.Errorf("unexpected diagnostics:\n%s", text)
			}
			if !strings.Contains(text, tc.diagnostic) {
				t.Errorf("diagnostics lack %q:\n%s", tc.diagnostic, text)
			}
			if tc.absent != "" && strings.Contains(text, tc.absent) {
				t.Errorf("diagnostics hold %q:\n%s", tc.absent, text)
			}
			if a.Comp.Digest != c.Digest || a.Admitted.Digest != c.Digest {
				t.Error("admission changed the digest")
			}
		})
	}
}

// The id of the loader's row is boot's: a composition that holds it is refused,
// and the copy WithLoader makes of one that does not is what boot admits.
func TestCheckReserved(t *testing.T) {
	c := comp(t, rows("id: a, plugin: o/a"))
	if err := CheckReserved(c); err != nil {
		t.Fatal(err)
	}
	clash := comp(t, rows("id: a, plugin: o/a"), "- insert: [{id: loader, plugin: o/a}]")
	if err := CheckReserved(clash); err == nil || !strings.Contains(err.Error(), `the row id "loader" is reserved`) {
		t.Fatalf("got %v", err)
	}
}

// A malformed, missing or empty layer is refused by Compose, before admission.
func TestMalformedLayerIsRefusedByCompose(t *testing.T) {
	for _, bundle := range []string{"", "not: a list", "- {id: x, plugin: o/a}"} {
		if _, err := tryComp(bundle); err == nil {
			t.Errorf("%q composed", bundle)
		}
	}
}
