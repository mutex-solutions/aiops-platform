// SPDX-License-Identifier: Apache-2.0
// Lifecycle semantics informed by Cordis, copyright Shigma and DeepSeek.
// The adapted-source MIT notices are preserved in NOTICE.

package concurrency

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func trackedCandidates() map[string]func() Runtime {
	return map[string]func() Runtime{"reconciler": NewReconciler, "graph-lock": NewGraphLock}
}

func trackedRuntime(t *testing.T, makeRuntime func() Runtime) (context.Context, Runtime) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	r := makeRuntime()
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		// A failed activation may make Close report its sticky failure. Tests
		// assert that failure explicitly instead of treating it as cleanup loss.
		_ = r.Close(closeCtx)
		cancel()
	})
	return ctx, r
}

func trackedWait[T any](t *testing.T, ctx context.Context, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-ctx.Done():
		t.Fatalf("tracked-work barrier: %v", ctx.Err())
		var zero T
		return zero
	}
}

func trackedAwait(t *testing.T, ctx context.Context, r Runtime, id uint64) error {
	t.Helper()
	waiter, ok := r.(Waiter)
	if !ok {
		t.Fatal("native fiber join unavailable")
	}
	return waiter.AwaitFiber(ctx, id)
}

func trackedMount(t *testing.T, ctx context.Context, r Runtime, spec Spec) uint64 {
	t.Helper()
	id, err := r.Mount(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := trackedAwait(t, ctx, r, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func trackedSnapshot(t *testing.T, ctx context.Context, r Runtime, id uint64) Snapshot {
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

// Instrument only completion delivery on an actual runtime-created Context.
// Every registration and completion still goes through its original native hook;
// this barrier observes owner acknowledgement without timing-based polling.
func trackedCompletions(c *Context, capacity int) <-chan error {
	completed := make(chan error, capacity)
	native := c.trackedStart
	c.trackedStart = func(label string) (func(error), error) {
		done, err := native(label)
		if err != nil {
			return nil, err
		}
		return func(err error) {
			done(err)
			completed <- err
		}, nil
	}
	return completed
}

func TestTrackedGoDrainsActualWorkBeforeEffectsAndDependencies(t *testing.T) {
	for name, makeRuntime := range trackedCandidates() {
		t.Run(name, func(t *testing.T) {
			ctx, r := trackedRuntime(t, makeRuntime)
			release := make(chan struct{})
			var releaseOnce sync.Once
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			started, canceled := make(chan context.Context, 1), make(chan struct{})
			contexts := make(chan *Context, 1)
			var workerExited atomic.Bool
			var mu sync.Mutex
			var cleanup []string
			addCleanup := func(label string) {
				mu.Lock()
				cleanup = append(cleanup, label)
				mu.Unlock()
			}
			token := &struct{ Value string }{"retained dependency"}
			provider := trackedMount(t, ctx, r, Spec{ID: "provider", Provides: []string{"sample"}, Apply: func(c *Context) error {
				if err := c.Defer("provider-cleanup", func(context.Context) error { addCleanup("provider"); return nil }); err != nil {
					return err
				}
				return c.Provide("sample", token)
			}})
			consumer := trackedMount(t, ctx, r, Spec{ID: "worker", Needs: []string{"sample"}, Apply: func(c *Context) error {
				contexts <- c
				if err := c.Defer("worker-cleanup", func(context.Context) error {
					if !workerExited.Load() {
						return errors.New("effect released before actual worker exit")
					}
					value, err := c.Read("sample")
					if err != nil || value != token {
						return errors.New("dependency lost before worker cleanup")
					}
					addCleanup("worker")
					return nil
				}); err != nil {
					return err
				}
				return c.Go("blocked-work", func(std context.Context) error {
					inspection, err := r.(Inspector).Inspect(ctx)
					if err != nil {
						return err
					}
					registered := false
					for _, snapshot := range inspection.Fibers {
						if snapshot.ID == c.fiber && reflect.DeepEqual(snapshot.Tracked, []string{"blocked-work"}) {
							registered = true
						}
					}
					if !registered {
						return errors.New("worker entered before native ledger registration")
					}
					started <- std
					<-std.Done()
					close(canceled)
					<-release
					value, err := c.Read("sample")
					if err != nil || value != token {
						return errors.New("worker lost its committed dependency before exiting")
					}
					workerExited.Store(true)
					return fmt.Errorf("handled shutdown: %w", std.Err())
				})
			}})
			c := trackedWait(t, ctx, contexts)
			if std := trackedWait(t, ctx, started); std != c.Std() {
				t.Fatal("worker did not receive the activation's standard context")
			}
			if s := trackedSnapshot(t, ctx, r, consumer); s.State != Active || !reflect.DeepEqual(s.Tracked, []string{"blocked-work"}) {
				t.Fatalf("active native worker ledger = %+v", s)
			}
			// Both public snapshot paths must copy native ledger labels.
			snapshots, err := r.Snapshots(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, snapshot := range snapshots {
				if snapshot.ID == consumer {
					snapshot.Tracked[0] = "tampered snapshot"
				}
			}
			if s := trackedSnapshot(t, ctx, r, consumer); !reflect.DeepEqual(s.Tracked, []string{"blocked-work"}) {
				t.Fatalf("snapshot mutation rewrote native work ownership: %+v", s)
			}
			if err := r.Dispose(ctx, provider); err != nil {
				t.Fatal(err)
			}
			trackedWait(t, ctx, canceled)
			if c.Enter() {
				c.Leave()
				t.Fatal("quiescent worker activation accepted a lease")
			}
			var extra atomic.Bool
			if err := c.Go("late-work", func(context.Context) error { extra.Store(true); return nil }); !errors.Is(err, ErrInactive) {
				t.Fatalf("work after quiescence = %v", err)
			}
			if extra.Load() || c.lease.Load() != 1 {
				t.Fatalf("rejected start launched work or leaked lease: ran=%v leases=%d", extra.Load(), c.lease.Load())
			}
			mu.Lock()
			premature := append([]string(nil), cleanup...)
			mu.Unlock()
			if len(premature) != 0 || c.released.Load() {
				t.Fatalf("cancellation was mistaken for actual exit: cleanup=%v released=%v", premature, c.released.Load())
			}
			if s := trackedSnapshot(t, ctx, r, consumer); !reflect.DeepEqual(s.Tracked, []string{"blocked-work"}) {
				t.Fatalf("blocked worker disappeared during drain: %+v", s)
			}
			short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
			if err := trackedAwait(t, short, r, provider); !errors.Is(err, context.DeadlineExceeded) {
				cancel()
				t.Fatalf("provider join did not retain blocked dependent: %v", err)
			}
			cancel()
			releaseOnce.Do(func() { close(release) })
			if err := trackedAwait(t, ctx, r, provider); err != nil {
				t.Fatal(err)
			}
			if err := trackedAwait(t, ctx, r, consumer); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			got := append([]string(nil), cleanup...)
			mu.Unlock()
			if !reflect.DeepEqual(got, []string{"worker", "provider"}) {
				t.Fatalf("actual worker/dependent/provider release order = %v", got)
			}
			if s := trackedSnapshot(t, ctx, r, consumer); len(s.Tracked) != 0 || s.State != Pending || s.Error != "" {
				t.Fatalf("expected canceled work poisoned activation or retained ledger: %+v", s)
			}
			if c.lease.Load() != 0 || !c.released.Load() {
				t.Fatalf("actual exit failed to release lease: leases=%d released=%v", c.lease.Load(), c.released.Load())
			}
		})
	}
}

func TestTrackedGoDuplicateLabelsRetireIndividuallyWhileActive(t *testing.T) {
	for name, makeRuntime := range trackedCandidates() {
		t.Run(name, func(t *testing.T) {
			ctx, r := trackedRuntime(t, makeRuntime)
			releases := []chan struct{}{make(chan struct{}), make(chan struct{})}
			var once [2]sync.Once
			t.Cleanup(func() {
				for i := range releases {
					once[i].Do(func() { close(releases[i]) })
				}
			})
			var completed <-chan error
			started := make(chan struct{}, 2)
			id := trackedMount(t, ctx, r, Spec{ID: "duplicate-labels", Apply: func(c *Context) error {
				completed = trackedCompletions(c, 2)
				for _, release := range releases {
					if err := c.Go("same-label", func(context.Context) error {
						started <- struct{}{}
						<-release
						return nil
					}); err != nil {
						return err
					}
				}
				return nil
			}})
			trackedWait(t, ctx, started)
			trackedWait(t, ctx, started)
			if s := trackedSnapshot(t, ctx, r, id); s.State != Active || !reflect.DeepEqual(s.Tracked, []string{"same-label", "same-label"}) {
				t.Fatalf("same labels collapsed distinct native workers: %+v", s)
			}
			for i := range releases {
				once[i].Do(func() { close(releases[i]) })
				if err := trackedWait(t, ctx, completed); err != nil {
					t.Fatal(err)
				}
				if s := trackedSnapshot(t, ctx, r, id); s.State != Active || len(s.Tracked) != 1-i {
					t.Fatalf("completion %d did not retire exactly one live worker: %+v", i, s)
				}
			}
		})
	}
}

func TestTrackedGoFailureUnloadsActiveOwnerAndDependents(t *testing.T) {
	failure := errors.New("tracked worker rejected")
	cases := []struct {
		name string
		run  func() error
		want error
		text string
	}{
		{"error", func() error { return failure }, failure, failure.Error()},
		{"panic", func() error { panic("tracked worker exploded") }, nil, "tracked goroutine \"failing-work\" panic: tracked worker exploded"},
		{"uncanceled-context-error", func() error { return fmt.Errorf("not shutdown: %w", context.Canceled) }, context.Canceled, "not shutdown"},
	}
	for name, makeRuntime := range trackedCandidates() {
		for _, test := range cases {
			t.Run(name+"/"+test.name, func(t *testing.T) {
				ctx, r := trackedRuntime(t, makeRuntime)
				fail, releaseSibling := make(chan struct{}), make(chan struct{})
				var failOnce, siblingOnce sync.Once
				t.Cleanup(func() {
					failOnce.Do(func() { close(fail) })
					siblingOnce.Do(func() { close(releaseSibling) })
				})
				contexts, dependents := make(chan *Context, 1), make(chan *Context, 1)
				var mu sync.Mutex
				var cleanup []string
				addCleanup := func(label string) {
					mu.Lock()
					cleanup = append(cleanup, label)
					mu.Unlock()
				}
				id := trackedMount(t, ctx, r, Spec{ID: "owner", Provides: []string{"sample"}, Apply: func(c *Context) error {
					contexts <- c
					if err := c.Defer("owner-cleanup", func(context.Context) error { addCleanup("owner"); return nil }); err != nil {
						return err
					}
					if err := c.Provide("sample", "value"); err != nil {
						return err
					}
					if err := c.Go("sibling-work", func(std context.Context) error {
						<-std.Done()
						<-releaseSibling
						return std.Err()
					}); err != nil {
						return err
					}
					return c.Go("failing-work", func(context.Context) error { <-fail; return test.run() })
				}})
				dependent := trackedMount(t, ctx, r, Spec{ID: "dependent", Needs: []string{"sample"}, Apply: func(c *Context) error {
					dependents <- c
					return c.Defer("dependent-cleanup", func(context.Context) error { addCleanup("dependent"); return nil })
				}})
				c, dependentContext := trackedWait(t, ctx, contexts), trackedWait(t, ctx, dependents)
				failOnce.Do(func() { close(fail) })
				trackedWait(t, ctx, c.Std().Done())
				trackedWait(t, ctx, dependentContext.Std().Done())
				if c.released.Load() {
					t.Fatal("failed activation released a still-running sibling")
				}
				if s := trackedSnapshot(t, ctx, r, id); !reflect.DeepEqual(s.Tracked, []string{"sibling-work"}) {
					t.Fatalf("failure did not retire only the actual exited worker: %+v", s)
				}
				siblingOnce.Do(func() { close(releaseSibling) })
				err := trackedAwait(t, ctx, r, id)
				if err == nil || (test.want != nil && !errors.Is(err, test.want)) || !strings.Contains(err.Error(), test.text) {
					t.Fatalf("native failed join = %v, want cause %v containing %q", err, test.want, test.text)
				}
				if err := trackedAwait(t, ctx, r, dependent); err != nil {
					t.Fatal(err)
				}
				if s := trackedSnapshot(t, ctx, r, id); s.State != Failed || len(s.Tracked) != 0 || !strings.Contains(s.Error, test.text) {
					t.Fatalf("worker did not produce native sticky FAILED: %+v", s)
				}
				if s := trackedSnapshot(t, ctx, r, dependent); s.State != Pending || len(s.Effects) != 0 {
					t.Fatalf("dependent retained failed owner's activation: %+v", s)
				}
				mu.Lock()
				got := append([]string(nil), cleanup...)
				mu.Unlock()
				if !reflect.DeepEqual(got, []string{"dependent", "owner"}) {
					t.Fatalf("failed activation cleanup order = %v", got)
				}
			})
		}
	}
}

func TestTrackedGoRejectsMissingBridgeAndQuiescenceRaceWithoutLeakingLease(t *testing.T) {
	for name, makeRuntime := range trackedCandidates() {
		t.Run(name, func(t *testing.T) {
			ctx, r := trackedRuntime(t, makeRuntime)
			contexts := make(chan *Context, 1)
			id := trackedMount(t, ctx, r, Spec{ID: "start-guards", Apply: func(c *Context) error { contexts <- c; return nil }})
			c := trackedWait(t, ctx, contexts)
			var ran atomic.Bool
			run := func(context.Context) error { ran.Store(true); return nil }
			if err := c.Go("", run); err == nil || errors.Is(err, ErrInactive) {
				t.Fatalf("empty label was not rejected as an argument error: %v", err)
			}
			if err := c.Go("nil-callback", nil); err == nil || errors.Is(err, ErrInactive) {
				t.Fatalf("nil callback was not rejected as an argument error: %v", err)
			}
			native := c.trackedStart
			c.trackedStart = nil
			if err := c.Go("unsupported", run); !errors.Is(err, ErrTrackedUnavailable) {
				t.Fatalf("missing native hook silently launched untracked work: %v", err)
			}
			c.trackedStart = func(label string) (func(error), error) {
				if c.lease.Load() != 1 {
					return nil, errors.New("native start was not protected by a lease")
				}
				if err := r.Dispose(ctx, id); err != nil {
					return nil, err
				}
				return native(label)
			}
			if err := c.Go("lost-start-race", run); !errors.Is(err, ErrInactive) {
				t.Fatalf("native hook accepted a start after quiescence: %v", err)
			}
			c.trackedStart = native
			if err := trackedAwait(t, ctx, r, id); err != nil {
				t.Fatal(err)
			}
			if ran.Load() || c.lease.Load() != 0 {
				t.Fatalf("failed start launched or leaked work: ran=%v leases=%d", ran.Load(), c.lease.Load())
			}
			if err := c.Go("after-disposal", run); !errors.Is(err, ErrInactive) {
				t.Fatalf("disposed context accepted work: %v", err)
			}
			if s := trackedSnapshot(t, ctx, r, id); s.State != Disposed || len(s.Tracked) != 0 {
				t.Fatalf("rejected start retained native work: %+v", s)
			}
		})
	}
}

func TestRunTrackedCancellationPolicy(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	failure := errors.New("independent failure")
	for _, test := range []struct {
		name string
		ctx  context.Context
		err  error
		want error
	}{
		{"nil", canceled, nil, nil},
		{"expected-cancel", canceled, context.Canceled, nil},
		{"wrapped-expected-cancel", canceled, fmt.Errorf("shutdown: %w", context.Canceled), nil},
		{"uncanceled-error", context.Background(), context.Canceled, context.Canceled},
		{"deadline-is-not-cancel", canceled, context.DeadlineExceeded, context.DeadlineExceeded},
		{"independent-failure", canceled, failure, failure},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := runTracked(test.ctx, "policy", func(context.Context) error { return test.err }); got != test.want {
				t.Fatalf("worker error normalization = %v, want %v", got, test.want)
			}
		})
	}
}
