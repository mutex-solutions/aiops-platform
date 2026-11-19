// SPDX-License-Identifier: Apache-2.0

package concurrency

// A panic in kernel code behind a plugin-facing call is reported as an internal
// error naming the entry point, not as the plugin's "Apply panic", and leaves
// the graph lock released. Kernel bugs are injected by corrupting kernel state
// (a nil ledger makes the next event append panic); a plugin panic is checked
// to keep its own text.

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"testing/synctest"
)

func TestGraphLockKernelPanicBehindPluginCallIsInternalError(t *testing.T) {
	entries := []struct {
		entry string
		call  func(c *Context) error
	}{
		{"provide", func(c *Context) error { return c.Provide("k", 1) }},
		{"Defer", func(c *Context) error { return c.Defer("d", func(context.Context) error { return nil }) }},
		{"MountChild", func(c *Context) error { _, err := c.MountChild(Spec{ID: "child"}); return err }},
		{"Dispose", func(c *Context) error { return c.Dispose() }},
		{"RequestEffectScope", func(c *Context) error {
			_, err := c.RequestEffectScope("s", func(*EffectScope) error { return nil })
			return err
		}},
		{"Go", func(c *Context) error { return c.Go("work", func(context.Context) error { return nil }) }},
	}
	for _, e := range entries {
		t.Run(e.entry, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				g := graphTestRuntime(t).(*GraphLock)
				ctx := context.Background()
				var got error
				id := graphMount(t, ctx, g, Spec{ID: "victim", Provides: []string{"k"}, Apply: func(c *Context) error {
					g.mu.Lock()
					ledger := g.ledger
					g.ledger = nil // The next event append panics inside kernel code.
					g.mu.Unlock()
					got = e.call(c)
					g.mu.Lock()
					g.ledger = ledger
					g.mu.Unlock()
					return got
				}})
				synctest.Wait()
				if got == nil || !strings.Contains(got.Error(), "internal error in "+e.entry) {
					t.Fatalf("plugin call returned %v, want an internal error naming %s", got, e.entry)
				}
				// The lock was released (this would hang otherwise) and the fiber's
				// diagnostic is the internal error, never a plugin Apply panic.
				snap := graphSnapshotByID(t, g, id)
				if strings.Contains(snap.Error, "Apply panic") || !strings.Contains(snap.Error, "internal error in "+e.entry) {
					t.Fatalf("fiber diagnostic: %q", snap.Error)
				}
			})
		})
	}
}

// A panic in the plugin's own Apply keeps its text.
func TestGraphLockPluginPanicStaysApplyPanic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := graphTestRuntime(t).(*GraphLock)
		ctx := context.Background()
		id := graphMount(t, ctx, g, Spec{ID: "plugin", Apply: func(c *Context) error {
			_ = c.Defer("d", func(context.Context) error { return nil })
			panic("plugin bug")
		}})
		synctest.Wait()
		snap := graphSnapshotByID(t, g, id)
		if !strings.Contains(snap.Error, "Apply panic: plugin bug") || strings.Contains(snap.Error, "internal error") {
			t.Fatalf("plugin panic diagnostic: %q", snap.Error)
		}
	})
}

// Every lock region reachable from a plugin-facing call unlocks with defer: no
// bare g.mu.Unlock() statement may appear in these functions (nested function
// literals included), so no early return or panic can leave the graph locked.
func TestGraphLockPluginFacingFunctionsUnlockWithDefer(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "graphlock.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	facing := map[string]bool{
		"provide": true, "deferEffect": true, "requestScope": true, "cleanupEffect": true,
		"scopeCleanupContext": true, "cleanupBudget": true, "startTracked": true, "activationLocked": true,
		"guardEntry": true,
	}
	seen := map[string]bool{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || !facing[fn.Name.Name] {
			continue
		}
		seen[fn.Name.Name] = true
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			stmt, ok := n.(*ast.ExprStmt)
			if !ok {
				return true
			}
			if call, ok := stmt.X.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Unlock" {
					t.Errorf("%s: %s: bare Unlock; use defer (or a closure with defer)", fset.Position(stmt.Pos()), fn.Name.Name)
				}
			}
			return true
		})
	}
	for name := range facing {
		if !seen[name] {
			t.Errorf("function %s not found; update the list", name)
		}
	}
}
