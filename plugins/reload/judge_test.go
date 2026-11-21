// SPDX-License-Identifier: Apache-2.0

package reload_test

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/plugins/reload"
	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	launchfake "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0/fake"
	loader "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// script is a loader whose rows the test scripts. Apply records each operation with the
// outcome the real loader gives it (a create of an id that exists is refused whole) and
// then calls onApply, whose error it returns with its results, as the real loader does
// when its second batch did not run. A row starts in the state start names, ACTIVE by
// default. Await reads a row, then runs the test's hook for it: the hook stands for what
// the kernel does meanwhile.
type script struct {
	mu      sync.Mutex
	rows    map[string]loader.Entry
	start   map[string]kernel.State
	after   map[string]func(ctx context.Context)
	onApply func(ctx context.Context, n int, ops []loader.Op) error
	refuse  func(ops []loader.Op) error // an error refuses the plan whole, with no effect
	applies int
}

func newScript() *script {
	return &script{rows: map[string]loader.Entry{}, start: map[string]kernel.State{}, after: map[string]func(context.Context){}}
}

func (s *script) Apply(ctx context.Context, ops []loader.Op) ([]loader.Result, error) {
	if s.refuse != nil {
		if err := s.refuse(ops); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	var res []loader.Result
	for _, op := range ops {
		id := op.Entry.ID
		switch _, ok := s.rows[id]; {
		case op.Kind == loader.OpCreate && ok:
			s.mu.Unlock()
			return nil, fmt.Errorf("create %s: %w", id, loader.ErrExists)
		case op.Kind == loader.OpCreate:
			s.rows[id] = loader.Entry{Spec: op.Entry, Mount: loader.Mounted, State: cmp.Or(s.start[id], kernel.Active)}
			res = append(res, loader.Result{ID: id, Outcome: loader.Created})
		case op.Kind == loader.OpRemove:
			delete(s.rows, id)
			res = append(res, loader.Result{ID: id, Outcome: loader.Removed})
		case op.Kind == loader.OpRestart:
			res = append(res, loader.Result{ID: id, Outcome: loader.Restarted})
		default:
			e := s.rows[id]
			e.Spec = op.Entry
			s.rows[id] = e
			res = append(res, loader.Result{ID: id, Outcome: loader.Restarted})
		}
	}
	s.applies++
	n := s.applies
	s.mu.Unlock()
	if s.onApply == nil {
		return res, nil
	}
	return res, s.onApply(ctx, n, ops)
}

func (s *script) Await(ctx context.Context, id string) (kernel.State, error) {
	s.mu.Lock()
	e, ok := s.rows[id]
	h := s.after[id]
	s.mu.Unlock()
	if h != nil {
		h(ctx)
	}
	switch {
	case ctx.Err() != nil:
		return "", ctx.Err()
	case !ok:
		return "", loader.ErrNotFound
	case e.Mount != loader.Mounted:
		return "", loader.ErrNotMounted
	}
	return e.State, nil
}

func (s *script) set(id string, st kernel.State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.rows[id]
	e.State = st
	s.rows[id] = e
}

func (s *script) Locate(id string) (loader.Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.rows[id]
	return e, ok
}

func (s *script) Create(context.Context, loader.EntrySpec) error { return nil }
func (s *script) Update(context.Context, loader.EntrySpec) error { return nil }
func (s *script) Remove(context.Context, string) error           { return nil }

// sworld is reload on the launcher fake and a script loader.
type sworld struct {
	t       *testing.T
	root    *kernel.Root
	l       *launchfake.Launcher
	s       *script
	fiber   *kernel.Fiber
	svc     def.Reload
	next    launch.Composition
	commits []launch.Digest
}

// newScripted mounts reload over s, with prev as the composition the tree runs: the
// script holds its rows, ACTIVE unless mounts says otherwise.
func newScripted(t *testing.T, s *script, mounts map[string]loader.Mount, prev ...launch.Row) *sworld {
	t.Helper()
	w := &sworld{t: t, root: kerneltest.New(t), s: s}
	var lp kernel.Plugin
	w.l, lp = launchfake.New()
	w.l.Current = launch.Composition{Rows: prev, Digest: launch.Digest{1}}
	w.l.ComposeFn = func(context.Context) (launch.Composition, error) { return w.next, nil }
	w.l.PlanFn = func(_ context.Context, f, to launch.Composition) (launch.Plan, error) { return diff(f, to), nil }
	w.l.CommitFn = func(c launch.Composition) error { w.commits = append(w.commits, c.Digest); return nil }
	for _, r := range prev {
		s.rows[r.ID] = loader.Entry{Spec: loader.EntrySpec{ID: r.ID, Plugin: r.Plugin, Config: r.Config, Enabled: true},
			Mount: cmp.Or(mounts[r.ID], loader.Mounted), State: kernel.Active}
	}
	sp := kernel.Define(kernel.Plugin{ID: "aiops/script-loader", Provides: []kernel.AnyKey{loader.Key},
		Apply: func(c *kernel.Context, _ any) error { return loader.Key.Provide(c, s) }})
	kerneltest.Mount(w.root, lp, sp)
	w.fiber = w.root.Plugin(reload.New(), reload.Config{Queue: 4, History: 8})
	active(t, w.fiber)
	w.svc = submitter(t, w.root, "aiops/submitter")
	return w
}

// submitter mounts a row that needs reload and returns the service it was given.
func submitter(t *testing.T, root *kernel.Root, id string) def.Reload {
	t.Helper()
	var svc def.Reload
	p := kernel.Define(kernel.Plugin{ID: id, Inject: []kernel.Dep{kernel.Need(def.Key)},
		Apply: func(c *kernel.Context, _ any) error { svc = def.Key.Of(c); return nil }})
	active(t, root.Plugin(p, nil))
	return svc
}

// await submits to svc and waits a bounded time for the result: a judge that waits
// for one row before it reads another never answers in the tests that need it to.
func await(t *testing.T, svc def.Reload) def.Result {
	t.Helper()
	id, err := svc.Submit(t.Context(), def.Op{Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	r, err := svc.Await(ctx, id)
	if err != nil {
		t.Fatalf("no result: %v", err)
	}
	return r
}

func outcome(rep composition.Report, id string) composition.RowReport {
	for _, r := range rep.Rows {
		if r.ID == id {
			return r
		}
	}
	return composition.RowReport{}
}

// A generation that changes the reload row itself is applied, settled, judged, committed
// and published like any other; reload restarts its own row last, so the restart cancels
// nothing of the apply, and the next generation is planned from what the tree runs.
func TestGenerationThatRestartsTheReloadRowFinishesBeforeTheRestart(t *testing.T) {
	s := newScript()
	self := row("reload", "aiops/reload", map[string]any{"queue": int64(4), "history": int64(8)})
	w := newScripted(t, s, nil, self, row("a", "aiops/p", nil))
	changed := row("reload", "aiops/reload", map[string]any{"queue": int64(8), "history": int64(8)})
	w.next = launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{changed, row("a", "aiops/p", nil), row("x", "aiops/p", nil)}}
	var order []string
	s.onApply = func(_ context.Context, _ int, ops []loader.Op) error {
		for _, op := range ops {
			order = append(order, op.Entry.ID)
			if op.Entry.ID == "reload" { // the loader restarts the reload fiber with its new config
				if err := w.fiber.Update(t.Context(), reload.Config{Queue: 8, History: 8}); err != nil {
					t.Error(err)
				}
			}
		}
		return nil
	}

	r := await(t, w.svc)
	if r.Status != def.Passed || r.Err != nil || r.Generation != 1 || outcome(r.Report, "x").Outcome != "pass" {
		t.Fatalf("result = %+v: the restart of the reload row cancelled the apply of its own generation", r)
	}
	if want := []string{"x", "reload"}; !slices.Equal(order, want) {
		t.Errorf("applied %v, want %v: the reload row is applied last", order, want)
	}
	if got := ids(r.Applied); !slices.Equal(got, []string{"created x", "restarted reload"}) {
		t.Errorf("loader results = %v", got)
	}
	if w.l.Committed().Digest != (launch.Digest{2}) {
		t.Errorf("committed = %x", w.l.Committed().Digest)
	}
	active(t, w.fiber) // the restart is over
	svc2 := submitter(t, w.root, "aiops/submitter2")
	r2 := await(t, svc2)
	if r2.Status != def.Passed || r2.Previous.Digest != (launch.Digest{2}) || len(r2.Applied) != 0 {
		t.Errorf("next result = %+v: it is planned from the generation the tree runs", r2)
	}
	// The restarted row goes on numbering, and still knows the operation that restarted it.
	if r2.ID != 2 || r2.Generation != 2 {
		t.Errorf("next operation %d, generation %d: want 2 and 2 after operation 1 and generation 1", r2.ID, r2.Generation)
	}
	if r1, ok := svc2.Result(1); !ok || r1.Status != def.Passed || r1.Generation != 1 {
		t.Errorf("the result of the operation that restarted the row is %+v, %v", r1, ok)
	}
}

// The reload row's own operation waits for the verdict: a failed generation never applies
// it, and its rollback has nothing of it to undo.
func TestReloadRowIsAppliedOnlyAfterAPassedVerdict(t *testing.T) {
	s := newScript()
	w := newScripted(t, s, nil, row("reload", "aiops/reload", "old"))
	w.next = launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{row("reload", "aiops/reload", "new"), row("x", "aiops/p", nil)}}
	s.start["x"] = kernel.Failed
	var order []string
	s.onApply = func(_ context.Context, _ int, ops []loader.Op) error {
		for _, op := range ops {
			order = append(order, op.Entry.ID)
		}
		return nil
	}
	r := await(t, w.svc)
	if r.Status != def.RolledBack || !slices.Equal(order, []string{"x", "x"}) {
		t.Fatalf("result = %+v, applied %v", r, order)
	}
	if r.Rollback == nil || r.Rollback.Err != nil || w.l.Committed().Digest != (launch.Digest{1}) {
		t.Errorf("rollback = %+v, committed %x: the tree runs the previous composition", r.Rollback, w.l.Committed().Digest)
	}
	if e, _ := s.Locate("reload"); e.Spec.Config != "old" {
		t.Errorf("the reload row runs %v", e.Spec.Config)
	}
}

// opLog is what a listener of reload/operation heard, in order.
type opLog struct {
	mu      sync.Mutex
	results []def.Result
	wake    chan struct{} // signalled after each event
}

// opEvents mounts a listener of reload/operation and returns its log.
func opEvents(t *testing.T, root *kernel.Root) *opLog {
	t.Helper()
	o := &opLog{wake: make(chan struct{}, 1)}
	p := kernel.Define(kernel.Plugin{ID: "aiops/op-events", Hooks: []kernel.HookUse{def.Operation.Use(kernel.DefaultStage)},
		Apply: func(c *kernel.Context, _ any) error {
			def.Operation.On(c, kernel.DefaultStage, func(_ context.Context, r def.Result) (any, error) {
				o.mu.Lock()
				o.results = append(o.results, r)
				o.mu.Unlock()
				select {
				case o.wake <- struct{}{}:
				default:
				}
				return nil, nil
			})
			return nil
		}})
	active(t, root.Plugin(p, nil))
	return o
}

// heard returns the events so far, as "status stage", and their results.
func (o *opLog) heard() ([]string, []def.Result) {
	o.mu.Lock()
	defer o.mu.Unlock()
	var s []string
	for _, r := range o.results {
		s = append(s, fmt.Sprintf("%s %s", r.Status, r.Stage))
	}
	return s, slices.Clone(o.results)
}

// await waits until n events were heard and returns their results.
func (o *opLog) await(t *testing.T, n int) []def.Result {
	t.Helper()
	for {
		if _, got := o.heard(); len(got) >= n {
			return got
		}
		select {
		case <-o.wake:
		case <-t.Context().Done():
			t.Fatalf("fewer than %d reload/operation events", n)
		}
	}
}

// A refusal of the reload row's own change, which comes once the verdict passed and was
// published, leaves the generation passed: the row keeps its instance, the refusal is on
// the result, and reload/operation carries one running and one final event for the
// operation. The next generation that passes applies the change.
func TestRefusedChangeOfTheReloadRowLeavesTheGenerationPassed(t *testing.T) {
	s := newScript()
	w := newScripted(t, s, nil, row("reload", "aiops/reload", "old"))
	events := opEvents(t, w.root)
	w.next = launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{row("reload", "aiops/reload", "new"), row("x", "aiops/p", nil)}}
	boom := errors.New("refused")
	s.refuse = func(ops []loader.Op) error {
		if slices.ContainsFunc(ops, func(op loader.Op) bool { return op.Entry.ID == "reload" }) {
			return boom
		}
		return nil
	}
	r := await(t, w.svc)
	if r.Status != def.Passed || r.Rollback != nil || !errors.Is(r.Err, boom) || !strings.Contains(r.Err.Error(), "apply the reload row") {
		t.Fatalf("result = %+v", r)
	}
	if got, _ := events.heard(); !slices.Equal(got, []string{"running apply", "passed publish"}) {
		t.Errorf("reload/operation = %q, want one running and one final event", got)
	}
	if s.applies != 1 || w.l.Committed().Digest != (launch.Digest{2}) {
		t.Errorf("applies %d, committed %x: the generation was applied and committed", s.applies, w.l.Committed().Digest)
	}
	if e, _ := s.Locate("reload"); e.Spec.Config != "old" {
		t.Errorf("the reload row runs %v, want its instance kept", e.Spec.Config)
	}
	s.refuse = nil
	if r := await(t, w.svc); r.Status != def.Passed || !slices.Equal(ids(r.Applied), []string{"restarted reload"}) {
		t.Fatalf("next result = %+v", r)
	}
	if e, _ := s.Locate("reload"); e.Spec.Config != "new" {
		t.Errorf("the reload row runs %v, want the change the next generation applied", e.Spec.Config)
	}
}

