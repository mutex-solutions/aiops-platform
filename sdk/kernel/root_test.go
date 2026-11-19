// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// The helpers below are shared by key_test.go, context_test.go and this file.

func frontConfig() Config {
	c := testConfig()
	return Config{
		LoadDeadline: c.loadDeadline, DrainDeadline: c.drainDeadline, DisposeDeadline: c.disposeDeadline,
		ArchiveLimit: c.archiveLimit, EventWindow: c.eventWindow, ObserverQueue: c.observerQueue,
		OwnerInbox: c.ownerInbox, TerminalLimit: c.terminalLimit,
	}
}

func frontCtx(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// newKernel is a kernel with both binding maps off.
func newKernel(t *testing.T) *Root { return NewForTest(t, frontConfig()) }

// newBound is a kernel with the given binding maps, closed when the test ends.
func newBound(t *testing.T, providers, consumers Bindings) *Root {
	t.Helper()
	cfg := frontConfig()
	cfg.Providers, cfg.Consumers = providers, consumers
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(context.Background()); err != nil {
			t.Logf("close: %v", err)
		}
	})
	return r
}

// plug builds a Plugin whose Apply runs apply; each call has its own identity.
func plug(id string, inject []Dep, provides []AnyKey, apply func(*Context) error) Plugin {
	return Define(Plugin{ID: id, Inject: inject, Provides: provides, Apply: func(c *Context, _ any) error {
		if apply == nil {
			return nil
		}
		return apply(c)
	}})
}

// await waits for f and returns its state and startup error.
func await(t testing.TB, f *Fiber) (State, error) {
	t.Helper()
	ctx := frontCtx(t)
	err := f.Await(ctx)
	s, serr := f.State(ctx)
	if serr != nil {
		t.Fatal(serr)
	}
	return s, err
}

// want waits for f to leave its transition and checks its state.
func want(t testing.TB, f *Fiber, state State) {
	t.Helper()
	if got, err := await(t, f); got != state {
		t.Fatalf("state = %s (err %v), want %s", got, err, state)
	}
}

// failure waits for f and returns the error of a FAILED fiber.
func failure(t testing.TB, f *Fiber) error {
	t.Helper()
	s, err := await(t, f)
	if s != Failed || err == nil {
		t.Fatalf("state = %s, err = %v, want FAILED with an error", s, err)
	}
	return err
}

// catch returns what fn panics with, or nil.
func catch(fn func()) (p any) {
	defer func() { p = recover() }()
	fn()
	return nil
}

// injectFailure returns the *InjectError that fn panics with, directly or as a refused call.
func injectFailure(t testing.TB, fn func()) *InjectError {
	t.Helper()
	p := catch(fn)
	if rc, ok := p.(*refusedCall); ok {
		p = rc.err
	}
	e, ok := p.(*InjectError)
	if !ok {
		t.Fatalf("panic = %#v, want *InjectError", p)
	}
	return e
}

func settle(t testing.TB, r *Root) {
	t.Helper()
	if err := r.Settle(frontCtx(t)); err != nil {
		t.Fatalf("settle: %v", err)
	}
}

// notify asks the dependants of keys to re-run their availability checks.
func notify(t testing.TB, r *Root, keys ...AnyKey) {
	t.Helper()
	var ks []svcKey
	for _, k := range keys {
		d, err := dataOf(k)
		if err != nil {
			t.Fatal(err)
		}
		ks = append(ks, d.def)
	}
	if err := r.core.notifyKeys(frontCtx(t), ks...); err != nil {
		t.Fatal(err)
	}
}

