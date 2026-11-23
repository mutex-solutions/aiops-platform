// SPDX-License-Identifier: Apache-2.0

// Package conformance is the suite of the echoers definition (07-c4-plugin-sdk.md:1463).
// The obligations half is what an echoer owes; it runs on a contribution, with the
// recording fake as its dispatcher. The guard half is what a dispatcher owes; it runs
// on the real dispatcher and on the fake. Both halves carry the dispose case and the
// ErrInactive case; the two-tenant cases arrive with the tenants definition under a
// raised suite revision (ADR-060 decision 43).
//
// Every case has an id. A failed case prints one line, which starts with its id, so
// plugin check and a fresh template print one line per failed obligation
// (07-c4-plugin-sdk.md:1561, :1563; ADR-060 decision 5). The package imports the
// standard library, sdk/kernel, the definition and its fake only.
package conformance

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	echoers "github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Subject is the implementation under test, made by Dispatcher or Contribution.
type Subject struct {
	fiber *kernel.Fiber
	guard bool
}

// Dispatcher names the fiber of a dispatcher under test, which provides echoers.Key:
// Run runs the guard half on it.
func Dispatcher(f *kernel.Fiber) Subject { return Subject{fiber: f, guard: true} }

// Contribution names the fiber of a contribution under test, mounted on a root where
// nothing provides echoers.Key: Run runs the obligations half on it and mounts the
// recording fake as its dispatcher.
func Contribution(f *kernel.Fiber) Subject { return Subject{fiber: f} }

// Mount mounts the implementation under test on a root of its own and returns it with
// the root (ADR-059 decision 32). Run calls it once per case.
type Mount func(t *testing.T) (Subject, *kernel.Root)

// Obligations and Guards are the ids of the cases of each half, in the order Run runs
// them.
var (
	Obligations = ids(obligations)
	Guards      = ids(guards)
)

type check struct {
	id  string
	run func(e *env)
}

// Run runs the half that the subject of the first mount asks for, each case on a
// mount of its own; the first case runs on that first mount.
func Run(t *testing.T, mount Mount) {
	t.Helper()
	s, root := mount(t)
	cases, setup := guards, (*env).guard
	if !s.guard {
		cases, setup = obligations, (*env).obligation
	}
	for i, c := range cases {
		t.Run(strings.TrimPrefix(c.id, "echoers/"), func(t *testing.T) {
			if i > 0 {
				s, root = mount(t)
			}
			e := &env{t: t, id: c.id, root: root, s: s}
			setup(e)
			c.run(e)
		})
	}
}

func ids(cs []check) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.id)
	}
	return out
}

// messages are what the obligations send.
var messages = []string{"", "hello", "héllo, wörld", strings.Repeat("echo ", 1000)}

var obligations = []check{
	{"echoers/register", func(e *env) {
		if labels, _ := e.s.fiber.Effects(e.t.Context()); !slices.Contains(labels, echoers.Label(e.name)) {
			e.fail("the contribution's fiber does not hold the registration %q in its ledger: %v", echoers.Label(e.name), labels)
		}
		if _, err := e.reg.Echo(e.t.Context(), e.name, "ping"); err != nil {
			e.fail("the echoer registered as %q does not answer: %v", e.name, err)
		}
	}},
	{"echoers/reply", func(e *env) {
		for _, m := range messages {
			if r, err := e.reg.Echo(e.t.Context(), e.name, m); err != nil || !strings.HasSuffix(r, m) {
				e.fail("Echo(%.20q) = %.40q, %v; want a reply that ends with the message", m, r, err)
			}
		}
	}},
	{"echoers/stable", func(e *env) {
		r1, err1 := e.reg.Echo(e.t.Context(), e.name, "same")
		r2, err2 := e.reg.Echo(e.t.Context(), e.name, "same")
		if err1 != nil || err2 != nil || r1 != r2 {
			e.fail("two echoes of one message answered %q, %v and %q, %v; want one reply twice", r1, err1, r2, err2)
		}
	}},
	{"echoers/cancel", func(e *env) {
		ctx, cancel := context.WithCancel(e.t.Context())
		cancel()
		if _, err := e.reg.Echo(ctx, e.name, "late"); !errors.Is(err, context.Canceled) {
			e.fail("Echo with a cancelled context = %v, want context.Canceled", err)
		}
	}},
	{"echoers/release", func(e *env) {
		x, _ := e.rec.Contribution(e.name)
		e.dispose(e.s.fiber)
		if slices.Contains(e.rec.Inventory(), e.name) {
			e.fail("the registration %q outlived the contribution's fiber", e.name)
		}
		if _, err := x.Echo(e.t.Context(), "late"); !errors.Is(err, kernel.ErrInactive) {
			e.fail("Echo after the contribution's fiber was disposed = %v, want kernel.ErrInactive", err)
		}
	}},
}