// A generation that would end the activation applying it half way is rejected before
// anything is applied: a replacement of the reload row (its plugin, inject, isolate or
// intercept changes) is a dispose and then a mount that the disposed activation cannot
// finish, and a restart of the row through its group or a provider it needs cancels the
// apply of the rest. Before this, such a generation left reload with no fiber and
// reported it as passed.
func TestGenerationThatEndsTheReloadRowsActivationIsRejected(t *testing.T) {
	self := row("reload", "aiops/reload", "old")
	replaced := func(f func(*launch.Row)) launch.Row {
		r := row("reload", "aiops/reload", "new")
		f(&r)
		return r
	}
	for _, tc := range []struct {
		name    string
		reload  launch.Row
		closure []string
	}{
		{"inject", replaced(func(r *launch.Row) { r.Inject = []string{"configRefsReady"} }), nil},
		{"isolate", replaced(func(r *launch.Row) { r.Isolate = []string{"store"} }), nil},
		{"intercept", replaced(func(r *launch.Row) { r.Intercept = map[string]any{"store": map[string]any{"mark": int64(1)}} }), nil},
		{"plugin", replaced(func(r *launch.Row) { r.Plugin = "aiops/other" }), nil},
		{"restart through a provider", self, []string{"reload", "refs"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newScript()
			w := newScripted(t, s, nil, self, row("refs", "aiops/config-refs", "a"))
			w.next = launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{tc.reload, row("refs", "aiops/config-refs", "b"), row("x", "aiops/p", nil)}}
			w.l.PlanFn = func(_ context.Context, f, to launch.Composition) (launch.Plan, error) {
				p := diff(f, to)
				p.Closure = tc.closure
				return p, nil
			}
			r := await(t, w.svc)
			if r.Status != def.Rejected || r.Stage != def.StagePlan || r.Err == nil || !strings.Contains(r.Err.Error(), "ends the activation that applies it") {
				t.Fatalf("result = %+v", r)
			}
			if s.applies != 0 || len(w.commits) != 0 {
				t.Errorf("applies %d, commits %v: a rejected generation touches nothing", s.applies, w.commits)
			}
		})
	}
}

