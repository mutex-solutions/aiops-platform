// SPDX-License-Identifier: Apache-2.0

package reload_test

import (
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
	loaderfake "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0/fake"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// trace records what happened, in order, from any goroutine.
type trace struct {
	mu sync.Mutex
	s  []string
}

func (t *trace) add(s string) {
	t.mu.Lock()
	t.s = append(t.s, s)
	t.mu.Unlock()
}

func (t *trace) get() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.s)
}

func row(id, plugin string, cfg any) launch.Row {
	return launch.Row{ID: id, Plugin: plugin, Config: cfg, Enabled: true}
}

func child(parent, id, plugin string) launch.Row {
	r := row(parent+":"+id, plugin, nil)
	r.Parent = parent
	return r
}

// diff is a Plan by row id: the stand-in of the launcher's pure stage.
func diff(from, to launch.Composition) launch.Plan {
	p := launch.Plan{From: from.Digest, To: to.Digest}
	old := map[string]launch.Row{}
	for _, r := range from.Rows {
		old[r.ID] = r
	}
	seen := map[string]bool{}
	for _, r := range to.Rows {
		seen[r.ID] = true
		switch o, ok := old[r.ID]; {
		case !ok:
			p.Entries = append(p.Entries, launch.PlanEntry{ID: r.ID, Action: launch.ActionCreate})
		case fmt.Sprint(o.Config, o.Enabled) != fmt.Sprint(r.Config, r.Enabled):
			p.Entries = append(p.Entries, launch.PlanEntry{ID: r.ID, Action: launch.ActionUpdate})
		default:
			p.Entries = append(p.Entries, launch.PlanEntry{ID: r.ID, Action: launch.ActionUnchanged})
		}
	}
	for _, r := range from.Rows {
		if !seen[r.ID] {
			p.Entries = append(p.Entries, launch.PlanEntry{ID: r.ID, Action: launch.ActionRemove})
		}
	}
	return p
}

type world struct {
	t     *testing.T
	root  *kernel.Root
	l     *launchfake.Launcher
	ld    *loaderfake.Loader
	tr    *trace
	svc   def.Reload
	fiber *kernel.Fiber
	next  launch.Composition // what Compose returns
}

// newWorld mounts the launcher and the loader fakes and reload with the given bounds.
func newWorld(t *testing.T, queue, history int) *world {
	t.Helper()
	w := base(t)
	w.fiber = w.root.Plugin(reload.Plugin, reload.Config{Queue: queue, History: history})
	active(t, w.fiber)
	w.svc = w.consumer("aiops/submitter")
	return w
}

// base mounts the launcher and the loader fakes, scripted to compose what w.next
// holds and to plan by row id, and no reload.
func base(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, root: kerneltest.New(t), tr: &trace{}}
	var lp, ldp kernel.Plugin
	w.l, lp = launchfake.New()
	w.ld, ldp = loaderfake.New()
	w.l.Modules = []launch.CatalogEntry{{ID: "aiops/sqlp", Provides: []string{"sql"}}}
	w.l.ComposeFn = func(context.Context) (launch.Composition, error) {
		w.tr.add("compose")
		return w.next, nil
	}
	w.l.AdmitFn = func(context.Context, launch.Composition) (launch.Admission, error) {
		w.tr.add("admit")
		return launch.Admission{}, nil
	}
	w.l.PlanFn = func(_ context.Context, from, to launch.Composition) (launch.Plan, error) {
		w.tr.add("plan")
		return diff(from, to), nil
	}
	kerneltest.Mount(w.root, lp, ldp)
	return w
}

// consumer mounts a row that needs reload and returns the service it was given.
func (w *world) consumer(id string) def.Reload {
	w.t.Helper()
	var svc def.Reload
	p := kernel.Define(kernel.Plugin{ID: id, Inject: []kernel.Dep{kernel.Need(def.Key)},
		Apply: func(c *kernel.Context, _ any) error { svc = def.Key.Of(c); return nil }})
	active(w.t, w.root.Plugin(p, nil))
	return svc
}

