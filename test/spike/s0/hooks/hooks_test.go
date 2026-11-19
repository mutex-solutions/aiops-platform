// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
	"github.com/mutex-solutions/aiops-platform/test/spike/s0/contextmodel"
)

// Every listener below is created by a real candidate's Mount/Apply and owns a
// real Defer effect. There is no fake lifecycle Context or unowned On helper.
var candidates = map[string]func() concurrency.Runtime{
	"reconciler": concurrency.NewReconciler,
	"graph-lock": concurrency.NewGraphLock,
	"kernel":     concurrency.NewKernel,
}

type fixture struct {
	t       testing.TB
	ctx     context.Context
	runtime concurrency.Runtime
	root    *contextmodel.Root
	key     contextmodel.AnyKey
	entries map[string]contextmodel.Plugin
	owner   Binding
	scope   contextmodel.Scope[string]
}

func newFixture(t testing.TB, makeRuntime func() concurrency.Runtime) *fixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	runtime := makeRuntime()
	t.Cleanup(func() {
		if err := runtime.Close(ctx); err != nil {
			t.Error(err)
		}
		cancel()
	})
	if gap := concurrency.UnsupportedGap(ctx, runtime); gap != "" {
		t.Skipf("UNSUPPORTED backend, not run, not a pass: %s", gap)
	}
	key := contextmodel.NewKey[int]("test-owner", 0, contextmodel.Classes(contextmodel.ProviderBound, contextmodel.Open)).Any()
	entries := make(map[string]contextmodel.Plugin)
	var registered []contextmodel.Plugin
	for _, id := range []string{"owner", "a", "b", "c", "d", "e", "other"} {
		entries[id] = contextmodel.NewEntry(id)
		registered = append(registered, entries[id])
	}
	root, err := contextmodel.NewRoot(contextmodel.RootSpec{Entries: registered, Keys: []contextmodel.AnyKey{key},
		Bindings: []contextmodel.Binding{{Key: key, Providers: []contextmodel.Plugin{entries["owner"]}}}})
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, ctx: ctx, runtime: runtime, root: root, key: key, entries: entries, scope: contextmodel.NewScope[string](key)}
	metadata, err := root.WithEntry(root.Context(), entries["owner"], contextmodel.Spec{ID: "owner", Provides: []contextmodel.AnyKey{key}})
	if err != nil {
		t.Fatal(err)
	}
	f.owner, _ = f.mount(metadata, nil, nil)
	return f
}

func (f *fixture) metadata(entry, id, scope string) *contextmodel.Context {
	f.t.Helper()
	parent := f.root.Context()
	var err error
	if scope != "" {
		parent, err = f.scope.With(f.owner.Metadata, scope)
	}
	if err != nil {
		f.t.Fatal(err)
	}
	metadata, err := f.root.WithEntry(parent, f.entries[entry], contextmodel.Spec{ID: id})
	if err != nil {
		f.t.Fatal(err)
	}
	return metadata
}

func (f *fixture) mount(metadata *contextmodel.Context, uses []Use, setup func(Binding) error) (Binding, uint64) {
	f.t.Helper()
	type outcome struct {
		binding Binding
		err     error
	}
	ready := make(chan outcome, 1)
	provides := make([]string, 0, len(metadata.Spec().Provides))
	for _, key := range metadata.Spec().Provides {
		provides = append(provides, key.Name())
	}
	id, err := f.runtime.Mount(f.ctx, concurrency.Spec{ID: metadata.Spec().ID, Provides: provides, Apply: func(c *concurrency.Context) error {
		for _, address := range provides {
			if err := c.Provide(address, struct{}{}); err != nil {
				return err
			}
		}
		binding := Binding{Metadata: metadata, Runtime: c, Uses: uses,
			Owns: func(key contextmodel.AnyKey) bool { return c.Owns(key.Name()) },
			Defer: func(label string, dispose func(context.Context) error) error {
				return c.Defer(label, concurrency.Disposer(dispose))
			}}
		var err error
		if setup != nil {
			err = setup(binding)
		}
		ready <- outcome{binding, err}
		return err
	}})
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.runtime.Settle(f.ctx); err != nil {
		f.t.Fatal(err)
	}
	select {
	case result := <-ready:
		if result.err != nil {
			f.t.Fatal(result.err)
		}
		return result.binding, id
	case <-f.ctx.Done():
		f.t.Fatal(f.ctx.Err())
	}
	return Binding{}, 0
}