// When the reload fiber is disposed while a generation it applied settles, the operation
// is abandoned, and the composition the loader holds, the candidate, is committed.
func TestAbandonAfterTheApplyCommitsWhatTheLoaderHolds(t *testing.T) {
	root := kerneltest.New(t)
	l, lp := launchfake.New()
	bl := &blockingLoader{entered: make(chan struct{}, 4), release: make(chan struct{}), awaiting: make(chan struct{}, 4)}
	close(bl.release)
	lpl := kernel.Define(kernel.Plugin{ID: "aiops/blocking-loader", Provides: []kernel.AnyKey{loader.Key},
		Apply: func(c *kernel.Context, _ any) error { return loader.Key.Provide(c, bl) }})
	l.Current = launch.Composition{Digest: launch.Digest{1}}
	l.ComposeFn = func(context.Context) (launch.Composition, error) {
		return launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{row("a", "aiops/p", nil)}}, nil
	}
	l.PlanFn = func(_ context.Context, f, to launch.Composition) (launch.Plan, error) { return diff(f, to), nil }
	kerneltest.Mount(root, lp, lpl)
	rf := root.Plugin(reload.New(), reload.Config{Queue: 4, History: 4})
	active(t, rf)
	svc := submitter(t, root, "aiops/caller")
	id, err := svc.Submit(t.Context(), def.Op{})
	if err != nil {
		t.Fatal(err)
	}
	<-bl.awaiting
	if err := rf.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	r, err := svc.Await(t.Context(), id)
	if err != nil || r.Status != def.Abandoned || r.Stage != def.StageSettle {
		t.Fatalf("result = %+v, %v", r, err)
	}
	if l.Committed().Digest != (launch.Digest{2}) {
		t.Errorf("committed = %x: the loader holds the candidate, so the next generation must plan from it", l.Committed().Digest)
	}
}

