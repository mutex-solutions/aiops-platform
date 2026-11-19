// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func effectCandidates() map[string]func() Runtime {
	return map[string]func() Runtime{"reconciler": NewReconciler}
}

func effectRuntime(t testing.TB, makeRuntime func() Runtime) (context.Context, Runtime) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	r := makeRuntime()
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		// Startup failures are asserted at AwaitFiber; Close can report them again.
		_ = closeRuntime(closeCtx, r)
		cancel()
	})
	return ctx, r
}

func effectWait[T any](t testing.TB, ctx context.Context, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-ctx.Done():
		t.Fatalf("effect barrier: %v", ctx.Err())
		var zero T
		return zero
	}
}

func effectAwait(t testing.TB, ctx context.Context, r Runtime, id uint64) error {
	t.Helper()
	waiter, ok := r.(Waiter)
	if !ok {
		t.Fatal("native fiber join unavailable")
	}
	return waiter.AwaitFiber(ctx, id)
}

func effectMount(t testing.TB, ctx context.Context, r Runtime, spec Spec) uint64 {
	t.Helper()
	id, err := r.Mount(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := effectAwait(t, ctx, r, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func effectSnapshot(t *testing.T, ctx context.Context, r Runtime, id uint64) Snapshot {
	t.Helper()
	inspection, err := r.(Inspector).Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range inspection.Fibers {
		if snapshot.ID == id {
			return snapshot
		}
	}
	t.Fatalf("missing native fiber %d", id)
	return Snapshot{}
}

func TestEffectSuccessfulSetupUsesNativeLIFO(t *testing.T) {
	for name, makeRuntime := range effectCandidates() {
		t.Run(name, func(t *testing.T) {
			ctx, r := effectRuntime(t, makeRuntime)
			contexts := make(chan *Context, 1)
			var mu sync.Mutex
			var cleanup []string
			add := func(label string) {
				mu.Lock()
				cleanup = append(cleanup, label)
				mu.Unlock()
			}
			id := effectMount(t, ctx, r, Spec{ID: "effect-owner", Apply: func(c *Context) error {
				contexts <- c
				if err := c.Defer("older", func(context.Context) error { add("older"); return nil }); err != nil {
					return err
				}
				if err := c.Effect("returned", func() (Disposer, error) {
					if c.lease.Load() != 1 {
						return nil, errors.New("setup entered without its real activation lease")
					}
					// Reentrant inspection proves setup is outside the native owner.
					if _, err := r.(Inspector).Inspect(ctx); err != nil {
						return nil, err
					}
					if err := c.Defer("body", func(context.Context) error { add("body"); return nil }); err != nil {
						return nil, err
					}
					return func(context.Context) error { add("returned"); return nil }, nil
				}); err != nil {
					return err
				}
				if err := c.Effect("no-resource", func() (Disposer, error) { return nil, nil }); err != nil {
					return err
				}
				return c.Defer("newer", func(context.Context) error { add("newer"); return nil })
			}})
			c := effectWait(t, ctx, contexts)
			if snapshot := effectSnapshot(t, ctx, r, id); !reflect.DeepEqual(snapshot.Effects, []string{"older", "body", "returned", "newer"}) {
				t.Fatalf("actual effect ledger = %+v", snapshot)
			}
			if c.lease.Load() != 0 {
				t.Fatal("successful setup retained a lease")
			}
			if err := r.Dispose(ctx, id); err != nil {
				t.Fatal(err)
			}
			if err := effectAwait(t, ctx, r, id); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			got := append([]string(nil), cleanup...)
			mu.Unlock()
			if !reflect.DeepEqual(got, []string{"newer", "returned", "body", "older"}) {
				t.Fatalf("native LIFO cleanup = %v", got)
			}
		})
	}
}

func TestEffectPartialSetupCleansImmediatelyWithoutRetainedSlot(t *testing.T) {
	setupErr, cleanupErr := errors.New("setup rejected"), errors.New("partial cleanup rejected")
	for name, makeRuntime := range effectCandidates() {
		for _, cleanupPanics := range []bool{false, true} {
			caseName := "error"
			if cleanupPanics {
				caseName = "panic"
			}
			t.Run(name+"/"+caseName, func(t *testing.T) {
				ctx, r := effectRuntime(t, makeRuntime)
				contexts, results := make(chan *Context, 1), make(chan error, 1)
				var calls atomic.Int64
				var mu sync.Mutex
				var cleanup []string
				add := func(label string) {
					mu.Lock()
					cleanup = append(cleanup, label)
					mu.Unlock()
				}
				id, err := r.Mount(ctx, Spec{ID: "partial-effect", Apply: func(c *Context) error {
					contexts <- c
					if err := c.Defer("older", func(context.Context) error { add("older"); return nil }); err != nil {
						return err
					}
					err := c.Effect("partial", func() (Disposer, error) {
						if err := c.Defer("body", func(context.Context) error { add("body"); return nil }); err != nil {
							return nil, err
						}
						return func(cleanupCtx context.Context) error {
							calls.Add(1)
							if _, ok := cleanupCtx.Deadline(); !ok || cleanupCtx.Err() != nil || c.lease.Load() != 1 {
								return errors.New("partial cleanup lost its native deadline or real lease")
							}
							// Cleanup also executes outside the state owner.
							if _, err := r.(Inspector).Inspect(ctx); err != nil {
								return err
							}
							add("partial")
							if cleanupPanics {
								panic("partial disposer exploded")
							}
							return cleanupErr
						}, setupErr
					})
					results <- err
					inspection, inspectErr := r.(Inspector).Inspect(ctx)
					if inspectErr != nil {
						return inspectErr
					}
					for _, snapshot := range inspection.Fibers {
						if snapshot.ID == c.fiber && !reflect.DeepEqual(snapshot.Effects, []string{"older", "body"}) {
							return errors.New("failed Effect retained a native ledger slot")
						}
					}
					return err
				}})
				if err != nil {
					t.Fatal(err)
				}
				err = effectAwait(t, ctx, r, id)
				returned := effectWait(t, ctx, results)
				for _, result := range []error{returned, err} {
					if !errors.Is(result, setupErr) {
						t.Fatalf("setup cause lost: %v", result)
					}
					if cleanupPanics {
						if !strings.Contains(result.Error(), "partial disposer exploded") {
							t.Fatalf("cleanup panic was not converted by native boundary: %v", result)
						}
					} else if !errors.Is(result, cleanupErr) {
						t.Fatalf("cleanup cause lost: %v", result)
					}
				}
				c := effectWait(t, ctx, contexts)
				mu.Lock()
				got := append([]string(nil), cleanup...)
				mu.Unlock()
				if calls.Load() != 1 || !reflect.DeepEqual(got, []string{"partial", "body", "older"}) {
					t.Fatalf("partial cleanup count/order = %d %v", calls.Load(), got)
				}
				if c.lease.Load() != 0 || !c.released.Load() {
					t.Fatal("failed setup retained its lease or prevented native unwind")
				}
				if snapshot := effectSnapshot(t, ctx, r, id); snapshot.State != Failed || len(snapshot.Effects) != 0 {
					t.Fatalf("failed activation retained partial effects: %+v", snapshot)
				}
			})
		}
	}
}

func TestEffectLateCleanupRetainsChildParentAndProviderThroughClose(t *testing.T) {
	for name, makeRuntime := range effectCandidates() {
		t.Run(name, func(t *testing.T) {
			ctx, r := effectRuntime(t, makeRuntime)
			releaseSetup, releaseCleanup := make(chan struct{}), make(chan struct{})
			var setupOnce, cleanupOnce sync.Once
			t.Cleanup(func() {
				setupOnce.Do(func() { close(releaseSetup) })
				cleanupOnce.Do(func() { close(releaseCleanup) })
			})
			setupEntered := make(chan *Context, 1)
			cleanupEntered := make(chan context.Context, 1)
			results, closeReturned := make(chan error, 1), make(chan error, 1)
			var parentCtx, providerCtx *Context
			var child uint64
			var cleanupCalls atomic.Int64
			var mu sync.Mutex
			var cleanup []string
			parentCleaned := make(chan struct{})
			add := func(label string) {
				mu.Lock()
				cleanup = append(cleanup, label)
				mu.Unlock()
			}
			token := &struct{ value string }{"retained"}
			provider := effectMount(t, ctx, r, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Context) error {
				providerCtx = c
				if err := c.Defer("provider-cleanup", func(context.Context) error { add("provider"); return nil }); err != nil {
					return err
				}
				return c.Provide("sample", token)
			}})
			parent := effectMount(t, ctx, r, Spec{ID: "parent", Needs: []string{"sample"}, Apply: func(c *Context) error {
				parentCtx = c
				if err := c.Defer("parent-cleanup", func(context.Context) error { add("parent"); close(parentCleaned); return nil }); err != nil {
					return err
				}
				var err error
				child, err = c.MountChild(Spec{ID: "child", Needs: []string{"sample"}, Apply: func(c *Context) error {
					return c.Go("late-setup", func(context.Context) error {
						err := c.Effect("late-result", func() (Disposer, error) {
							setupEntered <- c
							<-releaseSetup
							return func(cleanupCtx context.Context) error {
								cleanupCalls.Add(1)
								cleanupEntered <- cleanupCtx
								<-releaseCleanup
								value, err := c.Read("sample")
								if err != nil || value != token || c.lease.Load() != 2 {
									return errors.New("late cleanup lost its committed dependency or two actual leases")
								}
								add("late")
								return nil
							}, nil
						})
						results <- err
						if !errors.Is(err, ErrInactiveEffect) || !errors.Is(err, ErrInactive) {
							return errors.New("late Effect did not report both inactive sentinels")
						}
						if c.lease.Load() != 1 || c.released.Load() {
							return errors.New("Effect did not release exactly its own lease")
						}
						// Expected shutdown is handled here, not silently swallowed by Go.
						return nil
					})
				}})
				return err
			}})
			c := effectWait(t, ctx, setupEntered)
			if err := effectAwait(t, ctx, r, child); err != nil {
				t.Fatal(err)
			}
			if c.lease.Load() != 2 {
				t.Fatalf("setup must hold Go and Effect leases, got %d", c.lease.Load())
			}
			go func() { closeReturned <- r.Close(ctx) }()
			effectWait(t, ctx, c.Std().Done())
			effectWait(t, ctx, parentCtx.Std().Done())
			assertRetained := func(stage string) {
				t.Helper()
				if c.lease.Load() != 2 || c.released.Load() || parentCtx.released.Load() || providerCtx.released.Load() {
					t.Fatalf("%s released live work: leases=%d child=%v parent=%v provider=%v", stage, c.lease.Load(), c.released.Load(), parentCtx.released.Load(), providerCtx.released.Load())
				}
				if value, err := c.Read("sample"); err != nil || value != token {
					t.Fatalf("%s lost retained provider: %v %v", stage, value, err)
				}
				inspection, err := r.(Inspector).Inspect(ctx)
				if err != nil {
					t.Fatal(err)
				}
				reserved := false
				for _, p := range inspection.Provisions {
					if p.Owner == provider && p.Key == "sample" && p.Value == token {
						reserved = true
					}
				}
				if !reserved {
					t.Fatalf("%s removed the real retained provision", stage)
				}
				if snapshot := effectSnapshot(t, ctx, r, child); !reflect.DeepEqual(snapshot.Tracked, []string{"late-setup"}) || len(snapshot.Effects) != 0 {
					t.Fatalf("%s changed native work ownership or fabricated an effect slot: %+v", stage, snapshot)
				}
				mu.Lock()
				premature := append([]string(nil), cleanup...)
				mu.Unlock()
				// Oracle order: the parent's own effect (registered before the
				// child) may already have run while the child's late work is
				// held; the child's cleanup and the provider's must not have.
				for _, label := range premature {
					if label != "parent" {
						t.Fatalf("%s ran premature cleanup: %v", stage, premature)
					}
				}
				select {
				case err := <-closeReturned:
					t.Fatalf("%s completed Close while callbacks remained held: %v", stage, err)
				default:
				}
			}
			assertRetained("held setup")
			effectWait(t, ctx, parentCleaned)
			assertRetained("parent cleaned during child teardown")
			setupOnce.Do(func() { close(releaseSetup) })
			cleanupCtx := effectWait(t, ctx, cleanupEntered)
			if _, ok := cleanupCtx.Deadline(); !ok || cleanupCtx.Err() != nil || c.Std().Err() == nil {
				t.Fatalf("late cleanup did not receive a fresh native dispose deadline: cleanup=%v activation=%v", cleanupCtx.Err(), c.Std().Err())
			}
			assertRetained("held late cleanup")
			for _, id := range []uint64{child, parent, provider} {
				short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
				err := effectAwait(t, short, r, id)
				cancel()
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("fiber %d joined before late cleanup returned: %v", id, err)
				}
			}
			cleanupOnce.Do(func() { close(releaseCleanup) })
			if err := effectWait(t, ctx, results); !errors.Is(err, ErrInactiveEffect) || !errors.Is(err, ErrInactive) {
				t.Fatalf("late Effect result = %v", err)
			}
			if err := effectWait(t, ctx, closeReturned); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			got := append([]string(nil), cleanup...)
			mu.Unlock()
			if cleanupCalls.Load() != 1 || !reflect.DeepEqual(got, []string{"parent", "late", "provider"}) {
				t.Fatalf("parent/late/provider cleanup count and order = %d %v", cleanupCalls.Load(), got)
			}
			if c.lease.Load() != 0 || !c.released.Load() || !parentCtx.released.Load() || !providerCtx.released.Load() {
				t.Fatal("actual completion failed to release the child, parent, or provider")
			}
		})
	}
}

