// SPDX-License-Identifier: Apache-2.0

// Package engine is the queue and the pipeline of the reload row: one queue, one
// goroutine, and for each submission compose, admit, plan, apply in one loader batch,
// settle and judge, then publish composition/applied (03-c3-kernel.md:65, :374-382;
// plugin-system-2026-11.md:363-371).
//
// At S1 there is no committer (ADR-059 decision 5; S4a), so a passed verdict is
// committed to the launcher with Profile.Commit, and a failed one is rolled back at
// once with one inverse plan through the loader (decision 12).
package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
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
	// Progress emits reload/operation.
	Progress func(ctx context.Context, r def.Result) error
	// Self is the plugin id of the row that runs the engine (see split).
	Self string
	// Go starts a tracked goroutine of the row (kernel.Context.Go). It returns the
	// kernel's refusal, instead of panicking, when the row is unloading.
	Go func(label string, run func(context.Context) error) error
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

	// The worker's own: the number of the last generation applied or restored.
	generation uint64
}

var _ def.Reload = (*Engine)(nil)

type job struct {
	res  def.Result
	done chan struct{}
}

// nested marks the context of the worker's own work.
type nested struct{}

// New returns an engine whose queue holds at most queueMax waiting operations and
// whose history keeps the results of the last historyMax final ones. The tree runs
// Profile.Committed as it is at each operation.
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
// Submit reads. An operation that gets as far as applying emits reload/operation as it
// begins to apply and again with its final result, after any rollback was judged and
// committed: in between, restart-policy holds its give-ups.
func (e *Engine) process(ctx context.Context, r def.Result) (final def.Result) {
	applying := false
	defer func() {
		if applying {
			e.progress(ctx, &final)
		}
	}()
	var tree *launch.Composition // set once the loader's entries hold the candidate
	stop := func(stage def.Stage, err error) def.Result {
		r.Stage, r.Err = stage, err
		if tree != nil {
			e.commit(&r, *tree) // readiness and the next generation follow the real tree
		}
		if ctx.Err() != nil {
			r.Status, r.Err = def.Abandoned, errors.Join(def.ErrAbandoned, r.Err)
		} else {
			r.Status = def.Rejected
		}
		return r
	}
	prev := e.Profile.Committed()
	r.Previous = prev
	cand, err := e.Profile.Compose(ctx)
	if err != nil {
		return stop(def.StageCompose, err)
	}
	r.Digest = cand.Digest
	r.Admission, err = e.Profile.Admit(ctx, cand)
	if len(r.Admission.Denied) > 0 {
		// At boot a denied optional row is detached as disabled; a live generation is
		// rejected as a whole (ADR-059 decision 15). A denied required row is listed
		// with Admit's error.
		err = errors.Join(denied(r.Admission), err)
	}
	if err != nil {
		return stop(def.StageAdmit, err)
	}
	r.Plan, err = e.Profile.Plan(ctx, prev, cand)
	if err != nil {
		return stop(def.StagePlan, err)
	}
	before := e.activeBefore(prev)
	ops, last := e.split(operations(r.Plan, prev, cand), prev)
	r.Stage, applying = def.StageApply, true
	e.progress(ctx, &r)
	var partial error
	if len(ops) > 0 {
		// The loader refuses a plan whole and with no effect. An error that comes with
		// results is one after its first batch committed (loader.Loader.Apply): the tree
		// is the candidate's entries, and the judge sees the rows left with no fiber.
		if r.Applied, partial = e.Loader.Apply(ctx, ops); len(r.Applied) == 0 && partial != nil {
			return stop(def.StageApply, partial)
		}
		tree = &cand
	}
	e.generation++
	r.Generation, r.Stage, r.Err = e.generation, def.StageSettle, partial
	r.Report, err = e.judge(ctx, cand, r.Applied, before)
	if err != nil {
		return stop(def.StageSettle, errors.Join(partial, err))
	}
	r.Stage = def.StagePublish
	if !r.Report.Pass {
		// The failed generation is published as failed, then rolled back; what it
		// restored is published after it. The committed composition stays the previous
		// one until the rollback says what the tree runs.
		r.Err = errors.Join(def.ErrVerdict, r.Err)
		e.publish(ctx, &r, cand.Digest, r.Generation, r.Report)
		return e.rollback(ctx, r, cand, prev, before)
	}
	// Committed before it is published: a listener of composition/applied, readiness
	// among them, reads the required set of this generation.
	r.Status = def.Passed
	e.commit(&r, cand)
	e.publish(ctx, &r, cand.Digest, r.Generation, r.Report)
	if len(last) > 0 {
		// The generation is judged: the operation is final for reload/operation now, as
		// the change of the reload row may end the activation that would emit it.
		applying = false
		e.progress(ctx, &r)
		res, err := e.Loader.Apply(ctx, last)
		r.Applied = append(r.Applied, res...)
		if err != nil {
			r.Err = errors.Join(r.Err, fmt.Errorf("apply the reload row: %w", err))
		}
	}
	return r
}

