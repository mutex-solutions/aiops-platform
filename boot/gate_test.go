package boot

import (
	"strings"
	"testing"
)

const gateMods = `
o/prov: {provides: [pk]}
o/other: {provides: [pk]}
o/cons: {needs: [ck]}
o/want: {wants: [pk]}
o/hook: {hooks: [{hook: sh, stage: 1}]}
o/open: {provides: [ok]}
o/needpk: {needs: [pk]}
o/wantck: {wants: [ck]}
o/ready: {provides: [ready]}
o/inthook: {hooks: [{hook: internal/plugin, stage: 0}]}
`

const gateDist = `
providers: {pk: [o/prov]}
consumers: {ck: [o/open]}
sealedHooks: {sh: [o/open]}
`

func TestGateChecksTable(t *testing.T) {
	cases := []struct {
		name    string
		bundle  string
		patches []string
		dist    string // replaces gateDist when set
		want    []string
		pass    bool
		refused bool
		detach  []string
		inact   []string
	}{
		{name: "clean", bundle: rows("id: a, plugin: o/prov", "id: b, plugin: o/open", "id: g, plugin: core:group, config: []"),
			pass: true},
		{name: "module missing optional", bundle: rows("id: a, plugin: x/none"),
			want: []string{"0 a module-missing stop=false"}, inact: []string{"a"}},
		{name: "module missing required row", bundle: rows("id: a, plugin: x/none, required: true"),
			want: []string{"0 a module-missing stop=true"}, refused: true},
		{name: "module missing named by the dist", bundle: rows("id: loader, plugin: x/none"), dist: "requiredRows: [loader]",
			want: []string{"0 loader module-missing stop=true"}, refused: true},
		{name: "disabled row with no module is inert", bundle: rows("id: a, plugin: x/none, enabled: false"), pass: true},
		{name: "provider outside binding", bundle: rows("id: a, plugin: o/other"),
			want: []string{"7 a provider-binding stop=false"}, detach: []string{"a"}},
		{name: "provider outside binding, required", bundle: rows("id: a, plugin: o/other, required: true"),
			want: []string{"7 a provider-binding stop=true"}, refused: true},
		{name: "consumer outside binding by need", bundle: rows("id: a, plugin: o/cons"),
			want: []string{"7 a consumer-binding stop=false"}, detach: []string{"a"}},
		{name: "consumer outside binding by inject", bundle: rows("id: a, plugin: o/prov, inject: [ck]"),
			want: []string{"7 a consumer-binding stop=false"}, detach: []string{"a"}},
		{name: "consumer in binding", bundle: rows("id: a, plugin: o/open, inject: [ck]"), pass: true},
		{name: "want on provider-bound key", bundle: rows("id: a, plugin: o/want"),
			want: []string{"7 a want-bound stop=false"}, detach: []string{"a"}},
		{name: "isolate by a plugin that is not the bound provider", bundle: rows("id: a, plugin: o/open, isolate: [pk]"),
			want: []string{"7 a isolate-bound stop=true"}, refused: true},
		{name: "isolate by the bound provider", bundle: rows("id: a, plugin: o/prov, isolate: [pk]"), pass: true},
		{name: "isolate by the bound provider on a required row", bundle: rows("id: a, plugin: o/prov, isolate: [pk], required: true"),
			want: []string{"7 a isolate-bound stop=true"}, refused: true},
		{name: "isolate of an open key", bundle: rows("id: a, plugin: o/open, isolate: [ok]"), pass: true},
		{name: "sealed hook", bundle: rows("id: a, plugin: o/hook"),
			want: []string{"7 a sealed-hook stop=false"}, detach: []string{"a"}},
		{name: "disabled rows are not checked by 7", bundle: rows("id: a, plugin: o/other, enabled: false", "id: b, plugin: o/prov"), pass: true},
		{name: "inject patched on a required row named by the dist", bundle: rows("id: loader, plugin: o/open"),
			patches: []string{"- {id: loader, inject: [x]}"}, dist: "requiredRows: [loader]",
			want: []string{"8 loader required-row-field stop=true"}, refused: true},
		{name: "isolate and group patched on a provider of a bound key", bundle: rows("id: a, plugin: o/prov"),
			patches: []string{"- {id: a, isolate: [zz], group: true}"},
			want:    []string{"8 a bound-provider-field stop=true", "8 a bound-provider-field stop=true"}, refused: true},
		{name: "fields set at insert are fine on a provider", bundle: rows("id: a, plugin: o/prov, inject: [zz]"), pass: true},
		{name: "intercept of a bound key in a patch", bundle: rows("id: a, plugin: o/open"),
			patches: []string{"- {id: a, intercept: {pk: {x: 1}}}"},
			want:    []string{"8 a intercept-bound stop=false"}, detach: []string{"a"}},
		{name: "intercept of a bound key in the bundle layer", bundle: rows("id: a, plugin: o/open, intercept: {pk: {x: 1}}"), pass: true},
		{name: "intercept of an open key in a patch", bundle: rows("id: a, plugin: o/open"),
			patches: []string{"- {id: a, intercept: {ok: {x: 1}}}"}, pass: true},
		{name: "last provider disabled", bundle: rows("id: a, plugin: o/prov", "id: b, plugin: o/prov"),
			patches: []string{"- {id: a, enabled: false}", "- {id: b, enabled: false}"},
			want:    []string{"8 a last-provider stop=true", "8 b last-provider stop=true"}, refused: true},
		{name: "one provider left", bundle: rows("id: a, plugin: o/prov", "id: b, plugin: o/prov"),
			patches: []string{"- {id: a, enabled: false}"}, pass: true},
		{name: "key not composed", bundle: rows("id: b, plugin: o/open"), pass: true},
		// A Want resolves through the consumer binding as a Need does.
		{name: "consumer outside binding by want", bundle: rows("id: a, plugin: o/wantck"),
			want: []string{"7 a consumer-binding stop=false"}, detach: []string{"a"}},
		// The root provides its keys and the kernel seals its hooks whatever dist.yaml lists.
		{name: "provider of a key of the root", bundle: rows("id: a, plugin: o/ready"),
			want: []string{"7 a provider-binding stop=false"}, detach: []string{"a"}},
		{name: "listener on a hook of the kernel that dist.yaml does not list", bundle: rows("id: a, plugin: o/inthook"),
			want: []string{"7 a sealed-hook stop=false"}, detach: []string{"a"}},
		{name: "listener on a hook of the kernel that dist.yaml lists", bundle: rows("id: a, plugin: o/inthook"),
			dist: "sealedHooks: {internal/plugin: [o/inthook]}", pass: true},
		// A row in a disabled group is not mounted, and a patch on a group reaches its children.
		{name: "last provider in a disabled group", bundle: rows("id: g, plugin: core:group, config: [{id: h, plugin: core:group, config: [{id: a, plugin: o/prov}]}]"),
			patches: []string{"- {id: g, enabled: false}"},
			want:    []string{"8 g:h:a last-provider stop=true"}, refused: true},
		{name: "inject patched on the group of a provider of a bound key", bundle: rows("id: g, plugin: core:group, config: [{id: a, plugin: o/prov}]"),
			patches: []string{"- {id: g, inject: [nothere]}"},
			want:    []string{"8 g:a bound-provider-field stop=true"}, refused: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dist := tc.dist
			if dist == "" {
				dist = gateDist
			}
			c := comp(t, tc.bundle, tc.patches...)
			g := Gate(c, mustMods(t, gateMods), mustDist(t, dist))
			if got := gateLines(g); !equalLines(got, tc.want) {
				t.Fatalf("failures\n got %q\nwant %q", got, tc.want)
			}
			if g.Pass != tc.pass || g.Refused != tc.refused {
				t.Errorf("Pass=%v Refused=%v, want %v %v", g.Pass, g.Refused, tc.pass, tc.refused)
			}
			if !equalStrings(g.Detached, tc.detach) || !equalStrings(g.Inactive, tc.inact) {
				t.Errorf("Detached=%v Inactive=%v, want %v %v", g.Detached, g.Inactive, tc.detach, tc.inact)
			}
			// The admitted copy disables exactly the rows with a failure, and keeps the digest.
			ad := g.Admitted(c)
			if ad.Digest != c.Digest {
				t.Error("Admitted changed the digest")
			}
			for i, r := range ad.Rows {
				_, denied := g.Denial(r.ID)
				if denied && r.Enabled || !denied && r.Enabled != c.Rows[i].Enabled {
					t.Errorf("row %s: denied=%v enabled=%v", r.ID, denied, r.Enabled)
				}
			}
		})
	}
}

