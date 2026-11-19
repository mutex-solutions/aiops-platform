// SPDX-License-Identifier: Apache-2.0

package concurrency

// Plugin-supplied error values are rendered once, where they enter the kernel
// (on the plugin's worker, off the graph lock, under a recover), and the kernel
// only ever uses the stored text afterwards (ADR-034, error rendering).

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

// renderProbe is an error whose Error method counts its calls and may panic.
type renderProbe struct {
	calls atomic.Int64
	boom  bool
}

func (p *renderProbe) Error() string {
	p.calls.Add(1)
	if p.boom {
		panic("Error called on a plugin value")
	}
	return "probe text"
}

const renderPlaceholder = "error formatting panic"

type renderCase struct {
	name string
	// spec builds a fiber that fails with p. trigger runs after it was mounted
	// and settled (it receives the activation context the Apply captured).
	spec      func(p *renderProbe, captured chan<- *Context) Spec
	trigger   func(t *testing.T, g *GraphLock, id uint64, c *Context, p *renderProbe)
	wantCalls int64 // renderings of the probe: one per time it enters the kernel
}

func renderCases() []renderCase {
	return []renderCase{
		{name: "apply", wantCalls: 1, spec: func(p *renderProbe, _ chan<- *Context) Spec {
			return Spec{ID: "f", Apply: func(*Context) error { return p }}
		}},
		{name: "apply-effect", wantCalls: 1, spec: func(p *renderProbe, _ chan<- *Context) Spec {
			return Spec{ID: "f", ApplyEffect: func(*Context) (Disposer, error) { return nil, p }}
		}},
		{name: "resolve", wantCalls: 1, spec: func(p *renderProbe, _ chan<- *Context) Spec {
			cell := NewConfiguration("raw", ConfigHooks{Resolve: func(context.Context, any) (any, error) { return nil, p }})
			return Spec{ID: "f", Config: cell}
		}},
		{name: "disposer", wantCalls: 1, spec: func(p *renderProbe, _ chan<- *Context) Spec {
			return Spec{ID: "f", Apply: func(c *Context) error {
				return c.Defer("d", func(context.Context) error { return p })
			}}
		}, trigger: func(t *testing.T, g *GraphLock, id uint64, _ *Context, _ *renderProbe) {
			if err := g.Dispose(context.Background(), id); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "tracked", wantCalls: 1, spec: func(p *renderProbe, captured chan<- *Context) Spec {
			return Spec{ID: "f", Apply: func(c *Context) error { captured <- c; return nil }}
		}, trigger: func(t *testing.T, g *GraphLock, id uint64, c *Context, p *renderProbe) {
			if err := c.Go("work", func(context.Context) error { return p }); err != nil {
				t.Fatal(err)
			}
			<-c.Std().Done() // The failure retires the activation.
		}},
		{name: "scope-setup", wantCalls: 1, spec: func(p *renderProbe, _ chan<- *Context) Spec {
			return Spec{ID: "f", Apply: func(c *Context) error {
				_, err := c.RequestEffectScope("s", func(*EffectScope) error { return p })
				return err
			}}
		}},
		{name: "effect-rollback", wantCalls: 2, spec: func(p *renderProbe, _ chan<- *Context) Spec {
			// The disposer's error enters the kernel at cleanupEffect, and the
			// plugin then returns it, joined, from Apply: a second entry.
			return Spec{ID: "f", Apply: func(c *Context) error {
				return c.Effect("e", func() (Disposer, error) {
					return func(context.Context) error { return p }, errors.New("setup failed")
				})
			}}
		}},
	}
}

func TestGraphLockPluginErrorsAreRenderedOnceAtTheBoundary(t *testing.T) {
	for _, c := range renderCases() {
		for _, boom := range []bool{false, true} {
			name := c.name + "/text"
			want := "probe text"
			if boom {
				name, want = c.name+"/panicking-Error", renderPlaceholder
			}
			t.Run(name, func(t *testing.T) {
				ctx := graphTestContext(t)
				g := graphTestRuntime(t).(*GraphLock)
				p := &renderProbe{boom: boom}
				captured := make(chan *Context, 1)
				id := graphMount(t, ctx, g, c.spec(p, captured))
				graphSettle(t, ctx, g)
				if c.trigger != nil {
					var owner *Context
					if len(captured) != 0 {
						owner = <-captured
					}
					c.trigger(t, g, id, owner, p)
					graphSettle(t, ctx, g)
				}
				// Every reader of the stored diagnostic, several times, on this
				// goroutine: none may call the plugin's Error method.
				for i := 0; i < 3; i++ {
					snaps, err := g.Snapshots(ctx)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := g.Inspect(ctx); err != nil {
						t.Fatal(err)
					}
					if _, err := g.Events(ctx); err != nil {
						t.Fatal(err)
					}
					got, ok := findSnapshot(snaps, "f")
					if !ok || !strings.Contains(got.Error, want) {
						t.Fatalf("snapshot error = %q (found %v), want it to contain %q", got.Error, ok, want)
					}
					if boom && strings.Contains(got.Error, "probe text") {
						t.Fatalf("snapshot error = %q", got.Error)
					}
				}
				if calls := p.calls.Load(); calls != c.wantCalls {
					t.Fatalf("Error was called %d times, want %d (once per entry into the kernel, none by readers)", calls, c.wantCalls)
				}
				if c.name == "disposer" || c.name == "apply" {
					// The caller still gets the plugin's own value for errors.Is.
					if err := g.AwaitFiber(ctx, id); c.name == "apply" && !errors.Is(err, p) {
						t.Fatalf("AwaitFiber lost the plugin error identity: %v", err)
					}
				}
				if err := g.Close(ctx); err != nil {
					// A failed fiber's startup error is not a Close error; a
					// returned error must still be printable without plugin code.
					_ = err.Error()
				}
				if calls := p.calls.Load(); calls != c.wantCalls {
					t.Fatalf("Close called Error again: %d calls, want %d", calls, c.wantCalls)
				}
			})
		}
	}
}

// A root worker failure comes back from Close: the returned error is the kernel's
// stored text around the plugin's value, so printing it never calls plugin code.
func TestGraphLockCloseReturnsStoredTextOfRootFailure(t *testing.T) {
	for _, boom := range []bool{false, true} {
		ctx := graphTestContext(t)
		g := graphTestRuntime(t).(*GraphLock)
		root, err := g.RootContext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		p := &renderProbe{boom: boom}
		if err := root.Go("failure", func(context.Context) error { return p }); err != nil {
			t.Fatal(err)
		}
		<-root.Std().Done()
		graphSettle(t, ctx, g)
		err = g.Close(ctx)
		if !errors.Is(err, p) {
			t.Fatalf("Close lost the root failure identity: %v", err)
		}
		text := err.Error()
		if boom != strings.Contains(text, renderPlaceholder) {
			t.Fatalf("Close text %q (boom=%v)", text, boom)
		}
		_ = err.Error()
		if calls := p.calls.Load(); calls != 1 {
			t.Fatalf("boom=%v: Error called %d times, want 1", boom, calls)
		}
	}
}
