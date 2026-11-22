// SPDX-License-Identifier: Apache-2.0

package concurrency

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

var serviceValueConstructors = map[string]func(Retention) Runtime{
	"kernel": NewKernelWith, "reconciler": NewReconcilerWith, "graph-lock": NewGraphLockWith,
}

// providedValueConstructors are the backends whose provide events and whose service
// events of an ending provision carry the provided value (graph-lock reports neither).
var providedValueConstructors = map[string]func(Retention) Runtime{"kernel": NewKernelWith, "reconciler": NewReconcilerWith}

// An observer registered after a Provide sees the service event of a later Notify with
// the provided value, the value that Events reports for that sequence: the observer
// stream is the backend's exact ledger, whether or not an export met the provide event
// first.
func TestLateObserverSeesTheValueOfAServiceEvent(t *testing.T) {
	for name, mk := range serviceValueConstructors {
		t.Run(name, func(t *testing.T) {
			ctx, r, observers, root := nativeObserverTestRuntime(t, func() Runtime { return mk(Retention{}) })
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
			serviceValueAgrees(t, ctx, r, []Event{got})
		})
	}
}

// An observer registered before a Provide is delivered its provide event with the
// provided value, the value that Events reports for that sequence. The consumers
// waiting for the key lengthen the owner turn that appends the event, so the
// observer is handed it well before Provide returns to the provider.
func TestEarlyObserverSeesTheValueOfAProvideEvent(t *testing.T) {
	for name, mk := range providedValueConstructors {
		t.Run(name, func(t *testing.T) {
			ctx, r, observers, root := nativeObserverTestRuntime(t, func() Runtime { return mk(Retention{}) })
			for i := range 64 {
				if _, err := r.Mount(ctx, Spec{ID: fmt.Sprintf("consumer%d", i), Needs: []string{"sample"}}); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			ch := make(chan Event, 1)
			if _, err := observers.ObserveEvents(root, "early", func(_ context.Context, e Event) error {
				if e.Kind == "provide" {
					ch <- e
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := root.Provide("sample", "V"); err != nil {
				t.Fatal(err)
			}
			if err := observers.AwaitObservers(ctx); err != nil {
				t.Fatal(err)
			}
			serviceValueAgrees(t, ctx, r, []Event{nativeObserverReceive(t, ctx, ch)})
		})
	}
}

// An observer held while the provider is disposed is delivered the service events of
// the ended provision with its value, the value that Events reports for their
// sequences.
func TestLateObserverSeesTheValueOfAnEndedProvision(t *testing.T) {
	for name, mk := range providedValueConstructors {
		t.Run(name, func(t *testing.T) {
			ctx, r, observers, root := nativeObserverTestRuntime(t, func() Runtime { return mk(Retention{}) })
			id, err := r.Mount(ctx, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Context) error {
				return c.Provide("sample", "V")
			}})
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			got := serviceValueHeld(t, ctx, r, observers, root, func() {
				if err := r.Dispose(ctx, id); err != nil {
					t.Fatal(err)
				}
				if err := r.Settle(ctx); err != nil {
					t.Fatal(err)
				}
			})
			if len(got) == 0 {
				t.Fatal("the held observer saw no service event of the ended provision")
			}
			serviceValueAgrees(t, ctx, r, got)
		})
	}
}

// An observer that lags behind a narrow event window is delivered the provide and
// service events of "sample" with its value after those events have left the window,
// whether the provision is still live or has ended, and however many provisions the
// backend made meanwhile.
func TestLaggingObserverSeesTheValueOfAnEventThatLeftTheWindow(t *testing.T) {
	for name, mk := range providedValueConstructors {
		for _, end := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/ended=%v", name, end), func(t *testing.T) {
				ctx, r, observers, root := nativeObserverTestRuntime(t, func() Runtime { return mk(Retention{EventWindow: 16}) })
				got := serviceValueHeld(t, ctx, r, observers, root, func() {
					id, err := r.Mount(ctx, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Context) error {
						return c.Provide("sample", "V")
					}})
					if err != nil {
						t.Fatal(err)
					}
					if err := r.Settle(ctx); err != nil {
						t.Fatal(err)
					}
					if end {
						if err := r.Dispose(ctx, id); err != nil {
							t.Fatal(err)
						}
					}
					for i := range 400 {
						key := fmt.Sprintf("k%d", i)
						if _, err := r.Mount(ctx, Spec{ID: key, Provides: []string{key}, Apply: func(c *Context) error {
							return c.Provide(key, i)
						}}); err != nil {
							t.Fatal(err)
						}
					}
					if err := r.Settle(ctx); err != nil {
						t.Fatal(err)
					}
				})
				var provide bool
				for _, e := range got {
					provide = provide || e.Kind == "provide"
					if e.Value != "V" {
						t.Errorf("observer event %+v; want Value V", e)
					}
				}
				if !provide {
					t.Errorf("the observer saw no provide event of sample: %+v", got)
				}
			})
		}
	}
}

// serviceValueHeld registers an observer, holds it on a marker while act runs, then
// releases it and returns the provide and service events of "sample" it was delivered.
func serviceValueHeld(t *testing.T, ctx context.Context, r Runtime, observers ObserverRuntime, root *Context, act func()) []Event {
	t.Helper()
	release, unblock := nativeObserverRelease(t)
	started := make(chan struct{}, 1)
	var mu sync.Mutex
	var got []Event
	if _, err := observers.ObserveEvents(root, "held", func(_ context.Context, e Event) error {
		if e.Kind == "fixture" && e.Key == "held" {
			started <- struct{}{}
			<-release
		}
		if (e.Kind == "service" || e.Kind == "provide") && e.Key == "sample" {
			mu.Lock()
			got = append(got, e)
			mu.Unlock()
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	nativeObserverRecord(t, ctx, r, "held")
	nativeObserverReceive(t, ctx, started)
	act()
	unblock()
	if err := observers.AwaitObservers(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	return got
}

// serviceValueAgrees checks that every observed event carries V and that Events reports
// V for its sequence.
func serviceValueAgrees(t *testing.T, ctx context.Context, r Runtime, got []Event) {
	t.Helper()
	ledger, err := r.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range got {
		var want any
		for _, e := range ledger {
			if e.Sequence == g.Sequence {
				want = e.Value
			}
		}
		if g.Value != "V" || want != "V" {
			t.Errorf("observer event %+v, Events reports Value %v for its sequence; want V for both", g, want)
		}
	}
}
