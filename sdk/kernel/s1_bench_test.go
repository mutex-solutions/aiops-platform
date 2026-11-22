// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"testing"
	"time"
)

// BenchmarkMountDispose measures one mount and one dispose of a fiber on the
// public front: the owner path that the ledger and the observer dispatcher sit on.
func BenchmarkMountDispose(b *testing.B) {
	r := NewForTest(b, Config{LoadDeadline: time.Second, DrainDeadline: time.Second, DisposeDeadline: time.Second,
		ArchiveLimit: 8, EventWindow: 64, ObserverQueue: 1024, OwnerInbox: 64, TerminalLimit: 8})
	p := plug("bench/p", nil, nil, nil)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		f := r.Plugin(p, nil)
		if err := f.Await(ctx); err != nil {
			b.Fatal(err)
		}
		if err := f.Dispose(ctx); err != nil {
			b.Fatal(err)
		}
		if err := f.Await(ctx); err != nil {
			b.Fatal(err)
		}
	}
}
