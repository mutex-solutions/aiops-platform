// SPDX-License-Identifier: Apache-2.0

package loader

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// A replacement disposes the running fiber and mounts its entry again. The new config
// is committed to the running fiber in the batch that disposes it, so the kernel
// resolves it through internal/config and Decode first: a config it refuses refuses the
// plan whole and the row keeps its instance (08-profiles-roadmap-adrs.md §1.6, "Config
// fails schema or Validate; a reference does not resolve": live, "Rejected; the row
// keeps its instance"). That is the case of a row that gains its first reference, whose
// inject gains configRefsReady.
func TestReplacementRefusesAConfigTheKernelDoesNotResolve(t *testing.T) {
	k := kernel.NewKey[int]("k", 0)
	prov := kernel.Define(kernel.Plugin{ID: "t/prov", Provides: []kernel.AnyKey{k}, Apply: func(c *kernel.Context, _ any) error { return k.Provide(c, 1) }})
	var applies, disposed atomic.Int32
	dec := kernel.Define(kernel.Plugin{ID: "t/dec", Decode: func(raw any) (any, error) {
		if raw == "bad" {
			return nil, errors.New("config refused")
		}
		return raw, nil
	}, Apply: func(c *kernel.Context, _ any) error {
		applies.Add(1)
		c.Defer("count", func(context.Context) error { disposed.Add(1); return nil })
		return nil
	}})
	rz := row("z", "t/dec")
	rz.Config = "good"
	x := newFx(t, fxOpt{plugins: plugins(dec), keys: map[string]kernel.AnyKey{"k": k}, pre: []kernel.Plugin{prov}, rows: []def.EntrySpec{rz}})
	x.want("z", kernel.Active)

	bad := rz
	bad.Inject, bad.Config = []string{"k"}, "bad"
	if _, err := x.ld.Apply(t.Context(), []def.Op{update(bad)}); err == nil || !contains(err.Error(), "config refused") {
		t.Fatalf("Apply = %v, want the refusal of the config", err)
	}
	x.settle()
	x.want("z", kernel.Active)
	if e, _ := x.ld.Locate("z"); e.Spec.Config != "good" || len(e.Spec.Inject) != 0 {
		t.Errorf("the refused plan changed z: %+v", e.Spec)
	}
	if applies.Load() != 1 || disposed.Load() != 0 {
		t.Errorf("the refused plan replaced z: %d applies, %d disposals", applies.Load(), disposed.Load())
	}

	// A config the kernel takes replaces the fiber as before.
	good := rz
	good.Inject, good.Config = []string{"k"}, "better"
	if res := x.apply(update(good)); len(res) != 1 || res[0].Outcome != def.Restarted {
		t.Fatalf("replacement: %v", res)
	}
	x.want("z", kernel.Active)
	if applies.Load() != 2 || disposed.Load() != 1 || x.fibers("t/dec") != 1 {
		t.Errorf("replacement: %d applies, %d disposals, %d fibers", applies.Load(), disposed.Load(), x.fibers("t/dec"))
	}
}

// The operations of a plan apply in order against the tree as the earlier ones leave it
// (sdk/def/loader/v0 Loader.Apply). A later operation that retires an entry's fiber
// takes back what an earlier one queued for it: the kernel refuses an update or a commit
// of a fiber after the batch disposes it, and an entry with no activation left is told
// nothing on loader/volatile-update.
func TestUpdateThenRemoveOfOneEntry(t *testing.T) {
	p := newProbe()
	ls := newListener("t/listener")
	rv := row("v", "t/v")
	rv.Config = m{"window": int64(5), "name": "x"}
	x := newFx(t, fxOpt{plugins: plugins(p.plugin("t/v", nil)), pre: []kernel.Plugin{ls.plugin},
		modules: []launch.CatalogEntry{{ID: "t/v", Volatile: []string{"window"}}}})
	for _, tc := range []struct {
		name string
		to   m
		want def.Outcome
	}{
		{"volatile", m{"window": int64(6), "name": "x"}, def.Committed},
		{"restart", m{"window": int64(5), "name": "y"}, def.Restarted},
	} {
		x.apply(create(rv))
		x.want("v", kernel.Active)
		up := rv
		up.Config = tc.to
		res, err := x.ld.Apply(t.Context(), []def.Op{update(up), remove("v")})
		if err != nil || len(res) != 2 || res[0].Outcome != tc.want || res[1].Outcome != def.Removed {
			t.Fatalf("%s: Apply = %v, %v", tc.name, res, err)
		}
		x.settle()
		if _, ok := x.ld.Locate("v"); ok || x.fibers("t/v") != 0 {
			t.Fatalf("%s: v is still there (%d fibers)", tc.name, x.fibers("t/v"))
		}
	}
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if len(ls.volatile) != 0 {
		t.Errorf("volatile-update heard %+v for a removed entry", ls.volatile)
	}
}
