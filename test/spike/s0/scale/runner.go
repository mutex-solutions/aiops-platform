// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"time"

	cc "github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
)

const TrialGuard = 15 * time.Second // NOT the frozen 2s mount performance limit.

// RunT runs one fresh, closed root. The caller owns context and benchmark
// authorization; DefaultMeasure is opt-in. This helper does not mark a single
// run passed: sample floors, repetitions, host noise and hashes are suite gates.
// Graphs: mount+Settle, leaf Update+Settle, node-0 Update+Settle, teardown.
// Realms additionally restart every provider before a single Settle, explicitly
// not an atomic Batch: neither prototype exposes Batch. Trees execute ten cycles.
func RunT(ctx context.Context, backend Backend, f Fixture) (result RunResult) {
	result = RunResult{Backend: backend.Name, StartedUTC: time.Now().UTC(), GOMAXPROCS: runtime.GOMAXPROCS(0), Classification: Inconclusive,
		Workload: Workload{Name: f.Name, Topology: f.Topology, Fibers: len(f.Nodes), Trees: f.TreeCount, TreeWidth: f.TreeWidth, Seed: f.Seed, PRNG: "none; fixed declaration and operation order"},
		Gaps:     []string{"pending event/observer backlog unavailable; Events measures retained history only"}}
	result.HarnessBaseline = checkpoint(nil, true)
	r, err := NewRealCandidateRuntime(backend, f)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		result.Classification, result.EndedUTC = Fail, time.Now().UTC()
		return
	}
	result.EmptyRoot = r.EmptyRoot
	defer func() {
		// Always use a fresh cleanup guard: an expired operation guard must not
		// skip disposal of this run's root or retain a reconciler owner worker.
		cleanup, cancel := context.WithTimeout(context.Background(), TrialGuard)
		defer cancel()
		defer r.Clear()
		if err := r.RequestDisposal(cleanup, 0, len(f.Nodes)); err != nil {
			result.Errors = append(result.Errors, "final disposal request: "+err.Error())
		}
		if err := r.Runtime.Settle(cleanup); err != nil {
			result.Errors = append(result.Errors, "final settle: "+err.Error())
		}
		if err := inspectDisposed(cleanup, r, &result); err != nil {
			result.Errors = append(result.Errors, err.Error())
		}
		result.ResourcesEnd = r.Resources.Active.Load()
		result.Events, err = r.Runtime.Events(cleanup)
		if err != nil {
			result.Errors = append(result.Errors, "final event ledger: "+err.Error())
		}
		result.DisposedWithArchives = checkpoint(&r.Resources, true)
		if err := r.Runtime.Close(cleanup); err != nil {
			result.Errors = append(result.Errors, "root Close: "+err.Error())
		}
		result.PostClose = checkpoint(&r.Resources, true)
		for owner, count := range result.PostClose.Goroutines.Counts {
			if owner != "external" && count > result.HarnessBaseline.Goroutines.Counts[owner] {
				result.Errors = append(result.Errors, fmt.Sprintf("candidate goroutine owner remains after root Close: %s count=%d baseline=%d", owner, count, result.HarnessBaseline.Goroutines.Counts[owner]))
			}
		}
		result.EndedUTC = time.Now().UTC()
		if len(result.Errors) > 0 {
			result.Classification = Fail
		}
	}()
	if f.Topology == Trees {
		result.Gaps = append(result.Gaps, "native Batch unavailable: shared provider uses one Update; tree disposal uses reverse tree order and one Settle")
		runTrees(ctx, r, &result)
		return
	}
	if !measure(ctx, r, &result, "mount-settle", 0, -1, func(op context.Context) error {
		return errors.Join(r.Mount(op, 0, len(f.Nodes)), r.Runtime.Settle(op))
	}, func(op context.Context) error { return inspectActive(op, r, &result, 0, len(f.Nodes)) }) {
		return
	}
	result.Retained = append(result.Retained, checkpoint(&r.Resources, true))
	if !measure(ctx, r, &result, "leaf-update-settle", 0, -1, func(op context.Context) error {
		return errors.Join(r.Runtime.Update(op, r.IDs[f.Leaf]), r.Runtime.Settle(op))
	}, func(op context.Context) error { return inspectActive(op, r, &result, 0, len(f.Nodes)) }) {
		return
	}
	if !measure(ctx, r, &result, "root-restart-settle", 0, -1, func(op context.Context) error {
		// Fiber index 0 is always selected, even independent: its closure is
		// just itself; it is never mislabeled a full independent restart.
		return errors.Join(r.Runtime.Update(op, r.IDs[0]), r.Runtime.Settle(op))
	}, func(op context.Context) error { return inspectActive(op, r, &result, 0, len(f.Nodes)) }) {
		return
	}
	result.Retained = append(result.Retained, checkpoint(&r.Resources, true))
	if f.Topology == Realms {
		result.Gaps = append(result.Gaps, "native Batch unavailable: all-provider restart submits ordinary Updates then Settle, not a Batch acceptance result")
		if !measure(ctx, r, &result, "all-providers-update-loop-settle", 0, -1, func(op context.Context) error {
			for _, index := range f.RootProviders {
				if err := r.Runtime.Update(op, r.IDs[index]); err != nil {
					return err
				}
			}
			return r.Runtime.Settle(op)
		}, func(op context.Context) error { return inspectActive(op, r, &result, 0, len(f.Nodes)) }) {
			return
		}
	}
	measure(ctx, r, &result, "teardown", 0, -1, func(op context.Context) error {
		return errors.Join(r.RequestDisposal(op, 0, len(f.Nodes)), r.Runtime.Settle(op))
	}, func(op context.Context) error { return inspectDisposed(op, r, &result) })
	return
}

