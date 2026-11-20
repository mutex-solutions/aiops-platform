// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"sync"
	"testing"
)

// Tests of the config lock of a fiber (cfgCell.cmu).

// A config change of a fiber waits for a batch that holds the fiber's config until
// the core has admitted or refused it, so the restore of a refused batch cannot
// overwrite it. The batch is held inside its internal/update hook, where it holds
// the lock; the change has passed its commit by then and is released after.
func TestAConfigChangeIsNotLostToTheRestoreOfARefusedBatch(t *testing.T) {
	for name, change := range map[string]func(context.Context, *Fiber) error{
		"update": func(ctx context.Context, f *Fiber) error { return f.Update(ctx, "mine") },
		"commit": func(ctx context.Context, f *Fiber) error { return f.Commit(ctx, "mine") },
	} {
		t.Run(name, func(t *testing.T) {
			r := newKernel(t)
			ctx := frontCtx(t)
			entered, release, committed := make(chan struct{}), make(chan struct{}), make(chan struct{})
			InternalUpdate.On(r.Context(), DefaultStage, func(_ context.Context, a any, next Next[any]) (any, error) {
				if a == "batch" {
					close(entered)
					<-release
				}
				return next()
			})
			InternalCommit.On(r.Context(), DefaultStage, func(_ context.Context, a any, next Next[bool]) (bool, error) {
				if a == "mine" {
					close(committed)
				}
				return next()
			})
			rec := &cfgRecorder{}
			f := r.Plugin(rec.plugin(nil), "v0")
			want(t, f, Active)

			var wg sync.WaitGroup
			var batchErr, changeErr error
			wg.Go(func() { _, batchErr = r.Batch(ctx, DisposeOp(f), UpdateOpWith(f, "batch")) })
			<-entered
			if f.cell.cmu.TryLock() {
				t.Fatal("a batch in flight does not hold the config lock of its fiber")
			}
			wg.Go(func() { changeErr = change(ctx, f) })
			<-committed
			close(release)
			wg.Wait()
			if batchErr == nil {
				t.Fatal("the core accepted a batch that updates a fiber it disposes")
			}
			if changeErr != nil {
				t.Fatal(changeErr)
			}
			if got := f.cell.load(); got != "mine" {
				t.Fatalf("retained %v: the change was lost", got)
			}
			if !f.cell.cmu.TryLock() {
				t.Fatal("the config lock was not released")
			}
			f.cell.cmu.Unlock()
		})
	}
}

// Two batches that name the same fibers in opposite order take their locks in one
// order, so they cannot deadlock; a fiber named twice is locked once.
func TestBatchesOfTheSameFibersDoNotDeadlock(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	a := r.Plugin(plug("s1/a", nil, nil, nil), "a")
	b := r.Plugin(plug("s1/b", nil, nil, nil), "b")
	want(t, a, Active)
	want(t, b, Active)
	var wg sync.WaitGroup
	for _, ops := range [][]BatchOp{
		{CommitOp(a, 1), CommitOp(b, 1), CommitOp(a, 2)},
		{CommitOp(b, 3), CommitOp(a, 3)},
	} {
		wg.Go(func() {
			for range 200 {
				if _, err := r.Batch(ctx, ops...); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
}
