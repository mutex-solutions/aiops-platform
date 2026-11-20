// SPDX-License-Identifier: Apache-2.0

// Package engine is the queue and the pipeline of the reload row: one queue, one
// goroutine, and for each submission compose, admit, plan, apply in one loader batch,
// settle and judge, then publish composition/applied (03-c3-kernel.md:65, :374-382;
// plugin-system-2026-11.md:363-371).
//
// At S1 there is no committer and no restore (ADR-059 decision 5; S4a): a failed
// verdict is reported, and the tree stays as applied. The report names the previous
// composition so that a later step can restore it.
package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	loader "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Deps is what the pipeline reads and calls.
type Deps struct {
	Profile launch.ProfileContext
	Catalog launch.Catalog
	Loader  loader.Loader
	// Publish emits composition/applied.
	Publish func(ctx context.Context, a composition.Applied) error
}

// Engine is the queue and its worker. It serves reload.Reload.
type Engine struct {
	Deps
	queueMax, historyMax int

	mu       sync.Mutex
	closed   bool
	last     def.ID
	queue    []*job
	jobs     map[def.ID]*job
	finished []def.ID // ids of final jobs, oldest first, for the bound on history
	wake     chan struct{}

	// The worker's own: the composition the tree runs, and the generation number.
	current    launch.Composition
	generation uint64
	started    bool
}

var _ def.Reload = (*Engine)(nil)

type job struct {
	res  def.Result
	done chan struct{}
}

// nested marks the context of the worker's own work.
type nested struct{}

// New returns an engine whose queue holds at most queueMax waiting operations and
// whose history keeps the results of the last historyMax final ones. The tree is
// taken to run Profile.Committed as it is at the first operation.
func New(d Deps, queueMax, historyMax int) *Engine {
	return &Engine{Deps: d, queueMax: queueMax, historyMax: historyMax, jobs: map[def.ID]*job{},
		wake: make(chan struct{}, 1)}
}

// Submit queues op. It takes a lock for a few instructions and never waits for the
// worker, so it is safe in a leased call: the apply waits for a fiber's lease, and a
// Submit that waited for the apply would wait for itself.
func (e *Engine) Submit(ctx context.Context, op def.Op) (def.ID, error) {
	if ctx.Value(nested{}) != nil {
		return 0, def.ErrNested
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	switch {
	case e.closed:
		return 0, def.ErrClosed
	case len(e.queue) >= e.queueMax:
		return 0, def.ErrQueueFull
	}
	e.last++
	j := &job{res: def.Result{ID: e.last, Op: op, Status: def.Queued}, done: make(chan struct{})}
	e.queue = append(e.queue, j)
	e.jobs[j.res.ID] = j
	select {
	case e.wake <- struct{}{}:
	default:
	}
	return j.res.ID, nil
}

// Result returns the result of id as it stands.
func (e *Engine) Result(id def.ID) (def.Result, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	j, ok := e.jobs[id]
	if !ok {
		return def.Result{}, false
	}
	return j.res, true
}

// Await waits until id is final.
func (e *Engine) Await(ctx context.Context, id def.ID) (def.Result, error) {
	e.mu.Lock()
	j, ok := e.jobs[id]
	e.mu.Unlock()
	if !ok {
		return def.Result{}, def.ErrUnknown
	}
	select {
	case <-j.done:
		e.mu.Lock() // the job holds its result even after the history evicted its id
		defer e.mu.Unlock()
		return j.res, nil
	case <-ctx.Done():
		return def.Result{}, ctx.Err()
	}
}

// Run is the one goroutine. It returns when ctx ends, which is when the reload fiber
// unloads. A queued operation is then abandoned, not applied: applying a generation
// while the process is disposing the plugin that applies it would leave the tree
// half way with no one to judge it (open point, the docs are silent).
func (e *Engine) Run(ctx context.Context) error {
	wctx := context.WithValue(ctx, nested{}, struct{}{})
	for {
		if ctx.Err() != nil {
			e.close()
			return nil
		}
		j := e.pop()
		if j == nil {
			select {
			case <-ctx.Done():
			case <-e.wake:
			}
			continue
		}
		e.finish(j, e.process(wctx, j.res))
	}
}

// pop takes the oldest waiting job and marks it Running.
func (e *Engine) pop() *job {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.queue) == 0 {
		return nil
	}
	j := e.queue[0]
	e.queue = e.queue[1:]
	j.res.Status = def.Running
	return j
}

