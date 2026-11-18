// SPDX-License-Identifier: Apache-2.0

package concurrency

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// joinOwner waits until the reconciler owner goroutine of r has finished (a
// graph-lock runtime has no such goroutine). Close can return before that: it
// returns a lifecycle diagnostic or a bounded wait's error while the owner is
// still alive. A test must not return while its owner runs, or the owner leaks
// into the next test. done closes as the owner returns; the few instructions
// between that close and the frame disappearing cannot be joined, so a test
// that inspects goroutine stacks must scope them to its own bubble
// (bubbleStacks) rather than rely on this join alone.
func joinOwner(ctx context.Context, r Runtime) error {
	rr, ok := r.(*reconcilerRuntime)
	if !ok {
		return nil
	}
	select {
	case <-rr.done:
		return nil
	default:
	}
	select {
	case <-rr.done:
		return nil
	case <-ctx.Done():
		return errors.New("reconciler owner still running after Close: " + ctx.Err().Error())
	}
}

// closeRuntime is Close followed by joinOwner; it returns Close's error first.
func closeRuntime(ctx context.Context, r Runtime) error {
	return errors.Join(r.Close(ctx), joinOwner(ctx, r))
}

func TestSharedAvailabilityContract(t *testing.T) {
	for name, makeRuntime := range map[string]func() Runtime{"reconciler": NewReconciler, "graph-lock": NewGraphLock} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r := makeRuntime()
			defer closeRuntime(ctx, r)
			notifier, ok := r.(Notifier)
			if !ok {
				t.Fatal("candidate does not support explicit availability notification")
			}
			var allow, loads, checks atomic.Int64
			_, err := r.Mount(ctx, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Context) error {
				return c.Provide("sample", "value")
			}})
			if err != nil {
				t.Fatal(err)
			}
			id, err := r.Mount(ctx, Spec{ID: "checked", Needs: []string{"sample"}, Check: func(c *Context) bool {
				checks.Add(1)
				// Reentrant inspection must not execute inside the state-owner lock.
				if _, err := r.Snapshots(ctx); err != nil {
					return false
				}
				if c.Enter() || !errors.Is(c.Defer("forbidden", nil), ErrInactive) || !errors.Is(c.Dispose(), ErrInactive) {
					return false
				}
				value, err := c.Read("sample")
				return err == nil && value == "value" && allow.Load() == 1
			}, Apply: func(*Context) error { loads.Add(1); return nil }})
			if err != nil {
				t.Fatal(err)
			}
			state := func(want State) {
				t.Helper()
				if err := r.Settle(ctx); err != nil {
					t.Fatal(err)
				}
				list, err := r.Snapshots(ctx)
				if err != nil {
					t.Fatal(err)
				}
				for _, s := range list {
					if s.ID == id {
						if s.State != want {
							t.Fatalf("state = %s, want %s", s.State, want)
						}
						return
					}
				}
				t.Fatal("missing checked fiber")
			}
			state(Pending)
			if loads.Load() != 0 || checks.Load() == 0 {
				t.Fatal("denied check ran Apply or was never evaluated")
			}
			allow.Store(1)
			if err := notifier.Notify(ctx, "sample"); err != nil {
				t.Fatal(err)
			}
			state(Active)
			if loads.Load() != 1 {
				t.Fatal("allowed check did not activate exactly once")
			}
			if err := notifier.Notify(ctx, "sample"); err != nil {
				t.Fatal(err)
			}
			state(Active)
			if loads.Load() != 1 {
				t.Fatal("unchanged availability restarted activation")
			}
			allow.Store(0)
			if err := notifier.Notify(ctx, "sample"); err != nil {
				t.Fatal(err)
			}
			state(Pending)
		})
	}
}

