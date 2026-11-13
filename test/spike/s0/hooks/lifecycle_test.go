// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
	"github.com/mutex-solutions/aiops-platform/test/spike/s0/contextmodel"
)

func TestSynchronousCallbackLeaseDrainsBeforeEffects(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, factory)
			e, use := makeEvent(t, f)
			started, release, cleaned := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			b, id := f.mount(f.metadata("a", "a", ""), []Use{use}, func(binding Binding) error {
				if err := binding.Defer("resource", func(context.Context) error { close(cleaned); return nil }); err != nil {
					return err
				}
				_, err := e.On(binding, "outer", func(context.Context, int) (Decision, error) { close(started); <-release; return Decision{}, nil })
				return err
			})
			returned := make(chan error, 1)
			go func() { returned <- e.Emit(f.ctx, f.owner, contextmodel.Audience{}, 0) }()
			<-started
			if err := f.runtime.Dispose(f.ctx, id); err != nil {
				t.Fatal(err)
			}
			select {
			case <-b.Runtime.(*concurrency.Context).Std().Done():
			case <-f.ctx.Done():
				t.Fatal(f.ctx.Err())
			}
			if b.Runtime.Enter() {
				b.Runtime.Leave()
				t.Fatal("unloading owner accepted a lease")
			}
			select {
			case <-cleaned:
				t.Fatal("effect disposed underneath active callback")
			default:
			}
			unblock()
			if err := <-returned; err != nil {
				t.Fatal(err)
			}
			if err := f.runtime.Settle(f.ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case <-cleaned:
			default:
				t.Fatal("resource not cleaned")
			}
		})
	}
}

func TestCanceledAwaitRetainsActualFutureLease(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, factory)
			e, use := makeEvent(t, f)
			future, finish := NewCompletion()
			defer finish(Decision{}, nil)
			started, cleaned := make(chan struct{}), make(chan struct{})
			b, id := f.mount(f.metadata("a", "a", ""), []Use{use}, func(binding Binding) error {
				if err := binding.Defer("resource", func(context.Context) error { close(cleaned); return nil }); err != nil {
					return err
				}
				_, err := e.On(binding, "outer", func(context.Context, int) (Decision, error) { close(started); return Decision{Future: future}, nil })
				return err
			})
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			returned := make(chan error, 1)
			go func() { _, err := e.Serial(ctx, f.owner, contextmodel.Audience{}, 0); returned <- err }()
			<-started
			cancel()
			if err := <-returned; !errors.Is(err, context.Canceled) {
				t.Fatalf("await cancellation = %v", err)
			}
			if err := f.runtime.Dispose(f.ctx, id); err != nil {
				t.Fatal(err)
			}
			select {
			case <-b.Runtime.(*concurrency.Context).Std().Done():
			case <-f.ctx.Done():
				t.Fatal(f.ctx.Err())
			}
			select {
			case <-cleaned:
				t.Fatal("canceled Await falsely released running work")
			default:
			}
			finish(Decision{}, nil)
			if err := f.runtime.Settle(f.ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case <-cleaned:
			default:
				t.Fatal("resource not cleaned")
			}
		})
	}
}

func TestConcurrentRegistrationDisposalAndDispatch(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, factory)
			e, use := makeEvent(t, f)
			binding, id := f.mount(f.metadata("a", "a", ""), []Use{use}, nil)
			var calls atomic.Int64
			fn := func(context.Context, int) (Decision, error) { calls.Add(1); return Decision{}, nil }
			start := make(chan struct{})
			failures := make(chan error, 8)
			var pending sync.WaitGroup
			for range 4 {
				pending.Add(1)
				go func() {
					defer pending.Done()
					<-start
					for range 50 {
						dispose, err := e.On(binding, "outer", fn)
						if err != nil {
							if errors.Is(err, ErrInactive) || errors.Is(err, concurrency.ErrInactive) {
								continue
							}
							failures <- err
							return
						}
						if err := dispose(f.ctx); err != nil {
							failures <- err
							return
						}
					}
				}()
			}
			for range 3 {
				pending.Add(1)
				go func() {
					defer pending.Done()
					<-start
					for range 100 {
						if err := e.Emit(f.ctx, f.owner, contextmodel.Audience{}, 0); err != nil {
							failures <- err
							return
						}
					}
				}()
			}
			pending.Add(1)
			go func() {
				defer pending.Done()
				<-start
				if err := f.runtime.Dispose(f.ctx, id); err != nil {
					failures <- err
				}
			}()
			close(start)
			pending.Wait()
			close(failures)
			for err := range failures {
				t.Error(err)
			}
			if err := f.runtime.Settle(f.ctx); err != nil {
				t.Fatal(err)
			}
			before := calls.Load()
			if err := e.Emit(f.ctx, f.owner, contextmodel.Audience{}, 0); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != before || len(e.listeners.load()) != 0 {
				t.Fatal("disposed listeners remained callable or published")
			}
		})
	}
}

func TestListenerAndAudienceOutsideMutationMutex(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, factory)
			e, use := makeEvent(t, f)
			f.mount(f.metadata("a", "a", "A"), []Use{use}, func(binding Binding) error {
				original := binding.Defer
				binding.Defer = func(label string, dispose func(context.Context) error) error {
					if !e.listeners.mutation.TryLock() {
						return errors.New("lifecycle setup under mutation lock")
					}
					e.listeners.mutation.Unlock()
					return original(label, dispose)
				}
				_, err := e.On(binding, "outer", func(context.Context, int) (Decision, error) {
					if !e.listeners.mutation.TryLock() {
						return Decision{}, errors.New("listener under mutation lock")
					}
					e.listeners.mutation.Unlock()
					// Reentrant actual runtime inspection must not require a graph lock
					// already held by hook dispatch.
					_, err := f.runtime.Snapshots(f.ctx)
					return Decision{}, err
				})
				return err
			})
			audience, err := f.scope.Audience(f.owner.Metadata, func(value string) bool {
				if !e.listeners.mutation.TryLock() {
					t.Error("audience under mutation lock")
					return false
				}
				e.listeners.mutation.Unlock()
				return value == "A"
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := e.Emit(f.ctx, f.owner, audience, 0); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCompletionConcurrentSettlementAndSubscription(t *testing.T) {
	p, complete := NewCompletion()
	var callbacks atomic.Int64
	var pending sync.WaitGroup
	for range 20 {
		pending.Add(1)
		go func() { defer pending.Done(); p.OnSettled(func() { callbacks.Add(1) }) }()
	}
	for range 20 {
		pending.Add(1)
		go func() { defer pending.Done(); complete(Decision{Value: 0, Stop: true}, nil) }()
	}
	pending.Wait()
	value, err := p.Await(context.Background())
	if err != nil || !value.Stop || value.Value != 0 || callbacks.Load() != 20 {
		t.Fatalf("completion=%+v err=%v callbacks=%d", value, err, callbacks.Load())
	}
}