// close refuses every later Submit and abandons what is queued.
func (e *Engine) close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	for _, j := range e.queue {
		j.res.Status, j.res.Err = def.Abandoned, def.ErrAbandoned
		e.final(j)
	}
	e.queue = nil
}

func (e *Engine) finish(j *job, r def.Result) {
	e.mu.Lock()
	defer e.mu.Unlock()
	j.res = r
	e.final(j)
}

// final records a finished job: it wakes the waiters and keeps the id in the history,
// of which only the newest historyMax are kept. e.mu is held.
func (e *Engine) final(j *job) {
	close(j.done)
	e.finished = append(e.finished, j.res.ID)
	for len(e.finished) > e.historyMax {
		delete(e.jobs, e.finished[0])
		e.finished = e.finished[1:]
	}
}

// process runs the stages of one operation. It touches no field of the engine that a
// Submit reads.
func (e *Engine) process(ctx context.Context, r def.Result) def.Result {
	stop := func(stage def.Stage, err error) def.Result {
		r.Stage, r.Err = stage, err
		if ctx.Err() != nil {
			r.Status, r.Err = def.Abandoned, errors.Join(def.ErrAbandoned, err)
		} else {
			r.Status = def.Rejected
		}
		return r
	}
	if !e.started {
		e.started, e.current = true, e.Profile.Committed()
	}
	r.Previous = e.current
	cand, err := e.Profile.Compose(ctx)
	if err != nil {
		return stop(def.StageCompose, err)
	}
	r.Digest = cand.Digest
	r.Admission, err = e.Profile.Admit(ctx, cand)
	if err != nil {
		return stop(def.StageAdmit, err)
	}
	cand = detach(cand, r.Admission)
	r.Plan, err = e.Profile.Plan(ctx, e.current, cand)
	if err != nil {
		return stop(def.StagePlan, err)
	}
	before := e.activeBefore()
	ops := operations(r.Plan, e.current, cand)
	if len(ops) > 0 {
		if r.Applied, err = e.Loader.Apply(ctx, ops); err != nil {
			return stop(def.StageApply, err)
		}
	}
	// The loader refuses a plan whole and with no effect, so from here the tree is
	// the candidate. A generation whose verdict fails stays applied at S1.
	e.current = cand
	e.generation++
	r.Generation, r.Stage = e.generation, def.StageSettle
	r.Report, err = e.judge(ctx, cand, r.Plan, r.Applied, before)
	if err != nil {
		return stop(def.StageSettle, err)
	}
	r.Status = def.Passed
	if !r.Report.Pass {
		r.Status, r.Err = def.Failed, def.ErrVerdict
	}
	r.Stage = def.StagePublish
	if err := e.Publish(ctx, composition.Applied{Digest: composition.Digest(cand.Digest), Generation: r.Generation, Report: r.Report}); err != nil {
		r.Err = errors.Join(r.Err, fmt.Errorf("publish composition/applied: %w", err))
	}
	return r
}

// detach disables the optional rows the gate denied: "a denied optional row is
// detached as disabled" (03-c3-kernel.md:358). A denied required row is an error of
// Admit and never reaches here.
func detach(c launch.Composition, a launch.Admission) launch.Composition {
	if len(a.Denied) == 0 {
		return c
	}
	rows := slices.Clone(c.Rows)
	for i := range rows {
		if slices.ContainsFunc(a.Denied, func(d launch.Denial) bool { return d.Row == rows[i].ID }) {
			rows[i].Enabled = false
		}
	}
	c.Rows = rows
	return c
}

// operations turns a plan into the one batch the loader applies: removes, children
// before their groups, then updates, then creates, parents before children
// (03-c3-kernel.md:379; sdk/def/loader/v0 Loader).
func operations(p launch.Plan, from, to launch.Composition) []loader.Op {
	action := map[string]launch.Action{}
	for _, e := range p.Entries {
		action[e.ID] = e.Action
	}
	var ops []loader.Op
	for _, row := range slices.Backward(from.Rows) {
		if action[row.ID] == launch.ActionRemove {
			ops = append(ops, loader.Op{Kind: loader.OpRemove, Entry: loader.EntrySpec{ID: row.ID}})
		}
	}
	for _, kind := range []struct {
		a launch.Action
		k loader.OpKind
	}{{launch.ActionUpdate, loader.OpUpdate}, {launch.ActionCreate, loader.OpCreate}} {
		for _, row := range to.Rows {
			if action[row.ID] == kind.a {
				ops = append(ops, loader.Op{Kind: kind.k, Entry: spec(row)})
			}
		}
	}
	return ops
}