// A consumer listed before its provider is PENDING while the provider loads, and a PENDING
// fiber is not in a transition: the judge reads it again once the provider settled.
func TestDependantListedBeforeItsProviderIsJudgedOnceTheTreeSettles(t *testing.T) {
	s := newScript()
	w := newScripted(t, s, nil)
	w.next = launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{row("c", "aiops/p", nil), row("p", "aiops/p", nil)}}
	s.start["c"] = kernel.Pending
	s.after["p"] = func(context.Context) { s.set("c", kernel.Active) } // p is ACTIVE: c activates
	r := await(t, w.svc)
	if r.Status != def.Passed || !r.Report.Pass {
		t.Fatalf("result = %+v: c was judged before the tree settled", r)
	}
	if got := outcome(r.Report, "c"); got.Outcome != "pass" || got.State != kernel.Active {
		t.Errorf("c = %+v", got)
	}
}

// A row that the generation created and whose Apply failed is a failed verdict, whatever an
// unrelated row takes to load: restart-policy may retry it meanwhile, but the judge read it
// when it settled.
func TestRetriedRowIsJudgedWhenItSettledWhateverAnotherRowTakes(t *testing.T) {
	s := newScript()
	w := newScripted(t, s, nil, row("a", "aiops/p", nil))
	w.next = launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{row("a", "aiops/p", nil),
		row("slow", "aiops/p", nil), row("flaky", "aiops/p", nil)}}
	s.start["flaky"] = kernel.Failed
	read := make(chan struct{})
	var once sync.Once
	s.after["flaky"] = func(context.Context) { once.Do(func() { close(read) }) }
	s.after["slow"] = func(ctx context.Context) {
		// slow loads for longer than flaky's backoff: restart-policy retries flaky and it is
		// ACTIVE before slow is.
		select {
		case <-read:
		case <-ctx.Done():
			return
		}
		s.set("flaky", kernel.Active)
	}
	r := await(t, w.svc)
	if r.Status != def.RolledBack {
		t.Fatalf("result = %+v", r)
	}
	if got := outcome(r.Report, "flaky"); got.Outcome != "fail" || got.State != kernel.Failed {
		t.Errorf("flaky = %+v: its Apply failed in this generation", got)
	}
	if got := outcome(r.Report, "slow"); got.Outcome != "pass" {
		t.Errorf("slow = %+v", got)
	}
}

// A generation is judged by its own required set: one that removes a row the committed
// composition marks required passes, and a rollback is judged by the required set of
// the composition it restores.
func TestGenerationIsJudgedByItsOwnRequiredSet(t *testing.T) {
	s := newScript()
	extra := row("extra", "aiops/p", nil)
	extra.Required = true
	w := newScripted(t, s, nil, row("a", "aiops/p", nil), extra)
	w.next = launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{row("a", "aiops/p", nil)}}
	if r := await(t, w.svc); r.Status != def.Passed || !r.Report.Pass {
		t.Fatalf("a generation that removes a row the committed one requires: result = %+v", r)
	}
	// The candidate requires a row that is not ACTIVE: it fails, and the rollback to a
	// composition that does not require it passes.
	bad := row("bad", "aiops/p", nil)
	bad.Required = true
	s.start["bad"] = kernel.Failed
	w.next = launch.Composition{Digest: launch.Digest{4}, Rows: []launch.Row{row("a", "aiops/p", nil), bad}}
	r := await(t, w.svc)
	if r.Status != def.RolledBack || outcome(r.Report, "bad").Outcome != "fail" || r.Rollback == nil || !r.Rollback.Report.Pass {
		t.Fatalf("result = %+v", r)
	}
}