// split takes out of ops those on a row of from that runs the plugin of the engine.
// Applied with the others, such an operation restarts or disposes the fiber whose
// goroutine applies the plan, and the plan's context ends with it half way: before the
// loader's second batch and before the verdict. They are applied last, once the
// verdict passed and is published; a rollback leaves them out, as they were not
// applied.
// ponytail: a reload row restarted through its group or a provider of its own is not
// found here; that needs the loader to finish a plan on its own lifecycle.
func (e *Engine) split(ops []loader.Op, from launch.Composition) (rest, self []loader.Op) {
	for _, op := range ops {
		if slices.ContainsFunc(from.Rows, func(r launch.Row) bool { return r.ID == op.Entry.ID && r.Plugin == e.Self }) {
			self = append(self, op)
		} else {
			rest = append(rest, op)
		}
	}
	return rest, self
}

// rollback restores prev after the verdict on cand failed (ADR-059 decision 12): one
// inverse plan through the loader, which creates the rows that cand removed once
// their previous fibers are DISPOSED (the loader's rule), removes the rows that cand
// created, gives the rows that cand changed their previous config and reverts its
// volatile commits through the commit path. The restored rows are settled and judged by
// the rule that judges a generation, against what was ACTIVE before cand. It is tried
// once: a failure is reported with both verdicts and is not retried, and the committed
// composition is the one the loader's entries hold afterwards, so that readiness and
// the next generation follow the real tree. (There is no committer, no lock and no
// audit: those are S4a.)
func (e *Engine) rollback(ctx context.Context, r def.Result, cand, prev launch.Composition, before map[string]bool) def.Result {
	rb := &def.Rollback{}
	r.Rollback, r.Stage = rb, def.StageRollback
	tree := cand // what the loader's entries hold
	fail := func(err error) def.Result {
		rb.Err = err
		e.commit(&r, tree)
		if ctx.Err() != nil {
			r.Status, r.Err = def.Abandoned, errors.Join(r.Err, def.ErrAbandoned, err)
		} else {
			r.Status, r.Err = def.RollbackFailed, errors.Join(r.Err, def.ErrRollback, err)
		}
		return r
	}
	var err error
	if rb.Plan, err = e.Profile.Plan(ctx, cand, prev); err != nil {
		return fail(err)
	}
	if ops, _ := e.split(operations(rb.Plan, cand, prev), prev); len(ops) > 0 {
		if rb.Applied, err = e.Loader.Apply(ctx, ops); err != nil {
			if len(rb.Applied) > 0 { // applied half way: the entries are the previous ones
				tree = prev
			}
			return fail(err)
		}
	}
	tree = prev
	if rb.Report, err = e.judge(ctx, prev, rb.Applied, before); err != nil {
		return fail(err)
	}
	if !rb.Report.Pass {
		return fail(errNotRestored)
	}
	e.generation++
	rb.Generation, r.Status = e.generation, def.RolledBack
	e.commit(&r, prev)
	r.Stage = def.StagePublish
	e.publish(ctx, &r, prev.Digest, rb.Generation, rb.Report)
	return r
}

// errNotRestored is why a rollback fails when its loader plan was applied and the
// restored rows are not ACTIVE: its report says which.
var errNotRestored = errors.New("the rows of the previous composition are not ACTIVE again")

// commit tells the launcher what the tree runs, so that Committed and Required follow
// it. A refusal is reported on the result and changes no status.
func (e *Engine) commit(r *def.Result, c launch.Composition) {
	if err := e.Profile.Commit(c); err != nil {
		r.Err = errors.Join(r.Err, fmt.Errorf("commit the composition: %w", err))
	}
}

// publish emits composition/applied for a generation. A failure is reported on the
// result and changes no status.
func (e *Engine) publish(ctx context.Context, r *def.Result, d launch.Digest, generation uint64, rep composition.Report) {
	if err := e.Publish(ctx, composition.Applied{Digest: composition.Digest(d), Generation: generation, Report: rep}); err != nil {
		r.Err = errors.Join(r.Err, fmt.Errorf("publish composition/applied: %w", err))
	}
}