// listen mounts a row that listens on composition/applied and on loader/entry-init.
func (w *world) listen(onInit func(loader.EntryInitArgs), onApplied func(context.Context, composition.Applied)) {
	w.t.Helper()
	p := kernel.Define(kernel.Plugin{ID: "aiops/listener",
		Hooks: []kernel.HookUse{def.Applied.Use(kernel.DefaultStage), loader.EntryInit.Use(kernel.DefaultStage)},
		Apply: func(c *kernel.Context, _ any) error {
			loader.EntryInit.On(c, kernel.DefaultStage, func(_ context.Context, a loader.EntryInitArgs) (any, error) {
				w.tr.add("apply:" + a.Entry.ID)
				if onInit != nil {
					onInit(a)
				}
				return nil, nil
			})
			def.Applied.On(c, kernel.DefaultStage, func(ctx context.Context, a composition.Applied) (any, error) {
				w.tr.add("applied")
				if onApplied != nil {
					onApplied(ctx, a)
				}
				return nil, nil
			})
			return nil
		}})
	active(w.t, w.root.Plugin(p, nil))
}

// seed makes the running tree: the loader holds the rows and the launcher reports them.
func (w *world) seed(rows ...launch.Row) {
	w.t.Helper()
	for _, r := range rows {
		if err := w.ld.Create(w.t.Context(), loader.EntrySpec{ID: r.ID, Parent: r.Parent, Plugin: r.Plugin, Enabled: true}); err != nil {
			w.t.Fatal(err)
		}
	}
	w.l.Current = launch.Composition{Rows: rows, Digest: launch.Digest{1}}
	w.tr = &trace{} // the seeding made events of its own
}

func (w *world) submit(src string) def.Result {
	w.t.Helper()
	id, err := w.svc.Submit(w.t.Context(), def.Op{Source: src})
	if err != nil {
		w.t.Fatal(err)
	}
	r, err := w.svc.Await(w.t.Context(), id)
	if err != nil {
		w.t.Fatal(err)
	}
	return r
}

func ids(rs []loader.Result) []string {
	var out []string
	for _, r := range rs {
		out = append(out, string(r.Outcome)+" "+r.ID)
	}
	return out
}

func TestGenerationIsComposedAdmittedPlannedAppliedJudgedAndPublished(t *testing.T) {
	w := newWorld(t, 4, 8)
	w.l.Needed = launch.RequiredSet{Rows: []string{"keep"}, Keys: []string{"sql"}}
	from := []launch.Row{row("a", "aiops/p", nil), child("a", "k", "aiops/p"), row("u", "aiops/p", "old"),
		row("keep", "aiops/p", nil), row("db", "aiops/sqlp", nil)}
	w.seed(from...)
	w.next = launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{row("u", "aiops/p", "new"), row("keep", "aiops/p", nil),
		row("db", "aiops/sqlp", nil), row("n", "aiops/p", nil), child("n", "k", "aiops/p")}}
	var heard composition.Applied
	w.listen(nil, func(_ context.Context, a composition.Applied) { heard = a })

	r := w.submit("test")
	if r.Status != def.Passed || r.Err != nil || r.Stage != def.StagePublish {
		t.Fatalf("result = %+v", r)
	}
	if got, want := w.tr.get(), []string{"compose", "admit", "plan", "apply:n", "apply:n:k", "applied"}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
	// Removes with the child first, then the update, then the creates, parents first.
	want := []string{"removed a:k", "removed a", "committed u", "created n", "created n:k"}
	if got := ids(r.Applied); !slices.Equal(got, want) {
		t.Errorf("loader results = %v, want %v", got, want)
	}
	if r.Generation != 1 || r.Digest != (launch.Digest{2}) || r.Previous.Digest != (launch.Digest{1}) || r.Plan.To != (launch.Digest{2}) {
		t.Errorf("generation %d digest %x previous %x plan %+v", r.Generation, r.Digest[:1], r.Previous.Digest[:1], r.Plan)
	}
	if !r.Report.Pass {
		t.Errorf("report = %+v", r.Report)
	}
	if heard.Generation != 1 || heard.Digest != (composition.Digest{2}) || !heard.Report.Pass || len(heard.Report.Rows) == 0 {
		t.Errorf("composition/applied = %+v", heard)
	}
	if got, ok := w.svc.Result(r.ID); !ok || got.Status != def.Passed {
		t.Errorf("Result = %+v, %v", got, ok)
	}

	// The second generation is planned from the first and carries it as previous.
	w.next = launch.Composition{Digest: launch.Digest{3}, Rows: w.next.Rows[:4]}
	r2 := w.submit("test")
	if r2.Generation != 2 || r2.Previous.Digest != (launch.Digest{2}) || r2.Plan.From != (launch.Digest{2}) {
		t.Errorf("second result = %+v", r2)
	}
	if got := ids(r2.Applied); !slices.Equal(got, []string{"removed n:k"}) {
		t.Errorf("second loader results = %v", got)
	}
}

