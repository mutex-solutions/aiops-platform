// SPDX-License-Identifier: Apache-2.0

package replay

import (
	"context"
	"testing"

	cc "github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
)

func TestNativeEffectTreeResultCopiesRemainIndependent(t *testing.T) {
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
		handles := make(chan *cc.EffectHandle, 1)
		id, err := runtime.Mount(ctx, cc.Spec{ID: "real scope metadata", Apply: func(c *cc.Context) error {
			handle, err := c.RequestEffectScope("outer", func(scope *cc.EffectScope) error {
				return scope.Scope("nested", func(child *cc.EffectScope) error {
					return child.Defer("actual resource", func(context.Context) error { return nil })
				})
			})
			if err == nil {
				handles <- handle
			}
			return err
		}})
		if err != nil {
			t.Fatal(err)
		}
		if err := runtime.(cc.Waiter).AwaitFiber(ctx, id); err != nil {
			t.Fatal(err)
		}
		handle := <-handles
		if err := handle.AwaitSetup(ctx); err != nil {
			t.Fatal(err)
		}
		original, err := runtime.(cc.Inspector).Inspect(ctx)
		if err != nil {
			t.Fatal(err)
		}
		copied := copyInspection(original)
		if len(copied.Fibers) != 1 || len(copied.Fibers[0].EffectTree) != 1 || len(copied.Fibers[0].EffectTree[0].Children) != 1 || len(copied.Fibers[0].EffectTree[0].Children[0].Children) != 1 {
			t.Fatalf("actual native nested ownership missing: %+v", copied)
		}
		copied.Fibers[0].EffectTree[0].Children[0].Children[0].Label = "caller mutation"
		if original.Fibers[0].EffectTree[0].Children[0].Children[0].Label != "actual resource" {
			t.Fatal("result copy aliases the original native inspection")
		}
		fresh, err := runtime.(cc.Inspector).Inspect(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if fresh.Fibers[0].EffectTree[0].Children[0].Children[0].Label != "actual resource" || handle.Metadata().Children[0].Children[0].Label != "actual resource" {
			t.Fatal("caller mutation changed native ownership metadata")
		}
	})
}
