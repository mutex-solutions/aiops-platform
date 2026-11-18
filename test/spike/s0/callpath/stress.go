// SPDX-License-Identifier: Apache-2.0

package callpath

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mutex-solutions/aiops-platform/test/spike/s0/scale"
)

// Mutation outcomes. Overflow is a failure: the item is not queued or slowed.
const (
	MutationOK       = "ok"
	MutationOverflow = "overflow"
	MutationTimeout  = "timeout"
	MutationError    = "error"
)

// Mutation records one scheduled open-time update. Times are monotonic offsets
// from the arm start. Every scheduled arrival has a record, including overflow.
type Mutation struct {
	K           int    `json:"schedule"`
	Leaf        int    `json:"leaf"`
	Owner       bool   `json:"alsoUpdatesLeasedOwner"`
	ScheduledNS int64  `json:"scheduledNS"`
	StartNS     int64  `json:"startedNS"`
	AckNS       int64  `json:"updateAcknowledgedNS"`
	DoneNS      int64  `json:"awaitDoneNS"`
	Outcome     string `json:"outcome"`
	Error       string `json:"error,omitempty"`
	GenBefore   uint64 `json:"bindingGenBefore"`
	GenAfter    uint64 `json:"bindingGenAfter"`
}

// StressRecord is the raw record of one stress run at one caller concurrency.
type StressRecord struct {
	Callers       int          `json:"callers"`
	Observations  Arm          `json:"observations"`
	Mutations     []Mutation   `json:"mutations"`
	Checkpoints   []Checkpoint `json:"causalCheckpoints"`
	DrainTimedOut bool         `json:"drainTimedOut"`
	Outstanding   int          `json:"outstandingAtDrainTimeout"`
	DurationNS    int64        `json:"windowNS"`
	OwnerGenStart uint64       `json:"ownerActivationGenAtStart"`
	OwnerGenEnd   uint64       `json:"ownerActivationGenAtEnd"`
	BindGenStart  uint64       `json:"bindingGenAtStart"`
	BindGenEnd    uint64       `json:"bindingGenAtEnd"`
	Violations    int64        `json:"inactiveBodyExecutions"`
}

// StressSummary is the evaluation view of a StressRecord.
type StressSummary struct {
	Rep                   int                `json:"rep"`
	Candidate             string             `json:"candidate"`
	Callers               int                `json:"callers"`
	File                  string             `json:"file"`
	Observations          int                `json:"observations"`
	OutcomeCounts         map[string]int     `json:"outcomeCounts"`
	Dist                  scale.Distribution `json:"allAttemptLatency"`
	Scheduled             int                `json:"scheduledUpdates"`
	Succeeded             int                `json:"succeeded"`
	Overflow              int                `json:"overflow"`
	Failed                int                `json:"failed"`
	TimedOut              int                `json:"timedOut"`
	OwnerUpdates          int                `json:"ownerUpdatesScheduled"`
	OwnerReactivations    uint64             `json:"ownerActivationsPublished"`
	ConsumerReactivations uint64             `json:"consumerBindingsPublished"`
	Violations            int64              `json:"inactiveBodyExecutions"`
	DrainTimedOut         bool               `json:"drainTimedOut"`
	Problems              []string           `json:"problems,omitempty"`
}

// StressHooks are test-only seams; nil in measurement.
type StressHooks struct {
	BeforeItem    func(k int) // Runs inside an admitted work item before its Update.
	AfterSchedule func()      // Runs once every arrival has been scheduled or rejected.
}

func since(t0 time.Time) int64 { return time.Since(t0).Nanoseconds() }

