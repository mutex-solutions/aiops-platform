// SPDX-License-Identifier: Apache-2.0

package reload_test

import (
	"context"
	"testing"

	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

func outcome(rep composition.Report, id string) composition.RowReport {
	for _, r := range rep.Rows {
		if r.ID == id {
			return r
		}
	}
	return composition.RowReport{}
}

// A row of the restart closure that was already PENDING before the generation (it waits
// for a key nothing provides) is neither created, changed nor enabled by it: it stays a
// warning, as it was.
func TestClosureRowThatWasPendingBeforeIsAWarning(t *testing.T) {
	w := newWorld(t, 4, 8)
	w.seed(row("spine", "aiops/p", "old"), row("dependant", "aiops/p", nil))
	w.ld.SetState("dependant", kernel.Pending)
	w.next = launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{row("spine", "aiops/p", "new"), row("dependant", "aiops/p", nil)}}
	w.l.PlanFn = func(_ context.Context, f, to launch.Composition) (launch.Plan, error) {
		p := diff(f, to)
		p.Closure = []string{"dependant", "spine"} // dependant needs what spine provides
		return p, nil
	}
	r := w.submit("test")
	if r.Status != def.Passed {
		t.Fatalf("result = %+v", r)
	}
	if got := outcome(r.Report, "dependant"); got.Outcome != "warning" {
		t.Errorf("dependant = %+v", got)
	}
}