func runTrees(ctx context.Context, r *RealCandidateRuntime, result *RunResult) {
	f := r.Fixture
	if err := r.Mount(ctx, 0, f.Shared); err != nil {
		result.Errors = append(result.Errors, err.Error())
		return
	}
	if err := r.Runtime.Settle(ctx); err != nil {
		result.Errors = append(result.Errors, err.Error())
		return
	}
	if err := inspectActive(ctx, r, result, 0, f.Shared); err != nil {
		result.Errors = append(result.Errors, err.Error())
		return
	}
	initial, err := r.Runtime.Snapshots(ctx)
	if err != nil || len(initial) != f.Shared {
		result.Errors = append(result.Errors, fmt.Sprintf("eager local runtime mount before cold phase: snapshots=%d, want shared=%d, error=%v", len(initial), f.Shared, err))
		return
	}
	baseline := checkpoint(&r.Resources, true)
	result.SharedOnly = &baseline
	// No local runtime fiber exists until its own explicit cold mount.
	for cycle := 0; cycle < MinimumRepetitions; cycle++ {
		for tree := 0; tree < f.TreeCount; tree++ {
			start := f.Shared + tree*f.TreeWidth
			if !measure(ctx, r, result, "cold-tree-mount-settle", cycle, tree, func(op context.Context) error {
				return errors.Join(r.Mount(op, start, start+f.TreeWidth), r.Runtime.Settle(op))
			}, func(op context.Context) error { return inspectActive(op, r, result, start, start+f.TreeWidth) }) {
				return
			}
		}
		if err := inspectActive(ctx, r, result, 0, len(f.Nodes)); err != nil {
			result.Errors = append(result.Errors, err.Error())
			return
		}
		result.Retained = append(result.Retained, checkpoint(&r.Resources, true))
		if !measure(ctx, r, result, "hot-tree-lookup", cycle, -1, func(context.Context) error { return r.HotLookup() }, nil) {
			return
		}
		if !measure(ctx, r, result, "shared-provider-restart-settle", cycle, -1, func(op context.Context) error {
			// Exactly one provider: one ordinary Update has the entire closure.
			return errors.Join(r.Runtime.Update(op, r.IDs[0]), r.Runtime.Settle(op))
		}, func(op context.Context) error { return inspectActive(op, r, result, 0, len(f.Nodes)) }) {
			return
		}
		if !measure(ctx, r, result, "reverse-trees-dispose-settle", cycle, -1, func(op context.Context) error {
			for tree := f.TreeCount - 1; tree >= 0; tree-- {
				start := f.Shared + tree*f.TreeWidth
				if err := r.RequestDisposal(op, start, start+f.TreeWidth); err != nil {
					return err
				}
			}
			return r.Runtime.Settle(op)
		}, func(op context.Context) error {
			if r.Resources.Active.Load() != int64(f.Shared) || r.Resources.Leases.Load() != 0 {
				return fmt.Errorf("tree disposal resources = %d leases = %d", r.Resources.Active.Load(), r.Resources.Leases.Load())
			}
			return inspectActive(op, r, result, 0, f.Shared)
		}) {
			return
		}
	}
}

func measure(ctx context.Context, r *RealCandidateRuntime, result *RunResult, operation string, cycle, tree int, action, validate func(context.Context) error) bool {
	sample := Sample{Operation: operation, Cycle: cycle, Tree: tree, BacklogNote: "not exposed; history count is not backlog"}
	events, eventErr := r.Runtime.Events(ctx)
	sample.EventsBefore = len(events)
	sample.Before = checkpoint(&r.Resources, false)
	op, cancel := context.WithTimeout(ctx, TrialGuard)
	defer cancel()
	start := time.Now()
	sample.ScheduledUTC, sample.StartedUTC = start.UTC(), start.UTC() // sequential closed-loop graph operation
	err := errors.Join(eventErr, action(op))
	end := time.Now()
	sample.EndedUTC, sample.LatencyNS = end.UTC(), end.Sub(start).Nanoseconds()
	if validate != nil {
		err = errors.Join(err, validate(op)) // correctness inspection outside latency
	}
	events, eventErr = r.Runtime.Events(op)
	sample.EventsAfter = len(events)
	err = errors.Join(err, eventErr)
	if err == nil {
		for i, event := range events {
			if event.Sequence != uint64(i+1) {
				err = fmt.Errorf("event sequence %d at offset %d", event.Sequence, i)
				break
			}
		}
	}
	sample.After = checkpoint(&r.Resources, false)
	if err != nil {
		sample.Error = err.Error()
		result.Errors = append(result.Errors, operation+": "+err.Error())
	}
	result.Samples = append(result.Samples, sample)
	return err == nil
}