func TestNothingToApplyStillPublishes(t *testing.T) {
	w := newWorld(t, 4, 8)
	w.seed(row("a", "aiops/p", nil))
	w.next = launch.Composition{Digest: launch.Digest{1}, Rows: w.l.Current.Rows}
	w.listen(nil, nil)
	r := w.submit("test")
	if r.Status != def.Passed || len(r.Applied) != 0 || r.Generation != 1 {
		t.Fatalf("result = %+v", r)
	}
	if got := w.tr.get(); got[len(got)-1] != "applied" {
		t.Errorf("order = %v", got)
	}
}

func TestStagesThatRefuseTheGenerationTouchNothing(t *testing.T) {
	boom := errors.New("boom")
	for _, tc := range []struct {
		name  string
		setup func(w *world)
		stage def.Stage
	}{
		{"compose", func(w *world) {
			w.l.ComposeFn = func(context.Context) (launch.Composition, error) { return launch.Composition{}, boom }
		}, def.StageCompose},
		{"admit", func(w *world) {
			w.l.AdmitFn = func(context.Context, launch.Composition) (launch.Admission, error) { return launch.Admission{}, boom }
		}, def.StageAdmit},
		{"plan", func(w *world) {
			w.l.PlanFn = func(context.Context, launch.Composition, launch.Composition) (launch.Plan, error) {
				return launch.Plan{}, boom
			}
		}, def.StagePlan},
		{"apply", func(w *world) {
			w.next = launch.Composition{Rows: []launch.Row{row("a", "aiops/p", nil)}}
			w.l.PlanFn = func(context.Context, launch.Composition, launch.Composition) (launch.Plan, error) {
				return launch.Plan{Entries: []launch.PlanEntry{{ID: "a", Action: launch.ActionCreate}}}, nil // it exists
			}
		}, def.StageApply},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t, 4, 8)
			w.seed(row("a", "aiops/p", nil))
			w.listen(nil, nil)
			tc.setup(w)
			r := w.submit("test")
			if r.Status != def.Rejected || r.Stage != tc.stage || r.Generation != 0 {
				t.Fatalf("result = %+v", r)
			}
			if tc.stage != def.StageApply && !errors.Is(r.Err, boom) || tc.stage == def.StageApply && !errors.Is(r.Err, loader.ErrExists) {
				t.Errorf("err = %v", r.Err)
			}
			if slices.Contains(w.tr.get(), "applied") {
				t.Error("composition/applied was published for a rejected generation")
			}
			if len(w.ld.Entries()) != 1 {
				t.Errorf("the tree changed: %v", w.ld.Entries())
			}
			// The generation counter and the running composition did not move.
			w.next = w.l.Current
			w.l.AdmitFn = nil
			w.l.ComposeFn = func(context.Context) (launch.Composition, error) { return w.l.Current, nil }
			w.l.PlanFn = func(_ context.Context, f, to launch.Composition) (launch.Plan, error) { return diff(f, to), nil }
			if r2 := w.submit("again"); r2.Generation != 1 || r2.Previous.Digest != w.l.Current.Digest {
				t.Errorf("next result = %+v", r2)
			}
		})
	}
}

func TestDeniedOptionalRowIsDetachedAsDisabled(t *testing.T) {
	w := newWorld(t, 4, 8)
	w.seed()
	w.next = launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{row("ok", "aiops/p", nil), row("denied", "aiops/p", nil)}}
	w.l.AdmitFn = func(context.Context, launch.Composition) (launch.Admission, error) {
		return launch.Admission{Denied: []launch.Denial{{Row: "denied", Check: "4", Reason: "stability"}}}, nil
	}
	var created []loader.EntrySpec
	w.listen(func(a loader.EntryInitArgs) { created = append(created, a.Entry) }, nil)
	r := w.submit("test")
	if r.Status != def.Passed || len(r.Admission.Denied) != 1 {
		t.Fatalf("result = %+v", r)
	}
	if len(created) != 2 || !created[0].Enabled || created[1].Enabled {
		t.Errorf("entries created = %+v: the denied row must be created disabled", created)
	}
}

