// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
)

// RunWith puts the owner's built-in step innermost (07-c4-plugin-sdk.md:386).
func TestWaterfallRunWithPutsTheOwnersStepInnermost(t *testing.T) {
	fx := newHookFx(t)
	ctx := frontCtx(t)
	var order []string
	var mu sync.Mutex
	inner := func(_ context.Context, a int) (int, error) {
		record(&order, &mu, "builtin")
		return a * 10, nil
	}

	// An empty chain runs the step alone; a nil step is Run.
	w, wD := newWf[int, int](fx, "t/with")
	if v, err := wD.RunWith(ctx, Unscoped, 3, inner); v != 30 || err != nil {
		t.Fatalf("empty chain: %d, %v", v, err)
	}
	if v, err := wD.RunWith(ctx, Unscoped, 3, nil); v != 0 || err != nil {
		t.Fatalf("nil step: %d, %v", v, err)
	}

	// Listeners wrap it, outermost first, and may change its result.
	wrap := func(id string) WaterfallListener[int, int] {
		return func(_ context.Context, _ int, next Next[int]) (int, error) {
			record(&order, &mu, id+" before")
			v, err := next()
			record(&order, &mu, id+" after")
			return v + 1, err
		}
	}
	want(t, fx.mountIn("", listenWaterfall(w, "b", DefaultStage, wrap("b"))), Active)
	want(t, fx.mountIn("", listenWaterfall(w, "a", DefaultStage, wrap("a"))), Active)
	order = nil
	if v, err := wD.RunWith(ctx, Unscoped, 3, inner); v != 32 || err != nil ||
		!slices.Equal(order, []string{"a before", "b before", "builtin", "b after", "a after"}) {
		t.Fatalf("wrapped: %d, %v, order %v", v, err, order)
	}

	// A listener that does not call next vetoes the step; an error of the step
	// fails the dispatch.
	v, vD := newWf[int, int](fx, "t/with-veto")
	want(t, fx.mountIn("", listenWaterfall(v, "a", DefaultStage, func(context.Context, int, Next[int]) (int, error) { return 5, nil })), Active)
	order = nil
	if got, err := vD.RunWith(ctx, Unscoped, 3, inner); got != 5 || err != nil || len(order) != 0 {
		t.Fatalf("veto: %d, %v, order %v", got, err, order)
	}
	boom := errors.New("boom")
	_, eD := newWf[int, int](fx, "t/with-err")
	var le *ListenerError
	if _, err := eD.RunWith(ctx, Unscoped, 3, func(context.Context, int) (int, error) { return 0, boom }); !errors.Is(err, boom) || !errors.As(err, &le) {
		t.Fatalf("step error: %v", err)
	}

	// A dispatcher that is not bound calls nothing.
	var zero WaterfallDispatcher[int, int]
	if _, err := zero.RunWith(ctx, Unscoped, 3, inner); !errors.Is(err, ErrInactive) {
		t.Fatalf("zero dispatcher: %v", err)
	}
}
