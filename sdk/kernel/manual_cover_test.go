// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A manualOwner is the real owner, driven by the test instead of by its goroutine:
// the test delivers each message and runs each loop turn itself, in the order
// owner.run does, so an interleaving that is a rare race under the goroutine is an
// explicit sequence here. Nothing else differs: the callbacks still run on their
// own goroutines and report through the inbox, which pump reads.
type manualOwner struct {
	t *testing.T
	r *core
	o *owner
}

func newManualOwner(t *testing.T) *manualOwner {
	t.Helper()
	cfg := testConfig()
	r := &core{cfg: cfg, inbox: make(chan message, cfg.ownerInbox), wake: make(chan struct{}, 1), done: make(chan struct{})}
	m := &manualOwner{t: t, r: r, o: newOwner(r)}
	t.Cleanup(func() { _ = r.observers.close(context.Background()) })
	return m
}

// turn is the top of one iteration of owner.run.
func (m *manualOwner) turn() {
	m.o.consumeNotices()
	m.o.consumeCancelled()
	m.o.expire(time.Now())
	m.o.work()
	m.o.flushServices()
	m.o.finishWaiters()
}

// deliver is the message arm of owner.run for msg.
func (m *manualOwner) deliver(msg message) {
	if msg.claim != nil && !msg.claim.CompareAndSwap(admQueued, admClaimed) {
		return
	}
	if !msg.at.IsZero() {
		m.o.expire(time.Now())
	}
	reply, deferred := m.o.handle(msg)
	m.o.consumeNotices()
	m.o.work()
	m.o.flushServices()
	if !deferred && msg.ack != nil {
		msg.ack <- reply
	}
}

// send builds and delivers one command as a caller would, and returns its ack.
func (m *manualOwner) send(ctx context.Context, msg message) chan answer {
	msg.ctx, msg.ack = ctx, make(chan answer, 1)
	if !isWait(msg.kind) {
		msg.claim = new(atomic.Uint32)
	}
	m.deliver(msg)
	m.turn()
	return msg.ack
}

// pump delivers what callbacks post to the inbox, one turn after each message,
// until cond holds. A callback that never reports fails the test.
func (m *manualOwner) pump(cond func() bool) {
	m.t.Helper()
	timeout := time.After(15 * time.Second)
	for !cond() {
		select {
		case msg := <-m.r.inbox:
			m.deliver(msg)
			m.turn()
		case <-timeout:
			m.t.Fatal("owner never received the awaited callback report")
		}
	}
}

func (m *manualOwner) mount(spec pluginSpec) uint64 {
	m.t.Helper()
	ack := m.send(context.Background(), message{kind: cmdMount, spec: spec})
	if reply := <-ack; reply.err != nil {
		m.t.Fatal(reply.err)
	}
	return m.o.nextFiber
}

// spinUntil waits for an asynchronous hand-off by yielding, never sleeping.
func spinUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition never held")
		}
		runtime.Gosched()
	}
}

// loadingFiber mounts a fiber whose Apply waits on the returned release function.
func (m *manualOwner) loadingFiber(id string) (fiberID uint64, release func()) {
	m.t.Helper()
	gate := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }
	m.t.Cleanup(release)
	fiberID = m.mount(pluginSpec{ID: id, Apply: func(*handle) error { <-gate; return nil }})
	if f := m.o.fibers[fiberID]; f.state != Loading {
		m.t.Fatalf("fiber is %s, want LOADING", f.state)
	}
	return fiberID, release
}

func (m *manualOwner) active(id uint64) bool { return m.o.fibers[id].state == Active }

