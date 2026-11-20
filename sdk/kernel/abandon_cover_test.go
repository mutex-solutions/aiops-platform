// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"testing"
)

// An abandoned wait whose reply is already queued takes that reply and not the
// cancellation. Whether a wait meets this depends on the schedule, so the block is
// covered here by construction and not by the end of a test's context.
func TestAnAbandonedWaitTakesAReplyThatAlreadyArrived(t *testing.T) {
	ack := make(chan answer, 1)
	ack <- answer{id: 7}
	var c core
	if got := c.abandon(message{kind: cmdAwait, ack: ack}, context.Canceled); got.id != 7 || got.err != nil {
		t.Fatalf("abandon returned %+v, want the queued reply", got)
	}
}

// A command sent to a core that has closed, whose inbox takes nothing, is refused
// as closed. Which of the ready channels of the send wins depends on the schedule
// in a live core, so the block is covered here with a core that has only a closed
// done channel and an inbox with no reader.
func TestACommandToAClosedCoreWithAFullInboxIsRefusedAsClosed(t *testing.T) {
	c := core{inbox: make(chan message), done: make(chan struct{})}
	close(c.done)
	if got := c.exchange(context.Background(), message{kind: cmdAwait}); got.err != errClosed {
		t.Fatalf("exchange returned %+v, want errClosed", got)
	}
}
