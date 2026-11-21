// SPDX-License-Identifier: Apache-2.0

package loader

import (
	"testing"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

type m = map[string]any
type l = []any

// A volatile change of a config that holds or held a reference is not committed in
// place: the tree holds the reference unresolved, so the fiber restarts and
// config-refs resolves it at its activation, and loader/volatile-update never
// carries a reference (sdk/def/loader/v0 VolatileOnly).
func TestVolatileChangeWithAReferenceRestarts(t *testing.T) {
	p := newProbe()
	ls := newListener("t/listener")
	rv := row("v", "t/v")
	rv.Config = m{"note": "plain", "rules": l{m{"url": "a"}}}
	x := newFx(t, fxOpt{plugins: plugins(p.plugin("t/v", nil)), pre: []kernel.Plugin{ls.plugin}, rows: []def.EntrySpec{rv},
		modules: []launch.CatalogEntry{{ID: "t/v", Volatile: []string{"note", "rules"}}}})
	ref := launch.Ref{Scheme: "env", Name: "AIOPS_REF_A"}
	for i, to := range []m{
		{"note": ref, "rules": l{m{"url": "a"}}},                    // becomes a reference
		{"note": "plain", "rules": l{m{"url": "a"}}},                // was one
		{"note": "plain", "rules": l{m{"url": "a"}, m{"url": ref}}}, // holds one beneath
	} {
		rv.Config = to
		if res := x.apply(update(rv)); res[0].Outcome != def.Restarted {
			t.Fatalf("change %d: outcome %v", i, res)
		}
		x.want("v", kernel.Active)
	}
	ls.mu.Lock()
	if p.count("t/v") != 4 || len(ls.volatile) != 0 {
		t.Errorf("applies %d, volatile-update heard %+v", p.count("t/v"), ls.volatile)
	}
	ls.mu.Unlock()

	// A reference at a path that did not change restarts the fiber too: the event would
	// carry it in Config.
	rv.Config = m{"note": "plain", "rules": l{m{"url": ref}}}
	x.apply(update(rv))
	rv.Config = m{"note": "changed", "rules": l{m{"url": ref}}}
	if res := x.apply(update(rv)); res[0].Outcome != def.Restarted {
		t.Fatalf("a volatile change beside an unchanged reference: %v", res)
	}
	x.want("v", kernel.Active)
	// With no reference left on either side the change is committed in place.
	rv.Config = m{"note": "changed", "rules": l{m{"url": "b"}}}
	x.apply(update(rv))
	rv.Config = m{"note": "again", "rules": l{m{"url": "b"}}}
	if res := x.apply(update(rv)); res[0].Outcome != def.Committed {
		t.Fatalf("a volatile change with no reference: %v", res)
	}
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if p.count("t/v") != 7 || len(ls.volatile) != 1 || !deepEqual(ls.volatile[0].Config, rv.Config) {
		t.Errorf("applies %d, volatile-update heard %+v", p.count("t/v"), ls.volatile)
	}
}
