// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"testing"
)

// Tests of Fiber.Commit and CommitOp: a config retained without a restart
// (ADR-059 decision 2).

func TestCommitRetainsTheConfigWithoutARestart(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	var seen, updates []any
	InternalCommit.On(r.Context(), DefaultStage, func(_ context.Context, raw any, next Next[bool]) (bool, error) {
		seen = append(seen, raw)
		return next()
	})
	InternalUpdate.On(r.Context(), DefaultStage, func(_ context.Context, a any, next Next[any]) (any, error) {
		updates = append(updates, a)
		return next()
	})
	rec := &cfgRecorder{}
	f := r.Plugin(rec.plugin(decodeUpper), "a")
	want(t, f, Active)
	if err := f.Commit(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	if rec.count() != 1 || len(updates) != 0 || len(seen) != 1 || seen[0] != "b" {
		t.Fatalf("commit: applies %v, update hooks %v, commit hooks %v", rec.seen, updates, seen)
	}
	// A restart that the kernel starts applies what was committed.
	if _, err := r.Batch(ctx, UpdateOp(f)); err != nil {
		t.Fatal(err)
	}
	want(t, f, Active)
	if rec.count() != 2 || rec.last() != "decoded:b" {
		t.Fatalf("restart applied %v", rec.seen)
	}
}

func TestCommitRefusalRetainsNothing(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	boom := errors.New("boom")
	mode := ""
	InternalCommit.On(r.Context(), DefaultStage, func(_ context.Context, _ any, next Next[bool]) (bool, error) {
		switch mode {
		case "veto":
			return false, nil
		case "error":
			return false, boom
		}
		return next()
	})
	rec := &cfgRecorder{}
	f := r.Plugin(rec.plugin(decodeUpper), "a")
	want(t, f, Active)
	for _, tc := range []struct {
		mode string
		cfg  any
		want error
	}{{"", 1, nil}, {"veto", "b", ErrCommitVetoed}, {"error", "b", boom}} {
		mode = tc.mode
		err := f.Commit(ctx, tc.cfg)
		if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
			t.Fatalf("mode %q: %v", tc.mode, err)
		}
	}
	mode = ""
	if _, err := r.Batch(ctx, UpdateOp(f)); err != nil {
		t.Fatal(err)
	}
	want(t, f, Active)
	if rec.last() != "decoded:a" {
		t.Fatalf("a refused commit was retained: %v", rec.seen)
	}
	// A disposed fiber, and a fiber that takes no config.
	if err := f.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.Await(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.Commit(ctx, "c"); !errors.Is(err, ErrInactive) {
		t.Fatalf("commit of a disposed fiber: %v", err)
	}
	w := r.Context().When(nil, func(*Context) error { return nil })
	if err := w.Commit(ctx, "c"); err != nil {
		t.Fatalf("commit of a When fiber: %v", err)
	}
}

func TestCommitOpIsRetainedWithTheBatchOrNotAtAll(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	var restarts int
	InternalUpdate.On(r.Context(), DefaultStage, func(_ context.Context, a any, next Next[any]) (any, error) {
		restarts++
		return next()
	})
	veto := false
	InternalCommit.On(r.Context(), DefaultStage, func(_ context.Context, _ any, next Next[bool]) (bool, error) {
		if veto {
			return false, nil
		}
		return next()
	})
	var x, y cfgRecorder
	a := r.Plugin(x.plugin(nil), "a0")
	b := r.Plugin(y.plugin(nil), "b0")
	want(t, a, Active)
	want(t, b, Active)

	// One batch: a committed, b updated. Only b restarts.
	if _, err := r.Batch(ctx, CommitOp(a, "a1"), UpdateOpWith(b, "b1")); err != nil {
		t.Fatal(err)
	}
	want(t, b, Active)
	if restarts != 1 || x.count() != 1 || y.last() != "b1" {
		t.Fatalf("restarts %d, a %v, b %v", restarts, x.seen, y.seen)
	}
	// A refused batch (a veto, or the core) gives the committed config back.
	veto = true
	if _, err := r.Batch(ctx, CommitOp(a, "a2"), CommitOp(b, "b2")); !errors.Is(err, ErrCommitVetoed) {
		t.Fatalf("veto: %v", err)
	}
	veto = false
	if _, err := r.Batch(ctx, CommitOp(a, "a3"), DisposeOp(b), UpdateOp(b)); err == nil {
		t.Fatal("a batch that updates a fiber it disposes was accepted")
	}
	if got := a.cell.load(); got != "a1" {
		t.Fatalf("a retains %v after refused batches, want a1", got)
	}
	if _, err := r.Batch(ctx, UpdateOp(a)); err != nil {
		t.Fatal(err)
	}
	want(t, a, Active)
	if x.last() != "a1" {
		t.Fatalf("a applied %v", x.seen)
	}
}
