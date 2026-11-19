// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// Frozen thresholds and rules from criteria §4, §8.1, §8.2 and §9. They live here
// and are never tuned; the criteria document stays the authority.
const (
	RequiredGOMAXPROCS = 8                     // §4.
	MountP99Limit      = 2 * time.Second       // §8.1 mount and settle, p99.
	LeafP99Limit       = 10 * time.Millisecond // §8.1 leaf update and settle, p99.
	RestartLimit       = 5 * time.Second       // §8.1/§8.2 restart and settle (the maximum is held to it).
	GraphHeapLimit     = int64(128 << 20)      // §8.1 retained heap over the empty/shared baseline.
	TreeHeapLimit      = int64(32 << 20)       // §8.2 retained heap over the shared-only baseline.
	ColdMountLimit     = 50 * time.Millisecond // §8.2 each cold mount is strictly below this.
	NoiseCVLimit       = 0.10                  // §4 throughput CV above this requires a repeat.
	FrozenTreeCycles   = 10                    // §8.2 ten full cycles.
	realmGroupWidth    = 100                   // fibers per realm group: one provider + 99 consumers (§8.1).
)

// Plan carries every count the driver uses. FrozenPlan is the only acceptance
// plan; any other value (including every Dry plan) can never satisfy the gate.
type Plan struct {
	Dry         bool  `json:"dry"`
	Repetitions int   `json:"repetitions"`
	GraphTrials int   `json:"graphTrials"`
	TreeTrials  int   `json:"treeTrials"`
	TreeCycles  int   `json:"treeCycles"`
	GraphFibers int   `json:"graphFibers"`
	TreeCounts  []int `json:"treeCounts"`
}

func FrozenPlan() Plan {
	return Plan{Repetitions: MinimumRepetitions, GraphTrials: 100, TreeTrials: 10, TreeCycles: FrozenTreeCycles,
		GraphFibers: GraphFibers, TreeCounts: []int{500, 300}}
}

// DryPlan is a reduced plan for pipeline development only: tiny fixtures, two
// repetitions. It is labelled in every record and can never classify as pass.
func DryPlan() Plan {
	return Plan{Dry: true, Repetitions: 2, GraphTrials: 2, TreeTrials: 2, TreeCycles: 2, GraphFibers: 400, TreeCounts: []int{12}}
}

// DryFullSizePlan keeps the frozen fixture sizes (10,000-fiber graphs, 500- and
// 300-tree fixtures, ten tree cycles) but runs one root per cell and two
// repetitions. It exists to observe cost and raw-record size of real-size roots;
// it is labelled Dry and can never pass.
func DryFullSizePlan() Plan {
	p := FrozenPlan()
	p.Dry, p.Repetitions, p.GraphTrials, p.TreeTrials = true, 2, 1, 1
	return p
}

// Frozen reports whether every count equals the frozen protocol and Dry is off.
func (p Plan) Frozen() bool {
	f := FrozenPlan()
	return !p.Dry && p.Repetitions == f.Repetitions && p.GraphTrials == f.GraphTrials && p.TreeTrials == f.TreeTrials &&
		p.TreeCycles == f.TreeCycles && p.GraphFibers == f.GraphFibers && slices.Equal(p.TreeCounts, f.TreeCounts)
}

// validatePlan refuses a plan below the frozen independent repetition/sample
// floors. It does not make a plan frozen; Frozen does.
func validatePlan(plan Plan) error {
	if plan.Repetitions < MinimumRepetitions || plan.GraphTrials <= 0 || plan.TreeTrials <= 0 ||
		int64(plan.GraphTrials)*int64(plan.Repetitions) < MinimumSamples ||
		int64(plan.TreeTrials)*int64(plan.TreeCycles)*int64(plan.Repetitions) < MinimumSamples {
		return fmt.Errorf("plan is below frozen independent repetition/sample floors")
	}
	return nil
}

func (p Plan) validateStructure() error {
	if p.Repetitions < 1 || p.GraphTrials < 1 || p.TreeTrials < 1 || p.TreeCycles < 1 || p.GraphFibers < 4 || len(p.TreeCounts) == 0 {
		return errors.New("plan has a non-positive count")
	}
	for _, n := range p.TreeCounts {
		if n < 1 {
			return errors.New("plan has a non-positive tree count")
		}
	}
	return nil
}

// graphFixture builds a graph of the requested size. The diamonds shape needs
// 1+3n fibers and realms need a multiple of 100 (one hundred-fiber group each).
func graphFixture(topology Topology, n int) (Fixture, error) {
	if topology == Realms {
		if n%(realmGroupWidth) != 0 {
			return Fixture{}, fmt.Errorf("realm graph size %d is not a multiple of %d", n, realmGroupWidth)
		}
		return RealmFixture(n/realmGroupWidth, realmGroupWidth-1)
	}
	return Graph(topology, n)
}

// Fixtures returns every fixture the plan runs, in execution order. For the
// frozen plan this is exactly FrozenFixtures().
func (p Plan) Fixtures() ([]Fixture, error) {
	var out []Fixture
	for _, topology := range []Topology{Independent, Chain, FanOut, Diamonds, Realms} {
		f, err := graphFixture(topology, p.GraphFibers)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	for _, count := range p.TreeCounts {
		for _, width := range []int{2, 4} {
			f, err := TreeFixture(count, width)
			if err != nil {
				return nil, err
			}
			out = append(out, f)
		}
	}
	return out, nil
}

// operations lists the measured operation labels a fixture must produce.
func operations(f Fixture) []string {
	if f.Topology == Trees {
		return []string{"cold-tree-mount-settle", "hot-tree-lookup", "shared-provider-restart-settle", "reverse-trees-dispose-settle"}
	}
	ops := []string{"mount-settle", "leaf-update-settle", "root-restart-settle"}
	if f.Topology == Realms {
		ops = append(ops, "all-providers-batch-restart-settle")
	}
	return append(ops, "teardown")
}

// expectedObservations is the per-bucket observation count a plan schedules.
func (p Plan) expectedObservations(f Fixture, op string) int {
	if f.Topology != Trees {
		return p.Repetitions * p.GraphTrials
	}
	per := p.Repetitions * p.TreeTrials * p.TreeCycles
	if op == "cold-tree-mount-settle" {
		return per * f.TreeCount
	}
	return per
}
