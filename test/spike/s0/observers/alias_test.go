// SPDX-License-Identifier: Apache-2.0

package observers

import (
	"context"
	"testing"

	"github.com/mutex-solutions/aiops-platform/test/spike/s0/internal/eventqueue"
)

func TestAliasesPreserveDispatcherTypesAndSentinelIdentities(t *testing.T) {
	for _, pair := range [][2]error{
		{ErrClosed, eventqueue.ErrClosed},
		{ErrSequenceGap, eventqueue.ErrSequenceGap},
		{ErrNotAccepted, eventqueue.ErrNotAccepted},
		{ErrBinding, eventqueue.ErrBinding},
		{ErrObserver, eventqueue.ErrObserver},
		{ErrInactive, eventqueue.ErrInactive},
		{ErrReentrant, eventqueue.ErrReentrant},
	} {
		if pair[0] != pair[1] {
			t.Fatal("alias boundary replaced a sentinel error identity")
		}
	}
	d := New[int](nil)
	var native *eventqueue.Dispatcher[int] = d
	var old *Dispatcher[int] = native
	var callback eventqueue.Observer[int] = Observer[int](func(context.Context, int) error { return nil })
	var callbackAlias Observer[int] = callback
	_ = callbackAlias
	if old != d {
		t.Fatal("alias boundary wrapped or replaced the dispatcher")
	}
	if err := native.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