// forgetful is a launcher that cannot say the required set of any composition.
type forgetful struct{ *launchfake.Launcher }

var errForgotten = errors.New("composition was forgotten")

func (forgetful) RequiredOf(launch.Composition) (launch.RequiredSet, error) {
	return launch.RequiredSet{}, errForgotten
}

// A generation whose required set the launcher cannot give fails its verdict, and so
// does the rollback for the same reason.
func TestGenerationWithNoRequiredSetFailsItsVerdict(t *testing.T) {
	root := kerneltest.New(t)
	l, _ := launchfake.New()
	l.Current = launch.Composition{Digest: launch.Digest{1}}
	l.ComposeFn = func(context.Context) (launch.Composition, error) {
		return launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{row("a", "aiops/p", nil)}}, nil
	}
	l.PlanFn = func(_ context.Context, f, to launch.Composition) (launch.Plan, error) { return diff(f, to), nil }
	lp := kernel.Define(kernel.Plugin{ID: "aiops/forgetful", Provides: []kernel.AnyKey{launch.ProfileContextKey, launch.CatalogKey},
		Apply: func(c *kernel.Context, _ any) error {
			if err := launch.ProfileContextKey.Provide(c, forgetful{l}); err != nil {
				return err
			}
			return launch.CatalogKey.Provide(c, l)
		}})
	s := newScript()
	sp := kernel.Define(kernel.Plugin{ID: "aiops/script-loader", Provides: []kernel.AnyKey{loader.Key},
		Apply: func(c *kernel.Context, _ any) error { return loader.Key.Provide(c, s) }})
	kerneltest.Mount(root, lp, sp)
	active(t, root.Plugin(reload.New(), reload.Config{Queue: 4, History: 4}))
	r := await(t, submitter(t, root, "aiops/caller"))
	if got := outcome(r.Report, "required set"); r.Status != def.RollbackFailed || got.Outcome != "fail" || !strings.Contains(got.Detail, errForgotten.Error()) {
		t.Fatalf("result = %+v", r)
	}
}

// A required row of another role is not mounted in this process, so it is not judged:
// boot's required set leaves it out (boot.RequiredFor), and so does the verdict.
func TestRequiredRowOfAnotherRoleIsNotJudged(t *testing.T) {
	s := newScript()
	worker := row("w", "aiops/p", nil)
	worker.Required, worker.Roles = true, []string{"worker"}
	w := newScripted(t, s, map[string]loader.Mount{"w": loader.OtherRole}, worker)
	w.l.RoleName = "api" // the process role, so the launcher's required set leaves w out too
	w.next = launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{worker, row("n", "aiops/p", nil)}}
	r := await(t, w.svc)
	if r.Status != def.Passed || !r.Report.Pass {
		t.Fatalf("result = %+v", r)
	}
	if got := outcome(r.Report, "w"); got.Outcome != "" {
		t.Errorf("w = %+v: a row of another role has no fiber to judge", got)
	}

	// The same row in this role is judged, and fails when it is not mounted.
	off := row("off", "aiops/p", nil)
	off.Required = true
	w.next = launch.Composition{Digest: launch.Digest{3}, Rows: []launch.Row{worker, off}}
	s.onApply = func(context.Context, int, []loader.Op) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if e, ok := s.rows["off"]; ok {
			e.Mount = loader.Disabled
			s.rows["off"] = e
		}
		return nil
	}
	if r := await(t, w.svc); r.Report.Pass || !strings.Contains(outcome(r.Report, "off").Detail, "not mounted") {
		t.Errorf("result = %+v", r)
	}
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

// An apply whose first batch committed and whose second did not (a previous fiber that
// did not dispose) has touched the tree: it is judged and rolled back, not rejected, and
// the composition the loader holds is committed.
func TestApplyThatFailsAfterItsFirstBatchIsJudgedAndRolledBack(t *testing.T) {
	s := newScript()
	w := newScripted(t, s, nil, row("r", "aiops/p", nil), row("x", "aiops/p", "old"))
	w.next = launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{row("x", "aiops/p", "new")}}
	leak := fmt.Errorf(`entry "x": %w`, loader.ErrNotDisposed)
	s.onApply = func(context.Context, int, []loader.Op) error {
		s.set("x", kernel.Pending) // its previous fiber leaked, so it has none
		return leak
	}
	r := await(t, w.svc)
	if r.Status != def.RollbackFailed || r.Generation != 1 || !errors.Is(r.Err, def.ErrVerdict) || !errors.Is(r.Err, loader.ErrNotDisposed) ||
		!errors.Is(r.Err, def.ErrRollback) {
		t.Fatalf("result = %+v: the tree was touched, so the generation is judged", r)
	}
	if got := outcome(r.Report, "x"); got.Outcome != "fail" || got.State != kernel.Pending {
		t.Errorf("x = %+v", got)
	}
	if rb := r.Rollback; rb == nil || len(rb.Applied) == 0 || !errors.Is(rb.Err, loader.ErrNotDisposed) {
		t.Errorf("rollback = %+v", rb)
	}
	// The inverse plan was applied, half way too: the loader's entries are the previous
	// composition's, and that is what is committed.
	if !slices.Equal(w.commits, []launch.Digest{{1}}) {
		t.Errorf("commits = %x", w.commits)
	}
}

