// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"errors"
	"testing"
	"time"
)

// A notice that is already queued is not queued again: a second wake before the
// owner reads the stack would otherwise link the notice to itself.
func TestNoticeIsQueuedOnceUntilTheOwnerReadsIt(t *testing.T) {
	r := bareCore(t, 1)
	n := &notice{}
	r.notify(n)
	r.notify(n)
	if r.notices.Load() != n || n.next != nil {
		t.Fatalf("notice linked twice: head=%p next=%p", r.notices.Load(), n.next)
	}
	if len(r.wake) != 1 {
		t.Fatalf("wake signals pending: %d", len(r.wake))
	}
	// Once the owner has read it, the notice can be queued again.
	n.queued.Store(false)
	r.notices.Store(nil)
	r.notify(n)
	if r.notices.Load() != n {
		t.Fatal("a read notice was not queued again")
	}
}

// Two callers of Dispose on one context can both pass the context's own liveness
// check before the owner reads either request. The owner applies the first and
// refuses the second: the activation is no longer current.
func TestSecondSelfDisposalOfOneActivationIsRefused(t *testing.T) {
	m := newManualOwner(t)
	handles := make(chan *handle, 1)
	id := m.mount(pluginSpec{ID: "w", Apply: func(c *handle) error { handles <- c; return nil }})
	m.pump(func() bool { return m.active(id) })
	h := <-handles

	results := make(chan error, 2)
	for range 2 {
		go func() { results <- h.requestDispose() }()
	}
	var requests [2]message
	for i := range requests {
		select {
		case requests[i] = <-m.r.inbox:
		case <-time.After(15 * time.Second):
			t.Fatal("a disposal request never reached the owner")
		}
		if requests[i].kind != cmdSelfDispose {
			t.Fatalf("queued %v, want a self-disposal", requests[i].kind)
		}
	}
	m.deliver(requests[0])
	m.deliver(requests[1])
	first, second := <-results, <-results
	if (first == nil) == (second == nil) {
		t.Fatalf("results %v and %v: exactly one request must be applied", first, second)
	}
	if err := errors.Join(first, second); !errors.Is(err, ErrInactive) {
		t.Fatalf("the refused request answered %v, want ErrInactive", err)
	}
	m.pump(func() bool { return m.o.fibers[id] == nil })
}
