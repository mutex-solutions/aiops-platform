package boot

import (
	"strings"
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
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
o/entryinit: {hooks: [{hook: loader/entry-init, stage: 0}]}
o/needloader: {needs: [loader]}
o/wantregistry: {wants: [registry]}
o/wantready: {wants: [ready]}
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
		// The launch keys are provider-bound to the launcher, which is no row: a Want of one
		// and an isolate of one are refused as for any provider-bound key.
		{name: "want of a key of the root", bundle: rows("id: a, plugin: o/wantready"),
			want: []string{"7 a want-bound stop=false"}, detach: []string{"a"}},
		{name: "isolate of a key of the root", bundle: rows("id: a, plugin: o/open, isolate: [ready]"),
			want: []string{"7 a isolate-bound stop=true"}, refused: true},
		{name: "listener on a hook of the kernel that dist.yaml does not list", bundle: rows("id: a, plugin: o/inthook"),
			want: []string{"7 a sealed-hook stop=false"}, detach: []string{"a"}},
		{name: "listener on a hook of the kernel that dist.yaml lists", bundle: rows("id: a, plugin: o/inthook"),
			dist: "sealedHooks: {internal/plugin: [o/inthook]}", pass: true},
		// So are the keys and hooks of the definitions boot imports, and the kernel's registry.
		{name: "listener on a hook of the loader that dist.yaml does not list", bundle: rows("id: a, plugin: o/entryinit"),
			want: []string{"7 a sealed-hook stop=false"}, detach: []string{"a"}},
		{name: "consumer of the loader that dist.yaml does not list", bundle: rows("id: a, plugin: o/needloader"),
			want: []string{"7 a consumer-binding stop=false"}, detach: []string{"a"}},
		{name: "consumer of the registry that dist.yaml does not list", bundle: rows("id: a, plugin: o/wantregistry"),
			want: []string{"7 a consumer-binding stop=false"}, detach: []string{"a"}},
		{name: "consumer of the loader that dist.yaml lists", bundle: rows("id: a, plugin: o/needloader"),
			dist: "consumers: {loader: [o/needloader]}", pass: true},
		// A row in a disabled group is not mounted, and a patch on a group reaches its children.
		{name: "last provider in a disabled group", bundle: rows("id: g, plugin: core:group, config: [{id: h, plugin: core:group, config: [{id: a, plugin: o/prov}]}]"),
			patches: []string{"- {id: g, enabled: false}"},
			want:    []string{"8 g:h:a last-provider stop=true"}, refused: true},
		{name: "inject patched on the group of a provider of a bound key", bundle: rows("id: g, plugin: core:group, config: [{id: a, plugin: o/prov}]"),
			patches: []string{"- {id: g, inject: [nothere]}"},
			want:    []string{"8 g:a bound-provider-field stop=true"}, refused: true},
		{name: "inject patched on the group of a required row", bundle: rows("id: g, plugin: core:group, config: [{id: x, plugin: o/open}]"),
			patches: []string{"- {id: g, inject: [nothere]}"}, dist: "requiredRows: [g:x]",
			want: []string{"8 g:x required-row-field stop=true"}, refused: true},
		// The intercept of a bound key is the bundle's: a later patch may not add, change or drop it.
		{name: "a patch drops the bundle intercept of a bound key", bundle: rows("id: a, plugin: o/open, intercept: {pk: {x: 1}, ok: {y: 1}}"),
			patches: []string{"- {id: a, intercept: {ok: {y: 2}}}"},
			want:    []string{"8 a intercept-bound stop=false"}, detach: []string{"a"}},
		{name: "a patch empties the intercept", bundle: rows("id: a, plugin: o/open, intercept: {pk: {x: 1}}"),
			patches: []string{"- {id: a, intercept: {}}"},
			want:    []string{"8 a intercept-bound stop=false"}, detach: []string{"a"}},
		{name: "a patch changes the intercept of a bound key", bundle: rows("id: a, plugin: o/open, intercept: {pk: {x: 1}}"),
			patches: []string{"- {id: a, intercept: {pk: {x: 2}}}"},
			want:    []string{"8 a intercept-bound stop=false"}, detach: []string{"a"}},
		{name: "a patch keeps the bound intercept and changes an open one", bundle: rows("id: a, plugin: o/open, intercept: {pk: {x: 1}, ok: {y: 1}}"),
			patches: []string{"- {id: a, intercept: {pk: {x: 1}, ok: {y: 2}}}", "- {id: a, intercept: {pk: {x: 1}}}"}, pass: true},
		{name: "a bundle patch drops the intercept of a bound key", bundle: rows("id: a, plugin: o/open, intercept: {pk: {x: 1}}") + "- {id: a, intercept: {}}\n",
			pass: true},
		{name: "a row inserted by a patch intercepts a bound key", bundle: rows("id: b, plugin: o/open"),
			patches: []string{"- insert: [{id: a, plugin: o/open, intercept: {pk: {x: 1}}}]"},
			want:    []string{"8 a intercept-bound stop=false"}, detach: []string{"a"}},
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

// Gate check 7 takes the classes of keys from their definitions, through the plugins of
// the module table (ADR-059 decision 20): dist.yaml lists who is allowed, and a bound key
// it does not list admits nobody, while a binding it lists for an open key binds nothing.
func TestGateReadsClassesFromTheDefinitions(t *testing.T) {
	cbKey := kernel.NewKey[string]("cb", 0, kernel.ConsumerBound())
	// Hooks that plugin definitions declare: the module table holds a listener of each as
	// a HookUse of its Plugin.Hooks, so the seal comes from the definition too.
	sealedEv := kernel.NewEvent[int]("d20/sealed", kernel.OwnedBy(storeKey), kernel.Sealed())
	openEv := kernel.NewEvent[int]("d20/open", kernel.OwnedBy(storeKey))
	listener := func(id string, h kernel.HookUse) ModuleEntry {
		return ModuleEntry{ID: id, Plugin: kernel.Define(kernel.Plugin{ID: id, Hooks: []kernel.HookUse{h}}),
			Hooks: []HookDecl{{Hook: h.Hook(), StageName: h.Stage()}}}
	}
	mods := ModuleTable{}
	for _, e := range []ModuleEntry{
		listener("o/sealed-listener", sealedEv.Use(kernel.DefaultStage)),
		listener("o/open-listener", openEv.Use(kernel.DefaultStage)),
		// Entries read from modules.yaml carry the classes of the definitions instead.
		{ID: "o/yaml-bound", Provides: []string{"ypk"}, Needs: []string{"ycb"}, Hooks: []HookDecl{{Hook: "y/sealed"}},
			Classes: &ModuleClasses{ProviderBound: []string{"ypk"}, ConsumerBound: []string{"ycb"}, SealedHooks: []string{"y/sealed"}}},
		{ID: "o/yaml-open", Provides: []string{"yok"}, Hooks: []HookDecl{{Hook: "y/open"}}, Classes: &ModuleClasses{}},
		toy(toySpec{id: "o/policy", provides: []kernel.AnyKey{policyKey}}),
		toy(toySpec{id: "o/cbprov", provides: []kernel.AnyKey{cbKey}}),
		toy(toySpec{id: "o/cbuser", needs: []kernel.AnyKey{cbKey}}),
		toy(toySpec{id: "o/store", provides: []kernel.AnyKey{storeKey}}),
		{ID: "o/plain"}, // no Go value: what dist.yaml lists stands for the class
		// A zero Dep has no key and no class; the kernel refuses it at mount.
		{ID: "o/zero", Plugin: kernel.Define(kernel.Plugin{ID: "o/zero", Inject: []kernel.Dep{{}}})},
	} {
		mods[e.ID] = e
	}
	for _, tc := range []struct {
		name, bundle, dist string
		want               []string
	}{
		{"provider-bound by its definition, not listed", rows("id: a, plugin: o/policy"), "",
			[]string{"7 a provider-binding stop=false"}},
		{"provider-bound by its definition, listed", rows("id: a, plugin: o/policy"), "providers: {policy: [o/policy]}", nil},
		{"consumer-bound by its definition, not listed", rows("id: p, plugin: o/cbprov", "id: a, plugin: o/cbuser"), "",
			[]string{"7 a consumer-binding stop=false"}},
		{"consumer-bound by its definition, listed", rows("id: p, plugin: o/cbprov", "id: a, plugin: o/cbuser"), "consumers: {cb: [o/cbuser]}", nil},
		{"isolate of a key bound by its definition", rows("id: a, plugin: o/plain, isolate: [policy]"), "",
			[]string{"7 a isolate-bound stop=true"}},
		// The kernel refuses a binding of an open key; the gate holds no row to it.
		{"open by its definition, listed", rows("id: a, plugin: o/store"), "providers: {store: [o/plain]}", nil},
		{"open by its definition, listed, intercepted in a patch", rows("id: a, plugin: o/plain"), "providers: {store: [o/plain]}", nil},
		{"a plugin with a zero dependency", rows("id: a, plugin: o/zero"), "", nil},
		// The kernel refuses the listener of a sealed hook that Config.Listeners omits, so
		// the gate does too; an open hook stays open whoever dist.yaml lists for it.
		{"sealed by its definition, not listed", rows("id: a, plugin: o/sealed-listener"), "",
			[]string{"7 a sealed-hook stop=false"}},
		{"sealed by its definition, listed", rows("id: a, plugin: o/sealed-listener"), "sealedHooks: {d20/sealed: [o/sealed-listener]}", nil},
		{"open by its definition, listed for another plugin", rows("id: a, plugin: o/open-listener"), "sealedHooks: {d20/open: [o/plain]}", nil},
		{"bound and sealed by the classes of modules.yaml, not listed", rows("id: a, plugin: o/yaml-bound"), "",
			[]string{"7 a provider-binding stop=false", "7 a consumer-binding stop=false", "7 a sealed-hook stop=false"}},
		{"open by the classes of modules.yaml, listed", rows("id: a, plugin: o/yaml-open"),
			"providers: {yok: [o/plain]}\nsealedHooks: {y/open: [o/plain]}", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var patches []string
			if strings.Contains(tc.name, "patch") {
				patches = []string{"- {id: a, intercept: {store: {x: 1}}}"}
			}
			g := Gate(comp(t, tc.bundle, patches...), mods, mustDist(t, tc.dist))
			if got := gateLines(g); !equalLines(got, tc.want) {
				t.Errorf("failures %q, want %q", got, tc.want)
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
	// Checks 3 and 9 are S2b everywhere; the S1 form of profile render evaluates 7 and 8
	// only, and says why for each other check and each clause it leaves out.
	wantUntil := []string{"", "", "S2b", "", "", "", "", "", "S2b", ""}
	for i, s := range g.Slots {
		evaluated := i+1 == CheckKeyHookClass || i+1 == CheckLayerRules
		if s.N != i+1 || s.Until != wantUntil[i] || s.Evaluated != evaluated || !evaluated && s.Until == "" && s.Reason == "" {
			t.Errorf("slot %d: %+v", i+1, s)
		}
	}
	if c := g.Slots[CheckKeyHookClass-1].Clauses; len(c) != 2 || !strings.HasPrefix(c[0], "fake: ") || !strings.HasPrefix(c[1], "signed: ") {
		t.Errorf("clauses of check 7: %q", c)
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
		{CheckManifest, "a", StatusNotEvaluated, "", ""},
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
		if tc.status == StatusNotEvaluated && tc.until != "" && !strings.HasPrefix(r.Reason, "not evaluated until ") ||
			tc.check == CheckManifest && !strings.Contains(r.Reason, "stand-ins") {
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
