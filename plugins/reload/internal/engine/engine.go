// SPDX-License-Identifier: Apache-2.0

// Package engine is the queue and the pipeline of the reload row: one queue, one
// goroutine, and for each submission compose, admit, plan, apply in one loader batch,
// settle and judge, then publish composition/applied (03-c3-kernel.md:65, :374-382;
// plugin-system-2026-11.md §9).
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
	"reflect"
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
	*Memory
	queueMax, historyMax int

	closed bool
	wake   chan struct{}

	// The worker's own: it applied a restart of its own row, so the next activation
	// takes the queue over.
	handover bool
}

// Memory is what the engines of one reload row keep across its activations: the last
// operation id, the waiting operations, the number of the last generation, the results
// of the final operations and whether the state of the tree is known. A restart of the
// row (a generation that changes its config) then numbers no operation or generation
// again from 1, keeps the result of the operation that restarted it and runs what
// waited behind it.
type Memory struct {
	mu       sync.Mutex
	last     def.ID
	jobs     map[def.ID]*job
	finished []def.ID // ids of final jobs, oldest first, for the bound on history
	queue    []*job
	unknown  bool // after RollbackFailed, or an abandon after the apply: every later operation is refused (ADR-059)

	// The worker's own. Only one worker runs at a time: an activation starts once the
	// last one has unloaded.
	generation uint64      // the number of the last generation applied or restored
	announce   *def.Result // a final result that left the state unknown and whose event was not emitted
}

// NewMemory returns a memory with no operation.
func NewMemory() *Memory { return &Memory{jobs: map[def.ID]*job{}} }

var _ def.Reload = (*Engine)(nil)

type job struct {
	res  def.Result
	done chan struct{}
}

// nested marks the context of the worker's own work.
type nested struct{}

