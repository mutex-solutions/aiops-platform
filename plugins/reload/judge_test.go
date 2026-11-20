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
	applies int
}

func newScript() *script {
	return &script{rows: map[string]loader.Entry{}, start: map[string]kernel.State{}, after: map[string]func(context.Context){}}
}

func (s *script) Apply(ctx context.Context, ops []loader.Op) ([]loader.Result, error) {
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
	w.fiber = w.root.Plugin(reload.Plugin, reload.Config{Queue: 4, History: 8})
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
	r2 := await(t, submitter(t, w.root, "aiops/submitter2"))
	if r2.Status != def.Passed || r2.Previous.Digest != (launch.Digest{2}) || len(r2.Applied) != 0 {
		t.Errorf("next result = %+v: it is planned from the generation the tree runs", r2)
	}
}

// The reload row's own operation waits for the verdict: a failed generation never applies
// it, and its rollback has nothing of it to undo; a refusal of it is on the result.
func TestReloadRowIsAppliedOnlyAfterAPassedVerdict(t *testing.T) {
	self := row("reload", "aiops/reload", "old")
	for _, tc := range []struct {
		name   string
		start  kernel.State // of the row x that the generation creates
		status def.Status
		order  []string
	}{
		{"failed verdict", kernel.Failed, def.RolledBack, []string{"x", "x"}},
		{"refused", kernel.Active, def.Passed, []string{"x", "reload"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newScript()
			w := newScripted(t, s, nil, self)
			w.next = launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{row("reload", "aiops/reload", "new"), row("x", "aiops/p", nil)}}
			s.start["x"] = tc.start
			boom := errors.New("refused")
			var order []string
			s.onApply = func(_ context.Context, _ int, ops []loader.Op) error {
				for _, op := range ops {
					if order = append(order, op.Entry.ID); op.Entry.ID == "reload" {
						return boom
					}
				}
				return nil
			}
			r := await(t, w.svc)
			if r.Status != tc.status || !slices.Equal(order, tc.order) {
				t.Fatalf("result = %+v, applied %v", r, order)
			}
			if tc.status == def.Passed && (!errors.Is(r.Err, boom) || !strings.Contains(r.Err.Error(), "apply the reload row")) {
				t.Errorf("err = %v", r.Err)
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
	rf := root.Plugin(reload.Plugin, reload.Config{Queue: 4, History: 4})
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

// A required row of another role is not mounted in this process, so it is not judged:
// boot's required set leaves it out (boot.RequiredFor), and so does the verdict.
func TestRequiredRowOfAnotherRoleIsNotJudged(t *testing.T) {
	s := newScript()
	worker := row("w", "aiops/p", nil)
	worker.Required, worker.Roles = true, []string{"worker"}
	w := newScripted(t, s, map[string]loader.Mount{"w": loader.OtherRole}, worker)
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
