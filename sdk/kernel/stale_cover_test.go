// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

// Quiescence can begin between enter's first liveness check and its increment.
// The lease is then rolled back, nothing runs inside it, and the drainer is woken
// because the rollback may be the move that brings the lease count to zero.
func TestEnterRollsBackALeaseTakenAfterQuiescenceBegan(t *testing.T) {
	h := newHandle(nil, context.Background(), 1, nil)
	wakes := 0
	h.leaseChanged = func() { wakes++ }
	var shard atomic.Int64
	// live is false: the state the second check sees after the flip.
	leave, ok := h.confirm(&shard)
	if ok || leave != nil || shard.Load() != 0 || wakes != 1 {
		t.Fatalf("confirm on a quiesced handle: ok=%v leave=%v shard=%d wakes=%d", ok, leave != nil, shard.Load(), wakes)
	}
	h.live.Store(true)
	leave, ok = h.confirm(&shard)
	if !ok || leave == nil || shard.Load() != 1 || wakes != 1 {
		t.Fatalf("confirm on a live handle: ok=%v shard=%d wakes=%d", ok, shard.Load(), wakes)
	}
}

// A command whose context ended while it waited in the inbox is answered with the
// context error and applied nowhere.
func TestOwnerAnswersACommandWhoseContextEndedInTheInbox(t *testing.T) {
	m := newManualOwner(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reply, deferred := m.o.handle(message{kind: cmdMount, ctx: ctx, spec: pluginSpec{ID: "never"}})
	if deferred || !errors.Is(reply.err, context.Canceled) || len(m.o.fibers) != 0 || m.o.nextFiber != 0 {
		t.Fatalf("reply=%+v deferred=%v fibers=%d", reply, deferred, len(m.o.fibers))
	}
}

// A provision requested through a stale activation is refused, and no provision
// is recorded.
func TestOwnerRefusesAProvisionFromAStaleActivation(t *testing.T) {
	r := rcRuntime(t)
	handles := make(chan *Handle, 1)
	id := rcMount(t, r, Spec{ID: "w", Provides: []string{"K"}, Apply: func(c *Handle) error { handles <- c; return nil }})
	h := <-handles
	rcSettle(t, r)
	if err := r.Dispose(rcGuard(t), id); err != nil {
		t.Fatal(err)
	}
	rcSettle(t, r)
	if err := h.provideFn("K", "late", 1); !errors.Is(err, ErrInactive) {
		t.Fatalf("provideFn on a stale activation = %v", err)
	}
	if view := rcInspect(t, r).Provisions; len(view) != 0 {
		t.Fatalf("a refused provision was recorded: %+v", view)
	}
}
