// SPDX-License-Identifier: Apache-2.0

package fake_test

import (
	"context"
	"errors"
	"testing"
	"time"

	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

func TestSubmitAwaitResult(t *testing.T) {
	f, p := fake.New()
	kerneltest.Mount(kerneltest.New(t), p)
	ctx := t.Context()

	id, err := f.Submit(ctx, reload.Op{Source: "a"})
	if err != nil || id != 1 {
		t.Fatalf("Submit = %v, %v", id, err)
	}
	f.Next = &reload.Result{Status: reload.RolledBack, Err: reload.ErrVerdict}
	id2, _ := f.Submit(ctx, reload.Op{Source: "b"})
	r, err := f.Await(ctx, id2)
	if err != nil || r.Status != reload.RolledBack || r.ID != id2 || r.Op.Source != "b" {
		t.Errorf("Await = %+v, %v", r, err)
	}
	if r, ok := f.Result(id); !ok || r.Status != reload.Passed {
		t.Errorf("Result = %+v, %v", r, ok)
	}
	if _, ok := f.Result(99); ok {
		t.Error("Result of an unknown id")
	}
	if _, err := f.Await(ctx, 99); !errors.Is(err, reload.ErrUnknown) {
		t.Errorf("Await unknown = %v", err)
	}
	if got := f.Submissions(); len(got) != 2 || got[1].Source != "b" {
		t.Errorf("Submissions = %v", got)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := f.Await(cctx, id); !errors.Is(err, context.Canceled) {
		t.Errorf("Await cancelled = %v", err)
	}
	f.SubmitErr = reload.ErrQueueFull
	if _, err := f.Submit(ctx, reload.Op{}); !errors.Is(err, reload.ErrQueueFull) {
		t.Errorf("Submit scripted error = %v", err)
	}
}

// A scripted result that is not final stays as it stands until Finish, and Await
// waits for it or for its context (ADR-062 decision 8).
func TestAwaitWaitsUntilFinish(t *testing.T) {
	f, p := fake.New()
	kerneltest.Mount(kerneltest.New(t), p)
	f.Next = &reload.Result{Status: reload.Queued}
	id, _ := f.Submit(t.Context(), reload.Op{Source: "a"})
	short, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if r, err := f.Await(short, id); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Await of a queued result = %+v, %v; want it to wait past its context", r, err)
	}
	f.Finish(id, reload.Result{ID: 99, Status: reload.Running})
	if r, ok := f.Result(id); !ok || r.Status != reload.Running || r.ID != id || r.Op.Source != "a" {
		t.Fatalf("Result after Finish(Running) = %+v, %v", r, ok)
	}
	got := make(chan reload.Result, 1)
	go func() {
		r, err := f.Await(context.Background(), id)
		if err != nil {
			t.Error(err)
		}
		got <- r
	}()
	f.Finish(id, reload.Result{Status: reload.Passed, Generation: 3})
	if r := <-got; r.Status != reload.Passed || r.Generation != 3 || r.ID != id {
		t.Errorf("Await after Finish = %+v", r)
	}
	f.Finish(42, reload.Result{Status: reload.Passed})
	if _, ok := f.Result(42); ok {
		t.Error("Finish of an id never submitted made it known")
	}
}

func TestPublishReachesListener(t *testing.T) {
	f, p := fake.New()
	var got []composition.Applied
	var ops []reload.Status
	l := kernel.Define(kernel.Plugin{ID: "aiops/listener",
		Hooks: []kernel.HookUse{reload.Applied.Use(kernel.DefaultStage), reload.Operation.Use(kernel.DefaultStage)},
		Apply: func(c *kernel.Context, _ any) error {
			reload.Applied.On(c, kernel.DefaultStage, func(_ context.Context, a composition.Applied) (any, error) {
				got = append(got, a)
				return nil, nil
			})
			reload.Operation.On(c, kernel.DefaultStage, func(_ context.Context, r reload.Result) (any, error) {
				ops = append(ops, r.Status)
				return nil, nil
			})
			return nil
		}})
	kerneltest.Mount(kerneltest.New(t), p, l)
	if err := f.Publish(t.Context(), composition.Applied{Generation: 4}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Generation != 4 {
		t.Errorf("listener heard %v", got)
	}
	for _, s := range []reload.Status{reload.Running, reload.RolledBack} {
		if err := f.Progress(t.Context(), reload.Result{ID: 1, Status: s}); err != nil {
			t.Fatal(err)
		}
	}
	if len(ops) != 2 || ops[0] != reload.Running || ops[1] != reload.RolledBack {
		t.Errorf("listener heard %v", ops)
	}
}