var errEcho = errors.New("conformance: the echoer failed")

var guards = []check{
	{"echoers/guard-register", func(e *env) {
		_, fa := e.contribute("a", nil)
		_, fb := e.contribute("b", nil)
		if got := e.reg.Inventory(); !slices.Equal(got, []string{"a", "b"}) {
			e.fail("Inventory = %v, want [a b]", got)
		}
		la, _ := fa.Effects(e.t.Context())
		lb, _ := fb.Effects(e.t.Context())
		if !slices.Contains(la, echoers.Label("a")) || slices.Contains(la, echoers.Label("b")) || !slices.Contains(lb, echoers.Label("b")) {
			e.fail("each registration is not an effect of its own fiber: a holds %v, b holds %v", la, lb)
		}
	}},
	{"echoers/guard-dispatch", func(e *env) {
		a, _ := e.contribute("a", nil)
		b, _ := e.contribute("b", nil)
		e.contribute("bad", errEcho)
		if r, err := e.reg.Echo(e.t.Context(), "a", "hi"); r != "a:hi" || err != nil {
			e.fail("Echo(a) = %q, %v; want a's reply a:hi", r, err)
		}
		if got, other := a.calls(), b.calls(); !slices.Equal(got, []string{"hi"}) || len(other) != 0 {
			e.fail("a heard %v and b heard %v; want only a, once", got, other)
		}
		if _, err := e.reg.Echo(e.t.Context(), "bad", "hi"); !errors.Is(err, errEcho) {
			e.fail("Echo of a failing echoer = %v, want its error unchanged", err)
		}
	}},
	{"echoers/guard-refuse", func(e *env) {
		e.contribute("a", nil)
		if _, err := e.reg.Echo(e.t.Context(), "nope", "hi"); !errors.Is(err, echoers.ErrUnknown) {
			e.fail("Echo of an unknown name = %v, want ErrUnknown", err)
		}
		for _, name := range []string{"", "a"} {
			if err := e.refused((&echoer{name: name}).plugin("conformance/again-" + name)); !errors.Is(err, echoers.ErrName) {
				e.fail("Register(%q) = %v, want ErrName", name, err)
			}
		}
		if r, err := e.reg.Echo(e.t.Context(), "a", "hi"); r != "a:hi" || err != nil || !slices.Equal(e.reg.Inventory(), []string{"a"}) {
			e.fail("a refused registration changed the registry: Echo(a) = %q, %v; Inventory = %v", r, err, e.reg.Inventory())
		}
	}},
	{"echoers/guard-effect", func(e *env) {
		f, late := e.hold()
		e.dispose(f)
		if err := e.reg.Register(late, "late", &echoer{name: "late"}); !errors.Is(err, kernel.ErrInactiveEffect) {
			e.fail("Register from a disposed fiber = %v, want kernel.ErrInactiveEffect", err)
		}
		if slices.Contains(e.reg.Inventory(), "late") {
			e.fail("a registration from a disposed fiber is listed")
		}
	}},
	{"echoers/guard-dispose", func(e *env) {
		_, fa := e.contribute("a", nil)
		e.contribute("b", nil)
		e.dispose(fa)
		_, errA := e.reg.Echo(e.t.Context(), "a", "hi")
		r, errB := e.reg.Echo(e.t.Context(), "b", "hi")
		if got := e.reg.Inventory(); !slices.Equal(got, []string{"b"}) || !errors.Is(errA, echoers.ErrUnknown) || r != "b:hi" || errB != nil {
			e.fail("after a's fiber was disposed: Inventory = %v, Echo(a) = %v, Echo(b) = %q, %v", got, errA, r, errB)
		}
	}},
	{"echoers/guard-release", func(e *env) {
		e.contribute("a", nil)
		_, live := e.hold()
		e.dispose(e.s.fiber)
		_, errCall := e.reg.Echo(e.t.Context(), "a", "hi")
		errReg := e.reg.Register(live, "x", &echoer{name: "x"})
		if !errors.Is(errCall, kernel.ErrInactive) || !errors.Is(errReg, kernel.ErrInactive) || len(e.reg.Inventory()) != 0 {
			e.fail("after the dispatcher was disposed: Echo = %v, Register = %v, Inventory = %v; want kernel.ErrInactive twice and nothing listed",
				errCall, errReg, e.reg.Inventory())
		}
	}},
}