func makeEvent(t testing.TB, f *fixture) (*Event[int], Use) {
	t.Helper()
	e, err := NewEvent[int](Definition{Name: "test/event", Owner: f.key, Stages: []string{"outer", "inner"}})
	if err != nil {
		t.Fatal(err)
	}
	use, err := e.Use("outer")
	if err != nil {
		t.Fatal(err)
	}
	return e, use
}

func TestEventOrderAndValidation(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, factory)
			e, use := makeEvent(t, f)
			inner, _ := e.Use("inner")
			var got []string
			for _, item := range []struct {
				entry, id, stage string
				use              Use
			}{
				{"c", "c", "outer", use}, {"b", "b", "inner", inner}, {"a", "a", "outer", use},
			} {
				f.mount(f.metadata(item.entry, item.id, ""), []Use{item.use}, func(b Binding) error {
					_, err := e.On(b, item.stage, func(context.Context, int) (Decision, error) {
						got = append(got, item.id)
						return Decision{Stop: true}, nil
					})
					return err
				})
			}
			// Equal stage+plugin IDs remain permitted and retain stable tie order.
			f.mount(f.metadata("a", "a", ""), []Use{use}, func(b Binding) error {
				_, err := e.On(b, "outer", func(context.Context, int) (Decision, error) { got = append(got, "a2"); return Decision{}, nil })
				return err
			})
			if err := e.Emit(f.ctx, f.owner, contextmodel.Audience{}, 7); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, []string{"a", "a2", "c", "b"}) {
				t.Fatalf("order = %v", got)
			}
			listener, _ := f.mount(f.metadata("d", "d", ""), nil, nil)
			fn := func(context.Context, int) (Decision, error) { return Decision{}, nil }
			if _, err := e.On(listener, "outer", fn); !errors.Is(err, ErrUndeclared) {
				t.Fatalf("undeclared = %v", err)
			}
			listener.Uses = []Use{use}
			if _, err := e.On(listener, "unknown", fn); !errors.Is(err, ErrStage) {
				t.Fatalf("stage = %v", err)
			}
			if err := e.Emit(f.ctx, listener, contextmodel.Audience{}, 0); !errors.Is(err, ErrOwner) {
				t.Fatalf("owner = %v", err)
			}
			listener.Defer = nil
			if _, err := e.On(listener, "outer", fn); !errors.Is(err, ErrBinding) {
				t.Fatalf("unowned = %v", err)
			}
			// A textual copy of a registered entry is not authority to provide owner.
			spoof := contextmodel.NewEntry(f.entries["owner"].Name())
			if _, err := f.root.WithEntry(f.root.Context(), spoof, contextmodel.Spec{ID: "owner", Provides: []contextmodel.AnyKey{f.key}}); !errors.Is(err, contextmodel.ErrEntry) {
				t.Fatalf("spoof = %v", err)
			}
		})
	}
}

