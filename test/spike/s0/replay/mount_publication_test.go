// SPDX-License-Identifier: Apache-2.0

package replay

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/test/differential"
	"github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
)

// Built here, deliberately not in the frozen corpus: the parent's body mounts a
// child and the root script awaits it at once, with no mark ordering the two.
const immediateChildAwait = `{
  "schema": "s0-script/v1", "id": "D90",
  "title": "root awaits a child that its parent body is still mounting",
  "covers": ["child-teardown"],
  "plugins": [
    {"id": "parent", "apply": [{"op": "child", "fiber": "child", "plugin": "child", "config": {}}]},
    {"id": "child", "apply": []}
  ],
  "steps": [
    {"op": "mount", "fiber": "parent", "plugin": "parent", "config": {}},
    {"op": "awaitFiber", "fiber": "child"},
    {"op": "awaitFiber", "fiber": "parent"},
    {"op": "settle", "label": "after"}
  ],
  "expect": []
}`

func eachProcs(t *testing.T, repeat int, test func(t *testing.T)) {
	t.Helper()
	for _, procs := range []int{1, 2, 0} {
		name := "default"
		if procs != 0 {
			name = string(rune('0' + procs))
		}
		t.Run("GOMAXPROCS="+name, func(t *testing.T) {
			if procs != 0 {
				defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(procs))
			}
			for i := 0; i < repeat; i++ {
				test(t)
			}
		})
	}
}

func TestChildAwaitedBeforeItsMountReturns(t *testing.T) {
	eachProcs(t, 40, func(t *testing.T) {
		eachRuntime(t, func(t *testing.T, ctx context.Context, rt concurrency.Runtime) {
			result, err := RunComposedJSON(ctx, rt, []byte(immediateChildAwait))
			if err != nil {
				t.Fatal(err)
			}
			states := result.Trace.Checkpoints[0].States
			if len(states) != 2 || states[0].Fiber != "parent" || states[1].Fiber != "child" ||
				states[1].State != string(concurrency.Active) {
				t.Fatalf("unexpected checkpoint: %+v", states)
			}
		})
	})
}

// failingMount holds Runtime.Mount open until released, then refuses it.
type failingMount struct {
	concurrency.Runtime
	entered chan struct{}
	release chan struct{}
	err     error
}

func (m failingMount) Mount(context.Context, concurrency.Spec) (uint64, error) {
	close(m.entered)
	<-m.release
	return 0, m.err
}

func TestChildMountFailureReachesAwaitingStep(t *testing.T) {
	for _, op := range []string{"awaitFiber", "dispose", "update", "rawConfig", "inspect"} {
		t.Run(op, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			boom := errors.New("child mount refused")
			m := failingMount{Runtime: concurrency.NewRecordingReconciler(), entered: make(chan struct{}), release: make(chan struct{}), err: boom}
			r := &interpreter{ctx: ctx, runtime: m, changed: make(chan struct{}), fibers: map[string]*fiber{},
				plugins: map[string]differential.PluginSpec{"p": {ID: "p"}}}
			mounted := make(chan error, 1)
			go func() {
				mounted <- r.mount(differential.Action{Op: "mount", Fiber: "child", Plugin: "p"}, nil, environment{})
			}()
			<-m.entered // The fiber is registered and its mount is in flight.
			awaited := make(chan error, 1)
			go func() { awaited <- r.execute(differential.Action{Op: op, Fiber: "child"}, environment{std: ctx}) }()
			close(m.release)
			if err := <-mounted; err != boom {
				t.Fatalf("mount returned %v", err)
			}
			if err := <-awaited; err != boom {
				t.Fatalf("%s reported %v, want the mount failure", op, err)
			}
		})
	}
}

// A body that has not finished may still mount the name; the root must wait for
// the registration, and stop waiting once no body can mount it any more.
func TestLookupWaitsForBodyMount(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	newInterpreter := func() (*interpreter, *fiber) {
		parent := newTestFiber("parent")
		parent.plugin = "parent"
		r := &interpreter{ctx: ctx, changed: make(chan struct{}), fibers: map[string]*fiber{"parent": parent},
			order: []*fiber{parent}, declared: map[string]map[string]bool{"child": {"parent": true}}}
		return r, parent
	}
	type found struct {
		f   *fiber
		err error
	}
	t.Run("registered", func(t *testing.T) {
		r, _ := newInterpreter()
		got := make(chan found, 1)
		go func() { f, err := r.lookup("child"); got <- found{f, err} }()
		child := newTestFiber("child")
		r.mu.Lock()
		r.fibers["child"] = child
		r.broadcastLocked()
		r.mu.Unlock()
		if o := <-got; o.err != nil || o.f != child {
			t.Fatalf("lookup = %v, %v", o.f, o.err)
		}
	})
	t.Run("body finished without mounting", func(t *testing.T) {
		r, parent := newInterpreter()
		got := make(chan found, 1)
		go func() { f, err := r.lookup("child"); got <- found{f, err} }()
		r.mu.Lock()
		parent.bodyDone = true
		r.broadcastLocked()
		r.mu.Unlock()
		if o := <-got; o.err != nil || o.f != nil {
			t.Fatalf("lookup = %v, %v, want unknown", o.f, o.err)
		}
	})
	t.Run("never declared is unknown at once", func(t *testing.T) {
		r, _ := newInterpreter()
		if f, err := r.lookup("other"); f != nil || err != nil {
			t.Fatalf("lookup = %v, %v", f, err)
		}
	})
}

func newTestFiber(name string) *fiber { return &fiber{name: name, ready: make(chan struct{})} }

func TestPublishWakesWaitersWithIDOrFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := &interpreter{ctx: ctx, changed: make(chan struct{})}
	boom := errors.New("mount refused")
	for _, tc := range []struct {
		name string
		id   uint64
		err  error
	}{{"ok", 7, nil}, {"failed", 0, boom}, {"id-and-error", 9, boom}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newTestFiber(tc.name)
			type outcome struct {
				id  uint64
				err error
			}
			got := make(chan outcome)
			for i := 0; i < 4; i++ {
				go func() { id, err := r.fiberID(f); got <- outcome{id, err} }()
			}
			r.publish(f, tc.id, tc.err)
			wantErr := tc.err
			if tc.id != 0 {
				wantErr = nil // A native id exists: the fiber's own state reports failure.
			}
			for i := 0; i < 4; i++ {
				if o := <-got; o.id != tc.id || !errors.Is(o.err, wantErr) && o.err != wantErr {
					t.Fatalf("waiter saw (%d, %v), want (%d, %v)", o.id, o.err, tc.id, wantErr)
				}
			}
		})
	}
}

func TestFiberIDWaitIsBoundedByContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &interpreter{ctx: ctx}
	done := make(chan error)
	go func() { _, err := r.fiberID(newTestFiber("never")); done <- err }()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("wanted context.Canceled, got %v", err)
	}
}
