// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"context"
	"errors"
	"testing"
	"time"

	co "github.com/mutex-solutions/aiops-platform/test/spike/s0/composition"
	cc "github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
	cm "github.com/mutex-solutions/aiops-platform/test/spike/s0/contextmodel"
)

func TestComposedHookDispatchRequiresActualNativeOwnership(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			native := factory()
			defer native.Close(ctx)
			ownerEntry := cm.NewEntry("owner")
			listenerEntry := cm.NewEntry("listener")
			key := cm.NewKey[int]("owner", 0, cm.Classes(cm.ProviderBound, cm.Open))
			root, err := co.NewRuntime(native, cm.RootSpec{Entries: []cm.Plugin{ownerEntry, listenerEntry},
				Keys: []cm.AnyKey{key.Any()}, Bindings: []cm.Binding{{Key: key.Any(), Providers: []cm.Plugin{ownerEntry}}}})
			if err != nil {
				t.Fatal(err)
			}
			event, err := NewEvent[int](Definition{Name: "owner/event", Owner: key.Any(), Stages: []string{"main"}})
			if err != nil {
				t.Fatal(err)
			}
			use, err := event.Use("main")
			if err != nil {
				t.Fatal(err)
			}
			var owner Binding
			var before error
			provider, err := root.Start(ctx, root.Context(), ownerEntry, co.Spec{
				Metadata: cm.Spec{ID: "provider", Provides: []cm.AnyKey{key.Any()}},
				Apply: func(c *co.Context) (cc.Disposer, error) {
					binding, err := Bind(c)
					if err != nil {
						return nil, err
					}
					owner = binding
					before = event.Emit(ctx, binding, cm.Audience{}, 1)
					if err := co.Provide(c, key, 1, nil); err != nil {
						return nil, err
					}
					return nil, nil
				}})
			if err != nil {
				t.Fatal(err)
			}
			if err := provider.Await(ctx); err != nil {
				t.Fatal(err)
			}
			if !errors.Is(before, ErrOwner) {
				t.Fatalf("declaration-only dispatch was not refused: %v", before)
			}
			var called int
			listener, err := root.Start(ctx, root.Context(), listenerEntry, co.Spec{
				Metadata: cm.Spec{ID: "listener"}, Apply: func(c *co.Context) (cc.Disposer, error) {
					binding, err := Bind(c, use)
					if err != nil {
						return nil, err
					}
					_, err = event.On(binding, "main", func(context.Context, int) (Decision, error) {
						called++
						return Decision{}, nil
					})
					return nil, err
				}})
			if err != nil {
				t.Fatal(err)
			}
			if err := listener.Await(ctx); err != nil {
				t.Fatal(err)
			}
			if err := event.Emit(ctx, owner, cm.Audience{}, 1); err != nil || called != 1 {
				t.Fatalf("native owner dispatch = %v, calls=%d", err, called)
			}
			if err := native.Dispose(ctx, provider.ID); err != nil {
				t.Fatal(err)
			}
			if err := provider.Await(ctx); err != nil {
				t.Fatal(err)
			}
			if err := event.Emit(ctx, owner, cm.Audience{}, 1); err == nil || called != 1 {
				t.Fatalf("released owner dispatched: %v, calls=%d", err, called)
			}
			if _, err := Bind(nil); !errors.Is(err, ErrBinding) {
				t.Fatal("nil composition context was accepted")
			}
		})
	}
}
