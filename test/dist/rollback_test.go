// SPDX-License-Identifier: Apache-2.0

package testdist

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/dist"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	toy "github.com/mutex-solutions/aiops-platform/test/dist/def/toy/v0"
	"github.com/mutex-solutions/aiops-platform/test/dist/plugins/teststate"
)

// A live generation restarts a provider, and a dependant of it that was ACTIVE fails
// once on the activation the restart gives it. The verdict fails ("was ACTIVE and is not
// any more") and the generation is rolled back. The kernel keeps the dependant FAILED
// until something updates it and restart-policy holds its retry until the operation is
// final, so the rollback restarts it with the restored provider: the rollback passes, the
// state of the tree stays known and the next generation is taken (08-profiles-roadmap-
// adrs.md §1.6: a live Apply error is rolled back, not left in a failed rollback).
func TestRollbackRestartsADependantThatFailedInTheGeneration(t *testing.T) {
	var failNext atomic.Bool
	flip := kernel.Define(kernel.Plugin{ID: "aiops-test/flip", Inject: []kernel.Dep{kernel.Need(toy.StoreKey)},
		Apply: func(c *kernel.Context, _ any) error {
			_ = toy.StoreKey.Of(c)
			if failNext.CompareAndSwap(true, false) {
				return errors.New("flip: fails once")
			}
			return nil
		}})
	d, err := Distribution()
	if err != nil {
		t.Fatal(err)
	}
	keys := d.Registry.Keys
	d.Registry = dist.Table(append(Modules(), dist.Module{Plugin: flip})...)
	d.Registry.Keys = keys

	live := filepath.Join(t.TempDir(), "live.yml")
	write := func(body string) {
		if err := os.WriteFile(live, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const ins = "- insert:\n    - {id: flip, plugin: aiops-test/flip, roles: [api]}\n"
	write(ins)
	p := startDist(t, d, nil, "smoke", live)
	p.ready()
	rowState := func(s teststate.State, id string) string {
		for _, r := range s.Rows {
			if r.ID == id {
				return r.State
			}
		}
		return ""
	}
	waitState(t, p, func(s teststate.State) bool {
		return s.Ready && rowState(s, "flip") == "ACTIVE" && rowState(s, "store") == "ACTIVE"
	})
	submit := func(marker string) {
		control := filepath.Join(p.home, "run", "submit.json")
		if err := os.WriteFile(control+".tmp", []byte(marker), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(control+".tmp", control); err != nil {
			t.Fatal(err)
		}
	}
	final := func(id uint64) teststate.State {
		return waitState(t, p, func(s teststate.State) bool {
			return s.Reload != nil && s.Reload.ID == id && s.Reload.Status != "queued" && s.Reload.Status != "running"
		})
	}

	write(ins + "- {id: store, config: {applyDelay: 1ms}}\n")
	failNext.Store(true)
	submit("1")
	st := final(1)
	if st.Reload.Status != "rolled back" || st.Reload.Rollback == nil || st.Reload.Rollback.Error != "" {
		t.Fatalf("operation 1: %+v (rollback %+v)", st.Reload, st.Reload.Rollback)
	}
	if failNext.Load() {
		t.Fatal("the dependant never failed: the generation did not restart it")
	}
	write(ins)
	submit("2")
	if st = final(2); st.Reload.Status != "passed" {
		t.Fatalf("operation 2 after a rollback that passed: %+v", st.Reload)
	}
	waitState(t, p, func(s teststate.State) bool { return s.Ready && rowState(s, "flip") == "ACTIVE" })
	if code := p.stop(); code != boot.ExitOK {
		t.Fatalf("exit %d; stderr:\n%s", code, p.errb.String())
	}
}