func TestFailedVerdictIsReportedAndTheTreeStaysAsApplied(t *testing.T) {
	w := newWorld(t, 4, 8)
	w.l.Needed = launch.RequiredSet{Rows: []string{"must", "gone", "pend"}, Keys: []string{"sql", "queue"}}
	w.seed(row("keep", "aiops/p", nil), row("idle", "aiops/p", nil), row("must", "aiops/p", nil), row("db", "aiops/sqlp", nil),
		row("pend", "aiops/p", nil))
	w.ld.SetState("idle", kernel.Pending) // inactive before and untouched: a warning
	w.ld.SetState("pend", kernel.Pending) // the same, but required: a failure
	w.next = launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{
		row("keep", "aiops/p", nil), row("idle", "aiops/p", nil), row("must", "aiops/p", "changed"), row("db", "aiops/sqlp", nil),
		row("pend", "aiops/p", nil), row("new", "aiops/p", nil)}}
	w.l.PlanFn = func(_ context.Context, f, to launch.Composition) (launch.Plan, error) {
		p := diff(f, to)
		p.Closure = []string{"keep"} // keep restarts with its provider
		return p, nil
	}
	w.listen(func(a loader.EntryInitArgs) {
		w.ld.SetState("new", kernel.Failed) // created and fails
		w.ld.SetState("keep", kernel.Failed)
		w.ld.SetState("db", kernel.Failed) // the provider of sql, which is required
	}, nil)
	var heard composition.Applied
	w.listen2(&heard)

	r := w.submit("test")
	if r.Status != def.Failed || !errors.Is(r.Err, def.ErrVerdict) || r.Report.Pass {
		t.Fatalf("result = %+v", r)
	}
	got := map[string]composition.RowReport{}
	for _, rr := range r.Report.Rows {
		got[rr.ID] = rr
	}
	for id, want := range map[string]string{"new": "fail", "keep": "fail", "db": "fail", "idle": "warning", "must": "pass", "pend": "fail",
		"key:queue": "fail", "key:sql": "fail", "row:gone": "fail"} {
		if got[id].Outcome != want {
			t.Errorf("%s: outcome %q, want %q (%+v)", id, got[id].Outcome, want, r.Report)
		}
	}
	if !strings.Contains(got["new"].Detail, "created, changed or enabled") || !strings.Contains(got["db"].Detail, "was ACTIVE") {
		t.Errorf("details: new %q, db %q", got["new"].Detail, got["db"].Detail)
	}
	if r.Previous.Digest != (launch.Digest{1}) || len(r.Previous.Rows) != 5 {
		t.Errorf("previous = %+v: a later step must be able to restore it", r.Previous)
	}
	if heard.Generation != 1 || heard.Report.Pass {
		t.Errorf("composition/applied = %+v: a failed verdict is published as failed", heard)
	}
	if _, ok := w.ld.Locate("new"); !ok {
		t.Error("the failed generation was undone: S1 only reports it")
	}
}

// listen2 records the last composition/applied in out.
func (w *world) listen2(out *composition.Applied) {
	w.t.Helper()
	p := kernel.Define(kernel.Plugin{ID: "aiops/listener2", Hooks: []kernel.HookUse{def.Applied.Use(kernel.DefaultStage)},
		Apply: func(c *kernel.Context, _ any) error {
			def.Applied.On(c, kernel.DefaultStage, func(_ context.Context, a composition.Applied) (any, error) {
				*out = a
				return nil, nil
			})
			return nil
		}})
	active(w.t, w.root.Plugin(p, nil))
}