// New returns an engine over m whose queue holds at most queueMax waiting operations and
// whose history keeps the results of the last historyMax final ones. The tree runs
// Profile.Committed as it is at each operation.
func New(d Deps, m *Memory, queueMax, historyMax int) *Engine {
	return &Engine{Deps: d, Memory: m, queueMax: queueMax, historyMax: historyMax, wake: make(chan struct{}, 1)}
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
	case e.unknown:
		return 0, def.ErrStateUnknown
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
// half way with no one to judge it (the docs are silent; ADR-059 decision 37). Once the worker
// has applied a restart of its own row, it takes no more operations and leaves them
// queued for the next activation. Run first emits the final event of an operation
// that left the state unknown when the activation that ran it could not.
func (e *Engine) Run(ctx context.Context) error {
	wctx := context.WithValue(ctx, nested{}, struct{}{})
	if a := e.announce; a != nil && e.Progress(wctx, *a) == nil {
		e.announce = nil
	}
	for {
		if ctx.Err() != nil || e.handover {
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

// close refuses every later Submit to this engine and abandons what is queued, unless
// the next activation takes it over.
func (e *Engine) close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	if e.handover {
		return
	}
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
// Submit reads but unknown, under the lock. An operation that gets as far as applying
// emits reload/operation as it begins to apply and again with its final result, after
// any rollback was judged and committed: in between, restart-policy holds its give-ups
// and the retries of the plan's rows. Once the state of the tree is unknown, every
// operation is rejected before it composes.
func (e *Engine) process(ctx context.Context, r def.Result) (final def.Result) {
	applying := false
	defer func() {
		if applying && e.progress(ctx, &final) != nil && errors.Is(final.Err, def.ErrStateUnknown) {
			a := final // the activation is unloading: the next one emits it
			e.announce = &a
		}
	}()
	var tree *launch.Composition // set once the loader's entries hold the candidate
	stop := func(stage def.Stage, err error) def.Result {
		r.Stage, r.Err, r.Status = stage, err, def.Rejected
		if tree != nil { // applied and never judged
			e.commit(&r, *tree) // readiness follows the real tree
			e.lose(&r)
		}
		if tree != nil || ctx.Err() != nil {
			r.Status, r.Err = def.Abandoned, errors.Join(def.ErrAbandoned, r.Err)
		}
		return r
	}
	e.mu.Lock()
	unknown := e.unknown
	e.mu.Unlock()
	if unknown {
		return stop(def.StageCompose, def.ErrStateUnknown)
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
	if err == nil {
		err = e.selfEnds(r.Plan, prev, cand)
	}
	if err != nil {
		return stop(def.StagePlan, err)
	}
	before := e.activeBefore(prev)
	ops, last := e.split(operations(r.Plan, prev, cand), prev)
	last = e.unapplied(last, cand)
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
	r.Report, err = e.judge(ctx, cand, r.Applied, before, false)
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
			// A refusal (Admit decoded the config and selfEnds refused a new reference, so a
			// veto of internal/commit) leaves the row its instance and the generation passed
			// and committed: the next generation that passes applies the change (unapplied).
			r.Err = errors.Join(r.Err, fmt.Errorf("apply the reload row: %w", err))
		}
		// The restart of the row ends this activation; the next one runs what waits.
		// ponytail: a restart that a listener of internal/update vetoes leaves the queue
		// to the row's next activation; no S1 row listens there.
		e.handover = slices.ContainsFunc(res, func(x loader.Result) bool { return x.Outcome == loader.Restarted })
	}
	return r
}

// lose marks the state of the tree unknown (ADR-059): the operation of r applied a
// generation that it could neither judge nor roll back. Every later operation is
// refused, so none plans from a composition the loader may not hold.
func (e *Engine) lose(r *def.Result) {
	e.mu.Lock()
	e.unknown = true
	e.mu.Unlock()
	r.Err = errors.Join(r.Err, def.ErrStateUnknown)
}

// errSelf is why a generation that would end the activation applying it is rejected.
var errSelf = errors.New("reload: the generation ends the activation that applies it; restart the process to apply it")

// errSelfRef is why a change of the reload row's config that holds a reference is rejected.
var errSelfRef = errors.New("reload: the reload row's new config holds a reference, which cannot be resolved before the rest of the generation is committed; restart the process to apply it")

// selfEnds refuses, before anything is applied, a plan that would end the activation
// that applies it half way. split defers the reload row's own config change to the end,
// where it restarts the row in one loader batch; but a replacement of the row (its
// plugin, inject, isolate or intercept changes) is a dispose and then a mount once the
// old fiber is DISPOSED, which the disposed activation cannot wait for, and a restart
// of the row through its group or a provider of a key its row injects (rows of the
// plan's restart closure) cancels the apply of the rest. It also refuses a change of
// the row's config to one that holds a reference: the deferred change is applied after
// the generation was committed and published, too late to reject it when the kernel
// does not resolve it (08 §1.6). A change of anything else leaves a reference in the
// unchanged config as it is, with nothing to resolve.
func (e *Engine) selfEnds(p launch.Plan, prev, cand launch.Composition) error {
	next := map[string]launch.Row{}
	for _, r := range cand.Rows {
		next[r.ID] = r
	}
	changed := map[string]bool{}
	for _, pe := range p.Entries {
		changed[pe.ID] = pe.Action != launch.ActionUnchanged
	}
	for _, o := range prev.Rows {
		n, ok := next[o.ID]
		switch {
		case o.Plugin != e.Self:
		case ok && replaced(spec(o), spec(n)):
			return fmt.Errorf("%w: row %q is replaced, as its plugin, inject, isolate or intercept changes", errSelf, o.ID)
		case slices.Contains(p.Closure, o.ID) && (!changed[o.ID] || e.gatedOn(n, p.Closure, next)):
			return fmt.Errorf("%w: row %q restarts through its group or a provider it needs", errSelf, o.ID)
		case changed[o.ID] && loader.HoldsRef(n.Config) && !reflect.DeepEqual(n.Config, o.Config):
			return fmt.Errorf("%w: row %q", errSelfRef, o.ID)
		}
	}
	return nil
}

// gatedOn reports whether a row of the closure other than r is a group above r or
// provides a key that r's row injects: r restarts through it (the closure holds every
// row that restarts, its dependants included).
func (e *Engine) gatedOn(r launch.Row, closure []string, rows map[string]launch.Row) bool {
	for _, id := range closure {
		en, _ := e.Catalog.Lookup(rows[id].Plugin)
		if id != r.ID && slices.ContainsFunc(en.Provides, func(k string) bool { return slices.Contains(r.Inject, k) }) {
			return true
		}
	}
	for g, ok := rows[r.Parent]; ok; g, ok = rows[g.Parent] {
		if slices.Contains(closure, g.ID) {
			return true
		}
	}
	return false
}

func sameSet(a, b []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b)))
}