func TestSharedConfigRequestPrefixDoesNotJoinOwnTeardown(t *testing.T) {
	for name, makeRuntime := range map[string]func() Runtime{"reconciler": NewReconciler, "graph-lock": NewGraphLock} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r := makeRuntime()
			defer closeRuntime(ctx, r)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			var applied atomic.Int64
			cell := NewConfiguration(int64(1), ConfigHooks{})
			id, err := r.Mount(ctx, Spec{ID: "configured", Config: cell, ApplyEffect: func(c *Context) (Disposer, error) {
				applied.Store(c.Config().(int64))
				return func(context.Context) error {
					once.Do(func() { close(entered) })
					<-release
					return nil
				}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			requester, ok := r.(ConfigRequester)
			if !ok {
				t.Fatal("native config request API missing")
			}
			first, err := requester.RequestUpdateConfig(ctx, id, int64(2), false)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("request did not enter real teardown")
			}
			second, err := requester.RequestUpdateConfig(ctx, id, int64(3), false)
			if err != nil {
				t.Fatal(err)
			}
			if cell.Snapshot().Raw != int64(3) || applied.Load() != 1 {
				t.Fatal("request did not synchronously retain latest raw while old cleanup remained held")
			}
			close(release)
			if err := first.Await(ctx); err != nil {
				t.Fatal(err)
			}
			if err := second.Await(ctx); err != nil {
				t.Fatal(err)
			}
			if applied.Load() != 3 {
				t.Fatal("latest native configuration was not applied after coalesced teardown")
			}
		})
	}
}

func TestSharedLifecycleContract(t *testing.T) {
	for name, makeRuntime := range map[string]func() Runtime{"reconciler": NewReconciler, "graph-lock": NewGraphLock} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r := makeRuntime()
			defer closeRuntime(ctx, r)
			var mu sync.Mutex
			var cleanup []string
			add := func(label string) {
				mu.Lock()
				cleanup = append(cleanup, label)
				mu.Unlock()
			}
			var providerCtx, consumerCtx *Context
			provider, err := r.Mount(ctx, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Context) error {
				providerCtx = c
				if err := c.Defer("provider", func(context.Context) error { add("provider"); return nil }); err != nil {
					return err
				}
				return c.Provide("sample", "observed")
			}})
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := r.Mount(ctx, Spec{ID: "consumer", Needs: []string{"sample"}, Apply: func(c *Context) error {
				consumerCtx = c
				if _, err := c.Read("sample"); err != nil {
					return err
				}
				return c.Defer("consumer", func(context.Context) error {
					value, err := c.Read("sample")
					if err != nil || value != "observed" {
						return errors.New("committed injection lost before consumer cleanup")
					}
					add("consumer")
					return nil
				})
			}})
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			snapshots, err := r.Snapshots(ctx)
			if err != nil || len(snapshots) != 2 {
				t.Fatalf("snapshots = %v %v", snapshots, err)
			}
			for _, s := range snapshots {
				if s.State != Active || s.Revision != 0 {
					t.Fatalf("initial state/revision = %+v", s)
				}
			}
			if providerCtx == nil || consumerCtx == nil {
				t.Fatal("activation did not publish fixture contexts")
			}
			if !consumerCtx.Enter() {
				t.Fatal("active consumer refused lease")
			}
			if err := r.Dispose(ctx, provider); err != nil {
				t.Fatalf("disposal request failed: %v", err)
			}
			select {
			case <-consumerCtx.Std().Done():
			case <-ctx.Done():
				t.Fatal("consumer was not quiesced")
			}
			if consumerCtx.Enter() {
				t.Fatal("quiescent consumer accepted new lease")
			}
			mu.Lock()
			premature := len(cleanup)
			mu.Unlock()
			if premature != 0 {
				t.Fatal("cleanup ran while contribution lease remained open")
			}
			consumerCtx.Leave()
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			got := append([]string(nil), cleanup...)
			mu.Unlock()
			if len(got) != 2 || got[0] != "consumer" || got[1] != "provider" {
				t.Fatalf("dependent/provider disposal order = %v", got)
			}
			if _, err := consumerCtx.Read("sample"); !errors.Is(err, ErrInactive) {
				t.Fatalf("released injection read = %v", err)
			}
			if err := r.Dispose(ctx, consumer); err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			events, err := r.Events(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for i, event := range events {
				if event.Sequence != uint64(i+1) {
					t.Fatalf("event total order at %d = %d", i, event.Sequence)
				}
			}
		})
	}
}