// The context of a parked waiter can end after the owner last read the stack of
// cancelled waiters and before it answers the waiter for a lifecycle reason. The
// owner then answers once, with the context error, and the late cancellation is
// ignored.
func TestWaiterAnsweredAfterItsContextEndedIsNotAnsweredTwice(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  func(id uint64) message
	}{
		{"settle", func(uint64) message { return message{kind: cmdSettle} }},
		{"await", func(id uint64) message { return message{kind: cmdAwait, id: id} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newManualOwner(t)
			id, release := m.loadingFiber("w")
			ctx, cancel := context.WithCancel(context.Background())
			ack := m.send(ctx, tc.msg(id))
			select {
			case reply := <-ack:
				t.Fatalf("a waiter on a loading fiber was answered early: %v", reply.err)
			default:
			}
			cancel()
			spinUntil(t, func() bool { return m.r.cancelled.Load() != nil })
			release()
			// The completion is handled, then the turn answers the waiter before it
			// reads the cancelled stack (as when the stack was empty a moment earlier).
			m.deliver(<-m.r.inbox)
			m.o.consumeNotices()
			m.o.expire(time.Now())
			m.o.work()
			m.o.flushServices()
			m.o.finishWaiters()
			if len(ack) != 1 {
				t.Fatalf("waiter not answered by the lifecycle turn: %d answers", len(ack))
			}
			if reply := <-ack; !errors.Is(reply.err, context.Canceled) {
				t.Fatalf("reply = %v, want the context error", reply.err)
			}
			m.o.consumeCancelled()
			if len(ack) != 0 {
				t.Fatal("the late cancellation answered the waiter a second time")
			}
		})
	}
}

// The turn that handles a completion also prunes the cancelled waiters first, so
// the fiber it touched can be left with no waiter to answer.
func TestCancelledAwaitLeavesNoWaiterForTheTouchedFiber(t *testing.T) {
	m := newManualOwner(t)
	id, release := m.loadingFiber("w")
	ctx, cancel := context.WithCancel(context.Background())
	ack := m.send(ctx, message{kind: cmdAwait, id: id})
	f := m.o.fibers[id]
	if f.awaiters != 1 {
		t.Fatalf("awaiters = %d", f.awaiters)
	}
	cancel()
	spinUntil(t, func() bool { return m.r.cancelled.Load() != nil })
	release()
	// The load completion touches the fiber in the turn that also prunes the
	// cancelled waiter, so the touched fiber has nobody left to answer.
	m.pump(func() bool { return m.o.fibers[id].phase == phIdle })
	if reply := <-ack; !errors.Is(reply.err, context.Canceled) {
		t.Fatalf("reply = %v", reply.err)
	}
	if f.awaiters != 0 || len(m.o.awaiting) != 0 || len(m.o.dirty) != 0 {
		t.Fatalf("waiter state left behind: awaiters=%d awaiting=%d dirty=%d", f.awaiters, len(m.o.awaiting), len(m.o.dirty))
	}
}

// requestWithdrawal passes its own check that cleanup has not finished, then the
// owner finishes the scope's fiber before it reads the request. Whatever the
// answer of the owner, the caller must see success for a scope that is gone.
func TestWithdrawalRacingTheScopesDisposal(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply func(msg message, m *manualOwner)
	}{
		{"owner answers a gone scope", func(msg message, m *manualOwner) { m.deliver(msg) }},
		{"owner exited before the request", func(msg message, _ *manualOwner) { msg.ack <- answer{err: errClosed} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newManualOwner(t)
			handles := make(chan *effectHandle, 1)
			id := m.mount(pluginSpec{ID: "w", Apply: func(c *handle) error {
				eh, err := c.requestScope("scope", func(*effectScope) error { return nil })
				handles <- eh
				return err
			}})
			m.pump(func() bool { return m.active(id) })
			eh := <-handles

			result := make(chan error, 1)
			go func() { result <- eh.requestWithdrawal() }()
			var request message
			select {
			case request = <-m.r.inbox:
			case <-time.After(15 * time.Second):
				t.Fatal("withdrawal never reached the owner")
			}
			if request.kind != cmdScopeWithdraw {
				t.Fatalf("queued %v, want a scope withdrawal", request.kind)
			}
			// The fiber is disposed and removed before the owner reads the request.
			m.send(context.Background(), message{kind: cmdDispose, id: id})
			m.pump(func() bool { return m.o.fibers[id] == nil })
			if !eh.effect.cleanupFinished() {
				t.Fatal("the scope outlived its fiber")
			}
			tc.reply(request, m)
			if err := <-result; err != nil {
				t.Fatalf("withdrawal of a finished scope = %v", err)
			}
		})
	}
}
