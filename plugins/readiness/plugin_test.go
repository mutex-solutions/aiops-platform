// SPDX-License-Identifier: Apache-2.0

package readiness_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/plugins/readiness"
	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	compfake "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0/fake"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	launchfake "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0/fake"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	reloadfake "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// world is a kernel with the fakes readiness needs, mounted alone: the required set
// is the row "db" and the key "sql", provided by the plugin aiops/sql.
type world struct {
	t      *testing.T
	root   *kernel.Root
	launch *launchfake.Launcher
	comp   *compfake.Reader
	tree   *tree
	reload *reloadfake.Reload
	ticks  int
}

// tree serves the composition key from the fake. It tells the test that a read
// started, keeping one notice, and a read waits until held is closed.
type tree struct {
	*compfake.Reader
	reads chan struct{}
	held  chan struct{}
}

func (t *tree) Committed(s composition.Scope) (composition.Generation, bool) {
	select {
	case t.reads <- struct{}{}:
	default:
	}
	<-t.held
	return t.Reader.Committed(s)
}

func newWorld(t *testing.T, opts ...kerneltest.Option) *world {
	t.Helper()
	w := &world{t: t, root: kerneltest.New(t, opts...)}
	var lp, rp kernel.Plugin
	w.launch, lp = launchfake.New()
	w.comp, _ = compfake.New()
	w.tree = &tree{Reader: w.comp, reads: make(chan struct{}, 1), held: make(chan struct{})}
	close(w.tree.held)
	cp := kernel.Define(kernel.Plugin{ID: "aiops/tree", Provides: []kernel.AnyKey{composition.Key},
		Apply: func(c *kernel.Context, _ any) error { return composition.Key.Provide(c, w.tree) }})
	w.reload, rp = reloadfake.New()
	w.launch.Needed = launch.RequiredSet{Rows: []string{"db"}, Keys: []string{"sql"}}
	w.launch.Modules = []launch.CatalogEntry{{ID: "aiops/sql", Provides: []string{"sql"}}}
	kerneltest.Mount(w.root, lp, cp, rp)
	return w
}

// tick makes one more fiber, which gives the kernel events that cannot be predicted.
func (w *world) tick() {
	w.t.Helper()
	w.ticks++
	kerneltest.Mount(w.root, kernel.Define(kernel.Plugin{ID: fmt.Sprintf("aiops/tick-%d", w.ticks),
		Apply: func(*kernel.Context, any) error { return nil }}))
}

// read makes events until a read of the tree starts. An event may be lost to an
// overflow, so it does not wait for the read of one event.
func (w *world) read() {
	w.t.Helper()
	for {
		select {
		case <-w.tree.reads:
			return
		case <-w.t.Context().Done():
			w.t.Fatal("the tree was not read")
		default:
			w.tick()
		}
	}
}

// settle waits until the latch has acted on the tree as it is now. It drops the
// notice of a read that may have started earlier, waits for a read, which reads
// the tree as it is now, and then for one more: reads are serialised, so the first
// has acted when the second starts.
func (w *world) settle() {
	w.t.Helper()
	select {
	case <-w.tree.reads:
	default:
	}
	w.read()
	w.read()
}

func (w *world) rows(db, sql kernel.State) {
	w.comp.Commit(composition.Generation{Scope: composition.ScopeProcess, Rows: []composition.Row{
		{ID: "db", Plugin: "aiops/db", State: db}, {ID: "sqlrow", Plugin: "aiops/sql", State: sql},
		{ID: "extra", Plugin: "aiops/extra", State: kernel.Failed}, // not required: never matters
	}})
}

func (w *world) wantReady(want bool) {
	w.t.Helper()
	if got := w.launch.IsReady(); got != want {
		w.t.Fatalf("ready = %v, want %v", got, want)
	}
}

func TestLatchFollowsTheRequiredSetOnStatusEvents(t *testing.T) {
	w := newWorld(t)
	f := w.root.Plugin(readiness.New(), nil)
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	w.settle()
	w.wantReady(false) // no tree committed: nothing required is ACTIVE

	w.rows(kernel.Active, kernel.Active)
	w.settle()
	w.wantReady(true)

	w.rows(kernel.Active, kernel.Failed) // the provider of a required key fails
	w.settle()
	w.wantReady(false)

	w.rows(kernel.Active, kernel.Active)
	w.settle()
	w.wantReady(true)

	w.rows(kernel.Unloading, kernel.Active) // a required row restarts in place
	w.settle()
	w.wantReady(false)

	w.rows(kernel.Active, kernel.Pending) // the provider waits for what it needs
	w.settle()
	w.wantReady(false)
}

