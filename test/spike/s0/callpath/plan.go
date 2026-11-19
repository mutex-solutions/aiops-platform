// SPDX-License-Identifier: Apache-2.0

package callpath

import (
	"errors"
	"slices"
	"time"
)

// Frozen thresholds and rules from criteria §4 and §8.3. They live here, not in
// criteria.go (which stays untouched), and are never tuned.
const (
	LeaseP99Limit      = 2 * time.Microsecond // §8.3: Enter+Leave p99 <= 2µs, zero allocation.
	NoiseCVLimit       = 0.10                 // §4: throughput CV > 10% requires a repeat.
	OperationTimeout   = 5 * time.Second      // §4: from scheduled arrival, queue included.
	DrainLimit         = 30 * time.Second     // §4: after the steady-state window.
	RequiredGOMAXPROCS = 8                    // §4.
	MinObservations    = 1_000_000            // §4: per operation/candidate/concurrency over all runs.
	MinRepetitions     = 10                   // §4: independent repetitions.
	// WarmupRepetitions is amendment 2: one baseline repetition at process start,
	// recorded and labelled, excluded from every classification input.
	WarmupRepetitions = 1
)

// Plan carries every count the runner uses. FrozenPlan is the only acceptance
// plan; any other value is a development or unit-test plan and can never satisfy
// the gate.
type Plan struct {
	Repetitions      int           `json:"repetitions"`
	WarmupReps       int           `json:"warmupRepetitions"` // Amendment 2: recorded, excluded from classification.
	SamplesPerRun    int           `json:"samplesPerRun"`
	StressSamples    int           `json:"stressSamplesPerRun"`
	Leaves           int           `json:"leaves"`
	UpdatesPerSecond int           `json:"updatesPerSecond"`
	OutstandingLimit int           `json:"outstandingLimit"`
	OwnerUpdateEvery int           `json:"ownerUpdateEvery"`
	ScheduledUpdates int           `json:"scheduledUpdates"`
	StressDuration   time.Duration `json:"stressDurationNS"`
	Callers          []int         `json:"callers"`
}

func FrozenPlan() Plan {
	return Plan{Repetitions: Repetitions, WarmupReps: WarmupRepetitions, SamplesPerRun: SamplesPerRun, StressSamples: SamplesPerRun, Leaves: Leaves,
		UpdatesPerSecond: UpdatesPerSecond, OutstandingLimit: OutstandingLimit, OwnerUpdateEvery: OwnerUpdateEvery,
		ScheduledUpdates: ScheduledUpdates, StressDuration: StressDuration, Callers: CallerConcurrencies()}
}

// DryPlan is reduced for pipeline development only (about a minute).
func DryPlan() Plan {
	return Plan{Repetitions: 2, WarmupReps: WarmupRepetitions, SamplesPerRun: 20_000, StressSamples: 2_000, Leaves: Leaves,
		UpdatesPerSecond: UpdatesPerSecond, OutstandingLimit: OutstandingLimit, OwnerUpdateEvery: 50,
		ScheduledUpdates: 200, StressDuration: 2 * time.Second, Callers: CallerConcurrencies()}
}

// Frozen reports whether every count equals the frozen protocol.
func (p Plan) Frozen() bool {
	f := FrozenPlan()
	return p.Repetitions == f.Repetitions && p.WarmupReps == f.WarmupReps && p.SamplesPerRun == f.SamplesPerRun && p.StressSamples == f.StressSamples &&
		p.Leaves == f.Leaves && p.UpdatesPerSecond == f.UpdatesPerSecond && p.OutstandingLimit == f.OutstandingLimit &&
		p.OwnerUpdateEvery == f.OwnerUpdateEvery && p.ScheduledUpdates == f.ScheduledUpdates &&
		p.StressDuration == f.StressDuration && slices.Equal(p.Callers, f.Callers)
}

func (p Plan) validate() error {
	if p.WarmupReps < 0 || p.WarmupReps > 1 {
		return errors.New("callpath plan warm-up is zero or one repetition")
	}
	if p.Repetitions < 1 || p.SamplesPerRun < 1 || p.StressSamples < 1 || p.Leaves < 1 || p.UpdatesPerSecond < 1 ||
		p.OutstandingLimit < 1 || p.OwnerUpdateEvery < 1 || p.ScheduledUpdates < 1 || p.StressDuration <= 0 || len(p.Callers) == 0 {
		return errors.New("callpath plan has a non-positive count")
	}
	for _, c := range p.Callers {
		if c < 1 || c > p.SamplesPerRun || c > p.StressSamples {
			return errors.New("callpath plan has an unusable caller count")
		}
	}
	if int64(p.ScheduledUpdates)*int64(time.Second) != int64(p.UpdatesPerSecond)*int64(p.StressDuration) {
		return errors.New("scheduled updates must equal rate times duration")
	}
	return nil
}
