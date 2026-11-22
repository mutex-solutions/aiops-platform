// SPDX-License-Identifier: Apache-2.0

package loader

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	compfake "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0/fake"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	loaderfake "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

func TestDisposingTheLoaderDisposesTheTreeInReverse(t *testing.T) {
	p := newProbe()
	x := newFx(t, fxOpt{
		plugins: plugins(p.plugin("t/r1", nil), p.plugin("t/r2", nil), p.plugin("t/k", nil), p.plugin("t/late", nil)),
		rows:    []def.EntrySpec{row("r1", "t/r1"), group("g"), child("g", "k", "t/k"), row("r2", "t/r2")},
	})
	x.apply(create(row("late", "t/late")))
	x.want("g:k", kernel.Active)
	l := record(t, x.root)
	if err := x.fiber.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := x.fiber.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	x.settle()
	// The kernel requests the disposal of the rows in reverse order of their mounts,
	// one that a batch mounted as any other, and a group's children once the group's
	// own disposal runs. It does not wait for one before the next, so their
	// disposers may overlap (ADR-034, 2026-11-19 teardown order).
	at := map[string]int{}
	for i, id := range l.of(t, x.root, "dispose:request") {
		at[id] = i
	}
	if !(at["t/late"] < at["t/r2"] && at["t/r2"] < at[def.GroupPlugin] && at[def.GroupPlugin] < at["t/r1"] && at[def.GroupPlugin] < at["t/k"]) {
		t.Errorf("disposal requests %v", at)
	}
	if got := slices.Sorted(slices.Values(p.order())); !slices.Equal(got, []string{"t/k", "t/late", "t/r1", "t/r2"}) {
		t.Errorf("disposers that ran: %v", got)
	}
	if _, ok := x.ld.Locate("r1"); ok {
		t.Error("a disposed loader still holds entries")
	}
	if _, err := x.ld.Apply(t.Context(), []def.Op{create(row("late", "t/r1"))}); !errors.Is(err, kernel.ErrInactive) {
		t.Errorf("Apply on a disposed loader: %v", err)
	}
	if g, _ := x.rd.Committed(composition.ScopeProcess); len(g.Rows) != 0 {
		t.Errorf("a disposed loader serves rows: %v", g.Rows)
	}
}

// A child's drain and disposal count against the child's own deadlines, never
// against those of the loader or of its group: the kernel's join of a parent's
// children has no clock (ADR-034, 2026-11-19 teardown order).
func TestAParentIsNotChargedForItsChildren(t *testing.T) {
	slow := kernel.Define(kernel.Plugin{ID: "t/slow", Apply: func(c *kernel.Context, _ any) error {
		// Each takes 600ms of the child's own 1s budgets, 1.2s in all.
		c.Go("drain", func(ctx context.Context) error { <-ctx.Done(); time.Sleep(600 * time.Millisecond); return nil })
		c.Defer("dispose", func(context.Context) error { time.Sleep(600 * time.Millisecond); return nil })
		return nil
	}})
	x := newFx(t, fxOpt{plugins: plugins(slow), kopts: []kerneltest.Option{kerneltest.WithDeadlines(10*time.Second, time.Second, time.Second)},
		rows: []def.EntrySpec{group("g"), child("g", "s", "t/slow")}})
	x.want("g:s", kernel.Active)
	l := record(t, x.root)
	if err := x.fiber.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := x.fiber.Await(t.Context()); err != nil {
		t.Fatalf("disposing the loader: %v", err)
	}
	x.settle()
	if got := l.of(t, x.root, "deadline"); len(got) != 0 {
		t.Errorf("deadlines passed for %v", got)
	}
}

func TestRemountingTheLoaderLeavesNothingBehind(t *testing.T) {
	p := newProbe()
	root, l := bareKernel(t)
	l.Plugins = plugins(p.plugin("t/a", nil), p.plugin("t/k", nil))
	rows := []def.EntrySpec{row("a", "t/a"), group("g"), child("g", "k", "t/k")}
	baseline := func() (n int) {
		in, err := root.Inspect(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range in.Fibers {
			if f.State != kernel.Disposed {
				n++
			}
			if len(f.Tracked) != 0 {
				t.Errorf("%s has tracked work %v", f.Plugin, f.Tracked)
			}
		}
		return n
	}
	idle := baseline()
	for i := 1; i <= 3; i++ {
		f := root.Plugin(New(), Config{Rows: rows, Batch: root.Batch})
		if err := f.Await(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := root.Settle(t.Context()); err != nil {
			t.Fatal(err)
		}
		if p.count("t/a") != i || p.count("t/k") != i || p.maxLive["t/a"] != 1 || p.maxLive["t/k"] != 1 {
			t.Fatalf("round %d: applies a %d, k %d", i, p.count("t/a"), p.count("t/k"))
		}
		if err := f.Dispose(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := f.Await(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := root.Settle(t.Context()); err != nil {
			t.Fatal(err)
		}
		if n := baseline(); n != idle {
			t.Fatalf("round %d leaves %d fibers, want %d", i, n, idle)
		}
	}
}

// The loader provides two keys, and a second provider of either fails the loader.
func TestLoaderFailsWhenAKeyIsTaken(t *testing.T) {
	_, other := loaderfake.New()
	_, comp := compfake.New()
	for name, taken := range map[string]kernel.Plugin{"loader": other, "composition": comp} {
		t.Run(name, func(t *testing.T) {
			root, _ := bareKernel(t)
			kerneltest.Mount(root, taken)
			if err := root.Plugin(New(), Config{}).Await(t.Context()); err == nil {
				t.Fatal("a second provider mounted")
			}
		})
	}
}