// split takes out of ops those on a row of from that runs the plugin of the engine.
// Applied with the others, such an operation restarts or disposes the fiber whose
// goroutine applies the plan, and the plan's context ends with it half way: before the
// loader's second batch and before the verdict. They are applied last, once the
// verdict passed and is published; a rollback leaves them out, as they were not
// applied. selfEnds has refused the plans whose change of the row is not one batch.
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

// unapplied adds to last an update of each reload row of cand whose config the loader
// does not hold: a passed generation committed the row's change, which the loader then
// refused, and no plan from the committed composition lists it. It is applied with the
// row's own operations, after the next verdict that passes. (An abandon after the apply
// and a failed rollback commit such a change too, but leave the state unknown, after
// which nothing is planned.)
func (e *Engine) unapplied(last []loader.Op, cand launch.Composition) []loader.Op {
	for _, row := range cand.Rows {
		held, ok := e.Loader.Locate(row.ID)
		if row.Plugin == e.Self && ok && !reflect.DeepEqual(held.Spec.Config, row.Config) &&
			!slices.ContainsFunc(last, func(op loader.Op) bool { return op.Entry.ID == row.ID }) {
			last = append(last, loader.Op{Kind: loader.OpUpdate, Entry: spec(row)})
		}
	}
	return last
}

