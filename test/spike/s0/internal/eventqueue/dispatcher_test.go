// SPDX-License-Identifier: Apache-2.0

package eventqueue

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAppendRejectsSequenceWraparoundWithoutQueueMutation(t *testing.T) {
	// A stopped worker makes this arithmetic-only boundary deterministic without
	// synthesizing 2^64 source events. This is not a fabricated lifecycle host.
	d := New[int](nil)
	if err := d.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	d.status.Closing = false
	d.status.Accepted, d.status.Delivered = ^uint64(0), ^uint64(0)
	d.mu.Unlock()
	for _, sequence := range []uint64{0, 1, ^uint64(0)} {
		if err := d.Append(sequence, 0); !errors.Is(err, ErrSequenceGap) {
			t.Fatalf("wraparound sequence %d = %v", sequence, err)
		}
	}
	status := d.Status()
	if status.Accepted != ^uint64(0) || status.Delivered != ^uint64(0) || status.Queued != 0 {
		t.Fatalf("wraparound rejection changed state: %+v", status)
	}
}

func TestPopClearsReferencesAndReleasesDrainedBackingStorage(t *testing.T) {
	// Keep queue storage deterministic until the one real worker starts.
	// These are algorithm-only envelopes with no listeners or lifecycle binding.
	// All lifecycle lease/cleanup proof remains in observers' real-backend tests.
	const count = 2048
	d := &Dispatcher[*int]{
		progress: make(chan struct{}), wake: make(chan struct{}, 1), done: make(chan struct{}),
		status: Status{Accepted: count, Closing: true},
	}
	d.queue = make([]envelope[*int], count)
	for index := range d.queue {
		value := index
		d.queue[index] = envelope[*int]{sequence: uint64(index + 1), event: &value}
	}
	backing := d.queue
	go d.run()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case <-d.done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	d.mu.Lock()
	retained := d.queue != nil || d.head != 0
	d.mu.Unlock()
	if retained {
		t.Fatal("drained queue retained event backing storage")
	}
	for index, event := range backing {
		if event.event != nil || event.listeners != nil || event.sequence != 0 {
			t.Fatalf("popped slot %d retained an envelope reference", index)
		}
	}
	if status := d.Status(); status.Delivered != count || status.Remaining != 0 || !status.Stopped {
		t.Fatalf("actual algorithm drain = %+v", status)
	}
}
