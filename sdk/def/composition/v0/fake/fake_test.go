// SPDX-License-Identifier: Apache-2.0

package fake_test

import (
	"context"
	"testing"

	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

func TestConsumerReadsTheCommittedGeneration(t *testing.T) {
	f, provider := fake.New()
	f.Commit(composition.Generation{Scope: composition.ScopeProcess, Rows: []composition.Row{
		{ID: "a", Plugin: "aiops/a", State: kernel.Active, Hooks: []string{"approval/gates"}, Registrations: []string{"route"}},
		{ID: "b", Plugin: "aiops/b", State: kernel.Failed, Hooks: []string{"approval/gates", "x/y"}},
		{ID: "c", Plugin: "aiops/c", State: kernel.Pending},
	}})
	ran := false
	consumer := kernel.Define(kernel.Plugin{ID: "aiops/consumer", Inject: []kernel.Dep{kernel.Need(composition.Key)}, Apply: func(c *kernel.Context, _ any) error {
		ran = true
		r := composition.Key.Of(c)
		g, ok := r.Committed(composition.ScopeProcess)
		if !ok || len(g.Rows) != 3 {
			t.Errorf("Committed = %+v, %v", g, ok)
		}
		g.Rows[0].ID = "mutated" // a copy: the service is unchanged
		if g2, _ := r.Committed(composition.ScopeProcess); g2.Rows[0].ID != "a" {
			t.Error("Committed returned a shared slice")
		}
		if _, ok := r.Committed(composition.ScopeTenant); ok {
			t.Error("the tenant scope has no generation")
		}
		rows := r.Declaring(composition.ScopeProcess, "approval/gates")
		if len(rows) != 2 || rows[0].ID != "a" || rows[1].State != kernel.Failed {
			t.Errorf("Declaring = %v", rows)
		}
		if rows := r.Declaring(composition.ScopeProcess, "none"); len(rows) != 0 {
			t.Errorf("Declaring(none) = %v", rows)
		}
		if rows := r.Declaring(composition.ScopeTenant, "approval/gates"); len(rows) != 0 {
			t.Errorf("Declaring in an empty scope = %v", rows)
		}
		return nil
	}})
	root := kerneltest.New(t)
	kerneltest.Mount(root, provider, consumer)
	if !ran {
		t.Fatal("the consumer did not run")
	}
}

// composition/applied: an event of the open class, dispatched only by the fiber
// that provides the owning key (07-c4-plugin-sdk.md:461).
func TestAppliedIsOwnedByItsKeyHolder(t *testing.T) {
	owner := kernel.NewKey[int]("reload-standin", 0)
	applied := composition.NewApplied(owner)
	if applied.Name() != "composition/applied" || applied.Owner().Name() != "reload-standin" {
		t.Fatalf("name %q owner %q", applied.Name(), applied.Owner().Name())
	}
	var heard []composition.Applied
	listener := kernel.Define(kernel.Plugin{ID: "aiops/drift", Hooks: []kernel.HookUse{applied.Use(kernel.DefaultStage)},
		Apply: func(c *kernel.Context, _ any) error {
			applied.On(c, kernel.DefaultStage, func(_ context.Context, a composition.Applied) (any, error) {
				heard = append(heard, a)
				return nil, nil
			})
			return nil
		}})
	var dispatch kernel.EventDispatcher[composition.Applied]
	holder := kernel.Define(kernel.Plugin{ID: "aiops/reload-standin", Provides: []kernel.AnyKey{owner}, Apply: func(c *kernel.Context, _ any) error {
		dispatch = applied.Bind(c)
		return owner.Provide(c, 1)
	}})
	root := kerneltest.New(t)
	kerneltest.Mount(root, listener, holder)
	want := composition.Applied{Digest: composition.Digest{3}, Generation: 2, Report: composition.Report{Pass: true, Rows: []composition.RowReport{{ID: "a", Outcome: "pass", State: kernel.Active}}}}
	if err := dispatch.Emit(context.Background(), kernel.Unscoped, want); err != nil {
		t.Fatal(err)
	}
	if len(heard) != 1 || heard[0].Generation != 2 || !heard[0].Report.Pass || heard[0].Digest != want.Digest {
		t.Fatalf("heard %v", heard)
	}
	// A fiber that does not provide the key cannot dispatch.
	intruder := deftest.Plugin("aiops/intruder", nil, nil, nil, func(c *kernel.Context) error { applied.Bind(c); return nil })
	if err := deftest.Failure(t, root.Plugin(intruder, nil)); err == nil {
		t.Fatal("no error")
	}
}

// composition is open: any plugin may provide and inject it (03-c3-kernel.md:64).
func TestKeyIsOpenUnderBindingMaps(t *testing.T) {
	_, provider := fake.New()
	consumer := deftest.Plugin("aiops/consumer", []kernel.Dep{kernel.Need(composition.Key)}, nil, nil, func(c *kernel.Context) error { _ = composition.Key.Of(c); return nil })
	root := deftest.Bound(t, deftest.Config(nil, nil))
	deftest.Active(t, root.Plugin(provider, nil))
	deftest.Active(t, root.Plugin(consumer, nil))
}