func TestEmitNoAwaitAndFutureLease(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, factory)
			e, use := makeEvent(t, f)
			future, finish := NewCompletion()
			var got []string
			cleaned := make(chan struct{})
			listener, id := f.mount(f.metadata("a", "a", ""), []Use{use}, func(b Binding) error {
				if err := b.Defer("resource", func(context.Context) error { close(cleaned); return nil }); err != nil {
					return err
				}
				_, err := e.On(b, "outer", func(context.Context, int) (Decision, error) {
					got = append(got, "async-prefix")
					return Decision{Future: future}, nil
				})
				return err
			})
			defer finish(Decision{}, nil)
			f.mount(f.metadata("b", "b", ""), []Use{use}, func(b Binding) error {
				_, err := e.On(b, "outer", func(context.Context, int) (Decision, error) { got = append(got, "sync-second"); return Decision{}, nil })
				return err
			})
			if err := e.Emit(f.ctx, f.owner, contextmodel.Audience{}, 0); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, []string{"async-prefix", "sync-second"}) {
				t.Fatal(got)
			}
			if err := f.runtime.Dispose(f.ctx, id); err != nil {
				t.Fatal(err)
			}
			// Dispose acknowledges a target request, not completed cleanup. The
			// actual candidate context cancellation proves quiesce has started.
			select {
			case <-listener.Runtime.(*concurrency.Context).Std().Done():
			case <-f.ctx.Done():
				t.Fatal(f.ctx.Err())
			}
			select {
			case <-cleaned:
				t.Fatal("cleanup ran before asynchronous lease settled")
			default:
			}
			if _, err := e.On(listener, "outer", func(context.Context, int) (Decision, error) { return Decision{}, nil }); !errors.Is(err, ErrInactive) {
				t.Fatalf("inactive registration = %v", err)
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

func TestParallelAllSettledAndSerialBail(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, factory)
			e, use := makeEvent(t, f)
			first := errors.New("first failure")
			second := errors.New("second failure")
			future, finish := NewCompletion()
			started := make(chan struct{})
			f.mount(f.metadata("a", "a", ""), []Use{use}, func(b Binding) error {
				_, err := e.On(b, "outer", func(context.Context, int) (Decision, error) { return Decision{}, first })
				return err
			})
			f.mount(f.metadata("b", "b", ""), []Use{use}, func(b Binding) error {
				_, err := e.On(b, "outer", func(context.Context, int) (Decision, error) { close(started); return Decision{Future: future}, nil })
				return err
			})
			result := make(chan error, 1)
			go func() { result <- e.Parallel(f.ctx, f.owner, contextmodel.Audience{}, 0) }()
			<-started
			select {
			case err := <-result:
				t.Fatalf("not all-settled: %v", err)
			default:
			}
			finish(Decision{}, second)
			err := <-result
			if !errors.Is(err, first) || !errors.Is(err, second) || strings.Index(err.Error(), "first") > strings.Index(err.Error(), "second") {
				t.Fatalf("aggregate = %v", err)
			}
			// Emit propagates a synchronous error and never invokes a later listener.
			if err := e.Emit(f.ctx, f.owner, contextmodel.Audience{}, 0); !errors.Is(err, first) {
				t.Fatalf("emit = %v", err)
			}

			s, use := makeEvent(t, f)
			future, finish = NewCompletion()
			defer finish(Decision{}, nil)
			var later atomic.Int64
			f.mount(f.metadata("a", "a", ""), []Use{use}, func(b Binding) error {
				_, err := s.On(b, "outer", func(context.Context, int) (Decision, error) { return Decision{Future: future}, nil })
				return err
			})
			f.mount(f.metadata("b", "b", ""), []Use{use}, func(b Binding) error {
				_, err := s.On(b, "outer", func(context.Context, int) (Decision, error) { later.Add(1); return Decision{Value: 0, Stop: true}, nil })
				return err
			})
			f.mount(f.metadata("c", "c", ""), []Use{use}, func(b Binding) error {
				_, err := s.On(b, "outer", func(context.Context, int) (Decision, error) { later.Add(100); return Decision{}, nil })
				return err
			})
			bail, err := s.Bail(f.ctx, f.owner, contextmodel.Audience{}, 0)
			if err != nil || !bail.Stop || bail.Value != future || later.Load() != 0 {
				t.Fatalf("future bail = %+v, %v", bail, err)
			}
			type reply struct {
				value Decision
				err   error
			}
			serial := make(chan reply, 1)
			go func() {
				value, err := s.Serial(f.ctx, f.owner, contextmodel.Audience{}, 0)
				serial <- reply{value, err}
			}()
			finish(Decision{Value: false}, nil)
			out := <-serial
			if out.err != nil || !out.value.Stop || out.value.Value != 0 || later.Load() != 1 {
				t.Fatalf("serial = %+v, %v, later %d", out.value, out.err, later.Load())
			}
		})
	}
}