func TestRequiredRowThatIsNotMountedFailsTheVerdict(t *testing.T) {
	w := newWorld(t, 4, 8)
	w.l.Needed = launch.RequiredSet{Rows: []string{"off"}}
	w.seed()
	off := row("off", "aiops/p", nil)
	off.Enabled = false
	extra := row("opt", "aiops/p", nil)
	w.next = launch.Composition{Rows: []launch.Row{off, extra}}
	w.l.PlanFn = func(context.Context, launch.Composition, launch.Composition) (launch.Plan, error) {
		return launch.Plan{}, nil // the fake loader mounts what it is given, so give it nothing
	}
	r := w.submit("test")
	if r.Status != def.Failed {
		t.Fatalf("result = %+v", r)
	}
	if rr := r.Report.Rows; len(rr) != 1 || rr[0].ID != "off" || !strings.Contains(rr[0].Detail, "not mounted") {
		t.Errorf("report = %+v: an optional row with no entry is not judged, a required one fails", rr)
	}
}

func TestRequiredFlagOfARowCountsAsRequired(t *testing.T) {
	w := newWorld(t, 4, 8)
	w.seed()
	must := row("must", "aiops/p", nil)
	must.Required = true
	w.next = launch.Composition{Rows: []launch.Row{must}}
	w.listen(func(loader.EntryInitArgs) { w.ld.SetState("must", kernel.Failed) }, nil)
	r := w.submit("test")
	if r.Status != def.Failed {
		t.Fatalf("result = %+v", r)
	}
}

// gate blocks a stage of the pipeline until released and says when it was entered.
type gate struct {
	entered chan struct{}
	release chan struct{}
}

func newGate() *gate { return &gate{entered: make(chan struct{}, 16), release: make(chan struct{})} }