func TestGateSlotsAndResult(t *testing.T) {
	c := comp(t, rows("id: a, plugin: o/other", "id: b, plugin: o/open", "id: m, plugin: x/none"))
	g := Gate(c, mustMods(t, gateMods), mustDist(t, gateDist))
	infos := GateChecks()
	if len(infos) != NumChecks || len(g.Slots) != NumChecks {
		t.Fatalf("want ten slots, got %d and %d", len(infos), len(g.Slots))
	}
	wantUntil := []string{"S2a", "S2a", "S2b", "S2a", "S2a", "S2a", "", "", "S2b", "S2a"}
	for i, s := range g.Slots {
		if s.N != i+1 || s.Until != wantUntil[i] || s.Evaluated != (wantUntil[i] == "") {
			t.Errorf("slot %d: %+v", i+1, s.CheckInfo)
		}
	}
	for _, tc := range []struct {
		check  int
		row    string
		status Status
		rule   Rule
		until  string
	}{
		{CheckKeyHookClass, "a", StatusFail, RuleProviderBinding, ""},
		{CheckKeyHookClass, "b", StatusPass, "", ""},
		{CheckLayerRules, "a", StatusPass, "", ""},
		{CheckManifest, "a", StatusNotEvaluated, "", "S2a"},
		{CheckRemoteProto, "b", StatusNotEvaluated, "", "S2b"},
		{0, "m", StatusFail, RuleModuleMissing, ""},
		{0, "a", StatusPass, "", ""},
		{CheckKeyHookClass, "nope", StatusNotEvaluated, "", ""},
		{11, "a", StatusNotEvaluated, "", ""},
		{-1, "a", StatusNotEvaluated, "", ""},
	} {
		r := g.Result(tc.check, tc.row)
		if r.Status != tc.status || r.Rule != tc.rule || r.Until != tc.until {
			t.Errorf("Result(%d, %q) = %+v", tc.check, tc.row, r)
		}
		if tc.status == StatusNotEvaluated && tc.until != "" && !strings.HasPrefix(r.Reason, "not evaluated until ") {
			t.Errorf("reason %q", r.Reason)
		}
	}
	if f, ok := g.Denial("m"); !ok || f.Check != 0 {
		t.Errorf("Denial(m) = %+v %v", f, ok)
	}
	if f, ok := g.Denial("a"); !ok || f.Check != CheckKeyHookClass {
		t.Errorf("Denial(a) = %+v %v", f, ok)
	}
	if _, ok := g.Denial("b"); ok {
		t.Error("b has no denial")
	}
	if g.Pass || g.Refused || len(g.Detached) != 1 || len(g.Inactive) != 1 {
		t.Errorf("verdict %+v", g)
	}
}