func TestExplicitBailValuesAndSerialFirstError(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, factory)
			for _, final := range []any{0, ""} {
				e, use := makeEvent(t, f)
				var later atomic.Int64
				f.mount(f.metadata("a", "a", ""), []Use{use}, func(binding Binding) error {
					_, err := e.On(binding, "outer", func(context.Context, int) (Decision, error) { return Decision{Value: false}, nil })
					return err
				})
				f.mount(f.metadata("b", "b", ""), []Use{use}, func(binding Binding) error {
					_, err := e.On(binding, "outer", func(context.Context, int) (Decision, error) { return Decision{Value: final, Stop: true}, nil })
					return err
				})
				f.mount(f.metadata("c", "c", ""), []Use{use}, func(binding Binding) error {
					_, err := e.On(binding, "outer", func(context.Context, int) (Decision, error) { later.Add(1); return Decision{}, nil })
					return err
				})
				for _, mode := range []func(context.Context, Binding, contextmodel.Audience, int) (Decision, error){e.Bail, e.Serial} {
					value, err := mode(f.ctx, f.owner, contextmodel.Audience{}, 0)
					if err != nil || !value.Stop || value.Value != final || later.Load() != 0 {
						t.Fatalf("bail value = %+v err=%v later=%d", value, err, later.Load())
					}
				}
			}
			e, use := makeEvent(t, f)
			failure := errors.New("serial first error")
			future, finish := NewCompletion()
			finish(Decision{}, failure)
			var later atomic.Int64
			f.mount(f.metadata("a", "a", ""), []Use{use}, func(binding Binding) error {
				_, err := e.On(binding, "outer", func(context.Context, int) (Decision, error) { return Decision{Future: future}, nil })
				return err
			})
			f.mount(f.metadata("b", "b", ""), []Use{use}, func(binding Binding) error {
				_, err := e.On(binding, "outer", func(context.Context, int) (Decision, error) { later.Add(1); return Decision{}, nil })
				return err
			})
			if _, err := e.Serial(f.ctx, f.owner, contextmodel.Audience{}, 0); !errors.Is(err, failure) || later.Load() != 0 {
				t.Fatalf("serial error=%v later=%d", err, later.Load())
			}
		})
	}
}

func TestAudienceSelectionIsFrozenBeforeFirstListener(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, factory)
			e, use := makeEvent(t, f)
			var allow atomic.Bool
			allow.Store(true)
			audience, err := f.scope.Audience(f.owner.Metadata, func(string) bool { return allow.Load() })
			if err != nil {
				t.Fatal(err)
			}
			var first, second int
			f.mount(f.metadata("a", "a", "scope"), []Use{use}, func(binding Binding) error {
				_, err := e.On(binding, "outer", func(context.Context, int) (Decision, error) {
					first++
					allow.Store(false)
					return Decision{}, nil
				})
				return err
			})
			f.mount(f.metadata("b", "b", "scope"), []Use{use}, func(binding Binding) error {
				_, err := e.On(binding, "outer", func(context.Context, int) (Decision, error) {
					second++
					return Decision{}, nil
				})
				return err
			})
			if err := e.Emit(f.ctx, f.owner, audience, 0); err != nil {
				t.Fatal(err)
			}
			if first != 1 || second != 1 {
				t.Fatalf("audience reevaluated after dispatch started: first=%d second=%d", first, second)
			}
			if err := e.Emit(f.ctx, f.owner, audience, 0); err != nil {
				t.Fatal(err)
			}
			if first != 1 || second != 1 {
				t.Fatal("new dispatch did not reevaluate its own audience snapshot")
			}
		})
	}
}