// progress emits reload/operation with r. A failure is reported on the result and
// changes no status.
func (e *Engine) progress(ctx context.Context, r *def.Result) {
	if err := e.Progress(ctx, *r); err != nil {
		r.Err = errors.Join(r.Err, fmt.Errorf("emit reload/operation: %w", err))
	}
}

// denied is the cause of a rejected generation: the gate denied these rows.
func denied(a launch.Admission) error {
	parts := make([]string, len(a.Denied))
	for i, d := range a.Denied {
		parts[i] = fmt.Sprintf("row %q (%s): %s", d.Row, d.Check, d.Reason)
	}
	return fmt.Errorf("%w: %s", def.ErrDenied, strings.Join(parts, "; "))
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

// activeBefore lists the rows of the composition c, the one the tree runs, that are ACTIVE
// now.
func (e *Engine) activeBefore(c launch.Composition) map[string]bool {
	out := map[string]bool{}
	for _, row := range c.Rows {
		if en, ok := e.Loader.Locate(row.ID); ok && en.Mount == loader.Mounted && en.State == kernel.Active {
			out[row.ID] = true
		}
	}
	return out
}

// judge settles and judges the generation by the rule of 03-c3-kernel.md:380 and
// plugin-system-2026-11.md:369: every row this generation created, changed or enabled
// (by the loader's outcomes; a row of the restart closure is none of these) must be
// ACTIVE, and so must every required row, and a row that was ACTIVE must still be; a
// row that was inactive and is untouched is a warning. A row that the loader did not
// mount (disabled, of another role) is not judged unless it is required, and then it
// fails. The required set is cand's own for the process role (Profile.RequiredOf): the
// candidate forward, the composition restored for a rollback.
func (e *Engine) judge(ctx context.Context, cand launch.Composition, applied []loader.Result, before map[string]bool) (composition.Report, error) {
	need, needErr := e.Profile.RequiredOf(cand)
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
	reads, err := e.settle(ctx, cand.Rows)
	if err != nil {
		return composition.Report{}, err
	}
	var states []launch.RowState
	var rep composition.Report
	if needErr != nil { // with no required set, the generation cannot pass
		rep.Rows = append(rep.Rows, composition.RowReport{ID: "required set", Outcome: "fail", Detail: needErr.Error()})
	}
	for i, row := range cand.Rows {
		st, err := reads[i].state, reads[i].err
		switch {
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

// reading is what loader.Await said of one row.
type reading struct {
	state kernel.State
	err   error
}

func same(a, b reading) bool { return a.state == b.state && (a.err == nil) == (b.err == nil) }

// settle reads the rows, each once it is out of its transition (loader.Await), and reads
// them again until a pass reads what the one before it read: a PENDING fiber is not in a
// transition, so a dependant read while its provider loaded is read again after
// ("settle, then judge", 03-c3-kernel.md:380). The rows of a pass are read together, so
// each is read when it settles, whatever another takes. A row read FAILED stays FAILED
// for the verdict: failure is sticky in the kernel, and a retry by restart-policy since
// does not undo an Apply that failed in this generation (08-profiles-roadmap-adrs.md
// §1.6: a live Apply error is a failed verdict). The kernel's deadlines bound each read
// and ctx ends them at disposal.
func (e *Engine) settle(ctx context.Context, rows []launch.Row) ([]reading, error) {
	var last []reading
	failed := make([]bool, len(rows))
	for {
		reads := make([]reading, len(rows))
		done := make(chan struct{}, len(rows))
		var err error
		started := 0
		for i, row := range rows {
			// A tracked goroutine of the row, joined below. Its read ends with the row's
			// Std, so none outlives the row.
			if err = e.Go("reload settle", func(ctx context.Context) error {
				reads[i].state, reads[i].err = e.Loader.Await(ctx, row.ID)
				done <- struct{}{}
				return nil
			}); err != nil {
				break
			}
			started++
		}
		for range started {
			<-done
		}
		if err := errors.Join(err, ctx.Err()); err != nil {
			return nil, err
		}
		for i, rd := range reads {
			failed[i] = failed[i] || rd.state == kernel.Failed
		}
		if slices.EqualFunc(reads, last, same) {
			break
		}
		last = reads
	}
	for i := range last {
		if failed[i] {
			last[i].state = kernel.Failed
		}
	}
	return last, nil
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
