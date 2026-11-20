// SPDX-License-Identifier: Apache-2.0

package fake_test

import (
	"context"
	"errors"
	"testing"

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

func TestPublishReachesListener(t *testing.T) {
	f, p := fake.New()
	var got []composition.Applied
	l := kernel.Define(kernel.Plugin{ID: "aiops/listener",
		Hooks: []kernel.HookUse{reload.Applied.Use(kernel.DefaultStage)},
		Apply: func(c *kernel.Context, _ any) error {
			reload.Applied.On(c, kernel.DefaultStage, func(_ context.Context, a composition.Applied) (any, error) {
				got = append(got, a)
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
}