// A reload row that starts unloading while its apply runs reads no row afterwards: the
// kernel refuses the tracked goroutines of the settle, and the generation is abandoned
// at settle with what the loader holds committed.
func TestRowUnloadingDuringTheApplyReadsNoRow(t *testing.T) {
	s := newScript()
	w := newScripted(t, s, nil)
	w.next = launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{row("a", "aiops/p", nil)}}
	applying := make(chan struct{})
	s.onApply = func(ctx context.Context, _ int, _ []loader.Op) error {
		close(applying)
		<-ctx.Done() // the row's Std ends once its unload has begun
		return nil
	}
	var read atomic.Bool
	s.after["a"] = func(context.Context) { read.Store(true) }
	id, err := w.svc.Submit(t.Context(), def.Op{Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	<-applying
	if err := w.fiber.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	r, err := w.svc.Await(t.Context(), id)
	if err != nil || r.Status != def.Abandoned || r.Stage != def.StageSettle || !errors.Is(r.Err, kernel.ErrInactive) {
		t.Fatalf("result = %+v, %v", r, err)
	}
	if read.Load() {
		t.Error("a row was read after the reload row began to unload")
	}
	if !slices.Equal(w.commits, []launch.Digest{{2}}) {
		t.Errorf("commits = %x, want the candidate the loader holds", w.commits)
	}
}

// A change of the reload row's config whose candidate holds a reference is rejected before
// anything is applied. Admit decodes a literal config only, and the row's own change is
// applied after the rest of the generation was committed and published, too late to
// reject the generation when the reference does not resolve (08 §1.6, "a reference does
// not resolve": live, "Rejected; the row keeps its instance").
func TestReloadRowConfigWithAReferenceIsRejected(t *testing.T) {
	ref := launch.Ref{Scheme: "env", Name: "AIOPS_REF_Q"}
	for _, tc := range []struct {
		name     string
		from, to any
	}{
		{"gains one", map[string]any{"queue": int64(4)}, map[string]any{"queue": ref}},
		{"keeps one", map[string]any{"queue": ref, "history": int64(8)}, map[string]any{"queue": ref, "history": int64(16)}},
		{"in a list", map[string]any{"queue": int64(4)}, map[string]any{"queue": int64(4), "tags": []any{"a", ref}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newScript()
			w := newScripted(t, s, nil, row("reload", "aiops/reload", tc.from))
			w.next = launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{row("reload", "aiops/reload", tc.to), row("x", "aiops/p", nil)}}
			r := await(t, w.svc)
			if r.Status != def.Rejected || r.Stage != def.StagePlan || r.Err == nil || !strings.Contains(r.Err.Error(), "reference") {
				t.Fatalf("result = %+v", r)
			}
			if s.applies != 0 || len(w.commits) != 0 {
				t.Errorf("applies %d, commits %v: a rejected generation touches nothing", s.applies, w.commits)
			}
		})
	}
}

// A change of the reload row that keeps its config, a reference in it included, has
// nothing to resolve: it is applied after the verdict as any change of the row is.
func TestChangeOfTheReloadRowThatKeepsAReferenceIsApplied(t *testing.T) {
	cfg := map[string]any{"queue": launch.Ref{Scheme: "env", Name: "AIOPS_REF_Q"}}
	s := newScript()
	self := row("reload", "aiops/reload", cfg)
	w := newScripted(t, s, nil, self)
	moved := self
	moved.Roles = []string{"api"}
	w.next = launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{moved, row("x", "aiops/p", nil)}}
	w.l.PlanFn = func(_ context.Context, f, to launch.Composition) (launch.Plan, error) {
		p := diff(f, to)
		for i := range p.Entries {
			if p.Entries[i].ID == "reload" {
				p.Entries[i].Action = launch.ActionUpdate // its roles change
			}
		}
		return p, nil
	}
	r := await(t, w.svc)
	if r.Status != def.Passed || r.Err != nil || !slices.Contains(ids(r.Applied), "restarted reload") {
		t.Fatalf("result = %+v", r)
	}
}

// An operation queued behind a generation that changes the reload row is run by the
// row's next activation: the planned restart of the row abandons nothing.
func TestOperationQueuedBehindARestartOfTheReloadRowRunsAfterIt(t *testing.T) {
	s := newScript()
	w := newScripted(t, s, nil, row("reload", "aiops/reload", map[string]any{"queue": int64(4), "history": int64(8)}))
	w.next = launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{row("reload", "aiops/reload",
		map[string]any{"queue": int64(8), "history": int64(8)})}}
	var queued def.ID
	s.onApply = func(context.Context, int, []loader.Op) error { // the reload row's change, applied last
		var err error
		if queued, err = w.svc.Submit(t.Context(), def.Op{Source: "behind"}); err != nil {
			t.Error(err)
		}
		if err := w.fiber.Update(t.Context(), reload.Config{Queue: 8, History: 8}); err != nil {
			t.Error(err)
		}
		return nil
	}
	if r := await(t, w.svc); r.Status != def.Passed {
		t.Fatalf("result = %+v", r)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	r, err := w.svc.Await(ctx, queued)
	if err != nil || r.Status != def.Passed || r.Generation != 2 || r.Previous.Digest != (launch.Digest{2}) {
		t.Fatalf("queued result = %+v, %v: the next activation runs it", r, err)
	}
}