// rollback restores prev after the verdict on cand failed (ADR-059 decision 12): one
// inverse plan through the loader, which creates the rows that cand removed once
// their previous fibers are DISPOSED (the loader's rule), removes the rows that cand
// created, gives the rows that cand changed their previous config, reverts its
// volatile commits through the commit path and restarts every row that was ACTIVE
// before and failed in the generation (restartFailed). The restored rows are settled and
// judged against the state before cand (judge, restore): every row that was ACTIVE before
// and every required row must be ACTIVE, and a row that was not ACTIVE before is a
// warning, whatever the inverse plan did to it. It is tried once: a failure is reported with both verdicts and is not retried, the committed
// composition is the one the loader's entries hold afterwards, and the state of the
// tree is unknown (lose). (There is no committer, no lock and no audit: those are S4a.)
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
		e.lose(&r)
		return r
	}
	var err error
	if rb.Plan, err = e.Profile.Plan(ctx, cand, prev); err != nil {
		return fail(err)
	}
	if ops, _ := e.split(e.restartFailed(operations(rb.Plan, cand, prev), prev, before), prev); len(ops) > 0 {
		if rb.Applied, err = e.Loader.Apply(ctx, ops); err != nil {
			if len(rb.Applied) > 0 { // applied half way: the entries are the previous ones
				tree = prev
			}
			return fail(err)
		}
	}
	tree = prev
	if rb.Report, err = e.judge(ctx, prev, rb.Applied, before, true); err != nil {
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

// restartFailed adds to the inverse plan ops a restart (loader.OpRestart) of each row of
// prev that was ACTIVE before the generation, is FAILED now and that no operation of ops
// restarts. Such a row failed in the generation: a dependant that a restart of its
// provider restarted (a row of the closure), or a row whose change restarts nothing. The
// kernel keeps it FAILED until something updates it, an update that changes nothing the
// loader sees leaves it so, and restart-policy holds its retry until the operation is
// final, so without the restart the rollback could never pass.
func (e *Engine) restartFailed(ops []loader.Op, prev launch.Composition, before map[string]bool) []loader.Op {
	for _, row := range prev.Rows {
		en, ok := e.Loader.Locate(row.ID)
		if before[row.ID] && ok && en.State == kernel.Failed &&
			!slices.ContainsFunc(ops, func(op loader.Op) bool { return op.Entry.ID == row.ID && restarts(op, en.Spec) }) {
			ops = append(ops, loader.Op{Kind: loader.OpRestart, Entry: loader.EntrySpec{ID: row.ID}})
		}
	}
	return ops
}

// restarts reports whether op restarts or replaces the FAILED fiber of the entry that
// holds held, by the loader's rule (loader.Loader.Update): an update does only when it
// changes the plugin, inject, isolate or intercept, or a config path of a row that is not
// a group. One of only what the loader does not see (required, roles that keep it
// mounted, a group's config) leaves the fiber FAILED.
func restarts(op loader.Op, held loader.EntrySpec) bool {
	n := op.Entry
	paths, _ := loader.VolatileOnly(held.Config, n.Config, nil)
	return op.Kind != loader.OpUpdate || replaced(held, n) ||
		len(paths) > 0 && n.Plugin != loader.GroupPlugin && n.Plugin != loader.IncludePlugin
}

// replaced reports whether the loader replaces the fiber of an entry whose spec goes from
// o to n: a fiber's plugin, needs and context are fixed when it is mounted.
func replaced(o, n loader.EntrySpec) bool {
	return n.Plugin != o.Plugin || !sameSet(n.Inject, o.Inject) || !sameSet(n.Isolate, o.Isolate) ||
		!(len(n.Intercept) == 0 && len(o.Intercept) == 0 || reflect.DeepEqual(n.Intercept, o.Intercept))
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

// progress emits reload/operation with r. A failure is reported on the result, changes
// no status, and is returned.
func (e *Engine) progress(ctx context.Context, r *def.Result) error {
	err := e.Progress(ctx, *r)
	if err != nil {
		r.Err = errors.Join(r.Err, fmt.Errorf("emit reload/operation: %w", err))
	}
	return err
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
// plugin-system-2026-11.md §9, step 5: every row this generation created, changed or enabled
// (by the loader's outcomes; a row of the restart closure is none of these) must be
// ACTIVE, and so must every required row, and a row that was ACTIVE must still be; a
// row that was inactive and is untouched is a warning. A row that the loader did not
// mount (disabled, of another role) is not judged unless it is required, and then it
// fails. The required set is cand's own for the process role (Profile.RequiredOf): the
// candidate forward, the composition restored for a rollback. A rollback (restore) is
// judged against the state before the generation, not by what its inverse plan touched:
// a row it puts back that was not ACTIVE before (FAILED, given up on; PENDING, its
// provider absent) is a warning, as it was before the generation; every row that was
// ACTIVE before and every required row must be ACTIVE.
func (e *Engine) judge(ctx context.Context, cand launch.Composition, applied []loader.Result, before map[string]bool, restore bool) (composition.Report, error) {
	need, needErr := e.Profile.RequiredOf(cand)
	required := map[string]bool{}
	for _, id := range need.Rows {
		required[id] = true
	}
	touched, untouched := map[string]bool{}, "was not ACTIVE and this generation did not touch it"
	if restore {
		applied, untouched = nil, "was not ACTIVE before the generation either"
	}
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
			rep.Rows = append(rep.Rows, composition.RowReport{ID: row.ID, Outcome: "warning", State: st, Detail: untouched})
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