// env is what one case runs on.
type env struct {
	t    *testing.T
	id   string
	root *kernel.Root
	s    Subject
	reg  echoers.Registry
	rec  *fake.Registry // the obligations half: the dispatcher the suite mounted
	name string         // the obligations half: the one name the contribution registered
}

// fail ends the case with its one line.
func (e *env) fail(format string, a ...any) {
	e.t.Helper()
	e.t.Fatalf(e.id+": "+format, a...)
}

// obligation mounts the fake under the contribution and reads its one registration.
func (e *env) obligation() {
	e.t.Helper()
	rec, p := fake.New()
	e.mount(p)
	if err := e.root.Settle(e.t.Context()); err != nil {
		e.fail("the kernel did not settle: %v", err)
	}
	err := e.s.fiber.Await(e.t.Context())
	if st, _ := e.s.fiber.State(e.t.Context()); st != kernel.Active {
		e.fail("the contribution is %s with echoers served, want ACTIVE: %v", st, err)
	}
	if names := rec.Inventory(); len(names) != 1 {
		e.fail("the contribution registered %v, want one name", names)
	}
	e.reg, e.rec, e.name = rec, rec, rec.Inventory()[0]
}

// guard waits for the dispatcher and reads the registry through a consumer of the key.
func (e *env) guard() {
	e.t.Helper()
	err := e.s.fiber.Await(e.t.Context())
	if st, _ := e.s.fiber.State(e.t.Context()); st != kernel.Active {
		e.fail("the dispatcher is %s, want ACTIVE: %v", st, err)
	}
	e.mount(kernel.Define(kernel.Plugin{ID: "conformance/probe", Inject: []kernel.Dep{kernel.Need(echoers.Key)},
		Apply: func(c *kernel.Context, _ any) error { e.reg = echoers.Key.Of(c); return nil }}))
}

// mount mounts p and fails the case unless it is ACTIVE.
func (e *env) mount(p kernel.Plugin) *kernel.Fiber {
	e.t.Helper()
	f := e.root.Plugin(p, nil)
	err := f.Await(e.t.Context())
	if st, _ := f.State(e.t.Context()); st != kernel.Active {
		e.fail("%s is %s, want ACTIVE: %v", p.ID, st, err)
	}
	return f
}

// refused mounts p and returns why it failed; it fails the case if p is ACTIVE.
func (e *env) refused(p kernel.Plugin) error {
	e.t.Helper()
	f := e.root.Plugin(p, nil)
	err := f.Await(e.t.Context())
	if st, _ := f.State(e.t.Context()); st != kernel.Failed {
		e.fail("%s is %s, want FAILED", p.ID, st)
	}
	return err
}

// contribute mounts an echoer of the suite that registers under name and answers
// name+":"+msg with err.
func (e *env) contribute(name string, err error) (*echoer, *kernel.Fiber) {
	e.t.Helper()
	x := &echoer{name: name, err: err}
	return x, e.mount(x.plugin("conformance/echoer-" + name))
}

// hold mounts a fiber that needs nothing and returns it with its context.
func (e *env) hold() (*kernel.Fiber, *kernel.Context) {
	e.t.Helper()
	var held *kernel.Context
	f := e.mount(kernel.Define(kernel.Plugin{ID: "conformance/holder",
		Apply: func(c *kernel.Context, _ any) error { held = c; return nil }}))
	return f, held
}

// dispose disposes f and waits until it is.
func (e *env) dispose(f *kernel.Fiber) {
	e.t.Helper()
	if err := f.Dispose(e.t.Context()); err != nil {
		e.fail("dispose: %v", err)
	}
	_ = f.Await(e.t.Context())
}

// echoer is a contribution of the guard half.
type echoer struct {
	name  string
	err   error
	mu    sync.Mutex
	heard []string
}

func (x *echoer) plugin(id string) kernel.Plugin {
	return kernel.Define(kernel.Plugin{ID: id, Inject: []kernel.Dep{kernel.Need(echoers.Key)},
		Apply: func(c *kernel.Context, _ any) error { return echoers.Key.Of(c).Register(c, x.name, x) }})
}

func (x *echoer) Echo(_ context.Context, msg string) (string, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.heard = append(x.heard, msg)
	return x.name + ":" + msg, x.err
}

func (x *echoer) calls() []string {
	x.mu.Lock()
	defer x.mu.Unlock()
	return slices.Clone(x.heard)
}