func TestEffectSetupPanicReleasesLeaseBeforeNativeUnwind(t *testing.T) {
	for name, makeRuntime := range effectCandidates() {
		t.Run(name, func(t *testing.T) {
			ctx, r := effectRuntime(t, makeRuntime)
			contexts := make(chan *Context, 1)
			var cleanup atomic.Int64
			id, err := r.Mount(ctx, Spec{ID: "panicking-effect", Apply: func(c *Context) error {
				contexts <- c
				return c.Effect("unreturned", func() (Disposer, error) {
					if err := c.Defer("actual-owned-resource", func(context.Context) error {
						if c.lease.Load() != 0 {
							return errors.New("setup panic stranded its Effect lease")
						}
						cleanup.Add(1)
						return nil
					}); err != nil {
						return nil, err
					}
					panic("effect setup exploded")
				})
			}})
			if err != nil {
				t.Fatal(err)
			}
			if err := effectAwait(t, ctx, r, id); err == nil || !strings.Contains(err.Error(), "effect setup exploded") {
				t.Fatalf("setup panic did not reach the real Apply boundary: %v", err)
			}
			c := effectWait(t, ctx, contexts)
			if cleanup.Load() != 1 || c.lease.Load() != 0 || !c.released.Load() {
				t.Fatalf("panic native unwind: cleanup=%d leases=%d released=%v", cleanup.Load(), c.lease.Load(), c.released.Load())
			}
			if snapshot := effectSnapshot(t, ctx, r, id); snapshot.State != Failed || len(snapshot.Effects) != 0 {
				t.Fatalf("panic retained an unreturned Effect slot: %+v", snapshot)
			}
		})
	}
}