// RunStress executes the stress arm exactly: open-time scheduled updates at the
// plan rate round-robin over the unrelated leaves, at most OutstandingLimit
// outstanding through Update acknowledgement and target Await, every
// OwnerUpdateEvery-th schedule also updating the leased owner in the same
// admitted item, while callers observe the full call path on a schedule spread
// over the whole window. Overflow is recorded and never queued, slowed or
// compensated.
func RunStress(ctx context.Context, fx *Fixture, p Plan, callers int, hooks StressHooks) StressRecord {
	rec := StressRecord{Callers: callers, Mutations: make([]Mutation, p.ScheduledUpdates)}
	itemsCtx, cancelItems := context.WithCancel(ctx)
	defer cancelItems()

	var mu sync.Mutex
	t0 := time.Now()
	rec.OwnerGenStart, rec.BindGenStart = fx.ownerGens.Load(), fx.bindGens.Load()
	rec.Checkpoints = append(rec.Checkpoints, fx.Checkpoint(ctx, "before-window", -1, 0))

	var outstanding atomic.Int64
	var items sync.WaitGroup
	period := time.Second / time.Duration(p.UpdatesPerSecond)

	runItem := func(m *Mutation, target time.Time) {
		defer items.Done()
		defer outstanding.Add(-1)
		ictx, cancel := context.WithDeadline(itemsCtx, target.Add(OperationTimeout))
		defer cancel()
		if hooks.BeforeItem != nil {
			hooks.BeforeItem(m.K)
		}
		m.StartNS = since(t0)
		err := func() error {
			if b := fx.binding.Load(); b != nil {
				m.GenBefore = b.Gen
			}
			leaf := fx.Leaves[m.Leaf]
			if err := fx.Runtime.Update(ictx, leaf.ID); err != nil {
				return err
			}
			if m.Owner {
				if err := fx.Runtime.Update(ictx, fx.Provider.ID); err != nil {
					return err
				}
			}
			m.AckNS = since(t0)
			if err := leaf.Await(ictx); err != nil {
				return err
			}
			if m.Owner {
				// Fresh consumer binding proves the full owner reactivation cycle.
				if err := fx.WaitBindingGen(ictx, m.GenBefore); err != nil {
					return err
				}
				if err := fx.Provider.Await(ictx); err != nil {
					return err
				}
				return fx.Consumer.Await(ictx)
			}
			return nil
		}()
		m.DoneNS = since(t0)
		switch {
		case err == nil:
			m.Outcome = MutationOK
		case errors.Is(err, context.DeadlineExceeded):
			m.Outcome, m.Error = MutationTimeout, err.Error()
		default:
			m.Outcome, m.Error = MutationError, err.Error()
		}
		if b := fx.binding.Load(); b != nil {
			m.GenAfter = b.Gen
		}
		if m.Owner {
			cp := fx.Checkpoint(context.WithoutCancel(ctx), "after-owner-update-item", m.K, since(t0))
			mu.Lock()
			rec.Checkpoints = append(rec.Checkpoints, cp)
			mu.Unlock()
		}
	}

	var schedDone sync.WaitGroup
	schedDone.Add(1)
	go func() {
		defer schedDone.Done()
		for k := range p.ScheduledUpdates {
			target := t0.Add(time.Duration(k) * period)
			if d := time.Until(target); d > 0 {
				time.Sleep(d) // Open-loop pacing to the scheduled arrival; never rate-adjusted.
			}
			m := &rec.Mutations[k]
			m.K, m.Leaf, m.Owner = k, k%len(fx.Leaves), (k+1)%p.OwnerUpdateEvery == 0
			m.ScheduledNS = target.Sub(t0).Nanoseconds()
			if outstanding.Add(1) > int64(p.OutstandingLimit) {
				outstanding.Add(-1)
				m.StartNS, m.DoneNS, m.Outcome, m.Error = since(t0), since(t0), MutationOverflow, "outstanding cap exceeded; not queued, not slowed"
				continue
			}
			items.Add(1)
			go runItem(m, target)
		}
	}()

	// Callers observe the whole call path at times spread across the interval.
	counts := split(p.StressSamples, callers)
	obs := Arm{Operation: StressObservationOp(), Callers: callers, Samples: p.StressSamples, CallerCounts: counts,
		LatencyNS: make([]int64, p.StressSamples), StartOffsetNS: make([]int64, p.StressSamples),
		ScheduledNS: make([]int64, p.StressSamples), ActivationGen: make([]uint64, p.StressSamples),
		AllocsMeasured: "not measured: concurrent mutation allocates; allocation budget applies to the baseline"}
	outcomes := make([]byte, p.StressSamples)
	op := fx.Operation(StressObservationOp())
	var callersDone sync.WaitGroup
	offset := 0
	for c, n := range counts {
		base := offset
		offset += n
		callersDone.Add(1)
		go func() {
			defer callersDone.Done()
			for i := range n {
				j := c + i*callers // Global observation index; arrivals interleave across callers.
				scheduled := int64(j) * int64(p.StressDuration) / int64(p.StressSamples)
				if d := time.Until(t0.Add(time.Duration(scheduled))); d > 0 {
					time.Sleep(d)
				}
				obs.ScheduledNS[base+i] = scheduled
				obs.StartOffsetNS[base+i] = since(t0)
				start := time.Now()
				outcome, gen := op()
				obs.LatencyNS[base+i] = int64(time.Since(start))
				outcomes[base+i] = outcome.char()
				obs.ActivationGen[base+i] = gen
			}
		}()
	}
	callersDone.Wait()
	schedDone.Wait()
	if hooks.AfterSchedule != nil {
		hooks.AfterSchedule()
	}
	rec.DurationNS = since(t0)

	// Stop new arrivals; allow at most DrainLimit for acknowledged work to finish.
	drained := make(chan struct{})
	go func() { items.Wait(); close(drained) }()
	select {
	case <-drained:
	case <-time.After(DrainLimit):
		rec.DrainTimedOut, rec.Outstanding = true, int(outstanding.Load())
		cancelItems()
		<-drained
	}
	obs.Outcomes = string(outcomes)
	obs.OutcomeCounts = countOutcomes(obs.Outcomes)
	obs.Summary = scale.Summarize(obs.LatencyNS)
	rec.Observations = obs
	rec.Checkpoints = append(rec.Checkpoints, fx.Checkpoint(context.WithoutCancel(ctx), "after-drain", -1, since(t0)))
	rec.OwnerGenEnd, rec.BindGenEnd, rec.Violations = fx.ownerGens.Load(), fx.bindGens.Load(), fx.violations.Load()
	return rec
}

// StressObservationOp is the operation callers observe while the graph mutates.
func StressObservationOp() Operation { return Dispatch }

// SummarizeStress derives evaluation facts from the raw record only.
func SummarizeStress(rec StressRecord) StressSummary {
	s := StressSummary{Callers: rec.Callers, Observations: len(rec.Observations.LatencyNS), OutcomeCounts: rec.Observations.OutcomeCounts,
		Dist: rec.Observations.Summary, Scheduled: len(rec.Mutations), Violations: rec.Violations, DrainTimedOut: rec.DrainTimedOut,
		OwnerReactivations: rec.OwnerGenEnd - rec.OwnerGenStart, ConsumerReactivations: rec.BindGenEnd - rec.BindGenStart}
	for _, m := range rec.Mutations {
		if m.Owner {
			s.OwnerUpdates++
		}
		switch m.Outcome {
		case MutationOK:
			s.Succeeded++
		case MutationOverflow:
			s.Overflow++
		case MutationTimeout:
			s.TimedOut++
		default:
			s.Failed++
		}
	}
	return s
}