func TestAudienceAcrossModes(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, factory)
			e, use := makeEvent(t, f)
			var a, b, unscoped atomic.Int64
			for _, item := range []struct {
				entry, scope string
				calls        *atomic.Int64
			}{{"a", "A", &a}, {"b", "B", &b}, {"c", "", &unscoped}} {
				f.mount(f.metadata(item.entry, item.entry, item.scope), []Use{use}, func(binding Binding) error {
					_, err := e.On(binding, "outer", func(context.Context, int) (Decision, error) { item.calls.Add(1); return Decision{}, nil })
					return err
				})
			}
			audience, err := f.scope.Audience(f.owner.Metadata, func(value string) bool { return value == "A" })
			if err != nil {
				t.Fatal(err)
			}
			for _, dispatch := range []func(contextmodel.Audience) error{
				func(a contextmodel.Audience) error { return e.Emit(f.ctx, f.owner, a, 0) },
				func(a contextmodel.Audience) error { return e.Parallel(f.ctx, f.owner, a, 0) },
				func(a contextmodel.Audience) error { _, err := e.Serial(f.ctx, f.owner, a, 0); return err },
				func(a contextmodel.Audience) error { _, err := e.Bail(f.ctx, f.owner, a, 0); return err },
			} {
				if err := dispatch(audience); err != nil {
					t.Fatal(err)
				}
				if err := dispatch(contextmodel.Audience{}); err != nil {
					t.Fatal(err)
				}
			}
			if a.Load() != 4 || b.Load() != 0 || unscoped.Load() != 4 {
				t.Fatalf("A=%d B=%d unscoped=%d", a.Load(), b.Load(), unscoped.Load())
			}
		})
	}
}

func TestCopyOnWriteReentrantMutationAndPanic(t *testing.T) {
	for name, factory := range candidates {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, factory)
			e, use := makeEvent(t, f)
			binding, _ := f.mount(f.metadata("a", "a", ""), []Use{use}, nil)
			var oldCalls, newCalls atomic.Int64
			var old Disposer
			var once sync.Once
			_, err := e.On(binding, "outer", func(context.Context, int) (Decision, error) {
				// Successful TryLock proves callbacks run outside the mutation mutex.
				if !e.listeners.mutation.TryLock() {
					return Decision{}, errors.New("callback holds mutation mutex")
				}
				e.listeners.mutation.Unlock()
				once.Do(func() {
					_ = old(f.ctx)
					_, err := e.On(binding, "outer", func(context.Context, int) (Decision, error) { newCalls.Add(1); return Decision{}, nil })
					if err != nil {
						t.Error(err)
					}
				})
				return Decision{}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			old, err = e.On(binding, "outer", func(context.Context, int) (Decision, error) { oldCalls.Add(1); return Decision{}, nil })
			if err != nil {
				t.Fatal(err)
			}
			if err := e.Emit(f.ctx, f.owner, contextmodel.Audience{}, 0); err != nil {
				t.Fatal(err)
			}
			if newCalls.Load() != 0 || oldCalls.Load() != 1 {
				t.Fatal("current snapshot changed after manual disposal/registration")
			}
			if err := e.Emit(f.ctx, f.owner, contextmodel.Audience{}, 0); err != nil {
				t.Fatal(err)
			}
			if newCalls.Load() != 1 {
				t.Fatal("new listener missing from next snapshot")
			}
			panicCause := errors.New("panic cause")
			_, err = e.On(binding, "outer", func(context.Context, int) (Decision, error) { panic(panicCause) })
			if err != nil {
				t.Fatal(err)
			}
			err = e.Emit(f.ctx, f.owner, contextmodel.Audience{}, 0)
			var panicErr *PanicError
			if !errors.Is(err, panicCause) || !errors.As(err, &panicErr) {
				t.Fatalf("panic = %v", err)
			}
		})
	}
}