// ledger returns the effect labels of f.
func ledger(t testing.TB, f *Fiber) []string {
	t.Helper()
	l, err := f.Effects(frontCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

type fakeTB struct {
	testing.TB
	cleanups []func()
	fatal    string
	errors   []string
	logs     []string
}

type fatalStop struct{}

func (f *fakeTB) Helper()                   {}
func (f *fakeTB) Cleanup(fn func())         { f.cleanups = append(f.cleanups, fn) }
func (f *fakeTB) Logf(s string, a ...any)   { f.logs = append(f.logs, fmt.Sprintf(s, a...)) }
func (f *fakeTB) Errorf(s string, a ...any) { f.errors = append(f.errors, fmt.Sprintf(s, a...)) }
func (f *fakeTB) Fatalf(s string, a ...any) {
	f.fatal = fmt.Sprintf(s, a...)
	panic(fatalStop{})
}

func TestNewRejectsBadConfigAndBindings(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("a zero configuration was accepted")
	}
	open := NewKey[int]("open", 0)
	bound := NewKey[int]("bound", 0, ProviderBound())
	consumed := NewKey[int]("consumed", 0, ConsumerBound())
	applied := plug("a", nil, nil, nil)
	noApply := Plugin{ID: "none"}
	for name, tc := range map[string]struct{ p, c Bindings }{
		"open key as provider":       {p: Bindings{open: {applied}}},
		"open key as consumer":       {c: Bindings{open: {applied}}},
		"consumer-bound as provider": {p: Bindings{consumed: {applied}}},
		"entry never defined":        {p: Bindings{bound: {noApply}}},
		"nil key":                    {p: Bindings{nil: {applied}}},
		"zero key":                   {c: Bindings{Key[int]{}: {applied}}},
		"key twice":                  {p: Bindings{bound: {applied}, bound.k: {applied}}},
	} {
		cfg := frontConfig()
		cfg.Providers, cfg.Consumers = tc.p, tc.c
		r, err := New(cfg)
		if err == nil {
			_ = r.Close(frontCtx(t))
			t.Errorf("%s: accepted", name)
		}
	}
	cfg := frontConfig()
	cfg.Providers, cfg.Consumers = Bindings{bound: {applied}}, Bindings{consumed: {applied}}
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(frontCtx(t)); err != nil {
		t.Fatal(err)
	}
}

func TestNewForTestFailsAClosedKernelAndReportsCloseDiagnostics(t *testing.T) {
	bad := &fakeTB{}
	if p := catch(func() { NewForTest(bad, Config{}) }); p != (fatalStop{}) || bad.fatal == "" {
		t.Fatalf("a bad configuration did not stop the test: %v %q", p, bad.fatal)
	}

	cfg := frontConfig()
	cfg.DisposeDeadline = 20 * time.Millisecond
	cfg.Providers = Bindings{NewKey[int]("pb", 0, ProviderBound()): {plug("x", nil, nil, nil)}}
	fake := &fakeTB{}
	r := NewForTest(fake, cfg)
	if r.unbound != true || len(r.providers) != 0 {
		t.Fatal("NewForTest kept the binding maps")
	}
	// A disposer that outlives the dispose deadline makes the kernel's Close fail.
	release := make(chan struct{})
	f := r.Plugin(plug("stuck", nil, nil, func(c *Context) error {
		c.Defer("stuck", func(context.Context) error { <-release; return nil })
		return nil
	}), nil)
	want(t, f, Active)
	for _, fn := range fake.cleanups {
		fn()
	}
	close(release)
	if len(fake.errors) != 1 {
		t.Fatalf("Close diagnostics = %v, want one", fake.errors)
	}

	// A Close diagnostic that is not a leak, here a failing host disposer, is logged.
	plain := &fakeTB{}
	r2 := NewForTest(plain, frontConfig())
	r2.Context().Defer("host", func(context.Context) error { return errors.New("boom") })
	for _, fn := range plain.cleanups {
		fn()
	}
	if len(plain.errors) != 0 || len(plain.logs) != 1 {
		t.Fatalf("errors = %v, logs = %v", plain.errors, plain.logs)
	}
}

func TestPluginMountDisposeRemountThreeTimes(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	svc := NewKey[string]("svc", 1)
	var disposed atomic.Int32
	provider := plug("p", nil, []AnyKey{svc}, func(c *Context) error {
		c.Defer("res", func(context.Context) error { disposed.Add(1); return nil })
		return svc.Provide(c, "v")
	})
	for round := 1; round <= 3; round++ {
		f := r.Plugin(provider, nil)
		want(t, f, Active)
		if got := ledger(t, f); !slices.Equal(got, []string{"res", "provide:svc"}) {
			t.Fatalf("round %d: effects = %v", round, got)
		}
		if err := f.Dispose(ctx); err != nil {
			t.Fatal(err)
		}
		want(t, f, Disposed)
		if int(disposed.Load()) != round {
			t.Fatalf("round %d: disposed %d times", round, disposed.Load())
		}
		if got := ledger(t, f); len(got) != 0 {
			t.Fatalf("round %d: ledger after dispose = %v", round, got)
		}
	}
	settle(t, r)
}

func TestFiberUpdateRestartsAndAwaitReportsTheStartupError(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	var runs atomic.Int32
	f := r.Plugin(plug("flaky", nil, nil, func(*Context) error {
		if runs.Add(1) == 1 {
			return errors.New("first run fails")
		}
		return nil
	}), nil)
	if err := failure(t, f); err.Error() == "" {
		t.Fatal("empty error")
	}
	if err := f.Update(ctx, nil); err != nil {
		t.Fatal(err)
	}
	want(t, f, Active)
	if runs.Load() != 2 {
		t.Fatalf("runs = %d", runs.Load())
	}
	if err := f.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.Dispose(ctx); err != nil {
		t.Fatalf("second dispose: %v", err)
	}
	want(t, f, Disposed)
	if err := f.Update(ctx, nil); err == nil {
		t.Fatal("update of a disposed fiber succeeded")
	}
}

func TestPluginIsRefusedAsAFailedFiber(t *testing.T) {
	cb := NewKey[int]("cb", 0, ConsumerBound())
	pb := NewKey[int]("pb2", 0, ProviderBound())
	cfgKey := NewKey[int]("cfg", 0, InterceptConfig[string]())
	open := NewKey[int]("open", 0)
	k := newBound(t, Bindings{pb: nil}, Bindings{cb: nil})
	for name, tc := range map[string]struct {
		p    Plugin
		want error
	}{
		"consumer-bound Need":     {plug("a", []Dep{Need(cb)}, nil, nil), ErrBinding},
		"consumer-bound Want":     {plug("b", []Dep{Want(cb)}, nil, nil), ErrBinding},
		"provider-bound Want":     {plug("c", []Dep{Want(pb)}, nil, nil), ErrWant},
		"duplicate Dep":           {plug("d", []Dep{Need(open), Need(open)}, nil, nil), ErrDuplicate},
		"zero Dep":                {plug("e", []Dep{{}}, nil, nil), ErrKey},
		"bad NeedWith":            {plug("f", []Dep{NeedWith(cfgKey, 7)}, nil, nil), ErrConfig},
		"provider-bound Provides": {plug("g", nil, []AnyKey{pb}, nil), ErrBinding},
		"duplicate Provides":      {plug("h", nil, []AnyKey{open, open}, nil), ErrDuplicate},
		"nil Provides":            {plug("i", nil, []AnyKey{nil}, nil), ErrKey},
		"zero Provides":           {plug("j", nil, []AnyKey{Key[int]{}}, nil), ErrKey},
	} {
		f := k.Plugin(tc.p, nil)
		if err := failure(t, f); !errors.Is(err, tc.want) {
			t.Errorf("%s: error = %v, want %v", name, err, tc.want)
		}
		var ie *InjectError
		if err := failure(t, f); !errors.As(err, &ie) {
			t.Errorf("%s: error %v is no *InjectError", name, err)
		}
	}
	// Settle reports the failed fibers, one diagnostic each.
	if err := k.Settle(frontCtx(t)); err == nil {
		t.Fatal("Settle hid the failed fibers")
	}
}

func TestWhenRunsOnceItsNeedsAreActive(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	svc := NewKey[int]("svc", 0)
	var got atomic.Int64
	var when *Fiber
	parent := r.Plugin(plug("parent", []Dep{Want(svc)}, nil, func(c *Context) error {
		if p := catch(func() { svc.Of(c) }); p == nil {
			return errors.New("a Want allowed a read")
		}
		when = c.When([]Dep{Need(svc)}, func(w *Context) error { got.Store(int64(svc.Of(w))); return nil })
		return nil
	}), nil)
	want(t, parent, Active)
	if s, _ := await(t, when); s != Pending {
		t.Fatalf("When = %s without a provider, want PENDING", s)
	}
	prov := r.Plugin(plug("prov", nil, []AnyKey{svc}, func(c *Context) error { return svc.Provide(c, 42) }), nil)
	want(t, prov, Active)
	want(t, when, Active)
	if got.Load() != 42 {
		t.Fatalf("When body read %d", got.Load())
	}
	// The parent never waited for the When: it was ACTIVE before the provider existed.
	if err := parent.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	want(t, when, Disposed)
	if err := prov.Dispose(ctx); err != nil {
		t.Fatal(err)
	}

	var bad *Fiber
	host := r.Plugin(plug("host", nil, nil, func(c *Context) error {
		bad = c.When(nil, func(*Context) error { return errors.New("body failed") })
		return nil
	}), nil)
	want(t, host, Active)
	if err := failure(t, bad); err.Error() == "" {
		t.Fatal("empty error")
	}
}

func TestMountOnAnInactiveOrClosedContextPanics(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	var saved *Context
	f := r.Plugin(plug("host", nil, nil, func(c *Context) error { saved = c; return nil }), nil)
	want(t, f, Active)
	if err := f.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	want(t, f, Disposed)
	p := catch(func() { saved.Plugin(plug("late", nil, nil, nil), nil) })
	if e, ok := p.(*refusedCall); !ok || !errors.Is(e, ErrInactiveEffect) || !errors.Is(e, ErrInactive) {
		t.Fatalf("late mount panic = %#v", p)
	}
	// Inside a callback the same refusal fails the fiber that made it.
	late := r.Plugin(plug("late-caller", nil, nil, func(c *Context) error {
		c.Go("late", func(ctx context.Context) error {
			<-ctx.Done()
			// The activation is unloading: the registration is refused.
			if p := catch(func() { c.Defer("x", func(context.Context) error { return nil }) }); p == nil {
				return errors.New("Defer was not refused")
			}
			return nil
		})
		return nil
	}), nil)
	want(t, late, Active)
	if err := late.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	want(t, late, Disposed)

	if err := r.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if p := catch(func() { r.Plugin(plug("after", nil, nil, nil), nil) }); p == nil {
		t.Fatal("mount after Close did not panic")
	}
}

func TestBatchCommitsMountsUpdatesAndDisposals(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	var runs atomic.Int32
	counted := plug("counted", nil, nil, func(*Context) error { runs.Add(1); return nil })
	fs, err := r.Batch(ctx, MountOp(counted, nil), MountOp(plug("other", nil, nil, nil), nil))
	if err != nil {
		t.Fatal(err)
	}
	want(t, fs[0], Active)
	want(t, fs[1], Active)
	got, err := r.Batch(ctx, UpdateOp(fs[0]), UpdateOp(fs[0]), DisposeOp(fs[1]))
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != fs[0] || got[2] != fs[1] {
		t.Fatal("update and dispose did not return their fibers")
	}
	want(t, fs[0], Active)
	want(t, fs[1], Disposed)
	if runs.Load() != 2 {
		t.Fatalf("two updates ran Apply %d times in total, want the first run plus one restart", runs.Load())
	}

	other := newKernel(t)
	foreign := other.Plugin(plug("foreign", nil, nil, nil), nil)
	for name, ops := range map[string][]BatchOp{
		"nil fiber":     {UpdateOp(nil)},
		"foreign fiber": {DisposeOp(foreign)},
		"empty id":      {MountOp(Plugin{}, nil)},
		"zero fiber":    {UpdateOp(&Fiber{})},
	} {
		if _, err := r.Batch(ctx, ops...); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := r.Batch(ctx); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
}

func TestRootSettleAndCloseAreIdempotent(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	f := r.Plugin(plug("p", nil, nil, nil), nil)
	settle(t, r)
	if err := r.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(ctx); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if _, err := f.State(ctx); !errors.Is(err, errClosed) {
		t.Fatalf("State after close: %v, want %v", err, errClosed)
	}
}

func TestCompileCheckAndReadFailClosedOnAnUnboundActivation(t *testing.T) {
	r := newKernel(t)
	svc := NewKey[int]("svc", 0)
	spec := r.ctx.compile("c", r.host, []Dep{Need(svc)}, nil, nil)
	if spec.Check == nil || spec.Apply != nil {
		t.Fatal("a Need must give a Check and no Apply")
	}
	h := newHandle(r.core, context.Background(), 0, map[svcKey]provision{})
	if spec.Check(h) {
		t.Fatal("Check passed without a binding")
	}
	h.bindings[svc.k.def] = provision{value: "not a provision"}
	if spec.Check(h) {
		t.Fatal("Check passed on a foreign value")
	}

	// A foreign value in a binding makes a read fail instead of mistyping.
	other := &Context{root: r, h: h, entry: r.host, frame: &frame{id: "x", inject: []Dep{Need(svc)}, at: r.ctx}}
	if e := injectFailure(t, func() { svc.Of(other) }); !errors.Is(e, errNoBinding) {
		t.Fatalf("foreign value: %v", e)
	}
	// A declared Need whose activation holds no binding is refused the same way.
	delete(h.bindings, svc.k.def)
	if e := injectFailure(t, func() { svc.Of(other) }); !errors.Is(e, errNoBinding) {
		t.Fatalf("error = %v", e)
	}
}

func TestNewRootStopsItsCoreWhenTheRootHandleIsRefused(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r, err := newRoot(ctx, testConfig(), nil, nil, true); !errors.Is(err, context.Canceled) || r != nil {
		t.Fatalf("newRoot with a cancelled context = %v, %v", r, err)
	}
}