// After a rollback that failed, reload does not know what the tree runs: the final event
// says so, an operation queued behind is refused, and so is every later Submit, without
// planning from a composition the loader may not hold (ADR-059).
func TestRollbackThatFailedLeavesTheStateUnknown(t *testing.T) {
	s := newScript()
	w := newScripted(t, s, nil, row("a", "aiops/p", nil))
	events := opEvents(t, w.root)
	w.next = launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{row("a", "aiops/p", nil), row("born", "aiops/p", nil)}}
	s.start["born"] = kernel.Failed
	plans := 0
	w.l.PlanFn = func(_ context.Context, f, to launch.Composition) (launch.Plan, error) {
		plans++
		return diff(f, to), nil
	}
	var behind def.ID
	s.refuse = func(ops []loader.Op) error {
		if slices.ContainsFunc(ops, func(op loader.Op) bool { return op.Kind == loader.OpRemove }) {
			return errors.New("refused") // the inverse plan
		}
		var err error
		if behind, err = w.svc.Submit(t.Context(), def.Op{Source: "behind"}); err != nil {
			t.Error(err)
		}
		return nil
	}
	r := await(t, w.svc)
	if r.Status != def.RollbackFailed || !errors.Is(r.Err, def.ErrStateUnknown) {
		t.Fatalf("result = %+v", r)
	}
	if _, got := events.heard(); len(got) != 2 || !got[1].Status.Done() || !errors.Is(got[1].Err, def.ErrStateUnknown) {
		t.Errorf("reload/operation = %+v, want the final event to say that the state is unknown", got)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if rb, err := w.svc.Await(ctx, behind); err != nil || rb.Status != def.Rejected || !errors.Is(rb.Err, def.ErrStateUnknown) || rb.Generation != 0 {
		t.Errorf("queued result = %+v, %v", rb, err)
	}
	if _, err := w.svc.Submit(t.Context(), def.Op{}); !errors.Is(err, def.ErrStateUnknown) {
		t.Errorf("Submit = %v, want ErrStateUnknown", err)
	}
	if plans != 2 || s.applies != 1 {
		t.Errorf("plans %d, applies %d: nothing is planned or applied after the state is unknown", plans, s.applies)
	}
}

// A generation abandoned after it was applied leaves the state unknown, and the reload
// row's next activation keeps it so: it refuses every Submit, and it emits the final
// event that the unloading activation could not.
func TestAbandonAfterTheApplyLeavesTheStateUnknownAcrossARestart(t *testing.T) {
	root := kerneltest.New(t)
	l, lp := launchfake.New()
	bl := &blockingLoader{entered: make(chan struct{}, 4), release: make(chan struct{}), awaiting: make(chan struct{}, 4)}
	close(bl.release)
	lpl := kernel.Define(kernel.Plugin{ID: "aiops/blocking-loader", Provides: []kernel.AnyKey{loader.Key},
		Apply: func(c *kernel.Context, _ any) error { return loader.Key.Provide(c, bl) }})
	l.Current = launch.Composition{Digest: launch.Digest{1}}
	l.ComposeFn = func(context.Context) (launch.Composition, error) {
		return launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{row("a", "aiops/p", nil)}}, nil
	}
	l.PlanFn = func(_ context.Context, f, to launch.Composition) (launch.Plan, error) { return diff(f, to), nil }
	kerneltest.Mount(root, lp, lpl)
	events := opEvents(t, root)
	rf := root.Plugin(reload.New(), reload.Config{Queue: 4, History: 4})
	active(t, rf)
	svc := submitter(t, root, "aiops/caller")
	id, err := svc.Submit(t.Context(), def.Op{})
	if err != nil {
		t.Fatal(err)
	}
	<-bl.awaiting
	if err := rf.Update(t.Context(), reload.Config{Queue: 4, History: 4}); err != nil { // the row restarts
		t.Fatal(err)
	}
	r, err := svc.Await(t.Context(), id)
	if err != nil || r.Status != def.Abandoned || !errors.Is(r.Err, def.ErrStateUnknown) {
		t.Fatalf("result = %+v, %v", r, err)
	}
	active(t, rf)
	if _, err := submitter(t, root, "aiops/caller2").Submit(t.Context(), def.Op{}); !errors.Is(err, def.ErrStateUnknown) {
		t.Errorf("Submit to the next activation = %v, want ErrStateUnknown", err)
	}
	got := events.await(t, 2) // running, then the final event from the next activation
	if f := got[1]; f.ID != id || f.Status != def.Abandoned || !errors.Is(f.Err, def.ErrStateUnknown) {
		t.Errorf("final event = %+v", f)
	}
}

