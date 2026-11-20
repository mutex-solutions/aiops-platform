// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
)

// A caller whose context ends while its command waits for room in a full inbox
// gets the context error, and the command is never queued.
func TestExchangeEndsWithTheContextWhileTheInboxIsFull(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := bareCore(t, 1)
		r.inbox <- message{kind: cmdSnapshot} // Full, and no owner drains it.
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() { _, err := r.mount(ctx, pluginSpec{ID: "blocked"}); result <- err }()
		synctest.Wait() // Parked in the send.
		select {
		case err := <-result:
			t.Fatalf("mount returned while the inbox was full: %v", err)
		default:
		}
		cancel()
		synctest.Wait()
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("mount = %v, want the context error", err)
		}
		if len(r.inbox) != 1 {
			t.Fatalf("inbox holds %d messages, the blocked mount must not be queued", len(r.inbox))
		}
	})
}

// A batch whose context ends after the owner admitted the command but before it
// commits changes nothing: no fiber is created and no id is consumed.
func TestBatchWhoseContextEndedBeforeTheCommitChangesNothing(t *testing.T) {
	m := newManualOwner(t)
	ctx := newLateCancelCtx(1, nil) // Live for the owner's admission check, ended after.
	reply, deferred := m.o.handle(message{kind: cmdBatch, ctx: ctx, batch: []batchOp{
		{Kind: batchMount, Spec: pluginSpec{ID: "never"}},
	}})
	if deferred || !errors.Is(reply.err, context.Canceled) || reply.ids != nil {
		t.Fatalf("batch = %+v deferred=%v, want the context error and no ids", reply, deferred)
	}
	if len(m.o.fibers) != 0 || m.o.nextFiber != 0 || len(m.o.queue) != 0 || m.o.batchDepth != 0 {
		t.Fatalf("a refused batch left state: fibers=%d next=%d queue=%d depth=%d", len(m.o.fibers), m.o.nextFiber, len(m.o.queue), m.o.batchDepth)
	}
}

// The owner refuses a nil disposer from a live activation and registers nothing.
func TestOwnerRefusesANilDisposerFromALiveActivation(t *testing.T) {
	r := rcRuntime(t)
	got := make(chan error, 1)
	id := rcMount(t, r, Spec{ID: "w", Apply: func(c *Handle) error {
		got <- c.tryDefer("resource", nil)
		return nil
	}})
	err := <-got
	if err == nil || errors.Is(err, ErrInactive) || err.Error() != "nil disposer" {
		t.Fatalf("tryDefer(nil) = %v, want the owner's nil disposer refusal", err)
	}
	rcSettle(t, r)
	if snap := rcSnapshot(t, r, id); snap.State != Active || len(snap.Effects) != 0 {
		t.Fatalf("state=%s effects=%v", snap.State, snap.Effects)
	}
}

// An effect registration that passed its own liveness check and then lost the
// race with disposal reaches the owner on a stale activation and is refused.
func TestOwnerRefusesAnEffectFromAStaleActivation(t *testing.T) {
	r := rcRuntime(t)
	handles := make(chan *Handle, 1)
	id := rcMount(t, r, Spec{ID: "w", Apply: func(c *Handle) error { handles <- c; return nil }})
	h := <-handles
	rcSettle(t, r)
	if err := r.Dispose(rcGuard(t), id); err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
	ran := false
	if err := h.effectFn("late", func(context.Context) error { ran = true; return nil }); !errors.Is(err, ErrInactive) {
		t.Fatalf("effectFn on a stale activation = %v", err)
	}
	if ran {
		t.Fatal("the refused disposer ran")
	}
}