func spec(r launch.Row) loader.EntrySpec {
	return loader.EntrySpec{ID: r.ID, Parent: r.Parent, Plugin: r.Plugin, Config: r.Config, Enabled: r.Enabled,
		Roles: r.Roles, Inject: r.Inject, Isolate: r.Isolate, Intercept: r.Intercept}
}

// activeBefore lists the rows of the running composition that are ACTIVE now.
func (e *Engine) activeBefore() map[string]bool {
	out := map[string]bool{}
	for _, row := range e.current.Rows {
		if en, ok := e.Loader.Locate(row.ID); ok && en.Mount == loader.Mounted && en.State == kernel.Active {
			out[row.ID] = true
		}
	}
	return out
}

// judge settles and judges the generation by the rule of 03-c3-kernel.md:380 and
// plugin-system-2026-11.md:369: every row this generation created, changed or enabled
// must be ACTIVE, and so must every required row, and a row that was ACTIVE must
// still be; a row that was inactive and is untouched is a warning. A row that the
// loader did not mount (disabled, of another role) is not judged unless it is
// required, and then it fails. Settling is loader.Await, which returns once a fiber is out of its
// transition, and the kernel's three deadlines bound every transition, so the
// settle deadline of the docs is those deadlines; ctx ends it at disposal.
func (e *Engine) judge(ctx context.Context, cand launch.Composition, plan launch.Plan, applied []loader.Result, before map[string]bool) (composition.Report, error) {
	need := e.Profile.Required()
	required := map[string]bool{}
	for _, id := range need.Rows {
		required[id] = true
	}
	touched := map[string]bool{}
	for _, r := range applied {
		if r.Outcome == loader.Created || r.Outcome == loader.Restarted || r.Outcome == loader.Committed {
			touched[r.ID] = true
		}
	}
	for _, id := range plan.Closure {
		touched[id] = true
	}
	var states []launch.RowState
	var rep composition.Report
	for _, row := range cand.Rows {
		if row.Required {
			required[row.ID] = true
		}
		st, err := e.Loader.Await(ctx, row.ID)
		switch {
		case ctx.Err() != nil:
			return rep, ctx.Err()
		case err != nil && required[row.ID]:
			rep.Rows = append(rep.Rows, composition.RowReport{ID: row.ID, Outcome: "fail", Detail: "required row is not mounted: " + err.Error()})
			continue
		case err != nil:
			continue // not mounted (disabled, other role): nothing runs, nothing to judge
		}
		states = append(states, launch.RowState{ID: row.ID, Plugin: row.Plugin, State: st})
		switch {
		case st == kernel.Active && (touched[row.ID] || required[row.ID] || before[row.ID]):
			rep.Rows = append(rep.Rows, composition.RowReport{ID: row.ID, Outcome: "pass", State: st})
		case touched[row.ID] || required[row.ID] || before[row.ID]:
			rep.Rows = append(rep.Rows, composition.RowReport{ID: row.ID, Outcome: "fail", State: st, Detail: why(row.ID, touched, required)})
		case st != kernel.Active:
			rep.Rows = append(rep.Rows, composition.RowReport{ID: row.ID, Outcome: "warning", State: st, Detail: "was not ACTIVE and this generation did not touch it"})
		}
	}
	// The required keys: each needs an ACTIVE provider. A required row that is absent
	// from the candidate is as unmet as one that is not ACTIVE.
	unmet := launch.RequiredSet{Keys: need.Keys}.Unmet(states, func(plugin string) []string {
		en, _ := e.Catalog.Lookup(plugin)
		return en.Provides
	})
	for _, u := range unmet {
		rep.Rows = append(rep.Rows, composition.RowReport{ID: u, Outcome: "fail", Detail: "required key has no ACTIVE provider"})
	}
	for id := range required {
		if !slices.ContainsFunc(cand.Rows, func(r launch.Row) bool { return r.ID == id }) {
			rep.Rows = append(rep.Rows, composition.RowReport{ID: "row:" + id, Outcome: "fail", Detail: "required row is not in the generation"})
		}
	}
	slices.SortFunc(rep.Rows, func(a, b composition.RowReport) int { return cmp.Compare(a.ID, b.ID) })
	rep.Pass = !slices.ContainsFunc(rep.Rows, func(r composition.RowReport) bool { return r.Outcome == "fail" })
	return rep, nil
}

func why(id string, touched, required map[string]bool) string {
	switch {
	case touched[id]:
		return "created, changed or enabled by this generation and not ACTIVE"
	case required[id]:
		return "required and not ACTIVE"
	}
	return "was ACTIVE and is not any more"
}
