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
	reload *reloadfake.Reload
	ticks  int
}

func newWorld(t *testing.T, opts ...kerneltest.Option) *world {
	t.Helper()
	w := &world{t: t, root: kerneltest.New(t, opts...)}
	var lp, cp, rp kernel.Plugin
	w.launch, lp = launchfake.New()
	w.comp, cp = compfake.New()
	w.reload, rp = reloadfake.New()
	w.launch.Needed = launch.RequiredSet{Rows: []string{"db"}, Keys: []string{"sql"}}
	w.launch.Modules = []launch.CatalogEntry{{ID: "aiops/sql", Provides: []string{"sql"}}}
	kerneltest.Mount(w.root, lp, cp, rp)
	return w
}

// settle makes one more fiber, which gives the kernel events that cannot be
// predicted, and waits for every listener to have been called for what happened so far.
func (w *world) settle() {
	w.t.Helper()
	w.ticks++
	kerneltest.Mount(w.root, kernel.Define(kernel.Plugin{ID: fmt.Sprintf("aiops/tick-%d", w.ticks),
		Apply: func(*kernel.Context, any) error { return nil }}))
	if err := w.root.AwaitObservers(w.t.Context()); err != nil && !errors.Is(err, kernel.ErrObserverOverflow) {
		w.t.Fatal(err)
	}
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
	f := w.root.Plugin(readiness.Plugin, nil)
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
	if err := w.root.Plugin(readiness.Plugin, nil).Await(t.Context()); err != nil {
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
	f := w.root.Plugin(readiness.Plugin, nil)
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
	w.settle() // later events no longer reach it
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
	if err := w.root.Plugin(readiness.Plugin, nil).Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	for held := false; !held; { // make events until the dispatcher is held inside one
		select {
		case <-s.entered:
			held = true
		case <-t.Context().Done():
			t.Fatal("the dispatcher was never held")
		default:
			w.ticks++
			f := w.root.Plugin(kernel.Define(kernel.Plugin{ID: fmt.Sprintf("aiops/tick-%d", w.ticks),
				Apply: func(*kernel.Context, any) error { return nil }}), nil)
			if err := f.Await(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
	}

	w.rows(kernel.Active, kernel.Active) // this change has no event of its own
	for range queue * 3 {                // the events behind the held one overflow the queue
		w.ticks++
		f := w.root.Plugin(kernel.Define(kernel.Plugin{ID: fmt.Sprintf("aiops/tick-%d", w.ticks),
			Apply: func(*kernel.Context, any) error { return nil }}), nil)
		if err := f.Await(t.Context()); err != nil {
			t.Fatal(err)
		}
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
			var l []kernel.Plugin
			if listed {
				l = append(l, readiness.Plugin)
			}
			root := bound(t, lp, l...)
			for _, p := range []kernel.Plugin{lp, cp, rp} {
				if err := root.Plugin(p, nil).Await(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			err := root.Plugin(readiness.Plugin, nil).Await(t.Context())
			if listed && err != nil {
				t.Errorf("listed: %v", err)
			}
			if !listed && !errors.Is(err, kernel.ErrSealed) {
				t.Errorf("unlisted: err = %v, want ErrSealed", err)
			}
		})
	}
}