func inspectActive(ctx context.Context, r *RealCandidateRuntime, result *RunResult, start, end int) error {
	inspector, ok := r.Runtime.(cc.Inspector)
	if !ok {
		return errors.New("real candidate lacks Inspector")
	}
	inspection, err := inspector.Inspect(ctx)
	if err != nil {
		return err
	}
	fibers := make(map[uint64]cc.Snapshot)
	for _, fiber := range inspection.Fibers {
		fibers[fiber.ID] = fiber
	}
	provisions := make(map[string]cc.ProvisionSnapshot)
	for _, provision := range inspection.Provisions {
		provisions[provision.Key] = provision
	}
	for i := start; i < end; i++ {
		node := r.Fixture.Nodes[i]
		fiber, exists := fibers[r.IDs[i]]
		if !exists || fiber.State != cc.Active || !fiber.Alive || fiber.Target == nil || fiber.Error != "" || !fiber.ConfigPrepared || fiber.EffectiveConfig != node.Index {
			return fmt.Errorf("node %d did not reach actual ACTIVE/configured target: %+v", i, fiber)
		}
		if len(fiber.Epoch) != len(node.Needs) || fiber.Target.Revision != fiber.Revision || !slices.Equal(fiber.Epoch, fiber.Target.UIDs) {
			return fmt.Errorf("node %d actual activation epoch does not match target/declared needs", i)
		}
		for _, provided := range node.Provides {
			address := nativeKey(provided)
			if r.authority != nil {
				address, err = r.parents[node.Realm].NativeAddress(r.keys[provided.Name].Any())
				if err != nil {
					return err
				}
			}
			provision, found := provisions[address]
			if !found || !provision.Active || provision.Owner != r.IDs[i] {
				return fmt.Errorf("node %d declared provider %q absent/inactive or foreign-owned", i, address)
			}
		}
		for j, need := range node.Needs {
			address := nativeKey(need)
			if r.authority != nil {
				address, err = r.parents[node.Realm].NativeAddress(r.keys[need.Name].Any())
				if err != nil {
					return err
				}
			}
			provision, found := provisions[address]
			if !found || !provision.Active || provision.Owner != r.IDs[r.providers[need]] || fiber.Epoch[j] != provision.UID {
				return fmt.Errorf("node %d missing active same-realm provision %q", i, address)
			}
		}
	}
	result.ActiveChecks++
	return nil
}

func inspectDisposed(ctx context.Context, r *RealCandidateRuntime, result *RunResult) error {
	inspector, ok := r.Runtime.(cc.Inspector)
	if !ok {
		return errors.New("real candidate lacks Inspector")
	}
	inspection, err := inspector.Inspect(ctx)
	if err != nil {
		return err
	}
	result.FinalFibers, result.FinalProvisions = inspection.Fibers, inspection.Provisions
	result.DisposedArchives, result.ActiveProvisionsEnd = 0, 0
	for _, fiber := range inspection.Fibers {
		if fiber.State != cc.Disposed || fiber.Alive || len(fiber.Effects) != 0 || len(fiber.Tracked) != 0 || fiber.Target != nil || fiber.Error != "" {
			return fmt.Errorf("nonempty disposed owner: %+v", fiber)
		}
		result.DisposedArchives++
	}
	for _, provision := range inspection.Provisions {
		if provision.Active {
			result.ActiveProvisionsEnd++
		}
	}
	if len(inspection.Provisions) != 0 || r.Resources.Active.Load() != 0 || r.Resources.Leases.Load() != 0 {
		return fmt.Errorf("teardown still owns provisions=%d active=%d resources=%d leases=%d", len(inspection.Provisions), result.ActiveProvisionsEnd, r.Resources.Active.Load(), r.Resources.Leases.Load())
	}
	// Archives are evidence, not active fibers. All generated handles must be
	// present even though previous tree incarnations also remain archived.
	archived := make([]uint64, 0, len(inspection.Fibers))
	for _, fiber := range inspection.Fibers {
		archived = append(archived, fiber.ID)
	}
	slices.Sort(archived)
	for _, id := range r.IDs {
		if id != 0 {
			if _, found := slices.BinarySearch(archived, id); !found {
				return fmt.Errorf("disposed handle %d not archived", id)
			}
		}
	}
	return nil
}