func TestLatchFollowsCompositionApplied(t *testing.T) {
	w := newWorld(t)
	if err := w.root.Plugin(readiness.New(), nil).Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	w.settle()
	w.wantReady(false)

	w.rows(kernel.Active, kernel.Active)
	if err := w.reload.Publish(t.Context(), composition.Applied{Generation: 1}); err != nil {
		t.Fatal(err)
	}
	w.wantReady(true) // no kernel event was needed

	w.rows(kernel.Failed, kernel.Active)
	if err := w.reload.Publish(t.Context(), composition.Applied{Generation: 2}); err != nil {
		t.Fatal(err)
	}
	w.wantReady(false)
}

func TestLatchIsDroppedOnDisposal(t *testing.T) {
	w := newWorld(t)
	f := w.root.Plugin(readiness.New(), nil)
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	w.rows(kernel.Active, kernel.Active)
	w.settle()
	w.wantReady(true)

	if err := f.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	w.wantReady(false)
	w.tick() // later events no longer reach it
	if err := w.root.AwaitObservers(t.Context()); err != nil && !errors.Is(err, kernel.ErrObserverOverflow) {
		t.Fatal(err)
	}
	w.wantReady(false)
}

// Once reload says that the state of the tree is unknown, the latch is dropped and stays
// dropped, whatever the required set does, across a restart of the row too (ADR-059).
func TestLatchStaysDroppedWhileTheStateIsUnknown(t *testing.T) {
	w := newWorld(t)
	f := w.root.Plugin(readiness.New(), nil) // its activations share what they know
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	w.rows(kernel.Active, kernel.Active)
	w.settle()
	w.wantReady(true)
	progress := func(r reload.Result) {
		t.Helper()
		if err := w.reload.Progress(t.Context(), r); err != nil {
			t.Fatal(err)
		}
	}
	// An operation that is not final, and a final one that knows the state, change nothing.
	progress(reload.Result{ID: 1, Status: reload.Running, Err: reload.ErrStateUnknown})
	progress(reload.Result{ID: 1, Status: reload.RolledBack, Err: reload.ErrVerdict})
	w.settle()
	w.wantReady(true)

	progress(reload.Result{ID: 2, Status: reload.RollbackFailed, Err: errors.Join(reload.ErrVerdict, reload.ErrStateUnknown)})
	w.wantReady(false)
	if err := w.reload.Publish(t.Context(), composition.Applied{Generation: 3, Report: composition.Report{Pass: true}}); err != nil {
		t.Fatal(err)
	}
	w.wantReady(false)
	w.settle()
	w.wantReady(false)
	if err := f.Update(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	w.settle()
	w.wantReady(false)
}

// stall is a listener of internal/status that holds the observer dispatcher on its
// first event until released, and records the kinds it is given.
type stall struct {
	mu      sync.Mutex
	kinds   []string
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	// released is set before the release: only the notices after it are of the stall
	// that the test made, and not of an overflow that setup may have caused.
	released atomic.Bool
}

func (s *stall) plugin() kernel.Plugin {
	return kernel.Define(kernel.Plugin{ID: "aiops/stall",
		Hooks: []kernel.HookUse{kernel.InternalStatus.Use(kernel.DefaultStage)},
		Apply: func(c *kernel.Context, _ any) error {
			kernel.InternalStatus.On(c, kernel.DefaultStage, func(_ context.Context, e kernel.LedgerEvent) (any, error) {
				if s.released.Load() {
					s.mu.Lock()
					s.kinds = append(s.kinds, e.Kind)
					s.mu.Unlock()
				}
				s.once.Do(func() { close(s.entered); <-s.release })
				return nil, nil
			})
			return nil
		}})
}

func (s *stall) saw(kind string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Contains(s.kinds, kind)
}

func TestOverflowNoticeResyncs(t *testing.T) {
	const queue = 4
	w := newWorld(t, kerneltest.WithConfig(func(c *kernel.Config) { c.ObserverQueue = queue }))
	s := &stall{entered: make(chan struct{}), release: make(chan struct{})}
	kerneltest.Mount(w.root, s.plugin())
	if err := w.root.Plugin(readiness.New(), nil).Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	for held := false; !held; { // make events until the dispatcher is held inside one
		select {
		case <-s.entered:
			held = true
		case <-t.Context().Done():
			t.Fatal("the dispatcher was never held")
		default:
			w.tick()
		}
	}

	w.rows(kernel.Active, kernel.Active) // this change has no event of its own
	for range queue * 3 {                // the events behind the held one overflow the queue
		w.tick()
	}
	s.released.Store(true)
	close(s.release)
	// The kernel subscribes again once the stalled listener returns, and the notice
	// comes with the first event after that: make events until it has been given.
	for !s.saw("overflow") && t.Context().Err() == nil {
		w.settle()
	}
	if !s.saw("overflow") {
		t.Fatalf("the listeners were not given the overflow notice: %v", s.kinds)
	}
	w.wantReady(true)
}

// A read of the tree is two round trips to the owner per row (loader/composition.go).
// The latch must not make them on the observer dispatcher: one per status event, a
// burst of events, such as the restart of a provider with many consumers, overflows
// Config.ObserverQueue and every listener of a kernel hook loses events. The test
// makes too few events to overflow and asserts that they are delivered while a read
// is held; the bound only ends the wait when the read holds the dispatcher.
func TestSlowTreeReadDoesNotHoldTheDispatcher(t *testing.T) {
	w := newWorld(t)
	w.tree.held = make(chan struct{})
	release := sync.OnceFunc(func() { close(w.tree.held) })
	defer release()
	if err := w.root.Plugin(readiness.New(), nil).Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	w.tick()
	select { // the latch is inside a read that does not end
	case <-w.tree.reads:
	case <-t.Context().Done():
		t.Fatal("the tree was not read")
	}

	w.rows(kernel.Active, kernel.Active)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for range 3 {
		w.tick()
		if err := w.root.AwaitObservers(ctx); err != nil {
			t.Fatalf("a held read of the tree holds the dispatcher: %v", err)
		}
	}
	release()
	w.settle()
	w.wantReady(true)
}

// bound is a kernel with the binding maps on: the launch keys are provider-bound,
// the appExit key is consumer-bound and internal/status is sealed to listeners.
func bound(t *testing.T, launcher kernel.Plugin, listeners ...kernel.Plugin) *kernel.Root {
	t.Helper()
	const n = 64
	r, err := kernel.New(kernel.Config{
		LoadDeadline: 10 * time.Second, DrainDeadline: 10 * time.Second, DisposeDeadline: 10 * time.Second,
		ArchiveLimit: n, EventWindow: n, ObserverQueue: n, OwnerInbox: n, TerminalLimit: n,
		Providers: kernel.Bindings{
			launch.ProfileContextKey: {launcher}, launch.CatalogKey: {launcher}, launch.CmdlineKey: {launcher},
			launch.ReadyKey: {launcher}, launch.KillSwitchesKey: {launcher}, launch.AppExitKey: {launcher},
		},
		Consumers: kernel.Bindings{launch.AppExitKey: nil},
		Listeners: map[kernel.AnyHook][]kernel.Plugin{kernel.InternalStatus: listeners},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(context.Background()) })
	return r
}

func TestSealedHookNeedsAListenerEntry(t *testing.T) {
	for _, listed := range []bool{true, false} {
		t.Run(fmt.Sprintf("listed=%v", listed), func(t *testing.T) {
			_, lp := launchfake.New()
			_, cp := compfake.New()
			_, rp := reloadfake.New()
			p := readiness.New()
			var l []kernel.Plugin
			if listed {
				l = append(l, p)
			}
			root := bound(t, lp, l...)
			for _, p := range []kernel.Plugin{lp, cp, rp} {
				if err := root.Plugin(p, nil).Await(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			err := root.Plugin(p, nil).Await(t.Context())
			if listed && err != nil {
				t.Errorf("listed: %v", err)
			}
			if !listed && !errors.Is(err, kernel.ErrSealed) {
				t.Errorf("unlisted: err = %v, want ErrSealed", err)
			}
		})
	}
}
