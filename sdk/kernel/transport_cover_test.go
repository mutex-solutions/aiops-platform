// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

// A bareCore is a core whose owner goroutine was never started: the test plays the
// owner, so each ordering of an owner exit against a caller is deterministic.
func bareCore(t *testing.T, inbox int) *core {
	t.Helper()
	cfg := testConfig()
	r := &core{cfg: cfg, inbox: make(chan message, inbox), wake: make(chan struct{}, 1), done: make(chan struct{})}
	newOwner(r)
	t.Cleanup(func() { _ = r.observers.close(context.Background()) })
	return r
}

func TestExchangeAnswersAWaitWhoseOwnerExitedAsClosed(t *testing.T) {
	r := bareCore(t, 1)
	result := make(chan error, 1)
	go func() { result <- r.settle(context.Background()) }()
	if msg := <-r.inbox; msg.kind != cmdSettle {
		t.Fatalf("queued %v, want a settle", msg.kind)
	}
	close(r.done) // The owner exits without answering.
	if err := <-result; !errors.Is(err, errClosed) {
		t.Fatalf("settle on an exited owner = %v", err)
	}
}

func TestExchangeAnswersAnAdmissionWhoseOwnerExitedAsClosed(t *testing.T) {
	r := bareCore(t, 1)
	result := make(chan error, 1)
	go func() { _, err := r.mount(context.Background(), pluginSpec{ID: "late"}); result <- err }()
	msg := <-r.inbox
	close(r.done)
	if err := <-result; !errors.Is(err, errClosed) {
		t.Fatalf("mount on an exited owner = %v", err)
	}
	if msg.claim.Load() != admAbandoned {
		t.Fatal("the unapplied command was not marked abandoned")
	}
}

func TestCloseTreatsAnOwnerThatExitsMidCommandAsClosed(t *testing.T) {
	r := bareCore(t, 1)
	result := make(chan error, 1)
	go func() { result <- r.close(context.Background()) }()
	if msg := <-r.inbox; msg.kind != cmdClose {
		t.Fatalf("queued %v, want a close", msg.kind)
	}
	close(r.done)
	if err := <-result; err != nil {
		t.Fatalf("close on an owner that exited under it = %v", err)
	}
}

func TestCloseJoinsAnOwnerThatExitsAfterAnswering(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := bareCore(t, 1)
		rootErr := errors.New("host failure")
		r.rootErr = rootErr
		result := make(chan error, 1)
		go func() { result <- r.close(context.Background()) }()
		msg := <-r.inbox
		msg.ack <- answer{} // Answered, but the owner has not exited yet.
		synctest.Wait()
		select {
		case err := <-result:
			t.Fatalf("close returned before the owner exited: %v", err)
		default:
		}
		close(r.done)
		synctest.Wait()
		if err := <-result; !errors.Is(err, rootErr) {
			t.Fatalf("close = %v, want the retained host failure", err)
		}
	})
}

func TestCloseReturnsTheContextErrorAndStillReleasesObservers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := bareCore(t, 1)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		result := make(chan error, 1)
		go func() { result <- r.close(ctx) }()
		msg := <-r.inbox
		msg.ack <- answer{}
		synctest.Wait()
		time.Sleep(2 * time.Second) // The owner stays alive past the caller's deadline.
		synctest.Wait()
		if err := <-result; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("close = %v", err)
		}
		// The late exit still closes the observer queue, through the one-shot waiter.
		close(r.done)
		synctest.Wait()
		if status := r.observers.d.snapshot(); !status.Closing || !status.Stopped {
			t.Fatalf("observers were not released after the owner exited: %+v", status)
		}
	})
}

func TestCompletionOfAWorkerIsDroppedOnceTheOwnerExited(t *testing.T) {
	r := bareCore(t, 0) // Nobody receives: only the exit can release the sender.
	finished := make(chan struct{})
	go func() { r.completed(message{kind: cmdLoadDone}); close(finished) }()
	close(r.done)
	<-finished
}