// A generation that changes the reload row's config and restarts a row it is gated on (a
// provider of a key its row injects, or a group above it) ends the activation applying it
// half way, as when its config does not change: it is rejected before anything is applied.
func TestReloadRowThatChangesAndRestartsThroughAnotherRowIsRejected(t *testing.T) {
	gated := func(cfg string) launch.Row {
		r := row("reload", "aiops/reload", cfg)
		r.Inject = []string{"k"}
		return r
	}
	under := func(cfg string) launch.Row {
		r := row("g:reload", "aiops/reload", cfg)
		r.Parent = "g"
		return r
	}
	for _, tc := range []struct {
		name     string
		from, to []launch.Row
		closure  []string
		rejected bool
	}{
		{"provider", []launch.Row{gated("old"), row("p", "aiops/kp", "a")}, []launch.Row{gated("new"), row("p", "aiops/kp", "b")}, []string{"p", "reload"}, true},
		{"group", []launch.Row{row("g", loader.GroupPlugin, nil), under("old")}, []launch.Row{row("g", loader.GroupPlugin, "b"), under("new")}, []string{"g", "g:reload"}, true},
		{"unrelated", []launch.Row{gated("old"), row("q", "aiops/p", "a")}, []launch.Row{gated("new"), row("q", "aiops/p", "b")}, []string{"q", "reload"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newScript()
			w := newScripted(t, s, nil, tc.from...)
			w.l.Modules = []launch.CatalogEntry{{ID: "aiops/kp", Provides: []string{"k"}}, {ID: "aiops/p"}}
			w.next = launch.Composition{Digest: launch.Digest{2}, Rows: tc.to}
			w.l.PlanFn = func(_ context.Context, f, to launch.Composition) (launch.Plan, error) {
				p := diff(f, to)
				p.Closure = tc.closure
				return p, nil
			}
			r := await(t, w.svc)
			rejected := r.Status == def.Rejected && r.Stage == def.StagePlan && r.Err != nil && strings.Contains(r.Err.Error(), "ends the activation that applies it")
			if rejected != tc.rejected || tc.rejected && (s.applies != 0 || len(w.commits) != 0) {
				t.Fatalf("result = %+v, applies %d, commits %v", r, s.applies, w.commits)
			}
		})
	}
}

// The reload row's own change is applied only after a passed verdict. A generation that
// holds one and whose rollback is refused whole leaves the row's previous config in the
// loader and the state of the tree unknown: the change is never applied against it, as
// every later Submit is refused (ADR-059).
func TestReloadRowChangeOfAFailedRollbackIsNeverApplied(t *testing.T) {
	s := newScript()
	w := newScripted(t, s, nil, row("reload", "aiops/reload", "old"))
	w.next = launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{row("reload", "aiops/reload", "new"), row("x", "aiops/p", nil)}}
	s.start["x"] = kernel.Failed
	s.refuse = func(ops []loader.Op) error {
		if slices.ContainsFunc(ops, func(op loader.Op) bool { return op.Kind == loader.OpRemove }) {
			return errors.New("refused")
		}
		return nil
	}
	if r := await(t, w.svc); r.Status != def.RollbackFailed || !errors.Is(r.Err, def.ErrStateUnknown) {
		t.Fatalf("result = %+v", r)
	}
	s.refuse = nil
	s.set("x", kernel.Active)
	if _, err := w.svc.Submit(t.Context(), def.Op{}); !errors.Is(err, def.ErrStateUnknown) {
		t.Errorf("Submit = %v, want ErrStateUnknown", err)
	}
	if e, _ := s.Locate("reload"); e.Spec.Config != "old" || s.applies != 1 {
		t.Errorf("the reload row runs %v after %d applies, want its previous config and no apply after the failure", e.Spec.Config, s.applies)
	}
}

// A dependant that was ACTIVE fails when the generation restarts its provider (a row of
// the closure, not of the plan). The kernel keeps it FAILED until something updates it,
// so the rollback restarts it with the restored provider (loader.OpRestart), and the
// rollback passes: the state of the tree stays known (08-profiles-roadmap-adrs.md §1.6, a
// live Apply error is rolled back). A row that the inverse plan already updates is not
// restarted as well.
func TestRollbackRestartsARowThatFailedInTheGeneration(t *testing.T) {
	s := newScript()
	w := newScripted(t, s, nil, row("p", "aiops/p", "old"), row("c", "aiops/p", nil), row("q", "aiops/p", "old"))
	w.next = launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{row("p", "aiops/p", "new"), row("c", "aiops/p", nil), row("q", "aiops/p", "new")}}
	w.l.PlanFn = func(_ context.Context, f, to launch.Composition) (launch.Plan, error) {
		p := diff(f, to)
		p.Closure = []string{"c", "p", "q"} // c needs what p provides
		return p, nil
	}
	var rollback []loader.Op
	s.onApply = func(_ context.Context, n int, ops []loader.Op) error {
		if n == 1 {
			s.set("c", kernel.Failed) // its activation under the new provider failed
			s.set("q", kernel.Failed)
			return nil
		}
		rollback = ops
		for _, op := range ops {
			s.set(op.Entry.ID, kernel.Active) // restarted with the restored config
		}
		return nil
	}
	r := await(t, w.svc)
	if r.Status != def.RolledBack || r.Rollback == nil || r.Rollback.Err != nil || errors.Is(r.Err, def.ErrStateUnknown) {
		t.Fatalf("result = %+v (rollback %+v)", r, r.Rollback)
	}
	var ids []string
	for _, op := range rollback {
		ids = append(ids, string(op.Kind)+" "+op.Entry.ID)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"restart c", "update p", "update q"}) {
		t.Errorf("the rollback applied %v, want c restarted, p and q updated, once each", ids)
	}
	if _, err := w.svc.Submit(t.Context(), def.Op{Source: "next"}); err != nil {
		t.Errorf("Submit after a rollback that passed = %v", err)
	}
}
