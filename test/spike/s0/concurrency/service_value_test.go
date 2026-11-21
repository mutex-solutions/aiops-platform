// SPDX-License-Identifier: Apache-2.0

package concurrency

import (
	"context"
	"testing"
)

// An observer registered after a Provide sees the service event of a later Notify with
// the provided value, the value that Events reports for that sequence: the observer
// stream is the backend's exact ledger, whether or not an export met the provide event
// first.
func TestLateObserverSeesTheValueOfAServiceEvent(t *testing.T) {
	for name, mk := range map[string]func() Runtime{"kernel": NewKernel, "reconciler": NewReconciler, "graph-lock": NewGraphLock} {
		t.Run(name, func(t *testing.T) {
			ctx, r, observers, root := nativeObserverTestRuntime(t, mk)
			if _, err := r.Mount(ctx, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Context) error {
				return c.Provide("sample", "V")
			}}); err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			ch := make(chan Event, 64)
			if _, err := observers.ObserveEvents(root, "late", func(_ context.Context, e Event) error {
				if e.Kind == "service" {
					ch <- e
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := r.(Notifier).Notify(ctx, "sample"); err != nil {
				t.Fatal(err)
			}
			if err := observers.AwaitObservers(ctx); err != nil {
				t.Fatal(err)
			}
			got := nativeObserverReceive(t, ctx, ch)
			ledger, err := r.Events(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var want any
			for _, e := range ledger {
				if e.Sequence == got.Sequence {
					want = e.Value
				}
			}
			if got.Value != "V" || want != "V" {
				t.Errorf("observer service event %+v, Events reports Value %v for its sequence; want V for both", got, want)
			}
		})
	}
}