func (g *gate) wait(ctx context.Context) error {
	g.entered <- struct{}{}
	select {
	case <-g.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestFullQueueRefusesSubmit(t *testing.T) {
	w := newWorld(t, 2, 8)
	w.seed()
	g := newGate()
	w.l.ComposeFn = func(ctx context.Context) (launch.Composition, error) { return launch.Composition{}, g.wait(ctx) }
	first, err := w.svc.Submit(t.Context(), def.Op{Source: "1"})
	if err != nil {
		t.Fatal(err)
	}
	<-g.entered // the worker holds the first: it is Running, so the queue is empty
	if r, _ := w.svc.Result(first); r.Status != def.Running {
		t.Errorf("status = %s", r.Status)
	}
	var queued []def.ID
	for range 2 {
		id, err := w.svc.Submit(t.Context(), def.Op{})
		if err != nil {
			t.Fatal(err)
		}
		queued = append(queued, id)
	}
	if r, _ := w.svc.Result(queued[0]); r.Status != def.Queued {
		t.Errorf("status = %s", r.Status)
	}
	if _, err := w.svc.Submit(t.Context(), def.Op{}); !errors.Is(err, def.ErrQueueFull) {
		t.Fatalf("Submit on a full queue = %v, want ErrQueueFull", err)
	}
	close(g.release)
	w.l.ComposeFn = func(context.Context) (launch.Composition, error) { return launch.Composition{}, nil }
	for _, id := range append([]def.ID{first}, queued...) {
		if _, err := w.svc.Await(t.Context(), id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.svc.Submit(t.Context(), def.Op{}); err != nil {
		t.Errorf("Submit after the queue drained = %v", err)
	}
}

func TestDisposalAbandonsTheRunningAndTheQueuedOperations(t *testing.T) {
	w := newWorld(t, 4, 8)
	w.seed()
	g := newGate()
	w.l.ComposeFn = func(ctx context.Context) (launch.Composition, error) { return launch.Composition{}, g.wait(ctx) }
	ids := make([]def.ID, 3)
	for i := range ids {
		id, err := w.svc.Submit(t.Context(), def.Op{})
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = id
	}
	<-g.entered
	if err := w.fiber.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := w.fiber.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		r, err := w.svc.Await(t.Context(), id)
		if err != nil || r.Status != def.Abandoned || !errors.Is(r.Err, def.ErrAbandoned) {
			t.Errorf("result of %d = %+v, %v", id, r, err)
		}
	}
	if r, _ := w.svc.Result(ids[0]); r.Stage != def.StageCompose {
		t.Errorf("the running operation was abandoned at stage %q, want compose", r.Stage)
	}
	if _, err := w.svc.Submit(t.Context(), def.Op{}); !errors.Is(err, def.ErrClosed) {
		t.Errorf("Submit after disposal = %v, want ErrClosed", err)
	}
	if r, ok := w.svc.Result(ids[2]); !ok || r.Status != def.Abandoned {
		t.Errorf("Result after disposal = %+v, %v", r, ok)
	}
}

// blockingLoader is a loader whose Apply waits for a release, as an apply does while
// it waits for a fiber to drain its leases.
type blockingLoader struct {
	entered  chan struct{}
	release  chan struct{}
	awaiting chan struct{} // when set, Await signals it and waits for its context to end
}

func (b *blockingLoader) Apply(ctx context.Context, _ []loader.Op) ([]loader.Result, error) {
	b.entered <- struct{}{}
	select {
	case <-b.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (b *blockingLoader) Create(context.Context, loader.EntrySpec) error { return nil }
func (b *blockingLoader) Update(context.Context, loader.EntrySpec) error { return nil }
func (b *blockingLoader) Remove(context.Context, string) error           { return nil }
func (b *blockingLoader) Await(ctx context.Context, _ string) (kernel.State, error) {
	if b.awaiting == nil {
		return kernel.Active, nil
	}
	b.awaiting <- struct{}{}
	<-ctx.Done()
	return "", ctx.Err()
}
func (b *blockingLoader) Locate(string) (loader.Entry, bool) { return loader.Entry{}, false }

func TestSubmitFromALeasedCallDoesNotWait(t *testing.T) {
	root := kerneltest.New(t)
	l, lp := launchfake.New()
	bl := &blockingLoader{entered: make(chan struct{}, 4), release: make(chan struct{})}
	lpl := kernel.Define(kernel.Plugin{ID: "aiops/blocking-loader", Provides: []kernel.AnyKey{loader.Key},
		Apply: func(c *kernel.Context, _ any) error { return loader.Key.Provide(c, bl) }})
	l.ComposeFn = func(context.Context) (launch.Composition, error) {
		return launch.Composition{Rows: []launch.Row{row("a", "aiops/p", nil)}}, nil
	}
	l.PlanFn = func(_ context.Context, f, to launch.Composition) (launch.Plan, error) { return diff(f, to), nil }
	kerneltest.Mount(root, lp, lpl)
	active(t, root.Plugin(reload.Plugin, reload.Config{Queue: 4, History: 4}))

	var svc def.Reload
	var cc *kernel.Context
	caller := kernel.Define(kernel.Plugin{ID: "aiops/caller", Inject: []kernel.Dep{kernel.Need(def.Key)},
		Apply: func(c *kernel.Context, _ any) error { svc, cc = def.Key.Of(c), c; return nil }})
	cf := root.Plugin(caller, nil)
	active(t, cf)

	leave, ok := cc.Enter() // a leased call: the apply of a restart would wait for it
	if !ok {
		t.Fatal("no lease")
	}
	first, err := svc.Submit(t.Context(), def.Op{Source: "in the lease"})
	if err != nil {
		t.Fatal(err)
	}
	<-bl.entered // the worker is inside Apply, waiting
	second, err := svc.Submit(t.Context(), def.Op{Source: "still in the lease"})
	if err != nil {
		t.Fatalf("Submit behind a running apply = %v", err)
	}
	leave()
	close(bl.release)
	for _, id := range []def.ID{first, second} {
		if r, err := svc.Await(t.Context(), id); err != nil || r.Status != def.Passed {
			t.Errorf("result of %d = %+v, %v", id, r, err)
		}
	}
}

func TestSubmitFromTheWorkersOwnCallbackIsRefusedAsNested(t *testing.T) {
	w := newWorld(t, 4, 8)
	w.seed()
	w.next = launch.Composition{}
	var nestedErr error
	var svc def.Reload
	w.listen(nil, func(ctx context.Context, _ composition.Applied) {
		_, nestedErr = svc.Submit(ctx, def.Op{Source: "nested"})
	})
	svc = w.svc
	if r := w.submit("outer"); r.Status != def.Passed {
		t.Fatalf("result = %+v", r)
	}
	if !errors.Is(nestedErr, def.ErrNested) {
		t.Errorf("nested Submit = %v, want ErrNested", nestedErr)
	}
}

func TestDisposalWhilePublishingIsReportedOnTheResult(t *testing.T) {
	w := newWorld(t, 4, 8)
	w.seed()
	w.next = launch.Composition{}
	w.listen(nil, func(ctx context.Context, _ composition.Applied) {
		_ = w.fiber.Dispose(ctx)
		<-ctx.Done() // the dispatch ends with the context: Emit reports it
	})
	r := w.submit("test")
	if r.Status != def.Passed || r.Err == nil || !strings.Contains(r.Err.Error(), "publish composition/applied") {
		t.Errorf("result = %+v", r)
	}
}

func TestDisposalWhileSettlingAbandonsTheOperation(t *testing.T) {
	root := kerneltest.New(t)
	l, lp := launchfake.New()
	bl := &blockingLoader{entered: make(chan struct{}, 4), release: make(chan struct{}), awaiting: make(chan struct{}, 4)}
	close(bl.release)
	lpl := kernel.Define(kernel.Plugin{ID: "aiops/blocking-loader", Provides: []kernel.AnyKey{loader.Key},
		Apply: func(c *kernel.Context, _ any) error { return loader.Key.Provide(c, bl) }})
	l.ComposeFn = func(context.Context) (launch.Composition, error) {
		return launch.Composition{Rows: []launch.Row{row("a", "aiops/p", nil)}}, nil
	}
	l.PlanFn = func(_ context.Context, f, to launch.Composition) (launch.Plan, error) { return diff(f, to), nil }
	kerneltest.Mount(root, lp, lpl)
	rf := root.Plugin(reload.Plugin, reload.Config{Queue: 4, History: 4})
	active(t, rf)
	var svc def.Reload
	caller := kernel.Define(kernel.Plugin{ID: "aiops/caller", Inject: []kernel.Dep{kernel.Need(def.Key)},
		Apply: func(c *kernel.Context, _ any) error { svc = def.Key.Of(c); return nil }})
	active(t, root.Plugin(caller, nil))
	id, err := svc.Submit(t.Context(), def.Op{})
	if err != nil {
		t.Fatal(err)
	}
	<-bl.awaiting // the worker waits for the row to settle
	if err := rf.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	r, err := svc.Await(t.Context(), id)
	if err != nil || r.Status != def.Abandoned || r.Stage != def.StageSettle || !errors.Is(r.Err, def.ErrAbandoned) {
		t.Errorf("result = %+v, %v", r, err)
	}
	if r.Generation != 1 {
		t.Errorf("generation = %d: the apply had happened", r.Generation)
	}
}

func TestHistoryIsBounded(t *testing.T) {
	w := newWorld(t, 4, 2)
	w.seed()
	w.next = launch.Composition{}
	var results []def.Result
	for i := range 3 {
		results = append(results, w.submit(fmt.Sprint(i)))
	}
	if _, ok := w.svc.Result(results[0].ID); ok {
		t.Error("the oldest result was kept beyond the bound")
	}
	if _, err := w.svc.Await(t.Context(), results[0].ID); !errors.Is(err, def.ErrUnknown) {
		t.Errorf("Await of an evicted id = %v, want ErrUnknown", err)
	}
	for _, r := range results[1:] {
		if got, ok := w.svc.Result(r.ID); !ok || got.Op.Source != r.Op.Source {
			t.Errorf("result %d = %+v, %v", r.ID, got, ok)
		}
	}
	if _, err := w.svc.Await(t.Context(), 999); !errors.Is(err, def.ErrUnknown) {
		t.Errorf("Await of an unknown id = %v", err)
	}
	if _, ok := w.svc.Result(999); ok {
		t.Error("Result of an unknown id")
	}
}

func TestAwaitReturnsWhenItsContextEnds(t *testing.T) {
	w := newWorld(t, 4, 8)
	w.seed()
	g := newGate()
	w.l.ComposeFn = func(ctx context.Context) (launch.Composition, error) { return launch.Composition{}, g.wait(ctx) }
	id, _ := w.svc.Submit(t.Context(), def.Op{})
	<-g.entered
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := w.svc.Await(ctx, id); !errors.Is(err, context.Canceled) {
		t.Errorf("Await = %v, want context.Canceled", err)
	}
	close(g.release)
	if _, err := w.svc.Await(t.Context(), id); err != nil {
		t.Fatal(err)
	}
}

func TestDecode(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  any
		ok   bool
	}{
		{"struct", reload.Config{Queue: 1, History: 1}, true},
		{"map int64", map[string]any{"queue": int64(2), "history": int64(3)}, true},
		{"map int", map[string]any{"queue": 2, "history": 3}, true},
		{"map float", map[string]any{"queue": 2.0, "history": 3.0}, true},
		{"nil", nil, false},
		{"wrong type", "x", false},
		{"zero queue", reload.Config{Queue: 0, History: 1}, false},
		{"zero history", map[string]any{"queue": 1}, false},
		{"fractional", map[string]any{"queue": 1.5, "history": 1}, false},
		{"huge", map[string]any{"queue": 1e12, "history": 1}, false},
		{"text", map[string]any{"queue": "1", "history": 1}, false},
		{"unknown field", map[string]any{"queue": 1, "history": 1, "timeout": 1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := base(t).root.Plugin(reload.Plugin, tc.raw).Await(t.Context())
			if (err == nil) != tc.ok {
				t.Errorf("err = %v, want ok = %v", err, tc.ok)
			}
		})
	}
}

func TestBindingMaps(t *testing.T) {
	_, lp := launchfake.New()
	_, ldp := loaderfake.New()
	submitter := kernel.Define(kernel.Plugin{ID: "aiops/submitter", Inject: []kernel.Dep{kernel.Need(def.Key)},
		Apply: func(c *kernel.Context, _ any) error { _ = def.Key.Of(c); return nil }})
	stranger := kernel.Define(kernel.Plugin{ID: "aiops/stranger", Inject: []kernel.Dep{kernel.Need(def.Key)},
		Apply: func(c *kernel.Context, _ any) error { _ = def.Key.Of(c); return nil }})
	rl := kernel.Define(reload.Plugin)
	providers := kernel.Bindings{launch.ProfileContextKey: {lp}, launch.CatalogKey: {lp}, launch.CmdlineKey: {lp},
		launch.ReadyKey: {lp}, launch.KillSwitchesKey: {lp}, launch.AppExitKey: {lp}}
	consumers := kernel.Bindings{loader.Key: {rl}, def.Key: {submitter}, launch.AppExitKey: nil}
	root := bound(t, providers, consumers)
	active(t, root.Plugin(lp, nil))
	active(t, root.Plugin(ldp, nil))
	active(t, root.Plugin(rl, reload.Config{Queue: 1, History: 1}))
	active(t, root.Plugin(submitter, nil))
	if err := failure(t, root.Plugin(stranger, nil)); !errors.Is(err, kernel.ErrBinding) {
		t.Errorf("an unlisted consumer of reload: err = %v, want ErrBinding", err)
	}

	// The loader key is consumer-bound too: a reload row that is not listed cannot
	// reach it.
	other := bound(t, providers, kernel.Bindings{loader.Key: nil, def.Key: nil, launch.AppExitKey: nil})
	active(t, other.Plugin(lp, nil))
	active(t, other.Plugin(ldp, nil))
	if err := failure(t, other.Plugin(rl, reload.Config{Queue: 1, History: 1})); !errors.Is(err, kernel.ErrBinding) {
		t.Errorf("reload not listed for loader: err = %v, want ErrBinding", err)
	}
}

// bound builds a kernel with both binding maps on. sdk/def/internal/deftest does this
// for the definitions, but Go keeps that package inside sdk/def.
func bound(t *testing.T, providers, consumers kernel.Bindings) *kernel.Root {
	t.Helper()
	const n = 64
	r, err := kernel.New(kernel.Config{
		LoadDeadline: 10 * time.Second, DrainDeadline: 10 * time.Second, DisposeDeadline: 10 * time.Second,
		ArchiveLimit: n, EventWindow: n, ObserverQueue: n, OwnerInbox: n, TerminalLimit: n,
		Providers: providers, Consumers: consumers,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(context.Background()) })
	return r
}

func active(t *testing.T, f *kernel.Fiber) {
	t.Helper()
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	if st, _ := f.State(t.Context()); st != kernel.Active {
		t.Fatalf("state = %s, want ACTIVE", st)
	}
}

func failure(t *testing.T, f *kernel.Fiber) error {
	t.Helper()
	err := f.Await(t.Context())
	if st, _ := f.State(t.Context()); st != kernel.Failed || err == nil {
		t.Fatalf("state = %s, err = %v, want FAILED with an error", st, err)
	}
	return err
}