func TestEffectArgumentsMissingHookAndInactiveContext(t *testing.T) {
	for name, makeRuntime := range effectCandidates() {
		t.Run(name, func(t *testing.T) {
			ctx, r := effectRuntime(t, makeRuntime)
			contexts := make(chan *Context, 1)
			var setups atomic.Int64
			setup := func() (Disposer, error) { setups.Add(1); return nil, nil }
			id := effectMount(t, ctx, r, Spec{ID: "effect-arguments", Apply: func(c *Context) error {
				contexts <- c
				if err := c.Effect("", setup); err == nil || errors.Is(err, ErrInactive) {
					return errors.New("empty Effect label was not a programming error")
				}
				if err := c.Effect("nil-setup", nil); err == nil || errors.Is(err, ErrInactive) {
					return errors.New("nil Effect setup was not a programming error")
				}
				// Remove only the bridge on this actual runtime-created activation;
				// no synthetic Context, fake runtime, or replacement ledger is used.
				native := c.cleanupFn
				if native == nil {
					return errors.New("candidate failed to install its native cleanup hook")
				}
				c.cleanupFn = nil
				err := c.Effect("no-native-hook", setup)
				c.cleanupFn = native
				if !errors.Is(err, ErrEffectUnavailable) || c.lease.Load() != 0 {
					return errors.New("unavailable hook invoked setup or stranded its lease")
				}
				return nil
			}})
			c := effectWait(t, ctx, contexts)
			if setups.Load() != 0 || len(effectSnapshot(t, ctx, r, id).Effects) != 0 {
				t.Fatal("invalid or unsupported Effect invoked setup or added a ledger slot")
			}
			if err := r.Dispose(ctx, id); err != nil {
				t.Fatal(err)
			}
			if err := effectAwait(t, ctx, r, id); err != nil {
				t.Fatal(err)
			}
			if err := c.Effect("inactive", setup); !errors.Is(err, ErrInactiveEffect) || !errors.Is(err, ErrInactive) {
				t.Fatalf("inactive Effect must match both native and public sentinels: %v", err)
			}
			// A refused registration is ErrInactiveEffect for Defer as well
			// (07-c4-plugin-sdk.md section 3.3: a registration on an unloading or
			// disposed fiber returns ErrInactiveEffect); it still matches ErrInactive.
			if err := c.Defer("inactive-defer", func(context.Context) error { return nil }); !errors.Is(err, ErrInactive) || !errors.Is(err, ErrInactiveEffect) {
				t.Fatalf("refused Defer must match both sentinels: %v", err)
			}
			if err := c.Effect("", setup); err == nil || errors.Is(err, ErrInactive) {
				t.Fatalf("invalid arguments must be validated before inactive admission: %v", err)
			}
			if setups.Load() != 0 || c.lease.Load() != 0 {
				t.Fatal("rejected Effect ran setup or leaked a real activation lease")
			}
		})
	}
}
